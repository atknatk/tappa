package main

// tableslock_test.go -- the "tappa/test/operator-tables" advisory lock is asked for
// ONCE per test tree, read from the test sources of every package that takes it.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// tablesLockDirs are the test packages that take the operator-tables lock, relative to
// this directory: internal/db EXCLUSIVE (opTx) and SHARED (TestOperatorDB_RunsAsTappaOperator
// and opLiveFixture, OP-10), the others SHARED (internal/adminauth since M14-07; internal/domain/signup
// and internal/handler since M14-08, whose registrations and panel identity tests commit identities).
var tablesLockDirs = []string{".", "../../internal/db", "../../internal/handler/operator", "../../internal/operatorauth", "../../internal/adminauth", "../../internal/domain/signup", "../../internal/handler"}

// tablesLockStatements are the functions that send the lock statement themselves, per
// directory -- the closed list the scan must find (its control against going blind).
var tablesLockStatements = map[string][]string{
	".":                               {"sharedTablesLock"},
	"../../internal/db":               {"TestOperatorDB_RunsAsTappaOperator", "bfTxLocking", "opLiveFixture", "opTx"},
	"../../internal/handler/operator": {"newE2E"},
	"../../internal/operatorauth":     {"sharedTablesLock"},
	// M14-07: the identity worlds commit identities; M14-09's audit: every committed admin row too
	// (ownerConn and newAdminRow take it shared, through holdTablesLock).
	"../../internal/adminauth": {"sharedTablesLock"},
	// M14-08: a registration of a new address commits an identity (identityOwner takes it shared).
	"../../internal/domain/signup": {"sharedTablesLock"},
	// M14-08: the panel's flag-off and password tests link a committed identity (linkHarness);
	// M14-09: every panel harness holds it (its rows are committed), through holdPanelTablesLock.
	"../../internal/handler": {"sharedTablesLock"},
}

// tablesLockReentrant are, per directory, the functions that hold the lock RE-ENTRANTLY for the
// whole test process: only the first caller sends the statement, every later one -- while any is
// alive -- counts itself in, and the last to end releases it, so a caller already holding it never
// asks again (internal/handler's holdPanelTablesLock, M14-09: every panel harness takes it, and a test
// builds two harnesses or one inside a subtest). A call TO one is therefore not a site of the lock,
// so its callers are not checked by the once-per-tree rule; in exchange the directory's statement
// may be reached from NOTHING ELSE: every function that reaches the lock there must be a statement
// function of tablesLockStatements or a re-entrant holder (TestTablesLock_IsTakenOncePerTestTree).
var tablesLockReentrant = map[string][]string{
	"../../internal/handler": {"holdPanelTablesLock"},
	// M14-09's audit: every test that commits an admin row (newAdminRow) or an identity (ownerConn).
	"../../internal/adminauth": {"holdTablesLock"},
}

// tablesLockRowHolders are, per directory, the re-entrant holders whose call HOLDS the lock from that
// call until the test ends -- or until the release it returns is called. What else holds is DERIVED
// (rowHoldersOf): a function that calls a holder at its own top level and never releases holds through
// it for the whole test (internal/handler's panel harnesses through newPanelHarnessWith, internal/adminauth's
// ownerConn). A helper that holds it only around its own write and releases it (internal/adminauth's
// newAdminRow) is NOT one -- a test that called it holds nothing afterwards (M14 test-infra card, the
// audit's finding: TestNoticeRecipient_IsTheAddressOnTheRowAndNothingElse committed a raw admin row after
// newAdminRow had released). TestTablesLock_AdminRowsAreWrittenUnderTheHolder reads it. internal/handler
// since the second test-infra card: its panel harnesses commit admin rows, and so did six fixtures
// that held nothing. The third test-infra card added the two other directories that take the lock (an
// audit's finding: their raw writes were read by nothing): internal/handler/operator, whose newE2E
// takes it on its own connection for the whole test (op8, op12 and op15cross's fixtures write admin
// rows under it), and internal/domain/signup, whose sharedTablesLock holds it until its connection
// closes (identityOwner, and the tests built on it). Neither is re-entrant -- the rule does not need
// it: what holds is derived from the holder's call alone.
var tablesLockRowHolders = map[string][]string{
	"../../internal/adminauth":        {"holdTablesLock"},
	"../../internal/handler":          {"holdPanelTablesLock"},
	"../../internal/handler/operator": {"newE2E"},
	"../../internal/domain/signup":    {"sharedTablesLock"},
}

// tablesLockRowOutside are the test directories of the repository with a raw write of
// tablesLockRowTables that tablesLockRowHolders does not read, each with its reason (the third
// test-infra card, an audit's finding: "a directory outside tablesLockRowHolders is not read" was a
// blind spot; it is now a CLOSED inventory -- TestTablesLock_AdminRowsAreWrittenUnderTheHolder walks
// every test directory, and one with such a write must be in tablesLockRowHolders or here). Measured
// 2026-10-27 on m14-identity @ ba673e12. Two kinds, each checked (tablesLockOutsideFlaws):
//
//   - rowOutsideExclusive: internal/db, the EXCLUSIVE side itself. bfTx's tests run there, and a test
//     binary runs its sequential tests one at a time and its parallel ones only after all of them, so
//     nothing else in it writes while one runs. Checked: no function that calls bfTx calls
//     .Parallel.
//   - rowOutsidePlain: directories that take no session lock and commit only PLAIN rows -- every write
//     is one complete INSERT INTO admin_users (...) literal that names no link column (identity_id,
//     linked_via, linked_at, unlinked_at) and no ON CONFLICT: a fresh row with no identity, which
//     00073's closing assertion and its Down never count -- and no such row COMMITS while a bfTx test
//     runs: bfTx holds admin_users IN EXCLUSIVE MODE for the whole test, so a plain INSERT elsewhere
//     waits for it (closed by the third test-infra card's audit, measured: without that lock
//     TestAdminBackfill00073_TheClosingAssertion beside a ~200/s plain INSERT loop failed 8 runs of 8,
//     with it 8 of 8 passed, and every TestAdminBackfill00073_ test passed under the same load). The shape
//     is still checked: an UPDATE, an upsert or a link written outside the holder is a different
//     question from a fresh row's commit. A write text that is no statement (a message, a scanner's
//     input) is named in tablesLockRowTexts.
type rowOutsideKind int

const (
	rowOutsideExclusive rowOutsideKind = iota + 1
	rowOutsidePlain
)

var tablesLockRowOutside = map[string]rowOutsideKind{
	"../../internal/db": rowOutsideExclusive,
	// The api rig's business (surface_db_test.go) commits its owner through the application pool, no
	// lock: its pg_advisory_xact_lock_shared is asOperator's and operatorSession's, around the operator
	// tables only.
	"../../internal/handler/api":    rowOutsidePlain,
	"../../internal/domain/billing": rowOutsidePlain,
	"../../internal/domain/checkin": rowOutsidePlain,
	// main's M15-16 (brought into M14 before Deploy 1, 2026-10-27): claim_db_test.go's world commits
	// its owner, and the forged-reviewer test a manager, each one plain INSERT INTO admin_users.
	"../../internal/domain/claim":      rowOutsidePlain,
	"../../internal/domain/ledger":     rowOutsidePlain,
	"../../internal/domain/legal":      rowOutsidePlain,
	"../../internal/domain/manual":     rowOutsidePlain,
	"../../internal/domain/tenant":     rowOutsidePlain,
	"../../internal/apikey":            rowOutsidePlain,
	"../../internal/egress":            rowOutsidePlain,
	"../../internal/encode":            rowOutsidePlain,
	"../../internal/invite":            rowOutsidePlain,
	"../../internal/sandbox":           rowOutsidePlain,
	"../../internal/webhook/endpoints": rowOutsidePlain,
	"../../cmd/tappa":                  rowOutsidePlain,
	"../../cmd/opadmin":                rowOutsidePlain,
	"../../web/templates/email":        rowOutsidePlain,
}

// tablesLockRowTexts are, per directory of tablesLockRowOutside, the functions whose write-shaped
// literals are no statement sent to a database, each with its reason. Checked: the function is there,
// the scan sees a write in it, and neither it nor anything of its package it calls sends SQL
// (rowSendsSQL).
var tablesLockRowTexts = map[string]map[string]string{
	"../../cmd/opadmin": {
		"TestTablesLock_AdminRowsAreWrittenUnderTheHolder": "this scan's own positive controls: Go sources it parses",
	},
	"../../cmd/tappa": {
		"insertScopeFindings":                             "a finding's label (name + \": INSERT INTO \" + table) of a scanner over db/queries",
		"TestRedline_R4R7R7bR7dCatchDeliberateViolations": "\"UPD\" + \"ATE\": the redline script's planted violation, spelled at run time",
	},
	"../../internal/domain/tenant": {
		"insertFindings": "Sprintf messages of a scanner over db/queries (\"has an INSERT into %s ...\")",
	},
	"../../web/templates/email": {
		"seedNames": "a regexp over test/fixtures/seed.sql (`INSERT INTO ` + table)",
	},
}

// tablesLockRowTables are the tables a raw write of which (INSERT, UPDATE, DELETE) changes what
// internal/db's EXCLUSIVE backfill tests read whole (bfTx, READ COMMITTED: each statement sees what
// committed before it): a row of one written past the holder is the M14-09 deadlock -- or a closing
// assertion that counts another package's row.
var tablesLockRowTables = []string{"admin_users"}

// tablesLockRowExempt are, per directory, the functions that write a row of tablesLockRowTables without
// the holder, each with the reason. CLOSED and checked: an entry names a function of the directory
// that does write one AND sends a transaction-scoped lock statement itself (a pg_advisory_xact_lock
// literal) -- the one shape exempted: every write on one owner transaction that holds the lock for
// itself and is rolled back, so nothing is ever committed for bfTx to see. Calling the re-entrant holder
// there instead would be the deadlock TestTablesLock_IsTakenOncePerTestTree is about (a second request
// on a second connection while the first is held).
var tablesLockRowExempt = map[string]map[string]string{
	"../../internal/handler": {
		"TestIdentityAudit_TheDefinersRowsAreTheOrdinaryOnes":           "one owner transaction under pg_advisory_xact_lock_shared, rolled back: nothing is committed",
		"TestIdentityResetAudit_ALinkedRowsRecoveryRowIsTheHandlersRow": "one owner transaction per subtest under pg_advisory_xact_lock_shared, rolled back: nothing is committed",
	},
}

// lockSite is one place a function asks for the lock: the statement itself (via ""),
// or a call to a function that reaches it.
type lockSite struct {
	at    token.Position
	via   string
	where string // "" at the function's own top level, else what encloses the call
}

// isTablesLockCall: a call that sends a pg_advisory_lock… statement with
// operatorTablesTestLock as an argument (shared or exclusive; unlock is not a match).
func isTablesLockCall(c *ast.CallExpr) bool {
	var sql, key bool
	for _, a := range c.Args {
		switch x := a.(type) {
		case *ast.BasicLit:
			sql = sql || (x.Kind == token.STRING && strings.Contains(x.Value, "pg_advisory_lock"))
		case *ast.Ident:
			key = key || x.Name == "operatorTablesTestLock"
		}
	}
	return sql && key
}

// funcKey names a declaration the way calleeKey names a call: a method by ".name"
// (matched on the selector alone).
func funcKey(fd *ast.FuncDecl) string {
	if fd.Recv != nil {
		return "." + fd.Name.Name
	}
	return fd.Name.Name
}

func calleeKey(c *ast.CallExpr) string {
	switch f := c.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return "." + f.Sel.Name
	}
	return ""
}

// enclosing names the innermost construct of stack (stack[0] is the function body)
// that can run a call more than once or outside the function's own top level.
func enclosing(stack []ast.Node) string {
	for i := len(stack) - 1; i >= 1; i-- {
		switch stack[i].(type) {
		case *ast.FuncLit:
			return "a func literal"
		case *ast.ForStmt, *ast.RangeStmt:
			return "a loop"
		case *ast.GoStmt:
			return "a go statement"
		case *ast.DeferStmt:
			return "a defer statement"
		}
	}
	return ""
}

func sitesIn(fset *token.FileSet, body *ast.BlockStmt, takers map[string]bool) []lockSite {
	var sites []lockSite
	var stack []ast.Node
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if c, ok := n.(*ast.CallExpr); ok {
			if isTablesLockCall(c) {
				sites = append(sites, lockSite{at: fset.Position(c.Pos()), where: enclosing(stack)})
			} else if k := calleeKey(c); takers[k] {
				sites = append(sites, lockSite{at: fset.Position(c.Pos()), via: k, where: enclosing(stack)})
			}
		}
		stack = append(stack, n)
		return true
	})
	return sites
}

// tablesLockSites returns, for every function of one package (files) that reaches the
// lock -- directly or through other functions of the package -- the sites in its body. A call to
// a re-entrant holder (reentrant, tablesLockReentrant) is not a site: the holder is listed with its
// own sites, and nothing reaches the lock through it.
func tablesLockSites(fset *token.FileSet, files []*ast.File, reentrant []string) map[string][]lockSite {
	decls := map[string]*ast.FuncDecl{}
	for _, f := range files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				decls[funcKey(fd)] = fd
			}
		}
	}
	takers := map[string]bool{}
	// through is takers without the re-entrant holders: what a call can reach the lock through.
	through := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for k, fd := range decls {
			if !takers[k] && len(sitesIn(fset, fd.Body, through)) > 0 {
				takers[k], changed = true, true
				if !slices.Contains(reentrant, k) {
					through[k] = true
				}
			}
		}
	}
	out := map[string][]lockSite{}
	for k := range takers {
		out[k] = sitesIn(fset, decls[k].Body, through)
	}
	return out
}

// tablesLockOutsiders: with re-entrant holders, every function reaching the lock other than the
// statement functions and the holders themselves -- a call that bypasses the holder.
func tablesLockOutsiders(sites map[string][]lockSite, statements, reentrant []string) []string {
	if len(reentrant) == 0 {
		return nil
	}
	var bad []string
	for fn := range sites {
		if !slices.Contains(statements, fn) && !slices.Contains(reentrant, fn) {
			bad = append(bad, fmt.Sprintf("%s reaches the lock outside the re-entrant holder %v", fn, reentrant))
		}
	}
	sort.Strings(bad)
	return bad
}

// tablesLockFindings: a function that reaches the lock more than once, or from inside
// a func literal (a subtest, a cleanup), a loop, a go or a defer statement.
func tablesLockFindings(sites map[string][]lockSite) []string {
	var bad []string
	for fn, ss := range sites {
		if len(ss) > 1 {
			var at []string
			for _, s := range ss {
				at = append(at, fmt.Sprintf("line %d via %q", s.at.Line, s.via))
			}
			bad = append(bad, fmt.Sprintf("%s reaches the lock %d times (%s)", fn, len(ss), strings.Join(at, ", ")))
		}
		for _, s := range ss {
			if s.where != "" {
				bad = append(bad, fmt.Sprintf("%s reaches the lock inside %s (line %d via %q)", fn, s.where, s.at.Line, s.via))
			}
		}
	}
	sort.Strings(bad)
	return bad
}

// parsePackages parses dir's _test.go files, grouped by package clause (a directory
// can hold an external _test package next to the internal one).
func parsePackages(t *testing.T, fset *token.FileSet, dir string) map[string][]*ast.File {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil || len(names) == 0 {
		t.Fatalf("%s: no test files (%v)", dir, err)
	}
	pkgs := map[string][]*ast.File{}
	for _, n := range names {
		f, err := parser.ParseFile(fset, n, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", n, err)
		}
		pkgs[f.Name.Name] = append(pkgs[f.Name.Name], f)
	}
	return pkgs
}

// TestTablesLock_IsTakenOncePerTestTree -- written for CI run 37084001714 (2026-10-03),
// where `go test -race ./...` lost TestApply_AFailureAnywhereLeavesNoRow (its reset-mfa
// subtest) and internal/db's TestOpRecordAuthEvent_ClosedSetNoActorNoAddress to a
// 3-minute timeout each.
//
// PART I -- MEASURED (2026-10-03, development PostgreSQL). That test held the lock SHARED
// on one connection and its reset-mfa subtest asked for it again, through ownerTx, on a
// second one. A SHARED request queues behind a waiting EXCLUSIVE one, and internal/db's
// opTx asks EXCLUSIVE from the test binary `go test ./...` runs in parallel: the subtest
// waited on opTx, opTx waited on the parent's connection, and the parent waits for its
// subtest -- a cycle through the test process that PostgreSQL's deadlock detector cannot
// see. Reproduced by queueing an EXCLUSIVE request the moment the test's SHARED lock was
// granted: pg_locks showed the parent's ShareLock granted, the EXCLUSIVE request blocked
// by it and the subtest's ShareLock blocked by the EXCLUSIVE request; with a 180 s
// lock_timeout on that request the subtest failed exactly as on CI (at c6d8ef4,
// "opadmin_db_test.go:828: timeout: context already done", 184.67 s). With the subtest on
// beginOwnerTx (no second request) the same probe left the test green in 5.43 s (6.14 s
// without the probe) and the EXCLUSIVE request was granted when the test ended.
//
// PART II -- PINNED HERE. In the _test.go files of tablesLockDirs, every function -- a
// test or a helper -- reaches the lock at most ONCE, and never from inside a func
// literal (a subtest, a cleanup), a loop, a go or a defer statement; reaching it means
// sending the statement or calling a function of the same package that does
// (transitively). The statement itself is in exactly tablesLockStatements' functions:
// cmd/opadmin sharedTablesLock (ownerTx; TestApply_AFailureAnywhereLeavesNoRow), internal/db
// opTx (EXCLUSIVE), TestOperatorDB_RunsAsTappaOperator and opLiveFixture (SHARED; OP-10's
// three tests whose subject is a commit), internal/handler/operator newE2E,
// internal/operatorauth sharedTablesLock (ownerTx, ownerPool). Measured when written: the
// one finding was the subtest above; after the fix, none. POSITIVE CONTROL: the same scan
// finds the CI shape, a function that asks twice and one that asks in a loop -- and not
// one whose subtest asks nothing, nor an unlock -- in a source written here.
//
// THE ONE EXCEPTION, M14-09: a directory may name a RE-ENTRANT holder (tablesLockReentrant --
// internal/handler's holdPanelTablesLock, which every panel harness takes, and internal/adminauth's
// holdTablesLock, which every committed admin row and identity world takes). It sends the statement
// only while nothing in the process holds the lock, so a test that already holds it never asks
// again; calls to it are not sites, and in exchange nothing else in that directory may reach the
// statement (tablesLockOutsiders). Its own positive control is the second source below.
//
// PART III -- NO COMPLETENESS CLAIM. The scan is syntactic: a lock reached through a
// function value, an interface, a helper in another package or a statement built at run
// time is not seen, a method is matched by its name alone, and a package outside
// tablesLockDirs is not read.
func TestTablesLock_IsTakenOncePerTestTree(t *testing.T) {
	const control = `package p
func lock(t *testing.T) { conn.Exec(ctx, "SELECT pg_advisory_lock_shared(hashtext($1))", operatorTablesTestLock) }
func tx(t *testing.T) { lock(t) }
func nested(t *testing.T) { lock(t); t.Run("sub", func(t *testing.T) { tx(t) }) }
func twice(t *testing.T) { tx(t); tx(t) }
func looped(t *testing.T) { for range 2 { tx(t) } }
func once(t *testing.T) { tx(t); t.Run("sub", func(t *testing.T) { use(t) }) }
func unlocks(t *testing.T) { tx(t); conn.Exec(ctx, "SELECT pg_advisory_unlock_shared(hashtext($1))", operatorTablesTestLock) }
`
	cfset := token.NewFileSet()
	cf, err := parser.ParseFile(cfset, "control.go", control, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	got := tablesLockFindings(tablesLockSites(cfset, []*ast.File{cf}, nil))
	want := []string{
		`looped reaches the lock inside a loop (line 6 via "tx")`,
		`nested reaches the lock 2 times (line 4 via "lock", line 4 via "tx")`,
		`nested reaches the lock inside a func literal (line 4 via "tx")`,
		`twice reaches the lock 2 times (line 5 via "tx", line 5 via "tx")`,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("POSITIVE CONTROL FAILED: the scan reports\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// THE RE-ENTRANT HOLDER'S CONTROL (M14-09): callers of the holder -- twice, in a subtest -- are
	// no finding, and a function that ALSO reaches the statement past the holder is an outsider.
	const reentrantControl = `package p
func lock(t *testing.T) { conn.Exec(ctx, "SELECT pg_advisory_lock_shared(hashtext($1))", operatorTablesTestLock) }
func hold(t *testing.T) { if first { lock(t) } }
func harness(t *testing.T) { hold(t) }
func two(t *testing.T) { harness(t); harness(t); t.Run("sub", func(t *testing.T) { harness(t) }) }
func bypass(t *testing.T) { harness(t); lock(t) }
`
	rf, err := parser.ParseFile(cfset, "reentrant.go", reentrantControl, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	rsites := tablesLockSites(cfset, []*ast.File{rf}, []string{"hold"})
	got = append(tablesLockFindings(rsites), tablesLockOutsiders(rsites, []string{"lock"}, []string{"hold"})...)
	want = []string{"bypass reaches the lock outside the re-entrant holder [hold]"}
	if !slices.Equal(got, want) {
		t.Fatalf("POSITIVE CONTROL FAILED (re-entrant holder): the scan reports\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	fset := token.NewFileSet()
	for _, dir := range tablesLockDirs {
		var holders, tests []string
		for _, files := range parsePackages(t, fset, dir) {
			sites := tablesLockSites(fset, files, tablesLockReentrant[dir])
			for _, f := range tablesLockFindings(sites) {
				t.Errorf("%s: %s", dir, f)
			}
			for _, f := range tablesLockOutsiders(sites, tablesLockStatements[dir], tablesLockReentrant[dir]) {
				t.Errorf("%s: %s", dir, f)
			}
			for fn, ss := range sites {
				if strings.HasPrefix(fn, "Test") {
					tests = append(tests, fn)
				}
				if len(ss) == 1 && ss[0].via == "" {
					holders = append(holders, fn)
				}
			}
		}
		sort.Strings(holders)
		if !slices.Equal(holders, tablesLockStatements[dir]) {
			t.Errorf("%s: the lock statement is in %v, want %v -- a new site is a new tree to check (or the scan went blind)", dir, holders, tablesLockStatements[dir])
		}
		t.Logf("%s: the statement in %v; %d tests reach it", dir, holders, len(tests))
	}
}

// rowOpensWrite: a text (normSQL's form) that leaves the table of a write open -- it ends in a write
// verb ("UPDATE " + name) or carries a format slot right after one ("INSERT INTO %s"). The third
// test-infra card added UPDATE ONLY, DELETE FROM ONLY, MERGE INTO, TRUNCATE and COPY (an audit's
// finding: each wrote a row unseen); TRUNCATE and COPY only where a statement starts -- "the Go copy
// %06X" is a message.
var rowOpensWrite = regexp.MustCompile(`(^|[^a-z0-9_$])(insert into|update( only)?|delete from( only)?|merge into)( %|$)|(^|;) ?(copy|truncate( table)?( only)?)( %|$)`)

// normSQL: s with its SQL comments dropped, lower-cased, white space collapsed to one space, the double
// quotes of a quoted identifier dropped and every qualifier chain before a name dropped -- so
// `INSERT INTO public."admin_users"` (the second test-infra card's audit, O-1), `UPDATE /* c */
// admin_users`, `UPDATE public . admin_users` and `UPDATE tappa.public.admin_users` (the third's) all
// read as the plain spelling. A qualifier before a format slot is dropped for public. alone.
func normSQL(s string) string {
	s = sqlComment.ReplaceAllString(s, " ")
	s = strings.ToLower(strings.Join(strings.Fields(s), " "))
	s = strings.ReplaceAll(s, `"`, "")
	s = sqlDot.ReplaceAllString(s, ".")
	s = strings.ReplaceAll(s, "public.", "")
	// Any other qualifier only before a name: before a format slot ("may UPDATE tenants.%s", a test's
	// message) it would turn a message into an open write.
	for t := ""; t != s; {
		t, s = s, sqlQualifier.ReplaceAllString(s, "$1")
	}
	return s
}

var (
	sqlComment   = regexp.MustCompile(`(?s)/\*.*?\*/|--[^\n]*`)
	sqlDot       = regexp.MustCompile(`\s*\.\s*`)
	sqlQualifier = regexp.MustCompile(`\b[a-z_][a-z0-9_$]*\.([a-z_])`)
)

// litText is a string literal's value (its source text if it does not unquote).
func litText(lit *ast.BasicLit) string {
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		s = lit.Value
	}
	return s
}

// writesRowSQL: s writes a row of one of tables -- INSERT INTO, UPDATE [ONLY], DELETE FROM [ONLY],
// MERGE INTO, COPY (either way: fail closed), or TRUNCATE naming it anywhere in its list. A table name
// that ends s counts, so a statement built by concatenation is still seen.
func writesRowSQL(s string, tables []string) bool {
	norm := normSQL(s)
	for _, tbl := range tables {
		for _, re := range rowWriteREs(tbl) {
			if re.MatchString(norm) {
				return true
			}
		}
	}
	return false
}

var rowWriteRECache sync.Map // table -> []*regexp.Regexp

// rowWriteREs: writesRowSQL's two patterns for tbl, over normSQL's form.
func rowWriteREs(tbl string) []*regexp.Regexp {
	if v, ok := rowWriteRECache.Load(tbl); ok {
		return v.([]*regexp.Regexp)
	}
	t := regexp.QuoteMeta(tbl)
	res := []*regexp.Regexp{
		regexp.MustCompile(`(^|[^a-z0-9_$])(insert into|update( only)?|delete from( only)?|merge into|copy) ` + t + `([^a-z0-9_$]|$)`),
		regexp.MustCompile(`(^|[^a-z0-9_$])truncate( table)?( only)? ([a-z0-9_$]+\*? ?, ?(only )?)*` + t + `([^a-z0-9_$]|$)`),
	}
	rowWriteRECache.Store(tbl, res)
	return res
}

// writesRow: lit is a string literal that writes a row of one of tables on its own (writesRowSQL).
func writesRow(lit *ast.BasicLit, tables []string) bool {
	return lit.Kind == token.STRING && writesRowSQL(litText(lit), tables)
}

// opensWriteSQL: s leaves the table of a write open -- it ends in a write verb ("UPDATE " + name) or
// carries a format slot right after one ("INSERT INTO %s").
func opensWriteSQL(s string) bool {
	return rowOpensWrite.MatchString(normSQL(s))
}

// strLit: e, parentheses dropped, is a string literal.
func strLit(e ast.Expr) (*ast.BasicLit, bool) {
	l, ok := ast.Unparen(e).(*ast.BasicLit)
	return l, ok && l.Kind == token.STRING
}

// addChain: the operands of the outermost + chain lit stands in (stack: lit's ancestors, its parent
// last), parentheses dropped and the chain flattened -- `p + "UPDATE " + t` and `("UPDATE ") + t` alike --
// and lit's index there; nil when lit is in no +.
func addChain(lit *ast.BasicLit, stack []ast.Node) ([]ast.Expr, int) {
	var root *ast.BinaryExpr
	for i := len(stack) - 1; i >= 0; i-- {
		if _, ok := stack[i].(*ast.ParenExpr); ok {
			continue
		}
		b, ok := stack[i].(*ast.BinaryExpr)
		if !ok || b.Op != token.ADD {
			break
		}
		root = b
	}
	if root == nil {
		return nil, -1
	}
	var flat func(e ast.Expr, out []ast.Expr) []ast.Expr
	flat = func(e ast.Expr, out []ast.Expr) []ast.Expr {
		e = ast.Unparen(e)
		if b, ok := e.(*ast.BinaryExpr); ok && b.Op == token.ADD {
			return flat(b.Y, flat(b.X, out))
		}
		return append(out, e)
	}
	ops := flat(root, nil)
	return ops, slices.Index(ops, ast.Expr(lit))
}

// isIdentifierLit: c is a pgx.Identifier composite literal (by the type's name alone).
func isIdentifierLit(c *ast.CompositeLit) bool {
	switch t := c.Type.(type) {
	case *ast.Ident:
		return t.Name == "Identifier"
	case *ast.SelectorExpr:
		return t.Sel.Name == "Identifier"
	}
	return false
}

// writesAt: n, with its ancestors on stack (its parent last), is a write of a row of tables. A string
// literal is one on its own (writesRow), or as the open half of one (opensWriteSQL) in the two places a
// statement is assembled:
//   - a + chain (parentheses dropped, any position -- the third test-infra card, an audit's finding:
//     `prefix + "UPDATE " + tbl` and `("UPDATE ") + tbl` slipped): the literal and the literals right
//     after it, joined, write the row, or leave a write open and are followed by a non-literal or end
//     the chain;
//   - the format of a Sprintf whose arguments are not all literals naming none of tables.
//
// A pgx.Identifier composite literal (CopyFrom's table) is one when its parts, joined with ".", name
// one of tables, or when a part is not a literal. A bare "UPDATE" anywhere else (a test case's label) is
// not one. Fail closed: what cannot be resolved syntactically counts as a write.
func writesAt(n ast.Node, stack []ast.Node, tables []string) bool {
	// pgx's CopyFrom(ctx, table, columns, rows) whose table is not a pgx.Identifier literal the scan can
	// read (that one is judged as a node of its own, below): []string{...}, a conversion, a variable --
	// fail closed (the third test-infra card's audit). pgconn's CopyFrom(ctx, r, sql) has three
	// arguments and is judged by its SQL literal.
	if c, ok := n.(*ast.CallExpr); ok {
		if calleeKey(c) != ".CopyFrom" || len(c.Args) != 4 {
			return false
		}
		if id, ok := ast.Unparen(c.Args[1]).(*ast.CompositeLit); ok && isIdentifierLit(id) {
			return false
		}
		return true
	}
	if c, ok := n.(*ast.CompositeLit); ok {
		if !isIdentifierLit(c) {
			return false
		}
		var parts []string
		for _, e := range c.Elts {
			l, ok := strLit(e)
			if !ok {
				return true
			}
			parts = append(parts, litText(l))
		}
		return slices.Contains(tables, normSQL(strings.Join(parts, ".")))
	}
	lit, ok := n.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	if writesRow(lit, tables) {
		return true
	}
	if ops, i := addChain(lit, stack); i >= 0 {
		text, j := "", i
		for ; j < len(ops); j++ {
			l, ok := strLit(ops[j])
			if !ok {
				break
			}
			text += litText(l)
		}
		return writesRowSQL(text, tables) || opensWriteSQL(text)
	}
	var parent ast.Node
	for i := len(stack) - 1; i >= 0; i-- {
		if _, ok := stack[i].(*ast.ParenExpr); !ok {
			parent = stack[i]
			break
		}
	}
	p, ok := parent.(*ast.CallExpr)
	if !ok {
		return false
	}
	if k := calleeKey(p); (k != ".Sprintf" && k != "Sprintf") || len(p.Args) == 0 || ast.Unparen(p.Args[0]) != ast.Expr(lit) {
		return false
	}
	// The format, rendered: a literal argument stands in its slot, any other as "%" -- so the verb or
	// the table may be in an argument (`Sprintf("%s admin_users SET ...", "UPDATE")`, the third
	// test-infra card's audit). A write is one rendered whole, a write left open ("UPDATE %"), or an
	// unknown verb before a table ("% admin_users").
	text := renderSprintf(litText(lit), p.Args[1:])
	if writesRowSQL(text, tables) || opensWriteSQL(text) {
		return true
	}
	norm := normSQL(text)
	for _, tbl := range tables {
		if regexp.MustCompile(`(^|[^a-z0-9_$])% ` + regexp.QuoteMeta(tbl) + `([^a-z0-9_$]|$)`).MatchString(norm) {
			return true
		}
	}
	return false
}

// sprintfVerb is a fmt verb (flags, width and precision included) or "%%".
var sprintfVerb = regexp.MustCompile(`%%|%[-+# 0-9.*]*[a-zA-Z]`)

// renderSprintf: format with each verb replaced by its argument when that is a string literal, by "%"
// otherwise (and when the arguments run out); "%%" stays.
func renderSprintf(format string, args []ast.Expr) string {
	i := 0
	return sprintfVerb.ReplaceAllStringFunc(format, func(v string) string {
		if v == "%%" {
			return v
		}
		if i >= len(args) {
			return "%"
		}
		a := args[i]
		i++
		if l, ok := strLit(a); ok {
			return litText(l)
		}
		return "%"
	})
}

// rowWritesIn: the nodes under n that write a row of one of tables (writesAt).
func rowWritesIn(n ast.Node, tables []string) []token.Pos {
	var at []token.Pos
	var stack []ast.Node
	ast.Inspect(n, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if writesAt(n, stack, tables) {
			at = append(at, n.Pos())
		}
		stack = append(stack, n)
		return true
	})
	return at
}

// packageRowWrites: every write of a row of tables in a package-level declaration (a const or var) of
// files -- always a finding: the scan places a write by where its literal stands, and a package-level
// one stands in no function (O-1). Inline it.
func packageRowWrites(fset *token.FileSet, files []*ast.File, tables []string) []string {
	var bad []string
	for _, f := range files {
		for _, d := range f.Decls {
			if gd, ok := d.(*ast.GenDecl); ok {
				for _, at := range rowWritesIn(gd, tables) {
					bad = append(bad, fmt.Sprintf("a package-level declaration writes %v (%s:%d) -- inline it where it runs, so the scan can see under what",
						tables, filepath.Base(fset.Position(at).Filename), fset.Position(at).Line))
				}
			}
		}
	}
	sort.Strings(bad)
	return bad
}

// rowPoint is a place in a function body: a holder call (pos..end), a release, a call site or a
// write, with the constructs around it that decide whether it runs (rowScopes).
type rowPoint struct {
	pos, end token.Pos
	scopes   []ast.Node
	deferred bool
	// late is the outermost construct that makes the point run after the statements around it rather
	// than where it stands -- a defer or go statement, or a func literal handed to a Cleanup (rowLate);
	// nil for a point that runs in place.
	late ast.Node
}

// rowLate: the outermost defer or go statement, or func literal passed to a Cleanup, on stack.
func rowLate(stack []ast.Node) ast.Node {
	for i, n := range stack {
		switch n.(type) {
		case *ast.DeferStmt, *ast.GoStmt:
			return n
		case *ast.FuncLit:
			if i > 0 {
				if c, ok := stack[i-1].(*ast.CallExpr); ok && calleeKey(c) == ".Cleanup" {
					return n
				}
			}
		}
	}
	return nil
}

// rowScopes: the nodes of stack that bound where a point runs -- the blocks and clauses it is in, and a
// func literal, go or defer statement around it. stack[0] is the function body.
func rowScopes(stack []ast.Node) []ast.Node {
	var out []ast.Node
	for _, n := range stack {
		switch n.(type) {
		case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause, *ast.FuncLit, *ast.GoStmt, *ast.DeferStmt:
			out = append(out, n)
		}
	}
	return out
}

// dominates: hold has run, on every path, by the time at runs -- it ends before at starts and every
// scope it is in also encloses at (a hold in a branch or a loop body covers only that branch or body).
func (hold rowPoint) dominates(at rowPoint) bool {
	if hold.end > at.pos {
		return false
	}
	for _, s := range hold.scopes {
		if !slices.Contains(at.scopes, s) {
			return false
		}
	}
	return true
}

// rowFunc is what the row scan reads of one function.
type rowFunc struct {
	holds, releases, returns, writes []rowPoint
	calls                            map[string][]rowPoint // callee -> its call sites (at the call's ")")
	top                              *ast.BlockStmt
}

// readRowFunc reads fd against holders. A release is a call to a value a holder call was assigned to,
// or to a field or method named releaseTablesLock (the panel harness keeps its holder's release there).
func readRowFunc(fd *ast.FuncDecl, holders map[string]bool, tables []string) *rowFunc {
	rf := &rowFunc{calls: map[string][]rowPoint{}, top: fd.Body}
	bound := map[string]bool{}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if a, ok := n.(*ast.AssignStmt); ok && len(a.Lhs) == len(a.Rhs) {
			for i, r := range a.Rhs {
				if c, ok := r.(*ast.CallExpr); ok && holders[calleeKey(c)] {
					if id, ok := a.Lhs[i].(*ast.Ident); ok && id.Name != "_" {
						bound[id.Name] = true
					}
				}
			}
		}
		return true
	})
	var stack []ast.Node
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		point := func(pos, end token.Pos) rowPoint {
			_, deferred := stack[len(stack)-1].(*ast.DeferStmt)
			return rowPoint{pos: pos, end: end, scopes: rowScopes(stack), deferred: deferred, late: rowLate(stack)}
		}
		if writesAt(n, stack, tables) {
			rf.writes = append(rf.writes, point(n.Pos(), n.End()))
		}
		switch x := n.(type) {
		case *ast.ReturnStmt:
			rf.returns = append(rf.returns, point(x.Pos(), x.End()))
		case *ast.CallExpr:
			k := calleeKey(x)
			switch {
			case holders[k]:
				rf.holds = append(rf.holds, point(x.Pos(), x.End()))
			case k == ".releaseTablesLock":
				rf.releases = append(rf.releases, point(x.Pos(), x.End()))
			default:
				if id, ok := x.Fun.(*ast.Ident); ok && bound[id.Name] {
					rf.releases = append(rf.releases, point(x.Pos(), x.End()))
				}
			}
			if k != "" {
				rf.calls[k] = append(rf.calls[k], point(x.Rparen, x.Rparen))
			}
		}
		stack = append(stack, n)
		return true
	})
	return rf
}

// holdsForTheTest: rf calls a holder at its own top level (in no branch, loop, func literal, go or
// defer statement, and after no return), and releases nothing -- so once it has returned, its caller
// holds the lock until the test ends.
func (rf *rowFunc) holdsForTheTest() bool {
	if len(rf.releases) > 0 {
		return false
	}
	for _, h := range rf.holds {
		if len(h.scopes) != 1 || h.scopes[0] != rf.top {
			continue
		}
		early := false
		for _, r := range rf.returns {
			if r.end <= h.pos && !slices.ContainsFunc(r.scopes, func(s ast.Node) bool { _, ok := s.(*ast.FuncLit); return ok }) {
				early = true
			}
		}
		if !early {
			return true
		}
	}
	return false
}

// coveredAt: a holder call dominates at and no release (other than a deferred one) runs between them.
// A point that runs LATE relative to the hold -- in a defer or go statement or a Cleanup func literal the
// hold is not itself inside (rowLate), or reached from one (late, passed down the call chain) -- runs
// after the function's statements, so ANY release after the hold, deferred or not, comes before it
// (the second test-infra card's audit, D-1). Conservative: a deferred write registered after a deferred
// release (LIFO: it runs first) is still counted uncovered.
func (rf *rowFunc) coveredAt(at rowPoint, late bool) bool {
	for _, h := range rf.holds {
		if !h.dominates(at) {
			continue
		}
		after := late || (at.late != nil && !slices.Contains(h.scopes, at.late))
		released := false
		for _, r := range rf.releases {
			if r.pos > h.end && (after || (!r.deferred && r.pos < at.pos)) {
				released = true
			}
		}
		if !released {
			return true
		}
	}
	return false
}

// rowHoldersOf reads files' functions and derives, to a fixed point, which of them hold through
// holders for the whole test (holdsForTheTest); it returns every function read against the full set.
func rowHoldersOf(files []*ast.File, holders, tables []string) (map[string]*rowFunc, map[string]bool) {
	decls := map[string]*ast.FuncDecl{}
	for _, f := range files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				decls[funcKey(fd)] = fd
			}
		}
	}
	set := map[string]bool{}
	for _, h := range holders {
		set[h] = true
	}
	for changed := true; changed; {
		changed = false
		for k, fd := range decls {
			if !set[k] && readRowFunc(fd, set, tables).holdsForTheTest() {
				set[k], changed = true, true
			}
		}
	}
	funcs := map[string]*rowFunc{}
	for k, fd := range decls {
		funcs[k] = readRowFunc(fd, set, tables)
	}
	return funcs, set
}

// tablesLockRowOutsiders: every function of one package that writes a row of tables (a raw INSERT,
// UPDATE or DELETE literal) at a point not covered by a holder, except exempt's. A point of a function
// is covered when a holder call -- one of holders or a function derived from them (rowHoldersOf) --
// dominates it with no release in between, or, for a helper, when it has callers and every call to it
// is itself at a covered point of its caller (methods matched by name alone).
func tablesLockRowOutsiders(fset *token.FileSet, files []*ast.File, holders, tables []string, exempt map[string]string) []string {
	funcs, _ := rowHoldersOf(files, holders, tables)
	type call struct {
		caller string
		at     rowPoint
	}
	callers := map[string][]call{}
	for k, rf := range funcs {
		for callee, sites := range rf.calls {
			if callee != k {
				for _, s := range sites {
					callers[callee] = append(callers[callee], call{k, s})
				}
			}
		}
	}
	var covered func(fn string, at rowPoint, late bool, seen map[string]bool) bool
	covered = func(fn string, at rowPoint, late bool, seen map[string]bool) bool {
		if funcs[fn].coveredAt(at, late) {
			return true
		}
		// Not covered here: the callers must cover the call, and a point that runs late here runs late
		// for them too.
		late = late || at.late != nil
		cs := callers[fn]
		if len(cs) == 0 || seen[fn] || strings.HasPrefix(fn, "Test") {
			return false
		}
		seen[fn] = true
		defer delete(seen, fn)
		for _, c := range cs {
			if !covered(c.caller, c.at, late, seen) {
				return false
			}
		}
		return true
	}
	bad := packageRowWrites(fset, files, tables)
	for k, rf := range funcs {
		if slices.Contains(holders, k) || exempt[k] != "" {
			continue
		}
		for _, at := range rf.writes {
			if !covered(k, at, false, map[string]bool{}) {
				bad = append(bad, fmt.Sprintf("%s writes %v (line %d) outside the holder %v", k, tables, fset.Position(at.pos).Line, holders))
			}
		}
	}
	sort.Strings(bad)
	return bad
}

// xactLockStatements are the transaction-scoped lock statements an exemption may send (normSQL's form).
var xactLockStatements = map[string]bool{
	"select pg_advisory_xact_lock_shared(hashtext($1))": true,
	"select pg_advisory_xact_lock(hashtext($1))":        true,
}

// reportsText: c is a call that writes its arguments as text -- a test's log or report, a print --
// rather than sending them to the database.
func reportsText(c *ast.CallExpr) bool {
	k := strings.TrimPrefix(calleeKey(c), ".")
	for _, p := range []string{"Log", "Error", "Fatal", "Skip", "Print", "Sprint", "Errorf", "Info", "Warn", "Debug"} {
		if strings.HasPrefix(k, p) {
			return true
		}
	}
	return false
}

// exemptionFlaws: what fd -- a tablesLockRowExempt entry, rf its reading against the full holder set
// (rowHoldersOf) -- lacks of the one exempted shape (the second test-infra card's audit, D-2):
//   - it writes a row of tables;
//   - the statement SELECT pg_advisory_xact_lock[_shared](hashtext($1)), exactly, is the first argument
//     of a call that is not a log or a report (sent, not a text: reportsText) and stands before every
//     write;
//   - it calls .Rollback (nothing is committed);
//   - it calls no holder, nor anything that holds through one: a second lock on a second connection
//     is the deadlock TestTablesLock_IsTakenOncePerTestTree is about;
//   - (the third test-infra card, an audit's finding) it reaches the session lock statement through NO
//     call path at all -- reaches, its sites in tablesLockSites read with no re-entrant holder, so a call
//     to the holder is a site too. A fixture that holds the lock and releases it (internal/handler's
//     brandBusiness) is no derived holder, so the rule above missed it, yet it asks for the lock again
//     on a second connection while this function's transaction holds it.
func exemptionFlaws(fd *ast.FuncDecl, rf *rowFunc, reaches []lockSite) []string {
	var flaws []string
	if len(rf.writes) == 0 {
		flaws = append(flaws, "it writes no row")
	}
	lock := token.NoPos
	rollback := false
	var stack []ast.Node
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		switch x := n.(type) {
		case *ast.BasicLit:
			if c, ok := stack[len(stack)-1].(*ast.CallExpr); ok && len(c.Args) > 0 && c.Args[0] == ast.Expr(x) &&
				xactLockStatements[normSQL(litText(x))] && !reportsText(c) && (lock == token.NoPos || x.Pos() < lock) {
				lock = x.Pos()
			}
		case *ast.CallExpr:
			rollback = rollback || calleeKey(x) == ".Rollback"
		}
		stack = append(stack, n)
		return true
	})
	if lock == token.NoPos {
		flaws = append(flaws, "it sends no SELECT pg_advisory_xact_lock statement")
	} else {
		for _, w := range rf.writes {
			if w.pos < lock {
				flaws = append(flaws, "a write stands before its transaction-scoped lock")
				break
			}
		}
	}
	if !rollback {
		flaws = append(flaws, "it calls no Rollback")
	}
	if len(rf.holds) > 0 {
		flaws = append(flaws, "it also calls a holder (a second lock on a second connection)")
	}
	if len(reaches) > 0 {
		var via []string
		for _, r := range reaches {
			if r.via == "" {
				r.via = "the statement itself"
			}
			if !slices.Contains(via, r.via) {
				via = append(via, r.via)
			}
		}
		sort.Strings(via)
		flaws = append(flaws, fmt.Sprintf("it reaches the session lock through %s (a second lock on a second connection)", strings.Join(via, ", ")))
	}
	return flaws
}

// reachers: the functions of files that make a call match accepts, directly or through other functions
// of files (methods matched by name alone).
func reachers(files []*ast.File, match func(*ast.CallExpr) bool) map[string]bool {
	decls := map[string]*ast.FuncDecl{}
	for _, f := range files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				decls[funcKey(fd)] = fd
			}
		}
	}
	set := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for k, fd := range decls {
			if set[k] {
				continue
			}
			found := false
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok && (match(c) || set[calleeKey(c)]) {
					found = true
				}
				return !found
			})
			if found {
				set[k], changed = true, true
			}
		}
	}
	return set
}

// sendsSQL: c hands SQL to a database -- pgx's and internal/db's calls, by method name.
func sendsSQL(c *ast.CallExpr) bool {
	switch calleeKey(c) {
	case ".Exec", ".Query", ".QueryRow", ".CopyFrom", ".SendBatch", ".Begin", ".BeginTx", ".WithTenant":
		return true
	}
	return false
}

// rowLinkColumns: what a plain row does not name -- the link columns, and an upsert. rowPlainInsert: the
// statement's start, its table captured.
var (
	rowLinkColumns = regexp.MustCompile(`identity_id|linked_via|linked_at|unlinked_at|on conflict`)
	rowPlainInsert = regexp.MustCompile(`^insert into ([a-z0-9_$]+) ?\(`)
)

// plainInsert: n is ONE string literal that inserts a fresh row of one of tables and names no link
// column and no ON CONFLICT (rowOutsidePlain).
func plainInsert(n ast.Node, tables []string) bool {
	lit, ok := n.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	norm := normSQL(litText(lit))
	// One statement: a ";" other than a trailing one opens a second (the third test-infra card's audit).
	if rowLinkColumns.MatchString(norm) || strings.Contains(strings.TrimRight(norm, "; "), ";") {
		return false
	}
	m := rowPlainInsert.FindStringSubmatch(norm)
	return m != nil && slices.Contains(tables, m[1])
}

// tablesLockOutsideFlaws: what one package (files) of a tablesLockRowOutside directory lacks of its
// kind's shape; texts is the directory's tablesLockRowTexts, and seen collects the text functions found.
func tablesLockOutsideFlaws(fset *token.FileSet, files []*ast.File, kind rowOutsideKind, tables []string, texts map[string]string, seen map[string]bool) []string {
	var bad []string
	switch kind {
	case rowOutsideExclusive:
		// bfTx, bfTxForDDL and the bfTxLocking both open with: every way into the EXCLUSIVE transaction.
		bf := reachers(files, func(c *ast.CallExpr) bool { return strings.HasPrefix(calleeKey(c), "bfTx") })
		for _, f := range files {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil || !bf[funcKey(fd)] {
					continue
				}
				// Its own body, func literals (subtests) included: a .Parallel there runs it beside others.
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					if c, ok := n.(*ast.CallExpr); ok && calleeKey(c) == ".Parallel" {
						bad = append(bad, fmt.Sprintf("%s reaches bfTx and calls .Parallel (line %d): it runs beside the package's other tests", funcKey(fd), fset.Position(c.Pos()).Line))
					}
					return true
				})
			}
		}
	case rowOutsidePlain:
		// A directory that takes the session lock AND writes rows belongs in tablesLockRowHolders; one
		// whose every write is a text (cmd/opadmin: this scan's controls) does not.
		statements := 0
		var lockAt []string
		for fn, ss := range tablesLockSites(fset, files, nil) {
			for _, s := range ss {
				if s.via == "" {
					lockAt = append(lockAt, fmt.Sprintf("%s sends the session lock statement (line %d): a directory that takes it belongs in tablesLockRowHolders", fn, s.at.Line))
				}
			}
		}
		sends := reachers(files, sendsSQL)
		bad = append(bad, packageRowWrites(fset, files, tables)...)
		for _, f := range files {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				k := funcKey(fd)
				var writes []ast.Node
				var stack []ast.Node
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					if n == nil {
						stack = stack[:len(stack)-1]
						return true
					}
					if writesAt(n, stack, tables) {
						writes = append(writes, n)
					}
					stack = append(stack, n)
					return true
				})
				if texts[k] != "" {
					seen[k] = true
					if len(writes) == 0 {
						bad = append(bad, fmt.Sprintf("%s is named a text but the scan sees no write in it", k))
					}
					if sends[k] {
						bad = append(bad, fmt.Sprintf("%s is named a text but sends SQL (itself or through a function it calls)", k))
					}
					continue
				}
				statements += len(writes)
				for _, w := range writes {
					if !plainInsert(w, tables) {
						bad = append(bad, fmt.Sprintf("%s writes %v (line %d), and not as one plain INSERT of a fresh row", k, tables, fset.Position(w.Pos()).Line))
					}
				}
			}
		}
		if statements > 0 {
			bad = append(bad, lockAt...)
		}
	default:
		bad = append(bad, fmt.Sprintf("unknown kind %d", kind))
	}
	sort.Strings(bad)
	return bad
}

// testDirs: every directory under root holding a _test.go file, as root-relative paths joined to root
// (the spelling tablesLockRowHolders uses); hidden directories and testdata skipped.
func testDirs(t *testing.T, root string) []string {
	t.Helper()
	var dirs []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if p != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if m, _ := filepath.Glob(filepath.Join(p, "*_test.go")); len(m) > 0 {
			dirs = append(dirs, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return dirs
}

// TestTablesLock_AdminRowsAreWrittenUnderTheHolder -- the M14 test-infra card (an audit's finding).
// internal/adminauth's newAdminRow holds the operator-tables lock only AROUND ITS OWN INSERT (since
// 5a45f7d6; a whole-test hold made every EXCLUSIVE waiter queue behind the package), so a test that
// called it holds nothing afterwards -- and TestNoticeRecipient_IsTheAddressOnTheRowAndNothingElse
// then committed a raw admin_users row (the NULL-address one newAdminRow cannot write) with no lock
// at all: exactly the commit internal/db's backfill tests must not see while they hold it EXCLUSIVE
// (holdTablesLock says why). TestTablesLock_IsTakenOncePerTestTree could not see it: it checks who
// ASKS for the lock, not who writes without it.
//
// THE SECOND TEST-INFRA CARD widened it: internal/handler is read too (six fixtures there committed
// admin rows holding nothing -- the tap-brand, logo-route, brand-editor, API-keys and peer-logo fixtures
// and the activation-logo test; internal/adminauth had two status UPDATEs past newAdminRow), an UPDATE or DELETE is a write as well as an INSERT (bfTx is READ COMMITTED, and
// an unlink committed mid-test turns a row into a candidate its closing assertion counts), a holder
// that only might run (in a branch, or a loop body the write is after) covers nothing, and a release
// ends the cover (p.releaseTablesLock(), release()).
//
// PINNED HERE. In every directory of tablesLockRowHolders, a raw write of a row of
// tablesLockRowTables in a _test.go function is at a covered point: a holder call -- the re-entrant
// holder, or a function derived from it that holds for the whole test -- dominates it with no release
// in between, or every call to the helper is at a covered point of its caller; or the function is one
// of tablesLockRowExempt's, which must be the one exempted shape (exemptionFlaws). A write in a
// package-level declaration is always a finding. POSITIVE CONTROL first, below, each line with what
// the scan must say of it.
//
// ITS AUDIT (single lens, RET) widened it once more: O-1, a write whose table is not in its literal --
// assembled with + or Sprintf, spelled public."admin_users", or held in a package-level const -- went
// uncounted; D-1, a write registered in a defer, a go statement or a Cleanup between the hold and a
// release counted as covered though it runs after the release; D-2, the exemption passed on any
// literal naming the xact lock, a log text included.
//
// THE THIRD TEST-INFRA CARD (an audit's LOW findings) widened it again: a write-verb literal anywhere in
// a + chain (`prefix + "UPDATE " + tbl`, ending the chain fails closed), in parentheses, split over
// literals; UPDATE ONLY, DELETE FROM ONLY, MERGE INTO, TRUNCATE (any place in its list) and COPY; a
// pgx.Identifier{"admin_users"} (CopyFrom's table, with or without its schema, fail closed on a part
// it cannot read). An exemption may reach the session lock through NO call path (a fixture that holds
// and releases, like brandBusiness, is no derived holder). internal/handler/operator (newE2E) and
// internal/domain/signup (sharedTablesLock) are read too. And the directories are a CLOSED inventory:
// every test directory of the repository with a raw write is in tablesLockRowHolders or in
// tablesLockRowOutside, with its kind's shape (tablesLockOutsideFlaws) -- measured: 21 directories, 4
// read by the holder rule, 17 named outside it. ITS AUDIT added: SQL comments, a spaced dot and any
// qualifier chain before a table are read through; a Sprintf format is rendered with its literal
// arguments (the verb may be one) and "%" for the others; pgx's CopyFrom with a table that is no
// readable pgx.Identifier literal fails closed; a plain INSERT is one statement.
//
// NO COMPLETENESS CLAIM (as PART III above): the scan is syntactic. Not seen: a write whose verb and
// table are both outside its literals (a whole statement in a variable, a function value, an interface,
// another package), a statement assembled in any other way than + or Sprintf (strings.Join, a builder,
// +=, a variable that holds the open half: q := "UPDATE "; exec(q + t) is seen only because the
// literal ends its chain), a TRUNCATE list assembled from parts, an assembled COPY, a qualifier other
// than public. before a format slot ("UPDATE tappa.%s"), a Sprintf whose format is no literal; a holder or a
// release reached through a function value, an aliased release (r := release; r()), a release in a loop
// or a goroutine counted only by where it stands, a release inside another function than the one
// holding; a lock reached through another package (an exemption's transitive check reads the
// directory's own functions). A method is matched by its name alone, a release by its field name
// (releaseTablesLock) or the variable a holder's result was assigned to, a Cleanup by the method name
// .Cleanup, pgx.Identifier by its type name, SQL sent by method name (sendsSQL). Fail closed, hence
// tablesLockRowTexts: a message or a scanner's input assembled like a statement counts as a write until
// named. Conservative: a deferred write registered after a deferred release counts as uncovered although
// it runs first. The inventory reads every directory under the repository root but hidden ones and
// testdata; rowOutsideExclusive trusts Go's test order (sequential tests one at a time, parallel ones
// after them), and rowOutsidePlain relies on bfTx's table lock for the commits it cannot see.
func TestTablesLock_AdminRowsAreWrittenUnderTheHolder(t *testing.T) {
	const control = `package p
func hold(t *testing.T) func() { lock(t); return nil }
func whole(t *testing.T) *pgx.Conn { hold(t); return nil }
func scoped(t *testing.T) { release := hold(t); defer release(); exec("INSERT INTO admin_users (id) VALUES ($1)") }
func TestFinding(t *testing.T) { scoped(t); exec(` + "`" + `INSERT  INTO
	admin_users (id, email) VALUES ($1, NULL)` + "`" + `) }
func TestLate(t *testing.T) { exec("insert into admin_users(id) values ($1)"); hold(t) }
func TestBare(t *testing.T) { exec("INSERT INTO admin_users (id) VALUES ($1)") }
func (w *world) row() { w.exec("INSERT INTO admin_users (id) VALUES ($1)") }
func (w *world) linked() { w.row() }
func TestHeldWorld(t *testing.T) { w := newWorld(whole(t)); w.linked(); t.Run("s", func(t *testing.T) { w.row() }) }
func shared() { exec("INSERT INTO admin_users (id) VALUES ($1)") }
func TestSharedHeld(t *testing.T) { hold(t); shared() }
func TestSharedBare(t *testing.T) { shared() }
func TestHeld(t *testing.T) { release := hold(t); exec("INSERT INTO admin_users (id) VALUES ($1)"); release() }
func TestOtherTable(t *testing.T) { exec("INSERT INTO admin_sessions (id) VALUES ($1)"); exec("INSERT INTO admin_users_archive (id) VALUES ($1)") }
func harness(t *testing.T) *h { return &h{c: whole(t)} }
func TestHarness(t *testing.T) { p := harness(t); exec("UPDATE admin_users SET status = 'disabled' WHERE id = $1", p) }
func TestReleased(t *testing.T) { p := harness(t); p.releaseTablesLock(); exec("DELETE FROM admin_users WHERE id = $1") }
func TestReheld(t *testing.T) { p := harness(t); p.releaseTablesLock(); for range 2 { release := hold(t); exec("UPDATE admin_users SET x = 1"); release() } }
func TestAfterTheLoop(t *testing.T) { for range 2 { hold(t) }; exec("UPDATE admin_users SET x = 1") }
func TestBranch(t *testing.T) { if ok { hold(t) }; exec("UPDATE admin_users SET x = 1") }
func maybe(t *testing.T) *h { if skip { return nil }; return &h{c: whole(t)} }
func TestMaybe(t *testing.T) { maybe(t); exec("UPDATE admin_users SET x = 1") }
func plant(c *pgx.Conn) { exec("INSERT INTO admin_users (id) VALUES ($1)") }
func TestPlantHeld(t *testing.T) { plant(whole(t)) }
func TestConcat(t *testing.T) { exec("INSERT INTO admin_users" + " (id) VALUES ($1)") }
func TestExempt(t *testing.T) { exec("SELECT pg_advisory_xact_lock_shared(hashtext($1))", key); exec("INSERT INTO admin_users (id) VALUES ($1)") }
func TestSprintf(t *testing.T) { exec(fmt.Sprintf("INSERT INTO %s (id) VALUES ($1)", "admin_users")) }
func TestSprintfVar(t *testing.T) { exec(fmt.Sprintf("DELETE FROM %s WHERE id = $1", table)) }
func TestSprintfOther(t *testing.T) { exec(fmt.Sprintf("INSERT INTO %s (id) VALUES ($1)", "tenants")) }
const packageWrite = "INSERT INTO admin_users (id) VALUES ($1)"
func TestPlus(t *testing.T) { exec("UPDATE " + adminUsers + " SET x = 1") }
func TestPlusOther(t *testing.T) { exec("UPDATE " + "tenants SET x = 1") }
func TestSchema(t *testing.T) { exec("UPDATE public.\"admin_users\" SET x = 1") }
func TestLabel(t *testing.T) { cases := []struct{ what, sql string }{{"UPDATE", "UPDATE tenants SET x = 1"}}; run(cases) }
func TestCleanupAfterRelease(t *testing.T) { release := hold(t); t.Cleanup(func() { exec("DELETE FROM admin_users WHERE id = $1") }); release() }
func TestDeferAfterRelease(t *testing.T) { release := hold(t); defer exec("DELETE FROM admin_users WHERE id = $1"); release() }
func TestCleanupHeld(t *testing.T) { whole(t); t.Cleanup(func() { exec("DELETE FROM admin_users WHERE id = $1") }) }
func TestCleanupHoldsItself(t *testing.T) { t.Cleanup(func() { release := hold(t); defer release(); exec("DELETE FROM admin_users WHERE id = $1") }) }
func registers(t *testing.T) { t.Cleanup(func() { exec("DELETE FROM admin_users WHERE id = $1") }) }
func TestHelperLate(t *testing.T) { release := hold(t); registers(t); release() }
func TestPlusRight(t *testing.T) { exec(prefix + "UPDATE " + adminUsers + " SET x = 1") }
func TestParen(t *testing.T) { exec(("UPDATE ") + adminUsers) }
func TestParenSprintf(t *testing.T) { exec(fmt.Sprintf(("DELETE FROM %s WHERE id = $1"), table)) }
func TestPlusSplit(t *testing.T) { exec("UPD" + "ATE " + adminUsers) }
func TestPlusEnds(t *testing.T) { q := prefix + "DELETE FROM "; exec(q + table) }
func TestOnly(t *testing.T) { exec("UPDATE ONLY admin_users SET x = 1"); exec("DELETE FROM ONLY public.admin_users") }
func TestMerge(t *testing.T) { exec("MERGE INTO admin_users AS a USING s ON false WHEN NOT MATCHED THEN INSERT (id) VALUES (s.id)") }
func TestTruncate(t *testing.T) { exec("TRUNCATE admin_sessions, admin_users CASCADE") }
func TestTruncateOther(t *testing.T) { exec("TRUNCATE admin_users_archive, tenants") }
func TestCopy(t *testing.T) { conn.CopyFrom(ctx, pgx.Identifier{"admin_users"}, cols, src) }
func TestCopySchema(t *testing.T) { conn.CopyFrom(ctx, pgx.Identifier{"public", "admin_users"}, cols, src) }
func TestCopyVar(t *testing.T) { conn.CopyFrom(ctx, pgx.Identifier{schema, "x"}, cols, src) }
func TestCopyOther(t *testing.T) { conn.CopyFrom(ctx, pgx.Identifier{"tenants"}, cols, src) }
func TestCopySQL(t *testing.T) { conn.PgConn().CopyFrom(ctx, r, "COPY admin_users (id) FROM STDIN") }
func TestSprintfVerb(t *testing.T) { exec(fmt.Sprintf("%s admin_users SET x = 1", "UPDATE")) }
func TestSprintfVerbVar(t *testing.T) { exec(fmt.Sprintf("%s admin_users SET x = 1", verb)) }
func TestComment(t *testing.T) { exec("UPDATE /* c */ admin_users SET x = 1") }
func TestSpacedDot(t *testing.T) { exec("UPDATE public . admin_users SET x = 1") }
func TestThreePart(t *testing.T) { exec("UPDATE tappa.public.admin_users SET x = 1") }
func TestCopySlice(t *testing.T) { conn.CopyFrom(ctx, []string{"admin_users"}, cols, src) }
func TestCopyConv(t *testing.T) { conn.CopyFrom(ctx, pgx.Identifier([]string{"admin_users"}), cols, src) }
func TestCopyDot(t *testing.T) { ident := Identifier{"admin_users"}; copyInto(ident) }
func TestSprintfOtherVerb(t *testing.T) { exec(fmt.Sprintf("%s tenants SET x = 1", "UPDATE")) }
func TestCommentOnly(t *testing.T) { exec("SELECT 1 /* UPDATE admin_users SET x = 1 */ -- DELETE FROM admin_users") }
`
	cfset := token.NewFileSet()
	cf, err := parser.ParseFile(cfset, "control.go", control, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	got := tablesLockRowOutsiders(cfset, []*ast.File{cf}, []string{"hold"}, tablesLockRowTables, map[string]string{"TestExempt": "control"})
	want := []string{
		"TestAfterTheLoop writes [admin_users] (line 21) outside the holder [hold]", // the loop may not run
		"TestBare writes [admin_users] (line 8) outside the holder [hold]",
		"TestBranch writes [admin_users] (line 22) outside the holder [hold]", // the branch may not run
		// D-1: registered between the hold and the release, it runs after the release.
		"TestCleanupAfterRelease writes [admin_users] (line 37) outside the holder [hold]",
		"TestConcat writes [admin_users] (line 27) outside the holder [hold]", // the table name ends a literal
		"TestDeferAfterRelease writes [admin_users] (line 38) outside the holder [hold]",
		"TestFinding writes [admin_users] (line 5) outside the holder [hold]", // scoped: holds nothing after
		"TestLate writes [admin_users] (line 7) outside the holder [hold]",
		"TestMaybe writes [admin_users] (line 24) outside the holder [hold]", // maybe may return first
		// O-1: the table is not in the literal -- an identifier after "UPDATE " + ..., a Sprintf slot
		// (a literal argument, or one the scan cannot read), a schema prefix and a quoted identifier.
		"TestPlus writes [admin_users] (line 33) outside the holder [hold]",
		"TestReleased writes [admin_users] (line 19) outside the holder [hold]", // released before it
		"TestSchema writes [admin_users] (line 35) outside the holder [hold]",
		"TestSprintf writes [admin_users] (line 29) outside the holder [hold]",
		"TestSprintfVar writes [admin_users] (line 30) outside the holder [hold]",
		// O-1: a package-level statement stands in no function.
		"a package-level declaration writes [admin_users] (control.go:32) -- inline it where it runs, so the scan can see under what",
		"registers writes [admin_users] (line 41) outside the holder [hold]", // late, reached from TestHelperLate
		"shared writes [admin_users] (line 12) outside the holder [hold]",    // TestSharedBare
		// The third test-infra card (an audit's finding): the verb literal mid-chain, in parentheses
		// (in a + and as a Sprintf format), split over two literals, or ending a chain (fail closed);
		// UPDATE ONLY / DELETE FROM ONLY (twice: two literals), MERGE, a TRUNCATE list, COPY, and a
		// pgx.Identifier table -- with its schema, and with a part the scan cannot read (fail closed).
		"TestPlusRight writes [admin_users] (line 43) outside the holder [hold]",
		"TestParen writes [admin_users] (line 44) outside the holder [hold]",
		"TestParenSprintf writes [admin_users] (line 45) outside the holder [hold]",
		"TestPlusSplit writes [admin_users] (line 46) outside the holder [hold]",
		"TestPlusEnds writes [admin_users] (line 47) outside the holder [hold]",
		"TestOnly writes [admin_users] (line 48) outside the holder [hold]",
		"TestOnly writes [admin_users] (line 48) outside the holder [hold]",
		"TestMerge writes [admin_users] (line 49) outside the holder [hold]",
		"TestTruncate writes [admin_users] (line 50) outside the holder [hold]",
		"TestCopy writes [admin_users] (line 52) outside the holder [hold]",
		"TestCopySchema writes [admin_users] (line 53) outside the holder [hold]",
		"TestCopyVar writes [admin_users] (line 54) outside the holder [hold]",
		"TestCopySQL writes [admin_users] (line 56) outside the holder [hold]",
		// Its audit: the verb in a Sprintf argument (a literal, and one the scan cannot read); a comment, a
		// spaced dot and a three-part name before the table; CopyFrom's table as a []string, a conversion
		// and a dot-imported Identifier (held in a variable). Not a write: another table's verb argument, a statement in a
		// comment only.
		"TestSprintfVerb writes [admin_users] (line 57) outside the holder [hold]",
		"TestSprintfVerbVar writes [admin_users] (line 58) outside the holder [hold]",
		"TestComment writes [admin_users] (line 59) outside the holder [hold]",
		"TestSpacedDot writes [admin_users] (line 60) outside the holder [hold]",
		"TestThreePart writes [admin_users] (line 61) outside the holder [hold]",
		"TestCopySlice writes [admin_users] (line 62) outside the holder [hold]",
		"TestCopyConv writes [admin_users] (line 63) outside the holder [hold]",
		"TestCopyDot writes [admin_users] (line 64) outside the holder [hold]",
	}
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Fatalf("POSITIVE CONTROL FAILED: the scan reports\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// ... and the derivation: whole (top level, no release) and harness (through whole) hold for the
	// test; scoped (it releases) and maybe (a return before) do not.
	if _, set := rowHoldersOf([]*ast.File{cf}, []string{"hold"}, tablesLockRowTables); !set["whole"] || !set["harness"] || set["scoped"] || set["maybe"] {
		t.Fatalf("POSITIVE CONTROL FAILED: derived holders %v", set)
	}
	// ... and the exemption's shape (D-2): each function below lacks exactly what its name says.
	const exemptControl = `package p
func lock(t *testing.T) { conn.Exec(ctx, "SELECT pg_advisory_lock_shared(hashtext($1))", operatorTablesTestLock) }
func hold(t *testing.T) func() { lock(t); return nil }
func brand(t *testing.T) { release := hold(t); defer release(); exec("INSERT INTO tenants (id) VALUES ($1)") }
func brandVia(t *testing.T) { brand(t) }
func TestGood(t *testing.T) { tx := begin(); defer func() { tx.Rollback(ctx) }(); exec("SELECT pg_advisory_xact_lock_shared(hashtext($1))", key); exec("INSERT INTO admin_users (id) VALUES ($1)") }
func TestLogged(t *testing.T) { tx := begin(); defer tx.Rollback(ctx); t.Log("SELECT pg_advisory_xact_lock_shared(hashtext($1))", key); exec("INSERT INTO admin_users (id) VALUES ($1)") }
func TestLockAfter(t *testing.T) { tx := begin(); defer tx.Rollback(ctx); exec("INSERT INTO admin_users (id) VALUES ($1)"); exec("SELECT pg_advisory_xact_lock_shared(hashtext($1))", key) }
func TestCommits(t *testing.T) { tx := begin(); exec("SELECT pg_advisory_xact_lock_shared(hashtext($1))", key); exec("INSERT INTO admin_users (id) VALUES ($1)"); tx.Commit(ctx) }
func TestHeldToo(t *testing.T) { hold(t); tx := begin(); defer tx.Rollback(ctx); exec("SELECT pg_advisory_xact_lock_shared(hashtext($1))", key); exec("INSERT INTO admin_users (id) VALUES ($1)") }
func TestNoWrite(t *testing.T) { tx := begin(); defer tx.Rollback(ctx); exec("SELECT pg_advisory_xact_lock_shared(hashtext($1))", key) }
func TestFixtureToo(t *testing.T) { tx := begin(); defer tx.Rollback(ctx); exec("SELECT pg_advisory_xact_lock_shared(hashtext($1))", key); exec("INSERT INTO admin_users (id) VALUES ($1)"); t.Run("s", func(t *testing.T) { brandVia(t) }) }
`
	ef, err := parser.ParseFile(cfset, "exempt.go", exemptControl, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	efuncs, _ := rowHoldersOf([]*ast.File{ef}, []string{"hold"}, tablesLockRowTables)
	esites := tablesLockSites(cfset, []*ast.File{ef}, nil)
	wantFlaws := map[string]string{
		"TestGood":      "",
		"TestLogged":    "it sends no SELECT pg_advisory_xact_lock statement",
		"TestLockAfter": "a write stands before its transaction-scoped lock",
		"TestCommits":   "it calls no Rollback",
		"TestHeldToo":   "it also calls a holder (a second lock on a second connection); it reaches the session lock through hold (a second lock on a second connection)",
		"TestNoWrite":   "it writes no row",
		// brand holds and releases -- no derived holder -- and is reached two calls down, in a subtest.
		"TestFixtureToo": "it reaches the session lock through brandVia (a second lock on a second connection)",
	}
	for _, d := range ef.Decls {
		fd := d.(*ast.FuncDecl)
		want, ok := wantFlaws[fd.Name.Name]
		if !ok {
			continue
		}
		if got := strings.Join(exemptionFlaws(fd, efuncs[fd.Name.Name], esites[fd.Name.Name]), "; "); got != want {
			t.Errorf("POSITIVE CONTROL FAILED (exemption shape): %s has flaws %q, want %q", fd.Name.Name, got, want)
		}
	}

	fset := token.NewFileSet()
	for dir, holders := range tablesLockRowHolders {
		writers := 0
		exempt := tablesLockRowExempt[dir]
		seen := map[string]bool{}
		var derived []string
		for _, files := range parsePackages(t, fset, dir) {
			for _, f := range tablesLockRowOutsiders(fset, files, holders, tablesLockRowTables, exempt) {
				t.Errorf("%s: %s", dir, f)
			}
			funcs, set := rowHoldersOf(files, holders, tablesLockRowTables)
			lockSites := tablesLockSites(fset, files, nil)
			for k := range set {
				if !slices.Contains(holders, k) && !strings.HasPrefix(k, "Test") {
					derived = append(derived, k)
				}
			}
			for _, f := range files {
				for _, d := range f.Decls {
					fd, ok := d.(*ast.FuncDecl)
					if !ok || fd.Body == nil {
						continue
					}
					n := len(rowWritesIn(fd.Body, tablesLockRowTables))
					writers += n
					// The exemption is closed: it names a function here of the one exempted shape --
					// not a stale name, and not a way round the holder (exemptionFlaws).
					if exempt[funcKey(fd)] != "" {
						seen[funcKey(fd)] = true
						if flaws := exemptionFlaws(fd, funcs[funcKey(fd)], lockSites[funcKey(fd)]); len(flaws) > 0 {
							t.Errorf("%s: %s is exempt but is not the exempted shape: %s", dir, funcKey(fd), strings.Join(flaws, "; "))
						}
					}
				}
			}
		}
		for fn := range exempt {
			if !seen[fn] {
				t.Errorf("%s: the exemption names %s, which is not a function there", dir, fn)
			}
		}
		// Against going blind: the directory's writers are there to be checked.
		if writers == 0 {
			t.Errorf("%s: no raw write of %v found -- the scan went blind or the directory moved", dir, tablesLockRowTables)
		}
		sort.Strings(derived)
		t.Logf("%s: %d raw writes of %v checked against %v and the helpers that hold through them %v; exempt %v", dir, writers, tablesLockRowTables, holders, derived, exempt)
	}

	// THE CLOSED INVENTORY (the third test-infra card): every test directory with a raw write is read by
	// the holder rule or named in tablesLockRowOutside, whose kind's shape it has. Its controls first.
	const plainControl = `package p
func TestPlain(t *testing.T) { exec("INSERT INTO admin_users (id, email) VALUES ($1, $2)") }
func TestUpdates(t *testing.T) { exec("UPDATE admin_users SET status = 'disabled' WHERE id = $1") }
func TestLinks(t *testing.T) { exec("INSERT INTO admin_users (id, identity_id) VALUES ($1, $2)") }
func TestUpserts(t *testing.T) { exec("INSERT INTO admin_users (id) VALUES ($1) ON CONFLICT (id) DO UPDATE SET email = 'x'") }
func TestAssembled(t *testing.T) { exec("INSERT INTO " + table + " (id) VALUES ($1)") }
func TestCopies(t *testing.T) { conn.CopyFrom(ctx, pgx.Identifier{"admin_users"}, cols, src) }
func label(name string) string { return name + ": INSERT INTO " + table }
func labelSends(name string) { send(name + ": INSERT INTO " + table) }
func send(q string) { conn.Exec(ctx, q) }
func lock(t *testing.T) { conn.Exec(ctx, "SELECT pg_advisory_lock_shared(hashtext($1))", operatorTablesTestLock) }
func TestTwoStatements(t *testing.T) { exec("INSERT INTO admin_users (id) VALUES ($1); DELETE FROM admin_users WHERE id = $2") }
func TestOtherTableFirst(t *testing.T) { exec("INSERT INTO tenants (id) SELECT id FROM (UPDATE admin_users SET x = 1 RETURNING id) u") }
func TestTrailingSemicolon(t *testing.T) { exec("INSERT INTO admin_users (id) VALUES ($1);") }
`
	pf, err := parser.ParseFile(cfset, "plain.go", plainControl, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	pseen := map[string]bool{}
	got = tablesLockOutsideFlaws(cfset, []*ast.File{pf}, rowOutsidePlain, tablesLockRowTables, map[string]string{"label": "a label", "labelSends": "a label"}, pseen)
	want = []string{
		"TestAssembled writes [admin_users] (line 6), and not as one plain INSERT of a fresh row",
		"TestCopies writes [admin_users] (line 7), and not as one plain INSERT of a fresh row",
		"TestLinks writes [admin_users] (line 4), and not as one plain INSERT of a fresh row",
		"TestOtherTableFirst writes [admin_users] (line 13), and not as one plain INSERT of a fresh row",
		"TestTwoStatements writes [admin_users] (line 12), and not as one plain INSERT of a fresh row",
		"TestUpdates writes [admin_users] (line 3), and not as one plain INSERT of a fresh row",
		"TestUpserts writes [admin_users] (line 5), and not as one plain INSERT of a fresh row",
		"labelSends is named a text but sends SQL (itself or through a function it calls)",
		"lock sends the session lock statement (line 11): a directory that takes it belongs in tablesLockRowHolders",
	}
	if !slices.Equal(got, want) || !pseen["label"] || !pseen["labelSends"] {
		t.Fatalf("POSITIVE CONTROL FAILED (plain directory): the scan reports\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	const exclusiveControl = `package p
func bfTx(t *testing.T) (context.Context, pgx.Tx) { return nil, nil }
func helper(t *testing.T) { bfTx(t) }
func TestBackfill(t *testing.T) { bfTx(t) }
func TestBackfillParallel(t *testing.T) { t.Parallel(); bfTx(t) }
func TestViaHelper(t *testing.T) { t.Run("s", func(t *testing.T) { t.Parallel(); helper(t) }) }
func TestOther(t *testing.T) { t.Parallel(); opTx(t) }
func TestDDLParallel(t *testing.T) { t.Parallel(); bfTxForDDL(t) }
`
	xf, err := parser.ParseFile(cfset, "exclusive.go", exclusiveControl, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	got = tablesLockOutsideFlaws(cfset, []*ast.File{xf}, rowOutsideExclusive, tablesLockRowTables, nil, map[string]bool{})
	want = []string{
		"TestBackfillParallel reaches bfTx and calls .Parallel (line 5): it runs beside the package's other tests",
		"TestDDLParallel reaches bfTx and calls .Parallel (line 8): it runs beside the package's other tests",
		"TestViaHelper reaches bfTx and calls .Parallel (line 6): it runs beside the package's other tests",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("POSITIVE CONTROL FAILED (the exclusive side): the scan reports\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	inventory := map[string]bool{}
	for _, dir := range testDirs(t, filepath.Join("..", "..")) {
		writes := 0
		pkgs := parsePackages(t, fset, dir)
		for _, files := range pkgs {
			for _, f := range files {
				writes += len(rowWritesIn(f, tablesLockRowTables))
			}
		}
		if writes == 0 {
			continue
		}
		inventory[dir] = true
		if tablesLockRowHolders[dir] != nil {
			continue
		}
		kind := tablesLockRowOutside[dir]
		if kind == 0 {
			t.Errorf("%s: %d raw writes of %v, and the directory is in neither tablesLockRowHolders nor tablesLockRowOutside -- give it a holder, or name it with its reason", dir, writes, tablesLockRowTables)
			continue
		}
		seen := map[string]bool{}
		for _, files := range pkgs {
			for _, f := range tablesLockOutsideFlaws(fset, files, kind, tablesLockRowTables, tablesLockRowTexts[dir], seen) {
				t.Errorf("%s: %s", dir, f)
			}
		}
		for fn := range tablesLockRowTexts[dir] {
			if !seen[fn] {
				t.Errorf("%s: tablesLockRowTexts names %s, which is not a function there", dir, fn)
			}
		}
	}
	for dir := range tablesLockRowOutside {
		if !inventory[dir] {
			t.Errorf("%s: named in tablesLockRowOutside, but the scan sees no raw write there -- take it off", dir)
		}
		if tablesLockRowHolders[dir] != nil {
			t.Errorf("%s: both in tablesLockRowHolders and in tablesLockRowOutside", dir)
		}
	}
	for dir := range tablesLockRowTexts {
		if tablesLockRowOutside[dir] != rowOutsidePlain {
			t.Errorf("%s: tablesLockRowTexts names a directory that is no rowOutsidePlain one", dir)
		}
	}
	t.Logf("the inventory: %d test directories write %v raw; %d read by the holder rule, %d named outside it", len(inventory), tablesLockRowTables, len(tablesLockRowHolders), len(tablesLockRowOutside))
}
