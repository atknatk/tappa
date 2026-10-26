package handler

// m15_e2e_db_test.go -- M15's acceptance scenario end to end (M15-19, ADR 0032 Faz C): a forgotten
// checkout, the next business day's tap, the claim card on its result screen, POST /api/claim, the
// manager's decision on the Spike's "Time claims", then the report and the anomaly list -- every step
// through a real router over real Postgres, with the production decision engine, claim service,
// manual writer and ledger reader.
//
// TWO ROUTERS, ONE BUSINESS. The tap surface (tapHarness: /api/checkin, /api/claim) and the panel
// (panelHarness: /admin/...) are separate harnesses; the panel is signed in as an owner written into
// the TAP harness's tenant, so both drive the same business, the same person and the same rows.
//
// THE FORGOTTEN CHECK-IN IS WRITTEN, THE TAP IS REAL. A check-in a day old cannot be tapped on a real
// clock, so it is inserted as the engine writes one since M15 (nfc, `ok`, its business day frozen on
// the row). The next tap is a real POST /api/checkin decided by the engine at the server's now. The
// person's business day is kept far from now (newTapHarness → fixtures.GiveFarDayCutoff: the
// boundary ~15 h behind now), so the check-in 25 h back is the previous business day's and the
// claimed checkout 8 h after it is inside that day -- 2 h from the boundary, more than the cutoff's
// 15-minute rounding and a DST step.
//
// THE SHIFT IS THE CLAIMED INTERVAL. The venue's shift is set to the check-in's wall clock and 8 h
// later, so the claimed checkout is inside the §A.7 band and Approve is ONE press; the band's step
// is TestClaimQueueDB_OnePressInTheBandTwoOutsideAndEveryRefusalWritesNothing's.

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/tap"
)

// forgottenCheckout is the scenario's business after the forgotten check-in and before the tap.
type forgottenCheckout struct {
	h        *tapHarness
	p        *panelHarness
	reader   *ledger.Reader
	zone     *time.Location
	anchor   uuid.UUID // the forgotten check-in
	anchorAt time.Time
	leftAt   time.Time // the checkout the person claims: anchorAt + 8 h
	day      ledger.Date
}

func newForgottenCheckout(t *testing.T) *forgottenCheckout {
	t.Helper()
	h := newTapHarness(t)
	cutoff := h.dayCutoff(t)
	zone, err := time.LoadLocation(cutoff.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	f := &forgottenCheckout{h: h, zone: zone}
	f.anchorAt = time.Now().Add(-25 * time.Hour).Truncate(time.Minute)
	f.leftAt = f.anchorAt.Add(8 * time.Hour)
	local := f.anchorAt.In(zone)
	f.day = ledger.Date{Year: local.Year(), Month: local.Month(), Day: local.Day()}

	s, ok := tap.BusinessDayStart(f.anchorAt, cutoff)
	if !ok {
		t.Fatal("no business day for the forgotten check-in")
	}
	next, ok := tap.NextBusinessDayStart(s, cutoff)
	if !ok || !f.leftAt.Before(next) || !next.Before(time.Now()) {
		t.Fatalf("PREMISE: the check-in's business day [%v, %v) must hold the claimed %v and end before now", s, next, f.leftAt)
	}
	ctx := context.Background()
	if err := h.data.WithTenant(ctx, h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `UPDATE locations SET shift_start = $3::time, shift_end = $4::time WHERE tenant_id = $1 AND id = $2`,
			h.tenantID, h.locationID, local.Format("15:04"), f.leftAt.In(zone).Format("15:04")); e != nil {
			return e
		}
		return tx.QueryRow(ctx,
			`INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, type, occurred_at, created_at,
			                           verdict, channel, sun_valid, trust, ctr, business_day_start, direction_source)
			 VALUES ($1, $2, $3, $4, 'in', $5, $5, 'ok', 'nfc', true, 100, $6, $7, 'auto') RETURNING id`,
			h.tenantID, h.employeeID, h.locationID, h.tagUID, f.anchorAt, h.startCtr, s).Scan(&f.anchor)
	}); err != nil {
		t.Fatalf("the forgotten check-in: %v", err)
	}

	// The panel, signed in as an owner of THIS business.
	p := newPanelHarnessWith(t, panelHarnessOptions{claims: true})
	p.tenantID, p.adminID = h.tenantID, uuid.New()
	p.email = "m15-e2e-" + uuid.NewString() + "@m15.example"
	if err := p.data.WithTenant(ctx, h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
			 VALUES ($1, $2, 'M15 Owner', $3, $4, 'owner', 'active')`,
			p.adminID, h.tenantID, p.email, ownerDigest)
		return e
	}); err != nil {
		t.Fatalf("the owner: %v", err)
	}
	p.signIn(t)
	f.p = p
	if f.reader, err = ledger.NewReader(p.data, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	return f
}

// paid is the person's counted hours on the check-in's local day, by the panel's own reader.
func (f *forgottenCheckout) paid(t *testing.T) time.Duration {
	t.Helper()
	r, err := f.reader.Hours(context.Background(), f.h.tenantID, ledger.ReportFilter{Day: f.day})
	if err != nil || !r.Queried {
		t.Fatalf("the report: %v (queried %v)", err, r.Queried)
	}
	d := -1
	for i := 0; i < ledger.ReportDays; i++ {
		x := time.Date(r.Period.FirstDay.Year, r.Period.FirstDay.Month, r.Period.FirstDay.Day+i, 12, 0, 0, 0, f.zone)
		if x.Year() == f.day.Year && x.Month() == f.day.Month && x.Day() == f.day.Day {
			d = i
		}
	}
	if d < 0 {
		t.Fatalf("the check-in's day %s is not in the report's week from %s", f.day, r.Period.FirstDay)
	}
	for _, ph := range r.People {
		if ph.EmployeeID == f.h.employeeID {
			return ph.Daily[d]
		}
	}
	return 0
}

// listed is whether the forgotten check-in is on the anomaly list's open check-ins, by its row id,
// and whether it carries the "claim pending" mark.
func (f *forgottenCheckout) listed(t *testing.T) (open, pending bool) {
	t.Helper()
	s, err := f.reader.Anomalies(context.Background(), f.h.tenantID, ledger.ReportFilter{Day: f.day})
	if err != nil {
		t.Fatalf("the anomaly list: %v", err)
	}
	for _, o := range s.Open {
		if o.ID == f.anchor {
			return true, o.ClaimPending
		}
	}
	return false, false
}

func (f *forgottenCheckout) count(t *testing.T, sql string) int {
	t.Helper()
	return countRows(t, f.p, f.h.tenantID, sql, f.h.employeeID)
}

// tapAndClaim is the person's half: the next business day's real tap, its result screen's card and
// one POST /api/claim of the checkout 8 h after the check-in. It returns the claim's id.
func (f *forgottenCheckout) tapAndClaim(t *testing.T) uuid.UUID {
	t.Helper()
	h := f.h
	if open, _ := f.listed(t); !open || f.paid(t) != 0 {
		t.Fatalf("PREMISE: before the tap the check-in is open %v and paid %v; want open and 0", open, f.paid(t))
	}
	cookie := h.sessionCookieFor(t)
	w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+1), cookie, nil)
	rec := h.lastRecord(t, h.employeeID)
	body := w.Body.String()
	if w.Code != http.StatusOK || rec.Verdict != "ok" || rec.Type == nil || *rec.Type != "in" {
		t.Fatalf("the tap: status %d, record %s/%v; want 200 and an ok `in` (a new business day)", w.Code, rec.Verdict, rec.Type)
	}
	signed := cardContextOn(body)
	if signed == "" || !strings.Contains(body, "What time did you leave?") || !strings.Contains(body, "Send to my manager") {
		t.Fatalf("no checkout card on the result screen:\n%s", body)
	}
	if open, _ := f.listed(t); !open {
		t.Fatal("the tap closed the previous business day's check-in")
	}
	before := h.countFor(t, h.employeeID)
	sent := h.postClaim(t, cookie, signed, f.leftAt.In(f.zone).Format("15:04"))
	if sent.Code != http.StatusOK || !strings.Contains(sent.Body.String(), "Sent — your manager will review it.") {
		t.Fatalf("POST %s: %d\n%s", ClaimPath, sent.Code, sent.Body.String())
	}
	if after := h.countFor(t, h.employeeID); after != before {
		t.Fatalf("sending the claim wrote %d transactions row(s); a claim is not a record", after-before)
	}
	var id, anchor uuid.UUID
	var kind string
	var at time.Time
	if err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id, anchor_transaction_id, kind, claimed_at FROM punch_claims WHERE tenant_id = $1 AND employee_id = $2`,
			h.tenantID, h.employeeID).Scan(&id, &anchor, &kind, &at)
	}); err != nil {
		t.Fatalf("the claim row: %v", err)
	}
	if anchor != f.anchor || kind != "out" || !at.Equal(f.leftAt) {
		t.Fatalf("the claim is %s/%s at %v; want an `out` of %s at %v", anchor, kind, at, f.anchor, f.leftAt)
	}
	if open, pending := f.listed(t); !open || !pending {
		t.Fatalf("after the claim: listed %v, marked %v; want the check-in listed and marked", open, pending)
	}
	if _, page := f.p.get(t, anomaliesHref+"?week="+f.day.String()); !strings.Contains(page, "Claim pending") ||
		!strings.Contains(page, "focus="+id.String()) {
		t.Fatal("the anomaly screen does not mark the check-in with its pending claim")
	}
	if _, page := f.p.get(t, claimsHref+"?focus="+id.String()); !strings.Contains(page, "Maria Borg") ||
		!strings.Contains(page, `name="id" value="`+id.String()+`"`) {
		t.Fatal("the Spike's Time claims does not offer the claim in focus")
	}
	return id
}

// decide is the manager's one press on the claim, followed to its banner.
func (f *forgottenCheckout) decide(t *testing.T, id uuid.UUID, outcome, banner string) {
	t.Helper()
	res, page := f.p.post(t, claimsHref, decisionForm(id, outcome))
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("%s answered %d, want 303 (one press -- the claimed checkout is the shift's end):\n%s", outcome, res.StatusCode, page)
	}
	if _, done := f.p.get(t, res.Header.Get("Location")); !strings.Contains(done, banner) {
		t.Errorf("the redirect does not say %q", banner)
	}
}

// TestM15DB_AForgottenCheckoutIsClaimedApprovedAndPaid is M15-19's acceptance scenario: yesterday's
// check-in left open (no checkout), today's tap is an `in` whose result screen asks "What time did you
// leave?", the person sends the checkout, the manager approves it with one press -- and the forgotten
// day is paid 8 h in the report, its entry gone from the anomaly list. The approval is ONE new manual
// row (§4.3: nothing is updated) and one decision.
func TestM15DB_AForgottenCheckoutIsClaimedApprovedAndPaid(t *testing.T) {
	f := newForgottenCheckout(t)
	id := f.tapAndClaim(t)
	rows := f.count(t, `SELECT count(*) FROM transactions WHERE tenant_id = $1 AND employee_id = $2`)

	f.decide(t, id, "approved", "Claim approved")

	if got := f.count(t, `SELECT count(*) FROM transactions WHERE tenant_id = $1 AND employee_id = $2`); got != rows+1 {
		t.Fatalf("the approval wrote %d transactions rows, want 1", got-rows)
	}
	var manualAt time.Time
	var manualType string
	if err := f.h.data.WithTenant(context.Background(), f.h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT occurred_at, type FROM transactions WHERE tenant_id = $1 AND employee_id = $2 AND channel = 'manual'`,
			f.h.tenantID, f.h.employeeID).Scan(&manualAt, &manualType)
	}); err != nil {
		t.Fatalf("the approval's manual row: %v", err)
	}
	if !manualAt.Equal(f.leftAt) || manualType != "out" {
		t.Errorf("the approval's row is a %s at %v, want an `out` at %v", manualType, manualAt, f.leftAt)
	}
	if n := f.count(t, `SELECT count(*) FROM punch_claim_decisions d JOIN punch_claims c ON c.id = d.claim_id AND c.tenant_id = d.tenant_id
	                    WHERE d.tenant_id = $1 AND c.employee_id = $2 AND d.outcome = 'approved'`); n != 1 {
		t.Errorf("%d approved decision(s), want 1", n)
	}

	if got := f.paid(t); got != 8*time.Hour {
		t.Errorf("the forgotten day is paid %v, want 8h", got)
	}
	if _, page := f.p.get(t, reportsHref+"?week="+f.day.String()); !strings.Contains(page, hoursMinutes(8*time.Hour)) {
		t.Error("the report screen does not print the day's 8 h")
	}
	if open, _ := f.listed(t); open {
		t.Error("the approved check-in is still on the anomaly list")
	}
	if _, page := f.p.get(t, anomaliesHref+"?week="+f.day.String()); strings.Contains(page, "focus="+id.String()) {
		t.Error("the anomaly screen still marks the decided claim")
	}
}

// TestM15DB_ARejectedClaimLeavesTheForgottenDayListed is the scenario's other ending: the manager
// rejects the claim -- one decision, no record -- and the forgotten day stays unpaid and stays on the
// anomaly list (no longer marked pending), where the manager can still enter a record.
func TestM15DB_ARejectedClaimLeavesTheForgottenDayListed(t *testing.T) {
	f := newForgottenCheckout(t)
	id := f.tapAndClaim(t)
	rows := f.count(t, `SELECT count(*) FROM transactions WHERE tenant_id = $1 AND employee_id = $2`)

	f.decide(t, id, "rejected", "Claim rejected")

	if got := f.count(t, `SELECT count(*) FROM transactions WHERE tenant_id = $1 AND employee_id = $2`); got != rows {
		t.Errorf("the rejection wrote %d transactions row(s), want 0", got-rows)
	}
	if n := f.count(t, `SELECT count(*) FROM punch_claim_decisions d JOIN punch_claims c ON c.id = d.claim_id AND c.tenant_id = d.tenant_id
	                    WHERE d.tenant_id = $1 AND c.employee_id = $2 AND d.outcome = 'rejected'`); n != 1 {
		t.Errorf("%d rejected decision(s), want 1", n)
	}
	if got := f.paid(t); got != 0 {
		t.Errorf("the rejected day is paid %v, want 0", got)
	}
	if open, pending := f.listed(t); !open || pending {
		t.Errorf("after the rejection: listed %v, marked pending %v; want listed and unmarked", open, pending)
	}
	if _, page := f.p.get(t, anomaliesHref+"?week="+f.day.String()); strings.Contains(page, "focus="+id.String()) {
		t.Error("the anomaly screen still marks the rejected claim as pending")
	}
}
