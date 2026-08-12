package handler

// anomalies_db_test.go -- M6-11's red-line proofs, against REAL Postgres, over REAL
// HTTP, with a real panel session.
//
// 🔴 WHY THESE CANNOT BE UNIT TESTS. The rules this section touches are:
//
//	§4.5  tenant A's anomaly page must never count tenant B's records
//	§4.6  reading this screen must not write a single row anywhere
//	§4.7  a coordinate or a source address must never reach the page
//	§5    a training tap is NOT a check-in with no checkout
//
// A fake reader proves none of them: it hands the handler a value that has no
// coordinate, no foreign row and no practice row in it, so every assertion passes
// because nothing could ever have failed. The only way to measure them is to put rows in
// the database that HAVE a coordinate, a second tenant that HAS signals, a practice row
// that IS a type='in' with no checkout after it, and a policy snapshot that DOES carry a
// counter gap.
//
// EVERY TEST BELOW OPENS WITH A POSITIVE CONTROL, for the reason the transactions file
// gives: "B's records are absent" is satisfied perfectly by a page that renders nothing
// at all.

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/ledger"
)

// anomalyFixture is one tenant's worth of odd shapes.
type anomalyFixture struct {
	tenantID   uuid.UUID
	locationID uuid.UUID
	employeeID uuid.UUID
	mateID     uuid.UUID
	employee   string
	mate       string
	location   string
	tagUID     string
	// versionID is a REAL policy_versions row.
	//
	// 🔴 IT EXISTS BECAUSE THE SCHEMA REFUSES THE SHORTCUT, and the shortcut was what
	// this fixture used to take. migration 0008's transactions_policy_decision_consistent
	// says a record on the `baseline` or `tenant` layer MUST name a policy version,
	// while a `guardrail` record must NOT -- so stamping every rule as a guardrail was
	// the only way to write these rows without one. That produced
	// `base:ip-or-gps-ok / guardrail`, a pairing §5 makes impossible (`sys:*` is the
	// guardrail layer, `base:*` the baseline) and therefore a row no tap could record.
	// A rule-breakdown assertion verified against an impossible row is verified against
	// nothing, so the fixture now writes the policy and the version the real path would.
	versionID uuid.UUID
}

// anomalyRow is one record to plant, written so the fixture reads as a list of SHAPES
// rather than a list of column values.
type anomalyRow struct {
	dir        string // "in", "out", or "" for a record with no direction
	at         time.Time
	who        uuid.UUID
	practice   bool
	manual     bool
	ipMatch    bool
	gpsMatch   bool
	ctrGap     int
	conflict   bool
	crossVenue bool
	// noSnapshot plants a record with policy_context NULL, which is the shape every
	// pre-M3-07 record has and the one the screen must report as unanswerable rather
	// than as clean.
	noSnapshot bool
	sid        string
}

// seedAnomalyTenant plants one venue, two people, one plaque and a week of shapes.
//
// 🔴 EVERY ROW CARRIES A COORDINATE AND A SOURCE ADDRESS, which is what makes the §4.7
// assertion below a measurement rather than a restatement of a struct definition.
func seedAnomalyTenant(t *testing.T, p *panelHarness, tenantID uuid.UUID, label string,
	week ledger.Period, zone *time.Location,
) anomalyFixture {
	t.Helper()
	// 🔴 THE PLAQUE ID IS GENERATED, NOT WRITTEN DOWN, and that is a measured
	// correction rather than caution. `transactions` is immutable and `tags` is
	// REVOKE DELETE, so nothing this fixture writes can ever be cleaned up: a
	// hard-coded uid passed on the first `make test` run and failed on the second with
	// a primary-key collision. The schema wants 14 upper-case hex characters.
	tagUID := strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))[:14]
	f := anomalyFixture{
		tenantID:   tenantID,
		location:   label + " Venue",
		employee:   label + " Worker",
		mate:       label + " Mate",
		locationID: uuid.New(), employeeID: uuid.New(), mateID: uuid.New(),
		tagUID: tagUID, versionID: uuid.New(),
	}
	policyID := uuid.New()

	at := func(day, hh, mm, ss int) time.Time {
		base := time.Date(week.FirstDay.Year, week.FirstDay.Month, week.FirstDay.Day, 0, 0, 0, 0, zone)
		return base.AddDate(0, 0, day).
			Add(time.Duration(hh)*time.Hour + time.Duration(mm)*time.Minute + time.Duration(ss)*time.Second).UTC()
	}

	rows := []anomalyRow{
		// A clean day: address matched at both ends.
		{dir: "in", at: at(0, 9, 0, 0), who: f.employeeID, ipMatch: true, gpsMatch: true, sid: "base:ip-or-gps-ok"},
		{dir: "out", at: at(0, 17, 0, 0), who: f.employeeID, ipMatch: true, gpsMatch: true, sid: "base:ip-or-gps-ok"},
		// GPS ALONE: inside the radius, no address match.
		{dir: "in", at: at(1, 9, 0, 0), who: f.employeeID, gpsMatch: true, sid: "base:gps-only-allow"},
		{dir: "out", at: at(1, 17, 0, 0), who: f.employeeID, gpsMatch: true, sid: "base:gps-only-allow"},
		// TWO COUNTER GAPS ON THE SAME PLAQUE, OF DIFFERENT SIZES (3 and 5).
		//
		// 🔴 THE SECOND ONE EXISTS BECAUSE `max` AND `sum` AGREE ON A SINGLE ROW. With
		// one gap per plaque the fixture could not tell them apart, and a mutation
		// measured the consequence: changing `max(...)` to `sum(...)` in
		// ListAnomalyPlaques left the WHOLE suite green while the screen printed a jump
		// nobody's counter ever made. With 3 and 5 present, max is 5 and sum is 8; the
		// assertions below name 5 and refuse 8.
		{dir: "in", at: at(2, 9, 0, 0), who: f.employeeID, ipMatch: true, ctrGap: 3, sid: "base:ctr-gap-review"},
		{dir: "out", at: at(2, 17, 0, 0), who: f.employeeID, ipMatch: true, ctrGap: 5, sid: "base:ctr-gap-review"},
		// A READING OUTSIDE THE VENUE that ALSO answered from the venue's address range
		// -- the shape the M6-11 card was reaching for, and one that does NOT appear in
		// the GPS-only count.
		{dir: "in", at: at(3, 9, 0, 0), who: f.employeeID, ipMatch: true, conflict: true, sid: "base:no-evidence-review"},
		{dir: "out", at: at(3, 17, 0, 0), who: f.employeeID, ipMatch: true, sid: "base:ip-or-gps-ok"},
		// A TAP AT ANOTHER VENUE.
		{dir: "in", at: at(4, 9, 0, 0), who: f.employeeID, ipMatch: true, crossVenue: true, sid: "base:ip-or-gps-ok"},
		{dir: "out", at: at(4, 17, 0, 0), who: f.employeeID, ipMatch: true, sid: "base:ip-or-gps-ok"},
		// A RECORD WITH NO POLICY SNAPSHOT AT ALL -- every pre-M3-07 row's shape.
		{dir: "in", at: at(5, 8, 0, 0), who: f.employeeID, ipMatch: true, noSnapshot: true},
		{dir: "out", at: at(5, 16, 0, 0), who: f.employeeID, ipMatch: true, noSnapshot: true},

		// THE MATE, who arrives seconds after the worker on three separate days. §5
		// calls one queue ordinary, which is why the threshold is a number of DAYS.
		{dir: "in", at: at(0, 9, 0, 4), who: f.mateID, ipMatch: true, sid: "base:ip-or-gps-ok"},
		{dir: "out", at: at(0, 17, 0, 6), who: f.mateID, ipMatch: true, sid: "base:ip-or-gps-ok"},
		{dir: "in", at: at(1, 9, 0, 5), who: f.mateID, ipMatch: true, sid: "base:ip-or-gps-ok"},
		{dir: "out", at: at(1, 17, 0, 3), who: f.mateID, ipMatch: true, sid: "base:ip-or-gps-ok"},
		{dir: "in", at: at(2, 9, 0, 2), who: f.mateID, ipMatch: true, sid: "base:ip-or-gps-ok"},
		{dir: "out", at: at(2, 17, 0, 7), who: f.mateID, ipMatch: true, sid: "base:ip-or-gps-ok"},

		// 🔴 A TRAINING TAP. It is a type='in' that nothing ever closes -- ADR 0008's
		// exact shape -- so it is the false positive the open list must NOT produce.
		{dir: "in", at: at(6, 10, 0, 0), who: f.mateID, practice: true, ipMatch: true, sid: "base:ip-or-gps-ok"},
		// AND A REAL OPEN CHECK-IN, BENEATH IT IN TIME, so the masked-open counter has
		// something honest to find: this entry has a practice row AFTER it.
		{dir: "in", at: at(6, 9, 0, 0), who: f.mateID, ipMatch: true, sid: "base:ip-or-gps-ok"},
		// A MANAGER-TYPED ROW: no address, no reading, no snapshot.
		{dir: "in", at: at(5, 20, 0, 0), who: f.employeeID, manual: true, noSnapshot: true},
		{dir: "out", at: at(5, 22, 0, 0), who: f.employeeID, manual: true, noSnapshot: true},
	}

	err := p.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name, static_ips, shift_start, shift_end)
			 VALUES ($1, $2, $3, '{203.0.113.0/24}', '09:00', '18:00')`,
			f.locationID, tenantID, f.location); e != nil {
			return fmt.Errorf("location: %w", e)
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status)
			 VALUES ($1, $2, $3, '\x00'::bytea, 100, 'active')`,
			f.tagUID, tenantID, f.locationID); e != nil {
			return fmt.Errorf("tag: %w", e)
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO policies (id, tenant_id, name, layer, enabled)
			 VALUES ($1, $2, $3, 'baseline', true)`,
			policyID, tenantID, label+" baseline"); e != nil {
			return fmt.Errorf("policy: %w", e)
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO policy_versions (id, tenant_id, policy_id, version_no, document)
			 VALUES ($1, $2, $3, 1, '{}'::jsonb)`,
			f.versionID, tenantID, policyID); e != nil {
			return fmt.Errorf("policy version: %w", e)
		}
		for _, who := range []struct {
			id   uuid.UUID
			name string
		}{{f.employeeID, f.employee}, {f.mateID, f.mate}} {
			if _, e := tx.Exec(ctx,
				`INSERT INTO employees (id, tenant_id, location_id, full_name, status)
				 VALUES ($1, $2, $3, $4, 'active')`,
				who.id, tenantID, f.locationID, who.name); e != nil {
				return fmt.Errorf("employee %s: %w", who.name, e)
			}
		}
		for i, r := range rows {
			channel := "nfc"
			if r.manual {
				channel = "manual"
			}
			// THE SNAPSHOT IS BUILT HERE RATHER THAN IN THE QUERY'S OWN WORDS, so the
			// test and the query agree only through the key names migration 0008 froze.
			ctx7 := map[string]any{
				"tap:ctrGap":             r.ctrGap,
				"tap:gpsConflict":        r.conflict,
				"employee:crossLocation": r.crossVenue,
			}
			var snapshot any
			var sid, layer, versionID any
			// 🔴 A MANAGER-TYPED ROW CARRIES NO EVIDENCE AT ALL, so its evidence
			// columns are NULL rather than false. The distinction is the whole point
			// of the `judged` denominator: `false` means "we looked and it did not
			// match", NULL means "there was nothing to look at". A fixture that wrote
			// false here would quietly put typed rows into every rate on the page.
			var ipMatch, gpsMatch any = r.ipMatch, r.gpsMatch
			if r.manual {
				ipMatch, gpsMatch = nil, nil
			}
			if !r.noSnapshot {
				snapshot = ctx7
			}
			if r.sid != "" {
				// 🔴 THE LAYER FOLLOWS THE RULE'S PREFIX, AND THE VERSION FOLLOWS THE
				// LAYER. §5 fixes `sys:*` as the guardrail layer and `base:*` as the
				// baseline; migration 0008's CHECK then requires a guardrail record to
				// carry NO policy version and a baseline record to carry one. Getting
				// either half wrong writes a row the product could never produce -- and
				// the CHECK refuses the half that is refusable, which is how the missing
				// version was found.
				sid = r.sid
				if strings.HasPrefix(r.sid, "sys:") {
					layer = "guardrail"
				} else {
					layer, versionID = "baseline", f.versionID
				}
			}
			if _, e := tx.Exec(ctx,
				`INSERT INTO transactions
				   (tenant_id, employee_id, location_id, tag_uid, type, occurred_at, source_ip,
				    ip_match, gps_lat, gps_lng, gps_match, sun_valid, trust, verdict,
				    channel, practice, queued, policy_context, matched_sid, policy_layer,
				    policy_version_id)
				 VALUES ($1,$2,$3,$4,$5,$6,$7::inet,$8,$9::numeric,$10::numeric,$11,true,100,
				         'ok',$12,$13,false,$14,$15,$16,$17)`,
				tenantID, r.who, f.locationID, f.tagUID, r.dir, r.at, theIP,
				ipMatch, theLat, theLng, gpsMatch, channel, r.practice,
				snapshot, sid, layer, versionID); e != nil {
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

// anomalyWeek is the week every case reports on. February 2026 is chosen because the
// development database's own records are in August: a fixture that shared a week with
// them would have its counts drown in whatever `make test` left behind.
func anomalyWeek(t *testing.T) (ledger.Period, *time.Location) {
	t.Helper()
	zone, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatalf("Europe/Malta: %v", err)
	}
	return ledger.WeekOf(ledger.Date{Year: 2026, Month: time.February, Day: 4}, zone), zone
}

func anomalyWeekParam(p ledger.Period) string { return "?week=" + p.FirstDay.String() }

// TestPanelAnomaliesDB_CountsOnlyItsOwnTenantsSignals is §4.5, measured twice: by the
// names on the page and by the ARITHMETIC.
//
// 🔴 THE NUMERIC PROBE IS THE ONE THAT MATTERS. A leak on this section would not
// necessarily print anybody's name — every figure here is a COUNT, so the cheapest way
// for a missing tenant predicate to show up is a total that is exactly twice as large
// while every label on the page still looks right.
func TestPanelAnomaliesDB_CountsOnlyItsOwnTenantsSignals(t *testing.T) {
	p := newPanelHarness(t)
	week, zone := anomalyWeek(t)

	a := seedAnomalyTenant(t, p, p.tenantID, "Alpha", week, zone)

	bTenant := uuid.New()
	if err := p.data.WithTenant(context.Background(), bTenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Anomaly Bravo Ltd', $2, 'bar', 'single')`,
			bTenant, "VAT-"+bTenant.String())
		return e
	}); err != nil {
		t.Fatalf("insert tenant B: %v", err)
	}
	b := seedAnomalyTenant(t, p, bTenant, "Bravo", week, zone)

	p.signIn(t)
	res, body := p.get(t, anomaliesHref+anomalyWeekParam(week))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET the anomalies page: %d, want 200", res.StatusCode)
	}

	// POSITIVE CONTROL -- A's own week really is on the page.
	for _, want := range []string{a.location, a.employee, a.tagUID} {
		if !strings.Contains(body, want) {
			t.Fatalf("the page does not show tenant A's own %q, so every isolation "+
				"assertion here would be vacuous", want)
		}
	}

	// THE ISOLATION CLAIM, by name.
	for _, leak := range []struct{ what, text string }{
		{"another tenant's venue", b.location},
		{"another tenant's employee", b.employee},
		{"another tenant's mate", b.mate},
		{"another tenant's plaque", b.tagUID},
		{"another tenant's employee id", b.employeeID.String()},
		{"another tenant's location id", b.locationID.String()},
	} {
		if strings.Contains(body, leak.text) {
			t.Errorf("§4.5 BREACH: the anomalies page rendered %s (%q)", leak.what, leak.text)
		}
	}

	// THE ISOLATION CLAIM, by arithmetic. Both tenants planted the SAME shapes in the
	// same week, so a query that lost its tenant predicate answers with exactly twice
	// the records while every visible label stays correct.
	single, both := 22, 44
	if !strings.Contains(body, fmt.Sprintf("%d records", single)) {
		t.Errorf("the page does not report %d records; the fixture plants that many for "+
			"one tenant, so either the count is wrong or the week is", single)
	}
	if strings.Contains(body, fmt.Sprintf("%d records", both)) {
		t.Errorf("§4.5 BREACH: the page reports %d records, which is A's rows PLUS B's", both)
	}
}

// TestPanelAnomaliesDB_ReadingTheScreenWritesNothing is §4.6 measured rather than
// claimed, with a positive control for the counter itself.
//
// 🔴 A COUNTER THAT CANNOT SEE A CHANGE WOULD REPORT "no writes" FOR A READER THAT WROTE
// A HUNDRED ROWS, which is why the same counting method is shown moving at the end. The
// tables counted are every one this path can reach: the records it reads, the audit log
// four other sections write to, and the review table the queue writes to.
func TestPanelAnomaliesDB_ReadingTheScreenWritesNothing(t *testing.T) {
	p := newPanelHarness(t)
	week, zone := anomalyWeek(t)
	f := seedAnomalyTenant(t, p, p.tenantID, "Quiet", week, zone)
	p.signIn(t)

	count := func() map[string]int {
		t.Helper()
		out := map[string]int{}
		err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
			for _, table := range []string{"transactions", "audit_log", "transaction_reviews", "sessions"} {
				var n int
				if e := tx.QueryRow(ctx,
					`SELECT count(*) FROM `+table+` WHERE tenant_id = $1`, p.tenantID).Scan(&n); e != nil {
					return fmt.Errorf("%s: %w", table, e)
				}
				out[table] = n
			}
			return nil
		})
		if err != nil {
			t.Fatalf("counting: %v", err)
		}
		return out
	}

	before := count()
	res, body := p.get(t, anomaliesHref+anomalyWeekParam(week))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET the anomalies page: %d, want 200", res.StatusCode)
	}
	// POSITIVE CONTROL for the READ: the page really rendered this tenant's week, so
	// the row counts below are about a request that did the work.
	if !strings.Contains(body, f.employee) {
		t.Fatalf("the page did not render the fixture, so 'reading it wrote nothing' is a " +
			"statement about a request that read nothing")
	}
	after := count()

	for table, n := range before {
		if after[table] != n {
			t.Errorf("§4.6/§4.3 BREACH: reading the anomalies screen changed %s from %d to "+
				"%d. This section is a read of an immutable table; a page view that "+
				"appended a row would be writing to the product's own evidence.",
				table, n, after[table])
		}
	}

	// POSITIVE CONTROL for the COUNTER.
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO audit_log (tenant_id, action, target, detail)
			 VALUES ($1, 'test.counter.probe', 'anomalies', '{}'::jsonb)`, p.tenantID)
		return e
	}); err != nil {
		t.Fatalf("positive control insert: %v", err)
	}
	if got := count(); got["audit_log"] == before["audit_log"] {
		t.Fatal("the row counter did not move after an INSERT, so the assertion above " +
			"could not have failed for a reader that wrote")
	}
}

// TestPanelAnomaliesDB_NoCoordinateAndNoAddressReachesThePage is §4.7 measured on rows
// that really carry one.
//
// 🔴 THE FIXTURE PLANTS A COORDINATE AND A SOURCE ADDRESS ON EVERY ROW. Without them
// this test would assert that a page with no coordinates on it has no coordinates on it.
func TestPanelAnomaliesDB_NoCoordinateAndNoAddressReachesThePage(t *testing.T) {
	p := newPanelHarness(t)
	week, zone := anomalyWeek(t)
	f := seedAnomalyTenant(t, p, p.tenantID, "Delta", week, zone)
	p.signIn(t)

	res, body := p.get(t, anomaliesHref+anomalyWeekParam(week))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET the anomalies page: %d, want 200", res.StatusCode)
	}
	// POSITIVE CONTROL: the records really are on the page.
	if !strings.Contains(body, f.employee) {
		t.Fatal("the fixture is not on the page; the §4.7 sweep below would be vacuous")
	}

	for _, leak := range []struct{ what, text string }{
		{"the latitude", theLat},
		{"the longitude", theLng},
		{"the source address", theIP},
		// ROUNDED AND TRUNCATED FORMS. A coordinate rounded to three decimals still
		// places somebody to about 110 m, and a literal-only sweep walks straight past
		// it -- measured on this exact page with `round(gps_lat::numeric, 3)::text`.
		{"the latitude, rounded", theLat[:6]},
		{"the longitude, rounded", theLng[:6]},
		{"the address, cut to its network", "203.0.113"},
		// And the key names of the frozen snapshot, which is never selected at all.
		{"a policy context key", "tap:gpsDistanceM"},
		{"a policy context key", "tap:gpsConflict"},
	} {
		if strings.Contains(body, leak.text) {
			t.Errorf("§4.7 BREACH: the anomalies page rendered %s (%q)", leak.what, leak.text)
		}
	}

	// 🔴 THE SHAPE SWEEP, WHICH IS THE HALF A LITERAL LIST CANNOT DO. Every literal
	// above names a value THIS fixture happens to use; a reformatted or differently
	// rounded coordinate is a different string and the list misses it. Measured: a
	// `round(gps_lat::numeric, 3)::text` column reached the rendered page while a
	// literal-only sweep answered PASS.
	//
	// WHAT IT MATCHES is the SHAPE of a degree: two or three whole digits and two or
	// more decimals. What this page legitimately prints -- counts, whole-number
	// percentages, clock times, seconds, plaque ids -- never takes that form, and the
	// accept half of TestDecimalDegreeShape_MatchesACoordinateAndNotThisPagesOwnNUMBERS
	// is what keeps that true.
	if m := decimalDegreeShape.FindAllString(body, -1); len(m) > 0 {
		t.Errorf("§4.7 BREACH: the anomalies page carries %d string(s) shaped like a "+
			"decimal degree: %q. A coordinate does not stop being one because it was "+
			"rounded or reformatted.", len(m), m)
	}
}

// decimalDegreeShape is the coordinate SHAPE rather than any particular coordinate.
//
// 🔴 THE FIRST VERSION WAS `\b\d{2,3}\.\d{2,}\b` AND IT WAS BLIND TO MOST OF THE
// WORLD AND TO ONE PUNCTUATION MARK. Two five-layer mutations walked past it:
//
//	replace(gps_lat::text, '.', ',')  ->  "35,898909"  a comma decimal separator
//	(gps_lat - 30)::text              ->   "5.898909"  ONE whole digit
//
// The second is the one that mattered. `\d{2,3}` cannot match a degree with fewer than
// two whole digits, which is every latitude and longitude between 0° and 10°: Accra
// (5.60, -0.19), Singapore (1.35), London (-0.13), Paris (2.35). Malta (35.9, 14.5) and
// Turkey happen to be the geographies the narrow pattern worked in, and nothing said so.
//
// WIDENING IT WAS MEASURED BEFORE IT WAS DONE, because a sweep that fires on honest
// output is one the next person deletes. Every decimal-shaped string on the rendered
// page was extracted first, on all three page shapes:
//
//	empty week                 0 decimal-shaped strings
//	the database fixture week  0
//	every listing at the cap   0
//
// The page prints no decimals at all -- percentages are whole numbers, clock times are
// colon-separated, every other figure is an integer -- so both the narrow and the wide
// pattern match nothing on any of the three. Widening costs zero false alarms, measured
// rather than argued.
//
// ⚠️ WHAT IT STILL CANNOT SEE, COUNTED RATHER THAN GLOSSED. The first version of this
// list said "COUNTED" while missing two classes a reviewer then demonstrated end to
// end, so the two are named first:
//
//	scientific notation      to_char(gps_lat,'9.99999999EEEE') renders a FULL-PRECISION
//	                         latitude as "3.58989090e+01" and every net stays green
//	non-ASCII separators     U+FF0E FULLWIDTH FULL STOP (35．898909) and U+066B ARABIC
//	                         DECIMAL SEPARATOR (35٫898909) are not the ASCII "." or ","
//	                         this pattern knows about
//	one decimal place        "35.9" is ~11 km and is deliberately not matched; two
//	                         places is where a reading starts locating somebody
//	other separators         a space, a degree sign, or degrees/minutes/seconds
//	encoded forms            hex, base64, or a coordinate split across two fields
//	four whole digits        a value beyond ±180 is not a degree, so it is not matched
//
// 🔴 WIDENING TO COVER THE FIRST TWO WOULD BE FREE, AND IT WAS MEASURED BEFORE BEING
// DECLINED. Both extra shapes were compiled and run against all three rendered pages
// beside the shipped pattern: `\d[.,]\d+[eE][+-]?\d+` and
// `\d{1,3}[\x{FF0E}\x{066B}\x{2024}]\d{2,}` matched NOTHING on any of them, so the
// false-alarm cost of adding them is zero, exactly as it was for the widening that DID
// happen. They are counted rather than added because no shipped path produces either
// form — every one of these queries selects counts and names — so adding them chases a
// shape nothing writes. The measurement is here so the next reader can reverse that
// call without re-taking it.
//
// AND ONE FALSE-ALARM SHAPE IT WOULD NOW HIT: a three-part version string such as
// "1.28.17" matches on its middle pair. Nothing on this page prints one (measured
// above); if one ever appears, this is where it will fire.
var decimalDegreeShape = regexp.MustCompile(`\b\d{1,3}[.,]\d{2,}\b`)

// TestDecimalDegreeShape_MatchesACoordinateAndNotThisPagesOwnNUMBERS controls the sweep
// in both directions.
//
// 🔴 THE ACCEPT HALF IS AS LOAD-BEARING AS THE REFUSE HALF. A sweep that fires on
// honest output is one the next person deletes rather than fixes, so every numeric form
// this section really prints is required to pass it.
func TestDecimalDegreeShape_MatchesACoordinateAndNotThisPagesOwnNUMBERS(t *testing.T) {
	for _, probe := range []string{
		theLat, theLng, "35.898", "14.51", "at 35.8989 north", "35.898909,14.514550",
		// THE TWO MUTATIONS THAT DEFEATED THE NARROW PATTERN, as literals: a comma
		// separator, and a degree with a single whole digit (everywhere between 0° and
		// 10°, which is most of the equatorial world).
		"35,898909", "5.898909", "1.35", "-0.19", "2.35", "0,1913",
	} {
		if !decimalDegreeShape.MatchString(probe) {
			t.Errorf("the coordinate-shape sweep does NOT match %q", probe)
		}
	}
	// 🔴 THE ACCEPT HALF IS BUILT FROM WHAT THE PAGE REALLY PRINTS. Measured by
	// extracting every decimal-shaped string from the rendered page in all three of its
	// shapes: there are none at all. These are the numeric forms it does print.
	for _, probe := range []string{
		"22 records", "2 of 20 verified by GPS alone", "\u00b7 10%", "5 reads missing",
		"closest 2 seconds apart", "3 days", "Mon 2 Feb 09:00", "6 times together",
		"1 training tap", "Plaque AA0000000000A1", "base:ctr-gap-review",
		"18 carry the rule snapshot", "Showing 50 of 120", "2026-02-02",
		"Europe/Malta", "/static/css/app.css", "9.5", "35.9",
	} {
		if m := decimalDegreeShape.FindString(probe); m != "" {
			t.Errorf("the coordinate-shape sweep matches %q inside %q, which this page "+
				"legitimately prints; a sweep that fires on honest output is one somebody "+
				"deletes rather than fixes", m, probe)
		}
	}
}

// TestPanelAnomaliesDB_CountsTheShapesTheCardASKSFOR is the acceptance criteria,
// measured on real rows.
//
// 🔴 EACH ASSERTION NAMES ITS SHAPE, because the numbers are only meaningful against the
// fixture that produced them. The fixture is deliberately built so that no two signals
// share a count: a query that answered another query's number would be visible.
func TestPanelAnomaliesDB_CountsTheShapesTheCardASKSFOR(t *testing.T) {
	p := newPanelHarness(t)
	week, zone := anomalyWeek(t)
	f := seedAnomalyTenant(t, p, p.tenantID, "Echo", week, zone)
	p.signIn(t)

	res, body := p.get(t, anomaliesHref+anomalyWeekParam(week))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET the anomalies page: %d, want 200", res.StatusCode)
	}

	for _, c := range []struct{ what, want string }{
		// Two records matched on distance with no address match (the day-2 pair).
		// Read out of the COLUMNS, so the base is the 20 judged records.
		{"the GPS-only count", "2 of 20 verified by GPS alone"},
		// 🔴 THE NEXT THREE ARE READ OUT OF THE SNAPSHOT, SO THE BASE IS 18 -- not 22,
		// and not 20. A record with no snapshot cannot be asked these questions, and
		// putting it in the base is what "counting it as clean" looks like written as
		// arithmetic. The first draft printed "1 of 22 after a counter gap" directly
		// above a sentence saying those records are NOT counted as clean.
		{"the counter-gap count", "2 of 18 after a counter gap"},
		{"the outside-the-venue count", "1 of 18 read outside the venue"},
		{"the other-venue count", "1 of 18 at another venue"},
		// max, not sum: the plaque carries gaps of 3 and 5.
		{"the largest gap on the plaque", "5 reads missing"},
		// 🔴 THE APOSTROPHE IS DELIBERATELY NOT IN THIS STRING. templ escapes it to
		// &#39;, so the first version of this assertion was red against a page that
		// said exactly the right thing -- the shape that gets an assertion loosened
		// until it stops seeing anything.
		{"the on-network subset", "1 of 1 also matched the venue"},
		// One training tap, two manager-typed rows.
		{"the training count", "1 training tap"},
		{"the manager-typed count", "2 typed by a manager"},
		// Four records carry no policy snapshot at all.
		{"the unanswerable count", "4 records carry no policy snapshot"},
		// AND THE PAGE NAMES BOTH BASES rather than leaving a reader to notice they
		// differ and assume one is a bug.
		{"the basis sentence", "Two bases are in use below"},
		{"the judged base", "20 records were judged on evidence"},
		{"the answerable base", "18 carry the rule snapshot"},
		// The pair listing: the worker and the mate arrived within seconds on three
		// days, which is over the threshold.
		{"the pair's day count", "3 days"},
		{"the pair's closest gap", "closest 2 seconds apart"},
		{"the pair's occurrence count", "6 times together"},
	} {
		if !strings.Contains(body, c.want) {
			t.Errorf("%s is not on the page; wanted the text %q", c.what, c.want)
		}
	}
	// 🔴 AND THE WRONG BASES ARE REFUSED BY NAME. Without this half, a figure that went
	// back to dividing by 22 or by 20 would only be caught if the strings above happened
	// to pin the surrounding text closely enough.
	for _, wrong := range []string{
		"of 22 after a counter gap", "of 20 after a counter gap",
		"of 22 read outside the venue", "of 20 read outside the venue",
		"of 22 at another venue", "of 20 at another venue",
	} {
		if strings.Contains(body, wrong) {
			t.Errorf("the page prints %q -- a figure read out of the policy snapshot is "+
				"dividing by a base that includes records carrying no snapshot, which is "+
				"counting those records as clean", wrong)
		}
	}
	// 🔴 max, NOT sum, AND IT NEEDS ITS OWN SENTENCE. The plaque carries gaps of 3 and
	// 5: the biggest single jump is 5 and their sum is 8. A mutation from max to sum
	// left the whole suite green before this fixture had a second gap on one plaque.
	if strings.Contains(body, "8 reads missing") {
		t.Error("the plaque's figure is 8, which is the SUM of its two gaps rather than " +
			"the largest one. Ten taps each one short is a busy plaque; one tap eight " +
			"short is a different event, and adding them up describes neither.")
	}
	// The pair is named by both people.
	if !strings.Contains(body, f.employee) || !strings.Contains(body, f.mate) {
		t.Error("the pair listing does not name both people")
	}
	// THE POLICY BREAKDOWN, which is the one block that is not a suspicion.
	if !strings.Contains(body, "base:ctr-gap-review") {
		t.Error("the rule breakdown does not name the rule that decided the counter-gap row")
	}
	if !strings.Contains(body, "No rule recorded") {
		t.Error("the rule breakdown drops the records with no policy decision, so its " +
			"column no longer adds up to the record count above it")
	}
	// 🔴 AND THE LAYER BESIDE EACH RULE IS THE ONE §5 GIVES IT. Every rule this fixture
	// writes is a `base:*` rule, so every layer on the page must read `baseline`; a
	// `guardrail` here would mean the fixture is producing rows the product cannot --
	// which it did until the policy version above existed.
	if !strings.Contains(body, "baseline") {
		t.Error("the rule breakdown names no layer at all, so the check below runs over " +
			"nothing")
	}
	if strings.Contains(body, "guardrail") {
		t.Error("the rule breakdown shows a base:* rule on the guardrail layer. §5 fixes " +
			"sys:* as the guardrail layer and base:* as the baseline, so that pairing is " +
			"a row no tap could ever record.")
	}
}

// TestPanelAnomaliesDB_ATrainingTapIsNotAnOpenCheckIn is §5 and the M6-11 card's most
// specific warning, measured on a real practice row.
//
// 🔴 A PRACTICE ROW IS A type='in' WITH NO 'out' AFTER IT (ADR 0008). In SQL that is
// exactly the shape of a forgotten checkout, so without the exclusion this list produces
// one false positive for EVERY new starter -- and "the report does not comment, it shows
// data" is broken by the report's own arithmetic.
func TestPanelAnomaliesDB_ATrainingTapIsNotAnOpenCheckIn(t *testing.T) {
	p := newPanelHarness(t)
	week, zone := anomalyWeek(t)
	f := seedAnomalyTenant(t, p, p.tenantID, "Foxtrot", week, zone)
	p.signIn(t)

	res, body := p.get(t, anomaliesHref+anomalyWeekParam(week))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET the anomalies page: %d, want 200", res.StatusCode)
	}

	// POSITIVE CONTROL: the ONE genuinely open check-in IS listed. The fixture gives
	// the mate an unclosed 09:00 entry on the last day; without this, "the training tap
	// is absent" would be satisfied by an empty list.
	if !strings.Contains(body, "Check-ins with no checkout") {
		t.Fatal("the open section is missing entirely")
	}
	if strings.Contains(body, "Every check-in this week has a checkout after it") {
		t.Fatal("the open list is empty, so the exclusion below is vacuous — the fixture " +
			"plants one real unclosed check-in")
	}

	// 🔴 THE TRAINING TAP AND THE REAL ENTRY ARE THE SAME PERSON ON THE SAME DAY, so
	// counting rows is the only way to tell whether the practice row leaked in. The
	// mate's name appears once per open docket.
	openDockets := strings.Count(body, `<p class="report-who">`+f.mate+`</p>`)
	if openDockets != 1 {
		t.Errorf("the open list holds %d docket(s) for %s, want exactly 1. A training tap "+
			"is a check-in nothing ever closes, so leaving it in produces one false "+
			"positive per new starter.", openDockets, f.mate)
	}

	// AND THE ADR 0008 RETROSPECTIVE TALLY FINDS THE REAL ENTRY, because a practice row
	// of that person's sits after it.
	if !strings.Contains(body, "Check-ins with a training tap after them") {
		t.Error("the retrospective tally is missing; the open list alone cannot find a " +
			"check-in a later checkout has already taken off it")
	}
	if !strings.Contains(body, "1 still on the list above") {
		t.Error("the retrospective tally does not report the one entry with a training tap " +
			"after it")
	}
}
