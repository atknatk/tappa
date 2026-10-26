package tenant

// managerinvitegate_db_test.go -- M14-15 (M7-07's sixth criterion, ADR 0031 §5.11): the manager
// invitation is gated by the POLICY ENGINE through RuleWriter.May -- policy:edit on the stored rulebook
// -- and this file is the card's "önce ölç": the trap M7-07 names (a NEW action against a
// materialised baseline answers deny sid=default and locks the owner out) measured, and the gate the
// panel uses shown not to fall into it.

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/policy"
)

// TestRuleWriterDB_TheManagerInvitationGateLetsAMaterialisedOwnerIn: in a business whose baseline is
// stored (every shipped document, as a tap materialises it), the owner may invite and the manager may
// not (sys:policy-edit-owner-only, terminal). The CONTRAST is the trap itself: the same stored set asked
// about an action the materialised baseline does not name answers deny with the engine's default
// sentinel -- what a new invitation action would have answered every owner of every business
// materialised before it shipped. The named cost: a business with NOTHING stored refuses its owner
// too (the policies screen's state; checkin.Service.StoredSet's header).
func TestRuleWriterDB_TheManagerInvitationGateLetsAMaterialisedOwnerIn(t *testing.T) {
	f := newWriterFixture(t)
	ctx := context.Background()
	if f.writer.May(ctx, f.tenantID, f.owner) {
		t.Fatal("PREMISE: the owner of a business with nothing stored is allowed")
	}
	f.seedAuthzDocument(t, f.tenantID)
	if !f.writer.May(ctx, f.tenantID, f.owner) {
		t.Fatal("the owner of a materialised business is refused the invitation gate -- the M7-07 lockout")
	}
	if f.writer.May(ctx, f.tenantID, f.manager) {
		t.Fatal("a manager passes the invitation gate")
	}
	set := storedAuthority{data: f.data, params: testParams()}.StoredSet(ctx, f.tenantID)
	in := policy.Context{Action: policy.Action("admin:invite"), Resources: []string{"policy/" + f.tenantID.String()},
		Keys: map[policy.ContextKey]any{policy.CtxActorRole: "owner"}}
	got, empty := policy.Evaluate(set, in), policy.Evaluate(policy.Set{}, in)
	if got.Effect == policy.EffectAllow || got.MatchedSid != empty.MatchedSid {
		t.Fatalf("CONTRAST: a new action against the materialised baseline answered %s/%s, the trap is %s/%s",
			got.Effect, got.MatchedSid, policy.EffectDeny, empty.MatchedSid)
	}
	t.Logf("measured: policy:edit owner allowed, manager refused; a new action -> %s sid=%s (the M7-07 lockout the gate avoids)", got.Effect, got.MatchedSid)
	if f.writer.May(ctx, uuid.New(), f.owner) {
		t.Error("an unknown business allows the owner")
	}
}
