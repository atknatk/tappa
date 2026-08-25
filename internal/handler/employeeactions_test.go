package handler

// employeeactions_test.go -- M6-05 phase B's unit-level nets.
//
// WHAT IS HERE AND WHAT IS IN employeeactions_db_test.go. A fake accepts whatever it
// is told, so it can only ever prove things about THIS layer: which command the
// handler builds from a request, where each field of that command came from, which
// sentence a refusal turns into, and what the rendered page offers. §4.5 (a foreign
// id writes nothing), the audit row sharing a transaction, and "the next tap is
// rejected AND recorded" are properties of the database and are measured against
// real Postgres in the _db_ file.

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/invite"
)

// panelBrowserWithActions is panelBrowser with the two write sides under the
// caller's control.
func panelBrowserWithActions(t *testing.T, staff *fakeStaff, invites *fakeInviter) *browser {
	t.Helper()
	return panelBrowserWithActionsAndTrail(t, staff, invites, &fakeTrail{})
}

// panelBrowserWithLedger drives the section against a CALLER-SUPPLIED ledger, so a
// test can model a business in a state the default fixture is not in -- above all a
// business with NO VENUE, which is the state the add form must refuse to render for
// (M6-13). Without this every add test would silently exercise the same one business.
func panelBrowserWithLedger(t *testing.T, records *fakeLedger, staff *fakeStaff, invites *fakeInviter) *browser {
	t.Helper()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: "owner", FullName: "KF Owner",
		}, nil
	}}
	b := newBrowser(t, newAdminRouterWithActions(t, admins, &fakeTrail{}, records, &fakeReviewer{}, staff, invites))
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

// panelBrowserWithLogsAndLedger captures what the section writes to its logger, with a
// CALLER-SUPPLIED ledger so a test can drive the outcomes that only happen when a READ
// fails. It returns the buffer rather than a matcher: the assertion belongs in the
// test, and a helper that decided what counted as a secret would be a second place to
// get that wrong.
//
// 🔴 THE LEDGER IS A PARAMETER BECAUSE THE §4.7 TABLE COULD NOT REACH ONE OF ITS OWN
// OUTCOMES WITHOUT IT. Every refusal path re-renders through employeesView, which logs
// when a.ledger.Roster fails -- and with a fixed fake that never fails, that line was
// unreachable and a leak on it stayed green through three packages. An audit measured
// exactly that. A net whose fixture cannot produce an outcome is not covering it,
// however the comment above it is worded.
//
// ⚠️ THERE IS NO LEDGER-LESS CONVENIENCE WRAPPER. One existed for a round and
// staticcheck found it unused the moment the last caller moved over -- a helper kept
// "in case" is the same dead weight as an unreachable branch, and this section deleted
// one of those in round 2 rather than finding it a caller.
func panelBrowserWithLogsAndLedger(t *testing.T, records *fakeLedger, staff *fakeStaff, invites *fakeInviter) (*browser, *strings.Builder) {
	t.Helper()
	var buf strings.Builder
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: "owner", FullName: "KF Owner",
		}, nil
	}}
	h, err := NewAdminAuth(admins, &fakeTrail{}, records, records, &fakeReviewer{},
		staff, invites, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(),
		newFakeBooks(), newFakeTexts(), newFakeAccount(), nil, adminTestConfig(),
		slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b, &buf
}

func panelBrowserWithActionsAndTrail(t *testing.T, staff *fakeStaff, invites *fakeInviter, trail *fakeTrail) *browser {
	t.Helper()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: "owner", FullName: "KF Owner",
		}, nil
	}}
	b := newBrowser(t, newAdminRouterWithActions(t, admins, trail, newFakeLedger(), &fakeReviewer{}, staff, invites))
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

// deactivateConfirmed walks the confirmation step and returns the form values a
// deactivation must carry.
//
// 🔴 IT EXISTS BECAUSE THE GATE IS REAL (user decision, 2026-08-08). Every test that
// deactivates somebody has to pass through the warning screen, exactly as a manager
// does — which is the point: a helper that forged the token would be a test proving
// the product works against a door it propped open itself.
func deactivateConfirmed(t *testing.T, b *browser, employeeID uuid.UUID, extra url.Values) url.Values {
	t.Helper()
	page := htmlOf(t, b.do(http.MethodGet,
		managedRosterHref(employeeID)+"&confirm=deactivate", nil))
	form := url.Values{"id": {employeeID.String()}, confirmField: {confirmTokenIn(t, page)}}
	for k, vs := range extra {
		for _, v := range vs {
			form.Add(k, v)
		}
	}
	return form
}

// managedRosterHref is the section with one person's action card open.
func managedRosterHref(employeeID uuid.UUID) string {
	return employeesHref + "?manage=" + employeeID.String()
}

// TestEmployeesSection_EveryControlLeadsSomewhereThatExists REPLACES phase A's
// TestEmployeesSection_OffersNoWriteAtAll, which asserted the four actions were
// ABSENT.
//
// 🔴 THE PROPERTY THE OLD TEST PROTECTED IS THE ONE KEPT: a control on this screen
// must not lead somewhere the server does not serve. Phase A held it by having no
// controls; phase B holds it by DRIVING every control the page offers through the
// SAME router the page came from. Nothing here is a list — the form actions and the
// links are read off the rendered markup, so a button wired to a route somebody
// meant to add later is a red test rather than a 404 a manager finds.
//
// TWO SHAPES, because a page can write without a button (a form with no submit is
// still submittable) and can link without a form.
func TestEmployeesSection_EveryControlLeadsSomewhereThatExists(t *testing.T) {
	employeeID := uuid.New()
	b := panelBrowserWithActions(t, &fakeStaff{}, &fakeInviter{})

	rec := b.do(http.MethodGet, managedRosterHref(employeeID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d, want 200", managedRosterHref(employeeID), rec.Code)
	}
	body := htmlOf(t, rec)
	// THE DESTRUCTIVE ACTION IS TWO STEPS, so its form is not on the first one. Both
	// pages are read: a test that looked only at the card would report the deactivate
	// route as unoffered, and one that looked only at the confirmation would miss the
	// other two.
	confirmed := htmlOf(t, b.do(http.MethodGet, managedRosterHref(employeeID)+"&confirm=deactivate", nil))
	// 🔴 THE ADD FORM IS BEHIND A DISCLOSURE TOO (M6-13), so it needs its own page in
	// this net. Without this line the newest control on the section would be the only
	// one nothing drove through the router — which is precisely how M6-04 shipped a
	// form target that 404ed with the whole package green.
	adding := htmlOf(t, b.do(http.MethodGet, managedRosterHref(employeeID)+"&add=new", nil))

	// 1. THE POSTING FORMS ARE EXACTLY THE ONES THIS PANEL REGISTERS. Sign-out comes
	// from the shared chrome; the rest are this section's actions.
	got := map[string]bool{}
	for _, page := range []string{body, confirmed, adding} {
		for _, m := range postFormRE.FindAllStringSubmatch(page, -1) {
			if c := attrValueRE("action").FindStringSubmatch(m[1]); len(c) == 2 {
				got[c[1]] = true
			}
		}
	}
	want := map[string]bool{
		"/admin/logout":        true,
		employeeAddHref:        true,
		employeeInviteHref:     true,
		employeeDeactivateHref: true,
		employeeMoveHref:       true,
	}
	for action := range want {
		if !got[action] {
			t.Errorf("the managed employees section does not post to %s; the card is "+
				"supposed to offer it", action)
		}
	}
	for action := range got {
		if !want[action] {
			t.Errorf("the employees section posts to %s, which is not one of this "+
				"panel's routes", action)
		}
	}

	// 2. EVERY POSTING TARGET IS A REGISTERED ROUTE. A 404 or a 405 here is the
	// failure this test exists for: a form that looks like it works.
	for action := range got {
		if action == "/admin/logout" {
			continue // driving it would end the session the rest of this test uses
		}
		rec := b.do(http.MethodPost, action, url.Values{"id": {employeeID.String()}})
		switch rec.Code {
		case http.StatusOK, http.StatusSeeOther:
		default:
			t.Errorf("POST %s answered %d; a form target that is not served is a control "+
				"that lies about what pressing it does", action, rec.Code)
		}
	}

	// 3. EVERY SAME-ORIGIN LINK ON EITHER PAGE RESOLVES THROUGH THE SAME ROUTER.
	for _, page := range []string{body, confirmed} {
		for _, href := range sameOriginHrefs(page) {
			rec := b.do(http.MethodGet, href, nil)
			switch rec.Code {
			case http.StatusOK, http.StatusSeeOther:
			default:
				t.Errorf("the employees section offers a link to %s, which answers %d", href, rec.Code)
			}
		}
	}
}

// TestEmployeesSection_OffersNoActionUntilAPersonIsNamed. The plain roster is still
// a READ: the three action forms appear only when a person's card is open, so a
// manager cannot deactivate somebody by pressing a control they walked past.
func TestEmployeesSection_OffersNoActionUntilAPersonIsNamed(t *testing.T) {
	// A ROSTER WITH SOMEBODY ON IT, or the assertion below would pass over an empty
	// page — the degenerate-fixture shape this repository has been bitten by twice.
	records := newFakeLedger()
	records.roster = ledger.RosterScreen{
		RosterPage: ledger.RosterPage{
			Queried: true,
			People: []ledger.Person{{
				ID: uuid.New(), Name: "Maria Borg", Status: ledger.StatusActive,
				LocationName: "KF St Julians",
			}},
		},
	}
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: "owner", FullName: "KF Owner",
		}, nil
	}}
	b := newBrowser(t, newAdminRouterWithActions(t, admins, &fakeTrail{}, records,
		&fakeReviewer{}, &fakeStaff{}, &fakeInviter{}))
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	body := htmlOf(t, b.do(http.MethodGet, employeesHref, nil))

	for _, m := range postFormRE.FindAllStringSubmatch(body, -1) {
		if c := attrValueRE("action").FindStringSubmatch(m[1]); len(c) == 2 && c[1] != "/admin/logout" {
			t.Errorf("the unmanaged roster posts to %s; every write on this screen belongs "+
				"to one named person's card", c[1])
		}
	}
	// AND THE ROWS STILL OFFER THE WAY IN, or the section would be unusable and this
	// test would be passing over a screen with no actions at all.
	if !strings.Contains(body, "?manage=") {
		t.Error("no row offers a way to manage anybody; the assertion above is passing " +
			"over a page with no actions on it")
	}
}

// TestEmployeesSection_OffersNoActionTheServerWouldRefuse is the net for the two
// conditions that decide whether a control is rendered at all — and it exists because
// an audit measured that NOTHING held them.
//
// 🔴 BOTH `CanInvite: true` AND `CanDeactivate: true` LEFT THE WHOLE PACKAGE GREEN
// (measured, -race, two separate full runs). The product was right — a deactivated
// person's card really did show neither control — but the property was held by
// nobody, and the test named as holding it
// (TestEmployeesSection_EveryControlLeadsSomewhereThatExists) checks something
// STRICTLY WEAKER: that a form's target is a served route. It never renders a
// deactivated person, because the fake's default person is active.
//
// ⚠️ THIS IS THE PROPERTY THE RETIRED PHASE-A TEST USED TO HOLD. "The screen offers
// nothing the server would refuse" survived phase A by having no controls at all;
// here it survives by rendering them CONDITIONALLY, so the conditions need a net of
// their own.
//
// THE TWO REFUSALS ARE THE DATABASE'S, NOT THIS SCREEN'S PREFERENCE: an invitation
// for a deactivated employee cannot be spent (db/queries/invites.sql refuses it, and
// that refusal is a deliberate barrier around manager-only deactivation), and a
// second deactivation writes nothing (the statement's own status predicate).
func TestEmployeesSection_OffersNoActionTheServerWouldRefuse(t *testing.T) {
	staff := &fakeStaff{person: &tenant.Person{
		Name: "Paul Spiteri", Status: ledger.StatusDeactivated,
		LocationID: panelTestLocation, LocationName: "KF St Julians",
	}}
	b := panelBrowserWithActions(t, staff, &fakeInviter{})
	employeeID := uuid.New()

	body := htmlOf(t, b.do(http.MethodGet, managedRosterHref(employeeID), nil))
	// ANTI-VACUITY: the card has to BE there, or every absence below is the absence
	// of a page rather than of a control.
	if !strings.Contains(body, "Managing") || !strings.Contains(body, "Paul Spiteri") {
		t.Fatalf("the action card did not render for a deactivated person; this test " +
			"would then be measuring an empty page")
	}
	// AND THE ONE CONTROL THAT STAYS is the positive control: a deactivated person can
	// still be MOVED (§4.6 — correcting where somebody worked must stay possible).
	if !strings.Contains(body, employeeMoveHref) {
		t.Fatalf("the card offers no move form for a deactivated person; §4.6 wants that " +
			"repair to stay possible, and without it the absences below prove only that " +
			"the card is empty")
	}

	if strings.Contains(body, employeeInviteHref) {
		t.Error("the card offers an invitation to a DEACTIVATED person. The consuming " +
			"statement refuses to spend it (db/queries/invites.sql), so the button would " +
			"mint a credential that can never work.")
	}
	if strings.Contains(body, "confirm=deactivate") {
		t.Error("the card offers to deactivate somebody who is ALREADY deactivated. " +
			"Pressing it writes nothing, and offering it says otherwise.")
	}
	// 🔴 AND THE SCREEN SAYS WHY, rather than simply missing two controls. A control
	// that is absent with no explanation reads as a bug; both of these are rules the
	// database enforces, so the page states them.
	for _, want := range []string{
		"No invitation can be sent to somebody who has been deactivated",
		"Their taps are already being refused",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the card does not say %q; a missing control with no sentence beside "+
				"it teaches the manager nothing", want)
		}
	}

	// THE POSITIVE CONTROL FOR THE WHOLE TEST: an ACTIVE person's card DOES offer both.
	// Without it, a template that rendered neither control for anybody would pass every
	// assertion above.
	active := panelBrowserWithActions(t, &fakeStaff{}, &fakeInviter{})
	live := htmlOf(t, active.do(http.MethodGet, managedRosterHref(uuid.New()), nil))
	if !strings.Contains(live, employeeInviteHref) || !strings.Contains(live, "confirm=deactivate") {
		t.Error("an ACTIVE person's card offers no invitation or no deactivation, so the " +
			"absences above are not evidence of anything")
	}
}

// TestEmployeeDeactivate_TheConfirmationStepIsENFORCED is the INVERTED ledger:
// until 2026-08-08 this test asserted that an unconfirmed POST deactivated somebody,
// and its comment said to invert it when a gate landed. This is that.
//
// 🔴 WHAT IT USED TO MEASURE. A security audit posted straight at the route without
// ever loading the warning: 303, and `status = 'deactivated'`. Read with ADR 0010 —
// nothing reactivates anybody — one of the product's three irreversible actions (the other two are removing a venue and removing a department, M6-06 phase A) was one
// request away with its warning never rendered. The comment claiming otherwise was
// the worse half: a reader would have believed the gate existed.
//
// THE BRANCH COUNT IS NOT WRITTEN HERE, deliberately. Three files carried three
// different numbers for it (four / five / five-plus-a-control) and an audit
// reproduced the drift with `grep -c "t.Run("`. What matters is not how many there
// are but what they cover: each failure mode the gate creates owes the manager a
// sentence (§4.6), and a gate with no positive control is a gate that might simply
// refuse everything — so both are below, and counting them is the reader's job.
func TestEmployeeDeactivate_TheConfirmationStepIsENFORCED(t *testing.T) {
	employeeID := uuid.New()

	t.Run("an unconfirmed POST writes nothing", func(t *testing.T) {
		staff := &fakeStaff{}
		b := panelBrowserWithActions(t, staff, &fakeInviter{})
		rec := b.do(http.MethodPost, employeeDeactivateHref, url.Values{"id": {employeeID.String()}})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("answered %d, want 303", rec.Code)
		}
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "problem=confirm-required") {
			t.Errorf("redirected to %q, want problem=confirm-required", loc)
		}
		if d, _ := staff.commands(); len(d) != 0 {
			t.Errorf("an unconfirmed POST reached the domain %d time(s); nothing may be "+
				"written before the warning has been served", len(d))
		}
		// AND THE MANAGER READS WHY. A refusal carried in a query string that no
		// template renders is a silent refusal with extra steps.
		body := htmlOf(t, b.do(http.MethodGet, rec.Header().Get("Location"), nil))
		if !strings.Contains(body, "needs the warning screen first") {
			t.Error("the page does not explain why nothing happened")
		}
	})

	t.Run("the confirmed flow works", func(t *testing.T) {
		// POSITIVE CONTROL. Without it a gate that refused everything would satisfy
		// every other branch here.
		staff := &fakeStaff{}
		b := panelBrowserWithActions(t, staff, &fakeInviter{})
		confirming := htmlOf(t, b.do(http.MethodGet,
			managedRosterHref(employeeID)+"&confirm=deactivate", nil))
		token := confirmTokenIn(t, confirming)

		rec := b.do(http.MethodPost, employeeDeactivateHref, url.Values{
			"id": {employeeID.String()}, confirmField: {token},
		})
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "done=deactivated") {
			t.Fatalf("a CONFIRMED deactivation redirected to %q, want done=deactivated", loc)
		}
		if d, _ := staff.commands(); len(d) != 1 {
			t.Fatalf("the domain saw %d deactivation(s) from a confirmed POST, want 1", len(d))
		}

		// A REPLAY BY AN ORDINARY BROWSER IS REFUSED, because the response cleared the
		// cookie and the browser honoured it.
		//
		// ⚠️ THAT IS A STATEMENT ABOUT THE BROWSER, NOT ABOUT THE SERVER, and this
		// comment is the correction: the assertion below passes because THIS TEST
		// HELPER implements cookie clearing. What the server keeps is measured
		// separately, by a client that does not cooperate — see
		// TestEmployeeDeactivate_TheOneShotDependsOnTheClient.
		again := b.do(http.MethodPost, employeeDeactivateHref, url.Values{
			"id": {employeeID.String()}, confirmField: {token},
		})
		if loc := again.Header().Get("Location"); !strings.Contains(loc, "problem=confirm-required") {
			t.Errorf("a REPLAYED confirmation redirected to %q; a browser that honoured "+
				"the cleared cookie must be refused", loc)
		}
		if d, _ := staff.commands(); len(d) != 1 {
			t.Errorf("a replayed confirmation wrote a second deactivation (%d in total)", len(d))
		}
	})

	t.Run("a confirmation for somebody else is refused", func(t *testing.T) {
		// THE SECOND-TAB CASE, which is the failure mode the BINDING exists for: one
		// cookie per browser, so opening another person's warning overwrites the first
		// token — and without the binding the older tab's button would deactivate the
		// wrong human being.
		staff := &fakeStaff{}
		b := panelBrowserWithActions(t, staff, &fakeInviter{})
		other := uuid.New()
		confirming := htmlOf(t, b.do(http.MethodGet,
			managedRosterHref(other)+"&confirm=deactivate", nil))
		token := confirmTokenIn(t, confirming)

		rec := b.do(http.MethodPost, employeeDeactivateHref, url.Values{
			"id": {employeeID.String()}, confirmField: {token},
		})
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "problem=confirm-stale") {
			t.Errorf("redirected to %q, want problem=confirm-stale", loc)
		}
		if d, _ := staff.commands(); len(d) != 0 {
			t.Errorf("a confirmation bound to another person deactivated somebody (%d)", len(d))
		}
		body := htmlOf(t, b.do(http.MethodGet, rec.Header().Get("Location"), nil))
		if !strings.Contains(body, "confirming a different person") {
			t.Error("the page does not explain that the confirmation belonged to somebody else")
		}
	})

	t.Run("a forged token is refused", func(t *testing.T) {
		staff := &fakeStaff{}
		b := panelBrowserWithActions(t, staff, &fakeInviter{})
		b.do(http.MethodGet, managedRosterHref(employeeID)+"&confirm=deactivate", nil)
		rec := b.do(http.MethodPost, employeeDeactivateHref, url.Values{
			"id": {employeeID.String()}, confirmField: {"not-the-token"},
		})
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "problem=confirm-required") {
			t.Errorf("a forged token redirected to %q", loc)
		}
		if d, _ := staff.commands(); len(d) != 0 {
			t.Errorf("a forged token deactivated somebody (%d)", len(d))
		}
	})

	t.Run("the warning is still on the screen", func(t *testing.T) {
		// The gate is the mechanism; the SENTENCE is what the gate exists to guarantee
		// gets read. Asserting only the mechanism would let the warning be deleted.
		b := panelBrowserWithActions(t, &fakeStaff{}, &fakeInviter{})
		body := htmlOf(t, b.do(http.MethodGet,
			managedRosterHref(employeeID)+"&confirm=deactivate", nil))
		if !strings.Contains(body, "Tappa has no way to undo it") {
			t.Error("the confirmation screen no longer says the action cannot be undone")
		}
	})
}

// TestEmployeeDeactivate_TheOneShotDependsOnTheClient measures what the SERVER keeps,
// which is nothing — and it exists because the test that claimed otherwise was
// measuring its own helper.
//
// 🔴 THE DEFECT WAS IN THE NET, NOT IN THE PRODUCT. The replay assertion in the test
// above drives `browser`, and that helper deletes a cookie when a response clears it.
// So "the value is spent" was really "this client agreed to forget it" — the same
// class as the closed reason-list activate_test.go warns about, one size down. A
// client that re-prints the cookie before every POST spends ONE minted confirmation
// as often as it likes, for the whole TTL:
//
//	attempt 1 -> 303 ?done=deactivated
//	attempt 2 -> 303 ?done=deactivated
//	attempt 3 -> 303 ?done=deactivated
//
// 🔴 WHY IT IS NOT CLOSED, AND WHY THAT IS A COUNTED LIMIT RATHER THAN A SHRUG.
// Server-side one-shot needs server-side state — a table or a column, i.e.
// infrastructure — and the measured gain is ZERO: the actor is the panel's own
// signed-in operator, who can mint a fresh confirmation with one GET anyway, and the
// second and later spends write NOTHING (the assertion at the end of this test, and
// its database twin). What the MAC does hold is the property ADR 0010 leans on: the
// server rendered the warning, for this person, in this session, inside the window.
// What it does not hold is single use.
func TestEmployeeDeactivate_TheOneShotDependsOnTheClient(t *testing.T) {
	staff := &fakeStaff{}
	b := panelBrowserWithActions(t, staff, &fakeInviter{})
	employeeID := uuid.New()

	page := htmlOf(t, b.do(http.MethodGet, managedRosterHref(employeeID)+"&confirm=deactivate", nil))
	token := confirmTokenIn(t, page)

	const spends = 3
	for i := 0; i < spends; i++ {
		// THE UNCOOPERATIVE CLIENT: it puts the cookie back before every request, which
		// is all a script has to do. b.do would otherwise honour the Set-Cookie that
		// clears it.
		b.cookies[adminConfirmCookieName] = token
		rec := b.do(http.MethodPost, employeeDeactivateHref, url.Values{
			"id": {employeeID.String()}, confirmField: {token},
		})
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "done=deactivated") {
			t.Fatalf("spend %d redirected to %q; this test measures a value the server "+
				"accepts repeatedly, so a refusal here means the behaviour changed and "+
				"the limit written above (and in docs/adr/0010) is out of date", i+1, loc)
		}
	}

	d, _ := staff.commands()
	if len(d) != spends {
		t.Errorf("one minted confirmation reached the domain %d time(s) from %d spends. "+
			"If this is now 1, a server-side ledger was added: delete this test, and "+
			"correct the limit in docs/adr/0010 and in deactivateconfirm.go, which both "+
			"say single use is NOT held.", len(d), spends)
	}

	// 🔴 AND THE REASON IT IS HARMLESS IS ASSERTED, NOT ASSUMED. Every spend after the
	// first meets the statement's own `status <> 'deactivated'` predicate, so the
	// domain answers ErrAlreadyDeactivated and writes neither a second employees row
	// nor a second audit row. The database twin of this is
	// TestPanelEmployeesDB_ARepeatedConfirmationWritesOnlyOnce.
	already := &fakeStaff{deactivateErr: tenant.ErrAlreadyDeactivated}
	b2 := panelBrowserWithActions(t, already, &fakeInviter{})
	page2 := htmlOf(t, b2.do(http.MethodGet, managedRosterHref(employeeID)+"&confirm=deactivate", nil))
	token2 := confirmTokenIn(t, page2)
	b2.cookies[adminConfirmCookieName] = token2
	rec := b2.do(http.MethodPost, employeeDeactivateHref, url.Values{
		"id": {employeeID.String()}, confirmField: {token2},
	})
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "problem=already-deactivated") {
		t.Errorf("a repeated spend against an already-deactivated person redirected to "+
			"%q, want problem=already-deactivated", loc)
	}
}

// TestEmployeeDeactivate_TheConfirmationCannotBeForged is the ATTACK TABLE, and it
// exists because the first version of this gate was forged in two lines.
//
// 🔴 WHAT THE AUDIT DID. The gate was a pure double-submit cookie — no key, no server
// record — so a client that printed its own cookie and echoed the same string
// deactivated somebody without ever loading the warning:
//
//	cookie: tappa_admin_confirm=attacker-chosen-value.<employee id>
//	POST   ... id=<employee id>&confirm_token=attacker-chosen-value  -> 303, deactivated
//
// The comment beside it claimed it was logincontext.go's shape "deliberately", and
// that was the whole defect: a shape is not a mechanism. The value is now an HMAC
// under a key derived from the server's secret, bound to one employee AND one panel
// session, expiring against the server clock.
//
// NINE ATTACKS: the eight an audit ran against the old gate, plus the forgery that
// beat it. Every one must be refused, nothing may be written, and the positive
// control must still pass — a gate that refuses everything is not a gate.
func TestEmployeeDeactivate_TheConfirmationCannotBeForged(t *testing.T) {
	employeeID := uuid.New()
	other := uuid.New()

	// A GENUINE value for another employee, minted through the product, for the
	// "somebody else's confirmation" arm.
	genuineOther := func(t *testing.T, b *browser) string {
		t.Helper()
		page := htmlOf(t, b.do(http.MethodGet, managedRosterHref(other)+"&confirm=deactivate", nil))
		return confirmTokenIn(t, page)
	}

	attacks := []struct {
		name string
		// build returns the form value and, when non-empty, a cookie the attacker
		// sets for themselves.
		build func(t *testing.T, b *browser) (token, cookie string)
		want  string
	}{
		{"no confirmation at all", func(*testing.T, *browser) (string, string) {
			return "", ""
		}, "confirm-required"},
		{"a guessed token, no cookie", func(*testing.T, *browser) (string, string) {
			return "guessed-value", ""
		}, "confirm-required"},
		{"a guessed token WITH a matching cookie", func(*testing.T, *browser) (string, string) {
			// 🔴 THE FORGERY THAT BEAT THE OLD GATE. Both halves are the attacker's.
			return "guessed-value", "guessed-value"
		}, "confirm-required"},
		{"a self-minted cookie in the OLD format", func(*testing.T, *browser) (string, string) {
			v := "attacker-chosen-value." + employeeID.String()
			return v, v
		}, "confirm-required"},
		{"a genuine token minted for ANOTHER employee", func(t *testing.T, b *browser) (string, string) {
			v := genuineOther(t, b)
			return v, v
		}, "confirm-stale"},
		{"a cookie with no separator", func(*testing.T, *browser) (string, string) {
			return "no-separator-here", "no-separator-here"
		}, "confirm-required"},
		{"an empty token half", func(*testing.T, *browser) (string, string) {
			return ".", "."
		}, "confirm-required"},
		{"a genuine token with one byte removed", func(t *testing.T, b *browser) (string, string) {
			v := genuineOther(t, b)
			return v[:len(v)-1], v[:len(v)-1]
		}, "confirm-required"},
		{"a genuine token re-signed under a guessed key", func(t *testing.T, b *browser) (string, string) {
			// The payload is readable — it is base64, not a secret — so an attacker can
			// rebuild it for the employee they want. What they cannot do is sign it.
			forged, err := adminConfirm{key: []byte("not-the-servers-key"), ttl: adminConfirmTTL}.
				mint(confirmActionDeactivate, employeeID.String(), panelTestSession)
			if err != nil {
				t.Fatalf("building the forgery: %v", err)
			}
			return forged, forged
		}, "confirm-required"},
	}

	for _, tc := range attacks {
		t.Run(tc.name, func(t *testing.T) {
			staff := &fakeStaff{}
			b := panelBrowserWithActions(t, staff, &fakeInviter{})
			token, cookie := tc.build(t, b)
			if cookie != "" {
				b.cookies[adminConfirmCookieName] = cookie
			} else {
				delete(b.cookies, adminConfirmCookieName)
			}
			form := url.Values{"id": {employeeID.String()}}
			if token != "" {
				form.Set(confirmField, token)
			}
			rec := b.do(http.MethodPost, employeeDeactivateHref, form)

			if rec.Code != http.StatusSeeOther {
				t.Fatalf("answered %d, want 303", rec.Code)
			}
			if loc := rec.Header().Get("Location"); !strings.Contains(loc, "problem="+tc.want) {
				t.Errorf("redirected to %q, want problem=%s", loc, tc.want)
			}
			if d, _ := staff.commands(); len(d) != 0 {
				t.Errorf("the attack reached the domain %d time(s); nothing may be written "+
					"without a confirmation this server minted", len(d))
			}
		})
	}

	// POSITIVE CONTROL, LAST: the honest flow still works. Without it every refusal
	// above is satisfied by a gate that is simply shut.
	t.Run("the honest flow still passes", func(t *testing.T) {
		staff := &fakeStaff{}
		b := panelBrowserWithActions(t, staff, &fakeInviter{})
		rec := b.do(http.MethodPost, employeeDeactivateHref,
			deactivateConfirmed(t, b, employeeID, nil))
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "done=deactivated") {
			t.Fatalf("the confirmed flow redirected to %q", loc)
		}
		if d, _ := staff.commands(); len(d) != 1 {
			t.Fatalf("the confirmed flow reached the domain %d time(s), want 1", len(d))
		}
	})
}

// TestAdminConfirm_IsBoundAndExpires drives the value itself, where the HTTP layer
// cannot reach: a confirmation from ANOTHER SESSION, and one that has aged past the
// window on the SERVER clock rather than in a browser.
func TestAdminConfirm_IsBoundAndExpires(t *testing.T) {
	c, err := newAdminConfirm(adminTestConfig())
	if err != nil {
		t.Fatalf("newAdminConfirm: %v", err)
	}
	employee, session := uuid.New(), uuid.New()

	token, err := c.mint(confirmActionDeactivate, employee.String(), session)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	// POSITIVE CONTROL FIRST.
	if err := c.parse(token, confirmActionDeactivate, employee.String(), session); err != nil {
		t.Fatalf("a freshly minted confirmation was refused: %v", err)
	}

	// 🔴 ANOTHER SESSION. The same operator signing in again, or a different admin of
	// the same business, cannot spend a confirmation minted elsewhere. It reads as
	// "not confirmed" rather than as "another employee" on purpose: naming which
	// binding was missed would describe the mechanism to whoever is probing it.
	if err := c.parse(token, confirmActionDeactivate, employee.String(), uuid.New()); !errors.Is(err, errConfirmInvalid) {
		t.Errorf("a confirmation from another session returned %v, want errConfirmInvalid", err)
	}
	// ANOTHER EMPLOYEE — genuine, this session, so it earns its own sentence.
	if err := c.parse(token, confirmActionDeactivate, uuid.NewString(), session); !errors.Is(err, errConfirmOtherPerson) {
		t.Errorf("a confirmation for another employee returned %v, want errConfirmOtherPerson", err)
	}

	// 🔴 EXPIRY IS THE SERVER'S, NOT THE BROWSER'S. The cookie's Max-Age is a hint; a
	// client that kept sending the value past the window is refused anyway.
	aged := c
	aged.now = func() time.Time { return time.Now().Add(adminConfirmTTL + time.Second) }
	if err := aged.parse(token, confirmActionDeactivate, employee.String(), session); !errors.Is(err, errConfirmInvalid) {
		t.Errorf("a confirmation past the TTL returned %v, want errConfirmInvalid", err)
	}
	// AND THE BOUNDARY IS NOT OFF BY A WINDOW: one second INSIDE it still passes.
	fresh := c
	fresh.now = func() time.Time { return time.Now().Add(adminConfirmTTL - time.Second) }
	if err := fresh.parse(token, confirmActionDeactivate, employee.String(), session); err != nil {
		t.Errorf("a confirmation one second inside the window was refused: %v", err)
	}

	// 🔴 AND THE FUTURE IS BOUNDED TOO. An audit passed a confirmation stamped a YEAR
	// ahead through the earlier version, which tested only `now > issued + ttl`. No
	// attacker can reach it — issuedAt is this process's clock and sits inside the MAC
	// — but the guard is what stops a backwards clock step (an NTP correction, a
	// suspended VM) from silently widening the window.
	backwards := c
	backwards.now = func() time.Time { return time.Now().Add(-adminConfirmFutureSkew - time.Minute) }
	if err := backwards.parse(token, confirmActionDeactivate, employee.String(), session); !errors.Is(err, errConfirmInvalid) {
		t.Errorf("a confirmation from beyond the future skew returned %v, want errConfirmInvalid", err)
	}
	// The tolerated direction still works, or the guard would refuse a legitimate
	// second of clock jitter.
	jitter := c
	jitter.now = func() time.Time { return time.Now().Add(-adminConfirmFutureSkew + time.Second) }
	if err := jitter.parse(token, confirmActionDeactivate, employee.String(), session); err != nil {
		t.Errorf("a confirmation inside the tolerated skew was refused: %v", err)
	}

	// 🔴 THE ZERO VALUE IS AN ERROR, NEVER A DEFAULT. A zero adminConfirm would sign
	// under an empty key — a MAC anybody can compute — so both halves refuse.
	var zero adminConfirm
	if _, err := zero.mint(confirmActionDeactivate, employee.String(), session); err == nil {
		t.Error("a zero adminConfirm minted a confirmation; it must refuse rather than " +
			"sign under an empty key")
	}
	if err := zero.parse(token, confirmActionDeactivate, employee.String(), session); err == nil {
		t.Error("a zero adminConfirm accepted a confirmation")
	}
	// AND A VALUE SIGNED UNDER A DIFFERENT KEY IS REFUSED, which is the property the
	// whole file exists for.
	otherKey, err := newAdminConfirm(&config.Config{
		Env: config.EnvDev, BaseURL: testBaseURL,
		SessionHMACKey: []byte("ffffffffffffffffffffffffffffffff"),
	})
	if err != nil {
		t.Fatalf("newAdminConfirm(other key): %v", err)
	}
	foreign, err := otherKey.mint(confirmActionDeactivate, employee.String(), session)
	if err != nil {
		t.Fatalf("mint under the other key: %v", err)
	}
	if err := c.parse(foreign, confirmActionDeactivate, employee.String(), session); !errors.Is(err, errConfirmInvalid) {
		t.Errorf("a confirmation signed under a DIFFERENT key was accepted (%v)", err)
	}
}

// confirmTokenIn lifts the signed confirmation value off the rendered form. It is
// a value the page is SUPPOSED to expose — its twin is the HttpOnly cookie.
func confirmTokenIn(t *testing.T, body string) string {
	t.Helper()
	re := regexp.MustCompile(`(?is)<input[^>]*name="` + regexp.QuoteMeta(confirmField) +
		`"[^>]*value="([^"]*)"`)
	m := re.FindStringSubmatch(body)
	if m == nil || m[1] == "" {
		t.Fatal("the confirmation screen renders no confirmation token; the gate cannot " +
			"be passed and every assertion about it would be vacuous")
	}
	return m[1]
}

// TestEmployeeActions_TheTenantAndTheActorComeFromTheSession is §4.5 and §4.7 at
// this boundary.
//
// 🔴 THE REQUEST IS ALLOWED TO ASK FOR ANYTHING; the command must carry the SESSION's
// tenant and the SESSION's admin regardless. A body claiming another tenant is not a
// theoretical shape — it is one line of curl — and the only reason it cannot work is
// that neither value is read from it.
func TestEmployeeActions_TheTenantAndTheActorComeFromTheSession(t *testing.T) {
	staff := &fakeStaff{}
	b := panelBrowserWithActions(t, staff, &fakeInviter{})
	employeeID := uuid.New()
	hostileTenant, hostileActor := uuid.New(), uuid.New()

	hostile := url.Values{
		"tenant_id": {hostileTenant.String()},
		"actor_id":  {hostileActor.String()},
		// The move needs a destination; the same hostile fields ride along.
		"to_location": {panelTestLocation.String()},
	}
	// The deactivation walks the confirmation step like a manager would; the hostile
	// fields ride along with it.
	if rec := b.do(http.MethodPost, employeeDeactivateHref,
		deactivateConfirmed(t, b, employeeID, hostile)); rec.Code != http.StatusSeeOther {
		t.Fatalf("deactivate answered %d, want 303", rec.Code)
	}
	form := url.Values{"id": {employeeID.String()}}
	for k, vs := range hostile {
		form[k] = vs
	}
	if rec := b.do(http.MethodPost, employeeMoveHref, form); rec.Code != http.StatusSeeOther {
		t.Fatalf("move answered %d, want 303", rec.Code)
	}

	deactivations, moves := staff.commands()
	if len(deactivations) != 1 || len(moves) != 1 {
		t.Fatalf("the domain saw %d deactivation(s) and %d move(s), want 1 and 1",
			len(deactivations), len(moves))
	}
	for _, c := range []struct {
		what          string
		tenant, actor uuid.UUID
		employee      uuid.UUID
	}{
		{"deactivate", deactivations[0].TenantID, deactivations[0].ActorID, deactivations[0].EmployeeID},
		{"move", moves[0].TenantID, moves[0].ActorID, moves[0].EmployeeID},
	} {
		if c.tenant != panelTestTenant {
			t.Errorf("%s carried tenant %s, want the session's %s — a posted tenant id "+
				"must not be readable", c.what, c.tenant, panelTestTenant)
		}
		if c.actor != panelTestAdmin {
			t.Errorf("%s carried actor %s, want the session's %s — audit_log.actor_id "+
				"answers WHO, and a caller must not be able to name somebody else",
				c.what, c.actor, panelTestAdmin)
		}
		if c.employee != employeeID {
			t.Errorf("%s carried employee %s, want %s", c.what, c.employee, employeeID)
		}
	}
}

// TestEmployeeActions_AreRefusedCrossOriginBeforeTheResolverRuns is the net for the
// ORDER of ProtectWriting's four stages.
//
// 🔴 IT COUNTS RESOLVER READS, NOT STATUS CODES, because the status code is the same
// either way and the whole difference the gate's POSITION buys is COST. M6-04 shipped
// the gate after the resolver and an audit measured what that meant: 300 cross-origin
// POSTs spent a signed-in manager's session budget and locked them out of their own
// panel. A test asserting only "303 to /admin" would have passed on that code.
func TestEmployeeActions_AreRefusedCrossOriginBeforeTheResolverRuns(t *testing.T) {
	staff := &fakeStaff{}
	invites := &fakeInviter{}
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: "owner", FullName: "KF Owner",
		}, nil
	}}
	b := newBrowser(t, newAdminRouterWithActions(t, admins, &fakeTrail{}, newFakeLedger(),
		&fakeReviewer{}, staff, invites))
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	employeeID := uuid.New()
	form := url.Values{"id": {employeeID.String()}, "to_location": {panelTestLocation.String()}}

	// POSITIVE CONTROL FIRST. Without it, a router that never reaches the resolver at
	// all would satisfy the zero below.
	if rec := b.do(http.MethodPost, employeeDeactivateHref, form); rec.Code != http.StatusSeeOther {
		t.Fatalf("same-origin POST answered %d, want 303", rec.Code)
	}
	baseline := admins.verifiedCount()
	if baseline == 0 {
		t.Fatal("a served POST resolved no session; the counter is not wired")
	}
	deactivationsBefore, movesBefore := staff.commands()

	b.origin = "https://evil.example"
	for _, target := range []string{employeeInviteHref, employeeDeactivateHref, employeeMoveHref} {
		rec := b.do(http.MethodPost, target, form)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin" {
			t.Errorf("cross-origin POST %s: %d %q, want 303 /admin",
				target, rec.Code, rec.Header().Get("Location"))
		}
	}
	if got := admins.verifiedCount() - baseline; got != 0 {
		t.Errorf("three cross-origin POSTs cost %d resolver read(s), want 0. The Origin "+
			"check must run BEFORE requireAdmin, or a page on another origin of the same "+
			"site can spend a manager's session budget", got)
	}
	deactivationsAfter, movesAfter := staff.commands()
	if len(deactivationsAfter) != len(deactivationsBefore) || len(movesAfter) != len(movesBefore) {
		t.Error("a cross-origin POST reached the domain")
	}
	if n := len(invites.issuedParams()); n != 0 {
		t.Errorf("a cross-origin POST minted %d invitation(s)", n)
	}
}

// TestEmployeeActions_SecFetchSiteSameSitePassesTheOriginGate is S4, MEASURED rather
// than described, because this diff is what put three mutating routes — one of them
// irreversible — behind that one line.
//
// 🔴 WHAT IT PINS. With NO Origin header, AdminAuth.sameOrigin accepts
// `Sec-Fetch-Site: same-site`, and "same-site" means precisely A DIFFERENT ORIGIN OF
// THE SAME SITE — a subdomain with an XSS, a subdomain takeover, the http twin of the
// https origin. ProtectWriting's own comment names those three as the threat it
// exists for, so the gate accepts what its reason rejects.
//
// ⚠️ IT IS NOT EXPLOITABLE FROM A BROWSER TODAY and the test says so rather than
// implying a hole: browsers always send Origin on POST, so a real cross-origin page
// cannot reach this branch, and a non-browser attacker forges Origin anyway. The
// value of pinning it is that the line is now load-bearing for a write nobody can
// undo, and tightening it (accept only `same-origin` when Origin is absent) is a
// one-word change in a shared function — which is why it is REPORTED rather than
// made here: that function also gates sign-out and the review queue, and their
// rounds are not this task's.
func TestEmployeeActions_SecFetchSiteSameSitePassesTheOriginGate(t *testing.T) {
	staff := &fakeStaff{}
	b := panelBrowserWithActions(t, staff, &fakeInviter{})
	b.origin = "" // no Origin header at all
	employeeID := uuid.New()

	// The confirmation is obtained the ordinary way first: this test is about the
	// ORIGIN gate, so every other gate must be satisfied or a refusal here would be
	// the wrong refusal.
	confirmed := deactivateConfirmed(t, b, employeeID, nil)
	req := httptest.NewRequest(http.MethodPost, employeeDeactivateHref,
		strings.NewReader(confirmed.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-site")
	req.RemoteAddr = b.ip
	for name, value := range b.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	rec := httptest.NewRecorder()
	b.h.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther || strings.Contains(rec.Header().Get("Location"), "problem=") {
		t.Fatalf("same-site POST answered %d -> %q; this test is the LEDGER of the gate "+
			"accepting it, so if it is now refused the gate has been tightened and this "+
			"test must be rewritten as the proof of that",
			rec.Code, rec.Header().Get("Location"))
	}
	if d, _ := staff.commands(); len(d) != 1 {
		t.Fatalf("the domain saw %d deactivation(s) from a same-site POST with no Origin, "+
			"want 1 (measured behaviour). A 0 means the gate was tightened.", len(d))
	}

	// THE CONTROLS, so the pass above is about `same-site` and not about a gate that
	// lets everything through.
	cross := panelBrowserWithActions(t, &fakeStaff{}, &fakeInviter{})
	cross.origin = "https://evil.example"
	if rec := cross.do(http.MethodPost, employeeDeactivateHref,
		url.Values{"id": {employeeID.String()}}); rec.Header().Get("Location") != "/admin" {
		t.Errorf("a cross-ORIGIN POST was not refused (Location %q); the gate is open to "+
			"everything and the measurement above means nothing", rec.Header().Get("Location"))
	}
}

// TestEmployeeActions_AnonymousPostsReachNothing. The three routes are behind the
// same gate the rest of the panel is; an unauthenticated POST is a redirect to the
// login form and nothing else.
func TestEmployeeActions_AnonymousPostsReachNothing(t *testing.T) {
	staff := &fakeStaff{}
	invites := &fakeInviter{}
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{}, adminauth.ErrNoSession
	}}
	b := newBrowser(t, newAdminRouterWithActions(t, admins, &fakeTrail{}, newFakeLedger(),
		&fakeReviewer{}, staff, invites))

	for _, target := range []string{employeeInviteHref, employeeDeactivateHref, employeeMoveHref} {
		rec := b.do(http.MethodPost, target, url.Values{
			"id": {uuid.NewString()}, "to_location": {panelTestLocation.String()},
		})
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login" {
			t.Errorf("anonymous POST %s: %d %q, want 303 /admin/login",
				target, rec.Code, rec.Header().Get("Location"))
		}
	}
	if d, m := staff.commands(); len(d)+len(m) != 0 {
		t.Errorf("an anonymous POST reached the domain (%d deactivation(s), %d move(s))", len(d), len(m))
	}
	if n := len(invites.issuedParams()); n != 0 {
		t.Errorf("an anonymous POST minted %d invitation(s)", n)
	}
}

// TestEmployeeActions_TheBodyIsBounded. An oversized body is refused by the READ
// rather than buffered and then measured, and nothing is written.
func TestEmployeeActions_TheBodyIsBounded(t *testing.T) {
	staff := &fakeStaff{}
	invites := &fakeInviter{}
	b := panelBrowserWithActions(t, staff, invites)
	huge := "id=" + uuid.NewString() + "&pad=" + strings.Repeat("x", maxEmployeeActionBody+1)

	for _, target := range []string{employeeInviteHref, employeeDeactivateHref, employeeMoveHref} {
		rec := b.doRaw(http.MethodPost, target, huge)
		if rec.Code != http.StatusSeeOther {
			t.Errorf("oversized POST %s answered %d, want 303", target, rec.Code)
		}
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "problem=unreadable") {
			t.Errorf("oversized POST %s redirected to %q, want problem=unreadable — §4.6 "+
				"forbids a refusal the manager cannot see", target, loc)
		}
	}
	if d, m := staff.commands(); len(d)+len(m) != 0 {
		t.Error("an oversized body reached the domain")
	}
	if n := len(invites.issuedParams()); n != 0 {
		t.Errorf("an oversized body minted %d invitation(s)", n)
	}
}

// TestEmployeeActions_EveryRefusalIsASentence is §4.6 at this boundary: for each
// way the domain can say no, the manager gets a redirect carrying a word the
// template turns into an explanation — and the explanation is RENDERED, not merely
// carried.
func TestEmployeeActions_EveryRefusalIsASentence(t *testing.T) {
	employeeID := uuid.New()
	cases := []struct {
		name   string
		target string
		staff  *fakeStaff
		want   string
		expect string
	}{
		{
			name:   "unknown employee",
			target: employeeDeactivateHref,
			staff:  &fakeStaff{deactivateErr: tenant.ErrUnknownEmployee},
			want:   "problem=unknown",
			expect: "not on this business",
		},
		{
			name:   "already deactivated",
			target: employeeDeactivateHref,
			staff:  &fakeStaff{deactivateErr: tenant.ErrAlreadyDeactivated},
			want:   "problem=already-deactivated",
			expect: "already deactivated",
		},
		{
			name:   "unknown placement",
			target: employeeMoveHref,
			staff:  &fakeStaff{moveErr: tenant.ErrUnknownPlacement},
			want:   "problem=unknown-placement",
			// THE SENTENCE NAMES BOTH CAUSES since a security audit found the second one
			// being accepted silently: a department that belongs to a DIFFERENT venue of
			// the same business. The fragment asserted here is that second half, because
			// it is the one that was missing.
			expect: "the department belongs to a different venue",
		},
		{
			name:   "no change",
			target: employeeMoveHref,
			staff:  &fakeStaff{moveErr: tenant.ErrSamePlacement},
			want:   "problem=no-change",
			expect: "already where they work",
		},
		{
			name:   "not invitable",
			target: employeeInviteHref,
			staff: &fakeStaff{person: &tenant.Person{
				Name: "Paul Spiteri", Status: ledger.StatusDeactivated, LocationID: panelTestLocation,
			}},
			want:   "problem=not-invitable",
			expect: "deactivated",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := panelBrowserWithActions(t, tc.staff, &fakeInviter{})
			form := url.Values{"id": {employeeID.String()}, "to_location": {panelTestLocation.String()}}
			if tc.target == employeeDeactivateHref {
				form = deactivateConfirmed(t, b, employeeID, nil)
			}
			rec := b.do(http.MethodPost, tc.target, form)
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("answered %d, want 303", rec.Code)
			}
			loc := rec.Header().Get("Location")
			if !strings.Contains(loc, tc.want) {
				t.Fatalf("redirected to %q, want it to carry %q", loc, tc.want)
			}
			// THE WORD IS NOT THE POINT; THE SENTENCE IS. A redirect carrying a
			// vocabulary the template does not render is a silent refusal with extra
			// steps.
			body := htmlOf(t, b.do(http.MethodGet, loc, nil))
			if !strings.Contains(body, tc.expect) {
				t.Errorf("the page at %s does not say %q; §4.6 wants the manager to know "+
					"why nothing happened", loc, tc.expect)
			}
		})
	}
}

// TestEmployeeActions_ADatabaseFailureIsNotARefusal. Every branch a caller's input
// can select is a redirect; a failure the caller did not cause is a 500. Collapsing
// the two would tell a manager their action was refused when in fact nothing was
// asked, and would hide an outage behind a plausible sentence.
func TestEmployeeActions_ADatabaseFailureIsNotARefusal(t *testing.T) {
	boom := errors.New("connection refused")
	for _, tc := range []struct {
		name   string
		target string
		staff  *fakeStaff
	}{
		{"deactivate", employeeDeactivateHref, &fakeStaff{deactivateErr: boom}},
		{"move", employeeMoveHref, &fakeStaff{moveErr: boom}},
		{"invite", employeeInviteHref, &fakeStaff{personErr: boom}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := panelBrowserWithActions(t, tc.staff, &fakeInviter{})
			id := uuid.New()
			form := url.Values{"id": {id.String()}, "to_location": {panelTestLocation.String()}}
			if tc.target == employeeDeactivateHref {
				form = deactivateConfirmed(t, b, id, nil)
			}
			rec := b.do(http.MethodPost, tc.target, form)
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("a failing database answered %d, want 500", rec.Code)
			}
		})
	}
}

// TestEmployeeInvite_ShowsTheLinkOnceAndNeverLogsIt is §4.7 on the one screen that
// renders a credential.
//
// THREE PLACES ARE CHECKED and each one has been a real leak somewhere in this
// repository: the process log, the audit trail's jsonb payload, and the redirect
// target. The fourth — the database — is internal/invite's, which stores the HMAC.
func TestEmployeeInvite_ShowsTheLinkOnceAndNeverLogsIt(t *testing.T) {
	trail := &fakeTrail{}
	staff := &fakeStaff{}
	invites := &fakeInviter{}
	b := panelBrowserWithActionsAndTrail(t, staff, invites, trail)
	logs := &strings.Builder{}
	b.h = newAdminRouterLogging(t, logs, trail, staff, invites)

	employeeID := uuid.New()
	rec := b.do(http.MethodPost, employeeInviteHref, url.Values{"id": {employeeID.String()}})
	if rec.Code != http.StatusOK {
		t.Fatalf("invite answered %d, want 200 with the link rendered", rec.Code)
	}
	body := htmlOf(t, rec)
	if !strings.Contains(body, "FAKE-CODE-VALUE") {
		t.Fatalf("the response does not carry the activation link; there is no other " +
			"screen that can show it, so this is the whole of the manager's access to it")
	}
	// AND IT IS NOT A LINK. A manager clicking it would move the code into their own
	// browser's activation cookie and into their history.
	if regexp.MustCompile(`(?is)<a\b[^>]*href="[^"]*FAKE-CODE-VALUE`).MatchString(body) {
		t.Error("the activation link is rendered as an anchor; it must be text")
	}
	if strings.Contains(logs.String(), "FAKE-CODE-VALUE") {
		t.Error("the activation link reached the process log (§4.7)")
	}
	for _, e := range trail.eventsSnapshot() {
		if strings.Contains(detailJSON(t, e.Detail), "FAKE-CODE-VALUE") {
			t.Errorf("the activation link reached audit_log.detail on action %q (§4.7)", e.Action)
		}
	}
	if n := trail.count(invite.ActionCodeShownToManager); n != 1 {
		t.Errorf("the disclosure was recorded %d time(s), want 1 — ADR 0005 Y-D's "+
			"detection signal is exactly this row", n)
	}

	// THE SECOND PRESS MINTS A SECOND INVITATION rather than replaying the first. That
	// is what "shown once" costs and it is asserted rather than described.
	if rec := b.do(http.MethodPost, employeeInviteHref, url.Values{"id": {employeeID.String()}}); rec.Code != http.StatusOK {
		t.Fatalf("the second press answered %d", rec.Code)
	}
	if n := len(invites.issuedParams()); n != 2 {
		t.Errorf("two presses minted %d invitation(s), want 2", n)
	}
	if n := trail.count(invite.ActionCodeShownToManager); n != 2 {
		t.Errorf("two disclosures wrote %d audit row(s), want 2", n)
	}
	// AND NO GET CAN SHOW IT AGAIN. The roster is the only other screen that names
	// this person.
	roster := htmlOf(t, b.do(http.MethodGet, managedRosterHref(employeeID), nil))
	if strings.Contains(roster, "FAKE-CODE-VALUE") {
		t.Error("the roster renders an activation link; it must exist in exactly one " +
			"response body, the answer to the POST that minted it")
	}
}

// TestEmployeeInvite_CarriesTheSessionsTenantAndEmployee. The invitation is minted
// for the tenant the cookie resolved to, never for one the body names.
func TestEmployeeInvite_CarriesTheSessionsTenantAndEmployee(t *testing.T) {
	invites := &fakeInviter{}
	b := panelBrowserWithActions(t, &fakeStaff{}, invites)
	employeeID := uuid.New()

	if rec := b.do(http.MethodPost, employeeInviteHref, url.Values{
		"id": {employeeID.String()}, "tenant_id": {uuid.NewString()},
	}); rec.Code != http.StatusOK {
		t.Fatalf("invite answered %d", rec.Code)
	}
	issued := invites.issuedParams()
	if len(issued) != 1 {
		t.Fatalf("minted %d invitation(s), want 1", len(issued))
	}
	if issued[0].TenantID != panelTestTenant {
		t.Errorf("minted for tenant %s, want the session's %s", issued[0].TenantID, panelTestTenant)
	}
	if issued[0].EmployeeID != employeeID {
		t.Errorf("minted for employee %s, want %s", issued[0].EmployeeID, employeeID)
	}
}

// TestEmployeeMove_TheDestinationIsNotAFilter is the collision this form's field
// names exist to avoid.
//
// 🔴 THE ROSTER'S FILTERS ARE CALLED location AND department AND THE MOVE FORM
// CARRIES THEM BOTH BACK. If the destination shared those names, pressing Save would
// move somebody AND narrow the list they returned to — a silent, invisible filter
// change caused by an unrelated action.
func TestEmployeeMove_TheDestinationIsNotAFilter(t *testing.T) {
	staff := &fakeStaff{}
	b := panelBrowserWithActions(t, staff, &fakeInviter{})
	employeeID := uuid.New()
	destination := uuid.New()
	filterVenue := uuid.New()

	rec := b.do(http.MethodPost, employeeMoveHref, url.Values{
		"id":          {employeeID.String()},
		"to_location": {destination.String()},
		"location":    {filterVenue.String()},
		"status":      {ledger.StatusActive},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("move answered %d, want 303", rec.Code)
	}
	_, moves := staff.commands()
	if len(moves) != 1 {
		t.Fatalf("the domain saw %d move(s), want 1", len(moves))
	}
	if moves[0].LocationID != destination {
		t.Errorf("moved to %s, want the destination %s — the filter must not be read as "+
			"the destination", moves[0].LocationID, destination)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "location="+filterVenue.String()) {
		t.Errorf("the redirect %q dropped the filter the manager was using", loc)
	}
	if !strings.Contains(loc, "status="+ledger.StatusActive) {
		t.Errorf("the redirect %q dropped the status filter", loc)
	}
}

// TestEmployeeMove_AnEmptyDepartmentIsADestination. "No department" is a first-class
// state (migration 00003: a bar does not model departments), so an empty field is a
// destination; anything that is neither empty nor a uuid is a refusal rather than a
// silent move out of somebody's department.
func TestEmployeeMove_AnEmptyDepartmentIsADestination(t *testing.T) {
	staff := &fakeStaff{}
	b := panelBrowserWithActions(t, staff, &fakeInviter{})

	if rec := b.do(http.MethodPost, employeeMoveHref, url.Values{
		"id": {uuid.NewString()}, "to_location": {panelTestLocation.String()}, "to_department": {""},
	}); rec.Code != http.StatusSeeOther {
		t.Fatalf("move answered %d, want 303", rec.Code)
	}
	_, moves := staff.commands()
	if len(moves) != 1 {
		t.Fatalf("the domain saw %d move(s), want 1", len(moves))
	}
	if moves[0].DepartmentID != nil {
		t.Errorf("an empty department became %v, want nil", moves[0].DepartmentID)
	}

	rec := b.do(http.MethodPost, employeeMoveHref, url.Values{
		"id": {uuid.NewString()}, "to_location": {panelTestLocation.String()},
		"to_department": {"not-a-uuid"},
	})
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "problem=unreadable") {
		t.Errorf("a mangled department redirected to %q; it must be refused rather than "+
			"read as 'no department'", loc)
	}
	if _, moves := staff.commands(); len(moves) != 1 {
		t.Errorf("a mangled department reached the domain (%d move(s) in total)", len(moves))
	}
}

// TestRosterReturn_EchoesOnlyValidatedValues. The redirect is rebuilt from a parsed
// filter, so nothing a caller posted travels back into a Location header as text —
// which is also why this cannot become an open redirect.
func TestRosterReturn_EchoesOnlyValidatedValues(t *testing.T) {
	staff := &fakeStaff{}
	b := panelBrowserWithActions(t, staff, &fakeInviter{})

	rec := b.do(http.MethodPost, employeeDeactivateHref, url.Values{
		"id":       {uuid.NewString()},
		"status":   {"not-a-status"},
		"location": {"https://evil.example/"},
		"after_id": {"../../etc/passwd"},
		"name":     {"Maria"},
	})
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, employeesHref+"?") {
		t.Fatalf("redirected to %q, want a URL under %s", loc, employeesHref)
	}
	for _, hostile := range []string{"not-a-status", "evil.example", "etc/passwd"} {
		if strings.Contains(loc, hostile) {
			t.Errorf("the redirect %q echoes %q, which no validator accepted", loc, hostile)
		}
	}
	if !strings.Contains(loc, "name=Maria") {
		t.Errorf("the redirect %q dropped the name filter, which IS valid — a return "+
			"that loses the manager's place is the reason these fields travel", loc)
	}
}

// TestExpiryPhrase_SaysALengthAndNeverZero. The invitation page prints how long the
// link lasts; a zero or negative window must not read as "no time at all", and no
// branch prints an instant (which would need a timezone this response has not read).
func TestExpiryPhrase_SaysALengthAndNeverZero(t *testing.T) {
	created := time.Date(2026, 8, 7, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		ttl  time.Duration
		want string
	}{
		{7 * 24 * time.Hour, "7 days"},
		{30 * 24 * time.Hour, "30 days"},
		{2 * time.Hour, "less than a day"},
		{0, "unknown length"},
		{-time.Hour, "unknown length"},
	} {
		if got := expiryPhrase(created, created.Add(tc.ttl)); !strings.Contains(got, tc.want) {
			t.Errorf("expiryPhrase(%s) = %q, want it to contain %q", tc.ttl, got, tc.want)
		}
	}
}

// newAdminRouterLogging is the same wiring with the process log captured, so a test
// can assert what is NOT in it.
func newAdminRouterLogging(t *testing.T, into *strings.Builder, trail *fakeTrail, staff panelStaff, invites panelInviter) http.Handler {
	t.Helper()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: "owner", FullName: "KF Owner",
		}, nil
	}}
	h, err := NewAdminAuth(admins, trail, newFakeLedger(), newFakeLedger(), &fakeReviewer{},
		staff, invites, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(), newFakeTexts(), newFakeAccount(), nil, adminTestConfig(),
		slog.New(slog.NewTextHandler(into, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	return r
}

// detailJSON renders an audit event's payload exactly as internal/audit would, so a
// leak assertion reads what the database would store rather than what a Go %v would
// print.
func detailJSON(t *testing.T, detail any) string {
	t.Helper()
	if detail == nil {
		return "{}"
	}
	b, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal audit detail: %v", err)
	}
	return string(b)
}
