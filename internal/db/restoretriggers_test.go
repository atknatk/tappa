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
//     and replica-only 'R' are, by name; its definition is `public.`-qualified, as pg_dump prints it.
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
}
