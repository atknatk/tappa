package db

// txclient_test.go -- migrations 00109 and 00110 (M16-04, ADR 0045 §2.4): transactions.client, which
// client wrote a tap ('browser' | 'app'), and its two CHECKs.
//
// What is pinned, and why each half:
//   - the column's shape: nullable, no default, no stored missing value -- a default would be a claim
//     on every old row (§4.3), NOT NULL would refuse every manual INSERT;
//   - TWO CHECKs name the column -- the value set (admits NULL) and "a manual row carries none" --
//     and at HEAD both are validated (00110). There is no
//     "a tap channel must carry a client" CHECK: it would refuse the previous binary's taps during
//     the rollout and lose them (§4.6). The proof that a new tap carries a client is the Go side's
//     (internal/handler's client_db_test.go, internal/domain/checkin's client_test.go);
//   - the CHECK through the tap's own statement as tappa_app: NULL and every tap.Clients value land,
//     every other value is 23514 on the value-set CHECK -- the Go vocabulary and the CHECK's are one --
//     and any client on a manual row is 23514 on the manual CHECK;
//   - the previous binary's InsertTransaction (no client) still lands on the new schema, nfc and qr,
//     and reads NULL -- the expand half of the deploy;
//   - of the shipped INSERTs only InsertTransaction names the column (the three manual statements
//     write NULL);
//   - tappa_app inserts it by name (ADR 0038), never updates it; no other role reads it (the punch
//     feed, /v1 and the holding projection do not carry it -- the card);
//   - 00109's ALTER scans nothing and 00110's VALIDATEs scan, over historical NULL rows, and succeed;
//     00110 holds SHARE UPDATE EXCLUSIVE and a tap is written while it does;
//   - both files open with the lock_timeout; 00109's Down locks before it looks, refuses while a row
//     carries a client and otherwise gives back 00108's catalogue; 00110's Down gives back both NOT
//     VALID by the same text; the Ups take HEAD again.
//
// ⚠️ ORDER (2026-11-18): M16-04 was written on a branch without M16-03, so goose then applied 00103
// and the client pair with a gap; a database already past the pair cannot take a lower file later
// (the repo does not run goose with -allow-missing) and must be rebuilt -- so M16-03 reaches main
// before or together with M16-04 (docs/plan/m16-mobil.md, "Migration ve ADR numaraları"). The merge
// that brought main's OAuth 00104-00107 into M16 renumbered the three to 00108, 00109 and 00110. The
// tests read the files from db/migrations and run their sections by name, so a number changes none
// of them; "00108's catalogue" below means "every file under 00109".

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	clientFile         = "00109_record_which_client_wrote_a_tap.sql"
	clientValidateFile = "00110_validate_the_client_checks.sql"
	clientCheck        = "transactions_client_is_known"
	// clientCheckDef is the value-set CHECK as pg_get_constraintdef prints it.
	clientCheckDef = "CHECK (((client IS NULL) OR (client = ANY (ARRAY['browser'::text, 'app'::text]))))"
	// clientManualCheck is the second CHECK: a manual row carries no client (the M16-04 security
	// review -- the database's half of clientColumn's manual rule).
	clientManualCheck    = "transactions_manual_has_no_client"
	clientManualCheckDef = "CHECK (((channel <> 'manual'::text) OR (client IS NULL)))"
	// clientMigration is 00109's number.
	clientMigration = 109
)

// clSetAsideSQL NULLs every committed client (the append-only trigger off, the UPDATE, back on). It
// runs only inside a test's transaction that is rolled back. aiLaterMigrations puts it in front of
// 00109's Down for the identity walks, which run every Down above 00070 in goose's order.
const clSetAsideSQL = `ALTER TABLE transactions DISABLE TRIGGER transactions_no_mutation;
UPDATE transactions SET client = NULL WHERE client IS NOT NULL;
ALTER TABLE transactions ENABLE TRIGGER transactions_no_mutation;`

// clSetAside NULLs every committed client inside sp so 00109's Down -- which refuses while one
// exists -- can run; the savepoint's rollback restores them. Since M16-04 the tap page writes one on
// every row, so on a database other packages' tests have written to this is the ordinary case.
func clSetAside(t *testing.T, ctx context.Context, sp pgx.Tx) {
	t.Helper()
	if opInt(t, ctx, sp, `SELECT count(*) FROM transactions WHERE client IS NOT NULL`) == 0 {
		return
	}
	opRunSection(t, ctx, sp, clSetAsideSQL, "set the clients aside")
}

// clDown runs 00110's and 00109's Downs inside sp after setting every client aside: the step an older
// migration's walk (00088's, 00065's) takes first, in goose's own order. The caller holds transactions
// (lockForDowns).
func clDown(t *testing.T, ctx context.Context, sp pgx.Tx) {
	t.Helper()
	_, down110 := opMigrationSections(t, clientValidateFile)
	_, down109 := opMigrationSections(t, clientFile)
	clSetAside(t, ctx, sp)
	opRunSection(t, ctx, sp, down110, "00110 Down")
	opRunSection(t, ctx, sp, down109, "00109 Down")
}

// clUp runs 00109's and 00110's Ups inside sp: the step such a walk takes last.
func clUp(t *testing.T, ctx context.Context, sp pgx.Tx) {
	t.Helper()
	up109, _ := opMigrationSections(t, clientFile)
	up110, _ := opMigrationSections(t, clientValidateFile)
	opRunSection(t, ctx, sp, up109, "00109 Up")
	opRunSection(t, ctx, sp, up110, "00110 Up")
}

// clSurface is what 00109 and 00110 add, read from the catalogue.
type clSurface struct {
	column, checks, appInsert, appUpdate string
}

func clRead(t *testing.T, ctx context.Context, q opQuerier) clSurface {
	t.Helper()
	var s clSurface
	for dst, sql := range map[*string]string{
		&s.column: `
			SELECT coalesce((SELECT format_type(a.atttypid, a.atttypmod) || ' notnull=' || a.attnotnull
			                        || ' default=' || coalesce(pg_get_expr(ad.adbin, ad.adrelid), '-') || ' missing=' || a.atthasmissing
			                   FROM pg_attribute a LEFT JOIN pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
			                  WHERE a.attrelid = 'public.transactions'::regclass AND a.attname = 'client' AND NOT a.attisdropped), '')`,
		// Every constraint whose key names the column, so a second CHECK on client is seen here
		// whatever it is called.
		&s.checks: `
			SELECT coalesce(string_agg(c.conname || ' validated=' || c.convalidated || ' ' || pg_get_constraintdef(c.oid), ' | ' ORDER BY c.conname), '')
			  FROM pg_constraint c
			  JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attname = 'client' AND NOT a.attisdropped
			 WHERE c.conrelid = 'public.transactions'::regclass AND a.attnum = ANY (c.conkey)`,
	} {
		if err := q.QueryRow(ctx, sql).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	s.appInsert = opColumns(t, ctx, q, "tappa_app", "transactions", "INSERT")
	s.appUpdate = opColumns(t, ctx, q, "tappa_app", "transactions", "UPDATE")
	return s
}

// TestTxClient00109_TheColumnIsNullableWithNoDefaultAndTwoValidatedChecks: at HEAD the column is a
// nullable text with no default and no stored missing value, exactly two constraints name it -- the
// value set (admits NULL) and "a manual row carries none", both validated by 00110; no CHECK forces a
// client on a tap channel -- tappa_app inserts it by name and updates nothing, and no other role
// reads it.
func TestTxClient00109_TheColumnIsNullableWithNoDefaultAndTwoValidatedChecks(t *testing.T) {
	ctx, tx := opTx(t)
	got := clRead(t, ctx, tx)
	if want := "text notnull=false default=- missing=false"; got.column != want {
		t.Errorf("transactions.client at HEAD: %q, want %q", got.column, want)
	}
	if want := clientCheck + " validated=true " + clientCheckDef + " | " + clientManualCheck + " validated=true " + clientManualCheckDef; got.checks != want {
		t.Errorf("the constraints naming client at HEAD:\n %s\nwant exactly these two, validated\n %s", got.checks, want)
	}
	if !slices.Contains(strings.Split(got.appInsert, ","), "client") {
		t.Errorf("tappa_app's INSERT on transactions %q does not carry client (ADR 0038: a new column is granted by name)", got.appInsert)
	}
	if got.appUpdate != "" {
		t.Errorf("tappa_app UPDATEs %q on transactions, want nothing (§4.3)", got.appUpdate)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_class WHERE oid = 'public.transactions'::regclass AND has_table_privilege('tappa_app', oid, 'INSERT')`); n != 0 {
		t.Error("tappa_app holds a table-level INSERT on transactions again; ADR 0038 took it")
	}
	readers := clString(t, ctx, tx, `
		SELECT coalesce(string_agg(r.rolname || ':' || p.priv, ',' ORDER BY r.rolname, p.priv), '')
		  FROM pg_roles r CROSS JOIN (VALUES ('SELECT'), ('INSERT'), ('UPDATE'), ('REFERENCES')) AS p(priv)
		 WHERE r.rolname LIKE 'tappa\_%' AND r.rolname NOT IN ('tappa_owner', 'tappa_app')
		   AND NOT r.rolsuper
		   AND has_column_privilege(r.oid, 'public.transactions', 'client', p.priv)`)
	if readers != "" {
		t.Errorf("roles other than tappa_app reach transactions.client: %s -- the feed, /v1 and the holding projection do not carry it (M16-04)", readers)
	}
}

// clString reads one text value.
func clString(t *testing.T, ctx context.Context, q opQuerier, sql string, args ...any) string {
	t.Helper()
	var s string
	if err := q.QueryRow(ctx, sql, args...).Scan(&s); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return s
}

// TestTxClient00109_TheCheckAdmitsNullAndTheTwoClientsAndRefusesEveryOther goes through the tap's own
// statement as tappa_app under RLS, each write rolled back. NULL lands on every channel and verdict --
// the previous binary's rows, the manual rows, the sandbox's -- and every tap.Clients value lands on
// nfc and qr (so a Go constant the CHECK refuses is red here); every other value is 23514 on the
// value-set CHECK. Any client on a MANUAL row -- each tap.Clients value -- is 23514 on the manual
// CHECK, by name (the database's half of clientColumn's manual rule). A tap channel with NULL lands:
// there is no "tap channel must carry a client" CHECK.
func TestTxClient00109_TheCheckAdmitsNullAndTheTwoClientsAndRefusesEveryOther(t *testing.T) {
	d := appDB(t)
	w := bdNewWorld(t, d)
	person := bdEmployee(t, d, w, &w.branchA, nil)
	rollback := errors.New("roll back")
	type row struct {
		name, verdict, channel string
		direction              *string
		client                 *string
		refusedBy              string // "" = it lands
	}
	tests := []row{
		{"NULL on an nfc ok (every row before 00109)", "ok", "nfc", ptrStr("in"), nil, ""},
		{"NULL on a qr flag", "flag", "qr", ptrStr("out"), nil, ""},
		{"NULL on an nfc reject", "reject", "nfc", nil, nil, ""},
		{"NULL on a manual row", "ok", "manual", ptrStr("in"), nil, ""},
		{"an unknown client", "ok", "nfc", ptrStr("in"), ptrStr("ios"), clientCheck},
		{"android is no client", "ok", "nfc", ptrStr("in"), ptrStr("android"), clientCheck},
		{"web is no client", "ok", "qr", ptrStr("in"), ptrStr("web"), clientCheck},
		{"a capital is no client", "ok", "nfc", ptrStr("in"), ptrStr("Browser"), clientCheck},
		{"an upper-case app is no client", "ok", "nfc", ptrStr("in"), ptrStr("APP"), clientCheck},
		{"a padded value is no client", "ok", "nfc", ptrStr("in"), ptrStr(" browser"), clientCheck},
		{"the empty string is no client", "reject", "nfc", nil, ptrStr(""), clientCheck},
		// Both CHECKs refuse it; PostgreSQL reports the first by name (measured), and the row is
		// refused either way.
		{"an unknown client on a manual row", "ok", "manual", ptrStr("in"), ptrStr("kiosk"), clientCheck},
	}
	for _, c := range tap.Clients {
		v := string(c)
		tests = append(tests,
			row{"an nfc ok from the " + v, "ok", "nfc", ptrStr("in"), &v, ""},
			row{"a qr flag from the " + v, "flag", "qr", ptrStr("out"), &v, ""},
			row{"an nfc reject from the " + v, "reject", "nfc", nil, &v, ""},
			row{"an nfc ignored from the " + v, "ignored", "nfc", nil, &v, ""},
			row{"a manual row naming the " + v, "ok", "manual", ptrStr("in"), &v, clientManualCheck})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := d.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
				p := store.InsertTransactionParams{
					TenantID: w.tenant, EmployeeID: &person, LocationID: &w.branchA, Type: tc.direction,
					OccurredAt: time.Now().UTC(), Verdict: tc.verdict, Channel: tc.channel, Client: tc.client,
				}
				if tc.channel == "manual" {
					p.EnteredBy = &person // any uuid: entered_by is not a key
				}
				row, e := store.New(tx).InsertTransaction(ctx, p)
				if e != nil {
					return e
				}
				if (row.Client == nil) != (tc.client == nil) || (row.Client != nil && *row.Client != *tc.client) {
					t.Errorf("the row RETURNs client %v, want %v", row.Client, tc.client)
				}
				return rollback
			})
			code, name := m11Constraint(err)
			switch {
			case tc.refusedBy == "" && !errors.Is(err, rollback):
				t.Errorf("refused: %v", err)
			case tc.refusedBy != "" && (code != "23514" || name != tc.refusedBy):
				t.Errorf("answered %v, want 23514 on %s", err, tc.refusedBy)
			}
		})
	}
}

// clPre00109Insert is InsertTransaction's text as the previous binary sends it (db/queries before
// M16-04: the thirty-five columns, no client), with the RETURNING the old Transaction model scans.
const clPre00109Insert = `
INSERT INTO transactions (
    tenant_id, employee_id, location_id, department_id, tag_uid, ctr, type,
    occurred_at, source_ip, ip_match, gps_lat, gps_lng, gps_match, sun_valid,
    trust, verdict, note, channel, entered_by, practice, queued,
    policy_version_id, matched_sid, policy_layer, policy_context,
    site_tenant_id, site_location_id, site_tag_uid, business_day_start,
    direction_source, shift_source, shift_start, shift_end, shift_overnight,
    shift_timezone
) VALUES (
    $1, $2, $3, NULL, NULL, NULL, $5,
    now(), NULL, NULL, NULL, NULL, NULL, NULL,
    20, $6, NULL, $4, NULL, false, false,
    NULL, NULL, NULL, NULL,
    NULL, NULL, NULL, NULL,
    NULL, NULL, NULL, NULL, NULL,
    NULL
)
RETURNING id, tenant_id, employee_id, location_id, department_id, tag_uid, ctr,
          type, occurred_at, source_ip, ip_match, gps_lat, gps_lng, gps_match,
          sun_valid, trust, verdict, note, channel, entered_by, practice, queued,
          created_at, policy_version_id, matched_sid, policy_layer, policy_context,
          site_tenant_id, site_location_id, site_tag_uid, business_day_start,
          direction_source, shift_source, shift_start, shift_end, shift_overnight,
          shift_timezone`

// TestTxClient00109_ThePreviousBinarysTapStillLands is the reason there is no "a tap must carry a
// client" CHECK, measured: the migrate Job runs 00109 while the previous pod still serves, and that
// pod's InsertTransaction names no client. As tappa_app under RLS its nfc and qr taps -- ok and
// refused alike -- land, return the thirty-seven columns the old model scans, and read NULL. Its
// CONTROL is the next test (the rejected shape refuses the same tap).
func TestTxClient00109_ThePreviousBinarysTapStillLands(t *testing.T) {
	d := appDB(t)
	w := bdNewWorld(t, d)
	person := bdEmployee(t, d, w, &w.branchA, nil)
	rollback := errors.New("roll back")
	for _, tc := range []struct {
		channel, verdict string
		direction        *string
	}{
		{"nfc", "ok", ptrStr("in")},
		{"qr", "flag", ptrStr("in")},
		{"nfc", "reject", nil},
	} {
		t.Run(tc.channel+" "+tc.verdict, func(t *testing.T) {
			err := d.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
				rows, e := tx.Query(ctx, clPre00109Insert, w.tenant, person, w.branchA, tc.channel, tc.direction, tc.verdict)
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
				if columns != 37 {
					t.Errorf("the old INSERT returned %d columns, want the 37 the old Transaction model scans", columns)
				}
				var null bool
				if e := tx.QueryRow(ctx, `SELECT client IS NULL FROM transactions WHERE tenant_id = $1 AND id = $2`, w.tenant, id).Scan(&null); e != nil {
					return e
				}
				if !null {
					t.Error("the old binary's row carries a client; want NULL")
				}
				return rollback
			})
			if !errors.Is(err, rollback) {
				t.Fatalf("the previous binary's InsertTransaction on the 00109 schema: %v, want it to land", err)
			}
		})
	}
}

// TestTxClient00109_TheRejectedTapChannelCheckWouldLoseThePreviousBinarysTap is the CONTROL of the
// test above: the same statement under the rejected shape -- a CHECK that demands a client on a tap
// channel, added NOT VALID in a rolled-back savepoint, exactly as 00109 would have added it -- is
// refused with 23514. NOT VALID does not exempt new rows, so this is the record the rejected design
// loses during the rollout (§4.6), and the measure above can tell a refusal.
func TestTxClient00109_TheRejectedTapChannelCheckWouldLoseThePreviousBinarysTap(t *testing.T) {
	d := appDB(t)
	w := bdNewWorld(t, d)
	person := bdEmployee(t, d, w, &w.branchA, nil)
	ctx, tx := opTx(t)
	lockForDowns(t, ctx, tx, []string{"transactions"})
	dsSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		if _, err := sp.Exec(ctx, `ALTER TABLE transactions ADD CONSTRAINT zz_m1604_tap_needs_a_client
		                             CHECK (channel = 'manual' OR client IS NOT NULL) NOT VALID`); err != nil {
			t.Fatal(err)
		}
		if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1::text, true)`, w.tenant); err != nil {
			t.Fatal(err)
		}
		err := ssTry(t, ctx, sp, fmt.Sprintf(`INSERT INTO transactions (tenant_id, employee_id, location_id, type, occurred_at, verdict, channel)
		                                      VALUES ('%s', '%s', '%s', 'in', now(), 'ok', 'nfc')`, w.tenant, person, w.branchA))
		if code, name := m11Constraint(err); code != "23514" || name != "zz_m1604_tap_needs_a_client" {
			t.Errorf("under the rejected CHECK the previous binary's tap answered %v, want 23514 -- the measure cannot tell the loss", err)
		}
	})
}

// TestTxClient00109_OnlyTheTapsInsertNamesTheClient: of the INSERTs into transactions in db/queries,
// InsertTransaction alone names client (and names it once); the three manual statements do not, so
// with no DEFAULT their rows are NULL. A new INSERT that names it is read against this card.
func TestTxClient00109_OnlyTheTapsInsertNamesTheClient(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "db", "queries", "transactions.sql"))
	if err != nil {
		t.Fatal(err)
	}
	var clean strings.Builder
	for _, line := range strings.Split(string(b), "\n") {
		if i := strings.Index(line, "--"); i >= 0 && !strings.HasPrefix(strings.TrimSpace(line), "-- name:") {
			line = line[:i]
		}
		clean.WriteString(line + "\n")
	}
	named := map[string]int{}
	for _, block := range strings.Split(clean.String(), "-- name: ")[1:] {
		name := strings.Fields(block)[0]
		for _, m := range cgInsertList.FindAllStringSubmatch(block, -1) {
			for _, col := range strings.Split(strings.Trim(m[1], "()"), ",") {
				if strings.TrimSpace(col) == "client" {
					named[name]++
				}
			}
			if _, ok := named[name]; !ok {
				named[name] = 0
			}
		}
	}
	want := map[string]int{"InsertTransaction": 1, "InsertManualTransaction": 0, "InsertManualSiteTransaction": 0, "InsertManualTransactionAtAnchor": 0}
	if fmt.Sprint(named) != fmt.Sprint(want) {
		t.Errorf("the INSERTs into transactions name client %v times, want %v", named, want)
	}
	others, err := filepath.Glob(filepath.Join("..", "..", "db", "queries", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range others {
		if filepath.Base(f) == "transactions.sql" {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if cgInsertList.Match(b) {
			t.Errorf("%s inserts into transactions -- read it against M16-04's client rule", filepath.Base(f))
		}
	}
}

// TestTxClient00109And00110_OpenWithALockTimeoutAndTheDownLocksBeforeItLooks pins the lines no walk
// can see: every section's first statement is the lock_timeout (the hot-table rule), 00109's Down's
// next is the LOCK TABLE, before the DO block's EXISTS (without it a tap's uncommitted client is
// invisible to the check and the DROP COLUMN then erases it), the two files name transactions alone,
// 00109 adds the column with no DEFAULT and no NOT NULL and exactly two CHECKs, NOT VALID -- the
// value set and "a manual row carries none", neither demanding a client anywhere (no `client IS NOT
// NULL`) -- and no Up writes a row (no back-fill, §4.3).
func TestTxClient00109And00110_OpenWithALockTimeoutAndTheDownLocksBeforeItLooks(t *testing.T) {
	statements := func(section string) []string {
		var out []string
		for _, line := range strings.Split(section, "\n")[1:] {
			if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "--") {
				out = append(out, line)
			}
		}
		return out
	}
	up109, down109 := opMigrationSections(t, clientFile)
	up110, down110 := opMigrationSections(t, clientValidateFile)
	for name, section := range map[string]string{"00109 Up": up109, "00110 Up": up110, "00110 Down": down110} {
		if s := statements(section); len(s) == 0 || s[0] != "SET LOCAL lock_timeout = '1s';" {
			t.Errorf("%s opens with %q, want the lock_timeout", name, s[:min(1, len(s))])
		}
	}
	s := statements(down109)
	if len(s) < 3 || s[0] != "SET LOCAL lock_timeout = '1s';" || s[1] != "LOCK TABLE transactions IN ACCESS EXCLUSIVE MODE;" || s[2] != "DO $$" {
		t.Errorf("00109's Down opens with %q, want the lock_timeout, the LOCK TABLE, then the DO block that looks", s[:min(3, len(s))])
	}
	if tables := slices.Compact(append(downTables(down109), downTables(down110)...)); strings.Join(tables, ",") != "transactions" {
		t.Errorf("00109's and 00110's Downs name %v, want transactions alone", tables)
	}
	body := opStatementsOnly(up109)
	if col := regexp.MustCompile(`ADD COLUMN client[^,;]*`).FindString(body); strings.TrimSpace(col) != "ADD COLUMN client text" {
		t.Errorf("00109 adds %q, want exactly %q -- no DEFAULT (a claim on every old row), no NOT NULL", col, "ADD COLUMN client text")
	}
	if n := strings.Count(body, "CHECK ("); n != 2 {
		t.Errorf("00109's Up adds %d CHECKs, want two: the value set and the manual rule", n)
	}
	for _, want := range []string{
		`ADD CONSTRAINT ` + clientCheck + `\s+CHECK \(client IS NULL OR client IN \('browser', 'app'\)\) NOT VALID,`,
		`ADD CONSTRAINT ` + clientManualCheck + `\s+CHECK \(channel <> 'manual' OR client IS NULL\) NOT VALID;`,
	} {
		if !regexp.MustCompile(`(?s)` + want).MatchString(body) {
			t.Errorf("00109's Up does not carry %s", want)
		}
	}
	if strings.Contains(body, "client IS NOT NULL") {
		t.Error("00109's Up demands a client somewhere -- a CHECK forcing a client on a tap channel loses the previous binary's taps (§4.6)")
	}
	for name, section := range map[string]string{"00109 Up": up109, "00110 Up": up110} {
		if m := regexp.MustCompile(`(?i)\b(UPDATE|INSERT INTO|DELETE FROM)\s+(public\.)?transactions\b`).FindString(opStatementsOnly(section)); m != "" {
			t.Errorf("%s writes rows (%q) -- no back-fill (§4.3)", name, m)
		}
	}
}

// TestTxClient00109_TheAlterScansNothingAnd00110ScansHistory is the hot-table claim measured, not
// read (TestDirectionSource00065_TheAlterScansNothingAnd00066Scans' method): 00109's ALTER, run
// against a scratch copy of transactions' shape without the column and holding history, scans it ZERO
// times (pg_stat_xact_user_tables.seq_scan) and leaves every old row NULL; 00110's VALIDATE over that
// history -- every row NULL, manual rows among them -- scans it and succeeds, for each of the two. CONTROL: the same ALTER with NOT VALID removed
// scans -- the measure can tell a scan.
func TestTxClient00109_TheAlterScansNothingAnd00110ScansHistory(t *testing.T) {
	ctx, tx := opTx(t)
	up109, _ := opMigrationSections(t, clientFile)
	up110, _ := opMigrationSections(t, clientValidateFile)
	alter := regexp.MustCompile(`(?s)ALTER TABLE transactions\s+ADD COLUMN client.*?NOT VALID;`).FindString(up109)
	validates := regexp.MustCompile(`ALTER TABLE transactions VALIDATE CONSTRAINT (\w+);`).FindAllStringSubmatch(up110, -1)
	var validated []string
	for _, m := range validates {
		validated = append(validated, m[1])
	}
	if alter == "" || strings.Join(validated, ",") != clientCheck+","+clientManualCheck {
		t.Fatalf("00109's ALTER (%q) not found, or 00110 validates %v, want %s and %s", alter, validated, clientCheck, clientManualCheck)
	}
	const rows = 50000
	scratch := strings.NewReplacer("ALTER TABLE transactions", "ALTER TABLE zz_m1604_tx",
		"CONSTRAINT transactions_", "CONSTRAINT zz_m1604_")
	for _, s := range []string{
		`CREATE TABLE zz_m1604_tx (LIKE transactions INCLUDING DEFAULTS)`,
		`ALTER TABLE zz_m1604_tx DROP COLUMN client`,
		fmt.Sprintf(`INSERT INTO zz_m1604_tx (tenant_id, occurred_at, verdict, channel, type)
		             SELECT gen_random_uuid(), now(), CASE WHEN g %% 2 = 0 THEN 'ok' ELSE 'reject' END,
		                    CASE WHEN g %% 5 = 0 THEN 'manual' WHEN g %% 3 = 0 THEN 'qr' ELSE 'nfc' END, CASE WHEN g %% 2 = 0 THEN 'in' END
		               FROM generate_series(1, %d) AS g`, rows),
	} {
		if _, err := tx.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	scans := func(sp pgx.Tx, sql string) int64 {
		t.Helper()
		const read = `SELECT coalesce((SELECT seq_scan FROM pg_stat_xact_user_tables WHERE relid = 'zz_m1604_tx'::regclass), 0)`
		before := opInt(t, ctx, sp, read)
		if _, err := sp.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return opInt(t, ctx, sp, read) - before
	}
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		if n := scans(sp, scratch.Replace(alter)); n != 0 {
			t.Errorf("00109's ALTER scanned the table %d times under its ACCESS EXCLUSIVE, want 0", n)
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM zz_m1604_tx WHERE client IS NOT NULL`); n != 0 {
			t.Errorf("after 00109's ALTER %d of %d old rows carry a client, want 0 -- the column back-fills nothing (§4.3)", n, rows)
		}
		for _, m := range validates {
			if n := scans(sp, scratch.Replace(m[0])); n == 0 {
				t.Errorf("%s scanned nothing -- a VALIDATE reads every row; the measure cannot tell a scan", m[0])
			}
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM pg_constraint WHERE conrelid = 'zz_m1604_tx'::regclass
		                             AND conname IN ('zz_m1604_client_is_known', 'zz_m1604_manual_has_no_client') AND convalidated`); n != 2 {
			t.Errorf("00110's VALIDATEs over historical NULL rows left %d of the two CHECKs validated, want 2", n)
		}
	})
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		if n := scans(sp, strings.ReplaceAll(scratch.Replace(alter), " NOT VALID", "")); n == 0 {
			t.Error("CONTROL: the direct path (NOT VALID removed) scanned nothing -- the measure is blind")
		}
	})
}

// TestTxClient00110_ATapIsWrittenWhileItValidates is the reason 00110 is a file of its own: its
// VALIDATE holds SHARE UPDATE EXCLUSIVE on transactions -- read from pg_locks, nothing stronger --
// and a tap's INSERT from another connection, as tappa_app under a lock_timeout of 1 s, is written
// while it is held. CONTROL: under 00110's Down (ACCESS EXCLUSIVE) the same INSERT gives up with 55P03.
func TestTxClient00110_ATapIsWrittenWhileItValidates(t *testing.T) {
	d := appDB(t)
	w := bdNewWorld(t, d)
	person := bdEmployee(t, d, w, &w.branchA, nil)
	ctx, tx := opTx(t)
	up110, down110 := opMigrationSections(t, clientValidateFile)
	rollback := errors.New("roll back")
	tapWrite := func() (time.Duration, error) {
		start := time.Now()
		err := d.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
			if _, e := tx.Exec(ctx, `SET LOCAL lock_timeout = '1s'`); e != nil {
				return e
			}
			if _, e := store.New(tx).InsertTransaction(ctx, store.InsertTransactionParams{
				TenantID: w.tenant, EmployeeID: &person, LocationID: &w.branchA, Type: ptrStr("in"),
				OccurredAt: time.Now().UTC(), Verdict: "ok", Channel: "nfc", Client: ptrStr(string(tap.ClientBrowser)),
			}); e != nil {
				return e
			}
			return rollback
		})
		return time.Since(start), err
	}
	locks := func(q pgx.Tx) string {
		t.Helper()
		return clString(t, ctx, q, `
			SELECT coalesce(string_agg(DISTINCT l.mode, ',' ORDER BY l.mode), '') FROM pg_catalog.pg_locks l
			 WHERE l.locktype = 'relation' AND l.granted AND l.pid = pg_backend_pid() AND l.relation = 'public.transactions'::regclass`)
	}
	if got := locks(tx); strings.Contains(got, "ShareUpdateExclusiveLock") || strings.Contains(got, "AccessExclusiveLock") {
		t.Fatalf("PREMISE: before 00110's Up this transaction already holds %q on transactions", got)
	}
	dsSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		opRunSection(t, ctx, sp, up110, "00110 Up")
		got := locks(sp)
		if !strings.Contains(got, "ShareUpdateExclusiveLock") {
			t.Fatalf("00110's Up holds %q on transactions, want ShareUpdateExclusiveLock", got)
		}
		for _, strong := range []string{"ShareLock", "ShareRowExclusiveLock", "ExclusiveLock", "AccessExclusiveLock"} {
			if slices.Contains(strings.Split(got, ","), strong) {
				t.Errorf("00110's Up holds %s on transactions -- a tap's INSERT would wait for it", strong)
			}
		}
		if took, err := tapWrite(); !errors.Is(err, rollback) {
			t.Errorf("a tap while 00110's VALIDATE holds its lock: %v (after %v), want written", err, took)
		}
	})
	dsSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		lockForDowns(t, ctx, sp, downTables(down110))
		opRunSection(t, ctx, sp, down110, "00110 Down")
		took, err := tapWrite()
		if code, _ := opCode(err); code != "55P03" {
			t.Errorf("CONTROL: a tap under 00110's Down answered %v after %v, want 55P03 -- the probe cannot tell a blocking lock", err, took)
		}
	})
}

// TestTxClient00109And00110_DownsRefuseAClientAndOtherwiseGiveBack is the card's Down criterion (the
// 00043/00064/00065/00095 precedent: evidence is not dropped):
//   - 00110's Down gives back 00109's state -- the CHECK by the same text, NOT VALID -- and a NOT VALID
//     CHECK still refuses a NEW unknown client (NOT VALID exempts old rows only);
//   - with a row carrying a client -- each value alone, so a condition narrowed to one would be red --
//     00109's Down is 55000 naming 00109 and changes nothing;
//   - without one it takes the column and the CHECK away and leaves tappa_app's INSERT on every column
//     but id and created_at (the column grant goes with the column), and the Ups take HEAD again:
//     00110's VALIDATE succeeding over the historical rows, all NULL.
//
// The walk runs in this test's rolled-back transaction with transactions locked first (lockForDowns),
// and the Downs may take no strong lock outside it.
func TestTxClient00109And00110_DownsRefuseAClientAndOtherwiseGiveBack(t *testing.T) {
	ctx, tx := opTx(t)
	up109, down109 := opMigrationSections(t, clientFile)
	up110, down110 := opMigrationSections(t, clientValidateFile)
	lockForDowns(t, ctx, tx, []string{"transactions"})
	head := clRead(t, ctx, tx)
	notValid := clientCheck + " validated=false " + clientCheckDef + " NOT VALID | " +
		clientManualCheck + " validated=false " + clientManualCheckDef + " NOT VALID"

	fixture := func(sp pgx.Tx) (tenant, venue, person uuid.UUID) {
		t.Helper()
		tenant, venue, person = uuid.New(), uuid.New(), uuid.New()
		for _, s := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'm16-04 down', $2, 'restaurant', 'single')`,
				[]any{tenant, "VAT-M1604D-" + tenant.String()}},
			{`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'm16-04 down venue')`, []any{venue, tenant}},
			{`INSERT INTO employees (id, tenant_id, location_id, full_name, status) VALUES ($1, $2, $3, 'm16-04 down person', 'active')`,
				[]any{person, tenant, venue}},
			// punch_feed (00049) asks the write to run in its tenant's context, as every tap does.
			{`SELECT set_config('app.tenant_id', $1::text, true)`, []any{tenant}},
		} {
			if _, err := sp.Exec(ctx, s.sql, s.args...); err != nil {
				t.Fatalf("fixture: %v", err)
			}
		}
		return tenant, venue, person
	}
	const insert = `INSERT INTO transactions (tenant_id, employee_id, location_id, type, occurred_at, verdict, channel, client)
	                VALUES ($1, $2, $3, 'in', now(), 'ok', 'nfc', $4)`

	t.Run("a row with a client: 00110 gives back NOT VALID and 00109 refuses", func(t *testing.T) {
		dsSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			opRunSection(t, ctx, sp, down110, "00110 Down")
			at109 := clRead(t, ctx, sp)
			if at109.checks != notValid || at109.column != head.column || at109.appInsert != head.appInsert {
				t.Errorf("after 00110's Down:\n %+v\nwant both CHECKs by the same text, NOT VALID (%q), the rest HEAD's", at109, notValid)
			}
			tenant, venue, person := fixture(sp)
			err := ssTry(t, ctx, sp, fmt.Sprintf(`INSERT INTO transactions (tenant_id, employee_id, location_id, type, occurred_at, verdict, channel, client)
			                                      VALUES ('%s', '%s', '%s', 'in', now(), 'ok', 'nfc', 'ios')`, tenant, person, venue))
			if code, name := m11Constraint(err); code != "23514" || name != clientCheck {
				t.Errorf("a NEW 'ios' under the NOT VALID CHECK answered %v, want 23514 on %s", err, clientCheck)
			}
			err = ssTry(t, ctx, sp, fmt.Sprintf(`INSERT INTO transactions (tenant_id, employee_id, location_id, type, occurred_at, verdict, channel, entered_by, client)
			                                     VALUES ('%s', '%s', '%s', 'in', now(), 'ok', 'manual', '%s', 'browser')`, tenant, person, venue, person))
			if code, name := m11Constraint(err); code != "23514" || name != clientManualCheck {
				t.Errorf("a NEW manual row with a client under the NOT VALID CHECK answered %v, want 23514 on %s", err, clientManualCheck)
			}
			for _, c := range tap.Clients {
				dsSavepoint(t, ctx, sp, func(one pgx.Tx) {
					if _, err := one.Exec(ctx, insert, tenant, person, venue, string(c)); err != nil {
						t.Fatalf("fixture: a row with client %q: %v", c, err)
					}
					err := ssTry(t, ctx, one, down109)
					var pg *pgconn.PgError
					if !errors.As(err, &pg) || pg.Code != "55000" || !strings.Contains(pg.Message, "migration 00109 Down") {
						t.Errorf("00109's Down over a row with client %q answered %v, want 55000 naming 00109", c, err)
					}
					if after := clRead(t, ctx, one); after != at109 {
						t.Errorf("the Down over %q changed the catalogue:\n %+v\nwant 00109's\n %+v", c, after, at109)
					}
				})
			}
		})
	})

	t.Run("no client: the Downs give 00108 back and the Ups take HEAD again", func(t *testing.T) {
		dsSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			clSetAside(t, ctx, sp)
			opRunSection(t, ctx, sp, down110, "00110 Down")
			at109 := clRead(t, ctx, sp)
			opRunSection(t, ctx, sp, down109, "00109 Down")
			at108 := clRead(t, ctx, sp)
			if at108.column != "" || at108.checks != "" {
				t.Errorf("after 00109's Down: column %q, checks %q, want neither", at108.column, at108.checks)
			}
			if want := cgString(t, sp, cgAllColumnsBut); at108.appInsert != want {
				t.Errorf("after 00109's Down tappa_app INSERTs\n %s\nwant every column but id and created_at\n %s", at108.appInsert, want)
			}
			if extra := strongTableLocksOutside(t, ctx, sp, []string{"transactions"}); len(extra) > 0 {
				t.Errorf("the Downs took a strong lock on %v, outside transactions", extra)
			}
			if n := opInt(t, ctx, sp, `SELECT count(*) FROM transactions`); n == 0 {
				t.Log("no historical rows on this database: 00110's VALIDATE below proves nothing about history (the scratch test does)")
			}
			opRunSection(t, ctx, sp, up109, "00109 Up again")
			if again := clRead(t, ctx, sp); again != at109 {
				t.Errorf("after 00109's Up again:\n %+v\nwant what 00110's Down left -- its text is 00109's\n %+v", again, at109)
			}
			opRunSection(t, ctx, sp, up110, "00110 Up again (VALIDATE over the historical rows, all NULL)")
			if again := clRead(t, ctx, sp); again != head {
				t.Errorf("after 00110's Up again:\n %+v\nwant HEAD's\n %+v", again, head)
			}
		})
	})
}
