package pages

import "github.com/atknatk/tappa/web/templates/components"

// The TRANSACTIONS section's view model (M6-03).
//
// THE ROW AND FILTER SHAPES ARE IN components, not here — components.DocketView
// and components.FilterBarView — because pages imports components and a component
// taking a pages type would close an import cycle. That file carries the §4.7
// argument for what a DocketView may and may not hold; this one holds the page.

// TransactionsView is the Transactions section.
type TransactionsView struct {
	PanelChrome

	// 🔴 Queried IS THE ANTI-FABRICATION FLAG. It is true only once the database
	// has answered. The template will not print "no taps recorded" without it,
	// because a zero view renders identically to a genuinely quiet day and the
	// product must never state something it has not measured — the class M5-11
	// closed. A failed read renders an error page and never reaches the template.
	Queried bool

	// Dockets is this page of records, newest first.
	Dockets []components.DocketView

	// Filters is the filter bar's whole state: the six controls, their options,
	// and what is currently selected.
	Filters components.FilterBarView

	// DayLabel is the day being shown, spelled out ("Wednesday 5 August 2026").
	DayLabel string
	// ZoneLabel is the tenant's timezone name, rendered beside the day because
	// "which day" is meaningless without it — and because an overnight shift's
	// 02:00 check-out lands on the NEXT day's page, which a manager cannot reason
	// about if the zone is invisible.
	ZoneLabel string

	// MoreHref is the next page as an ORDINARY LINK; MoreFragmentHref is the same
	// page as an HTMX fragment. Both are empty when this page is the last.
	//
	// 🔴 THE PAIR IS WHAT MAKES THE SCRIPT OPTIONAL. The control is one <a href>
	// that htmx intercepts: with JavaScript the next dockets are appended in
	// place, without it the browser follows the link and loads the next page as a
	// document. Neither path can reach records the other cannot.
	MoreHref         string
	MoreFragmentHref string
}
