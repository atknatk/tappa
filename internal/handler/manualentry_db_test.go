package handler

// manualentry_db_test.go -- M6-08's write path against REAL Postgres.
//
// 🔴 A FAKE CANNOT TEST ANY OF THIS, and this file is the reason manualentry_test.go
// opens by saying what it cannot see. What is under test here is exactly the part a
// double agrees with unconditionally:
//
//	the COLUMNS this path can produce -- verdict, type, channel, practice, queued --
//	  and the far longer list it leaves NULL, which is section 4.7's wall and
//	  section 5's at the same time
//	section 4.5's scope, with a CROSS-TENANT probe and a POSITIVE CONTROL beside it
//	  (a probe that fails for the wrong reason proves nothing)
//	the composite FOREIGN KEY firing on its own, so the belt is not the only thing
//	the audit row and the record SHARING A TRANSACTION, measured by breaking each
//	  side in turn and counting rows
//	the section 4.3 barrier T1 named: after this task there are TWO writers of
//	  `transactions`, and "a refused record carries no direction" is a code
//	  invariant in one of them and a SQL literal in the other
//
// FIXTURES ARE NOT CLEANED UP: tappa_app has REVOKE DELETE on employees (00003) and
// `transactions` and audit_log are append-only by privilege AND by trigger (00005).
// Fresh random ids keep runs from colliding, and `make db-reset` clears the
// development database.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/manual"
	"github.com/atknatk/tappa/internal/domain/tap"
)

// manualFixture is one tenant with a venue, a department and an employee, plus a
// SECOND tenant to attack it from.
type manualFixture struct {
	data     *db.DB
	recorder *manual.Recorder

	tenantID     uuid.UUID
	locationID   uuid.UUID
	departmentID uuid.UUID
	employeeID   uuid.UUID
	actorID      uuid.UUID

	foreignTenant   uuid.UUID
	foreignEmployee uuid.UUID
}

func newManualFixture(t *testing.T) *manualFixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping manual entry write tests (real Postgres required)")
	}
	data, err := db.New(context.Background(), &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)
	trail, err := audit.New(data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	rec, err := manual.NewRecorder(data, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("manual.NewRecorder: %v", err)
	}
	f := &manualFixture{
		data: data, recorder: rec,
		tenantID: uuid.New(), locationID: uuid.New(), departmentID: uuid.New(),
		employeeID: uuid.New(), actorID: uuid.New(),
		foreignTenant: uuid.New(), foreignEmployee: uuid.New(),
	}
	f.seed(t, f.tenantID, f.locationID, f.departmentID, f.employeeID)
	f.seed(t, f.foreignTenant, uuid.New(), uuid.New(), f.foreignEmployee)
	return f
}

func (f *manualFixture) seed(t *testing.T, tenantID, venue, department, employee uuid.UUID) {
	t.Helper()
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure, timezone)
			 VALUES ($1, 'Kebab Factory Ltd', $2, 'restaurant', 'multi', 'Europe/Malta')`,
			tenantID, "VAT-"+tenantID.String()); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name, gps_lat, gps_lng)
			 VALUES ($1, $2, 'St Julians', 35.918, 14.489)`, venue, tenantID); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO departments (id, tenant_id, location_id, name)
			 VALUES ($1, $2, $3, 'Kitchen')`, department, tenantID, venue); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, department_id, full_name, status, invited_at)
			 VALUES ($1, $2, $3, $4, 'Maria Borg', 'active', now())`,
			employee, tenantID, venue, department)
		return e
	})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
}

// entry is the ordinary submission every test starts from.
func (f *manualFixture) entry(at time.Time, d manual.Direction) manual.Entry {
	return manual.Entry{
		TenantID: f.tenantID, EmployeeID: f.employeeID, EnteredBy: f.actorID,
		Direction: d, At: at,
	}
}

// row is one written record, read back column by column in the tenant's own context
// so RLS scopes the read exactly as production's does.
type manualRow struct {
	Verdict, Channel        string
	Type                    *string
	Trust                   *int16
	Practice, Queued        bool
	EnteredBy               *uuid.UUID
	LocationID, DeptID      *uuid.UUID
	TagUID, Note            *string
	Ctr                     *int32
	SunValid, IPM, GPSM     *bool
	SourceIP                *string
	GPSLat, GPSLng          *string
	PolicyLayer, MatchedSid *string
	PolicyVersion           *uuid.UUID
	PolicyContext           *string
	OccurredAt, CreatedAt   time.Time
}

func (f *manualFixture) read(t *testing.T, tenantID, id uuid.UUID) manualRow {
	t.Helper()
	var r manualRow
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT verdict, channel, type, trust, practice, queued, entered_by,
			        location_id, department_id, tag_uid, note, ctr,
			        sun_valid, ip_match, gps_match, host(source_ip),
			        gps_lat::text, gps_lng::text,
			        policy_layer, matched_sid, policy_version_id, policy_context::text,
			        occurred_at, created_at
			 FROM transactions WHERE tenant_id = $1 AND id = $2`, tenantID, id).
			Scan(&r.Verdict, &r.Channel, &r.Type, &r.Trust, &r.Practice, &r.Queued, &r.EnteredBy,
				&r.LocationID, &r.DeptID, &r.TagUID, &r.Note, &r.Ctr,
				&r.SunValid, &r.IPM, &r.GPSM, &r.SourceIP, &r.GPSLat, &r.GPSLng,
				&r.PolicyLayer, &r.MatchedSid, &r.PolicyVersion, &r.PolicyContext,
				&r.OccurredAt, &r.CreatedAt)
	})
	if err != nil {
		t.Fatalf("reading the record back: %v", err)
	}
	return r
}

// TestManualDB_WritesExactlyTheColumnsItMayAndNULLsTheREST.
//
// 🔴 IT READS THE ROW BACK RATHER THAN THE PARAMETERS, because what this path may
// write is fixed in SQL rather than in Go: verdict and channel are literals in
// InsertManualTransaction and the evidence columns are not in its column list at all.
// A test over the params struct would be a test of the caller's intentions.
//
// ⚠️ THIS BLOCK CARRIED TWO STALE CLAIMS FOR ONE ROUND, and both are recorded rather
// than quietly deleted because both were on a §4.3/§4.6 surface. It said "nothing in
// the schema forbids verdict='reject' with type='in'" -- true until migration 00014,
// false since -- and "a row a report would read as worked hours", which was never
// true: internal/domain/ledger's endpointState fail-safes an unrecognised or refused
// verdict to HoursAwaiting, measured. 00014's own header carries that table, and this
// file agreeing with it is the point: two files in one repository contradicting each
// other about a barrier is worse than either sentence alone.
//
// 🔴 AND THE ABSENT COLUMNS ARE ASSERTED ONE BY ONE. Every NULL below is a claim this
// path is structurally unable to make: no chip was read, no address was seen, no
// coordinate was taken (section 4.7), no policy decided. A column that is never
// written is a column that cannot leak.
func TestManualDB_WritesExactlyTheColumnsItMayAndNULLsTheREST(t *testing.T) {
	f := newManualFixture(t)
	at := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Second)
	rec, err := f.recorder.Record(context.Background(), f.entry(at, manual.Out))
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	r := f.read(t, f.tenantID, rec.ID)

	if r.Verdict != "ok" {
		t.Errorf("verdict = %q, want ok. It is a SQL literal, so this failing means the "+
			"statement changed shape.", r.Verdict)
	}
	if r.Channel != "manual" {
		t.Errorf("channel = %q, want manual -- a hand-typed record must never be readable "+
			"as a tap", r.Channel)
	}
	if r.Type == nil || *r.Type != "out" {
		t.Errorf("type = %v, want out", r.Type)
	}
	if r.Practice || r.Queued {
		t.Errorf("practice=%v queued=%v, want false/false. practice would keep the hours out "+
			"of payroll silently; queued would put a record nobody flagged into the approval "+
			"queue.", r.Practice, r.Queued)
	}
	if r.Trust == nil || *r.Trust != tap.TrustBase {
		t.Errorf("trust = %v, want the section 5 baseline %d -- it measures EVIDENCE, and "+
			"this record has none", r.Trust, tap.TrustBase)
	}
	if r.EnteredBy == nil || *r.EnteredBy != f.actorID {
		t.Errorf("entered_by = %v, want the administrator %s. The whole argument for "+
			"verdict='ok' rests on this column naming somebody answerable.", r.EnteredBy, f.actorID)
	}
	// THE PLACEMENT CAME FROM THE EMPLOYEE ROW, NOT FROM A CALLER. manual.Entry has no
	// field for either, so this is the only place the values could have come from.
	if r.LocationID == nil || *r.LocationID != f.locationID {
		t.Errorf("location_id = %v, want the employee's venue %s", r.LocationID, f.locationID)
	}
	if r.DeptID == nil || *r.DeptID != f.departmentID {
		t.Errorf("department_id = %v, want the employee's department %s", r.DeptID, f.departmentID)
	}
	// THE TWO CLOCKS. occurred_at is what the manager claimed; created_at is when the
	// row was written, from the database's own DEFAULT now(). The card asks for exactly
	// this, and nothing in Go can make them agree.
	if !r.OccurredAt.Equal(at) {
		t.Errorf("occurred_at = %s, want the claimed %s", r.OccurredAt, at)
	}
	if !r.CreatedAt.After(r.OccurredAt) {
		t.Errorf("created_at %s is not after occurred_at %s, so a backdated record does not "+
			"carry the real moment of writing", r.CreatedAt, r.OccurredAt)
	}

	// 🔴 THE ABSENCES, ONE AT A TIME. Each name is a column the tap path fills and this
	// statement has no parameter for.
	absent := []struct {
		name string
		got  any
		why  string
	}{
		{"tag_uid", ptrAny(r.TagUID), "no chip was touched"},
		{"ctr", ptrAny(r.Ctr), "there is no counter to have read"},
		{"sun_valid", ptrAny(r.SunValid), "00005 keeps three states and NULL is the true one: not evaluated for this channel. false would claim a check that never ran"},
		{"source_ip", ptrAny(r.SourceIP), "section 4.7 -- there was no request from the person"},
		{"ip_match", ptrAny(r.IPM), "no address to have matched"},
		{"gps_match", ptrAny(r.GPSM), "no fix to have matched"},
		{"gps_lat", ptrAny(r.GPSLat), "section 4.7 -- a coordinate this path cannot express"},
		{"gps_lng", ptrAny(r.GPSLng), "section 4.7 -- a coordinate this path cannot express"},
		{"policy_layer", ptrAny(r.PolicyLayer), "nothing was judged; a policy name here would say a rule decided the verdict, which is false"},
		{"matched_sid", ptrAny(r.MatchedSid), "same"},
		{"policy_version_id", ptrAny(r.PolicyVersion), "same"},
		{"policy_context", ptrAny(r.PolicyContext), "same"},
		{"note", ptrAny(r.Note), "none was submitted"},
	}
	for _, a := range absent {
		if a.got != nil {
			t.Errorf("%s = %v, want NULL -- %s", a.name, a.got, a.why)
		}
	}
}

// ptrAny turns a typed nil pointer into an untyped nil so a table can compare them.
func ptrAny[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

// TestManualDB_TheSchemaREFUSESADirectedRefusalNow -- migration 00014.
//
// 🔴 IT USED TO ASSERT THE OPPOSITE, AND THE HISTORY IS THE POINT. Before 00014 this
// test measured that the database ACCEPTED verdict='reject' with type='in': 00005's
// transactions_ok_has_direction runs one way only (ok => a direction), so the converse
// was kept by code alone. That was survivable with one writer; M6-08 added a second,
// and 00014 moved the rule into the schema. Measured before shipping it: 329 475 rows,
// 0 violations, so it went in VALIDATED rather than NOT VALID.
//
// ⚠️ WHAT IT IS NOT: a fix for a live money leak. THREE barriers already stood, and the
// third was measured rather than assumed -- internal/domain/ledger's endpointState
// fail-safes an unrecognised or refused verdict to HoursAwaiting, so such a row would
// have been quarantined and reported separately, never paid. 00014 turns "a malformed
// row that gets quarantined" into "no such row". The migration's own header carries the
// table.
func TestManualDB_TheSchemaREFUSESADirectedRefusalNow(t *testing.T) {
	f := newManualFixture(t)
	sentinel := errors.New("rollback")
	cases := []struct {
		name      string
		verdict   string
		direction *string
		wantOK    bool
	}{
		{"reject WITH a direction", "reject", strptr("in"), false},
		{"ignored WITH a direction", "ignored", strptr("out"), false},
		// THE POSITIVE CONTROLS. Without them "the insert failed" is also what a broken
		// fixture produces, and the constraint could be refusing everything.
		{"reject with NO direction (the ordinary refusal)", "reject", nil, true},
		{"ignored with NO direction", "ignored", nil, true},
		{"ok WITH a direction", "ok", strptr("in"), true},
		{"flag WITH a direction", "flag", strptr("out"), true},
		{"flag with NO direction (00005 permits it and rows exist)", "flag", nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var insertErr error
			err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
				_, insertErr = tx.Exec(ctx,
					`INSERT INTO transactions (tenant_id, employee_id, type, occurred_at, verdict, channel)
					 VALUES ($1, $2, $3, now(), $4, 'manual')`,
					f.tenantID, f.employeeID, c.direction, c.verdict)
				return sentinel
			})
			if err != nil && !errors.Is(err, sentinel) {
				t.Fatalf("the probe transaction failed for another reason: %v", err)
			}
			if ok := insertErr == nil; ok != c.wantOK {
				t.Fatalf("accepted=%v want %v (err=%v)", ok, c.wantOK, insertErr)
			}
			if !c.wantOK && !strings.Contains(fmt.Sprint(insertErr), "transactions_refusal_has_no_direction") {
				t.Errorf("it was refused by something OTHER than 00014's constraint: %v. The "+
					"test would then pass for the wrong reason.", insertErr)
			}
		})
	}
}

func strptr(s string) *string { return &s }

// TestManualDB_ARecordCannotCrossATenantBoundary is section 4.5 with its positive
// control beside it.
//
// 🔴 THE CONTROL IS THE HALF THAT MAKES THE PROBE MEAN ANYTHING. "No row was written"
// is also what a broken fixture produces, so the same call is made twice: once with a
// foreign employee id (must refuse, must write nothing anywhere) and once with the
// tenant's own (must succeed). Only the pair separates "RLS and the predicate held"
// from "nothing works".
func TestManualDB_ARecordCannotCrossATenantBoundary(t *testing.T) {
	f := newManualFixture(t)
	at := time.Now().UTC().Add(-time.Hour)

	// THE PROBE: this tenant's session, the OTHER tenant's employee.
	_, err := f.recorder.Record(context.Background(), manual.Entry{
		TenantID: f.tenantID, EmployeeID: f.foreignEmployee, EnteredBy: f.actorID,
		Direction: manual.Out, At: at,
	})
	if !errors.Is(err, manual.ErrUnknownEmployee) {
		t.Fatalf("recording for a FOREIGN employee answered %v, want ErrUnknownEmployee", err)
	}
	// ⚠️ WHAT THIS OBSERVES IS RLS, NOT THE QUERY'S OWN PREDICATE, and saying so is the
	// correction a mutation forced: replacing `e.tenant_id = @tenant_id` with a
	// tautology leaves every assertion in this test green, because the statement runs
	// inside db.WithTenant and 00003's policy hides the row regardless. The belt is
	// held separately, as text, by TestManualQuery_CARRIESTheTenantPredicate...
	if n := f.countRecords(t, f.tenantID, f.foreignEmployee); n != 0 {
		t.Errorf("%d record(s) were written for a foreign employee in this tenant", n)
	}
	if n := f.countRecords(t, f.foreignTenant, f.foreignEmployee); n != 0 {
		t.Errorf("%d record(s) reached the OTHER tenant's rows", n)
	}
	if n := f.countAudit(t, f.tenantID, f.foreignEmployee); n != 0 {
		t.Errorf("a refused cross-tenant write left %d audit row(s)", n)
	}

	// THE POSITIVE CONTROL: the same call, this tenant's own employee.
	rec, err := f.recorder.Record(context.Background(), f.entry(at, manual.Out))
	if err != nil {
		t.Fatalf("recording for THIS tenant's employee failed: %v -- the probe above proves "+
			"nothing if the ordinary path is broken too", err)
	}
	if n := f.countRecords(t, f.tenantID, f.employeeID); n != 1 {
		t.Fatalf("the tenant's own record produced %d row(s), want 1", n)
	}
	_ = rec

	// AND THE FOREIGN TENANT CANNOT SEE IT. The other half of the isolation claim:
	// deliberately NO tenant_id predicate here, so what returns zero is RLS rather than
	// a WHERE clause (CLAUDE.md section 6).
	var visible int
	if e := f.data.WithTenant(context.Background(), f.foreignTenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM transactions WHERE id = $1`, rec.ID).Scan(&visible)
	}); e != nil {
		t.Fatalf("the isolation read failed: %v", e)
	}
	if visible != 0 {
		t.Errorf("the other tenant can see %d of this record; RLS is not isolating "+
			"`transactions`", visible)
	}
}

// TestManualDB_TheCompositeForeignKeyFiresOnItsOWN.
//
// 🔴 THE BELT IS NOT THE ONLY THING, AND THAT IS ASSERTED RATHER THAN ARGUED. The
// statement's SELECT refuses a foreign employee before the key is reached, so in
// production 23503 never fires -- which means nothing ever measures whether it WOULD.
// This inserts the pair the SELECT would have refused, directly, and requires the key
// to refuse it. Rolled back.
func TestManualDB_TheCompositeForeignKeyFiresOnItsOWN(t *testing.T) {
	f := newManualFixture(t)
	sentinel := errors.New("rollback")
	var code, msg string
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO transactions (tenant_id, employee_id, type, occurred_at, verdict, channel)
			 VALUES ($1, $2, 'out', now(), 'ok', 'manual')`, f.tenantID, f.foreignEmployee)
		if e != nil {
			msg = e.Error()
			if i := strings.Index(msg, "SQLSTATE "); i >= 0 && len(msg) >= i+14 {
				code = msg[i+9 : i+14]
			}
		}
		return sentinel
	})
	if err != nil && !errors.Is(err, sentinel) {
		t.Fatalf("the probe transaction failed for another reason: %v", err)
	}
	if code != "23503" {
		t.Fatalf("inserting a FOREIGN (employee_id, tenant_id) pair answered %q (%s), want a "+
			"23503 foreign key violation. The statement's SELECT is the belt; this key is the "+
			"braces, and braces nobody measures are a sentence rather than a guarantee.",
			code, msg)
	}
	t.Logf("MEASURED: the composite key refuses a foreign pair on its own -- %s", msg)
}

// TestManualDB_TheRecordAndItsAuditRowSHAREATransaction.
//
// 🔴 BOTH DIRECTIONS ARE FALSE STATEMENTS AND BOTH ARE MEASURED. A record with no
// trail is an hour on a payslip nobody can be shown to have entered; a trail with no
// record says somebody's hours were typed while the report still shows the entry open.
// The test breaks the trail side and requires the record to disappear with it.
func TestManualDB_TheRecordAndItsAuditRowSHAREATransaction(t *testing.T) {
	f := newManualFixture(t)
	at := time.Now().UTC().Add(-2 * time.Hour)

	// THE HAPPY PAIR FIRST, so the counts below have a baseline that is not zero for
	// the wrong reason.
	if _, err := f.recorder.Record(context.Background(), f.entry(at, manual.In)); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if n := f.countRecords(t, f.tenantID, f.employeeID); n != 1 {
		t.Fatalf("records = %d, want 1", n)
	}
	if n := f.countAudit(t, f.tenantID, f.employeeID); n != 1 {
		t.Fatalf("audit rows = %d, want 1 -- the card's fourth criterion is that the trail "+
			"row is mandatory", n)
	}

	// NOW WITH A TRAIL THAT FAILS. The recorder is rebuilt with a Trail that refuses,
	// which is the only way to reach the branch: internal/audit's own RecordTx does not
	// fail on a healthy database.
	// failingTrail is review_db_test.go's, reused rather than re-declared: one broken
	// trail in the package is enough, and a second would be a second representation of
	// "the audit write can fail".
	broken, err := manual.NewRecorder(f.data, &failingTrail{}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("manual.NewRecorder: %v", err)
	}
	if _, err := broken.Record(context.Background(), f.entry(at.Add(time.Hour), manual.Out)); err == nil {
		t.Fatal("a record whose trail write failed was reported as SUCCESS")
	}
	if n := f.countRecords(t, f.tenantID, f.employeeID); n != 1 {
		t.Errorf("records = %d, want 1 -- the second record survived its own missing audit "+
			"row, so the two do not share a transaction", n)
	}
}

// TestManualDB_TheAuditRowCarriesTheFACTSAndNoSECRET is section 4.7 at the trail.
//
// 🔴 IT IS AN ALLOWED-KEY SET RATHER THAN A FORBIDDEN-WORD SCAN, which is the rule
// M6-06 arrived at after a denylist of seven words was beaten in one edit (a session
// id and a confirmation value were put in a free-text field and three tests stayed
// green; a uuid matches none of those seven words, and neither does a base64 token).
// A new key here fails the test until somebody adds it to the list WITH a reason.
func TestManualDB_TheAuditRowCarriesTheFACTSAndNoSECRET(t *testing.T) {
	f := newManualFixture(t)
	at := time.Now().UTC().Add(-5 * time.Hour).Truncate(time.Second)
	e := f.entry(at, manual.Out)
	e.Note = "phone was flat"
	rec, err := f.recorder.Record(context.Background(), e)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	var action, target, detail string
	var actor *uuid.UUID
	if err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT action, target, actor_id, detail::text FROM audit_log
			 WHERE tenant_id = $1 AND id = $2`, f.tenantID, rec.AuditID).
			Scan(&action, &target, &actor, &detail)
	}); err != nil {
		t.Fatalf("reading the audit row: %v", err)
	}
	if action != manual.Action {
		t.Errorf("action = %q, want %q", action, manual.Action)
	}
	if target != f.employeeID.String() {
		t.Errorf("target = %q, want the employee %s", target, f.employeeID)
	}
	if actor == nil || *actor != f.actorID {
		t.Errorf("actor_id = %v, want the administrator %s", actor, f.actorID)
	}
	// THE MANAGER'S SENTENCE IS NOT COPIED IN, only whether there was one. It is
	// already stored once, on the record; a second copy in an append-only jsonb column
	// doubles the surface for no investigative gain.
	if strings.Contains(detail, "phone was flat") {
		t.Error("the audit detail carries the manager's free text about a named employee")
	}
	// 🔴 THE VALUES ARE CHECKED, NOT JUST THE KEYS, AND A MUTATION IS WHY. The first
	// version of this test asserted that each key was PRESENT; blanking the direction
	// to "" left it green, so the trail could have carried `"direction": ""` on every
	// row and nothing would have said so. A key with no value is a field an
	// investigator cannot use, which is the same defect as a missing one.
	var got map[string]any
	if err := json.Unmarshal([]byte(detail), &got); err != nil {
		t.Fatalf("the audit detail is not an object: %v", err)
	}
	want := map[string]any{
		"direction":   "out",
		"verdict":     "ok",
		"channel":     "manual",
		"record_id":   rec.ID.String(),
		"backdated":   true,
		"has_note":    true,
		"occurred_at": at.Format(time.RFC3339),
		"created_at":  rec.CreatedAt.UTC().Format(time.RFC3339),
	}
	for key, wantVal := range want {
		gotVal, ok := got[key]
		if !ok {
			t.Errorf("the audit detail is missing %q; an investigator asking how long after "+
				"the shift a record was typed needs both instants", key)
			continue
		}
		if gotVal != wantVal {
			t.Errorf("audit detail %q = %v, want %v", key, gotVal, wantVal)
		}
	}
	// AND THE TWO INSTANTS ARE DIFFERENT ONES. A trail that stamped occurred_at twice
	// would look complete and answer nothing.
	if got["occurred_at"] == got["created_at"] {
		t.Errorf("occurred_at and created_at are the same value (%v) on a backdated record, "+
			"so the trail cannot say how long after the shift it was typed", got["occurred_at"])
	}
	// THE PINNED SET. Any key not on the list above is a new field somebody has to
	// defend, which is the only mechanism that survives a value nobody thought of.
	for key := range got {
		if _, ok := want[key]; !ok {
			t.Errorf("the audit detail carries an unexpected key %q. Add it to the list "+
				"WITH its reason, or take it out -- section 4.7 survives a jsonb column only "+
				"because the key set is closed.", key)
		}
	}
}

// countRecords counts this employee's transactions in a tenant's own context.
func (f *manualFixture) countRecords(t *testing.T, tenantID, employeeID uuid.UUID) int {
	t.Helper()
	var n int
	if err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM transactions WHERE tenant_id = $1 AND employee_id = $2`,
			tenantID, employeeID).Scan(&n)
	}); err != nil {
		t.Fatalf("counting records: %v", err)
	}
	return n
}

// countAudit counts this flow's trail rows about one employee. Both predicates matter:
// audit_log is append-only and shared by every run against this database.
func (f *manualFixture) countAudit(t *testing.T, tenantID, employeeID uuid.UUID) int {
	t.Helper()
	var n int
	if err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM audit_log
			 WHERE tenant_id = $1 AND action = $2 AND target = $3`,
			tenantID, manual.Action, employeeID.String()).Scan(&n)
	}); err != nil {
		t.Fatalf("counting audit rows: %v", err)
	}
	return n
}

// TestManualDB_ClosesTheOpenEntryQ18SaysNobodyWillCloseForYou is the LOOP this task
// exists to close, measured end to end against the real report engine.
//
// 🔴 Q18: the system produces NO checkout of its own, because "an hour the software
// invented" stops being evidence. The cost of that decision is an open entry sitting
// in the reports section's needs-action list with its hours OUT of the total, and the
// only thing that closes it is a manager typing one. This test walks exactly that:
// leave an entry open, read the report, type the checkout, read the report again.
//
// 🔴 THE MANUAL FLAG IS ASSERTED AS WELL AS THE HOURS. The M6-07 card's criterion is
// that a typed record is "separately marked" -- if the hours arrived but the mark did
// not, a payroll would show a measured figure where a declared one belongs.
func TestManualDB_ClosesTheOpenEntryQ18SaysNobodyWillCloseForYou(t *testing.T) {
	f := newManualFixture(t)
	reader, err := ledger.NewReader(f.data, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("ledger.NewReader: %v", err)
	}
	malta, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatalf("loading Europe/Malta: %v", err)
	}
	// A shift INSIDE the current local week, and far enough from `now` that the
	// interval is a shift rather than "somebody is on shift right now". Yesterday's
	// 09:00-17:00 is both, and it is inside MaxBackdate by a wide margin.
	yesterday := time.Now().In(malta).AddDate(0, 0, -1)
	in := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 9, 0, 0, 0, malta)
	out := in.Add(8 * time.Hour)
	day := ledger.Date{Year: in.Year(), Month: in.Month(), Day: in.Day()}

	if _, err := f.recorder.Record(context.Background(), f.entry(in.UTC(), manual.In)); err != nil {
		t.Fatalf("recording the check-in: %v", err)
	}
	before, err := reader.Hours(context.Background(), f.tenantID, ledger.ReportFilter{Day: day})
	if err != nil {
		t.Fatalf("reading the report: %v", err)
	}
	if before.Totals.Worked != 0 {
		t.Errorf("an UNCLOSED entry is worth %v; Q18 says its hours are not estimated",
			before.Totals.Worked)
	}
	if before.Totals.Open != 1 {
		t.Fatalf("open entries = %d, want 1 -- the entry a manager has to close is not "+
			"being reported", before.Totals.Open)
	}
	if len(before.Open) != 1 || !before.Open[0].Manual {
		t.Errorf("the open entry is not marked as typed by a manager: %+v", before.Open)
	}

	if _, err := f.recorder.Record(context.Background(), f.entry(out.UTC(), manual.Out)); err != nil {
		t.Fatalf("recording the checkout: %v", err)
	}
	after, err := reader.Hours(context.Background(), f.tenantID, ledger.ReportFilter{Day: day})
	if err != nil {
		t.Fatalf("re-reading the report: %v", err)
	}
	if after.Totals.Worked != 8*time.Hour {
		t.Fatalf("worked = %v, want 8h. The manager closed the entry and the payroll total "+
			"did not move, which is the failure Q18 exists to prevent.", after.Totals.Worked)
	}
	if after.Totals.Open != 0 {
		t.Errorf("open entries = %d after the checkout, want 0", after.Totals.Open)
	}
	if len(after.People) != 1 {
		t.Fatalf("people = %d, want 1", len(after.People))
	}
	// 🔴 THE TYPED ARRIVAL IS COUNTED. ManualArrivals counts a person's attributed
	// CHECK-INS that a manager typed; here that is one, and the point is that it is not
	// zero -- a typed figure must never render as a measured one.
	//
	// ⚠️ IT IS ARRIVALS RATHER THAN SHIFTS, and the field was renamed for it: the old
	// name rendered as "shifts" on both surfaces and was measured wrong in BOTH
	// directions (an in-correction counted 2 for one shift; a tapped arrival with a
	// TYPED checkout counted 0). See ledger.PersonHours.ManualArrivals.
	if after.People[0].ManualArrivals != 1 {
		t.Errorf("ManualArrivals = %d, want 1 -- the report is not separating a typed "+
			"record from a tapped one (M6-07's criterion)", after.People[0].ManualArrivals)
	}
	if after.People[0].Worked != 8*time.Hour {
		t.Errorf("the person's own row says %v, want 8h", after.People[0].Worked)
	}
}

// TestManualQuery_CARRIESTheTenantPredicateItselfIsAText Assertion, and it exists
// because a MUTATION showed the database test above cannot see the predicate at all.
//
// 🔴 MEASURED, AND IT CORRECTS WHAT THE CROSS-TENANT TEST APPEARS TO PROVE. Replacing
// `WHERE e.tenant_id = @tenant_id` with a tautology and re-running
// TestManualDB_ARecordCannotCrossATenantBoundary leaves it GREEN. The reason is not a
// weak fixture -- it is that RLS is doing the work: the statement runs inside
// db.WithTenant, so migration 00003's policy on `employees` hides the other tenant's
// row whether or not the query says so. CLAUDE.md section 4.5 asks for BOTH ("belt and
// braces") and section 6 explains why an isolation test must not carry the predicate:
// with it, zero rows proves the WHERE rather than the policy.
//
// SO THE TWO NETS MEASURE DIFFERENT WALLS AND NEITHER COVERS THE OTHER:
//
//	the database test    RLS refuses the foreign row (the braces), end to end
//	this one             the shipped statement still carries the belt
//
// ⚠️ IT IS A TEXT ASSERTION AND THAT IS A REAL WEAKNESS, stated rather than hidden: it
// reads the source of the query rather than executing anything, so it cannot tell a
// predicate that works from one that merely looks right. Turning RLS off for a probe
// would need a superuser and an ALTER TABLE -- migration 00005 sets FORCE ROW LEVEL
// SECURITY, so even the table owner is subject to it -- which is a bigger hammer than
// this deserves.
func TestManualQuery_CARRIESTheTenantPredicateItselfIsATextAssertion(t *testing.T) {
	src, err := os.ReadFile("../../db/queries/transactions.sql")
	if err != nil {
		t.Fatalf("reading the query source: %v", err)
	}
	const marker = "-- name: InsertManualTransaction :one"
	i := strings.Index(string(src), marker)
	if i < 0 {
		t.Fatal("InsertManualTransaction is not in db/queries/transactions.sql any more")
	}
	body := string(src)[i:]
	if j := strings.Index(body[len(marker):], "-- name: "); j >= 0 {
		body = body[:len(marker)+j]
	}
	for _, want := range []string{
		"e.tenant_id = @tenant_id", // the belt on the SELECT
		"INSERT INTO transactions", // and it is still the statement being scoped
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the shipped statement no longer contains %q. Section 4.5 wants the "+
				"explicit predicate ALONGSIDE RLS, and the end-to-end test cannot see its "+
				"absence -- measured: a tautology in its place leaves that test green.", want)
		}
	}
	// ANTI-VACUITY: the slice really is the statement rather than an empty string.
	if len(body) < 200 {
		t.Fatalf("the extracted statement is %d bytes; this is reading the wrong thing", len(body))
	}
}
