package components

import (
	"strconv"
	"strings"
)

// The LOCATIONS section's view models (M6-06 phase A).
//
// 🔴 EVERY FIELD HERE IS A STRING THE HANDLER ALREADY FORMATTED, and on this screen
// that is more than the usual "render layer does the formatting" rule (CLAUDE.md
// §6). Three of these values distinguish "absent" from "zero" and the distinction
// decides how later taps are judged:
//
//	Shift == ""   lateness is NOT MEASURED here. It is NOT "00:00–00:00", which is
//	              a real shift starting at midnight and would make every arrival
//	              late. internal/handler/locations.go's shiftText is the only thing
//	              that produces this field, from a nil *tenant.Shift.
//	GPS == ""     no coordinate is registered. It is NOT "0.000000, 0.000000",
//	              which is a real point in the Gulf of Guinea.
//	Ranges empty  no IP evidence is configured, which is migration 00002's own
//	              '{}' and is a configuration rather than a fault.
//
// A view model carrying numbers instead would let a template render a zero for an
// absence, which is the one mistake this screen must not make. The types make it
// unavailable.
//
// 🔴 §4.7 — THERE IS NO FIELD HERE A SECRET COULD TRAVEL IN. No key, no token, no
// hash, no employee name. WiFi is the network NAME a venue publishes to its own
// staff (migration 00010), which is display data and explicitly not evidence.

// VenueRowView is one venue as the docket renders it.
type VenueRowView struct {
	Name string

	// Ranges are the static IP ranges, already rendered. An empty slice is the
	// state NoProofOfPlace is about.
	Ranges []string
	// GPS is the coordinate pair, or "" for none.
	GPS string
	// Shift is the working window, or "" when lateness is not measured here.
	Shift string
	// WiFi is the network name shown during activation, or "" for none.
	WiFi string

	// NoProofOfPlace is true when this venue has NEITHER static IPs NOR a
	// coordinate.
	//
	// 🔴 IT IS THE ONE DERIVED FLAG ON THIS SCREEN AND IT EARNS ITS PLACE. In that
	// state every tap at this venue reaches CLAUDE.md §5 row 7: it is still
	// RECORDED — nothing is lost, §4.6 holds — but it is judged `flag` and lands in
	// the approval queue for somebody to clear by hand. A manager who empties the
	// address list to "tidy up" has changed how the venue is policed, and nothing
	// else on the screen would tell them.
	//
	// THE CARD STATES THE RULE, NOT A PREDICTION. "Taps here will be flagged" is
	// what the engine does; "37 taps a day will need approving" would be a claim
	// about the future that nothing measured.
	NoProofOfPlace bool

	// Departments are the names inside this venue, so the venue card answers
	// "what is in here" without the reader scrolling to the second list.
	Departments []string

	// DepartmentCount is how many departments this venue REALLY has, counted by the
	// database; DepartmentsShown is how many of their names are in the slice above.
	//
	// 🔴 THEY ARE TWO FIELDS BECAUSE THEY CAN DISAGREE, AND THE DISAGREEMENT IS THE
	// THING WORTH SAYING. The name list is assembled from a slice venuePageLimit may
	// have cut; the count is a tenant-scoped subquery over the whole table. When they
	// differ the card says so, because a venue quietly appearing to have fewer
	// departments than it has is exactly the §4.6 failure the capped-list notice
	// exists to prevent one level up.
	DepartmentCount  int
	DepartmentsShown int

	EditHref string
}

// DepartmentRowView is one department as the docket renders it.
type DepartmentRowView struct {
	Name string
	// Venue is the location it sits in. "" only if the join missed, which
	// location_id being NOT NULL makes a repair problem rather than a state — the
	// template says so in words rather than leaving a blank a reader takes for
	// "none".
	Venue string
	// Shift is the working window, or "" when lateness is not measured for this
	// department — in which case its VENUE's shift is what judges its people
	// (§5, Q17, M4-05). The template says that rather than showing an empty cell.
	Shift    string
	EditHref string
}

// VenueFormView is the add/edit card for one venue.
//
// 🔴 ID == "" MEANS "ADD", AND THAT IS THE ONLY THING THAT DISTINGUISHES THE TWO.
// One form for both is what keeps the eight fields, their validation and their
// sentences from existing twice — the "second representation" shape this repository
// has paid for repeatedly. The server reads the same absence: an empty id posts to
// the same route and becomes a create.
//
// EVERY VALUE IS A RAW STRING, INCLUDING AFTER A REJECTED SUBMISSION. When
// validation fails the handler rebuilds this from what was POSTED rather than from
// what parsed, so the manager gets their own text back and fixes one field instead
// of retyping eight. templ escapes all of it on the way out.
type VenueFormView struct {
	Heading string
	// Action is where the form posts — the section's own write route, supplied by
	// the handler from the SAME constant it registers, so the form cannot submit to
	// a URL that is not mounted.
	Action string
	// CloseHref is this section without the card — where "Cancel" goes.
	CloseHref string
	ID        string
	Name      string

	// Ranges is the static IP list as TEXT, one range per line.
	//
	// 🔴 A TEXTAREA RATHER THAN A SCRIPTED REPEATER, WHICH IS THIS SECTION'S SCRIPT
	// DECISION MADE VISIBLE. The panel's widened Content-Security-Policy is pinned
	// to exactly one url by a test; adding a second so a manager could press "+" to
	// get another box would spend that pin on a control they use a handful of times
	// in the life of a venue. One range per line needs nothing.
	Ranges string

	GPSLat string
	GPSLng string

	ShiftStart string
	ShiftEnd   string
	Overnight  bool

	WiFiSSID string

	// ErrorField names the input the message belongs to, or "" when the form is
	// clean. It is a SERVER-CHOSEN word from a fixed set, not something posted.
	ErrorField string
	// ErrorMessage is the sentence shown beside that field. §4.6: a refused save
	// says which field and why, because "nothing happened" is the failure mode this
	// panel is watched for.
	ErrorMessage string

	Submit string

	// Removal is the delete control, or nil on an ADD where there is nothing to
	// remove yet.
	Removal *RemovalView
}

// RemovalView is the delete control on an edit card, or the sentence that stands in
// for it (user decision, 2026-08-08: option (b) — unreferenced rows only).
//
// 🔴 THE CONTROL IS ABSENT WHEN IT WOULD FAIL, AND THE REASON IS STATED WITH NUMBERS.
// Six foreign keys reference locations and departments and all six are ON DELETE
// RESTRICT, so a venue in use can be neither removed NOR retired — there is no status
// or archived column to fall back on. Offering a button and then refusing the press is
// the exact class this section has spent four review rounds closing, so the button
// simply is not rendered and the card says what is in the way instead.
//
// 🔴 AND "STILL IN USE" IS RENDERED AS A RULE, NOT AS A FAULT. Nothing is broken about
// a venue that has staff and taps; §4.3 is why it must stay — `transactions` rows point
// at the venue they happened at, and a mesai record whose venue can be deleted is a
// record whose evidence can be edited away.
type RemovalView struct {
	// Action is where the removal posts.
	Action string
	// ID is the row being removed.
	ID string
	// Blocked is true when something still points at the row.
	Blocked bool
	// Reserved is true when this ADMIN may not remove anything, whatever the row
	// contains (user decision, 2026-08-09: removal is owner-only).
	//
	// 🔴 IT IS A SEPARATE FLAG FROM Blocked BECAUSE THEY ARE DIFFERENT SENTENCES AND
	// DIFFERENT FIXES. Blocked says "this venue still has staff in it", which the
	// manager can act on; Reserved says "this act belongs to an owner", which they
	// cannot. Collapsing them would send somebody to move employees around in the hope
	// of unlocking a button that was never theirs.
	Reserved bool
	// Reasons are the counted references, already rendered ("3 employees", "1 plaque").
	// Non-empty exactly when Blocked.
	Reasons []string
	// Counted names the reference kinds the query behind this card actually looked
	// at, so the "nothing belongs to it" sentence can list them.
	//
	// 🔴 IT IS PER-CARD BECAUSE THE TWO QUERIES COUNT DIFFERENT THINGS. A venue's
	// counts four kinds; a department's counts two — departments_location_fk and
	// tags_location_fk point at LOCATIONS, so a department has neither
	// sub-departments nor plaques of its own. One shared sentence made the
	// department card claim a search that never ran.
	Counted []string
	// Confirming is true on the second step: the warning is shown and the real
	// button is armed.
	Confirming bool
	// ConfirmHref opens the warning; ConfirmToken and ConfirmField carry the
	// server-minted value the POST must echo.
	ConfirmHref  string
	ConfirmToken string
	ConfirmField string
	// CancelHref leaves the warning without removing anything.
	CancelHref string
	// What the row is called, for the warning's own sentence.
	Kind string
	Name string
}

// DepartmentFormView is the add/edit card for one department.
type DepartmentFormView struct {
	Heading   string
	Action    string
	CloseHref string
	ID        string
	Name      string

	// Venues is the list a NEW department may be created in. It is not rendered
	// when VenueFixed is true.
	Venues  []OptionView
	VenueID string

	// VenueFixed says this department already exists and its venue cannot be
	// changed here.
	//
	// 🔴 THE CONTROL IS ABSENT, NOT DISABLED, AND THE SCREEN SAYS WHY. UpdateDepartment
	// does not name location_id (db/queries/departments.sql carries the argument:
	// moving a department between venues would leave every employee in it holding a
	// (venue, department) pair MoveEmployee refuses, judged against another branch's
	// shift). A disabled dropdown would still say "this is a thing you could do here
	// if only"; its absence plus a sentence says the true thing.
	VenueFixed bool
	// VenueName is the venue's name, shown in place of the dropdown when fixed.
	VenueName string

	ShiftStart string
	ShiftEnd   string
	Overnight  bool

	ErrorField   string
	ErrorMessage string

	Submit string

	// Removal is the delete control, or nil on an ADD.
	Removal *RemovalView
}

// departmentCountText renders a count for the venue card. It exists because templ
// interpolates strings and this is the only number this component prints.
func departmentCountText(n int) string {
	return strconv.Itoa(n)
}

// RemovalReceiptView is a removal the trail CONFIRMED (user decision, 2026-08-09,
// reading C').
//
// 🔴 IT IS A POINTER ON THE VIEW AND nil MEANS "SAY NOTHING", which is the whole
// design. A `?done=venue-deleted` word alone proves nothing — it was measured
// rendering "The venue has been removed" in a browser that had never posted anything —
// so the section only builds this after finding the SIGNED-IN ADMIN'S OWN audit entry
// for the id in the address, inside the window. A stranger's URL, a stale bookmark,
// another business's id and a colleague's removal all produce nil, and nil renders
// exactly the plain list.
type RemovalReceiptView struct {
	// Name is what the row was called, read from the trail — the only surviving copy,
	// since the row itself is gone. "" when the entry carries no name, in which case
	// the heading says what happened without naming it rather than printing a blank.
	Name string
}

// countedList renders the reference kinds a card checked as an English list, so the
// sentence above reads naturally for two kinds or for four.
//
// AN EMPTY LIST IS "records" RATHER THAN A HOLE IN THE SENTENCE. It cannot happen —
// both callers pass a non-empty list and a test holds them to the queries — but a
// template that renders "No  belong to this venue" on a future mistake would be worse
// than one that is merely vague.
func countedList(kinds []string) string {
	switch len(kinds) {
	case 0:
		return "records"
	case 1:
		return kinds[0]
	}
	return strings.Join(kinds[:len(kinds)-1], ", ") + " or " + kinds[len(kinds)-1]
}
