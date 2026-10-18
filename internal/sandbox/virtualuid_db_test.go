package sandbox

import (
	"context"
	"encoding/hex"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/encode"
	"github.com/atknatk/tappa/internal/sun"
)

// TestSandbox_RealNXPUIDRefusedOnEveryResolveAndInsert (ADR 0028 §11: "kontrol her çözümlemede ve
// her eklemede koşar (encoded_at'a dayanılmaz)"). A real NXP plaque row -- 0x04, active, on a wall,
// encoded_at left NULL as a real unstamped plaque may be -- is put into the database by the owner:
//
//	resolve  the development pool resolves it (CONTROL); the sandbox pool refuses it before the query
//	         -- ErrRealPlaqueRefused, which is pgx.ErrNoRows -- on the pool and on a transaction
//	         (GetTagByUIDOn, the simulation's resolver), and so do the two paths every tap takes over
//	         that pool: the tap page's SUN pre-check (unknown plaque) and the tap's recording
//	         (unknown plaque, nothing written)
//	insert   internal/encode's InsertUnassigned over the sandbox pool refuses a real UID before a
//	         transaction opens and writes no row; a virtual UID loads (CONTROL); the development pool
//	         admits a real UID (CONTROL)
//	prod     the production pool loads a real UID through internal/encode and resolves it, on the
//	         pool and on a transaction: production plaques are NXP chips
//	the code the only writers of a tags row (calls of InsertUnassigned outside internal/store) are
//	         internal/encode's and this package's minter (writePlaque), and each asks AdmitTagUID
//	         first; both resolvers' first statement is AdmitTagUID; db/queries has exactly one
//	         INSERT INTO tags
//
// MUTANTS (measured): AdmitTagUID removed from GetTagByUID (resolve rows red); removed from
// GetTagByUIDOn (the transaction row red); removed from encode's InsertUnassigned (insert row red);
// removed from writePlaque (the code row red); virtualTagsOnly keyed on EnvProd (resolve rows red);
// virtualTagsOnly keyed on cfg.IsHardened() -- production refusing its own chips (the prod row red); IsVirtualTagUID accepting any 14 hex digits
// (TestIsVirtualTagUID_IsTheRangeAndNothingElse red).
func TestSandbox_RealNXPUIDRefusedOnEveryResolveAndInsert(t *testing.T) {
	appDSN, ownerDSN := os.Getenv("DATABASE_URL"), os.Getenv("DATABASE_MIGRATE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("DATABASE_URL and DATABASE_MIGRATE_URL are both required (real Postgres)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	kek := random(t, 32)
	pool := func(env string) *db.DB {
		d, err := db.New(ctx, &config.Config{Env: env, DatabaseURL: appDSN})
		if err != nil {
			t.Fatalf("db.New (%s): %v", env, err)
		}
		t.Cleanup(d.Close)
		return d
	}
	sandboxPool, devPool, prodPool := pool(config.EnvSandbox), pool(config.EnvDev), pool(config.EnvProd)
	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Close(context.Background()) }()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := owner.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	tenantID, employee, venue := uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'API-11 NXP', $2, 'restaurant', 'multi')`, tenantID, "VAT-API11N-"+tenantID.String())
	exec(`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'Real wall')`, venue, tenantID)
	exec(`INSERT INTO employees (id, tenant_id, full_name, location_id, status, invited_at) VALUES ($1, $2, 'NXP Staff', $3, 'active', now())`, employee, tenantID, venue)
	nxp := "04" + strings.ToUpper(hex.EncodeToString(random(t, 6)))
	exec(`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status) VALUES ($1, $2, $3, $4, 5, 'active')`,
		nxp, tenantID, venue, random(t, sun.WrappedKeyLen))

	// resolve
	if tag, err := devPool.GetTagByUID(ctx, nxp); err != nil || tag.TenantID != tenantID {
		t.Fatalf("CONTROL: the development pool does not resolve the real plaque: %v", err)
	}
	_, err = sandboxPool.GetTagByUID(ctx, nxp)
	if !errors.Is(err, db.ErrRealPlaqueRefused) || !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("the sandbox pool resolved a real NXP plaque: %v", err)
	}
	// the resolver on a transaction the caller holds (the simulation's, runner.go): the same door
	onTx := func(d *db.DB, uid string) error {
		return d.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
			tag, err := d.GetTagByUIDOn(ctx, tx, uid)
			if err == nil && tag.TenantID != tenantID {
				return errors.New("resolved another tenant")
			}
			return err
		})
	}
	if err := onTx(devPool, nxp); err != nil {
		t.Fatalf("CONTROL: the development pool does not resolve the real plaque on a transaction: %v", err)
	}
	if err := onTx(sandboxPool, nxp); !errors.Is(err, db.ErrRealPlaqueRefused) {
		t.Fatalf("the sandbox pool resolved a real NXP plaque on a transaction: %v", err)
	}
	qr := sun.Params{UID: nxp, Channel: sun.ChannelQR}
	if _, err := sun.NewVerifier(devPool, kek).PreviewWithoutReplayProtection(ctx, qr); err != nil {
		t.Fatalf("CONTROL: the development pre-check: %v", err)
	}
	if _, err := sun.NewVerifier(sandboxPool, kek).PreviewWithoutReplayProtection(ctx, qr); !errors.Is(err, sun.ErrUnknownTag) {
		t.Fatalf("the sandbox tap page's pre-check: %v, want an unknown plaque", err)
	}
	trail, err := audit.New(sandboxPool)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := checkin.New(sandboxPool, trail, &config.Config{Env: config.EnvSandbox, GPSRadiusMeters: 150, Debounce: time.Minute, Freshness: 3 * time.Minute}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Record(ctx, checkin.Request{SessionTenantID: tenantID, EmployeeID: employee, TagUID: nxp, Channel: tap.ChannelQR,
		TagTenantID: tenantID, PageIssuedAt: time.Now()}); !errors.Is(err, checkin.ErrUnknownTag) {
		t.Fatalf("the sandbox tap's recording: %v, want an unknown plaque", err)
	}
	var rows int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM transactions WHERE tenant_id = $1`, tenantID).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("the refused tap wrote %d rows (%v)", rows, err)
	}

	// insert
	loader, err := encode.NewDBRows(sandboxPool, trail)
	if err != nil {
		t.Fatal(err)
	}
	nxp2 := "04" + strings.ToUpper(hex.EncodeToString(random(t, 6)))
	if err := loader.InsertUnassigned(ctx, tenantID, uuid.Nil, nxp2, random(t, sun.WrappedKeyLen), nil, "api11"); !errors.Is(err, db.ErrRealPlaqueRefused) {
		t.Fatalf("the sandbox loaded a real NXP UID: %v", err)
	}
	var n int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM tags WHERE uid = $1`, nxp2).Scan(&n); err != nil || n != 0 {
		t.Fatalf("the refused load left %d rows (%v)", n, err)
	}
	virtual := VirtualUID(uuid.New())
	if err := loader.InsertUnassigned(ctx, tenantID, uuid.Nil, virtual, random(t, sun.WrappedKeyLen), nil, "api11"); err != nil {
		t.Fatalf("CONTROL: the sandbox does not load a virtual UID: %v", err)
	}
	if err := devPool.AdmitTagUID(nxp2); err != nil {
		t.Fatalf("CONTROL: the development pool refuses a real UID: %v", err)
	}

	// prod (API-11 fix 1, the third eye's finding 1): production plaques ARE NXP chips, so the
	// production pool's doors are open -- a real UID is loaded through internal/encode and resolved,
	// on the pool and on a transaction, like any plaque.
	prodTrail, err := audit.New(prodPool)
	if err != nil {
		t.Fatal(err)
	}
	prodLoader, err := encode.NewDBRows(prodPool, prodTrail)
	if err != nil {
		t.Fatal(err)
	}
	nxp3 := "04" + strings.ToUpper(hex.EncodeToString(random(t, 6)))
	if err := prodLoader.InsertUnassigned(ctx, tenantID, uuid.Nil, nxp3, random(t, sun.WrappedKeyLen), nil, "api11"); err != nil {
		t.Fatalf("the production pool refused to load a real NXP UID: %v", err)
	}
	if tag, err := prodPool.GetTagByUID(ctx, nxp3); err != nil || tag.TenantID != tenantID || tag.UID != nxp3 {
		t.Fatalf("the production pool does not resolve the real plaque it loaded: %+v %v", tag, err)
	}
	if err := onTx(prodPool, nxp3); err != nil {
		t.Fatalf("the production pool does not resolve a real plaque on a transaction: %v", err)
	}

	// the code
	theWritersAskTheDoorFirst(t)
}

// theWritersAskTheDoorFirst is the code half: every use of the store's InsertUnassigned statement
// outside internal/store sits in a function that calls AdmitTagUID before it, and those functions are the two known ones;
// db.DB.GetTagByUID's first statement asks AdmitTagUID; db/queries has one INSERT INTO tags.
func theWritersAskTheDoorFirst(t *testing.T) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	writers := map[string]bool{}
	resolverAsks := map[string]bool{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			if strings.HasPrefix(rel, ".") && rel != "." || rel == "internal/store" || rel == "android" || rel == "web/static" {
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
			if !ok || fd.Body == nil {
				continue
			}
			// A WRITE is the store's statement: a function naming store.InsertUnassignedParams (the
			// Rows port's InsertUnassigned -- internal/encode's driver -- reaches the database only
			// through DBRows, which is one of these).
			admit, insert := token.NoPos, token.NoPos
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.CallExpr:
					if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "AdmitTagUID" && !admit.IsValid() {
						admit = x.Pos()
					}
				case *ast.SelectorExpr:
					if id, ok := x.X.(*ast.Ident); ok && id.Name == "store" && x.Sel.Name == "InsertUnassignedParams" && !insert.IsValid() {
						insert = x.Pos()
					}
				}
				return true
			})
			name := rel + ":" + fd.Name.Name
			if insert.IsValid() {
				writers[name] = true
				if !admit.IsValid() || admit > insert {
					t.Errorf("%s writes a tags row without asking AdmitTagUID first", name)
				}
			}
			if rel == "internal/db/resolve.go" && (fd.Name.Name == "GetTagByUID" || fd.Name.Name == "GetTagByUIDOn") && len(fd.Body.List) > 0 {
				if ifs, ok := fd.Body.List[0].(*ast.IfStmt); ok {
					if as, ok := ifs.Init.(*ast.AssignStmt); ok && len(as.Rhs) == 1 {
						if c, ok := as.Rhs[0].(*ast.CallExpr); ok {
							if sel, ok := c.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "AdmitTagUID" {
								resolverAsks[fd.Name.Name] = true
							}
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"internal/encode/rows.go:InsertUnassigned": true, "internal/sandbox/plaque.go:writePlaque": true}
	if len(writers) != len(want) {
		t.Errorf("the writers of a tags row are %v, want %v", writers, want)
	}
	for w := range want {
		if !writers[w] {
			t.Errorf("CONTROL: %s is not seen as a writer (seen: %v)", w, writers)
		}
	}
	for _, f := range []string{"GetTagByUID", "GetTagByUIDOn"} {
		if !resolverAsks[f] {
			t.Errorf("db.DB.%s does not ask AdmitTagUID as its first statement", f)
		}
	}
	n := 0
	queries, err := filepath.Glob(filepath.Join(root, "db", "queries", "*.sql"))
	if err != nil || len(queries) == 0 {
		t.Fatalf("db/queries: %v", err)
	}
	for _, q := range queries {
		b, err := os.ReadFile(q)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			if l := strings.TrimSpace(line); strings.HasPrefix(l, "INSERT INTO tags") {
				n++
			}
		}
	}
	if n != 1 {
		t.Errorf("db/queries has %d INSERT INTO tags statements, want 1 (InsertUnassigned)", n)
	}
}
