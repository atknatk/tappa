package db

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/test/fixtures"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// branding_test.go -- migration 00028 (tenant_branding) and db/queries/branding.sql,
// M10 WL-1 (ADR 0023 §1, ADR 0024 §4). Real Postgres, tappa_app (CLAUDE.md §8).
//
// WHY tenant_branding IS NOT IN rls_test.go's TABLE-DRIVEN LISTS: the table holds at
// most one row per tenant (tenant_id is the primary key), so the write list's "own"
// positive control -- a second insert for B -- would collide with B's fixture row on
// the PK and fail for a reason that is not RLS. That is the reason `tenants` has its
// own test there; this file is the same arrangement for this table.
//
// ISOLATION PROBES CARRY NO WHERE (CLAUDE.md §6): a 0 that comes from a tenant
// predicate would stay 0 with RLS off. Each isolation negative below is paired with
// the same probe in the owning tenant's context, which must see the row.
//
// FIXTURES ARE NOT CLEANED UP (rls_test.go's reasoning): tappa_app holds no DELETE
// here, brandTenant mints a fresh random uuid per tenant, and `make db-reset` clears the
// dev DB. The logo bytes are random bytes -- the table's CHECKs measure and hash them
// and do not decode them -- and the fixtures carry no real tenant's or plaque's data.

// brandTenant creates a committed tenant and one owner admin_users row for it, as
// tappa_app inside the tenant's own context.
func brandTenant(t *testing.T, d *DB) (tenantID, adminID uuid.UUID) {
	t.Helper()
	tenantID, adminID = uuid.New(), uuid.New()
	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'brand-fixture', $2, 'bar', 'single')`,
			tenantID, "VAT-"+tenantID.String()); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role)
			 VALUES ($1, $2, 'brand-admin', $3, $4, 'owner')`,
			adminID, tenantID, "brand-"+uuid.NewString()+"@iso.example", fixtures.UnusablePasswordHash)
		return e
	})
	if err != nil {
		t.Fatalf("brandTenant: %v", err)
	}
	return tenantID, adminID
}

type brandLogo struct {
	data []byte
	sha  string
}

// randLogo returns n random bytes and their lower-case hex sha256 -- the shape
// internal/brand.Logo carries (Data, SHA256). They are not an image; the table's CHECKs
// do not decode them.
func randLogo(t *testing.T, n int) brandLogo {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	sum := sha256.Sum256(b)
	return brandLogo{data: b, sha: hex.EncodeToString(sum[:])}
}

// saveBrand runs the write sequence branding.sql documents (Ensure, then Set) for an
// accent and a logo, committed, in the tenant's own context.
func saveBrand(t *testing.T, d *DB, tenantID, adminID uuid.UUID, accent string, logo brandLogo) {
	t.Helper()
	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if e := q.EnsureTenantBrand(ctx, store.EnsureTenantBrandParams{TenantID: tenantID, UpdatedBy: adminID}); e != nil {
			return e
		}
		if n, e := q.SetTenantAccent(ctx, store.SetTenantAccentParams{Accent: accent, UpdatedBy: adminID, TenantID: tenantID}); e != nil || n != 1 {
			return errors.Join(e, errors.New("SetTenantAccent changed "+strconv.FormatInt(n, 10)+" rows, want 1"))
		}
		n, e := q.SetTenantLogo(ctx, store.SetTenantLogoParams{
			Logo: logo.data, LogoSha256: logo.sha, LogoMime: "image/png",
			LogoWidth: 64, LogoHeight: 32, UpdatedBy: adminID, TenantID: tenantID,
		})
		if e != nil || n != 1 {
			return errors.Join(e, errors.New("SetTenantLogo changed "+strconv.FormatInt(n, 10)+" rows, want 1"))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("saveBrand: %v", err)
	}
}

// brandTenantIDs runs a WHERE-less read of tenant_branding's scope column inside the
// given context and returns what it sees.
func brandTenantIDs(t *testing.T, d *DB, ctxTenant uuid.UUID) []uuid.UUID {
	t.Helper()
	var ids []uuid.UUID
	err := d.WithTenant(context.Background(), ctxTenant, func(ctx context.Context, tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `SELECT tenant_id FROM tenant_branding`)
		if e != nil {
			return e
		}
		got, e := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		ids = got
		return e
	})
	if err != nil {
		t.Fatalf("WHERE-less read in %s's context: %v", ctxTenant, err)
	}
	return ids
}

// =====================================================================================
// RLS: read side. A's context, with no WHERE at all, sees exactly A's row -- while B's
// row exists (B's own context reads it), as may rows that earlier runs left in the
// table.
// =====================================================================================

func TestRLS_TenantBranding_ReadIsolationWithoutWhere(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)

	a, aAdmin := brandTenant(t, app)
	b, bAdmin := brandTenant(t, app)
	aLogo, bLogo := randLogo(t, 512), randLogo(t, 512)
	saveBrand(t, app, a, aAdmin, "1F5C41", aLogo)
	saveBrand(t, app, b, bAdmin, "DA291C", bLogo)

	// Positive control: B's own context reads B's row -- the row the next probe must
	// not see is really there.
	if got := brandTenantIDs(t, app, b); len(got) != 1 || got[0] != b {
		t.Fatalf("positive control: B's context read %v from tenant_branding, want exactly [%s]", got, b)
	}
	got := brandTenantIDs(t, app, a)
	if len(got) != 1 || got[0] != a {
		t.Fatalf("RLS FAILED: A's WHERE-less read returned %v, want exactly [%s] (B is %s)", got, a, b)
	}

	// The logo route's read, through the shipped query. In A's context, B's digest finds
	// no row whether the explicit filter names A (the belt) or B (then RLS is what stands
	// in the way). Positive control: B's context reaches the bytes.
	err := app.WithTenant(context.Background(), b, func(ctx context.Context, tx pgx.Tx) error {
		row, e := store.New(tx).GetTenantLogo(ctx, store.GetTenantLogoParams{TenantID: b, LogoSha256: bLogo.sha})
		if e != nil {
			return e
		}
		if string(row.Logo) != string(bLogo.data) || row.LogoMime == nil || *row.LogoMime != "image/png" {
			t.Errorf("positive control: B's GetTenantLogo returned %d bytes, mime %v; want B's %d bytes, image/png",
				len(row.Logo), row.LogoMime, len(bLogo.data))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("positive control: B's context could not read its own logo: %v", err)
	}
	for _, named := range []uuid.UUID{a, b} {
		err := app.WithTenant(context.Background(), a, func(ctx context.Context, tx pgx.Tx) error {
			_, e := store.New(tx).GetTenantLogo(ctx, store.GetTenantLogoParams{TenantID: named, LogoSha256: bLogo.sha})
			return e
		})
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("A's context, GetTenantLogo(tenant=%s, B's digest): err=%v, want pgx.ErrNoRows", named, err)
		}
	}
}

// =====================================================================================
// RLS: write side. In A's context a row stamped with another tenant's id is refused by
// WITH CHECK, with the same error whether that tenant already has a row or not; and an
// UPDATE with no WHERE changes A's row only.
// =====================================================================================

func TestRLS_TenantBranding_WriteWithCheck(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)

	a, aAdmin := brandTenant(t, app)
	b, bAdmin := brandTenant(t, app) // has a branding row
	c, cAdmin := brandTenant(t, app) // has none
	saveBrand(t, app, a, aAdmin, "1F5C41", randLogo(t, 64))
	saveBrand(t, app, b, bAdmin, "DA291C", randLogo(t, 64))

	forge := func(target, targetAdmin uuid.UUID) error {
		return app.WithTenant(context.Background(), a, func(ctx context.Context, tx pgx.Tx) error {
			_, e := tx.Exec(ctx, `INSERT INTO tenant_branding (tenant_id, updated_by) VALUES ($1, $2)`, target, targetAdmin)
			return e
		})
	}
	errB, errC := forge(b, bAdmin), forge(c, cAdmin)
	assertWriteBlocked(t, "tenant_branding (target has a row)", blockRLS, errB)
	assertWriteBlocked(t, "tenant_branding (target has no row)", blockRLS, errC)
	// The two refusals are the same refusal: WITH CHECK runs before the primary key is
	// consulted, so the error does not say whether the other tenant has a brand.
	if pb, pc := asPgErr(errB), asPgErr(errC); pb.Code != pc.Code || pb.Message != pc.Message {
		t.Errorf("the refusal differs by whether the target tenant has a row:\n  has a row: %s %q\n  has none:  %s %q",
			pb.Code, pb.Message, pc.Code, pc.Message)
	}

	// The shipped write's step 1, pointed at another tenant from A's context.
	err := app.WithTenant(context.Background(), a, func(ctx context.Context, tx pgx.Tx) error {
		return store.New(tx).EnsureTenantBrand(ctx, store.EnsureTenantBrandParams{TenantID: c, UpdatedBy: cAdmin})
	})
	assertWriteBlocked(t, "EnsureTenantBrand(tenant=C) in A's context", blockRLS, err)

	// Positive control: the same INSERT for C in C's own context succeeds.
	if err := app.WithTenant(context.Background(), c, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO tenant_branding (tenant_id, updated_by) VALUES ($1, $2)`, c, cAdmin)
		return e
	}); err != nil {
		t.Fatalf("positive control: C could not insert its own row: %v", err)
	}

	// UPDATE with no WHERE in A's context touches exactly A's row; B's accent survives.
	var touched int64
	if err := app.WithTenant(context.Background(), a, func(ctx context.Context, tx pgx.Tx) error {
		tag, e := tx.Exec(ctx, `UPDATE tenant_branding SET accent = 'BE3D2A'`)
		touched = tag.RowsAffected()
		return e
	}); err != nil {
		t.Fatalf("WHERE-less UPDATE in A's context: %v", err)
	}
	if touched != 1 {
		t.Errorf("WHERE-less UPDATE in A's context changed %d rows, want 1 (A's own)", touched)
	}
	if got := brandAccent(t, app, b); got != "DA291C" {
		t.Errorf("RLS FAILED: B's accent is %q after A's WHERE-less UPDATE, want DA291C", got)
	}
	if got := brandAccent(t, app, a); got != "BE3D2A" {
		t.Errorf("positive control: A's accent is %q after its own UPDATE, want BE3D2A", got)
	}

	// The shipped UPDATE pointed at B from A's context changes nothing (USING hides it).
	if err := app.WithTenant(context.Background(), a, func(ctx context.Context, tx pgx.Tx) error {
		n, e := store.New(tx).SetTenantAccent(ctx, store.SetTenantAccentParams{Accent: "BE3D2A", UpdatedBy: aAdmin, TenantID: b})
		if e == nil && n != 0 {
			t.Errorf("SetTenantAccent(tenant=B) in A's context changed %d rows, want 0", n)
		}
		return e
	}); err != nil {
		t.Fatalf("SetTenantAccent(tenant=B) in A's context: %v", err)
	}

	// Moving a row to another tenant is not a privilege tappa_app holds.
	err = app.WithTenant(context.Background(), a, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE tenant_branding SET tenant_id = $1`, b)
		return e
	})
	assertPermissionDenied(t, "UPDATE tenant_branding SET tenant_id", err)
}

func brandAccent(t *testing.T, d *DB, tenantID uuid.UUID) string {
	t.Helper()
	var accent string
	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, e := store.New(tx).GetTenantBrand(ctx, tenantID)
		if e != nil {
			return e
		}
		if row.Accent != nil {
			accent = *row.Accent
		}
		return nil
	})
	if err != nil {
		t.Fatalf("GetTenantBrand(%s): %v", tenantID, err)
	}
	return accent
}

// =====================================================================================
// RLS: no context. On a connection whose app.tenant_id was written by an earlier
// transaction the GUC is '' (ADR 0002, Q27); the NULLIF policy turns that into "no
// rows", where a bare ::uuid cast would raise 22P02.
// =====================================================================================

func TestRLS_TenantBranding_NoContextFailsClosed(t *testing.T) {
	d := testDB(t) // one connection: the probe runs on the backend the write used
	assertAppRole(t, d)

	a, aAdmin := brandTenant(t, d)
	saveBrand(t, d, a, aAdmin, "1F5C41", randLogo(t, 64))

	if got := brandTenantIDs(t, d, a); len(got) != 1 || got[0] != a {
		t.Fatalf("positive control: A's context read %v, want [%s]", got, a)
	}
	var guc string
	if err := d.pool.QueryRow(context.Background(), `SELECT current_setting('app.tenant_id', true)`).Scan(&guc); err != nil {
		t.Fatalf("read app.tenant_id: %v", err)
	}
	if guc != "" {
		t.Fatalf("app.tenant_id after the tenant transactions = %q, want '' (the case this test measures)", guc)
	}
	var n int
	if err := d.pool.QueryRow(context.Background(), `SELECT count(*) FROM tenant_branding`).Scan(&n); err != nil {
		t.Fatalf("context-less WHERE-less read errored (%v); the NULLIF policy must yield 0 rows, not an error", err)
	}
	if n != 0 {
		t.Fatalf("context-less WHERE-less read saw %d rows, want 0", n)
	}
}

// =====================================================================================
// Privileges: what tappa_app holds on the table, by column, and that a DELETE is
// refused by privilege rather than by an empty table.
// =====================================================================================

func TestTenantBranding_AppPrivileges(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	ctx := context.Background()

	tableLevel := []struct {
		priv string
		want bool
	}{
		{"SELECT", true},
		{"INSERT", false}, // column-level only, below
		{"UPDATE", false}, // column-level only, below
		{"DELETE", false},
		{"TRUNCATE", false},
		{"REFERENCES", false},
		{"TRIGGER", false},
	}
	for _, c := range tableLevel {
		var got bool
		if err := app.pool.QueryRow(ctx, `SELECT has_table_privilege('tappa_app', 'public.tenant_branding', $1)`, c.priv).Scan(&got); err != nil {
			t.Fatalf("has_table_privilege(%s): %v", c.priv, err)
		}
		if got != c.want {
			t.Errorf("has_table_privilege('tappa_app','tenant_branding','%s') = %v, want %v", c.priv, got, c.want)
		}
	}

	columns := []struct{ priv, want string }{
		{"SELECT", "tenant_id,accent,logo,logo_sha256,logo_mime,logo_width,logo_height,created_at,updated_at,updated_by"},
		{"INSERT", "tenant_id,updated_by"},
		{"UPDATE", "accent,logo,logo_sha256,logo_mime,logo_width,logo_height,updated_at,updated_by"},
	}
	for _, c := range columns {
		var got string
		if err := app.pool.QueryRow(ctx, `
			SELECT coalesce(string_agg(a.attname, ',' ORDER BY a.attnum), '')
			  FROM pg_attribute a
			 WHERE a.attrelid = 'public.tenant_branding'::regclass AND a.attnum > 0 AND NOT a.attisdropped
			   AND has_column_privilege('tappa_app', a.attrelid, a.attname, $1)`, c.priv).Scan(&got); err != nil {
			t.Fatalf("column privileges (%s): %v", c.priv, err)
		}
		if got != c.want {
			t.Errorf("tappa_app %s columns on tenant_branding = (%s), want (%s)", c.priv, got, c.want)
		}
	}

	// Behaviour: the DELETE is refused by privilege, on a table where A's row exists.
	a, aAdmin := brandTenant(t, app)
	saveBrand(t, app, a, aAdmin, "1F5C41", randLogo(t, 64))
	err := app.WithTenant(ctx, a, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `DELETE FROM tenant_branding`)
		return e
	})
	assertPermissionDenied(t, "DELETE FROM tenant_branding", err)
	if got := brandTenantIDs(t, app, a); len(got) != 1 {
		t.Errorf("positive control: A's row count after the refused DELETE is %d, want 1", len(got))
	}
}

// =====================================================================================
// CHECKs, with hostile values, through the shipped queries where a shipped query can
// carry the value. Each refusal is pinned to its SQLSTATE and constraint NAME, so a
// value refused by a different constraint than the one meant for it fails the case.
// =====================================================================================

func TestTenantBranding_ChecksRefuseHostileValues(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)

	a, aAdmin := brandTenant(t, app)
	if err := app.WithTenant(context.Background(), a, func(ctx context.Context, tx pgx.Tx) error {
		return store.New(tx).EnsureTenantBrand(ctx, store.EnsureTenantBrandParams{TenantID: a, UpdatedBy: aAdmin})
	}); err != nil {
		t.Fatalf("EnsureTenantBrand: %v", err)
	}

	setAccent := func(accent string) func(ctx context.Context, q *store.Queries, tx pgx.Tx) error {
		return func(ctx context.Context, q *store.Queries, _ pgx.Tx) error {
			_, e := q.SetTenantAccent(ctx, store.SetTenantAccentParams{Accent: accent, UpdatedBy: aAdmin, TenantID: a})
			return e
		}
	}
	good := randLogo(t, 128)
	other := randLogo(t, 128)
	setLogo := func(p store.SetTenantLogoParams) func(ctx context.Context, q *store.Queries, tx pgx.Tx) error {
		p.UpdatedBy, p.TenantID = aAdmin, a
		return func(ctx context.Context, q *store.Queries, _ pgx.Tx) error {
			_, e := q.SetTenantLogo(ctx, p)
			return e
		}
	}
	logo := func(mut func(*store.SetTenantLogoParams)) store.SetTenantLogoParams {
		p := store.SetTenantLogoParams{Logo: good.data, LogoSha256: good.sha, LogoMime: "image/png", LogoWidth: 64, LogoHeight: 64}
		mut(&p)
		return p
	}
	over := randLogo(t, 262145)

	const (
		checkViolation = "23514"
		tooLong        = "22001"
	)
	type checkCase struct {
		name       string
		run        func(ctx context.Context, q *store.Queries, tx pgx.Tx) error
		code       string
		constraint string
	}
	cases := []checkCase{
		{"accent lower-case hex", setAccent("1f5c41"), checkViolation, "tenant_branding_accent_canonical"},
		{"accent mixed case", setAccent("1F5c41"), checkViolation, "tenant_branding_accent_canonical"},
		{"accent three hex digits", setAccent("FFF"), checkViolation, "tenant_branding_accent_canonical"},
		{"accent with # in six characters", setAccent("#1F5C4"), checkViolation, "tenant_branding_accent_canonical"},
		{"accent with # in seven characters", setAccent("#1F5C41"), tooLong, ""},
		{"accent seven hex digits, not truncated", setAccent("1F5C41A"), tooLong, ""},
		{"accent non-hex letter", setAccent("1F5C4G"), checkViolation, "tenant_branding_accent_canonical"},
		{"accent leading space", setAccent(" 1F5C4"), checkViolation, "tenant_branding_accent_canonical"},
		{"accent empty", setAccent(""), checkViolation, "tenant_branding_accent_canonical"},

		{"logo 262145 bytes", setLogo(logo(func(p *store.SetTenantLogoParams) { p.Logo, p.LogoSha256 = over.data, over.sha })), checkViolation, "tenant_branding_logo_size"},
		{"logo zero bytes", setLogo(logo(func(p *store.SetTenantLogoParams) {
			p.Logo = []byte{}
			sum := sha256.Sum256(nil)
			p.LogoSha256 = hex.EncodeToString(sum[:])
		})), checkViolation, "tenant_branding_logo_size"},
		{"sha256 upper-case hex", setLogo(logo(func(p *store.SetTenantLogoParams) { p.LogoSha256 = strings.ToUpper(good.sha) })), checkViolation, "tenant_branding_logo_sha256_hex"},
		{"sha256 63 characters", setLogo(logo(func(p *store.SetTenantLogoParams) { p.LogoSha256 = good.sha[:63] })), checkViolation, "tenant_branding_logo_sha256_hex"},
		{"sha256 of other bytes", setLogo(logo(func(p *store.SetTenantLogoParams) { p.LogoSha256 = other.sha })), checkViolation, "tenant_branding_logo_sha256_matches_logo"},
		{"mime image/gif", setLogo(logo(func(p *store.SetTenantLogoParams) { p.LogoMime = "image/gif" })), checkViolation, "tenant_branding_logo_mime_known"},
		{"mime image/svg+xml", setLogo(logo(func(p *store.SetTenantLogoParams) { p.LogoMime = "image/svg+xml" })), checkViolation, "tenant_branding_logo_mime_known"},
		{"mime upper-case", setLogo(logo(func(p *store.SetTenantLogoParams) { p.LogoMime = "IMAGE/PNG" })), checkViolation, "tenant_branding_logo_mime_known"},
		{"mime trailing space", setLogo(logo(func(p *store.SetTenantLogoParams) { p.LogoMime = "image/png " })), checkViolation, "tenant_branding_logo_mime_known"},
		{"width 0", setLogo(logo(func(p *store.SetTenantLogoParams) { p.LogoWidth = 0 })), checkViolation, "tenant_branding_logo_width_range"},
		{"width 513", setLogo(logo(func(p *store.SetTenantLogoParams) { p.LogoWidth = 513 })), checkViolation, "tenant_branding_logo_width_range"},
		{"width -1", setLogo(logo(func(p *store.SetTenantLogoParams) { p.LogoWidth = -1 })), checkViolation, "tenant_branding_logo_width_range"},
		{"height 0", setLogo(logo(func(p *store.SetTenantLogoParams) { p.LogoHeight = 0 })), checkViolation, "tenant_branding_logo_height_range"},
		{"height 513", setLogo(logo(func(p *store.SetTenantLogoParams) { p.LogoHeight = 513 })), checkViolation, "tenant_branding_logo_height_range"},
		{"shipped SetTenantLogo with no bytes", setLogo(logo(func(p *store.SetTenantLogoParams) { p.Logo = nil })), checkViolation, "tenant_branding_logo_all_or_none"},
	}

	// Partial logo columns: each of the five set alone, and each of the five left out,
	// written directly (the shipped query always writes all five). The values that ARE
	// written are valid, so the only constraint that can fire is all-or-none.
	logoColumns := []struct {
		col string
		val any
	}{
		{"logo", good.data}, {"logo_sha256", good.sha}, {"logo_mime", "image/png"},
		{"logo_width", int32(64)}, {"logo_height", int32(64)},
	}
	partial := func(cols []int) func(ctx context.Context, q *store.Queries, tx pgx.Tx) error {
		return func(ctx context.Context, _ *store.Queries, tx pgx.Tx) error {
			sets := make([]string, 0, len(cols))
			args := []any{a}
			for _, i := range cols {
				args = append(args, logoColumns[i].val)
				sets = append(sets, logoColumns[i].col+" = $"+strconv.Itoa(len(args)))
			}
			_, e := tx.Exec(ctx, `UPDATE tenant_branding SET `+strings.Join(sets, ", ")+` WHERE tenant_id = $1`, args...)
			return e
		}
	}
	for i := range logoColumns {
		cases = append(cases, checkCase{"only " + logoColumns[i].col, partial([]int{i}), checkViolation, "tenant_branding_logo_all_or_none"})
		var rest []int
		for j := range logoColumns {
			if j != i {
				rest = append(rest, j)
			}
		}
		cases = append(cases, checkCase{"all but " + logoColumns[i].col, partial(rest), checkViolation, "tenant_branding_logo_all_or_none"})
	}

	// Every hostile case runs in a transaction that is ROLLED BACK whatever happens, so
	// a value a mutant schema wrongly accepts is not left in A's row to change what the
	// next case runs against (measured: with the size bound at 262145 the accepted
	// oversized logo stayed committed and turned five later cases into different
	// refusals).
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			tx := beginInTenant(t, ctx, app, a)
			err := c.run(ctx, store.New(tx), tx)
			if rerr := tx.Rollback(ctx); rerr != nil {
				t.Fatalf("rollback: %v", rerr)
			}
			pg := asPgErr(err)
			if pg == nil {
				t.Fatalf("accepted (err=%v), want SQLSTATE %s %s", err, c.code, c.constraint)
			}
			if pg.Code != c.code || pg.ConstraintName != c.constraint {
				t.Fatalf("refused with SQLSTATE %s constraint %q (%s), want %s %q", pg.Code, pg.ConstraintName, pg.Message, c.code, c.constraint)
			}
		})
	}

	// Positive controls, through the shipped queries: the canonical accent, the
	// largest logo the table admits (262 144 bytes) and all five logo columns together
	// at the edge of every range. Without these, every refusal above could be a table
	// that refuses everything.
	maxLogo := randLogo(t, 262144)
	positives := []struct {
		name string
		run  func(ctx context.Context, q *store.Queries, tx pgx.Tx) error
	}{
		{"accent canonical", setAccent("1F5C41")},
		{"logo 262144 bytes, 512x512, image/jpeg", setLogo(store.SetTenantLogoParams{Logo: maxLogo.data, LogoSha256: maxLogo.sha, LogoMime: "image/jpeg", LogoWidth: 512, LogoHeight: 512})},
		{"logo 1 byte, 1x1, image/png", func() func(ctx context.Context, q *store.Queries, tx pgx.Tx) error {
			one := randLogo(t, 1)
			return setLogo(store.SetTenantLogoParams{Logo: one.data, LogoSha256: one.sha, LogoMime: "image/png", LogoWidth: 1, LogoHeight: 1})
		}()},
	}
	for _, p := range positives {
		if err := app.WithTenant(context.Background(), a, func(ctx context.Context, tx pgx.Tx) error {
			return p.run(ctx, store.New(tx), tx)
		}); err != nil {
			t.Errorf("positive control %q refused: %v", p.name, err)
		}
	}

	// Documented acceptance, not a refusal: assigning text to char(6) trims extra
	// characters when they are all trailing spaces, so this spelling is ACCEPTED and the
	// stored value is the canonical six digits. Pinned so a reader of the refusals above
	// does not take "longer than six" as always refused (WL-4: the audit "after" is the
	// stored value or Color.Hex(), not the argument's text).
	t.Run("accent with trailing spaces is stored canonical", func(t *testing.T) {
		ctx := context.Background()
		tx := beginInTenant(t, ctx, app, a)
		q := store.New(tx)
		n, err := q.SetTenantAccent(ctx, store.SetTenantAccentParams{Accent: "1F5C41   ", UpdatedBy: aAdmin, TenantID: a})
		if err != nil || n != 1 {
			t.Fatalf("SetTenantAccent(%q): %d rows, %v; want 1 row, accepted", "1F5C41   ", n, err)
		}
		row, err := q.GetTenantBrand(ctx, a)
		if err != nil {
			t.Fatalf("GetTenantBrand: %v", err)
		}
		if row.Accent == nil || *row.Accent != "1F5C41" {
			t.Fatalf("stored accent = %v, want exactly 1F5C41", row.Accent)
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatalf("rollback: %v", err)
		}
	})
}

// =====================================================================================
// Composite FK: updated_by must be an admin of the row's own tenant. B's admin id is
// a real admin_users row, so only the composite (updated_by, tenant_id) reference
// refuses it; a single-column FK on updated_by would accept it.
// =====================================================================================

func TestTenantBranding_UpdatedByIsAnAdminOfTheSameTenant(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)

	a, aAdmin := brandTenant(t, app)
	_, bAdmin := brandTenant(t, app)

	wantFK := func(op string, err error) {
		t.Helper()
		pg := asPgErr(err)
		if pg == nil || pg.Code != "23503" || pg.ConstraintName != "tenant_branding_updated_by_fk" {
			t.Errorf("%s: err=%v, want 23503 on tenant_branding_updated_by_fk", op, err)
		}
	}

	// The insert path (no row yet) with B's admin.
	err := app.WithTenant(context.Background(), a, func(ctx context.Context, tx pgx.Tx) error {
		return store.New(tx).EnsureTenantBrand(ctx, store.EnsureTenantBrandParams{TenantID: a, UpdatedBy: bAdmin})
	})
	wantFK("EnsureTenantBrand(updated_by = B's admin)", err)

	// Positive control: A's own admin creates the row.
	if err := app.WithTenant(context.Background(), a, func(ctx context.Context, tx pgx.Tx) error {
		return store.New(tx).EnsureTenantBrand(ctx, store.EnsureTenantBrandParams{TenantID: a, UpdatedBy: aAdmin})
	}); err != nil {
		t.Fatalf("positive control: EnsureTenantBrand with A's admin: %v", err)
	}

	// The update path, on the row that now exists, with B's admin.
	err = app.WithTenant(context.Background(), a, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).SetTenantAccent(ctx, store.SetTenantAccentParams{Accent: "1F5C41", UpdatedBy: bAdmin, TenantID: a})
		return e
	})
	wantFK("SetTenantAccent(updated_by = B's admin)", err)

	// Positive control: the same update with A's admin.
	if err := app.WithTenant(context.Background(), a, func(ctx context.Context, tx pgx.Tx) error {
		n, e := store.New(tx).SetTenantAccent(ctx, store.SetTenantAccentParams{Accent: "1F5C41", UpdatedBy: aAdmin, TenantID: a})
		if e == nil && n != 1 {
			t.Errorf("positive control: SetTenantAccent with A's admin changed %d rows, want 1", n)
		}
		return e
	}); err != nil {
		t.Fatalf("positive control: SetTenantAccent with A's admin: %v", err)
	}
}

// =====================================================================================
// No global uniqueness on the digest: two tenants storing the same bytes both succeed,
// so the second write gets no 23505 that would tell it the first exists (ADR 0024
// Iddia D).
// =====================================================================================

func TestTenantBranding_SameLogoInTwoTenants(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)

	a, aAdmin := brandTenant(t, app)
	b, bAdmin := brandTenant(t, app)
	same := randLogo(t, 2048)
	saveBrand(t, app, a, aAdmin, "1F5C41", same)
	saveBrand(t, app, b, bAdmin, "1F5C41", same) // t.Fatalf inside on 23505

	for _, tenant := range []uuid.UUID{a, b} {
		if err := app.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
			row, e := store.New(tx).GetTenantLogo(ctx, store.GetTenantLogoParams{TenantID: tenant, LogoSha256: same.sha})
			if e == nil && string(row.Logo) != string(same.data) {
				t.Errorf("tenant %s: GetTenantLogo returned %d bytes that are not the stored ones", tenant, len(row.Logo))
			}
			return e
		}); err != nil {
			t.Errorf("tenant %s: GetTenantLogo by the shared digest: %v", tenant, err)
		}
	}
}

// =====================================================================================
// The shipped write sequence end to end: a reset is an UPDATE, the row stays, and the
// per-page read reports exactly what was written.
// =====================================================================================

func TestTenantBranding_StoreRoundTrip(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)

	a, aAdmin := brandTenant(t, app)
	lg := randLogo(t, 4096)
	saveBrand(t, app, a, aAdmin, "D98E2B", lg)

	read := func() store.GetTenantBrandRow {
		t.Helper()
		var row store.GetTenantBrandRow
		if err := app.WithTenant(context.Background(), a, func(ctx context.Context, tx pgx.Tx) error {
			r, e := store.New(tx).GetTenantBrand(ctx, a)
			row = r
			return e
		}); err != nil {
			t.Fatalf("GetTenantBrand: %v", err)
		}
		return row
	}
	str := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}
	num := func(p *int32) string {
		if p == nil {
			return "<nil>"
		}
		return strconv.Itoa(int(*p))
	}

	r := read()
	if str(r.Accent) != "D98E2B" || str(r.LogoSha256) != lg.sha || str(r.LogoMime) != "image/png" ||
		num(r.LogoWidth) != "64" || num(r.LogoHeight) != "32" || r.UpdatedBy != aAdmin {
		t.Fatalf("after saving: accent=%s sha=%s mime=%s %sx%s by=%s; want D98E2B, the logo's digest, image/png, 64x32, A's admin",
			str(r.Accent), str(r.LogoSha256), str(r.LogoMime), num(r.LogoWidth), num(r.LogoHeight), r.UpdatedBy)
	}

	if err := app.WithTenant(context.Background(), a, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if n, e := q.ClearTenantLogo(ctx, store.ClearTenantLogoParams{UpdatedBy: aAdmin, TenantID: a}); e != nil || n != 1 {
			return errors.Join(e, errors.New("ClearTenantLogo changed "+strconv.FormatInt(n, 10)+" rows, want 1"))
		}
		n, e := q.ClearTenantAccent(ctx, store.ClearTenantAccentParams{UpdatedBy: aAdmin, TenantID: a})
		if e == nil && n != 1 {
			return errors.New("ClearTenantAccent changed " + strconv.FormatInt(n, 10) + " rows, want 1")
		}
		return e
	}); err != nil {
		t.Fatalf("clear: %v", err)
	}

	r = read() // the row is still there: the reset was an UPDATE
	if r.Accent != nil || r.LogoSha256 != nil || r.LogoMime != nil || r.LogoWidth != nil || r.LogoHeight != nil {
		t.Errorf("after both resets: accent=%s sha=%s mime=%s %sx%s; want every brand field NULL",
			str(r.Accent), str(r.LogoSha256), str(r.LogoMime), num(r.LogoWidth), num(r.LogoHeight))
	}
	err := app.WithTenant(context.Background(), a, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).GetTenantLogo(ctx, store.GetTenantLogoParams{TenantID: a, LogoSha256: lg.sha})
		return e
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("GetTenantLogo by the cleared digest: err=%v, want pgx.ErrNoRows", err)
	}
}

// =====================================================================================
// The write sequence's "before" under concurrency (branding.sql's header): a second
// writer waits for the first -- in EnsureTenantBrand when the row is new, in
// GetTenantBrandForUpdate when it exists -- and then reads the value the first one
// committed. Without EnsureTenantBrand, the lock finds no row while a first write is
// still in flight.
// =====================================================================================

// beginInTenant opens a transaction on its own pooled connection with the tenant
// context set the way WithTenant sets it, rolled back at cleanup unless committed.
func beginInTenant(t *testing.T, ctx context.Context, d *DB, tenantID uuid.UUID) pgx.Tx {
	t.Helper()
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Logf("rollback: %v", err)
		}
	})
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID.String()); err != nil {
		t.Fatalf("set tenant context: %v", err)
	}
	return tx
}

// twoWriters runs the documented write sequence (Ensure, ForUpdate, SetTenantAccent)
// in two transactions on the same tenant, interleaved at the point that decides the
// "before": writer 1 runs steps 1 and 2 and stops; writer 2 starts and is observed
// WAITING on a lock, in the statement whose sqlc name is waitIn; writer 1 sets 1F5C41
// and commits; writer 2 sets DA291C and commits. It returns the "before" writer 2 read.
//
// Writer 1 stops before it changes the row so that, on an existing row, nothing but
// GetTenantBrandForUpdate's FOR UPDATE holds writer 2 back -- measured: with FOR UPDATE
// removed from the query, writer 2 runs to the end without waiting and this fails.
func twoWriters(t *testing.T, ctx context.Context, app *DB, tenant, admin uuid.UUID, waitIn string) *string {
	t.Helper()
	tx1 := beginInTenant(t, ctx, app, tenant)
	q1 := store.New(tx1)
	if err := q1.EnsureTenantBrand(ctx, store.EnsureTenantBrandParams{TenantID: tenant, UpdatedBy: admin}); err != nil {
		t.Fatalf("writer 1 ensure: %v", err)
	}
	if _, err := q1.GetTenantBrandForUpdate(ctx, tenant); err != nil {
		t.Fatalf("writer 1 lock: %v", err)
	}

	tx2 := beginInTenant(t, ctx, app, tenant)
	var pid2 int32
	if err := tx2.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid2); err != nil {
		t.Fatalf("writer 2 pid: %v", err)
	}
	type result struct {
		before *string
		err    error
	}
	done := make(chan result, 1)
	go func() {
		q2 := store.New(tx2)
		if err := q2.EnsureTenantBrand(ctx, store.EnsureTenantBrandParams{TenantID: tenant, UpdatedBy: admin}); err != nil {
			done <- result{err: errors.Join(errors.New("writer 2 ensure"), err)}
			return
		}
		b, err := q2.GetTenantBrandForUpdate(ctx, tenant)
		if err != nil {
			done <- result{err: errors.Join(errors.New("writer 2 lock"), err)}
			return
		}
		if _, err := q2.SetTenantAccent(ctx, store.SetTenantAccentParams{Accent: "DA291C", UpdatedBy: admin, TenantID: tenant}); err != nil {
			done <- result{err: errors.Join(errors.New("writer 2 set"), err)}
			return
		}
		done <- result{before: b.Accent, err: tx2.Commit(ctx)}
	}()

	// Writer 2 must be WAITING, in the expected statement, before writer 1 writes and
	// commits; otherwise the order this case is about did not happen.
	if stmt := waitForLockWait(t, ctx, app, pid2, func() bool { return len(done) > 0 }); !strings.Contains(stmt, "-- name: "+waitIn+" ") {
		t.Fatalf("writer 2 waits in %q, want the statement named %s", firstLine(stmt), waitIn)
	}
	if _, err := q1.SetTenantAccent(ctx, store.SetTenantAccentParams{Accent: "1F5C41", UpdatedBy: admin, TenantID: tenant}); err != nil {
		t.Fatalf("writer 1 set: %v", err)
	}
	if err := tx1.Commit(ctx); err != nil {
		t.Fatalf("writer 1 commit: %v", err)
	}
	var r result
	select {
	case r = <-done:
	case <-ctx.Done():
		t.Fatalf("writer 2 did not finish: %v", ctx.Err())
	}
	if r.err != nil {
		t.Fatalf("%v", r.err)
	}
	if got := brandAccent(t, app, tenant); got != "DA291C" {
		t.Errorf("final accent %q, want DA291C", got)
	}
	return r.before
}

func TestTenantBranding_EnsureThenLockReturnsWhatTheOtherWriterCommitted(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// First brand: writer 2 waits in EnsureTenantBrand, on writer 1's uncommitted
	// insert of the same primary key.
	t.Run("first write", func(t *testing.T) {
		tenant, admin := brandTenant(t, app)
		if before := twoWriters(t, ctx, app, tenant, admin, "EnsureTenantBrand"); before == nil || *before != "1F5C41" {
			t.Fatalf("writer 2's before = %v, want 1F5C41 (writer 1's committed value)", before)
		}
	})

	// Existing row: writer 2's EnsureTenantBrand finds the committed row and does
	// nothing, and it waits in GetTenantBrandForUpdate, on writer 1's row lock.
	t.Run("existing row", func(t *testing.T) {
		tenant, admin := brandTenant(t, app)
		saveBrand(t, app, tenant, admin, "BE3D2A", randLogo(t, 64))
		if before := twoWriters(t, ctx, app, tenant, admin, "GetTenantBrandForUpdate"); before == nil || *before != "1F5C41" {
			t.Fatalf("writer 2's before = %v, want 1F5C41 (writer 1's committed value, not the BE3D2A it replaced)", before)
		}
	})

	t.Run("without ensure", func(t *testing.T) {
		tenant, admin := brandTenant(t, app)
		tx1 := beginInTenant(t, ctx, app, tenant)
		q1 := store.New(tx1)
		if err := q1.EnsureTenantBrand(ctx, store.EnsureTenantBrandParams{TenantID: tenant, UpdatedBy: admin}); err != nil {
			t.Fatalf("writer 1 ensure: %v", err)
		}
		if _, err := q1.SetTenantAccent(ctx, store.SetTenantAccentParams{Accent: "1F5C41", UpdatedBy: admin, TenantID: tenant}); err != nil {
			t.Fatalf("writer 1 set: %v", err)
		}
		tx2 := beginInTenant(t, ctx, app, tenant)
		_, err := store.New(tx2).GetTenantBrandForUpdate(ctx, tenant)
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("writer 2's lock without ensure: err=%v, want pgx.ErrNoRows while writer 1's first row is uncommitted", err)
		}
	})
}

// firstLine returns s up to its first newline, for messages.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// waitForLockWait polls pg_stat_activity until the backend pid is waiting on a lock and
// returns the text of the statement it is waiting in. It stops early if finished
// reports that the waiter has already returned, which means it did not wait.
func waitForLockWait(t *testing.T, ctx context.Context, d *DB, pid int32, finished func() bool) string {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var wait *string
		var query string
		if err := d.pool.QueryRow(ctx, `SELECT wait_event_type, query FROM pg_stat_activity WHERE pid = $1`, pid).Scan(&wait, &query); err != nil {
			t.Fatalf("read pg_stat_activity: %v", err)
		}
		if wait != nil && *wait == "Lock" {
			return query
		}
		if finished() {
			t.Fatalf("writer 2 finished without waiting on writer 1's lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("writer 2 (pid %d) did not wait on a lock within 15 s", pid)
	return ""
}

// =====================================================================================
// The catalogue: the primary key is the only index, RLS is enabled and forced, the
// single policy carries the NULLIF expression in BOTH USING and WITH CHECK, and the FK
// on updated_by is the composite one. Two of these are invisible to the behaviour tests
// above (measured by mutation): an ALL policy without WITH CHECK applies USING to
// writes, and FORCE binds the table owner, which tappa_app is not.
// =====================================================================================

func TestTenantBranding_CatalogShape(t *testing.T) {
	app := appDB(t)
	ctx := context.Background()

	rows, err := app.pool.Query(ctx, `
		SELECT i.relname, ix.indisprimary, ix.indisunique,
		       (SELECT string_agg(a.attname, ',' ORDER BY k.ord)
		          FROM unnest(ix.indkey::int2[]) WITH ORDINALITY AS k(attnum, ord)
		          JOIN pg_attribute a ON a.attrelid = ix.indrelid AND a.attnum = k.attnum)
		  FROM pg_index ix JOIN pg_class i ON i.oid = ix.indexrelid
		 WHERE ix.indrelid = 'public.tenant_branding'::regclass
		 ORDER BY 1`)
	if err != nil {
		t.Fatalf("read indexes: %v", err)
	}
	var indexes []string
	for rows.Next() {
		var name, cols string
		var primary, unique bool
		if err := rows.Scan(&name, &primary, &unique, &cols); err != nil {
			t.Fatalf("scan index: %v", err)
		}
		indexes = append(indexes, name+" primary="+strconv.FormatBool(primary)+" unique="+strconv.FormatBool(unique)+" ("+cols+")")
	}
	rows.Close()
	if want := []string{"tenant_branding_pkey primary=true unique=true (tenant_id)"}; strings.Join(indexes, "|") != strings.Join(want, "|") {
		t.Errorf("indexes on tenant_branding = %v, want %v", indexes, want)
	}

	var enabled, forced bool
	if err := app.pool.QueryRow(ctx, `SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = 'public.tenant_branding'::regclass`).Scan(&enabled, &forced); err != nil {
		t.Fatalf("read RLS flags: %v", err)
	}
	if !enabled || !forced {
		t.Errorf("tenant_branding RLS enabled=%v forced=%v, want both true", enabled, forced)
	}

	const nullif = `(tenant_id = (NULLIF(current_setting('app.tenant_id'::text, true), ''::text))::uuid)`
	prow, err := app.pool.Query(ctx, `
		SELECT policyname, permissive, roles::text, cmd, coalesce(qual, '<none>'), coalesce(with_check, '<none>')
		  FROM pg_policies WHERE schemaname = 'public' AND tablename = 'tenant_branding' ORDER BY 1`)
	if err != nil {
		t.Fatalf("read policies: %v", err)
	}
	var policies []string
	for prow.Next() {
		var name, permissive, roles, cmd, qual, check string
		if err := prow.Scan(&name, &permissive, &roles, &cmd, &qual, &check); err != nil {
			t.Fatalf("scan policy: %v", err)
		}
		policies = append(policies, strings.Join([]string{name, permissive, roles, cmd, qual, check}, " | "))
	}
	prow.Close()
	want := strings.Join([]string{"tenant_branding_tenant_isolation", "PERMISSIVE", "{public}", "ALL", nullif, nullif}, " | ")
	if len(policies) != 1 || policies[0] != want {
		t.Errorf("policies on tenant_branding:\n  %s\nwant exactly:\n  %s", strings.Join(policies, "\n  "), want)
	}

	var fk string
	if err := app.pool.QueryRow(ctx, `
		SELECT coalesce(string_agg(pg_get_constraintdef(oid), ' ; ' ORDER BY conname), '')
		  FROM pg_constraint
		 WHERE conrelid = 'public.tenant_branding'::regclass AND contype = 'f'
		   AND conkey @> ARRAY[(SELECT attnum FROM pg_attribute
		                         WHERE attrelid = 'public.tenant_branding'::regclass AND attname = 'updated_by')]`).Scan(&fk); err != nil {
		t.Fatalf("read the updated_by FK: %v", err)
	}
	if want := "FOREIGN KEY (updated_by, tenant_id) REFERENCES admin_users(id, tenant_id) ON DELETE RESTRICT"; fk != want {
		t.Errorf("the FK(s) on updated_by = %q, want exactly %q", fk, want)
	}
}

// =====================================================================================
// The per-page reads do not select the logo's bytes (ADR 0024 §4: "test sorgu metnini
// okur"). It reads the statement text sqlc SHIPPED -- the query constants in
// internal/store/branding.sql.go -- not db/queries.
// =====================================================================================

// brandingQueryConstants returns internal/store/branding.sql.go's string constants by
// name, unquoted.
func brandingQueryConstants(t *testing.T) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join("..", "store", "branding.sql.go"), nil, 0)
	if err != nil {
		t.Fatalf("parse internal/store/branding.sql.go: %v", err)
	}
	out := map[string]string{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, sp := range gd.Specs {
			vs, ok := sp.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, id := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("unquote %s: %v", id.Name, err)
				}
				out[id.Name] = s
			}
		}
	}
	return out
}

var sqlIdentRE = regexp.MustCompile(`[a-z_][a-z0-9_]*|\*`)

// selectList returns the identifier tokens between the statement's first SELECT and
// the first FROM after it, lower-cased; "-- name:" header lines are dropped first.
func selectList(stmt string) []string {
	var body []string
	for _, line := range strings.Split(stmt, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			body = append(body, line)
		}
	}
	s := strings.ToLower(strings.Join(body, "\n"))
	i := strings.Index(s, "select")
	if i < 0 {
		return nil
	}
	s = s[i+len("select"):]
	if j := strings.Index(s, "from"); j >= 0 {
		s = s[:j]
	}
	return sqlIdentRE.FindAllString(s, -1)
}

func TestTenantBranding_PerPageReadsDoNotSelectTheLogo(t *testing.T) {
	t.Parallel()
	consts := brandingQueryConstants(t)

	want := "accent,logo_sha256,logo_mime,logo_width,logo_height,updated_at,updated_by"
	for _, name := range []string{"getTenantBrand", "getTenantBrandForUpdate"} {
		stmt, ok := consts[name]
		if !ok {
			t.Fatalf("internal/store/branding.sql.go has no constant %s", name)
		}
		if got := strings.Join(selectList(stmt), ","); got != want {
			t.Errorf("%s selects (%s), want exactly (%s)", name, got, want)
		}
		all := sqlIdentRE.FindAllString(strings.ToLower(stmt), -1)
		sort.Strings(all)
		for _, tok := range all {
			if tok == "logo" || tok == "*" {
				t.Errorf("%s's statement text carries the token %q:\n%s", name, tok, stmt)
			}
		}
	}

	// Positive control: the same tokenizer finds `logo` in the one statement that is
	// meant to return the bytes. If it did not, the checks above would pass on any text.
	logoStmt, ok := consts["getTenantLogo"]
	if !ok {
		t.Fatalf("internal/store/branding.sql.go has no constant getTenantLogo")
	}
	if got := strings.Join(selectList(logoStmt), ","); got != "logo,logo_mime" {
		t.Errorf("positive control: getTenantLogo selects (%s), want (logo,logo_mime)", got)
	}
}

// =====================================================================================
// The belt (CLAUDE.md §4.5): every SELECT and UPDATE in branding.sql compares tenant_id
// with a bound parameter in its WHERE clause. RLS alone already hides other tenants'
// rows, so no behaviour test above can see a statement that lost this predicate
// (measured: GetTenantLogo with `WHERE @tenant_id::uuid IS NOT NULL AND logo_sha256 =
// ...` left every test in this file green). The INSERT's tenant column is
// cmd/tappa's TestQueriesInsertValuesNameTheTenantTheCallerNamed's business.
// =====================================================================================

var tenantPredicateRE = regexp.MustCompile(`(^|[^a-z0-9_.])tenant_id\s*=\s*\$[0-9]+`)

func TestTenantBranding_EveryStatementNamesTheTenant(t *testing.T) {
	t.Parallel()
	consts := brandingQueryConstants(t)

	checked := 0
	for name, stmt := range consts {
		var body []string
		for _, line := range strings.Split(stmt, "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "--") {
				body = append(body, line)
			}
		}
		s := strings.ToLower(strings.Join(body, "\n"))
		if strings.HasPrefix(strings.TrimSpace(s), "insert") {
			continue
		}
		i := strings.LastIndex(s, "where")
		if i < 0 {
			t.Errorf("%s has no WHERE clause:\n%s", name, stmt)
			continue
		}
		if !tenantPredicateRE.MatchString(s[i:]) {
			t.Errorf("%s's WHERE does not compare tenant_id with a bound parameter:\n%s", name, stmt)
		}
		checked++
	}
	// Seven of branding.sql's eight statements are SELECT or UPDATE; a count below that
	// means the constants were not found, and the loop above checked nothing.
	if checked != 7 {
		t.Errorf("checked %d SELECT/UPDATE statement(s) in internal/store/branding.sql.go, want 7", checked)
	}
}
