package checkin

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// feed_db_test.go -- M11 API-2 on the product path: the punch feed's counter wait (ADR 0028 §5;
// ADR 0027 §13 item 23). An ok tap's AFTER trigger raises its tenant's feed counter under the
// row's lock and its own lock_timeout (2 s); a transaction that holds the row longer made the
// tap's INSERT fail 55P03 -- the advanced plaque's record lost -- until checkin.insert's one
// retry took punch_feed's 55P03 in (holding_feed's was already there since API-1c).

// holdCounter takes tenant's feed counter row FOR UPDATE on a connection of its own and holds it
// (uncommitted) -- what a stuck writer of that tenant does. The returned func commits.
func (w *holdingWorld) holdCounter(tenant uuid.UUID) (release func()) {
	w.t.Helper()
	conn, err := pgx.Connect(w.ctx, os.Getenv("DATABASE_MIGRATE_URL"))
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() { _ = conn.Close(context.Background()) })
	tx, err := conn.Begin(w.ctx)
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	var n int
	if err := tx.QueryRow(w.ctx, `SELECT count(*) FROM (SELECT 1 FROM feed_counters WHERE tenant_id = $1 FOR UPDATE) k`, tenant).Scan(&n); err != nil || n != 1 {
		w.t.Fatalf("hold %v's counter row: %d rows, %v", tenant, n, err)
	}
	return func() {
		if err := tx.Commit(w.ctx); err != nil {
			w.t.Errorf("release the counter row: %v", err)
		}
	}
}

// events is how many punch_events rows name the record id, in any feed.
func (w *holdingWorld) events(id uuid.UUID) int64 {
	w.t.Helper()
	var n int64
	if err := w.owner.QueryRow(w.ctx, `SELECT count(*) FROM punch_events WHERE punch_id = $1`, id).Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// TestFeedCounter_LockWaitIsWrittenOnceMore: KF's own employee taps KF's plaque on KF's network --
// an ok tap, an event -- and before the INSERT another transaction holds KF's feed counter row.
// punch_feed waits its 2 s lock_timeout and the INSERT fails 55P03 inside punch_feed; the retry
// (the hook between the two attempts releases the hold) writes the advanced record AND its event.
// MUTANT (measured -- the punch_feed arm taken out of insert's retry condition): Record fails
// 55P03, no row at KF, KF's plaque counter advanced -- the record lost.
func TestFeedCounter_LockWaitIsWrittenOnceMore(t *testing.T) {
	w := newHoldingWorld(t)
	var release func()
	between := 0
	w.svc.hooks.beforeWrite = func(context.Context) { release = w.holdCounter(w.kf) }
	w.svc.hooks.betweenSiteWrites = func(context.Context) {
		between++
		release()
	}
	start := time.Now()
	res, err := w.svc.Record(w.ctx, w.nfc(w.kf, w.kfEmp, w.kfTag, w.kf, 101, w.kfOnNet))
	took := time.Since(start)
	w.svc.hooks = recordHooks{}
	if err != nil {
		t.Fatalf("Record: %v -- the second attempt must land the advanced record", err)
	}
	if between != 1 || took < 2*time.Second {
		t.Errorf("the hook between the attempts ran %d times after %v; want once, after the counter's 2 s wait", between, took)
	}
	if w.rows(w.kf) != 1 || w.events(res.TransactionID) != 1 {
		t.Errorf("%d rows at KF and %d events of the record, want the advanced record and its event", w.rows(w.kf), w.events(res.TransactionID))
	}
}

// TestFeedCounter_HoldingTapLockWaitIsWrittenOnceMore: the same wait on a holding punch -- KM's
// employee on KF's plaque, KM's (the employer's) counter row held. holding_feed's canonical
// pre-lock waits 2 s and fails 55P03 inside holding_feed; the retry writes the record, its two
// events and KF's projection.
func TestFeedCounter_HoldingTapLockWaitIsWrittenOnceMore(t *testing.T) {
	w := newHoldingWorld(t)
	var release func()
	between := 0
	w.svc.hooks.beforeWrite = func(context.Context) { release = w.holdCounter(w.km) }
	w.svc.hooks.betweenSiteWrites = func(context.Context) {
		between++
		release()
	}
	res, err := w.svc.Record(w.ctx, w.nfc(w.km, w.kmEmp, w.kfTag, w.kf, 101, w.kfOnNet))
	w.svc.hooks = recordHooks{}
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if between != 1 || w.rows(w.km) != 1 || w.events(res.TransactionID) != 2 {
		t.Errorf("%d retries, %d rows at KM, %d events; want 1, 1, 2", between, w.rows(w.km), w.events(res.TransactionID))
	}
}

// TestFeedCounter_LockHeldThroughBothWaitsIsTheNamedCost is the path the retry does not close (ADR
// 0027 §13 item 23): KF's counter row held through BOTH waits -- 55P03 from punch_feed after two
// 2-second waits, nothing written at KF, KF's plaque counter advanced: that record is lost, the
// gate's and the projection's class. The bound on how long a row can be held is the stuck-writer
// timeouts of the customer pool (idle_in_transaction_session_timeout 30 s, TCP keepalives) --
// TestFeedTimeouts_ShownAtStartup.
func TestFeedCounter_LockHeldThroughBothWaitsIsTheNamedCost(t *testing.T) {
	w := newHoldingWorld(t)
	between := 0
	w.svc.hooks.beforeWrite = func(context.Context) { _ = w.holdCounter(w.kf) }
	w.svc.hooks.betweenSiteWrites = func(context.Context) { between++ }
	start := time.Now()
	_, err := w.svc.Record(w.ctx, w.nfc(w.kf, w.kfEmp, w.kfTag, w.kf, 101, w.kfOnNet))
	took := time.Since(start)
	w.svc.hooks = recordHooks{}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "55P03" || !strings.Contains(pg.Where, ownFeedFunction) {
		t.Fatalf("Record: %v, want punch_feed's 55P03 after the second wait", err)
	}
	if between != 1 || took < 4*time.Second {
		t.Errorf("%d retries after %v; want one, two 2 s waits", between, took)
	}
	if n := w.rows(w.kf); n != 0 {
		t.Errorf("%d rows at KF, want none", n)
	}
	if got := w.lastCtr(w.kfTag); got != 101 {
		t.Errorf("KF's last_ctr %d, want 101 -- the advance committed before the write, which is why this path is a cost", got)
	}
}

// TestFeedCounter_RowsThatAreNoEventAreWrittenPlainly: a row neither feed trigger locks a counter
// for -- here a flag -- is written without a savepoint (feedEligible), and is written while its
// tenant's counter row is held: no wait, nothing to retry.
func TestFeedCounter_RowsThatAreNoEventAreWrittenPlainly(t *testing.T) {
	w := newHoldingWorld(t)
	release := w.holdCounter(w.kf)
	defer release()
	between := 0
	w.svc.hooks.betweenSiteWrites = func(context.Context) { between++ }
	start := time.Now()
	res, err := w.svc.Record(w.ctx, w.nfc(w.kf, w.kfEmp, w.kfTag, w.kf, 101, w.elsewhere))
	took := time.Since(start)
	w.svc.hooks = recordHooks{}
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if r := w.record(res.TransactionID); r.verdict != "flag" {
		t.Fatalf("PREMISE: a tap off KF's network with no GPS is %s, want flag", r.verdict)
	}
	if between != 0 || took > 1500*time.Millisecond || w.events(res.TransactionID) != 0 {
		t.Errorf("a flag waited %v, was retried %d time(s) and is %d event(s); want no wait, no retry, no event", took, between, w.events(res.TransactionID))
	}
}

// TestFeedDirection_NightShiftInThenOutAcrossMidnight is the user's decision K-40 on the tap path
// (ADR 0028 §4; CLAUDE.md §5's Rusty Bar rule): KF's employee checks in at 18:00 and taps again at
// 02:00 the next day. Tappa computes the second as OUT against the last OPEN check-in, never the
// calendar day, and the feed carries exactly that: the 18:00 event 'in', the 02:00 event 'out'.
// The 18:00 row is written as tappa_app in KF's context (Record cannot move the database's clock);
// the 02:00 tap goes through Record with the service's clock set to 02:00.
func TestFeedDirection_NightShiftInThenOutAcrossMidnight(t *testing.T) {
	w := newHoldingWorld(t)
	day := time.Now().UTC().Truncate(24 * time.Hour)
	checkIn, checkOut := day.Add(-6*time.Hour), day.Add(2*time.Hour) // 18:00, then 02:00 the next day
	var inID uuid.UUID
	if err := w.data.WithTenant(w.ctx, w.kf, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, created_at, verdict, channel, type, sun_valid, ctr)
		                         VALUES ($1, $2, $3, $4, $5, $5, 'ok', 'nfc', 'in', true, 100) RETURNING id`,
			w.kf, w.kfEmp, w.kfVenue, w.kfTag, checkIn).Scan(&inID)
	}); err != nil {
		t.Fatalf("the 18:00 check-in: %v", err)
	}
	w.svc.now = func() time.Time { return checkOut }
	res, err := w.svc.Record(w.ctx, w.nfc(w.kf, w.kfEmp, w.kfTag, w.kf, 101, w.kfOnNet))
	w.svc.now = nil
	if err != nil {
		t.Fatalf("Record at 02:00: %v", err)
	}
	if r := w.record(res.TransactionID); r.verdict != "ok" || str(r.kind) != "out" {
		t.Fatalf("PREMISE: the 02:00 tap is %s/%s, want ok/out", r.verdict, str(r.kind))
	}
	for id, want := range map[uuid.UUID]string{inID: "in", res.TransactionID: "out"} {
		var dir string
		if err := w.owner.QueryRow(w.ctx, `SELECT direction FROM punch_events WHERE punch_id = $1 AND tenant_id = $2`, id, w.kf).Scan(&dir); err != nil {
			t.Fatalf("the event of %v: %v", id, err)
		}
		if dir != want {
			t.Errorf("the event of the %s tap says %q", want, dir)
		}
	}
}
