package components

// The ROSTER ROW and its filter bar (M6-05 phase A).
//
// SAME PLACEMENT RULE AS docketview.go, and for the same mechanical reason: pages
// imports components, so a component taking a pages type would close an import
// cycle. A component owns the shape of its own input.
//
// 🔴 §4.7 — THE ABSENCE OF FOUR FIELDS IS THE SPEC. There is no InviteCode, no
// SessionToken, no DeviceLabel and no Email on RosterRowView. The first is phase
// B's and is shown exactly once, at the moment it is created, and never read back
// from storage (it is stored hashed); the second never leaves the database at all
// (db/queries/sessions.sql: token_hash exists to be matched); the third is not
// selected by the query; the fourth has no reader on a roster. Three independent
// walls again — the query does not select them, the domain type has no field for
// them, and this view could not carry one if it tried.
//
// 🔴 AND THERE IS NO ACTION FIELD, WHICH IS THE PHASE BOUNDARY MADE STRUCTURAL. The
// card asks for four actions — invite, re-invite, deactivate, move — and none of
// them is in phase A. A view model with a "Deactivate" href or a form target would
// let a template offer a control the server does not implement, which is this
// repository's most expensive class of mistake: a screen promising something the
// system does not do. Phase B adds the fields together with the handlers.

// RosterRowView is one person as a kitchen docket.
//
// EVERY DATE IS A PRE-FORMATTED STRING, as on DocketView: CLAUDE.md §6 keeps
// instants in UTC below the render layer and converts once, in the handler, against
// the zone the database gave for this tenant.
type RosterRowView struct {
	// Name is the person's full name. It is never empty — employees.full_name is
	// NOT NULL (migration 00003) — so unlike DocketView.Who there is no "nobody
	// recognised" branch to write.
	Name string
	// Venue is the person's home location, "" only if the join missed. The column
	// is NOT NULL, so an empty string here means a data defect rather than "this
	// person has no venue", and the template says so in words instead of leaving a
	// blank cell that reads as "none".
	Venue string
	// Department is "" for a business that does not model departments — a bar, in
	// the two markets this ships to. That is a first-class state, not a gap.
	Department string
	// Status is the RAW lifecycle value: invited | active | deactivated. It picks
	// the chip, and the chip carries the WORD as well as the tone, so status is
	// never told by colour alone (skill tappa-brand).
	Status string
	// Since is the lifecycle date that matches Status, already formatted, or "" when
	// the stamp was never written. The template prints "not recorded" for the empty
	// case rather than nothing: a blank beside a date label reads as a value.
	Since string
	// Devices is the one sentence this row says about signed-in phones, built by the
	// handler from a COUNT and a nullable timestamp.
	//
	// 🔴 IT IS A SENTENCE RATHER THAN TWO FIELDS BECAUSE THE TWO FACTS DO NOT
	// COMPOSE INDEPENDENTLY. "No device" and "a device that has never been used" are
	// different situations that both have no timestamp, and a template joining a
	// count field to a date field would render them identically. The handler decides
	// which sentence is true; see deviceLine.
	Devices string
}

// RosterFilterView is the employees section's filter bar: what can be picked, and
// what is picked.
//
// IT IS A SEPARATE TYPE FROM FilterBarView RATHER THAN A WIDENING OF IT. The two
// bars filter different things — a day of records versus a list of people — and the
// transactions bar's six controls include three (day, verdict, channel) that mean
// nothing here. One struct serving both would carry fields that are always empty on
// one of the two screens, and "always empty" is how a control eventually gets
// rendered on the screen it does not belong to. What the two DO share is the CSS,
// which is the part that would actually drift.
type RosterFilterView struct {
	// Action is where the form submits — the section's own URL, so filtering is a
	// plain GET and every filtered view is a bookmarkable, shareable address.
	Action string

	Locations   []OptionView
	Departments []OptionView
	// Statuses is the closed lifecycle vocabulary, supplied by the handler from THE
	// SAME LIST IT VALIDATES AGAINST — so the dropdown cannot offer a value the
	// validator would reject, and the validator cannot accept one the dropdown never
	// showed.
	Statuses []OptionView

	// The four selected values. Empty means "do not narrow".
	//
	// 🔴 AN EMPTY Status MEANS EVERYONE, INCLUDING PEOPLE WHO HAVE LEFT, and that is
	// §4.6 rather than a default somebody happened to pick. M6-03 measured and
	// rejected three ways of shortlisting a roster for exactly this reason and handed
	// the question to this section; a status filter that defaulted to "active" would
	// reopen it here.
	Name         string
	Status       string
	LocationID   string
	DepartmentID string

	// Narrowed is true when anything is set. The empty state uses it to say "nobody
	// matches these filters" rather than "nobody works here" — different claims, and
	// only one of them can be true at a time.
	Narrowed bool
}
