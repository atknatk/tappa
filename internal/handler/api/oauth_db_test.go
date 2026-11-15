package api

// oauth_db_test.go -- /v1 with an OAuth access token (issue #37 phase 3; ADR 0047 §8, §9, §11, §17)
// over the REAL chain, the shipped authorization server and the database: a grant minted by the
// product's own consent and code exchange, then used on /v1 -- its scopes, GET /v1/business, the
// holding peers left out, another business's data unreachable, Disconnect and the operator's closing
// on the next request, attribution, K-25 and Idempotency-Key per grant, the suspension.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/oauth"
)

const oauthFixtureRegistrar = "api-oauth-fixture@example.test"

// connection is one live grant the rig made: the access token's raw value and its ids.
type connection struct {
	raw            string
	grant, app     uuid.UUID
	client         oauth.Client
	refresh        oauth.Value
	scopes         []string
	tenant, approv uuid.UUID
}

// app registers an app (as the owner -- the registry's op_* need an operator session) with the
// scopes given, under the rig's TAPPA_OAUTH_HMAC_KEY.
func (r *dbRig) app(scopes []string) oauth.Client {
	r.t.Helper()
	ctx := context.Background()
	var operator uuid.UUID
	if err := r.owner.QueryRow(ctx, `
		WITH made AS (
		    INSERT INTO platform_admins (email, display_name, status)
		    SELECT $1, 'api oauth fixture', 'disabled'
		     WHERE NOT EXISTS (SELECT 1 FROM platform_admins WHERE email = $1)
		    RETURNING id)
		SELECT id FROM made UNION ALL SELECT id FROM platform_admins WHERE email = $1 LIMIT 1`, oauthFixtureRegistrar).Scan(&operator); err != nil {
		r.t.Fatalf("the registrar: %v", err)
	}
	secret, hash, err := r.grants.Hasher().MintClientSecret()
	if err != nil {
		r.t.Fatal(err)
	}
	var id uuid.UUID
	if err := r.owner.QueryRow(ctx, `INSERT INTO oauth_clients (name, redirect_uris, allowed_scopes, secret_hash, secret_hint, created_by)
	                                 VALUES ($1, '{https://app.example/cb}', $2, $3, $4, $5) RETURNING id`,
		"App "+uuid.NewString()[:8], oauth.Canonical(scopes), hash.RevealToTheDatabase(), secret.SecretHint(), operator).Scan(&id); err != nil {
		r.t.Fatalf("the app: %v", err)
	}
	c, err := r.grants.AuthenticateClient(ctx, id, secret)
	if err != nil {
		r.t.Fatal(err)
	}
	return c
}

// connect is the product's consent and code exchange for tenant's owner: a live grant with scopes.
func (r *dbRig) connect(tenant, owner uuid.UUID, c oauth.Client, scopes ...string) connection {
	r.t.Helper()
	ctx := context.Background()
	verifier := strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")
	sum := sha256.Sum256([]byte(verifier))
	req := oauth.Request{ClientID: c.ID, RedirectURI: c.RedirectURIs[0], ResponseType: "code", Method: oauth.MethodS256,
		Challenge: base64.RawURLEncoding.EncodeToString(sum[:]), Scopes: oauth.Canonical(scopes), State: "s"}
	code, err := r.grants.Approve(ctx, tenant, owner, req)
	if err != nil {
		r.t.Fatalf("approve: %v", err)
	}
	tok, err := r.grants.ExchangeCode(ctx, ctx, c, code, verifier, nil)
	if err != nil {
		r.t.Fatalf("exchange: %v", err)
	}
	var grant uuid.UUID
	if err := r.owner.QueryRow(ctx, `SELECT id FROM oauth_grants WHERE tenant_id = $1 AND client_id = $2 AND revoked_at IS NULL`, tenant, c.ID).Scan(&grant); err != nil {
		r.t.Fatal(err)
	}
	return connection{raw: tok.Access.RevealInTokenResponse(), grant: grant, app: c.ID, client: c, refresh: tok.Refresh,
		scopes: oauth.Canonical(scopes), tenant: tenant, approv: owner}
}

// neverOpened makes tenant's integration API never opened (both K-22 columns NULL -- OA-1).
func (r *dbRig) neverOpened(tenant uuid.UUID) {
	r.t.Helper()
	if _, err := r.owner.Exec(context.Background(), `UPDATE tenants SET api_enabled_at = NULL, api_code = NULL WHERE id = $1`, tenant); err != nil {
		r.t.Fatal(err)
	}
}

// TestOAuthAPI_ATokenOpensItsScopesAndTheBusinessCard (ADR 0047 §8.2, §8.4, §11.1, §17): on a business
// whose API Taptime never opened, a grant of timesheets:read employees:read employees:write (Malta
// Calculator's request) reads GET /v1/business -- {id: biz_…, name, companyCode: null} --, its
// employees and its timesheets; GET /v1/punches and PUT /v1/locations are 403 insufficient_scope with
// the route's scope; a key of that business is 403 api_disabled (K-22 is unchanged for keys); and a
// key of an OPEN business reads its card with its company code.
func TestOAuthAPI_ATokenOpensItsScopesAndTheBusinessCard(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	keyRaw, _ := r.key(biz, owner)
	r.neverOpened(biz)
	c := r.connect(biz, owner, r.app([]string{oauth.ScopeTimesheetsRead, oauth.ScopeEmployeesRead, oauth.ScopeEmployeesWrite}),
		oauth.ScopeTimesheetsRead, oauth.ScopeEmployeesRead, oauth.ScopeEmployeesWrite)
	w := r.do(http.MethodGet, "/v1/business", c.raw, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /v1/business: %d %s", w.Code, w.Body.String())
	}
	var card map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &card); err != nil {
		t.Fatal(err)
	}
	if card["id"] != "biz_"+biz.String() || card["name"] != "API-4 fixture" || card["companyCode"] != nil || len(card) != 3 {
		t.Fatalf("the card: %v", card)
	}
	if !strings.Contains(w.Body.String(), `"companyCode":null`) {
		t.Fatalf("companyCode is not null on the wire: %s", w.Body.String())
	}
	for _, p := range []string{"/v1/employees", "/v1/timesheets?from=2026-11-02&to=2026-11-08"} {
		if w := r.do(http.MethodGet, p, c.raw, ""); w.Code != http.StatusOK {
			t.Fatalf("GET %s with its scope: %d %s", p, w.Code, w.Body.String())
		}
	}
	for _, x := range []struct{ method, path, body, scope string }{
		{http.MethodGet, "/v1/punches?scope=employer", "", "punches:read"},
		{http.MethodPut, "/v1/locations/L-1", `{"name":"V","email":null,"active":true}`, "locations:write"},
	} {
		w := r.do(x.method, x.path, c.raw, x.body)
		p := problemOf(t, w)
		if w.Code != http.StatusForbidden || p.Code != CodeInsufficientScope || p.Details["scope"] != x.scope {
			t.Fatalf("%s %s without its scope: %d %s", x.method, x.path, w.Code, w.Body.String())
		}
	}
	if w := r.do(http.MethodGet, "/v1/business", keyRaw, ""); w.Code != http.StatusForbidden || problemOf(t, w).Code != CodeAPIDisabled {
		t.Fatalf("the business's key on a never-opened API: %d %s", w.Code, w.Body.String())
	}
	open, openOwner := r.business(true)
	openKey, _ := r.key(open, openOwner)
	w = r.do(http.MethodGet, "/v1/business", openKey, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"companyCode":"APIFOUR"`) || !strings.Contains(w.Body.String(), `"id":"biz_`+open.String()+`"`) {
		t.Fatalf("a key's card: %d %s", w.Code, w.Body.String())
	}
	var used *time.Time
	if err := r.owner.QueryRow(context.Background(), `SELECT last_used_at FROM oauth_grants WHERE id = $1`, c.grant).Scan(&used); err != nil || used == nil {
		t.Fatalf("the grant's last use: %v %v", used, err)
	}
}

// TestOAuthAPI_EveryEndIsInForceOnTheNextRequest (ADR 0047 §7.3, §11.4, §17 "iptal edilen grant bir
// sonraki istekte red"): Disconnect -> 401 invalid_token; a new connection, then the operator
// disables the app -> 401; a third, then the operator closes the business's API -> 403 api_disabled,
// and reopened -> 200 with the same token; an expired token -> 401.
//
// MUTANT (measured, phase 3 report): ResolveAccess's GrantRevokedAt check removed -> the Disconnected
// grant's token is 200 and this is red.
func TestOAuthAPI_EveryEndIsInForceOnTheNextRequest(t *testing.T) {
	r := newDBRig(t)
	ctx := context.Background()
	biz, owner := r.business(true)
	scopes := []string{oauth.ScopeEmployeesRead}
	app := r.app(scopes)
	c := r.connect(biz, owner, app, scopes...)
	if w := r.do(http.MethodGet, "/v1/employees", c.raw, ""); w.Code != http.StatusOK {
		t.Fatalf("a live token: %d %s", w.Code, w.Body.String())
	}
	if err := r.grants.Disconnect(ctx, biz, owner, c.grant); err != nil {
		t.Fatal(err)
	}
	w := r.do(http.MethodGet, "/v1/employees", c.raw, "")
	if w.Code != http.StatusUnauthorized || w.Header().Get("WWW-Authenticate") != `Bearer realm="taptime", error="invalid_token"` {
		t.Fatalf("after Disconnect: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	c2 := r.connect(biz, owner, app, scopes...)
	if _, err := r.owner.Exec(ctx, `UPDATE oauth_clients SET disabled_at = clock_timestamp() WHERE id = $1`, app.ID); err != nil {
		t.Fatal(err)
	}
	if w := r.do(http.MethodGet, "/v1/employees", c2.raw, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("a disabled app's token: %d %s", w.Code, w.Body.String())
	}
	c3 := r.connect(biz, owner, r.app(scopes), scopes...)
	r.setAPI(biz, false)
	if w := r.do(http.MethodGet, "/v1/employees", c3.raw, ""); w.Code != http.StatusForbidden || problemOf(t, w).Code != CodeAPIDisabled {
		t.Fatalf("the operator closed the API: %d %s", w.Code, w.Body.String())
	}
	r.setAPI(biz, true)
	if w := r.do(http.MethodGet, "/v1/employees", c3.raw, ""); w.Code != http.StatusOK {
		t.Fatalf("reopened: %d %s", w.Code, w.Body.String())
	}
	if _, err := r.owner.Exec(ctx, `UPDATE oauth_tokens SET created_at = created_at - interval '2 hours', expires_at = expires_at - interval '2 hours'
	                                WHERE grant_id = $1 AND kind = 'access'`, c3.grant); err != nil {
		t.Fatal(err)
	}
	if w := r.do(http.MethodGet, "/v1/employees", c3.raw, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("an expired token: %d %s", w.Code, w.Body.String())
	}
}

// TestOAuthAPI_AnotherBusinessIsNeverReached (§9, §17 "cross-tenant token reddi"): business A's token,
// holding every scope a grant can carry here, reaches NONE of business B's data through any /v1 route
// of its scopes -- the tenant is the grant's, the request names none; every read and write is
// WithTenant(the grant's) + the explicit filter. A and B are not holding peers (the punch world's KF
// and CC). Each has an employee under a reference, and a completed shift -- an in and an out on its own
// plaque. B's card is B's; A's card is A's; B's reference is 404 to A's read, DELETE and resend, and is
// A's OWN new row to A's PUT -- B's row is unchanged and B's trail has no row of A's grant. A's lists,
// directory, timesheet and both punch scopes carry A's own data (the positive half: an empty answer
// would also "name nothing of B's") and nothing of B's.
//
// MUTANT (measured, phase 4 reports; the phase 3 audit's probe made permanent): the chain's principal
// given the tenant of another grant it has seen -> A's card is B's, red; with the card's check skipped,
// the timesheet and both punch scopes are red too.
func TestOAuthAPI_AnotherBusinessIsNeverReached(t *testing.T) {
	w := newPunchWorld(t, unmetered)
	r := w.r
	ctx := context.Background()
	a, b := w.kf, w.c
	for _, x := range []struct {
		tenant    uuid.UUID
		ref, name string
	}{{a.id, "A-1", "Alpha Staff"}, {b.id, "B-1", "Bravo Staff"}} {
		if _, err := r.owner.Exec(ctx, `INSERT INTO employees (tenant_id, external_ref, full_name, status) VALUES ($1, $2, $3, 'active')`, x.tenant, x.ref, x.name); err != nil {
			t.Fatal(err)
		}
	}
	shift := func(biz punchBiz) []string {
		in, out := own(biz, "nfc", "in"), own(biz, "nfc", "out")
		in.ago, out.ago = 3*time.Hour, time.Hour
		return []string{pch(w.write(in)), pch(w.write(out))}
	}
	aPunches, bPunches := shift(a), shift(b)
	all := []string{oauth.ScopeTimesheetsRead, oauth.ScopeEmployeesRead, oauth.ScopeEmployeesWrite, oauth.ScopePunchesRead}
	ca := r.connect(a.id, a.admin, r.app(all), all...)
	cb := r.connect(b.id, b.admin, r.app(all), all...)
	// B's markers: its id, its venue's, its employees' (by id and by reference) and its punches'.
	bMarks := append([]string{b.id.String(), b.venue.String(), b.emp.String(), "B-1", "Bravo", b.code + "-EMP"}, bPunches...)
	noneOfB := func(what, body string) {
		t.Helper()
		for _, m := range bMarks {
			if strings.Contains(body, m) {
				t.Fatalf("%s by A's token names B's %q: %s", what, m, body)
			}
		}
	}

	// CONTROL first: B's own token reads B's card.
	if w := r.do(http.MethodGet, "/v1/business", cb.raw, ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"id":"biz_`+b.id.String()+`"`) {
		t.Fatalf("CONTROL: B's card by B's token: %d %s", w.Code, w.Body.String())
	}
	card := r.do(http.MethodGet, "/v1/business", ca.raw, "")
	if card.Code != http.StatusOK || !strings.Contains(card.Body.String(), `"id":"biz_`+a.id.String()+`"`) {
		t.Fatalf("A's card by A's token: %d %s", card.Code, card.Body.String())
	}
	noneOfB("the card", card.Body.String())
	if w := r.do(http.MethodGet, "/v1/employees/B-1", ca.raw, ""); w.Code != http.StatusNotFound {
		t.Fatalf("B's reference with A's token: %d %s", w.Code, w.Body.String())
	}
	if w := r.do(http.MethodGet, "/v1/employees/A-1", ca.raw, ""); w.Code != http.StatusOK {
		t.Fatalf("CONTROL: A's own reference: %d %s", w.Code, w.Body.String())
	}
	day := func(d time.Duration) string { return time.Now().UTC().Add(d).Format("2006-01-02") }
	reads := []struct{ path, own string }{
		{"/v1/employees", "Alpha"},
		{"/v1/directory", "Alpha"},
		{"/v1/timesheets?from=" + day(-24*time.Hour) + "&to=" + day(24*time.Hour), a.emp.String()},
	}
	for _, rd := range reads {
		w := r.do(http.MethodGet, rd.path, ca.raw, "")
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), rd.own) {
			t.Fatalf("%s by A's token: %d, A's own %q missing: %s", rd.path, w.Code, rd.own, w.Body.String())
		}
		noneOfB(rd.path, w.Body.String())
		if strings.HasPrefix(rd.path, "/v1/timesheets") {
			var page struct {
				Data []struct {
					Employee      struct{ ID string }
					WorkedMinutes int64
				}
			}
			if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			worked := int64(-1)
			for _, p := range page.Data {
				if p.Employee.ID == employeeIDPrefix+a.emp.String() {
					worked = p.WorkedMinutes
				}
			}
			if worked != 120 {
				t.Fatalf("A's timesheet counts %d minutes for A's own shift, want 120: %s", worked, w.Body.String())
			}
		}
	}
	token := punchBiz{code: "A-token", raw: ca.raw}
	for _, scope := range []string{"employer", "location"} {
		got := w.all(token, scope, 1000)
		if gotIDs := ids(got); !slices.Equal(gotIDs, aPunches) {
			t.Fatalf("punches?scope=%s by A's token: %v, want A's own shift %v", scope, gotIDs, aPunches)
		}
		raw, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		noneOfB("punches?scope="+scope, string(raw))
	}
	if w := r.do(http.MethodDelete, "/v1/employees/B-1", ca.raw, ""); w.Code != http.StatusNotFound {
		t.Fatalf("DELETE B-1 by A's token: %d %s", w.Code, w.Body.String())
	}
	if w := r.do(http.MethodPost, "/v1/employees/B-1/resend-activation", ca.raw, ""); w.Code != http.StatusNotFound {
		t.Fatalf("resend B-1 by A's token: %d %s", w.Code, w.Body.String())
	}
	// A PUT of B's reference is A's own new employee -- the reference is looked up in A alone.
	if w := r.do(http.MethodPut, "/v1/employees/B-1", ca.raw, `{"firstName":"Mallory","lastName":"X","email":null,"active":true}`); w.Code != http.StatusCreated {
		t.Fatalf("PUT B-1 by A's token: %d %s", w.Code, w.Body.String())
	}
	var bName, bStatus string
	if err := r.owner.QueryRow(ctx, `SELECT full_name, status FROM employees WHERE tenant_id = $1 AND external_ref = 'B-1'`, b.id).Scan(&bName, &bStatus); err != nil {
		t.Fatal(err)
	}
	if bName != "Bravo Staff" || bStatus != "active" {
		t.Fatalf("B's B-1 changed by A's token: %q %q", bName, bStatus)
	}
	if n := r.ownerCount(`SELECT count(*) FROM employees WHERE tenant_id = $1 AND external_ref = 'B-1'`, a.id); n != 1 {
		t.Fatalf("A's own B-1: %d rows", n)
	}
	if n := r.ownerCount(`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND actor_id = $2`, b.id, ca.grant); n != 0 {
		t.Fatalf("%d audit rows in B by A's grant", n)
	}
}

// TestOAuthAPI_HoldingPeersNeverLeaveThroughAToken (ADR 0047 §8.5, OA-4, ORTA-1; §17 "eş süzgeci"):
// KF and KM are holding peers. KF's OAuth token reads, in the directory, KF's people alone (no KM
// employee); in the feed's two scopes and in the history query, KF's own punch alone -- not KF's
// employee at KM's plaque (employer), not KM's employee at KF's plaque (location). KF's API KEY reads
// all of them, as before. The feed's cursor walks the holes with limit=1 and skips nothing of KF's own.
//
// MUTANT (measured, phase 3 report): the directory's OwnOnly ignored -> KM's employee is in KF's token
// directory; the feed's own_only passed false -> the holding punches are in it. Each red.
func TestOAuthAPI_HoldingPeersNeverLeaveThroughAToken(t *testing.T) {
	w := newPunchWorld(t)
	kfkf := w.write(own(w.kf, "nfc", "in"))
	kfkm := w.write(at(w.kf, w.km, "out"))
	kmkf := w.write(at(w.km, w.kf, "in"))
	kfkf2 := w.write(own(w.kf, "nfc", "out"))
	all := []string{oauth.ScopeEmployeesRead, oauth.ScopePunchesRead}
	c := w.r.connect(w.kf.id, w.kf.admin, w.r.app(all), all...)
	token := punchBiz{code: "KF-token", raw: c.raw}

	// The feed, both scopes, a page of one at a time.
	ownOnly := []string{pch(kfkf), pch(kfkf2)}
	for _, scope := range []string{"employer", "location"} {
		if got := ids(w.all(token, scope, 1)); !slices.Equal(got, ownOnly) {
			t.Errorf("KF's token, scope=%s: %v, want %v", scope, got, ownOnly)
		}
	}
	if got := ids(w.all(w.kf, "employer", 1000)); !slices.Equal(got, []string{pch(kfkf), pch(kfkm), pch(kfkf2)}) {
		t.Errorf("CONTROL: KF's key, employer: %v", got)
	}
	if got := ids(w.all(w.kf, "location", 1000)); !slices.Equal(got, []string{pch(kfkf), pch(kmkf), pch(kfkf2)}) {
		t.Errorf("CONTROL: KF's key, location: %v", got)
	}

	// The history query.
	from, to := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339), time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	for _, scope := range []string{"employer", "location"} {
		p := pageOf(t, w.get(token.raw, "scope="+scope+"&from="+from+"&to="+to))
		if got := ids(p.Data); !slices.Equal(got, ownOnly) || p.HasMore {
			t.Errorf("KF's token history, scope=%s: %v (hasMore %v), want %v", scope, got, p.HasMore, ownOnly)
		}
		kp := pageOf(t, w.get(w.kf.raw, "scope="+scope+"&from="+from+"&to="+to))
		if len(kp.Data) != 3 {
			t.Errorf("CONTROL: KF's key history, scope=%s: %v", scope, ids(kp.Data))
		}
	}

	// The directory.
	entries, _, _ := w.dirWalk(token, "", 1000)
	for _, e := range entries {
		if e.ID == employeeIDPrefix+w.km.emp.String() {
			t.Fatal("KF's token directory names KM's employee")
		}
	}
	if !slices.ContainsFunc(entries, func(e directoryEntryBody) bool { return e.ID == employeeIDPrefix+w.kf.emp.String() }) {
		t.Fatal("CONTROL: KF's token directory lacks KF's own employee")
	}
	keyEntries, _, _ := w.dirWalk(w.kf, "", 1000)
	if !slices.ContainsFunc(keyEntries, func(e directoryEntryBody) bool { return e.ID == employeeIDPrefix+w.km.emp.String() }) {
		t.Fatal("CONTROL: KF's KEY directory does not name KM's employee (the peer half is the key's)")
	}
}

// TestOAuthAPI_AGrantsWritesAreItsOwn (ADR 0047 §8.7, §17): a PUT through a token leaves audit rows
// whose actor_id is the GRANT and whose detail says via=api and the app's client_id; Idempotency-Key
// replays in the grant's own namespace (api_idempotency.grant_id), and another grant's identical key
// is a different request (no collision); K-25's budget is the grant's -- shrunk to 1, the grant's
// second deactivation is 429 while the business's key still has its own; the owner's budget alert
// names the grant.
func TestOAuthAPI_AGrantsWritesAreItsOwn(t *testing.T) {
	r := newDBRig(t, func(c *Config) { c.DeactivationBudget = 1 })
	ctx := context.Background()
	biz, owner := r.business(true)
	keyRaw, _ := r.key(biz, owner)
	write := []string{oauth.ScopeEmployeesWrite}
	c := r.connect(biz, owner, r.app(write), write...)
	other := r.connect(biz, owner, r.app(write), write...)
	body := `{"firstName":"Jane","lastName":"Grant","email":null,"active":true}`
	idem := []string{"Idempotency-Key", "grant-order-1"}
	first := r.do(http.MethodPut, "/v1/employees/G-1", c.raw, body, idem...)
	if first.Code != http.StatusCreated {
		t.Fatalf("a grant's PUT: %d %s", first.Code, first.Body.String())
	}
	if again := r.do(http.MethodPut, "/v1/employees/G-1", c.raw, body, idem...); again.Code != http.StatusCreated || again.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("the replay: %d %v", again.Code, again.Header())
	}
	if w := r.do(http.MethodPut, "/v1/employees/G-1", other.raw, body, idem...); w.Code != http.StatusOK || w.Header().Get("Idempotent-Replayed") != "" {
		t.Fatalf("another grant's identical key: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	var rows, keyed int
	if err := r.owner.QueryRow(ctx, `SELECT count(*) FILTER (WHERE grant_id = $2), count(*) FILTER (WHERE key_id IS NOT NULL)
	                                 FROM api_idempotency WHERE tenant_id = $1`, biz, c.grant).Scan(&rows, &keyed); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || keyed != 0 {
		t.Fatalf("the ledger: %d rows of the grant, %d of a key", rows, keyed)
	}
	var trail int
	if err := r.owner.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = 'employee.added'
	                                 AND actor_id = $2 AND detail->>'via' = 'api' AND detail->>'client_id' = $3`,
		biz, c.grant, oauth.ClientIDOf(c.app)).Scan(&trail); err != nil {
		t.Fatal(err)
	}
	if trail != 1 {
		t.Fatalf("%d employee.added rows attributed to the grant and its app", trail)
	}
	for _, ref := range []string{"G-2", "G-3", "K-1"} {
		if w := r.do(http.MethodPut, "/v1/employees/"+ref, keyRaw, `{"firstName":"A","lastName":"B","email":null,"active":true}`); w.Code != http.StatusCreated {
			t.Fatalf("seed %s: %d %s", ref, w.Code, w.Body.String())
		}
	}
	if w := r.do(http.MethodDelete, "/v1/employees/G-2", c.raw, ""); w.Code != http.StatusOK {
		t.Fatalf("the grant's first deactivation: %d %s", w.Code, w.Body.String())
	}
	if w := r.do(http.MethodDelete, "/v1/employees/G-3", c.raw, ""); w.Code != http.StatusTooManyRequests || problemOf(t, w).Code != CodeDeactivationBudgetExceeded {
		t.Fatalf("the grant's second deactivation: %d %s", w.Code, w.Body.String())
	}
	if w := r.do(http.MethodDelete, "/v1/employees/K-1", keyRaw, ""); w.Code != http.StatusOK {
		t.Fatalf("the key's own budget: %d %s", w.Code, w.Body.String())
	}
	var alert int
	if err := r.owner.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = 'api.deactivation_budget_exceeded'
	                                 AND target = $2 AND detail->>'client_id' = $3`, biz, c.grant.String(), oauth.ClientIDOf(c.app)).Scan(&alert); err != nil {
		t.Fatal(err)
	}
	if alert != 1 {
		t.Fatalf("%d budget alerts naming the grant", alert)
	}
	r.keys.NameAppsWith(r.d)
	alerts, err := r.keys.BudgetAlerts(ctx, biz, owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 || alerts[0].ClientID == nil || *alerts[0].ClientID != c.app || alerts[0].AppName != c.client.Name || alerts[0].KeyName != "" {
		t.Fatalf("the owner's alerts: %+v", alerts)
	}
}

// TestOAuthAPI_ASuspensionClosesAGrantsWritesNotItsReads (ADR 0047 §8.7, §11.2: ADR 0028 §10's table
// applied to a grant, cell by cell as for a key): while the business is suspended a token's GET is
// 200, opening a new employee stays open (the precondition of any record -- K-B1), and a deactivation
// is 403 tenant_suspended.
func TestOAuthAPI_ASuspensionClosesAGrantsWritesNotItsReads(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	both := []string{oauth.ScopeEmployeesRead, oauth.ScopeEmployeesWrite}
	c := r.connect(biz, owner, r.app(both), both...)
	if _, err := r.owner.Exec(context.Background(), `INSERT INTO employees (tenant_id, external_ref, full_name, status) VALUES ($1, 'S-0', 'Suspended Staff', 'active')`, biz); err != nil {
		t.Fatal(err)
	}
	r.suspend(biz)
	if w := r.do(http.MethodGet, "/v1/employees", c.raw, ""); w.Code != http.StatusOK {
		t.Fatalf("a read while suspended: %d %s", w.Code, w.Body.String())
	}
	if w := r.do(http.MethodPut, "/v1/employees/S-1", c.raw, `{"firstName":"S","lastName":"T","email":null,"active":true}`); w.Code != http.StatusCreated {
		t.Fatalf("opening an employee while suspended (ADR 0028 §10: open): %d %s", w.Code, w.Body.String())
	}
	if w := r.do(http.MethodDelete, "/v1/employees/S-0", c.raw, ""); w.Code != http.StatusForbidden || problemOf(t, w).Code != CodeTenantSuspended {
		t.Fatalf("a deactivation while suspended: %d %s", w.Code, w.Body.String())
	}
}

// TestOAuthAPI_ATokenInvitesOnABusinessWhoseAPIWasNeverOpened is ADR 0047 §11.3 end to end (YÜKSEK-1,
// §17 "e-postalı çalışan PUT, adres değişikliği ve resend-activation OAuth tokenıyla uçtan uca bir
// davet satırı doğurur"): on a business whose API Taptime never opened (both K-22 columns NULL), an
// employees:write token's three paths -- a new employee with an address, an address given to a
// not-yet-activated employee, and resend-activation an hour after the send -- each leave one
// invite_requests row, and the queue (the shipped gate, the shipped app resolver) SENDS each: three
// e-mails. The business's key is 403 api_disabled meanwhile (K-22 is unchanged for keys).
//
// MUTANT (measured, phase 4 report): the queue gate's grant arm removed (a never-opened API closed
// whatever grant) -> nothing is sent, red.
func TestOAuthAPI_ATokenInvitesOnABusinessWhoseAPIWasNeverOpened(t *testing.T) {
	r := newDBRig(t)
	ctx := context.Background()
	biz, owner := r.business(true)
	keyRaw, _ := r.key(biz, owner)
	r.neverOpened(biz)
	write := []string{oauth.ScopeEmployeesWrite}
	c := r.connect(biz, owner, r.app(write), write...)
	q, sink, _ := r.queue()
	requests := func(id uuid.UUID) int {
		return r.ownerCount(`SELECT count(*) FROM invite_requests WHERE tenant_id = $1 AND employee_id = $2`, biz, id)
	}

	ref, email := newRef("OA"), uniqueEmail("oauth-new")
	w := r.putEmp(c.raw, ref, empBody("Maria", "Borg", &email, true))
	if w.Code != http.StatusCreated || empOf(t, w).ActivationEmail.Status != "queued" {
		t.Fatalf("a new employee with an address by the token: %d %s", w.Code, w.Body.String())
	}
	first := idOfEmp(t, empOf(t, w))
	if requests(first) != 1 {
		t.Fatal("the token's PUT queued no request")
	}
	runQueue(t, q, biz)
	if len(sink.sent()) != 1 {
		t.Fatalf("the queue sent %d e-mails for the token's new employee, want 1 (the grant opens the gate)", len(sink.sent()))
	}

	later := newRef("OA")
	w = r.putEmp(c.raw, later, empBody("Joe", "Camilleri", nil, true))
	if w.Code != http.StatusCreated {
		t.Fatalf("a new employee with no address: %d %s", w.Code, w.Body.String())
	}
	second := idOfEmp(t, empOf(t, w))
	address := uniqueEmail("oauth-change")
	if w := r.putEmp(c.raw, later, empBody("Joe", "Camilleri", &address, true)); w.Code != http.StatusOK || empOf(t, w).ActivationEmail.Status != "queued" {
		t.Fatalf("an address for a not-activated employee: %d %s", w.Code, w.Body.String())
	}
	if requests(second) != 1 {
		t.Fatal("the address change queued no request")
	}
	runQueue(t, q, biz)
	if len(sink.sent()) != 2 {
		t.Fatalf("after the address change the queue sent %d e-mails in all, want 2", len(sink.sent()))
	}

	if _, err := r.owner.Exec(ctx, `UPDATE employee_invites SET created_at = now() - interval '61 minutes' WHERE tenant_id = $1 AND employee_id = $2`, biz, first); err != nil {
		t.Fatal(err)
	}
	w = r.do(http.MethodPost, "/v1/employees/"+ref+"/resend-activation", c.raw, "")
	if w.Code != http.StatusAccepted || empOf(t, w).ActivationEmail.Status != "queued" {
		t.Fatalf("resend-activation by the token an hour later: %d %s", w.Code, w.Body.String())
	}
	runQueue(t, q, biz)
	if len(sink.sent()) != 3 {
		t.Fatalf("after the resend the queue sent %d e-mails in all, want 3", len(sink.sent()))
	}
	if w := r.do(http.MethodGet, "/v1/business", keyRaw, ""); w.Code != http.StatusForbidden || problemOf(t, w).Code != CodeAPIDisabled {
		t.Fatalf("CONTROL: the business's key on its never-opened API: %d %s", w.Code, w.Body.String())
	}
}

// TestOAuthAPI_ASecretRotationKillsNoToken (ADR 0047 §10.2, §17 "secret rotasyonu tokenları
// öldürmez"): the operator's rotation replaces the app's secret -- the old one is refused at the
// token endpoint from the next request on -- and leaves every issued token alive: the access token
// still reads /v1, and the refresh token, presented with the NEW secret, still rotates.
//
// The rotation is op_rotate_oauth_client_secret's own write applied as the owner (rotateSecret):
// that function's body updates oauth_clients alone (00107) and tappa_opdefiner holds no right on
// oauth_grants or oauth_tokens (00104, 00107 grant it none), so this is the whole of what a rotation
// changes. MUTANT (measured, phase 4 report):
// AuthenticateClient ignoring secret_ok -> the old secret still authenticates, red.
func TestOAuthAPI_ASecretRotationKillsNoToken(t *testing.T) {
	r := newDBRig(t)
	ctx := context.Background()
	biz, owner := r.business(true)
	scopes := []string{oauth.ScopeEmployeesRead}
	app := r.app(scopes)
	c := r.connect(biz, owner, app, scopes...)
	var oldSecret oauth.Value
	{
		// The rig registered the app with a secret it did not keep: rotate twice, keeping the first
		// rotation's secret as "the old one".
		v, d, err := r.grants.Hasher().MintClientSecret()
		if err != nil {
			t.Fatal(err)
		}
		r.rotateSecret(app.ID, v, d)
		oldSecret = v
	}
	if _, err := r.grants.AuthenticateClient(ctx, app.ID, oldSecret); err != nil {
		t.Fatalf("CONTROL: the current secret: %v", err)
	}
	newSecret, digest, err := r.grants.Hasher().MintClientSecret()
	if err != nil {
		t.Fatal(err)
	}
	r.rotateSecret(app.ID, newSecret, digest)
	if _, err := r.grants.AuthenticateClient(ctx, app.ID, oldSecret); !errors.Is(err, oauth.ErrInvalidClient) {
		t.Fatalf("the secret before the rotation: %v, want invalid_client", err)
	}
	if w := r.do(http.MethodGet, "/v1/employees", c.raw, ""); w.Code != http.StatusOK {
		t.Fatalf("the access token after the rotation: %d %s", w.Code, w.Body.String())
	}
	client, err := r.grants.AuthenticateClient(ctx, app.ID, newSecret)
	if err != nil {
		t.Fatalf("the new secret: %v", err)
	}
	tok, err := r.grants.Refresh(ctx, ctx, client, c.refresh, nil)
	if err != nil {
		t.Fatalf("the refresh token after the rotation: %v", err)
	}
	if w := r.do(http.MethodGet, "/v1/employees", tok.Access.RevealInTokenResponse(), ""); w.Code != http.StatusOK {
		t.Fatalf("the refreshed access token: %d %s", w.Code, w.Body.String())
	}
	if n := r.ownerCount(`SELECT count(*) FROM oauth_grants WHERE id = $1 AND revoked_at IS NULL`, c.grant); n != 1 {
		t.Fatal("the rotation revoked the grant")
	}
}

// rotateSecret is op_rotate_oauth_client_secret's write, as the owner (the rig has no operator
// session -- app() registers the same way): a new hash and hint, the rotation instant.
func (r *dbRig) rotateSecret(app uuid.UUID, secret oauth.Value, d oauth.Digest) {
	r.t.Helper()
	if _, err := r.owner.Exec(context.Background(), `UPDATE oauth_clients SET secret_hash = $2, secret_hint = $3, secret_rotated_at = clock_timestamp() WHERE id = $1`,
		app, d.RevealToTheDatabase(), secret.SecretHint()); err != nil {
		r.t.Fatal(err)
	}
}

// TestOAuthAPI_ANarrowedTokenIsRefusedOutsideItsScope (ADR 0047 §5.3, §8.1 step 8, §17 "daraltılmış
// erişim tokenı daraltmanın dışındaki rotada 403 insufficient_scope alır (grant o kapsamı taşısa
// bile)"): a grant of timesheets:read employees:read refreshed with scope=timesheets:read gives an
// access token that reads /v1/timesheets and is 403 insufficient_scope (details.scope employees:read)
// on /v1/employees -- the grant still holds employees:read, and the first access token still reads it.
//
// MUTANTS (measured, phase 4 report): the refresh writing the grant's scopes on the access token
// instead of the narrowed set, and ResolveAccess answering every known scope instead of the token's
// own -> the narrowed token reads /v1/employees, each red.
func TestOAuthAPI_ANarrowedTokenIsRefusedOutsideItsScope(t *testing.T) {
	r := newDBRig(t)
	ctx := context.Background()
	biz, owner := r.business(true)
	both := []string{oauth.ScopeTimesheetsRead, oauth.ScopeEmployeesRead}
	c := r.connect(biz, owner, r.app(both), both...)
	tok, err := r.grants.Refresh(ctx, ctx, c.client, c.refresh, []string{oauth.ScopeTimesheetsRead})
	if err != nil {
		t.Fatal(err)
	}
	narrow := tok.Access.RevealInTokenResponse()
	if w := r.do(http.MethodGet, "/v1/timesheets?from=2026-11-02&to=2026-11-08", narrow, ""); w.Code != http.StatusOK {
		t.Fatalf("the narrowed token on its own scope: %d %s", w.Code, w.Body.String())
	}
	w := r.do(http.MethodGet, "/v1/employees", narrow, "")
	if p := problemOf(t, w); w.Code != http.StatusForbidden || p.Code != CodeInsufficientScope || p.Details["scope"] != oauth.ScopeEmployeesRead {
		t.Fatalf("the narrowed token outside its scope: %d %s", w.Code, w.Body.String())
	}
	var grantScopes []string
	if err := r.owner.QueryRow(ctx, `SELECT scopes FROM oauth_grants WHERE id = $1`, c.grant).Scan(&grantScopes); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(grantScopes, oauth.ScopeEmployeesRead) {
		t.Fatalf("CONTROL: the grant's scopes %v lack employees:read", grantScopes)
	}
	if w := r.do(http.MethodGet, "/v1/employees", c.raw, ""); w.Code != http.StatusOK {
		t.Fatalf("CONTROL: the first (grant-wide) access token: %d %s", w.Code, w.Body.String())
	}
}
