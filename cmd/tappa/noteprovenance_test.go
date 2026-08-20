package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// noteprovenance_test.go -- the STRUCTURAL half of "the sentence a tap shows is
// ours" (M8-04 FAZ B3, correction round 3).
//
// 🔴 WHAT THIS FILE USED TO BE, AND WHY IT WAS WRONG. Its first version was a prose
// detector: it walked the tree and failed the build on any unquoted repetition of
// two sentences -- "internal/policy's reasons are fixed strings written by us" and
// "unlike Note above, which is one of our own policy sentences" -- on the ground
// that the M8-04 audit had measured them false. The audit that followed measured the
// measurement, and the sentences are TRUE on this tree. So the gate was banning a
// correct statement and pinning an incorrect one in its place, which is the exact
// defect this task has had to fix over and over: a claim shipped with a gate holding
// it, and nobody had checked the claim.
//
// 🔴 AND ITS SECOND VERSION SHIPPED A CLAIM ITS CODE DID NOT KEEP. It said it
// reported "any writer of policy_versions.document ... whether it goes through the
// store or straight at the table in SQL". Round 3's audit measured three ways past
// it, all three compiling and all four gates staying green:
//
//	(a) a hand-written method in internal/store -- the walk SKIPPED that directory
//	    outright, and the two writer names were an ALLOW-LIST, so a third name was
//	    invisible twice over;
//	(b) a new `-- name:` query in db/queries/policies.sql -- the walk read `.go`
//	    only, so THE PLACE CLAUDE.md §6 PUTS EVERY QUERY was never opened;
//	(c) `INSERT INTO` and `policy_versions` on two lines of one Go raw string -- the
//	    raw-SQL check compared one LINE at a time.
//
// (a) and (b) are closed STRUCTURALLY rather than by name, and (c) by reading syntax
// instead of lines. What replaced each is described where it is checked, and what is
// still open is in the limits list below -- counted, not silenced.
//
// 🔴 WHY A GATE IS WARRANTED AT ALL. "The prose is ours" is a property of the WRITE
// PATHS, and it holds because there are exactly two of them:
//
//	internal/domain/tenant/rulewriter.go   AppendPolicyVersion         layer 'tenant'
//	internal/domain/checkin/policyset.go   EnsureBaselinePolicyVersion layer 'baseline'
//
// The first is fed by copyOfShipped, which copies a shipped statement WHOLE and
// changes only its sid and its resource list -- pinned behaviourally by
// TestAuthoredRule_TheProseIsOursAndOnlyTheScopeIsTheirs (internal/domain/tenant).
// The second serialises policy.Baseline() as this binary ships it. A THIRD writer is
// how that stops being true, and Q22 already names the shape it would take: the
// raw-JSON rule editor deferred to M9-07.
//
// 🔴 WHICH SHAPES TURN THIS FILE RED, NAMED ONE BY ONE RATHER THAN PROMISED IN A
// WORD. Each was injected into this tree, measured red, and removed (2026-08-20):
//
//	a new `-- name:` query writing the table in db/          derivation test
//	a hand-written store method with its own INSERT          store test
//	the same, wearing a forged `-- name:` header             store test
//	a store method that runs the generated constant          store test
//	a store method that wraps the generated writer,          store test
//	as a call OR as a method value
//	an INSERT in a shipped Go string constant, however it     caller test
//	is broken across lines or joined with `+`
//	a reference to either writer from a file that may not     caller test
//	-- a call, a method value, or a method expression
//
// 🔴 AND TWO MORE THAT ROUND 4's AUDIT MEASURED GOING STRAIGHT THROUGH, both of them
// closed here (2026-08-20):
//
//	INSERT INTO /* audit */ policy_versions (…)              derivation test
//	INSERT INTO -- audit ⏎ policy_versions (…)               derivation test
//	write := q.AppendPolicyVersion; write(ctx, arg)          caller test
//
// The first two are the reason stripSQLComments (insertscope_test.go) is a lexer now
// rather than a line filter, and they needed no hand-written Go: sqlc v1.28.0 copies
// the comment into the generated constant verbatim, so the escape arrived through the
// `make gen` step CLAUDE.md §2 makes mandatory.
//
// AND THE SHAPE THAT DOES NOT, which is why there is no word like "every" above: SQL
// this file cannot evaluate -- assembled at run time from something that is not a
// string constant. The first limit below says what stands against that instead.
//
// THREE TESTS, THREE DIFFERENT QUESTIONS, and the division is the point:
//
//	TestNoteProvenance_TheWriterListIsDerivedFromTheQueriesRatherThanNamed
//	    reads db/ and asks WHICH named queries write the table. The two names below
//	    are checked AGAINST that answer instead of standing in for it.
//	TestNoteProvenance_TheStoreCarriesNoWriterTheQueriesDoNotName
//	    reads internal/store and asks whether the generated layer holds a writer the
//	    queries never asked for -- the (a) escape.
//	TestNoteProvenance_OnlyTwoProductionCallSitesWriteAPolicyDocument
//	    reads the rest of the shipped Go and asks who CALLS them, and whether a
//	    string constant there writes the table without them.
//
// TestNoteProvenanceScan_ReportsTheBypassesItExistsToReport drives the same
// functions over the three measured escapes, so a scan that stops firing is caught
// by something other than the tree happening to be clean.
//
// ⚠️ COUNTED LIMITS, said rather than left to be discovered. The first TWO are the
// ones round 3 attacked and did not close; the rest predate it:
//
//   - SQL ASSEMBLED AT RUNTIME IS INVISIBLE. The Go side reads string CONSTANTS,
//     including `+` chains of them, because that is what a parser can evaluate.
//     fmt.Sprintf("INSERT INTO %s", t) is not a constant and is not seen. What
//     stands against that shape is CLAUDE.md §6 ("handler icine ham SQL yazilmaz")
//     and the six package query_test.go belts, not this file.
//   - A NAME THIS FILE TRUSTS CAN BE FORGED IN THE STORE. Attribution follows sqlc's
//     `-- name:` header, so a hand-written store file could put the header of an
//     ALLOWED writer above a different statement. What that costs the forger is
//     measured: the derived set, the one-segment-per-writer count and the
//     const-usage check below each have to be satisfied at the same time, so the
//     forgery has to REPLACE the real generated query rather than sit beside it --
//     at which point `make gen` puts it back and the diff is the review.
//   - IT READS THE SHIPPED TREE, NOT THE DATABASE. A trigger, a rule or a view that
//     forwarded some other table's write into policy_versions would not be read
//     here; migration 00007's own trigger goes the other way (it REFUSES mutation),
//     and scripts/redline-check.sh is what reads DDL.
//   - It sees a new writer; it does not see an existing one being handed a document
//     built some other way. That half is
//     TestAuthoredRule_TheProseIsOursAndOnlyTheScopeIsTheirs, which calls the
//     generator instead of reading it.
//   - TESTS AND FIXTURES ARE OUT OF SCOPE, deliberately, and one test does write a
//     bespoke reason: TestPolicySetDB_TenantLayerIsEmptyToday inserts a tenant
//     document with the reason "night taps are reviewed" through raw SQL, as the
//     positive control proving the loader reads the tenant layer at all. That is the
//     point of it -- the layer is empty in every deployment because of the write
//     paths, not because the reader ignores it -- and a gate that failed on it would
//     be deleting the control.
//   - 🔴 THIS SLOT USED TO HOLD A GUARANTEE WRITTEN BACKWARDS, and it is worth
//     leaving the wreck visible. It said block comments were not stripped, "so a
//     write inside a `/* */` would be read as code and REPORTED rather than missed --
//     the safe direction". The direction was the opposite one. A comment INSIDE a
//     statement -- `INSERT INTO /* audit */ policy_versions` -- was not read as code,
//     it broke the `\s+` between the two halves of the pattern, and the write was
//     MISSED. Round 4's audit built it and measured all four gates green. The
//     sentence's own supporting fact was sound and did no work: db/ really does hold
//     zero block-comment opens outside a `--` line (2026-08-20, still true), but
//     nothing was holding it there, and the escape did not need the tree's help. The
//     stripper is a lexer now (stripSQLComments), it is pinned by
//     TestStripSQLComments_ReadsCommentsTheWayPostgresDoes, and both comment forms are
//     cases in TestNoteProvenanceScan_ReportsTheBypassesItExistsToReport. What is left
//     is stated where the lexer is: block comments do not NEST here and do in
//     PostgreSQL, which removes less text and therefore over-reports.

// policyDocumentWriters names the store methods that put a policy DOCUMENT into
// policy_versions, and the one production file that may call each.
//
// 🔴 IT IS NO LONGER THE SOURCE OF TRUTH, AND THAT IS THE ROUND-3 FIX. As an
// allow-list it answered "is this name known?", which is silent about a name nobody
// listed -- escape (a). The set of writers is now DERIVED from db/ by looking at what
// each named query DOES, and this map is checked against that derivation: a third
// query writing the table makes the two sets differ, whatever it is called. What the
// map still carries is the half a derivation cannot know -- WHICH production file is
// allowed to call each -- and the layer each writes, because that is the column
// ledger.Record.NoteIsTenants reads.
var policyDocumentWriters = map[string]string{
	"AppendPolicyVersion":         "internal/domain/tenant/rulewriter.go",
	"EnsureBaselinePolicyVersion": "internal/domain/checkin/policyset.go",
}

// policyVersionWriteRE matches the HEAD of a statement whose target is
// policy_versions.
//
// 🔴 `\s+` RATHER THAN A LINE, which is escape (c). The old check asked whether one
// LINE held both "INSERT INTO" and "policy_versions", so splitting a Go raw string
// across two lines walked past it. A statement is not a line, so the pattern spans
// whitespace and the text it runs over is normalised the same way redline-check.sh
// normalises DDL: the writings that differ only in punctuation and spacing are made
// into one text BEFORE matching, rather than answered with one more pattern each.
// The optional `public.` and the optional identifier quotes are there for the same
// reason -- `INSERT INTO "public"."policy_versions"` is the same statement.
//
// 🔴 AND `\s+` IS ONLY TRUE OF THE TEXT AFTER stripSQLComments RAN. A comment is
// whitespace to PostgreSQL and is not whitespace to `\s`, so this pattern is exactly
// as good as the stripper feeding it -- which is how `INSERT INTO /* audit */
// policy_versions` walked past round 3's version of this file. Widening the pattern to
// swallow comments would have been the wrong repair: the comment can sit between ANY
// two tokens, so it belongs to the lexer, not to one more alternation here.
var policyVersionWriteRE = regexp.MustCompile(
	`(?is)\b(?:INSERT\s+INTO|UPSERT\s+INTO|MERGE\s+INTO|UPDATE|DELETE\s+FROM|COPY)\s+` +
		`(?:ONLY\s+)?(?:"?public"?\s*\.\s*)?"?policy_versions"?\b`)

// sqlNameMarkerRE matches sqlc's query header. It is what makes a write in db/queries
// ATTRIBUTABLE, and sqlc copies the header verbatim into the generated Go constant,
// so the same segmentation reads both sides.
var sqlNameMarkerRE = regexp.MustCompile(`--[ \t]*name:[ \t]*([A-Za-z_][A-Za-z0-9_]*)[ \t]*:[ \t]*([a-z]+)`)

// sqlWrite is one matched write, with the line it sits on RELATIVE TO the chunk it
// was found in.
type sqlWrite struct {
	head string
	line int
}

// policyVersionWrites returns the writes against policy_versions in one chunk of SQL.
// Comments go first, and they go first in BOTH directions: this repository's SQL
// discusses the table at length -- the first version of this walk reported
// rulewriter.go's own doc comment as a write -- and a comment sitting INSIDE a
// statement hid a real write from round 3's version of it.
func policyVersionWrites(sqlText string) []sqlWrite {
	// stripSQLComments (insertscope_test.go) keeps every newline where it was, so the
	// line arithmetic below still points at the source.
	body := stripSQLComments(sqlText)
	var out []sqlWrite
	for _, loc := range policyVersionWriteRE.FindAllStringIndex(body, -1) {
		out = append(out, sqlWrite{
			head: strings.Join(strings.Fields(body[loc[0]:loc[1]]), " "),
			line: 1 + strings.Count(body[:loc[0]], "\n"),
		})
	}
	return out
}

// namedQuery is one `-- name:` section of a chunk. The text before the first header
// is returned with an empty name, because a write there belongs to nobody.
type namedQuery struct {
	name     string
	body     string
	bodyLine int
}

func splitNamedQueries(sqlText string) []namedQuery {
	locs := sqlNameMarkerRE.FindAllStringSubmatchIndex(sqlText, -1)
	if len(locs) == 0 {
		return []namedQuery{{body: sqlText, bodyLine: 1}}
	}
	var out []namedQuery
	if strings.TrimSpace(sqlText[:locs[0][0]]) != "" {
		out = append(out, namedQuery{body: sqlText[:locs[0][0]], bodyLine: 1})
	}
	for i, loc := range locs {
		end := len(sqlText)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		out = append(out, namedQuery{
			name:     sqlText[loc[2]:loc[3]],
			body:     sqlText[loc[1]:end],
			bodyLine: 1 + strings.Count(sqlText[:loc[1]], "\n"),
		})
	}
	return out
}

// attributedPolicyVersionWrites answers both halves of the question at once: WHICH
// named queries write policy_versions, and which writes belong to no name at all.
//
// 🔴 THE SECOND RETURN IS THE ONE THAT MATTERS. A write nobody can attribute is a
// write no derivation will ever list, which is how a hand-written store file or a
// migration would slip a document into the table.
func attributedPolicyVersionWrites(where, sqlText string) (byName map[string][]string, loose []string) {
	byName = map[string][]string{}
	for _, q := range splitNamedQueries(sqlText) {
		for _, w := range policyVersionWrites(q.body) {
			at := fmt.Sprintf("%s:%d", where, q.bodyLine+w.line-1)
			if q.name == "" {
				loose = append(loose, at+": "+w.head)
				continue
			}
			byName[q.name] = append(byName[q.name], at)
		}
	}
	return byName, loose
}

// ------------------------------------------------------------------ the Go side --

// stringConstant is a string CONSTANT the parser could evaluate, and its position.
type stringConstant struct {
	value string
	pos   token.Pos
}

// constConcat evaluates a `+` chain of string literals, so a statement written as
// "INSERT INTO " + "policy_versions ..." reads as one text. Go joins those at compile
// time; a check that did not would be reporting the source's line breaks rather than
// the statement.
func constConcat(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.ParenExpr:
		return constConcat(v.X)
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, ok := constConcat(v.X)
		if !ok {
			return "", false
		}
		r, ok := constConcat(v.Y)
		if !ok {
			return "", false
		}
		return l + r, true
	}
	return "", false
}

// stringConstantsIn collects every string constant in a parsed file.
//
// 🔴 IT PARSES INSTEAD OF READING LINES, and that closes escape (c) at the root
// rather than one writing at a time. A comment cannot be mistaken for code because
// the parser is given no comments at all; a raw string spanning ten lines is one
// value; and a `+` chain is joined before anything is matched.
func stringConstantsIn(f *ast.File) []stringConstant {
	var out []stringConstant
	ast.Inspect(f, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.BinaryExpr:
			if s, ok := constConcat(e); ok {
				out = append(out, stringConstant{s, e.Pos()})
				return false // the operands are already inside s
			}
		case *ast.BasicLit:
			if e.Kind == token.STRING {
				if s, err := strconv.Unquote(e.Value); err == nil {
					out = append(out, stringConstant{s, e.Pos()})
				}
			}
		}
		return true
	})
	return out
}

// eachShippedGoFile parses every shipped .go file whose repository-relative path
// `want` accepts, and reports how many it read.
//
// A file that will not parse is REPORTED rather than skipped: a file nothing can read
// is a file nothing is checking, and silence there is the same failure as a mistyped
// root.
func eachShippedGoFile(t *testing.T, want func(rel string) bool, fn func(rel string, fset *token.FileSet, f *ast.File)) int {
	t.Helper()
	fset := token.NewFileSet()
	read := 0
	err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
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
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(repoRoot, path)
		rel = filepath.ToSlash(rel)
		if !want(rel) {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Errorf("%s will not parse, so nothing in it was scanned: %v", rel, perr)
			return nil
		}
		read++
		fn(rel, fset, f)
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
	return read
}

// writerRef is one place in the shipped Go that NAMES a policy-document writer.
type writerRef struct {
	name string
	pos  token.Pos
}

// writerRefsIn returns every reference to a policy-document writer inside n.
//
// 🔴 IT READS NAMES, NOT CALLS, AND THAT IS THE FAZ B3 FIX. Its predecessor read the
// CALLEE of an *ast.CallExpr, so it saw `q.AppendPolicyVersion(ctx, arg)` and did not
// see the same write through a method VALUE:
//
//	write := q.AppendPolicyVersion
//	_, err := write(ctx, arg)
//
// Measured (2026-08-20, FAZ B3's security audit): injected as
// internal/handler/zzprobe.go that shape left `go build ./...` at exit 0 and
// `go test -run TestNoteProvenance_OnlyTwoProductionCallSitesWriteAPolicyDocument`
// at `ok`, while the identical write
// spelled as a direct call turned the gate red.
//
// A method on *store.Queries can only be reached through a SELECTOR -- `q.X`, or the
// method expression `(*store.Queries).X` -- and calling it is one of the things you
// may do with that selector rather than the only one. So the selector is what is
// counted. A bare identifier is read only when it is the thing being CALLED, which
// keeps every case the old check caught; counting a bare identifier ANYWHERE would
// report an interface DECLARING a method of that name, which writes nothing.
func writerRefsIn(n ast.Node) []writerRef {
	var out []writerRef
	ast.Inspect(n, func(node ast.Node) bool {
		switch e := node.(type) {
		case *ast.SelectorExpr:
			if _, isWriter := policyDocumentWriters[e.Sel.Name]; isWriter {
				out = append(out, writerRef{e.Sel.Name, e.Pos()})
			}
		case *ast.CallExpr:
			// `X()` with no receiver; `q.X()` is the selector case above, so this
			// arm cannot double-count it.
			if id, ok := e.Fun.(*ast.Ident); ok {
				if _, isWriter := policyDocumentWriters[id.Name]; isWriter {
					out = append(out, writerRef{id.Name, e.Pos()})
				}
			}
		}
		return true
	})
	return out
}

// storeConstName is sqlc's naming rule for the constant holding a query's SQL.
func storeConstName(query string) string {
	if query == "" {
		return ""
	}
	return strings.ToLower(query[:1]) + query[1:]
}

// ------------------------------------------------------------------- the checks --

// derivedPolicyVersionWriters reads db/ and answers which named queries write
// policy_versions, plus any write it could not attribute to a name.
func derivedPolicyVersionWriters(t *testing.T) (byName map[string][]string, loose []string, queriesSeen int) {
	t.Helper()
	byName = map[string][]string{}
	root := filepath.Join(repoRoot, "db")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".sql") {
			return nil
		}
		rel, _ := filepath.Rel(repoRoot, path)
		rel = filepath.ToSlash(rel)
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("reading %s: %v", rel, rerr)
		}
		for _, q := range splitNamedQueries(string(raw)) {
			if q.name != "" {
				queriesSeen++
			}
		}
		named, unattributed := attributedPolicyVersionWrites(rel, string(raw))
		for name, at := range named {
			byName[name] = append(byName[name], at...)
		}
		loose = append(loose, unattributed...)
		return nil
	})
	if err != nil {
		t.Fatalf("walking db/: %v", err)
	}
	return byName, loose, queriesSeen
}

// TestNoteProvenance_TheWriterListIsDerivedFromTheQueriesRatherThanNamed closes
// escape (b): db/ was never opened, so the directory CLAUDE.md §6 says every query
// lives in was the one place a third writer could be added in silence.
//
// It derives the writer set from what each named query DOES and compares it with
// policyDocumentWriters, so the map above stops being an allow-list and becomes a
// claim that is checked.
func TestNoteProvenance_TheWriterListIsDerivedFromTheQueriesRatherThanNamed(t *testing.T) {
	t.Parallel()

	byName, loose, queriesSeen := derivedPolicyVersionWriters(t)

	// CONTROL: the walk really read db/. Every assertion below is vacuous on a tree
	// where the segmentation stopped matching, and that is the failure mode every
	// scanner in this repository has hit at least once.
	if queriesSeen < 100 {
		t.Fatalf("CONTROL FAILED: only %d named queries were found under db/; 109 were there when "+
			"this floor was set (2026-08-20, measured by this test), so the segmentation has gone "+
			"blind and nothing below was measured", queriesSeen)
	}
	t.Logf("%d named queries read under db/", queriesSeen)

	for _, at := range loose {
		t.Errorf("🔴 %s writes policy_versions and belongs to no named query.\n"+
			"   A statement outside sqlc's `-- name:` form is one no derivation will ever "+
			"list, and a policy document reaching the table without going through "+
			"tenant.Author or checkin's materialise is a document nothing validated and "+
			"nothing copied from a shipped statement -- the one way a customer's own prose "+
			"could become tap.Decision.Note and appear, unlabelled, on an employee's screen.", at)
	}

	for name, at := range byName {
		if _, known := policyDocumentWriters[name]; known {
			t.Logf("%s writes policy_versions, declared at %s", name, strings.Join(at, ", "))
			continue
		}
		t.Errorf("🔴 db/ declares a THIRD query that writes policy_versions: %s (%s).\n"+
			"   The provenance comments on ledger.Record.Note, components.DocketView.Note, "+
			"pages.ResultView.Note, db/queries/transactions.sql and db/queries/reviews.sql all "+
			"rest on there being TWO writers of a policy document, both of them fed by prose "+
			"this binary ships. A third one -- M9-07's raw-JSON editor is the shape Q22 "+
			"anticipates -- makes a customer the author of the sentence a tap shows, and every "+
			"one of those comments has to be rewritten in the same change.",
			name, strings.Join(at, ", "))
	}
	for name := range policyDocumentWriters {
		if len(byName[name]) == 0 {
			t.Errorf("CONTROL FAILED: no query under db/ writes policy_versions through %s. Either "+
				"the query was renamed or this scan is not reading it; in both cases the map it "+
				"is checked against is excusing a name that no longer exists.", name)
		}
	}
}

// TestNoteProvenance_TheStoreCarriesNoWriterTheQueriesDoNotName closes escape (a).
//
// 🔴 THE OLD WALK SKIPPED internal/store OUTRIGHT, on the true-but-insufficient
// ground that sqlc's output DECLARES the two methods and reporting it would report
// the generator. What that reasoning missed is that a directory nothing reads is a
// directory anything can be added to: a hand-written `func (q *Queries) X` with an
// INSERT in it compiles, is callable from production, and was invisible to all four
// gates.
//
// The rule is behavioural rather than nominal: internal/store may hold a write
// against policy_versions only under a `-- name:` header that db/ ALSO declares, once
// each, and the constant carrying that SQL may be used only by the function of the
// same name.
func TestNoteProvenance_TheStoreCarriesNoWriterTheQueriesDoNotName(t *testing.T) {
	t.Parallel()

	declared, _, _ := derivedPolicyVersionWriters(t)

	inStore := func(rel string) bool { return strings.HasPrefix(rel, "internal/store/") }

	segments := map[string][]string{} // query name -> where its writing SQL sits
	var loose []string
	constUsers := map[string]map[string]bool{} // query const -> functions using it
	var calls []string

	consts := map[string]string{} // const identifier -> query name
	for name := range policyDocumentWriters {
		consts[storeConstName(name)] = name
	}

	read := eachShippedGoFile(t, inStore, func(rel string, fset *token.FileSet, f *ast.File) {
		for _, lit := range stringConstantsIn(f) {
			at := fmt.Sprintf("%s:%d", rel, fset.Position(lit.pos).Line)
			named, unattributed := attributedPolicyVersionWrites(at, lit.value)
			for name := range named {
				segments[name] = append(segments[name], at)
			}
			loose = append(loose, unattributed...)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if id, isIdent := n.(*ast.Ident); isIdent {
					if q, isQueryConst := consts[id.Name]; isQueryConst {
						if constUsers[q] == nil {
							constUsers[q] = map[string]bool{}
						}
						constUsers[q][fn.Name.Name] = true
					}
				}
				return true
			})
			for _, ref := range writerRefsIn(fn.Body) {
				calls = append(calls, fmt.Sprintf("%s:%d names %s",
					rel, fset.Position(ref.pos).Line, ref.name))
			}
		}
	})

	// CONTROL: the generated layer was really read.
	if read < 15 {
		t.Fatalf("CONTROL FAILED: only %d file(s) were parsed under internal/store; 19 were there "+
			"when this floor was set (2026-08-20, measured by this test), so nothing below was "+
			"measured", read)
	}
	t.Logf("%d file(s) parsed under internal/store", read)

	for _, at := range loose {
		t.Errorf("🔴 %s writes policy_versions from internal/store under no `-- name:` header.\n"+
			"   sqlc copies that header into every constant it emits, so SQL without one was not "+
			"generated -- it was written by hand into the one directory this repository treats as "+
			"machine output and therefore does not read. That is a writer of the table with no "+
			"query behind it and no review of the document it writes.", at)
	}

	for name, at := range segments {
		if len(declared[name]) == 0 {
			t.Errorf("🔴 internal/store holds a writer of policy_versions that db/ does not declare: "+
				"%s (%s).\n   Either it was hand-written, or a query was deleted from db/queries "+
				"without regenerating. Both mean the generated layer and the SQL this repository "+
				"reviews have parted company for the table that holds every policy document.",
				name, strings.Join(at, ", "))
			continue
		}
		if len(at) != 1 {
			t.Errorf("🔴 %s writes policy_versions from %d places in internal/store (%s); sqlc emits "+
				"one constant per query, so a second one is a copy nobody generated -- and a copy "+
				"under a name this file trusts is how a forged header would arrive.",
				name, len(at), strings.Join(at, ", "))
		}
	}
	for name := range policyDocumentWriters {
		if len(segments[name]) == 0 {
			t.Errorf("CONTROL FAILED: internal/store carries no writing SQL for %s, so this scan "+
				"measured nothing about it. sqlc's output and db/queries have to disagree for that "+
				"to happen, which is itself the finding.", name)
		}
	}

	// AND THE CONSTANT ITSELF. A hand-written method needs no SQL of its own if it can
	// hand the generated constant to q.db.Exec, so the constant's users are pinned too.
	for constIdent, name := range consts {
		users := constUsers[name]
		if len(users) == 0 {
			t.Errorf("CONTROL FAILED: nothing in internal/store uses %s. sqlc emits a constant and "+
				"exactly one function that runs it, so zero users means this check is reading the "+
				"wrong identifier and proves nothing.", constIdent)
			continue
		}
		for fn := range users {
			if fn == name {
				continue
			}
			t.Errorf("🔴 internal/store.%s runs %s, and the only function that may is %s.\n"+
				"   A second runner of the query that appends a policy version is a second write "+
				"path with no query, no name and no caller check -- it would be called from "+
				"production under a name none of these gates knows.", fn, constIdent, name)
		}
	}

	// sqlc never names its own methods inside a function body; production does. A
	// reference inside the generated layer means something there is wrapping a writer
	// -- as a call, or as the method VALUE that FAZ B3 measured walking past this.
	for _, at := range calls {
		t.Errorf("🔴 %s. internal/store is generated output and does not call itself, so this is a "+
			"wrapper around a policy-document writer -- and a wrapper is what the caller check in "+
			"TestNoteProvenance_OnlyTwoProductionCallSitesWriteAPolicyDocument sees only as its "+
			"own harmless name.", at)
	}
}

// TestNoteProvenance_OnlyTwoProductionCallSitesWriteAPolicyDocument walks the shipped
// Go outside the generated layer and reports two things: a call of either writer from
// a file that is not its one allowed caller, and a string constant in that Go holding
// a statement that writes the table without them.
func TestNoteProvenance_OnlyTwoProductionCallSitesWriteAPolicyDocument(t *testing.T) {
	t.Parallel()

	found := map[string][]string{}
	var rawSQL []string

	// internal/store is sqlc output: it DECLARES the methods this test is about, and
	// TestNoteProvenance_TheStoreCarriesNoWriterTheQueriesDoNotName is what reads it.
	outsideStore := func(rel string) bool { return !strings.HasPrefix(rel, "internal/store/") }

	read := eachShippedGoFile(t, outsideStore, func(rel string, fset *token.FileSet, f *ast.File) {
		for _, ref := range writerRefsIn(f) {
			found[ref.name] = append(found[ref.name],
				fmt.Sprintf("%s:%d", rel, fset.Position(ref.pos).Line))
		}
		// AND THE BYPASS. A raw INSERT would write the same column without touching
		// either method, which is exactly how the positive control in
		// internal/domain/checkin does it -- so shipped code doing the same has to be
		// reported rather than assumed impossible.
		for _, lit := range stringConstantsIn(f) {
			for _, w := range policyVersionWrites(lit.value) {
				rawSQL = append(rawSQL, fmt.Sprintf("%s:%d: %s",
					rel, fset.Position(lit.pos).Line, w.head))
			}
		}
	})

	// CONTROL: the walk reached the tree. 161 shipped .go files sit outside
	// internal/store on this one (2026-08-20, measured by this test).
	if read < 130 {
		t.Fatalf("CONTROL FAILED: only %d shipped .go file(s) were parsed; the walk is not reaching "+
			"the tree and every result below is vacuous", read)
	}
	t.Logf("%d shipped .go file(s) parsed outside internal/store", read)

	for _, where := range rawSQL {
		t.Errorf("🔴 %s writes policy_versions with SQL held in shipped Go.\n"+
			"   A policy document reaching the table without going through tenant.Author or "+
			"checkin's materialise is a document nothing validated and nothing copied from a "+
			"shipped statement -- which is the one way a customer's own prose could become "+
			"tap.Decision.Note and appear, unlabelled, on an employee's screen.", where)
	}

	for method, allowed := range policyDocumentWriters {
		sites := found[method]
		sort.Strings(sites)

		// CONTROL: the walk really reached the code this rule is about. Zero call
		// sites is not a pass -- it is the shape of "green because nothing was
		// measured", and it is what a mistyped root or a widened skip list produces.
		if len(sites) == 0 {
			t.Errorf("CONTROL FAILED: no production call of %s was found anywhere. Either "+
				"the walk is not reaching %s or the method was renamed; in both cases every "+
				"result from this test is vacuous.", method, allowed)
			continue
		}
		for _, site := range sites {
			if strings.HasPrefix(site, allowed+":") {
				continue
			}
			t.Errorf("🔴 %s names %s -- as a call, a method value or a method expression -- "+
				"and the only production file that may is %s.\n"+
				"   The provenance comments on ledger.Record.Note, "+
				"components.DocketView.Note, pages.ResultView.Note, "+
				"db/queries/transactions.sql and db/queries/reviews.sql all rest on there "+
				"being TWO writers of a policy document, both of them fed by prose this "+
				"binary ships. A third one -- M9-07's raw-JSON editor is the shape Q22 "+
				"anticipates -- makes a customer the author of the sentence a tap shows, "+
				"and every one of those comments has to be rewritten in the same change.",
				site, method, allowed)
		}
		t.Logf("%s: %d production call site(s), all in %s", method, len(sites), allowed)
	}
}

// TestNoteProvenanceScan_ReportsTheBypassesItExistsToReport is the negative control.
//
// 🔴 IT DRIVES THE SCAN, IT DOES NOT RE-IMPLEMENT IT. Every case below is one of the
// three escapes round 3's audit compiled and measured green, expressed as the text
// the real walk would have handed to the same functions. A scan with no proof that it
// fires is a scan that may have stopped reading -- and this one had, in three places
// at once, while four gates reported success.
func TestNoteProvenanceScan_ReportsTheBypassesItExistsToReport(t *testing.T) {
	t.Parallel()

	t.Run("escape (b): a new named query in db/queries", func(t *testing.T) {
		named, loose := attributedPolicyVersionWrites("db/queries/policies.sql", `
-- name: ForcePolicyDocument :exec
INSERT INTO policy_versions (tenant_id, policy_id, document)
VALUES (@tenant_id, @policy_id, @document);
`)
		if len(named["ForcePolicyDocument"]) == 0 {
			t.Errorf("the scan did not attribute the write to ForcePolicyDocument: %v / %v", named, loose)
		}
		if _, known := policyDocumentWriters["ForcePolicyDocument"]; known {
			t.Error("the control is broken: ForcePolicyDocument must not be a known writer")
		}
	})

	t.Run("escape (a): SQL in the store under no header", func(t *testing.T) {
		_, loose := attributedPolicyVersionWrites("internal/store/handwritten.go:9", `
INSERT INTO policy_versions (tenant_id, policy_id, document) VALUES ($1, $2, $3)
`)
		if len(loose) != 1 {
			t.Errorf("a write with no `-- name:` header produced %d unattributed finding(s), want 1", len(loose))
		}
	})

	t.Run("escape (a): SQL in the store under a header db/ does not declare", func(t *testing.T) {
		named, _ := attributedPolicyVersionWrites("internal/store/handwritten.go:9", `
-- name: ReplacePolicyDocument :exec
INSERT INTO policy_versions (tenant_id, policy_id, document) VALUES ($1, $2, $3)
`)
		if len(named["ReplacePolicyDocument"]) == 0 {
			t.Error("the scan did not see a hand-written store query at all")
		}
	})

	// 🔴 THE SHAPE THAT USED TO PASS, and the reason the check is no longer per-line.
	for name, sql := range map[string]string{
		"the statement broken across two lines": "INSERT INTO\npolicy_versions (tenant_id, document)\nVALUES ($1, $2)",
		"an indented continuation":              "INSERT INTO\n\t\tpolicy_versions (document) VALUES ($1)",
		"schema-qualified and quoted":           `INSERT INTO "public"."policy_versions" (document) VALUES ($1)`,
		"lower case":                            "insert into policy_versions (document) values ($1)",
		"an UPDATE of the document column":      "UPDATE policy_versions SET document = $1 WHERE id = $2",
		"a DELETE":                              "DELETE FROM policy_versions WHERE id = $1",
		// 🔴 THE TWO ROUND-4 ESCAPES. Both parse as an INSERT on this database --
		// EXPLAIN (COSTS OFF) inside BEGIN … ROLLBACK answers "Insert on
		// policy_versions" for each -- and both matched nothing until the stripper
		// became a lexer. The `\s+` in the pattern is only as good as what ran first.
		"a block comment between INSERT INTO and the table":   "INSERT INTO /* audit */ policy_versions (document) VALUES ($1)",
		"a block comment with no space around it":             "INSERT INTO/*audit*/policy_versions (document) VALUES ($1)",
		"a line comment between INSERT INTO and the table":    "INSERT INTO -- audit\npolicy_versions (document) VALUES ($1)",
		"a block comment spanning lines inside the statement": "INSERT INTO /* why\nthis exists */ policy_versions (document) VALUES ($1)",
		"a comment before the table in an UPDATE":             "UPDATE /* audit */ policy_versions SET document = $1",
		"a comment after a trailing bit of real SQL":          "SELECT 1; -- x\nINSERT INTO policy_versions (document) VALUES ($1)",
		// A `--` inside a STRING is not a comment, so it must not blind the scan to
		// the statement that follows it.
		"a write after a string literal containing --": "SELECT '-- not a comment'; INSERT INTO policy_versions (document) VALUES ($1)",
		// blockOpen (insertscope_test.go) rather than the two characters, for the
		// reason its own comment gives: written out here, it opens a strip that runs
		// past the declarations below and hides them from TestEveryNamedTestExists.
		"a write after a string literal containing a block open": "SELECT '" + blockOpen + " not a comment'; INSERT INTO policy_versions (document) VALUES ($1)",
	} {
		if len(policyVersionWrites(sql)) == 0 {
			t.Errorf("%s: the scan reported nothing.\nSQL: %q", name, sql)
		}
	}

	// AND IT MUST STAY QUIET ON WHAT IS NOT A WRITE, or the next author waters it down.
	for name, sql := range map[string]string{
		"a read":                     "SELECT document FROM policy_versions WHERE policy_id = $1",
		"a join":                     "SELECT p.id FROM policies p JOIN policy_versions v ON v.policy_id = p.id",
		"a comment line":             "-- policy_versions revokes UPDATE and DELETE from tappa_app\nSELECT 1",
		"a write to another table":   "INSERT INTO policy_attachments (tenant_id, policy_id) VALUES ($1, $2)",
		"a table whose name extends": "INSERT INTO policy_versions_archive (document) VALUES ($1)",
		// The other half of the lexer, and the half that keeps it from becoming
		// noise: a statement PostgreSQL will never run is not a write. (SQL held in a
		// string LITERAL is a different matter and is deliberately still reported --
		// `SELECT 'INSERT INTO policy_versions'` fires. It is data to PostgreSQL and
		// the raw material of a dynamic statement to this repository, so the
		// over-reporting direction is the one worth having.)
		"a write commented out with a block comment": "/* INSERT INTO policy_versions (document) VALUES ($1) */",
		"a write commented out with a line comment":  "-- INSERT INTO policy_versions (document) VALUES ($1)",
		"a write commented out mid-line":             "SELECT 1; -- INSERT INTO policy_versions (document) VALUES ($1)",
	} {
		if n := policyVersionWrites(sql); len(n) != 0 {
			t.Errorf("%s: the scan reported %d finding(s) on a statement that writes nothing.\nSQL: %q",
				name, len(n), sql)
		}
	}

	// escape (c) AT THE SOURCE LEVEL: the two writings a per-line check walked past,
	// read the way the real walk reads them.
	for name, src := range map[string]string{
		"a raw string spanning lines": "package p\n\nconst q = `INSERT INTO\npolicy_versions (document)\nVALUES ($1)`\n",
		"a `+` chain":                 "package p\n\nconst q = \"INSERT INTO \" + \"policy_versions (document) VALUES ($1)\"\n",
	} {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "control.go", src, 0)
		if err != nil {
			t.Fatalf("%s: the control source will not parse: %v", name, err)
		}
		hits := 0
		for _, lit := range stringConstantsIn(f) {
			hits += len(policyVersionWrites(lit.value))
		}
		if hits == 0 {
			t.Errorf("%s: the scan found no write.\nsource: %s", name, src)
		}
	}

	// A COMMENT IS NOT CODE, and this repository's comments discuss the table at
	// length -- the first version of this walk reported rulewriter.go's own doc as a
	// raw write.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "control.go",
		"package p\n\n// INSERT INTO policy_versions is what this function does not do.\nfunc f() {}\n", 0)
	if err != nil {
		t.Fatalf("the comment control will not parse: %v", err)
	}
	for _, lit := range stringConstantsIn(f) {
		if len(policyVersionWrites(lit.value)) != 0 {
			t.Error("a comment was read as a write; the scan is not reading syntax")
		}
	}

	// 🔴 AND WHO NAMES A WRITER, which is the D55 escape. `write := q.X` and
	// `q.X(…)` are the same write, and only the second was visible until FAZ B3.
	for name, tc := range map[string]struct {
		src  string
		want int
	}{
		"a direct call": {
			"package p\n\nfunc f(q Q) { _, _ = q.AppendPolicyVersion(ctx, arg) }\n", 1},
		"a method VALUE, the shape that used to be invisible": {
			"package p\n\nfunc f(q Q) { write := q.AppendPolicyVersion; _, _ = write(ctx, arg) }\n", 1},
		"a method expression": {
			"package p\n\nvar w = (*store.Queries).EnsureBaselinePolicyVersion\n", 1},
		"a value handed to something else": {
			"package p\n\nfunc f(q Q) { register(q.AppendPolicyVersion) }\n", 1},
		"a value parked in a struct literal": {
			"package p\n\nvar d = deps{write: q.EnsureBaselinePolicyVersion}\n", 1},
		"a value returned": {
			"package p\n\nfunc f(q Q) any { return q.AppendPolicyVersion }\n", 1},
		"another method entirely": {
			"package p\n\nfunc f(q Q) { _, _ = q.GetPolicyByID(ctx, id) }\n", 0},
		// A method DECLARED on an interface writes nothing; only using it does.
		"an interface declaring the method": {
			"package p\n\ntype W interface{ AppendPolicyVersion(c C, a A) (R, error) }\n", 0},
	} {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "control.go", tc.src, 0)
		if err != nil {
			t.Fatalf("%s: the control source will not parse: %v", name, err)
		}
		if got := len(writerRefsIn(f)); got != tc.want {
			t.Errorf("%s: writerRefsIn found %d reference(s), want %d.\nsource: %s",
				name, got, tc.want, tc.src)
		}
	}

	// AND THE CONSTANT NAMING RULE, which the store check depends on.
	for query, want := range map[string]string{
		"AppendPolicyVersion":         "appendPolicyVersion",
		"EnsureBaselinePolicyVersion": "ensureBaselinePolicyVersion",
	} {
		if got := storeConstName(query); got != want {
			t.Errorf("storeConstName(%q) = %q, want %q", query, got, want)
		}
	}
}
