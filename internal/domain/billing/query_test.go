package billing

// query_test.go -- §4.5's BELT, asserted separately from its BRACES.
//
// 🔴 WHY THIS FILE EXISTS AT ALL is measured in internal/domain/ledger's copy and not
// re-derived here: a behavioural isolation test can only ever observe the CONJUNCTION
// of RLS and the explicit predicate, so deleting the predicate leaves every such test
// green. billing_db_test.go's TestBillingDB_PeriodsAreTenantIsolated is exactly that
// kind of test -- deliberately, because CLAUDE.md §6 says an isolation test must NOT
// carry the filter -- and it therefore cannot see the belt at all. This file is the
// only place the second defence is visible.
//
// 🔴 IT IS THE FOURTH COPY OF ONE NET, AND IT IS THE UNION OF THE THREE RATHER THAN A
// COPY OF ANY ONE OF THEM. The three that exist diverge on two tokens, and backlog
// T24 is the open item:
//
//	internal/domain/ledger  HAS the aggregate-FILTER narrowing, has NO `RETURNING` stop
//	internal/domain/review  lacks the FILTER narrowing, HAS the `RETURNING` stop
//	internal/domain/tenant  lacks the FILTER narrowing, HAS the `RETURNING` stop
//
// Copying either side would have imported a known defect, so this copy takes the
// FILTER narrowing (T24's fix, from ledger) AND the `RETURNING` stop (from the other
// two). Both were measured against THIS package's queries before being kept:
//
//	FILTER      `rg -c 'FILTER \(' db/queries/billing.sql` -> 1, and that one
//	            occurrence is on a COMMENT line in the file header. queryBody strips
//	            `--` lines, so the token never reaches the scanner and the narrowing
//	            is INERT here today. It is kept because the reproduction command in
//	            that header is exactly the kind of prose that migrates into a query,
//	            and because inheriting the fixed copy costs nothing.
//	RETURNING   CloseBillingPeriod ends in one. Without the stop, its WHERE clause
//	            reads as `... AND b.month >= b.signup_month` plus the whole RETURNING
//	            list; ledger's copy records that this is fail-closed rather than
//	            dangerous, but the stop is free and the sibling copies already have it.
//
// WHAT THIS BUYS, STATED HONESTLY: it does not make the product safer on its own --
// RLS is what actually stops the rows. What it adds is that deleting the second
// defence from a query THIS SCAN CAN SEE turns a test red.
//
// 🔴 WHAT IT DOES NOT HOLD. The escapes are the ones ledger's copy measured and named
// (a UNION branch, a top-level JOIN ... ON, `FROM public.x`, `FROM ONLY x`, a comma
// join, a query named through reflection, a call made from another package). They are
// NOT re-measured here and they are NOT closed here; they are inherited with the
// mechanism, and ledger's copy is the record. Adding a fourth copy that claims more
// than the original would be worse than the divergence it is trying to end.
//
// ⚠️ AND ONE LIMIT THAT IS THIS PACKAGE'S OWN: the belt lives in the CTEs rather than
// on the outer statement, because sqlc v1.28's analyser refuses a qualified CTE
// reference in an outer WHERE ("table alias c does not exist") and calls an
// unqualified one ambiguous. Both were measured while writing db/queries/billing.sql.
// The scanner reads CTE WHERE clauses, so this costs it nothing -- but it is why the
// predicate is not where a reader of the INSERT would look first.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// tenantParam is the parameter every scoped query binds its scope column to.
const tenantParam = "@tenant_id"

// TestBillingQueries_CarryAnExplicitTenantPredicate is the belt.
//
// THE QUERIES ARE DERIVED, NOT LISTED. A hand-written list is a change detector: a
// new billing read with no tenant predicate would simply not be in it, and the
// natural repair -- adding the newcomer to the list -- is exactly the wrong move.
func TestBillingQueries_CarryAnExplicitTenantPredicate(t *testing.T) {
	declared := declaredQueries(t)
	names := storeQueryNames(t, declared)
	// ANTI-VACUITY: an empty derivation would make every assertion below pass over
	// nothing. This package makes four store calls (preview, close, get, list); the
	// floor is set at four rather than at "today's number plus room", because a floor
	// that tracks the present goes red on correct deletions.
	if len(names) < 4 {
		t.Fatalf("derived %d store call(s) from this package's non-test files (%v); "+
			"billing.go makes four, so the scan is reading the wrong directory",
			len(names), names)
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	// ⚠️ THE COVERAGE IS PRINTED, NOT ASSERTED, and that is a gap rather than a
	// design -- the same one ledger's copy records. Nothing goes red if this package
	// stops calling a query; the only brake is the floor above.
	t.Logf("§4.5 belt coverage from THIS package: %d of %d queries declared in "+
		"db/queries (%.1f%%). The other internal/domain packages carry their own "+
		"copies over their own calls.\nseen here: %s",
		len(names), len(declared), 100*float64(len(names))/float64(len(declared)),
		strings.Join(sorted, ", "))

	for _, name := range names {
		file, body, ok := findQuery(t, name)
		if !ok {
			t.Errorf("no query named %q anywhere in db/queries -- this package calls it, "+
				"so either it was renamed or this scan cannot see it", name)
			continue
		}
		// A subquery that reads a tenant table must have a WHERE AT ALL, checked
		// before the predicate check because a block with no WHERE is invisible to it.
		for _, sub := range unscopedSubqueries(t, body) {
			t.Errorf("%s (%s) contains a subquery that reads the tenant-scoped table %q "+
				"and has NO WHERE clause at all, so nothing in it is scoped to the "+
				"caller's tenant.\n\nsubquery read:\n%s", name, file, sub.table, sub.text)
		}
		blocks := whereBlocks(body)
		if len(blocks) == 0 {
			t.Errorf("%s (%s) has no WHERE clause at all, so it is scoped by RLS alone.\n"+
				"§4.5 asks for belt AND braces.", name, file)
			continue
		}
		for _, blk := range blocks {
			if scopedByTenant(blk.where, blk.column) {
				continue
			}
			t.Errorf("%s (%s) has a WHERE clause that does not bind %s to %s as a "+
				"TOP-LEVEL CONJUNCT.\n"+
				"§4.5 asks for belt AND braces: RLS scopes the transaction, and the query "+
				"says the same thing again. Without it, TestBillingDB_PeriodsAreTenantIsolated "+
				"still passes -- RLS alone blocks the rows -- so this is the only place the "+
				"second defence is visible.\n\n"+
				"A predicate inside an OR, or moved into a JOIN ... ON, does NOT count. A "+
				"CTE or subquery reading a tenant table needs its OWN predicate.\n\n"+
				"WHERE clause read:\n%s", name, file, blk.column, tenantParam, blk.where)
		}
	}
}

// TestBillingQueries_TheScopeCheckIsNotVacuous is the negative control for the
// MATCHER: the shapes that must NOT pass, and the shapes that must.
//
// 🔴 WITHOUT IT, A MATCHER THAT ACCEPTED EVERYTHING WOULD LOOK IDENTICAL to one that
// works, on green output alone.
func TestBillingQueries_TheScopeCheckIsNotVacuous(t *testing.T) {
	cases := []struct {
		name   string
		where  string
		column string
		want   bool
	}{
		{"the ordinary form", "b.tenant_id = @tenant_id AND b.to_at <= now()", "tenant_id", true},
		{"reversed, semantically identical", "@tenant_id = b.tenant_id", "tenant_id", true},
		{"unqualified", "tenant_id = @tenant_id", "tenant_id", true},
		{"an alias with a digit", "e2.tenant_id = @tenant_id", "tenant_id", true},
		{"the tenants table scopes on id", "t.id = @tenant_id", "id", true},
		{"a cast does not hide it", "b.tenant_id = @tenant_id::uuid", "tenant_id", true},
		// The refusals, each a real way to look scoped without being scoped.
		{"neutralised by OR", "(b.tenant_id = @tenant_id OR TRUE)", "tenant_id", false},
		{"OR at top level", "b.tenant_id = @tenant_id OR b.id IS NOT NULL", "tenant_id", false},
		{"a different parameter", "b.tenant_id = @other_id", "tenant_id", false},
		{"a different column", "b.location_id = @tenant_id", "tenant_id", false},
		{"scoped to a literal rather than the caller", "b.tenant_id = '00000000-0000-0000-0000-000000000000'", "tenant_id", false},
		{"no predicate at all", "b.to_at <= now()", "tenant_id", false},
		{"the tenants column against the wrong scope column", "t.id = @tenant_id", "tenant_id", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := scopedByTenant(c.where, c.column); got != c.want {
				t.Fatalf("scopedByTenant(%q, %q) = %v, want %v", c.where, c.column, got, c.want)
			}
		})
	}
}

// TestWhereBlocks_SkipsAggregateFilterAndStillSeesTheRealClause pins the narrowing in
// BOTH directions -- the property ledger's copy names and the two sibling copies lack.
//
// An aggregate's FILTER (WHERE ...) chooses which rows of an ALREADY-SCOPED result an
// aggregate sees. It scopes nothing and it can scope nothing, so demanding a tenant
// predicate inside it is demanding one in the wrong place. Skipping it must not,
// however, make a query whose ONLY tenant predicate is inside a FILTER acceptable.
func TestWhereBlocks_SkipsAggregateFilterAndStillSeesTheRealClause(t *testing.T) {
	// (a) The real WHERE is still read when a FILTER sits beside it.
	good := `SELECT count(*) FILTER (WHERE practice) AS n
	         FROM transactions t
	         WHERE t.tenant_id = @tenant_id`
	blocks := whereBlocks(good)
	if len(blocks) != 1 {
		t.Fatalf("whereBlocks read %d block(s) from a query with one real WHERE and one "+
			"aggregate FILTER, want 1: %#v", len(blocks), blocks)
	}
	if !scopedByTenant(blocks[0].where, blocks[0].column) {
		t.Fatalf("the real WHERE was not recognised as scoped: %q", blocks[0].where)
	}

	// (b) A predicate that lives ONLY inside a FILTER does not scope the query.
	bad := `SELECT count(*) FILTER (WHERE t.tenant_id = @tenant_id) AS n
	        FROM transactions t
	        WHERE t.occurred_at > now()`
	for _, blk := range whereBlocks(bad) {
		if scopedByTenant(blk.where, blk.column) {
			t.Fatalf("a tenant predicate inside an aggregate FILTER was accepted as the "+
				"query's scope: %q", blk.where)
		}
	}
}

// TestQueryBody_IsBounded is the negative control for the READER rather than the
// matcher: a body that ran past its own query would let a NEIGHBOUR's predicate
// satisfy the check.
func TestQueryBody_IsBounded(t *testing.T) {
	_, body, ok := findQuery(t, "PreviewBillingPeriod")
	if !ok {
		t.Fatal("PreviewBillingPeriod not found; the reader is looking in the wrong place")
	}
	// It must contain its own text ...
	if !strings.Contains(body, "tappa_first_chargeable_month") {
		t.Fatal("the body of PreviewBillingPeriod does not contain its own SQL")
	}
	// ... and NOT the next query's.
	if strings.Contains(strings.ToUpper(body), "INSERT INTO BILLING_PERIODS") {
		t.Fatal("the body of PreviewBillingPeriod runs into CloseBillingPeriod; a " +
			"neighbour's predicate could satisfy the scope check")
	}
	// ... and no comment line, so a tenant_id mentioned only in prose cannot scope
	// anything. The file header carries `WHERE tenant_id = '...'` inside a comment,
	// which is exactly the shape this guards against.
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			t.Fatalf("queryBody returned a comment line, so prose can satisfy the scope "+
				"check: %q", line)
		}
	}
}

// ---------------------------------------------------------------------------
// The mechanism. Copied from internal/domain/ledger/query_test.go, whose comments
// carry the measurements behind each piece; only what differs is re-commented here.
// ---------------------------------------------------------------------------

type subqueryRead struct {
	table string
	text  string
}

// unscopedSubqueries finds parenthesised blocks that read a tenant-scoped table and
// carry NO WHERE clause at all -- a block that whereBlocks cannot see, because it
// contributes no block.
func unscopedSubqueries(t *testing.T, body string) []subqueryRead {
	t.Helper()
	tables := tenantScopedTables(t)
	var out []subqueryRead
	for _, span := range parenSpans(body) {
		own := maskNested(span)
		if strings.Contains(strings.ToUpper(own), "WHERE") {
			continue
		}
		for _, m := range fromOrJoinRE.FindAllStringSubmatch(own, -1) {
			if tables[strings.ToLower(m[1])] {
				out = append(out, subqueryRead{table: strings.ToLower(m[1]), text: strings.TrimSpace(span)})
				break
			}
		}
	}
	return out
}

// maskNested blanks the contents of every nested parenthesis with SPACES, so a
// nested subquery's WHERE cannot satisfy its parent. Spaces rather than deletion:
// removing text can join two tokens and invent a keyword that is not there.
func maskNested(s string) string {
	out := []byte(s)
	depth := 0
	for i := 0; i < len(out); i++ {
		switch out[i] {
		case '(':
			depth++
			continue
		case ')':
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth > 0 {
			out[i] = ' '
		}
	}
	return string(out)
}

var fromOrJoinRE = regexp.MustCompile(`(?i)\b(?:FROM|JOIN)\s+(?:LATERAL\s+)?([a-z_][a-z0-9_]*)`)

func parenSpans(s string) []string {
	var out []string
	var stack []int
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			stack = append(stack, i)
		case ')':
			if n := len(stack); n > 0 {
				out = append(out, s[stack[n-1]+1:i])
				stack = stack[:n-1]
			}
		}
	}
	return out
}

// tenantScopedTables DERIVES which tables carry a tenant policy by reading the
// migrations. Every tenant-scoped table gets a CREATE POLICY (CLAUDE.md §6), so the
// policies ARE the list -- including billing_periods, which is how this package's own
// table joined the set without anybody editing a list.
func tenantScopedTables(t *testing.T) map[string]bool {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "db", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading db/migrations: %v", err)
	}
	re := regexp.MustCompile(`(?i)CREATE\s+POLICY\s+\w+\s+ON\s+([a-z_][a-z0-9_]*)`)
	out := map[string]bool{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
			out[strings.ToLower(m[1])] = true
		}
	}
	if len(out) < 10 {
		t.Fatalf("derived %d tenant-scoped table(s) from db/migrations; the schema has "+
			"more, so this scan is reading the wrong directory", len(out))
	}
	return out
}

// scopeColumnFor names the scope column of the table a WHERE clause filters. It is
// `tenant_id` everywhere except `tenants`, whose own scope key is its primary key
// (ADR 0002 madde 5) -- which this package's scope CTE relies on.
func scopeColumnFor(before string) string {
	froms := regexp.MustCompile(`(?i)\bFROM\s+([a-z_]+)`).FindAllStringSubmatch(before, -1)
	if len(froms) > 0 && strings.EqualFold(froms[len(froms)-1][1], "tenants") {
		return "id"
	}
	return "tenant_id"
}

type whereBlock struct {
	where  string
	column string
}

// whereBlocks returns EVERY top-level WHERE clause, each paired with the scope column
// of its own table. Every one is checked: this package's belt lives in CTEs, so
// reading only the first would check the wrong clause and reading only the last would
// miss the scope CTE entirely.
//
// JOIN ... ON is structurally excluded: a condition there constrains the join, not
// the rows returned. GetBillingPeriod and ListBillingPeriods both join `tenants` for
// the addressee's name, and that join is NOT what scopes them.
func whereBlocks(body string) []whereBlock {
	var out []whereBlock
	whereRE := regexp.MustCompile(`(?is)\bWHERE\b`)
	// The lookbehind for an aggregate FILTER. Go's regexp has no lookbehind, so the
	// preceding text is matched with its own anchored pattern instead.
	filterRE := regexp.MustCompile(`(?is)\bFILTER\s*\(\s*$`)
	// RETURNING is in the stop list here (as in review's and tenant's copies, and
	// unlike ledger's): CloseBillingPeriod ends in one, and without the stop its
	// RETURNING list is read as part of its WHERE clause.
	stopRE := regexp.MustCompile(`(?is)\b(ORDER\s+BY|GROUP\s+BY|LIMIT|HAVING|RETURNING|FOR\s+NO\s+KEY\s+UPDATE|FOR\s+UPDATE|FOR\s+SHARE)\b`)
	for _, loc := range whereRE.FindAllStringIndex(body, -1) {
		if filterRE.MatchString(body[:loc[0]]) {
			continue
		}
		rest := body[loc[1]:]
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

// scopedByTenant reports whether one TOP-LEVEL conjunct is an equality between the
// scope column and the tenant parameter. Top-level is the whole point:
// `(a = @tenant_id OR TRUE)` scopes nothing.
func scopedByTenant(where, column string) bool {
	for _, conjunct := range splitTopLevelAnd(where) {
		if isScopeEquality(conjunct, column) {
			return true
		}
	}
	return false
}

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
	if i > 0 && isWordByte(s[i-1]) {
		return false
	}
	return i+n >= len(s) || !isWordByte(s[i+n])
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isScopeEquality(conjunct, column string) bool {
	c := strings.TrimSpace(conjunct)
	c = regexp.MustCompile(`::\s*[a-zA-Z_]+`).ReplaceAllString(c, "")
	c = strings.Trim(c, "() \t\n")
	c = strings.Join(strings.Fields(c), " ")

	col := `(?:[a-z_][a-z0-9_]*\.)?` + regexp.QuoteMeta(column)
	param := regexp.QuoteMeta(tenantParam)
	forward := regexp.MustCompile(`(?i)^` + col + ` = ` + param + `$`)
	reverse := regexp.MustCompile(`(?i)^` + param + ` = ` + col + `$`)
	return forward.MatchString(c) || reverse.MatchString(c)
}

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

// queryMarker matches a `-- name: X ` header ANCHORED TO THE START OF A LINE. The
// anchor is not cosmetic: unanchored, a `-- name: X` written inside a COMMENT in an
// alphabetically earlier file resolves first and the "body" returned is the next
// query's. Measured end to end in ledger's copy.
func queryMarker(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^-- name: ` + regexp.QuoteMeta(name) + ` `)
}

// queryBody returns the SQL of one named query WITHOUT its comment lines, so a
// tenant_id mentioned only in prose cannot satisfy the check. This file's own header
// depends on that: db/queries/billing.sql carries both `FILTER (WHERE ...)` and
// `WHERE tenant_id = '...'` inside comments.
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
			"scan is reading the wrong directory", len(names))
	}
	return names
}

// storeQueryNames returns every sqlc query this package NAMES, found with go/ast.
//
// AN AST RATHER THAN A REGEX, because three spellings beat the regex version in
// ledger's copy: a newline between the dot and the method name (which gofmt does NOT
// join), a space before the parenthesis, and taking the method as a VALUE. All three
// are *ast.SelectorExpr, so walking selectors -- not call expressions -- catches
// them.
//
// WHAT STILL ESCAPES: a query named through reflection (there is no selector to
// find), and a call made from another package. Both are inherited limits, measured
// and recorded in ledger's copy, and neither is closed here.
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
