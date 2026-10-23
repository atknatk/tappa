package handler

// businessdaycheck_test.go -- M15-08's three release surfaces over fakes: the owner's "Business
// day check" card on Account (a manager's Account neither draws nor reads it), the notice on
// every section of the owner's panel while a setting cuts a shift (and on none without), and
// the release announcement's two modes on the anomaly and report screens. The check's reads
// are measured against real Postgres in internal/domain/tenant/daycutoffcheck_db_test.go.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/web/templates/pages"
)

const checkNoticeHeading = "Your business day setting cuts 2 shifts — review it"

// cuttingCheck is a business whose 05:00 cuts the bakery and whose venue's 01:00 cuts Rusty
// Bar's night, with a department that cuts nothing.
func cuttingCheck(venue uuid.UUID) tenant.BusinessDayCheck {
	rusty := tenant.CutoffShift{Owner: "Rusty Bar", Start: 18 * time.Hour, End: 2 * time.Hour, Overnight: true}
	return tenant.BusinessDayCheck{Zone: "Europe/Malta", CutShifts: 2, Levels: []tenant.LevelCheck{
		{Level: tenant.CutoffBusiness, SubjectID: panelTestTenant, SubjectName: "KM", Value: 5 * time.Hour, People: 4, Shifts: 2,
			Conflicts: []tenant.CutoffConflict{pastryConflict}, Suggested: cutoffAt(23, 0)},
		{Level: tenant.CutoffBranch, SubjectID: venue, SubjectName: "Gzira", Value: time.Hour, People: 1, Shifts: 1,
			Conflicts: []tenant.CutoffConflict{{Shift: rusty, Cutoff: time.Hour}}, Suggested: cutoffAt(10, 0)},
		{Level: tenant.CutoffDepartment, SubjectID: uuid.New(), SubjectName: "Kitchen", Value: 23 * time.Hour, People: 2, Shifts: 1},
	}}
}

// TestBusinessDayCheck_TheOwnerReadsItOnAccountAndAManagerDoesNot: the owner's Account carries
// the card -- the status line, the summary, per cutting level its label, facts, M15-07's
// conflict sentence, the suggestion and the link to the form that changes it, and the levels
// that cut nothing in one sentence -- and the page reads the check ONCE although the shell's
// notice and the card both use it. A manager's Account carries neither the card nor the notice
// and reads nothing. A failed read is said on the card, never drawn as "no shift is cut".
func TestBusinessDayCheck_TheOwnerReadsItOnAccountAndAManagerDoesNot(t *testing.T) {
	venue := uuid.New()
	days := &fakeBusinessDays{review: tenant.CutoffReview{Zone: "Europe/Malta", Business: 5 * time.Hour, Stored: cutoffAt(5, 0)},
		check: cuttingCheck(venue)}
	owner := htmlOf(t, dayBrowser(t, adminRoleOwner, days, &fakeStaff{}, &fakeTrail{}).do(http.MethodGet, accountHref, nil))
	for _, want := range []string{
		`id="` + businessDayCheckAnchor + `"`, ">Business day check<",
		"Not active yet — the business day starts applying when Taptime switches it on",
		"The settings on file cut 2 shifts. Each setting that does is below, with a time outside its shifts.",
		"The business — 05:00", "4 people · 2 shifts checked",
		"05:00 falls inside KM Pastry &amp; Bakery&#39;s shift (04:00–12:00): a 03:58 clock-in would count for the previous day and the 12:05 clock-out would be recorded as a clock-in.",
		"Suggested: 23:00 — outside every shift.",
		`href="` + accountHref + `#business-day"`, "Change the business setting",
		"Gzira (venue) — 01:00", "1 person · 1 shift checked",
		`href="/admin/locations?venue=` + venue.String() + `#business-day"`, "Open Gzira",
		"Checked and cutting no shift: 1 department.",
		checkNoticeHeading, `href="` + accountHref + `#` + businessDayCheckAnchor + `"`,
	} {
		if !strings.Contains(owner, want) {
			t.Errorf("the owner's Account does not carry %q", want)
		}
	}
	if strings.Contains(owner, "Kitchen (department)") {
		t.Errorf("a level that cuts nothing got a line of its own")
	}
	if n := days.checkCount(); n != 1 || days.checks[0] != panelTestTenant {
		t.Fatalf("the owner's Account read the check %d time(s) for %v, want once for the session's business", n, days.checks)
	}

	quiet := &fakeBusinessDays{review: days.review, check: cuttingCheck(venue)}
	manager := htmlOf(t, dayBrowser(t, "manager", quiet, &fakeStaff{}, &fakeTrail{}).do(http.MethodGet, accountHref, nil))
	if strings.Contains(manager, "Business day check") || strings.Contains(manager, businessDayCheckAnchor) || strings.Contains(manager, checkNoticeHeading) {
		t.Errorf("a manager's Account shows the business day check or its notice")
	}
	if n := quiet.checkCount(); n != 0 {
		t.Fatalf("a manager's Account read the check %d time(s)", n)
	}

	for _, c := range []struct {
		err  error
		want string
	}{
		{errors.New("the read failed"), "The business day check could not be read just now."},
		{tenant.ErrDayCutoffTooLarge, "more venues, departments or people than this check reads at once"},
	} {
		broken := &fakeBusinessDays{review: days.review, check: cuttingCheck(venue), checkErr: c.err}
		page := htmlOf(t, dayBrowser(t, adminRoleOwner, broken, &fakeStaff{}, &fakeTrail{}).do(http.MethodGet, accountHref, nil))
		if !strings.Contains(page, c.want) || strings.Contains(page, "The settings on file cut") || strings.Contains(page, checkNoticeHeading) {
			t.Errorf("a failed check (%v): the card does not say %q, or it claims a result", c.err, c.want)
		}
	}
}

// TestBusinessDayCheckNotice_OnEveryOwnerSectionWhileItCutsAndOnNoneWithout is step 3a's
// criterion: with a setting that cuts a shift every section of the owner's panel opens with the
// notice and its link to the card; with nothing cut, none does; a manager's sections never do.
func TestBusinessDayCheckNotice_OnEveryOwnerSectionWhileItCutsAndOnNoneWithout(t *testing.T) {
	review := tenant.CutoffReview{Zone: "Europe/Malta", Business: 5 * time.Hour, Stored: cutoffAt(5, 0)}
	cutting := &fakeBusinessDays{review: review, check: cuttingCheck(uuid.New())}
	clear := &fakeBusinessDays{review: review, check: tenant.BusinessDayCheck{Zone: "Europe/Malta",
		Levels: []tenant.LevelCheck{{Level: tenant.CutoffBusiness, Value: 23 * time.Hour, People: 4, Shifts: 2}}}}
	ownerCut := dayBrowser(t, adminRoleOwner, cutting, &fakeStaff{}, &fakeTrail{})
	ownerClear := dayBrowser(t, adminRoleOwner, clear, &fakeStaff{}, &fakeTrail{})
	managerCut := dayBrowser(t, "manager", &fakeBusinessDays{review: review, check: cuttingCheck(uuid.New())}, &fakeStaff{}, &fakeTrail{})
	walked := 0
	for _, s := range pages.PanelSections {
		rec := ownerCut.do(http.MethodGet, s.Href, nil)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d", s.Href, rec.Code)
			continue
		}
		walked++
		page := htmlOf(t, rec)
		if !strings.Contains(page, checkNoticeHeading) || !strings.Contains(page, `href="`+accountHref+`#`+businessDayCheckAnchor+`"`) ||
			!strings.Contains(page, "Open the business day check") {
			t.Errorf("the owner's %s does not carry the notice while a setting cuts a shift", s.Href)
		}
		if strings.Contains(htmlOf(t, ownerClear.do(http.MethodGet, s.Href, nil)), "Your business day setting cuts") {
			t.Errorf("the owner's %s carries the notice although nothing is cut", s.Href)
		}
		if strings.Contains(managerCut.do(http.MethodGet, s.Href, nil).Body.String(), "Your business day setting cuts") {
			t.Errorf("a manager's %s carries the owner's notice", s.Href)
		}
	}
	if walked != len(pages.PanelSections) || walked == 0 {
		t.Fatalf("only %d of %d sections answered 200; the walk has gone blind", walked, len(pages.PanelSections))
	}
	one := businessDayCheckNotice(businessDayCheckRead{ran: true, check: tenant.BusinessDayCheck{CutShifts: 1}})
	if one.Heading != "Your business day setting cuts 1 shift — review it" {
		t.Errorf("one cut shift reads %q", one.Heading)
	}
}

// TestBusinessDayRelease_TheTwoSentencesInTheTwoModes is step 4's render test, through the two
// screens that carry it: with the switch off only the list sentence; with it on both, the
// business day sentence dated with the switch's value in the business's zone; 28 days after
// the value neither; and a panel without the service draws none.
func TestBusinessDayRelease_TheTwoSentencesInTheTwoModes(t *testing.T) {
	now := time.Now()
	since := now.Add(-48 * time.Hour).Truncate(time.Minute)
	malta, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	dayIn := func(zone *time.Location) string {
		return "From " + since.In(zone).Format("2 January 2006 at 15:04") +
			", a check-in is closed only within its business day; records received before that were paired by the old rule."
	}
	// The fake ledger's screens are in UTC; the zone is the screen's, measured below on Malta.
	day := dayIn(time.UTC)
	list := "The open check-in list now also shows entries an older rule had hidden."
	for _, c := range []struct {
		name     string
		sw       tenant.BusinessDaySwitch
		list     bool
		dayToo   bool
		withDays bool
	}{
		{"switch off", tenant.BusinessDaySwitch{}, true, false, true},
		{"switch on two days ago", tenant.BusinessDaySwitch{Active: true, Since: since}, true, true, true},
		{"switch on 28 days ago", tenant.BusinessDaySwitch{Active: true, Since: now.Add(-businessDayNoticeFor - time.Minute)}, false, false, true},
		{"no service", tenant.BusinessDaySwitch{}, false, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			var days *fakeBusinessDays
			if c.withDays {
				days = &fakeBusinessDays{sw: c.sw}
			}
			for _, href := range []string{anomaliesHref, mustSectionHref(pages.TabReports)} {
				rec := dayBrowser(t, adminRoleOwner, days, &fakeStaff{}, &fakeTrail{}).do(http.MethodGet, href, nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("GET %s = %d", href, rec.Code)
				}
				page := htmlOf(t, rec)
				if got := strings.Contains(page, list); got != c.list {
					t.Errorf("%s: the list sentence shown = %v, want %v", href, got, c.list)
				}
				if got := strings.Contains(page, ", a check-in is closed only within its business day"); got != c.dayToo {
					t.Errorf("%s: the business day sentence shown = %v, want %v", href, got, c.dayToo)
				}
				if c.dayToo && !strings.Contains(page, day) {
					t.Errorf("%s: the business day sentence is not dated in the screen's zone:\n%q", href, day)
				}
				if n := strings.Count(page, releaseHeading); (c.list && n != 1) || (!c.list && n != 0) {
					t.Errorf("%s: the announcement is drawn %d time(s)", href, n)
				}
			}
		})
	}
	// The date is the business's wall clock: the same switch on a Malta screen.
	a := &AdminAuth{businessDays: &fakeBusinessDays{sw: tenant.BusinessDaySwitch{Active: true, Since: since}}}
	if got := a.businessDayRelease(now, malta).Day; got != dayIn(malta) || got == dayIn(time.UTC) {
		t.Errorf("on a Malta screen the business day sentence reads %q, want %q", got, dayIn(malta))
	}
	// The two sentences are one notice: rendered alone, one frame holds both paragraphs.
	var out strings.Builder
	v := pages.AnomaliesView{Release: pages.BusinessDayRelease{Heading: releaseHeading, List: list, Day: day}}
	if err := pages.AdminAnomalies(v).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), `<div role="status"`) != 1 || !strings.Contains(out.String(), "<p>"+list+"</p>") ||
		!strings.Contains(out.String(), `<p class="mt-1">`+htmlText(day)+`</p>`) {
		t.Errorf("the announcement is not one notice with the two sentences as paragraphs:\n%s", out.String())
	}
}
