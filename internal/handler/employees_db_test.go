package handler

// employees_db_test.go -- M6-05 phase A's red-line proofs, against REAL Postgres,
// over REAL HTTP, with a real panel session.
//
// 🔴 WHY THESE CANNOT BE UNIT TESTS, for the reason transactions_db_test.go gives
// and one more that belongs to this section. The rules being measured are:
//
//	§4.5  tenant A's roster must never show tenant B's people
//	§4.6  somebody who has LEFT must still be listed
//	§4.7  no session token, no device label, no invitation must reach the page
//
// A fake reader proves none of them. It hands the handler a domain value that has
// no token field, no foreign row and whatever status the test chose, so "no secret
// appears" passes because none could ever have appeared and "the departed are
// listed" passes because the fake was told to list them. The only way to measure
// any of the three is to put the real rows in the database -- a second tenant with
// its own staff, a person whose status is deactivated, a session with a real
// token_hash and a real device label -- and then read what the HTTP response
// actually contains.
//
// EVERY TEST BELOW OPENS WITH A POSITIVE CONTROL for that reason: the roster is
// first shown to render somebody, so that "B's people are absent" and "no token is
// printed" cannot be satisfied by a page with nobody on it.
//
// 🔴 WHAT THESE NETS DO NOT COVER — ATTEMPTED, MEASURED, AND LEFT OPEN ON PURPOSE.
// Each line below is a route somebody was TRIED against these tests, not a route
// somebody imagined. A counted hole is safer than one that is believed to be shut.
//
//	1. A PREFIX OR A TRANSFORM OF A SECRET — NOT COVERED HERE, AND NOT RELIABLY
//	   COVERED ANYWHERE. The §4.7 assertions below are strings.Contains(body, the
//	   WHOLE secret), so eight characters of a token hash on the page are invisible
//	   to them.
//
//	   🔴 THIS ENTRY USED TO SAY "COVERED ELSEWHERE — the cover is the type wall",
//	   AND A SECURITY AUDIT MEASURED THAT FALSE. The type wall
//	   (personFields in internal/domain/ledger/roster_test.go) fires when a leak
//	   needs a NEW FIELD on Person: adding `Value string` turns it red, verified. A
//	   leak that REUSES AN EXISTING FIELD never reaches it, because that test
//	   enumerates Person's fields and Person is unchanged. The audit's one-liner —
//	   the whole change, no Go edited:
//
//	       -- in place of `d.name AS department_name`:
//	       (SELECT left(s2.token_hash, 8) FROM sessions s2
//	        WHERE s2.tenant_id = $1 AND s2.employee_id = e.id
//	          AND s2.revoked_at IS NULL LIMIT 1) AS department_name
//
//	   — reported `make test` 16/16 ok with the page carrying the prefix.
//
//	   ⚠️ AND MY REPRODUCTION DIVERGED, WHICH IS RECORDED RATHER THAN SMOOTHED OVER.
//	   Four spellings of that expression (scalar subquery, `||` concatenation,
//	   LATERAL, NULLIF/CASE/array) all typed as `string`, `bool` or `interface{}`
//	   under sqlc v1.28 and did not compile against the mapper. Routing the value
//	   into DepartmentName through a Go edit DID compile — Person untouched, so the
//	   type wall stayed silent, exactly as the audit says — but three tests in this
//	   file went red, because several of them assert what a department cell contains.
//
//	   SO THE HONEST STATEMENT IS THE NARROW ONE: the type wall covers leaks that
//	   need a new field. Leaks through an existing field are outside it, and whether
//	   anything else notices depends on whether that particular field's value happens
//	   to be asserted somewhere — which is coverage by luck, not by design. What
//	   stops this from being a live defect is that the shipped query does not select
//	   token_hash at all.
//	2. A SECRET IN A LOG LINE. These read the HTTP body and nothing else. What
//	   covers that is scripts/redline-check.sh's R7 scan and CLAUDE.md §7, neither
//	   of which is exercised from here.
//	3. ✅ HIDING SOMEBODY BY ORDERING — WAS a limit, is now CLOSED, and the closing
//	   is why the fixture has six people in it. The first version measured
//	   `ORDER BY (status = 'deactivated')` leaving the §4.6 test GREEN over three
//	   people. Step 5 of that test now asserts alphabetical order, and the second
//	   cohort makes every status-first ordering displace a row: all three of
//	   deactivated / invited / active turn it red (the middle one survived the
//	   three-person fixture, which is what forced the sixth row).
//	   ⚠️ STILL OPEN, AND THE SHAPE IS KNOWN — an audit built it in one edit, so the
//	   sentence that used to say "no such shape has been constructed" lasted exactly
//	   one round. THE NET'S TEETH DEPEND ON FIXTURE SCALE:
//
//	        ORDER BY (e.status = 'deactivated'
//	                  AND (SELECT count(*) FROM employees e3
//	                       WHERE e3.tenant_id = @tenant_id) > 100) ASC,
//	                 e.full_name ASC, e.id ASC
//
//	   Every fixture in this suite is under a hundred people, so the guard is false
//	   throughout and alphabetical order holds — `internal/handler` ok,
//	   `internal/domain/ledger` ok. On any real roster the guard is true and everyone
//	   who has left is buried at the end. Closing it needs a fixture past the
//	   threshold an attacker chooses, which is not a threshold this suite can know.
//	   The rows remain reachable (the status filter finds them in one request), so
//	   this is a §4.6 SHARPNESS gap rather than a breach — counted, not shut.
//	4. THE WRITE PATH. Phase B is not here to be tested.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/ledger"
)

// rosterFixture is one tenant's worth of people.
type rosterFixture struct {
	tenantID     uuid.UUID
	locationID   uuid.UUID
	departmentID uuid.UUID
	location     string
	// The three lifecycle states, one person each, so a status assertion never has
	// to guess which row it is talking about.
	activeID, invitedID, departedID uuid.UUID
	active, invited, departed       string
	// tokenHash and deviceLabel belong to the ACTIVE person's live session. They are
	// the §4.7 needles: present in the database, forbidden on the page.
	tokenHash   string
	deviceLabel string
}

// seedRosterTenant creates a venue, a department and three people in the three
// lifecycle states, plus one live session and one revoked one.
func seedRosterTenant(t *testing.T, p *panelHarness, tenantID uuid.UUID, label string) rosterFixture {
	t.Helper()
	f := rosterFixture{
		tenantID:     tenantID,
		locationID:   uuid.New(),
		departmentID: uuid.New(),
		location:     label + " Venue",
		activeID:     uuid.New(),
		invitedID:    uuid.New(),
		departedID:   uuid.New(),
		active:       label + " Active Person",
		invited:      label + " Invited Person",
		departed:     label + " Departed Person",
		// The hash is what sessions.token_hash really holds: an opaque hex string.
		// It is deliberately long and distinctive so a substring search for it
		// cannot collide with an id or a timestamp on the page.
		tokenHash:   "SECRETTOKENHASH" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		deviceLabel: label + " iPhone Safari",
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
		people := []struct {
			id     uuid.UUID
			name   string
			status string
			stamp  string
		}{
			{f.activeID, f.active, "active", "activated_at"},
			{f.invitedID, f.invited, "invited", "invited_at"},
			{f.departedID, f.departed, "deactivated", "deactivated_at"},
		}
		for _, who := range people {
			if _, e := tx.Exec(ctx, fmt.Sprintf(
				`INSERT INTO employees (id, tenant_id, location_id, department_id, full_name, status, %s)
				 VALUES ($1, $2, $3, $4, $5, $6, now())`, who.stamp),
				who.id, tenantID, f.locationID, f.departmentID, who.name, who.status); e != nil {
				return fmt.Errorf("employee %s: %w", who.name, e)
			}
		}
		// ONE LIVE SESSION on the active person, with a device label and a use.
		if _, e := tx.Exec(ctx,
			`INSERT INTO sessions (tenant_id, employee_id, token_hash, device_info, last_used_at)
			 VALUES ($1, $2, $3, $4, now())`,
			tenantID, f.activeID, f.tokenHash, f.deviceLabel); e != nil {
			return fmt.Errorf("live session: %w", e)
		}
		// AND ONE REVOKED SESSION on the departed person, which must NOT count as an
		// active device. Without it, "None signed in" beside a departed person would
		// be true for the trivial reason that they never had a session at all.
		if _, e := tx.Exec(ctx,
			`INSERT INTO sessions (tenant_id, employee_id, token_hash, device_info, last_used_at, revoked_at)
			 VALUES ($1, $2, $3, $4, now(), now())`,
			tenantID, f.departedID, f.tokenHash+"REVOKED", f.deviceLabel+" (old)"); e != nil {
			return fmt.Errorf("revoked session: %w", e)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seeding roster for %s: %v", label, err)
	}
	return f
}

// rosterRowRE isolates the roster's cards, so an assertion about what is LISTED is
// not satisfied by the filter bar echoing what the visitor typed.
var rosterRowRE = regexp.MustCompile(`(?s)<article class="docket">.*?</article>`)

func rosterRowsOn(body string) string {
	return strings.Join(rosterRowRE.FindAllString(body, -1), "\n")
}

func rosterRowCount(body string) int { return len(rosterRowRE.FindAllString(body, -1)) }

// rosterCardFor returns the ONE card that names this person.
//
// 🔴 IT EXISTS BECAUSE A PAGE-WIDE SUBSTRING SEARCH IS NOT AN ASSERTION ABOUT A
// PERSON, and a mutation proved it. An earlier version of this file checked that the
// string "None signed in" appeared SOMEWHERE among the cards; the mutation that
// deleted `revoked_at IS NULL` from the query -- so a revoked device counts as an
// active one, which is the whole point of the predicate -- left it GREEN, because
// the invited person has no session at all and went on printing the sentence. The
// assertion has to name whose card it is reading.
func rosterCardFor(t *testing.T, body, name string) string {
	t.Helper()
	var found []string
	for _, card := range rosterRowRE.FindAllString(body, -1) {
		if strings.Contains(card, htmlText(name)) {
			found = append(found, card)
		}
	}
	switch len(found) {
	case 0:
		t.Fatalf("no roster card names %q; the assertion about that person cannot be "+
			"made over a page they are not on.\ncards:\n%s", name, rosterRowsOn(body))
	case 1:
		return found[0]
	default:
		t.Fatalf("%d roster cards name %q; the fixture's names must be unique or this "+
			"reads the wrong one", len(found), name)
	}
	return ""
}

// TestPanelEmployeesDB_ShowsOwnPeopleAndNeverAnotherTenants is §4.5 measured end to
// end, with the positive control first.
func TestPanelEmployeesDB_ShowsOwnPeopleAndNeverAnotherTenants(t *testing.T) {
	p := newPanelHarness(t)
	a := seedRosterTenant(t, p, p.tenantID, "Alpha")

	// Tenant B is a COMPLETE second business with its own staff. It has no admin and
	// never signs in -- it exists to be invisible.
	bTenant := uuid.New()
	if err := p.data.WithTenant(context.Background(), bTenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Bravo Ltd', $2, 'bar', 'single')`,
			bTenant, "VAT-"+bTenant.String()[:8])
		return e
	}); err != nil {
		t.Fatalf("insert tenant B: %v", err)
	}
	b := seedRosterTenant(t, p, bTenant, "Bravo")

	p.signIn(t)
	res, body := p.get(t, employeesHref)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET the roster: %d, want 200", res.StatusCode)
	}

	// POSITIVE CONTROL -- A's own people really are on the page.
	for _, want := range []string{a.active, a.invited, a.departed, a.location} {
		if !strings.Contains(body, want) {
			t.Fatalf("the roster does not show tenant A's own %q. Every isolation "+
				"assertion below would be vacuous over a page with nobody on it.", want)
		}
	}

	// THE ISOLATION CLAIM.
	for _, leak := range []struct{ what, text string }{
		{"another tenant's active employee", b.active},
		{"another tenant's invited employee", b.invited},
		{"another tenant's departed employee", b.departed},
		{"another tenant's venue", b.location},
		{"another tenant's employee id", b.activeID.String()},
		{"another tenant's location id", b.locationID.String()},
		{"another tenant's department id", b.departmentID.String()},
	} {
		if strings.Contains(body, leak.text) {
			t.Errorf("§4.5 BREACH: the roster rendered %s (%q)", leak.what, leak.text)
		}
	}

	// AND THE FILTERS CANNOT BE USED TO REACH ACROSS EITHER. A foreign id is a
	// narrowing predicate inside A's tenant, so it can only ever match nothing --
	// but that is a claim about the SQL, and this measures it.
	for _, param := range []string{
		"location=" + b.locationID.String(),
		"department=" + b.departmentID.String(),
	} {
		_, filtered := p.get(t, employeesHref+"?"+param)
		for _, text := range []string{b.active, b.invited, b.departed, b.location} {
			if strings.Contains(filtered, text) {
				t.Errorf("§4.5 BREACH: filtering by %s reached tenant B's data (%q)", param, text)
			}
		}
		if n := rosterRowCount(filtered); n != 0 {
			t.Errorf("filtering by a FOREIGN %s listed %d person(s); an id that belongs "+
				"to another business must match nobody", param, n)
		}
	}

	// THE NAME BOX IS CHECKED OVER THE CARDS ONLY, and the difference is not a
	// loophole -- it is what the control is. An id is never echoed anywhere, so any
	// appearance of a foreign venue is data we rendered; a text input echoes what
	// the visitor typed, so searching the whole document for a string the visitor
	// chose finds their own keystrokes and calls it a breach.
	_, byName := p.get(t, employeesHref+"?name="+url.QueryEscape(b.active))
	if strings.Contains(rosterRowsOn(byName), b.active) {
		t.Errorf("§4.5 BREACH: searching for another tenant's employee by name listed them")
	}
	if n := rosterRowCount(byName); n != 0 {
		t.Errorf("searching for a foreign employee listed %d person(s); a name that "+
			"belongs to nobody in THIS tenant must match nobody", n)
	}
}

// TestPanelEmployeesDB_ListsPeopleWhoHaveLEFT is §4.6, and it is the debt M6-03
// handed to this section.
//
// 🔴 THE DEFAULT VIEW IS THE ASSERTION. M6-03 measured three ways of shortlisting a
// roster and rejected all three because each made somebody who had left unfindable,
// and it recorded that "who works here?" had become this section's question. A
// status predicate quietly added to the default -- `AND status <> 'deactivated'`,
// or a filter defaulting to "active" -- would reopen exactly that hole, and it is
// the single most plausible edit somebody makes to this screen ("why is a person
// who left cluttering my list?").
//
// It is the twin of TestPanelTransactionsDB_NameFilterFindsPeopleWhoHaveLEFT, which
// holds the same rule on the other screen.
func TestPanelEmployeesDB_ListsPeopleWhoHaveLEFT(t *testing.T) {
	p := newPanelHarness(t)
	f := seedRosterTenant(t, p, p.tenantID, "Gamma")

	// A SECOND PERSON IN EACH LIFECYCLE STATE, NAMED TO INTERLEAVE, and the reason is
	// step 5 below rather than variety.
	//
	// 🔴 THE THREE-PERSON FIXTURE COULD NOT DETECT EVERY STATUS ORDERING, MEASURED.
	// `ORDER BY (status = 'deactivated')` was caught, and `ORDER BY
	// (status = 'invited')` was NOT — because "Gamma Invited Person" already sorted
	// last, so moving the invited to the end moved nothing. With one person per
	// status only two of the three orderings can ever be visible: a status-first sort
	// displaces a row only if that status's member is not already alphabetically
	// last, and only one person can be.
	//
	// Six people, two per status, alphabetically interleaved, makes all three
	// visible: whichever status is sorted out, its EARLIER member lands behind
	// somebody who should follow it. Verified against all three (see the mutation log
	// in the M6-05 card).
	err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		for _, who := range []struct{ name, status string }{
			{"Gamma Aa Second Active", "active"},
			{"Gamma Ab Second Departed", "deactivated"},
			{"Gamma Ac Second Invited", "invited"},
		} {
			if _, e := tx.Exec(ctx,
				`INSERT INTO employees (id, tenant_id, location_id, full_name, status)
				 VALUES ($1, $2, $3, $4, $5)`,
				uuid.New(), p.tenantID, f.locationID, who.name, who.status); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seeding the interleaved second cohort: %v", err)
	}

	p.signIn(t)

	// 1. THE UNFILTERED ROSTER LISTS THEM.
	_, body := p.get(t, employeesHref)
	rows := rosterRowsOn(body)
	if !strings.Contains(rows, f.active) {
		t.Fatalf("the default roster does not list the ACTIVE person; every assertion " +
			"below would be vacuous")
	}
	if !strings.Contains(rows, f.departed) {
		t.Errorf("§4.6 BREACH: the default roster does not list %q, who is deactivated.\n"+
			"Somebody who has left is exactly the person a manager comes looking for "+
			"months later, and this section is the ONLY screen that answers 'who works "+
			"here?' since M6-03 turned its employee picker into a text box. A default "+
			"that hides them makes their records unreachable by browsing.", f.departed)
	}
	if !strings.Contains(rows, f.invited) {
		t.Errorf("the default roster does not list %q, who is invited but not yet "+
			"active. All three lifecycle states are people who work here.", f.invited)
	}

	// 2. AND EACH PERSON'S OWN CARD SAYS WHICH STATE THEY ARE IN, so "listed" is not
	// the same as "listed indistinguishably from everybody else".
	//
	// IT IS ASSERTED PER CARD RATHER THAN PER PAGE for the reason rosterCardFor
	// records: "the word appears somewhere" is satisfied by the word appearing on
	// somebody ELSE's row, which is how one of this file's mutations survived.
	// DEACTIVATED is checked with a word boundary because ACTIVE is a substring of
	// it -- without that, a page printing only DEACTIVATED would satisfy both.
	for _, who := range []struct{ name, chip string }{
		{f.invited, "INVITED"},
		{f.active, "ACTIVE"},
		{f.departed, "DEACTIVATED"},
	} {
		card := rosterCardFor(t, body, who.name)
		if !strings.Contains(card, ">"+who.chip+"<") {
			t.Errorf("%q's card does not carry the %s chip; the chip is what makes a "+
				"departed person recognisable rather than merely present.\ncard:\n%s",
				who.name, who.chip, card)
		}
	}

	// 3. THE FILTER NARROWS TO THEM -- which is the difference between "not hidden"
	// and "findable". A manager who wants only the people who have left can ask.
	_, only := p.get(t, employeesHref+"?status=deactivated")
	onlyRows := rosterRowsOn(only)
	if !strings.Contains(onlyRows, f.departed) {
		t.Errorf("filtering on status=deactivated does not list %q", f.departed)
	}
	for _, unwanted := range []string{f.active, f.invited} {
		if strings.Contains(onlyRows, unwanted) {
			t.Errorf("filtering on status=deactivated listed %q, who is not", unwanted)
		}
	}

	// 4. AN UNKNOWN STATUS IS DROPPED, NOT OBEYED. Dropping can only WIDEN inside
	// the tenant; obeying a value the vocabulary does not contain would produce an
	// empty page that looks like an answer.
	_, bogus := p.get(t, employeesHref+"?status=fired")
	if !strings.Contains(rosterRowsOn(bogus), f.departed) {
		t.Errorf("a status the vocabulary does not contain narrowed the list. An " +
			"unrecognised filter must be dropped, so a stale bookmark shows MORE " +
			"rather than an empty page that reads as an answer.")
	}

	// 5. 🔴 THE ORDER DOES NOT DEPEND ON STATUS EITHER, which is the half the four
	// checks above cannot see.
	//
	// An audit route was measured and it walked straight through them: sorting the
	// deactivated to the END of the list hides nobody on a three-person fixture, so
	// `ORDER BY (status = 'deactivated') ASC, full_name ASC` left every assertion
	// above GREEN. On a real roster it pushes everybody who has left past the page
	// boundary — not unreachable (the status filter still finds them, and the last
	// page still exists), but "listed" quietly becomes "listed where nobody looks".
	//
	// GROWING THE FIXTURE PAST A PAGE WAS THE OBVIOUS FIX AND IS THE WRONG ONE: it
	// would cost 50-odd inserts per run and would still only measure the arrangement
	// of that particular fixture. What is asserted instead is the PROPERTY —
	// alphabetical order — and what gives it teeth is the INTERLEAVING seeded at the
	// top of this test: two people per status, so a status-first sort always displaces
	// somebody. Six rows, one property, no scale needed.
	//
	// ⚠️ THE FIRST VERSION OF THIS BLOCK CLAIMED "any status-first ordering moves a
	// row" OVER A THREE-PERSON FIXTURE AND THAT WAS FALSE — `(status = 'invited')`
	// went straight through it, because the invited person already sorted last. The
	// sentence was written before the third mutation was tried; the fixture is what
	// changed, not the claim.
	names := rosterNamesOn(body)
	const seededPeople = 6
	if len(names) != seededPeople {
		t.Fatalf("expected the %d seeded people, read %d name(s): %q",
			seededPeople, len(names), names)
	}
	assertRosterIsInDatabaseOrder(t, p, p.tenantID, names)
}

// assertRosterIsInDatabaseOrder compares the rendered sequence against the order
// POSTGRES produces for the same ORDER BY.
//
// 🔴 IT ASKS THE DATABASE INSTEAD OF COMPARING IN GO, AND THAT IS A PORTABILITY BUG
// CAUGHT BEFORE IT SHIPPED RATHER THAN A STYLE CHOICE. "Alphabetical" is not a
// property of a string, it is a property of a COLLATION. Measured in this
// environment: pg_database.datcollate reads en_US.utf8, but the image is
// postgres:17-alpine, whose musl libc carries no locale data, so the server actually
// sorts by BYTE — `SELECT 'a' < 'B'` is false and `'Zebra' < 'apple'` is true. Go's
// `<` on strings is also byte order, so a Go-side comparison agrees here BY
// COINCIDENCE.
//
// ⚠️ ON MANAGED POSTGRES (glibc or ICU) THAT COINCIDENCE ENDS, and a Go-side check
// would raise a FALSE ALARM on a product that is behaving correctly: the query's
// ORDER BY and the keyset's row comparison use the SAME collation as each other, so
// the shipped paging stays correct either way — only the test would be wrong. This
// is aimed squarely at M8 (deploy on managed Postgres), where a suite that goes red
// for a reason nobody can reproduce locally is expensive.
func assertRosterIsInDatabaseOrder(t *testing.T, p *panelHarness, tenantID uuid.UUID, got []string) {
	t.Helper()
	var want []string
	if err := p.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, e := tx.Query(ctx,
			`SELECT full_name FROM employees WHERE tenant_id = $1 ORDER BY full_name ASC, id ASC`,
			tenantID)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var n string
			if e := rows.Scan(&n); e != nil {
				return e
			}
			want = append(want, n)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("reading the expected order from the database: %v", err)
	}
	if len(want) != len(got) {
		t.Fatalf("the database holds %d people and the page rendered %d; the order "+
			"comparison below would be meaningless", len(want), len(got))
	}
	for i := range want {
		if want[i] != got[i] {
			t.Errorf("the roster is not in the order the database produces for "+
				"ORDER BY full_name, id.\nposition %d: page has %q, database has %q\n"+
				"page:     %q\ndatabase: %q\n"+
				"Ordering on status rather than on name pushes everybody who has left to "+
				"the end of the list -- which on a real roster means off the page a "+
				"manager is looking at. §4.6 asks for reachable; the default view "+
				"promises the stronger thing, which is the same place anybody else "+
				"would be.", i, got[i], want[i], got, want)
			return
		}
	}
}

// TestPanelEmployeesDB_ShowsNoSessionSecret is §4.7, measured against a session
// that really exists.
//
// 🔴 THE NEEDLES ARE IN THE DATABASE. A test that asserted "no token appears" over a
// tenant with no sessions would pass over an empty page. Here the active person has
// a live session with a real token_hash and a real device label, and the page is
// required to say that a device EXISTS while printing neither.
func TestPanelEmployeesDB_ShowsNoSessionSecret(t *testing.T) {
	p := newPanelHarness(t)
	f := seedRosterTenant(t, p, p.tenantID, "Delta")
	p.signIn(t)

	_, body := p.get(t, employeesHref)

	// POSITIVE CONTROL -- the session is genuinely visible AS A FACT, on the card of
	// the person who owns it.
	if card := rosterCardFor(t, body, f.active); !strings.Contains(card, "1 signed in") {
		t.Fatalf("the active person's card does not report their live device, so the "+
			"assertions below would be vacuous.\ncard:\n%s", card)
	}
	// AND THE REVOKED ONE DOES NOT COUNT, asserted on the card of the person whose
	// session is revoked.
	//
	// 🔴 THIS USED TO SEARCH THE WHOLE PAGE AND A MUTATION WALKED THROUGH IT.
	// Deleting `revoked_at IS NULL` from the query left the old form GREEN: the
	// INVITED person has no session at all, so the sentence stayed on the page while
	// the departed person's revoked device was being counted as live.
	if card := rosterCardFor(t, body, f.departed); !strings.Contains(card, "None signed in") {
		t.Errorf("the departed person's card does not say %q. Their only session is "+
			"REVOKED, so the roster must not count it as an active device.\ncard:\n%s",
			"None signed in", card)
	}

	// THE §4.7 CLAIM, over the WHOLE DOCUMENT rather than the cards: a secret in the
	// chrome, an attribute or a comment is still a secret on the page.
	for _, leak := range []struct{ what, text string }{
		{"a session token hash", f.tokenHash},
		{"a revoked session's token hash", f.tokenHash + "REVOKED"},
		{"a device label", f.deviceLabel},
		{"a revoked device's label", f.deviceLabel + " (old)"},
	} {
		if strings.Contains(body, leak.text) {
			t.Errorf("§4.7 BREACH: the roster rendered %s (%q)", leak.what, leak.text)
		}
	}

	// AND NO SESSION ID EITHER. It is not a credential, but it names one, and the
	// query does not select it -- so its appearance would mean somebody widened the
	// column list without widening this test.
	var sessionID uuid.UUID
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT id FROM sessions WHERE tenant_id = $1 AND employee_id = $2`,
			p.tenantID, f.activeID).Scan(&sessionID)
	}); err != nil {
		t.Fatalf("reading the seeded session id: %v", err)
	}
	if strings.Contains(body, sessionID.String()) {
		t.Errorf("§4.7 BREACH: the roster rendered a session id (%s)", sessionID)
	}
}

// TestPanelEmployeesDB_PagesThroughEverybodyExactlyOnce is the paging property, and
// it is a §4.6 assertion rather than a convenience one: a roster whose pages skip
// somebody makes that person unreachable by browsing, which is the same failure as
// hiding them.
func TestPanelEmployeesDB_PagesThroughEverybodyExactlyOnce(t *testing.T) {
	p := newPanelHarness(t)
	f := seedRosterTenant(t, p, p.tenantID, "Epsilon")

	// ENOUGH PEOPLE FOR SEVERAL PAGES, and NAMESAKES among them -- which is what the
	// id half of the cursor exists for. A roster of distinct names would page
	// correctly even with a name-only cursor, so the duplicates are the test.
	const extra = 60
	const sameName = "Epsilon Twin"
	err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		for i := 0; i < extra; i++ {
			name := sameName
			if i%3 == 0 {
				name = fmt.Sprintf("Epsilon Person %03d", i)
			}
			if _, e := tx.Exec(ctx,
				`INSERT INTO employees (id, tenant_id, location_id, full_name, status)
				 VALUES ($1, $2, $3, $4, 'active')`,
				uuid.New(), p.tenantID, f.locationID, name); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seeding the extra people: %v", err)
	}

	// 🔴 THE TRUTH IS A MULTISET OF NAMES, NOT A COUNT, and the difference is what
	// this test's name claims. An earlier version compared totals only: a run that
	// showed one person TWICE and skipped another would have produced the same
	// number and passed while doing exactly the thing "exactly once" forbids. Names
	// are not unique here on purpose (the namesakes are what the cursor's id half is
	// for), so the comparison is between two multisets rather than two sets.
	want := map[string]int{}
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `SELECT full_name FROM employees WHERE tenant_id = $1`, p.tenantID)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var n string
			if e := rows.Scan(&n); e != nil {
				return e
			}
			want[n]++
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("reading the roster from the database: %v", err)
	}
	total := 0
	for _, n := range want {
		total += n
	}

	p.signIn(t)
	got := map[string]int{}
	seen, pages := 0, 0
	href := employeesHref
	for href != "" {
		res, body := p.get(t, href)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %d, want 200", href, res.StatusCode)
		}
		for _, name := range rosterNamesOn(body) {
			got[name]++
			seen++
		}
		pages++
		if pages > 20 {
			t.Fatalf("the roster did not terminate after 20 pages; a cursor that does "+
				"not advance is an infinite list (seen %d of %d)", seen, total)
		}
		href = nextRosterHref(body)
	}

	if seen != total {
		t.Errorf("paging through the roster listed %d people over %d page(s); the tenant "+
			"has %d.\nA page that skips somebody makes them unreachable by browsing, "+
			"which is the §4.6 failure this list exists to prevent.", seen, pages, total)
	}
	for name, n := range want {
		if got[name] != n {
			t.Errorf("%q appears %d time(s) in the database and %d time(s) across the "+
				"pages. Repeating one person while dropping another keeps the TOTAL "+
				"right and is exactly what a cursor with no tiebreak does to namesakes.",
				name, n, got[name])
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("%q was listed and is not in this tenant's roster at all", name)
		}
	}
	if pages < 2 {
		t.Fatalf("the whole roster fitted on %d page(s); this test measures paging and "+
			"would be vacuous without at least two", pages)
	}
	// 🔴 AND THE PAGE COUNT IS EXACTLY THE ARITHMETIC THE BUDGET IS DERIVED FROM.
	// adminratelimit.go charges a roster-walk at ceil(E / RosterPageSize) requests and
	// the user's 2026-08-07 page-size decision was taken on that formula; this is the
	// one place it is measured rather than computed on paper. A page that quietly
	// returned fewer rows than it claimed would make every figure in that derivation
	// optimistic while every other test here stayed green.
	wantPages := (total + ledger.RosterPageSize - 1) / ledger.RosterPageSize
	if pages != wantPages {
		t.Errorf("walking %d people took %d page(s); ceil(%d / RosterPageSize=%d) is %d.\n"+
			"The request budget in adminratelimit.go is derived from that formula, so a "+
			"mismatch means the derivation is describing a product that does not exist.",
			total, pages, total, ledger.RosterPageSize, wantPages)
	}
	// AND PAGE ONE IS THE DATABASE'S OWN ORDER, which is the property that makes
	// browsing a way to FIND somebody rather than merely a way to see everybody. It
	// compares against a SELECT rather than against Go's `<` for the collation reason
	// assertRosterIsInDatabaseOrder records; here it is checked against the first
	// page only, since the walk above already proved the pages partition the roster.
	_, first := p.get(t, employeesHref)
	names := rosterNamesOn(first)
	var wantFirst []string
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, e := tx.Query(ctx,
			`SELECT full_name FROM employees WHERE tenant_id = $1
			 ORDER BY full_name ASC, id ASC LIMIT $2`,
			p.tenantID, ledger.RosterPageSize)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var n string
			if e := rows.Scan(&n); e != nil {
				return e
			}
			wantFirst = append(wantFirst, n)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("reading page one's expected order: %v", err)
	}
	for i := range wantFirst {
		if i >= len(names) || names[i] != wantFirst[i] {
			t.Errorf("page one is not in the database's own order at position %d.\n"+
				"page:     %q\ndatabase: %q", i, names, wantFirst)
			break
		}
	}
}

// TestPanelEmployeesDB_ACursorPastTheEndDoesNotClaimTheBusinessIsEmpty.
//
// 🔴 IT WAS A REAL SENTENCE ON A REAL PAGE. With three people on the books,
// GET /admin/employees?after_name=zzz…&after_id=<uuid> rendered "Nobody on the books
// yet" and "No people have been added to this business yet" over zero cards. Nothing
// exotic reaches it -- a stale bookmark, a shared link, a browser restoring a tab --
// and it is the exact class this whole task keeps closing: the product stating
// something it has not measured. It had measured a POSITION and reported a BUSINESS.
//
// The empty state deliberately does not fold the cursor into Narrowed() (a position
// is not a filter, and TestRosterFilter_ZeroValueNarrowsNothing pins that), so the
// fix is a third branch rather than a widened second one.
func TestPanelEmployeesDB_ACursorPastTheEndDoesNotClaimTheBusinessIsEmpty(t *testing.T) {
	p := newPanelHarness(t)
	f := seedRosterTenant(t, p, p.tenantID, "Kappa")
	p.signIn(t)

	// POSITIVE CONTROL -- there really are people on the books.
	_, home := p.get(t, employeesHref)
	if n := rosterRowCount(home); n < 3 {
		t.Fatalf("the tenant renders %d card(s); this test needs a NON-empty business "+
			"or the claim it forbids would be true", n)
	}

	// 🔴 THE CURSOR MUST RESOLVE, WHICH IS WHY IT IS THE LAST PERSON'S OWN ID. Since
	// the anchor became an id, a made-up uuid is not "past the end" — it is "no
	// cursor", and the page answers with the first page, which is the right answer to
	// a link the product never emitted. Anchoring on the LAST row is what actually
	// lands past the end, and it is reachable in practice: a manager bookmarks page
	// two, then applies a filter that excludes everybody after that anchor.
	var lastID uuid.UUID
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT id FROM employees WHERE tenant_id = $1
			 ORDER BY full_name DESC, id DESC LIMIT 1`, p.tenantID).Scan(&lastID)
	}); err != nil {
		t.Fatalf("reading the last person: %v", err)
	}
	past := employeesHref + "?after_id=" + lastID.String()
	res, body := p.get(t, past)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET a past-the-end cursor: %d, want 200", res.StatusCode)
	}
	if n := rosterRowCount(body); n != 0 {
		t.Fatalf("the past-the-end cursor returned %d card(s); the fixture's names must "+
			"sort before it or this test measures nothing", n)
	}
	for _, lie := range []string{
		"Nobody on the books yet",
		"No people have been added to this business yet",
	} {
		if strings.Contains(body, lie) {
			t.Errorf("a cursor past the last name rendered %q over a business with %q, "+
				"%q and %q on the books. The page measured a POSITION and reported a "+
				"BUSINESS.", lie, f.active, f.invited, f.departed)
		}
	}
	// AND IT SAYS THE TRUE THING INSTEAD, with a way back -- an empty state that only
	// removes a false sentence leaves the reader stuck at the end of a list.
	if !strings.Contains(body, "That is the end of the list") {
		t.Errorf("the past-the-end page does not say where the reader is.\nbody:\n%s", body)
	}
	if !strings.Contains(body, "Back to the start") {
		t.Errorf("the past-the-end page offers no way back to the top of the list")
	}
	// THE WAY BACK MUST WORK. A control that leads nowhere is the failure
	// TestEmployeesSection_OffersNoWriteAtAll exists to prevent, and it would be
	// introduced HERE rather than there.
	if href := nextRosterHref(body); href != "" {
		t.Errorf("the past-the-end page still offers a Next page link to %s", href)
	}
	back := regexp.MustCompile(`href="([^"]*)"[^>]*>Back to the start</a>`).FindStringSubmatch(body)
	if back == nil {
		t.Fatal("could not read the Back to the start href")
	}
	res2, body2 := p.get(t, htmlUnescaper.Replace(back[1]))
	if res2.StatusCode != http.StatusOK || rosterRowCount(body2) < 3 {
		t.Errorf("Back to the start answered %d with %d card(s); it must return to a "+
			"full list", res2.StatusCode, rosterRowCount(body2))
	}
}

// rosterNameRE reads the person's name out of each card. It is anchored on the
// "Who" label's own paragraph so it cannot pick up the venue or the department,
// which use the same typography.
var rosterNameRE = regexp.MustCompile(
	`(?s)<p class="docket-label">Who</p><p class="[^"]*">(.*?)</p>`)

func rosterNamesOn(body string) []string {
	var out []string
	for _, card := range rosterRowRE.FindAllString(body, -1) {
		if m := rosterNameRE.FindStringSubmatch(card); m != nil {
			out = append(out, strings.TrimSpace(m[1]))
		}
	}
	return out
}

// rosterPageByteCeiling is how big ONE page of the roster may get.
//
// 🔴 IT IS DERIVED FROM THE CONTROL M6-03 MEASURED AND REMOVED, not chosen. That
// page was 867 233 B, of which one unbounded <select> was 835 319 -- 96% -- on a
// Cache-Control: no-store document, and removing it is the reason this section
// exists at all. An eighth of it is the line this screen may not cross: far enough
// above what it actually renders to be a wall rather than a tripwire, far enough
// below the thing it replaced that the failure class cannot come back here.
//
// ⚠️ IT DOES NOT DISTINGUISH 50 FROM 100 AND IS NOT TRYING TO. Measured on the
// largest real tenant: 40 103 B at 50, 76 253 B at 100, 365 471 B at 500. So this
// catches the catastrophic direction and leaves the ordinary one to arithmetic and a
// human -- which is where the 2026-08-07 decision was taken.
const rosterPageByteCeiling = 867233 / 8

// TestPanelEmployeesDB_APageStaysFarUnderTheControlM603Removed is the UPWARD half of
// the page-size net; TestRosterPageSize_KeepsAWholeRosterInsideTheSessionBudget is
// the downward half.
//
// IT SEEDS MORE PEOPLE THAN FIT ON A PAGE, so the page it measures is a FULL one --
// a ceiling asserted against a half-empty page would pass at any page size.
func TestPanelEmployeesDB_APageStaysFarUnderTheControlM603Removed(t *testing.T) {
	p := newPanelHarness(t)
	f := seedRosterTenant(t, p, p.tenantID, "Iota")

	// Names as long as the real ones this screen renders. The development database's
	// longest is 108 characters and four rows are already over 100
	// (maxRosterCursorName records the measurement), so a fixture of "Bob" would
	// measure a page nobody will ever be served.
	const longName = "Iota Ġużeppi Żammit-Borġ [simulated hire 2026-08-07T10:37:42 abcd]"
	err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		for i := 0; i < ledger.RosterPageSize+5; i++ {
			if _, e := tx.Exec(ctx,
				`INSERT INTO employees (id, tenant_id, location_id, department_id, full_name, status, activated_at)
				 VALUES ($1, $2, $3, $4, $5, 'active', now())`,
				uuid.New(), p.tenantID, f.locationID, f.departmentID,
				fmt.Sprintf("%s %04d", longName, i)); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seeding a full page: %v", err)
	}

	p.signIn(t)
	_, body := p.get(t, employeesHref)
	if n := rosterRowCount(body); n != ledger.RosterPageSize {
		t.Fatalf("the page carries %d cards, not a full page of %d; a byte ceiling "+
			"measured on a short page proves nothing", n, ledger.RosterPageSize)
	}
	if len(body) > rosterPageByteCeiling {
		t.Errorf("one page of the roster is %d B, over the %d B ceiling.\n"+
			"That ceiling is an eighth of the 867 233 B control M6-03 measured and "+
			"REMOVED from the transactions section -- an unbounded roster on a "+
			"no-store page, re-sent on every view. This section exists because of "+
			"that measurement; it may not reproduce it.\n"+
			"RosterPageSize is %d.", len(body), rosterPageByteCeiling, ledger.RosterPageSize)
	}
	t.Logf("one full page of %d people with %d-character names: %d B (ceiling %d B)",
		ledger.RosterPageSize, len(longName)+5, len(body), rosterPageByteCeiling)
}

// ⚠️ THE REAL-DATA TWIN OF TestRosterPageSize_KeepsAWholeRosterInsideTheSessionBudget
// WAS ATTEMPTED AND IS STRUCTURALLY IMPOSSIBLE HERE, which is worth recording because
// it looks like an omission and is the opposite of one.
//
// The measurement it wanted is "the LARGEST roster in this database needs how many
// page-turns" — a max over EVERY tenant. The application role cannot take it:
// tappa_app is subject to RLS on employees (migration 00003, FORCE ROW LEVEL
// SECURITY), db.DB exposes no unscoped pool ON PURPOSE, and enumerating tenants to
// loop over them is blocked by the tenants policy too, which scopes on id. Writing
// that test would have meant adding a Pool() accessor — a §4.5 regression far more
// expensive than the assertion is worth. So the product genuinely cannot measure its
// own worst case; only an operator with the owner role can, and that measurement is
// recorded as prose in internal/handler/employees.go with the command beside it.
// What IS asserted here, within one tenant, is the page-turn ARITHMETIC that the
// budget derivation is built on — see the walk in
// TestPanelEmployeesDB_PagesThroughEverybodyExactlyOnce.

// TestPanelEmployeesDB_PagesPastAnAbsurdlyLongName is §4.6, and it is the test whose
// ABSENCE let a real defect ship.
//
// 🔴 THE DEFECT IT WAS WRITTEN FOR. The paging cursor used to carry the anchor's
// full_name in the query string, bounded at maxRosterCursorName. A name longer than
// that bound was DROPPED — and if such a name landed on a page boundary, "Next page"
// silently returned page one again. Measured by a security audit on real Postgres: a
// 60-person roster with a 605-rune name at row 50 served page 1, then page 1 again,
// leaving 10 people unreachable by browsing and listing 50 of them twice.
//
// employees.full_name is unbounded `text` in the live schema with no CHECK, so
// nothing prevented the input. Nothing in production writes an employee row today —
// the trigger arrives with seeding, an import, or phase B's invite form — which is
// why this is a defect to close in phase A rather than an incident.
//
// TWO WRITTEN CLAIMS WERE FALSIFIED BY THAT MEASUREMENT and are corrected where they
// live (parseRosterFilter): "dropping can only ever show MORE of the roster" — it
// showed LESS; and "it is visible because the filter bar echoes the values that took
// effect" — the cursor is echoed nowhere, so the drop was completely silent.
//
// The bound is gone now: the cursor carries an ID and the server reads the name. This
// test is what keeps it gone.
func TestPanelEmployeesDB_PagesPastAnAbsurdlyLongName(t *testing.T) {
	p := newPanelHarness(t)
	f := seedRosterTenant(t, p, p.tenantID, "Lambda")

	// Enough people for two pages.
	err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		for i := 0; i < ledger.RosterPageSize+10; i++ {
			if _, e := tx.Exec(ctx,
				`INSERT INTO employees (id, tenant_id, location_id, full_name, status)
				 VALUES ($1, $2, $3, $4, 'active')`,
				uuid.New(), p.tenantID, f.locationID,
				fmt.Sprintf("Lambda %04d", i)); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seeding the roster: %v", err)
	}
	p.signIn(t)

	// 🔴 THE LONG NAME IS PLACED ON THE BOUNDARY EMPIRICALLY, NOT BY ARITHMETIC, and
	// the reason is sharper than the one first written here.
	//
	// The first version computed the index and MISSED, so the test passed over the
	// defect it exists to reproduce. The explanation given was "the fixture's own
	// three people shift the boundary" -- true of THAT version, which named its rows
	// "Lambda P000...", and an audit could not reproduce it against the fixture as it
	// stands now. Both observations are right, and the difference is COLLATION:
	// measured in this container, 'Lambda P049' < 'Lambda Active Person' is FALSE
	// while 'Lambda 0049' < 'Lambda Active Person' is TRUE. Under the P-prefix the
	// three fixture people sorted FIRST and pushed the boundary down by three; under
	// the numeric names they sort last and the arithmetic would have landed.
	//
	// ⚠️ WHICH IS EXACTLY WHY THE ARITHMETIC IS NOT COMING BACK. That "would have
	// landed" holds only in a BYTE-ordering collation, which is what this image
	// happens to give (postgres:17-alpine on musl -- see
	// assertRosterIsInDatabaseOrder). On managed Postgres with glibc or ICU the
	// ordering of digits against letters is a different question, and an index
	// computed here would silently miss the boundary there -- a test that passes for
	// the wrong reason, on the deployment target, which is the worst place for one.
	// Asking the page which row it ends on is collation-independent.
	_, firstPage := p.get(t, employeesHref)
	names := rosterNamesOn(firstPage)
	if len(names) != ledger.RosterPageSize {
		t.Fatalf("page one holds %d rows, expected a full page of %d",
			len(names), ledger.RosterPageSize)
	}
	anchor := names[len(names)-1]

	// Appending keeps the row exactly where it sorted, so it is still the last row of
	// page one -- and it is now longer than any bound the cursor could carry.
	const absurd = 605
	longName := anchor + strings.Repeat("x", absurd)
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		tag, e := tx.Exec(ctx,
			`UPDATE employees SET full_name = $3 WHERE tenant_id = $1 AND full_name = $2`,
			p.tenantID, anchor, longName)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("updated %d rows, want 1", tag.RowsAffected())
		}
		return nil
	}); err != nil {
		t.Fatalf("lengthening the boundary row: %v", err)
	}

	// ANTI-VACUITY, BOTH HALVES: the name really is past the bound that used to drop
	// the cursor, and it really is the row page one ends on. Without either, this test
	// walks an ordinary roster and proves nothing -- which is exactly what its first
	// version did.
	if utf8.RuneCountInString(longName) <= 512 {
		t.Fatalf("the long name is %d runes; it must exceed the old 512-rune cursor "+
			"bound", utf8.RuneCountInString(longName))
	}
	_, firstPage = p.get(t, employeesHref)
	again := rosterNamesOn(firstPage)
	if len(again) == 0 || again[len(again)-1] != longName {
		t.Fatalf("the long name is not the last row of page one, so the cursor is not " +
			"built from it and this test measures nothing")
	}

	var want int
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM employees WHERE tenant_id = $1`, p.tenantID).Scan(&want)
	}); err != nil {
		t.Fatalf("counting: %v", err)
	}

	seen := map[string]int{}
	total, pages := 0, 0
	href := employeesHref
	for href != "" {
		res, body := p.get(t, href)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %d, want 200", href, res.StatusCode)
		}
		for _, n := range rosterNamesOn(body) {
			seen[n]++
			total++
		}
		pages++
		if pages > 20 {
			t.Fatalf("the roster did not terminate after 20 pages. A cursor that does not "+
				"advance is an infinite list: the page ending on a %d-rune name serves "+
				"itself forever and everybody after it is unreachable by browsing (§4.6). "+
				"Seen %d of %d.",
				utf8.RuneCountInString(longName), total, want)
		}
		href = nextRosterHref(body)
	}

	if total != want {
		t.Errorf("walking the roster listed %d rows over %d page(s); the tenant has %d.\n"+
			"A name on the page boundary must not cost the people behind it their place "+
			"on the list.", total, pages, want)
	}
	for n, c := range seen {
		if c != 1 {
			short := n
			if len(short) > 60 {
				short = short[:60] + "..."
			}
			t.Errorf("%q appears %d times across the pages; a stuck cursor repeats a page "+
				"and hides the rest", short, c)
			break
		}
	}
	if seen[longName] != 1 {
		t.Errorf("the %d-rune name appears %d times, want once",
			utf8.RuneCountInString(longName), seen[longName])
	}
	if pages < 2 {
		t.Fatalf("the roster fitted on %d page(s); this test measures paging", pages)
	}
}

// nextRosterRE reads the "Next page" link's href out of a rendered roster. It is
// anchored on the control's own text so it cannot pick up the filter bar's Clear
// link, which is the only other quiet button on the page.
var nextRosterRE = regexp.MustCompile(`href="([^"]*)"[^>]*>Next page</a>`)

func nextRosterHref(body string) string {
	m := nextRosterRE.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return htmlUnescaper.Replace(m[1])
}

// htmlUnescaper undoes the escaping templ applies to an href, so the string can be
// used as a URL again. Only the entities an ampersand-separated query string
// produces are needed.
var htmlUnescaper = strings.NewReplacer("&amp;", "&", "&#34;", `"`, "&#39;", "'")

// TestPanelEmployeesDB_SaysWhenADeviceHasNeverBeenUsED is the fourth device state,
// which is the one a template joining a count to a date renders wrongly.
//
// A session created and never used has last_used_at NULL (migration 00003 stamps it
// on USE), which is the ordinary state on the day somebody activates -- the day a
// manager is most likely to be reading this screen. It must not render the same way
// as "no device at all".
func TestPanelEmployeesDB_SaysWhenADeviceHasNeverBeenUsED(t *testing.T) {
	p := newPanelHarness(t)
	f := seedRosterTenant(t, p, p.tenantID, "Zeta")

	fresh := uuid.New()
	err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status, activated_at)
			 VALUES ($1, $2, $3, 'Zeta Just Activated', 'active', now())`,
			fresh, p.tenantID, f.locationID); e != nil {
			return e
		}
		// NO last_used_at: the column is left NULL, which is what CreateSession does.
		_, e := tx.Exec(ctx,
			`INSERT INTO sessions (tenant_id, employee_id, token_hash)
			 VALUES ($1, $2, $3)`,
			p.tenantID, fresh, "FRESH"+strings.ReplaceAll(uuid.NewString(), "-", ""))
		return e
	})
	if err != nil {
		t.Fatalf("seeding the fresh activation: %v", err)
	}

	p.signIn(t)
	_, body := p.get(t, employeesHref+"?name="+url.QueryEscape("Zeta Just Activated"))
	rows := rosterRowsOn(body)
	if n := rosterRowCount(body); n != 1 {
		t.Fatalf("expected exactly the one freshly activated person, got %d row(s)", n)
	}
	if !strings.Contains(rows, "1 signed in · not used yet") {
		t.Errorf("a device that exists but has never been used does not say so.\n"+
			"It must not read the same as %q, which is what a template joining a count "+
			"to a nullable date would produce.\nrow:\n%s", "None signed in", rows)
	}
}

// TestPanelEmployeesDB_TheSectionIsBehindTheSessionGate. The table-driven scan in
// dashboard_test.go covers every section, but it drives the FAKE reader; this drives
// the real one against real Postgres, which is the wiring a section only gets when
// it stops being a placeholder.
func TestPanelEmployeesDB_TheSectionIsBehindTheSessionGate(t *testing.T) {
	p := newPanelHarness(t)
	seedRosterTenant(t, p, p.tenantID, "Eta")

	res, _ := p.get(t, employeesHref)
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin/login" {
		t.Fatalf("signed OUT, GET %s: %d %q, want 303 /admin/login -- the roster is a "+
			"list of every person a business employs",
			employeesHref, res.StatusCode, res.Header.Get("Location"))
	}
	p.signIn(t)
	res, _ = p.get(t, employeesHref)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("signed in, GET %s: %d, want 200", employeesHref, res.StatusCode)
	}
}

// TestPanelEmployeesDB_ANameWithMarkupInItIsEscaped. Employee names are free text a
// manager types, so they are the one value on this page whose escaping matters.
// templ escapes on output; this proves it rather than asserting it, the same way
// TestReviewDB_AHostileNoteIsEscapedWhereItIsRendered does for the manager's note.
func TestPanelEmployeesDB_ANameWithMarkupInItIsEscaped(t *testing.T) {
	p := newPanelHarness(t)
	f := seedRosterTenant(t, p, p.tenantID, "Theta")

	const hostile = `Theta <script>alert("x")</script> & 'co'`
	err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status)
			 VALUES ($1, $2, $3, $4, 'active')`,
			uuid.New(), p.tenantID, f.locationID, hostile)
		return e
	})
	if err != nil {
		t.Fatalf("seeding the hostile name: %v", err)
	}

	p.signIn(t)
	_, body := p.get(t, employeesHref+"?name="+url.QueryEscape("Theta <script>"))
	if strings.Contains(body, "<script>alert") {
		t.Errorf("a name containing a script tag reached the page unescaped")
	}
	if !strings.Contains(rosterRowsOn(body), "&lt;script&gt;") {
		t.Errorf("the hostile name was not rendered at all, so the escaping claim is "+
			"vacuous.\nrows:\n%s", rosterRowsOn(body))
	}
}
