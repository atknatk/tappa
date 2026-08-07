package pages

import "github.com/atknatk/tappa/web/templates/components"

// The EMPLOYEES section's view model (M6-05 phase A).
//
// 🔴 IT IS A READ-ONLY VIEW AND THE ABSENCE OF EVERY ACTION FIELD IS THE SPEC. The
// card asks for four actions — invite, re-invite, deactivate, move — and phase A
// ships none of them. There is no CSRFToken here, no form target, no per-row href:
// a template cannot render a button whose destination the view has no field to
// carry, so "the screen offers something the server does not do" is not a mistake
// this phase can make. Phase B adds the fields in the same edit as the handlers.
//
// EVERY DATE IS ALREADY A STRING. CLAUDE.md §6 keeps instants UTC below the render
// layer; the handler converts once, against the zone the database gave.
type EmployeesView struct {
	PanelChrome

	// Queried is set ONLY after the database has answered, and it gates the empty
	// state.
	//
	// 🔴 IT IS THE ANTI-FABRICATION FLAG, the same one TransactionsView carries.
	// "Nobody works here" is a claim about a business, and a zero EmployeesView
	// renders identically to a real empty roster unless something separates them.
	// A failed read renders a problem page in the handler and never reaches this
	// template, so this can only be true when a query returned.
	Queried bool

	People  []components.RosterRowView
	Filters components.RosterFilterView

	// MoreHref is the whole-document link to the next page, "" on the last one.
	//
	// 🔴 THERE IS NO FRAGMENT TWIN OF THIS FIELD, unlike TransactionsView's
	// MoreFragmentHref, and that is the section's script decision made visible in
	// the view model: this page loads no script, so its "next" is an ordinary
	// navigation. internal/handler/employees.go records what that costs.
	MoreHref string

	// ZoneLabel names the timezone the dates above are printed in — the same
	// disclosure the transactions section makes, and for the same reason: a date
	// with no zone beside it is a date somebody will read in their own.
	ZoneLabel string

	// Paged says this request carried a paging cursor, and it exists to stop the
	// empty state making a claim about the BUSINESS when it is only a claim about
	// the POSITION.
	//
	// 🔴 IT WAS A MEASURED DEFECT, NOT A HYPOTHETICAL. With three people on the books,
	// a cursor pointing past the last of them rendered "Nobody on the books yet" and
	// "No people have been added to this business yet" over zero cards — a false
	// sentence about a business that has three employees, reachable from a stale
	// bookmark. (The URL that produced it named the anchor by NAME; the cursor became
	// an id the same day, for a different §4.6 defect, so the reproduction is now
	// `?after_id=<the last person's id>`.) RosterFilter.Narrowed() deliberately
	// does NOT count the cursor (a position is not a filter, and that is pinned by a
	// test), so the empty state had only two branches and picked the wrong one.
	//
	// The fix is a third branch rather than folding the cursor into Narrowed: "you
	// have reached the end" and "nothing matches your filters" are different true
	// sentences, and merging them would trade one wrong claim for another.
	Paged bool

	// StartHref is this view with its filters kept and the cursor dropped — where
	// "back to the start" goes. It is only rendered on the paged empty state.
	StartHref string
}
