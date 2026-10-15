package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestAPIWiring_TheChainTakesThePanelsPrefixAndTheOneWorker pins M11 API-4's wiring as the command
// spells it (no handler test can see it -- each builds its own surface):
//
//	the prefix   the command calls apikey.NewAuthenticator exactly ONCE, in openAPISurface, with
//	             apikey.LivePrefix and cfg.APIKeyHMACKey -- the two arguments
//	             TestAPIKeyWiring_TheKeysServiceReachesThePanel pins on the panel's apikey.NewKeys, so a
//	             key the panel issues is a key the chain accepts (plan §5, API-3's hand-over; API-11
//	             changes both together)
//	the surface  api.New once, with api.NoSuspensionColumn{} (until OP-15), the worker parameter as
//	             its Work and the venues parameter as its Locations (API-5)
//	the venues   run() hands openAPISurface, as its venues argument, the identifier it assigned from
//	             tenant.NewVenues -- the PANEL's venue domain, so a venue the API writes passes the
//	             panel's rules and leaves its trail through the panel's recorder
//	K-19         run() calls webhook.NewWorker once, assigning one identifier W, and in its OWN body
//	             starts W, hands W to checkins.NoticeWorkTo and to openAPISurface, and passes W inside
//	             the drainers list shutdown drains
//
// MUTANTS (measured, API-4 report): the chain built with apikey.SandboxPrefix; NoticeWorkTo removed;
// W left out of the drainers -- each red. API-5 report: Locations left out of api.New; another
// tenant.NewVenues built for the API -- each red.
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
	venuesParam := open.Type.Params.List[3].Names[0].Name
	var suspension, work, locations bool
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
			case "Locations":
				if id, ok := kv.Value.(*ast.Ident); ok && id.Name == venuesParam {
					locations = true
				}
			}
		}
	}
	if !suspension || !work || !locations {
		t.Fatalf("api.New: Suspension api.NoSuspensionColumn{} %v, Work the %q parameter %v, Locations the %q parameter %v",
			suspension, workParam, work, venuesParam, locations)
	}
	if n := len(calls(apiFile, "tenant", "NewVenues")); n != 0 {
		t.Fatalf("api.go builds its own tenant.NewVenues (%d); the API takes run()'s", n)
	}

	run := fn(mainFile, "run")
	if n := len(calls(mainFile, "webhook", "NewWorker")); n != 1 {
		t.Fatalf("main.go calls webhook.NewWorker %d times, want 1", n)
	}
	worker, venues := "", ""
	for _, st := range run.Body.List {
		if as, ok := st.(*ast.AssignStmt); ok && len(as.Rhs) == 1 {
			if c, ok := as.Rhs[0].(*ast.CallExpr); ok && isSel(c.Fun, "webhook", "NewWorker") {
				worker = as.Lhs[0].(*ast.Ident).Name
			}
			if c, ok := as.Rhs[0].(*ast.CallExpr); ok && isSel(c.Fun, "tenant", "NewVenues") {
				venues = as.Lhs[0].(*ast.Ident).Name
			}
		}
	}
	if n := len(calls(mainFile, "tenant", "NewVenues")); n != 1 || venues == "" {
		t.Fatalf("main.go calls tenant.NewVenues %d times (assigned in run()'s body: %q), want once", n, venues)
	}
	if worker == "" {
		t.Fatal("webhook.NewWorker is not assigned in run()'s own body")
	}
	started, noticed, surfaced, drained, venuesSurfaced := false, false, false, false, false
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
			if len(c.Args) > 3 {
				if id, ok := c.Args[3].(*ast.Ident); ok && id.Name == venues {
					venuesSurfaced = true
				}
			}
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
	if !venuesSurfaced {
		t.Fatalf("openAPISurface is not handed %q (tenant.NewVenues's result) as its venues argument", venues)
	}
}

// TestAPIWiring_TheSuspensionReaderFollowsTheColumn is the tripwire of the suspension checkpoint
// (API-4 review, both lenses): openAPISurface wires api.NoSuspensionColumn because this branch has no
// tenants.suspended_at -- a reader that always says "not suspended". The day OP-15 brings the column,
// that wiring silently keeps every business unsuspendable through /v1. So this test reads whether
// the column exists, two ways, and refuses the pair "column present + NoSuspensionColumn wired":
//
//	the schema     tenants.suspended_at in the live catalog (DATABASE_URL; skipped without one,
//	               which CI always has)
//	the migrations any db/migrations file naming suspended_at (no database needed)
//
// API-4b closes when this turns red and the reader is replaced (plan §5, API-4b).
//
// MUTANT (measured, API-4 review report): the column added to a throwaway database -> red; a
// migration file naming it -> red.
func TestAPIWiring_TheSuspensionReaderFollowsTheColumn(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRoot, "cmd", "tappa", "api.go"))
	if err != nil {
		t.Fatal(err)
	}
	wired := strings.Contains(string(src), "Suspension: api.NoSuspensionColumn{}")
	if !wired {
		t.Log("the checkpoint is no longer wired to api.NoSuspensionColumn; this tripwire has nothing left to guard")
		return
	}
	files, err := filepath.Glob(filepath.Join(repoRoot, "db", "migrations", "*.sql"))
	if err != nil || len(files) < 50 {
		t.Fatalf("anti-vacuity: %d migration files (%v)", len(files), err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "suspended_at") {
			t.Errorf("%s names suspended_at while /v1's suspension checkpoint is wired to api.NoSuspensionColumn: "+
				"replace the reader with the column's read and write API-4b's table test", filepath.Base(f))
		}
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Log("DATABASE_URL not set: the schema arm is skipped (CI runs it)")
		return
	}
	ctx := context.Background()
	c, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = c.Close(ctx) }()
	var has, tenants bool
	if err := c.QueryRow(ctx, `SELECT to_regclass('public.tenants') IS NOT NULL,
	                                  EXISTS (SELECT 1 FROM pg_catalog.pg_attribute
	                                           WHERE attrelid = to_regclass('public.tenants') AND attname = 'suspended_at' AND NOT attisdropped)`).
		Scan(&tenants, &has); err != nil {
		t.Fatal(err)
	}
	if !tenants {
		t.Fatal("CONTROL: the database has no tenants table")
	}
	if has {
		t.Error("tenants.suspended_at exists while /v1's suspension checkpoint is wired to api.NoSuspensionColumn: " +
			"every business is unsuspendable through /v1 -- replace the reader and write API-4b's table test")
	}
}
