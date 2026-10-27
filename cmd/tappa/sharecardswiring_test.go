package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// TestShareCardWiring_TheMarketingSurfaceNamesTheConfiguredOrigin pins LP-13's wiring as
// main.go spells it: exactly one handler.NewMarketing call, whose fourth argument is
// cfg.BaseURL (TAPPA_BASE_URL). No handler test can see it -- each builds its own
// Marketing with testBaseURL -- and a "" there would ship every public page without its
// share image, with nothing red (shareImagesFor answers "" for no origin, by design).
// The sign-up wizard reads the same field from the *config.Config NewSignup is handed.
//
// MUTANT (measured): the argument changed to "" -- red.
func TestShareCardWiring_TheMarketingSurfaceNamesTheConfiguredOrigin(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(repoRoot, "cmd", "tappa", "main.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var calls []*ast.CallExpr
	ast.Inspect(f, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := c.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "NewMarketing" {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "handler" {
				calls = append(calls, c)
			}
		}
		return true
	})
	if len(calls) != 1 {
		t.Fatalf("main.go calls handler.NewMarketing %d times, want 1", len(calls))
	}
	if len(calls[0].Args) != 4 {
		t.Fatalf("handler.NewMarketing takes %d arguments in main.go, want 4", len(calls[0].Args))
	}
	sel, ok := calls[0].Args[3].(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "BaseURL" {
		t.Fatal("handler.NewMarketing's fourth argument is not cfg.BaseURL")
	}
	if id, ok := sel.X.(*ast.Ident); !ok || id.Name != "cfg" {
		t.Fatal("handler.NewMarketing's fourth argument is not cfg.BaseURL")
	}
}
