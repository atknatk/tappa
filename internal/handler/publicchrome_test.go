package handler

import (
	"bytes"
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

// TestPublicChrome_TheMenuAndTheFooterListOnlyMountedRoutesFromTheTable: the landing
// page's menu and footer are the table, mounted entries only.
func TestPublicChrome_TheMenuAndTheFooterListOnlyMountedRoutesFromTheTable(t *testing.T) {
	t.Parallel()
	body := mustFetchMarketing(t, marketingRouter(t), "/")
	if len(pages.PublicProductLinks) != 6 {
		t.Fatalf("PublicProductLinks has %d entries; the boards' Product column has six", len(pages.PublicProductLinks))
	}
	if len(pages.MountedPublicLinks(pages.PublicProductLinks)) == 0 {
		t.Fatal("PREMISE: no Product entry is mounted, so the scan below would compare two empty lists")
	}
	for _, v := range publicChromeLinkViolations(body, pages.PublicProductLinks, pages.LegalPages) {
		t.Error(v)
	}
	// One menu, and it is the native element: no script opens it.
	if n := strings.Count(body, `<details class="menu"`); n != 1 {
		t.Errorf("the page renders %d phone menu(s), want one <details class=\"menu\">", n)
	}
	// Header and menu are named landmarks.
	for _, label := range []string{"Primary", "Menu", "Product", "Legal"} {
		if chromeSegment(body, label) == "" {
			t.Errorf("no <nav aria-label=%q> on the page", label)
		}
	}
	if !strings.Contains(body, `<header class="lp-head" aria-label="`) {
		t.Error("the public header carries no aria-label")
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

	// The table says a page is mounted that the chrome does not draw: the first entry
	// whose page is not mounted yet, given a route.
	ahead := clone()
	unmounted := slices.IndexFunc(ahead, func(l pages.PublicLink) bool { return l.Href == "" })
	if unmounted < 0 {
		t.Fatal("PREMISE: every Product entry is mounted; this control needs one that is not (retire it in LP-11)")
	}
	ahead[unmounted].Href = "/not-mounted"
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
