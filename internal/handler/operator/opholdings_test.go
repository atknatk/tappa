package operator_test

// opholdings_test.go -- M13-13: the holdings screen in the redesign. docs/plan/m13-operator-redesign.md,
// card M13-13; docs/design/operator-redesign README §6 "Pager", §6.1 "Holdings", screens.md §11.

import (
	"context"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// The holdings screen's two cards, its docket and its pager, and the pieces of them screens.md §11
// names, each a whole tag. The cards space their blocks (README §6 "Card": gap-3.5 in .op-card's rule,
// no mt-* in the bodies); the h1 is the board's 26 / 28 / 32 px (orchestrator decision (e)); the
// create card's heading is the 19 px op-heading, its id kept; the field and its button share a
// line from the tablet and the help sits under them, the document keeping the code's order (the
// help before the button); each holding's add form is the boards' dashed box; the empty sentences
// are the boards' own; the pager's links are op-pager, each half the row on the phone.
const (
	holdingsCard        = `<section class="op-card">`
	holdingsLabel       = `<p class="op-label">Holdings</p>`
	holdingsH1          = `<h1 class="font-display text-[26px] font-bold leading-[1.1] tracking-[-0.03em] md:text-[28px] desk:text-[32px]">Holdings</h1>`
	holdingsIntro       = `<p class="text-[15px] leading-[1.55]">`
	holdingsCreateCard  = `<section class="op-card" aria-labelledby="holding-create">`
	holdingsCreateH2    = `<h2 id="holding-create" class="op-heading">Create a holding</h2>`
	holdingsCreateForm  = `<form method="post" action="/operator/holdings" class="flex flex-wrap gap-x-2.5 gap-y-1.5">`
	holdingsNameField   = `<input id="holding-name" type="text" name="name" maxlength="120" required autocomplete="off" spellcheck="false" class="op-input min-w-0 max-w-[520px] flex-[1_1_240px]">`
	holdingsAddForm     = `" class="flex flex-wrap gap-x-2.5 gap-y-1.5 rounded-xl border border-dashed border-line bg-ink/[.03] p-3.5 md:px-4">`
	holdingsAddField    = `" type="text" name="tenant_id" maxlength="64" required autocomplete="off" spellcheck="false" class="op-input min-w-0 max-w-[440px] flex-[1_1_240px]">`
	holdingsEmpty       = `<p class="pt-[22px] pb-2 font-display text-[15px] leading-normal">`
	holdingsSay         = `<p class="font-display text-sm leading-normal">`
	holdingsFact        = `<span class="font-display text-sm text-ink/70">`
	holdingsPagerNav    = `<nav class="flex flex-wrap gap-2.5 md:gap-3 max-md:*:flex-1" aria-label="Pages of the list">`
	holdingsIntroWords  = `A holding groups businesses that work in one timezone; while a business is in one, and for 60 seconds after it leaves, its owner cannot change its timezone. Only this screen changes who is in a holding.`
	holdingsCreateWords = `One line of at most 120 characters. The new holding has no business in it yet.`
	holdingsAddWords    = `The business's id, from its overview. It must work in the timezone of every member.`
)

// holdingsWantColumn is the whole content column of v, every holding and membership written out:
// the intro card, the sentence above the list in the operator's notice (when v has one), the
// create card, the docket -- its pinned heading, the state's empty sentence or the holdings, each
// with its name, creation time and id, its page-cut sentences, its memberships (the name link and
// the chip, the timezone and the API code, the times, the id, Remove on a live one) and its add
// form -- and the pager with the ways v offers.
func holdingsWantColumn(v operatorpages.HoldingsView) string {
	e := html.EscapeString
	s := holdingsCard + holdingsLabel + holdingsH1 + holdingsIntro + holdingsIntroWords +
		`</p><p class="text-sm"><a href="/operator" class="op-link">Back to the console</a></p></section>`
	switch v.NoticeTone {
	case operatorpages.HoldingNoticeOK, operatorpages.HoldingNoticeWarn:
		tone := "ok"
		if v.NoticeTone == operatorpages.HoldingNoticeWarn {
			tone = "warn"
		}
		s += `<section role="status" class="op-notice op-notice--` + tone + `"><p class="font-display font-bold tracking-tight">` +
			e(v.NoticeHeading) + `</p><div class="mt-1 text-sm text-ink/85"><p>` + e(v.Notice) + `</p>`
		if tone == "warn" && len(v.NoticeRules) > 0 {
			s += `<ul class="mt-1 list-disc pl-5">`
			for _, r := range v.NoticeRules {
				s += `<li>` + e(r) + `</li>`
			}
			s += `</ul>`
		}
		s += `</div></section>`
	}
	s += holdingsCreateCard + holdingsCreateH2 + holdingsCreateForm + `<label for="holding-name" class="op-label basis-full">Name</label>` +
		holdingsNameField + `<p class="order-last basis-full text-xs text-ink/70">` + holdingsCreateWords + `</p>` +
		`<button type="submit" class="btn btn--primary max-md:basis-full">Create</button></form></section>`
	s += `<section class="docket" aria-labelledby="holding-list"><h2 id="holding-list" class="docket-label">Newest first · page <span class="font-mono">` +
		strconv.Itoa(v.Page) + `</span></h2>`
	switch {
	case len(v.Holdings) == 0 && v.Page > 1:
		s += holdingsEmpty + `This page is empty: the list ended on the page before.</p>`
	case len(v.Holdings) == 0:
		s += holdingsEmpty + `There are no holdings yet.</p>`
	default:
		s += `<ol>`
		for _, h := range v.Holdings {
			name := `<bdi>` + e(h.Name) + `</bdi>`
			if h.Name == "" {
				name = `<span class="italic">Unnamed holding</span>`
			}
			s += `<li><p class="font-display text-[21px] font-bold leading-[1.2] tracking-[-0.02em]">` + name + `</p>` +
				`<p class="font-display text-sm text-ink/70">Created <span class="font-mono font-bold text-ink">` + e(h.CreatedAt) +
				`</span></p><p class="break-all text-xs text-ink/70">` + e(h.ID) + `</p>`
			if h.ContinuedFromBefore {
				s += holdingsSay + `Its first memberships are on the page before.</p>`
			}
			if len(h.Members) == 0 {
				s += holdingsSay + `No business has been in this holding.</p>`
			} else {
				s += `<ol>`
				for _, m := range h.Members {
					s += holdingsWantMember(m)
				}
				s += `</ol>`
			}
			if h.ContinuesAfter {
				s += holdingsSay + `More of its memberships are on the next page.</p>`
			}
			s += `<form method="post" action="` + e(h.MembersPath) + holdingsAddForm +
				`<label for="holding-add-` + e(h.ID) + `" class="op-label basis-full">Add a business</label>` +
				`<input id="holding-add-` + e(h.ID) + holdingsAddField +
				`<p class="order-last basis-full font-display text-xs text-ink/70">` + holdingsAddWords + `</p>` +
				`<button type="submit" class="btn btn--primary max-md:basis-full">Add</button></form></li>`
		}
		s += `</ol>`
	}
	s += `</section>`
	if v.Page > 1 || v.HasNext {
		s += holdingsPagerNav
		if v.Page > 1 {
			s += `<a href="/operator/holdings?page=` + strconv.Itoa(v.Page-1) + `" class="op-pager">Previous page</a>`
		}
		if v.HasNext {
			s += `<a href="/operator/holdings?page=` + strconv.Itoa(v.Page+1) + `" class="op-pager">Next page</a>`
		}
		s += `</nav>`
	}
	return s
}

// holdingsWantMember is one membership's row: <li class="op-row"> (pinned), the name link and the
// chip (holdings_test.go's chip markup), the timezone and the API code (their value spans pinned
// there), the times, the business's id and, on a live one, the Remove form (op8_test.go's prefix).
func holdingsWantMember(m operatorpages.HoldingMemberView) string {
	e := html.EscapeString
	name := `<bdi>` + e(m.Name) + `</bdi>`
	if m.Name == "" {
		name = `<span class="italic">Unnamed tenant</span>`
	}
	chip := `<span class="tally tally--active">Live</span>`
	if !m.Live {
		chip = `<span class="tally tally--deactivated">Ended</span>`
	}
	code := holdingsFact + `No API code</span>`
	if m.APICode != "" {
		code = holdingsFact + `API code <span class="font-mono">` + e(m.APICode) + `</span></span>`
	}
	s := `<li class="op-row"><div class="flex flex-wrap items-center gap-x-2.5 gap-y-1 text-[15px] leading-snug"><a href="` + e(m.TenantPath) +
		`" class="op-link">` + name + `</a>` + chip + `</div><div class="flex flex-col gap-px">` + holdingsFact + `Timezone <span class="font-mono">` +
		e(m.Timezone) + `</span></span>` + code + `</div><div class="flex flex-col gap-px">` + holdingsFact + `Joined <span class="font-mono">` +
		e(m.JoinedAt) + `</span></span>`
	if !m.Live {
		s += holdingsFact + `Left <span class="font-mono">` + e(m.LeftAt) + `</span></span>`
	}
	s += `</div><span class="break-all text-xs text-ink/70">` + e(m.TenantID) + `</span>`
	if m.Live {
		s += `<form method="post" action="` + e(m.RemovePath) + `"><button type="submit" class="btn btn--quiet">Remove</button></form>`
	}
	return s + `</li>`
}

// holdingsNotShown is screens.md §11's "Not shown" list as words: a rename or a deletion of a
// holding, a tenant picker or search, a confirmation step.
var holdingsNotShown = regexp.MustCompile(`(?i)\b(?:renam(?:e|ed|ing)|delet(?:e|ed|ing)|search(?:ed)?|pick(?:er)?|choose|select(?:ed)?|confirm(?:ation|ed)?|are you sure)\b`)

// holdingsHref is the column's links: the console, a membership's overview, a page of the list --
// never a per-holding page.
var holdingsHref = regexp.MustCompile(`^(?:/operator|/operator/tenants/[0-9a-f-]{36}|/operator/holdings\?page=[0-9]+)$`)

// holdingsScreenFindings is TestHoldingsScreen_IsTheCreateCardAndTheDocket's reading of one
// render of v: one finding per way it departs from what holdingsWantColumn says.
func holdingsScreenFindings(name, body string, v operatorpages.HoldingsView) []string {
	var f []string
	bad := func(format string, a ...any) { f = append(f, name+": "+fmt.Sprintf(format, a...)) }
	start, end := strings.Index(body, "<main"), strings.Index(body, "</main>")
	if start < 0 || end < start || strings.Count(body, "<main") != 1 {
		return []string{name + ": no single <main>"}
	}
	column := body[start : end+len("</main>")]
	open := regexp.MustCompile(`^<main [^>]*>`).FindString(column)
	if !strings.Contains(open, `class="op-main `) {
		return []string{name + ": the column opens with " + open}
	}
	got, want := tight(column[len(open):len(column)-len("</main>")]), tight(holdingsWantColumn(v))
	if got != want {
		at := 0
		for at < len(got) && at < len(want) && got[at] == want[at] {
			at++
		}
		lo := max(at-80, 0)
		bad("the column departs from screens.md §11 at byte %d:\n  got  …%s\n  want …%s", at, got[lo:min(at+120, len(got))],
			want[lo:min(at+120, len(want))])
	}

	// The page's forms: the bar's sign-out, the create form, one add form per holding and one
	// Remove per live membership -- each a POST with method before action; its buttons the same;
	// the pager's ways links, none a button and none an op-link; no hx-.
	live, ways := 0, 0
	for _, h := range v.Holdings {
		for _, m := range h.Members {
			if m.Live {
				live++
			}
		}
	}
	if v.Page > 1 {
		ways++
	}
	if v.HasNext {
		ways++
	}
	forms := 2 + len(v.Holdings) + live
	if strings.Count(body, "<form") != forms || strings.Count(body, `<form method="post" action="/operator/`) != forms ||
		strings.Count(body, "<button") != forms || strings.Contains(body, "hx-") {
		bad("%d form(s) (%d POST to the surface), %d button(s) or an hx- attribute on the page; want the sign-out, the create form, %d add form(s) and %d Remove",
			strings.Count(body, "<form"), strings.Count(body, `<form method="post" action="/operator/`), strings.Count(body, "<button"), len(v.Holdings), live)
	}
	if strings.Count(column, `class="op-pager">`) != ways || strings.Count(column, `<a href="/operator/holdings?page=`) != ways {
		bad("%d op-pager link(s), want the %d way(s)", strings.Count(column, `class="op-pager">`), ways)
	}
	for _, label := range []string{"Previous page", "Next page"} {
		if strings.Contains(body, `class="op-link">`+label+`</a>`) {
			bad("%s is still an op-link", label)
		}
	}
	for _, m := range regexp.MustCompile(`\shref="([^"]*)"`).FindAllStringSubmatch(column, -1) {
		if !holdingsHref.MatchString(html.UnescapeString(m[1])) {
			bad("the column links %q (the console, a business's overview and a page of the list only; no per-holding page)", m[1])
		}
	}
	// Nothing hidden and nothing screens.md §11 does not show. The notice's live region is the
	// column's one role.
	for _, banned := range []string{"hidden", "sr-only", "aria-hidden", "<svg", "<img", "<canvas", "<picture", "<select", "<datalist",
		"<textarea", "<table", "<script", "<iframe", "<details", "<dialog", "tabindex", "list=", "download", "onclick", "formaction",
		`type="hidden"`, `type="checkbox"`, `type="radio"`} {
		if strings.Contains(column, banned) {
			bad("the column carries %q", banned)
		}
	}
	if strings.Count(column, "role=") != strings.Count(column, `<section role="status" class="op-notice`) {
		bad("the column carries a role other than the notice's")
	}
	if n, want := strings.Count(column, "<input"), 1+len(v.Holdings); n != want {
		bad("%d input(s) in the column, want the name and %d business id(s)", n, len(v.Holdings))
	}
	members := 0
	for _, h := range v.Holdings {
		members += len(h.Members)
	}
	chips := regexp.MustCompile(`<span class="tally (tally--[a-z-]+)">`).FindAllStringSubmatch(column, -1)
	if len(chips) != members || strings.Count(column, "tally--") != members {
		bad("%d chip(s) in the column, want one per membership (%d)", len(chips), members)
	}
	for _, c := range chips {
		if c[1] != "tally--active" && c[1] != "tally--deactivated" {
			bad("the column carries the chip %s (Live and Ended only)", c[1])
		}
	}
	text := html.UnescapeString(noticeTag.ReplaceAllString(column, " "))
	if m := holdingsNotShown.FindString(text); m != "" {
		bad("the column says %q (screens.md §11 Not shown: rename / delete a holding, a tenant picker or search, a confirmation)", m)
	}
	return f
}

// TestHoldingsScreen_IsTheCreateCardAndTheDocket (M13-13; screens.md §11, README §6 "Pager", §6.1
// "Holdings"): each holdings state -- screens()'s six "holdings" renders (live and ended members
// with a next page; empty; page 3, past the end; added; not added with its rules; an unnamed
// holding and member cut across pages, both ways) and a render here of long values (a holding name
// of 120 characters and a business name of 130 with no space, the longest IANA zone name, an API
// code of 16, an unnamed holding with no business, a warn notice with the four rules, both ways)
// -- is the frame's column holding exactly, tags and words, holdingsWantColumn: the intro card (the
// op-label, the h1 at 26 / 28 / 32 px, the intro, the way back; no top margin), the notice, the
// create card (its 19 px op-heading keeping id="holding-create"; the Name field with its whole
// attribute set, the help, Create), the docket (its pinned heading, the state's empty sentence or
// the holdings -- each name, creation time, id, page-cut sentences, membership rows with every
// fact and the business's id after the times as the code orders them, the add form with its whole
// attribute set -- each membership's Remove only when live) and the pager's op-pager links. The
// page: the sign-out, the create form, an add form per holding and a Remove per live membership
// are its forms and buttons, each a POST to the surface; the pager's ways are op-pager links and
// none an op-link; no hx-. Nowhere in the column: hidden content (a hidden attribute or class,
// sr-only, aria-hidden), a drawing or image, a select, a datalist or a list attribute (a tenant
// picker), a text area, a table, a script, a frame, a disclosure or a dialog, a role but the
// notice's, an input but the name and the business ids, a chip but Live and Ended, a link but the
// console, an overview and a page of the list (a per-holding page), the words of a rename, a
// deletion, a search, a picker or a confirmation. CONTROLS, each read by the same function and
// each a finding: a word of the intro changed; the intro given back its top margin; the create
// heading back to docket-label; the name's required, maxlength or autocomplete dropped; the add
// field's name changed or its maxlength dropped; the help moved back after the button in view
// (its order dropped); aria-hidden on the way back; a pager link back to op-link; a pager way
// turned into a form; a rename form; a tenant picker; a membership's id moved before its name in
// the document; a live membership's Remove dropped; Live re-toned; the member list hidden; the
// empty sentences' words changed or swapped; the rules of a refused add dropped; a holding's name
// shown without its bdi.
//
// PART II -- red on any of the above. PART III -- what the screen looks like at each width is the
// screenshot round's (320 / 390 / 834 / 1200 / 1440 px), the docket's rules
// TestHoldingsDocket_TheScopedRulesKeepTheBoardsGrid'; the notice's words are holdings_test.go's --
// no completeness claim.
func TestHoldingsScreen_IsTheCreateCardAndTheDocket(t *testing.T) {
	all := screens(t)
	// screens()'s views (op8_test.go), restated: the reading is checked against them.
	live := operatorpages.HoldingMemberView{TenantID: "10000000-0000-4000-8000-000000000001", TenantPath: "/operator/tenants/10000000-0000-4000-8000-000000000001",
		Name: "Kebab Factory Ltd.", Timezone: "Europe/Malta", APICode: "KF", JoinedAt: "2026-10-12 09:00 UTC", Live: true,
		RemovePath: "/operator/holdings/60000000-0000-4000-8000-000000000001/members/10000000-0000-4000-8000-000000000001/remove"}
	ended := operatorpages.HoldingMemberView{TenantID: "10000000-0000-4000-8000-000000000002",
		TenantPath: "/operator/tenants/10000000-0000-4000-8000-000000000002", Name: "Kebab Manufacturing Ltd.",
		Timezone: "Europe/Malta", JoinedAt: "2026-10-12 09:01 UTC", LeftAt: "2026-10-12 09:02 UTC"}
	kf := operatorpages.HoldingView{ID: "60000000-0000-4000-8000-000000000001", Name: "Kebab Group", CreatedAt: "2026-10-12 08:59 UTC",
		MembersPath: "/operator/holdings/60000000-0000-4000-8000-000000000001/members", Members: []operatorpages.HoldingMemberView{live, ended}}
	empty := operatorpages.HoldingView{ID: "60000000-0000-4000-8000-000000000002", Name: "Empty Group", CreatedAt: "2026-10-12 08:58 UTC",
		MembersPath: "/operator/holdings/60000000-0000-4000-8000-000000000002/members"}
	page := operatorpages.HoldingsView{Page: 1, HasNext: true, Holdings: []operatorpages.HoldingView{kf, empty}}
	added, notAdded, unnamed := page, page, page
	added.NoticeTone, added.NoticeHeading, added.Notice = operatorpages.HoldingNoticeOK, "Added.", "N"
	notAdded.NoticeTone, notAdded.NoticeHeading, notAdded.Notice = operatorpages.HoldingNoticeWarn, "Not added — nothing changed.", "N"
	notAdded.NoticeRules = []string{"R1", "R2"}
	unnamedKF, unnamedLive := kf, live
	unnamedLive.Name = ""
	unnamedKF.Name, unnamedKF.ContinuedFromBefore, unnamedKF.ContinuesAfter = "", true, true
	unnamedKF.Members = []operatorpages.HoldingMemberView{unnamedLive}
	unnamed.Page, unnamed.Holdings = 2, []operatorpages.HoldingView{unnamedKF}
	states := map[string]operatorpages.HoldingsView{
		"holdings": page, "holdings, empty": {Page: 1}, "holdings, past the end": {Page: 3},
		"holdings, added": added, "holdings, not added": notAdded, "holdings, unnamed": unnamed,
	}
	for name := range all {
		if _, ok := states[name]; strings.HasPrefix(name, "holdings") && !ok {
			t.Fatalf("PREMISE: screens() has a holdings render %q this test does not read", name)
		}
	}
	long := holdingsLongView()
	var b strings.Builder
	if err := operatorpages.Holdings(long).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	all["long values"] = b.String()
	states["long values"] = long
	for name, v := range states {
		body, ok := all[name]
		if !ok {
			t.Fatalf("PREMISE: screens() has no %q", name)
		}
		for _, finding := range holdingsScreenFindings(name, body, v) {
			t.Error(finding)
		}
	}
	if !strings.Contains(all["long values"], "<bdi>"+strings.Repeat("M", 130)+"</bdi>") || !strings.Contains(all["long values"], "<bdi>"+strings.Repeat("H", 120)+"</bdi>") {
		t.Fatal("PREMISE: the long render does not carry the spaceless names")
	}

	const holding1 = "60000000-0000-4000-8000-000000000001"
	const removeForm = `<form method="post" action="/operator/holdings/` + holding1 + `/members/10000000-0000-4000-8000-000000000001/remove">` +
		`<button type="submit" class="btn btn--quiet">Remove</button></form>`
	const idSpan = `<span class="break-all text-xs text-ink/70">10000000-0000-4000-8000-000000000001</span>`
	firstRow := `<li class="op-row"><div class="flex flex-wrap items-center gap-x-2.5 gap-y-1 text-[15px] leading-snug">`
	for _, c := range []struct{ what, render, from, to string }{
		{"a word of the intro changed", "holdings", "Only this screen changes", "Only this screen can change"},
		{"the intro's top margin back", "holdings", holdingsIntro, strings.Replace(holdingsIntro, `class="`, `class="mt-3 `, 1)},
		{"the create heading back to docket-label", "holdings", holdingsCreateH2, `<h2 id="holding-create" class="docket-label">Create a holding</h2>`},
		{"the name's required dropped", "holdings", `maxlength="120" required`, `maxlength="120"`},
		{"the name's maxlength dropped", "holdings", ` maxlength="120"`, ""},
		{"the name's autocomplete dropped", "holdings", `name="name" maxlength="120" required autocomplete="off"`, `name="name" maxlength="120" required`},
		{"the add field's name changed", "holdings", `name="tenant_id"`, `name="tenant"`},
		{"the add field's maxlength dropped", "holdings", ` maxlength="64"`, ""},
		{"the help back after the button in view", "holdings", `<p class="order-last basis-full text-xs text-ink/70">`, `<p class="basis-full text-xs text-ink/70">`},
		{"the way back hidden from a reader", "holdings", `<a href="/operator" class="op-link">`, `<a href="/operator" class="op-link" aria-hidden="true">`},
		{"a pager link back to op-link", "holdings", `class="op-pager">Next page</a>`, `class="op-link">Next page</a>`},
		{"a pager way turned into a form", "holdings", `<a href="/operator/holdings?page=2" class="op-pager">Next page</a>`,
			`<form method="post" action="/operator/holdings"><input type="hidden" name="page" value="2"><button type="submit" class="op-pager">Next page</button></form>`},
		{"a rename form", "holdings", `</li></ol></section>`, `<form method="post" action="/operator/holdings/` + holding1 +
			`/rename"><input type="text" name="name"><button type="submit" class="btn btn--quiet">Save</button></form></li></ol></section>`},
		{"a tenant picker", "holdings", `<button type="submit" class="btn btn--primary max-md:basis-full">Add</button>`,
			`<select name="tenant_id"><option value="x">Kebab Factory Ltd.</option></select><button type="submit" class="btn btn--primary max-md:basis-full">Add</button>`},
		{"a membership's id moved before its name", "holdings", firstRow, `<li class="op-row">` + idSpan + `<div class="flex flex-wrap items-center gap-x-2.5 gap-y-1 text-[15px] leading-snug">`},
		{"a live membership's Remove dropped", "holdings", removeForm, ""},
		{"Live re-toned", "holdings", `<span class="tally tally--active">Live</span>`, `<span class="tally tally--running">Live</span>`},
		{"the member list hidden", "holdings", `<ol><li class="op-row">`, `<ol hidden><li class="op-row">`},
		{"the empty sentence's words changed", "holdings, empty", "There are no holdings yet.", "There are no holdings."},
		{"the past-the-end sentence given the empty one", "holdings, past the end", "This page is empty: the list ended on the page before.",
			"There are no holdings yet."},
		{"the rules of a refused add dropped", "holdings, not added", `<ul class="mt-1 list-disc pl-5"><li>R1</li><li>R2</li></ul>`, ""},
		{"a holding's name without its bdi", "long values", "<bdi>" + strings.Repeat("H", 120) + "</bdi>", strings.Repeat("H", 120)},
	} {
		mutated := strings.Replace(all[c.render], c.from, c.to, 1)
		if mutated == all[c.render] {
			t.Fatalf("CONTROL %s: %q is not on the page", c.what, c.from)
		}
		if len(holdingsScreenFindings("control", mutated, states[c.render])) == 0 {
			t.Errorf("CONTROL: %s is not a finding; the reading is blind", c.what)
		}
	}
}

// holdingsLongView is a page of the holdings' longest values, for the column test and the
// screenshot round: a holding name of 120 characters with no space (the most holdingName lets
// through) and a business name of 130 (a tenant's name has no length limit), the longest IANA
// zone name, an API code of 16 (the most APICodePattern allows), a live and an ended membership,
// an unnamed membership, an unnamed holding with no business, both page cuts, a warn notice with
// the four rules, and both ways of the pager.
func holdingsLongView() operatorpages.HoldingsView {
	const h1, h2 = "60000000-0000-4000-8000-0000000000a1", "60000000-0000-4000-8000-0000000000a2"
	const t1, t2 = "10000000-0000-4000-8000-0000000000b1", "10000000-0000-4000-8000-0000000000b2"
	return operatorpages.HoldingsView{Page: 2, HasNext: true, NoticeTone: operatorpages.HoldingNoticeWarn,
		NoticeHeading: "Not added — nothing changed.",
		Notice:        "The database adds a business only when every rule below holds. The list shows each member's timezone and API code.",
		NoticeRules: []string{"Its timezone is the timezone of every member of the holding.",
			"It is in no other holding, and at least 60 seconds have passed since it last left one.",
			"Its API code is not one a member of the holding already has.", "The holding and the business both exist."},
		Holdings: []operatorpages.HoldingView{
			{ID: h1, Name: strings.Repeat("H", 120), CreatedAt: "2026-10-25 23:59 UTC", MembersPath: "/operator/holdings/" + h1 + "/members",
				ContinuedFromBefore: true, ContinuesAfter: true, Members: []operatorpages.HoldingMemberView{
					{TenantID: t1, TenantPath: "/operator/tenants/" + t1, Name: strings.Repeat("M", 130), Timezone: "America/Argentina/ComodRivadavia",
						APICode: "KF0123456789ABCD", JoinedAt: "2026-10-25 23:58 UTC", Live: true,
						RemovePath: "/operator/holdings/" + h1 + "/members/" + t1 + "/remove"},
					{TenantID: t2, TenantPath: "/operator/tenants/" + t2, Timezone: "America/Argentina/ComodRivadavia",
						JoinedAt: "2026-10-24 08:00 UTC", LeftAt: "2026-10-25 08:00 UTC"},
				}},
			{ID: h2, CreatedAt: "2026-10-24 07:00 UTC", MembersPath: "/operator/holdings/" + h2 + "/members"},
		}}
}

// holdingsDocketRules is the holdings docket's layout in web/static/css/input.css (README §6.1
// "Holdings"), each rule scoped from the docket's heading id, its selector and its utilities
// exactly: the heading's dashed rule and bold page number; a holding a block between 2 px dashed
// rules whose names and ids break anywhere, its name with the id under it and, from the tablet,
// the creation time at the line's end (on the phone the name and the id drawn first), every later
// part the whole width, the memberships under a dashed 1 px rule; a membership the boards' grid,
// its rule the docket row's shared dashed one (README §6 "Docket list") -- one column on the
// phone, three tracks on the tablet (the timezone and the times side by side under the name,
// Remove centred in the third), four on the desktop (172 / 212 / 96 px after the name's, the id
// under the name, the name's row as tall as the name so the id stays 3 px under it beside a
// zone name that wraps) --, Remove at least 120 px wide on the phone, the facts' values in ink;
// a time never breaking (it moves whole under its word); and the name link's line height from the
// tablet, under the audit link's overlay.
var holdingsDocketRules = map[string][]string{
	`[aria-labelledby="holding-list"] > h2`:        {"border-b-2", "border-dashed", "border-line", "pb-3"},
	`[aria-labelledby="holding-list"] > h2 > span`: {"font-bold", "text-ink"},
	`[aria-labelledby="holding-list"] > ol > li`: {"flex", "flex-wrap", "items-baseline", "gap-x-5", "gap-y-3.5", "border-b-2", "border-dashed",
		"border-line", "pt-[22px]", "pb-6", "[overflow-wrap:anywhere]", "last:border-b-0", "last:pb-2"},
	`[aria-labelledby="holding-list"] > ol > li > :first-child`:    {"min-w-0", "max-md:-order-1", "max-md:basis-full", "md:flex-1"},
	`[aria-labelledby="holding-list"] > ol > li > :nth-child(2)`:   {"max-md:-mt-2.5"},
	`[aria-labelledby="holding-list"] > ol > li > :nth-child(3)`:   {"-mt-3", "basis-full", "max-md:-order-1"},
	`[aria-labelledby="holding-list"] > ol > li > :nth-child(n+4)`: {"basis-full"},
	`[aria-labelledby="holding-list"] > ol > li > ol`:              {"border-t", "border-dashed", "border-line"},
	`[aria-labelledby="holding-list"] .op-row`: {"grid", "grid-cols-1", "items-start", "gap-x-[18px]", "gap-y-1.5", "py-3.5",
		"md:grid-cols-[minmax(0,1fr)_minmax(0,1.15fr)_92px]", "md:gap-y-2", "desk:grid-cols-[minmax(0,1fr)_172px_212px_96px]",
		"desk:grid-rows-[auto_1fr]", "desk:gap-y-1.5"},
	`[aria-labelledby="holding-list"] .op-row > div:first-child`: {"row-start-1", "md:col-start-1", "md:col-end-3", "desk:col-end-2"},
	`[aria-labelledby="holding-list"] .op-row > div:nth-child(2)`: {"md:col-start-1", "md:row-start-3", "desk:col-start-2", "desk:row-start-1",
		"desk:row-end-3"},
	`[aria-labelledby="holding-list"] .op-row > div:nth-child(3)`: {"md:col-start-2", "md:row-start-3", "desk:col-start-3", "desk:row-start-1",
		"desk:row-end-3"},
	`[aria-labelledby="holding-list"] .op-row > span`: {"row-start-2", "-mt-[3px]", "md:col-start-1", "md:col-end-3", "md:-mt-[5px]",
		"desk:col-end-2", "desk:-mt-[3px]"},
	`[aria-labelledby="holding-list"] .op-row > form`: {"mt-1", "md:col-start-3", "md:row-start-1", "md:row-end-4", "md:mt-0", "md:self-center",
		"md:justify-self-end", "desk:col-start-4", "desk:row-end-3"},
	`[aria-labelledby="holding-list"] .op-row > form > button`:                  {"max-md:min-w-[120px]"},
	`[aria-labelledby="holding-list"] .op-row > div > span > span`:              {"text-ink"},
	`[aria-labelledby="holding-list"] .op-row > div:nth-child(3) > span > span`: {"whitespace-nowrap"},
	`[aria-labelledby="holding-list"] .op-row > div > .op-link`:                 {"md:relative", "md:min-h-0"},
}

// holdingsLinkOverlay is the name link's 44 px overlay (README §6 "Inline link hit area"): a
// declaration and an @apply, so readScopedRules (one @apply per rule) cannot read it; it is read
// here whole, once.
const holdingsLinkOverlay = `[aria-labelledby="holding-list"] .op-row > div > .op-link::before {
    content: '';
    @apply md:absolute md:inset-x-0 md:top-1/2 md:h-11 md:-translate-y-1/2;
  }`

// readHoldingsDocketRules is TestHoldingsDocket_TheScopedRulesKeepTheBoardsGrid's reading of input.css.
func readHoldingsDocketRules(css string) []string {
	var f []string
	if n := strings.Count(css, holdingsLinkOverlay); n != 1 {
		f = append(f, fmt.Sprintf("the name link's 44 px overlay occurs %d time(s) in input.css, want 1", n))
	}
	return append(f, readScopedRules(strings.Replace(css, holdingsLinkOverlay, "", 1), "holding-list", holdingsDocketRules)...)
}

// TestHoldingsDocket_TheScopedRulesKeepTheBoardsGrid (M13-13; README §6.1 "Holdings", boards
// OpHoldings, T-OpHoldings, M-OpHoldings): in web/static/css/input.css (comments stripped, read as
// flat `selectors { @apply …; }` rules by readScopedRules), every rule whose selector names the
// holdings docket's heading id is one of holdingsDocketRules' eighteen or the name link's
// overlay, each exactly once, one @apply of exactly its utilities; the overlay is there whole,
// once. CONTROLS, each read by the same function and each a finding: a holding's rule deleted;
// a name with no space no longer breaking; the name not drawn first on the phone; the creation
// time not beside the name from the tablet; the id not pulled under the name; a later part not
// the whole width; the membership not a grid; the id sliding down beside a tall zone (the
// desktop's rows sharing the extra height, the M13-13 review's measured 4-21 px); the boards'
// solid rule back on the rows; the list's top rule solid; the desktop's tracks changed; the id not under the
// name on the desktop; the timezone not spanning the name's two lines; Remove not in the last
// track; Remove narrower on the phone; the values' ink reaching the chip's word; a time breaking at its spaces; the link back to
// its own 44 px box from the tablet; the overlay deleted; a nineteenth rule, in the double-quoted,
// the single-quoted and the unquoted spelling.
//
// PART II -- red on any of the above. PART III -- what the rules draw at each width is the
// screenshot round's; the markup is TestHoldingsScreen_IsTheCreateCardAndTheDocket's -- no
// completeness claim.
func TestHoldingsDocket_TheScopedRulesKeepTheBoardsGrid(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	for _, f := range readHoldingsDocketRules(css) {
		t.Error(f)
	}
	const entry, ink = `[aria-labelledby="holding-list"] > ol > li {`, `[aria-labelledby="holding-list"] .op-row > div > span > span {`
	for _, c := range []struct{ what, from, to string }{
		{"a holding's rule deleted", entry, `.m1313-control-deleted {`},
		{"a name with no space no longer breaking", "pb-6\n           [overflow-wrap:anywhere] last:border-b-0", "pb-6\n           last:border-b-0"},
		{"the name not drawn first on the phone", "@apply min-w-0 max-md:-order-1 max-md:basis-full", "@apply min-w-0 max-md:basis-full"},
		{"the creation time not beside the name", " md:flex-1;", ";"},
		{"the id not pulled under the name", "@apply -mt-3 basis-full", "@apply basis-full"},
		{"a later part not the whole width", ":nth-child(n+4) { @apply basis-full; }", ":nth-child(n+4) { @apply w-auto; }"},
		{"the membership not a grid", "@apply grid grid-cols-1 items-start", "@apply grid-cols-1 items-start"},
		{"the id sliding down beside a tall zone (rows sharing the height)", " desk:grid-rows-[auto_1fr]", ""},
		{"the boards' solid rule back on the rows", "@apply grid grid-cols-1 items-start gap-x-[18px] gap-y-1.5 py-3.5",
			"@apply grid grid-cols-1 items-start gap-x-[18px] gap-y-1.5 border-solid py-3.5"},
		{"the list's top rule solid", "> ol { @apply border-t border-dashed border-line; }", "> ol { @apply border-t border-line; }"},
		{"the desktop's tracks changed", "desk:grid-cols-[minmax(0,1fr)_172px_212px_96px]", "desk:grid-cols-[minmax(0,1fr)_160px_212px_96px]"},
		{"the id not under the name on the desktop", "md:-mt-[5px] desk:col-end-2", "md:-mt-[5px]"},
		{"the timezone not spanning the name's two lines", "desk:col-start-2 desk:row-start-1 desk:row-end-3", "desk:col-start-2 desk:row-start-1"},
		{"Remove not in the last track", " desk:col-start-4 desk:row-end-3", " desk:row-end-3"},
		{"Remove narrower on the phone", "@apply max-md:min-w-[120px]", "@apply max-md:min-w-[96px]"},
		{"the values' ink reaching the chip's word", ink, `[aria-labelledby="holding-list"] .op-row > div > span {`},
		{"a time breaking at its spaces", "> div:nth-child(3) > span > span { @apply whitespace-nowrap; }", "> div:nth-child(3) > span > span { @apply text-ink; }"},
		{"the link back to its own 44 px box", "> .op-link { @apply md:relative md:min-h-0; }", "> .op-link { @apply md:relative; }"},
		{"the overlay deleted", holdingsLinkOverlay, ""},
		{"a nineteenth rule", entry, `[aria-labelledby="holding-list"] .op-row > div { @apply text-xs; }` + "\n  " + entry},
		{"a nineteenth rule, single-quoted", entry, `[aria-labelledby='holding-list'] .op-row { @apply py-0; }` + "\n  " + entry},
		{"a nineteenth rule, unquoted", entry, `[aria-labelledby=holding-list] .op-row > span { @apply hidden; }` + "\n  " + entry},
	} {
		mutated := strings.Replace(css, c.from, c.to, 1)
		if mutated == css {
			t.Fatalf("CONTROL %s: %q is not in input.css", c.what, c.from)
		}
		if len(readHoldingsDocketRules(mutated)) == 0 {
			t.Errorf("CONTROL: %s is not a finding; the reading is blind", c.what)
		}
	}
}

// TestHoldingsScreen_TheTextClearsAA recomputes, from tailwind.config.js's palette, the contrast of
// the holdings screen's text on its grounds since M13-13 (WCAG 2.1 relative luminance, sRGB; a
// translucent token composited on its ground): ink on paper (the cards, the holdings' names, the
// facts' values, Remove's word on its own paper, the pager's word on the op-pager's paper), ink at
// 70 % on paper (the label, the create help, the creation time's word, the ids, the facts' words),
// ink at 70 % on ink at 3 % over paper (the add box's label and help), ink on green-lite (Live) and
// on line at 10 % over paper (Ended), tappa-green on paper (a membership's link, the way back),
// paper on tappa-green (Create, Add) -- each against AA's 4.5:1 (the chips and the labels are
// 11 px, the ids 12 px; this test does not relax for the h1 as large text). And it ties the two
// chips to the grounds computed here: web/static/css/input.css's one rule naming tally--active
// applies the tappa-green frame on green-lite, its one naming tally--deactivated the line frame on
// line at 10 %. CONTROL: the same reading finds a third chip rule.
func TestHoldingsScreen_TheTextClearsAA(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "tailwind.config.js"))
	if err != nil {
		t.Fatal(err)
	}
	hexOf := func(token string) [3]float64 {
		m := regexp.MustCompile(`'?` + regexp.QuoteMeta(token) + `'?:\s*'#([0-9A-Fa-f]{6})'`).FindSubmatch(b)
		if m == nil {
			t.Fatalf("tailwind.config.js has no %s", token)
		}
		var c [3]float64
		for i := 0; i < 3; i++ {
			v, err := strconv.ParseUint(string(m[1][2*i:2*i+2]), 16, 8)
			if err != nil {
				t.Fatal(err)
			}
			c[i] = float64(v)
		}
		return c
	}
	over := func(fg [3]float64, alpha float64, bg [3]float64) [3]float64 {
		var c [3]float64
		for i := range c {
			c[i] = alpha*fg[i] + (1-alpha)*bg[i]
		}
		return c
	}
	ink, paper, green, greenLite, line := hexOf("ink"), hexOf("paper"), hexOf("tappa-green"), hexOf("green-lite"), hexOf("line")
	for _, c := range []struct {
		what   string
		fg, bg [3]float64
	}{
		{"ink on paper", ink, paper},
		{"ink at 70% on paper", over(ink, 0.7, paper), paper},
		{"ink at 70% on ink at 3% over paper (the add box)", over(ink, 0.7, over(ink, 0.03, paper)), over(ink, 0.03, paper)},
		{"ink on green-lite (Live)", ink, greenLite},
		{"ink on line at 10% over paper (Ended)", ink, over(line, 0.1, paper)},
		{"tappa-green on paper", green, paper},
		{"paper on tappa-green", paper, green},
	} {
		if got := contrast(c.fg, c.bg); got < 4.5 {
			t.Errorf("%s = %.2f:1, want at least 4.5:1", c.what, got)
		}
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	chipRules := func(css, chip string) []string {
		src := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(css, "")
		var out []string
		for _, m := range regexp.MustCompile(`([^{}]*)\{([^{}]*)\}`).FindAllStringSubmatch(src, -1) {
			for _, sel := range strings.Split(m[1], ",") {
				if strings.TrimSpace(sel) == "."+chip {
					out = append(out, strings.Join(strings.Fields(m[2]), " "))
				}
			}
		}
		return out
	}
	css := string(raw)
	for chip, want := range map[string]string{
		"tally--active":      "@apply border-tappa-green bg-green-lite;",
		"tally--deactivated": "@apply border-line bg-line/10;",
	} {
		if got := chipRules(css, chip); len(got) != 1 || got[0] != want {
			t.Errorf("input.css's rules naming .%s are %q, want exactly [%q]", chip, got, want)
		}
	}
	extra := strings.Replace(css, ".tally--deactivated,", ".tally--deactivated { @apply bg-saffron; }\n  .tally--deactivated,", 1)
	if extra == css || len(chipRules(extra, "tally--deactivated")) != 2 {
		t.Fatal("CONTROL: a second rule naming tally--deactivated is not read")
	}
}

// operatorHeadingRules is the two in-card headings of web/static/css/input.css (README §6 "Step /
// card heading"), their selectors and utilities exactly: the card heading 19 px (Holdings' "Create
// a holding", M13-13) and the enrollment step 16 px, 17 px from the tablet, line height 1.3
// (M13-05).
var operatorHeadingRules = map[string]map[string][]string{
	"op-heading": {`.op-heading`: {"font-display", "text-[19px]", "font-bold", "tracking-tight"}},
	"op-step":    {`.op-step`: {"font-display", "text-base", "font-bold", "leading-[1.3]", "tracking-tight", "md:text-[17px]"}},
}

// readOperatorHeadings is TestOperatorHeadings_TheRulesKeepTheBoardsSizes' reading: every rule of
// css naming either class is its one pinned rule, and README §6's recipe for each names exactly
// the same utilities.
func readOperatorHeadings(css, readme string) []string {
	var f []string
	for class, want := range operatorHeadingRules {
		f = append(f, readScopedRules(css, class, want)...)
		m := regexp.MustCompile("`\\."+regexp.QuoteMeta(class)+"` `([^`]*)`").FindAllStringSubmatch(readme, -1)
		got := []string{}
		if len(m) == 1 {
			got = strings.Fields(m[0][1])
		}
		w := want["."+class]
		if len(m) != 1 || len(got) != len(w) || !holdingsSameSet(got, w) {
			f = append(f, fmt.Sprintf("README §6 gives .%s the recipe(s) %q, want exactly one naming %v", class, m, w))
		}
	}
	return f
}

func holdingsSameSet(a, b []string) bool {
	seen := map[string]int{}
	for _, x := range a {
		seen[x]++
	}
	for _, x := range b {
		seen[x]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}

// TestOperatorHeadings_TheRulesKeepTheBoardsSizes (M13-13 review; README §6 "Step / card
// heading"): in web/static/css/input.css (comments stripped, read by readScopedRules) the one
// rule naming op-heading is one @apply of exactly the 19 px card heading's utilities and the one
// naming op-step exactly the enrollment step's (16 px, 17 px from the tablet, line height 1.3);
// docs/design/operator-redesign/README.md's §6 recipe for each names the same utilities, once.
// The markup tests pin only the class names (the Holdings column test, the Enroll card test), so
// without this one a heading could change size with every markup test green. CONTROLS, each read
// by the same function and each a finding: the card heading at 17 px; the step without its
// tablet size; the step's line height dropped; a second rule naming op-heading; README's card
// heading recipe at 17 px.
//
// PART II -- red on any of the above. PART III -- these two classes only (no completeness claim).
func TestOperatorHeadings_TheRulesKeepTheBoardsSizes(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "design", "operator-redesign", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	css, readme := string(raw), string(doc)
	for _, f := range readOperatorHeadings(css, readme) {
		t.Error(f)
	}
	const heading = `.op-heading { @apply font-display text-[19px] font-bold tracking-tight; }`
	for _, c := range []struct{ what, from, to string }{
		{"the card heading at 17 px", heading, `.op-heading { @apply font-display text-[17px] font-bold tracking-tight; }`},
		{"the step without its tablet size", " tracking-tight md:text-[17px]; }", " tracking-tight; }"},
		{"the step's line height dropped", "font-bold leading-[1.3] tracking-tight", "font-bold tracking-tight"},
		{"a second rule naming op-heading", heading, heading + "\n  .op-main .op-heading { @apply text-[17px]; }"},
	} {
		mutated := strings.Replace(css, c.from, c.to, 1)
		if mutated == css {
			t.Fatalf("CONTROL %s: %q is not in input.css", c.what, c.from)
		}
		if len(readOperatorHeadings(mutated, readme)) == 0 {
			t.Errorf("CONTROL: %s is not a finding; the reading is blind", c.what)
		}
	}
	const recipe = "`.op-heading` `font-display text-[19px] font-bold tracking-tight`"
	if mutated := strings.Replace(readme, recipe, strings.Replace(recipe, "19px", "17px", 1), 1); mutated == readme {
		t.Fatal("CONTROL: README §6's card heading recipe is not in README.md")
	} else if len(readOperatorHeadings(css, mutated)) == 0 {
		t.Error("CONTROL: README's card heading recipe at 17 px is not a finding; the reading is blind")
	}
}
