package db

// locksavepoint_test.go -- T191: the test lock helpers against real Postgres. lockNowait and
// lockInTurn end a failed try without leaving the transaction inside a subtransaction, and
// bfUpgradeGuard sees an upgrade of bfTx's table lock on every path a test has.

import (
	"context"
	"fmt"
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
	lockWantTopLevel(t, ctx, tx)
}

// lockWantTopLevel reads both things that depend on tx being at its top level: a new row's xmin, and
// identity_create_for_signup's answer on such a row.
func lockWantTopLevel(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
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

// TestAiSavepoint_LeavesTheTopLevel (T194): whatever shape a case leaves aiSavepoint in -- rows written, a
// nested call, a refusal through opAs (pgx's nested Rollback, whose savepoint stays open), a statement that
// failed -- what it did is gone and tx is back at its top level.
func TestAiSavepoint_LeavesTheTopLevel(t *testing.T) {
	ctx, tx := opTx(t)
	exists := func(q pgx.Tx, id uuid.UUID) bool {
		return opInt(t, ctx, q, `SELECT count(*) FROM tenants WHERE id = $1`, id) == 1
	}
	var outer, inner uuid.UUID
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		outer = aiTenant(t, ctx, sp)
		aiSavepoint(t, ctx, sp, func(nested pgx.Tx) { inner = aiTenant(t, ctx, nested) })
		if exists(sp, inner) || !exists(sp, outer) {
			t.Fatalf("after the nested call: its row exists = %v, the outer's = %v; want false, true", exists(sp, inner), exists(sp, outer))
		}
		if err := opExecAs(t, ctx, sp, "tappa_app", `SELECT 1 / 0`); err == nil {
			t.Fatal("PREMISE: the refused statement answered")
		}
	})
	if exists(tx, outer) {
		t.Fatal("a row written in the savepoint outlived it")
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		if _, err := sp.Exec(ctx, `SELECT 1 / 0`); err == nil {
			t.Fatal("PREMISE: the failing statement answered")
		}
	})
	lockWantTopLevel(t, ctx, tx)
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

// The first table is taken in the savepoint and goes back with it. goose_db_version: lockInTurn waits only
// 200 ms for it, so it must be a table no other package keeps open (bfGuardCase's comment).
func TestLockHelpers_AnInTurnMissOnTheSecondTableLeavesTheTopLevel(t *testing.T) {
	lockMissCase(t, func(ctx context.Context, tx pgx.Tx) { lockWantBusy(t, ctx, tx, "public."+bfGuardCaseTable) })
}

// lockInTurn's other way out: every table taken. lockMissCase cannot see it, because it ends through
// lockForDowns' NOWAIT path; here lockInTurn runs first and nothing else leaves a savepoint behind.
func TestLockHelpers_AnInTurnHitLeavesTheTopLevel(t *testing.T) {
	ctx, tx := bfTxLocking(t, func(ctx context.Context, tx pgx.Tx) {
		if busy, ok := lockInTurn(t, ctx, tx, []string{"public." + bfGuardCaseTable}); !ok {
			t.Fatalf("PREMISE: lockInTurn did not take a quiet table: busy=%q", busy)
		}
		lockForDowns(t, ctx, tx, []string{"admin_users"})
	})
	lockWantTopLevel(t, ctx, tx)
}

// bfGuardCase drives the guard through one way a test has to ask for ACCESS EXCLUSIVE over bfTx's
// EXCLUSIVE, or through what a bfTx test may do; the TestBfUpgradeGuard_ tests below are its cases. The
// paths are the ones that escaped the guard's first form (T191): PgConn().Exec (opRunSection, afRun), a
// section that goes on to fail, a savepoint rolled back, a Batch, and one more the statement's own error
// cannot show -- a DO block that catches lock_not_available. lock_timeout is 1 s, twenty of the poller's
// pauses; the read's round trip comes on top of each (bfGuardPoll). One test per case, like lockMissCase.
//
// ON goose_db_version, NOT admin_users: the request waits, and a request for ACCESS EXCLUSIVE on
// admin_users behind bfTx's EXCLUSIVE is the cycle this guard exists to prevent -- a session of another
// package that read admin_users and asks for a row lock would be made a 40P01 victim by this test.
// Nothing but plain reads ever touches goose_db_version while tests run.
func bfGuardCase(t *testing.T, upgrades bool, run func(ctx context.Context, tx pgx.Tx)) {
	t.Helper()
	guard, reports := bfGuardCaseGuard(t)
	ctx, tx := bfGuardCaseTx(t, guard)
	time.Sleep(2 * bfGuardPoll) // the poller has looked once before the request: what it names is not its first read
	run(ctx, tx)
	time.Sleep(2 * bfGuardPoll) // the negative cases: the poller had its look after the statement
	guard.finish()
	switch got := *reports; {
	case upgrades && len(got) != 1:
		t.Errorf("the guard reported %d times, want once: %q", len(got), got)
	case upgrades && !strings.Contains(got[0], bfGuardCaseAsk):
		t.Errorf("the guard named another statement: %s", got[0])
	case !upgrades && len(got) != 0:
		t.Errorf("the guard reported a test that did not upgrade: %q", got)
	}
}

// bfGuardCaseGuard builds the guard with its report captured: the finding is the case's assertion, not
// its failure.
func bfGuardCaseGuard(t *testing.T) (*bfUpgradeGuard, *[]string) {
	t.Helper()
	guard := newBfUpgradeGuard(t, opOwnerDSN(t), bfGuardCaseTable)
	reports := &[]string{}
	guard.report = func(format string, args ...any) { *reports = append(*reports, fmt.Sprintf(format, args...)) }
	return guard, reports
}

func bfGuardCaseTx(t *testing.T, guard *bfUpgradeGuard) (context.Context, pgx.Tx) {
	t.Helper()
	return bfTxOpen(t, guard, func(ctx context.Context, tx pgx.Tx) {
		for _, q := range []string{`LOCK TABLE public.` + bfGuardCaseTable + ` IN EXCLUSIVE MODE`, `SET LOCAL lock_timeout = '1s'`} {
			if _, err := tx.Exec(ctx, q); err != nil {
				t.Fatalf("%s: %v", q, err)
			}
		}
	})
}

// The guard's own cleanup is what fails a test, so this case leaves finish to it: its assertion is a
// cleanup registered first, which runs after the guard's (LIFO). With finish's report taken out, or its
// t.Cleanup, this is red; bfGuardCase's direct call would not notice the second.
func TestBfUpgradeGuard_ItsCleanupReports(t *testing.T) {
	var reports *[]string
	t.Cleanup(func() {
		if reports == nil || len(*reports) != 1 || !strings.Contains((*reports)[0], bfGuardCaseAsk) {
			t.Errorf("the cleanup did not report the request once, by name: %v", reports)
		}
	})
	guard, r := bfGuardCaseGuard(t)
	reports = r
	ctx, tx := bfGuardCaseTx(t, guard)
	_, err := tx.Exec(ctx, bfGuardCaseAsk)
	bfWant55P03(t, err)
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

// lockSnapshotCase (T255): lockForDowns must leave opTx's REPEATABLE READ transaction WITHOUT a
// snapshot, so the caller's first SELECT sees every row committed while it waited for the locks (a
// row newer than an earlier snapshot is opAtVersion's 40001 -- lockForDowns says how it was
// measured). The transaction's snapshot shows as its backend_xmin in pg_stat_activity, read from a
// second session: NULL after lockForDowns, set after one SELECT (the premise that the reading sees a
// snapshot at all). With inTurn, a reader holds the table until lockInTurn's blocking LOCK is seen
// WAITING on it -- the transaction's pid blocked by the holder's (pg_blocking_pids), which a NOWAIT try
// never shows -- so the case ends only through lockInTurn, however long the 40 NOWAIT rounds take. A
// first version released on a 1.5 s timer instead: under -race at load ~44 the NOWAIT rounds outlasted
// it once in six and the settings mutant below stayed green (third-eye audit, measured).
//
// MUTANTS (measured, T255): lockForDowns' existence check back to SELECT to_regclass(...) -> red in
// both tests; lockInTurn's settings read back to SELECT ... FROM pg_settings -> red in the in-turn
// test only.
func lockSnapshotCase(t *testing.T, inTurn bool) {
	t.Helper()
	ctx, tx := opTx(t)
	watcher, err := pgx.Connect(ctx, opOwnerDSN(t))
	if err != nil {
		t.Fatalf("connect the watcher: %v", err)
	}
	t.Cleanup(func() {
		if err := watcher.Close(context.Background()); err != nil {
			t.Logf("close the watcher: %v", err)
		}
	})
	pid := tx.Conn().PgConn().PID()
	hasSnapshot := func() bool {
		t.Helper()
		return snapshotSeenBy(t, ctx, watcher, pid)
	}
	if hasSnapshot() {
		t.Fatal("PREMISE: opTx's transaction has a snapshot before lockForDowns")
	}
	// The holder is released by a goroutine of its own, watching on a connection of its own, and its
	// answer is received HERE, before the test can end: t.Fatalf belongs to the test's goroutine, and a
	// release racing its connection's cleanup was CI 42939616527's data race.
	type outcome struct {
		waited bool
		err    error
	}
	released := make(chan outcome, 1)
	if inTurn {
		holder, err := pgx.Connect(ctx, opOwnerDSN(t))
		if err != nil {
			t.Fatalf("connect the lock holder: %v", err)
		}
		t.Cleanup(func() {
			if err := holder.Close(context.Background()); err != nil {
				t.Logf("close the lock holder: %v", err)
			}
		})
		spy, err := pgx.Connect(ctx, opOwnerDSN(t))
		if err != nil {
			t.Fatalf("connect the lock spy: %v", err)
		}
		t.Cleanup(func() {
			if err := spy.Close(context.Background()); err != nil {
				t.Logf("close the lock spy: %v", err)
			}
		})
		for _, q := range []string{`BEGIN`, `LOCK TABLE public.` + bfGuardCaseTable + ` IN ACCESS SHARE MODE`} {
			if _, err := holder.Exec(ctx, q); err != nil {
				t.Fatalf("lock holder: %s: %v", q, err)
			}
		}
		holderPid := holder.PgConn().PID()
		go func() {
			var o outcome
			// lockForDowns gives up after 90 s; this gives the holder back before that either way.
			for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
				var blocked bool
				if o.err = spy.QueryRow(context.Background(), `SELECT $1::int = ANY (pg_catalog.pg_blocking_pids($2))`,
					int64(holderPid), int64(pid)).Scan(&blocked); o.err != nil {
					break
				}
				if blocked {
					o.waited = true
					break
				}
			}
			if _, err := holder.Exec(context.Background(), `ROLLBACK`); err != nil && o.err == nil {
				o.err = err
			}
			released <- o
		}()
	} else {
		released <- outcome{}
	}
	lockForDowns(t, ctx, tx, []string{"zz_no_such_table", bfGuardCaseTable})
	o := <-released
	if o.err != nil {
		t.Fatalf("watch for lockInTurn's wait, or release the holder: %v", o.err)
	}
	if inTurn && !o.waited {
		t.Fatal("CONTROL: lockInTurn never waited on the held table: the in-turn path did not run")
	}
	if hasSnapshot() {
		t.Error("lockForDowns left the transaction with a snapshot: rows committed while it waited are invisible to the caller")
	}
	if _, err := tx.Exec(ctx, `SELECT 1`); err != nil {
		t.Fatal(err)
	}
	if !hasSnapshot() {
		t.Error("PREMISE: a SELECT did not give the transaction a snapshot the watcher can see")
	}
}

func TestLockHelpers_TheLocksComeBeforeTheSnapshot(t *testing.T) { lockSnapshotCase(t, false) }

func TestLockHelpers_TheLocksComeBeforeTheSnapshotInTurnToo(t *testing.T) { lockSnapshotCase(t, true) }
