package handler

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/session"
)

// admincookiepath_test.go -- the SECOND half of the M6-01 acceptance criterion
// "an employee cookie cannot reach the panel and an admin cookie cannot reach the
// tap surface", tested as BEHAVIOUR rather than as a constant.
//
// 🔴 WHY THIS FILE EXISTS: THE PREVIOUS TESTS WERE TAUTOLOGICAL, and an
// independent audit proved it by mutation. Both places that mentioned the path
// compared the constant WITH ITSELF:
//
//	token_test.go        if ck.Path != CookiePath        -- true for any value
//	adminlogin_test.go   strings.HasPrefix(route, CookiePath) -- "/" passes for ALL
//
// So changing adminauth.CookiePath from "/admin" to "/" -- and NOTHING else --
// left internal/adminauth, internal/handler and internal/httpx all GREEN, while
// every panel cookie became one the browser sends to /t, /api/checkin and
// /activate. Reproduced here before writing this file:
//
//	=== mutation: CookiePath = "/" (definition only) ===
//	ok  internal/adminauth  23.431s
//	ok  internal/handler   116.489s
//	ok  internal/httpx       4.295s
//
// Breaking the constant's USE was caught (Set() with a literal "/" turns
// TestCookies_ClearMatchesSet red); breaking its DEFINITION was not. cookie.go
// calls the path "THE STRUCTURAL HALF" of the separation, so the structural half
// had no net at all.
//
// WHAT THIS FILE PINS INSTEAD: what a REAL http.CookieJar does. The jar is the
// thing that actually decides whether a cookie crosses to /t, and it decides on
// the Path attribute -- so driving it is testing the mechanism rather than the
// spelling. A stub stands in for the tap and activation handlers because the
// property is entirely about TRANSMISSION: a handler cannot read a cookie the
// browser never sent, whatever that handler does.
//
// EVERY DIRECTION HAS A POSITIVE CONTROL, because a test that asserts "nothing was
// seen" is satisfied by an instrument that sees nothing. This repo has been bitten
// by exactly that (M5-09 found an assertion helper that was structurally incapable
// of failing — the helper is long gone, the lesson is not),
// so each assertion below is paired with one that must SUCCEED.

// tapSurfacePaths are the employee-facing routes an admin cookie must never reach.
//
// ⚠️ IT USED TO BE HAND-WRITTEN AND IT WAS INCOMPLETE: four of the six routes the
// employee flows actually mount, missing /activate/tour and /api/activate, under a
// comment claiming it was "the real paths". That is the countable-list class this
// task has already been bitten by twice, so the list is now DERIVED FROM SOURCE and
// the hand-written copy is only a floor.
//
// employeeRoutes() reads the Mount methods of this package and returns every route
// literal outside the panel's own prefix, so a new employee route joins the check
// the day it is written rather than the day somebody remembers this file.
var tapSurfacePaths = employeeRoutes()

// employeeRoutes scans this package's non-test sources for chi route registrations
// and returns the ones OUTSIDE adminauth.CookiePath.
//
// A source scan rather than a live chi.Walk, deliberately: walking the real router
// would mean constructing handler.Activation and handler.Tap, which need a
// database, an invite manager, a session manager, a SUN verifier and an audit
// recorder — a lot of machinery to answer a question that is textually decidable.
// The trade is stated rather than hidden: this sees route LITERALS, so a route
// registered through a variable would be missed. knownEmployeeRoutes below is the
// floor that keeps that from silently emptying the list.
func employeeRoutes() []string {
	files, err := filepath.Glob("*.go")
	if err != nil {
		panic("handler: globbing package sources: " + err.Error())
	}
	re := regexp.MustCompile(`r\.(?:Get|Post|Put|Delete|Patch|Head|Handle)\("(/[^"]*)"`)
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			panic("handler: reading " + f + ": " + err.Error())
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			route := m[1]
			if route == adminauth.CookiePath || strings.HasPrefix(route, adminauth.CookiePath+"/") {
				continue // the panel's own routes
			}
			if !seen[route] {
				seen[route] = true
				out = append(out, route)
			}
		}
	}
	sort.Strings(out)
	return out
}

// knownEmployeeRoutes is the FLOOR: routes that must appear whatever the scanner
// does. If the regex stops matching, the derived list would quietly shrink to
// nothing and every assertion built on it would pass vacuously — the exact failure
// this repo names as "a check that cannot fail".
var knownEmployeeRoutes = []string{"/t", "/api/checkin", "/activate", "/activate/done", "/activate/tour", "/api/activate"}

// TestEmployeeRoutes_DerivationIsNotVacuous pins the scanner itself.
func TestEmployeeRoutes_DerivationIsNotVacuous(t *testing.T) {
	got := employeeRoutes()
	t.Logf("derived %d employee-facing routes: %v", len(got), got)
	if len(got) < len(knownEmployeeRoutes) {
		t.Fatalf("the scanner derived %d routes (%v) but at least %d are known to exist (%v) — "+
			"the derivation has gone blind and every cookie assertion built on it would be vacuous",
			len(got), got, len(knownEmployeeRoutes), knownEmployeeRoutes)
	}
	for _, want := range knownEmployeeRoutes {
		if !contains(got, want) {
			t.Fatalf("the derived route list is missing %q; got %v", want, got)
		}
	}
	// And it must NOT have swept in the panel's own routes.
	for _, r := range got {
		if r == adminauth.CookiePath || strings.HasPrefix(r, adminauth.CookiePath+"/") {
			t.Fatalf("the derived list contains the panel route %q", r)
		}
	}
}

// panelCookieNames is every cookie this feature writes. All three are scoped to
// adminauth.CookiePath, and the test covers all three rather than only the session
// one -- the two short-lived auth cookies carry a synchronizer token and a signed
// verified-candidate set, and leaking either to another surface is the same class
// of mistake.
func panelCookieNames() []string {
	return []string{adminauth.CookieName, adminLoginCookieName, adminChoiceCookieName}
}

// cookieSpy is the stub tap surface: it answers with the names of the cookies the
// BROWSER chose to send, sorted, so the assertions read as a set.
func cookieSpy(w http.ResponseWriter, r *http.Request) {
	var names []string
	for _, c := range r.Cookies() {
		names = append(names, c.Name)
	}
	sort.Strings(names)
	_, _ = fmt.Fprint(w, strings.Join(names, ","))
}

type pathHarness struct {
	t      *testing.T
	server *httptest.Server
	client *http.Client
	jar    http.CookieJar
	admins *fakeAdmins
}

func newPathHarness(t *testing.T, verified []adminauth.Verified) *pathHarness {
	t.Helper()

	// The server is started BEFORE the handler is built: the panel's Origin check
	// is strict, so cfg.BaseURL must be the address this test really serves from.
	r := chi.NewRouter()
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	cfg := adminTestConfig()
	cfg.BaseURL = srv.URL // http, so the jar keeps a non-Secure cookie

	attempts := make([]adminauth.Attempt, 0, len(verified))
	for _, v := range verified {
		attempts = append(attempts, adminauth.Attempt{
			AdminUserID: v.AdminUserID, TenantID: v.TenantID,
			PasswordMatched: true, Active: true,
		})
	}
	admins := &fakeAdmins{
		authenticate: func(string, string) (adminauth.Authentication, error) {
			return adminauth.Authentication{Resolved: len(attempts), Attempts: attempts}, nil
		},
		verify: func() (adminauth.Resolved, error) {
			return adminauth.Resolved{
				SessionID: uuid.New(), TenantID: verified[0].TenantID,
				AdminUserID: verified[0].AdminUserID, Role: "owner", FullName: "KF Owner",
			}, nil
		},
	}
	fake := newFakeLedger()
	h, err := NewAdminAuth(admins, &fakeTrail{}, fake, fake, &fakeReviewer{}, &fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), cfg, discardLogger())
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	h.Mount(r)
	for _, p := range tapSurfacePaths {
		r.Get(p, cookieSpy)
		r.Post(p, cookieSpy)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	return &pathHarness{
		t: t, server: srv, jar: jar, admins: admins,
		client: &http.Client{
			Jar:           jar,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// saw returns the cookie names the browser sent to path, as reported by the
// server itself.
func (p *pathHarness) saw(path string) []string {
	p.t.Helper()
	res, err := p.client.Get(p.server.URL + path)
	if err != nil {
		p.t.Fatalf("GET %s: %v", path, err)
	}
	body := readAll(p.t, res)
	if body == "" {
		return nil
	}
	return strings.Split(body, ",")
}

// jarSees is the SECOND, independent reading of the same fact: ask the jar
// directly which cookies it would attach to a URL. It does not go through the
// server at all, so a bug in the stub cannot make both readings agree.
func (p *pathHarness) jarSees(path string) []string {
	p.t.Helper()
	u, err := url.Parse(p.server.URL + path)
	if err != nil {
		p.t.Fatalf("parse url: %v", err)
	}
	var names []string
	for _, c := range p.jar.Cookies(u) {
		names = append(names, c.Name)
	}
	sort.Strings(names)
	return names
}

func (p *pathHarness) post(path string, form url.Values) *http.Response {
	p.t.Helper()
	req, err := http.NewRequest(http.MethodPost, p.server.URL+path, strings.NewReader(form.Encode()))
	if err != nil {
		p.t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", p.server.URL)
	res, err := p.client.Do(req)
	if err != nil {
		p.t.Fatalf("POST %s: %v", path, err)
	}
	return res
}

// plantEmployeeCookie puts a Path=/ employee session cookie in the jar, exactly as
// internal/session.Cookies.Set would. It is the POSITIVE CONTROL for the whole
// file: it must be visible at /t, which proves the spy and the jar really do
// report cookies that are supposed to arrive.
func (p *pathHarness) plantEmployeeCookie() {
	p.t.Helper()
	u, err := url.Parse(p.server.URL + "/")
	if err != nil {
		p.t.Fatalf("parse url: %v", err)
	}
	p.jar.SetCookies(u, []*http.Cookie{{
		Name: session.CookieName, Value: "EMPLOYEEfakeEMPLOYEEfakeEMPLOYEEfake12345678",
		Path: "/", HttpOnly: true,
	}})
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// TestPanelCookies_NeverReachTheTapSurface is the criterion, in both directions,
// for all THREE panel cookies, through a real http.CookieJar.
//
// 🔴 IT IS THE NET FOR adminauth.CookiePath. Change that constant to "/" and this
// test goes red -- verified by mutation, which is the whole reason the file
// exists.
func TestPanelCookies_NeverReachTheTapSurface(t *testing.T) {
	v1 := adminauth.Verified{AdminUserID: uuid.New(), TenantID: uuid.New()}
	v2 := adminauth.Verified{AdminUserID: uuid.New(), TenantID: uuid.New()}
	p := newPathHarness(t, []adminauth.Verified{v1, v2})

	// POSITIVE CONTROL FIRST, before any panel cookie exists: the instrument works.
	p.plantEmployeeCookie()
	for _, path := range tapSurfacePaths {
		if got := p.saw(path); !contains(got, session.CookieName) {
			t.Fatalf("control: the EMPLOYEE cookie is not visible at %s (saw %v) — the spy reports "+
				"nothing, so every 'not seen' assertion below would be vacuous", path, got)
		}
	}

	// ---- STATE 1: mid-login. The two SHORT-LIVED auth cookies exist. ----------
	page := htmlOf(t, httpGet(t, p, "/admin/login"))
	csrf := csrfFrom(t, page)
	res := p.post("/admin/login", url.Values{
		"csrf": {csrf}, "email": {"owner@example.test"}, "password": {"right"},
	})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin/login/choose" {
		t.Fatalf("control: two verified businesses did not reach the picker (%d %q)",
			res.StatusCode, res.Header.Get("Location"))
	}
	// Both short-lived cookies really exist, or the assertions below prove nothing.
	atPanel := p.jarSees("/admin/login/choose")
	for _, name := range []string{adminLoginCookieName, adminChoiceCookieName} {
		if !contains(atPanel, name) {
			t.Fatalf("control: %s is not in the jar for the panel (saw %v)", name, atPanel)
		}
	}
	assertPanelCookiesAbsent(t, p, "mid-login")

	// ---- STATE 2: signed in. The SESSION cookie exists. -----------------------
	picker := htmlOf(t, httpGet(t, p, "/admin/login/choose"))
	res = p.post("/admin/login/choose", url.Values{
		"csrf": {csrfFrom(t, picker)}, "tenant_id": {v2.TenantID.String()},
	})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin" {
		t.Fatalf("control: the legitimate choice did not sign in (%d %q)",
			res.StatusCode, res.Header.Get("Location"))
	}
	if got := p.jarSees("/admin"); !contains(got, adminauth.CookieName) {
		t.Fatalf("control: the panel session cookie is not in the jar for /admin (saw %v)", got)
	}
	assertPanelCookiesAbsent(t, p, "signed in")

	// And the employee cookie is STILL visible at /t in the signed-in state, so the
	// instrument did not stop working half way through.
	if got := p.saw("/t"); !contains(got, session.CookieName) {
		t.Fatalf("control: the employee cookie vanished from /t (saw %v)", got)
	}
	t.Logf("GET /t while signed into the panel saw exactly: %v", p.saw("/t"))
}

// assertPanelCookiesAbsent checks BOTH readings -- what the server received and
// what the jar would attach -- for every panel cookie on every tap-surface path.
func assertPanelCookiesAbsent(t *testing.T, p *pathHarness, state string) {
	t.Helper()
	for _, path := range tapSurfacePaths {
		serverSaw := p.saw(path)
		jarWould := p.jarSees(path)
		for _, name := range panelCookieNames() {
			if contains(serverSaw, name) {
				t.Fatalf("[%s] the tap surface RECEIVED the panel cookie %q at %s (saw %v). "+
					"adminauth.CookiePath is what stops this; cookie.go calls it THE STRUCTURAL "+
					"HALF of the employee/admin separation (M6-01 criterion K2).",
					state, name, path, serverSaw)
			}
			if contains(jarWould, name) {
				t.Fatalf("[%s] the cookie jar would attach the panel cookie %q to %s (would send %v)",
					state, name, path, jarWould)
			}
		}
	}
}

// TestPanelCookiePath_IsNarrowerThanTheTapSurface is the cheap, direct statement
// of the same invariant, so a reader does not have to run a browser to see it.
//
// It is NOT a comparison of the constant with itself: it asserts a RELATION
// between adminauth.CookiePath and the paths the employee flow actually serves.
// "/" fails it, "/admin" passes, and any future widening that swallows a tap route
// fails it too.
func TestPanelCookiePath_IsNarrowerThanTheTapSurface(t *testing.T) {
	if adminauth.CookiePath == "/" {
		t.Fatalf("adminauth.CookiePath is %q — a root-scoped panel cookie is sent to every "+
			"employee route", adminauth.CookiePath)
	}
	if !strings.HasPrefix(adminauth.CookiePath, "/") || len(adminauth.CookiePath) < 2 {
		t.Fatalf("adminauth.CookiePath %q is not a usable path prefix", adminauth.CookiePath)
	}
	for _, p := range tapSurfacePaths {
		// http.CookieJar's rule (RFC 6265 5.1.4): a cookie with Path=P is sent to
		// request path R when R == P, or R starts with P and P ends in '/', or R
		// starts with P + '/'. This mirrors that test rather than restating "/admin".
		if p == adminauth.CookiePath ||
			strings.HasPrefix(p, strings.TrimSuffix(adminauth.CookiePath, "/")+"/") {
			t.Fatalf("the tap route %s is inside the panel cookie path %s — panel cookies would be "+
				"sent to it", p, adminauth.CookiePath)
		}
	}
}

// httpGet is a small adapter so the cookie-path harness can reuse htmlOf, which
// takes a recorder. It replays the response into one.
func httpGet(t *testing.T, p *pathHarness, path string) *httptest.ResponseRecorder {
	t.Helper()
	res, err := p.client.Get(p.server.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	rec := httptest.NewRecorder()
	rec.Code = res.StatusCode
	for k, vs := range res.Header {
		for _, v := range vs {
			rec.Header().Add(k, v)
		}
	}
	rec.Body.WriteString(readAll(t, res))
	return rec
}
