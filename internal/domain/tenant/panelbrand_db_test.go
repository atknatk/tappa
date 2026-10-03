package tenant

// panelbrand_db_test.go -- M10 WL-8's brand read for the panel chrome
// (BrandReader.PanelBrand) against real Postgres, as tappa_app, on the production path
// (WithTenant + the statement's own tenant predicate). What a fake database would agree
// with anyway is not measured here: what is, is that the read is ONE statement, that
// it finds only its own business's name and brand, and that the unbranded shapes the
// table can hold (no row, an all-NULL row, an accent the gate refuses) come back as the
// zero value. The seam tests at the bottom need no database.
//
// Fixtures stay in the dev database for brand_db_test.go's reason (no DELETE on
// tenant_branding for tappa_app; audit_log is append-only).

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// statementCountingDB is the dev pool with every transaction's statements counted.
type statementCountingDB struct {
	inner        *db.DB
	transactions atomic.Int32
	statements   atomic.Int32
}

func (c *statementCountingDB) WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error {
	c.transactions.Add(1)
	return c.inner.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return fn(ctx, statementCountingTx{Tx: tx, n: &c.statements})
	})
}

// statementCountingTx counts the three calls a sqlc query makes.
type statementCountingTx struct {
	pgx.Tx
	n *atomic.Int32
}

func (c statementCountingTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	c.n.Add(1)
	return c.Tx.Exec(ctx, sql, args...)
}

func (c statementCountingTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	c.n.Add(1)
	return c.Tx.Query(ctx, sql, args...)
}

func (c statementCountingTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	c.n.Add(1)
	return c.Tx.QueryRow(ctx, sql, args...)
}

// rename gives a fixture business a name of its own (the fixture's are all alike).
func (f *brandFixture) rename(t *testing.T, tenantID uuid.UUID, name string) {
	t.Helper()
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE tenants SET name = $2 WHERE id = $1`, tenantID, name)
		return e
	})
	if err != nil {
		t.Fatalf("rename the fixture business: %v", err)
	}
}

// storeAccentAsIs writes an accent the way an older palette's gate could have let it
// in: past the write side's brand.Check, straight into the column (whose CHECK holds
// the spelling only).
func (f *brandFixture) storeAccentAsIs(t *testing.T, tenantID, actorID uuid.UUID, hex string) {
	t.Helper()
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if err := q.EnsureTenantBrand(ctx, store.EnsureTenantBrandParams{TenantID: tenantID, UpdatedBy: actorID}); err != nil {
			return err
		}
		_, err := q.SetTenantAccent(ctx, store.SetTenantAccentParams{TenantID: tenantID, Accent: hex, UpdatedBy: actorID})
		return err
	})
	if err != nil {
		t.Fatalf("store the accent %s as is: %v", hex, err)
	}
}

// TestBrandReadDB_PanelBrandIsOneStatementForItsOwnBusiness: two businesses, each with
// its own name, accent and logo. Each read returns its own business's name, accent and
// logo (digest, type, box), and opens ONE transaction with ONE statement in it -- the
// chrome's "+1 read". Neither business's read returns the other's name.
func TestBrandReadDB_PanelBrandIsOneStatementForItsOwnBusiness(t *testing.T) {
	f := newBrandFixture(t)
	ctx := context.Background()
	f.rename(t, f.tenant, "wl8 own business")
	f.rename(t, f.foreign, "wl8 other business")
	mine, theirs := f.pngLogo(t, 64, 16, 21), f.jpegLogo(t, 30, 40, 22)
	for _, c := range []struct {
		tenant, actor uuid.UUID
		accent        string
	}{{f.tenant, f.owner, "DA291C"}, {f.foreign, f.foreignOwner, "FFC72C"}} {
		if err := f.brands.SaveAccent(ctx, BrandAccentCommand{TenantID: c.tenant, ActorID: c.actor, Accent: mustColour(t, c.accent)}); err != nil {
			t.Fatalf("save accent: %v", err)
		}
	}
	if err := f.brands.SaveLogo(ctx, BrandLogoCommand{TenantID: f.tenant, ActorID: f.owner, Logo: mine}); err != nil {
		t.Fatalf("save this business's logo: %v", err)
	}
	if err := f.brands.SaveLogo(ctx, BrandLogoCommand{TenantID: f.foreign, ActorID: f.foreignOwner, Logo: theirs}); err != nil {
		t.Fatalf("save the other business's logo: %v", err)
	}

	for _, c := range []struct {
		tenant        uuid.UUID
		name, accent  string
		sha, mime     string
		width, height int
		otherName     string
	}{
		{f.tenant, "wl8 own business", "DA291C", mine.SHA256, "image/png", mine.Width, mine.Height, "wl8 other business"},
		{f.foreign, "wl8 other business", "FFC72C", theirs.SHA256, "image/jpeg", theirs.Width, theirs.Height, "wl8 own business"},
	} {
		counted := &statementCountingDB{inner: f.data}
		got, err := newBrandReaderOn(t, counted).PanelBrand(ctx, c.tenant)
		if err != nil {
			t.Fatalf("PanelBrand: %v", err)
		}
		want := PanelBrand{
			Name: c.name, Accent: mustColour(t, c.accent), HasAccent: true,
			Logo: LogoRef{SHA256: c.sha, MIME: c.mime, Width: c.width, Height: c.height}, HasLogo: true,
		}
		if got != want {
			t.Errorf("PanelBrand = %+v, want %+v", got, want)
		}
		if got.Name == c.otherName {
			t.Errorf("the read returned the other business's name")
		}
		if tx, st := counted.transactions.Load(), counted.statements.Load(); tx != 1 || st != 1 {
			t.Errorf("one read opened %d transaction(s) with %d statement(s); want 1 and 1", tx, st)
		}
	}
}

// TestBrandReadDB_PanelBrandUnbrandedShapesAreTheZeroValue walks one business through
// the shapes the table can hold: no row and an all-NULL row read as the zero value; a
// legible accent alone is branded, with the name; an accent stored as is that the gate
// refuses today reads as AccentRefused and NOT branded (no name); the same accent with
// a logo is the logo, the name, and AccentRefused.
func TestBrandReadDB_PanelBrandUnbrandedShapesAreTheZeroValue(t *testing.T) {
	f := newBrandFixture(t)
	ctx := context.Background()
	r := newBrandReaderOn(t, f.data)
	f.rename(t, f.tenant, "wl8 shapes")
	read := func(what string) PanelBrand {
		t.Helper()
		b, err := r.PanelBrand(ctx, f.tenant)
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		return b
	}

	if f.row(t, f.tenant).exists {
		t.Fatal("fixture: the fresh business already has a brand row")
	}
	if got := read("no brand row"); got != (PanelBrand{}) {
		t.Errorf("no brand row = %+v, want the zero value", got)
	}
	if err := f.brands.ClearLogo(ctx, BrandClearCommand{TenantID: f.tenant, ActorID: f.owner}); err != nil {
		t.Fatalf("clear without a row: %v", err)
	}
	if row := f.row(t, f.tenant); !row.exists || row.accent != "NULL" || row.sha != "NULL" {
		t.Fatalf("fixture: the cleared row is not all-NULL: %+v", row)
	}
	if got := read("an all-NULL row"); got != (PanelBrand{}) {
		t.Errorf("an all-NULL row = %+v, want the zero value", got)
	}

	if err := f.brands.SaveAccent(ctx, BrandAccentCommand{TenantID: f.tenant, ActorID: f.owner, Accent: mustColour(t, "1F5C41")}); err != nil {
		t.Fatalf("save accent: %v", err)
	}
	if got, want := read("a legible accent"), (PanelBrand{Name: "wl8 shapes", Accent: mustColour(t, "1F5C41"), HasAccent: true}); got != want {
		t.Errorf("a legible accent = %+v, want %+v", got, want)
	}

	// 808080: neither paper nor ink reaches 4.5:1 (ADR 0023 §3's table), so the
	// write side refuses it; stored as is, it stands for an accent the gate passed
	// under another palette.
	if err := f.brands.SaveAccent(ctx, BrandAccentCommand{TenantID: f.tenant, ActorID: f.owner, Accent: mustColour(t, "808080")}); err == nil {
		t.Fatal("PREMISE: the write side accepted 808080")
	}
	f.storeAccentAsIs(t, f.tenant, f.owner, "808080")
	if got, want := read("an accent the gate refuses"), (PanelBrand{AccentRefused: true}); got != want {
		t.Errorf("an accent the gate refuses = %+v, want %+v (not branded, no name)", got, want)
	}

	logo := f.pngLogo(t, 40, 20, 9)
	if err := f.brands.SaveLogo(ctx, BrandLogoCommand{TenantID: f.tenant, ActorID: f.owner, Logo: logo}); err != nil {
		t.Fatalf("save logo: %v", err)
	}
	want := PanelBrand{
		Name: "wl8 shapes", AccentRefused: true,
		Logo: LogoRef{SHA256: logo.SHA256, MIME: "image/png", Width: logo.Width, Height: logo.Height}, HasLogo: true,
	}
	if got := read("a refused accent and a logo"); got != want {
		t.Errorf("a refused accent and a logo = %+v, want %+v", got, want)
	}
	// And the other business, which has no row, is still unbranded.
	if got, err := r.PanelBrand(ctx, f.foreign); err != nil || got != (PanelBrand{}) {
		t.Errorf("the other business = %+v, %v; want the zero value", got, err)
	}
}

// TestBrandRead_PanelBrandOfKeepsEveryNonBrandAnError is panelBrandOf at the seam,
// without a database: GetTenantPanelBrand's "no brand" spellings (no row; every brand
// column NULL) are the zero value even with the name in the row; a read error, each of
// the fourteen partial combinations of the four logo columns (with a legible accent
// beside them, so the accent cannot hide them) and each malformed stored accent are
// errors; a legible accent is branded with the name; a refused one is AccentRefused and
// unbranded, and beside a full logo it is the logo, the name and AccentRefused.
func TestBrandRead_PanelBrandOfKeepsEveryNonBrandAnError(t *testing.T) {
	name, legible, refused := "wl8 seam", "DA291C", "808080"
	sha, mime := "ab", "image/png"
	w, h := int32(4), int32(3)

	if b, err := panelBrandOf(store.GetTenantPanelBrandRow{}, pgx.ErrNoRows); err != nil || b != (PanelBrand{}) {
		t.Errorf("no row = %+v, %v; want the zero value", b, err)
	}
	if b, err := panelBrandOf(store.GetTenantPanelBrandRow{Name: name}, nil); err != nil || b != (PanelBrand{}) {
		t.Errorf("an all-NULL row = %+v, %v; want the zero value (no name)", b, err)
	}
	boom := errors.New("connection reset")
	if b, err := panelBrandOf(store.GetTenantPanelBrandRow{Name: name}, boom); !errors.Is(err, boom) || b != (PanelBrand{}) {
		t.Errorf("a read error = %+v, %v; want the error", b, err)
	}

	partial := 0
	for mask := 1; mask < 15; mask++ {
		row := store.GetTenantPanelBrandRow{Name: name, Accent: &legible}
		if mask&1 != 0 {
			row.LogoSha256 = &sha
		}
		if mask&2 != 0 {
			row.LogoMime = &mime
		}
		if mask&4 != 0 {
			row.LogoWidth = &w
		}
		if mask&8 != 0 {
			row.LogoHeight = &h
		}
		if b, err := panelBrandOf(row, nil); err == nil || b != (PanelBrand{}) {
			t.Errorf("logo columns %04b = %+v, %v; want an error and nothing drawn", mask, b, err)
		}
		partial++
	}
	if partial != 14 {
		t.Fatalf("drove %d partial rows, want 14", partial)
	}

	for _, bad := range []string{"1f5c41", "ZZZZZZ", "1F5C4", "#1F5C4", ""} {
		bad := bad
		if b, err := panelBrandOf(store.GetTenantPanelBrandRow{Name: name, Accent: &bad}, nil); err == nil || b != (PanelBrand{}) {
			t.Errorf("stored accent %q = %+v, %v; want an error", bad, b, err)
		}
	}

	accent := mustColour(t, legible)
	if b, err := panelBrandOf(store.GetTenantPanelBrandRow{Name: name, Accent: &legible}, nil); err != nil ||
		b != (PanelBrand{Name: name, Accent: accent, HasAccent: true}) {
		t.Errorf("a legible accent = %+v, %v", b, err)
	}
	if b, err := panelBrandOf(store.GetTenantPanelBrandRow{Name: name, Accent: &refused}, nil); err != nil ||
		b != (PanelBrand{AccentRefused: true}) || b.Branded() {
		t.Errorf("a refused accent = %+v, %v; want AccentRefused alone, unbranded", b, err)
	}
	full := store.GetTenantPanelBrandRow{Name: name, Accent: &refused, LogoSha256: &sha, LogoMime: &mime, LogoWidth: &w, LogoHeight: &h}
	if b, err := panelBrandOf(full, nil); err != nil ||
		b != (PanelBrand{Name: name, AccentRefused: true, Logo: LogoRef{SHA256: sha, MIME: mime, Width: 4, Height: 3}, HasLogo: true}) {
		t.Errorf("a refused accent and a logo = %+v, %v", b, err)
	}
}

// TestBrandRead_PanelBrandNeedsATenantAndReportsAFailingDatabase: a nil tenant is
// refused before a transaction is opened; a failing database is an error, never the
// zero value.
func TestBrandRead_PanelBrandNeedsATenantAndReportsAFailingDatabase(t *testing.T) {
	d := &refusingDB{}
	r := newBrandReaderOn(t, d)
	if _, err := r.PanelBrand(context.Background(), uuid.Nil); err == nil {
		t.Fatal("PanelBrand with a nil tenant returned no error")
	}
	if n := d.calls.Load(); n != 0 {
		t.Fatalf("a nil tenant opened %d transaction(s), want 0", n)
	}
	if b, err := r.PanelBrand(context.Background(), uuid.New()); err == nil || b != (PanelBrand{}) {
		t.Fatalf("a failing database = %+v, %v; want its error", b, err)
	}
}

// TestBrandRead_TheBeltSeesThePanelRead pins where the panel read's section 4.5 belt
// comes from: TestStaffQueries_CarryAnExplicitTenantPredicate derives its list from
// this package's store calls, so GetTenantPanelBrand is under it while this package
// makes the call.
func TestBrandRead_TheBeltSeesThePanelRead(t *testing.T) {
	if names := storeQueryNames(t, declaredQueries(t)); !slices.Contains(names, "GetTenantPanelBrand") {
		t.Fatalf("this package's derived store calls %v do not include GetTenantPanelBrand", names)
	}
}
