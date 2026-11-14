package checkin

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/policy"
)

// TestPolicySetDB_AnAddedBaselineDocumentReachesAnExistingTenantOnItsNextTap is ADR 0045
// Bedel 10, measured against the real tables: a business set up before 2026-11-14 holds every
// shipped document EXCEPT base:mock-location-review. Its next tap materialises that one document
// as its version 1 -- no accept step -- and leaves every stored document as it was. Until then
// the panel's stored-set read is all-or-nothing and answers on the guardrails alone, which is
// recorded here as a MEASURED cost of shipping an addition (it refuses the owner policy:edit
// until the next tap), not as a property anybody chose for its own sake.
//
// Then the verdicts: under the materialised set, a context with no tap:mockLocation (every
// browser tap) is decided exactly as under the set the business held before, and an app
// context reporting a mock position is a review via the new rule.
func TestPolicySetDB_AnAddedBaselineDocumentReachesAnExistingTenantOnItsNextTap(t *testing.T) {
	data, tenantID, sets := newPolicyHarness(t)
	svc := &Service{policies: sets}
	ctx := context.Background()

	var mockName string
	var before []policy.BaselineDoc
	for _, d := range policy.Baseline() {
		if d.Document.Statements[0].Sid == policy.SidMockLocationReview {
			mockName = d.Name
			continue
		}
		before = append(before, d)
		doc := d.Document
		doc.Version = "2026-10-18" // the stamp every document carried before the addition
		seedPolicyDocument(t, data, tenantID, d.Name, doc)
	}
	if mockName == "" || len(before) != len(policy.Baseline())-1 {
		t.Fatal("this binary does not ship the mock-location document")
	}

	// BEFORE THE NEXT TAP: the stored set is incomplete, so the panel's read gets guardrails alone.
	if n := len(svc.StoredSet(ctx, tenantID).Baseline); n != 0 {
		t.Fatalf("stored set before the next tap has %d baseline policies; the read is all-or-nothing, want 0", n)
	}

	// THE NEXT TAP materialises the one missing document.
	set := sets.forTenant(ctx, tenantID)
	if len(set.Baseline) != len(policy.Baseline()) {
		t.Fatalf("after the next tap the set has %d baseline documents, want %d", len(set.Baseline), len(policy.Baseline()))
	}
	stamps := map[string]string{}
	if err := data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `
			SELECT p.name, v.document->>'version', v.version_no
			FROM policies p JOIN policy_versions v ON v.policy_id = p.id AND v.tenant_id = p.tenant_id
			WHERE p.tenant_id = $1 AND p.layer = 'baseline'`, tenantID)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var name, stamp string
			var no int32
			if e := rows.Scan(&name, &stamp, &no); e != nil {
				return e
			}
			if no != 1 {
				t.Errorf("%q has a version %d; nothing may be appended to a stored document", name, no)
			}
			stamps[name] = stamp
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("read stamps: %v", err)
	}
	if stamps[mockName] != policy.BaselineVersion {
		t.Fatalf("the added document is stamped %q, want %q", stamps[mockName], policy.BaselineVersion)
	}
	for _, d := range before {
		if stamps[d.Name] != "2026-10-18" {
			t.Fatalf("%q was rewritten to stamp %q; a stored document is never touched", d.Name, stamps[d.Name])
		}
	}
	if n := len(svc.StoredSet(ctx, tenantID).Baseline); n != len(policy.Baseline()) {
		t.Fatalf("stored set after the next tap has %d baseline policies, want %d", n, len(policy.Baseline()))
	}

	// THE VERDICTS. The set the business held before is the materialised one minus the new rule.
	var old policy.Set = set
	old.Baseline = nil
	for _, p := range set.Baseline {
		if p.Document.Statements[0].Sid != policy.SidMockLocationReview {
			old.Baseline = append(old.Baseline, p)
		}
	}
	tapCtx := func(keys map[policy.ContextKey]any) policy.Context {
		base := map[policy.ContextKey]any{
			policy.CtxTapChannel: "nfc", policy.CtxTapSunValid: true, policy.CtxTapGPSConflict: false,
			policy.CtxTapOccurredAtSkewSeconds: float64(0), policy.CtxTapCtrGap: 0,
			policy.CtxTagStatus: "active", policy.CtxEmployeeStatus: "active",
			policy.CtxEmployeeCrossLocation: false,
		}
		for k, v := range keys {
			base[k] = v
		}
		return policy.Context{
			Action: policy.ActionTapRecord, Resources: []string{"location/" + uuid.New().String()},
			Keys: base, SessionTenantID: &tenantID, TagTenantID: &tenantID,
		}
	}
	for _, ev := range []map[policy.ContextKey]any{
		{policy.CtxTapIPMatch: true, policy.CtxTapGPSMatch: false, policy.CtxTapClient: "browser"},
		{policy.CtxTapIPMatch: false, policy.CtxTapGPSMatch: true, policy.CtxTapClient: "browser"},
		{policy.CtxTapIPMatch: false, policy.CtxTapGPSMatch: false, policy.CtxTapClient: "browser"},
	} {
		c := tapCtx(ev)
		got, want := policy.Evaluate(set, c), policy.Evaluate(old, c)
		if got.Effect != want.Effect || got.MatchedSid != want.MatchedSid || !samePolicyVersion(got.PolicyVersionID, want.PolicyVersionID) {
			t.Fatalf("%v: a browser tap is %q via %q after the addition, %q via %q before",
				ev, got.Effect, got.MatchedSid, want.Effect, want.MatchedSid)
		}
	}
	d := policy.Evaluate(set, tapCtx(map[policy.ContextKey]any{
		policy.CtxTapIPMatch: true, policy.CtxTapGPSMatch: true,
		policy.CtxTapClient: "app", policy.CtxTapMockLocation: true,
	}))
	if d.Effect != policy.EffectReview || d.MatchedSid != policy.SidMockLocationReview || d.PolicyVersionID == nil {
		t.Fatalf("an app tap reporting a mock position is %q via %q (version %v), want a review via %s with its stored version",
			d.Effect, d.MatchedSid, d.PolicyVersionID, policy.SidMockLocationReview)
	}
}

func samePolicyVersion(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
