package ledger

// query_test.go -- §4.5's BELT, asserted separately from its BRACES.
//
// 🔴 THIS FILE EXISTS BECAUSE A MUTATION SURVIVED. M6-03's end-to-end isolation
// test (internal/handler/transactions_db_test.go) puts two tenants in real
// Postgres and proves that tenant A's panel never renders tenant B's records. It
// is a genuine net and it passes. But when the explicit `t.tenant_id = @tenant_id`
// predicate was DELETED from ListPanelTransactions and the whole thing re-run, the
// test stayed GREEN -- because RLS alone still blocked every foreign row.
//
// That is not a defect in the end-to-end test; it is the thing CLAUDE.md §6 warns
// about from the other direction. §4.5 asks for TWO independent defences, and a
// behavioural test can only ever observe their CONJUNCTION: as long as either one
// holds, nothing leaks and every behavioural assertion passes. So the second
// defence needs a test that can see it directly, and the only place it is visible
// is the SQL itself.
//
// 🔴 THE FIRST VERSION OF THIS FILE WAS A SYNTAX CHANGE DETECTOR, AND AN AUDIT
// MEASURED FIVE WAYS THROUGH IT. It matched one literal spelling of the predicate,
// which meant:
//
//	WRONG ALARMS  `@tenant_id = t.tenant_id` -- semantically identical, reversed --
//	              failed. So would a cast or a different alias.
//	REAL HOLES    `(t.tenant_id = @tenant_id OR TRUE)` passed while scoping nothing;
//	              the predicate MOVED into a `LEFT JOIN ... ON` passed while the
//	              main table went unscoped; blinding queryBody to return the whole
//	              file passed; and a NEW panel query with no predicate at all was
//	              never looked at, because the query list was hand-written.
//
// All five are closed below by asserting the PROPERTY instead of the spelling: the
// query's own WHERE clause must carry, as a TOP-LEVEL CONJUNCT, an equality
// between its table's scope column and the caller's tenant parameter.
//
// WHAT THIS BUYS, STATED HONESTLY: it does not make the product safer on its own --
// RLS is what actually stops the rows. What it adds is that deleting the second
// defence from a query THIS SCAN CAN SEE turns a test red.
//
// 🔴 THE SENTENCE THAT USED TO BE HERE SAID "non-deletable without a red test" AND AN
// AUDIT MEASURED IT FALSE. With the call written as `q.` + newline + `Method(`, the
// whole `t.tenant_id = @tenant_id` predicate was removed from ListFlaggedForReview
// and both belt tests stayed green -- gofmt -l empty, go vet clean, go build fine.
// The scan is an AST now (storeQueryNames) and all three shapes that beat it are
// caught, verified one by one; but "non-deletable" is an absolute, and this file has
// now been wrong about it once. What is claimed is bounded by two things it does not
// cover, both stated at storeQueryNames and at
// TestPanelQueries_CarryAnExplicitTenantPredicate: a query named only as a string
// (reflection), and a query called from a package this scan does not read.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// tenantParam is the parameter every scoped query binds its scope column to.
const tenantParam = "@tenant_id"

// panelQueries DERIVES the queries to check by reading this package's own source.
//
// 🔴 IT IS DERIVED RATHER THAN LISTED, and that is the fix for the fifth hole. A
// hand-written list is a change detector: a new panel read with no tenant predicate
// was simply not in it, so it passed silently -- the failure mode where the natural
// repair (add the newcomer to the list) is exactly the wrong move. Now every store
// query this package calls is checked because it is called, and adding an unscoped
// one turns this red on the next run.
func panelQueries(t *testing.T) []string {
	t.Helper()
	return storeQueryNames(t, declaredQueries(t))
}

// 🔴 HOW MUCH OF THE PRODUCT THIS BELT ACTUALLY COVERS — 8 QUERIES OF 41, MEASURED.
// The number is here because "the belt" reads as though it covered the product and
// it covers two domain packages:
//
//	declared in db/queries   41   grep -rhoE '^-- name: [A-Za-z_]+' db/queries/*.sql | wc -l
//	seen by this net + the
//	  one in domain/review    8   CountPendingFlagged, GetTenantClock,
//	                              GetTransactionReview, InsertTransactionReview,
//	                              ListDepartmentsForTenant, ListLocationsForTenant,
//	                              ListFlaggedForReview, ListPanelTransactions
//	                              => 20%
//
// So for the other 33 — InsertTransaction, GetLastOpenTransaction,
// CreateAdminSession and the rest — §4.5's SECOND defence is asserted by NO test at
// all. RLS still stops the rows; what is unguarded is the explicit predicate beside
// it, which is the thing this file exists to make non-deletable.
//
// ⚠️ WIDENING IT WAS MEASURED AND REJECTED, and the reason is recorded in
// internal/domain/review/query_test.go: running this over every internal/domain
// package turns RED on code that is correct today — LockEmployeeForTap is an
// advisory lock with no WHERE clause at all. Closing the gap properly means a
// per-query opt-out with a reason, which is a task rather than a line.
//
// TestPanelQueries_CarryAnExplicitTenantPredicate is the belt.
func TestPanelQueries_CarryAnExplicitTenantPredicate(t *testing.T) {
	names := panelQueries(t)
	// ANTI-VACUITY: an empty derivation would make every assertion pass over nothing.
	if len(names) < 4 {
		t.Fatalf("derived %d store call(s) from ledger.go (%v); the panel read path "+
			"makes more than that, so the scan is reading the wrong file", len(names), names)
	}

	for _, name := range names {
		file, body, ok := findQuery(t, name)
		if !ok {
			t.Errorf("no query named %q anywhere in db/queries -- ledger.go calls it, so "+
				"either it was renamed or this scan cannot see it", name)
			continue
		}
		blocks := whereBlocks(body)
		if len(blocks) == 0 {
			t.Errorf("%s (%s) has no WHERE clause at all, so it is scoped by RLS alone.\n"+
				"§4.5 asks for belt AND braces.", name, file)
			continue
		}
		// EVERY block, not just the first: ListPanelTransactions opens with a
		// MATERIALIZED CTE that reads `employees`, so the query now has two WHERE
		// clauses and BOTH read a tenant table. Checking one of them would have
		// checked the wrong one.
		for _, blk := range blocks {
			if scopedByTenant(blk.where, blk.column) {
				continue
			}
			t.Errorf("%s (%s) has a WHERE clause that does not bind %s to %s as a "+
				"TOP-LEVEL CONJUNCT.\n"+
				"§4.5 asks for belt AND braces: RLS scopes the transaction, and the query "+
				"says the same thing again. Without it, every behavioural isolation test "+
				"still passes -- RLS alone blocks the rows -- so this is the only place "+
				"the second defence is visible. It was MEASURED surviving on this exact "+
				"query, which is why this test exists.\n\n"+
				"A predicate inside an OR, or moved into a JOIN ... ON, does NOT count: "+
				"neither scopes the block's own result. A CTE or subquery reading a "+
				"tenant table needs its OWN predicate.\n\nWHERE clause read:\n%s",
				name, file, blk.column, tenantParam, blk.where)
		}
	}
}

// scopeColumnFor DERIVES which column scopes this query's primary table.
//
// 🔴 IT IS NOT ALWAYS tenant_id. `tenants` has no such column -- it IS the tenant,
// so its own primary key is the scope, and migration 0001's RLS policy says exactly
// that: USING (id = NULLIF(current_setting(...), <empty string>)::uuid).
//
// The first version of this test hard-coded tenant_id and reported GetTenantClock
// as unscoped, which was wrong about the product rather than about the query.
func scopeColumnFor(before string) string {
	// The LAST FROM before this WHERE names the table the clause filters. With a
	// CTE there is more than one, and taking the first would name the wrong table.
	froms := regexp.MustCompile(`(?i)\bFROM\s+([a-z_]+)`).FindAllStringSubmatch(before, -1)
	if len(froms) > 0 && strings.EqualFold(froms[len(froms)-1][1], "tenants") {
		return "id"
	}
	return "tenant_id"
}

// whereBlock is one WHERE clause and the scope column of the table it filters.
type whereBlock struct {
	where  string
	column string
}

// whereBlocks returns EVERY top-level WHERE clause in the query, each paired with
// the scope column of its own table.
//
// 🔴 IT USED TO RETURN ONLY THE FIRST, AND ONE EDIT OUTGREW THAT.
// ListPanelTransactions now opens with a MATERIALIZED CTE that reads `employees`,
// so the first WHERE in the text belongs to the CTE and the main query's is the
// second. Returning one of them checked the wrong clause -- and reading only the
// first declared the query unscoped while it was in fact scoped twice.
//
// CHECKING ALL OF THEM IS STRICTLY STRONGER: a CTE or subquery that reads a tenant
// table is a read that needs its own predicate, and this makes adding one without
// a scope go red.
//
// JOIN ... ON CLAUSES REMAIN STRUCTURALLY EXCLUDED -- they precede their WHERE and
// are not one. A condition there constrains the JOIN, not the rows returned.
func whereBlocks(body string) []whereBlock {
	var out []whereBlock
	whereRE := regexp.MustCompile(`(?is)\bWHERE\b`)
	stopRE := regexp.MustCompile(`(?is)\b(ORDER\s+BY|GROUP\s+BY|LIMIT|HAVING)\b`)
	for _, loc := range whereRE.FindAllStringIndex(body, -1) {
		rest := body[loc[1]:]
		// The clause ends at the enclosing block's closing paren -- where a CTE or
		// subquery ends -- or at a clause that may follow WHERE.
		depth, end := 0, len(rest)
		for i := 0; i < len(rest); i++ {
			if rest[i] == '(' {
				depth++
				continue
			}
			if rest[i] == ')' {
				if depth == 0 {
					end = i
					break
				}
				depth--
			}
		}
		clause := rest[:end]
		if k := stopRE.FindStringIndex(clause); k != nil {
			clause = clause[:k[0]]
		}
		clause = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(clause), ";"))
		if clause == "" {
			continue
		}
		out = append(out, whereBlock{where: clause, column: scopeColumnFor(body[:loc[0]])})
	}
	return out
}

// scopedByTenant reports whether one TOP-LEVEL conjunct of the WHERE clause is an
// equality between the scope column and the tenant parameter.
//
// TOP-LEVEL IS THE WHOLE POINT. `a = @tenant_id AND b` scopes the query;
// `(a = @tenant_id OR TRUE)` does not, and the difference is only visible if the
// clause is split on AND at parenthesis depth zero.
func scopedByTenant(where, column string) bool {
	for _, conjunct := range splitTopLevelAnd(where) {
		if isScopeEquality(conjunct, column) {
			return true
		}
	}
	return false
}

// splitTopLevelAnd splits on AND that is not inside parentheses.
func splitTopLevelAnd(s string) []string {
	var parts []string
	depth, last := 0, 0
	upper := strings.ToUpper(s)
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth != 0 {
			continue
		}
		if strings.HasPrefix(upper[i:], "AND") && wordBoundary(upper, i, 3) {
			parts = append(parts, s[last:i])
			last = i + 3
		}
	}
	return append(parts, s[last:])
}

func wordBoundary(s string, i, n int) bool {
	before := i == 0 || !isWordByte(s[i-1])
	after := i+n >= len(s) || !isWordByte(s[i+n])
	return before && after
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

// isScopeEquality accepts either operand order, an optional table alias and any
// casts -- all of which are the SAME predicate, and rejecting them was a wrong
// alarm the first version produced.
func isScopeEquality(conjunct, column string) bool {
	c := strings.TrimSpace(conjunct)
	c = regexp.MustCompile(`::\s*[a-zA-Z_]+`).ReplaceAllString(c, "")
	c = strings.Trim(c, "() \t\n")
	c = strings.Join(strings.Fields(c), " ")

	// The alias may contain DIGITS (e2, t1). An earlier `[a-z_]+` rejected `e2.`
	// and reported a correctly scoped CTE as unscoped -- the test caught it, which
	// is the point of having a matcher that can be wrong in only one direction.
	col := `(?:[a-z_][a-z0-9_]*\.)?` + regexp.QuoteMeta(column)
	param := regexp.QuoteMeta(tenantParam)
	forward := regexp.MustCompile(`(?i)^` + col + ` = ` + param + `$`)
	reverse := regexp.MustCompile(`(?i)^` + param + ` = ` + col + `$`)
	return forward.MatchString(c) || reverse.MatchString(c)
}

// findQuery locates a named sqlc query in db/queries and returns its SQL.
func findQuery(t *testing.T, name string) (file, body string, ok bool) {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "db", "queries")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading db/queries: %v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		if b, found := queryBody(t, filepath.Join(dir, e.Name()), name); found {
			return e.Name(), b, true
		}
	}
	return "", "", false
}

// queryMarker matches a `-- name: X ` header ANCHORED TO THE START OF A LINE, which
// is the same pattern declaredQueries uses. The two must not drift apart -- see
// queryBody.
func queryMarker(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^-- name: ` + regexp.QuoteMeta(name) + ` `)
}

// queryBody returns the SQL of one named sqlc query, WITHOUT its comment lines --
// so a tenant_id mentioned only in prose cannot satisfy the check.
//
// 🔴 THE MARKER IS LINE-ANCHORED, AND IT WAS NOT UNTIL AN AUDIT WALKED THROUGH THE
// CONSEQUENCE. This used to be strings.Index(raw, "-- name: X "), unanchored, while
// declaredQueries a few lines below used `(?m)^-- name: (\w+) `. Two patterns for
// one fact, and the loose one belonged to the READER. Measured end to end:
//
//  1. delete `t.tenant_id = @tenant_id` from ListFlaggedForReview entirely
//  2. add ONE comment line to admins.sql -- alphabetically earlier, and files are
//     walked in os.ReadDir order:
//     -- cross-reference: the panel's queue read is -- name: ListFlaggedForReview :many
//  3. leave the call site alone
//     => make sqlc succeeds silently, internal/store is regenerated with the UNSCOPED
//     query, production now runs it, and `make test` answers ok x 16, exit 0.
//
// The reader resolved the name to a position inside a COMMENT in another file, and
// the "body" it returned was whatever followed -- the next query's. Positive control:
// removing only the comment (predicate still deleted) turns the belt red, so the
// comment was the sole cause.
//
// ⚠️ ANCHORING IS NOT A PROOF OF UNIQUENESS. A line that genuinely begins `-- name: X `
// inside another file would still be found first; that is a duplicate query name,
// which sqlc itself rejects, so it is not reachable here -- but this function does
// not check it and does not claim to.
func queryBody(t *testing.T, path, name string) (string, bool) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	loc := queryMarker(name).FindStringIndex(string(raw))
	if loc == nil {
		return "", false
	}
	rest := string(raw)[loc[1]:]
	// The NEXT header is also line-anchored, for the same reason: a `-- name: ` inside
	// a comment must not be able to truncate this body either.
	if j := regexp.MustCompile(`(?m)^-- name: `).FindStringIndex(rest); j != nil {
		rest = rest[:j[0]]
	}
	var body []string
	for _, line := range strings.Split(rest, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		body = append(body, line)
	}
	return strings.Join(body, "\n"), true
}

// TestPanelQueries_TheScopeCheckIsNotVacuous is the negative control for the
// MATCHER, and TestQueryBody_IsBounded below is the one for the READER. An audit
// found only the first existed, so blinding queryBody to return the whole file left
// the belt test green.
func TestPanelQueries_TheScopeCheckIsNotVacuous(t *testing.T) {
	unscoped := map[string]string{
		"no predicate at all":               "occurred_at >= @from_at",
		"the mutation that survived":        "(@tenant_id IS NOT NULL)",
		"neutralised by OR":                 "(t.tenant_id = @tenant_id OR TRUE)",
		"OR at the top level":               "t.tenant_id = @tenant_id OR 1 = 1",
		"a literal, not the caller":         "t.tenant_id = '00000000-0000-0000-0000-000000000000'",
		"another column, not the parameter": "t.tenant_id = l.tenant_id",
	}
	for why, where := range unscoped {
		if scopedByTenant(where, "tenant_id") {
			t.Errorf("the scope check ACCEPTED %q (%s), which scopes nothing to the "+
				"caller's tenant", where, why)
		}
	}

	// And the spellings that ARE the same predicate must all be accepted -- rejecting
	// them made this a syntax detector rather than a property.
	scoped := map[string]string{
		"plain":                  "t.tenant_id = @tenant_id",
		"reversed operands":      "@tenant_id = t.tenant_id",
		"cast":                   "t.tenant_id = @tenant_id::uuid",
		"no alias":               "tenant_id = @tenant_id",
		"conjoined with filters": "t.tenant_id = @tenant_id AND (a IS NULL OR b = c)",
		"second conjunct":        "occurred_at >= @from_at AND t.tenant_id = @tenant_id",
		"parenthesised":          "(t.tenant_id = @tenant_id) AND x = 1",
		"alias with a digit":     "e2.tenant_id = @tenant_id AND y IS NOT NULL",
		"CTE shape":              "e2.tenant_id = @tenant_id\n  AND a IS NOT NULL\n  AND b ILIKE '%x%'",
	}
	for why, where := range scoped {
		if !scopedByTenant(where, "tenant_id") {
			t.Errorf("the scope check REJECTED %q (%s), which IS correctly scoped -- a "+
				"check that only accepts one spelling is a change detector", where, why)
		}
	}

	// The `tenants` table's own column.
	if !scopedByTenant("id = @tenant_id", "id") {
		t.Error("the scope check rejects the tenants table's own scope column")
	}
	if scopedByTenant("id = @tenant_id", "tenant_id") {
		t.Error("the scope check accepts the wrong column")
	}
}

// TestQueryBody_IsBounded is the control for the READER, and it is the one whose
// absence let a blinded queryBody pass.
//
// If queryBody returned more than the query asked for -- the whole file, say -- then
// a tenant predicate ANYWHERE in db/queries would satisfy the belt test for EVERY
// query in it, including one with no predicate of its own.
func TestQueryBody_IsBounded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "probe.sql")
	if err := os.WriteFile(path, []byte(
		"-- name: FirstQuery :one\n"+
			"SELECT 1 FROM a WHERE x = 1;\n"+
			"\n"+
			"-- name: SecondQuery :many\n"+
			"SELECT 2 FROM b WHERE tenant_id = @tenant_id;\n"), 0o600); err != nil {
		t.Fatalf("writing the probe: %v", err)
	}

	first, ok := queryBody(t, path, "FirstQuery")
	if !ok {
		t.Fatal("queryBody could not find FirstQuery in the probe file")
	}
	if strings.Contains(first, "SecondQuery") || strings.Contains(first, "FROM b") {
		t.Errorf("queryBody returned more than the query asked for:\n%s\n"+
			"A reader that overruns into the next query lets ONE tenant predicate "+
			"anywhere in a file satisfy the belt test for every query in it.", first)
	}
	if !strings.Contains(first, "FROM a") {
		t.Errorf("queryBody did not return FirstQuery's own body:\n%s", first)
	}
	// And the belt check, applied through the real reader, must disagree about the
	// two -- which is the end-to-end proof that reader and matcher compose.
	if b := whereBlocks(first); len(b) > 0 && scopedByTenant(b[0].where, "tenant_id") {
		t.Error("FirstQuery has no tenant predicate, but the check says it does -- the " +
			"reader is leaking the neighbouring query into it")
	}
	second, _ := queryBody(t, path, "SecondQuery")
	if b := whereBlocks(second); len(b) == 0 || !scopedByTenant(b[0].where, "tenant_id") {
		t.Error("SecondQuery IS scoped and the check says it is not -- the pair is " +
			"reading nothing at all")
	}

	// 🔴 AND A MARKER INSIDE A COMMENT MUST NOT RESOLVE. This is the shape the bounded
	// check above could not see: it tested overrun into the NEXT query, not the reader
	// being sent to the WRONG one. An audit used exactly this to make an unscoped
	// query pass -- one comment line in an alphabetically earlier file, and the belt
	// went green with a tenant predicate deleted from production SQL.
	hijack := filepath.Join(dir, "aaa_earlier.sql")
	if err := os.WriteFile(hijack, []byte(
		"-- name: SomethingElse :one\n"+
			"-- cross-reference: the panel's queue read is -- name: SecondQuery :many\n"+
			"SELECT 3 FROM d WHERE x = 1;\n"), 0o600); err != nil {
		t.Fatalf("writing the hijack probe: %v", err)
	}
	if _, found := queryBody(t, hijack, "SecondQuery"); found {
		t.Error("a `-- name:` marker sitting INSIDE a comment resolved to a query body. " +
			"The reader must anchor to the start of a line, exactly as declaredQueries " +
			"does -- two patterns for one fact is how the loose one becomes the reader.")
	}
	// POSITIVE CONTROL for the anchor: the same name AT the start of a line is found.
	real := filepath.Join(dir, "zzz_real.sql")
	if err := os.WriteFile(real, []byte(
		"-- name: SecondQuery :many\nSELECT 2 FROM c WHERE tenant_id = @tenant_id;\n"), 0o600); err != nil {
		t.Fatalf("writing the anchor control: %v", err)
	}
	if _, found := queryBody(t, real, "SecondQuery"); !found {
		t.Error("the anchored reader cannot find a genuine header; it is now blind " +
			"rather than strict")
	}

}

// declaredQueries reads every `-- name: X` in db/queries. It is what turns the wide
// method-call scan above into a precise one: a capitalised method call is only
// treated as a store query if a query by that name EXISTS.
func declaredQueries(t *testing.T) map[string]bool {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "db", "queries")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading db/queries: %v", err)
	}
	names := map[string]bool{}
	re := regexp.MustCompile(`(?m)^-- name: (\w+) `)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
			names[m[1]] = true
		}
	}
	if len(names) < 10 {
		t.Fatalf("found %d query name(s) in db/queries; the product has more, so this "+
			"scan is reading the wrong directory and would filter everything out", len(names))
	}
	return names
}

// storeQueryNames returns every sqlc query this package NAMES, found with go/ast.
//
// 🔴 IT PARSES THE PACKAGE INSTEAD OF GREPPING IT, AND THE REASON IS THAT THE REGEX
// VERSION WAS BEATEN THREE WAYS IN ONE AUDIT. The scan used to be
// `\.([A-Z]\w+)\(` over raw source, with a comment claiming it found "ANY method
// call whose name starts with a capital". Measured, it did not:
//
//	q.
//		Method(ctx, …)      a newline between the dot and the name -- and `gofmt`
//	                        does NOT join those lines, so the evasion survives
//	                        gofmt -l, go vet and go build
//	q.Method (ctx, …)       a space before the parenthesis
//	f := x.Method; f(ctx)   the method taken as a value and called later
//
// The end-to-end consequence was measured, not argued: with the newline form, the
// ENTIRE `t.tenant_id = @tenant_id` predicate was deleted from ListFlaggedForReview
// and both belt tests stayed green. The sentence this file is built around — "it
// makes the SECOND defence non-deletable without a red test" — was false in a
// spelling the toolchain is perfectly happy with.
//
// An AST does not see whitespace. Every one of those three shapes is an
// *ast.SelectorExpr whose Sel is the method name, so all three are collected here by
// walking selectors rather than call expressions: taking a method as a VALUE is a
// selector too, which is what closes the third shape.
//
// 🔴 WHAT STILL ESCAPES IT — MEASURED, NOT SUPPOSED, AND DELIBERATELY LEFT OPEN:
//
//	reflect.ValueOf(q).MethodByName("List"+"Flagged…").Call(…)
//	    the name is a STRING, so there is no selector to find. An audit BUILT this
//	    and it was reproduced here: gofmt -l empty, go vet clean, go build clean,
//	    and with `t.tenant_id = @tenant_id` deleted from ListFlaggedForReview BOTH
//	    belt packages answer ok and the queue keeps working end to end. An earlier
//	    version of this comment called it exotic and said no compiling example could
//	    be made; that was wrong, and it was wrong in the direction that flatters the
//	    net.
//	a helper in ANOTHER package making the call
//	    not in this package's AST at all — the coverage limit recorded below.
//
// NEITHER IS CLOSED, ON PURPOSE. This task is eight rounds deep and every net it
// has written was beaten in the next round; past that point the honest move is to
// COUNT a channel rather than close it, because a counted hole is safer than one
// somebody believes is shut. Closing the first would mean forbidding reflection in
// this package (a lint rule nobody enforces) or type-checking the call graph; both
// are tasks, not lines.
func storeQueryNames(t *testing.T, declared map[string]bool) []string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing the package: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("go/ast parsed no non-test package here; the derivation is reading nothing")
	}
	seen := map[string]bool{}
	var out []string
	files := 0
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			files++
			ast.Inspect(f, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				name := sel.Sel.Name
				if declared[name] && !seen[name] {
					seen[name] = true
					out = append(out, name)
				}
				return true
			})
		}
	}
	if files == 0 {
		t.Fatal("no non-test Go file in this package -- the derivation is reading nothing")
	}
	return out
}
