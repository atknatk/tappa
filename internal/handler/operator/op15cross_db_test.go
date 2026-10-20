package operator_test

// op15cross_db_test.go -- M10 OP-15's closing measurement (K15-13; ADR 0025 "C fazı eki", LC7):
// the operator's screen suspends a business and the business's two write surfaces close -- the
// panel (phase B's gate) and the integration API's /v1 (M11 API-4b's checkpoint) -- and the
// operator's screen lifts it and both open again. ONE router composes the three features as
// cmd/tappa's appRouter does (httpx.NewRouter: the operator surface on its host, the panel on the
// main host, /v1 on the API host); each feature is built over the database the product uses: the
// operator surface over newLegalE2E's tappa_operator connection, the panel and /v1 over tappa_app's
// pool.
//
// WHY HERE AND NOT IN cmd/tappa. cmd/tappa composes the served router, but its operator side needs a
// tappa_operator login, which a development database does not have (the role is born NOLOGIN); the
// one committed rig that drives the operator's real sign-in and real op_* writes through the surface
// is this package's newLegalE2E. The panel and /v1 are built here from their exported constructors,
// the dependencies cmd/tappa's buildAppFeatures mirrors, over the real pool -- no fake on any of the
// three sides. Which router cmd/tappa serves is that package's own test
// (TestAppRoutes_EveryNonReadRouteHasOneOfFourClasses walks it; the two suspension POSTs are classed
// there as the operator surface's).
//
// WHAT ONE RUN LEAVES BEHIND (committed, never deleted: tappa_app cannot, and every row is
// append-only or a fixture): one business ('FAKE op15c cross …', a made-up VAT number), its owner
// (an @example.test address), one API key, four locations (the panel's and /v1's, each before the
// suspension and after its lifting), the trail and ledger rows of those writes (and of the owner's
// sign-in and the key's creation: nine rows of six actions in the business's audit_log), the panel's one
// 'tenant.write_refused' row (its refused write; ADR 0025 §3) and the suspension's two rows in the
// business's audit_log; on the operator's side, newLegalE2E's account and sessions and the run's
// operator_audit_log rows. 🔴 THE BUSINESS ENDS NOT SUSPENDED: the run lifts it, and a
// t.Cleanup lifts it again through the surface if a failure left it suspended (00063's Down refuses
// while any tenant is suspended, K15-6). No other business is suspended.

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/apikey"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/billing"
	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/manual"
	"github.com/atknatk/tappa/internal/domain/review"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/handler"
	"github.com/atknatk/tappa/internal/handler/api"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/invite"
)

// crossAPIHost is the integration API's host on the cross-surface router.
const crossAPIHost = "api.taptime.mt"

// crossSurfaces is the business and the three features on one router.
type crossSurfaces struct {
	h        http.Handler
	data     *db.DB
	biz      uuid.UUID
	name     string
	email    string
	password string
	apiKey   string
}

// newCrossSurfaces commits the business (the file header says what it leaves), builds the panel and
// /v1 over tappa_app's pool as cmd/tappa builds them, and composes them with l's operator surface.
func newCrossSurfaces(t *testing.T, l *legalE2E) *crossSurfaces {
	t.Helper()
	ctx := l.ctx
	cfg := &config.Config{
		Env: config.EnvDev, BaseURL: opBase, OperatorHost: opHost, APIHost: crossAPIHost,
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		SessionHMACKey: randBytes(t, 32), InviteHMACKey: randBytes(t, 32), TagKEK: randBytes(t, 32),
		APIKeyHMACKey:   randBytes(t, 32),
		GPSRadiusMeters: 150, Debounce: 60 * time.Second, Freshness: 180 * time.Second, RetentionYears: 2,
	}
	data, err := db.New(ctx, cfg)
	if err != nil {
		t.Fatalf("tappa_app's pool: %v", err)
	}
	t.Cleanup(data.Close)
	log := slog.New(slog.DiscardHandler)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	c := &crossSurfaces{data: data, biz: uuid.New(), password: "FAKE op15c cross passphrase " + uuid.NewString()[:8]}
	c.name = "FAKE op15c cross " + c.biz.String()[:8]
	c.email = "op15c-cross-" + c.biz.String()[:12] + "@example.test"
	digest, err := adminauth.Hash(c.password)
	must(err)
	admin := uuid.New()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO tenants (id, name, vat_number, business_type, structure, timezone)
		  VALUES ($1, $2, $3, 'restaurant', 'multi', 'Europe/Malta')`, []any{c.biz, c.name, "FAKE-OP15C-X-" + c.biz.String()}},
		{`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
		  VALUES ($1, $2, 'FAKE op15c owner', $3, $4, 'owner', 'active')`, []any{admin, c.biz, c.email, digest}},
		// K-22: the operator opened the API for the business (the operator's own screen for it is
		// tenantapi.go's; here the owner writes the two columns, as cmd/tappa's /v1 tests do).
		{`UPDATE tenants SET api_code = 'XOPC', api_enabled_at = now() WHERE id = $1`, []any{c.biz}},
	} {
		if _, err := l.owner.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("the business: %v", err)
		}
	}

	// The panel, as cmd/tappa builds it (buildAppFeatures mirrors run()).
	trail, err := audit.New(data)
	must(err)
	checkins, err := checkin.New(data, trail, cfg, log)
	must(err)
	admins, err := adminauth.New(data, cfg)
	must(err)
	records, err := ledger.NewReader(data, log)
	must(err)
	reviewer, err := review.NewReviewer(data, trail, log)
	must(err)
	staff, err := tenant.NewStaff(data, trail, log)
	must(err)
	invites, err := invite.New(data, cfg)
	must(err)
	venues, err := tenant.NewVenues(data, trail, log)
	must(err)
	plaques, err := tenant.NewPlaques(data, trail, log)
	must(err)
	entries, err := manual.NewRecorder(data, trail, log)
	must(err)
	rules, err := tenant.NewRulebook(data, checkins.Params(), checkins.GPSRadiusM(), log)
	must(err)
	ruleWriter, err := tenant.NewRuleWriter(data, trail, checkins, checkins.Params(), log)
	must(err)
	books, err := billing.NewBook(data, trail, log)
	must(err)
	accounts, err := tenant.NewAccounts(data, trail, log)
	must(err)
	brandReader, err := tenant.NewBrandReader(data)
	must(err)
	brandWriter, err := tenant.NewBrands(data, trail, log)
	must(err)
	resets, err := adminauth.NewResets(data, cfg)
	must(err)
	resetFlow, err := handler.NewAdminReset(resets, nil, trail, cfg, log)
	must(err)
	keys, err := apikey.NewKeys(apikey.LivePrefix, cfg.APIKeyHMACKey, data, trail)
	must(err)
	panel, err := handler.NewAdminAuth(admins, trail, records, records, reviewer, staff, invites, venues, plaques, entries, rules,
		ruleWriter, books, accounts, brandReader, brandWriter, nil, resetFlow, cfg, log, handler.IntegrationKeys(keys))
	must(err)

	// /v1, as openAPISurface builds it.
	chain, err := apikey.NewAuthenticator(apikey.LivePrefix, cfg.APIKeyHMACKey, data, data)
	must(err)
	writes, err := apikey.NewWrites(data)
	must(err)
	punches, err := tenant.NewPunchFeed(data)
	must(err)
	directory, err := tenant.NewEmployeeDirectory(data)
	must(err)
	v1, err := api.New(api.Config{
		Host: crossAPIHost, Keys: chain, Writes: writes, Suspension: tenant.WriteSuspension{}, Work: crossNoWork{},
		Locations: venues, Employees: staff, Punches: punches, Directory: directory, Log: log,
	})
	must(err)
	created, err := keys.Create(ctx, apikey.CreateCommand{TenantID: c.biz, ActorID: admin, Name: "op15c cross"})
	must(err)
	c.apiKey = created.Key.RevealToTheOwnerOnce()

	c.h = httpx.NewRouter(cfg, log, panel, l.surface, v1)
	return c
}

// crossNoWork is K-19's set: this test reads no webhook.
type crossNoWork struct{}

func (crossNoWork) Notice(uuid.UUID) {}

// serve sends r through the cross-surface router.
func (c *crossSurfaces) serve(r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	return w
}

// panelSignIn signs the business's owner in on the main host: the sign-in page's CSRF field and
// cookies, then the form -- the panel's own sign-in, as a browser drives it.
func (c *crossSurfaces) panelSignIn(t *testing.T) []*http.Cookie {
	t.Helper()
	page := c.serve(httptest.NewRequest(http.MethodGet, "http://"+custHost+"/admin/login", nil))
	body := page.Body.String()
	const marker = `name="csrf" value="`
	i := strings.Index(body, marker)
	if page.Code != http.StatusOK || i < 0 {
		t.Fatalf("the panel's sign-in page = %d, or it carries no CSRF field", page.Code)
	}
	csrf := body[i+len(marker):]
	csrf = csrf[:strings.Index(csrf, `"`)]
	form := url.Values{"csrf": {csrf}, "email": {c.email}, "password": {c.password}}
	r := httptest.NewRequest(http.MethodPost, "http://"+custHost+"/admin/login", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", opBase)
	for _, k := range page.Result().Cookies() {
		r.AddCookie(k)
	}
	w := c.serve(r)
	if w.Code != http.StatusSeeOther || len(w.Result().Cookies()) == 0 {
		t.Fatalf("the panel's sign-in = %d with %d cookie(s), want 303 and a session", w.Code, len(w.Result().Cookies()))
	}
	return w.Result().Cookies()
}

// panelSaveVenue is a gated panel write -- saving a new location -- on the owner's session.
func (c *crossSurfaces) panelSaveVenue(t *testing.T, session []*http.Cookie, name string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "http://"+custHost+"/admin/locations/venue",
		strings.NewReader(url.Values{"name": {name}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", opBase)
	for _, k := range session {
		r.AddCookie(k)
	}
	return c.serve(r)
}

// v1PutVenue is a /v1 write the suspension closes -- PUT /v1/locations/{ref} -- with the business's key.
func (c *crossSurfaces) v1PutVenue(t *testing.T, ref, name string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPut, "http://"+crossAPIHost+"/v1/locations/"+ref,
		strings.NewReader(`{"name":"`+name+`","active":true}`))
	r.Header.Set("Authorization", "Bearer "+c.apiKey)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "op15c-"+uuid.NewString())
	return c.serve(r)
}

// venuesNamed counts the business's locations with one name, in its own context.
func (c *crossSurfaces) venuesNamed(t *testing.T, l *legalE2E, name string) int {
	t.Helper()
	return l.ownerInt(`SELECT count(*)::int FROM locations WHERE tenant_id = $1 AND name = $2`, c.biz, name)
}

// TestE2E_TheOperatorsSuspensionClosesThePanelAndTheAPIAndItsLiftingOpensThem is OP-15's closing
// measurement (K15-13; ADR 0025 C phase annex, LC7 closed), on ONE router and PostgreSQL:
//
//  0. control -- the business is not suspended: the owner's panel write (a new location) is 303 and
//     the location exists; its /v1 write (PUT /v1/locations/{ref}) is 201;
//  1. the operator signs in on the operator host and suspends the business from its overview
//     (POST …/suspend, non_payment, the box ticked): 303 to the overview, which says Suspended;
//  2. the SAME panel session's next write is 403 and writes no location (phase B's gate); the
//     /v1 write is 403 with the problem code tenant_suspended and writes no location (API-4b's
//     checkpoint, reading the column the operator's screen wrote);
//  3. the operator lifts the suspension from the overview (POST …/reinstate, the box ticked): 303,
//     the overview says Active;
//  4. both writes go through again: the panel's 303 and its location, /v1's 201 and its location.
//
// MUTANTS (the card's table): the screen's suspend that writes nothing (store not called) leaves step
// 2's two writes through; the reinstate that writes nothing leaves step 4's two refused.
func TestE2E_TheOperatorsSuspensionClosesThePanelAndTheAPIAndItsLiftingOpensThem(t *testing.T) {
	l := newLegalE2E(t)
	c := newCrossSurfaces(t, l)
	l.h = c.h // the operator's requests go through the same router
	overview := overviewPathOf(c.biz.String())
	suspended := func() bool {
		t.Helper()
		return l.ownerInt(`SELECT count(*)::int FROM tenants WHERE id = $1 AND suspended_at IS NOT NULL`, c.biz) == 1
	}

	// 0. Control.
	panelSession := c.panelSignIn(t)
	if w := c.panelSaveVenue(t, panelSession, "Before"); w.Code != http.StatusSeeOther || c.venuesNamed(t, l, "Before") != 1 {
		t.Fatalf("CONTROL: the panel's location save on a business that is not suspended = %d", w.Code)
	}
	if w := c.v1PutVenue(t, "OP15C-BEFORE", "Before API"); w.Code != http.StatusCreated {
		t.Fatalf("CONTROL: the /v1 PUT on a business that is not suspended = %d %s", w.Code, w.Body.String())
	}

	// 1. The operator suspends the business from its overview.
	op := l.signIn(l.f)
	t.Cleanup(func() {
		if suspended() {
			l.post(reinstatePathOf(c.biz.String()), reinstateForm(), op)
			if suspended() {
				t.Errorf("CLEANUP: the business %s is still suspended; lift it before a migration's Down (K15-6)", c.biz)
			}
		}
	})
	w := l.post(suspendPathOf(c.biz.String()), suspendForm("non_payment"), op)
	if w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != overview || !suspended() {
		t.Fatalf("the operator's suspension = %d to %q (suspended: %v), want 303 to the overview and the column set",
			w.Code, w.Result().Header.Get("Location"), suspended())
	}
	if body := l.get(overview, op).Body.String(); !strings.Contains(body, chipSuspended) {
		t.Fatal("the overview after the suspension does not say Suspended")
	}

	// 2. Both write surfaces are closed.
	if w := c.panelSaveVenue(t, panelSession, "During"); w.Code != http.StatusForbidden || c.venuesNamed(t, l, "During") != 0 {
		t.Errorf("the panel's location save on the suspended business = %d (%d location(s)), want 403 and none",
			w.Code, c.venuesNamed(t, l, "During"))
	}
	w = c.v1PutVenue(t, "OP15C-DURING", "During API")
	var problem struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
		t.Errorf("the /v1 answer is not a problem document: %v", err)
	}
	if w.Code != http.StatusForbidden || problem.Code != string(api.CodeTenantSuspended) || c.venuesNamed(t, l, "During API") != 0 {
		t.Errorf("the /v1 PUT on the suspended business = %d %q (%d location(s)), want 403 tenant_suspended and none",
			w.Code, problem.Code, c.venuesNamed(t, l, "During API"))
	}

	// 3. The operator lifts the suspension.
	w = l.post(reinstatePathOf(c.biz.String()), reinstateForm(), op)
	if w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != overview || suspended() {
		t.Fatalf("the operator's reinstatement = %d to %q (suspended: %v), want 303 to the overview and the column clear",
			w.Code, w.Result().Header.Get("Location"), suspended())
	}
	if body := l.get(overview, op).Body.String(); !strings.Contains(body, chipActive) {
		t.Fatal("the overview after the reinstatement does not say Active")
	}

	// 4. Both write surfaces are open again.
	if w := c.panelSaveVenue(t, panelSession, "After"); w.Code != http.StatusSeeOther || c.venuesNamed(t, l, "After") != 1 {
		t.Errorf("the panel's location save after the lifting = %d (%d location(s)), want 303 and one", w.Code, c.venuesNamed(t, l, "After"))
	}
	if w := c.v1PutVenue(t, "OP15C-AFTER", "After API"); w.Code != http.StatusCreated || c.venuesNamed(t, l, "After API") != 1 {
		t.Errorf("the /v1 PUT after the lifting = %d %s, want 201 and one location", w.Code, w.Body.String())
	}
}
