package db

// holdingmigration_test.go -- M11 API-1a's three migrations as files: 00040 (the migration
// ledger closed to tappa_app), 00041 (the holding schema) and 00042 (its operator surface) --
// their preconditions, their Downs and Ups, the tables' reach under production's default ACL,
// the role a fresh pod is born with, and the operator's two-phase read. Everything that changes
// the schema runs inside a SAVEPOINT of opTx's transaction and is rolled back AT ONCE (00041's
// Down takes ACCESS EXCLUSIVE on tenants; a rolled-back savepoint releases its locks, so no other
// package's test waits for more than the savepoint's own few statements).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	op00041File = "00041_create_holdings.sql"
	op00042File = "00042_put_holdings_on_the_operator_surface.sql"
)

// opKindsAt42 is the closed set of read kinds at 00042 (HEAD): 00034's seven and 'holdings'.
var opKindsAt42 = append(append([]string{}, opKindsAt34...), "holdings")

// opHoldingWriteShapeDef is operator_audit_log_holding_write_shape's text (pg_get_constraintdef,
// 00042): a holding row names its holding in its detail and nothing else -- its tenant for the two
// membership kinds, none for holding_created.
const opHoldingWriteShapeDef = `CHECK (((kind <> ALL (ARRAY['holding_created'::text, 'holding_member_added'::text, 'holding_member_removed'::text])) OR ((target_admin_id IS NULL) AND (target_scope IS NULL) AND (page_number IS NULL) AND (page_size IS NULL) AND ((target_tenant_id IS NULL) = (kind = 'holding_created'::text)) AND (detail ? 'holding_id'::text) AND ((detail - 'holding_id'::text) = '{}'::jsonb) AND (jsonb_typeof((detail -> 'holding_id'::text)) = 'string'::text) AND ((detail ->> 'holding_id'::text) ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'::text))))`

// op00042ListEdits are the edits 00042 makes to 00039's bodies, in order. Undoing them must give
// 00039's body back byte for byte (TestOperator00042_TheFunctionsAndTheirExactSignatures).
var op00042ListEdits = map[string][][2]string{
	"op_begin_read": {
		{"'operator_audit', 'tenant_billing', 'tenant_vat') THEN\n",
			"'operator_audit', 'tenant_billing', 'tenant_vat', 'holdings') THEN\n"},
		{"                                                   'tenant_api_enabled', 'tenant_api_disabled') THEN\n",
			"                                                   'tenant_api_enabled', 'tenant_api_disabled',\n" +
				"                                                   'holding_created', 'holding_member_added',\n" +
				"                                                   'holding_member_removed') THEN\n"},
	},
	"op_read_audit": {
		{"                                                            'tenant_api_enabled', 'tenant_api_disabled')\n",
			"                                                            'tenant_api_enabled', 'tenant_api_disabled',\n" +
				"                                                            'holding_created', 'holding_member_added',\n" +
				"                                                            'holding_member_removed')\n"},
		{"                            p.target_scope IN ('legal_versions', 'tenant_detail', 'tenant_plaques', 'tenant_billing',\n" +
			"                                                'tenant_vat')\n",
			"                            p.target_scope IN ('legal_versions', 'tenant_detail', 'tenant_plaques', 'tenant_billing',\n" +
				"                                                'tenant_vat', 'holdings')\n"},
		{"                       ELSE p.target_scope IS NULL AND p.detail = '{}'::jsonb\n",
			"                       WHEN p.kind IN ('holding_created', 'holding_member_added', 'holding_member_removed') THEN\n" +
				"                            p.target_scope IS NULL\n" +
				"                            AND p.detail ? 'holding_id' AND (p.detail - 'holding_id') = '{}'::jsonb\n" +
				"                            AND jsonb_typeof(p.detail -> 'holding_id') = 'string'\n" +
				"                            AND (p.detail ->> 'holding_id') ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'\n" +
				"                       ELSE p.target_scope IS NULL AND p.detail = '{}'::jsonb\n"},
		{"                                            'tenant_plaques', 'operator_audit', 'tenant_billing',\n" +
			"                                            'tenant_vat')\n",
			"                                            'tenant_plaques', 'operator_audit', 'tenant_billing',\n" +
				"                                            'tenant_vat', 'holdings')\n"},
	},
}

// holdingPrecondition is a migration's first DO block, as goose runs it.
func holdingPrecondition(t *testing.T, file string) string {
	t.Helper()
	up, _ := opMigrationSections(t, file)
	i, j := strings.Index(up, "DO $$"), strings.Index(up, "-- +goose StatementEnd")
	if i < 0 || j < i {
		t.Fatalf("%s's Up does not open with the precondition DO block", file)
	}
	return up[i:j]
}

// holdingInSavepoint runs body in a savepoint of tx that is rolled back when body returns. It is
// for bodies that take NO strong table lock: one that does -- a Down, a DISABLE TRIGGER, any DDL on
// a table of public -- fails the test here, because such a body belongs in holdingLockedSavepoint,
// which takes holdingDownTables FIRST. (The API-1b review's DÜŞÜK-A: the lock was a call each
// Down-walking body had to remember, and the measured mutant -- the call left out of
// TestHolding00043And00044_DownGiveBackAndUpTakesItAgain's first savepoint -- stayed green and
// deadlocked 20 times under load.)
func holdingInSavepoint(t *testing.T, tx pgx.Tx, body func(sp pgx.Tx)) {
	t.Helper()
	holdingSavepoint(t, tx, false, body)
}

// holdingLockedSavepoint runs body in a savepoint of tx, rolled back when body returns, with
// holdingDownTables taken FIRST (holdingLockForDowns) -- the only way a body that walks a Down or
// changes a table (TestHoldingCheck_ChannelRules' DISABLE TRIGGER: the check below found it
// taking ACCESS EXCLUSIVE on transactions with nothing taken first) gets its savepoint, so no such
// body can forget the lock. Before the rollback it fails the test
// when the savepoint holds a strong lock on a table outside the list (the list fell behind the
// Downs) or does not hold every listed table that still exists (the lock was not taken).
func holdingLockedSavepoint(t *testing.T, tx pgx.Tx, body func(sp pgx.Tx)) {
	t.Helper()
	holdingSavepoint(t, tx, true, body)
}

func holdingSavepoint(t *testing.T, tx pgx.Tx, downs bool, body func(sp pgx.Tx)) {
	t.Helper()
	ctx := t.Context()
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	defer func() {
		if err := sp.Rollback(ctx); err != nil {
			t.Fatalf("rollback to savepoint: %v", err)
		}
	}()
	if downs {
		holdingLockForDowns(t, ctx, sp)
	}
	body(sp)
	if t.Failed() {
		return
	}
	if sp.Conn().PgConn().TxStatus() == 'E' {
		t.Errorf("the savepoint ended aborted, so its locks cannot be read: run a statement that is meant to fail in a nested savepoint")
		return
	}
	if !downs {
		if strong := strongTableLocksOutside(t, ctx, sp, nil); len(strong) > 0 {
			t.Errorf("the savepoint took a strong lock on %v: a body that walks a Down or changes a table belongs in "+
				"holdingLockedSavepoint, which takes holdingDownTables first (ADR 0027's API-1b note, DÜŞÜK-A)", strong)
		}
		return
	}
	if extra := strongTableLocksOutside(t, ctx, sp, holdingDownTables); len(extra) > 0 {
		t.Errorf("the savepoint took a strong lock on %v, outside holdingDownTables: a Down touches a table "+
			"holdingLockForDowns does not take first (the 40P01 class ADR 0027's API-1b note measured) -- add it", extra)
	}
	var missing []string
	if err := sp.QueryRow(ctx, `
		SELECT coalesce(array_agg(n ORDER BY n), '{}') FROM unnest($1::text[]) AS n
		 WHERE to_regclass('public.' || n) IS NOT NULL
		   AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_locks l
		                    WHERE l.locktype = 'relation' AND l.pid = pg_backend_pid() AND l.granted
		                      AND l.mode = 'AccessExclusiveLock' AND l.relation = to_regclass('public.' || n))`,
		holdingDownTables).Scan(&missing); err != nil {
		t.Fatalf("read the savepoint's locks: %v", err)
	}
	if len(missing) > 0 {
		t.Errorf("the savepoint does not hold %v ACCESS EXCLUSIVE: holdingLockedSavepoint's lock was not taken", missing)
	}
}

// holdingNestedRun runs sql (a migration's DO block, with its own dollar quoting -- the simple
// protocol) in a savepoint nested in sp and rolls it back, so a statement that is meant to fail
// leaves sp usable: holdingSavepoint reads its locks before the rollback. Its error, nil when
// it ran.
func holdingNestedRun(t *testing.T, ctx context.Context, sp pgx.Tx, sql string) error {
	t.Helper()
	nested, err := sp.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, runErr := nested.Conn().PgConn().Exec(ctx, sql).ReadAll()
	if err := nested.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	return runErr
}

// holdingClearKinds removes, inside the transaction, the operator rows of the holding kinds and
// the 'holdings' tickets (operator_audit_log is append-only: its row trigger is disabled for the
// delete and enabled again) -- so 00042's Down restores the two kind CHECKs VALIDATED, the shape
// 00039's own tests expect. Committed holding rows exist on any database the live tests ran on.
func holdingClearKinds(t *testing.T, sp pgx.Tx) {
	t.Helper()
	ctx := t.Context()
	opMustHoldTheTablesLockExclusive(t, ctx, sp)
	for _, s := range []string{
		`DELETE FROM public.operator_read_tickets WHERE kind = 'holdings'`,
		`ALTER TABLE public.operator_audit_log DISABLE TRIGGER operator_audit_log_append_only`,
		`DELETE FROM public.operator_audit_log WHERE kind LIKE 'holding\_%'`,
		`ALTER TABLE public.operator_audit_log ENABLE TRIGGER operator_audit_log_append_only`,
	} {
		if _, err := sp.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

// ----------------------------------------------------------------- 00041 schema --

// TestHoldingSchema_AppRoleHasNoVerbOnHoldingTables is ADR 0027 §1's "tappa_app dört fiilde
// yetkisiz, arwd simüle edilerek" and CLAUDE.md §6's tenant-scope-less class:
//   - on this database: tappa_app holds no SELECT, INSERT or UPDATE on any column of holdings or
//     holding_members and no DELETE, TRUNCATE, REFERENCES or TRIGGER; both tables have ENABLE and
//     FORCE row level security and NO policy; neither has a column named tenant_id;
//   - under production's default ACL, made inside a savepoint (and proven in force by a table
//     created there, which gets all four verbs), 00042's and 00041's Downs and then their Ups leave
//     tappa_app with nothing again -- the explicit REVOKEs, not the default, decide;
//   - the R5 waiver of each table is in 00041's Up, in the form scripts/redline-check.sh reads
//     (the script's own pattern is held to it here) -- so the script's R5 WARN names both.
func TestHoldingSchema_AppRoleHasNoVerbOnHoldingTables(t *testing.T) {
	ctx, tx := opTx(t)
	check := func(q opQuerier, when string) {
		t.Helper()
		for _, table := range []string{"holdings", "holding_members"} {
			for _, priv := range []string{"SELECT", "INSERT", "UPDATE"} {
				if got := opColumns(t, ctx, q, "tappa_app", table, priv); got != "" {
					t.Errorf("%s: tappa_app %s on %s = (%s)", when, priv, table, got)
				}
			}
			for _, priv := range []string{"DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"} {
				var may bool
				if err := q.QueryRow(ctx, `SELECT has_table_privilege('tappa_app', 'public.' || $1, $2)`, table, priv).Scan(&may); err != nil {
					t.Fatal(err)
				}
				if may {
					t.Errorf("%s: tappa_app holds %s on %s", when, priv, table)
				}
			}
			var enabled, forced bool
			if err := q.QueryRow(ctx, `SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = ('public.' || $1)::regclass`, table).Scan(&enabled, &forced); err != nil {
				t.Fatal(err)
			}
			policies := opInt(t, ctx, q, `SELECT count(*) FROM pg_policy WHERE polrelid = ('public.' || $1)::regclass`, table)
			tenantID := opInt(t, ctx, q, `SELECT count(*) FROM pg_attribute WHERE attrelid = ('public.' || $1)::regclass AND attname = 'tenant_id' AND NOT attisdropped`, table)
			if !enabled || !forced || policies != 0 || tenantID != 0 {
				t.Errorf("%s: %s ENABLE=%v FORCE=%v, %d policies, %d tenant_id column(s); want ENABLE, FORCE, no policy, no tenant_id", when, table, enabled, forced, policies, tenantID)
			}
		}
	}
	check(tx, "on this database")

	up41, down41 := opMigrationSections(t, op00041File)
	up42, down42 := opMigrationSections(t, op00042File)
	holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
		holdingClearKinds(t, sp)
		if _, err := sp.Exec(ctx, `ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO tappa_app`); err != nil {
			t.Fatal(err)
		}
		if _, err := sp.Exec(ctx, `CREATE TABLE zz_api1a_default_probe (id int)`); err != nil {
			t.Fatal(err)
		}
		var s, i, u, d bool
		if err := sp.QueryRow(ctx, `SELECT has_table_privilege('tappa_app', 'zz_api1a_default_probe', 'SELECT'), has_table_privilege('tappa_app', 'zz_api1a_default_probe', 'INSERT'),
		                                   has_table_privilege('tappa_app', 'zz_api1a_default_probe', 'UPDATE'), has_table_privilege('tappa_app', 'zz_api1a_default_probe', 'DELETE')`).Scan(&s, &i, &u, &d); err != nil {
			t.Fatal(err)
		}
		if !(s && i && u && d) {
			t.Fatalf("PREMISE: the widened default did not reach a new table (%v %v %v %v)", s, i, u, d)
		}
		holdingBelow43(t, ctx, sp)
		opRunSection(t, ctx, sp, down42, "00042 Down")
		opRunSection(t, ctx, sp, down41, "00041 Down")
		opRunSection(t, ctx, sp, up41, "00041 Up under the production default")
		opRunSection(t, ctx, sp, up42, "00042 Up under the production default")
		check(sp, "after 00041/00042 re-run under production's default ACL")
	})

	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "redline-check.sh"))
	if err != nil {
		t.Fatal(err)
	}
	const pattern = `^[[:space:]]*-- redline: no-tenant-scope\([a-z0-9_.]+\) .+`
	if !strings.Contains(string(script), "grep -nE '"+pattern+"'") {
		t.Fatalf("scripts/redline-check.sh no longer reads the R5 waiver with %q; this test reads 00041 with it", pattern)
	}
	re := regexp.MustCompile(`(?m)^[[:space:]]*-- redline: no-tenant-scope\(([a-z0-9_.]+)\) .+`)
	var waived []string
	for _, m := range re.FindAllStringSubmatch(up41, -1) {
		waived = append(waived, m[1])
	}
	if strings.Join(waived, ",") != "holdings,holding_members" {
		t.Errorf("00041's Up waives %v, want holdings and holding_members (in that order)", waived)
	}
}

// ------------------------------------------------------------- preconditions --

// TestHolding00041_PreconditionRefusesAWrongCluster: 00041's first statement refuses, with 55000
// naming 00041, a database where its objects already exist (HEAD), and -- on this database taken
// back to 00040 inside a savepoint -- a cluster without tappa_holdingdefiner (naming the runbook
// section and the 01-roles.sql block), a holding definer that can log in, is a superuser or does
// not bypass RLS, that has a member or is a member, and one that already reaches a column or a
// sequence in public (a column grant cannot narrow it). The cluster at 00040 passes.
func TestHolding00041_PreconditionRefusesAWrongCluster(t *testing.T) {
	ctx, tx := opTx(t)
	pre := holdingPrecondition(t, op00041File)
	run := func(sp pgx.Tx) (string, string) {
		return opCode(holdingNestedRun(t, ctx, sp, pre))
	}
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		if code, msg := run(sp); code != sqlstatePrerequisiteState || !strings.Contains(msg, "already there") || !strings.Contains(msg, "00041") {
			t.Errorf("at HEAD: %q %q, want 55000: the objects are already there", code, msg)
		}
	})
	_, down41 := opMigrationSections(t, op00041File)
	_, down42 := opMigrationSections(t, op00042File)
	for _, c := range []struct {
		what  string
		setup []string
		want  string
	}{
		{"at 00040", nil, ""},
		{"the role absent", []string{`ALTER ROLE tappa_holdingdefiner RENAME TO zz_api1a_was_holdingdefiner`}, `section "Holding role (M11 API-1a)"`},
		{"the role can log in", []string{`ALTER ROLE tappa_holdingdefiner LOGIN`}, "must be NOLOGIN"},
		{"the role is a superuser", []string{`ALTER ROLE tappa_holdingdefiner SUPERUSER`}, "must be NOLOGIN"},
		{"the role does not bypass RLS", []string{`ALTER ROLE tappa_holdingdefiner NOBYPASSRLS`}, "must be NOLOGIN"},
		{"the role has a member", []string{`GRANT tappa_holdingdefiner TO tappa_app`}, "has members"},
		{"the role is a member", []string{`GRANT tappa_resolver TO tappa_holdingdefiner`}, "is a member of another role"},
		{"the role reaches a key column", []string{`GRANT SELECT (aes_key_ref) ON tags TO tappa_holdingdefiner`}, "already reaches"},
		{"the role reaches a sequence", []string{`GRANT USAGE ON SEQUENCE goose_db_version_id_seq TO tappa_holdingdefiner`}, "already reaches"},
	} {
		holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
			holdingClearKinds(t, sp)
			holdingBelow43(t, ctx, sp)
			opRunSection(t, ctx, sp, down42, "00042 Down")
			opRunSection(t, ctx, sp, down41, "00041 Down")
			for _, s := range c.setup {
				if _, err := sp.Exec(ctx, s); err != nil {
					t.Fatalf("%s: %q: %v", c.what, s, err)
				}
			}
			code, msg := run(sp)
			switch {
			case c.want == "" && code != "":
				t.Errorf("%s: the precondition refuses a correct cluster: %s %s", c.what, code, msg)
			case c.want != "" && (code != sqlstatePrerequisiteState || !strings.Contains(msg, c.want) || !strings.Contains(msg, "00041")):
				t.Errorf("%s: %q %q, want 55000 naming 00041 and containing %q", c.what, code, msg, c.want)
			}
		})
	}
}

// TestOperator00042_PreconditionRefusesAWrongCluster: 00042's first statement passes this cluster
// taken to 00041 (00042's Down) and refuses, with 55000 naming 00042: the operator roles of the
// wrong shape; 00041's objects missing; a kind CHECK another migration widened in between; a body
// of the three functions it replaces that another migration changed (00039's own case, one step
// on: this file must stop, not undo it); and a definer that already reaches what the grants give.
func TestOperator00042_PreconditionRefusesAWrongCluster(t *testing.T) {
	ctx, tx := opTx(t)
	pre := holdingPrecondition(t, op00042File)
	_, down41 := opMigrationSections(t, op00041File)
	_, down42 := opMigrationSections(t, op00042File)
	up39, _ := opMigrationSections(t, op00039File)
	bodyDrift := func(fn string) string {
		stmt := regexp.MustCompile(`(?s)CREATE (?:OR REPLACE )?FUNCTION public\.` + fn + `\(.*?\nAS \$\$.*?\$\$;`).FindString(up39)
		if stmt == "" {
			t.Fatalf("00039's Up does not define %s", fn)
		}
		stmt = strings.Replace(stmt, "CREATE FUNCTION", "CREATE OR REPLACE FUNCTION", 1)
		return strings.Replace(stmt, "\nAS $$", "\nAS $$\n-- changed by a migration between 00039 and 00042", 1)
	}
	widened := `ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check;
	            ALTER TABLE operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check CHECK (kind IN ('` +
		strings.Join(opAuditKinds39, "', '") + `', 'tenant_suspended')) NOT VALID`
	for _, c := range []struct {
		what  string
		setup []string
		want  string
	}{
		{"at 00041", nil, ""},
		{"tappa_opdefiner is a superuser", []string{`ALTER ROLE tappa_opdefiner SUPERUSER`}, "tappa_opdefiner must be"},
		{"tappa_operator has a member", []string{`GRANT tappa_operator TO tappa_app`}, "role tappa_operator has members"},
		{"00041's tables missing", []string{down41}, "that state is not there"},
		{"the kind CHECK widened by another migration", []string{widened}, "rebase this file onto it"},
		{"op_begin_read's body changed", []string{bodyDrift("op_begin_read")}, "a live body is not 00039's"},
		{"op_read_audit's body changed", []string{bodyDrift("op_read_audit")}, "a live body is not 00039's"},
		{"op_enable_tenant_api's body changed", []string{bodyDrift("op_enable_tenant_api")}, "a live body is not 00039's"},
		{"the definer already reads api_code", []string{`GRANT SELECT (api_code) ON tenants TO tappa_opdefiner`}, "a privilege no migration gave"},
		{"the definer already reads the holdings", []string{`GRANT SELECT ON holdings TO tappa_opdefiner`}, "a privilege no migration gave"},
	} {
		holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
			holdingClearKinds(t, sp)
			opRunSection(t, ctx, sp, down42, "00042 Down")
			for _, s := range c.setup {
				if _, err := sp.Conn().PgConn().Exec(ctx, s).ReadAll(); err != nil {
					t.Fatalf("%s: setup: %v", c.what, err)
				}
			}
			err := holdingNestedRun(t, ctx, sp, pre)
			code, msg := opCode(err)
			switch {
			case c.want == "" && err != nil:
				t.Errorf("%s: the precondition refuses a correct cluster: %v", c.what, err)
			case c.want != "" && (code != sqlstatePrerequisiteState || !strings.Contains(msg, c.want) || !strings.Contains(msg, "00042")):
				t.Errorf("%s: %q %q, want 55000 naming 00042 and containing %q", c.what, code, msg, c.want)
			}
		})
	}
}

// --------------------------------------------------------------------- Down/Up --

// holdingSurface is the state 00041 and 00042 change, read off the catalogue.
type holdingSurface struct {
	beginMD5, auditMD5, enableMD5 string
	kinds, tickets                string
	kindsOK, ticketsOK            bool
	shape                         string
	opFns, holdingObjects         int64
	opdefTenants, opdefHoldings   string
	holdingdefReach               string
}

func holdingSurfaceOf(t *testing.T, q opQuerier) holdingSurface {
	t.Helper()
	ctx := t.Context()
	var s holdingSurface
	for fn, dst := range map[string]*string{"op_begin_read": &s.beginMD5, "op_read_audit": &s.auditMD5, "op_enable_tenant_api": &s.enableMD5} {
		if err := q.QueryRow(ctx, `SELECT md5(prosrc) FROM pg_proc WHERE proname = $1`, fn).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	s.kinds, s.kindsOK = opConstraint(t, ctx, q, "operator_audit_log", "operator_audit_log_kind_check")
	s.tickets, s.ticketsOK = opConstraint(t, ctx, q, "operator_read_tickets", "operator_read_tickets_kind_check")
	if n := opInt(t, ctx, q, `SELECT count(*) FROM pg_constraint WHERE conname = 'operator_audit_log_holding_write_shape'`); n == 1 {
		s.shape, _ = opConstraint(t, ctx, q, "operator_audit_log", "operator_audit_log_holding_write_shape")
	}
	s.opFns = opInt(t, ctx, q, `SELECT count(*) FROM pg_proc WHERE proname IN ('op_create_holding', 'op_add_holding_member', 'op_remove_holding_member', 'op_read_holdings')`)
	s.holdingObjects = opInt(t, ctx, q, `SELECT (SELECT count(*) FROM pg_class WHERE relnamespace = 'public'::regnamespace AND relname IN ('holdings', 'holding_members'))
	                                          -- 00041's holding_peer: the pair form (00060's set form is not 00041's object)
	                                          + (SELECT count(*) FROM pg_proc WHERE proname = 'holding_timezone_lock'
	                                                                             OR oid = to_regprocedure('public.holding_peer(uuid, uuid, text)'))
	                                          + (SELECT count(*) FROM pg_trigger WHERE tgname = 'tenants_holding_timezone_lock')`)
	s.opdefTenants = opColumns(t, ctx, q, "tappa_opdefiner", "tenants", "SELECT")
	if s.holdingObjects > 0 {
		s.opdefHoldings = opColumns(t, ctx, q, "tappa_opdefiner", "holdings", "SELECT") + "|" + opColumns(t, ctx, q, "tappa_opdefiner", "holding_members", "SELECT") +
			"|" + opColumns(t, ctx, q, "tappa_opdefiner", "holding_members", "UPDATE")
		s.holdingdefReach = opColumns(t, ctx, q, "tappa_holdingdefiner", "holding_members", "SELECT") + "|" + opColumns(t, ctx, q, "tappa_holdingdefiner", "holding_members", "UPDATE")
	}
	return s
}

// TestHolding00041And00042_DownGiveBackAndUpTakesItAgain: 00042's Down gives op_begin_read,
// op_read_audit and op_enable_tenant_api 00039's bodies (by digest -- the digests 00042's own
// precondition names), drops the four op_* and the holding-row shape, restores 00039's two kind
// CHECKs VALIDATED (with no holding row) and takes back exactly its column grants (tenants back to
// 00039's list; nothing on the holding tables); 00041's Down then drops the tables, holding_peer
// and the trigger. Their Ups take HEAD's state again, identical. With holding rows present -- an
// operator row of a holding kind and a 'holdings' ticket -- 00042's Down leaves the kind CHECKs NOT
// VALID and the rows in place, 00041's Down drops the memberships with the table, and the Ups
// compose over that too.
func TestHolding00041And00042_DownGiveBackAndUpTakesItAgain(t *testing.T) {
	ctx, tx := opTx(t)
	up41, down41 := opMigrationSections(t, op00041File)
	up42, down42 := opMigrationSections(t, op00042File)
	head := holdingSurfaceOf(t, tx)
	at41 := func(s holdingSurface, when string, notValid bool) {
		t.Helper()
		kinds, tickets := opKindCheckDef(opAuditKinds39), opKindCheckDef(opKindsAt34)
		if notValid {
			kinds += " NOT VALID"
			tickets += " NOT VALID"
		}
		if s.beginMD5 != "d1327314ee37600077e453664988eb6b" || s.auditMD5 != "3182b5376cff359ed521631fa95dbac1" || s.enableMD5 != "a385fee5eadf7c641fd6393d6f6e1b08" ||
			s.kinds != kinds || s.kindsOK == notValid || s.tickets != tickets || s.ticketsOK == notValid || s.shape != "" || s.opFns != 0 ||
			s.holdingObjects != 5 || s.opdefTenants != opDefinerTenantsSel39 || s.opdefHoldings != "||" ||
			s.holdingdefReach != "holding_id,member_tenant_id,joined_at,left_at|lock_anchor" {
			t.Errorf("%s: not 00041's state (NOT VALID=%v): %+v", when, notValid, s)
		}
	}
	if head.opFns != 4 || head.holdingObjects != 5 || head.shape != opHoldingWriteShapeDef || head.opdefTenants != opDefinerTenantsSel42 ||
		head.opdefHoldings != "id,name,created_at|holding_id,member_tenant_id,joined_at,left_at|left_at" {
		t.Fatalf("PREMISE at HEAD: %+v", head)
	}
	holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
		holdingClearKinds(t, sp)
		holdingBelow43(t, ctx, sp)
		opRunSection(t, ctx, sp, down42, "00042 Down")
		at41(holdingSurfaceOf(t, sp), "after 00042's Down", false)
		opRunSection(t, ctx, sp, down41, "00041 Down")
		if s := holdingSurfaceOf(t, sp); s.holdingObjects != 0 || opInt(t, ctx, sp, `SELECT count(*) FROM pg_proc WHERE proowner = 'tappa_holdingdefiner'::regrole`) != 0 {
			t.Errorf("after 00041's Down: %d holding objects, the definer owns functions", s.holdingObjects)
		}
		opRunSection(t, ctx, sp, up41, "00041 Up again")
		opRunSection(t, ctx, sp, up42, "00042 Up again")
		again := holdingSurfaceOf(t, sp)
		head.kindsOK, head.ticketsOK = true, true
		if again != head {
			t.Errorf("after the Ups again:\n %+v\nwant HEAD's\n %+v", again, head)
		}
	})
	holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
		a := opNewActive(t, ctx, sp)
		hash, session := opNewSession(t, ctx, sp, a.id, true)
		h, err := holdingCreateAs(t, ctx, sp, hash, "api1a down "+opToken(t))
		if err != nil {
			t.Fatal(err)
		}
		member := holdingTenant(t, ctx, sp, "Europe/Malta", nil)
		if err := holdingAddAs(t, ctx, sp, hash, h, member); err != nil {
			t.Fatal(err)
		}
		opForgeRead(t, ctx, sp, session, a.id, holdingsReadKind, opRandHex(t), nil, opCommittedXact(t, ctx), "30 seconds")
		holdingBelow43(t, ctx, sp)
		opRunSection(t, ctx, sp, down42, "00042 Down with holding rows present")
		at41(holdingSurfaceOf(t, sp), "after 00042's Down (holding rows)", true)
		if len(holdingOperatorRowsOf(t, ctx, sp, session)) != 2 || len(holdingTrailOf(t, ctx, sp, member)) != 1 || !holdingLiveIn(t, ctx, sp, h, member) {
			t.Error("00042's Down took away a row: the operator's rows, the tenant's row and the membership stay")
		}
		opRunSection(t, ctx, sp, down41, "00041 Down with a membership present")
		if len(holdingTrailOf(t, ctx, sp, member)) != 1 {
			t.Error("00041's Down took the tenant's audit row with the table")
		}
		opRunSection(t, ctx, sp, up41, "00041 Up again (holding rows)")
		opRunSection(t, ctx, sp, up42, "00042 Up again (holding rows)")
		if s := holdingSurfaceOf(t, sp); !s.kindsOK || !s.ticketsOK || s.opFns != 4 {
			t.Errorf("after the Ups over the rows: %+v", s)
		}
	})
}

// ------------------------------------------------------------------ the role --

// holdingRolesBlock returns the HOLDING ROLE block of scripts/db-init/01-roles.sql exactly as the
// runbook's sed extracts it (markers included).
func holdingRolesBlock(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "db-init", "01-roles.sql"))
	if err != nil {
		t.Fatal(err)
	}
	const begin, end = "-- >>> HOLDING ROLE (M11 API-1a) >>>", "-- <<< HOLDING ROLE (M11 API-1a) <<<"
	src := string(b)
	i, j := strings.Index(src, begin), strings.Index(src, end)
	if i < 0 || j < i {
		t.Fatalf("01-roles.sql does not carry the HOLDING ROLE markers in order (begin=%d end=%d)", i, j)
	}
	return src[i : j+len(end)]
}

// TestRestore_FreshPodCarriesTheHoldingRole (ADR 0027 §2: pg_dump carries no role, so the restore
// rehearsal is to a pod built from nothing): the role a fresh volume gets is 01-roles.sql's HOLDING
// ROLE block -- this test runs that block, cut as the runbook cuts it, on this cluster taken back to
// 00040 with the role renamed away (a cluster that never had it): it creates tappa_holdingdefiner
// NOLOGIN NOSUPERUSER BYPASSRLS with USAGE on public; run again it is a no-op; run over a role that
// can log in it takes LOGIN away; and 00041's precondition then passes and its Up applies against
// the fresh role. The runbook section 00041's refusal names exists and extracts the block by the
// same markers. (The same was measured on a throwaway Postgres built from scripts/db-init:
// the role was there and goose went to HEAD -- the API-1a report.)
func TestRestore_FreshPodCarriesTheHoldingRole(t *testing.T) {
	ctx, tx := opTx(t)
	block := holdingRolesBlock(t)
	readme, err := os.ReadFile(filepath.Join("..", "..", "deploy", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	pre := holdingPrecondition(t, op00041File)
	if !strings.Contains(pre, `section "Holding role (M11 API-1a)"`) || !strings.Contains(pre, "scripts/db-init/01-roles.sql") {
		t.Fatal("00041's refusal does not name the runbook section and the block")
	}
	if !regexp.MustCompile(`(?m)^#+ .*Holding role \(M11 API-1a\)`).Match(readme) {
		t.Error(`deploy/README.md has no heading "Holding role (M11 API-1a)"`)
	}
	for _, marker := range []string{">>> HOLDING ROLE (M11 API-1a) >>>", "<<< HOLDING ROLE (M11 API-1a) <<<"} {
		if !strings.Contains(string(readme), marker) {
			t.Errorf("deploy/README.md's runbook does not cut the block by %q", marker)
		}
	}
	up41, down41 := opMigrationSections(t, op00041File)
	_, down42 := opMigrationSections(t, op00042File)
	holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
		holdingClearKinds(t, sp)
		holdingBelow43(t, ctx, sp)
		opRunSection(t, ctx, sp, down42, "00042 Down")
		opRunSection(t, ctx, sp, down41, "00041 Down")
		if _, err := sp.Exec(ctx, `ALTER ROLE tappa_holdingdefiner RENAME TO zz_api1a_was_holdingdefiner`); err != nil {
			t.Fatal(err)
		}
		shape := func() (login, super, bypass, usage bool) {
			if err := sp.QueryRow(ctx, `SELECT rolcanlogin, rolsuper, rolbypassrls, has_schema_privilege(oid, 'public', 'USAGE')
			                             FROM pg_roles WHERE rolname = 'tappa_holdingdefiner'`).Scan(&login, &super, &bypass, &usage); err != nil {
				t.Fatalf("read the role: %v", err)
			}
			return
		}
		for k := 0; k < 2; k++ {
			opRunSection(t, ctx, sp, block, "the HOLDING ROLE block")
			if login, super, bypass, usage := shape(); login || super || !bypass || !usage {
				t.Errorf("run %d of the block: login=%v super=%v bypassrls=%v usage=%v", k+1, login, super, bypass, usage)
			}
		}
		if _, err := sp.Exec(ctx, `ALTER ROLE tappa_holdingdefiner LOGIN`); err != nil {
			t.Fatal(err)
		}
		opRunSection(t, ctx, sp, block, "the HOLDING ROLE block over a role that can log in")
		if login, _, _, _ := shape(); login {
			t.Error("the block left tappa_holdingdefiner able to log in")
		}
		if _, err := sp.Conn().PgConn().Exec(ctx, pre).ReadAll(); err != nil {
			t.Errorf("00041's precondition refuses the role the block made: %v", err)
		}
		opRunSection(t, ctx, sp, up41, "00041 Up against the fresh role")
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM pg_proc WHERE proowner = 'tappa_holdingdefiner'::regrole`); n != 2 {
			t.Errorf("after 00041's Up the fresh role owns %d functions, want holding_peer and the trigger", n)
		}
	})
}

// --------------------------------------------------------------------- the read --

// TestOpReadHoldings_ReadsWhatTheWritersWrote drives operator.go's four functions on the pool the
// production opener builds (tappa_operator; each call its own transactions), on committed rows:
// CreateHolding, two AddHoldingMember, one RemoveHoldingMember, then Holdings -- a 'read' row
// (scope 'holdings', the page, no tenant, detail {}) and the page: the holding's live member
// before its ended one, with name, zone and code; an empty holding as one row with no member; a
// dead session is ErrOperatorRefused, and a pgx.Tx -- one transaction, so phase one's ticket is
// not committed -- is refused too. What it leaves: holdingLiveFixture's.
func TestOpReadHoldings_ReadsWhatTheWritersWrote(t *testing.T) {
	f := holdingLiveFixture(t)
	ctx := f.ctx
	o, err := openOperatorDB(ctx, f.dsn, asOperator)
	if err != nil {
		t.Fatalf("open the operator pool: %v", err)
	}
	defer o.Close()
	kf := f.tenant(t, "Europe/Malta", "KF")
	km := f.tenant(t, "Europe/Malta", "KM")
	h, err := CreateHolding(ctx, o.pool, f.hash, "api1a read "+opToken(t))
	if err != nil {
		t.Fatalf("CreateHolding: %v", err)
	}
	f.adopt(h)
	empty, err := CreateHolding(ctx, o.pool, f.hash, "api1a empty "+opToken(t))
	if err != nil {
		t.Fatal(err)
	}
	f.adopt(empty)
	for _, x := range []uuid.UUID{kf, km} {
		if err := AddHoldingMember(ctx, o.pool, f.hash, h, x); err != nil {
			t.Fatalf("AddHoldingMember: %v", err)
		}
	}
	if err := RemoveHoldingMember(ctx, o.pool, f.hash, h, km); err != nil {
		t.Fatalf("RemoveHoldingMember: %v", err)
	}
	reads := f.liveReads(t, ctx, f.session)
	rows, err := Holdings(ctx, o.pool, f.hash, 1)
	if err != nil {
		t.Fatalf("Holdings: %v", err)
	}
	if n := f.liveReads(t, ctx, f.session); n != reads+1 {
		t.Errorf("%d 'read' rows written by one read, want 1", n-reads)
	}
	var scope string
	var number, size *int32
	var tenant *uuid.UUID
	var detail string
	if err := f.owner.QueryRow(ctx, `SELECT target_scope, page_number, page_size, target_tenant_id, detail::text FROM operator_audit_log
	                                  WHERE session_id = $1 AND kind = 'read' ORDER BY at DESC, id DESC LIMIT 1`, f.session).Scan(&scope, &number, &size, &tenant, &detail); err != nil {
		t.Fatal(err)
	}
	if scope != "holdings" || number == nil || *number != 1 || size == nil || *size != HoldingsPerPage || tenant != nil || detail != "{}" {
		t.Errorf("the read row is scope=%s page=%v size=%v tenant=%v detail=%s", scope, opDeref(number), opDeref(size), tenant, detail)
	}
	var mine, none []HoldingRow
	for _, r := range rows {
		switch r.HoldingID {
		case h:
			mine = append(mine, r)
		case empty:
			none = append(none, r)
		}
	}
	if len(mine) != 2 || mine[0].MemberTenantID == nil || *mine[0].MemberTenantID != kf || mine[0].LeftAt != nil ||
		opDeref(mine[0].MemberAPICode) != "KF" || opDeref(mine[0].MemberTimezone) != "Europe/Malta" || mine[0].MemberName == nil ||
		mine[1].MemberTenantID == nil || *mine[1].MemberTenantID != km || mine[1].LeftAt == nil {
		t.Errorf("the holding's rows are %+v, want the live KF member then the ended KM one", mine)
	}
	if len(none) != 1 || none[0].MemberTenantID != nil || none[0].JoinedAt != nil {
		t.Errorf("the empty holding's rows are %+v, want one row with no member", none)
	}
	if _, err := Holdings(ctx, o.pool, opRandHex(t), 1); !errors.Is(err, ErrOperatorRefused) {
		t.Errorf("Holdings with no session: %v, want ErrOperatorRefused", err)
	}
	conn := f.connect(t, ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SET LOCAL SESSION AUTHORIZATION tappa_operator`); err != nil {
		t.Fatal(err)
	}
	if _, err := Holdings(ctx, tx, f.hash, 1); !errors.Is(err, ErrOperatorRefused) {
		t.Errorf("Holdings on one transaction: %v, want ErrOperatorRefused (phase one is not committed)", err)
	}
}
