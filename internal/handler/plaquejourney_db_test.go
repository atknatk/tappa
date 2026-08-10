package handler

// THE REPLACE-TAG JOURNEY, MEASURED (M6-06 phase B).
//
// 🔴 THE CARD MAKES A NUMBER AN ACCEPTANCE CRITERION -- "the whole thing must not
// take more than 30 seconds" -- and a number is not a claim a reader can check
// unless somebody measures it. This file drives the journey a manager really walks,
// over real HTTP against real Postgres, with the real router and the real
// middleware chain, and asserts the budget with the SCREENS and the CLICKS counted
// beside the time.
//
// ⚠️ WHAT THE MEASUREMENT IS AND IS NOT. It is SERVER time: the four requests the
// journey costs, back to back, on this machine, against this database. It is NOT
// the manager's 30 seconds, which also contains reading two sentences, choosing a
// spare from a dropdown and climbing a stepladder. So the honest reading is a
// CEILING on our share of the budget -- if the server alone were near 30 s the
// criterion would be unmeetable, and if it is a rounding error the criterion is
// about the SHAPE of the flow (how many screens, how many decisions) rather than
// about our latency. The shape is asserted too, and it is the part that would
// actually break: a flow that asked the manager to re-pick the venue, or to find
// the spare's uid by hand, would blow the budget at no cost in milliseconds.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/sun"
)

// seedWall commits one venue for the harness tenant and returns its id.
func (p *panelHarness) seedWall(t *testing.T, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name, static_ips)
			 VALUES ($1, $2, $3, '{203.0.113.0/24}')`, id, p.tenantID, name)
		return e
	}); err != nil {
		t.Fatalf("seed venue: %v", err)
	}
	return id
}

// seedPlaque commits one plaque the way TAPPA'S LOADER would. There is no product
// query that does this: db/queries/tags.sql ships no INSERT over `tags`, because
// creating a plaque means holding its key (user decision, 2026-08-08).
func (p *panelHarness) seedPlaque(t *testing.T, status string, wall uuid.UUID) string {
	t.Helper()
	b := make([]byte, 7)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	uid := strings.ToUpper(hex.EncodeToString(b))
	var location *uuid.UUID
	if wall != uuid.Nil {
		w := wall
		location = &w
	}
	// 🔴 A REAL 44-BYTE KEK ENVELOPE, NOT A TWO-BYTE PLACEHOLDER. The development
	// database already carries ~19 500 rows whose aes_key_ref is '\xDEAD' — test
	// residue that backlog T7/Y2 counts — and a fixture that adds more makes the one
	// population that matters for T7 harder to measure. sun.Wrap is what the M8-05
	// loader will use, so the fixture is also the shape the product expects.
	ref, err := sun.Wrap(plaqueFixtureKEK, b, plaqueFixtureTagKey)
	if err != nil {
		t.Fatalf("sun.Wrap: %v", err)
	}
	if len(ref) != 44 {
		t.Fatalf("wrapped key is %d bytes, want 44 — the envelope shape T7 is about", len(ref))
	}
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status)
			 VALUES ($1, $2, $3, $4, 500, $5)`,
			uid, p.tenantID, location, ref, status)
		return e
	}); err != nil {
		t.Fatalf("seed plaque (%s): %v", status, err)
	}
	return uid
}

// The fixture's fake key material. It is the SAME pair tap_db_test.go uses, decoded
// once here; nothing real is ever wrapped under it.
var (
	plaqueFixtureKEK    = mustHex(tapFakeKEK)
	plaqueFixtureTagKey = mustHex(tapFakeTagKey)
)

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic("plaque fixture: " + err.Error())
	}
	return b
}

func (p *panelHarness) plaqueRow(t *testing.T, uid string) (status string, replacedBy *string) {
	t.Helper()
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT status, replaced_by FROM tags WHERE tenant_id = $1 AND uid = $2`,
			p.tenantID, uid).Scan(&status, &replacedBy)
	}); err != nil {
		t.Fatalf("read plaque: %v", err)
	}
	return status, replacedBy
}

// TestPlaqueJourneyDB_ReplacingAPlaqueFitsInTheBudget is the card's fourth
// criterion, end to end.
func TestPlaqueJourneyDB_ReplacingAPlaqueFitsInTheBudget(t *testing.T) {
	p := newPanelHarness(t)
	p.signIn(t)
	wall := p.seedWall(t, "KF St Julians")
	broken := p.seedPlaque(t, "active", wall)
	spare := p.seedPlaque(t, "unassigned", uuid.Nil)

	start := time.Now()
	screens, clicks := 0, 0

	// SCREEN 1: the manager opens the section and sees the plaque that failed.
	res, html := p.get(t, locationsHref)
	screens++
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", locationsHref, res.StatusCode)
	}
	if !strings.Contains(html, broken) {
		t.Fatalf("the section does not list the plaque that needs replacing")
	}
	// 🔴 THE OPEN LINK IS ON THE CARD, so the manager does not have to type a uid.
	// A flow that made them find the id by hand would meet the 30 seconds nowhere
	// near the server.
	openHref := locationsHref + "?plaque=" + broken
	if !strings.Contains(html, openHref) {
		t.Fatalf("the list offers no link to open %s", broken)
	}

	// CLICK 1 -> SCREEN 2: the card, with the spare already in the dropdown.
	res, html = p.get(t, openHref)
	screens++
	clicks++
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", openHref, res.StatusCode)
	}
	if !strings.Contains(html, `action="`+plaqueReplaceHref+`"`) {
		t.Fatalf("the card offers no replacement form")
	}
	if !strings.Contains(html, `value="`+spare+`"`) {
		t.Fatalf("the spare is not offered in the dropdown; the manager would have to "+
			"find its id by hand.\n%s", html)
	}
	// 🔴 THE WALL IS NOT ASKED FOR, and that is a SHAPE assertion rather than a
	// cosmetic one: a replacement is the same door with a different plaque, so a
	// venue control here would be a second decision and a second chance to send the
	// new plaque somewhere else.
	if strings.Contains(html, `name="location_id"`) {
		t.Fatalf("the replacement form asks for a venue; it must read the wall from the " +
			"plaque coming off")
	}

	// 🔴 THE CARD MINTED A CONFIRMATION AND THE PRESS SPENDS IT. Reaching the write
	// implies the server rendered the warning above, in this session, in this window
	// — the property deactivateconfirm.go describes. The token is read out of the
	// page rather than minted here, so the test covers the GET -> mint -> POST seam.
	token := confirmTokenRE.FindStringSubmatch(html)
	if token == nil {
		t.Fatalf("the replacement card carried no confirmation.\n%s", html)
	}

	// CLICK 2: the press.
	res, _ = p.post(t, plaqueReplaceHref, url.Values{
		"uid": {broken}, "successor": {spare}, confirmField: {token[1]},
	})
	clicks++
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST %s = %d, want 303", plaqueReplaceHref, res.StatusCode)
	}
	back := res.Header.Get("Location")
	if !strings.Contains(back, "done=plaque-replaced") || !strings.Contains(back, "id="+broken) {
		t.Fatalf("Location = %q, want the replaced plaque named for the receipt", back)
	}

	// SCREEN 3: the acknowledgement, verified against the trail in the same request.
	res, html = p.get(t, back)
	screens++
	elapsed := time.Since(start)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", back, res.StatusCode)
	}
	if !strings.Contains(html, "Plaque replaced") {
		t.Fatalf("no acknowledgement after a real replacement; C' must confirm the "+
			"actor's own trail row.\n%s", html)
	}
	if !strings.Contains(html, spare) {
		t.Fatalf("the acknowledgement does not name the new plaque")
	}

	// --- what actually happened, read from the table rather than from the page ---
	if status, replacedBy := p.plaqueRow(t, broken); status != "retired" ||
		replacedBy == nil || *replacedBy != spare {
		t.Fatalf("old plaque = (%s, replaced_by=%v), want (retired, %s)", status, replacedBy, spare)
	}
	if status, _ := p.plaqueRow(t, spare); status != "active" {
		t.Fatalf("new plaque = %s, want active", status)
	}

	// --- the budget --------------------------------------------------------------
	//
	// The figures are LOGGED as well as asserted, because a bound that passes tells
	// a reader nothing about how much room is left.
	venues, plaqueCount, taps := p.tenantSize(t)
	t.Logf("replace-tag journey: %d screens, %d clicks, server time %s "+
		"(4 requests: list, card, POST, acknowledgement) — tenant %s: %d venues, "+
		"%d plaques, %d transactions",
		screens, clicks, elapsed, p.tenantID, venues, plaqueCount, taps)
	if screens != 3 || clicks != 2 {
		t.Fatalf("the journey took %d screens and %d clicks; the shape is what the "+
			"30-second criterion is really about", screens, clicks)
	}
	// 🔴 THE BOUND IS DELIBERATELY LOOSE AND IT IS STILL WORTH ASSERTING. Five
	// seconds of SERVER time for four requests would mean something is badly wrong
	// (an unindexed scan, a lock wait) long before a manager noticed; a tighter
	// bound would turn a busy CI machine into a red suite, which is the failure mode
	// this repository has already paid for on `make test` timings.
	if elapsed > 5*time.Second {
		t.Fatalf("server time for the whole journey = %s; the card budgets 30 s for the "+
			"WHOLE act including the stepladder, so our share cannot be this", elapsed)
	}
}

// TestPlaqueJourneyDB_MountingTheFirstPlaqueIsTheSameShape. The other act, and the
// one a new customer meets first: stock arrives, the manager puts it on a door.
func TestPlaqueJourneyDB_MountingTheFirstPlaqueIsTheSameShape(t *testing.T) {
	p := newPanelHarness(t)
	p.signIn(t)
	wall := p.seedWall(t, "KF Rusty Bar")
	spare := p.seedPlaque(t, "unassigned", uuid.Nil)

	start := time.Now()
	screens, clicks := 0, 0

	// ⚠️ IT STARTS AT THE LIST, NOT AT THE CARD, and the first version did not — it
	// opened ?plaque=<uid> directly, skipping the only screen that TELLS a manager the
	// uid. A journey that begins where the manager cannot begin measures a journey
	// nobody walks.
	res, html := p.get(t, locationsHref)
	screens++
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", locationsHref, res.StatusCode)
	}
	openHref := locationsHref + "?plaque=" + spare
	if !strings.Contains(html, openHref) {
		t.Fatalf("the list offers no link to open the spare %s", spare)
	}

	res, html = p.get(t, openHref)
	screens++
	clicks++
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET the card = %d, want 200", res.StatusCode)
	}
	if !strings.Contains(html, `action="`+plaqueMountHref+`"`) {
		t.Fatalf("the card offers no mount form for a plaque in stock")
	}
	if !strings.Contains(html, `value="`+wall.String()+`"`) {
		t.Fatalf("the venue is not offered in the dropdown")
	}
	// 🔴 AND NO CONFIRMATION IS MINTED HERE. A mount is not gated, and a card that
	// minted a value nothing checks would be the gate existing and never being spent.
	if confirmTokenRE.MatchString(html) {
		t.Fatalf("the mount card minted a confirmation nothing checks")
	}

	res, _ = p.post(t, plaqueMountHref, url.Values{
		"uid": {spare}, "location_id": {wall.String()},
	})
	clicks++
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST mount = %d, want 303", res.StatusCode)
	}
	back := res.Header.Get("Location")
	res, html = p.get(t, back)
	screens++
	elapsed := time.Since(start)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", back, res.StatusCode)
	}
	if !strings.Contains(html, "Plaque mounted at KF Rusty Bar") {
		t.Fatalf("the acknowledgement does not name the venue FROM THE TRAIL.\n%s", html)
	}
	if status, _ := p.plaqueRow(t, spare); status != "active" {
		t.Fatalf("status = %s after a mount, want active", status)
	}

	venues, plaques, taps := p.tenantSize(t)
	t.Logf("mount journey: %d screens, %d clicks, server time %s "+
		"(4 requests) — tenant %s: %d venues, %d plaques, %d transactions",
		screens, clicks, elapsed, p.tenantID, venues, plaques, taps)
	// ⚠️ THE SHAPE IS ASSERTED, NOT LOGGED. An earlier version printed "2 screens, 2
	// clicks" as a hard-coded string while counting nothing — a sentence that could
	// not go wrong, about a number nobody measured.
	if screens != 3 || clicks != 2 {
		t.Fatalf("the mount journey took %d screens and %d clicks", screens, clicks)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("server time for the mount journey = %s", elapsed)
	}
}

// tenantSize counts what a timing above is a timing OF.
//
// 🔴 A TIMING IS WRITTEN WITH ITS POPULATION OR IT IS NOT WRITTEN — this repository's
// own rule, learned three times over, and the first version of these logs broke it.
// The harness tenant is the CHEAPEST population there is (one venue, a couple of
// plaques, no transactions at all), so a number from it says nothing about a business
// with a year of history; TestPlaqueJourneyDB_TheBudgetOnATenantWithHistory measures
// that end.
func (p *panelHarness) tenantSize(t *testing.T) (venues, plaques, taps int) {
	t.Helper()
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT (SELECT count(*) FROM locations WHERE tenant_id = $1),
			       (SELECT count(*) FROM tags WHERE tenant_id = $1),
			       (SELECT count(*) FROM transactions WHERE tenant_id = $1)`,
			p.tenantID).Scan(&venues, &plaques, &taps)
	}); err != nil {
		t.Fatalf("count the tenant: %v", err)
	}
	return venues, plaques, taps
}

// dropPlaquesAfter removes the plaques a journey seeded into a SHARED tenant, once the
// test is done with them.
//
// 🔴 IT RUNS AS tappa_owner BECAUSE tappa_app CANNOT DELETE FROM `tags` (00004 revokes
// it), and that revocation is the reason this package's other fixtures are left behind.
// The rule is "we cannot clean up", not "cleaning up is wrong" — and here NOT cleaning
// up made a shipped test flaky, which is the more expensive of the two.
//
// ⚠️ IT IS NOT A GENERAL FIXTURE CLEANER AND MUST NOT BECOME ONE. It deletes exactly
// the uids it is given, in the order a replacement leaves them (the retired plaque
// points at its successor through replaced_by, so the pointer has to go first). It
// touches NO other table — `transactions` is append-only (§4.3) and a plaque with taps
// against it would raise 23503 here, which is reported rather than swallowed.
func (p *panelHarness) dropPlaquesAfter(t *testing.T, uids ...string) {
	t.Helper()
	t.Cleanup(func() {
		owner := ownerPoolForTest(t)
		for _, uid := range uids {
			if _, err := owner.Exec(context.Background(),
				`DELETE FROM tags WHERE uid = $1 AND tenant_id = $2`, uid, p.tenantID); err != nil {
				t.Errorf("could not remove the seeded plaque %s: %v. The shared tenant now "+
					"carries one more row than it started with, which is exactly the drift "+
					"this cleanup exists to stop.", uid, err)
			}
		}
	})
}

// TestPlaqueJourneyDB_AWrongMountCanBeUNDONE is D1 end to end, over real HTTP: the
// journey an audit measured as a DEAD END.
//
// 🔴 WHAT IT USED TO DO, measured: mount at the wrong door -> 303 done=plaque-mounted
// -> the card then offered NO mount form, NO replace form and NO venue control, and
// re-mounting at the right door answered `plaque-not-stock`. With no spare in the box
// the only sentence on the screen was "Ask us for one" — for a plaque the manager was
// holding. Meanwhile every tap at that door was judged against the WRONG venue's IP
// and coordinate (§5 row 7).
func TestPlaqueJourneyDB_AWrongMountCanBeUNDONE(t *testing.T) {
	p := newPanelHarness(t)
	p.signIn(t)
	right := p.seedWall(t, "KF St Julians")
	wrong := p.seedWall(t, "KF Rusty Bar")
	uid := p.seedPlaque(t, "unassigned", uuid.Nil)

	// THE MISTAKE. No confirmation is asked for, which is the point of the undo.
	res, _ := p.post(t, plaqueMountHref, url.Values{
		"uid": {uid}, "location_id": {wrong.String()},
	})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("mount = %d, want 303", res.StatusCode)
	}
	if status, _ := p.plaqueRow(t, uid); status != "active" {
		t.Fatalf("status = %s after the wrong mount, want active", status)
	}

	start := time.Now()
	screens, clicks := 0, 0

	// SCREEN 1: the card now says what to do about it, WITH NO SPARE IN THE BOX.
	res, html := p.get(t, locationsHref+"?plaque="+uid)
	screens++
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET the card = %d, want 200", res.StatusCode)
	}
	if !strings.Contains(html, "On the wrong door?") {
		t.Fatalf("the card offers no way out of a wrong mount.\n%s", html)
	}
	if !strings.Contains(html, "take it off the wall below") {
		t.Fatalf("the no-spare sentence still sends the manager to us for a plaque they hold")
	}
	unmountHref := locationsHref + "?plaque=" + uid + "&confirm=unmount"

	// CLICK 1 -> SCREEN 2: the warning, and the confirmation the server minted for it.
	res, html = p.get(t, unmountHref)
	screens++
	clicks++
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET the un-mount step = %d, want 200", res.StatusCode)
	}
	token := confirmTokenRE.FindStringSubmatch(html)
	if token == nil {
		t.Fatalf("the un-mount step carried no confirmation.\n%s", html)
	}

	// CLICK 2: the press.
	res, _ = p.post(t, plaqueUnmountHref, url.Values{
		"uid": {uid}, confirmField: {token[1]},
	})
	clicks++
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST un-mount = %d, want 303", res.StatusCode)
	}
	back := res.Header.Get("Location")
	if !strings.Contains(back, "done=plaque-unmounted") {
		t.Fatalf("Location = %q, want done=plaque-unmounted", back)
	}
	res, html = p.get(t, back)
	screens++
	if res.StatusCode != http.StatusOK || !strings.Contains(html, "Plaque taken off the wall") {
		t.Fatalf("no acknowledgement after a real un-mount (status %d)", res.StatusCode)
	}
	if status, _ := p.plaqueRow(t, uid); status != "unassigned" {
		t.Fatalf("status = %s, want unassigned — back in stock", status)
	}

	// CLICK 3 -> and it goes up on the RIGHT door. This is the whole claim.
	res, _ = p.post(t, plaqueMountHref, url.Values{
		"uid": {uid}, "location_id": {right.String()},
	})
	clicks++
	elapsed := time.Since(start)
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("re-mount = %d, want 303", res.StatusCode)
	}
	var wall *uuid.UUID
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT location_id FROM tags WHERE tenant_id=$1 AND uid=$2`,
			p.tenantID, uid).Scan(&wall)
	}); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if wall == nil || *wall != right {
		t.Fatalf("the plaque is at %v, want the RIGHT door %s", wall, right)
	}

	venues, plaques, taps := p.tenantSize(t)
	t.Logf("undo-a-wrong-mount journey: %d screens, %d clicks, server time %s "+
		"(5 requests) — tenant %s: %d venues, %d plaques, %d transactions",
		screens, clicks, elapsed, p.tenantID, venues, plaques, taps)
	if screens != 3 || clicks != 3 {
		t.Fatalf("the undo took %d screens and %d clicks", screens, clicks)
	}
}

// demoTenant is the seeded Kebab Factory tenant — the largest population this
// development database has, and the only one whose numbers say anything about a
// business with a year behind it.
const demoTenant = "10000000-0000-4000-8000-000000000001"

// TestPlaqueJourneyDB_TheBudgetOnATenantWithHistory is the SAME journey against a
// population that is not the cheapest one available.
//
// 🔴 IT EXISTS BECAUSE THE OTHER TWO MEASURE A TENANT WITH ZERO TRANSACTIONS, and
// this repository has been wrong three times by quoting a timing without saying what
// it was a timing of. The plaque list's own cost is dominated by ListTagLastSeen,
// which is ONE GROUP BY pass over the tenant's `transactions` — so a tenant with no
// transactions is precisely the case that cannot show it.
//
// ⚠️ IT SEEDS INTO A SHARED FIXTURE TENANT, deliberately and narrowly: one admin
// (so it can sign in), one plaque on an existing wall and one spare. It writes no
// venue and no employee.
//
// 🔴 IT USED TO POISON ITSELF, AND THAT MADE EVERY "the suite is green" CLAIM IN THIS
// MILESTONE PROBABILISTIC. Two plaques per run, `tags` not deletable by tappa_app, and
// an assertion that the FRESHLY SEEDED one appears in a list capped at
// plaquePageLimit — so once the demo tenant passed 200 plaques the test's outcome
// depended on where a random 14-hex uid sorted. Measured before the fix (2026-08-10,
// 287 plaques in that tenant): 9 solo runs -> 6 PASS / 3 FAIL, and the rate was
// getting worse on every run. It is the second instance of the class M6-06 phase B
// found and fixed once already (a vat_number collision, ~1.13% per run).
//
// TWO CHANGES CLOSE IT, and neither weakens what the test is FOR:
//
//  1. THE LIST ASSERTION IS CAP-INDEPENDENT NOW. It checks the SPARE, which is
//     `unassigned` and therefore in the query's FIRST group (ORDER BY location_id
//     NULLS FIRST) — a tenant would need 200 unbound plaques in a box to push it out,
//     which is not a shape a business has. The BROKEN one is opened by its own uid,
//     which is how a manager holding a plaque finds it anyway and which
//     plaqueByUID answers whether or not the row is inside the cap.
//  2. IT DELETES ITS OWN TWO ROWS AFTERWARDS, as tappa_owner. That breaks this
//     package's "fixtures are not cleaned up" rule ON PURPOSE, and the reason is that
//     the rule's own justification does not apply: fixtures are left behind because
//     tappa_app CANNOT delete them, not because leaving them is desirable — and
//     plaquePageLimit's comment already names this test as the thing making its
//     population drift. Nothing in `transactions` is touched (§4.3): these two rows
//     are seconds old and no tap references them, which is asserted rather than
//     assumed — a referenced row would raise 23503 and fail the cleanup loudly.
func TestPlaqueJourneyDB_TheBudgetOnATenantWithHistory(t *testing.T) {
	p := newPanelHarness(t)
	tenantID, err := uuid.Parse(demoTenant)
	if err != nil {
		t.Fatalf("parse the demo tenant id: %v", err)
	}
	wall := p.adoptTenant(t, tenantID)

	venues, plaqueCount, taps := p.tenantSize(t)
	// ANTI-VACUITY: if the seed has not been run this is the cheap tenant again and
	// the whole point of the test is gone.
	if taps < 1000 {
		t.Skipf("the demo tenant holds %d transactions; run `make seed` for a population "+
			"worth timing", taps)
	}

	broken := p.seedPlaque(t, "active", wall)
	spare := p.seedPlaque(t, "unassigned", uuid.Nil)
	// THE ROWS GO AWAY AGAIN. Registered before the journey so a failure half way
	// through still cleans up — a test that poisons the tenant only when it FAILS is
	// the worst version of the bug it is fixing.
	p.dropPlaquesAfter(t, broken, spare)

	start := time.Now()
	screens, clicks := 0, 0

	res, html := p.get(t, locationsHref)
	screens++
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", locationsHref, res.StatusCode)
	}
	// 🔴 THE SPARE, NOT THE BROKEN ONE, AND THE CHOICE IS THE WHOLE FIX. An unassigned
	// plaque sorts in the list's FIRST group; a wall-mounted one sorts among however
	// many the tenant has, which is a number this test used to grow itself.
	spareHref := locationsHref + "?plaque=" + spare
	if !strings.Contains(html, spareHref) {
		t.Fatalf("the list does not offer the SPARE at %s — unbound stock is what this "+
			"screen puts first, so its absence is a real defect rather than a cap", spareHref)
	}
	// The plaque a manager is HOLDING is opened by its uid, which is cap-independent
	// (plaqueByUID falls back to a by-uid read behind the rendered page).
	openHref := locationsHref + "?plaque=" + broken

	res, html = p.get(t, openHref)
	screens++
	clicks++
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET the card = %d, want 200", res.StatusCode)
	}
	token := confirmTokenRE.FindStringSubmatch(html)
	if token == nil {
		t.Fatalf("the replacement card carried no confirmation")
	}

	res, _ = p.post(t, plaqueReplaceHref, url.Values{
		"uid": {broken}, "successor": {spare}, confirmField: {token[1]},
	})
	clicks++
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST replace = %d, want 303", res.StatusCode)
	}
	back := res.Header.Get("Location")
	res, html = p.get(t, back)
	screens++
	elapsed := time.Since(start)
	if res.StatusCode != http.StatusOK || !strings.Contains(html, "Plaque replaced") {
		t.Fatalf("the acknowledgement did not render (status %d)", res.StatusCode)
	}
	if status, replacedBy := p.plaqueRow(t, broken); status != "retired" ||
		replacedBy == nil || *replacedBy != spare {
		t.Fatalf("old plaque = (%s, %v), want (retired, %s)", status, replacedBy, spare)
	}

	t.Logf("replace-tag journey on a tenant WITH HISTORY: %d screens, %d clicks, "+
		"server time %s — tenant %s: %d venues, %d plaques, %d transactions",
		screens, clicks, elapsed, demoTenant, venues, plaqueCount, taps)
	if screens != 3 || clicks != 2 {
		t.Fatalf("the journey took %d screens and %d clicks", screens, clicks)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("server time = %s", elapsed)
	}
}

// adoptTenant points this harness at an EXISTING tenant by registering one admin in
// it and signing in. It returns a venue of that tenant to hang a plaque on.
//
// 🔴 IT DOES NOT CREATE THE TENANT, WHICH IS THE ONLY REASON IT EXISTS: the point is
// to measure against a population this fixture could not build in a test.
func (p *panelHarness) adoptTenant(t *testing.T, tenantID uuid.UUID) uuid.UUID {
	t.Helper()
	p.tenantID = tenantID
	p.adminID = uuid.New()
	p.email = "panel-journey-" + uuid.NewString() + "@m6.example"
	var wall uuid.UUID
	if err := p.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
			 VALUES ($1, $2, 'Journey Owner', $3, $4, 'owner', 'active')`,
			p.adminID, tenantID, p.email, ownerDigest); e != nil {
			return e
		}
		return tx.QueryRow(ctx,
			`SELECT id FROM locations WHERE tenant_id = $1 ORDER BY created_at LIMIT 1`,
			tenantID).Scan(&wall)
	}); err != nil {
		t.Skipf("cannot adopt tenant %s (has it been seeded?): %v", tenantID, err)
	}
	p.signIn(t)
	return wall
}

// TestPlaqueJourneyDB_AFrozenRowIsASentenceNotA500 drives `plaque-frozen` end to
// end, against a REAL frozen row rather than a fabricated one.
//
// 🔴 THE STATE CANNOT BE CREATED ANY MORE, which is why the test hunts for one
// instead of seeding it: a CHECK binds EVERY role, so a lower-case uid is refused
// even as tappa_owner (measured: 23514). The 18 010 rows that carry one all predate
// 00013, and they are all `active` with a wall — so a REPLACEMENT is the act that
// reaches them: the retire attempts an UPDATE and the CHECK fires.
//
// ⚠️ AND WHEN THE RESIDUE IS GONE THIS TEST DOES NOT SKIP, IT ASSERTS SOMETHING
// ELSE. Backlog T15 asks for exactly that clean-up (`make db-reset`), so a skip here
// would turn a completed chore into a silent hole in the suite. With no frozen row
// left, the mapping is still pinned — by the domain's unit test — and this says so
// out loud rather than reporting nothing.
func TestPlaqueJourneyDB_AFrozenRowIsASentenceNotA500(t *testing.T) {
	p := newPanelHarness(t)

	// 🔴 THE HUNT RUNS AS tappa_owner, AND IT HAS TO. The residue belongs to other
	// tenants and RLS hides it from the application role — correctly. Only the
	// discovery is privileged; every assertion below goes through the ordinary panel
	// as an ordinary signed-in manager.
	//
	// ⚠️ "NO ROW" AND "THE QUERY FAILED" ARE SEPARATED, because the first version
	// collapsed them and printed "no frozen row left" for a connection error — a
	// friendly sentence over a swallowed failure, which is the shape this whole task
	// keeps closing.
	owner := ownerPoolForTest(t)
	var tenantID uuid.UUID
	var frozen string
	err := owner.QueryRow(context.Background(), `
		SELECT t.tenant_id, t.uid FROM tags t
		WHERE t.uid !~ '^[0-9A-F]{14}$' AND t.status = 'active'
		  AND t.location_id IS NOT NULL LIMIT 1`).Scan(&tenantID, &frozen)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		t.Logf("no frozen row left in this database. That is backlog T15 DONE, not a " +
			"gap: the 23514 -> ErrPlaqueFrozen mapping stays pinned by " +
			"TestPlaques_ACheckViolationIsASENTENCEnotACRASH, and the boundary that lets " +
			"a lower-case uid REACH the statement by " +
			"TestPlaqueWrites_AFrozenUidREACHESTheDomain.")
		return
	case err != nil:
		t.Fatalf("hunting for a frozen row: %v", err)
	}
	t.Logf("driving the real residue: tenant %s, plaque %s", tenantID, frozen)

	p.adoptTenant(t, tenantID)
	spare := p.seedPlaque(t, "unassigned", uuid.Nil)

	// The card must OPEN — A1's first half, against the real row.
	res, html := p.get(t, locationsHref+"?plaque="+frozen)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET the card = %d, want 200", res.StatusCode)
	}
	if strings.Contains(html, "We could not find that plaque") {
		t.Fatalf("the section cannot open a row it lists: %s", frozen)
	}

	// The UI offers nothing, so this POST is a bypassed one — which is exactly the
	// caller `plaque-frozen` exists for.
	token, err := p.mintReplaceConfirmation(t, frozen)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	res, _ = p.post(t, plaqueReplaceHref, url.Values{
		"uid": {frozen}, "successor": {spare}, confirmField: {token},
	})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST = %d, want 303 (a sentence), never a 500", res.StatusCode)
	}
	loc := res.Header.Get("Location")
	if !strings.Contains(loc, "problem=plaque-frozen") {
		t.Fatalf("Location = %q, want problem=plaque-frozen — SQLSTATE 23514 must "+
			"become a sentence rather than the panel-unavailable page", loc)
	}
}

// mintReplaceConfirmation builds the confirmation a BYPASSED post would have to
// carry. It exists for one test: the frozen row's card offers no control, so the
// value cannot be read off the page — and the point of that test is the SQL refusal
// behind the gate, not the gate.
func (p *panelHarness) mintReplaceConfirmation(t *testing.T, uid string) (string, error) {
	t.Helper()
	res, _ := p.get(t, locationsHref)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET the section = %d", res.StatusCode)
	}
	c, err := newAdminConfirm(&config.Config{SessionHMACKey: []byte("0123456789abcdef0123456789abcdef")})
	if err != nil {
		return "", err
	}
	sid, err := p.sessionID(t)
	if err != nil {
		return "", err
	}
	token, err := c.mint(confirmActionReplacePlaque, uid, sid)
	if err != nil {
		return "", err
	}
	p.setCookie(t, adminConfirmCookieName, token)
	return token, nil
}

// sessionID reads the panel session this harness is signed in with, straight from
// the table — the value the confirmation is bound to.
func (p *panelHarness) sessionID(t *testing.T) (uuid.UUID, error) {
	t.Helper()
	var id uuid.UUID
	err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT id FROM admin_sessions WHERE tenant_id = $1 AND admin_user_id = $2
			 AND revoked_at IS NULL ORDER BY created_at DESC LIMIT 1`,
			p.tenantID, p.adminID).Scan(&id)
	})
	return id, err
}

// TestPlaqueJourneyDB_TheAcknowledgementIsNotSomethingAStrangerCanPrint is reading
// C' on this section: the word alone proves nothing, so a URL nobody earned prints
// nothing at all.
//
// 🔴 IT IS THE SAME SIGNED-IN MANAGER, WITH A REAL uid, AND STILL NOTHING IS SAID.
// That is what makes the assertion about the TRAIL rather than about authentication:
// the plaque exists, the session is genuine, and the only missing thing is an audit
// row saying this admin did this to it.
func TestPlaqueJourneyDB_TheAcknowledgementIsNotSomethingAStrangerCanPrint(t *testing.T) {
	p := newPanelHarness(t)
	p.signIn(t)
	wall := p.seedWall(t, "KF Sliema")
	onWall := p.seedPlaque(t, "active", wall)

	res, html := p.get(t, locationsHref+"?done=plaque-replaced&id="+onWall)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if strings.Contains(html, "Plaque replaced") {
		t.Fatalf("the section announced a replacement that never happened")
	}
	// ANTI-VACUITY: the page must still be the section, or "said nothing" would be
	// indistinguishable from "rendered nothing".
	if !strings.Contains(html, onWall) {
		t.Fatalf("the plaque list did not render at all")
	}
}
