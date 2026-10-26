package operator_test

// opaudit_test.go -- M13-12: the audit log in the redesign. docs/plan/m13-operator-redesign.md,
// card M13-12; docs/design/operator-redesign README §6 "Pager", "Inline link hit area", §6.1
// "Audit row", screens.md §10.

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

// The audit screen's card, docket and pager and the pieces of them screens.md §10 names, each a
// whole tag. The card spaces its blocks (README §6 "Card": gap-3.5 on the markup, no mt-* in the
// bodies); the h1 is the board's 26 / 28 / 32 px (orchestrator decision (e)); the filter is the
// boards' framed box, the select in a box of its own so its pinned tag keeps its one class; the
// empty sentences and the last-page note are the boards' own; the pager's buttons are op-pager,
// each form half the row on the phone.
const (
	auditCard   = `<section class="op-card flex flex-col gap-3.5">`
	auditLabel  = `<p class="op-label">Audit log</p>`
	auditH1     = `<h1 class="font-display text-[26px] font-bold leading-[1.1] tracking-[-0.03em] md:text-[28px] desk:text-[32px]">Who did what on this surface</h1>`
	auditIntro  = `<p class="text-[15px] leading-[1.55]">`
	auditFilter = `<form method="post" action="/operator/audit" class="flex flex-col gap-2 rounded-xl border border-line bg-ink/[.03] p-3.5 md:px-[18px] md:py-4">`
	auditKinds  = `<div class="flex flex-wrap gap-2.5"><div class="min-w-0 max-w-[440px] flex-[1_1_240px] *:h-full *:font-display *:text-[15px]">`
	auditShow   = `<button type="submit" class="btn btn--primary max-md:basis-full">Show</button>`
	auditEmpty  = `<p class="pt-[22px] pb-2 font-display text-[15px] leading-normal">`
	auditLast   = `<p class="mt-1.5 border-t-2 border-dashed border-line pt-3 font-display text-[13px] text-ink/70">`
	auditPagerN = `<nav class="flex flex-wrap gap-2.5 md:gap-3 max-md:*:grid max-md:*:flex-1" aria-label="Pages of the audit log">`
)

// auditWantColumn is the whole content column of v, each row's facts left out (an empty
// <li class="op-row"></li> per row; the facts are TestAuditScreen_EveryRowSaysWhatTheLogHolds'):
// the card with the filter's options, the docket -- its pinned heading, the empty sentence of
// the state or the rows, the last-page note -- and the pager with the ways v offers.
func auditWantColumn(v operatorpages.AuditLogView) string {
	e := html.EscapeString
	s := auditCard + auditLabel + auditH1 + auditIntro + `Every sign-in, sign-out, read and publication on the operator ` +
		`surface, newest first, for every operator. Opening this page is a read too, and it is listed.</p>` + auditFilter +
		`<label for="audit-kind" class="op-label">Kind</label>` + auditKinds + `<select id="audit-kind" name="kind" class="op-input">`
	for _, o := range v.Options {
		sel := ""
		if o.Selected {
			sel = " selected"
		}
		s += `<option value="` + e(o.Value) + `"` + sel + `>` + e(o.Label) + `</option>`
	}
	s += `</select></div>` + auditShow + `</div></form><p class="text-sm"><a href="/operator" class="op-link">Back to the console</a></p></section>`
	s += `<section class="docket" aria-labelledby="audit-list"><h2 id="audit-list" class="docket-label">Newest first · ` + e(v.Filter) +
		` · page <span class="font-mono">` + strconv.Itoa(v.Page) + `</span></h2>`
	switch {
	case len(v.Rows) == 0 && v.Page > 1:
		s += auditEmpty + `This page is empty: the log ended on the page before.</p>`
	case len(v.Rows) == 0:
		s += auditEmpty + `The log holds no entry of this kind.</p>`
	default:
		s += `<ol>` + strings.Repeat(`<li class="op-row"></li>`, len(v.Rows)) + `</ol>`
	}
	if v.LastPage {
		s += auditLast + `This is the last page the screen shows: the newest <span class="font-mono">` + e(v.Reach) +
			`</span> entries of the kind chosen.</p>`
	}
	s += `</section>`
	if v.Page > 1 || v.HasNext {
		form := func(page int, label string) string {
			return `<form method="post" action="/operator/audit"><input type="hidden" name="kind" value="` + e(v.Kind) +
				`"><input type="hidden" name="page" value="` + strconv.Itoa(page) + `"><button type="submit" class="op-pager">` +
				label + `</button></form>`
		}
		s += auditPagerN
		if v.Page > 1 {
			s += form(v.Page-1, "Previous page")
		}
		if v.HasNext {
			s += form(v.Page+1, "Next page")
		}
		s += `</nav>`
	}
	return s
}

// auditNotShown is screens.md §10's "Not shown" list as words, and the derived verdict a
// suspension row must not grow (the word says the action was recorded, not that a state moved).
var auditNotShown = regexp.MustCompile(`(?i)\b(?:exports?|csv|pdf|download|print|compromised|suspicious|breach(?:ed)?|attack|suspended|reinstated|search term|searched for)\b`)

// auditScreenFindings is TestAuditScreen_IsTheFilterCardAndTheDocket's reading of one render of
// v: one finding per way it departs from what auditWantColumn says.
func auditScreenFindings(name, body string, v operatorpages.AuditLogView) []string {
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
	inner := tight(column[len(open) : len(column)-len("</main>")])
	rows := regexp.MustCompile(`<li class="op-row">(.*?)</li>`)
	for i, r := range rows.FindAllStringSubmatch(inner, -1) {
		if i < len(v.Rows) && !strings.HasPrefix(r[1], `<span class="font-mono font-bold">`+html.EscapeString(v.Rows[i].At)+`</span>`) {
			bad("row %d does not open with its time's span", i+1)
		}
	}
	got := rows.ReplaceAllString(inner, `<li class="op-row"></li>`)
	if want := tight(auditWantColumn(v)); got != want {
		at := 0
		for at < len(got) && at < len(want) && got[at] == want[at] {
			at++
		}
		lo := max(at-80, 0)
		bad("the column departs from screens.md §10 at byte %d:\n  got  …%s\n  want …%s", at, got[lo:min(at+120, len(got))],
			want[lo:min(at+120, len(want))])
	}

	// The page's forms: the bar's sign-out, the filter and the pager's ways; its buttons the same;
	// the pager's buttons exactly the ways and none an op-link; no hx-.
	ways := 0
	if v.Page > 1 {
		ways++
	}
	if v.HasNext {
		ways++
	}
	if strings.Count(body, "<form") != 2+ways || strings.Count(body, "<button") != 2+ways ||
		strings.Count(body, `<button type="submit" class="op-pager">`) != ways || strings.Contains(body, "hx-") {
		bad("%d form(s), %d button(s) or an hx- attribute on the page; want the sign-out, the filter and %d pager way(s)",
			strings.Count(body, "<form"), strings.Count(body, "<button"), ways)
	}
	for _, label := range []string{"Previous page", "Next page"} {
		if strings.Contains(body, `class="op-link">`+label+`</button>`) {
			bad("%s is still an op-link button", label)
		}
	}
	// Nothing hidden and nothing screens.md §10 does not show. The pager's kind and page fields are
	// the column's only inputs and are read out first; the select is the filter's one.
	scan := regexp.MustCompile(`<input type="hidden" name="(?:kind" value="[a-z_]*|page" value="[0-9]+)">`).ReplaceAllString(column, "")
	for _, banned := range []string{"hidden", "sr-only", "aria-hidden", "<svg", "<img", "<canvas", "<picture", "<input",
		"<textarea", "<table", "<script", "<iframe", "role=", "tabindex", "download", "?"} {
		if strings.Contains(scan, banned) {
			bad("the column carries %q", banned)
		}
	}
	if n := strings.Count(column, "<select"); n != 1 {
		bad("%d select(s) in the column, want the filter's one", n)
	}
	if m := regexp.MustCompile(`tally--[a-z-]+`).FindAllString(column, -1); len(m) != strings.Count(column, "tally--unrecognised") {
		bad("the column carries a chip other than Unrecognised: %q", m)
	}
	text := html.UnescapeString(noticeTag.ReplaceAllString(column, " "))
	if m := auditNotShown.FindString(text); m != "" {
		bad("the column says %q (screens.md §10 Not shown: an export, a per-tenant view, a derived verdict, a search term)", m)
	}
	return f
}

// TestAuditScreen_IsTheFilterCardAndTheDocket (M13-12; screens.md §10, README §6 "Pager", §6.1
// "Audit row"): each audit-screen state -- screens()'s five "audit log" renders (every row shape
// with a next page; filtered, page 2 with both ways; past the end; the last page; empty) and a
// render here of long values (a tenant, an operator and an account name of 130 characters with no
// space, an unnamed tenant, a tenant id no tenant has, the two OP-15 kinds' tenant-targeted rows,
// a raw kind of 63 characters) -- is the frame's column holding exactly, tags and words, with each
// row's facts left out, auditWantColumn: the card (the op-label, the h1 at 26 / 28 / 32 px, the
// intro, the filter's framed box -- its op-label Kind, the pinned select in its own box, every
// option with the chosen one selected, Show -- and the way back; no top margin), the docket (its
// pinned heading, the state's empty sentence or the rows, each opening with its time's span, the
// last-page note) and the pager (the nav, its forms posting the kind and the page in hidden
// fields to the log, each button an op-pager). The page: the sign-out, the filter and the pager's
// ways are its forms and buttons, the op-pager buttons exactly the ways, none an op-link; no hx-.
// Nowhere in the column (the pager's fields read out first): hidden content (a hidden attribute or
// class, sr-only, aria-hidden), a drawing or image, a control but the filter's select and the
// buttons, a table, a script, a frame, a download, a query in a link, a chip but Unrecognised (no
// derived "suspended"), the words of what §10 does not show. CONTROLS, each read by the same
// function and each a finding: a word of the card changed; the intro given back its top margin;
// the select's box dropped; the Kind label back to docket-label; aria-hidden on the way back; a
// pager button back to op-link; a third pager form; a suspended chip on a suspension's row; an
// export link; the empty sentence's words changed; the empty sentence back to the docket's line
// height (the boards' .dempty is 1.5).
//
// PART II -- red on any of the above. PART III -- what the screen looks like at each width is the
// screenshot round's (320 / 390 / 834 / 1200 / 1440 px), the row's rules
// TestAuditRow_TheScopedRulesKeepTheBoardsColumns'; the rows' facts and words are the op14 tests'
// -- no completeness claim.
func TestAuditScreen_IsTheFilterCardAndTheDocket(t *testing.T) {
	all := screens(t)
	// screens()'s views (op8_test.go), restated: the reading is checked against them.
	options := []operatorpages.AuditKindOption{{Value: "", Label: "Every kind", Selected: true},
		{Value: "login", Label: "Signed in"}, {Value: "read", Label: "Read"}}
	rows := func(n int) []operatorpages.AuditRow {
		out := make([]operatorpages.AuditRow, n)
		for i := range out {
			out[i].At = fmt.Sprintf("2026-10-07 09:30:%02d UTC", 15-i)
		}
		return out
	}
	states := map[string]operatorpages.AuditLogView{
		"audit log":                {Filter: "Every kind", Page: 1, Options: options, Rows: rows(9), HasNext: true, Reach: "50,000"},
		"audit log, filtered":      {Kind: "login", Filter: "Signed in", Page: 2, Options: options, Rows: rows(1), HasNext: true, Reach: "50,000"},
		"audit log, past the end":  {Kind: "login", Filter: "Signed in", Page: 3, Options: options, Reach: "50,000"},
		"audit log, the last page": {Filter: "Every kind", Page: 1000, Options: options, Rows: rows(9), LastPage: true, Reach: "50,000"},
		"audit log, empty":         {Filter: "Every kind", Page: 1, Options: options},
	}
	for name := range all {
		if _, ok := states[name]; strings.HasPrefix(name, "audit log") && !ok {
			t.Fatalf("PREMISE: screens() has an audit render %q this test does not read", name)
		}
	}
	long := auditLongView()
	var b strings.Builder
	if err := operatorpages.AuditLog(long).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	all["long values"] = b.String()
	states["long values"] = long
	for name, v := range states {
		body, ok := all[name]
		if !ok {
			t.Fatalf("PREMISE: screens() has no %q", name)
		}
		for _, finding := range auditScreenFindings(name, body, v) {
			t.Error(finding)
		}
	}
	if !strings.Contains(all["long values"], "Tenant suspension recorded") || !strings.Contains(all["long values"], "<bdi>"+strings.Repeat("N", 130)+"</bdi>") {
		t.Fatal("PREMISE: the long render does not carry the suspension's row or the spaceless tenant name")
	}

	page := all["audit log, filtered"]
	prev := `<button type="submit" class="op-pager">Previous page</button>`
	for _, c := range []struct{ what, from, to string }{
		{"a word of the card changed", "Opening this page is a read too", "Opening this page is a read, too"},
		{"the intro's top margin back", auditIntro, strings.Replace(auditIntro, `class="`, `class="mt-3 `, 1)},
		{"the select's box dropped", `<div class="min-w-0 max-w-[440px] flex-[1_1_240px] *:h-full *:font-display *:text-[15px]"><select`, `<div><select`},
		{"the Kind label back to docket-label", `<label for="audit-kind" class="op-label">`, `<label for="audit-kind" class="docket-label">`},
		{"the way back hidden from a reader", `<a href="/operator" class="op-link">`, `<a href="/operator" class="op-link" aria-hidden="true">`},
		{"a pager button back to op-link", prev, `<button type="submit" class="op-link">Previous page</button>`},
		{"a third pager form", `</nav>`, `<form method="post" action="/operator/audit"><input type="hidden" name="kind" value="login">` +
			`<input type="hidden" name="page" value="9"><button type="submit" class="op-pager">Last page</button></form></nav>`},
		{"an export link", `Back to the console</a>`, `Back to the console</a><a href="/operator/audit.csv" class="op-link">Export</a>`},
	} {
		mutated := strings.Replace(page, c.from, c.to, 1)
		if mutated == page {
			t.Fatalf("CONTROL %s: %q is not on the page", c.what, c.from)
		}
		if len(auditScreenFindings("control", mutated, states["audit log, filtered"])) == 0 {
			t.Errorf("CONTROL: %s is not a finding; the reading is blind", c.what)
		}
	}
	for _, c := range []struct{ what, render, from, to string }{
		{"a suspended chip on a suspension's row", "long values", `>Tenant suspension recorded</span>`,
			`>Tenant suspension recorded</span><span class="tally tally--suspended">Suspended</span>`},
		{"the empty sentence's words changed", "audit log, empty", "holds no entry of this kind", "holds no entry"},
		{"the empty sentence back to the docket's line height", "audit log, empty", ` leading-normal">The log holds`, `">The log holds`},
		{"the past-the-end sentence given the empty one", "audit log, past the end", "This page is empty: the log ended on the page before.",
			"The log holds no entry of this kind."},
	} {
		mutated := strings.Replace(all[c.render], c.from, c.to, 1)
		if mutated == all[c.render] {
			t.Fatalf("CONTROL %s: %q is not on the page", c.what, c.from)
		}
		if len(auditScreenFindings("control", mutated, states[c.render])) == 0 {
			t.Errorf("CONTROL: %s is not a finding; the reading is blind", c.what)
		}
	}
}

// auditLongView is a page of the audit log's longest values, for the column test and the
// screenshot round: names of 130 characters with no space in them (a tenant's, an operator's, an
// account's -- none has a length limit), an unnamed tenant and a tenant id no tenant has, the two
// OP-15 kinds' rows (tenant-targeted, no reason), a raw kind of 63 characters, a page and a
// filter, and both ways of the pager.
func auditLongView() operatorpages.AuditLogView {
	const tenantID, tenantPath = "10000000-0000-4000-8000-000000000001", "/operator/tenants/10000000-0000-4000-8000-000000000001"
	const actorID = "30000000-0000-4000-8000-000000000001"
	n, o, a := strings.Repeat("N", 130), strings.Repeat("O", 130), strings.Repeat("A", 130)
	word := func(s string) operatorpages.AuditWord { return operatorpages.AuditWord{Text: s} }
	return operatorpages.AuditLogView{Kind: "read", Filter: "Read", Page: 2, HasNext: true, Reach: "50,000",
		Options: []operatorpages.AuditKindOption{{Value: "", Label: "Every kind"},
			{Value: "unknown_email", Label: "Sign-in refused: no active account with that address"}, {Value: "read", Label: "Read", Selected: true},
			{Value: "tenant_suspended", Label: "Tenant suspension recorded"}, {Value: "tenant_reinstated", Label: "Tenant reinstatement recorded"}},
		Rows: []operatorpages.AuditRow{
			{At: "2026-10-25 23:59:59 UTC", Kind: word("Read"), ActorID: actorID, Actor: o, TenantID: tenantID, TenantPath: tenantPath,
				Tenant: n, Scope: word("a tenant's billing"), Page: "1000", PageSize: "50", Session: "3f9a1c0e"},
			{At: "2026-10-25 23:59:58 UTC", Kind: word("Tenant suspension recorded"), ActorID: actorID, Actor: o, TenantID: tenantID,
				TenantPath: tenantPath, Tenant: n, Session: "3f9a1c0e"},
			{At: "2026-10-25 23:59:57 UTC", Kind: word("Tenant reinstatement recorded"), ActorID: actorID, Actor: "Mara Vella",
				TenantID: tenantID, TenantPath: tenantPath, Session: "3f9a1c0e"},
			{At: "2026-10-25 23:59:56 UTC", Kind: word("Sign-in refused: no active account with that address"), AccountID: actorID, Account: a},
			{At: "2026-10-25 23:59:55 UTC", Kind: word("Read"), ActorID: actorID, TenantID: "40000000-0000-4000-8000-000000000009",
				Scope: word("a tenant's overview"), Session: "c81e0b94"},
			{At: "2026-10-25 23:59:54 UTC", Kind: operatorpages.AuditWord{Text: "zz_" + strings.Repeat("x", 60), Unknown: true},
				ActorID: actorID, Actor: "Joseph Camilleri", Filter: word("Tenant reinstatement recorded"),
				Scope: word("this audit log"), Search: word("Searched by tenant id"), DetailHidden: true, Session: "c81e0b94"},
		}}
}

// auditRowRules is the audit docket's layout and type in web/static/css/input.css (README §6.1
// "Audit row", §6 "Inline link hit area"), each rule scoped from the docket's heading id, its
// selector and its utilities exactly: the heading's dashed rule and bold page number; the row one
// column on the phone and, from the tablet, the indent of the time's column (206 px on the tablet,
// 220 on the desktop) with the time pulled back into it, 18 px between facts, values breaking
// anywhere; the time 13 px, never breaking; the kind 15 px, 6 px above the first fact at every
// width (2 px under the phone's 4 px gap, 4 px under the tablet's 2 px), on a line of its own from
// the tablet;
// the facts in Space Grotesk at ink 70 %, their values (not the link) in ink; on the phone the tenant's
// fact a wrapping flex, its word centred on the 44 px link; and M13-02's tenant
// link, inline-block from the tablet so its 44 px overlay holds on a wrapping name.
var auditRowRules = map[string][]string{
	`[aria-labelledby="audit-list"] > h2`:        {"border-b-2", "border-dashed", "border-line", "pb-3"},
	`[aria-labelledby="audit-list"] > h2 > span`: {"font-bold", "text-ink"},
	`[aria-labelledby="audit-list"] .op-row`: {"flex-col", "items-start", "gap-y-1", "py-3.5", "[overflow-wrap:anywhere]", "last:pb-1.5",
		"md:flex-row", "md:items-baseline", "md:gap-x-[18px]", "md:gap-y-0.5", "md:py-4", "md:pl-[206px]", "desk:pl-[220px]"},
	`[aria-labelledby="audit-list"] .op-row > :first-child`: {"whitespace-nowrap", "text-[13px]", "md:-ml-[206px]", "md:w-[188px]",
		"desk:-ml-[220px]", "desk:w-[202px]"},
	`[aria-labelledby="audit-list"] .op-row > :nth-child(2)`:             {"mb-0.5", "text-[15px]", "leading-[1.35]", "md:mb-1", "md:basis-full"},
	`[aria-labelledby="audit-list"] .op-row > :nth-child(n+3)`:           {"font-display", "text-ink/70"},
	`[aria-labelledby="audit-list"] .op-row > :nth-child(n+3) > :not(a)`: {"text-ink"},
	`[aria-labelledby="audit-list"] .op-row > span:has(> .op-link)`:      {"max-md:flex", "max-md:flex-wrap", "max-md:items-center", "max-md:gap-x-1.5"},
	`[aria-labelledby="audit-list"] .op-row > span > .op-link`:           {"md:relative", "md:inline-block", "md:min-h-0"},
}

// auditLinkOverlay is M13-02's 44 px overlay on the row's tenant link (README §6 "Inline link hit
// area"): a declaration and an @apply, so readScopedRules (one @apply per rule) cannot read it;
// it is read here whole, once.
const auditLinkOverlay = `[aria-labelledby="audit-list"] .op-row > span > .op-link::before {
    content: '';
    @apply md:absolute md:inset-x-0 md:top-1/2 md:h-11 md:-translate-y-1/2;
  }`

// readAuditRowRules is TestAuditRow_TheScopedRulesKeepTheBoardsColumns' reading of input.css.
func readAuditRowRules(css string) []string {
	var f []string
	if n := strings.Count(css, auditLinkOverlay); n != 1 {
		f = append(f, fmt.Sprintf("the tenant link's 44 px overlay occurs %d time(s) in input.css, want 1", n))
	}
	return append(f, readScopedRules(strings.Replace(css, auditLinkOverlay, "", 1), "audit-list", auditRowRules)...)
}

// TestAuditRow_TheScopedRulesKeepTheBoardsColumns (M13-12; README §6.1 "Audit row", §6 "Inline
// link hit area", boards OpAudit, T-OpAudit, M-OpAudit): in web/static/css/input.css (comments
// stripped, read as flat `selectors { @apply …; }` rules by readScopedRules), every rule whose
// selector names the audit docket's heading id is one of auditRowRules' nine or M13-02's overlay,
// each exactly once, one @apply of exactly its utilities; the overlay is there whole, once.
// CONTROLS, each read by the same function and each a finding: the row's rule deleted; the phone's
// one column dropped; the values not breaking; the tablet's indent dropped; the time not pulled
// back; the time breaking; the kind not alone on its line; the phone's 6 px under the kind back to 4; the facts' face dropped; the values'
// ink rule reaching the link; the phone's centred word dropped; the link inline (M13-02's measured collapse); the overlay deleted;
// a tenth rule, in the double-quoted, the single-quoted and the unquoted spelling.
//
// PART II -- red on any of the above. PART III -- what the rules draw at each width is the
// screenshot round's; the markup is TestAuditScreen_IsTheFilterCardAndTheDocket's -- no
// completeness claim.
func TestAuditRow_TheScopedRulesKeepTheBoardsColumns(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	for _, f := range readAuditRowRules(css) {
		t.Error(f)
	}
	const row, kind = `[aria-labelledby="audit-list"] .op-row {`, `[aria-labelledby="audit-list"] .op-row > :nth-child(2) {`
	for _, c := range []struct{ what, from, to string }{
		{"the row's rule deleted", row, `.m1312-control-deleted {`},
		{"the phone's one column dropped", "@apply flex-col items-start gap-y-1", "@apply items-start gap-y-1"},
		{"the values not breaking", "py-3.5 [overflow-wrap:anywhere]", "py-3.5"},
		{"the tablet's indent dropped", " md:pl-[206px]", ""},
		{"the time not pulled back", " md:-ml-[206px]", ""},
		{"the time breaking", "@apply whitespace-nowrap text-[13px]", "@apply text-[13px]"},
		{"the kind not alone on its line", " md:basis-full", ""},
		{"the phone's 6 px under the kind back to 4", "@apply mb-0.5 text-[15px]", "@apply text-[15px]"},
		{"the facts' face dropped", "@apply font-display text-ink/70", "@apply text-ink/70"},
		{"the values' ink reaching the link", "> :nth-child(n+3) > :not(a) {", "> :nth-child(n+3) > * {"},
		{"the phone's centred word dropped", " max-md:items-center", ""},
		{"the link inline", "md:relative md:inline-block md:min-h-0", "md:relative md:inline md:min-h-0"},
		{"the overlay deleted", auditLinkOverlay, ""},
		{"a tenth rule", kind, `[aria-labelledby="audit-list"] .op-row > span { @apply text-xs; }` + "\n  " + kind},
		{"a tenth rule, single-quoted", kind, `[aria-labelledby='audit-list'] .op-row { @apply py-0; }` + "\n  " + kind},
		{"a tenth rule, unquoted", kind, `[aria-labelledby=audit-list] .op-row > span { @apply hidden; }` + "\n  " + kind},
	} {
		mutated := strings.Replace(css, c.from, c.to, 1)
		if mutated == css {
			t.Fatalf("CONTROL %s: %q is not in input.css", c.what, c.from)
		}
		if len(readAuditRowRules(mutated)) == 0 {
			t.Errorf("CONTROL: %s is not a finding; the reading is blind", c.what)
		}
	}
}
