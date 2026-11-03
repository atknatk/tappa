package handler

// The activation wizard (ADR 0026) against FAKES: where each step's links point,
// that nothing on a step runs inline script, that every touch target is large
// enough, and that the waiting screen's script obeys its own rules. The database
// half — consent records nothing but consent, the tap activates — is in
// e2e_db_test.go.
//
// This file replaced tour_test.go (the M5-07 mini tour, folded into the wizard's
// first step). anchorsOf/anchorRE moved here with it because other screens' tests
// use them.

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/web/templates/layout"
	"github.com/atknatk/tappa/web/templates/pages"
)

// anchorsOf returns every anchor of a rendered page as "href -> label", in
// document order. The label is the anchor's text with runs of whitespace
// collapsed, which is what a person reads; an anchor with no text yields
// "href -> " and therefore matches nothing on an expected list.
func anchorsOf(body string) []string {
	var out []string
	for _, m := range anchorRE.FindAllStringSubmatch(body, -1) {
		href := ""
		if h := attrValueRE("href").FindStringSubmatch(m[1]); len(h) == 2 {
			href = h[1]
		}
		out = append(out, href+" -> "+strings.Join(strings.Fields(m[2]), " "))
	}
	return out
}

var (
	anchorRE = regexp.MustCompile(`(?is)(<a\b[^>]*>)(.*?)</a>`)
	// Any HTML event-handler attribute. Deliberately the whole `on…=` family
	// rather than a list of names: the list is the part that would go stale.
	inlineHandlerRE = regexp.MustCompile(`(?i)\son[a-z]+\s*=`)
)

// TestWizard_EveryStepPointsOnlyAtItsOwnFlow pins the exact set of addresses each
// step offers, in order. A link added to a step — a help page, a menu, a way to
// "skip setup" — is a change to a screen every new employee sees, and it fails
// here before it ships. Step 1's one way forward is still the agree button: its links are
// the terms sheet's (user §9 decision of 2026-11-03), which opens and closes in the page, and
// the two legal documents inside it. The waiting screen has no forward link, because its next
// action is physical.
func TestWizard_EveryStepPointsOnlyAtItsOwnFlow(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	for _, tc := range []struct {
		target string
		cookie *http.Cookie
		want   []string
	}{
		{"/activate?step=1", codeCookie(), []string{
			"#terms -> terms and privacy details.",
			"/legal/privacy -> Privacy policy",
			"/legal/terms -> Terms of service",
			"#terms-open -> Close",
		}},
		{"/activate?step=2", pendingCookie(), []string{"/activate?step=3 -> I'm ready", "/activate?step=1 -> Back"}},
		{"/activate?step=3", pendingCookie(), []string{"/activate?step=2 -> Back"}},
	} {
		body := get(t, h, tc.target, tc.cookie).Body.String()
		got := anchorsOf(body)
		if strings.Join(got, " | ") != strings.Join(tc.want, " | ") {
			t.Errorf("%s anchors = %q, want %q", tc.target, got, tc.want)
		}
		if inlineHandlerRE.MatchString(body) || strings.Contains(body, "<script>") || strings.Contains(body, "style=") {
			t.Errorf("%s carries inline script or style; the CSP forbids both", tc.target)
		}
	}
}

// TestWizardStep_ClampsAndStaleLinksAreSafe is the whole table of wizardStep (user §9
// decision of 2026-11-02: three steps). An unconsented browser only ever gets step 1, so
// nothing past the notice shows before agreeing; a number out of range is what a link
// from the four-step wizard now is (?step=4), and it behaves as a bare /activate does.
func TestWizardStep_ClampsAndStaleLinksAreSafe(t *testing.T) {
	if pages.ActivateSteps != 2 || pages.ActivateStepTap != 3 {
		t.Fatalf("ActivateSteps = %d, ActivateStepTap = %d, want 2 and 3", pages.ActivateSteps, pages.ActivateStepTap)
	}
	for _, tc := range []struct {
		step      string
		consented bool
		want      int
	}{
		{"", false, 1},
		{"0", false, 1},
		{"1", false, 1},
		{"2", false, 1},
		{"3", false, 1},
		{"4", false, 1},
		{"nonsense", false, 1},
		{"", true, 3},
		{"0", true, 3},
		{"1", true, 1},
		{"2", true, 2},
		{"3", true, 3},
		{"4", true, 3},
		{"nonsense", true, 3},
	} {
		r := httptest.NewRequest(http.MethodGet, "/activate?step="+tc.step, nil)
		if got := wizardStep(r, tc.consented); got != tc.want {
			t.Errorf("?step=%q consented=%v -> step %d, want %d", tc.step, tc.consented, got, tc.want)
		}
	}
}

// TestWizard_TheProgressBarHasOneSegmentPerStep: three segments in the three-column grid,
// and the label counts to three. The grid's class is a literal the stylesheet must carry;
// an uncompiled one would stack the bar's segments.
func TestWizard_TheProgressBarHasOneSegmentPerStep(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	for _, tc := range []struct {
		target string
		cookie *http.Cookie
		label  string
	}{
		{"/activate?step=1", codeCookie(), "Step 1 of 3 · Welcome"},
		{"/activate?step=2", pendingCookie(), "Step 2 of 3 · Get ready"},
		{"/activate?step=3", pendingCookie(), "Step 3 of 3 · Tap"},
	} {
		body := get(t, h, tc.target, tc.cookie).Body.String()
		if n := strings.Count(body, `<li class="wizard-segment`); n != 3 {
			t.Errorf("%s: %d segments, want 3", tc.target, n)
		}
		if !strings.Contains(body, `grid grid-cols-3 gap-1.5`) {
			t.Errorf("%s: the bar is not the three-column grid", tc.target)
		}
		if text := strings.Join(strings.Fields(screenText(t, body)), " "); !strings.Contains(text, tc.label) {
			t.Errorf("%s does not say %q:\n%s", tc.target, tc.label, text)
		}
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "web", "static", "css", "app.css"))
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("web/static/css/app.css is not built (make css)")
	}
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), ".grid-cols-3{") {
		t.Error("app.css has no rule for the bar's grid")
	}
}

// agreeButton is the wizard's one consent control (user §9 decision of 2026-11-02). The
// value is on the button, not in a hidden input: the server's check means somebody
// pressed it.
const agreeButton = `<button type="submit" name="consent" value="yes" class="tap-button">Agree and continue</button>`

// TestWizard_TheAgreeButtonIsTheOnlyForm: step 1's form posts to /api/activate with the
// synchronizer token and one button carrying the consent value. There is no consent box
// and no hidden consent field, and no other step has a form.
func TestWizard_TheAgreeButtonIsTheOnlyForm(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	body := get(t, h, "/activate?step=1", codeCookie()).Body.String()
	if n := strings.Count(body, "<form"); n != 1 || !strings.Contains(body, `<form method="post" action="/api/activate"`) {
		t.Errorf("step 1 must carry exactly the consent form; %d forms", n)
	}
	if n := strings.Count(body, agreeButton); n != 1 {
		t.Errorf("agree buttons = %d, want 1\n%s", n, body)
	}
	if n := strings.Count(body, `name="consent"`); n != 1 {
		t.Errorf("%d elements are named consent; only the button may be", n)
	}
	if strings.Contains(body, `type="checkbox"`) {
		t.Error("the ordinary step 1 carries a box; there is nothing to tick")
	}
	if !strings.Contains(body, `name="csrf" value="`+fakeCSRF+`"`) {
		t.Error("the consent form must echo the synchronizer token")
	}
	for _, gone := range []string{"wizard-consent", "Setting up this phone for", "How it works", "about a minute", `class="wizard-back"`} {
		if strings.Contains(body, gone) {
			t.Errorf("step 1 still carries %q", gone)
		}
	}
	for step, cookie := range map[string]*http.Cookie{"2": pendingCookie(), "3": pendingCookie()} {
		if forms := strings.Count(get(t, h, "/activate?step="+step, cookie).Body.String(), `action="/api/activate"`); forms != 0 {
			t.Errorf("step %s carries %d consent forms; only step 1 may", step, forms)
		}
	}
}

// wizardLayers splits a rendered step 1 into what is on screen and the terms sheet, which
// shows only while the address ends in #terms. Neither half can hide in the other: the sheet
// is one <section> with no <section> inside it.
func wizardLayers(t *testing.T, body string) (screen, sheet string) {
	t.Helper()
	start := strings.Index(body, `<section id="terms"`)
	if start < 0 {
		t.Fatalf("step 1 has no terms sheet:\n%s", body)
	}
	end := start + strings.Index(body[start:], "</section>") + len("</section>")
	if strings.Contains(body[start+1:end], "<section") {
		t.Fatal("the terms sheet holds another section; wizardLayers would cut it short")
	}
	return body[:start] + body[end:], `<body>` + body[start:end]
}

// renderStep1 renders step 1 directly, for the shapes the fake handler cannot ask for.
func renderStep1(t *testing.T, v pages.ActivateView) string {
	t.Helper()
	v.EmployeeName, v.Step = "Maria Borg", 1
	var page bytes.Buffer
	if err := pages.Activate(v, layout.Brand{}).Render(context.Background(), &page); err != nil {
		t.Fatal(err)
	}
	return page.String()
}

// TestWizard_TheNoticeKeepsItsFacts reads step 1's two layers the way a person does (user §9
// decision of 2026-11-03). The screen holds the short panel and none of the detail; the sheet
// holds who is responsible, who processes it, what is recorded, what for and where to read more,
// pinned as WHOLE TEXT so an added sentence fails here. By the user's decision of 2026-11-03
// neither layer gives a retention period or a rights statement, and the forbidden list says so.
// It makes no claim of legal sufficiency: those and the legal basis have to be in the published
// privacy policy, which M8-06's gate verifies and no test here can.
func TestWizard_TheNoticeKeepsItsFacts(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	screen, sheet := wizardLayers(t, get(t, h, "/activate?step=1", codeCookie()).Body.String())

	text := screenText(t, screen)
	for _, want := range []string{
		"Kebab Factory Ltd",
		"Hi Maria Borg",
		"Let's set up your phone.",
		"Kebab Factory Ltd uses Taptime to record your shifts. Each tap saves the time, your phone's IP address and, if your phone shares it, your location at that moment. Your location is never tracked between taps. No fingerprints, no face, no voice.",
		"By continuing, you agree to the terms and privacy details.",
		"Agree and continue",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the screen does not say %q:\n%s", want, text)
		}
	}
	for _, gone := range []string{"Nothing is recorded between taps", "What Taptime records", "When you tap", "Your data", "Who is responsible", "password"} {
		if strings.Contains(text, gone) {
			t.Errorf("the screen still carries %q; the details belong in the sheet:\n%s", gone, text)
		}
	}
	if strings.Contains(screen, "<ul") || strings.Contains(screen, "<h2") || strings.Contains(screen, "<h3") {
		t.Error("the screen's panel has a list or a sub-heading; it is one short paragraph")
	}

	wantSheet := func(employer, why string) string {
		return "Terms and privacy Who is responsible " + employer + " is responsible for your attendance records. Taptime processes them on its behalf. " +
			"What is recorded The time of each tap, the venue, your phone's IP address and, if your phone shares it, your location at that moment. A cookie keeps this phone signed in, so you don't need a password. Your location is never tracked between taps. No fingerprints, no face, no voice. " +
			"Why To record your working hours for " + why + " For more information, visit: Privacy policy Terms of service Close"
	}
	if got, want := screenText(t, sheet), wantSheet("Kebab Factory Ltd", "Kebab Factory Ltd."); got != want {
		t.Errorf("the sheet says:\n%s\nwant:\n%s", got, want)
	}

	_, stop := wizardLayers(t, renderStep1(t, pages.ActivateView{EmployerName: "Kebab Factory Ltd."}))
	if got, want := screenText(t, stop), wantSheet("Kebab Factory Ltd.", "Kebab Factory Ltd."); got != want {
		t.Errorf("a name ending in a full stop reads:\n%s\nwant:\n%s", got, want)
	}

	// The whole page, this development deployment included: nothing about the period, the rights
	// or the placeholder notice that only made sense beside the period.
	whole := screenText(t, screen) + " " + screenText(t, sheet)
	for _, gone := range []string{"Kept for", "How long", "year", "retention", "Your rights", "GDPR", "ask your manager", "data protection authority", "Full documents", "Non-production", "placeholder"} {
		if strings.Contains(whole, gone) {
			t.Errorf("step 1 still says %q:\n%s", gone, whole)
		}
	}
	if !strings.Contains(sheet, `<p class="terms-body mt-4">For more information, visit:</p>`) {
		t.Error("the line above the legal links is not a plain paragraph; no heading goes above it")
	}
}

// TestWizard_TheTermsLinkSitsDirectlyAboveTheButton: what pressing the button agrees to is said
// at the button, on every shape of the form, once. Only the sentence's own end separates the link
// from the button, so a warning or the take-over box cannot come between them.
func TestWizard_TheTermsLinkSitsDirectlyAboveTheButton(t *testing.T) {
	inv, sess := victimHolds(t)
	victim := &http.Cookie{Name: session.CookieName, Value: victimSession}
	for name, body := range map[string]string{
		"ordinary":  get(t, newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{}), "/activate?step=1", codeCookie()).Body.String(),
		"take-over": get(t, newHandler(t, inv, sess, &fakeAudit{}), "/activate?step=1", codeCookie(), victim).Body.String(),
	} {
		link := `href="#terms"`
		if n := strings.Count(body, link); n != 1 {
			t.Errorf("%s: %d links open the sheet, want 1", name, n)
		}
		at, button := strings.Index(body, link), strings.Index(body, "<button")
		if at < 0 || button < at {
			t.Fatalf("%s: link at %d, button at %d", name, at, button)
		}
		if between := strings.TrimSpace(body[at:button]); !strings.HasSuffix(between, "terms and privacy details.</a></p>") {
			t.Errorf("%s: something sits between the link and the button: %q", name, between)
		}
		if panel := strings.Index(body, `docket wizard-panel`); panel < 0 || panel > at {
			t.Errorf("%s: panel at %d, link at %d; the link comes after the panel", name, panel, at)
		}
	}
	// The form's error stays, and the sentence stays below it, against the button.
	missing := post(t, newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{}), url.Values{"csrf": {fakeCSRF}}, codeCookie()).Body.String()
	if e, l := strings.Index(missing, `id="consent-error"`), strings.Index(missing, `href="#terms"`); e < 0 || l < e {
		t.Errorf("consent error at %d, link at %d; want the error above the link", e, l)
	}
}

// TestWizard_TheTermsSheetIsOutsideTheFormAndNeedsNoScript covers the mechanism on all three
// shapes of step 1 (user §9 decision of 2026-11-03): the sheet follows the form rather than
// sitting in it, the page carries no script at all, the dialog is named and can take focus, Close
// goes to an id that exists, and the legal documents open in a new tab so the wizard is not lost.
func TestWizard_TheTermsSheetIsOutsideTheFormAndNeedsNoScript(t *testing.T) {
	inv, sess := victimHolds(t)
	victim := &http.Cookie{Name: session.CookieName, Value: victimSession}
	plain := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	for name, body := range map[string]string{
		"ordinary":        get(t, plain, "/activate?step=1", codeCookie()).Body.String(),
		"take-over":       get(t, newHandler(t, inv, sess, &fakeAudit{}), "/activate?step=1", codeCookie(), victim).Body.String(),
		"consent-missing": post(t, plain, url.Values{"csrf": {fakeCSRF}}, codeCookie()).Body.String(),
	} {
		if strings.Contains(body, "<script") {
			t.Errorf("%s: step 1 carries a script", name)
		}
		if n := strings.Count(body, `id="terms"`); n != 1 {
			t.Errorf("%s: %d elements are #terms, want 1", name, n)
		}
		formEnd, sheetAt := strings.Index(body, "</form>"), strings.Index(body, `<section id="terms"`)
		if formEnd < 0 || sheetAt < formEnd {
			t.Errorf("%s: the form ends at %d, the sheet starts at %d; the sheet must not be in the form", name, formEnd, sheetAt)
		}
		_, sheet := wizardLayers(t, body)
		for _, want := range []string{
			`role="dialog"`, `aria-labelledby="terms-title"`, `id="terms-title"`, `tabindex="-1"`,
			`<a href="/legal/privacy" target="_blank" rel="noopener"`,
			`<a href="/legal/terms" target="_blank" rel="noopener"`,
			`<a href="#terms-open"`,
		} {
			if !strings.Contains(sheet, want) {
				t.Errorf("%s: the sheet lost %s", name, want)
			}
		}
		if !strings.Contains(body, `id="terms-open" href="#terms"`) {
			t.Errorf("%s: Close points at an id the page does not carry", name)
		}
		for _, control := range []string{"<input", "<button", "<form", "style="} {
			if strings.Contains(sheet, control) {
				t.Errorf("%s: the sheet holds %s; it is reading text and links", name, control)
			}
		}
	}
	for _, step := range []string{"2", "3"} {
		if strings.Contains(get(t, plain, "/activate?step="+step, pendingCookie()).Body.String(), `id="terms"`) {
			t.Errorf("step %s carries the terms sheet; only step 1 may", step)
		}
	}
}

// TestCompiledCSS_TheTermsSheetOpensOnTargetAndReserves44px reads the built stylesheet, because
// the HTML is identical whether or not the mechanism works: the sheet is out of the page until
// :target, fixed above it, scrolls in its middle, and its controls and the opening link reserve
// 44 px (min-h-11 is 2.75rem).
func TestCompiledCSS_TheTermsSheetOpensOnTargetAndReserves44px(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "web", "static", "css", "app.css"))
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("web/static/css/app.css is not built (make css)")
	}
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	rule := func(selector string) string {
		t.Helper()
		at := strings.Index(css, selector+"{")
		if at < 0 {
			t.Fatalf("app.css has no rule %s", selector)
		}
		at += len(selector) + 1
		return css[at : at+strings.Index(css[at:], "}")]
	}
	for _, tc := range []struct{ selector, want string }{
		{".terms-sheet", "position:fixed"},
		{".terms-sheet", "display:none"},
		{".terms-sheet:target", "display:flex"},
		{".terms-scroll", "overflow-y:auto"},
		{".terms-doc", "min-height:2.75rem"},
		{".terms-close", "min-height:2.75rem"},
		{".hit-link:after", "min-height:2.75rem"},
		{".terms-open", "underline"},
	} {
		if got := rule(tc.selector); !strings.Contains(got, tc.want) {
			t.Errorf("%s = {%s}, want %s", tc.selector, got, tc.want)
		}
	}
}

// termsRuleRE finds every compiled rule whose selector names a terms-* class.
var termsRuleRE = regexp.MustCompile(`([^{}]*\.terms-[^{}]*)\{([^{}]*)\}`)

// TestCompiledCSS_TheTermsSheetStaysModalDimmedAndOnPalette reads the built stylesheet, because
// none of this shows in the HTML. Removing the visibility rule would leave aria-modal claiming
// something false; removing the dim would leave the sheet floating on the page. Every terms-*
// rule keeps to the palette and generates no text. A colour property may name only a palette
// colour written out, so var(), and with it the tenant's accent, fails here too.
func TestCompiledCSS_TheTermsSheetStaysModalDimmedAndOnPalette(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "web", "static", "css", "app.css"))
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("web/static/css/app.css is not built (make css)")
	}
	if err != nil {
		t.Fatal(err)
	}
	rules := termsRuleRE.FindAllStringSubmatch(string(raw), -1)
	if len(rules) < 10 {
		t.Fatalf("found %d terms-* rule(s); the sheet has more, so this scan is reading the wrong file", len(rules))
	}

	var modal, dim bool
	for _, r := range rules {
		selector, body := r[1], r[2]
		if strings.Contains(selector, ".wizard:has(.terms-sheet:target)>:not(.terms-sheet)") && strings.Contains(body, "visibility:hidden") {
			modal = true
		}
		if selector == ".terms-sheet" && strings.Contains(body, "background-color:rgba(21,34,25,.6)") {
			dim = true
		}
	}
	if !modal {
		t.Error("no rule hides the wizard behind the open sheet; aria-modal would be false")
	}
	if !dim {
		t.Error(".terms-sheet lost its dim (bg-ink/60)")
	}

	palette := map[[3]float64]bool{}
	for _, c := range brandPalette(t) {
		palette[c] = true
	}
	onPalette := func(selector, written string, r, g, b uint64) {
		t.Helper()
		if !palette[[3]float64{float64(r), float64(g), float64(b)}] {
			t.Errorf("%s uses %s, which is not on the palette", selector, written)
		}
	}
	hexRE := regexp.MustCompile(`#([0-9a-fA-F]{3,8})\b`)
	rgbRE := regexp.MustCompile(`rgba?\(\s*(\d+)[ ,]+(\d+)[ ,]+(\d+)[^)]*?(?:var\([^)]*\))?[^)]*\)`)
	wordRE := regexp.MustCompile(`\b[a-zA-Z][a-zA-Z-]*\b`)
	colourProp := regexp.MustCompile(`color|background|border|outline|shadow|fill|stroke|caret|accent|decoration`)
	plainWords := map[string]bool{"transparent": true, "solid": true, "none": true, "underline": true}
	for _, r := range rules {
		selector, body := strings.TrimSpace(r[1]), r[2]
		for _, m := range hexRE.FindAllStringSubmatch(body, -1) {
			h := m[1]
			if len(h) == 3 || len(h) == 4 {
				h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
			}
			if len(h) < 6 {
				t.Errorf("%s has an unreadable colour %s", selector, m[0])
				continue
			}
			c, _ := strconv.ParseUint(h[:6], 16, 32)
			onPalette(selector, m[0], c>>16, c>>8&0xff, c&0xff)
		}
		for _, m := range rgbRE.FindAllStringSubmatch(body, -1) {
			r, _ := strconv.ParseUint(m[1], 10, 8)
			g, _ := strconv.ParseUint(m[2], 10, 8)
			b, _ := strconv.ParseUint(m[3], 10, 8)
			onPalette(selector, m[0], r, g, b)
		}
		for _, decl := range strings.Split(body, ";") {
			name, value, ok := strings.Cut(decl, ":")
			if !ok {
				continue
			}
			name = strings.TrimSpace(name)
			if name == "content" || name == "--tw-content" {
				t.Errorf("%s generates content (%s); words on this sheet come from the template", selector, decl)
			}
			if strings.HasPrefix(name, "--") || !colourProp.MatchString(name) {
				continue
			}
			rest := rgbRE.ReplaceAllString(hexRE.ReplaceAllString(value, ""), "")
			for _, w := range wordRE.FindAllString(rest, -1) {
				if !plainWords[strings.ToLower(w)] {
					t.Errorf("%s declares %s, and %q is not a palette colour written out", selector, decl, w)
				}
			}
		}
	}
}

// TestWizard_TheWarningsSitAboveTheAgreeButtonOnce: another person's session on this phone
// and this person's other phone are said once, on step 1, before the control that agrees.
// The take-over box stays, required: it is a safety control, not reading text.
func TestWizard_TheWarningsSitAboveTheAgreeButtonOnce(t *testing.T) {
	inv, sess := victimHolds(t)
	h := newHandler(t, inv, sess, &fakeAudit{})
	victim := &http.Cookie{Name: session.CookieName, Value: victimSession}
	body := get(t, h, "/activate?step=1", codeCookie(), victim).Body.String()
	warn := strings.Index(body, "This phone belongs to someone else")
	box := strings.Index(body, `name="switch" value="yes" required`)
	button := strings.Index(body, agreeButton)
	if warn < 0 || box < 0 || button < 0 || warn > box || box > button {
		t.Errorf("warning at %d, take-over box at %d, agree button at %d; want them in that order", warn, box, button)
	}
	if n := strings.Count(body, "This phone belongs to someone else"); n != 1 {
		t.Errorf("the warning is drawn %d times, want once", n)
	}
	if n := strings.Count(body, `type="checkbox"`); n != 1 {
		t.Errorf("%d boxes, want the take-over box alone", n)
	}
	if !strings.Contains(body, "accent-tomato") {
		t.Error("the take-over box lost its tomato")
	}

	second := newHandler(t, &fakeInvites{lookup: func(invite.Code) (invite.Context, error) { return okContext("active"), nil }}, &fakeSessions{}, &fakeAudit{})
	page := get(t, second, "/activate?step=1", codeCookie()).Body.String()
	warn, button = strings.Index(page, "This is a new phone"), strings.Index(page, agreeButton)
	if warn < 0 || button < 0 || warn > button {
		t.Errorf("new-phone warning at %d, agree button at %d", warn, button)
	}
}

// TestWizard_AHandMadePostGetsTheButtonsWords: the only way to reach the consent-missing
// form now is a POST that did not come from the button. The notice names the button, not
// a box, and is tied to it.
func TestWizard_AHandMadePostGetsTheButtonsWords(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	body := post(t, h, url.Values{"csrf": {fakeCSRF}}, codeCookie()).Body.String()
	if !strings.Contains(body, `<p id="consent-error"`) || !strings.Contains(body, "Please press Agree and continue so we know you have read this.") {
		t.Errorf("the notice is missing:\n%s", body)
	}
	if !strings.Contains(body, `aria-describedby="consent-error"`) {
		t.Error("the button is not described by the notice")
	}
	if strings.Contains(body, "tick the box") {
		t.Error("the notice refers to a box that no longer exists")
	}
}

// TestWizard_TellsTheEmployeeAboutTheBrowserTrap: the NFC tap opens the phone's
// default browser, so the get-ready step must say to finish in Safari/Chrome when
// the link was opened inside another app — or the session lands in a browser the
// plaque never opens.
func TestWizard_TellsTheEmployeeAboutTheBrowserTrap(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	body := get(t, h, "/activate?step=2", pendingCookie()).Body.String()
	for _, want := range []string{"Stay in this browser", "Safari or Chrome", "chat or email app"} {
		if !strings.Contains(body, want) {
			t.Errorf("the get-ready step does not say %q", want)
		}
	}
}

// TestWizard_WaitingScreenNamesTheEmployerAndSaysTheTapIsNotAttendance: the copy
// that matters on step 3 — any plaque of THIS employer, and the first tap finishes
// setup without clocking anybody in (ADR 0026).
func TestWizard_WaitingScreenNamesTheEmployerAndSaysTheTapIsNotAttendance(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	body := get(t, h, "/activate?step=3", pendingCookie()).Body.String()
	for _, want := range []string{"Now tap the plaque", "Kebab Factory Ltd", "doesn't clock you in", `role="status"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the waiting screen does not carry %q", want)
		}
	}
}

// TestActivateScript_KeepsItsPromises reads web/static/js/activate.js: it must not
// touch location, storage or another origin, must stop polling, and must only flip
// `hidden` (no markup injection). A scanner rather than a browser test, because
// what is pinned is what the file can DO, and that is in its text.
func TestActivateScript_KeepsItsPromises(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "web", "static", "js", "activate.js"))
	if err != nil {
		t.Fatalf("read activate.js: %v", err)
	}
	code := stripJSComments(string(src))
	for _, banned := range []string{"geolocation", "localStorage", "sessionStorage", "document.cookie",
		"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval(", "http://", "https://", "setInterval"} {
		if strings.Contains(code, banned) {
			t.Errorf("activate.js uses %q", banned)
		}
	}
	for _, want := range []string{"MAX_WAIT_MS", "credentials: 'same-origin'", "cache: 'no-store'", ".hidden = "} {
		if !strings.Contains(code, want) {
			t.Errorf("activate.js lost %q", want)
		}
	}
}

// stripJSComments drops // line comments and /* */ blocks, so the scan above reads
// code rather than the prose explaining what the code does not do.
func stripJSComments(s string) string {
	s = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(s, "")
	return regexp.MustCompile(`(?m)^\s*//.*$`).ReplaceAllString(s, "")
}
