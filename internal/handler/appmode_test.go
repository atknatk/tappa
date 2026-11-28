package handler

// appmode_test.go -- M16-54 (ADR 0051 §2.6): the sign-up wizard and the legal pages in the native
// app's in-app browser. [H54-a] the app's pages link only inside a closed set; [H54-b] without the
// flag every page is the golden; [H54-c] the flag loosens no check; [H54-d] the app's App Link
// paths stay {/t, /q}; [H54-e] the flag travels from step to step in the signed state cookie.
//
// /account-deletion (M16-55's page, ADR 0051 §2.9) does not exist yet: when it is mounted, its
// ?client=app render joins appModeRenders and so [H54-a] (the M16-54 card names it there).
//
// [H54-b] WITHOUT THE FLAG EVERY PAGE IS THE GOLDEN. testdata/appmode-golden/ holds one file per
// render of appModeRenders(t, false), WRITTEN FROM 3e51b5be, BEFORE ANY M16-54 CHANGE: a golden
// written after the change would pin whatever the change did. The write mode
// (TAPPA_WRITE_APPMODE_GOLDEN=1) writes only files that do not exist and then fails, so replacing a
// golden means deleting it first -- a visible diff, never a silent one (unbranded_golden_test.go's
// rule). Two values are masked because they are random per run: the synchronizer token every form
// echoes and the registration link's token on a dev installation's "Check your inbox".
//
// A GOLDEN'S BODY IS STORED WITH A LINE BREAK BEFORE EVERY TAG (appModeGoldenBytes), so a page's
// words ("password", the cookie notice's "token") do not share one line with its attributes, which
// is the shape the secret scan (redline R7d, a0-token) refuses in a committed file. The transform
// is reversible -- every "\n<" in the stored form is one "<" of the page -- so comparing stored forms
// compares the bytes. The pre-change renders were converted to it, never re-rendered.

import (
	"bytes"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/domain/signup"
	"github.com/atknatk/tappa/web/templates/layout"
	"github.com/atknatk/tappa/web/templates/pages"
)

const appModeGoldenDir = "testdata/appmode-golden"

// appModeGoldenCount is how many renders the golden holds.
const appModeGoldenCount = 19

var (
	appModeCSRFRE = regexp.MustCompile(`(name="csrf" value=")([^"]+)(")`)
	appModeLinkRE = regexp.MustCompile(`(\?t=)[A-Za-z0-9_-]+`)
)

// appModeMask replaces the two per-run values with placeholders.
func appModeMask(b []byte) []byte {
	b = appModeCSRFRE.ReplaceAll(b, []byte(`${1}{{CSRF}}${3}`))
	return appModeLinkRE.ReplaceAll(b, []byte(`${1}{{LINK}}`))
}

// appModeGoldenBytes is goldenBytes with the body broken before every tag (this file's header).
func appModeGoldenBytes(r screenGolden) []byte {
	r.body = bytes.ReplaceAll(r.body, []byte("<"), []byte("\n<"))
	return goldenBytes(r)
}

// appModeRender is one response of the app-mode walk. inApp says the render is a page the app's
// in-app browser draws when the walk starts with the flag: the wizard's own steps and the problems
// a wizard already carrying the flag can reach, and the legal pages opened with it. The others are
// in the walk for [H54-b] (the confirmation and the done page live under /admin, out of the flag's
// reach; a request with no cookie at all cannot be known as the app's).
type appModeRender struct {
	screenGolden
	inApp bool
}

// appModeRenders walks the wizard and the legal pages, with or without the app's flag. The flag is
// written as the app writes it ("?client=app", ADR 0051 §2.6), never from the code's own constant.
func appModeRenders(t *testing.T, app bool) []appModeRender {
	t.Helper()
	entry := signupPath
	withFlag := func(p string) string { return p }
	if app {
		entry = signupPath + "?client=app"
		withFlag = func(p string) string { return p + "?client=app" }
	}
	var out []appModeRender
	add := func(name string, inApp bool, rec *httptest.ResponseRecorder) {
		r := readRecorded(t, name, rec)
		r.body = appModeMask(r.body)
		out = append(out, appModeRender{screenGolden: r, inApp: inApp})
	}
	see := func(what string, rec *httptest.ResponseRecorder, want int) {
		t.Helper()
		if rec.Code != want {
			t.Fatalf("%s = %d, want %d\n%s", what, rec.Code, want, rec.Body.String())
		}
	}

	// The whole wizard, its three refused forms, "Check your inbox", its reload, and the
	// link's two pages.
	d, _ := newSignupDriver(t, &fakeProvisioner{}, &fakeVAT{status: signup.VATValid})
	start := d.get(entry)
	see("GET "+entry, start, http.StatusOK)
	add("signup-business", true, start)
	csrf := d.token(start)
	bad := businessForm(csrf)
	bad.Set("name", "")
	add("signup-business-refused", true, d.post(signupPath, bad))
	see("POST /signup", d.post(signupPath, businessForm(csrf)), http.StatusSeeOther)
	places := d.get(signupPlacesPath)
	see("GET /signup/places", places, http.StatusOK)
	add("signup-places", true, places)
	add("signup-places-refused", true, d.post(signupPlacesPath, placesForm(d.token(places))))
	see("POST /signup/places", d.post(signupPlacesPath, placesForm(d.token(places), "St Julians", "Sliema")), http.StatusSeeOther)
	account := d.get(signupAccountPath)
	see("GET /signup/account", account, http.StatusOK)
	add("signup-account", true, account)
	d.advance(time.Minute)
	abad := accountForm(d.token(account))
	abad.Set("email", "not-an-address")
	add("signup-account-refused", true, d.post(signupAccountPath, abad))
	see("POST /signup/account", d.post(signupAccountPath, accountForm(d.token(account))), http.StatusSeeOther)
	sent := d.get(signupSentPath)
	see("GET /signup/sent", sent, http.StatusOK)
	add("signup-sent", true, sent)
	// "Check your inbox" clears both wizard cookies (the cookie notice says so), so its reload
	// is a request the server cannot tell from any other cookie-less one: not the app's.
	add("signup-sent-reload", false, d.get(signupSentPath))
	m := screenLinkRE.FindStringSubmatch(sent.Body.String())
	if m == nil {
		t.Fatalf("no registration link on %s", signupSentPath)
	}
	link, err := url.Parse(m[1])
	if err != nil {
		t.Fatal(err)
	}
	see("GET the link", d.get(link.RequestURI()), http.StatusSeeOther)
	confirm := d.get(signupConfirmPath)
	see("GET "+signupConfirmPath, confirm, http.StatusOK)
	add("signup-confirm", false, confirm)
	done := d.post(signupConfirmPath, url.Values{"csrf": {d.token(confirm)}, "password": {accountForm("").Get("password")}})
	see("POST "+signupConfirmPath, done, http.StatusOK)
	add("signup-done", false, done)

	// The problems a wizard carrying the flag can reach: a step asked for before its state
	// exists, and a state past its half hour whose cookies the browser still holds.
	early, _ := newSignupDriver(t, &fakeProvisioner{}, nil)
	see("GET "+entry, early.get(entry), http.StatusOK)
	add("signup-problem-restart", true, early.get(signupAccountPath))
	late, _ := newSignupDriver(t, &fakeProvisioner{}, nil)
	lstart := late.get(entry)
	see("POST /signup", late.post(signupPath, businessForm(late.token(lstart))), http.StatusSeeOther)
	late.advance(signupTTL + signupFutureSkew + time.Minute)
	add("signup-problem-expired", true, late.get(signupPlacesPath))
	// And one it cannot: a browser with no wizard cookie at all.
	cold, _ := newSignupDriver(t, &fakeProvisioner{}, nil)
	add("signup-problem-nocookie", false, cold.get(signupPlacesPath))

	// Step one's "this business is already with Taptime" answer: no handler draws it since
	// M14-17 (the VAT number is checked at the confirmation), so it is rendered here.
	known := httptest.NewRecorder()
	known.WriteHeader(http.StatusOK)
	if err := pages.SignupBusiness(pages.SignupBusinessView{
		CSRFToken: "x", Types: businessTypeOptions(), Name: "Kebab Factory Ltd", VATNumber: "MT12345678",
		SignInHref: adminLoginPath, App: app,
	}).Render(t.Context(), known); err != nil {
		t.Fatalf("rendering the known business: %v", err)
	}
	add("signup-business-known", true, known)

	// The four legal documents as they ship (nothing published), and one published text with
	// every kind of link a text can carry (legal.SafeHref: https, mailto, tel).
	skeletons := marketingRouterWithTexts(t, nil)
	for _, p := range pages.LegalPages {
		add("legal-"+legalSlugOf(p.Path), true, appModeGet(skeletons, withFlag(p.Path)))
	}
	texts := newFakeTexts()
	texts.put("imprint", "Taptime is operated by a registered company.\n\n"+
		"Contact: info@taptime.mt or +356 9931 0184.\n\nhttps://codeveralabs.com")
	add("legal-imprint-published", true, appModeGet(marketingRouterWithTexts(t, texts), withFlag("/legal/imprint")))
	return out
}

// appModeGet is one GET on a router, as a browser with no cookie sends it.
func appModeGet(h http.Handler, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

// TestAppMode_WithoutTheFlagEveryPageIsTheGolden [H54-b]: with no ?client=app every render of the
// walk -- the wizard's screens, its problems, the link's two pages and the four legal documents -- is
// byte for byte the golden written before M16-54 (status, Content-Security-Policy, body).
func TestAppMode_WithoutTheFlagEveryPageIsTheGolden(t *testing.T) {
	renders := appModeRenders(t, false)
	if len(renders) != appModeGoldenCount {
		t.Errorf("the walk has %d renders, appModeGoldenCount says %d", len(renders), appModeGoldenCount)
	}
	write := os.Getenv("TAPPA_WRITE_APPMODE_GOLDEN") == "1"
	seen := map[string]bool{}
	var written []string
	for _, r := range renders {
		if seen[r.name] {
			t.Fatalf("two renders are named %q", r.name)
		}
		seen[r.name] = true
		path := filepath.Join(appModeGoldenDir, r.name+".golden")
		got := appModeGoldenBytes(r.screenGolden)
		want, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) && write {
			if err := os.MkdirAll(appModeGoldenDir, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(path, got, 0o644); err != nil {
				t.Fatalf("writing %s: %v", path, err)
			}
			written = append(written, r.name)
			continue
		}
		if err != nil {
			t.Fatalf("%s: no golden (%v) -- the golden is written from the code BEFORE M16-54, never after", r.name, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from its golden (%d bytes vs %d): without the flag the page changed", r.name, len(got), len(want))
		}
	}
	entries, err := os.ReadDir(appModeGoldenDir)
	if err != nil {
		t.Fatalf("reading %s: %v", appModeGoldenDir, err)
	}
	for _, e := range entries {
		if n := strings.TrimSuffix(e.Name(), ".golden"); !seen[n] {
			t.Errorf("golden %s is rendered by nothing in appModeRenders", e.Name())
		}
	}
	if len(written) > 0 {
		t.Fatalf("wrote %d missing golden file(s): %s -- unset TAPPA_WRITE_APPMODE_GOLDEN and rerun",
			len(written), strings.Join(written, ", "))
	}
}

// ------------------------------------------------------------------------- [H54-a] --

var (
	appModeAnchorRE = regexp.MustCompile(`<a\b[^>]*\bhref="([^"]*)"`)
	appModeActionRE = regexp.MustCompile(`<form\b[^>]*\baction="([^"]*)"`)
	appModeLegalRE  = regexp.MustCompile(`^(/legal/[a-z]+)\?client=app$`)
)

// appModeLinkProblems lists every link on an app page outside ADR 0051 §2.6's closed set: the
// wizard (/signup...), a legal document with the flag, /account-deletion with the flag, "Built by"
// (layout.CreditURL, decision K-B), mailto: and an anchor -- and two that only one kind of page may
// draw: a published legal text's own links (https to another host, tel:) on a legal page, and the
// dev or sandbox installation's on-screen registration link on "Check your inbox" (§2.6 keeps it,
// signup.templ's ScreenLink). Every form posts to the wizard. The head's <link> elements are not
// links a person follows and are not read.
func appModeLinkProblems(name, body string) []string {
	var out []string
	legalPage := strings.HasPrefix(name, "legal-")
	own, err := url.Parse(testBaseURL)
	if err != nil {
		return []string{err.Error()}
	}
	screenLink := strings.TrimRight(testBaseURL, "/") + signupConfirmPath + "?t={{LINK}}"
	for _, m := range appModeAnchorRE.FindAllStringSubmatch(body, -1) {
		h := html.UnescapeString(m[1])
		switch {
		case strings.HasPrefix(h, "#"), strings.HasPrefix(h, "mailto:"), h == layout.CreditURL:
		case h == signupPath, strings.HasPrefix(h, signupPath+"?"), strings.HasPrefix(h, signupPath+"/"):
		case appModeLegalDocument(h):
		case h == "/account-deletion?client=app":
		case legalPage && strings.HasPrefix(h, "tel:"):
		case legalPage && appModeExternal(h, own.Host):
		case name == "signup-sent" && h == screenLink:
		default:
			out = append(out, name+": a link to "+h)
		}
	}
	for _, m := range appModeActionRE.FindAllStringSubmatch(body, -1) {
		if a := html.UnescapeString(m[1]); a != signupPath && !strings.HasPrefix(a, signupPath+"/") {
			out = append(out, name+": a form posting to "+a)
		}
	}
	return out
}

// appModeLegalDocument reports whether h is one of the four documents with the app's flag.
func appModeLegalDocument(h string) bool {
	m := appModeLegalRE.FindStringSubmatch(h)
	if m == nil {
		return false
	}
	for _, p := range pages.LegalPages {
		if p.Path == m[1] {
			return true
		}
	}
	return false
}

// appModeExternal reports whether h is a web page on a host other than this deployment's.
func appModeExternal(h, ownHost string) bool {
	u, err := url.Parse(h)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && !strings.EqualFold(u.Host, ownHost)
}

// appModeInAppCount is how many renders of the walk the app's in-app browser draws: the wizard's
// three steps and their refused forms, "Check your inbox", two problems, step one's "already with
// Taptime" answer, the four legal documents and one published.
const appModeInAppCount = 15

// TestAppMode_TheAppsPagesLinkOnlyInsideTheClosedSet [H54-a]: every page the app's in-app browser
// draws -- every wizard step, "Check your inbox", the problems a wizard in the app reaches, and every
// legal document opened with ?client=app -- links only inside ADR 0051 §2.6's closed set; what §2.6
// keeps (Built by, the four documents with the flag, the on-screen registration link) is there; and
// Start free, which points at the wizard and so passes the set, is gone with the rest of the header.
// The done page is not here: it lives under /admin, where the flag never arrives, and stays the system
// browser's page with its "Sign in to your dashboard" ([H54-b] holds it unchanged; ADR 0051 §2.6).
func TestAppMode_TheAppsPagesLinkOnlyInsideTheClosedSet(t *testing.T) {
	t.Parallel()
	inApp := 0
	for _, r := range appModeRenders(t, true) {
		if !r.inApp {
			continue
		}
		inApp++
		body := string(r.body)
		for _, p := range appModeLinkProblems(r.name, body) {
			t.Error(p)
		}
		if !strings.Contains(body, `href="`+layout.CreditURL+`"`) {
			t.Errorf("%s: Built by (layout.CreditURL) is gone; decision K-B keeps it", r.name)
		}
		for _, p := range pages.LegalPages {
			if !strings.Contains(body, `href="`+p.Path+`?client=app"`) {
				t.Errorf("%s: no link to %s?client=app", r.name, p.Path)
			}
		}
		for _, gone := range []string{">Start free<", `class="lp-headNav"`, `<details class="menu"`, `aria-label="Product"`, `class="lp-mark" href=`} {
			if strings.Contains(body, gone) {
				t.Errorf("%s still draws %q", r.name, gone)
			}
		}
		if r.name == "signup-sent" && !strings.Contains(body, signupConfirmPath+"?t={{LINK}}") {
			t.Errorf("signup-sent: the on-screen registration link is gone; ADR 0051 §2.6 keeps it")
		}
	}
	if inApp != appModeInAppCount {
		t.Errorf("checked %d in-app renders, appModeInAppCount says %d", inApp, appModeInAppCount)
	}
	// The scan refuses the same pages without the flag: it is not a scan that admits anything.
	for _, r := range appModeRenders(t, false) {
		if r.inApp && len(appModeLinkProblems(r.name, string(r.body))) == 0 {
			t.Errorf("%s without the flag passes the closed-set scan, so the scan proves nothing", r.name)
		}
	}
}

// ------------------------------------------------------------------------- [H54-c] --

// appModeWalk drives steps one and two from entry and returns step three's token.
func appModeWalk(t *testing.T, d *wizardDriver, entry string) string {
	t.Helper()
	start := d.get(entry)
	if rec := d.post(signupPath, businessForm(d.token(start))); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s = %d", signupPath, rec.Code)
	}
	places := d.get(signupPlacesPath)
	if rec := d.post(signupPlacesPath, placesForm(d.token(places), "St Julians", "Sliema")); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s = %d", signupPlacesPath, rec.Code)
	}
	return d.token(d.get(signupAccountPath))
}

// appModeBind is the binding half of the driver's synchronizer cookie.
func appModeBind(t *testing.T, d *wizardDriver) string {
	t.Helper()
	_, bind, ok := strings.Cut(d.jar[signupCookieName], ".")
	if !ok {
		t.Fatalf("no synchronizer cookie in the jar")
	}
	return bind
}

// TestAppMode_TheFlagLoosensNoCheck [H54-c]: every refusal of the wizard answers the same with the
// flag as without it, and opens no registration draft: a POST with no Origin, a forged synchronizer
// token, each step asked for before the one it needs, a form completed faster than a person types
// AFTER a long wait on the empty first page (the app's first page writes a state; its stamp must not
// start the minimum fill time), the honeypot, a state past its half hour, and a hand-made flag-only
// state carrying answers. And a state at the flag-only step without the flag is malformed.
func TestAppMode_TheFlagLoosensNoCheck(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		run  func(t *testing.T, d *wizardDriver, w *Signup, entry string) *httptest.ResponseRecorder
	}{
		{"a POST with no Origin", func(t *testing.T, d *wizardDriver, _ *Signup, entry string) *httptest.ResponseRecorder {
			start := d.get(entry)
			return d.do(http.MethodPost, signupPath, businessForm(d.token(start)), func(r *http.Request) { r.Header.Del("Origin") })
		}},
		{"a forged synchronizer token", func(t *testing.T, d *wizardDriver, _ *Signup, entry string) *httptest.ResponseRecorder {
			d.get(entry)
			return d.post(signupPath, businessForm("forged"))
		}},
		{"step two before step one", func(t *testing.T, d *wizardDriver, _ *Signup, entry string) *httptest.ResponseRecorder {
			start := d.get(entry)
			return d.post(signupPlacesPath, placesForm(d.token(start), "St Julians"))
		}},
		{"step three before step two", func(t *testing.T, d *wizardDriver, _ *Signup, entry string) *httptest.ResponseRecorder {
			start := d.get(entry)
			csrf := d.token(start)
			d.post(signupPath, businessForm(csrf))
			d.advance(time.Minute)
			return d.post(signupAccountPath, accountForm(csrf))
		}},
		{"Check your inbox before the last step", func(t *testing.T, d *wizardDriver, _ *Signup, entry string) *httptest.ResponseRecorder {
			d.get(entry)
			return d.get(signupSentPath)
		}},
		{"faster than a person types, after a wait on the empty first page", func(t *testing.T, d *wizardDriver, _ *Signup, entry string) *httptest.ResponseRecorder {
			start := d.get(entry)
			d.advance(10 * time.Minute)
			if rec := d.post(signupPath, businessForm(d.token(start))); rec.Code != http.StatusSeeOther {
				t.Fatalf("POST %s = %d", signupPath, rec.Code)
			}
			places := d.get(signupPlacesPath)
			d.post(signupPlacesPath, placesForm(d.token(places), "St Julians"))
			return d.post(signupAccountPath, accountForm(d.token(d.get(signupAccountPath))))
		}},
		{"the honeypot", func(t *testing.T, d *wizardDriver, _ *Signup, entry string) *httptest.ResponseRecorder {
			csrf := appModeWalk(t, d, entry)
			d.advance(time.Minute)
			form := accountForm(csrf)
			form.Set(signupHoneypotField, "filled")
			return d.post(signupAccountPath, form)
		}},
		{"a state past its half hour", func(t *testing.T, d *wizardDriver, _ *Signup, entry string) *httptest.ResponseRecorder {
			csrf := appModeWalk(t, d, entry)
			d.advance(signupTTL + signupFutureSkew + time.Minute)
			return d.post(signupAccountPath, accountForm(csrf))
		}},
		{"a flag-only state carrying answers", func(t *testing.T, d *wizardDriver, w *Signup, entry string) *httptest.ResponseRecorder {
			start := d.get(entry)
			blob, err := w.state.mint(signupDraft{Step: signupAppOnlyStep, App: true, Name: "Kebab Factory Ltd",
				VATNumber: "MT12345678", BusinessType: "restaurant", Structure: "multi", Venues: []string{"St Julians"}}, appModeBind(t, d))
			if err != nil {
				t.Fatal(err)
			}
			d.jar[signupStateCookieName] = blob
			d.advance(time.Minute)
			return d.post(signupAccountPath, accountForm(d.token(start)))
		}},
	}
	for _, c := range cases {
		var codes [2]int
		for i, app := range []bool{false, true} {
			p := &fakeProvisioner{}
			d, w := newSignupDriver(t, p, nil)
			entry := signupPath
			if app {
				entry = signupPath + "?client=app"
			}
			rec := c.run(t, d, w, entry)
			codes[i] = rec.Code
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s (app %v) = %d, want 400", c.name, app, rec.Code)
			}
			if n := p.count(); n != 0 {
				t.Errorf("%s (app %v) opened %d registration draft(s)", c.name, app, n)
			}
		}
		if codes[0] != codes[1] {
			t.Errorf("%s: %d without the flag, %d with it", c.name, codes[0], codes[1])
		}
	}

	// The flag-only step exists only with the flag.
	_, w := newSignupDriver(t, &fakeProvisioner{}, nil)
	blob, err := w.state.mint(signupDraft{Step: signupAppOnlyStep}, "bind")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.state.parse(blob, "bind"); !errors.Is(err, errSignupMalformed) {
		t.Errorf("a flag-only-step state without the flag parsed: %v, want errSignupMalformed", err)
	}
}

// ------------------------------------------------------------------------- [H54-d] --

// TestAppMode_TheAppLinksStayTheTapAndTheLiveQR [H54-d]: the paths the app claims from the browser
// (Universal Links / App Links) are exactly {/t, /q}, as written; the pages the app opens in its
// in-app browser -- the wizard, the legal documents, /account-deletion -- are not among them, so the
// in-app browser is never handed back to the app; and both served files list exactly those two.
func TestAppMode_TheAppLinksStayTheTapAndTheLiveQR(t *testing.T) {
	t.Parallel()
	if want := []string{"/t", "/q"}; !slices.Equal(appLinkPaths, want) {
		t.Fatalf("appLinkPaths = %v, want %v", appLinkPaths, want)
	}
	for _, p := range appLinkPaths {
		for _, page := range []string{signupPath, "/legal/privacy", "/account-deletion"} {
			if page == p || strings.HasPrefix(page, p+"/") {
				t.Errorf("the app link %s would take %s from the in-app browser", p, page)
			}
		}
	}
	h := appLinksRouter(t, "https://taptime.mt", fakeTeamID, []string{fakeCertA})
	for _, path := range []string{AppleAppSiteAssociationPath, AssetLinksPath} {
		body := appLinksDo(h, http.MethodGet, "taptime.mt", path).Body.String()
		if n := strings.Count(body, `{"/":`); n != 2 || !strings.Contains(body, `{"/":"/t"}`) || !strings.Contains(body, `{"/":"/q"}`) {
			t.Errorf("%s lists %d path(s): %s", path, n, body)
		}
	}
}

// ------------------------------------------------------------------------- [H54-e] --

// appModeDrawn reports whether a page is drawn for the app: its document links carry the flag and
// it has no link to the price.
func appModeDrawn(body string) bool {
	return strings.Contains(body, `href="/legal/privacy?client=app"`) && !strings.Contains(body, `href="/pricing"`)
}

// appModeOnlyDraft is the one registration draft the claims fake opened.
func appModeOnlyDraft(t *testing.T, f *fakeSignupClaims) []byte {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.claims) != 1 {
		t.Fatalf("%d drafts opened, want 1", len(f.claims))
	}
	for _, c := range f.claims {
		return c.draft
	}
	return nil
}

// TestAppMode_TheFlagTravelsInTheSignedStateCookie [H54-e]: GET /signup?client=app writes the flag into
// the wizard's signed state -- a state carrying the flag and nothing else -- and every later state
// carries it: each step is drawn for the app although its URL, its form and its redirect carry no flag,
// and the registration draft is byte for byte a browser's. The query is read on the first step only;
// without the state cookie there is no flag; and back at step one the cookie keeps the app's wizard
// the app's.
func TestAppMode_TheFlagTravelsInTheSignedStateCookie(t *testing.T) {
	t.Parallel()
	d, w := newSignupDriver(t, &fakeProvisioner{}, nil)
	state := func(want int) signupDraft {
		t.Helper()
		s, err := w.state.parse(d.jar[signupStateCookieName], appModeBind(t, d))
		if err != nil {
			t.Fatalf("the state cookie does not parse: %v", err)
		}
		if !s.App || s.Step != want {
			t.Fatalf("the state cookie is step %d with app %v, want step %d with the flag", s.Step, s.App, want)
		}
		return s
	}
	var bodies []string
	page := func(rec *httptest.ResponseRecorder, what string) {
		t.Helper()
		if !appModeDrawn(rec.Body.String()) {
			t.Fatalf("%s is not drawn for the app", what)
		}
		bodies = append(bodies, rec.Body.String())
	}
	next := func(rec *httptest.ResponseRecorder, to string) {
		t.Helper()
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != to {
			t.Fatalf("= %d -> %q, want 303 -> %q (no flag in the redirect)", rec.Code, rec.Header().Get("Location"), to)
		}
	}

	start := d.get(signupPath + "?client=app")
	page(start, "step one")
	if s := state(signupAppOnlyStep); s.Name != "" || s.VATNumber != "" || len(s.Venues) != 0 {
		t.Errorf("the first page's state carries more than the flag: %+v", s)
	}
	next(d.post(signupPath, businessForm(d.token(start))), signupPlacesPath)
	state(1)
	places := d.get(signupPlacesPath)
	page(places, "step two")
	page(d.post(signupPlacesPath, placesForm(d.token(places))), "step two refused")
	next(d.post(signupPlacesPath, placesForm(d.token(places), "St Julians", "Sliema")), signupAccountPath)
	state(2)
	account := d.get(signupAccountPath)
	page(account, "step three")
	d.advance(time.Minute)
	next(d.post(signupAccountPath, accountForm(d.token(account))), signupSentPath)
	state(signupDoneStep)
	page(d.get(signupSentPath), "Check your inbox")
	for _, b := range bodies {
		if strings.Contains(b, `name="client"`) {
			t.Errorf("a page carries the flag in a form field")
		}
		for _, m := range appModeActionRE.FindAllStringSubmatch(b, -1) {
			if strings.Contains(m[1], "?") {
				t.Errorf("a form posts to %q", m[1])
			}
		}
	}

	// The draft is a browser's, byte for byte (admin_signup_claims_draft_closed has no key for a flag).
	b, _ := newSignupDriver(t, &fakeProvisioner{}, nil)
	csrf := appModeWalk(t, b, signupPath)
	b.advance(time.Minute)
	next(b.post(signupAccountPath, accountForm(csrf)), signupSentPath)
	if app, browser := appModeOnlyDraft(t, d.claims), appModeOnlyDraft(t, b.claims); !bytes.Equal(app, browser) {
		t.Errorf("the app's registration draft differs from a browser's:\n    app %s\nbrowser %s", app, browser)
	}

	// The query is read on the first step only.
	q, _ := newSignupDriver(t, &fakeProvisioner{}, nil)
	qs := q.get(signupPath)
	q.post(signupPath, businessForm(q.token(qs)))
	if rec := q.get(signupPlacesPath + "?client=app"); rec.Code != http.StatusOK || appModeDrawn(rec.Body.String()) {
		t.Errorf("a browser's step two asked for with ?client=app = %d, drawn for the app %v", rec.Code, appModeDrawn(rec.Body.String()))
	}

	// Without the state cookie there is no flag.
	c, _ := newSignupDriver(t, &fakeProvisioner{}, nil)
	cs := c.get(signupPath + "?client=app")
	c.post(signupPath, businessForm(c.token(cs)))
	delete(c.jar, signupStateCookieName)
	if rec := c.get(signupPlacesPath); rec.Code != http.StatusBadRequest || appModeDrawn(rec.Body.String()) {
		t.Errorf("step two with no state cookie = %d, drawn for the app %v", rec.Code, appModeDrawn(rec.Body.String()))
	}

	// Back at step one with no flag in the URL, the app's wizard is still the app's.
	e, _ := newSignupDriver(t, &fakeProvisioner{}, nil)
	es := e.get(signupPath + "?client=app")
	e.post(signupPath, businessForm(e.token(es)))
	if rec := e.get(signupPath); !appModeDrawn(rec.Body.String()) {
		t.Errorf("back at step one, the app's wizard is drawn for the browser")
	}
}
