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
