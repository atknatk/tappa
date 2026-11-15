package ledger

// timesheet_test.go -- the engine's half of GET /v1/timesheets (M11 API-14, ADR 0046) without a
// database: the arbitrary range (RangeOf), the manual-shift tally accumulate gained, and the page
// rule that turns the read's budget into a page boundary (timesheetPage). The database half is
// internal/handler/api's timesheets_db_test.go.

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/store"
)

// TestRangeOf_AWeekIsWeekOf: a Monday..Sunday range is the panel's week exactly -- the same From, To,
// FirstDay and number of days -- in an ordinary week and in both of Malta's DST weeks, where the
// week is 167 and 169 hours long. One period builder, two callers.
func TestRangeOf_AWeekIsWeekOf(t *testing.T) {
	t.Parallel()
	zone := malta(t)
	for _, monday := range []Date{
		{Year: 2026, Month: time.August, Day: 3},
		{Year: 2026, Month: time.March, Day: 23},   // Sunday 29 March: 02:00 -> 03:00
		{Year: 2026, Month: time.October, Day: 19}, // Sunday 25 October: 03:00 -> 02:00
	} {
		sunday := Date{Year: monday.Year, Month: monday.Month, Day: monday.Day + 6}
		got, err := RangeOf(monday, sunday, zone)
		if err != nil {
			t.Fatalf("%v..%v: %v", monday, sunday, err)
		}
		want := WeekOf(monday, zone)
		if !got.From.Equal(want.From) || !got.To.Equal(want.To) || got.FirstDay != want.FirstDay || got.Days() != want.Days() {
			t.Errorf("%v..%v: RangeOf %v..%v (%d days), WeekOf %v..%v (%d days)", monday, sunday, got.From, got.To, got.Days(), want.From, want.To, want.Days())
		}
	}
	dst, _ := RangeOf(Date{Year: 2026, Month: time.March, Day: 29}, Date{Year: 2026, Month: time.March, Day: 29}, zone)
	if h := dst.To.Sub(dst.From); h != 23*time.Hour {
		t.Errorf("the spring-forward day is %v long, want 23h: the end is a local midnight, not +24h", h)
	}
}

// TestRangeOf_RefusesAReversedOrTooLongRange: 1..TimesheetMaxDays days are a period, anything else
// is ErrTimesheetRange -- and the period's days are its length, which the day columns follow.
func TestRangeOf_RefusesAReversedOrTooLongRange(t *testing.T) {
	t.Parallel()
	zone := malta(t)
	first := Date{Year: 2026, Month: time.January, Day: 15}
	tests := []struct {
		name string
		last Date
		days int // 0: refused
	}{
		{"one day", first, 1},
		{"thirty-one days", Date{Year: 2026, Month: time.February, Day: 14}, 31},
		{"thirty-two days", Date{Year: 2026, Month: time.February, Day: 15}, 0},
		{"the day before", Date{Year: 2026, Month: time.January, Day: 14}, 0},
		{"no last day", Date{}, 0},
	}
	for _, tc := range tests {
		p, err := RangeOf(first, tc.last, zone)
		switch {
		case tc.days == 0 && !errors.Is(err, ErrTimesheetRange):
			t.Errorf("%s: err %v, want ErrTimesheetRange", tc.name, err)
		case tc.days > 0 && (err != nil || p.Days() != tc.days || len(p.DayLabels(zone)) != tc.days):
			t.Errorf("%s: %d days (%v), want %d", tc.name, p.Days(), err, tc.days)
		}
	}
	if _, err := RangeOf(Date{}, first, zone); !errors.Is(err, ErrTimesheetRange) {
		t.Errorf("no first day: %v", err)
	}
	if got := (Period{}).Days(); got != ReportDays {
		t.Errorf("a Period built by hand has %d days, want ReportDays as before the field existed", got)
	}
}

// TestRangeOf_ANightShiftOnTheLastDayCountsInFullOnThatDay is the boundary rule over a range that is
// not a week: the read runs closingTail past the end, so an 18:00-02:00 shift begun on the range's
// last day is a closed interval attributed to that day; one begun the evening before the range is
// the previous period's (StartedEarlier), not this one's -- the report's rule, unchanged.
func TestRangeOf_ANightShiftOnTheLastDayCountsInFullOnThatDay(t *testing.T) {
	t.Parallel()
	zone := malta(t)
	p, err := RangeOf(Date{Year: 2026, Month: time.November, Day: 10}, Date{Year: 2026, Month: time.November, Day: 12}, zone)
	if err != nil {
		t.Fatal(err)
	}
	// The read starts at the period (Timesheet's window), so the 9th's check-in is not in it: its
	// 02:00 checkout arrives alone.
	evs := []reportEvent{
		ev(maria, "Maria Borg", "out", at(t, zone, 2026, time.November, 10, 2, 0), shift(nightShift())),
		ev(maria, "Maria Borg", "in", at(t, zone, 2026, time.November, 12, 18, 0), shift(nightShift())),
		ev(maria, "Maria Borg", "out", at(t, zone, 2026, time.November, 13, 2, 0), shift(nightShift())),
	}
	rep := accumulate(evs, p, zone, at(t, zone, 2026, time.November, 14, 12, 0))
	m := find(t, rep, maria)
	if m.Worked != 8*time.Hour || m.Shifts != 1 || len(m.Daily) != 3 || m.Daily[2] != 8*time.Hour || m.Daily[0] != 0 {
		t.Errorf("worked %v in %d shifts, daily %v; want 8h on the 12th only", m.Worked, m.Shifts, m.Daily)
	}
	if rep.StartedEarlier != 1 || m.Open != 0 {
		t.Errorf("StartedEarlier %d, Open %d; want the 9th's shift as the previous period's seam", rep.StartedEarlier, m.Open)
	}
}

// TestAccumulate_ManualShiftsCountsCountedShiftsWithATypedEnd: ManualShifts counts SHIFTS, as its
// name says -- unlike ManualArrivals (report.go's measured table), whose four cases are the four rows
// here -- and its day mark sits on the day the shift's hours are on.
func TestAccumulate_ManualShiftsCountsCountedShiftsWithATypedEnd(t *testing.T) {
	t.Parallel()
	zone := malta(t)
	p := week(t, zone)
	day := func(d, hh int) time.Time { return at(t, zone, 2026, time.August, d, hh, 0) }
	tests := []struct {
		name             string
		evs              []reportEvent
		shifts, manual   int
		arrivals         int
		markDay          int // -1: no ManualShift mark
		manualArrivalDay int // -1: no Manual mark
	}{
		{"a typed pair", []reportEvent{ev(maria, "M", "in", day(3, 9), manual()), ev(maria, "M", "out", day(3, 17), manual())}, 1, 1, 1, 0, 0},
		{"a tapped check-in, a typed checkout", []reportEvent{ev(maria, "M", "in", day(4, 9)), ev(maria, "M", "out", day(4, 17), manual())}, 1, 1, 0, 1, -1},
		{"a typed check-in nobody closed", []reportEvent{ev(maria, "M", "in", day(5, 9), manual())}, 0, 0, 1, -1, 2},
		{"a tapped pair", []reportEvent{ev(maria, "M", "in", day(6, 9)), ev(maria, "M", "out", day(6, 17))}, 1, 0, 0, -1, -1},
		{"a typed pair waiting for approval is not counted", []reportEvent{ev(maria, "M", "in", day(7, 9), manual(), verdict("flag", "")), ev(maria, "M", "out", day(7, 17), manual())}, 0, 0, 1, -1, 4},
	}
	for _, tc := range tests {
		rep := accumulate(tc.evs, p, zone, day(10, 12))
		m := find(t, rep, maria)
		if m.Shifts != tc.shifts || m.ManualShifts != tc.manual || m.ManualArrivals != tc.arrivals {
			t.Errorf("%s: shifts %d, ManualShifts %d, ManualArrivals %d; want %d, %d, %d", tc.name, m.Shifts, m.ManualShifts, m.ManualArrivals, tc.shifts, tc.manual, tc.arrivals)
		}
		for d, mk := range m.Marks {
			if mk.ManualShift != (d == tc.markDay) || mk.Manual != (d == tc.manualArrivalDay) {
				t.Errorf("%s: day %d marks %+v", tc.name, d, mk)
			}
		}
	}
}

// rowsOf builds a read: n rows for each person, in the read's order (grouped by person).
func rowsOf(people []uuid.UUID, n []int) []store.ListWorkedShiftEventsRow {
	var out []store.ListWorkedShiftEventsRow
	for i, p := range people {
		for range n[i] {
			id := p
			out = append(out, store.ListWorkedShiftEventsRow{EmployeeID: &id})
		}
	}
	return out
}

// TestTimesheetPage_TheBudgetIsAPageBoundaryNotAWrongTotal pins timesheetPage's rule: a read cut by
// the budget drops the person the cut fell in (their chain may be partial) and only them; a person
// complete before the cut is kept; one person alone over the budget is a floor (Truncated), not
// dropped; the (limit+1)th person only says there is more.
func TestTimesheetPage_TheBudgetIsAPageBoundaryNotAWrongTotal(t *testing.T) {
	t.Parallel()
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	tests := []struct {
		name      string
		rows      []store.ListWorkedShiftEventsRow
		limit     int
		want      []uuid.UUID
		keptRows  int
		truncated bool
		more      bool
	}{
		{"an empty read", nil, 10, nil, 0, false, false},
		{"under the budget, all", rowsOf([]uuid.UUID{a, b}, []int{3, 2}), 10, []uuid.UUID{a, b}, 5, false, false},
		{"the page's extra person is a sign of more", rowsOf([]uuid.UUID{a, b, c}, []int{3, 2, 4}), 2, []uuid.UUID{a, b}, 5, false, true},
		{"exactly the budget is a complete read", rowsOf([]uuid.UUID{a, b}, []int{ReportEventCap - 5, 5}), 10, []uuid.UUID{a, b}, ReportEventCap, false, false},
		{"the cut falls in the last person: dropped", rowsOf([]uuid.UUID{a, b}, []int{ReportEventCap - 5, 6}), 10, []uuid.UUID{a}, ReportEventCap - 5, false, true},
		{"the cut falls between two people: both kept", rowsOf([]uuid.UUID{a, b, c}, []int{ReportEventCap - 5, 5, 1}), 10, []uuid.UUID{a, b}, ReportEventCap, false, true},
		{"one person alone over the budget: a floor", rowsOf([]uuid.UUID{a}, []int{ReportEventCap + 1}), 10, []uuid.UUID{a}, ReportEventCap, true, true},
		{"one person exactly at the budget, then another", rowsOf([]uuid.UUID{a, b}, []int{ReportEventCap, 1}), 10, []uuid.UUID{a}, ReportEventCap, false, true},
	}
	for _, tc := range tests {
		order, kept, truncated, more := timesheetPage(tc.rows, tc.limit)
		if len(order) != len(tc.want) || len(kept) != tc.keptRows || truncated != tc.truncated || more != tc.more {
			t.Errorf("%s: %d people, %d rows, truncated %v, more %v; want %d, %d, %v, %v", tc.name, len(order), len(kept), truncated, more,
				len(tc.want), tc.keptRows, tc.truncated, tc.more)
			continue
		}
		for i := range order {
			if order[i] != tc.want[i] {
				t.Errorf("%s: person %d is %v, want %v", tc.name, i, order[i], tc.want[i])
			}
		}
		for _, r := range kept {
			found := false
			for _, p := range order {
				found = found || *r.EmployeeID == p
			}
			if !found {
				t.Errorf("%s: a kept row belongs to %v, who is not on the page", tc.name, *r.EmployeeID)
			}
		}
	}
}
