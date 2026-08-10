package pages

// The REPORTS section's view model (M6-07 phase A).
//
// 🔴 IT IS A READ-ONLY VIEW AND THE ABSENCE OF EVERY WRITE FIELD IS THE SPEC. There
// is no CSRFToken here, no form target, no per-row action href. Phase A totals the
// immutable record and changes nothing: the correction that closes an open entry is a
// NEW transaction plus audit_log and belongs to M6-08. A template cannot render a
// button whose destination the view has no field to carry, so "the screen offers
// something the server does not do" is not a mistake this phase can make.
//
// 🔴 §4.7 — NOR IS THERE A FIELD A COORDINATE COULD TRAVEL IN. No Latitude, no
// Longitude, no SourceIP, no PolicyContext: the query does not select them, the domain
// types have no field for them, and neither does anything below. Three independent
// walls, the same discipline DocketView applies.
//
// EVERY NUMBER IS ALREADY A STRING. CLAUDE.md §6 keeps instants UTC and durations
// exact below the render layer; the handler formats once, against the zone the
// database gave for this tenant. A template that did its own arithmetic would be a
// second implementation of the thing the whole section is about.
type ReportsView struct {
	PanelChrome

	// Queried is set ONLY after the database has answered, and it gates the empty
	// state.
	//
	// 🔴 IT IS THE ANTI-FABRICATION FLAG, the same one TransactionsView and
	// EmployeesView carry. "Nobody worked this week" is a claim about a business, and
	// a zero ReportsView renders identically to a real empty week unless something
	// separates them. A failed read renders a problem page in the handler and never
	// reaches this template.
	Queried bool

	// ZoneLabel names the timezone every time below is printed in — and here it does
	// more work than on the other sections, because the zone decides where the DAYS
	// begin. A week bounded by the wrong midnight is a different week.
	ZoneLabel string

	// WeekLabel is the period in words; WeekValue is the ISO date the picker echoes.
	WeekLabel string
	WeekValue string

	// The three navigation targets. ThisWeekHref is offered even when the current
	// week is showing, because it is also the "get me back" control after paging
	// several weeks away.
	PrevHref     string
	NextHref     string
	ThisWeekHref string

	// DayHeadings are the period's local days, in order — the columns each row's
	// Daily entries line up with. They come from the same Period the arithmetic used,
	// so a row can never have more or fewer columns than the heading claims.
	DayHeadings []string

	Totals ReportTotalsView
	People []ReportRowView
	Venues []ReportVenueView
	Open   []ReportOpenView

	// PeopleShown and PeopleTotal are how many rows are printed and how many the
	// report holds.
	//
	// 🔴 THEY ARE DIFFERENT NUMBERS ON PURPOSE AND THE DIFFERENCE IS NOT A
	// TRUNCATED TOTAL. The cap is on the LISTING; every figure in Totals is summed
	// over everybody. Saying so is the whole point of carrying both: "showing 200 of
	// 431" is a statement about the page, and a manager who reads it as a statement
	// about the hours would be reading a short payroll.
	PeopleShown int
	PeopleTotal int

	// OpenShown and OpenTotal are the same pair for the open list, and it needs one
	// for the same reason with a sharper edge: an unclosed check-in is a DEFECT the
	// business has not dealt with yet, so the list grows with the backlog rather than
	// with the payroll and has no natural ceiling at all. Measured 2026-08-10 on the
	// development database, one week of the seed tenant produced 1 012 of them --
	// 381 677 B of a 559 968 B page, i.e. 68% of it. The absolute figures drift with
	// that database; the PROPORTION is the point, and so is the shape: a list nobody
	// bounded was two thirds of the page. The tally in Totals.Open is always the true
	// number; this pair says how much of it is printed.
	OpenShown int
	OpenTotal int

	// Incomplete means the read hit its cap, so the totals are NOT a week's hours and
	// the screen refuses to present them as one (§4.6).
	Incomplete bool

	// PracticeTaps is how many TRAINING taps fall in the week. They are in no figure
	// above; the number is here so "excluded" is not the same as "invisible".
	PracticeTaps int

	// StartedEarlier counts checkouts whose shift began before this week, and
	// Unattributable counts direction-carrying records with no employee on them.
	// Both are seams rather than faults, and both are printed because a total that
	// silently differs from the transactions list is a total nobody can check.
	StartedEarlier int
	Unattributable int
}

// ReportTotalsView is the week in one docket.
type ReportTotalsView struct {
	Worked string
	Shifts int
	// Awaiting and Refused are "" when there are none. They are SEPARATE from Worked
	// and never folded into it: a flagged record is a record (§4.6) whose hours are
	// held out of payroll until somebody decides, and the two states are different
	// instructions — one needs a manager, one has had one.
	Awaiting string
	Refused  string
	// Open is how many check-ins have no checkout, and NeedsAction how many of those
	// have been open longer than the engine's forgotten-checkout threshold.
	Open        int
	NeedsAction int
	// TooLong counts pairs further apart than that threshold, which are not treated
	// as very long shifts but as an unclosed entry plus a stray checkout.
	TooLong int
}

// ReportRowView is one person's week.
type ReportRowView struct {
	// Name is "unknown" rather than "" when the record names nobody the roster still
	// has — a blank cell reads as "nobody worked", which is a different claim.
	Name   string
	Worked string
	// Daily lines up with ReportsView.DayHeadings.
	Daily  []string
	Shifts int
	// The four qualifiers, each "" when it does not apply. They are words rather
	// than numbers alone because a bare count beside a name says nothing about what
	// it counts.
	Awaiting   string
	Refused    string
	Late       string
	Unmeasured string
	Manual     string
	Open       string
}

// ReportVenueView is one location's week.
type ReportVenueView struct {
	Name   string
	Worked string
	Shifts int
}

// ReportOpenView is one check-in with no checkout (Q18).
//
// 🔴 IT CARRIES NO END AND NO DURATION, WHICH IS THE POINT. The system produces no
// checkout, so there is nothing honest to print in an "out" column; an estimate would
// be the product inventing a record.
type ReportOpenView struct {
	Who   string
	Venue string
	At    string
	// NeedsAction separates "somebody is on shift right now" from "somebody forgot",
	// which is how long it has been open and NOT why. The screen must not claim to
	// know the reason: a forgotten checkout and a pre-ADR-0008 masked check-in are
	// the same shape on disk.
	NeedsAction bool
	Manual      bool
}
