package routes

import (
	"slices"
	"testing"
	"time"
)

// ExpiryFirst spends what runs out soonest first, across the pools' own
// window resets: the batch's expiry is what is lost, while a window that
// renews later keeps its allowance. Unknown expiries go after known ones
// in Smart's order, an account all but used up waits, and alike expiries
// keep Smart's order, the vendor's prompt cache warm.
func TestExpiryFirstSpendsSoonestFirst(t *testing.T) {
	now := time.Now()
	at := func(d time.Duration) time.Time { return now.Add(d) }
	// two batches in one hour, both sides of it clear of the hour's edge
	hour := now.Add(48 * time.Hour).Truncate(time.Hour)
	cases := []struct {
		name  string
		seats []Seat
		want  []int
	}{
		{
			name: "soonest first, unknown after, spent last",
			seats: []Seat{
				{Key: "a", Known: true, Used: 10, Soon: []time.Time{at(30 * 24 * time.Hour)}, Expires: at(20 * 24 * time.Hour)},
				{Key: "b", Known: true, Used: 40, Soon: []time.Time{at(2 * 24 * time.Hour)}, Expires: at(48 * time.Hour)},
				{Key: "c", Known: true, Used: 5}, // the board knows no batch
				{Key: "d", Known: true, Used: 100, Expires: at(time.Hour)},
			},
			want: []int{1, 0, 2, 3},
		},
		{
			// an unknown expiry doesn't beat a known one even when Smart
			// would renew it sooner: the routing is about batches
			name: "known before unknown",
			seats: []Seat{
				{Key: "a", Known: true, Used: 10, Soon: []time.Time{at(time.Hour)}},
				{Key: "b", Known: true, Used: 10, Soon: []time.Time{at(10 * 24 * time.Hour)}, Expires: at(20 * 24 * time.Hour)},
			},
			want: []int{1, 0},
		},
		{
			// batches in the same hour are alike: Smart's order stands
			name: "same hour keeps Smart's order",
			seats: []Seat{
				{Key: "a", Known: true, Used: 10, Soon: []time.Time{at(3 * time.Hour)}, Expires: hour.Add(time.Minute)},
				{Key: "b", Known: true, Used: 10, Soon: []time.Time{at(2 * time.Hour)}, Expires: hour},
			},
			want: []int{1, 0},
		},
		{
			// nothing known: exactly Smart's order
			name: "none known",
			seats: []Seat{
				{Key: "a", Known: true, Used: 10, Soon: []time.Time{at(10 * 24 * time.Hour)}},
				{Key: "b", Known: true, Used: 10, Soon: []time.Time{at(24 * time.Hour)}},
				{Key: "c", Known: true, Used: 95},
			},
			want: []int{1, 0, 2},
		},
		{
			// one all but used up with a batch expiring first still waits:
			// it can't answer, and the others spend theirs by then
			name: "spent waits",
			seats: []Seat{
				{Key: "a", Known: true, Used: 100, Expires: at(10 * time.Minute)},
				{Key: "b", Known: true, Used: 10, Expires: at(10 * 24 * time.Hour)},
			},
			want: []int{1, 0},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Apply(ExpiryFirst, "group/g", c.seats); !slices.Equal(got, c.want) {
				t.Fatalf("order = %v, want %v", got, c.want)
			}
		})
	}
}

// ExpiryFirst asks the gateway for Expires (Strategy.ByExpiry), since
// nothing else fills it; the other routings don't pay for the sidecar.
func TestExpiryFirstIsByExpiry(t *testing.T) {
	if !ExpiryFirst.ByExpiry {
		t.Fatal("ExpiryFirst doesn't ask the gateway for Expires")
	}
	for name, s := range Builtin {
		if name == "expiry" {
			continue
		}
		if s.ByExpiry {
			t.Fatalf("%s weighs by Expires too, and would ask the sidecar for every pool", name)
		}
	}
}

// The routing a provider or group is saved with is served by plugins
// rotation: "expiry" is in Builtin.
func TestExpiryFirstIsABuiltin(t *testing.T) {
	s, ok := Builtin["expiry"]
	if !ok {
		t.Fatal("\"expiry\" is not among Builtin")
	}
	if s.Order == nil {
		t.Fatal("\"expiry\" has no order")
	}
}
