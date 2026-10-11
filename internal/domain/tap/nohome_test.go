package tap

import (
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/policy"
)

// TestDecide_AnEmployeeWithNoHomeVenueIsNeverCrossLocation is K-14 (M11 API-1) on the decision:
// an employee the integration manages may have no home venue (employees.location_id NULL), and
// a tap by them is judged exactly like any other on the TAPPED venue's evidence and shift -- it
// is simply never "away from home" (there is no home to be away from: plan §2 item 24), and
// the policy context names no home venue rather than an empty one.
func TestDecide_AnEmployeeWithNoHomeVenueIsNeverCrossLocation(t *testing.T) {
	t.Parallel()
	shift := &Shift{Start: 11 * time.Hour, End: 19 * time.Hour, Timezone: "Europe/Malta"}
	in := withShift(time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC), shift)
	in.Employee.Location = nil
	got := Decide(in)
	if got.Verdict != VerdictOK {
		t.Fatalf("a venue-less employee's on-site tap: Verdict = %q via %q, want ok", got.Verdict, got.MatchedSid)
	}
	if got.CrossLocation {
		t.Error("a venue-less employee's tap is marked cross-location")
	}
	if v, ok := got.PolicyContext[policy.CtxEmployeeLocationID]; ok {
		t.Errorf("the policy context names a home venue %v for an employee who has none", v)
	}
	if cross, ok := got.PolicyContext[policy.CtxEmployeeCrossLocation]; !ok || cross != false {
		t.Errorf("employee:crossLocation = %v (present %v), want false", cross, ok)
	}
	if got.MinutesLate == nil || *got.MinutesLate > 0 {
		t.Errorf("lateness is measured on the tapped venue's shift; an on-time tap read %v", got.MinutesLate)
	}
}
