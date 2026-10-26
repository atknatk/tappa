package claim

// chain_test.go -- the pure half, one table per line of ADR 0032 §C.1–§C.4. Fixed weeks in
// Europe/Malta (Mon 2026-10-19 … Fri 2026-10-23, CEST, before the 25 October change), a 05:00 cutoff
// (the business default), so every instant is a wall clock a reader can check.

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/manual"
	"github.com/atknatk/tappa/internal/domain/tap"
)

var malta = func() *time.Location {
	l, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		panic(err)
	}
	return l
}()

// cutoff05 is the person's resolved cutoff: 05:00, Malta.
var cutoff05 = &tap.DayCutoff{Cutoff: 5 * time.Hour, Timezone: "Europe/Malta"}

// wall is day (October 2026) hh:mm:ss in Malta, as UTC.
func wall(day, hh, mm, ss int) time.Time {
	return time.Date(2026, time.October, day, hh, mm, ss, 0, malta).UTC()
}

// id is a deterministic record id: n in the last byte, so the chain's tie order is n's.
func id(n byte) uuid.UUID { return uuid.UUID{15: n} }

// tapEv is a tap (nfc) with its frozen business day under cutoff05, written at its own instant.
func tapEv(n byte, dir tap.Type, at time.Time) Event {
	s, ok := tap.BusinessDayStart(at, *cutoff05)
	if !ok {
		panic("no business day")
	}
	return Event{ID: id(n), Direction: dir, At: at, WrittenAt: at, DayStart: &s, Channel: tap.ChannelNFC}
}

// manualEv is a manager's record: no business day, written at `written`.
func manualEv(n byte, dir tap.Type, at, written time.Time) Event {
	return Event{ID: id(n), Direction: dir, At: at, WrittenAt: written, Channel: tap.ChannelManual}
}

func chainOf(events ...Event) Chain { return NewChain(events, true) }

var farNow = wall(23, 12, 0, 0) // Fri noon: "now" never clips a window unless a case says so

func shift(start, end time.Duration, overnight bool) *tap.Shift {
	return &tap.Shift{Start: start, End: end, Overnight: overnight, Timezone: "Europe/Malta"}
}

// TestWindow_EveryEndIsWhereADR0032PutsIt is §C.3's two windows, one inside/outside pair per end, at
// the end and one second past it (the card's G-DÜŞÜK: each end moved a unit, or inclusive and
// exclusive swapped, turns a row red). The anchor is the chain's record `anchor`.
func TestWindow_EveryEndIsWhereADR0032PutsIt(t *testing.T) {
	t.Parallel()
	// out claims: the anchor an open in
	early := chainOf(tapEv(1, tap.TypeIn, wall(19, 5, 30, 0)))                                           // A Mon 05:30: A+18h = Mon 23:30 < Next(S) Tue 05:00
	rusty := chainOf(tapEv(1, tap.TypeIn, wall(19, 18, 5, 0)))                                           // A Mon 18:05: Next(S) = Tue 05:00 < A+18h
	split := chainOf(tapEv(1, tap.TypeIn, wall(19, 11, 0, 0)), tapEv(2, tap.TypeIn, wall(19, 18, 0, 0))) // next = Mon 18:00
	fresh := chainOf(tapEv(1, tap.TypeIn, wall(19, 9, 0, 0)))                                            // now = Mon 12:00 clips
	// in claims: the anchor an orphan out
	orphan := chainOf(tapEv(1, tap.TypeOut, wall(20, 9, 0, 0))) // X Tue 09:00 (c): [S Tue 05:00, 09:00)
	late := chainOf(tapEv(1, tap.TypeOut, wall(20, 23, 30, 0))) // X Tue 23:30: X−18h = Tue 05:30 > S
	afterPair := chainOf(tapEv(1, tap.TypeIn, wall(20, 9, 0, 0)), tapEv(2, tap.TypeOut, wall(20, 12, 0, 0)),
		tapEv(3, tap.TypeOut, wall(20, 17, 0, 0))) // X Tue 17:00 (c) after a closed pair: prev = 12:00
	otherDay := chainOf(tapEv(1, tap.TypeIn, wall(19, 15, 0, 0)), tapEv(2, tap.TypeOut, wall(20, 7, 0, 0))) // X (b)
	tests := []struct {
		name   string
		chain  Chain
		anchor int
		now    time.Time
		at     time.Time
		inside bool
	}{
		{"out: the anchor's own instant is excluded", early, 0, farNow, wall(19, 5, 30, 0), false},
		{"out: a second after the anchor is inside", early, 0, farNow, wall(19, 5, 30, 1), true},
		{"out: in.At + 18 h is included", early, 0, farNow, wall(19, 23, 30, 0), true},
		{"out: in.At + 18 h + 1 s is outside", early, 0, farNow, wall(19, 23, 30, 1), false},
		{"out: Rusty Bar's 02:00 is inside the night's business day", rusty, 0, farNow, wall(20, 2, 0, 0), true},
		{"out: a second before Next(in.S) is inside", rusty, 0, farNow, wall(20, 4, 59, 59), true},
		{"out: Next(in.S) itself is the new day's, outside", rusty, 0, farNow, wall(20, 5, 0, 0), false},
		{"out: a second before the next record is inside", split, 0, farNow, wall(19, 17, 59, 59), true},
		{"out: the next record's instant is excluded", split, 0, farNow, wall(19, 18, 0, 0), false},
		{"out: now is included", fresh, 0, wall(19, 12, 0, 0), wall(19, 12, 0, 0), true},
		{"out: a second after now is outside", fresh, 0, wall(19, 12, 0, 0), wall(19, 12, 0, 1), false},
		{"in: the out's business day start is included", orphan, 0, farNow, wall(20, 5, 0, 0), true},
		{"in: a second before the business day is outside", orphan, 0, farNow, wall(20, 4, 59, 59), false},
		{"in: a second before the out is inside", orphan, 0, farNow, wall(20, 8, 59, 59), true},
		{"in: the out's own instant is excluded", orphan, 0, farNow, wall(20, 9, 0, 0), false},
		{"in: out.At − 18 h is included", late, 0, farNow, wall(20, 5, 30, 0), true},
		{"in: a second before out.At − 18 h is outside", late, 0, farNow, wall(20, 5, 29, 59), false},
		{"in: the previous record's instant is excluded", afterPair, 2, farNow, wall(20, 12, 0, 0), false},
		{"in: a second after the previous record is inside", afterPair, 2, farNow, wall(20, 12, 0, 1), true},
		{"in (b): the out's day start, not the earlier day's in, bounds it", otherDay, 1, farNow, wall(20, 5, 0, 0), true},
		{"in (b): the earlier day is outside", otherDay, 1, farNow, wall(20, 4, 59, 59), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, ok := tc.chain.WindowOf(tc.anchor, cutoff05, tc.now)
			if !ok {
				t.Fatal("no window")
			}
			if got := w.Contains(tc.at); got != tc.inside {
				t.Errorf("Contains(%s) = %v, want %v (window %+v)", tc.at.In(malta).Format(time.DateTime), got, tc.inside, w)
			}
		})
	}
}

// TestWindow_NoWindowWithoutACutoffOrAnAnchor: no resolvable cutoff, no Next(in.S) -- no window is
// invented; a record that misses nothing has none.
func TestWindow_NoWindowWithoutACutoffOrAnAnchor(t *testing.T) {
	t.Parallel()
	open := chainOf(tapEv(1, tap.TypeIn, wall(19, 9, 0, 0)))
	if _, ok := open.WindowOf(0, nil, farNow); ok {
		t.Error("an out claim without a cutoff got a window")
	}
	if _, ok := open.WindowOf(0, &tap.DayCutoff{Cutoff: 5 * time.Hour, Timezone: "Not/AZone"}, farNow); ok {
		t.Error("an out claim with an unloadable zone got a window")
	}
	closed := chainOf(tapEv(1, tap.TypeIn, wall(19, 9, 0, 0)), tapEv(2, tap.TypeOut, wall(19, 17, 0, 0)))
	for i := range 2 {
		if _, ok := closed.WindowOf(i, cutoff05, farNow); ok {
			t.Errorf("record %d of a closed pair got a window", i)
		}
	}
}

// TestMissing_IsTheReadersClassification is §C.3's "çapa hâlâ eksik mi -- §A.6'nın
// sınıflandırmasıyla": the open in and the (b)/(c) out are anchors; a closing pair, (a), (e), (d)
// and a record without a business day (Ü-Y2) are not.
func TestMissing_IsTheReadersClassification(t *testing.T) {
	t.Parallel()
	mon := func(h, m int) time.Time { return wall(19, h, m, 0) }
	pre := Event{ID: id(1), Direction: tap.TypeIn, At: mon(9, 0), WrittenAt: mon(9, 0), Channel: tap.ChannelNFC} // no business day
	tests := []struct {
		name  string
		chain Chain
		at    int
		want  Kind
	}{
		{"an in with nothing after it is open", chainOf(tapEv(1, tap.TypeIn, mon(9, 0))), 0, KindOut},
		{"an in its out closes is not", chainOf(tapEv(1, tap.TypeIn, mon(9, 0)), tapEv(2, tap.TypeOut, mon(17, 0))), 0, ""},
		{"the closing out is not", chainOf(tapEv(1, tap.TypeIn, mon(9, 0)), tapEv(2, tap.TypeOut, mon(17, 0))), 1, ""},
		{"an in followed by another in stays open", chainOf(tapEv(1, tap.TypeIn, mon(9, 0)), tapEv(2, tap.TypeIn, mon(18, 0))), 0, KindOut},
		{"an in whose next out is of another business day stays open (b)", chainOf(tapEv(1, tap.TypeIn, mon(15, 0)), tapEv(2, tap.TypeOut, wall(20, 7, 0, 0))), 0, KindOut},
		{"that (b) out is a check-in claim's anchor", chainOf(tapEv(1, tap.TypeIn, mon(15, 0)), tapEv(2, tap.TypeOut, wall(20, 7, 0, 0))), 1, KindIn},
		{"an out with nothing before it (c)", chainOf(tapEv(1, tap.TypeOut, mon(9, 0))), 0, KindIn},
		{"an in older than 18 h stays open; its out (a) is no anchor", chainOf(tapEv(1, tap.TypeIn, mon(5, 30)), tapEv(2, tap.TypeOut, wall(20, 4, 0, 0))), 1, ""},
		{"the correction's surplus out (e) is no anchor", chainOf(tapEv(1, tap.TypeIn, mon(9, 0)),
			manualEv(2, tap.TypeOut, mon(17, 0), mon(21, 0)), tapEv(3, tap.TypeOut, mon(20, 0))), 2, ""},
		{"a manual out (no business day, d) is no anchor", chainOf(manualEv(1, tap.TypeOut, mon(17, 0), mon(18, 0))), 0, ""},
		// I at its edge (R08): an in exactly at the out's business day start is of that day, so a
		// correction's surplus after it is (e), not an orphan
		{"an in exactly at the day's start counts for I", chainOf(tapEv(1, tap.TypeIn, mon(5, 0)),
			manualEv(2, tap.TypeOut, mon(12, 0), mon(20, 0)), tapEv(3, tap.TypeOut, mon(18, 0))), 2, ""},
		{"a second before the day's start does not", chainOf(tapEv(1, tap.TypeIn, wall(19, 4, 59, 59)),
			manualEv(2, tap.TypeOut, mon(12, 0), mon(20, 0)), tapEv(3, tap.TypeOut, mon(18, 0))), 2, KindIn},
		{"an in written before M15 (no business day) is no anchor", chainOf(pre), 0, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tc.chain.Missing(tc.at)
			if !ok || got != tc.want {
				t.Errorf("Missing = %q, %v; want %q", got, ok, tc.want)
			}
		})
	}
}

// TestMissing_SaysWhenTheSliceCannotTell: a chain read from a window whose first record's
// predecessor lies outside cannot classify that record's checkout -- it says so rather than guess.
func TestMissing_SaysWhenTheSliceCannotTell(t *testing.T) {
	t.Parallel()
	partial := NewChain([]Event{tapEv(1, tap.TypeOut, wall(20, 9, 0, 0))}, false)
	if _, ok := partial.Missing(0); ok {
		t.Error("a partial slice classified its first checkout")
	}
	if partial.Closes(0, wall(20, 8, 0, 0), cutoff05) {
		t.Error("a partial slice approved a pair on its first checkout")
	}
	// the record right before a window can still be an out claim's anchor: its next is known
	open := NewChain([]Event{tapEv(1, tap.TypeIn, wall(19, 9, 0, 0)), tapEv(2, tap.TypeIn, wall(20, 9, 0, 0))}, false)
	if k, ok := open.Missing(0); !ok || k != KindOut {
		t.Errorf("Missing(first in) = %q, %v; want out", k, ok)
	}
}

// TestCloses_RefusesEveryPairThatDoesNotClose is §C.3's second check, apart from the window: the
// approval's record added to the chain must make a pair the READER calls closing. Every instant the
// window refuses, Closes refuses too -- by its own classification -- so a window arithmetic error
// cannot become hours alone.
func TestCloses_RefusesEveryPairThatDoesNotClose(t *testing.T) {
	t.Parallel()
	early := chainOf(tapEv(1, tap.TypeIn, wall(19, 5, 30, 0)))
	rusty := chainOf(tapEv(1, tap.TypeIn, wall(19, 18, 5, 0)))
	split := chainOf(tapEv(1, tap.TypeIn, wall(19, 11, 0, 0)), tapEv(2, tap.TypeIn, wall(19, 18, 0, 0)))
	orphan := chainOf(tapEv(1, tap.TypeOut, wall(20, 9, 0, 0)))
	late := chainOf(tapEv(1, tap.TypeOut, wall(20, 23, 30, 0)))
	afterPair := chainOf(tapEv(1, tap.TypeIn, wall(20, 9, 0, 0)), tapEv(2, tap.TypeOut, wall(20, 12, 0, 0)),
		tapEv(3, tap.TypeOut, wall(20, 17, 0, 0)))
	tests := []struct {
		name   string
		chain  Chain
		anchor int
		at     time.Time
		want   bool
	}{
		{"out: 18 h closes", early, 0, wall(19, 23, 30, 0), true},
		{"out: 18 h + 1 s is too long", early, 0, wall(19, 23, 30, 1), false},
		{"out: before the anchor", early, 0, wall(19, 5, 29, 59), false},
		{"out: at the anchor's instant", early, 0, wall(19, 5, 30, 0), false},
		{"out: Rusty Bar 02:00 closes", rusty, 0, wall(20, 2, 0, 0), true},
		{"out: at Next(in.S) the anchor is of another day", rusty, 0, wall(20, 5, 0, 0), false},
		{"out: inside, before the next record", split, 0, wall(19, 17, 59, 59), true},
		{"out: at the next record's instant", split, 0, wall(19, 18, 0, 0), false},
		{"out: past the next record, its predecessor is not the anchor", split, 0, wall(19, 18, 0, 1), false},
		{"in: at the business day start closes", orphan, 0, wall(20, 5, 0, 0), true},
		{"in: before the business day", orphan, 0, wall(20, 4, 59, 59), false},
		{"in: at the out's instant", orphan, 0, wall(20, 9, 0, 0), false},
		{"in: out.At − 18 h closes", late, 0, wall(20, 5, 30, 0), true},
		{"in: out.At − 18 h − 1 s is too long", late, 0, wall(20, 5, 29, 59), false},
		{"in: at the previous record", afterPair, 2, wall(20, 12, 0, 0), false},
		{"in: before the previous record, the predecessor is not the new in", afterPair, 2, wall(20, 11, 59, 59), false},
		{"in: after the previous record closes", afterPair, 2, wall(20, 12, 0, 1), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.chain.Closes(tc.anchor, tc.at, cutoff05); got != tc.want {
				t.Errorf("Closes(%s) = %v, want %v", tc.at.In(malta).Format(time.DateTime), got, tc.want)
			}
		})
	}
	if early.Closes(0, wall(19, 12, 0, 0), nil) {
		t.Error("an out claim closed with no cutoff to judge its day by")
	}
}

// TestWindow_TheThirdEyesCaseAcceptsBothClaims is the third eye's case (§C.3; the card's criterion):
// Mon 15:00 in, Tue 07:00 the person's chosen out. Both halves are claimable -- Monday's checkout
// (the in is open: Tuesday's out is of another business day) and Tuesday's check-in (the out is (b))
// -- and approving Monday's at 23:00 is an 8-hour pair that closes. The hours themselves are the
// report's: TestDecideDB_TheThirdEyesCasePaysMondayEightHours.
func TestWindow_TheThirdEyesCaseAcceptsBothClaims(t *testing.T) {
	t.Parallel()
	c := chainOf(tapEv(1, tap.TypeIn, wall(19, 15, 0, 0)), tapEv(2, tap.TypeOut, wall(20, 7, 0, 0)))
	if k, _ := c.Missing(0); k != KindOut {
		t.Fatalf("Mon 15:00 misses %q, want out", k)
	}
	if k, _ := c.Missing(1); k != KindIn {
		t.Fatalf("Tue 07:00 misses %q, want in", k)
	}
	w0, _ := c.WindowOf(0, cutoff05, farNow)
	if !w0.Contains(wall(19, 23, 0, 0)) || !c.Closes(0, wall(19, 23, 0, 0), cutoff05) {
		t.Errorf("Monday's checkout claim at 23:00 is refused (window %+v)", w0)
	}
	if got := Assess(KindOut, wall(19, 15, 0, 0), wall(19, 23, 0, 0), nil, nil).Interval.Duration(); got != 8*time.Hour {
		t.Errorf("Monday's interval %v, want 8h", got)
	}
	w1, _ := c.WindowOf(1, cutoff05, farNow)
	if !w1.Contains(wall(20, 5, 30, 0)) || !c.Closes(1, wall(20, 5, 30, 0), cutoff05) {
		t.Errorf("Tuesday's check-in claim at 05:30 is refused (window %+v)", w1)
	}
}

// TestAssess_GY1TheBandAsksForConfirmation is the security lens's two cases (G-Y1): inside the window,
// outside the shift's band -- the decision asks the confirmation step, and the domain computes the
// interval, its length and the deviation the step repeats.
func TestAssess_GY1TheBandAsksForConfirmation(t *testing.T) {
	t.Parallel()
	nineToFive := shift(9*time.Hour, 17*time.Hour, false)
	// (1) Ahmet's 09:00 "Not right? Clock out" -- an orphan out (c); the check-in claim 05:00
	ahmet := chainOf(tapEv(1, tap.TypeOut, wall(19, 9, 0, 0)))
	w, _ := ahmet.WindowOf(0, cutoff05, farNow)
	if !w.Contains(wall(19, 5, 0, 0)) {
		t.Fatalf("05:00 is outside the window %+v -- the case needs it inside", w)
	}
	a := Assess(KindIn, wall(19, 9, 0, 0), wall(19, 5, 0, 0), nineToFive, nil)
	if !a.RequiresConfirmation {
		t.Error("05:00 for a 09:00–17:00 shift approves in one click")
	}
	if a.Deviation != (Deviation{Side: BeforeStart, By: 4 * time.Hour}) {
		t.Errorf("deviation %+v, want 4 h before the shift starts", a.Deviation)
	}
	if a.Interval.In != wall(19, 5, 0, 0) || a.Interval.Out != wall(19, 9, 0, 0) || a.Interval.Duration() != 4*time.Hour {
		t.Errorf("interval %+v, want in 05:00 → out 09:00 = 4 h", a.Interval)
	}
	// (2) Mon 09:12 in; the checkout claim Tue 03:00 -- 17 h 48 m, inside 18 h
	mon := chainOf(tapEv(1, tap.TypeIn, wall(19, 9, 12, 0)))
	w, _ = mon.WindowOf(0, cutoff05, farNow)
	if !w.Contains(wall(20, 3, 0, 0)) {
		t.Fatalf("Tue 03:00 is outside the window %+v -- the case needs it inside", w)
	}
	a = Assess(KindOut, wall(19, 9, 12, 0), wall(20, 3, 0, 0), nineToFive, nil)
	if !a.RequiresConfirmation || a.Interval.Duration() != 17*time.Hour+48*time.Minute {
		t.Errorf("Tue 03:00: confirmation %v, interval %v; want true and 17h48m", a.RequiresConfirmation, a.Interval.Duration())
	}
	if a.Deviation != (Deviation{Side: AfterEnd, By: 10 * time.Hour}) {
		t.Errorf("deviation %+v, want 10 h after the shift ends", a.Deviation)
	}
}

// TestAssess_TheBandsEnds is §A.7's (start − 1 h, end + 2 h]: the lower end open, the upper closed;
// no shift asks the confirmation step too (§C.4: "vardiya yoksa").
func TestAssess_TheBandsEnds(t *testing.T) {
	t.Parallel()
	nineToFive := shift(9*time.Hour, 17*time.Hour, false)
	in := wall(19, 9, 12, 0)
	tests := []struct {
		name    string
		at      time.Time
		shift   *tap.Shift
		confirm bool
	}{
		{"start − 1 h is outside (open)", wall(19, 8, 0, 0), nineToFive, true},
		{"start − 1 h + 1 s is inside", wall(19, 8, 0, 1), nineToFive, false},
		{"end + 2 h is inside (closed)", wall(19, 19, 0, 0), nineToFive, false},
		{"end + 2 h + 1 s is outside", wall(19, 19, 0, 1), nineToFive, true},
		{"the shift's end is inside", wall(19, 17, 0, 0), nineToFive, false},
		{"no shift: always the confirmation step", wall(19, 17, 0, 0), nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Assess(KindOut, in, tc.at, tc.shift, nil).RequiresConfirmation; got != tc.confirm {
				t.Errorf("RequiresConfirmation = %v, want %v", got, tc.confirm)
			}
		})
	}
	if a := Assess(KindOut, in, wall(19, 17, 0, 0), nil, nil); a.Deviation.Side != NoShift || a.Shift != nil {
		t.Errorf("no shift: %+v", a)
	}
}

// TestAssess_TheManagersEditIsHeldToTheSameBand (ürün N8): the decision assesses the moment it would
// write -- an Edit outside the band asks the same step a claim there would. Decide calls Assess with
// the Edit's moment (TestDecideDB_AnEditOutsideTheBandAsksTheConfirmationStep is the end-to-end).
func TestAssess_TheManagersEditIsHeldToTheSameBand(t *testing.T) {
	t.Parallel()
	nineToFive := shift(9*time.Hour, 17*time.Hour, false)
	claimed, edited := wall(19, 17, 0, 0), wall(19, 22, 0, 0)
	if Assess(KindOut, wall(19, 9, 0, 0), claimed, nineToFive, &claimed).RequiresConfirmation {
		t.Fatal("the claimed 17:00 is outside the band -- the case needs it inside")
	}
	a := Assess(KindOut, wall(19, 9, 0, 0), edited, nineToFive, &claimed)
	if !a.RequiresConfirmation || a.DefaultUnchanged {
		t.Errorf("the Edit to 22:00: %+v; want the confirmation step and no 'default not changed'", a)
	}
}

// TestPrefill_IsTheShiftsRealEndOnlyInsideTheWindow is K3's two criteria (§C.2): (1) the split shift
// -- KF 10–22, 11:00 in, 18:00 "Not right? Clock in": 22:00 is outside (11:00, 18:00), no prefill,
// the field empty and required, "default not changed" never; (2) Mon 09:12 in, shift 09:00–17:00,
// asked the next day: 17:00 is inside, prefilled, and unchanged it is "default not changed" and in
// the band. No prefill is ever clipped to the window.
func TestPrefill_IsTheShiftsRealEndOnlyInsideTheWindow(t *testing.T) {
	t.Parallel()
	kf := shift(10*time.Hour, 22*time.Hour, false)
	split := chainOf(tapEv(1, tap.TypeIn, wall(19, 11, 0, 0)), tapEv(2, tap.TypeIn, wall(19, 18, 0, 0)))
	w, _ := split.WindowOf(0, cutoff05, farNow)
	if p := Prefill(KindOut, wall(19, 11, 0, 0), kf, w); p != nil {
		t.Errorf("split shift: prefilled %s, want none (22:00 is outside (11:00, 18:00); no clipping)", p.In(malta))
	}
	for _, at := range []time.Time{wall(19, 15, 0, 0), wall(19, 17, 59, 0)} {
		if Assess(KindOut, wall(19, 11, 0, 0), at, kf, nil).DefaultUnchanged {
			t.Errorf("split shift: %s reads 'default not changed' with no prefill", at.In(malta))
		}
	}

	nineToFive := shift(9*time.Hour, 17*time.Hour, false)
	mon := chainOf(tapEv(1, tap.TypeIn, wall(19, 9, 12, 0)))
	w, _ = mon.WindowOf(0, cutoff05, wall(20, 10, 0, 0))
	p := Prefill(KindOut, wall(19, 9, 12, 0), nineToFive, w)
	if p == nil || !p.Equal(wall(19, 17, 0, 0)) {
		t.Fatalf("Mon 09:12: prefilled %v, want 17:00", p)
	}
	a := Assess(KindOut, wall(19, 9, 12, 0), *p, nineToFive, p)
	if !a.DefaultUnchanged || a.RequiresConfirmation {
		t.Errorf("17:00 unchanged: %+v; want 'default not changed' inside the band", a)
	}
	if Assess(KindOut, wall(19, 9, 12, 0), wall(19, 17, 30, 0), nineToFive, p).DefaultUnchanged {
		t.Error("17:30 reads 'default not changed'")
	}
	// a check-in claim's prefill is the shift's real START, under the same rule
	ahmet := chainOf(tapEv(1, tap.TypeOut, wall(19, 17, 0, 0)))
	w, _ = ahmet.WindowOf(0, cutoff05, farNow)
	if p := Prefill(KindIn, wall(19, 17, 0, 0), nineToFive, w); p == nil || !p.Equal(wall(19, 9, 0, 0)) {
		t.Errorf("check-in claim: prefilled %v, want 09:00", p)
	}
	if p := Prefill(KindIn, wall(19, 17, 0, 0), nil, w); p != nil {
		t.Errorf("no shift: prefilled %v", p)
	}
}

// TestPrefill_TheRustyBarNightEndsTheNextMorning: an overnight shift's end is on the next date.
func TestPrefill_TheRustyBarNightEndsTheNextMorning(t *testing.T) {
	t.Parallel()
	rusty := shift(18*time.Hour, 2*time.Hour, true)
	c := chainOf(tapEv(1, tap.TypeIn, wall(19, 18, 5, 0)))
	w, _ := c.WindowOf(0, cutoff05, farNow)
	if p := Prefill(KindOut, wall(19, 18, 5, 0), rusty, w); p == nil || !p.Equal(wall(20, 2, 0, 0)) {
		t.Errorf("Rusty Bar: prefilled %v, want Tue 02:00", p)
	}
}

// TestShown_K4TheCheckoutClaimIsAskedAtTwoTaps is K4's checkout claim (§C.1): A open, T1 (in), T2
// (in, same day), T3 (out, same day), T4 (out) -- the card at T1 and T3, never at T2 or T4.
func TestShown_K4TheCheckoutClaimIsAskedAtTwoTaps(t *testing.T) {
	t.Parallel()
	c := chainOf(
		tapEv(1, tap.TypeIn, wall(19, 9, 0, 0)),   // A (Mon), left open
		tapEv(2, tap.TypeIn, wall(20, 9, 0, 0)),   // T1 (Tue): left A open -- previous business day
		tapEv(3, tap.TypeIn, wall(20, 13, 0, 0)),  // T2 (Tue, an in -- "Not right? Clock in")
		tapEv(4, tap.TypeOut, wall(20, 17, 0, 0)), // T3 (Tue): the first out after T1, T1's day
		tapEv(5, tap.TypeOut, wall(20, 18, 0, 0)), // T4 (Tue): not the first
	)
	want := map[int]bool{1: true, 2: false, 3: true, 4: false}
	for at, w := range want {
		if got := c.Shown(0, at); got != w {
			t.Errorf("Shown(A, T%d) = %v, want %v", at, got, w)
		}
	}
	// and the card itself. At T1, A's checkout claim; once sent, no card.
	if cand, ok := c.Offer(1, nil, cutoff05, farNow); !ok || cand.Anchor.ID != id(1) || cand.Kind != KindOut {
		t.Errorf("Offer(T1) = %+v, %v; want A's checkout claim", cand, ok)
	}
	if _, ok := c.Offer(1, func(x uuid.UUID) bool { return x == id(1) }, cutoff05, farNow); ok {
		t.Error("Offer(T1) after A's claim was sent still offers a card")
	}
	// At T3, T1 is an anchor too (T2's "Not right? Clock in" left it open) and the fresher one (N3);
	// with T1's claim sent, A's; with both sent, none.
	if cand, ok := c.Offer(3, nil, cutoff05, farNow); !ok || cand.Anchor.ID != id(2) {
		t.Errorf("Offer(T3) = %+v, %v; want T1's checkout claim (the fresher anchor)", cand, ok)
	}
	if cand, ok := c.Offer(3, func(x uuid.UUID) bool { return x == id(2) }, cutoff05, farNow); !ok || cand.Anchor.ID != id(1) {
		t.Errorf("Offer(T3), T1 claimed = %+v, %v; want A's checkout claim", cand, ok)
	}
	if _, ok := c.Offer(3, func(x uuid.UUID) bool { return x == id(1) || x == id(2) }, cutoff05, farNow); ok {
		t.Error("Offer(T3) with both claims sent still offers a card")
	}
	// T4 is past both showings of A and of T1; its own card is its own check-in claim (an out after an
	// out is an orphan, (c)), never A's
	if cand, ok := c.Offer(4, nil, cutoff05, farNow); !ok || cand.Anchor.ID != id(5) || cand.Kind != KindIn {
		t.Errorf("Offer(T4) = %+v, %v; want T4's own check-in claim", cand, ok)
	}
	if _, ok := c.Offer(4, func(x uuid.UUID) bool { return x == id(5) }, cutoff05, farNow); ok {
		t.Error("Offer(T4) with T4's claim sent offers another anchor's card")
	}
}

// TestShown_K4TheCheckInClaimIsAskedAtTwoTaps is K4's check-in claim: X (orphan out), Y (in), Z (out,
// X's day) -- the card at X and Z, not at Y (the third version's formula gave three).
func TestShown_K4TheCheckInClaimIsAskedAtTwoTaps(t *testing.T) {
	t.Parallel()
	c := chainOf(
		tapEv(1, tap.TypeOut, wall(19, 9, 0, 0)),  // X: chosen out, orphan (c)
		tapEv(2, tap.TypeIn, wall(19, 13, 0, 0)),  // Y
		tapEv(3, tap.TypeOut, wall(19, 17, 0, 0)), // Z: the first out after X, X's day
	)
	want := map[int]bool{0: true, 1: false, 2: true}
	for at, w := range want {
		if got := c.Shown(0, at); got != w {
			t.Errorf("Shown(X, %d) = %v, want %v", at, got, w)
		}
	}
	// a later day's first out is no showing
	next := chainOf(tapEv(1, tap.TypeOut, wall(19, 9, 0, 0)), tapEv(2, tap.TypeOut, wall(20, 17, 0, 0)))
	if next.Shown(0, 1) {
		t.Error("the next business day's out showed X's card")
	}
}

// TestShown_K4ADirectionlessTapIsNoShowing: an ignored or refused tap has no direction -- it is not in
// the chain, so it is never T1, never a showing and never counted (§C.1). A, then a refused tap R, then
// T1: T1 still shows A's card.
func TestShown_K4ADirectionlessTapIsNoShowing(t *testing.T) {
	t.Parallel()
	refused := Event{ID: id(2), At: wall(20, 8, 0, 0), WrittenAt: wall(20, 8, 0, 0), Channel: tap.ChannelNFC} // no direction
	c := chainOf(tapEv(1, tap.TypeIn, wall(19, 9, 0, 0)), refused, tapEv(3, tap.TypeIn, wall(20, 9, 0, 0)))
	if c.Index(id(2)) != -1 {
		t.Fatal("a directionless record is in the chain")
	}
	if !c.Shown(0, c.Index(id(3))) {
		t.Error("the refused tap took T1's showing")
	}
	// T1 and a directionless tap after A on its own: the card goes on T1, nothing else counted
	cand, ok := c.Offer(c.Index(id(3)), nil, cutoff05, farNow)
	if !ok || cand.Anchor.ID != id(1) {
		t.Errorf("Offer(T1) = %+v, %v; want A's checkout claim", cand, ok)
	}
}

// TestOffer_N3TheFreshestCheckoutAnchorWins is the race (ürün N3): on one tap both A (open since
// yesterday) and B (left open today by the person's own "Not right? Clock in") are shown -- B is
// asked, A stays on the anomaly list.
func TestOffer_N3TheFreshestCheckoutAnchorWins(t *testing.T) {
	t.Parallel()
	c := chainOf(
		tapEv(1, tap.TypeIn, wall(19, 9, 0, 0)),   // A: Mon, open
		tapEv(2, tap.TypeIn, wall(20, 11, 0, 0)),  // B: Tue 11:00 -- A's T1 (previous business day)
		tapEv(3, tap.TypeIn, wall(20, 18, 0, 0)),  // Tue 18:00 "Not right? Clock in": B's T1, B left open
		tapEv(4, tap.TypeOut, wall(20, 22, 0, 0)), // Tue 22:00: A's second showing and B's
	)
	if !c.Shown(0, 3) || !c.Shown(1, 3) {
		t.Fatalf("PREMISE: Shown(A) %v, Shown(B) %v -- both must be shown at 22:00", c.Shown(0, 3), c.Shown(1, 3))
	}
	cand, ok := c.Offer(3, nil, cutoff05, farNow)
	if !ok || cand.Anchor.ID != id(2) || cand.Kind != KindOut {
		t.Errorf("Offer = %+v, %v; want B's checkout claim", cand, ok)
	}
}

// TestOffer_ACheckInClaimBeatsACheckoutClaim (§C.1: "İkisi birden mümkünse giriş talebi"): Mon 15:00
// in left open, Tue 07:00 the person's chosen out -- on that tap both halves are shown; the check-in
// claim (today's) is asked.
func TestOffer_ACheckInClaimBeatsACheckoutClaim(t *testing.T) {
	t.Parallel()
	c := chainOf(tapEv(1, tap.TypeIn, wall(19, 15, 0, 0)), tapEv(2, tap.TypeOut, wall(20, 7, 0, 0)))
	if !c.Shown(0, 1) || !c.Shown(1, 1) {
		t.Fatal("PREMISE: both must be shown at Tue 07:00")
	}
	cand, ok := c.Offer(1, nil, cutoff05, farNow)
	if !ok || cand.Kind != KindIn || cand.Anchor.ID != id(2) {
		t.Errorf("Offer = %+v, %v; want the check-in claim on Tue 07:00", cand, ok)
	}
}

// TestOffer_NoCardWithoutAWindowLeft: an anchor whose window ended more than manual.MaxBackdate ago,
// and one without a business day, get no card.
func TestOffer_NoCardWithoutAWindowLeft(t *testing.T) {
	t.Parallel()
	c := chainOf(tapEv(1, tap.TypeIn, wall(19, 9, 0, 0)), tapEv(2, tap.TypeIn, wall(20, 9, 0, 0)))
	if _, ok := c.Offer(1, nil, cutoff05, wall(20, 9, 0, 0).Add(manual.MaxBackdate)); ok {
		t.Error("a card for an anchor whose window is past MaxBackdate")
	}
	pre := Event{ID: id(1), Direction: tap.TypeIn, At: wall(19, 9, 0, 0), WrittenAt: wall(19, 9, 0, 0)}
	c = chainOf(pre, tapEv(2, tap.TypeIn, wall(20, 9, 0, 0)))
	if _, ok := c.Offer(1, nil, cutoff05, farNow); ok {
		t.Error("a card for an anchor written before M15 (Ü-Y2)")
	}
}

// TestParseKind_RefusesEverythingButTheTwo.
func TestParseKind_RefusesEverythingButTheTwo(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"in", "out"} {
		if k, ok := ParseKind(s); !ok || string(k) != s {
			t.Errorf("ParseKind(%q) = %q, %v", s, k, ok)
		}
	}
	for _, s := range []string{"", "IN", "both", "out "} {
		if _, ok := ParseKind(s); ok {
			t.Errorf("ParseKind(%q) accepted", s)
		}
	}
}

// TestWindow_EmptyAndContainsAgree pins the two predicates on degenerate windows.
func TestWindow_EmptyAndContainsAgree(t *testing.T) {
	t.Parallel()
	at := wall(19, 9, 0, 0)
	for _, w := range []Window{
		{From: Bound{at, false}, To: Bound{at, true}},
		{From: Bound{at, true}, To: Bound{at, false}},
		{From: Bound{at.Add(time.Second), true}, To: Bound{at, true}},
	} {
		if !w.Empty() || w.Contains(at) {
			t.Errorf("%+v: Empty %v, Contains %v", w, w.Empty(), w.Contains(at))
		}
	}
	if w := (Window{From: Bound{at, true}, To: Bound{at, true}}); w.Empty() || !w.Contains(at) {
		t.Errorf("a one-instant window: Empty %v, Contains %v", w.Empty(), w.Contains(at))
	}
}

// TestAssess_TheBandJudgesBothEnds is the security lens's X2 case (M15-16 review; the orchestrator's
// decision, 2026-10-25): a 10:00–22:00 venue, the check-in at Mon 05:30 and its checkout claimed at
// 23:30 -- 18 h, inside the window, the claimed end inside the band (09:00, 24:00]. The anchor's end
// is outside it, so the step is asked; both ends' deviations are computed.
func TestAssess_TheBandJudgesBothEnds(t *testing.T) {
	t.Parallel()
	venue := shift(10*time.Hour, 22*time.Hour, false)
	c := chainOf(tapEv(1, tap.TypeIn, wall(19, 5, 30, 0)))
	w, _ := c.WindowOf(0, cutoff05, farNow)
	if !w.Contains(wall(19, 23, 30, 0)) {
		t.Fatalf("PREMISE: 23:30 is outside the window %+v", w)
	}
	a := Assess(KindOut, wall(19, 5, 30, 0), wall(19, 23, 30, 0), venue, nil)
	if !InBand(*a.Shift, wall(19, 23, 30, 0)) {
		t.Fatal("PREMISE: the claimed end is inside the band")
	}
	if !a.RequiresConfirmation || a.Interval.Duration() != 18*time.Hour {
		t.Errorf("X2: confirmation %v, interval %v; want the step and 18h", a.RequiresConfirmation, a.Interval.Duration())
	}
	if a.AnchorDeviation != (Deviation{Side: BeforeStart, By: 4*time.Hour + 30*time.Minute}) ||
		a.Deviation != (Deviation{Side: AfterEnd, By: 90 * time.Minute}) {
		t.Errorf("deviations: anchor %+v, claimed %+v", a.AnchorDeviation, a.Deviation)
	}
	// the same at a check-in claim: the anchor out at Tue 00:30 is 9 h 30 m before its shift's start
	a = Assess(KindIn, wall(20, 0, 30, 0), wall(19, 21, 0, 0), venue, nil)
	if !a.RequiresConfirmation || a.AnchorDeviation.Side != BeforeStart {
		t.Errorf("a check-in claim on a 00:30 out: %+v", a)
	}
	// both ends inside: no step
	if Assess(KindOut, wall(19, 10, 0, 0), wall(19, 22, 0, 0), venue, nil).RequiresConfirmation {
		t.Error("10:00–22:00 asks the step")
	}
}

// TestWindow_ACutoffThatEndsTheAnchorsDayEarlierLeavesNoWindow (M15-16 review D-1): the check-in
// Mon 05:30 was recorded in Monday's business day (05:00); with the cutoff now 06:00 it sits in
// Sunday's, which ended at Mon 06:00 -- no window, no card, no claim. A change that does not end the
// day earlier (05:00 to 04:00 for a 09:00 check-in) leaves the window as it was.
func TestWindow_ACutoffThatEndsTheAnchorsDayEarlierLeavesNoWindow(t *testing.T) {
	t.Parallel()
	moved := &tap.DayCutoff{Cutoff: 6 * time.Hour, Timezone: "Europe/Malta"}
	c := chainOf(tapEv(1, tap.TypeIn, wall(19, 5, 30, 0)), tapEv(2, tap.TypeIn, wall(20, 9, 0, 0)))
	if !c.DayCutShort(0, moved) {
		t.Fatal("DayCutShort = false for an 05:30 check-in after 05:00 → 06:00")
	}
	if _, ok := c.WindowOf(0, moved, farNow); ok {
		t.Error("a window for an anchor whose day the new cutoff cut short")
	}
	if _, ok := c.Offer(1, nil, moved, farNow); ok {
		t.Error("a card for it")
	}
	if _, ok := c.WindowOf(0, cutoff05, farNow); !ok {
		t.Error("PREMISE: under the cutoff it was recorded with, the window exists")
	}
	earlier := &tap.DayCutoff{Cutoff: 4 * time.Hour, Timezone: "Europe/Malta"}
	nine := chainOf(tapEv(1, tap.TypeIn, wall(19, 9, 0, 0)))
	if nine.DayCutShort(0, earlier) {
		t.Error("05:00 → 04:00 cut a 09:00 check-in's day short")
	}
	w, ok := nine.WindowOf(0, earlier, farNow)
	if !ok || !w.Contains(wall(19, 17, 0, 0)) || !nine.Closes(0, wall(19, 17, 0, 0), earlier) {
		t.Errorf("05:00 → 04:00: window %+v, %v", w, ok)
	}
	if c.DayCutShort(0, nil) || chainOf(tapEv(1, tap.TypeOut, wall(19, 5, 30, 0))).DayCutShort(0, moved) {
		t.Error("DayCutShort answered without a cutoff, or for an out")
	}
}

// TestAssess_WithAnchorChosenOnlyAddsTheStep (S5): the chooser folds in as an OR -- it asks the step
// inside the band, keeps it outside, and never takes one away.
func TestAssess_WithAnchorChosenOnlyAddsTheStep(t *testing.T) {
	t.Parallel()
	nineToFive := shift(9*time.Hour, 17*time.Hour, false)
	inBand := Assess(KindIn, wall(19, 18, 55, 0), wall(19, 9, 0, 0), nineToFive, nil)
	outBand := Assess(KindOut, wall(19, 9, 0, 0), wall(19, 22, 0, 0), nineToFive, nil)
	for _, tc := range []struct {
		name   string
		a      Assessment
		chosen bool
		want   bool
	}{
		{"in the band, the engine's anchor", inBand, false, false},
		{"in the band, the person's anchor", inBand, true, true},
		{"outside the band, the engine's anchor", outBand, false, true},
		{"outside the band, the person's anchor", outBand, true, true},
		{"no shift, the engine's anchor", Assess(KindOut, wall(19, 9, 0, 0), wall(19, 17, 0, 0), nil, nil), false, true},
	} {
		got := tc.a.WithAnchorChosen(tc.chosen)
		if got.RequiresConfirmation != tc.want || got.AnchorChosen != tc.chosen || got.OutsideBand != tc.a.OutsideBand {
			t.Errorf("%s: %+v", tc.name, got)
		}
	}
	if inBand.OutsideBand || !outBand.OutsideBand {
		t.Fatal("PREMISE: the two assessments are in and outside the band")
	}
}
