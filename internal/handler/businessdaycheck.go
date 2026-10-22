package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE BUSINESS DAY'S RELEASE SURFACES — M15-08 (ADR 0032 §A.8, "Yayın sırası ve ilk gün").
// Three things the panel says around the release, none of which writes anything:
//
//  1. THE "Business day check" CARD (step 3): on Account, for the OWNER only (mayEditAccount,
//     the business level's own gate). It is M15-07's conflict function applied to every level
//     of the business at its value on file (tenant.DayCutoffs.Check) -- the owner opens it
//     before the release, reads which shifts a setting cuts and the suggestion, and follows
//     the link to the form that changes that level. It is an ordinary read in the owner's own
//     tenant context: no cross-tenant report, no op_*, no command that bypasses RLS (the
//     card's security lens). A manager's Account page neither draws it nor runs the read.
//  2. THE NOTICE ON EVERY SECTION (step 3a): while the check finds a cut shift, the owner's
//     shell opens with "Your business day setting cuts 2 shifts — review it", from the release
//     (T0) on -- the check does not depend on the switch, the cut does not wait for it. It is
//     gone the moment no level cuts a shift.
//     ⚠️ THE CARD ALSO SAYS "until the owner opens the section and saves", AND THIS DOES NOT:
//     the section is read-only (the card's own word), and the save it would mean is M15-07's
//     confirmed change -- which may knowingly keep a cutting value. Hiding a true sentence
//     after one save would need an acknowledgement stored somewhere, a schema change this card
//     does not make; the acceptance criterion is the conflict ("on every screen while it
//     cuts, on none without"), and that is what is built.
//     WHO SEES IT: the owner. The card says "owner paneli"; the business level is the owner's
//     to change, and a manager would be sent to a card they cannot open. A manager's chrome
//     runs no read for it.
//  3. THE RELEASE ANNOUNCEMENT (step 4): one notice on the anomaly and report screens. Its
//     list sentence from the release on (the classification ships with the binary, not with
//     the switch); its business day sentence only while the switch is on, dated with the
//     switch's own value through the one gate (DayCutoffs.Switch -> Directory.DayCutoff).
//
// 🔴 WHERE THE ANNOUNCEMENT ENDS, AND WHY NOT A RELEASE DATE IN THE CODE. "The first weeks after
// the release" is businessDayNoticeFor (28 days) after the SWITCH's value. A release date
// written into the binary is a guess at the merge moment, which is the user's decision and not
// known when the code is written; a date read from the database (goose's version table) is
// a grant tappa_app does not have. The switch's value IS the deploy fact: it is written on
// release day (T2 is the same day as T0 — ADR 0032 §A.8, product lens P1), the config refuses
// a value more than 30 days ahead or older than 00064's floor, and the engine reads the same
// value. So: before the switch is on (T0 .. T2, or no value at all) the list sentence shows;
// from T2 both show; 28 days after T2 neither does. Twenty-eight days is four whole weeks --
// every weekday's shifts and at least one weekly report of every kind run while it is up --
// and the same span the check reads the venues people tapped at in (CutoffTouchedWindow).
// The cost of the choice: an environment that never sets the switch shows the list sentence
// for as long as it runs, which is true there (the list did change).
//
// THE COST, PER OWNER PAGE: the check is the five statements of a settings card with the people
// unfiltered (tenant.DayCutoffs.Check). Measured once, 2026-10-22, EXPLAIN ANALYZE on the M15
// development database's KF tenant (373 people, 768 taps in 28 days, warm): the touched-venue
// read 1.6 ms executing, the people read 0.3 ms. A manager's page runs none of it.
//
// §4.7: nothing secret passes through -- times of day, names of venues and departments, counts.

// businessDayCheckAnchor is the card's id; the notice links to it.
const businessDayCheckAnchor = "business-day-check"

// businessDayNoticeFor is how long after the switch's value the release announcement stays up.
const businessDayNoticeFor = 28 * 24 * time.Hour

// The announcement's two sentences (ADR 0032 §A.8; M15-08 step 4), each its own main clause.
const (
	releaseHeading      = "How open check-ins are read has changed"
	releaseListSentence = "The open check-in list now also shows entries an older rule had hidden."
)

// releaseDaySentence is the business day sentence, dated with the switch's value in the
// business's zone. "Received": the switch is compared with the server's clock (F-2), so the
// instant that counts is the one the record reached the server.
func releaseDaySentence(since time.Time, zone *time.Location) string {
	return "From " + since.In(zone).Format("2 January 2006 at 15:04") +
		", a check-in is closed only within its business day; records received before that were paired by the old rule."
}

// businessDayRelease is the announcement for a screen drawn at now, in the business's zone.
// A panel without the business-day service draws none: it cannot know the switch.
func (a *AdminAuth) businessDayRelease(now time.Time, zone *time.Location) pages.BusinessDayRelease {
	if a.businessDays == nil {
		return pages.BusinessDayRelease{}
	}
	if zone == nil {
		zone = time.UTC
	}
	sw := a.businessDays.Switch(now)
	if sw.Active && !now.Before(sw.Since.Add(businessDayNoticeFor)) {
		return pages.BusinessDayRelease{}
	}
	n := pages.BusinessDayRelease{Heading: releaseHeading, List: releaseListSentence}
	if sw.Active {
		n.Day = releaseDaySentence(sw.Since, zone)
	}
	return n
}

// --- the check -----------------------------------------------------------------------

// businessDayCheckRead is one request's check: whether it was read (an owner, on a panel with
// the service) and what came back.
type businessDayCheckRead struct {
	ran   bool
	check tenant.BusinessDayCheck
	err   error
}

// businessDayCheckKey carries a check already read on this request (withBusinessDayCheck).
type businessDayCheckKey struct{}

// readBusinessDayCheck runs the check for the signed-in owner; for anybody else it reads nothing.
func (a *AdminAuth) readBusinessDayCheck(r *http.Request) businessDayCheckRead {
	id := httpx.AdminOf(r)
	if a.businessDays == nil || !mayEditAccount(id) {
		return businessDayCheckRead{}
	}
	c, err := a.businessDays.Check(r.Context(), id.TenantID(), time.Now())
	if err != nil {
		a.log.Error("panel: could not read the business day check", "err", err)
	}
	return businessDayCheckRead{ran: true, check: c, err: err}
}

// withBusinessDayCheck reads the check once and keeps it on the request, for a page that draws
// both the shell's notice and the card (Account) -- one read, one answer for both.
func (a *AdminAuth) withBusinessDayCheck(r *http.Request) *http.Request {
	res := a.readBusinessDayCheck(r)
	return r.WithContext(context.WithValue(r.Context(), businessDayCheckKey{}, &res))
}

// businessDayCheckOf is the check kept on the request, or a fresh read.
func (a *AdminAuth) businessDayCheckOf(r *http.Request) businessDayCheckRead {
	if v, ok := r.Context().Value(businessDayCheckKey{}).(*businessDayCheckRead); ok && v != nil {
		return *v
	}
	return a.readBusinessDayCheck(r)
}

// businessDayCheckNotice is the shell's notice: drawn only when the check was read, read
// cleanly and found a cut shift. A failed read draws no notice here (the brand's rule for the
// chrome: the section is still worth rendering and the error is logged); the card on Account
// says the read failed instead of saying nothing is cut.
func businessDayCheckNotice(res businessDayCheckRead) pages.PanelBusinessDayCheck {
	if !res.ran || res.err != nil || !res.check.Cuts() {
		return pages.PanelBusinessDayCheck{}
	}
	return pages.PanelBusinessDayCheck{
		Heading: "Your business day setting cuts " + plural(res.check.CutShifts, "shift", "shifts") + " — review it",
		Sentence: "A business day that starts inside a shift splits it: the shift's clock-out would be recorded as a clock-in. " +
			"The business day check names each shift and suggests a time outside them.",
		Href:     accountHref + "#" + businessDayCheckAnchor,
		LinkText: "Open the business day check",
	}
}

// accountBusinessDayCheck is the Account card, for the owner; nil for anybody else.
func (a *AdminAuth) accountBusinessDayCheck(r *http.Request) *pages.BusinessDayCheckView {
	res := a.businessDayCheckOf(r)
	if !res.ran {
		return nil
	}
	v := &pages.BusinessDayCheckView{Anchor: businessDayCheckAnchor}
	if res.err != nil {
		v.Failed = "The business day check could not be read just now. Nothing was changed; try again in a minute."
		if errors.Is(res.err, tenant.ErrDayCutoffTooLarge) {
			v.Failed = "This business has more venues, departments or people than this check reads at once. " +
				"Tell us and we will check it with you."
		}
		return v
	}
	c := res.check
	v.Intro = "Every place a business day is set — the business, a venue, a department, a person — read against " +
		"the shifts of the people who use it, and of the venues they tapped at in the last 28 days. Nothing here changes a setting."
	v.Status = businessDayStatus(a.businessDays.Switch(time.Now()), c.Zone)
	v.Summary = "The settings on file cut no shift."
	if c.Cuts() {
		v.Summary = "The settings on file cut " + plural(c.CutShifts, "shift", "shifts") +
			". Each setting that does is below, with a time outside its shifts."
	}
	clear := map[tenant.CutoffLevel]int{}
	for _, l := range c.Levels {
		if len(l.Conflicts) == 0 {
			clear[l.Level]++
			continue
		}
		line := pages.BusinessDayCheckLine{
			Label: checkLevelLabel(l),
			Facts: plural(l.People, "person", "people") + " · " + plural(l.Shifts, "shift checked", "shifts checked"),
		}
		line.Warnings, line.Suggestion = conflictWords(l.Conflicts, l.Suggested)
		line.Href, line.LinkText = checkLevelLink(l)
		v.Lines = append(v.Lines, line)
	}
	v.Clear = checkClearSentence(clear)
	return v
}

// checkLevelLabel names a level and its value.
func checkLevelLabel(l tenant.LevelCheck) string {
	value := clockOf(l.Value)
	switch l.Level {
	case tenant.CutoffBranch:
		return l.SubjectName + " (venue) — " + value
	case tenant.CutoffDepartment:
		return l.SubjectName + " (department) — " + value
	case tenant.CutoffPerson:
		return "A person's own setting — " + value
	}
	return "The business — " + value
}

// checkLevelLink is the form that changes a level: the business card above on this page, the
// venue's or the department's edit card, the person's card -- each at its business day block.
func checkLevelLink(l tenant.LevelCheck) (string, string) {
	switch l.Level {
	case tenant.CutoffBranch:
		return locationsHref + "?venue=" + l.SubjectID.String() + "#" + businessDayAnchor, "Open " + l.SubjectName
	case tenant.CutoffDepartment:
		return locationsHref + "?department=" + l.SubjectID.String() + "#" + businessDayAnchor, "Open " + l.SubjectName
	case tenant.CutoffPerson:
		return rosterReturn(ledger.RosterFilter{}, l.SubjectID, "", "") + "#" + businessDayAnchor, "Open their card"
	}
	return accountHref + "#" + businessDayAnchor, "Change the business setting"
}

// checkClearSentence names the levels checked that cut nothing.
func checkClearSentence(clear map[tenant.CutoffLevel]int) string {
	var parts []string
	if clear[tenant.CutoffBusiness] > 0 {
		parts = append(parts, "the business setting")
	}
	for _, k := range []struct {
		level     tenant.CutoffLevel
		one, many string
	}{
		{tenant.CutoffBranch, "venue", "venues"},
		{tenant.CutoffDepartment, "department", "departments"},
		{tenant.CutoffPerson, "person's own setting", "people's own settings"},
	} {
		if n := clear[k.level]; n > 0 {
			parts = append(parts, plural(n, k.one, k.many))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "Checked and cutting no shift: " + strings.Join(parts, ", ") + "."
}
