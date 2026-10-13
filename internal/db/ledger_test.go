package db

// ledger_test.go -- migration 00040 (M11 API-1a, handed over by the API-1 review): goose's
// ledger, goose_db_version, is the migration role's alone. Owner session, everything rolled back
// (opTx).

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

const op00040File = "00040_close_the_migration_ledger_to_the_app.sql"

// ledgerInSavepoint runs body in a savepoint of tx that is rolled back when body returns.
func ledgerInSavepoint(t *testing.T, tx pgx.Tx, body func(sp pgx.Tx)) {
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
	body(sp)
}

// TestMigrationLedger_AppCannotForgeAVersion is the API-1 review's hand-over, closed by 00040:
// tappa_app holds NOTHING on goose_db_version and its sequence -- INSERT alone let it mark a
// future migration as applied (measured: a forged row, then `goose up` said "no migrations to
// run"). On the catalogue no role but the owner holds any privilege on either; as tappa_app the
// forging INSERT, a read, a rewrite, an erase and the sequence are 42501. The Down keeps the
// ledger closed and closes it again over a hand-made re-grant; the Up's postcondition refuses a
// ledger tappa_app still reaches through a role it is a member of (a REVOKE cannot take that back).
func TestMigrationLedger_AppCannotForgeAVersion(t *testing.T) {
	ctx, tx := opTx(t)
	reach := func(q opQuerier) string {
		t.Helper()
		var acl string
		if err := q.QueryRow(ctx, `
			SELECT coalesce(string_agg(DISTINCT c.relname || ':' || CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END, ','), '')
			  FROM pg_class c, aclexplode(coalesce(c.relacl, acldefault((CASE WHEN c.relkind = 'S' THEN 's' ELSE 'r' END)::"char", c.relowner))) AS a
			 WHERE c.oid IN ('public.goose_db_version'::regclass, 'public.goose_db_version_id_seq'::regclass)
			   AND a.grantee <> c.relowner`).Scan(&acl); err != nil {
			t.Fatal(err)
		}
		return acl
	}
	if got := reach(tx); got != "" {
		t.Errorf("the ledger is reached by %s; want its owner alone", got)
	}
	version := opInt(t, ctx, tx, `SELECT max(version_id) FROM goose_db_version`)
	for _, c := range []struct{ what, sql string }{
		{"forging a version", `INSERT INTO public.goose_db_version (version_id, is_applied) VALUES ($1, true)`},
		{"reading the ledger", `SELECT count(*) FROM public.goose_db_version WHERE version_id <> $1`},
		{"rewriting a row", `UPDATE public.goose_db_version SET is_applied = false WHERE version_id = $1`},
		{"erasing a row", `DELETE FROM public.goose_db_version WHERE version_id = $1`},
		{"drawing the ledger's sequence", `SELECT nextval('public.goose_db_version_id_seq') + $1`},
	} {
		opWant(t, opExecAs(t, ctx, tx, "tappa_app", c.sql, version+1), sqlstateInsufficientPrivi, "tappa_app "+c.what)
	}
	up, down := opMigrationSections(t, op00040File)
	ledgerInSavepoint(t, tx, func(sp pgx.Tx) {
		if _, err := sp.Exec(ctx, `GRANT INSERT ON goose_db_version TO tappa_app`); err != nil {
			t.Fatal(err)
		}
		opRunSection(t, ctx, sp, down, "00040 Down over a hand-made re-grant")
		if got := reach(sp); got != "" {
			t.Errorf("after 00040's Down the ledger is reached by %s; the Down keeps it closed", got)
		}
		opRunSection(t, ctx, sp, up, "00040 Up again")
		if got := reach(sp); got != "" {
			t.Errorf("after 00040's Up again the ledger is reached by %s", got)
		}
	})
	ledgerInSavepoint(t, tx, func(sp pgx.Tx) {
		for _, s := range []string{`GRANT INSERT ON goose_db_version TO tappa_resolver`, `GRANT tappa_resolver TO tappa_app`} {
			if _, err := sp.Exec(ctx, s); err != nil {
				t.Fatal(err)
			}
		}
		_, err := sp.Conn().PgConn().Exec(ctx, up).ReadAll()
		code, msg := opCode(err)
		if code != sqlstatePrerequisiteState || !strings.Contains(msg, "00040") || !strings.Contains(msg, "still reaches") {
			t.Errorf("00040's Up with the ledger reached through a membership answered %q %q, want 55000 naming 00040", code, msg)
		}
	})
}
