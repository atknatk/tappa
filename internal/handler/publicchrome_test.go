package handler

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/atknatk/tappa/web/templates/pages"
)

// THE PUBLIC CHROME (LP-02): the header, the phone menu, the phone's Start free bar
// and the footer every public page draws (pages.PublicChrome). What is held here is
// the link table's contract — one table, mounted routes only, in the boards' order —
// and the footer's red-line sentence. That every link resolves is
// TestMarketing_EveryInternalLinkResolves's; that the header's entries resolve and mark
// the current page is TestPublicNav_EveryEntryResolvesAndMarksItsOwnPage's.

// chromeSegment is the markup of the <nav> whose aria-label is label, up to its
// closing tag, or "" when the page renders no such nav.
func chromeSegment(body, label string) string {
	return between(body, `aria-label="`+label+`"`, "</nav>")
}

// hrefsIn is every link target in segment, in document order.
func hrefsIn(segment string) []string {
	var out []string
	for _, m := range hrefRE.FindAllStringSubmatch(segment, -1) {
		out = append(out, m[1])
	}
	return out
}

// publicChromeLinkViolations says how body's menu and footer depart from the table:
// the menu and the footer's Product column carry exactly the mounted entries of
// product, in order, and no label of an entry that is not mounted; the Legal column
// carries legal, in order. Package level so the negative control can hand it a table
// the page was not rendered from.
func publicChromeLinkViolations(body string, product []pages.PublicLink, legal []pages.LegalPage) []string {
	var out []string
	var want []string
	for _, l := range pages.MountedPublicLinks(product) {
		want = append(want, l.Href)
	}
	for _, where := range []string{"Menu", "Product"} {
		seg := chromeSegment(body, where)
		if seg == "" {
			out = append(out, "the page renders no <nav aria-label=\""+where+"\">")
			continue
		}
		if got := hrefsIn(seg); strings.Join(got, " ") != strings.Join(want, " ") {
			out = append(out, where+" links to ["+strings.Join(got, " ")+"]; the table's mounted entries are ["+
				strings.Join(want, " ")+"]")
		}
		text := strings.Join(strings.Fields(tagRE.ReplaceAllString(seg, " ")), " ")
		for _, l := range product {
			if l.Href == "" && strings.Contains(text, l.Label) {
				out = append(out, where+" names "+l.Label+", whose page is not mounted (a link arrives with its route)")
			}
		}
	}
	var legalWant []string
	for _, p := range legal {
		legalWant = append(legalWant, p.Path)
	}
	if got := hrefsIn(chromeSegment(body, "Legal")); strings.Join(got, " ") != strings.Join(legalWant, " ") {
		out = append(out, "Legal links to ["+strings.Join(got, " ")+"]; LegalPages is ["+strings.Join(legalWant, " ")+"]")
	}
	return out
}

// TestPublicChrome_TheMenuAndTheFooterListOnlyMountedRoutesFromTheTable: on every
// page that draws the chrome -- the landing, the five pages of the site, and since
// LP-11 sign-up, the four legal documents and the guide's two pages, thirteen -- the
// menu and footer are the table, mounted entries only.
//
// AND THE TABLE IS IN ITS FINAL STATE (LP-11 closes the P2 cards' conditional
// acceptance): every one of the six Product entries is mounted, in the boards' order,
// and the header's three entries are pages, none a "/#…" section of the landing.
func TestPublicChrome_TheMenuAndTheFooterListOnlyMountedRoutesFromTheTable(t *testing.T) {
	t.Parallel()
	if len(pages.PublicProductLinks) != 6 {
		t.Fatalf("PublicProductLinks has %d entries; the boards' Product column has six", len(pages.PublicProductLinks))
	}
	var labels []string
	for _, l := range pages.PublicProductLinks {
		labels = append(labels, l.Label)
		if l.Href == "" {
			t.Errorf("the Product column's %q is not mounted; every page exists since LP-10", l.Label)
		}
	}
	if got, want := strings.Join(labels, " | "), "Product tour | Security | Pricing | For restaurants & bars | For production & facilities | API docs"; got != want {
		t.Errorf("the Product column reads %q; the boards read %q", got, want)
	}
	for _, l := range pages.LandingNav {
		if strings.Contains(l.Href, "#") {
			t.Errorf("the header's %q is %q, a landing section; every header entry is a page since LP-08", l.Label, l.Href)
		}
	}
	r := marketingRouter(t)
	for _, url := range publicChromeURLs() {
		body := mustFetchMarketing(t, r, url)
		for _, v := range publicChromeLinkViolations(body, pages.PublicProductLinks, pages.LegalPages) {
			t.Errorf("%s: %s", url, v)
		}
		// One menu, and it is the native element: no script opens it.
		if n := strings.Count(body, `<details class="menu"`); n != 1 {
			t.Errorf("%s renders %d phone menu(s), want one <details class=\"menu\">", url, n)
		}
		// Header and menu are named landmarks.
		for _, label := range []string{"Primary", "Menu", "Product", "Legal"} {
			if chromeSegment(body, label) == "" {
				t.Errorf("%s: no <nav aria-label=%q> on the page", url, label)
			}
		}
		if !strings.Contains(body, `<header class="lp-head" aria-label="`) {
			t.Errorf("%s: the public header is missing or carries no aria-label", url)
		}
	}
}

// publicChromeURLs is every page that draws the public chrome and is served by the
// marketing router: the site's own (siteURLs), sign-up's first screen and the pages
// that draw it around a body of their own (chromeOnlyURLs).
func publicChromeURLs() []string {
	out := append(siteURLs(), signupPath)
	return append(out, chromeOnlyURLs()...)
}

// voidElements are the HTML elements with no closing tag.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true,
}

var (
	htmlTagRE   = regexp.MustCompile(`<(/?)([a-zA-Z][a-zA-Z0-9]*)((?:[^>"']|"[^"]*"|'[^']*')*)>`)
	htmlClassRE = regexp.MustCompile(`\bclass="([^"]*)"`)
)

// insideLPRoot reports, for the first element whose opening tag starts with open,
// whether an ancestor of it (or the element itself) carries the class lp; found is
// false when no such element exists. The page is walked as a stack of open tags,
// which is exact for the markup templ writes: every element closed, attributes
// quoted, no "<" in text.
func insideLPRoot(body, open string) (inside, found bool) {
	type frame struct {
		name string
		lp   bool
	}
	var stack []frame
	for _, m := range htmlTagRE.FindAllStringSubmatchIndex(body, -1) {
		name := strings.ToLower(body[m[4]:m[5]])
		if body[m[2]:m[3]] == "/" {
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].name == name {
					stack = stack[:i]
					break
				}
			}
			continue
		}
		attrs := body[m[6]:m[7]]
		lp := false
		if c := htmlClassRE.FindStringSubmatch(attrs); c != nil {
			lp = slices.Contains(strings.Fields(c[1]), "lp")
		}
		if strings.HasPrefix(body[m[0]:], open) {
			for _, f := range stack {
				lp = lp || f.lp
			}
			return lp, true
		}
		if !voidElements[name] && !strings.HasSuffix(attrs, "/") {
			stack = append(stack, frame{name, lp})
		}
	}
	return false, false
}

// lpRootProblems says how body departs from the layout its kind needs: ownBody pages
// (sign-up, the legal documents, the guide) keep <main> outside every .lp root and the
// header, the footer and the phone bar inside one; the site's own pages keep all of
// them inside theirs.
func lpRootProblems(body string, ownBody bool) []string {
	var out []string
	parts := []struct {
		open       string
		wantInside bool
		optional   bool
	}{
		{`<main id="main">`, !ownBody, false},
		{`<header class="lp-head"`, true, false},
		{`<footer class="lp-foot">`, true, false},
		{`<div class="mbar">`, true, true},
	}
	for _, p := range parts {
		inside, found := insideLPRoot(body, p.open)
		if !found {
			if !p.optional {
				out = append(out, p.open+" is not on the page")
			}
			continue
		}
		if inside != p.wantInside {
			out = append(out, fmt.Sprintf("%s inside a .lp root = %v, want %v", p.open, inside, p.wantInside))
		}
	}
	// Nor may a root open INSIDE the body: the reset reaches whatever such a root holds.
	if ownBody {
		for _, c := range htmlClassRE.FindAllStringSubmatch(between(body, `<main id="main">`, "</main>"), -1) {
			if slices.Contains(strings.Fields(c[1]), "lp") {
				out = append(out, fmt.Sprintf("an element in <main> opens a .lp root (class=%q)", c[1]))
			}
		}
	}
	return out
}

// TestPublicChrome_UtilityBodiesStayOutsideTheLPRoot: the pages whose body is written
// in the product's utility classes -- every sign-up screen, the four legal documents,
// the guide's two pages -- draw the chrome in .lp roots of its own and keep <main>
// outside them, because inside a .lp root `.lp *` zeroes the margins and paddings
// those classes set (measured in Chrome: publicchrome.templ gives the numbers). The
// site's own pages keep everything inside their root, as before. CONTROL: a body
// wrapped in a .lp root, a .lp root opened inside the body, and a footer taken out of
// its root are each red.
func TestPublicChrome_UtilityBodiesStayOutsideTheLPRoot(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	for _, url := range siteURLs() {
		for _, p := range lpRootProblems(mustFetchMarketing(t, r, url), false) {
			t.Errorf("%s: %s", url, p)
		}
	}
	own := map[string]string{}
	for _, url := range chromeOnlyURLs() {
		own[url] = mustFetchMarketing(t, r, url)
	}
	for name, body := range signupSurfaceScreens(t) {
		own["sign-up "+name] = body
	}
	for name, body := range own {
		for _, p := range lpRootProblems(body, true) {
			t.Errorf("%s: %s", name, p)
		}
	}
	legal := own[pages.LegalPages[0].Path]
	if len(lpRootProblems(legal, true)) != 0 {
		t.Fatal("PREMISE: the legal page fails the scan")
	}
	wrapped := strings.Replace(legal, `<main id="main">`, `<div class="lp"><main id="main">`, 1)
	wrapped = strings.Replace(wrapped, `</main>`, `</main></div>`, 1)
	if wrapped == legal || len(lpRootProblems(wrapped, true)) == 0 {
		t.Error("CONTROL: a legal body wrapped in a .lp root passed")
	}
	inner := strings.Replace(legal, `<main id="main">`, `<main id="main"><div class="lp">`, 1)
	inner = strings.Replace(inner, `</main>`, `</div></main>`, 1)
	if inner == legal || len(lpRootProblems(inner, true)) == 0 {
		t.Error("CONTROL: a legal body opening a .lp root inside <main> passed")
	}
	bare := strings.Replace(legal, `<div class="lp"><footer class="lp-foot">`, `<div><footer class="lp-foot">`, 1)
	if bare == legal || len(lpRootProblems(bare, true)) == 0 {
		t.Error("CONTROL: a footer outside every .lp root passed")
	}
}

// TestPublicChrome_LinkScanRefusesATableThePageDoesNotMatch is the negative control:
// a page the scan passes must fail it once the table and the page disagree, either
// way round.
func TestPublicChrome_LinkScanRefusesATableThePageDoesNotMatch(t *testing.T) {
	t.Parallel()
	body := mustFetchMarketing(t, marketingRouter(t), "/")
	if v := publicChromeLinkViolations(body, pages.PublicProductLinks, pages.LegalPages); len(v) != 0 {
		t.Fatalf("PREMISE: the real table fails the scan: %v", v)
	}
	clone := func() []pages.PublicLink { return append([]pages.PublicLink(nil), pages.PublicProductLinks...) }

	// The table says a page is mounted that the chrome does not draw. Since LP-09 and
	// LP-10 every Product entry is mounted, so the entry is a synthetic row -- a path no
	// card mounts, the way TestPublicPages_TheTableIsTheMountedRoutes' control writes one.
	ahead := append(clone(), pages.PublicLink{Label: "For nobody", Href: "/for/nobody"})
	if len(publicChromeLinkViolations(body, ahead, pages.LegalPages)) == 0 {
		t.Error("a table entry the menu and footer do not draw passed the scan")
	}
	// The chrome draws a link the table does not list as mounted.
	behind := clone()
	behind[len(behind)-1].Href = ""
	if len(publicChromeLinkViolations(body, behind, pages.LegalPages)) == 0 {
		t.Error("a link the table does not mount passed the scan")
	}
	// The legal column lost a document.
	if len(publicChromeLinkViolations(body, pages.PublicProductLinks, pages.LegalPages[:3])) == 0 {
		t.Error("a Legal column that does not match LegalPages passed the scan")
	}
}

// renderPublicChrome renders the chrome around nothing, inside a .lp root.
func renderPublicChrome(t *testing.T, c pages.PublicChromeOpts) string {
	t.Helper()
	var b bytes.Buffer
	if err := pages.PublicChrome(c).Render(t.Context(), &b); err != nil {
		t.Fatalf("rendering the public chrome: %v", err)
	}
	return b.String()
}

// TestPublicChrome_MarksTheCurrentPageAndNothingElse: an entry pointing at the
// page's own path carries aria-current="page"; on the landing page none does.
func TestPublicChrome_MarksTheCurrentPageAndNothingElse(t *testing.T) {
	t.Parallel()
	landing := mustFetchMarketing(t, marketingRouter(t), "/")
	if strings.Contains(landing, `aria-current`) {
		t.Error("the landing page marks a chrome entry as current; no entry points at /")
	}
	c := pages.PublicChromeOpts{Current: pages.APIGuidePath, SignInHref: adminLoginPath, SignupHref: signupPath}
	html := renderPublicChrome(t, c)
	for _, where := range []string{"Menu", "Product"} {
		seg := chromeSegment(html, where)
		if !strings.Contains(seg, `href="`+pages.APIGuidePath+`" aria-current="page"`) {
			t.Errorf("%s: the current page's entry is not marked aria-current=\"page\"", where)
		}
	}
	if n := strings.Count(html, `aria-current="page"`); n != 2 {
		t.Errorf("aria-current=\"page\" appears %d time(s); want the menu's and the footer's entry for the page", n)
	}
	legal := renderPublicChrome(t, pages.PublicChromeOpts{Current: pages.LegalPages[1].Path, SignInHref: adminLoginPath})
	if !strings.Contains(chromeSegment(legal, "Legal"), `href="`+pages.LegalPages[1].Path+`" aria-current="page"`) {
		t.Error("the Legal column does not mark the current document")
	}
}

// TestPublicChrome_ThePhoneBarIsTheWizardsAndCanBeLeftOut: the phone's Start free
// bar links the wizard; Sign-up leaves it out, and no wizard means no bar and no
// header button.
func TestPublicChrome_ThePhoneBarIsTheWizardsAndCanBeLeftOut(t *testing.T) {
	t.Parallel()
	landing := mustFetchMarketing(t, marketingRouter(t), "/")
	bar := between(landing, `<div class="mbar">`, "</div>")
	if !strings.Contains(bar, `href="`+signupPath+`"`) {
		t.Errorf("the landing page's phone bar does not link %s", signupPath)
	}
	// The bar follows the footer, so at the end of the page it sits below the last
	// content rather than over it.
	if strings.Index(landing, `<div class="mbar">`) < strings.Index(landing, "</footer>") {
		t.Error("the phone bar comes before the footer; at the foot of the page it would cover it")
	}
	off := renderPublicChrome(t, pages.PublicChromeOpts{SignInHref: adminLoginPath, SignupHref: signupPath, NoPhoneBar: true})
	if strings.Contains(off, `class="mbar"`) {
		t.Error("NoPhoneBar still renders the phone bar")
	}
	if !strings.Contains(off, `class="hbtn" href="`+signupPath+`"`) {
		t.Error("NoPhoneBar also took the header's Start free; it leaves only the bar out")
	}
	none := renderPublicChrome(t, pages.PublicChromeOpts{SignInHref: adminLoginPath})
	if strings.Contains(none, `class="mbar"`) || strings.Contains(none, "Start free") {
		t.Error("with no wizard mounted the chrome still offers Start free")
	}
	// LP-11: the legal documents and the guide draw the bar too, after the footer, in
	// the root that sticks it to the bottom; no sign-up screen draws it.
	r := marketingRouter(t)
	for _, url := range chromeOnlyURLs() {
		body := mustFetchMarketing(t, r, url)
		at := strings.Index(body, `<div class="lp lp-dock"><div class="mbar"><a class="hbtn" href="`+signupPath+`">`)
		if at < 0 || at < strings.Index(body, "</footer>") {
			t.Errorf("%s: the phone bar is missing, outside its root or before the footer", url)
		}
	}
	for name, body := range signupSurfaceScreens(t) {
		if strings.Contains(body, `class="mbar"`) {
			t.Errorf("the sign-up %s screen draws the phone bar; the boards draw it on every public phone page but Signup", name)
		}
	}
}

// TestPublicChrome_TheFooterStatesTheTwoRedLines: the footer carries §4.1 and §4.2 in
// the words redline-check's R1 already recognises, the §4.1 denial on one source line.
func TestPublicChrome_TheFooterStatesTheTwoRedLines(t *testing.T) {
	t.Parallel()
	const (
		noBodyData = "Taptime collects no biometric data of any kind: no fingerprints, no face, no voice."
		promise    = "A phone's position is read only at the moment somebody presses the button."
	)
	landing := mustFetchMarketing(t, marketingRouter(t), "/")
	footer := screenText(t, "<body>"+between(landing, "<footer", "</footer>")+"</body>")
	for _, s := range []string{noBodyData, promise} {
		if !strings.Contains(footer, s) {
			t.Errorf("the footer does not say %q", s)
		}
	}
	src := repoFile(t, "web", "templates", "pages", "publicchrome.templ")
	oneLine := false
	for _, line := range strings.Split(src, "\n") {
		if strings.Contains(line, noBodyData) {
			oneLine = true
		}
	}
	if !oneLine {
		t.Error("the §4.1 sentence is not on one line of publicchrome.templ; R1 is line-local and " +
			"clears it only beside its denial")
	}
}
