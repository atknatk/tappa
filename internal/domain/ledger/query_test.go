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
// RLS is what actually stops the rows. It makes the SECOND defence non-deletable
// without a red test, which is the entire point of having two.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ledgerQueryCall finds the store calls this package makes: `q.SomeQuery(`.
var ledgerQueryCall = regexp.MustCompile(`\bq\.([A-Z]\w+)\(`)

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
	// 🔴 EVERY NON-TEST FILE IN THE PACKAGE, NOT JUST ledger.go. This read
	// os.ReadFile("ledger.go") while the comment above claimed it derived from
	// "this package's own source" -- so a store call in ANY second file was
	// invisible. An audit proved it with a positive control: the same unscoped
	// query (q.LockEmployeeForTap, which has no WHERE at all) went RED inside
	// ledger.go and GREEN from a sibling file in the same package.
	//
	// IT IS NOT HYPOTHETICAL. Four tasks depend on M6-03 (M6-04, M6-05, M6-06,
	// M6-07) and every one of them adds reads; the first person to split this
	// package would have silently left their queries unchecked.
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("globbing the package: %v", err)
	}
	seen := map[string]bool{}
	var out []string
	scanned := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue // a test may name a query on purpose; the product may not
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		scanned++
		for _, m := range ledgerQueryCall.FindAllStringSubmatch(string(raw), -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				out = append(out, m[1])
			}
		}
	}
	if scanned == 0 {
		t.Fatal("no non-test Go file in this package -- the derivation is reading nothing")
	}
	return out
}

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

// queryBody returns the SQL of one named sqlc query, without its comment lines.
func queryBody(t *testing.T, path, name string) (string, bool) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	marker := "-- name: " + name + " "
	i := strings.Index(string(raw), marker)
	if i < 0 {
		return "", false
	}
	rest := string(raw)[i+len(marker):]
	if j := strings.Index(rest, "-- name: "); j >= 0 {
		rest = rest[:j]
	}
	// Comment lines are dropped so a tenant_id mentioned only in PROSE cannot
	// satisfy the check -- the same trap skill tappa-brand records for Tailwind,
	// in a different file format.
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
}
