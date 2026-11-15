package api

// oauthscope_test.go -- the OAuth access token's half of the Bearer chain (issue #37, ADR 0047 §8.1,
// §8.2, §8.3, §8.7) without a database: the shape before the resolver, the address bucket's
// Reserve/Keep/Refund, the grant and app buckets, invalid_token and api_disabled, the scope table on
// every authenticated route and its closed default. The database half (a real grant, Disconnect, the
// holding peers, another business) is oauth_db_test.go.

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/oauth"
)

// TestOAuthChain_TheShapeIsDecidedWithoutTheDatabase: an access token of the other build's prefix,
// one a character short and a refresh-shaped value are 401 with ZERO lookups -- neither the token
// resolver nor the key resolver is asked (ADR 0047 §17: "tt_at_test_ prod'da DB'siz 401").
func TestOAuthChain_TheShapeIsDecidedWithoutTheDatabase(t *testing.T) {
	r := newRig(t, unknown)
	tok := accessToken(t)
	for name, v := range map[string]string{
		"the other build's prefix": sandboxAccessToken(t),
		"one character short":      tok[:len(tok)-1],
		"a refresh token's prefix": oauth.Prefix(oauth.KindRefresh) + tok[len(oauth.Prefix(oauth.KindAccess)):],
	} {
		w := r.do(http.MethodGet, "/v1/business", "Bearer "+v, "", nil)
		if w.Code != http.StatusUnauthorized || problemOf(t, w).Code != CodeUnauthorized {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
	if res, _ := r.grants.counts(); res != 0 || r.resolver.count() != 0 {
		t.Fatalf("%d token and %d key lookups, want 0 and 0", res, r.resolver.count())
	}
}

// TestOAuthChain_InvalidTokenAndAClosedAPI: the resolver's two refusals -- 401 with RFC 6750's
// error="invalid_token" challenge, and 403 api_disabled (the operator closed the API, §11.4) -- and a
// live token reaches the route with its grant as the principal (its tenant, no key id).
func TestOAuthChain_InvalidTokenAndAClosedAPI(t *testing.T) {
	r := newRig(t, unknown)
	w := r.do(http.MethodGet, "/v1/business", "Bearer "+accessToken(t), "", nil)
	if w.Code != http.StatusUnauthorized || problemOf(t, w).Code != CodeUnauthorized ||
		w.Header().Get("WWW-Authenticate") != `Bearer realm="taptime", error="invalid_token"` {
		t.Fatalf("an invalid token: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	r.grants.set(func() (oauth.Access, error) { return oauth.Access{}, oauth.ErrAPIClosed })
	if w := r.do(http.MethodGet, "/v1/business", "Bearer "+accessToken(t), "", nil); w.Code != http.StatusForbidden || problemOf(t, w).Code != CodeAPIDisabled {
		t.Fatalf("a closed API: %d %s", w.Code, w.Body.String())
	}
	live := grantOf(oauth.ScopeTimesheetsRead)
	a, _ := live()
	r.grants.set(live)
	var seen struct {
		tenant, grant string
		key           bool
	}
	withTestRoutes(r.s, func(rt chi.Router) {
		rt.Get("/_who", func(w http.ResponseWriter, req *http.Request) {
			p, _ := PrincipalOf(req)
			seen.tenant, seen.grant, seen.key = p.TenantID.String(), p.GrantID.String(), p.KeyID != [16]byte{}
			w.WriteHeader(http.StatusNoContent)
		})
	})
	r.h = httpx.NewRouter(&config.Config{APIHost: apiHost}, slog.New(slog.DiscardHandler), r.s)
	// A test route is not in the scope table: closed to a token (the default -- below). Ask the
	// principal through a route a token MAY use instead: the business card, with a reader that
	// records it.
	rec := &recordingBusiness{}
	r2 := newRig(t, unknown, func(c *Config) { c.Business = rec })
	r2.grants.set(live)
	if w := r2.do(http.MethodGet, "/v1/business", "Bearer "+accessToken(t), "", nil); w.Code != http.StatusOK {
		t.Fatalf("a live token: %d %s", w.Code, w.Body.String())
	}
	if rec.tenant() != a.TenantID {
		t.Fatalf("the business was read for %v, want the grant's tenant %v", rec.tenant(), a.TenantID)
	}
	if w := r.do(http.MethodGet, "/v1/_who", "Bearer "+accessToken(t), "", nil); w.Code != http.StatusForbidden || problemOf(t, w).Code != CodeInsufficientScope {
		t.Fatalf("a token on a route outside the table: %d %s", w.Code, w.Body.String())
	}
	if seen.tenant != "" {
		t.Fatal("a route outside the scope table ran for an OAuth token")
	}
	if _, touches := r2.grants.counts(); touches != 1 {
		t.Fatalf("%d grant touches, want 1", touches)
	}
}

// TestOAuthChain_TheAddressBucketIsReservedKeptAndRefunded is ADR 0047 §8.7's exact bound (ORTA-A),
// T90's shape: from ONE address, 2 x limit CONCURRENT junk tokens reach the resolver EXACTLY limit
// times and the rest are 429 before it; junk keys and junk tokens share the one limiter (their sum
// is bounded); successful tokens are refunded -- many times the limit from one address pass, and are
// counted by the grant and app buckets instead.
//
// MUTANTS (measured, phase 3 report): Keep replaced by Refund on a refusal -> the shared limit is never
// reached sequentially and a window of refusals no longer costs the address (the concurrent subtest
// stays green: its requests hold their reservations while they are in flight); Refund dropped on an
// acceptance -> the 41st success from one address is 429.
func TestOAuthChain_TheAddressBucketIsReservedKeptAndRefunded(t *testing.T) {
	const limit = 40
	t.Run("concurrent junk is bounded exactly", func(t *testing.T) {
		r := newRig(t, unknown, func(c *Config) { c.AddressLimit = limit })
		release := make(chan struct{})
		r.grants.set(func() (oauth.Access, error) { <-release; return oauth.Access{}, oauth.ErrInvalidToken })
		var wg sync.WaitGroup
		codes := make(chan int, 2*limit)
		for range 2 * limit {
			wg.Add(1)
			go func() {
				defer wg.Done()
				codes <- r.do(http.MethodGet, "/v1/business", "Bearer "+accessToken(t), "198.51.100.70", nil).Code
			}()
		}
		// Every request either holds a reservation (blocked in the resolver) or was refused 429.
		deadline := time.Now().Add(10 * time.Second)
		for {
			res, _ := r.grants.counts()
			if res+len(codes) == 2*limit || time.Now().After(deadline) {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		close(release)
		wg.Wait()
		close(codes)
		var unauthorized, limited int
		for c := range codes {
			switch c {
			case http.StatusUnauthorized:
				unauthorized++
			case http.StatusTooManyRequests:
				limited++
			default:
				t.Fatalf("status %d", c)
			}
		}
		if res, _ := r.grants.counts(); res != limit || unauthorized != limit || limited != limit {
			t.Fatalf("%d concurrent junk tokens: %d lookups, %d 401, %d 429 -- want %d each", 2*limit, res, unauthorized, limited, limit)
		}
	})
	t.Run("keys and tokens share the limiter", func(t *testing.T) {
		r := newRig(t, unknown, func(c *Config) { c.AddressLimit = limit })
		for i := range limit {
			auth := "Bearer " + liveKey(t)
			if i%2 == 0 {
				auth = "Bearer " + accessToken(t)
			}
			r.do(http.MethodGet, "/v1/business", auth, "198.51.100.71", nil)
		}
		for _, auth := range []string{"Bearer " + liveKey(t), "Bearer " + accessToken(t)} {
			if w := r.do(http.MethodGet, "/v1/business", auth, "198.51.100.71", nil); w.Code != http.StatusTooManyRequests {
				t.Fatalf("past the shared limit: %d", w.Code)
			}
		}
		res, _ := r.grants.counts()
		if res+r.resolver.count() != limit {
			t.Fatalf("%d token + %d key lookups, want %d in all", res, r.resolver.count(), limit)
		}
	})
	t.Run("successes are refunded", func(t *testing.T) {
		r := newRig(t, unknown, func(c *Config) { c.AddressLimit = limit; c.Business = &recordingBusiness{} })
		r.grants.set(grantOf(oauth.ScopeTimesheetsRead))
		for i := range 5 * limit {
			if w := r.do(http.MethodGet, "/v1/business", "Bearer "+accessToken(t), "198.51.100.72", nil); w.Code != http.StatusOK {
				t.Fatalf("success %d from one address: %d %s", i+1, w.Code, w.Body.String())
			}
		}
		// The named cost (§8.7 (i)): a token that keeps failing spends the address's window.
		r.grants.set(func() (oauth.Access, error) { return oauth.Access{}, oauth.ErrInvalidToken })
		for range limit {
			r.do(http.MethodGet, "/v1/business", "Bearer "+accessToken(t), "198.51.100.72", nil)
		}
		r.grants.set(grantOf(oauth.ScopeTimesheetsRead))
		if w := r.do(http.MethodGet, "/v1/business", "Bearer "+accessToken(t), "198.51.100.72", nil); w.Code != http.StatusTooManyRequests {
			t.Fatalf("after a window of refusals a good token got %d, want 429 (the named cost)", w.Code)
		}
	})
}

// TestOAuthChain_TheGrantAndAppBuckets: a grant is 600/min whatever the address (shrunk to 3 here),
// an app 6000/min across its grants (shrunk to 5) -- 429 rate_limited past either, and the throttled
// request does not touch the grant.
func TestOAuthChain_TheGrantAndAppBuckets(t *testing.T) {
	r := newRig(t, unknown, func(c *Config) { c.GrantLimit, c.ClientLimit = 3, 5; c.Business = &recordingBusiness{} })
	app := grantOf(oauth.ScopeTimesheetsRead)
	a, _ := app()
	r.grants.set(app)
	var codes []int
	for i := range 4 {
		addr := fmt.Sprintf("198.51.100.%d", 80+i)
		codes = append(codes, r.do(http.MethodGet, "/v1/business", "Bearer "+accessToken(t), addr, nil).Code)
	}
	if !slices.Equal(codes, []int{200, 200, 200, 429}) {
		t.Fatalf("one grant from four addresses: %v", codes)
	}
	// Two more grants of the same app: the app's bucket (5) is reached on the app's sixth request.
	codes = nil
	for i := range 3 {
		b := a
		b.GrantID[0] ^= byte(i + 1)
		r.grants.set(func() (oauth.Access, error) { return b, nil })
		codes = append(codes, r.do(http.MethodGet, "/v1/business", "Bearer "+accessToken(t), "198.51.100.90", nil).Code)
	}
	// The fourth request above was refused by the GRANT bucket, before the app's: the app counted 3.
	if !slices.Equal(codes, []int{200, 200, 429}) {
		t.Fatalf("the app's other grants: %v (the app had 3 counted requests before them, its bucket is 5)", codes)
	}
	if _, touches := r.grants.counts(); touches != 5 {
		t.Fatalf("%d touches, want 5 -- a throttled request is not a use", touches)
	}
}

// TestScopes_EveryAuthenticatedRouteIsInTheTable walks the SHIPPED router (no test route, no
// extension): every authenticated /v1 route is in routeScopes, every row of routeScopes is a route,
// and the table is ADR 0047 §8.2's. A route added to Mount without a row turns this red.
func TestScopes_EveryAuthenticatedRouteIsInTheTable(t *testing.T) {
	r := newRig(t, unknown)
	r.s.testRoutes = nil
	mux := chi.NewRouter()
	r.s.Mount(mux)
	public := map[string]bool{"GET " + Prefix + openAPIPath: true, "GET " + Prefix + guidePath: true, "GET " + Prefix + guideTRPath: true}
	var routes []string
	if err := chi.Walk(mux, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.TrimSuffix(strings.ReplaceAll(route, "/"+wildcard+"/", "/"), "/"+wildcard)
		if route == Prefix || route == Prefix+"/" || public[method+" "+route] {
			return nil
		}
		routes = append(routes, method+" "+route)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(routes)
	var table []string
	for k := range routeScopes {
		table = append(table, k)
	}
	sort.Strings(table)
	if !slices.Equal(routes, table) {
		t.Fatalf("the authenticated routes %v, the scope table %v", routes, table)
	}
	want := map[string]string{
		"GET /v1/business": anyScope, "GET /v1/timesheets": "timesheets:read",
		"GET /v1/employees": "employees:read", "GET /v1/employees/{ref}": "employees:read", "GET /v1/directory": "employees:read",
		"PUT /v1/employees/{ref}": "employees:write", "DELETE /v1/employees/{ref}": "employees:write",
		"POST /v1/employees/{ref}/resend-activation": "employees:write",
		"PUT /v1/locations/{ref}":                    "locations:write", "GET /v1/punches": "punches:read",
	}
	for k, v := range want {
		if routeScopes[k] != v {
			t.Errorf("%s needs %q, ADR 0047 §8.2 says %q", k, routeScopes[k], v)
		}
	}
	if len(want) != len(routeScopes) {
		t.Fatalf("the table has %d rows, ADR 0047 §8.2 %d", len(routeScopes), len(want))
	}
	for _, s := range routeScopes {
		if s != anyScope && !oauth.Known(s) {
			t.Errorf("the table names %q, which is not in the closed scope set", s)
		}
	}
}

// TestScopes_EachRouteOpensToItsScopeAloneAndAKeyToAll is the table, row by row through HTTP (ADR
// 0047 §17: "tablonun her satırı (izinli 2xx, izinsiz 403) -- tablo bazlı"): a token with exactly the
// route's scope passes the gate (the rig's refusing domain answers past it -- never insufficient_scope
// and never 401); a token with every OTHER scope is 403 insufficient_scope, details.scope the route's,
// and RFC 6750 §3.1's challenge; an API key never gets insufficient_scope on any route.
//
// MUTANT (measured, phase 3 report): scopeGate's !p.HasScope(need) branch removed -> every "every
// other scope" row is answered past the gate and this is red.
func TestScopes_EachRouteOpensToItsScopeAloneAndAKeyToAll(t *testing.T) {
	type route struct{ method, path, body, pattern string }
	routes := []route{
		{"GET", "/v1/business", "", "GET /v1/business"},
		{"GET", "/v1/timesheets?from=2026-11-02&to=2026-11-08", "", "GET /v1/timesheets"},
		{"GET", "/v1/employees", "", "GET /v1/employees"},
		{"GET", "/v1/employees/E-1", "", "GET /v1/employees/{ref}"},
		{"PUT", "/v1/employees/E-1", `{"firstName":"J","lastName":"E","active":true}`, "PUT /v1/employees/{ref}"},
		{"DELETE", "/v1/employees/E-1", "", "DELETE /v1/employees/{ref}"},
		{"POST", "/v1/employees/E-1/resend-activation", "", "POST /v1/employees/{ref}/resend-activation"},
		{"GET", "/v1/directory", "", "GET /v1/directory"},
		{"PUT", "/v1/locations/L-1", `{"name":"V","active":true}`, "PUT /v1/locations/{ref}"},
		{"GET", "/v1/punches?scope=employer", "", "GET /v1/punches"},
	}
	if len(routes) != len(routeScopes) {
		t.Fatalf("%d rows here, %d in the table", len(routes), len(routeScopes))
	}
	for _, rt := range routes {
		need := routeScopes[rt.pattern]
		var others []string
		for _, s := range oauth.KnownScopes {
			if s != need {
				others = append(others, s)
			}
		}
		allowed := []string{need}
		if need == anyScope {
			allowed = []string{oauth.ScopePunchesRead}
		}
		t.Run(rt.pattern, func(t *testing.T) {
			r := newRig(t, known([16]byte{1}, [16]byte{2}))
			r.grants.set(grantOf(allowed...))
			w := r.do(rt.method, rt.path, "Bearer "+accessToken(t), "", strings.NewReader(rt.body))
			if w.Code == http.StatusForbidden || w.Code == http.StatusUnauthorized {
				t.Fatalf("a token with %v: %d %s -- the gate refused its own scope", allowed, w.Code, w.Body.String())
			}
			if need == anyScope {
				return
			}
			r.grants.set(grantOf(others...))
			w = r.do(rt.method, rt.path, "Bearer "+accessToken(t), "", strings.NewReader(rt.body))
			if w.Code != http.StatusForbidden {
				t.Fatalf("a token with every other scope: %d %s", w.Code, w.Body.String())
			}
			p := problemOf(t, w)
			if p.Code != CodeInsufficientScope || p.Details["scope"] != need ||
				w.Header().Get("WWW-Authenticate") != `Bearer realm="taptime", error="insufficient_scope", scope="`+need+`"` {
				t.Fatalf("the refusal: %v %+v", w.Header(), p)
			}
			k := r.do(rt.method, rt.path, "Bearer "+liveKey(t), "", strings.NewReader(rt.body))
			if strings.Contains(k.Body.String(), string(CodeInsufficientScope)) {
				t.Fatalf("an API key got insufficient_scope: %d %s", k.Code, k.Body.String())
			}
		})
	}
}

// TestOAuthChain_NothingOfTheTokenReachesALogOrAResponse is TestChain_NothingOfTheKeyReachesALogOrAResponse's
// twin for an access token (ADR 0047 §17): a live, a refused and a failing token, through the shipped
// router with its access log -- the token, its body and "Bearer <token>" are in no log line and no
// response.
func TestOAuthChain_NothingOfTheTokenReachesALogOrAResponse(t *testing.T) {
	r := newRig(t, unknown, func(c *Config) { c.Business = &recordingBusiness{} })
	logs := &bytes.Buffer{}
	r.h = httpx.NewRouter(&config.Config{APIHost: apiHost}, slog.New(slog.NewJSONHandler(&syncWriter{w: logs}, nil)), r.s)
	tok := accessToken(t)
	var responses bytes.Buffer
	keep := func(code int, h http.Header, body string) { fmt.Fprintf(&responses, "%d %v %s\n", code, h, body) }
	r.grants.set(grantOf(oauth.ScopeTimesheetsRead))
	w := r.do(http.MethodGet, "/v1/business", "Bearer "+tok, "", nil, "X-Request-Id", tok)
	keep(w.Code, w.Header(), w.Body.String())
	w = r.do(http.MethodGet, "/v1/punches?scope=employer", "Bearer "+tok, "", nil)
	keep(w.Code, w.Header(), w.Body.String())
	r.grants.set(func() (oauth.Access, error) { return oauth.Access{}, oauth.ErrInvalidToken })
	w = r.do(http.MethodGet, "/v1/business", "Bearer "+tok, "", nil)
	keep(w.Code, w.Header(), w.Body.String())
	r.grants.set(func() (oauth.Access, error) { return oauth.Access{}, fmt.Errorf("db: connection reset") })
	w = r.do(http.MethodGet, "/v1/business", "Bearer "+tok, "", nil)
	keep(w.Code, w.Header(), w.Body.String())
	all := logs.String() + r.logs.String()
	if !strings.Contains(all, "connection reset") || !strings.Contains(all, `"status":503`) {
		t.Fatalf("CONTROL: the requests or the failure are not in the logs: %s", all)
	}
	for _, c := range []string{tok, tok[len(oauth.Prefix(oauth.KindAccess)):], "Bearer " + tok} {
		if strings.Contains(all, c) || strings.Contains(responses.String(), c) {
			t.Fatalf("the token reached a log line or a response (%d chars)", len(c))
		}
	}
}

// TestScopes_AnUnlistedRouteIsClosedAndNamesNoScope pins the closed default's CONTRACT (ADR 0047's
// phase 3 note: "details boş, scope parametresi yok"): an authenticated route outside the scope table
// answers an OAuth token 403 insufficient_scope with the fixed sentence, details EMPTY and RFC 6750's
// challenge WITHOUT a scope parameter -- there is no scope to ask for. GET /v1/business (any scope)
// with a token of no scope at all is the same answer. The route does not run.
//
// MUTANTS (measured, phase 4 report): the !listed branch answering insufficientScope(need) (M13a --
// details {"scope": ""}, scope="" in the challenge) and the !listed branch dropped (the same, through
// the next case) -> each red here; TestOAuthChain_InvalidTokenAndAClosedAPI read the code alone and
// stayed green.
func TestScopes_AnUnlistedRouteIsClosedAndNamesNoScope(t *testing.T) {
	r := newRig(t, unknown, func(c *Config) { c.Business = &recordingBusiness{} })
	ran := false
	withTestRoutes(r.s, func(rt chi.Router) {
		rt.Get("/_unlisted", func(w http.ResponseWriter, _ *http.Request) { ran = true; w.WriteHeader(http.StatusNoContent) })
	})
	r.h = httpx.NewRouter(&config.Config{APIHost: apiHost}, slog.New(slog.DiscardHandler), r.s)
	for _, c := range []struct {
		name, path string
		scopes     []string
	}{
		{"a route outside the table", "/v1/_unlisted", []string{oauth.ScopeTimesheetsRead, oauth.ScopeEmployeesRead}},
		{"the any-scope route with no scope", "/v1/business", nil},
	} {
		r.grants.set(grantOf(c.scopes...))
		w := r.do(http.MethodGet, c.path, "Bearer "+accessToken(t), "", nil)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s: %d %s", c.name, w.Code, w.Body.String())
		}
		p := problemOf(t, w)
		if p.Code != CodeInsufficientScope || p.Message != "This route is not open to OAuth access tokens." || len(p.Details) != 0 {
			t.Fatalf("%s: the body %s", c.name, w.Body.String())
		}
		if got := w.Header().Get("WWW-Authenticate"); got != `Bearer realm="taptime", error="insufficient_scope"` {
			t.Fatalf("%s: the challenge %q names a scope or is not RFC 6750's", c.name, got)
		}
	}
	if ran {
		t.Fatal("a route outside the scope table ran for an OAuth token")
	}
}

// TestOAuthChain_AClosedAPIKeepsTheReservation pins step 4's ErrAPIClosed branch to KEEP (ADR 0047
// §8.1, §8.7): a token whose business the operator closed is a refused credential like an invalid
// one -- from one address, limit 403 api_disabled and then 429 before the resolver.
//
// MUTANT (measured, phase 4 report): Keep replaced by Refund in the ErrAPIClosed branch -> the
// (limit+1)th request is 403 again, red.
func TestOAuthChain_AClosedAPIKeepsTheReservation(t *testing.T) {
	const limit = 12
	r := newRig(t, unknown, func(c *Config) { c.AddressLimit = limit })
	r.grants.set(func() (oauth.Access, error) { return oauth.Access{}, oauth.ErrAPIClosed })
	for i := range limit {
		w := r.do(http.MethodGet, "/v1/business", "Bearer "+accessToken(t), "198.51.100.73", nil)
		if w.Code != http.StatusForbidden || problemOf(t, w).Code != CodeAPIDisabled {
			t.Fatalf("closed request %d: %d %s", i+1, w.Code, w.Body.String())
		}
	}
	w := r.do(http.MethodGet, "/v1/business", "Bearer "+accessToken(t), "198.51.100.73", nil)
	if w.Code != http.StatusTooManyRequests || problemOf(t, w).Code != CodeRateLimited {
		t.Fatalf("past %d refusals of a closed API from one address: %d %s, want 429 (the refusals are kept)", limit, w.Code, w.Body.String())
	}
	if res, _ := r.grants.counts(); res != limit {
		t.Fatalf("%d resolver calls, want %d -- the 429 is before the resolver", res, limit)
	}
}

// TestOAuthChain_AnAcceptedTokenOfNoGrantIsUnavailable: a resolver that accepts a token and names no
// grant is a fault of ours -- 503, never a principal (apikey.Principal.Full fails closed on such a
// principal; the chain refuses it first), the route does not run and no grant is touched.
//
// MUTANT (measured, phase 4 report): the chain's zero-grant check removed -> the business card is read
// for the token (200), red.
func TestOAuthChain_AnAcceptedTokenOfNoGrantIsUnavailable(t *testing.T) {
	rec := &recordingBusiness{}
	r := newRig(t, unknown, func(c *Config) { c.Business = rec })
	r.grants.set(func() (oauth.Access, error) {
		return oauth.Access{TenantID: [16]byte{7}, ClientID: [16]byte{8}, TokenID: [16]byte{9}, Scopes: []string{oauth.ScopeTimesheetsRead}}, nil
	})
	w := r.do(http.MethodGet, "/v1/business", "Bearer "+accessToken(t), "", nil)
	if w.Code != http.StatusServiceUnavailable || problemOf(t, w).Code != CodeUnavailable {
		t.Fatalf("a token of no grant: %d %s", w.Code, w.Body.String())
	}
	if rec.tenant() != [16]byte{} {
		t.Fatal("the route ran for a token of no grant")
	}
	if _, touches := r.grants.counts(); touches != 0 {
		t.Fatalf("%d grant touches, want 0", touches)
	}
}

// TestOpenAPI_TheOAuthSchemeIsTheScopeTable (issue #37 phase 4, ADR 0047 §8.2, §22): the document's
// `oauth` security scheme is an Authorization Code flow whose three URLs are Taptime's own host -- the
// API host's name without its "api." label, https -- at /oauth/authorize and /oauth/token, and whose
// scopes are the closed set (oauth.KnownScopes: no webhooks:manage); every authenticated operation's
// `security` is exactly "a key OR a token of the route's scope" -- the scope routeScopes gives it, []
// for the any-scope GET /v1/business -- and its x-error-codes name insufficient_scope exactly when
// the route has a scope; the three documents need no credential.
//
// MUTANTS (measured, phase 4 report): an operation's oauth scope edited in the document; a scope
// added to the scheme; the token URL put on the API host -- each red.
func TestOpenAPI_TheOAuthSchemeIsTheScopeTable(t *testing.T) {
	doc := openAPIDoc(t)
	scheme, ok := yat(doc, "components", "securitySchemes", "oauth")
	if !ok {
		t.Fatal("no oauth security scheme")
	}
	if v, _ := yat(scheme, "type"); v != "oauth2" {
		t.Fatalf("the oauth scheme's type is %v", v)
	}
	flows := mustMap(t, func() any { v, _ := yat(scheme, "flows"); return v }(), "oauth flows")
	if !slices.Equal(flows.keys, []string{"authorizationCode"}) {
		t.Fatalf("the oauth flows %v, want authorizationCode alone", flows.keys)
	}
	server, _ := yat(doc, "servers", "0", "url")
	apiHost, _ := strings.CutPrefix(fmt.Sprint(server), "https://")
	mainHost, isAPI := strings.CutPrefix(apiHost, "api.")
	if !isAPI || mainHost == "" {
		t.Fatalf("the server %v is not https://api.<Taptime's host>", server)
	}
	for k, suffix := range map[string]string{"authorizationUrl": "/oauth/authorize", "tokenUrl": "/oauth/token", "refreshUrl": "/oauth/token"} {
		if v, _ := yat(flows, "authorizationCode", k); v != "https://"+mainHost+suffix {
			t.Errorf("%s is %v, want https://%s%s (Taptime's own host -- ADR 0047 §3)", k, v, mainHost, suffix)
		}
	}
	scopes := mustMap(t, func() any { v, _ := yat(flows, "authorizationCode", "scopes"); return v }(), "oauth scopes")
	documented := slices.Sorted(slices.Values(scopes.keys))
	if known := slices.Sorted(slices.Values(oauth.KnownScopes)); !slices.Equal(documented, known) || slices.Contains(documented, "webhooks:manage") {
		t.Fatalf("the documented scopes %v, the closed set %v", documented, known)
	}
	public := map[string]bool{Prefix + openAPIPath: true, Prefix + guidePath: true, Prefix + guideTRPath: true}
	checked := 0
	for _, o := range operationsOf(t, doc) {
		if !strings.HasPrefix(o.where, "paths ") {
			continue
		}
		sec, has := o.op.get("security")
		list, _ := sec.([]any)
		if public[o.path] {
			if !has || len(list) != 0 {
				t.Errorf("%s: a document's security is %v, want []", o.where, sec)
			}
			continue
		}
		need, listed := RouteScope(strings.ToUpper(o.method), o.path)
		if !listed {
			t.Errorf("%s: an authenticated operation the scope table does not name", o.where)
			continue
		}
		want := `[{"bearer":[]},{"oauth":["` + need + `"]}]`
		if need == anyScope {
			want = `[{"bearer":[]},{"oauth":[]}]`
		}
		if got := canonical(sec); got != want {
			t.Errorf("%s: security %s, want %s", o.where, got, want)
		}
		xc, _ := o.op.get("x-error-codes")
		codes, _ := xc.([]any)
		if slices.Contains(codes, any(string(CodeInsufficientScope))) != (need != anyScope) {
			t.Errorf("%s: x-error-codes %v -- insufficient_scope must be there exactly when the route has a scope", o.where, codes)
		}
		checked++
	}
	if checked != len(routeScopes) {
		t.Fatalf("%d authenticated operations checked, the scope table has %d rows", checked, len(routeScopes))
	}
}
