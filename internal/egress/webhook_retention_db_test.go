package egress_test

// webhook_retention_db_test.go -- T130 on the worker's path (ADR 0028 "T130 uygulama notu"): the
// retention sweep runs inside the delivery job's turn, for the business the worker was handed
// (K-19 -- never a walk over businesses), at most once per SweepEvery unless its batch was full; its
// failure never stops a send; the owner's log and "Send again now" see a settled delivery for
// RetentionDays and no longer; and a sweep beside two pods' claims and beside a resend's lock loses
// and doubles nothing. The query, the policy, the trigger, the index and the migrations are
// internal/db's (webhookretention_test.go). Rows committed here stay (the feed is append-only; tags are
// never deleted), like the rest of this directory's fixtures.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/egress"
	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/internal/webhook/delivery"
	"github.com/atknatk/tappa/internal/webhook/endpoints"
)

// age writes a delivery's state and settled_at as the owner with the triggers off (00058's trigger
// dates a change of state itself) -- a lab row at a chosen age. ago "" is NULL (pending).
func (w *world) age(b wbiz, id uuid.UUID, state, ago string) {
	w.t.Helper()
	var at any
	if ago != "" {
		at = ago
	}
	c := w.owner()
	tx, err := c.Begin(w.ctx)
	if err != nil {
		w.t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(w.ctx) }()
	for _, s := range []struct {
		sql  string
		args []any
	}{
		{`SET LOCAL session_replication_role = replica`, nil},
		{`UPDATE webhook_deliveries SET state = $3, settled_at = statement_timestamp() - $4::interval, attempt = CASE WHEN $3 = 'pending' THEN attempt ELSE 6 END,
		         delivered_at = CASE WHEN $3 = 'delivered' THEN statement_timestamp() - $4::interval END WHERE tenant_id = $1 AND id = $2`, []any{b.id, id, state, at}},
	} {
		if _, err := tx.Exec(w.ctx, s.sql, s.args...); err != nil {
			w.t.Fatalf("%s: %v", s.sql, err)
		}
	}
	if err := tx.Commit(w.ctx); err != nil {
		w.t.Fatal(err)
	}
}

// present reports which of ids the business still holds.
func (w *world) present(b wbiz, ids ...uuid.UUID) map[uuid.UUID]bool {
	w.t.Helper()
	out := map[uuid.UUID]bool{}
	for _, d := range w.deliveries(b) {
		out[d.id] = true
	}
	got := map[uuid.UUID]bool{}
	for _, id := range ids {
		got[id] = out[id]
	}
	return got
}

// taps writes n taps of b and returns their deliveries' ids (the business's newest n, in order).
func (w *world) taps(b wbiz, n int) []uuid.UUID {
	w.t.Helper()
	before := len(w.deliveries(b))
	for range n {
		w.tap(b, nil, "ok")
	}
	rows := w.deliveries(b)
	if len(rows) != before+n {
		w.t.Fatalf("%d taps wrote %d deliveries", n, len(rows)-before)
	}
	var out []uuid.UUID
	for _, d := range rows[before:] {
		out = append(out, d.id)
	}
	return out
}

// TestWebhookRetention_TheSweepRunsOnTheBusinessesTurn: RunFor(A) deletes A's settled deliveries past
// 90 days (a delivered and a dead one), keeps A's 89-day one, still sends A's pending delivery in the
// same run, and touches nothing of B -- B, never run, keeps its old rows (K-19: the sweep is the
// business's own turn; nothing walks the businesses). A second run of the SAME job within SweepEvery
// does not sweep again (a row aged past 90 days meanwhile stays); a fresh job -- another process --
// sweeps at once.
//
// MUTANT (measured, T130): the sweep call removed from RunFor -> red; the throttle's check removed ->
// the second run deletes the newly aged row, red.
func TestWebhookRetention_TheSweepRunsOnTheBusinessesTurn(t *testing.T) {
	w := newWorld(t)
	a, b := w.biz("RA", false), w.biz("RB", false)
	ra, rb := w.taps(a, 4), w.taps(b, 3)
	for _, x := range []struct {
		biz   wbiz
		ids   []uuid.UUID
		state []string
		ago   []string
	}{
		{a, ra[:3], []string{"delivered", "dead", "delivered"}, []string{"91 days", "91 days", "89 days"}},
		{b, rb, []string{"delivered", "dead", "delivered"}, []string{"91 days", "91 days", "89 days"}},
	} {
		for i, id := range x.ids {
			w.age(x.biz, id, x.state[i], x.ago[i])
		}
	}
	job := w.job(w.client(5*time.Second, nil), nil, nil)
	if err := job.RunFor(w.ctx, a.id); err != nil {
		t.Fatalf("RunFor(A): %v", err)
	}
	got := w.present(a, ra...)
	if got[ra[0]] || got[ra[1]] || !got[ra[2]] || !got[ra[3]] {
		t.Errorf("after A's turn A holds %v (want the two 91-day rows gone, the 89-day and the pending one kept)", got)
	}
	for _, d := range w.deliveries(a) {
		if d.id == ra[3] && d.state != delivery.StateDelivered {
			t.Errorf("A's pending delivery is %s after the run: the sweep held the sends back", d.state)
		}
	}
	for id, left := range w.present(b, rb...) {
		if !left {
			t.Errorf("A's turn deleted B's delivery %s", id)
		}
	}
	w.age(a, ra[2], "delivered", "91 days")
	if err := job.RunFor(w.ctx, a.id); err != nil {
		t.Fatalf("RunFor(A) again: %v", err)
	}
	if !w.present(a, ra[2])[ra[2]] {
		t.Error("the same job swept A again within SweepEvery")
	}
	if err := w.job(w.client(5*time.Second, nil), nil, nil).RunFor(w.ctx, a.id); err != nil {
		t.Fatalf("a fresh job's RunFor(A): %v", err)
	}
	if w.present(a, ra[2])[ra[2]] {
		t.Error("a fresh job (another process) did not sweep A's row past 90 days")
	}
	if left := w.present(b, rb...); !left[rb[0]] || !left[rb[1]] {
		t.Error("B was swept without a turn of its own")
	}
}

// TestWebhookRetention_AFullBatchSweepsAgainOnTheNextTurn: 101 settled rows past 90 days -- one turn
// deletes the batch (100), and because the batch was full the next turn of the same job sweeps again
// (the last one); after a turn that found less than a batch the job waits SweepEvery.
func TestWebhookRetention_AFullBatchSweepsAgainOnTheNextTurn(t *testing.T) {
	w := newWorld(t)
	b := w.biz("RF", false)
	ids := w.taps(b, 102)
	for _, id := range ids[:101] {
		w.age(b, id, "delivered", "100 days")
	}
	w.age(b, ids[101], "delivered", "10 days")
	job := w.job(w.client(5*time.Second, nil), nil, nil)
	left := func() int {
		n := 0
		for _, ok := range w.present(b, ids[:101]...) {
			if ok {
				n++
			}
		}
		return n
	}
	if err := job.RunFor(w.ctx, b.id); err != nil {
		t.Fatal(err)
	}
	if n := left(); n != 1 {
		t.Fatalf("after one turn %d of 101 old rows are left, want 1 (a batch of 100)", n)
	}
	if err := job.RunFor(w.ctx, b.id); err != nil {
		t.Fatal(err)
	}
	if n := left(); n != 0 {
		t.Fatalf("the turn after a full batch left %d old rows, want 0", n)
	}
	w.age(b, ids[101], "delivered", "100 days")
	if err := job.RunFor(w.ctx, b.id); err != nil {
		t.Fatal(err)
	}
	if !w.present(b, ids[101])[ids[101]] {
		t.Error("the turn after a short batch swept again within SweepEvery")
	}
}

// failingSweep fails the job's first transaction (the sweep is the run's first) with a SQLSTATE
// whose message carries a canary, and passes every later one through.
type failingSweep struct {
	inner  *db.DB
	canary string
	mu     sync.Mutex
	calls  int
}

func (f *failingSweep) WithTenant(ctx context.Context, id uuid.UUID, fn db.TxFunc) error {
	f.mu.Lock()
	f.calls++
	first := f.calls == 1
	f.mu.Unlock()
	if first {
		return fmt.Errorf("sweep: %w", &pgconn.PgError{Code: "57014", Message: "canceling statement " + f.canary})
	}
	return f.inner.WithTenant(ctx, id, fn)
}

// TestWebhookRetention_ASweepFailureDoesNotStopTheDeliveries: the sweep's transaction fails -- the run
// still sends the business's pending delivery; the one log line names the tenant and the closed class
// (the SQLSTATE) and nothing of the error's text; the old row stays, and the next turn sweeps it (a
// failed sweep does not start the throttle).
func TestWebhookRetention_ASweepFailureDoesNotStopTheDeliveries(t *testing.T) {
	w := newWorld(t)
	b := w.biz("RX", false)
	ids := w.taps(b, 2)
	w.age(b, ids[0], "dead", "120 days")
	var buf bytes.Buffer
	var mu sync.Mutex
	logger := slog.New(slog.NewTextHandler(writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return buf.Write(p)
	}), nil))
	fdb := &failingSweep{inner: w.app, canary: w.canary}
	j, err := delivery.New(w.kek, fdb, w.client(5*time.Second, nil), logger)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.RunFor(w.ctx, b.id); err != nil {
		t.Fatalf("RunFor with a failing sweep: %v", err)
	}
	for _, d := range w.deliveries(b) {
		if d.id == ids[1] && d.state != delivery.StateDelivered {
			t.Fatalf("the pending delivery is %s: a failed sweep stopped the send", d.state)
		}
	}
	if len(w.rcv.to(b.host)) != 1 {
		t.Fatalf("%d requests reached the receiver, want 1", len(w.rcv.to(b.host)))
	}
	mu.Lock()
	logged := buf.String()
	mu.Unlock()
	var line string
	for _, l := range strings.Split(logged, "\n") {
		if strings.Contains(l, "retention sweep failed") {
			line = l
		}
	}
	if line == "" || !strings.Contains(line, "tenant_id="+b.id.String()) || !strings.Contains(line, "class=sqlstate_57014") {
		t.Fatalf("the failure's log line is %q, want the tenant and class=sqlstate_57014", line)
	}
	if strings.Contains(logged, w.canary) || strings.Contains(logged, "canceling") {
		t.Fatal("the error's text reached the log")
	}
	if !w.present(b, ids[0])[ids[0]] {
		t.Fatal("PREMISE: the failed sweep deleted the old row")
	}
	if err := j.RunFor(w.ctx, b.id); err != nil {
		t.Fatal(err)
	}
	if w.present(b, ids[0])[ids[0]] {
		t.Error("the turn after a failed sweep did not sweep (the failure started the throttle)")
	}
}

// TestWebhookRetention_TheLogAndTheResendSeeTheWindow: the owner's delivery log lists a dead delivery
// that died 89 days ago and the pending one, and NOT one that died 91 days ago though no sweep has run
// yet (what the owner sees does not depend on when the sweep last ran); "Send again now" on the
// 91-day one is the unknown delivery (ErrNoDelivery, no slot spent), on the 89-day one it is accepted
// -- the row is pending again and undated.
func TestWebhookRetention_TheLogAndTheResendSeeTheWindow(t *testing.T) {
	w := newWorld(t)
	b := w.biz("RW", false)
	ids := w.taps(b, 3)
	w.age(b, ids[0], "dead", "91 days")
	w.age(b, ids[1], "dead", "89 days")
	page, err := w.svc.Deliveries(w.ctx, b.id, b.owner, endpoints.DeliveryPosition{})
	if err != nil {
		t.Fatal(err)
	}
	listed := map[uuid.UUID]bool{}
	for _, d := range page.Deliveries {
		listed[d.ID] = true
	}
	if listed[ids[0]] || !listed[ids[1]] || !listed[ids[2]] {
		t.Errorf("the log lists %v of [91 days, 89 days, pending]: want the last two only", []bool{listed[ids[0]], listed[ids[1]], listed[ids[2]]})
	}
	slots := countTrail(t, w, b, endpoints.ActionResend)
	if err := w.svc.Resend(w.ctx, b.id, b.owner, ids[0]); !errors.Is(err, endpoints.ErrNoDelivery) {
		t.Errorf("resend of the 91-day delivery: %v, want ErrNoDelivery", err)
	}
	if n := countTrail(t, w, b, endpoints.ActionResend); n != slots {
		t.Errorf("the refused resend spent a slot (%d -> %d)", slots, n)
	}
	if err := w.svc.Resend(w.ctx, b.id, b.owner, ids[1]); err != nil {
		t.Fatalf("resend of the 89-day delivery: %v", err)
	}
	var state string
	var settled *time.Time
	if err := w.app.WithTenant(w.ctx, b.id, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT state, settled_at FROM webhook_deliveries WHERE tenant_id = $1 AND id = $2`, b.id, ids[1]).Scan(&state, &settled)
	}); err != nil {
		t.Fatal(err)
	}
	if state != delivery.StatePending || settled != nil {
		t.Errorf("the resent delivery is %s, settled_at %v: want pending and undated", state, settled)
	}
}

// recordingSender answers 204 and counts every delivery id it was handed.
type recordingSender struct {
	mu   sync.Mutex
	seen map[string]int
}

func (r *recordingSender) Post(_ context.Context, _ egress.Endpoint, body []byte, _ http.Header) egress.Result {
	var m struct {
		DeliveryID string `json:"deliveryId"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		m.DeliveryID = "undecodable"
	}
	r.mu.Lock()
	r.seen[m.DeliveryID]++
	r.mu.Unlock()
	return egress.Result{Status: http.StatusNoContent}
}

// TestWebhookRetention_ASweepBesideTwoPodsLosesNothing: two jobs on two pools (two pods) run the SAME
// business at the same time over 20 pending deliveries, 30 settled past 90 days and 5 settled 10 days
// ago -- each job sweeps on its first turn, concurrently with the other's claims and sweep. Every
// pending delivery is sent exactly once and is delivered; the 30 are gone; the 5 stay.
func TestWebhookRetention_ASweepBesideTwoPodsLosesNothing(t *testing.T) {
	w := newWorld(t)
	b := w.biz("RP", false)
	ids := w.taps(b, 55)
	pending, old, recent := ids[:20], ids[20:50], ids[50:]
	for _, id := range old {
		w.age(b, id, "dead", "100 days")
	}
	for _, id := range recent {
		w.age(b, id, "delivered", "10 days")
	}
	sender := &recordingSender{seen: map[string]int{}}
	pods := []*delivery.Job{w.job(sender, appDB(t, ""), nil), w.job(sender, appDB(t, ""), nil)}
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, j := range pods {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range 5 {
				var later *delivery.Later
				if err := j.RunFor(context.Background(), b.id); err != nil && !errors.As(err, &later) {
					errs <- err
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("a pod's run: %v", err)
	}
	for _, id := range pending {
		if k := sender.seen[delivery.IDPrefix+id.String()]; k != 1 {
			t.Errorf("pending delivery %s was sent %d times", id, k)
		}
	}
	if len(sender.seen) != len(pending) {
		t.Errorf("%d distinct deliveries were sent, want the %d pending", len(sender.seen), len(pending))
	}
	have := w.present(b, ids...)
	for _, id := range old {
		if have[id] {
			t.Errorf("an old settled delivery %s survived both pods' sweeps", id)
		}
	}
	for _, id := range append(append([]uuid.UUID{}, pending...), recent...) {
		if !have[id] {
			t.Errorf("delivery %s (pending or recent) was deleted", id)
		}
	}
	for _, d := range w.deliveries(b) {
		for _, id := range pending {
			if d.id == id && (d.state != delivery.StateDelivered || d.attempt != 1) {
				t.Errorf("delivery %s: %s after %d tries", d.id, d.state, d.attempt)
			}
		}
	}
}

// TestWebhookRetention_ASweepSkipsARowAResendHolds: a resend's lock taken at the very edge of the
// window (here: on a row 91 days dead, locked FOR UPDATE as the resend's statement locks it) -- the
// sweep in another session does not wait for it (SKIP LOCKED: it answers at once) and does not delete
// it; the resend commits the row back to pending; the next sweep keeps it (a pending row is never a
// candidate). And the other way round: while a sweep's deletion of a row is uncommitted, a resend's
// lock waits for it and, once it commits, finds no row -- never a revived ghost.
func TestWebhookRetention_ASweepSkipsARowAResendHolds(t *testing.T) {
	w := newWorld(t)
	b := w.biz("RL", false)
	ids := w.taps(b, 2)
	w.age(b, ids[0], "dead", "91 days")
	w.age(b, ids[1], "dead", "91 days")
	sweep := func(ctx context.Context) (int64, error) {
		var n int64
		err := w.app.WithTenant(ctx, b.id, func(ctx context.Context, tx pgx.Tx) error {
			var e error
			n, e = store.New(tx).DeleteSettledWebhookDeliveries(ctx, store.DeleteSettledWebhookDeliveriesParams{
				TenantID: b.id, RetentionDays: delivery.RetentionDays, MaxRows: 100,
			})
			return e
		})
		return n, err
	}

	// 1. The resend holds ids[0].
	held, release := make(chan struct{}), make(chan struct{})
	resend := make(chan error, 1)
	go func() {
		resend <- w.app.WithTenant(w.ctx, b.id, func(ctx context.Context, tx pgx.Tx) error {
			var id uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT id FROM webhook_deliveries WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, b.id, ids[0]).Scan(&id); err != nil {
				return err
			}
			close(held)
			<-release
			n, err := store.New(tx).ResendWebhookDelivery(ctx, store.ResendWebhookDeliveryParams{TenantID: b.id, ID: ids[0]})
			if err == nil && n != 1 {
				err = fmt.Errorf("the resend made %d rows due", n)
			}
			return err
		})
	}()
	<-held
	start := time.Now()
	n, err := sweep(w.ctx)
	took := time.Since(start)
	close(release)
	if err := <-resend; err != nil {
		t.Fatalf("the resend: %v", err)
	}
	if err != nil || n != 1 || took > time.Second {
		t.Fatalf("the sweep beside the resend's lock: %d rows, %v, after %v -- want the other row only, at once", n, err, took)
	}
	have := w.present(b, ids...)
	if !have[ids[0]] || have[ids[1]] {
		t.Fatalf("after the sweep beside the resend: %v", have)
	}
	if n, err := sweep(w.ctx); err != nil || n != 0 || !w.present(b, ids[0])[ids[0]] {
		t.Fatalf("the next sweep: %d rows, %v -- the resent (pending) row must stay", n, err)
	}

	// 2. A sweep's uncommitted deletion of a third row; the resend's lock waits, then finds nothing.
	third := w.taps(b, 1)[0]
	w.age(b, third, "dead", "91 days")
	deleting, commit := make(chan struct{}), make(chan struct{})
	swept := make(chan error, 1)
	go func() {
		swept <- w.app.WithTenant(w.ctx, b.id, func(ctx context.Context, tx pgx.Tx) error {
			n, err := store.New(tx).DeleteSettledWebhookDeliveries(ctx, store.DeleteSettledWebhookDeliveriesParams{
				TenantID: b.id, RetentionDays: delivery.RetentionDays, MaxRows: 100,
			})
			if err == nil && n != 1 {
				err = fmt.Errorf("the sweep deleted %d rows, want the third", n)
			}
			close(deleting)
			<-commit
			return err
		})
	}()
	<-deleting
	locked := make(chan error, 1)
	go func() {
		locked <- w.app.WithTenant(w.ctx, b.id, func(ctx context.Context, tx pgx.Tx) error {
			var id uuid.UUID
			return tx.QueryRow(ctx, `SELECT id FROM webhook_deliveries WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, b.id, third).Scan(&id)
		})
	}()
	select {
	case err := <-locked:
		t.Fatalf("the lock did not wait for the sweep's uncommitted deletion: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(commit)
	if err := <-swept; err != nil {
		t.Fatal(err)
	}
	if err := <-locked; !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("after the sweep committed the waiting lock got %v, want no row", err)
	}
}
