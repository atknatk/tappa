package ledger

// checkout.go -- ADR 0032 §A.6's checkout classification, as the ledger's readers walk a chain
// (M15-05). The class itself is tap.ClassifyCheckout's -- the Go half of the one rule whose SQL
// half is tappa_checkout_class (migration 00064); this file only finds its three inputs, P, I and
// W, from the order the rows arrive in, and never decides a class of its own.
//
// 🔴 ONE WALKER, TWO READERS. The reports section's accumulate (report.go) and The Pass's tape
// (tape.go) both pair a person's records through it, so "does this checkout close the check-in
// before it" has one answer on the payroll page, on the hourly tape and -- through the SQL half --
// on the Odd shapes list (ListOpenCheckIns). A private copy of P/I/W in either reader would be
// the second-representation shape this repository keeps paying for.
//
// WHAT THE WALKER CAN SEE IS THE READ'S WINDOW. Both readers start their read at an instant
// (the period's first midnight; the tape's midnight - tap.StaleOpenIn), so a checkout whose
// predecessor lies before it has no P here (it classifies as (c) or (d)) where the SQL half,
// which looks back without a bound, may find one. For the report that is ADR 0032 §A.6's
// "Dönem sınırı": such a checkout is the period's seam and is counted StartedEarlier whichever
// of the two it is; the parity test (checkout_parity_db_test.go) runs on chains read whole.

import (
	"time"

	"github.com/atknatk/tappa/internal/domain/tap"
)

// checkoutChain is one person's walk state: their directed, non-practice records, fed in
// (occurred_at, id) order -- the order ListWorkedShiftEvents returns them in. The zero value is
// a person with nothing read yet; a reader starts a new one for every person.
type checkoutChain struct {
	// prev is P: the record fed last, whichever direction it had.
	prev *chainRecord
	// lastIn is the latest `in` fed so far. Because the feed is ordered by occurred_at, it is
	// also the latest by occurred_at, so "an `in` before this checkout with occurred_at >= S"
	// (I) is exactly "lastIn >= S".
	lastIn *time.Time
}

// chainRecord is what P contributes: its direction, its instant and when it was written.
type chainRecord struct {
	in        bool
	at        time.Time
	createdAt time.Time
}

// checkin feeds an `in`.
func (c *checkoutChain) checkin(at, createdAt time.Time) {
	c.prev = &chainRecord{in: true, at: at, createdAt: createdAt}
	v := at
	c.lastIn = &v
}

// checkout classifies an `out` against what was fed before it, then feeds it.
//
// W IS created_at AGAINST created_at, BOTH THE DATABASE'S (ADR 0032 §A.6; DEFAULT now(), which
// no statement writes -- TestTransactions_NoStatementWritesCreatedAt). Equal is not "later": the
// tie leans to (c), the reporting side, as tappa_checkout_class does.
func (c *checkoutChain) checkout(at, createdAt time.Time, dayStart *time.Time) tap.CheckoutClass {
	var prev *tap.Prev
	if c.prev != nil {
		dir := tap.TypeOut
		if c.prev.in {
			dir = tap.TypeIn
		}
		prev = &tap.Prev{Direction: dir, At: c.prev.at, WrittenLater: c.prev.createdAt.After(createdAt)}
	}
	dayHasIn := dayStart != nil && c.lastIn != nil && !c.lastIn.Before(*dayStart)
	class := tap.ClassifyCheckout(prev, dayHasIn, at, dayStart)
	c.prev = &chainRecord{in: false, at: at, createdAt: createdAt}
	return class
}

// consumesTheIn says whether a checkout of this class ends the open slot of the `in` before it:
// the three classes whose P is an `in` -- it closes it, it is too far from it (a), or it is of a
// later business day (b). In the other three (c, d, e) there is no `in` before it to consume.
func consumesTheIn(class tap.CheckoutClass) bool {
	switch class {
	case tap.CheckoutCloses, tap.CheckoutTooLong, tap.CheckoutOtherDay:
		return true
	}
	return false
}
