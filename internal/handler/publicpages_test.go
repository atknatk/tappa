package handler

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/web/templates/pages"
)

// THE PUBLIC SITE'S OWN PAGES (LP-06, README §7): the infrastructure every page beyond
// the landing joins. Three things are held here, once for all of them:
//
//  1. THE TABLE IS THE ROUTES. pages.PublicPages lists the pages; Marketing.Mount
//     registers each under a named path. Walking the mounted routes, a page mounted
//     without a row or listed without a route is red (TestPublicPages_TheTableIsTheMountedRoutes).
//  2. EVERY VALUE IS RENDERED ON ITS OWN PAGE. publicPagesCopy names, per URL, the Lines
//     and Questions that page renders; each must carry a pin and reach the visitor on
//     that URL (TestPublicPages_EveryValueIsRenderedOnItsOwnPageAndRestsOnAFact).
//  3. THE CLOSURE COUNTS EVERY PAGE. claimedFacts reads publicPagesCopy, so a fact only
//     one page claims is claimed (TestPublicPages_TheClosureCountsEveryPagesSentences).
//
// A PAGE CARD (LP-07 … LP-10) JOINS BY: a row of pages.PublicPages and a named route in
// Mount; a view file declaring its values (and any new Fact) beside landingview.go; an
// entry in publicPagesCopy and in sitePageTemplates; a derivation table appended to
// pageFactDerivations. Every scanner over marketingURLs() then covers it.

// publicCopy is the claim-carrying values one public page renders.
type publicCopy struct {
	lines     map[string]pages.Line
	questions []pages.Question
}

// publicPagesCopy is every public page's copy, by URL. The landing's own blocks that are
// not Lines or Questions (the steps, the comparison, the evidence, the audiences) keep
// their dedicated tests; what is here is what every page shares the shape of.
func publicPagesCopy() map[string]publicCopy {
	landing := landingLines()
	for i, l := range pages.LandingPricingIncludes {
		landing[fmt.Sprintf("LandingPricingIncludes[%d]", i)] = l
	}
	return map[string]publicCopy{
		"/":                {lines: landing, questions: pages.LandingFAQ},
		pages.PricingPath:  {lines: pricingLines(), questions: pages.PricingFAQ},
		pages.SecurityPath: {lines: securityLines()},
	}
}

// renderedPublicText is the page at url as a visitor perceives it, through the real
// router and the package's screenText.
func renderedPublicText(t *testing.T, url string) string {
	t.Helper()
	return screenText(t, mustFetchMarketing(t, marketingRouter(t), url))
}

// publicCopyProblems says which of c's values text (one page's screen text) does not
// carry, and which rest on no fact.
func publicCopyProblems(text string, c publicCopy) []string {
	var out []string
	names := make([]string, 0, len(c.lines))
	for name := range c.lines {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		l := c.lines[name]
		if len(l.Facts) == 0 {
			out = append(out, name+" rests on no product fact")
		}
		if l.Text == "" || !strings.Contains(text, asRendered(l.Text)) {
			out = append(out, fmt.Sprintf("%s is declared and NOT rendered on this page:\n    %q", name, l.Text))
		}
	}
	for i, q := range c.questions {
		if len(q.Facts) == 0 {
			out = append(out, fmt.Sprintf("question %d (%q) rests on no product fact", i+1, q.Q))
		}
		if !strings.Contains(text, asRendered(q.Q)) || !strings.Contains(text, asRendered(q.A)) {
			out = append(out, fmt.Sprintf("question %d (%q) or its answer is declared and NOT rendered on this page", i+1, q.Q))
		}
	}
	return out
}

// TestPublicPages_EveryValueIsRenderedOnItsOwnPageAndRestsOnAFact: every page of the
// public site has its copy named here, and every value in it carries a pin and is
// rendered on that page's own URL. CONTROL: the pricing page's copy read against the
// landing is red -- the check is per page, not "somewhere on the site".
func TestPublicPages_EveryValueIsRenderedOnItsOwnPageAndRestsOnAFact(t *testing.T) {
	t.Parallel()
	copies := publicPagesCopy()
	for _, url := range siteURLs() {
		c, ok := copies[url]
		if !ok {
			t.Errorf("%s has no entry in publicPagesCopy, so nothing checks that its sentences rest on a fact", url)
			continue
		}
		if len(c.lines)+len(c.questions) < 3 {
			t.Errorf("%s names %d value(s); this is not the page's copy", url, len(c.lines)+len(c.questions))
		}
		for _, p := range publicCopyProblems(renderedPublicText(t, url), c) {
			t.Errorf("%s: %s", url, p)
		}
	}
	for url := range copies {
		if !slices.Contains(siteURLs(), url) {
			t.Errorf("publicPagesCopy names %s, which is not a page of the public site", url)
		}
	}
	if len(publicCopyProblems(renderedPublicText(t, "/"), copies[pages.PricingPath])) == 0 {
		t.Error("CONTROL: the pricing page's copy passed against the landing page's text")
	}
}

// TestPublicPages_TheClosureCountsEveryPagesSentences: the fact closure reads every
// page's copy. CONTROL: leave the pricing page's copy out and the facts only it claims
// fall unclaimed -- the closure test would be red.
func TestPublicPages_TheClosureCountsEveryPagesSentences(t *testing.T) {
	t.Parallel()
	declared := declaredFacts(t)
	if got := unclaimedFacts(declared, claimedFacts()); len(got) != 0 {
		t.Fatalf("PREMISE: facts no page claims: %v", got)
	}
	without := unclaimedFacts(declared, claimedFactsExcept(pages.PricingPath))
	if !slices.Contains(without, pages.FactBillingFreezeIsFinal) {
		t.Errorf("CONTROL: without the pricing page's copy the closure still counts %q as claimed "+
			"(unclaimed: %v), so it is not reading the pricing page", pages.FactBillingFreezeIsFinal, without)
	}
	if slices.Contains(without, pages.FactPricePerEmployee) {
		t.Error("CONTROL: without the pricing page's copy the price fact fell unclaimed, but the landing claims it")
	}
}

// mountedMarketingRoutes is every route Marketing.Mount registers for method, sorted.
func mountedMarketingRoutes(t *testing.T, method string) []string {
	t.Helper()
	r := chi.NewRouter()
	NewMarketing(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), "").Mount(r)
	var out []string
	err := chi.Walk(r, func(m, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if m == method {
			out = append(out, route)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the marketing routes: %v", err)
	}
	sort.Strings(out)
	return out
}

// publicTableProblems says how the mounted GET routes depart from the tables that list
// them: the landing, the legal documents, the guide's two pages and the public pages.
// Package level so the control can hand it a table the routes were not mounted from.
func publicTableProblems(mounted []string, public []pages.PublicPage) []string {
	known := map[string]string{"/": "the landing", pages.APIGuidePath: "the guide", pages.APIGuideTRPath: "the guide"}
	for _, p := range pages.LegalPages {
		known[p.Path] = "pages.LegalPages"
	}
	listed := map[string]bool{}
	for _, p := range public {
		listed[p.Path] = true
	}
	var out []string
	for _, route := range mounted {
		if _, ok := known[route]; ok {
			continue
		}
		if !listed[route] {
			out = append(out, route+" is mounted by Marketing and is not a row of pages.PublicPages, so no "+
				"public-surface scanner reads it")
		}
	}
	for _, p := range public {
		if !slices.Contains(mounted, p.Path) {
			out = append(out, p.Path+" is a row of pages.PublicPages and Marketing mounts no GET for it")
		}
		if p.Title == "" {
			out = append(out, p.Path+" has no title")
		}
	}
	return out
}

// TestPublicPages_TheTableIsTheMountedRoutes: pages.PublicPages and Marketing.Mount
// agree in both directions, every row answers HEAD as well (Mount's rule for this
// surface), and the handler's path constant is the page's. CONTROL: a table without
// the pricing row, while its route stays mounted, is red; a row with no route is red.
func TestPublicPages_TheTableIsTheMountedRoutes(t *testing.T) {
	t.Parallel()
	mounted := mountedMarketingRoutes(t, http.MethodGet)
	if len(mounted) < len(pages.LegalPages)+3 {
		t.Fatalf("PREMISE: walked %d GET route(s) (%v); Marketing mounts more", len(mounted), mounted)
	}
	for _, p := range publicTableProblems(mounted, pages.PublicPages) {
		t.Error(p)
	}
	heads := mountedMarketingRoutes(t, http.MethodHead)
	for _, p := range pages.PublicPages {
		if !slices.Contains(heads, p.Path) {
			t.Errorf("%s answers GET and not HEAD; every route on this surface answers both", p.Path)
		}
	}
	if pricingPath != pages.PricingPath {
		t.Errorf("Mount registers %q and the page's path is %q", pricingPath, pages.PricingPath)
	}
	if securityPath != pages.SecurityPath {
		t.Errorf("Mount registers %q and the page's path is %q", securityPath, pages.SecurityPath)
	}
	if pages.PublicPageTitle(pages.SecurityPath) == "Taptime" {
		t.Error("the security page has no row title")
	}
	if pages.PublicPageTitle(pages.PricingPath) == "Taptime" {
		t.Error("the pricing page has no row title")
	}

	// CONTROL: the table loses the pricing row while Mount still registers it.
	var without []pages.PublicPage
	for _, p := range pages.PublicPages {
		if p.Path != pages.PricingPath {
			without = append(without, p)
		}
	}
	if len(publicTableProblems(mounted, without)) == 0 {
		t.Error("CONTROL: a table without /pricing passed while /pricing is mounted")
	}
	// CONTROL: a row nobody mounted.
	ahead := append(slices.Clone(pages.PublicPages), pages.PublicPage{Path: "/product", Title: "Product — Taptime"})
	if len(publicTableProblems(mounted, ahead)) == 0 {
		t.Error("CONTROL: a row with no route passed")
	}
}
