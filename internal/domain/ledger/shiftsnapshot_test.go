package ledger

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/store"
)

// TestEvents_ARecordedShiftBeatsTodaysAndOnlyAnUnrecordedOneIsFromToday is T152 at the mapper (ADR
// 0042): a row carrying a shift snapshot is judged against THAT shift — in its recorded zone —
// whatever today's venue or department rows say; 'none' is a recorded "no shift" and stays
// unmeasured even when the venue has one today; only a row with no snapshot (before 00095, or
// manual) falls back to today's settings, and it alone is marked ShiftFromToday.
func TestEvents_ARecordedShiftBeatsTodaysAndOnlyAnUnrecordedOneIsFromToday(t *testing.T) {
	t.Parallel()
	pgTime := func(d time.Duration) pgtype.Time {
		return pgtype.Time{Microseconds: int64(d / time.Microsecond), Valid: true}
	}
	who, in := maria, "in"
	yes, no := true, false
	str := func(s string) *string { return &s }
	// Today the venue opens at 11:00 and the department at 06:00 -- both edited after the taps.
	today := store.ListWorkedShiftEventsRow{
		EmployeeID: &who, Type: &in, Verdict: "ok", Channel: "nfc",
		LocationShiftStart: pgTime(11 * time.Hour), LocationShiftEnd: pgTime(19 * time.Hour), LocationOvernight: &no,
	}
	withDept := today
	withDept.DepartmentShiftStart, withDept.DepartmentShiftEnd, withDept.DepartmentOvernight = pgTime(6*time.Hour), pgTime(14*time.Hour), &no

	recorded := withDept
	recorded.ShiftSource, recorded.ShiftStart, recorded.ShiftEnd = str("location"), pgTime(18*time.Hour), pgTime(2*time.Hour)
	recorded.ShiftOvernight, recorded.ShiftTimezone = &yes, str("Europe/Lisbon")

	none := withDept
	none.ShiftSource = str("none")

	manual := withDept
	manual.Channel = "manual"

	broken := today
	// Everything but the end: no snapshot. (A first version left the flag and the zone out too,
	// and a mutant that checked only those two stayed green -- measured.)
	broken.ShiftSource, broken.ShiftStart, broken.ShiftOvernight, broken.ShiftTimezone = str("location"), pgTime(9*time.Hour), &no, str("Europe/Lisbon")

	cases := []struct {
		name      string
		row       store.ListWorkedShiftEventsRow
		want      *tap.Shift
		fromToday bool
	}{
		{"a recorded shift, in its recorded zone, over today's department", recorded,
			&tap.Shift{Start: 18 * time.Hour, End: 2 * time.Hour, Overnight: true, Timezone: "Europe/Lisbon"}, false},
		{"a recorded 'none' stays unmeasured though the venue has a shift today", none, nil, false},
		{"no snapshot: today's department, marked", withDept,
			&tap.Shift{Start: 6 * time.Hour, End: 14 * time.Hour, Timezone: "Europe/Malta"}, true},
		{"no snapshot: today's venue, marked", today,
			&tap.Shift{Start: 11 * time.Hour, End: 19 * time.Hour, Timezone: "Europe/Malta"}, true},
		{"a manual row: today's, marked", manual,
			&tap.Shift{Start: 6 * time.Hour, End: 14 * time.Hour, Timezone: "Europe/Malta"}, true},
		{"a half snapshot is no snapshot: today's, marked", broken,
			&tap.Shift{Start: 11 * time.Hour, End: 19 * time.Hour, Timezone: "Europe/Malta"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, _ := events([]store.ListWorkedShiftEventsRow{c.row}, "Europe/Malta", nil)
			if len(got) != 1 {
				t.Fatalf("mapped %d events, want 1", len(got))
			}
			e := got[0]
			switch {
			case c.want == nil && e.Shift != nil:
				t.Errorf("shift %+v, want none", *e.Shift)
			case c.want != nil && (e.Shift == nil || *e.Shift != *c.want):
				t.Errorf("shift %+v, want %+v", e.Shift, *c.want)
			}
			if e.ShiftFromToday != c.fromToday {
				t.Errorf("ShiftFromToday = %v, want %v", e.ShiftFromToday, c.fromToday)
			}
		})
	}
}

// TestAccumulate_CountsTheArrivalsMeasuredAgainstTodaysShift: Totals.ShiftFromToday counts the
// period's attributed check-ins whose shift came from today's settings -- measured or not -- and
// nothing else: not a recorded arrival, not a checkout, not a check-in outside the period.
func TestAccumulate_CountsTheArrivalsMeasuredAgainstTodaysShift(t *testing.T) {
	t.Parallel()
	zone := malta(t)
	p := week(t, zone)
	mon := func(d, hh, mm int) time.Time { return at(t, zone, 2026, time.August, d, hh, mm) }
	evs := []reportEvent{
		{EmployeeID: maria, Direction: "in", Verdict: "ok", At: mon(3, 10, 20), Shift: dayShift(), ShiftFromToday: true},
		{EmployeeID: maria, Direction: "out", Verdict: "ok", At: mon(3, 18, 0), Shift: dayShift(), ShiftFromToday: true},
		{EmployeeID: maria, Direction: "in", Verdict: "ok", At: mon(4, 10, 5), Shift: dayShift()},
		{EmployeeID: maria, Direction: "out", Verdict: "ok", At: mon(4, 18, 0), Shift: dayShift()},
		{EmployeeID: maria, Direction: "in", Verdict: "ok", At: mon(5, 10, 0), Manual: true, ShiftFromToday: true}, // today: no shift
		{EmployeeID: maria, Direction: "out", Verdict: "ok", At: mon(5, 18, 0)},
		{EmployeeID: maria, Direction: "in", Verdict: "ok", At: mon(12, 10, 0), Shift: dayShift(), ShiftFromToday: true}, // after the week
	}
	r := accumulate(evs, p, zone, mon(12, 12, 0))
	if r.Totals.ShiftFromToday != 2 {
		t.Errorf("Totals.ShiftFromToday = %d, want 2: the period's two check-ins with no snapshot", r.Totals.ShiftFromToday)
	}
	if len(r.People) != 1 || r.People[0].LateShifts != 2 || r.People[0].Unmeasured != 1 {
		t.Fatalf("PREMISE: people %+v, want Maria late twice and one unmeasured", r.People)
	}
}
