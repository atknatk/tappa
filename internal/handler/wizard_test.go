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
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
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
// here before it ships. Back links exist on every step after the first; the
// waiting screen has no forward link at all, because its next action is physical.
func TestWizard_EveryStepPointsOnlyAtItsOwnFlow(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	for _, tc := range []struct {
		target string
		cookie *http.Cookie
		want   []string
	}{
		{"/activate?step=1", codeCookie(), []string{"/activate?step=2 -> Next"}},
		{"/activate?step=2", codeCookie(), []string{"/activate?step=1 -> Back"}},
		{"/activate?step=3", pendingCookie(), []string{"/activate?step=4 -> I'm ready", "/activate?step=2 -> Back"}},
		{"/activate?step=4", pendingCookie(), []string{"/activate?step=3 -> Back"}},
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

// TestWizard_ConsentIsTheOnlyForm: step 2's form posts to /api/activate with the
// synchronizer token and a REQUIRED consent box, and no other step has a form.
func TestWizard_ConsentIsTheOnlyForm(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	for step, cookie := range map[string]*http.Cookie{"1": codeCookie(), "2": codeCookie(), "3": pendingCookie(), "4": pendingCookie()} {
		body := get(t, h, "/activate?step="+step, cookie).Body.String()
		forms := strings.Count(body, "<form")
		if step == "2" {
			if forms != 1 || !strings.Contains(body, `<form method="post" action="/api/activate"`) {
				t.Errorf("step 2 must carry exactly the consent form; %d forms", forms)
			}
			if !regexp.MustCompile(`name="consent" value="yes" required`).MatchString(body) {
				t.Error("the consent box must be required")
			}
			if !strings.Contains(body, `name="csrf" value="`+fakeCSRF+`"`) {
				t.Error("the consent form must echo the synchronizer token")
			}
			continue
		}
		if forms != 0 {
			t.Errorf("step %s carries %d forms; only the consent step may", step, forms)
		}
	}
}

// TestWizard_AConsentedReturnKeepsTheBoxTicked: going Back to step 2 after
// agreeing shows the box as agreed, so the person is not asked to think they
// have not consented.
func TestWizard_AConsentedReturnKeepsTheBoxTicked(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	if body := get(t, h, "/activate?step=2", pendingCookie()).Body.String(); !strings.Contains(body, `value="yes" required checked`) {
		t.Error("a consented browser's step 2 must show the box ticked")
	}
	if body := get(t, h, "/activate?step=2", codeCookie()).Body.String(); strings.Contains(body, `value="yes" required checked`) {
		t.Error("an unconsented browser's step 2 must not pre-tick consent")
	}
}

// TestWizard_TellsTheEmployeeAboutTheBrowserTrap: the NFC tap opens the phone's
// default browser, so the get-ready step must say to finish in Safari/Chrome when
// the link was opened inside another app — or the session lands in a browser the
// plaque never opens.
func TestWizard_TellsTheEmployeeAboutTheBrowserTrap(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	body := get(t, h, "/activate?step=3", pendingCookie()).Body.String()
	for _, want := range []string{"Stay in this browser", "Safari or Chrome", "chat or email app"} {
		if !strings.Contains(body, want) {
			t.Errorf("the get-ready step does not say %q", want)
		}
	}
}

// TestWizard_WaitingScreenNamesTheEmployerAndSaysTheTapIsNotAttendance: the copy
// that matters on step 4 — any plaque of THIS employer, and the first tap finishes
// setup without clocking anybody in (ADR 0026).
func TestWizard_WaitingScreenNamesTheEmployerAndSaysTheTapIsNotAttendance(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	body := get(t, h, "/activate?step=4", pendingCookie()).Body.String()
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
