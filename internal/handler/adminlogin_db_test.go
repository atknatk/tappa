package handler

// End-to-end panel authentication against a REAL Postgres and the REAL router,
// with the REAL adminauth.Manager behind it.
//
// WHY THIS FILE EXISTS BESIDE adminlogin_test.go, which already covers the same
// endpoints with a fake. THE M5-04 LESSON: a capability can be delivered, tested,
// approved — and DEAD in production because the two halves were never assembled.
// There, NewTap built its rate limiter without an audit recorder, so an approved
// M5-03 criterion ("429 plus an audit_log row") was silently untrue in the wired
// product while every test stayed green, because the tests built their own
// limiter. So this file drives what cmd/tappa drives: adminauth.New over the real
// pool, handler.NewAdminAuth over that, the real chi router, real bcrypt, real
// RLS, real audit_log rows counted in the database.
//
// Fixtures are NOT cleaned up (tappa_app has REVOKE DELETE on the admin tables and
// on audit_log, which is the audit guarantee). Fresh random UUIDs and fresh random
// email addresses keep runs from colliding; `make db-reset` clears the dev DB.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/billing"
	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/manual"
	"github.com/atknatk/tappa/internal/domain/review"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The two stored digests these tests authenticate against, as LITERALS at
// bcrypt.MinCost.
//
// 🔬 THEY ARE NOT PRODUCED BY adminauth.Hash, AND THAT IS A MEASUREMENT RATHER
// THAN A SHORTCUT. `make test` runs with -race, and the race detector instruments
// blowfish's key schedule: one cost-12 bcrypt operation measured 11.34 s under
// -race against 0.60 s without -- a 19x slowdown. The first version of this file
// called Hash six times per run and this package went from ~140 s to 571 s.
//
// A bcrypt digest CARRIES ITS OWN COST, so CompareHashAndPassword verifies these
// through exactly the same path as a cost-12 row. Nothing measured in this file is
// a function of the work factor: the subjects are the wired flow, the tenant
// picker, the audit rows and the kill switch. The cost itself is pinned where it
// belongs -- internal/adminauth's TestHash_UsesTheDeclaredCost,
// TestCost_MatchesTheDummyDigest and TestSeedDigests_UseTheDeclaredCost.
//
// NEITHER IS A SECRET: both are digests of passwords written in plain sight three
// lines below, used by no account anywhere but a throwaway test tenant.
const (
	ownerPassword  = "the-owners-real-password"
	victimPassword = "the-victims-own-password"

	ownerDigest  = "$2a$04$C2HaLil2uKea.WgCZODsUukDR.2PDzFdPbnjUyEsiuvwjxXP4Uej."
	victimDigest = "$2a$04$4XU8qZtugczPw48bu2ImTe95AOGWWY5DrobXiGje7k/d2KgOwqrEi"
)

type panelHarness struct {
	server   *httptest.Server
	data     *db.DB
	client   *http.Client
	tenantID uuid.UUID
	adminID  uuid.UUID
	email    string
	password string
}

func newPanelHarness(t *testing.T) *panelHarness {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping panel end-to-end tests (real Postgres required). " +
			"Run `make test` — a bare `go test` skips this silently and proves nothing.")
	}
	data, err := db.New(context.Background(), &config.Config{DatabaseURL: withSmallPool(dsn)})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)

	// THE SERVER IS STARTED BEFORE THE HANDLER IS BUILT, on purpose: the panel's
	// Origin check is STRICT, so cfg.BaseURL must be the address this test really
	// serves from. Building the handler with a made-up BaseURL made every POST
	// answer 400 — which is the check working, and is why the order is this way
	// round rather than the check being relaxed for tests. chi accepts routes added
	// before the first request is served.
	r := chi.NewRouter()
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	cfg := &config.Config{
		Env: config.EnvDev,
		// http, so the test client keeps the cookies (a Secure cookie is dropped
		// over plain http by net/http/cookiejar). The Secure-in-prod guarantee is
		// proven separately and structurally in adminauth's cookie tests.
		BaseURL:        srv.URL,
		SessionHMACKey: []byte("0123456789abcdef0123456789abcdef"),
		// The invite key is SEPARATE from the session key in production and is
		// separate here too: a fixture that shares one key would let a test pass
		// while the two were conflated.
		InviteHMACKey: []byte("fedcba9876543210fedcba9876543210"),
		DatabaseURL:   dsn,
	}

	admins, err := adminauth.New(data, cfg)
	if err != nil {
		t.Fatalf("adminauth.New: %v", err)
	}
	trail, err := audit.New(data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	records, err := ledger.NewReader(data, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("ledger.NewReader: %v", err)
	}
	reviewer, err := review.NewReviewer(data, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("review.NewReviewer: %v", err)
	}
	// The employees section's two write sides, REAL against this database (M6-05
	// phase B): the harness exists so a test drives what production drives, and a
	// fake here would put the one thing these tests are for -- §4.5 and the audit
	// row's fate -- back behind a mock that agrees with whatever it is told.
	staff, err := tenant.NewStaff(data, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("tenant.NewStaff: %v", err)
	}
	invites, err := invite.New(data, cfg)
	if err != nil {
		t.Fatalf("invite.New: %v", err)
	}
	// THE REAL Venues, for the reason the real staff is used two lines up: a fake
	// would put §4.5 and the audit row's fate — the two things these tests exist for
	// — back behind a mock that agrees with whatever it is told.
	venues, err := tenant.NewVenues(data, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("tenant.NewVenues: %v", err)
	}
	// THE REAL Plaques, for the same reason (M6-06 phase B). The plaque path is
	// where §4.7's type wall and the one-transaction replace live; a fake would
	// agree that both hold.
	plaques, err := tenant.NewPlaques(data, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("tenant.NewPlaques: %v", err)
	}
	// THE REAL manual Recorder (M6-08), for the reason every other real dependency in
	// this harness is real: it is the SECOND writer of `transactions` in the product,
	// and the two things its tests exist for -- section 4.5's scope and the audit
	// row's shared fate -- are exactly what a fake would agree with regardless.
	entries, err := manual.NewRecorder(data, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("manual.NewRecorder: %v", err)
	}
	// THE REAL Rulebook (M6-09 phase A), for the reason every other real dependency
	// here is real -- and for one more that is specific to it: the property its tests
	// exist to prove is that rendering the policies screen writes NOTHING, and a fake
	// would agree with that whatever the real reader did. The windows are
	// policy.DefaultParams(): this harness has no checkin.Service, and what these
	// tests assert about the screen is its rows rather than its numbers.
	rules, err := tenant.NewRulebook(data, policy.DefaultParams(), 150, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("tenant.NewRulebook: %v", err)
	}
	// THE REAL billing Book (M6-12 phase B), for the reason every other real
	// dependency here is real -- and for one more that is specific to it: the property
	// its tests exist to prove is that rendering the billing screen FREEZES NOTHING,
	// and a fake would agree with that whatever the real register did. It also takes
	// the harness's real trail, which is what makes "the handler writes no second audit
	// row" a countable claim against audit_log rather than against a double.
	books, err := billing.NewBook(data, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("billing.NewBook: %v", err)
	}
	h, err := NewAdminAuth(admins, trail, records, records, reviewer, staff, invites, venues, plaques, entries, rules, newFakeScribe(), books, cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	h.Mount(r)

	// 🔴 THE ACTIVATION FLOW IS MOUNTED BESIDE THE PANEL (M6-05 phase B, round 3), so
	// one harness can drive the WHOLE loop a manager and an employee share: press
	// invite, then open the link. A security audit could not measure what a stale
	// invitation does because /activate was not on this router and it had to call
	// invite.Manager.Activate directly -- and it said so rather than presenting the
	// result as end to end. This closes that gap: the employee side is now HTTP too.
	sessions, err := session.New(data, cfg)
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	activation, err := NewActivation(invites, sessions, trail, cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewActivation: %v", err)
	}
	activation.Mount(r)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}

	ph := &panelHarness{
		server: srv,
		data:   data,
		client: &http.Client{
			Jar: jar,
			// Redirects are followed by hand so each hop's status can be asserted.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		password: ownerPassword,
		email:    "panel-e2e-" + uuid.NewString() + "@m6.example",
	}

	ph.tenantID = uuid.New()
	if err := data.WithTenant(context.Background(), ph.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Panel E2E Ltd', $2, 'bar', 'single')`,
			// 🔴 THE FULL UUID, NOT THE FIRST 8 HEX DIGITS -- A SEPARATE FIX, MARKED
			// BECAUSE IT IS NOT PART OF M6-06 (approved scope extension, 2026-08-09).
			// vat_number is UNIQUE. Every DB test helper in this repo built it from
			// uuid.String()[:8], i.e. 32 bits, and the development database already
			// held 128 553 distinct values in that space -- so each insert had a
			// 1-in-33 410 chance (2.99e-5) of a 23505 birthday collision, and the
			// odds grow with every suite run because these fixtures are never
			// cleaned up. It FIRED: an audit run of `make test` went red here with
			// tenants_vat_number_key, and a re-run was green -- which is the worst
			// kind of failure, because it makes the suite look flaky rather than
			// wrong and it poisons the measurement ground every later task stands on.
			//
			// PER RUN, which is the number that matters: one `make test` inserts 379
			// tenants (measured, by counting the table before and after), so the
			// chance that a run went red somewhere was 1 - (1 - 2.99e-5)^379 = 1.13%,
			// about ONE RUN IN 89 -- and climbing, because the denominator never
			// shrinks. The full uuid v4 carries 122 random bits: the same figure
			// becomes 9.2e-30. Applied at all 21 truncated call sites across 19 files
			// (measured: 0 left), and `make test` was then run TWICE, both green.
			ph.tenantID, "VAT-"+ph.tenantID.String())
		return e
	}); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	ph.adminID = uuid.New()
	if err := data.WithTenant(context.Background(), ph.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
			 VALUES ($1, $2, 'E2E Owner', $3, $4, 'owner', 'active')`,
			ph.adminID, ph.tenantID, ph.email, ownerDigest)
		return e
	}); err != nil {
		t.Fatalf("insert admin: %v", err)
	}
	return ph
}

func (p *panelHarness) get(t *testing.T, path string) (*http.Response, string) {
	t.Helper()
	res, err := p.client.Get(p.server.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return res, readAll(t, res)
}

func (p *panelHarness) post(t *testing.T, path string, form url.Values) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, p.server.URL+path, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", p.server.URL)
	res, err := p.client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return res, readAll(t, res)
}

// setCookie puts a cookie back into the jar by hand.
//
// 🔴 IT EXISTS TO MAKE A CLIENT UNCOOPERATIVE ON PURPOSE. A test whose client honours
// every Set-Cookie can only ever measure what a well-behaved browser does; when the
// question is what the SERVER keeps, the client has to be allowed to misbehave — which
// is all a script does. Used by
// TestPanelEmployeesDB_ARepeatedConfirmationWritesOnlyOnce.
func (p *panelHarness) setCookie(t *testing.T, name, value string) {
	t.Helper()
	u, err := url.Parse(p.server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	p.client.Jar.SetCookies(u, []*http.Cookie{{Name: name, Value: value, Path: "/"}})
}

func readAll(t *testing.T, res *http.Response) string {
	t.Helper()
	defer res.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := res.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}

// auditCount reads the real audit_log, in the tenant's own context, so the number
// is a database fact rather than a fake's counter.
func (p *panelHarness) auditCount(t *testing.T, action string) int {
	t.Helper()
	var n int
	err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2 AND target = $3`,
			p.tenantID, action, p.adminID.String()).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	return n
}

// TestPanelE2E_SignInAndOut walks the whole wired flow against real Postgres.
func TestPanelE2E_SignInAndOut(t *testing.T) {
	p := newPanelHarness(t)

	// The gate refuses before anything.
	res, _ := p.get(t, "/admin")
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin/login" {
		t.Fatalf("unauthenticated /admin: %d %q, want 303 /admin/login", res.StatusCode, res.Header.Get("Location"))
	}

	res, page := p.get(t, "/admin/login")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/login: %d", res.StatusCode)
	}
	csrf := csrfFrom(t, page)

	// A WRONG password first, so the success below cannot be passing because the
	// endpoint accepts anything.
	res, _ = p.post(t, "/admin/login", url.Values{
		"csrf": {csrf}, "email": {p.email}, "password": {"not-the-password"},
	})
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d, want 401", res.StatusCode)
	}
	if got := p.auditCount(t, ActionAdminLoginFailed); got != 1 {
		t.Fatalf("audit_log has %d admin.login.failed rows, want 1 — the criterion 'failed "+
			"sign-ins are written to audit_log' is measured HERE, in the database", got)
	}

	// The real thing.
	res, _ = p.post(t, "/admin/login", url.Values{
		"csrf": {csrf}, "email": {p.email}, "password": {p.password},
	})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin" {
		t.Fatalf("sign-in: %d %q, want 303 /admin", res.StatusCode, res.Header.Get("Location"))
	}
	if got := p.auditCount(t, ActionAdminLoginSucceeded); got != 1 {
		t.Fatalf("%d admin.login.succeeded rows, want 1", got)
	}

	// A session row really exists, and last_login_at was stamped.
	var sessions, stamped int
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if e := tx.QueryRow(ctx,
			`SELECT count(*) FROM admin_sessions WHERE tenant_id = $1 AND admin_user_id = $2 AND revoked_at IS NULL`,
			p.tenantID, p.adminID).Scan(&sessions); e != nil {
			return e
		}
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM admin_users WHERE id = $1 AND tenant_id = $2 AND last_login_at IS NOT NULL`,
			p.adminID, p.tenantID).Scan(&stamped)
	}); err != nil {
		t.Fatalf("read session state: %v", err)
	}
	if sessions != 1 {
		t.Fatalf("%d live admin sessions, want 1", sessions)
	}
	if stamped != 1 {
		t.Fatalf("last_login_at was not stamped")
	}

	// The gate now lets the request through, and the page names the operator.
	res, panel := p.get(t, "/admin")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("authenticated /admin: %d", res.StatusCode)
	}
	if !strings.Contains(panel, "E2E Owner") {
		t.Fatalf("the panel does not name the signed-in operator")
	}
	// And it lands on the panel shell M6-02 shipped: every section is there,
	// every one of them empty and saying which task fills it. Asserted through the
	// SECTION TABLE rather than against a sentence, so this stays true as the
	// sections are filled in one by one.
	for _, sec := range pages.PanelSections {
		// htmlText, not the raw label: "Locations & Wall Tags" reaches the page as
		// "Locations &amp; Wall Tags", and the first version of this loop compared the
		// unescaped string and failed on exactly that section.
		if !strings.Contains(panel, htmlText(sec.Label)) {
			t.Fatalf("the panel does not offer the %q section", sec.Label)
		}
	}
	if !strings.Contains(panel, "Nothing here yet") {
		t.Fatalf("the default section does not say it is empty")
	}

	// Sign out: the row is revoked in the database, not merely forgotten by the
	// browser.
	res, _ = p.post(t, "/admin/logout", url.Values{})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("sign-out: %d, want 303", res.StatusCode)
	}
	var live int
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM admin_sessions WHERE tenant_id = $1 AND admin_user_id = $2 AND revoked_at IS NULL`,
			p.tenantID, p.adminID).Scan(&live)
	}); err != nil {
		t.Fatalf("read session state: %v", err)
	}
	if live != 0 {
		t.Fatalf("%d live sessions after sign-out — the cookie was cleared but the row was not "+
			"revoked, so a copied cookie still works", live)
	}
	if got := p.auditCount(t, ActionAdminLoggedOut); got != 1 {
		t.Fatalf("%d sign-out rows, want 1", got)
	}

	res, _ = p.get(t, "/admin")
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("after sign-out /admin: %d, want 303", res.StatusCode)
	}
}

// TestPanelE2E_DisabledAdminCannotStaySignedIn covers both halves of the
// kill switch through the wired product: the login refuses, and an ALREADY signed
// in admin stops passing on the next request without any revocation.
func TestPanelE2E_DisabledAdminCannotStaySignedIn(t *testing.T) {
	p := newPanelHarness(t)

	_, page := p.get(t, "/admin/login")
	csrf := csrfFrom(t, page)
	res, _ := p.post(t, "/admin/login", url.Values{
		"csrf": {csrf}, "email": {p.email}, "password": {p.password},
	})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("control: sign-in failed (%d)", res.StatusCode)
	}
	if res, _ := p.get(t, "/admin"); res.StatusCode != http.StatusOK {
		t.Fatalf("control: /admin refused a fresh session (%d)", res.StatusCode)
	}

	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE admin_users SET status = 'disabled' WHERE id = $1 AND tenant_id = $2`,
			p.adminID, p.tenantID)
		return e
	}); err != nil {
		t.Fatalf("disable: %v", err)
	}

	res, _ = p.get(t, "/admin")
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("a disabled admin still reaches the panel: %d, want 303", res.StatusCode)
	}

	// And a fresh sign-in is refused with the SAME response a wrong password gets.
	_, page = p.get(t, "/admin/login")
	res, disabledBody := p.post(t, "/admin/login", url.Values{
		"csrf": {csrfFrom(t, page)}, "email": {p.email}, "password": {p.password},
	})
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("disabled sign-in: %d, want 401", res.StatusCode)
	}
	if strings.Contains(strings.ToLower(disabledBody), "disabled") {
		t.Fatalf("the response names the disabled account (PHASE B OBLIGATION 1)")
	}
}

// TestPanelE2E_TenantPickerWithRealRows drives the two-business flow end to end
// against real Postgres — including the refusal of a tenant the password never
// verified against.
func TestPanelE2E_TenantPickerWithRealRows(t *testing.T) {
	p := newPanelHarness(t)

	// A SECOND business for the same person, same password: the case the picker
	// exists for.
	secondTenant := uuid.New()
	if err := p.data.WithTenant(context.Background(), secondTenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Second Business Ltd', $2, 'restaurant', 'single')`,
			secondTenant, "VAT-"+secondTenant.String())
		return e
	}); err != nil {
		t.Fatalf("insert second tenant: %v", err)
	}
	secondAdmin := uuid.New()
	if err := p.data.WithTenant(context.Background(), secondTenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
			 VALUES ($1, $2, 'Second Manager', $3, $4, 'manager', 'active')`,
			secondAdmin, secondTenant, p.email, ownerDigest)
		return e
	}); err != nil {
		t.Fatalf("insert second admin: %v", err)
	}

	// A THIRD business the password does NOT verify against — the victim in
	// 00011's measured attack.
	victimTenant := uuid.New()
	if err := p.data.WithTenant(context.Background(), victimTenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'VictimCo', $2, 'bar', 'single')`,
			victimTenant, "VAT-"+victimTenant.String())
		return e
	}); err != nil {
		t.Fatalf("insert victim tenant: %v", err)
	}
	victimAdmin := uuid.New()
	if err := p.data.WithTenant(context.Background(), victimTenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
			 VALUES ($1, $2, 'Victim Owner', $3, $4, 'owner', 'active')`,
			victimAdmin, victimTenant, p.email, victimDigest)
		return e
	}); err != nil {
		t.Fatalf("insert victim admin: %v", err)
	}

	_, page := p.get(t, "/admin/login")
	res, _ := p.post(t, "/admin/login", url.Values{
		"csrf": {csrfFrom(t, page)}, "email": {p.email}, "password": {p.password},
	})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin/login/choose" {
		t.Fatalf("two verified businesses: %d %q, want 303 to the picker",
			res.StatusCode, res.Header.Get("Location"))
	}

	res, picker := p.get(t, "/admin/login/choose")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET the picker: %d", res.StatusCode)
	}
	// 🔴 THE PICKER OFFERS EXACTLY THE VERIFIED SET.
	if !strings.Contains(picker, "Panel E2E Ltd") || !strings.Contains(picker, "Second Business Ltd") {
		t.Fatalf("the picker does not offer both verified businesses")
	}
	if strings.Contains(picker, "VictimCo") || strings.Contains(picker, victimTenant.String()) {
		t.Fatalf("the picker offers VictimCo — the password never verified against it. This is "+
			"the cross-tenant authentication bypass migration 00011 measured (section 4.5). "+
			"Picker body: %q", picker)
	}

	// Posting the victim's tenant anyway must not issue anything.
	res, _ = p.post(t, "/admin/login/choose", url.Values{
		"csrf": {csrfFrom(t, picker)}, "tenant_id": {victimTenant.String()},
	})
	if res.StatusCode == http.StatusSeeOther && res.Header.Get("Location") == "/admin" {
		t.Fatalf("posting an unverified tenant signed somebody in")
	}
	var victimSessions int
	if err := p.data.WithTenant(context.Background(), victimTenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM admin_sessions WHERE tenant_id = $1`, victimTenant).Scan(&victimSessions)
	}); err != nil {
		t.Fatalf("count victim sessions: %v", err)
	}
	if victimSessions != 0 {
		t.Fatalf("%d sessions exist in the VICTIM's tenant — cross-tenant authentication bypass", victimSessions)
	}

	// POSITIVE CONTROL: a legitimate choice does work. The refusal above must not
	// be "the picker is broken".
	_, page = p.get(t, "/admin/login")
	p.post(t, "/admin/login", url.Values{
		"csrf": {csrfFrom(t, page)}, "email": {p.email}, "password": {p.password},
	})
	_, picker = p.get(t, "/admin/login/choose")
	res, _ = p.post(t, "/admin/login/choose", url.Values{
		"csrf": {csrfFrom(t, picker)}, "tenant_id": {secondTenant.String()},
	})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin" {
		t.Fatalf("control: a legitimate choice was refused (%d %q)", res.StatusCode, res.Header.Get("Location"))
	}
	var secondSessions int
	if err := p.data.WithTenant(context.Background(), secondTenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM admin_sessions WHERE tenant_id = $1 AND admin_user_id = $2`,
			secondTenant, secondAdmin).Scan(&secondSessions)
	}); err != nil {
		t.Fatalf("count second sessions: %v", err)
	}
	if secondSessions != 1 {
		t.Fatalf("%d sessions in the chosen business, want 1", secondSessions)
	}
}

// 🔴 TestPanelE2E_TimingIsFlatOverHTTP reproduces, at the HTTP boundary, the
// measurement a security audit used to break PHASE B OBLIGATION 2.
//
// THE HOLE IT GUARDS: adminauth.Compare used to return false WITHOUT running
// bcrypt when the password exceeded 72 bytes, and Authenticate paid the dummy only
// when NO candidate resolved. So an over-long password against a REGISTERED
// address did no bcrypt at all, while the same password against an unregistered
// one paid the full dummy. The body and the status were identical in every cell;
// the CLOCK was not. Measured by the audit on this stack:
//
//	 20 bytes, known   299.18 ms | 20 bytes, unknown  293.65 ms ->  0.98x
//	100 bytes, known     5.53 ms | 100 bytes, unknown 295.42 ms -> 53.43x
//
// One 100-byte probe told an attacker whether an address was a panel
// administrator, with certainty, in a single request — and cost the server no
// bcrypt at all, so the budget that bounds CPU was not even engaged.
//
// WHY IT NEEDS A COST-12 ROW and cannot use the cheap fixture digests the rest of
// this file uses: the dummy is a cost-12 digest by construction, so a cost-4 row
// would make the arms differ for a reason that has nothing to do with the bug. The
// harness's own admin is re-hashed at the shipped cost for this test only.
//
// IT SKIPS UNDER -short: it is a wall-clock SAMPLE, like its domain-level twin.
// The obligation is still proven under -short by the FOUR structural tests (three
// failure shapes) named in internal/adminauth/manager_timing_test.go's header —
// including TestAuthenticate_OverLongPasswordStillPaysBcrypt, which covers exactly
// the shape THIS test samples.
func TestPanelE2E_TimingIsFlatOverHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: skipping the HTTP wall-clock SAMPLE (4 cells x N cost-12 logins). " +
			"PHASE B OBLIGATION 2 is proven under -short by adminauth's FOUR structural " +
			"tests across THREE failure shapes — see manager_timing_test.go's header.")
	}
	p := newPanelHarness(t)

	// Re-hash the harness admin at the SHIPPED cost so the row and the dummy agree.
	realDigest, err := adminauth.Hash(p.password)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE admin_users SET password_hash = $1 WHERE id = $2 AND tenant_id = $3`,
			realDigest, p.adminID, p.tenantID)
		return e
	}); err != nil {
		t.Fatalf("re-hash: %v", err)
	}

	_, page := p.get(t, "/admin/login")
	csrf := csrfFrom(t, page)

	// ⚠️ SAMPLES ARE 2, AND THE BUDGET IS WHY. adminAttemptLimit is 10 FAILED
	// logins per address per window, and every cell here is a failure. The first
	// version used n=3 and the twelfth request came back 429 instead of 401 — the
	// rate limiter working exactly as designed, and a reminder that a timing probe
	// is itself abuse-shaped. 4 cells x 2 = 8 stays under the budget rather than
	// raising it for a test.
	//
	// The estimator is the MINIMUM, not the median: with two samples a "median" is
	// meaningless, and the minimum is the standard robust choice for wall-clock
	// timing because scheduler noise only ever adds. The signal being guarded is
	// 53x, so two samples are ample.
	const samples = 2
	median := func(email, password string) time.Duration {
		var d []time.Duration
		for i := 0; i < samples; i++ {
			start := time.Now()
			res, _ := p.post(t, "/admin/login", url.Values{
				"csrf": {csrf}, "email": {email}, "password": {password},
			})
			d = append(d, time.Since(start))
			// Every cell must be the SAME refusal, or the timing question is moot.
			if res.StatusCode != http.StatusUnauthorized {
				t.Fatalf("email=%q len(password)=%d answered %d, want 401",
					email, len(password), res.StatusCode)
			}
		}
		sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
		return d[0]
	}

	unknownEmail := "nobody-" + uuid.NewString() + "@m6.example"
	short := strings.Repeat("a", 20)
	long := strings.Repeat("a", 100)

	cells := []struct {
		name     string
		email    string
		password string
		med      time.Duration
	}{
		{name: " 20 bytes, known email", email: p.email, password: short},
		{name: " 20 bytes, UNKNOWN email", email: unknownEmail, password: short},
		{name: "100 bytes, known email", email: p.email, password: long},
		{name: "100 bytes, UNKNOWN email", email: unknownEmail, password: long},
	}
	var slowest, fastest time.Duration
	for i := range cells {
		cells[i].med = median(cells[i].email, cells[i].password)
		t.Logf("%-26s min=%v (n=%d)", cells[i].name, cells[i].med, samples)
		if i == 0 || cells[i].med > slowest {
			slowest = cells[i].med
		}
		if i == 0 || cells[i].med < fastest {
			fastest = cells[i].med
		}
	}
	ratio := float64(slowest) / float64(fastest)
	t.Logf("HTTP spread across all four cells: slowest/fastest = %.2fx", ratio)

	// The band is wide because this measures a whole HTTP + Postgres round trip
	// under -race, where the audit's signal was 53x. Anything near 1 is right;
	// anything near 50 is the hole reopened. httpTimingGate is pinned as a PROPERTY
	// by TestHTTPTimingGate_IsBetweenTheNoiseAndTheSignal — a family re-scan showed
	// every timing threshold in this suite could be widened silently.
	const gate = httpTimingGate
	if ratio >= gate {
		t.Fatalf("the four cells are distinguishable by TIME: spread %.2fx (gate %.2fx). "+
			"An over-long password against a REGISTERED address must cost the same as "+
			"against an unregistered one — see adminauth.Compare (PHASE B OBLIGATION 2)", ratio, gate)
	}
}

// httpTimingGate bounds the spread across the four HTTP cells.
const httpTimingGate = 4.0

// TestHTTPTimingGate_IsBetweenTheNoiseAndTheSignal pins that gate as a RELATION,
// for the reason its adminauth twin gives: the tempting repair for a flaky timing
// test is to widen its gate, and a widened gate is a disarmed one.
//
// THE ANCHORS ARE MEASUREMENTS on this stack:
//
//	observed noise   1.03-1.05x   four cells, correct code, real Postgres, -race
//	                              (an audit's independent run: 6 cells x n=10 -> 1.05x)
//	the signal       29.65x       the audit's mutation of the fixed Compare;
//	                              47.75x locally, 53.43x in the original report
func TestHTTPTimingGate_IsBetweenTheNoiseAndTheSignal(t *testing.T) {
	const (
		worstObservedNoise = 1.05
		weakestSignal      = 29.65
	)
	if httpTimingGate <= worstObservedNoise {
		t.Fatalf("httpTimingGate is %.2fx, at or below observed noise (%.2fx) — it will flake",
			httpTimingGate, worstObservedNoise)
	}
	if httpTimingGate >= weakestSignal {
		t.Fatalf("httpTimingGate is %.2fx, at or above the WEAKEST observed instance of the "+
			"oracle it exists to catch (%.2fx). Widening it this far means the HTTP half of "+
			"PHASE B OBLIGATION 2 catches nothing", httpTimingGate, weakestSignal)
	}
	t.Logf("httpTimingGate %.2fx sits %.1fx above observed noise and %.1fx below the weakest signal",
		httpTimingGate, httpTimingGate/worstObservedNoise, weakestSignal/httpTimingGate)
}

// withSmallPool caps this package's test pool.
// 🔬 THE POOL IS DELIBERATELY SMALL, AND THE REASON IS ARITHMETIC ABOUT THE WHOLE
// SUITE, not about this package.
//
// Postgres here runs max_connections=100 with superuser_reserved_connections=3, so
// 97 slots are available to tappa_app. Two PRE-EXISTING red-line race tests each
// open a pool of 54 (50 racing goroutines + 4):
//
//	internal/db/invites_test.go   raceInviteGoroutines = 50  (section 4.4 single-use)
//	internal/sun/advance_test.go  raceGoroutines       = 50  (section 4.4 replay)
//
// 54 + 54 = 108 > 97 ON THEIR OWN, so whenever those two packages overlap the
// suite is already over the limit. Measured once during M6-01 phase B: a full
// `make test` failed with "FATAL: sorry, too many clients already (SQLSTATE
// 53300)" inside the invite race, while this package was running concurrently.
//
// THIS PACKAGE IS NOT THE CAUSE and could not be — but it is one more concurrent
// pool, and pgx's default is max(4, NumCPU), which on this machine is more than
// this package will ever use: every test here is sequential. Capping it at 4 keeps
// M6-01's marginal contribution to the peak as small as it can honestly be made.
// The underlying collision belongs to internal/db and internal/sun and is NOT
// fixed here: lowering either goroutine count would weaken a red-line test.
func withSmallPool(dsn string) string {
	if strings.Contains(dsn, "pool_max_conns") {
		return dsn
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + "pool_max_conns=4"
}
