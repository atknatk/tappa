package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// TestBreakerWiring_TheTransportReachesTheChannelsOnlyThroughOneBreaker pins M10 EM-7A's
// wiring as main.go spells it (ADR 0022 §9: the process-wide breaker counts EVERY send),
// because no handler or mail test can see it: each builds its own breaker. If run()
// handed an e-mail channel the bare transport, or built a second breaker for one flow,
// that flow's sends would escape the one count and every other test would stay green.
//
// It asserts, over run() alone: run() calls mail.New at least once and mail.NewBreaker
// exactly once; every identifier run() assigns from mail.New appears nowhere in run()
// except on the left of that assignment and as mail.NewBreaker's FIRST argument (so the
// transport reaches nothing — no channel, no second wrapper — except the one breaker);
// and the first argument of every handler.NewEmail…Channel call is an identifier run()
// assigns from that one mail.NewBreaker call.
//
// PART II — what it catches: the transport passed to any call but the breaker; a second
// breaker; a channel given another breaker-shaped value or nil (NewEmailResetChannel's
// *mail.Breaker parameter already refuses the transport itself at compile time). WHAT IT
// DOES NOT CATCH: a breaker built outside run(); an identifier reassigned through a
// pointer or a closure; a channel constructor whose name does not start with NewEmail
// (an e-mail channel of another package is checked only through rule one — the
// transport cannot reach it). PART III: a form not on that list is code review's — no
// completeness claim.
func TestBreakerWiring_TheTransportReachesTheChannelsOnlyThroughOneBreaker(t *testing.T) {
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

	call := func(n ast.Node, pkg, name string) (*ast.CallExpr, bool) {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return nil, false
		}
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if !ok {
			return nil, false
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || id.Name != pkg {
			return nil, false
		}
		if name[len(name)-1] == '*' {
			p := name[:len(name)-1]
			return c, len(sel.Sel.Name) >= len(p) && sel.Sel.Name[:len(p)] == p
		}
		return c, sel.Sel.Name == name
	}

	transports := map[string]bool{} // identifiers assigned from mail.New
	breakerIDs := map[string]bool{} // identifiers assigned from mail.NewBreaker
	allowed := map[*ast.Ident]bool{}
	var news, breakers, channels []*ast.CallExpr
	ast.Inspect(run.Body, func(n ast.Node) bool {
		if c, ok := call(n, "mail", "New"); ok {
			news = append(news, c)
		}
		if c, ok := call(n, "mail", "NewBreaker"); ok {
			breakers = append(breakers, c)
		}
		if c, ok := call(n, "handler", "NewEmail*"); ok {
			channels = append(channels, c)
		}
		if as, ok := n.(*ast.AssignStmt); ok && len(as.Rhs) == 1 {
			for _, lhs := range as.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok {
					continue
				}
				if _, ok := call(as.Rhs[0], "mail", "New"); ok && id.Name != "err" && id.Name != "_" {
					transports[id.Name] = true
					allowed[id] = true
				}
				if _, ok := call(as.Rhs[0], "mail", "NewBreaker"); ok && id.Name != "err" && id.Name != "_" {
					breakerIDs[id.Name] = true
				}
			}
		}
		return true
	})
	if len(news) == 0 || len(transports) == 0 {
		t.Fatal("run() builds no mail transport it names; this pin reads mail.New's assignment")
	}
	if len(breakers) != 1 {
		t.Fatalf("run() calls mail.NewBreaker %d time(s), want exactly one: a second breaker is a second count, "+
			"and the flows behind it escape the first", len(breakers))
	}
	if len(breakers[0].Args) == 0 {
		t.Fatal("mail.NewBreaker is called without arguments; the file does not compile as read")
	}
	if id, ok := breakers[0].Args[0].(*ast.Ident); ok && transports[id.Name] {
		allowed[id] = true
	} else {
		t.Error("mail.NewBreaker's first argument is not the transport run() built with mail.New")
	}

	ast.Inspect(run.Body, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && transports[id.Name] && !allowed[id] {
			t.Errorf("the transport %q is used in run() other than as the breaker's argument (at offset %d): "+
				"whatever it reaches there sends around the process-wide count", id.Name, id.Pos())
		}
		return true
	})

	if len(channels) == 0 {
		t.Fatal("run() builds no handler.NewEmail… channel; this pin expects the reset channel at least")
	}
	for _, c := range channels {
		name := c.Fun.(*ast.SelectorExpr).Sel.Name
		if len(c.Args) == 0 {
			t.Fatalf("handler.%s is called without arguments", name)
		}
		if id, ok := c.Args[0].(*ast.Ident); !ok || !breakerIDs[id.Name] {
			t.Errorf("handler.%s is not given the one breaker run() builds as its sender", name)
		}
	}
}
