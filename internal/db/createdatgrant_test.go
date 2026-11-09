package db

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// createdatgrant_test.go -- migration 00088 (T163, ADR 0038): tappa_app's INSERT on transactions is
// a column list, every column but id and created_at. Both are the database's: created_at is what the
// debounce's server leg (ADR 0006, SecondsSinceLastRecordedTap) and M15's W (ADR 0032 §A.6) read as
// "when this row was written", and id breaks the (occurred_at, id) ties of the direction chain and
// the checkout classification (db/queries/anomalies.sql). A caller that could write either could
// choose an answer those readers give.
//
// What is pinned, and why each half:
//   - the catalogue, over EVERY column (has_column_privilege is effective: a membership or PUBLIC
//     grant counts), so a future column left out of the grant is red here before it is a 42501 on
//     every tap in production;
//   - the refusal as tappa_app, for each of the two columns, with a positive control (the same
//     statement without them lands) so the 42501 is the grant and not a broken row;
//   - the write side of the debounce is unchanged: created_at is still DEFAULT now() and an app
//     INSERT still gets its transaction's start (ADR 0006's write end), so the age the server leg
//     measures is what it was before 00088;
//   - every INSERT the shipped queries send (db/queries is identical on main: the running binary's
//     statements) names only granted columns -- the expand half of the deploy;
//   - the Down gives back 00005's table-level INSERT and the Up takes it again; neither names a
//     table lock (GRANT and REVOKE take none -- 00087's measurement, and 00088's header's).

const createdAtGrantFile = "00088_take_created_at_and_id_out_of_the_app_insert.sql"

// cgAppInsertColumns is the column list tappa_app holds INSERT on, in attnum order, as one string.
const cgAppInsertColumns = `
SELECT coalesce(string_agg(a.attname, ',' ORDER BY a.attnum), '')
  FROM pg_catalog.pg_attribute a
 WHERE a.attrelid = 'public.transactions'::regclass AND a.attnum > 0 AND NOT a.attisdropped
   AND has_column_privilege('tappa_app', 'public.transactions', a.attname, 'INSERT')`

// cgAllColumnsBut is every column of transactions but id and created_at, in attnum order.
const cgAllColumnsBut = `
SELECT string_agg(a.attname, ',' ORDER BY a.attnum)
  FROM pg_catalog.pg_attribute a
 WHERE a.attrelid = 'public.transactions'::regclass AND a.attnum > 0 AND NOT a.attisdropped
   AND a.attname NOT IN ('id', 'created_at')`

func cgString(t *testing.T, q opQuerier, sql string) string {
	t.Helper()
	var s string
	if err := q.QueryRow(t.Context(), sql).Scan(&s); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return s
}

// TestTransactions00088_TheAppInsertsEveryColumnButIDAndCreatedAt: on HEAD tappa_app's INSERT
// reaches exactly the table's columns minus id and created_at, and no table-level INSERT is left.
// It keeps its table-level SELECT (every INSERT RETURNs id and created_at) and gains nothing else.
func TestTransactions00088_TheAppInsertsEveryColumnButIDAndCreatedAt(t *testing.T) {
	ctx, tx := opTx(t)
	if got, want := cgString(t, tx, cgAppInsertColumns), cgString(t, tx, cgAllColumnsBut); got != want {
		t.Fatalf("tappa_app INSERTs on transactions\n %s\nwant every column but id and created_at\n %s", got, want)
	}
	for _, c := range []struct {
		sql  string
		want int64
	}{
		{`SELECT count(*) FROM pg_class WHERE oid = 'public.transactions'::regclass AND has_table_privilege('tappa_app', oid, 'INSERT')`, 0},
		{`SELECT count(*) FROM pg_class WHERE oid = 'public.transactions'::regclass AND has_table_privilege('tappa_app', oid, 'SELECT')`, 1},
		{`SELECT count(*) FROM pg_class WHERE oid = 'public.transactions'::regclass AND has_table_privilege('tappa_app', oid, 'UPDATE')`, 0},
		{`SELECT count(*) FROM pg_class WHERE oid = 'public.transactions'::regclass AND has_table_privilege('tappa_app', oid, 'DELETE')`, 0},
		{`SELECT count(*) FROM pg_attribute WHERE attrelid = 'public.transactions'::regclass AND attnum > 0 AND NOT attisdropped
		     AND has_column_privilege('tappa_app', 'public.transactions', attname, 'UPDATE')`, 0},
	} {
		if got := opInt(t, ctx, tx, c.sql); got != c.want {
			t.Errorf("%s = %d, want %d", c.sql, got, c.want)
		}
	}
}

// TestTransactions00088_TheAppCannotWriteCreatedAtOrID: as tappa_app in its own tenant's context, an
// INSERT that names created_at (a back-dated predecessor -- the shape that would age a row past the
// 60 s brake) or id is 42501, before any row exists; the same INSERT without them lands, and the
// created_at it gets is its transaction's start, which is what DEFAULT now() wrote before 00088.
func TestTransactions00088_TheAppCannotWriteCreatedAtOrID(t *testing.T) {
	ctx, tx := opTx(t)
	tenant := aiTenant(t, ctx, tx)
	const base = `INSERT INTO transactions (tenant_id, occurred_at, verdict, channel%s) VALUES ($1, now(), 'reject', 'qr'%s)`
	for _, c := range []struct {
		name, col, val string
		arg            any
	}{
		{"created_at, back-dated an hour", ", created_at", ", now() - interval '1 hour'", nil},
		{"created_at, the transaction's own now()", ", created_at", ", now()", nil},
		{"id", ", id", ", $2", uuid.New()},
	} {
		sql := strings.Replace(strings.Replace(base, "%s", c.col, 1), "%s", c.val, 1)
		args := []any{tenant}
		if c.arg != nil {
			args = append(args, c.arg)
		}
		err := aiApp(t, ctx, tx, tenant, func(sp pgx.Tx) error {
			_, e := sp.Exec(ctx, sql, args...)
			return e
		})
		opWant(t, err, sqlstateInsufficientPrivi, "tappa_app naming "+c.name)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM transactions WHERE tenant_id = $1`, tenant); n != 0 {
		t.Fatalf("%d rows landed from the refused statements", n)
	}

	// Positive control and the write side of the debounce: the database's value, the transaction's start.
	var atStart bool
	if err := aiApp(t, ctx, tx, tenant, func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `INSERT INTO transactions (tenant_id, occurred_at, verdict, channel)
		                         VALUES ($1, now(), 'reject', 'qr') RETURNING created_at = now()`, tenant).Scan(&atStart)
	}); err != nil {
		t.Fatalf("positive control: the same INSERT without id and created_at: %v", err)
	}
	if !atStart {
		t.Error("an app INSERT's created_at is not its transaction's start: the debounce's DB-age leg would measure something else (ADR 0006)")
	}
	if def := cgString(t, tx, `SELECT pg_get_expr(d.adbin, d.adrelid) FROM pg_attrdef d JOIN pg_attribute a ON a.attrelid = d.adrelid AND a.attnum = d.adnum
	                           WHERE d.adrelid = 'public.transactions'::regclass AND a.attname = 'created_at'`); def != "now()" {
		t.Errorf("transactions.created_at DEFAULT is %q, want now() (00005; 00088 changes the grant, not the value)", def)
	}
}

// cgInsertList finds every INSERT INTO transactions in the comment-stripped SQL and returns its
// column lists; a statement without one is returned as "" (it would need table-level INSERT).
var cgInsertList = regexp.MustCompile(`(?is)\binsert\s+into\s+(?:public\.)?"?transactions"?\s*(\([^)]*\))?`)

// TestTransactions00088_EveryShippedInsertNamesOnlyGrantedColumns is the expand half of the deploy:
// db/queries is the running binary's too (identical on main at 00088's writing), and its INSERTs
// into transactions -- four of them -- must name only columns tappa_app holds INSERT on. A
// privilege is checked per named column, so this is the whole of what 00088 can refuse them.
func TestTransactions00088_EveryShippedInsertNamesOnlyGrantedColumns(t *testing.T) {
	_, tx := opTx(t)
	granted := strings.Split(cgString(t, tx, cgAppInsertColumns), ",")
	files, err := filepath.Glob(filepath.Join("..", "..", "db", "queries", "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("db/queries: %v (%d files)", err, len(files))
	}
	statements := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var clean strings.Builder
		for _, line := range strings.Split(string(b), "\n") {
			if i := strings.Index(line, "--"); i >= 0 {
				line = line[:i]
			}
			clean.WriteString(line + "\n")
		}
		for _, m := range cgInsertList.FindAllStringSubmatch(clean.String(), -1) {
			statements++
			if m[1] == "" {
				t.Errorf("%s: an INSERT INTO transactions with no column list -- it needs a table-level INSERT, which 00088 took", filepath.Base(f))
				continue
			}
			for _, col := range strings.Split(strings.Trim(m[1], "()"), ",") {
				col = strings.Trim(strings.TrimSpace(col), `"`)
				if !slices.Contains(granted, col) {
					t.Errorf("%s: an INSERT INTO transactions names %q, which tappa_app may not insert (00088)", filepath.Base(f), col)
				}
			}
		}
	}
	if statements != 4 {
		t.Errorf("db/queries holds %d INSERTs into transactions, want the four 00088's header reads (InsertTransaction and the three manual ones) -- read the new one against the grant", statements)
	}
}

// TestTransactions00088_TheDownGivesBackTheTableInsertAndTheUpTakesItAgain: both sections open with
// the lock_timeout and name no table lock; the Down leaves tappa_app 00005's table-level INSERT and no
// column grant (a table REVOKE clears them), under which a back-dated created_at lands again; the Up
// takes it back to HEAD.
//
// Since T152 the walk is wrapped in 00095's Down and Up, goose's own order (ADR 0038 "Bedel"): 00095
// adds five columns 00088's list does not name, so 00088's Up at HEAD would refuse them -- its own
// DO block, the guard working. transactions is locked first for 00095's Down (lockForDowns).
func TestTransactions00088_TheDownGivesBackTheTableInsertAndTheUpTakesItAgain(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, createdAtGrantFile)
	for name, section := range map[string]string{"Up": up, "Down": down} {
		var first string
		for _, line := range strings.Split(section, "\n")[1:] {
			if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "--") {
				first = line
				break
			}
		}
		if first != "SET LOCAL lock_timeout = '1s';" {
			t.Errorf("00088's %s opens with %q, want the lock_timeout", name, first)
		}
	}
	if tables := downTables(up + down); len(tables) > 0 {
		t.Fatalf("00088 names a table lock: %v", tables)
	}
	lockForDowns(t, ctx, tx, []string{"transactions"})
	head := cgString(t, tx, cgAppInsertColumns)
	tenant := aiTenant(t, ctx, tx)
	backDated := func(q pgx.Tx) error {
		return aiApp(t, ctx, q, tenant, func(sp pgx.Tx) error {
			_, e := sp.Exec(ctx, `INSERT INTO transactions (tenant_id, occurred_at, created_at, verdict, channel)
			                      VALUES ($1, now(), now() - interval '1 hour', 'reject', 'qr')`, tenant)
			return e
		})
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		ssDown(t, ctx, sp)
		opRunSection(t, ctx, sp, down, "00088 Down")
		if opInt(t, ctx, sp, `SELECT count(*) FROM pg_class WHERE oid = 'public.transactions'::regclass AND has_table_privilege('tappa_app', oid, 'INSERT')`) != 1 {
			t.Fatal("after the Down tappa_app holds no table-level INSERT on transactions; want 00005's")
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM pg_attribute WHERE attrelid = 'public.transactions'::regclass AND attacl::text LIKE '%tappa_app=%'`); n != 0 {
			t.Fatalf("after the Down %d column ACLs still name tappa_app", n)
		}
		if err := backDated(sp); err != nil {
			t.Fatalf("after the Down a back-dated created_at as tappa_app: %v, want it to land (00005's shape)", err)
		}
		opRunSection(t, ctx, sp, up, "00088 Up")
		ssUp(t, ctx, sp)
		if got := cgString(t, sp, cgAppInsertColumns); got != head {
			t.Fatalf("Down then Up: tappa_app INSERTs %s, want HEAD's %s", got, head)
		}
		opWant(t, backDated(sp), sqlstateInsufficientPrivi, "a back-dated created_at after Down then Up")
	})
}
