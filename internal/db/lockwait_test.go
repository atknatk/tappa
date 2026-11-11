package db

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestBoundLockWaits_IsLocalAndRoundsUp (T219, real Postgres, a pool of ONE connection so the next
// acquire is the same backend): inside the transaction lock_timeout is the bound rounded up to a whole
// millisecond; after it the pooled connection is back to its default, so no later user of the
// connection inherits a bound it did not ask for.
//
// MUTATION: set_config's third argument false -> the bound survives the transaction -> red.
func TestBoundLockWaits_IsLocalAndRoundsUp(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	var before string
	if err := d.pool.QueryRow(ctx, `SELECT current_setting('lock_timeout')`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	var inside string
	if err := d.WithTenant(ctx, uuid.New(), func(ctx context.Context, tx pgx.Tx) error {
		if err := BoundLockWaits(ctx, tx, 1500*time.Microsecond); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT current_setting('lock_timeout')`).Scan(&inside)
	}); err != nil {
		t.Fatalf("WithTenant: %v", err)
	}
	if inside != "2ms" {
		t.Errorf("lock_timeout inside the transaction = %q, want 2ms (1.5 ms rounded up)", inside)
	}
	var after string
	if err := d.pool.QueryRow(ctx, `SELECT current_setting('lock_timeout')`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Errorf("lock_timeout on the pooled connection after the transaction = %q, want %q: the bound leaked", after, before)
	}
}

// TestBoundLockWaits_RefusesLessThanAMillisecond: lock_timeout 0 means "wait forever", so a bound that
// would round to it is refused before the database is asked.
func TestBoundLockWaits_RefusesLessThanAMillisecond(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second, 999 * time.Microsecond} {
		if err := BoundLockWaits(context.Background(), nil, d); err == nil {
			t.Errorf("BoundLockWaits(%v) = nil, want a refusal", d)
		}
	}
}

// TestLockWaitExpired_IsOnlyTheLockTimeout: 55P03, wrapped or not, and nothing else -- in particular
// not a statement timeout (57014), which may have run past a write's start.
func TestLockWaitExpired_IsOnlyTheLockTimeout(t *testing.T) {
	lock := &pgconn.PgError{Code: "55P03"}
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{lock, true},
		{fmt.Errorf("adminauth: issue reset: %w", lock), true},
		{&pgconn.PgError{Code: "57014"}, false},
		{context.DeadlineExceeded, false},
		{nil, false},
	} {
		if got := LockWaitExpired(tc.err); got != tc.want {
			t.Errorf("LockWaitExpired(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

// TestBoundAcquireWait_CutsTheWaitForAConnection (T236 (a), real Postgres, a pool of ONE connection,
// held by another transaction): a transaction under BoundAcquireWait gives up when the bound runs out
// instead of waiting for the connection, reports cut, and never began; with the connection free, the
// same bound lets the next one through.
//
// MUTATION: the bound's timer never cancels (BoundAcquireWait returning ctx unchanged) -> the call
// waits for the holder's 2 s -> red.
func TestBoundAcquireWait_CutsTheWaitForAConnection(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	held, free := make(chan struct{}), make(chan struct{})
	holder := make(chan error, 1)
	go func() {
		holder <- d.WithTenant(ctx, uuid.New(), func(context.Context, pgx.Tx) error {
			close(held)
			select {
			case <-free:
			case <-time.After(2 * time.Second):
			}
			return nil
		})
	}()
	<-held

	bounded, cut := BoundAcquireWait(ctx, 100*time.Millisecond)
	start := time.Now()
	ran := false
	err := d.WithTenant(bounded, uuid.New(), func(context.Context, pgx.Tx) error {
		ran = true
		return nil
	})
	took := time.Since(start)
	wasCut := cut()
	close(free)
	if herr := <-holder; herr != nil {
		t.Fatalf("the holder: %v", herr)
	}
	t.Logf("the only connection held: the bounded transaction gave up after %v (bound 100ms)", took)
	if err == nil || ran || !wasCut {
		t.Fatalf("err %v, ran %v, cut %v; want an error, fn not run, cut", err, ran, wasCut)
	}
	if took > time.Second {
		t.Errorf("gave up after %v: it waited for the connection instead of the bound", took)
	}

	bounded, cut = BoundAcquireWait(ctx, 100*time.Millisecond)
	err = d.WithTenant(bounded, uuid.New(), func(context.Context, pgx.Tx) error { return nil })
	if wasCut = cut(); err != nil || wasCut {
		t.Fatalf("with the connection free: err %v, cut %v; want neither", err, wasCut)
	}
}

// TestBoundAcquireWait_DoesNotCutTheTransaction (T236 (a)): once a connection is handed over the bound
// is disarmed, so a transaction slower than the bound -- a statement that runs, not a wait -- still
// commits, and cut is false. This is what keeps the bound off the COMMIT (no lost commit answer,
// backlog T169) and off healthy work on a slow machine.
//
// MUTATION: the pool's PrepareConn hook removed (pool.go) -> the bound fires during pg_sleep, the
// statement is cancelled -> red.
func TestBoundAcquireWait_DoesNotCutTheTransaction(t *testing.T) {
	d := testDB(t)
	bounded, cut := BoundAcquireWait(context.Background(), 50*time.Millisecond)
	err := d.WithTenant(bounded, uuid.New(), func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `SELECT pg_sleep(0.3)`)
		return e
	})
	if wasCut := cut(); err != nil || wasCut {
		t.Fatalf("a transaction slower than the bound: err %v, cut %v; want it committed and not cut", err, wasCut)
	}
}

// TestBoundAcquireWait_ACutGuardRefusesBeforeTheContextIsCancelled (third-eye review of T236 (a)): the
// timer marks the guard cut and only then cancels the context, so an acquire can reach the pool's hook
// in between. The hook must refuse on the guard alone.
//
// MUTATION: the hook returning context.Cause(ctx) (nil in that window) -> it passes -> red.
func TestBoundAcquireWait_ACutGuardRefusesBeforeTheContextIsCancelled(t *testing.T) {
	ctx, cut := BoundAcquireWait(context.Background(), time.Hour)
	defer cut()
	g := ctx.Value(acquireGuardKey{}).(*acquireGuard)
	if !g.state.CompareAndSwap(acquireWaiting, acquireCut) {
		t.Fatal("PREMISE: the guard was not waiting")
	}
	if ctx.Err() != nil {
		t.Fatal("PREMISE: the context is already cancelled; the window under test is gone")
	}
	ok, err := connectionHandedOver(ctx, nil)
	if !ok || err == nil {
		t.Fatalf("a cut guard, context not yet cancelled: ok=%v err=%v; want the connection kept and the acquire refused", ok, err)
	}
	if ok, err := connectionHandedOver(context.Background(), nil); !ok || err != nil {
		t.Errorf("CONTROL: a context without a bound: ok=%v err=%v, want it passed", ok, err)
	}
}
