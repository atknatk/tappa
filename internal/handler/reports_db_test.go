package handler

// reports_db_test.go -- M6-07 phase A's red-line proofs, against REAL Postgres, over
// REAL HTTP, with a real panel session.
//
// 🔴 WHY THESE CANNOT BE UNIT TESTS, which is the difference between a net and a
// tautology. The rules this task touches are:
//
//	§4.5  tenant A's report must never total tenant B's hours
//	§4.7  a raw coordinate must never reach a screen -- or, at phase B, a file
//	§5    a practice tap is in NO total and in NO "needs action" queue
//
// A fake reader proves none of them: it hands the handler a value that has no
// coordinate field, no foreign row and no practice row in it, so every assertion
// passes because nothing could ever have failed. The only way to measure them is to
// put rows in the database that HAVE a coordinate, a second tenant that HAS hours, and
// a practice tap that IS a type='in' with no checkout after it.
//
// EVERY TEST BELOW OPENS WITH A POSITIVE CONTROL, for the reason the transactions
// file gives: "B's hours are absent" is satisfied perfectly by a page that renders
// nothing at all, and "no coordinate appears" by a page with no records on it.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/ledger"
)

// reportFixture is one tenant's worth of worked time.
type reportFixture struct {
	tenantID   uuid.UUID
	locationID uuid.UUID
	employeeID uuid.UUID
	employee   string
	location   string
	// worked is what the report must say this tenant's week adds up to, computed
	// here from the shifts below rather than read back from the page.
	worked time.Duration
}

// reportWeek is the week every case reports on: the Monday-to-Sunday week containing
// 3 June 2026, derived rather than counted so the fixture cannot disagree with the
// code about which day a week starts on. June is CEST, well clear of both DST seams --
// the transitions are internal/domain/ledger's to prove and are proved there.
func reportWeek(t *testing.T) (ledger.Period, *time.Location) {
	t.Helper()
	zone, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatalf("Europe/Malta: %v", err)
	}
	return ledger.WeekOf(ledger.Date{Year: 2026, Month: time.June, Day: 3}, zone), zone
}

// localAt is a wall clock inside that week: day 0 is the Monday.
func localAt(p ledger.Period, zone *time.Location, dayOffset, hh, mm int) time.Time {
	base := time.Date(p.FirstDay.Year, p.FirstDay.Month, p.FirstDay.Day, 0, 0, 0, 0, zone)
	return base.AddDate(0, 0, dayOffset).Add(time.Duration(hh)*time.Hour + time.Duration(mm)*time.Minute).UTC()
}

// seedReportTenant creates a venue with a 10:00-22:00 shift, one employee, and a week
// of records: an ordinary day shift, an OVERNIGHT shift that crosses midnight, and a
// practice tap that is never closed.
//
// THE PRACTICE ROW IS THE POINT OF SEEDING IT AT ALL. §5 and the M6-07 card both say a
// training tap counts toward nothing, and ADR 0008 records what it used to do instead:
// a type='in' with no 'out' after it, which is indistinguishable in SQL from somebody
// who forgot to tap out. Without one in the database, the exclusion is untested.
func seedReportTenant(t *testing.T, p *panelHarness, tenantID uuid.UUID, label string, week ledger.Period, zone *time.Location) reportFixture {
	t.Helper()
	f := reportFixture{
		tenantID:   tenantID,
		location:   label + " Venue",
		employee:   label + " Worker",
		locationID: uuid.New(), employeeID: uuid.New(),
	}

	type row struct {
		dir      string
		at       time.Time
		practice bool
	}
	rows := []row{
		// Monday 10:00 -> 18:30 = 8h30m.
		{dir: "in", at: localAt(week, zone, 0, 10, 0)},
		{dir: "out", at: localAt(week, zone, 0, 18, 30)},
		// Wednesday 18:00 -> Thursday 02:00 = 8h, one interval across midnight (§5).
		{dir: "in", at: localAt(week, zone, 2, 18, 0)},
		// 🔴 A TRAINING TAP SITTING BETWEEN THEM, which is ADR 0008's exact shape and
		// is why it is placed here rather than somewhere harmless. If the read stops
		// excluding practice rows, this one becomes the open check-in the 02:00 exit
		// closes -- so the overnight entry above is orphaned, the week loses two and a
		// half hours, and a new starter appears in their manager's needs-action queue.
		// Measured: with `AND NOT t.practice` deleted and sqlc regenerated, both halves
		// of this file's practice test go red.
		{dir: "in", at: localAt(week, zone, 2, 20, 30), practice: true},
		{dir: "out", at: localAt(week, zone, 3, 2, 0)},
	}
	f.worked = 8*time.Hour + 30*time.Minute + 8*time.Hour

	err := p.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name, static_ips, shift_start, shift_end)
			 VALUES ($1, $2, $3, '{203.0.113.0/24}', '10:00', '22:00')`,
			f.locationID, tenantID, f.location); e != nil {
			return fmt.Errorf("location: %w", e)
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status)
			 VALUES ($1, $2, $3, $4, 'active')`,
			f.employeeID, tenantID, f.locationID, f.employee); e != nil {
			return fmt.Errorf("employee: %w", e)
		}
		for i, r := range rows {
			// EVERY ROW CARRIES A COORDINATE AND A SOURCE ADDRESS, which is what makes
			// the §4.7 assertion below a measurement rather than a restatement of the
			// struct definition.
			if _, e := tx.Exec(ctx,
				`INSERT INTO transactions
				   (tenant_id, employee_id, location_id, type, occurred_at, source_ip,
				    ip_match, gps_lat, gps_lng, gps_match, sun_valid, trust, verdict,
				    channel, practice, queued)
				 VALUES ($1,$2,$3,$4,$5,$6::inet,true,$7::numeric,$8::numeric,true,true,100,
				         'ok','nfc',$9,false)`,
				tenantID, f.employeeID, f.locationID, r.dir, r.at, theIP, theLat, theLng,
				r.practice); e != nil {
				return fmt.Errorf("transaction %d: %w", i, e)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seeding %s: %v", label, err)
	}
	return f
}

// weekParam is the query string the section takes.
func weekParam(p ledger.Period) string {
	return "?week=" + p.FirstDay.String()
}

// TestPanelReportsDB_TotalsOnlyItsOwnTenantsHours is §4.5 measured end to end, and it
// measures it TWICE: by the names on the page and by the ARITHMETIC.
//
// 🔴 THE NUMERIC PROBE IS THE ONE THAT MATTERS HERE. A leak on this section would not
// necessarily print anybody's name -- a report is a SUM, and the cheapest way for a
// missing tenant predicate to show up is a total that is too big while every label on
// the page still looks right. So tenant B is given a DIFFERENT number of hours, and
// A's page is required to say A's figure exactly and never the combined one.
func TestPanelReportsDB_TotalsOnlyItsOwnTenantsHours(t *testing.T) {
	p := newPanelHarness(t)
	week, zone := reportWeek(t)

	a := seedReportTenant(t, p, p.tenantID, "Alpha", week, zone)

	// Tenant B is a COMPLETE second business with its own week. It has no admin and
	// never signs in -- it exists to be invisible.
	bTenant := uuid.New()
	if err := p.data.WithTenant(context.Background(), bTenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Report Bravo Ltd', $2, 'bar', 'single')`,
			bTenant, "VAT-"+bTenant.String())
		return e
	}); err != nil {
		t.Fatalf("insert tenant B: %v", err)
	}
	b := seedReportTenant(t, p, bTenant, "Bravo", week, zone)

	p.signIn(t)
	res, body := p.get(t, reportsHref+weekParam(week))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET the report: %d, want 200", res.StatusCode)
	}

	// POSITIVE CONTROL -- A's own week really is on the page. Without this, every
	// assertion below would be satisfied by an error page or an empty week.
	for _, want := range []string{a.location, a.employee, hoursMinutes(a.worked)} {
		if !strings.Contains(body, want) {
			t.Fatalf("the report does not show tenant A's own %q, so every isolation "+
				"assertion here would be vacuous", want)
		}
	}

	// THE ISOLATION CLAIM, by name.
	for _, leak := range []struct{ what, text string }{
		{"another tenant's venue", b.location},
		{"another tenant's employee", b.employee},
		{"another tenant's employee id", b.employeeID.String()},
		{"another tenant's location id", b.locationID.String()},
	} {
		if strings.Contains(body, leak.text) {
			t.Errorf("§4.5 BREACH: the report rendered %s (%q)", leak.what, leak.text)
		}
	}

	// THE ISOLATION CLAIM, by arithmetic. Both tenants worked the same week, so a
	// query that lost its tenant predicate would answer with twice the hours and
	// twice the people while every visible label stayed correct.
	if got := hoursMinutes(a.worked + b.worked); strings.Contains(body, got) {
		t.Errorf("§4.5 BREACH: the report totals %s, which is A's hours PLUS B's", got)
	}
	if !strings.Contains(body, "1 person") {
		t.Error("§4.5 BREACH or an empty page: A's report must cover exactly one person")
	}
}

// TestPanelReportsDB_ExcludesTrainingTapsFromHoursAndFromTheNeedsActionList is the
// M6-07 card's most detailed requirement, measured on a real practice row.
//
// 🔴 A PRACTICE ROW IS A type='in' WITH NO 'out' AFTER IT (ADR 0008). In SQL that is
// exactly the shape of a forgotten checkout, so without the exclusion every new starter
// would appear in their manager's "needs action" queue on their first day -- which is
// the defect the card spells out at length. The positive control is beside it: the
// SAME page must show a genuine open entry, so "the practice row is absent" cannot be
// satisfied by an open list that is empty for some other reason.
func TestPanelReportsDB_ExcludesTrainingTapsFromHoursAndFromTheNeedsActionList(t *testing.T) {
	p := newPanelHarness(t)
	week, zone := reportWeek(t)
	f := seedReportTenant(t, p, p.tenantID, "Practice", week, zone)

	// A GENUINE unclosed check-in, by a SECOND person, so the open list is not empty.
	other := uuid.New()
	forgotten := localAt(week, zone, 5, 11, 0)
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status)
			 VALUES ($1, $2, $3, 'Forgetful Worker', 'active')`,
			other, p.tenantID, f.locationID); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO transactions
			   (tenant_id, employee_id, location_id, type, occurred_at, sun_valid, trust,
			    verdict, channel, practice, queued)
			 VALUES ($1,$2,$3,'in',$4,true,100,'ok','nfc',false,false)`,
			p.tenantID, other, f.locationID, forgotten)
		return e
	}); err != nil {
		t.Fatalf("seeding the forgotten checkout: %v", err)
	}

	p.signIn(t)
	_, body := p.get(t, reportsHref+weekParam(week))

	// POSITIVE CONTROL: the open list exists and holds the genuine one.
	if !strings.Contains(body, "Open — no checkout") || !strings.Contains(body, "Forgetful Worker") {
		t.Fatal("the genuine unclosed check-in is not listed, so the practice assertion below " +
			"would pass over an empty list")
	}
	// 🔴 THE CLAIM IS SCOPED TO THE OPEN SECTION, AND THE FIRST VERSION OF IT WAS NOT.
	// It searched the WHOLE page for "1 check-in" -- and measured against the real
	// mutation (deleting `AND NOT t.practice` from ListWorkedShiftEvents, regenerating,
	// re-running) it stayed GREEN, because with two open entries each PERSON's row says
	// "1 check-in with no checkout" and the substring was satisfied by the wrong thing.
	// A count inside the section is what the assertion was always meant to be.
	openSection := body[strings.Index(body, "Open — no checkout"):]
	if n := strings.Count(openSection, `<section class="docket">`); n != 1 {
		t.Errorf("the open list holds %d entries, want 1; the training tap has joined the "+
			"needs-action queue", n)
	}
	if strings.Contains(openSection, f.employee) {
		t.Errorf("§5 BREACH: %q is in the needs-action list, and the only unclosed record "+
			"they have is a TRAINING tap", f.employee)
	}
	// The practice tap's own person worked the two real shifts and NOTHING for the
	// training tap, so their total is unchanged by it.
	if !strings.Contains(body, hoursMinutes(f.worked)) {
		t.Errorf("the week does not total %s; a training tap has entered the hours", hoursMinutes(f.worked))
	}
	// AND IT IS STILL VISIBLE AS ITSELF (§4.6: excluded is not the same as invisible).
	if !strings.Contains(body, "1 training tap, not counted") {
		t.Error("the training tap is excluded and never mentioned, so the report silently " +
			"differs from the transactions list")
	}
}

// TestPanelReportsDB_PairsAnOvernightShiftAcrossMidnight is §5's named case measured
// through the real query rather than over a hand-built event slice: the seed's
// Wednesday 18:00 entry and Thursday 02:00 exit have to arrive as ONE interval.
//
// IT IS HERE AS WELL AS IN THE DOMAIN TESTS because the two halves can fail
// independently: the arithmetic could pair correctly over rows the ORDER BY delivered
// in the wrong sequence, or the read window could stop at the period's end and never
// see the closing tap at all.
func TestPanelReportsDB_PairsAnOvernightShiftAcrossMidnight(t *testing.T) {
	p := newPanelHarness(t)
	week, zone := reportWeek(t)
	f := seedReportTenant(t, p, p.tenantID, "Night", week, zone)

	p.signIn(t)
	_, body := p.get(t, reportsHref+weekParam(week))

	if !strings.Contains(body, hoursMinutes(f.worked)) {
		t.Fatalf("the week does not total %s: the 18:00 entry and the 02:00 exit did not "+
			"pair into one interval", hoursMinutes(f.worked))
	}
	if !strings.Contains(body, "2 shifts") {
		t.Error("the week is not two shifts; an overnight pair has been split or dropped")
	}
	// AND IT COUNTS ON THE DAY IT STARTED. The Wednesday column carries the whole 8h
	// and the Thursday column carries nothing -- which is the attribution rule the
	// screen states in words, measured here rather than believed.
	if !strings.Contains(body, "A shift counts on the day it STARTED") {
		t.Error("the screen does not say which attribution rule it used")
	}
}

// TestPanelReportsDB_NeverRendersACoordinateOrASourceAddress is §4.7 measured against
// rows that genuinely carry both.
//
// 🔴 THE ROWS BEHIND THIS PAGE HAVE gps_lat, gps_lng AND source_ip. That is what makes
// the assertion a measurement: the query does not select them, the domain types have
// no field for them and the view has none either, and this proves the composition
// rather than any one of the three.
func TestPanelReportsDB_NeverRendersACoordinateOrASourceAddress(t *testing.T) {
	p := newPanelHarness(t)
	week, zone := reportWeek(t)
	f := seedReportTenant(t, p, p.tenantID, "Coords", week, zone)

	p.signIn(t)
	_, body := p.get(t, reportsHref+weekParam(week))

	// POSITIVE CONTROL -- the records really are behind this page.
	if !strings.Contains(body, f.employee) || !strings.Contains(body, hoursMinutes(f.worked)) {
		t.Fatal("the report shows none of the seeded week, so the absence of a coordinate " +
			"below proves nothing")
	}
	for _, secret := range []struct{ what, text string }{
		{"the latitude", theLat},
		{"the longitude", theLng},
		{"the source address", theIP},
	} {
		if strings.Contains(body, secret.text) {
			t.Errorf("§4.7 BREACH: the report rendered %s (%q)", secret.what, secret.text)
		}
	}
	// AND THE DATABASE REALLY HOLDS THEM, which is the other half of the control: if
	// the seed had failed to write a coordinate, the sweep above would pass over rows
	// that never had one.
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
