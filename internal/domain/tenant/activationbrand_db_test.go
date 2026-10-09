package tenant

// activationbrand_db_test.go -- M10 WL-13's activation brand reads: the VIES gate
// (user decision 2 of 2026-10-09: the logo only for a business VIES verified). The first
// test needs no database: the seam where GetTenantActivationBrand's row becomes the
// page's brand. The second runs the production read path against real Postgres as
// tappa_app, with the verdict in each of its three stored states (TRUE, FALSE, NULL).
// Fixtures are not cleaned up, for brand_db_test.go's reason.

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/brand"
	"github.com/atknatk/tappa/internal/store"
)

// TestActivationBrandOf_TheLogoOnlyForAVerifiedBusiness: a logo row of a business VIES
// verified is the logo with the business's name (its alt); the SAME row with the verdict
// false is no logo and no name -- and the accent, the gate's other side, is the same in
// both. No row and an all-NULL row are the zero brand; a half-described logo and a
// non-canonical accent are errors whatever the verdict; an accent the legibility gate
// refuses today is AccentRefused, not an error.
func TestActivationBrandOf_TheLogoOnlyForAVerifiedBusiness(t *testing.T) {
	sha, mime := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", "image/png"
	w, h := int32(240), int32(60)
	legible, refused, bad := "DA291C", "808080", "da291c"
	row := func(verified bool, accent *string, logo bool) store.GetTenantActivationBrandRow {
		r := store.GetTenantActivationBrandRow{Name: "Kebab Factory Ltd", VatVerified: verified, Accent: accent}
		if logo {
			r.LogoSha256, r.LogoMime, r.LogoWidth, r.LogoHeight = &sha, &mime, &w, &h
		}
		return r
	}
	accent, err := brand.ParseAccent(legible)
	if err != nil {
		t.Fatal(err)
	}
	wantLogo := LogoRef{SHA256: sha, MIME: mime, Width: 240, Height: 60}

	for _, c := range []struct {
		name string
		row  store.GetTenantActivationBrandRow
		err  error
		want PageBrand
	}{
		{"verified, logo and accent", row(true, &legible, true), nil,
			PageBrand{Name: "Kebab Factory Ltd", Logo: wantLogo, HasLogo: true, Accent: accent, HasAccent: true}},
		{"not verified, logo and accent", row(false, &legible, true), nil, PageBrand{Accent: accent, HasAccent: true}},
		{"verified, logo alone", row(true, nil, true), nil, PageBrand{Name: "Kebab Factory Ltd", Logo: wantLogo, HasLogo: true}},
		{"not verified, logo alone", row(false, nil, true), nil, PageBrand{}},
		{"verified, accent alone", row(true, &legible, false), nil, PageBrand{Accent: accent, HasAccent: true}},
		{"not verified, accent alone", row(false, &legible, false), nil, PageBrand{Accent: accent, HasAccent: true}},
		{"verified, a refused accent beside the logo", row(true, &refused, true), nil,
			PageBrand{Name: "Kebab Factory Ltd", Logo: wantLogo, HasLogo: true, AccentRefused: true}},
		{"not verified, a refused accent beside the logo", row(false, &refused, true), nil, PageBrand{AccentRefused: true}},
		{"no brand row", store.GetTenantActivationBrandRow{}, pgx.ErrNoRows, PageBrand{}},
		{"an all-NULL row, verified", row(true, nil, false), nil, PageBrand{}},
	} {
		got, err := activationBrandOf(c.row, c.err)
		if err != nil || got != c.want {
			t.Errorf("%s: got (%+v, %v), want (%+v, nil)", c.name, got, err, c.want)
		}
	}

	boom := errors.New("connection reset")
	if got, err := activationBrandOf(store.GetTenantActivationBrandRow{}, boom); !errors.Is(err, boom) || got != (PageBrand{}) {
		t.Errorf("a read error: got (%+v, %v)", got, err)
	}
	for _, verified := range []bool{true, false} {
		half := row(verified, &legible, true)
		half.LogoMime = nil
		if got, err := activationBrandOf(half, nil); err == nil || got != (PageBrand{}) {
			t.Errorf("verified=%v, a half-described logo: got (%+v, %v), want an error", verified, got, err)
		}
		if got, err := activationBrandOf(row(verified, &bad, false), nil); err == nil || got != (PageBrand{}) {
			t.Errorf("verified=%v, a stored accent not in the canonical spelling: got (%+v, %v), want an error", verified, got, err)
		}
	}
}

// verdictBusiness writes a tenant with vat_verified in the given state (nil = NULL) and
// an active owner, and stores a logo and an accent for it through the product's writer.
func (f *brandFixture) verdictBusiness(t *testing.T, verified *bool, seed uint8) (uuid.UUID, brand.Logo) {
	t.Helper()
	id := uuid.New()
	err := f.data.WithTenant(context.Background(), id, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure, vat_verified)
			 VALUES ($1, 'wl13-verdict-fixture', $2, 'bar', 'single', $3)`,
			id, "VAT-"+id.String(), verified)
		return e
	})
	if err != nil {
		t.Fatalf("fixture tenant: %v", err)
	}
	owner := f.admin(t, id, "owner", "active")
	logo := f.pngLogo(t, 48, 32, seed)
	ctx := context.Background()
	if err := f.brands.SaveLogo(ctx, BrandLogoCommand{TenantID: id, ActorID: owner, Logo: logo}); err != nil {
		t.Fatalf("save logo: %v", err)
	}
	c, err := brand.ParseAccent("DA291C")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.brands.SaveAccent(ctx, BrandAccentCommand{TenantID: id, ActorID: owner, Accent: c}); err != nil {
		t.Fatalf("save accent: %v", err)
	}
	return id, logo
}

// TestActivationBrandDB_TheVIESGateHoldsOnBothReads: three businesses with the same
// brand shape and the verdict TRUE, FALSE and NULL. ActivationBrand draws the logo and
// the name only for the verified one and the accent for all three; ActivationLogo serves
// the bytes only for the verified one's own current digest -- the unverified ones' own
// digests, the verified one's request for another verified business's digest, and its
// own previous digest after a new logo are ErrLogoNotFound itself.
func TestActivationBrandDB_TheVIESGateHoldsOnBothReads(t *testing.T) {
	f := newBrandFixture(t)
	ctx := context.Background()
	r := newBrandReaderOn(t, f.data)
	yes, no := true, false
	verified, vLogo := f.verdictBusiness(t, &yes, 31)
	other, oLogo := f.verdictBusiness(t, &yes, 32)
	refused, rLogo := f.verdictBusiness(t, &no, 33)
	unasked, uLogo := f.verdictBusiness(t, nil, 34)
	accent, _ := brand.ParseAccent("DA291C")

	got, err := r.ActivationBrand(ctx, verified)
	if err != nil || !got.HasLogo || got.Logo.SHA256 != vLogo.SHA256 || got.Name != "wl13-verdict-fixture" ||
		!got.HasAccent || got.Accent != accent {
		t.Fatalf("verified: (%+v, %v), want its logo, its name and its accent", got, err)
	}
	for name, id := range map[string]uuid.UUID{"vat_verified false": refused, "vat_verified NULL": unasked} {
		got, err := r.ActivationBrand(ctx, id)
		if err != nil || got.HasLogo || got.Logo != (LogoRef{}) || got.Name != "" || !got.HasAccent || got.Accent != accent {
			t.Errorf("%s: (%+v, %v), want the accent and no logo, no name", name, got, err)
		}
	}
	// A business with no brand row at all: the zero brand.
	if got, err := r.ActivationBrand(ctx, f.business(t)); err != nil || got != (PageBrand{}) {
		t.Errorf("no brand row: (%+v, %v)", got, err)
	}

	// The bytes.
	if l, err := r.ActivationLogo(ctx, verified, vLogo.SHA256); err != nil || !slices.Equal(l.Data, vLogo.Data) || l.MIME != "image/png" {
		t.Fatalf("verified, own digest: (%d bytes %q, %v)", len(l.Data), l.MIME, err)
	}
	// POSITIVE CONTROL: the WL-6 read finds the unverified businesses' logos -- the rows
	// are there; only the gate stands between them and the wizard's route.
	for _, c := range []struct {
		id  uuid.UUID
		sha string
	}{{refused, rLogo.SHA256}, {unasked, uLogo.SHA256}} {
		if l, err := r.Logo(ctx, c.id, c.sha); err != nil || len(l.Data) == 0 {
			t.Fatalf("control: the WL-6 read of an unverified business's own logo: %v", err)
		}
	}
	for name, c := range map[string]struct {
		id  uuid.UUID
		sha string
	}{
		"vat_verified false, own digest":                {refused, rLogo.SHA256},
		"vat_verified NULL, own digest":                 {unasked, uLogo.SHA256},
		"verified, another verified business's digest":  {verified, oLogo.SHA256},
		"verified, an unverified business's digest":     {verified, rLogo.SHA256},
		"verified, a digest nobody stored":              {verified, unstoredDigest(t)},
		"another verified business, the first's digest": {other, vLogo.SHA256},
	} {
		l, err := r.ActivationLogo(ctx, c.id, c.sha)
		assertNotFound(t, name, l, err)
	}

	// A logo replaced: the old digest is gone, the new one served.
	owner := f.admin(t, verified, "owner", "active")
	next := f.pngLogo(t, 48, 32, 35)
	if err := f.brands.SaveLogo(ctx, BrandLogoCommand{TenantID: verified, ActorID: owner, Logo: next}); err != nil {
		t.Fatalf("replace logo: %v", err)
	}
	l, err := r.ActivationLogo(ctx, verified, vLogo.SHA256)
	assertNotFound(t, "verified, its previous digest", l, err)
	if l, err := r.ActivationLogo(ctx, verified, next.SHA256); err != nil || !slices.Equal(l.Data, next.Data) {
		t.Fatalf("verified, its new digest: %v", err)
	}
}

// TestActivationBrand_ANilTenantOpensNoTransaction: neither read opens a transaction for
// no business; and a failing database is an error -- for the logo, never the refusal
// value (an outage must not read as "this business has no such logo").
func TestActivationBrand_ANilTenantOpensNoTransaction(t *testing.T) {
	d := &refusingDB{}
	r := newBrandReaderOn(t, d)
	if _, err := r.ActivationBrand(context.Background(), uuid.Nil); err == nil {
		t.Error("ActivationBrand(uuid.Nil) returned no error")
	}
	if _, err := r.ActivationLogo(context.Background(), uuid.Nil, unstoredDigest(t)); err == nil || errors.Is(err, ErrLogoNotFound) {
		t.Errorf("ActivationLogo(uuid.Nil) = %v, want an error that is not the refusal", err)
	}
	if n := d.calls.Load(); n != 0 {
		t.Fatalf("a nil tenant opened %d transaction(s), want 0", n)
	}
	if b, err := r.ActivationBrand(context.Background(), uuid.New()); err == nil || b != (PageBrand{}) {
		t.Errorf("control: ActivationBrand over a failing database = (%+v, %v), want its error", b, err)
	}
	if _, err := r.ActivationLogo(context.Background(), uuid.New(), unstoredDigest(t)); err == nil || errors.Is(err, ErrLogoNotFound) {
		t.Errorf("control: ActivationLogo over a failing database = %v, want its error, not the refusal", err)
	}
	if n := d.calls.Load(); n != 2 {
		t.Fatalf("control: two reads for a real tenant opened %d transaction(s), want 2", n)
	}
}

// TestActivationBrand_TheBeltSeesBothReads pins where the two activation reads' section
// 4.5 belt comes from: TestStaffQueries_CarryAnExplicitTenantPredicate derives its list
// from this package's store calls, so both statements are under it while this package
// makes the calls.
func TestActivationBrand_TheBeltSeesBothReads(t *testing.T) {
	names := storeQueryNames(t, declaredQueries(t))
	for _, q := range []string{"GetTenantActivationBrand", "GetTenantActivationLogo"} {
		if !slices.Contains(names, q) {
			t.Errorf("this package's derived store calls %v do not include %s", names, q)
		}
	}
}
