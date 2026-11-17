package db

// scopedaccess_test.go -- ADR 0048. tappa_readonly and tappa_readwrite are a person's way past RLS
// in production; their privileges come from deploy/db/scoped-access.sql, which the operator runs
// by hand after the SCOPED ACCESS ROLES block of scripts/db-init/01-roles.sql. Nothing in a deploy
// runs that file, so nothing would notice a migration that makes it wrong -- a new append-only
// table it would open, a SECURITY DEFINER function it would hand over, a statement that no longer
// parses. This test runs both pieces exactly as the runbook cuts them, TWICE, inside the owner's
// rolled-back transaction, against this schema at its newest migration, and reads the result.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// scopedAccessSQL is what the runbook pipes into psql: the 01-roles.sql block, then the file.
func scopedAccessSQL(t *testing.T) string {
	t.Helper()
	roles, err := os.ReadFile(filepath.Join("..", "..", "scripts", "db-init", "01-roles.sql"))
	if err != nil {
		t.Fatal(err)
	}
	const begin, end = "-- >>> SCOPED ACCESS ROLES (ADR 0048) >>>", "-- <<< SCOPED ACCESS ROLES (ADR 0048) <<<"
	s := string(roles)
	i, j := strings.Index(s, begin), strings.Index(s, end)
	if i < 0 || j < i {
		t.Fatalf("01-roles.sql does not carry the SCOPED ACCESS ROLES markers in order (begin=%d end=%d)", i, j)
	}
	grants, err := os.ReadFile(filepath.Join("..", "..", "deploy", "db", "scoped-access.sql"))
	if err != nil {
		t.Fatal(err)
	}
	return s[i:j+len(end)] + "\n" + string(grants)
}

// scopedAccessSnapshot lists every privilege either role holds in public, one line each.
func scopedAccessSnapshot(t *testing.T, ctx context.Context, tx pgx.Tx) string {
	t.Helper()
	var s string
	if err := tx.QueryRow(ctx, `
		SELECT coalesce(string_agg(line, E'\n' ORDER BY line COLLATE "C"), '') FROM (
		  SELECT r.rolname || ' ' || c.relname || ' ' || x.privilege_type AS line
		    FROM pg_class c CROSS JOIN LATERAL aclexplode(c.relacl) x JOIN pg_roles r ON r.oid = x.grantee
		   WHERE c.relnamespace = 'public'::regnamespace AND r.rolname IN ('tappa_readonly', 'tappa_readwrite')
		  UNION ALL
		  SELECT r.rolname || ' ' || c.relname || '.' || a.attname || ' ' || x.privilege_type
		    FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid
		   CROSS JOIN LATERAL aclexplode(a.attacl) x JOIN pg_roles r ON r.oid = x.grantee
		   WHERE c.relnamespace = 'public'::regnamespace AND r.rolname IN ('tappa_readonly', 'tappa_readwrite')
		  UNION ALL
		  SELECT 'default ' || d.defaclobjtype::text || ' ' || x.privilege_type || ' ' || pg_get_userbyid(x.grantee)
		    FROM pg_default_acl d CROSS JOIN LATERAL aclexplode(d.defaclacl) x
		   WHERE pg_get_userbyid(x.grantee) IN ('tappa_readonly', 'tappa_readwrite')) s`).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestScopedAccess_TheOperatorFileConvergesAndWithholdsTheRedLines: after the block and the file
// run (twice -- the second run must change nothing), (a) the two roles bypass RLS and nothing
// else; (b) tappa_readonly writes nothing and its sessions start read-only; (c) on every table
// that carries a tappa_forbid_mutation row guard -- derived here the way the file derives it, and
// at least twelve -- tappa_readwrite holds no UPDATE and no DELETE (§4.3); (d) on tags neither role
// touches aes_key_ref or app_key_ref, tappa_readwrite cannot move last_ctr, insert or delete
// (§4.4, §4.7); (e) no TRUNCATE, no CREATE on public, no EXECUTE on any SECURITY DEFINER function;
// (f) every other table is fully readable and, but for goose's ledger, writable by tappa_readwrite;
// (g) a table created AFTER the file ran gets SELECT, INSERT and nothing more for tappa_readwrite
// (the default stops there on purpose: it cannot know the table will become append-only).
//
// MUTANT (measured, 2026-11-17): the grant loop's guard test removed AND the revoke loop emptied
// -> red at (c), twelve tables; `'last_ctr'` dropped from the UPDATE exclusion -> red at (d); the
// default widened to SELECT, INSERT, UPDATE, DELETE AND its REVOKE line removed -> red at (g).
// Each belt ALONE was measured green (the other belt held): the guard test removed with the revoke
// loop intact, and the widened default with its REVOKE line intact -- the file keeps both on
// purpose, and a mutant must take both away to be seen.
func TestScopedAccess_TheOperatorFileConvergesAndWithholdsTheRedLines(t *testing.T) {
	ctx, tx := opTx(t)
	sql := scopedAccessSQL(t)
	opRunSection(t, ctx, tx, sql, "the scoped-access block and file (first run)")
	first := scopedAccessSnapshot(t, ctx, tx)
	opRunSection(t, ctx, tx, sql, "the scoped-access block and file (second run)")
	if second := scopedAccessSnapshot(t, ctx, tx); second != first {
		t.Errorf("a second run changed the privileges; the file must converge.\nfirst:\n%s\nsecond:\n%s", first, second)
	}

	// (a)
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_roles WHERE rolname IN ('tappa_readonly', 'tappa_readwrite')
	        AND rolbypassrls AND NOT rolsuper AND NOT rolcreaterole AND NOT rolcreatedb AND NOT rolreplication`); n != 2 {
		t.Errorf("(a) %d of the two roles have the shape BYPASSRLS and nothing else", n)
	}
	// (b)
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_db_role_setting s JOIN pg_roles r ON r.oid = s.setrole
	        WHERE r.rolname = 'tappa_readonly' AND s.setdatabase = 0 AND 'default_transaction_read_only=on' = ANY (s.setconfig)`); n != 1 {
		t.Errorf("(b) tappa_readonly's sessions do not start read-only")
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_class c CROSS JOIN (VALUES ('INSERT'), ('UPDATE')) v(p)
	        WHERE c.relnamespace = 'public'::regnamespace AND c.relkind = 'r'
	          AND has_any_column_privilege('tappa_readonly', c.oid, v.p)`); n != 0 {
		t.Errorf("(b) tappa_readonly can INSERT or UPDATE %d table/verb pairs", n)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_class c WHERE c.relnamespace = 'public'::regnamespace
	        AND c.relkind = 'r' AND has_table_privilege('tappa_readonly', c.oid, 'DELETE')`); n != 0 {
		t.Errorf("(b) tappa_readonly can DELETE from %d tables", n)
	}

	// (c)
	const guarded = `SELECT DISTINCT c.oid, c.relname FROM pg_class c
	        JOIN pg_trigger g ON g.tgrelid = c.oid AND NOT g.tgisinternal
	        JOIN pg_proc p ON p.oid = g.tgfoid
	       WHERE c.relnamespace = 'public'::regnamespace AND p.proname = 'tappa_forbid_mutation'
	         AND p.pronamespace = 'public'::regnamespace`
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM (`+guarded+`) s`); n < 12 {
		t.Fatalf("(c) anti-vacuity: %d tables carry the append-only guard, want at least 12", n)
	}
	rows, err := tx.Query(ctx, `SELECT s.relname FROM (`+guarded+`) s
	        WHERE has_any_column_privilege('tappa_readwrite', s.oid, 'UPDATE')
	           OR has_table_privilege('tappa_readwrite', s.oid, 'DELETE')
	           OR NOT has_table_privilege('tappa_readwrite', s.oid, 'INSERT')
	           OR NOT has_table_privilege('tappa_readonly', s.oid, 'SELECT')`)
	if err != nil {
		t.Fatal(err)
	}
	bad, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range bad {
		t.Errorf("(c) append-only table %s: tappa_readwrite must hold exactly SELECT, INSERT (and tappa_readonly SELECT)", r)
	}

	// (d)
	for _, q := range []struct{ what, sql string }{
		{"a key column is reachable", `SELECT count(*) FROM pg_attribute a CROSS JOIN (VALUES ('tappa_readonly'), ('tappa_readwrite')) r(n)
		    CROSS JOIN (VALUES ('SELECT'), ('INSERT'), ('UPDATE')) v(p)
		   WHERE a.attrelid = 'public.tags'::regclass AND a.attname IN ('aes_key_ref', 'app_key_ref')
		     AND has_column_privilege(r.n, 'public.tags'::regclass, a.attnum, v.p)`},
		{"tappa_readwrite can move last_ctr", `SELECT count(*) FROM pg_attribute a WHERE a.attrelid = 'public.tags'::regclass
		     AND a.attname = 'last_ctr' AND has_column_privilege('tappa_readwrite', 'public.tags'::regclass, a.attnum, 'UPDATE')`},
		{"a role can INSERT into or DELETE from tags", `SELECT count(*) FROM (VALUES ('tappa_readonly'), ('tappa_readwrite')) r(n)
		   WHERE has_any_column_privilege(r.n, 'public.tags'::regclass, 'INSERT') OR has_table_privilege(r.n, 'public.tags'::regclass, 'DELETE')`},
	} {
		if n := opInt(t, ctx, tx, q.sql); n != 0 {
			t.Errorf("(d) tags: %s (%d)", q.what, n)
		}
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_attribute a WHERE a.attrelid = 'public.tags'::regclass
	        AND a.attname IN ('uid', 'status') AND has_column_privilege('tappa_readonly', 'public.tags'::regclass, a.attnum, 'SELECT')`); n != 2 {
		t.Errorf("(d) control: tappa_readonly cannot read tags.uid and tags.status")
	}

	// (e)
	for _, q := range []struct{ what, sql string }{
		{"TRUNCATE", `SELECT count(*) FROM pg_class c CROSS JOIN (VALUES ('tappa_readonly'), ('tappa_readwrite')) r(n)
		   WHERE c.relnamespace = 'public'::regnamespace AND c.relkind = 'r' AND has_table_privilege(r.n, c.oid, 'TRUNCATE')`},
		{"CREATE on schema public", `SELECT count(*) FROM (VALUES ('tappa_readonly'), ('tappa_readwrite')) r(n)
		   WHERE has_schema_privilege(r.n, 'public', 'CREATE')`},
		{"EXECUTE on a SECURITY DEFINER function", `SELECT count(*) FROM pg_proc p CROSS JOIN (VALUES ('tappa_readonly'), ('tappa_readwrite')) r(n)
		   WHERE p.pronamespace = 'public'::regnamespace AND p.prosecdef AND has_function_privilege(r.n, p.oid, 'EXECUTE')`},
	} {
		if n := opInt(t, ctx, tx, q.sql); n != 0 {
			t.Errorf("(e) %s: %d", q.what, n)
		}
	}

	// (f)
	rows, err = tx.Query(ctx, `SELECT c.relname FROM pg_class c
	        WHERE c.relnamespace = 'public'::regnamespace AND c.relkind = 'r' AND c.relname NOT IN ('tags', 'goose_db_version')
	          AND c.oid NOT IN (SELECT oid FROM (`+guarded+`) s)
	          AND NOT (has_table_privilege('tappa_readonly', c.oid, 'SELECT')
	                   AND has_table_privilege('tappa_readwrite', c.oid, 'SELECT, INSERT, UPDATE, DELETE'))`)
	if err != nil {
		t.Fatal(err)
	}
	if bad, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		t.Fatal(err)
	}
	for _, r := range bad {
		t.Errorf("(f) mutable table %s: want SELECT for tappa_readonly and all four for tappa_readwrite", r)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM (VALUES ('INSERT'), ('UPDATE'), ('DELETE')) v(p)
	        WHERE has_table_privilege('tappa_readwrite', 'public.goose_db_version', v.p)`); n != 0 {
		t.Errorf("(f) tappa_readwrite can write goose's ledger (%d verbs)", n)
	}

	// (g)
	aiExec(t, ctx, tx, `CREATE TABLE public.zz_scoped_access_probe (x int)`)
	var ro, rw string
	if err := tx.QueryRow(ctx, `SELECT
	        concat_ws(',', CASE WHEN has_table_privilege('tappa_readonly', 'public.zz_scoped_access_probe', 'SELECT') THEN 'SELECT' END,
	                       CASE WHEN has_table_privilege('tappa_readonly', 'public.zz_scoped_access_probe', 'INSERT') THEN 'INSERT' END,
	                       CASE WHEN has_table_privilege('tappa_readonly', 'public.zz_scoped_access_probe', 'UPDATE') THEN 'UPDATE' END,
	                       CASE WHEN has_table_privilege('tappa_readonly', 'public.zz_scoped_access_probe', 'DELETE') THEN 'DELETE' END),
	        concat_ws(',', CASE WHEN has_table_privilege('tappa_readwrite', 'public.zz_scoped_access_probe', 'SELECT') THEN 'SELECT' END,
	                       CASE WHEN has_table_privilege('tappa_readwrite', 'public.zz_scoped_access_probe', 'INSERT') THEN 'INSERT' END,
	                       CASE WHEN has_table_privilege('tappa_readwrite', 'public.zz_scoped_access_probe', 'UPDATE') THEN 'UPDATE' END,
	                       CASE WHEN has_table_privilege('tappa_readwrite', 'public.zz_scoped_access_probe', 'DELETE') THEN 'DELETE' END)`).Scan(&ro, &rw); err != nil {
		t.Fatal(err)
	}
	if ro != "SELECT" || rw != "SELECT,INSERT" {
		t.Errorf("(g) a table created after the file ran: tappa_readonly %q (want SELECT), tappa_readwrite %q (want SELECT,INSERT)", ro, rw)
	}
}
