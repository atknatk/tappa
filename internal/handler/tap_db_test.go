package handler

// The tap page against a REAL Postgres and the REAL router, with the real
// internal/sun verifier and the real middleware stack. What it measures cannot
// be measured with fakes:
//
//   - GET /t writes NO `transactions` row, ever — the §5 row-3 case (no session)
//     and the ordinary case alike. A fake handler cannot prove the absence of a
//     write; a row count can.
//   - Opening the page N times does not move tags.last_ctr (M5-04's central
//     requirement, §4.4), with a POSITIVE CONTROL showing the counter can move
//     and that the assertion is reading a live row.
//   - A plaque belonging to another tenant renders without that tenant's venue
//     name, under real RLS rather than a fake that was told to say so.
//
// Fixtures are NOT cleaned up (tappa_app has REVOKE DELETE on tags and
// transactions). Fresh random ids keep runs from colliding; `make db-reset`
// clears the dev database.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/internal/sun"
)

// harnessDebounce is the person-debounce window every DB test runs under. See
// the config block in newTapHarness for why it must not be the shipped default.
const harnessDebounce = 120 * time.Second

// tapFakeKEK and tapFakeTagKey are OBVIOUSLY FAKE (agent-brief madde 2), used
// only to wrap a per-tag key in a throwaway fixture row.
const (
	tapFakeKEK    = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	tapFakeTagKey = "000102030405060708090A0B0C0D0E0F"
)

type tapHarness struct {
	router     http.Handler
	tap        *Tap
	checkins   *checkin.Service
	trail      *audit.Recorder
	data       *db.DB
	cookies    session.Cookies
	sessions   *session.Manager
	tenantID   uuid.UUID
	locationID uuid.UUID
	employeeID uuid.UUID
	tagUID     string
	startCtr   int32
}

func newTapHarness(t *testing.T) *tapHarness {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping tap page end-to-end tests (real Postgres required)")
	}
	kek, err := hex.DecodeString(tapFakeKEK)
	if err != nil {
		t.Fatalf("kek: %v", err)
	}
	cfg := &config.Config{
		Env:            config.EnvDev,
		BaseURL:        "http://localhost:8080",
		DatabaseURL:    dsn,
		SessionHMACKey: []byte("SSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSS"),
		InviteHMACKey:  []byte("IIIIIIIIIIIIIIIIIIIIIIIIIIIIIIII"),
		TagKEK:         kek,
		RetentionYears: 2,
		// The decision engine's two configured thresholds, set explicitly because a
		// zero radius would silently make every GPS fix a miss and a zero debounce
		// would disable §5 row 5.
		//
		// 🔴 THE DEBOUNCE IS DELIBERATELY *NOT* THE SHIPPED DEFAULT. It was 60 s
		// here, which is exactly what policy.DefaultParams() hard-codes — so the one
		// line that carries TAPPA_DEBOUNCE_SECONDS into the guardrail (hand-off N3)
		// could be DELETED and the whole suite stayed green. An audit measured that:
		// the same shape as the M5-04 finding, a criterion that cannot fail, only
		// with a degenerate VALUE instead of a self-configured object. 120 s is
		// inside the ADR 0004 §11 range [30, 300] and differs from the default, so a
		// tap 90 seconds after the last one is `ignored` under the configured window
		// and RECORDED under the shipped one — see
		// TestCheckinDB_ConfiguredDebounceWindowReachesTheGuardrail.
		GPSRadiusMeters: 150,
		Debounce:        harnessDebounce,
	}

	data, err := db.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)

	sessions, err := session.New(data, cfg)
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	directory, err := tenant.NewDirectory(data)
	if err != nil {
		t.Fatalf("tenant.NewDirectory: %v", err)
	}
	// The REAL verifier, given only its non-advancing entry point by the
	// handler's consumer-side interface.
	trail, err := audit.New(data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	// THE REAL orchestrator, not a fake: this harness serves POST /api/checkin as
	// well, and the whole point of the DB tests is that the decision, the counter
	// and the row are the production ones.
	checkins, err := checkin.New(data, trail, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("checkin.New: %v", err)
	}
	tp, err := NewTap(sun.NewVerifier(data, kek), directory, sessions, checkins, trail, cfg,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewTap: %v", err)
	}
	r := chi.NewRouter()
	tp.Mount(r)

	h := &tapHarness{
		router: r, tap: tp, checkins: checkins, trail: trail, data: data, cookies: session.NewCookies(cfg), sessions: sessions,
		tenantID: uuid.New(), locationID: uuid.New(), employeeID: uuid.New(),
		startCtr: 700,
	}
	h.tagUID = h.newTag(t, kek, h.tenantID, h.locationID, h.startCtr)
	return h
}

// newTag commits a tenant + location + employee + one active tag holding
// tapFakeTagKey wrapped under kek.
func (h *tapHarness) newTag(t *testing.T, kek []byte, tenantID, locationID uuid.UUID, startCtr int32) string {
	t.Helper()
	// A fresh random 7-byte uid per fixture: tappa_app cannot DELETE tags
	// (redline R5), so runs must not collide. Uppercase, matching tags.uid and
	// sun.Parse's canonical form.
	uidBytes := make([]byte, 7)
	if _, err := rand.Read(uidBytes); err != nil {
		t.Fatalf("rand: %v", err)
	}
	uid := strings.ToUpper(hex.EncodeToString(uidBytes))
	tagKey, err := hex.DecodeString(tapFakeTagKey)
	if err != nil {
		t.Fatalf("tag key: %v", err)
	}
	ref, err := sun.Wrap(kek, uidBytes, tagKey)
	if err != nil {
		t.Fatalf("sun.Wrap: %v", err)
	}
	err = h.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Kebab Factory Ltd', $2, 'restaurant', 'multi')
			 ON CONFLICT (id) DO NOTHING`,
			tenantID, "VAT-"+tenantID.String()[:8]); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name, static_ips, gps_lat, gps_lng)
			 VALUES ($1, $2, 'St Julians', '{203.0.113.0/24}', 35.918, 14.489)`,
			locationID, tenantID); e != nil {
			return e
		}
		if tenantID == h.tenantID {
			if _, e := tx.Exec(ctx,
				`INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at)
				 VALUES ($1, $2, $3, 'Maria Borg', 'active', now())`,
				h.employeeID, tenantID, locationID); e != nil {
				return e
			}
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status)
			 VALUES ($1, $2, $3, $4, $5, 'active')`,
			uid, tenantID, locationID, ref, startCtr)
		return e
	})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return uid
}

// sessionCookieFor issues a real session and returns the cookie a browser would
// hold. The raw token can only leave internal/session through Cookies.Set, so
// that is how it is obtained here — no test-only accessor exists or is needed.
func (h *tapHarness) sessionCookieFor(t *testing.T) *http.Cookie {
	t.Helper()
	issued, err := h.sessions.Issue(context.Background(), session.IssueParams{
		TenantID: h.tenantID, EmployeeID: h.employeeID, DeviceInfo: "test",
	})
	if err != nil {
		t.Fatalf("session.Issue: %v", err)
	}
	rec := httptest.NewRecorder()
	if err := h.cookies.Set(rec, issued.Token); err != nil {
		t.Fatalf("cookies.Set: %v", err)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == session.CookieName {
			return c
		}
	}
	t.Fatal("no session cookie was written")
	return nil
}

// tapURLFor builds a SUN URL for uid. The CMAC is deliberately WRONG — see the
// note on TestTapDB_PageNeverMovesTheCounter for why that costs this file
// nothing and what covers the valid-CMAC case instead.
func tapURLFor(uid string) string {
	return "/t?tag=" + uid + "&ctr=0002bd&cmac=0000000000000000"
}

func (h *tapHarness) lastCtr(t *testing.T, uid string) int32 {
	t.Helper()
	tag, err := h.data.GetTagByUID(context.Background(), uid)
	if err != nil {
		t.Fatalf("read tag: %v", err)
	}
	return tag.LastCtr
}

func (h *tapHarness) transactionCount(t *testing.T) int {
	t.Helper()
	var n int
	err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM transactions WHERE tenant_id = $1`, h.tenantID).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	return n
}

// auditCount counts rows for one action inside this harness's tenant. It runs
// under WithTenant, so RLS scopes it and other runs' fixtures cannot inflate it.
func (h *tapHarness) auditCount(t *testing.T, action string) int {
	t.Helper()
	var n int
	err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2`,
			h.tenantID, action).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count audit_log: %v", err)
	}
	return n
}

func (h *tapHarness) get(t *testing.T, target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.RemoteAddr = "203.0.113.5:41234"
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)
	return w
}

// ---------------------------------------------------------------------------

// TestTapDB_SessionlessTapRedirectsAndWritesNothing is §5 row 3 end to end,
// including the half a fake cannot show: the row count.
func TestTapDB_SessionlessTapRedirectsAndWritesNothing(t *testing.T) {
	h := newTapHarness(t)
	before := h.transactionCount(t)

	for i := 0; i < 5; i++ {
		w := h.get(t, tapURLFor(h.tagUID))
		if w.Code != http.StatusSeeOther {
			t.Fatalf("open %d: status = %d, want 303", i, w.Code)
		}
		if loc := w.Header().Get("Location"); loc != "/activate" {
			t.Fatalf("open %d: Location = %q, want /activate", i, loc)
		}
	}

	if after := h.transactionCount(t); after != before {
		t.Fatalf("transactions went from %d to %d: §5 row 3 forbids a record for a session-less tap", before, after)
	}
	if got := h.lastCtr(t, h.tagUID); got != h.startCtr {
		t.Fatalf("last_ctr = %d, want %d unchanged", got, h.startCtr)
	}
}

// TestTapDB_PageNeverMovesTheCounter. Opening the page repeatedly leaves
// tags.last_ctr exactly where it was (§4.4, M5-04's central requirement), and
// writes no attendance record.
//
// THE URL CARRIES AN INVALID CMAC, and that is a deliberate limit of THIS file
// rather than a gap in the proof. Minting a valid one here would mean
// reimplementing the SDM MAC derivation outside internal/sun — a second copy of
// a cryptographic construction, which is the exact failure mode that package
// spends two files avoiding. The valid-CMAC case is measured where the
// derivation lives, against the same real Postgres and with its own positive
// control: internal/sun's TestPreview_AgainstPostgresDoesNotMoveTheCounter opens
// the preview twenty times with a GENUINE MAC, finds last_ctr unchanged, then
// calls Verify and watches it move.
//
// What this file adds on top is the HTTP path: a real router, a real session, a
// real RLS-scoped read, and a row count.
func TestTapDB_PageNeverMovesTheCounter(t *testing.T) {
	h := newTapHarness(t)
	cookie := h.sessionCookieFor(t)
	before := h.transactionCount(t)

	const opens = 10
	for i := 0; i < opens; i++ {
		w := h.get(t, tapURLFor(h.tagUID), cookie)
		if w.Code != http.StatusOK {
			t.Fatalf("open %d: status = %d, want 200 (a failed pre-check still renders, so the reject gets RECORDED at POST)", i, w.Code)
		}
		if !strings.Contains(w.Body.String(), "Hello Maria Borg") {
			t.Fatalf("open %d: the page did not greet the employee", i)
		}
		if !strings.Contains(w.Body.String(), "St Julians") {
			t.Fatalf("open %d: the page did not name the tapped venue", i)
		}
	}

	if got := h.lastCtr(t, h.tagUID); got != h.startCtr {
		t.Fatalf("last_ctr = %d after %d page opens, want %d UNCHANGED — the page spent the chip's counter (§4.4)",
			got, opens, h.startCtr)
	}
	if after := h.transactionCount(t); after != before {
		t.Fatalf("transactions went from %d to %d: GET /t records nothing, the decision is POST's", before, after)
	}

	// POSITIVE CONTROL: the counter CAN move on this row, and lastCtr is reading
	// the row that moves. Without this, "unchanged" would also be true of a test
	// pointed at the wrong tag.
	err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := sun.AdvanceCounter(ctx, store.New(tx), h.tenantID, h.tagUID, uint32(h.startCtr)+1)
		return e
	})
	if err != nil {
		t.Fatalf("positive control: advancing the counter directly failed: %v", err)
	}
	if got := h.lastCtr(t, h.tagUID); got != h.startCtr+1 {
		t.Fatalf("positive control: last_ctr = %d, want %d — the assertion above proves nothing", got, h.startCtr+1)
	}
}

// TestTapDB_ForeignPlaqueRendersWithoutTheVenueName: a tap on ANOTHER tenant's
// plaque, under real RLS. The page renders (the tenant decision is
// sys:tenant-mismatch's at POST time, and it needs a record) but the other
// tenant's venue name does not.
func TestTapDB_ForeignPlaqueRendersWithoutTheVenueName(t *testing.T) {
	h := newTapHarness(t)
	kek, err := hex.DecodeString(tapFakeKEK)
	if err != nil {
		t.Fatalf("kek: %v", err)
	}
	otherTenant, otherLocation := uuid.New(), uuid.New()
	foreignUID := h.newTag(t, kek, otherTenant, otherLocation, 100)
	cookie := h.sessionCookieFor(t)
	before := h.transactionCount(t)

	w := h.get(t, tapURLFor(foreignUID), cookie)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — refusing here would decide sys:tenant-mismatch, and decide it without a record", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Hello Maria Borg") {
		t.Fatal("the employee's own name was dropped")
	}
	if strings.Contains(body, "St Julians") {
		t.Fatal("the other tenant's venue name was rendered to this employee")
	}
	if !strings.Contains(body, `name="ctx"`) {
		t.Fatal("no tap context: the attempt could not reach the decision engine to be recorded")
	}
	if after := h.transactionCount(t); after != before {
		t.Fatalf("transactions went from %d to %d", before, after)
	}
}

// TestTapDB_RefusedTapWritesExactlyOneAuditRow reproduces the audit's own probe
// against real Postgres and counts the table it said was never touched.
//
// The finding was that handler.NewTap built TapLimiter WITHOUT a recorder, so
// 300 requests produced 285x200 / 15x429 and audit_log stayed at 4145 — half of
// an ACCEPTED M5-03 criterion silently not happening. The row count is the only
// thing that can show this; a fake proves the call is made, not that a row lands
// in an append-only table.
func TestTapDB_RefusedTapWritesExactlyOneAuditRow(t *testing.T) {
	h := newTapHarness(t)
	cookie := h.sessionCookieFor(t)

	before := h.auditCount(t, httpx.ActionTapRateLimited)
	beforeTxn := h.transactionCount(t)

	// 🔑 THE REAL LIMITER, AT ITS REAL BUDGET. An earlier version of this test
	// swapped in a narrow limiter of its own — which passed Audit explicitly and
	// therefore could not notice that NewTap did not. Driving the production
	// budget costs a few hundred requests and is the only shape that fails when
	// the wiring is removed. The SESSION budget is the one under test: it is the
	// only one that refuses AFTER identity resolves, so it is the only one whose
	// refusals can name a tenant and be written down at all.
	served, refused := 0, 0
	for i := 0; i < httpx.TapSessionLimit()+3; i++ {
		switch h.get(t, tapURLFor(h.tagUID), cookie).Code {
		case http.StatusOK:
			served++
		case http.StatusTooManyRequests:
			refused++
		}
	}
	if served == 0 || refused == 0 {
		t.Fatalf("served=%d refused=%d — the probe must produce both", served, refused)
	}

	after := h.auditCount(t, httpx.ActionTapRateLimited)
	if after != before+1 {
		t.Fatalf("%s rows went %d -> %d for %d refusals, want exactly one more: "+
			"0 new rows is the audit's finding (recorder never wired); more than one means "+
			"the refusal path can flood an append-only table", httpx.ActionTapRateLimited, before, after, refused)
	}
	if got := h.transactionCount(t); got != beforeTxn {
		t.Fatalf("transactions went %d -> %d: a refused tap must not write an attendance record either", beforeTxn, got)
	}
}

// TestTapDB_UnknownPlaqueIsRefused: a uid that resolves to nothing has no tenant
// and no location, so there is nothing to render around and nothing to record
// against.
func TestTapDB_UnknownPlaqueIsRefused(t *testing.T) {
	h := newTapHarness(t)
	cookie := h.sessionCookieFor(t)
	before := h.transactionCount(t)

	w := h.get(t, "/t?tag=AABBCCDDEEFF01&ctr=000001&cmac=0000000000000000", cookie)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if after := h.transactionCount(t); after != before {
		t.Fatalf("transactions went from %d to %d", before, after)
	}
}
