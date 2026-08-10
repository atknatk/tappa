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
// ⚠️ THE PARAGRAPH THAT WAS HERE SAID "THERE IS NO ACTION FIELD" AND PHASE B MADE IT
// FALSE, so it is replaced rather than left standing. What it was protecting is the
// rule, not the emptiness: a view model must not carry a control the server does not
// implement. The four actions arrived WITH their handlers, their authorisation and
// their audit rows, in one edit — RosterActionsView below is that edit, and TWO tests
// hold the rule between them: TestEmployeesSection_EveryControlLeadsSomewhereThatExists
// (every form target and every link resolves through the real router) and
// TestEmployeesSection_OffersNoActionTheServerWouldRefuse (the conditional fields
// below are not offered when the database would refuse them). The second was missing
// for one round and an audit measured what that cost: CanInvite and CanDeactivate
// could both be hard-wired to true with the whole package still green.
//
// 🔴 WHAT IS STILL ABSENT IS THE SECRET. There is no InviteCode field on any type in
// this file. The activation link reaches exactly one template — pages.InviteIssued,
// rendered by the POST that mints it — and nothing on the roster or its action card
// has a field it could travel in.

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

	// ManageHref opens this person's action card (M6-05 phase B). It is the SAME
	// section URL with ?manage=<id> added and the reader's filters and page kept, so
	// closing the card returns them to the row they left.
	//
	// 🔴 IT IS A LINK TO A GET AND NOT A CONTROL THAT CHANGES ANYTHING. Every write
	// on this screen is a POST from the card, so a row cannot act on somebody by
	// itself and a crawler following every link on the page changes nothing.
	ManageHref string
}

// FormField is one hidden input: the roster position an action form carries back so
// the manager returns to the page they acted from.
//
// 🔴 THE VALUES ARE RE-VALIDATED ON ARRIVAL, NOT TRUSTED. The handler builds these
// from the filter it has already parsed, and rosterFilterFrom parses them again on
// the way in — the same function the GET uses, so a POST cannot accept a value a GET
// would reject. Nothing here is a URL: there is no field an open redirect could live
// in, because the target is rebuilt server-side from the parsed filter.
type FormField struct {
	Name  string
	Value string
}

// RosterActionsView is the action card for ONE person (M6-05 phase B): the four
// things a manager can do, and the facts they need to do them.
//
// 🔴 IT IS ONE CARD ON THE PAGE RATHER THAN CONTROLS ON EVERY ROW, and the reason is
// the cost M6-03 measured. The move form needs the venue and department option
// lists; rendering them once per row would multiply exactly the kind of control that
// was 96% of a page in the transactions section, on a page that is
// Cache-Control: no-store.
//
// 🔴 §4.7 — THERE IS NO FIELD FOR AN INVITATION CODE HERE, AND THERE CANNOT BE ONE.
// The activation link exists in exactly one response body — the answer to the POST
// that minted it, which renders InviteIssuedView — and this card is rendered by a
// GET. A field for it here would be a field a GET could fill.
type RosterActionsView struct {
	// Name, Status, Venue and Department are read from the database in the SAME
	// request that renders this card, so what the manager acts on is what they see.
	Name       string
	Status     string
	Venue      string
	Department string

	// Hidden is the roster position, posted back by every form on the card.
	Hidden []FormField
	// CloseHref is this roster without the card — where "Close" goes.
	CloseHref string

	// CanInvite is false for somebody deactivated: db/queries/invites.sql refuses to
	// spend an invitation for them, so the button would mint a dead credential. The
	// template says WHY in that case rather than simply omitting the control — a
	// missing button teaches nothing and reads as a bug.
	CanInvite    bool
	InviteLabel  string
	InviteAction string

	// RecordHref opens the manual record entry screen for this person (M6-08).
	//
	// 🔴 IT IS A LINK TO A GET AND THERE IS NO CanRecord BESIDE IT, WHICH IS THE ONE
	// UNCONDITIONAL ACTION ON THIS CARD. Every other control here is gated on a state
	// the database would refuse; this one is not gated because the server refuses
	// nobody — a deactivated person's last shift still has to be payable, and
	// deactivation is one-way (docs/adr/0010) so there is no route back if it is not
	// typed. A boolean that was always true would be the shape
	// TestEmployeesSection_OffersNoActionTheServerWouldRefuse exists to catch.
	RecordHref string

	// CanDeactivate is false for somebody already deactivated — pressing it again
	// writes nothing, and offering it would suggest otherwise.
	CanDeactivate bool
	// Confirming is true on the second step of the deactivation: the card has said
	// what it cannot undo and is now showing the button. The first step is a plain
	// LINK (ConfirmHref) because this section loads no script and a confirmation a
	// server cannot require is not a confirmation.
	Confirming  bool
	ConfirmHref string
	// ConfirmToken is the one-shot value the confirmation screen minted, and
	// ConfirmField is the input name the server reads it back from. They are FIELDS
	// rather than constants in the template because the server owns both halves: the
	// token is bound to this person in an HttpOnly cookie, and a form that carried
	// the wrong name would fail closed rather than silently skip the gate.
	//
	// 🔴 THE TOKEN IS NOT A SECRET IN THE §4.7 SENSE — it is rendered into the page on
	// purpose, exactly like the login form's synchronizer token, and the cookie half
	// is what makes it worth anything.
	ConfirmToken     string
	ConfirmField     string
	DeactivateAction string

	// The move form. LocationID and DepartmentID are the person's CURRENT placement,
	// pre-selected, so the form opens saying where they are rather than proposing a
	// change nobody asked for.
	MoveAction   string
	Locations    []OptionView
	Departments  []OptionView
	LocationID   string
	DepartmentID string
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
