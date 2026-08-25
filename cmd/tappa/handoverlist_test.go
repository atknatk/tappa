package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestHandoverList_TheBucketsCoverEveryItemExactlyOnce pins M8-05's hand-over list
// against its own summary.
//
// 🔴 IT EXISTS BECAUSE THAT ONE SUMMARY LINE HAS BEEN WRONG IN THREE CONSECUTIVE
// ROUNDS, EACH TIME DIFFERENTLY, AND EACH TIME AFTER SOMEBODY "FIXED" IT:
//
//	B2c-2a   the header said "hiçbiri kapatılmadı" while three items were stamped
//	         closed in the list below it.
//	B2c-2b   the summary said "AÇIK — on dokuz" and listed TWENTY numbers, because
//	         item 15 sat in the half-closed bucket AND the open one.
//	round 2  the repair corrected the lower block, left the UPPER label reading "on
//	         sekiz" over NINETEEN numbers, and published a mechanical check that
//	         verified the NUMBERS covered 1..25 — while never checking the label.
//
// That last one is the reason this test is written the way it is. Mechanising "the
// numbers are right" is not the same as mechanising "the summary is right", and the
// round that made the distinction still got it wrong. The card no longer spells any
// count in words; this reads the buckets and the item list and compares them.
//
// ⚠️ WHAT IT DOES NOT CHECK: whether an item is in the RIGHT bucket. That is a human
// judgement about whether something is closed, and no scan can make it. What it makes
// impossible is a bucket list that double-counts, drops, or invents an item.
const handoverCardPath = "docs/plan/m8-deploy-pilot.md"

// handoverItemCount is the number of items the hand-over list holds.
//
// 🔴 IT IS A RATCHET AND ITS DIRECTION IS THE POINT. The list is append-only in
// practice: a closed item keeps its number and gains a ✅, because the next session
// reads this list as the only record of what is outstanding. So the count may only be
// raised, and raising it is a line somebody writes on purpose. It replaced a floor of
// twenty, which let five of twenty-five items be deleted in silence.
//
// ⚠️ IT IS MAINTAINED BY HAND, AND THAT MAKES IT TECHNICALLY A SECOND COPY OF THE
// LIST — the defect class this task has been bitten by three times. It is accepted
// here for two reasons, both stated rather than assumed: the LIST is the source and
// this is only a gate on it, and the membership check below means a DELETION fails by
// NAME ("item 7 is MISSING") rather than by arithmetic. What the bare count buys on
// its own is the one case membership cannot see — an item appended without anybody
// deciding to. Adding an item is one line here; that is the whole cost.
const handoverItemCount = 28

// The three bucket labels, exactly as the card writes them.
const (
	bucketClosed = "> **KAPANDI:**"
	bucketHalf   = "> **YARISI KAPANDI:**"
	bucketOpen   = "> **TAM AÇIK:**"
)

func TestHandoverList_TheBucketsCoverEveryItemExactlyOnce(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot, handoverCardPath))
	if err != nil {
		t.Fatalf("reading %s: %v", handoverCardPath, err)
	}
	text := string(b)

	const head = "## 🔴 KAPATILMAMIŞ SAYIM"
	start := strings.Index(text, head)
	if start < 0 {
		t.Fatalf("%s no longer contains %q; the hand-over list is what a later session "+
			"reads, and a renamed heading makes this check silently vacuous",
			handoverCardPath, head)
	}
	section := text[start:]
	if end := strings.Index(section, "\n## "); end > 0 {
		section = section[:end]
	}

	// The items themselves: quoted list entries "> N." at the start of a line.
	itemRe := regexp.MustCompile(`(?m)^>\s*(\d+)\.\s`)
	items := map[int]int{}
	for _, m := range itemRe.FindAllStringSubmatch(section, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("unparsable item number %q", m[1])
		}
		items[n]++
	}
	// 🔴 A RATCHET, NOT A FLOOR — AND THE FLOOR IT REPLACES WAS BEATEN (audit, sixth
	// round). It read `len(items) < 20` while the list held 25, so FIVE items could be
	// deleted from both the list and their bucket and everything stayed green, printing
	// a self-consistent "20 items; coverage exact". This test's own comment names that
	// exact loss ("item 17 … went missing once already") and did not catch it.
	//
	// The list only ever GROWS: an item is closed in place, never removed.
	//
	// 🔴 THE SET IS CHECKED, NOT THE COUNT — AND THE COUNT-ONLY VERSION WAS BEATEN IN
	// ONE EDIT (audit, seventh round). Renumbering item 7 to 26 — delete from the
	// middle, append at the end — kept len(items) at 25 and the whole test PASSED,
	// printing "25 items; coverage exact" while §6 md. 12b dropped out of the record
	// entirely. That is precisely the class this ratchet was written to close, and the
	// mutation the previous round chose (deleting 21-25) was the one shape it already
	// caught.
	//
	// Membership over 1..N is a FINITE question, which is why it is the right one:
	// there is no spelling of "item 7 is gone" that leaves 7 present.
	if len(items) != handoverItemCount {
		t.Fatalf("the hand-over list holds %d numbered items, want %d.\n"+
			"MORE is fine — bump handoverItemCount and say what was added. FEWER means items "+
			"were DELETED, and this list is append-only: an item is marked closed in place, "+
			"never removed, because the next session reads it as the only record.", len(items),
			handoverItemCount)
	}
	for n := 1; n <= handoverItemCount; n++ {
		if items[n] == 0 {
			t.Errorf("item %d is MISSING from the hand-over list. Numbers are not reassigned "+
				"and items are not renumbered: the next session reads this list as the only "+
				"record of what is outstanding, and a number that vanishes takes its item with "+
				"it. If %d really is finished, mark it closed in place.", n, n)
		}
	}
	for n, count := range items {
		if count != 1 {
			t.Errorf("item %d is declared %d times in the hand-over list", n, count)
		}
	}

	buckets := map[string][]int{}
	all := map[int]string{}
	present := 0
	for _, label := range []string{bucketClosed, bucketHalf, bucketOpen} {
		// A bucket may legitimately be ABSENT — the half-closed one emptied in the
		// fourth round when its only member was re-labelled open — but a bucket that is
		// PRESENT and unreadable would make the coverage check below pass for the wrong
		// reason.
		if !strings.Contains(section, label) {
			continue
		}
		present++
		nums := handoverBucket(t, section, label)
		if len(nums) == 0 {
			t.Fatalf("bucket %q is present but no item numbers could be read from it", label)
		}
		buckets[label] = nums
		for _, n := range nums {
			if other, dup := all[n]; dup {
				t.Errorf("item %d is in %q AND %q. That is the exact defect this test exists "+
					"for: item 15 sat in two buckets and the summary counted 24 of 25",
					n, other, label)
				continue
			}
			all[n] = label
		}
	}

	if present < 2 {
		t.Fatalf("only %d bucket label(s) found in the hand-over list; with fewer than two the "+
			"summary is not summarising anything", present)
	}

	// Coverage: the buckets and the list must name the same set.
	var missing, extra []int
	for n := range items {
		if _, ok := all[n]; !ok {
			missing = append(missing, n)
		}
	}
	for n := range all {
		if items[n] == 0 {
			extra = append(extra, n)
		}
	}
	sort.Ints(missing)
	sort.Ints(extra)
	if len(missing) > 0 {
		t.Errorf("these items are in the list but in NO bucket: %v. An item that falls out of "+
			"the summary is an item the next session does not read — which is how item 17, "+
			"described at the time as 'this round's whole product', went missing once already",
			missing)
	}
	if len(extra) > 0 {
		t.Errorf("these numbers are in a bucket but are not items in the list: %v", extra)
	}

	// 🔴 THE PROSE HALF IS NOT CLOSED, AND THE GATE BELOW IS A SHAPE RULE RATHER THAN A
	// THIRD ATTEMPT AT ONE. Three designs tried to catch "a count spelled in words
	// beside a bucket" and each was beaten by a spelling the previous one had not
	// imagined:
	//
	//	DESIGN 1  match "— word" / "- word"      beaten by writing it without a dash
	//	DESIGN 2  scan label -> first digit      beaten TWICE, by putting the words
	//	                                         AFTER the digits and by putting them on
	//	                                         the line ABOVE the label
	//
	// "How does a number written in prose look" has no bottom — before the digits,
	// after them, above, below, in another language, `5` / `five` / `beş` / `V`,
	// "beş madde" or "maddelerin beşi". Improving the window a third time is the same
	// mistake a third time, and this task already applied the stopping rule to exactly
	// this class once (hand-over item 15).
	//
	// ✅ SO THE QUESTION IS SPLIT INSTEAD, and both halves are stated:
	//
	//	MECHANICAL  the item SET, coverage, double counting, an item in no bucket, a
	//	            bucket naming a number that is not an item, and — below — the
	//	            CONTENT OF THE BUCKET LINE ITSELF, as a permitted character set.
	//	            All of these are digits, and digits are countable.
	//	NOT BOUND   prose anywhere else on the card. That is ordinary markdown on a
	//	            six-thousand-line file and no scan owns it. COUNTED, not closed —
	//	            hand-over item 26.
	//
	// The shape rule below is finite because it is an ALLOW-LIST of characters: a
	// bucket line may contain its label, digits, the separator, and nothing else. A
	// number spelled in words cannot be written without letters.
	for _, label := range []string{bucketClosed, bucketHalf, bucketOpen} {
		line := handoverBucketLine(section, label)
		if line == "" {
			continue
		}
		body := line[strings.Index(line, label)+len(label):]
		// Labelled because the break below must leave the RUNE LOOP: a bare break
		// inside a switch leaves only the switch, so the first version reported every
		// offending rune on the line instead of the first (staticcheck SA4011/S1023).
	scan:
		for _, r := range body {
			switch {
			case r >= '0' && r <= '9', r == '·', r == ' ', r == '.', r == '\n', r == '>', r == '\t':
				continue
			default:
				t.Errorf("bucket %q carries %q in its own line, which may hold only its label, "+
					"item numbers and separators.\n"+
					"Three rounds published a WRONG count in words beside a bucket while the "+
					"numbers were right; the card states the numbers and lets this test do the "+
					"counting.\nafter the label: %q", label, r, body)
				break scan
			}
		}
	}

	// Guarded: "coverage exact" printed under a failing run is a verdict contradicting
	// the errors above it. Measured on this test's own mutations — it said "coverage
	// exact" beneath "these items are in the list but in NO bucket: [26]".
	if !t.Failed() {
		t.Logf("hand-over list: %d items; buckets closed=%d half=%d open=%d; coverage exact",
			len(items), len(buckets[bucketClosed]), len(buckets[bucketHalf]), len(buckets[bucketOpen]))
	}
}

// handoverBucketLine returns the card line that opens a bucket, plus its continuation
// lines up to the next blank quoted line — the numbers wrap.
func handoverBucketLine(section, label string) string {
	at := strings.Index(section, label)
	if at < 0 {
		return ""
	}
	rest := section[at:]
	var out []string
	for _, line := range strings.Split(rest, "\n") {
		out = append(out, line)
		if strings.Contains(line, ".") && !strings.HasSuffix(strings.TrimSpace(line), "·") {
			// A bucket ends at the first line closing with a full stop.
			break
		}
		if len(out) > 6 {
			break
		}
	}
	return strings.Join(out, " ")
}

// handoverBucket parses the item numbers listed under one bucket label.
func handoverBucket(t *testing.T, section, label string) []int {
	t.Helper()
	line := handoverBucketLine(section, label)
	if line == "" {
		return nil
	}
	line = strings.TrimPrefix(line, label)
	// Stop at the next bucket label if the wrap ran into it.
	for _, other := range []string{bucketClosed, bucketHalf, bucketOpen} {
		if other == label {
			continue
		}
		if at := strings.Index(line, other); at >= 0 {
			line = line[:at]
		}
	}
	numRe := regexp.MustCompile(`\b(\d+)\b`)
	var out []int
	for _, m := range numRe.FindAllStringSubmatch(line, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 || n > 99 {
			continue
		}
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}
