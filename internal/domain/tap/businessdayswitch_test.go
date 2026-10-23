package tap

import (
	"testing"
	"time"
)

// TestDecide_TheBusinessDaySwitchIsOnFromItsInstantByTheServersClock pins
// SwitchedDayCutoff, the switch's one gate (ADR 0032 §A.8): unset is off, a server
// clock before the value is off, AT the value and after it is on, and the cutoff and
// zone pass through unchanged. The gate takes no declared instant at all — the
// orchestrator's DB tests pin that it is handed the server's clock.
func TestDecide_TheBusinessDaySwitchIsOnFromItsInstantByTheServersClock(t *testing.T) {
	from := time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC)
	cutoff := 5 * time.Hour
	tests := []struct {
		name string
		from *time.Time
		now  time.Time
		on   bool
	}{
		{"no value is off", nil, from.Add(1000 * time.Hour), false},
		{"the server's clock one nanosecond before the value is off", &from, from.Add(-time.Nanosecond), false},
		{"the server's clock a day before the value is off", &from, from.Add(-24 * time.Hour), false},
		{"the value's own instant is on", &from, from, true},
		{"after the value is on", &from, from.Add(time.Minute), true},
		{"a value in another offset is the same instant", ptr(from.In(time.FixedZone("x", 2*3600))), from, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SwitchedDayCutoff(tc.from, tc.now, cutoff, "Europe/Malta")
			if !tc.on {
				if got != nil {
					t.Fatalf("switch off, got %+v", *got)
				}
				return
			}
			if got == nil || got.Cutoff != cutoff || got.Timezone != "Europe/Malta" {
				t.Fatalf("switch on, got %+v, want {5h Europe/Malta}", got)
			}
		})
	}
}
