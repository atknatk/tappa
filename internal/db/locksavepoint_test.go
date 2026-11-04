package db

// locksavepoint_test.go -- T191: the test lock helpers against real Postgres. lockNowait and
// lockInTurn end a failed try without leaving the transaction inside a subtransaction, and
// bfUpgradeGuard sees an upgrade of bfTx's table lock on every path a test has.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// lockHoldAccessShare opens a second session that holds ACCESS SHARE on table in an open transaction
// -- what a reader looks like to a NOWAIT try -- and returns its release (safe to call twice).
func lockHoldAccessShare(t *testing.T, table string) (release func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	holder, err := pgx.Connect(ctx, opOwnerDSN(t))
	if err != nil {
		t.Fatalf("connect the lock holder: %v", err)
	}
	t.Cleanup(func() {
		if err := holder.Close(context.Background()); err != nil {
			t.Logf("close the lock holder: %v", err)
		}
	})
	for _, q := range []string{`BEGIN`, `LOCK TABLE public.` + table + ` IN ACCESS SHARE MODE`} {
		if _, err := holder.Exec(ctx, q); err != nil {
			t.Fatalf("lock holder: %s: %v", q, err)
		}
	}
	done := false
	return func() {
		t.Helper()
		if done {
			return
		}
		done = true
		if _, err := holder.Exec(ctx, `ROLLBACK`); err != nil {
			t.Fatalf("release the lock holder: %v", err)
		}
	}
}

// lockMissCase: a failed try of lockNowait or lockInTurn must not leave the test inside a subtransaction.
// pgx's nested Rollback sends ROLLBACK TO SAVEPOINT and no RELEASE (pgx tx.go,
// dbSimulatedNestedTx.Rollback), so with it every later row carried the savepoint's xid and a "born in
// this transaction" check (xmin = pg_current_xact_id()) answered false. Each case forces its miss with
// a second session, so it cannot pass vacuously, then reads both things that depend on the level: the
// row's xmin, and identity_create_for_signup's own answer on such a row. One test per case: the tables
// lock is taken once per test tree (cmd/opadmin's TestTablesLock_IsTakenOncePerTestTree).
func lockMissCase(t *testing.T, miss func(ctx context.Context, tx pgx.Tx)) {
	t.Helper()
	ctx, tx := bfTxLocking(t, func(ctx context.Context, tx pgx.Tx) {
		release := lockHoldAccessShare(t, "admin_users")
		miss(ctx, tx)
		miss(ctx, tx) // two misses: pgx's savepoints would nest
		release()
		lockForDowns(t, ctx, tx, []string{"admin_users"})
	})
	w := isNewWorld(t, ctx, tx, "", "")
	var topLevel bool
	if err := tx.QueryRow(ctx, `SELECT xmin = pg_catalog.pg_current_xact_id()::xid FROM admin_users WHERE id = $1`, w.admin).Scan(&topLevel); err != nil {
		t.Fatalf("read the row's xmin: %v", err)
	}
	if !topLevel {
		t.Error("a row written after the miss carries a subtransaction's xid: the miss left its savepoint open")
	}
	if !isCall(t, ctx, tx, w.tenant, w.tenant, w.admin) {
		t.Error("identity_create_for_signup answered false for a row and a business this transaction wrote")
	}
	if ident, via := isLinked(t, ctx, tx, w.admin); ident == uuid.Nil || via != "signup" {
		t.Errorf("the row is not linked: identity %s via %q", ident, via)
	}
}

// lockWantBusy asks lockInTurn for tables, whose last is admin_users, and wants its wait to run out there.
func lockWantBusy(t *testing.T, ctx context.Context, tx pgx.Tx, tables ...string) {
	t.Helper()
	const held = "public.admin_users"
	if busy, ok := lockInTurn(t, ctx, tx, append(tables, held)); ok || busy != held {
		t.Fatalf("PREMISE: lockInTurn answered ok=%v busy=%q, want a timeout on the held table", ok, busy)
	}
}

func TestLockHelpers_ANowaitMissLeavesTheTopLevel(t *testing.T) {
	lockMissCase(t, func(ctx context.Context, tx pgx.Tx) {
		if lockNowait(t, ctx, tx, []string{"public.admin_users"}) {
			t.Fatal("PREMISE: lockNowait took the table a second session holds")
		}
	})
}

func TestLockHelpers_AnInTurnMissLeavesTheTopLevel(t *testing.T) {
	lockMissCase(t, func(ctx context.Context, tx pgx.Tx) { lockWantBusy(t, ctx, tx) })
}

// The first table is taken in the savepoint and goes back with it.
func TestLockHelpers_AnInTurnMissOnTheSecondTableLeavesTheTopLevel(t *testing.T) {
	lockMissCase(t, func(ctx context.Context, tx pgx.Tx) { lockWantBusy(t, ctx, tx, "public.tenant_branding") })
}

// bfGuardCase drives the guard through one way a test has to ask for ACCESS EXCLUSIVE over bfTx's
// EXCLUSIVE, or through what a bfTx test may do; the TestBfUpgradeGuard_ tests below are its cases. The
// paths are the ones that escaped the guard's first form (T191): PgConn().Exec (opRunSection, afRun), a
// section that goes on to fail, a savepoint rolled back, a Batch, and one more the statement's own error
// cannot show -- a DO block that catches lock_not_available. lock_timeout is 1 s, twenty polls. One test
// per case, like lockMissCase.
//
// ON goose_db_version, NOT admin_users: the request waits, and a request for ACCESS EXCLUSIVE on
// admin_users behind bfTx's EXCLUSIVE is the cycle this guard exists to prevent -- a session of another
// package that read admin_users and asks for a row lock would be made a 40P01 victim by this test.
// Nothing but plain reads ever touches goose_db_version while tests run.
func bfGuardCase(t *testing.T, upgrades bool, run func(ctx context.Context, tx pgx.Tx)) {
	t.Helper()
	guard := newBfUpgradeGuard(t, opOwnerDSN(t), bfGuardCaseTable)
	guard.report = t.Logf // the finding is this test's assertion, not its failure
	ctx, tx := bfTxOpen(t, guard, func(ctx context.Context, tx pgx.Tx) {
		for _, q := range []string{`LOCK TABLE public.` + bfGuardCaseTable + ` IN EXCLUSIVE MODE`, `SET LOCAL lock_timeout = '1s'`} {
			if _, err := tx.Exec(ctx, q); err != nil {
				t.Fatalf("%s: %v", q, err)
			}
		}
	})
	time.Sleep(2 * bfGuardPoll) // the poller has looked once before the request: what it names is not its first read
	run(ctx, tx)
	time.Sleep(2 * bfGuardPoll) // the negative cases: the poller had its look after the statement
	got := guard.seen()
	switch {
	case upgrades && got == nil:
		t.Error("the guard did not see the request")
	case upgrades && !strings.Contains(*got, "ACCESS EXCLUSIVE MODE"):
		t.Errorf("the guard named another statement: %s", *got)
	case !upgrades && got != nil:
		t.Errorf("the guard reported a test that did not upgrade: %s", *got)
	}
}

const (
	bfGuardCaseTable = "goose_db_version"
	bfGuardCaseAsk   = `LOCK TABLE public.` + bfGuardCaseTable + ` IN ACCESS EXCLUSIVE MODE`
)

func bfWant55P03(t *testing.T, err error) {
	t.Helper()
	if code, _ := opCode(err); code != "55P03" {
		t.Fatalf("the request answered %v, want 55P03", err)
	}
}

func TestBfUpgradeGuard_SeesExec(t *testing.T) {
	bfGuardCase(t, true, func(ctx context.Context, tx pgx.Tx) {
		_, err := tx.Exec(ctx, bfGuardCaseAsk)
		bfWant55P03(t, err)
	})
}

func TestBfUpgradeGuard_SeesQuery(t *testing.T) {
	bfGuardCase(t, true, func(ctx context.Context, tx pgx.Tx) {
		rows, err := tx.Query(ctx, bfGuardCaseAsk)
		if err == nil {
			rows.Close()
			err = rows.Err()
		}
		bfWant55P03(t, err)
	})
}

func TestBfUpgradeGuard_SeesABatch(t *testing.T) {
	bfGuardCase(t, true, func(ctx context.Context, tx pgx.Tx) {
		b := &pgx.Batch{}
		b.Queue(bfGuardCaseAsk)
		bfWant55P03(t, tx.SendBatch(ctx, b).Close())
	})
}

// opRunSection's path, in a savepoint that is rolled back.
func TestBfUpgradeGuard_SeesPgConnExecInASavepoint(t *testing.T) {
	bfGuardCase(t, true, func(ctx context.Context, tx pgx.Tx) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			_, err := sp.Conn().PgConn().Exec(ctx, bfGuardCaseAsk).ReadAll()
			bfWant55P03(t, err)
		})
	})
}

func TestBfUpgradeGuard_SeesAfRunOnASectionThatFails(t *testing.T) {
	bfGuardCase(t, true, func(ctx context.Context, tx pgx.Tx) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			bfWant55P03(t, afRun(t, ctx, sp, bfGuardCaseAsk+`; SELECT 1 / 0`))
		})
	})
}

func TestBfUpgradeGuard_SeesADoBlockThatSwallowsTheRefusal(t *testing.T) {
	bfGuardCase(t, true, func(ctx context.Context, tx pgx.Tx) {
		if _, err := tx.Exec(ctx, `DO $$ BEGIN `+bfGuardCaseAsk+`; EXCEPTION WHEN lock_not_available THEN NULL; END $$`); err != nil {
			t.Fatalf("the DO block: %v", err)
		}
	})
}

func TestBfUpgradeGuard_LeavesWhatABfTxTestMayDo(t *testing.T) {
	bfGuardCase(t, false, func(ctx context.Context, tx pgx.Tx) {
		for _, q := range []string{
			`SELECT count(*) FROM public.` + bfGuardCaseTable,
			`SELECT version_id FROM public.` + bfGuardCaseTable + ` ORDER BY id DESC LIMIT 1 FOR UPDATE`,
			`LOCK TABLE public.` + bfGuardCaseTable + ` IN EXCLUSIVE MODE`,
		} {
			if _, err := tx.Exec(ctx, q); err != nil {
				t.Fatalf("%s: %v", q, err)
			}
		}
	})
}

// STILL BLIND, pinned: the guard's comment names this. A NOWAIT request never waits, so pg_locks never
// shows it; it is never granted either, so it deadlocks nothing.
func TestBfUpgradeGuard_DoesNotSeeNowait(t *testing.T) {
	bfGuardCase(t, false, func(ctx context.Context, tx pgx.Tx) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			_, err := sp.Exec(ctx, bfGuardCaseAsk+` NOWAIT`)
			bfWant55P03(t, err)
		})
	})
}
