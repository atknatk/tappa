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
//	(1) the birth: the locked row's tenant and admin, authenticated_at = now(), the token hash written
//	    (its admin.login.succeeded row: internal/handler's identityaudit_db_test.go, byte for byte);
//	(2) every row the function must not open, each 0 rows, no session row and no sign-in row: disabled, refused
//	    (unlinked_at with its CURRENT digest -- the legacy statement relied on the sentinel), a stale or
//	    empty digest, another business's row named under this business's tenant (the tenant predicate),
//	    a tenant that is not the transaction's context, no context, an unknown, empty or NULL purpose (no
//	    silent arm), NULL arguments;
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
// calls (OpenRowSession, the const it sends) as tappa_app in the row's own tenant context, once per
// purpose: the session, and exactly ONE audit row in its business -- the sign-in's admin.login.succeeded
// {outcome, session_id} (byte for byte the handler's: internal/handler's identityaudit_db_test.go) or the
// link proof's admin.session.link_proof {session_id} and NO sign-in row (ADR 0031 §5.6).
func TestIdentityOpenRowSession_BirthsTheSessionWithItsProofNow(t *testing.T) {
	ctx, tx := opTx(t)
	for _, c := range []struct {
		purpose RowSessionPurpose
		action  string
		keys    string
	}{
		{RowSessionSignIn, "admin.login.succeeded", "outcome,session_id"},
		{RowSessionLinkProof, "admin.session.link_proof", "session_id"},
	} {
		t.Run(string(c.purpose), func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				a := aiTenant(t, ctx, sp)
				row := aiAdmin(t, ctx, sp, a, aiEmail())
				h := opRandHex(t)
				var got IdentitySession
				if err := aiApp(t, ctx, sp, a, func(app pgx.Tx) error {
					var e error
					got, e = OpenRowSession(ctx, app, row, a, h, NewPasswordHash(opFakeDigest("a")), c.purpose)
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
				if err := sp.QueryRow(ctx, `SELECT tenant_id, admin_user_id, token_hash, authenticated_at = now(), created_at = now(),
				                                   last_used_at, revoked_at, authenticated_at
				                              FROM admin_sessions WHERE id = $1`, got.SessionID).
					Scan(&tenant, &admin, &hash, &proofIsNow, &bornNow, &lastUsed, &revoked, &proof); err != nil {
					t.Fatalf("the session the function returned: %v", err)
				}
				if tenant != a || admin != row || hash != h || !proofIsNow || !bornNow || lastUsed != nil || revoked != nil || proof == nil {
					t.Fatalf("the session = tenant %s admin %s hash-match %v proof-now %v born-now %v last_used %v revoked %v; want %s, %s, true, true, true, nil, nil",
						tenant, admin, hash == h, proofIsNow, bornNow, lastUsed, revoked, a, row)
				}
				rows, err := sp.Query(ctx, `SELECT action, actor_id, target, (SELECT string_agg(k, ',' ORDER BY k) FROM jsonb_object_keys(detail) AS k),
				                                   detail->>'session_id'
				                              FROM audit_log WHERE tenant_id = $1`, a)
				if err != nil {
					t.Fatal(err)
				}
				type auditRow struct {
					Action  string
					Actor   *uuid.UUID
					Target  *string
					Keys    *string
					Session *string
				}
				trail, err := pgx.CollectRows(rows, pgx.RowToStructByPos[auditRow])
				if err != nil {
					t.Fatal(err)
				}
				if len(trail) != 1 {
					t.Fatalf("the business's trail = %+v; want exactly one row", trail)
				}
				r := trail[0]
				if r.Action != c.action || r.Actor == nil || *r.Actor != row || r.Target == nil || *r.Target != row.String() ||
					r.Keys == nil || *r.Keys != c.keys || r.Session == nil || *r.Session != got.SessionID.String() {
					t.Fatalf("the business's row = %s actor %v target %v keys %v session %v; want %s by and on %s, keys %s, the session",
						r.Action, r.Actor, r.Target, r.Keys, r.Session, c.action, row, c.keys)
				}
			})
		})
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
		purpose       RowSessionPurpose // "" = sign_in, "-" = ""
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
		{name: "an unknown purpose", purpose: "sign_out"},
		{name: "an empty purpose", purpose: "-"},
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
				purpose := RowSessionSignIn
				switch c.purpose {
				case "":
				case "-":
					purpose = ""
				default:
					purpose = c.purpose
				}
				h := opRandHex(t)
				err := aiApp(t, ctx, sp, ctxTenant, func(app pgx.Tx) error {
					_, e := OpenRowSession(ctx, app, admin, tenant, h, NewPasswordHash(digest), purpose)
					return e
				})
				n := rsSessionsByHash(t, ctx, sp, h)
				rows := opInt(t, ctx, sp, `SELECT count(*) FROM audit_log WHERE tenant_id IN ($1, $2) AND action IN ('admin.login.succeeded', 'admin.session.link_proof')`, a, b)
				if c.name == "control: the active row with its current digest" {
					if err != nil || n != 1 || rows != 1 {
						t.Fatalf("control: %v, %d sessions, %d sign-in rows; want one and one", err, n, rows)
					}
					return
				}
				if !errors.Is(err, pgx.ErrNoRows) || n != 0 || rows != 0 {
					t.Fatalf("OpenRowSession = %v, %d sessions with the hash, %d sign-in rows; want pgx.ErrNoRows, none, none", err, n, rows)
				}
			})
		})
	}
	t.Run("NULL arguments", func(t *testing.T) {
		for i, args := range [][]any{{nil, a, opRandHex(t), good, "sign_in"}, {rowA, nil, opRandHex(t), good, "sign_in"}, {rowA, a, nil, good, "sign_in"},
			{rowA, a, opRandHex(t), nil, "sign_in"}, {rowA, a, opRandHex(t), good, nil}} {
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
				if trail := opInt(t, ctx, sp, `SELECT count(*) FROM audit_log WHERE tenant_id = $1`, a); trail != 0 {
					t.Fatalf("NULL argument %d left %d audit rows", i, trail)
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
			_, err := OpenRowSession(ctx, tx, w.rowX, w.x, "t187-"+uuid.NewString(), verified, RowSessionSignIn)
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

// TestIdentityOpenRowSession00086_ExpandsAndItsDownDropsOnlyTheFunction is (4) -- and T187's contract
// reminder (ADR 0036): it pins the INSERT grant the contract must revoke, and its message names the rest
// of the contract. Chosen over "red once a migration above 00086 exists while the grant stands": that
// would turn the next unrelated migration red (00087 is another lane's), coupling work that has nothing
// to do with sessions; the contract's place on the plan is state.md's. On HEAD tappa_app still
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
		t.Fatalf("tappa_app on admin_sessions = %s; 00086 (expand) keeps INSERT %s. If T187's CONTRACT step (ADR 0036, "+
			"Karar 3) revoked it, finish the contract in the same change: delete CreateAdminSession from db/queries, "+
			"move the fixtures that insert sessions as tappa_app to the owner role, drop internal/store's exemption in "+
			"internal/adminauth/rowsession_test.go, and turn this test into \"no INSERT, the old statement is 42501\"", before, want)
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
	// Its sign-in row is its handler's, written after this statement: the database writes none on that
	// path, so an old pod during the rollout leaves one row, not two.
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1`, a); n != 0 {
		t.Fatalf("the previous binary's statement wrote %d audit rows in the database; its handler writes the one", n)
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
