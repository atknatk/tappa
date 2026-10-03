package handler

// brandlogo_db_test.go -- M10 WL-6's two logo routes over the REAL reader
// (tenant.BrandReader) and the dev Postgres, as tappa_app. It is the production path end
// to end below the session: the logos are stored through the product's own writer
// (tenant.Brands, from brand.LogoGate output), read back through WithTenant and the
// query's own tenant predicate, and served by the routes. Only the session is a fake
// -- it names the business, which is the one thing a route takes from it.
//
// What a fake reader cannot show and this does: that another business's digest and a
// digest nobody stored are the SAME answer when the database decides it (ADR 0024 §5,
// Iddia D). Fixtures are not cleaned up: tappa_app holds no DELETE on tenant_branding
// (00028) and audit_log is append-only (00005); fresh random ids keep runs apart.

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/brand"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/test/fixtures"
)

// logoDBBusiness writes a tenant and one active owner, and returns both ids.
func logoDBBusiness(t *testing.T, data *db.DB) (tenantID, ownerID uuid.UUID) {
	t.Helper()
	tenantID, ownerID = uuid.New(), uuid.New()
	err := data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'wl6-logo-route-fixture', $2, 'bar', 'single')`,
			tenantID, "VAT-"+tenantID.String()); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
			 VALUES ($1, $2, 'wl6-owner', $3, $4, 'owner', 'active')`,
			ownerID, tenantID, "wl6-"+uuid.NewString()+"@iso.example", fixtures.UnusablePasswordHash)
		return err
	})
	if err != nil {
		t.Fatalf("fixture business: %v", err)
	}
	return tenantID, ownerID
}

// logoDBPicture encodes a small generated picture and runs it through the logo gate,
// so what is stored is what the product stores.
func logoDBPicture(t *testing.T, gate *brand.LogoGate, asJPEG bool, seed uint8) brand.Logo {
	t.Helper()
	m := image.NewNRGBA(image.Rect(0, 0, 36, 24))
	for y := range 24 {
		for x := range 36 {
			m.SetNRGBA(x, y, color.NRGBA{R: uint8(7*x) + seed, G: uint8(3*y) ^ seed, B: seed, A: 255})
		}
	}
	var buf bytes.Buffer
	var err error
	if asJPEG {
		err = jpeg.Encode(&buf, m, &jpeg.Options{Quality: 90})
	} else {
		err = png.Encode(&buf, m)
	}
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	l, err := gate.Normalize(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	return l
}

func TestLogoRoutesDB_AnotherBusinessesDigestIsAnUnknownDigest(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping the logo routes over real Postgres")
	}
	ctx := context.Background()
	data, err := db.New(ctx, &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)
	trail, err := audit.New(data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	brands, err := tenant.NewBrands(data, trail, discardLogger())
	if err != nil {
		t.Fatalf("NewBrands: %v", err)
	}
	gate, err := brand.NewLogoGate(brand.LogoDecodeSlots)
	if err != nil {
		t.Fatalf("NewLogoGate: %v", err)
	}
	reader, err := tenant.NewBrandReader(data)
	if err != nil {
		t.Fatalf("NewBrandReader: %v", err)
	}

	a, ownerA := logoDBBusiness(t, data)
	b, ownerB := logoDBBusiness(t, data)
	logoA := logoDBPicture(t, gate, false, 21)
	logoB := logoDBPicture(t, gate, true, 42)
	if err := brands.SaveLogo(ctx, tenant.BrandLogoCommand{TenantID: a, ActorID: ownerA, Logo: logoA}); err != nil {
		t.Fatalf("store A's logo: %v", err)
	}
	if err := brands.SaveLogo(ctx, tenant.BrandLogoCommand{TenantID: b, ActorID: ownerB, Logo: logoB}); err != nil {
		t.Fatalf("store B's logo: %v", err)
	}

	asA := logoRouter(t, reader, liveEmployee(a), livePanel(a))
	asB := logoRouter(t, reader, liveEmployee(b), livePanel(b))
	for _, route := range []struct {
		name   string
		path   func(string) string
		cookie *http.Cookie
	}{
		{"GET /t/logo", tapLogoPath, employeeCookie()},
		{"GET /admin/brand/logo", panelLogoPath, panelCookie()},
	} {
		// A's own logo: the stored bytes, under exactly the ADR's headers.
		want := answer{http.StatusOK, wantLogoHeader(logoA.SHA256, "image/png", "logo.png", len(logoA.Data)), logoA.Data}
		if got := ask(t, asA, http.MethodGet, route.path(logoA.SHA256), route.cookie); !got.same(want) {
			t.Fatalf("%s, A's own digest:\n got %s\nwant %s", route.name, got, want)
		}
		// POSITIVE CONTROL: B's digest is a real stored logo -- B's session gets it.
		wantB := answer{http.StatusOK, wantLogoHeader(logoB.SHA256, "image/jpeg", "logo.jpg", len(logoB.Data)), logoB.Data}
		if got := ask(t, asB, http.MethodGet, route.path(logoB.SHA256), route.cookie); !got.same(wantB) {
			t.Fatalf("control: %s, B's own digest:\n got %s\nwant %s", route.name, got, wantB)
		}

		// A's session asking for B's digest, and for a digest nobody stored: one answer,
		// byte for byte, and it is the routes' one refusal.
		foreign := ask(t, asA, http.MethodGet, route.path(logoB.SHA256), route.cookie)
		unknown := ask(t, asA, http.MethodGet, route.path(unknownDigest), route.cookie)
		if !foreign.same(unknown) {
			t.Errorf("%s: A asking for B's digest and for an unknown digest differ:\n foreign %s\n unknown %s",
				route.name, foreign, unknown)
		}
		if !foreign.same(wantNotFound()) {
			t.Errorf("%s: A asking for B's digest = %s, want the one 404", route.name, foreign)
		}
	}
}
