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
	"github.com/jackc/pgx/v5"

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
	f.seedAuthzDocument(t, f.tenantID)
	if !f.writer.MayInviteManager(ctx, f.tenantID, f.owner) || !f.writer.May(ctx, f.tenantID, f.owner) {
		t.Fatal("the owner of a materialised business is refused the invitation gate -- the M7-07 lockout")
	}
	if f.writer.MayInviteManager(ctx, f.tenantID, f.manager) {
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
	// A stored rulebook with an unreadable document is decided by what is stored: refused (the shipped
	// baseline answers only for a business with NOTHING stored).
	name, _ := shippedDoc(t, 0)
	f.appendVersion(t, f.tenantID, f.baselineID(t, f.tenantID, name), 2, []byte(`{}`))
	if f.writer.MayInviteManager(ctx, f.tenantID, f.owner) {
		t.Error("an owner whose stored rulebook cannot be read passes the invitation gate")
	}
}

// TestRuleWriterDB_AFreshBusinessOwnerMayInviteFromTheShippedBaseline is the user's decision of
// 2026-10-27: a business with NO stored rule (never materialised -- no tap yet) decides the invitation
// gate on the SHIPPED baseline -- its owner may invite, its manager may not -- and the gate writes
// nothing (the rulebook is still empty afterwards). policy:edit itself (May) is unchanged: still refused
// there, the policies screen's state. MUTATION (measured): the shipped-baseline step removed -> red.
func TestRuleWriterDB_AFreshBusinessOwnerMayInviteFromTheShippedBaseline(t *testing.T) {
	f := newWriterFixture(t)
	ctx := context.Background()
	if !f.writer.MayInviteManager(ctx, f.tenantID, f.owner) {
		t.Fatal("the owner of a business with no rulebook may not invite")
	}
	if f.writer.MayInviteManager(ctx, f.tenantID, f.manager) {
		t.Fatal("a manager of a business with no rulebook may invite")
	}
	if f.writer.May(ctx, f.tenantID, f.owner) {
		t.Error("May (policy:edit) changed for an empty rulebook")
	}
	if n := f.policyCount(t, f.tenantID); n != 0 {
		t.Errorf("the gate wrote %d policy row(s)", n)
	}
}

// policyCount is how many policies rows tenantID holds (read in its own context).
func (f *writerFixture) policyCount(t *testing.T, tenantID uuid.UUID) int {
	t.Helper()
	var n int
	if err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM policies WHERE tenant_id = $1`, tenantID).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n
}
