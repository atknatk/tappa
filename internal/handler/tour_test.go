package handler

// The mini tour (M5-07) against FAKES: the three slides, the clamp, the session
// gate, and the exact set of addresses the slides point at. What these cannot
// measure is the thing the card cares most about — that walking or skipping the
// tour writes NOTHING and that the practice flag still lands on the first record.
// That is a property of the database and is measured in tour_db_test.go.

import (
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/web/templates/pages"
)

// tourCookie is the session cookie a phone holds after activation. The fake
// session manager accepts any value; what is being exercised here is the
// handler's gate, not the token format (session's own tests own that).
func tourCookie() *http.Cookie {
	return &http.Cookie{Name: session.CookieName, Value: "FAKEsessionFAKEsessionFAKEsessionFAKEsess12"}
}

// TestTour_HasExactlyTourStepsSlides ties the number in pages.TourSteps to the
// slides that actually render, which is the tripwire agent-brief.md asks for
// whenever prose counts something out loud: every slide says "Step N of
// <TourSteps>", so a slide added without moving the constant would either be
// unreachable or would mislabel every other slide.
//
// It asserts three things at once, and they are three because any one alone can
// be satisfied by a broken tour:
//
//   - each step 1..TourSteps renders a DISTINCT heading (no slide is a copy),
//   - each one labels itself with its own number and the constant, and
//   - TourSteps+1 renders the same body as step 1, i.e. there is no further slide
//     hiding behind a larger number.
func TestTour_HasExactlyTourStepsSlides(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})

	bodies := map[int]string{}
	headings := map[string]int{}
	for step := 1; step <= pages.TourSteps; step++ {
		w := get(t, h, "/activate/tour?step="+strconv.Itoa(step), tourCookie())
		if w.Code != http.StatusOK {
			t.Fatalf("step %d: status = %d, want 200", step, w.Code)
		}
		body := w.Body.String()
		bodies[step] = body

		want := "Step " + strconv.Itoa(step) + " of " + strconv.Itoa(pages.TourSteps)
		if !strings.Contains(body, want) {
			t.Errorf("step %d does not label itself %q", step, want)
		}
		h1 := headingOf(t, body)
		if h1 == "" {
			t.Fatalf("step %d renders no heading", step)
		}
		if prev, dup := headings[h1]; dup {
			t.Errorf("steps %d and %d share the heading %q: a slide is a copy", prev, step, h1)
		}
		headings[h1] = step
	}

	beyond := get(t, h, "/activate/tour?step="+strconv.Itoa(pages.TourSteps+1), tourCookie())
	if beyond.Body.String() != bodies[1] {
		t.Errorf("step %d renders something of its own; the tour must have exactly %d slides",
			pages.TourSteps+1, pages.TourSteps)
	}
}

// TestTour_ClampsEveryUnusableStep: a missing, negative, zero, huge or
// non-numeric step is step one, never an error screen. Nothing is at stake on
// this page — no record, no credential — so a mistyped number does not earn a
// failure page, and the template's own "unknown behaves as one" fallback stays
// unreachable in production.
func TestTour_ClampsEveryUnusableStep(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	first := get(t, h, "/activate/tour?step=1", tourCookie()).Body.String()

	for _, target := range []string{
		"/activate/tour",
		"/activate/tour?step=",
		"/activate/tour?step=0",
		"/activate/tour?step=-3",
		"/activate/tour?step=99",
		"/activate/tour?step=two",
		"/activate/tour?step=1.5",
		"/activate/tour?step=%201",
	} {
		w := get(t, h, target, tourCookie())
		if w.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", target, w.Code)
			continue
		}
		if w.Body.String() != first {
			t.Errorf("%s: did not clamp to the first slide", target)
		}
	}
}

// TestTour_NeedsASession is the same gate the confirmation keeps, for the same
// reason: this page is personal-flow furniture reached from a credential-bearing
// redirect, and a visitor with no session has nothing to be shown. A REVOKED
// session lands on the same screen as a missing one — the ErrRevoked/ErrNoSession
// distinction that matters enormously on the tap path (§5 rows 3 vs 4) buys
// nothing where no record is at stake.
func TestTour_NeedsASession(t *testing.T) {
	cases := []struct {
		name string
		sess *fakeSessions
		with []*http.Cookie
	}{
		{"no cookie at all", &fakeSessions{}, nil},
		{"unknown session", &fakeSessions{
			verify: func() (session.Resolved, error) { return session.Resolved{}, session.ErrNoSession },
		}, []*http.Cookie{tourCookie()}},
		{"revoked session", &fakeSessions{
			verify: func() (session.Resolved, error) { return session.Resolved{}, session.ErrRevoked },
		}, []*http.Cookie{tourCookie()}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHandler(t, &fakeInvites{}, tc.sess, &fakeAudit{})
			w := get(t, h, "/activate/tour", tc.with...)
			// screenText, not the raw body: the title carries an apostrophe and templ
			// escapes it, so a raw Contains would be testing the escaping.
			text := screenText(t, w.Body.String())
			if !strings.Contains(text, problemNoSession.Title) {
				t.Errorf("want the %q screen, got:\n%s", problemNoSession.Title, text)
			}
			for _, leak := range []string{"Tap the plaque", "One button", "First tap is practice"} {
				if strings.Contains(text, leak) {
					t.Errorf("a session-less visitor was shown the slide copy %q", leak)
				}
			}
		})
	}
}

// TestTour_EveryStepOffersAWayOut is the card's "the tour can be skipped",
// asserted as a property of every slide rather than of one: from any step there
// is a link whose target is the confirmation, so nobody is ever trapped between
// activation and the end of the flow.
//
// The LAST slide's way out is its primary link, not a separate "skip" — the two
// would go to the same address, and offering the same destination twice on one
// screen is the kind of second action §9 keeps off these pages.
func TestTour_EveryStepOffersAWayOut(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	for step := 1; step <= pages.TourSteps; step++ {
		body := get(t, h, "/activate/tour?step="+strconv.Itoa(step), tourCookie()).Body.String()
		if !strings.Contains(body, `href="/activate/done"`) {
			t.Errorf("step %d has no way out of the tour", step)
		}
	}
}

// TestTour_PointsOnlyAtItsOwnFlow pins the set of addresses each slide emits IN
// THE ATTRIBUTES assertRefs READS — today href, src and ping, plus an outright
// refusal of any <meta http-equiv>. The shape and the reasoning are
// TestScreens_ReferenceOnlyOurOwnAssets's: such an attribute is one no whole-text
// check looks at, and a data: or third-party URL in one is how a screen reaches
// somewhere nothing else can see.
//
// ⚠️ "THE SET OF ADDRESSES EACH SLIDE EMITS" IS WHAT THIS USED TO SAY, FULL STOP,
// and it was wider than the code. assertRefs reads a LIST of attribute names, and
// `ping` was not on it: an audit added
// <a href="/activate/done" ping="https://evil.example/beacon"> to a slide — a POST
// to a third party on click — and the whole package stayed green. `ping` is on the
// list now and that mutation is red, but the honest form of the claim names the
// attributes rather than implying every possible one, because the list is a list.
//
// 🔴 RETRACTED CLAIM. An earlier version of this comment said "it is written as
// an exact set rather than a 'contains', so a link ADDED to a slide fails too —
// the tour is three slides with two links". BOTH HALVES WERE WRONG and an audit
// measured it. assertRefs compares the set of href/src VALUES, never how many
// there are, so an extra anchor pointing at an address already on the list is
// invisible to it — measured with an EMPTY one on slide two:
//
//	<a href="/activate/done" class="tap-button flex items-center justify-center"></a>
//
// The whole suite stayed green: the text pin sees no text node, the closed
// element set already allows `a`, and the value is already allowed here. That is
// an invisible second touch target on a screen §9 keeps second actions off. And
// the count itself was wrong too — slide three has ONE link ("Got it"), so the
// tour has five anchors, not two.
//
// What this test does is narrower and worth stating exactly: it refuses a
// destination nobody listed, IN THE ATTRIBUTES NAMED ABOVE. The COUNT and SHAPE of
// the touch targets are pinned by TestTour_HasExactlyTheseTouchTargets, which is
// what the mutation above now fails.
func TestTour_PointsOnlyAtItsOwnFlow(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	const stylesheet = "/static/css/app.css"

	want := map[int][]string{
		1: {stylesheet, "/activate/tour?step=2", "/activate/done"},
		2: {stylesheet, "/activate/tour?step=3", "/activate/done"},
		3: {stylesheet, "/activate/done"},
	}
	if len(want) != pages.TourSteps {
		t.Fatalf("this table covers %d slides but the tour has %d", len(want), pages.TourSteps)
	}
	for step, allowed := range want {
		body := get(t, h, "/activate/tour?step="+strconv.Itoa(step), tourCookie()).Body.String()
		assertRefs(t, "tour step "+strconv.Itoa(step), body, allowed...)
	}
}

// TestTour_HasExactlyTheseTouchTargets is the §9 half of the tour's defence, and
// it exists because an audit walked an invisible one past everything else.
//
// WHAT §9 ACTUALLY PROTECTS on these screens is not "no unknown URL" — it is that
// a person is offered a KNOWN, SMALL set of things to press. The three narrow
// whitelists each miss that from their own angle: the text pin reads text NODES,
// so an anchor with no text is silent to it; the closed element set allows `a`
// because the tour legitimately has anchors; and assertRefs compares href VALUES,
// so a duplicate of an allowed address is free. An empty
// `<a href="/activate/done" class="tap-button …"></a>` added to slide two
// therefore left the entire package green — measured before this test existed.
//
// So the assertion is the ordered list of (destination, label) pairs per slide.
// Adding an anchor fails, removing one fails, relabelling one fails, re-pointing
// one fails, and an EMPTY label fails because "" is not the label on the list.
//
// ANCHORS ARE THE WHOLE VOCABULARY HERE, and that is not an assumption:
// TestTour_RendersOnlyTheseElements is a closed set that contains no button, no
// form and no input, so there is no other element on these slides that could be
// pressed. The one channel left is an inline event handler on an allowed element,
// which is why this test also refuses `on…=` outright — cheap, and the tour has
// no script of any kind to need one.
//
// LIMIT, stated rather than implied: this reads MARKUP. A target made pressable
// purely by stylesheet — a huge ::after over the card — is not something HTML
// reading can see, and nothing here claims otherwise.
func TestTour_HasExactlyTheseTouchTargets(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})

	want := map[int][]string{
		1: {"/activate/tour?step=2 -> Next", "/activate/done -> Skip the tour"},
		2: {"/activate/tour?step=3 -> Next", "/activate/done -> Skip the tour"},
		3: {"/activate/done -> Got it"},
	}
	if len(want) != pages.TourSteps {
		t.Fatalf("this table covers %d slides but the tour has %d", len(want), pages.TourSteps)
	}
	for step, exact := range want {
		body := get(t, h, "/activate/tour?step="+strconv.Itoa(step), tourCookie()).Body.String()

		got := anchorsOf(body)
		if len(got) != len(exact) {
			t.Errorf("step %d offers %d anchor(s), want %d\n got: %v\nwant: %v\n"+
				"An anchor added here is a second thing to press on a screen CLAUDE.md §9 keeps "+
				"to one action. If it belongs, put it on this list.", step, len(got), len(exact), got, exact)
			continue
		}
		for i := range exact {
			if got[i] != exact[i] {
				t.Errorf("step %d anchor %d = %q, want %q", step, i, got[i], exact[i])
			}
		}
		if m := inlineHandlerRE.FindString(body); m != "" {
			t.Errorf("step %d carries an inline event handler (%q): the tour has no script and "+
				"needs none, and a handler is a press target no anchor list can see", step, m)
		}
	}
}

// anchorsOf returns every anchor of a rendered slide as "href -> label", in
// document order. The label is the anchor's text with runs of whitespace
// collapsed, which is what a person reads; an anchor with no text yields
// "href -> " and therefore matches nothing on the expected list.
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

// TestTour_SaysExactlyThisAndNothingElse extends M5-06's whole-text pin to the
// tour, and the extension is deliberate rather than assumed: that mechanism's
// stated limit is that coverage is PER-SCREEN AND MANUAL (see screenText), so a
// template added to this package is unprotected until somebody writes it down.
// The tour is the first screen added since, and this is it being written down.
//
// 🔴 WHAT IS BEING PROTECTED HERE IS A PROMISE, WHICH IS WHY IT IS THE WHOLE TEXT
// AND NOT A KEYWORD. Slide three tells an employee that their first tap will not
// count toward their hours. That is a claim about the decision engine made to
// somebody who cannot check it, and the measurements behind the exact wording are
// in internal/domain/tap (TestDecide_ThePracticeRunIsSpentByANYPriorRecord and
// its neighbours). A sentence added here — "your manager will see this", "nothing
// is recorded until you finish the tour" — would be a new claim nobody measured,
// on a screen shown to every new employee.
func TestTour_SaysExactlyThisAndNothingElse(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})

	// screenText collapses every run of whitespace to ONE SPACE, so the words below
	// run together with single spaces across tag boundaries — the same shape
	// shellText and the result-screen table use.
	want := map[int]string{
		1: "How Tappa works — Tappa tappa punchless " +
			"Step 1 of 3 Tap the plaque " +
			"Hold the top of your phone against the Tappa plaque by the door. Your phone " +
			"opens this page for you — there is nothing to install and nothing to remember. " +
			"If your phone does not react, ask your manager. " +
			"Next Skip the tour",
		2: "One button — Tappa tappa punchless " +
			"Step 2 of 3 One button " +
			"The page opens with your name on it and a single button. Press it — that is " +
			"the whole thing. " +
			"Tappa works out whether you are arriving or leaving, so you never have to choose. " +
			"Next Skip the tour",
		3: "Your first tap — Tappa tappa punchless " +
			"Step 3 of 3 First tap is practice " +
			"Go ahead and try it: your first tap is a practice run. " +
			"TRAINING " +
			"The screen marks a practice tap like that, and a TRAINING tap never counts " +
			"toward your hours. " +
			"Whatever a tap turns out to be, the screen right after it says so. " +
			"Got it",
	}
	if len(want) != pages.TourSteps {
		t.Fatalf("this table covers %d slides but the tour has %d", len(want), pages.TourSteps)
	}
	for step, exact := range want {
		got := screenText(t, get(t, h, "/activate/tour?step="+strconv.Itoa(step), tourCookie()).Body.String())
		if got != exact {
			t.Errorf("step %d text\n got: %q\nwant: %q", step, got, exact)
		}
	}
}

// TestTour_RendersOnlyTheseElements is the tour's half of the closed element set.
// The reasoning is TestScreens_RenderOnlyTheseElements's in full: a blacklist can
// always be beaten by one more element, an <iframe srcdoc> or an <img
// src="data:image/svg+xml,…<text>…"> puts words on a screen that no text reader
// finds, and the only shape that does not lose that race is an exact set.
//
// The tour's set is the shell plus the docket, the card, the stamp and the two
// links. It is MEASURED from the rendered slides, not chosen.
func TestTour_RendersOnlyTheseElements(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	got := map[string]bool{}
	for step := 1; step <= pages.TourSteps; step++ {
		collectTags(get(t, h, "/activate/tour?step="+strconv.Itoa(step), tourCookie()).Body.String(), got)
	}
	assertTagSet(t, "tour", got, []string{
		"html", "head", "meta", "title", "link", "body", "main", "header",
		"section", "p", "h1", "span", "a",
	})
}

// TestTour_ShowsTheTrainingStampItTalksAbout: slide three names a mark the
// employee will meet on the confirmation screen, so it renders the REAL one —
// the same `stamp stamp--training` component result.templ uses. A picture of the
// thing beats a description of it, and pinning the exact class string is what
// keeps a stray colour utility from being added beside it (the escape measured on
// the result screen, where `class="stamp stamp--training text-saffron"` rendered
// the word at 2.15:1 with the whole suite green).
func TestTour_ShowsTheTrainingStampItTalksAbout(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	body := get(t, h, "/activate/tour?step=3", tourCookie()).Body.String()
	const want = `<span class="stamp stamp--training">TRAINING</span>`
	if !strings.Contains(body, want) {
		t.Errorf("slide three does not render the training stamp exactly as %s", want)
	}
	for step := 1; step < pages.TourSteps; step++ {
		if strings.Contains(get(t, h, "/activate/tour?step="+strconv.Itoa(step), tourCookie()).Body.String(), "stamp--") {
			t.Errorf("step %d renders a stamp; only the slide that explains one should", step)
		}
	}
}

// TestTour_CarriesNoPersonalDataAtAll is the structural half of pages.TourView's
// "one field, and it carries nothing about a person": the slides are literals, so
// the name, employer and venue that the activation form and the confirmation
// legitimately render must be absent here. Asserted against the same fixture
// those screens are asserted against, so the strings really are the ones in play.
func TestTour_CarriesNoPersonalDataAtAll(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	ictx := okContext("active")
	for step := 1; step <= pages.TourSteps; step++ {
		body := get(t, h, "/activate/tour?step="+strconv.Itoa(step), tourCookie()).Body.String()
		for _, secret := range []string{
			ictx.FullName, ictx.TenantName, ictx.LocationName, ictx.WiFiSSID,
			ictx.TenantID.String(), ictx.EmployeeID.String(), ictx.InviteID.String(),
			fakeCode, fakeCSRF,
		} {
			if strings.Contains(body, secret) {
				t.Errorf("step %d renders %q; the tour teaches and reports nothing", step, secret)
			}
		}
	}
}

// TestTourView_FieldCountIsTheSpec mirrors TestResultView_FieldCountIsTheSpec:
// the guarantee "no personal data can reach the tour" is a property of the FIELD
// LIST, and a comment saying so only catches a smuggled field if somebody
// re-counts. One field, and adding a second is a conversation.
func TestTourView_FieldCountIsTheSpec(t *testing.T) {
	if n := reflect.TypeOf(pages.TourView{}).NumField(); n != 1 {
		t.Fatalf("TourView has %d fields, want 1. A field is how a name, a venue or an id "+
			"reaches a screen that is meant to be literals only — see pages.TourView.", n)
	}
}

// headingOf extracts the single <h1> of a rendered slide.
func headingOf(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`(?s)<h1[^>]*>(.*?)</h1>`).FindStringSubmatch(body)
	if len(m) != 2 {
		return ""
	}
	return strings.Join(strings.Fields(m[1]), " ")
}
