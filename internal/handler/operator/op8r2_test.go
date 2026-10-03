package operator_test

// op8r2_test.go -- OP-8's second round: the 40-class header table (driven with hostile
// request headers since the 4th round), the script/policy pairing, the problem pages'
// links, the enrollment script's reviewed body and the sign-out flood measurements. (The
// 2nd round's two syntax pins were replaced by go/types pins in the 3rd round:
// typepins_test.go.)

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

const (
	policyBase   = "default-src 'none'; style-src 'self'; font-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"
	policyEnroll = policyBase + "; script-src " + opOrigin + "/static/js/operator/enroll.js"
)

// addrs hands out a fresh client address per call, so no class's budget is spent by
// another's (the throttle classes reuse one address on purpose).
type addrs struct{ n int }

func (a *addrs) next() string {
	a.n++
	return fmt.Sprintf("198.19.%d.%d", a.n/250, a.n%250+1)
}

// TestOperatorHeaders_FortyResponseClassesCarryThePolicy drives the 40 response classes
// C1-C40 below, each with its status and its last request held to classRoutes
// (op8r5_test.go; a class that stops being reached is red).
//
// WHAT IS READ: the response headers AT WriteHeader -- the recorder's Result().Header,
// the snapshot it takes when the status line is written; a header set or deleted after
// that does not change it (a trailer goes to Result().Trailer, not read here).
// TestOperatorHeaders_TheRecorderSnapshotIsWhatTheWireCarries measures it equal to the
// wire's header section on three classes (C1, C18, C28), Content-Length and Date aside.
//
// PART I -- measured. Each class is sent with the 15 hostile request headers of
// hostileHeaders (Location, Content-Security-Policy, Cache-Control, Referrer-Policy,
// X-Content-Type-Options, Set-Cookie, X-Frame-Options, Access-Control-Allow-Origin, Refresh,
// Referer, User-Agent, Accept-Language, X-Requested-With, HX-Current-URL, HX-Target)
// and a second Cookie line naming the operator's two cookies (hostileCookieLine); the 28
// classes whose last request goes to /operator/login, /operator/login/totp or
// /operator/enroll (C1-C26, C38, C40 -- hostileDrive adds the query on those paths, C8's
// hand-built request gets it from hostileOn) also carry hostileQuery (a token, an address,
// a password, a code, a blob, and an id on a POST). On each of the 40, at WriteHeader:
//   - the response's header NAMES are exactly the designed set (designedHeaders);
//   - Content-Security-Policy is one value: enrollCSP on the four classes whose body loads
//     a script (C18, C20, C21, C22 -- checked by name), operatorCSP on the other 36;
//     Cache-Control is "no-store", X-Content-Type-Options "nosniff", Referrer-Policy
//     "no-referrer", each one value; Location and Content-Type equal the class's designed value or are absent;
//     Allow is C40's designed pair;
//   - each Set-Cookie is one of the operator's two cookies, set or cleared as designed,
//     with Path=/, Secure, HttpOnly, SameSite=Strict and no Domain;
//   - no header value and no body byte carries a hostile request value (hostileValues),
//     raw or query-escaped.
//
// CONTROL: a handler that copies the request's headers into its response and writes the
// query into its body fails the check on the hostile headers and the query.
//
// PART II -- the list above, on these 40 classes.
//
// PART III -- This test measures the 40 classes and the raw and query-escaped forms only;
// anything else (examples: a branch a later handler adds; those a request cannot reach,
// below; a hostile value echoed HTML-escaped or base32-encoded) is code review's -- no
// completeness claim.
//
// NOT REACHABLE BY A REQUEST, BY NAME (so not in the table): a cookie setter refusing an
// empty value, BeginEnrollment's randomness failure, the guards of home, logout and
// sessionGate that fire when mounted without the link in front, and a failing templ render
// (TestTenantScreen_RefusesToRenderWithoutAName drives that one through render). The
// off-host 404 is the router's own (TestHostGate_OperatorRoutesAnswerTheRoutersOwn404OnEveryOtherHost).
func TestOperatorHeaders_FortyResponseClassesCarryThePolicy(t *testing.T) {
	g := newRig(t)
	ad := &addrs{}
	f, lockedF := g.active(), g.active()
	until := g.now.Add(10 * time.Minute)
	g.store.mu.Lock()
	g.store.locked[lockedF.id] = true
	la := g.store.byID[lockedF.id]
	la.LockedUntil = &until
	g.store.byID[lockedF.id] = la
	g.store.accounts[strings.ToLower(lockedF.email)] = la
	g.store.mu.Unlock()
	last := &driveLog{}
	send := func(r req) *httptest.ResponseRecorder {
		if r.remote == "" {
			r.remote = ad.next() + ":1"
		}
		if r.host == "" {
			r.host = opHost
		}
		*last = driveLog{r.method, r.path}
		return g.do(hostileDrive(r))
	}
	post := func(path string, form url.Values, cookies ...*http.Cookie) req {
		return req{method: http.MethodPost, path: path, form: form, origin: opOrigin, cookies: cookies}
	}
	get := func(path string, cookies ...*http.Cookie) req {
		return req{method: http.MethodGet, path: path, cookies: cookies, header: map[string]string{"Sec-Fetch-Site": "same-origin"}}
	}
	challenge := func(fx fixture) *http.Cookie {
		t.Helper()
		c := cookie(send(post("/operator/login", url.Values{"email": {fx.email}, "password": {fx.password}})), operatorauth.ChallengeCookieName)
		if c == nil {
			t.Fatal("PREMISE: no challenge")
		}
		return c
	}
	signIn := func() *http.Cookie {
		t.Helper()
		c := cookie(send(post("/operator/login/totp", url.Values{"code": {totpAt(f.key, g.now)}}, challenge(f))), operatorauth.SessionCookieName)
		if c == nil {
			t.Fatal("PREMISE: no session")
		}
		return c
	}
	// The enrollment page, for the enrollment classes.
	pending := fixture{id: uuid.New()}
	page := send(get("/operator/enroll?id=" + pending.id.String()))
	km := regexp.MustCompile(`<p class="op-key">([A-Z2-7 ]+)</p>`).FindStringSubmatch(page.Body.String())
	bm := regexp.MustCompile(`name="blob" value="([^"]+)"`).FindStringSubmatch(page.Body.String())
	if km == nil || bm == nil {
		t.Fatal("PREMISE: the enrollment page carries no key or blob")
	}
	pageKey, err := base32.StdEncoding.DecodeString(strings.ReplaceAll(km[1], " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	blob := html.UnescapeString(bm[1])
	linkToken := base64.RawURLEncoding.EncodeToString(randBytes(t, 32))
	g.store.mu.Lock()
	g.store.tokens[pending.id] = linkToken
	g.store.mu.Unlock()
	enroll := func(token, p1, p2, code string) url.Values {
		return url.Values{"id": {pending.id.String()}, "token": {token}, "blob": {blob}, "password": {p1}, "password_again": {p2}, "code": {code}}
	}
	const pw = "op8 headers passphrase"
	failing := func(method string, r req) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder {
			g.store.mu.Lock()
			g.store.fail[method] = errFakeDB
			g.store.mu.Unlock()
			defer func() { g.store.mu.Lock(); delete(g.store.fail, method); g.store.mu.Unlock() }()
			return send(r)
		}
	}
	repeat := func(n int, r req) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder {
			r.remote = ad.next() + ":1"
			var w *httptest.ResponseRecorder
			for i := 0; i < n; i++ {
				w = send(r)
			}
			return w
		}
	}
	once := func(r req) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder { return send(r) }
	}

	classes := []headerClass{
		{"C1 sign-in page", 200, once(get("/operator/login"))},
		{"C2 sign-in refused", 401, once(post("/operator/login", url.Values{"email": {f.email}, "password": {"wrong"}}))},
		{"C3 sign-in accepted", 303, once(post("/operator/login", url.Values{"email": {f.email}, "password": {f.password}}))},
		{"C4 sign-in throttled (work budget)", 429, repeat(21, post("/operator/login", url.Values{"email": {f.email}, "password": {"wrong"}}))},
		{"C5 sign-in fault", 503, failing("OperatorByEmail", post("/operator/login", url.Values{"email": {f.email}, "password": {"x"}}))},
		{"C6 sign-in cross-origin", 403, once(req{method: http.MethodPost, path: "/operator/login", form: url.Values{}, origin: "https://taptime.mt"})},
		{"C7 sign-in oversized form", 413, once(post("/operator/login", url.Values{"email": {strings.Repeat("a", 20<<10)}}))},
		{"C8 sign-in unreadable form", 400, func() *httptest.ResponseRecorder {
			r := httptest.NewRequest(http.MethodPost, "http://"+opHost+"/operator/login", strings.NewReader("email=%zz"))
			r.Host = opHost
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("Origin", opOrigin)
			hostileOn(r)
			r.RemoteAddr = ad.next() + ":1"
			*last = driveLog{r.Method, r.URL.Path}
			w := httptest.NewRecorder()
			g.h.ServeHTTP(w, r)
			return w
		}},
		{"C9 code page without a challenge", 303, once(get("/operator/login/totp"))},
		{"C10 code page", 200, func() *httptest.ResponseRecorder { return send(get("/operator/login/totp", challenge(f))) }},
		{"C11 code without a challenge", 303, once(post("/operator/login/totp", url.Values{"code": {"000000"}}))},
		{"C12 code accepted", 303, func() *httptest.ResponseRecorder {
			return send(post("/operator/login/totp", url.Values{"code": {totpAt(f.key, g.now)}}, challenge(f)))
		}},
		{"C13 code with a challenge that does not verify", 401, once(post("/operator/login/totp", url.Values{"code": {"000000"}},
			&http.Cookie{Name: operatorauth.ChallengeCookieName, Value: "x.y"}))},
		{"C14 code rejected", 401, func() *httptest.ResponseRecorder {
			return send(post("/operator/login/totp", url.Values{"code": {wrongCodeAt(f.key, g.now)}}, challenge(f)))
		}},
		{"C15 code while locked", 401, func() *httptest.ResponseRecorder {
			w := send(post("/operator/login/totp", url.Values{"code": {totpAt(lockedF.key, g.now)}}, challenge(lockedF)))
			if !strings.Contains(w.Body.String(), "Sign-in is locked for a while") {
				t.Error("C15 is not the locked page")
			}
			return w
		}},
		{"C16 code throttled (account budget)", 429, func() *httptest.ResponseRecorder {
			fx := g.active()
			ch := challenge(fx)
			var w *httptest.ResponseRecorder
			for i := 0; i < 11; i++ {
				w = send(post("/operator/login/totp", url.Values{"code": {wrongCodeAt(fx.key, g.now)}}, ch))
			}
			return w
		}},
		{"C17 code fault", 503, func() *httptest.ResponseRecorder {
			ch := challenge(f)
			return failing("OperatorByID", post("/operator/login/totp", url.Values{"code": {"000000"}}, ch))()
		}},
		{"C18 enrollment page", 200, once(get("/operator/enroll?id=" + pending.id.String()))},
		{"C19 enrollment link incomplete", 400, once(get("/operator/enroll"))},
		{"C20 enrollment re-render, passwords differ", 400, once(post("/operator/enroll", enroll(linkToken, pw, pw+"x", totpAt(pageKey, g.now))))},
		{"C21 enrollment re-render, weak password", 400, once(post("/operator/enroll", enroll(linkToken, "short", "short", totpAt(pageKey, g.now))))},
		{"C22 enrollment re-render, code rejected", 401, once(post("/operator/enroll", enroll(linkToken, pw, pw, wrongCodeAt(pageKey, g.now))))},
		{"C23 enrollment refused (malformed link)", 400, once(post("/operator/enroll", enroll("short", pw, pw, totpAt(pageKey, g.now))))},
		{"C24 enrollment throttled (per-address share)", 429, repeat(4, post("/operator/enroll",
			enroll(base64.RawURLEncoding.EncodeToString(randBytes(t, 32)), pw, pw, totpAt(pageKey, g.now))))},
		{"C25 enrollment fault", 503, failing("CompleteOperatorEnrollment", post("/operator/enroll", enroll(linkToken, pw, pw, totpAt(pageKey, g.now))))},
		{"C26 enrollment completed", 303, once(post("/operator/enroll", enroll(linkToken, pw, pw, totpAt(pageKey, g.now))))},
		{"C27 console", 200, func() *httptest.ResponseRecorder { return send(get("/operator", signIn())) }},
		{"C28 console without a cookie", 303, once(get("/operator"))},
		{"C29 console with a dead cookie", 303, once(get("/operator", &http.Cookie{Name: operatorauth.SessionCookieName, Value: strings.Repeat("D", 43)}))},
		{"C30 console, a same-site read", 303, func() *httptest.ResponseRecorder {
			return send(req{method: http.MethodGet, path: "/operator", cookies: []*http.Cookie{signIn()}, header: map[string]string{"Sec-Fetch-Site": "same-site"}})
		}},
		{"C31 console, session budget", 429, func() *httptest.ResponseRecorder {
			c := signIn()
			var w *httptest.ResponseRecorder
			for i := 0; i < 101; i++ {
				w = send(get("/operator", c))
			}
			return w
		}},
		{"C32 console, the session check fails", 303, func() *httptest.ResponseRecorder {
			return failing("TouchOperatorSession", get("/operator", signIn()))()
		}},
		{"C33 sign-out", 303, func() *httptest.ResponseRecorder { return send(post("/operator/logout", url.Values{}, signIn())) }},
		{"C34 sign-out without a cookie", 303, once(post("/operator/logout", url.Values{}))},
		{"C35 sign-out fault", 503, func() *httptest.ResponseRecorder {
			return failing("CloseOperatorSession", post("/operator/logout", url.Values{}, signIn()))()
		}},
		{"C36 sign-out ceiling", 429, repeat(3001, post("/operator/logout", url.Values{},
			&http.Cookie{Name: operatorauth.SessionCookieName, Value: strings.Repeat("E", 43)}))},
		{"C37 sign-out cross-origin", 403, once(req{method: http.MethodPost, path: "/operator/logout", form: url.Values{}, origin: "https://taptime.mt"})},
		{"C38 flood ceiling", 429, repeat(301, get("/operator/login"))},
		{"C39 unknown path", 404, once(get("/operator/no-such"))},
		{"C40 wrong method", 405, once(req{method: http.MethodPut, path: "/operator/login", origin: opOrigin})},
	}
	if len(classes) != 40 {
		t.Fatalf("PREMISE: %d classes, the comment says 40", len(classes))
	}
	for i, c := range classes {
		if !strings.HasPrefix(c.name, "C"+strconv.Itoa(i+1)+" ") {
			t.Fatalf("PREMISE: class %d is named %q, want C%d", i+1, c.name, i+1)
		}
	}
	scripted := runHeaderClasses(t, classes, last)
	// CONTROL: the hostile drive reaches the request, and the check sees it -- a handler
	// that copies the request's headers into its response and writes its query into the
	// body fails the check once per hostile header at least, and on the query.
	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		maps.Copy(w.Header(), r.Header)
		w.Header().Set("Content-Type", pageType)
		_, _ = w.Write([]byte(r.URL.RawQuery))
	})
	hr := hostileDrive(req{method: http.MethodGet, path: "/operator/login"})
	er := httptest.NewRequest(hr.method, "http://"+opHost+hr.path, nil)
	for k, v := range hr.header {
		er.Header.Set(k, v)
	}
	er.Header.Add("Cookie", hr.cookieLine)
	ew := httptest.NewRecorder()
	echo.ServeHTTP(ew, er)
	var caught []string
	checkDesignedHeaders(func(f string, a ...any) { caught = append(caught, fmt.Sprintf(f, a...)) }, "control", ew, designedHeaders["C1"])
	if len(caught) < len(hostileHeaders) || !strings.Contains(strings.Join(caught, "\n"), hostileQuery.Get("token")) {
		t.Fatalf("CONTROL: a header-copying, query-echoing handler failed the check %d time(s): %v", len(caught), caught)
	}
	// The classes whose body loads a script, by identity: the enrollment screen's first
	// load and its three re-renders (4th-round audit, N6: a count alone let two classes
	// trade places).
	if got := strings.Join(scripted, ","); got != "C18,C20,C21,C22" {
		t.Errorf("the classes whose body loads a script are %s, want C18,C20,C21,C22", got)
	}
}

// TestProblemPages_LinkOnlyToMountedRoutes renders the ten ProblemViews problemPages lists
// and holds each link to a route the surface mounts (operatorRoutes).
// TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo is the pin on where ProblemView
// fields are set (its header lists what it catches).
func TestProblemPages_LinkOnlyToMountedRoutes(t *testing.T) {
	mounted := map[string]bool{}
	for _, r := range operatorRoutes {
		mounted[r.path] = true
	}
	pages := operator.ProblemPagesForTest()
	if len(pages) < 10 {
		t.Fatalf("PREMISE: %d problem page(s)", len(pages))
	}
	links := 0
	for i, v := range pages {
		var b bytes.Buffer
		if err := operatorpages.Problem(v).Render(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		for _, m := range regexp.MustCompile(`\s(href|action|src)="([^"]*)"`).FindAllStringSubmatch(b.String(), -1) {
			if strings.HasPrefix(m[2], "/static/") {
				continue
			}
			links++
			if !mounted[m[2]] {
				t.Errorf("problem page %d (%q): %s=%q is not a route the surface mounts", i, v.Title, m[1], m[2])
			}
		}
	}
	if links == 0 {
		t.Fatal("PREMISE: no link found on any problem page; the scan is blind")
	}
}

// enrollScriptReviewed is the sha256 of the enrollment script's CODE -- its text with
// whole-line comments and blank lines dropped and the lines trimmed -- as reviewed in M10
// OP-8 (2026-10-02). An edit to the code turns TestEnrollScript_IsTheReviewedBody red; a
// reviewer updates this value with the script's behaviour re-measured in a browser (the
// card's L15 says how it was measured).
const enrollScriptReviewed = "685da65570bf211052c1b4447b80ce9061f1db4e697cd6468ad4a44f06ff97ab"

// enrollScriptCode is the normalised code of the shipped script.
func enrollScriptCode(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "js", "operator", "enroll.js"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "//") {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// TestEnrollScript_IsTheReviewedBody pins the enrollment script's code, after
// normalisation, to the reviewed body. It is a TEXT pin: no test here runs the script (the
// repository has no JavaScript engine and adds none); the behaviour of the reviewed body
// was measured by hand in headless Chrome on 2026-10-02 (the card's L15). CONTROL: the
// normalisation keeps code and drops comments.
func TestEnrollScript_IsTheReviewedBody(t *testing.T) {
	code := enrollScriptCode(t)
	if !strings.Contains(code, "history.replaceState") || strings.Contains(code, "THE POLICY") {
		t.Fatal("CONTROL: the normalisation does not keep the code and drop the comments")
	}
	sum := sha256.Sum256([]byte(code))
	if got := hex.EncodeToString(sum[:]); got != enrollScriptReviewed {
		t.Errorf("web/static/js/operator/enroll.js's code is not the reviewed body (sha256 %s, reviewed %s). "+
			"Re-measure its behaviour in a browser before updating the constant.", got, enrollScriptReviewed)
	}
}

// TestLogout_ACookielessFloodCostsNothingAndCannotRefuseIt: 3 001 sign-outs without a
// session cookie from the operator's own address make no store call (requireOperator
// answers them before logoutGate), and the operator's sign-out from there afterwards
// revokes the session.
func TestLogout_ACookielessFloodCostsNothingAndCannotRefuseIt(t *testing.T) {
	g := newRig(t)
	sess := g.signIn(g.active())
	const shared = "203.0.113.70:1"
	before := g.store.total()
	for i := 0; i < 3001; i++ {
		if w := g.do(req{method: http.MethodPost, host: opHost, path: "/operator/logout", form: url.Values{}, origin: opOrigin, remote: shared}); w.Code != http.StatusSeeOther {
			t.Fatalf("cookieless sign-out %d = %d, want 303", i+1, w.Code)
		}
	}
	if n := g.store.total() - before; n != 0 {
		t.Fatalf("3 001 cookieless sign-outs made %d store call(s)", n)
	}
	w := g.do(req{method: http.MethodPost, host: opHost, path: "/operator/logout", form: url.Values{}, origin: opOrigin, remote: shared,
		cookies: []*http.Cookie{sess}})
	if w.Code != http.StatusSeeOther || g.store.liveSessions() != 0 {
		t.Fatalf("the operator's sign-out after the flood = %d with %d live session(s)", w.Code, g.store.liveSessions())
	}
}

// TestLogout_PastTheCeilingTheBrowserStillForgetsTheSession measures the WEAKER invariant
// the sign-out keeps (routes.go, mount): after 3 000 cookie-bearing sign-outs from the
// operator's address (each one definer call), the operator's own sign-out from there
// answers 429; that 429 clears the session cookie in the browser, and the session is still
// live in the store (and the console still renders with a copy of the token). CONTROL:
// from another address the same session signs out.
func TestLogout_PastTheCeilingTheBrowserStillForgetsTheSession(t *testing.T) {
	g := newRig(t)
	sess := g.signIn(g.active())
	const shared = "203.0.113.71:1"
	for i := 0; i < 3000; i++ {
		g.do(req{method: http.MethodPost, host: opHost, path: "/operator/logout", form: url.Values{}, origin: opOrigin, remote: shared,
			cookies: []*http.Cookie{{Name: operatorauth.SessionCookieName, Value: base64.RawURLEncoding.EncodeToString(randBytes(t, 32))}}})
	}
	closes := g.store.count("CloseOperatorSession")
	if closes != 3000 {
		t.Fatalf("PREMISE: the third party's sign-outs made %d close call(s), want 3000", closes)
	}
	w := g.do(req{method: http.MethodPost, host: opHost, path: "/operator/logout", form: url.Values{}, origin: opOrigin, remote: shared,
		cookies: []*http.Cookie{sess}})
	c := cookie(w, operatorauth.SessionCookieName)
	if w.Code != http.StatusTooManyRequests || c == nil || c.MaxAge >= 0 || g.store.count("CloseOperatorSession") != closes {
		t.Fatalf("the operator's sign-out past the ceiling = %d, cookie cleared %v, closes +%d", w.Code, c != nil && c.MaxAge < 0,
			g.store.count("CloseOperatorSession")-closes)
	}
	if g.store.liveSessions() != 1 {
		t.Fatalf("PREMISE: %d live session(s); the refused sign-out should have left the session alive", g.store.liveSessions())
	}
	if w := g.get("/operator", sess); w.Code != http.StatusOK {
		t.Fatalf("a copy of the token after the refused sign-out = %d, want the console (the session is alive)", w.Code)
	}
	// CONTROL: from another address.
	if w := g.post("/operator/logout", url.Values{}, sess); w.Code != http.StatusSeeOther || g.store.liveSessions() != 0 {
		t.Fatalf("CONTROL: the sign-out from another address = %d with %d live", w.Code, g.store.liveSessions())
	}
}

// hostileHeaders are fifteen request headers the header tables send on their requests:
// nine named like response headers and six a handler could copy into a response.
var hostileHeaders = map[string]string{
	"Location":                    "https://evil.example/hostile-location",
	"Content-Security-Policy":     "script-src * 'unsafe-inline' hostile",
	"Cache-Control":               "public, max-age=86400, hostile",
	"Referrer-Policy":             "unsafe-url-hostile",
	"X-Content-Type-Options":      "hostile-sniff",
	"Set-Cookie":                  "__Host-taptime_op=hostile-set-cookie; Path=/",
	"X-Frame-Options":             "ALLOWALL-hostile",
	"Access-Control-Allow-Origin": "https://evil.example/hostile-acao",
	"Refresh":                     "0;url=https://evil.example/hostile-refresh",
	// Request headers a handler could copy into a response (5th round, F2: H05b put the
	// Referer into Location).
	"Referer":          "https://evil.example/hostile-referer",
	"User-Agent":       "hostile-agent/1.0",
	"Accept-Language":  "xx-hostile",
	"X-Requested-With": "hostile-xhr",
	"HX-Current-URL":   "https://evil.example/hostile-hx-current-url",
	"HX-Target":        "hostile-hx-target",
}

// hostileQuery is a query string carrying a value under five credential field names and
// "blob"; hostileDrive appends it to the sign-in, code and enrollment requests (with an id
// on a POST: the enrollment GET reads its id from the query by design).
var hostileQuery = url.Values{
	"token": {"qry-hostile-token-" + strings.Repeat("q", 6)}, "email": {"qry-hostile@example.test"},
	"password": {"qry hostile passphrase"}, "password_again": {"qry hostile passphrase"},
	"code": {"917351"}, "blob": {"QRYblobHostileValue"},
}

// hostileValues are the hostile request values checkDesignedHeaders looks for in a response.
func hostileValues() []string {
	var out []string
	for _, v := range hostileHeaders {
		out = append(out, v)
	}
	for _, vs := range hostileQuery {
		out = append(out, vs...)
	}
	return append(out, "hostile-cookie-line", hostileQueryID)
}

const hostileQueryID = "0b0b0b0b-1111-4222-8333-444444444444"

// hostileCookieLine is the second Cookie line: for each of the operator's two cookie names,
// the name with a hostile value when the request already carries that cookie (net/http
// hands a handler the first of two cookies of one name -- the card's escape measurement),
// and otherwise the name inside another cookie's value, so a class is not given a cookie it
// was not driven with.
func hostileCookieLine(cookies []*http.Cookie) string {
	var parts []string
	for i, name := range []string{operatorauth.SessionCookieName, operatorauth.ChallengeCookieName} {
		has := false
		for _, c := range cookies {
			has = has || c.Name == name
		}
		if has {
			parts = append(parts, name+"=hostile-cookie-line")
		} else {
			parts = append(parts, fmt.Sprintf("op8_hostile_%d=%s", i, name))
		}
	}
	return strings.Join(parts, "; ")
}

// hostileDrive is r with the hostile headers (a header the request sets itself --
// Origin, Sec-Fetch-Site -- is kept), the second Cookie line and, on the sign-in, code,
// enrollment and (OP-10) legal paths, the hostile query.
func hostileDrive(r req) req {
	h := map[string]string{}
	for k, v := range hostileHeaders {
		h[k] = v
	}
	for k, v := range r.header {
		h[k] = v
	}
	r.header = h
	r.cookieLine = hostileCookieLine(r.cookies)
	path, _, _ := strings.Cut(r.path, "?")
	switch path {
	case "/operator/login", "/operator/login/totp", "/operator/enroll", "/operator/legal":
		q := url.Values{}
		for k, v := range hostileQuery {
			q[k] = v
		}
		if r.method == http.MethodPost {
			q.Set("id", hostileQueryID)
		}
		sep := "?"
		if strings.Contains(r.path, "?") {
			sep = "&"
		}
		r.path += sep + q.Encode()
	}
	return r
}

// hostileOn applies the hostile headers and the second Cookie line to a request built
// by hand (C8; it carries no cookie and no query of its own -- the hostile query is
// added to its URL).
func hostileOn(r *http.Request) {
	for k, v := range hostileHeaders {
		r.Header.Set(k, v)
	}
	r.Header.Add("Cookie", hostileCookieLine(nil))
	q := url.Values{}
	for k, v := range hostileQuery {
		q[k] = v
	}
	q.Set("id", hostileQueryID)
	r.URL.RawQuery = q.Encode()
}

// designed is a class's response headers beyond the four each of the 66 (C1-C66, the
// three header tables) carries (Content-Security-Policy, Cache-Control,
// X-Content-Type-Options, Referrer-Policy):
// its Location and Content-Type ("" = absent), its Allow values (chi writes one per
// registered method, sorted here; "" = absent) and its Set-Cookie headers, each
// "<cookie name>=set" or "<cookie name>=clear". Read off the shipped handlers and
// measured on them (2026-10-02, the 4th round; C41-C48 the 5th; C49-C66 OP-10, 2026-10-03).
type designed struct {
	loc, ct, allow string
	cookies        []string
}

const (
	pageType      = "text/html; charset=utf-8"
	notFoundType  = "text/plain; charset=utf-8"
	sessionSet    = operatorauth.SessionCookieName + "=set"
	sessionClear  = operatorauth.SessionCookieName + "=clear"
	challengeSet  = operatorauth.ChallengeCookieName + "=set"
	challengeDrop = operatorauth.ChallengeCookieName + "=clear"
)

var designedHeaders = map[string]designed{
	"C1": {ct: pageType}, "C2": {ct: pageType},
	"C3": {loc: "/operator/login/totp", cookies: []string{challengeSet}},
	"C4": {ct: pageType}, "C5": {ct: pageType}, "C6": {ct: pageType}, "C7": {ct: pageType}, "C8": {ct: pageType},
	"C9": {loc: "/operator/login"}, "C10": {ct: pageType}, "C11": {loc: "/operator/login"},
	"C12": {loc: "/operator", cookies: []string{sessionSet, challengeDrop}},
	"C13": {ct: pageType, cookies: []string{challengeDrop}},
	"C14": {ct: pageType}, "C15": {ct: pageType}, "C16": {ct: pageType}, "C17": {ct: pageType},
	"C18": {ct: pageType}, "C19": {ct: pageType}, "C20": {ct: pageType}, "C21": {ct: pageType},
	"C22": {ct: pageType}, "C23": {ct: pageType}, "C24": {ct: pageType}, "C25": {ct: pageType},
	"C26": {loc: "/operator", cookies: []string{sessionSet, challengeDrop}},
	"C27": {ct: pageType}, "C28": {loc: "/operator/login"},
	"C29": {loc: "/operator/login", cookies: []string{sessionClear}},
	"C30": {loc: "/operator/login"}, "C31": {ct: pageType}, "C32": {loc: "/operator/login"},
	"C33": {loc: "/operator/login", cookies: []string{sessionClear, challengeDrop}},
	"C34": {loc: "/operator/login"}, "C35": {ct: pageType},
	"C36": {ct: pageType, cookies: []string{sessionClear}},
	"C37": {ct: pageType}, "C38": {ct: pageType}, "C39": {ct: notFoundType}, "C40": {allow: "GET,POST"},
	"C41": {allow: "GET"}, "C42": {allow: "GET"}, "C43": {allow: "GET"},
	"C44": {allow: "GET,POST"}, "C45": {allow: "GET,POST"}, "C46": {allow: "POST"},
	"C47": {ct: pageType}, "C48": {ct: pageType},
	// OP-10's legal screen (op10_test.go): the page and its refusals are pages; the
	// sign-in redirects of the gate and of a session the store refuses; the dead cookie
	// cleared; the publication's 303 back to the screen, a failed refresh after it
	// included (C64, POST -> 303 -> GET); PUT's 405.
	"C49": {ct: pageType}, "C50": {loc: "/operator/login"},
	"C51": {loc: "/operator/login", cookies: []string{sessionClear}},
	"C52": {loc: "/operator/login"}, "C53": {ct: pageType}, "C54": {loc: "/operator/login"}, "C55": {ct: pageType},
	"C56": {loc: "/operator/legal"}, "C57": {loc: "/operator/login"}, "C58": {ct: pageType}, "C59": {ct: pageType},
	"C60": {ct: pageType}, "C61": {ct: pageType}, "C62": {ct: pageType}, "C63": {ct: pageType}, "C64": {loc: "/operator/legal"},
	"C65": {loc: "/operator/login"},
	"C66": {allow: "GET,POST"},
}

// checkDesignedHeaders holds the response headers AT WriteHeader (w.Result().Header, the
// snapshot taken there; op8r5_test.go compares it with the wire's header section on three
// classes) to d: the set of header NAMES is exactly the designed set, each single-valued
// header carries exactly one value equal to its designed one, each Set-Cookie is one of
// the operator's two cookies with the designed attributes and the designed set/clear, and
// no header value and no byte of the body carries a hostile request value (hostileValues)
// in its raw or its query-escaped form -- the two forms it searches.
func checkDesignedHeaders(errorf func(string, ...any), class string, w *httptest.ResponseRecorder, d designed) {
	h := w.Result().Header // the snapshot at WriteHeader (compared with the wire on three classes: op8r5_test.go)
	single := map[string]string{"Cache-Control": "no-store", "X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer",
		"Location": d.loc, "Content-Type": d.ct}
	want := []string{"Content-Security-Policy"}
	if d.allow != "" {
		want = append(want, "Allow")
	}
	allow := append([]string(nil), h.Values("Allow")...)
	sort.Strings(allow)
	if strings.Join(allow, ",") != d.allow {
		errorf("%s: Allow = %q, want %q", class, allow, d.allow)
	}
	for k, v := range single {
		if v != "" {
			want = append(want, k)
		}
	}
	if len(d.cookies) > 0 {
		want = append(want, "Set-Cookie")
	}
	var got []string
	for k := range h {
		got = append(got, k)
	}
	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		errorf("%s: response header names %v, want %v", class, got, want)
	}
	for k, v := range single {
		if vs := h.Values(k); v != "" && (len(vs) != 1 || vs[0] != v) {
			errorf("%s: %s = %q, want exactly %q", class, k, vs, v)
		}
	}
	var cookies []string
	for _, c := range w.Result().Cookies() {
		state := "set"
		if c.MaxAge < 0 && c.Value == "" {
			state = "clear"
		} else if c.MaxAge <= 0 || c.Value == "" {
			state = "malformed"
		}
		cookies = append(cookies, c.Name+"="+state)
		if c.Path != "/" || !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Domain != "" {
			errorf("%s: Set-Cookie %s with attributes path=%q secure=%v httponly=%v samesite=%v domain=%q", class, c.Name, c.Path, c.Secure, c.HttpOnly, c.SameSite, c.Domain)
		}
	}
	wantCookies := append([]string(nil), d.cookies...)
	sort.Strings(cookies)
	sort.Strings(wantCookies)
	if strings.Join(cookies, ",") != strings.Join(wantCookies, ",") {
		errorf("%s: Set-Cookie %v, want %v", class, cookies, wantCookies)
	}
	var text strings.Builder
	text.WriteString(w.Body.String())
	for k, vs := range h {
		text.WriteString("\n" + k + ": " + strings.Join(vs, "\n"))
	}
	for _, v := range hostileValues() {
		for _, form := range []string{v, url.QueryEscape(v)} {
			if strings.Contains(text.String(), form) {
				errorf("%s: the response carries the hostile request value %q", class, form)
			}
		}
	}
}
