package checkin

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/policy"
)

// TestPolicySetDB_ABusinessSetUpBeforeTheLiveQRRuleGetsItOnItsFirstLiveQRTap is the card's central
// claim, kept (the third eye's probe, M16-21): a business whose stored baseline predates 2026-11-19
// -- every shipped document but base:live-qr-requires-ip, stamped 2026-11-14 -- takes its first live
// QR tap with GPS alone, and the tap path's set (forTenant) materialises the added document and
// reviews it via base:live-qr-requires-ip, with a stored version id. That is why the rule is its own
// document and not a widened base:qr-requires-ip: a change would never have reached this business.
// CONTROL: the stored set before the addition lets the same tap through via base:gps-only-allow,
// and with an IP match the new set allows it.
func TestPolicySetDB_ABusinessSetUpBeforeTheLiveQRRuleGetsItOnItsFirstLiveQRTap(t *testing.T) {
	data, tenantID, sets := newPolicyHarness(t)
	ctx := context.Background()
	var liveName string
	var before []policy.BaselineDoc
	for _, d := range policy.Baseline() {
		if d.Document.Statements[0].Sid == policy.SidLiveQRRequiresIP {
			liveName = d.Name
			continue
		}
		dd := d
		dd.Document.Version = "2026-11-14"
		before = append(before, dd)
	}
	if liveName == "" {
		t.Fatal("this binary does not ship base:live-qr-requires-ip")
	}
	if err := sets.materialise(ctx, tenantID, before); err != nil {
		t.Fatal(err)
	}
	loc := uuid.New()
	c := policy.Context{
		Action: policy.ActionTapRecord, Resources: []string{"location/" + loc.String()},
		Keys: map[policy.ContextKey]any{
			policy.CtxTapChannel: "qr_live", policy.CtxTapSunValid: false, policy.CtxTapGPSConflict: false,
			policy.CtxTapIPMatch: false, policy.CtxTapGPSMatch: true,
			policy.CtxTapOccurredAtSkewSeconds: float64(0), policy.CtxTapPageAgeSeconds: float64(3),
			policy.CtxTapTokenVerified: true, policy.CtxTapTokenOverdueSeconds: float64(-10),
			policy.CtxScreenStatus: "active", policy.CtxLocationID: loc.String(),
			policy.CtxEmployeeStatus: "active", policy.CtxEmployeeCrossLocation: false,
			policy.CtxTapClient: "browser",
		},
		SessionTenantID: &tenantID, TagTenantID: &tenantID,
	}

	rows, err := sets.load(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	old := sets.guardrailsOnly()
	old.Baseline, _ = assembleBaseline(rows)
	if len(old.Baseline) != len(policy.Baseline())-1 {
		t.Fatalf("PREMISE: the business stores %d documents, want every one but the live QR rule", len(old.Baseline))
	}
	if d := policy.Evaluate(old, c); d.Effect != policy.EffectAllow || d.MatchedSid != policy.SidGPSOnlyAllow {
		t.Fatalf("CONTROL: the stored set before the addition decides %s via %s, want allow via %s", d.Effect, d.MatchedSid, policy.SidGPSOnlyAllow)
	}

	set := sets.forTenant(ctx, tenantID)
	if d := policy.Evaluate(set, c); d.Effect != policy.EffectReview || d.MatchedSid != policy.SidLiveQRRequiresIP || d.PolicyVersionID == nil {
		t.Fatalf("first GPS-only live QR tap: %s via %s (version %v), want review via %s with a stored version",
			d.Effect, d.MatchedSid, d.PolicyVersionID, policy.SidLiveQRRequiresIP)
	}
	if s := baselineRows(t, data, tenantID)[liveName]; len(s) != 1 || s[0] != policy.BaselineVersion {
		t.Fatalf("the live QR rule is stored as %v, want once at %s", s, policy.BaselineVersion)
	}
	c.Keys[policy.CtxTapIPMatch] = true
	if d := policy.Evaluate(set, c); d.Effect != policy.EffectAllow {
		t.Fatalf("CONTROL: with an IP match: %s via %s, want allow", d.Effect, d.MatchedSid)
	}
}
