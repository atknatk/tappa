package tap

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// This file is ADR 0032 §A.1 (direction within the business day) and §A.6 (the
// checkout classification) on the engine. Every instant is fixed; the wall clock is
// Europe/Malta's unless a case says otherwise (summer CEST = UTC+2 in July 2026).

// mt is a Malta wall clock in July 2026 (Monday is the 27th) as a UTC instant.
func mt(t *testing.T, day, hh, mm int) time.Time {
	t.Helper()
	malta, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	return time.Date(2026, time.July, day, hh, mm, 0, 0, malta).UTC()
}

// maltaCutoff is a resolved cutoff in Malta's zone.
func maltaCutoff(d time.Duration) *DayCutoff {
	return &DayCutoff{Cutoff: d, Timezone: "Europe/Malta"}
}

// openIn is an open, non-practice check-in at `at`.
func openIn(at time.Time) *Transaction {
	return &Transaction{ID: uuid.New(), OccurredAt: at, Direction: TypeIn}
}

// TestDecide_DirectionWithinTheBusinessDay is ADR 0032 §A.1's table on single taps:
// for each, the engine's direction, the check-in it left open (and why), the notes,
// and S. The rows that used to be `out` are the point (the "17 h" row side by side
// with and without a cutoff).
func TestDecide_DirectionWithinTheBusinessDay(t *testing.T) {
	t.Parallel()
	type want struct {
		dir      Type
		left     LeftOpenReason // "" = nothing left open
		dayStart *time.Time     // nil = no business day
	}
	at := func(v time.Time) *time.Time { return &v }
	tests := []struct {
		name   string
		cutoff *DayCutoff
		open   time.Time // zero = no open check-in
		now    time.Time
		want   want
	}{
		// "17 h": Monday 16:00 left open, Tuesday 09:00. The business day ended at
		// Tuesday 05:00, so with the cutoff the tap is an `in`; without one only the
		// 18 h rule applies and 17 h still closes.
		{"seventeen_hours_with_cutoff_is_in", maltaCutoff(5 * time.Hour), mt(t, 27, 16, 0), mt(t, 28, 9, 0),
			want{TypeIn, LeftOpenPreviousBusinessDay, at(mt(t, 28, 5, 0))}},
		{"seventeen_hours_without_cutoff_is_out", nil, mt(t, 27, 16, 0), mt(t, 28, 9, 0),
			want{TypeOut, "", nil}},
		// Rusty Bar 18:05 -> 02:10: one business day (Monday 05:00), closes.
		{"rusty_bar_0210_closes_the_evening", maltaCutoff(5 * time.Hour), mt(t, 27, 18, 5), mt(t, 28, 2, 10),
			want{TypeOut, "", at(mt(t, 27, 5, 0))}},
		// The factory night shift 22:00 -> 06:05: 05:00 cuts it (06:05 is Tuesday's
		// business day), the department's 14:00 does not.
		{"factory_night_0500_cuts_the_shift", maltaCutoff(5 * time.Hour), mt(t, 27, 22, 0), mt(t, 28, 6, 5),
			want{TypeIn, LeftOpenPreviousBusinessDay, at(mt(t, 28, 5, 0))}},
		{"factory_night_1400_closes", maltaCutoff(14 * time.Hour), mt(t, 27, 22, 0), mt(t, 28, 6, 5),
			want{TypeOut, "", at(mt(t, 27, 14, 0))}},
		// Boundaries of the S row: an `in` AT the business day's start is that day's.
		{"open_at_the_start_closes", maltaCutoff(5 * time.Hour), mt(t, 28, 5, 0), mt(t, 28, 10, 0),
			want{TypeOut, "", at(mt(t, 28, 5, 0))}},
		{"open_one_second_before_the_start_is_left", maltaCutoff(5 * time.Hour), mt(t, 28, 5, 0).Add(-time.Second), mt(t, 28, 10, 0),
			want{TypeIn, LeftOpenPreviousBusinessDay, at(mt(t, 28, 5, 0))}},
		// Boundaries of the 18 h row, with no cutoff ("> 18 h is stale").
		{"exactly_18h_closes", nil, mt(t, 28, 10, 0).Add(-StaleOpenIn), mt(t, 28, 10, 0),
			want{TypeOut, "", nil}},
		{"18h_and_a_second_is_left", nil, mt(t, 28, 10, 0).Add(-StaleOpenIn - time.Second), mt(t, 28, 10, 0),
			want{TypeIn, LeftOpenStale, nil}},
		// The 18 h rule inside one business day: a 00:00 cutoff, an entry at 01:00
		// and a tap at 23:30 the same day — 22.5 h, same business day, still left.
		{"stale_within_one_business_day_is_left", maltaCutoff(0), mt(t, 28, 1, 0), mt(t, 28, 23, 30),
			want{TypeIn, LeftOpenStale, at(mt(t, 28, 0, 0))}},
		// Both reasons: the previous business day is named (a ∧ b → b).
		{"both_reasons_names_the_business_day", maltaCutoff(5 * time.Hour), mt(t, 27, 15, 0), mt(t, 28, 10, 0),
			want{TypeIn, LeftOpenPreviousBusinessDay, at(mt(t, 28, 5, 0))}},
		// No open check-in: an `in` with its business day and nothing left open.
		{"no_open_check_in", maltaCutoff(5 * time.Hour), time.Time{}, mt(t, 28, 9, 0),
			want{TypeIn, "", at(mt(t, 28, 5, 0))}},
		// An unresolvable cutoff is no business day: the 17 h row closes again.
		{"unresolvable_zone_is_the_18h_rule_alone", &DayCutoff{Cutoff: 5 * time.Hour, Timezone: "Nowhere/Atlantis"},
			mt(t, 27, 16, 0), mt(t, 28, 9, 0), want{TypeOut, "", nil}},
		{"empty_zone_is_the_18h_rule_alone", &DayCutoff{Cutoff: 5 * time.Hour},
			mt(t, 27, 16, 0), mt(t, 28, 9, 0), want{TypeOut, "", nil}},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := onSiteInput()
			in.Now = tc.now
			in.DayCutoff = tc.cutoff
			if !tc.open.IsZero() {
				in.LastOpenIn = openIn(tc.open)
			}
			got := Decide(in)
			if got.Verdict != VerdictOK {
				t.Fatalf("precondition: want ok, got %q via %q", got.Verdict, got.MatchedSid)
			}
			if got.Type == nil || *got.Type != tc.want.dir {
				t.Fatalf("Type = %v, want %q", got.Type, tc.want.dir)
			}
			assertLeftOpen(t, got, in.LastOpenIn, tc.want.left)
			switch {
			case tc.want.dayStart == nil && got.BusinessDayStart != nil:
				t.Errorf("BusinessDayStart = %s, want nil", got.BusinessDayStart)
			case tc.want.dayStart != nil && (got.BusinessDayStart == nil || !got.BusinessDayStart.Equal(*tc.want.dayStart)):
				t.Errorf("BusinessDayStart = %v, want %s", got.BusinessDayStart, tc.want.dayStart)
			}
		})
	}
}

// assertLeftOpen checks LeftOpen and the two notes against the expected reason.
func assertLeftOpen(t *testing.T, got Decision, open *Transaction, reason LeftOpenReason) {
	t.Helper()
	rule := "network proof of place: the source IP matches the location"
	if reason == "" {
		if got.LeftOpen != nil {
			t.Errorf("LeftOpen = %+v, want nil", got.LeftOpen)
		}
		if got.Note != rule || got.EmployeeNote != rule {
			t.Errorf("Note %q / EmployeeNote %q, want the rule's sentence alone", got.Note, got.EmployeeNote)
		}
		return
	}
	if got.LeftOpen == nil || got.LeftOpen.ID != open.ID || got.LeftOpen.Reason != reason {
		t.Errorf("LeftOpen = %+v, want {%s %s}", got.LeftOpen, open.ID, reason)
	}
	if want := rule + "; " + LeftOpenNote; got.Note != want {
		t.Errorf("Note = %q, want %q", got.Note, want)
	}
	if want := rule + "; " + LeftOpenEmployeeNote; got.EmployeeNote != want {
		t.Errorf("EmployeeNote = %q, want %q", got.EmployeeNote, want)
	}
	if strings.Contains(got.Note, staleOpenInNote) {
		t.Errorf("Note %q carries the historic stale note, which nothing writes any more", got.Note)
	}
}

// TestDecide_DirectionTheUsersWeekNoLongerInverts is the measured defect (ADR 0032
// "Ölçülen kusur") as a walk: Monday 09:12 in, Monday's 17:30 checkout forgotten,
// Tuesday 09:05, Tuesday 17:40, Wednesday 09:00. The old engine answered in, out,
// in, out — every day after the slip inverted. Now the slip stays in one day: the
// test plays GetLastOpenTransaction (the last record, if it is an `in`, is open) and
// runs the walk with the 05:00 cutoff and with none — the 18 h rule alone already
// stops it.
func TestDecide_DirectionTheUsersWeekNoLongerInverts(t *testing.T) {
	t.Parallel()
	walk := []struct {
		at   time.Time
		want Type
		left bool
	}{
		{mt(t, 27, 9, 12), TypeIn, false},
		{mt(t, 28, 9, 5), TypeIn, true}, // Monday's entry is left open: "checkout missing"
		{mt(t, 28, 17, 40), TypeOut, false},
		{mt(t, 29, 9, 0), TypeIn, false},
	}
	for _, cutoff := range []*DayCutoff{maltaCutoff(5 * time.Hour), nil} {
		cutoff := cutoff
		t.Run(fmt.Sprintf("cutoff_%v", cutoff != nil), func(t *testing.T) {
			t.Parallel()
			var last *Transaction
			for i, step := range walk {
				in := onSiteInput()
				in.Now = step.at
				in.DayCutoff = cutoff
				if last != nil && last.Direction == TypeIn {
					in.LastOpenIn = last
				}
				got := Decide(in)
				if got.Type == nil || *got.Type != step.want {
					t.Fatalf("tap %d at %s: Type = %v, want %q", i, step.at, got.Type, step.want)
				}
				if (got.LeftOpen != nil) != step.left {
					t.Fatalf("tap %d at %s: LeftOpen = %+v, want left=%v", i, step.at, got.LeftOpen, step.left)
				}
				last = &Transaction{ID: uuid.New(), OccurredAt: step.at, Direction: *got.Type}
			}
		})
	}
}

// TestDecide_DirectionReadsTheDeclaredInstant is ADR 0032 §A.4: the business day and
// the 18 h rule are judged at the tap's DECLARED instant when the client stated one
// (the M9-01 queue's shape). Monday 17:00's checkout was queued and arrives Tuesday
// 09:01; against the declared 17:00 it closes Monday 09:00 (the record is flagged by
// base:queued-window — still directed). Read at the server's Tuesday 09:01 it would
// have been an `in` — the same tap undeclared shows it. Without a cutoff the 18 h
// rule alone tells them apart (8 h vs 24 h), which is the half that used to read
// in.Now.
func TestDecide_DirectionReadsTheDeclaredInstant(t *testing.T) {
	t.Parallel()
	open := openIn(mt(t, 27, 9, 0))
	for _, cutoff := range []*DayCutoff{maltaCutoff(5 * time.Hour), nil} {
		cutoff := cutoff
		t.Run(fmt.Sprintf("cutoff_%v", cutoff != nil), func(t *testing.T) {
			t.Parallel()
			declared := onSiteInput()
			declared.Now = mt(t, 28, 9, 1)
			declared.OccurredAt = mt(t, 27, 17, 0)
			declared.OccurredAtFromClient = true
			declared.DayCutoff = cutoff
			declared.LastOpenIn = open
			got := Decide(declared)
			if got.Verdict != VerdictFlag || got.Type == nil || *got.Type != TypeOut || got.LeftOpen != nil {
				t.Fatalf("declared Monday 17:00: %q/%v left %+v via %q, want flag/out, nothing left open",
					got.Verdict, got.Type, got.LeftOpen, got.MatchedSid)
			}
			if cutoff != nil && (got.BusinessDayStart == nil || !got.BusinessDayStart.Equal(mt(t, 27, 5, 0))) {
				t.Errorf("BusinessDayStart = %v, want Monday 05:00 (the declared instant's day)", got.BusinessDayStart)
			}

			live := declared
			live.OccurredAt, live.OccurredAtFromClient = time.Time{}, false
			got = Decide(live)
			if got.Type == nil || *got.Type != TypeIn || got.LeftOpen == nil {
				t.Fatalf("undeclared Tuesday 09:01: Type %v, LeftOpen %+v; want in, Monday left open", got.Type, got.LeftOpen)
			}
		})
	}
}

// TestDecide_DirectionAnOutOfOrderClaimIsAnIn: a declared instant BEFORE the open
// check-in (ADR 0032 §A.1 row 2) cannot close it — it is an `in`, nothing is left
// open and no note is written (the entry is not stale, it is in the tap's future).
func TestDecide_DirectionAnOutOfOrderClaimIsAnIn(t *testing.T) {
	t.Parallel()
	for _, cutoff := range []*DayCutoff{maltaCutoff(5 * time.Hour), nil} {
		in := onSiteInput()
		in.Now = mt(t, 28, 11, 1)
		in.OccurredAt = mt(t, 28, 11, 0)
		in.OccurredAtFromClient = true
		in.DayCutoff = cutoff
		in.LastOpenIn = openIn(mt(t, 28, 12, 0))
		got := Decide(in)
		if got.Verdict != VerdictOK {
			t.Fatalf("precondition: want ok, got %q via %q", got.Verdict, got.MatchedSid)
		}
		if got.Type == nil || *got.Type != TypeIn {
			t.Fatalf("cutoff %v: Type = %v, want in", cutoff != nil, got.Type)
		}
		assertLeftOpen(t, got, in.LastOpenIn, "")
	}
}

// TestDecide_DirectionBusinessDayOnlyOnDirectedDecisions: reject, ignored and the
// redirect carry no direction, so no business day and nothing left open either —
// even with a cutoff and a stale open check-in in the Input. A practice open
// check-in is no open check-in: an `in` with its business day, nothing left open.
func TestDecide_DirectionBusinessDayOnlyOnDirectedDecisions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(in *Input)
	}{
		{"reject_lost_tag", func(in *Input) { in.Tag.Status = TagLost }},
		{"reject_deactivated", func(in *Input) { in.Employee.Status = EmployeeDeactivated }},
		{"ignored_debounce", func(in *Input) {
			in.LastForPerson = &Transaction{OccurredAt: in.Now.Add(-10 * time.Second), Direction: TypeIn}
		}},
		{"redirect_no_session", func(in *Input) { in.Employee = nil }},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			in := onSiteInput()
			in.Now = mt(t, 28, 9, 0)
			in.DayCutoff = maltaCutoff(5 * time.Hour)
			in.LastOpenIn = openIn(mt(t, 27, 9, 0))
			c.mutate(&in)
			got := Decide(in)
			if got.Type != nil || got.BusinessDayStart != nil || got.LeftOpen != nil {
				t.Fatalf("Type %v, BusinessDayStart %v, LeftOpen %+v; want all nil", got.Type, got.BusinessDayStart, got.LeftOpen)
			}
		})
	}
	t.Run("practice_open_in", func(t *testing.T) {
		t.Parallel()
		in := onSiteInput()
		in.Now = mt(t, 28, 9, 0)
		in.DayCutoff = maltaCutoff(5 * time.Hour)
		in.LastOpenIn = &Transaction{ID: uuid.New(), OccurredAt: mt(t, 27, 9, 0), Direction: TypeIn, Practice: true}
		got := Decide(in)
		if got.Type == nil || *got.Type != TypeIn || got.LeftOpen != nil {
			t.Fatalf("Type %v, LeftOpen %+v; want in, nothing left open", got.Type, got.LeftOpen)
		}
		if got.BusinessDayStart == nil || !got.BusinessDayStart.Equal(mt(t, 28, 5, 0)) {
			t.Errorf("BusinessDayStart = %v, want Tuesday 05:00", got.BusinessDayStart)
		}
	})
}

// TestDecide_ClassifyCheckoutEveryClassAndBound is ADR 0032 §A.6's six classes cell
// by cell, with the bounds, in the order migration 00064's tappa_checkout_class
// decides them — M15-05's parity test runs the same shapes through both. S is a
// Malta Tuesday 05:00; the out is at 10:00.
func TestDecide_ClassifyCheckoutEveryClassAndBound(t *testing.T) {
	t.Parallel()
	s := mt(t, 28, 5, 0)
	out := mt(t, 28, 10, 0)
	in := func(at time.Time) *Prev { return &Prev{Direction: TypeIn, At: at} }
	prevOut := func(writtenLater bool) *Prev {
		return &Prev{Direction: TypeOut, At: mt(t, 28, 9, 0), WrittenLater: writtenLater}
	}
	tests := []struct {
		name     string
		prev     *Prev
		dayHasIn bool
		outAt    time.Time
		dayStart *time.Time
		want     CheckoutClass
	}{
		// No `in` before it.
		{"no_prev_no_day_is_unanchored_d", nil, false, out, nil, CheckoutUnanchored},
		{"no_prev_with_day_is_orphan_c", nil, false, out, &s, CheckoutOrphan},
		{"no_prev_with_day_and_I_is_orphan_c", nil, true, out, &s, CheckoutOrphan},
		{"prev_out_no_day_is_unanchored_d_even_with_I_and_W", prevOut(true), true, out, nil, CheckoutUnanchored},
		{"prev_out_I_and_W_is_extra_e", prevOut(true), true, out, &s, CheckoutExtra},
		{"prev_out_I_not_W_is_orphan_c_the_forgotten_return", prevOut(false), true, out, &s, CheckoutOrphan},
		{"prev_out_W_not_I_is_orphan_c_the_claim_shape", prevOut(true), false, out, &s, CheckoutOrphan},
		{"prev_out_neither_is_orphan_c", prevOut(false), false, out, &s, CheckoutOrphan},
		{"prev_without_direction_leans_to_orphan_c", &Prev{At: mt(t, 28, 9, 0), WrittenLater: true}, true, out, &s, CheckoutOrphan},
		{"prev_without_direction_no_day_is_unanchored_d", &Prev{At: mt(t, 28, 9, 0)}, true, out, nil, CheckoutUnanchored},
		// An `in` before it.
		{"in_same_day_closes", in(mt(t, 28, 9, 0)), true, out, &s, CheckoutCloses},
		{"in_at_the_start_closes", in(s), true, out, &s, CheckoutCloses},
		{"in_a_microsecond_before_the_start_is_other_day_b", in(s.Add(-time.Microsecond)), false, out, &s, CheckoutOtherDay},
		{"in_no_day_closes", in(mt(t, 28, 9, 0)), false, out, nil, CheckoutCloses},
		{"in_exactly_18h_no_day_closes", in(out.Add(-StaleOpenIn)), false, out, nil, CheckoutCloses},
		{"in_18h_and_a_microsecond_no_day_is_too_long_a", in(out.Add(-StaleOpenIn - time.Microsecond)), false, out, nil, CheckoutTooLong},
		{"in_18h_and_a_second_no_day_is_too_long_a", in(out.Add(-StaleOpenIn - time.Second)), false, out, nil, CheckoutTooLong},
		{"in_exactly_18h_inside_a_midnight_day_closes", in(mt(t, 28, 23, 30).Add(-StaleOpenIn)), true, mt(t, 28, 23, 30), ptr(mt(t, 28, 0, 0)), CheckoutCloses},
		{"in_over_18h_inside_one_day_is_too_long_a", in(mt(t, 28, 1, 0)), true, mt(t, 28, 23, 30), ptr(mt(t, 28, 0, 0)), CheckoutTooLong},
		{"a_and_b_is_other_day_b", in(mt(t, 27, 15, 0)), false, out, &s, CheckoutOtherDay},
		{"in_ignores_I_and_W", &Prev{Direction: TypeIn, At: mt(t, 28, 9, 0), WrittenLater: true}, true, out, &s, CheckoutCloses},
		// No instant: no class (the SQL's NULL).
		{"no_instant_is_no_class", in(mt(t, 28, 9, 0)), true, time.Time{}, &s, ""},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ClassifyCheckout(tc.prev, tc.dayHasIn, tc.outAt, tc.dayStart); got != tc.want {
				t.Fatalf("ClassifyCheckout = %q, want %q", got, tc.want)
			}
		})
	}
	// The vocabulary is the SQL function's, verbatim.
	sql := []CheckoutClass{"closes", "too_long", "other_day", "orphan", "extra", "unanchored"}
	ours := []CheckoutClass{CheckoutCloses, CheckoutTooLong, CheckoutOtherDay, CheckoutOrphan, CheckoutExtra, CheckoutUnanchored}
	for i := range sql {
		if sql[i] != ours[i] {
			t.Errorf("class %d is %q, tappa_checkout_class says %q", i, ours[i], sql[i])
		}
	}
}

func ptr(t time.Time) *time.Time { return &t }

// TestDecide_TheEnginesOutIsAlwaysACheckoutThatCloses is the property tying the two
// questions together (ADR 0032 §A.6, card step 4): the engine answers "should this
// tap close the open check-in", the classification "did this out close the in
// before it". Read back with the open check-in as its predecessor and the decision's
// business day as its own, every `out` the engine produces classifies as closes, and
// every `in` it produces over an open check-in classifies as exactly the reason it
// gave — previous business day -> other_day (b), stale -> too_long (a). Swept over
// Malta's two DST weeks, Santiago's midnight gap and Lord Howe's 30-minute step, a
// set of cutoffs (none included), and open check-ins from 0 to 30 h old around the
// 18 h bound.
func TestDecide_TheEnginesOutIsAlwaysACheckoutThatCloses(t *testing.T) {
	t.Parallel()
	windows := []struct {
		zone string
		from time.Time
	}{
		{"Europe/Malta", time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)},
		{"Europe/Malta", time.Date(2027, 3, 26, 0, 0, 0, 0, time.UTC)},
		{"America/Santiago", time.Date(2025, 9, 5, 0, 0, 0, 0, time.UTC)},
		{"Australia/Lord_Howe", time.Date(2025, 4, 4, 0, 0, 0, 0, time.UTC)},
	}
	cutoffs := []time.Duration{0, 2 * time.Hour, 2*time.Hour + 30*time.Minute, 5 * time.Hour, 14 * time.Hour, 23*time.Hour + 45*time.Minute}
	ages := []time.Duration{0, time.Minute, time.Hour, 8 * time.Hour, 17*time.Hour + 59*time.Minute,
		StaleOpenIn, StaleOpenIn + time.Second, 20 * time.Hour, 30 * time.Hour}
	for _, w := range windows {
		w := w
		t.Run(w.zone+"_"+w.from.Format("2006-01"), func(t *testing.T) {
			t.Parallel()
			checked := map[CheckoutClass]int{}
			for at := w.from; at.Before(w.from.Add(5 * 24 * time.Hour)); at = at.Add(20 * time.Minute) {
				for ci := -1; ci < len(cutoffs); ci++ {
					var cutoff *DayCutoff
					if ci >= 0 {
						cutoff = &DayCutoff{Cutoff: cutoffs[ci], Timezone: w.zone}
					}
					for _, age := range ages {
						in := onSiteInput()
						in.Now = at
						in.DayCutoff = cutoff
						in.LastOpenIn = openIn(at.Add(-age))
						got := Decide(in)
						if got.Type == nil {
							t.Fatalf("no direction at %s", at)
						}
						class := ClassifyCheckout(&Prev{Direction: TypeIn, At: at.Add(-age)}, true, at, got.BusinessDayStart)
						want := CheckoutCloses
						if *got.Type == TypeIn {
							switch {
							case got.LeftOpen == nil:
								t.Fatalf("an `in` over an open check-in left nothing open (at %s, age %v)", at, age)
							case got.LeftOpen.Reason == LeftOpenPreviousBusinessDay:
								want = CheckoutOtherDay
							default:
								want = CheckoutTooLong
							}
						}
						if class != want {
							t.Fatalf("at %s, cutoff %v, age %v: engine %q (left %+v), class %q, want %q",
								at, cutoff, age, *got.Type, got.LeftOpen, class, want)
						}
						checked[class]++
					}
				}
			}
			for _, c := range []CheckoutClass{CheckoutCloses, CheckoutOtherDay, CheckoutTooLong} {
				if checked[c] == 0 {
					t.Errorf("the sweep never produced %q: it does not exercise that row", c)
				}
			}
		})
	}
}
