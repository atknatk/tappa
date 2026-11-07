package db

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// restoreTriggerSQL cuts one shell variable's SQL out of scripts/pg-restore-verify.sh: the text the
// operator's restore runs, not a copy of it.
func restoreTriggerSQL(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "pg-restore-verify.sh"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?s)\n` + name + `="(.*?)"\n`).FindStringSubmatch(string(b))
	if m == nil {
		t.Fatalf("scripts/pg-restore-verify.sh carries no %s=\"...\"", name)
	}
	return m[1]
}

// TestRestoreVerify_TriggerQueries (T100) runs section 5b's two queries, cut from the script, on
// PostgreSQL, in a transaction that is rolled back:
//   - the migrated schema has no trigger that is off, and its definitions name a row append-only
//     trigger and 00033's `at` trigger (the two kinds the section was blind to);
//   - a scratch table's trigger is seen in each state: origin 'O' and always 'A' are not listed, 'D'
//     and replica-only 'R' are, by name; its definition is `public.`-qualified, as pg_dump prints it;
//   - an INTERNAL trigger is seen too: DISABLE TRIGGER ALL on a table with a foreign key and no
//     trigger of its own turns off only the foreign key's, and the query lists it;
//   - the floor: all of trunc_tables have their row guard and tags its counter trigger; a row guard
//     on UPDATE alone, switched off, with a WHEN or with UPDATE OF is not one;
//   - the floor and section 5 read the guard function in public only: one of the same name in another
//     schema, refusing nothing, is not a guard (T215).
//
// What this does not run is the chain itself, a real dump restored the way the runbook says: that is
// scripts/restore-chain-check.sh (`make restore-check`, a CI step since T215), whose one healthy run
// every check here must pass. This test keeps the states that run never produces.
func TestRestoreVerify_TriggerQueries(t *testing.T) {
	off := restoreTriggerSQL(t, "TRIGGERS_OFF_SQL")
	defs := restoreTriggerSQL(t, "TRIGGER_DEFS_SQL")
	_, query, ok := strings.Cut(defs, "; ")
	if !ok || !strings.HasPrefix(defs, "SET search_path = ''") {
		t.Fatalf("TRIGGER_DEFS_SQL no longer starts with the empty search_path: %q", defs)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn, err := pgx.Connect(ctx, opOwnerDSN(t))
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}
	defer func() { _ = conn.Close(context.Background()) }()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	lines := func(sql string) []string {
		t.Helper()
		rows, err := tx.Query(ctx, sql)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		out, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		return out
	}
	if _, err := tx.Exec(ctx, `SET LOCAL search_path = ''`); err != nil {
		t.Fatal(err)
	}
	if got := lines(off); len(got) != 0 {
		t.Errorf("on the migrated schema these triggers are off: %v", got)
	}
	all := strings.Join(lines(query), "\n")
	for _, want := range []string{
		"CREATE TRIGGER audit_log_no_mutation BEFORE DELETE OR UPDATE ON public.audit_log FOR EACH ROW EXECUTE FUNCTION public.tappa_forbid_mutation();",
		"CREATE TRIGGER operator_audit_log_at_is_the_wall_clock BEFORE INSERT ON public.operator_audit_log FOR EACH ROW EXECUTE FUNCTION public.tappa_audit_at_is_the_wall_clock();",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("the definitions do not carry %q", want)
		}
	}

	if strings.Contains(all, "RI_ConstraintTrigger") {
		t.Error("the definitions list a foreign key's internal trigger, which is no CREATE TRIGGER line of a dump: every restore would differ")
	}

	if _, err := tx.Exec(ctx, `
		CREATE TABLE public.zz_t100 (a int);
		CREATE TRIGGER zz_t100_no_mutation BEFORE UPDATE ON public.zz_t100 FOR EACH ROW EXECUTE FUNCTION public.tappa_forbid_mutation()`); err != nil {
		t.Fatalf("the scratch table: %v", err)
	}
	const name = "zz_t100.zz_t100_no_mutation"
	for _, s := range []struct {
		alter string
		want  []string
	}{
		{"ENABLE TRIGGER", nil},
		{"ENABLE ALWAYS TRIGGER", nil},
		{"DISABLE TRIGGER", []string{name + " tgenabled=D"}},
		{"ENABLE REPLICA TRIGGER", []string{name + " tgenabled=R"}},
	} {
		if _, err := tx.Exec(ctx, `ALTER TABLE public.zz_t100 `+s.alter+` zz_t100_no_mutation`); err != nil {
			t.Fatalf("%s: %v", s.alter, err)
		}
		if got := lines(off); !slices.Equal(got, s.want) {
			t.Errorf("after %s the query lists %v, want %v", s.alter, got, s.want)
		}
	}
	if all := strings.Join(lines(query), "\n"); !strings.Contains(all, "ON public.zz_t100 ") {
		t.Errorf("the definitions do not list a disabled trigger: a disabled trigger would read as a lost one")
	}
	if _, err := tx.Exec(ctx, `ALTER TABLE public.zz_t100 ENABLE TRIGGER zz_t100_no_mutation`); err != nil {
		t.Fatal(err)
	}

	if _, err := tx.Exec(ctx, `
		CREATE TABLE public.zz_t100_parent (a int PRIMARY KEY);
		CREATE TABLE public.zz_t100_child (a int REFERENCES public.zz_t100_parent (a));
		ALTER TABLE public.zz_t100_child DISABLE TRIGGER ALL`); err != nil {
		t.Fatalf("the foreign key's table: %v", err)
	}
	got := lines(off)
	if len(got) == 0 {
		t.Error("after DISABLE TRIGGER ALL on a table whose only triggers are its foreign key's, the query lists none")
	}
	for _, g := range got {
		if !strings.HasPrefix(g, "zz_t100_child.RI_ConstraintTrigger_") || !strings.HasSuffix(g, " tgenabled=D") {
			t.Errorf("the query lists %q, want the foreign key's disabled triggers", g)
		}
	}
	if _, err := tx.Exec(ctx, `ALTER TABLE public.zz_t100_child ENABLE TRIGGER ALL`); err != nil {
		t.Fatal(err)
	}

	floor := restoreTriggerSQL(t, "FLOOR_ROW_GUARDS_SQL")
	counter := restoreTriggerSQL(t, "FLOOR_COUNTER_SQL")
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "pg-restore-verify.sh"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^trunc_tables="([^"]*)"`).FindSubmatch(script)
	if m == nil {
		t.Fatal(`the script has no trunc_tables="..."`)
	}
	guarded := lines(floor)
	for _, table := range strings.Fields(string(m[1])) {
		if !slices.Contains(guarded, table) {
			t.Errorf("the floor does not find %s's row guard on the migrated schema", table)
		}
	}
	if got := lines(counter); !slices.Equal(got, []string{"1"}) {
		t.Errorf("the floor's counter query answers %v, want 1", got)
	}
	if slices.Contains(guarded, "zz_t100") {
		t.Errorf("the floor counts a row guard on UPDATE alone: DELETE would pass it")
	}
	if _, err := tx.Exec(ctx, `
		CREATE OR REPLACE TRIGGER zz_t100_no_mutation BEFORE UPDATE OR DELETE ON public.zz_t100 FOR EACH ROW EXECUTE FUNCTION public.tappa_forbid_mutation()`); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(lines(floor), "zz_t100") {
		t.Errorf("the floor does not see an UPDATE OR DELETE row guard")
	}
	for what, def := range map[string]string{
		"WHEN (false)": `BEFORE UPDATE OR DELETE ON public.zz_t100 FOR EACH ROW WHEN (false)`,
		"UPDATE OF a":  `BEFORE UPDATE OF a OR DELETE ON public.zz_t100 FOR EACH ROW`,
	} {
		if _, err := tx.Exec(ctx, `CREATE OR REPLACE TRIGGER zz_t100_no_mutation `+def+` EXECUTE FUNCTION public.tappa_forbid_mutation()`); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if slices.Contains(lines(floor), "zz_t100") {
			t.Errorf("the floor counts a guard with %s, which refuses nothing or almost nothing", what)
		}
	}
	if _, err := tx.Exec(ctx, `
		CREATE OR REPLACE TRIGGER zz_t100_no_mutation BEFORE UPDATE OR DELETE ON public.zz_t100 FOR EACH ROW EXECUTE FUNCTION public.tappa_forbid_mutation()`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `ALTER TABLE public.zz_t100 DISABLE TRIGGER zz_t100_no_mutation`); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(lines(floor), "zz_t100") {
		t.Errorf("the floor counts a disabled row guard")
	}

	// T215: the guard function is read by name AND schema. A same-named function in another schema
	// that refuses nothing passed the floor (T213's last look, zz_sec.tappa_forbid_mutation); section
	// 5's TRUNCATE query had the same blind spot.
	m = regexp.MustCompile(`(?s)psql_to "\$TMP/have\.truncguards" "(.*?)"\n`).FindSubmatch(script)
	if m == nil {
		t.Fatal(`the script has no psql_to "$TMP/have.truncguards" "..."`)
	}
	truncGuards := string(m[1])
	if _, err := tx.Exec(ctx, `
		ALTER TABLE public.zz_t100 ENABLE TRIGGER zz_t100_no_mutation;
		CREATE SCHEMA zz_t215;
		CREATE FUNCTION zz_t215.tappa_forbid_mutation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END $$;
		CREATE FUNCTION zz_t215.tappa_forbid_counter_rewind() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;
		CREATE TRIGGER zz_t100_no_truncate BEFORE TRUNCATE ON public.zz_t100 FOR EACH STATEMENT EXECUTE FUNCTION public.tappa_forbid_mutation()`); err != nil {
		t.Fatalf("the other schema: %v", err)
	}
	if !slices.Contains(lines(floor), "zz_t100") || !slices.Contains(lines(truncGuards), "zz_t100") {
		t.Fatal("CONTROL FAILED: bound to public's function, the scratch table's guards are not seen")
	}
	if _, err := tx.Exec(ctx, `
		CREATE OR REPLACE TRIGGER zz_t100_no_mutation BEFORE UPDATE OR DELETE ON public.zz_t100 FOR EACH ROW EXECUTE FUNCTION zz_t215.tappa_forbid_mutation();
		CREATE OR REPLACE TRIGGER zz_t100_no_truncate BEFORE TRUNCATE ON public.zz_t100 FOR EACH STATEMENT EXECUTE FUNCTION zz_t215.tappa_forbid_mutation()`); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(lines(floor), "zz_t100") {
		t.Errorf("the floor counts a row guard bound to zz_t215.tappa_forbid_mutation, which refuses nothing")
	}
	if slices.Contains(lines(truncGuards), "zz_t100") {
		t.Errorf("section 5 counts a TRUNCATE guard bound to zz_t215.tappa_forbid_mutation, which refuses nothing")
	}
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '10s';
		CREATE OR REPLACE TRIGGER tags_counter_monotonic BEFORE UPDATE ON public.tags FOR EACH ROW EXECUTE FUNCTION zz_t215.tappa_forbid_counter_rewind()`); err != nil {
		t.Fatal(err)
	}
	if got := lines(counter); !slices.Equal(got, []string{"0"}) {
		t.Errorf("the floor's counter query answers %v on tags_counter_monotonic bound to zz_t215's function, want 0", got)
	}
	if _, err := tx.Exec(ctx, `
		CREATE OR REPLACE TRIGGER tags_counter_monotonic BEFORE UPDATE ON public.tags FOR EACH ROW EXECUTE FUNCTION public.tappa_forbid_counter_rewind()`); err != nil {
		t.Fatal(err)
	}
	if got := lines(counter); !slices.Equal(got, []string{"1"}) {
		t.Fatalf("CONTROL FAILED: tags_counter_monotonic bound to public's function again, the counter query answers %v", got)
	}

	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '10s'; ALTER TABLE public.tags DISABLE TRIGGER tags_counter_monotonic`); err != nil {
		t.Fatal(err)
	}
	if got := lines(counter); !slices.Equal(got, []string{"0"}) {
		t.Errorf("the floor's counter query answers %v on a disabled trigger, want 0", got)
	}
}

// TestRestoreVerify_TheColumnGrantsSeeTheSystemColumns (T214) runs section 3's actual-grants query,
// cut from the script, on PostgreSQL, in a transaction that is rolled back. The blind spot in one
// line: information_schema.column_privileges has no xmin, so the dump's SELECT (xmin) read as MISSING
// on every restore from goose 74 on, with the privilege in place.
//   - the two grants 00074 and 00078 make are in the answer;
//   - REVOKE takes one out, the verifier's `-`; a grant the dump does not have comes in, its `+`;
//   - an ordinary column's grant still arrives through the first arm.
//
// What it does not prove is the chain from a real dump to a real restore: since T215 CI runs that,
// scripts/restore-chain-check.sh (`make restore-check`), and with this arm removed that run is red
// (measured, T215: `- admin_users tappa_identitydefiner xmin SELECT`). This test keeps `-`, `+` and
// the owner's exclusion, which a healthy run never shows.
func TestRestoreVerify_TheColumnGrantsSeeTheSystemColumns(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "pg-restore-verify.sh"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?s)psql_to "\$TMP/have\.colgrants" "(.*?)"\n`).FindSubmatch(b)
	if m == nil {
		t.Fatal(`scripts/pg-restore-verify.sh has no psql_to "$TMP/have.colgrants" "..."`)
	}
	query := string(m[1])
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn, err := pgx.Connect(ctx, opOwnerDSN(t))
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}
	defer func() { _ = conn.Close(context.Background()) }()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	grants := func() []string {
		t.Helper()
		rows, err := tx.Query(ctx, query)
		if err != nil {
			t.Fatalf("the query: %v", err)
		}
		var out []string
		for rows.Next() {
			var a, b, c, d string
			if err := rows.Scan(&a, &b, &c, &d); err != nil {
				t.Fatal(err)
			}
			out = append(out, a+" "+b+" "+c+" "+d)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	const tenantsX, adminsX = "tenants tappa_identitydefiner xmin SELECT", "admin_users tappa_identitydefiner xmin SELECT"
	got := grants()
	for _, want := range []string{tenantsX, adminsX, "tenants tappa_identitydefiner vat_number SELECT"} {
		if !slices.Contains(got, want) {
			t.Errorf("the answer does not carry %q", want)
		}
	}
	if _, err := tx.Exec(ctx, `REVOKE SELECT (xmin) ON public.tenants FROM tappa_identitydefiner`); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(grants(), tenantsX) {
		t.Errorf("a revoked grant is still in the answer: the verifier could not say `-`")
	}
	const extra = "tags tappa_app xmin SELECT"
	if _, err := tx.Exec(ctx, `GRANT SELECT (xmin) ON public.tags TO tappa_app`); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(grants(), extra) {
		t.Errorf("a grant the dump does not have is not in the answer: the verifier could not say `+`")
	}
	if _, err := tx.Exec(ctx, `GRANT SELECT (xmin) ON public.tags TO tappa_owner`); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(grants(), "tags tappa_owner xmin SELECT") {
		t.Errorf("the table owner's own column grant is in the answer; the query's first arm leaves the owner out, and so does the dump's expected set")
	}
}
