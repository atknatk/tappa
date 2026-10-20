package handler

// group_test.go -- GRP-1, the panel's Group section, through the real router and middleware over a
// fake ledger: the tab is drawn for a live holding member alone (both roles), a business in no group
// gets the empty state and no tab, a failed read is a problem page, the tenant asked is the session's.
// The database half (holding_group itself) is internal/db's; the real reader end to end is
// group_db_test.go.

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/web/templates/pages"
)

// fakeMember makes f a live member of one holding of two businesses, with one partner record of each
// kind and a figure for its own staff at partner venues.
func fakeMember(f *fakeLedger) {
	at := time.Date(2026, 10, 12, 9, 30, 0, 0, time.UTC)
	f.inGroup = true
	f.group = ledger.GroupScreen{
		Queried: true, Member: true, Zone: time.UTC,
		Window: ledger.Period{From: at.AddDate(0, 0, -30), To: at},
		Groups: []ledger.Group{{
			Name: "Kebab Holding", Since: at.AddDate(0, -2, 0),
			Members: []ledger.GroupMember{
				{Name: "Kebab Factory", Code: "KF", Joined: at.AddDate(0, -2, 0), Self: true},
				{Name: "Kebab Manufacturing", Code: "KM", Joined: at.AddDate(0, -1, 0)},
			},
		}},
		Partners: ledger.PartnerTaps{Queried: true,
			Taps:   []ledger.PartnerTap{{At: at, Direction: "in", Employer: "KM", EmployeeName: "Joe Vella", EmployeeCode: "E-7", Venue: "St Julians", Outcome: "counted"}},
			Manual: []ledger.PartnerTap{{At: at, Direction: "out", Employer: "KM", EmployeeName: "Ann Camilleri", Venue: "Sliema", Outcome: "employer_manual"}},
		},
		AtPartnerVenues: 17,
	}
}

// groupBrowser is panelBrowserWith in a role of the caller's.
func groupBrowser(t *testing.T, role string, records *fakeLedger) *browser {
	t.Helper()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: role, FullName: "KF Admin",
		}, nil
	}}
	b := newBrowser(t, newAdminRouterWithLedger(t, admins, &fakeTrail{}, records))
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

func groupTabDrawn(body string) bool {
	for _, l := range tabLinksOf(body) {
		if l.href == groupHref {
			return true
		}
	}
	return false
}

// TestGroup_ANonMemberSeesNoTabAndTheEmptyState: a business in no group (the fake's default, and a
// membership that has ended reads the same) -- as an owner and as a manager -- gets no Group tab on
// ANY section, and the route answers 200 with the panel's empty state -- labelled with the
// section's own name (M12-09's PanelEmpty; until then "No group"), never "Nothing here yet" (the
// GRP-1 review's DUSUK-2: "yet" promises a group) -- and nothing of a group.
// MUTANT (measured): panelTabs' GroupOnly condition made always-true -> red here.
func TestGroup_ANonMemberSeesNoTabAndTheEmptyState(t *testing.T) {
	for _, role := range []string{"owner", "manager"} {
		b := groupBrowser(t, role, newFakeLedger())
		for _, s := range pages.PanelSections {
			if s.OwnerOnly && role != "owner" {
				continue
			}
			rec := b.do(http.MethodGet, s.Href, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s GET %s: %d", role, s.Href, rec.Code)
			}
			body := htmlOf(t, rec)
			if groupTabDrawn(body) {
				t.Errorf("%s GET %s draws the Group tab for a business in no group", role, s.Href)
			}
			if s.Tab != pages.TabGroup {
				continue
			}
			if !strings.Contains(body, "This business is not in a group") {
				t.Errorf("%s GET %s: no empty state", role, s.Href)
			}
			// Nothing is about to arrive here: the label says what the state is, never "yet".
			label := `<p class="lbl">` + htmlText(s.Label) + `</p><h2 `
			if !strings.Contains(body, label) || strings.Contains(body, "Nothing here yet") || strings.Contains(body, " yet</p>") {
				t.Errorf("%s GET %s: the empty state is not labelled %q alone", role, s.Href, s.Label)
			}
			for _, never := range []string{"Your group", "Members", "Partner staff at your venues", "What the group shares"} {
				if strings.Contains(body, never) {
					t.Errorf("%s GET %s: a business in no group is shown %q", role, s.Href, never)
				}
			}
		}
	}
}

// TestGroup_OwnerAndManagerSeeTheTabAndThePage: a live member, in both roles, gets the Group tab on
// every section it may open, and the section: the group's name and its own start, each member's name,
// code and join date with this business marked, the partner records (outcome words, never a reason),
// the figure for its staff at partner venues, a link to Reports, and the box saying what is shared.
// Nothing on it posts.
func TestGroup_OwnerAndManagerSeeTheTabAndThePage(t *testing.T) {
	for _, role := range []string{"owner", "manager"} {
		records := newFakeLedger()
		fakeMember(records)
		b := groupBrowser(t, role, records)
		for _, s := range pages.PanelSections {
			if s.OwnerOnly && role != "owner" {
				continue
			}
			body := htmlOf(t, b.do(http.MethodGet, s.Href, nil))
			if !groupTabDrawn(body) {
				t.Errorf("%s GET %s: no Group tab for a member", role, s.Href)
			}
		}
		rec := b.do(http.MethodGet, groupHref, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s GET %s: %d", role, groupHref, rec.Code)
		}
		body := htmlOf(t, rec)
		for _, want := range []string{
			"Kebab Holding", "A member since", "12 Aug 2026",
			"Kebab Factory", "Kebab Manufacturing", "This business", "Joined", "12 Sep 2026", ">KF<", ">KM<",
			"Partner staff at your venues", "Last 30 days", "Joe Vella", "Counted by their employer",
			"Entered by their employer", "Ann Camilleri", "Hours: at their employer",
			"Your staff at partner venues", ">17<", `href="` + reportsHref + `"`,
			"What the group shares", "never sees the reason", "timezone cannot be changed", "Taptime runs the group",
			// The shared-list box: a manual record for a partner's venue is seen there too (employer_manual),
			// and the "never" list names a headcount, not an ambiguous "staff numbers".
			"or your manager enters a record for one of its", "or entered by", "address, headcount and billing",
		} {
			if !strings.Contains(body, htmlText(want)) && !strings.Contains(body, want) {
				t.Errorf("%s GET %s: the page does not say %q", role, groupHref, want)
			}
		}
		// The chrome's own form (sign-out) is on every panel page; the section adds none.
		chrome := htmlOf(t, groupBrowser(t, role, newFakeLedger()).do(http.MethodGet, groupHref, nil))
		if n, m := strings.Count(body, "<form"), strings.Count(chrome, "<form"); n != m || m == 0 {
			t.Errorf("%s GET %s: %d forms, the chrome alone has %d -- the section is read only", role, groupHref, n, m)
		}
		if strings.Contains(body, "not in a group") || strings.Contains(body, "staff numbers") {
			t.Errorf("%s GET %s: a member is told it is not in a group, or reads \"staff numbers\"", role, groupHref)
		}
		records.mu.Lock()
		asked := append(records.groupTenant, records.inGroupTenant...)
		records.mu.Unlock()
		if len(asked) == 0 {
			t.Fatal("anti-vacuity: the ledger was never asked")
		}
		for _, id := range asked {
			if id != panelTestTenant {
				t.Errorf("the ledger was asked about %s, want the session's tenant %s", id, panelTestTenant)
			}
		}
	}
}

// TestGroup_AFailedReadIsAProblemPageNeverNotInAGroup (§4.6): the section's read failing is a 500
// problem page -- never the "not in a group" empty state.
func TestGroup_AFailedReadIsAProblemPageNeverNotInAGroup(t *testing.T) {
	records := newFakeLedger()
	fakeMember(records)
	records.groupErr = errors.New("connection reset")
	rec := groupBrowser(t, "owner", records).do(http.MethodGet, groupHref, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("GET %s with a failed read: %d, want 500", groupHref, rec.Code)
	}
	if body := htmlOf(t, rec); strings.Contains(body, "not in a group") || strings.Contains(body, "Kebab Holding") {
		t.Errorf("a failed read rendered a group answer: %s", body)
	}
}

// TestGroup_AFailedMembershipReadDrawsTheChromeOfNoGroup: the chrome's membership question failing
// does not fail the section a manager asked for; the bar is the one of a business in no group (the
// brand's rule: the chrome falls back to what it was before the feature).
func TestGroup_AFailedMembershipReadDrawsTheChromeOfNoGroup(t *testing.T) {
	records := newFakeLedger()
	fakeMember(records)
	records.inGroupErr = errors.New("connection reset")
	rec := groupBrowser(t, "owner", records).do(http.MethodGet, reportsHref, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d", reportsHref, rec.Code)
	}
	if groupTabDrawn(htmlOf(t, rec)) {
		t.Error("a failed membership read drew the Group tab")
	}
}

// TestGroup_TheSectionAsksTheHoldingOnce: on the Group section the chrome takes the membership from
// the section's own read (chromeInGroup) -- holding_group is asked ONCE per page, never again by the
// chrome, and the tab follows that one answer: drawn for a member even when the chrome's own question
// would fail, absent for a business in no group. Every other section still asks the chrome's question.
// MUTANT (measured): groupSection building its chrome with a.chrome -> red here.
func TestGroup_TheSectionAsksTheHoldingOnce(t *testing.T) {
	for _, member := range []bool{true, false} {
		records := newFakeLedger()
		if member {
			fakeMember(records)
			records.inGroupErr = errors.New("connection reset")
		} else {
			records.group = ledger.GroupScreen{Queried: true}
		}
		b := groupBrowser(t, "manager", records)
		rec := b.do(http.MethodGet, groupHref, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("member %v: GET %s: %d", member, groupHref, rec.Code)
		}
		if got := groupTabDrawn(htmlOf(t, rec)); got != member {
			t.Errorf("member %v: the Group section draws the tab: %v", member, got)
		}
		records.mu.Lock()
		section, chrome := len(records.groupTenant), len(records.inGroupTenant)
		records.mu.Unlock()
		if section != 1 || chrome != 0 {
			t.Errorf("member %v: one Group page asked Group %d time(s) and InGroup %d, want 1 and 0", member, section, chrome)
		}
		b.do(http.MethodGet, reportsHref, nil)
		records.mu.Lock()
		chrome = len(records.inGroupTenant)
		records.mu.Unlock()
		if chrome != 1 {
			t.Errorf("member %v: Reports asked InGroup %d time(s), want 1 (the chrome's question)", member, chrome)
		}
	}
}
