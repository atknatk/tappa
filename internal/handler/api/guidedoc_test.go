package api

// guidedoc_test.go -- the integration guide (docs/api/integration-guide.md and its Turkish
// translation) as a contract (M11 API-12; plan §2, plan §5 API-12; ADR 0028 "API-12 uygulama notu"):
//
//	TestGuide_TheTwoLanguagesAreOneDocument       the heading tree, the anchors, the numbered items,
//	                                              the code blocks (byte for byte), the link targets,
//	                                              the markers, the tables' code-and-number cells,
//	                                              each section's inline code and each section's
//	                                              numbers in prose are the same in both
//	TestGuide_CarriesAll31ContractDifferences     plan §2's 31 items are §11's, numbered, each with
//	                                              the item's own code tokens and key phrases, each
//	                                              linked to the section that explains it; item 31's
//	                                              warning stands in §6.5 too
//	TestGuide_EveryAPI12NoteHasItsSection         every note plan §5 collected for API-12 maps, through
//	                                              the ADR's table, to a section of both guides
//	TestGuide_EveryEndpointCodeAndLimitIsTheDocuments
//	                                              the endpoints the guide names are the OpenAPI
//	                                              document's, both ways; its code, reason and details
//	                                              tables are the document's (and the source's); its
//	                                              limits are x-limits; its retry table is the
//	                                              sender's; its JSON examples and request bodies are
//	                                              the document's examples -- which are the handlers'
//	TestGuide_TheSignatureVectorIsTheSenders      the known-answer vector's punch body is the
//	                                              document's webhook example and the sender's encoding,
//	                                              byte for byte (the vector itself is checked against
//	                                              the signer by internal/webhook's
//	                                              TestWebhookSignature_KnownAnswer, which reads it here)
//	TestGuide_NoSecretAddressOrPersonalData       no key, address, e-mail or source path in either guide

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	apidocs "github.com/atknatk/tappa/docs/api"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/webhook/delivery"
)

// mdDoc is what the tests read of a guide.
type mdDoc struct {
	name     string
	lines    []string
	headings []mdHeading
	anchors  map[string]bool
	blocks   []mdBlock
	links    []string
	items    []mdItem
	tables   []mdTable
	// code is each section's inline code spans, in order (the section is the last heading's anchor).
	code map[string][]string
	// numbers is each section's numbers in prose -- outside code blocks, inline code, link targets
	// and headings -- in order.
	numbers map[string][]string
}

type mdHeading struct {
	level        int
	text, anchor string
	line         int
}

type mdBlock struct {
	info, body, marker, section string
	line                        int
}

type mdItem struct {
	n       int
	text    string
	section string
}

type mdTable struct {
	section string
	rows    [][]string
}

var (
	anchorLine  = regexp.MustCompile(`^<a id="([a-z0-9-]+)"></a>$`)
	headingLine = regexp.MustCompile(`^(#{1,6}) (.+)$`)
	markerLine  = regexp.MustCompile(`^<!-- ((?:example|vector|error): .+) -->$`)
	linkTarget  = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	itemLine    = regexp.MustCompile(`^([0-9]+)\. (.*)$`)
	inlineCode  = regexp.MustCompile("`([^`]+)`")
	tableSep    = regexp.MustCompile(`^\|[-| :]+\|$`)
	proseNumber = regexp.MustCompile(`[0-9]+(?:[.,][0-9]+)?`)
)

func readGuide(t *testing.T, name string, src []byte) mdDoc {
	t.Helper()
	d := mdDoc{name: name, lines: strings.Split(string(src), "\n"), anchors: map[string]bool{}, code: map[string][]string{},
		numbers: map[string][]string{}}
	pending, marker, section := "", "", ""
	var fence *mdBlock
	var body []string
	var table *mdTable
	flushTable := func() {
		if table != nil {
			d.tables = append(d.tables, *table)
			table = nil
		}
	}
	for i, line := range d.lines {
		if fence != nil {
			if line == "```" {
				fence.body = strings.Join(body, "\n")
				d.blocks = append(d.blocks, *fence)
				fence, body = nil, nil
				continue
			}
			body = append(body, line)
			continue
		}
		if strings.HasPrefix(line, "```") {
			flushTable()
			fence = &mdBlock{info: strings.TrimPrefix(line, "```"), marker: marker, section: section, line: i + 1}
			marker = ""
			continue
		}
		if m := anchorLine.FindStringSubmatch(line); m != nil {
			if d.anchors[m[1]] {
				t.Errorf("%s:%d: the anchor %q twice", name, i+1, m[1])
			}
			d.anchors[m[1]] = true
			pending = m[1]
			continue
		}
		if m := markerLine.FindStringSubmatch(line); m != nil {
			marker = m[1]
			continue
		}
		if m := headingLine.FindStringSubmatch(line); m != nil {
			flushTable()
			h := mdHeading{level: len(m[1]), text: m[2], anchor: pending, line: i + 1}
			if h.level > 1 && pending == "" {
				t.Errorf("%s:%d: the heading %q has no anchor line before it", name, i+1, m[2])
			}
			d.headings = append(d.headings, h)
			section, pending = h.anchor, ""
			continue
		}
		if strings.TrimSpace(line) != "" {
			pending = ""
		}
		for _, l := range linkTarget.FindAllStringSubmatch(line, -1) {
			d.links = append(d.links, l[1])
		}
		for _, c := range inlineCode.FindAllStringSubmatch(line, -1) {
			d.code[section] = append(d.code[section], c[1])
		}
		prose := linkTarget.ReplaceAllString(inlineCode.ReplaceAllString(line, ""), "]")
		d.numbers[section] = append(d.numbers[section], proseNumber.FindAllString(prose, -1)...)
		if m := itemLine.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[1])
			d.items = append(d.items, mdItem{n: n, text: m[2], section: section})
		}
		if strings.HasPrefix(line, "|") {
			if table == nil {
				table = &mdTable{section: section}
			}
			if !tableSep.MatchString(line) {
				var cells []string
				for _, c := range strings.Split(strings.Trim(line, "|"), "|") {
					cells = append(cells, strings.TrimSpace(c))
				}
				table.rows = append(table.rows, cells)
			}
			continue
		}
		flushTable()
	}
	flushTable()
	if fence != nil {
		t.Fatalf("%s:%d: a code block that is never closed", name, fence.line)
	}
	return d
}

// theGuides reads both guides: the embedded bytes, which must be the files' (what the binary serves
// is what the repository holds).
func theGuides(t *testing.T) (en, tr mdDoc) {
	t.Helper()
	for _, c := range []struct {
		file     string
		embedded []byte
	}{{"integration-guide.md", apidocs.English()}, {"integration-guide.tr.md", apidocs.Turkish()}} {
		onDisk, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "api", c.file))
		if err != nil {
			t.Fatal(err)
		}
		if string(onDisk) != string(c.embedded) {
			t.Fatalf("the embedded %s is not the file's bytes", c.file)
		}
	}
	return readGuide(t, "integration-guide.md", apidocs.English()), readGuide(t, "integration-guide.tr.md", apidocs.Turkish())
}

// codeCell is a table cell that is language-neutral: a code span or a number.
var codeCell = regexp.MustCompile("^(`[^`]+`|[0-9]+)$")

// TestGuide_TheTwoLanguagesAreOneDocument (M11 API-12; the user's decision of 2026-10-16: English is
// the original, Turkish its translation, and their structural parity is pinned): a translation that
// drifts -- a section added on one side, a numbered item missing, a code block or a vector edited in
// one language, a link retargeted, a table's value changed -- is red.
//
// MUTANTS (measured, API-12 report): a heading dropped from the Turkish; an item renumbered; one byte
// of a code block changed; a link target changed; a limit's value changed in one table; an inline
// code token renamed in one section -- each red. API-12 fix (the prose numbers): in the Turkish
// alone, 120 saniye -> 180, 10 dakika -> 5, 6 saat -> 12, günde 300 -> 500, 72 saat -> 48 -- each red
// (each was green before the pin).
func TestGuide_TheTwoLanguagesAreOneDocument(t *testing.T) {
	en, tr := theGuides(t)
	shape := func(d mdDoc) []string {
		var out []string
		for _, h := range d.headings {
			num := strings.SplitN(h.text, " ", 2)[0]
			if !regexp.MustCompile(`^[0-9]+(\.[0-9]+)*\.?$`).MatchString(num) {
				num = ""
			}
			out = append(out, fmt.Sprintf("%d %s %s", h.level, h.anchor, num))
		}
		return out
	}
	if a, b := shape(en), shape(tr); !slices.Equal(a, b) {
		t.Errorf("the heading trees differ:\n en %v\n tr %v", a, b)
	}
	if len(en.headings) < 40 {
		t.Fatalf("anti-vacuity: %d headings", len(en.headings))
	}
	itemNumbers := func(d mdDoc) []string {
		var out []string
		for _, it := range d.items {
			out = append(out, fmt.Sprintf("%s:%d", it.section, it.n))
		}
		return out
	}
	if a, b := itemNumbers(en), itemNumbers(tr); !slices.Equal(a, b) {
		t.Errorf("the numbered items differ:\n en %v\n tr %v", a, b)
	}
	if len(en.blocks) != len(tr.blocks) {
		t.Fatalf("the code blocks: %d in English, %d in Turkish", len(en.blocks), len(tr.blocks))
	}
	for i := range en.blocks {
		a, b := en.blocks[i], tr.blocks[i]
		if a.info != b.info || a.body != b.body || a.marker != b.marker || a.section != b.section {
			t.Errorf("code block %d (English line %d, Turkish line %d) is not the same in both languages", i+1, a.line, b.line)
		}
	}
	if !slices.Equal(en.links, tr.links) {
		t.Errorf("the link targets differ:\n en %v\n tr %v", en.links, tr.links)
	}
	for _, d := range []mdDoc{en, tr} {
		for _, l := range d.links {
			if !strings.HasPrefix(l, "#") || !d.anchors[l[1:]] {
				t.Errorf("%s: the link %q goes to no anchor of the guide", d.name, l)
			}
		}
	}
	if len(en.tables) != len(tr.tables) {
		t.Fatalf("the tables: %d in English, %d in Turkish", len(en.tables), len(tr.tables))
	}
	for i := range en.tables {
		a, b := en.tables[i], tr.tables[i]
		if a.section != b.section || len(a.rows) != len(b.rows) {
			t.Errorf("table %d: section %s/%s, %d/%d rows", i+1, a.section, b.section, len(a.rows), len(b.rows))
			continue
		}
		for r := 1; r < len(a.rows); r++ {
			if len(a.rows[r]) != len(b.rows[r]) {
				t.Errorf("table %d row %d: %d/%d cells", i+1, r, len(a.rows[r]), len(b.rows[r]))
				continue
			}
			for c := range a.rows[r] {
				if codeCell.MatchString(a.rows[r][c]) || codeCell.MatchString(b.rows[r][c]) {
					if a.rows[r][c] != b.rows[r][c] {
						t.Errorf("table %d (%s) row %d cell %d: %q in English, %q in Turkish", i+1, a.section, r, c+1, a.rows[r][c], b.rows[r][c])
					}
				}
			}
		}
	}
	for section, codes := range en.code {
		a, b := slices.Clone(codes), slices.Clone(tr.code[section])
		slices.Sort(a)
		slices.Sort(b)
		if !slices.Equal(a, b) {
			t.Errorf("section %q: the inline code differs:\n en %v\n tr %v", section, a, b)
		}
	}
	for section := range tr.code {
		if _, ok := en.code[section]; !ok {
			t.Errorf("section %q has inline code in Turkish only", section)
		}
	}
	// The numbers of the prose, per section, in their ORDER: a limit, a wait or a count translated
	// into another value ("120 seconds" as "180 saniye") is red, and so are two values that changed
	// places ("1 minute, 5 minutes" as "5 dakika, 1 dakika"). Turkish puts a modifier before what it
	// modifies, so in the sections of numberOrderFree the translation reorders naturally ("a 02:00
	// exit of an 18:00 shift" is "18:00 vardiyasının 02:00 çıkışı"): there the numbers are compared
	// as a multiset only, and a swap is not seen. Nor is a sentence whose meaning the translation
	// turned round with the same numbers ("is not in the total" as "is in the total") -- both stay a
	// reviewer's (ADR 0028, the API-12 fix notes).
	numbered := 0
	for _, section := range slices.Sorted(maps.Keys(mergeKeys(en.numbers, tr.numbers))) {
		a, b := slices.Clone(en.numbers[section]), slices.Clone(tr.numbers[section])
		if numberOrderFree[section] {
			slices.Sort(a)
			slices.Sort(b)
		}
		if !slices.Equal(a, b) {
			t.Errorf("section %q: the numbers of the prose differ:\n en %v\n tr %v", section, a, b)
		}
		numbered += len(a)
	}
	for section := range numberOrderFree {
		if a, b := en.numbers[section], tr.numbers[section]; slices.Equal(a, b) {
			t.Errorf("section %q is in numberOrderFree but its numbers are in the same order: take it out", section)
		}
	}
	if numbered < 200 {
		t.Fatalf("anti-vacuity: %d numbers in the English prose", numbered)
	}
}

// numberOrderFree names the sections whose prose numbers the Turkish translation reorders by its
// word order (measured, API-12 second fix): §6.3 ("18:00 vardiyasının 02:00 çıkışı") and §7 ("bölüm
// 3.7'deki gibi RFC 3339"). A section is taken out once its numbers agree in order.
var numberOrderFree = map[string]bool{"feed-punch": true, "history": true}

// mergeKeys is the union of two maps' keys.
func mergeKeys(a, b map[string][]string) map[string]bool {
	out := map[string]bool{}
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}

// planSection2Items reads plan §2's numbered items.
func planSection2Items(t *testing.T) map[int]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "plan", "m11-entegrasyon-api.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	start := strings.Index(s, "\n## 2. ")
	end := strings.Index(s, "\n## 3. ")
	if start < 0 || end < start {
		t.Fatal("plan §2 not found")
	}
	items := map[int]string{}
	for _, line := range strings.Split(s[start:end], "\n") {
		if m := itemLine.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[1])
			items[n] = m[2]
		}
	}
	return items
}

// differenceTokens is an item's code tokens: its inline code, without the source references the
// plan cites for its reviewers (a guide for integrators names no source file).
func differenceTokens(text string) []string {
	var out []string
	for _, c := range inlineCode.FindAllStringSubmatch(text, -1) {
		if strings.Contains(c[1], ".go") || strings.HasPrefix(c[1], "internal/") {
			continue
		}
		out = append(out, c[1])
	}
	return out
}

// differencePhrases are each item's key phrases beyond its code tokens: what the item SAYS, in each
// language.
var differencePhrases = map[int]struct{ en, tr []string }{
	1:  {[]string{"not a punch", "own company"}, []string{"punch değildir", "kendi şirketinin"}},
	2:  {[]string{"approves", "never comes"}, []string{"onaylarsa", "hiç gelmez"}},
	3:  {[]string{"never produced"}, []string{"hiç üretilmez"}},
	4:  {[]string{"both channels"}, []string{"her iki kanalda"}},
	5:  {[]string{"120 seconds", "72 hours"}, []string{"120 saniye", "72 saat"}},
	6:  {[]string{"60 seconds", "two companies"}, []string{"60 saniye", "iki şirketin"}},
	7:  {[]string{"422"}, []string{"422"}},
	8:  {[]string{"label"}, []string{"etiket"}},
	9:  {[]string{"new"}, []string{"yeni"}},
	10: {[]string{"wait for approval", "panel"}, []string{"onaya düşer", "panel"}},
	11: {[]string{"ASCII", "422", "409", "queue"}, []string{"ASCII", "422", "409", "kuyruk"}},
	12: {[]string{"202", "last hour"}, []string{"202", "son bir saat"}},
	13: {[]string{"409"}, []string{"409"}},
	14: {[]string{"no activation e-mail is sent"}, []string{"aktivasyon e-postası gönderilmez"}},
	15: {[]string{"NFC", "QR"}, []string{"NFC", "QR"}},
	16: {[]string{"no order guarantee"}, []string{"sıra garantisi yoktur"}},
	17: {[]string{"Manual records"}, []string{"Manuel kayıtlar"}},
	18: {[]string{"31 days and one hour", "paged", "snapshot"}, []string{"31 gün ve bir saat", "sayfalıdır", "anlık görüntü"}},
	19: {[]string{"10-minute"}, []string{"10 dakikalık"}},
	20: {[]string{"restarts", "next tap"}, []string{"yeniden başladığında", "sonraki dokunuş"}},
	21: {nil, nil},
	22: {[]string{"hourly budget", "429"}, []string{"saatlik", "429"}},
	23: {[]string{"Taptime enabled"}, []string{"Taptime'ın işletme için açtığı"}},
	24: {[]string{"home location"}, []string{"Ev lokasyonu"}},
	25: {[]string{"employer's", "network"}, []string{"işverenin", "ağından"}},
	26: {[]string{"membership ends", "refused without a record"}, []string{"üyeliği bittiğinde", "kayıtsız reddedilir"}},
	27: {[]string{"at the punch"}, []string{"punch anındaki"}},
	28: {[]string{"lost or retired"}, []string{"kayıp ya da emekli"}},
	29: {[]string{"paged", "1000"}, []string{"sayfalıdır", "1000"}},
	30: {[]string{"only the **in**", "at the employer"}, []string{"yalnız **girişi**", "işverende"}},
	31: {[]string{"too long", "with `direction` too", "events in between", "`employer` scope"}, []string{"**fazla**", "`direction` ile de", "aradaki olaylar", "`employer` kapsamı"}},
}

// TestGuide_CarriesAll31ContractDifferences (M11 API-12; ADR 0028 §8 and "Test edilecekler",
// API-12; plan §2: "API-12'nin rehberinde bu liste açıkça yazılır"): plan §2 has exactly 31 items,
// and §11 of each guide has them, numbered 1 to 31 in order -- each carrying the plan item's code
// tokens and its key phrases, and a link to the section that explains it. Item 31 (the warning about
// working time from scope=location) is also the caution of §6.5.
//
// MUTANTS (measured, API-12 report): item 31 deleted from the Turkish; item 21's token edited; a
// 32nd item added to the plan -- each red.
func TestGuide_CarriesAll31ContractDifferences(t *testing.T) {
	plan := planSection2Items(t)
	if len(plan) != 31 {
		t.Fatalf("plan §2 has %d items, the guide is written for 31", len(plan))
	}
	if len(differencePhrases) != 31 {
		t.Fatalf("the phrase table has %d items", len(differencePhrases))
	}
	en, tr := theGuides(t)
	for _, c := range []struct {
		d    mdDoc
		lang string
	}{{en, "en"}, {tr, "tr"}} {
		var got []mdItem
		for _, it := range c.d.items {
			if it.section == "differences" {
				got = append(got, it)
			}
		}
		if len(got) != 31 {
			t.Fatalf("%s: §11 has %d items, want 31", c.d.name, len(got))
		}
		for i, it := range got {
			n := i + 1
			if it.n != n {
				t.Errorf("%s: §11 item %d is numbered %d", c.d.name, n, it.n)
			}
			for _, tok := range differenceTokens(plan[n]) {
				if !strings.Contains(it.text, "`"+tok+"`") {
					t.Errorf("%s: item %d does not carry the plan's `%s`", c.d.name, n, tok)
				}
			}
			phrases := differencePhrases[n].en
			if c.lang == "tr" {
				phrases = differencePhrases[n].tr
			}
			for _, p := range phrases {
				if !strings.Contains(it.text, p) {
					t.Errorf("%s: item %d does not say %q", c.d.name, n, p)
				}
			}
			links := linkTarget.FindAllStringSubmatch(it.text, -1)
			if len(links) == 0 {
				t.Errorf("%s: item %d links to no section", c.d.name, n)
			}
			for _, l := range links {
				if !c.d.anchors[strings.TrimPrefix(l[1], "#")] {
					t.Errorf("%s: item %d links to %s, no anchor", c.d.name, n, l[1])
				}
			}
		}
		if len(differenceTokens(plan[21])) == 0 || len(differenceTokens(plan[31])) == 0 {
			t.Fatal("anti-vacuity: items 21 and 31 carry code tokens")
		}
		caution := c.d.section("feed-working-time")
		want := []string{"`scope=location`", "too long", "`direction`", "`employer` scope"}
		if c.lang == "tr" {
			want = []string{"`scope=location`", "**fazla**", "`direction`", "`employer` kapsamı"}
		}
		for _, w := range want {
			if !strings.Contains(caution, w) {
				t.Errorf("%s: §6.5's caution does not say %q", c.d.name, w)
			}
		}
	}
}

// section is the text from a heading's anchor to the next heading of the same or a higher level.
func (d mdDoc) section(anchor string) string {
	for i, h := range d.headings {
		if h.anchor != anchor {
			continue
		}
		end := len(d.lines)
		for _, n := range d.headings[i+1:] {
			if n.level <= h.level {
				end = n.line - 1
				break
			}
		}
		return strings.Join(d.lines[h.line:end], "\n")
	}
	return ""
}

// api12NoteCount is the number of rows the ADR's mapping table has: one per note plan §5 collected
// for API-12. A note added to the plan without its row, or a row without its note, moves it.
const api12NoteCount = 67

var collapse = regexp.MustCompile(`\s+`)

// TestGuide_EveryAPI12NoteHasItsSection (M11 API-12; the brief: "plan §5'teki her API-12 notunun
// rehberde karşılığı"): the ADR 0028 "API-12 uygulama notu" table maps each note -- a phrase quoted
// from plan §5's API-12 item -- to an anchor of the guide. Every phrase is in that item, word for
// word; every anchor is a section of both guides; the rows are numbered 1..api12NoteCount; and every
// block of the item (the text before its first "(API-…)" header and each header's text) has at
// least one row -- a note the table forgot is a block without a row.
//
// MUTANTS (measured, API-12 report): a row's anchor misspelt; a phrase that is not the plan's; the
// rows of the API-9 block removed -- each red.
func TestGuide_EveryAPI12NoteHasItsSection(t *testing.T) {
	plan, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "plan", "m11-entegrasyon-api.md"))
	if err != nil {
		t.Fatal(err)
	}
	ps := string(plan)
	start := strings.Index(ps, "\n- **API-12:**")
	end := strings.Index(ps, "\n- **API-13:**")
	if start < 0 || end < start {
		t.Fatal("plan §5's API-12 item not found")
	}
	item := collapse.ReplaceAllString(ps[start:end], " ")
	adr, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "adr", "0028-dis-api-anahtar-akis-webhook.md"))
	if err != nil {
		t.Fatal(err)
	}
	as := string(adr)
	tStart := strings.Index(as, "<!-- api12-notes -->")
	tEnd := strings.Index(as, "<!-- /api12-notes -->")
	if tStart < 0 || tEnd < tStart {
		t.Fatal("the ADR's API-12 note table (between <!-- api12-notes --> markers) not found")
	}
	en, tr := theGuides(t)
	row := regexp.MustCompile(`^\| ([0-9]+) \| [^|]+ \| "([^"]+)" \| \[[^\]]+\]\(([^)]+)\) \|$`)
	var phrases []string
	n := 0
	for _, line := range strings.Split(as[tStart:tEnd], "\n") {
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| #") || tableSep.MatchString(line) {
			continue
		}
		m := row.FindStringSubmatch(line)
		if m == nil {
			t.Errorf("a row the test does not read: %q", line)
			continue
		}
		n++
		if m[1] != strconv.Itoa(n) {
			t.Errorf("row %s is numbered out of order (want %d)", m[1], n)
		}
		phrase := collapse.ReplaceAllString(m[2], " ")
		if !strings.Contains(item, phrase) {
			t.Errorf("row %s: %q is not in plan §5's API-12 item", m[1], phrase)
		}
		phrases = append(phrases, phrase)
		target := m[3]
		anchor := target[strings.Index(target, "#")+1:]
		if !strings.HasPrefix(target, "../api/integration-guide.md#") || !en.anchors[anchor] || !tr.anchors[anchor] {
			t.Errorf("row %s: %s is not a section of both guides", m[1], target)
		}
	}
	if n != api12NoteCount {
		t.Errorf("the table has %d rows, want %d", n, api12NoteCount)
	}
	header := regexp.MustCompile(`\*{1,2}\((API-[0-9]+[a-z]?|Üçüncü tur|Dördüncü tur)[^)]*\)\*{1,2}`)
	cuts := header.FindAllStringIndex(item, -1)
	if len(cuts) < 10 {
		t.Fatalf("anti-vacuity: %d header blocks in the API-12 item", len(cuts))
	}
	bounds := []int{0}
	for _, c := range cuts {
		bounds = append(bounds, c[0])
	}
	bounds = append(bounds, len(item))
	for i := 0; i+1 < len(bounds); i++ {
		block := item[bounds[i]:bounds[i+1]]
		covered := false
		for _, p := range phrases {
			covered = covered || strings.Contains(block, p)
		}
		if !covered {
			t.Errorf("the API-12 item's block %d (%.60q...) has no row in the ADR's table", i, block)
		}
	}
}

var guideEndpoint = regexp.MustCompile(`\b(GET|PUT|POST|DELETE) (/v1/[A-Za-z0-9/{}._-]*[A-Za-z0-9}_])`)

// tableIn is the first table of a section whose header's first cell is first.
func (d mdDoc) tableIn(t *testing.T, section string, index int) [][]string {
	t.Helper()
	k := 0
	for _, tb := range d.tables {
		if tb.section == section {
			if k == index {
				return tb.rows[1:]
			}
			k++
		}
	}
	t.Fatalf("%s: section %s has no table %d", d.name, section, index)
	return nil
}

func unquoteCode(s string) string { return strings.TrimSuffix(strings.TrimPrefix(s, "`"), "`") }

// TestGuide_EveryEndpointCodeAndLimitIsTheDocuments (M11 API-12; the brief: "rehberdeki her uç ve
// hata kodu OpenAPI'de var ve tersi"): what the guide states that the OpenAPI document also states is
// the document's -- and through the document's own tests, the code's.
//
// MUTANTS (measured, API-12 report): an endpoint the document does not have named in the guide; a
// code's status edited in the codes table; a limit's value edited in §3.5; a retry wait edited; a
// field renamed in a guide example -- each red.
func TestGuide_EveryEndpointCodeAndLimitIsTheDocuments(t *testing.T) {
	doc := openAPIDoc(t)
	var documented []string
	for _, o := range operationsOf(t, doc) {
		if strings.HasPrefix(o.where, "paths ") {
			documented = append(documented, strings.ToUpper(o.method)+" "+o.path)
		}
	}
	slices.Sort(documented)
	en, tr := theGuides(t)
	for _, d := range []mdDoc{en, tr} {
		text := strings.Join(d.lines, "\n")
		set := map[string]bool{}
		for _, m := range guideEndpoint.FindAllStringSubmatch(text, -1) {
			set[m[1]+" "+m[2]] = true
		}
		var named []string
		for k := range set {
			named = append(named, k)
		}
		slices.Sort(named)
		if !slices.Equal(named, documented) {
			t.Errorf("%s: the endpoints it names %v, the document's %v", d.name, named, documented)
		}

		// §3.1's tables: codes (with statuses), details keys, reasons.
		var codes []string
		for _, r := range d.tableIn(t, "errors", 0) {
			c := unquoteCode(r[0])
			codes = append(codes, c)
			if r[1] != strconv.Itoa(StatusOf(Code(c))) {
				t.Errorf("%s: %s is listed with %s, it answers %d", d.name, c, r[1], StatusOf(Code(c)))
			}
		}
		var want []string
		for _, c := range Codes() {
			want = append(want, string(c))
		}
		if !slices.Equal(codes, want) {
			t.Errorf("%s: the codes table %v, the set %v", d.name, codes, want)
		}
		var keys []string
		for _, r := range d.tableIn(t, "errors", 1) {
			keys = append(keys, unquoteCode(r[0]))
		}
		props, _ := yat(doc, "components", "schemas", "ErrorDetails", "properties")
		if !slices.Equal(keys, props.(*ymap).keys) {
			t.Errorf("%s: the details table %v, ErrorDetails %v", d.name, keys, props.(*ymap).keys)
		}
		var reasons []string
		_, closed := problemDetailsInSource(t)
		reasonCodes := reasonCodesInSource(t)
		for _, r := range d.tableIn(t, "errors", 2) {
			reason := unquoteCode(r[0])
			reasons = append(reasons, reason)
			if got, want := unquoteCode(r[1]), reasonCodes[reason]; got != want {
				t.Errorf("%s: reason %s is listed under %s, the source gives it %s", d.name, reason, got, want)
			}
		}
		slices.Sort(reasons)
		if !slices.Equal(reasons, closed["reason"]) {
			t.Errorf("%s: the reasons table %v, the source's %v", d.name, reasons, closed["reason"])
		}

		// §3.4 and §3.5: the limits are x-limits.
		limits, _ := yat(doc, "info", "x-limits")
		lm := limits.(*ymap)
		var rowsKeys []string
		for _, r := range d.tableIn(t, "limits", 0) {
			k := unquoteCode(r[0])
			rowsKeys = append(rowsKeys, k)
			if v, ok := lm.get(k); !ok || r[1] != strconv.FormatFloat(v.(float64), 'f', -1, 64) {
				t.Errorf("%s: §3.5 says %s is %s, x-limits %v", d.name, k, r[1], v)
			}
		}
		if !slices.Equal(rowsKeys, lm.keys) {
			t.Errorf("%s: §3.5's keys %v, x-limits' %v", d.name, rowsKeys, lm.keys)
		}
		rate := d.tableIn(t, "rate-limits", 0)
		for i, k := range []string{"requestsPerAddressPerMinute", "requestsPerKeyPerMinute", "deactivationsPerKeyPerHour"} {
			v, _ := lm.get(k)
			if rate[i][1] != strconv.FormatFloat(v.(float64), 'f', -1, 64) {
				t.Errorf("%s: §3.4 row %d is %s, x-limits.%s %v", d.name, i+1, rate[i][1], k, v)
			}
		}

		// §9.6: the retry table is the sender's.
		waits := []string{"0"}
		for _, b := range delivery.Backoff {
			waits = append(waits, strconv.Itoa(int(b.Seconds())))
		}
		var got []string
		for i, r := range d.tableIn(t, "webhook-retries", 0) {
			if r[0] != strconv.Itoa(i+1) {
				t.Errorf("%s: §9.6 row %d is try %s", d.name, i+1, r[0])
			}
			got = append(got, r[1])
		}
		if !slices.Equal(got, waits) || len(got) != delivery.MaxAttempts {
			t.Errorf("%s: §9.6's waits %v, the sender's %v (%d tries)", d.name, got, waits, delivery.MaxAttempts)
		}

		// The JSON examples are the document's; the request bodies of the curl examples are its request
		// examples.
		examples := 0
		for _, b := range d.blocks {
			switch {
			case strings.HasPrefix(b.marker, "example: "):
				name := strings.TrimPrefix(b.marker, "example: ")
				where, ok := storyExamples[name]
				if !ok {
					t.Errorf("%s:%d: the example %q is no answer of the story", d.name, b.line, name)
					continue
				}
				ex, _ := yat(doc, where.answer...)
				val, err := jsonValue([]byte(b.body))
				if err != nil || canonical(val) != canonical(ex) {
					t.Errorf("%s:%d: the example %q is not the document's (%v)", d.name, b.line, name, err)
				}
				examples++
			case strings.HasPrefix(b.marker, "error: "):
				parts := strings.SplitN(strings.TrimPrefix(b.marker, "error: "), "/", 2)
				ex, ok := yat(doc, "components", "responses", parts[0], "content", "application/json", "examples", parts[1], "value")
				val, err := jsonValue([]byte(b.body))
				if !ok || err != nil || canonical(val) != canonical(ex) {
					t.Errorf("%s:%d: the error example %v is not the document's", d.name, b.line, parts)
				}
				examples++
			case b.info == "sh":
				for _, m := range regexp.MustCompile(`--data-binary '([^']*)'`).FindAllStringSubmatch(b.body, -1) {
					val, err := jsonValue([]byte(m[1]))
					if err != nil {
						t.Errorf("%s:%d: a request body that is not JSON", d.name, b.line)
						continue
					}
					matched := false
					for _, where := range storyExamples {
						if where.request != nil {
							ex, _ := yat(doc, where.request...)
							matched = matched || canonical(ex) == canonical(val)
						}
					}
					if !matched {
						t.Errorf("%s:%d: the request body %s is no request example of the document", d.name, b.line, m[1])
					}
					examples++
				}
			}
		}
		if examples < 9 {
			t.Errorf("anti-vacuity: %s has %d checked examples", d.name, examples)
		}
	}
}

// reasonCodesInSource maps each reason to the code the package's refusals carry it with: a Problem
// literal's Code and its Details' reason; invalidField's are validation_failed.
func reasonCodesInSource(t *testing.T) map[string]string {
	t.Helper()
	files, consts := apiSource(t)
	value := func(e ast.Expr) string {
		switch x := e.(type) {
		case *ast.BasicLit:
			v, _ := strconv.Unquote(x.Value)
			return v
		case *ast.Ident:
			return consts[x.Name]
		}
		return ""
	}
	out := map[string]string{}
	for _, af := range files {
		ast.Inspect(af, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CompositeLit:
				if id, ok := x.Type.(*ast.Ident); !ok || id.Name != "Problem" {
					return true
				}
				code, reason := "", ""
				for _, el := range x.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					switch k := kv.Key.(*ast.Ident); {
					case k != nil && k.Name == "Code":
						code = value(kv.Value)
					case k != nil && k.Name == "Details":
						if m, ok := kv.Value.(*ast.CompositeLit); ok {
							for _, e := range m.Elts {
								if d, ok := e.(*ast.KeyValueExpr); ok && value(d.Key) == "reason" {
									reason = value(d.Value)
								}
							}
						}
					}
				}
				if reason != "" {
					out[reason] = code
				}
			case *ast.CallExpr:
				if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "invalidField" && len(x.Args) == 3 {
					if r := value(x.Args[1]); r != "" {
						out[r] = string(CodeValidationFailed)
					}
				}
			}
			return true
		})
	}
	return out
}

// guideVector is one known answer of the guide's signature block.
type guideVector struct{ key, body, signature string }

// guideVectors reads the block after <!-- vector: webhook-signature -->: a key line, then body and
// signature lines in pairs ("(empty)" is the empty body).
func guideVectors(t *testing.T, d mdDoc) []guideVector {
	t.Helper()
	for _, b := range d.blocks {
		if b.marker != "vector: webhook-signature" {
			continue
		}
		var out []guideVector
		key := ""
		for _, line := range strings.Split(b.body, "\n") {
			k, v, ok := strings.Cut(line, ": ")
			if !ok {
				t.Fatalf("%s: a vector line without a colon: %q", d.name, line)
			}
			switch k {
			case "key":
				key = v
			case "body":
				if v == "(empty)" {
					v = ""
				}
				out = append(out, guideVector{key: key, body: v})
			case "signature":
				out[len(out)-1].signature = v
			default:
				t.Fatalf("%s: a vector line %q", d.name, line)
			}
		}
		return out
	}
	t.Fatalf("%s: no signature vector block", d.name)
	return nil
}

// TestGuide_TheSignatureVectorIsTheSenders (M11 API-12; ADR 0028 §6): the vector block's punch body
// is a delivery the SENDER encodes -- decoded into delivery.Body and encoded again it is the same
// bytes, field order included -- and it is the document's webhook example. (Whether each signature is
// the signer's is internal/webhook's TestWebhookSignature_KnownAnswer, which reads this block: the
// vector has one source, the guide.)
func TestGuide_TheSignatureVectorIsTheSenders(t *testing.T) {
	en, _ := theGuides(t)
	vs := guideVectors(t, en)
	if len(vs) != 3 || vs[2].body == "" {
		t.Fatalf("the vector block has %d known answers, want ping, empty and a punch", len(vs))
	}
	var body delivery.Body
	dec := json.NewDecoder(strings.NewReader(vs[2].body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		t.Fatalf("the vector's punch body is not a delivery body: %v", err)
	}
	again, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != vs[2].body {
		t.Errorf("the vector's punch body is not the sender's encoding:\n guide  %s\n sender %s", vs[2].body, again)
	}
	doc := openAPIDoc(t)
	ex, _ := yat(doc, storyExamples["webhook punch"].answer...)
	val, _ := jsonValue([]byte(vs[2].body))
	if canonical(ex) != canonical(val) {
		t.Error("the vector's punch body is not the document's webhook example")
	}
}

// TestGuide_TheExamplePunchesAreOutsideTheRepeatWindow (M11 API-12, second fix; CLAUDE.md §5, ADR
// 0006): the documents tell one story, so two punches of one employee anywhere in them -- the
// examples of openapi.yaml, the guide's example blocks, the signature vector's body -- are a pair the
// product would have recorded: a repeat of the same person within 60 seconds is no punch, and the
// distance is the SMALLER of the declared instants' (punchTime) and the server's (recordedAt; an ok
// tap's is the row's own instant). The story's handlers make the examples
// (TestOpenAPI_TheExamplesAreTheHandlersAnswers, through the tap path), but that test compares an
// instant by kind only, so this one reads the printed values.
//
// MUTANT (measured, API-12 second fix report): the first fix's examples -- Joe's in and out 63 s
// apart in punchTime, 26 ms in recordedAt -- red; the out's recordedAt moved 59 s after the in's, red.
func TestGuide_TheExamplePunchesAreOutsideTheRepeatWindow(t *testing.T) {
	type punch struct {
		id, where         string
		punchT, recordedT time.Time
	}
	byEmployee := map[string][]punch{}
	instant := func(where string, m *ymap, k string) time.Time {
		v, _ := m.get(k)
		s, _ := v.(string)
		at, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			t.Fatalf("%s: the punch's %s %q: %v", where, k, s, err)
		}
		return at
	}
	collect := func(where string, v any) {
		walkValues(v, where, func(path string, v any) {
			m, ok := v.(*ymap)
			if !ok {
				return
			}
			// A punch's punchTime is an instant; a schema's property of that name is a map.
			if v, _ := m.get("punchTime"); !isString(v) {
				return
			}
			id, _ := yat(m, "id")
			emp, _ := yat(m, "employee", "id")
			p := punch{id: fmt.Sprint(id), where: path, punchT: instant(path, m, "punchTime"), recordedT: instant(path, m, "recordedAt")}
			byEmployee[fmt.Sprint(emp)] = append(byEmployee[fmt.Sprint(emp)], p)
		})
	}
	doc := openAPIDoc(t)
	for _, section := range []string{"paths", "webhooks", "components"} {
		sec, _ := doc.get(section)
		collect("openapi.yaml/"+section, sec)
	}
	for _, d := range func() []mdDoc { en, tr := theGuides(t); return []mdDoc{en, tr} }() {
		for _, b := range d.blocks {
			if !strings.HasPrefix(b.marker, "example: ") {
				continue
			}
			if v, err := jsonValue([]byte(b.body)); err == nil {
				collect(d.name+"/"+b.marker, v)
			}
		}
		for _, v := range guideVectors(t, d) {
			if strings.HasPrefix(v.body, "{\"event\":\"punch\"") {
				val, err := jsonValue([]byte(v.body))
				if err != nil {
					t.Fatal(err)
				}
				collect(d.name+"/vector", val)
			}
		}
	}
	pairs := 0
	for emp, ps := range byEmployee {
		for i := range ps {
			for j := range ps[:i] {
				a, b := ps[i], ps[j]
				if a.id == b.id {
					continue
				}
				pairs++
				declared, recorded := a.punchT.Sub(b.punchT).Abs(), a.recordedT.Sub(b.recordedT).Abs()
				if gap := min(declared, recorded); gap < 60*time.Second {
					t.Errorf("employee %s: %s at %s and %s at %s are %v apart (punchTime %v, recordedAt %v): the second would be an ignored repeat, not a punch",
						emp, a.id, a.where, b.id, b.where, gap, declared, recorded)
				}
			}
		}
	}
	if pairs == 0 {
		t.Fatal("anti-vacuity: no two punches of one employee in the documents")
	}
}

// TestGuide_TheRequestIDRuleIsTheLogs (M11 API-12, second fix; guide §2.2): the X-Request-Id an
// integration sends is the id Taptime's log records carry exactly when the guide says -- 1 to
// httpx.MaxRequestIDLen characters of A-Z, a-z, 0-9, '-', '_', '.', none of the three key prefixes --
// and is replaced otherwise. The guide's numbers are the constant's.
//
// MUTANTS (measured, API-12 second fix report): the Turkish guide's "64" edited, red;
// MaxRequestIDLen moved to 65, red; '.' dropped from the allowed set in httpx, red.
func TestGuide_TheRequestIDRuleIsTheLogs(t *testing.T) {
	en, tr := theGuides(t)
	for _, c := range []struct {
		d    mdDoc
		says string
	}{{en, fmt.Sprintf("1 to %d characters", httpx.MaxRequestIDLen)}, {tr, fmt.Sprintf("1 ile %d karakter", httpx.MaxRequestIDLen)}} {
		if !strings.Contains(collapse.ReplaceAllString(c.d.section("auth-keys"), " "), c.says) {
			t.Errorf("%s section 2.2 does not say %q", c.d.name, c.says)
		}
	}
	logged := func(id string) string {
		var buf bytes.Buffer
		log := slog.New(httpx.WithRequestID(slog.NewJSONHandler(&buf, nil)))
		h := httpx.RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { log.InfoContext(r.Context(), "x") }))
		req := httptest.NewRequest(http.MethodGet, "/v1/punches", nil)
		req.Header.Set(httpx.RequestIDHeader, id)
		h.ServeHTTP(httptest.NewRecorder(), req)
		var rec map[string]any
		if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
			t.Fatal(err)
		}
		return fmt.Sprint(rec[httpx.LogRequestIDKey])
	}
	set := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_."
	// The set is 65 characters: its first 64 and its last 64 are the longest ids kept, all of it is
	// one too long.
	kept := []string{"a", set[:httpx.MaxRequestIDLen], set[len(set)-httpx.MaxRequestIDLen:], "erp-2026.10.16_000042", strings.Repeat("A", 43)}
	replaced := []string{set, strings.Repeat("a", httpx.MaxRequestIDLen+1), "TT_LIVE_abc", "x-tt_test_y", "tt_whsec_1"}
	for c := 0x21; c < 0x7f; c++ {
		if !strings.ContainsRune(set, rune(c)) {
			replaced = append(replaced, "erp"+string(rune(c))+"1")
		}
	}
	replaced = append(replaced, "erp 1", "erp\u00e91")
	for _, id := range kept {
		if got := logged(id); got != id {
			t.Errorf("the request id %q is logged as %q, the guide says as it is", id, got)
		}
	}
	for _, id := range replaced {
		if got := logged(id); got == id {
			t.Errorf("the request id %q is logged as it is, the guide says replaced", id)
		}
	}
	if len(replaced) < 30 {
		t.Fatalf("anti-vacuity: %d values that are replaced", len(replaced))
	}
}

func isString(v any) bool { _, ok := v.(string); return ok }

var (
	emailShape   = regexp.MustCompile(`[A-Za-z0-9._%+-]+@([A-Za-z0-9-]+\.)+[A-Za-z]{2,}`)
	ipv4Shape    = regexp.MustCompile(`\b([0-9]{1,3}\.){3}[0-9]{1,3}\b`)
	ipv6Token    = regexp.MustCompile(`[0-9A-Fa-f.]*:[0-9A-Fa-f:.]*(?:%[0-9A-Za-z_.-]+)?`)
	keyShapeText = regexp.MustCompile(`tt_(live|test|whsec)_([A-Za-z0-9_-]{32,})`)
	urlShape     = regexp.MustCompile(`https?://([A-Za-z0-9.-]+)`)
	// internalHost is a cluster's or a private network's host name, with a scheme or without: a
	// Kubernetes service name, *.internal, *.local and the like.
	internalHost = regexp.MustCompile(`(?i)\b[a-z0-9-]+(?:\.[a-z0-9-]+)*\.(?:svc\.cluster\.local|cluster\.local|internal|local|localdomain|lan|corp|intranet|home\.arpa)\b|\blocalhost\b`)
	// base64Run is a run of standard base64 long enough for 32 bytes -- the KEK's and an HMAC key's
	// shape in the environment (TAPPA_TAG_KEK). A hexadecimal run (a signature) is not one.
	base64Run = regexp.MustCompile(`[A-Za-z0-9+/]{43,}={0,2}`)
	hexRun    = regexp.MustCompile(`^[0-9A-Fa-f]+$`)
)

// base64Secrets is every base64Run in s that could be a secret: not hexadecimal, and with more than
// four distinct characters (redline R7d's rule: padding like the vector's 'A's is not a key).
func base64Secrets(s string) []string {
	var out []string
	for _, run := range base64Run.FindAllString(s, -1) {
		if hexRun.MatchString(strings.TrimRight(run, "=")) {
			continue
		}
		distinct := map[rune]bool{}
		for _, r := range run {
			distinct[r] = true
		}
		if len(distinct) > 4 {
			out = append(out, run)
		}
	}
	return out
}

// ipv6Addresses is every IPv6 literal in s: each run of hex digits, dots and colons with a colon in
// it that net/netip reads as an IPv6 address -- the short forms (::1, fe80::1), the IPv4-mapped ones
// in hex (::ffff:a01:203) or dotted, a zone. A clock time (06:48:50) or a ratio does not parse.
func ipv6Addresses(s string) []string {
	var out []string
	for _, tok := range ipv6Token.FindAllString(s, -1) {
		tok = strings.TrimRight(tok, ".:")
		if strings.Count(tok, ":") < 2 {
			continue
		}
		if a, err := netip.ParseAddr(tok); err == nil && a.Is6() {
			out = append(out, tok)
		}
	}
	return out
}

// TestGuide_NoSecretAddressOrPersonalData (M11 API-12; the repository is PUBLIC, CLAUDE.md §7, the
// brief): in either guide AND in openapi.yaml (served without a key like them) every e-mail address
// is at example.com; no IPv4 or IPv6 address (ipv6Addresses: every form netip reads); no key of any
// of the three prefixes with a body of more than four distinct characters (redline R7d's rule: the
// vector's 43 'A's are padding, not a key); every URL is on example.com -- the document's servers
// URL, the API's public address, is the one exception; no path of the source tree.
//
// MUTANTS (measured, API-12 report): a real-looking key, an address at another domain, an IPv4
// literal, a taptime URL and an internal/ path put into a guide -- each red. API-12 fix: ::1,
// ::ffff:a01:203 and fe80::1%eth0 in a guide, and an IPv4 literal in openapi.yaml -- each red (the
// first two, and anything in openapi.yaml, were green before). API-12 second fix: fe80::1%lo0 (a
// zone name of letters), a host name of a cluster or a private network without a scheme
// (*.svc.cluster.local, *.internal), a base64 value of 32 bytes (the KEK's shape) and a scripts/,
// deploy/ or /Users/ path in a guide -- each red (each green before).
func TestGuide_NoSecretAddressOrPersonalData(t *testing.T) {
	servers, _ := yat(openAPIDoc(t), "servers")
	list, _ := servers.([]any)
	if len(list) != 1 {
		t.Fatalf("the document's servers: %v, want one", servers)
	}
	server, _ := yat(list[0], "url")
	serverURL, _ := server.(string)
	serverHost := strings.TrimPrefix(serverURL, "https://")
	if serverHost == serverURL || serverHost == "" {
		t.Fatalf("the document's server URL %q is not https://<host>", serverURL)
	}
	for _, src := range []struct {
		name string
		b    []byte
	}{{"integration-guide.md", apidocs.English()}, {"integration-guide.tr.md", apidocs.Turkish()}, {"openapi.yaml", OpenAPI()}} {
		s := string(src.b)
		emails := emailShape.FindAllString(s, -1)
		if len(emails) == 0 {
			t.Fatalf("anti-vacuity: %s names no example address", src.name)
		}
		for _, e := range emails {
			if !strings.HasSuffix(e, "@example.com") {
				t.Errorf("%s: the e-mail address %q is not at example.com", src.name, e)
			}
		}
		for _, ip := range ipv4Shape.FindAllString(s, -1) {
			t.Errorf("%s: an IPv4 address %q", src.name, ip)
		}
		for _, ip := range ipv6Addresses(s) {
			t.Errorf("%s: an IPv6 address %q", src.name, ip)
		}
		for _, m := range keyShapeText.FindAllStringSubmatch(s, -1) {
			distinct := map[rune]bool{}
			for _, r := range m[2] {
				distinct[r] = true
			}
			if len(distinct) > 4 {
				t.Errorf("%s: a key-shaped value %.14q…", src.name, m[0])
			}
		}
		for _, m := range urlShape.FindAllStringSubmatch(s, -1) {
			if m[1] != "example.com" && !strings.HasSuffix(m[1], ".example.com") && (src.name != "openapi.yaml" || m[1] != serverHost) {
				t.Errorf("%s: a URL on %q", src.name, m[1])
			}
		}
		for _, banned := range []string{"internal/", ".go:", "cmd/tappa", "db/migrations", "scripts/", "deploy/", "/Users/", "/home/"} {
			if strings.Contains(s, banned) {
				t.Errorf("%s: names the source tree or a machine's path (%q)", src.name, banned)
			}
		}
		for _, h := range internalHost.FindAllString(s, -1) {
			t.Errorf("%s: an internal host name %q", src.name, h)
		}
		for _, b := range base64Secrets(s) {
			t.Errorf("%s: a base64 value long enough for a key %.8q…", src.name, b)
		}
	}
	// The detector itself: the forms a regular expression of groups missed.
	for _, ip := range []string{"::1", "::ffff:a01:203", "::ffff:10.1.2.3", "fe80::1%eth0", "2001:db8::7", "[2001:db8::7]:443"} {
		if len(ipv6Addresses("at "+ip+".")) != 1 {
			t.Errorf("ipv6Addresses misses %q", ip)
		}
	}
	for _, not := range []string{"06:48:50", "2026-10-16T08:23:20.461171Z", "sha256:abc", "U+200E", "1:20:30"} {
		if got := ipv6Addresses(not); len(got) != 0 {
			t.Errorf("ipv6Addresses reads %q as %v", not, got)
		}
	}
	for _, ip := range []string{"fe80::1%lo0", "fe80::1%en0.", "fe80::1%eth0"} {
		if len(ipv6Addresses("at "+ip)) != 1 {
			t.Errorf("ipv6Addresses misses %q", ip)
		}
	}
	for _, h := range []string{"postgres.tappa.svc.cluster.local", "db.cluster.local", "vault.internal", "nas.local", "localhost", "Build.Corp"} {
		if len(internalHost.FindAllString("at "+h+":5432.", -1)) != 1 {
			t.Errorf("internalHost misses %q", h)
		}
	}
	for _, not := range []string{"a local calendar month", "internal state", "hooks.example.com", "the cluster's inside"} {
		if got := internalHost.FindAllString(not, -1); len(got) != 0 {
			t.Errorf("internalHost reads %q as %v", not, got)
		}
	}
	// A made-up 32 bytes ("self-check: not a secret, 32 b!!"), in base64 with and without padding.
	for _, b := range []string{"c2VsZi1jaGVjazogbm90IGEgc2VjcmV0LCAzMiBiISE=", "c2VsZi1jaGVjazogbm90IGEgc2VjcmV0LCAzMiBiISE"} {
		if len(base64Secrets("KEK="+b+".")) != 1 {
			t.Errorf("base64Secrets misses %q", b)
		}
	}
	for _, not := range []string{"sha256=" + strings.Repeat("be925cb6", 8), strings.Repeat("A", 43), "emp_8973fbc8-5e76-48e2-a80b-67d5b10baa91"} {
		if got := base64Secrets(not); len(got) != 0 {
			t.Errorf("base64Secrets reads %q as %v", not, got)
		}
	}
}
