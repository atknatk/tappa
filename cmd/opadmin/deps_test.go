package main

// deps_test.go -- what this command links and reads, measured on the source and on
// the toolchain's own dependency listing. ADR 0020 §6: no connection, no DSN, no
// driver. These tests are PART II pins: each one names the forms it catches, and a
// form not listed here is the subject of code review (main.go's header).

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const modulePath = "github.com/atknatk/tappa"

// importAllowList is EVERY import of this command's non-test sources. Adding one, or
// dropping one, turns TestDeps_ImportsAreTheListedOnes red: the list is the review.
var importAllowList = []string{
	"crypto/rand", "crypto/sha256", "encoding/base64", "encoding/hex", "errors", "flag", "io", "os",
	"strconv", "strings", "time", "unicode", "unicode/utf8",
}

// closureDenyList are standard-library packages the closure must not hold: the
// database/sql pair a driver plugs into, net and net/http (a connection through the
// standard library's network packages), os/exec and plugin. A process can also be
// started from package os itself (os.StartProcess) or from syscall, which every
// closure with os holds: TestDeps_StartsNoProcess looks for those calls.
var closureDenyList = []string{"database/sql", "database/sql/driver", "net", "net/http", "os/exec", "plugin"}

// ports are the GOOS/GOARCH pairs the closure is listed for.
var ports = [][2]string{{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}, {"windows", "amd64"}}

// listDeps is `go list -deps` of pkg for one port, CGO off, as "path|standard" lines.
func listDeps(t *testing.T, pkg, goos, goarch string) [][2]string {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}|{{.Standard}}", pkg)
	cmd.Dir = filepath.Join("..", "..")
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0", "GOFLAGS=")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps %s (%s/%s): %v", pkg, goos, goarch, err)
	}
	var deps [][2]string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		p, std, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("go list printed %q", line)
		}
		deps = append(deps, [2]string{p, std})
	}
	return deps
}

// closureFindings is what is wrong with one closure: a non-standard package other
// than the command itself, a package on the deny list, or a path naming a known
// PostgreSQL driver.
func closureFindings(deps [][2]string, self string) []string {
	var bad []string
	for _, d := range deps {
		p, std := d[0], d[1] == "true"
		switch {
		case p == self:
		case !std:
			bad = append(bad, p+" (not the standard library)")
		case slices.Contains(closureDenyList, p):
			bad = append(bad, p+" (on the deny list)")
		}
		for _, drv := range []string{"pgx", "lib/pq", "pgconn", "jackc"} {
			if strings.Contains(p, drv) {
				bad = append(bad, p+" (a PostgreSQL driver)")
			}
		}
	}
	return bad
}

// TestDeps_NoDriverInTheClosure: on five ports, the command's dependency closure is
// the standard library plus the command, without the deny list. POSITIVE CONTROL:
// the same check finds pgx in internal/operatorauth's closure -- the measurement that
// made this command carry its own copy of the link-secret contract (main.go). And the
// serving binary's closure does not hold this command.
func TestDeps_NoDriverInTheClosure(t *testing.T) {
	self := modulePath + "/cmd/opadmin"
	for _, p := range ports {
		deps := listDeps(t, "./cmd/opadmin", p[0], p[1])
		if len(deps) < 20 || !slices.Contains(deps, [2]string{self, "false"}) {
			t.Fatalf("%s/%s: the listing (%d entries) is not this command's closure", p[0], p[1], len(deps))
		}
		if bad := closureFindings(deps, self); len(bad) > 0 {
			t.Errorf("%s/%s: the closure holds %v", p[0], p[1], bad)
		}
	}
	if bad := closureFindings(listDeps(t, "./internal/operatorauth", "linux", "amd64"), ""); !slices.ContainsFunc(bad,
		func(s string) bool { return strings.Contains(s, "github.com/jackc/pgx/v5 ") }) {
		t.Fatalf("CONTROL FAILED: the check did not find pgx in internal/operatorauth's closure: %v", bad)
	}
	for _, d := range listDeps(t, "./cmd/tappa", "linux", "amd64") {
		if strings.Contains(d[0], "opadmin") {
			t.Errorf("the serving binary's closure holds %s", d[0])
		}
	}
}

// sourceFiles are this command's non-test .go files.
func sourceFiles(t *testing.T) []string {
	t.Helper()
	all, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range all {
		if !strings.HasSuffix(f, "_test.go") {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		t.Fatal("no source files found")
	}
	return out
}

// importsOf is every import path of src, read with build constraints IGNORED (the
// parser does not evaluate them), so a file another port would compile is read too.
func importsOf(t *testing.T, name string, src []byte) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	var out []string
	for _, im := range f.Imports {
		out = append(out, strings.Trim(im.Path.Value, "`\""))
	}
	return out
}

// TestDeps_ImportsAreTheListedOnes: the set of imports of the non-test sources is
// exactly importAllowList. POSITIVE CONTROL: a source importing a driver, and one
// importing "C", are seen.
func TestDeps_ImportsAreTheListedOnes(t *testing.T) {
	got := map[string]bool{}
	for _, f := range sourceFiles(t) {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range importsOf(t, f, src) {
			got[p] = true
		}
	}
	var gotList []string
	for p := range got {
		gotList = append(gotList, p)
	}
	slices.Sort(gotList)
	want := slices.Clone(importAllowList)
	slices.Sort(want)
	if !slices.Equal(gotList, want) {
		t.Errorf("the imports are %v, the reviewed list is %v", gotList, want)
	}
	probe := importsOf(t, "probe.go", []byte("//go:build ignore\n\npackage main\n\nimport (\n\t_ \"github.com/jackc/pgx/v5\"\n\t\"C\"\n)\n"))
	if !slices.Contains(probe, "github.com/jackc/pgx/v5") || !slices.Contains(probe, "C") {
		t.Fatalf("CONTROL FAILED: the import reader did not see a driver and cgo behind a build constraint: %v", probe)
	}
}

// envCalls are the environment readers and writers TestDeps_NoEnvironmentAndNoDSNName
// looks for, as package.Function.
var envCalls = []string{
	"os.Getenv", "os.LookupEnv", "os.Environ", "os.ExpandEnv", "os.Expand", "os.Setenv", "os.Unsetenv",
	"os.Clearenv", "syscall.Getenv", "syscall.Environ",
}

// dsnNames are connection-string variable names and URL schemes that must not be
// written in the non-test sources.
var dsnNames = []string{
	"DATABASE_MIGRATE_URL", "DATABASE_URL", "TAPPA_OPERATOR_DATABASE_URL",
	"PGPASSWORD", "PGHOST", "PGUSER", "PGDATABASE", "PGSERVICE", "PGPASSFILE",
	"postgres://", "postgresql://",
}

func envAndDSNFindings(t *testing.T, name string, src []byte) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), name, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	var bad []string
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); ok && slices.Contains(envCalls, x.Name+"."+sel.Sel.Name) {
			bad = append(bad, x.Name+"."+sel.Sel.Name)
		}
		return true
	})
	for _, n := range dsnNames {
		if strings.Contains(string(src), n) {
			bad = append(bad, n)
		}
	}
	return bad
}

// TestDeps_NoEnvironmentAndNoDSNName: the non-test sources call none of envCalls and
// write none of dsnNames (comments included -- the scan is over the text). POSITIVE
// CONTROL: a source that reads the migration role's DSN variable is caught twice, once
// for the call and once for the name. (This test file and the database tests name
// those variables: they are not the command's sources.)
func TestDeps_NoEnvironmentAndNoDSNName(t *testing.T) {
	for _, f := range sourceFiles(t) {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if bad := envAndDSNFindings(t, f, src); len(bad) > 0 {
			t.Errorf("%s: %v", f, bad)
		}
	}
	probe := "package main\n\nimport \"os\"\n\nvar d = os.Getenv(\"DATABASE_MIGRATE_URL\")\n"
	if bad := envAndDSNFindings(t, "probe.go", []byte(probe)); !slices.Equal(bad, []string{"os.Getenv", "DATABASE_MIGRATE_URL"}) {
		t.Fatalf("CONTROL FAILED: a source reading the owner DSN variable produced %v", bad)
	}
}

// TestDeps_NoBuildConstraintOrDirective: no non-test source carries a //go: line (build
// constraint, linkname, embed, …), a // +build line or a //line directive, and on every
// port the toolchain compiles exactly the files on disk with none ignored and no cgo.
func TestDeps_NoBuildConstraintOrDirective(t *testing.T) {
	files := sourceFiles(t)
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			l := strings.TrimSpace(line)
			if strings.HasPrefix(l, "//go:") || strings.HasPrefix(l, "// +build") || strings.HasPrefix(l, "//line ") {
				t.Errorf("%s:%d carries a directive: %q", f, i+1, l)
			}
		}
	}
	for _, p := range ports {
		cmd := exec.Command("go", "list", "-f", "{{join .GoFiles \",\"}}|{{join .IgnoredGoFiles \",\"}}|{{join .CgoFiles \",\"}}", ".")
		cmd.Env = append(os.Environ(), "GOOS="+p[0], "GOARCH="+p[1], "CGO_ENABLED=0", "GOFLAGS=")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list (%s/%s): %v", p[0], p[1], err)
		}
		parts := strings.Split(strings.TrimSpace(string(out)), "|")
		if len(parts) != 3 || parts[0] != strings.Join(files, ",") || parts[1] != "" || parts[2] != "" {
			t.Errorf("%s/%s: compiled %q, ignored %q, cgo %q; want exactly %v", p[0], p[1], parts[0], parts[1], parts[2], files)
		}
	}
}

// processCalls are the process starters of packages os and syscall -- reachable from
// a closure that holds neither os/exec nor plugin.
var processCalls = []string{"os.StartProcess", "syscall.Exec", "syscall.ForkExec", "syscall.StartProcess"}

func processCallFindings(t *testing.T, name string, src []byte) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), name, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	var bad []string
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); ok && slices.Contains(processCalls, x.Name+"."+sel.Sel.Name) {
			bad = append(bad, x.Name+"."+sel.Sel.Name)
		}
		return true
	})
	return bad
}

// TestDeps_StartsNoProcess: the non-test sources write none of the four selectors in
// processCalls. POSITIVE CONTROL: a source starting psql through os.StartProcess is
// caught.
func TestDeps_StartsNoProcess(t *testing.T) {
	for _, f := range sourceFiles(t) {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if bad := processCallFindings(t, f, src); len(bad) > 0 {
			t.Errorf("%s: %v", f, bad)
		}
	}
	probe := "package main\n\nimport \"os\"\n\nfunc f() { _, _ = os.StartProcess(\"psql\", nil, nil) }\n"
	if bad := processCallFindings(t, "probe.go", []byte(probe)); !slices.Equal(bad, []string{"os.StartProcess"}) {
		t.Fatalf("CONTROL FAILED: a source starting a process produced %v", bad)
	}
}
