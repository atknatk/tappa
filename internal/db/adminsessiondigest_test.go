package db

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// adminsessiondigest_test.go -- what is left of CreateAdminSession's M14-07 tests after T187's contract
// (00087, ADR 0036) deleted the query: a panel session on a single admin_users row is born in
// identity_open_row_session, and its digest predicate and row lock are adminrowsession_test.go's
// (TestIdentityOpenRowSession_RefusesEveryRowItMustNot, TestIdentityOpenRowSession_WaitsForAnOpenUnlinkAndThenRefuses).
// What stays is the measurement that tappa_app's own FOR NO KEY UPDATE on admin_users -- the membership
// changes' owner lock (db/queries/adminmembers.sql) -- is its to take under FORCE RLS, and the helper the
// row-session tests count live sessions with.

func liveSessionsOf(t *testing.T, ctx context.Context, owner *pgx.Conn, admin uuid.UUID) int {
	t.Helper()
	var n int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM admin_sessions WHERE admin_user_id = $1 AND revoked_at IS NULL`, admin).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestAdminUsersRowLock_IsTappaAppsUnderForceRLS is measured rather than assumed:
// admin_users is FORCE RLS; tappa_app holds no table-level UPDATE and holds UPDATE (status) (00017),
// which is what a row lock needs; the policy that applies to an UPDATE -- and so to the lock -- is
// the tenant isolation policy (FOR ALL); and in tappa_app's own tenant a locking read
// returns the row while from another business's context it returns nothing.
func TestAdminUsersRowLock_IsTappaAppsUnderForceRLS(t *testing.T) {
	ctx, tx := opTx(t)
	var force, tableUpdate, statusUpdate bool
	if err := tx.QueryRow(ctx, `SELECT c.relforcerowsecurity,
	                                   has_table_privilege('tappa_app', c.oid, 'UPDATE'),
	                                   has_column_privilege('tappa_app', c.oid, 'status', 'UPDATE')
	                              FROM pg_class c WHERE c.oid = 'public.admin_users'::regclass`).Scan(&force, &tableUpdate, &statusUpdate); err != nil {
		t.Fatal(err)
	}
	if !force || tableUpdate || !statusUpdate {
		t.Fatalf("admin_users: FORCE RLS %v, tappa_app table UPDATE %v, UPDATE (status) %v; want true, false, true", force, tableUpdate, statusUpdate)
	}
	rows, err := tx.Query(ctx, `SELECT polcmd::text, pg_get_expr(polqual, polrelid) FROM pg_policy
	                             WHERE polrelid = 'public.admin_users'::regclass AND polcmd IN ('*', 'w')`)
	if err != nil {
		t.Fatal(err)
	}
	var policies []string
	for rows.Next() {
		var cmd, qual string
		if err := rows.Scan(&cmd, &qual); err != nil {
			t.Fatal(err)
		}
		policies = append(policies, cmd+" "+qual)
	}
	rows.Close()
	if len(policies) != 1 || !strings.HasPrefix(policies[0], "* ") || !strings.Contains(policies[0], "app.tenant_id") {
		t.Fatalf("the policies an UPDATE (and a row lock) meets on admin_users = %v; want the one tenant isolation policy, FOR ALL", policies)
	}

	a, b := aiTenant(t, ctx, tx), aiTenant(t, ctx, tx)
	row := aiAdmin(t, ctx, tx, a, aiEmail())
	lock := `SELECT a.id FROM admin_users a WHERE a.id = $1 AND a.password_hash = $2 FOR NO KEY UPDATE OF a`
	var got []uuid.UUID
	if err := aiApp(t, ctx, tx, a, func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, lock, row, opFakeDigest("a")).Scan(new(uuid.UUID))
	}); err != nil {
		t.Fatalf("tappa_app's row lock on its own business's row: %v", err)
	}
	if err := aiApp(t, ctx, tx, b, func(sp pgx.Tx) error {
		r, err := sp.Query(ctx, lock, row, opFakeDigest("a"))
		if err != nil {
			return err
		}
		got, err = pgx.CollectRows(r, pgx.RowTo[uuid.UUID])
		return err
	}); err != nil {
		t.Fatalf("the row lock from another business's context: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("from another business's context the lock reached %v", got)
	}
}
