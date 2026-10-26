package powermeterproxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeSupervisor implements Supervisor with no child and no network, so the
// proxy's routing and status logic is tested in isolation.
type fakeSupervisor struct {
	enabled bool
	running bool
	url     string
	token   string
	lastErr string
	health  error // nil = healthy
}

func (f fakeSupervisor) Enabled() bool                { return f.enabled }
func (f fakeSupervisor) Running() bool                { return f.running }
func (f fakeSupervisor) URL() string                  { return f.url }
func (f fakeSupervisor) Token() string                { return f.token }
func (f fakeSupervisor) Health(context.Context) error { return f.health }
func (f fakeSupervisor) LastError() string            { return f.lastErr }

func readState(t *testing.T, h *Handler, path string) (int, map[string]any) {
	t.Helper()
	rw := httptest.NewRecorder()
	h.serve(rw, httptest.NewRequest(http.MethodGet, path, nil))
	var body map[string]any
	if err := json.Unmarshal(rw.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s: response isn't JSON: %v (%q)", path, err, rw.Body.String())
	}
	return rw.Code, body
}

// A disabled sidecar answers a clear "disabled" status and 503s a proxied
// path, instead of a bare mux 404 the page couldn't read.
func TestDisabledSidecar(t *testing.T) {
	h := New(fakeSupervisor{enabled: false})

	code, body := readState(t, h, statusPath)
	if code != http.StatusOK || body["state"] != "disabled" {
		t.Fatalf("status = %d %v, want 200 disabled", code, body)
	}

	code, body = readState(t, h, Prefix+"summary")
	if code != http.StatusServiceUnavailable || body["state"] != "disabled" {
		t.Fatalf("proxied path while disabled = %d %v, want 503 disabled", code, body)
	}
}

// A supervisor that hasn't printed its ready line yet is "starting": the
// proxy must not guess an address.
func TestStartingSidecar(t *testing.T) {
	h := New(fakeSupervisor{enabled: true, url: ""})

	code, body := readState(t, h, statusPath)
	if body["state"] != "starting" {
		t.Fatalf("status = %d %v, want starting", code, body)
	}
	code, body = readState(t, h, Prefix+"summary")
	if code != http.StatusServiceUnavailable || body["state"] != "starting" {
		t.Fatalf("proxied path while starting = %d %v, want 503 starting", code, body)
	}
}

// A sidecar that keeps failing to start is "down" with the reason, not
// "starting" for ever: the page's Diagnostics can say what's wrong.
func TestFailedStartReportsReason(t *testing.T) {
	h := New(fakeSupervisor{enabled: true, lastErr: "starting powermeter: exec: no such file"})

	_, body := readState(t, h, statusPath)
	if body["state"] != "down" {
		t.Fatalf("status = %v, want down", body)
	}
	if d, _ := body["detail"].(string); !strings.Contains(d, "no such file") {
		t.Fatalf("status detail = %q, want the supervisor's error", body["detail"])
	}

	code, body := readState(t, h, Prefix+"summary")
	if code != http.StatusServiceUnavailable || body["state"] != "down" {
		t.Fatalf("proxied path after a failed start = %d %v, want 503 down", code, body)
	}
	if d, _ := body["detail"].(string); !strings.Contains(d, "no such file") {
		t.Fatalf("proxied error = %q, want the supervisor's error", body["error"])
	}
}

// When the health probe fails, status is "down" — a failed read means
// unknown, never a guessed-up.
func TestDownSidecar(t *testing.T) {
	h := New(fakeSupervisor{
		enabled: true, url: "http://127.0.0.1:1", health: errors.New("connection refused"),
	})
	_, body := readState(t, h, statusPath)
	if body["state"] != "down" {
		t.Fatalf("status = %v, want down", body)
	}
}

// A healthy sidecar gets requests proxied: the prefix is stripped and the
// token injected, so the page never handles the secret.
func TestProxiesWithTokenAndStripsPrefix(t *testing.T) {
	var gotPath, gotToken string
	backend := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotToken = r.Header.Get("X-Power-Meter-Token")
		rw.Header().Set("Content-Type", "application/json")
		rw.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()

	h := New(fakeSupervisor{enabled: true, url: backend.URL, token: "tok-123"})
	// A real mux request arrives with an absolute URL; httptest.NewRequest with a
	// bare path leaves Scheme/Host empty, which the reverse proxy rejects. Give
	// it the shape a server hands the handler.
	req := httptest.NewRequest(http.MethodGet, "http://magpie.local"+Prefix+"summary", nil)
	rw := httptest.NewRecorder()
	h.serve(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("proxied status = %d, want 200 (%q)", rw.Code, rw.Body.String())
	}
	if gotPath != "/api/summary" {
		t.Fatalf("backend saw path %q, want /api/summary (only /api/powermeter stripped)", gotPath)
	}
	if gotToken != "tok-123" {
		t.Fatalf("backend saw token %q, want the injected tok-123", gotToken)
	}
	if !strings.Contains(rw.Body.String(), `"ok":true`) {
		t.Fatalf("backend body not passed through: %q", rw.Body.String())
	}
}

// A privileged call (account add/remove, reveal) must reach the sidecar the
// way its guard reads it: the intent marker there, and no Origin, since the
// window's webview says wails://wails and the sidecar refuses any Origin that
// isn't loopback.
func TestPrivilegedCallCarriesIntentAndNoOrigin(t *testing.T) {
	var intent, origin string
	var body string
	backend := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		intent = r.Header.Get("X-Power-Meter-Intent")
		origin = r.Header.Get("Origin")
		b := new(strings.Builder)
		_, _ = io.Copy(b, r.Body)
		body = b.String()
		rw.Header().Set("Content-Type", "application/json")
		rw.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()

	h := New(fakeSupervisor{enabled: true, url: backend.URL, token: "tok-123"})
	req := httptest.NewRequest(http.MethodPost, "http://magpie.local"+Prefix+"accounts/add",
		strings.NewReader(`{"provider":"codebuddy"}`))
	req.Header.Set("Origin", "wails://wails")
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	h.serve(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("proxied status = %d, want 200 (%q)", rw.Code, rw.Body.String())
	}
	if intent == "" {
		t.Fatal("the backend saw no X-Power-Meter-Intent: the sidecar would answer 400")
	}
	if origin != "" {
		t.Fatalf("the backend saw Origin %q, want none: the sidecar refuses a webview's", origin)
	}
	if body != `{"provider":"codebuddy"}` {
		t.Fatalf("the backend saw body %q, want the request's own", body)
	}
}
