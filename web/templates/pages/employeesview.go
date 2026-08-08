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

	// Actions is the card for the person named by ?manage=, or nil.
	//
	// 🔴 A POINTER, AND THE nil CASE IS A STATE RATHER THAN AN ABSENCE. The card is
	// only built after the database returned a person in THIS tenant, so a template
	// that renders it is rendering facts that were read in the same request. A
	// zero-valued struct would render an empty card about nobody, which is the
	// fabrication shape Queried exists to prevent one level up.
	Actions *components.RosterActionsView

	// Done is what the last action did: "deactivated", "moved", or "" — and the
	// handler only sets it after CHECKING what it can check.
	//
	// 🔴 M6-04 SHIPPED A BANNER PRINTED FROM THE QUERY STRING ALONE and an audit
	// measured a URL claiming a decision that did not exist. Here "deactivated" is
	// shown only when the person the card just read back IS deactivated; "moved" is
	// not verifiable as a change, so the sentence beside it names the placement the
	// card read rather than a transition nobody re-checked.
	Done string

	// Problem is why the last action did nothing:
	//
	//	"unreadable"           the REQUEST could not be read — an oversized body, an
	//	                       id that is not a uuid. Nothing was looked up, so
	//	                       nothing is claimed about anybody
	//	"unknown"              no such person in this business
	//	"already-deactivated"  a second press. Nothing was written and no second
	//	                       audit row exists
	//	"unknown-placement"    the venue or department is not this business's
	//	                       (or no longer exists)
	//	"no-change"            the requested placement is the one they already have
	//	"not-invitable"        a deactivated person cannot be sent an activation link
	//	"confirm-required"     the deactivation arrived without a valid confirmation,
	//	                       or with one older than ten minutes. Nothing was written
	//	"confirm-stale"        the confirmation this browser holds is bound to a
	//	                       DIFFERENT person — the second-tab case
	//	"actions-unavailable"  OUR read of that person failed. The roster answered, so
	//	                       the list is current; only the card is missing. It is a
	//	                       separate word from "unreadable" because that one says
	//	                       "nothing was looked up", and here something was
	//
	// 🔴 §4.6: A REFUSED ACTION IS NEVER SILENT. Six words rather than one because
	// each is a different instruction to the manager, and the closed set is what
	// keeps a hand-edited URL from putting a sentence of its own on the screen.
	Problem string
}

// InviteIssuedView is the ONE screen an activation link is ever rendered on
// (M6-05 phase B).
//
// 🔴 IT IS THE RESPONSE TO A POST AND HAS NO URL OF ITS OWN, which is §4.7 rather
// than a routing preference. A GET that could render this would be a GET that hands
// out a live credential to anybody who can replay an address — including from a
// browser history, a shared screen or a Referer header. There is nothing to replay:
// the code is not stored (00009 keeps its HMAC), invite.Manager never returns it,
// and the panel's only access to one is the per-request sink that fills this struct.
type InviteIssuedView struct {
	PanelChrome

	// Name is who the link belongs to. It is here so the manager can check they are
	// handing it to the right person before they read it out.
	Name string

	// ActivationURL is the link, in full, shown exactly once.
	//
	// ⚠️ THE TEMPLATE RENDERS IT AS TEXT AND NOT AS AN ANCHOR, deliberately. A
	// clickable link invites the MANAGER to open it, which would move the code into
	// their own browser's activation cookie and put it in their history; the person
	// it belongs to needs it typed, messaged or read aloud, not clicked here.
	ActivationURL string

	// Expires is how long the link lasts, as a LENGTH ("7 days") rather than as an
	// instant. internal/handler's expiryPhrase says why: this response has no row to
	// carry a timezone on, and a deadline printed in the wrong zone is a manager
	// telling somebody the wrong day.
	Expires string

	// Retired is how many of this person's earlier invitations this one retired.
	//
	// 🔴 IT IS ON THE SCREEN BECAUSE THE SCREEN USED TO BE WRONG ABOUT IT, twice and
	// in opposite directions. Until 2026-08-08 issuing a second invitation left the
	// first one live — an account-takeover path measured end to end — and the page
	// implied the opposite. Now the retirement is real (migration 00012), and the
	// number says how many links stopped working, so a manager reading it aloud knows
	// which one is current.
	Retired int

	// BackHref returns to the roster the manager came from, filters and page intact.
	BackHref string
}
