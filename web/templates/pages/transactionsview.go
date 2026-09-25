package pages

import (
	"strconv"

	"github.com/atknatk/tappa/web/templates/components"
)

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

	// Plaques is what this business's wall plaques amount to. The section reads it
	// through PlaqueState() and never branches on the numbers directly.
	Plaques PlaqueCounts

	// History is whether this business has any record at all, on any day. It
	// decides ONE thing — whether "Pick another day above" is offered — and is
	// read through OffersAnotherDay().
	History HistoryReading
}

// HistoryReading is the ledger's "has this business any record" answer.
//
// 🔴 IT IS NOT DERIVED FROM THE PLAQUE COUNTS, AND AN AUDIT IS WHY. A manager can
// type a record by hand with no plaque anywhere in the business (channel='manual',
// tag_uid NULL), proved with a real INSERT into a zero-plaque tenant. Keying the
// advice off plaques withdrew it from exactly the manager whose records are on
// another day.
type HistoryReading struct {
	// Queried is the anti-fabrication flag; unmeasured KEEPS the advice.
	Queried bool
	// Any is true when at least one record exists for this business.
	Any bool
}

// OffersAnotherDay reports whether "Pick another day above" can possibly help.
//
// ⚠️ IT WITHDRAWS ADVICE AND NEVER MAKES A CLAIM. The template prints one sentence
// less when this is false; it never prints "this business has no records", which
// would be a statement about the world.
//
// 🔴 UNMEASURED MEANS KEEP IT. The safe direction here is the opposite of the
// plaque notice's: a notice nobody measured must stay silent, but advice nobody
// measured must stay offered — withdrawing it on a zero value would hide a working
// business's own history from it whenever a caller forgets to fill the field in.
func (v TransactionsView) OffersAnotherDay() bool {
	if !v.History.Queried {
		return true
	}
	return v.History.Any
}

// PlaqueCounts is the ledger's plaque reading, carried onto the view unchanged.
//
// 🔴 IT IS COUNTS RATHER THAN A BOOL, AND THE BOOL IS THE BUG IT AVOIDS. A single
// "canRecord" flag would make the handler decide which sentence is true, and the
// three sentences this screen can print are not opposites: "Tappa has not loaded
// any plaques", "your plaques are in the box, none is on a wall" and "every plaque
// you have is out of service" are all "cannot record", and only one of them is ever
// right. Keeping the measurement and deriving the sentence from it (PlaqueState)
// means a fourth state cannot be quietly folded into a sentence written for a
// third.
type PlaqueCounts struct {
	// Queried is the anti-fabrication flag, for the reason TransactionsView.Queried
	// is: zero counts and "we never asked" are the same struct otherwise, and the
	// difference decides whether the screen may make a claim about the business at
	// all (§4.6). The HTMX fragment never asks, so it renders nothing from here.
	Queried bool
	// InService is how many plaques are `active` — the status half of §5 row 1's
	// test, nothing more.
	//
	// ⚠️ IT IS NOT "PLAQUES A TAP COULD ACTUALLY USE", AND THIS LINE USED TO SAY SO
	// (corrected M10 F0-6, third round). The count includes an `active` plaque with no
	// encoded_at stamp — Rusty Bar's shape — whose taps failed the SUN check live (12
	// taps, 0 valid). Whether an active plaque's chip actually carries the key the
	// database holds is not knowable from this row, so the screen's "working" state
	// means "a plaque is on a wall", not "taps there verify". Behaviour is unchanged;
	// the open item is the orchestrator's backlog.
	InService int
	// InStock is how many are loaded and not yet mounted.
	InStock int
	// ReadyToMount is how many of those could go on a wall — encoded as well as in
	// the box (M10 F0-6). The sentence that says "ready to mount" prints THIS, never
	// InStock; see ledger.Plaques.ReadyToMount.
	ReadyToMount int
	// Loaded is every plaque row this business has, whatever its status.
	Loaded int
}

// PlaqueState is the closed vocabulary of what the plaque counts mean.
//
// A STRING WITH A FIXED SET OF VALUES RATHER THAN A PILE OF BOOLS, because three
// bools describe eight states of which four cannot exist, and the template would
// have to be trusted not to render them.
type PlaqueState string

const (
	// PlaqueStateUnknown — nothing was measured. The screen says NOTHING about
	// plaques. It is the zero value on purpose: a view nobody filled in cannot
	// accidentally make a claim.
	PlaqueStateUnknown PlaqueState = ""
	// PlaqueStateWorking — at least one plaque is in service, so nothing about the
	// PLAQUES stands between this business and a tap. The screen adds nothing.
	//
	// ⚠️ IT IS NOT "TAPS CAN BE RECORDED", AND THIS COMMENT USED TO SAY THAT. It is
	// not derivable from a plaque count: a business with a plaque on the wall and
	// nobody activated yet (Provision writes the tenant, its venues and one admin —
	// no employees) meets §5 row 3, which sends the visitor to the activation page
	// and writes NO RECORD AT ALL. So a working plaque is a necessary condition and
	// not a sufficient one. The screen is already silent in that state, which is
	// correct; only the justification was overstated.
	PlaqueStateWorking PlaqueState = "working"
	// PlaqueStateInStock — plaques are loaded and none is on a wall.
	PlaqueStateInStock PlaqueState = "in-stock"
	// PlaqueStateOutOfService — plaques exist, none is in service and none is in the
	// box: every one of them has been retired or reported lost. 00013's CHECK on
	// tags.status is what makes that leftover exactly those two.
	PlaqueStateOutOfService PlaqueState = "out-of-service"
	// PlaqueStateNoneLoaded — this business has no plaque rows at all.
	PlaqueStateNoneLoaded PlaqueState = "none-loaded"
)

// PlaqueState derives which one is true. It is the ONLY reader of the counts, so
// the mapping exists once rather than in each branch of the template.
//
// ⚠️ THE STRONGEST CLAIM ANY OF THESE STATES SUPPORTS IS "NOBODY CAN TAP IN OR
// OUT", never "nothing can be recorded". A manager-typed record needs no plaque,
// and a tap on a plaque that is in stock or retired is REFUSED AND STILL WRITTEN
// DOWN (§4.6). transactions.templ keeps to that and a test holds it there.
//
// 🔴 THERE WAS A SECOND METHOD HERE — NoTapCanBeRecorded() — AND IT WAS DELETED
// RATHER THAN KEPT. Nothing rendered it: the templates switch on this function,
// and the only callers were its own tests. Thirteen lines of production code
// existing to be asserted about is the "second representation" this package's
// comments warn against, and it was one edit away from disagreeing with the switch
// it duplicated.
func (v TransactionsView) PlaqueState() PlaqueState {
	switch {
	case !v.Plaques.Queried:
		return PlaqueStateUnknown
	case v.Plaques.InService > 0:
		return PlaqueStateWorking
	case v.Plaques.InStock > 0:
		return PlaqueStateInStock
	case v.Plaques.Loaded > 0:
		return PlaqueStateOutOfService
	default:
		return PlaqueStateNoneLoaded
	}
}

// PlaquesHref and PlaquesLabel are the section a reader is sent to, READ FROM THE
// SECTION TABLE rather than typed into the markup — see SectionHref. The template
// renders no link at all when the href is empty.
func (v TransactionsView) PlaquesHref() string { return SectionHref(TabLocations) }

// PlaquesLabel is that section's tab label, so the sentence names it the way the
// navigation does.
func (v TransactionsView) PlaquesLabel() string { return SectionLabel(TabLocations) }

// ReadyToMountCount is the figure the sentence calls "ready to mount", as text for
// a mono cell (skill tappa-brand: every number the product prints is data and data
// is mono).
//
// 🔴 IT WAS InStockCount UNTIL M10 F0-6, AND THE NAME WAS THE BUG. It printed every
// `unassigned` row beside the words "ready to mount", including rows whose encode
// round never recorded its finish — which AssignTagToLocation refuses. The words now
// get the count the bind agrees with.
func (v TransactionsView) ReadyToMountCount() string {
	return strconv.Itoa(v.Plaques.ReadyToMount)
}

// UnrecordedStock is how many plaques are in the box but cannot go on a wall because
// their encoding was not recorded as finished. Zero means the sentence for them is
// not printed at all.
func (v TransactionsView) UnrecordedStock() int {
	if n := v.Plaques.InStock - v.Plaques.ReadyToMount; n > 0 {
		return n
	}
	return 0
}

// UnrecordedStockCount is UnrecordedStock as text, for a mono cell.
func (v TransactionsView) UnrecordedStockCount() string { return strconv.Itoa(v.UnrecordedStock()) }
