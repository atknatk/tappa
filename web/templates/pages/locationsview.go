package pages

import "github.com/atknatk/tappa/web/templates/components"

// The LOCATIONS section's view model (M6-06, both phases).
//
// ⚠️ THIS HEADER USED TO SAY "there is no field here for a plaque, a tag UID, a
// counter, a last-seen stamp, a replace-tag flow or an encoded/pending state" and
// every clause of it is now FALSE — phase B added all six, in the same edit as the
// handlers, exactly as that sentence promised. It is rewritten rather than deleted
// because the property it was really asserting still holds and is what the next
// reader needs: THE VIEW MODEL IS THE SPEC OF WHAT THIS SECTION CAN SAY. A screen
// offering something the server does not do is this repository's most expensive
// class of mistake, and the cheapest guard against it is that a field has to be
// added here, in the same change as the handler that fills it.
//
// 🔴 WHAT IS STILL DELIBERATELY ABSENT, AND IS THE HEAVIEST RULE ON THIS SCREEN
// (§4.7): no field on this path has a SHAPE that could hold a plaque's AES key —
// not here, not on components.PlaqueRowView or PlaqueFormView, not on
// internal/domain/tenant.Plaque. That is asserted by two walks
// (TestPlaqueViewModels_CannotCarryAKey and its domain twin) which enumerate the
// plaque types' field NAMES exactly and refuse any array, map, interface or []byte
// anywhere beneath them — including beneath THIS type, which is walked for shape.
//
// ⚠️ IT IS A SHAPE CLAIM, NOT AN ABSOLUTE ONE. A key HEX-ENCODED into a permitted
// string would pass both walks; what makes that unreachable is that no shipped tag
// query RETURNS aes_key_ref -- asserted in two places, against db/queries/tags.sql
// and against the generated internal/store types (cmd/tappa/storekeyshape_test.go).
// ⚠️ THIS LINE SAID "the FIVE shipped tag queries" UNTIL 2026-08-24 AND THERE ARE
// TEN (grep -c '^-- name:' db/queries/tags.sql). A count in prose over a file this
// package does not own is the defect this repository keeps paying for; the number is
// gone rather than raised. An earlier version of this paragraph said "NO FIELD
// ANYWHERE ... COULD TRAVEL IN", which an audit disproved in three shapes.
//
// The card's "encoded/pending" criterion is answered by a WORD derived from whether
// the plaque has a wall; components/plaqueview.go states precisely what backs that
// word and what does not.
//
// 🔴 THE DELETE IS OFFERED ONLY WHERE IT CAN WORK (user decision, 2026-08-08: option
// (b) of the three the M6-06 card records). Six foreign keys reference locations and
// departments and all six are ON DELETE RESTRICT; neither table has a status,
// archived or deleted_at column. So a row IN USE can be neither removed nor retired —
// about five sixths of the location rows in the development database — and the screen
// says that as a RULE with counted reasons rather than as a fault. There is still no
// field for it HERE: the control belongs to the edit card
// (components.VenueFormView.Removal), which is where the reference count is paid for
// once instead of on every row of the list.

type LocationsView struct {
	PanelChrome

	// Queried is set ONLY after the database has answered, and it gates the empty
	// state.
	//
	// 🔴 IT IS THE ANTI-FABRICATION FLAG, the same one TransactionsView and
	// EmployeesView carry. "This business has no venues" is a claim about a
	// business, and a zero LocationsView renders identically to a real empty one
	// unless something separates them. A failed read renders a problem page in the
	// handler and never reaches this template, so this can only be true when a
	// query returned.
	Queried bool

	Venues      []components.VenueRowView
	Departments []components.DepartmentRowView

	// VenuesCapped / DepartmentsCapped say the ceiling was reached and the list
	// below is therefore INCOMPLETE.
	//
	// 🔴 §4.6: A TRUNCATED LIST THAT DOES NOT SAY SO IS A FALSE STATEMENT ABOUT THE
	// BUSINESS. The bound exists because an unbounded read is what M6-03 measured at
	// 867 KB and removed; saying nothing when it bites would tell a manager they run
	// 200 venues when they run more.
	VenuesCapped      bool
	DepartmentsCapped bool
	// VenueLimit and DepartmentLimit are how many rows of EACH list are actually
	// shown, as text.
	//
	// 🔴 THERE ARE TWO OF THEM BECAUSE ONE WAS WRONG, and the way it was wrong is
	// worth recording rather than quietly fixing. A single VenueLimit was rendered
	// into BOTH "only the first N venues" and "only the first N departments", so a
	// business with 3 venues and 250 departments was told "only the first 3
	// departments are shown" — a sentence that is false in the exact place §4.6 asks
	// the product to be true, and one no reader could tell from a correct one. The
	// two lists are capped independently (internal/domain/tenant.venuePageLimit is
	// applied to each), so they need two numbers.
	// TestLocationsSection_ACappedListNamesItsOwnCount holds it.
	VenueLimit      string
	DepartmentLimit string

	// VenueForm and DepartmentForm are the add/edit cards, or nil.
	//
	// 🔴 POINTERS, AND THE nil CASE IS A STATE RATHER THAN AN ABSENCE — the shape
	// EmployeesView.Actions uses. A zero-valued struct would render an empty form
	// about nothing, which is the fabrication Queried prevents one level up.
	//
	// AT MOST ONE IS EVER SET. Two open cards would mean two forms posting to two
	// routes with one Save button each and no indication which one the manager is
	// in; the handler picks one from the query string and the section renders what
	// it is given.
	VenueForm      *components.VenueFormView
	DepartmentForm *components.DepartmentFormView

	// --- the WALL PLAQUES (M6-06 phase B) ---------------------------------------

	// Mounted, Stock and OutOfService are the tenant's plaques in THREE lists, and
	// the split is a decision with a measurement behind it.
	//
	// 🔴 THE QUERY'S OWN ORDER WAS THE OTHER READING AND IT WAS NOT KEPT.
	// ListTagsForTenant sorts `location_id NULLS FIRST, uid`, i.e. stock first and
	// then the mounted plaques BY LOCATION UUID — which is a deterministic order and
	// not one any human reads: two plaques at the same door sit together only by
	// accident of the id. The two readings, both stated because the card leaves the
	// screen language to this round:
	//
	//	ONE LIST, SQL ORDER      stock first, then by location id. Cheapest, and it
	//	                         does put the plaques that need an action at the top.
	//	                         Rejected: the mounted half is ordered by a value the
	//	                         page never shows.
	//	THREE LISTS BY LIFECYCLE what is on a wall, what is in the box, what has been
	//	                         taken off. Chosen: each list answers ONE question a
	//	                         manager actually arrives with, and the section
	//	                         already reads as a stack of lists (Venues,
	//	                         Departments).
	//
	// The SQL order is still doing work — it is what makes the grouping below
	// deterministic for two plaques that are otherwise equal.
	Mounted      []components.PlaqueRowView
	Stock        []components.PlaqueRowView
	OutOfService []components.PlaqueRowView

	// PlaquesQueried is set ONLY after the plaque read answered — the anti-fabrication
	// flag Queried is for the venues, kept separate because the two reads can fail
	// independently.
	PlaquesQueried bool

	// PlaquesCapped says the rendered plaque list was cut, and PlaqueLimit is how
	// many rows are shown.
	//
	// ⚠️ THE CAP IS ON THE RENDER, NOT ON THE READ, and that is stated rather than
	// implied: ListTagsForTenant carries no LIMIT. What bounds it in practice is that
	// a plaque is screwed to a door frame, so a tenant's list is tens.
	//
	// 🔴 NO LEVEL IS QUOTED, AND THE NUMBER THAT USED TO BE HERE WAS CARRYING THE
	// WHOLE ARGUMENT. It said "the largest single tenant holds 11", measured
	// 2026-08-09 — an audit read 101 the NEXT DAY, and it climbs on every suite run
	// because this milestone's own journey tests seed plaques into the shared demo
	// tenant and `tags` cannot be deleted by the application role. That is exactly the
	// figure that decided whether the truncation notice below is a theoretical branch,
	// so a stale one does not merely age: it argues the wrong way. Count the table if
	// you need a number; internal/domain/tenant/plaque.go states the same policy.
	PlaquesCapped bool
	PlaqueLimit   string

	// PlaqueForm is the one plaque card, or nil. Same rule as the two above: at most
	// ONE card is ever open, and nil is a state rather than an absence.
	PlaqueForm *components.PlaqueFormView

	// PlaqueReceipt is a plaque act the TRAIL confirmed, or nil. It is what lets
	// this section acknowledge a mount or a replacement at all — see
	// components.PlaqueReceiptView for why Done alone is never enough.
	PlaqueReceipt *components.PlaqueReceiptView

	// 🔴 THERE IS NO "AddPlaqueHref", AND ITS ABSENCE IS THE PRODUCT DECISION. The
	// panel cannot create a plaque, because creating one means holding its AES key
	// and the key never passes through the panel (user decision, 2026-08-08: Tappa
	// encodes the plaque and loads the row; the panel only binds it). A field here
	// would be the first half of a control nothing could serve; the empty state says
	// the sentence instead.

	// RemovalReceipt is a removal the TRAIL confirmed, or nil.
	//
	// 🔴 IT IS WHAT LETS THIS SECTION ACKNOWLEDGE A DELETION AT ALL. Done alone cannot
	// be trusted for the two removal words: the row a banner would describe is gone,
	// so there is nothing on the page to check the sentence against. This field is set
	// only after the handler found the signed-in admin's own audit entry for the id in
	// the address (user decision, 2026-08-09, reading C'), and the template prints the
	// removal heading only when it is non-nil — never on Done alone.
	RemovalReceipt *components.RemovalReceiptView

	AddVenueHref      string
	AddDepartmentHref string
	CloseHref         string

	// VenueAction and DepartmentAction are the two write routes, passed down into
	// whichever form is open. They are read from the handler's own constants, so a
	// form cannot post to a URL that is not mounted.
	VenueAction      string
	DepartmentAction string

	// Done is what the last act did: one word from the section's closed set, or "".
	//
	// ⚠️ NO COUNT. This line has now gone stale TWICE — "one of four words", then "there
	// are eight now" written in the same edit that shipped a NINTH — which is the
	// argument, not an embarrassment: a count beside a slice that owns the count is a
	// second representation of it. The words are venueDoneWords; read them there.
	//
	// ⚠️ IT IS NOT VERIFIED THE WAY THE ROSTER'S "deactivated" IS, and the sentence
	// is built so it does not need to be. M6-04 shipped a banner printed from the
	// query string alone and an audit measured a URL claiming a decision that did
	// not exist. Here the SELF-BACKING words describe an act on a row whose CURRENT
	// state is rendered directly beneath the banner in the same request — so a replayed
	// address repeats a heading over a list that is true, rather than inventing a
	// change. The banner claims no before-state and names no numbers. The words that
	// are NOT self-backing (the removals and the plaque acts) are verified against the
	// trail first; pages.ClaimsARemoval and pages.ClaimsAPlaqueAct are the two lists,
	// and the handler holds the same ones.
	Done string

	// Problem is why the last save did nothing:
	//
	//	"unreadable"           the REQUEST could not be read — an oversized body, an
	//	                       id that is not a uuid. Nothing was looked up
	//	"unknown-venue"        no such venue in this business (or the venue a new
	//	                       department was to sit in is not this business's)
	//	"unknown-department"   no such department in this business
	//	"unknown-plaque"       no plaque with that id in this business
	//	"plaque-not-stock"     the new plaque is already on a wall, or retired —
	//	                       somebody mounted it between the screen and the press
	//	"plaque-not-active"    the plaque being replaced is not on a wall any more
	//	"same-plaque"          a plaque cannot replace itself
	//	"plaque-frozen"        the database refuses to write this row at all (a
	//	                       development-only state — see PlaqueRowView.Frozen)
	//
	// ⚠️ THERE IS NO WORD FOR "OUR READ FAILED", and its absence is the decision. A
	// failed read renders a PROBLEM PAGE (§4.6) rather than redirecting here, so a
	// word for it could only ever arrive from a hand-edited URL — and it would then
	// print "we could not load your venues" above a list of the venues it just
	// loaded. internal/handler/locations.go carries the argument; one such word was
	// removed rather than left to be triggered by strangers.
	//
	// 🔴 §4.6: A REFUSED ACTION IS NEVER SILENT, and the closed set is what keeps a
	// hand-edited URL from putting a sentence of its own on the screen.
	//
	// ⚠️ VALIDATION FAILURES ARE NOT IN THIS SET. A rejected form re-renders with
	// the manager's own values and a sentence beside the offending field
	// (internal/handler/locationactions.go says why); it never redirects, so it
	// never becomes a word here.
	Problem string
}
