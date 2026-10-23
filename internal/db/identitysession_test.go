package db

// identitysession_test.go -- M14-04 (ADR 0031 §5.2, §5.4, §5.6): the identity resolver and the four
// session functions of migration 00071 against real Postgres, called AS tappa_app through the very
// SQL the hand-written callers in identity.go send. Everything but the concurrency test runs in
// opTx's transaction and is rolled back (00069's Down refuses while an identity exists, so nothing
// here may leave one behind); the concurrency test needs committed rows and takes them back itself.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atknatk/tappa/internal/store"
)

// idWorld is one person with two memberships (A owner, B manager) linked to one identity, another
// person with a membership in C, and an unlinked admin in A.
type idWorld struct {
	a, b, c              uuid.UUID
	ident, other         uuid.UUID
	email                string
	ma, mb, mc, unlinked uuid.UUID
}

func idNewWorld(t *testing.T, ctx context.Context, q pgx.Tx) idWorld {
	t.Helper()
	w := idWorld{a: idTenant(t, ctx, q, "Alpha"), b: idTenant(t, ctx, q, "Bravo"), c: idTenant(t, ctx, q, "Charlie"), email: aiEmail()}
	w.ident = aiIdentity(t, ctx, q, w.email)
	w.ma, w.mb = aiAdmin(t, ctx, q, w.a, w.email), aiAdmin(t, ctx, q, w.b, w.email)
	aiExec(t, ctx, q, `UPDATE admin_users SET role = 'manager' WHERE id = $1`, w.mb)
	aiLink(t, ctx, q, w.ma, w.ident)
	aiLink(t, ctx, q, w.mb, w.ident)
	otherEmail := aiEmail()
	w.other = aiIdentity(t, ctx, q, otherEmail)
	w.mc = aiAdmin(t, ctx, q, w.c, otherEmail)
	aiLink(t, ctx, q, w.mc, w.other)
	w.unlinked = aiAdmin(t, ctx, q, w.a, aiEmail())
	return w
}

// idAs runs one call as tappa_app (no tenant context: these functions produce it) and collects every
// row as text-free values.
func idAs(t *testing.T, ctx context.Context, tx pgx.Tx, sql string, args ...any) ([][]any, error) {
	t.Helper()
	var out [][]any
	err := opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		rows, err := sp.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) ([]any, error) { return r.Values() })
		return err
	})
	return out, err
}

// idSwitch calls identity_switch_session as tappa_app and returns its rows.
func idSwitch(t *testing.T, ctx context.Context, tx pgx.Tx, hash string, target uuid.UUID, newHash string) [][]any {
	t.Helper()
	rows, err := idAs(t, ctx, tx, switchIdentitySessionSQL, hash, target, newHash)
	if err != nil {
		t.Fatalf("identity_switch_session: %v", err)
	}
	return rows
}

func idCount(t *testing.T, ctx context.Context, tx pgx.Tx, sql string, args ...any) int64 {
	t.Helper()
	return opInt(t, ctx, tx, sql, args...)
}

// ------------------------------------------------------------- the resolver --

// TestResolveIdentityByEmail_OneRowWritesNothingAndSaysWhetherAMembershipIsActive: an address in
// any letter case resolves to its ONE identity (citext + OPERATOR(public.=)), the digest and the
// mailbox stamp come back, has_active_membership is true with an active membership and false when
// every membership is disabled or refused (R14) or there is none; an unknown address is 0 rows; and
// the call WRITES NOTHING -- in a fresh transaction it assigns no transaction id (a write would).
func TestResolveIdentityByEmail_OneRowWritesNothingAndSaysWhetherAMembershipIsActive(t *testing.T) {
	ctx, tx := opTx(t)
	w := idNewWorld(t, ctx, tx)
	active := func(email string) (bool, int) {
		rows, err := idAs(t, ctx, tx, resolveIdentityByEmailSQL, email)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if len(rows) != 1 {
			return false, len(rows)
		}
		if uuid.UUID(rows[0][0].([16]byte)) != w.ident {
			t.Fatalf("resolved %v, want the identity %v", rows[0][0], w.ident)
		}
		return rows[0][3].(bool), 1
	}
	if ok, n := active(strings.ToUpper(w.email)); n != 1 || !ok {
		t.Fatalf("an upper-case spelling: %d rows, active=%v; want 1 row, active", n, ok)
	}
	aiExec(t, ctx, tx, `UPDATE admin_users SET status = 'disabled' WHERE id = $1`, w.ma)
	if ok, _ := active(w.email); !ok {
		t.Fatal("one membership left active: has_active_membership false")
	}
	aiExec(t, ctx, tx, `UPDATE admin_users SET unlinked_at = now() WHERE id = $1`, w.mb)
	if ok, n := active(w.email); n != 1 || ok {
		t.Fatalf("every membership disabled or refused: %d rows, active=%v; want 1 row, inactive (R14)", n, ok)
	}
	if rows, err := idAs(t, ctx, tx, resolveIdentityByEmailSQL, aiEmail()); err != nil || len(rows) != 0 {
		t.Fatalf("an unknown address: %d rows, %v; want 0", len(rows), err)
	}
	if v := opInt(t, ctx, tx, `SELECT count(*) FROM pg_proc WHERE oid = 'public.resolve_identity_by_email(citext)'::regprocedure
	                            AND provolatile = 's' AND prolang = (SELECT oid FROM pg_language WHERE lanname = 'sql')`); v != 1 {
		t.Fatal("resolve_identity_by_email is not a STABLE sql function (it could write)")
	}

	// No write: a committed-free probe on its own connection, as tappa_app.
	app := appDB(t)
	err := app.pool.AcquireFunc(ctx, func(c *pgxpool.Conn) error {
		ptx, err := c.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = ptx.Rollback(ctx) }()
		if _, err := ptx.Exec(ctx, resolveIdentityByEmailSQL, aiEmail()); err != nil {
			return err
		}
		var xid *int64
		if err := ptx.QueryRow(ctx, `SELECT pg_current_xact_id_if_assigned()::text::bigint`).Scan(&xid); err != nil {
			return err
		}
		if xid != nil {
			return fmt.Errorf("the resolver's transaction was assigned xid %d: it wrote", *xid)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// ------------------------------------------------------------- open session --

// TestIdentityOpenSession_LandsRequestedThenLastUsedThenOldest (K-4): the requested business when it
// is an active membership; otherwise the one used last; otherwise the oldest active membership; a
// requested business that is not a membership falls through. Each session is stamped
// authenticated_at = now(), the membership and the identity get last_login_at, last_used_tenant
// follows, and the tenant gets ONE ordinary login row. No active membership, or no identity: 0 rows
// and nothing written.
func TestIdentityOpenSession_LandsRequestedThenLastUsedThenOldest(t *testing.T) {
	ctx, tx := opTx(t)
	w := idNewWorld(t, ctx, tx)
	aiExec(t, ctx, tx, `UPDATE admin_users SET created_at = created_at - interval '1 day' WHERE id = $1`, w.mb)
	open := func(tenant any) (uuid.UUID, uuid.UUID, uuid.UUID, int) {
		rows, err := idAs(t, ctx, tx, openIdentitySessionSQL, w.ident, tenant, opRandHex(t))
		if err != nil {
			t.Fatalf("identity_open_session: %v", err)
		}
		if len(rows) == 0 {
			return uuid.Nil, uuid.Nil, uuid.Nil, 0
		}
		return uuid.UUID(rows[0][0].([16]byte)), uuid.UUID(rows[0][1].([16]byte)), uuid.UUID(rows[0][2].([16]byte)), len(rows)
	}

	if _, tenant, admin, _ := open(nil); tenant != w.b || admin != w.mb {
		t.Fatalf("no request, no last used: landed in %v as %v, want the OLDEST membership B", tenant, admin)
	}
	if _, tenant, _, _ := open(w.a); tenant != w.a {
		t.Fatalf("requested A: landed in %v", tenant)
	}
	if _, tenant, _, _ := open(nil); tenant != w.a {
		t.Fatalf("no request after A: landed in %v, want the last used (A)", tenant)
	}
	if _, tenant, _, _ := open(w.c); tenant != w.a {
		t.Fatalf("requested C (another person's business): landed in %v, want the fallback A", tenant)
	}
	session, tenant, admin, _ := open(w.b)
	if tenant != w.b {
		t.Fatalf("requested B: landed in %v", tenant)
	}
	var fresh, stamped, identStamped bool
	var last uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT s.authenticated_at = now() AND s.tenant_id = $2 AND s.admin_user_id = $3,
	                                   (SELECT last_login_at = now() FROM admin_users WHERE id = $3),
	                                   (SELECT last_login_at = now() FROM admin_identities WHERE id = $4),
	                                   (SELECT last_used_tenant FROM admin_identities WHERE id = $4)
	                              FROM admin_sessions s WHERE s.id = $1`, session, w.b, admin, w.ident).Scan(&fresh, &stamped, &identStamped, &last); err != nil {
		t.Fatal(err)
	}
	if !fresh || !stamped || !identStamped || last != w.b {
		t.Fatalf("the session's proof and the stamps: authenticated_at now=%v, membership stamped=%v, identity stamped=%v, last used=%v", fresh, stamped, identStamped, last)
	}
	if n := idCount(t, ctx, tx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = 'admin.login.succeeded'
	                              AND actor_id = $2 AND target = $2::text AND detail = jsonb_build_object('outcome', 'ok', 'session_id', $3::text)`, w.b, admin, session); n != 1 {
		t.Fatalf("B's login rows for the session: %d, want 1", n)
	}

	aiExec(t, ctx, tx, `UPDATE admin_users SET status = 'disabled' WHERE id IN ($1, $2)`, w.ma, w.mb)
	before := idCount(t, ctx, tx, `SELECT count(*) FROM admin_sessions`)
	if _, _, _, n := open(nil); n != 0 {
		t.Fatal("every membership disabled: a session was opened")
	}
	if rows, err := idAs(t, ctx, tx, openIdentitySessionSQL, uuid.New(), nil, opRandHex(t)); err != nil || len(rows) != 0 {
		t.Fatalf("an identity that does not exist: %d rows, %v", len(rows), err)
	}
	if after := idCount(t, ctx, tx, `SELECT count(*) FROM admin_sessions`); after != before {
		t.Fatalf("a refused open wrote %d session(s)", after-before)
	}
}

// ------------------------------------------------------------ memberships --

// TestIdentityMemberships_TheSessionsPersonOnlyAndAtMostFifty: the list is the session's person's
// active, not-refused memberships, current first, with the business's name, role and suspension; no
// input lists another person's (the other identity's business C never appears -- whichever hash is
// passed; an unknown or revoked hash lists nothing); an unlinked row's session lists itself alone;
// and 55 memberships list 50, the current one among them.
func TestIdentityMemberships_TheSessionsPersonOnlyAndAtMostFifty(t *testing.T) {
	ctx, tx := opTx(t)
	w := idNewWorld(t, ctx, tx)
	aiExec(t, ctx, tx, `UPDATE tenants SET suspended_at = now(), suspension_reason = 'non_payment' WHERE id = $1`, w.b)
	hb := idSession(t, ctx, tx, w.b, w.mb, nil)
	list := func(hash string) [][]any {
		rows, err := idAs(t, ctx, tx, identityMembershipsSQL, hash)
		if err != nil {
			t.Fatalf("identity_memberships: %v", err)
		}
		return rows
	}
	rows := list(hb)
	if len(rows) != 2 || uuid.UUID(rows[0][0].([16]byte)) != w.b || rows[0][4] != true || rows[0][3] != true || rows[0][2] != "manager" ||
		uuid.UUID(rows[1][0].([16]byte)) != w.a || rows[1][4] != false || rows[1][2] != "owner" {
		t.Fatalf("B's session lists %v; want B (current, suspended, manager) then A (owner)", rows)
	}
	for _, r := range rows {
		if uuid.UUID(r[0].([16]byte)) == w.c {
			t.Fatal("another identity's business is listed")
		}
	}
	if got := list(opRandHex(t)); len(got) != 0 {
		t.Fatalf("an unknown hash lists %v", got)
	}
	hu := idSession(t, ctx, tx, w.a, w.unlinked, nil)
	if got := list(hu); len(got) != 1 || uuid.UUID(got[0][0].([16]byte)) != w.a || got[0][4] != true {
		t.Fatalf("an unlinked row's session lists %v; want itself alone", got)
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		aiExec(t, ctx, sp, `UPDATE admin_users SET unlinked_at = now() WHERE id = $1`, w.mb)
		rows, err := idAs(t, ctx, sp, identityMembershipsSQL, hb)
		if err != nil || len(rows) != 0 {
			t.Fatalf("a session whose own row is refused (identity kept) lists %v, %v; want nothing", rows, err)
		}
	})
	aiExec(t, ctx, tx, `UPDATE admin_sessions SET revoked_at = now() WHERE token_hash = $1`, hb)
	if got := list(hb); len(got) != 0 {
		t.Fatalf("a revoked session lists %v", got)
	}
	aiExec(t, ctx, tx, `UPDATE admin_users SET status = 'disabled' WHERE id = $1`, w.ma)
	if got := list(idSession(t, ctx, tx, w.b, w.mb, nil)); len(got) != 1 {
		t.Fatalf("a disabled membership is listed: %v", got)
	}

	// 55 memberships: 50 listed, the current one first.
	for range 53 {
		tn := idTenant(t, ctx, tx, "many")
		m := aiAdmin(t, ctx, tx, tn, w.email)
		aiLink(t, ctx, tx, m, w.ident)
	}
	last := idTenant(t, ctx, tx, "zzz last")
	ml := aiAdmin(t, ctx, tx, last, w.email)
	aiLink(t, ctx, tx, ml, w.ident)
	got := list(idSession(t, ctx, tx, last, ml, nil))
	if len(got) != 50 || uuid.UUID(got[0][0].([16]byte)) != last || got[0][4] != true {
		t.Fatalf("55 memberships: %d listed, first %v; want 50, the current first", len(got), got[0])
	}
}

// ---------------------------------------------------------------- the switch --

// TestIdentitySwitch_RefusesEachCaseSeparately: every refusal is 0 rows and writes nothing (no
// session, no revocation, no audit row) -- the target holds no membership of the person / the target
// membership is disabled / the target membership is refused (unlinked_at) / the session is revoked /
// the session belongs to another identity (whose person is not in the target) / the session's row
// is not linked / the session's own row is disabled / the target is the current business / an
// unknown hash.
func TestIdentitySwitch_RefusesEachCaseSeparately(t *testing.T) {
	ctx, tx := opTx(t)
	w := idNewWorld(t, ctx, tx)
	cases := []struct {
		name  string
		setup func(sp pgx.Tx) (hash string, target uuid.UUID)
	}{
		{"no membership in the target", func(sp pgx.Tx) (string, uuid.UUID) {
			return idSession(t, ctx, sp, w.a, w.ma, nil), w.c
		}},
		{"a disabled target", func(sp pgx.Tx) (string, uuid.UUID) {
			aiExec(t, ctx, sp, `UPDATE admin_users SET status = 'disabled' WHERE id = $1`, w.mb)
			return idSession(t, ctx, sp, w.a, w.ma, nil), w.b
		}},
		{"a refused target", func(sp pgx.Tx) (string, uuid.UUID) {
			aiExec(t, ctx, sp, `UPDATE admin_users SET unlinked_at = now() WHERE id = $1`, w.mb)
			return idSession(t, ctx, sp, w.a, w.ma, nil), w.b
		}},
		{"a revoked session", func(sp pgx.Tx) (string, uuid.UUID) {
			h := idSession(t, ctx, sp, w.a, w.ma, nil)
			aiExec(t, ctx, sp, `UPDATE admin_sessions SET revoked_at = now() WHERE token_hash = $1`, h)
			return h, w.b
		}},
		{"another identity's session", func(sp pgx.Tx) (string, uuid.UUID) {
			return idSession(t, ctx, sp, w.c, w.mc, nil), w.b
		}},
		{"an unlinked row's session", func(sp pgx.Tx) (string, uuid.UUID) {
			// Same address as the person's, unlinked, in a business where the person IS a member.
			d := idTenant(t, ctx, sp, "Delta")
			u := aiAdmin(t, ctx, sp, d, w.email)
			return idSession(t, ctx, sp, d, u, nil), w.b
		}},
		{"the session's own row disabled", func(sp pgx.Tx) (string, uuid.UUID) {
			h := idSession(t, ctx, sp, w.a, w.ma, nil)
			aiExec(t, ctx, sp, `UPDATE admin_users SET status = 'disabled' WHERE id = $1`, w.ma)
			return h, w.b
		}},
		{"the session's own row refused (identity kept)", func(sp pgx.Tx) (string, uuid.UUID) {
			// The third eye's M10: both `a.unlinked_at IS NULL` of the switch removed stayed green.
			h := idSession(t, ctx, sp, w.a, w.ma, nil)
			aiExec(t, ctx, sp, `UPDATE admin_users SET unlinked_at = now() WHERE id = $1`, w.ma)
			return h, w.b
		}},
		{"the current business", func(sp pgx.Tx) (string, uuid.UUID) {
			return idSession(t, ctx, sp, w.a, w.ma, nil), w.a
		}},
		{"an unknown hash", func(sp pgx.Tx) (string, uuid.UUID) { return opRandHex(t), w.b }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				hash, target := c.setup(sp)
				count := func() string {
					var s string
					if err := sp.QueryRow(ctx, `SELECT (SELECT count(*) FROM admin_sessions)::text || '/' ||
					                                   (SELECT count(*) FROM admin_sessions WHERE revoked_at IS NOT NULL)::text || '/' ||
					                                   (SELECT count(*) FROM audit_log WHERE tenant_id IN ($1, $2, $3))::text || '/' ||
					                                   (SELECT coalesce(last_used_tenant::text, '-') FROM admin_identities WHERE id = $4)`,
						w.a, w.b, w.c, w.ident).Scan(&s); err != nil {
						t.Fatal(err)
					}
					return s
				}
				before := count()
				if rows := idSwitch(t, ctx, sp, hash, target, opRandHex(t)); len(rows) != 0 {
					t.Fatalf("switched: %v", rows)
				}
				if after := count(); after != before {
					t.Fatalf("a refused switch wrote: sessions/revoked/audit/last-used %s -> %s", before, after)
				}
			})
		})
	}
}

// TestIdentitySwitch_ANewSessionTheOldRevokedTheProofCarried: a switch from A to B opens a session in
// B for the person's B membership under the new hash, revokes A's session, copies authenticated_at
// from the old session's ROW (a past instant stays that instant; NULL stays NULL -- R15), sets
// last_used_tenant and B's last_login_at, and writes exactly two audit rows: A's ordinary sign-out
// and B's ordinary login, whose text names neither the other business (id or name), nor the
// identity, nor a count, nor a `via` -- the detail is exactly {outcome, session_id}.
func TestIdentitySwitch_ANewSessionTheOldRevokedTheProofCarried(t *testing.T) {
	ctx, tx := opTx(t)
	w := idNewWorld(t, ctx, tx)
	var names [2]string
	if err := tx.QueryRow(ctx, `SELECT (SELECT name FROM tenants WHERE id = $1), (SELECT name FROM tenants WHERE id = $2)`, w.a, w.b).Scan(&names[0], &names[1]); err != nil {
		t.Fatal(err)
	}
	proof := time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)
	for _, c := range []struct {
		name   string
		authAt any
	}{{"a past proof", proof}, {"no proof (an old session)", "null"}} {
		t.Run(c.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				var old string
				if c.authAt == "null" {
					old = opRandHex(t)
					aiExec(t, ctx, sp, `INSERT INTO admin_sessions (tenant_id, admin_user_id, token_hash, authenticated_at) VALUES ($1, $2, $3, NULL)`, w.a, w.ma, old)
				} else {
					old = idSession(t, ctx, sp, w.a, w.ma, c.authAt)
				}
				var oldID uuid.UUID
				if err := sp.QueryRow(ctx, `SELECT id FROM admin_sessions WHERE token_hash = $1`, old).Scan(&oldID); err != nil {
					t.Fatal(err)
				}
				newHash := opRandHex(t)
				rows := idSwitch(t, ctx, sp, old, w.b, newHash)
				if len(rows) != 1 {
					t.Fatalf("the switch returned %d rows", len(rows))
				}
				newID, tenant, admin := uuid.UUID(rows[0][0].([16]byte)), uuid.UUID(rows[0][1].([16]byte)), uuid.UUID(rows[0][2].([16]byte))
				if tenant != w.b || admin != w.mb {
					t.Fatalf("landed in %v as %v, want B as the person's B membership", tenant, admin)
				}
				var (
					ok       bool
					authAt   *time.Time
					oldDead  bool
					lastUsed uuid.UUID
					stamped  bool
				)
				if err := sp.QueryRow(ctx, `SELECT s.token_hash = $2 AND s.tenant_id = $3 AND s.admin_user_id = $4 AND s.revoked_at IS NULL,
				                                   s.authenticated_at,
				                                   (SELECT revoked_at IS NOT NULL FROM admin_sessions WHERE id = $5),
				                                   (SELECT last_used_tenant FROM admin_identities WHERE id = $6),
				                                   (SELECT last_login_at = now() FROM admin_users WHERE id = $4)
				                              FROM admin_sessions s WHERE s.id = $1`, newID, newHash, w.b, w.mb, oldID, w.ident).
					Scan(&ok, &authAt, &oldDead, &lastUsed, &stamped); err != nil {
					t.Fatal(err)
				}
				if !ok || !oldDead || lastUsed != w.b || !stamped {
					t.Fatalf("new session right=%v, old revoked=%v, last used=%v, B stamped=%v", ok, oldDead, lastUsed, stamped)
				}
				if c.authAt == "null" {
					if authAt != nil {
						t.Fatalf("an old session's NULL proof became %v (R15: NULL means stale, it is carried)", authAt)
					}
				} else if authAt == nil || !authAt.Equal(proof) {
					t.Fatalf("authenticated_at %v, want the old session's %v (R15: copied from the row, never renewed)", authAt, proof)
				}

				type row struct {
					Tenant uuid.UUID
					Actor  *uuid.UUID
					Action string
					Target *string
					Detail string
				}
				r, err := sp.Query(ctx, `SELECT tenant_id, actor_id, action, target, detail::text FROM audit_log
				                          WHERE tenant_id IN ($1, $2, $3) ORDER BY at, action DESC`, w.a, w.b, w.c)
				if err != nil {
					t.Fatal(err)
				}
				audit, err := pgx.CollectRows(r, pgx.RowToStructByPos[row])
				if err != nil {
					t.Fatal(err)
				}
				if len(audit) != 2 {
					t.Fatalf("audit rows in the three businesses: %d (%v), want exactly 2", len(audit), audit)
				}
				want := map[uuid.UUID]row{
					w.a: {w.a, &w.ma, "admin.logout", ptrStr(w.ma.String()), fmt.Sprintf(`{"outcome": "ok", "session_id": "%s"}`, oldID)},
					w.b: {w.b, &w.mb, "admin.login.succeeded", ptrStr(w.mb.String()), fmt.Sprintf(`{"outcome": "ok", "session_id": "%s"}`, newID)},
				}
				for _, got := range audit {
					exp := want[got.Tenant]
					if got.Action != exp.Action || got.Actor == nil || *got.Actor != *exp.Actor || got.Target == nil || *got.Target != *exp.Target || got.Detail != exp.Detail {
						t.Errorf("audit row in %v = %+v, want %+v", got.Tenant, got, exp)
					}
					text := strings.ToLower(fmt.Sprintf("%v %v %s %v %s", got.Tenant, *got.Actor, got.Action, *got.Target, got.Detail))
					other, otherName := w.b, names[1]
					if got.Tenant == w.b {
						other, otherName = w.a, names[0]
					}
					for _, banned := range []string{other.String(), strings.ToLower(otherName), w.ident.String(), "via", "switch", "membership", "count", "identity"} {
						if strings.Contains(text, banned) {
							t.Errorf("the audit row in %v names %q: %s", got.Tenant, banned, text)
						}
					}
				}
			})
		})
	}
}

// TestIdentitySwitch_ConcurrentSwitchesOpenExactlyOneSession (ADR 0031 §5.4, bulgu 29): N goroutines
// switch the SAME session at once, each with its own new hash, through SwitchIdentitySession on the
// application pool -- exactly one gets a session, every other gets pgx.ErrNoRows, the old session is
// revoked once, and the person has exactly one new live session. Run with -race. The rows are
// COMMITTED (separate connections must see them) and taken back at the end: the identity and its
// links (00069's Down refuses while one exists); the sessions, the business rows and the audit rows
// stay, as every package's committed panel fixtures do.
func TestIdentitySwitch_ConcurrentSwitchesOpenExactlyOneSession(t *testing.T) {
	app := appDB(t)
	owner := ownerDB(t)
	ctx := context.Background()
	var w idWorld
	var old string
	err := pgx.BeginFunc(ctx, owner.pool, func(tx pgx.Tx) error {
		w = idNewWorld(t, ctx, tx)
		old = idSession(t, ctx, tx, w.a, w.ma, nil)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		err := pgx.BeginFunc(context.Background(), owner.pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(context.Background(), `UPDATE admin_users SET identity_id = NULL, linked_via = NULL, linked_at = NULL
			                                             WHERE identity_id IN ($1, $2)`, w.ident, w.other); err != nil {
				return err
			}
			_, err := tx.Exec(context.Background(), `DELETE FROM admin_identities WHERE id IN ($1, $2)`, w.ident, w.other)
			return err
		})
		if err != nil {
			t.Errorf("take the committed identities back: %v", err)
		}
	})

	const n = 12
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		won  []IdentitySession
		lost int
		errs []error
	)
	start := make(chan struct{})
	hashes := make([]string, n)
	for i := range hashes {
		hashes[i] = opRandHex(t)
	}
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			s, err := app.SwitchIdentitySession(ctx, old, w.b, hashes[i])
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				won = append(won, s)
			case errors.Is(err, pgx.ErrNoRows):
				lost++
			default:
				errs = append(errs, err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if len(errs) > 0 {
		t.Fatalf("%d switch(es) failed rather than being refused: %v", len(errs), errs)
	}
	if len(won) != 1 || lost != n-1 {
		t.Fatalf("%d winners and %d refusals of %d, want exactly 1 and %d", len(won), lost, n, n-1)
	}
	var live, oldRevoked int64
	if err := owner.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM admin_sessions WHERE admin_user_id = $1 AND revoked_at IS NULL),
	                                           (SELECT count(*) FROM admin_sessions WHERE token_hash = $2 AND revoked_at IS NOT NULL)`, w.mb, old).
		Scan(&live, &oldRevoked); err != nil {
		t.Fatal(err)
	}
	if live != 1 || oldRevoked != 1 {
		t.Fatalf("live sessions in B: %d, old revoked: %d; want 1 and 1", live, oldRevoked)
	}

	// THE OVERLAP, MADE CERTAIN. Twelve free-running calls may happen to run one after another (each
	// is a millisecond), and then the person's lock is never exercised -- measured: with the lock
	// and the re-read's FOR UPDATE removed, the free-for-all above stayed green. So: a winner switches
	// B's live session back to A inside a transaction it keeps open; three more switches of the same
	// session are started and the test waits until all three are WAITING ON A LOCK in the database;
	// only then the winner commits. Each of the three must be refused (pgx.ErrNoRows) -- not
	// switched, and not failed.
	var bSession string
	if err := owner.pool.QueryRow(ctx, `SELECT token_hash FROM admin_sessions WHERE admin_user_id = $1 AND revoked_at IS NULL`, w.mb).Scan(&bSession); err != nil {
		t.Fatal(err)
	}
	conn, err := app.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	held, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Rollback(ctx) }()
	var heldNew uuid.UUID
	if err := held.QueryRow(ctx, `SELECT session_id FROM identity_switch_session($1, $2, $3)`, bSession, w.a, opRandHex(t)).Scan(&heldNew); err != nil {
		t.Fatalf("the held winner: %v", err)
	}
	const waiters = 3
	results := make(chan error, waiters)
	for range waiters {
		h := opRandHex(t)
		go func() {
			_, err := app.SwitchIdentitySession(ctx, bSession, w.a, h)
			results <- err
		}()
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		var waiting int64
		if err := owner.pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
		                                     WHERE wait_event_type = 'Lock' AND query LIKE '%identity_switch_session%'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting == waiters {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("PREMISE: %d of %d switches are waiting on a lock after 15 s", waiting, waiters)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := held.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for range waiters {
		if err := <-results; !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("a switch that waited behind the winner answered %v, want pgx.ErrNoRows", err)
		}
	}
	if err := owner.pool.QueryRow(ctx, `SELECT count(*) FROM admin_sessions WHERE admin_user_id IN ($1, $2) AND revoked_at IS NULL`, w.ma, w.mb).Scan(&live); err != nil {
		t.Fatal(err)
	}
	if live != 1 {
		t.Fatalf("after the held winner and three waiters the person has %d live sessions, want 1", live)
	}

	// The other four hand-written callers, on the same committed world through the application pool
	// (identity.go's scans: a digest, a nullable stamp, a boolean, a list, a count).
	r, err := app.ResolveIdentityByEmail(ctx, strings.ToUpper(w.email))
	if err != nil || r.ID != w.ident || !r.HasActiveMembership || r.EmailVerifiedAt != nil || r.PasswordHash.RevealForPasswordComparison() != opFakeDigest("a") {
		t.Fatalf("ResolveIdentityByEmail: %+v, %v", r, err)
	}
	if _, err := app.ResolveIdentityByEmail(ctx, aiEmail()); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("ResolveIdentityByEmail of an unknown address: %v, want pgx.ErrNoRows", err)
	}
	h := opRandHex(t)
	opened, err := app.OpenIdentitySession(ctx, w.ident, uuid.Nil, h)
	if err != nil || opened.TenantID != w.a || opened.AdminUserID != w.ma {
		t.Fatalf("OpenIdentitySession: %+v, %v; want A (the business used last)", opened, err)
	}
	if _, err := app.OpenIdentitySession(ctx, uuid.New(), w.a, opRandHex(t)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("OpenIdentitySession of no identity: %v, want pgx.ErrNoRows", err)
	}
	ms, err := app.IdentityMemberships(ctx, h)
	if err != nil || len(ms) != 2 || ms[0].TenantID != w.a || !ms[0].IsCurrent || ms[0].Role != "owner" || ms[1].TenantID != w.b || ms[1].IsCurrent {
		t.Fatalf("IdentityMemberships: %+v, %v", ms, err)
	}
	if ms, err := app.IdentityMemberships(ctx, opRandHex(t)); err != nil || len(ms) != 0 {
		t.Fatalf("IdentityMemberships of an unknown session: %+v, %v; want none", ms, err)
	}
	if n, err := app.RevokeIdentitySessions(ctx, h, false); err != nil || n != 2 {
		t.Fatalf("RevokeIdentitySessions: %d, %v; want the 2 live sessions (A's two)", n, err)
	}
	if _, err := app.RevokeIdentitySessions(ctx, h, false); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("RevokeIdentitySessions of a revoked session: %v, want pgx.ErrNoRows", err)
	}
}

// ------------------------------------------------------------- sign out everywhere --

// TestIdentityRevokeSessions_EveryBusinessOrdinaryRowsKeepCurrent: with keep_current the person's
// live sessions in every business but the current one are revoked, each leaving its own business an
// ordinary sign-out row (actor = that membership); another person's session and the unlinked admin's
// stay live; without keep_current the current one goes too; an unlinked row's session and a revoked
// one are 0 rows.
func TestIdentityRevokeSessions_EveryBusinessOrdinaryRowsKeepCurrent(t *testing.T) {
	ctx, tx := opTx(t)
	w := idNewWorld(t, ctx, tx)
	cur := idSession(t, ctx, tx, w.a, w.ma, nil)
	a2 := idSession(t, ctx, tx, w.a, w.ma, nil)
	b1 := idSession(t, ctx, tx, w.b, w.mb, nil)
	b2 := idSession(t, ctx, tx, w.b, w.mb, nil)
	other := idSession(t, ctx, tx, w.c, w.mc, nil)
	plain := idSession(t, ctx, tx, w.a, w.unlinked, nil)
	revoke := func(hash string, keep bool) [][]any {
		rows, err := idAs(t, ctx, tx, revokeIdentitySessionsSQL, hash, keep)
		if err != nil {
			t.Fatalf("identity_revoke_sessions: %v", err)
		}
		return rows
	}
	if rows := revoke(cur, true); len(rows) != 1 || rows[0][0] != int32(3) {
		t.Fatalf("keep current: %v, want 3 revoked", rows)
	}
	live := func(h string) bool {
		return idCount(t, ctx, tx, `SELECT count(*) FROM admin_sessions WHERE token_hash = $1 AND revoked_at IS NULL`, h) == 1
	}
	for h, want := range map[string]bool{cur: true, a2: false, b1: false, b2: false, other: true, plain: true} {
		if live(h) != want {
			t.Errorf("session live=%v, want %v", !want, want)
		}
	}
	for _, c := range []struct {
		tenant, actor uuid.UUID
		n             int64
	}{{w.a, w.ma, 1}, {w.b, w.mb, 2}, {w.c, w.mc, 0}} {
		if got := idCount(t, ctx, tx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND actor_id = $2 AND action = 'admin.logout'
		                                AND target = $2::text AND detail ?& ARRAY['outcome', 'session_id'] AND detail - 'outcome' - 'session_id' = '{}'::jsonb`,
			c.tenant, c.actor); got != c.n {
			t.Errorf("ordinary sign-out rows in %v: %d, want %d", c.tenant, got, c.n)
		}
	}
	if rows := revoke(plain, false); len(rows) != 0 {
		t.Fatalf("an unlinked row's session: %v, want 0 rows", rows)
	}
	if rows := revoke(cur, false); len(rows) != 1 || rows[0][0] != int32(1) || live(cur) {
		t.Fatalf("without keep current: %v, current live=%v", rows, live(cur))
	}
	if rows := revoke(cur, false); len(rows) != 0 {
		t.Fatalf("a revoked session: %v, want 0 rows", rows)
	}
}

// TestIdentityRevokeSessions_ADisabledOrRefusedMembershipIsRevokedSilently (the security audit's
// ORTA, ADR 0031 §5.4 / bulgu 17): "sign out everywhere" revokes the live session of a DISABLED
// membership and of a REFUSED one (unlinked_at stamped, identity kept) -- they are the person's -- but
// writes NO audit row in those businesses: a sign-out row where the person can no longer sign in
// would tell that business's owner the person signed out from somewhere else. The active
// membership's revoked session still gets its ordinary row. And a session whose OWN row is refused
// is no key at all: 0 rows, nothing revoked.
func TestIdentityRevokeSessions_ADisabledOrRefusedMembershipIsRevokedSilently(t *testing.T) {
	ctx, tx := opTx(t)
	w := idNewWorld(t, ctx, tx)
	d, e := idTenant(t, ctx, tx, "Disabled"), idTenant(t, ctx, tx, "Refused")
	md, me := aiAdmin(t, ctx, tx, d, w.email), aiAdmin(t, ctx, tx, e, w.email)
	aiLink(t, ctx, tx, md, w.ident)
	aiLink(t, ctx, tx, me, w.ident)
	hd, he := idSession(t, ctx, tx, d, md, nil), idSession(t, ctx, tx, e, me, nil)
	aiExec(t, ctx, tx, `UPDATE admin_users SET status = 'disabled' WHERE id = $1`, md)
	aiExec(t, ctx, tx, `UPDATE admin_users SET unlinked_at = now() WHERE id = $1`, me)
	cur := idSession(t, ctx, tx, w.a, w.ma, nil)
	hb := idSession(t, ctx, tx, w.b, w.mb, nil)

	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		rows, err := idAs(t, ctx, sp, revokeIdentitySessionsSQL, he, false)
		if err != nil || len(rows) != 0 {
			t.Fatalf("a session whose own row is refused: %v, %v; want 0 rows", rows, err)
		}
		if n := idCount(t, ctx, sp, `SELECT count(*) FROM admin_sessions WHERE token_hash IN ($1, $2, $3, $4) AND revoked_at IS NOT NULL`, hd, he, cur, hb); n != 0 {
			t.Fatalf("a refused row's session revoked %d session(s)", n)
		}
	})

	rows, err := idAs(t, ctx, tx, revokeIdentitySessionsSQL, cur, true)
	if err != nil || len(rows) != 1 || rows[0][0] != int32(3) {
		t.Fatalf("keep current: %v, %v; want 3 revoked (B's, the disabled one's, the refused one's)", rows, err)
	}
	if n := idCount(t, ctx, tx, `SELECT count(*) FROM admin_sessions WHERE token_hash IN ($1, $2, $3) AND revoked_at IS NOT NULL`, hd, he, hb); n != 3 {
		t.Fatalf("%d of the three sessions revoked, want 3", n)
	}
	for _, c := range []struct {
		name   string
		tenant uuid.UUID
		want   int64
	}{{"the disabled membership's business", d, 0}, {"the refused membership's business", e, 0}, {"B", w.b, 1}} {
		if n := idCount(t, ctx, tx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1`, c.tenant); n != c.want {
			t.Errorf("audit rows in %s: %d, want %d", c.name, n, c.want)
		}
	}
}

// ------------------------------------------ tappa_app's INSERT on admin_sessions --

// TestAdminSessions00071_TheApplicationCannotChooseAuthenticatedAt (ADR 0031 §5.1, R15; orchestrator
// decision on M14-03's audit): tappa_app's INSERT on admin_sessions is the column list (id,
// tenant_id, admin_user_id, token_hash) -- a session born with authenticated_at = 'infinity' would
// pass every freshness predicate forever -- so naming authenticated_at (or created_at, revoked_at) is
// 42501, while CreateAdminSession -- the binary before 00071 -- still opens a session that takes
// now(), and the fixtures' explicit-id INSERT still works.
func TestAdminSessions00071_TheApplicationCannotChooseAuthenticatedAt(t *testing.T) {
	ctx, tx := opTx(t)
	w := idNewWorld(t, ctx, tx)
	if got := opColumns(t, ctx, tx, "tappa_app", "admin_sessions", "INSERT"); got != "id,tenant_id,admin_user_id,token_hash" {
		t.Fatalf("tappa_app INSERT on admin_sessions = (%s)", got)
	}
	for _, col := range []string{"authenticated_at", "created_at", "revoked_at", "last_used_at"} {
		opWant(t, aiApp(t, ctx, tx, w.a, func(sp pgx.Tx) error {
			_, err := sp.Exec(ctx, fmt.Sprintf(`INSERT INTO admin_sessions (tenant_id, admin_user_id, token_hash, %s) VALUES ($1, $2, $3, 'infinity')`, col), w.a, w.unlinked, opRandHex(t))
			return err
		}), sqlstateInsufficientPrivi, "tappa_app naming "+col)
	}
	if err := aiApp(t, ctx, tx, w.a, func(sp pgx.Tx) error {
		row, err := store.New(sp).CreateAdminSession(ctx, store.CreateAdminSessionParams{TokenHash: opRandHex(t), AdminUserID: w.unlinked, TenantID: w.a})
		if err != nil {
			return err
		}
		var fresh bool
		if err := sp.QueryRow(ctx, `SELECT authenticated_at = now() FROM admin_sessions WHERE id = $1`, row.ID).Scan(&fresh); err != nil {
			return err
		}
		if !fresh {
			return errors.New("CreateAdminSession's session did not take now()")
		}
		_, err = sp.Exec(ctx, `INSERT INTO admin_sessions (id, tenant_id, admin_user_id, token_hash) VALUES ($1, $2, $3, $4)`, uuid.New(), w.a, w.unlinked, opRandHex(t))
		return err
	}); err != nil {
		t.Fatalf("the binary before 00071 (CreateAdminSession) and the explicit-id fixture: %v", err)
	}
}
