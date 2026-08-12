package tenant

// query_test.go -- section 4.5's BELT over the queries THIS package calls, asserted
// separately from its BRACES (RLS).
//
// 🔴 IT IS THE THIRD COPY OF THIS NET AND THAT IS A COST, NOT A PATTERN. The other
// two are internal/domain/ledger/query_test.go (the original, with the full history
// of five ways an earlier version was walked through) and
// internal/domain/review/query_test.go. Each copy exists because the derivation
// parses ITS OWN package directory, so a package with no copy has no belt at all --
// and M6-05 phase B put the first employee WRITES in this package. Three copies of
// one rule is exactly the "second representation" shape this repository has paid for
// repeatedly, so the alternatives were weighed rather than assumed:
//
//	widen ledger's scan to every internal/domain package   MEASURED AND REJECTED
//	  before this task, and the reason is recorded there: it turns RED on code that
//	  is correct today (LockEmployeeForTap is an advisory lock with no WHERE at all).
//	  Doing it properly needs a per-query opt-out with a reason, which is a task.
//	move the scanner into a shared non-test package        rejected HERE: it would
//	  rewrite two files whose comments carry the measurements that shaped them, as a
//	  side effect of a task about employees. That is how a settled net acquires an
//	  unreviewed change.
//
// 🔴 WHAT THIS COPY CHECKS, AND WHY IT IS NOT ledger's SHAPE. It walks STATEMENT
// BLOCKS -- the top level and every parenthesised block, each with its own nested
// parentheses masked -- and requires each block that reads or writes a tenant-scoped
// table to carry a WHERE that binds ITS OWN subject's scope column to @tenant_id.
// Two mutations forced that shape and both are kept as probes in
// TestStaffBlockScan_FlagsTheShapesThatBeatTheOlderCheck: a joined table's predicate
// standing in for the subject's, and a DATA-MODIFYING CTE whose UPDATE had no WHERE
// at all while the outer SELECT answered for it.
//
// ⚠️ AN EARLIER VERSION OF THIS HEADER DECLARED THE SECOND SHAPE A LIMIT ("no
// unscopedSubqueries scan here") AND POINTED AT ledger's COPY AS THE ONE THAT
// HANDLES IT. Both halves were wrong: the shape was reachable HERE, on a write, and
// "covered elsewhere" is the sentence this repository forbids. It is closed rather
// than counted, and the closing is measured.

import (
	"fmt"
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

// TestStaffQueries_CarryAnExplicitTenantPredicate is the belt.
//
// THE QUERIES ARE DERIVED, NOT LISTED. A hand-written list is a change detector: a
// new query with no tenant predicate is simply not in it, and the natural repair
// when it eventually goes red is to add the newcomer -- which is precisely the wrong
// move. Every store query this package NAMES is checked because it is named.
func TestStaffQueries_CarryAnExplicitTenantPredicate(t *testing.T) {
	declared := declaredQueries(t)
	names := storeQueryNames(t, declared)
	// ANTI-VACUITY: an empty derivation would make every assertion below pass over
	// nothing. The floor is 3 because this package's write path alone names three
	// (read, deactivate, move) and the tap page named two before it.
	if len(names) < 3 {
		t.Fatalf("derived %d store call(s) from this package (%v); the employee write "+
			"path names more than that, so the scan is reading the wrong directory",
			len(names), names)
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	// ⚠️ THE COVERAGE IS PRINTED, NOT ASSERTED, for the reason ledger's copy states:
	// coverage falls LEGITIMATELY whenever a read is deleted or moved, so a floor at
	// today's figure would go red on correct changes. The anti-vacuity floor above is
	// the only brake. Both quantities are computed at run time so neither can rot.
	t.Logf("section 4.5 belt coverage from THIS package: %d of %d queries declared in "+
		"db/queries (%.1f%%). internal/domain/ledger and internal/domain/review each "+
		"have their own copy over their own calls, so the product-wide figure is "+
		"higher than this one and lower than 100.\nseen here: %s",
		len(names), len(declared), 100*float64(len(names))/float64(len(declared)),
		strings.Join(sorted, ", "))

	scoped := tenantScopedTables(t)
	for _, name := range names {
		file, body, ok := findQuery(t, name)
		if !ok {
			t.Errorf("no query named %q anywhere in db/queries -- this package calls it, "+
				"so either it was renamed or this scan cannot see it", name)
			continue
		}
		findings, checked := scopeFindings(body, scoped)
		for _, f := range findings {
			t.Errorf("%s (%s) %s", name, file, f)
		}
		// ANTI-VACUITY, PER QUERY. A block walk that recognised no tenant table would
		// check nothing and report nothing -- the shape where a scanner passes because
		// it stopped reading. Every query this package calls touches a tenant-scoped
		// table by construction.
		if checked == 0 {
			t.Errorf("%s (%s): the scan found no statement touching a tenant-scoped table, "+
				"so it checked nothing. Either the query stopped reading one (unlikely) or "+
				"the table list (%d names, derived from CREATE POLICY) no longer matches "+
				"the SQL.", name, file, len(scoped))
		}
	}
}

// tagsQueryFile is the one query file whose statements have NO non-test caller
// yet, and therefore no AST-derived belt. See the test below.
const tagsQueryFile = "tags.sql"

// txnQueryFile is the SECOND file-derived belt, added by M6-08 (2026-08-10).
//
// 🔴 IT WAS ADDED BECAUSE A MUTATION PROVED THE OTHER TWO NETS CANNOT SEE THIS. M6-08
// added InsertManualTransaction and guarded its §4.5 belt two ways: a cross-tenant
// database test, and a TEXT assertion that the predicate is still in the file. An audit
// beat both in one edit -- `OR e.tenant_id IS NOT NULL` leaves the substring intact, so
// the text net sees it, and RLS still empties the SELECT, so the database test stays
// green. Measured here against the SAME matcher (2026-08-10):
//
//	as shipped                     0 findings
//	OR e.tenant_id IS NOT NULL     CAUGHT
//	a top-level OR TRUE            CAUGHT
//	a tautology in its place       CAUGHT
//	the predicate deleted          CAUGHT
//	bound to a literal, not @param CAUGHT
//
// So the matcher already answers the question the other two nets cannot, and pointing
// it at one more file is forty lines rather than a fourth scanner.
//
// ⚠️ THE WHOLE DIRECTORY IS STILL NOT COVERED, and the cost of that was measured
// rather than guessed before choosing one file: over all of db/queries the matcher
// flags exactly TWO queries, and BOTH are false alarms of one shape -- a
// correlated sub-query scoped to the OUTER statement's alias (`o.tenant_id =
// t.tenant_id`) rather than to the parameter, which this file's own negative control
// deliberately refuses ("another column, not the parameter"). One of the two is in
// THIS file and is opted out below with its reason; the other is
// ConsumeInviteAndActivate in invites.sql. Widening further is therefore cheap in
// count and expensive in judgement: every opt-out has to be argued, which is a task.
const txnQueryFile = "transactions.sql"

// txnScopeExemptions are the statements in transactions.sql the matcher flags and a
// human has cleared, each with the reason. It is an ALLOWLIST THAT MUST SHRINK: adding
// a name here is the only way to make this belt weaker, so it is an edit somebody has
// to defend.
var txnScopeExemptions = map[string]string{
	// A CORRELATED SUB-QUERY, AND IT IS CORRECTLY SCOPED. The NOT EXISTS binds
	// `o.tenant_id = t.tenant_id`, and the outer statement binds `t.tenant_id =
	// @tenant_id` -- so every row the sub-query can see is already inside the caller's
	// tenant, transitively. The matcher rejects it because it accepts ONLY a direct
	// binding to the parameter, which is the same strictness that makes it catch a
	// predicate on a different table; loosening it to follow aliases would weaken the
	// case its negative control exists to refuse. Fail-closed, in the safe direction.
	"GetLastOpenTransaction": "correlated NOT EXISTS scoped to the outer alias, which is itself bound to @tenant_id",
}

// txnScopeUnseen are the statements this matcher STRUCTURALLY CANNOT CHECK, each with
// the reason. They are listed rather than silently skipped, because a scanner that
// reports nothing about a statement and a scanner that approves it look identical from
// the outside.
//
// 🔴 THE SECOND ENTRY IS THE FINDING WORTH READING. The matcher looks for a tenant
// table in a FROM / UPDATE / JOIN and then walks WHERE clauses -- so an
// `INSERT ... VALUES` is INVISIBLE to it, no matter what it writes. M6-08's
// InsertManualTransaction is covered only because it happens to be an
// `INSERT ... SELECT` (the reviews.sql shape), where the SELECT gives the matcher
// something to hold. A future writer that used VALUES would get zero coverage from
// this belt and no warning that it had none.
//
// ⚠️ THE PRODUCT-WIDE SIZE OF THE BLIND SPOT, BECAUSE THE FIRST VERSION OF THIS
// PARAGRAPH QUOTED THE WRONG SCOPE. It said "exactly ONE statement is in that blind
// spot", which is true of THIS FILE and false of the product.
//
// ⚠️ AND THE FIGURE THAT REPLACED IT ROTTED IN ONE TASK. This paragraph said "70
// declared queries" as of 2026-08-10; M6-09 phase A added two reads and made it 72
// the same week, with the stale number still sitting here explaining a blind spot.
// A denominator that changes whenever anybody writes SQL does not belong in prose,
// so what is written down now is the SHAPE and the command:
//
//	grep -c '^-- name: ' db/queries/*.sql        # how many statements there are
//
// THE SHAPE, which does not rot: an `INSERT ... VALUES` is INVISIBLE to this
// matcher wherever it is pointed, because the matcher looks for a tenant table in a
// FROM / UPDATE / JOIN and then walks WHERE clauses, and such a statement has
// neither. The INSERTs that ARE held are the `INSERT ... SELECT` ones (the
// reviews.sql shape), where the SELECT gives the matcher something to hold.
//
// 🔴 AND TWO MORE WAYS PAST THIS BELT WERE MEASURED, so it is not read as a wall:
//
//  1. THE MATCHER NEVER LOOKS AT THE TENANT VALUE AN INSERT WRITES. It reads the
//     SELECT's WHERE; a statement that scoped its SELECT correctly and then wrote
//     `tenant_id` from a SECOND parameter would pass. Nothing does that today.
//  2. IT ONLY COVERS THE FILES IT IS POINTED AT. An unscoped read added to any file
//     other than tags.sql or transactions.sql is seen by none of the three belts.
//
// ⚠️ ALL THREE ARE CLOSED BY RLS RATHER THAN BY THIS TEST — they are holes in the
// BELT, not live openings (§4.5 is belt AND braces, and the braces hold: the policy,
// the WITH CHECK and the composite foreign keys were each probed). They are counted
// here rather than fixed: teaching the matcher to read an INSERT's target, and
// widening it to the whole directory, are each a change to a settled matcher shared by
// four call sites and belong with their own mutation run.
var txnScopeUnseen = map[string]string{
	"LockEmployeeForTap": "an advisory lock (pg_advisory_xact_lock) with no table and no WHERE at all",
	"InsertTransaction":  "INSERT ... VALUES -- the matcher can only see a tenant table through FROM/UPDATE/JOIN",
}

// TestTagsQueries_CarryAnExplicitTenantPredicate closes a HOLE IN THE DERIVATION
// ITSELF, not in a query.
//
// 🔴 THE MEASUREMENT THAT PUT IT HERE (M6-06 phase B, 2026-08-09). Migration 00013
// shipped four panel statements over `tags` and NONE of the three belt copies
// counted them -- the printed coverage stayed at 18 while the denominator went
// 60 -> 64. The cause is not the queries' shape, it is the SCANNER'S SCOPE, and it
// is two layers deep:
//
//	storeQueryNames parses ITS OWN package directory ....... so a query called from
//	  another package is invisible to this copy by construction; and
//	it filters out *_test.go (see the parser filter) ....... so a query whose ONLY
//	  caller today is a test is invisible to EVERY copy.
//
// Measured: each of the four is named in exactly one file, internal/db/
// tagsinventory_test.go, because the panel handlers are a later round. So there was
// no derivation anywhere that could reach them -- a fourth AST copy in internal/db
// would have derived ZERO names and printed a green vacuum.
//
// 🔴 SO THIS ONE DERIVES FROM THE FILE, NOT FROM A CALLER, and that is the property
// worth having: a query is checked because it EXISTS, not because something already
// uses it. The window this closes is exactly the dangerous one -- between "the SQL
// is merged" and "a handler calls it", which is when nobody is looking.
//
// WHY IT LIVES IN THIS FILE INSTEAD OF A FOURTH COPY: the matcher (whereClauses,
// subjectOf, scopedBySubject, scopeFindings) is already here, already hardened by
// the mutations recorded above, and this header's own warning is that a fourth copy
// is a cost. This test adds a second DERIVATION over the existing matcher -- about
// forty lines -- rather than a fourth scanner. It drives the same scopeFindings the
// belt drives, exactly as TestStaffBlockScan_... already does over bodies read from
// db/queries.
//
// SCOPE, STATED SO IT IS NOT MISTAKEN FOR MORE: it covers db/queries/tags.sql and
// nothing else. Widening it to the whole directory is the change this file's header
// records as MEASURED AND REJECTED -- it turns red on queries that are correct today
// (LockEmployeeForTap is an advisory lock with no WHERE at all) and needs a
// per-query opt-out with a reason, which is a task. tags.sql needs no opt-out:
// EVERY statement in it is tenant-scoped, and the day one is not, this test goes red
// and somebody has to decide -- which is the point.
//
// ⚠️ A MEASURED FALSE ALARM, WRITTEN DOWN BECAUSE IT WILL SURPRISE SOMEBODY. The
// matcher masks nested parentheses, so a WHERE whose ENTIRE body is parenthesised --
// `WHERE (g.tenant_id = @tenant_id AND g.uid = @uid)` -- is masked away and reported
// as "has NO WHERE clause at all". Reproduced on GetTagForTenant. It is not a hole:
// it fails CLOSED, refusing a correctly scoped query rather than accepting an
// unscoped one, so the worst it costs is a puzzled author. Written as a limit rather
// than fixed, because changing the mask is a change to a settled matcher shared by
// three copies and belongs with its own mutation run.
//
// ⚠️ THE SET IS DERIVED, SO THIS COMMENT NAMES NO COUNT. An earlier version said
// "all five of its statements" and was stale within the same task (the file went to
// six when ListTagLastSeen landed), while the runtime belt in
// internal/db/tagsinventory_test.go said "all four" -- two nets describing the same
// file with two different numbers, neither of which was right. The floor below is
// the only number, and it is an ANTI-VACUITY floor rather than a description.
func TestTagsQueries_CarryAnExplicitTenantPredicate(t *testing.T) {
	declared := declaredQueries(t)
	names := queriesDeclaredIn(t, tagsQueryFile)

	// ANTI-VACUITY: an empty or shrunken derivation would make everything below pass
	// over nothing. The floor is 5 -- what 00013 shipped before ListTagLastSeen was
	// added -- deliberately BELOW today's count, so that deleting a query fails
	// loudly while adding one never does.
	if len(names) < 5 {
		t.Fatalf("derived %d query name(s) from db/queries/%s (%v); this file carries at "+
			"least five, so this scan is reading the wrong file", len(names), tagsQueryFile, names)
	}
	for _, n := range names {
		if !declared[n] {
			t.Errorf("%q is declared in %s but declaredQueries did not see it; the two "+
				"readers disagree, so one of them is misparsing", n, tagsQueryFile)
		}
	}

	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	// 🔴 THE UNION IS COMPUTED, NOT ADDED, AND THAT IS A CORRECTION WITH A DATE.
	// Until M6-06 phase B this line said "the two sets are DISJOINT, so together they
	// are N+M" -- true while nothing in this package CALLED a tags query, and false
	// the moment internal/domain/tenant/plaque.go did. The overlap is now five
	// (everything in tags.sql except AdvanceTagCounter, whose caller is internal/sun),
	// so the sum over-counted. A derived union cannot go stale the same way; the
	// sentence that asserted a relationship between two sets could, and did.
	called := storeQueryNames(t, declared)
	union := map[string]bool{}
	for _, n := range names {
		union[n] = true
	}
	overlap := 0
	for _, n := range called {
		if union[n] {
			overlap++
		}
		union[n] = true
	}
	t.Logf("section 4.5 belt coverage over db/queries/%s (file-derived, no caller "+
		"required): %d of %d queries declared in db/queries (%.1f%%). This package's "+
		"CALL-derived belt covers %d, of which %d are the same statements; the UNION is "+
		"%d of %d (%.1f%%).\nseen here: %s",
		tagsQueryFile, len(names), len(declared),
		100*float64(len(names))/float64(len(declared)),
		len(called), overlap, len(union), len(declared),
		100*float64(len(union))/float64(len(declared)),
		strings.Join(sorted, ", "))

	scoped := tenantScopedTables(t)
	for _, name := range names {
		file, body, ok := findQuery(t, name)
		if !ok {
			t.Errorf("no body for %q -- declared in %s but unreadable", name, tagsQueryFile)
			continue
		}
		findings, checked := scopeFindings(body, scoped)
		for _, f := range findings {
			t.Errorf("%s (%s) %s", name, file, f)
		}
		if checked == 0 {
			t.Errorf("%s (%s): the scan found no statement touching a tenant-scoped table, "+
				"so it checked nothing -- every query in this file touches `tags`.", name, file)
		}
	}
}

// queriesDeclaredIn reads every `-- name: X` from ONE file in db/queries, with the
// same line-anchored regexp declaredQueries uses (a marker inside a comment must not
// resolve).
func queriesDeclaredIn(t *testing.T, file string) []string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "db", "queries", file)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading db/queries/%s: %v", file, err)
	}
	re := regexp.MustCompile(`(?m)^-- name: (\w+) `)
	var out []string
	for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
		out = append(out, m[1])
	}
	return out
}

// TestTransactionsQueries_CarryAnExplicitTenantPredicate is the tags belt pointed at
// transactions.sql (M6-08). See txnQueryFile for why it exists and what widening
// further would cost.
//
// It is FILE-DERIVED like the tags one: a statement is checked because it EXISTS, not
// because something calls it -- which is the window that matters, between "the SQL is
// merged" and "a handler calls it".
func TestTransactionsQueries_CarryAnExplicitTenantPredicate(t *testing.T) {
	declared := declaredQueries(t)
	names := queriesDeclaredIn(t, txnQueryFile)

	// ANTI-VACUITY: a shrunken derivation would pass over nothing. The floor is
	// deliberately BELOW today's count, so deleting a query fails loudly while adding
	// one never does.
	if len(names) < 8 {
		t.Fatalf("derived %d query name(s) from db/queries/%s (%v); this file carries at "+
			"least eight, so this scan is reading the wrong file", len(names), txnQueryFile, names)
	}
	scoped := tenantScopedTables(t)
	exempted, flagged, unseen := 0, 0, 0
	for _, n := range names {
		if !declared[n] {
			t.Errorf("%q is declared in %s but declaredQueries did not see it; the two "+
				"readers disagree, so one of them is misparsing", n, txnQueryFile)
		}
		_, body, ok := findQuery(t, n)
		if !ok {
			t.Errorf("no query named %q anywhere in db/queries", n)
			continue
		}
		findings, checked := scopeFindings(body, scoped)
		if why, ok := txnScopeExemptions[n]; ok {
			exempted++
			// 🔴 AN EXEMPTION THAT STOPS BEING NEEDED IS DELETED, NOT LEFT. A name here
			// with nothing to exempt is an allowlist entry protecting nothing, and the
			// next reader would take it as evidence the query is unscoped.
			if len(findings) == 0 {
				t.Errorf("%s is exempted (%q) but the matcher no longer flags it. Remove the "+
					"entry: an exemption nobody needs is a hole nobody is watching.", n, why)
			}
			continue
		}
		for _, f := range findings {
			flagged++
			t.Errorf("%s (%s) %s", n, txnQueryFile, f)
		}
		if checked == 0 {
			if why, ok := txnScopeUnseen[n]; ok {
				unseen++
				_ = why
				continue
			}
			t.Errorf("%s: the scan found no statement touching a tenant-scoped table, so it "+
				"checked nothing. Either the query stopped reading one, or it is a shape this "+
				"matcher cannot see -- in which case add it to txnScopeUnseen WITH the reason "+
				"rather than leaving a silent gap. (%d table names, derived from CREATE POLICY.)",
				n, len(scoped))
		}
	}
	// AN ENTRY THAT IS NO LONGER NEEDED IS A HOLE NOBODY IS WATCHING, so the blind-spot
	// list is held to its own size the same way the exemption list is.
	if unseen != len(txnScopeUnseen) {
		t.Errorf("txnScopeUnseen names %d statement(s) but only %d were actually invisible "+
			"to the matcher; remove the stale entries", len(txnScopeUnseen), unseen)
	}
	t.Logf("section 4.5 belt over db/queries/%s (file-derived): %d queries, %d exempted "+
		"with a written reason, %d invisible to the matcher (also written down), %d flagged",
		txnQueryFile, len(names), exempted, unseen, flagged)
}

// TestStaffScopeCheck_IsNotVacuous is the negative control for the MATCHER.
// Without it a blinded matcher would make the belt pass over unscoped SQL.
func TestStaffScopeCheck_IsNotVacuous(t *testing.T) {
	unscoped := map[string]string{
		"no predicate at all":               "e.id = @id",
		"neutralised by OR":                 "(e.tenant_id = @tenant_id OR TRUE)",
		"OR at the top level":               "e.tenant_id = @tenant_id OR 1 = 1",
		"a literal, not the caller":         "e.tenant_id = '00000000-0000-0000-0000-000000000000'",
		"another column, not the parameter": "e.tenant_id = l.tenant_id",
		// 🔴 THE CASE A MUTATION FOUND: the clause IS scoped -- just not the table
		// being written. Deleting the subject's predicate from MoveEmployee left
		// exactly this shape behind, and the older matcher accepted it.
		"a DIFFERENT table's predicate": "l.tenant_id = @tenant_id AND e.id = @id",
	}
	for why, where := range unscoped {
		if scopedBySubject(where, subject{table: "employees", alias: "e", column: "tenant_id"}) {
			t.Errorf("the scope check ACCEPTED %q (%s), which scopes nothing to the "+
				"caller's tenant", where, why)
		}
	}
	scoped := map[string]string{
		"plain":                            "e.tenant_id = @tenant_id",
		"reversed operands":                "@tenant_id = e.tenant_id",
		"cast":                             "e.tenant_id = @tenant_id::uuid",
		"no alias":                         "tenant_id = @tenant_id",
		"the subject beside another table": "l.tenant_id = @tenant_id AND e.tenant_id = @tenant_id",
		"conjoined with filters":           "e.tenant_id = @tenant_id AND e.status <> 'deactivated'",
		"second conjunct":                  "e.id = @id AND e.tenant_id = @tenant_id",
	}
	for why, where := range scoped {
		if !scopedBySubject(where, subject{table: "employees", alias: "e", column: "tenant_id"}) {
			t.Errorf("the scope check REJECTED %q (%s), which IS correctly scoped -- a "+
				"check that accepts only one spelling is a change detector", where, why)
		}
	}
}

// TestStaffQueryBody_IsBounded is the control for the READER. A reader that overruns
// into the next query lets ONE tenant predicate anywhere in a file satisfy the belt
// for every query in it -- and a reader whose marker is not line-anchored can be
// sent to a DIFFERENT file entirely by one comment line, which is how an audit made
// an unscoped query pass in the ledger copy.
func TestStaffQueryBody_IsBounded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "probe.sql")
	if err := os.WriteFile(path, []byte(
		"-- name: FirstQuery :one\n"+
			"UPDATE a SET x = 1 WHERE id = @id RETURNING id;\n"+
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
	if c := whereClauses(first); len(c) == 0 || scopedBySubject(c[0], subjectOf(first)) {
		t.Error("FirstQuery has no tenant predicate, but the check says it does -- the " +
			"reader is leaking the neighbouring query into it")
	}
	second, _ := queryBody(t, path, "SecondQuery")
	if c := whereClauses(second); len(c) == 0 || !scopedBySubject(c[0], subjectOf(second)) {
		t.Error("SecondQuery IS scoped and the check says it is not -- the pair is " +
			"reading nothing at all")
	}

	// A `-- name:` marker sitting INSIDE a comment must not resolve, in an
	// alphabetically earlier file or anywhere else.
	hijack := filepath.Join(dir, "aaa_earlier.sql")
	if err := os.WriteFile(hijack, []byte(
		"-- name: SomethingElse :one\n"+
			"-- cross-reference: the write is -- name: SecondQuery :many\n"+
			"SELECT 3 FROM d WHERE x = 1;\n"), 0o600); err != nil {
		t.Fatalf("writing the hijack probe: %v", err)
	}
	if _, found := queryBody(t, hijack, "SecondQuery"); found {
		t.Error("a `-- name:` marker inside a comment resolved to a query body; the " +
			"reader must anchor to the start of a line, exactly as declaredQueries does")
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

// TestStaffQueryClauses_StopAtRETURNING: every write this package calls ends in a
// RETURNING list, and one of them RETURNS a column called tenant_id would satisfy
// the belt for a statement with no predicate at all if the clause reader ran past
// the WHERE.
func TestStaffQueryClauses_StopAtRETURNING(t *testing.T) {
	body := "UPDATE employees SET status = 'deactivated'\nWHERE id = @id\nRETURNING tenant_id = @tenant_id;"
	clauses := whereClauses(body)
	if len(clauses) != 1 {
		t.Fatalf("read %d WHERE clause(s) from an UPDATE ... RETURNING, want 1: %q", len(clauses), clauses)
	}
	if strings.Contains(strings.ToUpper(clauses[0]), "RETURNING") {
		t.Errorf("the WHERE clause swallowed the RETURNING list: %q", clauses[0])
	}
	if scopedBySubject(clauses[0], subjectOf(body)) {
		t.Errorf("a statement whose only tenant_id is in its RETURNING list was accepted "+
			"as scoped: %q", clauses[0])
	}
}

// TestStaffBlockScan_FlagsTheShapesThatBeatTheOlderCheck is the negative control for
// the BLOCK WALK, and every probe below is a shape that was measured passing before
// it existed.
//
// 🔴 A TEST THAT ONLY ASSERTS "THE SHIPPED QUERIES ARE FINE" IS SATISFIED BY A
// SCANNER THAT LOOKS AT NOTHING. This one drives the same function the belt drives
// (scopeFindings) over hostile bodies and requires it to object -- and requires it
// to STAY QUIET on the shape the product actually ships, because a scanner that
// flags correct SQL is one the next person switches off.
//
// ⚠️ THE FILE USED TO CARRY A TEST NAMED ...ASubqueryWithNoWhereIsInvisible, WHICH
// ASSERTED THE HOLE AND CLAIMED IT WOULD GO RED THE DAY SOMEBODY CLOSED IT. An
// auditor wrote the missing scan, wired it to the belt, and BOTH tests stayed green:
// that test only ever ran the matcher over its own literal probe, so it had no way to
// observe whether a scan existed. The claim was a memory dressed as a measurement.
// The hole is CLOSED now, so the test is gone rather than reworded.
func TestStaffBlockScan_FlagsTheShapesThatBeatTheOlderCheck(t *testing.T) {
	scoped := tenantScopedTables(t)
	hostile := map[string]string{
		// 1. A DATA-MODIFYING CTE WITH NO WHERE AT ALL. The auditor's mutation, on
		// MoveEmployee: the UPDATE rewrites every row the role can see and the outer
		// SELECT's predicate used to answer for it.
		"a CTE that updates with no WHERE": `
			WITH moved AS (
			    UPDATE employees SET location_id = @location_id
			    RETURNING id, tenant_id
			)
			SELECT m.id FROM moved m WHERE m.tenant_id = @tenant_id AND m.id = @id;`,
		// 2. THE SAME CTE WITH AN UNSCOPED WHERE. Having a WHERE is not the property;
		// binding the subject's tenant_id is.
		"a CTE whose WHERE does not scope": `
			WITH ended AS (
			    UPDATE employees SET status = 'deactivated'
			    WHERE id = @id
			    RETURNING id, tenant_id
			)
			SELECT id FROM ended WHERE tenant_id = @tenant_id;`,
		// 3. A SUBQUERY READ WITH NO WHERE. This is the shape internal/domain/ledger
		// closed with a scan of its own and this copy used to declare invisible.
		"a scalar subquery reading a tenant table": `
			SELECT e.id, (SELECT count(*) FROM sessions) AS n
			FROM employees e WHERE e.tenant_id = @tenant_id;`,
		// 4. A LATERAL with no WHERE -- the shape the ledger audit used.
		"a LATERAL reading a tenant table": `
			SELECT e.id, s.last_used_at
			FROM employees e
			LEFT JOIN LATERAL (SELECT s2.last_used_at FROM sessions s2 LIMIT 1) s ON TRUE
			WHERE e.tenant_id = @tenant_id;`,
		// 5. THE SUBJECT UNSCOPED WHILE A JOINED TABLE IS SCOPED -- the first mutation
		// this file ever survived.
		"a joined table's predicate standing in for the subject's": `
			UPDATE employees e SET location_id = l.id
			FROM locations l
			WHERE e.id = @id AND l.tenant_id = @tenant_id AND l.id = @location_id;`,
	}
	for why, body := range hostile {
		findings, checked := scopeFindings(body, scoped)
		if checked == 0 {
			t.Errorf("the scan checked NO block in %s; it is reading nothing", why)
			continue
		}
		if len(findings) == 0 {
			t.Errorf("the scan ACCEPTED %s, which scopes nothing to the caller's tenant:\n%s",
				why, body)
		}
	}

	// AND IT MUST STAY QUIET ON THE SHIPPED SHAPES. Their bodies are read from
	// db/queries rather than restated here, so this control cannot drift away from
	// the product the way a copied literal would.
	for _, name := range []string{"MoveEmployee", "DeactivateEmployee", "GetPanelEmployeeForAction"} {
		_, body, ok := findQuery(t, name)
		if !ok {
			t.Fatalf("%s is not in db/queries; this control is reading nothing", name)
		}
		findings, checked := scopeFindings(body, scoped)
		if checked == 0 {
			t.Errorf("%s: the scan checked no block, so its silence means nothing", name)
		}
		if len(findings) != 0 {
			t.Errorf("the scan flagged the SHIPPED %s:\n%s", name, strings.Join(findings, "\n"))
		}
	}
}

// --- the scanner ---------------------------------------------------------------
//
// The four functions below are ledger's, kept behaviourally identical on purpose:
// three copies that DISAGREE would be worse than three that agree, because each one
// passes on its own and the drift is invisible. Their reasoning is documented once,
// in internal/domain/ledger/query_test.go, and is not repeated here -- what is
// repeated is the code, which is the cost this file's header names.

// whereClauses returns every top-level WHERE clause in the query text, ending each
// at its enclosing block's closing paren or at a clause that may follow WHERE.
func whereClauses(body string) []string {
	var out []string
	whereRE := regexp.MustCompile(`(?is)\bWHERE\b`)
	// FOR UPDATE / FOR SHARE ADDED 2026-08-09 (M6-06 phase B), and it was a REAL
	// blind spot rather than a tidy-up: a locking clause follows the WHERE, so
	// without it the last conjunct of a locked read is returned as
	// "p.tenant_id = @tenant_id\n FOR UPDATE" and the equality no longer matches --
	// the matcher flags a query that IS correctly scoped. Measured on
	// AdvanceTagCounter, the only locked read in db/queries and, until the
	// file-derived net above existed, the only one no belt covered. The same three
	// tokens are added to all three copies of this scanner: copies that DISAGREE are
	// worse than copies that agree, and this one is a no-op for the other two (no
	// query they cover takes a row lock -- grepped).
	stopRE := regexp.MustCompile(`(?is)\b(ORDER\s+BY|GROUP\s+BY|LIMIT|HAVING|RETURNING|FOR\s+NO\s+KEY\s+UPDATE|FOR\s+UPDATE|FOR\s+SHARE)\b`)
	for _, loc := range whereRE.FindAllStringIndex(body, -1) {
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
		if clause != "" {
			out = append(out, clause)
		}
	}
	return out
}

// scopeFindings runs the whole check over one query body and RETURNS what it found,
// so the belt test and its negative control drive the SAME code.
//
// 🔴 THE UNIT OF THE CHECK IS A BLOCK, NOT A QUERY, AND A MUTATION FORCED THAT. The
// previous version walked the query's WHERE clauses and asked whether any of them
// scoped the query's subject; an auditor beat it with a DATA-MODIFYING CTE, where the
// outer SELECT's predicate answered for an `UPDATE employees` that had no WHERE at
// all. A CTE is a statement, so it is checked as one -- see statementBlocks.
//
// It returns the findings and HOW MANY blocks it actually checked, because a scanner
// that recognised nothing would otherwise report success.
func scopeFindings(body string, scoped map[string]bool) (findings []string, checked int) {
	for _, blk := range statementBlocks(body) {
		tables := tenantTablesIn(blk, scoped)
		if len(tables) == 0 {
			continue
		}
		checked++
		subj := subjectOf(blk)
		clauses := whereClauses(blk)
		if len(clauses) == 0 {
			findings = append(findings, fmt.Sprintf(
				"has a statement that reads or writes %v and has NO WHERE clause at all, "+
					"so every row the role can see is in scope.\n"+
					"section 4.5 asks for belt AND braces, and a block with no WHERE is not "+
					"merely unscoped -- a check that walks WHERE clauses cannot SEE it.\n\n"+
					"statement read:\n%s", tables, strings.TrimSpace(blk)))
			continue
		}
		for _, where := range clauses {
			if scopedBySubject(where, subj) {
				continue
			}
			findings = append(findings, fmt.Sprintf(
				"has a WHERE clause that does not bind %s to %s as a TOP-LEVEL CONJUNCT.\n"+
					"Without it every behavioural isolation test still passes -- RLS alone "+
					"blocks the rows -- so this is the only place the second defence is "+
					"visible. A predicate inside an OR, or moved into a JOIN ... ON, does NOT "+
					"count: neither scopes the statement's own result, and neither does a "+
					"predicate on a DIFFERENT table that happens to be in the same "+
					"clause.\n\nWHERE clause read:\n%s",
				subj.qualified(), tenantParam, where))
		}
	}
	return findings, checked
}

// subject is the table a statement is ABOUT: what it updates, or what it selects
// from first, together with the column that scopes it.
type subject struct {
	table string
	alias string
	// column is "tenant_id" everywhere except `tenants`, which IS the tenant -- its
	// own primary key is the scope, and migration 00001's policy says exactly that.
	// internal/domain/ledger's copy learned this the same way: a hard-coded
	// tenant_id reported GetTenantClock as unscoped, which was wrong about the
	// product rather than about the query.
	column string
}

func (s subject) qualified() string {
	if s.alias == "" {
		return s.column
	}
	return s.alias + "." + s.column
}

// scopedBySubject reports whether one TOP-LEVEL conjunct binds THE SUBJECT TABLE's
// scope column to the caller's parameter.
//
// 🔴 "THE SUBJECT'S" IS THE PART A MUTATION FORCED, and it is the one place this copy
// is STRICTER than internal/domain/ledger's. That version asks whether the clause
// contains any `<alias>.tenant_id = @tenant_id`, which is right for its queries
// because each table it reads has a WHERE clause of its own. This package's write
// path does not have that shape: an `UPDATE employees e ... FROM locations l ...
// WHERE ...` puts BOTH tables' predicates in ONE clause, so "some alias is bound"
// was satisfied by the JOINED table alone. Measured before this function existed:
// deleting `e.tenant_id = @tenant_id` from MoveEmployee left `l.tenant_id =
// @tenant_id` behind and the belt answered ok -- the employees row went unscoped and
// nothing went red.
//
// ⚠️ THIS IS NOT A CLAIM THAT LEDGER'S COPY IS WRONG. It is a claim that its matcher
// does not cover the shape THIS package introduced, which is a different sentence.
func scopedBySubject(where string, subj subject) bool {
	for _, conjunct := range splitTopLevelAnd(where) {
		if isScopeEquality(conjunct, subj) {
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
	before := i == 0 || !isWordByte(s[i-1])
	after := i+n >= len(s) || !isWordByte(s[i+n])
	return before && after
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

// statementBlocks splits a query into the statements a scope check must consider
// SEPARATELY: the top level, and every parenthesised block, each with ITS OWN nested
// parentheses masked so a nested WHERE cannot be mistaken for the enclosing one.
//
// 🔴 IT EXISTS BECAUSE AN AUDITOR WALKED THROUGH THE PREVIOUS VERSION WITH A WRITE.
// The check used to run over the whole query's WHERE clauses, which meant a
// DATA-MODIFYING CTE was invisible: measured on MoveEmployee, exactly this shape
// left the belt green --
//
//	WITH moved AS (
//	  UPDATE employees SET location_id = @location_id
//	  RETURNING id, tenant_id, full_name, location_id, department_id
//	)
//	SELECT ... FROM moved m WHERE m.tenant_id = @tenant_id AND m.id = @id;
//
// The UPDATE has no WHERE at all -- it rewrites every row the role can see -- and
// the outer SELECT's predicate satisfied the old check on the way out. A CTE is a
// STATEMENT, so it is now checked as one.
//
// Masking preserves offsets and newlines so the caller can still print what it read.
func statementBlocks(body string) []string {
	blocks := []string{maskParens(body)}
	for _, span := range parenSpans(body) {
		inner := body[span[0]+1 : span[1]]
		blocks = append(blocks, maskParens(inner))
	}
	return blocks
}

// parenSpans returns every balanced parenthesised span, at every depth.
func parenSpans(s string) [][2]int {
	var out [][2]int
	var stack []int
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			stack = append(stack, i)
		case ')':
			if n := len(stack); n > 0 {
				out = append(out, [2]int{stack[n-1], i})
				stack = stack[:n-1]
			}
		}
	}
	return out
}

// maskParens blanks out every parenthesised span, keeping length and line breaks so
// the caller can still search the top level of a statement.
func maskParens(s string) string {
	out := []byte(s)
	depth := 0
	for i := 0; i < len(out); i++ {
		switch out[i] {
		case '(':
			depth++
			out[i] = ' '
			continue
		case ')':
			if depth > 0 {
				depth--
			}
			out[i] = ' '
			continue
		}
		if depth > 0 && out[i] != '\n' {
			out[i] = ' '
		}
	}
	return string(out)
}

// tableRefRE finds the tables a statement reads or writes AT ITS OWN LEVEL.
//
// ⚠️ INSERT IS DELIBERATELY NOT IN THE LIST, and the reason is that an
// `INSERT ... VALUES` has no WHERE and cannot have one. Its belt is a different
// shape -- tenant_id is supplied as a VALUE and the RLS WITH CHECK refuses a
// mismatch, which db/queries/invites.sql states at CreateInvite. It is written down
// because a scanner that flagged every INSERT would be a scanner somebody switches
// off.
//
// ⚠️ THIS PARAGRAPH USED TO END "No query this package calls inserts anything, so
// nothing here relies on that", AND M6-06 PHASE A MADE THAT FALSE. It now calls
// two: CreateLocation and CreateDepartment. Neither relies on the exclusion,
// because neither is an INSERT ... VALUES -- both are INSERT ... SELECT with a
// scoped source (`tenants` and `locations` respectively), so the block walk sees
// an ordinary statement and checks it like any other.
//
// 🔴 AND THAT SHAPE WAS CHOSEN BY THIS FILE GOING RED RATHER THAN BY TASTE.
// CreateLocation was first written as INSERT ... VALUES, and the per-query
// anti-vacuity guard in TestStaffQueries_CarryAnExplicitTenantPredicate reported
// that it had checked NOTHING -- which is the guard doing exactly its job. The fix
// was to give the statement a scoped source rather than to add an exemption,
// because an exemption is a hole that the next INSERT inherits silently.
var tableRefRE = regexp.MustCompile(
	`(?is)\b(?:FROM|JOIN|UPDATE|DELETE\s+FROM)\s+(?:ONLY\s+)?(?:public\.)?([a-z_][a-z0-9_]*)`)

// tenantTablesIn names the tenant-scoped tables one block touches.
func tenantTablesIn(block string, scoped map[string]bool) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range tableRefRE.FindAllStringSubmatch(block, -1) {
		name := strings.ToLower(m[1])
		if scoped[name] && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// subjectOf names the table a block is ABOUT: what it updates or deletes from, else
// what it selects from first.
//
// THE WRITE TARGET WINS OVER THE FIRST FROM, which is what makes an
// `UPDATE employees e ... FROM locations l` block ask about employees rather than
// about locations.
func subjectOf(block string) subject {
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`(?is)\b(?:UPDATE|DELETE\s+FROM)\s+(?:ONLY\s+)?(?:public\.)?([a-z_][a-z0-9_]*)\s*(?:AS\s+)?([a-z_][a-z0-9_]*)?`),
		regexp.MustCompile(`(?is)\bFROM\s+(?:ONLY\s+)?(?:public\.)?([a-z_][a-z0-9_]*)\s*(?:AS\s+)?([a-z_][a-z0-9_]*)?`),
	} {
		if m := re.FindStringSubmatch(block); m != nil {
			return newSubject(m[1], m[2])
		}
	}
	return subject{column: "tenant_id"}
}

func newSubject(table, alias string) subject {
	switch strings.ToUpper(alias) {
	case "", "SET", "WHERE", "JOIN", "LEFT", "RIGHT", "INNER", "OUTER", "FULL",
		"CROSS", "ON", "USING", "ORDER", "GROUP", "LIMIT", "RETURNING", "VALUES",
		"SELECT", "AS", "NATURAL", "LATERAL", "FOR", "HAVING", "WINDOW", "UNION":
		alias = ""
	}
	column := "tenant_id"
	if strings.EqualFold(table, "tenants") {
		column = "id"
	}
	return subject{table: strings.ToLower(table), alias: alias, column: column}
}

// tenantScopedTables DERIVES the table list from the migrations' RLS policies, so a
// new tenant-scoped table joins the scan the day it lands rather than the day
// somebody remembers to add it here.
func tenantScopedTables(t *testing.T) map[string]bool {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "db", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading db/migrations: %v", err)
	}
	re := regexp.MustCompile(`(?is)CREATE\s+POLICY\s+\S+\s+ON\s+(?:public\.)?([a-z_][a-z0-9_]*)`)
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
	if len(out) < 5 {
		t.Fatalf("derived %d tenant-scoped table(s) from CREATE POLICY; the schema has "+
			"more, so this scan is reading the wrong directory and would check nothing",
			len(out))
	}
	return out
}

// isScopeEquality accepts either operand order and any cast -- both of which are the
// SAME predicate -- but only on the SUBJECT's own tenant_id.
//
// The alias is optional when there is none to write: `UPDATE employees SET ... WHERE
// tenant_id = @tenant_id` binds the subject just as `e.tenant_id` does. The COLUMN
// comes from the subject too, because `tenants` is scoped by its own id.
func isScopeEquality(conjunct string, subj subject) bool {
	c := strings.TrimSpace(conjunct)
	c = regexp.MustCompile(`::\s*[a-zA-Z_]+`).ReplaceAllString(c, "")
	c = strings.Trim(c, "() \t\n")
	c = strings.Join(strings.Fields(c), " ")

	col := `(?:[a-z_][a-z0-9_]*\.)?` + regexp.QuoteMeta(subj.column)
	if subj.alias != "" {
		col = `(?:` + regexp.QuoteMeta(subj.alias) + `\.)?` + regexp.QuoteMeta(subj.column)
	}
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

// queryBody returns the SQL of one named sqlc query WITHOUT its comment lines, so a
// tenant_id mentioned only in prose cannot satisfy the check. The marker is
// LINE-ANCHORED, which is what stops a `-- name:` inside a comment resolving.
func queryBody(t *testing.T, path, name string) (string, bool) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	marker := regexp.MustCompile(`(?m)^-- name: ` + regexp.QuoteMeta(name) + ` `)
	loc := marker.FindStringIndex(string(raw))
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

// declaredQueries reads every `-- name: X` in db/queries.
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

// storeQueryNames returns every sqlc query this package NAMES, found with go/ast
// rather than with a regexp -- an audit beat the regexp version three ways (a
// newline between the dot and the method name, a space before the parenthesis, and
// the method taken as a value). What still escapes it is a name held as a STRING
// (reflection) and a call made from another package.
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
