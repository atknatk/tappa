package operator_test

// ophome_test.go -- M13-06: the console's front page in the redesign. docs/plan/m13-operator-redesign.md,
// card M13-06; docs/design/operator-redesign README §6.1 "Home hub", screens.md §3.

import (
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// homeCard is the console's one card, its blocks spaced by the card (README §6 "Card": gap-3.5 in
// .op-card's rule, no mt-* in the bodies).
const homeCard = `<section class="op-card">`

// homeRow is each of the four list items (README §6.1 "Home hub"): on the phone the link above its
// gloss, from the tablet the two side by side in a 128 px and a free column, a dashed rule under
// each; the links' 17 px is the item's, not the anchor's.
const homeRow = `<li class="flex flex-col items-start border-b border-dashed border-line pt-0.5 pb-3 text-[17px] ` +
	`md:grid md:grid-cols-[128px_minmax(0,1fr)] md:items-baseline md:justify-items-start md:gap-x-4 md:py-1">`

// TestHomeScreen_IsOneCardOfFourLinksAndReadsNothing (M13-06; screens.md §3, README §6.1 "Home
// hub"): screens()'s "home" render is the frame's column holding exactly one card (homeCard) and
// nothing else; inside it no element carries a top margin (the card spaces its blocks); the card's
// words, tags dropped, are exactly screens.md §3's in order -- the label, the h1, the four links each
// followed by its gloss, the session sentence -- so no count, no tenant, no name and no address;
// the label is the whole tag <p class="op-label">Operator console</p> and the h1 the whole tag at
// the board's 28 px, line height 1.1 and -0.03em, the page's only h1; the list holds four items, each
// the whole opening tag homeRow followed by exactly one anchor and its gloss span, and the column's
// anchors are exactly the four whole tags in the order Tenants, Legal texts, Holdings, Audit log;
// the page has one form and one button (the bar's sign-out), and the column no form, input, svg,
// img, docket, chip or description list, and nothing hidden from sight or from assistive
// technology (no hidden, sr-only or aria-hidden). Through the surface, a live session's GET
// /operator answers 200 with that very column and costs the store exactly one call, the session's
// TouchOperatorSession: Home makes no read.
//
// PART II -- red on: the column's or the card's class changed, a second card or a block beside it,
// a top margin back in a body, a word changed, added or dropped, the label or the h1's tag changed,
// a row's class changed or a row added, dropped or given a second link, a link's tag or order
// changed, a form, a field, a drawing, a chip, a list of facts or a hidden element added, a store
// call added to the console. PART III -- the nav's five icons are TestEnrollQR_OnlyOnTheFirstLoad's,
// the links' routes TestOperatorScreens_EveryActionAndLinkIsAMountedRoute's; the look at 320 / 390 /
// 834 / 1200 / 1440 px is the screenshot round's.
func TestHomeScreen_IsOneCardOfFourLinksAndReadsNothing(t *testing.T) {
	body, ok := screens(t)["home"]
	if !ok {
		t.Fatal(`PREMISE: screens() has no "home"`)
	}
	column := columnOf(t, "home", body)
	open := regexp.MustCompile(`^<main [^>]*>`).FindString(column)
	if open == "" || !strings.Contains(open, `class="op-main `) {
		t.Fatalf("PREMISE: the console's column opens with %q", open)
	}
	inner := strings.TrimSpace(column[len(open) : len(column)-len("</main>")])
	if !strings.HasPrefix(inner, homeCard) || !strings.HasSuffix(inner, "</section>") || strings.Count(inner, "<section") != 1 {
		t.Fatalf("the column does not hold exactly the one card %q", homeCard)
	}
	for _, m := range classAttr.FindAllStringSubmatch(inner, -1) {
		for _, c := range strings.Fields(m[1]) {
			if topMargin.MatchString(c) {
				t.Errorf("%q in the card carries a top margin; the card spaces its blocks", m[1])
			}
		}
	}
	const words = "Operator console You are signed in " +
		"Tenants — find a business, and see what it has live " +
		"Legal texts — Taptime's privacy policy, terms, company details and cookie notice " +
		"Holdings — group businesses that work in one timezone, and see who is in each " +
		"Audit log — every sign-in, read and publication on this surface " +
		"A session ends after 30 minutes without use, and after 8 hours in any case. Sign out when you are done."
	if got := spaced(html.UnescapeString(noticeTag.ReplaceAllString(inner, " "))); got != words {
		t.Errorf("the card says %q, want %q", got, words)
	}
	const label = `<p class="op-label">Operator console</p>`
	const h1 = `<h1 class="font-display text-[28px] font-bold leading-[1.1] tracking-[-0.03em]">You are signed in</h1>`
	if !strings.Contains(inner, label) || strings.Count(body, "<h1") != 1 || !strings.Contains(inner, h1) {
		t.Errorf("the card's label is not %s, or the page's one h1 is not %s in the card", label, h1)
	}
	links := []string{
		`<a href="/operator/tenants" class="op-link">Tenants</a>`,
		`<a href="/operator/legal" class="op-link">Legal texts</a>`,
		`<a href="/operator/holdings" class="op-link">Holdings</a>`,
		`<a href="/operator/audit" class="op-link">Audit log</a>`,
	}
	if got := anchorTag.FindAllString(column, -1); strings.Join(got, "|") != strings.Join(links, "|") {
		t.Errorf("the column's links are %q, want %q", got, links)
	}
	row := regexp.MustCompile(`(?s)<li[ >].*?</li>`)
	rows := row.FindAllString(inner, -1)
	if len(rows) != len(links) || strings.Count(inner, "<li") != len(links) {
		t.Fatalf("the card holds %d list item(s), want %d", strings.Count(inner, "<li"), len(links))
	}
	shape := regexp.MustCompile(`^<li [^>]*>\s*(<a [^>]*>[^<]*</a>)\s*<span class="text-sm leading-\[1\.45\] text-ink/70">— [^<]+</span>\s*</li>$`)
	for i, r := range rows {
		m := shape.FindStringSubmatch(r)
		if !strings.HasPrefix(r, homeRow) || m == nil || m[1] != links[i] {
			t.Errorf("item %d is not %s holding %s and its gloss: %s", i+1, homeRow, links[i], r)
		}
	}
	if strings.Count(body, "<form") != 1 || !strings.Contains(body, `<form method="post" action="/operator/logout">`) ||
		strings.Count(body, "<button") != 1 {
		t.Errorf("the page has %d form(s) and %d button(s), want the sign-out's one of each", strings.Count(body, "<form"), strings.Count(body, "<button"))
	}
	for _, banned := range []string{"<form", "<input", "<svg", "<img", "<dl", "docket", "tally", "hidden", "sr-only", "aria-hidden"} {
		if strings.Contains(column, banned) {
			t.Errorf("the column carries %q", banned)
		}
	}

	g := newRig(t)
	live := g.signIn(g.active())
	touches, calls := g.store.count("TouchOperatorSession"), g.store.total()
	w := g.get("/operator", live)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), column) {
		t.Fatalf("GET /operator = %d, and its page is not the console's column", w.Code)
	}
	if g.store.count("TouchOperatorSession") != touches+1 || g.store.total() != calls+1 {
		t.Errorf("GET /operator made %d store call(s), TouchOperatorSession %d; want the session's one and no read",
			g.store.total()-calls, g.store.count("TouchOperatorSession")-touches)
	}
}
