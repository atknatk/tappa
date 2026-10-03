package handler

// tapbrand_db_test.go -- M10 WL-9 against the REAL directory, decision engine and
// Postgres (tappa_app): two businesses, each with a logo and an accent stored by the
// product's own writer (tenant.Brands, from brand.LogoGate output). ADR 0023 Iddia D:
// a business's brand is not shown to another business's employee. Fixtures are not
// cleaned up (tappa_app holds no DELETE on tenant_branding, tags or transactions).

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/brand"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/atknatk/tappa/test/fixtures"
)

// brandBusiness gives tenantID an owner, a logo and an accent, and returns the logo.
func (h *tapHarness) brandBusiness(t *testing.T, tenantID uuid.UUID, seed uint8, accent string) brand.Logo {
	t.Helper()
	owner := uuid.New()
	err := h.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
			 VALUES ($1, $2, 'wl9-owner', $3, $4, 'owner', 'active')`,
			owner, tenantID, "wl9-"+uuid.NewString()+"@iso.example", fixtures.UnusablePasswordHash)
		return e
	})
	if err != nil {
		t.Fatalf("fixture owner: %v", err)
	}
	brands, err := tenant.NewBrands(h.data, h.trail, discardLogger())
	if err != nil {
		t.Fatalf("NewBrands: %v", err)
	}
	gate, err := brand.NewLogoGate(brand.LogoDecodeSlots)
	if err != nil {
		t.Fatalf("NewLogoGate: %v", err)
	}
	logo := logoDBPicture(t, gate, false, seed)
	c, err := brand.ParseAccent(accent)
	if err != nil {
		t.Fatalf("accent: %v", err)
	}
	ctx := context.Background()
	if err := brands.SaveLogo(ctx, tenant.BrandLogoCommand{TenantID: tenantID, ActorID: owner, Logo: logo}); err != nil {
		t.Fatalf("save logo: %v", err)
	}
	if err := brands.SaveAccent(ctx, tenant.BrandAccentCommand{TenantID: tenantID, ActorID: owner, Accent: c}); err != nil {
		t.Fatalf("save accent: %v", err)
	}
	return logo
}

// TestTapDB_ABrandIsDrawnOnlyOnTheBusinesssOwnPlaque is ADR 0023 Iddia D's WL-9 PART I:
// an employee of business A in front of business B's plaque -- both businesses with a
// logo and an accent -- gets a tap page and, after the button, a screen with no
// /t/logo/ and no theme stylesheet in the body; on A's own plaque the tap page draws
// A's logo and A's theme, and the confirmation screen draws A's logo and no theme.
func TestTapDB_ABrandIsDrawnOnlyOnTheBusinesssOwnPlaque(t *testing.T) {
	h := newTapHarness(t)
	kek, err := hex.DecodeString(tapFakeKEK)
	if err != nil {
		t.Fatalf("kek: %v", err)
	}
	otherTenant, otherLocation := uuid.New(), uuid.New()
	foreignUID := h.newTag(t, kek, otherTenant, otherLocation, 100)
	logoA := h.brandBusiness(t, h.tenantID, 31, "DA291C")
	logoB := h.brandBusiness(t, otherTenant, 62, "FFC72C")
	if logoA.SHA256 == logoB.SHA256 {
		t.Fatal("fixture: the two businesses hold the same logo")
	}
	cookie := h.sessionCookieFor(t)

	noBrand := func(t *testing.T, what string, body string) {
		t.Helper()
		for _, s := range []string{"/t/logo/", "/brand/theme/", "<img"} {
			if strings.Contains(body, s) {
				t.Fatalf("%s carries %q:\n%s", what, s, body)
			}
		}
	}

	// B's plaque: the tap page and the screen after the button.
	page := h.get(t, tapURLFor(foreignUID), cookie)
	if page.Code != http.StatusOK {
		t.Fatalf("GET /t on B's plaque = %d", page.Code)
	}
	noBrand(t, "the tap page on B's plaque", page.Body.String())
	if csp := page.Result().Header.Get("Content-Security-Policy"); csp != tapPolicy {
		t.Fatalf("the tap page on B's plaque names %q", csp)
	}
	foreign := h.postTap(t, tapContext{UID: foreignUID, Ctr: 101, Channel: sun.ChannelNFC, CMACVerified: true,
		TagTenantID: otherTenant, LocationID: otherLocation}, cookie, nil)
	if foreign.Code != http.StatusForbidden {
		t.Fatalf("POST on B's plaque = %d, want 403 (sys:tenant-mismatch)", foreign.Code)
	}
	noBrand(t, "the screen after the button on B's plaque", foreign.Body.String())

	// CONTROL -- A's own plaque: A's logo and theme on the page, A's logo on the screen.
	own := h.get(t, tapURLFor(h.tagUID), cookie)
	ownBody := own.Body.String()
	for _, want := range []string{`src="/t/logo/` + logoA.SHA256 + `"`, `alt="Kebab Factory Ltd"`, `href="/brand/theme/DA291C.css"`} {
		if !strings.Contains(ownBody, want) {
			t.Fatalf("A's own plaque: the tap page lacks %q:\n%s", want, ownBody)
		}
	}
	if strings.Contains(ownBody, logoB.SHA256) || strings.Contains(ownBody, "FFC72C") {
		t.Fatal("A's tap page carries B's brand")
	}
	result := h.postTap(t, h.nfcContext(701), cookie, nil)
	if result.Code != http.StatusOK {
		t.Fatalf("POST on A's plaque = %d", result.Code)
	}
	body := result.Body.String()
	if !strings.Contains(body, `src="/t/logo/`+logoA.SHA256+`"`) {
		t.Fatalf("the confirmation screen on A's own plaque lacks A's logo:\n%s", body)
	}
	if strings.Contains(body, "/brand/theme/") {
		t.Fatal("the confirmation screen links a theme (D-C: no accent there)")
	}
	if csp := result.Result().Header.Get("Content-Security-Policy"); csp != tapPolicyLogo {
		t.Fatalf("the confirmation screen names %q", csp)
	}
}

// oneConnectionDSN is dsn with the pool held to one connection (pgx's pool_max_conns),
// in either spelling pgx accepts. It is never printed: it carries the password.
func oneConnectionDSN(t *testing.T, dsn string) string {
	t.Helper()
	if !strings.Contains(dsn, "://") {
		return dsn + " pool_max_conns=1"
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("DATABASE_URL does not parse as a URL") // the parser's message would quote it
	}
	q := u.Query()
	q.Set("pool_max_conns", "1")
	u.RawQuery = q.Encode()
	return u.String()
}

// holdAfterRecord is the real recorder; right after its commit, the one-connection
// pool's only connection is taken and held -- S1's saturated pool, arriving between
// the record and the brand read.
type holdAfterRecord struct {
	checkinRecorder
	data     *db.DB
	tenantID uuid.UUID

	started       bool
	held, release chan struct{}
	done          chan error
	once          sync.Once
}

const holdCap = 10 * time.Second

func (h *holdAfterRecord) Record(ctx context.Context, req checkin.Request) (checkin.Result, error) {
	res, err := h.checkinRecorder.Record(ctx, req)
	if err != nil {
		return res, err
	}
	h.started = true
	go func() {
		h.done <- h.data.WithTenant(context.Background(), h.tenantID, func(context.Context, pgx.Tx) error {
			close(h.held)
			// Held until let go, or for holdCap at most: a read that does not end with
			// its context then shows up as a measured duration, not a deadlocked test.
			select {
			case <-h.release:
			case <-time.After(holdCap):
			}
			return nil
		})
	}()
	select {
	case <-h.held:
		return res, nil
	case <-time.After(5 * time.Second):
		return res, errors.New("test: the pool's connection could not be taken")
	}
}

// letGo releases the held connection once and reports how holding it ended.
func (h *holdAfterRecord) letGo() error {
	var err error
	h.once.Do(func() {
		close(h.release)
		if h.started {
			err = <-h.done
		}
	})
	return err
}

// TestTapDB_ARecordedTapIsConfirmedInFullWhileThePoolIsHeld is B1 of WL-9's round 3
// against Postgres: a pool of ONE connection (pool_max_conns=1), the real record, and
// right after its commit that connection taken and held, so the brand read waits on
// the pool. The request's deadline is 1.5 s (chi's Timeout behind httpx.RequestID, as
// in production), so less of it is left than the read's 2 s bound. The answer is a 200
// with the whole confirmation and no logo; the request took at least the read's own
// bound (the request deadline did not end it) and less than a second and a half over;
// the database holds exactly the one record; one ERROR line carries the request's
// request_id. CONTROL: the same pool and chain with nothing held draws the business's
// logo, so the hold is what cost it.
func TestTapDB_ARecordedTapIsConfirmedInFullWhileThePoolIsHeld(t *testing.T) {
	h := newTapHarness(t)
	kek, err := hex.DecodeString(tapFakeKEK)
	if err != nil {
		t.Fatalf("kek: %v", err)
	}
	logo := h.brandBusiness(t, h.tenantID, 47, "DA291C")

	cfg := *h.cfg
	cfg.DatabaseURL = oneConnectionDSN(t, h.cfg.DatabaseURL)
	one, err := db.New(context.Background(), &cfg)
	if err != nil {
		t.Fatalf("db.New with one connection: %v", err)
	}
	t.Cleanup(one.Close)
	sessions, err := session.New(one, &cfg)
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	dir, err := tenant.NewDirectory(one)
	if err != nil {
		t.Fatalf("tenant.NewDirectory: %v", err)
	}
	trail, err := audit.New(one)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	recorder, err := checkin.New(one, trail, &cfg, discardLogger())
	if err != nil {
		t.Fatalf("checkin.New: %v", err)
	}
	holder := &holdAfterRecord{checkinRecorder: recorder, data: one, tenantID: h.tenantID,
		held: make(chan struct{}), release: make(chan struct{}), done: make(chan error, 1)}
	// Registered after one.Close, so it runs first: Close waits for a held connection.
	t.Cleanup(func() {
		if err := holder.letGo(); err != nil {
			t.Errorf("holding the connection: %v", err)
		}
	})

	post := func(rec checkinRecorder, c tapContext, cookie *http.Cookie, requestID string) (*httptest.ResponseRecorder, string, time.Duration) {
		t.Helper()
		var logs bytes.Buffer
		tp, err := NewTap(sun.NewVerifier(one, kek), dir, sessions, rec, trail, &cfg,
			slog.New(httpx.WithRequestID(slog.NewTextHandler(&logs, nil))))
		if err != nil {
			t.Fatalf("NewTap: %v", err)
		}
		r := chi.NewRouter()
		tp.Mount(r)
		chain := httpx.RequestID(middleware.Timeout(1500 * time.Millisecond)(r))
		signed, err := tp.contexts.mint(c, h.sessionIDOf(t, cookie))
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/checkin", strings.NewReader(url.Values{"ctx": {signed}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set(httpx.RequestIDHeader, requestID)
		req.RemoteAddr = "203.0.113.5:41234" // inside the fixture location's 203.0.113.0/24
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		start := time.Now()
		chain.ServeHTTP(w, req)
		return w, logs.String(), time.Since(start)
	}

	// CONTROL: a colleague's tap on the same pool, nothing held -- the logo is drawn.
	colleague := h.newEmployee(t, "active")
	w, logs, _ := post(recorder, h.nfcContext(701), h.cookieForEmployee(t, colleague), "wl9-r3-db-control")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "/t/logo/"+logo.SHA256) || strings.Contains(logs, "level=ERROR") {
		t.Fatalf("control: status %d, logs:\n%s\nbody:\n%s", w.Code, logs, w.Body.String())
	}

	if n := h.countFor(t, h.employeeID); n != 0 {
		t.Fatalf("fixture: %d record(s) before the tap", n)
	}
	w, logs, took := post(holder, h.nfcContext(702), h.sessionCookieFor(t), "wl9-r3-db-held")
	if err := holder.letGo(); err != nil {
		t.Fatalf("holding the connection: %v", err)
	}
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, "<title>Tapped — Taptime</title>") || !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Fatalf("status %d, %d bytes: the recorded tap's confirmation is not whole:\n%s", w.Code, len(body), body)
	}
	if strings.Contains(body, "/t/logo/") || w.Result().Header.Get("Content-Security-Policy") != tapPolicy {
		t.Fatalf("the held pool's confirmation draws a logo or names %q", w.Result().Header.Get("Content-Security-Policy"))
	}
	if took < resultBrandWait || took > resultBrandWait+1500*time.Millisecond {
		t.Fatalf("the request took %v; want the read's own bound (%v) and at most a second and a half more",
			took.Round(time.Millisecond), resultBrandWait)
	}
	if n := h.countFor(t, h.employeeID); n != 1 {
		t.Fatalf("%d record(s) after the tap; want exactly 1", n)
	}
	if errs := errorLines(logs); len(errs) != 1 || !strings.Contains(errs[0], "request_id=wl9-r3-db-held") {
		t.Fatalf("want one ERROR line carrying request_id=wl9-r3-db-held, got:\n%s", logs)
	}
}
