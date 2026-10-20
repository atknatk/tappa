package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestWebhookWiring_TheEndpointServiceReachesThePanel pins cmd/tappa's API-9 wiring (ADR 0028 §6, §7):
// run() builds ONE egress client, with exactly two options -- the configured node/ingress list
// (cfg.WebhookBlockedAddresses) and the development allowance tied to TAPPA_ENV=dev and nothing else --
// hands THAT client and cfg.WebhookKEK to ONE endpoints.New, and appends handler.IntegrationWebhook
// of that service to the options NewAdminAuth spreads. Then: internal/egress is imported by no
// production package but cmd/tappa, internal/webhook/endpoints and internal/handler (the client is
// built in one place; a second, hand-assembled client would escape these pins).
//
// API-10 (the sender): run() calls delivery.New ONCE, with cfg.WebhookKEK and THE SAME egress client
// (one SSRF gate for the ping and every delivery), adds its result to K-19's worker (W.Add), and hands W
// to the endpoint service's and the reviewer's NoticeWorkTo (a resend and an approval enter K-19's set);
// internal/webhook/delivery joins the packages that import internal/egress.
//
// API-13a (K-22's operator screen): the operator surface run() builds (openOperatorSurface) is the one
// it hands appRouter (since OP-15 phase B the composition httpx.NewRouter is reached through --
// TestAppRouter_IsTheOnlyComposition holds that appRouter hands every feature on), and W reaches its NoticeWorkTo too -- an accepted API open puts the
// business into K-19's set at once (ADR 0028, "API-13a uygulama notu" 2); without the call the open
// still answers "Opened." and the waiting deliveries sit until delivery.GateRecheck.
//
// MUTANTS (measured): the allowance set to true -> red; the Blocked option dropped -> red; the panel
// option appended with another value -> red. API-10: delivery.New given another sender, the job not
// added to W, either NoticeWorkTo removed -> each red. API-13a: operatorSurface.NoticeWorkTo(work)
// removed -> red.
func TestWebhookWiring_TheEndpointServiceReachesThePanel(t *testing.T) {
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
		t.Fatal("main.go declares no run()")
	}
	isSel := func(e ast.Expr, pkg, name string) bool {
		sel, ok := e.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != name {
			return false
		}
		id, ok := sel.X.(*ast.Ident)
		return ok && id.Name == pkg
	}
	var egressCalls, endpointCalls, panelCalls, deliveryCalls int
	egressVar, endpointsVar, deliveryVar, workVar, reviewerVar, operatorVar := "", "", "", "", "", ""
	panelOK := false
	ast.Inspect(run, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok && isSel(c.Fun, "handler", "IntegrationWebhook") {
			panelCalls++
		}
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 || len(as.Lhs) == 0 {
			return true
		}
		c, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		lhs, _ := as.Lhs[0].(*ast.Ident)
		switch {
		case isSel(c.Fun, "egress", "New"):
			egressCalls++
			if lhs != nil {
				egressVar = lhs.Name
			}
			lit, ok := c.Args[0].(*ast.CompositeLit)
			if !ok || len(c.Args) != 1 || len(lit.Elts) != 2 {
				t.Fatal("egress.New is not called with one Options literal of exactly two fields")
			}
			for _, e := range lit.Elts {
				kv := e.(*ast.KeyValueExpr)
				switch kv.Key.(*ast.Ident).Name {
				case "Blocked":
					if !isSel(kv.Value, "cfg", "WebhookBlockedAddresses") {
						t.Error("Blocked is not cfg.WebhookBlockedAddresses")
					}
				case "DevelopmentWithoutBlockedList":
					be, ok := kv.Value.(*ast.BinaryExpr)
					if !ok || be.Op != token.EQL || !isSel(be.X, "cfg", "Env") || !isSel(be.Y, "config", "EnvDev") {
						t.Error("the development allowance is not cfg.Env == config.EnvDev")
					}
				default:
					t.Errorf("egress.New takes an unexpected option %s", kv.Key.(*ast.Ident).Name)
				}
			}
		case isSel(c.Fun, "webhook", "NewWorker"):
			if lhs != nil {
				workVar = lhs.Name
			}
		case isSel(c.Fun, "review", "NewReviewer"):
			if lhs != nil {
				reviewerVar = lhs.Name
			}
		case exprName(c.Fun) == "openOperatorSurface":
			if lhs != nil {
				operatorVar = lhs.Name
			}
		case isSel(c.Fun, "delivery", "New"):
			deliveryCalls++
			if lhs != nil {
				deliveryVar = lhs.Name
			}
			sender, _ := c.Args[2].(*ast.Ident)
			if len(c.Args) != 4 || !isSel(c.Args[0], "cfg", "WebhookKEK") || sender == nil || sender.Name != egressVar {
				t.Error("delivery.New is not given cfg.WebhookKEK and the egress client built above")
			}
		case isSel(c.Fun, "endpoints", "New"):
			endpointCalls++
			if lhs != nil {
				endpointsVar = lhs.Name
			}
			last, _ := c.Args[len(c.Args)-1].(*ast.Ident)
			if len(c.Args) != 4 || !isSel(c.Args[0], "cfg", "WebhookKEK") || last == nil || last.Name != egressVar {
				t.Error("endpoints.New is not given cfg.WebhookKEK and the egress client built above")
			}
		default:
			if fn, ok := c.Fun.(*ast.Ident); ok && fn.Name == "append" && len(c.Args) == 2 {
				if iw, ok := c.Args[1].(*ast.CallExpr); ok && isSel(iw.Fun, "handler", "IntegrationWebhook") {
					// Counted once, below, where Inspect reaches the call itself.
					arg, _ := iw.Args[0].(*ast.Ident)
					base, _ := c.Args[0].(*ast.Ident)
					panelOK = arg != nil && arg.Name == endpointsVar && endpointsVar != "" && base != nil && lhs != nil &&
						base.Name == "panelOptions" && lhs.Name == "panelOptions"
				}
			}
		}
		return true
	})
	if egressCalls != 1 || endpointCalls != 1 || panelCalls != 1 || !panelOK {
		t.Fatalf("run(): egress.New %d, endpoints.New %d, handler.IntegrationWebhook %d (appended to panelOptions with the service: %v)",
			egressCalls, endpointCalls, panelCalls, panelOK)
	}

	// API-10: the job joins the worker, and a resend and an approval notice it.
	var added, endpointsNotice, reviewerNotice, operatorNotice, operatorRouted bool
	ast.Inspect(run, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if ok && isIdent(c.Fun, "appRouter") {
			for _, a := range c.Args {
				if id, isID := a.(*ast.Ident); isID && id.Name == operatorVar && operatorVar != "" {
					operatorRouted = true
				}
			}
		}
		if !ok || len(c.Args) != 1 {
			return true
		}
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		recv, _ := sel.X.(*ast.Ident)
		arg, _ := c.Args[0].(*ast.Ident)
		if recv == nil || arg == nil {
			return true
		}
		switch {
		case sel.Sel.Name == "Add" && recv.Name == workVar && arg.Name == deliveryVar:
			added = true
		case sel.Sel.Name == "NoticeWorkTo" && recv.Name == endpointsVar && arg.Name == workVar:
			endpointsNotice = true
		case sel.Sel.Name == "NoticeWorkTo" && recv.Name == reviewerVar && arg.Name == workVar:
			reviewerNotice = true
		case sel.Sel.Name == "NoticeWorkTo" && recv.Name == operatorVar && arg.Name == workVar:
			operatorNotice = true
		}
		return true
	})
	if deliveryCalls != 1 || workVar == "" || deliveryVar == "" || !added || !endpointsNotice || !reviewerNotice {
		t.Fatalf("run(): delivery.New %d (as %q), added to the worker %q: %v; NoticeWorkTo -- endpoints %v, reviewer %v",
			deliveryCalls, deliveryVar, workVar, added, endpointsNotice, reviewerNotice)
	}
	if operatorVar == "" || !operatorRouted || !operatorNotice {
		t.Fatalf("run(): the operator surface (openOperatorSurface as %q) handed to appRouter: %v; W handed to its NoticeWorkTo: %v",
			operatorVar, operatorRouted, operatorNotice)
	}

	allowed := map[string]bool{"cmd/tappa": true, "internal/webhook/endpoints": true, "internal/handler": true, "internal/webhook/delivery": true}
	importers := 0
	err = filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == ".git" || n == "node_modules" || n == "android" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, im := range src.Imports {
			if p, _ := strconv.Unquote(im.Path.Value); p == "github.com/atknatk/tappa/internal/egress" {
				rel, _ := filepath.Rel(repoRoot, filepath.Dir(path))
				importers++
				if !allowed[filepath.ToSlash(rel)] {
					t.Errorf("%s imports internal/egress", rel)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if importers < 4 {
		t.Fatalf("CONTROL: only %d production files import internal/egress", importers)
	}
}
