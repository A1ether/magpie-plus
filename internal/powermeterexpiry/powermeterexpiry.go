// Package powermeterexpiry tells the gateway's expiry routing when each
// account's soonest credit lot runs out, as the power-meter sidecar's
// Balance page shows it (its /api/summary expiryTimeline). It also gives
// Smart an account whose plugin reports a balance and no window: that is a
// prepaid wallet's credits left, and its soonest lot is the renewal Smart
// weighs (Wallet).
//
// power-meter reads batches the vendor plugins often don't report — Qoder's
// package behind a web cookie, CodeArts' console ledger, Trae's check-ins —
// so this is where "which batch expires first" comes from for a provider
// whose plugin says nothing of it. The sidecar is only a source of data:
// the gateway still decides, and a sidecar off or unread is no expiry,
// never an invented one (an unread summary leaves the accounts to the
// routing's own order).
package powermeterexpiry

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

// Source is what the sidecar's supervisor offers: whether it is on, where
// it listens, and the per-run token its /api calls require. The GUI's
// liveSupervisor is the one; a process where the sidecar is off, or that
// never set one, tells nothing.
type Source interface {
	Enabled() bool
	URL() string
	Token() string
}

// ttl is how long a summary is used before it is read again: a batch's
// expiry doesn't move, and the sidecar itself caches its providers, so
// reading it on every request would only cost. Short enough that a lot
// refreshed on the Balance page reaches routing within half a minute.
const ttl = 30 * time.Second

// readTimeout bounds a summary read, so a sidecar that stopped answering
// doesn't hold a request: an unread summary means unknown, as any failed
// read does.
const readTimeout = 2 * time.Second

var live = struct {
	mu   sync.Mutex
	at   time.Time
	data *data
	busy bool // a read is out: the last summary answers until it comes back
	gen  int  // which source that read was of: one of a replaced source is dropped
}{}

// source is the supervisor set by the GUI, nil when there is none. A
// pointer keeps it in one atomic word: an interface's two words are not
// written together.
var source atomic.Pointer[sourceBox]

type sourceBox struct{ s Source }

// SetSource says where the sidecar is, or nil to tell nothing (a process
// without a GUI, or a test between cases). The cached summary is dropped:
// the next question asks the new one.
func SetSource(s Source) {
	if s == nil {
		source.Store(nil)
	} else {
		source.Store(&sourceBox{s})
	}
	live.mu.Lock()
	live.at, live.data, live.busy = time.Time{}, nil, false
	live.gen++
	live.mu.Unlock()
}

// Earliest is when the soonest batch the sidecar knows for provider's
// account runs out, in the account provider is spelled as by magpie. ok is
// false when the sidecar is off or hasn't been read yet, the provider has
// no batch with an expiry, or the account can't be told from the sidecar's
// names — the routing then leaves it to its own order.
func Earliest(provider, user string) (time.Time, bool) {
	d := summary()
	if d == nil {
		return time.Time{}, false
	}
	return d.earliest(provider, user)
}

// Wallet is what the sidecar tells of provider's account user as a prepaid
// wallet: what it has left in its unit, and when its soonest batch runs
// out (zero when it knows none). ok is false when the sidecar is off or
// unread, the provider's card folds several accounts, the account can't be
// told, or the board tells it no balance — the same answer Earliest gives
// for an unknown expiry. It is what a plugin that reports a balance and no
// window leaves routing without (the Raccoon plugin), which keeps such an
// account behind the known ones until the Balance board supplies it.
func Wallet(provider, user string) (left float64, unit string, at time.Time, ok bool) {
	d := summary()
	if d == nil {
		return 0, "", time.Time{}, false
	}
	return d.wallet(provider, user)
}

// summary is the last read, refreshed behind the request once it is older
// than ttl. Reading it never blocks a request: the first request after a
// refresh asks for one and goes by what is known, which is nothing, and
// the routing falls back to its own order until the read lands (a few
// milliseconds, loopback). A failed read leaves nothing known, as any
// failed read does; it is not papered over with an older summary.
func summary() *data {
	live.mu.Lock()
	if live.data != nil && time.Since(live.at) < ttl {
		d := live.data
		live.mu.Unlock()
		return d
	}
	if !live.busy {
		live.busy = true
		go refresh()
	}
	d := live.data
	live.mu.Unlock()
	return d
}

// refresh reads the sidecar and keeps what it says. The source is asked
// here, not on every question: whether the sidecar is on is the setting's
// answer. A pool routed Smart asks for a balance only when an account's
// plugin told no allowance, and an expiry pool asks for every seat; either
// way this is read at most once per ttl.
func refresh() {
	live.mu.Lock()
	gen := live.gen
	live.mu.Unlock()
	box := source.Load()
	var d *data
	err := errNoSource
	if box != nil && box.s != nil && box.s.Enabled() && box.s.URL() != "" {
		d, err = fetch(box.s)
	}
	live.mu.Lock()
	if gen != live.gen {
		// the source was replaced while this read was out: its answer is
		// about a sidecar the user turned off or another one
		live.mu.Unlock()
		return
	}
	live.busy = false
	live.at = time.Now()
	if err == nil {
		live.data = d
	} else {
		live.data = nil
	}
	live.mu.Unlock()
}

// errNoSource says the question was asked with no sidecar to ask, which
// leaves nothing known.
var errNoSource = errors.New("power-meter sidecar is off")

// data is what the sidecar said, keyed by the magpie provider the batches
// are of, each account with the soonest of its batches that still counts
// and the balance its card tells.
type data struct {
	accounts map[string][]account
}

type account struct {
	// names it is known by: the display name as a whole, its part after the
	// platform's dot (the login the board shows), and the account id
	names []string
	at    time.Time
	// left is the card's balance in unit, when the board tells one
	left       float64
	unit       string
	hasBalance bool
}

// matching are the accounts of provider's user: a name the sidecar writes
// as it is in magpie (a login, an email, a masked phone) matches itself; a
// display name that holds it ("CodeArts 华为云 · a1ether" against an IAM
// name ending in a1ether) matches too; and a provider the sidecar knows one
// account of is that one, whatever it calls it, since there is nothing else
// the expiry or the balance could belong to. Two accounts the sidecar knows
// and neither name matches stay unknown: a wrong one would be another's
// credits.
func (d *data) matching(provider, user string) []account {
	list := d.accounts[provider]
	if len(list) == 0 {
		return nil
	}
	u := normal(user)
	var out []account
	for _, a := range list {
		for _, name := range a.names {
			n := normal(name)
			if u != "" && (u == n || len(n) >= 4 && strings.Contains(u, n)) {
				out = append(out, a)
				break
			}
		}
	}
	if len(out) == 0 && len(list) == 1 {
		return list
	}
	return out
}

// earliest is the soonest batch of provider's account user.
func (d *data) earliest(provider, user string) (time.Time, bool) {
	var at time.Time
	found := false
	for _, a := range d.matching(provider, user) {
		if a.at.IsZero() {
			continue
		}
		if !found || a.at.Before(at) {
			at, found = a.at, true
		}
	}
	return at, found
}

// wallet is matching's account with the soonest batch: what it has left and
// when that batch runs out. A card folding several accounts tells neither.
func (d *data) wallet(provider, user string) (float64, string, time.Time, bool) {
	list := d.matching(provider, user)
	var picked *account
	for i := range list {
		a := &list[i]
		if !a.hasBalance {
			continue
		}
		if picked == nil || a.at.Before(picked.at) {
			picked = a
		}
	}
	if picked == nil {
		return 0, "", time.Time{}, false
	}
	return picked.left, picked.unit, picked.at, true
}

// normal is a name as it is compared: its letters and digits, lower case,
// so a masked phone's stars, an IAM name's colons and a display name's
// spacing don't keep two spellings of one account apart.
func normal(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// powermeterProvider is the provider a sidecar provider's batches belong
// to in magpie, where the two name the same subscription differently
// (power-meter tells Qoder's sites and WorkBuddy's two by their own names).
var powermeterProvider = map[string]string{
	"qoder":          "qoder-cn",
	"qoder-intl":     "qoder",
	"trae":           "trae-cn",
	"trae-global":    "trae-global",
	"codebuddy":      "workbuddy",
	"codebuddy-intl": "workbuddy-ai",
	"codearts":       "codearts",
	"minimax":        "minimax-code",
	"qwenwork":       "qwenwork",
	"xiaohuanxiong":  "raccoon",
	"a1etheropenai":  "a1ether",
}

// summaryJSON is the part of the sidecar's /api/summary this package
// reads. A lot's amount is its remaining or granted count as the board
// says; both are counted as the board's timeline counts them. A card's
// balance is the account's own figure in its currency, which a plugin that
// tells no window leaves routing without.
type summaryJSON struct {
	Providers []struct {
		Provider     string `json:"provider"`
		DisplayName  string `json:"displayName"`
		AccountID    string `json:"accountId"`
		NextExpiryAt string `json:"nextExpiryAt"`
		Entries      []any  `json:"entries"`
		Balance      *struct {
			Amount   *float64 `json:"amount"`
			Currency string   `json:"currency"`
		} `json:"balance"`
		Credits []struct {
			Amount    float64 `json:"amount"`
			ExpiresAt *string `json:"expiresAt"`
		} `json:"credits"`
	} `json:"providers"`
}

// fetch reads the sidecar's summary and keeps each account's soonest
// expiry. The board's own timeline counts a batch with a time still ahead
// and something left in it, and adds a window boundary when a provider has
// no batch to show for one (Qoder's package resets, "未用完即作废"); this
// keeps the same two, so routing goes by what the board shows.
func fetch(src Source) (*data, error) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimSuffix(src.URL(), "/")+"/api/summary", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Power-Meter-Token", src.Token())
	client := &http.Client{Timeout: readTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &statusError{resp.StatusCode}
	}
	var s summaryJSON
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, err
	}
	d := &data{accounts: map[string][]account{}}
	now := time.Now()
	for _, p := range s.Providers {
		to := powermeterProvider[p.Provider]
		if to == "" {
			to = p.Provider
		}
		// a provider the board folds several accounts into one card
		// (A1etherOpenAi's project keys) has no expiry per account
		if len(p.Entries) > 0 {
			continue
		}
		at, ok := soonest(p.Credits, p.NextExpiryAt, now)
		left, unit, hasBalance := 0.0, "", false
		if p.Balance != nil && p.Balance.Amount != nil {
			// the card's balance is the account's own figure, not a share, so
			// routing weighs it as a prepaid wallet's (its credits left)
			left, unit, hasBalance = *p.Balance.Amount, strings.ToLower(strings.TrimSpace(p.Balance.Currency)), true
		}
		if !ok && !hasBalance {
			continue
		}
		names := []string{afterDot(p.DisplayName), p.DisplayName}
		if p.AccountID != "" {
			names = append(names, p.AccountID)
		}
		d.accounts[to] = append(d.accounts[to], account{names: names, at: at, left: left, unit: unit, hasBalance: hasBalance})
	}
	return d, nil
}

// soonest is the earliest of the batches with a time still ahead and a
// count left, and the next expiry the board computes beside them.
func soonest(lots []struct {
	Amount    float64 `json:"amount"`
	ExpiresAt *string `json:"expiresAt"`
}, next string, now time.Time) (time.Time, bool) {
	var at time.Time
	keep := func(t time.Time) {
		if t.After(now) && (at.IsZero() || t.Before(at)) {
			at = t
		}
	}
	for _, l := range lots {
		if l.ExpiresAt == nil || l.Amount <= 0 {
			continue
		}
		if t, err := time.Parse(time.RFC3339, *l.ExpiresAt); err == nil {
			keep(t)
		}
	}
	if t, err := time.Parse(time.RFC3339, next); err == nil {
		keep(t)
	}
	return at, !at.IsZero()
}

// afterDot is the account's name as the board writes it after the
// platform: "CodeBuddy · 夜雨声烦" is 夜雨声烦, and a name without a dot is
// all of it.
func afterDot(name string) string {
	if i := strings.LastIndex(name, "·"); i >= 0 {
		return strings.TrimSpace(name[i+len("·"):])
	}
	return strings.TrimSpace(name)
}

// statusError is the sidecar's answer when it isn't 200, so a refused read
// leaves nothing known rather than decoding an error page as a summary.
type statusError struct{ code int }

func (e *statusError) Error() string { return "power-meter summary: HTTP " + http.StatusText(e.code) }
