package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// TestSignupWiring_SharesTheSignInsIdentityBudget pins M14-17b's wiring (the 2026-10-27 security audit's
// ORTA finding, the user's decision "İkisini birden"): main.go's ONE handler.NewSignup call passes
// handler.SignupSharesIdentityBudget(panelAuth) -- the sign-in's own identity failure budget, so a guess on
// the registration's password branch and a guess at the sign-in count once. Without it the branch refuses
// with a 500 (never runs unbudgeted), and no handler test could see the wiring: each builds its own Signup.
//
// MUTANT (measured): the option removed from main.go -- red.
func TestSignupWiring_SharesTheSignInsIdentityBudget(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(repoRoot, "cmd", "tappa", "main.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var calls []*ast.CallExpr
	ast.Inspect(f, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if sel, ok := c.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "NewSignup" {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "handler" {
					calls = append(calls, c)
				}
			}
		}
		return true
	})
	if len(calls) != 1 {
		t.Fatalf("main.go calls handler.NewSignup %d times, want 1", len(calls))
	}
	shared := 0
	for _, a := range calls[0].Args {
		c, ok := a.(*ast.CallExpr)
		if !ok {
			continue
		}
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "SignupSharesIdentityBudget" || len(c.Args) != 1 {
			continue
		}
		if id, ok := c.Args[0].(*ast.Ident); ok && id.Name == "panelAuth" {
			shared++
		}
	}
	if shared != 1 {
		t.Fatal("handler.NewSignup is not handed handler.SignupSharesIdentityBudget(panelAuth)")
	}
}
