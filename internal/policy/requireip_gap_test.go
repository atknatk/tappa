package policy

import "testing"

// TestBaseline_RequiringIPLeavesACrossLocationGPSTapAllowed MEASURES a gap (backlog T125, found in
// M11 API-1c's third review; not fixed here). Q16's lever for "this business requires IP" is
// disabling base:gps-only-allow (a disabled policy is simply absent from the Set). For a tap at the
// employee's HOME location that works: no statement matches and the evaluator's default is review.
// For a tap at ANOTHER location of the same business, base:cross-location-note -- an allow that only
// meant "a cross-location tap is not penalised for that alone" -- matches instead, and the GPS-only
// tap is allowed. With no evidence at all base:no-evidence-review still wins (review out-restricts
// allow), so the gap is the GPS-only shape exactly; and the OTHER lever, a tenant review over the
// same condition (TestBaseline_TenantOverridesGPSOnly), out-restricts the note's allow and is not
// affected (measured with a cross-location context: review via the tenant's sid). When T125 is fixed this test's cross-location
// row turns red, and is rewritten to the fixed outcome.
func TestBaseline_RequiringIPLeavesACrossLocationGPSTapAllowed(t *testing.T) {
	var baseline []Policy
	for _, p := range fullBaseline() {
		if p.Document.Statements[0].Sid != SidGPSOnlyAllow {
			baseline = append(baseline, p)
		}
	}
	set := Set{Guardrails: DefaultGuardrails(), Baseline: baseline}
	tests := []struct {
		name              string
		ip, gps, crossLoc bool
		wantEffect        Effect
		wantSid           string
	}{
		{"home location, GPS only: review by default", false, true, false, EffectReview, ""},
		{"other location, GPS only: allowed by the cross-location note (the gap)", false, true, true, EffectAllow, SidCrossLocationNote},
		{"other location, no evidence: no-evidence-review still wins", false, false, true, EffectReview, SidNoEvidenceReview},
		{"other location, IP: allowed by the IP statement", true, false, true, EffectAllow, SidIPOrGPSOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := Evaluate(set, benignTap(map[ContextKey]any{
				CtxTapIPMatch: tc.ip, CtxTapGPSMatch: tc.gps, CtxEmployeeCrossLocation: tc.crossLoc,
			}))
			if d.Effect != tc.wantEffect || (tc.wantSid != "" && d.MatchedSid != tc.wantSid) {
				t.Fatalf("got %q via %q, want %q via %q", d.Effect, d.MatchedSid, tc.wantEffect, tc.wantSid)
			}
		})
	}
}
