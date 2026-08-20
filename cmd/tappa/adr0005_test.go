package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The acceptance table M8-04 FAZ B3 wrote into ADR 0005, and the four claims this
// file turns from prose into a gate.
//
// 🔴 WHY THIS EXISTS. The card's remaining criterion was that accepted MEDIUM/LOW
// findings be "accepted with a reason AND WRITTEN DOWN", anchored to something that
// goes red if the acceptance stops holding. B3 wrote the table and anchored most rows
// to a test name, which TestEveryNamedTestExists keeps honest — a cited test that is
// deleted or renamed turns the document red.
//
// It left two statements about that table unguarded, and BOTH were wrong when
// measured on the tree that shipped them (2026-08-20):
//
//   - the section header said "EVERY ROW CARRIES A MECHANICAL ANCHOR, AND THE ANCHOR
//     IS A TEST NAME", and thirty lines below the same document said "AND THREE ROWS
//     HAVE NO MECHANICAL ANCHOR". A document refuting itself in one screen.
//   - the M8-04 card called the table "eleven rows". It had twelve.
//
// Neither broke the product and neither broke the net: every row that claimed a test
// name really had one. What was false was the SENTENCE ABOUT THE NET — the defect
// class this task hit seven times, and the reason CLAUDE.md's working rule is that a
// number is either BOUND, DATED, or DELETED. These numbers are now bound.
//
// 🔴 WHAT THIS PINS THAT TestEveryNamedTestExists CANNOT. That check asks "does every
// cited name exist". It cannot ask "does every row that is SUPPOSED to cite a name
// actually cite one" — a row whose anchor cell were quietly emptied would cite
// nothing, so nothing would dangle, and the check would stay green while the
// acceptance lost its anchor. Claim 4 below is exactly that hole.
const (
	adr0005Path = "docs/adr/0005-kabul-edilen-riskler.md"

	// adr0005SectionHead and adr0005SectionEnd bracket the B3 table. The end marker is
	// the paragraph that follows it, so a row appended after the table is still counted.
	adr0005SectionHead = "## Kabul edilen ORTA/DÜŞÜK denetim bulguları"
	adr0005SectionEnd  = "**Bu turda KAPATILANLAR**"

	// adr0005NoAnchorStamp is the ONE spelling a row uses to say it has no mechanical
	// anchor.
	//
	// 🔴 IT IS ONE LITERAL BECAUSE THE THREE ROWS USED TO SPELL IT TWO WAYS. T38 and T39
	// said "MEKANİK ÇAPASI YOK, SAYILDI"; T28 said "bu satırın MEKANİK bir çapası YOKTUR
	// ve bu sayılmıştır" — same meaning, and a count of the first spelling returned 2
	// while the prose said 3. Defining a set by listing the ways people happen to write
	// it is the same defect this task met in netx (a space defined by naming blocks);
	// the repair is the same shape: one canonical form, mechanically checkable.
	adr0005NoAnchorStamp = "MEKANİK ÇAPASI YOK, SAYILDI"
)

// adr0005Row is one line of the acceptance table.
type adr0005Row struct {
	label    string // "T27", "T48 (3)"
	anchor   string // the last cell
	stamped  bool   // carries adr0005NoAnchorStamp
	lineNo   int
	rawCells int
}

// readADR0005Rows slices the B3 table out of the ADR and parses its rows.
func readADR0005Rows(t *testing.T) (section string, rows []adr0005Row) {
	t.Helper()

	path := filepath.Join(repoRoot, adr0005Path)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", adr0005Path, err)
	}
	text := string(b)

	start := strings.Index(text, adr0005SectionHead)
	if start < 0 {
		t.Fatalf("%s no longer contains the section %q. The acceptance texts for M8-04 "+
			"FAZ B3 live under that heading; if it was renamed, this gate and the card's "+
			"criterion both need to follow it.", adr0005Path, adr0005SectionHead)
	}
	rest := text[start:]
	end := strings.Index(rest, adr0005SectionEnd)
	if end < 0 {
		t.Fatalf("%s: the section %q is not terminated by %q, so the table cannot be "+
			"bounded and every count below would be measuring the rest of the file.",
			adr0005Path, adr0005SectionHead, adr0005SectionEnd)
	}
	section = rest[:end]

	labelRe := regexp.MustCompile(`^\|\s*\*\*(T\d+(?:\s*\(\d+\))?)\*\*`)
	lineNo := strings.Count(text[:start], "\n")
	for _, line := range strings.Split(section, "\n") {
		lineNo++
		if !strings.HasPrefix(line, "| **T") {
			continue
		}
		m := labelRe.FindStringSubmatch(line)
		if m == nil {
			t.Errorf("%s:%d starts a table row but no finding label could be read from it: %.80s",
				adr0005Path, lineNo, line)
			continue
		}
		cells := strings.Split(line, "|")
		row := adr0005Row{
			label:    strings.Join(strings.Fields(m[1]), " "),
			stamped:  strings.Contains(line, adr0005NoAnchorStamp),
			lineNo:   lineNo,
			rawCells: len(cells),
		}
		// A row is `| finding | accepted | why not closed | anchor |` — six fields once
		// split, because the leading and trailing pipes each produce an empty one. The
		// shape is asserted rather than assumed: a row that grew or lost a column would
		// otherwise make `anchor` silently the wrong cell.
		if len(cells) == 6 {
			row.anchor = strings.TrimSpace(cells[4])
		}
		rows = append(rows, row)
	}
	return section, rows
}

// TestADR0005_TheAnchorCountsMatchTheProse binds the four statements ADR 0005 makes
// about its own acceptance table to the table itself.
func TestADR0005_TheAnchorCountsMatchTheProse(t *testing.T) {
	section, rows := readADR0005Rows(t)

	// CONTROL: the slice really found a table. Without this every count below could be
	// zero-versus-zero and the gate would pass while measuring nothing.
	if len(rows) == 0 {
		t.Fatal("CONTROL FAILED: no table rows were parsed out of the section, so nothing " +
			"below is being measured")
	}

	for _, r := range rows {
		if r.rawCells != 6 {
			t.Errorf("%s:%d row %s has %d pipe-separated fields, want 6. The anchor is read "+
				"from the last cell; a row with a different shape is read wrong.",
				adr0005Path, r.lineNo, r.label, r.rawCells)
		}
	}

	// --- claim 1: "**satır = N**" ---------------------------------------------------
	//
	// 🔴 THE ** ARE PART OF THE PATTERN AND THAT IS THE FIX FOR AN AUDIT FINDING (D45).
	// The document states both counts in one sentence — "**satır = N** · **çapasız
	// satır = M**" — and the bare pattern `satır = (\d+)` matches INSIDE the second
	// one too. It read the right number only because Go's regexp returns the LEFTMOST
	// match and the two happen to be written in that order: swap them in the prose and
	// claim 1 silently binds itself to the unanchored-row count instead. Measured by
	// transposing the two expressions in the ADR and running both patterns against it:
	// the bare one reported "holds 3 rows", the bolded one still read the row count.
	// The bold markers make the two claims lexically distinct, which is what the two
	// claims already are.
	wantRows := adr0005ProseNumber(t, section, `\*\*satır = (\d+)\*\*`)
	if len(rows) != wantRows {
		t.Errorf("🔴 %s says the acceptance table holds %d rows; it holds %d. "+
			"The number is written in the document and this test is what makes it true — "+
			"update both together, or the document is once again describing a table that "+
			"is not there. (The card called it eleven when it was twelve; that is why this "+
			"gate exists.)", adr0005Path, wantRows, len(rows))
	}

	// --- claim 2: "**çapasız satır = N**" -------------------------------------------
	//
	// Bolded for the same reason as claim 1, though this half was never ambiguous:
	// "çapasız satır" cannot appear inside "satır". It is pinned the same way so the
	// two patterns cannot drift into different rules for the same sentence.
	var stamped []string
	for _, r := range rows {
		if r.stamped {
			stamped = append(stamped, r.label)
		}
	}
	wantStamped := adr0005ProseNumber(t, section, `\*\*çapasız satır = (\d+)\*\*`)
	if len(stamped) != wantStamped {
		t.Errorf("🔴 %s says %d rows carry no mechanical anchor; %d rows carry the stamp %q (%s). "+
			"Either a row lost its anchor without the prose noticing, or an anchor was found "+
			"for a row still marked as having none.",
			adr0005Path, wantStamped, len(stamped), adr0005NoAnchorStamp, strings.Join(stamped, ", "))
	}

	// --- claim 3: the unanchored rows are NAMED, and the names are the real ones ----
	//
	// A count alone would let one row quietly trade places with another: close T38 and
	// stop anchoring T51 and the count is still three. The document names them, so the
	// names are what is checked.
	wantNames := adr0005ProseNames(t, section)
	if got, want := strings.Join(stamped, " · "), strings.Join(wantNames, " · "); got != want {
		t.Errorf("🔴 %s names [%s] as the rows with no mechanical anchor, but the table stamps [%s]. "+
			"A count would not have caught this: a row can lose its anchor while another "+
			"gains one and the total never moves.", adr0005Path, want, got)
	}

	// --- claim 4: every OTHER row really cites a test name --------------------------
	//
	// 🔴 THIS IS THE HOLE TestEveryNamedTestExists CANNOT SEE. That check verifies that
	// cited names exist; it cannot verify that a row which is supposed to cite one does.
	// An anchor cell emptied to "-" would dangle nothing and stay green there, while the
	// acceptance it was supposed to hold up would be unanchored.
	citeRe := regexp.MustCompile(`\bTest[A-Z][A-Za-z0-9_]{4,}\b`)
	for _, r := range rows {
		if r.stamped {
			continue
		}
		if !citeRe.MatchString(r.anchor) {
			t.Errorf("🔴 %s:%d row %s is not stamped %q, so it claims a mechanical anchor — but its "+
				"anchor cell names no test: %q. Either cite the test that holds the "+
				"acceptance, or stamp the row and say plainly that nothing does.",
				adr0005Path, r.lineNo, r.label, adr0005NoAnchorStamp, r.anchor)
		}
	}

	t.Logf("%d rows, %d without a mechanical anchor (%s)", len(rows), len(stamped),
		strings.Join(stamped, ", "))
}

// adr0005ProseNumber reads a decimal the document states about itself.
func adr0005ProseNumber(t *testing.T, section, pattern string) int {
	t.Helper()
	m := regexp.MustCompile(pattern).FindStringSubmatch(section)
	if m == nil {
		t.Fatalf("%s no longer states its own count in the form %q. The count is the thing "+
			"this test binds; if it is gone, the document is carrying an unguarded claim "+
			"again or the claim was deleted without deleting this check.", adr0005Path, pattern)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("%s: %q did not parse as a number: %v", adr0005Path, m[1], err)
	}
	return n
}

// adr0005ProseNames reads the finding labels the document lists as unanchored.
func adr0005ProseNames(t *testing.T, section string) []string {
	t.Helper()
	m := regexp.MustCompile(`çapasızlar tam olarak \*\*([^*]+)\*\*`).FindStringSubmatch(section)
	if m == nil {
		t.Fatalf("%s no longer names the rows that have no mechanical anchor. The sentence "+
			"is the claim being bound; a count on its own lets one row trade places with "+
			"another.", adr0005Path)
	}
	var out []string
	for _, part := range strings.Split(m[1], "·") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// adr0005MainTableHead and adr0005MainTableEnd bracket the MAIN accepted-risk table
// (the numbered one under "## Karar"), which is a DIFFERENT table from the M8-04 B3
// one above and is parsed by a different function for that reason.
const (
	adr0005MainTableHead = "## Karar"
	adr0005MainTableEnd  = "Referanslanan `sid`'ler kodda gerçektir"
)

// adr0005MainProseCount reads the Turkish number word the ADR uses for its own risk
// count. A word and not a digit, because that is how the document is written
// ("Aşağıdaki **sekiz risk** kabul edilir") and rewriting the prose to suit the test
// would be the test dictating the document.
var adr0005NumberWords = map[string]int{
	"bir": 1, "iki": 2, "üç": 3, "dört": 4, "beş": 5, "altı": 6,
	"yedi": 7, "sekiz": 8, "dokuz": 9, "on": 10, "on bir": 11, "on iki": 12,
}

// TestADR0005_TheRiskCountMatchesTheTable binds the ONE number the main table had no
// gate for.
//
// 🔴 WHY IT EXISTS, and it is not a hypothetical. The sibling gate above
// (TestADR0005_TheAnchorCountsMatchTheProse) slices only the M8-04 B3 section,
// which sits ~450 lines below this table — so
// when M8-05 appended risks 7 and 8 on 2026-08-20 the sentence "Aşağıdaki **altı
// risk** kabul edilir" was left describing a table that now held eight, and every
// existing gate stayed green. Four of that round's blocking findings were the same
// class in other files: a count copied into prose and left behind. This repository's
// rule is that a number is BOUND, DATED, or DELETED; this one is now bound.
//
// It deliberately does NOT re-check anchors, cell shape or names — that is the other
// test's job and duplicating it would give two places to update for one change.
func TestADR0005_TheRiskCountMatchesTheTable(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot, adr0005Path))
	if err != nil {
		t.Fatalf("reading %s: %v", adr0005Path, err)
	}
	text := string(b)

	start := strings.Index(text, adr0005MainTableHead)
	if start < 0 {
		t.Fatalf("%s no longer contains %q", adr0005Path, adr0005MainTableHead)
	}
	rest := text[start:]
	end := strings.Index(rest, adr0005MainTableEnd)
	if end < 0 {
		t.Fatalf("%s: the main risk table is not terminated by %q, so any count below "+
			"would be measuring the rest of the file", adr0005Path, adr0005MainTableEnd)
	}
	section := rest[:end]

	// Rows are `| 7 | **Risk** | ... |`. The leading digit is what numbers a risk.
	//
	// ONE ROW PATTERN, PLUS THE TWO LINES THAT ARE NOT ROWS. Every pipe-led line in the
	// slice must be the header, the separator, or a row of that exact form; anything
	// else fails below. There is no second "loose" row pattern and no comparison of two
	// PATTERN totals — that WAS the design for one round and it is gone, because
	// counting patterns kept leaving holes (the history is at the failure site below).
	// What IS compared, further down, is the row count against the number the prose
	// states.
	//
	// 🔴 COUNTED ESCAPE, NAMED RATHER THAN CLOSED — AND IT IS THE HEAVIER DIRECTION.
	// "Pipe-led" is the literal truth of the scan and it is also a HOLE, not only the
	// convenience it looks like. GitHub-flavoured Markdown makes the leading and
	// trailing pipes OPTIONAL on body rows, so a line written
	//
	//	9 | **Ninth risk, no leading pipe** | why | signal | task |
	//
	// RENDERS as a ninth row of this very table and this scan NEVER SEES IT: the
	// loop skips any line that does not start with a pipe. Measured 2026-08-20 (eighth
	// audit, reproduced here): with that line appended the gate stays GREEN at "8 rows,
	// prose says sekiz".
	//
	// DIRECTION: A MISSED ROW. That is the opposite of the limit recorded at the
	// failure site below (a second table makes this gate blame the wrong one — a false
	// alarm), and it is WORSE, because a missed row is exactly the defect class this
	// gate exists for: an accepted risk that the count does not cover.
	//
	// WHY IT IS NOT CLOSED: closing it means moving the scan toward GFM's real table
	// grammar (optional delimiters, escaped pipes, alignment rows), which is a parser,
	// not a guard. That is more than this gate is worth today. It is recorded so the
	// next reader knows the boundary is a boundary and not an oversight — and so that
	// nobody reads "every pipe-led line" as "every row".
	//
	// 🔴 AND THE COMMENT THAT DESCRIBED THE OLD DESIGN OUTLIVED IT BY A ROUND, WHICH IS
	// THE SECOND TIME IN TWO ROUNDS THAT THIS TASK DELETED A MECHANISM AND LEFT ITS
	// PROSE STANDING. The first was an AN12196 table whose withdrawn completeness claim
	// stayed in its own heading while the retraction sat thirty-six lines above it; the
	// second was this block, still announcing a `looseRe` that no longer exists, while
	// the corrected explanation sat seventeen lines below. Both times the WRONG half was
	// the one a reader meets first.
	//
	// THE RULE THIS TASK LEARNED, WRITTEN WHERE IT WAS BROKEN: when you remove a
	// mechanism, remove the text that DESCRIBES it in the same edit, and prove it with a
	// grep for the removed identifier. A retraction that lives away from the retracted
	// sentence is not a retraction; a comment is not documentation of intent, it is a
	// claim about the code beside it.
	rowRe := regexp.MustCompile(`^\|\s*(\d+)\s*\|\s*\*\*`)
	headerRe := regexp.MustCompile(`^\|\s*#\s*\|`)
	sepRe := regexp.MustCompile(`^\|[-:\s|]+\|\s*$`)

	var nums []string
	for i, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		line = strings.TrimSpace(line)
		switch {
		case headerRe.MatchString(line), sepRe.MatchString(line):
			continue
		}
		m := rowRe.FindStringSubmatch(line)
		if m == nil {
			// 🔴 EVERY PIPE LINE IS EITHER THE HEADER, THE SEPARATOR, OR A WELL-FORMED
			// ROW — a positive shape assertion, and it is positive BECAUSE COUNTING
			// PATTERNS LEFT HOLES. Two rounds of audit mutation measured them: a row
			// numbered but NOT bold was caught by an added second pattern, and then a
			// row BOLD BUT NOT NUMBERED sailed through both, because neither pattern
			// described what a row must look like — only what some rows do look like.
			// A table row that matches no pattern is now a failure by construction,
			// which is the only shape that does not need a new pattern per new defect.
			//
			// ⚠️ COUNTED LIMIT: THE SLICE IS ASSUMED TO HOLD EXACTLY ONE PIPE TABLE, and
			// today it does. Measured by an audit: drop a SECOND well-formed table into
			// the slice and this check fails — correctly, in the sense that it fires,
			// but it blames "the main risk table" for a line that is not in it. The
			// direction is the safe one (a false alarm, never a missed row) and the
			// offending line is printed, so a reader is one look from the truth. Fixing
			// it properly means bounding the table itself rather than the section, which
			// is a bigger change than this gate is worth today; recorded rather than
			// pretended away. (Pipes inside PROSE are unaffected -- the scan only looks
			// at lines that START with one. ⚠️ That same "pipe-led" restriction is ALSO
			// an escape in the other direction, and it is named at the top of this
			// function: a body row written WITHOUT its leading pipe renders as a row and
			// is never scanned. Do not read this parenthesis as the whole story.)
			// 🔴 THE NUMBER IS COUNTED FROM THE START OF THE SLICE, NOT FROM THE FILE,
			// and it says so — because the sibling parser in this same file
			// (readADR0005Rows) reports ABSOLUTE file lines, so one file carries two
			// bases and a reader who assumes the wrong one looks in the wrong place.
			// The offending text is printed alongside precisely so the number never has
			// to be trusted on its own.
			t.Errorf("🔴 %s: line %d OF THE SLICE (counting from %q, not from the top of "+
				"the file) matches neither the header, the separator, nor the row form "+
				"`| N | **Risk** | … |`:\n    %.100s\n"+
				"A row the counter cannot read is a risk the count does not cover, and "+
				"the prose above would stay 'right' about a table that had grown.",
				adr0005Path, i+1, adr0005MainTableHead, line)
			continue
		}
		nums = append(nums, m[1])
	}
	if len(nums) == 0 {
		t.Fatal("CONTROL FAILED: no numbered rows were parsed out of the main risk table, " +
			"so nothing below is being measured")
	}

	// The numbering must be dense and start at 1: a gap or a duplicate would make the
	// count agree with the prose while the table itself was wrong.
	for i, n := range nums {
		got, err := strconv.Atoi(n)
		if err != nil || got != i+1 {
			t.Errorf("%s: main risk table row %d is numbered %q; the rows must run 1..N "+
				"without gaps or repeats, otherwise 'N risk' is true of the count and "+
				"false of the table", adr0005Path, i+1, n)
		}
	}

	proseRe := regexp.MustCompile(`Aşağıdaki \*\*(\p{L}+(?: \p{L}+)?) risk\*\* kabul edilir`)
	pm := proseRe.FindStringSubmatch(section)
	if pm == nil {
		t.Fatalf("%s: could not find the sentence that states the risk count "+
			`("Aşağıdaki **N risk** kabul edilir"). If it was reworded, this gate has to `+
			"follow it — a count nobody checks is how risks 7 and 8 shipped under the "+
			"word 'altı'.", adr0005Path)
	}
	want, ok := adr0005NumberWords[strings.ToLower(pm[1])]
	if !ok {
		t.Fatalf("%s says %q risks are accepted and this test does not know that number "+
			"word. Add it to adr0005NumberWords.", adr0005Path, pm[1])
	}
	if len(nums) != want {
		t.Errorf("🔴 %s says %q (%d) risks are accepted; the main table holds %d rows. "+
			"Append rule: a new accepted risk is added to THAT table and to this sentence "+
			"in the same change.", adr0005Path, pm[1], want, len(nums))
	}
	t.Logf("main risk table: %d rows, prose says %q (%d)", len(nums), pm[1], want)
}
