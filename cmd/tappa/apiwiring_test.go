package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestAPIWiring_TheChainTakesThePanelsPrefixAndTheOneWorker pins M11 API-4's wiring as the command
// spells it (no handler test can see it -- each builds its own surface):
//
//	the prefix   the command calls apikey.NewAuthenticator exactly ONCE, in openAPISurface, with
//	             apiKeyPrefix (the build's: TestAPIKeyPrefix_IsTheBuilds) and cfg.APIKeyHMACKey -- the two arguments
//	             TestAPIKeyWiring_TheKeysServiceReachesThePanel pins on the panel's apikey.NewKeys, so a
//	             key the panel issues is a key the chain accepts (plan §5, API-3's hand-over; API-11
//	             changes both together)
//	the surface  api.New once, with tenant.WriteSuspension{} (API-4b: OP-15's column), the worker parameter as
//	             its Work and the venues parameter as its Locations (API-5)
//	the venues   run() hands openAPISurface, as its venues argument, the identifier it assigned from
//	             tenant.NewVenues -- the PANEL's venue domain, so a venue the API writes passes the
//	             panel's rules and leaves its trail through the panel's recorder
//	K-19         run() calls webhook.NewWorker once, assigning one identifier W, and in its OWN body
//	             starts W, hands W to checkins.NoticeWorkTo and to openAPISurface, and passes W inside
//	             the drainers list shutdown drains
//
//	API-6        api.New's Employees is the staff parameter, which run() hands as tenant.NewStaff's
//	             result (the PANEL's employee domain); run() builds invite.NewQueue once from its
//	             invite.New result and the e-mail transport's QueueSink, adds it to W, and starts W
//	             after that
//	API-7        api.New's Punches is the result of the one tenant.NewPunchFeed call, made in
//	             openAPISurface over its data parameter (the request pool -- tappa_app, RLS)
//
// MUTANTS (measured, API-4 report): the chain built with apikey.SandboxPrefix; NoticeWorkTo removed;
// W left out of the drainers -- each red. API-5 report: Locations left out of api.New; another
// tenant.NewVenues built for the API -- each red. API-6 report: Employees left out; W started before
// the queue is added; the queue's sink not the transport's QueueSink -- each red. API-7 report:
// Punches left out of api.New (New refuses it -- the build's own test is red); NewPunchFeed handed
// another value than the data parameter; a second NewPunchFeed in main.go -- each red.
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
	if a := auths[0].Args; len(a) < 2 || !isIdent(a[0], "apiKeyPrefix") || !isSel(a[1], "cfg", "APIKeyHMACKey") {
		t.Fatal("the chain is not built with apiKeyPrefix and cfg.APIKeyHMACKey -- the panel's two arguments")
	}
	news := calls(apiFile, "api", "New")
	if len(news) != 1 {
		t.Fatalf("api.New is called %d times", len(news))
	}
	workParam := open.Type.Params.List[2].Names[0].Name
	venuesParam := open.Type.Params.List[3].Names[0].Name
	staffParam := open.Type.Params.List[4].Names[0].Name
	dataParam := open.Type.Params.List[1].Names[0].Name
	// API-7: the feed reader is built HERE, once, over the data parameter, and is api.New's Punches.
	feeds := calls(apiFile, "tenant", "NewPunchFeed")
	punchesIdent := ""
	if len(feeds) == 1 && len(calls(open, "tenant", "NewPunchFeed")) == 1 && len(feeds[0].Args) == 1 {
		if id, ok := feeds[0].Args[0].(*ast.Ident); ok && id.Name == dataParam {
			ast.Inspect(open.Body, func(n ast.Node) bool {
				if as, ok := n.(*ast.AssignStmt); ok && len(as.Rhs) == 1 && as.Rhs[0] == ast.Expr(feeds[0]) {
					punchesIdent = as.Lhs[0].(*ast.Ident).Name
				}
				return true
			})
		}
	}
	if punchesIdent == "" || len(calls(mainFile, "tenant", "NewPunchFeed")) != 0 {
		t.Fatalf("tenant.NewPunchFeed: %d call(s) in api.go, assigned to %q; want one, in openAPISurface, over its %q parameter (and none in main.go)",
			len(feeds), punchesIdent, dataParam)
	}
	var suspension, work, locations, employees, punches bool
	if lit, ok := news[0].Args[0].(*ast.CompositeLit); ok {
		for _, e := range lit.Elts {
			kv, ok := e.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			switch kv.Key.(*ast.Ident).Name {
			case "Suspension":
				if c, ok := kv.Value.(*ast.CompositeLit); ok && isSel(c.Type, "tenant", "WriteSuspension") {
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
			case "Employees":
				if id, ok := kv.Value.(*ast.Ident); ok && id.Name == staffParam {
					employees = true
				}
			case "Punches":
				if id, ok := kv.Value.(*ast.Ident); ok && id.Name == punchesIdent {
					punches = true
				}
			}
		}
	}
	if !suspension || !work || !locations || !employees || !punches {
		t.Fatalf("api.New: Suspension tenant.WriteSuspension{} %v, Work the %q parameter %v, Locations the %q parameter %v, Employees the %q parameter %v, Punches %q %v",
			suspension, workParam, work, venuesParam, locations, staffParam, employees, punchesIdent, punches)
	}
	if n := len(calls(apiFile, "tenant", "NewStaff")); n != 0 {
		t.Fatalf("api.go builds its own tenant.NewStaff (%d); the API takes run()'s", n)
	}
	if n := len(calls(apiFile, "tenant", "NewVenues")); n != 0 {
		t.Fatalf("api.go builds its own tenant.NewVenues (%d); the API takes run()'s", n)
	}

	run := fn(mainFile, "run")
	if n := len(calls(mainFile, "webhook", "NewWorker")); n != 1 {
		t.Fatalf("main.go calls webhook.NewWorker %d times, want 1", n)
	}
	worker, venues, staff, invites := "", "", "", ""
	for _, st := range run.Body.List {
		if as, ok := st.(*ast.AssignStmt); ok && len(as.Rhs) == 1 {
			if c, ok := as.Rhs[0].(*ast.CallExpr); ok && isSel(c.Fun, "webhook", "NewWorker") {
				worker = as.Lhs[0].(*ast.Ident).Name
			}
			if c, ok := as.Rhs[0].(*ast.CallExpr); ok && isSel(c.Fun, "tenant", "NewVenues") {
				venues = as.Lhs[0].(*ast.Ident).Name
			}
			if c, ok := as.Rhs[0].(*ast.CallExpr); ok && isSel(c.Fun, "tenant", "NewStaff") {
				staff = as.Lhs[0].(*ast.Ident).Name
			}
			if c, ok := as.Rhs[0].(*ast.CallExpr); ok && isSel(c.Fun, "invite", "New") {
				invites = as.Lhs[0].(*ast.Ident).Name
			}
		}
	}
	if staff == "" || invites == "" || len(calls(mainFile, "tenant", "NewStaff")) != 1 || len(calls(mainFile, "invite", "New")) != 1 {
		t.Fatalf("run() assigns tenant.NewStaff to %q and invite.New to %q; want each once, in its own body", staff, invites)
	}
	if n := len(calls(mainFile, "tenant", "NewVenues")); n != 1 || venues == "" {
		t.Fatalf("main.go calls tenant.NewVenues %d times (assigned in run()'s body: %q), want once", n, venues)
	}
	if worker == "" {
		t.Fatal("webhook.NewWorker is not assigned in run()'s own body")
	}
	started, noticed, surfaced, drained, venuesSurfaced, staffSurfaced := false, false, false, false, false, false
	var startedAt, addedAt token.Pos
	queueIdent, queueSink := "", false
	ast.Inspect(run.Body, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok && len(as.Rhs) == 1 {
			if c, ok := as.Rhs[0].(*ast.CallExpr); ok && isSel(c.Fun, "invite", "NewQueue") && len(c.Args) == 3 {
				queueIdent = as.Lhs[0].(*ast.Ident).Name
				if id, ok := c.Args[0].(*ast.Ident); !ok || id.Name != invites {
					queueIdent = ""
				}
				if sc, ok := c.Args[1].(*ast.CallExpr); ok {
					if sel, ok := sc.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "QueueSink" {
						queueSink = true
					}
				}
			}
		}
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
			started, startedAt = true, c.Pos()
		case isSel(c.Fun, worker, "Add") && len(c.Args) == 1:
			if id, ok := c.Args[0].(*ast.Ident); ok && id.Name == queueIdent && queueIdent != "" {
				addedAt = c.Pos()
			}
		case isSel(c.Fun, "checkins", "NoticeWorkTo") && argIs(0):
			noticed = true
		case exprName(c.Fun) == "openAPISurface" && argIs(2):
			surfaced = true
			if len(c.Args) > 4 {
				if id, ok := c.Args[3].(*ast.Ident); ok && id.Name == venues {
					venuesSurfaced = true
				}
				if id, ok := c.Args[4].(*ast.Ident); ok && id.Name == staff {
					staffSurfaced = true
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
	if !staffSurfaced {
		t.Fatalf("openAPISurface is not handed %q (tenant.NewStaff's result) as its staff argument", staff)
	}
	// API-6: the invitation queue is built from run()'s invite.Manager and the panel's e-mail
	// transport (EmailInvitations.QueueSink), added to THE worker, and the worker starts after it.
	if n := len(calls(mainFile, "invite", "NewQueue")); n != 1 || queueIdent == "" || !queueSink {
		t.Fatalf("invite.NewQueue: %d calls, assigned to %q, with a QueueSink %v; want once, from %q and the e-mail transport's QueueSink",
			n, queueIdent, queueSink, invites)
	}
	if addedAt == token.NoPos || !(addedAt < startedAt) {
		t.Fatalf("the queue is added to the worker at %v and the worker started at %v; want added, then started", addedAt, startedAt)
	}
}

// TestAPIWiring_TheSuspensionReaderFollowsTheColumn was API-4's tripwire (both lenses of its review):
// openAPISurface wired api.NoSuspensionColumn -- a reader that always said "not suspended" -- because
// the branch had no tenants.suspended_at, and the test turned red the day OP-15 brought the column.
// It did (00063); API-4b replaced the reader, and the test now pins the REPLACEMENT -- the arm "the
// column exists and the always-false reader is wired" is unreachable (the type is gone), so it is not
// kept; the arm "the column's reader is wired" is tightened to five:
//
//	the column      a db/migrations file names suspended_at, and (DATABASE_URL) tenants.suspended_at is
//	                in the live catalog -- the anti-vacuity of everything below
//	the wiring      api.New's Suspension in api.go is exactly the literal tenant.WriteSuspension{}
//	one reader      in the whole module's non-test Go, the ONE method named Suspended is
//	                tenant.WriteSuspension's -- no stand-in (always false, a cache, a set) can be
//	                declared beside it and wired instead
//	its read        that method calls store's GetTenantSuspended and nothing else of store; the query
//	                (db/queries/tenants.sql) reads suspended_at from tenants by `id = @tenant_id`, with
//	                no row-lock clause (API-4b's measured decision -- suspension.go)
//	the behaviour   (DATABASE_URL + DATABASE_MIGRATE_URL) through openAPISurface itself: a business whose
//	                suspended_at is set is answered 403 tenant_suspended on PUT /v1/locations and the
//	                venue is not written; once lifted, the same PUT is 201
//
// The suspension here is set by the OWNER (a direct UPDATE of the two columns), not by
// op_suspend_tenant: this test's subject is which reader the command wires, and the operator's path
// end to end is internal/handler/api's TestSuspension_* (suspension_db_test.go).
//
// MUTANTS (measured, API-4b report): api.go wiring an always-false reader (re-declared in api) ->
// red (the wiring, one reader, the behaviour); the query reading another column -> red (its read, the
// behaviour); FOR SHARE added to the query -> red (its read).
func TestAPIWiring_TheSuspensionReaderFollowsTheColumn(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(repoRoot, "db", "migrations", "*.sql"))
	if err != nil || len(files) < 50 {
		t.Fatalf("anti-vacuity: %d migration files (%v)", len(files), err)
	}
	named := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "suspended_at") {
			named++
		}
	}
	if named == 0 {
		t.Fatal("no migration names suspended_at: the column the checkpoint reads does not exist")
	}

	// The wiring.
	fset := token.NewFileSet()
	apiFile, err := parser.ParseFile(fset, filepath.Join(repoRoot, "cmd", "tappa", "api.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	wired, seen := false, 0
	ast.Inspect(apiFile, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		if k, ok := kv.Key.(*ast.Ident); !ok || k.Name != "Suspension" {
			return true
		}
		seen++
		lit, ok := kv.Value.(*ast.CompositeLit)
		if !ok || len(lit.Elts) != 0 {
			return true
		}
		if sel, ok := lit.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "WriteSuspension" && isIdent(sel.X, "tenant") {
			wired = true
		}
		return true
	})
	if seen != 1 || !wired {
		t.Fatalf("api.go: %d Suspension field(s), wired to tenant.WriteSuspension{} %v; want exactly one, that literal", seen, wired)
	}

	// One reader in the module, and what it reads.
	var readers []string
	var readerCalls []string
	err = filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".tools", "node_modules", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || fd.Name.Name != "Suspended" {
				continue
			}
			recv := fd.Recv.List[0].Type
			if st, ok := recv.(*ast.StarExpr); ok {
				recv = st.X
			}
			name := "?"
			if id, ok := recv.(*ast.Ident); ok {
				name = id.Name
			}
			rel, _ := filepath.Rel(repoRoot, path)
			readers = append(readers, filepath.ToSlash(rel)+":"+name)
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok {
					if sel, ok := c.Fun.(*ast.SelectorExpr); ok {
						readerCalls = append(readerCalls, sel.Sel.Name)
					}
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(readers) != 1 || readers[0] != "internal/domain/tenant/suspension.go:WriteSuspension" {
		t.Fatalf("methods named Suspended in the module's non-test Go: %v; want only internal/domain/tenant/suspension.go:WriteSuspension", readers)
	}
	if !slices.Contains(readerCalls, "GetTenantSuspended") {
		t.Fatalf("tenant.WriteSuspension.Suspended calls %v; want store's GetTenantSuspended", readerCalls)
	}
	q, err := os.ReadFile(filepath.Join(repoRoot, "db", "queries", "tenants.sql"))
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(q), "-- name: GetTenantSuspended :one")
	if i < 0 {
		t.Fatal("db/queries/tenants.sql declares no GetTenantSuspended")
	}
	body := string(q[i:])
	if j := strings.Index(body[1:], "-- name: "); j >= 0 {
		body = body[:j+1]
	}
	var stmt []string
	for _, line := range strings.Split(body, "\n") {
		if l := strings.TrimSpace(line); l != "" && !strings.HasPrefix(l, "--") {
			stmt = append(stmt, l)
		}
	}
	want := "SELECT (suspended_at IS NOT NULL)::boolean AS suspended FROM tenants WHERE id = @tenant_id;"
	if got := strings.Join(stmt, " "); got != want {
		t.Fatalf("GetTenantSuspended is\n  %s\nwant\n  %s\n(the column, the belt, and no row lock -- suspension.go's measured decision)", got, want)
	}

	appDSN, ownerDSN := os.Getenv("DATABASE_URL"), os.Getenv("DATABASE_MIGRATE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Log("DATABASE_URL / DATABASE_MIGRATE_URL not set: the schema and behaviour arms are skipped (CI runs them)")
		return
	}
	sh := newShippedAPI(t, "API-4b wiring", "APIFOURB")
	ctx := context.Background()
	var has bool
	if err := sh.owner.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_attribute
	                                   WHERE attrelid = to_regclass('public.tenants') AND attname = 'suspended_at' AND NOT attisdropped)`).Scan(&has); err != nil || !has {
		t.Fatalf("tenants.suspended_at in the live catalog: %v (%v)", has, err)
	}
	set := func(on bool) {
		t.Helper()
		q := `UPDATE tenants SET suspended_at = NULL, suspension_reason = NULL WHERE id = $1`
		if on {
			q = `UPDATE tenants SET suspended_at = clock_timestamp(), suspension_reason = 'non_payment' WHERE id = $1`
		}
		if _, err := sh.owner.Exec(ctx, q, sh.biz); err != nil {
			t.Fatal(err)
		}
	}
	// A committed suspension left behind would make 00063's Down refuse (K15-6).
	t.Cleanup(func() {
		if _, err := sh.owner.Exec(context.Background(), `UPDATE tenants SET suspended_at = NULL, suspension_reason = NULL WHERE id = $1`, sh.biz); err != nil {
			t.Errorf("lift the fixture's suspension: %v", err)
		}
	})
	ref := "SUSP-" + uuid.NewString()[:8]
	put := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "http://"+shippedAPIHost+"/v1/locations/"+ref, strings.NewReader(`{"name":"Birgu","active":true}`))
		req.Header.Set("Authorization", "Bearer "+sh.raw)
		w := httptest.NewRecorder()
		sh.h.ServeHTTP(w, req)
		return w
	}
	venues := func() (n int) {
		t.Helper()
		if err := sh.owner.QueryRow(ctx, `SELECT count(*) FROM locations WHERE tenant_id = $1 AND external_ref = $2`, sh.biz, ref).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	set(true)
	if w := put(); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), `"code":"tenant_suspended"`) || venues() != 0 {
		t.Fatalf("suspended, through the shipped surface: %d %s, %d venue(s); want 403 tenant_suspended and none", w.Code, w.Body.String(), venues())
	}
	set(false)
	if w := put(); w.Code != http.StatusCreated || venues() != 1 {
		t.Fatalf("lifted, through the shipped surface: %d %s, %d venue(s); want 201 and one", w.Code, w.Body.String(), venues())
	}
}
