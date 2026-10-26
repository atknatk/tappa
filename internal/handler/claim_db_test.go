package handler

// claim_db_test.go -- the result screen's claim card and POST /api/claim through the real router,
// the real decision engine, the real claim service and real Postgres (M15-17, ADR 0032 Faz C).
// Every test builds its own tenant through newTapHarness, whose employee carries a business day far
// from now (fixtures.GiveFarDayCutoff), so a chain written relative to now never meets a cutoff.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tap"
)

// dayCutoff is the harness employee's business day: their own cutoff, in the tenant's zone.
func (h *tapHarness) dayCutoff(t *testing.T) tap.DayCutoff {
	t.Helper()
	var micros int64
	var zone string
	err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXTRACT(EPOCH FROM e.day_cutoff)::bigint * 1000000, t.timezone
		                         FROM employees e JOIN tenants t ON t.id = e.tenant_id
		                         WHERE e.tenant_id = $1 AND e.id = $2`, h.tenantID, h.employeeID).Scan(&micros, &zone)
	})
	if err != nil {
		t.Fatalf("read the day cutoff: %v", err)
	}
	return tap.DayCutoff{Cutoff: time.Duration(micros) * time.Microsecond, Timezone: zone}
}

// seedDirected commits a decided nfc tap of the harness employee `ago` in the past, in both
// stamps, WITH its frozen business day -- what the engine writes since M15 (a pre-M15 row is
// openCheckInAgedBy's, with none).
func (h *tapHarness) seedDirected(t *testing.T, direction string, ago time.Duration) uuid.UUID {
	t.Helper()
	at := time.Now().Add(-ago)
	s, ok := tap.BusinessDayStart(at, h.dayCutoff(t))
	if !ok {
		t.Fatal("no business day for the seed")
	}
	var id uuid.UUID
	err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, type, occurred_at, created_at,
			                           verdict, channel, sun_valid, trust, business_day_start, direction_source)
			 VALUES ($1, $2, $3, $4, $5, $6, $6, 'ok', 'nfc', true, 100, $7, 'auto') RETURNING id`,
			h.tenantID, h.employeeID, h.locationID, h.tagUID, direction, at, s).Scan(&id)
	})
	if err != nil {
		t.Fatalf("seeding a directed tap: %v", err)
	}
	return id
}

func (h *tapHarness) claimRows(t *testing.T) int {
	t.Helper()
	var n int
	err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM punch_claims WHERE tenant_id = $1 AND employee_id = $2`,
			h.tenantID, h.employeeID).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count claims: %v", err)
	}
	return n
}

func (h *tapHarness) postClaim(t *testing.T, cookie *http.Cookie, signed, at string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"ctx": {signed}, "at": {at}}
	req := httptest.NewRequest(http.MethodPost, ClaimPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "203.0.113.5:41234"
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)
	return w
}

// cardContextOn is the claim card's signed context on a result screen, or "" without a card.
func cardContextOn(body string) string {
	if !strings.Contains(body, "data-claim-card") {
		return ""
	}
	m := ctxFieldRE.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return m[1]
}

// TestClaimDB_YesterdaysOpenCheckInIsAskedAndSentOnce is the card end to end: an open check-in of
// the previous business day, then a real tap -- recorded `in`, the card on its screen asking "What
// time did you leave?", the left-open sentence not on the screen and still in the record's note
// (N6). The answer, inside the window, is "Sent" and one punch_claims row; the second "Send" of the
// same card is "Sent" again (§C.5: never a 500) and still one row; no `transactions` row is
// written by either.
func TestClaimDB_YesterdaysOpenCheckInIsAskedAndSentOnce(t *testing.T) {
	h := newTapHarness(t)
	anchor := h.seedDirected(t, "in", 20*time.Hour)
	cookie := h.sessionCookieFor(t)
	w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+1), cookie, nil)
	rec := h.lastRecord(t, h.employeeID)
	body := w.Body.String()
	if w.Code != http.StatusOK || rec.Verdict != "ok" || rec.Type == nil || *rec.Type != "in" {
		t.Fatalf("status %d, record %s/%v; want 200 and an ok `in`", w.Code, rec.Verdict, rec.Type)
	}
	signed := cardContextOn(body)
	if signed == "" || !strings.Contains(body, "What time did you leave?") || !strings.Contains(body, "Send to my manager") {
		t.Fatalf("no checkout card on the screen:\n%s", body)
	}
	if strings.Contains(body, noteLeftOpenScreen) {
		t.Error("the left-open sentence is on the screen beside the card that asks it")
	}
	if rec.Note == nil || !strings.Contains(*rec.Note, noteLeftOpenRecord) {
		t.Errorf("the record's note %v lost the left-open annotation", rec.Note)
	}
	card, err := h.claims.Offer(context.Background(), h.tenantID, h.employeeID, rec.ID)
	if err != nil || card == nil || card.AnchorID != anchor {
		t.Fatalf("Offer(the tap) = %+v, %v; want the card about %s", card, err, anchor)
	}
	loc, err := time.LoadLocation(h.dayCutoff(t).Timezone)
	if err != nil {
		t.Fatal(err)
	}
	at := card.Window.From.At.Add(time.Hour).In(loc).Format("15:04")
	before := h.countFor(t, h.employeeID)
	for i := 0; i < 2; i++ {
		w := h.postClaim(t, cookie, signed, at)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Sent — your manager will review it.") {
			t.Fatalf("send %d: status %d\n%s", i+1, w.Code, w.Body.String())
		}
		if n := h.claimRows(t); n != 1 {
			t.Fatalf("after send %d: %d claim rows, want 1", i+1, n)
		}
	}
	if after := h.countFor(t, h.employeeID); after != before {
		t.Errorf("sending a claim wrote %d transactions rows", after-before)
	}
}

// TestClaimDB_AnAnchorWithNoBusinessDayShowsNoCard is Ü-Y2: a check-in written before M15 (no
// business_day_start) left open by the next tap gets no card -- "What time did you leave?" would be
// the wrong question for a chain the release reversed -- and its left-open sentence is shown.
func TestClaimDB_AnAnchorWithNoBusinessDayShowsNoCard(t *testing.T) {
	h := newTapHarness(t)
	h.openCheckInAgedBy(t, 20*time.Hour)
	w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+1), h.sessionCookieFor(t), nil)
	body := w.Body.String()
	if rec := h.lastRecord(t, h.employeeID); w.Code != http.StatusOK || rec.Type == nil || *rec.Type != "in" {
		t.Fatalf("status %d, type %v; want 200 and an `in` that left the old check-in open", w.Code, rec.Type)
	}
	if strings.Contains(body, "data-claim-card") {
		t.Fatalf("a card for an anchor with no business day:\n%s", body)
	}
	if !strings.Contains(body, noteLeftOpenScreen) {
		t.Errorf("without a card the left-open sentence must be shown:\n%s", body)
	}
}

// TestClaimDB_TheCardIsShownOnTwoTapsOnly is K4 through the real chain: A open since the previous
// business day; T1 (`in`, A's first showing) and T2 (the first `out` after T1 in T1's day, the
// second) both have the card about A; a real tap after them -- the `out` closing a later `in` --
// has none, and neither does an ignored tap nor a refused one.
func TestClaimDB_TheCardIsShownOnTwoTapsOnly(t *testing.T) {
	h := newTapHarness(t)
	a := h.seedDirected(t, "in", 20*time.Hour)
	t1 := h.seedDirected(t, "in", 50*time.Minute)
	t2 := h.seedDirected(t, "out", 40*time.Minute)
	h.seedDirected(t, "in", 30*time.Minute)
	for name, id := range map[string]uuid.UUID{"T1": t1, "T2": t2} {
		card, err := h.claims.Offer(context.Background(), h.tenantID, h.employeeID, id)
		if err != nil || card == nil || card.AnchorID != a {
			t.Fatalf("Offer(%s) = %+v, %v; want the card about A", name, card, err)
		}
	}
	cookie := h.sessionCookieFor(t)
	w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+1), cookie, nil)
	if rec := h.lastRecord(t, h.employeeID); w.Code != http.StatusOK || rec.Type == nil || *rec.Type != "out" {
		t.Fatalf("status %d, type %v; want 200 and an `out`", w.Code, rec.Type)
	}
	if strings.Contains(w.Body.String(), "data-claim-card") {
		t.Errorf("a third showing:\n%s", w.Body.String())
	}
	w = h.postTap(t, h.nfcContext(uint32(h.startCtr)+2), cookie, nil)
	if rec := h.lastRecord(t, h.employeeID); rec.Verdict != "ignored" || strings.Contains(w.Body.String(), "data-claim-card") {
		t.Errorf("the debounced tap: verdict %s, card %v; want ignored and no card", rec.Verdict, strings.Contains(w.Body.String(), "data-claim-card"))
	}
	h.retireTag(t, h.tagUID, "retired")
	w = h.postTap(t, h.nfcContext(uint32(h.startCtr)+3), cookie, nil)
	if rec := h.lastRecord(t, h.employeeID); rec.Verdict != "reject" || strings.Contains(w.Body.String(), "data-claim-card") {
		t.Errorf("the refused tap: verdict %s, card %v; want reject and no card", rec.Verdict, strings.Contains(w.Body.String(), "data-claim-card"))
	}
}

// TestClaimDB_AContextIsBoundToItsSessionAndItsLabel is the cross-label and cross-session negative
// test against Postgres: the card's context sent from another session of the same person is a 400
// and writes no claim; the tap's own context sent to /api/claim is a 400 and writes no claim; the
// card's context sent to /api/checkin is a 400 and writes no record. Control: the card's own
// session sends it.
func TestClaimDB_AContextIsBoundToItsSessionAndItsLabel(t *testing.T) {
	h := newTapHarness(t)
	h.seedDirected(t, "in", 20*time.Hour)
	cookie := h.sessionCookieFor(t)
	tapSigned, err := h.tap.contexts.mint(h.nfcContext(uint32(h.startCtr)+1), h.sessionIDOf(t, cookie))
	if err != nil {
		t.Fatal(err)
	}
	w := h.postSigned(t, tapSigned, cookie, "203.0.113.5:41234", nil)
	signed := cardContextOn(w.Body.String())
	if signed == "" {
		t.Fatalf("fixture: no card\n%s", w.Body.String())
	}
	rec := h.lastRecord(t, h.employeeID)
	card, err := h.claims.Offer(context.Background(), h.tenantID, h.employeeID, rec.ID)
	if err != nil || card == nil {
		t.Fatalf("fixture: Offer = %v, %v", card, err)
	}
	loc, _ := time.LoadLocation(h.dayCutoff(t).Timezone)
	at := card.Window.From.At.Add(time.Hour).In(loc).Format("15:04")

	other := h.sessionCookieFor(t)
	if w := h.postClaim(t, other, signed, at); w.Code != http.StatusBadRequest {
		t.Errorf("another session's send: status %d, want 400", w.Code)
	}
	if w := h.postClaim(t, cookie, tapSigned, at); w.Code != http.StatusBadRequest {
		t.Errorf("a tap context at /api/claim: status %d, want 400", w.Code)
	}
	if n := h.claimRows(t); n != 0 {
		t.Fatalf("%d claim rows after the refused sends", n)
	}
	before := h.countFor(t, h.employeeID)
	if w := h.postSigned(t, signed, cookie, "203.0.113.5:41234", nil); w.Code != http.StatusBadRequest {
		t.Errorf("a claim context at /api/checkin: status %d, want 400", w.Code)
	}
	if after := h.countFor(t, h.employeeID); after != before {
		t.Errorf("a claim context at /api/checkin wrote %d records", after-before)
	}
	if w := h.postClaim(t, cookie, signed, at); w.Code != http.StatusOK || h.claimRows(t) != 1 {
		t.Fatalf("control: the card's own session: status %d, %d rows", w.Code, h.claimRows(t))
	}
}

// TestClaimDB_ASuspendedBusinessesEmployeeStillSendsTheClaim: POST /api/claim is a tap surface and
// does not know the suspension (ADR 0025, ADR 0032 §C.4) -- the claim is written; deciding it is
// the panel's write, which the suspension does stop (M15-18).
func TestClaimDB_ASuspendedBusinessesEmployeeStillSendsTheClaim(t *testing.T) {
	owner := ownerPoolForTest(t)
	h := newTapHarness(t)
	h.seedDirected(t, "in", 20*time.Hour)
	suspendAsOwner(t, owner, h.tenantID, db.SuspensionSecurityHold)
	cookie := h.sessionCookieFor(t)
	w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+1), cookie, nil)
	signed := cardContextOn(w.Body.String())
	if signed == "" {
		t.Fatalf("no card for a suspended business's tap:\n%s", w.Body.String())
	}
	rec := h.lastRecord(t, h.employeeID)
	card, err := h.claims.Offer(context.Background(), h.tenantID, h.employeeID, rec.ID)
	if err != nil || card == nil {
		t.Fatalf("Offer = %v, %v", card, err)
	}
	loc, _ := time.LoadLocation(h.dayCutoff(t).Timezone)
	if w := h.postClaim(t, cookie, signed, card.Window.From.At.Add(time.Hour).In(loc).Format("15:04")); w.Code != http.StatusOK || h.claimRows(t) != 1 {
		t.Fatalf("status %d, %d rows; want the claim sent", w.Code, h.claimRows(t))
	}
}
