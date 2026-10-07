package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// paramIndex returns the position of the parameter called param in the declaration of
// the function fn in the Go file at path — read from the source, so this pin follows a
// signature that grows instead of carrying a second copy of it.
func paramIndex(t *testing.T, path, fn, param string) int {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv != nil || fd.Name.Name != fn {
			continue
		}
		i := 0
		for _, field := range fd.Type.Params.List {
			for _, name := range field.Names {
				if name.Name == param {
					return i
				}
				i++
			}
		}
		t.Fatalf("%s's %s has no parameter called %q; this pin reads it by name", path, fn, param)
	}
	t.Fatalf("%s declares no func %s", path, fn)
	return -1
}

// TestPasswordNoticeWiring_ThePanelIsGivenTheMountedRecoveryFlow pins M10 EM-9's wiring
// as main.go spells it, because no handler test can see it: every one of them builds
// its own AdminAuth with its own notifier. If run() handed the panel a recovery flow
// built WITHOUT the configured channel, an "email" deployment would write, on every
// change from the Account section, a notice row saying "this deployment sends no
// e-mail" — the wrong reason, and no e-mail — and every other test would stay green
// (the "delivered but not assembled" class, docs/plan/agent-brief.md).
//
// It asserts, over run() alone: exactly one handler.NewAdminAuth call and exactly one
// handler.NewAdminReset call; the argument in NewAdminAuth's `notices` position is a
// plain identifier, assigned in run() exactly once, from that NewAdminReset call; that
// call's argument in NewAdminReset's `mail` position is a plain identifier that run()
// assigns from handler.NewEmailResetChannel(...); and the same flow identifier is
// passed to the one NewRouter call (it is mounted) and to the shutdown call (it is
// drained). The two parameter positions are read from internal/handler's own
// declarations.
//
// PART II — what it catches: a second recovery flow (with or without a channel) given
// to the panel; nil or another value as the panel's notifier; a flow built with nil or
// with a channel that NewEmailResetChannel never produced; the notified flow not the
// mounted or the drained one. WHAT IT DOES NOT CATCH: what NewAdminReset and
// NewAdminAuth do with their arguments (internal/handler's own tests); a run() that
// reassigns the channel identifier to something else as well (the pin asks only that
// NewEmailResetChannel is one of its sources). PART III: a form not on that list is
// code review's — no completeness claim.
func TestPasswordNoticeWiring_ThePanelIsGivenTheMountedRecoveryFlow(t *testing.T) {
	noticesAt := paramIndex(t, filepath.Join(repoRoot, "internal", "handler", "adminlogin.go"), "NewAdminAuth", "notices")
	mailAt := paramIndex(t, filepath.Join(repoRoot, "internal", "handler", "adminreset.go"), "NewAdminReset", "mail")

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

	isHandlerCall := func(n ast.Node, name string) (*ast.CallExpr, bool) {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return nil, false
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != name {
			return nil, false
		}
		pkg, ok := sel.X.(*ast.Ident)
		return call, ok && pkg.Name == "handler"
	}

	var auths, resets []*ast.CallExpr
	var routers, shutdowns []*ast.CallExpr
	// assigned maps an identifier to the right-hand sides run() assigns to it.
	assigned := map[string][]ast.Expr{}
	ast.Inspect(run.Body, func(n ast.Node) bool {
		if c, ok := isHandlerCall(n, "NewAdminAuth"); ok {
			auths = append(auths, c)
		}
		if c, ok := isHandlerCall(n, "NewAdminReset"); ok {
			resets = append(resets, c)
		}
		if c, ok := n.(*ast.CallExpr); ok {
			switch exprName(c.Fun) {
			case "NewRouter":
				routers = append(routers, c)
			case "shutdown":
				shutdowns = append(shutdowns, c)
			}
		}
		if as, ok := n.(*ast.AssignStmt); ok {
			for i, lhs := range as.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok {
					continue
				}
				switch {
				case len(as.Rhs) == len(as.Lhs):
					assigned[id.Name] = append(assigned[id.Name], as.Rhs[i])
				case len(as.Rhs) == 1:
					// a, err := f(...) — every name on the left takes the one call.
					assigned[id.Name] = append(assigned[id.Name], as.Rhs[0])
				}
			}
		}
		return true
	})
	if len(auths) != 1 || len(resets) != 1 {
		t.Fatalf("run() calls handler.NewAdminAuth %d time(s) and handler.NewAdminReset %d time(s); "+
			"this pin expects the one panel and the one recovery flow the binary serves", len(auths), len(resets))
	}
	if len(auths[0].Args) <= noticesAt || len(resets[0].Args) <= mailAt {
		t.Fatal("a call has fewer arguments than its declaration's parameter position; the file does not compile as read")
	}

	flow, ok := auths[0].Args[noticesAt].(*ast.Ident)
	if !ok {
		t.Fatalf("the panel's notifier (NewAdminAuth argument %d) is not a plain identifier; "+
			"it must be the recovery flow run() built", noticesAt)
	}
	srcs := assigned[flow.Name]
	if len(srcs) != 1 || srcs[0] != ast.Expr(resets[0]) {
		t.Fatalf("the panel's notifier %q is assigned %d time(s) in run(), and not once from the one "+
			"handler.NewAdminReset call: an \"email\" deployment would announce Account-section changes "+
			"through a flow that is not the configured one", flow.Name, len(srcs))
	}

	channel, ok := resets[0].Args[mailAt].(*ast.Ident)
	if !ok || channel.Name == "nil" {
		t.Fatalf("NewAdminReset's channel (argument %d) is not the identifier run() fills from "+
			"TAPPA_RESET_DELIVERY; neither the reset link nor the change notice could be sent", mailAt)
	}
	fromEmail := false
	for _, rhs := range assigned[channel.Name] {
		if _, ok := isHandlerCall(rhs, "NewEmailResetChannel"); ok {
			fromEmail = true
		}
	}
	if !fromEmail {
		t.Errorf("run() never assigns %q from handler.NewEmailResetChannel(...): the recovery flow "+
			"the panel is given could not send in an \"email\" deployment", channel.Name)
	}

	passes := func(calls []*ast.CallExpr, what string) {
		t.Helper()
		if len(calls) != 1 {
			t.Fatalf("run() makes %d %s call(s); this pin expects one", len(calls), what)
		}
		for _, a := range calls[0].Args {
			if id, ok := a.(*ast.Ident); ok && id.Name == flow.Name {
				return
			}
		}
		t.Errorf("the panel's notifier %q is not passed to %s: the flow that announces changes is not "+
			"the one the binary %s", flow.Name, what, map[string]string{"NewRouter": "mounts", "shutdown": "drains"}[what])
	}
	passes(routers, "NewRouter")
	passes(shutdowns, "shutdown")
}
