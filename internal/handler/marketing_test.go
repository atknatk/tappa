package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/web/templates/components"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The PUBLIC surface's tests (M7-01).
//
// 🔴 WHAT THESE ARE FOR IS NOT THE USUAL THING. Nothing on this surface can lose a
// record or leak a tenant's rows — it holds no database and no cookie codec. What a
// marketing page CAN do, and what this repository has repeatedly shipped, is state
// a guarantee the product does not keep. So most of what follows measures COPY
// against CODE: the price against the migration that charges it, the free months
// against the function that grants them, the cookie table against the constants
// that write the cookies, and every link against the router that has to serve it.
//
// Every scanner below has a negative control. A regexp that matched nothing would
// leave the whole file green over a page full of invented claims, and the failure
// would look exactly like success.

// marketingRouter is the surface as it is actually served: the real router, with
// the real global middleware, and no panel or tap feature mounted.
//
// cfg is nil on purpose. httpx.NewRouter tolerates it (it reads only
// TrustedProxies), and passing nil is the strongest available statement that this
// feature reads no configuration — if it ever starts to, this stops compiling or
// starts panicking rather than quietly picking up a default.
// marketingRouter is the REAL router with the public surface on it.
//
// ✅ IT MOUNTS THE SIGN-UP WIZARD AS WELL AS OF M7-02, and that is what keeps
// TestMarketing_EveryInternalLinkResolves meaningful rather than merely green: the
// landing page's "Start free" button now points at signupPath, and a router without
// the wizard would answer 404 there. M7-01's card named this as one of the two
// things M7-02 owed ("rotayı mount etmek ve handler.Marketing.signupHref'i
// doldurmak"), and the link test is the mechanism that would have caught either half
// being skipped.
//
// The provisioner is a stub because NOTHING ON THE MARKETING SURFACE POSTS. These
// tests drive GETs; the wizard's own behaviour is driven in signup_test.go against
// the same real handler.
func marketingRouter(t *testing.T) http.Handler {
	t.Helper()
	// 🔴 NIL TEXTS, WHICH IS THE STATE THE PRODUCT SHIPS IN AND THE STATE EVERY
	// ASSERTION IN THIS FILE IS ABOUT (M7-06). Nothing has been published, so all four
	// legal pages render their skeleton and their "not in force" block — exactly what
	// M7-01 built and what these tests were written against. A helper that quietly
	// pre-published four documents would have silently deleted the skeleton coverage.
	// The published case has its own router below.
	return marketingRouterWithTexts(t, nil)
}

// marketingRouterWithTexts is marketingRouter with a legal-document snapshot.
func marketingRouterWithTexts(t *testing.T, texts legalReader) http.Handler {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	wizard, err := NewSignup(&fakeProvisioner{}, nil, signupTestConfig(), log)
	if err != nil {
		t.Fatalf("NewSignup: %v", err)
	}
	return httpx.NewRouter(nil, nil, NewMarketing(texts, log), wizard)
}

// marketingURLs is every URL this feature serves, derived from the same table the
// footer and Mount both read.
func marketingURLs() []string {
	out := []string{"/"}
	for _, p := range pages.LegalPages {
		out = append(out, p.Path)
	}
	return out
}

// fetchMarketing performs one GET against the real router.
func fetchMarketing(t *testing.T, r http.Handler, url string, decorate func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	if decorate != nil {
		decorate(req)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// mustFetchMarketing is fetchMarketing with the status asserted, since a 404 body
// would satisfy most of the scans below by being empty.
func mustFetchMarketing(t *testing.T, r http.Handler, url string) string {
	t.Helper()
	rec := fetchMarketing(t, r, url, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", url, rec.Code)
	}
	return rec.Body.String()
}

// repoFile reads a file from the repository root.
func repoFile(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(append([]string{"..", ".."}, parts...)...)
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("reading %s: %v", p, err)
	}
	return string(raw)
}

// --- the shape of the feature ----------------------------------------------

// TestMarketing_HandlerHoldsNoStatefulDependency is the reflection half of the
// argument on the Marketing type: the panel's protections do not apply here, and
// what replaces them is that there is nothing to protect.
//
// 🔴 IT ASSERTS THE FIELD SET RATHER THAN A BEHAVIOUR, on purpose. A test that
// drove the handlers and observed no query would pass on a handler that holds a
// pool and simply did not use it on the path the test took. A type with no pool
// FIELD cannot make a query on any path, and adding one is what this goes red on.
func TestMarketing_HandlerHoldsNoStatefulDependency(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeOf(Marketing{})
	// Only these two kinds may appear: a plain string (a path this feature links
	// to) and the logger. Anything else -- a pool, a manager, a limiter, a cookie
	// codec -- is a capability this surface argued it does not have.
	//
	// ⚠️ []string USED TO BE ON THIS LIST AND NOTHING ON THE TYPE NEEDED IT. It was
	// pre-approval for a field nobody had asked for, in the one test whose entire
	// purpose is that a dependency must arrive with its own argument. Removed: if a
	// slice is ever genuinely needed here, adding it to this list is the argument.
	allowed := map[string]bool{
		"string":       true,
		"*slog.Logger": true,
		// 🔴 handler.legalReader, ADDED BY M7-06, AND THIS LINE IS THE ARGUMENT THE
		// COMMENT ABOVE DEMANDS. The four legal documents are published from the panel
		// into legal_documents (migration 00020) and this surface has to render them,
		// so it needs SOMETHING. What it was given is an interface with one method that
		// takes no context.Context and returns no error, reading an in-memory snapshot;
		// TestLegalReader_CannotReachTheDatabase asserts that signature, which is what
		// keeps "it touches no pool" true and therefore keeps the rate-limit reasoning
		// on this type intact. A *db.DB here would have retired that argument and put an
		// unmetered, unauthenticated path onto the pool check-in shares.
		"handler.legalReader": true,
	}
	if typ.NumField() == 0 {
		t.Fatal("handler.Marketing has no fields at all; this test would pass over anything")
	}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !allowed[f.Type.String()] {
			t.Errorf("handler.Marketing.%s is a %s.\n"+
				"This surface has no identity, so none of the panel's protections apply to "+
				"it; what stands in for them is that the type cannot reach a database, a "+
				"cookie or an audit sink because it has no field to reach one through. A "+
				"dependency here has to come with its own argument -- see the comment on "+
				"the type, and the rate-limit reasoning that depends on this being true.",
				f.Name, f.Type)
		}
	}
}

// TestMarketing_TheSameBytesForEveryVisitor is the PREMISE of the cacheable
// response, measured rather than asserted in a comment.
//
// The panel sends Cache-Control: no-store because its bodies carry an operator's
// name and a synchronizer token. This surface sends `public` instead, and that is
// only safe if nothing in a body belongs to one visitor. So the same URLs are
// driven with different cookies, different client addresses and different
// forwarding headers, and the bodies must be byte-identical.
func TestMarketing_TheSameBytesForEveryVisitor(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	for _, url := range marketingURLs() {
		plain := fetchMarketing(t, r, url, nil)
		loaded := fetchMarketing(t, r, url, func(req *http.Request) {
			req.AddCookie(&http.Cookie{Name: adminauthCookieNameForTest, Value: "not-a-real-token"})
			req.AddCookie(&http.Cookie{Name: "tappa_session", Value: "also-not-real"})
			req.Header.Set("X-Forwarded-For", "203.0.113.9")
			req.Header.Set("Accept-Language", "mt-MT")
			req.RemoteAddr = "198.51.100.4:44444"
		})
		if plain.Code != http.StatusOK || loaded.Code != http.StatusOK {
			t.Fatalf("GET %s = %d and %d, want 200 twice", url, plain.Code, loaded.Code)
		}
		if plain.Body.String() != loaded.Body.String() {
			t.Errorf("GET %s answers different bytes to two visitors. The Cache-Control on "+
				"this surface is `public`, which is only correct while nothing in a body "+
				"belongs to one caller. Either the difference is a mistake, or this page "+
				"must stop being publicly cacheable.", url)
		}
	}
}

// adminauthCookieNameForTest is a literal on purpose: the point of the test above
// is to send cookies the surface has no business reading, so it must not be tied
// to the constant the product uses.
const adminauthCookieNameForTest = "tappa_admin_session"

// TestMarketing_IsCacheableAndCarriesNoPanelHeaders is the Cache-Control decision.
//
// ⚠️ THE PANEL'S HEADER BLOCK WAS NOT COPIED WHOLE, and this is the half of that
// decision a test can hold. Cache-Control: no-store belongs to a surface that
// renders somebody's name; bringing it here would have cost every visitor a full
// render on every view for no protection at all.
func TestMarketing_IsCacheableAndCarriesNoPanelHeaders(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	for _, url := range marketingURLs() {
		rec := fetchMarketing(t, r, url, nil)
		if got := rec.Header().Get("Cache-Control"); got != marketingCacheControl {
			t.Errorf("GET %s Cache-Control = %q, want %q", url, got, marketingCacheControl)
		}
		if strings.Contains(rec.Header().Get("Cache-Control"), "no-store") {
			t.Errorf("GET %s sends no-store. That is the PANEL's header and it is here by "+
				"copying rather than by argument: a page with nothing per-visitor in it "+
				"gains nothing from being uncacheable.", url)
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("GET %s X-Content-Type-Options = %q, want nosniff", url, got)
		}
		if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
			t.Errorf("GET %s Content-Type = %q, want text/html", url, got)
		}
	}
}

// TestMarketing_PolicyNamesWhatThePageLoadsAndNothingElse checks the policy
// DIRECTIVE BY DIRECTIVE rather than comparing it with the panel's.
//
// 🔴 COMPARING THE TWO STRINGS WOULD BE THE WRONG TEST even though they are equal
// today. It would turn a widening of the PANEL's policy into a silent widening of
// the public one, and it would make an intentional divergence look like a
// regression. What is actually required of this surface is that every directive is
// justified by something the page loads, and that nothing it does not load is
// named.
func TestMarketing_PolicyNamesWhatThePageLoadsAndNothingElse(t *testing.T) {
	t.Parallel()
	want := map[string]string{
		"default-src":     "'none'",
		"style-src":       "'self'",
		"font-src":        "'self'",
		"form-action":     "'self'",
		"base-uri":        "'none'",
		"frame-ancestors": "'none'",
	}
	got := map[string]string{}
	for _, d := range strings.Split(marketingCSP, ";") {
		fields := strings.Fields(strings.TrimSpace(d))
		if len(fields) == 0 {
			continue
		}
		got[fields[0]] = strings.Join(fields[1:], " ")
	}
	for name, value := range want {
		if got[name] != value {
			t.Errorf("marketingCSP %s = %q, want %q", name, got[name], value)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("marketingCSP names %s, which nothing on this surface loads. A directive "+
				"added for something that is not there makes the next addition a silent "+
				"inheritance instead of a visible edit.", name)
		}
	}

	// AND THE PAGES AGREE WITH IT. A policy that forbids scripts on a page that
	// loads one is a broken page, not a strict one -- so the correspondence is
	// measured in both directions.
	r := marketingRouter(t)
	for _, url := range marketingURLs() {
		rec := fetchMarketing(t, r, url, nil)
		if header := rec.Header().Get("Content-Security-Policy"); header != marketingCSP {
			t.Errorf("GET %s policy = %q, want the marketing policy", url, header)
		}
		body := rec.Body.String()
		for _, forbidden := range []string{"<script", "<img", "<iframe", "<object", "<embed"} {
			if strings.Contains(strings.ToLower(body), forbidden) {
				t.Errorf("GET %s renders %s, which the policy does not permit. Either the "+
					"element goes or the directive it needs has to be argued for.", url, forbidden)
			}
		}
	}
}

// TestMarketing_AnswersHeadWithNoBodyAndTheSameHeaders.
//
// 🔴 IT DRIVES A REAL SERVER, NOT A RECORDER, AND THAT IS THE WHOLE POINT OF THE
// TEST. httptest.ResponseRecorder does not implement the body suppression that
// net/http performs for a HEAD response, so the same assertion written against a
// recorder would report a body on the wire that a real client never receives — it
// would look like a defect that is not there, or hide one that is.
//
// WHY HEAD AT ALL: measured before it was added — HEAD / answered 405 with
// Allow: GET, as every chi r.Get route in this product does. Crawlers, uptime
// monitors and link checkers HEAD the site root before they GET it, and this is the
// first surface in the product any of them reaches.
func TestMarketing_AnswersHeadWithNoBodyAndTheSameHeaders(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(marketingRouter(t))
	defer srv.Close()

	for _, path := range marketingURLs() {
		get, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, _ := io.ReadAll(get.Body)
		_ = get.Body.Close()

		req, err := http.NewRequestWithContext(t.Context(), http.MethodHead, srv.URL+path, nil)
		if err != nil {
			t.Fatalf("building HEAD %s: %v", path, err)
		}
		head, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("HEAD %s: %v", path, err)
		}
		hb, _ := io.ReadAll(head.Body)
		_ = head.Body.Close()

		if head.StatusCode != http.StatusOK {
			t.Errorf("HEAD %s = %d, want 200. It was 405 before M7-01 mounted the method, "+
				"which is what a link checker sees first.", path, head.StatusCode)
			continue
		}
		if len(hb) != 0 {
			t.Errorf("HEAD %s returned %d body bytes; a HEAD response carries none", path, len(hb))
		}
		if len(body) == 0 {
			t.Errorf("GET %s returned an empty body, so this comparison is vacuous", path)
		}
		// THE HEADERS A CACHE AND A CRAWLER ACT ON MUST MATCH, or HEAD becomes a
		// second, quieter answer that disagrees with the real one.
		for _, h := range []string{"Content-Type", "Cache-Control", "Content-Security-Policy", "X-Content-Type-Options"} {
			if g, hd := get.Header.Get(h), head.Header.Get(h); g != hd {
				t.Errorf("%s: GET sends %s=%q and HEAD sends %q", path, h, g, hd)
			}
		}
	}
}

// failingWriter is a ResponseWriter whose Write fails with a chosen error, which
// is how the two branches of render's error handling are driven without a real
// socket. A cancelled request against httptest does not necessarily fail a write,
// so triggering the branch directly is the only way to measure which severity it
// takes.
type failingWriter struct {
	http.ResponseWriter
	err error
}

func (f failingWriter) Write([]byte) (int, error) { return 0, f.err }

// TestMarketing_AVisitorLeavingIsNotAnError.
//
// 🔴 A SECURITY AUDIT MEASURED THIS AS AN UNBUDGETED WRITE INTO THE PROCESS LOG:
// 200 cancelled page loads produced 200 ERROR lines, where 2000 equivalent attempts
// against /activate produced 1, because that flow has a budget whose stated job is
// to bound the log. This surface has no budget by design, so the fix is at the
// source.
//
// ⚠️ AND THE REASON IS NOT THE ATTACK. An ordinary visitor on a slow connection who
// navigates away mid-render produces the identical line. It was never a report of a
// fault, and filing it at the severity reserved for faults is how a real render
// failure stops being findable.
func TestMarketing_AVisitorLeavingIsNotAnError(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		err       error
		wantLevel slog.Level
	}{
		{"a visitor who navigated away", context.Canceled, slog.LevelDebug},
		{"a wrapped cancellation", fmt.Errorf("writing the page: %w", context.Canceled), slog.LevelDebug},
		{"a genuine failure this server caused", errors.New("template blew up"), slog.LevelError},
	} {
		var buf bytes.Buffer
		m := NewMarketing(nil, slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
		rec := httptest.NewRecorder()
		m.render(failingWriter{ResponseWriter: rec, err: tc.err},
			httptest.NewRequest(http.MethodGet, "/", nil), pages.Landing(pages.LandingView{}))

		out := buf.String()
		gotError := strings.Contains(out, "level=ERROR")
		wantError := tc.wantLevel == slog.LevelError
		if gotError != wantError {
			t.Errorf("%s: logged at ERROR = %v, want %v.\nGot: %s", tc.name, gotError, wantError, out)
		}
		// NEITHER BRANCH IS SILENT. §7 forbids swallowing; what changed is severity.
		if strings.TrimSpace(out) == "" {
			t.Errorf("%s: nothing was logged at all. A failed render must never be "+
				"swallowed — the distinction being made here is severity, not silence.", tc.name)
		}
	}
}

// --- nothing leaves this origin ---------------------------------------------

// externalReference matches a reference to somewhere that is not this origin: an
// absolute URL in any attribute or stylesheet function, and a protocol-relative
// one. mailto: and tel: are NOT matched -- they open a mail client rather than
// making a request -- but the surface carries neither today.
var externalReference = regexp.MustCompile(
	`(?i)(?:href|src|srcset|action|data|poster|content)\s*=\s*["']\s*(?:[a-z][a-z0-9+.-]*:)?//|` +
		`url\(\s*["']?\s*(?:[a-z][a-z0-9+.-]*:)?//|` +
		`@import\s+["']?\s*(?:[a-z][a-z0-9+.-]*:)?//|` +
		`rel\s*=\s*["'](?:preconnect|dns-prefetch|prefetch|preload|prerender)`)

// TestMarketing_ReachesNoThirdParty is the GDPR criterion on the M7-01 card,
// measured rather than reasoned about: the fonts have been self-hosted since M5-04
// (web/static/fonts, declared in input.css against our own origin), and this task's
// obligation was to not break that and to prove it.
//
// It scans for ANY absolute or protocol-relative reference, not just for a list of
// known font hosts: a page that contacts a third party is a page that hands that
// party the visitor's address, and which party it is does not change the answer.
func TestMarketing_ReachesNoThirdParty(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	for _, url := range marketingURLs() {
		body := mustFetchMarketing(t, r, url)
		if hit := externalReference.FindString(body); hit != "" {
			t.Errorf("GET %s reaches outside this origin: %q.\n"+
				"Every asset this product serves is embedded in the binary and served from "+
				"/static -- the brand faces included, since M5-04. A runtime request to a "+
				"third party hands that party the visitor's address and the page they were "+
				"reading.", url, hit)
		}
	}

	// AND THE STYLESHEET THE PAGES LOAD. The scan above cannot see a url() inside
	// app.css, which is where an @font-face pointing at a CDN would live.
	css := repoFile(t, "web", "static", "css", "input.css")
	if hit := externalReference.FindString(css); hit != "" {
		t.Errorf("input.css reaches outside this origin: %q. The six woff2 files are in "+
			"web/static/fonts with their sources, sizes and sha256 sums recorded beside "+
			"them; an @font-face with a remote url() is the red line those files exist "+
			"to keep.", hit)
	}

	// 🔴 AND EVERY OTHER TEMPLATE IN THE PRODUCT, BECAUSE THE SENTENCE ON
	// /legal/cookies SPEAKS PRODUCT-WIDE: "every stylesheet, typeface and script
	// THIS PRODUCT loads is served from this domain". That is true today — htmx is
	// vendored into web/static/vendor with its version and sha256, and the tap
	// page's script is ours — but until this loop the net only covered the five
	// MARKETING bodies. An audit named the consequence exactly: a CDN <script>
	// added to the transactions section would falsify a LEGAL PAGE with nothing
	// going red.
	//
	// THE CLAIM WAS WIDENED RATHER THAN THE SENTENCE NARROWED, which was the choice
	// offered and is the right way round for a legal disclosure: the promise is the
	// one worth keeping, so the net is made to cover it.
	//
	// It reads the .templ SOURCES rather than rendering every screen, and that is
	// what makes it affordable — the panel and tap screens need a database, a
	// session and a tenant to render, and a net that only covers the screens a test
	// can cheaply drive is the net that missed this. Measured before it was added:
	// zero absolute or protocol-relative references across every template, and zero
	// bare URLs in their comments, so it starts clean rather than with exemptions.
	templates := 0
	err := filepath.Walk(filepath.Join("..", "..", "web", "templates"), func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(p, ".templ") {
			return nil
		}
		raw, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		templates++
		if hit := externalReference.FindString(string(raw)); hit != "" {
			t.Errorf("%s reaches outside this origin: %q.\n"+
				"/legal/cookies tells visitors that every stylesheet, typeface and script "+
				"this product loads is served from this domain. That is a legal disclosure "+
				"about the WHOLE product, so one external reference on any screen makes the "+
				"page wrong — vendor the asset (web/static/vendor, with its source and "+
				"sha256) or change what the page says.", p, hit)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the templates: %v", err)
	}
	// ANTI-VACUITY: a walk that read nothing would clear the whole product.
	if templates < 10 {
		t.Fatalf("scanned %d template(s); this product has more, so the product-wide half of "+
			"this test is reading the wrong directory", templates)
	}
	t.Logf("templates scanned for external references: %d", templates)
}

// TestExternalReferenceScanner_CatchesWhatItExistsToCatch is the negative control.
// Without it a broken alternation would leave the test above green over a page
// full of CDN links.
func TestExternalReferenceScanner_CatchesWhatItExistsToCatch(t *testing.T) {
	t.Parallel()
	mustCatch := []string{
		`<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=X">`,
		`<link rel="stylesheet" href="//fonts.googleapis.com/css2">`,
		`<script src="https://cdn.example.com/a.js"></script>`,
		`<img src="http://tracker.example.net/pixel.gif">`,
		`<link rel="preconnect" href="https://fonts.gstatic.com">`,
		`@font-face { src: url('https://fonts.gstatic.com/s/a.woff2'); }`,
		`@import url("//example.com/x.css");`,
		`<form action="https://evil.example/collect">`,
	}
	for _, s := range mustCatch {
		if externalReference.FindString(s) == "" {
			t.Errorf("the scanner accepted %q, so it would accept it on the landing page too", s)
		}
	}
	mustPass := []string{
		`<link rel="stylesheet" href="/static/css/app.css">`,
		`<a href="/legal/privacy">Privacy</a>`,
		`<a href="#main">Skip to content</a>`,
		`src: url('/static/fonts/space-grotesk-v22-latin-wght.woff2') format('woff2');`,
	}
	for _, s := range mustPass {
		if hit := externalReference.FindString(s); hit != "" {
			t.Errorf("the scanner rejected %q as external (%q); it would fail on our own assets", s, hit)
		}
	}
}

// --- every link goes somewhere ----------------------------------------------

// The anchor scanner is employees_test.go's hrefRE, reused rather than repeated:
// it matches href only inside an <a>, which is what "a link" means here — the
// stylesheet's <link> is an asset, not a link a visitor follows.
//
// ✅ AND EXCLUDING IT IS NOT A GAP — MEASURED, because "covered somewhere else" is
// the kind of claim this file has had to retract before. There is exactly ONE
// <link> element in the whole product (layout/base.templ, inside documentHead),
// all three shells render that same component — shell, PanelWithScript and
// Marketing — so a marketing page structurally cannot carry a different
// stylesheet href than a panel page, and
// TestPanelStylesheet_IsVendoredAndServedFromOurOwnOrigin already asserts that
// href is present in the EMBEDDED static tree rather than merely prefixed with
// /static (M6-12 phase B, which shipped for exactly this trap: app.css is
// gitignored, so a binary can serve a page whose stylesheet 404s).
//
// ⚠️ THE RESIDUAL, STATED: a <link> added directly to layout.Marketing, bypassing
// documentHead, would be checked by neither — an EXTERNAL one is caught by
// TestMarketing_ReachesNoThirdParty below, so what could slip is a same-origin
// href pointing at nothing. Nothing renders such a link today.

// TestMarketing_EveryInternalLinkResolves follows every link on the public surface
// against the REAL router.
//
// 🔴 THIS IS THE TEST THE "Start free" BUTTON EXISTS UNDER. The sign-up wizard is
// M7-02 and is not mounted, so the landing page renders a sentence instead of a
// button. That is a copy decision anybody could undo by pasting an href, and this
// is what would catch it: a link to /signup answers 404 today and goes red here.
//
// ⚠️ ONE HREF IS NOT SERVED BY THIS ROUTER AND IS HANDLED SEPARATELY. The panel's
// sign-in page belongs to AdminAuth, which cannot be constructed without a
// database. It is admitted by CONSTANT rather than by string -- the landing view is
// given adminLoginPath, which is the same constant AdminAuth.Mount registers -- and
// the registration itself is asserted below so the constant cannot become a path
// nothing serves.
func TestMarketing_EveryInternalLinkResolves(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	seen := 0
	for _, url := range marketingURLs() {
		body := mustFetchMarketing(t, r, url)
		for _, m := range hrefRE.FindAllStringSubmatch(body, -1) {
			href := m[1]
			switch {
			case href == "" || strings.HasPrefix(href, "#"):
				continue
			case !strings.HasPrefix(href, "/"):
				t.Errorf("%s links to %q, which is neither an in-page anchor nor a path on "+
					"this site", url, href)
				continue
			case href == adminLoginPath:
				// Served by AdminAuth; see the assertion below.
				seen++
				continue
			}
			seen++
			if code := fetchMarketing(t, r, href, nil).Code; code != http.StatusOK {
				t.Errorf("%s links to %s, which answers %d. A dead link on the most public "+
					"URL in the product is a promise the router does not keep.", url, href, code)
			}
		}
	}
	// ANTI-VACUITY: a page whose links stopped rendering would pass everything above.
	if seen < len(pages.LegalPages)+1 {
		t.Fatalf("followed %d link(s) across the public surface; the footer alone carries %d, "+
			"so this scan is not reading the pages", seen, len(pages.LegalPages))
	}

	// The one href this router does not serve. adminLoginPath is shared at compile
	// time, so the LINK cannot drift from the constant; what could drift is the
	// constant no longer being registered as a route.
	src := repoFile(t, "internal", "handler", "adminlogin.go")
	if !strings.Contains(src, "r.Get(adminLoginPath,") {
		t.Errorf("adminlogin.go no longer registers a GET on adminLoginPath, so the landing " +
			"page's \"Sign in\" link points at a route nothing serves.")
	}
}

// TestMarketing_MountsEveryLegalDocumentAndLinksToIt is Q23's "footer'dan
// erişilebilir", in both directions: a document with a route and no footer link is
// unreachable, and a footer link with no route is a 404.
func TestMarketing_MountsEveryLegalDocumentAndLinksToIt(t *testing.T) {
	t.Parallel()
	if len(pages.LegalPages) != 4 {
		t.Fatalf("pages.LegalPages has %d entries; Q23 names four documents (privacy policy, "+
			"terms of service, company details, cookie notice)", len(pages.LegalPages))
	}
	r := marketingRouter(t)
	landing := mustFetchMarketing(t, r, "/")
	for _, p := range pages.LegalPages {
		if code := fetchMarketing(t, r, p.Path, nil).Code; code != http.StatusOK {
			t.Errorf("GET %s = %d; the footer links to it", p.Path, code)
		}
		if !strings.Contains(landing, `href="`+p.Path+`"`) {
			t.Errorf("the landing page's footer does not link to %s", p.Path)
		}
	}
}

// --- the numbers on the page match the numbers in the product ---------------

var (
	priceDefaultRE = regexp.MustCompile(`price_per_employee_month\s+numeric\(10,\s*2\)\s+NOT NULL\s+DEFAULT\s+([0-9.]+)`)
	freeMonthsRE   = regexp.MustCompile(`p_plan\s*=\s*'founding'\s+THEN\s+interval\s+'(\d+)\s+months?'`)
)

// TestLanding_PriceMatchesTheSchemaItIsCharged.
//
// 🔴 THE PAGE AND THE DATABASE CARRY THE SAME NUMBER IN TWO PLACES AND ONE OF THEM
// IS AN INVOICE. Migration 00016 makes 1.50 the DEFAULT of
// tenants.price_per_employee_month, so it is what a new tenant is created with; the
// landing page publishes the offer. If the two ever disagree, the first invoice
// contradicts the page that sold it, and both halves look right on their own --
// which is why this is a test rather than a comment.
func TestLanding_PriceMatchesTheSchemaItIsCharged(t *testing.T) {
	t.Parallel()
	mig := repoFile(t, "db", "migrations", "00016_add_billing_price_and_periods.sql")
	m := priceDefaultRE.FindStringSubmatch(mig)
	if m == nil {
		t.Fatal("migration 00016 no longer declares a DEFAULT for " +
			"tenants.price_per_employee_month in a shape this test can read. It is the price " +
			"a new tenant is created with; find it and compare it by hand before changing " +
			"this test.")
	}
	schema, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		t.Fatalf("migration 00016's default price %q is not a number", m[1])
	}
	published, err := strconv.ParseFloat(publishedPricePerEmployeeMonth, 64)
	if err != nil {
		t.Fatalf("publishedPricePerEmployeeMonth %q is not a number", publishedPricePerEmployeeMonth)
	}
	if schema != published {
		t.Errorf("the landing page publishes %s per employee per month; migration 00016 "+
			"creates a tenant at %s. One of them is wrong and a customer will find out "+
			"on their first invoice.", publishedPricePerEmployeeMonth, m[1])
	}

	body := mustFetchMarketing(t, marketingRouter(t), "/")
	if !strings.Contains(body, publishedPricePerEmployeeMonth) {
		t.Errorf("the landing page does not print the price at all; the pricing section is " +
			"required by the M7-01 card and by handoff §11")
	}
}

// TestLanding_FreeMonthsMatchTheFunctionThatGrantsThem.
//
// The founding offer is the one commercial promise on this page that the PRODUCT
// keeps rather than a human: tappa_first_chargeable_month adds three months for
// plan='founding', db/queries/billing.sql calls it and the panel's billing screen
// shows the result. The page may say "three months free" precisely because of that.
func TestLanding_FreeMonthsMatchTheFunctionThatGrantsThem(t *testing.T) {
	t.Parallel()
	mig := repoFile(t, "db", "migrations", "00016_add_billing_price_and_periods.sql")
	m := freeMonthsRE.FindStringSubmatch(mig)
	if m == nil {
		t.Fatal("tappa_first_chargeable_month no longer grants a founding tenant a whole " +
			"number of free months in a shape this test can read. The landing page " +
			"advertises that period; check it by hand before changing this test.")
	}
	granted, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("free months %q is not a number", m[1])
	}
	if granted != foundingFreeMonths {
		t.Errorf("the landing page offers %d free months; migration 00016 grants %d",
			foundingFreeMonths, granted)
	}
}

// --- the claims ---------------------------------------------------------------

// TestLanding_CarriesTheSlogan is the M7-01 card's first acceptance criterion.
func TestLanding_CarriesTheSlogan(t *testing.T) {
	t.Parallel()
	text := screenText(t, mustFetchMarketing(t, marketingRouter(t), "/"))
	const slogan = "No app. No device. No fingerprints. Just tap."
	if !strings.Contains(text, slogan) {
		t.Errorf("the landing page does not carry %q as readable text.\nGot: %s", slogan, text)
	}
}

// unmeasuredNumber matches the shapes of a claim nobody in this repository has
// measured: a percentage, a "9 out of 10", an uptime figure, a guarantee.
//
// It deliberately does NOT match every number. The page prints a price, a free
// period and a clock time, and all three are checked against the product
// elsewhere in this file.
var unmeasuredNumber = regexp.MustCompile(
	`(?i)\d+\s*%|\b\d+(?:\.\d+)?\s*(?:percent|x faster|x more)\b|` +
		`\b(?:99|100)(?:\.\d+)?\b|\bout of (?:ten|10)\b|\buptime\b|\bsla\b|` +
		`\bguarantee(?:d|s)?\b|\bnever fails?\b|\bzero errors?\b|\b100% accurate\b`)

// TestLanding_MakesNoUnmeasuredNumericClaim.
//
// 🔴 THE PAGE CARRIES NO ACCURACY FIGURE, NO UPTIME FIGURE AND NO GUARANTEE, and
// the reason is that nobody here has measured one. A number on a marketing page is
// read as a commitment; a number nobody produced is a commitment nobody can keep.
func TestLanding_MakesNoUnmeasuredNumericClaim(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	for _, url := range marketingURLs() {
		text := screenText(t, mustFetchMarketing(t, r, url))
		if hit := unmeasuredNumber.FindString(text); hit != "" {
			t.Errorf("%s claims %q. Nothing in this repository measures that. If a figure "+
				"is real, cite what produced it and pin it against that source the way "+
				"the price and the free period are pinned; otherwise it does not go on "+
				"the page.", url, hit)
		}
	}
}

// superiorityClaim matches a verdict about being better than something else, which
// is the shape the comparison table was written to avoid.
var superiorityClaim = regexp.MustCompile(
	`(?i)\b(?:safer|better|more accurate|more reliable|more secure|faster|cheaper|superior)\s+than\b|` +
		`\bthe (?:best|safest|most accurate|most reliable)\b|\bindustry[- ]leading\b|\bunbeatable\b`)

// TestLanding_ComparesMechanismsAndDeclaresNoWinner.
//
// The comparison table is the section this repository was most likely to get
// wrong. CLAUDE.md §4.1's ban is a FACT about Tappa and a perfectly good reason to
// buy it. "Safer than a fingerprint terminal" is a different sentence: it is a
// comparison, and nobody here has run one. The table
// compares mechanisms, and it says so on the page rather than only in a comment.
func TestLanding_ComparesMechanismsAndDeclaresNoWinner(t *testing.T) {
	t.Parallel()
	text := screenText(t, mustFetchMarketing(t, marketingRouter(t), "/"))
	if hit := superiorityClaim.FindString(text); hit != "" {
		t.Errorf("the landing page claims %q. That is a measurement nobody made. State what "+
			"Tappa does and let the reader compare.", hit)
	}
	const disclaimer = "It is not a benchmark"
	if !strings.Contains(text, disclaimer) {
		t.Errorf("the comparison table no longer carries its footnote (%q). Without it the "+
			"table reads as a performance claim, which is exactly what it is not.", disclaimer)
	}
	// ANTI-VACUITY: the table has to be there for the footnote to be about anything.
	if len(pages.LandingComparison) < 3 {
		t.Fatalf("pages.LandingComparison has %d rows; the section this footnote belongs to "+
			"is not being rendered", len(pages.LandingComparison))
	}
	for _, row := range pages.LandingComparison {
		if !strings.Contains(text, row.Aspect) {
			t.Errorf("comparison row %q is in the table and not on the page", row.Aspect)
		}
	}
}

// TestClaimScanners_RejectTheThingsTheyExistToReject is the negative control for
// the two scanners above.
func TestClaimScanners_RejectTheThingsTheyExistToReject(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"99.9% uptime", "accurate 100 times out of 100", "we guarantee your records",
		"3x faster than a terminal", "a 99 percent match rate", "backed by an SLA",
	} {
		if unmeasuredNumber.FindString(s) == "" {
			t.Errorf("the numeric-claim scanner accepted %q", s)
		}
	}
	for _, s := range []string{
		"safer than a fingerprint terminal", "more accurate than a punch clock",
		"the best time clock in Malta", "industry-leading attendance",
	} {
		if superiorityClaim.FindString(s) == "" {
			t.Errorf("the superiority scanner accepted %q", s)
		}
	}
	// And the things the page legitimately says must NOT be flagged, or the
	// scanners would force the copy to be wrong in the other direction.
	for _, s := range []string{
		"1.50 per employee per month", "3 months free", "14:03:22", "365 days",
		"nothing — no fingerprints, no face, no voice, no biometric data of any kind",
	} {
		if hit := unmeasuredNumber.FindString(s); hit != "" {
			t.Errorf("the numeric-claim scanner flagged %q (as %q), which the page legitimately says", s, hit)
		}
		if hit := superiorityClaim.FindString(s); hit != "" {
			t.Errorf("the superiority scanner flagged %q (as %q)", s, hit)
		}
	}
}

// TestLanding_OffersTheWizardItMounts.
//
// 🔴 THIS TEST WAS INVERTED BY M7-02 AND THAT IS THE POINT OF IT. Until this
// milestone it was TestLanding_OffersNoLinkToAWizardThatIsNotMounted: the sign-up
// flow did not exist, so the landing page said so in words and rendered no button,
// and this test held the page to that. M7-01's card named the two things that had to
// change together — "rotayı mount etmek VE handler.Marketing.signupHref'i doldurmak
// — ikisi de yapılmazsa ... kırmızıya döner" — and the inversion is what makes that
// prediction true rather than decorative.
//
// SO IT NOW ASSERTS BOTH HALVES AND THE ABSENCE OF THE OLD SENTENCE. Half a change
// is the failure this file exists to catch: a button pointing at an unmounted route
// is a 404 on the most public URL in the product, and a mounted route with the page
// still apologising for not having one is a feature nobody can reach.
func TestLanding_OffersTheWizardItMounts(t *testing.T) {
	t.Parallel()
	body := mustFetchMarketing(t, marketingRouter(t), "/")
	if !strings.Contains(body, `href="`+signupPath+`"`) {
		t.Errorf("the landing page carries no link to %s, so the wizard M7-02 mounted is "+
			"unreachable from the page that sells it", signupPath)
	}
	text := screenText(t, body)
	// THE APOLOGY MUST BE GONE. A page that offers the button AND still says
	// self-service sign-up is closed contradicts itself, and the sentence is the half
	// a careless edit leaves behind.
	if strings.Contains(text, "Self-service sign-up is not open yet") {
		t.Error("the landing page still says self-service sign-up is not open while linking " +
			"to the wizard; the two halves of M7-02 disagree on the same page")
	}

	// The OTHER branch is driven too, because a branch nothing renders is a branch
	// that stops compiling correctly without anybody noticing. It is what a
	// deployment that ever unmounts the wizard would fall back to.
	var sb strings.Builder
	v := pages.LandingView{
		SignupHref:            "",
		SignInHref:            adminLoginPath,
		PricePerEmployeeMonth: publishedPricePerEmployeeMonth,
		FreeMonths:            foundingFreeMonths,
	}
	if err := pages.Landing(v).Render(t.Context(), &sb); err != nil {
		t.Fatalf("rendering the landing page without a sign-up href: %v", err)
	}
	if strings.Contains(sb.String(), `href="`+signupPath+`"`) {
		t.Error("an empty LandingView.SignupHref still produces a link, so a deployment " +
			"without the wizard would ship a button that answers 404")
	}
}

// --- the two-shapes block rests on the product, not on memory -------------------

// createTableBlock returns the body of `CREATE TABLE <name> ( … );` out of a
// migration, so a claim about the LOCATIONS table cannot be satisfied by a column
// on the DEPARTMENTS table two definitions below it. Both live in migration 00002
// and both have a shift, which is exactly the confusion a whole-file grep makes.
// hasColumn reports whether a CREATE TABLE body declares `col` with a type
// matching typ (a regexp fragment). Package level so the negative control below
// can prove it says no to a column that is not there.
func hasColumn(block, col, typ string) bool {
	return regexp.MustCompile(`(?im)^\s*` + regexp.QuoteMeta(col) + `\s+` + typ).MatchString(block)
}

// fieldNames maps a struct's field names to their types. Same reason as above.
func fieldNames(v any) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	typ := reflect.TypeOf(v)
	for i := 0; i < typ.NumField(); i++ {
		out[typ.Field(i).Name] = typ.Field(i).Type
	}
	return out
}

// TestAnchorMechanisms_SayNoWhenTheCapabilityIsAbsent is the negative control for
// the two mechanisms every anchor is built from.
//
// 🔴 IT EXISTS BECAUSE TWO ANCHORS COULD NOT BE MUTATION-TESTED THE ORDINARY WAY.
// Removing ledger.Report.Venues or components.FilterBarView.DepartmentID does not
// produce a red test — it produces a BUILD FAILURE, because the report builder and
// the filter template use both. That is a STRONGER gate than a test and it was
// measured (the compiler names six use sites for Venues, one for DepartmentID),
// but it means the derivation itself never got to say no. So the mechanisms are
// exercised here against synthetic inputs instead: a scan that could not recognise
// an absent column, or an absent field, would let a future anchor pass over
// nothing while looking exactly like the ones that work.
func TestAnchorMechanisms_SayNoWhenTheCapabilityIsAbsent(t *testing.T) {
	t.Parallel()
	const block = `
    id          uuid PRIMARY KEY,
    tenant_id   uuid NOT NULL,
    shift_start time,
    static_ips  cidr[] NOT NULL DEFAULT '{}',
    overnight   boolean NOT NULL DEFAULT false,
`
	for _, tc := range []struct {
		col, typ string
		want     bool
	}{
		{"shift_start", "time", true},
		{"static_ips", `cidr\[\]`, true},
		{"overnight", "boolean", true},
		{"tenant_id", `uuid\s+NOT NULL`, true},
		{"shift_end", "time", false},     // absent column
		{"department_id", "uuid", false}, // absent column
		{"static_ips", `uuid`, false},    // present column, wrong type
		{"id", `uuid\s+NOT NULL`, false}, // present column, constraint not there
	} {
		if got := hasColumn(block, tc.col, tc.typ); got != tc.want {
			t.Errorf("hasColumn(%q, %q) = %v, want %v — the schema half of every anchor is "+
				"this function, so a wrong answer here makes those anchors decorative",
				tc.col, tc.typ, got, tc.want)
		}
	}

	type withVenues struct {
		People []int
		Venues []string
	}
	type withoutVenues struct {
		People []int
	}
	if f, ok := fieldNames(withVenues{})["Venues"]; !ok || f.Kind() != reflect.Slice {
		t.Error("fieldNames cannot see a slice field that is present; the report and filter " +
			"anchors are built on it")
	}
	if _, ok := fieldNames(withoutVenues{})["Venues"]; ok {
		t.Error("fieldNames reports a field that is NOT on the struct, so the reflection " +
			"anchors would pass over a removed capability")
	}
	// AND THE LIVE ANCHORS ALL PASS TODAY, which is what makes the two checks above
	// a control rather than a separate universe.
	for anchor, derive := range anchorDerivations(t) {
		if why := derive(); why != "" {
			t.Errorf("anchor %q does not hold: %s", anchor, why)
		}
	}
}

func createTableBlock(t *testing.T, sql, name string) string {
	t.Helper()
	re := regexp.MustCompile(`(?is)CREATE TABLE (?:IF NOT EXISTS )?` + regexp.QuoteMeta(name) + `\s*\((.*?)\n\s*\);`)
	m := re.FindStringSubmatch(sql)
	if m == nil {
		t.Fatalf("no CREATE TABLE %s found; the anchor this feeds cannot be derived", name)
	}
	return m[1]
}

// anchorDerivations maps every pages.Anchor to a check that READS THE PRODUCT.
//
// 🔴 NOT ONE OF THEM COMPARES A SENTENCE. That is the whole point: a test that
// asserted the expected copy would agree with whatever was written beside it,
// including the claim that shipped wrong. Each function below fails when the
// CAPABILITY goes away, whatever the page happens to say about it.
//
// Each returns "" when the capability is present, or the reason it is not.
func anchorDerivations(t *testing.T) map[pages.Anchor]func() string {
	t.Helper()
	mig02 := repoFile(t, "db", "migrations", "00002_create_locations_departments.sql")
	mig04 := repoFile(t, "db", "migrations", "00004_create_tags.sql")
	mig05 := repoFile(t, "db", "migrations", "00005_create_transactions_audit_reviews.sql")

	// The two shift-carrying tables, isolated from each other.
	locations := createTableBlock(t, mig02, "locations")
	departments := createTableBlock(t, mig02, "departments")
	transactions := createTableBlock(t, mig05, "transactions")
	tags := createTableBlock(t, mig04, "tags")

	return map[pages.Anchor]func() string{
		pages.AnchorPlaqueBelongsToVenue: func() string {
			if !hasColumn(tags, "location_id", `uuid\s+NOT NULL`) {
				return "tags.location_id is no longer `uuid NOT NULL` in migration 00004, so a " +
					"plaque is no longer tied to exactly one venue"
			}
			return ""
		},
		pages.AnchorVenueShiftAndAddress: func() string {
			switch {
			case !hasColumn(locations, "shift_start", "time"), !hasColumn(locations, "shift_end", "time"):
				return "the locations table no longer carries its own shift"
			case !hasColumn(locations, "static_ips", `cidr\[\]`):
				return "locations.static_ips is gone, so a venue no longer has its own network address"
			}
			return ""
		},
		pages.AnchorCrossVenueNotPenalised: func() string {
			// DERIVED FROM THE POLICY BASELINE ITSELF, not from a source scan: the
			// statement that makes a cross-venue tap normal has to be present AND
			// still be an allow. A baseline that turned it into a review would make
			// the page's "never counted against the person" false.
			for _, doc := range policy.Baseline() {
				for _, st := range doc.Document.Statements {
					if st.Sid != policy.SidCrossLocationNote {
						continue
					}
					if st.Effect != policy.EffectAllow {
						return "the baseline statement " + policy.SidCrossLocationNote +
							" is now " + string(st.Effect) + " rather than allow, so a tap away " +
							"from the home venue IS held against the person"
					}
					return ""
				}
			}
			return "the baseline no longer carries a " + policy.SidCrossLocationNote + " statement"
		},
		pages.AnchorPerVenueReport: func() string {
			f, ok := fieldNames(ledger.Report{})["Venues"]
			if !ok {
				return "ledger.Report has no Venues field, so the report has no per-venue total"
			}
			if f.Kind() != reflect.Slice {
				return "ledger.Report.Venues is a " + f.String() + " rather than a list of venues"
			}
			return ""
		},
		pages.AnchorDepartmentShift: func() string {
			switch {
			case !hasColumn(departments, "shift_start", "time"), !hasColumn(departments, "shift_end", "time"):
				return "the departments table no longer carries its own shift"
			case !hasColumn(departments, "overnight", "boolean"):
				return "departments.overnight is gone, so a department shift can no longer cross midnight"
			}
			return ""
		},
		pages.AnchorLatenessFollowsDepartment: func() string {
			// HALF ONE: the query really does read BOTH shifts. Derived from the sqlc
			// output, which is derived from db/queries — so this fails if the query
			// stops selecting either.
			row := fieldNames(store.ListWorkedShiftEventsRow{})
			for _, want := range []string{"DepartmentShiftStart", "DepartmentShiftEnd", "LocationShiftStart", "LocationShiftEnd"} {
				if _, ok := row[want]; !ok {
					return "the worked-shift query no longer selects " + want +
						", so the two shifts cannot both be in play"
				}
			}
			// HALF TWO: the department is tested FIRST. The claim is about which one
			// WINS, and two fields on a row cannot say that. resolveShift is
			// unexported, so this reads the order of the two checks in its body.
			src := repoFile(t, "internal", "domain", "ledger", "report.go")
			body := regexp.MustCompile(`(?s)func resolveShift\(.*?\n}`).FindString(src)
			if body == "" {
				return "ledger's resolveShift is gone or renamed; which shift wins can no longer be derived"
			}
			dept := strings.Index(body, "DepartmentShiftStart")
			loc := strings.Index(body, "LocationShiftStart")
			if dept < 0 || loc < 0 {
				return "resolveShift no longer consults both shifts"
			}
			if dept > loc {
				return "resolveShift tests the LOCATION's shift before the department's, so " +
					"lateness follows the site rather than the person's own team"
			}
			return ""
		},
		pages.AnchorDepartmentOnEveryRecord: func() string {
			if !hasColumn(transactions, "department_id", "uuid") {
				return "transactions.department_id is gone, so a record no longer carries a department"
			}
			if _, ok := fieldNames(components.FilterBarView{})["DepartmentID"]; !ok {
				return "the transactions filter bar has no DepartmentID, so the list cannot be " +
					"narrowed to one department"
			}
			return ""
		},
	}
}

// TestLandingAudiences_EveryClaimRestsOnAProductAnchor is the pin the block that
// shipped a false sentence did not have.
//
// 🔴 IT CHECKS FOUR THINGS, AND THE FOURTH IS THE ONE THAT WOULD HAVE CAUGHT THE
// DEFECT: every claim names at least one anchor, every named anchor has a
// derivation, every derivation holds against the product, and every anchor DECLARED
// IN pages is actually claimed by something — so an anchor cannot be defined,
// pass, and pin nothing.
//
// ⚠️ THAT FOURTH CHECK WAS WRITTEN AGAINST THE WRONG SET AND AN AUDIT MEASURED IT.
// It walked `derivations` — the map in THIS FILE — while the doc comment promised
// it walked the constants in pages. The difference is the whole gate: adding
// `AnchorInventedCapability Anchor = "schema:nothing.at.all"` to landingview.go,
// with no derivation and no claim, left the package GREEN and go vet silent,
// because a constant the test file never mentions cannot appear in the test file's
// own map. The set is now DERIVED FROM pages' SOURCE (declaredAnchors), which is
// where the promise always pointed.
func TestLandingAudiences_EveryClaimRestsOnAProductAnchor(t *testing.T) {
	t.Parallel()
	derivations := anchorDerivations(t)
	declared := declaredAnchors(t)

	claims := 0
	used := map[pages.Anchor]bool{}
	for _, a := range pages.LandingAudiences {
		for _, c := range a.Points {
			claims++
			if len(c.Anchors) == 0 {
				t.Errorf("the claim %q rests on no product anchor.\n"+
					"Every sentence in this block has to name the capability it depends on, or "+
					"be deleted. A sentence with no anchor is how \"Reports and the monthly "+
					"headcount split by department\" shipped on a page that sells the product.",
					c.Text)
				continue
			}
			for _, anchor := range c.Anchors {
				used[anchor] = true
				derive, ok := derivations[anchor]
				if !ok {
					t.Errorf("the claim %q names the anchor %q, which has no derivation here. "+
						"An anchor nothing derives is a comment, not a pin.", c.Text, anchor)
					continue
				}
				if why := derive(); why != "" {
					t.Errorf("the landing page claims:\n    %q\n"+
						"and the product no longer supports it: %s\n"+
						"(anchor %q)\n"+
						"Fix the product or delete the sentence — a marketing page may not "+
						"describe a capability that is not there.", c.Text, why, anchor)
				}
			}
		}
	}

	// ANTI-VACUITY. A block that lost its claims would satisfy every loop above.
	if claims < 6 {
		t.Fatalf("the two-shapes block carries %d claim(s); it had six, so either the section "+
			"shrank without this test being reconsidered or the scan is reading nothing", claims)
	}
	// AND EVERY ANCHOR DECLARED IN pages IS BOTH DERIVED AND CLAIMED. The set comes
	// from pages' own source, so an anchor that this test file has never heard of
	// still has to answer for itself.
	for anchor, where := range declared {
		if _, ok := derivations[anchor]; !ok {
			t.Errorf("pages declares the anchor %s = %q (%s) and nothing in anchorDerivations "+
				"derives it. An anchor with no derivation is a comment: a claim could name "+
				"it and be pinned by nothing.", where, anchor, "landingview.go")
		}
		if !used[anchor] {
			t.Errorf("pages declares the anchor %s = %q and NO claim rests on it. Either a "+
				"sentence was deleted and its pin left behind, or the pin was written for a "+
				"sentence that was never added.", where, anchor)
		}
	}
	// And nothing is derived here that pages does not declare, or this file could
	// carry checks for a vocabulary the page no longer speaks.
	for anchor := range derivations {
		if _, ok := declared[anchor]; !ok {
			t.Errorf("anchorDerivations derives %q, which pages does not declare as an Anchor "+
				"constant", anchor)
		}
	}
}

// anchorConstRE matches a declaration in pages' Anchor const block.
var anchorConstRE = regexp.MustCompile(`(?m)^\s*(\w+)\s+Anchor\s*=\s*"([^"]+)"`)

// declaredAnchors reads the Anchor constants OUT OF pages' OWN SOURCE, returning
// value -> constant name.
//
// 🔴 IT READS THE SOURCE RATHER THAN REFLECTING, and that is not a shortcut — it is
// the only way to see the set. Go has no runtime enumeration of a package's
// constants: a `const` that nothing references is invisible to reflect, invisible
// to the linker and invisible to go vet. Reading the declaration is what makes
// "declared but unclaimed" a detectable state at all, which is exactly the hole an
// audit walked through by adding a constant no test file mentioned.
func declaredAnchors(t *testing.T) map[pages.Anchor]string {
	t.Helper()
	src := repoFile(t, "web", "templates", "pages", "landingview.go")
	out := map[pages.Anchor]string{}
	for _, m := range anchorConstRE.FindAllStringSubmatch(src, -1) {
		out[pages.Anchor(m[2])] = m[1]
	}
	// ANTI-VACUITY: a regexp that stopped matching would report a package with no
	// anchors and agree with everything.
	if len(out) < 6 {
		t.Fatalf("read %d Anchor constant(s) out of landingview.go; there are at least six, "+
			"so this scan is not seeing the const block and every check built on it would "+
			"pass over anything", len(out))
	}
	return out
}

// TestLandingAudiences_EveryClaimIsActuallyRendered closes the other half: a claim
// can be perfectly anchored and never reach a visitor, in which case the anchor
// guards nothing. Text-matching is legitimate HERE — the question is "did this
// exact sentence reach the page", not "is this sentence true".
func TestLandingAudiences_EveryClaimIsActuallyRendered(t *testing.T) {
	t.Parallel()
	text := screenText(t, mustFetchMarketing(t, marketingRouter(t), "/"))
	for _, a := range pages.LandingAudiences {
		if !strings.Contains(text, a.Title) {
			t.Errorf("the audience %q is not on the page", a.Title)
		}
		for _, c := range a.Points {
			if !strings.Contains(text, c.Text) {
				t.Errorf("the claim %q is anchored and is NOT rendered, so its pin guards "+
					"nothing.\nGot: %s", c.Text, text)
			}
		}
	}
}

// TestLandingAudiences_SayNothingAboutTheTwoBreakdownsThatDoNotExist is the
// tripwire for the exact defect, kept as a NAMED regression rather than trusted to
// the anchors.
//
// 🔴 THE ANCHORS ABOVE WOULD NOT HAVE CAUGHT THE ORIGINAL SENTENCE, and saying so
// is the honest part. "Reports and the monthly headcount split by department" was
// wrong because it described a breakdown that does not exist; an anchor system
// catches a claim whose capability DISAPPEARS, not one that was never there. What
// stops the same sentence coming back is this: the two surfaces are measured for
// the word, and if either ever grows a department dimension this test fails and
// tells the next reader they may now write the sentence.
func TestLandingAudiences_SayNothingAboutTheTwoBreakdownsThatDoNotExist(t *testing.T) {
	t.Parallel()

	// ⚠️ THE FIRST VERSION OF THIS TRIPWIRE WAS TOO COARSE AND FIRED ON A TRUE
	// CAPABILITY. It scanned internal/domain/ledger/report.go for the word
	// "department" and went red, because resolveShift reads the DEPARTMENT'S SHIFT —
	// which is the LATENESS rule, a different capability, one the page legitimately
	// claims two bullets earlier and which is pinned by
	// AnchorLatenessFollowsDepartment. The subject here is the report's BREAKDOWNS,
	// so the report half is checked on the TYPES that carry them rather than on the
	// word appearing anywhere in a 900-line file.
	reportFields := map[string]bool{}
	rt := reflect.TypeOf(ledger.Report{})
	for i := 0; i < rt.NumField(); i++ {
		reportFields[rt.Field(i).Name] = true
	}
	person := reflect.TypeOf(ledger.PersonHours{})
	personFields := map[string]bool{}
	for i := 0; i < person.NumField(); i++ {
		personFields[person.Field(i).Name] = true
	}
	// ANTI-VACUITY: the breakdowns this is reasoning about must still be there.
	if !reportFields["People"] || !reportFields["Venues"] {
		t.Fatal("ledger.Report no longer carries People and Venues; this tripwire is " +
			"reasoning about a report shape that is gone")
	}
	for _, grown := range []string{"Departments", "DepartmentHours"} {
		if reportFields[grown] {
			t.Errorf("ledger.Report has grown a %s breakdown. GOOD NEWS, and this is where it "+
				"is noticed: the landing page's two-shapes block deliberately does NOT claim "+
				"a per-department report, because on 2026-08-13 there was none. Add the "+
				"sentence back with an anchor and delete this arm.", grown)
		}
	}
	if personFields["Department"] {
		t.Error("ledger.PersonHours has grown a Department field, so the report can now be " +
			"read per department. The landing page does not say so; add the sentence back " +
			"with an anchor and delete this arm.")
	}
	// The rendered reports section, which is where a breakdown would have to appear.
	if strings.Contains(strings.ToLower(repoFile(t, "web", "templates", "pages", "reports.templ")), "department") {
		t.Error("the reports template now mentions a department. See above: the page may now " +
			"claim it, with an anchor.")
	}

	// The billing surface is checked by text across all five of its files, because
	// there the measurement was zero in every one of them -- domain, handler, CSV,
	// view and template -- so any mention at all is the news.
	for _, f := range [][]string{
		{"internal", "domain", "billing", "billing.go"},
		{"internal", "handler", "billing.go"},
		{"internal", "handler", "billingcsv.go"},
		{"web", "templates", "pages", "billingview.go"},
		{"web", "templates", "pages", "billing.templ"},
	} {
		if strings.Contains(strings.ToLower(repoFile(t, f...)), "department") {
			t.Errorf("%s now mentions a department, so the monthly headcount may have grown a "+
				"department dimension. The landing page deliberately does not claim one "+
				"(it was zero across all five billing files on 2026-08-13). Re-read the "+
				"surface, and if it is real, say so with an anchor.", filepath.Join(f...))
		}
	}
	// And the page does not claim it today.
	text := strings.ToLower(screenText(t, mustFetchMarketing(t, marketingRouter(t), "/")))
	for _, phrase := range []string{"split by department", "headcount by department", "report by department", "reports by department"} {
		if strings.Contains(text, phrase) {
			t.Errorf("the landing page says %q. Neither the reports section nor the monthly "+
				"headcount has a department dimension.", phrase)
		}
	}
}

// --- the legal surface --------------------------------------------------------

// TestLegalPages_SkeletonsAreNotIndexedAndSayWhatTheyAreWaitingFor.
//
// 🔴 THE ROBOTS VALUE IS DERIVED FROM WHETHER THE DOCUMENT HAS A TEXT, and the
// reason is that an unpublished policy in a search index is read as the published
// one. Both directions are driven: today's skeletons must be private, and a page
// whose list of needed facts is empty must become public without anybody
// remembering to flip anything.
//
// ⚠️ THE FIRST VERSION OF THIS TEST SURVIVED THE MUTATION IT EXISTS FOR, and the
// hole is worth recording because it is the shape every conditional test has.
// Every per-document assertion sat behind `if p.Published() { continue }`, so
// forcing Published() to return true made the loop skip all four documents and the
// count that would have noticed was a t.Logf rather than an assertion — a green run
// over four unpublished policies asking to be indexed. The derivation is now
// checked on CONSTRUCTED values first, where no product state can make the
// assertion vacuous, and the loop asserts both arms instead of skipping one.
func TestLegalPages_SkeletonsAreNotIndexedAndSayWhatTheyAreWaitingFor(t *testing.T) {
	t.Parallel()

	// 1. THE DERIVATION ITSELF, on values this test builds. Nothing about the
	//    product's current state can make these vacuous.
	//
	// ⚠️ THE DEFINITION MOVED IN M7-06 AND THIS BLOCK MOVED WITH IT. It used to read
	// LegalPage.Needs — "a document is published when nothing is left to supply" —
	// which was the only definition available while the texts lived in Go source.
	// They now live in legal_documents, so publication is "a text was published" and
	// the fact lives on the VIEW rather than on the route table.
	waiting := pages.LegalPageView{Page: pages.LegalPage{Path: "/legal/x", Title: "X", Needs: []string{"a fact somebody has to supply"}}}
	finished := pages.LegalPageView{Page: pages.LegalPage{Path: "/legal/x", Title: "X"}, Body: []string{"The finished text."}}
	if waiting.Published() {
		t.Error("a document with no published text reports itself as published. Publication " +
			"is defined as \"somebody published a text\" precisely so that it cannot be " +
			"reached by editing a flag.")
	}
	if !finished.Published() {
		t.Error("a document that carries a published text reports itself as unpublished, so " +
			"no legal text could ever be indexed")
	}
	if got := waiting.Robots(); got != "noindex, nofollow" {
		t.Errorf("an unpublished document renders robots %q, want noindex", got)
	}
	if got := finished.Robots(); got != "index, follow" {
		t.Errorf("a published document renders robots %q, want index", got)
	}

	// 2. THE LANDING PAGE IS THE ONE PAGE THAT ASKS TO BE FOUND.
	r := marketingRouter(t)
	if got := mustFetchMarketing(t, r, "/"); !strings.Contains(got, `content="index, follow"`) {
		t.Error("the landing page asks not to be indexed. It is the one page in this product " +
			"whose whole purpose is to be found; every other screen is reached from a " +
			"personal link and stays private.")
	}

	// 3. NOTHING PUBLISHED: every document is a skeleton and says so. This is the
	//    state a fresh deployment is in and the state marketingRouter builds.
	const draftNotice = "This text has not been published yet"
	for _, p := range pages.LegalPages {
		body := mustFetchMarketing(t, r, p.Path)
		text := screenText(t, body)
		if !strings.Contains(body, `content="noindex, nofollow"`) {
			t.Errorf("%s is a skeleton and asks to be indexed. A policy that is not in force "+
				"must not appear in a search result as though it were.", p.Path)
		}
		if !strings.Contains(text, draftNotice) {
			t.Errorf("%s does not say that it is unfinished. A placeholder that looks ready "+
				"is worse than no page at all.", p.Path)
		}
		if len(p.Needs) == 0 {
			t.Errorf("%s is unpublished and names nothing it is waiting for", p.Path)
		}
		for _, n := range p.Needs {
			if !strings.Contains(text, n) {
				t.Errorf("%s is waiting on %q and does not print it. The list is rendered so "+
					"the person who can supply the facts can see them.", p.Path, n)
			}
		}
	}
}

// TestLegalPages_PublishingOneDocumentChangesThatDocumentAndNoOther is the other
// arm, and it is the one the M7-06 brief asks for by name: what does the page say
// when the texts are PARTLY entered?
//
// 🔴 THE ANSWER IS "EACH PAGE SPEAKS ONLY FOR ITSELF", AND IT IS ASSERTED RATHER
// THAN CLAIMED. Exactly one of the four documents is published; that page must lose
// the "not in force" block and become indexable, and the OTHER THREE must be
// byte-for-byte what they were before — because a page that changed its language
// because a different document was published would be speaking for a document it is
// not.
func TestLegalPages_PublishingOneDocumentChangesThatDocumentAndNoOther(t *testing.T) {
	t.Parallel()
	const draftNotice = "This text has not been published yet"
	const published = "Kebab Factory Ltd is the controller of this fictional example."

	before := marketingRouter(t)
	texts := newFakeTexts()
	texts.put("privacy", published)
	after := marketingRouterWithTexts(t, texts)

	for _, p := range pages.LegalPages {
		was := mustFetchMarketing(t, before, p.Path)
		now := mustFetchMarketing(t, after, p.Path)
		if p.Path != "/legal/privacy" {
			if was != now {
				t.Errorf("%s changed when a DIFFERENT document was published. Each legal page "+
					"speaks for one document; publishing the privacy policy must not alter a "+
					"word of the terms.", p.Path)
			}
			continue
		}
		text := screenText(t, now)
		if !strings.Contains(text, published) {
			t.Errorf("%s was published and does not print its text", p.Path)
		}
		if strings.Contains(text, draftNotice) {
			t.Errorf("%s carries its text and still says it is a placeholder. That sentence was "+
				"honest while there was nothing to show; leaving it up over a published "+
				"policy is the product claiming less than it does, on the one page where a "+
				"reader has to know which it is.", p.Path)
		}
		for _, n := range p.Needs {
			if strings.Contains(text, n) {
				t.Errorf("%s is published and still prints %q as something it is waiting for", p.Path, n)
			}
		}
		if !strings.Contains(now, `content="index, follow"`) {
			t.Errorf("%s carries its text and still asks not to be indexed", p.Path)
		}
	}
}

// TestLegalPage_PublishedTextIsEscapedAndNeverRaw is the injection half.
//
// 🔴 THE ONE SCREEN IN THIS PRODUCT WHERE A PERSON PASTES FREE PROSE INTO A PAGE A
// STRANGER LOADS. templ escapes `{ }` through html.EscapeString, and the whole
// paragraph design exists so that this text never reaches templ.Raw — measured:
// templ.Raw has ZERO call sites here and not one of the twelve tests that scan
// .templ files would notice a first one appearing. So the guarantee is asserted on
// the rendered bytes rather than trusted to a comment.
func TestLegalPage_PublishedTextIsEscapedAndNeverRaw(t *testing.T) {
	t.Parallel()
	const attack = `<script>alert(1)</script> and an "attribute" break & an <img src=x onerror=1>`
	texts := newFakeTexts()
	texts.put("terms", attack)
	body := mustFetchMarketing(t, marketingRouterWithTexts(t, texts), "/legal/terms")

	// ⚠️ THE PATTERNS ARE THE ONES THAT ONLY EXIST UNESCAPED. `onerror=` was in the
	// first draft of this list and it is a FALSE POSITIVE: escaping turns
	// `<img src=x onerror=1>` into `&lt;img src=x onerror=1&gt;`, so the attribute
	// NAME survives as harmless text and the assertion would have failed on correct
	// output. What cannot survive escaping is the angle bracket that opens a tag.
	for _, raw := range []string{"<script>", "</script>", "<img ", "<img src=x"} {
		if strings.Contains(body, raw) {
			t.Fatalf("the published legal text put %q into the page verbatim. This is the one "+
				"screen where somebody types prose that a stranger's browser executes; it "+
				"must go through templ's escaping and never through templ.Raw.", raw)
		}
	}
	if !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Error("the escaped form of the pasted text is not on the page, so the assertion above " +
			"may be passing because the text never rendered at all")
	}
	// Single escaping: a body that arrived double-escaped would be safe and WRONG,
	// and a legal document whose ampersands read `&amp;amp;` is not the text somebody
	// published.
	if strings.Contains(body, "&amp;amp;") {
		t.Error("the published text was escaped twice; the document a reader sees is not the one that was published")
	}
}

// --- the cookie notice ---------------------------------------------------------

// cookieNameLiteral matches a Tappa cookie name written into Go source. Every
// cookie in this product is named by a string literal of this shape, and nothing
// else in non-test Go source is (measured: the six below and nothing more).
var cookieNameLiteral = regexp.MustCompile(`"(tappa_[a-z_]+)"`)

// TestCookieNotice_ListsExactlyTheCookiesTheProductSets.
//
// 🔴 A DISCLOSURE KEPT BY HAND GOES STALE ON THE NEXT FEATURE, and a cookie notice
// that omits a cookie is the one page on this surface with a legal consequence. So
// the set is DERIVED from the source of every package that writes one and compared
// with the table -- in both directions, because a table naming a cookie the product
// no longer sets is wrong in the other way.
func TestCookieNotice_ListsExactlyTheCookiesTheProductSets(t *testing.T) {
	t.Parallel()
	inSource := map[string]string{}
	setCookieCalls := 0
	root := filepath.Join("..", "..")
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.Walk(filepath.Join(root, dir), func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			raw, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			text := string(raw)
			setCookieCalls += strings.Count(text, "http.SetCookie(")
			for _, m := range cookieNameLiteral.FindAllStringSubmatch(text, -1) {
				inSource[m[1]] = strings.TrimPrefix(p, root+string(filepath.Separator))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
	}
	// ANTI-VACUITY, both halves. A walk that read nothing would report a product
	// with no cookies at all and agree with an empty table.
	if setCookieCalls < 6 {
		t.Fatalf("found %d http.SetCookie call(s) in non-test source; this product writes "+
			"more than that, so this scan is reading the wrong files", setCookieCalls)
	}
	if len(inSource) < 6 {
		t.Fatalf("derived %d cookie name(s) from the source (%v); the product sets six",
			len(inSource), sortedCookieNames(inSource))
	}

	inNotice := map[string]bool{}
	for _, row := range cookieNotice() {
		if inNotice[row.Name] {
			t.Errorf("the cookie notice lists %s twice", row.Name)
		}
		inNotice[row.Name] = true
		for field, value := range map[string]string{
			"purpose": row.Purpose, "lifetime": row.Lifetime, "scope": row.Scope, "flags": row.Flags,
		} {
			if strings.TrimSpace(value) == "" {
				t.Errorf("the cookie notice gives %s no %s", row.Name, field)
			}
		}
	}
	for name, where := range inSource {
		if !inNotice[name] {
			t.Errorf("%s is set by %s and is NOT in the cookie notice. Every cookie this "+
				"product writes has to be disclosed with its purpose and its lifetime; a "+
				"new one is not finished until it is on /legal/cookies.", name, where)
		}
	}
	for name := range inNotice {
		if _, ok := inSource[name]; !ok {
			t.Errorf("the cookie notice lists %s and nothing in the source sets it; the page "+
				"is telling visitors about a cookie they will never receive", name)
		}
	}

	// AND THE PAGE PRINTS THEM. A table built correctly and rendered nowhere
	// discloses nothing.
	body := mustFetchMarketing(t, marketingRouter(t), cookieNoticePath)
	for name := range inNotice {
		if !strings.Contains(body, name) {
			t.Errorf("%s is in the table and not on %s", name, cookieNoticePath)
		}
	}
}

// TestCookieNameScanner_CatchesANewCookie is the negative control: a seventh cookie
// added tomorrow must be visible to the scan above.
func TestCookieNameScanner_CatchesANewCookie(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		`const newThingCookieName = "tappa_new_thing"`,
		`http.SetCookie(w, &http.Cookie{Name: "tappa_ad_tracker", Value: v})`,
		`	name := "tappa_preferences"`,
	} {
		if cookieNameLiteral.FindString(s) == "" {
			t.Errorf("the cookie-name scanner would not see %q, so a cookie declared that "+
				"way could ship undisclosed", s)
		}
	}
}

// cookieWriter is the function that actually writes one cookie: which file it is
// in, and the name of the method whose body carries the http.Cookie literal.
type cookieWriter struct {
	file   []string
	method string
}

// cookieWriters maps each disclosed cookie to the code that writes it.
//
// ⚠️ IT IS A HAND-WRITTEN MAPPING AND IT IS BOUNDED BY ONE THAT IS NOT.
// TestCookieNotice_ListsExactlyTheCookiesTheProductSets derives the SET of cookie
// names from the source of every package that writes one, so a seventh cookie
// fails there; this map only says WHERE each of them is written, and the test
// below refuses a notice row with no entry here. A new cookie therefore cannot
// reach the page without somebody naming its writer.
var cookieWriters = map[string]cookieWriter{
	"tappa_session":       {[]string{"internal", "session", "cookie.go"}, "Set"},
	"tappa_admin_session": {[]string{"internal", "adminauth", "cookie.go"}, "Set"},
	"tappa_activation":    {[]string{"internal", "handler", "cookies.go"}, "set"},
	// The three short panel cookies all go through ONE setter, which is why they
	// share a writer: adminCookies.set takes the name as a parameter and applies
	// the same attributes to all three.
	"tappa_admin_login":   {[]string{"internal", "handler", "logincontext.go"}, "set"},
	"tappa_admin_choice":  {[]string{"internal", "handler", "logincontext.go"}, "set"},
	"tappa_admin_confirm": {[]string{"internal", "handler", "logincontext.go"}, "set"},
	// The two the sign-up wizard sets (M7-02). They share a setter for the same
	// reason the three above do: signupCookies.set takes the name as a parameter and
	// applies the same attributes to both.
	"tappa_signup":       {[]string{"internal", "handler", "signupstate.go"}, "set"},
	"tappa_signup_state": {[]string{"internal", "handler", "signupstate.go"}, "set"},
	// The two the recovery flow sets (M7-04 phase B), and they name DIFFERENT
	// writers, which is the disclosure this map exists to keep honest. The
	// synchronizer cookie goes through the shared adminCookies.set like the three
	// above; the one carrying the recovery LINK has its own writer because it has a
	// NARROWER Path — /admin/reset/new rather than /admin — so the raw token does not
	// ride along on every authenticated panel request for an hour. If both had stayed
	// on one writer the Scope column would have disclosed the wrong path for one of
	// them, silently.
	"tappa_admin_reset":      {[]string{"internal", "handler", "logincontext.go"}, "set"},
	"tappa_admin_reset_link": {[]string{"internal", "handler", "logincontext.go"}, "setResetLink"},
}

var (
	cookieLiteralRE = regexp.MustCompile(`(?s)http\.SetCookie\(w, &http\.Cookie\{(.*?)\n\t\}\)`)
	cookieFieldRE   = regexp.MustCompile(`(?m)^\s*(\w+):\s*(.+?),\s*$`)
)

// setCookieAttrs extracts the http.Cookie literal written by file's `method` and
// returns its field assignments as written.
func setCookieAttrs(t *testing.T, w cookieWriter) map[string]string {
	t.Helper()
	src := repoFile(t, w.file...)
	// The setter, from its receiver line to the closing brace at column 0.
	fnRE := regexp.MustCompile(`(?s)\nfunc \([^)]*\) ` + regexp.QuoteMeta(w.method) + `\(.*?\n\}`)
	body := fnRE.FindString(src)
	if body == "" {
		t.Fatalf("no func %s in %s; the cookie notice's attributes are derived from it",
			w.method, filepath.Join(w.file...))
	}
	lit := cookieLiteralRE.FindStringSubmatch(body)
	if lit == nil {
		t.Fatalf("func %s in %s no longer writes an http.Cookie literal this test can read",
			w.method, filepath.Join(w.file...))
	}
	out := map[string]string{}
	for _, m := range cookieFieldRE.FindAllStringSubmatch(lit[1], -1) {
		out[m[1]] = strings.TrimSpace(m[2])
	}
	return out
}

// TestCookieNotice_FlagsAndScopeMatchTheCookieTheCodeWrites.
//
// 🔴 THE LIFETIMES WERE PINNED AND THESE TWO COLUMNS WERE NOT, ON A LEGAL DOCUMENT.
// An audit measured the gap: Flags was a flat constant in marketing.go and two
// rows' Scope was a mirror, and the only thing checking either was "is it
// non-empty". A seventh cookie carrying SameSite=Strict, or a Path change in
// internal/session, would have left /legal/cookies quietly WRONG — the same drift
// the price and the lifetimes are held against, on the one page that carries a
// legal consequence.
//
// So every attribute the notice prints is now read out of the http.Cookie literal
// that writes it.
func TestCookieNotice_FlagsAndScopeMatchTheCookieTheCodeWrites(t *testing.T) {
	t.Parallel()
	rows := cookieNotice()
	if len(rows) < 6 {
		t.Fatalf("the notice carries %d row(s); this test would pass over almost nothing", len(rows))
	}
	for _, row := range rows {
		w, ok := cookieWriters[row.Name]
		if !ok {
			t.Errorf("%s is disclosed on /legal/cookies and no writer is named for it, so its "+
				"attributes and its scope are unchecked. Add it to cookieWriters.", row.Name)
			continue
		}
		attrs := setCookieAttrs(t, w)
		where := filepath.Join(w.file...) + " " + w.method

		// --- Flags -------------------------------------------------------------
		// The notice claims HttpOnly, SameSite=Lax and Secure. Each is compared
		// with what the literal assigns.
		if got := attrs["HttpOnly"]; got != "true" {
			t.Errorf("%s: the notice says HttpOnly and %s writes HttpOnly: %s", row.Name, where, got)
		}
		if got := attrs["SameSite"]; got != "http.SameSiteLaxMode" {
			t.Errorf("%s: the notice says SameSite=Lax and %s writes SameSite: %s.\n"+
				"The cookie notice is a legal disclosure; either the code or the page is "+
				"wrong, and a visitor cannot tell which.", row.Name, where, got)
		}
		// Secure must be BOUND TO THE CODEC rather than a literal. `Secure: false`
		// would ship a credential over plain http while the page promised otherwise.
		sec := attrs["Secure"]
		if !strings.Contains(sec, "ecure()") {
			t.Errorf("%s: the notice says Secure is set outside development and %s writes "+
				"Secure: %s, which is not the codec's own decision", row.Name, where, sec)
		}
		for _, claim := range []string{"HttpOnly", "SameSite=Lax", "Secure"} {
			if !strings.Contains(row.Flags, claim) {
				t.Errorf("%s: the code writes %s and the notice's Flags column (%q) does not "+
					"say so", row.Name, claim, row.Flags)
			}
		}

		// --- Scope -------------------------------------------------------------
		// The Path as WRITTEN, resolved through the one constant it may name.
		wantPath := map[string]string{
			`"/"`:                  "/",
			"CookiePath":           adminauth.CookiePath,
			"adminauth.CookiePath": adminauth.CookiePath,
			// M7-02. The wizard's two cookies are scoped away from both the tap
			// surface and the panel, which is the property the Scope column discloses.
			"signupCookiePath": signupCookiePath,
			// M7-04. The recovery LINK cookie is narrower than every other panel
			// cookie: it is the only one carrying a §4.7 secret, so it is scoped to
			// the single route that reads it.
			"adminResetLinkCookiePath": adminResetLinkCookiePath,
		}[attrs["Path"]]
		if wantPath == "" {
			t.Errorf("%s: %s writes Path: %s, which this test cannot resolve. Resolve it here "+
				"rather than leaving the notice's scope column unchecked.", row.Name, where, attrs["Path"])
			continue
		}
		if row.Scope != wantPath {
			t.Errorf("%s: %s sends the cookie to %q and the notice tells visitors %q.\n"+
				"Scope is the difference between a cookie that accompanies the tap pages "+
				"and one that only reaches the dashboard.", row.Name, where, wantPath, row.Scope)
		}
	}
}

// TestSetCookieAttrReader_SeesTheFieldsItIsAskedFor is the negative control for the
// parser above: a reader that returned an empty map would make every comparison in
// that test compare "" with "" and pass.
func TestSetCookieAttrReader_SeesTheFieldsItIsAskedFor(t *testing.T) {
	t.Parallel()
	for name, w := range cookieWriters {
		attrs := setCookieAttrs(t, w)
		for _, field := range []string{"Name", "Path", "MaxAge", "HttpOnly", "Secure", "SameSite"} {
			if attrs[field] == "" {
				t.Errorf("%s: the reader saw no %s in %s's cookie literal, so any assertion "+
					"about it is vacuous", name, field, filepath.Join(w.file...))
			}
		}
	}
}

var maxAgeLiteralRE = regexp.MustCompile(`cookieMaxAgeSeconds\s*=\s*([0-9\s*]+?)\n`)

// TestCookieNotice_LifetimesMatchTheCodeThatWritesThem.
//
// Four of the six lifetimes are constants in THIS package and cannot drift -- the
// notice uses them directly. The employee session's year and the panel session's
// twelve hours are unexported in internal/session and internal/adminauth, and
// handler.Marketing mirrors them. This re-reads both literals and compares, so the
// mirrors are held by arithmetic rather than by the comment beside them.
func TestCookieNotice_LifetimesMatchTheCodeThatWritesThem(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		file   []string
		mirror int
		what   string
	}{
		{[]string{"internal", "session", "cookie.go"}, sessionCookieMaxAge, "the employee session cookie"},
		{[]string{"internal", "adminauth", "cookie.go"}, adminSessionCookieMaxAge, "the panel session cookie"},
	} {
		src := repoFile(t, tc.file...)
		m := maxAgeLiteralRE.FindStringSubmatch(src)
		if m == nil {
			t.Errorf("%s no longer declares cookieMaxAgeSeconds in a shape this test can "+
				"read; the cookie notice mirrors that number and cannot be left unchecked",
				filepath.Join(tc.file...))
			continue
		}
		got, ok := evalProduct(m[1])
		if !ok {
			t.Errorf("%s declares cookieMaxAgeSeconds as %q, which this test cannot evaluate", filepath.Join(tc.file...), m[1])
			continue
		}
		if got != tc.mirror {
			t.Errorf("%s: %s lasts %d seconds; the cookie notice says %d (%s). The notice "+
				"mirrors an unexported constant, so the two have to be compared rather "+
				"than trusted.", filepath.Join(tc.file...), tc.what, got, tc.mirror,
				humanSeconds(tc.mirror))
		}
	}

	// The four this package owns, printed from the constants themselves. Asserted
	// so a wrong FORMATTER cannot make a right number read wrongly.
	//
	// ⚠️ THE WORD "FOUR" WAS TRUE OF THE PROSE AND FALSE OF THE TABLE: this listed
	// three of them and left adminConfirmMaxAge unasserted, so the one cookie whose
	// lifetime is derived from a TTL rather than written as a literal was the one
	// nothing checked. It is here now.
	for _, tc := range []struct {
		sec  int
		want string
	}{
		{activationCookieMaxAge, "15 minutes"},
		{adminLoginCookieMaxAge, "15 minutes"},
		{adminChoiceCookieMaxAge, "5 minutes"},
		{adminConfirmMaxAge, "10 minutes"},
		{sessionCookieMaxAge, "365 days"},
		{adminSessionCookieMaxAge, "12 hours"},
		{90, "90 seconds"},
		{3600, "1 hour"},
		{86400, "1 day"},
		{60, "1 minute"},
	} {
		if got := humanSeconds(tc.sec); got != tc.want {
			t.Errorf("humanSeconds(%d) = %q, want %q", tc.sec, got, tc.want)
		}
	}
}

// evalProduct evaluates "365 * 24 * 60 * 60" and nothing more adventurous.
func evalProduct(expr string) (int, bool) {
	n := 1
	for _, part := range strings.Split(expr, "*") {
		v, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return 0, false
		}
		n *= v
	}
	return n, true
}

// sortedCookieNames renders the derived set for an error message, in a stable
// order. It is not result_test.go's sortedKeys: that one takes a set (map to bool)
// and this map carries WHERE each name was found, which is the half of the message
// that tells somebody which file to open.
func sortedCookieNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, where := range m {
		out = append(out, k+" ("+where+")")
	}
	sort.Strings(out)
	return out
}

// --- the animation --------------------------------------------------------------

// TestLanding_ReducedMotionLeavesTheDocketVisible is the card's
// prefers-reduced-motion criterion, checked as a SAFETY property rather than as the
// presence of a media query.
//
// 🔴 THE HAZARD IS NOT "the animation still plays". It is that the animation fades
// in from zero opacity, so the WRONG way to respect the preference -- declare the
// animation, then switch it off in a `reduce` block -- leaves a reader looking at
// an invisible card the moment that second rule is dropped, reordered or renamed.
// The rule is therefore declared only inside `no-preference`, and this asserts
// exactly that: every mention of the class in the stylesheet is inside such a
// block. A reader who asks for less motion, and any browser that does not support
// the query at all, gets the card painted normally.
func TestLanding_ReducedMotionLeavesTheDocketVisible(t *testing.T) {
	t.Parallel()
	checked := 0
	for _, f := range [][]string{
		{"web", "static", "css", "input.css"},
		{"web", "static", "css", "app.css"},
	} {
		p := filepath.Join(append([]string{"..", ".."}, f...)...)
		raw, err := os.ReadFile(p)
		if err != nil {
			// app.css is built by `make css` and is gitignored, so it is absent in CI.
			// input.css is committed and its absence is a real failure.
			if strings.HasSuffix(p, "app.css") {
				t.Logf("SKIPPING the compiled stylesheet: %s is not built here (make css)", p)
				continue
			}
			t.Fatalf("reading %s: %v", p, err)
		}
		// 🔴 COMMENTS ARE STRIPPED FIRST AND THE FIRST VERSION OF THIS TEST DID NOT
		// DO IT — measured, on the very file it guards. Both halves below count
		// braces, and a comment that carries neither a brace nor a semicolon makes
		// the prelude that follows it read as prose rather than as an at-rule, so the
		// enclosing @media disappeared and this reported a correct stylesheet as
		// broken. It is the same step dashboard_test.go's ink-tone scan takes over
		// the same file, for the same reason.
		css := cssCommentRE.ReplaceAllString(string(raw), " ")
		idx := strings.Index(css, "docket-print")
		if idx < 0 {
			t.Errorf("%s carries no docket-print rule at all; the hero's one animation is "+
				"either gone or renamed, and this test is guarding nothing", p)
			continue
		}
		checked++
		for ; idx >= 0; idx = indexFrom(css, "docket-print", idx+1) {
			rules := enclosingAtRules(css, idx)
			if !containsSubstring(rules, "prefers-reduced-motion") ||
				!containsSubstring(rules, "no-preference") {
				t.Errorf("%s mentions docket-print outside a "+
					"`prefers-reduced-motion: no-preference` block (enclosing at-rules: %v).\n"+
					"The animation fades in from zero opacity, so a rule that exists outside "+
					"that block needs a second rule to undo it -- and a reader who asks for "+
					"less motion must never be shown an invisible card.", p, rules)
				break
			}
		}
		// AND NOTHING SWITCHES IT OFF UNDER `reduce`, which is the shape that would
		// make the visible state depend on a second rule surviving. .tap-button
		// legitimately uses such a block, so the match is on the CONDITION being the
		// reduce one — spelled with its colon, because "prefers-reduced-motion"
		// contains the substring "reduce" and the first version of this check
		// therefore flagged the no-preference block as its own opposite.
		for _, block := range splitAtRuleBodies(css, "prefers-reduced-motion") {
			if !reduceConditionRE.MatchString(block.condition) {
				continue
			}
			if strings.Contains(block.body, "docket-print") {
				t.Errorf("%s switches docket-print off inside a `reduce` block. That is the "+
					"shape this rule was written to avoid: it makes the visible state "+
					"depend on a second rule surviving.", p)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no stylesheet was checked; this test measured nothing")
	}
}

// reduceConditionRE matches the reduce arm of the preference query and NOT the
// no-preference arm. The colon is what separates them: without it, "reduce" is a
// substring of "prefers-reduced-motion" and every block matches.
var reduceConditionRE = regexp.MustCompile(`prefers-reduced-motion\s*:\s*reduce`)

func indexFrom(s, sub string, from int) int {
	if from >= len(s) {
		return -1
	}
	i := strings.Index(s[from:], sub)
	if i < 0 {
		return -1
	}
	return from + i
}

func containsSubstring(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// enclosingAtRules returns the @-rule preludes open at byte offset idx, outermost
// first. It counts braces from the start of the file, which is enough for a
// stylesheet and needs no CSS parser.
func enclosingAtRules(css string, idx int) []string {
	var stack []string
	start := 0
	for i := 0; i < idx && i < len(css); i++ {
		switch css[i] {
		case '{':
			prelude := strings.TrimSpace(css[start:i])
			if j := strings.LastIndexAny(prelude, "};"); j >= 0 {
				prelude = strings.TrimSpace(prelude[j+1:])
			}
			stack = append(stack, prelude)
			start = i + 1
		case '}':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			start = i + 1
		}
	}
	out := make([]string, 0, len(stack))
	for _, s := range stack {
		if strings.HasPrefix(s, "@") {
			out = append(out, s)
		}
	}
	return out
}

type atRuleBlock struct{ condition, body string }

// splitAtRuleBodies returns every at-rule whose prelude contains name, with its
// body. Brace-counted, same limitation and same sufficiency as above.
func splitAtRuleBodies(css, name string) []atRuleBlock {
	var out []atRuleBlock
	for i := 0; i < len(css); {
		j := indexFrom(css, "@media", i)
		if j < 0 {
			break
		}
		open := strings.Index(css[j:], "{")
		if open < 0 {
			break
		}
		open += j
		condition := strings.TrimSpace(css[j:open])
		depth, k := 1, open+1
		for ; k < len(css) && depth > 0; k++ {
			switch css[k] {
			case '{':
				depth++
			case '}':
				depth--
			}
		}
		if strings.Contains(condition, name) {
			out = append(out, atRuleBlock{condition: condition, body: css[open+1 : max(open+1, k-1)]})
		}
		i = open + 1
	}
	return out
}
