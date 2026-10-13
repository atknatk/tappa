package handler

// externalref_test.go -- M11 API-1's two panel writes (K-18): the employee's and the venue's
// external ref. What each route forwards, who reaches the domain, what every refusal says,
// and what the cards render. The rule, the uniqueness and the trail are measured against real
// Postgres in internal/domain/tenant/externalref_db_test.go.

import (
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/tenant"
)

// refVenueBrowser is the panel with a session of the given ROLE and the venues and the trail
// under the caller's control (emailBrowser's shape for the locations section).
func refVenueBrowser(t *testing.T, role string, venues *fakeVenues, trail *fakeTrail) *browser {
	t.Helper()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: role, FullName: "KF Admin",
		}, nil
	}}
	h, err := NewAdminAuth(admins, trail, newFakeLedger(), newFakeLedger(), &fakeReviewer{},
		&fakeStaff{}, &fakeInviter{}, venues, &fakePlaques{}, &fakeRecorder{}, newFakeRules(),
		newFakeScribe(), newFakeBooks(), newFakeAccount(), newFakeBrands(), newFakeBrandWriter(),
		nil, &fakeNotices{}, adminTestConfig(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

// TestEmployeeExternalRef_OnlyAnOwnerReachesTheDomainAndEveryOutcomeIsAWord: the owner's POST
// forwards ONE command carrying the session's tenant and actor (a posted tenant_id / actor_id
// is ignored), the posted employee and the RAW posted ref; success is a 303 to the card with
// done=external-ref. Each domain refusal is its own closed word, and an unknown employee goes
// back to the roster. A manager, no role and an undefined role reach nothing on the staff
// surface, are told external-ref-not-permitted and leave one refusal row naming the employee.
func TestEmployeeExternalRef_OnlyAnOwnerReachesTheDomainAndEveryOutcomeIsAWord(t *testing.T) {
	employeeID := uuid.New()
	form := url.Values{
		"id":           {employeeID.String()},
		"external_ref": {" EMP-1001 "},
		"tenant_id":    {uuid.NewString()},
		"actor_id":     {uuid.NewString()},
		"role":         {"owner"},
	}
	for _, c := range []struct {
		name string
		err  error
		want string
	}{
		{"success", nil, "done=external-ref"},
		{"bad shape", tenant.ErrExternalRef, "problem=bad-external-ref"},
		{"taken", tenant.ErrExternalRefTaken, "problem=external-ref-taken"},
		{"same", tenant.ErrSameExternalRef, "problem=same-external-ref"},
		{"no home venue", tenant.ErrExternalRefRequired, "problem=external-ref-required"},
		{"unknown employee", tenant.ErrUnknownEmployee, "problem=unknown"},
	} {
		t.Run(c.name, func(t *testing.T) {
			staff, trail := &fakeStaff{refErr: c.err}, &fakeTrail{}
			b, _, _ := emailBrowser(t, "owner", staff, trail)
			rec := b.do(http.MethodPost, employeeExternalRefHref, form)
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("answered %d, want 303", rec.Code)
			}
			if loc := rec.Header().Get("Location"); !strings.Contains(loc, c.want) {
				t.Errorf("redirected to %q, want %s", loc, c.want)
			}
			cmds := staff.refCommands()
			if len(cmds) != 1 {
				t.Fatalf("the domain saw %d change(s), want 1", len(cmds))
			}
			if got := cmds[0]; got.TenantID != panelTestTenant || got.ActorID != panelTestAdmin ||
				got.SubjectID != employeeID || got.ExternalRef != " EMP-1001 " {
				t.Errorf("command %+v; want the session's tenant and actor, the posted employee and the raw ref", got)
			}
		})
	}
	for _, refused := range []struct{ name, role string }{{"manager", "manager"}, {"no role", ""}, {"undefined role", "auditor"}} {
		t.Run(refused.name, func(t *testing.T) {
			staff, trail := &fakeStaff{}, &fakeTrail{}
			b, _, _ := emailBrowser(t, refused.role, staff, trail)
			rec := b.do(http.MethodPost, employeeExternalRefHref, form)
			if loc := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || !strings.Contains(loc, "problem=external-ref-not-permitted") {
				t.Errorf("%s: answered %d to %q, want 303 with external-ref-not-permitted", refused.name, rec.Code, loc)
			}
			if n := len(staff.refCommands()); n != 0 {
				t.Errorf("%s: the domain saw %d change(s)", refused.name, n)
			}
			events := trail.eventsSnapshot()
			if len(events) != 1 || events[0].Action != ActionEmployeeExternalRefChangeRefused ||
				events[0].Target != employeeID.String() || events[0].TenantID != panelTestTenant {
				t.Errorf("%s: trail %+v, want one %s naming the employee in the session's tenant", refused.name, events, ActionEmployeeExternalRefChangeRefused)
			}
		})
	}
}

// TestEmployeesCard_ShowsTheExternalRefAndOffersTheFormToAnOwnerOnly: the card shows the ref on
// file (or that there is none) to every admin; the form is rendered for an owner and replaced by
// the sentence that says who can for anybody else. A person with no home venue reads "No home
// venue", never "could not be read".
func TestEmployeesCard_ShowsTheExternalRefAndOffersTheFormToAnOwnerOnly(t *testing.T) {
	for _, c := range []struct {
		role     string
		form     bool
		ref      string
		location bool
	}{{"owner", true, "EMP-1001", true}, {"manager", false, "EMP-1001", true}, {"owner", true, "", false}} {
		person := &tenant.Person{ID: uuid.New(), Name: "Maria Borg", Status: "active", ExternalRef: c.ref}
		if c.location {
			person.LocationID, person.LocationName = &panelTestLocation, "KF St Julians"
		}
		b, _, _ := emailBrowser(t, c.role, &fakeStaff{person: person}, &fakeTrail{})
		rec := b.do(http.MethodGet, employeesHref+"?manage="+person.ID.String(), nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: GET answered %d", c.role, rec.Code)
		}
		body := rec.Body.String()
		if got := strings.Contains(body, `name="external_ref"`); got != c.form {
			t.Errorf("%s: the external-ref form rendered=%v, want %v", c.role, got, c.form)
		}
		if !c.form && !strings.Contains(body, "Only the business owner can change an external ref.") {
			t.Errorf("%s: no sentence saying who can change the ref", c.role)
		}
		if c.ref != "" && !strings.Contains(body, c.ref) {
			t.Errorf("%s: the ref on file %q is not shown", c.role, c.ref)
		}
		if c.ref == "" && !strings.Contains(body, "Not linked to an HR or ERP record.") {
			t.Errorf("%s: an unbound person does not say so", c.role)
		}
		if !c.location && (!strings.Contains(body, "No home venue") || strings.Contains(body, "Venue could not be read")) {
			t.Errorf("a person with no home venue is not shown as such")
		}
	}
}

// TestVenueExternalRef_OnlyAnOwnerReachesTheDomainAndEveryOutcomeIsAWord: the venue's twin --
// one command with the session's tenant and actor, the posted venue and the raw ref; each
// outcome a closed word on the section; a non-owner reaches nothing and leaves one refusal row;
// an id that is not a uuid is "unreadable".
func TestVenueExternalRef_OnlyAnOwnerReachesTheDomainAndEveryOutcomeIsAWord(t *testing.T) {
	venueID := uuid.New()
	form := url.Values{"id": {venueID.String()}, "external_ref": {"LOC-STJ"}, "tenant_id": {uuid.NewString()}}
	for _, c := range []struct {
		name string
		err  error
		want string
	}{
		{"success", nil, "done=venue-external-ref"},
		{"bad shape", tenant.ErrExternalRef, "problem=bad-external-ref"},
		{"taken", tenant.ErrExternalRefTaken, "problem=external-ref-taken"},
		{"same", tenant.ErrSameExternalRef, "problem=same-external-ref"},
		{"unknown venue", tenant.ErrUnknownVenue, "problem=unknown-venue"},
	} {
		t.Run(c.name, func(t *testing.T) {
			venues := &fakeVenues{venueRefErr: c.err}
			b := refVenueBrowser(t, "owner", venues, &fakeTrail{})
			rec := b.do(http.MethodPost, venueExternalRefHref, form)
			if loc := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || !strings.Contains(loc, c.want) {
				t.Errorf("answered %d to %q, want 303 with %s", rec.Code, loc, c.want)
			}
			if len(venues.venueRefs) != 1 || venues.venueRefs[0].SubjectID != venueID || venues.venueRefs[0].TenantID != panelTestTenant ||
				venues.venueRefs[0].ActorID != panelTestAdmin || venues.venueRefs[0].ExternalRef != "LOC-STJ" {
				t.Errorf("the domain saw %+v, want one command for the posted venue in the session's tenant", venues.venueRefs)
			}
		})
	}
	venues, trail := &fakeVenues{}, &fakeTrail{}
	b := refVenueBrowser(t, "manager", venues, trail)
	rec := b.do(http.MethodPost, venueExternalRefHref, form)
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || !strings.Contains(loc, "problem=external-ref-not-permitted") {
		t.Errorf("a manager: answered %d to %q, want 303 with external-ref-not-permitted", rec.Code, loc)
	}
	if len(venues.venueRefs) != 0 || trail.count(ActionLocationExternalRefChangeRefused) != 1 {
		t.Errorf("a manager reached the domain %d time(s), %d refusal row(s); want 0 and 1", len(venues.venueRefs), trail.count(ActionLocationExternalRefChangeRefused))
	}
	rec = refVenueBrowser(t, "owner", &fakeVenues{}, &fakeTrail{}).do(http.MethodPost, venueExternalRefHref, url.Values{"id": {"not-a-uuid"}, "external_ref": {"X"}})
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "problem=unreadable") {
		t.Errorf("an unreadable id redirected to %q, want problem=unreadable", loc)
	}
}

// TestVenueCard_ShowsTheExternalRefAndOffersTheFormToAnOwnerOnly: the venue list shows each
// venue's ref; the edit card of an existing venue carries the owner's form (and a manager's
// sentence instead); a NEW venue's card has none.
func TestVenueCard_ShowsTheExternalRefAndOffersTheFormToAnOwnerOnly(t *testing.T) {
	venue := tenant.Venue{ID: uuid.New(), Name: "KF St Julians", ExternalRef: "LOC-STJ"}
	for _, role := range []string{"owner", "manager"} {
		b := refVenueBrowser(t, role, &fakeVenues{screen: tenant.VenueScreen{Venues: []tenant.Venue{venue}}}, &fakeTrail{})
		body := b.do(http.MethodGet, locationsHref+"?venue="+venue.ID.String(), nil).Body.String()
		if !strings.Contains(body, "LOC-STJ") {
			t.Errorf("%s: the venue's ref is not shown", role)
		}
		if got, want := strings.Contains(body, `name="external_ref"`), role == "owner"; got != want {
			t.Errorf("%s: the ref form rendered=%v, want %v", role, got, want)
		}
		if role == "manager" && !strings.Contains(body, "Only the business owner can change an external ref.") {
			t.Errorf("a manager is not told who can change the ref")
		}
	}
	b := refVenueBrowser(t, "owner", &fakeVenues{screen: tenant.VenueScreen{Venues: []tenant.Venue{venue}}}, &fakeTrail{})
	if body := b.do(http.MethodGet, locationsHref+"?venue=new", nil).Body.String(); strings.Contains(body, `name="external_ref"`) {
		t.Error("a NEW venue's card offers an external-ref form; there is no row to bind yet")
	}
}

// TestPanelWrites_EveryRouteMountWritingRegistersRefusesAForeignOriginBeforeTheResolver: the two
// external-ref routes -- and every other POST mountWriting registers -- answer a foreign Origin, a
// same-site sibling's, `null` and an absent one with the Origin gate's refusal (303 to /admin)
// and NO session lookup; the two external-ref commands are never built. The same-origin control
// reaches the resolver and the domain once per route, so the zeros are about the Origin.
//
// 🔴 THE ROUTE SET IS mountWriting'S OWN REGISTRATION, NOT A LIST. The hand-written lists before
// this one (TestEmployeeActions_AreRefusedCrossOriginBeforeTheResolverRuns,
// TestVenueWrites_AreOnTheWritingChain, ...) each name their own section's routes, and API-1's two
// were in none of them. mountWriting is given a router of its own and that router is walked, so
// a route added there is covered the day it is added -- and sign-in and sign-out, which mount
// elsewhere on their own chains, are not dragged in, so no exemption list is needed
// (TestPlaqueWrites_AreOnTheWRITINGChain's walk is the same idea for one section's prefix).
func TestPanelWrites_EveryRouteMountWritingRegistersRefusesAForeignOriginBeforeTheResolver(t *testing.T) {
	staff, venues := &fakeStaff{}, &fakeVenues{}
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: "owner", FullName: "KF Owner",
		}, nil
	}}
	a, err := NewAdminAuth(admins, &fakeTrail{}, newFakeLedger(), newFakeLedger(), &fakeReviewer{},
		staff, &fakeInviter{}, venues, &fakePlaques{}, &fakeRecorder{}, newFakeRules(),
		newFakeScribe(), newFakeBooks(), newFakeAccount(), newFakeBrands(), newFakeBrandWriter(),
		nil, &fakeNotices{}, adminTestConfig(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	full := chi.NewRouter()
	a.Mount(full)
	writing := chi.NewRouter()
	a.mountWriting(writing)
	var routes []string
	if err := chi.Walk(writing, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method == http.MethodPost {
			routes = append(routes, route)
		}
		return nil
	}); err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	// ANTI-VACUITY: the walk reads the real registration -- the two routes this task added are in it.
	if !slices.Contains(routes, employeeExternalRefHref) || !slices.Contains(routes, venueExternalRefHref) {
		t.Fatalf("the walk of mountWriting found %v, without %s and %s; it is not reading the real registration",
			routes, employeeExternalRefHref, venueExternalRefHref)
	}

	b := newBrowser(t, full)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	form := url.Values{"id": {uuid.NewString()}, "external_ref": {"KF-1"}}

	// THE CONTROL: same-origin, each external-ref POST resolves the session and reaches its domain.
	for _, route := range []string{employeeExternalRefHref, venueExternalRefHref} {
		if rec := b.do(http.MethodPost, route, form); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") == "/admin" {
			t.Fatalf("control: a same-origin POST to %s answered %d -> %q, want the route's own 303", route, rec.Code, rec.Header().Get("Location"))
		}
	}
	baseline := admins.verifiedCount()
	if baseline != 2 || len(staff.refCommands()) != 1 || len(venues.venueRefs) != 1 {
		t.Fatalf("control: %d session lookup(s), %d employee and %d venue command(s); want 2, 1 and 1 -- the counters are not wired",
			baseline, len(staff.refCommands()), len(venues.venueRefs))
	}

	for _, o := range []struct{ name, origin string }{
		{"a foreign origin", "https://evil.example"},
		{"a same-site sibling", "https://evil.example.test"},
		{"Origin: null", "null"},
		{"no Origin", ""},
	} {
		b.origin = o.origin
		for _, route := range routes {
			rec := b.do(http.MethodPost, route, form)
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin" {
				t.Errorf("%s: POST %s answered %d -> %q, want the Origin gate's 303 -> /admin", o.name, route, rec.Code, rec.Header().Get("Location"))
			}
		}
	}
	if got := admins.verifiedCount() - baseline; got != 0 {
		t.Errorf("%d refused POSTs over %d route(s) cost %d session lookup(s), want 0: the Origin check must run "+
			"BEFORE the resolver on every route mountWriting registers", 4*len(routes), len(routes), got)
	}
	if len(staff.refCommands()) != 1 || len(venues.venueRefs) != 1 {
		t.Errorf("after the refused POSTs the domain holds %d employee and %d venue external-ref command(s), want still 1 and 1",
			len(staff.refCommands()), len(venues.venueRefs))
	}
	t.Logf("%d route(s) mountWriting registers, each refused for 4 origins", len(routes))
}
