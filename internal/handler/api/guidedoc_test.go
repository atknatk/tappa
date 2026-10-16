package api

// guidedoc_test.go -- the integration guide (docs/api/integration-guide.md and its Turkish
// translation) as a contract (M11 API-12; plan §2, plan §5 API-12; ADR 0028 "API-12 uygulama notu"):
//
//	TestGuide_TheTwoLanguagesAreOneDocument       the heading tree, the anchors, the numbered items,
//	                                              the code blocks (byte for byte), the link targets,
//	                                              the markers, the tables' code-and-number cells and
//	                                              each section's inline code are the same in both
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
	"encoding/json"
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	apidocs "github.com/atknatk/tappa/docs/api"
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
)

func readGuide(t *testing.T, name string, src []byte) mdDoc {
	t.Helper()
	d := mdDoc{name: name, lines: strings.Split(string(src), "\n"), anchors: map[string]bool{}, code: map[string][]string{}}
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
// code token renamed in one section -- each red.
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

var (
	emailShape   = regexp.MustCompile(`[A-Za-z0-9._%+-]+@([A-Za-z0-9-]+\.)+[A-Za-z]{2,}`)
	ipv4Shape    = regexp.MustCompile(`\b([0-9]{1,3}\.){3}[0-9]{1,3}\b`)
	ipv6Shape    = regexp.MustCompile(`(?i)\b[0-9a-f]{1,4}(:[0-9a-f]{0,4}){4,}\b|(?i)\b[0-9a-f]{1,4}::[0-9a-f:]*\b`)
	keyShapeText = regexp.MustCompile(`tt_(live|test|whsec)_([A-Za-z0-9_-]{32,})`)
	urlShape     = regexp.MustCompile(`https?://([A-Za-z0-9.-]+)`)
)

// TestGuide_NoSecretAddressOrPersonalData (M11 API-12; the repository is PUBLIC, CLAUDE.md §7, the
// brief): in either guide every e-mail address is at example.com; no IPv4 or IPv6 address; no key of
// any of the three prefixes with a body of more than four distinct characters (redline R7d's rule:
// the vector's 43 'A's are padding, not a key); every URL is on example.com; no path of the source
// tree.
//
// MUTANTS (measured, API-12 report): a real-looking key, an address at another domain, an IPv4
// literal, a taptime URL and an internal/ path put into a guide -- each red.
func TestGuide_NoSecretAddressOrPersonalData(t *testing.T) {
	for _, src := range []struct {
		name string
		b    []byte
	}{{"integration-guide.md", apidocs.English()}, {"integration-guide.tr.md", apidocs.Turkish()}} {
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
		for _, ip := range ipv6Shape.FindAllString(s, -1) {
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
			if m[1] != "example.com" && !strings.HasSuffix(m[1], ".example.com") {
				t.Errorf("%s: a URL on %q", src.name, m[1])
			}
		}
		for _, banned := range []string{"internal/", ".go:", "cmd/tappa", "db/migrations"} {
			if strings.Contains(s, banned) {
				t.Errorf("%s: names the source tree (%q)", src.name, banned)
			}
		}
	}
}
