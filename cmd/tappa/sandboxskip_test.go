package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// T139 (2026-10-19): `make check-sandbox` runs cmd/tappa's tests a second time with -tags sandbox,
// and most of them -- the redline, secret-scan and pre-push tests, the heartbeat and backup scripts' -- never touch the
// code the tag changes: they read files and run bash. Running them twice is time, not evidence
// (CI run 39204779143: the sandbox step 11:37, against 3:05 before T137 added a hundred redline
// runs). The Makefile's SANDBOX_SKIP names them and `go test -skip` leaves them to `make check`.
// This file is the proof that the list may skip what it names: every test it matches is shown,
// from the source, to reach no declaration the tag can change.

// tagReach reports, for each top-level Test function of the package in dir (cmd/tappa, or since
// T185 its script half cmd/tappa/scripttest -- both under SANDBOX_TEST_PKGS' ./cmd/tappa/...), the
// first thing it reaches that a build with -tags sandbox could change, or "" if it reaches none.
// What counts as reaching:
//
//   - a package-level declaration of a non-test file (main's code: nosandbox.go and sandbox.go are
//     the two halves the tag picks between, and any other file of main can call into them);
//   - a declaration of a test file whose //go:build line names the tag;
//   - an import of this module's packages (internal/config is tag-built, and any internal package
//     may import it), and the few standard calls that read the test binary itself
//     (runtime/debug.ReadBuildInfo, os.Executable, os.Args).
//
// The walk is transitive through every test-file declaration the function names -- helpers, their
// helpers, package-level vars and their initialisers, types and their methods. A name the parser
// resolved to a declaration inside a function is that local; every other name is looked up by
// spelling among the package-level declarations of every file -- a composite literal's field key
// spelled like one counts as a reference to it, which errs toward "reaches" (a test wrongly kept in
// the sandbox run), never the other way.
func tagReach(t *testing.T, pkg string) map[string]string {
	t.Helper()
	dir := filepath.Join(repoRoot, filepath.FromSlash(pkg))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	type decl struct {
		node   ast.Node
		file   string
		test   bool
		tagged bool
	}
	decls := map[string][]decl{}
	methods := map[string][]decl{} // receiver type name -> its methods
	topLevel := map[any]bool{}     // every package-level declaration node, for the parser's scope objects
	imports := map[string]map[string]string{}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		tagged := false
		for _, cg := range f.Comments {
			if cg.Pos() >= f.Package {
				break
			}
			for _, c := range cg.List {
				if strings.HasPrefix(c.Text, "//go:build") && strings.Contains(c.Text, "sandbox") {
					tagged = true
				}
			}
		}
		imp := map[string]string{}
		for _, is := range f.Imports {
			p, _ := strconv.Unquote(is.Path.Value)
			local := p[strings.LastIndex(p, "/")+1:]
			if is.Name != nil {
				local = is.Name.Name
			}
			imp[local] = p
		}
		imports[name] = imp
		isTest := strings.HasSuffix(name, "_test.go")
		add := func(m map[string][]decl, key string, n ast.Node) {
			m[key] = append(m[key], decl{node: n, file: name, test: isTest, tagged: tagged})
			topLevel[n] = true
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					add(decls, d.Name.Name, d)
					continue
				}
				rt := d.Recv.List[0].Type
				if s, ok := rt.(*ast.StarExpr); ok {
					rt = s.X
				}
				if g, ok := rt.(*ast.IndexExpr); ok {
					rt = g.X
				}
				if id, ok := rt.(*ast.Ident); ok {
					add(methods, id.Name, d)
				}
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.ValueSpec:
						for _, n := range s.Names {
							add(decls, n.Name, s)
						}
					case *ast.TypeSpec:
						add(decls, s.Name.Name, s)
					}
				}
			}
		}
	}
	readsTheBinary := map[string]bool{"runtime/debug.ReadBuildInfo": true, "os.Executable": true, "os.Args": true}
	const module = "github.com/atknatk/tappa/"

	reach := func(root decl) string {
		seen := map[ast.Node]bool{}
		queue := []decl{root}
		why := ""
		var file string
		ident := func(id *ast.Ident) {
			// The parser resolves a name declared inside a function (a parameter, a `:=`, a local
			// closure) to that declaration: it is not the package-level one of the same spelling.
			if id.Obj != nil && !topLevel[id.Obj.Decl] {
				return
			}
			for _, d := range decls[id.Name] {
				switch {
				case !d.test:
					why = file + ": names " + id.Name + ", declared in " + d.file + " (main's code)"
					return
				case d.tagged:
					why = file + ": names " + id.Name + ", declared in the tagged " + d.file
					return
				}
				queue = append(queue, d)
				queue = append(queue, methods[id.Name]...)
			}
		}
		var visit func(n ast.Node) bool
		visit = func(n ast.Node) bool {
			if why != "" {
				return false
			}
			switch n := n.(type) {
			case *ast.SelectorExpr:
				if x, ok := n.X.(*ast.Ident); ok && len(decls[x.Name]) == 0 {
					if p, ok := imports[file][x.Name]; ok {
						if strings.HasPrefix(p, module) {
							why = file + ": imports " + p
						} else if readsTheBinary[p+"."+n.Sel.Name] {
							why = file + ": " + p + "." + n.Sel.Name + " reads the test binary"
						}
						return false
					}
				}
				// The selected name is a field or a method, not a package-level declaration.
				ast.Inspect(n.X, visit)
				return false
			case *ast.Ident:
				ident(n)
			}
			return true
		}
		for len(queue) > 0 && why == "" {
			d := queue[0]
			queue = queue[1:]
			if seen[d.node] {
				continue
			}
			seen[d.node] = true
			file = d.file
			ast.Inspect(d.node, visit)
		}
		return why
	}

	out := map[string]string{}
	for name, ds := range decls {
		if !strings.HasPrefix(name, "Test") || name == "TestMain" {
			continue
		}
		for _, d := range ds {
			if _, ok := d.node.(*ast.FuncDecl); !ok || !d.test {
				continue
			}
			if d.tagged {
				out[name] = d.file + ": the test's own file carries the tag"
				continue
			}
			out[name] = reach(d)
		}
	}
	return out
}

// sandboxSkip is the Makefile's SANDBOX_SKIP regular expression.
func sandboxSkip(t *testing.T) *regexp.Regexp {
	t.Helper()
	mk, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	var re string
	n := 0
	for _, l := range strings.Split(string(mk), "\n") {
		if v, ok := strings.CutPrefix(l, "SANDBOX_SKIP := "); ok {
			re = strings.TrimSpace(v)
			n++
		}
	}
	if n != 1 || re == "" {
		t.Fatalf("the Makefile defines SANDBOX_SKIP %d times (%q), want once", n, re)
	}
	r, err := regexp.Compile(re)
	if err != nil {
		t.Fatalf("SANDBOX_SKIP %q: %v", re, err)
	}
	return r
}

// TestSandboxSkip_SkipsOnlyTestsTheTagCannotChange: check-sandbox's test line passes
// -skip '$(SANDBOX_SKIP)'; every cmd/tappa test the expression matches reaches nothing the tag
// changes (tagReach); it matches no test of the other SANDBOX_TEST_PKGS packages (-skip spans all
// three); and it is not vacuous -- it matches the families it was written for, while the walk still
// finds the tag in the tests that exist to measure it.
//
// MUTANTS (measured 2026-10-19): SANDBOX_SKIP widened to `^Test`; redlineRun (every skipped redline
// test's helper) made to name main's parseLevel; -skip dropped from the recipe; `|^TestLoad`
// (internal/config's tests) added to the expression -- each red.
func TestSandboxSkip_SkipsOnlyTestsTheTagCannotChange(t *testing.T) {
	skip := sandboxSkip(t)
	recipe, err := makefileRecipe("check-sandbox")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(recipe, " -skip '$(SANDBOX_SKIP)' ") {
		t.Errorf("check-sandbox's test line does not pass -skip '$(SANDBOX_SKIP)': %q", recipe)
	}

	// Both halves of cmd/tappa's tests run under the same -skip (./cmd/tappa/... in
	// SANDBOX_TEST_PKGS); a test name is unique across the two (TestEveryNamedTestExists reads names).
	reach := tagReach(t, "cmd/tappa")
	for name, why := range tagReach(t, "cmd/tappa/scripttest") {
		if _, dup := reach[name]; dup {
			t.Errorf("%s is declared in both cmd/tappa and cmd/tappa/scripttest", name)
		}
		reach[name] = why
	}
	var skipped []string
	for name, why := range reach {
		if !skip.MatchString(name) {
			continue
		}
		skipped = append(skipped, name)
		if why != "" {
			t.Errorf("SANDBOX_SKIP matches %s, which the tag can change: %s", name, why)
		}
	}
	sort.Strings(skipped)
	t.Logf("SANDBOX_SKIP leaves %d of %d cmd/tappa and cmd/tappa/scripttest tests to `make check`", len(skipped), len(reach))

	// Not vacuous: the families the expression exists for are in it...
	for _, want := range []string{
		"TestRedline_T137_DynamicSQLAndDDLInABody", "TestRedline_TheLexersReadTheSameUnderEveryAwk",
		"TestRedline_R5bWaiverIsThe00058DownAlone", "TestSecretScan_AWaiverIsBoundToTheWholeLine",
		"TestPrePush_ScansWhatAPushPublishes",
	} {
		if _, ok := reach[want]; !ok {
			t.Errorf("PREMISE: %s is not a test of cmd/tappa or cmd/tappa/scripttest", want)
		} else if !skip.MatchString(want) {
			t.Errorf("SANDBOX_SKIP does not match %s", want)
		}
	}
	// ...and the walk is not blind: it finds both kinds of reach -- main's code
	// (announceKEKRotationWindow, main.go) and a module package the tag builds (internal/config).
	for name, want := range map[string]string{
		"TestAnnounceKEKRotationWindow":           "declared in main.go (main's code)",
		"TestConfig_UntaggedBinaryRefusesSandbox": "imports github.com/atknatk/tappa/internal/config",
	} {
		if why, ok := reach[name]; !ok {
			t.Errorf("CONTROL FAILED: no cmd/tappa test named %s", name)
		} else if !strings.Contains(why, want) {
			t.Errorf("CONTROL FAILED: tagReach says %s reaches %q, want %q", name, why, want)
		}
	}

	// -skip applies to every package on the line; the expression must name only cmd/tappa's tests.
	for _, pkg := range []string{"internal/config", "internal/sandbox"} {
		err := filepath.WalkDir(filepath.Join(repoRoot, pkg), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
				return err
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range regexp.MustCompile(`(?m)^func (Test\w*)\(`).FindAllStringSubmatch(string(b), -1) {
				if skip.MatchString(m[1]) {
					t.Errorf("SANDBOX_SKIP matches %s in %s: -skip would drop it from the sandbox run", m[1], path)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
