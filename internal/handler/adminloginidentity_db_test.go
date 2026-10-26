package handler

// adminloginidentity_db_test.go -- M14-09 over HTTP against real Postgres with the real
// adminauth.Manager and TAPPA_ADMIN_IDENTITY on: the identity path signs a person straight in, lands
// them where K-4 says, keeps the picker for several verified legacy rows, writes no tenant audit row
// that names another business (R21), answers a lapsed choice with "sign in again", and takes one
// time over the five failure cases (R14). The identities are COMMITTED (the server reads them on its
// own connections); the harness holds the operator-tables lock while they live, and idPanel takes
// every one back.

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// idPanel is a panel harness with the identity flag on, plus an owner connection for the identity
// fixtures and the reads across businesses.
type idPanel struct {
	*panelHarness
	owner *pgx.Conn
}

// newIDPanel builds the harness (o.identity is forced on) and the owner connection. Every identity
// made for an address this panel used is taken back at the end: its rows unlinked, its events and
// the identity deleted.
func newIDPanel(t *testing.T, o panelHarnessOptions) *idPanel {
	t.Helper()
	o.identity = true
	p := &idPanel{panelHarness: newPanelHarnessWith(t, o)}
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; the identity fixtures are written as the owner (real Postgres required)")
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
	p.owner = owner
	return p
}

func (p *idPanel) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := p.owner.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func (p *idPanel) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := p.owner.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// identity makes an identity for email with digest and registers its take-back.
func (p *idPanel) identity(t *testing.T, email, digest string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := p.owner.QueryRow(context.Background(), `INSERT INTO admin_identities (email, password_hash) VALUES ($1, $2) RETURNING id`, email, digest).Scan(&id); err != nil {
		t.Fatalf("insert an identity: %v", err)
	}
	t.Cleanup(func() {
		for _, sql := range []string{
			`UPDATE admin_users SET identity_id = NULL, linked_via = NULL, linked_at = NULL, unlinked_at = NULL WHERE identity_id = $1`,
			`DELETE FROM admin_identity_events WHERE identity_id = $1`,
			`DELETE FROM admin_identities WHERE id = $1`,
		} {
			if _, err := p.owner.Exec(context.Background(), sql, id); err != nil {
				t.Errorf("take the identity back (%s): %v", sql, err)
			}
		}
	})
	return id
}

// tenant makes a business.
func (p *idPanel) tenant(t *testing.T, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	p.exec(t, `INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, $2, $3, 'bar', 'single')`, id, name, "VAT-M1409-"+id.String())
	return id
}

// row makes an admin row of email in tenant; age backdates created_at (the oldest-active landing).
func (p *idPanel) row(t *testing.T, tenant uuid.UUID, email, digest, status string, age time.Duration) uuid.UUID {
	t.Helper()
	id := uuid.New()
	p.exec(t, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status, created_at)
	           VALUES ($1, $2, 'M14-09 Person', $3, $4, 'owner', $5, now() - $6::interval)`,
		id, tenant, email, digest, status, age.String())
	return id
}

// link links a row to an identity (the way the backfill did).
func (p *idPanel) link(t *testing.T, row, ident uuid.UUID) {
	t.Helper()
	p.exec(t, `UPDATE admin_users SET identity_id = $2, linked_via = 'backfill', linked_at = now() WHERE id = $1`, row, ident)
}

// login loads the form and posts the address and password, from the harness's browser.
func (p *idPanel) login(t *testing.T, email, password string) (*http.Response, string) {
	t.Helper()
	_, page := p.get(t, "/admin/login")
	return p.post(t, "/admin/login", url.Values{"csrf": {csrfFrom(t, page)}, "email": {email}, "password": {password}})
}

// landedIn is the business of the newest session of any of rows.
func (p *idPanel) landedIn(t *testing.T, rows ...uuid.UUID) uuid.UUID {
	t.Helper()
	var tenant uuid.UUID
	if err := p.owner.QueryRow(context.Background(),
		`SELECT tenant_id FROM admin_sessions WHERE admin_user_id = ANY($1) ORDER BY created_at DESC LIMIT 1`, rows).Scan(&tenant); err != nil {
		t.Fatalf("the newest session: %v", err)
	}
	return tenant
}

// TestPanelIdentityLogin_OneMembershipSignsStraightIn: a person whose identity has one membership
// signs in with no picker -- 303 to /admin, the panel answers with their session -- and the business
// holds exactly ONE admin.login.succeeded row for it: identity_open_session's (the M14-07 audit's
// carry-over: the handler writes no second one). MUTATION: completePersonLogin records
// adminLoginSucceededEvent -> 2 rows, red.
func TestPanelIdentityLogin_OneMembershipSignsStraightIn(t *testing.T) {
	p := newIDPanel(t, panelHarnessOptions{})
	p.link(t, p.adminID, p.identity(t, p.email, ownerDigest))

	res, _ := p.login(t, p.email, p.password)
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin" {
		t.Fatalf("sign-in: %d to %q, want 303 to /admin", res.StatusCode, res.Header.Get("Location"))
	}
	shell, body := p.get(t, "/admin")
	if shell.StatusCode != http.StatusOK || !strings.Contains(body, "E2E Owner") {
		t.Fatalf("the panel after an identity sign-in: %d", shell.StatusCode)
	}
	if n := p.auditCount(t, ActionAdminLoginSucceeded); n != 1 {
		t.Fatalf("%d admin.login.succeeded rows for one identity sign-in, want identity_open_session's one", n)
	}
	if n := p.count(t, `SELECT count(*) FROM admin_sessions WHERE tenant_id = $1 AND admin_user_id = $2 AND revoked_at IS NULL`, p.tenantID, p.adminID); n != 1 {
		t.Fatalf("%d live sessions, want 1", n)
	}
}

// TestPanelIdentityLogin_LandsOnTheLastBusinessElseTheOldestActive is K-4 over HTTP: with two
// memberships the person lands in the one used last; with that one disabled, in the oldest active
// one -- never a picker.
func TestPanelIdentityLogin_LandsOnTheLastBusinessElseTheOldestActive(t *testing.T) {
	p := newIDPanel(t, panelHarnessOptions{})
	ident := p.identity(t, p.email, ownerDigest)
	p.link(t, p.adminID, ident)
	older := p.tenant(t, "M14-09 Older Business")
	olderRow := p.row(t, older, p.email, ownerDigest, "active", 72*time.Hour)
	p.link(t, olderRow, ident)

	// The harness's business was used last.
	p.exec(t, `UPDATE admin_identities SET last_used_tenant = $2 WHERE id = $1`, ident, p.tenantID)
	if res, _ := p.login(t, p.email, p.password); res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin" {
		t.Fatalf("sign-in: %d to %q", res.StatusCode, res.Header.Get("Location"))
	}
	if got := p.landedIn(t, p.adminID, olderRow); got != p.tenantID {
		t.Fatalf("landed in %s, want the business used last %s", got, p.tenantID)
	}

	// That membership disabled: the oldest active one.
	p.exec(t, `UPDATE admin_users SET status = 'disabled' WHERE id = $1`, p.adminID)
	if res, _ := p.login(t, p.email, p.password); res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin" {
		t.Fatalf("sign-in with the last business disabled: %d to %q", res.StatusCode, res.Header.Get("Location"))
	}
	if got := p.landedIn(t, p.adminID, olderRow); got != older {
		t.Fatalf("landed in %s, want the oldest active business %s", got, older)
	}
}

// TestPanelIdentityLogin_SeveralLegacyRowsKeepThePickerAndNameNoOtherBusiness: the identity's
// password is not the one typed, but two UNLINKED rows of the address verify it -- the legacy arm
// keeps the picker until contract (§5.6, bulgu 10). Along the way every audit row the flow writes
// is scanned (R21): a choice refused for a business outside the verified set, a successful choice,
// and a failed sign-in (the legacy rows' own rows; the identity's failure is an event row) -- none
// carries the four counting fields, the plural reason texts (r21Words) or the identity's id.
func TestPanelIdentityLogin_SeveralLegacyRowsKeepThePickerAndNameNoOtherBusiness(t *testing.T) {
	p := newIDPanel(t, panelHarnessOptions{})
	ident := p.identity(t, p.email, ownerDigest)
	p.link(t, p.adminID, ident)
	tA, tB := p.tenant(t, "M14-09 Legacy A"), p.tenant(t, "M14-09 Legacy B")
	p.row(t, tA, p.email, victimDigest, "active", 2*time.Hour)
	rowB := p.row(t, tB, p.email, victimDigest, "active", time.Hour)

	toPicker := func() string {
		t.Helper()
		res, _ := p.login(t, p.email, victimPassword)
		if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin/login/choose" {
			t.Fatalf("two verified legacy rows: %d to %q, want 303 to the picker", res.StatusCode, res.Header.Get("Location"))
		}
		_, page := p.get(t, "/admin/login/choose")
		if !strings.Contains(page, "M14-09 Legacy A") || !strings.Contains(page, "M14-09 Legacy B") {
			t.Fatalf("the picker does not offer both legacy businesses")
		}
		return page
	}

	// A choice outside the verified set (the harness's own business -- the identity's -- did not
	// verify this password): refused, its row in a verified business.
	page := toPicker()
	if res, _ := p.post(t, "/admin/login/choose", url.Values{"csrf": {csrfFrom(t, page)}, "tenant_id": {p.tenantID.String()}}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("a refused choice answered %d", res.StatusCode)
	}
	// The choice of B.
	page = toPicker()
	if res, _ := p.post(t, "/admin/login/choose", url.Values{"csrf": {csrfFrom(t, page)}, "tenant_id": {tB.String()}}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("the choice of B answered %d", res.StatusCode)
	}
	if n := p.count(t, `SELECT count(*) FROM admin_sessions WHERE tenant_id = $1 AND admin_user_id = $2`, tB, rowB); n != 1 {
		t.Fatalf("%d sessions in the chosen business, want 1", n)
	}
	// A failure.
	if res, _ := p.login(t, p.email, "not-anybodys-password"); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a wrong password answered %d", res.StatusCode)
	}

	tenants := []uuid.UUID{p.tenantID, tA, tB}
	for _, c := range []struct {
		tenant uuid.UUID
		action string
		want   int
	}{
		{tA, ActionAdminLoginRefused, 1},
		{tB, ActionAdminLoginSucceeded, 1},
		{tA, ActionAdminLoginFailed, 1},
		{tB, ActionAdminLoginFailed, 1},
		{p.tenantID, ActionAdminLoginFailed, 0},
	} {
		if n := p.count(t, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2`, c.tenant, c.action); n != c.want {
			t.Errorf("%s in %s: %d rows, want %d", c.action, c.tenant, n, c.want)
		}
	}
	if n := p.count(t, `SELECT count(*) FROM admin_identity_events WHERE identity_id = $1 AND event = 'login_failed'`, ident); n != 1 {
		t.Errorf("%d login_failed events for the identity, want 1 (the failure is the identity's, not a business's)", n)
	}
	rows, err := p.owner.Query(context.Background(), `SELECT action, detail::text FROM audit_log WHERE tenant_id = ANY($1)`, tenants)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	scanned := 0
	for rows.Next() {
		var action, detail string
		if err := rows.Scan(&action, &detail); err != nil {
			t.Fatal(err)
		}
		scanned++
		for _, k := range append(append([]string{ident.String()}, r21Keys...), r21Words...) {
			if strings.Contains(strings.ToLower(detail), strings.Trim(k, `"`)) {
				t.Errorf("%s row names %s: %s", action, k, detail)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if scanned < 4 {
		t.Fatalf("scanned %d audit rows; the flow wrote four (refused, succeeded, two failed)", scanned)
	}
}

// TestPanelLogin_AChoiceWhosePasswordChangedAsksToSignInAgain: two businesses verified, the picker
// is open, and the chosen row's password changes before the choice is posted. Issue's proof no longer
// holds (ErrProofStale): the answer is "Please sign in again" (409), not a 500, and no session is
// written there. Flag off -- completeLogin is shared by both paths. MUTATION: completeLogin's
// SignInLapsed branch removed -> 500, red.
func TestPanelLogin_AChoiceWhosePasswordChangedAsksToSignInAgain(t *testing.T) {
	p := newPanelHarness(t)
	second := uuid.New()
	if err := p.data.WithTenant(context.Background(), second, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure)
		                          VALUES ($1, 'M14-09 Second Ltd', $2, 'restaurant', 'single')`, second, "VAT-"+second.String()); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
		                      VALUES ($1, $2, 'Second Manager', $3, $4, 'manager', 'active')`, uuid.New(), second, p.email, ownerDigest)
		return e
	}); err != nil {
		t.Fatalf("the second business: %v", err)
	}
	_, page := p.get(t, "/admin/login")
	res, _ := p.post(t, "/admin/login", url.Values{"csrf": {csrfFrom(t, page)}, "email": {p.email}, "password": {p.password}})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin/login/choose" {
		t.Fatalf("two verified businesses: %d to %q, want the picker", res.StatusCode, res.Header.Get("Location"))
	}
	_, page = p.get(t, "/admin/login/choose")
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE admin_users SET password_hash = $1 WHERE id = $2 AND tenant_id = $3`, victimDigest, p.adminID, p.tenantID)
		return e
	}); err != nil {
		t.Fatalf("change the password: %v", err)
	}
	res, body := p.post(t, "/admin/login/choose", url.Values{"csrf": {csrfFrom(t, page)}, "tenant_id": {p.tenantID.String()}})
	if res.StatusCode != http.StatusConflict || !strings.Contains(body, problemAdminSignInAgain.Title) {
		t.Fatalf("the stale choice answered %d, want 409 and %q", res.StatusCode, problemAdminSignInAgain.Title)
	}
	if n := p.liveSessionCount(t); n != 0 {
		t.Fatalf("%d live sessions in the business whose password changed, want 0", n)
	}
}

// The two cost-12 digests the five-case timing sample signs in against, as literals (the reason
// ownerDigest is one: one cost-12 Hash is ~11 s under -race). Neither is a secret: the identity's is
// the digest of identityPassword12, the legacy row's of the-legacy-rows-cost-12-password, used by no
// account but a throwaway test business (scripts/secretscan.sh waives the two lines).
const (
	identityPassword12 = "the-identitys-cost-12-password"
	identityDigest12   = "$2a$12$7l/JBEr2/riraMBQtbK3VejVylJGw9kYc7CwoPLi5L7gUGyolpe4q"
	legacyDigest12     = "$2a$12$BvKPY/AobrIJp9DLe5t9fOfnpjP6FTbbOxzQzPlB8NU9c.MVHokoq"
)

// TestPanelE2E_IdentityTimingIsFlatOverHTTP is ADR 0031 §5.6's five failure cases (R14, S15) at the
// HTTP boundary, with the identity flag on: an identity or none x an unlinked row or none (an
// unknown address included), and the right password on an identity whose every membership is
// disabled. Every cell is the same 401 and the slowest is within httpTimingGate of the fastest.
//
// WHY COST 12 (the M14-07 carry-over measured the five at bcrypt.MinCost inside adminauth; this is
// the HTTP twin): the unknown address pads with the manager's dummy, which is cost 12 in the wired
// product and cannot be made cheaper from this package -- so every row here is cost 12 too, or the
// cells would differ by the fixtures. The identity's event row is written inside the measured
// request in three of the five (failLogin's pattern; S15). One sample per cell, the minimum being
// the single observation: TestPanelE2E_TimingIsFlatOverHTTP's argument, and its gate.
//
// ⚠️ WHAT httpTimingGate (4.0x) CAN SEE HERE, stated so it is not over-read (M14-09's security
// audit): only a GROSS loss of the padding -- a cell that skips most of its nine comparisons (the
// mutation "an unknown address compares nothing" is red). A cell short by one or two comparisons
// (~1.1-1.3x), or a decoy whose wait is gone, passes this gate. The exact proof of the five cases'
// one shape -- every exit's comparison count and call sequence -- is adminauth's
// TestAuthenticatePerson_EveryFailureIsOneShape; this sample only shows the property survives the
// HTTP and PostgreSQL path at the shipped cost.
func TestPanelE2E_IdentityTimingIsFlatOverHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: skipping the HTTP wall-clock SAMPLE (5 cells x 9 cost-12 comparisons); the five " +
			"cases' comparisons and calls are adminauth's TestAuthenticatePerson_EveryFailureIsOneShape, exactly")
	}
	p := newIDPanel(t, panelHarnessOptions{})
	world := func(name string, withIdentity, linkedActive, linkedDisabled, unlinked bool) string {
		t.Helper()
		email := "m14-09-five-" + name + "-" + uuid.NewString() + "@identity.example"
		var ident uuid.UUID
		if withIdentity {
			ident = p.identity(t, email, identityDigest12)
		}
		if linkedActive || linkedDisabled {
			status := "active"
			if linkedDisabled {
				status = "disabled"
			}
			p.link(t, p.row(t, p.tenant(t, "M14-09 Five "+name), email, identityDigest12, status, 48*time.Hour), ident)
		}
		if unlinked {
			p.row(t, p.tenant(t, "M14-09 Five "+name+" legacy"), email, legacyDigest12, "active", time.Hour)
		}
		return email
	}
	cells := []struct {
		name, email, password string
		took                  time.Duration
	}{
		{name: "identity, no unlinked row, wrong password", email: world("a", true, true, false, false), password: "wrong"},
		{name: "identity and an unlinked row, wrong password", email: world("b", true, true, false, true), password: "wrong"},
		{name: "no identity, an unlinked row, wrong password", email: world("c", false, false, false, true), password: "wrong"},
		{name: "an unknown address", email: "nobody-" + uuid.NewString() + "@identity.example", password: "wrong"},
		{name: "right password, every membership disabled", email: world("e", true, false, true, false), password: identityPassword12},
	}
	_, page := p.get(t, "/admin/login")
	csrf := csrfFrom(t, page)
	var slowest, fastest time.Duration
	for i := range cells {
		start := time.Now()
		res, _ := p.post(t, "/admin/login", url.Values{"csrf": {csrf}, "email": {cells[i].email}, "password": {cells[i].password}})
		cells[i].took = time.Since(start)
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s answered %d, want 401", cells[i].name, res.StatusCode)
		}
		t.Logf("%-46s %v", cells[i].name, cells[i].took)
		if i == 0 || cells[i].took > slowest {
			slowest = cells[i].took
		}
		if i == 0 || cells[i].took < fastest {
			fastest = cells[i].took
		}
	}
	ratio := float64(slowest) / float64(fastest)
	t.Logf("HTTP spread across the five failure cases: slowest/fastest = %.2fx (gate %.2fx)", ratio, httpTimingGate)
	if ratio >= httpTimingGate {
		t.Fatalf("the five failure cases are distinguishable by TIME over HTTP: spread %.2fx (gate %.2fx)", ratio, httpTimingGate)
	}
}
