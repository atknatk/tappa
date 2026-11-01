package db

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// adminrowsession_test.go -- T187, ADR 0036, migration 00086: identity_open_row_session, the one place a
// panel session on a single admin_users row is born (tappa_app's own INSERT is the contract step's to
// revoke). Against real Postgres:
//
//	(1) the birth: the locked row's tenant and admin, authenticated_at = now(), the token hash written;
//	(2) every row the function must not open, each 0 rows and no session row: disabled, refused
//	    (unlinked_at with its CURRENT digest -- the legacy statement relied on the sentinel), a stale or
//	    empty digest, another business's row named under this business's tenant (the tenant predicate),
//	    a tenant that is not the transaction's context, no context, NULL arguments;
//	(3) the refusal that is OPEN when the birth starts is waited for and then refuses (the row lock);
//	(4) expand: tappa_app still holds its INSERT and the previous binary's statement still opens on this
//	    schema; the Down drops the function and nothing else.
//
// The catalogue half (signature, owner, search path, EXECUTE, belt, every grant used) is
// identitycatalog_test.go's and identitybelt_test.go's, which carry the function's row.

const rowSessionFile = "00086_open_row_sessions_in_the_definer.sql"

// rsSessionsByHash counts admin_sessions rows carrying hash, as whoever q is (the owner sees all).
func rsSessionsByHash(t *testing.T, ctx context.Context, q opQuerier, hash string) int64 {
	t.Helper()
	return opInt(t, ctx, q, `SELECT count(*) FROM admin_sessions WHERE token_hash = $1`, hash)
}

// TestIdentityOpenRowSession_BirthsTheSessionWithItsProofNow is (1), through the Go accessor the binary
// calls (OpenRowSession, the const it sends) as tappa_app in the row's own tenant context.
func TestIdentityOpenRowSession_BirthsTheSessionWithItsProofNow(t *testing.T) {
	ctx, tx := opTx(t)
	a := aiTenant(t, ctx, tx)
	row := aiAdmin(t, ctx, tx, a, aiEmail())
	h := opRandHex(t)
	var got IdentitySession
	if err := aiApp(t, ctx, tx, a, func(sp pgx.Tx) error {
		var e error
		got, e = OpenRowSession(ctx, sp, row, a, h, NewPasswordHash(opFakeDigest("a")))
		return e
	}); err != nil {
		t.Fatalf("OpenRowSession on an active row with its current digest: %v", err)
	}
	if got.TenantID != a || got.AdminUserID != row || got.SessionID == uuid.Nil {
		t.Fatalf("OpenRowSession = %+v; want a session of %s in %s", got, row, a)
	}
	var (
		tenant, admin            uuid.UUID
		hash                     string
		proofIsNow, bornNow      bool
		lastUsed, revoked, proof *time.Time
	)
	if err := tx.QueryRow(ctx, `SELECT tenant_id, admin_user_id, token_hash, authenticated_at = now(), created_at = now(),
	                                   last_used_at, revoked_at, authenticated_at
	                              FROM admin_sessions WHERE id = $1`, got.SessionID).
		Scan(&tenant, &admin, &hash, &proofIsNow, &bornNow, &lastUsed, &revoked, &proof); err != nil {
		t.Fatalf("the session the function returned: %v", err)
	}
	if tenant != a || admin != row || hash != h || !proofIsNow || !bornNow || lastUsed != nil || revoked != nil || proof == nil {
		t.Fatalf("the session = tenant %s admin %s hash-match %v proof-now %v born-now %v last_used %v revoked %v; want %s, %s, true, true, true, nil, nil",
			tenant, admin, hash == h, proofIsNow, bornNow, lastUsed, revoked, a, row)
	}
}

// TestIdentityOpenRowSession_RefusesEveryRowItMustNot is (2). Each case is a savepoint of its own; the
// call answers 0 rows (pgx.ErrNoRows through OpenRowSession, or an empty set for a NULL argument) and
// no row with the case's hash exists afterwards -- as the owner, who sees every business. The control
// is the same call on the untouched row.
func TestIdentityOpenRowSession_RefusesEveryRowItMustNot(t *testing.T) {
	ctx, tx := opTx(t)
	a, b := aiTenant(t, ctx, tx), aiTenant(t, ctx, tx)
	rowA := aiAdmin(t, ctx, tx, a, aiEmail())
	rowB := aiAdmin(t, ctx, tx, b, aiEmail())
	good := opFakeDigest("a")
	cases := []struct {
		name          string
		setup         string // as the owner, before the call; $1 = rowA
		ctxTenant     uuid.UUID
		admin, tenant uuid.UUID
		digest        string
	}{
		{name: "control: the active row with its current digest"},
		{name: "a disabled membership", setup: `UPDATE admin_users SET status = 'disabled' WHERE id = $1`},
		{name: "a refused membership with its current digest", setup: `UPDATE admin_users SET unlinked_at = now() WHERE id = $1`},
		{name: "a stale digest", digest: opFakeDigest("b")},
		{name: "an empty digest", digest: "-"},
		{name: "another business's row named under this business's tenant", admin: rowB},
		{name: "another business's row in its own tenant from this business's context", admin: rowB, tenant: b},
		{name: "this business's row from another business's context", ctxTenant: b},
		{name: "no tenant context", ctxTenant: uuid.Max},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				if c.setup != "" {
					aiExec(t, ctx, sp, c.setup, rowA)
				}
				admin, tenant, ctxTenant, digest := rowA, a, a, good
				if c.admin != uuid.Nil {
					admin = c.admin
				}
				if c.tenant != uuid.Nil {
					tenant = c.tenant
				}
				switch c.ctxTenant {
				case uuid.Nil:
				case uuid.Max:
					ctxTenant = uuid.Nil // aiApp: an empty app.tenant_id
				default:
					ctxTenant = c.ctxTenant
				}
				switch c.digest {
				case "":
				case "-":
					digest = ""
				default:
					digest = c.digest
				}
				h := opRandHex(t)
				err := aiApp(t, ctx, sp, ctxTenant, func(app pgx.Tx) error {
					_, e := OpenRowSession(ctx, app, admin, tenant, h, NewPasswordHash(digest))
					return e
				})
				n := rsSessionsByHash(t, ctx, sp, h)
				if c.name == "control: the active row with its current digest" {
					if err != nil || n != 1 {
						t.Fatalf("control: %v, %d sessions; want one", err, n)
					}
					return
				}
				if !errors.Is(err, pgx.ErrNoRows) || n != 0 {
					t.Fatalf("OpenRowSession = %v and %d sessions with the hash; want pgx.ErrNoRows and none", err, n)
				}
			})
		})
	}
	t.Run("NULL arguments", func(t *testing.T) {
		for i, args := range [][]any{{nil, a, opRandHex(t), good}, {rowA, nil, opRandHex(t), good}, {rowA, a, nil, good}, {rowA, a, opRandHex(t), nil}} {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				var n int
				if err := aiApp(t, ctx, sp, a, func(app pgx.Tx) error {
					rows, e := app.Query(ctx, openRowSessionSQL, args...)
					if e != nil {
						return e
					}
					got, e := pgx.CollectRows(rows, pgx.RowToMap)
					n = len(got)
					return e
				}); err != nil || n != 0 {
					t.Fatalf("NULL argument %d: %v, %d rows; want none", i, err, n)
				}
				if total := opInt(t, ctx, sp, `SELECT count(*) FROM admin_sessions WHERE admin_user_id = $1`, rowA); total != 0 {
					t.Fatalf("NULL argument %d left %d sessions on the row", i, total)
				}
			})
		}
	})
}

// TestIdentityOpenRowSession_WaitsForAnOpenUnlinkAndThenRefuses is (3): the refusal's transaction is
// open (X locked, uncommitted) when the birth starts with the digest that verified; the birth is SEEN
// waiting on a lock before the refusal commits (pg_stat_activity, by application_name); after the
// commit it answers 0 rows and X has no live session. The fixture is ipRaceWorld (identitypassword_test.go).
func TestIdentityOpenRowSession_WaitsForAnOpenUnlinkAndThenRefuses(t *testing.T) {
	appDB(t) // the skip when there is no database
	appDSN := os.Getenv("DATABASE_URL")
	ctx := context.Background()
	owner, err := pgx.Connect(ctx, opOwnerDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	w := ipNewRaceWorld(t, ctx, owner)
	verified := NewPasswordHash(opFakeDigest("d"))

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

	marker := "t187-wait-" + uuid.NewString()[:8]
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
			_, err := OpenRowSession(ctx, tx, w.rowX, w.x, "t187-"+uuid.NewString(), verified)
			return err
		})
	}()
	waiting := false
	for i := 0; i < 100 && !waiting; i++ {
		select {
		case err := <-done:
			t.Fatalf("the birth answered (%v) while the unlink held X", err)
		case <-time.After(50 * time.Millisecond):
		}
		if err := owner.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE application_name = $1 AND wait_event_type = 'Lock')`, marker).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
	}
	if !waiting {
		t.Fatal("the birth is neither answered nor seen waiting on a lock after 5 s")
	}
	if err := txU.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("the birth after the unlink committed: %v, want pgx.ErrNoRows", err)
	}
	if n := liveSessionsOf(t, ctx, owner, w.rowX); n != 0 {
		t.Fatalf("%d live sessions on X after the refusal committed -- the birth read the row before it", n)
	}
}

// TestIdentityOpenRowSession00086_ExpandsAndItsDownDropsOnlyTheFunction is (4). On HEAD tappa_app still
// holds INSERT (id, tenant_id, admin_user_id, token_hash) on admin_sessions and the previous binary's
// statement (CreateAdminSession, the generated text) opens a session -- 00086 is expand-only; the
// REVOKE is ADR 0036's contract step and this test changes with it. The Down drops the function, leaves
// tappa_app's grants on admin_sessions as they were, and the Up gives the catalogue back.
func TestIdentityOpenRowSession00086_ExpandsAndItsDownDropsOnlyTheFunction(t *testing.T) {
	ctx, tx := opTx(t)
	const appGrants = `SELECT string_agg(privilege_type || ' ' || column_name, ',' ORDER BY privilege_type, column_name)
	                     FROM information_schema.column_privileges
	                    WHERE table_schema = 'public' AND table_name = 'admin_sessions' AND grantee = 'tappa_app'`
	var before string
	if err := tx.QueryRow(ctx, appGrants).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if want := "INSERT admin_user_id,INSERT id,INSERT tenant_id,INSERT token_hash"; len(before) < len(want) || before[:len(want)] != want {
		t.Fatalf("tappa_app on admin_sessions = %s; the expand keeps INSERT %s", before, want)
	}
	a := aiTenant(t, ctx, tx)
	row := aiAdmin(t, ctx, tx, a, aiEmail())
	if err := aiApp(t, ctx, tx, a, func(sp pgx.Tx) error {
		_, e := store.New(sp).CreateAdminSession(ctx, store.CreateAdminSessionParams{
			TokenHash: opRandHex(t), AdminUserID: row, TenantID: a, VerifiedDigest: opFakeDigest("a"),
		})
		return e
	}); err != nil {
		t.Fatalf("the previous binary's statement on 00086: %v", err)
	}

	up, down := opMigrationSections(t, rowSessionFile)
	if tables := downTables(up + down); len(tables) > 0 {
		t.Fatalf("00086 names a table lock: %v", tables)
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		opRunSection(t, ctx, sp, down, "00086 Down")
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM pg_proc WHERE proname = 'identity_open_row_session'`); n != 0 {
			t.Fatalf("after the Down %d functions named identity_open_row_session", n)
		}
		var after string
		if err := sp.QueryRow(ctx, appGrants).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if after != before {
			t.Fatalf("the Down changed tappa_app's grants on admin_sessions: %s -> %s", before, after)
		}
		opRunSection(t, ctx, sp, up, "00086 Up")
		findings, err := identityCatalogFindings(ctx, sp, identitySignatures)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) > 0 {
			t.Fatalf("Down then Up: %v", findings)
		}
	})
}
