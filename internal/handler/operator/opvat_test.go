package operator_test

// opvat_test.go -- M13-11: the VAT re-check in the redesign. docs/plan/m13-operator-redesign.md,
// card M13-11; docs/design/operator-redesign README §6.1 "VAT On file", screens.md §9.

import (
	"context"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// The VAT screen's blocks, each a whole tag (or a tag's opening). The intro card and the asking
// card space their blocks (README §6 "Card": gap-3.5 on the markup, no mt-* in the bodies); the
// docket keeps its own rhythm. On the docket the term list is one column on the phone and, from
// the tablet, two: the number's as wide as the number (never wider than the row leaves it, so a
// long number breaks rather than pushing the chip out) and the verdict's, never narrower than
// the chip (a chip does not wrap its word), 56 px apart. The sentence and the time line are Space
// Grotesk (decision (f)), the time line's word at ink 70 %.
const (
	vatCard        = `<section class="op-card flex flex-col gap-3.5">`
	vatLabel       = `<p class="op-label">VAT number</p>`
	vatH1          = `<h1 class="font-display text-[28px] font-bold leading-[1.1] tracking-[-0.03em]">VAT re-check</h1>`
	vatIntro       = `<p class="text-sm">The VAT number this business registered with, and the answer of the EU VAT register (VIES) on file for it.</p>`
	vatLinks       = `<p class="flex flex-col items-start text-sm md:flex-row md:flex-wrap md:gap-x-6">`
	vatDocket      = `<section class="docket" aria-labelledby="tenant-vat"><h2 id="tenant-vat" class="docket-label">On file</h2>`
	vatFacts       = `<dl class="mt-3 flex flex-col gap-3 md:grid md:grid-cols-[minmax(0,max-content)_minmax(max-content,1fr)] md:gap-x-14">`
	vatNumberFact  = `<div><dt class="docket-label">VAT number</dt><dd class="mt-1 break-all font-mono text-lg font-bold">`
	vatAnswerFact  = `<div><dt class="docket-label">VIES answer</dt><dd class="mt-1.5">`
	vatSentenceTag = `<p class="mt-3 font-display">`
	vatTimeTag     = `<p class="mt-2 font-display text-ink/70">`
	vatAskCard     = `<section class="op-card flex flex-col gap-3.5" aria-labelledby="tenant-vat-ask"><h2 id="tenant-vat-ask" class="docket-label">Ask again</h2>`
	vatAskRow      = `<div class="flex flex-col gap-3.5 md:flex-row md:items-center md:gap-6">`
	vatAskSentence = `<p class="text-sm md:max-w-[560px]">Sends the number above to VIES now. If VIES answers, its answer replaces the one on file ` +
		`and is written to this tenant's own audit log; if it does not answer, nothing changes.</p>`
	vatNotAskable = `<p class="text-sm">` + sentenceVATNotAskable + `</p>`
)

// vatScreenState is what one render of the VAT screen must show.
type vatScreenState struct {
	overview, vatPath string
	number            string // as stored, unescaped
	chip, label       string // the tone's class and the word
	sentence          string
	time              string // the time line's inner HTML, whole
	askable           bool
	heading, notice   string // "" for no notice
}

// betweenTags drops the white space between two tags, the only place a render and the expected
// column may differ in it.
var betweenTags = regexp.MustCompile(`>\s+<`)

// vatColumn is the column s must render, whole: the intro card, the notice when there is one,
// the docket of the record, the asking card -- the form when the number is askable, the sentence
// in its place when it is not.
func vatColumn(s vatScreenState) string {
	var b strings.Builder
	b.WriteString(vatCard + vatLabel + vatH1 + vatIntro + vatLinks +
		`<a href="` + s.overview + `" class="op-link">Back to the overview</a> <a href="/operator/tenants" class="op-link">Back to the tenants</a></p></section>`)
	if s.notice != "" {
		b.WriteString(`<section role="status" class="op-notice op-notice--warn"><p class="font-display font-bold tracking-tight">` +
			html.EscapeString(s.heading) + `</p><div class="mt-1 text-sm text-ink/85"><p>` + html.EscapeString(s.notice) + `</p></div></section>`)
	}
	b.WriteString(vatDocket + vatFacts + vatNumberFact + html.EscapeString(s.number) + `</dd></div>` + vatAnswerFact +
		`<span class="tally ` + s.chip + `">` + html.EscapeString(s.label) + `</span></dd></div></dl>` +
		vatSentenceTag + html.EscapeString(s.sentence) + `</p>` + vatTimeTag + s.time + `</p></section>`)
	b.WriteString(vatAskCard)
	if s.askable {
		b.WriteString(vatAskRow + vatAskSentence + vatForm(strings.TrimSuffix(strings.TrimPrefix(s.vatPath, "/operator/tenants/"), "/vat")) + `</div>`)
	} else {
		b.WriteString(vatNotAskable)
	}
	b.WriteString(`</section>`)
	return betweenTags.ReplaceAllString(b.String(), "><")
}

// vatScreenFindings is TestVATScreen_IsTheIntroCardTheRecordAndTheAsking's reading of one render:
// one finding per way it departs from s.
func vatScreenFindings(name, body string, s vatScreenState) []string {
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
	inner := betweenTags.ReplaceAllString(strings.TrimSpace(column[len(open):len(column)-len("</main>")]), "><")

	// The column, whole.
	if want := vatColumn(s); inner != want {
		at := 0
		for at < len(inner) && at < len(want) && inner[at] == want[at] {
			at++
		}
		bad("the column departs from the expected one at byte %d:\n  got  …%s\n  want …%s", at,
			inner[at:min(len(inner), at+160)], want[at:min(len(want), at+160)])
	}

	// The two cards space their blocks: no element in them carries a top margin.
	for _, card := range regexp.MustCompile(`<section class="op-card[^"]*"[^>]*>.*?</section>`).FindAllString(inner, -1) {
		for _, m := range classAttr.FindAllStringSubmatch(card, -1) {
			for _, c := range strings.Fields(m[1]) {
				if topMargin.MatchString(c) {
					bad("%q in a card carries a top margin; the card spaces its blocks", m[1])
				}
			}
		}
	}

	// CLAUDE.md §7 and §4.5 exception (1): the number is on the page once, as text in its fact --
	// not in the title, a link, the form's action or a field; the form carries no field, hidden
	// or not; one form when the number is askable, none when it is not; no success notice.
	if n := strings.Count(body, html.EscapeString(s.number)); n != 1 {
		bad("the number is on the page %d time(s), want once (its fact)", n)
	}
	forms, buttons := 1, 1 // the bar's sign-out
	if s.askable {
		forms, buttons = 2, 2
	}
	if strings.Count(body, "<form") != forms || strings.Count(body, "<button") != buttons {
		bad("%d form(s) and %d button(s) on the page, want %d and %d", strings.Count(body, "<form"), strings.Count(body, "<button"), forms, buttons)
	}
	for _, banned := range []string{"<input", "<select", "<textarea", "contenteditable", "op-notice--ok", "op-notice--alert"} {
		if strings.Contains(body, banned) {
			bad("the page carries %q", banned)
		}
	}
	// Nothing hidden and nothing beside what screens.md §9 lists, in the column.
	for _, banned := range []string{"hidden", "sr-only", "aria-hidden", "<svg", "<img", "<canvas", "<picture", "<table", "<script",
		"<iframe", "tabindex", "hx-", "readonly", "disabled", `role="alert"`, ` name="`} {
		if strings.Contains(column, banned) {
			bad("the column carries %q", banned)
		}
	}
	if n := strings.Count(body, `role="status"`); (s.notice != "") != (n == 1) || n > 1 {
		bad("%d status region(s) on the page; want one exactly when a POST changed nothing", n)
	}
	return f
}

// TestVATScreen_IsTheIntroCardTheRecordAndTheAsking (M13-11; screens.md §9, README §6.1 "VAT On
// file"): each VAT-screen state -- screens()'s six "tenant VAT" renders (the four verdicts, the
// three time lines among them; never asked; a notice; a number VIES does not take, no form; an
// unnamed tenant) and a render here of long values (a 64-character number with no space under a
// 130-character tenant name with none, a notice, the last-answer line) -- is the frame's column
// holding exactly vatColumn's blocks, whole, in that order, each tag with its classes: the intro
// card (op-label "VAT number", the h1 "VAT re-check" at 28 px, line height 1.1, -0.03em, the
// intro, the links' row with Back to the overview and Back to the tenants); the warn notice
// only when a POST changed nothing; the docket "On file" -- the term list (one column on the
// phone, two from the tablet: minmax(0,max-content) and minmax(max-content,1fr), 56 px apart),
// the number's pinned <dd class="mt-1 break-all font-mono text-lg font-bold">, the verdict's
// chip, the sentence
// and the time line in Space Grotesk, the time in its mono span; the asking card "Ask again" --
// the sentence and the form (method before action, no field, only "Ask VIES again") in one row
// from the tablet, or the not-askable sentence and no form. The cards' elements carry no top
// margin. The page: the number once (not in the title, a link, an action or a field); the
// sign-out's form plus exactly one form when askable, none otherwise; no input, select,
// textarea, editable text, ok or alert notice; in the column nothing hidden (a
// hidden attribute or class, sr-only, aria-hidden), no drawing, table, script, frame, tabindex,
// hx-, readonly or disabled control, named field (the page's head names its meta tags); one
// status region exactly when there is a notice.
// CONTROLS, each read by the same function and each a finding: a word of the intro changed; the
// intro given back a top margin; the number's pinned dd changed; the term list's tablet columns
// dropped; the time out of its mono span; the sentence hidden from a reader; a hidden field with
// the number in the form; a text field for the number; the form's attributes reordered; a form
// on the number VIES does not take; the number in the title; a success notice.
//
// PART II -- red on any of the above. PART III -- what the blocks look like at each width is the
// screenshot round's (320 / 390 / 834 / 1200 / 1440 px) and the time line's ink
// TestVATRecord_TheScopedRuleKeepsTheTimeInInk's; the handler's words and sentences, the server's
// number and the logs are the op16 tests' -- no completeness claim.
func TestVATScreen_IsTheIntroCardTheRecordAndTheAsking(t *testing.T) {
	all := screens(t)
	const overview = "/operator/tenants/10000000-0000-4000-8000-000000000001"
	asked := `Asked <span class="font-mono">2026-10-07 14:03 UTC</span>`
	// screens()'s six renders (op8_test.go), restated: the reading is checked against them.
	base := vatScreenState{overview: overview, vatPath: overview + "/vat", number: "MT12345678", chip: "tally--vat-confirmed",
		label: "Confirmed", sentence: "S", time: asked, askable: true}
	never, noAnswer, notFound, odd, notice := base, base, base, base, base
	never.chip, never.label, never.time = "tally--vat-not-checked", "Not checked", "No time of asking is on file."
	noAnswer.chip, noAnswer.label = "tally--vat-no-answer", "No answer"
	notFound.chip, notFound.label = "tally--vat-not-found", "Not found"
	odd.number, odd.askable = "MT1234", false
	notice.heading, notice.notice = "VIES did not answer", "N"
	states := map[string]vatScreenState{
		"tenant VAT": base, "tenant VAT, never asked": never, "tenant VAT, no answer": noAnswer,
		"tenant VAT, not found": notFound, "tenant VAT, odd number": odd, "tenant VAT, a notice": notice,
	}
	for name := range all {
		if _, ok := states[name]; strings.HasPrefix(name, "tenant VAT") && !ok {
			t.Fatalf("PREMISE: screens() has a VAT render %q this test does not read", name)
		}
	}
	long := vatScreenState{overview: "/operator/tenants/x", vatPath: "/operator/tenants/x/vat", number: strings.Repeat("MT1234567", 7) + "X",
		chip: "tally--vat-not-found", label: "Not found", sentence: "VIES did not recognise this number when it last answered.",
		time: `Last answer <span class="font-mono">2026-10-20 09:14 UTC</span>`, askable: true,
		heading: "Too many VAT re-checks", notice: sentenceVATBudgetNotice}
	var b strings.Builder
	if err := operatorpages.TenantVAT(operatorpages.TenantVATView{Name: mustName(t, strings.Repeat("W", 130)), ID: "x",
		OverviewPath: long.overview, VATPath: long.vatPath, Tabs: fixtureTabs, Number: long.number, Label: long.label,
		Sentence: long.sentence, Tone: operatorpages.VATToneNotFound, AskedAt: "2026-10-20 09:14 UTC", Answered: true, Askable: true,
		NoticeHeading: long.heading, Notice: long.notice}).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	all["long values"] = b.String()
	states["long values"] = long
	for name, s := range states {
		body, ok := all[name]
		if !ok {
			t.Fatalf("PREMISE: screens() has no %q", name)
		}
		for _, finding := range vatScreenFindings(name, body, s) {
			t.Error(finding)
		}
	}

	page, oddPage := all["tenant VAT"], all["tenant VAT, odd number"]
	form := vatForm("10000000-0000-4000-8000-000000000001")
	for _, c := range []struct {
		what, page, from, to string
		s                    vatScreenState
	}{
		{"a word of the intro changed", page, "on file for it.", "on record for it.", base},
		{"the intro's top margin back", page, vatIntro, strings.Replace(vatIntro, `class="`, `class="mt-3 `, 1), base},
		{"the number's dd changed", page, vatNumberFact, strings.Replace(vatNumberFact, `class="mt-1 `, `class="`, 1), base},
		{"the tablet's columns dropped", page, vatFacts, strings.Replace(vatFacts, " md:grid md:grid-cols-[minmax(0,max-content)_minmax(max-content,1fr)]", "", 1), base},
		{"the time out of its mono span", page, asked, `Asked <span>2026-10-07 14:03 UTC</span>`, base},
		{"the sentence hidden from a reader", page, vatSentenceTag, `<p aria-hidden="true" class="mt-3 font-display">`, base},
		{"a hidden field with the number", page, form, strings.Replace(form, "<button", `<input type="hidden" name="vat_number" value="MT12345678"><button`, 1), base},
		{"a text field for the number", page, vatAskSentence, vatAskSentence + `<label>VAT number <input type="text" value="MT12345678"></label>`, base},
		{"the form's attributes reordered", page, `<form method="post" action="` + overview + `/vat"`, `<form action="` + overview + `/vat" method="post"`, base},
		{"a form on a number VIES does not take", oddPage, vatNotAskable, vatNotAskable + form, odd},
		{"the number in the title", page, "<title>VAT number — ", "<title>MT12345678 — VAT number — ", base},
		{"a success notice", page, vatDocket, `<section role="status" class="op-notice op-notice--ok"><p class="font-display font-bold tracking-tight">Checked.</p></section>` + vatDocket, base},
	} {
		mutated := strings.Replace(c.page, c.from, c.to, 1)
		if mutated == c.page {
			t.Fatalf("CONTROL %s: %q is not on the page", c.what, c.from)
		}
		if len(vatScreenFindings("control", mutated, c.s)) == 0 {
			t.Errorf("CONTROL: %s is not a finding; the reading is blind", c.what)
		}
	}
}

// vatRecordRules is the VAT record's one scoped rule in web/static/css/input.css (README §6.1
// "VAT On file"): the time line's time in ink, its word at the paragraph's ink 70 %.
var vatRecordRules = map[string][]string{
	`[aria-labelledby="tenant-vat"] > p > span`: {"text-ink"},
}

// TestVATRecord_TheScopedRuleKeepsTheTimeInInk (M13-11; README §6.1 "VAT On file", boards OpVat,
// T-OpVat, M-OpVat): in web/static/css/input.css (comments stripped, read as flat `selectors {
// @apply …; }` rules by readScopedRules), every rule whose selector names the VAT docket's
// heading id (and so the asking card's, whose id begins with it) is vatRecordRules' one, exactly
// once, one @apply of exactly its utility. CONTROLS, each read by the same function and each a
// finding: the rule deleted (the time at the word's ink 70 %); its tone changed; a second rule on
// the docket, in the double-quoted, the single-quoted and the unquoted spelling; a rule on the
// asking card.
//
// PART II -- red on any of the above. PART III -- what the rule draws is the screenshot round's;
// the paragraph's own tone and face are TestVATScreen_IsTheIntroCardTheRecordAndTheAsking's (the
// markup) -- no completeness claim.
func TestVATRecord_TheScopedRuleKeepsTheTimeInInk(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	for _, f := range readScopedRules(css, "tenant-vat", vatRecordRules) {
		t.Error(f)
	}
	const rule = `[aria-labelledby="tenant-vat"] > p > span {`
	for _, c := range []struct{ what, from, to string }{
		{"the rule deleted", rule, `.m1311-control-deleted {`},
		{"its tone changed", rule + " @apply text-ink; }", rule + " @apply text-ink/70; }"},
		{"a second rule", rule, `[aria-labelledby="tenant-vat"] dl { @apply md:grid-cols-1; }` + "\n  " + rule},
		{"a second rule, single-quoted", rule, `[aria-labelledby='tenant-vat'] > p { @apply text-xs; }` + "\n  " + rule},
		{"a second rule, unquoted", rule, `[aria-labelledby=tenant-vat] dd { @apply break-normal; }` + "\n  " + rule},
		{"a rule on the asking card", rule, `[aria-labelledby="tenant-vat-ask"] form { @apply hidden; }` + "\n  " + rule},
	} {
		mutated := strings.Replace(css, c.from, c.to, 1)
		if mutated == css {
			t.Fatalf("CONTROL %s: %q is not in input.css", c.what, c.from)
		}
		if len(readScopedRules(mutated, "tenant-vat", vatRecordRules)) == 0 {
			t.Errorf("CONTROL: %s is not a finding; the reading is blind", c.what)
		}
	}
}
