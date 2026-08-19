package db

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
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
// LOCK NOTE: TRUNCATE takes ACCESS EXCLUSIVE on the table AND on everything it
// cascades to, and `go test ./...` runs other packages against the same database.
// lock_timeout turns a pathological wait into a diagnosable error instead of a
// hung suite; if a run ever fails with 55P03 the cause is contention, not a
// missing guard, and the message below says so.
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

// tryTruncate issues stmt inside a transaction that is ALWAYS rolled back and
// returns the error the statement produced (nil means it succeeded, which is the
// regression this file exists to catch).
func tryTruncate(t *testing.T, d *DB, stmt string) error {
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
	if _, e := tx.Exec(ctx, "SET LOCAL lock_timeout = '30s'"); e != nil {
		t.Fatalf("set lock_timeout: %v", e)
	}
	_, err = tx.Exec(ctx, stmt)
	if pg := asPgErr(err); pg != nil && pg.Code == "55P03" {
		t.Fatalf("%s: lock_not_available after 30s -- this is CONTENTION with another "+
			"package's transactions, not a missing guard; re-run", stmt)
	}
	return err
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
