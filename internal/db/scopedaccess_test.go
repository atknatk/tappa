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
// that carries a tappa_forbid_mutation row guard -- at least twelve -- tappa_readwrite holds no
// UPDATE and no DELETE (§4.3); (d) on tags neither role touches aes_key_ref or app_key_ref,
// tappa_readwrite cannot move last_ctr (which tappa_app CAN), insert or delete (§4.4, §4.7); (e) no
// TRUNCATE, no CREATE on public, no EXECUTE on any SECURITY DEFINER function; (f) every other table
// is readable by both, and tappa_readwrite's INSERT/UPDATE/DELETE equal tappa_app's own, column by
// column -- so the eight tables only a definer writes (security review, 2026-11-17) take no write;
// (g) a table created AFTER the file ran gets SELECT and nothing more for either role; (h) the
// file's own postcondition refuses to finish if tappa_app is ever granted a write on a definer-only
// table (the file then would copy it); (i) what BYPASSRLS would add beyond tappa_app is withheld:
// UPDATE of id/tenant_id anywhere, and DELETE where a RESTRICTIVE policy narrows it
// (oauth_codes, oauth_tokens, webhook_deliveries).
//
// MUTANT (measured, 2026-11-17, second model): the table-level copy loop's `x.grantee =
// 'tappa_app'::regrole` removed (every grantee's verbs copied) -> the file's postcondition (1)
// aborts the run ("writes columns tappa_app cannot: tenants.plan ..."); `'last_ctr'` dropped from
// the UPDATE exclusion -> postcondition (4) aborts it; a default INSERT for tappa_readwrite added
// AND the default REVOKE line weakened -> red at (g); the column-level copy loop emptied -> red at
// (c) (audit_log, punch_claims, punch_claim_decisions, transactions: tappa_app's INSERT there is
// column-level) and at (f). Third round: the `id`/`tenant_id` exclusion removed -> postcondition (7)
// aborts the run ("can UPDATE a key or tenant column: departments.id, ..."); the restrictive-policy
// filter of the INSERT/DELETE loop removed -> postcondition (8) aborts it.
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
	           OR has_any_column_privilege('tappa_readwrite', s.oid, 'INSERT') <> has_any_column_privilege('tappa_app', s.oid, 'INSERT')
	           OR NOT has_table_privilege('tappa_readonly', s.oid, 'SELECT')`)
	if err != nil {
		t.Fatal(err)
	}
	bad, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range bad {
		t.Errorf("(c) append-only table %s: tappa_readwrite must hold no UPDATE/DELETE and INSERT exactly where tappa_app does (and tappa_readonly SELECT)", r)
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
	rows, err = tx.Query(ctx, `SELECT c.relname || '.' || a.attname || ' ' || v.p
	        FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
	       CROSS JOIN (VALUES ('INSERT'), ('UPDATE')) v(p)
	       WHERE c.relnamespace = 'public'::regnamespace AND c.relkind = 'r' AND c.relname NOT IN ('tags', 'goose_db_version')
	         AND has_column_privilege('tappa_readwrite', c.oid, a.attnum, v.p)
	             <> (has_column_privilege('tappa_app', c.oid, a.attnum, v.p)
	                 AND NOT (v.p = 'UPDATE' AND a.attname IN ('id', 'tenant_id'))
	                 AND NOT EXISTS (SELECT 1 FROM pg_policy pol WHERE pol.polrelid = c.oid AND NOT pol.polpermissive
	                                  AND pol.polcmd IN ('*', CASE v.p WHEN 'INSERT' THEN 'a' ELSE 'w' END)))
	      UNION ALL
	      SELECT c.relname || ' DELETE' FROM pg_class c
	       WHERE c.relnamespace = 'public'::regnamespace AND c.relkind = 'r' AND c.relname NOT IN ('tags', 'goose_db_version')
	         AND has_table_privilege('tappa_readwrite', c.oid, 'DELETE')
	             <> (has_table_privilege('tappa_app', c.oid, 'DELETE')
	                 AND NOT EXISTS (SELECT 1 FROM pg_policy pol WHERE pol.polrelid = c.oid AND NOT pol.polpermissive
	                                  AND pol.polcmd IN ('*', 'd')))
	      UNION ALL
	      SELECT c.relname || ' SELECT' FROM pg_class c
	       WHERE c.relnamespace = 'public'::regnamespace AND c.relkind = 'r' AND c.relname <> 'tags'
	         AND NOT (has_table_privilege('tappa_readonly', c.oid, 'SELECT') AND has_table_privilege('tappa_readwrite', c.oid, 'SELECT'))`)
	if err != nil {
		t.Fatal(err)
	}
	if bad, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		t.Fatal(err)
	}
	for _, r := range bad {
		t.Errorf("(f) %s: tappa_readwrite's write must equal tappa_app's and both roles must read", r)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_class WHERE relnamespace = 'public'::regnamespace AND relkind = 'r'
	        AND relname IN ('feed_counters', 'holding_members', 'holdings', 'platform_admins', 'platform_sessions',
	                        'oauth_clients', 'operator_read_tickets', 'admin_identities')`); n != 8 {
		t.Fatalf("(f) anti-vacuity: %d of the eight definer-only tables exist", n)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_class c CROSS JOIN (VALUES ('INSERT'), ('UPDATE')) v(p)
	        WHERE c.relnamespace = 'public'::regnamespace
	          AND c.relname IN ('feed_counters', 'holding_members', 'holdings', 'platform_admins', 'platform_sessions',
	                            'oauth_clients', 'operator_read_tickets', 'admin_identities', 'goose_db_version')
	          AND (has_any_column_privilege('tappa_readwrite', c.oid, v.p) OR has_table_privilege('tappa_readwrite', c.oid, 'DELETE'))`); n != 0 {
		t.Errorf("(f) tappa_readwrite can write a definer-only table or goose's ledger (%d)", n)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM (VALUES ('locations', 'UPDATE'), ('locations', 'DELETE'), ('employees', 'INSERT')) v(r, p)
	        WHERE CASE WHEN v.p = 'DELETE' THEN has_table_privilege('tappa_readwrite', ('public.' || v.r)::regclass, 'DELETE')
	                   ELSE has_any_column_privilege('tappa_readwrite', ('public.' || v.r)::regclass, v.p) END`); n != 3 {
		t.Errorf("(f) control: tappa_readwrite lost a write tappa_app holds (locations UPDATE/DELETE, employees INSERT): %d of 3", n)
	}

	// (i) what BYPASSRLS would add on top of tappa_app's grants is withheld: UPDATE of id/tenant_id
	// (tappa_app's is bounded by RLS's WITH CHECK, this role's would move a row to another tenant),
	// and a verb a RESTRICTIVE policy narrows (BYPASSRLS skips it).
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0
	        WHERE c.relnamespace = 'public'::regnamespace AND c.relkind = 'r' AND a.attname IN ('id', 'tenant_id')
	          AND has_column_privilege('tappa_app', c.oid, a.attnum, 'UPDATE')`); n < 10 {
		t.Fatalf("(i) anti-vacuity: tappa_app may UPDATE %d id/tenant_id columns, want at least 10", n)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0
	        WHERE c.relnamespace = 'public'::regnamespace AND c.relkind = 'r' AND a.attname IN ('id', 'tenant_id')
	          AND has_column_privilege('tappa_readwrite', c.oid, a.attnum, 'UPDATE')`); n != 0 {
		t.Errorf("(i) tappa_readwrite can UPDATE %d id/tenant_id columns", n)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_policy pol JOIN pg_class c ON c.oid = pol.polrelid
	        WHERE c.relnamespace = 'public'::regnamespace AND NOT pol.polpermissive AND pol.polcmd = 'd'
	          AND c.relname IN ('oauth_codes', 'oauth_tokens', 'webhook_deliveries')`); n != 3 {
		t.Fatalf("(i) anti-vacuity: %d of the three restrictive DELETE policies exist", n)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM (VALUES ('oauth_codes'), ('oauth_tokens'), ('webhook_deliveries')) v(r)
	        WHERE has_table_privilege('tappa_readwrite', ('public.' || v.r)::regclass, 'DELETE')`); n != 0 {
		t.Errorf("(i) tappa_readwrite can DELETE from %d of the tables a RESTRICTIVE DELETE policy narrows", n)
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
	if ro != "SELECT" || rw != "SELECT" {
		t.Errorf("(g) a table created after the file ran: tappa_readonly %q, tappa_readwrite %q (want SELECT for both)", ro, rw)
	}

	// (h) inside a savepoint, so the refusal rolls back only itself.
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	aiExec(t, ctx, sp, `GRANT UPDATE ON public.feed_counters TO tappa_app`)
	_, err = sp.Conn().PgConn().Exec(ctx, sql).ReadAll()
	if err == nil || !strings.Contains(err.Error(), "definer-only table") {
		t.Errorf("(h) with tappa_app granted UPDATE on feed_counters, the file must refuse in its postcondition; got %v", err)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}
