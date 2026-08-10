package ledger

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// correction_test.go -- what an APPENDED record does to a week's hours (M6-08).
//
// 🔴 WHY IT IS HERE RATHER THAN IN internal/domain/manual. The M6-08 card's fifth
// criterion is "correcting an existing record = a new row + audit; no UPDATE". The
// no-UPDATE half is structural and belongs to the write path. THIS half -- whether
// appending a row corrects anything -- is entirely this package's arithmetic, and it
// was measured rather than assumed. The result is the reason internal/domain/manual
// carries CorrectionsOnlyShorten and the reason the confirmation screen says what it
// says.
//
// 🔴 THE RULE, IN ONE LINE: accumulate keeps a person's LATEST `in` and the EARLIEST
// `out` after it, so an appended record can only ever make an interval SHORTER.
// Half the corrections a manager would want are therefore not expressible as one row,
// and the two that are not are exactly the two that would RESTORE pay.
//
// ⚠️ THIS TEST DOES NOT SAY THE BEHAVIOUR IS WRONG. Each half of it is defended in
// accumulate's own comment and both defences hold on their own terms: pairing the
// first `in` with the `out` would over-count, and pairing both would double-count. It
// says the CONSEQUENCE, which nothing said before, and it goes red if the pairing ever
// changes -- at which point manual's constant and the screen's sentence have to move
// with it.

// TestReport_ACorrectingRecordCanOnlySHORTEN.
func TestReport_ACorrectingRecordCanOnlySHORTEN(t *testing.T) {
	t.Parallel()
	zone := time.UTC
	who := uuid.New()
	period := WeekOf(Date{Year: 2026, Month: time.August, Day: 5}, zone)
	at := func(h, m int) time.Time { return time.Date(2026, time.August, 5, h, m, 0, 0, zone) }
	ev := func(dir string, when time.Time) reportEvent {
		return reportEvent{EmployeeID: who, Name: "Maria Borg", At: when, Direction: dir,
			Verdict: "ok", Manual: true}
	}
	// `now` is a day later, so nothing here is "somebody is on shift right now".
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, zone)

	// THE TRUTH the manager is trying to record: 09:00 to 17:00, eight hours.
	cases := []struct {
		name string
		evs  []reportEvent
		// worked is the payroll figure; open and startedEarlier are where the row
		// that did NOT take effect ends up, because §4.6 says it is never lost.
		worked         time.Duration
		open           int
		startedEarlier int
		applied        bool
	}{
		{
			name:   "the truth, recorded once",
			evs:    []reportEvent{ev("in", at(9, 0)), ev("out", at(17, 0))},
			worked: 8 * time.Hour, applied: true,
		},
		{
			// UNDER-PAYS, AND THE CORRECTION DOES NOT REACH IT. The earliest `out`
			// wins, so the later, correct one is stranded and counted as a shift that
			// began before the period -- a true count under a sentence that is false
			// about this row.
			name:   "checkout typed too EARLY, then corrected",
			evs:    []reportEvent{ev("in", at(9, 0)), ev("out", at(12, 0)), ev("out", at(17, 0))},
			worked: 3 * time.Hour, startedEarlier: 1, applied: false,
		},
		{
			// OVER-PAYS, AND THE CORRECTION DOES REACH IT.
			name:   "checkout typed too LATE, then corrected",
			evs:    []reportEvent{ev("in", at(9, 0)), ev("out", at(17, 0)), ev("out", at(20, 0))},
			worked: 8 * time.Hour, startedEarlier: 1, applied: true,
		},
		{
			// UNDER-PAYS. The latest `in` wins, so the earlier, correct one is left
			// open and appears in the needs-action list.
			name:   "check-in typed too LATE, then corrected",
			evs:    []reportEvent{ev("in", at(9, 0)), ev("in", at(10, 0)), ev("out", at(17, 0))},
			worked: 7 * time.Hour, open: 1, applied: false,
		},
		{
			// OVER-PAYS, AND THE CORRECTION DOES REACH IT.
			name:   "check-in typed too EARLY, then corrected",
			evs:    []reportEvent{ev("in", at(8, 0)), ev("in", at(9, 0)), ev("out", at(17, 0))},
			worked: 8 * time.Hour, open: 1, applied: true,
		},
		{
			// 🔴 THE REMEDY THAT DOES WORK, AND IT IS UGLY. A compensating PAIR after a
			// wrongly early checkout restores the money -- and writes a statement about
			// the world that is not true (the person did not leave and come back).
			name: "the compensating pair after a checkout typed too early",
			evs: []reportEvent{
				ev("in", at(9, 0)), ev("out", at(12, 0)),
				ev("in", at(12, 1)), ev("out", at(17, 0)),
			},
			worked: 7*time.Hour + 59*time.Minute, applied: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := accumulate(c.evs, period, zone, now)
			if got.Totals.Worked != c.worked {
				t.Errorf("worked = %v, want %v", got.Totals.Worked, c.worked)
			}
			if got.Totals.Open != c.open {
				t.Errorf("open entries = %d, want %d -- a record that did not take effect is "+
					"still a record (§4.6) and has to be visible somewhere",
					got.Totals.Open, c.open)
			}
			if got.StartedEarlier != c.startedEarlier {
				t.Errorf("startedEarlier = %d, want %d", got.StartedEarlier, c.startedEarlier)
			}
			if applied := got.Totals.Worked == 8*time.Hour; applied != c.applied {
				t.Errorf("the correction reached the total = %v, want %v. If this flipped, "+
					"internal/domain/manual.CorrectionsOnlyShorten and the confirmation "+
					"screen's sentence are now wrong and have to move with it.",
					applied, c.applied)
			}
		})
	}
}

// TestReport_TheShortestIntervalWinsInBothDirections is the RULE behind the table
// above, stated once so a reader does not have to infer it from six rows.
//
// It is separate because the table measures CONSEQUENCES a manager sees and this
// measures the mechanism -- and the two can only be kept honest by each other.
func TestReport_TheShortestIntervalWinsInBothDirections(t *testing.T) {
	t.Parallel()
	zone := time.UTC
	who := uuid.New()
	period := WeekOf(Date{Year: 2026, Month: time.August, Day: 5}, zone)
	at := func(h int) time.Time { return time.Date(2026, time.August, 5, h, 0, 0, 0, zone) }
	ev := func(dir string, when time.Time) reportEvent {
		return reportEvent{EmployeeID: who, Name: "Maria Borg", At: when, Direction: dir, Verdict: "ok"}
	}
	// Three check-ins and three checkouts, in order. The latest `in` before the first
	// `out` is 11:00; the earliest `out` is 14:00. Three hours, not seven.
	evs := []reportEvent{
		ev("in", at(9)), ev("in", at(10)), ev("in", at(11)),
		ev("out", at(14)), ev("out", at(15)), ev("out", at(16)),
	}
	got := accumulate(evs, period, zone, time.Date(2026, time.August, 6, 12, 0, 0, 0, zone))
	if want := 3 * time.Hour; got.Totals.Worked != want {
		t.Fatalf("worked = %v, want %v -- the engine takes the LATEST in and the EARLIEST "+
			"out, which is the whole of why an appended record can only shorten",
			got.Totals.Worked, want)
	}
	if got.Totals.Open != 2 {
		t.Errorf("open = %d, want 2 (the two check-ins nothing closed)", got.Totals.Open)
	}
	if got.StartedEarlier != 2 {
		t.Errorf("startedEarlier = %d, want 2 (the two checkouts nothing opened)", got.StartedEarlier)
	}
}

// TestReport_AnOpenEntryLeftByACorrectionCanNEVERBeClosed.
//
// 🔴 THIS IS THE SHARPER HALF OF THE TABLE ABOVE AND TWO INDEPENDENT AUDITS PRODUCED
// IT SEPARATELY. When a manager corrects a CHECK-IN, accumulate flushes the earlier
// one into an OpenEntry — and every later checkout then finds `open == nil` and becomes
// StartedEarlier++. So the entry in the "needs action" list is not merely uncorrected:
// it is UNCLOSEABLE. Each attempt writes one more permanent row (§4.3) and one more
// wrong sentence on the report ("a shift that began before this week" — which is wrong
// twice over: it began inside the week, and its hours are in no report at all).
//
// 🔴 AND THE PRODUCT'S ACTUAL SCENARIO IS FINE, WHICH IS WHY THIS IS A COUNTED LIMIT
// RATHER THAN A BROKEN FEATURE. Q18's case — a forgotten checkout — closes correctly:
// an unclosed `in` alone reports 0 worked / 1 open, and the manager's `out` makes it
// 8h / 0 open. The trap needs a manager to have typed a CHECK-IN wrongly first.
//
// NOTHING HERE ASKS FOR THE BEHAVIOUR TO CHANGE. accumulate's readings are each
// defended on their own terms and the engine is M6-07's shipped arithmetic; what this
// pins is the CONSEQUENCE, so that docs/adr/0011, internal/domain/manual's
// CorrectionsOnlyShorten and the confirmation screen cannot drift away from it.
func TestReport_AnOpenEntryLeftByACorrectionCanNEVERBeClosed(t *testing.T) {
	t.Parallel()
	zone := time.UTC
	who := uuid.New()
	period := WeekOf(Date{Year: 2026, Month: time.August, Day: 5}, zone)
	at := func(h, m int) time.Time { return time.Date(2026, time.August, 5, h, m, 0, 0, zone) }
	ev := func(d string, tm time.Time) reportEvent {
		return reportEvent{EmployeeID: who, Name: "Maria Borg", At: tm, Direction: d, Verdict: "ok", Manual: true}
	}
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, zone)

	// The mistake: the check-in was typed an hour late, and a checkout already exists.
	// Then the correcting `in` is added, and then three attempts to close what it left.
	steps := []struct {
		name           string
		evs            []reportEvent
		worked         time.Duration
		open           int
		startedEarlier int
	}{
		{"the wrong pair", []reportEvent{ev("in", at(10, 0)), ev("out", at(17, 0))},
			7 * time.Hour, 0, 0},
		{"+ the correcting check-in", []reportEvent{ev("in", at(9, 0)), ev("in", at(10, 0)), ev("out", at(17, 0))},
			7 * time.Hour, 1, 0},
		{"+ one attempt to close it", []reportEvent{ev("in", at(9, 0)), ev("in", at(10, 0)), ev("out", at(17, 0)), ev("out", at(17, 0))},
			7 * time.Hour, 1, 1},
		{"+ a second attempt", []reportEvent{ev("in", at(9, 0)), ev("in", at(10, 0)), ev("out", at(17, 0)), ev("out", at(17, 0)), ev("out", at(17, 30))},
			7 * time.Hour, 1, 2},
		{"+ a third attempt", []reportEvent{ev("in", at(9, 0)), ev("in", at(10, 0)), ev("out", at(17, 0)), ev("out", at(17, 0)), ev("out", at(17, 30)), ev("out", at(18, 0))},
			7 * time.Hour, 1, 3},
	}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			got := accumulate(s.evs, period, zone, now)
			if got.Totals.Worked != s.worked {
				t.Errorf("worked = %v, want %v", got.Totals.Worked, s.worked)
			}
			if got.Totals.Open != s.open {
				t.Errorf("open = %d, want %d — the entry a manager is trying to close",
					got.Totals.Open, s.open)
			}
			if got.StartedEarlier != s.startedEarlier {
				t.Errorf("startedEarlier = %d, want %d — every closing attempt lands here "+
					"instead, as one more permanent row the screen describes wrongly",
					got.StartedEarlier, s.startedEarlier)
			}
		})
	}

	// 🔴 THE POSITIVE CONTROL, AND IT IS THE PRODUCT'S OWN SCENARIO. Without it this
	// test reads as "manual entry does not work", which is false: Q18's forgotten
	// checkout closes exactly as designed.
	t.Run("Q18's own case still closes", func(t *testing.T) {
		open := accumulate([]reportEvent{ev("in", at(9, 0))}, period, zone, now)
		if open.Totals.Worked != 0 || open.Totals.Open != 1 {
			t.Fatalf("an unclosed check-in reports worked=%v open=%d, want 0 and 1",
				open.Totals.Worked, open.Totals.Open)
		}
		closed := accumulate([]reportEvent{ev("in", at(9, 0)), ev("out", at(17, 0))}, period, zone, now)
		if closed.Totals.Worked != 8*time.Hour || closed.Totals.Open != 0 {
			t.Fatalf("after the manager's checkout: worked=%v open=%d, want 8h and 0. This is "+
				"the whole reason M6-08 exists (Q18), and if it ever breaks the counted "+
				"limits above stop being limits and become a broken feature.",
				closed.Totals.Worked, closed.Totals.Open)
		}
	})
}

// TestPersonHours_ManualArrivalsCountsARRIVALSAndSaysSo.
//
// 🔴 THE OLD NAME WAS WRONG IN BOTH DIRECTIONS ON A PAYROLL COLUMN, and only a table
// shows it. `Manual` was rendered as "N shifts entered by a manager" on the screen and
// as `manager_entered_shifts` in the CSV, while the counter runs inside arrival() --
// once per attributed CHECK-IN, closed or not.
//
// Every row below is a TRUE statement about arrivals and a FALSE one about shifts,
// which is the whole argument for the rename. The arithmetic is untouched: arrival()
// is where lateness is judged and a check-in is exactly what it is judged on.
func TestPersonHours_ManualArrivalsCountsARRIVALSAndSaysSo(t *testing.T) {
	t.Parallel()
	zone := time.UTC
	who := uuid.New()
	period := WeekOf(Date{Year: 2026, Month: time.August, Day: 5}, zone)
	at := func(h int) time.Time { return time.Date(2026, time.August, 5, h, 0, 0, 0, zone) }
	ev := func(d string, h int, manual bool) reportEvent {
		return reportEvent{EmployeeID: who, Name: "Maria Borg", At: at(h), Direction: d,
			Verdict: "ok", Manual: manual}
	}
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, zone)

	cases := []struct {
		name           string
		evs            []reportEvent
		wantArrivals   int
		wantShifts     int
		wasCalledShift string
	}{
		{"one typed pair", []reportEvent{ev("in", 9, true), ev("out", 17, true)}, 1, 1,
			"correct by luck -- one arrival, one shift"},
		{"a typed check-in corrected by another", []reportEvent{ev("in", 9, true), ev("in", 10, true), ev("out", 17, true)}, 2, 1,
			"OVER-counted: 2 on a payroll column for one shift"},
		{"a tapped check-in with a TYPED checkout", []reportEvent{ev("in", 9, false), ev("out", 17, true)}, 0, 1,
			"UNDER-counted: 0 for a shift a manager typed half of -- and it is Q18's own case"},
		{"a typed check-in nobody closed", []reportEvent{ev("in", 9, true)}, 1, 0,
			"not a shift at all"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := accumulate(c.evs, period, zone, now)
			if len(got.People) != 1 {
				t.Fatalf("people = %d, want 1", len(got.People))
			}
			p := got.People[0]
			if p.ManualArrivals != c.wantArrivals {
				t.Errorf("ManualArrivals = %d, want %d", p.ManualArrivals, c.wantArrivals)
			}
			if p.Shifts != c.wantShifts {
				t.Errorf("Shifts = %d, want %d", p.Shifts, c.wantShifts)
			}
			if p.ManualArrivals != c.wantShifts {
				t.Logf("the two differ here, which is why the old name was wrong: %s", c.wasCalledShift)
			}
		})
	}

	// ANTI-VACUITY: at least one case must actually separate the two numbers, or this
	// whole table is measuring a coincidence.
	separates := 0
	for _, c := range cases {
		if c.wantArrivals != c.wantShifts {
			separates++
		}
	}
	if separates < 3 {
		t.Fatalf("only %d case(s) separate arrivals from shifts; the table is not "+
			"exercising the difference it exists for", separates)
	}
}
