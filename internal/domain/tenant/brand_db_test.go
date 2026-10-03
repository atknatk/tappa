package tenant

// brand_db_test.go -- M10 WL-4's four brand writes against REAL Postgres, as tappa_app
// (CLAUDE.md §8). The properties measured here are ones a fake database would agree
// with whatever the code did: that the change and its tenant.brand_updated row share
// one transaction (broken on each side in turn), that the row's "before" is the locked
// stored value when two owners save at once, that the six detail keys are what jsonb
// holds, and which of six actors reach a write.
//
// FIXTURES ARE NOT CLEANED UP: tappa_app holds no DELETE on tenant_branding (00028) and
// audit_log is append-only (00005). Every fixture tenant is a fresh random uuid, and
// `make db-reset` clears the development database. The logos are small generated
// pictures; nothing here is a real tenant's data.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/brand"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/test/fixtures"
)

// brandFixture is one business with two owners, a manager and a disabled owner, plus a
// SECOND business with its own owner to attack it from.
type brandFixture struct {
	data   *db.DB
	trail  *audit.Recorder
	brands *Brands
	gate   *brand.LogoGate

	tenant   uuid.UUID
	owner    uuid.UUID
	owner2   uuid.UUID
	manager  uuid.UUID
	disabled uuid.UUID

	foreign      uuid.UUID
	foreignOwner uuid.UUID
}

func newBrandFixture(t *testing.T) *brandFixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping brand write tests (real Postgres required)")
	}
	data, err := db.New(context.Background(), &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)
	trail, err := audit.New(data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	brands, err := NewBrands(data, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewBrands: %v", err)
	}
	gate, err := brand.NewLogoGate(brand.LogoDecodeSlots)
	if err != nil {
		t.Fatalf("NewLogoGate: %v", err)
	}
	f := &brandFixture{data: data, trail: trail, brands: brands, gate: gate}
	f.tenant = f.business(t)
	f.owner = f.admin(t, f.tenant, "owner", "active")
	f.owner2 = f.admin(t, f.tenant, "owner", "active")
	f.manager = f.admin(t, f.tenant, "manager", "active")
	f.disabled = f.admin(t, f.tenant, "owner", "disabled")
	f.foreign = f.business(t)
	f.foreignOwner = f.admin(t, f.foreign, "owner", "active")
	return f
}

// business writes a tenant row in its own context and returns its id.
func (f *brandFixture) business(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	err := f.data.WithTenant(context.Background(), id, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'brand-domain-fixture', $2, 'bar', 'single')`,
			id, "VAT-"+id.String())
		return e
	})
	if err != nil {
		t.Fatalf("fixture tenant: %v", err)
	}
	return id
}

// admin writes one admin_users row with the given role and status.
func (f *brandFixture) admin(t *testing.T, tenantID uuid.UUID, role, status string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
			 VALUES ($1, $2, 'brand-admin', $3, $4, $5, $6)`,
			id, tenantID, "brand-"+uuid.NewString()+"@iso.example", fixtures.UnusablePasswordHash, role, status)
		return e
	})
	if err != nil {
		t.Fatalf("fixture admin: %v", err)
	}
	return id
}

// with returns a Brands over the given database and trail.
func (f *brandFixture) with(t *testing.T, data Database, trail Trail) *Brands {
	t.Helper()
	b, err := NewBrands(data, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewBrands: %v", err)
	}
	return b
}

// picture is a w×h gradient whose colours depend on seed, so two seeds give two
// different logos.
func picture(w, h int, seed uint8) *image.NRGBA {
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			m.SetNRGBA(x, y, color.NRGBA{R: uint8(3*x) + seed, G: uint8(5*y) ^ seed, B: seed, A: 255})
		}
	}
	return m
}

// pngLogo and jpegLogo run a generated picture through the logo gate.
func (f *brandFixture) pngLogo(t *testing.T, w, h int, seed uint8) brand.Logo {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, picture(w, h, seed)); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return f.normalize(t, buf.Bytes())
}

func (f *brandFixture) jpegLogo(t *testing.T, w, h int, seed uint8) brand.Logo {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, picture(w, h, seed), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	return f.normalize(t, buf.Bytes())
}

func (f *brandFixture) normalize(t *testing.T, data []byte) brand.Logo {
	t.Helper()
	l, err := f.gate.Normalize(context.Background(), bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	return l
}

// mustColour parses a canonical accent.
func mustColour(t *testing.T, s string) brand.Color {
	t.Helper()
	c, err := brand.ParseAccent(s)
	if err != nil {
		t.Fatalf("ParseAccent(%q): %v", s, err)
	}
	return c
}

// brandRow is the tenant_branding row as the database holds it, each column rendered
// as text ("NULL" for NULL), so two snapshots compare with ==.
type brandRow struct {
	exists                                 bool
	accent, sha, mime, width, height, size string
	logoNull                               bool
	updatedBy, updatedAt                   string
}

func nullText[T any](p *T) string {
	if p == nil {
		return "NULL"
	}
	return fmt.Sprint(*p)
}

func (f *brandFixture) row(t *testing.T, tenantID uuid.UUID) brandRow {
	t.Helper()
	var r brandRow
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var accent, sha, mime *string
		var width, height, size *int32
		var by uuid.UUID
		var at time.Time
		e := tx.QueryRow(ctx,
			`SELECT accent, logo_sha256, logo_mime, logo_width, logo_height, octet_length(logo),
			        logo IS NULL, updated_by, updated_at
			   FROM tenant_branding WHERE tenant_id = $1`, tenantID).
			Scan(&accent, &sha, &mime, &width, &height, &size, &r.logoNull, &by, &at)
		if errors.Is(e, pgx.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		r.exists = true
		r.accent, r.sha, r.mime = nullText(accent), nullText(sha), nullText(mime)
		r.width, r.height, r.size = nullText(width), nullText(height), nullText(size)
		// Fixed-width microseconds, so two values order as strings.
		r.updatedBy, r.updatedAt = by.String(), at.UTC().Format("2006-01-02T15:04:05.000000Z")
		return nil
	})
	if err != nil {
		t.Fatalf("read tenant_branding: %v", err)
	}
	return r
}

// trailRow is one tenant.brand_updated row.
type trailRow struct {
	id     uuid.UUID
	actor  string
	target string
	detail string // canonical JSON: keys sorted, no whitespace
}

// canonicalJSON re-renders a JSON object with sorted keys and no whitespace.
func canonicalJSON(t *testing.T, s string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("detail is not a JSON object: %v", err)
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("re-marshal detail: %v", err)
	}
	return string(b)
}

// trailRows returns every tenant.brand_updated row of one tenant, keyed by id. Every
// fixture tenant is a fresh uuid, so the rows in it were written by the test that
// created it. The action is the literal ADR 0024 §6 names, not the Go constant, so a
// renamed constant finds no rows here.
func (f *brandFixture) trailRows(t *testing.T, tenantID uuid.UUID) map[uuid.UUID]trailRow {
	t.Helper()
	out := map[uuid.UUID]trailRow{}
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, e := tx.Query(ctx,
			`SELECT id, actor_id, target, detail::text FROM audit_log
			  WHERE tenant_id = $1 AND action = 'tenant.brand_updated'`, tenantID)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var r trailRow
			var actor *uuid.UUID
			var target *string
			if e := rows.Scan(&r.id, &actor, &target, &r.detail); e != nil {
				return e
			}
			r.actor, r.target = nullText(actor), nullText(target)
			out[r.id] = r
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("read audit_log: %v", err)
	}
	for id, r := range out {
		r.detail = canonicalJSON(t, r.detail)
		out[id] = r
	}
	return out
}

// newTrailRow runs op and returns the one tenant.brand_updated row it added.
func (f *brandFixture) newTrailRow(t *testing.T, tenantID uuid.UUID, op func() error) trailRow {
	t.Helper()
	before := f.trailRows(t, tenantID)
	if err := op(); err != nil {
		t.Fatalf("write: %v", err)
	}
	var added []trailRow
	for id, r := range f.trailRows(t, tenantID) {
		if _, ok := before[id]; !ok {
			added = append(added, r)
		}
	}
	if len(added) != 1 {
		t.Fatalf("the write added %d %s row(s), want exactly 1", len(added), ActionBrandUpdated)
	}
	return added[0]
}

// wantDetail renders the expected detail the way canonicalJSON renders the stored one.
// nil is JSON null.
func wantDetail(t *testing.T, field string, before, after, size, width, height any) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"field": field, "before": before, "after": after,
		"bytes": size, "width": width, "height": height,
	})
	if err != nil {
		t.Fatalf("marshal expected detail: %v", err)
	}
	return canonicalJSON(t, string(b))
}

// countingDB is a Database that counts the transactions it is asked to open and can
// interpose on the transaction the domain is handed: onBegin runs first inside it, and
// exec, when set, answers the domain's Exec calls (the four brand UPDATEs and the
// INSERT; the reads and the trail's INSERT ... RETURNING go through QueryRow). It is a
// test double of the consumer-side interface (CLAUDE.md §7); no product code changes.
type countingDB struct {
	real    Database
	calls   atomic.Int32
	onBegin func(ctx context.Context, tx pgx.Tx) error
	exec    func(ctx context.Context, tx pgx.Tx, sql string, args ...any) (pgconn.CommandTag, error)
}

func (c *countingDB) WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error {
	c.calls.Add(1)
	return c.real.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if c.onBegin != nil {
			if err := c.onBegin(ctx, tx); err != nil {
				return err
			}
		}
		if c.exec == nil {
			return fn(ctx, tx)
		}
		return fn(ctx, &interposedTx{Tx: tx, exec: c.exec})
	})
}

type interposedTx struct {
	pgx.Tx
	exec func(ctx context.Context, tx pgx.Tx, sql string, args ...any) (pgconn.CommandTag, error)
}

func (i *interposedTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return i.exec(ctx, i.Tx, sql, args...)
}

// isBrandUpdate reports whether sql is one of the four brand UPDATE statements.
func isBrandUpdate(sql string) bool {
	return strings.Contains(sql, "UPDATE tenant_branding")
}

// fourWrites are the four operations, each as a function of the Brands that runs it,
// the tenant and the actor. The accent and the logos are fixed per call of fourWrites.
type brandOp struct {
	name string
	run  func(b *Brands, tenantID, actorID uuid.UUID) error
}

func (f *brandFixture) fourWrites(t *testing.T) []brandOp {
	t.Helper()
	ops, _ := f.fourWritesWithLogo(t)
	return ops
}

// fourWritesWithLogo is fourWrites, and the logo its SaveLogo stores: SaveAccent
// (DA291C), ClearAccent, SaveLogo (a 64×32 PNG), ClearLogo, in that order.
func (f *brandFixture) fourWritesWithLogo(t *testing.T) ([]brandOp, brand.Logo) {
	t.Helper()
	accent := mustColour(t, "DA291C")
	logo := f.pngLogo(t, 64, 32, 7)
	ctx := context.Background()
	return []brandOp{
		{"SaveAccent", func(b *Brands, tn, a uuid.UUID) error {
			return b.SaveAccent(ctx, BrandAccentCommand{TenantID: tn, ActorID: a, Accent: accent})
		}},
		{"ClearAccent", func(b *Brands, tn, a uuid.UUID) error {
			return b.ClearAccent(ctx, BrandClearCommand{TenantID: tn, ActorID: a})
		}},
		{"SaveLogo", func(b *Brands, tn, a uuid.UUID) error {
			return b.SaveLogo(ctx, BrandLogoCommand{TenantID: tn, ActorID: a, Logo: logo})
		}},
		{"ClearLogo", func(b *Brands, tn, a uuid.UUID) error {
			return b.ClearLogo(ctx, BrandClearCommand{TenantID: tn, ActorID: a})
		}},
	}, logo
}

// seedBrand gives a tenant a committed accent and logo, through the product's own path.
func (f *brandFixture) seedBrand(t *testing.T, tenantID, actorID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	if err := f.brands.SaveAccent(ctx, BrandAccentCommand{TenantID: tenantID, ActorID: actorID, Accent: mustColour(t, "BE3D2A")}); err != nil {
		t.Fatalf("seed accent: %v", err)
	}
	if err := f.brands.SaveLogo(ctx, BrandLogoCommand{TenantID: tenantID, ActorID: actorID, Logo: f.jpegLogo(t, 40, 40, 99)}); err != nil {
		t.Fatalf("seed logo: %v", err)
	}
}

// =====================================================================================
// What each write stores, and the one row it adds to the trail.
// =====================================================================================

func TestBrandDB_EachWriteStoresItsValueAndOneTrailRow(t *testing.T) {
	f := newBrandFixture(t)
	ctx := context.Background()
	tenant := f.tenant

	// The colour input's spelling, normalised by the handler; stored canonically.
	accent, err := brand.NormalizeAccent("#da291c")
	if err != nil {
		t.Fatalf("NormalizeAccent: %v", err)
	}
	r := f.newTrailRow(t, tenant, func() error {
		return f.brands.SaveAccent(ctx, BrandAccentCommand{TenantID: tenant, ActorID: f.owner, Accent: accent})
	})
	row := f.row(t, tenant)
	if row.accent != "DA291C" || row.updatedBy != f.owner.String() || !row.logoNull {
		t.Errorf("after SaveAccent the row is %+v, want accent DA291C, updated_by the owner, no logo", row)
	}
	if want := wantDetail(t, "accent", nil, "DA291C", nil, nil, nil); r.detail != want {
		t.Errorf("SaveAccent detail = %s, want %s", r.detail, want)
	}
	if r.actor != f.owner.String() || r.target != tenant.String() {
		t.Errorf("SaveAccent row actor=%s target=%s, want actor %s and target the tenant %s", r.actor, r.target, f.owner, tenant)
	}

	png1 := f.pngLogo(t, 64, 32, 1)
	r = f.newTrailRow(t, tenant, func() error {
		return f.brands.SaveLogo(ctx, BrandLogoCommand{TenantID: tenant, ActorID: f.owner2, Logo: png1})
	})
	row = f.row(t, tenant)
	if row.accent != "DA291C" || row.sha != png1.SHA256 || row.mime != "image/png" ||
		row.width != "64" || row.height != "32" || row.size != fmt.Sprint(len(png1.Data)) ||
		row.updatedBy != f.owner2.String() {
		t.Errorf("after SaveLogo the row is %+v, want the accent kept, the PNG's five values and updated_by the second owner", row)
	}
	if want := wantDetail(t, "logo", nil, png1.SHA256, len(png1.Data), 64, 32); r.detail != want {
		t.Errorf("SaveLogo detail = %s, want %s", r.detail, want)
	}
	if r.actor != f.owner2.String() {
		t.Errorf("SaveLogo row actor = %s, want the second owner %s", r.actor, f.owner2)
	}

	jpg := f.jpegLogo(t, 40, 24, 2)
	r = f.newTrailRow(t, tenant, func() error {
		return f.brands.SaveLogo(ctx, BrandLogoCommand{TenantID: tenant, ActorID: f.owner, Logo: jpg})
	})
	row = f.row(t, tenant)
	if row.sha != jpg.SHA256 || row.mime != "image/jpeg" || row.width != "40" || row.height != "24" ||
		row.size != fmt.Sprint(len(jpg.Data)) || row.updatedBy != f.owner.String() {
		t.Errorf("after the second SaveLogo the row is %+v, want the JPEG's five values", row)
	}
	if want := wantDetail(t, "logo", png1.SHA256, jpg.SHA256, len(jpg.Data), 40, 24); r.detail != want {
		t.Errorf("second SaveLogo detail = %s, want %s", r.detail, want)
	}

	r = f.newTrailRow(t, tenant, func() error {
		return f.brands.ClearAccent(ctx, BrandClearCommand{TenantID: tenant, ActorID: f.owner2})
	})
	row = f.row(t, tenant)
	if row.accent != "NULL" || row.sha != jpg.SHA256 || row.updatedBy != f.owner2.String() {
		t.Errorf("after ClearAccent the row is %+v, want accent NULL and the logo kept", row)
	}
	if want := wantDetail(t, "accent", "DA291C", nil, nil, nil, nil); r.detail != want {
		t.Errorf("ClearAccent detail = %s, want %s", r.detail, want)
	}

	r = f.newTrailRow(t, tenant, func() error {
		return f.brands.ClearLogo(ctx, BrandClearCommand{TenantID: tenant, ActorID: f.owner})
	})
	row = f.row(t, tenant)
	if !row.exists {
		t.Fatal("ClearLogo removed the row; a clear is an UPDATE")
	}
	// The five logo columns, each read on its own.
	if !row.logoNull || row.size != "NULL" || row.sha != "NULL" || row.mime != "NULL" ||
		row.width != "NULL" || row.height != "NULL" || row.updatedBy != f.owner.String() {
		t.Errorf("after ClearLogo the row is %+v, want all five logo columns NULL", row)
	}
	if want := wantDetail(t, "logo", jpg.SHA256, nil, nil, nil, nil); r.detail != want {
		t.Errorf("ClearLogo detail = %s, want %s", r.detail, want)
	}
}

// =====================================================================================
// The detail holds exactly ADR 0024 §6's six keys, on the row each of the four writes adds.
// =====================================================================================

func TestBrandDB_TheDetailHasExactlyTheSixKeys(t *testing.T) {
	f := newBrandFixture(t)
	want := []string{"after", "before", "bytes", "field", "height", "width"}
	for _, op := range f.fourWrites(t) {
		t.Run(op.name, func(t *testing.T) {
			r := f.newTrailRow(t, f.tenant, func() error { return op.run(f.brands, f.tenant, f.owner) })
			var m map[string]json.RawMessage
			if err := json.Unmarshal([]byte(r.detail), &m); err != nil {
				t.Fatalf("detail: %v", err)
			}
			got := make([]string, 0, len(m))
			for k := range m {
				got = append(got, k)
			}
			sort.Strings(got)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("detail keys = %v, want exactly %v (detail %s)", got, want, r.detail)
			}
		})
	}
}

// =====================================================================================
// The change and its trail row share one transaction -- broken from the trail's side.
// =====================================================================================

// writeThenFailTrail writes the real row through the transaction it is handed, then
// fails: the row it wrote must not survive.
type writeThenFailTrail struct {
	real  *audit.Recorder
	wrote *[]uuid.UUID
	mu    *sync.Mutex
}

func (w writeThenFailTrail) RecordTx(ctx context.Context, tx pgx.Tx, e audit.Event) (uuid.UUID, error) {
	id, err := w.real.RecordTx(ctx, tx, e)
	if err != nil {
		return uuid.Nil, err
	}
	w.mu.Lock()
	*w.wrote = append(*w.wrote, id)
	w.mu.Unlock()
	return uuid.Nil, errors.New("trail failed after writing its row")
}

// spyTrail reads the brand row through the transaction it is handed before writing the
// real row, so the test can see whether that transaction already holds the change.
type spyTrail struct {
	real *audit.Recorder
	mu   *sync.Mutex
	seen *[]string
}

func (s spyTrail) RecordTx(ctx context.Context, tx pgx.Tx, e audit.Event) (uuid.UUID, error) {
	var accent, sha *string
	if err := tx.QueryRow(ctx,
		`SELECT accent, logo_sha256 FROM tenant_branding WHERE tenant_id = $1`, e.TenantID).
		Scan(&accent, &sha); err != nil {
		return uuid.Nil, err
	}
	s.mu.Lock()
	*s.seen = append(*s.seen, nullText(accent)+"|"+nullText(sha))
	s.mu.Unlock()
	return s.real.RecordTx(ctx, tx, e)
}

func TestBrandDB_TheChangeAndItsTrailRowShareOneTransaction(t *testing.T) {
	f := newBrandFixture(t)

	t.Run("a trail that fails", func(t *testing.T) {
		broken := f.with(t, f.data, brokenTrail{err: errors.New("audit is down")})
		for _, seeded := range []bool{false, true} {
			tenant := f.business(t)
			owner := f.admin(t, tenant, "owner", "active")
			if seeded {
				f.seedBrand(t, tenant, owner)
			}
			for _, op := range f.fourWrites(t) {
				beforeRow, beforeTrail := f.row(t, tenant), len(f.trailRows(t, tenant))
				if err := op.run(broken, tenant, owner); err == nil {
					t.Errorf("%s (seeded=%v): reported success although its trail row could not be written", op.name, seeded)
				}
				if got := f.row(t, tenant); got != beforeRow {
					t.Errorf("%s (seeded=%v): the row changed to %+v although the trail failed; want %+v", op.name, seeded, got, beforeRow)
				}
				if n := len(f.trailRows(t, tenant)); n != beforeTrail {
					t.Errorf("%s (seeded=%v): the trail went from %d to %d rows", op.name, seeded, beforeTrail, n)
				}
			}
			if !seeded && f.row(t, tenant).exists {
				t.Error("an unseeded tenant has a brand row after four failed writes; the Ensure insert outlived its transaction")
			}
		}
	})

	t.Run("a trail that writes its row and then fails", func(t *testing.T) {
		var wrote []uuid.UUID
		failing := f.with(t, f.data, writeThenFailTrail{real: f.trail, wrote: &wrote, mu: &sync.Mutex{}})
		tenant := f.business(t)
		owner := f.admin(t, tenant, "owner", "active")
		f.seedBrand(t, tenant, owner)
		for _, op := range f.fourWrites(t) {
			beforeRow := f.row(t, tenant)
			if err := op.run(failing, tenant, owner); err == nil {
				t.Errorf("%s: reported success although its trail failed", op.name)
			}
			if got := f.row(t, tenant); got != beforeRow {
				t.Errorf("%s: the row changed to %+v; want %+v", op.name, got, beforeRow)
			}
		}
		if len(wrote) != 4 {
			t.Fatalf("the trail wrote %d rows before failing, want 4 (one per write)", len(wrote))
		}
		rows := f.trailRows(t, tenant)
		for _, id := range wrote {
			if _, ok := rows[id]; ok {
				t.Errorf("trail row %s survived its transaction's rollback", id)
			}
		}
	})

	t.Run("the trail is handed the transaction that holds the change", func(t *testing.T) {
		var seen []string
		spied := f.with(t, f.data, spyTrail{real: f.trail, mu: &sync.Mutex{}, seen: &seen})
		tenant := f.business(t)
		owner := f.admin(t, tenant, "owner", "active")
		f.seedBrand(t, tenant, owner)
		seeded := f.row(t, tenant).sha
		ops, logo := f.fourWritesWithLogo(t)
		for _, op := range ops {
			if err := op.run(spied, tenant, owner); err != nil {
				t.Fatalf("%s: %v", op.name, err)
			}
		}
		// What the trail's transaction saw at each of the four writes is the row after
		// that write: the seeded logo kept through the two accent writes, then the PNG,
		// then nothing.
		want := []string{"DA291C|" + seeded, "NULL|" + seeded, "NULL|" + logo.SHA256, "NULL|NULL"}
		if strings.Join(seen, " ") != strings.Join(want, " ") {
			t.Errorf("the trail's transaction saw %v, want %v", seen, want)
		}
	})
}

// =====================================================================================
// The change and its trail row share one transaction -- broken from the write's side.
// =====================================================================================

func TestBrandDB_AFailedWriteLeavesNoTrailRow(t *testing.T) {
	f := newBrandFixture(t)

	cases := []struct {
		name string
		exec func(ctx context.Context, tx pgx.Tx, sql string, args ...any) (pgconn.CommandTag, error)
		want string
	}{
		{
			// The statement is not sent: the transaction stays usable, so a domain that
			// ignored the error could still write its trail row and commit.
			name: "the statement fails",
			exec: func(ctx context.Context, tx pgx.Tx, sql string, args ...any) (pgconn.CommandTag, error) {
				if isBrandUpdate(sql) {
					return pgconn.CommandTag{}, errors.New("injected statement failure")
				}
				return tx.Exec(ctx, sql, args...)
			},
			want: "injected statement failure",
		},
		{
			// The statement is sent with one more conjunct, so Postgres itself reports
			// that it changed no row.
			name: "the statement changes no row",
			exec: func(ctx context.Context, tx pgx.Tx, sql string, args ...any) (pgconn.CommandTag, error) {
				if isBrandUpdate(sql) {
					return tx.Exec(ctx, strings.TrimRight(sql, "\n ;")+" AND false", args...)
				}
				return tx.Exec(ctx, sql, args...)
			},
			want: "0 rows changed, want 1",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tenant := f.business(t)
			owner := f.admin(t, tenant, "owner", "active")
			f.seedBrand(t, tenant, owner)
			faulty := f.with(t, &countingDB{real: f.data, exec: c.exec}, f.trail)
			for _, op := range f.fourWrites(t) {
				beforeRow, beforeTrail := f.row(t, tenant), len(f.trailRows(t, tenant))
				err := op.run(faulty, tenant, owner)
				if err == nil || !strings.Contains(err.Error(), c.want) {
					t.Errorf("%s: err = %v, want one saying %q", op.name, err, c.want)
				}
				if got := f.row(t, tenant); got != beforeRow {
					t.Errorf("%s: the row changed to %+v; want %+v", op.name, got, beforeRow)
				}
				if n := len(f.trailRows(t, tenant)); n != beforeTrail {
					t.Errorf("%s: the trail went from %d to %d rows although the write failed", op.name, beforeTrail, n)
				}
			}
		})
	}
}

// =====================================================================================
// Refusals decided before a transaction is opened: no transaction, no change.
// =====================================================================================

// exifJPEG is a JPEG with an APP1 Exif segment carrying the bytes "GPS" spliced in after
// SOI: the shape of a phone photo's upload, which Normalize re-encodes without it.
func exifJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, picture(40, 40, 5), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	payload := append([]byte("Exif\x00\x00"), []byte("GPS-fixture-not-a-place")...)
	seg := []byte{0xFF, 0xE1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}
	raw := append([]byte{}, buf.Bytes()[:2]...)
	raw = append(raw, seg...)
	raw = append(raw, payload...)
	return append(raw, buf.Bytes()[2:]...)
}

func TestBrandDB_RefusalsBeforeTheDatabaseWriteNothing(t *testing.T) {
	f := newBrandFixture(t)
	ctx := context.Background()
	f.seedBrand(t, f.tenant, f.owner)

	type attempt struct {
		name  string
		run   func(b *Brands) error
		check func(error) bool
	}
	isIllegible := func(err error) bool { return errors.Is(err, brand.ErrAccentIllegible) }
	isNotNormalized := func(err error) bool { return errors.Is(err, ErrBrandLogoNotNormalized) }
	isActorRequired := func(err error) bool { return err != nil && strings.Contains(err.Error(), "actor id is required") }
	isTenantRequired := func(err error) bool { return err != nil && strings.Contains(err.Error(), "tenant id is required") }

	good := f.pngLogo(t, 64, 32, 11)
	raw := exifJPEG(t)
	rawSum := sha256.Sum256(raw)
	changed := good
	changed.Width = 63

	attempts := []attempt{
		// ADR 0023 §3's two refused examples.
		{"accent 808080", func(b *Brands) error {
			return b.SaveAccent(ctx, BrandAccentCommand{TenantID: f.tenant, ActorID: f.owner, Accent: mustColour(t, "808080")})
		}, isIllegible},
		{"accent E0457B", func(b *Brands) error {
			return b.SaveAccent(ctx, BrandAccentCommand{TenantID: f.tenant, ActorID: f.owner, Accent: mustColour(t, "E0457B")})
		}, isIllegible},
		{"logo: a literal with Normalize's five values", func(b *Brands) error {
			lit := brand.Logo{Data: good.Data, MIME: good.MIME, Width: good.Width, Height: good.Height, SHA256: good.SHA256}
			return b.SaveLogo(ctx, BrandLogoCommand{TenantID: f.tenant, ActorID: f.owner, Logo: lit})
		}, isNotNormalized},
		{"logo: the upload's own bytes with a matching digest", func(b *Brands) error {
			lit := brand.Logo{Data: raw, MIME: brand.LogoMIMEJPEG, Width: 40, Height: 40, SHA256: hex.EncodeToString(rawSum[:])}
			return b.SaveLogo(ctx, BrandLogoCommand{TenantID: f.tenant, ActorID: f.owner, Logo: lit})
		}, isNotNormalized},
		{"logo: Normalize's output with a field changed", func(b *Brands) error {
			return b.SaveLogo(ctx, BrandLogoCommand{TenantID: f.tenant, ActorID: f.owner, Logo: changed})
		}, isNotNormalized},
		{"logo: the zero Logo", func(b *Brands) error {
			return b.SaveLogo(ctx, BrandLogoCommand{TenantID: f.tenant, ActorID: f.owner})
		}, isNotNormalized},
	}
	for _, op := range f.fourWrites(t) {
		attempts = append(attempts,
			attempt{op.name + " with a nil actor", func(b *Brands) error { return op.run(b, f.tenant, uuid.Nil) }, isActorRequired},
			attempt{op.name + " with a nil tenant", func(b *Brands) error { return op.run(b, uuid.Nil, f.owner) }, isTenantRequired},
		)
	}

	for _, a := range attempts {
		t.Run(a.name, func(t *testing.T) {
			counted := &countingDB{real: f.data}
			b := f.with(t, counted, f.trail)
			beforeRow, beforeTrail := f.row(t, f.tenant), len(f.trailRows(t, f.tenant))
			err := a.run(b)
			if !a.check(err) {
				t.Errorf("err = %v, not the refusal this attempt expects", err)
			}
			if n := counted.calls.Load(); n != 0 {
				t.Errorf("%d transaction(s) opened; a refusal decided before the database opens none", n)
			}
			if got := f.row(t, f.tenant); got != beforeRow {
				t.Errorf("the row changed to %+v; want %+v", got, beforeRow)
			}
			if n := len(f.trailRows(t, f.tenant)); n != beforeTrail {
				t.Errorf("the trail went from %d to %d rows", beforeTrail, n)
			}
		})
	}

	// Positive control: Normalize's own output passes the same gate and is written.
	t.Run("positive control", func(t *testing.T) {
		counted := &countingDB{real: f.data}
		b := f.with(t, counted, f.trail)
		if err := b.SaveLogo(ctx, BrandLogoCommand{TenantID: f.tenant, ActorID: f.owner, Logo: good}); err != nil {
			t.Fatalf("SaveLogo of Normalize's output: %v", err)
		}
		if err := b.SaveAccent(ctx, BrandAccentCommand{TenantID: f.tenant, ActorID: f.owner, Accent: mustColour(t, "1F5C41")}); err != nil {
			t.Fatalf("SaveAccent of tappa-green: %v", err)
		}
		if n := counted.calls.Load(); n != 2 {
			t.Errorf("%d transaction(s) opened, want 2", n)
		}
		if row := f.row(t, f.tenant); row.sha != good.SHA256 || row.accent != "1F5C41" {
			t.Errorf("row = %+v, want the positive control's logo and accent", row)
		}
	})
}

// =====================================================================================
// Which actors reach a write: this tenant's active owners, and none of the five others.
// =====================================================================================

// The refusal is the sentinel itself (==, not errors.Is) and its text is one string for
// the five actors, so the answer does not say which of them asked. The round on two
// tenants WITHOUT a brand row pins where the gate runs: before EnsureTenantBrand, whose
// INSERT would otherwise meet the composite foreign key first for an id that is not an
// admin of the tenant; that answer is SQLSTATE 23503, a different one from the
// manager's, and the INSERT would create the row for the others.
func TestBrandDB_OnlyAnActiveOwnerOfThisTenantMayWrite(t *testing.T) {
	f := newBrandFixture(t)

	for _, seeded := range []bool{true, false} {
		name := "tenants with a brand row"
		if !seeded {
			name = "tenants without a brand row"
		}
		t.Run(name, func(t *testing.T) {
			a := f.business(t)
			owner := f.admin(t, a, "owner", "active")
			owner2 := f.admin(t, a, "owner", "active")
			manager := f.admin(t, a, "manager", "active")
			disabled := f.admin(t, a, "owner", "disabled")
			b := f.business(t)
			bOwner := f.admin(t, b, "owner", "active")
			if seeded {
				f.seedBrand(t, a, owner)
				f.seedBrand(t, b, bOwner)
			}

			refused := []struct {
				name   string
				tenant uuid.UUID
				actor  uuid.UUID
			}{
				{"a manager of this tenant", a, manager},
				{"a disabled owner of this tenant", a, disabled},
				{"the other tenant's owner, on this tenant", a, bOwner},
				{"this tenant's owner, on the other tenant", b, owner},
				{"an id that is no admin", a, uuid.New()},
			}
			texts := map[string][]string{}
			for _, r := range refused {
				for _, op := range f.fourWrites(t) {
					t.Run(r.name+"/"+op.name, func(t *testing.T) {
						rowA, rowB := f.row(t, a), f.row(t, b)
						trailA, trailB := len(f.trailRows(t, a)), len(f.trailRows(t, b))
						err := op.run(f.brands, r.tenant, r.actor)
						if err != ErrBrandNotPermitted {
							t.Errorf("err = %v, want ErrBrandNotPermitted itself", err)
						}
						if err != nil {
							texts[err.Error()] = append(texts[err.Error()], r.name+"/"+op.name)
						}
						if got := f.row(t, a); got != rowA {
							t.Errorf("this tenant's row changed from %+v to %+v", rowA, got)
						}
						if got := f.row(t, b); got != rowB {
							t.Errorf("the other tenant's row changed from %+v to %+v", rowB, got)
						}
						if !seeded && (f.row(t, a).exists || f.row(t, b).exists) {
							t.Error("a refused write created a brand row")
						}
						if x, y := len(f.trailRows(t, a)), len(f.trailRows(t, b)); x != trailA || y != trailB {
							t.Errorf("trail rows went from (%d, %d) to (%d, %d)", trailA, trailB, x, y)
						}
					})
				}
			}
			if len(texts) != 1 {
				t.Errorf("the refusals carry %d different texts, want 1: %v", len(texts), texts)
			}

			// Positive control: each tenant's own active owners write.
			for _, ok := range []struct {
				tenant, actor uuid.UUID
			}{{a, owner}, {a, owner2}, {b, bOwner}} {
				for _, op := range f.fourWrites(t) {
					if err := op.run(f.brands, ok.tenant, ok.actor); err != nil {
						t.Errorf("positive control %s by %s: %v", op.name, ok.actor, err)
					}
				}
			}
		})
	}
}

// =====================================================================================
// Clearing a business that has no brand row: the row is created and the act recorded.
// =====================================================================================

func TestBrandDB_ClearingWithoutABrandRowCreatesTheRowAndRecordsIt(t *testing.T) {
	f := newBrandFixture(t)
	ctx := context.Background()
	clears := []struct {
		name  string
		field string
		run   func(tenantID, actorID uuid.UUID) error
	}{
		{"ClearAccent", "accent", func(tn, a uuid.UUID) error {
			return f.brands.ClearAccent(ctx, BrandClearCommand{TenantID: tn, ActorID: a})
		}},
		{"ClearLogo", "logo", func(tn, a uuid.UUID) error {
			return f.brands.ClearLogo(ctx, BrandClearCommand{TenantID: tn, ActorID: a})
		}},
	}
	for _, c := range clears {
		t.Run(c.name, func(t *testing.T) {
			tenant := f.business(t)
			owner := f.admin(t, tenant, "owner", "active")
			if f.row(t, tenant).exists {
				t.Fatal("a fresh tenant already has a brand row")
			}
			r := f.newTrailRow(t, tenant, func() error { return c.run(tenant, owner) })
			row := f.row(t, tenant)
			if !row.exists || row.accent != "NULL" || !row.logoNull || row.sha != "NULL" ||
				row.mime != "NULL" || row.width != "NULL" || row.height != "NULL" ||
				row.updatedBy != owner.String() {
				t.Errorf("after %s the row is %+v, want an existing row with every brand field NULL and updated_by the owner", c.name, row)
			}
			if want := wantDetail(t, c.field, nil, nil, nil, nil, nil); r.detail != want {
				t.Errorf("%s detail = %s, want %s", c.name, r.detail, want)
			}
		})
	}
}

// =====================================================================================
// The trail's vocabulary is ADR 0024 §6's, spelled out.
// =====================================================================================

func TestBrandTrail_TheActionAndFieldNamesAreTheADRs(t *testing.T) {
	if ActionBrandUpdated != "tenant.brand_updated" {
		t.Errorf("ActionBrandUpdated = %q, want %q (ADR 0024 §6)", ActionBrandUpdated, "tenant.brand_updated")
	}
	if BrandFieldAccent != "accent" || BrandFieldLogo != "logo" {
		t.Errorf("field values = %q, %q, want accent, logo", BrandFieldAccent, BrandFieldLogo)
	}
}

// =====================================================================================
// Two owners save at once: the second records the first's committed value as "before".
// =====================================================================================

// pausePoint holds the first writer inside its transaction until released.
type pausePoint struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newPausePoint() *pausePoint {
	return &pausePoint{entered: make(chan struct{}), release: make(chan struct{})}
}

func (p *pausePoint) hold(ctx context.Context) error {
	p.once.Do(func() { close(p.entered) })
	select {
	case <-p.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// pausingTrail holds the first writer after its UPDATE, before its commit.
type pausingTrail struct {
	real *audit.Recorder
	at   *pausePoint
}

func (p pausingTrail) RecordTx(ctx context.Context, tx pgx.Tx, e audit.Event) (uuid.UUID, error) {
	if err := p.at.hold(ctx); err != nil {
		return uuid.Nil, err
	}
	return p.real.RecordTx(ctx, tx, e)
}

// TestBrandDB_TheSecondOfTwoConcurrentSavesRecordsTheFirstAsItsBefore holds writer 1
// at one of two points of its transaction -- before its UPDATE (it holds the row lock
// from GetTenantBrandForUpdate, and on a new row its uncommitted insert) or after it
// (its new row version is uncommitted) -- starts writer 2, reads from pg_stat_activity
// the statement writer 2 waits in, then releases writer 1. The statement each case
// waits in is part of what is asserted: it says which step serialises the two writers
// in that case.
func TestBrandDB_TheSecondOfTwoConcurrentSavesRecordsTheFirstAsItsBefore(t *testing.T) {
	f := newBrandFixture(t)

	cases := []struct {
		name          string
		existing      bool
		pauseAtUpdate bool
		waitsIn       string
	}{
		{"new row, writer 1 held before its UPDATE", false, true, "EnsureTenantBrand"},
		{"new row, writer 1 held after its UPDATE", false, false, "EnsureTenantBrand"},
		{"existing row, writer 1 held before its UPDATE", true, true, "GetTenantBrandForUpdate"},
		{"existing row, writer 1 held after its UPDATE", true, false, "EnsureTenantBrand"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			tenant := f.business(t)
			first := f.admin(t, tenant, "owner", "active")
			second := f.admin(t, tenant, "owner", "active")
			var firstBefore any
			if c.existing {
				if err := f.brands.SaveAccent(ctx, BrandAccentCommand{TenantID: tenant, ActorID: first, Accent: mustColour(t, "BE3D2A")}); err != nil {
					t.Fatalf("seed: %v", err)
				}
				firstBefore = "BE3D2A"
			}

			pause := newPausePoint()
			w1 := f.with(t, f.data, pausingTrail{real: f.trail, at: pause})
			if c.pauseAtUpdate {
				w1 = f.with(t, &countingDB{real: f.data, exec: func(ctx context.Context, tx pgx.Tx, sql string, args ...any) (pgconn.CommandTag, error) {
					if isBrandUpdate(sql) {
						if err := pause.hold(ctx); err != nil {
							return pgconn.CommandTag{}, err
						}
					}
					return tx.Exec(ctx, sql, args...)
				}}, f.trail)
			}
			var pid atomic.Int32
			w2 := f.with(t, &countingDB{real: f.data, onBegin: func(ctx context.Context, tx pgx.Tx) error {
				var p int32
				if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&p); err != nil {
					return err
				}
				pid.Store(p)
				return nil
			}}, f.trail)

			done1, done2 := make(chan error, 1), make(chan error, 1)
			go func() {
				done1 <- w1.SaveAccent(ctx, BrandAccentCommand{TenantID: tenant, ActorID: first, Accent: mustColour(t, "1F5C41")})
			}()
			select {
			case <-pause.entered:
			case err := <-done1:
				t.Fatalf("writer 1 finished before reaching its pause point: %v", err)
			case <-ctx.Done():
				t.Fatal("writer 1 did not reach its pause point")
			}
			go func() {
				done2 <- w2.SaveAccent(ctx, BrandAccentCommand{TenantID: tenant, ActorID: second, Accent: mustColour(t, "DA291C")})
			}()

			// Writer 2 must be waiting on a lock before writer 1 commits.
			deadline := time.Now().Add(15 * time.Second)
			waitedIn := ""
			for waitedIn == "" && time.Now().Before(deadline) {
				select {
				case err := <-done2:
					close(pause.release)
					t.Fatalf("writer 2 finished without waiting on writer 1 (err %v)", err)
				default:
				}
				if p := pid.Load(); p != 0 {
					err := f.data.WithTenant(ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
						var wait *string
						var query string
						if e := tx.QueryRow(ctx, `SELECT wait_event_type, query FROM pg_stat_activity WHERE pid = $1`, p).Scan(&wait, &query); e != nil {
							return e
						}
						if wait != nil && *wait == "Lock" {
							waitedIn = strings.SplitN(query, "\n", 2)[0]
						}
						return nil
					})
					if err != nil {
						close(pause.release)
						t.Fatalf("read pg_stat_activity: %v", err)
					}
				}
				if waitedIn == "" {
					time.Sleep(10 * time.Millisecond)
				}
			}
			close(pause.release)
			if err := <-done1; err != nil {
				t.Fatalf("writer 1: %v", err)
			}
			if err := <-done2; err != nil {
				t.Fatalf("writer 2: %v", err)
			}
			if waitedIn == "" {
				t.Fatal("writer 2 never waited on a lock within 15 s")
			}
			if !strings.HasPrefix(waitedIn, "-- name: "+c.waitsIn+" ") {
				t.Errorf("writer 2 waited in %q, want %s", waitedIn, c.waitsIn)
			}

			var r1, r2 *trailRow
			for _, r := range f.trailRows(t, tenant) {
				switch r.actor {
				case first.String():
					if strings.Contains(r.detail, `"after":"1F5C41"`) {
						r1 = &r
					}
				case second.String():
					r2 = &r
				}
			}
			if r1 == nil || r2 == nil {
				t.Fatalf("trail rows: writer 1 %v, writer 2 %v; want one each", r1, r2)
			}
			if want := wantDetail(t, "accent", firstBefore, "1F5C41", nil, nil, nil); r1.detail != want {
				t.Errorf("writer 1's detail = %s, want %s", r1.detail, want)
			}
			if want := wantDetail(t, "accent", "1F5C41", "DA291C", nil, nil, nil); r2.detail != want {
				t.Errorf("writer 2's detail = %s, want %s (its before is writer 1's committed after)", r2.detail, want)
			}
			if row := f.row(t, tenant); row.accent != "DA291C" || row.updatedBy != second.String() {
				t.Errorf("final row %+v, want writer 2's accent and actor", row)
			}
		})
	}
}

// =====================================================================================
// Saving the value already stored is an act: the UPDATE runs and a row records it.
// =====================================================================================

func TestBrandDB_SavingTheSameAccentAgainIsRecorded(t *testing.T) {
	f := newBrandFixture(t)
	ctx := context.Background()
	green := mustColour(t, "1F5C41")

	f.newTrailRow(t, f.tenant, func() error {
		return f.brands.SaveAccent(ctx, BrandAccentCommand{TenantID: f.tenant, ActorID: f.owner, Accent: green})
	})
	first := f.row(t, f.tenant)
	r := f.newTrailRow(t, f.tenant, func() error {
		return f.brands.SaveAccent(ctx, BrandAccentCommand{TenantID: f.tenant, ActorID: f.owner2, Accent: green})
	})
	second := f.row(t, f.tenant)
	if want := wantDetail(t, "accent", "1F5C41", "1F5C41", nil, nil, nil); r.detail != want {
		t.Errorf("the repeated save's detail = %s, want %s", r.detail, want)
	}
	if second.accent != "1F5C41" || second.updatedBy != f.owner2.String() {
		t.Errorf("after the repeated save the row is %+v, want the same accent and updated_by the second owner", second)
	}
	if second.updatedAt <= first.updatedAt {
		t.Errorf("updated_at did not move (%s -> %s); the repeated save did not UPDATE", first.updatedAt, second.updatedAt)
	}
	if n := len(f.trailRows(t, f.tenant)); n != 2 {
		t.Errorf("the trail holds %d rows, want 2", n)
	}
}

// =====================================================================================
// Construction.
// =====================================================================================

func TestNewBrands_RefusesAMissingDependency(t *testing.T) {
	if _, err := NewBrands(nil, brokenTrail{}, nil); err == nil {
		t.Error("NewBrands accepted a nil database")
	}
	if _, err := NewBrands(&countingDB{}, nil, nil); err == nil {
		t.Error("NewBrands accepted a nil trail")
	}
	if b, err := NewBrands(&countingDB{}, brokenTrail{}, nil); err != nil || b.log == nil {
		t.Errorf("NewBrands with a nil logger: %v, log set = %v", err, b != nil && b.log != nil)
	}
}
