// Package powermeterproxy exposes the power-meter sidecar's API under
// /api/powermeter/ on magpie's GUI mux, so the Balance page reads it with the
// window's own origin. It reverse-proxies to the sidecar over loopback,
// injecting the per-run token the sidecar requires, and answers a status
// endpoint the page uses to tell "starting", "up" and "down" apart.
//
// The proxy never invents a sidecar state: when the supervisor hasn't
// reported a URL yet, or the health probe fails, it says so, instead of
// proxying to a guessed address.
package powermeterproxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
)

// A Supervisor is the slice of powermeterproc.Supervisor the proxy needs.
// The interface keeps this package testable with a fake, no real child.
type Supervisor interface {
	Enabled() bool
	Running() bool
	URL() string
	Token() string
	Health(ctx context.Context) error
	// LastError is why the sidecar last failed to come up, "" when it is up or
	// hasn't tried yet. Without it a sidecar that can never start would read
	// "starting" for ever.
	LastError() string
}

// Prefix is the mux subtree the proxy answers. Every request under it is
// forwarded to the sidecar with the "/api/powermeter" part stripped, keeping
// power-meter's own /api: /api/powermeter/summary becomes the sidecar's
// /api/summary, /api/powermeter/accounts/overview its /api/accounts/overview.
const Prefix = "/api/powermeter/"

// stripPrefix is Prefix without the trailing slash: the part removed from the
// inbound path, so power-meter's own /api prefix survives the forward.
const stripPrefix = "/api/powermeter"

// statusPath is the proxy's own endpoint, not forwarded: it reports the
// sidecar's state for the page's status chip.
const statusPath = Prefix + "status"

// A Handler serves the proxy. The zero value answers "disabled" for status
// and 503 for everything else.
type Handler struct {
	sup atomic.Value // Supervisor
}

// New returns a Handler bound to sup. sup may be nil (sidecar disabled); the
// caller hands a plain nil, not a typed-nil pointer, for the disabled case.
func New(sup Supervisor) *Handler {
	h := &Handler{}
	if sup != nil {
		h.sup.Store(sup)
	}
	return h
}

// supervisor returns the bound Supervisor, or nil when none was set.
func (h *Handler) supervisor() Supervisor {
	if v := h.sup.Load(); v != nil {
		return v.(Supervisor)
	}
	return nil
}

// Register mounts the proxy on mux. It is called unconditionally; the
// handler itself decides per request whether the sidecar is enabled, so a
// disabled install answers a clear status rather than a mux 404.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc(Prefix, h.serve)
}

// serve answers the status endpoint itself and reverse-proxies the rest.
func (h *Handler) serve(rw http.ResponseWriter, r *http.Request) {
	if r.URL.Path == statusPath {
		h.serveStatus(rw, r)
		return
	}
	sup := h.supervisor()
	if sup == nil || !sup.Enabled() {
		writeJSON(rw, http.StatusServiceUnavailable, map[string]any{
			"error": "power-meter sidecar is disabled",
			"state": "disabled",
		})
		return
	}
	base := sup.URL()
	if base == "" {
		body := map[string]any{
			"error": "power-meter sidecar is starting",
			"state": "starting",
		}
		if e := sup.LastError(); e != "" {
			body["error"] = "power-meter sidecar couldn't start: " + e
			body["state"] = "down"
			body["detail"] = e
		}
		writeJSON(rw, http.StatusServiceUnavailable, body)
		return
	}
	target, err := url.Parse(base)
	if err != nil {
		// The URL came from the sidecar's own ready line; a parse failure is a
		// bug, not a user state, so surface it as such.
		writeJSON(rw, http.StatusBadGateway, map[string]any{
			"error": "power-meter sidecar URL is unusable: " + err.Error(),
			"state": "error",
		})
		return
	}

	proxy := &httputil.ReverseProxy{
		// Rewrite (not Director) so the outgoing request is rebuilt cleanly:
		// SetURL re-derives scheme/host, and the prefix strip happens on the
		// outgoing path. The sidecar is loopback-only, so no X-Forwarded is set.
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			// SetURL joins the inbound path onto the target's (empty) base path;
			// strip "/api/powermeter" off, keeping power-meter's /api. Path and
			// RawPath are set together: an escaped inbound path left in RawPath
			// with a new Path makes the pair inconsistent and the proxy answers
			// 400. The result keeps its leading slash, always (power-meter's
			// routes all start /api).
			// "/api/powermeter/summary" -> "/api/summary": drop "/api/powermeter"
			// and re-anchor on power-meter's own /api.
			path := "/api" + strings.TrimPrefix(pr.In.URL.Path, stripPrefix)
			pr.Out.URL.Path = path
			pr.Out.URL.RawPath = ""
			// The sidecar answers to its own loopback host, not magpie's: send
			// the target's Host or its HTTP server rejects the request (400).
			pr.Out.Host = target.Host
			// The sidecar requires this token on every /api call but health;
			// inject it so the page never handles the secret. The Intent header
			// is power-meter's CSRF guard on its privileged routes (account
			// add/remove, reveal): magpie's mux has already kept cross-site and
			// framed requests off /api, so a forwarded request is same-origin and
			// earns the intent marker.
			pr.Out.Header.Set("X-Power-Meter-Token", sup.Token())
			pr.Out.Header.Set("X-Power-Meter-Intent", "magpie-gui")
			// The sidecar's privileged routes (account add/remove, reveal) refuse
			// an Origin that isn't loopback. The window's webview sends its own
			// scheme (wails://wails, and http://wails.localhost on Windows), which
			// the sidecar has no reason to know, so an honest request would be
			// turned away. Only this page reaches the proxy with the token and
			// intent above injected, so the sidecar judges it by its loopback Host.
			pr.Out.Header.Del("Origin")
		},
		// A sidecar that died between URL() and the dial is a bad gateway the
		// page can read, not a hung request.
		ErrorHandler: func(rw http.ResponseWriter, _ *http.Request, err error) {
			writeJSON(rw, http.StatusBadGateway, map[string]any{
				"error": "power-meter sidecar unreachable: " + err.Error(),
				"state": "down",
			})
		},
	}
	proxy.ServeHTTP(rw, r)
}

// serveStatus reports the sidecar's state for the page's status chip. It
// runs the health probe, so "up" means the sidecar actually answered, not
// only that the supervisor thinks a child exists.
func (h *Handler) serveStatus(rw http.ResponseWriter, r *http.Request) {
	sup := h.supervisor()
	state := map[string]any{"state": "disabled"}
	switch {
	case sup == nil || !sup.Enabled():
		// state stays "disabled"
	case sup.URL() == "":
		// A failed start is "down" with the reason, not "starting" for ever: the
		// supervisor keeps retrying, but the page should say why nothing works.
		if e := sup.LastError(); e != "" {
			state["state"] = "down"
			state["detail"] = "the sidecar couldn't start: " + e
		} else {
			state["state"] = "starting"
		}
	default:
		if err := sup.Health(r.Context()); err != nil {
			state["state"] = "down"
			state["detail"] = err.Error()
		} else {
			state["state"] = "up"
		}
	}
	if sup != nil {
		state["enabled"] = sup.Enabled()
		if u := sup.URL(); u != "" {
			state["url"] = u
		}
	}
	writeJSON(rw, http.StatusOK, state)
}

// writeJSON writes v as JSON with the status code, the GUI's own convention.
func writeJSON(rw http.ResponseWriter, code int, v any) {
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	rw.Header().Set("Cache-Control", "no-store")
	rw.WriteHeader(code)
	_ = json.NewEncoder(rw).Encode(v)
}
