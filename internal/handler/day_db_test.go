package handler

// A DAY AT KF ST JULIANS, produced by the decision engine (M5-09).
//
// WHAT THIS FILE IS. One test that drives the whole employee-facing product
// against real Postgres and the production router: an invitation, an activation,
// a mini-tour, practice taps, check-ins, check-outs, a queue of people on one
// plaque, a mobile-data tap, an evidence-less tap, a QR scan, a manager's manual
// entry, a deactivated account's attempt, a retired plaque and a Rusty Bar night
// shift that crosses midnight. Nothing below writes a `transactions` row by hand:
// every verdict, direction, trust score and practice flag on the day's records
// came out of internal/domain/tap through internal/domain/checkin, because a
// fixture that hand-writes those columns makes a broken engine look correct.
//
// WHY IT IS ONE TEST AND NOT TWELVE. The day is one causal chain: direction is a
// toggle against the person's last open check-in, the practice flag depends on
// whether the person has ever been recorded, and the debounce depends on when
// they last tapped. Split into independent tests those become twelve isolated
// fixtures, which is what the rest of this package already does well; what was
// missing — and what the card asks for — is the chain itself.
//
// THE THREE PHASES AND WHY THEY ARE MINUTES APART IN NOTHING BUT SECONDS. See
// seedDebounce in seedflow_db_test.go: ADR 0006 measures the debounce against a
// server-side age no client can move, so one person's consecutive taps must be
// separated by real time. The day therefore runs phase 0 (everyone's practice
// tap), waits, phase 1 (the check-ins), waits, phase 2 (the check-outs).
//
// 🔴 WHY EVERY WORKER HAS A PRACTICE ROW, which the M5-09 card's "~21 işlem" did
// not anticipate: §5 says the FIRST record after activation is a training tap,
// and on a freshly seeded database nobody has a record yet. So a first day really
// does start with one TRAINING row per person — that is the product, not a
// fixture artefact. The crew is ephemeral (fresh, run-stamped employee ids per
// run) because transactions are immutable (§4.3): a re-runnable test cannot
// inherit the last run's open check-in or its debounce predecessor. What that
// costs the demo database, and what driving the SEEDED crew was measured to do
// instead, is LIMITS L4.
//
// 🔴 ONE BIT OF EVERY NFC TAP IS STIPULATED, AND IT IS SAID OUT LOUD. The page is
// opened for real (GET /t with SUN parameters, real sun.Parse, real key unwrap,
// real preview) but the URL carries a WRONG chip MAC, so the server-minted
// context says CMACVerified=false. The simulation re-signs that same context with
// the bit flipped and posts it. Everything else on the record — channel, tag,
// counter, tenant, location, the atomic counter advance, the IP resolution, the
// verdict — is the server's. Minting a genuine MAC would mean a second
// implementation of the SDM construction outside internal/sun, which this repo
// has declined before (tap_db_test.go states why). So the honest reading is: this
// file proves everything about an NFC tap EXCEPT that the chip's signature
// verifies, and internal/sun proves that against its own vectors. (LIMITS L1 at
// the end of this file lists this alongside the three other gaps.)

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/test/fixtures"
)

// zeroCMAC is the deliberately wrong chip MAC every NFC URL below carries. See
// the file header: the page is opened with it, and the one bit it produces is the
// only thing the simulation overrides.
const zeroCMAC = "0000000000000000"

// dayWait is how long a phase waits before the same person may tap again.
//
// It is a VARIABLE so the wait itself can be mutated away and the consequence
// measured rather than asserted. Measured, with it set to 0: the day fails on the
// FIRST check-in of phase 1 — "Maria Borg: check-in = ignored/<nil>" — because
// her practice tap was seconds earlier. The same collapse is counted without
// aborting by TestSeedDB_WithoutTheWaitTheDayCollapsesIntoIgnoredRows (5 of 15
// records counted, 10 ignored); applied to this script it would swallow 19 of the
// 31 rows, every check-out among them.
var dayWait = seedDebounceWait

// --- the tap cycle -----------------------------------------------------------

// tapNFC drives one NFC tap end to end: open the plaque page from fromIP, take
// the context the SERVER minted, flip the one bit described in the file header,
// and post it back from the same address.
func (f *seedFlow) tapNFC(t *testing.T, p *phone, uid, fromIP string, form url.Values) (int, string) {
	t.Helper()
	ctr := f.tagCounter(t, uid) + 1
	status, page := f.openPage(t, p, nfcURL(uid, ctr, zeroCMAC), fromIP)
	if status != http.StatusOK {
		t.Fatalf("%s: GET /t status = %d, want 200", p.name, status)
	}
	signed := contextFieldOf(t, page)

	sid := f.sessionID(t, p)
	decoded, err := f.tap.contexts.parse(signed, sid)
	if err != nil {
		t.Fatalf("%s: the page's own context did not parse: %v", p.name, err)
	}
	if decoded.Ctr != ctr {
		t.Fatalf("%s: the page carried ctr %d, want %d — the counter is the server's, not ours", p.name, decoded.Ctr, ctr)
	}
	decoded.CMACVerified = true
	resigned, err := f.tap.contexts.mint(decoded, sid)
	if err != nil {
		t.Fatalf("%s: re-sign: %v", p.name, err)
	}
	return f.postCheckin(t, p, resigned, fromIP, form)
}

// tapQR drives a QR scan: the same plaque URL with NO counter and NO MAC, and the
// page's own context posted back untouched — nothing to stipulate on this channel.
func (f *seedFlow) tapQR(t *testing.T, p *phone, uid, fromIP string, form url.Values) (int, string) {
	t.Helper()
	status, page := f.openPage(t, p, seedQRURL(uid), fromIP)
	if status != http.StatusOK {
		t.Fatalf("%s: GET /t (qr) status = %d, want 200", p.name, status)
	}
	return f.postCheckin(t, p, contextFieldOf(t, page), fromIP, form)
}

// atFix is the form a phone sends when the browser gave it a coordinate.
func atFix(lat, lng float64) url.Values {
	return url.Values{"lat": {fmt.Sprintf("%.6f", lat)}, "lng": {fmt.Sprintf("%.6f", lng)}}
}

// declaring adds a client-stated tap time to a form (the offline-queue field).
func declaring(v url.Values, when time.Time) url.Values {
	out := url.Values{}
	for k, vs := range v {
		out[k] = vs
	}
	out.Set("occurred_at", when.Format(time.RFC3339))
	return out
}

// --- activation --------------------------------------------------------------

// activate walks a brand-new employee through invitation, consent and the mini
// tour on ONE cookie jar, and leaves the session cookie in it. This is the first
// third of the card's chain.
func (f *seedFlow) activate(t *testing.T, p *phone) {
	t.Helper()
	ch := &linkChannel{}
	if _, err := f.invites.IssueAndDeliver(context.Background(), invite.IssueParams{
		TenantID: f.tenantID, EmployeeID: p.id,
	}, ch); err != nil {
		t.Fatalf("IssueAndDeliver for %s: %v", p.name, err)
	}
	link, err := url.Parse(ch.url)
	if err != nil {
		t.Fatalf("activation url: %v", err)
	}
	code := link.Query().Get("code")
	if code == "" {
		t.Fatal("the delivered link carries no code")
	}

	status, page := f.openPage(t, p, "/activate?code="+url.QueryEscape(code), seedOffSiteAddr)
	if status != http.StatusOK {
		t.Fatalf("GET /activate status = %d, want 200", status)
	}
	if strings.Contains(page, code) {
		t.Fatal("the activation page carries the raw code")
	}
	token := formToken(t, page)

	req, err := http.NewRequest(http.MethodPost, f.server.URL+"/api/activate",
		strings.NewReader(url.Values{"consent": {"yes"}, "csrf": {token}}.Encode()))
	if err != nil {
		t.Fatalf("build activate POST: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client.Do(req)
	if err != nil {
		t.Fatalf("POST /api/activate: %v", err)
	}
	tour := body(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("activation status = %d, want 200", resp.StatusCode)
	}
	if resp.Request.URL.Path != "/activate/tour" {
		t.Errorf("a first activation must land on the mini tour, landed on %s", resp.Request.URL.Path)
	}
	if !strings.Contains(tour, "Tap the plaque") {
		t.Error("the first slide of the tour did not render")
	}
	// Walk out of the tour the way a phone does.
	if code, _ := f.openPage(t, p, "/activate/done", seedOffSiteAddr); code != http.StatusOK {
		t.Fatalf("GET /activate/done status = %d, want 200", code)
	}

	var got string
	for _, c := range p.client.Jar.Cookies(f.baseURL) {
		if c.Name == session.CookieName {
			got = c.Value
		}
	}
	if got == "" {
		t.Fatalf("%s walked out of activation without a session cookie", p.name)
	}
}

// ---------------------------------------------------------------------------

// TestSeedDB_ADayAtKFStJulians is the card's acceptance run.
//
// 🔴 AN INNER-LOOP CONVENIENCE — NOT A COVERAGE DECISION. `make test` (and
// therefore CI, which runs `make check` -> `make test`) passes NO -short flag, so
// this day runs there exactly as before. What -short buys is a developer's
// edit-run cycle.
//
// ⚠️ THIS COMMENT USED TO OPEN "THE ONLY testing.Short() SKIP IN THIS REPOSITORY"
// AND TO CARRY TWO STALE NUMBERS. Both were measured false. There are now THREE
// skips (`go test -race -count=1 -short -v ./... | grep -c -- "--- SKIP:"` -> 3):
// this one, plus the two bcrypt wall-clock SAMPLES M6-01 phase B added
// (TestAuthenticate_TimingIsFlat, TestPanelE2E_TimingIsFlatOverHTTP). The Makefile
// is the canonical list and names all three; it points HERE as skip #1, so the two
// files were pointing at each other while saying different things.
//
// ⚠️ THE TIMINGS LIVE IN THE MAKEFILE, NOT HERE — deliberately, and the reason is
// this comment's own history. Round 10 corrected two stale numbers here; round 12
// corrected the same numbers in the Makefile and left THESE behind, so the two
// files pointed at each other and disagreed AGAIN — the exact defect round 10 had
// been opened to fix, reproduced by its own fix. A number that lives in two places
// drifts; this one now lives in one.
//
// `make test-short` is the target this test's skip exists for. See the Makefile's
// test-short block for the measured band, the load condition it was measured under,
// and all three skips by name. Most of the difference `-short` buys is still the ~62 s this test
// spends asleep, because ADR 0006 measures the debounce on the SERVER clock
// (seedDebounce, seedflow_db_test.go).
//
// The skip is LOUD on purpose. This repo has a written lesson about the other
// kind: M5-06 found two tests that CI skipped in silence for want of `make css`,
// and recorded that a skipped test is not a passing one. So the message below
// names the test, says how much is not being run, and says where it still is.
func TestSeedDB_ADayAtKFStJulians(t *testing.T) {
	if testing.Short() {
		t.Skip("SKIPPED BY -short: TestSeedDB_ADayAtKFStJulians, the whole simulated day " +
			"(31 records, ~63 s, of which ~62 s is ADR 0006 debounce waiting) did NOT run. " +
			"Nothing here is covered by another test. `make test` and CI still run it; " +
			"a skipped test is not a passing one (M5-06). Use `make test` before you commit.")
	}
	f := newSeedFlow(t)
	stJulians := f.venue(t, fixtures.LocKFStJulians)
	rustyBar := f.venue(t, fixtures.LocKFRustyBar)

	onSite := stJulians.OnSiteIP
	venueGPS := atFix(stJulians.Lat, stJulians.Lng)

	// --- the crew ------------------------------------------------------------
	// Nine phones at St Julians plus one bartender at Rusty Bar. Names are the
	// seeded crew's (skill tappa-seed: no invented companies or people); the ROWS
	// are fresh and RUN-STAMPED in the database (newSeedFlow's runStamp), for the
	// reasons measured in LIMITS L4. The stamp is not in p.name, so the failure
	// messages below stay readable.
	maria := f.hire(t, "Maria Borg", stJulians.ID, "active")
	joseph := f.hire(t, "Joseph Camilleri", stJulians.ID, "active")
	antoine := f.hire(t, "Antoine Vella", stJulians.ID, "active")
	rita := f.hire(t, "Rita Zammit", stJulians.ID, "active")
	ahmed := f.hire(t, "Ahmed Hassan", stJulians.ID, "active")
	elena := f.hire(t, "Elena Petrova", stJulians.ID, "active")
	marco := f.hire(t, "Marco Bugeja", stJulians.ID, "active")
	ivan := f.hire(t, "Ivan Petrov", rustyBar.ID, "active")

	// Grace has never been activated: no session, no activated_at. She is the
	// card's chain — invitation -> activation -> practice -> in -> out.
	grace := f.insertEmployee(t, "Grace Attard", stJulians.ID, "invited", false)

	// Nadia has no phone at all. She is never activated, so she never taps; her
	// hours arrive as a manager's manual entry. (activated_at NULL also keeps §5's
	// practice rule from firing on a record she did not make.)
	nadia := f.insertEmployee(t, "Nadia Farrugia", stJulians.ID, "invited", false)

	// Paul was deactivated last week and his phone still holds a live session —
	// which is precisely the §5 row 4 case: the session resolves, the account does
	// not, and the attempt must be RECORDED.
	paul := f.hire(t, "Paul Spiteri", stJulians.ID, "deactivated")

	// The simulated crew must be TELLABLE APART from the seeded roster: LIMITS L4.
	// Checked here, before anything is slow, so a regression fails in seconds.
	f.assertTellableApart(t, maria, "Maria Borg")
	f.assertTellableApart(t, ivan, "Ivan Petrov")

	plaque := fixtures.TagKFStJulians
	securityBefore := f.auditRows(t, "tap.security_alert")

	// =========================================================================
	// PHASE 0 — activation, and everybody's first (TRAINING) tap.
	// =========================================================================
	f.activate(t, grace)

	practice := []*phone{maria, joseph, antoine, rita, ahmed, elena, marco, grace}
	for _, p := range practice {
		if code, _ := f.tapNFC(t, p, plaque, onSite, venueGPS); code != http.StatusOK {
			t.Fatalf("%s: practice tap status = %d, want 200", p.name, code)
		}
		rec := f.lastRow(t, p.id)
		if !rec.Practice {
			t.Fatalf("%s: the first record after activation must be a TRAINING tap (§5), practice = false", p.name)
		}
		if rec.Verdict != "ok" || rec.Type == nil || *rec.Type != "in" {
			t.Fatalf("%s: practice tap = %s/%v, want ok/in", p.name, rec.Verdict, show(rec.Type))
		}
	}

	// Ivan taps his plaque for the first time like everybody else — NO declared
	// time, so the training row is stamped at server-now, which is LATER than both
	// halves of the night shift he is about to work.
	//
	// 🔴 THIS LINE USED TO CARRY declaring(rbGPS, night.practice), AND THAT WAS A
	// WORKAROUND FOR A PRODUCT DEFECT (M5-11, ADR 0008). Pushing the training row
	// down to 17:50 kept it below the 18:05 check-in, because a practice row that
	// sorted newest hid the real open check-in and turned the 02:10 checkout into a
	// second `in`. The query now excludes practice rows, so the fixture no longer
	// has to arrange the history for the engine — and this ordering (training tap
	// newest, real shift beneath it) is exactly the shape that used to break.
	night := rustyBarNight(t, rustyBar.Timezone)
	rbGPS := atFix(rustyBar.Lat, rustyBar.Lng)
	if code, _ := f.tapNFC(t, ivan, fixtures.TagKFRustyBar, rustyBar.OnSiteIP, rbGPS); code != http.StatusOK {
		t.Fatalf("Ivan: practice tap status = %d, want 200", code)
	}
	if rec := f.lastRow(t, ivan.id); !rec.Practice {
		t.Fatal("Ivan's first record is not a TRAINING tap")
	}

	// §5 ROW 4 — a deactivated account tries. Recorded, refused, and a security
	// alert raised for the managers.
	if code, _ := f.tapNFC(t, paul, plaque, onSite, venueGPS); code != http.StatusOK {
		t.Fatalf("Paul: status = %d, want 200 (a reject is a rendered outcome)", code)
	}
	paulRec := f.lastRow(t, paul.id)
	if paulRec.Verdict != "reject" || paulRec.MatchedSid == nil || *paulRec.MatchedSid != "sys:employee-deactivated" {
		t.Fatalf("Paul: verdict/sid = %s/%q, want reject/sys:employee-deactivated",
			paulRec.Verdict, deref(paulRec.MatchedSid))
	}
	if got := f.auditRows(t, "tap.security_alert") - securityBefore; got != 1 {
		t.Fatalf("a deactivated account's attempt raised %d security alerts, want 1", got)
	}

	// =========================================================================
	// The wait. See seedDebounce: this is the price of ADR 0006 being real.
	// =========================================================================
	time.Sleep(dayWait)

	// =========================================================================
	// PHASE 1 — the shift starts.
	// =========================================================================

	// THE QUEUE (§5): three DIFFERENT people on the SAME plaque, back to back,
	// within a second of each other. All three must be recorded — the debounce is
	// per person, never per tag, and this is the assertion that says so.
	queue := []*phone{maria, joseph, antoine}
	for _, p := range queue {
		if code, _ := f.tapNFC(t, p, plaque, onSite, venueGPS); code != http.StatusOK {
			t.Fatalf("%s: check-in status = %d", p.name, code)
		}
		rec := f.lastRow(t, p.id)
		if rec.Verdict != "ok" || rec.Type == nil || *rec.Type != "in" || rec.Practice {
			t.Fatalf("%s: check-in = %s/%v practice=%v, want ok/in/false — a queue must not debounce",
				p.name, rec.Verdict, show(rec.Type), rec.Practice)
		}
		if rec.Trust == nil || *rec.Trust != 100 {
			t.Fatalf("%s: trust = %v, want 100 (20 base + 50 IP + 30 GPS)", p.name, show(rec.Trust))
		}
		if rec.MatchedSid == nil || *rec.MatchedSid != policy.SidIPOrGPSOK {
			t.Fatalf("%s: matched_sid = %q, want %s", p.name, deref(rec.MatchedSid), policy.SidIPOrGPSOK)
		}
		if rec.Channel != "nfc" || rec.SunValid == nil || !*rec.SunValid {
			t.Fatalf("%s: channel/sun_valid = %s/%v, want nfc/true", p.name, rec.Channel, show(rec.SunValid))
		}
	}
	// Joseph is the crew's "late" case and there is NOTHING HERE TO ASSERT ABOUT
	// IT. An earlier draft checked that his record's occurred_at was after St
	// Julians' 10:00 shift start; that assertion was structurally incapable of
	// failing (a live tap is stamped at server-now, the comparison time is by
	// construction at least twenty minutes older) and it was blind to the engine
	// besides. MEASURED, with tap.lateness mutated to `return nil` — a total
	// lateness failure — the day sailed past it and only died 110 lines later, at
	// the manual entry's MinutesLate check. It is removed rather than reworded:
	// LIMITS L2 records that lateness has no HTTP-side evidence at all.

	// THE DOUBLE TAP (§5 row 5). Antoine's phone did not show the confirmation
	// fast enough, so he tapped again. The chip produced the NEXT counter value —
	// this is a real second touch, not a replay — and it is still `ignored`.
	if code, _ := f.tapNFC(t, antoine, plaque, onSite, venueGPS); code != http.StatusOK {
		t.Fatalf("Antoine: second tap status = %d", code)
	}
	dbl := f.lastRow(t, antoine.id)
	if dbl.Verdict != "ignored" || dbl.MatchedSid == nil || *dbl.MatchedSid != "sys:person-debounce" {
		t.Fatalf("the double tap = %s/%q, want ignored/sys:person-debounce", dbl.Verdict, deref(dbl.MatchedSid))
	}
	if dbl.Type != nil {
		t.Fatalf("an ignored tap must carry no direction, got %q", *dbl.Type)
	}

	// An ordinary check-in for Rita, so the crew is not all edge cases.
	if code, _ := f.tapNFC(t, rita, plaque, onSite, venueGPS); code != http.StatusOK {
		t.Fatalf("Rita: check-in status = %d", code)
	}

	// MOBILE DATA (§5 row 6, GPS branch): Ahmed's phone is on 4G, so the venue's
	// address does not appear — but he is standing at the door and the fix proves
	// it. `ok`, at a trust score that says the network proof is missing.
	if code, _ := f.tapNFC(t, ahmed, plaque, seedOffSiteAddr, venueGPS); code != http.StatusOK {
		t.Fatalf("Ahmed: check-in status = %d", code)
	}
	gpsOnly := f.lastRow(t, ahmed.id)
	if gpsOnly.Verdict != "ok" || gpsOnly.Trust == nil || *gpsOnly.Trust != 50 {
		t.Fatalf("mobile-data check-in = %s trust %v, want ok/50 (20 base + 30 GPS)", gpsOnly.Verdict, show(gpsOnly.Trust))
	}
	if gpsOnly.MatchedSid == nil || *gpsOnly.MatchedSid != policy.SidGPSOnlyAllow {
		t.Fatalf("matched_sid = %q, want %s", deref(gpsOnly.MatchedSid), policy.SidGPSOnlyAllow)
	}
	if gpsOnly.Note == nil || !strings.Contains(strings.ToLower(*gpsOnly.Note), "gps") {
		t.Fatalf("a GPS-only tap must say so on the record, note = %q", deref(gpsOnly.Note))
	}
	if gpsOnly.IPMatch == nil || *gpsOnly.IPMatch {
		t.Fatal("ip_match is true for a tap from outside the venue's range")
	}

	// NO EVIDENCE AT ALL (§5 row 7): off the venue network and the browser refused
	// the location prompt. The record is written and QUEUED for a manager — never
	// dropped, never silently approved (§4.6).
	if code, _ := f.tapNFC(t, elena, plaque, seedOffSiteAddr, nil); code != http.StatusOK {
		t.Fatalf("Elena: check-in status = %d", code)
	}
	flagged := f.lastRow(t, elena.id)
	if flagged.Verdict != "flag" || !flagged.Queued {
		t.Fatalf("an evidence-less tap = %s queued=%v, want flag/true", flagged.Verdict, flagged.Queued)
	}
	if flagged.Trust == nil || *flagged.Trust != 20 {
		t.Fatalf("trust = %v, want 20 (base only)", show(flagged.Trust))
	}
	if flagged.Type == nil || *flagged.Type != "in" {
		t.Fatalf("a flagged tap still joins the in/out chain, type = %v", show(flagged.Type))
	}
	if flagged.MatchedSid == nil || *flagged.MatchedSid != policy.SidNoEvidenceReview {
		t.Fatalf("matched_sid = %q, want %s", deref(flagged.MatchedSid), policy.SidNoEvidenceReview)
	}

	// THE QR CHANNEL: Marco's phone has no NFC, so he scans the printed code. No
	// counter, no chip MAC — and therefore the venue address is mandatory (Q15).
	if code, _ := f.tapQR(t, marco, plaque, onSite, nil); code != http.StatusOK {
		t.Fatalf("Marco: QR check-in status = %d", code)
	}
	qr := f.lastRow(t, marco.id)
	if qr.Channel != "qr" || qr.Verdict != "ok" {
		t.Fatalf("QR check-in = channel %s / %s, want qr/ok", qr.Channel, qr.Verdict)
	}
	if qr.SunValid == nil || *qr.SunValid || qr.Ctr != nil {
		t.Fatalf("a QR record carries no proof of moment: sun_valid=%v ctr=%v", show(qr.SunValid), show(qr.Ctr))
	}

	// GRACE'S FIRST REAL CHECK-IN — the third step of the card's chain. Her
	// TRAINING row must NOT have held the chain open, or this would be an `out`.
	if code, _ := f.tapNFC(t, grace, plaque, onSite, venueGPS); code != http.StatusOK {
		t.Fatalf("Grace: check-in status = %d", code)
	}
	graceIn := f.lastRow(t, grace.id)
	if graceIn.Practice {
		t.Fatal("Grace's SECOND record is still marked TRAINING")
	}
	if graceIn.Type == nil || *graceIn.Type != "in" {
		t.Fatalf("Grace's first real tap = %v, want in — a practice row must not hold the chain open", show(graceIn.Type))
	}

	// THE MANAGER'S MANUAL ENTRY. Nadia has no phone, so the owner types her shift
	// in. There is no HTTP surface for this today (the manual-entry screen is
	// M6-04), so it goes through the SAME service the router uses — engine output,
	// not a hand-written row.
	//
	// It is also the ONE place lateness is observable AT ALL: tap.Decision.
	// MinutesLate is computed on every check-in and then written NOWHERE — not a
	// column, not a policy_context key, not the confirmation screen — so an HTTP
	// tap cannot show it. Here the caller holds the Decision itself. The day
	// therefore has NO lateness evidence on the HTTP side; LIMITS L2 says so, and
	// says what was measured when this file tried to pretend otherwise.
	shiftStart := shiftStartInPast(t, stJulians.Timezone, 10, 0)
	manual, err := f.checkins.Record(context.Background(), checkin.Request{
		SessionTenantID: f.tenantID,
		EmployeeID:      nadia.id,
		TagUID:          plaque,
		TagTenantID:     f.tenantID,
		Channel:         tap.ChannelManual,
		EnteredBy:       ptrUUID(fixtures.AdminKFOwner),
		OccurredAt:      ptrTime(shiftStart.Add(17 * time.Minute)),
	})
	if err != nil {
		t.Fatalf("manual entry: %v", err)
	}
	if manual.Decision.MinutesLate == nil {
		t.Fatal("lateness was not computed for a check-in against a venue with a shift")
	}
	// 🔴 A FINDING, PINNED RATHER THAN FIXED. The entry above declares 10:17 against
	// a 10:00 shift, so the skill's scenario table expects "late 17m". The engine
	// answers something else entirely, because tap.lateness measures in.Now — the
	// SERVER clock — and not the record's occurred_at (internal/domain/tap's
	// lateness()). For a live tap the two coincide, which is why no existing test
	// sees it: lateness_test.go never sets OccurredAt at all. For a manager's
	// back-dated entry, or an offline-queued tap (M9-01), it reports how late the
	// RECORDING was.
	//
	// It is not fixed here for two reasons: it changes decision-engine semantics
	// (M4-05, §5), which is not this task's to decide; and it has no production
	// consequence yet, because the value is persisted nowhere. It becomes real at
	// M6-07 (reports). The assertion below pins TODAY'S behaviour so a fix breaks
	// this test loudly instead of quietly disagreeing with this comment.
	wantLate := minutesSinceShiftStartOnServerClock(t, stJulians.Timezone, 10, 0)
	if got := *manual.Decision.MinutesLate; abs(got-wantLate) > 2 {
		t.Fatalf("MinutesLate = %d, and the server-clock reading is %d. Either the clock moved "+
			"under the test, or lateness now follows occurred_at — if it is the latter, this "+
			"assertion becomes `want 17` and the comment above comes out.", got, wantLate)
	}
	manualRec := f.lastRow(t, nadia.id)
	if manualRec.Channel != "manual" {
		t.Fatalf("channel = %q, want manual", manualRec.Channel)
	}
	if manualRec.SunValid != nil {
		t.Fatalf("sun_valid = %v, want NULL: SUN is meaningless for a record with no chip", show(manualRec.SunValid))
	}
	if manualRec.Type == nil || *manualRec.Type != "in" {
		t.Fatalf("the manual entry = %v, want in", show(manualRec.Type))
	}
	f.assertEnteredBy(t, manualRec.ID, fixtures.AdminKFOwner)
	// MEASURED — and the first version of this comment guessed WRONG, which is why
	// the sid is now asserted rather than described. The entry is back-dated by
	// hours, so base:queued-window (120 s) looked like the obvious reason; on the
	// live run the row is flagged by base:no-evidence-review instead. A manual
	// entry arrives with no address and no fix, so §5 row 7 matches first and the
	// stale-time rule never gets asked. The verdict-only assertion could not tell
	// the two apart; asserting matched_sid means the engine says so itself.
	//
	// Either way the product consequence is the same and belongs to M6-04: the
	// manager's own record lands in the manager's own review queue.
	if manualRec.Verdict != "flag" {
		t.Fatalf("a back-dated manual entry came back %q; this file records that it is `flag`. "+
			"If that changed, the reasoning in M6-04 changes with it.", manualRec.Verdict)
	}
	if manualRec.MatchedSid == nil || *manualRec.MatchedSid != policy.SidNoEvidenceReview {
		t.Fatalf("the manual entry was flagged by %q, measured %s: a manual row carries no IP and "+
			"no fix, so §5 row 7 is what should match", deref(manualRec.MatchedSid), policy.SidNoEvidenceReview)
	}

	// THE NIGHT SHIFT, first half: Ivan starts at 18:05 the previous evening.
	if code, _ := f.tapNFC(t, ivan, fixtures.TagKFRustyBar, rustyBar.OnSiteIP,
		declaring(rbGPS, night.in)); code != http.StatusOK {
		t.Fatalf("Ivan: 18:05 check-in status = %d", code)
	}
	ivanIn := f.lastRow(t, ivan.id)
	if ivanIn.Type == nil || *ivanIn.Type != "in" {
		t.Fatalf("Ivan's 18:05 tap = %v, want in", show(ivanIn.Type))
	}

	// =========================================================================
	time.Sleep(dayWait)
	// =========================================================================
	// PHASE 2 — the shift ends.
	// =========================================================================

	for _, p := range []*phone{maria, joseph, antoine, rita, ahmed, elena, grace} {
		if code, _ := f.tapNFC(t, p, plaque, onSite, venueGPS); code != http.StatusOK {
			t.Fatalf("%s: check-out status = %d", p.name, code)
		}
		rec := f.lastRow(t, p.id)
		if rec.Type == nil || *rec.Type != "out" {
			t.Fatalf("%s: check-out = %v, want out — direction toggles against the last OPEN check-in", p.name, show(rec.Type))
		}
		if rec.Practice {
			t.Fatalf("%s: a check-out can never be a TRAINING tap", p.name)
		}
	}
	if code, _ := f.tapQR(t, marco, plaque, onSite, nil); code != http.StatusOK {
		t.Fatalf("Marco: QR check-out status = %d", code)
	}
	if rec := f.lastRow(t, marco.id); rec.Type == nil || *rec.Type != "out" || rec.Channel != "qr" {
		t.Fatalf("Marco's QR check-out = %v on channel %s, want out/qr", show(rec.Type), rec.Channel)
	}

	// 🔴 THE NIGHT SHIFT'S SECOND HALF, and the regression this whole fixture
	// exists for: 02:10 is a DIFFERENT CALENDAR DAY from the 18:05 check-in. A
	// direction resolved per calendar day would call this an `in` and Ivan's night
	// would never close.
	if code, _ := f.tapNFC(t, ivan, fixtures.TagKFRustyBar, rustyBar.OnSiteIP,
		declaring(rbGPS, night.out)); code != http.StatusOK {
		t.Fatalf("Ivan: 02:10 check-out status = %d", code)
	}
	ivanOut := f.lastRow(t, ivan.id)
	if ivanOut.Type == nil || *ivanOut.Type != "out" {
		t.Fatalf("Ivan's 02:10 tap = %v, want out — the overnight shift is not a calendar day", show(ivanOut.Type))
	}
	if !ivanOut.OccurredAt.After(ivanIn.OccurredAt) {
		t.Fatal("the checkout is not after the check-in it closes")
	}
	if ivanOut.OccurredAt.In(time.UTC).YearDay() == ivanIn.OccurredAt.In(time.UTC).YearDay() {
		t.Fatal("the night shift did not cross a day boundary, so it proves nothing about the toggle")
	}
	f.assertNoOpenCheckIn(t, ivan.id)

	// §5 ROW 1 — the old plaque is still screwed to the wall next to the new one.
	// A retired plaque is refused before any cryptography and its counter never
	// moves.
	retiredCtr := f.tagCounter(t, fixtures.TagKFStJuliansOld)
	if code, _ := f.tapNFC(t, maria, fixtures.TagKFStJuliansOld, onSite, venueGPS); code != http.StatusOK {
		t.Fatalf("the retired plaque answered %d, want 200 with a recorded reject", code)
	}
	dead := f.lastRow(t, maria.id)
	if dead.Verdict != "reject" || dead.MatchedSid == nil || *dead.MatchedSid != "sys:tag-not-active" {
		t.Fatalf("the retired plaque = %s/%q, want reject/sys:tag-not-active", dead.Verdict, deref(dead.MatchedSid))
	}
	if got := f.tagCounter(t, fixtures.TagKFStJuliansOld); got != retiredCtr {
		t.Fatalf("the retired plaque's counter moved %d -> %d", retiredCtr, got)
	}

	// --- the day, counted ----------------------------------------------------
	// Nadia's entry is the FORGOTTEN CHECKOUT (§5): the manager typed her in and
	// nobody typed her out, so her record stays open. The system does NOT invent a
	// checkout; the open row is the anomaly a manager has to resolve.
	f.assertOpenCheckIn(t, nadia.id)

	crew := map[string]*phone{
		"Maria": maria, "Joseph": joseph, "Antoine": antoine, "Rita": rita,
		"Ahmed": ahmed, "Elena": elena, "Marco": marco, "Grace": grace,
		"Nadia": nadia, "Ivan": ivan,
	}
	total := 0
	for _, p := range crew {
		total += f.rowsFor(t, p.id)
	}
	total += f.rowsFor(t, paul.id)
	if len(crew) != 10 {
		t.Fatalf("the day has %d workers, the card says 10", len(crew))
	}
	// 31, and the breakdown is the point of asserting a total at all — if the
	// number moves, a scenario was added or silently lost:
	//
	//	phase 0  9 training taps + 1 refused attempt (deactivated)      = 10
	//	phase 1  6 NFC check-ins + 1 ignored double tap + 1 QR
	//	         + 1 manual entry + 1 Rusty Bar 18:05 + 1 Grace          = 11
	//	phase 2  7 NFC check-outs + 1 QR + 1 Rusty Bar 02:10
	//	         + 1 refused retired plaque                              = 10
	//
	// It is higher than the card's "~21" for a reason the card could not have
	// known: on a fresh database §5's practice rule gives every one of the nine
	// tapping workers a TRAINING row before their first real check-in.
	if total != 31 {
		t.Fatalf("the day produced %d records, want 31", total)
	}
	// Paul is NOT in `crew` — the day has ten workers, and he no longer works here.
	// His refused attempt IS a record though (§4.6), so his rows are added to the
	// total on their own line above. The log below says exactly that split.
	t.Logf("KF St Julians: %d workers plus one deactivated account, %d records in total",
		len(crew), total)
}

// TestSeedDB_WithoutTheWaitTheDayCollapsesIntoIgnoredRows is why the day above
// spends a minute sleeping, MEASURED rather than argued.
//
// It replays the day's shape with NO wait at all — five people, three taps each,
// fired as fast as HTTP allows — and counts what comes back. Every person's
// SECOND and THIRD tap is `ignored` (sys:person-debounce), because ADR 0006
// measures the gap against clock_timestamp() - created_at and nothing a client
// sends can move that. Ten of the fifteen records are lost to it here; applied to
// the full day the same arithmetic swallows 19 of 31, every check-out included.
//
// It is also the POSITIVE CONTROL for the sleeps: if somebody removes them, this
// test still passes and the day test starts failing, which is the right way round
// — the mechanism is pinned separately from the workaround for it.
func TestSeedDB_WithoutTheWaitTheDayCollapsesIntoIgnoredRows(t *testing.T) {
	f := newSeedFlow(t)
	venue := f.venue(t, fixtures.LocKFStJulians)
	fix := atFix(venue.Lat, venue.Lng)

	const people, tapsEach = 5, 3
	crew := make([]*phone, 0, people)
	for i := 0; i < people; i++ {
		crew = append(crew, f.hire(t, fmt.Sprintf("Debounce Probe %d", i+1), venue.ID, "active"))
	}

	counted, ignored := 0, 0
	for tapNo := 0; tapNo < tapsEach; tapNo++ {
		for _, p := range crew {
			if code, _ := f.tapNFC(t, p, fixtures.TagKFStJulians, venue.OnSiteIP, fix); code != http.StatusOK {
				t.Fatalf("%s tap %d: status = %d", p.name, tapNo+1, code)
			}
			switch rec := f.lastRow(t, p.id); rec.Verdict {
			case "ignored":
				if rec.MatchedSid == nil || *rec.MatchedSid != "sys:person-debounce" {
					t.Fatalf("%s tap %d was ignored by %q, expected sys:person-debounce",
						p.name, tapNo+1, deref(rec.MatchedSid))
				}
				ignored++
			case "ok":
				counted++
			default:
				t.Fatalf("%s tap %d = %q, expected ok or ignored", p.name, tapNo+1, rec.Verdict)
			}
		}
	}

	// One counted tap per person (the first), everything else swallowed. §4.6 is
	// untouched: every request still produced a ROW, it just does not count.
	if counted != people || ignored != people*(tapsEach-1) {
		t.Fatalf("counted=%d ignored=%d, want counted=%d ignored=%d — if this changed, "+
			"either the debounce or the way the day is timed no longer works the way ADR 0006 measured",
			counted, ignored, people, people*(tapsEach-1))
	}
	for _, p := range crew {
		if got := f.rowsFor(t, p.id); got != tapsEach {
			t.Fatalf("%s produced %d rows for %d taps: a debounced tap is RECORDED, never dropped (§4.6)",
				p.name, got, tapsEach)
		}
	}
	t.Logf("no wait: %d of %d records counted, %d ignored by sys:person-debounce",
		counted, people*tapsEach, ignored)
}

// --- scenario helpers --------------------------------------------------------

// nightTimes are the two declared instants of the Rusty Bar shift.
//
// There used to be a third, `practice`, and it was a WORKAROUND rather than part of
// the scenario: it declared Ivan's training tap at 17:50 so it sorted below the
// 18:05 check-in and could not hide it (M5-11, ADR 0008). With the defect fixed the
// training tap is an ordinary undeclared one, and the field is gone rather than left
// unused — an unused workaround is an invitation to reintroduce it.
type nightTimes struct{ in, out time.Time }

// rustyBarNight picks the most recent night whose 02:10 checkout is already in the
// past, so the fixture never declares a FUTURE time (sys:occurred-at-bound denies
// those outright) and never falls outside the 72 h tolerance either.
func rustyBarNight(t *testing.T, timezone string) nightTimes {
	t.Helper()
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		t.Fatalf("tenant timezone %q: %v", timezone, err)
	}
	now := time.Now().In(loc)
	day := now.AddDate(0, 0, -1) // the shift STARTED the previous evening
	out := time.Date(day.Year(), day.Month(), day.Day()+1, 2, 10, 0, 0, loc)
	if !out.Before(now) {
		day = day.AddDate(0, 0, -1)
		out = time.Date(day.Year(), day.Month(), day.Day()+1, 2, 10, 0, 0, loc)
	}
	return nightTimes{
		in:  time.Date(day.Year(), day.Month(), day.Day(), 18, 5, 0, 0, loc),
		out: out,
	}
}

// shiftStartInPast returns the most recent local occurrence of hh:mm that is
// already behind us, so a declared time built on it is never in the future.
func shiftStartInPast(t *testing.T, timezone string, hh, mm int) time.Time {
	t.Helper()
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		t.Fatalf("tenant timezone %q: %v", timezone, err)
	}
	now := time.Now().In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, loc)
	if !start.Add(20 * time.Minute).Before(now) {
		start = start.AddDate(0, 0, -1)
	}
	return start
}

// minutesSinceShiftStartOnServerClock reproduces what tap.lateness actually does
// today: it places the shift's wall-clock start on the SERVER's current local day
// and measures from there to now. It exists to pin the finding above, and it is
// the thing that should become unnecessary if lateness ever follows occurred_at.
func minutesSinceShiftStartOnServerClock(t *testing.T, timezone string, hh, mm int) int {
	t.Helper()
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		t.Fatalf("tenant timezone %q: %v", timezone, err)
	}
	now := time.Now().In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, loc)
	return int(now.Sub(start) / time.Minute)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func ptrUUID(id uuid.UUID) *uuid.UUID { return &id }
func ptrTime(v time.Time) *time.Time  { return &v }

// ---------------------------------------------------------------------------
// LIMITS — what this file does NOT prove, and what it steps around.
//
// It is at the END because the code above points at it by name (L1..L4). A day
// that reads as a full product demo is exactly the kind of fixture that gets
// trusted for more than it shows, so the gaps are listed rather than left for a
// reader to discover.
// ---------------------------------------------------------------------------
//
// L1 — THE CHIP'S SIGNATURE IS STIPULATED, NOT VERIFIED. Every NFC tap here
// opens the page for real and then flips ONE bit (CMACVerified) on the context
// the server minted. The file header says why: minting a genuine SDM MAC means a
// second implementation of the construction outside internal/sun. So this file
// proves everything about an NFC tap except that the chip's signature verifies;
// internal/sun proves that against its own vectors, and against real Postgres.
//
// L2 — LATENESS HAS NO HTTP-SIDE EVIDENCE HERE, AND CANNOT HAVE ANY TODAY.
// tap.Decision.MinutesLate is computed on every check-in and persisted nowhere:
// no column, no policy_context key, no confirmation screen. The only place the
// day sees a value is the manager's manual entry, where the CALLER holds the
// Decision — and that reading is the server-clock one this file pins as a
// finding. An earlier draft added an HTTP-side assertion (the record's instant is
// after the shift start); it was removed after measurement, because with
// tap.lateness mutated to `return nil` — a total lateness failure — the day
// walked straight past it and only died much later, at the manual entry's
// MinutesLate check. An assertion no engine defect can turn red is worse
// than no assertion: it reads like coverage. The skill's "late 17m" scenario is
// therefore NOT produced by this file over HTTP, and skill tappa-seed now says so.
//
// L3 — ✅ CLOSED (2026-08-02, M5-11, ADR 0008). It is kept, struck through rather
// than deleted, because the workaround it describes is gone from the file above and
// a reader who finds the old shape somewhere else should be able to recognise it.
//
// WHAT IT SAID. GetLastOpenTransaction returned ONE row (type='in', no later 'out',
// ORDER BY occurred_at DESC LIMIT 1) and was practice-neutral; checkin's gather
// DISCARDED that row when it was a practice row — `if !open.Practice {...}` — and
// did NOT look at the one beneath it. So a practice row that merely SORTED newest
// made a real open check-in invisible. Measured, two people, three taps each, plain
// HTTP both arms, the only difference being the practice row's occurred_at:
//
//	practice declared 4 h ago (older than the real 'in'):
//	  third tap -> direction "out", open check-ins left 0   (control)
//	practice at server-now (no declaration at all):
//	  third tap -> direction "in",  open check-ins left 2   (defect)
//
// §5's "direction is a toggle against the person's LAST OPEN check-in" was broken
// in the second arm: the checkout became an `in`, the real check-in never closed,
// and NOTHING signalled it — verdict ok, no note, no flag. Reachable over plain
// HTTP (occurred_at is a shipped form field; 3 h is well inside
// sys:occurred-at-bound's 72 h).
//
// WHAT CHANGED. `AND NOT t.practice` in GetLastOpenTransaction (ADR 0008). Ivan's
// training tap above no longer declares 17:50 — the workaround was removed as the
// task's finish condition, and this file's night shift closes with the training row
// sitting NEWEST, which is precisely the arrangement that used to break it.
//
// WHERE IT IS PINNED NOW, so this paragraph cannot rot into the only record:
// TestSeedDB_APracticeRowNeverHidesAnOlderOpenCheckIn (both arms, through
// checkin.Service.Record), TestGatherDB_APracticeRowDoesNotHideAnOlderOpenCheckIn
// (the query's own boundary, including two stacked practice rows) and
// TestDecide_PracticeIsAlwaysAnIn (the invariant that lets the NOT EXISTS stay
// practice-neutral).
//
// L4 — THE CREW IS EPHEMERAL AND RUN-STAMPED, SO THIS DAY DOES NOT FILL THE
// SEEDED ROSTER. Driving the SEEDED people instead would serve the skill's stated
// purpose (a dashboard with a realistic day on it) far better, and it was
// measured rather than assumed. Same fixed identity, two runs of the same probe:
//
//	run 1  rows=0, status=invited  -> invitation + activation + tour all work,
//	                                  first tap practice=true. The day as written.
//	run 2  rows=1, status=active   -> activation SKIPS the tour (lands on
//	                                  /activate/done, first slide never renders),
//	                                  first tap practice=FALSE, and the run starts
//	                                  with an open check-in inherited from run 1.
//
// So a fixed crew hosts this day exactly ONCE per database lifetime: transactions
// are immutable (§4.3), tappa_app has DELETE revoked, and three scenarios — the
// activation chain, the TRAINING tap, a clean direction chain — exist only while
// the person has no history. `make test` must be re-runnable, and skipping on the
// second run is not an option this repo accepts: the M5-06 card says twice, of a
// check CI silently skips, that skipping is not passing. Hence:
// fresh employees, stamped so they identify themselves, and skill tappa-seed's
// claim corrected to match what is actually produced.
