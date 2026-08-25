package handler

// employeeadd_test.go -- M6-13's HTTP boundary: the control, the form, the refusals
// and the redirect.
//
// 🔴 WHAT THESE TESTS EXIST FOR. The capability they cover was MISSING FROM THE
// PRODUCT for two milestones and two audit rounds, and was found by a customer in
// production. So the assertions here are deliberately about the things that would have
// caught that: does the section OFFER a way in, does the offer lead somewhere the
// router serves, and does pressing it reach the domain with what the manager typed.
//
// The database-side properties -- the schema default, the composite keys, the partial
// unique index, citext -- are asserted against real Postgres in
// internal/domain/tenant/staffadd_db_test.go and end to end in employeeadd_db_test.go.
// A fake cannot answer any of them, and one that appeared to would be worse than none.

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/tenant"
)

// addForm is a valid submission, which each test varies from.
func addForm() url.Values {
	return url.Values{
		"full_name":   {"Joseph Camilleri"},
		"location_id": {panelTestLocation.String()},
	}
}

// 🔴 THE DISCOVERABILITY ASSERTION, AND IT IS THE ONE THE PRODUCTION QUESTION ASKED
// FOR. A customer looking straight at this page could not find a way to add somebody
// because there was none. This requires the control to be ON the roster page -- not
// merely routable -- because a route nothing links to is a feature nobody has.
func TestEmployeeAdd_TheRosterOffersAWayIn(t *testing.T) {
	b := panelBrowserWithActions(t, &fakeStaff{}, &fakeInviter{})

	body := htmlOf(t, b.do(http.MethodGet, employeesHref, nil))
	if !strings.Contains(body, "Add somebody") {
		t.Fatal("the employees section offers no way to add anybody -- this is the gap M6-13 exists to close")
	}
	// The control leads to the disclosure, and the disclosure renders the form. Both
	// halves, because a link to a page that renders nothing is the same dead end.
	if !strings.Contains(body, addParam+"="+addOpenValue) {
		t.Fatalf("the control does not link to the add form (looking for %s=%s)", addParam, addOpenValue)
	}
	opened := htmlOf(t, b.do(http.MethodGet, employeesHref+"?"+addParam+"="+addOpenValue, nil))
	for _, field := range []string{`name="full_name"`, `name="location_id"`, `name="email"`, `name="role"`, `name="department_id"`} {
		if !strings.Contains(opened, field) {
			t.Errorf("the add form has no %s control", field)
		}
	}
}

// 🔴 THE EMPTY PATH, BUILT AND RUN RATHER THAN REASONED AWAY. employees.location_id is
// NOT NULL, so a business with no venue cannot have employees. The screen must SAY so
// and point at the fix -- it must not render a venue picker with nothing in it, and it
// must not answer 500.
//
// ⚠️ SIGNUP CREATES A VENUE TODAY, WHICH IS WHY THIS IS EASY TO DISMISS. That is one
// code path's current behaviour, not an invariant: nothing stops a business removing
// its only venue before it has hired anybody. "A healthy system is EMPTY" is the
// lesson this repository has paid for most often.
func TestEmployeeAdd_ABusinessWithNoVenueIsToldWhatToDo(t *testing.T) {
	ledgerFake := newFakeLedger()
	ledgerFake.roster.Options = ledger.Options{} // no venues at all
	b := panelBrowserWithLedger(t, ledgerFake, &fakeStaff{}, &fakeInviter{})

	rec := b.do(http.MethodGet, employeesHref+"?"+addParam+"="+addOpenValue, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("a business with no venue got %d from the employees section, want 200 -- "+
			"an empty business is a STATE, not a fault", rec.Code)
	}
	body := htmlOf(t, rec)
	if !strings.Contains(body, "Add a venue first") {
		t.Fatal("the screen does not tell a venueless business what to do")
	}
	// 🔴 THE ASSERTION IS SCOPED TO THE SECTION'S OWN DOCKET, AND IT TOOK TWO GOES.
	// It first asserted `strings.Contains(body, locationsHref)` -- satisfied by the
	// TAB BAR, so deleting the real button left three packages green. The second
	// version looked for the button's text anywhere on the page, and an audit beat
	// that too by deleting the button AND renaming the nav tab to the same words: the
	// nav link carries both the text and the href, so the pair still matched and the
	// venueless screen had nothing pressable left.
	//
	// The medicine is the one this file already uses for the venue <select>: scope the
	// assertion to the block that owns the control. Nothing from the page chrome can
	// be inside the section's docket, so no rename anywhere else can satisfy it.
	card := docketBlock(t, body, "Add a venue first")
	if !strings.Contains(card, ">Go to venues<") {
		t.Fatal("🔴 the venueless screen offers no control to go and create a venue -- the " +
			"instruction has no destination the manager can press")
	}
	if !strings.Contains(anchorFor(t, card, "Go to venues"), locationsHref) {
		t.Errorf("the control in the section's own card does not lead to %s", locationsHref)
	}
	// 🔴 AND IT MUST NOT OFFER THE FORM. A submission from it could only be refused,
	// which is the exact shape this section carries a test against.
	if strings.Contains(body, `name="full_name"`) {
		t.Fatal("🔴 the add form is rendered for a business with no venue -- every submission " +
			"from it can only be refused")
	}
	if strings.Contains(body, `action="`+employeeAddHref+`"`) {
		t.Fatal("🔴 the page posts to the add route with no venue to place anybody in")
	}
}

// 🔴 §4.6 — A VENUELESS BUSINESS THAT POSTS IS TOLD THE REFUSAL ITSELF, not merely
// shown a page about venues.
//
// THE DEFECT THIS PINS WAS MEASURED AND IS EASY TO RE-CREATE: the handler attached the
// refusal to the add FORM, and rosterAdd does not render a form when the business has
// no venue — so the message was silently dropped. The response was 200, the page said
// "Add a venue first", and nothing anywhere said that a submission had just been
// refused. The sentence had even been written; no branch could reach it.
//
// THE GET IS THE CONTROL. On an ordinary visit nothing has been attempted, so the
// refusal notice must be ABSENT — otherwise this test would pass against a page that
// always claims something was refused.
func TestEmployeeAdd_AVenuelessBusinessIsToldTheRefusalItself(t *testing.T) {
	newBrowser := func() *browser {
		lf := newFakeLedger()
		lf.roster.Options = ledger.Options{} // no venues at all
		return panelBrowserWithLedger(t, lf, &fakeStaff{addErr: tenant.ErrUnknownPlacement}, &fakeInviter{})
	}

	// CONTROL: a plain GET refuses nothing and must not claim otherwise.
	get := htmlOf(t, newBrowser().do(http.MethodGet, employeesHref, nil))
	if strings.Contains(get, "Nobody was added") {
		t.Error("an ordinary visit claims a refusal that never happened")
	}
	if !strings.Contains(get, "Add a venue first") {
		t.Error("a venueless business is not told it needs a venue")
	}

	// THE POST: something WAS refused, and the manager must be told.
	rec := newBrowser().do(http.MethodPost, employeeAddHref, addForm())
	if rec.Code != http.StatusOK {
		t.Fatalf("a venueless add answered %d, want 200 with the refusal on the page", rec.Code)
	}
	body := htmlOf(t, rec)
	if !strings.Contains(body, "Nobody was added") {
		t.Error("🔴 the manager pressed Add, the server refused, and the page does not say so " +
			"(§4.6: a refusal is never silent)")
	}
	if !strings.Contains(body, "Choose a venue that belongs to this business") {
		t.Error("the page does not carry the refusal's own sentence")
	}
	if !strings.Contains(body, "Add a venue first") {
		t.Error("the page no longer says what to do about it")
	}
}

// A VALID SUBMISSION REACHES THE DOMAIN WITH WHAT THE MANAGER TYPED, and the tenant
// and actor come from the SESSION rather than the form (§4.5, §4.7).
func TestEmployeeAdd_ReachesTheDomainWithTheSessionsTenantAndActor(t *testing.T) {
	staff := &fakeStaff{}
	b := panelBrowserWithActions(t, staff, &fakeInviter{})

	form := addForm()
	form.Set("email", "joseph@example.mt")
	form.Set("role", "Chef")
	// A hostile submission naming another tenant. It must be IGNORED, not honoured.
	form.Set("tenant_id", uuid.NewString())
	form.Set("actor_id", uuid.NewString())

	rec := b.do(http.MethodPost, employeeAddHref, form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("add answered %d, want 303", rec.Code)
	}
	adds := staff.addCommands()
	if len(adds) != 1 {
		t.Fatalf("the domain saw %d add(s), want 1", len(adds))
	}
	got := adds[0]
	if got.TenantID != panelTestTenant {
		t.Errorf("tenant = %v, want the SESSION's %v -- a posted tenant must never be read",
			got.TenantID, panelTestTenant)
	}
	if got.ActorID != panelTestAdmin {
		t.Errorf("actor = %v, want the SESSION's %v", got.ActorID, panelTestAdmin)
	}
	if got.FullName != "Joseph Camilleri" || got.Email != "joseph@example.mt" || got.Role != "Chef" {
		t.Errorf("the typed values did not survive the boundary: %+v", got)
	}
	if got.LocationID != panelTestLocation {
		t.Errorf("venue = %v, want %v", got.LocationID, panelTestLocation)
	}
	if got.DepartmentID != nil {
		t.Errorf("department = %v, want nil -- an unset department is 'no department'", got.DepartmentID)
	}
}

// 🔴 THE REDIRECT OPENS THE NEW PERSON'S CARD, WHICH IS WHAT MAKES ADD -> INVITE ONE
// SCREEN. Somebody added is 'invited' and cannot tap until a link is spent, so the
// invite button has to be the next thing they see.
func TestEmployeeAdd_TheRedirectOpensTheNewPersonsCard(t *testing.T) {
	staff := &fakeStaff{}
	b := panelBrowserWithActions(t, staff, &fakeInviter{})

	rec := b.do(http.MethodPost, employeeAddHref, addForm())
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("add answered %d, want 303", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "manage="+panelTestEmployee.String()) {
		t.Fatalf("the redirect %q does not open the new person's card -- the invite button "+
			"is on it, and without it the chain needs a second screen", loc)
	}
	if !strings.Contains(loc, "done=added") {
		t.Fatalf("the redirect %q does not acknowledge the add", loc)
	}
	// 🔴 THE CURSOR IS DROPPED. The roster is ordered by name and pages at 50, so a
	// stale cursor would return the manager to a page the new person cannot be on.
	if strings.Contains(loc, "after_id=") {
		t.Fatalf("the redirect %q keeps the paging cursor", loc)
	}
}

// THE ROSTER POSITION SURVIVES A SUCCESSFUL ADD (minus the cursor), so a manager who
// was looking at a filtered list comes back to it.
func TestEmployeeAdd_KeepsTheFiltersAndDropsTheCursor(t *testing.T) {
	staff := &fakeStaff{}
	b := panelBrowserWithActions(t, staff, &fakeInviter{})

	form := addForm()
	form.Set("status", ledger.StatusActive)
	form.Set("after_id", uuid.NewString())

	loc := b.do(http.MethodPost, employeeAddHref, form).Header().Get("Location")
	if !strings.Contains(loc, "status="+ledger.StatusActive) {
		t.Errorf("the redirect %q lost the manager's filter", loc)
	}
	if strings.Contains(loc, "after_id=") {
		t.Errorf("the redirect %q kept the cursor", loc)
	}
}

// 🔴 A REJECTED FORM RE-RENDERS WITH THE TYPING INTACT AND WRITES NOTHING. Five fields
// is too many to throw away over one mistyped address, which is the trade saveVenue
// already makes for the venue form.
func TestEmployeeAdd_ARejectedFormComesBackFilledIn(t *testing.T) {
	tests := []struct {
		name      string
		domainErr error
		field     string
		// wants is a fragment the re-rendered page must contain.
		wants string
	}{
		{"name refused", tenant.ErrEmployeeName, "full_name", "name we can store"},
		{"email refused", tenant.ErrEmployeeEmail, "email", "does not look like"},
		{"role refused", tenant.ErrEmployeeRole, "role", "job title is too long"},
		{"email taken", tenant.ErrEmailTaken, "email", "capitals make no"},
		{"placement refused", tenant.ErrUnknownPlacement, "location_id", "belongs to this business"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			staff := &fakeStaff{addErr: tc.domainErr}
			b := panelBrowserWithActions(t, staff, &fakeInviter{})

			form := addForm()
			form.Set("full_name", "Grazzja Farrugia")
			form.Set("email", "typed@example.mt")
			form.Set("role", "Sous chef")

			rec := b.do(http.MethodPost, employeeAddHref, form)
			// 200 rather than 303: nothing was written, and the correction belongs on
			// the page the manager is already looking at.
			if rec.Code != http.StatusOK {
				t.Fatalf("a refused add answered %d, want 200 with the form back", rec.Code)
			}
			body := htmlOf(t, rec)
			if !strings.Contains(strings.ToLower(body), strings.ToLower(tc.wants)) {
				t.Errorf("the page does not explain the refusal (looking for %q)", tc.wants)
			}
			// 🔴 THE TYPING IS BACK. Without this the test would pass for a page that
			// rendered an empty form with an error on it.
			for _, typed := range []string{"Grazzja Farrugia", "typed@example.mt", "Sous chef"} {
				if !strings.Contains(body, typed) {
					t.Errorf("the re-rendered form lost %q", typed)
				}
			}
			if !strings.Contains(body, `aria-invalid="true"`) {
				t.Error("no control is marked invalid, so a screen reader is told nothing")
			}
		})
	}
}

// AN UNREADABLE VENUE IS REFUSED BY THE BOUNDARY WITHOUT TROUBLING THE DOMAIN, and an
// unreadable department likewise. Neither may be silently treated as absent: a missing
// venue is not "nowhere" (the column is NOT NULL) and a mangled department is not "no
// department".
func TestEmployeeAdd_UnreadableIdsAreRefusedAtTheBoundary(t *testing.T) {
	tests := []struct {
		name  string
		set   func(url.Values)
		field string
	}{
		{"no venue at all", func(v url.Values) { v.Del("location_id") }, "location_id"},
		{"venue is not a uuid", func(v url.Values) { v.Set("location_id", "not-a-uuid") }, "location_id"},
		{"venue is empty", func(v url.Values) { v.Set("location_id", "") }, "location_id"},
		{"department is not a uuid", func(v url.Values) { v.Set("department_id", "¯\\_(ツ)_/¯") }, "department_id"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			staff := &fakeStaff{}
			b := panelBrowserWithActions(t, staff, &fakeInviter{})
			form := addForm()
			tc.set(form)

			rec := b.do(http.MethodPost, employeeAddHref, form)
			if rec.Code != http.StatusOK {
				t.Fatalf("answered %d, want 200 with the form back", rec.Code)
			}
			if n := len(staff.addCommands()); n != 0 {
				t.Fatalf("the domain was called %d time(s) for an unreadable submission; "+
					"the boundary is supposed to refuse it", n)
			}
		})
	}
}

// AN EMPTY DEPARTMENT IS A PLACEMENT, NOT A MISSING FIELD (00003: a bar does not model
// departments), so it must reach the domain as nil rather than be refused.
func TestEmployeeAdd_AnEmptyDepartmentIsAPlacement(t *testing.T) {
	staff := &fakeStaff{}
	b := panelBrowserWithActions(t, staff, &fakeInviter{})

	form := addForm()
	form.Set("department_id", "")
	if rec := b.do(http.MethodPost, employeeAddHref, form); rec.Code != http.StatusSeeOther {
		t.Fatalf("add answered %d, want 303 -- 'no department' is a legitimate placement", rec.Code)
	}
	adds := staff.addCommands()
	if len(adds) != 1 || adds[0].DepartmentID != nil {
		t.Fatalf("department reached the domain as %v, want nil", adds[0].DepartmentID)
	}
}

// 🔴 THE ROUTE IS ON ProtectWriting's CHAIN, WITH THE ORIGIN CHECK AHEAD OF THE
// RESOLVER. A create route reachable cross-origin is one another site can drive with a
// signed-in manager's cookie.
//
// 🔴 IT COUNTS RESOLVER READS RATHER THAN STATUS CODES, AND THE FIRST DRAFT OF THIS
// TEST GOT THAT WRONG IN EXACTLY THE WAY billingactions_test.go ALREADY WARNS ABOUT.
// It asserted that a cross-origin POST is NOT answered 303 -- which is precisely what
// the gate DOES answer (a.redirect to /admin), so the test failed against correct
// code. The status is identical on both chains; what the gate's POSITION buys is COST,
// and employeeactions_test.go records the measurement: with the check after the
// resolver, 300 cross-origin POSTs spent a manager's session budget and locked them out
// of their own panel.
func TestEmployeeAdd_IsRefusedCrossOriginBeforeTheResolverRuns(t *testing.T) {
	staff := &fakeStaff{}
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: "owner", FullName: "KF Owner",
		}, nil
	}}
	h, err := NewAdminAuth(admins, &fakeTrail{}, newFakeLedger(), newFakeLedger(), &fakeReviewer{},
		staff, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(),
		newFakeScribe(), newFakeBooks(), newFakeTexts(), newFakeAccount(), nil, adminTestConfig(),
		slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

	// POSITIVE CONTROL FIRST. Without it a router that never reaches the resolver at
	// all would satisfy the zero below.
	if rec := b.do(http.MethodPost, employeeAddHref, addForm()); rec.Code != http.StatusSeeOther {
		t.Fatalf("same-origin add answered %d, want 303", rec.Code)
	}
	baseline := admins.verifiedCount()
	if baseline == 0 {
		t.Fatal("a served POST resolved no session; the counter is not wired")
	}
	if n := len(staff.addCommands()); n != 1 {
		t.Fatalf("the control add reached the domain %d time(s), want 1", n)
	}

	b.origin = "https://not-tappa.example"
	rec := b.do(http.MethodPost, employeeAddHref, addForm())
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin" {
		t.Errorf("cross-origin add: %d %q, want 303 /admin", rec.Code, rec.Header().Get("Location"))
	}
	if got := admins.verifiedCount() - baseline; got != 0 {
		t.Errorf("a cross-origin add cost %d resolver read(s), want 0.\n"+
			"The Origin check must run BEFORE requireAdmin; a POST registered in "+
			"mountSections would silently take the READ chain.", got)
	}
	if n := len(staff.addCommands()); n != 1 {
		t.Errorf("🔴 a cross-origin request added somebody: the domain was called %d time(s), want still 1", n)
	}
}

// THE ROUTE IS POST-ONLY. A create reachable by GET is one a link, a prefetch or a
// crawler can fire.
func TestEmployeeAdd_IsPostOnly(t *testing.T) {
	staff := &fakeStaff{}
	b := panelBrowserWithActions(t, staff, &fakeInviter{})

	rec := b.do(http.MethodGet, employeeAddHref, nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET %s answered %d, want 405", employeeAddHref, rec.Code)
	}
	if n := len(staff.addCommands()); n != 0 {
		t.Fatalf("a GET reached the domain %d time(s)", n)
	}
}

// THE BODY IS BOUNDED BEFORE IT IS PARSED, so an oversized submission costs the
// allocation of the ceiling rather than of the body.
func TestEmployeeAdd_BoundsTheBody(t *testing.T) {
	staff := &fakeStaff{}
	b := panelBrowserWithActions(t, staff, &fakeInviter{})

	// 🔴 A LITERAL, NOT maxEmployeeActionBody*2. An assertion written in terms of the
	// constant slides with it: an audit widened the ceiling from 8 KiB to 8 MiB (1024x)
	// and everything stayed green, because every test that mentioned it moved too. That
	// is the shape TestPersonName_TheBoundIsExactlyOneHundredAndTwentyRunes closes for
	// the name bound, and this file was repeating it for the body bound.
	//
	// ⚠️ THE CONSTANT ITSELF IS STILL UNPINNED and that is a counted limit rather than
	// a fix: maxEmployeeActionBody predates M6-13 and bounds four routes, so pinning it
	// belongs with that constant and not with this card. What this line buys is that
	// THIS test measures a real size instead of whatever the constant happens to say.
	huge := "full_name=" + strings.Repeat("a", 16<<10)
	rec := b.doRaw(http.MethodPost, employeeAddHref, huge)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("an oversized add answered %d, want a 303 to the problem", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Location"), "problem=unreadable") {
		t.Errorf("the redirect %q does not say the submission could not be read",
			rec.Header().Get("Location"))
	}
	if n := len(staff.addCommands()); n != 0 {
		t.Fatalf("an oversized body reached the domain %d time(s)", n)
	}
}

// 🔴 §4.7 AS A PROPERTY OF THE HANDLER, NOT AS A LIST OF ITS LOG LINES.
//
// THIS TEST WAS REWRITTEN BECAUSE THE SAME DEFECT CAME BACK TWICE IN THE SAME
// FUNCTION. Round 3 found a name and an address reachable from the DOMAIN logger;
// the repair covered that logger and this handler's REFUSAL lines. Round 5 then put
// them into the handler's SUCCESS line -- two identifiers away from values it was
// already logging -- and every package stayed green, including the red-line scanner.
//
// 🔴 THE OLD SHAPE WAS A LIST AND THAT IS WHY IT LEAKED. "These call sites do not
// leak" is an enumeration: each round added an entry, and the next round found one the
// list did not have. It is the same shape as the citation scanner's root x extension
// pairing, which leaked five times and was fixed by replacing it with ONE RULE.
//
// THE RULE HERE: drive the REAL handler through the REAL router with a REAL logger,
// once per OBSERVABLE OUTCOME, and require that the whole captured stream carries no
// value the caller sent -- whichever line emitted it, and however many lines there
// are.
//
// 🔴 WHAT THIS TABLE CAN AND CANNOT PROVE -- THE DECISION, M6-13 ROUND 10.
//
// It proves that the outcomes BELOW do not leak. It cannot prove "nothing leaks
// anywhere", and three rounds of trying moved the axis three times: round 5 found a
// CALL SITE the list did not have, round 7 an OUTCOME the fixture could not produce,
// round 9 an INPUT the table does not send (a log line bound to the roster's `name`
// filter, which this fixture never fills). That is not a flaw in the table -- it is
// the limit of its genus. A test over DRIVEN PATHS cannot speak for a path nobody
// drove, and each repair invited the next axis.
//
// THREE ANSWERS WERE WEIGHED AND THE COSTS MEASURED:
//
//	type-level    Wrap the value so it cannot be printed, or give the handler a
//	              logger interface that refuses caller strings. STRONGEST -- it is
//	              the Trail/RecordTx medicine, a compile error rather than a test.
//	              MEASURED AND NOT CHOSEN: 224 a.log.* call sites across 22 files in
//	              this package and 46 more in internal/domain. That is a package-wide
//	              transformation, not a card's work, and it would rewrite code M6-13
//	              has no business touching. It is the right eventual answer.
//	source scan   A red-line rule that reads the SOURCE for caller-derived values
//	              inside a log call. CHOSEN, and it is cheap. It repairs a real gap in
//	              the repository's own claim -- CLAUDE.md §4 says `make audit` scans
//	              the red lines mechanically, and §4.7's personal-data half was never
//	              scanned; all three historical leaks returned redline exit 0. It
//	              lives in scripts/redline-check.sh as R7b.
//
//	              🔴 IT IS NOT AXIS-PROOF, AND THIS COMMENT SAID IT WAS. The claim
//	              here was "independent of the input space, so the axis cannot shift
//	              again". An audit falsified it in one command, and the axis moved a
//	              FIFTH time -- to the LOGGER'S OWN IDENTIFIER. R7b's pattern requires
//	              the call to be spelled `log.`/`slog.`/`fmt.`, so
//	              `a.logger.Warn(…, posted.FullName)`, `lg.Warn(…)`, `a.Warn(…)` and
//	              `a.audit.Info(…)` all walk past it. Measured: four for four.
//
//	              ⚠️ AND THE FOUR FALSE-NEGATIVE CLASSES R7b LISTS NAME THE RECEIVER OF
//	              THE VALUE (`row.Name`, `q.Name`), NOT THE RECEIVER OF THE LOGGER --
//	              so the `.FullName` / `.Email` / `r.FormValue(` triggers are NOT
//	              "receiver-independent" the way that list implies. R7b's own limit
//	              block is the authoritative statement of what it catches; read it
//	              there rather than trusting a summary here.
//
//	              MATERIALITY, MEASURED RATHER THAN ASSUMED -- AND THE FIRST
//	              MEASUREMENT WAS WRONG IN A WAY WORTH RECORDING. This paragraph once
//	              read "345 of them, and the six other .Info/.Warn/.Error matches are
//	              not loggers at all". The COUNT was right and the INSPECTION it
//	              licensed was not: there were SEVEN, and the seventh
//	              (cmd/tappa/main.go, the legal-texts start-up warning) WAS a logger,
//	              reached as `slog.Default().Error(...)`.
//
//	              🔴 HOW IT HID, WHICH IS THE ACTUAL LESSON: both of the patterns used
//	              to produce "six" missed that spelling -- the total pattern wanted
//	              word characters before `.Error(` and found `)`, and the "via" pattern
//	              wanted a bare `slog.` receiver. So it fell out of BOTH buckets, and a
//	              count derived by SUBTRACTING one from the other could never surface
//	              it. A difference of two measurements can only see what at least one
//	              of them counts.
//
//	              🔴 AND IT WAS REACHABLE, unlike the logger-identifier class above: an
//	              audit injected a name and an address on that line and redline-check
//	              returned exit 0, while the same leak written `slog.Error(...)`
//	              returned exit 1. It was closed by BINDING THE LOGGER TO A NAME there
//	              (see cmd/tappa/main.go) rather than by adding a sixth pattern --
//	              removing the shape, which is what R7b's own type-level note argues
//	              for. Measured after the change: the identical injection now returns
//	              exit 1 with R7b naming the file.
//
//	              WHERE THAT LEAVES THE COUNT: every logger call in production is
//	              spelled log./slog. -- 347 of them -- and the remaining seven
//	              `.Info(`/`.Warn(`/`.Error(` matches are not logger calls (http.Error,
//	              err.Error() x3, csv.Writer.Error, and two COMMENTS, one of them the
//	              comment in main.go that describes this very defect). The
//	              logger-identifier class above is therefore still unreachable today,
//	              and that sentence now rests on an inspection that was actually done.
//
//	counted limit Say the net covers driven paths and leave the rest to review.
//	              REJECTED AS THE ONLY answer -- it was already true and it is what
//	              let the axis move twice.
//
// SO THE DIVISION OF LABOUR IS DELIBERATE: R7b decides the SHAPE (a caller value
// named inside a log call, anywhere in the tree, whether or not a test drives it);
// this table decides the BEHAVIOUR (what actually reaches a real logger through a
// real router, including encodings and fragments a grep cannot see). Neither
// subsumes the other, and R7b's own comment measures what it still misses -- a value
// copied into a neutral local first, which was demonstrated rather than asserted.
// 🔴 EVERY CASE CARRIES ITS OWN POSITIVE CONTROL, AND THAT IS THE OTHER HALF OF THE
// REPAIR. An audit silenced all eight refusal lines and the previous version of this
// test PASSED -- a net that cannot tell "nothing leaked" from "nothing was written" is
// not a net. Each row below asserts the logger produced output for that outcome, so
// deleting a line fails just as loudly as leaking through one.
//
// THE SENTINELS GO IN EVERY FREE-TEXT FIELD. A leak of the role is as much a §4.7
// breach as a leak of the name, and the values are distinctive so they cannot collide
// with an id, a timestamp or another field's bytes and make an absence accidental.
func TestEmployeeAdd_NoOutcomeLogsAnythingTheCallerSent(t *testing.T) {
	const (
		sentinelName  = "Zzyzx-Caller-Name-9f1c"
		sentinelEmail = "zzyzx-caller-9f1c@example.mt"
		sentinelRole  = "Zzyzx-Caller-Role-9f1c"
	)

	// THE OUTCOMES ARE NAMED BY WHAT THE MANAGER SEES, not by which log statement runs.
	// That is what keeps this a rule: the table is derived from the function's
	// behaviour, and a new log line has to live on one of these paths to be reachable
	// at all.
	outcomes := []struct {
		name      string
		staff     *fakeStaff
		mutate    func(url.Values)
		oversize  bool
		rosterErr error
	}{
		{name: "success", staff: &fakeStaff{}},
		{name: "the name is refused", staff: &fakeStaff{addErr: tenant.ErrEmployeeName}},
		{name: "the email is refused", staff: &fakeStaff{addErr: tenant.ErrEmployeeEmail}},
		{name: "the role is refused", staff: &fakeStaff{addErr: tenant.ErrEmployeeRole}},
		{name: "the email is taken", staff: &fakeStaff{addErr: tenant.ErrEmailTaken}},
		{name: "the placement is refused", staff: &fakeStaff{addErr: tenant.ErrUnknownPlacement}},
		{name: "the domain fails outright", staff: &fakeStaff{addErr: errors.New("database on fire")}},
		{
			name:   "the venue is not a uuid",
			staff:  &fakeStaff{},
			mutate: func(v url.Values) { v.Set("location_id", "not-a-uuid") },
		},
		{
			name:   "the department is not a uuid",
			staff:  &fakeStaff{},
			mutate: func(v url.Values) { v.Set("department_id", "not-a-uuid") },
		},
		{name: "the body is oversized", staff: &fakeStaff{}, oversize: true},
		{
			// 🔴 THE OUTCOME THE FIXTURE COULD NOT PRODUCE. A refused add re-renders the
			// roster, and that read can fail; the handler logs when it does. Nothing
			// else in this table reaches that line.
			name:      "the roster read fails while re-rendering a refusal",
			staff:     &fakeStaff{addErr: tenant.ErrEmployeeName},
			rosterErr: errors.New("connection reset by peer"),
		},
	}

	for _, tc := range outcomes {
		t.Run(tc.name, func(t *testing.T) {
			lf := newFakeLedger()
			if tc.rosterErr != nil {
				lf.rosterErr = tc.rosterErr
			}
			b, logs := panelBrowserWithLogsAndLedger(t, lf, tc.staff, &fakeInviter{})

			if tc.oversize {
				// The caller's bytes still reach the boundary here, and net/url's parse
				// failure QUOTES its input -- the branch that leaked three bytes of a
				// manager's note in M6-04.
				b.doRaw(http.MethodPost, employeeAddHref,
					"full_name="+sentinelName+"&email="+sentinelEmail+
						"&role="+sentinelRole+"&pad="+strings.Repeat("a", maxEmployeeActionBody*2))
			} else {
				form := addForm()
				form.Set("full_name", sentinelName)
				form.Set("email", sentinelEmail)
				form.Set("role", sentinelRole)
				if tc.mutate != nil {
					tc.mutate(form)
				}
				b.do(http.MethodPost, employeeAddHref, form)
			}

			written := logs.String()
			// POSITIVE CONTROL, PER OUTCOME. Without it, silencing the branch makes the
			// assertion below vacuously true -- which is exactly how the previous version
			// of this test survived having all eight refusal lines deleted.
			if strings.TrimSpace(written) == "" {
				t.Fatalf("the handler logged NOTHING for outcome %q, so the absence of the "+
					"caller's values below proves nothing. Every outcome this function has "+
					"must leave a trace, or §4.7 is being satisfied by silence.", tc.name)
			}
			for _, secret := range []string{sentinelName, sentinelEmail, sentinelRole} {
				if leak := findLeak(written, secret); leak != "" {
					t.Errorf("🔴 §4.7: the handler log carries %s of %q on the %q path. A process "+
						"log has no retention story and no tenant boundary; personal data written "+
						"there cannot be corrected or erased.\nstream:\n%s",
						leak, secret, tc.name, written)
				}
			}
		})
	}
}

// A DOMAIN FAILURE THAT IS NOT ONE OF THE NAMED REFUSALS IS A 500, NOT A FORM ERROR.
// Guessing at an unknown error is how an outage gets rendered as "that name is too
// long" and sends a manager to fix their own typing.
func TestEmployeeAdd_AnUnknownFailureIsNotShownAsAFormError(t *testing.T) {
	staff := &fakeStaff{addErr: errors.New("the database is on fire")}
	b := panelBrowserWithActions(t, staff, &fakeInviter{})

	rec := b.do(http.MethodPost, employeeAddHref, addForm())
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("an unknown domain failure answered %d, want 500", rec.Code)
	}
}

// 🔴 THE BILLING SENTENCE IS ON THE FORM (the card's fifth criterion), AND THIS
// ASSERTS THE CLAIM RATHER THAN THE CONSTANT. `strings.Contains(body, addBillingNote)`
// was the original assertion and it is a TAUTOLOGY -- the page is rendered from that
// constant, so it holds for any text including a lie about somebody's money. An audit
// proved it by rewriting the sentence to the opposite and watching the suite stay
// green.
//
// The phrase below is this file's own. Which of the two claims is CORRECT is settled
// against the database in TestEmployeeAddDB_TheBillingNoteMatchesThePredicate; this
// one only holds the disclosure on the screen, because a measured sentence nobody
// renders is not a disclosure.
func TestEmployeeAdd_TheFormSaysWhatItDoesToTheBill(t *testing.T) {
	b := panelBrowserWithActions(t, &fakeStaff{}, &fakeInviter{})

	body := htmlOf(t, b.do(http.MethodGet, employeesHref+"?"+addParam+"="+addOpenValue, nil))
	unescaped := html.UnescapeString(body)
	if !strings.Contains(unescaped, sentenceNotBillableYet) {
		t.Fatalf("the add form does not tell the manager what adding somebody does to the bill "+
			"(looking for the whole sentence %q)", sentenceNotBillableYet)
	}
	// 🔴 THE OPPOSING SENTENCE MUST BE ABSENT TOO, which an audit pointed out this twin
	// was missing: a page that printed BOTH claims satisfied the `required` half and
	// only the database-backed test caught it. A screen that says both things about
	// somebody's bill is worse than one that says the wrong thing, because the reader
	// cannot tell which to believe.
	if strings.Contains(unescaped, sentenceBillableOnAdd) {
		t.Errorf("the add form carries BOTH billing sentences; a manager cannot tell which "+
			"applies:\n  also found: %q", sentenceBillableOnAdd)
	}
}

// THE SINGLE VENUE IS PRE-SELECTED, because with one option there is only one answer.
func TestEmployeeAdd_ASingleVenueIsPreSelected(t *testing.T) {
	b := panelBrowserWithActions(t, &fakeStaff{}, &fakeInviter{})

	body := htmlOf(t, b.do(http.MethodGet, employeesHref+"?"+addParam+"="+addOpenValue, nil))
	if !strings.Contains(body, `value="`+panelTestLocation.String()+`" selected`) {
		t.Fatalf("the only venue is not pre-selected; the manager has to choose from a list of one")
	}
}

// 🔴 U-3: A REJECTED FORM COMES BACK WITH THE MANAGER'S ROSTER POSITION, NOT A RESET
// ONE. The handler reads the filter from the POST BODY on this path, because the
// request that reaches it is the POST to the add route and its query string is empty --
// parseRosterFilter would silently return an unfiltered, page-one view.
//
// 🔴 THE CODE ALREADY DESCRIBED THIS DEFECT IN ITS OWN COMMENT AND NOTHING MEASURED
// IT. An audit deleted the three-line block and the suite stayed green; the comment
// beside it spells out exactly what that costs ("the form would come back correct and
// the list underneath it would be somebody else's view"). A described defect with no
// test is a defect waiting for the comment to be deleted as noise.
func TestEmployeeAdd_ARejectedFormKeepsTheRosterPosition(t *testing.T) {
	staff := &fakeStaff{addErr: tenant.ErrEmployeeName}
	b := panelBrowserWithActions(t, staff, &fakeInviter{})

	form := addForm()
	form.Set("status", ledger.StatusActive)
	form.Set("name", "borg")

	rec := b.do(http.MethodPost, employeeAddHref, form)
	if rec.Code != http.StatusOK {
		t.Fatalf("a refused add answered %d, want 200 with the form back", rec.Code)
	}
	body := htmlOf(t, rec)

	// THE FILTER BAR CAME BACK SET. Without the body-reading branch the select renders
	// with nothing selected and the roster underneath is everybody, page one.
	if !strings.Contains(body, `value="`+ledger.StatusActive+`" selected`) {
		t.Error("🔴 the status filter was lost when the form was refused -- the list under the " +
			"corrected form is not the list the manager was looking at")
	}
	if !strings.Contains(body, `value="borg"`) {
		t.Error("🔴 the name filter was lost when the form was refused")
	}
	// AND THE DOMAIN SAW THE FILTER AS A FILTER, never as part of the new person.
	adds := staff.addCommands()
	if len(adds) != 1 {
		t.Fatalf("the domain saw %d add(s), want 1", len(adds))
	}
	if adds[0].FullName == "borg" {
		t.Error("the roster's name FILTER was read as the new person's name")
	}
}

// 🔴 F7: WITH SEVERAL VENUES, AN UNTOUCHED CONTROL SUBMITS NOTHING AND IS REFUSED --
// it does not quietly hire somebody into whichever venue sorts first.
//
// THE DEFECT WAS A COMMENT THAT CLAIMED A PROPERTY THE MARKUP DID NOT HAVE. No option
// carried `selected`, and HTML says a select with none marked submits its FIRST
// option, so a manager who never touched the control placed somebody in the
// alphabetically-first venue silently. The comment asserted the opposite.
//
// BOTH HALVES ARE ASSERTED, because the placeholder alone is not the property: the
// form must OFFER an empty choice AND the server must REFUSE it.
func TestEmployeeAdd_WithSeveralVenuesNothingIsChosenByDefault(t *testing.T) {
	second := uuid.New()
	lf := newFakeLedger()
	lf.roster.Options = ledger.Options{Locations: []ledger.Option{
		{ID: panelTestLocation, Label: "Alpha Venue"},
		{ID: second, Label: "Beta Venue"},
	}}
	staff := &fakeStaff{}
	b := panelBrowserWithLedger(t, lf, staff, &fakeInviter{})

	body := htmlOf(t, b.do(http.MethodGet, employeesHref+"?"+addParam+"="+addOpenValue, nil))
	// 🔴 THE ASSERTION IS SCOPED TO THE VENUE SELECT, and the first draft of this test
	// was not -- it looked for `<option value="" selected` anywhere on the page and so
	// matched the DEPARTMENT placeholder, which is always present. Measured: deleting
	// the venue placeholder left that draft GREEN. A test for one control that another
	// control can satisfy is not a test for either.
	venueSelect := selectBlock(t, body, "add-location")
	// 1. THE EMPTY CHOICE IS THERE AND IT IS THE ONE SELECTED.
	if !strings.Contains(venueSelect, `<option value="" selected`) {
		t.Error("🔴 no empty option is selected in the VENUE control, so the browser will " +
			"submit the first venue for a manager who never touched it")
	}
	// 2. AND NO REAL VENUE IS PRE-SELECTED.
	if strings.Contains(body, `value="`+panelTestLocation.String()+`" selected`) ||
		strings.Contains(body, `value="`+second.String()+`" selected`) {
		t.Error("🔴 a venue is pre-selected even though there are several; nobody chose it")
	}

	// 3. THE SERVER REFUSES THE EMPTY SUBMISSION, which is what makes the placeholder
	// more than decoration.
	form := addForm()
	form.Set("location_id", "")
	rec := b.do(http.MethodPost, employeeAddHref, form)
	if rec.Code != http.StatusOK {
		t.Fatalf("an empty venue answered %d, want 200 with the form back", rec.Code)
	}
	if n := len(staff.addCommands()); n != 0 {
		t.Fatalf("🔴 an untouched venue control reached the domain %d time(s) -- somebody was "+
			"hired into a venue nobody picked", n)
	}
	if !strings.Contains(htmlOf(t, rec), "Choose the venue this person works at") {
		t.Error("the refusal does not name the field")
	}
}

// selectBlock returns the markup of ONE <select> by id, so an assertion about a
// control cannot be satisfied by a different control on the same page.
func selectBlock(t *testing.T, body, id string) string {
	t.Helper()
	marker := `id="` + id + `"`
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("no control with id %q on the page", id)
	}
	start := strings.LastIndex(body[:i], "<select")
	if start < 0 {
		t.Fatalf("id %q is not inside a <select>", id)
	}
	end := strings.Index(body[start:], "</select>")
	if end < 0 {
		t.Fatalf("unterminated <select> for id %q", id)
	}
	return body[start : start+end]
}

// findLeak reports how a secret reached the log, or "" if it did not.
//
// 🔴 IT LOOKS FOR MORE THAN THE WHOLE VALUE, BECAUSE THE WHOLE VALUE IS THE ONE FORM
// NOBODY WRITES BY ACCIDENT. An audit defeated the literal-containment version with
// four shapes; the two that matter are the ones a real change would produce:
//
//	a TRUNCATION      `posted.FullName[:12]` -- exactly what a "short context" log
//	                  line looks like, and the most plausible of the four
//	an EMAIL'S LOCAL  strings.Split(email, "@")[0] -- an address minus its domain is
//	                  PART              still the person
//
// Any run of 8 or more characters of the secret counts, which covers both without
// enumerating either. The sentinels are distinctive enough that an 8-character window
// cannot collide with an id, a timestamp or another field's bytes.
//
// BASE64 AND HEX ARE CHECKED TOO. They are not plausible accidents -- the audit said
// so and it is right -- but they cost two lines and they make the claim about the
// VALUE rather than about its spelling.
//
// ⚠️ THE COUNTED LIMIT, SAID PLAINLY: this closes the encodings named above and every
// substring of 8+ characters. It does NOT close an encoding nobody enumerated -- a
// hash, a compressed blob, rot13, a per-character cipher. A test cannot chase every
// transformation of a string, so what is claimed here is exactly what is measured: the
// literal value, its long fragments, and two standard encodings. Anything cleverer
// would be a deliberate act, and §4.7's answer to a deliberate act is review, not a
// scanner.
func findLeak(written, secret string) string {
	if strings.Contains(written, secret) {
		return "the whole value"
	}
	if b := base64.StdEncoding.EncodeToString([]byte(secret)); strings.Contains(written, b) {
		return "a base64 encoding"
	}
	if h := hex.EncodeToString([]byte(secret)); strings.Contains(written, h) {
		return "a hex encoding"
	}
	// FRAGMENTS. 8 is short enough to catch a truncation and long enough that a match
	// is not a coincidence.
	const minRun = 8
	for start := 0; start+minRun <= len(secret); start++ {
		for end := start + minRun; end <= len(secret); end++ {
			if strings.Contains(written, secret[start:end]) {
				return "a " + strconv.Itoa(end-start) + "-character fragment"
			}
		}
	}
	return ""
}

// anchorFor returns the <a …> tag whose visible text is label, so an assertion about a
// link cannot be satisfied by a different link that happens to share its href.
func anchorFor(t *testing.T, body, label string) string {
	t.Helper()
	i := strings.Index(body, ">"+label+"<")
	if i < 0 {
		t.Fatalf("no element with the text %q on the page", label)
	}
	start := strings.LastIndex(body[:i], "<a ")
	if start < 0 {
		t.Fatalf("the text %q is not inside an anchor", label)
	}
	return body[start : i+1]
}

// docketBlock returns the <section class="docket"> that contains marker, so an
// assertion about a section's own card cannot be satisfied by the page chrome.
//
// 🔴 IT EXISTS BECAUSE SCOPING BY TEXT WAS NOT ENOUGH. An audit defeated a text-plus-
// href pair by renaming a navigation tab to the same words. The chrome is outside
// every docket, so a block-scoped assertion is immune to what the rest of the page is
// called -- the same reason selectBlock exists one control over.
func docketBlock(t *testing.T, body, marker string) string {
	t.Helper()
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("no block containing %q on the page", marker)
	}
	start := strings.LastIndex(body[:i], "<section")
	if start < 0 {
		t.Fatalf("%q is not inside a <section>", marker)
	}
	end := strings.Index(body[start:], "</section>")
	if end < 0 {
		t.Fatalf("unterminated <section> around %q", marker)
	}
	return body[start : start+end]
}

// 🔴 A4/A5/A8: THE FORM'S ACCESSIBILITY AND ITS TWO EXPLANATORY SENTENCES.
//
// Each of these was CLAIMED by a comment and held by nothing. An audit deleted all
// five <label for> attributes, then the citext sentence, then the heading, and
// internal/handler stayed green each time -- while staffform.templ's header says
// "every control has a <label for>" and the citext note says it is on the screen
// "rather than only in the refusal".
//
// ⚠️ THE LABEL GAP IS OLDER AND WIDER THAN THIS CARD, and the audit said so: no test
// anywhere in the panel asserts <label for> on any form. M6-13 did not create the
// gap; it created the CLAIM. This closes it for this form only, which is the scope
// the claim has -- widening it to the other forms is a backlog line, not something to
// smuggle in at round 10.
//
// A LABEL IS NOT aria-describedby. The required Name field with its labels removed is
// still described, and still has no ACCESSIBLE NAME -- a screen reader announces the
// description and nothing to call the control.
func TestEmployeeAdd_TheFormIsLabelledAndExplainsItself(t *testing.T) {
	b := panelBrowserWithActions(t, &fakeStaff{}, &fakeInviter{})
	body := htmlOf(t, b.do(http.MethodGet, employeesHref+"?"+addParam+"="+addOpenValue, nil))
	form := docketBlock(t, body, "New person")

	// A4: every control the form ships carries a label bound to its id.
	for _, id := range []string{
		"add-full-name", "add-location", "add-department", "add-role", "add-email",
	} {
		if !strings.Contains(form, `for="`+id+`"`) {
			t.Errorf("🔴 the control %q has no <label for>, so it has no accessible name -- "+
				"aria-describedby is a DESCRIPTION and does not name a control", id)
		}
		if !strings.Contains(form, `id="`+id+`"`) {
			t.Errorf("the form has a label for %q but no control with that id", id)
		}
	}

	// A8: the heading names what the card is for.
	if !strings.Contains(form, "New person") {
		t.Error("the add card has lost its heading")
	}

	// A5: the citext sentence is ON THE SCREEN, not only in the refusal. Half of this
	// was already pinned (removing it from the refusal turns two tests red); this is
	// the other half, which the comment claimed and nothing held.
	if !strings.Contains(strings.ToLower(form), "capitals make no difference") {
		t.Error("🔴 the email field no longer says that capitals make no difference. The " +
			"column is citext, so a manager who hits the duplicate refusal will retype the " +
			"address with a different capital and be refused again with no idea why.")
	}

	// A8: the closed control explains the next step before it is pressed.
	closed := htmlOf(t, b.do(http.MethodGet, employeesHref, nil))
	if !strings.Contains(closed, "you send them their activation link afterwards") {
		t.Error("the closed Add control no longer says that adding does not invite")
	}
}
