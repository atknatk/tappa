package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestEveryNamedTestExists — the class-closer for a defect that shipped twice in
// one change set.
//
// 🔴 WHAT HAPPENED. Comments and documentation in this repository cite test names
// as evidence ("this is pinned by TestX"), and two of those citations named tests
// that DID NOT EXIST:
//
//   - cmd/rotatekek/main.go's writeAll comment cited
//     TestRun_AFailedWriteIsARefusalNotASuccess. The test had been written, then
//     deleted by a later edit that replaced everything after one function; the
//     comment survived and kept vouching for it. An auditor then reverted the
//     behaviour the test guarded and the suite stayed GREEN.
//   - deploy/README.md and the M8-02 card cited a runbook test under a name it
//     never had (the real one is TestRunbook_InvokesTheToolSoEveryExitCodeIsReadable),
//     and the card additionally claimed a MEASUREMENT about that name ("it was red
//     the moment it was written").
//
// The dead identifier is deliberately NOT written out here, which is the discipline
// this check enforces: describe a citation you cannot honour, do not spell it.
//
// A citation is the cheapest kind of evidence to produce and the easiest to leave
// behind. This makes it mechanical: any Test… identifier written in a Go comment,
// a Markdown file or a shell script under the scanned roots must exist as a real
// test function.
func TestEveryNamedTestExists(t *testing.T) {
	// 1. Collect every declared test function.
	declared := map[string]bool{}
	// 🔴 THE DECLARATION SCAN IGNORES COMMENTED-OUT CODE. `^func Test…(` matches
	// inside a /* */ block too, so the original defect — a deleted test with a
	// surviving citation — stayed reachable by COMMENTING OUT rather than deleting.
	declRe := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
	stripBlockComments := func(src string) string {
		return regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(src, "")
	}
	walk := func(root string, fn func(path string, b []byte)) {
		err := filepath.Walk(filepath.Join(repoRoot, root), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			fn(path, b)
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", root, err)
		}
	}
	for _, root := range []string{"cmd", "internal", "web", "test"} {
		walk(root, func(path string, b []byte) {
			if !strings.HasSuffix(path, "_test.go") {
				return
			}
			for _, m := range declRe.FindAllStringSubmatch(stripBlockComments(string(b)), -1) {
				declared[m[1]] = true
			}
		})
	}
	// CONTROL: the collector found a realistic number, and specifically this test.
	if len(declared) < 100 {
		t.Fatalf("only %d test functions found; the collector has gone blind", len(declared))
	}
	if !declared["TestEveryNamedTestExists"] {
		t.Fatal("CONTROL FAILED: the collector did not find this very test")
	}
	t.Logf("%d test functions declared", len(declared))

	// 2. Collect every Test… name CITED outside a declaration.
	citeRe := regexp.MustCompile(`\bTest[A-Z][A-Za-z0-9_]{4,}\b`)
	type cite struct{ file, name string }
	var cites []cite
	add := func(path string, b []byte) {
		text := string(b)
		for _, line := range strings.Split(text, "\n") {
			trimmed := strings.TrimSpace(line)
			// Skip declarations themselves.
			if strings.HasPrefix(trimmed, "func Test") {
				continue
			}
			for _, name := range citeRe.FindAllString(line, -1) {
				cites = append(cites, cite{path, name})
			}
		}
	}
	// 🔴 THE SCOPE WAS FIVE DIRECTORIES AND FOUR EXTENSIONS, AND THE GAPS WERE
	// MEASURED: the same dangling citation was RED in docs/plan but GREEN in
	// docs/adr (where CLAUDE.md §10 requires ADRs to live), GREEN in CLAUDE.md
	// itself, and GREEN in .github/workflows/ci.yml because .yml was not in the
	// extension list. A checker that closes five directories has not closed a class.
	for _, root := range []string{"cmd", "internal", "deploy", "docs", "scripts", ".github"} {
		walk(root, func(path string, b []byte) {
			switch {
			case strings.HasSuffix(path, ".go"),
				strings.HasSuffix(path, ".md"),
				strings.HasSuffix(path, ".sh"),
				strings.HasSuffix(path, ".yaml"),
				strings.HasSuffix(path, ".yml"):
				add(path, b)
			}
		})
	}
	if b, err := os.ReadFile(filepath.Join(repoRoot, "CLAUDE.md")); err == nil {
		add(filepath.Join(repoRoot, "CLAUDE.md"), b)
	}

	// The inventory lives in testdata (not scanned), so its entries cannot keep
	// themselves alive.
	known := map[string]bool{}
	invPath := filepath.Join(repoRoot, "cmd", "tappa", "testdata", "known-dangling-citations.txt")
	invBytes, err := os.ReadFile(invPath)
	if err != nil {
		t.Fatalf("reading the known-dangling inventory: %v", err)
	}
	for _, ln := range strings.Split(string(invBytes), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		known[ln] = true
	}
	// THE BUDGET RATCHETS DOWN: it is the CURRENT size, so any addition fails until
	// somebody edits this number, which is visible in review. A comment saying
	// "dated 2026-08-17" was not a mechanism; this is.
	// 🔴 `!=`, NOT `>`. With `>` the budget was a HIGH-WATER MARK: measured, repairing
	// one citation and deleting its line passed at 26/27, and then a BRAND NEW
	// dangling citation plus a new inventory line passed again at 27/27 — a
	// permanent exemption bought without editing a single Go file. The file's own
	// comment called it "the CURRENT size, so any addition fails"; that was only
	// true with equality.
	if err := ratchetOK(len(known), danglingBudget); err != nil {
		t.Fatalf("%v", err)
	}
	if len(cites) < 5 {
		t.Fatalf("only %d citations found; the citation scan has gone blind", len(cites))
	}
	t.Logf("%d citations of test names outside declarations", len(cites))

	// 3. Every citation must resolve.
	// A citation may name a FAMILY rather than one test — internal/domain/tap cites
	// "TestDecide_" for the whole §5 table, and internal/handler cites "TestAccount".
	// Those are legitimate and pre-date this check, so a citation resolves when it
	// is an exact name OR a prefix of one. A name that is neither still fails —
	// which is what caught the two dead citations described above.
	// 🔴 PREFIX MATCHING IS LIMITED TO EXPLICIT FAMILY CITATIONS (those ending in
	// "_"). The open prefix rule made the MOST COMMON rename invisible: renaming
	// TestX to TestX_v2 left a citation of TestX resolving, because the new name
	// has the old one as a prefix. Measured: exactly that mutation stayed green
	// across 2 513 citations — and "a citation that resolves to nothing" is the
	// class this whole check exists for, which it hit twice in this task.
	resolves := func(name string) bool {
		if declared[name] {
			return true
		}
		if !strings.HasSuffix(name, "_") {
			return false
		}
		for d := range declared {
			if strings.HasPrefix(d, name) {
				return true
			}
		}
		return false
	}
	seen := map[string]bool{}
	var dangling []string
	for _, c := range cites {
		if resolves(c.name) || seen[c.name] {
			continue
		}
		seen[c.name] = true
		dangling = append(dangling, c.name)
		if !known[c.name] {
			rel, _ := filepath.Rel(repoRoot, c.file)
			t.Errorf("%s cites %s, which is not a test that exists. A citation is evidence; a citation to "+
				"nothing is worse than none, because it stops anybody looking for the real pin.", rel, c.name)
		}
	}

	// THE INVENTORY IS A RATCHET: IT MAY ONLY SHRINK. An entry that no longer
	// dangles must be removed, or the list rots into a permanent exemption and
	// stops meaning anything.
	live := map[string]bool{}
	for _, d := range dangling {
		live[d] = true
	}
	// STALENESS, BOTH WAYS. An entry that no longer dangles must be deleted — and
	// this now actually fires, because the inventory lives in testdata and no longer
	// keeps its own names alive by citing them.
	for name := range known {
		if !live[name] {
			t.Errorf("%s is listed in cmd/tappa/testdata/known-dangling-citations.txt but now resolves. "+
				"Delete that line and lower danglingBudget — an exemption nobody removes is an "+
				"exemption nobody reads.", name)
		}
	}
	t.Logf("dangling citations: %d live, %d budgeted", len(dangling), danglingBudget)
}

const danglingBudget = 53

// ratchetOK enforces that the inventory size EQUALS the budget.
//
// 🔴 A FUNCTION, BECAUSE THE INLINE VERSION WAS ITS OWN ORACLE. With `>` the budget
// was a high-water mark and a brand-new dangling citation could buy a permanent
// exemption without touching a Go file; the fix to `!=` then could not be pinned
// inline, because today size == budget and BOTH comparisons pass. The predicate has
// its own table below.
func ratchetOK(size, budget int) error {
	if size != budget {
		return fmt.Errorf("the inventory holds %d entries but danglingBudget is %d. It is the CURRENT "+
			"size, so shrinking the inventory means lowering the number in the same change — and growing "+
			"it is not allowed at all", size, budget)
	}
	return nil
}

// TestRatchetOK pins the comparison itself.
func TestRatchetOK(t *testing.T) {
	for _, tc := range []struct {
		name         string
		size, budget int
		wantErr      bool
	}{
		{"exactly at budget", 27, 27, false},
		{"one MORE than budget — a new exemption bought for free", 28, 27, true},
		{"one FEWER — a repair that forgot to lower the number", 26, 27, true},
		{"lowered together", 26, 26, false},
		{"emptied together", 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ratchetOK(tc.size, tc.budget); (err != nil) != tc.wantErr {
				t.Errorf("ratchetOK(%d, %d) error = %v, wantErr %v", tc.size, tc.budget, err, tc.wantErr)
			}
		})
	}
}
