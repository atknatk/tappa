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
