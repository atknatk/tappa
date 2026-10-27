package claim

// claim_db_test.go -- Submit, Decide, Inspect and Offer against real Postgres (CLAUDE.md §8: RLS and
// the triggers of migration 00068 cannot be faked). Every test builds its OWN tenant (zone UTC, the
// business default 05:00 cutoff), venues, manager and employee; the seed tenant is not touched. The
// chains are written as tappa_app in the tenant's context with explicit business days and write
// instants, on a Monday two weeks back, so no window meets `now` and no DST day is crossed.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/manual"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/test/fixtures"
)

type world struct {
	t      *testing.T
	ctx    context.Context
	cfg    *config.Config
	data   *db.DB
	owner  *pgx.Conn
	trail  *audit.Recorder
	rec    *manual.Recorder
	svc    *Service
	tenant uuid.UUID
	home   uuid.UUID // the person's home venue, shift 09:00–17:00
	other  uuid.UUID // another venue, shift 10:00–22:00
	admin  uuid.UUID
	who    uuid.UUID
	plaque string
	ctr    int
	monday time.Time // 00:00 UTC of a Monday two weeks back
	cutoff tap.DayCutoff
}

func newWorld(t *testing.T) *world {
	t.Helper()
	appDSN, ownerDSN := os.Getenv("DATABASE_URL"), os.Getenv("DATABASE_MIGRATE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("DATABASE_URL and DATABASE_MIGRATE_URL are both required (real Postgres)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	cfg := &config.Config{
		Env: config.EnvDev, BaseURL: "http://localhost:8080", DatabaseURL: appDSN,
		SessionHMACKey: []byte("SSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSS"), InviteHMACKey: []byte("IIIIIIIIIIIIIIIIIIIIIIIIIIIIIIII"),
		TagKEK: make([]byte, 32), RetentionYears: 2, GPSRadiusMeters: 150, Debounce: 60 * time.Second, Freshness: 180 * time.Second,
		BusinessDayFrom: fixtures.BusinessDaySwitchOn(),
	}
	data, err := db.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(data.Close)
	trail, err := audit.New(data)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := manual.NewRecorder(data, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(data, trail, rec, cfg.BusinessDayFrom, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	today := time.Now().UTC().Truncate(24 * time.Hour)
	monday := today.AddDate(0, 0, -14)
	for monday.Weekday() != time.Monday {
		monday = monday.AddDate(0, 0, -1)
	}
	w := &world{t: t, ctx: ctx, cfg: cfg, data: data, owner: owner, trail: trail, rec: rec, svc: svc,
		tenant: uuid.New(), admin: uuid.New(), who: uuid.New(), monday: monday, ctr: 100,
		cutoff: tap.DayCutoff{Cutoff: 5 * time.Hour, Timezone: "UTC"}}
	w.exec(`INSERT INTO tenants (id, name, vat_number, business_type, structure, timezone) VALUES ($1, 'm15-16 claims', $2, 'restaurant', 'multi', 'UTC')`,
		w.tenant, "VAT-M1516-"+w.tenant.String())
	w.home = w.venue("m15-16 home", "09:00", "17:00")
	w.other = w.venue("m15-16 other", "10:00", "22:00")
	b, key := make([]byte, 7), make([]byte, 44)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	w.plaque = strings.ToUpper(hex.EncodeToString(b))
	w.exec(`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status) VALUES ($1, $2, $3, $4, 10000, 'active')`,
		w.plaque, w.tenant, w.home, key)
	w.exec(`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, 'Owner', $3, $4, 'owner')`,
		w.admin, w.tenant, "o-"+w.admin.String()[:12]+"@example.test", "$2a$12$"+strings.Repeat("h", 53))
	w.exec(`INSERT INTO employees (id, tenant_id, external_ref, full_name, status, invited_at, location_id) VALUES ($1, $2, $3, 'Claim Staff', 'active', now(), $4)`,
		w.who, w.tenant, "ext-"+w.who.String()[:8], w.home)
	return w
}

func (w *world) exec(sql string, args ...any) {
	w.t.Helper()
	if _, err := w.owner.Exec(w.ctx, sql, args...); err != nil {
		w.t.Fatalf("%s: %v", sql, err)
	}
}

func (w *world) venue(name, start, end string) uuid.UUID {
	w.t.Helper()
	var id uuid.UUID
	if err := w.owner.QueryRow(w.ctx, `INSERT INTO locations (tenant_id, name, static_ips, shift_start, shift_end)
	                                   VALUES ($1, $2, '{203.0.113.0/24}', $3, $4) RETURNING id`, w.tenant, name, start, end).Scan(&id); err != nil {
		w.t.Fatal(err)
	}
	return id
}

// at is day d of the scenario week (0 = Monday) at hh:mm UTC.
func (w *world) at(d, hh, mm int) time.Time {
	return w.monday.AddDate(0, 0, d).Add(time.Duration(hh)*time.Hour + time.Duration(mm)*time.Minute)
}

// tapAt writes a directed nfc tap of the person's at `at` at venue loc, with its frozen business day
// (the 05:00 cutoff) and written at its own instant -- as the engine would have.
func (w *world) tapAt(dir string, at time.Time, loc uuid.UUID) uuid.UUID {
	w.t.Helper()
	s, ok := tap.BusinessDayStart(at, w.cutoff)
	if !ok {
		w.t.Fatal("no business day")
	}
	return w.row(dir, at, loc, &s, "ok")
}

func (w *world) row(dir string, at time.Time, loc uuid.UUID, day *time.Time, verdict string) uuid.UUID {
	w.t.Helper()
	w.ctr++
	var id uuid.UUID
	var typ any
	if dir != "" {
		typ = dir
	}
	if err := w.data.WithTenant(w.ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, created_at, verdict, channel, type, sun_valid, ctr, business_day_start)
		                         VALUES ($1, $2, $3, $4, $5, $5, $6, 'nfc', $7, true, $8, $9) RETURNING id`,
			w.tenant, w.who, loc, w.plaque, at, verdict, typ, w.ctr, day).Scan(&id)
	}); err != nil {
		w.t.Fatalf("write a %q: %v", dir, err)
	}
	return id
}

func (w *world) count(sql string, args ...any) int {
	w.t.Helper()
	var n int
	if err := w.owner.QueryRow(w.ctx, sql, args...).Scan(&n); err != nil {
		w.t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func (w *world) claims() int {
	return w.count(`SELECT count(*) FROM punch_claims WHERE tenant_id = $1`, w.tenant)
}
func (w *world) decisions() int {
	return w.count(`SELECT count(*) FROM punch_claim_decisions WHERE tenant_id = $1`, w.tenant)
}
func (w *world) manualRows() int {
	return w.count(`SELECT count(*) FROM transactions WHERE tenant_id = $1 AND channel = 'manual'`, w.tenant)
}
func (w *world) trailRows(action string) int {
	return w.count(`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2`, w.tenant, action)
}

func (w *world) submit(anchor uuid.UUID, kind Kind, at time.Time) (Submitted, error) {
	return w.svc.Submit(w.ctx, Claim{TenantID: w.tenant, EmployeeID: w.who, AnchorID: anchor, Kind: kind, ClaimedAt: at})
}

func (w *world) mustSubmit(anchor uuid.UUID, kind Kind, at time.Time) Submitted {
	w.t.Helper()
	s, err := w.submit(anchor, kind, at)
	if err != nil {
		w.t.Fatalf("submit %s at %s: %v", kind, at, err)
	}
	return s
}

func (w *world) decide(claim uuid.UUID, o Outcome, edit *time.Time, confirmed bool) (Decided, error) {
	d := Decision{TenantID: w.tenant, ClaimID: claim, DecidedBy: w.admin, Outcome: o, At: edit}
	if confirmed {
		// The step's subject, as the panel would have signed it: the assessment at the moment.
		in, err := w.svc.Inspect(w.ctx, w.tenant, claim, edit)
		if err == nil {
			d.Confirmation = ConfirmationSubject(claim, in.Assessment)
		}
	}
	return w.svc.Decide(w.ctx, d)
}

// TestSubmitDB_WritesTheClaimAndItsTrailWithTheServersPrefill: Mon 09:12 in, shift 09:00–17:00 -- the
// checkout claim at 17:00 is written with the prefill the SERVER computed (17:00: "default not
// changed"), and its trail row names no actor (the employee acts on their own behalf).
func TestSubmitDB_WritesTheClaimAndItsTrailWithTheServersPrefill(t *testing.T) {
	w := newWorld(t)
	in := w.tapAt("in", w.at(0, 9, 12), w.home)
	got := w.mustSubmit(in, KindOut, w.at(0, 17, 0))
	if got.Prefilled == nil || !got.Prefilled.Equal(w.at(0, 17, 0)) || !got.DefaultUnchanged {
		t.Errorf("prefilled %v, unchanged %v; want 17:00 and true", got.Prefilled, got.DefaultUnchanged)
	}
	var kind string
	var prefilled *time.Time
	if err := w.owner.QueryRow(w.ctx, `SELECT kind, prefilled_at FROM punch_claims WHERE id = $1`, got.ID).Scan(&kind, &prefilled); err != nil {
		t.Fatal(err)
	}
	if kind != "out" || prefilled == nil || !prefilled.Equal(w.at(0, 17, 0)) {
		t.Errorf("the row: kind %q, prefilled %v", kind, prefilled)
	}
	var actor *uuid.UUID
	var detail string
	if err := w.owner.QueryRow(w.ctx, `SELECT actor_id, detail::text FROM audit_log WHERE id = $1 AND action = $2`, got.AuditID, ActionSubmitted).
		Scan(&actor, &detail); err != nil {
		t.Fatal(err)
	}
	if actor != nil {
		t.Errorf("claim.submitted names actor %s; an employee acting on their own behalf is nil", actor)
	}
	for _, want := range []string{got.ID.String(), in.String(), `"default_unchanged": true`} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail %s lacks %s", detail, want)
		}
	}
	if strings.Contains(detail, "@") || strings.Contains(detail, "Claim Staff") {
		t.Errorf("detail carries a name or an address: %s", detail)
	}
	// a moment other than the prefill is no "default not changed"
	w2 := newWorld(t)
	in2 := w2.tapAt("in", w2.at(0, 9, 12), w2.home)
	if s := w2.mustSubmit(in2, KindOut, w2.at(0, 17, 30)); s.DefaultUnchanged || s.Prefilled == nil {
		t.Errorf("17:30: %+v", s)
	}
}

// TestSubmitDB_TheSplitShiftHasNoPrefill: KF 10–22 (the other venue), 11:00 in, 18:00 "Not right?
// Clock in" -- the window is (11:00, 18:00), 22:00 is outside it, so the claim carries no prefill.
func TestSubmitDB_TheSplitShiftHasNoPrefill(t *testing.T) {
	w := newWorld(t)
	a := w.tapAt("in", w.at(0, 11, 0), w.other)
	w.tapAt("in", w.at(0, 18, 0), w.other)
	s := w.mustSubmit(a, KindOut, w.at(0, 15, 0))
	if s.Prefilled != nil || s.DefaultUnchanged {
		t.Errorf("split shift: %+v; want no prefill", s)
	}
	if _, err := w.submit(a, KindOut, w.at(0, 18, 0)); !errors.Is(err, ErrAlreadySent) {
		// the anchor is claimed already; a second send is "Sent" whatever its moment
		t.Errorf("second send: %v, want ErrAlreadySent", err)
	}
}

// TestSubmitDB_ASecondSendIsAlreadySent (§C.5): the same anchor twice -- the second is ErrAlreadySent
// (the "Sent" screen), and one row stays.
func TestSubmitDB_ASecondSendIsAlreadySent(t *testing.T) {
	w := newWorld(t)
	in := w.tapAt("in", w.at(0, 9, 0), w.home)
	w.mustSubmit(in, KindOut, w.at(0, 17, 0))
	// Whatever the second send's moment -- the same, one outside the window, or none the handler
	// could place -- it is "already sent", asked before the window (ADR 0032 §C.4 note (5)).
	for name, at := range map[string]time.Time{"the same moment": w.at(0, 17, 0), "outside the window": w.at(0, 8, 0), "no moment": {}} {
		if _, err := w.submit(in, KindOut, at); !errors.Is(err, ErrAlreadySent) {
			t.Fatalf("second send, %s: %v, want ErrAlreadySent", name, err)
		}
	}
	if n := w.claims(); n != 1 {
		t.Errorf("%d claims, want 1", n)
	}
	if n := w.trailRows(ActionSubmitted); n != 1 {
		t.Errorf("%d claim.submitted rows, want 1", n)
	}
}

// TestSubmitDB_EveryRefusalWritesNothing: each refusal of Submit, and zero rows behind it.
func TestSubmitDB_EveryRefusalWritesNothing(t *testing.T) {
	w := newWorld(t)
	open := w.tapAt("in", w.at(0, 9, 0), w.home)
	pairIn := w.tapAt("in", w.at(1, 9, 0), w.home)
	w.tapAt("out", w.at(1, 17, 0), w.home)
	refused := w.row("", w.at(2, 9, 0), w.home, nil, "reject")
	preM15 := w.row("in", w.at(3, 9, 0), w.home, nil, "ok")
	other := newWorld(t)
	foreign := other.tapAt("in", other.at(0, 9, 0), other.home)
	tests := []struct {
		name   string
		anchor uuid.UUID
		kind   Kind
		at     time.Time
		want   error
	}{
		{"another tenant's record", foreign, KindOut, w.at(0, 17, 0), ErrUnknownAnchor},
		{"no such record", uuid.New(), KindOut, w.at(0, 17, 0), ErrUnknownAnchor},
		{"outside the window (past 18 h)", open, KindOut, w.at(1, 3, 1), ErrOutsideWindow},
		{"before the anchor", open, KindOut, w.at(0, 8, 0), ErrOutsideWindow},
		{"no moment (the handler could not place the clock)", open, KindOut, time.Time{}, ErrOutsideWindow},
		{"the wrong half", open, KindIn, w.at(0, 8, 0), ErrResolved},
		{"a closed check-in", pairIn, KindOut, w.at(1, 16, 0), ErrResolved},
		{"a refused tap (no direction)", refused, KindOut, w.at(2, 12, 0), ErrResolved},
		{"a record before the business day (Ü-Y2)", preM15, KindOut, w.at(3, 17, 0), ErrResolved},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := w.submit(tc.anchor, tc.kind, tc.at); !errors.Is(err, tc.want) {
				t.Errorf("Submit = %v, want %v", err, tc.want)
			}
		})
	}
	if n, a := w.claims(), w.trailRows(ActionSubmitted); n != 0 || a != 0 {
		t.Fatalf("the refusals left %d claims and %d trail rows", n, a)
	}
	// another person's record of the same tenant: unknown too
	stranger := uuid.New()
	w.exec(`INSERT INTO employees (id, tenant_id, external_ref, full_name, status, invited_at, location_id) VALUES ($1, $2, $3, 'Stranger', 'active', now(), $4)`,
		stranger, w.tenant, "ext-"+stranger.String()[:8], w.home)
	if _, err := w.svc.Submit(w.ctx, Claim{TenantID: w.tenant, EmployeeID: stranger, AnchorID: open, Kind: KindOut, ClaimedAt: w.at(0, 17, 0)}); !errors.Is(err, ErrUnknownAnchor) {
		t.Errorf("another person's record: %v, want ErrUnknownAnchor", err)
	}
	// a deactivated person sends nothing (§C.5)
	w.exec(`UPDATE employees SET status = 'deactivated', deactivated_at = now() WHERE id = $1`, w.who)
	if _, err := w.submit(open, KindOut, w.at(0, 17, 0)); !errors.Is(err, ErrNotActive) {
		t.Errorf("deactivated: %v, want ErrNotActive", err)
	}
	if n := w.claims(); n != 0 {
		t.Errorf("%d claims, want 0", n)
	}
}

// TestDecideDB_AnApprovalIsOneRecordOneDecisionAndTwoTrailRows: the approval writes the manual record
// at the ANCHOR's venue (not the home venue: "mekân = çapanın mekânı"), entered by the decider, at
// the claimed moment, with the decision naming it, record.entered and claim.decided -- one
// transaction. A second decision is "already decided".
func TestDecideDB_AnApprovalIsOneRecordOneDecisionAndTwoTrailRows(t *testing.T) {
	w := newWorld(t)
	in := w.tapAt("in", w.at(0, 11, 0), w.other) // tapped at the other venue
	c := w.mustSubmit(in, KindOut, w.at(0, 20, 0))
	d, err := w.decide(c.ID, Approved, nil, false)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if d.RecordID == nil || d.ApprovedAt == nil || !d.ApprovedAt.Equal(w.at(0, 20, 0)) {
		t.Fatalf("decided %+v", d)
	}
	var loc, enteredBy *uuid.UUID
	var typ, channel, note *string
	if err := w.owner.QueryRow(w.ctx, `SELECT location_id, entered_by, type, channel, note FROM transactions WHERE id = $1`, *d.RecordID).
		Scan(&loc, &enteredBy, &typ, &channel, &note); err != nil {
		t.Fatal(err)
	}
	if loc == nil || *loc != w.other {
		t.Errorf("the record is filed at %v, want the anchor's venue %s (the home venue is %s)", loc, w.other, w.home)
	}
	if enteredBy == nil || *enteredBy != w.admin || *typ != "out" || *channel != "manual" || note == nil || *note != ClaimApprovedNote {
		t.Errorf("the record: entered_by %v, type %v, channel %v, note %v", enteredBy, *typ, *channel, note)
	}
	var outcome string
	var tx *uuid.UUID
	if err := w.owner.QueryRow(w.ctx, `SELECT outcome, transaction_id FROM punch_claim_decisions WHERE id = $1`, d.ID).Scan(&outcome, &tx); err != nil {
		t.Fatal(err)
	}
	if outcome != "approved" || tx == nil || *tx != *d.RecordID {
		t.Errorf("the decision: %s, %v", outcome, tx)
	}
	if a, r := w.trailRows(ActionDecided), w.trailRows(manual.Action); a != 1 || r != 1 {
		t.Errorf("trail rows: claim.decided %d, record.entered %d; want 1 and 1", a, r)
	}
	var actor *uuid.UUID
	if err := w.owner.QueryRow(w.ctx, `SELECT actor_id FROM audit_log WHERE id = $1`, d.AuditID).Scan(&actor); err != nil || actor == nil || *actor != w.admin {
		t.Errorf("claim.decided's actor %v (%v), want the decider", actor, err)
	}
	if _, err := w.decide(c.ID, Rejected, nil, false); !errors.Is(err, ErrAlreadyDecided) {
		t.Errorf("a second decision: %v, want ErrAlreadyDecided", err)
	}
	if n := w.decisions(); n != 1 {
		t.Errorf("%d decisions", n)
	}
	in2, err := w.svc.Inspect(w.ctx, w.tenant, c.ID, nil)
	if err != nil || in2.State != StateDecided || in2.Decided == nil || *in2.Decided != Approved {
		t.Errorf("Inspect after the decision: %+v, %v", in2, err)
	}
}

// TestDecideDB_ARejectionWritesTheDecisionAndItsTrailOnly (§C.5): no record; the anomaly stays.
func TestDecideDB_ARejectionWritesTheDecisionAndItsTrailOnly(t *testing.T) {
	w := newWorld(t)
	in := w.tapAt("in", w.at(0, 9, 0), w.home)
	c := w.mustSubmit(in, KindOut, w.at(0, 17, 0))
	if _, err := w.svc.Decide(w.ctx, Decision{TenantID: w.tenant, ClaimID: c.ID, DecidedBy: w.admin, Outcome: Rejected, Note: "not true"}); err != nil {
		t.Fatal(err)
	}
	if m, d, a := w.manualRows(), w.decisions(), w.trailRows(ActionDecided); m != 0 || d != 1 || a != 1 {
		t.Errorf("records %d, decisions %d, claim.decided %d; want 0, 1, 1", m, d, a)
	}
	var detail string
	if err := w.owner.QueryRow(w.ctx, `SELECT detail::text FROM audit_log WHERE tenant_id = $1 AND action = $2`, w.tenant, ActionDecided).Scan(&detail); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(detail, "not true") || !strings.Contains(detail, `"has_note": true`) {
		t.Errorf("the note leaked into the trail or was not flagged: %s", detail)
	}
}

// TestDecideDB_OutsideTheBandTheFirstApproveWritesNothing is G-Y1's two cases end to end: inside the
// window, outside the band -- the first Approve writes nothing and returns the assessment; the
// confirmed one writes.
func TestDecideDB_OutsideTheBandTheFirstApproveWritesNothing(t *testing.T) {
	w := newWorld(t)
	// (1) a 09:00 chosen out -- an orphan (c) -- and the check-in claim 05:00
	x := w.tapAt("out", w.at(0, 9, 0), w.home)
	c1 := w.mustSubmit(x, KindIn, w.at(0, 5, 0))
	// (2) Mon 09:12 in, a day later; the checkout claim Tue 03:00 (17 h 48 m)
	in := w.tapAt("in", w.at(2, 9, 12), w.home)
	c2 := w.mustSubmit(in, KindOut, w.at(3, 3, 0))
	for _, tc := range []struct {
		name     string
		claim    uuid.UUID
		duration time.Duration
		dev      Deviation
	}{
		{"in 05:00 → out 09:00", c1.ID, 4 * time.Hour, Deviation{Side: BeforeStart, By: 4 * time.Hour}},
		{"in 09:12 → out 03:00", c2.ID, 17*time.Hour + 48*time.Minute, Deviation{Side: AfterEnd, By: 10 * time.Hour}},
	} {
		d, err := w.decide(tc.claim, Approved, nil, false)
		if !errors.Is(err, ErrNeedsConfirmation) {
			t.Fatalf("%s: first approve %v, want ErrNeedsConfirmation", tc.name, err)
		}
		if d.Assessment == nil || d.Assessment.Interval.Duration() != tc.duration || d.Assessment.Deviation != tc.dev {
			t.Errorf("%s: the step repeats %+v", tc.name, d.Assessment)
		}
		if m, n := w.manualRows(), w.decisions(); m != 0 || n != 0 {
			t.Fatalf("%s: the first approve wrote %d records and %d decisions", tc.name, m, n)
		}
		ins, err := w.svc.Inspect(w.ctx, w.tenant, tc.claim, nil)
		if err != nil || ins.State != StatePending || !ins.Assessment.RequiresConfirmation || ins.Assessment.Interval.Duration() != tc.duration {
			t.Errorf("%s: Inspect %+v, %v", tc.name, ins, err)
		}
	}
	for _, id := range []uuid.UUID{c1.ID, c2.ID} {
		if _, err := w.decide(id, Approved, nil, true); err != nil {
			t.Fatalf("confirmed approve: %v", err)
		}
	}
	if m := w.manualRows(); m != 2 {
		t.Errorf("%d records after the confirmations, want 2", m)
	}
}

// TestDecideDB_AnEditOutsideTheBandAsksTheConfirmationStep (ürün N8): the manager's Edit is held to the
// same band; a claim inside it, edited outside it, asks the step; and an Edit outside the window is
// refused by the window.
func TestDecideDB_AnEditOutsideTheBandAsksTheConfirmationStep(t *testing.T) {
	w := newWorld(t)
	in := w.tapAt("in", w.at(0, 9, 0), w.home)
	c := w.mustSubmit(in, KindOut, w.at(0, 17, 0))
	edit := w.at(0, 22, 0)
	if _, err := w.decide(c.ID, Approved, &edit, false); !errors.Is(err, ErrNeedsConfirmation) {
		t.Fatalf("Edit to 22:00: %v, want ErrNeedsConfirmation", err)
	}
	if m := w.manualRows(); m != 0 {
		t.Fatalf("%d records", m)
	}
	past := w.at(1, 3, 0).Add(time.Second) // in.At + 18 h + 1 s
	if _, err := w.decide(c.ID, Approved, &past, true); !errors.Is(err, ErrOutsideWindow) {
		t.Errorf("Edit past 18 h: %v, want ErrOutsideWindow", err)
	}
	if m := w.manualRows(); m != 0 {
		t.Fatalf("the Edit past 18 h wrote %d records", m)
	}
	d, err := w.decide(c.ID, Approved, &edit, true)
	if err != nil || d.ApprovedAt == nil || !d.ApprovedAt.Equal(edit) {
		t.Fatalf("confirmed Edit: %+v, %v", d, err)
	}
	var approvedAt, occurred time.Time
	if err := w.owner.QueryRow(w.ctx, `SELECT d.approved_at, t.occurred_at FROM punch_claim_decisions d JOIN transactions t ON t.id = d.transaction_id WHERE d.id = $1`, d.ID).
		Scan(&approvedAt, &occurred); err != nil || !approvedAt.Equal(edit) || !occurred.Equal(edit) {
		t.Errorf("approved_at %v, record %v (%v); want the Edit", approvedAt, occurred, err)
	}
}

// TestDecideDB_AResolvedAnchorTakesOnlyReject (§C.5 "artık gereksiz"): the manager closed the check-in
// by hand meanwhile -- the approval is refused, Reject still decides.
func TestDecideDB_AResolvedAnchorTakesOnlyReject(t *testing.T) {
	w := newWorld(t)
	in := w.tapAt("in", w.at(0, 9, 0), w.home)
	c := w.mustSubmit(in, KindOut, w.at(0, 17, 0))
	if _, err := w.rec.Record(w.ctx, manual.Entry{TenantID: w.tenant, EmployeeID: w.who, EnteredBy: w.admin, Direction: manual.Out, At: w.at(0, 16, 0)}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.decide(c.ID, Approved, nil, true); !errors.Is(err, ErrResolved) {
		t.Fatalf("approve a resolved claim: %v, want ErrResolved", err)
	}
	if ins, _ := w.svc.Inspect(w.ctx, w.tenant, c.ID, nil); ins.State != StateResolved {
		t.Errorf("Inspect state %s, want resolved", ins.State)
	}
	if _, err := w.decide(c.ID, Rejected, nil, false); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if m := w.manualRows(); m != 1 {
		t.Errorf("%d manual records, want the manager's one", m)
	}
}

// TestDecideDB_AnExpiredClaimTakesOnlyReject (§C.5): past manual.MaxBackdate an approval is refused --
// no record can be written for that moment -- and Reject decides.
func TestDecideDB_AnExpiredClaimTakesOnlyReject(t *testing.T) {
	w := newWorld(t)
	in := w.tapAt("in", w.at(0, 9, 0), w.home)
	c := w.mustSubmit(in, KindOut, w.at(0, 17, 0))
	w.svc.now = func() time.Time { return w.at(0, 17, 0).Add(manual.MaxBackdate + time.Hour) }
	if _, err := w.decide(c.ID, Approved, nil, true); !errors.Is(err, ErrExpired) {
		t.Fatalf("approve an expired claim: %v, want ErrExpired", err)
	}
	if ins, _ := w.svc.Inspect(w.ctx, w.tenant, c.ID, nil); ins.State != StateExpired {
		t.Errorf("Inspect state %s, want expired", ins.State)
	}
	if _, err := w.decide(c.ID, Rejected, nil, false); err != nil {
		t.Fatal(err)
	}
	if m := w.manualRows(); m != 0 {
		t.Errorf("%d records", m)
	}
}

// TestDecideDB_ADeactivatedPersonsPendingClaimStaysDecidable (§C.5).
func TestDecideDB_ADeactivatedPersonsPendingClaimStaysDecidable(t *testing.T) {
	w := newWorld(t)
	in := w.tapAt("in", w.at(0, 9, 0), w.home)
	c := w.mustSubmit(in, KindOut, w.at(0, 17, 0))
	w.exec(`UPDATE employees SET status = 'deactivated', deactivated_at = now() WHERE id = $1`, w.who)
	if _, err := w.decide(c.ID, Approved, nil, false); err != nil {
		t.Fatalf("approve after the deactivation: %v", err)
	}
}

// TestDecideDB_TwoConcurrentApprovalsWriteOneDecisionAndOneRecord (the card's criterion; -race): eight
// approvals of one claim at once -- exactly one writes, the rest are "already decided".
func TestDecideDB_TwoConcurrentApprovalsWriteOneDecisionAndOneRecord(t *testing.T) {
	w := newWorld(t)
	in := w.tapAt("in", w.at(0, 9, 0), w.home)
	c := w.mustSubmit(in, KindOut, w.at(0, 17, 0))
	const n = 8
	var ok, already atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := w.decide(c.ID, Approved, nil, false)
			switch {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, ErrAlreadyDecided):
				already.Add(1)
			default:
				errs <- err
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("an approval failed otherwise: %v", err)
	}
	if ok.Load() != 1 || already.Load() != n-1 {
		t.Errorf("%d approvals wrote, %d were already decided; want 1 and %d", ok.Load(), already.Load(), n-1)
	}
	if m, d := w.manualRows(), w.decisions(); m != 1 || d != 1 {
		t.Errorf("%d records and %d decisions, want 1 and 1", m, d)
	}
}

// failingTrail fails claim.decided -- the last write of an approval -- and passes everything else.
type failingTrail struct{ inner Trail }

func (f failingTrail) RecordTx(ctx context.Context, tx pgx.Tx, e audit.Event) (uuid.UUID, error) {
	if e.Action == ActionDecided {
		return uuid.Nil, errors.New("injected: the trail is down")
	}
	return f.inner.RecordTx(ctx, tx, e)
}

// TestDecideDB_AnApprovalHalfDoneWritesNothing (the card's criterion): the approval's last write fails
// -- after its record, its record.entered row and its decision -- and none of them stays.
func TestDecideDB_AnApprovalHalfDoneWritesNothing(t *testing.T) {
	w := newWorld(t)
	in := w.tapAt("in", w.at(0, 9, 0), w.home)
	c := w.mustSubmit(in, KindOut, w.at(0, 17, 0))
	broken, err := New(w.data, failingTrail{w.trail}, w.rec, w.cfg.BusinessDayFrom, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := broken.Decide(w.ctx, Decision{TenantID: w.tenant, ClaimID: c.ID, DecidedBy: w.admin, Outcome: Approved}); err == nil {
		t.Fatal("the approval succeeded with its trail down")
	}
	if m, d, a := w.manualRows(), w.decisions(), w.trailRows(manual.Action); m != 0 || d != 0 || a != 0 {
		t.Errorf("the half-done approval left %d records, %d decisions, %d record.entered rows", m, d, a)
	}
	if _, err := w.decide(c.ID, Approved, nil, false); err != nil {
		t.Errorf("the claim is not decidable after the failure: %v", err)
	}
}

// TestDecideDB_TheThirdEyesCasePaysMondayEightHours is the third eye's case through the REPORT
// (internal/domain/ledger's Hours, the payroll arithmetic): Mon 15:00 in, Tue 07:00 chosen out; both
// claims accepted; after both approvals Monday is 8 h (15:00–23:00) and Tuesday 1 h 30 m
// (05:30–07:00). ADR 0011's measurement, here: no approval lengthens an interval that existed -- the
// Wednesday pair (09:00–17:00) and Thursday's 09:00–12:00 stay what they were, and Thursday's
// orphan 17:00 out becomes a new 13:00–17:00 pair beside it; every total grows by exactly the
// claimed intervals.
func TestDecideDB_TheThirdEyesCasePaysMondayEightHours(t *testing.T) {
	w := newWorld(t)
	reader, err := ledger.NewReader(w.data, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	monIn := w.tapAt("in", w.at(0, 15, 0), w.home)
	tueOut := w.tapAt("out", w.at(1, 7, 0), w.home)
	w.tapAt("in", w.at(2, 9, 0), w.home)
	w.tapAt("out", w.at(2, 17, 0), w.home)
	w.tapAt("in", w.at(3, 9, 0), w.home)
	w.tapAt("out", w.at(3, 12, 0), w.home)
	thuOrphan := w.tapAt("out", w.at(3, 17, 0), w.home)
	week := func() ledger.PersonHours {
		t.Helper()
		r, err := reader.Hours(w.ctx, w.tenant, ledger.ReportFilter{Day: ledger.Date{Year: w.monday.Year(), Month: w.monday.Month(), Day: w.monday.Day()}})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range r.People {
			if p.EmployeeID == w.who {
				return p
			}
		}
		t.Fatal("the person is not in the report")
		return ledger.PersonHours{}
	}
	before := week()
	if before.Daily[0] != 0 || before.Daily[1] != 0 || before.Daily[2] != 8*time.Hour || before.Daily[3] != 3*time.Hour {
		t.Fatalf("PREMISE: before the approvals the days are %v", before.Daily[:4])
	}
	a := w.mustSubmit(monIn, KindOut, w.at(0, 23, 0))
	b := w.mustSubmit(tueOut, KindIn, w.at(1, 5, 30))
	ins, err := w.svc.Inspect(w.ctx, w.tenant, b.ID, nil)
	// After is nil: Wednesday's 09:00 is past the 18 hours the claim is judged in (Inspection's doc)
	if err != nil || ins.State != StatePending || ins.Anchor.ID != tueOut || ins.Before == nil || ins.Before.ID != monIn ||
		ins.After != nil || ins.Kind != KindIn || ins.Assessment.Interval.Duration() != 90*time.Minute {
		t.Errorf("Inspect(Tuesday's claim) = %+v, %v; want pending, Monday's in before the anchor, nothing within 18 h after", ins, err)
	}
	if ins.Window == nil || !ins.Window.Contains(w.at(1, 5, 0)) || ins.Window.Contains(w.at(1, 4, 59)) {
		t.Errorf("Inspect's window %+v; want [Tue 05:00, 07:00)", ins.Window)
	}
	c := w.mustSubmit(thuOrphan, KindIn, w.at(3, 13, 0))
	for _, id := range []uuid.UUID{b.ID, a.ID, c.ID} { // Tuesday's first: the order must not matter
		if _, err := w.decide(id, Approved, nil, true); err != nil {
			t.Fatalf("approve: %v", err)
		}
	}
	after := week()
	want := []time.Duration{8 * time.Hour, 90 * time.Minute, 8 * time.Hour, 7 * time.Hour}
	for i, d := range want {
		if after.Daily[i] != d {
			t.Errorf("day %d: %v, want %v", i, after.Daily[i], d)
		}
	}
	if grown := after.Worked - before.Worked; grown != 8*time.Hour+90*time.Minute+4*time.Hour {
		t.Errorf("the week grew by %v, want exactly the three claimed intervals (13h30m)", grown)
	}
}

// TestDecideDB_TheLockHoldsATapUntilTheApprovalCommits: an approval whose checks have passed holds the
// person's tap lock until it commits, so the person's tap meanwhile waits and is then decided on the
// chain WITH the approved checkout -- an `in`, the check-in already closed. Without the lock the tap
// would read the check-in open and record an `out` closing it, and the approval would then write a
// checkout before that out: the pair its checks approved would no longer be the chain's. A real tap
// (checkin.Service.Record), the person's cutoff kept away from now (fixtures.GiveFarDayCutoff).
func TestDecideDB_TheLockHoldsATapUntilTheApprovalCommits(t *testing.T) {
	w := newWorld(t)
	if err := w.data.WithTenant(w.ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := fixtures.GiveFarDayCutoff(ctx, tx, w.tenant, w.who, time.Now())
		return e
	}); err != nil {
		t.Fatal(err)
	}
	var seconds int64
	if err := w.owner.QueryRow(w.ctx, `SELECT extract(epoch FROM day_cutoff)::bigint FROM employees WHERE id = $1`, w.who).Scan(&seconds); err != nil {
		t.Fatal(err)
	}
	w.cutoff = tap.DayCutoff{Cutoff: time.Duration(seconds) * time.Second, Timezone: "UTC"}
	start := time.Now().Add(-3 * time.Hour).Truncate(time.Second)
	in := w.tapAt("in", start, w.home)
	c := w.mustSubmit(in, KindOut, start.Add(2*time.Hour))
	svc, err := checkin.New(w.data, w.trail, w.cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	var res checkin.Result
	var tapErr error
	done := make(chan struct{})
	w.svc.afterCheck = func() {
		go func() {
			defer close(done)
			res, tapErr = svc.Record(w.ctx, checkin.Request{SessionTenantID: w.tenant, EmployeeID: w.who, TagUID: w.plaque, Ctr: 20000,
				Channel: tap.ChannelNFC, CMACVerified: true, TagTenantID: w.tenant, SourceIP: netip.MustParseAddr("203.0.113.7")})
		}()
		select {
		case <-done:
		case <-time.After(1500 * time.Millisecond):
		}
	}
	if _, err := w.decide(c.ID, Approved, nil, true); err != nil {
		t.Fatalf("approve: %v", err)
	}
	<-done
	if tapErr != nil {
		t.Fatalf("the tap: %v", tapErr)
	}
	if res.Decision.Type == nil || *res.Decision.Type != tap.TypeIn {
		t.Errorf("the tap is %v, want in: it was decided while the approval held its checks, not after its commit", res.Decision.Type)
	}
}

// TestOfferDB_TheCardFollowsTheChain is Offer end to end: the card on T1 (the tap that left the
// check-in open) with the server's prefill; none once the claim is sent; none on a refused tap; none
// for a check-in written before M15 (Ü-Y2).
func TestOfferDB_TheCardFollowsTheChain(t *testing.T) {
	w := newWorld(t)
	a := w.tapAt("in", w.at(0, 9, 12), w.home)
	t1 := w.tapAt("in", w.at(1, 9, 0), w.home)
	card, err := w.svc.Offer(w.ctx, w.tenant, w.who, t1)
	if err != nil || card == nil {
		t.Fatalf("Offer(T1) = %v, %v", card, err)
	}
	if card.Kind != KindOut || card.AnchorID != a || card.Prefilled == nil || !card.Prefilled.Equal(w.at(0, 17, 0)) {
		t.Errorf("card %+v; want Monday's checkout claim prefilled 17:00", card)
	}
	refused := w.row("", w.at(1, 9, 30), w.home, nil, "reject")
	if card, err := w.svc.Offer(w.ctx, w.tenant, w.who, refused); err != nil || card != nil {
		t.Errorf("Offer(a refused tap) = %+v, %v; want none", card, err)
	}
	w.mustSubmit(a, KindOut, w.at(0, 17, 0))
	if card, err := w.svc.Offer(w.ctx, w.tenant, w.who, t1); err != nil || card != nil {
		t.Errorf("Offer(T1) after the claim = %+v, %v; want none", card, err)
	}
	pre := w.row("in", w.at(3, 9, 0), w.home, nil, "ok")
	t2 := w.tapAt("in", w.at(4, 9, 0), w.home)
	if card, err := w.svc.Offer(w.ctx, w.tenant, w.who, t2); err != nil || card != nil {
		t.Errorf("Offer after a pre-M15 check-in %s = %+v, %v; want none", pre, card, err)
	}
	other := newWorld(t)
	if _, err := w.svc.Offer(w.ctx, other.tenant, other.who, t1); !errors.Is(err, ErrUnknownAnchor) {
		t.Errorf("Offer(another tenant's view of this tap) = %v, want ErrUnknownAnchor", err)
	}
}

// TestPunchClaimQueries_TheNewStatementsCarryTheirOwnTenantPredicate is §4.5's belt over the
// statements this package added (internal/db's TestPunchClaimQueries_CarryTheirOwnTenantPredicate's
// shape, which says the domain carries its own copy): as the migration role, which RLS does not bind,
// each given A's tenant id and B's ids finds or writes nothing -- only its own tenant predicate
// refuses it. Control: the same superuser with B's tenant id reaches B's rows. One transaction,
// rolled back.
func TestPunchClaimQueries_TheNewStatementsCarryTheirOwnTenantPredicate(t *testing.T) {
	a, b := newWorld(t), newWorld(t)
	in := b.tapAt("in", b.at(0, 9, 0), b.home)
	claim := b.mustSubmit(in, KindOut, b.at(0, 17, 0))
	ctx := context.Background()
	tx, err := a.owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, a.tenant.String()); err != nil {
		t.Fatal(err)
	}
	q := store.New(tx)
	probe := func(tenant uuid.UUID) []error {
		var errs []error
		if _, e := q.GetPunchClaimAnchor(ctx, store.GetPunchClaimAnchorParams{TenantID: tenant, ID: in, EmployeeID: b.who}); e != nil {
			errs = append(errs, fmt.Errorf("GetPunchClaimAnchor: %w", e))
		}
		if rows, e := q.ListPunchClaimChain(ctx, store.ListPunchClaimChainParams{TenantID: tenant, EmployeeID: b.who,
			FromAt: b.at(0, 0, 0), ToAt: b.at(1, 0, 0), RowLimit: 10}); e != nil || len(rows) == 0 {
			errs = append(errs, fmt.Errorf("ListPunchClaimChain: %d rows, %v", len(rows), e))
		}
		if rows, e := q.ListPunchClaimedAnchors(ctx, store.ListPunchClaimedAnchorsParams{TenantID: tenant, EmployeeID: b.who,
			AnchorIds: []uuid.UUID{in}}); e != nil || len(rows) == 0 {
			errs = append(errs, fmt.Errorf("ListPunchClaimedAnchors: %d rows, %v", len(rows), e))
		}
		if _, e := q.GetPunchClaimForDecision(ctx, store.GetPunchClaimForDecisionParams{TenantID: tenant, ID: claim.ID}); e != nil {
			errs = append(errs, fmt.Errorf("GetPunchClaimForDecision: %w", e))
		}
		if _, e := tx.Exec(ctx, "SAVEPOINT p"); e != nil {
			errs = append(errs, e)
		}
		if _, e := q.InsertPunchClaimDecision(ctx, store.InsertPunchClaimDecisionParams{Outcome: "rejected", DecidedBy: b.admin,
			ClaimID: claim.ID, TenantID: tenant}); e != nil {
			errs = append(errs, fmt.Errorf("InsertPunchClaimDecision: %w", e))
		}
		if _, e := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT p"); e != nil {
			errs = append(errs, e)
		}
		if _, e := q.InsertManualTransactionAtAnchor(ctx, store.InsertManualTransactionAtAnchorParams{TenantID: tenant, Type: "out",
			OccurredAt: b.at(0, 17, 0), Trust: tap.TrustBase, EnteredBy: b.admin, AnchorID: in, EmployeeID: b.who}); e != nil {
			errs = append(errs, fmt.Errorf("InsertManualTransactionAtAnchor: %w", e))
		}
		if _, e := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT p"); e != nil {
			errs = append(errs, e)
		}
		// M15-18: the panel's two reads (internal/domain/ledger calls them) -- the queue and the
		// pending mark.
		if rows, e := q.ListPendingPunchClaims(ctx, store.ListPendingPunchClaimsParams{TenantID: tenant, RowLimit: 10}); e != nil || len(rows) == 0 {
			errs = append(errs, fmt.Errorf("ListPendingPunchClaims: %d rows, %v", len(rows), e))
		}
		if rows, e := q.ListPendingPunchClaimAnchors(ctx, store.ListPendingPunchClaimAnchorsParams{TenantID: tenant,
			AnchorIds: []uuid.UUID{in}}); e != nil || len(rows) == 0 {
			errs = append(errs, fmt.Errorf("ListPendingPunchClaimAnchors: %d rows, %v", len(rows), e))
		}
		if n, e := q.CountPendingPunchClaims(ctx, store.CountPendingPunchClaimsParams{TenantID: tenant, Cap: 10}); e != nil || n == 0 {
			errs = append(errs, fmt.Errorf("CountPendingPunchClaims: 0 rows (counted %d), %v", n, e))
		}
		return errs
	}
	if got := probe(a.tenant); len(got) != 9 {
		t.Errorf("with A's tenant id and B's rows, %d of 9 statements found nothing (want all nine): %v", len(got), got)
	} else {
		for _, e := range got {
			if !strings.Contains(e.Error(), "no rows") && !strings.Contains(e.Error(), ": 0 rows") {
				t.Errorf("a refusal that is not zero rows: %v", e)
			}
		}
	}
	if got := probe(b.tenant); len(got) != 0 {
		t.Errorf("control: with B's own tenant id the statements failed: %v", got)
	}
}

// TestDecideDB_TheSelfDecisionCheckFiresOnlyOnAForgedIdentity is G-O1's honest limit, measured: a
// decider and a claimant are ids of two tables, so the comparison cannot hold for any real pair --
// the only way to make it fire is to forge an admin_users row with the employee's own id, which no
// product path does. With that forgery the decision is refused and writes nothing. This is a test
// that the line works, not a guarantee that one person cannot hold both identities (the package doc).
func TestDecideDB_TheSelfDecisionCheckFiresOnlyOnAForgedIdentity(t *testing.T) {
	w := newWorld(t)
	in := w.tapAt("in", w.at(0, 9, 0), w.home)
	c := w.mustSubmit(in, KindOut, w.at(0, 17, 0))
	w.exec(`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, 'Forged', $3, $4, 'manager')`,
		w.who, w.tenant, "f-"+w.who.String()[:12]+"@example.test", "$2a$12$"+strings.Repeat("h", 53))
	for _, o := range []Outcome{Approved, Rejected} {
		if _, err := w.svc.Decide(w.ctx, Decision{TenantID: w.tenant, ClaimID: c.ID, DecidedBy: w.who, Outcome: o}); !errors.Is(err, ErrSelfDecision) {
			t.Errorf("%s by the claimant's own id: %v, want ErrSelfDecision", o, err)
		}
	}
	if m, d := w.manualRows(), w.decisions(); m != 0 || d != 0 {
		t.Errorf("%d records, %d decisions", m, d)
	}
}

// TestSubmitDB_ATruncatedChainIsNotJudged: a person with more records around the anchor than the
// chain read allows gets an error, never a claim judged on part of the chain (§4.6).
func TestSubmitDB_ATruncatedChainIsNotJudged(t *testing.T) {
	w := newWorld(t)
	in := w.tapAt("in", w.at(0, 9, 0), w.home)
	if err := w.data.WithTenant(w.ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, verdict, channel, type, sun_valid, ctr, business_day_start)
		        SELECT $1, $2, $3, $4, $5::timestamptz + n * interval '1 second', 'ok', 'nfc', 'in', true, 50000 + n, $6
		          FROM generate_series(1, $7::int) AS n`, w.tenant, w.who, w.home, w.plaque, w.at(0, 9, 0), w.at(0, 5, 0), chainRowLimit)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	_, err := w.submit(in, KindOut, w.at(0, 17, 0))
	if err == nil || isTyped(err) || !errors.Is(err, errChainTooLong) {
		t.Errorf("Submit over a chain past the limit: %v, want errChainTooLong", err)
	}
	if n := w.claims(); n != 0 {
		t.Errorf("%d claims", n)
	}
}

// syncBuffer is a log sink safe for the goroutines a service logs from.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestClaimDB_TheLogAndTheTrailCarryOnlyOpaqueIds (§7, ADR 0032 §C.3): a submit, an approval and a
// rejection log ids, a kind and an outcome -- never the manager's note, the person's name or an
// e-mail address -- and neither trail row carries them.
func TestClaimDB_TheLogAndTheTrailCarryOnlyOpaqueIds(t *testing.T) {
	w := newWorld(t)
	var logs syncBuffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	rec, err := manual.NewRecorder(w.data, w.trail, log)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(w.data, w.trail, rec, w.cfg.BusinessDayFrom, log)
	if err != nil {
		t.Fatal(err)
	}
	const note = "she told me she left at five, sorry"
	a := w.tapAt("in", w.at(0, 9, 0), w.home)
	b := w.tapAt("in", w.at(1, 9, 0), w.home)
	ca, err := svc.Submit(w.ctx, Claim{TenantID: w.tenant, EmployeeID: w.who, AnchorID: a, Kind: KindOut, ClaimedAt: w.at(0, 17, 0)})
	if err != nil {
		t.Fatal(err)
	}
	cb, err := svc.Submit(w.ctx, Claim{TenantID: w.tenant, EmployeeID: w.who, AnchorID: b, Kind: KindOut, ClaimedAt: w.at(1, 17, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Decide(w.ctx, Decision{TenantID: w.tenant, ClaimID: ca.ID, DecidedBy: w.admin, Outcome: Approved, Note: note}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Decide(w.ctx, Decision{TenantID: w.tenant, ClaimID: cb.ID, DecidedBy: w.admin, Outcome: Rejected, Note: note}); err != nil {
		t.Fatal(err)
	}
	out := logs.String()
	if !strings.Contains(out, ca.ID.String()) || !strings.Contains(out, "punch claim decided") {
		t.Fatalf("PREMISE: the log did not record the claims: %s", out)
	}
	var trail string
	if err := w.owner.QueryRow(w.ctx, `SELECT string_agg(detail::text, ' ') FROM audit_log WHERE tenant_id = $1`, w.tenant).Scan(&trail); err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{note, "Claim Staff", "Owner", "@example.test"} {
		if strings.Contains(out, leak) {
			t.Errorf("the log carries %q", leak)
		}
		if strings.Contains(trail, leak) {
			t.Errorf("the trail carries %q", leak)
		}
	}
}

// TestSubmitDB_TheLockHoldsAManualRecordUntilTheClaimCommits (§C.3: "Gönderim ve karar advisory kilit
// altında"): a claim whose checks have passed holds the person's tap lock until it commits, so a
// manager's record for the person meanwhile -- here the checkout that closes the very anchor --
// waits and commits after the claim, never between the claim's checks and its row. What the test
// can observe is the order: the record has not committed while the claim's checks hold. (The end
// state alone cannot show the lock: a claim moves no hour, and the decision judges it again.)
func TestSubmitDB_TheLockHoldsAManualRecordUntilTheClaimCommits(t *testing.T) {
	w := newWorld(t)
	in := w.tapAt("in", w.at(0, 9, 0), w.home)
	var during atomic.Bool
	done := make(chan error, 1)
	w.svc.afterCheck = func() {
		go func() {
			_, err := w.rec.Record(w.ctx, manual.Entry{TenantID: w.tenant, EmployeeID: w.who, EnteredBy: w.admin, Direction: manual.Out, At: w.at(0, 16, 0)})
			done <- err
		}()
		select {
		case err := <-done:
			during.Store(true)
			done <- err
		case <-time.After(1500 * time.Millisecond):
		}
	}
	if _, err := w.submit(in, KindOut, w.at(0, 17, 0)); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("the manager's record: %v", err)
	}
	if during.Load() {
		t.Error("the manager's record committed while the claim's checks held: the claim is not under the person's lock")
	}
	if n := w.claims(); n != 1 {
		t.Errorf("%d claims", n)
	}
}

// TestClaimDB_ACutoffThatEndsTheAnchorsDayEarlierAdmitsNoClaim is the review's D-1 scenario: the
// check-in Mon 05:30 (cutoff 05:00), then the cutoff moved to 06:00. Before the fix Submit took
// "Sent" with a 17:00 prefill while every approval and every Edit was refused. Now one rule
// (Chain.DayCutShort, read by WindowOf) answers everywhere: no card on the next tap, Submit is
// ErrResolved, a claim sent before the change is resolved for Approve and for an Edit, and Reject
// still decides it.
func TestClaimDB_ACutoffThatEndsTheAnchorsDayEarlierAdmitsNoClaim(t *testing.T) {
	w := newWorld(t)
	early := w.tapAt("in", w.at(0, 5, 30), w.home)
	before := w.mustSubmit(early, KindOut, w.at(0, 12, 0)) // sent while the cutoff was 05:00
	other := w.tapAt("in", w.at(2, 5, 30), w.home)
	t1 := w.tapAt("in", w.at(3, 9, 0), w.home)
	w.exec(`UPDATE employees SET day_cutoff = '06:00' WHERE id = $1`, w.who)
	if card, err := w.svc.Offer(w.ctx, w.tenant, w.who, t1); err != nil || card != nil {
		t.Errorf("Offer after the change = %+v, %v; want no card", card, err)
	}
	if _, err := w.submit(other, KindOut, w.at(2, 17, 0)); !errors.Is(err, ErrResolved) {
		t.Errorf("Submit after the change: %v, want ErrResolved", err)
	}
	ins, err := w.svc.Inspect(w.ctx, w.tenant, before.ID, nil)
	if err != nil || ins.State != StateResolved || ins.Window != nil {
		t.Errorf("Inspect the earlier claim: state %s, window %+v, %v; want resolved and no window", ins.State, ins.Window, err)
	}
	edit := w.at(0, 5, 45)
	for _, at := range []*time.Time{nil, &edit} {
		if _, err := w.decide(before.ID, Approved, at, true); !errors.Is(err, ErrResolved) {
			t.Errorf("approve (edit %v): %v, want ErrResolved", at, err)
		}
	}
	if _, err := w.decide(before.ID, Rejected, nil, false); err != nil {
		t.Errorf("reject: %v", err)
	}
	if m, n := w.manualRows(), w.claims(); m != 0 || n != 1 {
		t.Errorf("%d records, %d claims; want 0 and the one sent before the change", m, n)
	}
}

// TestDecideDB_X2TheAnchorsEndOutsideTheBandAsksTheConfirmationStep is X2 end to end: the other venue
// (10:00–22:00), the check-in Mon 05:30, the checkout claimed at 23:30 -- the first Approve writes
// nothing and repeats both ends; the confirmed one writes the 18 h.
func TestDecideDB_X2TheAnchorsEndOutsideTheBandAsksTheConfirmationStep(t *testing.T) {
	w := newWorld(t)
	in := w.tapAt("in", w.at(0, 5, 30), w.other)
	c := w.mustSubmit(in, KindOut, w.at(0, 23, 30))
	d, err := w.decide(c.ID, Approved, nil, false)
	if !errors.Is(err, ErrNeedsConfirmation) || d.Assessment == nil {
		t.Fatalf("first approve: %+v, %v; want ErrNeedsConfirmation", d, err)
	}
	if a := d.Assessment; a.Interval.Duration() != 18*time.Hour || a.AnchorDeviation.Side != BeforeStart || a.Deviation.Side != AfterEnd {
		t.Errorf("the step repeats %+v", a)
	}
	if m := w.manualRows(); m != 0 {
		t.Fatalf("%d records after the first approve", m)
	}
	if _, err := w.decide(c.ID, Approved, nil, true); err != nil {
		t.Fatalf("confirmed: %v", err)
	}
}

// TestSubmitDB_AnExpiredMomentIsRefused (R14): a moment inside its window but further back than
// manual.MaxBackdate is ErrExpired, and nothing is written.
func TestSubmitDB_AnExpiredMomentIsRefused(t *testing.T) {
	w := newWorld(t)
	in := w.tapAt("in", w.at(0, 9, 0), w.home)
	w.svc.now = func() time.Time { return w.at(0, 17, 0).Add(manual.MaxBackdate + time.Hour) }
	if _, err := w.submit(in, KindOut, w.at(0, 17, 0)); !errors.Is(err, ErrExpired) {
		t.Errorf("Submit: %v, want ErrExpired", err)
	}
	if n := w.claims(); n != 0 {
		t.Errorf("%d claims", n)
	}
}

// TestSubmitDB_TheChainReachesBackPastTheAnchorsDay (R13, chainBefore): a check-in claim on an orphan
// out at Tue 01:00 needs the out's whole business day (Mon 05:00, 20 h back) to know I. With the read
// starting 27 h back the claim is judged and accepted; a read only 18 h back would start inside the
// day, find the slice unable to classify the out, and refuse the claim.
func TestSubmitDB_TheChainReachesBackPastTheAnchorsDay(t *testing.T) {
	w := newWorld(t)
	w.tapAt("in", w.at(0, 6, 0), w.home)
	w.tapAt("out", w.at(0, 10, 0), w.home)
	x := w.tapAt("out", w.at(1, 1, 0), w.home) // Tue 01:00: Monday's business day, P the 10:00 out -> (c)
	if _, err := w.submit(x, KindIn, w.at(0, 11, 0)); err != nil {
		t.Errorf("Submit: %v, want accepted", err)
	}
}

// peerWorld adds a holding peer to w: tenant KF (UTC too -- K-36 locks a holding to one zone), its
// venue and plaque, and the membership.
type peerWorld struct {
	*world
	peer, peerVenue, holding uuid.UUID
	peerTag                  string
}

func newPeerWorld(t *testing.T) *peerWorld {
	w := newWorld(t)
	p := &peerWorld{world: w, peer: uuid.New()}
	w.exec(`INSERT INTO tenants (id, name, vat_number, business_type, structure, timezone) VALUES ($1, 'm15-16 peer', $2, 'restaurant', 'multi', 'UTC')`,
		p.peer, "VAT-M1516P-"+p.peer.String())
	if err := w.owner.QueryRow(w.ctx, `INSERT INTO holdings (name) VALUES ('m15-16 claims') RETURNING id`).Scan(&p.holding); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = w.owner.Exec(bg, `DELETE FROM holding_members WHERE holding_id = $1`, p.holding)
		_, _ = w.owner.Exec(bg, `DELETE FROM holdings WHERE id = $1`, p.holding)
	})
	w.exec(`INSERT INTO holding_members (holding_id, member_tenant_id) VALUES ($1, $2), ($1, $3)`, p.holding, w.tenant, p.peer)
	if err := w.owner.QueryRow(w.ctx, `INSERT INTO locations (tenant_id, name, static_ips) VALUES ($1, 'm15-16 peer venue', '{198.51.100.0/24}') RETURNING id`,
		p.peer).Scan(&p.peerVenue); err != nil {
		t.Fatal(err)
	}
	b, key := make([]byte, 7), make([]byte, 44)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	p.peerTag = strings.ToUpper(hex.EncodeToString(b))
	w.exec(`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status) VALUES ($1, $2, $3, $4, 10000, 'active')`,
		p.peerTag, p.peer, p.peerVenue, key)
	return p
}

// peerTap writes the employer's directed tap at the PEER's plaque (ADR 0027 §5): no local venue, no
// local plaque, the site's tenant, venue and plaque named.
func (p *peerWorld) peerTap(dir string, at time.Time) uuid.UUID {
	p.t.Helper()
	s, ok := tap.BusinessDayStart(at, p.cutoff)
	if !ok {
		p.t.Fatal("no business day")
	}
	p.ctr++
	var id uuid.UUID
	if err := p.data.WithTenant(p.ctx, p.tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO transactions (tenant_id, employee_id, site_tenant_id, site_location_id, site_tag_uid, occurred_at, created_at, verdict, channel, type, sun_valid, ctr, business_day_start)
		                         VALUES ($1, $2, $3, $4, $5, $6, $6, 'ok', 'nfc', $7, true, $8, $9) RETURNING id`,
			p.tenant, p.who, p.peer, p.peerVenue, p.peerTag, at, dir, p.ctr, s).Scan(&id)
	}); err != nil {
		p.t.Fatalf("write a peer %s: %v", dir, err)
	}
	return id
}

// TestDecideDB_APeersVenueAnchorIsAnsweredAtThatVenue (review D-3, security X4): a check-in taken at a
// holding peer's plaque is answered at the peer's venue -- the employer's row, site_tenant_id the
// peer, site_location_id the peer's venue, no local venue (ADR 0032 §C.4, ADR 0027 §9). With no
// department shift there is no prefill and the step is asked. Once the partnership ends an approval
// writes nothing (ErrPartnershipEnded -- not manual's "no home venue" sentence) and Reject decides.
func TestDecideDB_APeersVenueAnchorIsAnsweredAtThatVenue(t *testing.T) {
	p := newPeerWorld(t)
	a := p.peerTap("in", p.at(0, 9, 0))
	c := p.mustSubmit(a, KindOut, p.at(0, 17, 0))
	if c.Prefilled != nil {
		t.Errorf("prefilled %v at a peer's venue with no department shift", c.Prefilled)
	}
	if _, err := p.decide(c.ID, Approved, nil, false); !errors.Is(err, ErrNeedsConfirmation) {
		t.Fatalf("first approve: %v, want the step (no shift)", err)
	}
	d, err := p.decide(c.ID, Approved, nil, true)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	var tenant uuid.UUID
	var site, venue, local *uuid.UUID
	if err := p.owner.QueryRow(p.ctx, `SELECT tenant_id, site_tenant_id, site_location_id, location_id FROM transactions WHERE id = $1`, *d.RecordID).
		Scan(&tenant, &site, &venue, &local); err != nil {
		t.Fatal(err)
	}
	if tenant != p.tenant || site == nil || *site != p.peer || venue == nil || *venue != p.peerVenue || local != nil {
		t.Errorf("the record: tenant %s, site %v, venue %v, local %v; want the employer's row at the peer's venue", tenant, site, venue, local)
	}

	b := p.peerTap("in", p.at(1, 9, 0))
	c2 := p.mustSubmit(b, KindOut, p.at(1, 17, 0))
	p.exec(`UPDATE holding_members SET left_at = clock_timestamp() WHERE member_tenant_id = $1 AND left_at IS NULL`, p.peer)
	if _, err := p.decide(c2.ID, Approved, nil, true); !errors.Is(err, ErrPartnershipEnded) {
		t.Errorf("approve after the partnership ended: %v, want ErrPartnershipEnded", err)
	}
	if n := p.count(`SELECT count(*) FROM transactions WHERE tenant_id = $1 AND channel = 'manual'`, p.tenant); n != 1 {
		t.Errorf("%d manual records, want the first approval's only", n)
	}
	if _, err := p.decide(c2.ID, Rejected, nil, false); err != nil {
		t.Errorf("reject: %v", err)
	}
}

// chosen writes a directed record of the person's at `at` with an explicit direction_source, on the
// given channel ('nfc' with its frozen business day; 'manual' with the same business day forged in, the
// one shape where a manual row could be a claim's anchor at all).
func (w *world) chosen(dir, channel, source string, at time.Time) uuid.UUID {
	w.t.Helper()
	s, ok := tap.BusinessDayStart(at, w.cutoff)
	if !ok {
		w.t.Fatal("no business day")
	}
	w.ctr++
	var id uuid.UUID
	if err := w.data.WithTenant(w.ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		if channel == "manual" {
			return tx.QueryRow(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, occurred_at, created_at, verdict, channel, type, trust, entered_by, business_day_start, direction_source)
			                         VALUES ($1, $2, $3, $4, $4, 'ok', 'manual', $5, 20, $6, $7, $8) RETURNING id`,
				w.tenant, w.who, w.home, at, dir, w.admin, s, source).Scan(&id)
		}
		return tx.QueryRow(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, created_at, verdict, channel, type, sun_valid, ctr, business_day_start, direction_source)
		                         VALUES ($1, $2, $3, $4, $5, $5, 'ok', 'nfc', $6, true, $7, $8, $9) RETURNING id`,
			w.tenant, w.who, w.home, w.plaque, at, dir, w.ctr, s, source).Scan(&id)
	}); err != nil {
		w.t.Fatalf("write a chosen %s: %v", dir, err)
	}
	return id
}

// TestDecideDB_S5AnAnchorThePersonChoseAlwaysAsksTheStep is the M15-19 security lens's S5 shape, the
// orchestrator's decision (2026-10-26): one tap at 18:55, 1 h 55 m after a 09:00–17:00 shift, "Not
// right? Clock out" -- an orphan out (c) whose direction the PERSON chose -- and the card's prefill,
// the shift's start, sent untouched. Both ends of the 9 h 55 m interval sit inside the band, so the
// band alone let one Approve write it. Now the first Approve writes nothing and asks the step; the
// step's press writes. The contrast rows: the engine's (auto) out in the same place is still one
// press, and a manual row carrying 'employee' is not believed (tap.RecordedDirectionSource) -- one
// press too.
func TestDecideDB_S5AnAnchorThePersonChoseAlwaysAsksTheStep(t *testing.T) {
	for _, tc := range []struct {
		name, dir, channel, source string
		kind                       Kind
		step                       bool
	}{
		{"the person chose the out (S5)", "out", "nfc", "employee", KindIn, true},
		{"the engine's out", "out", "nfc", "auto", KindIn, false},
		{"a manual in carrying 'employee'", "in", "manual", "employee", KindOut, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			at, claimed := w.at(0, 18, 55), w.at(0, 9, 0)
			if tc.kind == KindOut {
				at, claimed = w.at(0, 9, 0), w.at(0, 17, 0)
			}
			// the approvals' records only (the manual anchor is a manual row too)
			answers := func() int {
				return w.count(`SELECT count(*) FROM transactions WHERE tenant_id = $1 AND channel = 'manual' AND note = $2`, w.tenant, ClaimApprovedNote)
			}
			anchor := w.chosen(tc.dir, tc.channel, tc.source, at)
			c := w.mustSubmit(anchor, tc.kind, claimed)
			if tc.kind == KindIn && (c.Prefilled == nil || !c.DefaultUnchanged) {
				t.Fatalf("PREMISE: the prefill is the shift's start and was sent untouched: %+v", c)
			}
			ins, err := w.svc.Inspect(w.ctx, w.tenant, c.ID, nil)
			if err != nil || ins.Assessment.OutsideBand {
				t.Fatalf("PREMISE: both ends inside the band: %+v, %v", ins.Assessment, err)
			}
			if ins.AnchorChose != tc.step || ins.Assessment.AnchorChosen != tc.step || ins.Assessment.RequiresConfirmation != tc.step {
				t.Errorf("Inspect: mark %v, chosen %v, step %v; want %v", ins.AnchorChose, ins.Assessment.AnchorChosen,
					ins.Assessment.RequiresConfirmation, tc.step)
			}
			d, err := w.decide(c.ID, Approved, nil, false)
			switch {
			case tc.step && !errors.Is(err, ErrNeedsConfirmation):
				t.Fatalf("one Approve: %v, want ErrNeedsConfirmation", err)
			case tc.step && answers() != 0:
				t.Fatalf("one Approve wrote %d records", answers())
			case !tc.step && err != nil:
				t.Fatalf("one Approve: %v, want it written", err)
			}
			if tc.step {
				if d.Assessment == nil || !d.Assessment.AnchorChosen {
					t.Errorf("the refusal's assessment does not say why: %+v", d.Assessment)
				}
				if _, err := w.decide(c.ID, Approved, nil, true); err != nil {
					t.Fatalf("the step's press: %v", err)
				}
			}
			if n := answers(); n != 1 {
				t.Errorf("%d records, want 1", n)
			}
		})
	}
}

// TestDecideDB_AWindowThatAdmitsTooMuchIsStillRefusedByTheClassification is M15-16's re-validation
// criterion made visible (M15-19, ORTA-1): "pencere aritmetiğini bozan bir mutasyon sınıflandırma
// sınamasıyla yine kırmızı". A correct WindowOf never admits an instant the classification refuses,
// so the test widens the decision's window (Service.widen, test-only) one hour past in.At + 18 h;
// an Edit 30 minutes past 18 h is then inside it, and Chain.Closes -- the pair is too long -- refuses
// it: ErrResolved, nothing written. Without Closes in evaluate the record would be written.
func TestDecideDB_AWindowThatAdmitsTooMuchIsStillRefusedByTheClassification(t *testing.T) {
	w := newWorld(t)
	in := w.tapAt("in", w.at(0, 5, 30), w.home) // in.At + 18 h = Mon 23:30, before Next(S) Tue 05:00
	c := w.mustSubmit(in, KindOut, w.at(0, 17, 0))
	w.svc.widen = func(win Window) Window {
		win.To = Bound{At: win.To.At.Add(time.Hour), Inclusive: true}
		return win
	}
	edit := w.at(0, 23, 30).Add(30 * time.Minute)
	ins, err := w.svc.Inspect(w.ctx, w.tenant, c.ID, &edit)
	if err != nil || ins.Window == nil || !ins.Window.Contains(edit) {
		t.Fatalf("PREMISE: the widened window admits the Edit: %+v, %v", ins.Window, err)
	}
	if ins.State != StateResolved {
		t.Errorf("Inspect state %s, want resolved (the classification)", ins.State)
	}
	if _, err := w.decide(c.ID, Approved, &edit, true); !errors.Is(err, ErrResolved) {
		t.Errorf("approve the Edit: %v, want ErrResolved", err)
	}
	if n := w.manualRows(); n != 0 {
		t.Errorf("%d records: the 18 h 30 m pair was written", n)
	}
}
