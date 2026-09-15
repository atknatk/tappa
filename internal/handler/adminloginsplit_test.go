package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The SPLIT SIGN-IN SCREEN (2026-09-14, the user's drawing).
//
// WHAT THIS FILE IS FOR, AND IT IS NOT "the page still renders". The redesign
// touched a screen that is already the most heavily netted one in the product —
// adminlogin_test.go holds the byte-identical failures, the audit budgets and the
// cause-free sentence — so the tests below deliberately do NOT re-assert any of
// that. They hold the four things a LAYOUT change is able to break and the older
// tests cannot see:
//
//  1. the three fields the handler parses are still in the form it posts;
//  2. the drawing's magic-link button is still absent, because the capability is;
//  3. the 401 render still shows the failure notice somewhere a person can read it;
//  4. the two links the new chrome adds reach routes that are actually mounted.

// pageLink is one anchor as a person meets it: where it goes and what it says.
//
// IT IS A SECOND READER BESIDE tour_test.go's anchorsOf RATHER THAN A REPLACEMENT
// FOR IT, and the difference is the shape of the answer. That one flattens each
// anchor to the string "href -> label" because the tour pins a whole ordered list
// in one comparison; this one needs the two halves apart, because it follows the
// href through a router and reports the label when the follow fails. Both read the
// same anchorRE, so there is one regexp for "what is an anchor" and no second
// definition to drift.
type pageLink struct {
	href string
	text string
}

// innerTagRE strips the markup inside an anchor — the wordmark's two nested spans,
// an icon — so what is compared is what a person reads.
var innerTagRE = regexp.MustCompile(`(?s)<[^>]*>`)

func linksOf(body string) []pageLink {
	var out []pageLink
	for _, m := range anchorRE.FindAllStringSubmatch(body, -1) {
		href := ""
		if h := attrValueRE("href").FindStringSubmatch(m[1]); len(h) == 2 {
			href = h[1]
		}
		text := innerTagRE.ReplaceAllString(m[2], "")
		out = append(out, pageLink{href: href, text: strings.Join(strings.Fields(text), " ")})
	}
	return out
}

// loginPage fetches the sign-in screen through the real router and returns it.
func loginPage(t *testing.T) (*browser, string) {
	t.Helper()
	b := newBrowser(t, newAdminRouter(t, &fakeAdmins{}, &fakeTrail{}))
	rec := b.do(http.MethodGet, adminLoginPath, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", adminLoginPath, rec.Code)
	}
	return b, htmlOf(t, rec)
}

// TestAdminLogin_SplitLayoutKeepsTheFormItPosts is the regression the redesign was
// most able to cause: a rewritten template that LOOKS right and posts a form the
// handler cannot parse.
//
// IT ASSERTS THE NAMES THE SERVER READS, not the markup around them. adminlogin.go
// reads "csrf", "email" and "password" off the request and nothing else; a field
// renamed in the template is a sign-in that fails with the fixed, cause-free
// sentence — which is to say, indistinguishable from a wrong password.
func TestAdminLogin_SplitLayoutKeepsTheFormItPosts(t *testing.T) {
	_, body := loginPage(t)

	for _, want := range []string{
		`method="post"`,
		`action="/admin/login"`,
		`name="csrf"`,
		`name="email"`,
		`name="password"`,
		`type="password"`,
		`autocomplete="username"`,
		`autocomplete="current-password"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the sign-in page no longer carries %s; the split layout has changed "+
				"what the handler is able to parse, not just how it looks", want)
		}
	}

	// AND IT STILL WORKS END TO END. The assertions above are string matching, which
	// would survive two forms, a stray </form> or a field outside the one that posts.
	// Driving the real POST is what makes them mean something.
	admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
		return adminauth.Authentication{}, adminauth.ErrBadCredentials
	}}
	b := newBrowser(t, newAdminRouter(t, admins, &fakeTrail{}))
	csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, adminLoginPath, nil)))
	if csrf == "" {
		t.Fatal("no synchronizer token could be read out of the redesigned form")
	}
	rec := b.do(http.MethodPost, adminLoginPath, url.Values{
		"csrf": {csrf}, "email": {"someone@example.test"}, "password": {"whatever"},
	})
	// 401 is the RIGHT answer here — the credentials are refused. A 403 would mean
	// the token round-trip broke, which is the failure a layout change can cause and
	// a human would read as "wrong password".
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("posting the redesigned form answered %d, want 401. A 403 means the "+
			"synchronizer token no longer survives the round trip.", rec.Code)
	}
}

// TestAdminLogin_OffersNoSignInLinkItCannotSend is the mounted-capability tripwire,
// and it is cited from web/templates/pages/admin.templ.
//
// 🔴 THE DRAWING THIS SCREEN WAS BUILT FROM HAS AN "Email me a sign-in link" BUTTON
// AND AN "OR" RULE ABOVE IT. There is no passwordless sign-in in this product:
// adminlogin.go mounts GET/POST /admin/login and the picker, internal/adminauth
// compares a stored digest, and nothing in the repository mints a link that
// authenticates. Copying the button across would have shipped a control that does
// nothing — the defect class this repository catches most often — so it was left
// out, and this is what keeps it out.
//
// IT SCANS BOTH RENDERS. The refused sign-in re-renders the same template with 401,
// and a future edit that adds the button to only one of them would otherwise be
// half-caught.
func TestAdminLogin_OffersNoSignInLinkItCannotSend(t *testing.T) {
	admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
		return adminauth.Authentication{}, adminauth.ErrBadCredentials
	}}
	b := newBrowser(t, newAdminRouter(t, admins, &fakeTrail{}))
	ok := htmlOf(t, b.do(http.MethodGet, adminLoginPath, nil))
	csrf := csrfFrom(t, ok)
	refused := htmlOf(t, b.do(http.MethodPost, adminLoginPath, url.Values{
		"csrf": {csrf}, "email": {"someone@example.test"}, "password": {"whatever"},
	}))

	forbidden := []string{
		"magic link", "magic-link", "sign-in link", "sign in link", "signin link",
		"login link", "passwordless", "email me a link", "one-time link",
	}
	for where, body := range map[string]string{"GET /admin/login": ok, "the 401 render": refused} {
		lower := strings.ToLower(body)
		// ANTI-VACUITY: a page this scan could not read would report a clean result.
		if !strings.Contains(lower, "welcome back") || !strings.Contains(lower, `name="password"`) {
			t.Fatalf("%s does not look like the sign-in screen at all (%d bytes); this scan "+
				"would pass over anything", where, len(body))
		}
		for _, phrase := range forbidden {
			if strings.Contains(lower, phrase) {
				t.Errorf("%s offers %q. This product has no passwordless sign-in — "+
					"internal/adminauth compares a stored digest and nothing mints an "+
					"authenticating link — so that control would do nothing at all.",
					where, phrase)
			}
		}
	}

	// NEGATIVE CONTROL: the scan must be able to fail. Without this, a typo in the
	// list above would leave the tripwire green over a page that grew the button.
	planted := strings.ToLower(`<button>Email me a sign-in link</button>`)
	hit := false
	for _, phrase := range forbidden {
		if strings.Contains(planted, phrase) {
			hit = true
		}
	}
	if !hit {
		t.Error("the forbidden-phrase list does not match the very button it exists to " +
			"refuse; it is decoration")
	}
}

// TestAdminLogin_RefusedSignInStillRendersTheFailureNotice.
//
// 🔴 THE 401 RENDERS THE SAME TEMPLATE, AND THE REDESIGN MOVED THE FORM INSIDE A
// CARD INSIDE A COLUMN INSIDE A SPLIT. That is exactly the shape in which a status
// message ends up above the fold on a designer's screen and below a full-height
// brand panel on everybody else's. The notice is therefore asserted to be INSIDE
// the card, between the heading and the form, rather than merely present in the
// bytes.
func TestAdminLogin_RefusedSignInStillRendersTheFailureNotice(t *testing.T) {
	admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
		return adminauth.Authentication{}, adminauth.ErrBadCredentials
	}}
	b := newBrowser(t, newAdminRouter(t, admins, &fakeTrail{}))
	csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, adminLoginPath, nil)))
	rec := b.do(http.MethodPost, adminLoginPath, url.Values{
		"csrf": {csrf}, "email": {"someone@example.test"}, "password": {"whatever"},
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("a refused sign-in answered %d, want 401", rec.Code)
	}
	body := htmlOf(t, rec)

	const notice = "We could not sign you in"
	at := strings.Index(body, notice)
	if at < 0 {
		t.Fatalf("the 401 body does not carry %q at all", notice)
	}
	heading := strings.Index(body, "Welcome back")
	form := strings.Index(body, `<form method="post" action="/admin/login"`)
	if heading < 0 || form < 0 {
		t.Fatalf("could not locate the card heading (%d) or the form (%d) to place the "+
			"notice between", heading, form)
	}
	if !(heading < at && at < form) {
		t.Errorf("the failure notice is at byte %d, outside the card's heading (%d) and "+
			"form (%d). A message rendered somewhere else on a full-height split screen "+
			"is a message somebody has to scroll for.", at, heading, form)
	}
	// The brand's rule for an error is that it says what to do next, and the cause-free
	// sentence is held by TestAdminLogin_FailureBodyNamesNoCause.
	if !strings.Contains(body, "Check the email address and the password, then try again.") {
		t.Error("the failure notice lost the sentence that says what to do next")
	}
}

// TestAdminLogin_ChromeLinksReachRoutesThatAreMounted follows the two links the
// redesign added or renamed against the REAL handlers that own them.
//
// 🔴 IT MOUNTS THREE HANDLERS ON ONE ROUTER RATHER THAN COMPARING STRINGS. A test
// that asserted href == adminResetPath would pass on a deployment where nothing
// serves that path — which is the whole failure mode a link on a sign-in page has.
// The recovery form and the sign-up wizard are separate types with separate Mount
// methods, so the only way to ask "does this link go anywhere" is to put them all
// on a router and ask it.
func TestAdminLogin_ChromeLinksReachRoutesThatAreMounted(t *testing.T) {
	auth, err := NewAdminAuth(&fakeAdmins{}, &fakeTrail{}, newFakeLedger(), newFakeLedger(),
		&fakeReviewer{}, &fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{},
		&fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(), newFakeTexts(),
		newFakeAccount(), nil, adminTestConfig(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	reset, err := NewAdminReset(&fakeResets{}, nil, &fakeTrail{}, adminTestConfig(),
		slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewAdminReset: %v", err)
	}
	wizard, err := NewSignup(&fakeProvisioner{}, nil, signupTestConfig(),
		slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewSignup: %v", err)
	}
	r := chi.NewRouter()
	auth.Mount(r)
	reset.Mount(r)
	wizard.Mount(r)

	b := newBrowser(t, r)
	body := htmlOf(t, b.do(http.MethodGet, adminLoginPath, nil))

	want := map[string]string{
		"Forgot password?": adminResetPath,
		// The label carries the offer, so it is matched by prefix rather than in full.
		"Start now": signupPath,
	}
	found := map[string]bool{}
	for _, a := range linksOf(body) {
		for label, href := range want {
			if !strings.HasPrefix(a.text, label) {
				continue
			}
			found[label] = true
			if a.href != href {
				t.Errorf("%q points at %q, want %q", a.text, a.href, href)
				continue
			}
			rec := b.do(http.MethodGet, a.href, nil)
			if rec.Code == http.StatusNotFound {
				t.Errorf("%q points at %s, which answers 404 — the link is dead",
					a.text, a.href)
			}
		}
	}
	for label := range want {
		if !found[label] {
			t.Errorf("no anchor whose text starts with %q on the sign-in page; either the "+
				"link is gone or it was relabelled without this test following it", label)
		}
	}
}

// TestAdminLogin_BrandPanelClaimsOnlyWhatTheProductDoes.
//
// 🔴 THE BRAND HALF IS MADE OF CLAIMS, which is landingview.go's rule arriving on a
// second surface. The four lines live in pages.AdminLoginProof with the code that
// provides each one named beside it; this asserts they are all RENDERED (a list
// nobody prints proves nothing) and that the three shapes the drawing offered and
// this screen refused have not crept back in.
func TestAdminLogin_BrandPanelClaimsOnlyWhatTheProductDoes(t *testing.T) {
	_, body := loginPage(t)

	if len(pages.AdminLoginProof) < 3 {
		t.Fatalf("pages.AdminLoginProof has %d line(s); this test would prove almost "+
			"nothing", len(pages.AdminLoginProof))
	}
	for _, line := range pages.AdminLoginProof {
		if !strings.Contains(body, htmlText(line)) {
			t.Errorf("the brand panel does not render %q, so the fact recorded beside it "+
				"in pages.AdminLoginProof is guarding nothing", line)
		}
	}

	// THE OFFER COMES FROM THE CONSTANT THE DATABASE ENFORCES, not from a "3" typed
	// into the markup. migration 00016's tappa_first_chargeable_month is what actually
	// grants the months; TestLanding_FreeMonthsMatchTheFunctionThatGrantsThem ties
	// that function to foundingFreeMonths, and this ties foundingFreeMonths to what
	// this screen advertises.
	//
	// ⚠️ THE SAFFRON BADGE THAT ALSO CARRIED THIS NUMBER WAS REMOVED ON THE USER'S
	// INSTRUCTION (2026-09-14), so the sign-up link at the top is now the screen's
	// ONLY mention of the offer and the only thing holding it to the constant. That
	// is why the assertion below is a count as well as a match: if the last mention
	// goes too, this test must fail rather than pass over an empty page.
	offer := fmt.Sprintf("first %d months free", foundingFreeMonths)
	if n := strings.Count(strings.ToLower(body), offer); n == 0 {
		t.Errorf("the sign-in screen no longer says %q anywhere; a free period advertised "+
			"here that disagrees with the one the schema grants is the worst kind of wrong "+
			"page, because both halves look right on their own", offer)
	}

	// And no OTHER number may claim to be the offer. A "3" typed into the markup
	// beside a constant that has moved is exactly the drift the paragraph above
	// exists to catch, and it would otherwise sit here looking correct.
	for n := 1; n <= 12; n++ {
		if n == foundingFreeMonths {
			continue
		}
		if stray := fmt.Sprintf("first %d months free", n); strings.Contains(strings.ToLower(body), stray) {
			t.Errorf("the sign-in screen says %q while foundingFreeMonths is %d", stray, foundingFreeMonths)
		}
	}

	// The three shapes that were deliberately not carried across from the drawing.
	// "Minutes away" is the unmeasured duration TestLanding_MakesNoUnmeasuredNumericClaim
	// refuses on the public surface; "plaques are on us" is a delivery promise for
	// hardware nobody has encoded yet (M8-05 phase B3); "while you compare" is a
	// statement about a rollout rather than about anything this repository builds.
	lower := strings.ToLower(body)
	for _, phrase := range []string{"minutes away", "plaques are on us", "while you compare"} {
		if strings.Contains(lower, phrase) {
			t.Errorf("the sign-in screen says %q. It is not a product fact: see "+
				"pages.AdminLoginProof for which shapes were refused and why.", phrase)
		}
	}
}
