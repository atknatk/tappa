package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/invite"
)

// TestInvitationWiring_OneTransportServesBothFlows pins M10 EM-7B's wiring as main.go
// spells it, because no handler test can see it: each builds its own AdminAuth with
// its own transport.
//
// It asserts, over run() alone: exactly ONE mail.New call, its result assigned to one
// identifier; the one handler.NewEmailResetChannel call and the one
// handler.NewEmailInvitations call take the SAME first argument, and run() assigns it
// from mail.NewBreaker(<that identifier>, …) (one relay, one SMTP identity, one
// process-wide count — EM-7A, whose own pin holds that the transport reaches nothing
// else and that there is one breaker);
// handler.InvitationsByEmail is called exactly once, inside the
// `case config.InviteDeliveryEmail:` arm of a switch on cfg.InviteDelivery, appended to
// one identifier; and the one handler.NewAdminAuth call spreads that identifier as its
// variadic options.
//
// PART II — what it catches: a second transport for either flow; the invitation route
// built from something other than the shared sender; the option given outside the
// e-mail arm (the panel mode mailing links — the mode check in NewAdminAuth catches
// the same at run time, TestNewAdminAuth_TheInvitationModeMatchesTheConfiguration);
// the options built and never passed. WHAT IT DOES NOT CATCH: what NewEmailInvitations
// and NewAdminAuth do with their arguments (internal/handler's tests); an identifier
// reassigned between the calls. PART III: a form not on that list is code review's —
// no completeness claim.
func TestInvitationWiring_OneTransportServesBothFlows(t *testing.T) {
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
		if !ok || sel.Sel.Name != name {
			return nil, false
		}
		id, ok := sel.X.(*ast.Ident)
		return c, ok && id.Name == pkg
	}
	firstArg := func(c *ast.CallExpr) string {
		if len(c.Args) == 0 {
			return ""
		}
		if id, ok := c.Args[0].(*ast.Ident); ok {
			return id.Name
		}
		return ""
	}

	var transports, resets, invitations, options, auths []*ast.CallExpr
	transportVar := ""
	optionsVar := ""
	optionInEmailArm := false
	var walk func(n ast.Node, inEmailArm bool)
	walk = func(n ast.Node, inEmailArm bool) {
		ast.Inspect(n, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SwitchStmt:
				if sel, ok := x.Tag.(*ast.SelectorExpr); ok && sel.Sel.Name == "InviteDelivery" {
					for _, s := range x.Body.List {
						cc := s.(*ast.CaseClause)
						arm := len(cc.List) == 1
						if arm {
							sel, ok := cc.List[0].(*ast.SelectorExpr)
							arm = ok && sel.Sel.Name == "InviteDeliveryEmail"
						}
						for _, st := range cc.Body {
							walk(st, arm)
						}
					}
					return false
				}
			case *ast.AssignStmt:
				if len(x.Rhs) == 1 {
					if c, ok := call(x.Rhs[0], "mail", "New"); ok && len(x.Lhs) > 0 {
						transports = append(transports, c)
						if id, ok := x.Lhs[0].(*ast.Ident); ok {
							transportVar = id.Name
						}
						return false
					}
					// options = append(options, handler.InvitationsByEmail(...))
					if app, ok := x.Rhs[0].(*ast.CallExpr); ok && len(app.Args) == 2 {
						if fn, ok := app.Fun.(*ast.Ident); ok && fn.Name == "append" {
							if c, ok := call(app.Args[1], "handler", "InvitationsByEmail"); ok {
								options = append(options, c)
								optionInEmailArm = inEmailArm
								if id, ok := x.Lhs[0].(*ast.Ident); ok {
									optionsVar = id.Name
								}
							}
						}
					}
				}
			case *ast.CallExpr:
				if c, ok := call(x, "mail", "New"); ok {
					transports = append(transports, c)
				}
				if c, ok := call(x, "handler", "NewEmailResetChannel"); ok {
					resets = append(resets, c)
				}
				if c, ok := call(x, "handler", "NewEmailInvitations"); ok {
					invitations = append(invitations, c)
				}
				if c, ok := call(x, "handler", "InvitationsByEmail"); ok && !containsCall(options, c) {
					options = append(options, c)
				}
				if c, ok := call(x, "handler", "NewAdminAuth"); ok {
					auths = append(auths, c)
				}
			}
			return true
		})
	}
	walk(run.Body, false)

	if len(transports) != 1 || transportVar == "" {
		t.Fatalf("run() calls mail.New %d time(s) (assigned to %q); EM-7B builds ONE transport for both e-mail flows",
			len(transports), transportVar)
	}
	if len(resets) != 1 || len(invitations) != 1 {
		t.Fatalf("run() calls handler.NewEmailResetChannel %d time(s) and handler.NewEmailInvitations %d time(s); want one each",
			len(resets), len(invitations))
	}
	// ONE SENDER FOR BOTH, AND IT IS THE ONE BREAKER (EM-7A): the same identifier, and
	// run() assigns it from mail.NewBreaker(<the one transport>, …) — never the bare
	// transport. EM-7A's own pin (the breaker wiring test) holds the rest of that shape:
	// the transport reaches nothing but that call, and there is one breaker.
	resetFrom, invitesFrom := firstArg(resets[0]), firstArg(invitations[0])
	if resetFrom == "" || resetFrom != invitesFrom {
		t.Errorf("the reset channel is built from %q and the invitation route from %q: two senders, not one", resetFrom, invitesFrom)
	}
	if !wraps(run, invitesFrom, "NewBreaker", transportVar) {
		t.Errorf("the invitation route is built from %q, which run() does not assign from mail.NewBreaker(%s, …): "+
			"its sends would escape the process-wide count", invitesFrom, transportVar)
	}
	if len(options) != 1 || !optionInEmailArm || optionsVar == "" {
		t.Fatalf("handler.InvitationsByEmail is called %d time(s), inside the InviteDeliveryEmail arm: %v; "+
			"the panel must get the e-mail route only when TAPPA_INVITE_DELIVERY=email", len(options), optionInEmailArm)
	}
	if len(auths) != 1 {
		t.Fatalf("run() calls handler.NewAdminAuth %d time(s); want one", len(auths))
	}
	a := auths[0]
	last, ok := a.Args[len(a.Args)-1].(*ast.Ident)
	if a.Ellipsis == token.NoPos || !ok || last.Name != optionsVar {
		t.Errorf("handler.NewAdminAuth does not spread %q as its options: the e-mail route is built and never given to the panel",
			optionsVar)
	}
}

// inviteMintAllowance is what TestShutdownBudget_TheInvitationEmailNestsInsideTheHTTPGrace
// leaves for the press's own database work — the person read and the minting
// transaction (the business's lock, the count, the retirement, the insert, the address
// read) — before the send starts. passwordChangeAllowance's size, for the same kind of
// work.
const inviteMintAllowance = 5 * time.Second

// TestShutdownBudget_TheInvitationEmailNestsInsideTheHTTPGrace binds the invitation
// e-mail's two budgets (M10 EM-7B, internal/invite's EmailSendGrace and
// EmailRecordGrace) to the drain they run inside: the send happens IN the request that
// pressed the button, so http.Server.Shutdown waits for it, and the send and the row
// are SEQUENTIAL, each on its own clock, after the mint. If they stop fitting, a press
// in flight at a deploy is cut after its code was minted and before its row — the
// silent state §4.6 forbids. The password notice's test is the precedent.
func TestShutdownBudget_TheInvitationEmailNestsInsideTheHTTPGrace(t *testing.T) {
	worst := invite.EmailSendGrace + invite.EmailRecordGrace + inviteMintAllowance
	if worst > httpShutdownGrace {
		t.Fatalf("a press and its invitation e-mail can take %v (invite.EmailSendGrace %v + "+
			"invite.EmailRecordGrace %v + %v for the mint) but Shutdown only waits httpShutdownGrace (%v)",
			worst, invite.EmailSendGrace, invite.EmailRecordGrace, inviteMintAllowance, httpShutdownGrace)
	}
	if invite.EmailSendGrace < 2*time.Second || invite.EmailRecordGrace < time.Second {
		t.Errorf("the invitation e-mail's budgets (%v to send, %v to record) are too short for one relay "+
			"conversation and one INSERT", invite.EmailSendGrace, invite.EmailRecordGrace)
	}
}

// wraps reports whether run() assigns name from a call to mail.<fn> one of whose
// arguments is the identifier inner — `name, err = mail.NewBreaker(inner, …)`.
func wraps(run *ast.FuncDecl, name, fn, inner string) bool {
	found := false
	ast.Inspect(run.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 {
			return true
		}
		c, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if pkg, isID := selX(sel); !ok || !isID || pkg != "mail" || sel.Sel.Name != fn {
			return true
		}
		named := false
		for _, l := range as.Lhs {
			if id, ok := l.(*ast.Ident); ok && id.Name == name {
				named = true
			}
		}
		for _, a := range c.Args {
			if id, ok := a.(*ast.Ident); ok && id.Name == inner && named {
				found = true
			}
		}
		return true
	})
	return found
}

// selX is the package identifier of a selector expression, if it is one.
func selX(sel *ast.SelectorExpr) (string, bool) {
	if sel == nil {
		return "", false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	return id.Name, true
}

func containsCall(calls []*ast.CallExpr, c *ast.CallExpr) bool {
	for _, x := range calls {
		if x == c {
			return true
		}
	}
	return false
}
