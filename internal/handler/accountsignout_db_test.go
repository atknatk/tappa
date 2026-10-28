package handler

// accountsignout_db_test.go -- M14-11 "Sign out everywhere" end to end: the real panel (flag on),
// the real adminauth.Manager over the real pool, identity_revoke_sessions (00071) and the real
// audit trail. A person is ONE identity linked to admin rows in several test-made businesses; the
// identity is committed, so each test holds internal/db's operator-tables lock SHARED -- through its
// panel harness (holdPanelTablesLock, M14-09's re-entrant holder) -- and takes the identity back at the end.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
)

// everywhereBusiness is one business of the person: its tenant, name and the person's row there.
type everywhereBusiness struct {
	tenant, admin uuid.UUID
	name          string
}

// addBusiness makes a business with a row for p's person (the same address and digest), linked to
// ident when ident is not Nil (as the backfill links). status is the membership's.
func addBusiness(t *testing.T, p *panelHarness, owner *pgx.Conn, ident uuid.UUID, name string) everywhereBusiness {
	t.Helper()
	b := everywhereBusiness{tenant: uuid.New(), admin: uuid.New(), name: name}
	if err := p.data.WithTenant(context.Background(), b.tenant, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure)
		                          VALUES ($1, $2, $3, 'bar', 'single')`, b.tenant, name, "VAT-"+b.tenant.String()); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
		                      VALUES ($1, $2, 'E2E Owner', $3, $4, 'owner', 'active')`, b.admin, b.tenant, p.email, ownerDigest)
		return e
	}); err != nil {
		t.Fatalf("make business %s: %v", name, err)
	}
	if ident != uuid.Nil {
		if _, err := owner.Exec(context.Background(), `UPDATE admin_users SET identity_id = $2, linked_via = 'backfill', linked_at = now()
		                                                 WHERE id = $1`, b.admin, ident); err != nil {
			t.Fatalf("link the row in %s: %v", name, err)
		}
	}
	return b
}

// landOn makes tenant the business ident's next sign-in lands on: on the identity path there is no
// picker, and identity_open_session lands on the business used last (K-4, M14-09).
func landOn(t *testing.T, owner *pgx.Conn, ident, tenant uuid.UUID) {
	t.Helper()
	if _, err := owner.Exec(context.Background(), `UPDATE admin_identities SET last_used_tenant = $2 WHERE id = $1`, ident, tenant); err != nil {
		t.Fatalf("land the next sign-in on %s: %v", tenant, err)
	}
}

// anotherBrowser is p with a cookie jar of its own: another device of the same person.
func (p *panelHarness) anotherBrowser(t *testing.T) *panelHarness {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	q := *p
	q.client = &http.Client{Jar: jar, CheckRedirect: p.client.CheckRedirect}
	return &q
}

// signInTo signs this browser in to tenant through today's sign-in (the picker when the password
// verifies more than one business).
func (p *panelHarness) signInTo(t *testing.T, tenant uuid.UUID) {
	t.Helper()
	_, page := p.get(t, adminLoginPath)
	res, _ := p.post(t, adminLoginPath, url.Values{"csrf": {csrfFrom(t, page)}, "email": {p.email}, "password": {p.password}})
	if res.StatusCode == http.StatusSeeOther && res.Header.Get("Location") == "/admin/login/choose" {
		_, picker := p.get(t, "/admin/login/choose")
		res, _ = p.post(t, "/admin/login/choose", url.Values{"csrf": {csrfFrom(t, picker)}, "tenant_id": {tenant.String()}})
	}
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin" {
		t.Fatalf("sign-in to %s: %d %q", tenant, res.StatusCode, res.Header.Get("Location"))
	}
}

// signedIn reports whether this browser's panel session is alive: the shell answers 200, a dead one
// is sent to the sign-in.
func (p *panelHarness) signedIn(t *testing.T) bool {
	t.Helper()
	res, _ := p.get(t, "/admin")
	switch {
	case res.StatusCode == http.StatusOK:
		return true
	case res.StatusCode == http.StatusSeeOther && strings.HasPrefix(res.Header.Get("Location"), adminLoginPath):
		return false
	}
	t.Fatalf("GET /admin answered %d %q", res.StatusCode, res.Header.Get("Location"))
	return false
}

// tenantRows is every audit row of tenant with action, as its JSON text, oldest first, read as the
// owner (RLS-exempt -- a row written into the wrong business is visible here).
type tenantRow struct {
	actor, target, detail, text string
}

func tenantRows(t *testing.T, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, tenant uuid.UUID, action string) []tenantRow {
	t.Helper()
	rows, err := q.Query(context.Background(), `SELECT coalesce(actor_id::text, ''), target, detail::text, row_to_json(a)::text
	                                             FROM audit_log a WHERE tenant_id = $1 AND action = $2 ORDER BY at, id`, tenant, action)
	if err != nil {
		t.Fatalf("read %s rows: %v", action, err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (tenantRow, error) {
		var x tenantRow
		err := r.Scan(&x.actor, &x.target, &x.detail, &x.text)
		return x, err
	})
	if err != nil {
		t.Fatalf("read %s rows: %v", action, err)
	}
	return out
}

// liveSessions is how many live sessions admin holds in tenant.
func (p *panelHarness) liveSessionsOf(t *testing.T, tenant, admin uuid.UUID) int {
	t.Helper()
	var n int
	if err := p.data.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM admin_sessions WHERE tenant_id = $1 AND admin_user_id = $2 AND revoked_at IS NULL`,
			tenant, admin).Scan(&n)
	}); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return n
}

// sessionsOf is every session id admin ever held in tenant.
func (p *panelHarness) sessionsOf(t *testing.T, tenant, admin uuid.UUID) []string {
	t.Helper()
	var out []string
	if err := p.data.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `SELECT id::text FROM admin_sessions WHERE tenant_id = $1 AND admin_user_id = $2`, tenant, admin)
		if e != nil {
			return e
		}
		out, e = pgx.CollectRows(rows, pgx.RowTo[string])
		return e
	}); err != nil {
		t.Fatalf("read sessions: %v", err)
	}
	return out
}

// checkOrdinaryLogouts holds the new admin.logout rows of one business to the ordinary sign-out row
// (adminLoggedOutEvent): want of them, actor and target that business's membership, detail exactly
// {"outcome":"ok","session_id":<a session of that membership>} -- and nothing in any of them names
// another business (its id, its name, its membership) or the identity.
func checkOrdinaryLogouts(t *testing.T, label string, rows []tenantRow, want int, own everywhereBusiness, sessions []string, foreign []string) {
	t.Helper()
	if len(rows) != want {
		t.Errorf("%s: %d new sign-out row(s), want %d", label, len(rows), want)
	}
	for _, r := range rows {
		if r.actor != own.admin.String() || r.target != own.admin.String() {
			t.Errorf("%s: a sign-out row's actor/target is %s/%s, want the membership %s", label, r.actor, r.target, own.admin)
		}
		var d map[string]string
		if err := json.Unmarshal([]byte(r.detail), &d); err != nil {
			t.Errorf("%s: detail %q: %v", label, r.detail, err)
			continue
		}
		keys := slices.Sorted(func(yield func(string) bool) {
			for k := range d {
				if !yield(k) {
					return
				}
			}
		})
		if !slices.Equal(keys, []string{"outcome", "session_id"}) || d["outcome"] != "ok" || !slices.Contains(sessions, d["session_id"]) {
			t.Errorf("%s: detail %s is not the ordinary sign-out's {outcome: ok, session_id: <its session>}", label, r.detail)
		}
		for _, f := range foreign {
			if strings.Contains(r.text, f) {
				t.Errorf("%s: a sign-out row names %q -- another business or the identity: %s", label, f, r.text)
			}
		}
	}
}

// TestSignOutEverywhereDB_OnePressEndsThePersonsSessionsInEveryBusiness is the card end to end, flag
// on. One person (one identity), four businesses: A (two devices), B (one device), C (one device,
// then the membership is DISABLED), D (one device, A is suspended -- the press is made from A).
// One POST from the first A device:
//
//   - answers 303 to the sign-in and clears that device's cookie;
//   - every device in every business is signed out (the shell sends each to the sign-in), and no
//     live session of the person is left in any business -- including the one that pressed;
//   - A gets two ordinary admin.logout rows (one per session), B and D one each, the disabled C
//     NONE (its session is revoked silently -- 00071); each row is byte-for-byte the ordinary
//     sign-out (actor/target its own membership, detail {outcome, session_id}) and names no other
//     business, membership or the identity, and carries no count;
//   - A is SUSPENDED and the press still goes through (protectWritingThroughSuspension; ADR 0025's
//     M14 note) without a tenant.write_refused row.
//
// MUTATION (measured): the route moved into ProtectWriting's group -> A's press is the suspension's
// 403, red; keepCurrent=true in the handler -> the pressing device stays signed in, red; the handler
// writing its own rows on the identity path -> A gets four rows, red.
func TestSignOutEverywhereDB_OnePressEndsThePersonsSessionsInEveryBusiness(t *testing.T) {
	p := newPanelHarnessWith(t, panelHarnessOptions{adminIdentity: true, tenantName: "Everywhere A Ltd"})
	owner := linkHarness(t, p)
	var ident uuid.UUID
	if err := owner.QueryRow(context.Background(), `SELECT identity_id FROM admin_users WHERE id = $1`, p.adminID).Scan(&ident); err != nil {
		t.Fatalf("the identity: %v", err)
	}
	a := everywhereBusiness{tenant: p.tenantID, admin: p.adminID, name: "Everywhere A Ltd"}
	b := addBusiness(t, p, owner, ident, "Everywhere B Ltd")
	c := addBusiness(t, p, owner, ident, "Everywhere C Ltd")
	d := addBusiness(t, p, owner, ident, "Everywhere D Ltd")

	a1, a2, b1, c1, d1 := p, p.anotherBrowser(t), p.anotherBrowser(t), p.anotherBrowser(t), p.anotherBrowser(t)
	// The rows are linked and the flag is on, so each sign-in takes the identity path (M14-09): no
	// picker, it lands in the business used last (K-4) -- set before each one.
	for _, s := range []struct {
		br     *panelHarness
		tenant uuid.UUID
	}{{a1, a.tenant}, {a2, a.tenant}, {b1, b.tenant}, {c1, c.tenant}, {d1, d.tenant}} {
		landOn(t, owner, ident, s.tenant)
		s.br.signInTo(t, s.tenant)
	}
	for _, x := range []everywhereBusiness{a, b, c, d} {
		want := 1
		if x == a {
			want = 2
		}
		if n := p.liveSessionsOf(t, x.tenant, x.admin); n != want {
			t.Fatalf("CONTROL: %s holds %d live session(s) after the sign-ins, want %d", x.name, n, want)
		}
	}
	if _, err := owner.Exec(context.Background(), `UPDATE admin_users SET status = 'disabled' WHERE id = $1`, c.admin); err != nil {
		t.Fatalf("disable the membership in C: %v", err)
	}
	suspendAsOwner(t, ownerPoolForTest(t), a.tenant, db.SuspensionNonPayment)
	for name, br := range map[string]*panelHarness{"A device 1": a1, "A device 2": a2, "B": b1, "D": d1} {
		if !br.signedIn(t) {
			t.Fatalf("CONTROL: %s is not signed in before the press", name)
		}
	}
	if n := p.liveSessionsOf(t, c.tenant, c.admin); n != 1 {
		t.Fatalf("CONTROL: the disabled membership holds %d live session(s), want 1", n)
	}
	before := map[uuid.UUID]int{}
	for _, x := range []everywhereBusiness{a, b, c, d} {
		before[x.tenant] = len(tenantRows(t, owner, x.tenant, ActionAdminLoggedOut))
	}
	refusedBefore := len(tenantRows(t, owner, a.tenant, ActionTenantWriteRefused))

	// The account page offers it -- and in a suspended business too.
	if _, page := a1.get(t, accountHref); !strings.Contains(page, `action="`+accountSignOutEverywhereHref+`"`) ||
		!strings.Contains(page, "every business you") {
		t.Fatal("the account page of a linked session does not offer \"Sign out everywhere\" for every business")
	}
	res, _ := a1.post(t, accountSignOutEverywhereHref, url.Values{})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != adminLoginPath {
		t.Fatalf("the press answered %d %q, want 303 to %s", res.StatusCode, res.Header.Get("Location"), adminLoginPath)
	}

	for name, br := range map[string]*panelHarness{"A device 1 (the one that pressed)": a1, "A device 2": a2, "B": b1, "C": c1, "D": d1} {
		if br.signedIn(t) {
			t.Errorf("%s is still signed in after one press", name)
		}
	}
	for _, x := range []everywhereBusiness{a, b, c, d} {
		if n := p.liveSessionsOf(t, x.tenant, x.admin); n != 0 {
			t.Errorf("%s: %d live session(s) left", x.name, n)
		}
	}
	all := []everywhereBusiness{a, b, c, d}
	for _, x := range all {
		var foreign []string
		for _, y := range all {
			if y.tenant != x.tenant {
				foreign = append(foreign, y.tenant.String(), y.admin.String(), y.name)
			}
		}
		foreign = append(foreign, ident.String())
		want := map[uuid.UUID]int{a.tenant: 2, b.tenant: 1, c.tenant: 0, d.tenant: 1}[x.tenant]
		checkOrdinaryLogouts(t, x.name, tenantRows(t, owner, x.tenant, ActionAdminLoggedOut)[before[x.tenant]:], want, x,
			p.sessionsOf(t, x.tenant, x.admin), foreign)
	}
	if n := len(tenantRows(t, owner, a.tenant, ActionTenantWriteRefused)) - refusedBefore; n != 0 {
		t.Errorf("the suspended business got %d tenant.write_refused row(s) for the press", n)
	}
}

// TestSignOutEverywhereDB_AnUnlinkedRowIsTodaysOneBusiness: flag on, a person whose rows are NOT
// linked to an identity (two businesses under one address, today's model) presses in A: A's two
// sessions are revoked and A gets one ordinary sign-out row each -- written by the handler, the
// database wrote none -- while B's session is untouched and B gets no row (today's single-tenant
// path, adminauth.RevokeAll's fallback) -- and its card says "in this business", never "every
// business".
//
// MUTATION (measured): the handler's loop over Revocation.Sessions dropped -> A gets no row, red.
func TestSignOutEverywhereDB_AnUnlinkedRowIsTodaysOneBusiness(t *testing.T) {
	p := newPanelHarnessWith(t, panelHarnessOptions{adminIdentity: true, tenantName: "Unlinked A Ltd"})
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; the cross-business reads are the owner's (real Postgres required)")
	}
	ctx := context.Background()
	owner, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}
	t.Cleanup(func() {
		if err := owner.Close(context.Background()); err != nil {
			t.Errorf("close the owner connection: %v", err)
		}
	})
	a := everywhereBusiness{tenant: p.tenantID, admin: p.adminID, name: "Unlinked A Ltd"}
	b := addBusiness(t, p, owner, uuid.Nil, "Unlinked B Ltd")

	a1, a2, b1 := p, p.anotherBrowser(t), p.anotherBrowser(t)
	a1.signInTo(t, a.tenant)
	a2.signInTo(t, a.tenant)
	b1.signInTo(t, b.tenant)
	beforeA, beforeB := len(tenantRows(t, owner, a.tenant, ActionAdminLoggedOut)), len(tenantRows(t, owner, b.tenant, ActionAdminLoggedOut))
	// The card promises what this press does: this business, not every business.
	if _, page := a1.get(t, accountHref); !strings.Contains(page, "computer in this business") || strings.Contains(page, "every business you") {
		t.Fatal("an unlinked session's card does not say it signs out of this business only")
	}

	res, _ := a1.post(t, accountSignOutEverywhereHref, url.Values{})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != adminLoginPath {
		t.Fatalf("the press answered %d %q", res.StatusCode, res.Header.Get("Location"))
	}
	if a1.signedIn(t) || a2.signedIn(t) {
		t.Error("a device in the pressing business is still signed in")
	}
	if !b1.signedIn(t) {
		t.Error("an unlinked row's press signed the other business out too")
	}
	checkOrdinaryLogouts(t, a.name, tenantRows(t, owner, a.tenant, ActionAdminLoggedOut)[beforeA:], 2, a,
		p.sessionsOf(t, a.tenant, a.admin), []string{b.tenant.String(), b.admin.String(), b.name})
	if n := len(tenantRows(t, owner, b.tenant, ActionAdminLoggedOut)) - beforeB; n != 0 {
		t.Errorf("the other business got %d sign-out row(s)", n)
	}
}
