package tenant

// pagebrand_db_test.go -- M10 WL-9's brand reads for the tap and result screens: TapPage
// reading the brand in its own transaction, ResultBrand, and the seam pageBrandOf.
// The DB tests run against the dev Postgres as tappa_app through the production path
// (WithTenant + each query's tenant predicate); fixtures are not cleaned up, for
// brand_db_test.go's reason. The seam tests need no database.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/brand"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// wall gives tenantID a location and an employee there, and returns both ids.
func (f *brandFixture) wall(t *testing.T, tenantID uuid.UUID, venue string) (location, employee uuid.UUID) {
	t.Helper()
	location, employee = uuid.New(), uuid.New()
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name, gps_lat, gps_lng) VALUES ($1, $2, $3, 35.918, 14.489)`,
			location, tenantID, venue); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at)
			 VALUES ($1, $2, $3, 'Maria Borg', 'active', now())`,
			employee, tenantID, location)
		return e
	})
	if err != nil {
		t.Fatalf("fixture wall: %v", err)
	}
	return location, employee
}

// TestTapPageDB_TheBrandComesWithTheGreetingAndTheVenue: TapPage returns the session's
// business's brand with the two strings -- nothing when there is no brand row, the
// accent alone, the logo (digest, type, box) with the business's name for its alt,
// and nothing again after both are cleared -- and the ZERO brand, with
// ErrForeignLocation, for another business's wall and for no wall, even when both
// businesses have a brand.
func TestTapPageDB_TheBrandComesWithTheGreetingAndTheVenue(t *testing.T) {
	f := newBrandFixture(t)
	ctx := context.Background()
	dir, err := NewDirectory(f.data)
	if err != nil {
		t.Fatalf("NewDirectory: %v", err)
	}
	loc, emp := f.wall(t, f.tenant, "St Julians")
	foreignLoc, _ := f.wall(t, f.foreign, "Elsewhere")

	read := func(what string, location uuid.UUID) (TapPageFacts, error) {
		t.Helper()
		facts, err := dir.TapPage(ctx, f.tenant, emp, location)
		if facts.EmployeeName != "Maria Borg" {
			t.Fatalf("%s: greeting %q", what, facts.EmployeeName)
		}
		return facts, err
	}
	if facts, err := read("no brand row", loc); err != nil || facts.Brand != (PageBrand{}) || facts.LocationName != "St Julians" {
		t.Fatalf("no brand row: %+v, %v", facts, err)
	}

	accent := mustColour(t, "DA291C")
	if err := f.brands.SaveAccent(ctx, BrandAccentCommand{TenantID: f.tenant, ActorID: f.owner, Accent: accent}); err != nil {
		t.Fatalf("save accent: %v", err)
	}
	if facts, err := read("an accent", loc); err != nil || facts.Brand != (PageBrand{Accent: accent, HasAccent: true}) {
		t.Fatalf("an accent: %+v, %v", facts.Brand, err)
	}

	logo := f.pngLogo(t, 64, 16, 9)
	if err := f.brands.SaveLogo(ctx, BrandLogoCommand{TenantID: f.tenant, ActorID: f.owner, Logo: logo}); err != nil {
		t.Fatalf("save logo: %v", err)
	}
	want := PageBrand{Name: "brand-domain-fixture", HasLogo: true, Accent: accent, HasAccent: true,
		Logo: LogoRef{SHA256: logo.SHA256, MIME: "image/png", Width: logo.Width, Height: logo.Height}}
	if facts, err := read("a logo and an accent", loc); err != nil || facts.Brand != want {
		t.Fatalf("a logo and an accent: %+v, %v\nwant %+v", facts.Brand, err, want)
	}

	// The other business brands itself too; neither brand reaches a foreign wall.
	if err := f.brands.SaveLogo(ctx, BrandLogoCommand{TenantID: f.foreign, ActorID: f.foreignOwner, Logo: f.jpegLogo(t, 32, 32, 4)}); err != nil {
		t.Fatalf("save the other logo: %v", err)
	}
	for _, c := range []struct {
		what     string
		location uuid.UUID
	}{{"another business's wall", foreignLoc}, {"no wall", uuid.Nil}} {
		facts, err := read(c.what, c.location)
		if !errors.Is(err, ErrForeignLocation) || facts.Brand != (PageBrand{}) || facts.LocationName != "" {
			t.Fatalf("%s: %+v, %v; want the zero brand with ErrForeignLocation", c.what, facts, err)
		}
	}

	if err := f.brands.ClearLogo(ctx, BrandClearCommand{TenantID: f.tenant, ActorID: f.owner}); err != nil {
		t.Fatalf("clear logo: %v", err)
	}
	if err := f.brands.ClearAccent(ctx, BrandClearCommand{TenantID: f.tenant, ActorID: f.owner}); err != nil {
		t.Fatalf("clear accent: %v", err)
	}
	if facts, err := read("both cleared", loc); err != nil || facts.Brand != (PageBrand{}) {
		t.Fatalf("both cleared: %+v, %v", facts.Brand, err)
	}
}

// brandAbortingDB runs the real transaction, but the brand read executes a statement
// that FAILS on the real connection -- so the transaction is really aborted, the way a
// database error leaves it -- and returns that failure as the brand row.
type brandAbortingDB struct{ real *db.DB }

func (d brandAbortingDB) WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error {
	return d.real.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return fn(ctx, brandAbortingTx{tx})
	})
}

type brandAbortingTx struct{ pgx.Tx }

func (tx brandAbortingTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.HasPrefix(sql, "-- name: GetTenantBrand :one") {
		return tx.Tx.QueryRow(ctx, "SELECT 1/0")
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

// brandSleepingDB runs the real transaction, but the brand read sleeps on the real
// connection instead of reading -- a stalled statement, produced without DDL: one
// pg_sleep in place of the query, so only the caller's context can end it early.
type brandSleepingDB struct{ real *db.DB }

func (d brandSleepingDB) WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error {
	return d.real.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return fn(ctx, brandSleepingTx{tx})
	})
}

type brandSleepingTx struct{ pgx.Tx }

func (tx brandSleepingTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.HasPrefix(sql, "-- name: GetTenantBrand :one") {
		return tx.Tx.QueryRow(ctx, "SELECT pg_sleep(3)")
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

// TestResultBrandDB_TheReadEndsWhenItsContextDoes is N1 of WL-9's round 3: the
// confirmation screen's bound (internal/handler, resultBrandWait) is a context, so it
// holds only if ResultBrand hands that context to the database. With the brand
// statement sleeping 3 s on the real connection and a 300 ms context, the read returns
// ErrBrandUnread wrapping context.DeadlineExceeded, the zero brand, and within the
// deadline plus 1.5 s. A read that ran under a context of its own would sleep the 3 s.
func TestResultBrandDB_TheReadEndsWhenItsContextDoes(t *testing.T) {
	f := newBrandFixture(t)
	dir, err := NewDirectory(brandSleepingDB{f.data})
	if err != nil {
		t.Fatalf("NewDirectory: %v", err)
	}
	const deadline = 300 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	start := time.Now()
	b, err := dir.ResultBrand(ctx, f.tenant)
	took := time.Since(start)
	if !errors.Is(err, ErrBrandUnread) || !errors.Is(err, context.DeadlineExceeded) || b != (PageBrand{}) {
		t.Fatalf("brand %+v, err %v; want the zero brand and ErrBrandUnread wrapping context.DeadlineExceeded", b, err)
	}
	if took > deadline+1500*time.Millisecond {
		t.Fatalf("the read took %v under a %v context: it did not end with its context", took.Round(time.Millisecond), deadline)
	}
	// CONTROL: the same directory and a context with room returns after the sleep -- the
	// statement really sleeps, so the bound above was the context's doing.
	start = time.Now()
	_, err = dir.ResultBrand(context.Background(), f.tenant)
	if took := time.Since(start); err == nil || errors.Is(err, context.DeadlineExceeded) || took < 3*time.Second {
		t.Fatalf("control: err %v after %v; want a non-timeout failure after the 3 s sleep", err, took.Round(time.Millisecond))
	}
}

// TestTapPageDB_ABrandReadFailureKeepsTheGreetingAndTheVenue is §4.6 at the read: the
// brand query fails on the real connection (the transaction is aborted, so committing
// it would fail too), and TapPage still returns the greeting and the venue -- with
// the zero brand and ErrBrandUnread wrapping the database's own error.
func TestTapPageDB_ABrandReadFailureKeepsTheGreetingAndTheVenue(t *testing.T) {
	f := newBrandFixture(t)
	loc, emp := f.wall(t, f.tenant, "St Julians")
	if err := f.brands.SaveAccent(context.Background(), BrandAccentCommand{TenantID: f.tenant, ActorID: f.owner, Accent: mustColour(t, "DA291C")}); err != nil {
		t.Fatalf("save accent: %v", err)
	}
	dir, err := NewDirectory(brandAbortingDB{f.data})
	if err != nil {
		t.Fatalf("NewDirectory: %v", err)
	}
	facts, err := dir.TapPage(context.Background(), f.tenant, emp, loc)
	var pgErr *pgconn.PgError
	if !errors.Is(err, ErrBrandUnread) || !errors.As(err, &pgErr) || pgErr.Code != "22012" {
		t.Fatalf("err = %v; want ErrBrandUnread wrapping the database's 22012", err)
	}
	if facts.EmployeeName != "Maria Borg" || facts.LocationName != "St Julians" || facts.Brand != (PageBrand{}) {
		t.Fatalf("facts = %+v; want the greeting and the venue with the zero brand", facts)
	}
	// CONTROL: the same database without the failure gives the brand.
	ok, _ := NewDirectory(f.data)
	if facts, err := ok.TapPage(context.Background(), f.tenant, emp, loc); err != nil || !facts.Brand.HasAccent {
		t.Fatalf("control: %+v, %v", facts, err)
	}
}

// TestResultBrandDB_TheLogoAndTheNameAndNeverTheAccent: the confirmation screen's read
// gives the logo and the business's name, never the accent (D-C); a business with an
// accent and no logo gets nothing; each business reads only its own.
func TestResultBrandDB_TheLogoAndTheNameAndNeverTheAccent(t *testing.T) {
	f := newBrandFixture(t)
	ctx := context.Background()
	dir, err := NewDirectory(f.data)
	if err != nil {
		t.Fatalf("NewDirectory: %v", err)
	}
	if b, err := dir.ResultBrand(ctx, f.tenant); err != nil || b != (PageBrand{}) {
		t.Fatalf("no brand row: %+v, %v", b, err)
	}
	if err := f.brands.SaveAccent(ctx, BrandAccentCommand{TenantID: f.tenant, ActorID: f.owner, Accent: mustColour(t, "FFC72C")}); err != nil {
		t.Fatalf("save accent: %v", err)
	}
	if b, err := dir.ResultBrand(ctx, f.tenant); err != nil || b != (PageBrand{}) {
		t.Fatalf("an accent and no logo: %+v, %v", b, err)
	}
	logo := f.jpegLogo(t, 48, 48, 3)
	if err := f.brands.SaveLogo(ctx, BrandLogoCommand{TenantID: f.tenant, ActorID: f.owner, Logo: logo}); err != nil {
		t.Fatalf("save logo: %v", err)
	}
	want := PageBrand{Name: "brand-domain-fixture", HasLogo: true,
		Logo: LogoRef{SHA256: logo.SHA256, MIME: "image/jpeg", Width: logo.Width, Height: logo.Height}}
	if b, err := dir.ResultBrand(ctx, f.tenant); err != nil || b != want {
		t.Fatalf("a logo and an accent: %+v, %v\nwant %+v", b, err, want)
	}
	if b, err := dir.ResultBrand(ctx, f.foreign); err != nil || b != (PageBrand{}) {
		t.Fatalf("the other business: %+v, %v", b, err)
	}
	if _, err := dir.ResultBrand(ctx, uuid.Nil); err == nil {
		t.Fatal("a nil tenant was read")
	}
}

// TestPageBrand_EveryReadFailureIsAnErrorNeverNoBrand is the seam, without a database:
// no row and an all-NULL row are the zero brand; a database error, each of the
// fourteen partly described logo rows and a non-canonical stored accent are ERRORS --
// none of them ErrLogoNotFound, none of them a quiet "no brand" (ADR 0024's WL-6
// hand-off: half a logo row is a read error). An accent the gate refuses TODAY is not
// an error (accentOf, the panel chrome's read-side gate since the merge with WL-8): no
// accent, AccentRefused, and the logo beside it is kept.
func TestPageBrand_EveryReadFailureIsAnErrorNeverNoBrand(t *testing.T) {
	if b, err := pageBrandOf(store.GetTenantBrandRow{}, pgx.ErrNoRows); err != nil || b != (PageBrand{}) {
		t.Fatalf("no row: %+v, %v", b, err)
	}
	if b, err := pageBrandOf(store.GetTenantBrandRow{UpdatedBy: uuid.New()}, nil); err != nil || b != (PageBrand{}) {
		t.Fatalf("all-NULL row: %+v, %v", b, err)
	}
	sha, mime := "ab", "image/png"
	w, h := int32(4), int32(3)
	refuse := func(what string, row store.GetTenantBrandRow, rowErr error) {
		t.Helper()
		b, err := pageBrandOf(row, rowErr)
		if err == nil || errors.Is(err, ErrLogoNotFound) || b != (PageBrand{}) {
			t.Errorf("%s: (%+v, %v); want the zero brand and an error that is not ErrLogoNotFound", what, b, err)
		}
	}
	refuse("a database error", store.GetTenantBrandRow{}, errors.New("connection reset"))
	partial := 0
	for mask := 1; mask < 15; mask++ {
		var row store.GetTenantBrandRow
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
		partial++
		refuse("a partly described logo", row, nil)
	}
	if partial != 14 {
		t.Fatalf("drove %d partial rows, want 14", partial)
	}
	for _, a := range []string{"da291c", "#DA291C", "DA291", "DA291CC"} {
		accent := a
		refuse("stored accent "+a, store.GetTenantBrandRow{Accent: &accent}, nil)
	}
	for _, a := range []string{"808080", "E0457B"} {
		accent := a
		b, err := pageBrandOf(store.GetTenantBrandRow{Accent: &accent, LogoSha256: &sha, LogoMime: &mime, LogoWidth: &w, LogoHeight: &h}, nil)
		want := PageBrand{AccentRefused: true, HasLogo: true, Logo: LogoRef{SHA256: sha, MIME: mime, Width: 4, Height: 3}}
		if err != nil || b != want {
			t.Errorf("an accent the gate refuses today (%s) beside a logo: (%+v, %v); want no accent, AccentRefused and the logo", a, b, err)
		}
	}
	good := "DA291C"
	b, err := pageBrandOf(store.GetTenantBrandRow{Accent: &good, LogoSha256: &sha, LogoMime: &mime, LogoWidth: &w, LogoHeight: &h}, nil)
	if err != nil || !b.HasAccent || b.Accent != (brand.Color{R: 0xDA, G: 0x29, B: 0x1C}) || !b.HasLogo || b.Name != "" {
		t.Fatalf("control: %+v, %v", b, err)
	}
}
