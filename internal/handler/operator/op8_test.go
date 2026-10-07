package operator_test

// op8_test.go -- M10 OP-8's acceptance on the shipped router, with no database: the
// two-way host gate, the same-origin gate ahead of the store, the session gate's
// redirects, the cookie separation, the enrollment link's shape, the screens' chrome,
// links and contrast. The database's own decisions (replay, lock, session predicate,
// enrollment token) are op8_db_test.go's; the comparison count of each sign-in arm is
// internal/operatorauth's surface_external_test.go (that package's test build is the
// one that can count its comparer: export_test.go).

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// operatorRoutes are the routes OP-8 mounts, with the method each answers -- and OP-10's
// legal screen, OP-11's tenant screens, OP-13's plaque screen and OP-14's audit log. A path
// with {id} is a chi pattern; mountedPath reads a concrete path of it.
var operatorRoutes = []struct{ method, path string }{
	{http.MethodGet, "/operator"},
	{http.MethodGet, "/operator/"},
	{http.MethodGet, "/operator/login"},
	{http.MethodPost, "/operator/login"},
	{http.MethodGet, "/operator/login/totp"},
	{http.MethodPost, "/operator/login/totp"},
	{http.MethodGet, "/operator/enroll"},
	{http.MethodPost, "/operator/enroll"},
	{http.MethodPost, "/operator/logout"},
	{http.MethodGet, "/operator/legal"},
	{http.MethodPost, "/operator/legal"},
	{http.MethodGet, "/operator/tenants"},
	{http.MethodPost, "/operator/tenants"},
	{http.MethodGet, "/operator/tenants/{id}"},
	{http.MethodGet, "/operator/tenants/{id}/plaques"},
	{http.MethodGet, "/operator/audit"},
	{http.MethodPost, "/operator/audit"},
}

// tenantOverviewPath and tenantPlaquesPath are concrete paths of the two {id} routes: a
// tenant's overview and its plaques, the 36-character hyphenated id in lower case (the
// form tenantsView and the overview write).
var (
	tenantOverviewPath = regexp.MustCompile(`^/operator/tenants/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	tenantPlaquesPath  = regexp.MustCompile(`^/operator/tenants/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/plaques$`)
)

// mountedPath reports whether a page's link or action (its query aside) is a route of
// operatorRoutes: the path itself, or a tenant overview's or a tenant's plaques' path for
// the two {id} routes.
func mountedPath(v string) bool {
	p, _, _ := strings.Cut(v, "?")
	for _, r := range operatorRoutes {
		if r.path == p {
			return true
		}
	}
	return tenantOverviewPath.MatchString(p) || tenantPlaquesPath.MatchString(p)
}

// ingressHosts reads the hosts deploy/k8s/40-ingress.yaml routes to this Service -- the
// customer hosts today; a host added there is in this list without editing the test.
func ingressHosts(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", "k8s", "40-ingress.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var hosts []string
	for _, m := range regexp.MustCompile(`(?m)^\s*-\s*host:\s*(\S+)\s*$`).FindAllStringSubmatch(string(b), -1) {
		hosts = append(hosts, m[1])
	}
	if len(hosts) < 3 {
		t.Fatalf("PREMISE: read %d host(s) from the ingress (%v); it routes three today", len(hosts), hosts)
	}
	return hosts
}

// TestSurface_ThePrefixIsTheRoutersPrefix: the operator's Prefix and the customer half
// of the host gate's OperatorPrefix are one definition, and the routes the configured
// surface registers (walked with chi.Walk) are under it.
func TestSurface_ThePrefixIsTheRoutersPrefix(t *testing.T) {
	if operator.Prefix != httpx.OperatorPrefix {
		t.Fatalf("operator.Prefix %q != httpx.OperatorPrefix %q", operator.Prefix, httpx.OperatorPrefix)
	}
	g := newRig(t)
	r := chi.NewRouter()
	g.surface.Mount(r)
	n := 0
	err := chi.Walk(r, func(_ string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		n++
		if route != operator.Prefix && !strings.HasPrefix(route, operator.Prefix+"/") {
			return fmt.Errorf("the surface registered %q, outside %s", route, operator.Prefix)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < len(operatorRoutes)-1 {
		t.Fatalf("walked %d route(s); the surface mounts more", n)
	}
}

// TestHostGate_OperatorRoutesAnswerTheRoutersOwn404OnEveryOtherHost is the OPERATOR
// half of the two-way host gate (ADR 0020 §4): on the hosts the ingress routes here
// (read from the manifest), on the development host, on no host, on 127.0.0.1 and on
// three look-alikes, each of the operator paths below (a tenant's plaques among them since
// OP-13, the audit log since OP-14) under each of the seven methods below answers the SAME bytes as a path no
// feature registered -- status, body,
// the three headers net/http's NotFound sets, no CSP, no Location, no cookie -- and
// reaches no store method. (A method chi does not know is the root router's 405, before
// this gate: TestEscapes_CookieNamesDuplicatesExpiryAndOddMethods.) CONTROL: on the
// operator host the same routes answer the operator surface.
func TestHostGate_OperatorRoutesAnswerTheRoutersOwn404OnEveryOtherHost(t *testing.T) {
	g := newRig(t)
	hosts := append(ingressHosts(t), "localhost:8080", "", "127.0.0.1", "ops.taptime.mt.evil", "evil.ops.taptime.mt",
		"ops-taptime.mt")
	methods := []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete,
		http.MethodOptions, http.MethodPatch}
	paths := []string{"/operator", "/operator/", "/operator/login", "/operator/login/totp", "/operator/enroll?id=x",
		"/operator/logout", "/operator/tenants", "/operator/tenants/x", "/operator/tenants/" + uuidString(t),
		"/operator/tenants/" + uuidString(t) + "/plaques", "/operator/legal", "/operator/audit", "/operator/no-such"}
	for _, host := range hosts {
		want := g.do(req{method: http.MethodGet, host: host, path: "/no-such-route"})
		if want.Code != http.StatusNotFound {
			t.Fatalf("PREMISE: an unregistered path on %q = %d", host, want.Code)
		}
		for _, m := range methods {
			for _, p := range paths {
				w := g.do(req{method: m, host: host, path: p, origin: opOrigin, form: url.Values{"email": {"a@b"}},
					cookies: []*http.Cookie{{Name: operatorauth.SessionCookieName, Value: strings.Repeat("A", 43)}}})
				if w.Code != http.StatusNotFound || w.Body.String() != want.Body.String() ||
					w.Result().Header.Get("Content-Type") != want.Result().Header.Get("Content-Type") ||
					w.Result().Header.Get("X-Content-Type-Options") != want.Result().Header.Get("X-Content-Type-Options") ||
					w.Result().Header.Get("Content-Security-Policy") != "" || w.Result().Header.Get("Location") != "" ||
					len(w.Result().Cookies()) != 0 {
					t.Errorf("%s %s on host %q = %d %q %v, want the router's own 404", m, p, host, w.Code, w.Body.String(), w.Result().Header)
				}
			}
		}
	}
	if n := g.store.total(); n != 0 {
		t.Fatalf("off-host requests reached the store %d time(s): %v", n, g.store.calls)
	}
	// CONTROL: the operator host serves the surface.
	if w := g.get("/operator/login"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "TAPTIME") {
		t.Fatalf("CONTROL: GET /operator/login on the operator host = %d", w.Code)
	}
}

// TestHostGate_TheOperatorHostServesNoCustomerRoute is the CUSTOMER half, with the
// real surface mounted beside a customer stand-in: on the operator host each customer
// route in the table (four) is the router's 404, the root goes to the console, and the
// operator's own pages and their static assets load (httpx's
// TestOperatorHostOnly_EveryEscapeAttemptLandsOnOneSide drives the gate's other rows).
// CONTROL: the customer host serves the customer.
func TestHostGate_TheOperatorHostServesNoCustomerRoute(t *testing.T) {
	g := newRig(t)
	for _, c := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/admin", 404}, {http.MethodGet, "/t", 404}, {http.MethodPost, "/api/checkin", 404},
		{http.MethodGet, httpx.HealthPath, 404}, {http.MethodGet, "/", 303},
		{http.MethodGet, "/operator/login", 200}, {http.MethodGet, "/static/css/app.css", 200},
		{http.MethodGet, "/static/js/operator/enroll.js", 200},
		{http.MethodGet, "/static/fonts/space-grotesk-v22-latin-wght.woff2", 200},
	} {
		w := g.do(req{method: c.method, host: opHost, path: c.path, origin: opOrigin})
		if w.Code != c.want {
			t.Errorf("%s %s on the operator host = %d, want %d", c.method, c.path, w.Code, c.want)
		}
		if c.want == 404 && strings.Contains(w.Body.String(), "customer") {
			t.Errorf("%s %s on the operator host served the customer stand-in", c.method, c.path)
		}
	}
	for _, p := range []string{"/admin", "/t", "/"} {
		if w := g.do(req{method: http.MethodGet, host: custHost, path: p}); w.Code != 200 || w.Body.String() != "customer" {
			t.Errorf("CONTROL: GET %s on the customer host = %d %q", p, w.Code, w.Body.String())
		}
	}
}

// crossOrigins are POSTs that did not come from the operator origin, each with the
// header a browser (or a forger) would send.
var crossOrigins = []struct {
	name   string
	origin string
	site   string // Sec-Fetch-Site, "" = absent
}{
	{"the customer host", "https://taptime.mt", "same-site"},
	{"a sibling host", "https://www.taptime.mt", "same-site"},
	{"another site", "https://evil.example", "cross-site"},
	{"the operator host over http", "http://ops.taptime.mt", "same-site"},
	{"the operator host on another port", "https://ops.taptime.mt:8443", "same-site"},
	{"an opaque origin", "null", ""},
	{"an opaque origin, same-site metadata", "null", "same-site"},
	{"no Origin, same-site metadata", "", "same-site"},
	{"no Origin, cross-site metadata", "", "cross-site"},
	{"no Origin and no metadata", "", ""},
	{"no Origin, metadata 'none'", "", "none"},
}

// TestSameOriginGate_ACrossOriginPostReachesNoStore is the OP-8 acceptance "cross-origin
// POST'ta çözümleyici çağrısı 0": each of the four POST routes the surface mounts,
// under each of the cross-origin shapes in crossOrigins, answers 403 with ZERO calls to
// the store (the definers and the account lookup), no cookie and no Location. CONTROL:
// the same request from the operator origin -- or with no Origin and same-origin fetch
// metadata -- reaches the store.
//
// (The bcrypt half -- zero comparisons, zero digests -- is counted in
// internal/operatorauth's TestSurface_ACrossOriginPostPaysNothing, the one place that can
// count the comparer.)
func TestSameOriginGate_ACrossOriginPostReachesNoStore(t *testing.T) {
	g := newRig(t)
	f := g.active()
	sess := g.signIn(f)
	ch := cookie(g.post("/operator/login", url.Values{"email": {f.email}, "password": {f.password}}), operatorauth.ChallengeCookieName)
	posts := []struct {
		path    string
		form    url.Values
		cookies []*http.Cookie
	}{
		{"/operator/login", url.Values{"email": {f.email}, "password": {f.password}}, nil},
		{"/operator/login/totp", url.Values{"code": {totpAt(f.key, g.now)}}, []*http.Cookie{ch}},
		{"/operator/enroll", url.Values{"id": {f.id.String()}, "token": {strings.Repeat("A", 43)}, "password": {"x"}, "password_again": {"x"}}, nil},
		{"/operator/logout", url.Values{}, []*http.Cookie{sess}},
	}
	before := g.store.total()
	for _, p := range posts {
		for _, o := range crossOrigins {
			hdr := map[string]string{}
			if o.site != "" {
				hdr["Sec-Fetch-Site"] = o.site
			}
			w := g.do(req{method: http.MethodPost, host: opHost, path: p.path, form: p.form, origin: o.origin, header: hdr, cookies: p.cookies})
			if w.Code != http.StatusForbidden || len(w.Result().Cookies()) != 0 || w.Result().Header.Get("Location") != "" {
				t.Errorf("POST %s from %s = %d, cookies %d, Location %q; want 403 and nothing set", p.path, o.name, w.Code,
					len(w.Result().Cookies()), w.Result().Header.Get("Location"))
			}
		}
	}
	if n := g.store.total() - before; n != 0 {
		t.Fatalf("cross-origin POSTs reached the store %d time(s): %v", n, g.store.calls)
	}
	// CONTROL: from the operator origin, with no Origin but same-origin metadata, and --
	// the shape a browser actually sends here, the pages' referrer policy being
	// no-referrer -- `Origin: null` with same-origin metadata, the same POSTs reach the
	// store.
	for _, p := range posts[:2] {
		for _, hdr := range []req{{origin: opOrigin}, {header: map[string]string{"Sec-Fetch-Site": "same-origin"}},
			{origin: "null", header: map[string]string{"Sec-Fetch-Site": "same-origin"}}} {
			n := g.store.total()
			g.do(req{method: http.MethodPost, host: opHost, path: p.path, form: p.form, origin: hdr.origin, header: hdr.header, cookies: p.cookies})
			if g.store.total() == n {
				t.Fatalf("CONTROL: POST %s from the operator origin reached no store method; the counter is blind", p.path)
			}
		}
	}
	// The trailing-dot spelling of the operator host is the operator host for the host
	// gate but a DIFFERENT origin for a browser: its exact Origin is refused, and a POST
	// carrying `Origin: null` + same-origin metadata is admitted (a page under that
	// spelling is served by the operator surface). (Case is not a separate origin: a
	// browser serialises the host in lower case, and an Origin in capitals matches
	// case-insensitively.)
	for _, host := range []string{"ops.taptime.mt."} {
		form := url.Values{"email": {f.email}, "password": {"wrong"}}
		if w := g.do(req{method: http.MethodPost, host: host, path: "/operator/login", form: form, origin: "https://" + host}); w.Code != http.StatusForbidden {
			t.Errorf("POST on %s with its own Origin = %d, want 403 (not the operator origin)", host, w.Code)
		}
		w := g.do(req{method: http.MethodPost, host: host, path: "/operator/login", form: form, origin: "null",
			header: map[string]string{"Sec-Fetch-Site": "same-origin"}})
		if w.Code != http.StatusUnauthorized {
			t.Errorf("POST on %s with Origin null + same-origin metadata = %d, want 401 (admitted, refused sign-in)", host, w.Code)
		}
	}
}

// TestSessionGate_NoLiveSessionIsASignInRedirect: the console with no cookie, an empty
// one, a value of the wrong shape, a well-formed value no session has, a session the
// store refuses, and a store that fails -- each of the six is 303 to the sign-in. No cookie
// and an empty one cost NO store call (requireOperator); a refused session clears the
// cookie; a failing store keeps it (the session may be alive) and logs an ERROR.
// CONTROL: a live session renders the console.
func TestSessionGate_NoLiveSessionIsASignInRedirect(t *testing.T) {
	g := newRig(t)
	f := g.active()
	live := g.signIn(f)
	if w := g.get("/operator", live); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "You are signed in") {
		t.Fatalf("CONTROL: the console with a live session = %d", w.Code)
	}
	for _, c := range []struct {
		name      string
		cookies   []*http.Cookie
		storeHits int
		cleared   bool
	}{
		{"no cookie", nil, 0, false},
		{"an empty cookie", []*http.Cookie{{Name: operatorauth.SessionCookieName, Value: ""}}, 0, false},
		{"a value of the wrong shape", []*http.Cookie{{Name: operatorauth.SessionCookieName, Value: "junk"}}, 0, true},
		{"a well-formed value nobody issued", []*http.Cookie{{Name: operatorauth.SessionCookieName, Value: strings.Repeat("Q", 43)}}, 1, true},
	} {
		n := g.store.count("TouchOperatorSession")
		w := g.get("/operator", c.cookies...)
		if w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != "/operator/login" {
			t.Errorf("%s: %d %q, want 303 to the sign-in", c.name, w.Code, w.Result().Header.Get("Location"))
		}
		if got := g.store.count("TouchOperatorSession") - n; got != c.storeHits {
			t.Errorf("%s: %d session check(s), want %d", c.name, got, c.storeHits)
		}
		if cl := cookie(w, operatorauth.SessionCookieName); (cl != nil && cl.MaxAge < 0) != c.cleared {
			t.Errorf("%s: cookie cleared = %v, want %v", c.name, cl != nil, c.cleared)
		}
	}
	// A failing store: same redirect, cookie kept, an ERROR line.
	g.store.mu.Lock()
	g.store.fail["TouchOperatorSession"] = errFakeDB
	g.store.mu.Unlock()
	g.logs.Reset()
	w := g.get("/operator", live)
	if w.Code != http.StatusSeeOther || cookie(w, operatorauth.SessionCookieName) != nil || !strings.Contains(g.logs.String(), "could not check the operator session") {
		t.Errorf("a failing session check = %d, cookie %v, log %q", w.Code, cookie(w, operatorauth.SessionCookieName), g.logs.String())
	}
}

// TestSessionGate_ASameSiteReadDoesNotTouchTheSession: a GET of the console that a
// browser labels same-site or cross-site (an <img> on taptime.mt, a link from another
// site) is the sign-in redirect with NO session check -- so, in a browser that sends
// fetch metadata, a page on a sibling host that embeds the URL does not make the session
// predicate run (a browser that sends none is the card's counted limit L5). CONTROL:
// same-origin and no-metadata reads run the check.
func TestSessionGate_ASameSiteReadDoesNotTouchTheSession(t *testing.T) {
	g := newRig(t)
	live := g.signIn(g.active())
	for _, site := range []string{"same-site", "cross-site", "Same-Site"} {
		n := g.store.count("TouchOperatorSession")
		w := g.do(req{method: http.MethodGet, host: opHost, path: "/operator", cookies: []*http.Cookie{live},
			header: map[string]string{"Sec-Fetch-Site": site}})
		if w.Code != http.StatusSeeOther || g.store.count("TouchOperatorSession") != n {
			t.Errorf("a %s read = %d with %d session check(s); want 303 and none", site, w.Code, g.store.count("TouchOperatorSession")-n)
		}
	}
	for _, hdr := range []map[string]string{{"Sec-Fetch-Site": "same-origin"}, {"Sec-Fetch-Site": "none"}, nil} {
		n := g.store.count("TouchOperatorSession")
		w := g.do(req{method: http.MethodGet, host: opHost, path: "/operator", cookies: []*http.Cookie{live}, header: hdr})
		if w.Code != http.StatusOK || g.store.count("TouchOperatorSession") != n+1 {
			t.Errorf("CONTROL: a read with %v = %d, %d check(s)", hdr, w.Code, g.store.count("TouchOperatorSession")-n)
		}
	}
}

// TestCrossCookie_CustomerValuesUnderTheOperatorNamesAreRefused is the first direction
// of the OP-8 acceptance "çapraz çerez": a value of the customer cookies' shape -- the
// panel's and the employee's tokens are both 43 characters of base64url over 32 random
// bytes, the operator token's shape exactly, so the shape gate passes and the decision is
// the session predicate's -- under the operator session cookie's name is the sign-in
// redirect; under the challenge cookie's name it is the password step again. The same
// values under their own customer names leave the console's answer the sign-in redirect
// (the operator cookie is absent). (Against the real
// database's op_touch_session, and the reverse direction against the real customer
// resolvers: op8_db_test.go, TestE2E_CrossCookie_NeitherSideAcceptsTheOthersValue.)
func TestCrossCookie_CustomerValuesUnderTheOperatorNamesAreRefused(t *testing.T) {
	g := newRig(t)
	f := g.active()
	value := func() string { return base64.RawURLEncoding.EncodeToString(randBytes(t, 32)) }
	for _, name := range []string{adminauth.CookieName, session.CookieName} {
		v := value()
		w := g.get("/operator", &http.Cookie{Name: operatorauth.SessionCookieName, Value: v})
		if w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != "/operator/login" {
			t.Errorf("a %s-shaped value as the operator session = %d %q", name, w.Code, w.Result().Header.Get("Location"))
		}
		w = g.post("/operator/login/totp", url.Values{"code": {totpAt(f.key, g.now)}}, &http.Cookie{Name: operatorauth.ChallengeCookieName, Value: v})
		if w.Code != http.StatusUnauthorized || cookie(w, operatorauth.SessionCookieName) != nil || !strings.Contains(w.Body.String(), "That sign-in step expired") {
			t.Errorf("a %s-shaped value as the challenge = %d, session cookie %v", name, w.Code, cookie(w, operatorauth.SessionCookieName) != nil)
		}
		// Under its own customer name: the console answers the sign-in redirect.
		w = g.get("/operator", &http.Cookie{Name: name, Value: v})
		if w.Code != http.StatusSeeOther {
			t.Errorf("the %s cookie on the operator surface = %d", name, w.Code)
		}
	}
	if g.store.liveSessions() != 0 {
		t.Fatalf("a customer-shaped value opened %d operator session(s)", g.store.liveSessions())
	}
}

// TestEnroll_TheTokenNeverTravelsInTheURL pins the enrollment link's shape (ADR 0020 §6):
// the page is served for an account id in the query, a token put in the query is NOT
// read (the page does not carry it, the form's action has no query), the Location of
// the three redirected requests this test sends (listed at the loop below) carries
// neither the query's token nor a query, and the page's script is the one file the
// policy names. The 48-class header test holds Location against the hostile Referer and
// the other request headers its header lists by name.
func TestEnroll_TheTokenNeverTravelsInTheURL(t *testing.T) {
	g := newRig(t)
	f := g.active()
	tok := base64.RawURLEncoding.EncodeToString(randBytes(t, 32))
	w := g.get("/operator/enroll?id=" + f.id.String() + "&token=" + tok)
	if w.Code != http.StatusOK {
		t.Fatalf("GET the enrollment page = %d", w.Code)
	}
	page := w.Body.String()
	if strings.Contains(page, tok) {
		t.Error("a token sent in the query string was put into the page")
	}
	if !strings.Contains(page, `action="/operator/enroll"`) || strings.Contains(page, `action="/operator/enroll?`) {
		t.Error("the enrollment form's action is not the bare path")
	}
	if !strings.Contains(page, `<script src="/static/js/operator/enroll.js" defer></script>`) {
		t.Error("the enrollment page does not load its script")
	}
	if !strings.Contains(page, `value="`+f.id.String()+`"`) {
		t.Error("the account id is not in the form")
	}
	// The secret is shown twice: grouped base32 and inside the otpauth URI.
	if !regexp.MustCompile(`otpauth://totp/Taptime%20operator:ops\.taptime\.mt\?secret=[A-Z2-7]{32}&amp;issuer=Taptime%20operator`).MatchString(page) {
		t.Error("the otpauth URI is not on the page in its documented shape")
	}
	// On these three redirected requests Location carries no request value.
	for _, path := range []string{"/operator/login?next=" + tok, "/operator/login/totp?x=" + tok, "/operator?y=" + tok} {
		if loc := g.get(path).Result().Header.Get("Location"); strings.Contains(loc, tok) || strings.Contains(loc, "?") {
			t.Errorf("GET %s answered Location %q", path, loc)
		}
	}
}

// TestEnrollScript_TouchesTheFragmentAndNothingElse is a TEXT check of the shipped
// script's code (comments stripped): it names the fragment, the token's shape, the field,
// replaceState and the hidden attribute, and it names no request, storage, cookie, eval,
// HTML sink or navigation. It does not run the script -- what the code DOES is the
// reviewed body's (TestEnrollScript_IsTheReviewedBody), measured by hand in a browser.
func TestEnrollScript_TouchesTheFragmentAndNothingElse(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "js", "operator", "enroll.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := regexp.MustCompile(`(?m)^\s*//.*$`).ReplaceAllString(string(b), "")
	for _, must := range []string{"location.hash", "replaceState", "{43}", "getElementById('enroll-token')", "'hidden'"} {
		if !strings.Contains(src, must) {
			t.Errorf("the script's code does not contain %q", must)
		}
	}
	for _, never := range []string{"fetch", "XMLHttpRequest", "sendBeacon", "localStorage", "sessionStorage", "indexedDB",
		"document.cookie", "eval", "Function(", "innerHTML", "location.href =", "location.assign", "postMessage", "password", "code"} {
		if strings.Contains(src, never) {
			t.Errorf("the script's code contains %q", never)
		}
	}
}

// TestLogout_IsNotRefusedByTheBudgetAThirdPartyCanSpend: with the flood budget of an
// address spent (by another client behind it), the operator's sign-out from that address still
// revokes the session; and sign-out without a cookie costs no store call.
func TestLogout_IsNotRefusedByTheBudgetAThirdPartyCanSpend(t *testing.T) {
	g := newRig(t)
	sess := g.signIn(g.active())
	const shared = "203.0.113.9:1"
	for i := 0; i < 301; i++ {
		g.do(req{method: http.MethodGet, host: opHost, path: "/operator/login", remote: shared})
	}
	if w := g.do(req{method: http.MethodGet, host: opHost, path: "/operator/login", remote: shared}); w.Code != http.StatusTooManyRequests {
		t.Fatalf("PREMISE: the flood budget of %s is not spent (%d)", shared, w.Code)
	}
	n := g.store.count("CloseOperatorSession")
	w := g.do(req{method: http.MethodPost, host: opHost, path: "/operator/logout", origin: opOrigin, remote: shared, cookies: []*http.Cookie{sess}})
	if w.Code != http.StatusSeeOther || g.store.count("CloseOperatorSession") != n+1 || g.store.liveSessions() != 0 {
		t.Fatalf("sign-out with the address's flood budget spent = %d, closes %d, live %d", w.Code, g.store.count("CloseOperatorSession")-n, g.store.liveSessions())
	}
	if c := cookie(w, operatorauth.SessionCookieName); c == nil || c.MaxAge >= 0 {
		t.Error("sign-out did not clear the session cookie")
	}
	before := g.store.total()
	if w := g.post("/operator/logout", nil); w.Code != http.StatusSeeOther || g.store.total() != before {
		t.Errorf("a sign-out with no cookie = %d with %d store call(s)", w.Code, g.store.total()-before)
	}
	// A failing close keeps the cookie and says so.
	sess = g.signIn(g.active())
	g.store.mu.Lock()
	g.store.fail["CloseOperatorSession"] = errFakeDB
	g.store.mu.Unlock()
	w = g.post("/operator/logout", nil, sess)
	if w.Code != http.StatusServiceUnavailable || cookie(w, operatorauth.SessionCookieName) != nil || !strings.Contains(w.Body.String(), "still open") {
		t.Errorf("a failing sign-out = %d, cookie %v", w.Code, cookie(w, operatorauth.SessionCookieName))
	}
}

// screens renders the eleven exported screen constructors of operatorpages
// (operatorScreens, pinned against the package's API) in the 30 variants below (and the
// tenant chrome with a name).
func screens(t *testing.T) map[string]string {
	t.Helper()
	name, err := operatorpages.NewTenantName("Kebab Factory Ltd")
	if err != nil {
		t.Fatal(err)
	}
	unnamed, err := operatorpages.UnnamedTenant("10000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	row := operatorpages.TenantRow{ID: "10000000-0000-4000-8000-000000000001",
		Path: "/operator/tenants/10000000-0000-4000-8000-000000000001", Name: "Kebab Factory Ltd.",
		CreatedAt: "2026-07-05 09:30 UTC", Plan: "founding"}
	overview := operatorpages.TenantOverviewView{Name: name, ID: row.ID, CreatedAt: row.CreatedAt, Plan: "founding",
		BusinessType: "restaurant", Locations: "9", ActiveEmployees: "41", ActivePlaques: "9", ActiveAdmins: "2",
		PlaquesPath: row.Path + "/plaques"}
	unnamedOverview := overview
	unnamedOverview.Name = unnamed
	// The plaque screen (OP-13): one row of each tone and the facts each branch prints.
	plaqueRows := []operatorpages.PlaqueRow{
		{UID: "04A1B2C3D4E5F6", Label: "On a wall", Sentence: "S", Tone: operatorpages.PlaqueToneInService,
			Location: "Hamrun", LocationID: "20000000-0000-4000-8000-000000000009", EncodedAt: "2026-07-05 09:30 UTC",
			CreatedAt: "2026-07-01 09:30 UTC", LastCtr: "3"},
		{UID: "04A1B2C3D4E5F7", Label: "In stock", Sentence: "S", Tone: operatorpages.PlaqueToneStock,
			CreatedAt: "2026-07-01 09:30 UTC", LastCtr: "0"},
		{UID: "04A1B2C3D4E5F8", Label: "On a wall, never encoded", Sentence: "S", Tone: operatorpages.PlaqueToneAttention,
			LocationID: "20000000-0000-4000-8000-000000000009", CreatedAt: "2026-07-01 09:30 UTC", LastCtr: "500"},
		{UID: "04A1B2C3D4E5F9", Label: "Retired", Sentence: "S", Tone: operatorpages.PlaqueToneOut, Location: "Hamrun",
			LocationID: "20000000-0000-4000-8000-000000000009", CreatedAt: "2026-07-01 09:30 UTC",
			RetiredAt: "2026-08-01 09:30 UTC", ReplacedBy: "04A1B2C3D4E5F6", LastCtr: "9"},
		{UID: "04A1B2C3D4E5FA", Label: "Unrecognised", Sentence: "S", StoredStatus: `"quarantined"`,
			CreatedAt: "2026-07-01 09:30 UTC", LastCtr: "0"},
	}
	plaques := operatorpages.TenantPlaquesView{Name: name, ID: row.ID, OverviewPath: row.Path, Total: "5", Shown: "5",
		Noun: "plaques", Rows: plaqueRows}
	plaquesFirst := plaques
	plaquesFirst.Total, plaquesFirst.Truncated = "205", true
	plaquesNone := operatorpages.TenantPlaquesView{Name: name, ID: row.ID, OverviewPath: row.Path, Total: "0", Shown: "0", Noun: "plaques"}
	plaquesUnnamed := plaques
	plaquesUnnamed.Name = unnamed
	// The audit log (OP-14): one row of each shape a fact can take -- a named and an
	// unnamed tenant (each linked to its overview), a tenant id no tenant has, a named and an
	// unnamed account, a named and an unnamed actor, a row before sign-in, every kind of
	// value the log reads out, an unrecognised kind, scope and slug, a hidden detail.
	everyOption := []operatorpages.AuditKindOption{{Value: "", Label: "Every kind", Selected: true},
		{Value: "login", Label: "Signed in"}, {Value: "read", Label: "Read"}}
	auditRows := []operatorpages.AuditRow{
		{At: "2026-10-07 09:30:15 UTC", Kind: operatorpages.AuditWord{Text: "Read"}, ActorID: "30000000-0000-4000-8000-000000000001",
			Actor: "Ops One", TenantID: row.ID, TenantPath: row.Path, Tenant: "Kebab Factory Ltd.",
			Scope: operatorpages.AuditWord{Text: "a tenant's plaques"}, Session: "1a2b3c4d"},
		{At: "2026-10-07 09:30:14 UTC", Kind: operatorpages.AuditWord{Text: "Read"}, ActorID: "30000000-0000-4000-8000-000000000001",
			TenantID: row.ID, TenantPath: row.Path, Scope: operatorpages.AuditWord{Text: "the tenant list"}, Page: "2", PageSize: "50",
			Search: operatorpages.AuditWord{Text: "Searched by name"}, Session: "1a2b3c4d"},
		{At: "2026-10-07 09:30:13 UTC", Kind: operatorpages.AuditWord{Text: "Read"}, ActorID: "30000000-0000-4000-8000-000000000001",
			Actor: "Ops One", TenantID: "40000000-0000-4000-8000-000000000009", Scope: operatorpages.AuditWord{Text: "a tenant's overview"},
			Session: "1a2b3c4d"},
		{At: "2026-10-07 09:30:12 UTC", Kind: operatorpages.AuditWord{Text: "Read"}, ActorID: "30000000-0000-4000-8000-000000000001",
			Actor: "Ops One", Scope: operatorpages.AuditWord{Text: "this audit log"}, Page: "1", PageSize: "50",
			Filter: operatorpages.AuditWord{Text: "Every kind"}, Session: "1a2b3c4d"},
		{At: "2026-10-07 09:30:11 UTC", Kind: operatorpages.AuditWord{Text: "Legal text published"},
			ActorID: "30000000-0000-4000-8000-000000000001", Actor: "Ops One", Legal: operatorpages.AuditWord{Text: "/legal/privacy"},
			LegalBytes: "1234", Session: "1a2b3c4d"},
		{At: "2026-10-07 09:30:10 UTC", Kind: operatorpages.AuditWord{Text: "Sign-in refused: wrong password"},
			AccountID: "30000000-0000-4000-8000-000000000002", Account: "Ops Two"},
		{At: "2026-10-07 09:30:09 UTC", Kind: operatorpages.AuditWord{Text: "Code refused"}, AccountID: "30000000-0000-4000-8000-000000000003"},
		{At: "2026-10-07 09:30:08 UTC", Kind: operatorpages.AuditWord{Text: "zz_later_kind", Unknown: true},
			ActorID: "30000000-0000-4000-8000-000000000001", Actor: "Ops One", Scope: operatorpages.AuditWord{Text: "billing_runs", Unknown: true},
			Legal: operatorpages.AuditWord{Unknown: true}, DetailHidden: true, Session: "1a2b3c4d"},
		{At: "2026-10-07 09:30:07 UTC", Kind: operatorpages.AuditWord{Text: "Read"}, ActorID: "30000000-0000-4000-8000-000000000001",
			Actor: "Ops One", ScopeHidden: true, DetailHidden: true, Session: "1a2b3c4d"},
	}
	audit := operatorpages.AuditLogView{Filter: "Every kind", Page: 1, Options: everyOption, Rows: auditRows, HasNext: true,
		Reach: "50,000"}
	auditFiltered := operatorpages.AuditLogView{Kind: "login", Filter: "Signed in", Page: 2, Options: everyOption,
		Rows: auditRows[:1], HasNext: true, Reach: "50,000"}
	auditEmpty := operatorpages.AuditLogView{Kind: "login", Filter: "Signed in", Page: 3, Options: everyOption, Reach: "50,000"}
	auditLast := operatorpages.AuditLogView{Filter: "Every kind", Page: 1000, Options: everyOption, Rows: auditRows, LastPage: true,
		Reach: "50,000"}
	out := map[string]string{}
	for k, c := range map[string]templ.Component{
		"sign-in":            operatorpages.SignIn(operatorpages.SignInView{}),
		"sign-in, refused":   operatorpages.SignIn(operatorpages.SignInView{Failed: true}),
		"sign-in, expired":   operatorpages.SignIn(operatorpages.SignInView{Expired: true}),
		"code":               operatorpages.Code(operatorpages.CodeView{}),
		"code, rejected":     operatorpages.Code(operatorpages.CodeView{Rejected: true}),
		"code, locked":       operatorpages.Code(operatorpages.CodeView{Locked: true}),
		"enroll":             operatorpages.Enroll(operatorpages.EnrollView{AccountID: "x", Key: "ABCD EFGH", URI: "otpauth://totp/x", Blob: "b"}),
		"enroll, re-render":  operatorpages.Enroll(operatorpages.EnrollView{AccountID: "x", Blob: "b", Token: "t", Mismatch: true, WeakPassword: true, CodeRejected: true}),
		"home":               operatorpages.Home(),
		"problem":            operatorpages.Problem(operatorpages.ProblemView{Title: "T", Message: "M", Back: "/operator/login", BackLabel: "B"}),
		"problem, signed in": operatorpages.Problem(operatorpages.ProblemView{Title: "T", Message: "M", SignedIn: true}),
		"tenant screen":      operatorpages.TenantScreen("Plaques", name),
		"legal":              operatorpages.Legal(operatorpages.LegalView{Limit: 100}),
		"legal, published": operatorpages.Legal(operatorpages.LegalView{Limit: 1,
			Docs: []operatorpages.LegalDoc{{Slug: "privacy", Path: "/legal/privacy", Published: true, PublishedAt: "2026-10-03 09:30 UTC", Body: "T"},
				{Slug: "terms", Path: "/legal/terms"}},
			Versions: []operatorpages.LegalVersionRow{{Path: "/legal/privacy", PublishedAt: "2026-10-03 09:30 UTC", Bytes: "1 bytes",
				Publisher: "tenant admin (legacy)", Current: true}}}),
		"tenants":                  operatorpages.Tenants(operatorpages.TenantsView{Page: 1}),
		"tenants, a page":          operatorpages.Tenants(operatorpages.TenantsView{Page: 1, Rows: []operatorpages.TenantRow{row, {ID: row.ID, Path: row.Path, CreatedAt: row.CreatedAt, Plan: "standard"}}, HasNext: true}),
		"tenants, searched":        operatorpages.Tenants(operatorpages.TenantsView{Search: "kebab", Page: 1, Rows: []operatorpages.TenantRow{row}}),
		"tenants, a later page":    operatorpages.Tenants(operatorpages.TenantsView{Search: "kebab", Page: 2, Rows: []operatorpages.TenantRow{row}, HasNext: true}),
		"tenants, past the end":    operatorpages.Tenants(operatorpages.TenantsView{Page: 3}),
		"tenant overview":          operatorpages.TenantOverview(overview),
		"tenant overview, unnamed": operatorpages.TenantOverview(unnamedOverview),
		"tenant plaques":           operatorpages.TenantPlaques(plaques),
		"tenant plaques, first":    operatorpages.TenantPlaques(plaquesFirst),
		"tenant plaques, none":     operatorpages.TenantPlaques(plaquesNone),
		"tenant plaques, unnamed":  operatorpages.TenantPlaques(plaquesUnnamed),
		"audit log":                operatorpages.AuditLog(audit),
		"audit log, filtered":      operatorpages.AuditLog(auditFiltered),
		"audit log, past the end":  operatorpages.AuditLog(auditEmpty),
		"audit log, the last page": operatorpages.AuditLog(auditLast),
		"audit log, empty":         operatorpages.AuditLog(operatorpages.AuditLogView{Filter: "Every kind", Page: 1, Options: everyOption}),
	} {
		var b bytes.Buffer
		if err := c.Render(context.Background(), &b); err != nil {
			t.Fatalf("render %s: %v", k, err)
		}
		out[k] = b.String()
	}
	return out
}

// TestOperatorScreens_EveryOneWearsTheOperatorChrome: each of the 30 renders screens()
// makes (eleven exported screen constructors; the "Every" of the name is these 30) opens
// with the operator bar -- the "TAPTIME OPERATOR" lockup on the ink band -- and none of
// the 30 carries the restaurant panel's chrome (its tab bar, its green wordmark). The
// sign-out control is on the twenty-one signed-in renders and absent from the other nine.
func TestOperatorScreens_EveryOneWearsTheOperatorChrome(t *testing.T) {
	signedIn := map[string]bool{"home": true, "problem, signed in": true, "tenant screen": true, "legal": true, "legal, published": true,
		"tenants": true, "tenants, a page": true, "tenants, searched": true, "tenants, a later page": true, "tenants, past the end": true,
		"tenant overview": true, "tenant overview, unnamed": true, "tenant plaques": true, "tenant plaques, first": true,
		"tenant plaques, none": true, "tenant plaques, unnamed": true, "audit log": true, "audit log, filtered": true,
		"audit log, past the end": true, "audit log, the last page": true, "audit log, empty": true}
	all := screens(t)
	if len(all) != 30 || len(signedIn) != 21 {
		t.Fatalf("PREMISE: %d render(s), %d signed in; the comment says 30 and 21", len(all), len(signedIn))
	}
	for name, html := range all {
		bar := strings.Index(html, `<header class="op-bar">`)
		main := strings.Index(html, "<main")
		if bar < 0 || main < 0 || bar > main {
			t.Errorf("%s: the operator bar does not come before the page", name)
		}
		if !regexp.MustCompile(`TAPTIME <span class="text-saffron">OPERATOR</span>`).MatchString(html) {
			t.Errorf("%s: no TAPTIME OPERATOR lockup", name)
		}
		for _, panel := range []string{"tab-bar", "tab-link", "punchless", "text-tappa-green\">taptime"} {
			if strings.Contains(html, panel) {
				t.Errorf("%s: carries the restaurant panel's %q", name, panel)
			}
		}
		if got := strings.Contains(html, `action="/operator/logout"`); got != signedIn[name] {
			t.Errorf("%s: sign-out control present = %v, want %v", name, got, signedIn[name])
		}
		if !strings.Contains(html, "Taptime operator</title>") {
			t.Errorf("%s: the document title does not name the operator surface", name)
		}
	}
}

// TestTenantScreen_RefusesToRenderWithoutAName pins the tenant slot (ADR 0020 §9): a
// tenant screen is not rendered without the tenant's name -- NewTenantName refuses the
// three blank names below, TenantScreen refuses the zero value and writes 0 bytes, and
// the surface's render turns that refusal into a plain 500 with no page. CONTROL: a named
// tenant's screen carries the name, escaped, in the banner and in the document title.
func TestTenantScreen_RefusesToRenderWithoutAName(t *testing.T) {
	for _, blank := range []string{"", " ", "\t\n"} {
		if _, err := operatorpages.NewTenantName(blank); err == nil {
			t.Errorf("NewTenantName(%q) was accepted", blank)
		}
	}
	var b bytes.Buffer
	err := operatorpages.TenantScreen("Plaques", operatorpages.TenantName{}).Render(context.Background(), &b)
	if err == nil || b.Len() != 0 {
		t.Fatalf("the zero TenantName rendered (err %v, %d bytes)", err, b.Len())
	}
	name, err := operatorpages.NewTenantName(`Rusty <Bar> & "Grill"`)
	if err != nil {
		t.Fatal(err)
	}
	b.Reset()
	if err := operatorpages.TenantScreen("Plaques", name).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	html := b.String()
	esc := "Rusty &lt;Bar&gt; &amp; &#34;Grill&#34;"
	banner := strings.Index(html, `<section class="op-tenant" aria-label="Tenant">`)
	if banner < 0 || !strings.Contains(html[banner:], esc) || !strings.Contains(html, "<title>Plaques — "+esc+" — Taptime operator</title>") {
		t.Errorf("a named tenant's screen does not carry the escaped name in the banner and the title:\n%s", html)
	}
	if strings.Contains(html, "<Bar>") {
		t.Error("the tenant's name was written unescaped")
	}
	// Through the surface's render path: the refusal is a 500 with no page.
	g := newRig(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	operator.RenderForTest(g.surface, w, r, operatorpages.TenantScreen("Plaques", operatorpages.TenantName{}))
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "<html") {
		t.Errorf("a refused tenant screen through render = %d %q", w.Code, w.Body.String())
	}
}

// TestOperatorScreens_EveryActionAndLinkIsAMountedRoute: on the 30 renders screens()
// makes (the "Every" of the name is these 30), each form action and link is a relative
// path of a route the surface mounts (operatorRoutes; a tenant overview's and a tenant's
// plaques' paths are their {id} routes') -- so none of the 30 links to the billing screen
// of a later task -- and the count of absolute URLs in their action, href and src
// attributes is zero. CONTROL: the console links the tenant list and the audit log
// (OP-14), a list row links its overview, the overview links the tenant's plaques (OP-13),
// the plaques link back to the overview, an audit row links the overview of the tenant it
// names and the audit log's filter and pager post to it.
func TestOperatorScreens_EveryActionAndLinkIsAMountedRoute(t *testing.T) {
	attr := regexp.MustCompile(`\s(action|href|src)="([^"]*)"`)
	absolute := 0
	all := screens(t)
	if !strings.Contains(all["home"], `href="/operator/tenants"`) ||
		!strings.Contains(all["tenants, a page"], `href="/operator/tenants/10000000-0000-4000-8000-000000000001"`) ||
		!strings.Contains(all["tenant overview"], `href="/operator/tenants/10000000-0000-4000-8000-000000000001/plaques"`) ||
		!strings.Contains(all["tenant plaques"], `href="/operator/tenants/10000000-0000-4000-8000-000000000001"`) {
		t.Fatal("CONTROL: the console does not link the tenants, a list row its overview, the overview its plaques or the plaques the overview")
	}
	if !strings.Contains(all["home"], `<a href="/operator/audit" class="op-link">Audit log</a>`) ||
		!strings.Contains(all["audit log"], `href="/operator/tenants/10000000-0000-4000-8000-000000000001"`) ||
		strings.Count(all["audit log, filtered"], `<form method="post" action="/operator/audit"`) != 3 {
		t.Fatal("CONTROL: the console does not link the audit log, an audit row does not link its tenant, or the filter and the pager do not post to the log")
	}
	for name, html := range all {
		for _, m := range attr.FindAllStringSubmatch(html, -1) {
			v := m[2]
			if !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "//") || strings.Contains(v, "://") {
				absolute++
				t.Errorf("%s: %s=%q is not a same-origin path", name, m[1], v)
				continue
			}
			if strings.HasPrefix(v, "/static/") {
				continue
			}
			if !mountedPath(v) {
				t.Errorf("%s: %s=%q is not a route the operator surface mounts", name, m[1], v)
			}
		}
	}
	if absolute != 0 {
		t.Fatalf("%d absolute URL(s) on the operator screens", absolute)
	}
}

// relLum is WCAG 2.1's relative luminance of an sRGB colour.
func relLum(c [3]float64) float64 {
	lin := func(v float64) float64 {
		v /= 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c[0]) + 0.7152*lin(c[1]) + 0.0722*lin(c[2])
}

func contrast(a, b [3]float64) float64 {
	la, lb := relLum(a), relLum(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// TestOperatorChrome_TheColouredTextClearsAA recomputes the two coloured texts the
// operator bar draws on ink -- paper (the lockup, the sign-out control) and saffron (the
// OPERATOR half of the lockup, the control on hover) -- from tailwind.config.js's
// palette, against AA's 4.5:1 for normal text (the lockup is 14px bold, NOT large text).
// CONTROL: black on white is 21:1.
func TestOperatorChrome_TheColouredTextClearsAA(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "tailwind.config.js"))
	if err != nil {
		t.Fatal(err)
	}
	hex := func(token string) [3]float64 {
		m := regexp.MustCompile(`'?` + regexp.QuoteMeta(token) + `'?:\s*'#([0-9A-Fa-f]{6})'`).FindSubmatch(b)
		if m == nil {
			t.Fatalf("tailwind.config.js has no %s", token)
		}
		var c [3]float64
		for i := 0; i < 3; i++ {
			v, err := strconv.ParseUint(string(m[1][2*i:2*i+2]), 16, 8)
			if err != nil {
				t.Fatal(err)
			}
			c[i] = float64(v)
		}
		return c
	}
	if got := contrast([3]float64{0, 0, 0}, [3]float64{255, 255, 255}); got < 20.9 || got > 21.1 {
		t.Fatalf("CONTROL: black on white = %.2f:1", got)
	}
	ink := hex("ink")
	for _, tok := range []string{"paper", "saffron"} {
		if got := contrast(hex(tok), ink); got < 4.5 {
			t.Errorf("%s on ink = %.2f:1, want at least 4.5:1", tok, got)
		} else {
			t.Logf("%s on ink = %.2f:1", tok, got)
		}
	}
}

// TestFormValue_PrintsNoValue measures the redacting holder of form credentials
// (form.go) on the paths a mistaken print takes: the fmt verbs this test names, both
// slog formats, and encoding/json via MarshalText. CONTROL: the plain value is found by
// the same search.
func TestFormValue_PrintsNoValue(t *testing.T) {
	const plain = "FAKEformValueFAKE"
	v := operator.FormValueForTest(plain)
	var out bytes.Buffer
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%p", "%T"} {
		fmt.Fprintf(&out, verb+"\n", v)
		fmt.Fprintf(&out, verb+"\n", struct{ F any }{v})
		fmt.Fprintf(&out, verb+"\n", &v)
	}
	slog.New(slog.NewTextHandler(&out, nil)).Info("x", "v", v)
	slog.New(slog.NewJSONHandler(&out, nil)).Info("x", "v", v)
	if strings.Contains(out.String(), plain) {
		t.Fatalf("a formValue printed its value:\n%s", out.String())
	}
	var control bytes.Buffer
	control.WriteString(plain)
	if !strings.Contains(control.String(), plain) {
		t.Fatal("CONTROL FAILED")
	}
}

// TestHostGate_AnOperatorHostThatIsACustomerHostServesOnlyTheOperator measures what
// config.Load does NOT refuse (it compares the operator host with TAPPA_BASE_URL's): an operator host
// set to another host the ingress serves for customers (www.taptime.mt). In-process the
// operator side wins on that host -- the operator surface answers there and the customer
// route driven (/admin) is the router's 404; the cost is that host's customer pages. The
// canonical customer host keeps /admin. (At the ingress today both other customer hosts
// are permanent redirects to taptime.mt -- read from deploy/k8s/40-ingress.yaml, not
// measured against the cluster.)
func TestHostGate_AnOperatorHostThatIsACustomerHostServesOnlyTheOperator(t *testing.T) {
	const collide = "www.taptime.mt"
	if !slices.Contains(ingressHosts(t), collide) {
		t.Fatalf("PREMISE: %s is not an ingress host any more; pick one that is", collide)
	}
	g := newRig(t)
	s, err := operator.New(g.auth, g.store, g.store, g.store, g.store, g.texts, collide, opBase, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	h := httpx.NewRouter(&config.Config{OperatorHost: collide, BaseURL: opBase}, nil, customer{}, s)
	serveOn := func(host, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "http://"+host+path, nil)
		r.Host = host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := serveOn(collide, "/admin"); w.Code != http.StatusNotFound {
		t.Errorf("GET /admin on the colliding host = %d, want 404", w.Code)
	}
	if w := serveOn(collide, "/operator/login"); w.Code != http.StatusOK {
		t.Errorf("GET /operator/login on the colliding host = %d, want the operator sign-in", w.Code)
	}
	if w := serveOn(custHost, "/admin"); w.Code != http.StatusOK || w.Body.String() != "customer" {
		t.Errorf("CONTROL: GET /admin on the canonical host = %d", w.Code)
	}
	if w := serveOn(custHost, "/operator/login"); w.Code != http.StatusNotFound {
		t.Errorf("GET /operator/login on the canonical host = %d, want 404", w.Code)
	}
}

// TestHostGate_TheUnavailableSurfaceKeepsItsAnswerOnEveryHost: OP-7's unavailable state
// is unchanged by OP-8 on the paths it answers -- /operator is its designed 503 on the
// operator host AND on the customer host (the operator half of the gate is the
// configured surface's) -- while the customer half, which needs the configured host,
// answers the customer route driven on the operator host with 404.
func TestHostGate_TheUnavailableSurfaceKeepsItsAnswerOnEveryHost(t *testing.T) {
	h := httpx.NewRouter(&config.Config{OperatorHost: opHost, BaseURL: opBase}, nil, customer{}, operator.Unavailable())
	for _, c := range []struct {
		host, path string
		want       int
	}{
		{opHost, "/operator", 503}, {opHost, "/operator/login", 503}, {custHost, "/operator", 503},
		{custHost, "/operator/login", 503}, {opHost, "/admin", 404}, {custHost, "/admin", 200},
	} {
		r := httptest.NewRequest(http.MethodGet, "http://"+c.host+c.path, nil)
		r.Host = c.host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("GET %s on %s with the surface unavailable = %d, want %d", c.path, c.host, w.Code, c.want)
		}
		if c.want == 503 && !strings.Contains(w.Body.String(), "unavailable:") {
			t.Errorf("GET %s on %s: not the unavailable body", c.path, c.host)
		}
	}
}

// TestSurface_TheScreensOfLaterTasksAreNotMounted: ADR 0020 §4's billing screen is OP-12's
// B phase (its definer, op_read_tenant_billing, came with 00032 in the A phase); the
// surface does not register it, so it
// answers the router's OWN 404 on the operator host -- the bytes of a path no feature
// registered, not a page of the surface (a mounted route's 404, a tenant's "no such id"
// page, is HTML) -- and no screen links to it
// (TestOperatorScreens_EveryActionAndLinkIsAMountedRoute). (OP-10 took /operator/legal off
// this list, OP-11 the tenant list and overview, OP-13 the tenant's plaques under
// /operator/tenants/{id}/plaques and OP-14 /operator/audit: they are mounted, with the
// definers 00027, 00029, 00030 and 00031 added. ADR 0020 §4's earlier spelling
// /operator/plaques stays unmounted and stays here; so do the audit log's paths below it --
// it has one page, posted to, and no per-tenant view (the orchestrator's K14-5: no tenant
// filter in v1) -- each the router's own 404.)
func TestSurface_TheScreensOfLaterTasksAreNotMounted(t *testing.T) {
	g := newRig(t)
	live := g.signIn(g.active())
	want := g.get("/operator/no-such-screen", live)
	if want.Code != http.StatusNotFound || strings.Contains(want.Body.String(), "<html") {
		t.Fatalf("PREMISE: an unregistered path = %d %q, want the router's plain 404", want.Code, want.Body.String())
	}
	for _, p := range []string{"/operator/billing", "/operator/plaques", "/operator/audit/", "/operator/audit/x",
		"/operator/tenants/" + uuidString(t) + "/billing", "/operator/tenants/" + uuidString(t) + "/audit"} {
		if w := g.get(p, live); w.Code != http.StatusNotFound || w.Body.String() != want.Body.String() {
			t.Errorf("GET %s = %d %q, want the router's own 404", p, w.Code, w.Body.String())
		}
	}
}

func uuidString(t *testing.T) string {
	t.Helper()
	b := randBytes(t, 16)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// TestSessionGate_ABudgetPerSession: the session gate's own budget (100 requests per
// session per window -- OP-11 had re-derived it to 200 while a read charged it twice;
// OP-13 phase B moved the read's second unit to the read budget and re-derived it by
// requests) refuses the 101st console request of ONE session with 429, and another
// operator session afterwards still renders (CONTROL). The requests come from distinct
// addresses so the flood budget is not what refuses.
func TestSessionGate_ABudgetPerSession(t *testing.T) {
	g := newRig(t)
	a, b := g.signIn(g.active()), g.signIn(g.active())
	var w *httptest.ResponseRecorder
	for i := 0; i < 101; i++ {
		w = g.do(req{method: http.MethodGet, host: opHost, path: "/operator", cookies: []*http.Cookie{a},
			remote: fmt.Sprintf("198.18.%d.%d:1", i/200, i%200+1), header: map[string]string{"Sec-Fetch-Site": "same-origin"}})
		if i < 100 && w.Code != http.StatusOK {
			t.Fatalf("request %d of one session = %d, want 200", i+1, w.Code)
		}
	}
	if w.Code != http.StatusTooManyRequests || !strings.Contains(g.logs.String(), "operator session budget reached") {
		t.Fatalf("the 101st request of one session = %d, want 429 and the budget's WARN line", w.Code)
	}
	// The budget is charged AFTER the predicate: all 101 requests ran op_touch_session,
	// the refused one included (surface.go's sessionLimit comment).
	if n := g.store.count("TouchOperatorSession"); n != 101 {
		t.Errorf("%d session predicate call(s) for 101 requests, want 101 -- the budget does not bound the predicate", n)
	}
	if w := g.get("/operator", b); w.Code != http.StatusOK {
		t.Fatalf("CONTROL: another session = %d, want 200", w.Code)
	}
}

// TestSignIn_TheCodeStepClearsTheChallenge: the challenge is a bearer value for further
// code attempts (operatorauth's challenge is not single-use), so the response that
// issues the session also expires the challenge cookie.
func TestSignIn_TheCodeStepClearsTheChallenge(t *testing.T) {
	g := newRig(t)
	f := g.active()
	w := g.post("/operator/login", url.Values{"email": {f.email}, "password": {f.password}})
	ch := cookie(w, operatorauth.ChallengeCookieName)
	if ch == nil || ch.MaxAge <= 0 || !ch.Secure || !ch.HttpOnly || ch.SameSite != http.SameSiteStrictMode || ch.Path != "/" || ch.Domain != "" {
		t.Fatalf("the challenge cookie as set: %+v", ch != nil)
	}
	w = g.post("/operator/login/totp", url.Values{"code": {totpAt(f.key, g.now)}}, ch)
	sc, cleared := cookie(w, operatorauth.SessionCookieName), cookie(w, operatorauth.ChallengeCookieName)
	if sc == nil || cleared == nil || cleared.MaxAge >= 0 || cleared.Value != "" {
		t.Fatalf("the code step: session %v, challenge cleared %v", sc != nil, cleared != nil && cleared.MaxAge < 0)
	}
}

// TestSignIn_ReadsTheBodyNeverTheQuery: credentials put in the query string (where the
// ingress logs them) are not credentials here -- the right address and password in the
// query, an empty body, is a refused sign-in. CONTROL: the same values in the body sign in.
func TestSignIn_ReadsTheBodyNeverTheQuery(t *testing.T) {
	g := newRig(t)
	f := g.active()
	q := "/operator/login?email=" + url.QueryEscape(f.email) + "&password=" + url.QueryEscape(f.password)
	if w := g.post(q, url.Values{}); w.Code != http.StatusUnauthorized || cookie(w, operatorauth.ChallengeCookieName) != nil {
		t.Errorf("credentials in the query string = %d, challenge %v; want a refusal", w.Code, cookie(w, operatorauth.ChallengeCookieName) != nil)
	}
	if w := g.post("/operator/login", url.Values{"email": {f.email}, "password": {f.password}}); w.Code != http.StatusSeeOther {
		t.Fatalf("CONTROL: the same credentials in the body = %d", w.Code)
	}
}

// TestEscapes_CookieNamesDuplicatesExpiryAndOddMethods drives four escape shapes the
// gates above do not name:
//
//   - a live session token under the cookie name in another case (`__host-…`, which a
//     sibling host could set with a Domain attribute where a browser applies the prefix
//     rule case-sensitively) is not read: net/http matches cookie names exactly;
//   - two session cookies in one header: net/http hands the FIRST to the gate -- junk
//     first is the sign-in redirect, live first renders (that the junk-first response
//     also clears the cookie is sessionGate's code, read, and the 5th-round security
//     audit's measurement; this test checks the status only);
//   - a challenge past its five minutes is the sign-in again, and the challenge cookie is
//     cleared (the code is not checked: no store call for the account);
//   - a method chi does not know answers the router's 405 on a customer host for an
//     operator path and for an unknown path alike -- no difference to learn from.
func TestEscapes_CookieNamesDuplicatesExpiryAndOddMethods(t *testing.T) {
	g := newRig(t)
	f := g.active()
	live := g.signIn(f)
	if w := g.get("/operator", &http.Cookie{Name: strings.ToLower(operatorauth.SessionCookieName), Value: live.Value}); w.Code != http.StatusSeeOther {
		t.Errorf("a live token under the lower-case name = %d, want 303 (not read)", w.Code)
	}
	junk := &http.Cookie{Name: operatorauth.SessionCookieName, Value: strings.Repeat("J", 43)}
	if w := g.get("/operator", junk, live); w.Code != http.StatusSeeOther {
		t.Errorf("junk then live = %d, want 303 (the first is read)", w.Code)
	}
	if w := g.get("/operator", live, junk); w.Code != http.StatusOK {
		t.Errorf("live then junk = %d, want 200 (the first is read)", w.Code)
	}
	ch := cookie(g.post("/operator/login", url.Values{"email": {f.email}, "password": {f.password}}), operatorauth.ChallengeCookieName)
	g.now = g.now.Add(6 * time.Minute)
	n := g.store.count("OperatorByID")
	w := g.post("/operator/login/totp", url.Values{"code": {totpAt(f.key, g.now)}}, ch)
	if c := cookie(w, operatorauth.ChallengeCookieName); w.Code != http.StatusUnauthorized || c == nil || c.MaxAge >= 0 ||
		!strings.Contains(w.Body.String(), "That sign-in step expired") || g.store.count("OperatorByID") != n {
		t.Errorf("an expired challenge = %d, cleared %v, account lookups %d", w.Code, c != nil && c.MaxAge < 0, g.store.count("OperatorByID")-n)
	}
	a := g.do(req{method: "FOO", host: custHost, path: "/operator/login"})
	b := g.do(req{method: "FOO", host: custHost, path: "/no-such-route"})
	if a.Code != b.Code || a.Body.String() != b.Body.String() {
		t.Errorf("an unknown method: operator path %d %q, unknown path %d %q", a.Code, a.Body.String(), b.Code, b.Body.String())
	}
}
