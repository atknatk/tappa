package handler

// apikeys_db_test.go -- the API keys section (M11 API-3) driven over HTTP against REAL Postgres,
// with the real internal/apikey service and the real trail behind it. The admin is a real
// admin_users row (the domain re-reads the role), the session is a fake resolution of it.

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/apikey"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/test/fixtures"
)

// liveKeyRE finds anything of a live key's shape in a response or a log.
var liveKeyRE = regexp.MustCompile(`tt_(live|test)_[A-Za-z0-9_-]{43}`)

type apiKeysDBFixture struct {
	data           *db.DB
	keys           *apikey.Keys
	auth           *apikey.Authenticator
	tenantID       uuid.UUID
	owner, manager uuid.UUID
	logs           *bytes.Buffer
	macKey         []byte
}

func newAPIKeysDBFixture(t *testing.T) *apiKeysDBFixture {
	t.Helper()
	dsn, owner := os.Getenv("DATABASE_URL"), os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" || owner == "" {
		t.Skip("DATABASE_URL / DATABASE_MIGRATE_URL not set; skipping the API keys section's end-to-end tests")
	}
	data, err := db.New(context.Background(), &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)
	f := &apiKeysDBFixture{data: data, tenantID: uuid.New(), owner: uuid.New(), manager: uuid.New(), logs: &bytes.Buffer{}}
	if err := data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure, timezone)
		                          VALUES ($1, 'API Keys Fixture Ltd', $2, 'restaurant', 'multi', 'Europe/Malta')`,
			f.tenantID, "VAT-"+f.tenantID.String()); e != nil {
			return e
		}
		for _, a := range []struct {
			id   uuid.UUID
			role string
		}{{f.owner, "owner"}, {f.manager, "manager"}} {
			if _, e := tx.Exec(ctx, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
			                          VALUES ($1, $2, 'Keys Admin', $3, $4, $5, 'active')`,
				a.id, f.tenantID, "keys-"+a.id.String()+"@m11.example", fixtures.UnusablePasswordHash, a.role); e != nil {
				return e
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	f.setAPI(t, true)
	trail, err := audit.New(data)
	if err != nil {
		t.Fatal(err)
	}
	f.macKey = []byte(strings.Repeat("k", 32))
	if f.keys, err = apikey.NewKeys(apikey.LivePrefix, f.macKey, data, trail); err != nil {
		t.Fatal(err)
	}
	if f.auth, err = apikey.NewAuthenticator(apikey.LivePrefix, f.macKey, data, data); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *apiKeysDBFixture) setAPI(t *testing.T, open bool) {
	t.Helper()
	c, err := pgx.Connect(context.Background(), os.Getenv("DATABASE_MIGRATE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	sql := `UPDATE tenants SET api_code = 'KEYS', api_enabled_at = now() WHERE id = $1`
	if !open {
		sql = `UPDATE tenants SET api_enabled_at = NULL WHERE id = $1`
	}
	if _, err := c.Exec(context.Background(), sql, f.tenantID); err != nil {
		t.Fatal(err)
	}
}

// browserAs is a signed-in panel browser for one of the two admins.
func (f *apiKeysDBFixture) browserAs(t *testing.T, adminID uuid.UUID, role string) *browser {
	t.Helper()
	trail, err := audit.New(f.data)
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := tenant.NewAccounts(f.data, trail, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{SessionID: uuid.New(), TenantID: f.tenantID, AdminUserID: adminID, Role: role, FullName: "Keys Admin"}, nil
	}}
	records := newFakeLedger()
	log := slog.New(slog.NewTextHandler(f.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h, err := NewAdminAuth(admins, trail, records, records, &fakeReviewer{}, &fakeStaff{}, &fakeInviter{}, &fakeVenues{},
		&fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(), accounts, newFakeBrands(),
		newFakeBrandWriter(), nil, &fakeNotices{}, adminTestConfig(), log, IntegrationKeys(f.keys))
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

func (f *apiKeysDBFixture) keysOnFile(t *testing.T) []apikey.Key {
	t.Helper()
	l, err := f.keys.List(context.Background(), f.tenantID, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	return l.Keys
}

func (f *apiKeysDBFixture) trail(t *testing.T, action string) []string {
	t.Helper()
	var out []string
	if err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT detail::text FROM audit_log WHERE tenant_id = $1 AND action = $2`, f.tenantID, action)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d string
			if err := rows.Scan(&d); err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestApiKeyPanel_ManagerIs403EverywhereAndLeavesATrail: a manager of the business gets 403 at the
// section and at its three writes; each refused WRITE leaves one api_key.refused row naming the act
// and the role (nothing posted); nothing is created or revoked. The owner's own key survives the
// manager's revoke and revoke-all. The tab is not drawn for the manager.
func TestApiKeyPanel_ManagerIs403EverywhereAndLeavesATrail(t *testing.T) {
	f := newAPIKeysDBFixture(t)
	owner := f.browserAs(t, f.owner, "owner")
	rec := owner.do(http.MethodPost, apiKeysHref, url.Values{"name": {"Owner's"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("the owner's create: %d", rec.Code)
	}
	ownerKey := f.keysOnFile(t)[0]

	m := f.browserAs(t, f.manager, "manager")
	if rec := m.do(http.MethodGet, apiKeysHref, nil); rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), "Owner&#39;s") {
		t.Fatalf("GET as a manager: %d", rec.Code)
	}
	if rec := m.do(http.MethodGet, "/admin", nil); strings.Contains(rec.Body.String(), `href="`+apiKeysHref+`"`) {
		t.Fatal("the API keys tab is drawn for a manager")
	}
	for _, c := range []struct {
		path string
		form url.Values
	}{
		{apiKeysHref, url.Values{"name": {"Manager's"}}},
		{apiKeyRevokeHref, url.Values{"key_id": {ownerKey.ID.String()}}},
		{apiKeyRevokeAllHref, url.Values{"confirm": {"yes"}}},
	} {
		if rec := m.do(http.MethodPost, c.path, c.form); rec.Code != http.StatusForbidden {
			t.Errorf("POST %s as a manager: %d, want 403", c.path, rec.Code)
		}
	}
	keys := f.keysOnFile(t)
	if len(keys) != 1 || keys[0].RevokedAt != nil {
		t.Fatalf("after the manager's attempts: %d keys, the owner's revoked %v", len(keys), keys[0].RevokedAt)
	}
	refused := f.trail(t, ActionAPIKeyRefused)
	if len(refused) != 3 {
		t.Fatalf("%d api_key.refused rows, want 3", len(refused))
	}
	for _, d := range refused {
		if !strings.Contains(d, `"role": "manager"`) || strings.Contains(d, "Manager's") {
			t.Errorf("a refusal row %s: want the role and nothing posted", d)
		}
	}
}

// TestApiKeyPanel_TheKeyIsShownOnceAndNeverAgain is K-10's "ham anahtar yalnız oluşturma yanıtında,
// bir kez": the create answer is 200, no-store, and carries exactly one value of the key's shape --
// a real key, which the chain resolves to this business; the section read afterwards carries none
// (the hint, not the key); re-posting the same form (a reload, the back button) is refused with 422
// by the live-name rule and issues no second key and shows no key; the trail has one created row
// whose detail holds the name and the hint, never the key.
func TestApiKeyPanel_TheKeyIsShownOnceAndNeverAgain(t *testing.T) {
	f := newAPIKeysDBFixture(t)
	b := f.browserAs(t, f.owner, "owner")
	form := url.Values{"name": {"Payroll"}}
	rec := b.do(http.MethodPost, apiKeysHref, form)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("the response carrying the key is Cache-Control %q, want no-store", cc)
	}
	found := liveKeyRE.FindAllString(rec.Body.String(), -1)
	if len(found) != 1 {
		t.Fatalf("the create answer carries %d values of the key's shape, want 1", len(found))
	}
	raw := found[0]
	tok, err := apikey.ParseBearer("Bearer " + raw)
	if err != nil {
		t.Fatal(err)
	}
	checked, err := f.auth.Check(tok)
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.auth.Resolve(context.Background(), checked)
	if err != nil || p.TenantID != f.tenantID {
		t.Fatalf("the shown key does not resolve to this business: %v %v", p.TenantID, err)
	}

	page := b.do(http.MethodGet, apiKeysHref, nil)
	if page.Code != http.StatusOK || liveKeyRE.MatchString(page.Body.String()) || strings.Contains(page.Body.String(), raw[len(apikey.LivePrefix):]) {
		t.Fatalf("the section read afterwards: %d, carries the key", page.Code)
	}
	if !strings.Contains(page.Body.String(), raw[:len(apikey.LivePrefix)+4]) {
		t.Fatal("CONTROL: the section does not show the key's hint")
	}

	again := b.do(http.MethodPost, apiKeysHref, form)
	if again.Code != http.StatusUnprocessableEntity || liveKeyRE.MatchString(again.Body.String()) {
		t.Fatalf("the re-posted form: %d (a key in the answer: %v), want 422 and none", again.Code, liveKeyRE.MatchString(again.Body.String()))
	}
	if n := len(f.keysOnFile(t)); n != 1 {
		t.Fatalf("%d keys after the re-post, want 1", n)
	}
	created := f.trail(t, apikey.ActionCreated)
	if len(created) != 1 || !strings.Contains(created[0], "Payroll") || strings.Contains(created[0], raw[len(apikey.LivePrefix):]) {
		t.Fatalf("created rows %v: want one, with the name and without the key", created)
	}
	if strings.Contains(f.logs.String(), raw[len(apikey.LivePrefix):]) || liveKeyRE.MatchString(f.logs.String()) {
		t.Fatal("the process log carries the key")
	}
	if !strings.Contains(f.logs.String(), "panel api key created") {
		t.Fatal("CONTROL: the creation was not logged at all")
	}
}

// TestApiKeyPanel_AnotherBusinessCannotRevokeByGuessingTheID: the owner of A posts a revocation
// naming B's key id -> 303 "unknown-key", B's key still resolves, no revoked row anywhere.
func TestApiKeyPanel_AnotherBusinessCannotRevokeByGuessingTheID(t *testing.T) {
	a, bb := newAPIKeysDBFixture(t), newAPIKeysDBFixture(t)
	if rec := bb.browserAs(t, bb.owner, "owner").do(http.MethodPost, apiKeysHref, url.Values{"name": {"B's ERP"}}); rec.Code != http.StatusOK {
		t.Fatalf("B's create: %d", rec.Code)
	}
	bKey := bb.keysOnFile(t)[0]
	rec := a.browserAs(t, a.owner, "owner").do(http.MethodPost, apiKeyRevokeHref, url.Values{"key_id": {bKey.ID.String()}})
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "problem=unknown-key") {
		t.Fatalf("A revoking B's key: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if k := bb.keysOnFile(t)[0]; k.RevokedAt != nil {
		t.Fatal("B's key was revoked by A's owner")
	}
	if len(a.trail(t, apikey.ActionRevoked)) != 0 || len(bb.trail(t, apikey.ActionRevoked)) != 0 {
		t.Fatal("a revoked row was written")
	}
}

// TestApiKeyPanel_CrossSiteWritesAreRefusedBeforeTheResolver is GATE-1's chain on the three new
// writes: a POST whose Origin is another site creates and revokes nothing, and is answered before
// the role gate (no refusal row either); the same POSTs from the panel's own origin land.
func TestApiKeyPanel_CrossSiteWritesAreRefusedBeforeTheResolver(t *testing.T) {
	f := newAPIKeysDBFixture(t)
	owner := f.browserAs(t, f.owner, "owner")
	if rec := owner.do(http.MethodPost, apiKeysHref, url.Values{"name": {"Kept"}}); rec.Code != http.StatusOK {
		t.Fatalf("CONTROL create: %d", rec.Code)
	}
	kept := f.keysOnFile(t)[0]
	evil := f.browserAs(t, f.owner, "owner")
	evil.origin = "https://evil.example"
	for _, c := range []struct {
		path string
		form url.Values
	}{
		{apiKeysHref, url.Values{"name": {"Evil"}}},
		{apiKeyRevokeHref, url.Values{"key_id": {kept.ID.String()}}},
		{apiKeyRevokeAllHref, url.Values{"confirm": {"yes"}}},
	} {
		rec := evil.do(http.MethodPost, c.path, c.form)
		if rec.Code == http.StatusOK || liveKeyRE.MatchString(rec.Body.String()) {
			t.Errorf("cross-site POST %s: %d", c.path, rec.Code)
		}
	}
	keys := f.keysOnFile(t)
	if len(keys) != 1 || keys[0].RevokedAt != nil {
		t.Fatalf("after cross-site POSTs: %d keys, revoked %v", len(keys), keys[0].RevokedAt)
	}
	if n := len(f.trail(t, ActionAPIKeyRefused)); n != 0 {
		t.Fatalf("%d refusal rows: the cross-site POST reached the role gate", n)
	}
	if rec := owner.do(http.MethodPost, apiKeyRevokeAllHref, url.Values{"confirm": {"yes"}}); rec.Code != http.StatusSeeOther ||
		!strings.Contains(rec.Header().Get("Location"), "done=revoked-all") {
		t.Fatalf("CONTROL revoke-all from the panel's origin: %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

// TestApiKeyPanel_AClosedAPIOffersNoCreateAndStillRevokes: with the operator's gate closed the
// section says so, draws no create form, refuses a posted create (303 api-closed, no key), and
// still revokes an existing key (K-26).
func TestApiKeyPanel_AClosedAPIOffersNoCreateAndStillRevokes(t *testing.T) {
	f := newAPIKeysDBFixture(t)
	b := f.browserAs(t, f.owner, "owner")
	if rec := b.do(http.MethodPost, apiKeysHref, url.Values{"name": {"Before"}}); rec.Code != http.StatusOK {
		t.Fatalf("create: %d", rec.Code)
	}
	f.setAPI(t, false)
	page := b.do(http.MethodGet, apiKeysHref, nil).Body.String()
	if !strings.Contains(page, "API access is off for this business") || strings.Contains(page, `name="name"`) {
		t.Fatal("a closed API: the note is missing or the create form is drawn")
	}
	rec := b.do(http.MethodPost, apiKeysHref, url.Values{"name": {"After"}})
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "problem=api-closed") {
		t.Fatalf("create on a closed API: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	key := f.keysOnFile(t)[0]
	if rec := b.do(http.MethodPost, apiKeyRevokeHref, url.Values{"key_id": {key.ID.String()}}); !strings.Contains(rec.Header().Get("Location"), "done=revoked") {
		t.Fatalf("revoke on a closed API: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if keys := f.keysOnFile(t); len(keys) != 1 || keys[0].RevokedAt == nil {
		t.Fatal("the key was not revoked, or a second appeared")
	}
}

// TestApiKeyPanel_TheDeactivationBudgetAlertIsShownToTheOwner (M11 API-6; ADR 0028 §12, K-25: "owner'a
// panelde görünür uyarı"): a key that ran out of its deactivation budget -- the
// api.deactivation_budget_exceeded row internal/domain/tenant writes -- is named on the owner's API
// keys section with the time, in the business's zone; an alert older than a day is not; another
// business's alert never is; and without an alert the notice is absent (CONTROL).
func TestApiKeyPanel_TheDeactivationBudgetAlertIsShownToTheOwner(t *testing.T) {
	f := newAPIKeysDBFixture(t)
	other := newAPIKeysDBFixture(t)
	b := f.browserAs(t, f.owner, "owner")
	const heading = "A key reached its limit for deactivating employees"
	if page := b.do(http.MethodGet, apiKeysHref, nil); page.Code != 200 || strings.Contains(page.Body.String(), heading) {
		t.Fatalf("CONTROL: the section without an alert: %d, notice drawn %v", page.Code, strings.Contains(page.Body.String(), heading))
	}
	mk := func(fx *apiKeysDBFixture, name string) uuid.UUID {
		t.Helper()
		c, err := fx.keys.Create(context.Background(), apikey.CreateCommand{TenantID: fx.tenantID, ActorID: fx.owner, Name: name})
		if err != nil {
			t.Fatal(err)
		}
		return c.ID
	}
	recent, old, foreign := mk(f, "Payroll-recent"), mk(f, "Payroll-old"), mk(other, "Foreign-key")
	trail, err := audit.New(f.data)
	if err != nil {
		t.Fatal(err)
	}
	staff, err := tenant.NewStaff(f.data, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if err := staff.RecordDeactivationBudgetExceeded(context.Background(), f.tenantID, recent, "api", 20); err != nil {
		t.Fatal(err)
	}
	if err := staff.RecordDeactivationBudgetExceeded(context.Background(), other.tenantID, foreign, "api", 20); err != nil {
		t.Fatal(err)
	}
	// `at` is the database's alone (00053); the owner dates the old alert.
	ownerConn, err := pgx.Connect(context.Background(), os.Getenv("DATABASE_MIGRATE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ownerConn.Close(context.Background()) }()
	if _, err := ownerConn.Exec(context.Background(), `INSERT INTO audit_log (tenant_id, actor_id, action, target, detail, at)
	                      VALUES ($1, $2, 'api.deactivation_budget_exceeded', $3, '{"via":"api","budget":20,"window_seconds":3600}', now() - interval '25 hours')`,
		f.tenantID, old, old.String()); err != nil {
		t.Fatal(err)
	}
	body := b.do(http.MethodGet, apiKeysHref, nil).Body.String()
	if !strings.Contains(body, heading) || !strings.Contains(body, "Payroll-recent") {
		t.Fatal("the owner's section does not name the key that reached its budget")
	}
	alert := body[strings.Index(body, heading):]
	alert = alert[:strings.Index(alert, "</ul>")]
	if strings.Contains(alert, "Payroll-old") || strings.Contains(alert, "Foreign-key") {
		t.Fatal("the alert names a key past the day's window or another business's key")
	}
}

// TestPanelHome_TheKeyBudgetBannerIsTheOwnersAlone (the user's decision, 2026-10-15; ADR 0028 §12): the
// panel's home (/admin) draws the owner a banner when an integration key reached its deactivation
// budget in the last day, linking to the API keys section; before the alert it draws none, and the
// page's bytes are the same as a render whose banner field is set but empty (the banner adds nothing
// when it is absent); a manager of the same business never sees it -- the handler asks for the
// owner alone and the domain refuses another role. The API keys section shows the budget each
// alert recorded (detail.budget), not a number written into the page.
//
// MUTANT (measured): the owner check removed from the handler -> the manager's home asks the domain,
// which refuses, so the banner stays absent; the domain's own owner check removed as well -> the
// manager sees it, red.
func TestPanelHome_TheKeyBudgetBannerIsTheOwnersAlone(t *testing.T) {
	f := newAPIKeysDBFixture(t)
	owner, manager := f.browserAs(t, f.owner, "owner"), f.browserAs(t, f.manager, "manager")
	const heading = "An integration key reached its limit for deactivating employees"
	home := func(b *browser) string {
		t.Helper()
		rec := b.do(http.MethodGet, "/admin", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("the home page: %d", rec.Code)
		}
		return rec.Body.String()
	}
	if strings.Contains(home(owner), heading) {
		t.Fatal("CONTROL: the banner is drawn with no alert")
	}
	c, err := f.keys.Create(context.Background(), apikey.CreateCommand{TenantID: f.tenantID, ActorID: f.owner, Name: "HR sync"})
	if err != nil {
		t.Fatal(err)
	}
	trail, err := audit.New(f.data)
	if err != nil {
		t.Fatal(err)
	}
	staff, err := tenant.NewStaff(f.data, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if err := staff.RecordDeactivationBudgetExceeded(context.Background(), f.tenantID, c.ID, "api", 7); err != nil {
		t.Fatal(err)
	}
	body := home(owner)
	if !strings.Contains(body, heading) || !strings.Contains(body, `href="`+apiKeysHref+`"`) {
		t.Fatal("the owner's home does not draw the banner with its link to the API keys section")
	}
	if strings.Contains(home(manager), heading) {
		t.Fatal("a manager's home draws the owner's banner")
	}
	keysPage := owner.do(http.MethodGet, apiKeysHref, nil).Body.String()
	if !strings.Contains(keysPage, `<span class="font-mono">7</span> an hour`) || strings.Contains(keysPage, "at most 20") {
		t.Fatal("the API keys section does not show the budget the alert recorded")
	}
}
