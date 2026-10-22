package db

// directionsource_test.go -- M15-09 (migrations 00065 and 00066, ADR 0032 §A.5, §B.4, "Geri
// alma"): transactions.direction_source, its two NOT VALID CHECKs, and the file that validates
// them and 00064's.
//
// App-side writes run as tappa_app under RLS (appDB) on a fixture tenant of their own and are
// rolled back; nothing here commits a row with a direction_source -- such a row would make
// 00065's Down refuse for every later run (that refusal is itself tested, inside a rolled-back
// savepoint). The migration walks run in an owner transaction (opTx) that is rolled back.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/store"
)

const (
	ds00065File = "00065_record_who_chose_the_direction.sql"
	ds00066File = "00066_validate_the_direction_checks.sql"
)

// dsChecks are the three transactions CHECKs 00064 and 00065 add NOT VALID and 00066 validates.
var dsChecks = []string{
	"transactions_business_day_needs_a_direction",
	"transactions_direction_source_is_known",
	"transactions_direction_source_needs_a_direction",
}

// TestDirectionSource_TheCheckAdmitsTheTwoSourcesAndRefusesEveryOtherValue is the card's
// "'manager' reddedilir; yönsüz satıra değer yazılamaz", through the tap's own statement as
// tappa_app: every tap.DirectionSources value and NULL are written on a directed row (the Go
// vocabulary and 00065's CHECK are one list -- a constant the CHECK refuses is red here); 'manager'
// and the near misses are refused 23514 by transactions_direction_source_is_known; a source on an
// undirected row (reject, ignored) is refused 23514 by transactions_direction_source_needs_a_direction.
// Every write is rolled back.
func TestDirectionSource_TheCheckAdmitsTheTwoSourcesAndRefusesEveryOtherValue(t *testing.T) {
	d := appDB(t)
	w := bdNewWorld(t, d)
	person := bdEmployee(t, d, w, &w.branchA, nil)
	rollback := errors.New("roll back")
	const known, directed = "transactions_direction_source_is_known", "transactions_direction_source_needs_a_direction"
	type row struct {
		name      string
		verdict   string
		direction *string
		source    *string
		want      string // "" written, else the constraint that refuses it (23514)
	}
	tests := []row{
		{"a directed row with no source (every row before M15)", "ok", ptrStr("in"), nil, ""},
		{"an undirected refusal with no source", "reject", nil, nil, ""},
		{"manager is no source", "ok", ptrStr("in"), ptrStr("manager"), known},
		{"the empty string is no source", "ok", ptrStr("out"), ptrStr(""), known},
		{"a capital is no source", "ok", ptrStr("in"), ptrStr("AUTO"), known},
		{"a padded value is no source", "flag", ptrStr("out"), ptrStr(" employee"), known},
		{"an undirected refusal WITH auto", "reject", nil, ptrStr("auto"), directed},
		{"an ignored tap WITH employee", "ignored", nil, ptrStr("employee"), directed},
	}
	for _, s := range tap.DirectionSources {
		v := string(s)
		tests = append(tests,
			row{"a directed ok row with " + v, "ok", ptrStr("out"), &v, ""},
			row{"a directed flag row with " + v, "flag", ptrStr("in"), &v, ""})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := d.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
				_, e := store.New(tx).InsertTransaction(ctx, store.InsertTransactionParams{
					TenantID: w.tenant, EmployeeID: &person, LocationID: &w.branchA, Type: tc.direction,
					OccurredAt: time.Now().UTC(), Verdict: tc.verdict, Channel: "nfc", DirectionSource: tc.source,
				})
				if e != nil {
					return e
				}
				return rollback
			})
			code, name := m11Constraint(err)
			switch {
			case tc.want == "" && !errors.Is(err, rollback):
				t.Errorf("refused: %v", err)
			case tc.want != "" && (code != "23514" || name != tc.want):
				t.Errorf("answered %v, want 23514 on %s", err, tc.want)
			}
		})
	}
}

// TestDirectionSource_TheFeedWritersReachIsNotWidened is the card's "feed yazıcısının kolon
// yetkileri genişlemez": no role but tappa_app holds any column privilege on direction_source,
// tappa_app holds SELECT and INSERT and no UPDATE (§4.3: written once), and tappa_feedwriter's and
// tappa_holdingdefiner's whole reach on transactions is the column list it was at 00064 -- 00065
// grants nothing. M15-13 is the card that names the column for the punch event (00068).
func TestDirectionSource_TheFeedWritersReachIsNotWidened(t *testing.T) {
	ctx, tx := opTx(t)
	for _, role := range m11Roles {
		for _, priv := range []string{"SELECT", "INSERT", "UPDATE", "REFERENCES"} {
			var has bool
			if err := tx.QueryRow(ctx, `SELECT has_column_privilege($1, 'public.transactions', 'direction_source', $2)`, role, priv).Scan(&has); err != nil {
				t.Fatal(err)
			}
			want := role == "tappa_app" && (priv == "SELECT" || priv == "INSERT")
			if has != want {
				t.Errorf("%s %s on transactions.direction_source = %v, want %v", role, priv, has, want)
			}
		}
	}
	for _, c := range []struct{ role, want string }{
		{"tappa_feedwriter", "id,tenant_id,employee_id,location_id,tag_uid,type,occurred_at,verdict,channel,practice,site_tenant_id"},
		{"tappa_holdingdefiner", "id,tenant_id,employee_id,ctr,type,occurred_at,sun_valid,verdict,channel,practice,site_tenant_id,site_location_id,site_tag_uid"},
	} {
		for _, priv := range []string{"SELECT", "INSERT", "UPDATE"} {
			want := ""
			if priv == "SELECT" {
				want = c.want
			}
			if got := opColumns(t, ctx, tx, c.role, "transactions", priv); got != want {
				t.Errorf("%s %s on transactions = %q, want %q (00064's reach)", c.role, priv, got, want)
			}
		}
	}
}

// TestDirectionSource00065And00066_OpenWithASecondsLockTimeout: every section of both files opens
// with SET LOCAL lock_timeout = '1s' (ADR 0032 §A.5's rule for every ALTER TABLE transactions file;
// TestM11Migrations00037And00038_OpenWithASecondsLockTimeout's pin, for these two). 00065's ALTER
// and Down, and 00066's Down, take ACCESS EXCLUSIVE on the table every tap writes; behind a long
// reader the file gives up with 55P03 instead of queueing every later tap behind it.
func TestDirectionSource00065And00066_OpenWithASecondsLockTimeout(t *testing.T) {
	const want = "SET LOCAL lock_timeout = '1s';"
	for _, file := range []string{ds00065File, ds00066File} {
		up, down := opMigrationSections(t, file)
		for name, section := range map[string]string{"Up": up, "Down": down} {
			var first string
			for _, line := range strings.Split(section, "\n")[1:] {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "--") {
					continue
				}
				first = line
				break
			}
			if first != want {
				t.Errorf("%s's %s opens with %q, want %q", file, name, first, want)
			}
		}
	}
}

// TestDirectionSource00064And00065_TheDownLocksBeforeItLooks pins the line no walk can see: each
// refusing Down's statement after its lock_timeout is the LOCK TABLE that takes transactions ACCESS
// EXCLUSIVE, and it comes before the DO block's EXISTS. Without it a tap's uncommitted INSERT is
// invisible to the EXISTS, waits for the ALTER's lock, commits behind the check -- and the DROP
// COLUMN then drops the evidence it wrote (§4.3). The Down tests take the table first themselves
// (lockForDowns), so with the line deleted they stayed green (measured); this is a pin on the text.
// 00064 carries the same arrangement over the five tables its Down alters, in the Up's order.
func TestDirectionSource00064And00065_TheDownLocksBeforeItLooks(t *testing.T) {
	for _, c := range []struct{ file, lock string }{
		{bd00064File, "LOCK TABLE employees, departments, locations, tenants, transactions IN ACCESS EXCLUSIVE MODE;"},
		{ds00065File, "LOCK TABLE transactions IN ACCESS EXCLUSIVE MODE;"},
	} {
		_, down := opMigrationSections(t, c.file)
		var statements []string
		for _, line := range strings.Split(down, "\n")[1:] {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "--") {
				continue
			}
			statements = append(statements, line)
		}
		if len(statements) < 3 || statements[0] != "SET LOCAL lock_timeout = '1s';" || statements[1] != c.lock || statements[2] != "DO $$" {
			head := statements[:min(3, len(statements))]
			t.Errorf("%s's Down opens with %q, want the lock_timeout, then %q, then the DO block that looks", c.file, head, c.lock)
		}
	}
}

// dsAlterOf returns 00065's ALTER TABLE transactions statement and 00064's business-day CHECK
// (its ADD CONSTRAINT clause), from the files goose runs.
func dsAlterOf(t *testing.T) (alter, check64 string) {
	t.Helper()
	up65, _ := opMigrationSections(t, ds00065File)
	up64, _ := opMigrationSections(t, bd00064File)
	alter = regexp.MustCompile(`(?s)ALTER TABLE transactions\s+ADD COLUMN direction_source.*?;`).FindString(up65)
	check64 = regexp.MustCompile(`(?s)ADD CONSTRAINT transactions_business_day_needs_a_direction.*?NOT VALID`).FindString(up64)
	if alter == "" || check64 == "" {
		t.Fatalf("00065's ALTER (%d bytes) or 00064's CHECK (%d bytes) not found", len(alter), len(check64))
	}
	return alter, check64
}

// TestDirectionSource00065_TheAlterScansNothingAnd00066Scans is the card's "00065'in ALTER'ı satır
// taramaz, 00066 tarar" -- TestHoldingMigration_AlterWithinTwoSeconds' pattern. Pinned:
//   - by text: every ADD CONSTRAINT of 00065's ALTER is NOT VALID, its ADD COLUMN names no DEFAULT,
//     and 00066 validates exactly the NOT VALID CHECKs 00064 and 00065 add to transactions -- and at
//     HEAD all three are validated;
//   - BY MEASUREMENT: 00065's ALTER, run against a scratch copy of transactions' shape holding rows
//     (00064's CHECK on it NOT VALID, as 00064 left it), scans the table ZERO times
//     (pg_stat_xact_user_tables.seq_scan) and leaves every old row's direction_source NULL (no
//     back-fill: a DEFAULT 'auto' would read back on every one of them); each of 00066's VALIDATEs
//     scans it; the same ALTER with NOT VALID removed (the CONTROL -- the direct path) scans it.
func TestDirectionSource00065_TheAlterScansNothingAnd00066Scans(t *testing.T) {
	ctx, tx := opTx(t)
	alter, check64 := dsAlterOf(t)
	var added []string
	for _, piece := range strings.Split(alter, "ADD CONSTRAINT ")[1:] {
		name := strings.Fields(piece)[0]
		added = append(added, name)
		if !strings.Contains(piece, "NOT VALID") {
			t.Errorf("00065 adds %s without NOT VALID -- it would scan transactions under ACCESS EXCLUSIVE", name)
		}
	}
	if column := regexp.MustCompile(`(?s)ADD COLUMN direction_source[^,;]*`).FindString(alter); strings.TrimSpace(column) != "ADD COLUMN direction_source text" {
		t.Errorf("00065's column is %q, want exactly %q -- no DEFAULT (a default is a claim on every old row), no NOT NULL", column, "ADD COLUMN direction_source text")
	}
	notValid := append([]string{strings.Fields(strings.TrimPrefix(check64, "ADD CONSTRAINT "))[0]}, added...)
	slices.Sort(notValid)
	if strings.Join(notValid, ",") != strings.Join(dsChecks, ",") {
		t.Errorf("00064 and 00065 add %v NOT VALID, want %v", notValid, dsChecks)
	}
	up66, _ := opMigrationSections(t, ds00066File)
	var validated []string
	for _, m := range regexp.MustCompile(`VALIDATE CONSTRAINT (\w+);`).FindAllStringSubmatch(up66, -1) {
		validated = append(validated, m[1])
	}
	slices.Sort(validated)
	if strings.Join(validated, ",") != strings.Join(dsChecks, ",") {
		t.Errorf("00066 validates %v, want %v", validated, dsChecks)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_constraint WHERE conrelid = 'public.transactions'::regclass AND conname = ANY($1) AND convalidated`, dsChecks); n != 3 {
		t.Errorf("at HEAD %d of the three CHECKs are validated, want 3", n)
	}

	// The scratch: transactions' columns without direction_source, 00064's CHECK NOT VALID, and rows
	// -- directed and not -- created in this rolled-back transaction, so no other test waits on them.
	const rows = 50000
	scratch := strings.NewReplacer("ALTER TABLE transactions", "ALTER TABLE zz_m1509_tx",
		"ADD CONSTRAINT transactions_", "ADD CONSTRAINT zz_m1509_", "VALIDATE CONSTRAINT transactions_", "VALIDATE CONSTRAINT zz_m1509_")
	for _, s := range []string{
		`CREATE TABLE zz_m1509_tx (LIKE transactions INCLUDING DEFAULTS)`,
		`ALTER TABLE zz_m1509_tx DROP COLUMN direction_source`,
		scratch.Replace(`ALTER TABLE transactions ` + check64),
		fmt.Sprintf(`INSERT INTO zz_m1509_tx (tenant_id, occurred_at, verdict, channel, type)
		             SELECT gen_random_uuid(), now(), CASE WHEN g %% 2 = 0 THEN 'ok' ELSE 'reject' END, 'nfc',
		                    CASE WHEN g %% 2 = 0 THEN 'in' END
		               FROM generate_series(1, %d) AS g`, rows),
	} {
		if _, err := tx.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	scans := func(sp pgx.Tx, sql string) int64 {
		t.Helper()
		before := opInt(t, ctx, sp, `SELECT coalesce((SELECT seq_scan FROM pg_stat_xact_user_tables WHERE relid = 'zz_m1509_tx'::regclass), 0)`)
		if _, err := sp.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return opInt(t, ctx, sp, `SELECT coalesce((SELECT seq_scan FROM pg_stat_xact_user_tables WHERE relid = 'zz_m1509_tx'::regclass), 0)`) - before
	}
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		if n := scans(sp, scratch.Replace(alter)); n != 0 {
			t.Errorf("00065's ALTER scanned the table %d times under its ACCESS EXCLUSIVE, want 0", n)
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM zz_m1509_tx WHERE direction_source IS NOT NULL`); n != 0 {
			t.Errorf("after 00065's ALTER %d of %d old rows read a direction_source, want 0 -- the column back-fills nothing (§4.3)", n, rows)
		}
		for _, m := range regexp.MustCompile(`ALTER TABLE transactions VALIDATE CONSTRAINT \w+;`).FindAllString(up66, -1) {
			if n := scans(sp, scratch.Replace(m)); n == 0 {
				t.Errorf("%s scanned nothing -- a VALIDATE reads every row; the measure cannot tell a scan", m)
			}
		}
	})
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		direct := strings.ReplaceAll(scratch.Replace(alter), " NOT VALID", "")
		if n := scans(sp, direct); n == 0 {
			t.Errorf("CONTROL: the direct path (NOT VALID removed) scanned nothing -- the measure is blind")
		}
	})
}

// TestDirectionSource00066_ATapIsWrittenWhileItValidates is the reason 00066 is a file of its own
// (00044's): its VALIDATEs hold SHARE UPDATE EXCLUSIVE on transactions -- read from pg_locks, and
// nothing stronger -- and a tap's INSERT, from another connection as tappa_app under a lock_timeout
// of 1 s, is written while it is held. CONTROL: under 00066's Down (ACCESS EXCLUSIVE) the same
// INSERT gives up with 55P03 -- the probe can tell a blocking lock.
func TestDirectionSource00066_ATapIsWrittenWhileItValidates(t *testing.T) {
	d := appDB(t)
	w := bdNewWorld(t, d)
	person := bdEmployee(t, d, w, &w.branchA, nil)
	ctx, tx := opTx(t)
	up66, down66 := opMigrationSections(t, ds00066File)
	rollback := errors.New("roll back")
	tapWrite := func() (time.Duration, error) {
		start := time.Now()
		err := d.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
			if _, e := tx.Exec(ctx, `SET LOCAL lock_timeout = '1s'`); e != nil {
				return e
			}
			if _, e := store.New(tx).InsertTransaction(ctx, store.InsertTransactionParams{
				TenantID: w.tenant, EmployeeID: &person, LocationID: &w.branchA, Type: ptrStr("in"),
				OccurredAt: time.Now().UTC(), Verdict: "ok", Channel: "nfc",
			}); e != nil {
				return e
			}
			return rollback
		})
		return time.Since(start), err
	}
	locks := func(q pgx.Tx) string {
		t.Helper()
		var got string
		if err := q.QueryRow(ctx, `
			SELECT coalesce(string_agg(DISTINCT l.mode, ',' ORDER BY l.mode), '') FROM pg_catalog.pg_locks l
			 WHERE l.locktype = 'relation' AND l.granted AND l.pid = pg_backend_pid() AND l.relation = 'public.transactions'::regclass`).Scan(&got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := locks(tx); strings.Contains(got, "ShareUpdateExclusiveLock") || strings.Contains(got, "AccessExclusiveLock") {
		t.Fatalf("PREMISE: before 00066's Up this transaction already holds %q on transactions", got)
	}
	// Not holdingInSavepoint: it refuses a body that takes a strong table lock, and taking one is
	// what this body is for.
	dsSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		opRunSection(t, ctx, sp, up66, "00066 Up")
		got := locks(sp)
		if !strings.Contains(got, "ShareUpdateExclusiveLock") {
			t.Fatalf("00066's Up holds %q on transactions, want ShareUpdateExclusiveLock", got)
		}
		for _, strong := range []string{"ShareLock", "ShareRowExclusiveLock", "ExclusiveLock", "AccessExclusiveLock"} {
			if slices.Contains(strings.Split(got, ","), strong) {
				t.Errorf("00066's Up holds %s on transactions -- a tap's INSERT would wait for it", strong)
			}
		}
		if took, err := tapWrite(); !errors.Is(err, rollback) {
			t.Errorf("a tap while 00066's VALIDATEs hold their lock: %v (after %v), want written", err, took)
		}
	})
	dsSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		lockForDowns(t, ctx, sp, downTables(down66))
		opRunSection(t, ctx, sp, down66, "00066 Down")
		took, err := tapWrite()
		if code, _ := opCode(err); code != "55P03" {
			t.Errorf("CONTROL: a tap under 00066's Down answered %v after %v, want 55P03 -- the probe cannot tell a blocking lock", err, took)
		}
	})
}

// dsSavepoint runs body in a savepoint of tx that is rolled back when body returns -- with the
// locks body took.
func dsSavepoint(t *testing.T, ctx context.Context, tx pgx.Tx, body func(sp pgx.Tx)) {
	t.Helper()
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := sp.Rollback(ctx); err != nil {
			t.Fatalf("rollback to savepoint: %v", err)
		}
	}()
	body(sp)
}

// dsSurface is what 00065 and 00066 add or change, read from the catalogue.
type dsSurface struct {
	column, constraints, appColumns string
}

func dsRead(t *testing.T, ctx context.Context, q opQuerier) dsSurface {
	t.Helper()
	var s dsSurface
	for dst, sql := range map[*string]string{
		&s.column: `
			SELECT coalesce((SELECT format_type(a.atttypid, a.atttypmod) || ' notnull=' || a.attnotnull
			                        || ' default=' || coalesce(pg_get_expr(ad.adbin, ad.adrelid), '-') || ' missing=' || a.atthasmissing
			                   FROM pg_attribute a LEFT JOIN pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
			                  WHERE a.attrelid = 'public.transactions'::regclass AND a.attname = 'direction_source' AND NOT a.attisdropped), '')`,
		&s.constraints: `
			SELECT coalesce(string_agg(conname || ' validated=' || convalidated || ' ' || pg_get_constraintdef(oid), ' | ' ORDER BY conname), '')
			  FROM pg_constraint WHERE conrelid = 'public.transactions'::regclass AND conname = ANY('` + "{" + strings.Join(dsChecks, ",") + "}" + `'::text[])`,
	} {
		if err := q.QueryRow(ctx, sql).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	s.appColumns = opColumns(t, ctx, q, "tappa_app", "transactions", "SELECT") + "/" +
		opColumns(t, ctx, q, "tappa_app", "transactions", "INSERT") + "/" + opColumns(t, ctx, q, "tappa_app", "transactions", "UPDATE")
	return s
}

// TestDirectionSource00065And00066_DownsRefuseAChoiceAndOtherwiseGiveBack is the card's Down
// criterion (ADR 0032 "Geri alma"; the 00043 and 00064 precedent):
//   - 00066's Down gives back 00064's and 00065's state -- the three CHECKs by the same text, NOT
//     VALID -- and a NOT VALID CHECK still refuses a NEW 'manager' (NOT VALID exempts old rows only);
//   - with a row carrying a direction_source, 00065's Down is 55000 naming 00065 and changes nothing;
//   - without one it takes the column and its two CHECKs away and leaves 00064's CHECK as it was,
//     and the Ups take HEAD's catalogue again (00066's Up: all three validated).
//
// The walk runs in this test's transaction (rolled back), with the tables the Downs name locked
// first (lockForDowns), and the Downs may take no strong lock outside them.
func TestDirectionSource00065And00066_DownsRefuseAChoiceAndOtherwiseGiveBack(t *testing.T) {
	ctx, tx := opTx(t)
	up65, down65 := opMigrationSections(t, ds00065File)
	up66, down66 := opMigrationSections(t, ds00066File)
	tables := append(downTables(down65), downTables(down66)...)
	slices.Sort(tables)
	tables = slices.Compact(tables)
	if strings.Join(tables, ",") != "transactions" {
		t.Fatalf("PREMISE: the two Downs name %v, want transactions alone", tables)
	}
	lockForDowns(t, ctx, tx, tables)

	head := dsRead(t, ctx, tx)
	const (
		bdDef      = "CHECK (((business_day_start IS NULL) OR (type IS NOT NULL)))"
		knownDef   = "CHECK ((direction_source = ANY (ARRAY['auto'::text, 'employee'::text])))"
		directedOK = "CHECK (((direction_source IS NULL) OR (type IS NOT NULL)))"
	)
	wantHead := "transactions_business_day_needs_a_direction validated=true " + bdDef +
		" | transactions_direction_source_is_known validated=true " + knownDef +
		" | transactions_direction_source_needs_a_direction validated=true " + directedOK
	if head.column != "text notnull=false default=- missing=false" || head.constraints != wantHead {
		t.Fatalf("PREMISE at HEAD:\n column %q\n constraints %q\nwant a nullable text with no default and no stored missing value, and\n %q", head.column, head.constraints, wantHead)
	}
	if !strings.Contains(head.appColumns, "direction_source") || strings.Count(head.appColumns, "direction_source") != 2 {
		t.Fatalf("PREMISE at HEAD: tappa_app's SELECT/INSERT/UPDATE on transactions = %q, want direction_source in SELECT and INSERT only", head.appColumns)
	}

	tryIn := func(sp pgx.Tx, sql string, args ...any) error {
		t.Helper()
		nested, err := sp.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var runErr error
		if len(args) == 0 {
			_, runErr = nested.Conn().PgConn().Exec(ctx, sql).ReadAll()
		} else {
			_, runErr = nested.Exec(ctx, sql, args...)
		}
		if err := nested.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		return runErr
	}
	notValid := strings.ReplaceAll(strings.ReplaceAll(wantHead, "validated=true", "validated=false"), ")))", "))) NOT VALID")

	t.Run("a row with either direction source: 00066 gives back NOT VALID and 00065 refuses", func(t *testing.T) {
		dsSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			opRunSection(t, ctx, sp, down66, "00066 Down")
			at65 := dsRead(t, ctx, sp)
			if at65.constraints != notValid || at65.column != head.column {
				t.Errorf("after 00066's Down:\n %q\nwant the three by the same text, NOT VALID\n %q", at65.constraints, notValid)
			}
			tenant, venue, person := uuid.New(), uuid.New(), uuid.New()
			for _, s := range []struct {
				sql  string
				args []any
			}{
				{`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'm15-09 down', $2, 'restaurant', 'single')`,
					[]any{tenant, "VAT-M1509D-" + tenant.String()}},
				{`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'm15-09 down venue')`, []any{venue, tenant}},
				{`INSERT INTO employees (id, tenant_id, location_id, full_name, status) VALUES ($1, $2, $3, 'm15-09 down person', 'active')`,
					[]any{person, tenant, venue}},
				// punch_feed (00049) asks the write to run in its tenant's context, as every tap does.
				{`SELECT set_config('app.tenant_id', $1::text, true)`, []any{tenant}},
			} {
				if _, err := sp.Exec(ctx, s.sql, s.args...); err != nil {
					t.Fatalf("fixture: %v", err)
				}
			}
			insert := `INSERT INTO transactions (tenant_id, employee_id, location_id, type, occurred_at, verdict, channel, direction_source)
			           VALUES ($1, $2, $3, $4, now(), $5, 'nfc', $6)`
			for _, bad := range []struct {
				kind, verdict, source, constraint string
			}{
				{"in", "ok", "manager", "transactions_direction_source_is_known"},
				{"", "reject", "auto", "transactions_direction_source_needs_a_direction"},
			} {
				var kind *string
				if bad.kind != "" {
					kind = &bad.kind
				}
				err := tryIn(sp, insert, tenant, person, venue, kind, bad.verdict, bad.source)
				if code, name := m11Constraint(err); code != "23514" || name != bad.constraint {
					t.Errorf("a NEW %q (verdict %s) under the NOT VALID CHECKs answered %v, want 23514 on %s", bad.source, bad.verdict, err, bad.constraint)
				}
			}
			// EVERY value refuses, each alone in a savepoint of its own: a condition narrowed to one
			// value (= 'employee') would let the Down drop the other.
			for _, source := range tap.DirectionSources {
				func() {
					one, err := sp.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					defer func() {
						if err := one.Rollback(ctx); err != nil {
							t.Fatal(err)
						}
					}()
					if _, err := one.Exec(ctx, insert, tenant, person, venue, ptrStr("out"), "ok", string(source)); err != nil {
						t.Fatalf("fixture: a row with direction_source %q: %v", source, err)
					}
					err = tryIn(one, down65)
					var pg *pgconn.PgError
					if !errors.As(err, &pg) || pg.Code != "55000" || !strings.Contains(pg.Message, "migration 00065 Down") {
						t.Errorf("00065's Down over a row with direction_source %q answered %v, want 55000 naming 00065", source, err)
					}
					if after := dsRead(t, ctx, one); after != at65 {
						t.Errorf("the Down over %q changed the catalogue:\n %+v\nwant 00065's\n %+v", source, after, at65)
					}
				}()
			}
		})
	})

	t.Run("no direction source: the Downs give 00064 back and the Ups take HEAD again", func(t *testing.T) {
		dsSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			// A source another package committed would be evidence this test may not drop -- it is
			// set aside inside the savepoint (the append-only trigger off, NULLed, back on), and the
			// rollback restores it. Since M15-11 the tap path sets one on every directed nfc/qr
			// row, so on a database other packages' tests have written to this branch is the
			// ordinary one.
			if opInt(t, ctx, sp, `SELECT count(*) FROM transactions WHERE direction_source IS NOT NULL`) > 0 {
				for _, s := range []string{
					`ALTER TABLE transactions DISABLE TRIGGER transactions_no_mutation`,
					`UPDATE transactions SET direction_source = NULL WHERE direction_source IS NOT NULL`,
					`ALTER TABLE transactions ENABLE TRIGGER transactions_no_mutation`,
				} {
					if _, err := sp.Exec(ctx, s); err != nil {
						t.Fatalf("%s: %v", s, err)
					}
				}
			}
			opRunSection(t, ctx, sp, down66, "00066 Down")
			at65 := dsRead(t, ctx, sp)
			opRunSection(t, ctx, sp, down65, "00065 Down")
			at64 := dsRead(t, ctx, sp)
			want64 := "transactions_business_day_needs_a_direction validated=false " + bdDef + " NOT VALID"
			if at64.column != "" || at64.constraints != want64 || strings.Contains(at64.appColumns, "direction_source") {
				t.Errorf("after 00065's Down: %+v\nwant no column, 00064's CHECK alone and NOT VALID (%q), and no direction_source in tappa_app's reach", at64, want64)
			}
			if extra := strongTableLocksOutside(t, ctx, sp, tables); len(extra) > 0 {
				t.Errorf("the Downs took a strong lock on %v, outside the tables they name", extra)
			}
			opRunSection(t, ctx, sp, up65, "00065 Up again")
			if again := dsRead(t, ctx, sp); again != at65 {
				t.Errorf("after 00065's Up again:\n %+v\nwant what 00066's Down left -- its texts are 00065's\n %+v", again, at65)
			}
			opRunSection(t, ctx, sp, up66, "00066 Up again")
			if again := dsRead(t, ctx, sp); again != head {
				t.Errorf("after 00066's Up again:\n %+v\nwant HEAD's\n %+v", again, head)
			}
		})
	})
}
