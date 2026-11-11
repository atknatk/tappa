package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
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
	fixtures := map[string][]string{}
	walk := func(root string, fn func(path string, b []byte)) {
		err := filepath.Walk(filepath.Join(repoRoot, root), func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				// A _test.go file under testdata is not a test the go tool runs (T218 (b)).
				if rel, rerr := filepath.Rel(repoRoot, path); rerr == nil && skipGoDir(rel) {
					return filepath.SkipDir
				}
				return nil
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
			if err := addDeclaredTests(path, b, declared, fixtures); err != nil {
				t.Fatalf("%s will not parse, so its tests are not counted: %v", path, err)
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
	carried := 0
	for _, names := range fixtures {
		carried += len(names)
	}
	t.Logf("%d test functions declared, %d more carried in strings by %d files", len(declared), carried, len(fixtures))

	// 2. Collect every Test… name CITED outside a declaration.
	citeRe := regexp.MustCompile(`\bTest[A-Z][A-Za-z0-9_]{4,}\b`)
	var cites []citation
	add := func(path string, b []byte) {
		text := string(b)
		for _, line := range strings.Split(text, "\n") {
			trimmed := strings.TrimSpace(line)
			// Skip declarations themselves.
			if strings.HasPrefix(trimmed, "func Test") {
				continue
			}
			for _, name := range citeRe.FindAllString(line, -1) {
				cites = append(cites, citation{path, name})
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

	// 3. Every citation must resolve; citationResolves says what that means.
	dangling := danglingCitations(cites, fixtures, declared)
	for _, c := range dangling {
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
		live[d.name] = true
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

const danglingBudget = 58

// fixtureTestDeclRe is a test declared inside a string: Go source a test hands to a checker. It asks for
// the whole signature because a test that LOOKS FOR a declaration carries the same words: `goFuncBody(src,
// "func TestX(")` read as a fixture let that file's citation of TestX survive TestX's deletion.
var fixtureTestDeclRe = regexp.MustCompile(`(?m)^func (Test\w+)\(t \*testing\.T\)`)

// declaredTests reads a Go file with the parser and answers the test functions it declares, and the ones
// it only carries in a string.
//
// 🔴 IT PARSES BECAUSE TEXT CANNOT TELL A DECLARATION FROM ITS LOOK-ALIKES, IN EITHER DIRECTION. A deleted
// test with a surviving citation stayed reachable by COMMENTING OUT rather than deleting, so the scan
// stripped /* */ blocks first -- with a regexp that knew no Go strings. A "/*" string then swallowed every
// declaration down to the next */ (a false red), and a `func TestX(` opening a line of a raw string counted
// as a test anybody in the repository could cite (a false pass; 74 names in cmd/opadmin/tableslock_test.go,
// whose checker reads Go source, were "declared" that way, measured).
func declaredTests(path string, src []byte) (names, inStrings []string, err error) {
	f, err := parser.ParseFile(token.NewFileSet(), path, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, nil, err
	}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}
		if name := fn.Name.Name; strings.HasPrefix(name, "Test") && name != "Test" {
			names = append(names, name)
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if text, err := strconv.Unquote(lit.Value); err == nil {
			for _, m := range fixtureTestDeclRe.FindAllStringSubmatch(text, -1) {
				inStrings = append(inStrings, m[1])
			}
		}
		return true
	})
	return names, inStrings, nil
}

// addDeclaredTests records one Go file's tests: what it declares for every citation in the repository,
// what it only carries in a string for its own citations.
func addDeclaredTests(path string, src []byte, declared map[string]bool, fixtures map[string][]string) error {
	names, inStrings, err := declaredTests(path, src)
	if err != nil {
		return err
	}
	for _, name := range names {
		declared[name] = true
	}
	if len(inStrings) > 0 {
		fixtures[path] = inStrings
	}
	return nil
}

// citation is one Test… name written in one file.
type citation struct{ file, name string }

// danglingCitations returns the first citation of each name that resolves to nothing. A name counts once
// even when another file's citation of it resolves, because the inventory is a list of names.
func danglingCitations(cites []citation, fixtures map[string][]string, declared map[string]bool) []citation {
	seen := map[string]bool{}
	var dangling []citation
	for _, c := range cites {
		if citationResolves(c.name, fixtures[c.file], declared) || seen[c.name] {
			continue
		}
		seen[c.name] = true
		dangling = append(dangling, c)
	}
	return dangling
}

// citationResolves is the rule a citation is held to. A name resolves when it is a declared test, or a
// test the citing file itself carries in a string: its table names its own fixture, and that fixture is
// evidence nowhere else.
//
// A citation may also name a FAMILY rather than one test: internal/domain/tap cites "TestDecide_" for the
// whole section 5 table, and internal/handler cites "TestAccount". Those resolve as a prefix of a declared
// test.
//
// 🔴 PREFIX MATCHING IS LIMITED TO EXPLICIT FAMILY CITATIONS (those ending in "_"). The open prefix rule
// made the MOST COMMON rename invisible: renaming TestX to TestX_v2 left a citation of TestX resolving,
// because the new name has the old one as a prefix. Measured: exactly that mutation stayed green across
// 2 513 citations -- and "a citation that resolves to nothing" is the class this whole check exists for,
// which it hit twice in this task.
func citationResolves(name string, ownFixtures []string, declared map[string]bool) bool {
	if declared[name] || slices.Contains(ownFixtures, name) {
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

// TestDeclaredTests_ReadsDeclarationsAndNothingThatLooksLikeOne pins the shapes a text scan got wrong.
func TestDeclaredTests_ReadsDeclarationsAndNothingThatLooksLikeOne(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src       string
		want, inStrings []string
	}{
		{"plain", "func TestA(t *testing.T) {}\nfunc TestB(t *testing.T) {}\n", []string{"TestA", "TestB"}, nil},
		{"a block-comment opener in a string hides nothing",
			"var s = \"/*\"\nfunc TestA(t *testing.T) {}\nvar e = \"*/\"\nfunc TestB(t *testing.T) {}\n",
			[]string{"TestA", "TestB"}, nil},
		{"the same opener in a raw string",
			"var s = `/*`\nfunc TestA(t *testing.T) {}\n// */\n", []string{"TestA"}, nil},
		{"a commented-out test is gone",
			"/*\nfunc TestGone(t *testing.T) {}\n*/\nfunc TestA(t *testing.T) {}\n", []string{"TestA"}, nil},
		{"a test in a raw string is carried, not declared",
			"var s = `\nfunc TestStr(t *testing.T) {}\n`\nfunc TestA(t *testing.T) {}\n", []string{"TestA"}, []string{"TestStr"}},
		{"a test in a quoted string likewise",
			"var s = \"package x\\nfunc TestStr(t *testing.T) {}\\n\"\n", nil, []string{"TestStr"}},
		{"a search for a declaration carries nothing",
			"var s = find(src, \"func TestA(\")\nvar r = `\nfunc TestB(`\nvar q = \"\\nfunc TestC(x int)\"\n", nil, nil},
		{"a method is not a test", "type T struct{}\nfunc (T) TestM(t *testing.T) {}\n", nil, nil},
		{"neither is a function of no further name", "func helper() {}\nfunc Test() {}\n", nil, nil},
	} {
		got, inStrings, err := declaredTests("x_test.go", []byte("package x\n"+tc.src))
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if !slices.Equal(got, tc.want) || !slices.Equal(inStrings, tc.inStrings) {
			t.Errorf("%s: declared %q and %q in strings, want %q and %q", tc.name, got, inStrings, tc.want, tc.inStrings)
		}
	}
	if _, _, err := declaredTests("x_test.go", []byte("package x\nfunc TestA( {\n")); err == nil {
		t.Error("a file that will not parse returned no error, so its tests would silently stop counting")
	}
}

// TestCitationResolves_AFixtureIsEvidenceOnlyInItsOwnFile pins the rule, the fixture half of which no tree
// walk can show: the tree has no citation of a fixture from another file today.
func TestCitationResolves_AFixtureIsEvidenceOnlyInItsOwnFile(t *testing.T) {
	t.Parallel()
	// The names are this short so that this table is not itself read as citing them.
	declared := map[string]bool{"TestReal": true, "TestAb_C": true}
	for _, tc := range []struct {
		name, cited string
		own         []string
		want        bool
	}{
		{"a declared test", "TestReal", nil, true},
		{"a test its own file carries in a string", "TestStr", []string{"TestStr"}, true},
		{"a test another file carries in a string", "TestStr", nil, false},
		{"nothing at all", "TestNone", []string{"TestStr"}, false},
		{"a family", "TestAb_", nil, true},
		{"a family nothing declares", "TestZz_", nil, false},
		{"a family only a fixture belongs to", "TestS_", []string{"TestS_x"}, false},
		{"an open prefix is no family", "TestAb", nil, false},
	} {
		if got := citationResolves(tc.cited, tc.own, declared); got != tc.want {
			t.Errorf("%s: citationResolves(%q) = %v, want %v", tc.name, tc.cited, got, tc.want)
		}
	}
}

// TestDanglingCitations_AFixtureResolvesOnlyInTheFileThatCarriesIt drives the whole path a tree walk
// cannot, from the sources to the verdict: the tree has no citation of a fixture from another file today.
func TestDanglingCitations_AFixtureResolvesOnlyInTheFileThatCarriesIt(t *testing.T) {
	t.Parallel()
	declared, fixtures := map[string]bool{}, map[string][]string{}
	for path, src := range map[string]string{
		"carrier_test.go": "package x\nvar s = `\nfunc TestStr(t *testing.T) {}\n`\nfunc TestReal(t *testing.T) {}\n",
		"other_test.go":   "package x\nvar s = `\nfunc TestElse(t *testing.T) {}\n`\n",
	} {
		if err := addDeclaredTests(path, []byte(src), declared, fixtures); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name  string
		cites []citation
		want  []citation
	}{
		{"a declared test, from anywhere", []citation{{"carrier_test.go", "TestReal"}, {"doc.md", "TestReal"}}, nil},
		{"a fixture from its own file", []citation{{"carrier_test.go", "TestStr"}}, nil},
		{"a fixture from a document", []citation{{"doc.md", "TestStr"}}, []citation{{"doc.md", "TestStr"}}},
		{"a fixture from a file carrying another", []citation{{"other_test.go", "TestStr"}}, []citation{{"other_test.go", "TestStr"}}},
		{"own file first, then a document", []citation{{"carrier_test.go", "TestStr"}, {"doc.md", "TestStr"}}, []citation{{"doc.md", "TestStr"}}},
		{"one name counts once", []citation{{"a.md", "TestNone"}, {"b.md", "TestNone"}}, []citation{{"a.md", "TestNone"}}},
	} {
		if got := danglingCitations(tc.cites, fixtures, declared); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

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
//
// A NESTED CHECKOUT IS SKIPPED TOO (T87), and that half does read the disk: an agent's worktree under
// .claude/worktrees is a second copy of the repository, so each inventory saw a second reader, a second
// resolver and every other tree's citations. It is decided by the entry git itself leaves there and not
// by the path, so a worktree anywhere is the same case. git refuses to track a path named .git, so this
// can hide nothing from CI: a fresh checkout has no such directory.
func skipScanDir(rel string) bool {
	return skipScanDirIn(repoRoot, rel)
}

// skipGoDir is skipScanDir for a walk that reads GO SOURCE (T218 (b)): it also leaves out every
// directory named testdata, at any depth, because the go tool does -- a .go file there is a fixture,
// never part of a package, so it writes no brand slot, no policy document, no comparison and declares
// no test, and a fixture that does not parse is not this repository's syntax error. The TEXT walk of
// TestEveryNamedTestExists keeps skipScanDir: a citation written in a fixture is still a citation.
//
// MEASURED (T218 (b)): with an unparsable zz_*.go and zz_*_test.go planted in
// internal/handler/testdata, TestEveryNamedTestExists, TestBrandWrites_TheCallSitesAreTheOnesInventoried
// and TestNoteProvenance_OnlyTwoProductionCallSitesWriteAPolicyDocument were red; with skipGoDir, green.
func skipGoDir(rel string) bool {
	return filepath.Base(rel) == "testdata" || skipScanDir(rel)
}

// skipScanDirIn is skipScanDir under any root, so the nested-checkout half can be driven in a temporary
// directory instead of planting a .git in the repository.
func skipScanDirIn(root, rel string) bool {
	switch rel {
	case ".git", ".tools", "bin", "node_modules":
		return true
	// Gradle output for the Android relay (M8-05 FAZ B3). None of it is hand-written
	// and all of it is gitignored, but `make android` leaves it on disk and the walk
	// found cited test names inside a zip cache there -- 54 dangling against a budget
	// of 53, on a tree whose sources had not changed. The SOURCE tree under android/
	// stays in scope: android/README.md cites Go test names on purpose.
	case "android/.gradle", "android/.kotlin", "android/build", "android/app/build":
		return true
	// The walk's own root is not a nested checkout, and in a worktree it carries a .git file too.
	case ".":
		return false
	}
	return isCheckout(filepath.Join(root, rel))
}

// isCheckout reports whether a directory is the top of a git checkout: a .git file (a worktree or a
// submodule) or a .git directory (a clone). Lstat, because a dangling link is still that entry.
func isCheckout(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

// TestScanScope_ANestedCheckoutIsSkippedAndTheRootIsNot pins the nested half, which the tree cannot show:
// a fresh checkout holds no nested checkout.
func TestScanScope_ANestedCheckoutIsSkippedAndTheRootIsNot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mk := func(rel string) string {
		dir := filepath.Join(root, rel)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	write := func(rel string) {
		if err := os.WriteFile(filepath.Join(mk(rel), ".git"), []byte("gitdir: elsewhere\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".")
	write(filepath.Join(".claude", "worktrees", "agent-1"))
	write(filepath.Join("docs", "somewhere", "else"))
	mk(filepath.Join("clone", ".git"))
	mk(filepath.Join("docs", "plain"))
	mk(filepath.Join(".claude", "worktrees"))
	for _, tc := range []struct {
		rel  string
		want bool
	}{
		{".", false}, // this worktree's own shape: skipping it would blind every scan
		{filepath.Join(".claude", "worktrees", "agent-1"), true},
		{filepath.Join("docs", "somewhere", "else"), true},
		{"clone", true},
		{filepath.Join("docs", "plain"), false},
		{filepath.Join(".claude", "worktrees"), false}, // the parent is not a checkout; its children are
		{"docs", false},
		{filepath.Join("no", "such", "dir"), false},
	} {
		if got := skipScanDirIn(root, tc.rel); got != tc.want {
			t.Errorf("skipScanDirIn(%q) = %v, want %v", tc.rel, got, tc.want)
		}
	}
	// The repository's own root, in whichever shape this run has it.
	if skipScanDir(".") {
		t.Error("skipScanDir(\".\") is true: every scan that walks the repository would read nothing")
	}
	raw, err := os.ReadFile(filepath.Join(repoRoot, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(strings.Split(string(raw), "\n"), ".claude/worktrees/") {
		t.Error(".gitignore does not list .claude/worktrees/, so the agents' worktrees show up as untracked and reach `git ls-files --others`")
	}
	mf, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mf), `-not -path './.claude/*'`) {
		t.Error("`make fmt`'s find does not leave out .claude, so gofmt -w rewrites the source of a worktree another agent is working in")
	}
}

// TestScanScope_SkipsBuildOutputWithoutSkippingEverythingNamedLikeIt pins the path-vs-name
// decision, which the repository-wide walk cannot pin because nothing in the tree
// triggers it.
func TestScanScope_SkipsBuildOutputWithoutSkippingEverythingNamedLikeIt(t *testing.T) {
	for _, rel := range []string{
		".git", ".tools", "bin", "node_modules",
		"android/.gradle", "android/.kotlin", "android/build", "android/app/build",
	} {
		if !skipScanDir(rel) {
			t.Errorf("%s should be skipped: it holds no hand-written citations", rel)
		}
	}
	// 🔴 THE ONES THAT MUST NOT BE SKIPPED. Each is a real place somebody could put a
	// document, and each was invisible while the check matched on the base name.
	for _, rel := range []string{
		"docs/bin", "internal/bin", "web/static/bin", "deploy/bin",
		"docs/.git-notes", "internal/tools", "scripts/node_modules-notes",
		// The Android SOURCE tree is hand-written and cites Go tests; only its
		// Gradle output is skipped. "android/app" is the parent of "android/app/build".
		"android", "android/app", "android/app/src",
	} {
		if skipScanDir(rel) {
			t.Errorf("🔴 %s is being skipped, so a citation written there would be invisible. "+
				"The skip list names BUILD OUTPUT at the repository root, not every directory "+
				"that happens to share its name.", rel)
		}
	}
}

// TestScanScope_GoWalksSkipTestdataAndTheTextWalkDoesNot (T218 (b)): skipGoDir leaves out a testdata
// directory at any depth and nothing merely named like one; skipScanDir -- the citation walk's -- keeps it.
func TestScanScope_GoWalksSkipTestdataAndTheTextWalkDoesNot(t *testing.T) {
	for _, rel := range []string{"testdata", filepath.Join("cmd", "tappa", "testdata"), filepath.Join("internal", "handler", "testdata"),
		filepath.Join("internal", "qrcode", "testdata", "deeper", "testdata")} {
		if !skipGoDir(rel) {
			t.Errorf("skipGoDir(%q) is false: a fixture there would be read as this repository's Go source", rel)
		}
	}
	for _, rel := range []string{".", "internal", filepath.Join("internal", "handler"), filepath.Join("internal", "testdatax"),
		filepath.Join("docs", "testdata-notes")} {
		if skipGoDir(rel) {
			t.Errorf("skipGoDir(%q) is true: only a directory NAMED testdata is the go tool's fixture tree", rel)
		}
	}
	if skipScanDir(filepath.Join("internal", "handler", "testdata")) {
		t.Error("skipScanDir skips testdata: a citation written in a fixture would no longer be checked")
	}
	if !skipGoDir(".git") {
		t.Error("skipGoDir does not include skipScanDir's own list")
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
