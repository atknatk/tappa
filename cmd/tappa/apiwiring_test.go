package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// TestAPIWiring_TheChainTakesThePanelsPrefixAndTheOneWorker pins M11 API-4's wiring as the command
// spells it (no handler test can see it -- each builds its own surface):
//
//	the prefix   the command calls apikey.NewAuthenticator exactly ONCE, in openAPISurface, with
//	             apikey.LivePrefix and cfg.APIKeyHMACKey -- the two arguments
//	             TestAPIKeyWiring_TheKeysServiceReachesThePanel pins on the panel's apikey.NewKeys, so a
//	             key the panel issues is a key the chain accepts (plan §5, API-3's hand-over; API-11
//	             changes both together)
//	the surface  api.New once, with api.NoSuspensionColumn{} (until OP-15) and the worker parameter as
//	             its Work
//	K-19         run() calls webhook.NewWorker once, assigning one identifier W, and in its OWN body
//	             starts W, hands W to checkins.NoticeWorkTo and to openAPISurface, and passes W inside
//	             the drainers list shutdown drains
//
// MUTANTS (measured, API-4 report): the chain built with apikey.SandboxPrefix; NoticeWorkTo removed;
// W left out of the drainers -- each red.
func TestAPIWiring_TheChainTakesThePanelsPrefixAndTheOneWorker(t *testing.T) {
	fset := token.NewFileSet()
	parse := func(name string) *ast.File {
		f, err := parser.ParseFile(fset, filepath.Join(repoRoot, "cmd", "tappa", name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	isSel := func(e ast.Expr, pkg, name string) bool {
		sel, ok := e.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != name {
			return false
		}
		id, ok := sel.X.(*ast.Ident)
		return ok && id.Name == pkg
	}
	fn := func(f *ast.File, name string) *ast.FuncDecl {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == name {
				return fd
			}
		}
		t.Fatalf("no func %s", name)
		return nil
	}
	calls := func(n ast.Node, pkg, name string) []*ast.CallExpr {
		var out []*ast.CallExpr
		ast.Inspect(n, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok && isSel(c.Fun, pkg, name) {
				out = append(out, c)
			}
			return true
		})
		return out
	}

	mainFile, apiFile := parse("main.go"), parse("api.go")
	if n := len(calls(mainFile, "apikey", "NewAuthenticator")); n != 0 {
		t.Fatalf("main.go calls apikey.NewAuthenticator %d times; the chain is built in openAPISurface alone", n)
	}
	open := fn(apiFile, "openAPISurface")
	auths := calls(apiFile, "apikey", "NewAuthenticator")
	if len(auths) != 1 || len(calls(open, "apikey", "NewAuthenticator")) != 1 {
		t.Fatalf("apikey.NewAuthenticator is called %d times in api.go, want once, in openAPISurface", len(auths))
	}
	if a := auths[0].Args; len(a) < 2 || !isSel(a[0], "apikey", "LivePrefix") || !isSel(a[1], "cfg", "APIKeyHMACKey") {
		t.Fatal("the chain is not built with apikey.LivePrefix and cfg.APIKeyHMACKey -- the panel's two arguments")
	}
	news := calls(apiFile, "api", "New")
	if len(news) != 1 {
		t.Fatalf("api.New is called %d times", len(news))
	}
	workParam := open.Type.Params.List[2].Names[0].Name
	var suspension, work bool
	if lit, ok := news[0].Args[0].(*ast.CompositeLit); ok {
		for _, e := range lit.Elts {
			kv, ok := e.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			switch kv.Key.(*ast.Ident).Name {
			case "Suspension":
				if c, ok := kv.Value.(*ast.CompositeLit); ok && isSel(c.Type, "api", "NoSuspensionColumn") {
					suspension = true
				}
			case "Work":
				if id, ok := kv.Value.(*ast.Ident); ok && id.Name == workParam {
					work = true
				}
			}
		}
	}
	if !suspension || !work {
		t.Fatalf("api.New: Suspension api.NoSuspensionColumn{} %v, Work the %q parameter %v", suspension, workParam, work)
	}

	run := fn(mainFile, "run")
	if n := len(calls(mainFile, "webhook", "NewWorker")); n != 1 {
		t.Fatalf("main.go calls webhook.NewWorker %d times, want 1", n)
	}
	worker := ""
	for _, st := range run.Body.List {
		if as, ok := st.(*ast.AssignStmt); ok && len(as.Rhs) == 1 {
			if c, ok := as.Rhs[0].(*ast.CallExpr); ok && isSel(c.Fun, "webhook", "NewWorker") {
				worker = as.Lhs[0].(*ast.Ident).Name
			}
		}
	}
	if worker == "" {
		t.Fatal("webhook.NewWorker is not assigned in run()'s own body")
	}
	started, noticed, surfaced, drained := false, false, false, false
	ast.Inspect(run.Body, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		argIs := func(i int) bool {
			if i >= len(c.Args) {
				return false
			}
			id, ok := c.Args[i].(*ast.Ident)
			return ok && id.Name == worker
		}
		switch {
		case isSel(c.Fun, worker, "Start"):
			started = true
		case isSel(c.Fun, "checkins", "NoticeWorkTo") && argIs(0):
			noticed = true
		case exprName(c.Fun) == "openAPISurface" && argIs(2):
			surfaced = true
		case exprName(c.Fun) == "shutdown":
			for _, a := range c.Args {
				if lit, ok := a.(*ast.CompositeLit); ok {
					for _, e := range lit.Elts {
						if id, ok := e.(*ast.Ident); ok && id.Name == worker {
							drained = true
						}
					}
				}
			}
		}
		return true
	})
	if !started || !noticed || !surfaced || !drained {
		t.Fatalf("the worker %q: started %v, given to the tap path %v, to the API %v, drained %v -- want all four",
			worker, started, noticed, surfaced, drained)
	}
}
