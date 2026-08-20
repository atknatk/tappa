package db

import (
	"context"
	"errors"
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ============================================================================
// Migration 00021 part 1 (F2) -- TRUNCATE is a mutation, and until 00021 the
// append-only family did not know it.
//
// 00005 wrote the pattern: REVOKE UPDATE/DELETE from tappa_app (privilege) plus a
// BEFORE UPDATE OR DELETE trigger that binds tappa_owner too. TRUNCATE is neither
// UPDATE nor DELETE, so it went through the trigger untouched -- measured on the
// dev database before 00021: all six append-only tables emptied inside a
// rolled-back transaction as tappa_owner, which is the identity every deploy
// authenticates as (cmd/migrate).
//
// 🔴 EVERY PROBE HERE RUNS INSIDE A TRANSACTION THAT IS ALWAYS ROLLED BACK, AND
// THAT IS NOT TIDINESS -- IT IS THE ONLY THING THAT MAKES A POSITIVE CONTROL
// SURVIVABLE. The point of this file is that TRUNCATE fails; the way anybody
// verifies such a test is by REMOVING the guard and watching the test go red, and
// on that run the statement SUCCEEDS. Without the rollback that run would delete
// every transactions row on the machine -- append-only rows that, by design, no
// product path can restore. tx.Rollback is deferred before the statement is
// issued, so it runs on t.Fatal (Goexit runs defers) as well as on success.
//
// 🔴 LOCK NOTE -- THIS IS BACKLOG T57, AND THE PARAGRAPH THAT USED TO STAND HERE
// WAS WRONG IN A WAY ONLY A FULL SUITE RUN COULD SHOW.
//
// TRUNCATE takes ACCESS EXCLUSIVE on the table AND on everything it cascades to
// -- for `TRUNCATE tenants CASCADE` that is tenants plus the sixteen tables that
// reference it -- while `go test ./...` runs every other package against the same
// database. The old note said lock_timeout would turn that contention into a
// diagnosable 55P03. IT COULD NOT, AND SAYING SO WAS THE WHOLE DEFECT: the wait
// was set to 30s and PostgreSQL runs its deadlock detector at deadlock_timeout,
// one second by default, so a cycle is broken twenty-nine seconds before
// lock_timeout has anything to say. The error that actually reached this test was
// `deadlock detected (SQLSTATE 40P01)`, which is not 55P03 and which
// assertAppendOnlyTrigger reported as "want the append-only trigger ... got
// deadlock" -- i.e. the one mitigation in the file was inert, and the failure it
// failed to prevent read like a section 4.3 breach.
//
// THE CYCLE, measured 2026-08-20 with two psql sessions and no rows written
// (LOCK TABLE / FOR KEY SHARE take the same locks an INSERT does):
//
//	session A   ROW EXCLUSIVE on transactions (the INSERT every other package makes)
//	            then ROW SHARE on tenants     (that INSERT's tenant_id FK check)
//	this probe  ACCESS EXCLUSIVE on tenants   (TRUNCATE locks the named table first)
//	            then ACCESS EXCLUSIVE on transactions
//
// -> each waits for what the other holds. 🔴 AND THE VICTIM IS WHICHEVER SESSION'S
// DETECTOR FIRES FIRST: in that measurement it was SESSION A, so a long wait here
// does not merely risk this test, it can fail an innocent one in another package.
//
// THE FIX IS THE INEQUALITY, NOT THE NUMBER. lock_timeout is now far BELOW
// deadlock_timeout instead of far above it, so the probe hands every lock back
// long before any waiter's detector can run and no cycle is ever observed; a lost
// race answers 55P03 in milliseconds and is retried, because "somebody else holds
// this table" says nothing whatsoever about append-only. Same two sessions, same
// order, only the timeout changed:
//
//	lock_timeout 30s  -> ERROR deadlock detected (40P01); session A was the victim
//	lock_timeout 25ms -> ERROR canceling statement due to lock timeout (55P03)
//	                     here, and session A completed normally
//
// The inequality is enforced at runtime rather than trusted, and by the widest
// probe's real lock count rather than a remembered one: see
// TestAppendOnly_TruncateProbeCannotOutwaitTheDeadlockDetector.
// ============================================================================

// truncateTargets are the six append-only tables and the statement that empties
// each. Two of them are written with CASCADE because a FK child would otherwise
// refuse the statement before any trigger could fire (transaction_reviews under
// transactions; transactions under policy_versions) -- and CASCADE is also the
// interesting shape, see TestAppendOnly_TruncateCascadeIsRefusedToo.
var truncateTargets = []struct {
	name string
	stmt string
}{
	{"transactions", "TRUNCATE transactions CASCADE"},
	{"audit_log", "TRUNCATE audit_log"},
	{"transaction_reviews", "TRUNCATE transaction_reviews"},
	{"billing_periods", "TRUNCATE billing_periods"},
	{"policy_versions", "TRUNCATE policy_versions CASCADE"},
	{"legal_documents", "TRUNCATE legal_documents"},
}

// SQLSTATEs the retry loop classifies on, named rather than matched on message
// text: a message can be reworded, a code cannot.
const (
	sqlstateLockNotAvailable = "55P03" // lock_timeout fired -- somebody else holds the table
	sqlstateDeadlockDetected = "40P01" // the detector broke a cycle and picked a victim
)

// probeLockTimeout is how long ONE lock acquisition inside a TRUNCATE probe may
// wait. It is small on purpose, and the purpose is deadlock_timeout: see the LOCK
// NOTE above. The bound it has to satisfy is
//
//	(relations the widest probe locks) x probeLockTimeout  <  deadlock_timeout
//
// because in the worst case the probe waits that long for each lock in turn while
// holding the ones it already took; if that product exceeded deadlock_timeout a
// waiter could still see the cycle. Measured 2026-08-20: the closure is 17
// relations and deadlock_timeout is 1s, so the worst case is 425ms, the ceiling on
// this constant is 1000/17 = ~58ms, and 25ms goes on holding until the closure
// reaches 40 relations. NOT REMEMBERED, MEASURED, on every run --
// TestAppendOnly_TruncateProbeCannotOutwaitTheDeadlockDetector reads both
// operands from the live database and fails if the inequality stops holding.
const probeLockTimeout = 25 * time.Millisecond

// probeAttempts is the retry budget, and it is dated rather than derived because
// nothing mechanical can pin "generous enough".
//
// MEASURED 2026-08-20, the run that reproduced T57: a background session held ROW
// EXCLUSIVE on transactions for 300ms out of every ~500ms -- a ~60% duty cycle,
// far heavier than the real suite, which lost this race roughly once in several
// full runs. Attempts are spread by probeBackoff so they sample independent
// moments, so even at that 60% occupancy a budget of 40 leaves 0.6^40 ~ 1e-9.
// Exhausting it therefore does not mean "unlucky", it means the database is
// genuinely jammed -- and that is reported, not skipped.
const probeAttempts = 40

// isLockContention answers whether err is the database saying "somebody else is
// holding this table", which is the one class of failure that says NOTHING about
// append-only and may therefore be retried.
//
// 🔴 THE GUARD'S OWN SQLSTATE MUST NEVER LAND HERE. If 23001 were classified as
// contention the loop would retry a working refusal forty times and then report a
// jammed database -- a red test, so not silent, but pointing at the wrong thing,
// which is exactly the misdiagnosis T57 exists to remove. TestIsLockContention
// pins the membership of this set.
func isLockContention(err error) bool {
	pg := asPgErr(err)
	if pg == nil {
		return false
	}
	return pg.Code == sqlstateLockNotAvailable || pg.Code == sqlstateDeadlockDetected
}

// probeBackoff spreads retries out instead of hot-spinning inside one holder's
// window. The JITTER is the load-bearing half, not the ramp: a holder that takes
// and releases on a fixed cycle (the 300ms-in-500ms amplifier above) would be
// sampled at the same phase every time by evenly spaced retries.
func probeBackoff(attempt int) time.Duration {
	d := probeLockTimeout * time.Duration(attempt)
	if d > 200*time.Millisecond {
		d = 200 * time.Millisecond
	}
	return d/2 + time.Duration(rand.Int63n(int64(d/2)+1))
}

// tryTruncate issues stmt inside a transaction that is ALWAYS rolled back and
// returns the error the statement produced (nil means it succeeded, which is the
// regression this file exists to catch). A lost lock race is retried rather than
// returned, so the caller's assertions only ever see an answer about append-only.
func tryTruncate(t *testing.T, d *DB, stmt string) error {
	t.Helper()
	var err error
	for attempt := 1; attempt <= probeAttempts; attempt++ {
		err = truncateOnce(t, d, stmt)
		if !isLockContention(err) {
			if attempt > 1 {
				t.Logf("%s: answered on attempt %d of %d (earlier attempts lost the lock race, which is not a verdict on the guard)",
					stmt, attempt, probeAttempts)
			}
			return err
		}
		time.Sleep(probeBackoff(attempt))
	}
	// 🔴 FATAL, NOT SKIP, AND THAT IS A DELIBERATE READING OF THIS REPOSITORY'S OWN
	// HISTORY. `make test`'s require-db-env gate exists because silently skipped
	// database tests let a green run hide four red-line proofs; adding a skip to the
	// section 4.3 TRUNCATE guard would rebuild that hole on the very test that
	// guards the red line. The message names the cause instead, so the failure
	// cannot be read as "append-only broke".
	t.Fatalf("%s: %d attempts in a row could not take the locks (last: %v). This is LOCK CONTENTION "+
		"with concurrent packages -- the statement never reached the append-only trigger, so this is "+
		"NOT a section 4.3 regression and NOT a verdict on the guard. Something is holding these tables "+
		"far longer than the suite does.", stmt, probeAttempts, err)
	return nil // unreachable: t.Fatalf calls runtime.Goexit
}

// truncateOnce is one attempt: begin, arm the timeout, issue the statement, roll
// back whatever happened.
func truncateOnce(t *testing.T, d *DB, stmt string) error {
	t.Helper()
	ctx := context.Background()
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	// Deferred BEFORE the statement runs: if the guard is gone and TRUNCATE
	// succeeds, this is what saves the database.
	defer func() {
		if e := tx.Rollback(ctx); e != nil && e != pgx.ErrTxClosed {
			t.Errorf("rollback after %q: %v", stmt, e)
		}
	}()
	if _, e := tx.Exec(ctx, "SET LOCAL lock_timeout = "+quoteMillis(probeLockTimeout)); e != nil {
		t.Fatalf("set lock_timeout: %v", e)
	}
	_, err = tx.Exec(ctx, stmt)
	return err
}

// quoteMillis renders d as a SQL string literal Postgres reads as milliseconds.
// SET does not take a bind parameter, so the value is formatted; it comes from a
// constant in this file and is integer milliseconds, never user input.
func quoteMillis(d time.Duration) string {
	return "'" + strconv.FormatInt(d.Milliseconds(), 10) + "ms'"
}

// countRows reads a table's size through the owner pool (which bypasses RLS, so
// the number is the whole table rather than one tenant's slice).
func countRows(t *testing.T, d *DB, table string) int64 {
	t.Helper()
	var n int64
	if err := d.pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// TestAppendOnly_TruncateIsRefusedEvenForTheOwner is the F2 proof: the SUPERUSER
// that runs every migration cannot empty an append-only table.
//
// The role is proven at runtime (assertOwnerRole) for the reason case 5 in
// rls_test.go gives: naming a pool "owner" is not evidence, and a probe run as
// tappa_app would prove only that a privilege is missing -- the interesting claim
// is about the identity that HAS the privilege.
func TestAppendOnly_TruncateIsRefusedEvenForTheOwner(t *testing.T) {
	owner := ownerDB(t)
	assertOwnerRole(t, owner)

	for _, tc := range truncateTargets {
		t.Run(tc.name, func(t *testing.T) {
			before := countRows(t, owner, tc.name)

			err := tryTruncate(t, owner, tc.stmt)
			assertAppendOnlyTrigger(t, "owner "+tc.stmt, err)
			if pg := asPgErr(err); pg == nil || pg.Code != sqlstateRestrictViolation {
				t.Fatalf("%s: SQLSTATE = %v, want %s (the shared tappa_forbid_mutation RAISE)",
					tc.stmt, pg, sqlstateRestrictViolation)
			}

			// 🔴 THE MESSAGE MUST NAME THIS TABLE, AND A POSITIVE CONTROL IS WHY.
			// Dropping transactions_no_truncate left this subtest GREEN: the
			// statement carries CASCADE, so transaction_reviews' guard refused it
			// instead and the assertion above -- "some append-only table said no" --
			// was satisfied by the wrong table. Naming it makes each of the six
			// triggers individually load-bearing.
			if pg := asPgErr(err); !strings.Contains(pg.Message, "table "+tc.name+":") {
				t.Fatalf("%s was refused, but by %q -- the guard on %s itself is missing "+
					"and a neighbour's guard is covering for it", tc.stmt, pg.Message, tc.name)
			}

			// The rollback is belt; this is braces. A guard that raised AFTER the
			// rows were gone would still leave the table empty in this session.
			//
			// 🔴 THE ASSERTION IS ">=", NOT "==", AND THE FIRST DRAFT GOT THIS WRONG
			// IN A WAY THAT ONLY A FULL SUITE RUN COULD SHOW: `go test ./...` runs
			// other packages against the same database, and they INSERT audit rows
			// while this subtest runs. The first version demanded equality and went
			// red with "193 076 -> 193 077" -- a concurrent insert, not a truncation.
			// These tables are append-only, so their count can only GROW; a TRUNCATE
			// is the one thing that makes it FALL, which is exactly what this asks.
			if after := countRows(t, owner, tc.name); after < before {
				t.Fatalf("%s: row count FELL %d -> %d -- an append-only table lost rows",
					tc.stmt, before, after)
			}
		})
	}
}

// TestAppendOnly_TruncateCascadeIsRefusedToo is the finding the F2 measurement
// turned up and the reason six triggers are enough.
//
// A statement does not have to NAME an append-only table to empty it: TRUNCATE
// follows foreign keys when told to CASCADE, and this schema has two chains that
// end in section 4.3's own table --
//
//	policy_versions -> transactions -> transaction_reviews
//	tenants         -> (16 tables, among them transactions and audit_log)
//
// Measured before 00021 by creating the guard on the CHILD only, inside a rolled
// back transaction: the child's trigger fires on the cascaded truncation. So the
// guard on `transactions` is what refuses `TRUNCATE tenants CASCADE`, a statement
// that mentions neither transactions nor audit_log and would otherwise erase a
// whole business.
func TestAppendOnly_TruncateCascadeIsRefusedToo(t *testing.T) {
	owner := ownerDB(t)
	assertOwnerRole(t, owner)

	for _, tc := range []struct {
		name string
		stmt string
		// guardedBy is the append-only table the cascade reaches; the error must
		// name it, otherwise the statement was refused for some other reason (a
		// missing privilege, a FK) and this test would pass vacuously.
		guardedBy string
	}{
		{"policy_versions_cascade", "TRUNCATE policy_versions CASCADE", "policy_versions"},
		{"tenants_cascade", "TRUNCATE tenants CASCADE", "transactions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			txnBefore := countRows(t, owner, "transactions")
			auditBefore := countRows(t, owner, "audit_log")

			err := tryTruncate(t, owner, tc.stmt)
			assertAppendOnlyTrigger(t, "owner "+tc.stmt, err)
			if pg := asPgErr(err); pg == nil || !strings.Contains(pg.Message, tc.guardedBy) {
				t.Fatalf("%s: error %v does not name %s -- the refusal must come from the "+
					"append-only guard, not from something else", tc.stmt, err, tc.guardedBy)
			}

			// ">=" for the reason spelled out in the previous test: concurrent
			// packages append to both tables while this runs, so equality is a
			// flake and a FALLING count is the real regression.
			if n := countRows(t, owner, "transactions"); n < txnBefore {
				t.Fatalf("transactions FELL %d -> %d after %s", txnBefore, n, tc.stmt)
			}
			if n := countRows(t, owner, "audit_log"); n < auditBefore {
				t.Fatalf("audit_log FELL %d -> %d after %s", auditBefore, n, tc.stmt)
			}
		})
	}
}

// TestAppendOnly_AppHasNoTruncatePrivilege is the privilege half of the belt, and
// it is a TEST rather than a REVOKE in the migration on purpose.
//
// Measured: tappa_app holds no TRUNCATE on any of the six, because db-init grants
// SELECT+INSERT by default and TRUNCATE is not one of the four DML privileges a
// pg_dump restore re-widens to (scripts/db-init/01-roles.sql measures that
// widening). A `REVOKE TRUNCATE` in 00021 would therefore be a no-op that READS
// like a working guard; an assertion cannot be a no-op -- it fails the day
// somebody grants it.
func TestAppendOnly_AppHasNoTruncatePrivilege(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)

	for _, tc := range truncateTargets {
		t.Run(tc.name, func(t *testing.T) {
			var granted bool
			if err := app.pool.QueryRow(context.Background(),
				`SELECT has_table_privilege(current_user, $1, 'TRUNCATE')`, tc.name,
			).Scan(&granted); err != nil {
				t.Fatalf("has_table_privilege %s: %v", tc.name, err)
			}
			if granted {
				t.Fatalf("tappa_app holds TRUNCATE on %s -- the HTTP-facing role must not be "+
					"able to empty an append-only table (section 4.3)", tc.name)
			}
		})
	}
}

// TestAppendOnly_TruncateProbeCannotOutwaitTheDeadlockDetector is the gate under
// probeLockTimeout, and it exists because the number it guards was WRONG BY A
// FACTOR OF THIRTY in the shipped version of this file and nothing noticed.
//
// The old value was 30s against a 1s deadlock_timeout, i.e. the probe was
// guaranteed to sit in a cycle long enough for somebody's detector to fire, and
// the comment beside it asserted the opposite. A comment cannot hold an
// inequality; this reads BOTH operands from the live database and fails when it
// stops holding:
//
//	relations the widest probe locks  x  probeLockTimeout  <  deadlock_timeout
//
// The left operand is the transitive closure of "tables that reference tenants",
// which is what TRUNCATE ... CASCADE actually locks -- so ADDING A TENANT-SCOPED
// TABLE (CLAUDE.md section 6 makes tenant_id mandatory, so every new table joins
// this closure) walks the product toward the ceiling and this test says so before
// the flake comes back. Lowering the server's deadlock_timeout fails it too.
func TestAppendOnly_TruncateProbeCannotOutwaitTheDeadlockDetector(t *testing.T) {
	owner := ownerDB(t)
	ctx := context.Background()

	var deadlockMillis int64
	var unit string
	if err := owner.pool.QueryRow(ctx,
		`SELECT setting::bigint, unit FROM pg_settings WHERE name = 'deadlock_timeout'`,
	).Scan(&deadlockMillis, &unit); err != nil {
		t.Fatalf("read deadlock_timeout: %v", err)
	}
	// pg_settings reports this one in ms. Asserted rather than assumed: if a future
	// Postgres reported seconds, the comparison below would be off by 1000 and would
	// PASS, which is the direction that hurts.
	if unit != "ms" {
		t.Fatalf("deadlock_timeout is reported in %q, not ms -- the arithmetic below assumes ms", unit)
	}
	deadlock := time.Duration(deadlockMillis) * time.Millisecond

	// The closure TRUNCATE tenants CASCADE walks. UNION (not UNION ALL) so a
	// self-referencing FK terminates.
	var relations int64
	if err := owner.pool.QueryRow(ctx, `
		WITH RECURSIVE reached(rel) AS (
			SELECT 'tenants'::regclass
		  UNION
			SELECT c.conrelid
			FROM pg_constraint c JOIN reached r ON c.confrelid = r.rel
			WHERE c.contype = 'f'
		)
		SELECT count(*) FROM reached`,
	).Scan(&relations); err != nil {
		t.Fatalf("count cascade closure: %v", err)
	}
	if relations < 2 {
		t.Fatalf("the cascade closure of tenants is %d relation(s) -- the query has gone blind, "+
			"and a blind left operand makes this inequality pass for free", relations)
	}

	worst := time.Duration(relations) * probeLockTimeout
	t.Logf("widest probe locks %d relations x %v = %v worst-case hold, deadlock_timeout %v",
		relations, probeLockTimeout, worst, deadlock)
	if worst >= deadlock {
		t.Fatalf("probeLockTimeout %v x %d relations = %v, which is NOT below deadlock_timeout %v. "+
			"A waiter can see the cycle again and backlog T57 comes back -- lower probeLockTimeout "+
			"(the ceiling is %v) rather than raising the wait.",
			probeLockTimeout, relations, worst, deadlock, deadlock/time.Duration(relations))
	}
}

// TestIsLockContention pins WHICH failures may be retried, because that set is
// the whole safety argument for the retry loop: everything outside it reaches the
// caller's assertions unchanged.
//
// The case that matters is 23001. The append-only guard raises with it, and if it
// were ever classified as contention a working refusal would be retried away.
func TestIsLockContention(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"lock_timeout fired", &pgconn.PgError{Code: sqlstateLockNotAvailable, Message: "canceling statement due to lock timeout"}, true},
		{"detector picked us", &pgconn.PgError{Code: sqlstateDeadlockDetected, Message: "deadlock detected"}, true},
		{"the guard refusing -- MUST NOT be retried away", &pgconn.PgError{Code: sqlstateRestrictViolation, Message: "append-only table transactions: TRUNCATE is forbidden"}, false},
		{"permission denied", &pgconn.PgError{Code: sqlstateInsufficientPrivi, Message: "permission denied for table audit_log"}, false},
		{"TRUNCATE succeeded -- the regression this file catches", nil, false},
		{"not a Postgres error at all", errors.New("connection reset"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isLockContention(tc.err); got != tc.want {
				t.Errorf("isLockContention(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
