package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/tenant"
)

// mintingGET is one of the panel GETs that mint a confirmation or write a trail row
// (backlog T14, T19), driven through the real router.
type mintingGET struct {
	name string
	// open returns a signed-in browser and the minting path; minted reports, after the
	// request, whether the handler minted (or recorded) anything.
	open func(t *testing.T) (b *browser, path string, minted func(rec *httptest.ResponseRecorder) bool)
}

// confirmCookieSet is whether the response wrote the confirmation cookie.
func confirmCookieSet(rec *httptest.ResponseRecorder) bool {
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == adminConfirmCookieName && ck.Value != "" && ck.MaxAge >= 0 {
			return true
		}
	}
	return false
}

func confirmMinted(rec *httptest.ResponseRecorder) bool {
	return confirmCookieSet(rec) || strings.Contains(rec.Body.String(), `name="`+confirmField+`"`)
}

// mintingGETs is every GET that mints or records, one row each (the act, not the
// route: the locations section carries four).
func mintingGETs() []mintingGET {
	return []mintingGET{
		{"deactivate an employee (?confirm=deactivate)", func(t *testing.T) (*browser, string, func(*httptest.ResponseRecorder) bool) {
			return panelBrowserWithActions(t, &fakeStaff{}, &fakeInviter{}),
				managedRosterHref(uuid.New()) + "&confirm=deactivate", confirmMinted
		}},
		{"remove a venue (?confirm=remove)", func(t *testing.T) (*browser, string, func(*httptest.ResponseRecorder) bool) {
			_, b, id := venueWithRefs(t, tenant.References{})
			return b, locationsHref + "?venue=" + id.String() + "&confirm=remove", confirmMinted
		}},
		{"remove a department (?confirm=remove)", func(t *testing.T) (*browser, string, func(*httptest.ResponseRecorder) bool) {
			f := twoVenues()
			id := f.firstDepartmentID()
			f.refs = map[uuid.UUID]tenant.References{id: {}}
			return venueBrowser(t, f), locationsHref + "?department=" + id.String() + "&confirm=remove", confirmMinted
		}},
		{"replace a plaque (?plaque= opens the armed card)", func(t *testing.T) (*browser, string, func(*httptest.ResponseRecorder) bool) {
			venues := twoVenues()
			wall := venues.firstVenueID()
			onWall := tenant.Plaque{UID: uidOnWall, Status: tenant.PlaqueActive, LocationID: &wall, Canonical: true, EncodedAt: &fixtureStamp}
			spare := tenant.Plaque{UID: uidSpare2, Status: tenant.PlaqueUnassigned, Canonical: true, EncodedAt: &fixtureStamp}
			b := plaqueBrowser(t, venues, &fakePlaques{screen: tenant.PlaqueScreen{Zone: time.UTC, Plaques: []tenant.Plaque{onWall, spare}}})
			return b, locationsHref + "?plaque=" + uidOnWall, confirmMinted
		}},
		{"take a plaque down (?confirm=unmount)", func(t *testing.T) (*browser, string, func(*httptest.ResponseRecorder) bool) {
			venues := twoVenues()
			wall := venues.firstVenueID()
			onWall := tenant.Plaque{UID: uidOnWall, Status: tenant.PlaqueActive, LocationID: &wall, Canonical: true, EncodedAt: &fixtureStamp}
			b := plaqueBrowser(t, venues, &fakePlaques{screen: tenant.PlaqueScreen{Zone: time.UTC, Plaques: []tenant.Plaque{onWall}}})
			return b, locationsHref + "?plaque=" + uidOnWall + "&confirm=unmount", confirmMinted
		}},
		{"export the month's invoice (billing.exported)", func(t *testing.T) (*browser, string, func(*httptest.ResponseRecorder) bool) {
			trail := &fakeTrail{}
			b := billingBrowserWithTrail(t, newFakeBooks(), trail, "owner")
			return b, billingCSVHref, func(rec *httptest.ResponseRecorder) bool {
				return trail.count(ActionBillingExported) != 0 ||
					strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv")
			}
		}},
		{"export the week's hours (report.exported)", func(t *testing.T) (*browser, string, func(*httptest.ResponseRecorder) bool) {
			records := newFakeLedger()
			records.hours = reportWith(func(r *ledger.Report) {
				r.Totals = ledger.Totals{Worked: time.Hour, Shifts: 1}
			})
			b, trail := exportBrowser(t, records)
			return b, reportsCSVHref, func(rec *httptest.ResponseRecorder) bool {
				return trail.count(ActionReportExported) != 0 ||
					strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv")
			}
		}},
	}
}

// TestMintGate_AnotherSitesNavigationMintsAndRecordsNothing: every minting GET, started
// by another site, answers the panel's problem page with 403 -- no confirmation in the
// body, no confirmation cookie, no export row, no file.
func TestMintGate_AnotherSitesNavigationMintsAndRecordsNothing(t *testing.T) {
	for _, g := range mintingGETs() {
		for _, site := range []string{"cross-site", "same-site", " Cross-Site ", "SAME-SITE", "cross-origin"} {
			t.Run(g.name+"/"+site, func(t *testing.T) {
				b, path, minted := g.open(t)
				b.fetchSite = site
				rec := b.do(http.MethodGet, path, nil)
				if rec.Code != http.StatusForbidden {
					t.Fatalf("GET %s with Sec-Fetch-Site %q answered %d, want 403", path, site, rec.Code)
				}
				if minted(rec) {
					t.Errorf("the refused request minted or recorded something")
				}
				if confirmCookieSet(rec) {
					t.Errorf("the refused request wrote the confirmation cookie")
				}
				body := htmlOf(t, rec)
				for _, want := range []string{problemPanelElsewhere.Title, problemPanelElsewhere.Message} {
					if !strings.Contains(body, htmlText(want)) {
						t.Errorf("the refusal does not say %q", want)
					}
				}
				if !strings.Contains(body, panelProblemCard) {
					t.Error("the refusal is not the panel's problem card")
				}
			})
		}
	}
}

// TestMintGate_OurOwnLinkABookmarkAndAnOldBrowserStillMint is the positive control: the
// same requests from our own page (same-origin), typed or bookmarked (none) and from a
// browser that sends no fetch metadata reach the handler and mint as before -- so the
// refusals above are the gate's and not a broken fixture.
func TestMintGate_OurOwnLinkABookmarkAndAnOldBrowserStillMint(t *testing.T) {
	for _, g := range mintingGETs() {
		for _, site := range []string{"same-origin", "none", ""} {
			t.Run(g.name+"/"+site, func(t *testing.T) {
				b, path, minted := g.open(t)
				b.fetchSite = site
				rec := b.do(http.MethodGet, path, nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("GET %s with Sec-Fetch-Site %q answered %d, want 200", path, site, rec.Code)
				}
				if !minted(rec) {
					t.Fatalf("GET %s with Sec-Fetch-Site %q minted nothing; the fixture does not reach the mint", path, site)
				}
			})
		}
	}
}

// TestMintGate_APlainSectionLinkFromAnotherSiteStillOpens: the gate refuses only a
// request that would mint. A cross-site link to the plain roster, an employee's card,
// the plain locations page or a venue card is ordinary navigation and opens.
func TestMintGate_APlainSectionLinkFromAnotherSiteStillOpens(t *testing.T) {
	staff := panelBrowserWithActions(t, &fakeStaff{}, &fakeInviter{})
	_, venues, venueID := venueWithRefs(t, tenant.References{})
	records := newFakeLedger()
	records.hours = reportWith(func(r *ledger.Report) {})
	reports, _ := exportBrowser(t, records)
	for _, c := range []struct {
		b    *browser
		path string
	}{
		{staff, employeesHref},
		{staff, managedRosterHref(uuid.New())},
		{venues, locationsHref},
		{venues, locationsHref + "?venue=" + venueID.String()},
		{reports, reportsHref},
	} {
		c.b.fetchSite = "cross-site"
		rec := c.b.do(http.MethodGet, c.path, nil)
		if rec.Code != http.StatusOK {
			t.Errorf("a cross-site link to %s answered %d, want 200", c.path, rec.Code)
		}
		if confirmCookieSet(rec) {
			t.Errorf("a cross-site link to %s wrote the confirmation cookie", c.path)
		}
	}
}

// TestFetchSiteClass_IsAnAllowList pins the decision table: three values pass, every
// other value -- the two a browser sends and one it never does -- is refused.
func TestFetchSiteClass_IsAnAllowList(t *testing.T) {
	for _, c := range []struct{ header, want string }{
		{"", ""}, {"none", ""}, {"same-origin", ""}, {" Same-Origin ", ""},
		{"cross-site", "cross-site"}, {"same-site", "same-site"}, {"Same-Site", "same-site"},
		{"cross-origin", "unrecognised"}, {"same-origin, cross-site", "unrecognised"},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if c.header != "" {
			r.Header.Set("Sec-Fetch-Site", c.header)
		}
		if got := fetchSiteClass(r); got != c.want {
			t.Errorf("Sec-Fetch-Site %q: class %q, want %q", c.header, got, c.want)
		}
	}
}
