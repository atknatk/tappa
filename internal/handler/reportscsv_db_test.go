package handler

// reportscsv_db_test.go -- M6-07 phase B's red-line proofs, against REAL Postgres,
// over REAL HTTP, with a real panel session, downloading a REAL file.
//
// 🔴 WHY THESE CANNOT BE UNIT TESTS, which is the difference between a net and a
// tautology. A fake reader hands the exporter a value with no coordinate field, no
// foreign row and no name it did not itself invent, so every assertion below would
// pass because nothing could ever have failed. The rules that need a database are:
//
//	§4.5  tenant A's CSV must never carry tenant B's hours
//	§4.7  a raw coordinate must never reach a FILE -- which is worse than a screen,
//	      because a file is emailed, kept and forwarded
//	§4.6  the audit row for a bulk export must really be in audit_log, under RLS
//	      and under this tenant, rather than merely handed to a fake
//
// AND ONE MORE THAT ONLY A DATABASE CAN SHOW: a formula stored VERBATIM in Postgres
// (which is what an attacker-chosen employee name is) coming back out of the real
// query, through the real renderer, escaped. templ never touches this path.

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/ledger"
)

// downloadCSV fetches the export over real HTTP and parses it as any non-spreadsheet
// consumer would.
func downloadCSV(t *testing.T, p *panelHarness, week ledger.Period) (*http.Response, string, [][]string) {
	t.Helper()
	res, err := p.client.Get(p.server.URL + reportsCSVHref + weekParam(week))
	if err != nil {
		t.Fatalf("GET the export: %v", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("reading the export: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET the export: %d, want 200 (body %.200q)", res.StatusCode, raw)
	}
	r := csv.NewReader(strings.NewReader(string(raw)))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("the downloaded file is not readable as CSV: %v", err)
	}
	return res, string(raw), rows
}

// TestPanelReportsCSVDB_ExportsOnlyItsOwnTenantsHours is §4.5 measured on the FILE,
// and it measures it twice: by the names in it and by the ARITHMETIC.
//
// 🔴 THE NUMERIC PROBE IS THE ONE THAT MATTERS. A missing tenant predicate on an
// export need not print anybody's name to be a breach: a payroll file whose total is
// two businesses' hours while every label in it looks right is the cheapest way for
// this to go wrong, and the file is the artefact that gets paid.
func TestPanelReportsCSVDB_ExportsOnlyItsOwnTenantsHours(t *testing.T) {
	p := newPanelHarness(t)
	week, zone := reportWeek(t)

	a := seedReportTenant(t, p, p.tenantID, "CsvAlpha", week, zone)

	bTenant := uuid.New()
	if err := p.data.WithTenant(context.Background(), bTenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Csv Bravo Ltd', $2, 'bar', 'single')`,
			bTenant, "VAT-"+bTenant.String())
		return e
	}); err != nil {
		t.Fatalf("insert tenant B: %v", err)
	}
	b := seedReportTenant(t, p, bTenant, "CsvBravo", week, zone)

	p.signIn(t)
	_, body, rows := downloadCSV(t, p, week)

	// POSITIVE CONTROL -- A's own week really is in the file. Without it, every
	// isolation assertion below is satisfied by an empty export.
	for _, want := range []string{a.location, a.employee, hoursMinutes(a.worked)} {
		if !findCell(rows, want) {
			t.Fatalf("the export does not carry tenant A's own %q, so every isolation "+
				"assertion here would be vacuous", want)
		}
	}

	// THE ISOLATION CLAIM, by name.
	for _, leak := range []struct{ what, text string }{
		{"another tenant's venue", b.location},
		{"another tenant's employee", b.employee},
		{"another tenant's employee id", b.employeeID.String()},
		{"another tenant's location id", b.locationID.String()},
		{"another tenant's business name", "Csv Bravo Ltd"},
	} {
		if strings.Contains(body, leak.text) {
			t.Errorf("§4.5 BREACH: the export carries %s (%q)", leak.what, leak.text)
		}
	}

	// THE ISOLATION CLAIM, by arithmetic. Both tenants worked the same week, so a lost
	// tenant predicate answers with twice the hours while every visible label stays
	// correct -- and in a file nothing else would notice.
	if combined := hoursMinutes(a.worked + b.worked); findCell(rows, combined) {
		t.Errorf("§4.5 BREACH: the export totals %s, which is A's hours PLUS B's", combined)
	}
	people := rowStartingWith(rows, "People with hours")
	if people == nil || people[1] != "1" {
		t.Errorf("the export covers %v people; A's week has exactly one", people)
	}
	// AND THE PREAMBLE NAMES THE RIGHT BUSINESS, which is what a reader with two
	// files in a folder tells them apart by. The expected name is READ FROM THE
	// DATABASE rather than written here: a fixture that quotes the harness's own
	// tenant name is a second representation of it, and this assertion caught itself
	// doing exactly that on its first run.
	var own string
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT name FROM tenants WHERE id = $1`, p.tenantID).Scan(&own)
	}); err != nil {
		t.Fatalf("reading the tenant's name: %v", err)
	}
	if biz := rowStartingWith(rows, "Business"); biz == nil || biz[1] != own {
		t.Errorf("the export's Business line is %v, want the signed-in tenant's name %q", biz, own)
	}
}

// TestPanelReportsCSVDB_NeverWritesACoordinateOrASourceAddressIntoTheFile is §4.7
// against rows that genuinely carry both.
//
// 🔴 IT MATTERS MORE HERE THAN ON THE SCREEN AND THE DIFFERENCE IS NOT RHETORICAL. A
// screen is looked at by somebody signed in and is gone when the tab closes; this file
// is downloaded, emailed to an accountant and kept. The rows behind it have gps_lat,
// gps_lng and source_ip, and the control at the bottom proves they do -- otherwise
// this sweep would pass over rows that never had one.
func TestPanelReportsCSVDB_NeverWritesACoordinateOrASourceAddressIntoTheFile(t *testing.T) {
	p := newPanelHarness(t)
	week, zone := reportWeek(t)
	f := seedReportTenant(t, p, p.tenantID, "CsvCoords", week, zone)

	p.signIn(t)
	_, body, rows := downloadCSV(t, p, week)

	// POSITIVE CONTROL -- the records really are behind this file.
	if !findCell(rows, f.employee) || !findCell(rows, hoursMinutes(f.worked)) {
		t.Fatal("the export shows none of the seeded week, so the absence of a coordinate " +
			"below proves nothing")
	}
	for _, secret := range []struct{ what, text string }{
		{"the latitude", theLat},
		{"the longitude", theLng},
		{"the source address", theIP},
	} {
		if strings.Contains(body, secret.text) {
			t.Errorf("§4.7 BREACH: the export carries %s (%q)", secret.what, secret.text)
		}
	}
	// AND NO COLUMN COULD HOLD ONE EITHER, which is the wall that survives a fixture
	// that happened not to have a coordinate in it.
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		switch row[0] {
		case "employee_id", "employee", "venue", "measure", "what":
		default:
			continue
		}
		for _, col := range row {
			if why := forbiddenColumn(col); why != "" {
				t.Errorf("§4.7 BREACH: the export has a column %q (%s)", col, why)
			}
		}
	}

	// THE OTHER HALF OF THE CONTROL: the database really holds them.
	var stored int
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM transactions
			 WHERE tenant_id = $1 AND gps_lat IS NOT NULL AND source_ip IS NOT NULL`,
			p.tenantID).Scan(&stored)
	}); err != nil {
		t.Fatalf("counting the seeded coordinates: %v", err)
	}
	if stored == 0 {
		t.Fatal("no seeded row carries a coordinate; the §4.7 sweep above is vacuous")
	}
}

// TestPanelReportsCSVDB_EscapesAFormulaThatCameOUTOfPostgres is the escaping measured
// end to end on data the product did not invent.
//
// 🔴 templ's ESCAPING NEVER TOUCHES THIS PATH, which is the whole warning M6-04 left
// for this task. An employee name is free text somebody types into the panel; it is
// stored VERBATIM (that is correct -- the database is not the place to mangle a name),
// it comes back through the real query, and encoding/csv escapes CSV syntax and knows
// nothing about spreadsheets. So this seeds a name that is a command-execution formula
// and requires the FILE to be safe while the DATABASE still holds the name as typed.
func TestPanelReportsCSVDB_EscapesAFormulaThatCameOUTOfPostgres(t *testing.T) {
	p := newPanelHarness(t)
	week, zone := reportWeek(t)

	const hostileName = `=cmd|' /C calc'!A0`
	// 🔴 THE VENUE HIDES ITS TRIGGER BEHIND A FORMAT CHARACTER (Cf), which is the class
	// that reached a downloaded file unescaped until the second review. A tab alone is
	// not enough here: unicode.IsSpace already covered that, so a fixture built only
	// from tabs could not tell the two versions of the escaper apart.
	const hostileVenue = "\uFEFF\t@SUM(A1:A9)"
	// And a zero-width space in front of the open entry's person, for the same reason.
	const hostileOpen = "\u200B+1+1 Borg"

	venueID, workerID, openID := uuid.New(), uuid.New(), uuid.New()
	err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name, static_ips, shift_start, shift_end)
			 VALUES ($1, $2, $3, '{203.0.113.0/24}', '10:00', '22:00')`,
			venueID, p.tenantID, hostileVenue); e != nil {
			return fmt.Errorf("location: %w", e)
		}
		for _, w := range []struct {
			id   uuid.UUID
			name string
		}{{workerID, hostileName}, {openID, hostileOpen}} {
			if _, e := tx.Exec(ctx,
				`INSERT INTO employees (id, tenant_id, location_id, full_name, status)
				 VALUES ($1, $2, $3, $4, 'active')`,
				w.id, p.tenantID, venueID, w.name); e != nil {
				return fmt.Errorf("employee: %w", e)
			}
		}
		rows := []struct {
			who uuid.UUID
			dir string
			at  time.Time
		}{
			{workerID, "in", localAt(week, zone, 0, 10, 0)},
			{workerID, "out", localAt(week, zone, 0, 18, 0)},
			// A second person whose check-in is never closed, so the hostile name
			// reaches the OPEN block as well as the per-person one.
			{openID, "in", localAt(week, zone, 1, 11, 0)},
		}
		for i, r := range rows {
			if _, e := tx.Exec(ctx,
				`INSERT INTO transactions
				   (tenant_id, employee_id, location_id, type, occurred_at, sun_valid, trust,
				    verdict, channel, practice, queued)
				 VALUES ($1,$2,$3,$4,$5,true,100,'ok','nfc',false,false)`,
				p.tenantID, r.who, venueID, r.dir, r.at); e != nil {
				return fmt.Errorf("transaction %d: %w", i, e)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seeding the hostile names: %v", err)
	}

	p.signIn(t)
	_, _, rows := downloadCSV(t, p, week)

	// POSITIVE CONTROL: the hostile names reached the file, escaped. Without this the
	// sweep below is satisfied by an export that simply lost them.
	for _, want := range []string{hostileName, hostileVenue, hostileOpen} {
		if !findCell(rows, "'"+want) {
			t.Fatalf("the escaped form of %q is not in the export; either it never got there "+
				"or it got there UNescaped", want)
		}
	}

	// 🔴 THE SWEEP, STRIPPING BEFORE IT LOOKS, exactly as a spreadsheet does.
	for r, row := range rows {
		for c, cell := range row {
			if readsAsFormula(cell) {
				t.Errorf("row %d cell %d is a FORMULA when this file is opened: %q", r+1, c, cell)
			}
		}
	}

	// 🔴 AND THE DATABASE STILL HOLDS THE NAME AS TYPED. The escape belongs to the
	// FILE, not to the record: rewriting a person's name on the way in would be the
	// product deciding what somebody is called.
	var stored string
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT full_name FROM employees WHERE id = $1`, workerID).Scan(&stored)
	}); err != nil {
		t.Fatalf("reading the stored name: %v", err)
	}
	if stored != hostileName {
		t.Errorf("the stored name is %q, want the typed %q -- the escaping has leaked into "+
			"the record", stored, hostileName)
	}

	// AND THE SCREEN IS UNHARMED BY THE SAME ROW, which is the other consumer of the
	// same name and uses a completely different defence (templ).
	res, page := p.get(t, reportsHref+weekParam(week))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET the report: %d", res.StatusCode)
	}
	if !strings.Contains(page, "=cmd|&#39; /C calc&#39;!A0") && !strings.Contains(page, "cmd|") {
		t.Error("the hostile name is not on the screen at all, so the two surfaces are not " +
			"being compared over the same row")
	}
}

// TestPanelReportsCSVDB_WritesTheExportToAuditLog is obligation 4 measured in
// Postgres rather than against a fake.
//
// 🔴 A BULK EXPORT IS AN EVENT. The screen shows one week to one person; the file
// leaves the product, so the honest control on it is a record of who took what. This
// is the reports section's ONLY write, and it lands under RLS in the exporting
// tenant's own rows.
func TestPanelReportsCSVDB_WritesTheExportToAuditLog(t *testing.T) {
	p := newPanelHarness(t)
	week, zone := reportWeek(t)
	f := seedReportTenant(t, p, p.tenantID, "CsvAudit", week, zone)

	p.signIn(t)

	// POSITIVE CONTROL: reading the SCREEN records nothing, so the row below is
	// attributable to the export rather than to any panel request.
	if _, _ = p.get(t, reportsHref+weekParam(week)); exportRows(t, p, week) != 0 {
		t.Fatal("reading the reports screen wrote an export row")
	}

	_, _, rows := downloadCSV(t, p, week)
	if !findCell(rows, f.employee) {
		t.Fatal("the export is empty, so the audit row below would describe nothing")
	}
	if n := exportRows(t, p, week); n != 1 {
		t.Fatalf("%d audit row(s) for one export, want 1", n)
	}

	// THE ROW'S CONTENT: who, which week, how much -- and nothing else.
	var actor *uuid.UUID
	var detail []byte
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT actor_id, detail FROM audit_log
			 WHERE tenant_id = $1 AND action = $2 AND target = $3`,
			p.tenantID, ActionReportExported, week.FirstDay.String()).Scan(&actor, &detail)
	}); err != nil {
		t.Fatalf("reading the audit row: %v", err)
	}
	if actor == nil || *actor != p.adminID {
		t.Errorf("the audit row names actor %v, want the signed-in admin %v", actor, p.adminID)
	}
	var got reportExportDetail
	if err := json.Unmarshal(detail, &got); err != nil {
		t.Fatalf("the stored detail is not the typed struct: %v (%s)", err, detail)
	}
	if got.Week != week.FirstDay.String() || got.People != 1 || got.Bytes == 0 {
		t.Errorf("the stored detail does not describe the export: %+v", got)
	}
	if want := wholeMinutes(f.worked); got.WorkedMinutes != want {
		t.Errorf("the audit says %d worked minutes, the week is %d", got.WorkedMinutes, want)
	}
	// 🔴 §4.7 IN THE TRAIL. internal/audit cannot scrub a jsonb column, so the CALLER
	// must hand it nothing to scrub: counts, never a second copy of the week.
	low := strings.ToLower(string(detail))
	for _, secret := range []string{strings.ToLower(f.employee), strings.ToLower(f.location), theLat, theLng, theIP, "gps", "token"} {
		if strings.Contains(low, secret) {
			t.Errorf("§4.7 BREACH: the audit detail carries %q (%s)", secret, detail)
		}
	}

	// A SECOND DOWNLOAD IS A SECOND EVENT, which is the property that makes this trail
	// worth reading: the question it answers is how many times a payroll left, not
	// whether it ever did.
	downloadCSV(t, p, week)
	if n := exportRows(t, p, week); n != 2 {
		t.Errorf("two downloads recorded %d row(s), want 2", n)
	}
}

// exportRows counts this tenant's export events for one week.
func exportRows(t *testing.T, p *panelHarness, week ledger.Period) int {
	t.Helper()
	var n int
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2 AND target = $3`,
			p.tenantID, ActionReportExported, week.FirstDay.String()).Scan(&n)
	}); err != nil {
		t.Fatalf("counting export audit rows: %v", err)
	}
	return n
}

// TestPanelReportsCSVDB_KeepsTrainingTapsOutOfTheFileToo is §5 and the M6-07 card's
// most detailed criterion, measured on the FILE.
//
// 🔴 A PRACTICE ROW IS A type='in' WITH NO 'out' AFTER IT (ADR 0008) -- in SQL, the
// exact shape of a forgotten checkout. On the screen that mistake puts every new
// starter into their manager's needs-action queue; in a CSV it puts them into whatever
// the accountant does with the "open" block, and nobody looks at a file twice.
func TestPanelReportsCSVDB_KeepsTrainingTapsOutOfTheFileToo(t *testing.T) {
	p := newPanelHarness(t)
	week, zone := reportWeek(t)
	f := seedReportTenant(t, p, p.tenantID, "CsvPractice", week, zone)

	// A GENUINE unclosed check-in by a SECOND person, so the open block is not empty
	// for some other reason.
	other := uuid.New()
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status)
			 VALUES ($1, $2, $3, 'Csv Forgetful Worker', 'active')`,
			other, p.tenantID, f.locationID); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO transactions
			   (tenant_id, employee_id, location_id, type, occurred_at, sun_valid, trust,
			    verdict, channel, practice, queued)
			 VALUES ($1,$2,$3,'in',$4,true,100,'ok','nfc',false,false)`,
			p.tenantID, other, f.locationID, localAt(week, zone, 5, 11, 0))
		return e
	}); err != nil {
		t.Fatalf("seeding the forgotten checkout: %v", err)
	}

	p.signIn(t)
	_, _, rows := downloadCSV(t, p, week)

	// POSITIVE CONTROL: the open block exists and holds the genuine one.
	openAt := -1
	for i, row := range rows {
		if len(row) > 0 && strings.HasPrefix(row[0], "Open check-ins") {
			openAt = i
		}
	}
	if openAt < 0 {
		t.Fatal("the export has no open block, so the practice assertion below would be vacuous")
	}
	block := rows[openAt+1:]
	if !findCell(block, "Csv Forgetful Worker") {
		t.Fatal("the genuine unclosed check-in is not in the file, so the assertion below " +
			"would pass over an empty block")
	}
	// The block holds a heading row and exactly ONE entry.
	if got := len(block) - 1; got != 1 {
		t.Errorf("the open block holds %d entries, want 1; a training tap has joined the file", got)
	}
	if findCell(block, f.employee) {
		t.Errorf("§5 BREACH: %q is in the export's open block, and the only unclosed record "+
			"they have is a TRAINING tap", f.employee)
	}
	// The practice person's own hours are unchanged by it...
	if !findCell(rows, hoursMinutes(f.worked)) {
		t.Errorf("the export does not total %s; a training tap has entered the hours",
			hoursMinutes(f.worked))
	}
	// ...AND IT IS STILL VISIBLE AS ITSELF (§4.6: excluded is not the same as
	// invisible). A file whose reader cannot ask needs this more than a screen does.
	training := rowStartingWith(rows, "Training taps")
	if training == nil || training[1] == "0" {
		t.Errorf("the export does not count the training tap (%v); the file silently differs "+
			"from the transactions list", training)
	}
}

// TestPanelReportsCSVDB_TheDownloadIsAFileAndSaysSo drives the real client over real
// HTTP and reads the headers off the wire, because a header set on a recorder and a
// header that survives net/http are two different claims.
func TestPanelReportsCSVDB_TheDownloadIsAFileAndSaysSo(t *testing.T) {
	p := newPanelHarness(t)
	week, zone := reportWeek(t)
	seedReportTenant(t, p, p.tenantID, "CsvHeaders", week, zone)

	p.signIn(t)
	res, body, _ := downloadCSV(t, p, week)

	for _, c := range []struct{ name, want string }{
		{"Content-Type", "text/csv; charset=utf-8"},
		{"X-Content-Type-Options", "nosniff"},
		{"Cache-Control", "no-store"},
		{"Content-Disposition", `attachment; filename="tappa-hours-` + week.FirstDay.String() + `.csv"`},
	} {
		if got := res.Header.Get(c.name); got != c.want {
			t.Errorf("%s = %q, want %q", c.name, got, c.want)
		}
	}
	if got, want := res.ContentLength, int64(len(body)); got != want {
		t.Errorf("Content-Length = %d, the body is %d -- a truncated download would look complete", got, want)
	}
	// AND THE BYTES ON THE WIRE START WITH THE MARK, which is what a spreadsheet reads
	// to decide it is UTF-8.
	if !strings.HasPrefix(body, reportCSVBOM) {
		t.Error("the downloaded bytes carry no byte order mark")
	}
}
