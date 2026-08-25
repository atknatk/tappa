package handler

// The WALL PLAQUES section, over the REAL router (M6-06 phase B).
//
// 🔴 WHAT THESE TESTS CAN AND CANNOT PROVE, said here so nobody reads more into
// them than is there. They drive the production router and the production
// middleware chain, with the DOMAIN faked — so they prove what the SECTION does:
// which lists it builds, which control it offers, which word it redirects with,
// how many trail lookups it makes, and what a stranger's URL gets. They prove
// NOTHING about §4.5, about the retire and the bind sharing one transaction, or
// about a CHECK firing; a fake agrees with whatever it is told. Those live in
// internal/domain/tenant/plaque_db_test.go against real Postgres, and the tap side
// in plaquetap_db_test.go.

import (
	"encoding/base64"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/web/templates/components"
	"github.com/atknatk/tappa/web/templates/pages"
)

// plaqueBrowser is a signed-in manager looking at a section backed by the two
// fakes.
func plaqueBrowser(t *testing.T, venues panelVenues, plaques panelPlaques) *browser {
	t.Helper()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: "owner", FullName: "KF Owner",
		}, nil
	}}
	b := newBrowser(t, newAdminRouterWithPlaques(t, admins, venues, plaques))
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

// armReplacement walks the card the way a manager does and returns the confirmation
// the server minted there.
//
// 🔴 IT GOES THROUGH THE REAL GET RATHER THAN CALLING mint, and that is the point of
// the gate: the value exists only because the server RENDERED THE WARNING, in
// response to a request for it, for this session. A test that minted its own would
// be testing the signature and not the mechanism.
func armReplacement(t *testing.T, b *browser, uid string) string {
	t.Helper()
	html := htmlOf(t, b.do(http.MethodGet, locationsHref+"?plaque="+uid, nil))
	m := confirmTokenRE.FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("the replacement card carried no confirmation for %s.\n%s", uid, html)
	}
	return m[1]
}

var confirmTokenRE = regexp.MustCompile(`name="` + confirmField + `" value="([^"]*)"`)

// mintedConfirmation reports whether the SERVER MINTED a confirmation for this
// request, read off Set-Cookie rather than off the rendered HTML.
//
// 🔴 THE DIFFERENCE IS NOT PEDANTRY — IT IS WHAT AN AUDIT MEASURED. The assertions
// here used to scan the body for the hidden input, and the body is written by the
// template's `case` arms while the MINT happens in the handler. A mutation that made
// the mount card mint (plaques.go, `f.Mode == "replace"` -> `|| f.Mode == "mount"`)
// therefore set a confirmation cookie on a card that rendered no form, and the whole
// package stayed green: 2312 tests, 0 failures. A value the server signs, writes to
// the browser and never checks is the gate existing and never being spent — worse
// than no gate, and invisible to a test that only reads HTML.
func mintedConfirmation(rec *httptest.ResponseRecorder) bool {
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == adminConfirmCookieName && ck.Value != "" && ck.MaxAge >= 0 {
			return true
		}
	}
	return false
}

// Fixed uids, so an assertion reads as the plaque a manager would see on a wall
// rather than as a random string. They are canonical (upper-case hex, 14 chars) —
// the only spelling migration 00013 accepts.
const (
	uidOnWall  = "04AC7E55000601"
	uidInStock = "04AC7E55000602"
	uidRetired = "04AC7E55000603"
	uidSpare2  = "04AC7E55000604"
)

// threePlaques is the ordinary fixture: one on a wall, one spare in the box, one
// already replaced.
func threePlaques(wall uuid.UUID) *fakePlaques {
	seen := time.Date(2026, 8, 9, 6, 3, 22, 0, time.UTC)
	retiredAt := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	return &fakePlaques{screen: tenant.PlaqueScreen{
		Zone: time.UTC,
		Plaques: []tenant.Plaque{
			{UID: uidInStock, Status: tenant.PlaqueUnassigned, LastCtr: 0, Canonical: true},
			{UID: uidOnWall, Status: tenant.PlaqueActive, LocationID: &wall, LastCtr: 641,
				LastSeen: &seen, Replaces: uidRetired, Canonical: true},
			{UID: uidRetired, Status: tenant.PlaqueRetired, LocationID: &wall, LastCtr: 512,
				RetiredAt: &retiredAt, ReplacedBy: uidOnWall, Canonical: true},
		},
	}}
}

// --- the list -------------------------------------------------------------------

// TestPlaquesSection_ShowsTheFiveColumnsAndNeverAKey is the card's list criterion —
// uid, status, bound location, last_ctr, last seen — plus §4.7 asserted on the
// RENDERED BYTES rather than only on the types.
func TestPlaquesSection_ShowsTheFiveColumnsAndNeverAKey(t *testing.T) {
	venues := twoVenues()
	b := plaqueBrowser(t, venues, threePlaques(venues.firstVenueID()))

	html := htmlOf(t, b.do(http.MethodGet, locationsHref, nil))
	for _, want := range []string{
		uidOnWall,        // the uid
		"In service",     // the status, in the manager's words
		"KF St Julians",  // the bound location, by NAME
		"641",            // last_ctr
		"9 Aug 2026",     // last seen
		"Encoded",        // the encoded/pending state
		"pending a wall", // ...and its other half, on the stock plaque
		uidRetired,       // the replaced plaque is STILL LISTED (the history criterion)
		"Replaced by " + uidOnWall,
		"Never tapped", // the stock plaque, as an absence in words
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the plaque list does not contain %q", want)
		}
	}

	// 🔴 §4.7 ON THE WIRE. The type wall is asserted in the domain package; this is
	// the other end of it — whatever the section renders, none of these words may
	// appear, because a screen that names a key is one edit away from printing one.
	lower := strings.ToLower(html)
	for _, forbidden := range []string{"aes_key_ref", "aes key", "key ref", "kek", "cmac", "envelope"} {
		if strings.Contains(lower, forbidden) {
			t.Errorf("the plaque list renders %q; the panel never sees a plaque's key (§4.7)", forbidden)
		}
	}
}

// TestPlaquesSection_ReadingFailureIsAnErrorNotAnEmptyBox. §4.6: "this business has
// no plaques" is a claim, and a timeout is not evidence for it.
func TestPlaquesSection_ReadingFailureIsAnErrorNotAnEmptyBox(t *testing.T) {
	venues := twoVenues()
	plaques := &fakePlaques{screenErr: errors.New("connection refused")}
	b := plaqueBrowser(t, venues, plaques)

	rec := b.do(http.MethodGet, locationsHref, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 when the plaque read failed", rec.Code)
	}
	if html := htmlOf(t, rec); strings.Contains(html, "Nothing in stock") {
		t.Fatal("a failed read rendered the empty state; that is a claim about the business")
	}
}

// --- the card -------------------------------------------------------------------

// TestPlaquesCard_OffersOnlyTheActTheServerWouldACCEPT is the property this section
// has spent four review rounds establishing, applied to plaques: a control that
// leads to a refusal is never rendered, and the card says why instead.
func TestPlaquesCard_OffersOnlyTheActTheServerWouldACCEPT(t *testing.T) {
	wall := uuid.New()
	spare := tenant.Plaque{UID: uidSpare2, Status: tenant.PlaqueUnassigned, Canonical: true}
	onWall := tenant.Plaque{UID: uidOnWall, Status: tenant.PlaqueActive, LocationID: &wall, Canonical: true}
	retired := tenant.Plaque{UID: uidRetired, Status: tenant.PlaqueRetired, LocationID: &wall, Canonical: true}
	frozen := tenant.Plaque{UID: uidInStock, Status: tenant.PlaqueUnassigned, Canonical: false}

	tests := []struct {
		name       string
		venues     *fakeVenues
		plaques    []tenant.Plaque
		open       string
		wantAction string // "" = no form at all
		wantSays   string
	}{
		{
			name: "stock, with venues to mount it at", venues: twoVenues(),
			plaques: []tenant.Plaque{spare}, open: uidSpare2,
			wantAction: plaqueMountHref, wantSays: "Mount this plaque",
		},
		{
			// 🔴 NO VENUES MEANS NO CONTROL. A plaque is mounted at an entrance
			// (tags_location_fk), so a dropdown with nothing in it would offer a
			// submission the server can only refuse.
			name: "stock, but the business has no venues yet", venues: &fakeVenues{},
			plaques: []tenant.Plaque{spare}, open: uidSpare2,
			wantSays: "Add a venue first",
		},
		{
			name: "on a wall, with a spare in the box", venues: twoVenues(),
			plaques: []tenant.Plaque{onWall, spare}, open: uidOnWall,
			wantAction: plaqueReplaceHref, wantSays: "Replace this plaque",
		},
		{
			// 🔴 NO SPARE MEANS NO CONTROL, and the sentence says where a spare comes
			// from: the panel cannot create one, because that would mean holding its key.
			name: "on a wall, with nothing to replace it with", venues: twoVenues(),
			plaques: []tenant.Plaque{onWall}, open: uidOnWall,
			// ⚠️ AND THE SENTENCE NO LONGER ENDS AT "ask us for one". An audit measured
			// what that copy cost: a plaque on the WRONG door needs no spare, it needs
			// taking down — and the card sent the manager to us for a thing they had.
			wantSays: "take it off the wall below",
		},
		{
			name: "already retired", venues: twoVenues(),
			plaques: []tenant.Plaque{retired}, open: uidRetired,
			wantSays: "out of service",
		},
		{
			// The development residue migration 00013 froze. The database refuses every
			// write to it, so the card must not offer one.
			name: "frozen by the canonical-uid constraint", venues: twoVenues(),
			plaques: []tenant.Plaque{frozen}, open: uidInStock,
			wantSays: "cannot be changed",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plaques := &fakePlaques{screen: tenant.PlaqueScreen{Plaques: tc.plaques, Zone: time.UTC}}
			b := plaqueBrowser(t, tc.venues, plaques)
			html := htmlOf(t, b.do(http.MethodGet, locationsHref+"?plaque="+tc.open, nil))

			if !strings.Contains(html, tc.wantSays) {
				t.Errorf("the card does not say %q", tc.wantSays)
			}
			postsTo := func(action string) bool {
				return strings.Contains(html, `action="`+action+`"`)
			}
			if tc.wantAction == "" {
				for _, action := range []string{plaqueMountHref, plaqueReplaceHref} {
					if postsTo(action) {
						t.Errorf("the card offers a form posting to %s, which the server would refuse", action)
					}
				}
				return
			}
			if !postsTo(tc.wantAction) {
				t.Errorf("the card does not post to %s", tc.wantAction)
			}
		})
	}
}

// TestPlaquesSection_OpensEveryRowItLISTS is A1, and it is the defect this section
// has now closed FIVE times in one form or another.
//
// 🔴 WHAT WAS MEASURED, live (tenant 2f2bb0e2…, plaque b27188a6d32e49): the list
// rendered `href="/admin/locations?plaque=b27188a6d32e49"` and following that link
// answered "That plaque id is not one of this business's. Nothing was changed." —
// about a row the same page had printed two lines higher. The cause was a boundary
// that upper-cased a LOOKUP key against a case-SENSITIVE char(14) column, and the
// consequence was phase A's defect 2 word for word.
//
// THE FIXTURE IS A LOWER-CASE UID because that is the only spelling that can
// disagree: 00013's canonical CHECK is NOT VALID, so 18 010 pre-existing rows carry
// one and ListTagsForTenant returns them like any other.
func TestPlaquesSection_OpensEveryRowItLISTS(t *testing.T) {
	const frozenUID = "b27188a6d32e49"
	wall := uuid.New()
	venues := twoVenues()
	plaques := &fakePlaques{screen: tenant.PlaqueScreen{Zone: time.UTC, Plaques: []tenant.Plaque{
		{UID: uidOnWall, Status: tenant.PlaqueActive, LocationID: &wall, Canonical: true},
		// Canonical:false is what the domain sets for a uid the schema would now
		// refuse — the row is real, listed, and unwritable.
		{UID: frozenUID, Status: tenant.PlaqueActive, LocationID: &wall, Canonical: false},
	}}}
	b := plaqueBrowser(t, venues, plaques)

	list := htmlOf(t, b.do(http.MethodGet, locationsHref, nil))
	href := locationsHref + "?plaque=" + frozenUID
	if !strings.Contains(list, href) {
		t.Fatalf("the list does not offer %s; the link must carry the row's OWN spelling", href)
	}

	card := htmlOf(t, b.do(http.MethodGet, href, nil))
	// 🔴 THE LINK OPENS. This is the whole assertion.
	if strings.Contains(card, "We could not find that plaque") {
		t.Fatalf("following the list's own link answered 'not one of this business's' "+
			"about a row it had just listed.\n%s", card)
	}
	if !strings.Contains(card, frozenUID) {
		t.Fatalf("the card is not about %s", frozenUID)
	}
	// AND IT OFFERS NO CONTROL, with the reason — the database refuses every write to
	// this row, so a button here could only fail.
	if !strings.Contains(card, "cannot be changed") {
		t.Fatalf("the card does not say why it offers nothing")
	}
	for _, action := range []string{plaqueMountHref, plaqueReplaceHref} {
		if strings.Contains(card, `action="`+action+`"`) {
			t.Fatalf("the card offers a form posting to %s for a row the database will refuse", action)
		}
	}
}

// TestPlaqueWrites_AFrozenUidREACHESTheDomain is the other half of A1(c).
//
// 🔴 IT IS AN ASSERTION ABOUT REACHABILITY, NOT ABOUT AN ANSWER. `plaque-frozen`
// exists to turn SQLSTATE 23514 into a sentence instead of a 500 — and while the
// boundary upper-cased, NO lower-case uid could survive it, so the refusal was DEAD
// CODE that no test could fire. "A defence whose removal no test notices is not a
// defence" is this task's own lesson; this pins that the value now reaches the
// statement that can raise the 23514, and the domain's unit test pins the mapping.
func TestPlaqueWrites_AFrozenUidREACHESTheDomain(t *testing.T) {
	const frozenUID = "b27188a6d32e49"
	venues := twoVenues()
	plaques := threePlaques(venues.firstVenueID())
	b := plaqueBrowser(t, venues, plaques)

	rec := b.do(http.MethodPost, plaqueMountHref, url.Values{
		"uid": {frozenUID}, "location_id": {venues.firstVenueID().String()},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); strings.Contains(loc, "problem=unreadable") {
		t.Fatalf("the boundary refused a lower-case uid as unreadable (%q); it is a "+
			"LOOKUP key and the column is case-sensitive, so this makes every frozen "+
			"row unreachable and plaque-frozen dead code", loc)
	}
	mounts, _ := plaques.acts()
	if len(mounts) != 1 || mounts[0].UID != frozenUID {
		t.Fatalf("the domain saw %v, want one command carrying %q exactly as typed",
			mounts, frozenUID)
	}
}

// TestPlaquesCard_AUidThatResolvesToNothingIsANSWERED. §4.6: a stale bookmark, a
// hand-edited URL or another business's uid is answered, not silently ignored.
func TestPlaquesCard_AUidThatResolvesToNothingIsANSWERED(t *testing.T) {
	tests := []struct {
		name, query, want string
	}{
		{name: "a canonical uid this business does not have",
			query: "?plaque=AABBCCDDEEFF01", want: "We could not find that plaque"},
		{name: "not a plaque id at all",
			query: "?plaque=" + uuid.New().String(), want: "We could not read that"},
		{name: "the shape the column would TRUNCATE",
			query: "?plaque=AABBCCDDEEFF01ZZZZZZ", want: "We could not read that"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			venues := twoVenues()
			b := plaqueBrowser(t, venues, threePlaques(venues.firstVenueID()))
			html := htmlOf(t, b.do(http.MethodGet, locationsHref+tc.query, nil))
			if !strings.Contains(html, tc.want) {
				t.Fatalf("the section said nothing about %s; want %q", tc.query, tc.want)
			}
		})
	}
}

// TestPlaquesCard_ShowsWHODidWhatAndTheLISTPaysNothingForIt is the render half of
// C7 plus the cost claim, counted in QUERIES.
func TestPlaquesCard_ShowsWHODidWhatAndTheLISTPaysNothingForIt(t *testing.T) {
	venues := twoVenues()
	plaques := threePlaques(venues.firstVenueID())
	plaques.history = map[string][]tenant.PlaqueEvent{
		uidRetired: {
			{Action: tenant.ActionPlaqueRetired, ActorName: "Rita Camilleri",
				At: time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)},
			// An act with NO admin behind it: audit_log.actor_id is nullable because the
			// column is polymorphic and holds NULL for SYSTEM events. BySystem is what
			// says so — an admin stored with an EMPTY full_name also has no name, and
			// calling that act the product's would misattribute it.
			{Action: tenant.ActionPlaqueMounted, BySystem: true,
				At: time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)},
			// 🔴 THE CASE THAT SEPARATES THE TWO. admin_users.full_name is `text NOT NULL`
			// with no non-blank CHECK, so a nameless ADMIN is storable — and must not be
			// reported as the system.
			{Action: tenant.ActionPlaqueMounted, BySystem: false,
				At: time.Date(2026, 6, 1, 7, 0, 0, 0, time.UTC)},
		},
	}
	b := plaqueBrowser(t, venues, plaques)

	// THE LIST PAYS NOTHING. The trail is one bounded query for one plaque; on every
	// row it would be N of them, to answer a question about ONE door.
	b.do(http.MethodGet, locationsHref, nil)
	if plaques.historyCalls != 0 {
		t.Fatalf("the list issued %d trail lookup(s)", plaques.historyCalls)
	}

	html := htmlOf(t, b.do(http.MethodGet, locationsHref+"?plaque="+uidRetired, nil))
	if plaques.historyCalls != 1 {
		t.Fatalf("the card issued %d trail lookup(s), want exactly 1", plaques.historyCalls)
	}
	for _, want := range []string{
		"Who did what",
		"Retired 1 Aug 2026 09:00",
		"Rita Camilleri",
		"Mounted 1 Jul 2026 08:00",
		// The NULL-actor arm, said in words rather than left blank...
		"by the system",
		// ...and the nameless-ADMIN arm, which is a different sentence.
		"by an administrator with no name set",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the card does not show %q", want)
		}
	}
}

// TestPlaquesCard_ATrailFailureIsAnErrorNotAnEmptyHistory. §4.6: "nothing has been
// done to this plaque" is a claim, and a timeout is not evidence for it.
func TestPlaquesCard_ATrailFailureIsAnErrorNotAnEmptyHistory(t *testing.T) {
	venues := twoVenues()
	plaques := threePlaques(venues.firstVenueID())
	plaques.historyErr = errors.New("connection refused")
	b := plaqueBrowser(t, venues, plaques)

	rec := b.do(http.MethodGet, locationsHref+"?plaque="+uidOnWall, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 when the trail read failed", rec.Code)
	}
	if html := htmlOf(t, rec); strings.Contains(html, "Who did what") {
		t.Fatal("an empty history was rendered although the trail could not be read")
	}
}

// TestPlaquesCard_OpensOnlyWhenNoOtherCardDid. At most ONE card is ever open, and
// the precedence is venue, then department, then plaque. Asking for a plaque
// alongside a venue must not open two forms.
func TestPlaquesCard_OpensOnlyWhenNoOtherCardDid(t *testing.T) {
	venues := twoVenues()
	b := plaqueBrowser(t, venues, threePlaques(venues.firstVenueID()))

	html := htmlOf(t, b.do(http.MethodGet,
		locationsHref+"?venue="+venues.firstVenueID().String()+"&plaque="+uidInStock, nil))
	if strings.Contains(html, `action="`+plaqueMountHref+`"`) {
		t.Fatal("both a venue card and a plaque card opened; at most one may")
	}
	if !strings.Contains(html, `action="`+venueSaveHref+`"`) {
		t.Fatal("the venue card did not open, so the precedence is not venue-first")
	}
	// 🔴 AND THE PLAQUE IS NOT REPORTED MISSING, because it was never looked for.
	// Complaining that it could not be FOUND would be a claim about a search that
	// never ran — the defect ?venue=new&department=new already recorded.
	if strings.Contains(html, "We could not find that plaque") {
		t.Fatal("the section reported a plaque missing that it never looked for")
	}
}

// --- the writes -------------------------------------------------------------------

// TestPlaqueWrites_TheBoundaryRefusesTheSHAPEBeforeTheDomainSeesIt is §7 on the
// wire: an unreadable uid never reaches internal/domain/tenant.
//
// 🔴 THE ASSERTION IS "THE DOMAIN WAS NOT CALLED", NOT "THE ANSWER WAS A REFUSAL".
// A handler that passed the value through and let the SQL refuse it would produce
// the same redirect, and the whole point of the boundary is that the truncating
// shape is stopped before a statement sees it.
func TestPlaqueWrites_TheBoundaryRefusesTheSHAPEBeforeTheDomainSeesIt(t *testing.T) {
	tests := []struct {
		name string
		form url.Values
		path string
	}{
		{name: "mount: an over-long uid the column would truncate", path: plaqueMountHref,
			form: url.Values{"uid": {"AABBCCDDEEFF01ZZZZZZ"}, "location_id": {uuid.New().String()}}},
		{name: "mount: no uid at all", path: plaqueMountHref,
			form: url.Values{"location_id": {uuid.New().String()}}},
		{name: "replace: an over-long successor", path: plaqueReplaceHref,
			form: url.Values{"uid": {uidOnWall}, "successor": {"AABBCCDDEEFF01ZZZZZZ"}}},
		{name: "replace: a uuid where a plaque id belongs", path: plaqueReplaceHref,
			form: url.Values{"uid": {uidOnWall}, "successor": {uuid.New().String()}}},
		{
			// 🔴 THE CASE THAT SEPARATES THE TWO BOUNDARIES, and the one a mutation slipped
			// through. A LOWER-CASE successor is a legitimate LOOKUP key and an illegal
			// value to WRITE: `replaced_by`'s own SQL predicate is `~ '^[0-9A-F]{14}$'` on
			// the uncast parameter. Validating it with PlaqueRef compiles once the value is
			// cast into CanonicalUID, and the whole suite stayed green until this row —
			// the domain re-validation would still refuse it, but one layer late and with
			// the wrong sentence.
			name: "replace: a lower-case successor, which may be looked up but never written",
			path: plaqueReplaceHref,
			form: url.Values{"uid": {uidOnWall}, "successor": {"b27188a6d32e49"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			venues := twoVenues()
			plaques := threePlaques(venues.firstVenueID())
			b := plaqueBrowser(t, venues, plaques)

			rec := b.do(http.MethodPost, tc.path, tc.form)
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want 303", rec.Code)
			}
			if loc := rec.Header().Get("Location"); !strings.Contains(loc, "problem=unreadable") {
				t.Fatalf("Location = %q, want problem=unreadable", loc)
			}
			mounts, replaces := plaques.acts()
			if len(mounts) != 0 || len(replaces) != 0 {
				t.Fatalf("the domain was called (%d mounts, %d replaces) with a value the "+
					"boundary was supposed to refuse", len(mounts), len(replaces))
			}
		})
	}
}

// TestPlaqueWrites_TheCommandCarriesTheSESSIONsTenantAndActor. §4.5 and §4.7: the
// tenant and the actor are never inputs. A form cannot name either.
func TestPlaqueWrites_TheCommandCarriesTheSESSIONsTenantAndActor(t *testing.T) {
	venues := twoVenues()
	plaques := threePlaques(venues.firstVenueID())
	b := plaqueBrowser(t, venues, plaques)

	rec := b.do(http.MethodPost, plaqueMountHref, url.Values{
		"uid":         {uidInStock},
		"location_id": {venues.firstVenueID().String()},
		// A caller trying to name them. Both are ignored: they are not read at all.
		"tenant_id": {uuid.New().String()},
		"actor_id":  {uuid.New().String()},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	mounts, _ := plaques.acts()
	if len(mounts) != 1 {
		t.Fatalf("mounts = %d, want 1", len(mounts))
	}
	if mounts[0].TenantID != panelTestTenant {
		t.Errorf("TenantID = %s, want the SESSION's %s", mounts[0].TenantID, panelTestTenant)
	}
	if mounts[0].ActorID != panelTestAdmin {
		t.Errorf("ActorID = %s, want the SESSION's %s", mounts[0].ActorID, panelTestAdmin)
	}
	if mounts[0].UID != uidInStock {
		t.Errorf("UID = %s, want %s", mounts[0].UID, uidInStock)
	}
}

// TestPlaqueWrites_EveryDomainRefusalBecomesItsOWNSentence. §4.6: a refused act is
// never silent, and each refusal is a different thing for a manager to do about it.
func TestPlaqueWrites_EveryDomainRefusalBecomesItsOWNSentence(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "no such plaque", err: tenant.ErrUnknownPlaque, want: "problem=unknown-plaque"},
		{name: "the spare was mounted first", err: tenant.ErrPlaqueNotInStock, want: "problem=plaque-not-stock"},
		{name: "it is not on a wall any more", err: tenant.ErrPlaqueNotOnAWall, want: "problem=plaque-not-active"},
		{name: "a plaque cannot replace itself", err: tenant.ErrSamePlaque, want: "problem=same-plaque"},
		{name: "the row is frozen", err: tenant.ErrPlaqueFrozen, want: "problem=plaque-frozen"},
		{name: "another business's venue", err: tenant.ErrUnknownVenue, want: "problem=unknown-venue"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			venues := twoVenues()
			plaques := threePlaques(venues.firstVenueID())
			plaques.replaceErr = tc.err
			b := plaqueBrowser(t, venues, plaques)

			rec := b.do(http.MethodPost, plaqueReplaceHref, url.Values{
				"uid": {uidOnWall}, "successor": {uidInStock},
				confirmField: {armReplacement(t, b, uidOnWall)},
			})
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want 303", rec.Code)
			}
			if loc := rec.Header().Get("Location"); !strings.Contains(loc, tc.want) {
				t.Fatalf("Location = %q, want %s", loc, tc.want)
			}
			// And the sentence really renders, rather than only living in the URL.
			html := htmlOf(t, b.do(http.MethodGet, rec.Header().Get("Location"), nil))
			if strings.Contains(html, "We could not load your venues") {
				t.Fatalf("the word fell through to the default sentence, which is about a "+
					"failed READ and is false here: %s", tc.want)
			}
		})
	}
}

// TestPlaqueWrites_MountNeedsNoConfirmationAndReplaceDoes pins BOTH halves of the
// asymmetry, so it cannot drift into "nothing is gated" or "everything is".
//
// 🔴 THE ASYMMETRY IS A DECISION WITH A MEASUREMENT UNDER IT (2026-08-09). Nothing
// either act writes is destroyed — 00004 revokes DELETE on `tags` and every column
// is schema-reversible — but the PRODUCT ships no route that reverses a retirement,
// so a mis-pressed replacement leaves an entrance where every tap is refused (§5 row
// 1) until somebody physically swaps the plaque. A mount has no such failure mode: it
// takes a plaque out of a box, and the worst outcome is the wrong door.
func TestPlaqueWrites_MountNeedsNoConfirmationAndReplaceDoes(t *testing.T) {
	t.Run("a replacement with NO confirmation is refused and writes nothing", func(t *testing.T) {
		venues := twoVenues()
		plaques := threePlaques(venues.firstVenueID())
		b := plaqueBrowser(t, venues, plaques)

		rec := b.do(http.MethodPost, plaqueReplaceHref, url.Values{
			"uid": {uidOnWall}, "successor": {uidInStock},
		})
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "problem=confirm-required") {
			t.Fatalf("Location = %q, want problem=confirm-required", loc)
		}
		if _, replaces := plaques.acts(); len(replaces) != 0 {
			t.Fatalf("an unconfirmed replacement reached the domain (%d)", len(replaces))
		}
	})

	t.Run("an un-mount with NO confirmation is refused and writes nothing", func(t *testing.T) {
		// 🔴 THE UNDO IS GATED TOO, and for a reason a mount is not: a plaque off the
		// wall is `unassigned`, so §5 row 1 refuses every tap at that door until
		// something is mounted there. Reversible, but service-affecting.
		wall := uuid.New()
		venues := twoVenues()
		plaques := &fakePlaques{screen: tenant.PlaqueScreen{Zone: time.UTC, Plaques: []tenant.Plaque{
			{UID: uidOnWall, Status: tenant.PlaqueActive, LocationID: &wall, Canonical: true},
		}}}
		b := plaqueBrowser(t, venues, plaques)

		rec := b.do(http.MethodPost, plaqueUnmountHref, url.Values{"uid": {uidOnWall}})
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "problem=confirm-required") {
			t.Fatalf("Location = %q, want problem=confirm-required", loc)
		}
		if n := len(plaques.unmounts()); n != 0 {
			t.Fatalf("an unconfirmed un-mount reached the domain (%d)", n)
		}
	})

	t.Run("an un-mount confirmed for the REPLACE action is refused", func(t *testing.T) {
		// The action binding, on the fifth act: a value minted at the replacement card
		// must not open the un-mount gate. Same subject, same session, so the action is
		// the only thing standing in the way.
		wall := uuid.New()
		venues := twoVenues()
		plaques := &fakePlaques{screen: tenant.PlaqueScreen{Zone: time.UTC, Plaques: []tenant.Plaque{
			{UID: uidOnWall, Status: tenant.PlaqueActive, LocationID: &wall, Canonical: true},
			{UID: uidInStock, Status: tenant.PlaqueUnassigned, Canonical: true},
		}}}
		b := plaqueBrowser(t, venues, plaques)
		replaceToken := armReplacement(t, b, uidOnWall)

		rec := b.do(http.MethodPost, plaqueUnmountHref, url.Values{
			"uid": {uidOnWall}, confirmField: {replaceToken},
		})
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "problem=confirm") {
			t.Fatalf("Location = %q, want a confirmation refusal", loc)
		}
		if n := len(plaques.unmounts()); n != 0 {
			t.Fatalf("a replacement confirmation opened the un-mount gate (%d)", n)
		}
	})

	t.Run("an un-mount WITH its own confirmation goes through", func(t *testing.T) {
		// THE POSITIVE CONTROL. Without it every assertion above would pass on a route
		// that refused everything.
		wall := uuid.New()
		venues := twoVenues()
		plaques := &fakePlaques{screen: tenant.PlaqueScreen{Zone: time.UTC, Plaques: []tenant.Plaque{
			{UID: uidOnWall, Status: tenant.PlaqueActive, LocationID: &wall, Canonical: true},
		}}}
		b := plaqueBrowser(t, venues, plaques)

		html := htmlOf(t, b.do(http.MethodGet,
			locationsHref+"?plaque="+uidOnWall+"&confirm=unmount", nil))
		m := confirmTokenRE.FindStringSubmatch(html)
		if m == nil {
			t.Fatalf("the un-mount step carried no confirmation.\n%s", html)
		}
		rec := b.do(http.MethodPost, plaqueUnmountHref, url.Values{
			"uid": {uidOnWall}, confirmField: {m[1]},
		})
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "done=plaque-unmounted") {
			t.Fatalf("Location = %q, want done=plaque-unmounted", loc)
		}
		if n := len(plaques.unmounts()); n != 1 {
			t.Fatalf("un-mounts reaching the domain = %d, want 1", n)
		}
	})

	t.Run("a mount needs none", func(t *testing.T) {
		venues := twoVenues()
		plaques := threePlaques(venues.firstVenueID())
		b := plaqueBrowser(t, venues, plaques)

		rec := b.do(http.MethodPost, plaqueMountHref, url.Values{
			"uid": {uidInStock}, "location_id": {venues.firstVenueID().String()},
		})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303", rec.Code)
		}
		if loc := rec.Header().Get("Location"); strings.Contains(loc, "confirm") {
			t.Fatalf("Location = %q; a mount must not be gated", loc)
		}
		mounts, _ := plaques.acts()
		if len(mounts) != 1 {
			t.Fatalf("mounts = %d, want 1", len(mounts))
		}
	})

	t.Run("the mount card MINTS no confirmation, measured on the cookie", func(t *testing.T) {
		// 🔴 ON THE COOKIE, NOT ON THE HTML. This assertion used to read the body, and
		// an audit made the card MINT without RENDERING — a signed value written to the
		// browser that nothing ever checks — with the whole package still green.
		venues := twoVenues()
		plaques := &fakePlaques{screen: tenant.PlaqueScreen{Zone: time.UTC, Plaques: []tenant.Plaque{
			{UID: uidInStock, Status: tenant.PlaqueUnassigned, Canonical: true},
		}}}
		b := plaqueBrowser(t, venues, plaques)
		rec := b.do(http.MethodGet, locationsHref+"?plaque="+uidInStock, nil)
		if mintedConfirmation(rec) {
			t.Fatal("the mount card MINTED a confirmation nothing checks")
		}
		if confirmTokenRE.MatchString(htmlOf(t, rec)) {
			t.Fatal("the mount card rendered a confirmation field")
		}
	})

	t.Run("a card that mints EXACTLY ONE confirmation per render", func(t *testing.T) {
		// 🔴 THE COOKIE IS ONE PER BROWSER, so a card carrying two armed forms would
		// leave whichever one the manager did not submit unusable — a control that can
		// only fail. This is why the un-mount is a LINK on the replacement card and its
		// own mode behind it, rather than a second button.
		wall := uuid.New()
		venues := twoVenues()
		plaques := &fakePlaques{screen: tenant.PlaqueScreen{Zone: time.UTC, Plaques: []tenant.Plaque{
			{UID: uidOnWall, Status: tenant.PlaqueActive, LocationID: &wall, Canonical: true},
			{UID: uidInStock, Status: tenant.PlaqueUnassigned, Canonical: true},
		}}}
		b := plaqueBrowser(t, venues, plaques)

		card := b.do(http.MethodGet, locationsHref+"?plaque="+uidOnWall, nil)
		if !mintedConfirmation(card) {
			t.Fatal("the replacement card minted nothing")
		}
		html := htmlOf(t, card)
		if n := len(confirmTokenRE.FindAllString(html, -1)); n != 1 {
			t.Fatalf("the card rendered %d confirmation fields, want exactly 1", n)
		}
		// templ escapes the ampersand, so the query key is what is matched — asserting
		// on the raw "&confirm=" would be asserting on the escaping.
		if !strings.Contains(html, "confirm=unmount") {
			t.Fatal("the card offers no way to take the plaque off the wall")
		}

		// The un-mount step WITHDRAWS the replacement form and arms its own.
		step := b.do(http.MethodGet, locationsHref+"?plaque="+uidOnWall+"&confirm=unmount", nil)
		if !mintedConfirmation(step) {
			t.Fatal("the un-mount step minted nothing")
		}
		html = htmlOf(t, step)
		if n := len(confirmTokenRE.FindAllString(html, -1)); n != 1 {
			t.Fatalf("the un-mount step rendered %d confirmation fields, want exactly 1", n)
		}
		if strings.Contains(html, `action="`+plaqueReplaceHref+`"`) {
			t.Fatal("the un-mount step still offers the replacement form; its token was " +
				"overwritten by this render and pressing it could only fail")
		}
	})

	t.Run("a replacement confirmed for ANOTHER plaque is refused", func(t *testing.T) {
		// The second-tab case. The value is genuine, unexpired and this session's — and
		// it was minted for a different subject, which is its own sentence.
		venues := twoVenues()
		plaques := threePlaques(venues.firstVenueID())
		b := plaqueBrowser(t, venues, plaques)
		// Arm for the RETIRED plaque's card... which offers no control, so arm for the
		// only other subject the section will mint for: a venue removal.
		token := armReplacement(t, b, uidOnWall)
		other := threePlaques(venues.firstVenueID())
		other.screen.Plaques[1].UID = uidSpare2
		b2 := plaqueBrowser(t, venues, other)
		rec := b2.do(http.MethodPost, plaqueReplaceHref, url.Values{
			"uid": {uidSpare2}, "successor": {uidInStock}, confirmField: {token},
		})
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "problem=confirm") {
			t.Fatalf("Location = %q, want a confirmation refusal", loc)
		}
		if _, replaces := other.acts(); len(replaces) != 0 {
			t.Fatalf("a confirmation minted for another plaque reached the domain")
		}
	})
}

// TestPlaqueConfirmation_RefusesAVersion2Payload. Payload v3 widened the subject
// from a uuid to an opaque string; an older value is treated as MALFORMED rather
// than repaired, which is deactivateconfirm.go's part 9.
//
// 🔴 THE OLD VALUE IS BUILT AND SIGNED WITH THE SERVER'S OWN KEY, so the only thing
// that can refuse it is the VERSION check. A forgery would be refused by the
// signature and would prove nothing about versioning.
func TestPlaqueConfirmation_RefusesAVersion2Payload(t *testing.T) {
	c, err := newAdminConfirm(adminTestConfig())
	if err != nil {
		t.Fatalf("newAdminConfirm: %v", err)
	}
	session := uuid.New()
	v2 := strings.Join([]string{
		"2", strconv.FormatInt(time.Now().Unix(), 10),
		confirmActionReplacePlaque, uidOnWall, session.String(),
	}, "|")
	token := base64.RawURLEncoding.EncodeToString([]byte(v2)) + "." +
		base64.RawURLEncoding.EncodeToString(c.sign(v2))

	if err := c.parse(token, confirmActionReplacePlaque, uidOnWall, session); !errors.Is(err, errConfirmInvalid) {
		t.Fatalf("a version-2 payload parsed as %v, want errConfirmInvalid", err)
	}
	// THE POSITIVE CONTROL: the same fields at version 3 DO verify, so the refusal
	// above is about the version and not about the fields.
	fresh, err := c.mint(confirmActionReplacePlaque, uidOnWall, session)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if err := c.parse(fresh, confirmActionReplacePlaque, uidOnWall, session); err != nil {
		t.Fatalf("a version-3 confirmation does not open its own gate: %v", err)
	}
}

// TestPlaqueConfirmation_RefusesASubjectCarryingTheSeparator. The uuid type used to
// make this impossible; a string subject does not, and payload/parse split on "|".
func TestPlaqueConfirmation_RefusesASubjectCarryingTheSeparator(t *testing.T) {
	c, err := newAdminConfirm(adminTestConfig())
	if err != nil {
		t.Fatalf("newAdminConfirm: %v", err)
	}
	session := uuid.New()
	if _, err := c.mint(confirmActionReplacePlaque, "AABBCC|DDEEFF01", session); err == nil {
		t.Fatal("mint signed a subject carrying the field separator")
	}
	if _, err := c.mint("plaque|replace", uidOnWall, session); err == nil {
		t.Fatal("mint signed an action carrying the field separator")
	}
	// POSITIVE CONTROL: an ordinary subject still signs.
	if _, err := c.mint(confirmActionReplacePlaque, uidOnWall, session); err != nil {
		t.Fatalf("mint refused an ordinary subject: %v", err)
	}
}

// --- the two DERIVATIONS this section's nets are built on ------------------------
//
// 🔴 WHY DERIVED AND NOT LISTED, said once for both. Six times in this task a
// hand-maintained N-list grew an N+1 and opened a hole: a guardrail deny-list missed
// a fourth tag status, a §4.7 scan named seven forbidden words, the type wall was a
// deny-list, venueDoneWords was hand-counted, the trail's action switch named two of
// three, and the write-chain nets named two of three routes. A list a human keeps in
// step is a list that is correct until somebody is busy.

// plaqueActionsFromSource reads the plaque audit actions out of
// internal/domain/tenant's OWN const declarations.
//
// 🔴 IT PARSES THE WHOLE PACKAGE, NOT ONE FILE, AND THAT IS A REPAIR. It read
// plaque.go alone while claiming "exactly the set internal/domain/tenant can write"
// — a PACKAGE claim behind a FILE scan — and an audit beat it in one line by
// declaring the constant in venue.go. That is not exotic: this package already
// scatters its audit actions across THREE files and, inside venue.go, across two
// const blocks 420 lines apart. Every non-test file is parsed now, so where somebody
// puts the constant does not matter.
//
// 🔴 AND A VALUE IT CANNOT READ IS A FAILURE, NOT A SKIP. The second bypass was
// `ActionPlaqueLost = "plaque." + "lost"` — a BinaryExpr rather than a BasicLit, in
// the right file, with the right prefix — and the loop's `continue` swallowed it
// silently. Silence is the one answer a derivation may never give: it is
// indistinguishable from "there was nothing there". A constant this scan cannot
// evaluate now fails loudly and names itself.
//
// ⚠️ WHAT IT STILL CANNOT SEE — MEASURED, NOT GUESSED, and left as a COUNTED limit
// rather than chased. Each shape below was compiled and run against these tests on
// 2026-08-10; a counted hole is safer than one claimed shut.
//
//	SHAPE                                              CAUGHT?  BY WHAT
//	const/var named ActionPlaque*, string literal      yes      the name arm
//	the same declared with `var` rather than `const`   yes      both Tok arms
//	a value like `"plaque." + "lost"`                  yes      the loud failure above
//	a name that does NOT match, value "plaque.…"       yes      the VALUE-prefix arm
//	`Action: "plaque.x"` written inline at the write   yes      TestPlaqueActions_Are
//	`Action: "plaque." + "x"` (a BinaryExpr)           yes        WrittenOnlyFromThe
//	                                                            DeclaredConstants,
//	                                                            whose value check is an
//	                                                            ALLOW-LIST of shapes
//	a name that does not match AND a value that does   NO       — and it cannot reach
//	  not start with "plaque." (e.g. "plq.seized")              this screen at all:
//	                                                            ListPlaqueHistory
//	                                                            filters `action LIKE
//	                                                            'plaque.%'`, so such a
//	                                                            row is never rendered.
//	                                                            Measured both ways.
//	an action ASSEMBLED AT RUN TIME and referenced as  NO       the write-site check
//	  a name (`var a = "plaque." + f()`; `Action: a`)           accepts an Ident, and
//	                                                            no AST can evaluate f.
//	                                                            Nothing does this
//	                                                            today; closing it needs
//	                                                            a run-time assertion at
//	                                                            the write, which is a
//	                                                            different mechanism.
//	an action written from ANOTHER package             NO       this scan reads
//	                                                            internal/domain/tenant
//	                                                            only. 🔴 AND IT HAPPENS
//	                                                            — see the block below.
//
// 🔴 THE "ANOTHER PACKAGE" ROW IS NO LONGER HYPOTHETICAL, AND THE SENTENCE THAT USED
// TO SIT IN IT WAS FALSE WHEN IT WAS WRITTEN (corrected 2026-08-24, M8-05 FAZ
// B2c-2b). It said "Nothing else writes `plaque.*` (grepped), and doing so would be a
// visible edit in a new package." Measured on this tree:
//
//	grep -rn 'Action:  *ActionPlaque' --include='*.go' internal/ | grep -v _test
//	  -> internal/encode/rows.go  Action:  ActionPlaqueLoaded
//	  -> internal/encode/rows.go  Action:   ActionPlaqueEncoded
//	  -> internal/encode/rows.go  Action:   ActionPlaqueUnmarked
//
// internal/encode has written the first two since FAZ B2c-2a and the third since a
// security audit in FAZ B2c-2b, and that phase wired an HTTP endpoint that drives all
// three, so they reach a real audit_log.
//
// ⚠️ NO LINE NUMBERS ARE QUOTED ANY MORE, AND THAT IS THE REPAIR RATHER THAN LAZINESS.
// This block used to print ":261" and ":303"; both had moved by the next round, and the
// third write was added without the block being re-run at all — so a published
// measurement was wrong in two ways at once. A file:line pair in a comment is a second
// representation of something the command already returns.
//
// 🔴 WHY THE HOLE THIS ROW DESCRIBES IS STILL SHUT, WHICH IS THE PART WORTH KEEPING.
// The danger was never "somebody writes plaque.* elsewhere"; it was "an action reaches
// the trail card as a word nobody has a rendering for". internal/encode does not
// DECLARE its own vocabulary — rows.go aliases tenant.ActionPlaqueLoaded,
// tenant.ActionPlaqueEncoded and tenant.ActionPlaqueUnmarked, so the scan below sees
// all three, and TestPlaqueTrail_NamesEveryActionTheDOMAINCanWrite requires a word for
// each
// (plaques.go carries "Loaded into stock" and "Encoded"). rows.go's own header says
// this is deliberate: "Declaring the two constants where the scan already looks turns
// 'the panel prints a raw database word at a manager' into a RED TEST."
//
// ⚠️ WHAT REMAINS UNCOVERED, NARROWED RATHER THAN CLOSED: a package that declares its
// OWN plaque.* literal instead of aliasing. Nothing does; the alias pattern is the one
// the tree uses and rows.go argues for. This row is the reason to check when a third
// writer appears.
//
// 🔴 AND THE TEST NAMED FOR THE WRITE SIDE IS THE ONE ABOVE, NOT THIS ONE. An earlier
// version of this paragraph pointed the whole limit list at
// TestPlaqueActions_AreWrittenOnlyFromTheDeclaredConstants and said it held "these
// constants" — it does not: it constrains the SHAPE OF THE VALUE at a write site and
// says nothing about how a name was declared. Two nets, two jobs.
//
// go/ast RATHER THAN A REGEXP, for the reason this repo has twice relearned: a text
// scan over source is beaten by a comment, a string literal or a line break.
func plaqueActionsFromSource(t *testing.T) map[string]string {
	t.Helper()
	const dir = "../domain/tenant"
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", dir, err)
	}
	out := map[string]string{}
	files := 0
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			files++
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				// 🔴 const AND var. An audit declared `var ActionPlaqueSeized =
				// "plaque.seized"` — right prefix, right file, right package, genuinely
				// written by the domain — and ALL THREE nets stayed green because this
				// walk read only token.CONST. A vocabulary scan that depends on which
				// keyword somebody typed is a scan with a keyword-shaped hole.
				if !ok || (gen.Tok != token.CONST && gen.Tok != token.VAR) {
					continue
				}
				for _, spec := range gen.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, ident := range vs.Names {
						byName := strings.HasPrefix(ident.Name, "ActionPlaque")
						var value string
						if i < len(vs.Values) {
							if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
								value = strings.Trim(lit.Value, `"`)
							}
						}
						byValue := strings.HasPrefix(value, "plaque.")
						if !byName && !byValue {
							continue
						}
						if i >= len(vs.Values) {
							t.Fatalf("%s declares %s with no value this scan can read; a "+
								"derivation that shrugs is indistinguishable from one that "+
								"found nothing", name, ident.Name)
						}
						if value == "" {
							t.Fatalf("%s declares %s as a %T rather than a string literal. This "+
								"scan cannot evaluate it, and skipping it silently is how a "+
								"declaration slips past the completeness check below — measured, "+
								"with `\"plaque.\" + \"lost\"`.", name, ident.Name, vs.Values[i])
						}
						out[ident.Name] = value
					}
				}
			}
		}
	}
	// ANTI-VACUITY, AND THE SENTENCE NOW MATCHES THE MECHANISM. This floor guards
	// against a BROKEN SCAN — a wrong directory, a parser change, a rename — not
	// against somebody deleting an action: a floor below today's count cannot notice a
	// deletion, and an earlier comment here claimed it could. What notices a deletion
	// is the SURPLUS arm of the test below, where a word with no action fails.
	if files < 3 || len(out) < 2 {
		t.Fatalf("parsed %d file(s) and derived %d plaque action(s) from %s (%v); the "+
			"scan is not reading the package and every assertion below is vacuous",
			files, len(out), dir, out)
	}
	return out
}

// TestPlaqueActions_AreWrittenOnlyFromTheDeclaredConstants closes the one gap the
// scan above cannot: an action string that never appears as a constant.
//
// 🔴 IT READS THE PACKAGE'S OWN SOURCE FOR `Action:` FIELDS in audit.Event literals
// and requires every plaque one to be a CONSTANT REFERENCE rather than a literal. A
// string written inline would be an action the completeness check never hears about.
//
// ⚠️ THE FALSE ALARM AND THE FIX, AND THE FIRST FIX WAS WRONG (2026-08-11, M6-09
// phase A). internal/policy's vocabulary is namespaced ACTIONS, so the policies
// screen's read side has a type with an Action field and writes
// `Action: string(action)` -- a CallExpr, which this walk flagged as an audit
// action written as an expression. That is a false alarm, and the kind that gets a
// real net weakened to silence it.
//
// 🔴 THE FIRST ATTEMPT NARROWED THE WALK TO audit.Event LITERALS AND LOST REAL
// COVERAGE. Measured by an auditor, same run, two defects:
//
//	audit.Event{Action: "plaque.mounted"}                      RED  -> RED    kept
//	plan := struct{ Action string }{Action: "plaque.mounted"}
//	  ... audit.Event{Action: plan.Action}                     RED  -> GREEN  LOST
//
// The second is a real bypass and nothing else in the suite catches it. It also
// missed `[]audit.Event{{Action: …}}`, whose inner literal has no type of its own.
// So the walk is BROAD again -- every `Action:` key in the package -- and what
// changed instead is the ALLOW-LIST OF VALUE SHAPES: a declared name, or a
// CONVERSION of one, recursively (isDeclaredNameExpr). That admits the policies
// screen's line, still rejects a bare string literal wherever it is written, still
// rejects the BinaryExpr an earlier audit used, and rejects
// `string("plaque.seized")` -- a conversion is accepted only around a NAME.
//
// ⚠️ WHAT IT STILL CANNOT SEE, COUNTED RATHER THAN IMPLIED, and the first item is
// the one that makes the recovered coverage narrower than it looks. All four were
// measured by an auditor against this walk:
//
//	Action: plan.Action  where plan is struct{ Action string }{Action: "…"}   CAUGHT
//	Action: plan.A       where plan is struct{ A      string }{A:      "…"}   ESCAPES
//	Action: zzLoose      where `var zzLoose = "plaque.mounted"` is package level ESCAPES
//	Action: helper()     any function call                                    ESCAPES
//
// The struct detour is caught ONLY because the intermediate field is also called
// Action -- this walk keys on the KEY NAME, so renaming the field to anything else
// takes it back out of view. That is a real limit, not a technicality, and it is
// written here rather than left for the next auditor to rediscover: what the walk
// guarantees is that a bare string cannot sit next to an `Action:` key ANYWHERE in
// the package, and nothing more. Following a value to its declaration needs type
// information this test does not load.
// TestPlaqueTrail_NamesEveryActionTheDOMAINCanWrite is the behavioural half beside it.
func TestPlaqueActions_AreWrittenOnlyFromTheDeclaredConstants(t *testing.T) {
	const dir = "../domain/tenant"
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", dir, err)
	}
	seen := 0
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				kv, ok := n.(*ast.KeyValueExpr)
				if !ok {
					return true
				}
				key, ok := kv.Key.(*ast.Ident)
				if !ok || key.Name != "Action" {
					return true
				}
				seen++
				// 🔴 AN ALLOW-LIST OF VALUE SHAPES, NOT A BAN ON LITERALS. The first
				// version rejected *ast.BasicLit and accepted everything else — so
				// `Action: prefix + "seized"` (a BinaryExpr) passed, which is the same
				// bypass that beat the vocabulary scan one layer up. isDeclaredNameExpr
				// carries the rule and its own negative control.
				if !isDeclaredNameExpr(kv.Value) {
					t.Errorf("%s writes an action as a %T; it must be a declared name, or a "+
						"conversion of one, or neither the vocabulary scan nor the render "+
						"completeness check can see it", name, kv.Value)
				}
				return true
			})
		}
	}
	// ANTI-VACUITY: this package writes several audit actions, so finding none means
	// the walk is looking at the wrong nodes.
	if seen < 5 {
		t.Fatalf("found %d Action: field(s) in %s; the walk is not reading the writes", seen, dir)
	}
	t.Logf("Action: fields checked in %s: %d", dir, seen)
}

// isDeclaredNameExpr reports whether e is a declared name, or a conversion of one.
//
// A CONVERSION IS ACCEPTED ONLY AROUND A NAME, which is what keeps
// `string("plaque.seized")` out while letting `string(action)` in. The recursion is
// what makes it a rule rather than a special case for one call site.
func isDeclaredNameExpr(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.Ident, *ast.SelectorExpr:
		return true
	case *ast.CallExpr:
		if len(v.Args) != 1 {
			return false
		}
		switch v.Fun.(type) {
		case *ast.Ident, *ast.SelectorExpr:
			return isDeclaredNameExpr(v.Args[0])
		}
	}
	return false
}

// TestDeclaredNameExpr_AcceptsAConversionAndRefusesALiteral is the matcher's own
// negative control. Without it, widening the allow-list to silence a false alarm
// could widen it to everything and nothing would say so.
func TestDeclaredNameExpr_AcceptsAConversionAndRefusesALiteral(t *testing.T) {
	for src, want := range map[string]bool{
		"ActionPlaqueMounted":        true,
		"audit.ActionPlaqueMounted":  true,
		"string(action)":             true,
		"policy.Action(a.Action)":    true,
		`"plaque.mounted"`:           false,
		`string("plaque.mounted")`:   false,
		`prefix + "seized"`:          false,
		`fmt.Sprintf("%s", a)`:       false,
		`map[string]string{}["x"]`:   false,
		`[]string{"plaque.lost"}[0]`: false,
	} {
		e, err := parser.ParseExpr(src)
		if err != nil {
			t.Fatalf("parsing %q: %v", src, err)
		}
		if got := isDeclaredNameExpr(e); got != want {
			t.Errorf("isDeclaredNameExpr(%s) = %v, want %v", src, got, want)
		}
	}
}

// TestPlaqueTrail_NamesEveryActionTheDOMAINCanWrite is F1's closure.
//
// 🔴 THE PANEL PRINTED A RAW DATABASE WORD AT A MANAGER and no test saw it:
// "plaque.unmounted 10 Aug 2026 02:17 · E2E Owner". The switch that rendered the
// trail named two actions; the same change set had added a third.
func TestPlaqueTrail_NamesEveryActionTheDOMAINCanWrite(t *testing.T) {
	actions := plaqueActionsFromSource(t)
	for constName, action := range actions {
		word, ok := plaqueActionWords[action]
		if !ok {
			t.Errorf("%s (%q) has no word in plaqueActionWords, so the panel would print "+
				"the raw database action at a manager", constName, action)
			continue
		}
		if word == action {
			t.Errorf("%s (%q) maps to itself; that is the raw word, not the manager's",
				constName, action)
		}
		// AND IT REALLY REACHES THE VIEW, not just the map.
		got := plaqueTrailView([]tenant.PlaqueEvent{{Action: action}}, time.UTC)
		if len(got) != 1 || got[0].What != word {
			t.Errorf("plaqueTrailView(%q) rendered %v, want %q", action, got, word)
		}
	}
	// AND NOTHING EXTRA: a word for an action the domain cannot write is a sentence
	// only a database edit could produce, which is the unreachable-vocabulary defect
	// venueDoneWords already answers for the query string.
	declared := map[string]bool{}
	for _, a := range actions {
		declared[a] = true
	}
	for action := range plaqueActionWords {
		if !declared[action] {
			t.Errorf("plaqueActionWords names %q, which internal/domain/tenant cannot write", action)
		}
	}
}

// plaqueWriteRoutes walks the REAL router and returns every POST this section
// mounts, keyed by path.
//
// 🔴 IT IS WALKED, NOT LISTED, AND THAT IS F2's CLOSURE. The two nets below used to
// range over a hand-written {mount, replace} pair; the un-mount route shipped in the
// same change and was in NEITHER. A mutation that moved it from ProtectWriting to
// mountSections — i.e. onto the READ chain, so a cross-origin POST passes the Origin
// gate and spends a signed-in manager's session budget — compiled and left the whole
// package green. That is the M6-04 defect plaqueactions.go's own header quotes.
//
// THE PREFIX IS THE SECTION'S OWN URL, so a route added to this section is covered
// the day it is added, and the panel's other POSTs (sign-in, sign-out) are not
// dragged in — they have their own chains and their own tests.
//
// ⚠️ AND THE PREFIX IS THIS NET'S OWN LIMIT, COUNTED RATHER THAN CLOSED. A future
// plaque write mounted at some OTHER path — /admin/plaques/…, a nested router with a
// different base — is invisible here, which is one layer up from the defect this test
// was written for. It is left open deliberately (the second stopping rule): widening
// the walk to EVERY panel POST would drag in /admin/login and /admin/logout, which
// are legitimately not Origin-gated in the same way and have their own tests, so the
// net would need a per-route exemption list — and an exemption list is the shape six
// findings in this task have already been about. What makes the limit survivable:
// every href this section uses is built from locationsHref (grep: the four in
// locations.go and the three in plaques.go), so moving one out of the prefix is a
// visible edit in a file this test's own package owns.
func plaqueWriteRoutes(t *testing.T, h http.Handler) []string {
	t.Helper()
	routes, ok := h.(chi.Routes)
	if !ok {
		t.Fatalf("the harness router is %T, which cannot be walked; this net would be vacuous", h)
	}
	var out []string
	err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method == http.MethodPost && strings.HasPrefix(route, locationsHref+"/") {
			out = append(out, route)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the router: %v", err)
	}
	// ANTI-VACUITY, AND THE SENTENCE MATCHES THE MECHANISM. This floor guards against a
	// BROKEN WALK — a router that was never mounted, a prefix that stopped matching, a
	// chi change — not against somebody deleting a route: a floor below today's count
	// cannot notice a deletion, and an earlier comment here claimed it could. A deleted
	// route is caught by the tests that drive it, not by counting.
	if len(out) < 4 {
		t.Fatalf("walked %d write route(s) under %s (%v); the walk is not reading the "+
			"real router", len(out), locationsHref, out)
	}
	sort.Strings(out)
	return out
}

// TestPlaqueWrites_AreOnTheWRITINGChain. EVERY POST this section mounts must inherit
// floodGate → sameOriginGate → requireAdmin → sessionGate, with the Origin check
// BEFORE the resolver — so a cross-origin flood costs zero database work.
//
// 🔴 THE ROUTE SET IS WALKED OUT OF THE REAL ROUTER, not listed here. This test used
// to name {mount, replace} by hand; the un-mount shipped in the same change and was
// missing, and a mutation that put it on the READ chain left the whole package green
// while a cross-origin POST spent a signed-in manager's session budget.
func TestPlaqueWrites_AreOnTheWRITINGChain(t *testing.T) {
	venues := twoVenues()
	plaques := threePlaques(venues.firstVenueID())
	b := plaqueBrowser(t, venues, plaques)
	routes := plaqueWriteRoutes(t, b.h)
	t.Logf("derived %d write route(s) under %s: %v", len(routes), locationsHref, routes)

	for _, path := range routes {
		t.Run(path, func(t *testing.T) {
			venues := twoVenues()
			plaques := threePlaques(venues.firstVenueID())
			b := plaqueBrowser(t, venues, plaques)
			b.origin = "https://evil.example"

			// Every field any of these routes reads, so the refusal cannot be mistaken for
			// a validation failure on a form this route happens not to want.
			rec := b.do(http.MethodPost, path, url.Values{
				"uid": {uidOnWall}, "successor": {uidInStock}, "id": {uuid.New().String()},
				"location_id": {venues.firstVenueID().String()}, "name": {"x"},
			})
			// ⚠️ THE REFUSAL IS A 303 TO /admin, NOT A 4xx — sameOriginGate redirects
			// rather than erroring, so the STATUS says nothing here.
			if loc := rec.Header().Get("Location"); loc != "/admin" {
				t.Fatalf("a cross-origin POST to %s answered %d -> %q, want the Origin "+
					"refusal (303 -> /admin). A route on the READ chain answers with its own "+
					"vocabulary instead, which is exactly what this measures.",
					path, rec.Code, loc)
			}
			mounts, replaces := plaques.acts()
			if len(mounts)+len(replaces)+len(plaques.unmounts()) != 0 {
				t.Fatalf("a cross-origin POST to %s reached the domain", path)
			}
		})
	}
}

// TestPlaqueWrites_AGETOnAWriteRouteIsNotServed. Every write is a POST, so a link, a
// prefetch or a crawler cannot fire one. Same derived route set, same reason.
func TestPlaqueWrites_AGETOnAWriteRouteIsNotServed(t *testing.T) {
	venues := twoVenues()
	plaques := threePlaques(venues.firstVenueID())
	b := plaqueBrowser(t, venues, plaques)

	for _, path := range plaqueWriteRoutes(t, b.h) {
		rec := b.do(http.MethodGet, path, nil)
		if rec.Code == http.StatusOK || rec.Code == http.StatusSeeOther {
			t.Errorf("GET %s = %d; a write must not be reachable by navigation", path, rec.Code)
		}
	}
	mounts, replaces := plaques.acts()
	if len(mounts)+len(replaces)+len(plaques.unmounts()) != 0 {
		t.Fatal("a GET reached the domain")
	}
}

// --- the acknowledgement (reading C') ----------------------------------------------

// TestPlaqueReceipt_SaysNothingItCannotSee is the heart of the C' mechanism. A
// `?done=` word alone proves nothing; phase A measured one rendering "The venue has
// been removed" in a browser that had never posted anything.
func TestPlaqueReceipt_SaysNothingItCannotSee(t *testing.T) {
	venues := twoVenues()

	tests := []struct {
		name      string
		receipts  map[string]tenant.RemovalReceipt
		query     string
		wantSays  bool
		wantWords string
	}{
		{
			name:      "a mount this admin really made",
			receipts:  map[string]tenant.RemovalReceipt{tenant.ActionPlaqueMounted + "|" + uidInStock: {Found: true, Name: "KF St Julians"}},
			query:     "?done=plaque-mounted&id=" + uidInStock,
			wantSays:  true,
			wantWords: "Plaque mounted at KF St Julians",
		},
		{
			name:      "a replacement this admin really made",
			receipts:  map[string]tenant.RemovalReceipt{tenant.ActionPlaqueRetired + "|" + uidRetired: {Found: true, Name: uidOnWall}},
			query:     "?done=plaque-replaced&id=" + uidRetired,
			wantSays:  true,
			wantWords: "Plaque replaced — the new one is " + uidOnWall,
		},
		{
			// A STRANGER'S URL. No trail row, so nothing is printed — not an error, not
			// a hedge, nothing.
			name: "a word with nothing behind it", query: "?done=plaque-replaced&id=" + uidRetired,
		},
		{
			name: "a word with no id at all", query: "?done=plaque-mounted",
		},
		{
			name: "a word with an unreadable id", query: "?done=plaque-mounted&id=not-a-plaque",
		},
		{
			// 🔴 THE ONE THAT MATTERS MOST. A plain mount writes plaque.mounted for the
			// new uid — exactly as a replacement does. If "replaced" were verified
			// against plaque.mounted, a mere mount would print "Plaque replaced". It is
			// verified against plaque.RETIRED, which only a replacement writes.
			name:     "a MOUNT cannot back the REPLACED word",
			receipts: map[string]tenant.RemovalReceipt{tenant.ActionPlaqueMounted + "|" + uidInStock: {Found: true, Name: "KF St Julians"}},
			query:    "?done=plaque-replaced&id=" + uidInStock,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plaques := threePlaques(venues.firstVenueID())
			plaques.receipts = tc.receipts
			b := plaqueBrowser(t, venues, plaques)
			html := htmlOf(t, b.do(http.MethodGet, locationsHref+tc.query, nil))

			said := strings.Contains(html, "Plaque mounted") || strings.Contains(html, "Plaque replaced")
			if said != tc.wantSays {
				t.Fatalf("the section printed an acknowledgement = %v, want %v", said, tc.wantSays)
			}
			if tc.wantSays && !strings.Contains(html, tc.wantWords) {
				t.Fatalf("the heading does not read %q", tc.wantWords)
			}
			// ANTI-VACUITY: whatever happened above, the list itself must still render —
			// otherwise "said nothing" could mean "rendered nothing".
			if !strings.Contains(html, uidOnWall) {
				t.Fatal("the plaque list did not render at all")
			}
		})
	}
}

// TestPlaqueReceipt_IsScopedToTheSignedInAdminAndTheACTION, and the fast path pays
// NOTHING.
//
// 🔴 THE COST IS COUNTED IN QUERIES, NOT IN A FLAG. "The ordinary section view costs
// no extra statement" is a claim about a NUMBER, and a boolean could not see it.
func TestPlaqueReceipt_IsScopedToTheSignedInAdminAndTheACTION(t *testing.T) {
	venues := twoVenues()
	plaques := threePlaques(venues.firstVenueID())
	b := plaqueBrowser(t, venues, plaques)

	// The plain section, a save, and a venue word: none of them may issue the lookup.
	for _, target := range []string{locationsHref, locationsHref + "?done=venue-saved"} {
		b.do(http.MethodGet, target, nil)
	}
	if n, _ := plaques.plaqueConfirmations(); n != 0 {
		t.Fatalf("the section issued %d trail lookup(s) for a request that claims nothing", n)
	}

	b.do(http.MethodGet, locationsHref+"?done=plaque-replaced&id="+uidRetired, nil)
	n, seen := plaques.plaqueConfirmations()
	if n != 1 {
		t.Fatalf("trail lookups = %d, want exactly 1", n)
	}
	got := seen[0]
	if got[0] != panelTestTenant.String() {
		t.Errorf("scoped to tenant %s, want the session's %s", got[0], panelTestTenant)
	}
	if got[1] != panelTestAdmin.String() {
		t.Errorf("scoped to actor %s, want the session's %s — otherwise a manager could "+
			"learn that a COLLEAGUE replaced something", got[1], panelTestAdmin)
	}
	if got[2] != tenant.ActionPlaqueRetired {
		t.Errorf("scoped to action %q, want %q", got[2], tenant.ActionPlaqueRetired)
	}
	if got[3] != uidRetired {
		t.Errorf("scoped to target %q, want %q", got[3], uidRetired)
	}
}

// TestPlaqueReceipt_ATrailFailureIsSilenceNotAnOutage. Nothing is authorised by the
// answer; it decides whether a sentence appears. Turning a display detail into a 500
// would be the opposite of §4.6's intent here.
func TestPlaqueReceipt_ATrailFailureIsSilenceNotAnOutage(t *testing.T) {
	venues := twoVenues()
	plaques := threePlaques(venues.firstVenueID())
	plaques.confirmErr = errors.New("connection refused")
	b := plaqueBrowser(t, venues, plaques)

	rec := b.do(http.MethodGet, locationsHref+"?done=plaque-mounted&id="+uidInStock, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: a failed trail READ must not cost the manager the page", rec.Code)
	}
	html := htmlOf(t, rec)
	if strings.Contains(html, "Plaque mounted") {
		t.Fatal("an acknowledgement was printed although the trail could not be read")
	}
	if !strings.Contains(html, uidOnWall) {
		t.Fatal("the list did not render")
	}
}

// TestPlaqueViewModels_CannotCarryAKey is the RENDER-SIDE half of the §4.7 wall.
//
// 🔴 IT IS TWO ALLOW-LISTS, NOT A DENYLIST, AND THE PREVIOUS VERSION WAS BEATEN IN
// THREE SHAPES an audit added and ran: `Material [44]byte` (an ARRAY, exactly the
// envelope's length), `Ref string` (a neutral name), `Extras map[string][]byte` (the
// byte slice one indirection away). A forbidden-string scan protects against what
// you thought of; an allow-list protects against what you did not — this
// repository's own lesson, already written for audit_log.detail.
//
// 🔴 AND IT IS A SEPARATE TEST FROM THE DOMAIN'S BECAUSE OF A DEPENDENCY, not an
// oversight: a domain package cannot import web/templates, so its walk cannot reach
// these types, and this one cannot reach the domain's. Both halves are needed and
// both are named here and there.
//
// ⚠️ WHAT IS STILL NOT COVERED: a key ENCODED INTO A PERMITTED FIELD (hex in a
// string). No type system refuses that; what does is that no query on this path
// RETURNS aes_key_ref -- asserted by the domain package against db/queries/tags.sql
// and, since 2026-08-24, by cmd/tappa/storekeyshape_test.go against the generated
// store types, which is the half that survives a spelling nobody listed.
func TestPlaqueViewModels_CannotCarryAKey(t *testing.T) {
	fields := map[string][]string{
		"components.PlaqueRowView": {"UID", "Status", "Venue", "VenueUnknown", "Counter",
			"LastSeen", "KeyState", "Replaces", "ReplacedBy", "RetiredAt", "Loaded",
			"Frozen", "OpenHref"},
		"components.PlaqueFormView": {"Heading", "Action", "CloseHref", "Plaque", "Mode",
			"Venues", "Successors", "Blocked", "ErrorMessage", "Submit",
			"ConfirmField", "ConfirmToken", "UnmountHref", "Trail"},
		"components.PlaqueEventView":   {"What", "When", "Who", "BySystem"},
		"components.PlaqueReceiptView": {"Replaced", "Unmounted", "Name"},
	}
	seen := map[string]bool{}
	for _, root := range []struct {
		name string
		ty   reflect.Type
	}{
		{"components.PlaqueRowView", reflect.TypeOf(components.PlaqueRowView{})},
		{"components.PlaqueFormView", reflect.TypeOf(components.PlaqueFormView{})},
		{"components.PlaqueReceiptView", reflect.TypeOf(components.PlaqueReceiptView{})},
		// ⚠️ A ROOT OF ITS OWN, and the anti-rot check is what said so: the walker keys
		// its field lists by PATH, so a type reached only as PlaqueFormView.Trail[] is
		// never compared against a list filed under its own name. Naming it here is the
		// difference between a described type and a checked one.
		{"components.PlaqueEventView", reflect.TypeOf(components.PlaqueEventView{})},
		// 🔴 THE WHOLE SECTION VIEW, walked for SHAPE ONLY. A key would not have to
		// arrive through a plaque type to be rendered by this section, and enumerating
		// another milestone's fields here would make this test go red on their work
		// rather than on a leak.
		{"pages.LocationsView", reflect.TypeOf(pages.LocationsView{})},
	} {
		walkKeylessViewType(t, root.name, root.ty, fields, seen)
	}
	for name := range fields {
		if !seen[name] {
			t.Errorf("%s is described here but the walk never reached it", name)
		}
	}

	// 🔴 KeyState's NAME CONTAINS "key" AND ITS VALUE SET IS CLOSED, which is what
	// makes the field safe rather than merely permitted. keyStateOf is the only thing
	// that produces it, it takes a plaque and returns one of two sentences, and
	// neither reads anything about a key.
	onWall := tenant.Plaque{UID: uidOnWall, Status: tenant.PlaqueActive, Canonical: true}
	inBox := tenant.Plaque{UID: uidInStock, Status: tenant.PlaqueUnassigned, Canonical: true}
	for _, tc := range []struct {
		p    tenant.Plaque
		want string
	}{
		{onWall, "Encoded by Tappa"},
		{inBox, "Encoded by Tappa — pending a wall"},
	} {
		if got := keyStateOf(tc.p); got != tc.want {
			t.Errorf("keyStateOf(%s) = %q, want %q", tc.p.Status, got, tc.want)
		}
	}
}

// walkKeylessViewType is internal/domain/tenant's walkKeylessType over the render
// types. The duplication is NAMED rather than hidden — see the header.
func walkKeylessViewType(t *testing.T, path string, ty reflect.Type, fields map[string][]string, seen map[string]bool) {
	t.Helper()
	for ty.Kind() == reflect.Ptr {
		ty = ty.Elem()
	}
	// Permitted BY IDENTITY. uuid.UUID is itself a [16]byte, which is why the array
	// rule below cannot be "no arrays": permitting it by identity is what keeps
	// [44]byte — the envelope's exact length — refused.
	if ty == reflect.TypeOf(uuid.UUID{}) || ty == reflect.TypeOf(time.Time{}) ||
		ty == reflect.TypeOf(time.Location{}) {
		return
	}
	switch ty.Kind() {
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return
	case reflect.Slice:
		if ty.Elem().Kind() == reflect.Uint8 {
			t.Errorf("%s is a []byte: a plaque's KEK-wrapped key is a []byte (§4.7)", path)
			return
		}
		walkKeylessViewType(t, path+"[]", ty.Elem(), fields, seen)
		return
	case reflect.Struct:
	default:
		t.Errorf("%s is a %s; only named scalars, structs and non-byte slices are "+
			"permitted on this path, because every other shape can carry bytes (§4.7)",
			path, ty.Kind())
		return
	}

	if want, ok := fields[path]; ok {
		seen[path] = true
		got := make([]string, 0, ty.NumField())
		for i := 0; i < ty.NumField(); i++ {
			got = append(got, ty.Field(i).Name)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s has fields %v, want exactly %v (§4.7)", path, got, want)
		}
	}
	for i := 0; i < ty.NumField(); i++ {
		walkKeylessViewType(t, path+"."+ty.Field(i).Name, ty.Field(i).Type, fields, seen)
	}
}

// TestPlaqueWords_AgreeBetweenHandlerAndTemplate. Two lists that disagree would let
// a word be rendered on Done alone, which is the defect the whole mechanism
// replaces — the rule TestRemovalWords_AgreeBetweenHandlerAndTemplate already holds
// for removals.
func TestPlaqueWords_AgreeBetweenHandlerAndTemplate(t *testing.T) {
	for word := range plaqueActWords {
		if !pages.ClaimsAPlaqueAct(word) {
			t.Errorf("the handler treats %q as a claim needing a receipt; the template does not", word)
		}
	}
	// The other direction, over the section's whole vocabulary.
	for _, word := range venueDoneWords {
		_, handlerSays := plaqueActWords[word]
		if pages.ClaimsAPlaqueAct(word) != handlerSays {
			t.Errorf("%q: template says %v, handler says %v", word,
				pages.ClaimsAPlaqueAct(word), handlerSays)
		}
	}
	// ANTI-VACUITY: an empty map would pass the loop above over nothing.
	if len(plaqueActWords) != 3 {
		t.Fatalf("plaqueActWords holds %d word(s); the section ships three plaque acts "+
			"(mount, replace, un-mount)", len(plaqueActWords))
	}
}
