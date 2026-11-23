package tap

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestDecide_OpenUntilIsWherePredictDirectionStopsClosing ([G45-m]): for every check-in instant
// swept across Malta's two DST days and an ordinary day, and two cutoffs, every instant strictly
// before OpenUntil predicts `out`, and the first instant after it predicts LeftOpen. The bound
// itself is pinned per end: the business day's end already says LeftOpen, the 18 h end still says
// `out` (PredictDirection's `Before` and `>`).
func TestDecide_OpenUntilIsWherePredictDirectionStopsClosing(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	days := []time.Time{
		time.Date(2026, 3, 29, 0, 0, 0, 0, loc),  // spring forward
		time.Date(2026, 10, 25, 0, 0, 0, 0, loc), // fall back
		time.Date(2026, 11, 23, 0, 0, 0, 0, loc), // ordinary
	}
	for _, cut := range []time.Duration{5 * time.Hour, 23 * time.Hour} {
		c := &DayCutoff{Cutoff: cut, Timezone: "Europe/Malta"}
		for _, day := range days {
			for m := 0; m < 24*60; m += 17 {
				in := day.Add(time.Duration(m) * time.Minute)
				open := &Transaction{ID: uuid.New(), OccurredAt: in, Direction: TypeIn}
				until := OpenUntil(in, c)
				if !until.After(in) {
					t.Fatalf("cutoff %v, in %v: OpenUntil %v is not after the check-in", cut, in, until)
				}
				if got := PredictDirection(until.Add(-time.Nanosecond), open, c); got.Direction != TypeOut {
					t.Fatalf("cutoff %v, in %v: just before OpenUntil %v predicts %v, want out", cut, in, until, got.Direction)
				}
				if got := PredictDirection(until.Add(time.Nanosecond), open, c); got.LeftOpen == nil {
					t.Fatalf("cutoff %v, in %v: just after OpenUntil %v is still closable", cut, in, until)
				}
				for _, probe := range []time.Duration{time.Minute, time.Hour, 6 * time.Hour} {
					if at := in.Add(probe); at.Before(until) {
						if got := PredictDirection(at, open, c); got.Direction != TypeOut {
							t.Fatalf("cutoff %v, in %v, at %v (< %v): predicts %v, want out", cut, in, at, until, got.Direction)
						}
					}
				}
				atBound := PredictDirection(until, open, c)
				if until.Equal(in.Add(StaleOpenIn)) {
					if atBound.Direction != TypeOut {
						t.Fatalf("cutoff %v, in %v: at the 18 h bound the stale test (>) still closes, got %v", cut, in, atBound.Direction)
					}
				} else if atBound.LeftOpen == nil {
					t.Fatalf("cutoff %v, in %v: at the business day's end %v the check-in is already left open", cut, in, until)
				}
			}
		}
	}
}

// TestDecide_OpenUntilPinsTheCardsInstants: the card's worked values (M16-45 [G45-a], [G45-k],
// [G45-l]) in Malta winter time (UTC+1).
func TestDecide_OpenUntilPinsTheCardsInstants(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Malta")
	at := func(d, h, m int) time.Time { return time.Date(2026, 11, d, h, m, 0, 0, loc) }
	tests := []struct {
		name string
		cut  time.Duration
		in   time.Time
		want time.Time
	}{
		{"cutoff 05:00, in 09:00 -> 18 h, next day 03:00", 5 * time.Hour, at(23, 9, 0), at(24, 3, 0)},
		{"cutoff 05:00, in 19:30 -> the day ends next 05:00", 5 * time.Hour, at(23, 19, 30), at(24, 5, 0)},
		{"cutoff 23:00, in 18:00 -> the day ends 23:00", 23 * time.Hour, at(23, 18, 0), at(23, 23, 0)},
		{"cutoff 23:00, in 22:45 -> the day ends 23:00", 23 * time.Hour, at(23, 22, 45), at(23, 23, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := OpenUntil(tt.in, &DayCutoff{Cutoff: tt.cut, Timezone: "Europe/Malta"})
			if !got.Equal(tt.want) {
				t.Fatalf("OpenUntil = %v, want %v", got.In(loc), tt.want)
			}
		})
	}
	if got := OpenUntil(at(23, 9, 0), nil); !got.Equal(at(23, 9, 0).Add(StaleOpenIn)) {
		t.Fatalf("no business day: OpenUntil = %v, want in + 18 h", got)
	}
}
