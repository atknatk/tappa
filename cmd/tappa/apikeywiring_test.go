package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// TestAPIKeyWiring_TheKeysServiceReachesThePanel pins M11 API-3's wiring as main.go spells it
// (the second round's D3; TestInvitationWiring_OneTransportServesBothFlows' AST shape). No
// handler test can see it -- each builds its own AdminAuth -- and losing it would take the
// owner's revocation (K-26) away with no compile error.
//
// It asserts, over run(): exactly ONE apikey.NewKeys call, as a statement of run()'s own body
// (not inside a branch), with apikey.LivePrefix and cfg.APIKeyHMACKey as its first two
// arguments and its result assigned to one identifier K; exactly one handler.IntegrationKeys
// call, whose one argument is K, appended -- again in run()'s own body -- to the identifier O
// that the one handler.NewAdminAuth call spreads as its variadic options.
//
// The runtime half is NewAdminAuth's refusal of a key-holding configuration with no keys
// service (internal/handler TestNewAdminAuth_TheKeysServiceMatchesTheConfiguration).
//
// MUTANTS (measured): the append removed; IntegrationKeys moved into an `if`; the prefix
// changed to apikey.SandboxPrefix -- each red.
func TestAPIKeyWiring_TheKeysServiceReachesThePanel(t *testing.T) {
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
	isSel := func(e ast.Expr, pkg, name string) bool {
		sel, ok := e.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != name {
			return false
		}
		id, ok := sel.X.(*ast.Ident)
		return ok && id.Name == pkg
	}
	isCall := func(e ast.Expr, pkg, name string) (*ast.CallExpr, bool) {
		c, ok := e.(*ast.CallExpr)
		return c, ok && isSel(c.Fun, pkg, name)
	}

	// Every call of the three, anywhere in run().
	counts := map[string]int{}
	var auth *ast.CallExpr
	ast.Inspect(run, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			for _, k := range [][2]string{{"apikey", "NewKeys"}, {"handler", "IntegrationKeys"}, {"handler", "NewAdminAuth"}} {
				if isSel(c.Fun, k[0], k[1]) {
					counts[k[1]]++
					if k[1] == "NewAdminAuth" {
						auth = c
					}
				}
			}
		}
		return true
	})
	if counts["NewKeys"] != 1 || counts["IntegrationKeys"] != 1 || counts["NewAdminAuth"] != 1 {
		t.Fatalf("run() calls apikey.NewKeys %d, handler.IntegrationKeys %d and handler.NewAdminAuth %d times, want 1, 1, 1",
			counts["NewKeys"], counts["IntegrationKeys"], counts["NewAdminAuth"])
	}
	if !auth.Ellipsis.IsValid() || len(auth.Args) == 0 {
		t.Fatal("handler.NewAdminAuth does not spread its options")
	}
	optionsID, ok := auth.Args[len(auth.Args)-1].(*ast.Ident)
	if !ok {
		t.Fatal("handler.NewAdminAuth's spread argument is not an identifier")
	}

	// The two statements, in run()'s OWN body.
	keysID, appended := "", false
	for _, st := range run.Body.List {
		as, ok := st.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 || len(as.Lhs) == 0 {
			continue
		}
		if c, ok := isCall(as.Rhs[0], "apikey", "NewKeys"); ok {
			if len(c.Args) < 2 || !isSel(c.Args[0], "apikey", "LivePrefix") || !isSel(c.Args[1], "cfg", "APIKeyHMACKey") {
				t.Fatal("apikey.NewKeys is not called with apikey.LivePrefix and cfg.APIKeyHMACKey")
			}
			if id, ok := as.Lhs[0].(*ast.Ident); ok {
				keysID = id.Name
			}
		}
		if c, ok := as.Rhs[0].(*ast.CallExpr); ok {
			if fn, ok := c.Fun.(*ast.Ident); ok && fn.Name == "append" && len(c.Args) == 2 {
				lhs, ok1 := as.Lhs[0].(*ast.Ident)
				base, ok2 := c.Args[0].(*ast.Ident)
				ik, ok3 := isCall(c.Args[1], "handler", "IntegrationKeys")
				if ok1 && ok2 && ok3 && lhs.Name == optionsID.Name && base.Name == optionsID.Name && len(ik.Args) == 1 {
					if arg, ok := ik.Args[0].(*ast.Ident); ok && arg.Name == keysID && keysID != "" {
						appended = true
					}
				}
			}
		}
	}
	if keysID == "" {
		t.Fatal("apikey.NewKeys is not a statement of run()'s own body, assigned to an identifier")
	}
	if !appended {
		t.Fatalf("run()'s own body does not append handler.IntegrationKeys(%s) to %s, the options NewAdminAuth spreads",
			keysID, optionsID.Name)
	}
}
