package pages

import "github.com/atknatk/tappa/web/templates/components"

// The LOCATIONS section's view model (M6-06 phase A).
//
// 🔴 WHAT IS DELIBERATELY ABSENT IS THE SPEC OF THE PHASE. The M6-06 card asks for
// six things and this phase ships two of them — venue configuration and department
// management. There is no field here for a plaque, a tag UID, a counter, a last-seen
// stamp, a replace-tag flow or an "encoded/pending" state, so this template could
// not render one if it tried, and "the screen offers something the server does not
// do" is not a mistake this phase can make. Phase B adds the fields in the same edit
// as the handlers and the migration.
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

	// Done is what the last save did: one of four words, or "".
	//
	// ⚠️ IT IS NOT VERIFIED THE WAY THE ROSTER'S "deactivated" IS, and the sentence
	// is built so it does not need to be. M6-04 shipped a banner printed from the
	// query string alone and an audit measured a URL claiming a decision that did
	// not exist. Here the four words describe an act on a row whose CURRENT state is
	// rendered directly beneath the banner in the same request — so a replayed
	// address repeats a heading over a list that is true, rather than inventing a
	// change. The banner claims no before-state and names no numbers.
	Done string

	// Problem is why the last save did nothing:
	//
	//	"unreadable"           the REQUEST could not be read — an oversized body, an
	//	                       id that is not a uuid. Nothing was looked up
	//	"unknown-venue"        no such venue in this business (or the venue a new
	//	                       department was to sit in is not this business's)
	//	"unknown-department"   no such department in this business
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
