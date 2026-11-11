package handler

// holding_db_test.go -- M11 API-1b at the HTTP surface, against real Postgres: the tap and
// result screens of a tap on a PEER's plaque (K-1c: the plaque's business's brand), the logo
// route's peer form (holding_logo: the VIES gate, the one 404), and the activation rules on a
// peer's plaque (the user's decision of 2026-11-11, which replaced K-1b: activation completes on a
// LIVE peer's plaque of the invitation's business; K-35 as it left it: a live session with a
// pending activation taps a plaque of a peer of the SESSION's business -- one that is not the
// invitation's nor its peer -- as an ordinary holding tap). ADR 0027 §5, §10; plan §5 API-1b (h), (i).
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

	// THE VIES GATE ON THE PAGE (ADR 0027 §10): KF's VIES answer withdrawn -- the page keeps
	// KF's venue and accent and draws NO logo (holding_tap_site gives no digest).
	for _, answer := range []any{false, nil} {
		holdingVIES(t, owner, kf, answer)
		body := h.get(t, tapURLFor(kfUID), cookie).Body.String()
		if strings.Contains(body, "/t/logo/") || strings.Contains(body, "<img") {
			t.Errorf("VIES %v: the tap page draws a logo", answer)
		}
		if !strings.Contains(body, `href="/brand/theme/FFC72C.css"`) || !strings.Contains(body, "KF Sliema") {
			t.Errorf("VIES %v: the tap page lost KF's accent or venue", answer)
		}
	}

	// A PEER'S PLAQUE ON NO WALL (ADR 0023 §2: a brand only for a plaque on a wall; the third-eye
	// review's item 2): KF verified again, a second KF plaque taken off its wall. holding_tap_site
	// still answers -- a peer's plaque -- and the page draws NEITHER KF's logo NOR its accent, and
	// names no venue. MUTANT (measured, ADR 0027's API-1b note): PeerTapPage without its
	// LocationID == nil return draws both.
	holdingVIES(t, owner, kf, true)
	loose := h.newTag(t, kek, kf, uuid.New(), 100)
	if _, err := owner.Exec(context.Background(), `UPDATE tags SET location_id = NULL, status = 'unassigned' WHERE uid = $1`, loose); err != nil {
		t.Fatal(err)
	}
	unwalled := h.get(t, tapURLFor(loose), cookie)
	ub := unwalled.Body.String()
	if unwalled.Code != http.StatusOK || !strings.Contains(ub, "<form") {
		t.Fatalf("anti-vacuity: GET /t on a peer's plaque on no wall = %d, without the tap form:\n%s", unwalled.Code, ub)
	}
	if strings.Contains(ub, "/t/logo/") || strings.Contains(ub, "FFC72C") || strings.Contains(ub, "/brand/theme/") {
		t.Errorf("a peer's plaque on no wall drew a brand:\n%s", ub)
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
	data, err := db.New(ctx, &config.Config{DatabaseURL: config.NewDSN(dsn)})
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
		// The owner row is COMMITTED (owner autocommits each statement): written under the
		// operator-tables lock, held shared while this business is written (holdPanelTablesLock;
		// cmd/opadmin's TestTablesLock_AdminRowsAreWrittenUnderTheHolder).
		release := holdPanelTablesLock(t)
		defer release()
		id, adm, venue := uuid.New(), uuid.New(), uuid.New()
		b := make([]byte, 7)
		key := make([]byte, 44)
		if _, err := rand.Read(b); err != nil {
			t.Fatal(err)
		}
		if _, err := rand.Read(key); err != nil {
			t.Fatal(err)
		}
		// A hex letter in the uid, forced: all-numeric (1 uid in 721) strings.ToLower is a no-op, and
		// "a malformed tag" in TestHoldingLogo_OutsiderOrStaleDigestIs404 sends a well-formed one.
		b[0] |= 0xA0
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

// peerTrailRow is one row of a business's audit_log, read in its own context.
type peerTrailRow struct {
	action, target, detail string
	actor                  *uuid.UUID
}

// peerTrail reads every audit_log row of tenantID (tappa_app, in tenantID's own context).
func (h *harness) peerTrail(t *testing.T, tenantID uuid.UUID) []peerTrailRow {
	t.Helper()
	var out []peerTrailRow
	if err := h.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT action, coalesce(target, ''), detail::text, actor_id FROM audit_log WHERE tenant_id = $1 ORDER BY at`, tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r peerTrailRow
			if err := rows.Scan(&r.action, &r.target, &r.detail, &r.actor); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("read the trail of %s: %v", tenantID, err)
	}
	return out
}

// countIn counts query's rows in tenantID's own context ($1 = tenantID).
func (h *harness) countIn(t *testing.T, tenantID uuid.UUID, query string) int {
	t.Helper()
	var n int
	if err := h.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, query, tenantID).Scan(&n)
	}); err != nil {
		t.Fatalf("count in %s: %v", tenantID, err)
	}
	return n
}

// peerWorld is the invitation's business (the harness's) and a second business with one active
// plaque on a wall -- made a holding peer of the first by the caller, or not.
func peerWorld(t *testing.T, h *harness, startCtr int32) (peer, peerVenue uuid.UUID, peerUID string) {
	t.Helper()
	kek, err := hex.DecodeString(tapFakeKEK)
	if err != nil {
		t.Fatal(err)
	}
	peer, peerVenue = uuid.New(), uuid.New()
	peerUID = h.newTag(t, kek, peer, peerVenue, startCtr)
	return peer, peerVenue, peerUID
}

// assertNoPeerActivationTrail: the peer's trail holds no activation row.
func (h *harness) assertNoPeerActivationTrail(t *testing.T, peer uuid.UUID) {
	t.Helper()
	for _, r := range h.peerTrail(t, peer) {
		if r.action == "holding.activation_tap" {
			t.Errorf("the peer's trail holds an activation row on a refused tap: %+v", r)
		}
	}
}

// TestHoldingActivation_CompletesOnALivePeersPlaque is the user's decision of 2026-11-11 (ADR 0026
// and ADR 0027 notes of that day; it replaces K-1b): an invitee of business A (Eser, registered at
// KF), consented, tapping a plaque of A's LIVE holding peer B (KM, where Eser works) with a genuine
// signature completes the activation. The session is A's and the invitee's; B's counter advanced
// exactly once; no transactions row, no site_punches row, no punch_events row anywhere; A's trail
// says activation.completed with B's plaque and B's id; B's trail holds exactly one row --
// holding.activation_tap, the plaque's uid and wall, no actor -- naming nothing of the person, the
// invitation or A. The next tap on B's plaque is then an ordinary holding tap.
func TestHoldingActivation_CompletesOnALivePeersPlaque(t *testing.T) {
	h := newHarness(t, "invited")
	owner := holdingOwner(t)
	peer, peerVenue, peerUID := peerWorld(t, h, 100)
	holdingOf(t, owner, h.tenantID, peer)
	before := h.lastCtr(t, h.tagUID)
	c := h.client(t)
	h.consent(t, c, h.issue(t))

	resp, page := h.tapWith(t, c, signedTapURL(t, tapFakeTagKey, peerUID, 101))
	if resp.StatusCode != http.StatusOK || !strings.Contains(page, "Activation complete") {
		t.Fatalf("a live peer's plaque answered %d -- want the activation (2026-11-11):\n%s", resp.StatusCode, page)
	}
	if got := h.lastCtr(t, peerUID); got != 101 {
		t.Errorf("the peer's counter %d, want 101 (advanced once)", got)
	}
	if got := h.lastCtr(t, h.tagUID); got != before {
		t.Errorf("the invitation's own plaque moved from %d to %d", before, got)
	}
	h.assertInviteConsumed(t, true)
	h.assertEmployeeStatus(t, "active")
	h.assertLiveSessionCount(t, 1)
	for _, tenantID := range []uuid.UUID{h.tenantID, peer} {
		for what, q := range map[string]string{
			"transactions": `SELECT count(*) FROM transactions WHERE tenant_id = $1`,
			"site_punches": `SELECT count(*) FROM site_punches WHERE tenant_id = $1`,
			"punch_events": `SELECT count(*) FROM punch_events WHERE tenant_id = $1`,
		} {
			if tenantID == h.tenantID && what == "transactions" {
				// The harness's own fixtures may hold rows of other employees; the invitee's are none.
				if n := h.countIn(t, tenantID, `SELECT count(*) FROM transactions WHERE tenant_id = $1 AND employee_id = '`+h.employeeID.String()+`'`); n != 0 {
					t.Errorf("the activation wrote %d transactions rows for the invitee", n)
				}
				continue
			}
			if n := h.countIn(t, tenantID, q); n != 0 {
				t.Errorf("%s holds %d %s rows after an activation (want none)", tenantID, n, what)
			}
		}
	}
	h.assertAuditMentions(t, ActionActivationCompleted, peerUID)
	h.assertAuditMentions(t, ActionActivationCompleted, `"site_tenant_id": "`+peer.String()+`"`)

	trail := h.peerTrail(t, peer)
	if len(trail) != 1 {
		t.Fatalf("the peer's trail holds %d rows, want exactly 1: %+v", len(trail), trail)
	}
	row := trail[0]
	if row.action != "holding.activation_tap" || row.target != peerUID || row.actor != nil {
		t.Errorf("the peer's row is %+v, want holding.activation_tap on %s with no actor", row, peerUID)
	}
	if want := `{"location_id": "` + peerVenue.String() + `"}`; row.detail != want {
		t.Errorf("the peer's row detail = %s, want %s", row.detail, want)
	}
	for _, never := range []string{h.employeeID.String(), h.tenantID.String(), "Maria Borg", "Kebab Factory", "invite", "employee", "@"} {
		if strings.Contains(row.target+row.detail, never) {
			t.Errorf("the peer's row names %q: %+v", never, row)
		}
	}

	// The next tap on the peer's plaque is an ordinary holding tap: the tap page, the button.
	resp, page = h.tapWith(t, c, signedTapURL(t, tapFakeTagKey, peerUID, 102))
	if resp.StatusCode != http.StatusOK || !strings.Contains(page, `name="ctx"`) {
		t.Fatalf("the next tap on the peer's plaque answered %d, want the ordinary tap page:\n%s", resp.StatusCode, page)
	}
}

// TestHoldingActivation_ANonPeersPlaqueIsRefusedAndMovesNothing: B is NOT a peer of A -- the one
// refusal (foreign_tenant_tag), no counter moved anywhere (B's nor A's), B's trail untouched, B's
// uid kept out of A's trail, no session, the invitation unspent; then A's own plaque activates.
func TestHoldingActivation_ANonPeersPlaqueIsRefusedAndMovesNothing(t *testing.T) {
	h := newHarness(t, "invited")
	peer, _, peerUID := peerWorld(t, h, 100)
	before := h.lastCtr(t, h.tagUID)
	c := h.client(t)
	h.consent(t, c, h.issue(t))

	resp, page := h.tapWith(t, c, signedTapURL(t, tapFakeTagKey, peerUID, 101))
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(page, "finish setup") {
		t.Fatalf("a non-peer's plaque answered %d, want the activation's one refusal", resp.StatusCode)
	}
	if got := h.lastCtr(t, peerUID); got != 100 {
		t.Errorf("the non-peer's counter moved to %d", got)
	}
	if got := h.lastCtr(t, h.tagUID); got != before {
		t.Errorf("the invitation's own plaque moved from %d to %d", before, got)
	}
	h.assertSessionCount(t, 0)
	h.assertInviteConsumed(t, false)
	h.assertAuditMentions(t, ActionActivationFailed, "foreign_tenant_tag")
	h.assertAuditDoesNotMention(t, peerUID)
	if n := len(h.peerTrail(t, peer)); n != 0 {
		t.Errorf("the non-peer's trail holds %d rows", n)
	}

	if resp, page := h.tapWith(t, c, h.nextTap(t)); resp.StatusCode != http.StatusOK || !strings.Contains(page, "Activation complete") {
		t.Fatalf("the invitation's own plaque then answered %d", resp.StatusCode)
	}
}

// TestHoldingActivation_AReplayedPeerURLIsRefused: the peer's plaque already read past the URL's
// counter (150 > 101) -- a genuine signature, a counter that does not go up: the one refusal
// (peer_replay in A's trail), the counter left at 150, no row in B's trail, the invitation
// unspent. The plaque's next real read (151) then activates.
func TestHoldingActivation_AReplayedPeerURLIsRefused(t *testing.T) {
	h := newHarness(t, "invited")
	owner := holdingOwner(t)
	peer, _, peerUID := peerWorld(t, h, 150)
	holdingOf(t, owner, h.tenantID, peer)
	c := h.client(t)
	h.consent(t, c, h.issue(t))

	resp, page := h.tapWith(t, c, signedTapURL(t, tapFakeTagKey, peerUID, 101))
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(page, "finish setup") {
		t.Fatalf("a replayed peer URL answered %d, want the activation's one refusal", resp.StatusCode)
	}
	if got := h.lastCtr(t, peerUID); got != 150 {
		t.Errorf("the peer's counter moved to %d on a replay", got)
	}
	h.assertSessionCount(t, 0)
	h.assertInviteConsumed(t, false)
	h.assertAuditMentions(t, ActionActivationFailed, "peer_replay")
	h.assertNoPeerActivationTrail(t, peer)

	if resp, page := h.tapWith(t, c, signedTapURL(t, tapFakeTagKey, peerUID, 151)); resp.StatusCode != http.StatusOK || !strings.Contains(page, "Activation complete") {
		t.Fatalf("the plaque's next real read then answered %d", resp.StatusCode)
	}
	if got := h.lastCtr(t, peerUID); got != 151 {
		t.Errorf("the peer's counter %d, want 151", got)
	}
}

// TestHoldingActivation_AnInactivePeerPlaqueIsRefused: a live peer's plaque that is lost, and one
// taken off its wall -- the one refusal (peer_tag_not_active), nothing moved, no row in B's trail;
// and a forged signature on an active one (sun_invalid), nothing moved.
func TestHoldingActivation_AnInactivePeerPlaqueIsRefused(t *testing.T) {
	h := newHarness(t, "invited")
	owner := holdingOwner(t)
	peer, _, lost := peerWorld(t, h, 100)
	kek, err := hex.DecodeString(tapFakeKEK)
	if err != nil {
		t.Fatal(err)
	}
	unwalled := h.newTag(t, kek, peer, uuid.New(), 100)
	forged := h.newTag(t, kek, peer, uuid.New(), 100)
	if _, err := owner.Exec(context.Background(), `UPDATE tags SET status = 'lost' WHERE uid = $1`, lost); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(context.Background(), `UPDATE tags SET location_id = NULL, status = 'unassigned' WHERE uid = $1`, unwalled); err != nil {
		t.Fatal(err)
	}
	holdingOf(t, owner, h.tenantID, peer)
	c := h.client(t)
	h.consent(t, c, h.issue(t))

	for _, tc := range []struct{ name, url, uid, reason string }{
		{"a lost plaque", signedTapURL(t, tapFakeTagKey, lost, 101), lost, "peer_tag_not_active"},
		{"a plaque on no wall", signedTapURL(t, tapFakeTagKey, unwalled, 101), unwalled, "peer_tag_not_active"},
		{"a forged signature", signedTapURL(t, "0F0E0D0C0B0A09080706050403020100", forged, 101), forged, "sun_invalid"},
	} {
		resp, page := h.tapWith(t, c, tc.url)
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(page, "finish setup") {
			t.Fatalf("%s answered %d, want the activation's one refusal", tc.name, resp.StatusCode)
		}
		if got := h.lastCtr(t, tc.uid); got != 100 {
			t.Errorf("%s: the counter moved to %d", tc.name, got)
		}
		h.assertAuditMentions(t, ActionActivationFailed, tc.reason)
	}
	h.assertSessionCount(t, 0)
	h.assertInviteConsumed(t, false)
	h.assertNoPeerActivationTrail(t, peer)
}

// TestHoldingActivation_AnExMemberIsNotAPeer: B LEFT the holding 30 s ago -- inside K-29's 60 s
// exit window, which is the transactions gate's and not the advance's: the activation is held to
// the strict rule of a holding tap's advance (left_at > now). The one refusal (foreign_tenant_tag),
// B's counter unmoved, no row in B's trail, the invitation unspent.
func TestHoldingActivation_AnExMemberIsNotAPeer(t *testing.T) {
	h := newHarness(t, "invited")
	owner := holdingOwner(t)
	peer, _, peerUID := peerWorld(t, h, 100)
	hold := holdingOf(t, owner, h.tenantID, peer)
	if _, err := owner.Exec(context.Background(), `UPDATE holding_members SET left_at = clock_timestamp() - interval '30 seconds'
	                                               WHERE holding_id = $1 AND member_tenant_id = $2`, hold, peer); err != nil {
		t.Fatal(err)
	}
	c := h.client(t)
	h.consent(t, c, h.issue(t))

	resp, page := h.tapWith(t, c, signedTapURL(t, tapFakeTagKey, peerUID, 101))
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(page, "finish setup") {
		t.Fatalf("an ex-member's plaque answered %d, want the activation's one refusal", resp.StatusCode)
	}
	if got := h.lastCtr(t, peerUID); got != 100 {
		t.Errorf("the ex-member's counter moved to %d", got)
	}
	h.assertSessionCount(t, 0)
	h.assertInviteConsumed(t, false)
	h.assertAuditMentions(t, ActionActivationFailed, "foreign_tenant_tag")
	h.assertAuditDoesNotMention(t, peerUID)
	if n := len(h.peerTrail(t, peer)); n != 0 {
		t.Errorf("the ex-member's trail holds %d rows", n)
	}
}

// TestHoldingTap_PendingActivationWithLiveSessionCompletesOnTheInvitationsPeer is K-35 as the user's
// decision of 2026-11-11 left it (ADR 0027 §5): a phone with a LIVE session of business A's
// employee and a PENDING consented activation of A's invitee, on a plaque of A's live peer B --
// the activation completes there now (it was a holding tap before 2026-11-11), B's counter
// advanced once, B's trail holds its one row. The control, B not a peer: the activation flow's
// refusal, as ever.
func TestHoldingTap_PendingActivationWithLiveSessionCompletesOnTheInvitationsPeer(t *testing.T) {
	h := newHarness(t, "invited")
	owner := holdingOwner(t)
	peer, _, peerUID := peerWorld(t, h, 100)
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
	if resp.StatusCode != http.StatusOK || !strings.Contains(page, "Activation complete") {
		t.Fatalf("a peer's plaque with a live session and a pending activation answered %d -- want the activation:\n%s", resp.StatusCode, page)
	}
	h.assertInviteConsumed(t, true)
	if got := h.lastCtr(t, peerUID); got != 102 {
		t.Errorf("the peer's counter %d, want 102", got)
	}
	if n := len(h.peerTrail(t, peer)); n != 1 {
		t.Errorf("the peer's trail holds %d rows, want 1", n)
	}
}

// TestHoldingTap_PendingActivationOnASessionOnlyPeerTapsAsHolding is what is left of K-35: the live
// session is of business P's employee, the pending invitation is A's, and the plaque is of C -- a
// peer of P's, NOT of A's. The tap is an ordinary holding tap for the session (the tap page, the
// button's signed context), no activation, the invitation unspent, C's counter untouched by the page.
func TestHoldingTap_PendingActivationOnASessionOnlyPeerTapsAsHolding(t *testing.T) {
	h := newHarness(t, "invited")
	owner := holdingOwner(t)
	kek, err := hex.DecodeString(tapFakeKEK)
	if err != nil {
		t.Fatal(err)
	}
	p, pVenue := uuid.New(), uuid.New()
	h.newTag(t, kek, p, pVenue, 100)
	pEmployee := uuid.New()
	if _, err := owner.Exec(context.Background(), `INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at) VALUES ($1, $2, $3, 'Peer Staff', 'active', now())`,
		pEmployee, p, pVenue); err != nil {
		t.Fatal(err)
	}
	cTenant, _, cUID := peerWorld(t, h, 100)
	holdingOf(t, owner, p, cTenant)
	issued, err := h.sessions.Issue(context.Background(), session.IssueParams{TenantID: p, EmployeeID: pEmployee, DeviceInfo: "test"})
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

	resp, page := h.tapWith(t, c, signedTapURL(t, tapFakeTagKey, cUID, 101))
	if resp.StatusCode != http.StatusOK || !strings.Contains(page, `name="ctx"`) {
		t.Fatalf("a plaque of the session's peer (not the invitation's) answered %d -- want the ordinary tap page (K-35):\n%s", resp.StatusCode, page)
	}
	h.assertInviteConsumed(t, false)
	if got := h.lastCtr(t, cUID); got != 100 {
		t.Errorf("the page moved C's counter to %d", got)
	}
	if n := len(h.peerTrail(t, cTenant)); n != 0 {
		t.Errorf("C's trail holds %d rows", n)
	}
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
