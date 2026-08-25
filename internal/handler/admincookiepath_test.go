package handler

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/web/templates/pages"
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
//
// 🔴 IT RESOLVES NAMED CONSTANTS AS WELL AS LITERALS, AND THAT IS A REPAIR RATHER
// THAN A FEATURE (2026-08-24, M8-05 FAZ B2c-2b). The comment here already named the
// limit — "this sees route LITERALS, so a route registered through a variable would
// be missed" — and the limit went live the moment tap.go's `r.Get("/t", …)` became
// `r.Get(TapPath, …)`, which it had to, because the encode relay burns
// TAPPA_BASE_URL + that path into a PHYSICAL PLAQUE and two spellings of it is a
// plaque swap in the field. MEASURED, and the failure was worse than one red test:
//
//	TestEmployeeRoutes_DerivationIsNotVacuous  -> "missing /t"
//	TestPanelCookies_NeverReachTheTapSurface   -> control saw 404 at /t
//
// The second is the one that matters. newPathHarness MOUNTS the derived list, so a
// route falling out of the derivation did not merely go unchecked — it vanished from
// the harness, and the positive-control loop that exists to catch exactly that ranged
// over the SAME shrunken list and passed. A floor (knownEmployeeRoutes) caught it;
// without the floor the whole file would have gone quietly vacuous.
//
// ✅ AND IT WIDENED THE NET, SUBSTANTIALLY. Before the repair the derivation found SIX
// paths. Three passes were added — named constants, selector-driven tables, and the
// internal/httpx root router — and every route they brought in was one no cookie
// assertion had ever covered.
//
// 🔴 NO ARITHMETIC IS WRITTEN OUT HERE ANY MORE, AND THAT IS THE RULE RATHER THAN A
// RETREAT. Two consecutive versions of this paragraph published a breakdown and both
// were wrong: "six … now twelve" was short by four (written before the selector pass
// existed), and its replacement said "6 + 7 + 4 = 16" — which does not add up, and does
// not add up because `/` is a LITERAL (marketing.go) and was inside the original six.
// A tally in a comment about a set the code derives is the second-representation defect
// this file's own header is written about, and it has now drifted three times in three
// rounds. The live number is printed by TestEmployeeRoutes_DerivationIsNotVacuous on
// every run; that is the only place it belongs.
//
// 🔴 SELECTOR-NAMED ROUTES ARE COUNTED TOO, AND THE SENTENCE THAT USED TO STAND HERE
// WAS FALSE (corrected 2026-08-24, second audit of FAZ B2c-2b). It said: "a route
// named by a SELECTOR (dashboard.go's `r.Get(s.Href, …)`) is still invisible. THOSE
// ARE PANEL ROUTES and would be filtered out anyway." Measured — they are not:
//
//	marketing.go:223-224   r.Get(page.Path, serve) / r.Head(page.Path, serve)
//	                       over pages.LegalPages -> /legal/privacy, /legal/terms,
//	                       /legal/cookies, /legal/imprint
//
// Four PUBLIC routes, none under /admin, none in the derived list, and none in the
// harness this list mounts. Worse than the miss: unresolvedRouteNames was pinned EMPTY,
// so the file was asserting "nothing is out of scope" while four routes were. That is
// this file's own rule broken by this file — "silence is the one answer a derivation
// may never give" — in the very mechanism the previous round claimed to have repaired.
//
// THE SHAPE OF THE REPAIR: selector registrations are collected as EXPRESSIONS
// (`s.Href`, `page.Path`), the set is pinned by the test, and each entry is either
//
//	LICENSED-EXCLUDED  the table it walks is provably under adminauth.CookiePath, and
//	                   the test asserts that at run time over the real table, or
//	INCLUDED           the table's paths are added to the derived list.
//
// So a new selector-driven mount is a red test rather than a silent gap, and the
// answer it forces is a question about a TABLE (finite, enumerable at run time) rather
// than about a spelling.
//
// ⚠️ WHAT IS STILL NOT SEEN, counted honestly: a route path computed at run time from
// something that is not a package-level table — a function call, a config value, a
// database row. Nothing does that; if something starts to, its selector will appear in
// the pinned set below with no table to point at, which is the moment to decide.
// An unresolvable BARE identifier is COLLECTED rather than skipped, and a panic at
// package init was tried and rejected as too blunt (it kills every test in the
// package, including the one that would explain why).
func employeeRoutes() []string {
	files, err := filepath.Glob("*.go")
	if err != nil {
		panic("handler: globbing package sources: " + err.Error())
	}
	// 🔴 internal/httpx IS READ TOO, AND ITS ABSENCE WAS A COUNTED HOLE (audit, third
	// round). router.go registers `/static/*` and `/healthz` on the ROOT router — public,
	// outside adminauth.CookiePath, and invisible to a glob of this package. The net was
	// asserting "no panel cookie reaches an employee-facing route" while two such routes
	// were not in the list. Harmless today for the same reason /legal/* was harmless —
	// the cookie is Path=/admin — but that is the argument the net exists to PROVE
	// rather than to assume, and it was the wrong argument once already.
	more, err := filepath.Glob(filepath.Join("..", "httpx", "*.go"))
	if err != nil {
		panic("handler: globbing internal/httpx: " + err.Error())
	}
	files = append(files, more...)

	type source struct {
		name string
		text string
	}
	var sources []source
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			panic("handler: reading " + f + ": " + err.Error())
		}
		sources = append(sources, source{name: filepath.Base(f), text: string(b)})
	}

	// Pass one: every package-level `name = "/…"`, so a route registered by name can
	// be resolved back to the path it holds.
	constRe := regexp.MustCompile(`(?m)^\s*(?:const\s+)?([A-Za-z_]\w*)(?:\s+string)?\s*=\s*"(/[^"]*)"`)
	paths := map[string]string{}
	for _, src := range sources {
		for _, m := range constRe.FindAllStringSubmatch(src.text, -1) {
			paths[m[1]] = m[2]
		}
	}
	// Pass two: names ROOTED IN THE PANEL'S SECTION TABLE. mustSectionHref reads
	// pages.PanelSections and panics for a tab that is not there, and every entry in
	// that table is under adminauth.CookiePath — a fact
	// TestEmployeeRoutes_DerivationIsNotVacuous asserts at run time rather than
	// assuming. So a name built from one can never be an employee-facing route, and it
	// is EXCLUDED rather than reported as unresolvable.
	sectionRe := regexp.MustCompile(`(?m)^\s*(?:const\s+|var\s+)?([A-Za-z_]\w*)\s*=\s*mustSectionHref\(`)
	panelRooted := map[string]bool{}
	for _, src := range sources {
		for _, m := range sectionRe.FindAllStringSubmatch(src.text, -1) {
			panelRooted[m[1]] = true
		}
	}
	// Pass three: `name = otherName + "suffix"`. It is how the CSV exports and every
	// panel write route are built, and the entries live inside `var (…)` blocks, so the
	// keyword is optional here exactly as it is above. Run to a fixed point so a chain
	// of any length resolves; propagate BOTH the literal path and the panel-rooted flag.
	chainRe := regexp.MustCompile(`(?m)^\s*(?:const\s+|var\s+)?([A-Za-z_]\w*)\s*=\s*([A-Za-z_]\w*)\s*\+\s*"([^"]*)"`)
	for again := true; again; {
		again = false
		for _, src := range sources {
			for _, m := range chainRe.FindAllStringSubmatch(src.text, -1) {
				if _, done := paths[m[1]]; done {
					continue
				}
				if base, ok := paths[m[2]]; ok {
					paths[m[1]] = base + m[3]
					again = true
					continue
				}
				if panelRooted[m[2]] && !panelRooted[m[1]] {
					panelRooted[m[1]] = true
					again = true
				}
			}
		}
	}

	litRe := regexp.MustCompile(`\br\.(?:Get|Post|Put|Delete|Patch|Head|Handle)\("(/[^"]*)"`)
	nameRe := regexp.MustCompile(`\br\.(?:Get|Post|Put|Delete|Patch|Head|Handle)\(([A-Za-z_]\w*)\s*,`)
	// Pass four: SELECTOR-named routes (`r.Get(page.Path, …)`). They are collected as
	// expressions and resolved against the run-time tables below, never guessed.
	selectorRe := regexp.MustCompile(`\br\.(?:Get|Post|Put|Delete|Patch|Head|Handle)\(([A-Za-z_]\w*\.[A-Za-z_]\w*)\s*,`)

	seen := map[string]bool{}
	var out []string
	add := func(route string) {
		if route == adminauth.CookiePath || strings.HasPrefix(route, adminauth.CookiePath+"/") {
			return // the panel's own routes
		}
		if !seen[route] {
			seen[route] = true
			out = append(out, route)
		}
	}
	for _, src := range sources {
		for _, m := range litRe.FindAllStringSubmatch(src.text, -1) {
			add(m[1])
		}
		for _, m := range nameRe.FindAllStringSubmatch(src.text, -1) {
			if route, ok := paths[m[1]]; ok {
				add(route)
				continue
			}
			if panelRooted[m[1]] {
				continue // provably under adminauth.CookiePath — see pass two
			}
			if !seenUnresolved[m[1]] {
				seenUnresolved[m[1]] = true
				unresolvedRouteNames = append(unresolvedRouteNames, m[1])
			}
		}
		for _, m := range selectorRe.FindAllStringSubmatch(src.text, -1) {
			// 🔴 KEYED BY THE TABLE THE MOUNT WALKS, AND THE TWO WEAKER KEYS WERE EACH
			// BEATEN BY A MUTATION. Keying on the EXPRESSION alone collided the moment two
			// files used the same loop variable (`s.Href`); keying on FILE + EXPRESSION
			// collided the moment ONE FILE had two mounts with the same variable name —
			// an audit added a second `page.Path` range over a different table in
			// marketing.go, registered two public routes, and the pin stayed GREEN. The
			// pin is about WHICH MOUNT WALKS WHICH TABLE, so the table is what it records.
			key := src.name + ":" + m[1] + " over " + rangeTableFor(src.text, m[0])
			if !seenSelector[key] {
				seenSelector[key] = true
				selectorRouteExprs = append(selectorRouteExprs, key)
			}
		}
	}

	// The tables those selectors walk, resolved at RUN TIME from the real values
	// rather than guessed from the source. pages.PanelSections is licensed-excluded
	// (asserted under adminauth.CookiePath by the test); pages.LegalPages is PUBLIC and
	// is therefore INCLUDED — the four routes an audit found missing.
	for _, p := range pages.LegalPages {
		add(p.Path)
	}

	sort.Strings(out)
	sort.Strings(unresolvedRouteNames)
	sort.Strings(selectorRouteExprs)
	return out
}

// unresolvedRouteNames records every route registered under a BARE NAME this scan
// could not turn into a path; selectorRouteExprs records every route registered under
// a SELECTOR.
//
// 🔴 THEY ARE TWO SETS BECAUSE THEY FAIL DIFFERENTLY, AND CONFLATING THEM IS WHAT AN
// AUDIT CAUGHT. A bare name is resolvable in principle (a constant somewhere); a
// selector names a FIELD OF A TABLE and is only answerable by looking at the table. The
// first version of this file had only the first set, pinned EMPTY — which read as
// "nothing is out of scope" while four /legal/* routes were.
//
// 🔴 COUNTED RATHER THAN IGNORED, AND RATHER THAN FATAL. Silently skipping is what let
// /t fall out of the net; panicking at package init was tried and is too blunt — it
// kills every test in the package, including the ones that would say why. So both sets
// are recorded and TestEmployeeRoutes_DerivationIsNotVacuous pins both.
var (
	unresolvedRouteNames []string
	seenUnresolved       = map[string]bool{}
	selectorRouteExprs   []string
	seenSelector         = map[string]bool{}
)

// rangeTableFor names the table a selector-driven mount walks: the nearest enclosing
// `for … range <TABLE>` above the registration.
//
// It is a backwards text scan rather than a syntax walk because everything around it
// is one, and the answer it needs is textual: the identifier being ranged over. A
// registration with no range above it answers "(no enclosing range)", which is
// reported in the pinned set rather than swallowed — a mount whose table this cannot
// name is exactly the case a human should look at.
func rangeTableFor(src, registration string) string {
	at := strings.Index(src, registration)
	if at < 0 {
		return "(not found)"
	}
	re := regexp.MustCompile(`for\s+[^\n]*range\s+([A-Za-z_][\w.]*)`)
	best := "(no enclosing range)"
	for _, loc := range re.FindAllStringSubmatchIndex(src[:at], -1) {
		best = src[loc[2]:loc[3]] // the last range that opens before the registration
	}
	return best
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
	// 🔴 THE COUNTED GAP, PINNED. Every route name this scan could not resolve is
	// listed here with the reason it is safe to leave out. A NEW name turns this red,
	// and the question it asks is the one that matters: does that route sit outside
	// adminauth.CookiePath? If it does, it belongs in the cookie net and this
	// derivation has to learn how to resolve it.
	//
	// 🔴 THE EXCLUSION FOR PANEL-ROOTED NAMES IS LICENSED HERE, AT RUN TIME, RATHER
	// THAN ASSUMED IN THE SCANNER. The derivation drops any route whose name chains
	// back to mustSectionHref; that is only sound because every section href really is
	// under the panel's cookie path. Asserting it means the day somebody adds a section
	// outside /admin, this test says so instead of the route quietly leaving the net.
	for _, s := range pages.PanelSections {
		if s.Href != adminauth.CookiePath && !strings.HasPrefix(s.Href, adminauth.CookiePath+"/") {
			t.Fatalf("panel section %q is at %q, outside %q. employeeRoutes() EXCLUDES every "+
				"route whose name chains back to mustSectionHref on the grounds that it cannot "+
				"be employee-facing; this section breaks that", s.Tab, s.Href, adminauth.CookiePath)
		}
	}

	var wantUnresolved []string
	if !reflect.DeepEqual(unresolvedRouteNames, wantUnresolved) {
		t.Fatalf("route names this scan cannot resolve = %v, want %v.\n"+
			"A new one is not automatically a defect — but it IS a route outside the "+
			"cookie net until somebody says why it cannot be employee-facing",
			unresolvedRouteNames, wantUnresolved)
	}

	// 🔴 THE SELECTOR SET, PINNED WITH A REASON PER ENTRY — AND ITS FIRST VERSION WAS
	// THE DEFECT AN AUDIT FOUND. Until 2026-08-24 selector-named routes were not
	// collected at all and the comment above claimed they were all panel routes; four
	// public /legal/* routes were neither derived nor mounted, while
	// unresolvedRouteNames sat pinned EMPTY saying nothing was out of scope.
	wantSelectors := []string{
		// dashboard.go's mountSections, over pages.PanelSections. LICENSED-EXCLUDED by
		// the run-time assertion above: every entry is under adminauth.CookiePath.
		"dashboard.go:s.Href over pages.PanelSections",
		// marketing.go's Mount, over pages.LegalPages. PUBLIC, so employeeRoutes() adds
		// every path in that table — asserted below rather than assumed.
		"marketing.go:page.Path over pages.LegalPages",
	}
	if !reflect.DeepEqual(selectorRouteExprs, wantSelectors) {
		t.Fatalf("routes registered under a SELECTOR = %v, want %v.\n"+
			"Each entry must be answered against the TABLE it walks: either that table is "+
			"provably under %q (exclude it, and assert that here) or it is public (add its "+
			"paths to the derivation). A new selector with neither is a route outside the net",
			selectorRouteExprs, wantSelectors, adminauth.CookiePath)
	}

	// 🔴 AND THE PUBLIC TABLE IS ASSERTED BOTH WAYS. Every legal page must be in the
	// derived list (so the harness mounts it and the cookie assertions cover it), and
	// none of them may be under the panel's cookie path — if one ever is, the inclusion
	// above becomes wrong rather than merely unnecessary.
	if len(pages.LegalPages) == 0 {
		t.Fatalf("pages.LegalPages is empty; the inclusion below proves nothing")
	}
	for _, p := range pages.LegalPages {
		if strings.HasPrefix(p.Path, adminauth.CookiePath) {
			t.Fatalf("legal page %q is under %q; it is added to the derived list as a PUBLIC "+
				"route and that premise no longer holds", p.Path, adminauth.CookiePath)
		}
		if !contains(got, p.Path) {
			t.Fatalf("legal page %q is mounted by marketing.go but is not in the derived list "+
				"(%v). It is a PUBLIC route, so the panel cookie must be proven absent from it",
				p.Path, got)
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
	h, err := NewAdminAuth(admins, &fakeTrail{}, fake, fake, &fakeReviewer{}, &fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(), newFakeTexts(), newFakeAccount(), nil, cfg, discardLogger())
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
