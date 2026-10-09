package handler

// activationbrand_db_test.go -- M10 WL-13 end to end over the REAL invitation flow, the
// REAL brand reader and the dev Postgres, as tappa_app: an invitation is minted by
// invite.Manager, opened through the router (the code moves into the activation
// cookie), and the wizard and its logo route answer from what the database holds. What a
// fake reader cannot show and this does: the VIES gate is the DATABASE's answer --
// vat_verified TRUE, FALSE and NULL are three stored states, and only the first serves a
// logo -- and another business's digest is the same 404 as a digest nobody stored when
// the database decides it. Fixtures are not cleaned up (tappa_app holds no DELETE on the
// tables involved); fresh random ids keep runs apart.

import (
	"context"
	"encoding/hex"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/brand"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/atknatk/tappa/test/fixtures"
)

// wl13Business is one business of the test: its id, its stored logo, and a code for an
// invitation of one of its employees.
type wl13Business struct {
	id   uuid.UUID
	logo brand.Logo
	code string
}

func TestActivationLogoDB_OnlyAVerifiedBusinessesOwnLogoIsServed(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping the activation brand over real Postgres")
	}
	kek, err := hex.DecodeString(tapFakeKEK)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Env:            config.EnvDev,
		BaseURL:        "http://localhost:8080",
		DatabaseURL:    dsn,
		SessionHMACKey: []byte("SSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSS"),
		InviteHMACKey:  []byte("IIIIIIIIIIIIIIIIIIIIIIIIIIIIIIII"),
		TagKEK:         kek,
		RetentionYears: 2,
	}
	ctx := context.Background()
	data, err := db.New(ctx, cfg)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)
	trail, err := audit.New(data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	sessions, err := session.New(data, cfg)
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	invites, err := invite.New(data, cfg)
	if err != nil {
		t.Fatalf("invite.New: %v", err)
	}
	writer, err := tenant.NewBrands(data, trail, discardLogger())
	if err != nil {
		t.Fatalf("NewBrands: %v", err)
	}
	gate, err := brand.NewLogoGate(brand.LogoDecodeSlots)
	if err != nil {
		t.Fatalf("NewLogoGate: %v", err)
	}
	act, err := NewActivation(invites, sessions, sun.NewVerifier(data, kek), trail, dbBrandReader(t, data), cfg, discardLogger())
	if err != nil {
		t.Fatalf("NewActivation: %v", err)
	}
	r := chi.NewRouter()
	act.Mount(r)

	accent, err := brand.ParseAccent(brandTestAccent)
	if err != nil {
		t.Fatal(err)
	}
	business := func(verified *bool, seed uint8) wl13Business {
		t.Helper()
		b := wl13Business{id: uuid.New()}
		owner, location, employee := uuid.New(), uuid.New(), uuid.New()
		err := data.WithTenant(ctx, b.id, func(ctx context.Context, tx pgx.Tx) error {
			for _, q := range []struct {
				sql  string
				args []any
			}{
				{`INSERT INTO tenants (id, name, vat_number, business_type, structure, vat_verified)
				  VALUES ($1, 'wl13-activation-fixture', $2, 'restaurant', 'single', $3)`,
					[]any{b.id, "VAT-" + b.id.String(), verified}},
				{`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
				  VALUES ($1, $2, 'wl13-owner', $3, $4, 'owner', 'active')`,
					[]any{owner, b.id, "wl13-" + uuid.NewString() + "@iso.example", fixtures.UnusablePasswordHash}},
				{`INSERT INTO locations (id, tenant_id, name, static_ips, gps_lat, gps_lng)
				  VALUES ($1, $2, 'St Julians', '{203.0.113.0/24}', 35.918, 14.489)`,
					[]any{location, b.id}},
				{`INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at)
				  VALUES ($1, $2, $3, 'Maria Borg', 'invited', now())`,
					[]any{employee, b.id, location}},
			} {
				if _, e := tx.Exec(ctx, q.sql, q.args...); e != nil {
					return e
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("fixture business: %v", err)
		}
		b.logo = logoDBPicture(t, gate, false, seed)
		if err := writer.SaveLogo(ctx, tenant.BrandLogoCommand{TenantID: b.id, ActorID: owner, Logo: b.logo}); err != nil {
			t.Fatalf("save logo: %v", err)
		}
		if err := writer.SaveAccent(ctx, tenant.BrandAccentCommand{TenantID: b.id, ActorID: owner, Accent: accent}); err != nil {
			t.Fatalf("save accent: %v", err)
		}
		ch := &linkChannel{}
		if _, err := invites.IssueAndDeliver(ctx, invite.IssueParams{TenantID: b.id, EmployeeID: employee}, ch); err != nil {
			t.Fatalf("IssueAndDeliver: %v", err)
		}
		u, err := url.Parse(ch.url)
		if err != nil || u.Query().Get("code") == "" {
			t.Fatalf("the delivered link carries no code (%v)", err)
		}
		b.code = u.Query().Get("code")
		return b
	}
	// open is a browser opening its activation link: the 303 that moves the code into
	// the cookie, and the cookie it leaves.
	open := func(b wl13Business) *http.Cookie {
		t.Helper()
		rec := get(t, r, "/activate?code="+url.QueryEscape(b.code))
		c := cookieNamed(rec, activationCookieName)
		if rec.Code != http.StatusSeeOther || c == nil {
			t.Fatalf("opening the link = %d, cookie %v", rec.Code, c != nil)
		}
		return c
	}

	yes, no := true, false
	verified := business(&yes, 51)
	second := business(&yes, 52)
	refused := business(&no, 53)
	unasked := business(nil, 54)

	// The VERIFIED business: step 1 draws its logo from the wizard's route and links its
	// theme; the route serves the stored bytes under the logo routes' headers.
	vc := open(verified)
	page := get(t, r, "/activate", vc)
	body := page.Body.String()
	wantHeader := activationLogoHeaderHTML("/activate/logo/", verified.logo.SHA256, verified.logo.Width, verified.logo.Height, "wl13-activation-fixture")
	if headerRE.FindString(body) != wantHeader || !strings.Contains(body, appCSSLink+themeLink) ||
		page.Header().Get("Content-Security-Policy") != activationPolicyLogo {
		t.Fatalf("verified, step 1: header %q, policy %q", headerRE.FindString(body), page.Header().Get("Content-Security-Policy"))
	}
	want := answer{http.StatusOK, wantLogoHeader(verified.logo.SHA256, "image/png", "logo.png", len(verified.logo.Data)), verified.logo.Data}
	if got := ask(t, r, http.MethodGet, activationLogoPath(verified.logo.SHA256), vc); !got.same(want) {
		t.Fatalf("verified, own digest:\n got %s\nwant %s", got, want)
	}
	// POSITIVE CONTROL: the second verified business's digest is a real, served logo --
	// through its own invitation.
	sc := open(second)
	if got := ask(t, r, http.MethodGet, activationLogoPath(second.logo.SHA256), sc); got.status != http.StatusOK {
		t.Fatalf("control: the second business's own digest = %s", got)
	}

	// The verified business's invitation asking for the second business's digest, and
	// for a digest nobody stored: one answer, byte for byte, the one 404.
	foreign := ask(t, r, http.MethodGet, activationLogoPath(second.logo.SHA256), vc)
	unknown := ask(t, r, http.MethodGet, activationLogoPath(unknownDigest), vc)
	if !foreign.same(unknown) || !foreign.same(wantNotFound()) {
		t.Errorf("another business's digest and an unknown one:\n foreign %s\n unknown %s", foreign, unknown)
	}

	// vat_verified FALSE and NULL: the wizard draws the accent -- K-2a's ink wordmark and
	// the theme -- and no logo; the route answers the business's OWN digest with the 404.
	for name, b := range map[string]wl13Business{"vat_verified false": refused, "vat_verified NULL": unasked} {
		c := open(b)
		page := get(t, r, "/activate", c)
		body := page.Body.String()
		if headerRE.FindString(body) != inkMarkHTML || !strings.Contains(body, appCSSLink+themeLink) ||
			strings.Contains(body, "<img") || page.Header().Get("Content-Security-Policy") != activationPolicy {
			t.Errorf("%s, step 1: header %q, policy %q; want the ink wordmark, the theme and no image",
				name, headerRE.FindString(body), page.Header().Get("Content-Security-Policy"))
		}
		if got := ask(t, r, http.MethodGet, activationLogoPath(b.logo.SHA256), c); !got.same(wantNotFound()) {
			t.Errorf("%s, own digest:\n got %s\nwant the one 404", name, got)
		}
	}
}
