package ledger

// checkout_test.go -- M15-05: the reports section and The Pass's tape classify every checkout by
// ADR 0032 §A.6's one rule (checkout.go -> tap.ClassifyCheckout). Pure: no database. The Go/SQL
// parity, the list = report criterion and the correction written through manual.Recorder are the
// database's half (checkout_parity_db_test.go).

import (
	"math/rand"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/policy"
)

// maltaDay is the business day of a Malta instant under a 05:00 cutoff -- what the engine freezes
// on a directed nfc/qr row (ADR 0032 §A.5).
func maltaDay(t *testing.T, at time.Time) *time.Time {
	t.Helper()
	s, ok := tap.BusinessDayStart(at, tap.DayCutoff{Cutoff: 5 * time.Hour, Timezone: "Europe/Malta"})
	if !ok {
		t.Fatal("Europe/Malta did not resolve")
	}
	return &s
}

// cutoffDay is the business day of a Malta instant under another cutoff (00:00, 23:00 -- the
// kademe values the seed and the review use).
func cutoffDay(t *testing.T, at time.Time, cutoff time.Duration) *time.Time {
	t.Helper()
	s, ok := tap.BusinessDayStart(at, tap.DayCutoff{Cutoff: cutoff, Timezone: "Europe/Malta"})
	if !ok {
		t.Fatal("Europe/Malta did not resolve")
	}
	return &s
}

// written stamps the database's write instant on an event (W's input).
func written(at time.Time) evOpt { return func(e *reportEvent) { e.CreatedAt = at } }

// dayOf freezes a business day on an event (S).
func dayOf(s *time.Time) evOpt { return func(e *reportEvent) { e.DayStart = s } }

// TestAccumulate_EveryCheckoutClassPicksOneBranch is the card's ledger criteria (M15-05 step 3,
// M15-06 step 1), one per class, in the August 2026 week the rest of this package's report cases
// use: Monday is 3 August. Every event is written at its own instant (created_at = occurred_at)
// unless the case is about W. Every case also runs checkoutsCountedOnce: each checkout of the
// period is in exactly one place (M15-06, the engine lens's M-DÜŞÜK 10).
func TestAccumulate_EveryCheckoutClassPicksOneBranch(t *testing.T) {
	t.Parallel()
	zone := malta(t)
	p := week(t, zone)
	mon := func(hh, mm int) time.Time { return at(t, zone, 2026, time.August, 3, hh, mm) }
	tue := func(hh, mm int) time.Time { return at(t, zone, 2026, time.August, 4, hh, mm) }
	now := at(t, zone, 2026, time.August, 20, 12, 0)
	// tapped is an engine row: written at its instant, its business day frozen.
	tapped := func(dir string, when time.Time, opts ...evOpt) reportEvent {
		return ev(maria, "Maria Borg", dir, when, append([]evOpt{written(when), dayOf(maltaDay(t, when))}, opts...)...)
	}
	// typed is a manager's row: no business day, written when the manager typed it.
	typed := func(dir string, when, wrote time.Time) reportEvent {
		return ev(maria, "Maria Borg", dir, when, manual(), written(wrote))
	}
	const monday, tuesday = 0, 1
	tests := []struct {
		name                            string
		evs                             []reportEvent
		worked                          time.Duration
		open                            []time.Time
		tooLong, startedEarlier, shifts int
		outWithoutIn                    int
		// inMissingOn are the local days (0 = Monday) the person's InMissing mark is set on.
		inMissingOn []int
	}{
		{
			name:   "closes: an in and an out of one business day",
			evs:    []reportEvent{tapped("in", mon(9, 0)), tapped("out", mon(17, 0))},
			worked: 8 * time.Hour, shifts: 1,
		},
		{
			name:   "closes: the Rusty Bar night is one business day (05:00)",
			evs:    []reportEvent{tapped("in", mon(18, 5)), tapped("out", tue(2, 10))},
			worked: 8*time.Hour + 5*time.Minute, shifts: 1,
		},
		{
			name: "(b) the card's case: Monday 15:00 in, Tuesday 07:00 out (S Tuesday 05:00) -- " +
				"Monday is checkout-missing, Tuesday 07:00 is check-in-missing, nothing is paid",
			evs:          []reportEvent{tapped("in", mon(15, 0)), tapped("out", tue(7, 0))},
			open:         []time.Time{mon(15, 0)},
			outWithoutIn: 1, inMissingOn: []int{tuesday},
		},
		{
			name: "(a AND b) -> (b): Monday 07:00 in, Tuesday 07:00 out (24 h, S Tuesday 05:00) -- not TooLong",
			evs:  []reportEvent{tapped("in", mon(7, 0)), tapped("out", tue(7, 0))},
			open: []time.Time{mon(7, 0)}, outWithoutIn: 1, inMissingOn: []int{tuesday},
		},
		{
			name: "(b) then (c): the open slot empties, so Tuesday 08:00 has no in before it",
			evs: []reportEvent{tapped("in", mon(15, 0)), tapped("out", tue(7, 0)),
				tapped("out", tue(8, 0))},
			open:         []time.Time{mon(15, 0)},
			outWithoutIn: 2, inMissingOn: []int{tuesday},
		},
		{
			name:         "(c) its business day inside the period: a tapped checkout alone, Tuesday 10:00",
			evs:          []reportEvent{tapped("out", tue(10, 0))},
			outWithoutIn: 1, inMissingOn: []int{tuesday},
		},
		{
			name: "(c) its business day BEFORE the period: Monday 03:00 out alone (S Sunday 05:00) -- the seam, " +
				"StartedEarlier, not OutWithoutIn",
			evs:            []reportEvent{tapped("out", mon(3, 0))},
			startedEarlier: 1,
		},
		{
			// The seam's bound (review DÜŞÜK-3 g1): under a 00:00 cutoff Monday's business day starts
			// exactly at the period's first instant -- inside the period, so OutWithoutIn.
			name: "(c) its business day starting exactly at the period (a 00:00 cutoff): Monday 10:00 out alone",
			evs: []reportEvent{ev(maria, "Maria Borg", "out", mon(10, 0), written(mon(10, 0)),
				dayOf(cutoffDay(t, mon(10, 0), 0)))},
			outWithoutIn: 1, inMissingOn: []int{monday},
		},
		{
			// The mark's day (review DÜŞÜK-3 g3, the orchestrator's decision): a 23:00 cutoff puts
			// Tuesday 00:30 in the business day that began Monday 23:00; the mark is on TUESDAY, the
			// checkout's own local day -- the stamp a manager reads.
			name: "(c) across midnight under a 23:00 cutoff: Tuesday 00:30 out alone, its business day Monday 23:00 -- marked Tuesday",
			evs: []reportEvent{ev(maria, "Maria Borg", "out", tue(0, 30), written(tue(0, 30)),
				dayOf(cutoffDay(t, tue(0, 30), 23*time.Hour)))},
			outWithoutIn: 1, inMissingOn: []int{tuesday},
		},
		{
			name: "(a) the Phase B shape: 05:30 in, next day 04:30 out (23 h, one business day)",
			evs:  []reportEvent{tapped("in", mon(5, 30)), tapped("out", tue(4, 30))},
			open: []time.Time{mon(5, 30)}, tooLong: 1,
		},
		{
			name: "(a) the historical stale shape: S NULL, Monday 09:00 in, Tuesday 09:05 out (24 h)",
			evs: []reportEvent{ev(maria, "Maria Borg", "in", mon(9, 0), written(mon(9, 0))),
				ev(maria, "Maria Borg", "out", tue(9, 5), written(tue(9, 5)))},
			open: []time.Time{mon(9, 0)}, tooLong: 1,
		},
		{
			name: "(e) the manager's correction: engine 09:00 in, engine 20:00 out, typed 17:00 out written at 21:00 -- " +
				"8 h, and 20:00 is the surplus (StartedEarlier 1, ADR 0011's result), not a missing check-in",
			evs: []reportEvent{tapped("in", mon(9, 0)), typed("out", mon(17, 0), mon(21, 0)),
				tapped("out", mon(20, 0))},
			worked: 8 * time.Hour, shifts: 1, startedEarlier: 1,
		},
		{
			name: "(c) the break (the 5th version's HIGH): 09:00 in, 12:00 out, 17:00 out written after it -- " +
				"3 h; 17:00 is a missing check-in, not the surplus",
			evs: []reportEvent{tapped("in", mon(9, 0)), tapped("out", mon(12, 0)),
				tapped("out", mon(17, 0))},
			worked: 3 * time.Hour, shifts: 1, outWithoutIn: 1, inMissingOn: []int{monday},
		},
		{
			name: "the break moved by a backdated claim: + an out written 17:01 claiming 16:59:59 -- " +
				"17:00 becomes (e), 16:59:59 is (c): still one missing check-in",
			evs: []reportEvent{tapped("in", mon(9, 0)), tapped("out", mon(12, 0)),
				tapped("out", mon(16, 59).Add(59*time.Second), written(mon(17, 1))),
				tapped("out", mon(17, 0))},
			worked: 3 * time.Hour, shifts: 1, outWithoutIn: 1, startedEarlier: 1, inMissingOn: []int{monday},
		},
		{
			name: "Phase C's own flow: Monday 15:00 in, Tuesday 07:00 out (b), Monday's checkout approved later " +
				"(typed 23:00, written Tuesday 09:00) -- Monday is paid 8 h and Tuesday 07:00 is still (c): no in on its day",
			evs: []reportEvent{tapped("in", mon(15, 0)), typed("out", mon(23, 0), tue(9, 0)),
				tapped("out", tue(7, 0))},
			worked: 8 * time.Hour, shifts: 1, outWithoutIn: 1, inMissingOn: []int{tuesday},
		},
		{
			name: "Monday in, Tuesday in, Tuesday out: Monday stays open, Tuesday is paid",
			evs: []reportEvent{tapped("in", mon(9, 0)), tapped("in", tue(9, 0)),
				tapped("out", tue(17, 0))},
			worked: 8 * time.Hour, shifts: 1, open: []time.Time{mon(9, 0)},
		},
		{
			name:           "(d): a typed checkout with nothing before it",
			evs:            []reportEvent{typed("out", mon(17, 0), mon(18, 0))},
			startedEarlier: 1,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rep := accumulate(tc.evs, p, zone, now)
			var open []time.Time
			for _, o := range rep.Open {
				open = append(open, o.At)
			}
			if rep.Totals.Worked != tc.worked || rep.Totals.Shifts != tc.shifts ||
				rep.Totals.TooLong != tc.tooLong || rep.StartedEarlier != tc.startedEarlier ||
				rep.Totals.OutWithoutIn != tc.outWithoutIn ||
				!reflect.DeepEqual(open, tc.open) || rep.Totals.Open != len(tc.open) {
				t.Errorf("Worked %v Shifts %d TooLong %d StartedEarlier %d OutWithoutIn %d Open %v (%d); "+
					"want %v %d %d %d %d %v", rep.Totals.Worked, rep.Totals.Shifts, rep.Totals.TooLong,
					rep.StartedEarlier, rep.Totals.OutWithoutIn, open, rep.Totals.Open,
					tc.worked, tc.shifts, tc.tooLong, tc.startedEarlier, tc.outWithoutIn, tc.open)
			}
			var perPerson int
			var marked []int
			for _, ph := range rep.People {
				perPerson += ph.OutWithoutIn
				for d, m := range ph.Marks {
					if m.InMissing {
						marked = append(marked, d)
					}
				}
			}
			if perPerson != tc.outWithoutIn || !reflect.DeepEqual(marked, tc.inMissingOn) {
				t.Errorf("per person OutWithoutIn %d marked on days %v, want %d on %v", perPerson, marked, tc.outWithoutIn, tc.inMissingOn)
			}
			checkoutsCountedOnce(t, tc.evs, p, rep)
		})
	}
}

// checkoutsCountedOnce is the card's "no double counting" invariant (M15-06; the engine lens's
// M-DÜŞÜK 10): every checkout of the period is in exactly ONE place --
//
//	a closing pair (Worked, Awaiting or Refused: once, whatever its state)
//	+ Totals.TooLong + Totals.OutWithoutIn + StartedEarlier
//	+ the "another period's check-in" branch (accumulate's `continue`)
//	= the period's checkouts.
//
// The class of each checkout is the walker's (checkout.go) -- the subject here is accumulate's
// accounting, not the classification, which the parity test owns.
//
// 🔴 THE PAIRS ARE THE REPORT'S OWN FIGURES, NOT THIS WALK'S (review DÜŞÜK-5). A Report counts
// the counted pairs (Totals.Shifts) and carries the held-out ones as HOURS (Awaiting, Refused),
// so each state is bound to the report by its own figure: Shifts must be the number of counted
// pairs (whatever their length) and Worked their hours, Awaiting and Refused each the sum of their
// pairs' durations. A pair the report drops, counts twice or files under the wrong state moves one
// of those, so the walk's own pair count cannot stand in for the report's -- which is what the
// first version let it do (an accumulate that never counted an Awaiting pair passed it whenever a
// Refused one was present).
//
// ⚠️ A ZERO-LENGTH PAIR STAYS IN THE SWEEP (the M15-06 review's last round). Hours bind a held-out
// pair only when it has some, so a zero-length Awaiting or Refused pair -- an in and an out at one
// instant -- is counted by the walk alone (heldZero) and added to the total; a zero-length COUNTED
// pair is still bound, by Shifts. The previous round made that shape a PREMISE failure and kept a
// person's instants distinct, which pushed every zero-length pair out of the sweep: an accumulate
// that dropped one carrying a business day went green (measured).
//
// It needs no event at or after the period's end (a pair whose checkout lies in the closing tail
// is a pair of the period with no checkout in it): callers keep their events before p.To; the
// tail has its own test (TestAccumulate_ACheckoutInTheClosingTailIsNextWeeks).
func checkoutsCountedOnce(t *testing.T, evs []reportEvent, p Period, rep Report) {
	t.Helper()
	inPeriod := func(at time.Time) bool { return !at.Before(p.From) && at.Before(p.To) }
	var outs, otherPeriod, counted, awaiting, refused, heldZero int
	var worked, held, refusedHours time.Duration
	var chain checkoutChain
	var prev *reportEvent
	for i := range evs {
		e := evs[i]
		if !e.At.Before(p.To) {
			t.Fatalf("PREMISE: an event at or after the period's end (%v); this invariant counts no closing tail", e.At)
		}
		if i == 0 || evs[i-1].EmployeeID != e.EmployeeID {
			chain, prev = checkoutChain{}, nil
		}
		if e.Direction == "in" {
			chain.checkin(e.At, e.CreatedAt)
			prev = &evs[i]
			continue
		}
		class := chain.checkout(e.At, e.CreatedAt, e.DayStart)
		before := prev
		prev = &evs[i]
		if !inPeriod(e.At) {
			continue
		}
		outs++
		if class != tap.CheckoutCloses && class != tap.CheckoutTooLong {
			continue
		}
		if before == nil || before.Direction != "in" {
			t.Fatalf("PREMISE: a %q checkout at %v with no check-in before it", class, e.At)
		}
		if !inPeriod(before.At) {
			otherPeriod++
			continue
		}
		if class != tap.CheckoutCloses {
			continue
		}
		d := e.At.Sub(before.At)
		state := worse(endpointState(before.Verdict, before.Review), endpointState(e.Verdict, e.Review))
		switch {
		case state == HoursCounted:
			counted++
			worked += d
		case d == 0:
			// Held out and zero-length: no figure of the report can show it, so the walk counts it.
			heldZero++
		case state == HoursAwaiting:
			awaiting++
			held += d
		case state == HoursRefused:
			refused++
			refusedHours += d
		}
	}
	if rep.Totals.Shifts != counted || rep.Totals.Worked != worked || rep.Totals.Awaiting != held || rep.Totals.Refused != refusedHours {
		t.Errorf("the report's pairs: Shifts %d Worked %v Awaiting %v Refused %v; the period's closing pairs: "+
			"%d counted (%v), %d awaiting (%v), %d refused (%v) -- a pair dropped, doubled or misfiled",
			rep.Totals.Shifts, rep.Totals.Worked, rep.Totals.Awaiting, rep.Totals.Refused,
			counted, worked, awaiting, held, refused, refusedHours)
	}
	pairs := rep.Totals.Shifts + awaiting + refused + heldZero
	if got := pairs + rep.Totals.TooLong + rep.Totals.OutWithoutIn + rep.StartedEarlier + otherPeriod; got != outs {
		t.Errorf("checkouts counted %d times (pairs %d + TooLong %d + OutWithoutIn %d + StartedEarlier %d + another period's %d), "+
			"the period holds %d: a checkout counted twice or not at all",
			got, pairs, rep.Totals.TooLong, rep.Totals.OutWithoutIn, rep.StartedEarlier, otherPeriod, outs)
	}
}

// TestAccumulate_EveryCheckoutOfThePeriodIsCountedOnce is checkoutsCountedOnce over random weeks:
// several people, random directions, verdicts and reviews, business days frozen or absent, write
// instants in and out of order (so (e) and (c) both occur), and events from two days BEFORE the
// period (so the "another period's check-in" branch and the seam occur). Non-vacuity: every class
// and both seams are seen.
func TestAccumulate_EveryCheckoutOfThePeriodIsCountedOnce(t *testing.T) {
	t.Parallel()
	zone := malta(t)
	p := week(t, zone)
	rng := rand.New(rand.NewSource(1506))
	people := []uuid.UUID{maria, antoine, uuid.MustParse("33333333-3333-4333-8333-333333333333")}
	verdicts := []string{"ok", "ok", "ok", "flag", "reject"}
	reviews := []string{"", "", "approved", "rejected"}
	start := p.From.Add(-48 * time.Hour)
	span := p.To.Sub(start)
	seen := map[tap.CheckoutClass]int{}
	var otherPeriod, seam, zeroPairs int
	for round := 0; round < 3000; round++ {
		var evs []reportEvent
		for _, who := range people[:1+rng.Intn(len(people))] {
			n := rng.Intn(20)
			var instants []time.Time
			for i := 0; i < n; i++ {
				instants = append(instants, start.Add(time.Duration(rng.Int63n(int64(span/time.Minute)))*time.Minute))
			}
			sort.Slice(instants, func(i, j int) bool { return instants[i].Before(instants[j]) })
			for _, when := range instants {
				dir := "in"
				if rng.Intn(2) == 0 {
					dir = "out"
				}
				// Written at its instant, or up to six hours later (a typed or queued row).
				wrote := when.Add(time.Duration(rng.Int63n(int64(6*time.Hour))) * time.Duration(rng.Intn(2)))
				e := ev(who, "Staff "+who.String()[:4], dir, when,
					verdict(verdicts[rng.Intn(len(verdicts))], reviews[rng.Intn(len(reviews))]), written(wrote))
				if rng.Intn(4) != 0 {
					e.DayStart = maltaDay(t, when)
				} else {
					e.Manual = true
				}
				evs = append(evs, e)
			}
		}
		rep := accumulate(evs, p, zone, p.To.Add(72*time.Hour))
		checkoutsCountedOnce(t, evs, p, rep)
		if t.Failed() {
			t.Fatalf("round %d: events %+v", round, evs)
		}
		// Non-vacuity, read with the same walker.
		var chain checkoutChain
		var prevIn *time.Time
		for i, e := range evs {
			if i == 0 || evs[i-1].EmployeeID != e.EmployeeID {
				chain, prevIn = checkoutChain{}, nil
			}
			if e.Direction == "in" {
				chain.checkin(e.At, e.CreatedAt)
				v := e.At
				prevIn = &v
				continue
			}
			c := chain.checkout(e.At, e.CreatedAt, e.DayStart)
			if e.At.Before(p.From) {
				prevIn = nil
				continue
			}
			seen[c]++
			if (c == tap.CheckoutCloses || c == tap.CheckoutTooLong) && prevIn != nil && prevIn.Before(p.From) {
				otherPeriod++
			}
			if (c == tap.CheckoutOrphan || c == tap.CheckoutOtherDay) && e.DayStart.Before(p.From) {
				seam++
			}
			if c == tap.CheckoutCloses && prevIn != nil && prevIn.Equal(e.At) && e.DayStart != nil {
				zeroPairs++
			}
			prevIn = nil
		}
	}
	for _, c := range []tap.CheckoutClass{tap.CheckoutCloses, tap.CheckoutTooLong, tap.CheckoutOtherDay, tap.CheckoutOrphan, tap.CheckoutExtra, tap.CheckoutUnanchored} {
		if seen[c] == 0 {
			t.Errorf("PREMISE: the sweep never reached %q (%v)", c, seen)
		}
	}
	if otherPeriod == 0 || seam == 0 || zeroPairs == 0 {
		t.Errorf("PREMISE: another period's check-in seen %d times, the (b)/(c) seam %d times, a zero-length pair "+
			"with a business day %d times -- all three must occur", otherPeriod, seam, zeroPairs)
	}
}

// TestAccumulate_ACheckoutInTheClosingTailIsNextWeeks (review DÜŞÜK-3 g2): the read runs past the
// period (closingTail), so a checkout of next Monday is on hand -- and it is NEXT week's, whatever
// its class. A (b) (Sunday 20:00 in, next Monday 07:00 out of next Monday's business day) and a (c)
// (next Monday 03:00 out alone, its business day Sunday 05:00 -- INSIDE this period) are counted
// by neither this week's OutWithoutIn nor its StartedEarlier; next week counts them once each.
func TestAccumulate_ACheckoutInTheClosingTailIsNextWeeks(t *testing.T) {
	t.Parallel()
	zone := malta(t)
	p := week(t, zone)
	next := WeekOf(Date{Year: 2026, Month: time.August, Day: 10}, zone)
	if !next.From.Equal(p.To) {
		t.Fatalf("PREMISE: next week starts %v, this one ends %v", next.From, p.To)
	}
	now := at(t, zone, 2026, time.August, 20, 12, 0)
	tapped := func(who uuid.UUID, dir string, when time.Time) reportEvent {
		return ev(who, "Staff "+who.String()[:4], dir, when, written(when), dayOf(maltaDay(t, when)))
	}
	b := []reportEvent{tapped(maria, "in", at(t, zone, 2026, time.August, 9, 20, 0)), tapped(maria, "out", at(t, zone, 2026, time.August, 10, 7, 0))}
	c := []reportEvent{tapped(antoine, "out", at(t, zone, 2026, time.August, 10, 3, 0))}
	if !c[0].DayStart.Equal(at(t, zone, 2026, time.August, 9, 5, 0)) || c[0].DayStart.Before(p.From) {
		t.Fatalf("PREMISE: the (c)'s business day %v is not this period's Sunday", c[0].DayStart)
	}
	evs := append(append([]reportEvent(nil), b...), c...)
	this := accumulate(evs, p, zone, now)
	if this.Totals.OutWithoutIn != 0 || this.StartedEarlier != 0 || this.Totals.Open != 1 {
		t.Errorf("this week: OutWithoutIn %d StartedEarlier %d Open %d, want 0 0 1 (Sunday 20:00 open; both checkouts are next week's)",
			this.Totals.OutWithoutIn, this.StartedEarlier, this.Totals.Open)
	}
	// Next week reads from its own first instant, as Hours does.
	var read []reportEvent
	for _, e := range evs {
		if !e.At.Before(next.From) {
			read = append(read, e)
		}
	}
	nw := accumulate(read, next, zone, now)
	if nw.Totals.OutWithoutIn != 1 || nw.StartedEarlier != 1 {
		t.Errorf("next week: OutWithoutIn %d StartedEarlier %d, want 1 (the 07:00 of its own business day) and 1 (the 03:00, its seam)",
			nw.Totals.OutWithoutIn, nw.StartedEarlier)
	}
}

// accumulateBeforeTheClassification is accumulate as it stood at 898e607, the commit before
// M15-05, VERBATIM but for its name: the reference the "historical reports do not change"
// criterion is measured against. It is a test-only copy on purpose -- a frozen witness, never
// called by the product.
func accumulateBeforeTheClassification(evs []reportEvent, p Period, zone *time.Location, now time.Time) Report {
	var out Report
	people := map[uuid.UUID]*PersonHours{}
	venues := map[uuid.UUID]*VenueHours{}

	// person returns the accumulator for an employee, creating it on first sight so
	// that somebody whose only record is an unclosed check-in still gets a row.
	person := func(e reportEvent) *PersonHours {
		ph, ok := people[e.EmployeeID]
		if !ok {
			ph = &PersonHours{
				EmployeeID: e.EmployeeID,
				Name:       e.Name,
				Daily:      make([]time.Duration, p.Days()),
				Marks:      make([]DayMarks, p.Days()),
			}
			people[e.EmployeeID] = ph
		}
		return ph
	}

	// openEntry records a check-in that nothing closed. It is only reported when the
	// check-in itself falls inside the period: one that opened earlier belongs to the
	// report that covers it, and repeating it here would show the same anomaly in
	// every week until somebody fixed it.
	openEntry := func(e reportEvent) {
		if e.At.Before(p.From) || !e.At.Before(p.To) {
			return
		}
		ph := person(e)
		ph.Open++
		out.Totals.Open++
		if d := dayIndex(e.At, p, zone); d >= 0 && d < len(ph.Marks) {
			ph.Marks[d].Open = true
		}
		out.Open = append(out.Open, OpenEntry{
			EmployeeID:   e.EmployeeID,
			EmployeeName: e.Name,
			LocationName: e.LocationName,
			At:           e.At,
			Stale:        now.Sub(e.At) > tap.StaleOpenIn,
			Manual:       e.Manual,
		})
	}

	// arrival records what a check-in says about turning up on time. It runs for every
	// attributed check-in, closed or not: lateness is a property of ARRIVING, and a
	// person who came in late and forgot to tap out was still late.
	arrival := func(e reportEvent) {
		if e.At.Before(p.From) || !e.At.Before(p.To) {
			return
		}
		ph := person(e)
		if e.Manual {
			ph.ManualArrivals++
			out.Totals.ManualArrivals++
			if d := dayIndex(e.At, p, zone); d >= 0 && d < len(ph.Marks) {
				ph.Marks[d].Manual = true
			}
		}
		if e.Shift == nil {
			ph.Unmeasured++
			return
		}
		late := tap.MinutesLate(e.At, *e.Shift)
		if late == nil {
			// The zone would not load. M4-05's rule is to decline rather than measure
			// against the wrong wall clock, and the screen says "not measured here".
			ph.Unmeasured++
			return
		}
		if *late > 0 {
			ph.LateShifts++
			ph.LateBy += time.Duration(*late) * time.Minute
		}
	}

	var open *reportEvent
	var openOwner uuid.UUID
	flush := func() {
		if open != nil {
			openEntry(*open)
			open = nil
		}
	}

	for i := range evs {
		e := evs[i]
		if open != nil && openOwner != e.EmployeeID {
			flush()
		}
		openOwner = e.EmployeeID
		if e.Direction == "in" {
			// A second `in` on top of an open one: the first was never closed.
			flush()
			arrival(e)
			open = &evs[i]
			continue
		}
		// A checkout.
		if open == nil {
			if !e.At.Before(p.From) && e.At.Before(p.To) {
				out.StartedEarlier++
			}
			continue
		}
		in := *open
		open = nil
		if in.At.Before(p.From) || !in.At.Before(p.To) {
			// The interval belongs to another period; its closing tap merely ends the
			// chain here.
			continue
		}
		worked := e.At.Sub(in.At)
		ph := person(in)
		if worked > closingTail {
			// 🔴 NOT A SHIFT. The engine annotates a closing tap this far from its
			// check-in as a probable forgotten checkout, and paying it would charge
			// payroll for the hours nobody worked between them. The opening tap is
			// reported as open instead, which is the state a manager has to correct.
			out.Totals.TooLong++
			openEntry(in)
			continue
		}
		state := worse(endpointState(in.Verdict, in.Review), endpointState(e.Verdict, e.Review))
		switch state {
		case HoursAwaiting:
			ph.Awaiting += worked
			out.Totals.Awaiting += worked
		case HoursRefused:
			ph.Refused += worked
			out.Totals.Refused += worked
		default:
			ph.Worked += worked
			ph.Shifts++
			out.Totals.Worked += worked
			out.Totals.Shifts++
			d := dayIndex(in.At, p, zone)
			if d >= 0 && d < len(ph.Daily) {
				ph.Daily[d] += worked
			}
			// THE SHIFT'S VENUE IS ITS CHECK-IN'S, the same key VenueHours uses.
			switch {
			case in.Home == nil || in.LocationID == uuid.Nil:
				out.Totals.OtherVenueUnknown++
			case in.LocationID != *in.Home:
				out.Totals.OtherVenue++
				if d >= 0 && d < len(ph.Marks) {
					ph.Marks[d].Other = true
				}
			}
			if approvedFlag(in) || approvedFlag(e) {
				out.Totals.Approved++
			}
			v, ok := venues[in.LocationID]
			if !ok {
				v = &VenueHours{LocationID: in.LocationID, Name: in.LocationName}
				venues[in.LocationID] = v
			}
			v.Worked += worked
			v.Shifts++
		}
	}
	flush()

	out.People = make([]PersonHours, 0, len(people))
	for _, ph := range people {
		out.People = append(out.People, *ph)
	}
	// BY NAME, THEN BY ID. A stable order is what makes two views of the same week
	// comparable, and a name is not unique -- two people called Maria Borg would
	// otherwise swap places between requests, because Go randomises map iteration.
	sort.Slice(out.People, func(i, j int) bool {
		if out.People[i].Name != out.People[j].Name {
			return out.People[i].Name < out.People[j].Name
		}
		return out.People[i].EmployeeID.String() < out.People[j].EmployeeID.String()
	})
	out.Venues = make([]VenueHours, 0, len(venues))
	for _, v := range venues {
		out.Venues = append(out.Venues, *v)
	}
	sort.Slice(out.Venues, func(i, j int) bool {
		if out.Venues[i].Name != out.Venues[j].Name {
			return out.Venues[i].Name < out.Venues[j].Name
		}
		return out.Venues[i].LocationID.String() < out.Venues[j].LocationID.String()
	})
	sort.Slice(out.Open, func(i, j int) bool {
		if !out.Open[i].At.Equal(out.Open[j].At) {
			return out.Open[i].At.Before(out.Open[j].At)
		}
		return out.Open[i].EmployeeName < out.Open[j].EmployeeName
	})
	return out
}

// TestAccumulate_AWeekWithoutABusinessDayIsWhatItWasBeforeTheClassification is the card's
// "historical reports do not change" criterion (M15-05; ADR 0032 §A.6 "Tarihsel satırlar
// değişmez"): on rows with no business day -- every row written before M15, every manual row -- a
// checkout can only be closes, (a) or (d), the three branches accumulate had, so the whole Report
// is bit for bit the one the pre-M15-05 function builds. Measured on random weeks: several people,
// random directions, verdicts, reviews, manual rows, venues, homes, shifts, created_at values and
// instants that collide, read from the period's start into the closing tail (the read's own
// window). The control: the same comparison DOES tell the two apart once a business day is set
// ((b)), so the equality is not vacuous.
func TestAccumulate_AWeekWithoutABusinessDayIsWhatItWasBeforeTheClassification(t *testing.T) {
	t.Parallel()
	zone := malta(t)
	p := week(t, zone)
	rng := rand.New(rand.NewSource(1505))
	people := []uuid.UUID{maria, antoine, uuid.MustParse("33333333-3333-4333-8333-333333333333")}
	verdicts := []string{"ok", "ok", "ok", "flag", "reject"}
	reviews := []string{"", "", "approved", "rejected"}
	span := p.To.Add(closingTail).Sub(p.From)
	for round := 0; round < 3000; round++ {
		var evs []reportEvent
		for _, who := range people[:1+rng.Intn(len(people))] {
			n := rng.Intn(24)
			var instants []time.Time
			for i := 0; i < n; i++ {
				// Whole minutes inside the read, so two records do meet on one instant.
				instants = append(instants, p.From.Add(time.Duration(rng.Int63n(int64(span/time.Minute)))*time.Minute))
			}
			sort.Slice(instants, func(i, j int) bool { return instants[i].Before(instants[j]) })
			for _, when := range instants {
				dir := "in"
				if rng.Intn(2) == 0 {
					dir = "out"
				}
				e := ev(who, "Staff "+who.String()[:4], dir, when,
					verdict(verdicts[rng.Intn(len(verdicts))], reviews[rng.Intn(len(reviews))]),
					written(p.From.Add(time.Duration(rng.Int63n(int64(span))))))
				if rng.Intn(4) == 0 {
					e.Manual = true
				}
				if rng.Intn(3) == 0 {
					e.LocationID, e.LocationName = venueB, "KF Sliema"
				}
				if rng.Intn(2) == 0 {
					h := venueA
					e.Home = &h
				}
				switch rng.Intn(3) {
				case 0:
					e.Shift = nil
				case 1:
					e.Shift = nightShift()
				}
				evs = append(evs, e)
			}
		}
		now := p.From.Add(time.Duration(rng.Int63n(int64(span + 72*time.Hour))))
		want := accumulateBeforeTheClassification(evs, p, zone, now)
		got := accumulate(evs, p, zone, now)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("round %d: a week with no business day reports differently than before M15-05\n got  %+v\n want %+v\n events %+v",
				round, got, want, evs)
		}
	}

	// Control: with a business day frozen on the checkout, (b) is a class the old function did not
	// have -- Monday 15:00 to Tuesday 07:00 was paid 16 h, and is now an open entry.
	in := ev(maria, "Maria Borg", "in", at(t, zone, 2026, time.August, 3, 15, 0))
	outAt := at(t, zone, 2026, time.August, 4, 7, 0)
	out := ev(maria, "Maria Borg", "out", outAt, dayOf(maltaDay(t, outAt)))
	now := at(t, zone, 2026, time.August, 20, 12, 0)
	old, cur := accumulateBeforeTheClassification([]reportEvent{in, out}, p, zone, now), accumulate([]reportEvent{in, out}, p, zone, now)
	if old.Totals.Worked != 16*time.Hour || cur.Totals.Worked != 0 || reflect.DeepEqual(old, cur) {
		t.Errorf("CONTROL: (b) paid %v before and %v now (want 16h and 0) -- the comparison above could not tell the two apart",
			old.Totals.Worked, cur.Totals.Worked)
	}
}

// TestTape_ACheckoutOfALaterBusinessDayDrawsNoInterval: the tape pairs by the report's walker
// (M15-05). Yesterday 15:00 in, today 07:00 out with today's business day from 05:00 is (b): no
// interval, so nobody is on shift between midnight and 07:00 -- before M15-05 the 16 h pair was
// drawn. A same-day pair is still drawn (the control).
func TestTape_ACheckoutOfALaterBusinessDayDrawsNoInterval(t *testing.T) {
	t.Parallel()
	zone := malta(t)
	midnight := time.Date(2026, time.August, 4, 0, 0, 0, 0, zone)
	now := midnight.Add(12 * time.Hour)
	v := tapeVenueRef{id: venueA, name: "KF St Julians"}
	who, mate := uuid.New(), uuid.New()
	evAt := func(person uuid.UUID, in bool, when time.Time) tapeEvent {
		return tapeEvent{who: person, at: when, in: in, venue: v, createdAt: when, dayStart: maltaDay(t, when)}
	}
	// One person per uuid order, as the query hands them (grouped by person).
	first, second := who, mate
	if second.String() < first.String() {
		first, second = second, first
	}
	evs := []tapeEvent{
		evAt(first, true, midnight.Add(-9*time.Hour)), // yesterday 15:00
		evAt(first, false, midnight.Add(7*time.Hour)), // today 07:00, S today 05:00 -> (b)
		evAt(second, true, midnight.Add(8*time.Hour)),
		evAt(second, false, midnight.Add(10*time.Hour)),
	}
	hours, venues := tapeOf(evs, midnight, now)
	if len(venues) != 1 {
		t.Fatalf("venues %+v, want one", venues)
	}
	for i, h := range hours {
		want := 0
		if !h.Before(midnight.Add(8*time.Hour)) && h.Before(midnight.Add(10*time.Hour)) {
			want = 1 // the control: 08:00-10:00
		}
		if venues[0].People[i] != want {
			t.Errorf("hour %s: %d on shift, want %d", h.In(zone).Format("15:04"), venues[0].People[i], want)
		}
	}
}

// engineInput is an evidence-less but valid NFC tap by an active person: the engine flags it
// (§5 row 7) and gives it a direction -- all the direction rule needs.
func engineInput(now time.Time, open *tap.Transaction, cutoff *tap.DayCutoff) tap.Input {
	loc := uuid.New()
	return tap.Input{
		Now:        now,
		Channel:    tap.ChannelNFC,
		SUN:        tap.SUNResult{Valid: true},
		Tag:        &tap.Tag{Status: tap.TagActive, Location: loc},
		Employee:   &tap.Employee{Status: tap.EmployeeActive, Location: &loc, ActivatedAt: now.Add(-10000 * time.Hour)},
		GPSRadiusM: 150,
		Debounce:   60 * time.Second,
		PolicySet: policy.Set{
			Guardrails: policy.DefaultGuardrails(),
			Baseline:   policy.BaselinePolicies(func(name string) uuid.UUID { return uuid.NewSHA1(uuid.Nil, []byte(name)) }),
		},
		LastOpenIn: open,
		DayCutoff:  cutoff,
	}
}

// simRow is one record of a simulated chain.
type simRow struct {
	id        uuid.UUID
	in        bool
	at, wrote time.Time
	day       *time.Time
	engine    bool
}

// lastOpenAt is GetLastOpenTransaction (db/queries/transactions.sql) over a simulated chain: the
// latest `in` at or before at, in (occurred_at, id) order, with no `out` after it at or before at.
func lastOpenAt(rows []simRow, at time.Time) *tap.Transaction {
	after := func(a, b simRow) bool { // a > b in (occurred_at, id)
		if !a.at.Equal(b.at) {
			return a.at.After(b.at)
		}
		return a.id.String() > b.id.String()
	}
	var best *simRow
	for i := range rows {
		r := rows[i]
		if !r.in || r.at.After(at) {
			continue
		}
		closed := false
		for _, o := range rows {
			if !o.in && after(o, r) && !o.at.After(at) {
				closed = true
				break
			}
		}
		if !closed && (best == nil || after(r, *best)) {
			best = &rows[i]
		}
	}
	if best == nil {
		return nil
	}
	return &tap.Transaction{ID: best.id, OccurredAt: best.at, Direction: tap.TypeIn}
}

// classes walks a simulated chain in (occurred_at, id) order through the report's walker and
// returns each checkout's class, by row.
func classes(rows []simRow) map[uuid.UUID]tap.CheckoutClass {
	sorted := append([]simRow(nil), rows...)
	sort.Slice(sorted, func(i, j int) bool {
		if !sorted[i].at.Equal(sorted[j].at) {
			return sorted[i].at.Before(sorted[j].at)
		}
		return sorted[i].id.String() < sorted[j].id.String()
	})
	var c checkoutChain
	out := map[uuid.UUID]tap.CheckoutClass{}
	for _, r := range sorted {
		if r.in {
			c.checkin(r.at, r.wrote)
			continue
		}
		out[r.id] = c.checkout(r.at, r.wrote, r.day)
	}
	return out
}

var sweepZones = []string{"Europe/Malta", "America/Santiago", "Australia/Lord_Howe", "Asia/Tehran", "America/St_Johns"}

// TestAccumulate_NoEngineCheckoutIsOfAnotherDayOrAnOrphanInPhaseA is the card's Phase A property
// (M15-05 step 3; ADR 0032 §A.6 "Faz A'da motor çıkışı (b) ya da (c) olmaz"): random engine chains
// -- tap.Decide on every tap, its open check-in read the way GetLastOpenTransaction reads it, its
// business day frozen on the row -- with random manager rows typed into the past later on (S NULL,
// written at the moment they are typed), across zones and cutoffs. Read back whole, no `out` the
// ENGINE wrote is (b) or (c): it closes, or a later correction made it the surplus (e). So the
// card's temporary mapping of (b)/(c) to StartedEarlier is reached only by fixtures, typed rows and
// (in Phase B) a chosen direction. Both classes the property allows are seen (non-vacuity).
func TestAccumulate_NoEngineCheckoutIsOfAnotherDayOrAnOrphanInPhaseA(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(15051))
	seen := map[tap.CheckoutClass]int{}
	gaps := []time.Duration{2 * time.Minute, 40 * time.Minute, 3 * time.Hour, 8 * time.Hour, 15 * time.Hour, 20 * time.Hour, 30 * time.Hour}
	for round := 0; round < 400; round++ {
		cutoff := &tap.DayCutoff{Cutoff: time.Duration(rng.Intn(96)) * 15 * time.Minute, Timezone: sweepZones[rng.Intn(len(sweepZones))]}
		clock := time.Date(2026, time.March, 20, 0, 0, 0, 0, time.UTC).Add(time.Duration(rng.Intn(48*60)) * time.Minute)
		first := clock
		var rows []simRow
		for step := 0; step < 50; step++ {
			clock = clock.Add(time.Second + time.Duration(rng.Int63n(int64(gaps[rng.Intn(len(gaps))]))))
			if rng.Intn(4) == 0 {
				// A manager types a record into the past; it is written now.
				rows = append(rows, simRow{id: uuid.New(), in: rng.Intn(2) == 0,
					at: first.Add(time.Duration(rng.Int63n(int64(clock.Sub(first))))), wrote: clock})
				continue
			}
			dec := tap.Decide(engineInput(clock, lastOpenAt(rows, clock), cutoff))
			if dec.Type == nil {
				t.Fatalf("round %d: the engine gave no direction (%s via %s)", round, dec.Verdict, dec.MatchedSid)
			}
			rows = append(rows, simRow{id: uuid.New(), in: *dec.Type == tap.TypeIn, at: clock, wrote: clock,
				day: dec.BusinessDayStart, engine: true})
		}
		got := classes(rows)
		for _, r := range rows {
			if !r.engine || r.in {
				continue
			}
			c := got[r.id]
			seen[c]++
			if c != tap.CheckoutCloses && c != tap.CheckoutExtra {
				t.Fatalf("round %d (cutoff %v %s): the engine's checkout at %v classifies %q, want closes or extra\nrows %+v",
					round, cutoff.Cutoff, cutoff.Timezone, r.at, c, rows)
			}
		}
	}
	if seen[tap.CheckoutCloses] == 0 || seen[tap.CheckoutExtra] == 0 {
		t.Errorf("PREMISE: the sweep saw %v -- it must reach both a closing checkout and a correction's surplus", seen)
	}
}

// TestLastOpen_TheCheckInItHidesIsOneTheEngineWouldNotClose is the card's step 6 property (ADR 0032
// §A.6 "GetLastOpenTransaction bu sınıflandırmayı kullanmaz"): GetLastOpenTransaction calls an `in`
// closed as soon as ANY later `out` exists, where the classification says (a) or (b) when that out
// does not close it. With the cutoff FIXED, hiding such an `in` changes nothing: for every later tap
// the engine, shown the hidden entry, still says `in` -- it is more than StaleOpenIn old or of an
// earlier business day (S is monotone). Swept over zones, every 15-minute cutoff and random gaps.
// The cutoff-CHANGE bound, where it does matter, is the database's half
// (TestOpenCheckIns_ACutoffChangeKeepsTheEntryListedAndInventsNoCheckout).
func TestLastOpen_TheCheckInItHidesIsOneTheEngineWouldNotClose(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(15056))
	hidden := 0
	for i := 0; i < 30000; i++ {
		c := tap.DayCutoff{Cutoff: time.Duration(rng.Intn(96)) * 15 * time.Minute, Timezone: sweepZones[rng.Intn(len(sweepZones))]}
		x := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(rng.Int63n(int64(3 * 365 * 24 * time.Hour))))
		y := x.Add(time.Duration(rng.Int63n(int64(40 * time.Hour))))
		s, ok := tap.BusinessDayStart(y, c)
		if !ok {
			t.Fatalf("%s did not resolve", c.Timezone)
		}
		class := tap.ClassifyCheckout(&tap.Prev{Direction: tap.TypeIn, At: x}, false, y, &s)
		if class == tap.CheckoutCloses {
			continue
		}
		hidden++
		z := y.Add(time.Duration(rng.Int63n(int64(48 * time.Hour))))
		dec := tap.Decide(engineInput(z, &tap.Transaction{ID: uuid.New(), OccurredAt: x, Direction: tap.TypeIn}, &c))
		if dec.Type == nil || *dec.Type != tap.TypeIn {
			t.Fatalf("cutoff %v %s: an `in` at %v hidden by a %q checkout at %v would be CLOSED by a tap at %v (%v) -- "+
				"GetLastOpenTransaction's shortcut would then lose a pair", c.Cutoff, c.Timezone, x, class, y, z, dec.Type)
		}
	}
	if hidden < 500 {
		t.Errorf("PREMISE: only %d hidden entries were tried", hidden)
	}
}
