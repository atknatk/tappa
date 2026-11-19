package handler

// liveqr_db_test.go -- M16-20's screen page end to end over REAL Postgres: the panel's service creates
// a screen, the page pairs with its code, polls a drawing, and stops once the screen is removed. And
// the §7 log test of the page: no code, cookie, token or drawing in any line it writes.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/liveqr"
)

type screenDBFixture struct {
	data           *db.DB
	screens        *liveqr.Screens
	tenant, venue  uuid.UUID
	actor          uuid.UUID
	h              http.Handler
	log            *bytes.Buffer
	cookie         string
	code           liveqr.PairingCode
	createdID      uuid.UUID
	createdCodeTxt string
}

func newScreenDBFixture(t *testing.T) *screenDBFixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping the live QR screen page DB test (real Postgres required)")
	}
	d, err := db.New(context.Background(), &config.Config{DatabaseURL: config.NewDSN(dsn)})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(d.Close)
	f := &screenDBFixture{data: d, tenant: uuid.New(), venue: uuid.New(), actor: uuid.New(), log: &bytes.Buffer{}}
	if err := d.WithTenant(context.Background(), f.tenant, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure)
			VALUES ($1, 'm16-20 page', $2, 'bar', 'single')`, f.tenant, "VAT-M1620P-"+f.tenant.String()); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'Rusty Bar')`, f.venue, f.tenant)
		return err
	}); err != nil {
		t.Fatalf("world: %v", err)
	}
	trail, err := audit.New(d)
	if err != nil {
		t.Fatal(err)
	}
	secret := sha256.Sum256([]byte("handler liveqr test secret, not a key"))
	keys, err := liveqr.NewKeys(secret[:])
	if err != nil {
		t.Fatal(err)
	}
	f.screens, err = liveqr.NewScreens(keys, d, trail)
	if err != nil {
		t.Fatal(err)
	}
	f.h = screenRouter(t, f.screens, slog.New(slog.NewTextHandler(f.log, &slog.HandlerOptions{Level: slog.LevelDebug})))
	created, err := f.screens.Create(context.Background(), liveqr.CreateCommand{TenantID: f.tenant, ActorID: f.actor, LocationID: f.venue})
	if err != nil {
		t.Fatal(err)
	}
	f.code, f.createdID, f.createdCodeTxt = created.Code, created.ID, created.Code.ForThePanelOnce()
	return f
}

func (f *screenDBFixture) do(method, path string, form url.Values) *http.Response {
	rec := screenDo(f.h, method, path, form, func(r *http.Request) {
		if f.cookie != "" {
			r.AddCookie(&http.Cookie{Name: liveqr.SessionCookieName, Value: f.cookie})
		}
	})
	return rec.Result()
}

func readBody(t *testing.T, r *http.Response) string {
	t.Helper()
	var b bytes.Buffer
	if _, err := b.ReadFrom(r.Body); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// TestLiveScreenDB_PairShowAndRemove: a wrong code is refused; the panel's code -- typed lower case,
// with spaces -- pairs the browser (303, HttpOnly Lax cookie scoped to /screen); the page then is
// the QR page under the poll's policy; the poll answers a version-6 drawing (41 modules plus the
// quiet zone) valid for the token's 20 seconds and asks again before that; once the panel removes the
// screen the poll answers 410 and clears the cookie, and the page says the screen was removed.
func TestLiveScreenDB_PairShowAndRemove(t *testing.T) {
	f := newScreenDBFixture(t)
	if r := f.do(http.MethodPost, LiveScreenPairPath, url.Values{"code": {"0000-0000-00000"}}); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("a wrong code = %d", r.StatusCode)
	}
	typed := strings.ToLower(strings.ReplaceAll(f.createdCodeTxt, "-", " "))
	r := f.do(http.MethodPost, LiveScreenPairPath, url.Values{"code": {typed}})
	if r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != LiveScreenPath {
		t.Fatalf("pairing = %d to %q", r.StatusCode, r.Header.Get("Location"))
	}
	cks := r.Cookies()
	if len(cks) != 1 || cks[0].Name != liveqr.SessionCookieName || !cks[0].HttpOnly || !cks[0].Secure ||
		cks[0].SameSite != http.SameSiteLaxMode || cks[0].Path != LiveScreenPath || len(cks[0].Value) != 43 {
		t.Fatalf("cookie %+v", cks)
	}
	f.cookie = cks[0].Value
	if r := f.do(http.MethodPost, LiveScreenPairPath, url.Values{"code": {typed}}); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("the code paired twice: %d", r.StatusCode)
	}

	page := f.do(http.MethodGet, LiveScreenPath, nil)
	body := readBody(t, page)
	if page.StatusCode != http.StatusOK || page.Header.Get("Content-Security-Policy") != liveScreenCSP ||
		!strings.Contains(body, "Rusty Bar") || !strings.Contains(body, "livescreen.js") {
		t.Fatalf("the QR page = %d, CSP %q", page.StatusCode, page.Header.Get("Content-Security-Policy"))
	}

	poll := f.do(http.MethodGet, LiveScreenCodePath, nil)
	var code screenCode
	if err := json.Unmarshal([]byte(readBody(t, poll)), &code); err != nil || poll.StatusCode != http.StatusOK {
		t.Fatalf("poll = %d %v", poll.StatusCode, err)
	}
	if poll.Header.Get("Cache-Control") != "no-store" || code.Side != 41+8 || code.ExpiresIn < 19 || code.ExpiresIn > 20 ||
		code.RefreshIn < 16 || code.RefreshIn >= code.ExpiresIn || !regexp.MustCompile(`^(M\d+ \d+h\d+v1h-\d+z)+$`).MatchString(code.Path) {
		t.Fatalf("poll answer %+v", code)
	}
	if again := f.do(http.MethodGet, LiveScreenCodePath, nil); again.StatusCode != http.StatusOK {
		t.Fatalf("second poll = %d", again.StatusCode)
	}

	if _, err := f.screens.Revoke(context.Background(), liveqr.RevokeCommand{TenantID: f.tenant, ActorID: f.actor, DisplayID: f.createdID}); err != nil {
		t.Fatal(err)
	}
	gone := f.do(http.MethodGet, LiveScreenCodePath, nil)
	if gone.StatusCode != http.StatusGone || !strings.Contains(readBody(t, gone), `"removed"`) ||
		!strings.Contains(gone.Header.Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("after removal the poll = %d, Set-Cookie %q", gone.StatusCode, gone.Header.Get("Set-Cookie"))
	}
	after := f.do(http.MethodGet, LiveScreenPath, nil)
	if b := readBody(t, after); after.StatusCode != http.StatusOK || !strings.Contains(b, "This screen was removed") || strings.Contains(b, "livescreen.js") {
		t.Fatalf("after removal the page = %d", after.StatusCode)
	}
}

// TestLiveScreenDB_LogsNoSecret (CLAUDE.md §7, the card's log test): across an unknown code, a
// pairing, the SAME REAL CODE refused a second time, the QR page, two polls and a removal, the page's
// log holds no pairing code (either spelling), no cookie value, no drawing and no run of
// token-shaped characters. The page's own log lines prove the capture.
//
// MUTANT (measured, M16-20 fix round): `"typed", r.PostFormValue("code")` added to refusePairing's
// log line -> red here ("the log carries the code").
func TestLiveScreenDB_LogsNoSecret(t *testing.T) {
	f := newScreenDBFixture(t)
	f.do(http.MethodPost, LiveScreenPairPath, url.Values{"code": {"ZZZZ-ZZZZ-ZZZZZ"}})
	r := f.do(http.MethodPost, LiveScreenPairPath, url.Values{"code": {f.createdCodeTxt}})
	if r.StatusCode != http.StatusSeeOther {
		t.Fatalf("pairing = %d", r.StatusCode)
	}
	f.cookie = r.Cookies()[0].Value
	// THE REAL CODE, REFUSED: the same code a second time -- it is consumed, so the page refuses it
	// and logs the refusal. This is the line a code would leak into (third eye, M16-20 fix round).
	if again := f.do(http.MethodPost, LiveScreenPairPath, url.Values{"code": {f.createdCodeTxt}}); again.StatusCode != http.StatusBadRequest {
		t.Fatalf("the consumed code = %d, want 400", again.StatusCode)
	}
	f.do(http.MethodGet, LiveScreenPath, nil)
	var code screenCode
	if err := json.Unmarshal([]byte(readBody(t, f.do(http.MethodGet, LiveScreenCodePath, nil))), &code); err != nil {
		t.Fatal(err)
	}
	if _, err := f.screens.Revoke(context.Background(), liveqr.RevokeCommand{TenantID: f.tenant, ActorID: f.actor, DisplayID: f.createdID}); err != nil {
		t.Fatal(err)
	}
	f.do(http.MethodGet, LiveScreenCodePath, nil)
	f.do(http.MethodGet, LiveScreenPath, nil)

	got := f.log.String()
	if !strings.Contains(got, "live QR screen paired") || !strings.Contains(got, "pairing refused") {
		t.Fatalf("CONTROL: the page's log was not captured:\n%s", got)
	}
	for name, s := range map[string]string{
		"the code": f.createdCodeTxt, "the code, ungrouped": strings.ReplaceAll(f.createdCodeTxt, "-", ""),
		"the cookie": f.cookie, "the drawing": code.Path[:40],
	} {
		if strings.Contains(got, s) {
			t.Fatalf("the log carries %s", name)
		}
	}
	if m := regexp.MustCompile(`[A-Za-z0-9_-]{60,}`).FindString(got); m != "" {
		t.Fatalf("the log carries a token-shaped run: %q", m)
	}
}

// screenOwnerConn is the migration role's connection: the only role that may write
// tenants.suspended_at in a test. Skipped without DATABASE_MIGRATE_URL.
func screenOwnerConn(t *testing.T) *pgx.Conn {
	t.Helper()
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; the suspension test needs the owner role")
	}
	c, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("owner connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func (f *screenDBFixture) pair(t *testing.T) {
	t.Helper()
	r := f.do(http.MethodPost, LiveScreenPairPath, url.Values{"code": {f.createdCodeTxt}})
	if r.StatusCode != http.StatusSeeOther {
		t.Fatalf("pairing = %d", r.StatusCode)
	}
	f.cookie = r.Cookies()[0].Value
}

func (f *screenDBFixture) pollState(t *testing.T) (int, string) {
	t.Helper()
	r := f.do(http.MethodGet, LiveScreenCodePath, nil)
	var st screenState
	_ = json.Unmarshal([]byte(readBody(t, r)), &st)
	return r.StatusCode, st.State
}

// TestLiveScreenDB_ASuspendedBusinessShowsPausedAndComesBack (security ORTA-2): while the operator
// has suspended the business the poll mints nothing -- 409 "paused" -- and the page shows the paused
// sentence with its script (so it keeps asking), no business name or reason; once the suspension is
// lifted the same cookie gets codes again, no re-pairing.
func TestLiveScreenDB_ASuspendedBusinessShowsPausedAndComesBack(t *testing.T) {
	owner := screenOwnerConn(t)
	f := newScreenDBFixture(t)
	f.pair(t)
	if code, _ := f.pollState(t); code != http.StatusOK {
		t.Fatalf("before = %d", code)
	}
	ctx := context.Background()
	if _, err := owner.Exec(ctx, `UPDATE tenants SET suspended_at = now(), suspension_reason = 'security_hold' WHERE id = $1`, f.tenant); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = owner.Exec(context.Background(), `UPDATE tenants SET suspended_at = NULL, suspension_reason = NULL WHERE id = $1`, f.tenant)
	})
	if code, state := f.pollState(t); code != http.StatusConflict || state != "paused" {
		t.Fatalf("suspended poll = %d %q", code, state)
	}
	page := f.do(http.MethodGet, LiveScreenPath, nil)
	body := readBody(t, page)
	if page.StatusCode != http.StatusOK || !strings.Contains(body, "livescreen.js") ||
		!regexp.MustCompile(`data-qr-state="paused" role="status"`).MatchString(body) || strings.Contains(body, "security_hold") {
		t.Fatalf("the suspended page = %d", page.StatusCode)
	}
	if strings.Contains(body, `data-qr-state="paused" hidden`) {
		t.Fatal("the paused sentence is hidden on a suspended business's page")
	}
	// Nothing to scan while paused: the "Scan…" lines are hidden; the venue's name stays.
	if !strings.Contains(body, `data-qr-caption hidden`) || !strings.Contains(body, "Rusty Bar") {
		t.Fatal("the paused page shows the scan lines, or lost the venue's name")
	}
	if _, err := owner.Exec(ctx, `UPDATE tenants SET suspended_at = NULL, suspension_reason = NULL WHERE id = $1`, f.tenant); err != nil {
		t.Fatal(err)
	}
	if code, _ := f.pollState(t); code != http.StatusOK {
		t.Fatalf("lifted = %d", code)
	}
}

// TestLiveScreenDB_ASwitchedOffVenueComesBackByItself (DÜŞÜK 8): a venue switched off in the panel
// answers the poll 409 "inactive" and its page carries the script, so it keeps asking; switched on,
// the same screen gets codes again.
func TestLiveScreenDB_ASwitchedOffVenueComesBackByItself(t *testing.T) {
	f := newScreenDBFixture(t)
	f.pair(t)
	setActive := func(on bool) {
		if err := f.data.WithTenant(context.Background(), f.tenant, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE locations SET active = $3 WHERE tenant_id = $1 AND id = $2`, f.tenant, f.venue, on)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	setActive(false)
	if code, state := f.pollState(t); code != http.StatusConflict || state != "inactive" {
		t.Fatalf("switched off = %d %q", code, state)
	}
	body := readBody(t, f.do(http.MethodGet, LiveScreenPath, nil))
	if !strings.Contains(body, "livescreen.js") || strings.Contains(body, `data-qr-state="inactive" hidden`) {
		t.Fatal("the switched-off page has no script, or hides its sentence")
	}
	setActive(true)
	if code, _ := f.pollState(t); code != http.StatusOK {
		t.Fatalf("switched on = %d", code)
	}
}
