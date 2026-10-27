package gateway

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/powermeterexpiry"
	"github.com/yetone/magpie/internal/provider"
)

// expirySidecar serves the two providers' soonest batches: Trae's runs out
// in two days, Qoder's in twenty.
func expirySidecar(t *testing.T) {
	t.Helper()
	soon := time.Now().Add(48 * time.Hour).Format(time.RFC3339)
	late := time.Now().Add(20 * 24 * time.Hour).Format(time.RFC3339)
	body := fmt.Sprintf(`{"providers":[
	 {"provider":"qoder","displayName":"Qoder 国内版 · A1ether","accountId":"Qoder 国内版 · A1ether","nextExpiryAt":%q},
	 {"provider":"trae","displayName":"Trae · A1ether","accountId":"Trae · A1ether","nextExpiryAt":%q}]}`, late, soon)
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/summary" {
			http.NotFound(rw, r)
			return
		}
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	src := expirySource{srv.URL}
	powermeterexpiry.SetSource(src)
	t.Cleanup(func() { powermeterexpiry.SetSource(nil) })
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := powermeterexpiry.Earliest("qoder-cn", "A1ether"); ok {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("the sidecar was not read")
}

type expirySource struct{ url string }

func (s expirySource) Enabled() bool { return true }
func (s expirySource) URL() string   { return s.url }
func (s expirySource) Token() string { return "" }

// Expiry first spends the batch running out soonest before one with more
// time, whatever the accounts' windows renew by: the group's heads are
// weighed together, and the trace carries when each one's batch expires.
// Without the sidecar's word the pool keeps Smart's order.
func TestExpiryRoutingSpendsSoonestBatchFirst(t *testing.T) {
	expirySidecar(t)
	qoder := provider.Provider{ID: "qoder-cn", Account: &provider.Account{Agent: "plugin", User: "A1ether"}}
	trae := provider.Provider{ID: "trae-cn", Account: &provider.Account{Agent: "plugin", User: "A1ether"}}
	heads := []candidate{
		{p: qoder, model: "m", rest: "qoder-cn#A1ether"},
		{p: trae, model: "m", rest: "trae-cn#A1ether"},
	}
	g := provider.Provider{ID: provider.GroupPrefix + "g", Routing: provider.ExpiryFirst}
	got, wg := weigh(g, heads, "", provider.Chat)
	if len(got) != 2 || got[0].p.ID != "trae-cn" {
		t.Fatalf("order = %v, want trae-cn (two days) before qoder-cn (twenty)", restsOf(got))
	}
	w := weighed(got[0], got[0].p, wg, false, provider.Chat)
	if w.Expires == nil || time.Until(*w.Expires) > 3*24*time.Hour {
		t.Fatalf("the trace's expiry = %v, want Trae's in two days", w.Expires)
	}

	// the sidecar off: nothing is known, so Smart's order (the listed one,
	// both alike) stands
	powermeterexpiry.SetSource(nil)
	got, wg = weigh(g, heads, "", provider.Chat)
	if len(got) != 2 || got[0].p.ID != "qoder-cn" {
		t.Fatalf("without the sidecar: %v, want the listed order", restsOf(got))
	}
	if _, ok := wg.expiry["qoder-cn@a1ether"]; ok {
		t.Fatal("an expiry was told with the sidecar off")
	}
}

// The sidecar only adds data to a pool routed by expiry: a pool routed
// another way never asks it, and its trace carries no expiry.
func TestOnlyExpiryRoutingAsksTheSidecar(t *testing.T) {
	expirySidecar(t)
	p := provider.Provider{ID: "qoder-cn", Routing: provider.Ordered,
		Account: &provider.Account{Agent: "plugin", User: "A1ether"}}
	key := provider.Provider{ID: "openrouter"}
	heads := []candidate{
		{p: p, model: "m", rest: "qoder-cn#A1ether"},
		{p: key, model: "m", rest: "openrouter"},
	}
	g := provider.Provider{ID: provider.GroupPrefix + "g", Routing: provider.Ordered}
	_, wg := weigh(g, heads, "", provider.Chat)
	if wg.expiry != nil {
		t.Fatalf("an ordered pool asked the sidecar: %v", wg.expiry)
	}
}
