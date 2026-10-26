package operator_test

// opplaques_test.go -- M13-09: the plaque inventory in the redesign. docs/plan/m13-operator-redesign.md,
// card M13-09; docs/design/operator-redesign README §6.1 "Plaque row", screens.md §7.

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

// The plaque screen's card and docket and the pieces of them screens.md §7 names, each a whole
// tag. The card spaces its blocks (README §6 "Card": gap-3.5 on the markup, no mt-* in the
// bodies). A row opens with the uid's span; the chip, the sentence and the facts stand in one
// wrapper after it, which draws no box on the phone (the uid and the chip share the head line)
// and wraps beside the uid's column from the tablet; each fact is a line of its own on the
// phone. Values that cannot break break anywhere (the wrapper's overflow-wrap).
const (
	plaquesCard    = `<section class="op-card flex flex-col gap-3.5">`
	plaquesLabel   = `<p class="op-label">Plaques</p>`
	plaquesH1      = `<h1 class="font-display text-[28px] font-bold leading-[1.1] tracking-[-0.03em]">Plaque inventory</h1>`
	plaquesSummary = `<p class="text-sm">`
	plaquesNote    = `<p class="text-xs leading-[1.45] text-ink/70">Read when this page was opened. Nothing on this page changes a plaque.</p>`
	plaquesLinks   = `<p class="flex flex-col items-start text-sm md:flex-row md:flex-wrap md:gap-x-6">`
	plaquesDocket  = `<section class="docket" aria-labelledby="tenant-plaques"><h2 id="tenant-plaques" class="docket-label">Plaques of this tenant</h2><ol class="mt-3">`
	plaqueBody     = `<div class="contents [overflow-wrap:anywhere] md:flex md:min-w-0 md:flex-wrap md:items-baseline md:gap-x-[22px] md:gap-y-1.5">`
	plaqueFact     = `<span class="basis-full text-sm md:basis-auto">`
)

// plaqueRowShape is one row, whole: the uid's span, then the wrapper holding the chip, the
// sentence and one or more facts -- a fact's value mono (an unnamed location's id also breaking
// anywhere) or a location's name in a bdi -- and nothing after the wrapper.
var plaqueRowShape = regexp.MustCompile(`^<li class="op-row"><span class="font-mono font-bold">[^<]+</span>` +
	regexp.QuoteMeta(plaqueBody) + `<span class="tally tally--[a-z]+">[^<]+</span><span class="w-full text-sm">[^<]*</span>` +
	`(?: ` + regexp.QuoteMeta(plaqueFact) + `[^<]*(?:<span class="(?:break-all )?font-mono">[^<]*</span>|<bdi>[^<]*</bdi>)?</span>)+` +
	`</div></li>$`)

// plaqueFacts is the facts screens.md §7 lists for a row, in its order, each its whole span:
// [Stored status] · No location / At <name> / At an unnamed location <id> · Encoded <time> / No
// encode recorded · Added <time> · [Retired <time>] · [Replaced by <uid>] · Counter <n>.
func plaqueFacts(r operatorpages.PlaqueRow) []string {
	mono := func(word, v string) string {
		return plaqueFact + word + ` <span class="font-mono">` + html.EscapeString(v) + `</span></span>`
	}
	var out []string
	if r.StoredStatus != "" {
		out = append(out, mono("Stored status", r.StoredStatus))
	}
	switch {
	case r.LocationID == "":
		out = append(out, plaqueFact+"No location</span>")
	case r.Location != "":
		out = append(out, plaqueFact+"At <bdi>"+html.EscapeString(r.Location)+"</bdi></span>")
	default:
		out = append(out, plaqueFact+`At an unnamed location <span class="break-all font-mono">`+html.EscapeString(r.LocationID)+"</span></span>")
	}
	if r.EncodedAt != "" {
		out = append(out, mono("Encoded", r.EncodedAt))
	} else {
		out = append(out, plaqueFact+"No encode recorded</span>")
	}
	out = append(out, mono("Added", r.CreatedAt))
	if r.RetiredAt != "" {
		out = append(out, mono("Retired", r.RetiredAt))
	}
	if r.ReplacedBy != "" {
		out = append(out, mono("Replaced by", r.ReplacedBy))
	}
	return append(out, mono("Counter", r.LastCtr))
}

// plaqueChipClass is the tone class plaqueChip writes for a tone (the brand's fixed mapping; an
// unnamed tone draws the unrecognised chip).
var plaqueChipClass = map[operatorpages.PlaqueTone]string{
	operatorpages.PlaqueToneInService: "tally--mounted", operatorpages.PlaqueToneStock: "tally--stock",
	operatorpages.PlaqueToneAttention: "tally--unencoded", operatorpages.PlaqueToneOut: "tally--withdrawn",
	operatorpages.PlaqueToneUnknown: "tally--unrecognised",
}

// plaqueState is what one render of the plaque screen must say: the summary sentence, words
// only, and its rows ("" and nil: no plaques, no docket).
type plaqueState struct {
	overview string
	summary  string
	rows     []operatorpages.PlaqueRow
}

// plaqueScreenFindings is TestPlaqueScreen_IsTheSummaryCardAndTheDocket's reading of one render:
// one finding per way it departs from s.
func plaqueScreenFindings(name, body string, s plaqueState) []string {
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
	inner := strings.TrimSpace(column[len(open) : len(column)-len("</main>")])

	// The column: the card, then the docket exactly when there are rows, nothing else.
	cardEnd := strings.Index(inner, "</section>")
	if !strings.HasPrefix(inner, plaquesCard) || cardEnd < 0 {
		bad("the column does not open with the card %s", plaquesCard)
		return f
	}
	card := inner[:cardEnd+len("</section>")]
	rest := strings.TrimSpace(inner[len(card):])
	if len(s.rows) == 0 {
		if rest != "" {
			bad("a tenant with no plaques shows %q after the card, want nothing", rest)
		}
	} else if !strings.HasPrefix(rest, plaquesDocket) || !strings.HasSuffix(rest, "</ol></section>") ||
		strings.Count(rest, "<section") != 1 {
		bad("after the card the column is not the docket %s…</ol></section> alone", plaquesDocket)
	}

	// The card: its five blocks in order, each a whole tag; its words; no top margin; two links.
	words := "Plaques Plaque inventory " + s.summary +
		" Read when this page was opened. Nothing on this page changes a plaque. Back to the overview Back to the tenants"
	if got := spaced(html.UnescapeString(noticeTag.ReplaceAllString(card, " "))); got != words {
		bad("the card says %q, want %q", got, words)
	}
	at, last := 0, -1
	for _, whole := range []string{plaquesLabel, plaquesH1, plaquesSummary, plaquesNote, plaquesLinks} {
		at = strings.Index(card, whole)
		if strings.Count(card, whole) != 1 || at < last {
			bad("the card does not carry %s once, after the block before it", whole)
		}
		last = at
	}
	if n := len(regexp.MustCompile(`<p[ >]`).FindAllString(card, -1)); n != 4 || strings.Count(card, "<h1") != 1 {
		bad("the card has %d paragraph(s) and %d h1, want the label, the summary, the note, the links and the h1", n,
			strings.Count(card, "<h1"))
	}
	for _, m := range classAttr.FindAllStringSubmatch(card, -1) {
		for _, c := range strings.Fields(m[1]) {
			if topMargin.MatchString(c) {
				bad("%q in the card carries a top margin; the card spaces its blocks", m[1])
			}
		}
	}
	links := []string{`<a href="` + s.overview + `" class="op-link">Back to the overview</a>`,
		`<a href="/operator/tenants" class="op-link">Back to the tenants</a>`}
	if got := anchorTag.FindAllString(column, -1); strings.Join(got, "|") != strings.Join(links, "|") {
		bad("the column's links are %q, want %q", got, links)
	}

	// The docket: one row per plaque, each the row's shape, its uid, chip, sentence and facts.
	rows := regexp.MustCompile(`<li[ >].*?</li>`).FindAllString(rest, -1)
	if len(rows) != len(s.rows) || strings.Count(rest, "<li") != len(s.rows) {
		bad("%d row(s), want %d", len(rows), len(s.rows))
		return f
	}
	for i, r := range rows {
		want := s.rows[i]
		if !plaqueRowShape.MatchString(r) {
			bad("row %d is not the uid's span then the wrapper of chip, sentence and facts: %s", i+1, r)
			continue
		}
		class, ok := plaqueChipClass[want.Tone]
		if !ok {
			class = "tally--unrecognised"
		}
		head := `<li class="op-row"><span class="font-mono font-bold">` + html.EscapeString(want.UID) + `</span>` + plaqueBody +
			`<span class="tally ` + class + `">` + html.EscapeString(want.Label) + `</span><span class="w-full text-sm">` +
			html.EscapeString(want.Sentence) + `</span> `
		if got, w := r, head+strings.Join(plaqueFacts(want), " ")+"</div></li>"; got != w {
			bad("row %d is\n  %s\nwant\n  %s", i+1, got, w)
		}
	}
	if n := strings.Count(body, "tally"); n != 2*len(s.rows) {
		bad("%d tally word(s) on the page, want a chip per row (%d)", n, 2*len(s.rows))
	}

	// Nothing hidden, no control, nothing §4.7 keeps off the page: the page's one form is the
	// bar's sign-out and its one button the sign-out's; no hx- anywhere.
	if strings.Count(body, "<form") != 1 || !strings.Contains(body, `<form method="post" action="/operator/logout">`) ||
		strings.Count(body, "<button") != 1 || strings.Contains(body, "hx-") {
		bad("%d form(s), %d button(s) or an hx- attribute on the page; want the sign-out's one form and one button",
			strings.Count(body, "<form"), strings.Count(body, "<button"))
	}
	for _, banned := range []string{"hidden", "sr-only", "aria-hidden", "<svg", "<img", "<canvas", "<picture", "<input", "<select",
		"<textarea", "<button", "<form", "<dl", "<table", "<script", "<iframe", "role=", "tabindex", "href=\"/t"} {
		if strings.Contains(column, banned) {
			bad("the column carries %q", banned)
		}
	}
	if m := regexp.MustCompile(`(?i)\b(?:qr|aes|keys?|key ref|cmac|sdm|sun|picc|kek|tap url)\b`).FindString(
		html.UnescapeString(noticeTag.ReplaceAllString(column, " "))); m != "" {
		bad("the column says %q (screens.md §7 Not shown: the AES key, the key ref, the CMAC, the tap URL, a QR)", m)
	}
	return f
}

// TestPlaqueScreen_IsTheSummaryCardAndTheDocket (M13-09; screens.md §7, README §6.1 "Plaque row"):
// each plaque-screen state -- screens()'s four "tenant plaques" renders (five rows of every tone;
// the first 5 of 205; no plaques; an unnamed tenant) and a render here of every fact's branch
// with long values (a stored status, a named, an unnamed and no location, an encode and none, a
// retirement and a successor, an unrecognised tone and the zero tone) -- is the frame's column
// holding exactly the card (plaquesCard) and, only when there are rows, the docket
// (plaquesDocket … </ol></section>), nothing else. The card: its blocks are the label, the h1,
// the summary, the note and the links' row, in that order, each its whole tag above, and nothing
// else (four paragraphs and the h1); no element in it carries a top margin; its words, tags
// dropped, are exactly screens.md §7's -- "Plaques", "Plaque inventory", the summary for the
// state (one of the three sentences), the note, the two links. The column's anchors are exactly
// Back to the overview (to the tenant's overview) and Back to the tenants. The docket: one row
// per plaque, in the view's order, each exactly plaqueRowShape -- the uid's span first, then ONE
// wrapper <div> (plaqueBody) holding the chip, the sentence and the facts, nothing after it --
// and, whole, the uid, the chip of the tone's class with the label, the sentence, and the facts
// plaqueFacts lists in screens.md §7's order; a chip per row and no other tally on the page. The
// page: the bar's sign-out is its one form and its one button; no hx-. Nowhere in the column:
// hidden content (a hidden attribute or class, sr-only, aria-hidden), a drawing or image (svg,
// img, canvas, picture -- no plaque QR, no plaque drawing), a control (input, select, textarea,
// button, form, a role or tabindex), a list of terms or a table, a script, a frame, a link to the
// tap path, or the words of what §4.7 keeps off the page (a QR, an AES key, a key ref, a CMAC, an
// SDM / SUN / PICC value, a KEK, a tap URL). CONTROLS, each read by the same function and each a
// finding: a word of the card changed; the note given back its top margin; the wrapper dropped
// (the chip straight after the uid); a fact moved out of the wrapper; a fact's value lost its mono;
// aria-hidden on a fact; a second button; the wrapper turned into a button.
//
// PART II -- red on any of the above. PART III -- what the rows look like at each width is the
// screenshot round's (320 / 390 / 834 / 1200 / 1440 px) and the row's scoped rules
// TestPlaqueRow_TheScopedRulesKeepTheBoardsColumns's; the handler's words and sentences are
// TestPlaqueScreen_EveryShapeHasItsSentenceAndAnUnknownStatusStaysVisible's and
// TestPlaqueWords_NameEveryShapeAndNothingElse's; the key's absence by type and by scan
// TestPlaqueScreen_NoKeyReachesThePage's -- no completeness claim.
func TestPlaqueScreen_IsTheSummaryCardAndTheDocket(t *testing.T) {
	all := screens(t)
	const overview = "/operator/tenants/10000000-0000-4000-8000-000000000001"
	const loc = "20000000-0000-4000-8000-000000000009"
	// screens()'s five rows (op8_test.go), restated: the reading is checked against them.
	fixture := []operatorpages.PlaqueRow{
		{UID: "04A1B2C3D4E5F6", Label: "On a wall", Sentence: "S", Tone: operatorpages.PlaqueToneInService, Location: "Hamrun",
			LocationID: loc, EncodedAt: "2026-07-05 09:30 UTC", CreatedAt: "2026-07-01 09:30 UTC", LastCtr: "3"},
		{UID: "04A1B2C3D4E5F7", Label: "In stock", Sentence: "S", Tone: operatorpages.PlaqueToneStock, CreatedAt: "2026-07-01 09:30 UTC", LastCtr: "0"},
		{UID: "04A1B2C3D4E5F8", Label: "On a wall, never encoded", Sentence: "S", Tone: operatorpages.PlaqueToneAttention,
			LocationID: loc, CreatedAt: "2026-07-01 09:30 UTC", LastCtr: "500"},
		{UID: "04A1B2C3D4E5F9", Label: "Retired", Sentence: "S", Tone: operatorpages.PlaqueToneOut, Location: "Hamrun", LocationID: loc,
			CreatedAt: "2026-07-01 09:30 UTC", RetiredAt: "2026-08-01 09:30 UTC", ReplacedBy: "04A1B2C3D4E5F6", LastCtr: "9"},
		{UID: "04A1B2C3D4E5FA", Label: "Unrecognised", Sentence: "S", StoredStatus: `"quarantined"`, CreatedAt: "2026-07-01 09:30 UTC", LastCtr: "0"},
	}
	whole := "5 plaques, in the panel's own order: those with no location first."
	states := map[string]plaqueState{
		"tenant plaques":          {overview, whole, fixture},
		"tenant plaques, first":   {overview, "The first 5 of 205 plaques are listed, in the panel's own order: those with no location first.", fixture},
		"tenant plaques, none":    {overview, "This tenant has no plaques.", nil},
		"tenant plaques, unnamed": {overview, whole, fixture},
	}
	for name := range all {
		if _, ok := states[name]; strings.HasPrefix(name, "tenant plaques") && !ok {
			t.Fatalf("PREMISE: screens() has a plaque render %q this test does not read", name)
		}
	}
	long := strings.Repeat("Q", 140)
	every := []operatorpages.PlaqueRow{
		{UID: strings.Repeat("04AC7E55", 8), Label: "Unrecognised", Sentence: "Its stored status and location are not a state this screen knows; the status is shown as stored.",
			StoredStatus: `"` + strings.Repeat("x", 120) + `"`, LocationID: loc, Location: long + ` <b>&`, EncodedAt: "2026-08-14 07:30 UTC",
			CreatedAt: "2026-08-14 07:26 UTC", RetiredAt: "2026-09-28 18:47 UTC", ReplacedBy: strings.Repeat("04AC7E55", 8), LastCtr: "4294967295"},
		{UID: "04AC7E55000201", Label: "On a wall, never encoded", Sentence: "Mounted and in service, but no encode was ever recorded for it.",
			Tone: operatorpages.PlaqueToneAttention, LocationID: loc, CreatedAt: "2026-08-02 11:12 UTC", LastCtr: "37"},
		{UID: "04AC7E550001FF", Label: "In stock", Sentence: "Not on a wall; encoded and ready to mount.", Tone: operatorpages.PlaqueToneStock,
			EncodedAt: "2026-10-16 10:05 UTC", CreatedAt: "2026-10-16 09:58 UTC", LastCtr: "0"},
		{UID: "04AC7E55000101", Label: "On a wall", Sentence: "In service at its location; its encode was recorded.", Tone: operatorpages.PlaqueToneInService,
			LocationID: loc, Location: "Valletta — Merchants Street", EncodedAt: "2026-07-25 08:40 UTC", CreatedAt: "2026-07-25 08:31 UTC", LastCtr: "1482"},
		{UID: "04AC7E55000401", Label: "Lost", Sentence: "Reported lost; taps on it are rejected.", Tone: operatorpages.PlaqueToneOut,
			LocationID: loc, Location: "St Julian's — Spinola Bay", EncodedAt: "2026-07-26 12:15 UTC", CreatedAt: "2026-07-26 12:09 UTC", LastCtr: "211"},
		{UID: "04AC7E55000501", Label: "Odd", Sentence: "A tone no case names.", Tone: operatorpages.PlaqueTone(99), CreatedAt: "2026-08-14 07:26 UTC", LastCtr: "5"},
	}
	var b strings.Builder
	if err := operatorpages.TenantPlaques(operatorpages.TenantPlaquesView{Name: mustName(t, "FAKE Every Fact Ltd"), ID: "x",
		OverviewPath: "/operator/tenants/x", Tabs: fixtureTabs, Total: "6", Shown: "6", Noun: "plaques", Rows: every}).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	all["every fact"] = b.String()
	states["every fact"] = plaqueState{"/operator/tenants/x", "6 plaques, in the panel's own order: those with no location first.", every}
	for name, s := range states {
		body, ok := all[name]
		if !ok {
			t.Fatalf("PREMISE: screens() has no %q", name)
		}
		for _, finding := range plaqueScreenFindings(name, body, s) {
			t.Error(finding)
		}
	}

	page := all["tenant plaques"]
	fact := plaqueFact + `Added <span class="font-mono">2026-07-01 09:30 UTC</span></span>`
	counter := plaqueFact + `Counter <span class="font-mono">3</span></span>`
	for _, c := range []struct{ what, from, to string }{
		{"a word of the card changed", "Nothing on this page changes a plaque.", "Nothing here changes a plaque."},
		{"the note's top margin back", plaquesNote, strings.Replace(plaquesNote, `class="`, `class="mt-1 `, 1)},
		{"the wrapper dropped", plaqueBody, ""},
		{"a fact out of the wrapper", " " + counter + "</div>", "</div> " + counter},
		{"a value not mono", fact, plaqueFact + `Added <span>2026-07-01 09:30 UTC</span></span>`},
		{"a fact hidden from a reader", fact, strings.Replace(fact, `<span class="basis-full`, `<span aria-hidden="true" class="basis-full`, 1)},
		{"a second button", plaquesLinks, plaquesLinks + `<button type="button" class="op-link">Retire</button>`},
		{"the wrapper a button", plaqueBody, strings.Replace(plaqueBody, "<div", "<button", 1)},
	} {
		mutated := strings.Replace(page, c.from, c.to, 1)
		if mutated == page {
			t.Fatalf("CONTROL %s: %q is not on the page", c.what, c.from)
		}
		if len(plaqueScreenFindings("control", mutated, states["tenant plaques"])) == 0 {
			t.Errorf("CONTROL: %s is not a finding; the reading is blind", c.what)
		}
	}
}

// plaqueRowRules is the plaque row's layout and type in web/static/css/input.css (README §6.1
// "Plaque row"), each rule scoped from the docket's heading id, its selector and its utilities
// exactly. The row: on the phone the shared row's wrapping flex with the boards' 10 px between
// the uid and the chip and 14 px above and below; from the tablet a grid of two columns, the
// uid's 156 px (168 px on the desktop) and the rest, 24 px apart, 16 px above and below. The uid
// 15 px, breaking anywhere. The wrapper's children after the chip in Space Grotesk; the facts'
// words at ink 70 % and their values in ink.
var plaqueRowRules = map[string][]string{
	`[aria-labelledby="tenant-plaques"] .op-row`: {"gap-x-2.5", "py-3.5", "md:grid", "md:grid-cols-[156px_minmax(0,1fr)]", "md:gap-x-6",
		"md:gap-y-1", "md:py-4", "desk:grid-cols-[168px_minmax(0,1fr)]"},
	`[aria-labelledby="tenant-plaques"] .op-row > :first-child`:              {"break-all", "text-[15px]"},
	`[aria-labelledby="tenant-plaques"] .op-row > div > :nth-child(n+2)`:     {"font-display"},
	`[aria-labelledby="tenant-plaques"] .op-row > div > :nth-child(n+3)`:     {"text-ink/70"},
	`[aria-labelledby="tenant-plaques"] .op-row > div > :nth-child(n+3) > *`: {"text-ink"},
}

// TestPlaqueRow_TheScopedRulesKeepTheBoardsColumns (M13-09; README §6.1 "Plaque row", boards
// OpPlaques, T-OpPlaques, M-OpPlaques): in web/static/css/input.css (comments stripped, read as
// flat `selectors { @apply …; }` rules by readScopedRules), every rule whose selector names the
// plaque docket's heading id is one of plaqueRowRules' five, each exactly once, one @apply of
// exactly its utilities. CONTROLS, each read by the same function and each a finding: the row's
// rule deleted (no uid column from the tablet); the desktop's 168 px dropped; the tablet's column
// changed; the uid's break-all dropped (a long uid widens the page); the sentence's and facts'
// face rule deleted; the facts' word tone changed; the values' ink rule deleted; a sixth rule on
// the docket, in the double-quoted, the single-quoted and the unquoted spelling.
//
// PART II -- red on any of the above. PART III -- what the rules draw at each width is the
// screenshot round's; a utility added to the row in the template is
// TestPlaqueScreen_IsTheSummaryCardAndTheDocket's (the row's markup) -- no completeness claim.
func TestPlaqueRow_TheScopedRulesKeepTheBoardsColumns(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	for _, f := range readScopedRules(css, "tenant-plaques", plaqueRowRules) {
		t.Error(f)
	}
	const row, first = `[aria-labelledby="tenant-plaques"] .op-row {`, `[aria-labelledby="tenant-plaques"] .op-row > :first-child {`
	const face, word, value = `[aria-labelledby="tenant-plaques"] .op-row > div > :nth-child(n+2) {`,
		`[aria-labelledby="tenant-plaques"] .op-row > div > :nth-child(n+3) {`, `[aria-labelledby="tenant-plaques"] .op-row > div > :nth-child(n+3) > * {`
	for _, c := range []struct{ what, from, to string }{
		{"the row's rule deleted", row, `.m1309-control-deleted {`},
		{"the desktop's column dropped", " desk:grid-cols-[168px_minmax(0,1fr)]", ""},
		{"the tablet's column changed", "md:grid-cols-[156px_minmax(0,1fr)]", "md:grid-cols-[120px_minmax(0,1fr)]"},
		{"the uid not breaking", first + " @apply break-all text-[15px]; }", first + " @apply text-[15px]; }"},
		{"the face rule deleted", face, `.m1309-control-face {`},
		{"the words' tone changed", word + " @apply text-ink/70; }", word + " @apply text-ink/50; }"},
		{"the values' rule deleted", value, `.m1309-control-value {`},
		{"a sixth rule", row, `[aria-labelledby="tenant-plaques"] .op-row > div { @apply text-xs; }` + "\n  " + row},
		{"a sixth rule, single-quoted", row, `[aria-labelledby='tenant-plaques'] .op-row { @apply md:grid-cols-1; }` + "\n  " + row},
		{"a sixth rule, unquoted", row, `[aria-labelledby=tenant-plaques] .op-row > :first-child { @apply break-normal; }` + "\n  " + row},
	} {
		mutated := strings.Replace(css, c.from, c.to, 1)
		if mutated == css {
			t.Fatalf("CONTROL %s: %q is not in input.css", c.what, c.from)
		}
		if len(readScopedRules(mutated, "tenant-plaques", plaqueRowRules)) == 0 {
			t.Errorf("CONTROL: %s is not a finding; the reading is blind", c.what)
		}
	}
}
