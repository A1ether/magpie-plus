package routes

import (
	"sort"
	"time"
)

// ExpiryFirst spends what is about to be lost first: an account whose
// soonest credit lot runs out earliest answers first, whatever its windows
// renew by, since the batch is gone when it expires and nothing brings it
// back. That is the one thing Smart cannot see by itself: a plugin that
// doesn't report the batch (Qoder's package behind a cookie, CodeArts'
// ledger, Trae's check-ins) leaves Smart ranking the account by its window
// reset, and a week's reset can come after a day's batch is lost.
//
// The power-meter sidecar knows the batches (internal/powermeterexpiry), so a
// pool routed this way spends the account with the least time left on its
// soonest batch first, and the next one after it. To the hour: batches a
// few minutes apart are spent in Smart's order among themselves, so a
// request doesn't change which account answers over a minute's difference.
//
// The rest is Smart's: a seat the sidecar knows nothing of goes after
// those it does, in Smart's order (a provider it doesn't cover, a new
// account whose batches haven't been read, or the sidecar off, all leave
// the pool as Smart would route it); one all but used up goes after those
// that can answer; and alike seats keep the order Smart gave them, which
// also keeps a vendor's prompt cache warm. Learning seats come first among
// the unknown ones, as in Smart: a seat kept behind them would never
// answer and never be known.
var ExpiryFirst = Strategy{ByExpiry: true, Order: func(_ string, seats []Seat) []int {
	smart := Smart.Order("", seats)
	rank := make([]int, len(seats))
	for i, s := range smart {
		rank[s] = i
	}
	idx := append([]int(nil), smart...)
	sort.SliceStable(idx, func(a, b int) bool {
		ia, ib := idx[a], idx[b]
		sa, sb := seats[ia], seats[ib]
		ta, tb := tier(sa.Used), tier(sb.Used)
		if (ta == 2) != (tb == 2) {
			return tb == 2 // all but used up waits, whatever it expires
		}
		if ta == 2 {
			return rank[ia] < rank[ib]
		}
		ea, eb := sa.Expires, sb.Expires
		if ea.IsZero() != eb.IsZero() {
			return !ea.IsZero() // known expiries before unknown ones
		}
		if !ea.IsZero() {
			ea, eb = ea.Truncate(time.Hour), eb.Truncate(time.Hour)
			if !ea.Equal(eb) {
				return ea.Before(eb)
			}
		}
		return rank[ia] < rank[ib]
	})
	return idx
}}
