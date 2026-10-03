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
// this directory: internal/db EXCLUSIVE (opTx) and SHARED (one test), the others SHARED.
var tablesLockDirs = []string{".", "../../internal/db", "../../internal/handler/operator", "../../internal/operatorauth"}

// tablesLockStatements are the functions that send the lock statement themselves, per
// directory -- the closed list the scan must find (its control against going blind).
var tablesLockStatements = map[string][]string{
	".":                               {"sharedTablesLock"},
	"../../internal/db":               {"TestOperatorDB_RunsAsTappaOperator", "opTx"},
	"../../internal/handler/operator": {"newE2E"},
	"../../internal/operatorauth":     {"sharedTablesLock"},
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
// lock -- directly or through other functions of the package -- the sites in its body.
func tablesLockSites(fset *token.FileSet, files []*ast.File) map[string][]lockSite {
	decls := map[string]*ast.FuncDecl{}
	for _, f := range files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				decls[funcKey(fd)] = fd
			}
		}
	}
	takers := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for k, fd := range decls {
			if !takers[k] && len(sitesIn(fset, fd.Body, takers)) > 0 {
				takers[k], changed = true, true
			}
		}
	}
	out := map[string][]lockSite{}
	for k := range takers {
		out[k] = sitesIn(fset, decls[k].Body, takers)
	}
	return out
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
// opTx (EXCLUSIVE) and TestOperatorDB_RunsAsTappaOperator, internal/handler/operator newE2E,
// internal/operatorauth sharedTablesLock (ownerTx, ownerPool). Measured when written: the
// one finding was the subtest above; after the fix, none. POSITIVE CONTROL: the same scan
// finds the CI shape, a function that asks twice and one that asks in a loop -- and not
// one whose subtest asks nothing, nor an unlock -- in a source written here.
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
	got := tablesLockFindings(tablesLockSites(cfset, []*ast.File{cf}))
	want := []string{
		`looped reaches the lock inside a loop (line 6 via "tx")`,
		`nested reaches the lock 2 times (line 4 via "lock", line 4 via "tx")`,
		`nested reaches the lock inside a func literal (line 4 via "tx")`,
		`twice reaches the lock 2 times (line 5 via "tx", line 5 via "tx")`,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("POSITIVE CONTROL FAILED: the scan reports\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	fset := token.NewFileSet()
	for _, dir := range tablesLockDirs {
		var holders, tests []string
		for _, files := range parsePackages(t, fset, dir) {
			sites := tablesLockSites(fset, files)
			for _, f := range tablesLockFindings(sites) {
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
