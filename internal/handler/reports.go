package handler

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The REPORTS SECTION — M6-07 phase A: worked hours. The CSV export is phase B.
//
// 🔴 THE TENANT IS NEVER AN INPUT (§4.5). It comes from httpx.AdminOf(r), which the
// Protect chain resolved from a signed session cookie against the database. The one
// thing a client controls here is WHICH WEEK, which is a narrowing predicate inside
// that tenant: a hand-edited date can only move the reader along their own business's
// timeline.
//
// 🔴 §4.3 — READ ONLY, AND THE HALF OF THE PANEL PATTERN THAT IS ABSENT IS ABSENT ON
// PURPOSE. There is no entry in mountWriting for this section, no ProtectWriting
// chain, no synchronizer token, no MAC'd confirmation and no audit_log row — because
// nothing here writes. Saying so is worth a sentence: four of this repository's
// sections DO have those, and the next reader should not take their absence for an
// omission. What is NOT skipped is a.render's header block (the panel CSP,
// Cache-Control: no-store, nosniff), which is the one place those are set.
//
// 🔴 §4.7 — NO COORDINATE PASSES THROUGH THIS FILE, and there is nothing to strip by
// hand. ListWorkedShiftEvents does not select gps_lat/gps_lng/source_ip/policy_context/
// entered_by, ledger's report types have no field for them, and pages.ReportsView has
// none either. This handler maps the second onto the third and could not carry one if
// it tried. That matters more here than on the other sections, because phase B turns
// exactly these values into a downloadable file.
//
// 🔴 §4.6 — A FAILED READ IS AN ERROR, NEVER AN EMPTY WEEK. If the database does not
// answer, this renders a problem page. It must not fall through to a report of zero
// hours: "nobody worked" is a claim about a business and a timeout is not evidence
// for it.

// WHAT THIS SECTION NEEDS FROM THE READ SIDE is one more method on panelLedger
// (transactions.go) rather than a new interface or a new field on AdminAuth. It is the
// reason M6-05 gave when it added Roster: this is the same package and the same
// concrete *ledger.Reader the constructor already passes, and a third or fourth
// identical argument in the same position is the shape where somebody eventually
// passes the wrong one.

// reportsHref is the section's own URL, READ FROM THE SECTION TABLE rather than
// written out again. It is what the week picker submits to and what every week link is
// built from, so if the section moves they move with it; if the tab is removed
// altogether this panics at startup rather than silently building links to nowhere.
var reportsHref = mustSectionHref(pages.TabReports)

// maxReportRows is how many people the screen LISTS.
//
// 🔴 IT IS A BOUND ON THE PAGE, NOT ON THE ARITHMETIC, and the difference is the whole
// reason it can exist at all. ledger.Hours totals everybody; this only decides how
// many rows are printed, and the screen says "showing N of M" whenever the two differ.
// A cap on the SUM would be a truncated payroll figure, which is what
// ledger.ReportEventCap refuses to produce.
//
// WHY THE CAP EXISTS. A report row is per PERSON, and a payroll is the one quantity in
// this product that grows without limit — the same argument that sized
// ledger.RosterPageSize. Unlike the roster, this page cannot page: a week's totals are
// one answer, so there is no cursor to hang a second page from. So the honest shape is
// a complete total with a bounded listing, and the export (phase B) is where somebody
// with four hundred staff gets every row.
//
// WHAT IT COSTS, MEASURED 2026-08-10 ON THE REAL HANDLER rather than estimated. The
// probe renders THIS handler against the development database over a real session,
// varying only how many people worked the week (five shifts each), and reports
// len(body). The figures are one observation of a database that grows on every
// `make test` run -- what does NOT drift is the per-row cost, because a page holds
// maxReportRows rows whatever the payroll is:
//
//	people   document     dockets
//	     1     3 947 B         3
//	    10    10 855 B        12
//	    50    41 539 B        52
//	   200   156 592 B       202
//	   400   156 898 B       202   the extra 306 B is the "showing 200 of 400" notice
//
// So a person's row costs about 770 B, and at the shipped cap of 100 the table is
// ~86 KB. The page is Cache-Control: no-store and therefore re-sent on every view, so
// that is a per-view cost rather than a download.
//
// 🔴 AN EARLIER SHAPE COST 209 302 B AT 200 ROWS, and the difference was class
// attributes: the seven day cells repeat their Tailwind chain ReportDays times per
// person. Moving them into .report-days/.report-day/.report-head (web/static/css/
// input.css) is the brand's own rule about repeated chains, and it took 25% off the
// page.
//
// ⚠️ AND THE TABLE ABOVE WAS NOT THE WHOLE PAGE, which is recorded because believing
// it was is what let a 560 KB page ship past a probe. See maxOpenRows.
const maxReportRows = 100

// maxOpenRows is how many unclosed check-ins the screen LISTS. Totals.Open always
// counts every one of them.
//
// 🔴 IT EXISTS BECAUSE THE PROBE ABOVE MISSED THE BIGGEST LIST ON THE PAGE. Rendering
// N employees with clean shifts bounded the per-person table and said nothing about
// the open list -- which is not bounded by the payroll at all, but by the BACKLOG of
// records nobody has corrected, and a backlog has no ceiling. Measured on the real
// handler against the development database (curl with a real signed-in session; the
// commands are in the M6-07 phase A delivery notes):
//
//	section                      before      after
//	chrome + totals docket       3 473 B    3 473 B
//	per person (200 -> 100)    171 916 B   84 612 B
//	per venue                      317 B      317 B
//	open list (1 012 -> 100)   381 677 B   38 635 B
//	WHOLE PAGE                 559 968 B  128 320 B
//
// Both columns are MEASURED (2026-08-10, curl against the running binary with a real
// signed-in session), on the same tenant and the same week, before and after.
// 560 KB on a page that is Cache-Control: no-store, of which 68% was one list nobody
// had bounded. Both caps are 100 now and the page is bounded at ~128 KB whatever the
// business is -- four times the transactions section (32 271 B, measured in the same
// session) and 15% of the 867 233 B control M6-03 measured and removed. A week with
// nothing in it is 3 145 B.
//
// The tally is unaffected and says so on the page: that measurement reads
// "1 012 check-ins with no checkout" beside "Showing the 100 oldest of 1 012".
const maxOpenRows = 100

// reportsSection renders the whole section: chrome, week picker, totals, breakdowns.
//
// 🔴 IT LOADS NO SCRIPT, so changing week is an ordinary navigation. The panel's
// widened Content-Security-Policy is pinned to EXACTLY ONE url by
// TestPanelScreens_ScriptsAndPolicyAgreeAndReachNoThirdParty, and a report is browsed
// rather than appended to — replacing the page is the behaviour a reader wants anyway,
// and the request cost is one either way.
func (a *AdminAuth) reportsSection(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	f := parseReportFilter(r)

	screen, err := a.ledger.Hours(r.Context(), id.TenantID(), f)
	if err != nil {
		// §4.6: say the read failed. Do NOT fall through to a week of zero hours.
		a.log.Error("panel: could not read the week's hours", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}

	v := pages.ReportsView{PanelChrome: a.chrome(r, pages.TabReports)}
	fillReportsView(&v, screen)
	a.render(w, r, http.StatusOK, pages.AdminReports(v))
}

// fillReportsView maps a domain report onto the view.
//
// EVERY TIME AND EVERY DURATION IS CONVERTED HERE AND ONLY HERE (§6: the database is
// UTC and the render layer converts), against the zone the database supplied for this
// tenant.
func fillReportsView(v *pages.ReportsView, s ledger.ReportScreen) {
	zone := s.Zone
	if zone == nil {
		zone = time.UTC
	}
	p := s.Period

	v.Queried = s.Queried
	v.ZoneLabel = zone.String()
	v.Incomplete = s.Truncated
	v.PracticeTaps = s.PracticeTaps
	v.StartedEarlier = s.StartedEarlier
	v.Unattributable = s.Unattributable

	v.WeekValue = p.FirstDay.String()
	days := p.DayLabels(zone)
	last := days[len(days)-1]
	// "Mon 3 – Sun 9 August 2026". The month is printed once when both ends share it,
	// because a manager reads this line to check they are on the right week and a
	// repeated month is noise rather than precision.
	if days[0].Month() == last.Month() {
		v.WeekLabel = days[0].Format("Mon 2") + " – " + last.Format("Mon 2 January 2006")
	} else {
		v.WeekLabel = days[0].Format("Mon 2 Jan") + " – " + last.Format("Mon 2 Jan 2006")
	}
	v.DayHeadings = make([]string, 0, len(days))
	for _, d := range days {
		v.DayHeadings = append(v.DayHeadings, d.Format("Mon 2"))
	}

	v.ThisWeekHref = reportsHref
	v.PrevHref = weekHref(days[0].AddDate(0, 0, -1))
	v.NextHref = weekHref(days[0].AddDate(0, 0, 7))

	v.Totals = pages.ReportTotalsView{
		Worked:   hoursMinutes(s.Totals.Worked),
		Shifts:   s.Totals.Shifts,
		Awaiting: nonZeroHours(s.Totals.Awaiting),
		Refused:  nonZeroHours(s.Totals.Refused),
		Open:     s.Totals.Open,
		TooLong:  s.Totals.TooLong,
	}
	v.OpenTotal = len(s.Open)
	for _, o := range s.Open {
		if o.Stale {
			v.Totals.NeedsAction++
		}
		// THE TALLY COUNTS EVERY ENTRY AND THE LIST PRINTS THE FIRST maxOpenRows OF
		// THEM. ledger.Hours returns them OLDEST FIRST, so a shortened list keeps the
		// ones that have been waiting longest -- which is the order a manager works a
		// backlog in.
		//
		// 🔴 THE LOOP DOES NOT BREAK EARLY, AND WHAT MAKES THAT NECESSARY IS A LINE ON
		// THE PAGE: pages.needsActionLine prints how many of the WHOLE backlog have been
		// open too long, and it is fed by the counter above. An audit found this
		// justification written here while nothing rendered the count -- an unread field
		// with a comment defending the cost of filling it. The reader exists now; if it
		// is ever removed, this loop may break at maxOpenRows and this paragraph goes
		// with it.
		if len(v.Open) >= maxOpenRows {
			continue
		}
		v.Open = append(v.Open, pages.ReportOpenView{
			Who:         personName(o.EmployeeName),
			Venue:       venueName(o.LocationName),
			At:          o.At.In(zone).Format("Mon 2 Jan 15:04"),
			NeedsAction: o.Stale,
			Manual:      o.Manual,
		})
	}
	v.OpenShown = len(v.Open)

	v.PeopleTotal = len(s.People)
	people := s.People
	if len(people) > maxReportRows {
		people = people[:maxReportRows]
	}
	v.PeopleShown = len(people)
	v.People = make([]pages.ReportRowView, 0, len(people))
	for _, ph := range people {
		v.People = append(v.People, reportRowView(ph))
	}

	v.Venues = make([]pages.ReportVenueView, 0, len(s.Venues))
	for _, l := range s.Venues {
		v.Venues = append(v.Venues, pages.ReportVenueView{
			Name:   venueName(l.Name),
			Worked: hoursMinutes(l.Worked),
			Shifts: l.Shifts,
		})
	}
}

// reportRowView maps one person's week onto one docket.
func reportRowView(p ledger.PersonHours) pages.ReportRowView {
	row := pages.ReportRowView{
		Name:     personName(p.Name),
		Worked:   hoursMinutes(p.Worked),
		Shifts:   p.Shifts,
		Awaiting: nonZeroHours(p.Awaiting),
		Refused:  nonZeroHours(p.Refused),
	}
	row.Daily = make([]string, 0, len(p.Daily))
	for _, d := range p.Daily {
		// A DASH RATHER THAN "0h 00m" ON A DAY NOBODY WORKED. Zero is a measured
		// figure and a day with no shift is the absence of one; printing them the same
		// way makes a week of dashes look like a week of zeros.
		if d == 0 {
			row.Daily = append(row.Daily, "—")
			continue
		}
		row.Daily = append(row.Daily, hoursMinutes(d))
	}
	if p.LateShifts > 0 {
		row.Late = plural(p.LateShifts, "late arrival", "late arrivals") + " · " + hoursMinutes(p.LateBy)
	}
	if p.Unmeasured > 0 {
		// 🔴 THE THIRD ANSWER BESIDE "late" AND "on time" (M4-05, and the same words
		// the locations section uses). A venue with no opening hours has no yardstick,
		// and reporting those arrivals as punctual would be inventing one.
		row.Unmeasured = plural(p.Unmeasured, "arrival", "arrivals") + " not measured — no shift set"
	}
	if p.Manual > 0 {
		row.Manual = plural(p.Manual, "shift", "shifts") + " entered by a manager"
	}
	if p.Open > 0 {
		row.Open = plural(p.Open, "check-in", "check-ins") + " with no checkout"
	}
	return row
}

// hoursMinutes formats a duration the way a payslip reads it.
//
// 🔴 IT IS INTEGER ARITHMETIC ON time.Duration AND THERE IS NO DIVISION BY A FLOAT
// ANYWHERE (§6). d.Hours() returns a float64 and is deliberately not used: a week of
// odd minutes accumulated through float hours is off by nanoseconds, which is
// invisible here and is not invisible in phase B's export, where the same values are
// written to a file an accountant sums.
//
// SECONDS ARE DROPPED, NOT ROUNDED, and that is a decision rather than a default. A
// rounded total can exceed the sum of its own rows — seven days each rounded up is up
// to seven minutes more than the week — and a report whose columns do not add up to
// its own total is one nobody trusts. Truncating is consistent: the parts and the
// whole are both floors, so they agree.
func hoursMinutes(d time.Duration) string {
	if d < 0 {
		// Not reachable from the arithmetic above (an interval runs forwards), but a
		// negative printed as "-1h -30m" would be worse than one printed plainly.
		return "-" + hoursMinutes(-d)
	}
	// int rather than int64 because pad2 (locations.go) is the ONE copy of this
	// two-digit rule in the package and it takes an int. A private second copy here
	// would be a second representation of "pad a clock field", which is exactly the
	// class this repository keeps paying for.
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	return strconv.Itoa(h) + "h " + pad2(m) + "m"
}

// nonZeroHours is hoursMinutes with "" for nothing, so the template can ask "is there
// any" by testing the string it would print.
func nonZeroHours(d time.Duration) string {
	if d == 0 {
		return ""
	}
	return hoursMinutes(d)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// personName and venueName turn an absent name into a WORD.
//
// A BLANK CELL READS AS "NONE", which is a different claim from "we could not resolve
// it" — the same reasoning ledger.Person's LocationName documents. §4.6 makes both
// absences possible: a record is written even when almost nothing about it could be
// resolved.
func personName(s string) string {
	if s == "" {
		return "Unknown employee"
	}
	return s
}

func venueName(s string) string {
	if s == "" {
		return "Unknown venue"
	}
	return s
}

// weekHref is the section's URL for the week containing a given local day.
func weekHref(day time.Time) string {
	q := url.Values{}
	q.Set("week", day.Format("2006-01-02"))
	return reportsHref + "?" + q.Encode()
}

// parseReportFilter reads the week from the query string.
//
// 🔴 THE VALUE IS VALIDATED HERE, AT THE BOUNDARY (§7), so the domain sees data that
// is already good. A date that does not parse is DROPPED rather than refused, which
// shows the tenant's current week instead of an error page — and that is safe for the
// reason the other sections give: dropping can only move the reader inside their own
// tenant, never outside it. It is also visible, because the picker echoes the week
// that actually took effect.
func parseReportFilter(r *http.Request) ledger.ReportFilter {
	var f ledger.ReportFilter
	if d, err := ledger.ParseDate(r.URL.Query().Get("week")); err == nil {
		f.Day = d
	}
	return f
}
