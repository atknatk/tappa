package claim

// queue_db_test.go -- what the panel's claim queue reads (M15-18) against real Postgres: the anchor's
// "they chose this direction" mark, the queue's list (internal/domain/ledger.Claims) and Odd shapes'
// "claim pending" mark (ledger.Anomalies). Each test builds its own tenant (claim_db_test.go's world).

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/store"
)

// rowWith writes a directed record of the person's at `at` with its business day, on channel ch, with
// direction_source stored as given (nil: NULL). A manual row carries the admin as entered_by and no
// plaque -- the shape a code slip could write as tappa_app (no CHECK refuses it; ADR 0032 §B.4).
func (w *world) rowWith(dir string, at time.Time, ch string, source *string) uuid.UUID {
	w.t.Helper()
	s, ok := tap.BusinessDayStart(at, w.cutoff)
	if !ok {
		w.t.Fatal("no business day")
	}
	w.ctr++
	var id uuid.UUID
	if err := w.data.WithTenant(w.ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		if ch == "manual" {
			return tx.QueryRow(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, occurred_at, created_at, verdict, channel, type, trust, entered_by, business_day_start, direction_source)
			                         VALUES ($1, $2, $3, $4, $4, 'ok', 'manual', $5, 20, $6, $7, $8) RETURNING id`,
				w.tenant, w.who, w.home, at, dir, w.admin, s, source).Scan(&id)
		}
		return tx.QueryRow(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, created_at, verdict, channel, type, sun_valid, ctr, business_day_start, direction_source)
		                         VALUES ($1, $2, $3, $4, $5, $5, 'ok', $6, $7, true, $8, $9, $10) RETURNING id`,
			w.tenant, w.who, w.home, w.plaque, at, ch, dir, w.ctr, s, source).Scan(&id)
	}); err != nil {
		w.t.Fatalf("write a %s %s: %v", ch, dir, err)
	}
	return id
}

// claimDirect writes a claim on anchor as tappa_app through InsertPunchClaim -- the statement Submit
// uses, without Submit's window (00068's trigger still holds the anchor's shape).
func (w *world) claimDirect(anchor uuid.UUID, kind Kind, at time.Time) uuid.UUID {
	w.t.Helper()
	var id uuid.UUID
	if err := w.data.WithTenant(w.ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		row, err := store.New(tx).InsertPunchClaim(ctx, store.InsertPunchClaimParams{Kind: string(kind), ClaimedAt: at,
			AnchorTransactionID: anchor, TenantID: w.tenant, EmployeeID: w.who})
		id = row.ID
		return err
	}); err != nil {
		w.t.Fatalf("claim %s on %s: %v", kind, anchor, err)
	}
	return id
}

func strp(s string) *string { return &s }

// TestInspectDB_TheAnchorsMarkIsReadThroughTheReaderRule is the card's step 4 over real rows: the
// queue's "they chose this direction" mark is tap.RecordedDirectionSource's answer, so a directed nfc
// row the person chose is marked, the engine's (and a pre-M15 NULL) is not, and a MANUAL row carrying
// 'employee' -- which no code path writes and no CHECK refuses -- is not marked either.
func TestInspectDB_TheAnchorsMarkIsReadThroughTheReaderRule(t *testing.T) {
	w := newWorld(t)
	cases := []struct {
		name   string
		ch     string
		source *string
		want   bool
	}{
		{"nfc, the person chose", "nfc", strp("employee"), true},
		{"nfc, the engine chose", "nfc", strp("auto"), false},
		{"nfc, before M15 (NULL)", "nfc", nil, false},
		{"qr, the person chose", "qr", strp("employee"), true},
		{"manual carrying employee", "manual", strp("employee"), false},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			anchor := w.rowWith("in", w.at(i, 9, 0), tc.ch, tc.source)
			c := w.claimDirect(anchor, KindOut, w.at(i, 17, 0))
			in, err := w.svc.Inspect(w.ctx, w.tenant, c, nil)
			if err != nil {
				t.Fatal(err)
			}
			if in.AnchorChose != tc.want {
				t.Errorf("AnchorChose = %v, want %v", in.AnchorChose, tc.want)
			}
			if string(in.Anchor.Channel) != tc.ch {
				t.Errorf("the anchor's channel = %q, want %q", in.Anchor.Channel, tc.ch)
			}
		})
	}
}

// TestClaimQueueDB_ListsTheUndecidedClaimsOfThisBusinessOldestFirst: the queue is the business's
// claims nobody decided, oldest first; a decided claim leaves it, and another business's claims are
// never on it (RLS and the statement's own tenant predicate).
func TestClaimQueueDB_ListsTheUndecidedClaimsOfThisBusinessOldestFirst(t *testing.T) {
	w, other := newWorld(t), newWorld(t)
	reader, err := ledger.NewReader(w.data, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	a := w.mustSubmit(w.tapAt("in", w.at(0, 9, 0), w.home), KindOut, w.at(0, 17, 0))
	b := w.mustSubmit(w.tapAt("in", w.at(1, 9, 0), w.home), KindOut, w.at(1, 17, 0))
	c := w.mustSubmit(w.tapAt("in", w.at(2, 9, 0), w.home), KindOut, w.at(2, 17, 0))
	foreign := other.mustSubmit(other.tapAt("in", other.at(0, 9, 0), other.home), KindOut, other.at(0, 17, 0))
	if _, err := w.decide(b.ID, Rejected, nil, false); err != nil {
		t.Fatal(err)
	}
	q, err := reader.Claims(w.ctx, w.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if !q.Queried || q.More || q.Zone == nil {
		t.Fatalf("the read = %+v, want queried, not shortened, with a zone", q)
	}
	var got []uuid.UUID
	for _, p := range q.Claims {
		got = append(got, p.ID)
		if p.EmployeeName != "Claim Staff" || p.Kind != "out" {
			t.Errorf("claim %s reads %q %q", p.ID, p.EmployeeName, p.Kind)
		}
	}
	if len(got) != 2 || got[0] != a.ID || got[1] != c.ID {
		t.Fatalf("the queue lists %v, want [%s %s] (the rejected one gone, oldest first)", got, a.ID, c.ID)
	}
	for _, id := range got {
		if id == foreign.ID {
			t.Error("another business's claim is on this business's queue")
		}
	}
	oq, err := reader.Claims(w.ctx, other.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if len(oq.Claims) != 1 || oq.Claims[0].ID != foreign.ID {
		t.Errorf("the other business's queue lists %+v, want only its own claim", oq.Claims)
	}
}

// TestAnomaliesDB_TheClaimPendingMarkIsBoundByTheRecordsIDNotItsTime: two open check-ins of one
// person at the SAME instant, a claim on one of them -- exactly that one is marked (a mark matched by
// time would land on both). A check-in-missing checkout with a pending claim is marked; one whose
// claim was rejected is not.
func TestAnomaliesDB_TheClaimPendingMarkIsBoundByTheRecordsIDNotItsTime(t *testing.T) {
	w := newWorld(t)
	reader, err := ledger.NewReader(w.data, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	x := w.tapAt("in", w.at(0, 9, 0), w.home)
	y := w.tapAt("in", w.at(0, 9, 0), w.home)
	later, earlier := x, y
	if bytes.Compare(y[:], x[:]) > 0 {
		later, earlier = y, x
	}
	onLater := w.mustSubmit(later, KindOut, w.at(0, 17, 0))
	tue := w.tapAt("out", w.at(1, 12, 0), w.home)
	w.mustSubmit(tue, KindIn, w.at(1, 10, 0))
	wed := w.tapAt("out", w.at(2, 12, 0), w.home)
	rejected := w.mustSubmit(wed, KindIn, w.at(2, 10, 0))
	if _, err := w.decide(rejected.ID, Rejected, nil, false); err != nil {
		t.Fatal(err)
	}
	screen, err := reader.Anomalies(w.ctx, w.tenant, ledger.ReportFilter{Day: ledger.Date{Year: w.monday.Year(), Month: w.monday.Month(), Day: w.monday.Day()}})
	if err != nil {
		t.Fatal(err)
	}
	marked := map[uuid.UUID]bool{}
	open := 0
	for _, o := range screen.Open {
		if o.ID == x || o.ID == y {
			open++
			if !o.At.Equal(w.at(0, 9, 0)) {
				t.Errorf("open entry %s at %s", o.ID, o.At)
			}
		}
		marked[o.ID] = o.ClaimPending
		if o.ID == later && o.PendingClaimID != onLater.ID {
			t.Errorf("the marked check-in names claim %s, want %s (the mark's link)", o.PendingClaimID, onLater.ID)
		}
	}
	if open != 2 {
		t.Fatalf("the open list holds %d of the two same-instant check-ins (%+v); the test needs both", open, screen.Open)
	}
	if !marked[later] || marked[earlier] {
		t.Errorf("marks: claimed %s=%v, same instant %s=%v; want only the claimed one", later, marked[later], earlier, marked[earlier])
	}
	inMissing := map[uuid.UUID]bool{}
	for _, o := range screen.InMissing {
		inMissing[o.ID] = o.ClaimPending
	}
	if v, ok := inMissing[tue]; !ok || !v {
		t.Errorf("Tuesday's check-in-missing checkout: listed %v, marked %v; want listed and marked", ok, v)
	}
	if v, ok := inMissing[wed]; !ok || v {
		t.Errorf("Wednesday's checkout (claim rejected): listed %v, marked %v; want listed and unmarked", ok, v)
	}
}

// TestPendingDB_TheClaimCountIsThisBusinesssOwn (user decision "Üç yerde sayı"): the count the Spike's
// link, its empty state and The Pass print is this business's undecided claims -- a decided one leaves
// it, another business's never enters it.
func TestPendingDB_TheClaimCountIsThisBusinesssOwn(t *testing.T) {
	w, other := newWorld(t), newWorld(t)
	reader, err := ledger.NewReader(w.data, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	count := func(tenant uuid.UUID) ledger.ClaimCount {
		t.Helper()
		p, err := reader.Pending(w.ctx, tenant)
		if err != nil {
			t.Fatal(err)
		}
		return p.Claims
	}
	if c := count(w.tenant); !c.Queried || c.Failed || c.N != 0 {
		t.Fatalf("a business with no claim counts %+v, want a measured 0", c)
	}
	w.mustSubmit(w.tapAt("in", w.at(0, 9, 0), w.home), KindOut, w.at(0, 17, 0))
	decided := w.mustSubmit(w.tapAt("in", w.at(1, 9, 0), w.home), KindOut, w.at(1, 17, 0))
	w.mustSubmit(w.tapAt("in", w.at(2, 9, 0), w.home), KindOut, w.at(2, 17, 0))
	if _, err := w.decide(decided.ID, Rejected, nil, false); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		other.mustSubmit(other.tapAt("in", other.at(i, 9, 0), other.home), KindOut, other.at(i, 17, 0))
	}
	if c := count(w.tenant); c.N != 2 || c.Capped {
		t.Errorf("this business counts %+v, want 2 (three sent, one decided, the other business's three not counted)", c)
	}
	if c := count(other.tenant); c.N != 3 {
		t.Errorf("the other business counts %+v, want its own 3", c)
	}
}

// TestDecideDB_AConfirmationForAnotherShiftIsNotAConfirmation (M15-18 review round, security DÜŞÜK-1):
// the step was signed against the 09:00–17:00 instance; the venue's shift is 09:00–18:00 when Decide
// judges under the lock -- the approval writes nothing and asks the step again; the subject of the
// assessment Decide made is what writes.
func TestDecideDB_AConfirmationForAnotherShiftIsNotAConfirmation(t *testing.T) {
	w := newWorld(t)
	c := w.mustSubmit(w.tapAt("in", w.at(0, 9, 0), w.home), KindOut, w.at(1, 1, 0))
	in, err := w.svc.Inspect(w.ctx, w.tenant, c.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	signed := ConfirmationSubject(c.ID, in.Assessment)
	w.exec(`UPDATE locations SET shift_end = '18:00' WHERE id = $1`, w.home)
	got, err := w.svc.Decide(w.ctx, Decision{TenantID: w.tenant, ClaimID: c.ID, DecidedBy: w.admin, Outcome: Approved, Confirmation: signed})
	if !errors.Is(err, ErrNeedsConfirmation) || got.Assessment == nil {
		t.Fatalf("Decide with the old shift's subject = %v, want ErrNeedsConfirmation with the new assessment", err)
	}
	if n := w.manualRows(); n != 0 {
		t.Fatalf("%d manual record(s) written against a shift nobody was shown", n)
	}
	if _, err := w.svc.Decide(w.ctx, Decision{TenantID: w.tenant, ClaimID: c.ID, DecidedBy: w.admin, Outcome: Approved,
		Confirmation: ConfirmationSubject(c.ID, *got.Assessment)}); err != nil {
		t.Fatalf("Decide with the new shift's subject: %v", err)
	}
	if n := w.manualRows(); n != 1 {
		t.Errorf("%d manual record(s), want 1", n)
	}
}

// TestPendingDB_TheClaimCountStopsAtItsCap (M15-18 last round): 101 undecided claims count as "100+" --
// N is ledger.PendingCap and Capped is set -- never 101; the three places print the count as given
// (TestClaimQueue_TheWaitingClaimsAreCountedInThreePlaces, its "capped" case).
func TestPendingDB_TheClaimCountStopsAtItsCap(t *testing.T) {
	w := newWorld(t)
	reader, err := ledger.NewReader(w.data, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < ledger.PendingCap+1; i++ {
		at := w.at(0, 9, 0).Add(time.Duration(i) * time.Minute)
		w.claimDirect(w.rowWith("in", at, "nfc", nil), KindOut, at.Add(30*time.Second))
	}
	p, err := reader.Pending(w.ctx, w.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if c := p.Claims; !c.Queried || c.N != ledger.PendingCap || !c.Capped {
		t.Errorf("%d undecided claims count %+v, want N=%d and Capped", ledger.PendingCap+1, c, ledger.PendingCap)
	}
}
