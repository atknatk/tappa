package db

// execmodetypes_test.go -- M10 OP-7: TRIPWIRES on the source, beside the pool-level rule.
//
// WHAT THIS FILE CLAIMS follows the three-part statement at logparams.go's
// boundParameterModes (2h): today's shipped code, measured; named pins and wires and
// exactly what each catches; no completeness claim. The pins and wires here are PART II:
//   - TestProductCode_ExecModeWireAndConnectWire: the MODE wire trips on the spellings in
//     execModeEscapes, the CONNECT wire on connectFuncs outside connectAllowed, the
//     POOLCFG wire on the written forms listed at poolConfigHooks -- and on nothing else
//     that is claimed. A hit is allowed only in a PACKAGE-LEVEL declaration named by its
//     wire, in the named file, by the file's own position (allowedHit: a method of that
//     name, or a //line directive naming that file, is not allowed -- 2i), and the allowed
//     counts are exact (16 mode uses, 2 openers, 1 hook write);
//   - TestProductCode_CarriesNoBuildConstraint: the constraint kinds buildConstraintOf
//     lists (a closed structural rule, as narrow as that list);
//   - TestConstructors_TheHookReachesOnlyThePin and TestConstructors_BodiesAreTheReviewedOnes:
//     the declarations they name, token for token.
// Any code change these do not list is code review's (PART III). (History: 2c's name-based
// scan, 2d's type-based one, 2e's per-connection layer and 2f/2g's pins were each written
// up wider than they measured, and each was walked around by the next audit.)

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/format"
	"go/importer"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	modulePath       = "github.com/atknatk/tappa"
	pgxPath          = "github.com/jackc/pgx/v5"
	pgxpoolPath      = "github.com/jackc/pgx/v5/pgxpool"
	pgconnPath       = "github.com/jackc/pgx/v5/pgconn"
	pgxtestPath      = "github.com/jackc/pgx/v5/pgxtest"
	execModeTypeName = "QueryExecMode"
)

// execModeAllowedFile is the ONE product file in which the wire allows a value of a type
// built from pgx.QueryExecMode, and execModeAllowedDecls the only declarations in it: the
// list of modes a pool accepts and the check that enforces it.
var (
	execModeAllowedFile  = filepath.Join("internal", "db", "logparams.go")
	execModeAllowedDecls = []string{"boundParameterModes", "requireBoundParameters"}
)

// connectFuncs are the functions that open a connection or a pool (2f, N-3): a third
// place that opens one would not pass through the two constructors' checks. The wire
// allows them in the two constructors only (connectAllowed: file -> declaration).
var (
	connectFuncs = map[string][]string{
		pgxPath:     {"Connect", "ConnectConfig", "ConnectWithOptions"},
		pgxpoolPath: {"New", "NewWithConfig"},
		pgconnPath:  {"Connect", "ConnectConfig", "ConnectWithOptions", "Construct"},
	}
	connectAllowed = map[string]string{
		filepath.Join("internal", "db", "pool.go"):         "newDB",
		filepath.Join("internal", "db", "operatorpool.go"): "openOperatorDB",
	}
)

// modulePackage is one package of this module as `go list` describes it. GoFiles are the
// non-test files THE BUILD BEING RUN compiles; IgnoredGoFiles, CgoFiles and
// IgnoredOtherFiles are what that build leaves out of GoFiles (a build tag that does not
// hold, a GOOS/GOARCH file suffix, cgo). 2e (the 2nd closing auditor, measured): a
// `//go:build !race` or `!cgo` product file is in production's file set (CGO_ENABLED=0,
// no -race: Makefile, Dockerfile) and out of the test's (-race, cgo on).
type modulePackage struct {
	ImportPath, Dir, Export                              string
	GoFiles, IgnoredGoFiles, CgoFiles, IgnoredOtherFiles []string
}

// moduleExports lists every package of this module (and pgxtest, for one positive
// control) with every dependency's EXPORT DATA, and returns the module's own packages with
// an importer that reads that export data -- internal/operatorauth's exactImports,
// widened to the whole module. The same guards: the go command must be the toolchain that
// built this test, the -race build asks for the -race export data (raceBuild), and every
// failure stops the test (fail-closed).
func moduleExports(t *testing.T, fset *token.FileSet) ([]modulePackage, types.Importer) {
	t.Helper()
	if v, err := exec.Command("go", "env", "GOVERSION").Output(); err != nil {
		t.Fatalf("go env GOVERSION: %v", err)
	} else if got := strings.TrimSpace(string(v)); got != runtime.Version() {
		t.Fatalf("the go command is %s and this test was built by %s: its export data would not be this toolchain's",
			got, runtime.Version())
	}
	args := []string{"list", "-export", "-deps", "-json=ImportPath,Dir,Export,GoFiles,IgnoredGoFiles,CgoFiles,IgnoredOtherFiles"}
	if raceBuild {
		args = append(args, "-race")
	}
	out, err := exec.Command("go", append(args, modulePath+"/...", pgxtestPath)...).Output()
	if err != nil {
		// go list's stderr is package, module and toolchain diagnostics; it does not echo
		// the environment (exactImports' comment in internal/operatorauth has the reading).
		var ee *exec.ExitError
		var stderr string
		if errors.As(err, &ee) {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		t.Fatalf("go list -export: %v: %s", err, stderr)
	}
	exports := map[string]string{}
	var own []modulePackage
	for dec := json.NewDecoder(bytes.NewReader(out)); ; {
		var p modulePackage
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("go list output: %v", err)
		}
		exports[p.ImportPath] = p.Export
		if p.ImportPath == modulePath || strings.HasPrefix(p.ImportPath, modulePath+"/") {
			own = append(own, p)
		}
	}
	if exports[pgxPath] == "" || exports[pgxtestPath] == "" {
		t.Fatalf("PREMISE: go list named no export data for %s or %s; the load has gone blind", pgxPath, pgxtestPath)
	}
	return own, importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		f, ok := exports[path]
		if !ok || f == "" {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(f)
	})
}

// isExecModeType reports whether ty is pgx.QueryExecMode, through any alias.
func isExecModeType(ty types.Type) bool {
	named, ok := types.Unalias(ty).(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == pgxPath && obj.Name() == execModeTypeName
}

// carriesExecMode reports whether ty is pgx.QueryExecMode or is BUILT from it: a pointer,
// slice, array, map, channel, signature or tuple with it inside, or a generic named
// type's type arguments (2f: the 3rd closing auditor's []pgx.QueryExecMode went through
// the identity check). It does not look inside a named type's underlying type or a
// struct's fields -- pgx.ConnConfig holds a QueryExecMode, and every use of it is not a
// mode being chosen. A wire, not a closure: what it misses is listed at the top.
func carriesExecMode(ty types.Type) bool {
	var walk func(types.Type, int) bool
	walk = func(ty types.Type, depth int) bool {
		if ty == nil || depth > 32 {
			return false
		}
		ty = types.Unalias(ty)
		if isExecModeType(ty) {
			return true
		}
		switch x := ty.(type) {
		case *types.Pointer:
			return walk(x.Elem(), depth+1)
		case *types.Slice:
			return walk(x.Elem(), depth+1)
		case *types.Array:
			return walk(x.Elem(), depth+1)
		case *types.Map:
			return walk(x.Key(), depth+1) || walk(x.Elem(), depth+1)
		case *types.Chan:
			return walk(x.Elem(), depth+1)
		case *types.Tuple:
			for i := 0; i < x.Len(); i++ {
				if walk(x.At(i).Type(), depth+1) {
					return true
				}
			}
		case *types.Signature:
			return walk(x.Params(), depth+1) || walk(x.Results(), depth+1)
		case *types.Named:
			args := x.TypeArgs()
			for i := 0; i < args.Len(); i++ {
				if walk(args.At(i), depth+1) {
					return true
				}
			}
		}
		return false
	}
	return walk(ty, 0)
}

// wireHit is one place the wire trips, with the top-level declaration it sits in.
type wireHit struct {
	pos  token.Position
	decl string
	what string // "mode", "connect" or "poolcfg"
}

// poolConfigHooks are the pgxpool.Config fields through which a pool's connections are
// configured or checked (2g): AfterConnect is where the per-connection check lives,
// BeforeConnect can rewrite a connection's configuration before it is made, and ConnConfig
// is the configuration itself. THE POOLCFG WIRE trips, outside pinLogParameters, on
// exactly these written forms (2h; poolConfigEscapes holds one of each): an assignment
// to the field (c.AfterConnect = ...), an assignment through it (*c.ConnConfig = ...), a
// range clause assigning it (for _, c.AfterConnect = range ...), an increment, taking its
// address (&c.AfterConnect), a composite literal's key (pgxpool.Config{BeforeConnect:
// ...}), and a whole value written at once of pgxpool.Config or of any type with its
// underlying struct (*c = *d; type C pgxpool.Config; *(*C)(c) = d -- 2i). It does not see
// a write to a field INSIDE ConnConfig (c.ConnConfig.Config = ...), through an embedding
// struct, reflect or unsafe -- those are code review's.
var poolConfigHooks = []string{"AfterConnect", "BeforeConnect", "ConnConfig"}

// isPoolConfigHook reports whether obj is one of poolConfigHooks on pgxpool.Config.
func isPoolConfigHook(obj types.Object) bool {
	v, ok := obj.(*types.Var)
	return ok && v.IsField() && v.Pkg() != nil && v.Pkg().Path() == pgxpoolPath && slices.Contains(poolConfigHooks, v.Name())
}

// wireHits type-checks one package with exact imports and returns where the two wires
// trip. MODE: an expression, a defined object or a generic instance's type argument whose
// type carries pgx.QueryExecMode (carriesExecMode), and every use of pgx.ConnConfig's
// DefaultQueryExecMode field. CONNECT: every use of a function in connectFuncs.
func wireHits(fset *token.FileSet, imp types.Importer, path string, files []*ast.File) ([]wireHit, error) {
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Uses:       map[*ast.Ident]types.Object{},
		Defs:       map[*ast.Ident]types.Object{},
		Instances:  map[*ast.Ident]types.Instance{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	if _, err := (&types.Config{Importer: imp}).Check(path, fset, files, info); err != nil {
		return nil, err
	}
	var out []wireHit
	// Positions are the FILE'S OWN (PositionFor with adjusted=false): a //line directive
	// could otherwise make a function in another file look as if it sat in an allowed one
	// (2i, the 6th closing auditor).
	add := func(n ast.Node, what string) {
		out = append(out, wireHit{fset.PositionFor(n.Pos(), false), enclosingDecl(files, n), what})
	}
	var configShape types.Type
	if pkg, err := imp.Import(pgxpoolPath); err == nil {
		if obj := pkg.Scope().Lookup("Config"); obj != nil {
			configShape = obj.Type().Underlying()
		}
	}
	if configShape == nil {
		return nil, fmt.Errorf("PREMISE: no pgxpool.Config in the export data")
	}
	for e, tv := range info.Types {
		if carriesExecMode(tv.Type) {
			add(e, "mode")
		}
	}
	for id, obj := range info.Defs {
		if obj != nil && carriesExecMode(obj.Type()) {
			add(id, "mode")
		}
	}
	for id, inst := range info.Instances {
		for i := 0; i < inst.TypeArgs.Len(); i++ {
			if carriesExecMode(inst.TypeArgs.At(i)) {
				add(id, "mode")
			}
		}
	}
	written := func(e ast.Expr) {
		e = ast.Unparen(e)
		// A whole value of pgxpool.Config, or of any type with its underlying struct (an
		// alias, `type C pgxpool.Config`), written at once (*c = *d, *(*C)(c) = d) replaces
		// every hook in it.
		if tv, ok := info.Types[e]; ok && tv.Type != nil && types.Identical(tv.Type.Underlying(), configShape) {
			add(e, "poolcfg")
			return
		}
		// *c.ConnConfig = ... writes through the hook field: the deref's operand is it.
		if star, ok := e.(*ast.StarExpr); ok {
			e = ast.Unparen(star.X)
		}
		if sel, ok := e.(*ast.SelectorExpr); ok {
			if s := info.Selections[sel]; s != nil && isPoolConfigHook(s.Obj()) {
				add(sel, "poolcfg")
			}
		}
	}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				for _, l := range x.Lhs {
					written(l)
				}
			case *ast.IncDecStmt:
				written(x.X)
			case *ast.RangeStmt:
				if x.Tok == token.ASSIGN {
					if x.Key != nil {
						written(x.Key)
					}
					if x.Value != nil {
						written(x.Value)
					}
				}
			case *ast.UnaryExpr:
				if x.Op == token.AND {
					written(x.X)
				}
			case *ast.KeyValueExpr:
				if id, ok := x.Key.(*ast.Ident); ok && isPoolConfigHook(info.Uses[id]) {
					add(id, "poolcfg")
				}
			}
			return true
		})
	}
	for id, obj := range info.Uses {
		if v, ok := obj.(*types.Var); ok && v.IsField() && v.Name() == "DefaultQueryExecMode" &&
			v.Pkg() != nil && v.Pkg().Path() == pgxPath {
			add(id, "mode")
		}
		if f, ok := obj.(*types.Func); ok && f.Pkg() != nil && slices.Contains(connectFuncs[f.Pkg().Path()], f.Name()) {
			if sig, _ := f.Type().(*types.Signature); sig != nil && sig.Recv() == nil {
				add(id, "connect")
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].pos.String() < out[j].pos.String() })
	return out, nil
}

// enclosingDecl names the top-level declaration that holds n: a function's name, or the
// names a var, const or type declaration declares.
func enclosingDecl(files []*ast.File, n ast.Node) string {
	for _, f := range files {
		if n.Pos() < f.FileStart || n.Pos() >= f.FileEnd {
			continue
		}
		for _, d := range f.Decls {
			if n.Pos() < d.Pos() || n.Pos() >= d.End() {
				continue
			}
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv != nil {
					// A METHOD is never one of the allowed package-level declarations, whatever
					// its name (2i: a method called requireBoundParameters in logparams.go
					// was counted as allowed).
					return "(method) " + d.Name.Name
				}
				return d.Name.Name
			case *ast.GenDecl:
				var names []string
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.ValueSpec:
						for _, id := range s.Names {
							names = append(names, id.Name)
						}
					case *ast.TypeSpec:
						names = append(names, s.Name.Name)
					}
				}
				return strings.Join(names, ",")
			}
		}
	}
	return ""
}

// allowedHit reports whether a wire hit sits in its wire's allowed region: the file named
// by its UNADJUSTED position (rel, relative to the module root) and a PACKAGE-LEVEL
// declaration of the allowed name -- enclosingDecl names a method "(method) <name>", so a
// method never matches.
func allowedHit(h wireHit, rel string) bool {
	switch h.what {
	case "mode":
		return rel == execModeAllowedFile && slices.Contains(execModeAllowedDecls, h.decl)
	case "connect":
		return connectAllowed[rel] == h.decl
	case "poolcfg":
		return rel == execModeAllowedFile && h.decl == "pinLogParameters"
	}
	return false
}

// execModeEscapes are the spellings the MODE wire is known to catch, each a whole Go
// file -- the list IS the wire's claim, and nothing beyond it is claimed: the closing
// auditors' (a dot import, a conversion, arithmetic, an untyped constant into the field,
// pgxtest's list through an inferred generic), the name 2c's scan looked for, and the
// self-audits' (a generic instance, an alias, a type assertion out of `any` -- the S1
// precedent --, a struct literal's key, a slice through an inferred generic).
var execModeEscapes = map[string]string{
	"a dot import":       `package c; import . "github.com/jackc/pgx/v5"; var _ = QueryExecModeSimpleProtocol`,
	"a conversion":       `package c; import "github.com/jackc/pgx/v5"; var _ = pgx.QueryExecMode(5)`,
	"arithmetic":         `package c; import "github.com/jackc/pgx/v5"; var _ = pgx.QueryExecModeExec + 1`,
	"an untyped field":   `package c; import "github.com/jackc/pgx/v5"; func f(c *pgx.ConnConfig) { c.DefaultQueryExecMode = 5 }`,
	"the named constant": `package c; import "github.com/jackc/pgx/v5"; var _ = pgx.QueryExecModeSimpleProtocol`,
	"a generic instance": `package c; import "github.com/jackc/pgx/v5"; func conv[T ~int32](v int32) T { return T(v) }; var _ any = conv[pgx.QueryExecMode](5)`,
	"an alias":           `package c; import "github.com/jackc/pgx/v5"; type M = pgx.QueryExecMode; var _ = M(5)`,
	"out of any (S1)":    `package c; import "github.com/jackc/pgx/v5"; var a any = int32(5); var _, _ = a.(pgx.QueryExecMode)`,
	"a literal's key":    `package c; import "github.com/jackc/pgx/v5"; var _ = pgx.ConnConfig{DefaultQueryExecMode: 5}`,
	"pgxtest's list through an inferred generic (2f)": `package c; import "github.com/jackc/pgx/v5/pgxtest"
func last[S ~[]E, E any](s S) any { return s[len(s)-1] }
var _ = last(pgxtest.AllQueryExecModes)`,
	"a function value returning a mode": `package c; import "github.com/jackc/pgx/v5/pgxtest"
func pick(f func() any) any { return f() }
var _ = pick(func() any { m := pgxtest.AllQueryExecModes; return m[0] })`,
}

// poolConfigEscapes are the spellings the POOLCFG wire is known to catch (2g).
var poolConfigEscapes = map[string]string{
	"AfterConnect = nil":        `package c; import "github.com/jackc/pgx/v5/pgxpool"; func f(c *pgxpool.Config) { c.AfterConnect = nil }`,
	"ConnConfig replaced":       `package c; import "github.com/jackc/pgx/v5/pgxpool"; func f(c, d *pgxpool.Config) { c.ConnConfig = d.ConnConfig }`,
	"BeforeConnect in a lit":    `package c; import "github.com/jackc/pgx/v5/pgxpool"; var _ = pgxpool.Config{BeforeConnect: nil}`,
	"address of the hook":       `package c; import "github.com/jackc/pgx/v5/pgxpool"; func f(c *pgxpool.Config) { p := &c.AfterConnect; *p = nil }`,
	"through the field (2h)":    `package c; import "github.com/jackc/pgx/v5/pgxpool"; func f(c, d *pgxpool.Config) { *c.ConnConfig = *d.ConnConfig }`,
	"a range clause (2h)":       `package c; import ("context"; "github.com/jackc/pgx/v5"; "github.com/jackc/pgx/v5/pgxpool"); func f(c *pgxpool.Config, l []func(context.Context, *pgx.Conn) error) { for _, c.AfterConnect = range l {} }`,
	"the whole config (2h)":     `package c; import "github.com/jackc/pgx/v5/pgxpool"; func f(c, d *pgxpool.Config) { *c = *d }`,
	"a type defined on it (2i)": `package c; import "github.com/jackc/pgx/v5/pgxpool"; type C pgxpool.Config; func f(c *pgxpool.Config, d C) { *(*C)(c) = d }`,
}

// connectEscapes are the spellings the CONNECT wire is known to catch.
var connectEscapes = map[string]string{
	"pgx.Connect":            `package c; import ("context"; "github.com/jackc/pgx/v5"); func f() { pgx.Connect(context.Background(), "") }`,
	"pgxpool.New as a value": `package c; import "github.com/jackc/pgx/v5/pgxpool"; var open = pgxpool.New`,
	"pgconn.ConnectConfig":   `package c; import "github.com/jackc/pgx/v5/pgconn"; var _ = pgconn.ConnectConfig`,
}

// TestProductCode_ExecModeWireAndConnectWire are the tripwires (see the file comment for
// what they do and do not claim): no product Go file outside logparams.go's two
// declarations holds a value whose type carries pgx.QueryExecMode, and no product file
// outside the two constructors opens a connection or a pool. Each package is
// type-checked against its dependencies' exact export data.
//
// THE FILES THE WIRES SEE: the packages checked are EXACTLY the module's packages `go
// list` names (a walk that skipped one is red), and a non-test file the running build
// leaves out (IgnoredGoFiles, CgoFiles, IgnoredOtherFiles) is red here;
// TestProductCode_CarriesNoBuildConstraint covers, from text alone, the ways listed there
// that a file could be left out of another build.
//
// POSITIVE CONTROLS: every spelling in execModeEscapes and connectEscapes trips its wire;
// the uses inside the allowed declarations are counted, so wires that saw nothing would
// fail here rather than pass.
func TestProductCode_ExecModeWireAndConnectWire(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, imp := moduleExports(t, fset)

	for _, set := range []struct {
		what      string
		spellings map[string]string
	}{{"mode", execModeEscapes}, {"connect", connectEscapes}, {"poolcfg", poolConfigEscapes}} {
		for name, src := range set.spellings {
			f, err := parser.ParseFile(fset, "control.go", src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("control %q: %v", name, err)
			}
			hits, err := wireHits(fset, imp, "example.com/control", []*ast.File{f})
			if err != nil {
				t.Fatalf("control %q: %v", name, err)
			}
			if !slices.ContainsFunc(hits, func(h wireHit) bool { return h.what == set.what }) {
				t.Errorf("POSITIVE CONTROL FAILED: the %s wire did not trip on %s", set.what, name)
			}
		}
	}

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	// ALLOWED-REGION CONTROLS (2i): a method with an allowed declaration's name, written in
	// the allowed file, and a function behind a //line directive that names the allowed
	// file, both trip the mode wire and neither counts as allowed.
	for name, c := range map[string]struct{ file, src string }{
		"a method named requireBoundParameters in logparams.go": {
			filepath.Join(root, execModeAllowedFile),
			"package zz\n\nimport \"github.com/jackc/pgx/v5\"\n\ntype zz struct{}\n\n" +
				"func (zz) requireBoundParameters() any { return pgx.QueryExecModeSimpleProtocol }\n"},
		"a function behind //line ../db/logparams.go": {
			filepath.Join(root, "internal", "zzline", "x.go"),
			"package zz\n\nimport \"github.com/jackc/pgx/v5\"\n\n//line ../db/logparams.go:200\n" +
				"func requireBoundParameters() any { return pgx.QueryExecModeSimpleProtocol }\n"},
	} {
		f, err := parser.ParseFile(fset, c.file, c.src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("control %q: %v", name, err)
		}
		hits, err := wireHits(fset, imp, "example.com/zz", []*ast.File{f})
		if err != nil {
			t.Fatalf("control %q: %v", name, err)
		}
		modes := 0
		for _, h := range hits {
			rel, err := filepath.Rel(root, h.pos.Filename)
			if err != nil {
				t.Fatal(err)
			}
			if h.what == "mode" {
				modes++
			}
			if allowedHit(h, rel) {
				t.Errorf("ALLOWED-REGION CONTROL FAILED: %s is counted as allowed (%s, in %q)", name, rel, h.decl)
			}
		}
		if modes == 0 {
			t.Errorf("ALLOWED-REGION CONTROL FAILED: %s did not trip the mode wire", name)
		}
		// The //line control is not vacuous: the ADJUSTED position does name the allowed file.
		if strings.Contains(c.src, "//line") {
			fn := f.Decls[len(f.Decls)-1]
			if adj := fset.PositionFor(fn.Pos(), true).Filename; filepath.Base(adj) != "logparams.go" {
				t.Errorf("CONTROL PREMISE: the //line directive did not take (adjusted file %q)", adj)
			}
		}
	}
	want := modulePackagePaths(t)
	var packages, files, allowedModes, allowedConnects, allowedHooks int
	var checked []string
	for _, p := range pkgs {
		for _, list := range [][]string{p.IgnoredGoFiles, p.CgoFiles, p.IgnoredOtherFiles} {
			for _, name := range list {
				if !strings.HasSuffix(name, "_test.go") {
					t.Errorf("%s: %s is a product file this build leaves out of the wires (a build constraint, a "+
						"GOOS/GOARCH suffix or cgo): another build would compile it unchecked", p.ImportPath, name)
				}
			}
		}
		var parsed []*ast.File
		for _, name := range p.GoFiles {
			f, err := parser.ParseFile(fset, filepath.Join(p.Dir, name), nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse %s: %v", name, err)
			}
			parsed = append(parsed, f)
		}
		hits, err := wireHits(fset, imp, p.ImportPath, parsed)
		if err != nil {
			t.Fatalf("type-check %s with exact imports: %v", p.ImportPath, err)
		}
		packages++
		files += len(parsed)
		checked = append(checked, p.ImportPath)
		for _, h := range hits {
			rel, err := filepath.Rel(root, h.pos.Filename)
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case allowedHit(h, rel) && h.what == "mode":
				allowedModes++
			case allowedHit(h, rel) && h.what == "connect":
				allowedConnects++
			case allowedHit(h, rel) && h.what == "poolcfg":
				allowedHooks++
			case h.what == "poolcfg":
				t.Errorf("%s:%d:%d (in %q) writes a pgxpool.Config hook (AfterConnect, BeforeConnect or ConnConfig): "+
					"only pinLogParameters may, because its AfterConnect is where every connection is checked",
					rel, h.pos.Line, h.pos.Column, h.decl)
			case h.what == "mode":
				t.Errorf("%s:%d:%d (in %q) holds a value of a type built from pgx.QueryExecMode: choosing a query "+
					"exec mode is logparams.go's alone (boundParameterModes)", rel, h.pos.Line, h.pos.Column, h.decl)
			default:
				t.Errorf("%s:%d:%d (in %q) opens a connection or a pool: only the two constructors (newDB, "+
					"openOperatorDB) may, because their checks are what a connection passes through",
					rel, h.pos.Line, h.pos.Column, h.decl)
			}
		}
	}
	t.Logf("type-checked %d product packages, %d non-test Go files; %d mode uses in %s's allowed declarations, "+
		"%d connection openers in the two constructors, %d hook writes in pinLogParameters",
		packages, files, allowedModes, execModeAllowedFile, allowedConnects, allowedHooks)
	sort.Strings(checked)
	if !slices.Equal(checked, want) {
		t.Fatalf("the packages type-checked are not the module's packages:\nchecked: %v\ngo list: %v", checked, want)
	}
	if packages < 30 || files < 150 {
		t.Fatalf("only %d packages and %d files type-checked; the module walk has gone blind", packages, files)
	}
	// EXACT counts, all three (2i: the mode count was a floor). A change to the allowed
	// declarations moves them, and is to be checked against what changed.
	if allowedModes != 16 || allowedConnects != 2 || allowedHooks != 1 {
		t.Fatalf("%d mode uses in the allowed declarations (want exactly 16, measured), %d connection openers in "+
			"the constructors (want 2) and %d hook writes in pinLogParameters (want 1: AfterConnect)",
			allowedModes, allowedConnects, allowedHooks)
	}
}

// modulePackagePaths is `go list`'s own answer to "which packages does this module have",
// sorted: the set the wires must check, exactly.
func modulePackagePaths(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("go", "list", "-f", "{{.ImportPath}}", modulePath+"/...").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	paths := strings.Fields(string(out))
	sort.Strings(paths)
	if len(paths) < 30 {
		t.Fatalf("go list names only %d packages under %s", len(paths), modulePath)
	}
	return paths
}

// knownPorts are the GOOS and GOARCH names go/build recognises in a file-name suffix,
// read from the running toolchain's own list (GOROOT/src/internal/syslist/syslist.go:
// KnownOS, KnownArch -- go/build's goodOSArchFile uses exactly these), so the list is
// neither typed out here nor narrower than go/build's (2f: `go tool dist list` lacked
// zos, hurd, nacl and others go/build still honours).
func knownPorts(t *testing.T) (goos, goarch map[string]bool) {
	t.Helper()
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatalf("go env GOROOT: %v", err)
	}
	path := filepath.Join(strings.TrimSpace(string(out)), "src", "internal", "syslist", "syslist.go")
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("read go/build's port list: %v", err)
	}
	lists := map[string]map[string]bool{"KnownOS": {}, "KnownArch": {}}
	ast.Inspect(f, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || len(spec.Values) != 1 || lists[spec.Names[0].Name] == nil {
			return true
		}
		lit, ok := spec.Values[0].(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, el := range lit.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				if key, ok := kv.Key.(*ast.BasicLit); ok {
					if s, err := strconv.Unquote(key.Value); err == nil {
						lists[spec.Names[0].Name][s] = true
					}
				}
			}
		}
		return true
	})
	goos, goarch = lists["KnownOS"], lists["KnownArch"]
	for _, must := range []string{"linux", "darwin", "zos", "hurd", "nacl"} {
		if !goos[must] {
			t.Fatalf("go/build's OS list lacks %q; the list has gone blind (%d names)", must, len(goos))
		}
	}
	for _, must := range []string{"amd64", "arm64", "amd64p32", "arm64be"} {
		if !goarch[must] {
			t.Fatalf("go/build's arch list lacks %q; the list has gone blind (%d names)", must, len(goarch))
		}
	}
	return goos, goarch
}

// buildConstraintOf says why a product Go file could be left out of some build, or "".
// It reads the file's NAME and TEXT only, so its answer does not depend on GOOS, GOARCH,
// CGO_ENABLED or -race; the port names come from the running toolchain's go/build list
// (knownPorts). WHAT IT COVERS, AND ONLY THAT:
//   - a name go ignores: a leading "_" or ".";
//   - a GOOS/GOARCH name suffix, parsed as go/build's goodOSArchFile parses it: the name
//     cut at its FIRST dot, everything before the first "_" dropped, a trailing "_test"
//     dropped, then a GOOS or GOARCH last element (or GOOS_GOARCH) -- so
//     x_linux.impl.go and a_arm64.impl.go count;
//   - a //go:build or // +build line before the package clause;
//   - cgo: an import whose path, unquoted (either quote style), is "C".
//
// Not covered: a file go/build would leave out for a reason not on this list (none is
// known to apply to a .go file of this module); a port a future toolchain adds is covered
// once that toolchain runs this test.
func buildConstraintOf(name string, src []byte, goos, goarch map[string]bool) (string, error) {
	base := filepath.Base(name)
	if strings.HasPrefix(base, "_") || strings.HasPrefix(base, ".") {
		return "a file name go ignores", nil
	}
	stem, _, _ := strings.Cut(base, ".")
	if i := strings.Index(stem, "_"); i >= 0 {
		l := strings.Split(stem[i:], "_")
		if n := len(l); n > 0 && l[n-1] == "test" {
			l = l[:n-1]
		}
		if n := len(l); n >= 1 && (goos[l[n-1]] || goarch[l[n-1]]) {
			return "a GOOS/GOARCH file-name suffix", nil
		}
	}
	f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return "", err
	}
	for _, g := range f.Comments {
		if g.Pos() >= f.Package {
			break
		}
		for _, c := range g.List {
			if constraint.IsGoBuild(c.Text) || constraint.IsPlusBuild(c.Text) {
				return "a build constraint line", nil
			}
		}
	}
	for _, imp := range f.Imports {
		if path, err := strconv.Unquote(imp.Path.Value); err == nil && path == "C" {
			return "cgo (import \"C\")", nil
		}
	}
	return "", nil
}

// TestProductCode_CarriesNoBuildConstraint: no product Go file of the module carries any
// of the constraint kinds buildConstraintOf lists -- so, for those kinds, the files the
// wires see in the build they run in are the files of production's build too
// (CGO_ENABLED=0, no -race) and of the tests' and CI's (-race, cgo on). Measured when it was written: no such product file
// exists, so the allowed list is EMPTY; a file that needs one is a decision to write here
// with its reason.
//
// The walk reads every non-test .go file under the module root except in .git and in
// nested modules (a directory holding a go.mod FILE) -- INCLUDING the directories go
// itself skips when matching ./... (names starting with "." or "_", testdata), because a
// product package may import one of them and then it is compiled (2i, the 6th closing
// auditor; measured when written: no .go file in such a directory). It does NOT follow a
// symbolic link to a directory (filepath.WalkDir does not); a package reached that way and
// imported by a product package shows up instead as a difference between the packages
// TestProductCode_ExecModeWireAndConnectWire type-checks and `go list`'s set (2j, the 7th
// closing auditor). Only _test.go files are exempt. POSITIVE CONTROL: every
// shape the closing auditors tried is recognised.
func TestProductCode_CarriesNoBuildConstraint(t *testing.T) {
	goos, goarch := knownPorts(t)
	for _, probe := range []struct{ name, src string }{
		{"x.go", "//go:build !race\n\npackage x\n"},
		{"x.go", "//go:build !cgo\n\npackage x\n"},
		{"x.go", "//go:build ignore\n\npackage x\n"},
		{"x.go", "// +build !race\n\npackage x\n"},
		{"x.go", "// Copyright.\n\n//go:build linux\n\npackage x\n"},
		{"x_linux.go", "package x\n"},
		{"x_amd64.go", "package x\n"},
		{"x_linux_arm64.go", "package x\n"},
		{"zzmode_linux.impl.go", "package x\n"},
		{"a_arm64.impl.go", "package x\n"},
		{"x_zos.go", "package x\n"},
		{"x_hurd.go", "package x\n"},
		{"x_nacl.go", "package x\n"},
		{"x_arm64be.go", "package x\n"},
		{"x.go", "package x\n\nimport \"C\"\n"},
		{"x.go", "package x\n\nimport `C`\n"},
		{"_x.go", "package x\n"},
	} {
		why, err := buildConstraintOf(probe.name, []byte(probe.src), goos, goarch)
		if err != nil || why == "" {
			t.Errorf("POSITIVE CONTROL FAILED: %s %q was not recognised (err %v)", probe.name, probe.src, err)
		}
	}
	for _, clean := range []struct{ name, src string }{
		{"linux.go", "package x\n"},
		{"linux.impl.go", "package x\n"},
		{"x_test_helper.go", "package x\n"},
		{"x.go", "package x\n\n//go:build linux\n"},
	} {
		if why, err := buildConstraintOf(clean.name, []byte(clean.src), goos, goarch); err != nil || why != "" {
			t.Errorf("CONTROL: %s %q read as constrained (%q, err %v)", clean.name, clean.src, why, err)
		}
	}

	// WALK CONTROL (2i): the walk enters the directories go skips when matching ./...
	// ("_x", ".x", testdata) and leaves a nested module alone.
	tmp := t.TempDir()
	for path, src := range map[string]string{
		filepath.Join(tmp, "_zz", "a_linux.go"):         "package zz\n",
		filepath.Join(tmp, "testdata", "b.go"):          "//go:build ignore\n\npackage zz\n",
		filepath.Join(tmp, ".hidden", "c_amd64.go"):     "package zz\n",
		filepath.Join(tmp, "sub", "go.mod"):             "module example.com/sub\n",
		filepath.Join(tmp, "sub", "d_linux.go"):         "package sub\n",
		filepath.Join(tmp, "plain", "e.go"):             "package plain\n",
		filepath.Join(tmp, "plain", "f_linux_test.go"):  "package plain\n",
		filepath.Join(tmp, "dirmod", "go.mod", "x.txt"): "",
		filepath.Join(tmp, "dirmod", "g_linux.go"):      "package dirmod\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, n, err := constrainedProductFiles(tmp, goos, goarch)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(".hidden", "c_amd64.go"), filepath.Join("_zz", "a_linux.go"),
		filepath.Join("dirmod", "g_linux.go"), filepath.Join("testdata", "b.go")}
	var gotNames []string
	for _, g := range got {
		gotNames = append(gotNames, g.rel)
	}
	sort.Strings(gotNames)
	if !slices.Equal(gotNames, want) || n != 5 {
		t.Errorf("WALK CONTROL FAILED: found %v among %d files, want %v among 5 (the nested module and the "+
			"_test.go file are not read; a directory NAMED go.mod is not a nested module)", gotNames, n, want)
	}

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	found, files, err := constrainedProductFiles(root, goos, goarch)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		t.Errorf("%s carries %s: some build would compile it outside the wires' file set", f.rel, f.why)
	}
	t.Logf("%d product Go files read", files)
	if files < 150 {
		t.Fatalf("only %d product Go files read; the walk has gone blind", files)
	}
}

// constrainedFile is one product Go file buildConstraintOf objects to.
type constrainedFile struct{ rel, why string }

// constrainedProductFiles reads every non-test .go file under root except in .git and in
// nested modules (a directory holding a go.mod FILE) -- including the directories go
// skips when matching ./..., and not following symbolic links to directories -- and
// returns those carrying a listed constraint, with the number of files read.
func constrainedProductFiles(root string, goos, goarch map[string]bool) ([]constrainedFile, int, error) {
	var out []constrainedFile
	var files int
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path == root {
				return nil
			}
			if name == ".git" {
				return filepath.SkipDir
			}
			// A nested module is a go.mod FILE (2j: a directory named go.mod is not one).
			if fi, err := os.Stat(filepath.Join(path, "go.mod")); err == nil && !fi.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		why, err := buildConstraintOf(path, src, goos, goarch)
		if err != nil {
			return err
		}
		if why != "" {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			out = append(out, constrainedFile{rel, why})
		}
		files++
		return nil
	})
	return out, files, err
}

// TestConstructors_TheHookReachesOnlyThePin is a SOURCE pin (2f, N-2): the operator
// pool's behaviour is measured through its test hook (openOperatorDB with asOperator,
// TestPools_KeepArgumentsOutOfTheStatementText). What this test pins is narrower than
// "the hookless path is the measured path" (2g: a branch on something else -- the
// environment, the user name -- passed it): only how the hook and the two exported
// constructors are written. The bodies themselves are pinned token for token by
// TestConstructors_BodiesAreTheReviewedOnes. Pinned here, from the syntax tree:
//   - New is exactly `return newDB(ctx, cfg, nil)` and NewOperatorDB exactly
//     `return openOperatorDB(ctx, cfg.OperatorDatabaseURL, nil)`;
//   - in newDB and openOperatorDB, `before` is used in ONE place: as the third argument
//     of pinLogParameters, called as a statement of the function's own body (not under
//     any condition).
//
// The 3rd closing auditor's D1 (the pin only when before != nil) and D2 (the hookless
// path re-parses the DSN with simple_protocol) both use `before` somewhere else.
func TestConstructors_TheHookReachesOnlyThePin(t *testing.T) {
	fset := token.NewFileSet()
	decls := map[string]*ast.FuncDecl{}
	for _, file := range []string{"pool.go", "operatorpool.go"} {
		f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil {
				decls[fd.Name.Name] = fd
			}
		}
	}
	body := func(name string) string {
		t.Helper()
		fd := decls[name]
		if fd == nil {
			t.Fatalf("no func %s", name)
		}
		var b bytes.Buffer
		if err := format.Node(&b, fset, fd.Body); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	for name, want := range map[string]string{
		"New":           "{\n\treturn newDB(ctx, cfg, nil)\n}",
		"NewOperatorDB": "{\n\treturn openOperatorDB(ctx, cfg.OperatorDatabaseURL, nil)\n}",
	} {
		if got := body(name); got != want {
			t.Errorf("%s's body is\n%s\nwant exactly\n%s", name, got, want)
		}
	}
	for _, name := range []string{"newDB", "openOperatorDB"} {
		fd := decls[name]
		if fd == nil {
			t.Fatalf("no func %s", name)
		}
		var asThePinsArg, elsewhere int
		var stack []ast.Node
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			if id, ok := n.(*ast.Ident); ok && id.Name == "before" {
				parent := stack[len(stack)-1]
				call, ok := parent.(*ast.CallExpr)
				fun, _ := call.Fun.(*ast.Ident)
				stmt, isStmt := stack[len(stack)-2].(*ast.ExprStmt)
				if ok && fun != nil && fun.Name == "pinLogParameters" && len(call.Args) == 3 && call.Args[2] == id &&
					isStmt && slices.Contains(fd.Body.List, ast.Stmt(stmt)) {
					asThePinsArg++
				} else {
					elsewhere++
				}
			}
			stack = append(stack, n)
			return true
		})
		if asThePinsArg != 1 || elsewhere != 0 {
			t.Errorf("%s uses its hook %d time(s) as pinLogParameters' third argument in a statement of its own "+
				"body and %d time(s) elsewhere; want 1 and 0 -- the hookless path production takes must be the "+
				"measured path minus the hook", name, asThePinsArg, elsewhere)
		}
	}
}

// reviewedBodies are the bodies of the functions every pool's checks run through, as
// last reviewed (2g, the 4th closing auditor: a branch added to newDB or openOperatorDB
// -- on cfg.IsProd(), on the user name -- re-parsed the DSN with simple_protocol after the
// pin, took the per-connection check with it, and every test stayed green). They are
// compared token for token: a change to the TOKENS of these declarations turns the test
// red until the text here is updated on purpose. What is outside these declarations is
// not pinned by this test (logparams.go, PART III of the claim).
var reviewedBodies = map[string]string{
	"newDB": `{
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, errors.New("db: DATABASE_URL is not a valid PostgreSQL connection string (the parser's " +
			"message is not repeated: it quotes the value)")
	}
	if err := requireBoundParameters(poolCfg.ConnConfig, "DATABASE_URL", true); err != nil {
		return nil, err
	}
	pinLogParameters(poolCfg, "DATABASE_URL", before)
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("db: new pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	d := &DB{pool: pool}
	facts, err := d.readRole(ctx)
	if err != nil {
		pool.Close()
		return nil, err
	}
	if err := roleRefusal(facts, cfg.IsProd()); err != nil {
		pool.Close()
		return nil, err
	}
	d.role = facts
	return d, nil
}`,
	"openOperatorDB": `{
	if dsn == "" {
		return nil, errors.New("db: operator pool: TAPPA_OPERATOR_DATABASE_URL is empty; a process with no operator " +
			"configuration does not open this pool at all")
	}
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("db: operator pool: TAPPA_OPERATOR_DATABASE_URL is not a valid PostgreSQL connection " +
			"string (the parser's message is not repeated: it quotes the value)")
	}
	if err := requireBoundParameters(poolCfg.ConnConfig, "TAPPA_OPERATOR_DATABASE_URL", true); err != nil {
		return nil, err
	}
	pinLogParameters(poolCfg, "TAPPA_OPERATOR_DATABASE_URL", before)
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, operatorStepErr("build the pool", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, operatorConnectErr(err)
	}
	facts, err := readOperatorRole(ctx, pool)
	if err != nil {
		pool.Close()
		return nil, err
	}
	if err := operatorRoleRefusal(facts); err != nil {
		pool.Close()
		return nil, err
	}
	return &OperatorDB{pool: pool}, nil
}`,
	"pinLogParameters": `{
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	for k := range cfg.ConnConfig.RuntimeParams {
		if strings.EqualFold(k, logParameterPin) {
			delete(cfg.ConnConfig.RuntimeParams, k)
		}
	}
	cfg.ConnConfig.RuntimeParams[logParameterPin] = "0"
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		if before != nil {
			if err := before(ctx, c); err != nil {
				return err
			}
		}
		if err := requireBoundParameters(c.Config(), variable, false); err != nil {
			return err
		}
		return requireLogParametersPinned(ctx, c)
	}
}`,
	// Not a body: boundParameterModes' INITIALIZER (2h, the 5th closing auditor: the list
	// could become a call that adds simple_protocol in production, and the comparison in
	// TestQueryExecModes_OnlyTheListedOnesBindOnTheServer runs in the test's environment).
	"boundParameterModes": `[]pgx.QueryExecMode{
	pgx.QueryExecModeCacheStatement,
	pgx.QueryExecModeCacheDescribe,
	pgx.QueryExecModeDescribeExec,
	pgx.QueryExecModeExec,
}`,
	"requireBoundParameters": `{
	if slices.Contains(boundParameterModes, cfg.DefaultQueryExecMode) {
		return nil
	}
	return &execModeRefusedError{variable: variable, fromDSN: fromDSN,
		mode: strings.ReplaceAll(cfg.DefaultQueryExecMode.String(), " ", "_")}
}`,
}

// codeTokens is src as Go tokens -- comments, blank lines and layout dropped, every
// semicolon (written or inserted at a line end) spelled ";" -- so a change to a comment or
// to formatting is not a change, and a change to the code is.
func codeTokens(t *testing.T, src string) string {
	t.Helper()
	fset := token.NewFileSet()
	file := fset.AddFile("body", fset.Base(), len(src))
	var s scanner.Scanner
	var errs int
	s.Init(file, []byte(src), func(token.Position, string) { errs++ }, 0)
	var out []string
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		switch {
		case tok == token.SEMICOLON:
			out = append(out, ";")
		case lit != "":
			out = append(out, lit)
		default:
			out = append(out, tok.String())
		}
	}
	if errs != 0 {
		t.Fatalf("the text does not scan as Go (%d errors)", errs)
	}
	return strings.Join(out, " ")
}

// TestConstructors_BodiesAreTheReviewedOnes (2g) pins the bodies of newDB, openOperatorDB,
// pinLogParameters and requireBoundParameters to reviewedBodies, token for token. A red
// here is not a regression to fix around: it means a function every pool's checks run
// through has changed, and before the text above is updated the change must be
// re-verified -- the behaviour (TestPools_KeepArgumentsOutOfTheStatementText, the customer
// pool in dev AND production env), the per-connection check
// (TestPin_AModeChangedAfterTheCheckIsRefusedOnItsConnection), and the pool-level claim
// in ADR 0021's OP-7 note.
func TestConstructors_BodiesAreTheReviewedOnes(t *testing.T) {
	fset := token.NewFileSet()
	got := map[string]string{}
	for _, file := range []string{"pool.go", "operatorpool.go", "logparams.go"} {
		f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			var name string
			var node ast.Node
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					name, node = d.Name.Name, d.Body
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if vs, ok := spec.(*ast.ValueSpec); ok && len(vs.Names) == 1 && len(vs.Values) == 1 {
						if reviewedBodies[vs.Names[0].Name] != "" {
							name, node = vs.Names[0].Name, vs.Values[0]
						}
					}
				}
			}
			if node == nil || reviewedBodies[name] == "" {
				continue
			}
			var b bytes.Buffer
			if err := format.Node(&b, fset, node); err != nil {
				t.Fatal(err)
			}
			got[name] = b.String()
		}
	}
	for name, want := range reviewedBodies {
		body, ok := got[name]
		if !ok {
			t.Errorf("%s is gone; the pool-level claim rests on it", name)
			continue
		}
		if codeTokens(t, body) != codeTokens(t, want) {
			t.Errorf("%s changed from its reviewed body. Before updating reviewedBodies, re-verify: "+
				"TestPools_KeepArgumentsOutOfTheStatementText (both pools; the customer pool in dev and production "+
				"env), TestPin_AModeChangedAfterTheCheckIsRefusedOnItsConnection (the per-connection check), and "+
				"the pool-level sentence in ADR 0021's OP-7 note. The body now:\n%s", name, body)
		}
	}
	// CONTROL: the comparison is not blind -- one changed token is a change, and a comment
	// or a re-flowed line is not.
	base := reviewedBodies["requireBoundParameters"]
	if codeTokens(t, strings.Replace(base, "return nil", "return  nil // a comment\n", 1)) != codeTokens(t, base) {
		t.Error("CONTROL FAILED: a comment or a layout change counts as a code change")
	}
	if codeTokens(t, strings.Replace(base, "return nil", "return err", 1)) == codeTokens(t, base) {
		t.Error("CONTROL FAILED: a changed token is not seen")
	}
}
