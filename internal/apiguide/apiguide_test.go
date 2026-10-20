package apiguide

// apiguide_test.go -- the conversion's contract (M11 DOCS-1, ADR 0030):
//
//	TestRender_NoRawHTMLAndOnlySafeLinks        a hostile Markdown input: no raw HTML, no other
//	                                            scheme, no image is drawn; the link texts survive
//	TestRender_AnchorsBecomeHeadingIDs          <a id="x"></a> before a heading is that heading's id
//	                                            and permalink; the title leaves the body
//	TestRender_FileLinksAreMappedOrDrawnAsText  a mapped file-relative link becomes the site address
//	                                            (fragment kept); an unmapped one is text
//	TestRender_DropsHTMLComments                a comment block's text never reaches the page; neither
//	                                            guide's page carries a comment
//	TestRender_EveryInternalLinkResolves        in both guides, every href="#x" names an id on the
//	                                            page, and every anchor line of the Markdown is an id
//	TestRender_TheTwoLanguagesAreOneDocument    the two pages have the same ids, heading levels,
//	                                            tables, code blocks and link targets, in order
//	TestRender_LeavesTheSourceAlone             converting does not change the embedded bytes
//	                                            (/v1/guide.md serves them: TestGuide_IsServedUnderV1)

import (
	"bytes"
	"crypto/sha256"
	"regexp"
	"slices"
	"strings"
	"testing"

	apidocs "github.com/atknatk/tappa/docs/api"
)

func mustRender(t *testing.T, src string, opt Options) Guide {
	t.Helper()
	g, err := Render([]byte(src), opt)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return g
}

// TestRender_NoRawHTMLAndOnlySafeLinks feeds the converter what a hostile or careless edit of the
// guide could carry. Mutants (measured, DOCS-1 report): html.WithUnsafe on the renderer -> the
// <script>, the <img onerror> and the <iframe> are written; siteTarget accepting every target ->
// the ftp:, mailto: and tel: links are drawn (goldmark's own filter refuses javascript:, vbscript:,
// file: and data: but not those, which is why this rule is ours); siteTarget judging the source
// spelling instead of util.URLEscape(dest, true) -> the four escaped/entity //evil.example links
// are drawn (DOCS-1 low round).
func TestRender_NoRawHTMLAndOnlySafeLinks(t *testing.T) {
	src := "# Title\n\n" +
		"<script>alert(1)</script>\n\n" +
		"<iframe src=\"https://evil.example/\"></iframe>\n\n" +
		"A tag inline <img src=x onerror=alert(1)> and <b onclick=\"alert(1)\">bold</b>.\n\n" +
		"[js](javascript:alert(1)) [JS](JaVaScRiPt:alert(1)) [data](data:text/html,<script>alert(1)</script>) " +
		"[vb](vbscript:msgbox) [ftp](ftp://files.example/x) [mail](mailto:someone@example.test) [tel](tel:+35600000000) " +
		"[proto](//evil.example/x) [spaced](<java\tscript:alert(1)>)\n\n" +
		"<mailto:someone@example.test> <ftp://files.example/y> <https://ok.example/auto>\n\n" +
		"![pixel](https://tracker.example/p.gif)\n\n" +
		"[web](https://ok.example/a) [plain](http://ok.example/b) [here](#title-x) [site](/docs/api/tr)\n\n" +
		// Spelled as a site path, written as a scheme-relative //host: goldmark removes the backslash
		// escape and resolves the entities before it writes the href (DOCS-1 security audit, D).
		"[bs](/\\/evil.example) [num](/&#47;evil.example/x) [named](/&sol;evil.example) [ref][r] " +
		// A browser reads http:x on an https page as http://x/.
		"[hostless](http:x) <http:x>\n\n" +
		"[r]: /\\/evil.example\n"
	body := renderString(t, mustRender(t, src, Options{}))

	for _, refused := range []string{
		"<script", "<iframe", "<img", "onerror", "onclick", "<b ", "evil.example", "tracker.example",
		`href="javascript`, `href="JaVaScRiPt`, `href="data`, `href="vbscript`, `href="ftp`, `href="mailto`,
		`href="tel`, `href="//`, `href="java`, `href="/\\`, `href="http:x`, `evil.example`,
	} {
		if strings.Contains(body, refused) {
			t.Errorf("the output carries %q:\n%s", refused, body)
		}
	}
	for _, drawn := range []string{
		`<a href="https://ok.example/a">web</a>`, `<a href="http://ok.example/b">plain</a>`,
		`<a href="#title-x">here</a>`, `<a href="/docs/api/tr">site</a>`,
		`<a href="https://ok.example/auto">https://ok.example/auto</a>`,
		// A refused link keeps its text, without the anchor element.
		"js", "ftp", "mail", "tel", "proto", "pixel", "someone@example.test", "ftp://files.example/y",
		"bs", "num", "named", "ref", "hostless", "http:x",
	} {
		if !strings.Contains(body, drawn) {
			t.Errorf("the output lacks %q:\n%s", drawn, body)
		}
	}
	// Anti-vacuity: the raw blocks reached the renderer and were refused there, not lost earlier.
	if !strings.Contains(body, "raw HTML omitted") {
		t.Errorf("the raw HTML did not reach the renderer's refusal:\n%s", body)
	}
	if n := strings.Count(body, "<a "); n != 5 {
		t.Errorf("%d links drawn, want the five safe ones:\n%s", n, body)
	}
	// A browser reads a backslash in a URL as a slash, so /\host is //host. goldmark writes a raw
	// backslash in a link target as %5C today (a path on this site), so no Markdown reaches this
	// rule; it is held here, on the function, so that a renderer that stops encoding it does not
	// open the hole. Mutant (measured): the /\ prefix rule removed -> red.
	for _, written := range []string{`/\evil.example`, `/\/evil.example`, `//evil.example`, "http:x", "https:/x"} {
		if dest, ok := siteTarget(written, nil); ok {
			t.Errorf("siteTarget(%q) = %q, drawn", written, dest)
		}
	}
}

// TestRender_AnchorsBecomeHeadingIDs: the guide's anchor convention, and the title out of the body.
// Mutant (measured): an id written with a prefix, or the anchor regexp loosened to take the closing
// tag into the id -> red here and in TestRender_EveryInternalLinkResolves.
func TestRender_AnchorsBecomeHeadingIDs(t *testing.T) {
	src := "# The `guide` title\n\nIntro.\n\n<a id=\"first\"></a>\n## 1. First `code`\n\n" +
		"<a id=\"first-sub\"></a>\n### 1.1 Sub\n\nText [up](#first).\n\n## Unanchored\n"
	g := mustRender(t, src, Options{})
	body := renderString(t, g)
	if g.Title != "The guide title" {
		t.Errorf("Title %q", g.Title)
	}
	if strings.Contains(body, "<h1") || strings.Contains(body, "raw HTML omitted") || strings.Contains(body, "&lt;a id") {
		t.Errorf("the title or an anchor line is in the body:\n%s", body)
	}
	for _, want := range []string{
		`<h2 id="first"><a href="#first">1. First <code>code</code></a></h2>`,
		`<h3 id="first-sub"><a href="#first-sub">1.1 Sub</a></h3>`,
		`<h2>Unanchored</h2>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the output lacks %q:\n%s", want, body)
		}
	}
	want := []Section{{"first", "1. First code", 2}, {"first-sub", "1.1 Sub", 3}}
	if !slices.Equal(g.Sections, want) {
		t.Errorf("Sections %v, want %v", g.Sections, want)
	}
	if _, err := Render([]byte("# T\n\n<a id=\"orphan\"></a>\n\nnot a heading\n"), Options{}); err == nil {
		t.Error("an anchor line that is not before a heading converted silently")
	}
	if _, err := Render([]byte("## No title\n"), Options{}); err == nil {
		t.Error("a guide without a level-1 heading converted")
	}
}

// TestRender_FileLinksAreMappedOrDrawnAsText: the Markdown's links to its sibling files work on the
// site; a path into the repository that the site does not serve is not drawn as a dead link.
func TestRender_FileLinksAreMappedOrDrawnAsText(t *testing.T) {
	src := "# T\n\n[tr](integration-guide.tr.md) [tr-hooks](integration-guide.tr.md#webhooks) " +
		"[spec](openapi.yaml) [adr](../adr/0028-x.md) [nohost](openapi.yaml)\n"
	links := map[string]string{"integration-guide.tr.md": "/docs/api/tr", "openapi.yaml": "https://api.example.test/v1/openapi.yaml"}
	body := renderString(t, mustRender(t, src, Options{Links: links}))
	for _, want := range []string{
		`<a href="/docs/api/tr">tr</a>`, `<a href="/docs/api/tr#webhooks">tr-hooks</a>`,
		`<a href="https://api.example.test/v1/openapi.yaml">spec</a>`, " adr ",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the output lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "0028") {
		t.Errorf("an unmapped repository path was drawn:\n%s", body)
	}
	links["openapi.yaml"] = ""
	body = renderString(t, mustRender(t, src, Options{Links: links}))
	if strings.Contains(body, "openapi.yaml\"") || !strings.Contains(body, "spec") {
		t.Errorf("a link mapped to \"\" was drawn:\n%s", body)
	}
}

// TestRender_DropsHTMLComments holds ADR 0030 §3's sentence "HTML comment blocks (the guide's test
// markers) are dropped": a comment's text never reaches the page, and no comment does in either
// guide (eight markers each). Mutant (measured): the comment removal in transform skipped -> the
// renderer writes "<!-- raw HTML omitted -->" in their place and this goes red.
func TestRender_DropsHTMLComments(t *testing.T) {
	body := renderString(t, mustRender(t, "# T\n\n<!-- example: a private note -->\n\nText.\n\n<!--\nmulti\nline\n-->\n", Options{}))
	if strings.Contains(body, "<!--") || strings.Contains(body, "private note") || strings.Contains(body, "multi") {
		t.Errorf("a comment reached the page:\n%s", body)
	}
	if !strings.Contains(body, "<p>Text.</p>") {
		t.Errorf("the text around the comments was lost:\n%s", body)
	}
	for lang, g := range theGuides(t) {
		src, body := g[0], g[1]
		if n := strings.Count(src, "\n<!-- "); n < 5 {
			t.Fatalf("%s: %d comment markers in the Markdown -- not the guide", lang, n)
		}
		if n := strings.Count(body, "<!--"); n != 0 {
			t.Errorf("%s: %d comments on the page, want 0", lang, n)
		}
	}
}

var (
	idAttr    = regexp.MustCompile(` id="([^"]+)"`)
	hrefAttr  = regexp.MustCompile(` href="([^"]*)"`)
	mdAnchor  = regexp.MustCompile(`(?m)^<a id="([a-z0-9-]+)"></a>$`)
	mdHashRef = regexp.MustCompile(`\]\(#([^)\s]+)\)`)
	structure = regexp.MustCompile(`<(h[1-6]|table|pre|ol|ul|blockquote)[ >]`)
)

// theGuides converts both embedded guides as the site does.
func theGuides(t *testing.T) map[string][2]string {
	t.Helper()
	out := map[string][2]string{}
	for name, src := range map[string][]byte{"en": apidocs.English(), "tr": apidocs.Turkish()} {
		out[name] = [2]string{string(src), renderString(t, mustRender(t, string(src), Options{}))}
	}
	return out
}

// TestRender_EveryInternalLinkResolves: the guide's own links (its contents list, its "see section
// 6.2") reach a section on the page, in both languages, and no anchor of the Markdown is lost.
// /docs/api#webhooks is one of them -- the panel's webhook screen links there.
func TestRender_EveryInternalLinkResolves(t *testing.T) {
	for lang, g := range theGuides(t) {
		src, body := g[0], g[1]
		ids := map[string]bool{}
		for _, m := range idAttr.FindAllStringSubmatch(body, -1) {
			if ids[m[1]] {
				t.Errorf("%s: the id %q twice", lang, m[1])
			}
			ids[m[1]] = true
		}
		anchors := mdAnchor.FindAllStringSubmatch(src, -1)
		if len(anchors) < 50 || !ids["webhooks"] {
			t.Fatalf("%s: %d anchors in the Markdown, webhooks %v -- not the guide", lang, len(anchors), ids["webhooks"])
		}
		for _, m := range anchors {
			if !ids[m[1]] {
				t.Errorf("%s: the Markdown's anchor %q is no id on the page", lang, m[1])
			}
		}
		if len(ids) != len(anchors) {
			t.Errorf("%s: %d ids on the page, %d anchors in the Markdown", lang, len(ids), len(anchors))
		}
		refs := mdHashRef.FindAllStringSubmatch(src, -1)
		hrefs := 0
		for _, m := range hrefAttr.FindAllStringSubmatch(body, -1) {
			if !strings.HasPrefix(m[1], "#") {
				t.Errorf("%s: a link out of the page, %q (the guide has none today)", lang, m[1])
				continue
			}
			hrefs++
			if !ids[m[1][1:]] {
				t.Errorf("%s: the link %q names no id on the page", lang, m[1])
			}
		}
		// Every Markdown link is on the page, plus one permalink per anchored heading.
		if hrefs != len(refs)+len(anchors) {
			t.Errorf("%s: %d in-page links on the page, want the Markdown's %d plus %d permalinks", lang, hrefs, len(refs), len(anchors))
		}
	}
}

// TestRender_TheTwoLanguagesAreOneDocument extends the Markdown's parity test
// (internal/handler/api's TestGuide_TheTwoLanguagesAreOneDocument) to the pages: the same ids,
// headings, tables, lists, code blocks and link targets, in the same order.
//
// It found one difference (2026-10-19): a wrapped line of the English §9.9 began "+ `version`",
// which CommonMark -- on GitHub as here -- reads as a one-item bullet list the Turkish page did not
// have. The Markdown was rewrapped (DOCS-1 low round), so the comparison takes both files as they
// are; a line that starts a list by accident again goes red here.
func TestRender_TheTwoLanguagesAreOneDocument(t *testing.T) {
	en := renderString(t, mustRender(t, string(apidocs.English()), Options{}))
	tr := renderString(t, mustRender(t, string(apidocs.Turkish()), Options{}))
	for name, re := range map[string]*regexp.Regexp{"ids": idAttr, "links": hrefAttr, "structure": structure} {
		a, b := all(re, en), all(re, tr)
		if len(a) < 10 || !slices.Equal(a, b) {
			t.Errorf("the two pages' %s differ (%d vs %d)", name, len(a), len(b))
		}
	}
	codeBlocks := regexp.MustCompile(`(?s)<pre><code[^>]*>.*?</code></pre>`)
	if a, b := codeBlocks.FindAllString(en, -1), codeBlocks.FindAllString(tr, -1); len(a) < 10 || !slices.Equal(a, b) {
		t.Errorf("the two pages' code blocks differ (%d vs %d)", len(a), len(b))
	}
	ge, _ := Render(apidocs.English(), Options{})
	gt, _ := Render(apidocs.Turkish(), Options{})
	if len(ge.Sections) != len(gt.Sections) {
		t.Fatalf("sections %d vs %d", len(ge.Sections), len(gt.Sections))
	}
	for i := range ge.Sections {
		if ge.Sections[i].ID != gt.Sections[i].ID || ge.Sections[i].Level != gt.Sections[i].Level {
			t.Errorf("section %d: %v vs %v", i, ge.Sections[i], gt.Sections[i])
		}
	}
}

// TestRender_LeavesTheSourceAlone: converting is a read. The bytes /v1/guide.md serves are the
// file's (TestGuide_IsServedUnderV1 compares them with the disk); this holds that drawing the page
// does not change them.
func TestRender_LeavesTheSourceAlone(t *testing.T) {
	for _, src := range [][]byte{apidocs.English(), apidocs.Turkish()} {
		before := sha256.Sum256(src)
		if _, err := Render(src, Options{}); err != nil {
			t.Fatal(err)
		}
		if sha256.Sum256(src) != before {
			t.Error("Render changed its input")
		}
	}
}

func all(re *regexp.Regexp, s string) []string {
	var out []string
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		out = append(out, m[len(m)-1])
	}
	return out
}

func renderString(t *testing.T, g Guide) string {
	t.Helper()
	var b bytes.Buffer
	if err := g.Body().Render(t.Context(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
