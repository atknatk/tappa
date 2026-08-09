package handler

// transactions_db_test.go -- M6-03's red-line proofs, against REAL Postgres, over
// REAL HTTP, with a real panel session.
//
// 🔴 WHY THESE CANNOT BE UNIT TESTS, and it is the difference between a net and a
// tautology. The two rules this task touches are:
//
//	§4.5  tenant A's panel must never show tenant B's record
//	§4.7  a raw coordinate must never reach a screen
//
// A fake ledger proves NEITHER. It hands the handler a domain value that has no
// coordinate field and no foreign row in it, so the assertion "no coordinate
// appears" passes because none could ever have appeared. The only way to measure
// either is to put a row in the database that HAS gps_lat, gps_lng and source_ip,
// and a second tenant that HAS records, and then read what the HTTP response
// actually contains.
//
// EVERY TEST BELOW OPENS WITH A POSITIVE CONTROL for that reason: the panel is
// first shown to render the tenant's OWN record, so that "B's record is absent"
// cannot be satisfied by a page that renders nothing at all. That is the shape
// internal/db/rls_test.go uses and says why.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// panelFixture is one tenant's worth of readable data: a venue, a department, a
// person, a plaque and some records.
type panelFixture struct {
	tenantID     uuid.UUID
	locationID   uuid.UUID
	departmentID uuid.UUID
	employeeID   uuid.UUID
	employee     string
	location     string
	tagUID       string
}

// theGPS is the coordinate every fixture row carries.
//
// 🔴 IT IS THE THING THE §4.7 TEST LOOKS FOR, so it must be present in the
// DATABASE and absent from the PAGE. The values are deliberately distinctive
// (Valletta, to six decimals, which is what numeric(9,6) stores) so that a
// substring search for them cannot collide with an id, a timestamp or a trust
// score that happens to contain the same digits.
const (
	theLat = "35.898909"
	theLng = "14.514550"
	theIP  = "203.0.113.77"
)

// seedPanelTenant creates a tenant with `records` transactions in it, all on the
// same day, each carrying a coordinate and a source address.
func seedPanelTenant(t *testing.T, p *panelHarness, tenantID uuid.UUID, label string, day time.Time, records int) panelFixture {
	t.Helper()
	f := panelFixture{
		tenantID:   tenantID,
		location:   label + " Venue",
		employee:   label + " Person",
		tagUID:     strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))[:14],
		locationID: uuid.New(), departmentID: uuid.New(), employeeID: uuid.New(),
	}

	err := p.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name, static_ips)
			 VALUES ($1, $2, $3, '{203.0.113.0/24}')`,
			f.locationID, tenantID, f.location); e != nil {
			return fmt.Errorf("location: %w", e)
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO departments (id, tenant_id, location_id, name)
			 VALUES ($1, $2, $3, $4)`,
			f.departmentID, tenantID, f.locationID, label+" Kitchen"); e != nil {
			return fmt.Errorf("department: %w", e)
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, department_id, full_name, status)
			 VALUES ($1, $2, $3, $4, $5, 'active')`,
			f.employeeID, tenantID, f.locationID, f.departmentID, f.employee); e != nil {
			return fmt.Errorf("employee: %w", e)
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO tags (uid, tenant_id, location_id, status, last_ctr, aes_key_ref)
			 VALUES ($1, $2, $3, 'active', 0, 'test-key-ref')`,
			f.tagUID, tenantID, f.locationID); e != nil {
			return fmt.Errorf("tag: %w", e)
		}
		for i := 0; i < records; i++ {
			// Alternate the verdict and the channel so the filter tests have
			// something to separate, and vary the minute so the ordering is total.
			verdict, channel, direction := "ok", "nfc", "in"
			if i%2 == 1 {
				verdict, channel, direction = "flag", "qr", "out"
			}
			if _, e := tx.Exec(ctx,
				`INSERT INTO transactions
				   (tenant_id, employee_id, location_id, department_id, tag_uid, ctr,
				    type, occurred_at, source_ip, ip_match, gps_lat, gps_lng, gps_match,
				    sun_valid, trust, verdict, channel, practice, queued)
				 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::inet,true,$10::numeric,$11::numeric,
				         true,true,100,$12,$13,false,false)`,
				tenantID, f.employeeID, f.locationID, f.departmentID, f.tagUID, i+1,
				direction, day.Add(time.Duration(i)*time.Minute), theIP, theLat, theLng,
				verdict, channel); e != nil {
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

// signIn drives the real sign-in flow and leaves the harness holding a session.
func (p *panelHarness) signIn(t *testing.T) {
	t.Helper()
	_, page := p.get(t, "/admin/login")
	csrf := csrfFrom(t, page)
	res, _ := p.post(t, "/admin/login", url.Values{
		"csrf": {csrf}, "email": {p.email}, "password": {p.password},
	})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("sign-in answered %d, want 303", res.StatusCode)
	}
}

// dayOf is midnight of a fixed day in UTC. The seeded tenant's zone is the
// migration default (Europe/Malta), so mid-morning UTC is unambiguously the same
// calendar day in both zones and the test is not measuring a boundary.
func dayOf() time.Time { return time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC) }

func dayParam(d time.Time) string { return d.Format("2006-01-02") }

// TestPanelTransactionsDB_ShowsOwnRecordsAndNeverAnotherTenants is §4.5 measured
// end to end.
//
// 🔴 THE POSITIVE CONTROL IS THE POINT. Tenant A's panel is first required to
// render A's own venue, person and plaque; only then is B's absence evidence of
// anything. Without it, a handler that returned an error page, or a query that
// matched nothing at all, would satisfy the isolation claim perfectly.
func TestPanelTransactionsDB_ShowsOwnRecordsAndNeverAnotherTenants(t *testing.T) {
	p := newPanelHarness(t)
	day := dayOf()

	a := seedPanelTenant(t, p, p.tenantID, "Alpha", day, 3)

	// Tenant B is a COMPLETE second business with its own records. It has no admin
	// and never signs in -- it exists to be invisible.
	bTenant := uuid.New()
	if err := p.data.WithTenant(context.Background(), bTenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Bravo Ltd', $2, 'bar', 'single')`,
			bTenant, "VAT-"+bTenant.String())
		return e
	}); err != nil {
		t.Fatalf("insert tenant B: %v", err)
	}
	b := seedPanelTenant(t, p, bTenant, "Bravo", day, 3)

	p.signIn(t)
	res, body := p.get(t, transactionsHref+"?date="+dayParam(day))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET the panel: %d, want 200", res.StatusCode)
	}

	// POSITIVE CONTROL -- A's own records really are on the page.
	for _, want := range []string{a.location, a.employee, a.tagUID} {
		if !strings.Contains(body, want) {
			t.Fatalf("the panel does not show tenant A's own %q. Every isolation "+
				"assertion below would be vacuous over a page with no records on it.", want)
		}
	}

	// THE ISOLATION CLAIM.
	for _, leak := range []struct{ what, text string }{
		{"another tenant's venue", b.location},
		{"another tenant's employee", b.employee},
		{"another tenant's plaque", b.tagUID},
		{"another tenant's employee id", b.employeeID.String()},
		{"another tenant's location id", b.locationID.String()},
	} {
		if strings.Contains(body, leak.text) {
			t.Errorf("§4.5 BREACH: the panel rendered %s (%q)", leak.what, leak.text)
		}
	}

	// AND THE FILTERS CANNOT BE USED TO REACH ACROSS EITHER. A foreign id is a
	// narrowing predicate inside A's tenant, so it can only ever match nothing --
	// but that is a claim about the SQL, and this measures it.
	//
	// 🔴 THE ID FILTERS ARE CHECKED OVER THE WHOLE DOCUMENT AND THE NAME FILTER OVER
	// THE DOCKETS ONLY, and the difference is not a loophole -- it is what the two
	// controls are. A location id is never echoed anywhere, so ANY appearance of a
	// foreign venue is data we rendered. The name box is a TEXT INPUT: it echoes
	// what the visitor typed, so searching the whole page for a string the visitor
	// chose finds their own keystrokes and calls it a breach.
	//
	// MEASURED, because that distinction is exactly the kind this file gets wrong:
	// signed in as A and filtering for B's employee by name, "Bravo Person" occurs
	// ONCE on the page -- in the input's value attribute -- with 0 docket cards
	// rendered, 0 occurrences inside cards, and B's tag uid and venue absent
	// entirely. The reflection is the visitor's own input; the isolation claim is
	// about the RECORDS.
	for _, param := range []string{
		"location=" + b.locationID.String(),
		"department=" + b.departmentID.String(),
	} {
		_, filtered := p.get(t, transactionsHref+"?date="+dayParam(day)+"&"+param)
		for _, text := range []string{b.location, b.employee, b.tagUID} {
			if strings.Contains(filtered, text) {
				t.Errorf("§4.5 BREACH: filtering by %s reached tenant B's data (%q)", param, text)
			}
		}
	}
	_, byName := p.get(t, transactionsHref+"?date="+dayParam(day)+"&employee="+url.QueryEscape(b.employee))
	for _, text := range []string{b.location, b.employee, b.tagUID} {
		if strings.Contains(docketsOn(byName), text) {
			t.Errorf("§4.5 BREACH: searching for another tenant's employee by name "+
				"rendered their data (%q)", text)
		}
	}
	if n := strings.Count(docketsOn(byName), `class="docket"`); n != 0 {
		t.Errorf("searching for a foreign employee returned %d docket(s); a name that "+
			"belongs to nobody in THIS tenant must match nothing", n)
	}
	// The reflection itself must be ESCAPED. It is the one place on this screen
	// where a visitor's string reaches the HTML, and templ escapes attribute values
	// -- this pins that rather than assuming it.
	_, reflected := p.get(t, transactionsHref+"?employee="+url.QueryEscape(`"><script>x</script>`))
	if strings.Contains(reflected, "<script>x</script>") {
		t.Error("the employee box reflected a raw script tag into the page")
	}

	// The fragment endpoint reads the same rows and must obey the same rule.
	_, frag := p.get(t, docketFragmentPath+"?date="+dayParam(day))
	for _, text := range []string{b.location, b.employee, b.tagUID} {
		if strings.Contains(frag, text) {
			t.Errorf("§4.5 BREACH: the paging fragment rendered tenant B's %q", text)
		}
	}
}

// TestPanelTransactionsDB_NeverRendersACoordinateOrASourceAddress is §4.7
// measured against rows that genuinely carry both.
//
// 🔴 THE ROW ON DISK HAS THE COORDINATE. That is asserted first, from the
// database, because the whole test is worthless if the fixture silently stored
// NULL -- "the page does not show a coordinate" would then be true of a product
// that renders every coordinate it has.
func TestPanelTransactionsDB_NeverRendersACoordinateOrASourceAddress(t *testing.T) {
	p := newPanelHarness(t)
	day := dayOf()
	f := seedPanelTenant(t, p, p.tenantID, "Charlie", day, 2)

	// THE FIXTURE REALLY DOES CARRY THE THING WE ARE LOOKING FOR.
	var storedLat, storedLng, storedIP *string
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT gps_lat::text, gps_lng::text, host(source_ip)
			   FROM transactions WHERE tenant_id = $1 AND tag_uid = $2 LIMIT 1`,
			p.tenantID, f.tagUID).Scan(&storedLat, &storedLng, &storedIP)
	}); err != nil {
		t.Fatalf("reading the stored row: %v", err)
	}
	switch {
	case storedLat == nil || storedLng == nil:
		t.Fatal("the fixture stored no coordinate, so this test would pass over a " +
			"product that renders every coordinate it holds")
	case storedIP == nil:
		t.Fatal("the fixture stored no source address; same problem")
	}

	p.signIn(t)
	pages := map[string]string{}
	_, pages["the section"] = p.get(t, transactionsHref+"?date="+dayParam(day))
	_, pages["the paging fragment"] = p.get(t, docketFragmentPath+"?date="+dayParam(day))

	for where, body := range pages {
		// Not vacuous: the record IS on this page.
		if !strings.Contains(body, f.employee) {
			t.Fatalf("%s does not render the record at all", where)
		}
		for _, secret := range []struct{ what, text string }{
			{"the latitude", *storedLat},
			{"the longitude", *storedLng},
			{"the latitude as seeded", theLat},
			{"the longitude as seeded", theLng},
			{"the source IP address", *storedIP},
			// Six decimal places truncated to three would still locate somebody
			// to within ~100 m, so the shortened forms are searched too.
			{"a truncated latitude", theLat[:6]},
			{"a truncated longitude", theLng[:6]},
		} {
			if strings.Contains(body, secret.text) {
				t.Errorf("§4.7 BREACH: %s rendered %s (%q). The panel shows the SIGNS "+
					"(IP yes/no, GPS yes/no), never the values.", where, secret.what, secret.text)
			}
		}
		// POSITIVE CONTROL for the rule itself: the signs ARE shown, so §4.7 is
		// being satisfied by substituting the sign rather than by rendering nothing.
		if !strings.Contains(body, "IP yes") || !strings.Contains(body, "GPS yes") {
			t.Errorf("%s shows neither evidence sign. Hiding the coordinate is only "+
				"correct if the sign replaces it.", where)
		}
	}
}

// TestPanelTransactionsDB_EveryFilterChangesWhatTheDatabaseReturns is the filter
// bar's real claim, measured in SQL rather than in a fake.
//
// EACH CASE IS A PAIR: something that must still be there, and something that must
// have gone. A filter that returned an empty page would satisfy "the excluded row
// is absent" and is caught by the first half.
func TestPanelTransactionsDB_EveryFilterChangesWhatTheDatabaseReturns(t *testing.T) {
	p := newPanelHarness(t)
	day := dayOf()

	// Two venues in ONE tenant, so location/department/employee actually separate
	// something. Records alternate ok/nfc/in and flag/qr/out.
	a := seedPanelTenant(t, p, p.tenantID, "Delta", day, 2)
	b := seedPanelTenant(t, p, p.tenantID, "Echo", day.Add(2*time.Hour), 2)

	p.signIn(t)
	base := transactionsHref + "?date=" + dayParam(day)

	// Unfiltered, both venues are present -- the baseline every case below is a
	// narrowing of.
	//
	// 🔴 EVERY ASSERTION IN THIS TEST READS THE DOCKETS, NOT THE WHOLE PAGE, and
	// that is a correction this test made to itself. Scanning the document was wrong
	// in a way that looked right: the filter bar lists EVERY venue, department and
	// person in the tenant as <option> labels, so "Echo Venue" is on the page even
	// when the list is filtered to Delta -- and it SHOULD be, or the dropdown could
	// not offer it. Reading the whole body made three correct filters look broken.
	//
	// (The cross-tenant test above deliberately does the opposite and scans the
	// entire document, because a FOREIGN tenant's venue must not appear in the
	// dropdown either. Two different questions, two different scopes.)
	_, all := p.get(t, base)
	for _, want := range []string{a.location, b.location} {
		if !strings.Contains(docketsOn(all), want) {
			t.Fatalf("the unfiltered day's dockets do not contain %q; the cases below would be vacuous", want)
		}
	}

	for _, tc := range []struct {
		name    string
		query   string
		keeps   string
		removes string
	}{
		{"location", "location=" + a.locationID.String(), a.location, b.location},
		{"department", "department=" + a.departmentID.String(), a.employee, b.employee},
		{"employee", "employee=" + url.QueryEscape("Echo Person"), b.employee, a.employee},
		{"verdict", "verdict=flag", "FLAGGED", "APPROVED"},
		{"channel", "channel=qr", "FLAGGED", "APPROVED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, body := p.get(t, base+"&"+tc.query)
			if res.StatusCode != http.StatusOK {
				t.Fatalf("status %d", res.StatusCode)
			}
			body = docketsOn(body)
			if !strings.Contains(body, tc.keeps) {
				t.Errorf("filtering by %s removed %q, which should have stayed -- an "+
					"empty page satisfies the exclusion check below for the wrong reason",
					tc.name, tc.keeps)
			}
			if strings.Contains(body, tc.removes) {
				t.Errorf("filtering by %s still shows %q, so the filter reached the "+
					"screen but not the query", tc.name, tc.removes)
			}
		})
	}

	// THE DAY FILTER IS THE SIXTH, and it is different in kind: it moves the window
	// rather than narrowing inside it.
	t.Run("date", func(t *testing.T) {
		_, other := p.get(t, transactionsHref+"?date="+dayParam(day.AddDate(0, 0, -1)))
		if strings.Contains(docketsOn(other), a.employee) {
			t.Errorf("a record from %s appears on the previous day's page", dayParam(day))
		}
		if !strings.Contains(other, "No taps recorded") {
			t.Error("a genuinely empty day does not say so -- and it must, having been queried")
		}
	})
}

// TestPanelTransactionsDB_PagingCoversEveryRecordExactlyOnce is the keyset
// cursor's claim, and it is the reason the query does not use OFFSET.
//
// 🔴 THE FAILURE IT RULES OUT IS SILENT. With OFFSET, a record inserted between
// two page loads shifts everything down and page 2 repeats a row page 1 already
// showed -- on an append-only table whose newest rows sort FIRST, that is not a
// rare race, it is what happens whenever somebody taps while a manager is reading.
func TestPanelTransactionsDB_PagingCoversEveryRecordExactlyOnce(t *testing.T) {
	p := newPanelHarness(t)
	day := dayOf()

	// More records than one page holds, so there is a second page at all.
	const total = 60 // PageSize is 25 -> three pages
	f := seedPanelTenant(t, p, p.tenantID, "Foxtrot", day, total)
	p.signIn(t)

	seen := map[string]int{}
	next := transactionsHref + "?date=" + dayParam(day)
	pageCount := 0
	for next != "" && pageCount < 10 {
		pageCount++
		res, body := p.get(t, next)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("page %d: status %d", pageCount, res.StatusCode)
		}
		for _, ctr := range countersOn(body) {
			seen[ctr]++
		}
		next = nextPageHref(body)
	}

	if pageCount < 2 {
		t.Fatalf("walked %d page(s) over %d records with a page size of %d -- there is "+
			"no pagination here to test", pageCount, total, 25)
	}
	if len(seen) != total {
		t.Errorf("paging showed %d distinct records over %d pages, want %d",
			len(seen), pageCount, total)
	}
	for ctr, n := range seen {
		if n != 1 {
			t.Errorf("record #%s appeared %d times across the pages, want once", ctr, n)
		}
	}

	// AND A TAP LANDING MID-READ MUST NOT REPEAT A ROW. This is the OFFSET failure,
	// staged: read page 1, insert a NEWER record, then follow page 1's own cursor.
	res, first := p.get(t, transactionsHref+"?date="+dayParam(day))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("re-reading page 1: %d", res.StatusCode)
	}
	firstSet := map[string]bool{}
	for _, ctr := range countersOn(first) {
		firstSet[ctr] = true
	}
	cursor := nextPageHref(first)
	if cursor == "" {
		t.Fatal("page 1 offered no continuation")
	}
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO transactions
			   (tenant_id, employee_id, location_id, tag_uid, ctr, type, occurred_at,
			    ip_match, gps_match, trust, verdict, channel)
			 VALUES ($1,$2,$3,$4,$5,'in',$6,true,true,100,'ok','nfc')`,
			p.tenantID, f.employeeID, f.locationID, f.tagUID, 9999,
			day.Add(24*time.Hour-time.Minute))
		return e
	}); err != nil {
		t.Fatalf("inserting the mid-read record: %v", err)
	}
	_, second := p.get(t, cursor)
	for _, ctr := range countersOn(second) {
		if firstSet[ctr] {
			t.Errorf("record #%s appeared on page 1 AND on page 2 after a newer record "+
				"landed between the two reads. That is the OFFSET failure the keyset "+
				"cursor exists to prevent.", ctr)
		}
	}
}

// docketsOn returns just the docket cards, with the chrome and the filter bar's
// option lists stripped away. See the note in the filter test for why the
// distinction matters.
func docketsOn(body string) string {
	return strings.Join(docketRE.FindAllString(body, -1), "\n")
}

// countersOn reads the chip counters printed on each docket. They are the one
// per-record value on the page that is short, numeric and unique per fixture row.
func countersOn(body string) []string {
	var out []string
	for _, m := range docketCounterRE.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// nextPageHref reads the paging control's ORDINARY href -- the no-JavaScript path,
// which is also the one a test can follow.
func nextPageHref(body string) string {
	m := nextPageRE.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return strings.ReplaceAll(m[1], "&amp;", "&")
}

var (
	// The docket prints "· #<ctr>" after the tag uid.
	docketCounterRE = regexp.MustCompile(`·\s*#(\d+)`)
	// The paging control carries BOTH an href (no-JavaScript) and hx-get.
	nextPageRE = regexp.MustCompile(`(?is)<a\b[^>]*\bhref="([^"]+)"[^>]*\bhx-get=`)
	// One docket card. There is no nested <article> in this markup.
	docketRE = regexp.MustCompile(`(?is)<article\b[^>]*class="docket"[^>]*>.*?</article>`)
)

// TestPanelTransactionsDB_NameFilterFindsPeopleWhoHaveLEFT is §4.6, and it is the
// reason the shortlisting options were rejected rather than a nice-to-have.
//
// 🔴 THE DECISION IT PINS. Three ways to stop the employee roster growing without
// bound were measured: keep only active staff (saved 9%), keep only recent tappers
// (0.3%), or match names on the server (96%). The first two were refused because
// they make somebody who has LEFT unfindable through the panel while their records
// are still in the table -- and those are precisely the records a manager needs
// months later, when a dispute or a payroll question arrives. A record nobody can
// ask for is most of the way to lost.
//
// So the match must ignore status entirely, and there must be no status predicate
// in the query. This drives it end to end against a deactivated employee.
func TestPanelTransactionsDB_NameFilterFindsPeopleWhoHaveLEFT(t *testing.T) {
	p := newPanelHarness(t)
	day := dayOf()
	f := seedPanelTenant(t, p, p.tenantID, "Golf", day, 2)

	// The person leaves AFTER their taps were recorded -- the ordinary case.
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`UPDATE employees SET status = 'deactivated', deactivated_at = now()
			 WHERE tenant_id = $1 AND id = $2`, p.tenantID, f.employeeID)
		return e
	}); err != nil {
		t.Fatalf("deactivating the employee: %v", err)
	}
	var status string
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT status FROM employees WHERE tenant_id=$1 AND id=$2`,
			p.tenantID, f.employeeID).Scan(&status)
	}); err != nil || status != "deactivated" {
		t.Fatalf("the fixture is not deactivated (status=%q, err=%v); this test would "+
			"prove nothing about departed staff", status, err)
	}

	p.signIn(t)
	base := transactionsHref + "?date=" + dayParam(day)

	// Unfiltered, their records are listed.
	_, all := p.get(t, base)
	if !strings.Contains(docketsOn(all), f.employee) {
		t.Fatal("a departed employee's records vanished from the unfiltered day -- §4.6")
	}
	// And they are FINDABLE by name, which is the half the shortlists would break.
	_, byName := p.get(t, base+"&employee="+url.QueryEscape("Golf"))
	if !strings.Contains(docketsOn(byName), f.employee) {
		t.Errorf("filtering by name does not find %q, who has left.\n"+
			"Every alternative to this control was rejected BECAUSE it made departed "+
			"staff unfindable; a match that filters them out reintroduces exactly "+
			"that defect.", f.employee)
	}
}

// TestPanelTransactionsDB_NameFilterNarrowsAndDoesNotWildcard.
//
// TWO FAILURES IN ONE TEST because they are each other's control: a match that is
// too loose returns everyone (the filter does nothing), and one that is too tight
// returns nobody (the filter looks like an empty day). The wildcard case is the
// specific way the loose failure happens -- % and _ mean something to ILIKE, and a
// manager typing "100%" must not silently select the whole payroll.
func TestPanelTransactionsDB_NameFilterNarrowsAndDoesNotWildcard(t *testing.T) {
	p := newPanelHarness(t)
	day := dayOf()
	a := seedPanelTenant(t, p, p.tenantID, "Hotel", day, 2)
	b := seedPanelTenant(t, p, p.tenantID, "India", day.Add(2*time.Hour), 2)
	p.signIn(t)
	base := transactionsHref + "?date=" + dayParam(day)

	_, all := p.get(t, base)
	for _, who := range []string{a.employee, b.employee} {
		if !strings.Contains(docketsOn(all), who) {
			t.Fatalf("the unfiltered day is missing %q; every case below would be vacuous", who)
		}
	}

	// It NARROWS: a fragment of one name keeps that person and drops the other.
	_, hotel := p.get(t, base+"&employee=Hotel")
	if !strings.Contains(docketsOn(hotel), a.employee) {
		t.Errorf("matching %q lost the person it names", "Hotel")
	}
	if strings.Contains(docketsOn(hotel), b.employee) {
		t.Errorf("matching %q still shows %q -- the filter reached the screen, not the query",
			"Hotel", b.employee)
	}

	// A wildcard is a LITERAL. Nobody is called "%", so the day comes back empty
	// rather than complete.
	for _, wild := range []string{"%", "_", "%%", "Hotel%Person"} {
		_, got := p.get(t, base+"&employee="+url.QueryEscape(wild))
		dockets := docketsOn(got)
		if strings.Contains(dockets, a.employee) || strings.Contains(dockets, b.employee) {
			t.Errorf("employee=%q matched real people -- ILIKE's wildcards are reaching "+
				"the query, so a filter that looks applied is selecting everyone", wild)
		}
	}

	// And a matching substring still works after the escaping, so the escape did not
	// break ordinary use.
	_, partial := p.get(t, base+"&employee="+url.QueryEscape("otel Per"))
	if !strings.Contains(docketsOn(partial), a.employee) {
		t.Error("an ordinary substring stopped matching -- the escaping is too aggressive")
	}
}

// TestPanelTransactionsDB_NameFilterSurvivesHostileInput is the N1 net, and the
// defect it pins produced a 500 from ORDINARY input.
//
// 🔴 TWO WAYS THE BOX ANSWERED 500, both measured by an audit. The filter truncated
// at 100 BYTES with s[:100], which cuts a multi-byte character in half -- 99 ASCII
// characters plus "é" is 101 bytes, and the slice left invalid UTF-8 that the
// driver refused. And a NUL byte reached PostgreSQL, which cannot store one in
// text. Maltese names carry c-dot, g-dot, h-bar and z-dot -- the brand ships a
// latin-ext font subset for exactly that reason -- so the first is the ORDINARY
// case for this product, not an adversarial one.
//
// ⚠️ NEITHER WAS A §4.6 BREACH and that is worth recording: both produced an honest
// error page rather than a fabricated empty day, and no name reached a log. It is
// still a manager unable to use a filter.
func TestPanelTransactionsDB_NameFilterSurvivesHostileInput(t *testing.T) {
	p := newPanelHarness(t)
	day := dayOf()
	f := seedPanelTenant(t, p, p.tenantID, "Juliet", day, 2)
	p.signIn(t)
	base := transactionsHref + "?date=" + dayParam(day)

	// Not vacuous: the record is there to be filtered.
	_, all := p.get(t, base)
	if !strings.Contains(docketsOn(all), f.employee) {
		t.Fatal("the unfiltered day is empty; every case below would prove nothing")
	}

	long := strings.Repeat("a", 99) + "é" // 101 bytes, 100 runes -- the exact case
	for _, in := range []string{
		long,
		strings.Repeat("é", 300),
		"abc\x00def",
		"\x00",
		strings.Repeat("Ż", 150),
		"Borg\x00",
		"ħaddiem",
		strings.Repeat("a", 5000),
		"%_\\",
		"'; DROP TABLE transactions; --",
		"<script>alert(1)</script>",
	} {
		res, body := p.get(t, base+"&employee="+url.QueryEscape(in))
		if res.StatusCode != http.StatusOK {
			t.Errorf("employee=%q answered %d, want 200. A filter box must not be able "+
				"to take the panel down, and a name with an accent in it is ORDINARY "+
				"input for a Maltese payroll.", truncateForMsg(in), res.StatusCode)
			continue
		}
		// It must also not have fabricated anything: the page is a real answer.
		if strings.Contains(body, "We could not read your records") {
			t.Errorf("employee=%q produced the database-unavailable page", truncateForMsg(in))
		}
	}

	// The table is still there after the injection attempts -- the positive control
	// for "bound parameter", measured rather than assumed.
	var n int
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM transactions WHERE tenant_id = $1`, p.tenantID).Scan(&n)
	}); err != nil || n == 0 {
		t.Fatalf("transactions is gone or unreadable after the hostile inputs (n=%d, err=%v)", n, err)
	}
}

func truncateForMsg(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}
