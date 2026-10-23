package db

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// adminsessiondigest_test.go -- CreateAdminSession's two M14-07 additions (ADR 0031 §5.2,
// identity_unlink_backfilled's row; db/queries/admins.sql): the session is written only while the
// row still carries the digest the password verified against (`a.password_hash = @verified_digest`),
// under the row's share lock (`FOR SHARE OF a`). tappa_app cannot read unlinked_at (R16); the
// refusal's sentinel digest is what this statement can see.
//
//	(1) the refusal commits between the comparison and the write -> 0 rows, no session
//	    (mutation: drop the predicate -> red)
//	(2) the refusal's transaction is OPEN when the write starts and commits while it waits
//	    -> 0 rows, no live session (mutation: drop FOR SHARE -> red: the write reads the old row
//	    version, waits only in its foreign-key check, and inserts after the refusal committed)
//	(3) the share lock is tappa_app's to take under FORCE RLS: UPDATE (status) is its only UPDATE on
//	    admin_users and is enough, and the policy that guards an UPDATE (the tenant isolation policy,
//	    FOR ALL) hides another business's row from the lock as from the read.
//
// The fixture is internal/db's ipRaceWorld (identitypassword_test.go): identity I, a linked row in
// T with a fresh session (sA), a membership in X linked 'password' three days ago and listed by an
// open reset window, an unlinked row in Y; every row carries opFakeDigest("d").

// createAdminSessionIn runs CreateAdminSession as tappa_app in tenant's context on the pool.
func createAdminSessionIn(ctx context.Context, d *DB, tenant, admin uuid.UUID, verified string) (store.CreateAdminSessionRow, error) {
	var row store.CreateAdminSessionRow
	err := d.WithTenant(ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		row, e = store.New(tx).CreateAdminSession(ctx, store.CreateAdminSessionParams{
			TokenHash: "m14-07-" + uuid.NewString(), AdminUserID: admin, TenantID: tenant, VerifiedDigest: verified,
		})
		return e
	})
	return row, err
}

func liveSessionsOf(t *testing.T, ctx context.Context, owner *pgx.Conn, admin uuid.UUID) int {
	t.Helper()
	var n int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM admin_sessions WHERE admin_user_id = $1 AND revoked_at IS NULL`, admin).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestCreateAdminSession_RefusesARowWhoseDigestChangedSinceItVerified is test (1): the membership
// X is refused (identity_unlink_backfilled, committed) after its password verified; the session
// write with the digest that verified answers 0 rows and leaves nothing. A password change on an
// unlinked row refuses the old digest the same way, and the row's current digest opens (control).
func TestCreateAdminSession_RefusesARowWhoseDigestChangedSinceItVerified(t *testing.T) {
	d := appDB(t)
	ctx := context.Background()
	owner, err := pgx.Connect(ctx, opOwnerDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	w := ipNewRaceWorld(t, ctx, owner)
	verified := opFakeDigest("d")

	// Control: before anything changes, the verified digest opens X.
	if _, err := createAdminSessionIn(ctx, d, w.x, w.rowX, verified); err != nil {
		t.Fatalf("control: the verified digest on X before the refusal: %v", err)
	}
	var unlinked uuid.UUID
	if err := d.pool.QueryRow(ctx, `SELECT admin_user_id FROM identity_unlink_backfilled($1, $2)`, w.sA, w.x).Scan(&unlinked); err != nil || unlinked != w.rowX {
		t.Fatalf("control: the unlink of X = %s, %v", unlinked, err)
	}
	if _, err := createAdminSessionIn(ctx, d, w.x, w.rowX, verified); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("CreateAdminSession on the refused row with the digest that verified: %v, want pgx.ErrNoRows", err)
	}
	if n := liveSessionsOf(t, ctx, owner, w.rowX); n != 0 {
		t.Fatalf("%d live sessions on the refused row", n)
	}

	// A password change between the comparison and the write (an unlinked row, tappa_app's own path).
	newDigest := opFakeDigest("e")
	if err := d.WithTenant(ctx, w.y, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).SetOwnAdminPassword(ctx, store.SetOwnAdminPasswordParams{ID: w.rowY, TenantID: w.y, PasswordHash: newDigest})
		return e
	}); err != nil {
		t.Fatal(err)
	}
	before := liveSessionsOf(t, ctx, owner, w.rowY)
	if _, err := createAdminSessionIn(ctx, d, w.y, w.rowY, verified); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("CreateAdminSession with the digest before a password change: %v, want pgx.ErrNoRows", err)
	}
	if after := liveSessionsOf(t, ctx, owner, w.rowY); after != before {
		t.Fatalf("the refused write left %d sessions", after-before)
	}
	if _, err := createAdminSessionIn(ctx, d, w.y, w.rowY, newDigest); err != nil {
		t.Fatalf("control: the row's current digest: %v", err)
	}
}

// TestCreateAdminSession_WaitsForAnOpenUnlinkAndThenRefuses is test (2), the interleaving the
// share lock exists for: the refusal's transaction is open (X locked, its sessions revoked,
// uncommitted) when the session write starts with the digest that verified; the write is SEEN
// waiting on a lock before the refusal commits (pg_stat_activity, by application_name -- the
// interleaving is real, not lucky); after the commit it answers 0 rows and X has no live session.
func TestCreateAdminSession_WaitsForAnOpenUnlinkAndThenRefuses(t *testing.T) {
	appDB(t) // the skip when there is no database
	appDSN := os.Getenv("DATABASE_URL")
	ctx := context.Background()
	owner, err := pgx.Connect(ctx, opOwnerDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	w := ipNewRaceWorld(t, ctx, owner)
	verified := opFakeDigest("d")

	u, err := pgx.Connect(ctx, appDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = u.Close(context.Background()) })
	txU, err := u.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = txU.Rollback(context.Background()) }()
	var unlinked uuid.UUID
	if err := txU.QueryRow(ctx, `SELECT admin_user_id FROM identity_unlink_backfilled($1, $2)`, w.sA, w.x).Scan(&unlinked); err != nil || unlinked != w.rowX {
		t.Fatalf("control: the open unlink of X = %s, %v", unlinked, err)
	}

	marker := "m14-07-share-" + uuid.NewString()[:8]
	cfg, err := pgx.ParseConfig(appDSN)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RuntimeParams["application_name"] = marker
	b, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close(context.Background()) })
	done := make(chan error, 1)
	go func() {
		done <- pgx.BeginFunc(ctx, b, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", w.x.String()); err != nil {
				return err
			}
			_, err := store.New(tx).CreateAdminSession(ctx, store.CreateAdminSessionParams{
				TokenHash: "m14-07-" + uuid.NewString(), AdminUserID: w.rowX, TenantID: w.x, VerifiedDigest: verified,
			})
			return err
		})
	}()
	waiting := false
	for i := 0; i < 100 && !waiting; i++ {
		select {
		case err := <-done:
			t.Fatalf("the session write answered (%v) while the unlink held X", err)
		case <-time.After(50 * time.Millisecond):
		}
		if err := owner.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE application_name = $1 AND wait_event_type = 'Lock')`, marker).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
	}
	if !waiting {
		t.Fatal("the session write is neither answered nor seen waiting on a lock after 5 s")
	}
	if err := txU.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("the session write after the unlink committed: %v, want pgx.ErrNoRows", err)
	}
	if n := liveSessionsOf(t, ctx, owner, w.rowX); n != 0 {
		t.Fatalf("%d live sessions on X after the refusal committed -- the write read the row before it", n)
	}
}

// TestCreateAdminSession_TheShareLockIsTappaAppsUnderForceRLS is (3), measured rather than assumed:
// admin_users is FORCE RLS; tappa_app holds no table-level UPDATE and holds UPDATE (status) (00017),
// which is what a row lock needs; the policy that applies to an UPDATE -- and so to the lock -- is
// the tenant isolation policy (FOR ALL); and in tappa_app's own tenant the statement's locking read
// returns the row while from another business's context it returns nothing.
func TestCreateAdminSession_TheShareLockIsTappaAppsUnderForceRLS(t *testing.T) {
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
	lock := `SELECT a.id FROM admin_users a WHERE a.id = $1 AND a.password_hash = $2 FOR SHARE OF a`
	var got []uuid.UUID
	if err := aiApp(t, ctx, tx, a, func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, lock, row, opFakeDigest("a")).Scan(new(uuid.UUID))
	}); err != nil {
		t.Fatalf("tappa_app's share lock on its own business's row: %v", err)
	}
	if err := aiApp(t, ctx, tx, b, func(sp pgx.Tx) error {
		r, err := sp.Query(ctx, lock, row, opFakeDigest("a"))
		if err != nil {
			return err
		}
		got, err = pgx.CollectRows(r, pgx.RowTo[uuid.UUID])
		return err
	}); err != nil {
		t.Fatalf("the share lock from another business's context: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("from another business's context the lock reached %v", got)
	}
}
