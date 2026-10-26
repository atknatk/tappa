package operator_test

// opbilling_test.go -- M13-10: the billing months in the redesign. docs/plan/m13-operator-redesign.md,
// card M13-10; docs/design/operator-redesign README §6 "Billing receipt strip", "Pager", §6.1
// "Billing row", screens.md §8.

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

// The billing screen's card, docket and pager and the pieces of them screens.md §8 names, each a
// whole tag. The card spaces its blocks (README §6 "Card": gap-3.5 in .op-card's rule, no mt-* in the
// bodies); the h1 is the board's 26 / 28 / 32 px (orchestrator decision (e)). A month with figures
// writes them as the receipt strip (a term list of four cells) and its dated facts in one wrapper
// paragraph; the pager's buttons are op-pager, each form half the row on the phone.
const (
	billingCard   = `<section class="op-card">`
	billingLabel  = `<p class="op-label">Billing</p>`
	billingH1     = `<h1 class="font-display text-[26px] font-bold leading-[1.1] tracking-[-0.03em] md:text-[28px] desk:text-[32px]">Billing months</h1>`
	billingIntro  = `<p class="text-[15px] leading-[1.55]">`
	billingNote   = `<p class="text-sm text-ink/70">`
	billingLinks  = `<p class="flex flex-col items-start text-sm md:flex-row md:flex-wrap md:gap-x-6">`
	billingFacts  = `<p class="flex w-full flex-col gap-0.5 font-display text-ink/70 md:flex-row md:flex-wrap md:gap-x-[22px] md:gap-y-1">`
	billingOldest = `<p class="mt-1.5 border-t-2 border-dashed border-line pt-3 font-display text-[13px] text-ink/70">`
	billingPagerN = `<nav class="flex flex-wrap gap-2.5 md:gap-3 max-md:*:grid max-md:*:flex-1" aria-label="Pages of the billing months">`
)

// billingChipClass is the tone class billingChip writes for a tone (the brand's fixed mapping; a
// tone no case names draws the unreadable chip).
var billingChipClass = map[operatorpages.BillingTone]string{
	operatorpages.BillingToneFrozen: "tally--frozen", operatorpages.BillingToneRunning: "tally--running",
	operatorpages.BillingToneUnclosed: "tally--unclosed", operatorpages.BillingToneBeforeSignup: "tally--before-signup",
}

// billingWantRow is one month exactly as screens.md §8 lists it: the month, the chip, the sentence;
// with figures, the receipt strip of the four (Plan · People counted · Price per person · Amount),
// the dated facts ([Closed] · [First charged month] · Counted over) in their wrapper, then the
// free-window and the floor sentences; without, nothing more -- no strip (§4.6).
func billingWantRow(r operatorpages.BillingRow) string {
	class, ok := billingChipClass[r.Tone]
	if !ok {
		class = "tally--unreadable"
	}
	e := html.EscapeString
	s := `<li class="op-row"><span class="font-mono font-bold">` + e(r.Month) + `</span><span class="tally ` + class + `">` +
		e(r.Label) + `</span><span class="w-full text-sm">` + e(r.Sentence) + `</span>`
	if r.HasFigures {
		s += `<dl class="op-receipt">` + receiptCell("Plan", r.Plan) + receiptCell("People counted", r.People) +
			receiptCell("Price per person", r.UnitPrice) + receiptCell("Amount", r.Amount) + `</dl>` + billingFacts
		var facts []string
		if r.ClosedAt != "" {
			facts = append(facts, fact("Closed", r.ClosedAt))
		}
		if r.FirstCharged != "" {
			facts = append(facts, fact("First charged month", r.FirstCharged))
		}
		s += strings.Join(append(facts, fact("Counted over", r.Period)), " ") + `</p>`
		if r.Free {
			s += `<span class="w-full text-sm">Inside the founding offer's free window: the amount is zero because of the offer, not ` +
				`because nobody was counted.</span>`
		}
		if r.Floor != "" {
			s += `<span class="w-full text-sm">The count is a FLOOR: <span class="font-mono">` + e(r.Floor) + `</span> employee records ` +
				`of this business disagree with their own dates and are never counted, so the real count may be higher. That number ` +
				`is an upper bound on the effect, not the amount missing.</span>`
		}
	}
	return s + `</li>`
}

// billingWantColumn is the whole content column of v: the card, the docket of v's rows with the
// oldest-page note on the last page, and the pager with the ways v offers.
func billingWantColumn(v operatorpages.TenantBillingView) string {
	s := billingCard + billingLabel + billingH1 + billingIntro + `<span class="font-mono">` + strconv.Itoa(v.MonthsPerPage) +
		`</span> of this tenant's own months per page, newest first, each in the time zone it is counted in. These are the ` +
		`tenant's own figures: the arithmetic of its billing screen, read when this page was opened.</p>` + billingNote +
		`Nothing on this page closes, changes or charges a month. A count is all a month keeps: no person is listed, here or ` +
		`anywhere a month links.</p>` + billingLinks + `<a href="` + v.OverviewPath + `" class="op-link">Back to the overview</a> ` +
		`<a href="/operator/tenants" class="op-link">Back to the tenants</a></p></section>`
	s += `<section class="docket" aria-labelledby="tenant-billing"><h2 id="tenant-billing" class="docket-label">Newest first · page ` +
		`<span class="font-mono">` + strconv.Itoa(v.Page) + `</span> of <span class="font-mono">` + strconv.Itoa(v.LastPage) +
		`</span></h2><ol>`
	for _, r := range v.Rows {
		s += billingWantRow(r)
	}
	s += `</ol>`
	if v.Page == v.LastPage {
		s += billingOldest + `This is the oldest page this screen shows: <span class="font-mono">` +
			strconv.Itoa(v.LastPage*v.MonthsPerPage) + `</span> months, the tenant's own history depth.</p>`
	}
	s += `</section>`
	if v.Page > 1 || v.Page < v.LastPage {
		s += billingPagerN
		form := func(page int, label string) string {
			return `<form method="post" action="` + v.BillingPath + `"><input type="hidden" name="page" value="` + strconv.Itoa(page) +
				`"><button type="submit" class="op-pager">` + label + `</button></form>`
		}
		if v.Page > 1 {
			s += form(v.Page-1, "Newer months")
		}
		if v.Page < v.LastPage {
			s += form(v.Page+1, "Older months")
		}
		s += `</nav>`
	}
	return s
}

// tight is markup with every run of white space made one space and the space between two tags
// dropped, so the comparison reads the tags and the words, not the template's indentation.
func tight(s string) string {
	return regexp.MustCompile(`>\s+<`).ReplaceAllString(spaced(s), "><")
}

// billingScreenFindings is TestBillingScreen_IsTheIntroCardAndTheDocket's reading of one render
// of v: one finding per way it departs from what billingWantColumn says.
func billingScreenFindings(name, body string, v operatorpages.TenantBillingView) []string {
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
	if want := tight(billingWantColumn(v)); inner != want {
		at := 0
		for at < len(inner) && at < len(want) && inner[at] == want[at] {
			at++
		}
		lo := max(at-80, 0)
		bad("the column departs from screens.md §8 at byte %d:\n  got  …%s\n  want …%s", at, inner[lo:min(at+120, len(inner))],
			want[lo:min(at+120, len(want))])
	}

	// §4.6: a month with no figure carries no strip and no figure at all -- not even a zero.
	rows := regexp.MustCompile(`<li class="op-row">.*?</li>`).FindAllString(inner, -1)
	if len(rows) != len(v.Rows) {
		bad("%d row(s), want %d", len(rows), len(v.Rows))
	} else {
		for i, r := range rows {
			strip := strings.Count(r, `<dl class="op-receipt">`) == 1 && strings.Count(r, `<div><dt class="docket-label">`) == 4
			if v.Rows[i].HasFigures != strip {
				bad("row %d (%s): a strip of four cells = %v, figures = %v", i+1, v.Rows[i].Month, strip, v.Rows[i].HasFigures)
			}
			if !v.Rows[i].HasFigures && (strings.Contains(r, "<dl") || strings.Contains(r, "€") || strings.Contains(r, "0.00")) {
				bad("row %d (%s), with no figure, carries a strip or a figure", i+1, v.Rows[i].Month)
			}
		}
	}

	// The page's forms are the bar's sign-out and the pager's; its buttons the same; no hx-.
	ways := 0
	if v.Page > 1 {
		ways++
	}
	if v.Page < v.LastPage {
		ways++
	}
	if strings.Count(body, "<form") != 1+ways || strings.Count(body, "<button") != 1+ways ||
		strings.Count(body, `<button type="submit" class="op-pager">`) != ways || strings.Contains(body, "hx-") {
		bad("%d form(s), %d button(s) or an hx- attribute on the page; want the sign-out and %d pager way(s)",
			strings.Count(body, "<form"), strings.Count(body, "<button"), ways)
	}
	for _, label := range []string{"Older months", "Newer months"} {
		if strings.Contains(body, `class="op-link">`+label+`</button>`) {
			bad("%s is still an op-link button", label)
		}
	}
	// Nothing hidden and nothing screens.md §8 does not show. The pager's page fields are the
	// column's only inputs and are read out first.
	scan := regexp.MustCompile(`<input type="hidden" name="page" value="[1-5]">`).ReplaceAllString(column, "")
	for _, banned := range []string{"hidden", "sr-only", "aria-hidden", "<svg", "<img", "<canvas", "<picture", "<input", "<select",
		"<textarea", "<table", "<script", "<iframe", "role=", "tabindex", "download", "<bdi"} {
		if strings.Contains(scan, banned) {
			bad("the column carries %q", banned)
		}
	}
	text := html.UnescapeString(noticeTag.ReplaceAllString(column, " "))
	if m := regexp.MustCompile(`(?i)\b(?:invoices?|pdf|csv|export|print|freeze|unfreeze|close (?:this|the) month|employees? list|people list)\b`).FindString(text); m != "" {
		bad("the column says %q (screens.md §8 Not shown: a person, an invoice PDF, a payment status, a CSV, a close or freeze control)", m)
	}
	if found := verdictsIn(text); len(found) != 0 {
		bad("the column carries the verdict word(s) %q", found)
	}
	return f
}

// TestBillingScreen_IsTheIntroCardAndTheDocket (M13-10; screens.md §8, README §6 "Billing receipt
// strip", "Pager", §6.1 "Billing row"): each billing-screen state -- screens()'s four "tenant
// billing" renders (a row of every tone; page 1, page 3 with both ways, the last page with the
// oldest-page note, an unnamed tenant) and a render here of long values (a 87-character plan,
// a five-digit count, six-digit amounts, a long zone, a month with both a closing and a first charged
// month, the free window and the floor on one row, the tone no case names) -- is the frame's
// column holding exactly, tags and words, billingWantColumn: the card (the op-label, the h1 at
// 26 / 28 / 32 px, the intro with its count in the data face, the note, the two links; no top
// margin), the docket (its pinned heading, then each row as billingWantRow: the month, the chip
// of the tone's class, the sentence, and only for a month with figures the receipt strip -- a
// <dl class="op-receipt"> of the four cells, each a docket-label term over a mono value, in
// screens.md's order -- the dated facts' wrapper and the free-window and floor sentences; the
// oldest-page note on the last page) and the pager (the nav, its forms posting the page to the
// screen's path with the hidden page field, each button an op-pager). A row with no figure has no
// strip and no figure (§4.6: not even a zero). The page: the bar's sign-out and the pager's ways
// are its forms and buttons, the op-pager buttons exactly the ways; no pager button is op-link;
// no hx-. Nowhere in the column (the pager's page fields read out first): hidden content (a hidden
// attribute or class, sr-only, aria-hidden), a drawing or image, a control but the pager's, a
// table, a script, a frame, a download, a person's name (a bdi), the words of what §8 does not
// show (an invoice, a PDF, a CSV, an export, a print, a freeze or close control, a list of people)
// or a payment verdict (verdictWords). CONTROLS, each read by the same function and each a
// finding: a word of the card changed; the note given back its top margin; a figure back to its
// inline span; a strip cell's value not mono; a strip on a month before sign-up; the dated facts'
// wrapper dropped; aria-hidden on a fact; a pager button back to op-link; a third pager form; the
// receipt's labels moved under their values.
//
// PART II -- red on any of the above. PART III -- what the screen looks like at each width is the
// screenshot round's (320 / 390 / 834 / 1200 / 1440 px), the row's and the strip's rules
// TestBillingRow_TheScopedRulesKeepTheBoardsReceipt's; the handler's words, states and figures are
// the op12 tests' -- no completeness claim.
func TestBillingScreen_IsTheIntroCardAndTheDocket(t *testing.T) {
	all := screens(t)
	const tenantPath = "/operator/tenants/10000000-0000-4000-8000-000000000001"
	// screens()'s five rows (op8_test.go), restated: the reading is checked against them.
	fixture := []operatorpages.BillingRow{
		{Month: "October 2026", Label: "Live — month running", Sentence: "S", Tone: operatorpages.BillingToneRunning, HasFigures: true,
			Plan: "founding", People: "3", UnitPrice: "€1.10", Amount: "€3.30", FirstCharged: "September 2026",
			Period: "2026-10-01 00:00 to 2026-11-01 00:00 Europe/Malta", Floor: "1"},
		{Month: "September 2026", Label: "Ended, not closed by the business", Sentence: "S", Tone: operatorpages.BillingToneUnclosed,
			HasFigures: true, Plan: "founding", People: "2", UnitPrice: "€1.10", Amount: "€2.20", FirstCharged: "September 2026",
			Period: "2026-09-01 00:00 to 2026-10-01 00:00 Europe/Malta"},
		{Month: "August 2026", Label: "Frozen", Sentence: "S", Tone: operatorpages.BillingToneFrozen, HasFigures: true,
			Plan: "founding", People: "2", UnitPrice: "€1.10", Amount: "€0.00", Free: true, ClosedAt: "2026-09-02 12:30 Europe/Malta",
			Period: "2026-08-01 00:00 to 2026-09-01 00:00 Europe/Malta"},
		{Month: "March 2026", Label: "Before sign-up", Sentence: "S", Tone: operatorpages.BillingToneBeforeSignup},
		{Month: "February 2026", Label: "Unreadable", Sentence: "S"},
	}
	view := func(page int) operatorpages.TenantBillingView {
		return operatorpages.TenantBillingView{OverviewPath: tenantPath, BillingPath: tenantPath + "/billing", Page: page, LastPage: 5,
			MonthsPerPage: 12, Rows: fixture}
	}
	states := map[string]operatorpages.TenantBillingView{
		"tenant billing": view(1), "tenant billing, page 3": view(3), "tenant billing, the last": view(5),
		"tenant billing, unnamed": view(1),
	}
	for name := range all {
		if _, ok := states[name]; strings.HasPrefix(name, "tenant billing") && !ok {
			t.Fatalf("PREMISE: screens() has a billing render %q this test does not read", name)
		}
	}
	zone := " America/Argentina/ComodRivadavia"
	long := operatorpages.TenantBillingView{Name: mustName(t, strings.Repeat("Kebab Factory Holdings ", 6)), ID: "x",
		OverviewPath: "/operator/tenants/x", BillingPath: "/operator/tenants/x/billing", Tabs: fixtureTabs, Page: 2, LastPage: 5,
		MonthsPerPage: 12, Rows: []operatorpages.BillingRow{
			{Month: "September 2026", Label: "Ended, not closed by the business", Sentence: "This month is over & <not> closed.",
				Tone: operatorpages.BillingToneUnclosed, HasFigures: true, Plan: "founding-partner-programme-" + strings.Repeat("x", 60),
				People: "99999", UnitPrice: "€12345.67", Amount: "€123456.78", FirstCharged: "September 2026", ClosedAt: "2026-10-02 12:30" + zone,
				Period: "2026-09-01 00:00 to 2026-10-01 00:00" + zone, Free: true, Floor: "99999"},
			{Month: "August 2026", Label: "Frozen", Sentence: "F", Tone: operatorpages.BillingToneFrozen, HasFigures: true, Plan: "founding",
				People: "99999", UnitPrice: "€2.50", Amount: "€249997.50", ClosedAt: "2026-09-02 12:30" + zone,
				Period: "2026-08-01 00:00 to 2026-09-01 00:00" + zone},
			{Month: "July 2026", Label: "Before sign-up", Sentence: "B", Tone: operatorpages.BillingToneBeforeSignup},
			{Month: "A month with no date", Label: "Unreadable", Sentence: "U", Tone: operatorpages.BillingTone(99)},
		}}
	var b strings.Builder
	if err := operatorpages.TenantBilling(long).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	all["long values"] = b.String()
	states["long values"] = long
	for name, v := range states {
		body, ok := all[name]
		if !ok {
			t.Fatalf("PREMISE: screens() has no %q", name)
		}
		for _, finding := range billingScreenFindings(name, body, v) {
			t.Error(finding)
		}
	}

	page := all["tenant billing, page 3"]
	amount := receiptCell("Amount", "€3.30")
	closed := fact("Closed", "2026-09-02 12:30 Europe/Malta")
	older := `<button type="submit" class="op-pager">Older months</button>`
	for _, c := range []struct{ what, from, to string }{
		{"a word of the card changed", "A count is all a month keeps", "A count is what a month keeps"},
		{"the note's top margin back", billingNote, strings.Replace(billingNote, `class="`, `class="mt-1 `, 1)},
		{"a figure back to its inline span", amount, `<span class="text-sm">Amount <span class="font-mono">€3.30</span></span>`},
		{"a strip value not mono", amount, strings.Replace(amount, `<dd class="font-mono">`, `<dd>`, 1)},
		{"a strip before sign-up", `>March 2026</span><span class="tally tally--before-signup">Before sign-up</span><span class="w-full text-sm">S</span>`,
			`>March 2026</span><span class="tally tally--before-signup">Before sign-up</span><span class="w-full text-sm">S</span><dl class="op-receipt"></dl>`},
		{"the facts' wrapper dropped", billingFacts + closed, closed},
		{"a fact hidden from a reader", closed, strings.Replace(closed, `<span class="text-sm">`, `<span aria-hidden="true" class="text-sm">`, 1)},
		{"a pager button back to op-link", older, `<button type="submit" class="op-link">Older months</button>`},
		{"a third pager form", `</nav></main>`, `<form method="post" action="` + tenantPath + `/billing"><input type="hidden" name="page" value="5">` +
			`<button type="submit" class="op-pager">Oldest months</button></form></nav></main>`},
		{"the labels under their values", `<div><dt class="docket-label">Plan</dt><dd class="font-mono">founding</dd></div>`,
			`<div><dd class="font-mono">founding</dd><dt class="docket-label">Plan</dt></div>`},
	} {
		mutated := strings.Replace(page, c.from, c.to, 1)
		if mutated == page {
			t.Fatalf("CONTROL %s: %q is not on the page", c.what, c.from)
		}
		if len(billingScreenFindings("control", mutated, states["tenant billing, page 3"])) == 0 {
			t.Errorf("CONTROL: %s is not a finding; the reading is blind", c.what)
		}
	}
}

// billingRowRules is the billing docket's layout and type in web/static/css/input.css (README §6.1
// "Billing row"), each rule scoped from the docket's heading id, its selector and its utilities
// exactly: the heading's dashed rule and bold page numbers; the row's blocks 12 px apart, the
// month and chip 14 px, 18 px above and below (20 px from the tablet, the last row 6 px below),
// values breaking anywhere, and on the phone the chip at the head line's end; the month 16 px;
// the sentences in Space Grotesk; the strip the row's width; the dated facts' values in ink.
var billingRowRules = map[string][]string{
	`[aria-labelledby="tenant-billing"] > h2`:        {"border-b-2", "border-dashed", "border-line", "pb-3"},
	`[aria-labelledby="tenant-billing"] > h2 > span`: {"font-bold", "text-ink"},
	`[aria-labelledby="tenant-billing"] .op-row`: {"gap-x-3.5", "gap-y-3", "py-[18px]", "[overflow-wrap:anywhere]", "last:pb-1.5",
		"max-md:justify-between", "md:py-5"},
	`[aria-labelledby="tenant-billing"] .op-row > :first-child`:        {"text-base"},
	`[aria-labelledby="tenant-billing"] .op-row > span:nth-child(n+3)`: {"font-display"},
	`[aria-labelledby="tenant-billing"] .op-row > dl`:                  {"w-full"},
	`[aria-labelledby="tenant-billing"] .op-row > p > span > span`:     {"text-ink"},
}

// receiptRules is the receipt strip's component (README §6 "Billing receipt strip", built by
// M13-02 and first drawn here): two by two on the phone, four across from the tablet with the
// amount's column 1.2 times the others, 12 px 20 px between cells, a dashed rule above and below;
// the values 16 px, the amount's cell at the strip's end and its value 20 px bold.
var receiptRules = map[string][]string{
	`.op-receipt`: {"grid", "grid-cols-2", "gap-x-5", "gap-y-3", "border-y", "border-dashed", "border-line", "py-3",
		"md:grid-cols-[repeat(3,minmax(0,1fr))_minmax(0,1.2fr)]"},
	`.op-receipt dd`:                  {"text-base"},
	`.op-receipt > div:last-child`:    {"text-right"},
	`.op-receipt > div:last-child dd`: {"text-xl", "font-bold"},
}

// TestBillingRow_TheScopedRulesKeepTheBoardsReceipt (M13-10; README §6 "Billing receipt strip",
// §6.1 "Billing row", boards OpBilling, T-OpBilling, M-OpBilling): in web/static/css/input.css
// (comments stripped, read as flat `selectors { @apply …; }` rules by readScopedRules), every rule
// whose selector names the billing docket's heading id is one of billingRowRules' seven, and every
// rule whose selector names .op-receipt one of receiptRules' four, each exactly once, one @apply of
// exactly its utilities. CONTROLS, each read by the same function and each a finding: the row's
// rule deleted; the phone's chip at the line's end dropped; the values' break-anywhere dropped;
// the strip's width rule deleted; the heading's dash deleted; the strip four across on the phone;
// the tablet's four columns dropped; the amount not at the strip's end; the amount's 20 px
// dropped; an eighth rule on the docket, in the double-quoted, the single-quoted and the
// unquoted spelling; a fifth rule on the strip.
//
// PART II -- red on any of the above. PART III -- what the rules draw at each width is the
// screenshot round's; the markup is TestBillingScreen_IsTheIntroCardAndTheDocket's -- no
// completeness claim.
func TestBillingRow_TheScopedRulesKeepTheBoardsReceipt(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	read := func(css string) []string {
		return append(readScopedRules(css, "tenant-billing", billingRowRules), readScopedRules(css, ".op-receipt", receiptRules)...)
	}
	for _, f := range read(css) {
		t.Error(f)
	}
	const row, dl, head = `[aria-labelledby="tenant-billing"] .op-row {`, `[aria-labelledby="tenant-billing"] .op-row > dl {`,
		`[aria-labelledby="tenant-billing"] > h2 {`
	for _, c := range []struct{ what, from, to string }{
		{"the row's rule deleted", row, `.m1310-control-deleted {`},
		{"the phone's chip not at the line's end", " max-md:justify-between", ""},
		{"the values not breaking", "py-[18px] [overflow-wrap:anywhere]", "py-[18px]"},
		{"the strip's width deleted", dl, `.m1310-control-width {`},
		{"the heading's dash deleted", head, `.m1310-control-head {`},
		{"the strip four across on the phone", "grid grid-cols-2 gap-x-5", "grid grid-cols-4 gap-x-5"},
		{"the tablet's columns dropped", "md:grid-cols-[repeat(3,minmax(0,1fr))_minmax(0,1.2fr)]", ""},
		{"the amount not at the end", ".op-receipt > div:last-child { @apply text-right; }", ".op-receipt > div:last-child { @apply text-left; }"},
		{"the amount's size dropped", ".op-receipt > div:last-child dd { @apply text-xl font-bold; }", ".op-receipt > div:last-child dd { @apply font-bold; }"},
		{"an eighth rule", row, `[aria-labelledby="tenant-billing"] .op-row > p { @apply text-xs; }` + "\n  " + row},
		{"an eighth rule, single-quoted", row, `[aria-labelledby='tenant-billing'] .op-row { @apply py-0; }` + "\n  " + row},
		{"an eighth rule, unquoted", row, `[aria-labelledby=tenant-billing] .op-row > dl { @apply hidden; }` + "\n  " + row},
		{"a fifth rule on the strip", ".op-receipt dd {", ".op-receipt dt { @apply font-display; }\n  .op-receipt dd {"},
	} {
		mutated := strings.Replace(css, c.from, c.to, 1)
		if mutated == css {
			t.Fatalf("CONTROL %s: %q is not in input.css", c.what, c.from)
		}
		if len(read(mutated)) == 0 {
			t.Errorf("CONTROL: %s is not a finding; the reading is blind", c.what)
		}
	}
}
