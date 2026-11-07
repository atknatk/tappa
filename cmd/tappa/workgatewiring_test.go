package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// TestWorkGateWiring_OneGateServesEveryPasswordService pins backlog T86's wiring as main.go spells
// it, because no package test can see it: each builds its own services, and a constructor given no
// WithWorkGate installs a gate of its own (adminauth.gateOf) -- the ceiling would then be one per
// service instead of one per process.
//
// It asserts, over run() alone: exactly ONE adminauth.NewWorkGate call, assigned to one identifier;
// and each of adminauth.New, NewResets, NewSignupClaims and NewAdminInvites called exactly once, with
// adminauth.WithWorkGate(<that identifier>) among its arguments. WHAT IT DOES NOT CATCH: what the
// services do with the gate (internal/adminauth's tests); an identifier reassigned between the calls;
// a fifth service that runs bcrypt without the gate (adminauth's TestWorkGate_EveryPasswordMethodEntersTheGate).
func TestWorkGateWiring_OneGateServesEveryPasswordService(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(repoRoot, "cmd", "tappa", "main.go"), nil, 0)
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
	isCall := func(n ast.Node, name string) (*ast.CallExpr, bool) {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return nil, false
		}
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != name {
			return nil, false
		}
		pkg, ok := sel.X.(*ast.Ident)
		return c, ok && pkg.Name == "adminauth"
	}
	var gateNames []string
	ast.Inspect(run.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 {
			return true
		}
		if _, ok := isCall(as.Rhs[0], "NewWorkGate"); ok {
			if id, ok := as.Lhs[0].(*ast.Ident); ok {
				gateNames = append(gateNames, id.Name)
			}
		}
		return true
	})
	if len(gateNames) != 1 {
		t.Fatalf("run() builds %d work gate(s) (%v), want exactly one", len(gateNames), gateNames)
	}
	gate := gateNames[0]
	for _, ctor := range []string{"New", "NewResets", "NewSignupClaims", "NewAdminInvites"} {
		calls, shared := 0, 0
		ast.Inspect(run.Body, func(n ast.Node) bool {
			c, ok := isCall(n, ctor)
			if !ok {
				return true
			}
			calls++
			for _, arg := range c.Args {
				opt, ok := isCall(arg, "WithWorkGate")
				if !ok || len(opt.Args) != 1 {
					continue
				}
				if id, ok := opt.Args[0].(*ast.Ident); ok && id.Name == gate {
					shared++
				}
			}
			return true
		})
		if calls != 1 || shared != 1 {
			t.Errorf("adminauth.%s: %d call(s), %d given WithWorkGate(%s); want exactly one of each", ctor, calls, shared, gate)
		}
	}
}
