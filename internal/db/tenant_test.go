package db

import (
	"context"
	"errors"
	"net/url"
	"os"
	"testing"

	"github.com/atknatk/tappa/internal/config"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// These tests run against a REAL Postgres (CLAUDE.md §8 / Q04): RLS and the
// transaction-local nature of set_config cannot be proven against a fake DB.
// They connect as tappa_app via DATABASE_URL.

// testDB opens a pool pinned to a SINGLE connection (pool_max_conns=1). Pinning
// is essential for the leak proof: WithTenant runs its transaction on a
// connection it acquires from the pool internally, then releases it. With one
// connection in the pool, a later Acquire returns that SAME physical backend, so
// we can inspect whether the tenant context survived the transaction. Without
// pinning, Acquire might hand back a different, never-touched connection and the
// leak assertion would pass vacuously. We additionally verify sameness via
// pg_backend_pid() in TestWithTenant_NoLeakAfterReturn.
func testDB(t *testing.T) *DB {
	t.Helper()
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		t.Skip("DATABASE_URL not set; skipping tenant DB tests (real Postgres required — CLAUDE.md §8, Q04)")
	}
	db, err := New(context.Background(), &config.Config{DatabaseURL: pinToOneConn(t, raw)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(db.Close)
	return db
}

func pinToOneConn(t *testing.T, dsn string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	q := u.Query()
	q.Set("pool_max_conns", "1")
	u.RawQuery = q.Encode()
	return u.String()
}

// TestWithTenant_SetsContext: inside the transaction, app.tenant_id equals the
// tenant id passed in.
func TestWithTenant_SetsContext(t *testing.T) {
	db := testDB(t)
	tenantID := uuid.New()

	var got string
	err := db.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT current_setting('app.tenant_id', true)").Scan(&got)
	})
	if err != nil {
		t.Fatalf("WithTenant: %v", err)
	}
	if got != tenantID.String() {
		t.Fatalf("app.tenant_id inside tx = %q, want %q", got, tenantID.String())
	}
}

// TestWithTenant_NoLeakAfterReturn is the compensation proof required by the
// M1-07 card: after WithTenant returns, the SAME pooled connection must not
// carry the tenant context. set_config(..., true) is transaction-local, so once
// the transaction ends the GUC persists as an empty string, never as the tenant
// UUID (ADR 0002 / Q27 measurement).
func TestWithTenant_NoLeakAfterReturn(t *testing.T) {
	db := testDB(t)
	tenantID := uuid.New()

	var txPID uint32
	err := db.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var scoped string
		if err := tx.QueryRow(ctx, "SELECT current_setting('app.tenant_id', true)").Scan(&scoped); err != nil {
			return err
		}
		if scoped != tenantID.String() {
			t.Errorf("context not set inside tx: got %q, want %q", scoped, tenantID.String())
		}
		return tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&txPID)
	})
	if err != nil {
		t.Fatalf("WithTenant: %v", err)
	}

	// Acquire the pinned connection back and confirm it is the same backend the
	// transaction ran on, so the leak assertion is about that connection.
	conn, err := db.pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer conn.Release()

	var connPID uint32
	if err := conn.QueryRow(context.Background(), "SELECT pg_backend_pid()").Scan(&connPID); err != nil {
		t.Fatalf("read backend pid: %v", err)
	}
	if connPID != txPID {
		t.Fatalf("expected the same backend (pool_max_conns=1): tx pid=%d, conn pid=%d", txPID, connPID)
	}

	var after string
	if err := conn.QueryRow(context.Background(), "SELECT current_setting('app.tenant_id', true)").Scan(&after); err != nil {
		t.Fatalf("read app.tenant_id after tx: %v", err)
	}
	if after == tenantID.String() {
		t.Fatalf("tenant context leaked onto the pooled connection: %q", after)
	}
	if after != "" {
		t.Fatalf("app.tenant_id after tx = %q, want empty (transaction-local set must not persist)", after)
	}
}

var errSentinel = errors.New("sentinel: fn failed")

// TestWithTenant_ErrorRollsBack: when fn returns an error, WithTenant returns
// that error (unwrappable) and the transaction is rolled back. Proven with a
// TEMP table created in the aborted transaction: on the pinned connection it
// must not exist afterward. A committed control table proves the probe can
// actually detect existence (so the "absent" assertion is not vacuous).
func TestWithTenant_ErrorRollsBack(t *testing.T) {
	db := testDB(t)
	tenantID := uuid.New()

	err := db.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, "CREATE TEMP TABLE rollback_probe (x int)"); e != nil {
			return e
		}
		return errSentinel
	})
	if !errors.Is(err, errSentinel) {
		t.Fatalf("WithTenant error = %v, want errSentinel", err)
	}

	// Positive control: a committed TEMP table IS visible on the same session.
	if err := db.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "CREATE TEMP TABLE commit_probe (x int)")
		return e
	}); err != nil {
		t.Fatalf("control WithTenant: %v", err)
	}

	conn, err := db.pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer conn.Release()

	if got := regclass(t, conn, "rollback_probe"); got != "" {
		t.Fatalf("rolled-back TEMP table exists (%q): fn error did not roll back the tx", got)
	}
	if got := regclass(t, conn, "commit_probe"); got == "" {
		t.Fatal("committed TEMP table missing: probe cannot detect existence, so the rollback assertion is meaningless")
	}
}

// TestWithTenant_PanicRollsBackAndRepanics: a panic inside fn must roll back the
// transaction and propagate the panic to the caller.
func TestWithTenant_PanicRollsBackAndRepanics(t *testing.T) {
	db := testDB(t)
	tenantID := uuid.New()

	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("WithTenant did not re-panic")
			}
			if r != "boom" {
				t.Fatalf("recovered %v, want \"boom\"", r)
			}
		}()
		_ = db.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
			if _, e := tx.Exec(ctx, "CREATE TEMP TABLE panic_probe (x int)"); e != nil {
				return e
			}
			panic("boom")
		})
		t.Fatal("WithTenant returned instead of propagating the panic")
	}()

	// After the panic-rollback the connection is back in the pool clean, and the
	// TEMP table created in the aborted transaction must not exist.
	conn, err := db.pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire after panic: %v", err)
	}
	defer conn.Release()
	if got := regclass(t, conn, "panic_probe"); got != "" {
		t.Fatalf("TEMP table exists after panic (%q): the panic did not roll back the tx", got)
	}
}

// TestWithTenant_RejectsNilTenant: the nil UUID is rejected before any DB work,
// so this case needs no Postgres.
func TestWithTenant_RejectsNilTenant(t *testing.T) {
	d := &DB{} // pool is never touched: the guard returns first.
	err := d.WithTenant(context.Background(), uuid.Nil, func(ctx context.Context, tx pgx.Tx) error {
		t.Fatal("fn must not run for the nil tenant")
		return nil
	})
	if err == nil {
		t.Fatal("WithTenant(nil tenant) returned nil, want an error")
	}
}

// regclass returns the resolved name of a TEMP table, or "" if it does not
// exist on the connection's session. COALESCE keeps the result non-NULL so it
// scans cleanly into a string.
func regclass(t *testing.T, conn interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, name string) string {
	t.Helper()
	var out string
	if err := conn.QueryRow(context.Background(),
		"SELECT COALESCE(to_regclass('pg_temp.' || $1)::text, '')", name).Scan(&out); err != nil {
		t.Fatalf("regclass(%q): %v", name, err)
	}
	return out
}

// TestWithTenantIn_SwitchesInASavepointAndGivesTheContextBack (M11 API-11 fix 1): inside a
// WithTenant(outer) transaction, WithTenantIn runs fn in tenantID's context; after it returns -- on
// success and on fn's error alike -- the transaction is back in outer's context and still usable; a
// write fn made survives only when fn succeeded (the savepoint is released or rolled back); and a
// nil tenant on either side is refused before anything runs.
//
// MUTANT (measured): the restore to outer removed -- the success row reads tenantID after the
// return: red.
func TestWithTenantIn_SwitchesInASavepointAndGivesTheContextBack(t *testing.T) {
	db := testDB(t)
	outer, inner := uuid.New(), uuid.New()
	ctx := context.Background()
	setting := func(ctx context.Context, tx pgx.Tx) string {
		t.Helper()
		var got string
		if err := tx.QueryRow(ctx, "SELECT current_setting('app.tenant_id', true)").Scan(&got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	boom := errors.New("boom")
	err := db.WithTenant(ctx, outer, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "CREATE TEMP TABLE wti (v int) ON COMMIT DROP"); err != nil {
			return err
		}
		var inside string
		if err := WithTenantIn(ctx, tx, outer, inner, func(ctx context.Context, sp pgx.Tx) error {
			inside = setting(ctx, sp)
			_, err := sp.Exec(ctx, "INSERT INTO wti VALUES (1)")
			return err
		}); err != nil {
			return err
		}
		if inside != inner.String() || setting(ctx, tx) != outer.String() {
			t.Errorf("success: inside %q, after %q; want %s then %s", inside, setting(ctx, tx), inner, outer)
		}
		if err := WithTenantIn(ctx, tx, outer, inner, func(ctx context.Context, sp pgx.Tx) error {
			if _, err := sp.Exec(ctx, "INSERT INTO wti VALUES (2)"); err != nil {
				return err
			}
			return boom
		}); !errors.Is(err, boom) {
			t.Errorf("fn's error: %v, want it verbatim", err)
		}
		if got := setting(ctx, tx); got != outer.String() {
			t.Errorf("after fn's error the context is %q, want %s", got, outer)
		}
		var sum int
		if err := tx.QueryRow(ctx, "SELECT coalesce(sum(v), 0) FROM wti").Scan(&sum); err != nil || sum != 1 {
			t.Errorf("the writes kept: sum %d (%v), want 1 (the released savepoint's row only)", sum, err)
		}
		for _, pair := range [][2]uuid.UUID{{uuid.Nil, inner}, {outer, uuid.Nil}} {
			if err := WithTenantIn(ctx, tx, pair[0], pair[1], func(context.Context, pgx.Tx) error {
				t.Error("fn ran for a nil tenant")
				return nil
			}); err == nil {
				t.Errorf("WithTenantIn(%v, %v) was not refused", pair[0], pair[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTenant: %v", err)
	}
}

// TestWithTenantIn_AFailedRollbackIsReturned (M11 API-11 low round): when fn fails and the
// savepoint will not roll back, WithTenantIn returns both -- fn's error (errors.Is still finds it)
// and the rollback's -- instead of fn's alone, which would let the caller go on over a savepoint
// that may still carry fn's writes and fn's tenant context. A rollback that finds the savepoint
// already closed (pgx.ErrTxClosed) is not a failure and adds nothing. No database: the savepoint is a
// double whose Rollback answers what the case says.
//
// MUTANT (measured): the deferred rollback's error discarded again (`_ = sp.Rollback(...)`) -- the
// "will not roll back" row returns fn's error alone: red.
func TestWithTenantIn_AFailedRollbackIsReturned(t *testing.T) {
	boom, broken := errors.New("boom"), errors.New("rollback refused")
	for _, tc := range []struct {
		name     string
		rollback error
		want     []error
		notWant  error
	}{
		{name: "will not roll back", rollback: broken, want: []error{boom, broken}},
		{name: "already closed", rollback: pgx.ErrTxClosed, want: []error{boom}, notWant: pgx.ErrTxClosed},
		{name: "rolls back", rollback: nil, want: []error{boom}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sp := &savepointDouble{rollback: tc.rollback}
			err := WithTenantIn(context.Background(), outerDouble{sp: sp}, uuid.New(), uuid.New(),
				func(context.Context, pgx.Tx) error { return boom })
			for _, w := range tc.want {
				if !errors.Is(err, w) {
					t.Errorf("err %v does not carry %v", err, w)
				}
			}
			if tc.notWant != nil && errors.Is(err, tc.notWant) {
				t.Errorf("err %v carries %v", err, tc.notWant)
			}
			if sp.rollbacks != 1 {
				t.Errorf("the savepoint was rolled back %d times, want 1", sp.rollbacks)
			}
		})
	}
}

// outerDouble is the caller's transaction: Begin hands out the savepoint double. Every other method
// is the nil embedded interface's -- a call to one panics, so the test proves it is not reached.
type outerDouble struct {
	pgx.Tx
	sp *savepointDouble
}

func (o outerDouble) Begin(context.Context) (pgx.Tx, error) { return o.sp, nil }

// savepointDouble accepts every Exec and answers Rollback with rollback.
type savepointDouble struct {
	pgx.Tx
	rollback  error
	rollbacks int
}

func (s *savepointDouble) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (s *savepointDouble) Rollback(context.Context) error {
	s.rollbacks++
	return s.rollback
}
