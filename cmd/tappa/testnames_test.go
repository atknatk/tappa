package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf16"
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
	//
	// 🔴 THE ROOT-LIST × EXTENSION-LIST SHAPE LEAKED ON EVERY ROUND IT WAS TOUCHED, SO
	// IT WAS REPLACED WITH ONE RULE. The history is the argument:
	//
	//	round 1  .yml missing            -> a citation in ci.yml was invisible
	//	round 2  `web`/`db` missing      -> adding .templ/.sql was the reported repair,
	//	                                    but the real hole was the ROOT list, and the
	//	                                    two citations it exposed were in ordinary
	//	                                    .go files under web/templates
	//	round 3  Makefile (no extension) -> 8 real citations, still invisible;
	//	         input.css (.css)        -> 6 more, in a directory that WAS a root
	//
	// Three rounds, three holes, each one patched in the shape of the previous one —
	// the failure mode this repository names outright: a detector written to the shape
	// of the last defect documents nothing about the next.
	//
	// THE RULE IS NOW: EVERY TEXT FILE IN THE REPOSITORY, minus a named skip list.
	// There is no extension list to forget and no root list to omit, so a citation in a
	// Makefile, a stylesheet, a JSON fixture or a file type nobody has invented yet is
	// covered by construction rather than by remembering.
	//
	// WHAT IT COST: the dangling count went 55 -> 53 while the scope GREW, and the
	// citation total rose by a couple of dozen.
	//
	// ⚠️ NO CITATION TOTAL IS QUOTED HERE, and an earlier draft of this comment quoted
	// one anyway ("2 661 -> 2 685") three lines after saying live figures are
	// deliberately not repeated as literals. It was already 2 689 by the next audit,
	// because every test this repository adds moves it. The rule this comment states
	// applies to this comment: the counters are PRINTED on every run, and the run is
	// the place to read them.
	//
	// 🔴 THE DANGLING COUNT WENT DOWN WHILE THE SCOPE WENT UP, and that is the part
	// worth reading. Round 2 raised the budget 53 -> 55 to admit two citations from
	// web/templates/pages/*view.go. An audit then showed both were merely TRUNCATED —
	// each named a real test minus its suffix — so they were repairable all along and
	// the budget rise bought two permanent exemptions for two typos. They are fixed at
	// source and the budget is back to 53. The inventory's own header says the list may
	// only SHRINK; raising it to avoid a two-character fix is exactly the rot it warns
	// about.
	//
	// The skip list is deliberately tiny and every entry is a place with no
	// hand-written citations: .git (history, not source), .tools (downloaded
	// binaries), bin (build output), node_modules (which this repository forbids).
	//
	// ⚠️ THE INVENTORY FILE IS SKIPPED FOR A DIFFERENT REASON and it is not an
	// exemption: it lists dangling names, so scanning it would let every entry keep
	// itself alive and the staleness check below could never fire.
	// 🔴 THE SKIP LIST IS MATCHED ON THE PATH FROM THE ROOT, NOT ON A DIRECTORY'S NAME.
	// It used to test info.Name(), which meant ANY directory called `bin` ANYWHERE was
	// skipped -- an audit put a dangling citation in docs/bin/ and it became invisible.
	// The comment described "bin (build output)", so the mechanism was WIDER than the
	// description: the same gap between what a check says and what it does that this
	// whole test exists to catch.
	invPath := filepath.Join(repoRoot, "cmd", "tappa", "testdata", "known-dangling-citations.txt")
	scanned, binaries := 0, 0
	if err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			rel, rerr := filepath.Rel(repoRoot, path)
			if rerr == nil && skipScanDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if path == invPath {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		// 🔴 UTF-16 IS TEXT AND IS DECODED RATHER THAN SKIPPED. A BOM-led UTF-16 file is
		// full of NUL bytes, so the binary heuristic below would drop it -- and a .md
		// saved by a Windows editor is exactly the file somebody writes a citation in.
		// An audit flagged this as latent (the tree has no UTF-16 file today); it is
		// closed rather than counted because the decode is six lines and the failure
		// mode is silent.
		// ⚠️ COUNTED LIMIT: THE HELPER IS PINNED, THIS WIRING IS NOT.
		// TestDecodeUTF16_TreatsTextAsText drives the decoder directly, so a broken
		// DECODER goes red. Nothing drives this CALL, because the repository contains
		// no UTF-16 file -- deleting the two lines below leaves every package green.
		// Pinning it would mean writing a UTF-16 file into the tree for a test to find,
		// which trades a latent gap for a permanent oddity in the source. The gap is
		// recorded instead: if a UTF-16 file ever lands here, this branch is what makes
		// it scannable, and nothing but review says so.
		if decoded, ok := decodeUTF16(b); ok {
			scanned++
			add(path, decoded)
			return nil
		}
		// A NUL byte in the first few KiB means binary. Reading a compiled binary or a
		// font as text would not crash anything, but it would spend the scan on bytes
		// that cannot carry a citation.
		head := b
		if len(head) > 8000 {
			head = head[:8000]
		}
		if bytes.IndexByte(head, 0) >= 0 {
			binaries++
			return nil
		}
		scanned++
		add(path, b)
		return nil
	}); err != nil {
		t.Fatalf("walking the repository: %v", err)
	}
	t.Logf("%d text files scanned, %d binaries skipped", scanned, binaries)
	// CONTROL: the walk really reached the file types the old shape missed. Without
	// this the single rule could silently regress to the old coverage and nothing here
	// would notice.
	for _, must := range []string{"Makefile", filepath.Join("web", "static", "css", "input.css")} {
		if _, err := os.Stat(filepath.Join(repoRoot, must)); err == nil && scanned < 100 {
			t.Fatalf("only %d files scanned; the walk is not reaching the repository", scanned)
		}
	}
	// CLAUDE.md needs no special case any more: it is a text file in the repository
	// root, so the single rule above already reached it. It used to be read explicitly
	// because the root list did not include ".".

	// The inventory lives in testdata (not scanned), so its entries cannot keep
	// themselves alive.
	known := map[string]bool{}
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

// decodeUTF16 converts a BOM-led UTF-16 file to UTF-8 so it can be scanned as the text
// it is. It answers (decoded, true) only for a file that really carries a UTF-16 BOM.
//
// 🔴 WITHOUT IT SUCH A FILE IS SILENTLY CLASSED AS BINARY, because UTF-16 encodes
// ASCII with a NUL in every other byte. A Markdown file saved by a Windows editor is
// the ordinary case, and a citation inside one would be unprotected with nothing
// saying so.
func decodeUTF16(b []byte) ([]byte, bool) {
	if len(b) < 2 {
		return nil, false
	}
	var order binary.ByteOrder
	switch {
	case b[0] == 0xFF && b[1] == 0xFE:
		order = binary.LittleEndian
	case b[0] == 0xFE && b[1] == 0xFF:
		order = binary.BigEndian
	default:
		return nil, false
	}
	body := b[2:]
	units := make([]uint16, 0, len(body)/2)
	for i := 0; i+1 < len(body); i += 2 {
		units = append(units, order.Uint16(body[i:i+2]))
	}
	return []byte(string(utf16.Decode(units))), true
}

// skipScanDir reports whether a directory, named by its path RELATIVE TO THE
// REPOSITORY ROOT, is outside the citation scan.
//
// 🔴 IT TAKES A PATH AND NOT A NAME, AND THAT DISTINCTION IS THE WHOLE FUNCTION. The
// walk used to test info.Name(), so ANY directory called `bin` anywhere was skipped --
// an audit put a dangling citation in docs/bin/ and it became invisible, while the
// comment described the entry as "build output". It is a function so the rule can be
// driven directly: the defect it closes is LATENT (the tree has no such directory
// today), so a test that only walks the real repository cannot tell the two versions
// apart.
func skipScanDir(rel string) bool {
	switch rel {
	case ".git", ".tools", "bin", "node_modules":
		return true
	}
	return false
}

// TestScanScope_SkipsBuildOutputWithoutSkippingEverythingNamedLikeIt pins the path-vs-name
// decision, which the repository-wide walk cannot pin because nothing in the tree
// triggers it.
func TestScanScope_SkipsBuildOutputWithoutSkippingEverythingNamedLikeIt(t *testing.T) {
	for _, rel := range []string{".git", ".tools", "bin", "node_modules"} {
		if !skipScanDir(rel) {
			t.Errorf("%s should be skipped: it holds no hand-written citations", rel)
		}
	}
	// 🔴 THE ONES THAT MUST NOT BE SKIPPED. Each is a real place somebody could put a
	// document, and each was invisible while the check matched on the base name.
	for _, rel := range []string{
		"docs/bin", "internal/bin", "web/static/bin", "deploy/bin",
		"docs/.git-notes", "internal/tools", "scripts/node_modules-notes",
	} {
		if skipScanDir(rel) {
			t.Errorf("🔴 %s is being skipped, so a citation written there would be invisible. "+
				"The skip list names BUILD OUTPUT at the repository root, not every directory "+
				"that happens to share its name.", rel)
		}
	}
}

// TestDecodeUTF16_TreatsTextAsText pins the other half of the scan's file
// classification, and for the same reason: no UTF-16 file exists in the tree, so the
// repository walk cannot tell a working decoder from a missing one.
//
// 🔴 WITHOUT THE DECODER A UTF-16 FILE IS CLASSED AS BINARY AND SILENTLY DROPPED,
// because UTF-16 encodes ASCII with a NUL in every other byte. A .md saved by a
// Windows editor is the ordinary case.
func TestDecodeUTF16_TreatsTextAsText(t *testing.T) {
	// ⚠️ THE FIXTURE DELIBERATELY CONTAINS NO CITATION-SHAPED IDENTIFIER. The first
	// draft used one and this very scanner flagged it -- the net catching its own
	// test data, which is the check working, and a good reminder that a fixture in a
	// scanned repository is itself scanned.
	const want = "a line of ordinary prose with no identifier in it"

	le := append([]byte{0xFF, 0xFE}, encodeUTF16LE(want)...)
	got, ok := decodeUTF16(le)
	if !ok || string(got) != want {
		t.Errorf("little-endian BOM: ok=%v got=%q, want %q", ok, got, want)
	}
	be := append([]byte{0xFE, 0xFF}, encodeUTF16BE(want)...)
	got, ok = decodeUTF16(be)
	if !ok || string(got) != want {
		t.Errorf("big-endian BOM: ok=%v got=%q, want %q", ok, got, want)
	}
	// AND IT DOES NOT CLAIM ORDINARY FILES. A UTF-8 file has no BOM of this kind, and
	// treating one as UTF-16 would turn every source file into mojibake.
	if _, ok := decodeUTF16([]byte(want)); ok {
		t.Error("a plain UTF-8 file was decoded as UTF-16")
	}
	if _, ok := decodeUTF16([]byte{0xFF}); ok {
		t.Error("a one-byte file was decoded as UTF-16")
	}
	if _, ok := decodeUTF16(nil); ok {
		t.Error("an empty file was decoded as UTF-16")
	}
}

func encodeUTF16LE(s string) []byte {
	var out []byte
	for _, u := range utf16.Encode([]rune(s)) {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

func encodeUTF16BE(s string) []byte {
	var out []byte
	for _, u := range utf16.Encode([]rune(s)) {
		out = append(out, byte(u>>8), byte(u))
	}
	return out
}
