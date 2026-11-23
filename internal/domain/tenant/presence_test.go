package tenant

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/store"
)

// TestPresenceOf_TheCardsInstants pins M16-45's worked values ([G45-a], [G45-e], [G45-g], [G45-j],
// [G45-k], [G45-l]) in Malta winter time.
func TestPresenceOf_TheCardsInstants(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	at := func(d, h, m int) time.Time { return time.Date(2026, 11, d, h, m, 0, 0, loc) }
	ptr := func(x time.Time) *time.Time { return &x }
	day := &tap.Shift{Start: 9 * time.Hour, End: 17 * time.Hour, Timezone: "Europe/Malta"}
	night := &tap.Shift{Start: 18 * time.Hour, End: 2 * time.Hour, Overnight: true, Timezone: "Europe/Malta"}
	tests := []struct {
		name      string
		cut       time.Duration
		in        time.Time
		shift     *tap.Shift
		real      bool
		ends      *time.Time
		remind    *time.Time
		showUntil time.Time
		stale     time.Time
	}{
		{"G45-a real day shift", 5 * time.Hour, at(23, 9, 0), day, true, ptr(at(23, 17, 0)), ptr(at(23, 17, 30)), at(23, 19, 30), at(24, 3, 0)},
		{"G45-e no shift", 5 * time.Hour, at(23, 9, 0), nil, true, nil, ptr(at(23, 19, 0)), at(23, 21, 0), at(24, 3, 0)},
		{"G45-g shift set but not marked real", 5 * time.Hour, at(23, 9, 0), day, false, nil, ptr(at(23, 19, 0)), at(23, 21, 0), at(24, 3, 0)},
		{"G45-j night shift, 02:30 check-in clamps to the next evening", 5 * time.Hour, at(23, 2, 30), night, true, nil, ptr(at(23, 4, 30)), at(23, 5, 0), at(23, 5, 0)},
		{"G45-j day shift, 23:00 check-in", 5 * time.Hour, at(23, 23, 0), day, true, nil, ptr(at(24, 4, 30)), at(24, 5, 0), at(24, 5, 0)},
		{"G45-k 19:30 check-in, the clip", 5 * time.Hour, at(23, 19, 30), nil, false, nil, ptr(at(24, 4, 30)), at(24, 5, 0), at(24, 5, 0)},
		{"G45-l 18:00 check-in, 23:00 cutoff", 23 * time.Hour, at(23, 18, 0), nil, false, nil, ptr(at(23, 22, 30)), at(23, 23, 0), at(23, 23, 0)},
		{"G45-l 22:45 check-in, no reminder", 23 * time.Hour, at(23, 22, 45), nil, false, nil, nil, at(23, 23, 0), at(23, 23, 0)},
	}
	eq := func(a, b *time.Time) bool { return (a == nil && b == nil) || (a != nil && b != nil && a.Equal(*b)) }
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := presenceOf(tt.in, tt.shift, tt.real, &tap.DayCutoff{Cutoff: tt.cut, Timezone: "Europe/Malta"}, "Europe/Malta")
			if !p.InSince.Equal(tt.in) || !p.StaleAt.Equal(tt.stale) || !p.ShowUntil.Equal(tt.showUntil) ||
				!eq(p.ShiftEndsAt, tt.ends) || !eq(p.RemindAt, tt.remind) {
				t.Fatalf("got in %v ends %v remind %v show %v stale %v", p.InSince.In(loc), p.ShiftEndsAt, p.RemindAt, p.ShowUntil.In(loc), p.StaleAt.In(loc))
			}
			if p.ShowUntil.After(p.StaleAt) {
				t.Fatalf("showUntil %v after staleAt %v", p.ShowUntil, p.StaleAt)
			}
		})
	}
}

// TestPresenceOf_NightShiftAcrossMidnight ([G45-c]): an 18:00 check-in against 18:00-02:00 ends at
// 02:00 the next day, on both of Malta's DST days.
func TestPresenceOf_NightShiftAcrossMidnight(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Malta")
	night := &tap.Shift{Start: 18 * time.Hour, End: 2 * time.Hour, Overnight: true, Timezone: "Europe/Malta"}
	for _, d := range []time.Time{time.Date(2026, 3, 28, 18, 0, 0, 0, loc), time.Date(2026, 10, 24, 18, 0, 0, 0, loc)} {
		p := presenceOf(d, night, true, &tap.DayCutoff{Cutoff: 5 * time.Hour, Timezone: "Europe/Malta"}, "Europe/Malta")
		want := time.Date(d.Year(), d.Month(), d.Day()+1, 2, 0, 0, 0, loc)
		if p.ShiftEndsAt == nil || !p.ShiftEndsAt.Equal(want) {
			t.Fatalf("%v: shift ends %v, want %v", d, p.ShiftEndsAt, want)
		}
	}
}

func TestUsualShiftOf_DepartmentThenProfileLocation(t *testing.T) {
	tm := func(h int) pgtype.Time { return pgtype.Time{Microseconds: int64(h) * 3600e6, Valid: true} }
	yes := true
	row := store.GetEmployeeUsualShiftRow{StaffShiftVisible: true, LocationShiftStart: tm(10), LocationShiftEnd: tm(22), LocationName: "Sliema"}
	if s := usualShiftOf(row); s == nil || s.Start != 10*time.Hour || s.Venue != "Sliema" {
		t.Fatalf("profile location: %+v", s)
	}
	row.DepartmentShiftStart, row.DepartmentShiftEnd, row.DepartmentOvernight = tm(18), tm(2), &yes
	if s := usualShiftOf(row); s == nil || s.Start != 18*time.Hour || !s.Overnight {
		t.Fatalf("department first: %+v", s)
	}
	row.StaffShiftVisible = false
	if s := usualShiftOf(row); s != nil {
		t.Fatalf("switch off: %+v", s)
	}
}
