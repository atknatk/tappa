package handler

// dashboard_test.go -- the nets for M6-02, the panel shell.
//
// WHAT THIS FILE IS TRYING NOT TO BE. M6-01 phase B shipped five protections and
// an audit measured that DELETING ANY OF THE FIVE left the suite green: every
// test that mentioned a constant was written in terms of that constant, so the
// expectation moved with the value. Two shapes cause that and both are avoided
// here on purpose:
//
//	A TAUTOLOGY  compares an expectation against the very thing it is checking
//	             (`if got != TheConstant`). It accepts every value.
//	A FIXED LIST is a change detector, not a net. It goes red when something is
//	             ADDED, and the natural repair -- write the newcomer into the list
//	             -- is exactly the wrong move. internal/policy's
//	             unnamedAlertPreemptions is the working alternative and the model
//	             followed below: check the PROPERTY over the shipped data
//	             structure, and make the escape hatch a list that has to SHRINK.
//
// So every test below ranges over pages.PanelSections -- the table the product
// itself renders and routes from -- and asserts a property of each row. Adding a
// sixth section does not turn any of them red; shipping a sixth section that is
// not routed, not in the navigation, does not say which task fills it, or shows
// another section's text, turns four of them red.
//
// Each one carries an ANTI-VACUITY check first: a net that would pass over an
// empty input proves nothing, and three of these read a slice that could be
// empty and two read files that could be missing.

import (
	"fmt"
	"io/fs"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/web"
	"github.com/atknatk/tappa/web/templates/pages"
)

// panelBrowser returns a browser holding a session that resolves to a live admin,
// which is what every section test needs before it can see a section at all.
func panelBrowser(t *testing.T) *browser {
	t.Helper()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: "owner", FullName: "KF Owner",
		}, nil
	}}
	b := newBrowser(t, newAdminRouter(t, admins, &fakeTrail{}))
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

// sectionBodies renders every panel section once, keyed by href. Several tests
// need all five and none of them should pay for a second render.
func sectionBodies(t *testing.T) map[string]string {
	t.Helper()
	assertSectionTableIsUsable(t)
	b := panelBrowser(t)
	out := make(map[string]string, len(pages.PanelSections))
	for _, s := range pages.PanelSections {
		rec := b.do(http.MethodGet, s.Href, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d, want 200", s.Href, rec.Code)
		}
		out[s.Href] = htmlOf(t, rec)
	}
	return out
}

// assertSectionTableIsUsable is the anti-vacuity gate every test below opens
// with. An empty or degenerate PanelSections would make each of them pass while
// proving nothing at all -- and it is not a hypothetical shape, it is the shape
// the table has for the ten minutes before somebody fills it in.
func assertSectionTableIsUsable(t *testing.T) {
	t.Helper()
	if len(pages.PanelSections) < 2 {
		t.Fatalf("PanelSections holds %d section(s); every test in this file would pass "+
			"vacuously over fewer than two", len(pages.PanelSections))
	}
	seen := map[string]bool{}
	for _, s := range pages.PanelSections {
		switch {
		case s.Label == "", s.Href == "", s.Task == "", s.Blurb == "":
			t.Fatalf("section %q has an empty field; the tests below compare against those "+
				"strings and an empty one matches everything", s.Tab)
		case seen[s.Href]:
			t.Fatalf("two sections share the href %q, so one of them can never be current", s.Href)
		}
		seen[s.Href] = true
	}
}

// TestPanelSections_EveryOneIsRoutedAndBehindTheGate is the property that makes
// the section table and the router one fact: every row is reachable when signed
// in, and NONE of them is reachable when not.
//
// The second half is the one that matters. handler.Mount registers these routes
// by ranging over the same slice INSIDE the protected group, so a section added
// tomorrow inherits Protect() -- but "inherits" is a claim about a code shape, and
// the shape it depends on (the r.Group closure) is one indentation level away from
// being wrong. This asserts the outcome instead.
func TestPanelSections_EveryOneIsRoutedAndBehindTheGate(t *testing.T) {
	assertSectionTableIsUsable(t)

	signedIn := panelBrowser(t)
	// A browser with NO panel cookie and a manager who resolves to nothing.
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{}, adminauth.ErrNoSession
	}}
	anonymous := newBrowser(t, newAdminRouter(t, admins, &fakeTrail{}))

	for _, s := range pages.PanelSections {
		if rec := signedIn.do(http.MethodGet, s.Href, nil); rec.Code != http.StatusOK {
			t.Errorf("signed in, GET %s (%s): %d, want 200 -- the section is in the "+
				"navigation and its link 404s", s.Href, s.Label, rec.Code)
		}
		rec := anonymous.do(http.MethodGet, s.Href, nil)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login" {
			t.Errorf("anonymous, GET %s (%s): %d %q, want 303 /admin/login -- a panel "+
				"section outside the gate is the whole product's data behind no password",
				s.Href, s.Label, rec.Code, rec.Header().Get("Location"))
		}
	}
}

// TestPanelSections_EachRequestMarksExactlyItsOwnTabCurrent pins two things a
// tab bar can get wrong in opposite directions: showing no current tab (the
// operator cannot tell where they are) and showing more than one.
//
// It also asserts the navigation offers EVERY section, as a set both ways. That
// is what stops a section from being routed but unreachable by clicking, which is
// the same defect as a dead link and much harder to notice.
func TestPanelSections_EachRequestMarksExactlyItsOwnTabCurrent(t *testing.T) {
	bodies := sectionBodies(t)

	want := map[string]bool{}
	for _, s := range pages.PanelSections {
		want[s.Href] = true
	}

	for _, s := range pages.PanelSections {
		links := tabLinksOf(bodies[s.Href])
		got := map[string]bool{}
		var current []string
		for _, l := range links {
			got[l.href] = true
			if l.current {
				current = append(current, l.href)
			}
		}
		if len(current) != 1 {
			t.Errorf("GET %s renders %d current tab(s) %v, want exactly 1",
				s.Href, len(current), current)
		} else if current[0] != s.Href {
			t.Errorf("GET %s marks %s as the current tab", s.Href, current[0])
		}
		for href := range want {
			if !got[href] {
				t.Errorf("GET %s does not offer a link to %s -- the section is routed "+
					"but cannot be reached by clicking", s.Href, href)
			}
		}
		for href := range got {
			if !want[href] {
				t.Errorf("GET %s offers a tab to %s, which is not a section", s.Href, href)
			}
		}
	}
}

// TestPanelSections_ShowTheirOwnEmptyStateAndNameTheTaskThatFillsIt is the
// property that the shell actually SWITCHES, which the current-tab test above
// cannot see: a navigation that highlights correctly while rendering the same body
// five times would pass every other test in this file.
//
// So each section must carry its own blurb AND NOT carry any other section's. The
// second half is the load-bearing one, and it is stated as a property over the
// table rather than as five expected strings.
func TestPanelSections_ShowTheirOwnEmptyStateAndNameTheTaskThatFillsIt(t *testing.T) {
	bodies := sectionBodies(t)

	// The task id is rendered, so it has to look like one. A section whose Task
	// drifted to "TODO" or "" would otherwise satisfy the containment checks below
	// trivially (every body contains "").
	taskRE := regexp.MustCompile(`^M[0-9]-[0-9]{2}$`)
	tasks := map[string]string{}
	for _, s := range pages.PanelSections {
		if !taskRE.MatchString(s.Task) {
			t.Errorf("section %q names its task as %q, which is not a roadmap task id",
				s.Label, s.Task)
		}
		if other, dup := tasks[s.Task]; dup {
			t.Errorf("sections %q and %q both claim to be filled by %s", other, s.Label, s.Task)
		}
		tasks[s.Task] = s.Label
	}

	for _, s := range pages.PanelSections {
		body := bodies[s.Href]
		if !strings.Contains(body, s.Task) {
			t.Errorf("GET %s does not say which task fills it (%s)", s.Href, s.Task)
		}
		if !strings.Contains(body, htmlText(s.Blurb)) {
			t.Errorf("GET %s does not render its own blurb", s.Href)
		}
		for _, other := range pages.PanelSections {
			if other.Href == s.Href {
				continue
			}
			if strings.Contains(body, htmlText(other.Blurb)) {
				t.Errorf("GET %s renders the %q section's text -- the shell is not "+
					"switching sections, only the navigation is", s.Href, other.Label)
			}
		}
	}
}

// TestPanelScreens_EveryPressTargetCarriesATouchTargetClass -- CLAUDE.md section 9
// and skill tappa-brand ask for a touch target of at least 44px on every control.
//
// THIS TEST DOES NOT MEASURE PIXELS, and the split is deliberate. It asserts that
// every anchor and every button on a panel screen carries one of the classes in
// panelTouchTargets; the class's actual height is asserted against the COMPILED
// stylesheet by the test below. Two halves, because neither half can be done where
// the other one is: markup does not know its own height, and a stylesheet does not
// know which elements exist.
//
// ⚠️ INHERITED LIMIT (M5-07, tour_test.go): this reads MARKUP. An element made
// pressable purely by stylesheet -- a large ::after over a card -- is invisible to
// it, and there is no test in this product that measures a rendered pixel.
func TestPanelScreens_EveryPressTargetCarriesATouchTargetClass(t *testing.T) {
	assertSectionTableIsUsable(t)
	b := panelBrowser(t)

	screens := map[string]string{}
	for _, s := range pages.PanelSections {
		screens["GET "+s.Href] = htmlOf(t, b.do(http.MethodGet, s.Href, nil))
	}
	// The two unauthenticated panel screens are the same brand surface and use the
	// same button component, so they are held to the same rule.
	anon := newBrowser(t, newAdminRouter(t, &fakeAdmins{}, &fakeTrail{}))
	screens["GET /admin/login"] = htmlOf(t, anon.do(http.MethodGet, "/admin/login", nil))

	total := 0
	for where, body := range screens {
		targets := pressTargetsOf(body)
		total += len(targets)
		for _, tgt := range targets {
			if !hasTouchTargetClass(tgt.classes) {
				t.Errorf("%s: <%s class=%q> is a press target carrying none of %v.\n"+
					"Either give it one of those classes, or add its class to "+
					"panelTouchTargets WITH its measured min-height -- the stylesheet "+
					"test below will then demand that height exists.",
					where, tgt.tag, tgt.classes, panelTouchTargets)
			}
		}
	}
	if total == 0 {
		t.Fatal("found no anchors and no buttons on any panel screen at all -- this test " +
			"is reading the wrong thing and would pass over an empty string")
	}
}

// TestPanelScreens_TouchTargetClassesReserve44px is the second half: the classes
// the markup test accepts must actually reserve 44px.
//
// ⚠️ IT SKIPS IN CI AND THAT IS A KNOWN, INHERITED DEBT, not a property of this
// test. web/static/css/app.css is gitignored and `make check` does not run
// `make css`, so the two TestCompiledCSS_* tests have always skipped there too. A
// SKIP IS NOT A PASS. What holds in CI is the markup half above; what holds
// locally, after `make css`, is both.
func TestPanelScreens_TouchTargetClassesReserve44px(t *testing.T) {
	raw, err := fs.ReadFile(web.Static(), "css/app.css")
	if err != nil {
		t.Skipf("no compiled stylesheet to read (%v) — run `make css`. THIS IS NOT A PASS.", err)
	}
	css := string(raw)

	// One flat rule: selectors, then declarations with no nested braces. Same shape
	// TestCompiledCSS_StampWordIsInk reads, and it matches rules inside @media too.
	ruleRE := regexp.MustCompile(`([^{}]*)\{([^{}]*)\}`)
	minHeightRE := regexp.MustCompile(`(?:^|;)min-height:([^;]*)`)

	for class, wantPx := range panelTouchTargets {
		found := false
		for _, rule := range ruleRE.FindAllStringSubmatch(css, -1) {
			if !regexp.MustCompile(`\.` + regexp.QuoteMeta(class) + `\b`).MatchString(rule[1]) {
				continue
			}
			m := minHeightRE.FindStringSubmatch(rule[2])
			if m == nil {
				continue
			}
			px, ok := remOrPxToPixels(strings.TrimSpace(m[1]))
			if !ok {
				t.Errorf(".%s declares min-height:%s, which this test cannot read in "+
					"pixels; use rem or px", class, m[1])
				continue
			}
			found = true
			if px < 44 {
				t.Errorf(".%s reserves %.1fpx, want at least 44px. A control smaller than "+
					"that is missed by a thumb, which on this product means a manager "+
					"tapping twice and a page they did not want.", class, px)
			}
			if px < float64(wantPx) {
				t.Errorf(".%s reserves %.1fpx but panelTouchTargets records %dpx -- the "+
					"table and the stylesheet disagree", class, px, wantPx)
			}
		}
		if !found {
			t.Errorf("no rule in the compiled stylesheet gives .%s a min-height. Either the "+
				"class is not a touch target and does not belong in panelTouchTargets, or "+
				"it lost its height and the markup test is now accepting a class that "+
				"reserves nothing.", class)
		}
	}
}

// TestPanelScreens_LoadNoScriptAndReachNoThirdParty pins the decision M6-02 made
// and the header that depends on it.
//
// THE PANEL HAS NO SCRIPT TODAY. HTMX is in the approved stack (CLAUDE.md section
// 1) and is NOT vendored: the tabs are plain links, so there is no fragment to
// swap and nothing to load. The Content-Security-Policy in adminlogin.go names no
// script-src for exactly that reason, and this test is what makes adding a script
// a VISIBLE edit -- it goes red on the first <script> tag, at which point the
// policy has to be widened deliberately rather than inherited silently.
func TestPanelScreens_LoadNoScriptAndReachNoThirdParty(t *testing.T) {
	bodies := sectionBodies(t)
	b := panelBrowser(t)

	seen := 0
	for href, body := range bodies {
		if i := strings.Index(strings.ToLower(body), "<script"); i >= 0 {
			t.Errorf("GET %s loads a script. The panel's CSP names no script-src "+
				"(adminlogin.go), so this tag does nothing except tell the next reader "+
				"that scripts are allowed here. If the panel now needs one, widen the "+
				"policy in the same commit and say how much and why.", href)
		}
		for _, m := range refRE.FindAllStringSubmatch(body, -1) {
			seen++
			ref := m[2]
			if strings.HasPrefix(ref, "//") || strings.Contains(ref, "://") {
				t.Errorf("GET %s reaches %s. Every asset is served from our own origin "+
					"(skill tappa-brand: absolute URL count 0) -- a CDN request would "+
					"send this page's URL to a third party.", href, ref)
			}
		}
	}
	if seen == 0 {
		t.Fatal("found no href/src attribute on any panel section -- every page links its " +
			"own stylesheet, so this scan is reading the wrong thing")
	}

	// And the header itself, on a section response rather than on a login page:
	// section responses go through the same render() and must carry the same policy.
	rec := b.do(http.MethodGet, pages.PanelSections[0].Href, nil)
	csp := rec.Header().Get("Content-Security-Policy")
	switch {
	case csp == "":
		t.Fatal("a panel section answers with no Content-Security-Policy")
	case !strings.Contains(csp, "default-src 'none'"):
		t.Errorf("panel CSP %q no longer starts from default-src 'none'", csp)
	case strings.Contains(csp, "unsafe-inline"), strings.Contains(csp, "unsafe-eval"):
		t.Errorf("panel CSP %q allows unsafe inline code", csp)
	}
}

// --- the brand nets -------------------------------------------------------
//
// These two read SOURCE FILES rather than rendered output, and the reason is the
// trap skill tappa-brand documents at length: the Tailwind standalone CLI scans
// .templ files AS RAW TEXT, comments and attribute values included. A colour named
// only in a sentence still compiles a rule into app.css. So a scan that looked
// only at class="…" attributes would miss the exact way this product has actually
// grown rules nothing renders.
//
// ⚠️ NO COUNT IS QUOTED HERE ON PURPOSE. M6-01 measured "seven rules, 334 bytes";
// a later audit counting by a different rule (it does not credit utilities absorbed
// into an @apply) measured five and 280. Both are defensible and neither is this
// test's business — what matters is the MECHANISM, which both methods agree on and
// which is what the scanners below are shaped around. A number two honest methods
// disagree about is exactly the kind this file has learned not to restate.

// offPaletteColour matches a Tailwind colour utility whose colour is NOT one of
// the nine brand tokens (tailwind.config.js: ink, porcelain, paper, tappa-green,
// green-lite, saffron, saffron-lite, tomato, line).
//
// IT IS A NEGATIVE PATTERN ON PURPOSE. The positive form -- "every colour utility
// must name a brand token" -- needs a list of every non-colour suffix that may
// follow bg-/text-/border- (text-sm, border-2, border-dashed, bg-repeat-x …), and
// that list is a fixed list: it grows, and growing it is how somebody eventually
// waves a real colour through. This form needs no such list. It names the DEFAULT
// TAILWIND PALETTE, which is fixed by the framework and is the only place an
// off-brand colour can come from short of an arbitrary value, which the next
// pattern catches.
//
// green-lite and saffron-lite survive it because the default palette's greens are
// numeric (green-500); ours are not.
var offPaletteColour = regexp.MustCompile(
	`\b(?:bg|text|border|ring|ring-offset|outline|decoration|divide|shadow|fill|stroke|accent|caret|placeholder|from|via|to)-` +
		`(?:(?:slate|gray|grey|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)-\d{2,3}|white|black)\b`)

// arbitraryColour matches Tailwind's arbitrary-value escape hatch carrying a
// colour: bg-[#fff], text-[rgb(0,0,0)]. The palette rule is "use an existing
// token's opacity, do not invent a hex" and this is the syntax for inventing one.
var arbitraryColour = regexp.MustCompile(
	`\b(?:bg|text|border|ring|outline|decoration|divide|shadow|fill|stroke|accent|caret|placeholder|from|via|to)-\[(?:#|rgb|hsl|color|oklch|lab)`)

// gradientUtility matches the utilities that BUILD a gradient. It does not match a
// bare from-/via-/to-, which would fire on the "end-to-end" in a comment and which
// paints nothing on its own anyway.
var gradientUtility = regexp.MustCompile(`\bbg-(?:gradient-to-[a-z]+|linear|radial|conic)\b`)

// brandSources are the files Tailwind compiles from and the stylesheet it starts
// from. tailwind.config.js is excluded on purpose: it is where the nine hexes are
// DEFINED, so scanning it would flag the palette for being the palette.
func brandSources(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	root := filepath.Join("..", "..")
	err := filepath.Walk(filepath.Join(root, "web", "templates"), func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(p, ".templ") {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[p] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatalf("walking the templates: %v", err)
	}
	css := filepath.Join(root, "web", "static", "css", "input.css")
	raw, err := os.ReadFile(css)
	if err != nil {
		t.Fatalf("reading %s: %v", css, err)
	}
	out[css] = string(raw)
	return out
}

// TestBrand_NoOffPaletteColourInAnySource -- skill tappa-brand, translated because
// CLAUDE.md section 7 keeps identifiers, logs and comments in English: "Do not go
// outside the palette. If you need a colour, use an existing token's opacity
// (bg-tappa-green/10); do not invent a new hex."
func TestBrand_NoOffPaletteColourInAnySource(t *testing.T) {
	sources := brandSources(t)
	if len(sources) < 5 {
		t.Fatalf("found %d source file(s) to scan; the product has more than that and "+
			"this test would pass over the wrong directory", len(sources))
	}
	for path, text := range sources {
		for _, hit := range offPaletteColours(text) {
			t.Errorf("%s: %s is not a Tappa colour. The palette is the nine tokens in "+
				"tailwind.config.js; use an existing token or its opacity.", path, hit)
		}
	}
}

// TestBrand_NoGradientOutsideTheDocketPerforation -- the brand forbids gradients,
// with exactly one exception, and the exception is not a gradient utility: the
// perforation is a backgroundImage TOKEN (bg-perf-top / bg-perf-bottom) declared
// in tailwind.config.js, which is why it does not need an allowlist entry here.
func TestBrand_NoGradientOutsideTheDocketPerforation(t *testing.T) {
	sources := brandSources(t)
	perforations := 0
	for path, text := range sources {
		perforations += strings.Count(text, "bg-perf-")
		if hit := gradientUtility.FindString(text); hit != "" {
			t.Errorf("%s: %s builds a gradient. skill tappa-brand: gradient yok. The one "+
				"gradient in the product is the docket perforation, and it is a "+
				"backgroundImage token rather than a utility.", path, hit)
		}
	}
	if perforations == 0 {
		t.Fatal("no bg-perf-* anywhere in the sources -- the docket's perforation is the " +
			"motif this product is recognised by, and this scan cannot be reading the " +
			"files that carry it")
	}
}

// TestBrandScanners_RejectTheThingsTheyExistToReject is the negative control for
// the three patterns above, and it is the reason they are not decoration.
//
// Without it, a regexp that matched NOTHING -- a stray escape, a lost alternation
// -- would leave both brand tests green forever over a product drifting off
// palette, and the failure would look exactly like success.
func TestBrandScanners_RejectTheThingsTheyExistToReject(t *testing.T) {
	offPalette := []string{
		`<p class="text-white">`,
		`<p class="bg-blue-500">`,
		`<p class="border-gray-200">`,
		`<p class="text-black">`,
		`// never write bg-red-600 here`, // a COMMENT still compiles a rule
	}
	for _, s := range offPalette {
		if got := offPaletteColours(s); len(got) == 0 {
			t.Errorf("offPaletteColour accepted %q, so it would accept the product "+
				"drifting off palette", s)
		}
	}
	arbitrary := []string{`class="bg-[#ff0000]"`, `class="text-[rgb(1,2,3)]"`}
	for _, s := range arbitrary {
		if got := offPaletteColours(s); len(got) == 0 {
			t.Errorf("arbitraryColour accepted %q", s)
		}
	}
	keep := []string{
		`class="bg-tappa-green/10 text-ink border-line"`,
		`class="bg-green-lite text-ink/70 border-saffron-lite"`,
		`class="text-sm border-2 border-dashed bg-repeat-x text-[10px]"`,
		`the tour goes end-to-end`,
	}
	for _, s := range keep {
		if got := offPaletteColours(s); len(got) != 0 {
			t.Errorf("offPaletteColour flagged %q as off-brand (%v); it is not, and a "+
				"scanner that cries wolf gets deleted", s, got)
		}
	}
	for _, s := range []string{`class="bg-gradient-to-r"`, `class="bg-linear-45"`} {
		if !gradientUtility.MatchString(s) {
			t.Errorf("gradientUtility accepted %q", s)
		}
	}
	for _, s := range []string{`class="bg-perf-top"`, `an end-to-end run`, `from-scratch`} {
		if gradientUtility.MatchString(s) {
			t.Errorf("gradientUtility flagged %q, which builds no gradient", s)
		}
	}
}

// --- helpers --------------------------------------------------------------

// panelTouchTargets is the ALLOWLIST half of the touch-target rule: the classes a
// press target on a panel screen may carry, each with the height it reserves.
//
// IT IS A LIST THAT HAS TO SHRINK TO WIDEN THE RULE, the shape internal/policy's
// namedAlertPreemptors uses. Adding an entry is not free: the compiled-stylesheet
// test demands that the class really declares at least that height, so a class
// added here without a height turns THAT test red instead.
var panelTouchTargets = map[string]int{
	"btn":        44, // .btn      -> min-h-11 = 2.75rem
	"tab-link":   44, // .tab-link -> min-h-11 = 2.75rem
	"tap-button": 64, // .tap-button -> min-h-16, the employee-facing target
}

func hasTouchTargetClass(classes string) bool {
	for _, c := range strings.Fields(classes) {
		if _, ok := panelTouchTargets[c]; ok {
			return true
		}
	}
	return false
}

type pressTarget struct {
	tag     string
	classes string
}

// pressTargetRE matches the opening tag of anything a person can press. It reads
// the whole `<a`/`<button` family rather than a list of the ones in use today.
var pressTargetRE = regexp.MustCompile(`(?is)<(a|button)\b([^>]*)>`)

func pressTargetsOf(body string) []pressTarget {
	var out []pressTarget
	for _, m := range pressTargetRE.FindAllStringSubmatch(body, -1) {
		classes := ""
		if c := attrValueRE("class").FindStringSubmatch(m[2]); len(c) == 2 {
			classes = c[1]
		}
		out = append(out, pressTarget{tag: strings.ToLower(m[1]), classes: classes})
	}
	return out
}

type tabLink struct {
	href    string
	current bool
}

// tabLinksOf reads the tab bar's links. It asks TWO independent questions about
// "current" -- the class and aria-current -- and requires both, because the brand
// rule is that status is never carried by colour alone: a tab bar that highlighted
// in green without saying so is invisible to a screen reader and would otherwise
// pass.
func tabLinksOf(body string) []tabLink {
	var out []tabLink
	for _, m := range pressTargetRE.FindAllStringSubmatch(body, -1) {
		if strings.ToLower(m[1]) != "a" {
			continue
		}
		attrs := m[2]
		classes := ""
		if c := attrValueRE("class").FindStringSubmatch(attrs); len(c) == 2 {
			classes = c[1]
		}
		fields := strings.Fields(classes)
		isTab, isCurrent := false, false
		for _, f := range fields {
			switch f {
			case "tab-link":
				isTab = true
			case "tab-link--current":
				isCurrent = true
			}
		}
		if !isTab {
			continue
		}
		href := ""
		if h := attrValueRE("href").FindStringSubmatch(attrs); len(h) == 2 {
			href = h[1]
		}
		aria := attrValueRE("aria-current").FindStringSubmatch(attrs) != nil
		out = append(out, tabLink{href: href, current: isCurrent && aria})
	}
	return out
}

// offPaletteColours returns every off-brand colour token in text, both flavours.
func offPaletteColours(text string) []string {
	out := offPaletteColour.FindAllString(text, -1)
	return append(out, arbitraryColour.FindAllString(text, -1)...)
}

// remOrPxToPixels reads a CSS length in rem or px. 1rem is 16px, which is the
// browser default this product does not override.
func remOrPxToPixels(v string) (float64, bool) {
	switch {
	case strings.HasSuffix(v, "rem"):
		n, err := strconv.ParseFloat(strings.TrimSuffix(v, "rem"), 64)
		return n * 16, err == nil
	case strings.HasSuffix(v, "px"):
		n, err := strconv.ParseFloat(strings.TrimSuffix(v, "px"), 64)
		return n, err == nil
	}
	return 0, false
}

// htmlText is what a Go string looks like after templ has escaped it into the
// page. Comparing against the raw string would silently stop matching the day a
// blurb acquires an apostrophe.
var htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;")

func htmlText(s string) string { return htmlEscaper.Replace(s) }

// The identities panelBrowser's session resolves to. Package-level so every test
// in this file drives ONE admin -- the per-session budget is keyed on the session
// id, and a fresh uuid per test would quietly hand each one its own 300.
var (
	panelTestAdmin   = uuid.MustParse("9f1c0a2e-0000-4000-8000-000000000001")
	panelTestTenant  = uuid.MustParse("9f1c0a2e-0000-4000-8000-000000000002")
	panelTestSession = uuid.MustParse("9f1c0a2e-0000-4000-8000-000000000003")
)

// --- the contrast net -----------------------------------------------------
//
// 🔴 THIS IS THE FIRST TEST IN THE PRODUCT THAT COMPUTES A CONTRAST RATIO, and it
// exists because M6-02 measured that the numbers were only ever in PROSE.
// result_test.go carries "1.52:1", "2.62:1", "5.70:1" in comments and failure
// messages, and input.css carries a paragraph of them — but nothing recomputed any
// of it, so .docket-label sat at 3.13:1 against AA's 4.5:1 from the M0 scaffold
// until 2026-08-04 and no test noticed. A number in a comment is a claim; this
// file's whole discipline is that a claim nobody re-derives is a claim that rots.
//
// 🔴 IT READS SOURCES, NOT THE COMPILED STYLESHEET, AND THAT IS THE POINT. The two
// TestCompiledCSS_* tests and TestPanelScreens_TouchTargetClassesReserve44px read
// web/static/css/app.css, which is gitignored and which `make check` never builds
// — so all three ALWAYS SKIP IN CI, and a skip is not a pass. input.css and
// tailwind.config.js are both committed, so this test runs everywhere, every time.
// It is the only accessibility net in the product that CI actually executes.

// docketLabelGrounds are the light surfaces a .docket-label can be rendered on.
//
// ⚠️ IT DELIBERATELY CARRIES NO CALL-SITE COUNTS, and that is a correction. The
// first version of this map said "10 of the 12 call sites (… tour …)" and "the 2
// sign-in form labels" — and BOTH numbers were wrong within the same round that
// wrote them: this task itself added a third porcelain call site (the panel
// header), and "tour" is not a file at all, the tour lives inside activate.templ,
// so it was double-counting activate. A hand-written count in a comment is exactly
// the thing this file exists to argue against.
//
// WHAT IS DERIVED, EXACTLY, because the sentence that used to sit here promised
// more than the code delivered — it said "the derived count lives in
// TestBrand_DocketLabelClearsAAOnEveryGroundItSitsOn, which reports it on failure"
// and that test counted nothing at all. It does now: docketLabelCallSites scans the
// templates, the AA test refuses to run when the count is zero, and the count is
// printed in the failure message.
//
// ⚠️ WHAT IS STILL NOT DERIVED: which GROUND each call site sits on. That needs the
// enclosing element, and nothing here parses HTML (the templates are read as text).
// So the map below is a list of surfaces a label MAY sit on, checked against the
// tone; it is not evidence that each surface has a call site or that each call site
// is on a listed surface.
//
// IT IS AN ALLOWLIST THAT MUST SHRINK, the namedAlertPreemptors shape: a ground
// listed here is one the label's tone HAS to clear AA on, so removing an entry is
// the only way to make the rule weaker and that is an edit somebody has to defend.
// Adding one is free and safe — it can only make the test stricter.
var docketLabelGrounds = map[string]string{
	"paper":     "the ground of .docket, .empty-state and components.Card",
	"porcelain": "the page ground itself, behind both shells — form labels and the panel header sit directly on it",
	// Neither Notice tone carries a docket-label today. They are listed anyway
	// because they are light SURFACES a label could legitimately land on, and
	// including them costs nothing: the tone clears AA on all four.
	"green-lite":   "components.Notice(ToneOK) — listed so a label could be added there without a surprise",
	"saffron-lite": "components.Notice(ToneWarn) — same",
}

// nonSurfaceGrounds are the palette colours used as a background that a
// .docket-label never sits on, each with the reason. Same rule: it must shrink.
//
// This is the half that makes the ground list DERIVED rather than curated. The
// test below reads every bg-<palette-token> actually used in the product and
// demands that each one appears in one map or the other — so introducing a new
// surface colour forces a decision (is a label allowed here?) instead of silently
// creating a ground nobody checked.
var nonSurfaceGrounds = map[string]string{
	"tappa-green": "the primary button and .tap-button; their label is text-paper, and no docket-label is ever placed on one",
	"ink":         "stamp--training's 10% tint, inside .stamp only",
	"saffron":     "stamp--flagged's 10% tint, inside .stamp only",
	"tomato":      "stamp--rejected's 10% tint, inside .stamp only",
	"line":        "stamp--ignored's 10% tint, inside .stamp only",
}

// aaSmallText is WCAG 2.1 AA for text below 18pt (or 14pt bold). .docket-label is
// 10px, so this is the threshold that applies to it — 3:1 is the LARGE-text figure
// and does not.
const aaSmallText = 4.5

// TestBrand_DocketLabelClearsAAOnEveryGroundItSitsOn recomputes, from the two
// committed sources, the arithmetic input.css claims in a comment.
func TestBrand_DocketLabelClearsAAOnEveryGroundItSitsOn(t *testing.T) {
	palette := brandPalette(t)
	token, alpha := docketLabelTone(t)

	fg, ok := palette[token]
	if !ok {
		t.Fatalf(".docket-label is coloured with %q, which is not a palette token", token)
	}
	if len(docketLabelGrounds) == 0 {
		t.Fatal("no grounds to check; this test would pass over anything")
	}
	// Anti-vacuity of the other kind: a tone can be perfect on every ground and
	// still guard nothing, if the class has stopped being used. Derived, never
	// hand-written — the comment on docketLabelGrounds explains what that cost.
	sites := docketLabelCallSites(t)
	if sites.Total == 0 {
		t.Fatal("nothing in the templates carries the docket-label class, so this test " +
			"is guarding a class the product no longer renders. Either the class was " +
			"renamed — in which case this test must follow it — or the motif is gone.")
	}
	for ground, why := range docketLabelGrounds {
		bg, ok := palette[ground]
		if !ok {
			t.Errorf("ground %q is not a palette token (%s)", ground, why)
			continue
		}
		got := contrastRatio(composite(fg, alpha, bg), bg)
		if got < aaSmallText {
			t.Errorf(".docket-label is text-%s/%d on %s => %.2f:1, want at least %.2f:1.\n"+
				"That ground is: %s\n"+
				"Derived call sites carrying the class: %d (%s)\n"+
				"This is the defect the class shipped with from M0 until 2026-08-04 "+
				"(/50 measured 3.13:1 on porcelain). /60 does NOT fix it (4.18:1); /70 does.",
				token, int(alpha*100), ground, got, aaSmallText, why,
				sites.Total, strings.Join(sites.ByFile, ", "))
		}
	}
}

// TestBrand_EveryInkToneClearsAA is the WIDENED net, and the reason it exists is
// that the narrow one was not enough — measured, in this task, twice.
//
// Round 2 fixed .docket-label and guarded exactly that one class. An audit then
// found SIX more sub-AA tones already shipped, one of which (the "punchless"
// wordmark at text-ink/40, 2.38:1) renders on EVERY screen in the product — and
// this task had just increased its render count by adding four panel screens. A
// net that protects one class while six of its siblings are unguarded is a net
// that will watch the same defect come back somewhere else.
//
// 🔴 IT CHECKS EVERY TRANSLUCENT INK TONE AGAINST THE DARKEST LIGHT GROUND IN USE,
// rather than pairing each occurrence with the surface it actually sits on. Pairing
// would need to resolve which element ENCLOSES each label, and nothing here parses
// HTML — the templates are read as text (result_test.go made the same choice and
// says so). The conservative substitute is exact enough to be worth having, and
// the arithmetic says why: on Tailwind's standard opacity scale the verdict is the
// same on every light ground in the palette. ink/60 fails on all four (4.11-4.36)
// and ink/70 passes on all four (5.58-6.05), so the ground choice never decides a
// case.
//
// ⚠️ THE PRICE OF THAT SHORTCUT, MEASURED RATHER THAN HAND-WAVED: the ground choice
// changes the AA verdict at exactly three opacities — /61, /62 and /63, where paper
// clears 4.5:1 and green-lite does not. NONE of them is on Tailwind's standard
// scale, so no ordinary edit can land in the gap. A mutation confirms the cost is
// real and bounded: swapping the derivation to pick the LIGHTEST ground instead of
// the darkest leaves this test green, i.e. it is an EQUIVALENT MUTANT today. That
// is written down as a known hole rather than left for an audit to find.
//
// WHAT IT DOES NOT COVER, so nobody reads more into a green run than is there:
// opaque tokens on coloured grounds. text-paper on bg-tappa-green is the only one
// in the product (7.73:1, measured by hand this round) and no test recomputes it,
// because "which ground is this text on" is the question above that text-reading
// cannot answer. The ink tones are where all seven real defects were.
func TestBrand_EveryInkToneClearsAA(t *testing.T) {
	palette := brandPalette(t)
	ground, groundName := darkestLightGroundInUse(t, palette)
	ink := palette["ink"]

	tones, occurrences := inkTonesInUse(t)
	// 🔴 THE GATE COUNTS OCCURRENCES, NOT DISTINCT TONES, and the difference is a
	// false alarm this test used to produce. It gated on "fewer than 2 distinct
	// tones" while the message said "there are dozens" — dozens of OCCURRENCES, of
	// which there are two distinct values. Normalising every ink/85 to ink/70 is a
	// legitimate simplification (both clear AA) and it left ONE distinct tone, so a
	// correct edit was told the scan was "reading the wrong files". The magnitude
	// that actually detects a broken scan is how much it found, not how varied it
	// was: one tone painted everywhere is a fine product, zero is a broken reader.
	if occurrences < 5 {
		t.Fatalf("found %d ink-tone occurrence(s) across %d distinct tone(s); this product "+
			"paints translucent ink on nearly every screen, so a number this low means "+
			"the scan is reading the wrong files and would pass over anything.\n"+
			"(A LOW COUNT OF DISTINCT TONES IS NOT A PROBLEM — one tone used everywhere "+
			"is a simplification, not a fault. Only a low count of OCCURRENCES is.)",
			occurrences, len(tones))
	}
	for tone, where := range tones {
		got := contrastRatio(composite(ink, float64(tone)/100, ground), ground)
		if got < aaSmallText {
			sort.Strings(where)
			t.Errorf("text-ink/%d on %s => %.2f:1, want at least %.2f:1.\n"+
				"Used at: %s\n"+
				"The smallest standard step that clears AA on every light ground in this "+
				"palette is /70 (worst 5.58:1). /60 does NOT (worst 4.11:1) — it was "+
				"measured and rejected on 2026-08-04, when six tones like this one were "+
				"already shipped and one of them rendered on every screen.",
				tone, groundName, got, aaSmallText, strings.Join(where, ", "))
		}
	}
}

// TestBrand_ProseNamesTheGroundTheMeasurementDERIVES closes the class this whole
// task kept producing, rather than the individual case.
//
// 🔴 THE DEFECT IT EXISTS FOR WAS SHIPPED BY THE ROUND THAT FIXED THE TONES. That
// round derived the binding ground correctly in code (green-lite) and wrote the
// WRONG one into input.css (porcelain) — in the same uncommitted change, two files
// apart, with the CSS comment's own printed table contradicting its own sentence
// two lines below it. Every tone VALUE was pinned by a test; WHICH GROUND IS
// BINDING was pinned by nothing, so the wrong claim was free to sit in the file a
// human actually reads before choosing the next tone.
//
// A comment cannot be made correct by review alone — this task reviewed it three
// times and it survived. So the sentence is given a machine-readable form and this
// test compares it against the derivation. The prose and the arithmetic can no
// longer disagree silently: they disagree in red.
func TestBrand_ProseNamesTheGroundTheMeasurementDERIVES(t *testing.T) {
	palette := brandPalette(t)
	_, derived := darkestLightGroundInUse(t, palette)

	raw, err := os.ReadFile(filepath.Join("..", "..", "web", "static", "css", "input.css"))
	if err != nil {
		t.Fatalf("reading input.css: %v", err)
	}
	// 🔴 FindAll, NOT Find, AND THE COUNT IS CHECKED. With FindStringSubmatch this
	// test read only the FIRST marker, so a second, CONTRADICTING "BINDING GROUND:"
	// line could be added below it and the suite stayed green — measured. A file
	// carrying two different answers in the place a human reads is exactly the
	// silent disagreement this test exists to make impossible, so "exactly one" is
	// part of the claim rather than an implementation detail.
	all := bindingGroundRE.FindAllStringSubmatch(string(raw), -1)
	if len(all) > 1 {
		var named []string
		for _, m := range all {
			named = append(named, m[1])
		}
		t.Fatalf("input.css names the binding ground %d times (%s). Exactly one line may "+
			"name it: two of them can disagree, and a reader has no way to know which "+
			"one the arithmetic actually used.", len(all), strings.Join(named, ", "))
	}
	m := bindingGroundRE.FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatalf("input.css names no binding ground.\n"+
			"It must carry a line reading %q so the sentence a human reads can be "+
			"checked against the ground this test derives (%s). Removing the line is "+
			"not a way to make this pass — it is how the claim went unchecked before.",
			"BINDING GROUND: <token>", derived)
	}
	if got := m[1]; got != derived {
		t.Errorf("input.css says the binding ground is %q; the palette says it is %q.\n"+
			"The derivation wins — it compares relative luminance across the light "+
			"grounds actually in use, and a DARKER ground gives dark text LESS "+
			"contrast. Fix the comment, and fix any margin figure computed from the "+
			"wrong ground with it.", got, derived)
	}
}

// TestBrand_EveryBackgroundInUseIsClassified is what keeps the ground list honest.
// It derives the backgrounds from the product and requires each to be classified.
func TestBrand_EveryBackgroundInUseIsClassified(t *testing.T) {
	palette := brandPalette(t)
	inUse := backgroundTokensInUse(t, palette)
	if len(inUse) < 3 {
		t.Fatalf("found %d background token(s) in the whole product; it has more than "+
			"that and this scan is reading the wrong files", len(inUse))
	}
	for token := range inUse {
		_, surface := docketLabelGrounds[token]
		_, other := nonSurfaceGrounds[token]
		switch {
		case surface && other:
			t.Errorf("bg-%s is classified BOTH as a docket-label ground and as a "+
				"non-surface; it cannot be both", token)
		case !surface && !other:
			t.Errorf("bg-%s is used as a background somewhere in the product and is in "+
				"neither docketLabelGrounds nor nonSurfaceGrounds.\n"+
				"Decide which it is. If a .docket-label can sit on it, add it to "+
				"docketLabelGrounds and the AA test above will hold the tone to it; if "+
				"it cannot, say why in nonSurfaceGrounds.", token)
		}
	}
}

// TestBrandContrast_MathIsNotVacuous is the negative control for the arithmetic
// itself. Without it, a contrastRatio that always returned 21 would leave the test
// above green over an unreadable product.
func TestBrandContrast_MathIsNotVacuous(t *testing.T) {
	white, black := [3]float64{255, 255, 255}, [3]float64{0, 0, 0}
	if got := contrastRatio(black, white); got < 20.9 || got > 21.1 {
		t.Errorf("black on white = %.2f:1, want 21:1 — the formula is wrong", got)
	}
	if got := contrastRatio(white, white); got < 0.99 || got > 1.01 {
		t.Errorf("white on white = %.2f:1, want 1:1", got)
	}

	palette := brandPalette(t)
	ink, paper, porcelain := palette["ink"], palette["paper"], palette["porcelain"]

	// The HISTORICAL failure, recomputed. If this ever clears AA, the formula has
	// drifted and the test above is no longer measuring anything.
	for _, tc := range []struct {
		name  string
		alpha float64
		bg    [3]float64
		want  float64
	}{
		{"the shipped-until-2026-08-04 /50 on paper", 0.50, paper, 3.23},
		{"the shipped-until-2026-08-04 /50 on porcelain", 0.50, porcelain, 3.13},
		{"the rejected /60 on porcelain", 0.60, porcelain, 4.18},
		{"the chosen /70 on porcelain", 0.70, porcelain, 5.70},
	} {
		got := contrastRatio(composite(ink, tc.alpha, tc.bg), tc.bg)
		if got < tc.want-0.02 || got > tc.want+0.02 {
			t.Errorf("%s: recomputed %.2f:1, but this task measured %.2f:1", tc.name, got, tc.want)
		}
		belowAA := got < aaSmallText
		wantBelow := tc.want < aaSmallText
		if belowAA != wantBelow {
			t.Errorf("%s: below AA = %v, want %v", tc.name, belowAA, wantBelow)
		}
	}

	// And the parser: it must read the tone out of the real file, not default to
	// something that happens to pass.
	token, alpha := docketLabelTone(t)
	if token != "ink" {
		t.Errorf(".docket-label tone token = %q, want ink", token)
	}
	if alpha <= 0 || alpha > 1 {
		t.Errorf(".docket-label alpha = %v, which is not an opacity", alpha)
	}
}

// --- contrast helpers -----------------------------------------------------

// inkTonesInUse returns every translucent ink tone the product paints, mapped to
// the files it appears in, AND the total number of occurrences.
//
// TWO MAGNITUDES, RETURNED SEPARATELY, because they answer different questions and
// conflating them produced a false alarm: the map's length is how VARIED the tones
// are (a design fact, and one is fine), the count is how MUCH was found (the only
// one that can reveal a broken scan). The caller gates on the second.
//
// Class attributes and Go-built class lists in the templates, @apply lists in
// input.css, comments excluded on both sides — a tone merely NAMED in prose is not
// a tone the product paints.
func inkTonesInUse(t *testing.T) (map[int][]string, int) {
	t.Helper()
	out := map[int][]string{}
	total := 0
	add := func(where, s string) {
		for _, m := range inkToneRE.FindAllStringSubmatch(s, -1) {
			pct, err := strconv.Atoi(m[1])
			if err != nil || pct <= 0 || pct > 100 {
				t.Errorf("%s: unreadable ink opacity %q", where, m[1])
				continue
			}
			total++
			if !slices.Contains(out[pct], where) {
				out[pct] = append(out[pct], where)
			}
		}
	}
	for path, text := range brandSources(t) {
		where := filepath.Base(path)
		if strings.HasSuffix(path, ".css") {
			code := cssCommentRE.ReplaceAllString(text, " ")
			for _, m := range applyOnly.FindAllStringSubmatch(code, -1) {
				add(where, m[1])
			}
			continue
		}
		for _, list := range classListsIn(text) {
			add(where, list)
		}
	}
	return out, total
}

// classUse is a count and its breakdown, together.
//
// 🔴 IT IS A STRUCT BECAUSE A BARE SLICE CAUSED THE FIFTH COUNTING ERROR IN THIS
// TASK. docketLabelCallSites used to return one entry PER FILE, and the caller
// printed len() under the label "Derived call sites" — so a product with 12 call
// sites across 5 files reported "5", with a breakdown on the SAME LINE that added
// up to 12. The label named one magnitude and the number measured another, which is
// the same defect as the two-denominator headroom that blocked the round before.
// Total and breakdown now travel together and cannot drift apart.
type classUse struct {
	// Total is the number of OCCURRENCES — the magnitude every caller labels.
	Total int
	// ByFile is "file xN", sorted, for the human reading a failure.
	ByFile []string
}

// docketLabelCallSites returns how many places apply the class, and where.
//
// It reads class attributes AND the class lists Go builds inside a .templ file
// (components.noticeClass is the live example). Reading only class="…" was a real
// blind spot — backgroundTokensInUse already scanned both and these did not, and
// nothing said so. A mention in PROSE is still not a call site: only literals whose
// every token looks like a class are considered.
func docketLabelCallSites(t *testing.T) classUse {
	t.Helper()
	var use classUse
	for path, text := range brandSources(t) {
		if strings.HasSuffix(path, ".css") {
			continue // where the class is DEFINED, not where it is used
		}
		n := 0
		for _, list := range classListsIn(text) {
			if slices.Contains(strings.Fields(list), "docket-label") {
				n++
			}
		}
		if n > 0 {
			use.Total += n
			use.ByFile = append(use.ByFile, fmt.Sprintf("%s x%d", filepath.Base(path), n))
		}
	}
	sort.Strings(use.ByFile)
	return use
}

// classListsIn yields every string in text that is used as a class list: the value
// of a class="…" attribute, plus Go string literals whose tokens all look like
// classes (noticeClass returns three of those).
//
// The literal arm is deliberately narrow. A sentence in a comment is not a class
// list, and admitting one would inflate every count built on this — which is
// exactly the failure this helper was introduced to stop repeating.
func classListsIn(text string) []string {
	var out []string
	for _, m := range classAttrOnly.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	// 🔴 THE ATTRIBUTES ARE REMOVED BEFORE THE LITERAL SCAN, and skipping this step
	// DOUBLED every count — measured, by the mutation written to check this very
	// function. `class="docket-label"` in a .templ file is also a quoted string in
	// the raw text, so both arms matched the same markup and 12 call sites reported
	// as 24. The literal arm exists only for lists Go BUILDS; anything already read
	// as an attribute must not be seen twice.
	rest := classAttrOnly.ReplaceAllString(text, "")
	for _, m := range goStringLiteralRE.FindAllStringSubmatch(rest, -1) {
		fields := strings.Fields(m[1])
		if len(fields) == 0 {
			continue
		}
		classy := true
		for _, f := range fields {
			if !classTokenRE.MatchString(f) {
				classy = false
				break
			}
		}
		if classy {
			out = append(out, m[1])
		}
	}
	return out
}

// darkestLightGroundInUse picks the surface that gives dark text the LEAST
// contrast, from the light grounds that are actually used.
//
// IT IS DERIVED RATHER THAN NAMED, and the derivation caught something a hand-
// written constant would not have: the binding ground is NOT porcelain, which is
// what two rounds of this task assumed because it is the page ground. It is
// green-lite (relative luminance 0.8229 against porcelain's 0.8627) — the ToneOK
// notice. Nothing in the product had ever compared them.
func darkestLightGroundInUse(t *testing.T, palette map[string][3]float64) ([3]float64, string) {
	t.Helper()
	inUse := backgroundTokensInUse(t, palette)
	worst, worstName, found := [3]float64{}, "", false
	for name := range docketLabelGrounds {
		if !inUse[name] {
			continue
		}
		c, ok := palette[name]
		if !ok {
			continue
		}
		if !found || relativeLuminance(c) < relativeLuminance(worst) {
			worst, worstName, found = c, name, true
		}
	}
	if !found {
		t.Fatal("no light ground from docketLabelGrounds is actually used as a background; " +
			"every ratio computed from this would be meaningless")
	}
	return worst, worstName
}

// contrastRatio is WCAG 2.1's formula, on sRGB channels in 0..255.
func contrastRatio(fg, bg [3]float64) float64 {
	a, b := relativeLuminance(fg), relativeLuminance(bg)
	hi, lo := a, b
	if b > a {
		hi, lo = b, a
	}
	return (hi + 0.05) / (lo + 0.05)
}

func relativeLuminance(c [3]float64) float64 {
	lin := func(v float64) float64 {
		v /= 255
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c[0]) + 0.7152*lin(c[1]) + 0.0722*lin(c[2])
}

// composite flattens a translucent foreground onto an opaque ground, which is what
// a browser paints for text-<token>/<n>. Contrast must be computed on the RESULT:
// comparing the raw token against the ground would report the ratio of a colour
// nobody can see.
func composite(fg [3]float64, alpha float64, bg [3]float64) [3]float64 {
	var out [3]float64
	for i := range out {
		out[i] = alpha*fg[i] + (1-alpha)*bg[i]
	}
	return out
}

// --- source parsing -------------------------------------------------------

var (
	cssCommentRE = regexp.MustCompile(`(?s)/\*.*?\*/`)
	paletteRE    = regexp.MustCompile(`'?([a-z][a-z-]*)'?\s*:\s*'(#[0-9A-Fa-f]{6})'`)
	docketRuleRE = regexp.MustCompile(`(?s)\.docket-label\s*\{(.*?)\}`)
	inkToneRE    = regexp.MustCompile(`\btext-ink/(\d{1,3})\b`)
	// The pinned sentence in input.css. Deliberately a fixed marker rather than
	// prose matching: a sentence this test had to parse loosely would be a sentence
	// it could misread, and the point is that it cannot.
	bindingGroundRE = regexp.MustCompile(`BINDING GROUND:\s*([a-z][a-z-]*)`)
	// A double-quoted Go string literal on one line, and a token that could be a
	// Tailwind class. Together they are how a class list built in Go is recognised
	// without admitting prose.
	goStringLiteralRE = regexp.MustCompile(`"([^"\n]*)"`)
	classTokenRE      = regexp.MustCompile(`^[a-z0-9][a-z0-9:/\[\]._-]*$`)
	toneRE            = regexp.MustCompile(`\btext-([a-z][a-z-]*)/(\d{1,3})\b`)
	bgTokenRE         = regexp.MustCompile(`\bbg-([a-z][a-z-]*)(?:/\d{1,3})?\b`)
	classAttrOnly     = regexp.MustCompile(`class="([^"]*)"`)
	applyOnly         = regexp.MustCompile(`@apply([^;]*);`)
)

// brandPalette reads the nine tokens from tailwind.config.js, which is the single
// place the hexes are defined. Hard-coding them here would be a second
// representation of the palette — the failure class CLAUDE.md names, and the one
// that would let this test keep passing after somebody changed a brand colour.
func brandPalette(t *testing.T) map[string][3]float64 {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "tailwind.config.js"))
	if err != nil {
		t.Fatalf("reading tailwind.config.js: %v", err)
	}
	out := map[string][3]float64{}
	for _, m := range paletteRE.FindAllStringSubmatch(string(raw), -1) {
		var c [3]float64
		for i := 0; i < 3; i++ {
			v, err := strconv.ParseUint(m[2][1+2*i:3+2*i], 16, 8)
			if err != nil {
				t.Fatalf("parsing %s: %v", m[2], err)
			}
			c[i] = float64(v)
		}
		out[m[1]] = c
	}
	if len(out) < 9 {
		t.Fatalf("read %d colour(s) from tailwind.config.js; the palette has nine, so "+
			"this parse is broken and every ratio below would be computed from nothing",
			len(out))
	}
	return out
}

// docketLabelTone reads the class's colour token and opacity out of input.css.
//
// COMMENTS ARE STRIPPED FIRST — DEFENSIVELY, AND IT IS NOT CURRENTLY LOAD-BEARING.
// An earlier version of this comment claimed it was, and a mutation measured that
// claim to be false: removing the strip left every test green, because the rule
// regexp looks for `.docket-label {` and today's comment block — which does contain
// the literal strings "text-ink/70" and "ink/50" — never spells that. So it is an
// EQUIVALENT MUTANT today and is written down as one rather than described as a
// protection the file does not currently need.
//
// It is kept because the prose above .docket-label is exactly the kind that grows,
// and one future sentence quoting the selector would make the parser read the tone
// out of the paragraph DESCRIBING the tone. Cheap insurance, honestly labelled.
func docketLabelTone(t *testing.T) (string, float64) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "web", "static", "css", "input.css"))
	if err != nil {
		t.Fatalf("reading input.css: %v", err)
	}
	code := cssCommentRE.ReplaceAllString(string(raw), " ")
	rule := docketRuleRE.FindStringSubmatch(code)
	if rule == nil {
		t.Fatal("no .docket-label rule in input.css — the class this test exists for is gone")
	}
	m := toneRE.FindStringSubmatch(rule[1])
	if m == nil {
		t.Fatalf(".docket-label declares no text-<token>/<opacity>: %q\n"+
			"If the tone became a solid token, this test needs to learn that shape "+
			"rather than be deleted.", strings.TrimSpace(rule[1]))
	}
	pct, err := strconv.Atoi(m[2])
	if err != nil || pct <= 0 || pct > 100 {
		t.Fatalf("unreadable opacity %q", m[2])
	}
	return m[1], float64(pct) / 100
}

// backgroundTokensInUse returns every palette colour used as a background, read
// from class attributes in the templates and @apply lists in input.css. Comments
// are excluded on both sides, so a colour merely NAMED in prose is not mistaken
// for a surface that exists.
func backgroundTokensInUse(t *testing.T, palette map[string][3]float64) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	collect := func(s string) {
		for _, m := range bgTokenRE.FindAllStringSubmatch(s, -1) {
			if _, ok := palette[m[1]]; ok {
				out[m[1]] = true
			}
		}
	}
	for path, text := range brandSources(t) {
		if strings.HasSuffix(path, ".css") {
			code := cssCommentRE.ReplaceAllString(text, " ")
			for _, m := range applyOnly.FindAllStringSubmatch(code, -1) {
				collect(m[1])
			}
			continue
		}
		for _, m := range classAttrOnly.FindAllStringSubmatch(text, -1) {
			collect(m[1])
		}
		// Class lists assembled in Go inside a .templ file (components.noticeClass).
		for _, lit := range regexp.MustCompile(`"([^"\n]*)"`).FindAllStringSubmatch(text, -1) {
			if strings.Contains(lit[1], "bg-") {
				collect(lit[1])
			}
		}
	}
	return out
}
