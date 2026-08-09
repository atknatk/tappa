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
	"sort"
	"strings"
	"testing"
)

// tenantParam is the parameter every scoped query binds its scope column to.
const tenantParam = "@tenant_id"

// THE QUERIES TO CHECK ARE DERIVED, NOT LISTED, and that is the fix for the fifth
// hole. A hand-written list is a change detector: a new panel read with no tenant
// predicate was simply not in it, so it passed silently -- the failure mode where
// the natural repair (add the newcomer to the list) is exactly the wrong move. Every
// store query this package calls is checked because it is CALLED, so adding an
// unscoped one turns the belt red on the next run.
//
// The two halves are declaredQueries (every `-- name:` in db/queries) and
// storeQueryNames (every one of those this package's AST names); the belt composes
// them directly, which is also what lets it print its own coverage.

// 🔴 HOW MUCH OF THE PRODUCT THIS BELT COVERS IS DERIVED AND LOGGED, NOT WRITTEN
// DOWN — AND THAT IS THE THIRD TIME THE HAND-WRITTEN VERSION ROTTED.
//
// This paragraph used to say "8 QUERIES OF 41, MEASURED" and print the command that
// produced the 41. An audit ran that exact command and got 42; the list of 8 was
// also missing ListPanelEmployees, which this net demonstrably DOES see (two
// mutations prove it). So the sentence was wrong in both its numerator and its
// denominator while carrying a command that disproved it — the "assertion label"
// class this session has now paid for nine times.
//
// It is not restated with better numbers, because better numbers rot on the next
// commit that adds a query. Both quantities are already computed at run time —
// declaredQueries() reads every `-- name:` in db/queries, storeQueryNames() collects
// what this package calls — so the coverage is LOGGED by the belt test below and the
// figure of the day comes from:
//
//	go test ./internal/domain/ledger -run TestPanelQueries_CarryAnExplicitTenantPredicate -v
//
// WHAT IS STILL TRUE WITHOUT A NUMBER: this net sees the queries TWO domain packages
// call, and the product has many more. For all the rest — InsertTransaction,
// GetLastOpenTransaction, CreateAdminSession and so on — §4.5's SECOND defence is
// asserted by no test at all. RLS still stops the rows; what is unguarded is the
// explicit predicate beside it.
//
// ⚠️ WIDENING IT WAS MEASURED AND REJECTED, and the reason is recorded in
// internal/domain/review/query_test.go: running this over every internal/domain
// package turns RED on code that is correct today — LockEmployeeForTap is an
// advisory lock with no WHERE clause at all. Closing the gap properly means a
// per-query opt-out with a reason, which is a task rather than a line.
//
// TestPanelQueries_CarryAnExplicitTenantPredicate is the belt.
func TestPanelQueries_CarryAnExplicitTenantPredicate(t *testing.T) {
	declared := declaredQueries(t)
	names := storeQueryNames(t, declared)
	// ANTI-VACUITY: an empty derivation would make every assertion pass over nothing.
	if len(names) < 4 {
		t.Fatalf("derived %d store call(s) from ledger.go (%v); the panel read path "+
			"makes more than that, so the scan is reading the wrong file", len(names), names)
	}
	// ⚠️ THE COVERAGE IS PRINTED, NOT ASSERTED, AND THAT IS A GAP RATHER THAN A
	// DESIGN. If this package stopped calling three of these queries the log would
	// read 5 of 42 and nothing would go red; the only brake is the anti-vacuity
	// floor above, which is 4. A floor at today's figure was considered and rejected:
	// coverage falls legitimately whenever a read is deleted or moved, so it would go
	// red on correct changes, which is the change-detector shape this file has been
	// bitten by twice. What is NOT available is an assertion that says "every query
	// this package calls is checked" -- that is true by construction and therefore
	// proves nothing. So: the number is visible on every run and defended by nobody.
	// Counted.
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	// ⚠️ THE SENTENCE USED TO COUNT THE COPIES ("review has a second copy") AND M6-05
	// PHASE B MADE IT STALE by adding a third in internal/domain/tenant. A count of
	// sibling files is a fact about the calendar, so it is gone: what is true without
	// one is that other domain packages carry their own copies over their own calls.
	t.Logf("§4.5 belt coverage from THIS package: %d of %d queries declared in "+
		"db/queries (%.1f%%). Other internal/domain packages carry their own copies "+
		"over their own calls, so the product-wide figure is higher than this one and "+
		"lower than 100.\n"+
		"seen here: %s",
		len(names), len(declared), 100*float64(len(names))/float64(len(declared)),
		strings.Join(sorted, ", "))

	for _, name := range names {
		file, body, ok := findQuery(t, name)
		if !ok {
			t.Errorf("no query named %q anywhere in db/queries -- ledger.go calls it, so "+
				"either it was renamed or this scan cannot see it", name)
			continue
		}
		// 🔴 A SUBQUERY THAT READS A TENANT TABLE MUST HAVE A WHERE AT ALL, checked
		// BEFORE the predicate check below because a block with no WHERE was invisible
		// to it. See unscopedSubqueries.
		for _, sub := range unscopedSubqueries(t, body) {
			t.Errorf("%s (%s) contains a subquery that reads the tenant-scoped table %q "+
				"and has NO WHERE clause at all, so nothing in it is scoped to the "+
				"caller's tenant.\n"+
				"The predicate check below walks WHERE clauses, so a block without one "+
				"is not merely unscoped -- it is INVISIBLE to it. That hole was measured: "+
				"a second LEFT JOIN LATERAL reading `sessions` with no WHERE was added to "+
				"ListPanelEmployees and this package answered ok.\n\nsubquery read:\n%s",
				name, file, sub.table, sub.text)
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

// subqueryRead is one parenthesised block that reads a tenant-scoped table.
type subqueryRead struct {
	table string
	text  string
}

// unscopedSubqueries finds parenthesised blocks that read a tenant-scoped table and
// carry NO WHERE clause at all.
//
// 🔴 IT EXISTS BECAUSE AN AUDIT WALKED THROUGH THE PREDICATE CHECK BY REMOVING THE
// THING IT WALKS. whereBlocks iterates WHERE clauses; a subquery with no WHERE
// contributes no block, so it is not "unscoped" to that check — it is invisible.
// Measured: adding
//
//	LEFT JOIN LATERAL (SELECT s2.last_used_at FROM sessions s2
//	                   ORDER BY s2.last_used_at DESC LIMIT 1) ls2 ON TRUE
//
// to ListPanelEmployees left `go test ./internal/domain/ledger` answering ok, while
// the error message two functions up said in as many words that "a CTE or subquery
// reading a tenant table needs its OWN predicate". The sentence was true as an
// intention and false as a description, and M6-05 is the task that made it
// reachable: it brought this package its first LATERAL.
//
// 🔴 WHAT IT CATCHES AND WHAT WALKS PAST IT — 21 ATTACKS, 14 CAUGHT, 7 ESCAPED, AND
// THE SEVEN ARE NAMED. An earlier version of this paragraph said a subquery reading a
// tenant table "MUST have a WHERE"; that is not a property this scanner holds, and
// three of the escapes below refute it directly. THE SCANNER IS NOT WIDENED TO CLOSE
// THEM, on purpose: this file is eleven rounds deep and every net it has written was
// beaten in the next round, so past that point the honest move is to COUNT a channel
// rather than to claim it shut. A counted hole is safer than one somebody believes
// is closed.
//
// CAUGHT (each verified by mutation): a WHERE-less LATERAL · a WHERE-less scalar
// subquery in the SELECT list · a WHERE-less CTE · a nested subquery's WHERE trying
// to mask its parent (maskNested) · EXISTS and IN, both spellings · a subquery inside
// ORDER BY · a read of audit_log, i.e. a table nobody hand-listed (the table set is
// derived from CREATE POLICY: 15 policies, 15 tables) · the word WHERE inside a
// string literal · a lateral whose predicate is neutralised by OR.
//
// ESCAPED — measured, not supposed:
//
//	UNION ALL branch reading employees/sessions with no WHERE
//	    the branch is not inside parentheses, so parenSpans never sees it
//	top-level JOIN ... ON with the tenant condition deleted (two variants)
//	    whereBlocks excludes JOIN ... ON structurally -- a condition there
//	    constrains the join, not the rows, which is true and also means the
//	    ON clause is unpoliced
//	FROM public.sessions with no WHERE
//	    fromOrJoinRE captures `public` as the table name, and `public` is not in
//	    the derived set. THIS IS THE ONE THAT REFUTES THE OLD SENTENCE HARDEST:
//	    the scanner KNOWS `sessions`, it just did not read that far
//	FROM ONLY sessions with no WHERE
//	    same shape -- `only` is captured as the table name
//	FROM generate_series(1,1) g, sessions s2
//	    a comma join, which is neither FROM <t> nor JOIN <t>
//
// WHAT STILL STOPS THE ROWS IN ALL SEVEN: RLS, which is the FIRST defence and the
// one that actually blocks. Everything in this file is about the SECOND -- the
// explicit predicate beside it -- and about making that second defence hard to
// delete quietly. Seven ways remain to delete it quietly; they are written here so
// nobody has to rediscover them.
func unscopedSubqueries(t *testing.T, body string) []subqueryRead {
	t.Helper()
	tables := tenantScopedTables(t)
	var out []subqueryRead
	for _, span := range parenSpans(body) {
		// 🔴 AT THIS SPAN'S OWN DEPTH ONLY, and that was the fourth escape rather
		// than a refinement. The first version asked whether the span CONTAINED the
		// word WHERE, and a nested subquery's WHERE satisfied it: measured, a
		// LATERAL reading `sessions` with no predicate of its own passed because it
		// had `ORDER BY (SELECT count(*) FROM locations WHERE tenant_id = @tenant_id)`
		// inside it. Blanking the nested spans is what makes "its OWN predicate" mean
		// what the error message says.
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

// maskNested replaces the contents of every nested parenthesis with spaces, leaving
// the span's own text in place and at its own offsets.
//
// SPACES RATHER THAN DELETION, which is the same rule scripts/redline-check.sh's
// lexer follows: removing text can join two tokens that were never adjacent and
// invent a keyword that is not there.
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

// fromOrJoinRE names the table a FROM or JOIN reads. LATERAL is optional and is
// skipped rather than captured, which is the form this package's own query uses.
var fromOrJoinRE = regexp.MustCompile(`(?i)\b(?:FROM|JOIN)\s+(?:LATERAL\s+)?([a-z_][a-z0-9_]*)`)

// parenSpans returns the contents of every parenthesised span in the text,
// innermost first. A subquery is always inside one.
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

// tenantScopedTables DERIVES which tables carry a tenant policy, by reading the
// migrations rather than by being told.
//
// A HAND-WRITTEN LIST IS A CHANGE DETECTOR — this file has been bitten by one
// already (the query list, closed by deriving it). Every tenant-scoped table in this
// schema gets a CREATE POLICY, because CLAUDE.md §6 makes that one of the five
// things a new table is born with, so the policies ARE the list.
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
	// ANTI-VACUITY: an empty map makes unscopedSubqueries find nothing, forever.
	for _, must := range []string{"employees", "sessions", "transactions"} {
		if !out[must] {
			t.Fatalf("derived %d tenant-scoped table(s) from db/migrations and %q is not "+
				"among them; the derivation is reading the wrong thing and every "+
				"subquery check built on it would pass vacuously", len(out), must)
		}
	}
	return out
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
	// FOR UPDATE / FOR SHARE added 2026-08-09 (M6-06 phase B) to keep the three
	// copies identical; the measurement is recorded in internal/domain/tenant's
	// copy. No-op for the queries this package covers (none takes a row lock).
	//
	// ⚠️ AND WHILE ADDING THEM, A PRE-EXISTING DIVERGENCE WAS MEASURED AND IS
	// RECORDED RATHER THAN SILENTLY FIXED: this copy's stop list has NO `RETURNING`,
	// while tenant's and review's do -- so this scanner reads a write's RETURNING
	// list as part of its WHERE clause. The header of tenant's copy states the three
	// are "behaviourally identical on purpose"; on this token they are not, and have
	// not been.
	//
	// 🔴 THE DIRECTION, MEASURED BOTH WAYS, BECAUSE "they differ" ON ITS OWN DOES NOT
	// SAY WHETHER ANYTHING IS AT RISK -- and an earlier version of this note stopped
	// there. Probed by rewriting a query this package covers (GetTransactionReview):
	//
	//   an UNSCOPED write whose only tenant mention is in the RETURNING list
	//     -> this net goes RED. The divergence does NOT let an unscoped write
	//        through; it is FAIL-CLOSED.
	//   a CORRECTLY scoped write that merely ENDS in a RETURNING list
	//     -> this net stays GREEN, and so does tenant's copy. No false alarm.
	//
	// So the divergence is INERT today: it cannot hide an unscoped statement, and it
	// does not flag a scoped one. That is why it is named rather than closed here --
	// closing it changes what this package's belt accepts and belongs with its own
	// mutation run, not with a task about `tags`.
	stopRE := regexp.MustCompile(`(?is)\b(ORDER\s+BY|GROUP\s+BY|LIMIT|HAVING|FOR\s+NO\s+KEY\s+UPDATE|FOR\s+UPDATE|FOR\s+SHARE)\b`)
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
