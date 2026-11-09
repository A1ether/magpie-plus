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

// walletSidecar serves the Balance board's answer for Raccoon, whose plugin
// reports a balance and no window: routing is left without an allowance
// unless the sidecar's figure is weighed.
func walletSidecar(t *testing.T, bal float64) {
	t.Helper()
	soon := time.Now().Add(48 * time.Hour).Format(time.RFC3339)
	body := fmt.Sprintf(`{"providers":[
	 {"provider":"xiaohuanxiong","displayName":"商汤小浣熊 · RaccoonMadison","accountId":"商汤小浣熊 · RaccoonMadison",
	  "nextExpiryAt":%q,"balance":{"amount":%v,"currency":"CREDITS"},
	  "credits":[{"amount":%v,"expiresAt":%q}]}]}`, soon, bal, bal, soon)
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/summary" {
			http.NotFound(rw, r)
			return
		}
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	powermeterexpiry.SetSource(expirySource{srv.URL})
	t.Cleanup(func() { powermeterexpiry.SetSource(nil) })
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, _, ok := powermeterexpiry.Wallet("raccoon", "Raccoon-pu-01a175e1-f34a-7ad4-9f76-ce7bfe84260b"); ok {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("the sidecar was not read")
}

// Smart spends a balance-only account's expiring credits first, as the
// Balance board tells them, instead of keeping it unknown behind every
// known account: Raccoon's plugin answers "9000 credits" with no window, so
// without the sidecar a group never sent it a request. A pool routed In
// order asks the sidecar for no wallet, and with the sidecar off the account
// is unknown as before.
func TestSmartWeighsASidecarWallet(t *testing.T) {
	walletSidecar(t, 9000)
	old := allowances
	t.Cleanup(func() { allowances = old })
	resets := time.Now().Add(20 * 24 * time.Hour)
	allowances = func(agent string) map[string]provider.Allowance {
		if agent == "wb-agent" {
			return map[string]provider.Allowance{"夜雨声烦": {{Used: 10, Resets: resets, Span: 7 * 24 * time.Hour}}}
		}
		return nil
	}
	raccoon := provider.Provider{ID: "raccoon", Account: &provider.Account{Agent: "plugin", User: "Raccoon-pu-01a175e1-f34a-7ad4-9f76-ce7bfe84260b"}}
	wb := provider.Provider{ID: "workbuddy", Account: &provider.Account{Agent: "wb-agent", User: "夜雨声烦"}}
	heads := []candidate{
		{p: raccoon, model: "m", rest: "raccoon#Raccoon-pu-01a175e1-f34a-7ad4-9f76-ce7bfe84260b"},
		{p: wb, model: "m", rest: "workbuddy#夜雨声烦"},
	}
	g := provider.Provider{ID: provider.GroupPrefix + "g"}
	got, wg := weigh(g, heads, "", provider.Chat)
	if len(got) != 2 || got[0].p.ID != "raccoon" {
		t.Fatalf("a Smart pool = %v, want raccoon's credits running out in two days before the week's twenty", restsOf(got))
	}
	w := weighed(got[0], got[0].p, wg, false, provider.Chat)
	if !w.Known || w.Balance == nil || *w.Balance != 9000 || w.Unit != "credits" {
		t.Fatalf("the trace = %+v, want it known by 9000 credits", w)
	}
	if w.Expires != nil {
		t.Fatalf("a Smart pool told the expiry strategy's field: %v", w.Expires)
	}

	// In order weighs nobody, and asks the sidecar for no wallet: the listed
	// order stands
	ordered := provider.Provider{ID: provider.GroupPrefix + "g", Routing: provider.Ordered}
	got, wg = weigh(ordered, heads, "", provider.Chat)
	if got[0].p.ID != "raccoon" {
		t.Fatalf("In order = %v, want the listed order", restsOf(got))
	}
	if _, ok := wg.lefts[heads[0].allowanceKey()]; ok {
		t.Fatal("an ordered pool asked the sidecar for a wallet")
	}

	// the sidecar off: Raccoon is unknown, so the known account leads
	powermeterexpiry.SetSource(nil)
	got, _ = weigh(g, heads, "", provider.Chat)
	if got[0].p.ID != "workbuddy" {
		t.Fatalf("without the sidecar = %v, want the known account first", restsOf(got))
	}
}
