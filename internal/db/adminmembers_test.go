package db

// adminmembers_test.go -- M14-16's two obligations toward the person's own unlink (ADR 0031 §5.2, the
// card's ONAY note 4), in the owner's transaction (opTx, rolled back), the panel's writes run as
// tappa_app through the very sqlc statements internal/adminauth.Memberships calls:
//
//   - the last-owner invariant is the PANEL's rule and does not stand in tappa_identitydefiner's way:
//     the person may unlink a membership that is its business's last active owner, before and after
//     contract B ("the same warning, allow it");
//   - the panel can flip a refused row back to 'active' (it cannot read unlinked_at -- R16), and that
//     does not reopen it: every path that GIVES a session filters unlinked_at IS NULL.

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// amActiveOwners counts a business's active owners as the panel's invariant counts them.
func amActiveOwners(t *testing.T, ctx context.Context, tx pgx.Tx, tenant uuid.UUID) int64 {
	t.Helper()
	return opInt(t, ctx, tx, `SELECT count(*) FROM admin_users WHERE tenant_id = $1 AND role = 'owner' AND status = 'active'`, tenant)
}

// amContractB stands in for contract B's VALID CHECK (a row born after the contract moment keeps
// identity_id), as TestIdentityUnlink_AfterContractBItDisablesTheMembership does.
func amContractB(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	var moment string
	if err := tx.QueryRow(ctx, `SELECT now()::text`).Scan(&moment); err != nil {
		t.Fatal(err)
	}
	aiExec(t, ctx, tx, `ALTER TABLE admin_users ADD CONSTRAINT zz_m14_16_contract_b
	                    CHECK (identity_id IS NOT NULL OR created_at < '`+moment+`'::timestamptz) NOT VALID`)
}

// TestAdminMembers_TheDefinersUnlinkIsNotStoppedByTheLastOwnerRule: the person's membership is its
// business's ONLY owner; the unlink goes through both before contract B (the row unlinked, still
// 'active' -- the business keeps a refused owner the contract precondition counts apart, §5.12) and
// after (the row disabled: the business has NO active owner left, and that is allowed -- the person's
// own decision). And no trigger on admin_users other than 00069's credential guard exists: an
// invariant trigger added later must come with its definer exception and this list.
func TestAdminMembers_TheDefinersUnlinkIsNotStoppedByTheLastOwnerRule(t *testing.T) {
	ctx, tx := opTx(t)
	var triggers []string
	rows, err := tx.Query(ctx, `SELECT tgname FROM pg_trigger WHERE tgrelid = 'public.admin_users'::regclass AND NOT tgisinternal ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	triggers, err = pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if len(triggers) != 1 || triggers[0] != "admin_users_identity_credentials" {
		t.Fatalf("admin_users carries the triggers %v, want only admin_users_identity_credentials -- a last-owner "+
			"trigger must not stop tappa_identitydefiner's unlink (ADR 0031 §5.2, M14-16)", triggers)
	}

	for _, contract := range []bool{false, true} {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			w := ipNewUnlinkWorld(t, ctx, sp)
			x, row := w.member(t, ctx, sp, "backfill", "2 days")
			if n := amActiveOwners(t, ctx, sp, x); n != 1 {
				t.Fatalf("CONTROL: %d active owners in the business, want the person's one", n)
			}
			ipOpenWindow(t, ctx, sp, w.v.id, "first_verify")
			if contract {
				amContractB(t, ctx, sp)
			}
			if n := ipUnlink(t, ctx, sp, w.sH, x); n != 1 {
				t.Fatalf("contract %v: the unlink of the business's last owner = %d rows, want 1", contract, n)
			}
			if opInt(t, ctx, sp, `SELECT count(*) FROM admin_users WHERE id = $1 AND unlinked_at IS NOT NULL`, row) != 1 {
				t.Fatalf("contract %v: the row is not stamped unlinked_at", contract)
			}
			want := int64(1) // before contract B the refused owner row stays 'active' (identity_id NULL, the sentinel)
			if contract {
				want = 0
			}
			if n := amActiveOwners(t, ctx, sp, x); n != want {
				t.Errorf("contract %v: %d active owners after the unlink, want %d", contract, n, want)
			}
		})
	}
}

// TestAdminMembers_EnablingARefusedRowGivesNoSession: the person refused a membership (after contract
// B: link kept, unlinked_at stamped, 'disabled'; before: unlinked, the sentinel, still 'active'); the
// business's other owner presses Disable and Enable on the panel -- the very statements the panel runs,
// as tappa_app in that business -- and the row ends 'active'. Still: the person's membership list does
// not offer it, a switch into it is 0 rows, a sign-in asking for it lands elsewhere, and the legacy
// resolver does not return it. The panel cannot REOPEN a refused row.
func TestAdminMembers_EnablingARefusedRowGivesNoSession(t *testing.T) {
	ctx, tx := opTx(t)
	for _, contract := range []bool{false, true} {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			w := ipNewUnlinkWorld(t, ctx, sp)
			x, row := w.member(t, ctx, sp, "backfill", "2 days")
			// The business's other owner -- the one who presses on the panel.
			ipMember(t, ctx, sp, x, ipEmail(), w.v.digest)
			listed := func() bool {
				for _, m := range mustRows(t, ctx, sp, `SELECT tenant_id::text FROM identity_memberships($1)`, w.sH) {
					if m[0] == x.String() {
						return true
					}
				}
				return false
			}
			if !listed() {
				t.Fatalf("contract %v: CONTROL -- before the unlink the person's list does not offer the business", contract)
			}
			ipOpenWindow(t, ctx, sp, w.v.id, "first_verify")
			if contract {
				amContractB(t, ctx, sp)
			}
			if n := ipUnlink(t, ctx, sp, w.sH, x); n != 1 {
				t.Fatalf("contract %v: the unlink = %d rows, want 1", contract, n)
			}
			// The panel's two presses, as the panel runs them.
			for _, status := range []string{"disabled", "active"} {
				if err := aiApp(t, ctx, sp, x, func(app pgx.Tx) error {
					_, err := store.New(app).SetAdminMemberStatus(ctx, store.SetAdminMemberStatusParams{Status: status, TenantID: x, ID: row})
					if errors.Is(err, pgx.ErrNoRows) {
						return nil // already that status
					}
					return err
				}); err != nil {
					t.Fatalf("contract %v: the panel setting %s: %v", contract, status, err)
				}
			}
			if opInt(t, ctx, sp, `SELECT count(*) FROM admin_users WHERE id = $1 AND status = 'active' AND unlinked_at IS NOT NULL`, row) != 1 {
				t.Fatalf("contract %v: CONTROL -- the panel did not leave the refused row 'active'", contract)
			}

			if listed() {
				t.Errorf("contract %v: the person's membership list offers the refused business", contract)
			}
			if n := ipRows(t, ctx, sp, `SELECT * FROM identity_switch_session($1, $2, $3)`, w.sH, x, opRandHex(t)); n != 0 {
				t.Errorf("contract %v: a switch into the refused business = %d rows, want 0", contract, n)
			}
			for _, s := range mustRows(t, ctx, sp, `SELECT tenant_id::text FROM identity_open_session($1, $2, $3, $4)`, w.v.id, x, opRandHex(t), w.v.digest) {
				if s[0] == x.String() {
					t.Errorf("contract %v: a sign-in asking for the refused business landed in it", contract)
				}
			}
			if n := ipRows(t, ctx, sp, `SELECT * FROM resolve_admin_by_email($1::citext) WHERE tenant_id = $2`, w.v.email, x); n != 0 {
				t.Errorf("contract %v: the legacy resolver returns the refused row", contract)
			}
		})
	}
}

// mustRows is ipApp that fails the test on an error.
func mustRows(t *testing.T, ctx context.Context, tx pgx.Tx, sql string, args ...any) [][]any {
	t.Helper()
	rows, err := ipApp(t, ctx, tx, sql, args...)
	if err != nil {
		t.Fatalf("tappa_app: %s: %v", sql, err)
	}
	return rows
}
