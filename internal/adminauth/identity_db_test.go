package adminauth

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// identity_db_test.go -- the identity sign-in against a real PostgreSQL (ADR 0031 §5.6, §5.7; M14-07):
// the identity's session and the database's own audit row, the one failure value and the
// identity's event rows, the automatic link two fresh sessions prove, the eight-row window that
// belongs to unlinked rows (T2), the refused row that is no candidate, and the cross-tenant bypass
// on the identity path. Plus Issue's Proof: a picker or a direct sign-in opens only the row and the
// digest the password verified (card test (1) end to end; internal/db's adminsessiondigest_test.go
// holds the statement's predicate and lock by themselves).
//
// FIXTURES ARE COMMITTED (the sign-in runs autocommitted statements on the application pool) and
// written as the owner -- no application path creates an identity before M14-17. idWorld's cleanup
// takes the identity back out (links, refusals, events, the identity); tenants, rows, sessions and
// audit rows stay, as every committed DB test here leaves them (audit_log is append-only).

// operatorTablesTestLock is the advisory lock internal/db's schema tests take EXCLUSIVE (opTx) around
// work that must see no committed identity -- 00069/00072/00073's Downs refuse while one exists. The
// identity worlds here COMMIT identities, events and sessions, so they hold it SHARED for the whole
// test (internal/operatorauth's and internal/handler/operator's precedent); measured without it: 66
// FAILs in internal/db when the two packages ran in parallel (M14-07's audit).
const operatorTablesTestLock = "tappa/test/operator-tables"

// sharedTablesLock takes the lock shared on conn, held until conn closes (the test's end).
func sharedTablesLock(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock_shared(hashtext($1))`, operatorTablesTestLock); err != nil {
		t.Fatalf("operator-tables test lock: %v", err)
	}
}

// ownerConn is a dedicated owner connection (DATABASE_MIGRATE_URL; test-only, redline R5), holding
// the operator-tables lock shared until the test ends. One per test: the lock is taken once per test
// tree (cmd/opadmin's TestTablesLock_IsTakenOncePerTestTree).
func ownerConn(t *testing.T) *pgx.Conn {
	t.Helper()
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; the identity fixtures are written as the owner (real Postgres required)")
	}
	c, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}
	t.Cleanup(func() {
		if err := c.Close(context.Background()); err != nil {
			t.Errorf("close the owner connection: %v", err)
		}
	})
	sharedTablesLock(t, context.Background(), c)
	return c
}

// TestIdentityWorlds_TakeTheLockInternalDBTakes: the spelling is internal/db's, read from its source,
// or the two packages stop taking turns.
func TestIdentityWorlds_TakeTheLockInternalDBTakes(t *testing.T) {
	src, err := os.ReadFile("../db/operatorschema_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `const operatorTablesTestLock = "`+operatorTablesTestLock+`"`) {
		t.Fatalf("internal/db no longer names the %q lock", operatorTablesTestLock)
	}
}

// idWorld is one person: an address, a password and the identity's digest (bcrypt.MinCost, the
// rest of this package's fixtures' cost), plus the tenants it builds.
type idWorld struct {
	t      *testing.T
	owner  *pgx.Conn
	email  string
	pw     string
	digest string
	ident  uuid.UUID
}

func (w *idWorld) exec(sql string, args ...any) {
	w.t.Helper()
	if _, err := w.owner.Exec(context.Background(), sql, args...); err != nil {
		w.t.Fatalf("%s: %v", sql, err)
	}
}

func (w *idWorld) int(sql string, args ...any) int {
	w.t.Helper()
	var n int
	if err := w.owner.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		w.t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// newIDWorld makes an identity for a fresh address (withIdentity) or only the address and password.
func newIDWorld(t *testing.T, owner *pgx.Conn, withIdentity bool) *idWorld {
	t.Helper()
	w := &idWorld{t: t, owner: owner, email: "m14-07-" + uuid.NewString() + "@identity.example", pw: "pw-" + uuid.NewString()}
	w.digest = fixtureDigest(t, w.pw)
	if !withIdentity {
		return w
	}
	if err := owner.QueryRow(context.Background(), `INSERT INTO admin_identities (email, password_hash) VALUES ($1, $2) RETURNING id`, w.email, w.digest).Scan(&w.ident); err != nil {
		t.Fatalf("insert an identity: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		for _, sql := range []string{
			`UPDATE admin_users SET identity_id = NULL, linked_via = NULL, linked_at = NULL, unlinked_at = NULL WHERE identity_id = $1 OR email = $2`,
			`DELETE FROM admin_identity_events WHERE identity_id = $1 AND $2::text IS NOT NULL`,
			`DELETE FROM admin_identities WHERE id = $1 AND $2::text IS NOT NULL`,
		} {
			if _, err := owner.Exec(ctx, sql, w.ident, w.email); err != nil {
				t.Errorf("cleanup %s: %v", sql, err)
			}
		}
	})
	return w
}

func (w *idWorld) tenant(name string) uuid.UUID {
	w.t.Helper()
	id := uuid.New()
	w.exec(`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, $2, $3, 'bar', 'single')`, id, name, "VAT-M1407-"+id.String())
	return id
}

// row inserts an admin_users row of this address; age backdates created_at (the resolver's order).
func (w *idWorld) row(tenant uuid.UUID, digest, status string, age time.Duration) uuid.UUID {
	w.t.Helper()
	id := uuid.New()
	w.exec(`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status, created_at)
	        VALUES ($1, $2, 'M14-07 Admin', $3, $4, 'owner', $5, now() - $6::interval)`,
		id, tenant, w.email, digest, status, age.String())
	return id
}

// linked inserts a row carrying the identity's digest and links it (the way a backfill would have).
func (w *idWorld) linked(tenant uuid.UUID, status, via string, age time.Duration) uuid.UUID {
	w.t.Helper()
	id := w.row(tenant, w.digest, status, age)
	w.exec(`UPDATE admin_users SET identity_id = $2, linked_via = $3, linked_at = now() - interval '3 days' WHERE id = $1`, id, w.ident, via)
	return id
}

func (w *idWorld) sessions(tenant uuid.UUID, live bool) int {
	w.t.Helper()
	q := `SELECT count(*) FROM admin_sessions WHERE tenant_id = $1`
	if live {
		q += ` AND revoked_at IS NULL`
	}
	return w.int(q, tenant)
}

func (w *idWorld) audit(tenant uuid.UUID, action string) int {
	w.t.Helper()
	return w.int(`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2`, tenant, action)
}

func (w *idWorld) events(event string) int {
	w.t.Helper()
	return w.int(`SELECT count(*) FROM admin_identity_events WHERE identity_id = $1 AND event = $2`, w.ident, event)
}

// link reports a row's identity_id and linked_via ("" when unlinked).
func (w *idWorld) link(row uuid.UUID) (uuid.UUID, string) {
	w.t.Helper()
	var ident *uuid.UUID
	var via *string
	if err := w.owner.QueryRow(context.Background(), `SELECT identity_id, linked_via FROM admin_users WHERE id = $1`, row).Scan(&ident, &via); err != nil {
		w.t.Fatal(err)
	}
	if ident == nil {
		return uuid.Nil, ""
	}
	v := ""
	if via != nil {
		v = *via
	}
	return *ident, v
}

func generousBudget() FailureBudget { return newCountingBudget(1000) }

// TestAuthenticatePerson_SignsTheIdentityInAndTheDatabaseWritesTheRow: the right password of an
// identity with an active membership opens a session there; Verify accepts its token; the
// business's trail holds exactly ONE admin.login.succeeded row for it -- identity_open_session's
// own (00071) -- so a caller that also wrote adminLoginSucceededEvent would double it.
func TestAuthenticatePerson_SignsTheIdentityInAndTheDatabaseWritesTheRow(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	w := newIDWorld(t, ownerConn(t), true)
	ta := w.tenant("M14-07 Identity Co")
	a := w.linked(ta, "active", "backfill", 48*time.Hour)

	in, err := m.AuthenticatePerson(context.Background(), w.email, w.pw, uuid.Nil, generousBudget())
	if err != nil || in.Session == nil {
		t.Fatalf("AuthenticatePerson = %+v, %v; want an identity session", in, err)
	}
	if in.Session.Session.TenantID != ta || in.Session.Session.AdminUserID != a {
		t.Fatalf("the session landed in %s/%s, want %s/%s", in.Session.Session.TenantID, in.Session.Session.AdminUserID, ta, a)
	}
	r, err := m.Verify(context.Background(), in.Session.Token)
	if err != nil || r.AdminUserID != a || r.TenantID != ta {
		t.Fatalf("Verify of the identity session = %+v, %v", r, err)
	}
	if n := w.int(`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = 'admin.login.succeeded' AND detail->>'session_id' = $2`, ta, in.Session.Session.ID.String()); n != 1 {
		t.Fatalf("%d admin.login.succeeded rows for the identity session, want exactly the database's one", n)
	}
	if n := w.events("login_failed"); n != 0 {
		t.Errorf("a successful sign-in wrote %d failure events", n)
	}
}

// TestAuthenticatePerson_EveryFailureIsOneValue: a wrong password, the right password on an identity
// whose every membership is disabled, and an unknown address are ErrBadCredentials and nothing else;
// none opens a session; each failure against the identity is one login_failed row
// (identity_record_failed_login), an unknown address none.
func TestAuthenticatePerson_EveryFailureIsOneValue(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	w := newIDWorld(t, ownerConn(t), true)
	ta := w.tenant("M14-07 Failures Co")
	a := w.linked(ta, "active", "backfill", 48*time.Hour)
	ctx := context.Background()

	if in, err := m.AuthenticatePerson(ctx, w.email, "not-the-password", uuid.Nil, generousBudget()); err != ErrBadCredentials || in.Session != nil {
		t.Fatalf("wrong password: %+v, %v", in, err)
	}
	if n := w.events("login_failed"); n != 1 {
		t.Fatalf("a wrong password wrote %d login_failed rows, want 1", n)
	}
	w.exec(`UPDATE admin_users SET status = 'disabled' WHERE id = $1`, a)
	if in, err := m.AuthenticatePerson(ctx, w.email, w.pw, uuid.Nil, generousBudget()); err != ErrBadCredentials || in.Session != nil {
		t.Fatalf("every membership disabled, right password: %+v, %v", in, err)
	}
	if n := w.events("login_failed"); n != 2 {
		t.Fatalf("after the disabled case %d login_failed rows, want 2", n)
	}
	before := w.int(`SELECT count(*) FROM admin_identity_events`)
	if in, err := m.AuthenticatePerson(ctx, "nobody-"+uuid.NewString()+"@identity.example", w.pw, uuid.Nil, generousBudget()); err != ErrBadCredentials || in.Session != nil {
		t.Fatalf("unknown address: %+v, %v", in, err)
	}
	if after := w.int(`SELECT count(*) FROM admin_identity_events`); after != before {
		t.Errorf("an unknown address wrote %d event rows", after-before)
	}
	if n := w.sessions(ta, false); n != 0 {
		t.Errorf("%d sessions exist after three failures", n)
	}
}

// TestAuthenticatePerson_TheSamePasswordLinksAProvenLegacyRow (ADR 0031 §5.6, §5.7 (1)): the
// password of the identity ALSO verifies an unlinked row of the address in another business, so the
// row is linked with two fresh sessions -- linked_via 'password', the identity's digest -- the legacy
// proof session is revoked, and that business's trail gets admin.membership.linked and no sign-in
// row (the person signed in as the identity, elsewhere). A legacy row whose password differs is
// neither linked nor given a session.
func TestAuthenticatePerson_TheSamePasswordLinksAProvenLegacyRow(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	w := newIDWorld(t, ownerConn(t), true)
	ta, tb, tc := w.tenant("M14-07 Linked Co"), w.tenant("M14-07 Legacy Co"), w.tenant("M14-07 Other Co")
	a := w.linked(ta, "active", "backfill", 72*time.Hour)
	b := w.row(tb, fixtureDigest(t, w.pw), "active", 48*time.Hour)
	c := w.row(tc, fixtureDigest(t, "another-password"), "active", 24*time.Hour)

	in, err := m.AuthenticatePerson(context.Background(), w.email, w.pw, uuid.Nil, generousBudget())
	if err != nil || in.Session == nil || in.Session.Session.AdminUserID != a {
		t.Fatalf("AuthenticatePerson = %+v, %v; want the identity session on A", in, err)
	}
	if in.Linked != 1 {
		t.Errorf("Linked = %d, want 1", in.Linked)
	}
	if id, via := w.link(b); id != w.ident || via != "password" {
		t.Fatalf("B is linked to %s via %q, want the identity via 'password'", id, via)
	}
	if n := w.int(`SELECT count(*) FROM admin_users a JOIN admin_identities i ON i.id = a.identity_id WHERE a.id = $1 AND a.password_hash = i.password_hash`, b); n != 1 {
		t.Error("the linked row does not carry the identity's digest")
	}
	if all, live := w.sessions(tb, false), w.sessions(tb, true); all != 1 || live != 0 {
		t.Errorf("B's business holds %d sessions (%d live); want the one proof session, revoked", all, live)
	}
	if n := w.audit(tb, "admin.membership.linked"); n != 1 {
		t.Errorf("B's business has %d admin.membership.linked rows, want 1", n)
	}
	if n := w.audit(tb, "admin.login.succeeded"); n != 0 {
		t.Errorf("B's business has %d sign-in rows; the proof session is not a sign-in", n)
	}
	if id, _ := w.link(c); id != uuid.Nil || w.sessions(tc, false) != 0 {
		t.Errorf("C (another password) was linked (%s) or given a session", id)
	}

	// Once linked, B is the identity's own row: no longer a legacy candidate.
	in, err = m.AuthenticatePerson(context.Background(), w.email, w.pw, uuid.Nil, generousBudget())
	if err != nil || in.Session == nil || in.Linked != 0 {
		t.Fatalf("second sign-in = %+v, %v; want a session and nothing left to link", in, err)
	}
}

// TestAuthenticatePerson_EightLinkedRowsDoNotHideTheNinth is T2's test: a person with eight linked
// memberships (older, so first in the resolver's incumbent-first order) and one unlinked row signs in
// with the right password, and the unlinked row is linked. Were the identity's own rows candidates,
// they would fill the MaxCandidates window and the ninth would never be compared.
func TestAuthenticatePerson_EightLinkedRowsDoNotHideTheNinth(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	w := newIDWorld(t, ownerConn(t), true)
	// One backfill link per identity, as 00073 makes them; the rest as sign-ins link them.
	for i := 0; i < MaxCandidates; i++ {
		via := "password"
		if i == 0 {
			via = "backfill"
		}
		w.linked(w.tenant("M14-07 Eight Co"), "active", via, time.Duration(100-i)*time.Hour)
	}
	tn := w.tenant("M14-07 Ninth Co")
	ninth := w.row(tn, fixtureDigest(t, w.pw), "active", time.Hour)

	in, err := m.AuthenticatePerson(context.Background(), w.email, w.pw, uuid.Nil, generousBudget())
	if err != nil || in.Session == nil {
		t.Fatalf("AuthenticatePerson = %+v, %v", in, err)
	}
	if id, via := w.link(ninth); id != w.ident || via != "password" || in.Linked != 1 {
		t.Fatalf("the ninth row is linked to %s via %q (Linked %d); want it linked via 'password'", id, via, in.Linked)
	}
}

// TestAuthenticatePerson_ARefusedRowIsNoCandidate: a row the person refused from the identity
// (unlinked_at stamped) is not compared even when the typed password would open its digest, is not
// linked again (R1) and gets no session.
func TestAuthenticatePerson_ARefusedRowIsNoCandidate(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	w := newIDWorld(t, ownerConn(t), true)
	ta, tr := w.tenant("M14-07 Kept Co"), w.tenant("M14-07 Refused Co")
	w.linked(ta, "active", "backfill", 48*time.Hour)
	refused := w.row(tr, fixtureDigest(t, w.pw), "active", 24*time.Hour)
	w.exec(`UPDATE admin_users SET linked_via = 'reconcile', linked_at = now() - interval '3 days', unlinked_at = now() - interval '1 day' WHERE id = $1`, refused)

	in, err := m.AuthenticatePerson(context.Background(), w.email, w.pw, uuid.Nil, generousBudget())
	if err != nil || in.Session == nil {
		t.Fatalf("AuthenticatePerson = %+v, %v", in, err)
	}
	if in.Linked != 0 {
		t.Errorf("Linked = %d; a refused row is never linked again", in.Linked)
	}
	if id, _ := w.link(refused); id != uuid.Nil || w.sessions(tr, false) != 0 {
		t.Errorf("the refused row was linked (%s) or given a session", id)
	}
}

// TestAuthenticatePerson_RefusesTheCrossTenantBypass is TestAuthenticate_RefusesTheCrossTenantBypass's
// scenario on the identity path: the victim has an identity; the attacker writes the victim's
// address into their OWN business with a password they know. The attacker's password verifies the
// attacker's row alone -- a legacy sign-in into their own business, no identity session, no link to
// the victim's identity, nothing in the victim's business. The victim's password signs the victim
// in and does not link (or open anything in) the attacker's row.
func TestAuthenticatePerson_RefusesTheCrossTenantBypass(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	w := newIDWorld(t, ownerConn(t), true)
	victimTenant, attackerTenant := w.tenant("VictimCo"), w.tenant("AttackerCo")
	victim := w.linked(victimTenant, "active", "backfill", 72*time.Hour)
	const attackerPassword = "the-attackers-own-password"
	attacker := w.row(attackerTenant, fixtureDigest(t, attackerPassword), "active", time.Hour)
	ctx := context.Background()

	in, err := m.AuthenticatePerson(ctx, w.email, attackerPassword, uuid.Nil, generousBudget())
	if err != nil || in.Session != nil {
		t.Fatalf("the attacker's sign-in = %+v, %v; want a legacy outcome and no identity session", in, err)
	}
	v := in.Legacy.Verified()
	if len(v) != 1 || v[0].AdminUserID != attacker || v[0].TenantID != attackerTenant {
		t.Fatalf("the attacker's password verified %+v; want the attacker's own row alone", v)
	}
	if id, _ := w.link(attacker); id != uuid.Nil {
		t.Fatalf("the attacker's row was linked to %s -- the victim's identity", id)
	}
	if n := w.sessions(victimTenant, false); n != 0 {
		t.Fatalf("%d sessions in the VICTIM's business -- cross-tenant bypass (§4.5)", n)
	}

	in, err = m.AuthenticatePerson(ctx, w.email, w.pw, uuid.Nil, generousBudget())
	if err != nil || in.Session == nil || in.Session.Session.AdminUserID != victim || in.Session.Session.TenantID != victimTenant {
		t.Fatalf("the victim's sign-in = %+v, %v; want the victim's own session", in, err)
	}
	if id, _ := w.link(attacker); id != uuid.Nil || in.Linked != 0 {
		t.Fatalf("the victim's sign-in linked the attacker's row (%s, Linked %d)", id, in.Linked)
	}
	if n := w.sessions(attackerTenant, false); n != 0 {
		t.Fatalf("the victim's sign-in opened %d sessions in the attacker's business", n)
	}
}

// TestIssue_OpensOnlyTheRowAndTheDigestTheProofNames: Issue refuses a Verified without a proof, a
// proof carried over to another row, and a proof whose digest the row no longer carries (a password
// changed after the sign-in); the proof survives Encode / ParseProof (the picker's cookie) and then
// opens the session. None of the refusals writes a session.
func TestIssue_OpensOnlyTheRowAndTheDigestTheProofNames(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	ctx := context.Background()
	tenantID := newTenantRow(t, d, "M14-07 Proof Ltd")
	email := randEmail(t)
	admin := newAdminRow(t, d, tenantID, email, "the-right-password", "active", "owner", "Owner")
	other := newAdminRow(t, d, tenantID, randEmail(t), "the-right-password", "active", "manager", "Other")

	auth, err := m.Authenticate(ctx, email, "the-right-password")
	if err != nil || len(auth.Verified()) != 1 {
		t.Fatalf("Authenticate: %v", err)
	}
	v := auth.Verified()[0]
	if v.Proof.IsZero() {
		t.Fatal("Verified() carries no proof")
	}
	for _, c := range []struct {
		name string
		v    Verified
	}{
		{"no proof", Verified{AdminUserID: admin, TenantID: tenantID}},
		{"the proof carried over to another row", Verified{AdminUserID: other, TenantID: tenantID, Proof: v.Proof}},
	} {
		if _, err := m.Issue(ctx, c.v); err == nil {
			t.Errorf("Issue accepted %s", c.name)
		}
	}
	wire, err := ParseProof(v.Proof.Encode())
	if err != nil || wire != v.Proof {
		t.Fatalf("the proof does not survive its wire form: %v", err)
	}
	if _, err := m.Issue(ctx, Verified{AdminUserID: v.AdminUserID, TenantID: v.TenantID, Proof: wire}); err != nil {
		t.Fatalf("control: Issue with the round-tripped proof: %v", err)
	}

	if err := d.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).SetOwnAdminPassword(ctx, store.SetOwnAdminPasswordParams{ID: admin, TenantID: tenantID, PasswordHash: fixtureDigest(t, "a-new-password")})
		return e
	}); err != nil {
		t.Fatal(err)
	}
	before := countSessions(t, d, tenantID, admin)
	if _, err := m.Issue(ctx, v); !errors.Is(err, errProofStale) {
		t.Fatalf("Issue after the password changed: %v, want the stale-proof refusal", err)
	}
	if after := countSessions(t, d, tenantID, admin); after != before {
		t.Fatalf("a refused Issue wrote %d sessions", after-before)
	}
}

func countSessions(t *testing.T, d *db.DB, tenantID, admin uuid.UUID) int {
	t.Helper()
	var n int
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM admin_sessions WHERE tenant_id = $1 AND admin_user_id = $2`, tenantID, admin).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestIssue_AnUnlinkAfterTheSignInLeavesNoSession is the card's test (1) end to end, on today's
// (flag-off) path: the password verifies a LINKED row -- the legacy login still sees linked rows --
// then the person refuses that membership (identity_unlink_backfilled commits: sentinel digest,
// unlinked_at, sessions revoked), and only then does Issue run. It writes nothing: the proof names
// the digest that verified and the row now carries the sentinel. (Dropping the proof check leaves
// a live session on the refused row -- Issue would read the sentinel and the statement would accept
// it -- so this test is that check's mutation proof; the statement's own predicate is internal/db's.)
func TestIssue_AnUnlinkAfterTheSignInLeavesNoSession(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	w := newIDWorld(t, ownerConn(t), true)
	ctx := context.Background()
	tt, tx := w.tenant("M14-07 Home Co"), w.tenant("M14-07 Unlinked Co")
	home := w.linked(tt, "active", "backfill", 72*time.Hour)
	x := w.linked(tx, "active", "password", 48*time.Hour)
	// A fresh identity session on the home row and an open reset window that lists X.
	h := "m14-07-" + uuid.NewString()
	w.exec(`INSERT INTO admin_sessions (tenant_id, admin_user_id, token_hash, authenticated_at) VALUES ($1, $2, $3, now())`, tt, home, h)
	w.exec(`UPDATE admin_identities SET email_verified_at = now() - interval '2 days', confirm_opened_at = now() - interval '1 day',
	               confirm_open_until = now() + interval '6 days', confirm_scope = 'reset' WHERE id = $1`, w.ident)

	auth, err := m.Authenticate(ctx, w.email, w.pw)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	var vx Verified
	for _, v := range auth.Verified() {
		if v.AdminUserID == x {
			vx = v
		}
	}
	if vx.AdminUserID != x {
		t.Fatal("control: the linked row X did not verify on today's path")
	}

	app, err := pgx.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := app.Close(context.Background()); err != nil {
			t.Errorf("close: %v", err)
		}
	}()
	var unlinked uuid.UUID
	var revoked int32
	if err := app.QueryRow(ctx, `SELECT admin_user_id, revoked_sessions FROM identity_unlink_backfilled($1, $2)`, h, tx).Scan(&unlinked, &revoked); err != nil || unlinked != x {
		t.Fatalf("control: the unlink of X = %s, %v", unlinked, err)
	}

	if _, err := m.Issue(ctx, vx); err == nil {
		t.Fatal("Issue opened a session on a membership refused after the password verified")
	}
	if n := w.sessions(tx, true); n != 0 {
		t.Fatalf("%d live sessions on the refused row's business", n)
	}
}

// TestAuthenticatePerson_ARefusedLinkLeavesNoProofSession: when identity_link_legacy refuses (here
// the defence its body names last -- the identity already holds a membership in the legacy row's
// business; the fixture reaches that state with a linked row under another address, which no
// application path writes), the sign-in still succeeds as the identity, the legacy row stays
// unlinked, the function records link_refused, and the proof session it left live is revoked: no
// session on the legacy row outlives the sign-in.
func TestAuthenticatePerson_ARefusedLinkLeavesNoProofSession(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	w := newIDWorld(t, ownerConn(t), true)
	ta, tl := w.tenant("M14-07 Home Co"), w.tenant("M14-07 Already Co")
	w.linked(ta, "active", "backfill", 72*time.Hour)
	other := uuid.New()
	w.exec(`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, 'M14-07 Other', $3, $4, 'manager')`,
		other, tl, "other-"+uuid.NewString()+"@identity.example", w.digest)
	w.exec(`UPDATE admin_users SET identity_id = $2, linked_via = 'password', linked_at = now() - interval '3 days' WHERE id = $1`, other, w.ident)
	legacy := w.row(tl, fixtureDigest(t, w.pw), "active", time.Hour)

	in, err := m.AuthenticatePerson(context.Background(), w.email, w.pw, uuid.Nil, generousBudget())
	if err != nil || in.Session == nil || in.Linked != 0 {
		t.Fatalf("AuthenticatePerson = %+v, %v; want the identity session and nothing linked", in, err)
	}
	if id, _ := w.link(legacy); id != uuid.Nil {
		t.Fatalf("the legacy row was linked to %s", id)
	}
	if n := w.events("link_refused"); n != 1 {
		t.Errorf("%d link_refused events, want 1", n)
	}
	if all := w.int(`SELECT count(*) FROM admin_sessions WHERE admin_user_id = $1`, legacy); all != 1 {
		t.Fatalf("%d sessions on the legacy row, want the one proof session", all)
	}
	if live := w.int(`SELECT count(*) FROM admin_sessions WHERE admin_user_id = $1 AND revoked_at IS NULL`, legacy); live != 0 {
		t.Fatalf("the refused link's proof session is still live")
	}
}

// TestIssue_ConcurrentSignInsOfOneAdminDoNotDeadlock: Issue's CreateAdminSession locks the admin row
// and MarkAdminLoggedIn then UPDATEs it in the same transaction. With FOR SHARE that was a lock
// upgrade, and two concurrent sign-ins of one admin (a double click) deadlocked -- 40P01, a 500 on
// today's login path (M14-07's audit: 86/120). With FOR NO KEY UPDATE every one of N concurrent
// Issues of the same Verified succeeds. MUTATION: back to FOR SHARE -> red.
func TestIssue_ConcurrentSignInsOfOneAdminDoNotDeadlock(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	ctx := context.Background()
	tenantID := newTenantRow(t, d, "M14-07 Double Click Ltd")
	email := randEmail(t)
	admin := newAdminRow(t, d, tenantID, email, "the-right-password", "active", "owner", "Owner")
	auth, err := m.Authenticate(ctx, email, "the-right-password")
	if err != nil || len(auth.Verified()) != 1 {
		t.Fatalf("Authenticate: %v", err)
	}
	v := auth.Verified()[0]
	const n = 8
	for round := 0; round < 5; round++ {
		errs := make(chan error, n)
		start := make(chan struct{})
		for i := 0; i < n; i++ {
			go func() {
				<-start
				_, err := m.Issue(ctx, v)
				errs <- err
			}()
		}
		close(start)
		for i := 0; i < n; i++ {
			if err := <-errs; err != nil {
				t.Fatalf("round %d: a concurrent Issue of the same admin failed: %v", round, err)
			}
		}
	}
	if got := countSessions(t, d, tenantID, admin); got != 5*n {
		t.Fatalf("%d sessions, want %d", got, 5*n)
	}
}

// TestAuthenticatePerson_ConcurrentFailuresKeepTheBudgetExact (the M14-07 security audit's first low
// finding): forty concurrent wrong-password sign-ins of one identity write exactly the budget's
// login_failed rows and exactly ONE login_limited row -- the budget is charged and read in one step
// (TryCharge).
func TestAuthenticatePerson_ConcurrentFailuresKeepTheBudgetExact(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	w := newIDWorld(t, ownerConn(t), true)
	w.linked(w.tenant("M14-07 Budget Co"), "active", "backfill", 48*time.Hour)
	const limit, n = 10, 40
	budget := newCountingBudget(limit)
	errs := make(chan error, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		go func() {
			<-start
			_, err := m.AuthenticatePerson(context.Background(), w.email, "not-the-password", uuid.Nil, budget)
			errs <- err
		}()
	}
	close(start)
	for i := 0; i < n; i++ {
		if err := <-errs; err != ErrBadCredentials {
			t.Fatalf("a concurrent failure answered %v", err)
		}
	}
	if failed, limited := w.events("login_failed"), w.events("login_limited"); failed != limit || limited != 1 {
		t.Fatalf("%d login_failed and %d login_limited rows after %d concurrent failures; want %d and 1", failed, limited, n, limit)
	}
}

// TestRevokeProofs_SurvivesTheClientGoingAway: proof sessions are revoked on the paths that unwind,
// and the commonest reason to unwind is a cancelled request; the revocation must still land.
// MUTATION: drop context.WithoutCancel -> the revocation fails with the request and the session lives.
func TestRevokeProofs_SurvivesTheClientGoingAway(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	ctx := context.Background()
	tenantID := newTenantRow(t, d, "M14-07 Gone Ltd")
	email := randEmail(t)
	admin := newAdminRow(t, d, tenantID, email, "the-right-password", "active", "owner", "Owner")
	rows, err := d.GetAdminByEmail(ctx, email)
	if err != nil || len(rows) != 1 {
		t.Fatalf("GetAdminByEmail: %v", err)
	}
	p, err := m.openLegacyProofSession(ctx, admin, tenantID, rows[0].PasswordHash)
	if err != nil {
		t.Fatalf("open a proof session: %v", err)
	}
	gone, cancel := context.WithCancel(ctx)
	cancel()
	if err := m.revokeProofs(gone, []proofSession{p}); err != nil {
		t.Fatalf("revokeProofs with the request cancelled: %v", err)
	}
	var live int
	if err := d.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM admin_sessions WHERE id = $1 AND revoked_at IS NULL`, p.id).Scan(&live)
	}); err != nil {
		t.Fatal(err)
	}
	if live != 0 {
		t.Fatal("the proof session is still live after the client went away")
	}
}
