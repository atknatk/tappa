package tap

import (
	"testing"
	"time"
)

// TestShiftSpanAt_IsTheInstanceLatenessMeasuresFrom (ADR 0032 §C.2, §C.4 -- the claim's prefill and
// band): the start is MinutesLate's, the end the End wall clock on the start's date or the next one
// for an overnight shift, both built in the zone -- so the October night that falls back is 9 hours
// on the clock face 22:00–06:00.
func TestShiftSpanAt_IsTheInstanceLatenessMeasuresFrom(t *testing.T) {
	t.Parallel()
	malta, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	w := func(y int, m time.Month, d, hh, mm int) time.Time { return time.Date(y, m, d, hh, mm, 0, 0, malta) }
	day := Shift{Start: 9 * time.Hour, End: 17 * time.Hour, Timezone: "Europe/Malta"}
	rusty := Shift{Start: 18 * time.Hour, End: 2 * time.Hour, Overnight: true, Timezone: "Europe/Malta"}
	night := Shift{Start: 22 * time.Hour, End: 6 * time.Hour, Overnight: true, Timezone: "Europe/Malta"}
	flat := Shift{Start: 22 * time.Hour, End: 6 * time.Hour, Timezone: "Europe/Malta"} // end before start, flag unset
	tests := []struct {
		name       string
		at         time.Time
		s          Shift
		start, end time.Time
		length     time.Duration
	}{
		{"a day shift", w(2026, 10, 19, 9, 12), day, w(2026, 10, 19, 9, 0), w(2026, 10, 19, 17, 0), 8 * time.Hour},
		{"a day shift read late in the evening", w(2026, 10, 19, 23, 0), day, w(2026, 10, 19, 9, 0), w(2026, 10, 19, 17, 0), 8 * time.Hour},
		{"Rusty Bar, the evening", w(2026, 10, 19, 18, 5), rusty, w(2026, 10, 19, 18, 0), w(2026, 10, 20, 2, 0), 8 * time.Hour},
		{"Rusty Bar, the after-midnight tail", w(2026, 10, 20, 1, 0), rusty, w(2026, 10, 19, 18, 0), w(2026, 10, 20, 2, 0), 8 * time.Hour},
		{"the night the clocks fall back", w(2026, 10, 24, 22, 30), night, w(2026, 10, 24, 22, 0), w(2026, 10, 25, 6, 0), 9 * time.Hour},
		{"an end before the start is the next day", w(2026, 10, 19, 22, 30), flat, w(2026, 10, 19, 22, 0), w(2026, 10, 20, 6, 0), 8 * time.Hour},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			start, end, ok := ShiftSpanAt(tc.at.UTC(), tc.s)
			if !ok {
				t.Fatal("no span")
			}
			if !start.Equal(tc.start) || !end.Equal(tc.end) || end.Sub(start) != tc.length {
				t.Errorf("span %s–%s (%v), want %s–%s (%v)", start.In(malta), end.In(malta), end.Sub(start), tc.start, tc.end, tc.length)
			}
			if start.Location() != time.UTC || end.Location() != time.UTC {
				t.Error("the span is not in UTC (§6)")
			}
			late := MinutesLate(tc.at.UTC(), tc.s)
			if late == nil || *late != int(tc.at.Sub(start)/time.Minute) {
				t.Errorf("the start is not MinutesLate's: late %v", late)
			}
		})
	}
	if _, _, ok := ShiftSpanAt(w(2026, 10, 19, 9, 0), Shift{Start: 9 * time.Hour, End: 17 * time.Hour}); ok {
		t.Error("a span in no zone")
	}
}
