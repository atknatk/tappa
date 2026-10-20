package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// TestBrandThemeWiring_RunMountsTheThemeRoute pins the wiring of M10 WL-5's route as
// main.go spells it. It asserts: run() makes exactly one call whose function is named
// appRouter (the one composition of the surface, router.go -- OP-15 phase B moved the
// router's assembly there from an inline httpx.NewRouter call), and that call's
// arguments hold handler.NewBrandTheme() -- the call itself, with no arguments --
// exactly once. That appRouter hands every argument to httpx.NewRouter is pinned by
// TestAppRouter_IsTheOnlyComposition, and that NewRouter mounts each is NewRouter's own
// code. The route's own tests drive a router built with it; without this pin they
// would stay green with the route passed to no router in the product (the "delivered
// but not mounted" class, docs/plan/agent-brief.md).
//
// PART II -- what it catches: the call removed from appRouter's arguments; the
// route built elsewhere in run() and not passed; a second appRouter call in run().
// PART III: a form not on that list is code review's -- no completeness claim.
func TestBrandThemeWiring_RunMountsTheThemeRoute(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(repoRoot, "cmd", "tappa", "main.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var run *ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == "run" {
			run = fd
		}
	}
	if run == nil {
		t.Fatal("main.go declares no run(); this test reads it by name")
	}
	routers, mounted := 0, 0
	ast.Inspect(run.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || exprName(call.Fun) != "appRouter" {
			return true
		}
		routers++
		for _, a := range call.Args {
			arg, ok := a.(*ast.CallExpr)
			if !ok || len(arg.Args) != 0 {
				continue
			}
			if sel, ok := arg.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "NewBrandTheme" {
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "handler" {
					mounted++
				}
			}
		}
		return true
	})
	if routers != 1 {
		t.Fatalf("run() calls appRouter %d times; this test expects the one router the binary serves", routers)
	}
	if mounted != 1 {
		t.Errorf("run()'s appRouter call is passed handler.NewBrandTheme() %d times, want once: "+
			"GET /brand/theme/{HEX}.css would answer 404 in the product", mounted)
	}
}
