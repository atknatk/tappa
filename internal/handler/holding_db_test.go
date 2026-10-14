package handler

// holding_db_test.go -- M11 API-1b at the HTTP surface, against real Postgres: the tap and
// result screens of a tap on a PEER's plaque (K-1c: the plaque's business's brand), the logo
// route's peer form (holding_logo: the VIES gate, the one 404), and the activation rules on a
// peer's plaque (K-1b: activation only on the invitation's own business's plaque; K-35: a live
// session with a pending activation taps a peer's plaque as an ordinary holding tap). ADR 0027
// §5, §10; plan §5 API-1b (h), (i).
//
// The holding memberships are written by the OWNER connection (DATABASE_MIGRATE_URL --
// tappa_app may not, ADR 0027 §1) and removed at the end; the rest stays, like every committed
// fixture of this package.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/brand"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/atknatk/tappa/test/fixtures"
)

// holdingOwner is the owner connection; the test skips without DATABASE_MIGRATE_URL.
func holdingOwner(t *testing.T) *pgx.Conn {
	t.Helper()
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; the holding memberships are written by the owner (ADR 0027 §1)")
	}
	c, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

// holdingOf makes tenants the members of one new holding, committed, and removes the
// memberships and the holding when the test ends.
func holdingOf(t *testing.T, owner *pgx.Conn, tenants ...uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var h uuid.UUID
	if err := owner.QueryRow(ctx, `INSERT INTO holdings (name) VALUES ($1) RETURNING id`, "api1b handler "+uuid.NewString()[:8]).Scan(&h); err != nil {
		t.Fatalf("holding: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		if _, err := owner.Exec(bg, `DELETE FROM holding_members WHERE holding_id = $1`, h); err != nil {
			t.Errorf("cleanup memberships: %v", err)
		}
		if _, err := owner.Exec(bg, `DELETE FROM holdings WHERE id = $1`, h); err != nil {
			t.Errorf("cleanup holding: %v", err)
		}
	})
	for _, tenantID := range tenants {
		if _, err := owner.Exec(ctx, `INSERT INTO holding_members (holding_id, member_tenant_id, joined_at) VALUES ($1, $2, clock_timestamp() - interval '10 minutes')`,
			h, tenantID); err != nil {
			t.Fatalf("membership: %v", err)
		}
	}
	return h
}

// holdingVIES sets a business's VIES answer (the owner: tappa_app may not write it after insert).
func holdingVIES(t *testing.T, owner *pgx.Conn, tenantID uuid.UUID, verified any) {
	t.Helper()
	if _, err := owner.Exec(context.Background(), `UPDATE tenants SET vat_verified = $2 WHERE id = $1`, tenantID, verified); err != nil {
		t.Fatalf("VIES: %v", err)
	}
}

// ------------------------------------------------------------------- (i) --

// TestHoldingBrand_TapAndResultShowTheSiteBrand is K-1c (ADR 0027 §10; plan §5 API-1b (i)): KM's
// employee (the harness's business) in front of a plaque of KF -- a peer, VIES verified, with a
// logo and an accent -- gets a tap page that names KF's venue and draws KF's logo (the tap route's
// peer form, /t/logo/<sha>?tag=<uid>) and KF's accent; after the button, a confirmation screen
// with KF's logo and no accent (CLAUDE.md §9: the result screen has no accent slot). Neither
// screen carries KM's own brand. The same plaque of a business that is NOT a peer stays the
// unbranded foreign-plaque page (the control).
func TestHoldingBrand_TapAndResultShowTheSiteBrand(t *testing.T) {
	h := newTapHarness(t)
	owner := holdingOwner(t)
	kek, err := hex.DecodeString(tapFakeKEK)
	if err != nil {
		t.Fatal(err)
	}
	kf, kfVenue := uuid.New(), uuid.New()
	kfUID := h.newTag(t, kek, kf, kfVenue, 100)
	if _, err := owner.Exec(context.Background(), `UPDATE locations SET name = 'KF Sliema' WHERE id = $1`, kfVenue); err != nil {
		t.Fatal(err)
	}
	kfLogo := h.brandBusiness(t, kf, 73, "FFC72C")
	kmLogo := h.brandBusiness(t, h.tenantID, 31, "DA291C")
	holdingVIES(t, owner, kf, true)
	cookie := h.sessionCookieFor(t)

	// CONTROL: not (yet) peers -- the ordinary foreign-plaque page.
	if body := h.get(t, tapURLFor(kfUID), cookie).Body.String(); strings.Contains(body, kfLogo.SHA256) || strings.Contains(body, "FFC72C") || strings.Contains(body, "KF Sliema") {
		t.Fatalf("a plaque of a business that is not a peer drew its brand:\n%s", body)
	}

	holdingOf(t, owner, h.tenantID, kf)
	page := h.get(t, tapURLFor(kfUID), cookie)
	if page.Code != http.StatusOK {
		t.Fatalf("GET /t on a peer's plaque = %d", page.Code)
	}
	body := page.Body.String()
	for _, want := range []string{`src="/t/logo/` + kfLogo.SHA256 + `?tag=` + kfUID + `"`, `href="/brand/theme/FFC72C.css"`, "KF Sliema"} {
		if !strings.Contains(body, want) {
			t.Errorf("the tap page on a peer's plaque lacks %q", want)
		}
	}
	if strings.Contains(body, kmLogo.SHA256) || strings.Contains(body, "DA291C") {
		t.Error("the tap page on a peer's plaque carries the session's own brand")
	}
	if csp := page.Result().Header.Get("Content-Security-Policy"); !strings.Contains(csp, "img-src 'self'") {
		t.Errorf("the page draws a logo and its policy does not name img-src: %q", csp)
	}

	res := h.postTap(t, tapContext{UID: kfUID, Ctr: 101, Channel: sun.ChannelNFC, CMACVerified: true,
		TagTenantID: kf, LocationID: kfVenue}, cookie, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("POST on a peer's plaque = %d:\n%s", res.Code, res.Body.String())
	}
	rb := res.Body.String()
	if !strings.Contains(rb, `src="/t/logo/`+kfLogo.SHA256+`?tag=`+kfUID+`"`) {
		t.Errorf("the confirmation screen lacks the peer's logo:\n%s", rb)
	}
	if strings.Contains(rb, "/brand/theme/") || strings.Contains(rb, kmLogo.SHA256) {
		t.Errorf("the confirmation screen carries an accent or the session's own logo:\n%s", rb)
	}
	if got := h.lastCtr(t, kfUID); got != 101 {
		t.Errorf("the peer's counter %d, want 101 (the holding advance)", got)
	}
}

// logoPeerFixture is two peers KM and KF (a plaque of KF's, KF's logo stored by the product's
// writer, KF's VIES answer as given) and an outsider C in no holding with a plaque and a logo
// of its own, through the REAL reader.
type logoPeerFixture struct {
	reader        *tenant.BrandReader
	brands        *tenant.Brands
	gate          *brand.LogoGate
	owner         *pgx.Conn
	km, kf, c     uuid.UUID
	kfOwner       uuid.UUID
	kfUID, cUID   string
	kfLogo, cLogo brand.Logo
}

func newLogoPeerFixture(t *testing.T, vies any) *logoPeerFixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping the peer logo route over real Postgres")
	}
	owner := holdingOwner(t)
	ctx := context.Background()
	data, err := db.New(ctx, &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(data.Close)
	trail, err := audit.New(data)
	if err != nil {
		t.Fatal(err)
	}
	f := &logoPeerFixture{owner: owner}
	if f.brands, err = tenant.NewBrands(data, trail, discardLogger()); err != nil {
		t.Fatal(err)
	}
	if f.gate, err = brand.NewLogoGate(brand.LogoDecodeSlots); err != nil {
		t.Fatal(err)
	}
	if f.reader, err = tenant.NewBrandReader(data); err != nil {
		t.Fatal(err)
	}
	business := func(name string) (uuid.UUID, uuid.UUID, string) {
		id, adm, venue := uuid.New(), uuid.New(), uuid.New()
		b := make([]byte, 7)
		key := make([]byte, 44)
		if _, err := rand.Read(b); err != nil {
			t.Fatal(err)
		}
		if _, err := rand.Read(key); err != nil {
			t.Fatal(err)
		}
		uid := strings.ToUpper(hex.EncodeToString(b))
		for _, s := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, $2, $3, 'restaurant', 'multi')`, []any{id, name, "VAT-" + id.String()}},
			{`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status) VALUES ($1, $2, 'api1b-owner', $3, $4, 'owner', 'active')`,
				[]any{adm, id, "api1b-" + uuid.NewString() + "@iso.example", fixtures.UnusablePasswordHash}},
			{`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'Venue')`, []any{venue, id}},
			{`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status) VALUES ($1, $2, $3, $4, 1, 'active')`, []any{uid, id, venue, key}},
		} {
			if _, err := owner.Exec(ctx, s.sql, s.args...); err != nil {
				t.Fatalf("%s: %v", s.sql, err)
			}
		}
		return id, adm, uid
	}
	f.km, _, _ = business("api1b KM")
	var cOwner uuid.UUID
	f.kf, f.kfOwner, f.kfUID = business("api1b KF")
	f.c, cOwner, f.cUID = business("api1b C")
	f.kfLogo = logoDBPicture(t, f.gate, false, 91)
	f.cLogo = logoDBPicture(t, f.gate, true, 92)
	if err := f.brands.SaveLogo(ctx, tenant.BrandLogoCommand{TenantID: f.kf, ActorID: f.kfOwner, Logo: f.kfLogo}); err != nil {
		t.Fatal(err)
	}
	if err := f.brands.SaveLogo(ctx, tenant.BrandLogoCommand{TenantID: f.c, ActorID: cOwner, Logo: f.cLogo}); err != nil {
		t.Fatal(err)
	}
	holdingVIES(t, owner, f.kf, vies)
	holdingVIES(t, owner, f.c, true)
	holdingOf(t, owner, f.km, f.kf)
	return f
}

// peerLogoPath is the tap route's peer form.
func peerLogoPath(digest, uid string) string { return tapLogoPath(digest) + "?tag=" + uid }

// TestHoldingLogo_VIESGate is ADR 0027 §10's VIES gate on the peer form (ADR 0023's WL-13 gate):
// KM's live session asking for KF's logo by KF's plaque gets the bytes, under exactly the ADR
// 0024 headers, when KF is VIES verified; when KF's answer is false or never asked (NULL), the
// same request is the one 404 -- byte-identical to a digest nobody stored.
func TestHoldingLogo_VIESGate(t *testing.T) {
	for _, c := range []struct {
		name  string
		vies  any
		serve bool
	}{
		{"verified", true, true},
		{"refused", false, false},
		{"never asked", nil, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newLogoPeerFixture(t, c.vies)
			asKM := logoRouter(t, f.reader, liveEmployee(f.km), livePanel(f.km))
			got := ask(t, asKM, http.MethodGet, peerLogoPath(f.kfLogo.SHA256, f.kfUID), employeeCookie())
			if c.serve {
				want := answer{http.StatusOK, wantLogoHeader(f.kfLogo.SHA256, "image/png", "logo.png", len(f.kfLogo.Data)), f.kfLogo.Data}
				if !got.same(want) {
					t.Fatalf("KF verified, its logo by its plaque:\n got %s\nwant %s", got, want)
				}
				return
			}
			unknown := ask(t, asKM, http.MethodGet, peerLogoPath(unknownDigest, f.kfUID), employeeCookie())
			if !got.same(unknown) || !got.same(wantNotFound()) {
				t.Fatalf("KF not verified: %s, want the one 404 (= an unknown digest: %s)", got, unknown)
			}
		})
	}
}

// TestHoldingLogo_OutsiderOrStaleDigestIs404 is ADR 0027 §10's "C'nin ya da eski bir özetin
// baytı asla": every one of these is the one 404, byte-identical to a digest nobody stored --
//   - C's session (no holding) asking for KF's logo by KF's plaque;
//   - KM's session asking for C's logo by C's plaque (C outside KM's holding);
//   - KM's session asking for C's logo by KF's plaque (another business's digest);
//   - KM's session asking for KF's OLD logo after KF replaced it (the new one is served);
//   - a malformed `tag` and a doubled `tag` (never read);
//
// and the plain route without `tag` still answers KM's own business only (KF's digest: 404).
func TestHoldingLogo_OutsiderOrStaleDigestIs404(t *testing.T) {
	f := newLogoPeerFixture(t, true)
	asKM := logoRouter(t, f.reader, liveEmployee(f.km), livePanel(f.km))
	asC := logoRouter(t, f.reader, liveEmployee(f.c), livePanel(f.c))
	notFound := wantNotFound()
	unknown := ask(t, asKM, http.MethodGet, peerLogoPath(unknownDigest, f.kfUID), employeeCookie())
	if !unknown.same(notFound) {
		t.Fatalf("PREMISE: an unknown digest by KF's plaque = %s", unknown)
	}
	served := ask(t, asKM, http.MethodGet, peerLogoPath(f.kfLogo.SHA256, f.kfUID), employeeCookie())
	if served.status != http.StatusOK {
		t.Fatalf("CONTROL: KM's session gets KF's logo by KF's plaque: %s", served)
	}
	old := f.kfLogo
	newer := logoDBPicture(t, f.gate, false, 93)
	for _, c := range []struct {
		name string
		h    http.Handler
		path string
	}{
		{"C's session, KF's logo by KF's plaque", asC, peerLogoPath(f.kfLogo.SHA256, f.kfUID)},
		{"KM's session, C's logo by C's plaque", asKM, peerLogoPath(f.cLogo.SHA256, f.cUID)},
		{"KM's session, C's logo by KF's plaque", asKM, peerLogoPath(f.cLogo.SHA256, f.kfUID)},
		{"a malformed tag", asKM, peerLogoPath(f.kfLogo.SHA256, strings.ToLower(f.kfUID))},
		{"a doubled tag", asKM, peerLogoPath(f.kfLogo.SHA256, f.kfUID) + "&tag=" + url.QueryEscape(f.kfUID)},
		{"the plain route, KF's digest", asKM, tapLogoPath(f.kfLogo.SHA256)},
	} {
		if got := ask(t, c.h, http.MethodGet, c.path, employeeCookie()); !got.same(notFound) {
			t.Errorf("%s: %s, want the one 404", c.name, got)
		}
	}
	if err := f.brands.SaveLogo(context.Background(), tenant.BrandLogoCommand{TenantID: f.kf, ActorID: f.kfOwner, Logo: newer}); err != nil {
		t.Fatal(err)
	}
	if got := ask(t, asKM, http.MethodGet, peerLogoPath(old.SHA256, f.kfUID), employeeCookie()); !got.same(notFound) {
		t.Errorf("KF's OLD logo after it was replaced: %s, want the one 404", got)
	}
	if got := ask(t, asKM, http.MethodGet, peerLogoPath(newer.SHA256, f.kfUID), employeeCookie()); got.status != http.StatusOK {
		t.Errorf("CONTROL: KF's new logo: %s", got)
	}
}

// ------------------------------------------------------------------- (h) --

// TestHoldingActivation_OnlyOnItsOwnTenantsPlaque is K-1b (ADR 0026 unchanged; plan §5 API-1b
// (h)): an invitee of business A, consented, tapping a plaque of A's PEER B with a genuine
// signature activates nothing -- the activation flow's one refusal, B's counter unmoved, no
// session, the invitation unspent -- and then A's own plaque activates.
func TestHoldingActivation_OnlyOnItsOwnTenantsPlaque(t *testing.T) {
	h := newHarness(t, "invited")
	owner := holdingOwner(t)
	kek, err := hex.DecodeString(tapFakeKEK)
	if err != nil {
		t.Fatal(err)
	}
	peer, peerVenue := uuid.New(), uuid.New()
	peerUID := h.newTag(t, kek, peer, peerVenue, 100)
	holdingOf(t, owner, h.tenantID, peer)
	c := h.client(t)
	h.consent(t, c, h.issue(t))

	resp, page := h.tapWith(t, c, signedTapURL(t, tapFakeTagKey, peerUID, 101))
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(page, "finish setup") {
		t.Fatalf("a PEER's plaque answered %d -- activation is only on the invitation's own business's plaque (K-1b)", resp.StatusCode)
	}
	if got := h.lastCtr(t, peerUID); got != 100 {
		t.Errorf("the peer's counter moved to %d", got)
	}
	h.assertSessionCount(t, 0)
	h.assertInviteConsumed(t, false)
	h.assertAuditMentions(t, ActionActivationFailed, "foreign_tenant_tag")

	if resp, page := h.tapWith(t, c, h.nextTap(t)); resp.StatusCode != http.StatusOK || !strings.Contains(page, "Activation complete") {
		t.Fatalf("the invitation's own plaque then answered %d", resp.StatusCode)
	}
}

// TestHoldingTap_PendingActivationBrowserWithLiveSessionTapsAsHolding is K-35 (ADR 0027 §5;
// plan §5 API-1b (h)): a phone with a LIVE session of business A's employee and a PENDING
// consented activation of A's invitee. On a plaque of A's PEER B -- not the invitation's
// business -- the tap is an ordinary holding tap: the tap page (200, the button's signed
// context), no activation, the invitation unspent, B's counter untouched by the page. On A's
// own plaque the activation completes as ever. The same phone with B NOT a peer gets the
// activation flow's refusal (the control: today's behaviour).
func TestHoldingTap_PendingActivationBrowserWithLiveSessionTapsAsHolding(t *testing.T) {
	h := newHarness(t, "invited")
	owner := holdingOwner(t)
	kek, err := hex.DecodeString(tapFakeKEK)
	if err != nil {
		t.Fatal(err)
	}
	peer, peerVenue := uuid.New(), uuid.New()
	peerUID := h.newTag(t, kek, peer, peerVenue, 100)
	c := h.client(t)
	h.consent(t, c, h.issue(t))
	u, err := url.Parse(h.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	c.Jar.SetCookies(u, []*http.Cookie{h.tapHarness.sessionCookieFor(t)})

	// CONTROL: B is not a peer -- the activation flow takes the tap and refuses it.
	if resp, _ := h.tapWith(t, c, signedTapURL(t, tapFakeTagKey, peerUID, 101)); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("CONTROL: a non-peer's plaque with a pending activation answered %d, want the activation's refusal", resp.StatusCode)
	}

	holdingOf(t, owner, h.tenantID, peer)
	resp, page := h.tapWith(t, c, signedTapURL(t, tapFakeTagKey, peerUID, 102))
	if resp.StatusCode != http.StatusOK || !strings.Contains(page, `name="ctx"`) {
		t.Fatalf("a peer's plaque with a live session and a pending activation answered %d -- want the ordinary tap page (K-35):\n%s", resp.StatusCode, page)
	}
	h.assertInviteConsumed(t, false)
	if got := h.lastCtr(t, peerUID); got != 100 {
		t.Errorf("the page moved the peer's counter to %d", got)
	}

	if resp, page := h.tapWith(t, c, h.nextTap(t)); resp.StatusCode != http.StatusOK || !strings.Contains(page, "Activation complete") {
		t.Fatalf("the invitation's own plaque then answered %d", resp.StatusCode)
	}
	h.assertInviteConsumed(t, true)
}

// TestHoldingTap_PendingActivationOnTheInvitationsPlaqueStillActivates is K-35's other half
// (ADR 0027 §5: "plaketin tenant'ı davetinkiyle aynıysa aktivasyon yine tamamlanır"): the live
// session is of the PEER's employee (B), the pending invitation is A's, and the plaque is A's --
// a peer's plaque from the session's point of view, and the invitation's own. The activation
// completes; the tap is not taken as a holding tap.
func TestHoldingTap_PendingActivationOnTheInvitationsPlaqueStillActivates(t *testing.T) {
	h := newHarness(t, "invited")
	owner := holdingOwner(t)
	kek, err := hex.DecodeString(tapFakeKEK)
	if err != nil {
		t.Fatal(err)
	}
	peer, peerVenue := uuid.New(), uuid.New()
	h.newTag(t, kek, peer, peerVenue, 100)
	peerEmployee := uuid.New()
	if _, err := owner.Exec(context.Background(), `INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at) VALUES ($1, $2, $3, 'Peer Staff', 'active', now())`,
		peerEmployee, peer, peerVenue); err != nil {
		t.Fatal(err)
	}
	holdingOf(t, owner, h.tenantID, peer)
	issued, err := h.sessions.Issue(context.Background(), session.IssueParams{TenantID: peer, EmployeeID: peerEmployee, DeviceInfo: "test"})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if err := h.cookies.Set(rec, issued.Token); err != nil {
		t.Fatal(err)
	}
	c := h.client(t)
	h.consent(t, c, h.issue(t))
	u, err := url.Parse(h.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	c.Jar.SetCookies(u, rec.Result().Cookies())

	if resp, page := h.tapWith(t, c, h.nextTap(t)); resp.StatusCode != http.StatusOK || !strings.Contains(page, "Activation complete") {
		t.Fatalf("the invitation's own plaque, with a peer's live session, answered %d -- want the activation:\n%s", resp.StatusCode, page)
	}
	h.assertInviteConsumed(t, true)
}
