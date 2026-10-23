package handler

// apiguidepage_test.go -- the integration guide on the product site (M11 DOCS-1, ADR 0030):
//
//	TestAPIGuidePages_AreTheGuideInTheMarketingChrome   /docs/api and /docs/api/tr: 200 HTML, indexable,
//	                                                    the title, the side navigation, the body, the
//	                                                    page's language, the public chrome around it
//	                                                    (LP-11) and below it; HEAD, caching, the policy, no
//	                                                    third party and every link resolving are the
//	                                                    marketing tests' (publicSurfaceURLs)
//	TestAPIGuidePages_RoutesAreTheLinkedPaths           the routes are the constants the links use
//	TestAPIGuidePages_LinkTheOtherLanguage              each page links the other
//	TestAPIGuidePages_NameBothTranslationsInTheHead     hreflang alternates for both, nowhere else
//	TestAPIGuidePages_FitAPhone                         tables and code scroll in their own box; the
//	                                                    contents list's links are touch targets
//	TestAPIGuidePages_TheSideNavigationSticksBelowTheHeader  its offset is the compiled header's height + 24
//	TestAPIGuidePages_JumpInsteadOfGliding              anchor jumps are instant on the guide only
//	TestAPIGuidePages_OfferOpenAPIOnlyWithAnAPIHost     the download link is the API host's, or absent
//	TestAPIGuidePages_AreLinkedFromTheFootersAndThePanel  the two footers, the panel's API keys screen
//	                                                    and its webhook section (to #webhooks)
//	TestAPIGuidePages_ServedOnTheProductHostOnly        404 on the operator host and on the API host

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	apidocs "github.com/atknatk/tappa/docs/api"
	"github.com/atknatk/tappa/internal/apiguide"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

func TestAPIGuidePages_AreTheGuideInTheMarketingChrome(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	for _, c := range []struct {
		path, lang string
		src        []byte
	}{{pages.APIGuidePath, "en", apidocs.English()}, {pages.APIGuideTRPath, "tr", apidocs.Turkish()}} {
		rec := fetchMarketing(t, r, c.path, nil)
		body := rec.Body.String()
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
			t.Fatalf("GET %s: %d %q", c.path, rec.Code, rec.Header().Get("Content-Type"))
		}
		g, err := apiguide.Render(c.src, apiguide.Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			`<meta name="robots" content="index, follow">`,
			"<title>" + htmlText(g.Title) + "</title>",
			"<h1 class=\"mt-2 font-display text-3xl font-bold tracking-tight\">" + htmlText(g.Title) + "</h1>",
			`<article class="` + pages.PublicBodyFrame + ` py-10 sm:py-14" lang="` + c.lang + `">`,
			`<div class="guide-prose mt-8">`,
			`<h2 id="webhooks"><a href="#webhooks">`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s lacks %q", c.path, want)
			}
		}
		// THE PUBLIC CHROME, since LP-11: its header and footer, and the page's own entry
		// marked in the menu and the footer on the English page (the Turkish page is another
		// URL, so no entry points at it). Where the side navigation sticks is
		// TestAPIGuidePages_TheSideNavigationSticksBelowTheHeader's.
		for _, want := range []string{`<header class="lp-head"`, `<footer class="lp-foot">`} {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s lacks %q", c.path, want)
			}
		}
		if marked := strings.Contains(body, `href="`+pages.APIGuidePath+`" aria-current="page"`); marked != (c.path == pages.APIGuidePath) {
			t.Errorf("GET %s marks the guide's chrome entry current = %v", c.path, marked)
		}
		// The guide's title names the product; the document title does not name it twice.
		if title := between(body, "<title>", "</title>"); strings.Count(title, "Taptime") != 1 {
			t.Errorf("GET %s: the title names Taptime %d times: %q", c.path, strings.Count(title, "Taptime"), title)
		}
		// The side navigation lists every anchored section but the guide's own contents list.
		nav := between(body, `<nav class="hidden xl:block`, "</nav>")
		listed := 0
		for _, s := range g.Sections {
			link := `href="#` + s.ID + `"`
			if s.ID == apiguide.ContentsID {
				if strings.Contains(nav, link) {
					t.Errorf("GET %s: the side navigation lists the contents list itself", c.path)
				}
				continue
			}
			listed++
			if !strings.Contains(nav, link) {
				t.Errorf("GET %s: the side navigation lacks %s", c.path, link)
			}
		}
		if listed < 50 {
			t.Fatalf("GET %s: %d sections -- not the guide", c.path, listed)
		}
		// The body is the conversion's, byte for byte.
		var b bytes.Buffer
		if err := g.Body().Render(t.Context(), &b); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body, b.String()) {
			t.Errorf("GET %s does not carry the converted guide", c.path)
		}
	}
}

// TestAPIGuidePages_RoutesAreTheLinkedPaths: the routes Mount registers are the paths the pages,
// the footers and the panel link to.
func TestAPIGuidePages_RoutesAreTheLinkedPaths(t *testing.T) {
	t.Parallel()
	if apiGuidePath != pages.APIGuidePath || apiGuideTRPath != pages.APIGuideTRPath {
		t.Fatalf("routes %q %q, links %q %q", apiGuidePath, apiGuideTRPath, pages.APIGuidePath, pages.APIGuideTRPath)
	}
}

// TestAPIGuidePages_LinkTheOtherLanguage. Mutant (measured): either link removed from the template
// -> red.
func TestAPIGuidePages_LinkTheOtherLanguage(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	for path, want := range map[string]*regexp.Regexp{
		pages.APIGuidePath:   regexp.MustCompile(`<a href="/docs/api/tr" hreflang="tr" lang="tr"[^>]*>\s*Türkçe\s*</a>`),
		pages.APIGuideTRPath: regexp.MustCompile(`<a href="/docs/api" hreflang="en" lang="en"[^>]*>\s*English\s*</a>`),
	} {
		if !want.MatchString(mustFetchMarketing(t, r, path)) {
			t.Errorf("GET %s does not link the other language (%s)", path, want)
		}
	}
}

// TestAPIGuidePages_NameBothTranslationsInTheHead: each page's head names the English and the
// Turkish page (hreflang, root-relative -- layout.MarketingTranslated says why), both are served,
// and no other marketing page carries an alternate. Mutant (measured): the alternates loop removed
// from documentHead -> red.
func TestAPIGuidePages_NameBothTranslationsInTheHead(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	want := []string{
		`<link rel="alternate" hreflang="en" href="` + pages.APIGuidePath + `">`,
		`<link rel="alternate" hreflang="tr" href="` + pages.APIGuideTRPath + `">`,
	}
	for _, path := range []string{pages.APIGuidePath, pages.APIGuideTRPath} {
		head := between(mustFetchMarketing(t, r, path), "<head>", "</head>")
		for _, w := range want {
			if !strings.Contains(head, w) {
				t.Errorf("GET %s: the head lacks %s", path, w)
			}
		}
		if n := strings.Count(head, `rel="alternate"`); n != len(want) {
			t.Errorf("GET %s: %d alternates in the head, want %d", path, n, len(want))
		}
		if code := fetchMarketing(t, r, path, nil).Code; code != http.StatusOK {
			t.Errorf("the alternate %s answers %d", path, code)
		}
	}
	for _, path := range marketingURLs() {
		if strings.Contains(mustFetchMarketing(t, r, path), `rel="alternate"`) {
			t.Errorf("GET %s carries an alternate; only the guide is translated", path)
		}
	}
}

// TestAPIGuidePages_FitAPhone holds the phone-width rules the page relies on (DOCS-1, low round):
// every table and code block of the guide is inside the scoped wrapper whose rules make it scroll
// in its own box (a table a block, both at most the column's width, sideways scroll), and the
// guide's contents list -- the table of contents where no side navigation is drawn -- follows its
// heading directly, which is what the 44 px touch-target rule selects. The rules are read from
// input.css and, when it has been built, from the compiled app.css. Mutants (measured): the
// sideways scroll removed from the table rule, or the contents rule's selector changed -> red.
func TestAPIGuidePages_FitAPhone(t *testing.T) {
	t.Parallel()
	css := repoFile(t, "web", "static", "css", "input.css")
	rules := map[string][]string{
		".guide-prose table":              {"block", "max-w-full", "overflow-x-auto"},
		".guide-prose pre":                {"max-w-full", "overflow-x-auto"},
		".guide-prose h2#contents + ul a": {"inline-flex", "min-h-11", "items-center"},
	}
	for sel, utilities := range rules {
		m := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(sel) + ` \{ @apply ([^;]*);`).FindStringSubmatch(css)
		if m == nil {
			t.Errorf("input.css has no %q rule", sel)
			continue
		}
		have := strings.Fields(m[1])
		for _, u := range utilities {
			if !slices.Contains(have, u) {
				t.Errorf("input.css: %q lacks %s (has %v)", sel, u, have)
			}
		}
	}
	if compiled, err := os.ReadFile(filepath.Join("..", "..", "web", "static", "css", "app.css")); err == nil {
		for _, want := range []string{"display:block;max-width:100%;overflow-x:auto", ".guide-prose h2#contents+ul a{align-items:center;display:inline-flex;min-height:2.75rem}"} {
			if !strings.Contains(string(compiled), want) {
				t.Errorf("app.css lacks %q", want)
			}
		}
	}

	r := marketingRouter(t)
	for _, path := range []string{pages.APIGuidePath, pages.APIGuideTRPath} {
		body := mustFetchMarketing(t, r, path)
		prose := between(body, `<div class="guide-prose mt-8">`, "</article>")
		for _, tag := range []string{"<table>", "<pre>"} {
			all, inside := strings.Count(body, tag), strings.Count(prose, tag)
			if all < 5 || inside != all {
				t.Errorf("GET %s: %d %s on the page, %d inside the guide's wrapper", path, all, tag, inside)
			}
		}
		if !regexp.MustCompile(`<h2 id="` + apiguide.ContentsID + `"><a href="#` + apiguide.ContentsID + `">[^<]+</a></h2>\s*<ul>\s*<li><a href="#`).MatchString(prose) {
			t.Errorf("GET %s: the contents heading is not directly followed by its list of links", path)
		}
	}
}

// sideNavClassFor is the class the side navigation's box must carry under css: it
// sticks the header's height plus 24 px from the top -- the header being its row's
// min-height from the tablet width (the side navigation is drawn only far above it) and
// the header's bottom hairline -- and leaves 24 px below. It says why it cannot derive
// the height instead, when the header's rules are not the shape it reads.
func sideNavClassFor(css string) (string, error) {
	row := cssDecls(css, "@media (min-width:768px)", ".lp .lp-headIn")
	minH, err := strconv.Atoi(strings.TrimSuffix(row["min-height"], "px"))
	if err != nil || row["height"] != "auto" {
		return "", fmt.Errorf("the header row's tablet rule is height %q, min-height %q; the derivation reads an auto height over a px min-height", row["height"], row["min-height"])
	}
	for _, media := range []string{"@media (min-width:1200px)", ""} {
		d := cssDecls(css, media, ".lp .lp-headIn")
		if _, ok := d["min-height"]; ok && media != "" {
			return "", fmt.Errorf("the desk width sets the header row's min-height (%q); the side navigation (from 1280 px) would sit under another height", d["min-height"])
		}
		if h, ok := d["height"]; ok && media != "" {
			return "", fmt.Errorf("the desk width sets the header row's height (%q)", h)
		}
	}
	edge, _, _ := strings.Cut(cssDecls(css, "", ".lp .lp-head")["border-bottom"], " ")
	line, err := strconv.Atoi(strings.TrimSuffix(edge, "px"))
	if err != nil {
		return "", fmt.Errorf("the header's bottom edge is %q; the derivation reads a px width", edge)
	}
	top := minH + line + 24
	return fmt.Sprintf("sticky top-[%dpx] max-h-[calc(100vh-%dpx)] ", top, top+24), nil
}

// TestAPIGuidePages_TheSideNavigationSticksBelowTheHeader: from the extra-large width the
// guide's side navigation sticks, and so does the public header above it (LP-11); the
// navigation's offset is DERIVED here from the compiled header rules -- its row's
// min-height and its hairline -- plus the 24 px gap, so a header of another height turns
// this red instead of sliding the navigation's first entries under it. Measured in
// Chrome at 1440: header 0-69, navigation 93-776 in an 800 px window. CONTROL: a header
// row 4 px taller, a desk rule giving it a height, and the navigation at the old 24 px
// offset are each red.
func TestAPIGuidePages_TheSideNavigationSticksBelowTheHeader(t *testing.T) {
	t.Parallel()
	css := compiledCSS(t)
	want, err := sideNavClassFor(css)
	if err != nil {
		t.Fatal(err)
	}
	if want != "sticky top-[93px] max-h-[calc(100vh-117px)] " {
		t.Logf("derived %q", want)
	}
	r := marketingRouter(t)
	for _, path := range []string{pages.APIGuidePath, pages.APIGuideTRPath} {
		nav := between(mustFetchMarketing(t, r, path), `<nav class="hidden xl:block`, "</nav>")
		if !strings.Contains(nav, `<div class="`+want) {
			t.Errorf("GET %s: the side navigation's box is not %q; the header above it is %s tall", path, want, strings.TrimPrefix(want, "sticky top-"))
		}
	}
	taller := strings.Replace(css, "min-height:68px;padding:0 40px}", "min-height:72px;padding:0 40px}", 1)
	if got, _ := sideNavClassFor(taller); taller == css || got == want {
		t.Error("CONTROL: a 72 px header row derived the same offset")
	}
	desk := strings.Replace(css, ".lp .lp-headIn{margin:0 auto;", ".lp .lp-headIn{height:80px;margin:0 auto;", 1)
	if _, err := sideNavClassFor(desk); desk == css || err == nil {
		t.Error("CONTROL: a desk rule giving the header row a height passed")
	}
	if want == "sticky top-6 max-h-[calc(100vh-3rem)] " {
		t.Error("CONTROL: the derivation answers the old offset")
	}
}

// TestAPIGuidePages_JumpInsteadOfGliding: the public site scrolls smoothly to an anchor
// (html:has(.lp)), and the integration guide -- some 50,000 px of text whose contents
// links are its navigation -- does not (an orchestrator decision at LP-11's review):
// its header root alone carries .lp-instant, and the compiled stylesheet answers it
// with an instant scroll after, and heavier than, the smooth one. Measured in Chrome:
// the guide's root element computes scroll-behavior auto, /legal/cookies and / smooth.
// CONTROL: the rule removed is red.
func TestAPIGuidePages_JumpInsteadOfGliding(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	for _, url := range append(publicChromeURLs(), pages.APIGuideTRPath) {
		guide := url == pages.APIGuidePath || url == pages.APIGuideTRPath
		if got := strings.Contains(mustFetchMarketing(t, r, url), "lp-instant"); got != guide {
			t.Errorf("%s carries the instant scroll = %v, want %v (the guide's pages only)", url, got, guide)
		}
	}
	problem := func(css string) string {
		smooth := strings.Index(css, "html:has(.lp){scroll-behavior:smooth}")
		instant := strings.Index(css, "html:has(.lp.lp-instant){scroll-behavior:auto}")
		if smooth < 0 || instant < smooth {
			return fmt.Sprintf("smooth rule at %d, the guide's instant rule at %d; want both, the guide's after", smooth, instant)
		}
		return ""
	}
	css := compiledCSS(t)
	if p := problem(css); p != "" {
		t.Error(p)
	}
	if problem(strings.Replace(css, "html:has(.lp.lp-instant){scroll-behavior:auto}", "", 1)) == "" {
		t.Error("CONTROL: app.css without the guide's instant scroll passed")
	}
}

func TestAPIGuidePages_OfferOpenAPIOnlyWithAnAPIHost(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.DiscardHandler)
	with := httpx.NewRouter(nil, nil, NewMarketing(nil, log, "api.example.test", ""))
	without := marketingRouter(t)
	for _, path := range []string{pages.APIGuidePath, pages.APIGuideTRPath} {
		if body := mustFetchMarketing(t, with, path); !strings.Contains(body, `<a href="https://api.example.test/v1/openapi.yaml"`) {
			t.Errorf("GET %s with an API host does not link its openapi.yaml", path)
		}
		if body := mustFetchMarketing(t, without, path); strings.Contains(body, "openapi.yaml\"") || strings.Contains(body, "Download openapi") {
			t.Errorf("GET %s without an API host links an OpenAPI document", path)
		}
	}
}

func TestAPIGuidePages_AreLinkedFromTheFootersAndThePanel(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	footer := `href="` + pages.APIGuidePath + `"`
	for _, path := range []string{"/", pages.LegalPages[0].Path} {
		if body := mustFetchMarketing(t, r, path); !strings.Contains(between(body, "<footer", "</footer>"), footer) {
			t.Errorf("GET %s: the footer does not link the integration guide", path)
		}
	}
	var b bytes.Buffer
	if err := pages.AdminAPIKeys(pages.APIKeysView{}).Render(t.Context(), &b); err != nil {
		t.Fatal(err)
	}
	panel := b.String()
	if !strings.Contains(between(panel, "Integration keys</h2>", "</section>"), footer) {
		t.Error("the API keys screen does not link the integration guide")
	}
	if !strings.Contains(between(panel, `id="webhook"`, "</section>"), `href="`+pages.APIGuidePath+`#webhooks"`) {
		t.Error("the webhook section does not link the guide's webhook section")
	}
}

// TestAPIGuidePages_ServedOnTheProductHostOnly: the operator host serves the operator, and the API
// host serves /v1 and nothing else (the two host gates); the guide's HTML is the product host's.
func TestAPIGuidePages_ServedOnTheProductHostOnly(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{OperatorHost: "ops.example.test", APIHost: "api.example.test"}
	h := httpx.NewRouter(cfg, nil, NewMarketing(nil, slog.New(slog.DiscardHandler), cfg.APIHost, cfg.BaseURL))
	for _, c := range []struct {
		host string
		want int
	}{{"example.test", http.StatusOK}, {"ops.example.test", http.StatusNotFound}, {"api.example.test", http.StatusNotFound}} {
		for _, path := range []string{pages.APIGuidePath, pages.APIGuideTRPath} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Host = c.host
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Errorf("GET %s on %s: %d, want %d", path, c.host, rec.Code, c.want)
			}
		}
	}
}

// between returns s from the first from to the next to after it, or "".
func between(s, from, to string) string {
	i := strings.Index(s, from)
	if i < 0 {
		return ""
	}
	j := strings.Index(s[i:], to)
	if j < 0 {
		return ""
	}
	return s[i : i+j]
}
