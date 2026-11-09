package powermeterexpiry

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakeSource is a sidecar the test points at.
type fakeSource struct {
	url     string
	token   string
	enabled bool
}

func (f fakeSource) Enabled() bool { return f.enabled }
func (f fakeSource) URL() string   { return f.url }
func (f fakeSource) Token() string { return f.token }

const summaryFixture = `{"providers":[
 {"provider":"qoder","displayName":"Qoder 国内版 · A1ether","accountId":"Qoder 国内版 · A1ether",
  "nextExpiryAt":"2099-12-01T00:00:00.000Z",
  "credits":[{"amount":100,"expiresAt":"2026-01-01T00:00:00.000Z"},{"amount":100,"expiresAt":"2099-11-09T08:49:07.506Z"}]},
 {"provider":"codebuddy","displayName":"CodeBuddy · 夜雨声烦","accountId":"CodeBuddy · 夜雨声烦",
  "nextExpiryAt":"2099-10-31T15:59:59.000Z","credits":[]},
 {"provider":"codebuddy","displayName":"CodeBuddy · 188****1835","accountId":"CodeBuddy · 188****1835",
  "nextExpiryAt":"2099-12-31T15:59:59.000Z","credits":[{"amount":7,"expiresAt":"2099-11-30T00:00:00.000Z"}]},
 {"provider":"codearts","displayName":"CodeArts 华为云 · a1ether","accountId":"CodeArts 华为云 · a1ether",
  "nextExpiryAt":"2099-10-31T15:20:32.258Z","credits":[{"amount":0,"expiresAt":"2099-01-01T00:00:00.000Z"}]},
 {"provider":"xiaohuanxiong","displayName":"商汤小浣熊 · RaccoonMadison","accountId":"商汤小浣熊 · RaccoonMadison",
  "nextExpiryAt":"2099-11-08T15:59:59.000Z","balance":{"amount":9000,"currency":"CREDITS"},
  "credits":[{"amount":1500,"expiresAt":"2099-11-08T15:59:59.000Z"},{"amount":7500,"expiresAt":null}]},
 {"provider":"trae-global","displayName":"Trae 国际版 · A1ether","accountId":"Trae 国际版 · A1ether",
  "balance":{"amount":0,"currency":"USD"},"credits":[]},
 {"provider":"a1etheropenai","displayName":"A1etherOpenAi","accountId":"",
  "entries":[{"id":"A1etherOpenAi · one"}],"credits":[]}
]}`

// sidecar serves summaryFixture with the token the source carries, and
// counts its reads.
func sidecar(t *testing.T, token string, reads *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/summary" {
			http.NotFound(rw, r)
			return
		}
		if r.Header.Get("X-Power-Meter-Token") != token {
			http.Error(rw, "token", http.StatusUnauthorized)
			return
		}
		*reads++
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte(summaryFixture))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// await waits for the read behind the request to land.
func await(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		live.mu.Lock()
		done := !live.busy && live.at.After(time.Time{})
		live.mu.Unlock()
		if done {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("the summary was not read")
}

func reset(t *testing.T) {
	t.Helper()
	SetSource(nil)
	t.Cleanup(func() { SetSource(nil) })
}

// TestEarliestByProvider maps the board's provider and account names onto
// magpie's: Qoder's one account under its own name, WorkBuddy's two by
// their logins, CodeArts' IAM name by the name it holds, and a provider
// whose card folds several accounts has no account expiry to give.
func TestEarliestByProvider(t *testing.T) {
	reset(t)
	var reads int
	srv := sidecar(t, "tok", &reads)
	SetSource(fakeSource{url: srv.URL, token: "tok", enabled: true})

	qoderWant := time.Date(2099, 11, 9, 8, 49, 7, 506_000_000, time.UTC)
	bookWant := time.Date(2099, 10, 31, 15, 59, 59, 0, time.UTC)
	phoneWant := time.Date(2099, 11, 30, 0, 0, 0, 0, time.UTC)
	artsWant := time.Date(2099, 10, 31, 15, 20, 32, 258_000_000, time.UTC)

	// the first question asks for a read and goes by nothing yet
	if at, ok := Earliest("qoder-cn", "01a0708c-2feb-7d2d-a9d3-cc03e2782303"); ok {
		t.Fatalf("an unread summary told %v", at)
	}
	await(t)

	for _, c := range []struct {
		provider, user string
		want           time.Time
	}{
		{"qoder-cn", "01a0708c-2feb-7d2d-a9d3-cc03e2782303", qoderWant}, // one account: it is that one
		{"workbuddy", "夜雨声烦", bookWant},                                 // its own lot on the card
		{"workbuddy", "188****1835", phoneWant},                         // its own, not the other's
		{"codearts", "iam::2685bc4135054f8b804bf604e64b6032:user:a1ether", artsWant},
	} {
		at, ok := Earliest(c.provider, c.user)
		if !ok || !at.Equal(c.want) {
			t.Errorf("%s/%s = %v, %v; want %v", c.provider, c.user, at, ok, c.want)
		}
	}
	if at, ok := Earliest("a1ether", "some-key"); ok {
		t.Errorf("a folded card told %v", at)
	}
	if reads == 0 {
		t.Fatal("the sidecar was never read")
	}
}

// TestWalletByProvider: a card that tells a balance and no window (Raccoon's
// credits) gives routing what the plugin left out — the figure left and when
// its soonest batch runs out — under the provider's name and its one
// account. A card with no balance tells none, and a folded card neither.
func TestWalletByProvider(t *testing.T) {
	reset(t)
	var reads int
	srv := sidecar(t, "tok", &reads)
	SetSource(fakeSource{url: srv.URL, token: "tok", enabled: true})
	Earliest("raccoon", "x")
	await(t)

	want := time.Date(2099, 11, 8, 15, 59, 59, 0, time.UTC)
	left, unit, at, ok := Wallet("raccoon", "Raccoon-pu-01a175e1-f34a-7ad4-9f76-ce7bfe84260b")
	if !ok || left != 9000 || unit != "credits" || !at.Equal(want) {
		t.Fatalf("Wallet(raccoon) = %v %q %v, %v; want 9000 credits %v", left, unit, at, ok, want)
	}
	if _, _, _, ok := Wallet("qoder-cn", "A1ether"); ok {
		t.Error("a card with no balance told one")
	}
	if _, _, _, ok := Wallet("a1ether", "some-key"); ok {
		t.Error("a folded card told a wallet")
	}
	// a balance with no batch to expire is a wallet with no expiry, never an
	// expiry at the zero time
	if at, ok := Earliest("trae-global", "A1ether"); ok {
		t.Errorf("a card with a balance alone told an expiry: %v", at)
	}
	if left, unit, at, ok := Wallet("trae-global", "A1ether"); !ok || left != 0 || unit != "usd" || !at.IsZero() {
		t.Errorf("Wallet(trae-global) = %v %q %v, %v; want an empty usd wallet with no expiry", left, unit, at, ok)
	}

	// the sidecar off tells nothing, without asking
	SetSource(fakeSource{url: srv.URL, token: "tok", enabled: false})
	Earliest("raccoon", "x")
	await(t)
	if _, _, _, ok := Wallet("raccoon", "Raccoon-pu-01a175e1-f34a-7ad4-9f76-ce7bfe84260b"); ok {
		t.Error("a sidecar off told a wallet")
	}
}

// TestEarliestIsUnknownWhenUndecided: a provider the board knows two
// accounts of tells neither when no name matches, and a sidecar off, a
// failed read or an empty provider tells nothing at all.
func TestEarliestIsUnknownWhenUndecided(t *testing.T) {
	reset(t)
	var reads int
	srv := sidecar(t, "tok", &reads)
	SetSource(fakeSource{url: srv.URL, token: "tok", enabled: true})
	Earliest("qoder-cn", "x")
	await(t)

	if at, ok := Earliest("workbuddy", "someone else"); ok {
		t.Errorf("an unmatched account among two told %v", at)
	}
	if at, ok := Earliest("minimax-code", "MiniMax237355"); ok {
		t.Errorf("a provider the board doesn't have told %v", at)
	}

	// the token is wrong: the sidecar refuses, and nothing is known
	SetSource(fakeSource{url: srv.URL, token: "wrong", enabled: true})
	Earliest("qoder-cn", "x")
	await(t)
	if at, ok := Earliest("qoder-cn", "01a0708c-2feb-7d2d-a9d3-cc03e2782303"); ok {
		t.Errorf("a refused read told %v", at)
	}

	// off tells nothing, without asking
	SetSource(fakeSource{url: srv.URL, token: "tok", enabled: false})
	Earliest("qoder-cn", "x")
	await(t)
	if at, ok := Earliest("qoder-cn", "01a0708c-2feb-7d2d-a9d3-cc03e2782303"); ok {
		t.Errorf("a sidecar off told %v", at)
	}
}

// TestSoonerOfLotAndBoundary: the board's next expiry counts beside a
// lot's own, so a package's reset that loses what is left (Qoder's window
// boundary) is not missed, and a batch already gone or empty is not told.
func TestSoonerOfLotAndBoundary(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	at := func(s string) *string { return &s }
	lots := func(pairs ...[2]any) []struct {
		Amount    float64 `json:"amount"`
		ExpiresAt *string `json:"expiresAt"`
	} {
		var out []struct {
			Amount    float64 `json:"amount"`
			ExpiresAt *string `json:"expiresAt"`
		}
		for _, p := range pairs {
			out = append(out, struct {
				Amount    float64 `json:"amount"`
				ExpiresAt *string `json:"expiresAt"`
			}{Amount: p[0].(float64), ExpiresAt: p[1].(*string)})
		}
		return out
	}
	got, ok := soonest(lots(
		[2]any{100.0, at("2026-10-20T00:00:00Z")}, // past
		[2]any{0.0, at("2026-10-28T00:00:00Z")},   // empty
		[2]any{5.0, at("2026-11-02T00:00:00Z")},
	), "2026-10-30T00:00:00Z", now)
	if want := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC); !ok || !got.Equal(want) {
		t.Fatalf("soonest = %v, %v; want the boundary %v", got, ok, want)
	}
	if _, ok := soonest(nil, "", now); ok {
		t.Fatal("nothing told an expiry")
	}
}

// TestNormalKeepsNamesApart: names are compared by letters and digits, so
// a masked phone and an IAM name match themselves and not a neighbour.
func TestNormalKeepsNamesApart(t *testing.T) {
	for in, want := range map[string]string{
		"188****1835":            "1881835",
		"iam::2685:user:a1ether": "iam2685usera1ether",
		"夜雨声烦":                   "夜雨声烦",
		"A1ether":                "a1ether",
	} {
		if got := normal(in); got != want {
			t.Errorf("normal(%q) = %q, want %q", in, got, want)
		}
	}
}
