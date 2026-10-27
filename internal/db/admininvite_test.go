package db

// admininvite_test.go -- migration 00077 (M14-15, ADR 0031 §5.1): admin_invites is TENANT-SCOPED, and
// RLS -- not a WHERE -- is what keeps one business's invitations from another. In opTx's transaction,
// rolled back.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const ai00077File = "00077_invite_administrators.sql"

// aiInvite inserts an open invitation of tenant as the owner and returns its id.
func aiInvite(t *testing.T, ctx context.Context, q pgx.Tx, tenant, by uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := q.QueryRow(ctx, `INSERT INTO admin_invites (tenant_id, email, full_name, role, token_hash, invited_by, expires_at)
	                           VALUES ($1, $2, 'M14-15 Invitee', 'manager', $3, $4, now() + interval '1 day') RETURNING id`,
		tenant, aiEmail(), opRandHex(t), by).Scan(&id); err != nil {
		t.Fatalf("insert an invitation: %v", err)
	}
	return id
}

// TestAdminInvitesRLS_ABusinessSeesOnlyItsOwnInvitations is the card's RLS isolation test, WITHOUT a
// tenant filter in the probing statements (a filter would make them pass with RLS off): as tappa_app in
// A's context, A's invitation is visible (positive control) and B's is not -- not by id, not in a bare
// count, not to an UPDATE (cancel) and not to an INSERT naming B; with no context nothing is visible.
// MUTATION (measured): the policy's USING widened to true -> red.
func TestAdminInvitesRLS_ABusinessSeesOnlyItsOwnInvitations(t *testing.T) {
	ctx, tx := opTx(t)
	a, b := aiTenant(t, ctx, tx), aiTenant(t, ctx, tx)
	ownerA, ownerB := aiAdmin(t, ctx, tx, a, aiEmail()), aiAdmin(t, ctx, tx, b, aiEmail())
	inA, inB := aiInvite(t, ctx, tx, a, ownerA), aiInvite(t, ctx, tx, b, ownerB)

	count := func(tenant uuid.UUID, sql string, args ...any) int64 {
		t.Helper()
		var n int64
		if err := aiApp(t, ctx, tx, tenant, func(sp pgx.Tx) error { return sp.QueryRow(ctx, sql, args...).Scan(&n) }); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}
	if n := count(a, `SELECT count(*) FROM admin_invites WHERE id = $1`, inA); n != 1 {
		t.Fatalf("POSITIVE CONTROL: A does not see its own invitation (%d)", n)
	}
	if n := count(a, `SELECT count(*) FROM admin_invites WHERE id = $1`, inB); n != 0 {
		t.Errorf("A sees B's invitation")
	}
	var total int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM admin_invites WHERE tenant_id IN ($1, $2)`, a, b).Scan(&total); err != nil || total != 2 {
		t.Fatalf("PREMISE: %d invitations in the two businesses (%v)", total, err)
	}
	if n := count(a, `SELECT count(*) FROM admin_invites WHERE id IN ($1, $2)`, inA, inB); n != 1 {
		t.Errorf("A's unfiltered read of both ids sees %d, want 1", n)
	}
	if n := count(uuid.Nil, `SELECT count(*) FROM admin_invites WHERE id IN ($1, $2)`, inA, inB); n != 0 {
		t.Errorf("no context sees %d invitations", n)
	}
	var cancelled int64
	if err := aiApp(t, ctx, tx, a, func(sp pgx.Tx) error {
		var e error
		cancelled, e = aiRows(ctx, sp, `UPDATE admin_invites SET cancelled_at = now() WHERE id = $1`, inB)
		return e
	}); err != nil || cancelled != 0 {
		t.Errorf("A cancelled %d of B's invitations (%v)", cancelled, err)
	}
	if err := aiApp(t, ctx, tx, a, func(sp pgx.Tx) error {
		_, e := sp.Exec(ctx, `INSERT INTO admin_invites (tenant_id, email, full_name, role, token_hash, invited_by, expires_at)
		                       VALUES ($1, $2, 'x', 'manager', $3, $4, now() + interval '1 hour')`, b, aiEmail(), opRandHex(t), ownerB)
		return e
	}); err == nil {
		t.Error("A inserted an invitation into B")
	}
}

// TestAdminInvites00077_TheAppReachIsTheFilesAndNothingMore: tappa_app cannot read token_hash, cannot
// write used_at (only the definer spends an invitation), cannot backdate created_at and cannot delete.
func TestAdminInvites00077_TheAppReachIsTheFilesAndNothingMore(t *testing.T) {
	ctx, tx := opTx(t)
	a := aiTenant(t, ctx, tx)
	in := aiInvite(t, ctx, tx, a, aiAdmin(t, ctx, tx, a, aiEmail()))
	for _, sql := range []string{
		`SELECT token_hash FROM admin_invites WHERE id = $1`,
		`UPDATE admin_invites SET used_at = now() WHERE id = $1`,
		`DELETE FROM admin_invites WHERE id = $1`,
	} {
		err := aiApp(t, ctx, tx, a, func(sp pgx.Tx) error { _, e := sp.Exec(ctx, sql, in); return e })
		if code, _ := opCode(err); code != sqlstateInsufficientPrivi {
			t.Errorf("%s as tappa_app: %v, want 42501", sql, err)
		}
	}
}

// TestAdminInvites00077_DownRefusesAnInvitationAndOtherwiseWalksBackAndUp: with an invitation the Down
// is 55000 naming 00077 and changes nothing; without, it drops the table and the three functions and
// takes back the definer's admin_users INSERT; the Up gives them back. In the test's transaction, the
// table the Down locks taken first.
func TestAdminInvites00077_DownRefusesAnInvitationAndOtherwiseWalksBackAndUp(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, ai00077File)
	lockForDowns(t, ctx, tx, downTables(down))
	shape := func(q pgx.Tx) string {
		t.Helper()
		var s string
		if err := q.QueryRow(ctx, `SELECT (to_regclass('public.admin_invites') IS NOT NULL)::text || ','
		                                 || (to_regprocedure('public.identity_accept_invite(text,text)') IS NOT NULL)::text || ','
		                                 || (to_regprocedure('public.identity_accept_invite_new(text,text)') IS NOT NULL)::text || ','
		                                 || (to_regprocedure('public.resolve_admin_invite_by_token_hash(text)') IS NOT NULL)::text || ','
		                                 || has_column_privilege('tappa_identitydefiner', 'public.admin_users', 'identity_id', 'INSERT')::text`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	if got := shape(tx); got != "true,true,true,true,true" {
		t.Fatalf("PREMISE at HEAD: %s", got)
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		a := aiTenant(t, ctx, sp)
		aiInvite(t, ctx, sp, a, aiAdmin(t, ctx, sp, a, aiEmail()))
		err := holdingNestedRun(t, ctx, sp, down)
		if code, _ := opCode(err); code != sqlstatePrerequisiteState {
			t.Fatalf("the Down with an invitation answered %v, want 55000", err)
		}
	})
	// M14-17 (00078) shares the definer's admin_users INSERT (identity_claim_signup's owner row): 00077's
	// Down takes it only while identity_claim_signup is absent. Both trees are walked: as the merged tree
	// stands (the claim present -> the shared grant stays) and with the claim dropped first (-> it goes).
	// 00080 (M14-17b) is walked back first: 00077's Down names 00078's three-argument signature.
	icUndo00080(t, ctx, tx)
	claimApplied := opInt(t, ctx, tx, `SELECT count(*) FROM pg_proc WHERE oid = to_regprocedure('public.identity_claim_signup(text,uuid,text)')`) == 1
	for _, dropClaim := range []bool{false, true} {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			if dropClaim {
				aiExec(t, ctx, sp, `DROP FUNCTION IF EXISTS public.identity_claim_signup(text, uuid, text)`)
			}
			aiExec(t, ctx, sp, `DELETE FROM admin_invites`)
			opRunSection(t, ctx, sp, down, "00077 Down")
			want := "false,false,false,false,false"
			if claimApplied && !dropClaim {
				want = "false,false,false,false,true"
			}
			if got := shape(sp); got != want {
				t.Fatalf("after the Down (claim dropped %v): %s, want %s", dropClaim, got, want)
			}
			opRunSection(t, ctx, sp, up, "00077 Up again")
			if got := shape(sp); got != "true,true,true,true,true" {
				t.Fatalf("after the Up again: %s", got)
			}
		})
	}
}

// kelvin is U+212A KELVIN SIGN: citext folds it onto the ASCII 'k' (the M14-08a finding, measured), so
// an address carrying it equals an ASCII address under OPERATOR(public.=).
const kelvin = "K"

// TestAdminInvites00077_NoIdentityIsBornOrLinkedForANonASCIIAddress is the 2026-10-26 address rule in
// the database, three ways, as tappa_app through the real functions:
//   - admin_invites refuses a non-ASCII address (admin_invites_email_ascii, 23514);
//   - identity_accept_invite_new, handed an invitation whose address is not ASCII (the CHECK taken away
//     in a savepoint -- a row a future writer could leave), births NOTHING: 0 rows, no identity;
//   - identity_accept_invite refuses a session whose identity's address only FOLDS onto the invited one
//     (a Kelvin sign where the invitation has 'k'): 0 rows, no membership -- the squat the rule closes.
//
// PREMISE: citext really folds the Kelvin sign (else the third case would prove nothing). MUTATION
// (measured): each function's ASCII predicate removed -> its case red.
func TestAdminInvites00077_NoIdentityIsBornOrLinkedForANonASCIIAddress(t *testing.T) {
	ctx, tx := opTx(t)
	a := aiTenant(t, ctx, tx)
	by := aiAdmin(t, ctx, tx, a, aiEmail())
	local := "m14-15-" + uuid.NewString()[:8]
	ascii, folded := local+"k@identity.example", local+kelvin+"@identity.example"
	var equal bool
	if err := tx.QueryRow(ctx, `SELECT $1::citext OPERATOR(public.=) $2::citext`, ascii, folded).Scan(&equal); err != nil || !equal {
		t.Fatalf("PREMISE: citext does not fold the Kelvin sign onto 'k' (%v, %v)", equal, err)
	}
	insert := func(q pgx.Tx, address, hash string) error {
		_, err := q.Exec(ctx, `INSERT INTO admin_invites (tenant_id, email, full_name, role, token_hash, invited_by, expires_at)
		                       VALUES ($1, $2, 'M14-15 Invitee', 'manager', $3, $4, now() + interval '1 day')`, a, address, hash, by)
		return err
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		if code, _ := opCode(insert(sp, folded, opRandHex(t))); code != "23514" {
			t.Errorf("a non-ASCII invitation address was stored")
		}
	})
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		aiExec(t, ctx, sp, `ALTER TABLE admin_invites DROP CONSTRAINT admin_invites_email_ascii`)
		h := opRandHex(t)
		if err := insert(sp, folded, h); err != nil {
			t.Fatal(err)
		}
		if err := aiApp(t, ctx, sp, uuid.Nil, func(app pgx.Tx) error {
			n, err := aiRows(ctx, app, `SELECT * FROM identity_accept_invite_new($1, $2)`, h, opFakeDigest("n"))
			if err == nil && n != 0 {
				t.Errorf("identity_accept_invite_new answered %d rows for a non-ASCII address", n)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities WHERE email = $1::citext`, folded); n != 0 {
			t.Errorf("an identity was born for a non-ASCII address")
		}
	})
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		b := aiTenant(t, ctx, sp)
		squatter := aiIdentity(t, ctx, sp, folded)
		row := aiAdmin(t, ctx, sp, b, folded)
		aiExec(t, ctx, sp, `UPDATE admin_users SET identity_id = $2, linked_via = 'backfill', linked_at = now(),
		                           password_hash = (SELECT password_hash FROM admin_identities WHERE id = $2) WHERE id = $1`, row, squatter)
		session := idSession(t, ctx, sp, b, row, nil)
		h := opRandHex(t)
		if err := insert(sp, ascii, h); err != nil {
			t.Fatal(err)
		}
		if err := aiApp(t, ctx, sp, uuid.Nil, func(app pgx.Tx) error {
			n, err := aiRows(ctx, app, `SELECT * FROM identity_accept_invite($1, $2)`, h, session)
			if err == nil && n != 0 {
				t.Errorf("identity_accept_invite linked a folded (non-ASCII) identity to the ASCII invitation (%d rows)", n)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_users WHERE tenant_id = $1 AND identity_id = $2`, a, squatter); n != 0 {
			t.Errorf("the squatter became a member of the inviting business")
		}
	})
}
