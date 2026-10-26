package operator_test

// optenants_test.go -- M13-07: the tenant list and its search in the redesign. docs/plan/m13-operator-redesign.md,
// card M13-07; docs/design/operator-redesign README §6.1 "Tenants row", screens.md §5.

import (
	"bytes"
	"context"
	"html"
	"maps"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// The tenant list's card and the pieces of it screens.md §5 names, each a whole tag: the card
// spaces its blocks (README §6 "Card": gap-3.5 on the markup, no mt-* in the bodies); the form's
// four children are in the order a phone reads them, the button placed beside the box and the
// help under both from the tablet (the board's search row).
const (
	tenantsCard   = `<section class="op-card flex flex-col gap-3.5">`
	tenantsLabel  = `<p class="op-label">Tenants</p>`
	tenantsH1     = `<h1 class="font-display text-[28px] font-bold leading-[1.1] tracking-[-0.03em]">Find a tenant</h1>`
	tenantsForm   = `<form method="post" action="/operator/tenants" role="search" class="flex flex-col gap-2 md:grid md:grid-cols-[minmax(0,1fr)_auto] md:gap-x-2.5">`
	tenantsSearch = `<label for="tenant-search" class="op-label md:col-span-2">Search</label>`
	tenantsHelp   = `<p class="text-xs leading-[1.45] text-ink/70 md:col-span-2 md:row-start-3">Part of a business name, a panel ` +
		`account's whole email address, or a tenant id. Leave it empty to list every tenant.</p>`
	tenantsButton = `<button type="submit" class="btn btn--primary md:col-start-2 md:row-start-2">Search</button>`
	// The searched line under a dashed rule: the term mono and bold, breaking anywhere (an address
	// has no space to break at).
	tenantsMatching = `<p class="border-t border-dashed border-line pt-3.5 text-sm [overflow-wrap:anywhere] [&amp;&gt;bdi]:font-bold">` +
		`Tenants matching <bdi class="font-mono">`
	tenantsLinks   = `<p class="flex flex-col items-start text-sm md:flex-row md:flex-wrap md:gap-x-6">`
	tenantsDocket  = `<section class="docket" aria-labelledby="tenant-list">`
	tenantsPager   = `<nav class="flex flex-wrap items-center gap-x-6" aria-label="Pages of the list">`
	tenantsEmpty   = `<p class="mt-4 pb-1 font-display">`
	tenantsShowAll = `<a href="/operator/tenants" class="op-link">Show every tenant</a>`
	tenantsBack    = `<a href="/operator" class="op-link">Back to the console</a>`
)

// tenantsPageForm is one pager button's whole form: a POST to the list with the term and the page
// as hidden fields, the button the text link (the tenant list's pager is not in the polished
// pager list, README §6 "Pager").
func tenantsPageForm(term string, page int, label string) string {
	return `<form method="post" action="/operator/tenants"><input type="hidden" name="q" value="` + html.EscapeString(term) +
		`"> <input type="hidden" name="page" value="` + strconv.Itoa(page) + `"> <button type="submit" class="op-link">` +
		label + `</button></form>`
}

// TestTenantsScreen_IsTheSearchCardTheDocketAndThePager (M13-07; screens.md §5, README §6.1
// "Tenants row"): each tenant-list state -- screens()'s six "tenants" renders and a searched
// render with no match -- is the frame's column holding the card (tenantsCard), the docket
// (tenantsDocket) and, only when there is a page before or after, the pager (tenantsPager), in
// that order and nothing else. The card: no element in it carries a top margin; its words, tags
// dropped, are exactly screens.md §5's in order (the label, the h1, the search label, the help,
// the button, on a searched render "Tenants matching <term>." and "Show every tenant", then "Back
// to the console"); the label, the h1 (the page's only h1), the form's opening tag, the search
// label, the help, the button, the matching line's opening and the links' row are the whole tags
// above, the form's children are exactly label, box, help and button in that order; the card's
// one input has exactly the attribute set id, type, name, value, maxlength, autocomplete,
// spellcheck -- the value the term as searched, nothing more (no required, no autofocus, no
// pattern, no aria wiring the code does not write); its anchors are exactly Show every tenant
// (searched only) and Back to the console. The docket: the heading names the page; an empty page
// says exactly the one sentence its state calls for (page > 1, searched, none) in tenantsEmpty; a
// page of rows is an <ol class="mt-3"> of <li class="op-row"> each holding exactly the name link,
// "Since <time>", the plan, the chip on a suspended tenant only, the id -- and on "tenants, a
// suspended row" exactly one chip, on the suspended row. The pager: each button the whole
// tenantsPageForm with the term and the page before or after. The term reaches the page in
// exactly the places screens.md §5 designs -- the box's value, the matching line and each pager
// form's hidden field -- and nowhere else (not the title, not a link). Nowhere in the column:
// hidden content (a hidden attribute or class, sr-only, aria-hidden; the pager's hidden inputs
// are counted, two per form), a drawing, an image, a select, a textarea, a description list, or a
// count, filter, sort or recent-searches control.
//
// PART II -- red on: a block added to or dropped from the column, a top margin back in the card,
// a word changed, added or dropped, the label, the h1, the form's tag, a field's tag or order, the
// button's tag, the matching line's or the links' tag changed, an attribute added to, dropped
// from or changed on the box (maxlength, autocomplete, spellcheck, type, name, value), a hidden
// field dropped or its value changed, a pager button's class changed, an empty sentence changed,
// a row's shape changed (a class added to the plan's or the id's span), a second chip, the term
// echoed somewhere else, a hidden element, a sort or count. PART III -- the row's layout per
// width is input.css's scoped rules, measured by the screenshot round at 320 / 390 / 834 / 1200
// / 1440 px, not here; the routes are TestOperatorScreens_EveryActionAndLinkIsAMountedRoute's,
// the escaping TestTenantScreens_EscapeWhatATenantAndAnOperatorTyped's, the chip's place
// between plan and id through the handler TestTenantList_ASuspendedTenantCarriesTheChip's.
func TestTenantsScreen_IsTheSearchCardTheDocketAndThePager(t *testing.T) {
	all := screens(t)
	var noMatch bytes.Buffer
	if err := operatorpages.Tenants(operatorpages.TenantsView{Search: "FAKE nothing", Page: 1}).Render(context.Background(), &noMatch); err != nil {
		t.Fatal(err)
	}
	all["tenants, no match"] = noMatch.String()
	type state struct {
		term       string
		page       int
		empty      string // the empty sentence, or "" for a page of rows
		rows, chip int
		prev, next bool
	}
	states := map[string]state{
		"tenants":                  {page: 1, empty: "There are no tenants yet."},
		"tenants, a page":          {page: 1, rows: 2, next: true},
		"tenants, a suspended row": {page: 1, rows: 2, chip: 1},
		"tenants, searched":        {term: "kebab", page: 1, rows: 1},
		"tenants, a later page":    {term: "kebab", page: 2, rows: 1, prev: true, next: true},
		"tenants, past the end":    {page: 3, empty: "This page is empty: the list ended on the page before.", prev: true},
		"tenants, no match":        {term: "FAKE nothing", page: 1, empty: "No tenant matches that search."},
	}
	rowShape := regexp.MustCompile(`^<li class="op-row"><a href="/operator/tenants/[0-9a-f-]{36}" class="op-link">` +
		`(?:<bdi>[^<]+</bdi>|<span class="italic">Unnamed tenant</span>)</a> <span>Since \d{4}-\d\d-\d\d \d\d:\d\d UTC</span> ` +
		`<span>(?:founding|standard)</span> (` + regexp.QuoteMeta(chipSuspended) + ` )?` +
		`<span class="break-all text-xs text-ink/70">[0-9a-f-]{36}</span></li>$`)
	formChildren := regexp.MustCompile(`<(label|input|p|button)[ >]`)
	bdiTag := regexp.MustCompile(`</?bdi[^>]*>`) // inline: the term reads into its full stop
	for name, s := range states {
		body, ok := all[name]
		if !ok {
			t.Fatalf("PREMISE: screens() has no %q", name)
		}
		column := columnOf(t, name, body)
		open := regexp.MustCompile(`^<main [^>]*>`).FindString(column)
		if !strings.Contains(open, `class="op-main `) {
			t.Fatalf("PREMISE: %s's column opens with %q", name, open)
		}
		inner := strings.TrimSpace(column[len(open) : len(column)-len("</main>")])

		// The column: the card, the docket, the pager when there is a page to go to.
		docketAt, pagerAt := strings.Index(inner, tenantsDocket), strings.Index(inner, tenantsPager)
		if !strings.HasPrefix(inner, tenantsCard) || docketAt < 0 || strings.Count(inner, "<section") != 2 {
			t.Fatalf("%s: the column is not the card %s then the docket %s", name, tenantsCard, tenantsDocket)
		}
		card := inner[:strings.Index(inner, "</section>")+len("</section>")]
		if strings.TrimSpace(inner[len(card):docketAt]) != "" {
			t.Errorf("%s: something stands between the card and the docket", name)
		}
		docketEnd := docketAt + strings.Index(inner[docketAt:], "</section>") + len("</section>")
		rest := strings.TrimSpace(inner[docketEnd:])
		if s.prev || s.next {
			if pagerAt != docketEnd+strings.Index(inner[docketEnd:], "<") || !strings.HasSuffix(rest, "</nav>") || strings.Count(rest, "<nav") != 1 {
				t.Errorf("%s: the pager %s is not the column's last block, after the docket", name, tenantsPager)
			}
		} else if rest != "" {
			t.Errorf("%s: the column carries %q after the docket, want nothing", name, rest)
		}

		// The card.
		for _, m := range classAttr.FindAllStringSubmatch(card, -1) {
			for _, c := range strings.Fields(m[1]) {
				if topMargin.MatchString(c) {
					t.Errorf("%s: %q in the card carries a top margin; the card spaces its blocks", name, m[1])
				}
			}
		}
		words := "Tenants Find a tenant Search Part of a business name, a panel account's whole email address, or a " +
			"tenant id. Leave it empty to list every tenant. Search"
		links := []string{tenantsBack}
		if s.term != "" {
			words += " Tenants matching " + s.term + ". Show every tenant"
			links = []string{tenantsShowAll, tenantsBack}
		}
		words += " Back to the console"
		if got := spaced(html.UnescapeString(noticeTag.ReplaceAllString(bdiTag.ReplaceAllString(card, ""), " "))); got != words {
			t.Errorf("%s: the card says %q, want %q", name, got, words)
		}
		for _, whole := range []string{tenantsLabel, tenantsH1, tenantsForm, tenantsSearch, tenantsHelp, tenantsButton, tenantsLinks} {
			if strings.Count(card, whole) != 1 {
				t.Errorf("%s: the card does not carry %s once", name, whole)
			}
		}
		if strings.Count(body, "<h1") != 1 {
			t.Errorf("%s: %d h1, want the card's one", name, strings.Count(body, "<h1"))
		}
		if matching := tenantsMatching + html.EscapeString(s.term) + `</bdi>.</p>`; (s.term != "") != strings.Contains(card, matching) ||
			strings.Count(card, "Tenants matching") > 1 {
			t.Errorf("%s: the matching line is not %s exactly when the list was searched", name, matching)
		}
		form := card[strings.Index(card, tenantsForm):]
		form = form[:strings.Index(form, "</form>")]
		var kids []string
		for _, m := range formChildren.FindAllStringSubmatch(form[len(tenantsForm):], -1) {
			kids = append(kids, m[1])
		}
		if strings.Join(kids, " ") != "label input p button" {
			t.Errorf("%s: the search form's children are %v, want label, input, p, button", name, kids)
		}
		inputs := inputTag.FindAllString(card, -1)
		if len(inputs) != 1 {
			t.Fatalf("%s: the card has %d input(s), want the search box", name, len(inputs))
		}
		want := map[string]string{"id": "tenant-search", "type": "search", "name": "q", "value": s.term, "maxlength": "254",
			"autocomplete": "off", "spellcheck": "false"}
		if got := attributesOf(inputs[0]); !maps.Equal(got, want) {
			t.Errorf("%s: the search box's attributes are %v, want exactly %v", name, got, want)
		}
		if !strings.Contains(inputs[0], ` class="op-input"`) {
			t.Errorf("%s: the search box is not an op-input: %s", name, inputs[0])
		}
		if got := anchorTag.FindAllString(card, -1); strings.Join(got, "|") != strings.Join(links, "|") {
			t.Errorf("%s: the card's links are %q, want %q", name, got, links)
		}
		if strings.Count(card, "<form") != 1 || strings.Count(card, "<button") != 1 {
			t.Errorf("%s: the card has %d form(s) and %d button(s), want the search's one", name,
				strings.Count(card, "<form"), strings.Count(card, "<button"))
		}

		// The docket.
		docket := inner[docketAt:docketEnd]
		heading := `<h2 id="tenant-list" class="docket-label">Newest first · page <span class="font-mono">` + strconv.Itoa(s.page) + `</span></h2>`
		if !strings.HasPrefix(docket, tenantsDocket+heading) {
			t.Errorf("%s: the docket does not open with %s", name, heading)
		}
		rows := regexp.MustCompile(`<li[ >].*?</li>`).FindAllString(docket, -1)
		if s.empty != "" {
			if docket != tenantsDocket+heading+tenantsEmpty+s.empty+"</p></section>" {
				t.Errorf("%s: the empty docket is %s, want its heading and %s%s</p>", name, docket, tenantsEmpty, s.empty)
			}
		} else {
			if !strings.HasPrefix(docket[len(tenantsDocket+heading):], `<ol class="mt-3">`) || len(rows) != s.rows ||
				strings.Count(docket, "<li") != s.rows || strings.Count(docket, tenantsEmpty) != 0 {
				t.Errorf("%s: the docket is not an ordered list of %d row(s): %s", name, s.rows, docket)
			}
			chips := 0
			for _, r := range rows {
				m := rowShape.FindStringSubmatch(r)
				if m == nil {
					t.Errorf("%s: a row is not name link, Since, plan, [chip], id: %s", name, r)
					continue
				}
				if m[1] != "" {
					chips++
				}
			}
			if chips != s.chip || strings.Count(body, "tally") != 2*s.chip {
				t.Errorf("%s: %d chip(s) on the rows and %d tally word(s) on the page, want %d and %d", name, chips,
					strings.Count(body, "tally"), s.chip, 2*s.chip)
			}
		}

		// The pager.
		var forms []string
		if s.prev {
			forms = append(forms, tenantsPageForm(s.term, s.page-1, "Previous page"))
		}
		if s.next {
			forms = append(forms, tenantsPageForm(s.term, s.page+1, "Next page"))
		}
		if len(forms) > 0 && rest != tenantsPager+strings.Join(forms, "")+"</nav>" {
			t.Errorf("%s: the pager is %s, want %s", name, rest, tenantsPager+strings.Join(forms, "")+"</nav>")
		}

		// The term: the box, the matching line, each pager form -- and nowhere else.
		if s.term != "" {
			if got, want := strings.Count(body, html.EscapeString(s.term)), 2+len(forms); got != want {
				t.Errorf("%s: the term is on the page %d time(s), want %d (box, matching line, %d pager form(s))", name, got, want, len(forms))
			}
		}

		// Nothing hidden, nothing the list does not have.
		if n := strings.Count(column, `type="hidden"`); n != 2*len(forms) {
			t.Errorf("%s: %d hidden field(s), want the pager's two per form", name, n)
		}
		if strings.Contains(strings.ReplaceAll(column, `type="hidden"`, ""), "hidden") || strings.Contains(column, "sr-only") {
			t.Errorf("%s: the column hides something (a hidden attribute or class, sr-only, aria-hidden)", name)
		}
		for _, banned := range []string{"<svg", "<img", "<select", "<textarea", "<dl", "<table"} {
			if strings.Contains(column, banned) {
				t.Errorf("%s: the column carries %q", name, banned)
			}
		}
		if m := regexp.MustCompile(`(?i)\b(?:sort|sorted|filter|recent|reason|results?|total|count|counts|showing)\b`).FindString(
			html.UnescapeString(noticeTag.ReplaceAllString(column, " "))); m != "" {
			t.Errorf("%s: the list says %q (screens.md §5 Not shown: counts, filter, sort, recent searches, a reason)", name, m)
		}
	}
}
