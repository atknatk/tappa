package pages

import "github.com/atknatk/tappa/web/templates/components"

// The REVIEW QUEUE's view model (M6-04).
//
// 🔴 §4.3 IS VISIBLE IN THE FIELD LIST. There is no Verdict to set, no Trust to
// correct and no OccurredAt to edit: the only thing a manager can submit from this
// screen is an outcome and a sentence, against a record id. The view has nothing
// else to carry, so a template cannot offer an edit control that the server would
// then have to refuse.

// ReviewItem is one record waiting for a decision: the docket a manager reads, and
// the id the form posts back.
type ReviewItem struct {
	// ID is the transaction's id as text. It is an opaque database handle, not a
	// secret — the server does not believe it either: the INSERT that consumes it
	// carries the tenant and the verdict='flag' predicate, so a foreign or
	// non-flagged id simply produces no row.
	ID string
	// Docket is the same card the Transactions section renders. The SAME COMPONENT
	// on purpose: a manager judging a record should see it in the shape they have
	// already learned to read, and a second layout would be a second place for the
	// evidence line to drift.
	Docket components.DocketView
	// When is the record's time as the queue says it -- "today 14:26", "yesterday 02:14",
	// "Mon 09:02", or "5 Oct 09:02" past a week -- in the tenant's zone, against the
	// handler's clock (M12-28).
	When string
	// FocusHref puts this record in focus: the section's own URL with focus=<ID>, keeping
	// the page's keyset position (after_at, after_id) so the same page stays loaded.
	// Selection is a link, not a script (README §3.3); the POST re-checks everything.
	FocusHref string
	// Current marks the record in focus. Exactly one item carries it on a non-empty page.
	Current bool
}

// ReviewPager is the phone's way through the page's records, one at a time (M12-28): the
// position of the record in focus, how many this page holds, and the two neighbours' focus
// links, "" at either end. "Of" is the PAGE's count, not the queue's: the queue's count is
// the line above, and the next page is "Show more".
type ReviewPager struct {
	Pos      int
	Of       int
	PrevHref string
	NextHref string
}

// DecidedToday is The Spike's "N decided today" (M12-25), three states as ledger.Decided:
// not queried -> the line claims nothing; Failed -> "not counted", never 0; Queried -> N.
type DecidedToday struct {
	Queried bool
	Failed  bool
	N       int
}

// ReviewView is the review queue section.
type ReviewView struct {
	PanelChrome

	// Action is where a decision POSTs — the section's OWN URL, supplied by the
	// handler from pages.PanelSections.
	//
	// 🔴 IT IS A FIELD BECAUSE THE LITERAL VERSION SHIPPED AND NOTHING CAUGHT IT.
	// The template used to write action="/admin/review" out by hand while
	// internal/handler/review.go's comment claimed the URL was "read from the
	// section table … it is what the decision form posts to". An audit changed the
	// form's action to /admin/nowhere, left the section table alone, and the whole
	// handler package stayed GREEN over real HTTP and real Postgres — i.e. every
	// Approve and Reject button in the shipped panel would have 404'd with no test
	// to say so. FilterBarView.Action is the same field for the same reason.
	Action string

	// 🔴 Queried IS THE ANTI-FABRICATION FLAG, the same one TransactionsView
	// carries. "Nothing is waiting" is a claim about the world; a zero view renders
	// identically to a genuinely empty queue, and the product may not say something
	// it has not measured (the class M5-11 closed). A failed read renders an error
	// page in the handler and never reaches the template.
	Queried bool

	Items []ReviewItem

	// Focus is the record being decided -- the one decision form is about this record and
	// no other (M12-28: the queue shows one record in focus instead of one form per record).
	// It is the zero value when Items is empty.
	//
	// 🔴 IT IS CHOSEN AMONG THE RECORDS THIS PAGE LOADED, never looked up by the id in the
	// address: an id that is not a uuid, another business's, already decided, or simply not on
	// this page falls back to the first item, with no error and no claim about that id (§4.6:
	// no blank page, no 404). What the form posts is re-checked by the INSERT anyway.
	Focus components.SpikeFocusView
	// FocusID is the focused record's id -- the decision form's hidden id.
	FocusID string
	// Pager is the phone's pager over this page's records.
	Pager ReviewPager

	// Decided is "N decided today" beside the queue's count.
	Decided DecidedToday

	// ZoneLabel is the tenant's timezone name. The queue spans days, so every time
	// on it is meaningless without saying which clock it is on.
	ZoneLabel string

	// MoreHref is the next page as an ordinary link, "" when this is the last one.
	//
	// 🔴 A PLAIN LINK RATHER THAN AN HTMX FRAGMENT, AND THAT IS A DELIBERATE
	// NON-USE. The transactions section pages with a script; this one does not, and
	// keeping it that way is what holds "EXACTLY ONE panel URL sends the widened
	// Content-Security-Policy" (TestPanelScreens_ScriptsAndPolicyAgreeAndReachNoThirdParty).
	// A queue whose rows disappear as they are decided is also the worst possible
	// candidate for in-place appending: the cursor a fragment carried would point
	// into a list that has since shifted underneath it.
	MoreHref string

	// Done is what the last decision was, echoed after the redirect: "approved",
	// "rejected", or "" on a plain view. It is a CONFIRMATION rather than a status:
	// the record it refers to has already left the queue, which is itself the
	// evidence.
	Done string
	// Problem is set when the last decision could not be recorded:
	//
	//	"taken"       somebody ELSE had already decided it
	//	"yours"       the CALLER had already decided it (a double click)
	//	"gone"        the id is not, or is no longer, a reviewable record of this
	//	              business — a statement about the RECORD, made only after the
	//	              database was asked
	//	"unreadable"  the REQUEST could not be read: an oversized body, an id that is
	//	              not a uuid, an outcome outside the two. Nothing was looked up,
	//	              so nothing is claimed about any record
	//
	// 🔴 §4.6: A REFUSED DECISION IS NEVER SILENT, AND ITS SUBJECT IS NEVER GUESSED.
	// The loser of a race must not be shown a clean queue and left to assume their
	// click worked — but neither may a manager who double-clicked be told that
	// "somebody else" decided a record they decided themselves. "taken" and "yours"
	// are separate words because the database is asked which one it is; collapsing
	// them was this screen's first version and it fabricated the who.
	Problem string
	// Was is the outcome ALREADY on record, and it is meaningful only beside
	// Problem == "yours". Empty otherwise.
	Was string

	// Clipped says the note that came with the last decision was longer than
	// internal/handler's maxReviewNote and was cut to fit.
	//
	// 🔴 IT EXISTS BECAUSE THE CUT USED TO BE SILENT (user decision, 2026-08-06).
	// The note was write-only, so a manager could type past the limit, be told
	// "Recorded as approved", and never learn that the rest was dropped. The fix is
	// two halves and needs both: this sentence, and the note being rendered back on
	// the record's docket so they can see what was actually kept.
	Clipped bool
}
