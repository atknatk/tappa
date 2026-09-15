package handler

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/atknatk/tappa/web"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The hero's "Watch demo" dialog (2026-09-14).
//
// 🔴 WHAT THESE TESTS ARE ACTUALLY FOR. The recording does not exist yet — the user
// will drop web/static/video/demo.mp4 in later — so the page has TWO shapes, and the
// one that ships today is the one nobody would notice being wrong. The classic defect
// here is a player rendered against a file that is not there: a black rectangle, a
// dead control bar and a 404 in the network panel, on the first screen a stranger
// sees. So the empty shape is measured as hard as the full one, and the assertions
// are written the negative way round — no <video>, no empty src, no poster="" —
// because those are the bytes that would actually ship.
//
// EVERY SCANNER BELOW HAS A NEGATIVE CONTROL, in this file's neighbours' style: a
// check that would pass over an empty page is a check that measures nothing.

// renderLandingForDemo renders the landing page from a view built here, so both
// branches can be driven. The router cannot drive both: web.Static() is a
// compile-time constant, so the handler's answer is whatever this binary carries.
func renderLandingForDemo(t *testing.T, v pages.LandingView) string {
	t.Helper()
	var sb strings.Builder
	if err := pages.Landing(v).Render(t.Context(), &sb); err != nil {
		t.Fatalf("rendering the landing page: %v", err)
	}
	if !strings.Contains(sb.String(), `id="demoDialog"`) {
		t.Fatal("the rendered page carries no demo dialog at all, so every assertion " +
			"below would be measuring an absence rather than a shape")
	}
	return sb.String()
}

// demoView is a complete landing view with the demo fields left to the caller.
func demoView(videoSrc, posterSrc string) pages.LandingView {
	return pages.LandingView{
		SignupHref:            signupPath,
		SignInHref:            adminLoginPath,
		PricePerEmployeeMonth: publishedPricePerEmployeeMonth,
		FreeMonths:            foundingFreeMonths,
		DemoVideoSrc:          videoSrc,
		DemoPosterSrc:         posterSrc,
	}
}

// --------------------------------------------------------------- the button ----

// openerRE finds the hero's second button by its id and captures its opening tag
// and its content, so each property is asserted on its own and the failure names
// what changed.
var openerRE = regexp.MustCompile(`(?s)<a\b([^>]*\bid="demoOpen"[^>]*)>(.*?)</a>`)

// TestLandingDemo_TheSecondHeroButtonSaysWatchDemoAndOpensTheDialog.
//
// The label is the user's instruction and the rest is what makes it not a lie: the
// element keeps the reference's classes (so nothing moved or resized), it keeps an
// href (so a browser that ran no script still goes somewhere real), it carries the
// play glyph the user asked for, and the thing it points at exists on the page.
//
// ⚠️ IT USED TO ASSERT ONE BYTE-EXACT STRING, and that string went stale the day
// the play icon arrived (the element gained lp-btn-withIcon and an inline <svg>
// before the label). The properties are now asserted one at a time, which is what
// makes a failure say WHICH one changed.
func TestLandingDemo_TheSecondHeroButtonSaysWatchDemoAndOpensTheDialog(t *testing.T) {
	t.Parallel()
	body := fetchMarketing(t, marketingRouter(t), "/", nil).Body.String()

	if strings.Contains(body, "See how it works") {
		t.Error(`the old label "See how it works" is still on the page`)
	}
	m := openerRE.FindStringSubmatch(body)
	if m == nil {
		t.Fatal(`the page carries no <a id="demoOpen">, so nothing opens the dialog`)
	}
	if n := len(openerRE.FindAllString(body, -1)); n != 1 {
		t.Errorf("the page carries %d elements with id demoOpen; ids are unique", n)
	}
	attrs, inner := m[1], m[2]
	// The reference's classes, so nothing moved or resized; the icon class, so the
	// glyph has its spacing.
	for _, class := range []string{"lp-btn", "lp-btn-ghost", "lp-btn-withIcon"} {
		if !regexp.MustCompile(`class="[^"]*\b` + class + `\b[^"]*"`).MatchString(attrs) {
			t.Errorf("the opener lost the class %q (attributes: %s)", class, strings.TrimSpace(attrs))
		}
	}
	// The fallback: a browser with no script or no <dialog> follows the href, and it
	// has to lead to the section that answers the same question in prose.
	if !strings.Contains(attrs, `href="#how"`) {
		t.Errorf("the opener no longer falls back to #how (attributes: %s); a browser that "+
			"ran no script would get a dead button", strings.TrimSpace(attrs))
	}
	if !strings.Contains(body, `id="how"`) {
		t.Error(`the opener falls back to #how and no element on the page carries that id`)
	}
	// The visible text is exactly the label, and the glyph is drawn rather than typed.
	if text := strings.Join(strings.Fields(tagRE.ReplaceAllString(inner, " ")), " "); text != "Watch demo" {
		t.Errorf(`the hero's second button reads %q, want "Watch demo"`, text)
	}
	if !strings.Contains(inner, "<svg") || !strings.Contains(inner, `aria-hidden="true"`) {
		t.Error("the opener carries no play icon, or carries one a screen reader would announce twice")
	}
	if strings.Index(inner, "<svg") > strings.Index(inner, "Watch demo") {
		t.Error("the play icon is after the label; the reference puts the glyph before the words")
	}
	if !strings.Contains(body, `<dialog id="demoDialog"`) {
		t.Error("the page renders no <dialog>, so the button opens nothing")
	}
}

// TestLandingDemo_TheDialogIsLabelledAndCanBeClosedWithoutAMouse.
//
// 🔴 THE THINGS THIS DOES NOT ASSERT ARE THE POINT. Escape, the focus trap and the
// dim behind the panel are <dialog>/showModal() behaviour, so there is nothing of
// ours to test and asserting them here would only be testing the browser. What IS
// ours is the markup the browser needs in order to do that: a labelled dialog and a
// visible, named close control of a size a finger can hit.
func TestLandingDemo_TheDialogIsLabelledAndCanBeClosedWithoutAMouse(t *testing.T) {
	t.Parallel()
	body := fetchMarketing(t, marketingRouter(t), "/", nil).Body.String()

	for _, want := range []string{
		`<dialog id="demoDialog" class="demoDialog" aria-labelledby="demoDialogTitle">`,
		`id="demoDialogTitle"`,
		`aria-label="Close the demo"`,
		`id="demoClose"`,
		// initial focus has one answer rather than a per-browser one
		`autofocus`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the dialog markup is missing %q", want)
		}
	}
	// A DIALOG THE BROWSER DOES NOT MAKE MODAL. `open` in the markup renders the
	// panel inline, in the page, with no backdrop and no focus trap -- it is the one
	// attribute that would quietly undo everything the element was chosen for.
	if strings.Contains(body, `<dialog id="demoDialog" open`) {
		t.Error("the dialog ships with the `open` attribute, which renders it inline and " +
			"non-modal; it must be opened with showModal()")
	}
	// THE 44px FLOOR IS MEASURED IN THE STYLESHEET THAT SHIPS, not in the source one:
	// a class in the markup with no rule behind it is not a touch target, and app.css
	// is the file the browser is served. It is minified, hence the tight strings.
	css := readStaticFile(t, "css/app.css")
	rule := cssRule(t, css, ".lp .demoClose{")
	for _, want := range []string{"width:44px", "height:44px"} {
		if !strings.Contains(rule, want) {
			t.Errorf(".lp .demoClose does not declare %s; the product's floor for anything "+
				"a finger has to hit is 44x44.\nrule: %s", want, rule)
		}
	}
	// AND THE OPENING MOVE IS ASKED FOR RATHER THAN ASSUMED. An animation declared
	// outside a no-preference query plays for a reader who has said they do not want
	// motion; that it is only 180ms does not make it their choice.
	if !strings.Contains(css, "@media (prefers-reduced-motion:no-preference){.lp .demoDialog[open]{animation:") {
		t.Error("the dialog's opening animation is not inside a prefers-reduced-motion: " +
			"no-preference query")
	}
}

// ------------------------------------------------- the two shapes of the panel --

// TestLandingDemo_WithNoRecordingTheDialogSaysSoAndRendersNoPlayer.
//
// 🔴 THIS IS THE SHAPE THAT SHIPS TODAY and the one worth the most assertions.
func TestLandingDemo_WithNoRecordingTheDialogSaysSoAndRendersNoPlayer(t *testing.T) {
	t.Parallel()
	body := renderLandingForDemo(t, demoView("", ""))

	if strings.Contains(body, "<video") {
		t.Error("there is no recording in this build and the page renders a <video> anyway; " +
			"a player with nothing behind it is a black box and a 404")
	}
	// The three shapes of a broken source, named one at a time so the failure says
	// which one happened.
	for _, broken := range []string{`src=""`, `poster=""`, `src="/static/video/`} {
		if strings.Contains(body, broken) {
			t.Errorf("the page carries %s with no file behind it", broken)
		}
	}
	// AND IT SAYS SO IN WORDS. An empty dialog would satisfy every assertion above.
	if !strings.Contains(body, "There is no recording here yet.") {
		t.Error("the dialog opens on nothing: it does not tell the visitor that the " +
			"recording does not exist")
	}
	// AND IT ENDS SOMEWHERE REAL. A dead end is the other way to waste the click.
	if !strings.Contains(body, `href="`+signupPath+`"`) {
		t.Errorf("the empty dialog offers no link to %s, so the click leads nowhere", signupPath)
	}
}

// TestLandingDemo_WithARecordingTheDialogRendersThePlayerAndItsSource walks the
// branch the user's own upload will take.
func TestLandingDemo_WithARecordingTheDialogRendersThePlayerAndItsSource(t *testing.T) {
	t.Parallel()

	t.Run("video and poster", func(t *testing.T) {
		t.Parallel()
		body := renderLandingForDemo(t, demoView("/static/video/demo.mp4", "/static/video/demo-poster.jpg"))
		for _, want := range []string{
			`<video`,
			`src="/static/video/demo.mp4"`,
			`poster="/static/video/demo-poster.jpg"`,
			`controls`,
			`preload="metadata"`,
			`playsinline`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("the player is missing %q", want)
			}
		}
		// NO autoplay, and it is asserted rather than assumed: a page that starts
		// talking is a page people close, every browser blocks unmuted autoplay
		// anyway, and a visitor who asked for reduced motion asked for exactly this.
		if strings.Contains(body, "autoplay") {
			t.Error("the player autoplays")
		}
		// The empty state must be GONE, not merely hidden behind it.
		if strings.Contains(body, "There is no recording here yet.") {
			t.Error("the dialog shows the player AND the sentence that says there is none")
		}
	})

	t.Run("video without a poster", func(t *testing.T) {
		t.Parallel()
		body := renderLandingForDemo(t, demoView("/static/video/demo.mp4", ""))
		if !strings.Contains(body, `src="/static/video/demo.mp4"`) {
			t.Error("the player lost its source when the poster went away")
		}
		// 🔴 poster="" IS NOT HARMLESS: an empty URL resolves to the current document,
		// which the browser fetches as an image and fails on. The attribute has to be
		// absent, not empty.
		if strings.Contains(body, "poster=") {
			t.Error(`there is no poster file and the page writes a poster attribute anyway`)
		}
	})
}

// ---------------------------------------------------- what the build actually is --

// TestLandingDemo_TheServedPageAgreesWithTheEmbeddedAssetTree is the test that keeps
// working after the user adds the file.
//
// 🔴 IT ASSERTS A CORRESPONDENCE, NOT A STATE. The two tests above drive the two
// branches from views written by hand; this one drives the REAL handler against the
// REAL embedded tree and says the served page shows a player exactly when the binary
// carries a video. Green today with no file, green tomorrow with one, red if the
// detection and the markup ever disagree -- which is the only failure the user could
// hit by simply dropping demo.mp4 into web/static/video and rebuilding.
func TestLandingDemo_TheServedPageAgreesWithTheEmbeddedAssetTree(t *testing.T) {
	t.Parallel()
	carriesVideo := staticAssetURL(web.Static(), demoVideoAsset) != ""
	body := fetchMarketing(t, marketingRouter(t), "/", nil).Body.String()
	rendersPlayer := strings.Contains(body, "<video")

	if carriesVideo != rendersPlayer {
		t.Errorf("web/static/%s is present=%v but the served page renders a player=%v.\n"+
			"Adding the recording is meant to be one file and a rebuild; these two have "+
			"come apart.", demoVideoAsset, carriesVideo, rendersPlayer)
	}
	t.Logf("this build carries %s: %v", demoVideoAsset, carriesVideo)
}

// TestStaticAssetURL_AnswersOnlyForAFileThatIsActuallyThere.
func TestStaticAssetURL_AnswersOnlyForAFileThatIsActuallyThere(t *testing.T) {
	t.Parallel()
	tree := fstest.MapFS{
		"video/demo.mp4":  {Data: []byte("not really an mp4, and it does not have to be")},
		"css/app.css":     {Data: []byte(".a{}")},
		"video/notes/x.t": {Data: []byte("x")},
	}
	tests := []struct {
		name string
		arg  string
		want string
	}{
		{"a file that is there", "video/demo.mp4", "/static/video/demo.mp4"},
		{"a file that is not", "video/demo-poster.jpg", ""},
		{"a sibling that is there", "css/app.css", "/static/css/app.css"},
		// 🔴 A DIRECTORY IS NOT AN ASSET. fs.Stat succeeds on it, and
		// <video src="/static/video"> answers 404 -- the exact broken element this
		// function exists to prevent.
		{"a directory", "video", ""},
		{"a path outside the tree", "../secrets", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := staticAssetURL(tree, tc.arg); got != tc.want {
				t.Errorf("staticAssetURL(%q) = %q, want %q", tc.arg, got, tc.want)
			}
		})
	}
}

// ----------------------------------------------------------------- the policy --

// TestLandingDemo_PolicyNamesMediaOnlyWhenTheBuildCarriesTheDemo.
//
// 🔴 THE RULE THIS ENFORCES IS marketingCSP'S OWN: "a directive named for something
// that does not load makes the next addition a silent inheritance instead of a
// visible edit". The recording is not in the repository, so a constant media-src
// would be a permission granted today for a file that arrives some other day, on the
// one page every stranger reaches.
func TestLandingDemo_PolicyNamesMediaOnlyWhenTheBuildCarriesTheDemo(t *testing.T) {
	t.Parallel()

	if got := landingCSPFor("", ""); got != landingCSP {
		t.Errorf("with no demo assets the policy is %q; it must be byte for byte the "+
			"policy this page sent before the dialog existed (%q)", got, landingCSP)
	}
	withVideo := landingCSPFor("/static/video/demo.mp4", "")
	if !strings.Contains(withVideo, "media-src 'self'") {
		t.Errorf("a build with a recording sends %q, which does not permit it to load", withVideo)
	}
	if strings.Contains(withVideo, "img-src") {
		t.Errorf("a build with no poster names img-src anyway: %q", withVideo)
	}
	withBoth := landingCSPFor("/static/video/demo.mp4", "/static/video/demo-poster.jpg")
	// A <video poster> is an IMAGE fetch and img-src has no fallback to media-src --
	// it falls back to default-src 'none'. Without this the poster is a silent
	// failure: a blank frame and a console line nobody reads.
	if !strings.Contains(withBoth, "img-src 'self'") {
		t.Errorf("a build with a poster sends %q, so the poster would be blocked", withBoth)
	}
	// NOTHING WIDER, IN ANY BUILD.
	for _, forbidden := range []string{"'unsafe-inline'", "'unsafe-eval'", "http:", "https:", "*"} {
		if strings.Contains(withBoth, forbidden) {
			t.Errorf("the widest policy this page can send contains %q: %q", forbidden, withBoth)
		}
	}

	// AND THE HEADER ON THE WIRE IS THAT SAME STRING, computed from this build.
	want := landingCSPFor(
		staticAssetURL(web.Static(), demoVideoAsset),
		staticAssetURL(web.Static(), demoPosterAsset),
	)
	got := fetchMarketing(t, marketingRouter(t), "/", nil).Header().Get("Content-Security-Policy")
	if got != want {
		t.Errorf("GET / sends %q, want %q", got, want)
	}
}

// ------------------------------------------------------- nowhere but the root --

// TestLandingDemo_NoOtherScreenOnThePublicSurfaceCarriesTheDialog.
//
// The dialog is a piece of the landing page, not of the marketing shell. The four
// legal documents and the four wizard screens render layout.Marketing, which has no
// script slot at all -- a dialog appearing there would be inert markup and, on the
// legal pages, markup the unchanged policy forbids the script for.
func TestLandingDemo_NoOtherScreenOnThePublicSurfaceCarriesTheDialog(t *testing.T) {
	t.Parallel()

	screens := map[string]string{}
	r := marketingRouter(t)
	for _, url := range marketingURLs() {
		if url == "/" {
			continue
		}
		screens[url] = fetchMarketing(t, r, url, nil).Body.String()
	}

	// The wizard's four screens are collected AS THEY ARE SERVED, in order, because a
	// cold GET of step two is the "start again" page rather than the screen.
	d, _ := newSignupDriver(t, &fakeProvisioner{}, nil)
	start := d.get(signupPath)
	screens[signupPath] = start.Body.String()
	d.post(signupPath, businessForm(d.token(start)))
	places := d.get(signupPlacesPath)
	screens[signupPlacesPath] = places.Body.String()
	d.post(signupPlacesPath, placesForm(d.token(places), "Front door"))
	account := d.get(signupAccountPath)
	screens[signupAccountPath] = account.Body.String()
	d.advance(time.Minute)
	d.post(signupAccountPath, accountForm(d.token(account)))
	screens[signupDonePath] = d.get(signupDonePath).Body.String()

	if len(screens) != 8 {
		t.Fatalf("collected %d screens, want 8 (four legal documents and four wizard "+
			"steps); this test is looking at the wrong set", len(screens))
	}
	for path, body := range screens {
		// ANTI-VACUITY: an empty body or a redirect would pass every check below.
		if !strings.Contains(body, "</html>") {
			t.Errorf("%s was collected as something that is not a rendered page, so its "+
				"silence below means nothing", path)
			continue
		}
		for _, forbidden := range []string{"<dialog", "demoDialog", "Watch demo", "landing.js"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("%s carries %q, which belongs to the landing page alone", path, forbidden)
			}
		}
	}
}

// ------------------------------------------------------------------ the script --

// TestLandingDemo_TheScriptDegradesInsteadOfBreaking.
//
// 🔴 IT READS THE SOURCE, and that is a weaker instrument than driving a browser --
// which is why it asserts only the two properties a reader cannot check by looking
// at the rendered page, and why the keyboard behaviour is measured against real
// Chrome rather than here.
func TestLandingDemo_TheScriptDegradesInsteadOfBreaking(t *testing.T) {
	t.Parallel()
	js := readStaticFile(t, "js/landing.js")

	// CONTROL: the file is the one this test thinks it is.
	if !strings.Contains(js, "demoDialog") {
		t.Fatal("web/static/js/landing.js does not mention the dialog at all")
	}
	// The guard that makes the fallback real. Without it, a browser with no <dialog>
	// would have its hero link cancelled by a handler that then cannot open anything.
	if !strings.Contains(js, "typeof dialog.showModal !== 'function'") {
		t.Error("the script does not check for showModal before taking over the hero link; " +
			"a browser without <dialog> would get a dead button instead of the #how fallback")
	}
	// The video stops on the way out, from the ONE handler all three exits reach.
	for _, want := range []string{"video.pause()", "video.currentTime = 0", "opener.focus()"} {
		if !strings.Contains(js, want) {
			t.Errorf("the script never calls %s, so closing the dialog leaves something "+
				"running or leaves focus nowhere", want)
		}
	}
	// NO NEW CAPABILITY. This file rotates text and now opens a panel; anything below
	// would be a request, a cookie or a coordinate on the public surface.
	for _, forbidden := range []string{"fetch(", "XMLHttpRequest", "document.cookie", "localStorage", "geolocation"} {
		if strings.Contains(js, forbidden) {
			t.Errorf("landing.js uses %s", forbidden)
		}
	}
}

// readStaticFile reads one file out of the EMBEDDED asset tree, which is what the
// browser is served -- not out of the working directory, which may hold a newer one.
func readStaticFile(t *testing.T, name string) string {
	t.Helper()
	raw, err := fs.ReadFile(web.Static(), name)
	if err != nil {
		t.Fatalf("reading web/static/%s: %v", name, err)
	}
	return string(raw)
}

// cssRule returns the declaration block that opens with head, so an assertion about
// one rule cannot be satisfied by the same words appearing in another.
func cssRule(t *testing.T, css, head string) string {
	t.Helper()
	i := strings.Index(css, head)
	if i < 0 {
		t.Fatalf("the stylesheet carries no rule beginning %q", head)
	}
	rest := css[i:]
	j := strings.Index(rest, "}")
	if j < 0 {
		t.Fatalf("the rule beginning %q is never closed", head)
	}
	return rest[:j+1]
}
