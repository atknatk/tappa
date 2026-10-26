package main

// tableslock_test.go -- the "tappa/test/operator-tables" advisory lock is asked for
// ONCE per test tree, read from the test sources of every package that takes it.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"sort"
	"strings"
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
	"../../internal/db":               {"TestOperatorDB_RunsAsTappaOperator", "bfTx", "opLiveFixture", "opTx"},
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

// tablesLockRowHolders are, per directory, the functions whose call HOLDS the lock from that call
// until the test ends: the re-entrant holder and what holds through it for the whole test. A helper
// that holds it only around its own insert and releases it (internal/adminauth's newAdminRow) is NOT
// one -- a test that called it holds nothing afterwards (M14 test-infra card, the audit's finding:
// TestNoticeRecipient_IsTheAddressOnTheRowAndNothingElse committed a raw admin row after newAdminRow
// had released). TestTablesLock_AdminRowsAreWrittenUnderTheHolder reads it.
var tablesLockRowHolders = map[string][]string{
	"../../internal/adminauth": {"holdTablesLock", "ownerConn"},
}

// tablesLockRowTables are the tables a raw INSERT of which commits what internal/db's EXCLUSIVE
// backfill tests read whole (bfTx): a row of one written past the holder is the M14-09 deadlock.
var tablesLockRowTables = []string{"admin_users"}

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

// rowInsertsIn: the string literals in body that INSERT INTO one of tables
// (case and white space ignored).
func rowInsertsIn(body *ast.BlockStmt, tables []string) []token.Pos {
	var at []token.Pos
	ast.Inspect(body, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		norm := strings.ToLower(strings.Join(strings.Fields(lit.Value), " "))
		for _, tbl := range tables {
			if strings.Contains(norm, "insert into "+tbl+" ") || strings.Contains(norm, "insert into "+tbl+"(") {
				at = append(at, lit.Pos())
				break
			}
		}
		return true
	})
	return at
}

// tablesLockRowOutsiders: every function of one package that writes a row of tables (a raw INSERT
// literal) at a point not covered by a holder. A point of a function is covered when the function
// called a holder BEFORE it (by source position), or -- for a helper -- when it has callers and every
// call to it is itself at a covered point of its caller (methods matched by name alone).
func tablesLockRowOutsiders(fset *token.FileSet, files []*ast.File, holders, tables []string) []string {
	type call struct {
		caller string
		at     token.Pos
	}
	decls := map[string]*ast.FuncDecl{}
	for _, f := range files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				decls[funcKey(fd)] = fd
			}
		}
	}
	held := map[string][]token.Pos{} // function -> positions of its calls to a holder
	callers := map[string][]call{}   // callee -> its call sites
	for k, fd := range decls {
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				ck := calleeKey(c)
				if slices.Contains(holders, ck) {
					held[k] = append(held[k], c.Pos())
				}
				if ck != "" && ck != k {
					callers[ck] = append(callers[ck], call{k, c.Pos()})
				}
			}
			return true
		})
	}
	var covered func(fn string, at token.Pos, seen map[string]bool) bool
	covered = func(fn string, at token.Pos, seen map[string]bool) bool {
		for _, h := range held[fn] {
			if h < at {
				return true
			}
		}
		cs := callers[fn]
		if len(cs) == 0 || seen[fn] || strings.HasPrefix(fn, "Test") {
			return false
		}
		seen[fn] = true
		defer delete(seen, fn)
		for _, c := range cs {
			if !covered(c.caller, c.at, seen) {
				return false
			}
		}
		return true
	}
	var bad []string
	for k, fd := range decls {
		if slices.Contains(holders, k) {
			continue
		}
		for _, at := range rowInsertsIn(fd.Body, tables) {
			if !covered(k, at, map[string]bool{}) {
				bad = append(bad, fmt.Sprintf("%s writes %v (line %d) outside the holder %v", k, tables, fset.Position(at).Line, holders))
			}
		}
	}
	sort.Strings(bad)
	return bad
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
// PINNED HERE. In every directory of tablesLockRowHolders, a raw INSERT INTO one of
// tablesLockRowTables in a _test.go function is at a covered point: the function called a holder
// (the re-entrant holder, or what holds through it until the test ends) before it, or every call to
// the helper is at a covered point of its caller. POSITIVE CONTROL first: the finding's shape (a
// scoped helper, then a raw insert), a holder called AFTER the insert, a helper called from one
// uncovered test among covered ones, and a test with no holder -- and none for the scoped helper's
// own insert (it holds around it), a helper called only under the holder (also from a subtest), a
// held test, or another table.
//
// NO COMPLETENESS CLAIM (as PART III above): the scan is syntactic -- a holder released before the
// insert, an INSERT built at run time or reached through a function value or another package, or a
// directory outside tablesLockRowHolders is not seen; a method is matched by its name alone.
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
`
	cfset := token.NewFileSet()
	cf, err := parser.ParseFile(cfset, "control.go", control, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	got := tablesLockRowOutsiders(cfset, []*ast.File{cf}, []string{"hold", "whole"}, tablesLockRowTables)
	want := []string{
		"TestBare writes [admin_users] (line 8) outside the holder [hold whole]",
		"TestFinding writes [admin_users] (line 5) outside the holder [hold whole]",
		"TestLate writes [admin_users] (line 7) outside the holder [hold whole]",
		"shared writes [admin_users] (line 12) outside the holder [hold whole]",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("POSITIVE CONTROL FAILED: the scan reports\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	fset := token.NewFileSet()
	for dir, holders := range tablesLockRowHolders {
		writers := 0
		for _, files := range parsePackages(t, fset, dir) {
			for _, f := range tablesLockRowOutsiders(fset, files, holders, tablesLockRowTables) {
				t.Errorf("%s: %s", dir, f)
			}
			for _, f := range files {
				for _, d := range f.Decls {
					if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
						writers += len(rowInsertsIn(fd.Body, tablesLockRowTables))
					}
				}
			}
		}
		// Against going blind: the directory's writers are there to be checked.
		if writers == 0 {
			t.Errorf("%s: no raw INSERT INTO %v found -- the scan went blind or the directory moved", dir, tablesLockRowTables)
		}
		t.Logf("%s: %d raw inserts of %v checked against %v", dir, writers, tablesLockRowTables, holders)
	}
}
