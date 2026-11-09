package db

// shiftsnapshot_test.go -- migration 00095 (T152, ADR 0042): the shift a directed tap was judged
// against, frozen on its transactions row (shift_source, shift_start, shift_end, shift_overnight,
// shift_timezone).
//
// What is pinned, and why each half:
//   - the columns' shape: nullable, no default, no stored missing value -- a default would be a
//     claim on every old row (§4.3), and NOT NULL would refuse every manual INSERT;
//   - the CHECK's shape through the tap's own statement as tappa_app (rolled back): the three
//     shapes the tap path writes land, every other shape is 23514 on the one constraint, and the
//     Go vocabulary (tap.ShiftSources) is the CHECK's list;
//   - the old binary's INSERT (the pre-00095 column list) still lands on the new schema and leaves
//     the snapshot NULL -- the expand half of the deploy;
//   - the ALTER scans nothing (catalogue only, NOT VALID), measured on a scratch copy with a control;
//   - both sections open with the lock_timeout, the Down locks before it looks, refuses while a
//     row carries a snapshot and otherwise gives back 00094's catalogue, and the Up takes HEAD's
//     again.
//
// The role matrix on the five columns is internal/db's m11Columns (m11schema_test.go), the INSERT
// list privcatalog_test.go's and createdatgrant_test.go's (every column but id and created_at).

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
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/store"
)

const (
	shiftSnapshotFile  = "00095_record_the_shift_a_tap_was_judged_against.sql"
	shiftSnapshotCheck = "transactions_shift_snapshot_shape"
)

// ssColumns are 00095's five columns in the order the file adds them.
var ssColumns = []string{"shift_source", "shift_start", "shift_end", "shift_overnight", "shift_timezone"}

// ssSetAsideSQL NULLs every committed snapshot (the append-only trigger off, the UPDATE, back on).
// It runs only inside a test's transaction that is rolled back. aiLaterMigrations puts it in front
// of 00095's Down for the identity walks, which run every Down above 00070 in goose's order.
const ssSetAsideSQL = `ALTER TABLE transactions DISABLE TRIGGER transactions_no_mutation;
UPDATE transactions SET shift_source = NULL, shift_start = NULL, shift_end = NULL, shift_overnight = NULL, shift_timezone = NULL
 WHERE shift_source IS NOT NULL;
ALTER TABLE transactions ENABLE TRIGGER transactions_no_mutation;`

// shiftSnapshotMigration is 00095's number.
const shiftSnapshotMigration = 95

// ssSetAside NULLs every committed snapshot inside sp (the append-only trigger off, the UPDATE, back
// on) so 00095's Down -- which refuses while one exists -- can run; the savepoint's rollback restores
// them. Since T152 the tap path writes one on every directed nfc/qr row, so on a database other
// packages' tests have written to this is the ordinary case (feedSetAsideDirectionSources' twin).
func ssSetAside(t *testing.T, ctx context.Context, sp pgx.Tx) {
	t.Helper()
	if opInt(t, ctx, sp, `SELECT count(*) FROM transactions WHERE shift_source IS NOT NULL`) == 0 {
		return
	}
	opRunSection(t, ctx, sp, ssSetAsideSQL, "set the shift snapshots aside")
}

// ssDown runs 00095's Down inside sp after setting every snapshot aside: the step an older
// migration's walk (00088's, 00065's) takes first, in goose's own order.
func ssDown(t *testing.T, ctx context.Context, sp pgx.Tx) {
	t.Helper()
	_, down := opMigrationSections(t, shiftSnapshotFile)
	ssSetAside(t, ctx, sp)
	opRunSection(t, ctx, sp, down, "00095 Down")
}

// ssUp runs 00095's Up inside sp: the step such a walk takes last.
func ssUp(t *testing.T, ctx context.Context, sp pgx.Tx) {
	t.Helper()
	up, _ := opMigrationSections(t, shiftSnapshotFile)
	opRunSection(t, ctx, sp, up, "00095 Up")
}

func ssBool(b bool) *bool { return &b }

// ssTry runs a whole section (simple protocol, several statements) in a savepoint of sp that is
// rolled back, and returns its error -- a refusing Down leaves sp usable.
func ssTry(t *testing.T, ctx context.Context, sp pgx.Tx, sql string) error {
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

// ssSurface is what 00095 adds, read from the catalogue.
type ssSurface struct {
	columns, check, appInsert string
}

func ssRead(t *testing.T, ctx context.Context, q opQuerier) ssSurface {
	t.Helper()
	var s ssSurface
	for dst, sql := range map[*string]string{
		&s.columns: `
			SELECT coalesce(string_agg(a.attname || ' ' || format_type(a.atttypid, a.atttypmod) || ' notnull=' || a.attnotnull
			                           || ' default=' || coalesce(pg_get_expr(ad.adbin, ad.adrelid), '-') || ' missing=' || a.atthasmissing,
			                           ' | ' ORDER BY a.attnum), '')
			  FROM pg_attribute a LEFT JOIN pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
			 WHERE a.attrelid = 'public.transactions'::regclass AND a.attname LIKE 'shift\_%' AND NOT a.attisdropped`,
		&s.check: `
			SELECT coalesce((SELECT 'validated=' || convalidated || ' ' || pg_get_constraintdef(oid)
			                   FROM pg_constraint WHERE conrelid = 'public.transactions'::regclass AND conname = '` + shiftSnapshotCheck + `'), '')`,
	} {
		if err := q.QueryRow(ctx, sql).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	s.appInsert = opColumns(t, ctx, q, "tappa_app", "transactions", "INSERT")
	return s
}

// TestShiftSnapshot00095_TheColumnsAreNullableWithNoDefaultAndTheCheckIsNotValid: at HEAD the five
// columns are nullable with no default and no stored missing value (ADD COLUMN wrote nothing into
// the old rows), and the CHECK is NOT VALID -- left so on purpose (00095's header: every old row is
// NULL, which the first arm admits, so a VALIDATE would read the hottest table to prove nothing).
func TestShiftSnapshot00095_TheColumnsAreNullableWithNoDefaultAndTheCheckIsNotValid(t *testing.T) {
	ctx, tx := opTx(t)
	got := ssRead(t, ctx, tx)
	want := "shift_source text notnull=false default=- missing=false" +
		" | shift_start time without time zone notnull=false default=- missing=false" +
		" | shift_end time without time zone notnull=false default=- missing=false" +
		" | shift_overnight boolean notnull=false default=- missing=false" +
		" | shift_timezone text notnull=false default=- missing=false"
	if got.columns != want {
		t.Errorf("00095's columns at HEAD:\n %s\nwant\n %s", got.columns, want)
	}
	if !strings.HasPrefix(got.check, "validated=false CHECK (") || !strings.HasSuffix(got.check, "NOT VALID") {
		t.Errorf("%s at HEAD is %q, want a NOT VALID CHECK", shiftSnapshotCheck, got.check)
	}
	if !strings.Contains(got.appInsert, strings.Join(ssColumns, ",")) {
		t.Errorf("tappa_app's INSERT on transactions %q does not carry the five columns in order", got.appInsert)
	}
}

// TestShiftSnapshot00095_TheCheckAdmitsTheTapsThreeShapesAndRefusesEveryOther goes through the tap's
// own statement as tappa_app under RLS, each write rolled back: the shapes insertParams writes --
// nothing, 'none', a full department/location shift on a directed nfc/qr row -- land; every other
// shape is 23514 on transactions_shift_snapshot_shape. Every tap.ShiftSources value is tried, so the
// Go list and the CHECK's are one (a constant the CHECK refuses is red here).
func TestShiftSnapshot00095_TheCheckAdmitsTheTapsThreeShapesAndRefusesEveryOther(t *testing.T) {
	d := appDB(t)
	w := bdNewWorld(t, d)
	person := bdEmployee(t, d, w, &w.branchA, nil)
	rollback := errors.New("roll back")
	type snap struct {
		source     *string
		start, end pgtype.Time
		overnight  *bool
		zone       *string
	}
	clock := func(h int) pgtype.Time {
		return pgtype.Time{Microseconds: int64(time.Duration(h) * time.Hour / time.Microsecond), Valid: true}
	}
	full := func(source string) snap {
		return snap{ptrStr(source), clock(18), clock(2), ssBool(true), ptrStr("Europe/Malta")}
	}
	type row struct {
		name      string
		verdict   string
		channel   string
		direction *string
		snap      snap
		refused   bool
	}
	tests := []row{
		{"no snapshot on a directed row (every row before 00095)", "ok", "nfc", ptrStr("in"), snap{}, false},
		{"no snapshot on a refusal", "reject", "nfc", nil, snap{}, false},
		{"no snapshot on a manual row", "ok", "manual", ptrStr("in"), snap{}, false},
		{"'none' on a refusal", "reject", "nfc", nil, snap{source: ptrStr("none")}, true},
		{"'none' on a manual row", "ok", "manual", ptrStr("in"), snap{source: ptrStr("none")}, true},
		{"'none' with a start", "ok", "nfc", ptrStr("in"), snap{source: ptrStr("none"), start: clock(9)}, true},
		{"'none' with a zone", "ok", "nfc", ptrStr("in"), snap{source: ptrStr("none"), zone: ptrStr("Europe/Malta")}, true},
		{"a location shift on a refusal", "reject", "nfc", nil, full("location"), true},
		{"a department shift on a manual row", "ok", "manual", ptrStr("in"), full("department"), true},
		{"a location shift with no end", "ok", "nfc", ptrStr("in"), snap{ptrStr("location"), clock(9), pgtype.Time{}, ssBool(false), ptrStr("Europe/Malta")}, true},
		{"a location shift with no overnight flag", "ok", "nfc", ptrStr("in"), snap{ptrStr("location"), clock(9), clock(17), nil, ptrStr("Europe/Malta")}, true},
		{"a location shift with no zone", "ok", "nfc", ptrStr("in"), snap{ptrStr("location"), clock(9), clock(17), ssBool(false), nil}, true},
		{"values with no source", "ok", "nfc", ptrStr("in"), snap{nil, clock(9), clock(17), ssBool(false), ptrStr("Europe/Malta")}, true},
		{"an unknown source", "ok", "nfc", ptrStr("in"), full("venue"), true},
		{"a capital is no source", "ok", "nfc", ptrStr("in"), full("Location"), true},
		{"the empty string is no source", "ok", "qr", ptrStr("out"), full(""), true},
	}
	for _, s := range tap.ShiftSources {
		v := string(s)
		sn := full(v)
		if s == tap.ShiftSourceNone {
			sn = snap{source: &v}
		}
		tests = append(tests,
			row{"an nfc check-in with " + v, "ok", "nfc", ptrStr("in"), sn, false},
			row{"a flagged qr checkout with " + v, "flag", "qr", ptrStr("out"), sn, false})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := d.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
				_, e := store.New(tx).InsertTransaction(ctx, store.InsertTransactionParams{
					TenantID: w.tenant, EmployeeID: &person, LocationID: &w.branchA, Type: tc.direction,
					OccurredAt: time.Now().UTC(), Verdict: tc.verdict, Channel: tc.channel,
					ShiftSource: tc.snap.source, ShiftStart: tc.snap.start, ShiftEnd: tc.snap.end,
					ShiftOvernight: tc.snap.overnight, ShiftTimezone: tc.snap.zone,
				})
				if e != nil {
					return e
				}
				return rollback
			})
			code, name := m11Constraint(err)
			switch {
			case !tc.refused && !errors.Is(err, rollback):
				t.Errorf("refused: %v", err)
			case tc.refused && (code != "23514" || name != shiftSnapshotCheck):
				t.Errorf("answered %v, want 23514 on %s", err, shiftSnapshotCheck)
			}
		})
	}
}

// ssPre00095Insert is InsertTransaction's text as the previous binary sends it (db/queries before
// T152: the thirty columns 00088 lists), with the RETURNING the old Transaction model scans.
const ssPre00095Insert = `
INSERT INTO transactions (
    tenant_id, employee_id, location_id, department_id, tag_uid, ctr, type,
    occurred_at, source_ip, ip_match, gps_lat, gps_lng, gps_match, sun_valid,
    trust, verdict, note, channel, entered_by, practice, queued,
    policy_version_id, matched_sid, policy_layer, policy_context,
    site_tenant_id, site_location_id, site_tag_uid, business_day_start,
    direction_source
) VALUES (
    $1, $2, $3, NULL, NULL, NULL, 'in',
    now(), NULL, NULL, NULL, NULL, NULL, NULL,
    20, 'ok', NULL, 'nfc', NULL, false, false,
    NULL, NULL, NULL, NULL,
    NULL, NULL, NULL, NULL,
    'auto'
)
RETURNING id, tenant_id, employee_id, location_id, department_id, tag_uid, ctr,
          type, occurred_at, source_ip, ip_match, gps_lat, gps_lng, gps_match,
          sun_valid, trust, verdict, note, channel, entered_by, practice, queued,
          created_at, policy_version_id, matched_sid, policy_layer, policy_context,
          site_tenant_id, site_location_id, site_tag_uid, business_day_start,
          direction_source`

// TestShiftSnapshot00095_TheOldBinarysInsertStillLands is the expand half of the deploy: the
// migrate Job runs 00095 while the previous pod still serves, and that pod's InsertTransaction
// names none of the five columns. As tappa_app under RLS it lands (it is granted every column it
// names), returns the thirty-two columns the old model scans, and the row's snapshot reads NULL --
// "not recorded", the answer the old report gave every row. Rolled back.
func TestShiftSnapshot00095_TheOldBinarysInsertStillLands(t *testing.T) {
	d := appDB(t)
	w := bdNewWorld(t, d)
	person := bdEmployee(t, d, w, &w.branchA, nil)
	rollback := errors.New("roll back")
	err := d.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		rows, e := tx.Query(ctx, ssPre00095Insert, w.tenant, person, w.branchA)
		if e != nil {
			return e
		}
		var id uuid.UUID
		var columns int
		for rows.Next() {
			columns = len(rows.FieldDescriptions())
			values, e := rows.Values()
			if e != nil {
				return e
			}
			id = values[0].([16]byte)
		}
		rows.Close()
		if e := rows.Err(); e != nil {
			return e
		}
		if columns != 32 {
			t.Errorf("the old INSERT returned %d columns, want the 32 the old Transaction model scans", columns)
		}
		var nulls bool
		if e := tx.QueryRow(ctx, `SELECT shift_source IS NULL AND shift_start IS NULL AND shift_end IS NULL
		                                 AND shift_overnight IS NULL AND shift_timezone IS NULL
		                            FROM transactions WHERE tenant_id = $1 AND id = $2`, w.tenant, id).Scan(&nulls); e != nil {
			return e
		}
		if !nulls {
			t.Error("the old binary's row carries a shift snapshot; want all five NULL")
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("the previous binary's InsertTransaction on the 00095 schema: %v, want it to land", err)
	}
}

// TestShiftSnapshot00095_OpensWithALockTimeoutAndTheDownLocksBeforeItLooks pins the lines no walk can
// see: each section's first statement is the lock_timeout (the hot-table rule -- every ALTER of
// transactions takes ACCESS EXCLUSIVE, and behind a long reader the file must give up with 55P03,
// not queue every tap behind it), and the Down's next is the LOCK TABLE, before the DO block's
// EXISTS -- without it a tap's uncommitted snapshot is invisible to the check and the DROP COLUMN
// then erases it (00064/00065's arrangement; TestDirectionSource00064And00065_TheDownLocksBeforeItLooks).
func TestShiftSnapshot00095_OpensWithALockTimeoutAndTheDownLocksBeforeItLooks(t *testing.T) {
	up, down := opMigrationSections(t, shiftSnapshotFile)
	statements := func(section string) []string {
		var out []string
		for _, line := range strings.Split(section, "\n")[1:] {
			if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "--") {
				out = append(out, line)
			}
		}
		return out
	}
	if s := statements(up); len(s) == 0 || s[0] != "SET LOCAL lock_timeout = '1s';" {
		t.Errorf("00095's Up opens with %q, want the lock_timeout", s[:min(1, len(s))])
	}
	s := statements(down)
	if len(s) < 3 || s[0] != "SET LOCAL lock_timeout = '1s';" || s[1] != "LOCK TABLE transactions IN ACCESS EXCLUSIVE MODE;" || s[2] != "DO $$" {
		t.Errorf("00095's Down opens with %q, want the lock_timeout, the LOCK TABLE, then the DO block that looks", s[:min(3, len(s))])
	}
	if tables := slices.Compact(downTables(down)); strings.Join(tables, ",") != "transactions" {
		t.Errorf("00095's Down names %v, want transactions alone", tables)
	}
}

// TestShiftSnapshot00095_TheAlterScansNothing is the hot-table claim measured, not read
// (TestDirectionSource00065_TheAlterScansNothingAnd00066Scans' method): 00095's ALTER, run against a
// scratch copy of transactions' shape without the five columns and holding rows, scans it ZERO times
// (pg_stat_xact_user_tables.seq_scan) and leaves every old row's snapshot NULL (no back-fill); the
// same ALTER with NOT VALID removed (the CONTROL) scans it -- the measure can tell a scan.
func TestShiftSnapshot00095_TheAlterScansNothing(t *testing.T) {
	ctx, tx := opTx(t)
	up, _ := opMigrationSections(t, shiftSnapshotFile)
	alter := regexp.MustCompile(`(?s)ALTER TABLE transactions\s+ADD COLUMN shift_source.*?NOT VALID;`).FindString(up)
	if alter == "" {
		t.Fatal("00095's ALTER TABLE transactions not found")
	}
	for _, c := range ssColumns {
		col := regexp.MustCompile(`ADD COLUMN ` + c + `[^,;]*`).FindString(alter)
		if strings.Contains(col, "DEFAULT") || strings.Contains(col, "NOT NULL") {
			t.Errorf("00095 adds %q -- a DEFAULT is a claim on every old row, NOT NULL refuses the manual INSERTs", col)
		}
	}
	const rows = 50000
	scratch := strings.NewReplacer("ALTER TABLE transactions", "ALTER TABLE zz_t152_tx", "ADD CONSTRAINT transactions_", "ADD CONSTRAINT zz_t152_")
	setup := []string{`CREATE TABLE zz_t152_tx (LIKE transactions INCLUDING DEFAULTS)`}
	for _, c := range ssColumns {
		setup = append(setup, `ALTER TABLE zz_t152_tx DROP COLUMN `+c)
	}
	setup = append(setup, fmt.Sprintf(`INSERT INTO zz_t152_tx (tenant_id, occurred_at, verdict, channel, type)
	             SELECT gen_random_uuid(), now(), CASE WHEN g %% 2 = 0 THEN 'ok' ELSE 'reject' END, 'nfc',
	                    CASE WHEN g %% 2 = 0 THEN 'in' END
	               FROM generate_series(1, %d) AS g`, rows))
	for _, s := range setup {
		if _, err := tx.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	scans := func(sp pgx.Tx, sql string) int64 {
		t.Helper()
		const read = `SELECT coalesce((SELECT seq_scan FROM pg_stat_xact_user_tables WHERE relid = 'zz_t152_tx'::regclass), 0)`
		before := opInt(t, ctx, sp, read)
		if _, err := sp.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return opInt(t, ctx, sp, read) - before
	}
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		if n := scans(sp, scratch.Replace(alter)); n != 0 {
			t.Errorf("00095's ALTER scanned the table %d times under its ACCESS EXCLUSIVE, want 0", n)
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM zz_t152_tx WHERE shift_source IS NOT NULL OR shift_timezone IS NOT NULL`); n != 0 {
			t.Errorf("after 00095's ALTER %d of %d old rows carry a snapshot, want 0 -- the columns back-fill nothing (§4.3)", n, rows)
		}
	})
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		if n := scans(sp, strings.ReplaceAll(scratch.Replace(alter), " NOT VALID", "")); n == 0 {
			t.Error("CONTROL: the direct path (NOT VALID removed) scanned nothing -- the measure is blind")
		}
	})
}

// TestShiftSnapshot00095_TheDownRefusesASnapshotAndOtherwiseGivesBack is 00095's Down criterion
// (the 00043/00064/00065 precedent: evidence is not dropped):
//   - with a row carrying a snapshot -- each source alone, so a condition narrowed to one value
//     would be red -- the Down is 55000 naming 00095 and changes nothing;
//   - without one it takes the five columns and the CHECK away and leaves tappa_app's INSERT on
//     every column but id and created_at (the column grants go with the columns), and the Up takes
//     HEAD's catalogue again.
//
// The walk runs in this test's rolled-back transaction with transactions locked first
// (lockForDowns), and the Down may take no strong lock outside it.
func TestShiftSnapshot00095_TheDownRefusesASnapshotAndOtherwiseGivesBack(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, shiftSnapshotFile)
	lockForDowns(t, ctx, tx, []string{"transactions"})
	head := ssRead(t, ctx, tx)

	t.Run("a snapshot refuses the Down", func(t *testing.T) {
		dsSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			tenant, venue, person := uuid.New(), uuid.New(), uuid.New()
			for _, s := range []struct {
				sql  string
				args []any
			}{
				{`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 't152 down', $2, 'restaurant', 'single')`,
					[]any{tenant, "VAT-T152D-" + tenant.String()}},
				{`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 't152 down venue')`, []any{venue, tenant}},
				{`INSERT INTO employees (id, tenant_id, location_id, full_name, status) VALUES ($1, $2, $3, 't152 down person', 'active')`,
					[]any{person, tenant, venue}},
				// punch_feed (00049) asks the write to run in its tenant's context, as every tap does.
				{`SELECT set_config('app.tenant_id', $1::text, true)`, []any{tenant}},
			} {
				if _, err := sp.Exec(ctx, s.sql, s.args...); err != nil {
					t.Fatalf("fixture: %v", err)
				}
			}
			ssSetAside(t, ctx, sp)
			for _, source := range tap.ShiftSources {
				dsSavepoint(t, ctx, sp, func(one pgx.Tx) {
					insert := `INSERT INTO transactions (tenant_id, employee_id, location_id, type, occurred_at, verdict, channel, shift_source)
					           VALUES ($1, $2, $3, 'in', now(), 'ok', 'nfc', $4)`
					args := []any{tenant, person, venue, string(source)}
					if source != tap.ShiftSourceNone {
						insert = `INSERT INTO transactions (tenant_id, employee_id, location_id, type, occurred_at, verdict, channel,
						                                    shift_source, shift_start, shift_end, shift_overnight, shift_timezone)
						          VALUES ($1, $2, $3, 'in', now(), 'ok', 'nfc', $4, '18:00', '02:00', true, 'Europe/Malta')`
					}
					if _, err := one.Exec(ctx, insert, args...); err != nil {
						t.Fatalf("fixture: a row with shift_source %q: %v", source, err)
					}
					err := ssTry(t, ctx, one, down)
					var pg *pgconn.PgError
					if !errors.As(err, &pg) || pg.Code != "55000" || !strings.Contains(pg.Message, "migration 00095 Down") {
						t.Errorf("00095's Down over a row with shift_source %q answered %v, want 55000 naming 00095", source, err)
					}
					if after := ssRead(t, ctx, one); after != head {
						t.Errorf("the Down over %q changed the catalogue:\n %+v\nwant HEAD's\n %+v", source, after, head)
					}
				})
			}
		})
	})

	t.Run("no snapshot: the Down gives 00094 back and the Up takes HEAD again", func(t *testing.T) {
		dsSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			ssSetAside(t, ctx, sp)
			opRunSection(t, ctx, sp, down, "00095 Down")
			at94 := ssRead(t, ctx, sp)
			if at94.columns != "" || at94.check != "" {
				t.Errorf("after 00095's Down: columns %q, check %q, want neither", at94.columns, at94.check)
			}
			if want := cgString(t, sp, cgAllColumnsBut); at94.appInsert != want {
				t.Errorf("after 00095's Down tappa_app INSERTs\n %s\nwant every column but id and created_at\n %s", at94.appInsert, want)
			}
			if extra := strongTableLocksOutside(t, ctx, sp, []string{"transactions"}); len(extra) > 0 {
				t.Errorf("00095's Down took a strong lock on %v, outside transactions", extra)
			}
			opRunSection(t, ctx, sp, up, "00095 Up again")
			if again := ssRead(t, ctx, sp); again != head {
				t.Errorf("after 00095's Up again:\n %+v\nwant HEAD's\n %+v", again, head)
			}
		})
	})
}
