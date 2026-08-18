package handler

// employeeadd_db_test.go -- M6-13 END TO END, over real HTTP against real Postgres.
//
// 🔴 THE ONE TEST THIS CARD EXISTS FOR IS THE FIRST ONE BELOW: add somebody through
// the panel, then invite THAT PERSON through the panel, and require that the invite
// does NOT answer `not-invitable`. Everything else in this repository could be green
// while that chain was broken -- it WAS green, for two milestones, with no way to
// create the row at all.

import (
	"context"
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/tenant"
)

// seedPanelVenue creates a venue for the harness tenant and returns its id, without
// creating anybody to put in it -- which is the state a business is in before it
// hires its first person.
func seedPanelVenue(t *testing.T, p *panelHarness, name string) uuid.UUID {
	t.Helper()
	venue := uuid.New()
	err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, $3)`,
			venue, p.tenantID, name)
		return e
	})
	if err != nil {
		t.Fatalf("seed venue: %v", err)
	}
	return venue
}

// addedEmployeeID pulls the new person's id out of the redirect the add answered
// with. It is read from the Location header rather than from the database, because
// what the test needs is the id the PRODUCT is about to manage -- if those two ever
// differ, the next request is about somebody else.
func addedEmployeeID(t *testing.T, res *http.Response) uuid.UUID {
	t.Helper()
	loc := res.Header.Get("Location")
	if loc == "" {
		t.Fatalf("the add did not redirect (status %d)", res.StatusCode)
	}
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse redirect %q: %v", loc, err)
	}
	raw := u.Query().Get("manage")
	if raw == "" {
		t.Fatalf("the redirect %q does not name the new person; the invite button cannot be shown", loc)
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		t.Fatalf("the redirect names %q, which is not a uuid", raw)
	}
	return id
}

// 🔴 THE CARD'S HEADLINE CRITERION, MEASURED END TO END: ADD -> INVITE CLOSES ON ONE
// SCREEN. The production complaint was that a manager could not add anybody at all;
// the subtler half is that adding somebody must leave them IMMEDIATELY invitable, with
// no second state to reach first.
//
// `not-invitable` is asserted ABSENT by name. That is the exact answer employeeInvite
// gives for a row it will not invite, and it is what a person born in the wrong status
// would produce.
func TestEmployeeAddDB_AddThenInviteClosesOnOneScreen(t *testing.T) {
	p := newPanelHarness(t)
	venue := seedPanelVenue(t, p, "St Julians")
	p.signIn(t)

	res, _ := p.post(t, employeeAddHref, url.Values{
		"full_name":   {"Joseph Camilleri"},
		"location_id": {venue.String()},
	})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("add answered %d, want 303", res.StatusCode)
	}
	employee := addedEmployeeID(t, res)

	// THE ROW IS REALLY THERE, AND IT IS 'invited' WITH NO STAMPS. Read straight out of
	// the database rather than off the screen.
	var status string
	var invitedAt, activatedAt *string
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT status, invited_at::text, activated_at::text
			   FROM employees WHERE tenant_id = $1 AND id = $2`,
			p.tenantID, employee).Scan(&status, &invitedAt, &activatedAt)
	}); err != nil {
		t.Fatalf("read the new employee: %v", err)
	}
	if status != "invited" {
		t.Fatalf("the new employee is %q, want \"invited\" -- any other value skips activation", status)
	}
	if activatedAt != nil {
		t.Fatalf("activated_at = %v on a brand new employee", *activatedAt)
	}
	if invitedAt != nil {
		t.Fatalf("invited_at = %v, but no invitation has been sent", *invitedAt)
	}

	// 🔴 THE CHAIN. Invite the person that was just added, through the same panel.
	res2, body := p.post(t, employeeInviteHref, url.Values{"id": {employee.String()}})
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("inviting the person just added answered %d, want 200 with the link rendered.\n"+
			"If this is a 303 to ?problem=not-invitable, the add is creating people the "+
			"invite flow refuses -- which is the chain M6-13 exists to close.", res2.StatusCode)
	}
	if strings.Contains(body, "not-invitable") {
		t.Fatal("🔴 the invite answered `not-invitable` for somebody just added")
	}
	// The link really was minted, so the chain reached the end rather than merely
	// answering 200.
	if link := activationLinkIn(t, body); link == "" {
		t.Fatal("no activation link was rendered")
	}
	var pending int
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM employee_invites
			   WHERE tenant_id = $1 AND employee_id = $2 AND used_at IS NULL AND cancelled_at IS NULL`,
			p.tenantID, employee).Scan(&pending)
	}); err != nil {
		t.Fatalf("count invites: %v", err)
	}
	if pending != 1 {
		t.Fatalf("%d pending invitation(s) for the new employee, want 1", pending)
	}

	// 🔴 AND THE BUTTON IS ON THE SCREEN THE ADD REDIRECTED TO, which is what "one
	// screen" means. Following the redirect must render the new person's card with the
	// invite control on it -- otherwise the chain works but the manager cannot find it.
	_, roster := p.get(t, employeesHref+"?manage="+employee.String())
	if !strings.Contains(roster, `action="`+employeeInviteHref+`"`) {
		t.Error("the page the add redirects to does not offer the invite control")
	}
	if !strings.Contains(roster, "Joseph Camilleri") {
		t.Error("the page the add redirects to does not name the person who was just added")
	}
}

// 🔴 THE "added" RECEIPT IS NOT SHOWN ONCE THE PERSON HAS ACTIVATED, because by then
// every word of it is false.
//
// THE DEFECT WAS MEASURED, NOT IMAGINED: add somebody, activate them, reopen the same
// address. The page said "They cannot record time yet. Send them their activation
// link" beside somebody who was already tapping, and offered the re-invite control --
// which for an ACTIVE person issues a link for a NEW DEVICE. A stale tab, a bookmark
// or a shared URL is enough to reach it.
//
// 🔴 THE SIBLING WORD ALREADY HAD THIS GATE AND THAT IS WHAT MAKES IT AN OVERSIGHT
// RATHER THAN A JUDGEMENT CALL: confirmedRosterAction verifies `deactivated` against
// the status the same request read back, and `added` was simply not given the same
// check. `moved` is safe without one because its body prints only what that request
// measured; the added body asserts a lifecycle nobody re-read.
//
// THE CONTROL IS THE FIRST HALF: immediately after the add, the person IS 'invited'
// and the receipt MUST appear. Without that arm this test would pass against a screen
// that had simply stopped acknowledging additions at all.
func TestEmployeeAddDB_TheAddedReceiptIsNotShownAfterActivation(t *testing.T) {
	p := newPanelHarness(t)
	venue := seedPanelVenue(t, p, "St Julians")
	p.signIn(t)

	res, _ := p.post(t, employeeAddHref, url.Values{
		"full_name":   {"Freshly Hired"},
		"location_id": {venue.String()},
	})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("add answered %d", res.StatusCode)
	}
	employee := addedEmployeeID(t, res)
	replay := res.Header.Get("Location")

	// CONTROL: while they are still 'invited', the receipt and its next step are right.
	_, before := p.get(t, replay)
	if !strings.Contains(before, "They cannot record time yet") {
		t.Fatal("the receipt is missing straight after the add -- the control arm failed, so " +
			"the assertion below would prove nothing")
	}

	// Move them to the state a real activation produces. The gate under test reads
	// employees.status, so this fixture write supplies exactly its input; the real
	// activation path (spending an invite) is proven separately in the invite tests.
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`UPDATE employees SET status = 'active', activated_at = now()
			  WHERE tenant_id = $1 AND id = $2`, p.tenantID, employee)
		return e
	}); err != nil {
		t.Fatalf("activate: %v", err)
	}

	_, after := p.get(t, replay)
	if strings.Contains(after, "They cannot record time yet") {
		t.Error("🔴 the page tells a manager that somebody who has ALREADY ACTIVATED cannot " +
			"record time, and points them at an activation link that person does not need")
	}
	if strings.Contains(after, "On the roster") {
		t.Error("the 'added' receipt is still shown after activation; it describes a state the " +
			"person has left")
	}
	// AND THE PAGE IS STILL THE PAGE: the card for that person must still render, or
	// this test would pass against a screen that had broken outright.
	if !strings.Contains(after, "Freshly Hired") {
		t.Error("the person's card no longer renders at all")
	}
}

// 🔴 §4.6 AGAINST REAL POSTGRES: A BUSINESS WITH NO VENUE THAT POSTS IS TOLD THE
// REFUSAL. This is the configuration an audit used to find the defect, and it is kept
// as its own test because the fake-based twin
// (TestEmployeeAdd_AVenuelessBusinessIsToldTheRefusalItself) can only model the
// domain's answer -- here the REAL domain refuses, for the real reason: no location
// row exists for this tenant, so CreateEmployee's SELECT matches nothing.
//
// THE HARNESS SEEDS NO VENUE, deliberately. Every other test in this file calls
// seedPanelVenue first; this one is the state before that call, which is where a
// business genuinely starts.
func TestEmployeeAddDB_AVenuelessBusinessIsToldTheRefusal(t *testing.T) {
	p := newPanelHarness(t)
	p.signIn(t)

	before := countTenantEmployees(t, p, p.tenantID)

	res, body := p.post(t, employeeAddHref, url.Values{
		"full_name":   {"Nowhere To Put Them"},
		"location_id": {uuid.NewString()}, // there is no venue to name
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("a venueless add answered %d, want 200 with the refusal on the page", res.StatusCode)
	}
	if !strings.Contains(body, "Nobody was added") {
		t.Error("🔴 the server refused and the page does not say so -- §4.6: a refusal is never silent")
	}
	if !strings.Contains(body, "Add a venue first") {
		t.Error("the page does not say what to do about it")
	}
	if got := countTenantEmployees(t, p, p.tenantID); got != before {
		t.Errorf("the roster grew from %d to %d on a refused add", before, got)
	}
}

// 🔴 §4.5 END TO END: A CROSS-TENANT VENUE POSTED AT THE ROUTE IS REFUSED, and nobody
// is created in either business. This is the HTTP half of the domain test; it exists
// separately because the handler is where a forged id actually arrives.
func TestEmployeeAddDB_AnotherBusinessesVenueIsRefused(t *testing.T) {
	p := newPanelHarness(t)
	seedPanelVenue(t, p, "St Julians")
	// A REAL venue belonging to a REAL other business.
	otherTenant := seedOtherBusiness(t, p)
	foreignVenue := uuid.New()
	if err := p.data.WithTenant(context.Background(), otherTenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'Their Venue')`,
			foreignVenue, otherTenant)
		return e
	}); err != nil {
		t.Fatalf("seed foreign venue: %v", err)
	}
	p.signIn(t)

	before := countTenantEmployees(t, p, p.tenantID)
	beforeForeign := countTenantEmployees(t, p, otherTenant)

	res, body := p.post(t, employeeAddHref, url.Values{
		"full_name":   {"Intruder"},
		"location_id": {foreignVenue.String()},
	})
	// The form comes back with the refusal on it; nothing is created anywhere.
	if res.StatusCode != http.StatusOK {
		t.Fatalf("a cross-tenant add answered %d, want 200 with the form back", res.StatusCode)
	}
	if !strings.Contains(body, "belongs to this business") {
		t.Errorf("the refusal does not explain itself")
	}
	if got := countTenantEmployees(t, p, p.tenantID); got != before {
		t.Errorf("this business's roster grew from %d to %d", before, got)
	}
	if got := countTenantEmployees(t, p, otherTenant); got != beforeForeign {
		t.Errorf("🔴 the OTHER business's roster grew from %d to %d -- a cross-tenant write",
			beforeForeign, got)
	}
}

// countTenantEmployees counts one business's roster in its OWN tenant context, so RLS
// scopes the read the way production's does.
func countTenantEmployees(t *testing.T, p *panelHarness, tenantID uuid.UUID) int {
	t.Helper()
	var n int
	if err := p.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM employees WHERE tenant_id = $1`, tenantID).Scan(&n)
	}); err != nil {
		t.Fatalf("count employees: %v", err)
	}
	return n
}

// seedOtherBusiness creates a second business to attack the harness tenant from.
//
// It is NOT seedPanelTenant (transactions_db_test.go): that one builds a whole tenant
// with venues, staff, plaques and transactions, and what §4.5 needs here is the
// opposite -- a business with a venue and NOBODY in it, so that a cross-tenant write
// would be visible as its roster growing from zero.
func seedOtherBusiness(t *testing.T, p *panelHarness) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := p.data.WithTenant(context.Background(), id, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Other Business Ltd', $2, 'restaurant', 'single')`,
			id, "VAT-"+id.String())
		return e
	}); err != nil {
		t.Fatalf("seed other tenant: %v", err)
	}
	return id
}

// 🔴 THE PARTIAL UNIQUE INDEX, END TO END OVER HTTP. Two people with no address are
// legal; a duplicate address is refused with a SENTENCE rather than a 500 -- and case
// makes no difference, because the column is citext.
//
// The empty-field case is the one that matters most: a browser posts `email=` for an
// untouched box, and if that reached the column as ” rather than NULL, the SECOND
// person a manager added without an address would be refused as a duplicate of the
// first.
func TestEmployeeAddDB_TheEmailRulesBehaveAsTheIndexSays(t *testing.T) {
	p := newPanelHarness(t)
	venue := seedPanelVenue(t, p, "St Julians")
	p.signIn(t)

	add := func(name, email string) *http.Response {
		res, _ := p.post(t, employeeAddHref, url.Values{
			"full_name":   {name},
			"location_id": {venue.String()},
			"email":       {email},
		})
		return res
	}

	// 1. TWO PEOPLE WITH AN EMPTY EMAIL FIELD ARE BOTH CREATED.
	for _, name := range []string{"No Address One", "No Address Two"} {
		if res := add(name, ""); res.StatusCode != http.StatusSeeOther {
			t.Fatalf("adding %q with an empty email answered %d, want 303 -- "+
				"an empty field must reach the column as NULL, not ''", name, res.StatusCode)
		}
	}

	// 2. A REAL ADDRESS IS STORED, AND A DUPLICATE OF IT IS REFUSED -- in any case.
	if res := add("First Holder", "shared@example.mt"); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("adding the first address holder answered %d", res.StatusCode)
	}
	for _, dup := range []string{"shared@example.mt", "SHARED@EXAMPLE.MT"} {
		res, body := p.post(t, employeeAddHref, url.Values{
			"full_name":   {"Second Holder"},
			"location_id": {venue.String()},
			"email":       {dup},
		})
		if res.StatusCode != http.StatusOK {
			t.Fatalf("a duplicate address (%q) answered %d, want 200 with the form back -- "+
				"never a 500", dup, res.StatusCode)
		}
		if !strings.Contains(strings.ToLower(body), "already has that address") {
			t.Errorf("the refusal for %q does not say the address is taken", dup)
		}
		// 🔴 THE CASE SENTENCE. Without it a manager retypes the address with a
		// different capital and is refused again with no idea why.
		if !strings.Contains(strings.ToLower(body), "capitals make no") {
			t.Errorf("the refusal for %q does not explain that capitals make no difference", dup)
		}
	}
}

// The WHOLE SENTENCES this test requires on the page, character for character.
//
// 🔴 THEY ARE DECLARED HERE AND NOT IMPORTED FROM THE HANDLER, AND THAT IS THE WHOLE
// MECHANISM. Asserting `strings.Contains(body, addBillingNote)` is a TAUTOLOGY -- the
// page contains the constant because it is rendered FROM the constant, so any text
// satisfies it, including the opposite of the truth about a customer's money. A copy
// written down HERE is a second representation ON PURPOSE: it is the only way a test
// can DISAGREE with the product.
//
// 🔴 AND THEY ARE WHOLE SENTENCES BECAUSE FRAGMENTS WERE NOT ENOUGH -- MEASURED TWICE,
// IN THE SAME PLACE. Round 2 pinned two short phrases and an audit defeated them with
// a sentence that CONTAINED the required phrase and still lied:
//
//	"Adding somebody does not change your bill UNTIL MIDNIGHT -- from then on they are
//	 counted at your plan's full monthly rate, whether or not they ever activate."
//
// That is coherent, shippable and false, and it passed because "does not change your
// bill" is a prefix of it. A fragment pins a substring; only the whole sentence pins
// the CLAIM. The cost is that rewording the shipped text means editing this file --
// which is the review a statement about somebody's money should get, and is the point
// rather than the price.
const (
	sentenceNotBillableYet = "Adding somebody does not change your bill on its own — " +
		"people start counting towards it when they activate, not when they are added."
	sentenceBillableOnAdd = "Adding somebody starts charging for them straight away, " +
		"at your plan's monthly rate — they are counted from the moment they are added."
)

// 🔴 THE DATABASE DECIDES WHICH SENTENCE IS TRUE, AND THE PAGE MUST CARRY THAT ONE.
// This is the card's fifth criterion with teeth: it asks
// tappa_employee_is_billable (migration 0016) about a row it has just created THROUGH
// THE PANEL, and then requires the rendered form to make the matching claim and NOT
// the opposing one.
//
// Both directions are asserted, so the test cannot be satisfied by a page that hedges,
// says both, or says neither. And because the phrases above live in this file rather
// than in the handler, rewriting the shipped sentence -- in either direction -- turns
// this red.
//
// THE SECOND ASSERTION IS NOT DECORATION: `unstamped` is the count the billing screen
// surfaces as an anomaly, so a new hire landing in it would make every addition look
// like a billing fault.
func TestEmployeeAddDB_TheBillingNoteMatchesThePredicate(t *testing.T) {
	p := newPanelHarness(t)
	venue := seedPanelVenue(t, p, "St Julians")
	p.signIn(t)

	res, _ := p.post(t, employeeAddHref, url.Values{
		"full_name":   {"Freshly Added"},
		"location_id": {venue.String()},
	})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("add answered %d", res.StatusCode)
	}
	employee := addedEmployeeID(t, res)

	var billable, lifecycleAgrees bool
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT tappa_employee_is_billable(e.status, e.activated_at, e.deactivated_at,
			                                   now() - interval '1 month', now() + interval '1 month'),
			        tappa_employee_lifecycle_status(e.activated_at, e.deactivated_at) = e.status
			   FROM employees e WHERE e.tenant_id = $1 AND e.id = $2`,
			p.tenantID, employee).Scan(&billable, &lifecycleAgrees)
	}); err != nil {
		t.Fatalf("ask the billing predicate: %v", err)
	}
	if !lifecycleAgrees {
		t.Fatal("🔴 a freshly added employee counts as `unstamped` -- the billing screen " +
			"would surface every new hire as an anomaly")
	}

	// NOW READ THE FORM THE MANAGER ACTUALLY SEES.
	_, raw := p.get(t, employeesHref+"?"+addParam+"="+addOpenValue)
	// THE BODY IS UNESCAPED BEFORE COMPARING, because templ escapes the apostrophe in
	// "plan's" to &#39; and the sentence is compared WHOLE. Unescaping is the honest
	// way round: escaping the expectation instead would make this assertion depend on
	// which characters templ happens to escape today.
	form := html.UnescapeString(raw)

	required, forbidden := sentenceNotBillableYet, sentenceBillableOnAdd
	if billable {
		required, forbidden = sentenceBillableOnAdd, sentenceNotBillableYet
	}
	if !strings.Contains(form, required) {
		t.Errorf("🔴 tappa_employee_is_billable says billable=%v for a freshly added person, so "+
			"the add form must carry EXACTLY this sentence and it does not:\n  want: %q\n"+
			"A manager is being told the wrong thing about what they are charged. If the "+
			"wording changed deliberately, change it here too -- that review is the point.",
			billable, required)
	}
	if strings.Contains(form, forbidden) {
		t.Errorf("🔴 tappa_employee_is_billable says billable=%v, but the add form carries the "+
			"OPPOSING sentence:\n  found: %q", billable, forbidden)
	}
}

// 🔴 THE ROW AND ITS AUDIT TRAIL ARRIVE TOGETHER, over HTTP, and the trail names the
// admin from the SESSION. An administrative act with no attributable actor is what
// audit_log.actor_id exists to prevent.
func TestEmployeeAddDB_TheHireIsTrailedAndAttributed(t *testing.T) {
	p := newPanelHarness(t)
	venue := seedPanelVenue(t, p, "St Julians")
	p.signIn(t)

	res, _ := p.post(t, employeeAddHref, url.Values{
		"full_name":   {"Trailed Person"},
		"location_id": {venue.String()},
		"email":       {"trailed@example.mt"},
	})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("add answered %d", res.StatusCode)
	}
	employee := addedEmployeeID(t, res)

	if n := employeeAuditRows(t, p, tenant.ActionEmployeeAdded, employee); n != 1 {
		t.Fatalf("%d employee.added row(s), want exactly 1", n)
	}
	// §4.7 -- neither the name nor the address may be copied into an append-only table.
	var detail string
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT detail::text FROM audit_log
			   WHERE tenant_id = $1 AND action = $2 AND target = $3`,
			p.tenantID, tenant.ActionEmployeeAdded, employee.String()).Scan(&detail)
	}); err != nil {
		t.Fatalf("read audit detail: %v", err)
	}
	for _, secret := range []string{"Trailed Person", "trailed@example.mt"} {
		if strings.Contains(detail, secret) {
			t.Errorf("🔴 the audit detail contains %q: %s", secret, detail)
		}
	}
}

// TWO IDENTICAL SUBMISSIONS CREATE TWO PEOPLE, and that is the CORRECT answer rather
// than a missing guard: two real people can share a name, and nothing in the schema
// says otherwise. What protects a manager from a double-click is POST-redirect-GET --
// the success answers 303, so a refresh re-fetches the roster instead of re-posting.
//
// It is written down because the opposite looks like a safety feature and would make
// hiring two brothers impossible.
func TestEmployeeAddDB_ASecondIdenticalSubmissionCreatesASecondPerson(t *testing.T) {
	p := newPanelHarness(t)
	venue := seedPanelVenue(t, p, "St Julians")
	p.signIn(t)

	form := url.Values{"full_name": {"John Borg"}, "location_id": {venue.String()}}
	res1, _ := p.post(t, employeeAddHref, form)
	res2, _ := p.post(t, employeeAddHref, form)
	if res1.StatusCode != http.StatusSeeOther || res2.StatusCode != http.StatusSeeOther {
		t.Fatalf("the two adds answered %d and %d, want 303 both", res1.StatusCode, res2.StatusCode)
	}
	first, second := addedEmployeeID(t, res1), addedEmployeeID(t, res2)
	if first == second {
		t.Fatal("the two submissions returned the same person")
	}
	// 🔴 AND THE SUCCESS IS A REDIRECT, which is what makes a browser refresh safe.
	if loc := res2.Header.Get("Location"); loc == "" {
		t.Fatal("the add did not redirect, so a refresh would re-post it")
	}
}
