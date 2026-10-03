package operator_test

// typepins_test.go -- M10 OP-8, third round: the package's STRUCTURAL pins, on go/types.
//
// WHY (2nd-round audit, measured): the 2nd round's two syntax pins matched a selector
// spelled `operatorauth.…` and a composite literal spelled `operatorpages.ProblemView`;
// an import alias and a type alias each walked around one. Here a name is resolved to
// the object it denotes (types.Info.Uses/Defs) and an expression to its type and constant
// value (Info.Types), with the export data of the build being run (typedOperator), so a
// use spelled through an alias, a dot import, a type alias or a method value resolves to
// the same object (mutations X19, X19d2, X19e, X19g, X20b, X20m).
//
// EACH PIN'S HEADER IS IN THREE PARTS (4th round):
//
//	PART I   the shipped package, measured green, with the test's CONTROL showing its
//	         scan finds what it should.
//	PART II  the list the pin catches. The list is the pin's whole claim.
//	PART III one sentence: the pin catches the list in PART II only; a form not on it
//	         is code review's -- no completeness claim.
//
// THE LOAD. The file set is go list's GoFiles for the build being run. typedOperator is
// red when the package has a non-test file that build leaves out (IgnoredGoFiles,
// CgoFiles, IgnoredOtherFiles, SFiles, CFiles, SysoFiles) or imports one of the eight
// packages in refusedImports.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/atknatk/tappa/internal/operatorauth"
)

const (
	operatorPkgPath      = "github.com/atknatk/tappa/internal/handler/operator"
	operatorauthPkgPath  = "github.com/atknatk/tappa/internal/operatorauth"
	operatorpagesPkgPath = "github.com/atknatk/tappa/web/templates/operatorpages"
	httpxPkgPath         = "github.com/atknatk/tappa/internal/httpx"
)

// refusedImports are the eight imports typedOperator refuses in the package, by name:
// reflect, unsafe, plugin, text/template, html/template, encoding/json, encoding/gob,
// encoding/xml. The shipped package imports none of the eight.
var refusedImports = []string{
	"reflect", "unsafe", "plugin", "text/template", "html/template",
	"encoding/json", "encoding/gob", "encoding/xml",
}

// typedPackage is internal/handler/operator, parsed and type-checked.
type typedPackage struct {
	fset    *token.FileSet
	files   []*ast.File
	pkg     *types.Package
	info    *types.Info
	parents map[ast.Node]ast.Node
}

var (
	typedOnce sync.Once
	typedPkg  *typedPackage
	typedErr  error
)

// typedOperator loads the package once per test binary. A load failure is a red test
// (fail-closed), not an empty scan.
func typedOperator(t *testing.T) *typedPackage {
	t.Helper()
	typedOnce.Do(func() { typedPkg, typedErr = loadTypedOperator() })
	if typedErr != nil {
		t.Fatal(typedErr)
	}
	return typedPkg
}

// loadTypedOperator is internal/operatorauth's exactImports (and internal/db's
// moduleExports) applied to this package: `go list -export -deps` for the same build
// (-race when this binary is), the go command checked to be the toolchain that built
// this test, then go/importer reading that export data.
func loadTypedOperator() (*typedPackage, error) {
	if v, err := exec.Command("go", "env", "GOVERSION").Output(); err != nil {
		return nil, fmt.Errorf("go env GOVERSION: %w", err)
	} else if got := strings.TrimSpace(string(v)); got != runtime.Version() {
		return nil, fmt.Errorf("the go command is %s and this test was built by %s: its export data would not be this toolchain's", got, runtime.Version())
	}
	args := []string{"list", "-export", "-deps",
		"-json=ImportPath,Dir,Export,GoFiles,CgoFiles,IgnoredGoFiles,IgnoredOtherFiles,SFiles,CFiles,SysoFiles,Imports"}
	if raceBuild {
		args = append(args, "-race")
	}
	out, err := exec.Command("go", append(args, operatorPkgPath)...).Output()
	if err != nil {
		// go list's stderr is package, module and toolchain diagnostics; it does not echo
		// the environment (exactImports' comment in internal/operatorauth has the reading).
		var ee *exec.ExitError
		var stderr string
		if errors.As(err, &ee) {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		return nil, fmt.Errorf("go list -export: %v: %s", err, stderr)
	}
	type listed struct {
		ImportPath, Dir, Export                                                         string
		GoFiles, CgoFiles, IgnoredGoFiles, IgnoredOtherFiles, SFiles, CFiles, SysoFiles []string
		Imports                                                                         []string
	}
	exports := map[string]string{}
	var self *listed
	for dec := json.NewDecoder(bytes.NewReader(out)); ; {
		var p listed
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("go list output: %w", err)
		}
		exports[p.ImportPath] = p.Export
		if p.ImportPath == operatorPkgPath {
			self = &p
		}
	}
	if self == nil || exports[operatorauthPkgPath] == "" || exports[operatorpagesPkgPath] == "" {
		return nil, errors.New("PREMISE: go list named no export data for the package or its imports; the load has gone blind")
	}
	// A PRODUCT file the build being run leaves out is a file this scan cannot see (a
	// test file left out -- race_on_test.go in a build without -race -- is not product).
	for kind, fs := range map[string][]string{
		"CgoFiles": self.CgoFiles, "IgnoredGoFiles": self.IgnoredGoFiles, "IgnoredOtherFiles": self.IgnoredOtherFiles,
		"SFiles": self.SFiles, "CFiles": self.CFiles, "SysoFiles": self.SysoFiles,
	} {
		fs = slices.DeleteFunc(slices.Clone(fs), func(n string) bool { return strings.HasSuffix(n, "_test.go") })
		if len(fs) != 0 {
			return nil, fmt.Errorf("the package has %s %v: a file this build does not type-check is invisible to every pin here", kind, fs)
		}
	}
	for _, imp := range self.Imports {
		if slices.Contains(refusedImports, imp) {
			return nil, fmt.Errorf("the package imports %s, one of refusedImports", imp)
		}
	}
	if !slices.Contains(self.GoFiles, "routes.go") || !slices.Contains(self.GoFiles, "render.go") || len(self.GoFiles) < 7 {
		return nil, fmt.Errorf("PREMISE: the package's files are %v", self.GoFiles)
	}
	tp := &typedPackage{fset: token.NewFileSet(), parents: map[ast.Node]ast.Node{}}
	for _, name := range self.GoFiles {
		f, err := parser.ParseFile(tp.fset, filepath.Join(self.Dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		tp.files = append(tp.files, f)
	}
	imp := importer.ForCompiler(tp.fset, "gc", func(path string) (io.ReadCloser, error) {
		f, ok := exports[path]
		if !ok || f == "" {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(f)
	})
	tp.info = &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{},
		Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{},
		Instances: map[*ast.Ident]types.Instance{},
	}
	tp.pkg, err = (&types.Config{Importer: imp}).Check(operatorPkgPath, tp.fset, tp.files, tp.info)
	if err != nil {
		return nil, fmt.Errorf("type-checking %s: %w", operatorPkgPath, err)
	}
	for _, f := range tp.files {
		var stack []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			if len(stack) > 0 {
				tp.parents[n] = stack[len(stack)-1]
			}
			stack = append(stack, n)
			return true
		})
	}
	return tp, nil
}

// imported is the package this one imports at path.
func (tp *typedPackage) imported(t *testing.T, path string) *types.Package {
	t.Helper()
	for _, p := range tp.pkg.Imports() {
		if p.Path() == path {
			return p
		}
	}
	t.Fatalf("PREMISE: the package does not import %s", path)
	return nil
}

func lookupFunc(t *testing.T, p *types.Package, name string) *types.Func {
	t.Helper()
	f, ok := p.Scope().Lookup(name).(*types.Func)
	if !ok {
		t.Fatalf("PREMISE: %s declares no function %s", p.Path(), name)
	}
	return f
}

// lookupMember is the method or field name of *p.typ.
func lookupMember(t *testing.T, p *types.Package, typ, name string) types.Object {
	t.Helper()
	tn, ok := p.Scope().Lookup(typ).(*types.TypeName)
	if !ok {
		t.Fatalf("PREMISE: %s declares no type %s", p.Path(), typ)
	}
	obj, _, _ := types.LookupFieldOrMethod(types.NewPointer(tn.Type()), true, p, name)
	if obj == nil {
		t.Fatalf("PREMISE: %s.%s has no member %s", p.Path(), typ, name)
	}
	return obj
}

// method is the package's own (*typ).name.
func (tp *typedPackage) method(t *testing.T, typ, name string) *types.Func {
	t.Helper()
	m, ok := lookupMember(t, tp.pkg, typ, name).(*types.Func)
	if !ok {
		t.Fatalf("PREMISE: %s.%s is not a method", typ, name)
	}
	return m
}

// decl is the top-level declaration that contains pos. Positions are token.Pos, which a
// //line directive does not move: a use is placed in the file and declaration it really
// sits in, whatever a directive claims.
func (tp *typedPackage) decl(pos token.Pos) ast.Decl {
	for _, f := range tp.files {
		if pos < f.FileStart || pos > f.FileEnd {
			continue
		}
		for _, d := range f.Decls {
			if d.Pos() <= pos && pos < d.End() {
				return d
			}
		}
		return nil
	}
	return nil
}

// in reports whether pos sits in the declaration of fn (its body, closures included).
func (tp *typedPackage) in(fn *types.Func, pos token.Pos) bool {
	d, ok := tp.decl(pos).(*ast.FuncDecl)
	return ok && tp.info.Defs[d.Name] == fn
}

// funcName names a function or method declaration as a reader would: "(*Surface).name".
func funcName(fn *types.Func) string {
	sig, _ := fn.Type().(*types.Signature)
	if sig == nil || sig.Recv() == nil {
		return fn.Name()
	}
	recv := types.TypeString(sig.Recv().Type(), func(*types.Package) string { return "" })
	return "(" + recv + ")." + fn.Name()
}

// where is "file:line in <declaration>" for a failure message (the real file and line).
func (tp *typedPackage) where(pos token.Pos) string {
	p := tp.fset.PositionFor(pos, false)
	in := "package level"
	switch d := tp.decl(pos).(type) {
	case *ast.FuncDecl:
		if fn, ok := tp.info.Defs[d.Name].(*types.Func); ok {
			in = funcName(fn)
		}
	case *ast.GenDecl:
		in = "a package-level " + d.Tok.String()
	}
	return fmt.Sprintf("%s:%d in %s", filepath.Base(p.Filename), p.Line, in)
}

// usesOf lists the identifiers that denote one of objs, in source order.
func (tp *typedPackage) usesOf(objs ...types.Object) []*ast.Ident {
	var out []*ast.Ident
	for id, obj := range tp.info.Uses {
		if obj != nil && slices.Contains(objs, obj) {
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pos() < out[j].Pos() })
	return out
}

// constString is one constant string expression and its value.
type constString struct {
	expr ast.Expr
	val  string
}

// constStrings lists the expressions of the package with a constant STRING value -- a
// literal, a named constant (this package's or an import's), a constant expression
// built from them -- in source order.
func (tp *typedPackage) constStrings() []constString {
	var out []constString
	for e, tv := range tp.info.Types {
		if tv.Value != nil && tv.Value.Kind() == constant.String {
			out = append(out, constString{e, constant.StringVal(tv.Value)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].expr.Pos() < out[j].expr.Pos() })
	return out
}

// unparenParent is n's parent with parentheses skipped.
func (tp *typedPackage) unparenParent(n ast.Node) (ast.Node, ast.Node) {
	p := tp.parents[n]
	for {
		pe, ok := p.(*ast.ParenExpr)
		if !ok {
			return n, p
		}
		n, p = pe, tp.parents[pe]
	}
}

// callOf is the call whose callee is the selector or identifier id, or nil when id is
// not called there (a method value, a function value).
func (tp *typedPackage) callOf(id *ast.Ident) *ast.CallExpr {
	var fun ast.Node = id
	if sel, ok := tp.parents[id].(*ast.SelectorExpr); ok && sel.Sel == id {
		fun = sel
	}
	fun, p := tp.unparenParent(fun)
	if c, ok := p.(*ast.CallExpr); ok && c.Fun == fun {
		return c
	}
	return nil
}

// callee is the object a call calls, or nil.
func (tp *typedPackage) callee(c *ast.CallExpr) types.Object {
	fun := ast.Unparen(c.Fun)
	if ix, ok := fun.(*ast.IndexExpr); ok {
		fun = ix.X
	}
	switch f := fun.(type) {
	case *ast.Ident:
		return tp.info.Uses[f]
	case *ast.SelectorExpr:
		return tp.info.Uses[f.Sel]
	}
	return nil
}

// argOf reports which argument of which call e is directly (parentheses aside).
func (tp *typedPackage) argOf(e ast.Node) (*ast.CallExpr, int) {
	e, p := tp.unparenParent(e)
	c, ok := p.(*ast.CallExpr)
	if !ok {
		return nil, -1
	}
	for i, a := range c.Args {
		if a == e {
			return c, i
		}
	}
	return nil, -1
}

// throughInterface reports whether fn is a method of an interface (or of a type
// parameter's constraint) named one of names: a call through it dispatches to whatever
// method of that name the dynamic value has -- *http.Request's among them.
func throughInterface(fn *types.Func, names ...string) bool {
	sig, _ := fn.Type().(*types.Signature)
	return sig != nil && sig.Recv() != nil && types.IsInterface(sig.Recv().Type()) && slices.Contains(names, fn.Name())
}

// rawCookieMethods are net/http's readers of a request's cookies by name or all at once.
var rawCookieMethods = []string{"Cookie", "Cookies", "CookiesNamed"}

// TestSessionCookieReads_TheListedFormsOccurOnlyInRequireOperator (3rd round, F1).
//
// PART I -- the shipped package: one use of operatorauth.ReadSessionCookie, in
// (*Surface).requireOperator, and none of SC2-SC6. CONTROL: the same object resolution
// finds operatorauth.ReadChallengeCookie in (*Surface).codePage and (*Surface).code; the
// method resolution finds (*http.Request).Context; the interface rule finds templ.Component's
// Render in render; the constant scan finds "Sec-Fetch-Site" twice.
//
// PART II -- red on:
//
//	SC1 a use of the function object operatorauth.ReadSessionCookie outside
//	    (*Surface).requireOperator, or a use count there other than 1;
//	SC2 a use of the constant object operatorauth.SessionCookieName;
//	SC3 a constant string expression whose value contains the session cookie's name,
//	    case aside;
//	SC4 a use of (*http.Request).Cookie, .Cookies, .CookiesNamed or http.ParseCookie;
//	SC5 a call through an interface or type-parameter method named Cookie, Cookies or
//	    CookiesNamed;
//	SC6 a constant string expression equal to "Cookie", case and spaces aside.
//
// PART III -- This pin catches the list in PART II only; a form not on it (examples: a header or cookie name built at run time, a reader in another package) is code review's -- no completeness claim.
func TestSessionCookieReads_TheListedFormsOccurOnlyInRequireOperator(t *testing.T) {
	tp := typedOperator(t)
	oa := tp.imported(t, operatorauthPkgPath)
	nh := tp.imported(t, "net/http")
	read := lookupFunc(t, oa, "ReadSessionCookie")
	readChallenge := lookupFunc(t, oa, "ReadChallengeCookie")
	name, ok := oa.Scope().Lookup("SessionCookieName").(*types.Const)
	if !ok || constant.StringVal(name.Val()) != operatorauth.SessionCookieName {
		t.Fatal("PREMISE: operatorauth.SessionCookieName is not the constant this test imports")
	}
	raw := map[types.Object]string{}
	for _, m := range rawCookieMethods {
		raw[lookupMember(t, nh, "Request", m)] = "(*http.Request)." + m
	}
	raw[lookupFunc(t, nh, "ParseCookie")] = "http.ParseCookie"
	ctxMethod := lookupMember(t, nh, "Request", "Context")
	allowed := tp.method(t, "Surface", "requireOperator")

	var bad []string
	var reads, challengeReads []string
	contexts, renders := 0, 0
	for _, id := range tp.usesOf(read, readChallenge, name, ctxMethod) {
		switch tp.info.Uses[id] {
		case read:
			if tp.in(allowed, id.Pos()) {
				reads = append(reads, tp.where(id.Pos()))
			} else {
				bad = append(bad, "SC1 operatorauth.ReadSessionCookie at "+tp.where(id.Pos()))
			}
		case name:
			bad = append(bad, "SC2 operatorauth.SessionCookieName at "+tp.where(id.Pos()))
		case readChallenge:
			challengeReads = append(challengeReads, tp.where(id.Pos()))
		case ctxMethod:
			contexts++
		}
	}
	for id, obj := range tp.info.Uses {
		if what, ok := raw[obj]; ok {
			bad = append(bad, "SC4 "+what+" at "+tp.where(id.Pos()))
		}
		if fn, ok := obj.(*types.Func); ok {
			if throughInterface(fn, rawCookieMethods...) {
				bad = append(bad, "SC5 a call through "+fn.FullName()+" at "+tp.where(id.Pos()))
			}
			if throughInterface(fn, "Render") {
				renders++
			}
		}
	}
	cookieName := strings.ToLower(operatorauth.SessionCookieName)
	fetchSite := 0
	for _, cs := range tp.constStrings() {
		v := strings.ToLower(cs.val)
		if strings.Contains(v, cookieName) {
			bad = append(bad, fmt.Sprintf("SC3 a constant %q at %s", cs.val, tp.where(cs.expr.Pos())))
		}
		if strings.TrimSpace(v) == "cookie" {
			bad = append(bad, fmt.Sprintf("SC6 the header name %q at %s", cs.val, tp.where(cs.expr.Pos())))
		}
		if v == "sec-fetch-site" {
			fetchSite++
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
	if len(reads) != 1 {
		t.Errorf("SC1 operatorauth.ReadSessionCookie is used %d time(s) in (*Surface).requireOperator, want 1: %v", len(reads), reads)
	}
	if len(challengeReads) != 2 || !strings.Contains(challengeReads[0], "(*Surface).codePage") || !strings.Contains(challengeReads[1], "(*Surface).code") {
		t.Fatalf("CONTROL: the challenge cookie's readers resolved to %v, want (*Surface).codePage and (*Surface).code", challengeReads)
	}
	if contexts == 0 || renders == 0 || fetchSite != 2 {
		t.Fatalf("CONTROL: (*http.Request).Context %d use(s), Render through templ.Component %d, \"Sec-Fetch-Site\" %d (want >0, >0, 2): a scan is blind", contexts, renders, fetchSite)
	}
}

// mentions reports whether ty is target or is built from it (pointer, slice, array, map,
// channel, signature, tuple, an unnamed struct's fields, a named type's type arguments).
func mentions(ty types.Type, target *types.TypeName) bool {
	var walk func(types.Type, int) bool
	walk = func(ty types.Type, depth int) bool {
		if ty == nil || depth > 32 {
			return false
		}
		switch x := types.Unalias(ty).(type) {
		case *types.Named:
			if x.Obj() == target {
				return true
			}
			for i := 0; i < x.TypeArgs().Len(); i++ {
				if walk(x.TypeArgs().At(i), depth+1) {
					return true
				}
			}
		case *types.Pointer:
			return walk(x.Elem(), depth+1)
		case *types.Slice:
			return walk(x.Elem(), depth+1)
		case *types.Array:
			return walk(x.Elem(), depth+1)
		case *types.Chan:
			return walk(x.Elem(), depth+1)
		case *types.Map:
			return walk(x.Key(), depth+1) || walk(x.Elem(), depth+1)
		case *types.Signature:
			return walk(x.Params(), depth+1) || walk(x.Results(), depth+1)
		case *types.Tuple:
			for i := 0; i < x.Len(); i++ {
				if walk(x.At(i).Type(), depth+1) {
					return true
				}
			}
		case *types.Struct:
			for i := 0; i < x.NumFields(); i++ {
				if walk(x.Field(i).Type(), depth+1) {
					return true
				}
			}
		}
		return false
	}
	return walk(ty, 0)
}

// isNamed reports whether ty, through any alias, is the named type target.
func isNamed(ty types.Type, target *types.TypeName) bool {
	n, ok := types.Unalias(ty).(*types.Named)
	return ok && n.Obj() == target
}

// TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo (3rd round, F2).
//
// PART I -- the shipped package: thirteen keyed ProblemView literals (OP-8's eight and the
// legal screen's five, OP-10), each the value of a package-level variable of render.go,
// one keyed literal in problemTooMany; problemPages uses the thirteen variables and calls
// problemTooMany with false and with true; each Back in those literals is a constant. (The
// premise below asks for at least eight -- OP-8's floor; PV2 holds every variable to
// problemPages, whatever their number.) CONTROL: the same type resolution finds the three
// operatorpages.SignInView literals of signin.go and their Failed/Expired keys.
//
// PART II -- red on:
//
//	PV1 a composite literal of type operatorpages.ProblemView (resolved through aliases)
//	    that is neither directly the value of a package-level variable of render.go nor
//	    inside problemTooMany; more than one in problemTooMany; an unkeyed one;
//	PV2 a render.go variable of PV1 that problemPages does not use (the blank identifier
//	    included);
//	PV3 problemPages calling problemTooMany other than twice, with false and with true;
//	PV4 a Back in a PV1 literal whose value is not a constant expression;
//	PV5 a use of a ProblemView field other than as a key of a PV1 literal;
//	PV6 a conversion to a type built from ProblemView;
//	PV7 an instantiation with a type argument built from ProblemView.
//
// PART III -- This pin catches the list in PART II only; a form not on it (examples: a zero ProblemView, one returned by another package, one decoded by encoding/asn1) is code review's -- no completeness claim.
func TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo(t *testing.T) {
	tp := typedOperator(t)
	op := tp.imported(t, operatorpagesPkgPath)
	pv, ok := op.Scope().Lookup("ProblemView").(*types.TypeName)
	if !ok {
		t.Fatal("PREMISE: operatorpages declares no ProblemView")
	}
	signInView, ok := op.Scope().Lookup("SignInView").(*types.TypeName)
	if !ok {
		t.Fatal("PREMISE: operatorpages declares no SignInView")
	}
	fields := map[types.Object]string{}
	st := pv.Type().Underlying().(*types.Struct)
	for i := 0; i < st.NumFields(); i++ {
		fields[st.Field(i)] = st.Field(i).Name()
	}
	if len(fields) != 5 {
		t.Fatalf("PREMISE: ProblemView has %d fields", len(fields))
	}
	tooMany := lookupFunc(t, tp.pkg, "problemTooMany")
	pages := lookupFunc(t, tp.pkg, "problemPages")

	var bad []string
	vars := map[types.Object]string{}
	keys := map[*ast.Ident]bool{}
	inTooMany, signInLits := 0, 0
	for _, f := range tp.files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CompositeLit:
				ty := tp.info.Types[x].Type
				if isNamed(ty, signInView) {
					signInLits++
				}
				if !isNamed(ty, pv) {
					return true
				}
				allowed := false
				switch {
				case tp.in(tooMany, x.Pos()):
					inTooMany++
					allowed = true
				default:
					vs, ok := tp.parents[x].(*ast.ValueSpec)
					gd, _ := tp.parents[vs].(*ast.GenDecl)
					_, top := tp.parents[gd].(*ast.File)
					if !ok || gd == nil || gd.Tok != token.VAR || !top ||
						filepath.Base(tp.fset.File(x.Pos()).Name()) != "render.go" {
						break
					}
					for i, v := range vs.Values {
						if v == x && i < len(vs.Names) {
							vars[tp.info.Defs[vs.Names[i]]] = vs.Names[i].Name
							allowed = true
						}
					}
				}
				if !allowed {
					bad = append(bad, "PV1 a ProblemView literal at "+tp.where(x.Pos()))
					return true
				}
				for _, e := range x.Elts {
					kv, ok := e.(*ast.KeyValueExpr)
					k, _ := kv.Key.(*ast.Ident)
					if !ok || k == nil {
						bad = append(bad, "PV1 an unkeyed ProblemView literal at "+tp.where(x.Pos()))
						continue
					}
					keys[k] = true
					if k.Name == "Back" && tp.info.Types[kv.Value].Value == nil {
						bad = append(bad, "PV4 a Back that is not a constant at "+tp.where(kv.Pos()))
					}
				}
			case *ast.CallExpr:
				if tv, ok := tp.info.Types[x.Fun]; ok && tv.IsType() && mentions(tv.Type, pv) {
					bad = append(bad, "PV6 a conversion to "+tv.Type.String()+" at "+tp.where(x.Pos()))
				}
			}
			return true
		})
	}
	signInKeys := 0
	for id, obj := range tp.info.Uses {
		if name, ok := fields[obj]; ok && !keys[id] {
			bad = append(bad, "PV5 the field "+name+" at "+tp.where(id.Pos()))
		}
		if v, ok := obj.(*types.Var); ok && v.IsField() && (v.Name() == "Failed" || v.Name() == "Expired") && v.Pkg() == op {
			signInKeys++
		}
	}
	for id, inst := range tp.info.Instances {
		for i := 0; i < inst.TypeArgs.Len(); i++ {
			if mentions(inst.TypeArgs.At(i), pv) {
				bad = append(bad, "PV7 "+id.Name+" instantiated with "+inst.TypeArgs.At(i).String()+" at "+tp.where(id.Pos()))
			}
		}
	}
	for obj, name := range vars {
		used := false
		for _, id := range tp.usesOf(obj) {
			used = used || tp.in(pages, id.Pos())
		}
		if !used {
			bad = append(bad, "PV2 render.go's "+name+" is not listed by problemPages")
		}
	}
	var args []string
	for _, id := range tp.usesOf(tooMany) {
		if !tp.in(pages, id.Pos()) {
			continue
		}
		c := tp.callOf(id)
		if c == nil || len(c.Args) != 1 || tp.info.Types[c.Args[0]].Value == nil {
			bad = append(bad, "PV3 problemPages names problemTooMany other than as a call with a constant at "+tp.where(id.Pos()))
			continue
		}
		args = append(args, tp.info.Types[c.Args[0]].Value.String())
	}
	sort.Strings(args)
	if !slices.Equal(args, []string{"false", "true"}) {
		bad = append(bad, fmt.Sprintf("PV3 problemPages calls problemTooMany with %v, want [false true]", args))
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
	if len(vars) < 8 || inTooMany != 1 {
		t.Errorf("PREMISE/PV1: %d render.go variable literal(s) (want >= 8), %d literal(s) in problemTooMany (want 1)", len(vars), inTooMany)
	}
	if signInLits != 3 || signInKeys != 2 {
		t.Fatalf("CONTROL: %d SignInView literal(s) and %d Failed/Expired key(s) resolved, want 3 and 2: the scan is blind", signInLits, signInKeys)
	}
}

// TestEnrollScreen_TheListedFormsRenderItOnlyInRenderEnroll (3rd round).
//
// PART I -- the shipped package: operatorpages.Enroll is used once, in
// (*Surface).renderEnroll; operatorpages.EnrollScript once, in (*Surface).enrollCSP;
// (*Surface).enrollCSP once, in renderEnroll. CONTROL: the same resolution finds
// operatorpages.SignIn at its three sites.
//
// PART II -- red on: EN1 a use of operatorpages.Enroll outside renderEnroll, or a count
// there other than 1; EN2 a use of operatorpages.EnrollScript outside enrollCSP, or a count
// there other than 1; EN3 a use of the method enrollCSP outside renderEnroll, or a count
// there other than 1.
//
// PART III -- This pin catches the list in PART II only; a form not on it (examples: another templ component that includes the enrollment screen or its script tag) is code review's -- no completeness claim.
func TestEnrollScreen_TheListedFormsRenderItOnlyInRenderEnroll(t *testing.T) {
	tp := typedOperator(t)
	op := tp.imported(t, operatorpagesPkgPath)
	renderEnroll := tp.method(t, "Surface", "renderEnroll")
	enrollCSP := tp.method(t, "Surface", "enrollCSP")
	for _, c := range []struct {
		code string
		obj  types.Object
		in   *types.Func
	}{
		{"EN1", lookupFunc(t, op, "Enroll"), renderEnroll},
		{"EN2", lookupFunc(t, op, "EnrollScript"), enrollCSP},
		{"EN3", enrollCSP, renderEnroll},
	} {
		uses := tp.usesOf(c.obj)
		inside := 0
		for _, id := range uses {
			if tp.in(c.in, id.Pos()) {
				inside++
			} else {
				t.Errorf("%s %s used at %s", c.code, c.obj.Name(), tp.where(id.Pos()))
			}
		}
		if inside != 1 {
			t.Errorf("%s %s is used %d time(s) in %s, want 1", c.code, c.obj.Name(), inside, funcName(c.in))
		}
	}
	var at []string
	for _, id := range tp.usesOf(lookupFunc(t, op, "SignIn")) {
		at = append(at, tp.where(id.Pos()))
	}
	if len(at) != 3 {
		t.Fatalf("CONTROL: operatorpages.SignIn resolved at %v, want 3 sites", at)
	}
}

// TestFormValues_TheListedSitesAloneRevealOrReadTheForm (3rd round).
//
// PART I -- the shipped package: reveal() is called at twelve sites -- V1 six direct
// arguments of (*operatorauth.Authenticator).Password (email, password), TOTP (code) and
// CompleteEnrollment (token, password, code); V2 the Token key of the EnrollView literal in
// (*Surface).enroll; V3 the two operands of the != in (*Surface).enroll; and (OP-11, the
// tenant search term) V4 one in tenantSearchTerm, V5 the Search key of the
// db.TenantListQuery literal in (*Surface).listTenants, V6 the Search key of the
// operatorpages.TenantsView literal in tenantsView. r.PostForm is read in postValue, in
// (*Surface).enroll as .Get("id") and .Get("blob"), (OP-10) in (*Surface).publishLegal as
// .Get("slug") and .Get("body"), and (OP-11) in (*Surface).searchTenants as .Get("page"),
// once each. CONTROL: postValue's eight calls resolve, each with a constant credential
// name.
//
// PART II -- red on:
//
//	FV1 a reveal() call at another site, or site counts other than 6 + 1 + 2 + 1 + 1 + 1;
//	FV2 reveal taken as a method value or expression;
//	FV3 a constant string equal to "email", "password", "password_again", "code",
//	    "token" or (OP-11) "q" -- the search term, personal data when it is an address --
//	    other than as postValue's name argument;
//	FV4 a use of the request's Form, MultipartForm or RequestURI field, of its FormValue,
//	    PostFormValue, FormFile, MultipartReader or ParseMultipartForm method, or of
//	    url.URL's RawQuery field or String method;
//	FV5 r.PostForm read other than in postValue, in (*Surface).enroll as .Get("id") or
//	    .Get("blob"), in (*Surface).publishLegal as .Get("slug") or .Get("body") (OP-10)
//	    and in (*Surface).searchTenants as .Get("page") (OP-11) -- a count there other than
//	    one of each;
//	FV6 url.URL.Query other than once in (*Surface).enrollPage, as .Get("id");
//	FV7 ParseForm other than once in (*Surface).readForm.
//
// PART III -- This pin catches the list in PART II only; a form not on it (examples: url.URL's RequestURI or Redacted method, a name built at run time, r.Body read directly) is code review's -- no completeness claim.
func TestFormValues_TheListedSitesAloneRevealOrReadTheForm(t *testing.T) {
	tp := typedOperator(t)
	oa := tp.imported(t, operatorauthPkgPath)
	op := tp.imported(t, operatorpagesPkgPath)
	nh := tp.imported(t, "net/http")
	nu := tp.imported(t, "net/url")
	reveal := tp.method(t, "formValue", "reveal")
	postValue := lookupFunc(t, tp.pkg, "postValue")
	enroll := tp.method(t, "Surface", "enroll")
	enrollPage := tp.method(t, "Surface", "enrollPage")
	readForm := tp.method(t, "Surface", "readForm")
	publishLegal := tp.method(t, "Surface", "publishLegal")
	searchTenants := tp.method(t, "Surface", "searchTenants")
	listTenants := tp.method(t, "Surface", "listTenants")
	searchTerm := lookupFunc(t, tp.pkg, "tenantSearchTerm")
	tenantsView := lookupFunc(t, tp.pkg, "tenantsView")
	legalReads, tenantReads := map[string]int{}, map[string]int{}
	consumers := map[types.Object]string{}
	for _, m := range []string{"Password", "TOTP", "CompleteEnrollment"} {
		consumers[lookupMember(t, oa, "Authenticator", m)] = m
	}
	enrollView, _ := op.Scope().Lookup("EnrollView").(*types.TypeName)
	if enrollView == nil {
		t.Fatal("PREMISE: operatorpages declares no EnrollView")
	}
	tenantsViewType, _ := op.Scope().Lookup("TenantsView").(*types.TypeName)
	listQuery, _ := tp.imported(t, "github.com/atknatk/tappa/internal/db").Scope().Lookup("TenantListQuery").(*types.TypeName)
	if tenantsViewType == nil || listQuery == nil {
		t.Fatal("PREMISE: operatorpages declares no TenantsView, or internal/db no TenantListQuery")
	}
	// keyOfLiteral reports whether p is the key-value element `key: …` of a composite
	// literal of the named type typ.
	keyOfLiteral := func(p ast.Node, key string, typ *types.TypeName) bool {
		if !isKeyOf(p, key) {
			return false
		}
		lit, ok := tp.parents[p].(*ast.CompositeLit)
		return ok && isNamed(tp.info.Types[lit].Type, typ)
	}

	var bad []string
	sites := map[string]int{}
	for _, id := range tp.usesOf(reveal) {
		c := tp.callOf(id)
		if c == nil {
			bad = append(bad, "FV2 reveal not called at "+tp.where(id.Pos()))
			continue
		}
		outer, i := tp.argOf(c)
		_, p := tp.unparenParent(c)
		switch {
		case outer != nil && consumers[tp.callee(outer)] != "":
			sites["V1 "+consumers[tp.callee(outer)]+fmt.Sprintf(" arg %d", i)]++
		case tp.in(enroll, c.Pos()) && isKeyOf(p, "Token") && isNamed(tp.info.Types[tp.parents[p].(ast.Expr)].Type, enrollView):
			sites["V2 EnrollView.Token"]++
		case tp.in(enroll, c.Pos()) && isNEQ(p):
			sites["V3 !="]++
		case tp.in(searchTerm, c.Pos()):
			sites["V4 tenantSearchTerm"]++
		case tp.in(listTenants, c.Pos()) && keyOfLiteral(p, "Search", listQuery):
			sites["V5 TenantListQuery.Search"]++
		case tp.in(tenantsView, c.Pos()) && keyOfLiteral(p, "Search", tenantsViewType):
			sites["V6 TenantsView.Search"]++
		default:
			bad = append(bad, "FV1 reveal() at "+tp.where(id.Pos()))
		}
	}
	want := map[string]int{
		"V1 Password arg 2": 1, "V1 Password arg 3": 1, "V1 TOTP arg 2": 1,
		"V1 CompleteEnrollment arg 3": 1, "V1 CompleteEnrollment arg 5": 1, "V1 CompleteEnrollment arg 6": 1,
		"V2 EnrollView.Token": 1, "V3 !=": 2,
		"V4 tenantSearchTerm": 1, "V5 TenantListQuery.Search": 1, "V6 TenantsView.Search": 1,
	}
	if fmt.Sprint(sites) != fmt.Sprint(want) {
		bad = append(bad, fmt.Sprintf("FV1 reveal() sites %v, want %v", sites, want))
	}
	credentialNames := []string{"email", "password", "password_again", "code", "token", "q"}
	names := 0
	for _, cs := range tp.constStrings() {
		if !slices.Contains(credentialNames, cs.val) {
			continue
		}
		if c, i := tp.argOf(cs.expr); c != nil && i == 1 && tp.callee(c) == postValue {
			names++
			continue
		}
		bad = append(bad, fmt.Sprintf("FV3 the credential field name %q at %s", cs.val, tp.where(cs.expr.Pos())))
	}
	forbidden := map[types.Object]string{}
	for _, m := range []string{"Form", "MultipartForm", "FormValue", "PostFormValue", "FormFile", "MultipartReader", "ParseMultipartForm", "RequestURI"} {
		forbidden[lookupMember(t, nh, "Request", m)] = "(*http.Request)." + m
	}
	for _, m := range []string{"RawQuery", "String"} {
		forbidden[lookupMember(t, nu, "URL", m)] = "(*url.URL)." + m
	}
	postForm := lookupMember(t, nh, "Request", "PostForm")
	query := lookupMember(t, nu, "URL", "Query")
	parseForm := lookupMember(t, nh, "Request", "ParseForm")
	parses, queries := 0, 0
	for id, obj := range tp.info.Uses {
		if what, ok := forbidden[obj]; ok {
			bad = append(bad, "FV4 "+what+" at "+tp.where(id.Pos()))
		}
		switch obj {
		case postForm:
			if tp.in(postValue, id.Pos()) {
				continue
			}
			if tp.in(enroll, id.Pos()) {
				if k := tp.getKey(id); k == "id" || k == "blob" {
					continue
				}
			}
			if tp.in(publishLegal, id.Pos()) {
				if k := tp.getKey(id); k == "slug" || k == "body" {
					legalReads[k]++
					continue
				}
			}
			if tp.in(searchTenants, id.Pos()) {
				if k := tp.getKey(id); k == "page" {
					tenantReads[k]++
					continue
				}
			}
			bad = append(bad, "FV5 r.PostForm at "+tp.where(id.Pos()))
		case query:
			if tp.in(enrollPage, id.Pos()) && tp.getKey(id) == "id" {
				queries++
				continue
			}
			bad = append(bad, "FV6 URL.Query at "+tp.where(id.Pos()))
		case parseForm:
			if tp.in(readForm, id.Pos()) {
				parses++
				continue
			}
			bad = append(bad, "FV7 ParseForm at "+tp.where(id.Pos()))
		}
	}
	if queries != 1 || parses != 1 {
		bad = append(bad, fmt.Sprintf("FV6/FV7 URL.Query in enrollPage %d time(s), ParseForm in readForm %d (want 1, 1)", queries, parses))
	}
	if legalReads["slug"] != 1 || legalReads["body"] != 1 {
		bad = append(bad, fmt.Sprintf("FV5 publishLegal reads r.PostForm %v, want slug and body once each", legalReads))
	}
	if len(tenantReads) != 1 || tenantReads["page"] != 1 {
		bad = append(bad, fmt.Sprintf("FV5 searchTenants reads r.PostForm %v, want page once", tenantReads))
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
	if names != 8 {
		t.Fatalf("CONTROL: %d postValue call(s) with a constant credential name resolved, want 8", names)
	}
}

// isKeyOf reports whether p is a key: value element whose key is the identifier key.
func isKeyOf(p ast.Node, key string) bool {
	kv, ok := p.(*ast.KeyValueExpr)
	if !ok {
		return false
	}
	k, ok := kv.Key.(*ast.Ident)
	return ok && k.Name == key
}

func isNEQ(p ast.Node) bool {
	b, ok := p.(*ast.BinaryExpr)
	return ok && b.Op == token.NEQ
}

// getKey is the constant argument of the .Get call made on what id selects --
// r.PostForm.Get("id"), r.URL.Query().Get("id") -- or "" when id's value is used
// any other way.
func (tp *typedPackage) getKey(id *ast.Ident) string {
	sel, ok := tp.parents[id].(*ast.SelectorExpr)
	if !ok || sel.Sel != id {
		return ""
	}
	var x ast.Node = sel
	if c := tp.callOf(id); c != nil { // a method called (URL.Query()): its result
		x = c
	}
	x, p := tp.unparenParent(x)
	get, ok := p.(*ast.SelectorExpr)
	if !ok || get.X != x || get.Sel.Name != "Get" {
		return ""
	}
	c := tp.callOf(get.Sel)
	if c == nil || len(c.Args) != 1 {
		return ""
	}
	if v := tp.info.Types[c.Args[0]].Value; v != nil && v.Kind() == constant.String {
		return constant.StringVal(v)
	}
	return ""
}

// forwardedHeaders are six request header names a proxy uses to name a client, the list
// AD3 catches.
var forwardedHeaders = []string{"x-forwarded-for", "x-real-ip", "forwarded", "true-client-ip", "cf-connecting-ip", "x-client-ip"}

// TestClientAddress_TheListedReadsFeedOnlyTheBudgets (3rd round).
//
// PART I -- the shipped package: httpx.ClientIP and httpx.RateKey are used once each, in
// rateKey; rateKey is called four times, and each result is a direct argument of
// (*operatorauth.Authenticator).AllowRequest, Password or CompleteEnrollment, or the value
// of logoutGate's local key, whose two uses are arguments of AllowRequest and
// (*httpx.Limiter).Charge. CONTROL: the four calls resolve and reach the four sinks.
//
// PART II -- red on: AD1 httpx.ClientIP or httpx.RateKey used outside rateKey, or a count
// there other than 1; AD2 a use of the request's RemoteAddr field; AD3 a constant string
// equal (case aside) to one of the six names in forwardedHeaders; AD4 a rateKey result used
// other than as a direct argument of AllowRequest, Password, CompleteEnrollment or
// Limiter.Charge, or as the value of a one-variable := whose uses are such arguments;
// AD5 rateKey taken as a function value.
//
// PART III -- This pin catches the list in PART II only; a form not on it (examples: a header name outside the six (X-Cluster-Client-IP), the address read through another package) is code review's -- no completeness claim.
func TestClientAddress_TheListedReadsFeedOnlyTheBudgets(t *testing.T) {
	tp := typedOperator(t)
	hx := tp.imported(t, httpxPkgPath)
	oa := tp.imported(t, operatorauthPkgPath)
	nh := tp.imported(t, "net/http")
	rateKey := lookupFunc(t, tp.pkg, "rateKey")
	sinks := map[types.Object]string{lookupMember(t, hx, "Limiter", "Charge"): "Charge"}
	for _, m := range []string{"AllowRequest", "Password", "CompleteEnrollment"} {
		sinks[lookupMember(t, oa, "Authenticator", m)] = m
	}
	var bad []string
	for _, name := range []string{"ClientIP", "RateKey"} {
		inside := 0
		for _, id := range tp.usesOf(lookupFunc(t, hx, name)) {
			if tp.in(rateKey, id.Pos()) {
				inside++
			} else {
				bad = append(bad, "AD1 httpx."+name+" at "+tp.where(id.Pos()))
			}
		}
		if inside != 1 {
			bad = append(bad, fmt.Sprintf("AD1 httpx.%s used %d time(s) in rateKey, want 1", name, inside))
		}
	}
	for _, id := range tp.usesOf(lookupMember(t, nh, "Request", "RemoteAddr")) {
		bad = append(bad, "AD2 RemoteAddr at "+tp.where(id.Pos()))
	}
	for _, cs := range tp.constStrings() {
		if slices.Contains(forwardedHeaders, strings.ToLower(strings.TrimSpace(cs.val))) {
			bad = append(bad, fmt.Sprintf("AD3 the header name %q at %s", cs.val, tp.where(cs.expr.Pos())))
		}
	}
	sinkOf := func(e ast.Node) string {
		if c, _ := tp.argOf(e); c != nil {
			return sinks[tp.callee(c)]
		}
		return ""
	}
	reached := map[string]int{}
	calls := 0
	for _, id := range tp.usesOf(rateKey) {
		c := tp.callOf(id)
		if c == nil {
			bad = append(bad, "AD5 rateKey not called at "+tp.where(id.Pos()))
			continue
		}
		calls++
		if s := sinkOf(c); s != "" {
			reached[s]++
			continue
		}
		_, p := tp.unparenParent(c)
		as, ok := p.(*ast.AssignStmt)
		if !ok || as.Tok != token.DEFINE || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			bad = append(bad, "AD4 rateKey's result used at "+tp.where(c.Pos()))
			continue
		}
		v := tp.info.Defs[as.Lhs[0].(*ast.Ident)]
		uses := tp.usesOf(v)
		if len(uses) == 0 {
			bad = append(bad, "AD4 rateKey's result unused at "+tp.where(c.Pos()))
		}
		for _, u := range uses {
			if s := sinkOf(u); s != "" {
				reached[s]++
			} else {
				bad = append(bad, "AD4 rateKey's result, through "+v.Name()+", at "+tp.where(u.Pos()))
			}
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
	if calls != 4 || len(reached) != 4 {
		t.Fatalf("CONTROL: %d rateKey call(s) reaching %v, want 4 calls reaching all four sinks", calls, reached)
	}
}

// guardedHeaders are the response header names RH1 catches (case aside), with the
// functions allowed to write each and how often.
var guardedHeaders = map[string]map[string]int{
	"content-security-policy": {"(*Surface).securityHeaders": 1, "(*Surface).renderEnroll": 1},
	"cache-control":           {"(*Surface).securityHeaders": 1, "serviceUnavailable": 1},
	"x-content-type-options":  {"(*Surface).securityHeaders": 1, "serviceUnavailable": 1},
	"referrer-policy":         {"(*Surface).securityHeaders": 1},
	"location":                {"(*Surface).redirect": 1},
}

// TestResponseHeaders_TheListedNamesAreWrittenOnlyInTheirFunctions (3rd round). The
// response headers' behaviour is measured on the headers at WriteHeader by the three
// header tables (TestOperatorHeaders_FortyResponseClassesCarryThePolicy,
// TestOperatorHeaders_TheWrongMethodAndOversizedClassesCarryThePolicy and, OP-10,
// TestOperatorHeaders_TheLegalClassesCarryThePolicy: 66 classes, hostile request
// headers); this pin is the source-side list below.
//
// PART I -- the shipped package: the constant occurrences of each guardedHeaders name are
// its allowed functions with its allowed counts; each http.Header Set/Add takes a
// constant name; the scan finds no Del, no http.Redirect and no index expression on an
// http.Header and no builtin delete or clear on one; each (*Surface).redirect call passes
// a constant that is a path of operatorRoutes. CONTROL: the five names are found and at
// least ten redirect calls resolve.
//
// PART II -- red on: RH1 a guardedHeaders name (constant, case aside) outside its functions,
// or a count other than allowed; RH2 a Header Set or Add whose name is not a constant;
// RH3 a use of http.Header's Del, of http.Redirect, an index expression on an http.Header,
// or the builtin delete or clear with an http.Header argument (5th round); RH4 a redirect
// call whose target is not a constant path of operatorRoutes; RH5 redirect taken as a
// method value.
//
// PART III -- This pin catches the list in PART II only; a form not on it (examples: maps.Copy into w.Header(), textproto.MIMEHeader(w.Header()).Set, a ResponseWriter wrapped by another package) is code review's -- no completeness claim.
func TestResponseHeaders_TheListedNamesAreWrittenOnlyInTheirFunctions(t *testing.T) {
	tp := typedOperator(t)
	nh := tp.imported(t, "net/http")
	redirect := tp.method(t, "Surface", "redirect")
	set, add, del := lookupMember(t, nh, "Header", "Set"), lookupMember(t, nh, "Header", "Add"), lookupMember(t, nh, "Header", "Del")
	header, _ := nh.Scope().Lookup("Header").(*types.TypeName)
	var bad []string
	found := map[string]map[string]int{}
	for _, cs := range tp.constStrings() {
		name := strings.ToLower(strings.TrimSpace(cs.val))
		if _, ok := guardedHeaders[name]; !ok {
			continue
		}
		in := "package level"
		if d, ok := tp.decl(cs.expr.Pos()).(*ast.FuncDecl); ok {
			in = funcName(tp.info.Defs[d.Name].(*types.Func))
		}
		if found[name] == nil {
			found[name] = map[string]int{}
		}
		found[name][in]++
	}
	for name, allowed := range guardedHeaders {
		if fmt.Sprint(found[name]) != fmt.Sprint(allowed) {
			bad = append(bad, fmt.Sprintf("RH1 %q is written in %v, want %v", name, found[name], allowed))
		}
	}
	for id, obj := range tp.info.Uses {
		switch obj {
		case set, add:
			if c := tp.callOf(id); c == nil || len(c.Args) != 2 || tp.info.Types[c.Args[0]].Value == nil {
				bad = append(bad, "RH2 a Header."+obj.Name()+" without a constant name at "+tp.where(id.Pos()))
			}
		case del:
			bad = append(bad, "RH3 Header.Del at "+tp.where(id.Pos()))
		}
		if fn, ok := obj.(*types.Func); ok && fn.Pkg() == nh && fn.Name() == "Redirect" {
			bad = append(bad, "RH3 http.Redirect at "+tp.where(id.Pos()))
		}
		// The 4th audit's H01/L01 deleted a header after WriteHeader with the builtin.
		if b, ok := obj.(*types.Builtin); ok && (b.Name() == "delete" || b.Name() == "clear") {
			if c := tp.callOf(id); c != nil && len(c.Args) > 0 && isNamed(tp.info.Types[c.Args[0]].Type, header) {
				bad = append(bad, "RH3 the builtin "+b.Name()+" on an http.Header at "+tp.where(id.Pos()))
			}
		}
	}
	for e := range tp.info.Types {
		if ix, ok := e.(*ast.IndexExpr); ok && isNamed(tp.info.Types[ix.X].Type, header) {
			bad = append(bad, "RH3 an index on an http.Header at "+tp.where(ix.Pos()))
		}
	}
	mounted := map[string]bool{}
	for _, r := range operatorRoutes {
		mounted[r.path] = true
	}
	redirects := 0
	for _, id := range tp.usesOf(redirect) {
		c := tp.callOf(id)
		if c == nil {
			bad = append(bad, "RH5 redirect not called at "+tp.where(id.Pos()))
			continue
		}
		redirects++
		v := tp.info.Types[c.Args[len(c.Args)-1]].Value
		if v == nil || v.Kind() != constant.String || !mounted[constant.StringVal(v)] {
			bad = append(bad, "RH4 a redirect target that is not a mounted route's constant at "+tp.where(c.Pos()))
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
	if len(found) != len(guardedHeaders) || redirects < 10 {
		t.Fatalf("CONTROL: %d of %d guarded header names found, %d redirect call(s) (want all, >= 10)", len(found), len(guardedHeaders), redirects)
	}
}

// operatorScreens are the nine screen constructors screens() (op8_test.go) renders, by
// name -- the list SN1/SN2 compare with operatorpages' exported API (OP-10 added Legal,
// OP-11 Tenants and TenantOverview).
var operatorScreens = []string{"Code", "Enroll", "Home", "Legal", "Problem", "SignIn", "TenantOverview", "TenantScreen", "Tenants"}

// TestOperatorPages_TheExportedScreensAreTheOnesScreensRenders (3rd round).
//
// PART I -- operatorpages' exported functions that return a templ.Component are the nine
// of operatorScreens (read from the export data of the build being run).
//
// PART II -- red on: SN1 an exported constructor not in operatorScreens; SN2 a name in
// operatorScreens that is not one.
//
// PART III -- This pin catches the list in PART II only; a form not on it (examples: screens() not calling a listed constructor, a view field combination screens() does not render) is code review's -- no completeness claim.
func TestOperatorPages_TheExportedScreensAreTheOnesScreensRenders(t *testing.T) {
	tp := typedOperator(t)
	op := tp.imported(t, operatorpagesPkgPath)
	tm := tp.imported(t, "github.com/a-h/templ")
	component, _ := tm.Scope().Lookup("Component").(*types.TypeName)
	if component == nil {
		t.Fatal("PREMISE: templ declares no Component")
	}
	var got []string
	for _, n := range op.Scope().Names() {
		fn, ok := op.Scope().Lookup(n).(*types.Func)
		if !ok || !fn.Exported() {
			continue
		}
		res := fn.Type().(*types.Signature).Results()
		for i := 0; i < res.Len(); i++ {
			if isNamed(res.At(i).Type(), component) {
				got = append(got, n)
				break
			}
		}
	}
	sort.Strings(got)
	if !slices.Equal(got, operatorScreens) {
		t.Errorf("operatorpages' exported screen constructors are %v; screens() renders %v", got, operatorScreens)
	}
}

// TestHostGate_TheListedHostReadsOccurOnlyThroughOnHost (3rd round).
//
// PART I -- the shipped package: httpx.OnHost is used once, in (*Surface).hostGate; the
// request's Host field is not used; url.URL's Host, Hostname and Port are used in
// operatorOrigin (the base URL, parsed at start-up). CONTROL: operatorOrigin's reads
// resolve (at least two).
//
// PART II -- red on: HG1 httpx.OnHost used outside hostGate, or a count there other than
// 1; HG2 a use of (*http.Request).Host, or of url.URL's Host, Hostname or Port outside
// operatorOrigin; HG3 a constant equal (case aside) to "host" or "x-forwarded-host".
//
// PART III -- This pin catches the list in PART II only; a form not on it (examples: r.TLS.ServerName, the customer half's call in internal/httpx, a host read through another package) is code review's -- no completeness claim.
func TestHostGate_TheListedHostReadsOccurOnlyThroughOnHost(t *testing.T) {
	tp := typedOperator(t)
	hx := tp.imported(t, httpxPkgPath)
	nh := tp.imported(t, "net/http")
	nu := tp.imported(t, "net/url")
	hostGate := tp.method(t, "Surface", "hostGate")
	origin := lookupFunc(t, tp.pkg, "operatorOrigin")
	var bad []string
	inside := 0
	for _, id := range tp.usesOf(lookupFunc(t, hx, "OnHost")) {
		if tp.in(hostGate, id.Pos()) {
			inside++
		} else {
			bad = append(bad, "HG1 httpx.OnHost at "+tp.where(id.Pos()))
		}
	}
	if inside != 1 {
		bad = append(bad, fmt.Sprintf("HG1 httpx.OnHost used %d time(s) in hostGate, want 1", inside))
	}
	for _, id := range tp.usesOf(lookupMember(t, nh, "Request", "Host")) {
		bad = append(bad, "HG2 (*http.Request).Host at "+tp.where(id.Pos()))
	}
	baseReads := 0
	for _, id := range tp.usesOf(lookupMember(t, nu, "URL", "Host"), lookupMember(t, nu, "URL", "Hostname"), lookupMember(t, nu, "URL", "Port")) {
		if tp.in(origin, id.Pos()) {
			baseReads++
			continue
		}
		bad = append(bad, "HG2 url.URL."+id.Name+" at "+tp.where(id.Pos()))
	}
	for _, cs := range tp.constStrings() {
		if v := strings.ToLower(strings.TrimSpace(cs.val)); v == "host" || v == "x-forwarded-host" {
			bad = append(bad, fmt.Sprintf("HG3 the header name %q at %s", cs.val, tp.where(cs.expr.Pos())))
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
	if baseReads < 2 {
		t.Fatalf("CONTROL: %d url.URL host/port read(s) resolved in operatorOrigin, want >= 2", baseReads)
	}
}

// TestOperatorPages_ImportedOnlyByTheSurfaceAndSharingOnlyTheShell (3rd round).
//
// PART I -- `go list` of the module (non-test imports, the build being run):
// internal/handler/operator is the module package that imports operatorpages; of the
// module, operatorpages imports web/templates/layout and web/templates/components;
// internal/httpx's dependency closure contains none of operatorauth,
// internal/handler/operator, operatorpages.
//
// PART II -- red on: IM1 another package of the module importing operatorpages; IM2
// operatorpages importing another package of the module; IM3 one of the three in
// internal/httpx's dependency closure.
//
// PART III -- This pin catches the list in PART II only; a form not on it (examples: a test file's import, a copy of the templates in another package) is code review's -- no completeness claim.
func TestOperatorPages_ImportedOnlyByTheSurfaceAndSharingOnlyTheShell(t *testing.T) {
	args := []string{"list", "-json=ImportPath,Imports,Deps"}
	if raceBuild {
		args = append(args, "-race")
	}
	out, err := exec.Command("go", append(args, "github.com/atknatk/tappa/...")...).Output()
	if err != nil {
		var ee *exec.ExitError
		var stderr string
		if errors.As(err, &ee) {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		t.Fatalf("go list: %v: %s", err, stderr)
	}
	var importers, own []string
	seen, httpxDeps := 0, 0
	for dec := json.NewDecoder(bytes.NewReader(out)); ; {
		var p struct {
			ImportPath    string
			Imports, Deps []string
		}
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		seen++
		if slices.Contains(p.Imports, operatorpagesPkgPath) {
			importers = append(importers, p.ImportPath)
		}
		if p.ImportPath == httpxPkgPath {
			for _, forbidden := range []string{operatorauthPkgPath, operatorPkgPath, operatorpagesPkgPath} {
				if slices.Contains(p.Deps, forbidden) {
					t.Errorf("IM3 internal/httpx depends on %s", forbidden)
				}
			}
			httpxDeps = len(p.Deps)
		}
		if p.ImportPath == operatorpagesPkgPath {
			for _, imp := range p.Imports {
				if strings.HasPrefix(imp, "github.com/atknatk/tappa/") {
					own = append(own, imp)
				}
			}
		}
	}
	if seen < 20 || httpxDeps < 20 {
		t.Fatalf("PREMISE: go list named %d package(s) of the module, %d dependencies of httpx", seen, httpxDeps)
	}
	sort.Strings(importers)
	sort.Strings(own)
	if !slices.Equal(importers, []string{operatorPkgPath}) {
		t.Errorf("IM1 operatorpages is imported by %v, want only %s", importers, operatorPkgPath)
	}
	if want := []string{"github.com/atknatk/tappa/web/templates/components", "github.com/atknatk/tappa/web/templates/layout"}; !slices.Equal(own, want) {
		t.Errorf("IM2 operatorpages imports %v of the module, want %v", own, want)
	}
}
