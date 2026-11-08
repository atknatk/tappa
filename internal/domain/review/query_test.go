package review

// query_test.go -- §4.5's BELT for this package's one write, asserted separately
// from its BRACES.
//
// 🔴 WHY THIS EXISTS BESIDE internal/domain/ledger/query_test.go INSTEAD OF REUSING
// IT, stated as a cost rather than dressed up. That file derives its query list by
// scanning ITS OWN package for `q.X(` calls and checks each one's WHERE clauses; it
// is the better machinery and it is unexported, so a second package cannot call it.
// Three options were weighed:
//
//	widen it to every internal/domain package   REJECTED, measured: it turns RED on
//	                                            code that is correct today --
//	                                            LockEmployeeForTap is an advisory
//	                                            lock with no WHERE clause at all,
//	                                            and internal/domain/checkin calls it.
//	move the write into ledger                  REJECTED: ledger's first paragraph
//	                                            claims no store call there is not a
//	                                            SELECT, and that is a fact grep can
//	                                            check. Trading it for a shared test
//	                                            helper is a bad exchange.
//	a small check here                          TAKEN, and this is it.
//
// ⚠️ SO THE DUPLICATION IS REAL AND IS THE PRICE. What is duplicated is the IDEA,
// not the code: this checks ONE query against the same property (a top-level
// equality between the scope column and the caller's tenant parameter, in every
// WHERE clause the query has) with about a tenth of the machinery, because there is
// one query rather than a growing set. If this package ever holds several, the
// right move is to lift ledger's derivation into a shared place and delete this.
//
// WHAT IT BUYS, HONESTLY: it does not make the product safer on its own -- RLS and
// the composite FK are what stop a cross-tenant review. What it adds is that
// deleting the second defence from a query THIS SCAN CAN SEE turns a test red.
//
// 🔴 IT SAID "non-deletable without a red test" AND THAT IS MEASURED FALSE, TODAY --
// not merely historically. Two ways through it are recorded below and in
// internal/domain/ledger/query_test.go: a query named only as a STRING through
// reflect.MethodByName (an audit built one that compiles, is gofmt- and vet-clean,
// and leaves both belts green with a tenant predicate deleted), and a query called
// from a package this scan does not read. Neither is closed. The headline is
// narrowed rather than deleted because the property it does hold is still worth
// having; what it may not do is claim to be absolute.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/sqlbelt"
)

const tenantParam = "@tenant_id"

// packageQueries DERIVES the queries to check from this package's own source, for
// the reason ledger's version records: a hand-written list is a change detector,
// and the natural repair when it goes red (add the newcomer to the list) is exactly
// the wrong move.
func packageQueries(t *testing.T) []string {
	t.Helper()
	return storeQueryNames(t, declaredQueries(t))
}

// TestReviewQueries_CarryAnExplicitTenantPredicate is the belt.
func TestReviewQueries_CarryAnExplicitTenantPredicate(t *testing.T) {
	names := packageQueries(t)
	// ANTI-VACUITY: with no derived query every assertion below passes over nothing.
	if len(names) == 0 {
		t.Fatal("derived no store call from this package's source. It writes the review " +
			"row through sqlc, so either the write moved or this scan is reading the " +
			"wrong files -- and every check below would pass vacuously.")
	}

	for _, name := range names {
		file, body, ok := findQuery(t, name)
		if !ok {
			t.Errorf("no query named %q anywhere in db/queries -- this package calls it", name)
			continue
		}
		for _, f := range beltFindings(t, body) {
			t.Errorf("%s (%s) %s", name, file, f)
		}
	}
}

// beltFindings is the per-statement check, as one function so TestBelt_SharedLexicalCorpus
// drives exactly what the belt drives.
//
// 🔴 A BODY THE LEXER CANNOT READ IS UNSCOPED, AND EVERYTHING BELOW READS THE MASKED BODY
// (T164): internal/sqlbelt's Prepare refuses an E-string, a dollar quote, a block comment and a
// quoted identifier holding anything but a name, and blanks plain literals and line comments, so
// a paren, an OR or a WHERE the database reads as data cannot move the structure read here.
func beltFindings(t *testing.T, body string) []string {
	t.Helper()
	prepared, err := sqlbelt.Prepare(body)
	if err != nil {
		return []string{err.Error()}
	}
	body = prepared
	out := insertSelectWrites(body)
	// A SUBQUERY THAT READS A TENANT TABLE MUST HAVE A WHERE AT ALL (third-eye audit of 86689718:
	// this belt passed `(SELECT count(*) FROM sessions)` in a select list and `EXISTS (SELECT 1 FROM
	// sessions s)` in a WHERE, before and after T164 -- a block with no WHERE is invisible to the
	// clause walk below). internal/sqlbelt's WherelessSubqueries, the scan ledger's copy carried.
	for _, sub := range sqlbelt.WherelessSubqueries(body, tenantScopedTables(t)) {
		out = append(out, fmt.Sprintf("contains a subquery that reads the tenant-scoped table %q "+
			"and has NO WHERE clause at all, so nothing in it is scoped to the caller's tenant.\n\n"+
			"subquery read:\n%s", sub.Table, sub.Text))
	}
	clauses := whereClauses(body)
	if len(clauses) == 0 {
		return append(out, "has no WHERE clause at all, so it is scoped by RLS alone.\n"+
			"§4.5 asks for belt AND braces.")
	}
	for _, c := range clauses {
		if scopedByTenant(c) {
			continue
		}
		out = append(out, fmt.Sprintf("has a WHERE clause that does not bind tenant_id to %s as a "+
			"TOP-LEVEL CONJUNCT.\n"+
			"A predicate inside an OR does not count. Without it, a behavioural "+
			"isolation test still passes -- RLS alone blocks the rows -- so this is "+
			"the only place the second defence is visible.\n\nWHERE clause read:\n%s",
			tenantParam, c))
	}
	return out
}

// TestBelt_SharedLexicalCorpus runs internal/sqlbelt's one corpus through THIS belt; the five
// sibling belts run the same corpus through theirs, so the six cannot drift apart on a lexical
// form without one of them going red (T164). Planted: every statement scopes nothing and must be
// flagged. Clean: every statement is scoped and must pass -- including an aggregate FILTER with
// an OR beside a scoped WHERE, which this copy flagged before T24 carried the narrowing here.
func TestBelt_SharedLexicalCorpus(t *testing.T) {
	for _, c := range sqlbelt.Planted {
		if f := beltFindings(t, c.SQL); len(f) == 0 {
			t.Errorf("planted %q passed this belt (closed by: %s):\n%s", c.Name, c.Closes, c.SQL)
		}
	}
	for _, c := range sqlbelt.Clean {
		if f := beltFindings(t, c.SQL); len(f) != 0 {
			t.Errorf("clean %q was flagged:\n%s\n%s", c.Name, c.SQL, strings.Join(f, "\n"))
		}
	}
}

// whereClauses returns every top-level WHERE clause in the query text: internal/sqlbelt's
// WhereClauses (since T164/T24), which blanks literals, skips an aggregate's FILTER (WHERE ...)
// -- a row filter over an already-scoped result, not a scope -- and ends each clause at its
// block's closing paren or at a clause that may follow WHERE (ORDER BY, GROUP BY, LIMIT, HAVING,
// RETURNING, FOR UPDATE/SHARE, ON CONFLICT).
func whereClauses(body string) []string {
	var out []string
	for _, c := range sqlbelt.WhereClauses(body) {
		out = append(out, c.Text)
	}
	return out
}

// scopedByTenant reports whether one TOP-LEVEL conjunct is an equality between
// tenant_id and the caller's parameter. Top-level is the point: `a = @tenant_id AND
// b` scopes the query and `(a = @tenant_id OR TRUE)` does not.
func scopedByTenant(where string) bool {
	if sqlbelt.HasTopLevelOr(where) {
		return false
	}
	for _, conjunct := range sqlbelt.SplitTopLevelAnd(where) {
		if isScopeEquality(conjunct) {
			return true
		}
	}
	return false
}

// isScopeEquality accepts either operand order, an optional alias and any cast --
// all of which are the SAME predicate. Rejecting a spelling would make this a
// syntax detector rather than a property.
func isScopeEquality(conjunct string) bool {
	c := strings.TrimSpace(conjunct)
	c = regexp.MustCompile(`::\s*[a-zA-Z_]+`).ReplaceAllString(c, "")
	c = strings.Trim(c, "() \t\n")
	c = strings.Join(strings.Fields(c), " ")

	col := `(?:[a-z_][a-z0-9_]*\.)?tenant_id`
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

// TestReviewScopeCheck_IsNotVacuous is the negative control for the MATCHER, and
// TestReviewQueryBody_IsBounded below is the one for the READER. ledger's version
// of this pair exists because an audit found only the first, and blinding the
// reader to return the whole file left the belt test green.
func TestReviewScopeCheck_IsNotVacuous(t *testing.T) {
	unscoped := map[string]string{
		"no predicate at all":               "t.id = @transaction_id",
		"neutralised by OR":                 "(t.tenant_id = @tenant_id OR TRUE)",
		"OR at the top level":               "t.tenant_id = @tenant_id OR 1 = 1",
		"OR after the scope conjunct":       "t.tenant_id = @tenant_id AND x OR true",
		"OR a caller's switch":              "t.tenant_id = @tenant_id AND x = 1 OR @all::boolean",
		"a literal, not the caller":         "t.tenant_id = '00000000-0000-0000-0000-000000000000'",
		"another column, not the parameter": "t.tenant_id = r.tenant_id",
	}
	for why, where := range unscoped {
		if scopedByTenant(where) {
			t.Errorf("the scope check ACCEPTED %q (%s), which scopes nothing to the "+
				"caller's tenant", where, why)
		}
	}
	scoped := map[string]string{
		"plain":                  "t.tenant_id = @tenant_id",
		"reversed operands":      "@tenant_id = t.tenant_id",
		"cast":                   "t.tenant_id = @tenant_id::uuid",
		"no alias":               "tenant_id = @tenant_id",
		"conjoined with filters": "t.tenant_id = @tenant_id AND t.verdict = 'flag'",
		"second conjunct":        "t.id = @transaction_id AND t.tenant_id = @tenant_id",
	}
	for why, where := range scoped {
		if !scopedByTenant(where) {
			t.Errorf("the scope check REJECTED %q (%s), which IS correctly scoped -- a "+
				"check that only accepts one spelling is a change detector", where, why)
		}
	}
}

// TestReviewQueryBody_IsBounded is the control for the READER: a reader that
// overruns into the next query lets ONE tenant predicate anywhere in a file satisfy
// the belt test for every query in it.
func TestReviewQueryBody_IsBounded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "probe.sql")
	if err := os.WriteFile(path, []byte(
		"-- name: FirstQuery :one\n"+
			"INSERT INTO a SELECT 1 FROM b WHERE x = 1 RETURNING id;\n"+
			"\n"+
			"-- name: SecondQuery :many\n"+
			"SELECT 2 FROM c WHERE tenant_id = @tenant_id;\n"), 0o600); err != nil {
		t.Fatalf("writing the probe: %v", err)
	}

	first, ok := queryBody(t, path, "FirstQuery")
	if !ok {
		t.Fatal("queryBody could not find FirstQuery in the probe file")
	}
	if strings.Contains(first, "SecondQuery") || strings.Contains(first, "FROM c") {
		t.Errorf("queryBody returned more than the query asked for:\n%s", first)
	}
	if c := whereClauses(first); len(c) == 0 || scopedByTenant(c[0]) {
		t.Errorf("FirstQuery has no tenant predicate, but the check says it does -- the " +
			"reader is leaking the neighbouring query into it")
	}
	second, _ := queryBody(t, path, "SecondQuery")
	if c := whereClauses(second); len(c) == 0 || !scopedByTenant(c[0]) {
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

// TestReviewQueryClauses_StopAtRETURNING is the one shape ledger's reader never
// meets and this one does: an INSERT ... SELECT ... WHERE ... RETURNING. Without
// RETURNING in the stop list the clause would swallow the returned column list, and
// a column called tenant_id there would satisfy the belt test for a query with no
// predicate at all.
func TestReviewQueryClauses_StopAtRETURNING(t *testing.T) {
	body := "INSERT INTO r (tenant_id) SELECT @tenant_id FROM t\nWHERE t.id = @id\nRETURNING tenant_id = @tenant_id;"
	clauses := whereClauses(body)
	if len(clauses) != 1 {
		t.Fatalf("read %d WHERE clause(s) from an INSERT ... RETURNING, want 1: %q", len(clauses), clauses)
	}
	if strings.Contains(strings.ToUpper(clauses[0]), "RETURNING") {
		t.Errorf("the WHERE clause swallowed the RETURNING list: %q", clauses[0])
	}
	if scopedByTenant(clauses[0]) {
		t.Errorf("a query whose only tenant_id is in its RETURNING list was accepted as "+
			"scoped: %q", clauses[0])
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
// ⚠️ WHAT STILL ESCAPES IT, tried and named rather than left implied:
//
//	reflect.ValueOf(q).MethodByName("X").Call(…)   the name is a STRING, not a
//	                                               selector -- invisible here
//	a helper in ANOTHER package doing the call     not in this package's AST at all
//
// The first is exotic; the second is the coverage limit already recorded at
// TestPanelQueries_CarryAnExplicitTenantPredicate. Neither is closed.
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

// tenantScopedTables DERIVES the tenant-scoped tables from the migrations' CREATE POLICY lines
// (internal/sqlbelt's TenantScopedTables), floored so an empty derivation cannot make the subquery
// scan vacuous.
func tenantScopedTables(t *testing.T) map[string]bool {
	t.Helper()
	out, err := sqlbelt.TenantScopedTables(filepath.Join("..", "..", "..", "db", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	for _, must := range []string{"employees", "sessions", "admin_users"} {
		if !out[must] {
			t.Fatalf("derived %d tenant-scoped table(s) from db/migrations and %q is not among them; "+
				"the derivation is reading the wrong thing and the subquery scan would pass vacuously",
				len(out), must)
		}
	}
	return out
}
