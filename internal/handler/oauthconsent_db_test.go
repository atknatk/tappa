package handler

// oauthconsent_db_test.go -- the app consent (issue #37, ADR 0047 §4) driven over HTTP against REAL
// Postgres, with the real oauth.Server behind the panel: the registry row, the codes and the grants
// are real rows; the panel session is a fake resolution of a real owner or manager (the domain
// re-reads the role in the approval's own transaction).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/oauth"
	"github.com/atknatk/tappa/test/fixtures"
)

const consentRedirect = "https://consent.example/api/integrations/taptime/callback"

type consentFixture struct {
	t        *testing.T
	data     *db.DB
	owner    *pgx.Conn
	srv      *oauth.Server
	logs     *bytes.Buffer
	tenantID uuid.UUID
	ownerID  uuid.UUID
	manager  uuid.UUID
	client   uuid.UUID
	clientB  uuid.UUID
	secret   oauth.Value

	mu       sync.Mutex
	as       adminauth.Resolved
	signedIn bool
	clock    time.Time
	h        http.Handler
	admins   *fakeAdmins
}

// adminsPersonCalls is how many identity sign-ins the fake was asked for.
func (f *consentFixture) adminsPersonCalls() int {
	f.admins.mu.Lock()
	defer f.admins.mu.Unlock()
	return f.admins.personCalls
}

func newConsentFixture(t *testing.T, tune ...func(*config.Config, *fakeAdmins)) *consentFixture {
	t.Helper()
	dsn, ownerDSN := os.Getenv("DATABASE_URL"), os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" || ownerDSN == "" {
		t.Skip("DATABASE_URL / DATABASE_MIGRATE_URL not set; skipping the app consent's end-to-end tests")
	}
	ctx := context.Background()
	data, err := db.New(ctx, &config.Config{DatabaseURL: config.NewDSN(dsn)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(data.Close)
	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	f := &consentFixture{t: t, data: data, owner: owner, logs: &bytes.Buffer{}, tenantID: uuid.New(), ownerID: uuid.New(), manager: uuid.New(),
		clock: time.Now()}
	release := holdPanelTablesLock(t)
	err = data.WithTenant(ctx, f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure, timezone)
		                          VALUES ($1, 'Consent Fixture Ltd', $2, 'restaurant', 'multi', 'Europe/Malta')`,
			f.tenantID, "VAT-OAC-"+f.tenantID.String()); e != nil {
			return e
		}
		for _, a := range []struct {
			id   uuid.UUID
			role string
		}{{f.ownerID, "owner"}, {f.manager, "manager"}} {
			if _, e := tx.Exec(ctx, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
			                          VALUES ($1, $2, 'Consent Admin', $3, $4, $5, 'active')`,
				a.id, f.tenantID, "consent-"+a.id.String()+"@m11.example", fixtures.UnusablePasswordHash, a.role); e != nil {
				return e
			}
		}
		return nil
	})
	release()
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	trail, err := audit.New(data)
	if err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte("c"), 32)
	log := slog.New(slog.NewTextHandler(f.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	if f.srv, err = oauth.NewServer(key, data, trail, log); err != nil {
		t.Fatal(err)
	}
	f.client, f.secret = f.register("Malta Calculator", consentRedirect)
	f.clientB, _ = f.register("Another App", "https://another.example/cb")
	f.as = adminauth.Resolved{SessionID: uuid.New(), TenantID: f.tenantID, AdminUserID: f.ownerID, Role: "owner", FullName: "Consent Admin", TenantName: "Consent Fixture Ltd"}
	f.signedIn = true
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if !f.signedIn {
			return adminauth.Resolved{}, adminauth.ErrNoSession
		}
		return f.as, nil
	}, authenticate: func(string, string) (adminauth.Authentication, error) {
		return adminauth.Authentication{Resolved: 1, Attempts: []adminauth.Attempt{{AdminUserID: f.ownerID, TenantID: f.tenantID, PasswordMatched: true, Active: true}}}, nil
	}}
	accounts := newFakeAccount()
	records := newFakeLedger()
	cfg := adminTestConfig()
	cfg.OAuthHMACKey = config.NewKey(key)
	for _, f := range tune {
		f(cfg, admins)
	}
	f.admins = admins
	h, err := NewAdminAuth(admins, trail, records, records, &fakeReviewer{}, &fakeStaff{}, &fakeInviter{}, &fakeVenues{},
		&fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(), accounts, newFakeBrands(),
		newFakeBrandWriter(), nil, &fakeNotices{}, cfg, log, OAuthConsent(f.srv))
	if err != nil {
		t.Fatal(err)
	}
	h.oauthClock = func() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return f.clock }
	r := chi.NewRouter()
	h.Mount(r)
	registerPanel(r, h)
	f.h = r
	return f
}

// register writes an app as the owner (the operator's op_* is the shipped writer; the registry's
// rules are internal/db's to test).
func (f *consentFixture) register(name, redirect string) (uuid.UUID, oauth.Value) {
	f.t.Helper()
	ctx := context.Background()
	var operator uuid.UUID
	if err := f.owner.QueryRow(ctx, `
		WITH made AS (
		    INSERT INTO platform_admins (email, display_name, status)
		    SELECT $1, 'oauth consent fixture', 'disabled'
		     WHERE NOT EXISTS (SELECT 1 FROM platform_admins WHERE email = $1)
		    RETURNING id)
		SELECT id FROM made UNION ALL SELECT id FROM platform_admins WHERE email = $1 LIMIT 1`, "oauth-consent-fixture@example.test").Scan(&operator); err != nil {
		f.t.Fatal(err)
	}
	secret, err := oauth.Mint(oauth.KindSecret)
	if err != nil {
		f.t.Fatal(err)
	}
	d, _ := f.srv.Hasher().Hash(oauth.KindSecret, secret)
	var id uuid.UUID
	if err := f.owner.QueryRow(ctx, `INSERT INTO oauth_clients (name, redirect_uris, allowed_scopes, secret_hash, secret_hint, created_by)
	                                 VALUES ($1, $2, '{employees:read,employees:write,timesheets:read}', $3, $4, $5) RETURNING id`,
		name, []string{redirect}, d.RevealToTheDatabase(), secret.SecretHint(), operator).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	return id, secret
}

func (f *consentFixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.owner.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatalf("%s: %v", sql, err)
	}
}

func (f *consentFixture) count(sql string, args ...any) int64 {
	f.t.Helper()
	var n int64
	if err := f.owner.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		f.t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func (f *consentFixture) set(edit func(*consentFixture)) { f.mu.Lock(); edit(f); f.mu.Unlock() }

// browser is a panel browser holding the fake session cookie.
func (f *consentFixture) browser() *browser {
	b := newBrowser(f.t, f.h)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

// query is a valid authorization request for an app: the query and the verifier.
func consentQuery(client uuid.UUID, redirect, state string) (url.Values, string) {
	verifier := strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{"client_id": {oauth.ClientIDOf(client)}, "redirect_uri": {redirect}, "response_type": {"code"},
		"code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])},
		"scope": {"timesheets:read employees:write employees:read"}}
	if state != "" {
		q.Set("state", state)
	}
	return q, verifier
}

// rdOf lifts the rd hidden field out of a consent page.
func rdOf(t *testing.T, html string) string {
	t.Helper()
	i := strings.Index(html, `name="rd" value="`)
	if i < 0 {
		t.Fatalf("no rd on the page")
	}
	rest := html[i+len(`name="rd" value="`):]
	return rest[:strings.Index(rest, `"`)]
}

// TestOAuthConsentDB_ARequestIsCheckedBeforeTheSession (§4.1, §4.2): V1/V2 failures are Taptime's 400
// page -- no redirect, no cookie; a V3-V7 failure is a 303 to the REGISTERED URI with error, the state
// and iss, and no cookie; a signed-out browser with a valid request gets the cookie and the sign-in page
// (onUnauthenticated, not requireLogin's bare redirect alone).
func TestOAuthConsentDB_ARequestIsCheckedBeforeTheSession(t *testing.T) {
	f := newConsentFixture(t)
	f.set(func(f *consentFixture) { f.signedIn = false })
	b := f.browser()
	q, _ := consentQuery(f.client, consentRedirect, "st-1")
	for name, edit := range map[string]func(url.Values){
		"an unknown app":    func(q url.Values) { q.Set("client_id", oauth.ClientIDOf(uuid.New())) },
		"a trailing slash":  func(q url.Values) { q.Set("redirect_uri", consentRedirect+"/") },
		"another app's URI": func(q url.Values) { q.Set("redirect_uri", "https://another.example/cb") },
		"www.": func(q url.Values) {
			q.Set("redirect_uri", strings.Replace(consentRedirect, "https://", "https://www.", 1))
		},
		"no redirect_uri":      func(q url.Values) { q.Del("redirect_uri") },
		"a repeated client_id": func(q url.Values) { q.Add("client_id", oauth.ClientIDOf(f.client)) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := url.Values{}
			for k, v := range q {
				bad[k] = append([]string(nil), v...)
			}
			edit(bad)
			rec := b.do(http.MethodGet, oauthAuthorizeHref+"?"+bad.Encode(), nil)
			if rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" || len(rec.Result().Cookies()) != 0 {
				t.Fatalf("%d, Location %q, %d cookies", rec.Code, rec.Header().Get("Location"), len(rec.Result().Cookies()))
			}
		})
	}
	plain := url.Values{}
	for k, v := range q {
		plain[k] = v
	}
	plain.Set("code_challenge_method", "plain")
	rec := b.do(http.MethodGet, oauthAuthorizeHref+"?"+plain.Encode(), nil)
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusSeeOther || loc == nil || loc.Scheme+"://"+loc.Host+loc.Path != consentRedirect ||
		loc.Query().Get("error") != "invalid_request" || loc.Query().Get("state") != "st-1" || loc.Query().Get("iss") != testBaseURL ||
		len(rec.Result().Cookies()) != 0 {
		t.Fatalf("plain PKCE: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	badState := url.Values{}
	for k, v := range q {
		badState[k] = v
	}
	badState.Set("state", strings.Repeat("x", 1025))
	rec = b.do(http.MethodGet, oauthAuthorizeHref+"?"+badState.Encode(), nil)
	if loc, _ := url.Parse(rec.Header().Get("Location")); rec.Code != http.StatusSeeOther || loc.Query().Get("error") != "invalid_request" || loc.Query().Has("state") {
		t.Fatalf("a V7 failure reflected the state: %q", rec.Header().Get("Location"))
	}
	// Signed out, valid: the cookie and the sign-in page.
	b.fetchSite = "cross-site"
	rec = b.do(http.MethodGet, oauthAuthorizeHref+"?"+q.Encode(), nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != adminLoginPath {
		t.Fatalf("signed out: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	var pending *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == oauth.RequestCookieName {
			pending = c
		}
	}
	if pending == nil || pending.Path != "/admin" || pending.MaxAge != 600 || !pending.HttpOnly || pending.SameSite != http.SameSiteLaxMode {
		t.Fatalf("the pending request cookie: %+v", pending)
	}
	// Signing in lands on the ONE continue path, and the page is drawn from the cookie.
	b.fetchSite = ""
	csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, "/admin/login", nil)))
	rec = b.do(http.MethodPost, "/admin/login", url.Values{"csrf": {csrf}, "email": {"owner@example.test"}, "password": {"right"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != oauthContinueHref {
		t.Fatalf("the sign-in's landing: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	f.set(func(f *consentFixture) { f.signedIn = true })
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	page := b.do(http.MethodGet, oauthContinueHref, nil)
	if page.Code != 200 || !strings.Contains(htmlOf(t, page), "Malta Calculator wants to connect to Consent Fixture Ltd") {
		t.Fatalf("continue: %d", page.Code)
	}
	// /continue is mintGate's: another site cannot drive it.
	b.fetchSite = "cross-site"
	if rec := b.do(http.MethodGet, oauthContinueHref, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("a cross-site continue: %d", rec.Code)
	}
}

// TestOAuthConsentDB_TheOwnerApprovesAndTheCodeExchanges (§4.3-§4.6): the page -- arrived at from
// another site (the named mintGate exception: 200 and the cookie) -- names the app, the business and the
// return host, lists the scopes in words, carries its headers (frame-ancestors 'none', DENY, no-store,
// no-referrer, form-action 'self' plus the MATCHED origin only); "Allow" is a 303 to the registered
// URI with code, state and iss, the cookie cleared; the code exchanges at the token endpoint's server.
func TestOAuthConsentDB_TheOwnerApprovesAndTheCodeExchanges(t *testing.T) {
	f := newConsentFixture(t)
	b := f.browser()
	b.fetchSite = "cross-site"
	q, verifier := consentQuery(f.client, consentRedirect, "st & ü?")
	q.Set("state", "st & x?")
	rec := b.do(http.MethodGet, oauthAuthorizeHref+"?"+q.Encode(), nil)
	if rec.Code != 200 {
		t.Fatalf("the consent page: %d %s", rec.Code, rec.Body.String())
	}
	html := htmlOf(t, rec)
	for _, want := range []string{"Malta Calculator wants to connect to Consent Fixture Ltd", "consent.example", "Read the hours your employees worked",
		"Add, change and deactivate your employees", "See your employees", ">Allow<", "t allow</button>", "data-oauth-allow", "oauthconsent.js"} {
		if !strings.Contains(html, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	if strings.Contains(html, "st &amp; x?") || strings.Contains(html, q.Get("code_challenge")) || strings.Contains(html, consentRedirect) {
		t.Error("the page prints the request's state, challenge or URI")
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "form-action 'self' https://consent.example;") || strings.Contains(csp, "another.example") ||
		!strings.Contains(csp, "frame-ancestors 'none'") || !strings.Contains(csp, "script-src 'self'") || strings.Contains(csp, "unsafe") {
		t.Errorf("the consent CSP: %q", csp)
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" || rec.Header().Get("Referrer-Policy") != "no-referrer" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("the consent headers: %v", rec.Header())
	}
	b.fetchSite = ""
	rec = b.do(http.MethodPost, oauthAuthorizeHref, url.Values{"decision": {"allow"}, "rd": {rdOf(t, html)}})
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusSeeOther || loc.Scheme+"://"+loc.Host+loc.Path != consentRedirect || loc.Query().Get("state") != "st & x?" ||
		loc.Query().Get("iss") != testBaseURL || !strings.HasPrefix(loc.Query().Get("code"), "tt_ac_"+oauth.Env+"_") || loc.Query().Has("tb") {
		t.Fatalf("the approval: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if _, still := b.cookies[oauth.RequestCookieName]; still {
		t.Fatal("the pending request cookie survived the approval")
	}
	c, err := f.srv.AuthenticateClient(context.Background(), f.client, f.secret)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := f.srv.ExchangeCode(context.Background(), context.Background(), c, oauth.ParseValue(loc.Query().Get("code")), verifier, nil)
	if err != nil {
		t.Fatalf("the exchange of the consent's code: %v", err)
	}
	if oauth.ScopeString(tok.Scopes) != "employees:read employees:write timesheets:read" {
		t.Fatalf("the grant's scope: %q", oauth.ScopeString(tok.Scopes))
	}
	if strings.Contains(f.logs.String(), loc.Query().Get("code")) || strings.Contains(f.logs.String(), "st & x?") {
		t.Fatal("the code or the state reached the log")
	}
}

// TestOAuthConsentDB_ACookieOverwrittenInAnotherTabRefusesTheFirstPage (§4.2, rd): app A's page is open;
// a request for app B overwrites the cookie; A's "Allow" is refused (409) -- B is not approved, and no
// code is written. Pressing B's own page then works.
func TestOAuthConsentDB_ACookieOverwrittenInAnotherTabRefusesTheFirstPage(t *testing.T) {
	f := newConsentFixture(t)
	b := f.browser()
	qa, _ := consentQuery(f.client, consentRedirect, "a")
	rdA := rdOf(t, htmlOf(t, b.do(http.MethodGet, oauthAuthorizeHref+"?"+qa.Encode(), nil)))
	qb, _ := consentQuery(f.clientB, "https://another.example/cb", "b")
	rdB := rdOf(t, htmlOf(t, b.do(http.MethodGet, oauthAuthorizeHref+"?"+qb.Encode(), nil)))
	rec := b.do(http.MethodPost, oauthAuthorizeHref, url.Values{"decision": {"allow"}, "rd": {rdA}})
	if rec.Code != http.StatusConflict || rec.Header().Get("Location") != "" {
		t.Fatalf("A's allow after B's request: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if n := f.count(`SELECT count(*) FROM oauth_codes WHERE tenant_id = $1`, f.tenantID); n != 0 {
		t.Fatalf("%d codes written", n)
	}
	rec = b.do(http.MethodPost, oauthAuthorizeHref, url.Values{"decision": {"allow"}, "rd": {rdB}})
	if loc, _ := url.Parse(rec.Header().Get("Location")); rec.Code != http.StatusSeeOther || loc.Host != "another.example" {
		t.Fatalf("B's own allow: %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

// TestOAuthConsentDB_ADeniedOrRecheckedRequest (§4.2's ten items, §4.5): "Don't allow" is access_denied
// with the state and writes no code; an expired cookie, a tampered one and a missing one are refused
// with no redirect; the operator removing the matched URI or disabling the app in between makes the
// approval Taptime's error page (no redirect); narrowing the app's scopes in between is invalid_scope.
func TestOAuthConsentDB_ADeniedOrRecheckedRequest(t *testing.T) {
	f := newConsentFixture(t)
	b := f.browser()
	open := func(state string) string {
		q, _ := consentQuery(f.client, consentRedirect, state)
		return rdOf(t, htmlOf(t, b.do(http.MethodGet, oauthAuthorizeHref+"?"+q.Encode(), nil)))
	}
	rd := open("deny-me")
	rec := b.do(http.MethodPost, oauthDenyHref, url.Values{"rd": {rd}})
	if loc, _ := url.Parse(rec.Header().Get("Location")); rec.Code != http.StatusSeeOther || loc.Query().Get("error") != "access_denied" || loc.Query().Get("state") != "deny-me" || loc.Query().Get("iss") != testBaseURL {
		t.Fatalf("deny: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if _, still := b.cookies[oauth.RequestCookieName]; still {
		t.Fatal("the refusal kept the cookie")
	}
	// No cookie at all.
	if rec := b.do(http.MethodPost, oauthAuthorizeHref, url.Values{"decision": {"allow"}, "rd": {rd}}); rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
		t.Fatalf("no cookie: %d", rec.Code)
	}
	// Expired: eleven minutes later.
	rd = open("late")
	f.set(func(f *consentFixture) { f.clock = f.clock.Add(11 * time.Minute) })
	if rec := b.do(http.MethodPost, oauthAuthorizeHref, url.Values{"decision": {"allow"}, "rd": {rd}}); rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
		t.Fatalf("an expired cookie: %d", rec.Code)
	}
	if rec := b.do(http.MethodGet, oauthContinueHref, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("continue with an expired cookie: %d", rec.Code)
	}
	f.set(func(f *consentFixture) { f.clock = time.Now() })
	// Tampered.
	rd = open("tamper")
	b.cookies[oauth.RequestCookieName] = b.cookies[oauth.RequestCookieName][:20] + "x" + b.cookies[oauth.RequestCookieName][21:]
	if rec := b.do(http.MethodPost, oauthAuthorizeHref, url.Values{"decision": {"allow"}, "rd": {rd}}); rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
		t.Fatalf("a tampered cookie: %d", rec.Code)
	}
	// The scopes narrowed in between -> invalid_scope at the registered URI.
	rd = open("narrowed")
	f.exec(`UPDATE oauth_clients SET allowed_scopes = '{timesheets:read}' WHERE id = $1`, f.client)
	rec = b.do(http.MethodPost, oauthAuthorizeHref, url.Values{"decision": {"allow"}, "rd": {rd}})
	if loc, _ := url.Parse(rec.Header().Get("Location")); rec.Code != http.StatusSeeOther || loc.Query().Get("error") != "invalid_scope" {
		t.Fatalf("narrowed scopes: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	// The refusal of a request whose scopes were narrowed is still access_denied (the refusal is not
	// an error path, §4.5).
	f.exec(`UPDATE oauth_clients SET allowed_scopes = '{employees:read,employees:write,timesheets:read}' WHERE id = $1`, f.client)
	rd = open("narrowed-deny")
	f.exec(`UPDATE oauth_clients SET allowed_scopes = '{timesheets:read}' WHERE id = $1`, f.client)
	rec = b.do(http.MethodPost, oauthDenyHref, url.Values{"rd": {rd}})
	if loc, _ := url.Parse(rec.Header().Get("Location")); rec.Code != http.StatusSeeOther || loc.Query().Get("error") != "access_denied" {
		t.Fatalf("a refusal after the scopes narrowed: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	f.exec(`UPDATE oauth_clients SET allowed_scopes = '{employees:read,employees:write,timesheets:read}' WHERE id = $1`, f.client)
	// The URI removed in between -> no redirect.
	rd = open("moved")
	f.exec(`UPDATE oauth_clients SET redirect_uris = '{https://consent.example/new}' WHERE id = $1`, f.client)
	if rec := b.do(http.MethodPost, oauthAuthorizeHref, url.Values{"decision": {"allow"}, "rd": {rd}}); rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
		t.Fatalf("a URI removed in between: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	f.exec(`UPDATE oauth_clients SET redirect_uris = $2 WHERE id = $1`, f.client, []string{consentRedirect})
	// The app disabled in between -> no redirect.
	rd = open("disabled")
	f.exec(`UPDATE oauth_clients SET disabled_at = clock_timestamp() WHERE id = $1`, f.client)
	if rec := b.do(http.MethodPost, oauthAuthorizeHref, url.Values{"decision": {"allow"}, "rd": {rd}}); rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
		t.Fatalf("an app disabled in between: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if n := f.count(`SELECT count(*) FROM oauth_codes WHERE tenant_id = $1`, f.tenantID); n != 0 {
		t.Fatalf("%d codes written", n)
	}
}

// TestOAuthConsentDB_OnlyAnActiveOwnerApproves (§4.4, OA-6): a manager's page says connecting apps is the
// owner's and draws no "Allow"; a forged allow POST of the manager is access_denied and writes no code
// (the role is re-read in the approval's transaction); the way back is access_denied.
func TestOAuthConsentDB_OnlyAnActiveOwnerApproves(t *testing.T) {
	f := newConsentFixture(t)
	f.set(func(f *consentFixture) { f.as.AdminUserID, f.as.Role = f.manager, "manager" })
	b := f.browser()
	q, _ := consentQuery(f.client, consentRedirect, "m")
	html := htmlOf(t, b.do(http.MethodGet, oauthAuthorizeHref+"?"+q.Encode(), nil))
	if !strings.Contains(html, "Connecting apps is the owner") || strings.Contains(html, "data-oauth-allow") || !strings.Contains(html, "Back to Malta Calculator") {
		t.Fatal("the manager's page")
	}
	rec := b.do(http.MethodPost, oauthAuthorizeHref, url.Values{"decision": {"allow"}, "rd": {rdOf(t, html)}})
	if loc, _ := url.Parse(rec.Header().Get("Location")); rec.Code != http.StatusSeeOther || loc.Query().Get("error") != "access_denied" {
		t.Fatalf("a manager's forged allow: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if n := f.count(`SELECT count(*) FROM oauth_codes WHERE tenant_id = $1`, f.tenantID); n != 0 {
		t.Fatalf("a manager wrote %d codes", n)
	}
	// The session says owner, the row says manager (demoted after sign-in): still refused.
	f.set(func(f *consentFixture) { f.as.Role = "owner" })
	html = htmlOf(t, b.do(http.MethodGet, oauthAuthorizeHref+"?"+q.Encode(), nil))
	rec = b.do(http.MethodPost, oauthAuthorizeHref, url.Values{"decision": {"allow"}, "rd": {rdOf(t, html)}})
	if loc, _ := url.Parse(rec.Header().Get("Location")); rec.Code != http.StatusSeeOther || loc.Query().Get("error") != "access_denied" {
		t.Fatalf("a stale owner session: %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

// TestOAuthConsentDB_ASuspendedOrClosedBusiness (§11.2, §11.4, DÜŞÜK-1): suspended -- the page says so,
// the approval is 403 at the suspension ring, the refusal is access_denied; operator-closed -- the page
// says "Integrations are turned off for this business by Taptime", the refusal is access_denied, and a
// forged approval is access_denied with no code.
func TestOAuthConsentDB_ASuspendedOrClosedBusiness(t *testing.T) {
	f := newConsentFixture(t)
	f.set(func(f *consentFixture) {
		f.as.Suspended = &adminauth.Suspension{Since: time.Now(), Reason: "non_payment"}
	})
	f.exec(`UPDATE tenants SET suspended_at = now(), suspension_reason = 'non_payment' WHERE id = $1`, f.tenantID)
	b := f.browser()
	q, _ := consentQuery(f.client, consentRedirect, "s")
	rec := b.do(http.MethodGet, oauthAuthorizeHref+"?"+q.Encode(), nil)
	html := htmlOf(t, rec)
	if !strings.Contains(html, "This business is suspended") || strings.Contains(html, "data-oauth-allow") {
		t.Fatal("the suspended page")
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "form-action 'self' https://consent.example;") {
		t.Fatalf("the suspended page's CSP lacks the return origin: %q", csp)
	}
	rd := rdOf(t, html)
	if rec := b.do(http.MethodPost, oauthAuthorizeHref, url.Values{"decision": {"allow"}, "rd": {rd}}); rec.Code != http.StatusForbidden {
		t.Fatalf("an approval in a suspended business: %d", rec.Code)
	}
	rec = b.do(http.MethodPost, oauthDenyHref, url.Values{"rd": {rd}})
	if loc, _ := url.Parse(rec.Header().Get("Location")); rec.Code != http.StatusSeeOther || loc.Query().Get("error") != "access_denied" {
		t.Fatalf("the refusal in a suspended business: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	f.set(func(f *consentFixture) { f.as.Suspended = nil })
	f.exec(`UPDATE tenants SET suspended_at = NULL, suspension_reason = NULL, api_code = 'OAC', api_enabled_at = NULL WHERE id = $1`, f.tenantID)
	html = htmlOf(t, b.do(http.MethodGet, oauthAuthorizeHref+"?"+q.Encode(), nil))
	if !strings.Contains(html, "Integrations are turned off for this business by Taptime") || strings.Contains(html, "data-oauth-allow") {
		t.Fatal("the operator-closed page")
	}
	rec = b.do(http.MethodPost, oauthAuthorizeHref, url.Values{"decision": {"allow"}, "rd": {rdOf(t, html)}})
	if loc, _ := url.Parse(rec.Header().Get("Location")); rec.Code != http.StatusSeeOther || loc.Query().Get("error") != "access_denied" {
		t.Fatalf("a forged approval in a closed business: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if n := f.count(`SELECT count(*) FROM oauth_codes WHERE tenant_id = $1`, f.tenantID); n != 0 {
		t.Fatalf("%d codes written", n)
	}
}

// TestOAuthConsentDB_AFormOfAnotherBusinessIsRefused (§4.3, ADR 0031 §5.5): the consent page drawn for
// this business, posted after the session moved to another, is the form binding's 409 -- no code.
func TestOAuthConsentDB_AFormOfAnotherBusinessIsRefused(t *testing.T) {
	f := newConsentFixture(t)
	b := f.browser()
	b.unbound = true
	q, _ := consentQuery(f.client, consentRedirect, "kf")
	b.raw = true
	html := htmlOf(t, b.do(http.MethodGet, oauthAuthorizeHref+"?"+q.Encode(), nil))
	tb := b.tb
	if tb == "" {
		t.Fatal("the consent form carries no binding")
	}
	f.set(func(f *consentFixture) { f.as.TenantID = uuid.New() })
	rec := b.do(http.MethodPost, oauthAuthorizeHref+"?tb="+tb, url.Values{"decision": {"allow"}, "rd": {rdOf(t, html)}})
	if rec.Code != http.StatusConflict || rec.Header().Get("Location") != "" {
		t.Fatalf("a KF form posted from KM: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if n := f.count(`SELECT count(*) FROM oauth_codes WHERE tenant_id = $1`, f.tenantID); n != 0 {
		t.Fatalf("%d codes written", n)
	}
}

// TestOAuthConsentDB_TheOtherOwnedBusinessesAreSwitchForms (§4.3): with the panel identity on, the page
// lists the person's OTHER businesses where they are an owner and that are not suspended -- each the
// ordinary switch form (POST /admin/switch, its synchronizer token, the binding) -- and not the current
// one, a managed one or a suspended one. With the flag off there is no list.
func TestOAuthConsentDB_TheOtherOwnedBusinessesAreSwitchForms(t *testing.T) {
	km, kx, ks := uuid.New(), uuid.New(), uuid.New()
	var self *consentFixture
	f := newConsentFixture(t, func(cfg *config.Config, admins *fakeAdmins) {
		cfg.AdminIdentity = true
		admins.memberships = func() ([]adminauth.Membership, error) {
			return []adminauth.Membership{
				{TenantID: self.tenantID, TenantName: "Consent Fixture Ltd", Role: "owner", Current: true},
				{TenantID: km, TenantName: "Kebab Manufacturing", Role: "owner"},
				{TenantID: kx, TenantName: "Managed Elsewhere", Role: "manager"},
				{TenantID: ks, TenantName: "Suspended Elsewhere", Role: "owner", Suspended: true},
			}, nil
		}
	})
	self = f
	b := f.browser()
	b.raw = true
	q, _ := consentQuery(f.client, consentRedirect, "multi")
	html := htmlOf(t, b.do(http.MethodGet, oauthAuthorizeHref+"?"+q.Encode(), nil))
	if !strings.Contains(html, "Not this business?") || !strings.Contains(html, "Kebab Manufacturing") || !strings.Contains(html, `value="`+km.String()+`"`) {
		t.Fatal("the other owned business is not offered")
	}
	for _, not := range []string{"Managed Elsewhere", "Suspended Elsewhere", kx.String(), ks.String()} {
		if strings.Contains(html, not) {
			t.Errorf("the list offers %q", not)
		}
	}
	if !strings.Contains(html, `action="`+adminSwitchHref+`?tb=`) || !strings.Contains(html, `name="csrf"`) {
		t.Fatal("the switch form is not the bound switch with its synchronizer")
	}
	g := newConsentFixture(t)
	if html := htmlOf(t, g.browser().do(http.MethodGet, oauthAuthorizeHref+"?"+q.Encode(), nil)); strings.Contains(html, "Not this business?") {
		t.Fatal("the list is drawn with the panel identity off")
	}
}

// addBusiness writes a second business with an owner (committed, like the fixture's own).
func (f *consentFixture) addBusiness(name string) (tenant, owner uuid.UUID) {
	f.t.Helper()
	tenant, owner = uuid.New(), uuid.New()
	release := holdPanelTablesLock(f.t)
	defer release()
	if err := f.data.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure, timezone)
		                          VALUES ($1, $2, $3, 'restaurant', 'multi', 'Europe/Malta')`, tenant, name, "VAT-OACB-"+tenant.String()); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
		                       VALUES ($1, $2, 'Consent Admin', $3, $4, 'owner', 'active')`,
			owner, tenant, "consentb-"+owner.String()+"@m11.example", fixtures.UnusablePasswordHash)
		return e
	}); err != nil {
		f.t.Fatalf("the second business: %v", err)
	}
	return tenant, owner
}

// TestOAuthConsentDB_AllowIsDrawnDisabledWithANoscriptTwin (§4.6's double-clickjacking defence; review
// S2): the scripted "Allow" ships disabled (the script enables it after the delay), and a <noscript> twin
// posts the SAME action -- the same form binding -- with the same hidden fields, enabled. The way back
// ("Don't allow") is never disabled.
func TestOAuthConsentDB_AllowIsDrawnDisabledWithANoscriptTwin(t *testing.T) {
	f := newConsentFixture(t)
	b := f.browser()
	b.raw = true
	q, _ := consentQuery(f.client, consentRedirect, "ns")
	html := htmlOf(t, b.do(http.MethodGet, oauthAuthorizeHref+"?"+q.Encode(), nil))
	formRE := regexp.MustCompile(`(?s)<form method="post" action="(/admin/oauth/authorize\?tb=[A-Za-z0-9_-]{43})"[^>]*>(.*?)</form>`)
	forms := formRE.FindAllStringSubmatch(html, -1)
	if len(forms) != 2 {
		t.Fatalf("%d allow forms, want the scripted one and its noscript twin", len(forms))
	}
	if !strings.Contains(forms[0][2], `<button type="submit" data-oauth-allow disabled`) {
		t.Fatalf("the scripted Allow is not drawn disabled: %s", forms[0][2])
	}
	ns := strings.Index(html, "<noscript>")
	if ns < 0 || strings.Index(html, forms[1][0]) < ns || strings.Contains(forms[1][2], "disabled") {
		t.Fatal("the second allow form is not an enabled <noscript> twin")
	}
	hidden := regexp.MustCompile(`<input type="hidden" name="[a-z]+" value="[^"]*">`)
	if forms[0][1] != forms[1][1] || strings.Join(hidden.FindAllString(forms[0][2], -1), "|") != strings.Join(hidden.FindAllString(forms[1][2], -1), "|") ||
		len(hidden.FindAllString(forms[1][2], -1)) != 2 {
		t.Fatal("the noscript twin does not carry the same binding and hidden fields")
	}
	if strings.Contains(html, `class="btn btn--quiet min-h-12 w-full rounded-xl" disabled`) {
		t.Fatal("the refusal is drawn disabled")
	}
}

// TestOAuthConsentDB_ASwitchLandsOnTheConsentOfTheNewBusiness (§4.2, §4.3; review D1 a): with a pending
// request in the browser, switching to another business of the person's is a 303 to the ONE continue
// path, and the page drawn there names the business switched into.
func TestOAuthConsentDB_ASwitchLandsOnTheConsentOfTheNewBusiness(t *testing.T) {
	var self *consentFixture
	var km uuid.UUID
	f := newConsentFixture(t, func(cfg *config.Config, admins *fakeAdmins) {
		cfg.AdminIdentity = true
		admins.memberships = func() ([]adminauth.Membership, error) {
			return []adminauth.Membership{
				{TenantID: self.tenantID, TenantName: "Consent Fixture Ltd", Role: "owner", Current: true},
				{TenantID: km, TenantName: "Kebab Manufacturing", Role: "owner"},
			}, nil
		}
		admins.switchTo = func(target uuid.UUID) (adminauth.Issued, error) {
			return adminauth.Issued{Session: adminauth.Session{ID: uuid.New(), TenantID: target, AdminUserID: self.ownerID}, Token: fakeIssuedToken()}, nil
		}
	})
	self = f
	var kmOwner uuid.UUID
	km, kmOwner = f.addBusiness("Kebab Manufacturing")
	b := f.browser()
	b.raw = true
	q, _ := consentQuery(f.client, consentRedirect, "sw")
	html := htmlOf(t, b.do(http.MethodGet, oauthAuthorizeHref+"?"+q.Encode(), nil))
	csrf := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(html)
	if csrf == nil {
		t.Fatal("PREMISE: no switch form on the page")
	}
	rec := b.do(http.MethodPost, adminSwitchHref, url.Values{"csrf": {csrf[1]}, "tenant_id": {km.String()}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != oauthContinueHref {
		t.Fatalf("the switch with a pending request: %d %q, want 303 %s", rec.Code, rec.Header().Get("Location"), oauthContinueHref)
	}
	f.set(func(f *consentFixture) {
		f.as.TenantID, f.as.AdminUserID, f.as.TenantName, f.as.SessionID = km, kmOwner, "Kebab Manufacturing", uuid.New()
	})
	page := htmlOf(t, b.do(http.MethodGet, oauthContinueHref, nil))
	if !strings.Contains(page, "Malta Calculator wants to connect to Kebab Manufacturing") {
		t.Fatal("the continue page does not name the business switched into")
	}
	// Without a pending request the switch lands on the panel as before.
	delete(b.cookies, oauth.RequestCookieName)
	f.set(func(f *consentFixture) {
		f.as.TenantID, f.as.AdminUserID, f.as.TenantName = f.tenantID, f.ownerID, "Consent Fixture Ltd"
	})
	html = htmlOf(t, b.do(http.MethodGet, oauthAuthorizeHref+"?"+q.Encode(), nil))
	delete(b.cookies, oauth.RequestCookieName)
	csrf = regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(html)
	if rec := b.do(http.MethodPost, adminSwitchHref, url.Values{"csrf": {csrf[1]}, "tenant_id": {km.String()}}); rec.Header().Get("Location") != transactionsHref {
		t.Fatalf("a switch without a pending request: %q, want %s", rec.Header().Get("Location"), transactionsHref)
	}
}

// TestOAuthConsentDB_AnIdentitySignInLandsOnContinue (§4.2; review D1 b): with TAPPA_ADMIN_IDENTITY on,
// the person's sign-in (completePersonLogin) with a pending request in the browser is a 303 to the ONE
// continue path; without one, to /admin.
func TestOAuthConsentDB_AnIdentitySignInLandsOnContinue(t *testing.T) {
	var self *consentFixture
	f := newConsentFixture(t, func(cfg *config.Config, admins *fakeAdmins) {
		cfg.AdminIdentity = true
		admins.person = func(string, string, uuid.UUID, adminauth.FailureBudget) (adminauth.PersonSignIn, error) {
			return adminauth.PersonSignIn{Session: &adminauth.Issued{
				Session: adminauth.Session{ID: uuid.New(), TenantID: self.tenantID, AdminUserID: self.ownerID}, Token: fakeIssuedToken()}}, nil
		}
	})
	self = f
	f.set(func(f *consentFixture) { f.signedIn = false })
	for _, pending := range []bool{true, false} {
		b := f.browser()
		if pending {
			q, _ := consentQuery(f.client, consentRedirect, "id")
			if rec := b.do(http.MethodGet, oauthAuthorizeHref+"?"+q.Encode(), nil); rec.Header().Get("Location") != adminLoginPath {
				t.Fatalf("PREMISE: signed out, the request is %d %q", rec.Code, rec.Header().Get("Location"))
			}
		}
		csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, "/admin/login", nil)))
		rec := b.do(http.MethodPost, "/admin/login", url.Values{"csrf": {csrf}, "email": {"owner@example.test"}, "password": {"right"}})
		want := "/admin"
		if pending {
			want = oauthContinueHref
		}
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want {
			t.Fatalf("pending %v: the identity sign-in lands on %d %q, want %s", pending, rec.Code, rec.Header().Get("Location"), want)
		}
	}
	if f.adminsPersonCalls() == 0 {
		t.Fatal("PREMISE: the identity path was not taken")
	}
}
