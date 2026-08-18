package components

// The ADD-SOMEBODY control and form (M6-13) — the roster's way of creating the
// people every other control on that page acts on.
//
// 🔴 IT EXISTS BECAUSE THE SECTION SHIPPED WITHOUT IT AND NOBODY NOTICED FOR TWO
// AUDIT ROUNDS. M6-05 built the roster, M6-06 the invitation flow; the panel could
// list, invite, deactivate and move employees, and could not create one. The gap
// reached live production, where a customer signed up and asked how to add somebody.
// The answer was that they could not — the only thing in the repository that made an
// employee row was test/fixtures/seed.sql, which is outside the product.
//
// 🔴 THE CONTROL AND THE FORM ARE ONE VIEW MODEL BECAUSE THE ANSWER TO "CAN I ADD
// SOMEBODY" IS NOT ALWAYS YES, and a screen that offered the control and then refused
// the submission would be the very shape this section is watched for. Possible below
// is that answer, and it is computed from the business's own venues rather than
// assumed.
type RosterAddView struct {
	// Href opens the form — this roster with its filters and position kept, plus the
	// disclosure parameter. It is the section's own address, built by the handler.
	Href string

	// Open is true when the manager asked for the form.
	//
	// THE FORM IS BEHIND A DISCLOSURE RATHER THAN ALWAYS ON THE PAGE, which is the
	// shape the locations section already uses for its create forms (?venue=new). The
	// roster is a LIST first: a permanent five-field form above it would push the
	// thing the page is for below the fold on every visit, including the overwhelming
	// majority of visits that are not adding anybody.
	Open bool

	// Possible is false when the business has NO VENUE AT ALL.
	//
	// 🔴 employees.location_id IS NOT NULL (migration 00003), so somebody must be
	// hired INTO somewhere. A business with no venues therefore cannot add anybody,
	// and the honest screen says so and points at the fix — it does not render a
	// picker with nothing in it and let the manager discover the refusal by pressing
	// Save. The locations section makes exactly this call for its department form
	// ("with no venues there is nothing to add one to"), and this is the same rule
	// one table over.
	//
	// ⚠️ IT IS NOT A HYPOTHETICAL STATE EVEN THOUGH SIGNUP CREATES A VENUE TODAY.
	// That is one code path's current behaviour, not an invariant: nothing in the
	// schema or in this product prevents a business from removing its only venue
	// (DeleteVenue refuses only a venue something still POINTS AT, and a business
	// that has not hired anybody yet has nothing pointing at it). "A healthy system
	// is EMPTY" is the lesson this repository has paid for most often, so the empty
	// path is built and exercised rather than reasoned away.
	Possible bool

	// VenuesHref is where somebody goes to create that first venue. It is a field
	// rather than a literal in the template for the reason every other href on this
	// page is: the sections own their addresses, and a hand-written target is what
	// M6-04 measured going dead with the whole package green.
	VenuesHref string

	// Refusal is what the server just refused, when it refused something and there is
	// no form to put the message back on. It is "" the rest of the time.
	//
	// 🔴 IT EXISTS BECAUSE A VENUELESS BUSINESS WAS TOLD NOTHING AT ALL (§4.6). Posting
	// at the add route from a business with no venue is refused by the domain — the
	// column is NOT NULL, so there is nowhere to put anybody — and the handler
	// re-rendered the page with the message attached to a FORM THAT IS NOT RENDERED IN
	// THAT STATE. An audit measured the result: 200, "Add a venue first" on the page,
	// and the refusal itself nowhere. The manager pressed a button, something was
	// refused, and the screen discussed venues instead of saying so.
	//
	// §4.6 is that a refusal is never silent. The sentence had even been WRITTEN; it
	// just had no branch that could reach it.
	Refusal string

	// Form is the form itself. It is only meaningful when Open && Possible.
	Form StaffFormView
}

// StaffFormView is the new-employee form: what a manager fills in, and what came
// back when the server refused it.
//
// 🔴 THE REQUIRED FIELDS ARE DERIVED FROM THE SCHEMA, NOT CHOSEN. Migration 00003
// makes full_name and location_id NOT NULL and leaves department_id, role and email
// nullable, so exactly two controls here are required and three are not. A form that
// demanded an email would refuse the ordinary shift worker who does not have one, and
// the invitation flow does not need it — an activation link is read out or handed over
// (internal/invite), never necessarily emailed.
type StaffFormView struct {
	// Action is where the form submits.
	Action string
	// Hidden carries the roster position back, so a manager who was on page three of
	// a filtered list returns to it. Same mechanism as the action card's forms.
	Hidden []FormField
	// CloseHref abandons the form and returns to the roster.
	CloseHref string

	// The posted values, echoed back so a refusal costs one field rather than five.
	// On a fresh form they are empty except LocationID, which is pre-selected when
	// the business has exactly one venue — the overwhelmingly common case, and one
	// less decision for somebody adding their first employee.
	FullName     string
	Role         string
	Email        string
	LocationID   string
	DepartmentID string

	// Locations is every venue in the business. It NEVER contains an empty option:
	// location_id is NOT NULL, so "no venue" is not a choice, and offering it would
	// offer a submission the server can only refuse. When this is empty the form is
	// not rendered at all — see RosterAddView.Possible.
	Locations []OptionView
	// Departments is every department in the business, grouped by venue in the list
	// the handler builds. An empty choice IS offered here and is first class: a bar
	// does not model departments.
	Departments []OptionView

	// ErrorField names the control that was refused and ErrorMessage is the sentence.
	//
	// ⚠️ EVERY REFUSAL ON THIS FORM NAMES A FIELD — there is no "whole submission"
	// case, and this comment used to describe one in the present tense. The branch that
	// rendered a fieldless notice was deleted in round 2 as unreachable (all seven
	// addFormAgain call sites pass a field name), and an audit caught the sentence
	// still advertising it: a description of a capability the code does not have, which
	// is the same defect class as a citation to a test that does not exist.
	//
	// The field names are NOT listed here either, and that is the same rule applied a
	// second time: an earlier version enumerated four and had already fallen out of
	// step by missing "department_id". employeeactions.go is where they are produced,
	// and it is the only place that can be right about them.
	//
	// 🔴 §4.6 — A REFUSAL IS NEVER SILENT AND NEVER A 500. The two that surprise
	// people most are the email ones, and they are separate sentences for a reason:
	// "that is not an address" is the manager's typo, and "somebody already has it"
	// is not their mistake at all. The second is also where `citext` shows: the
	// column folds case, so changing the capitalisation does not make an address
	// different, and the message has to say that or the manager will try.
	//
	// The VENUELESS case is not one of these: it has no form to attach a message to,
	// and RosterAddView.Refusal carries it instead.
	ErrorField   string
	ErrorMessage string

	// BillingNote says what adding somebody does to the bill.
	//
	// 🔴 IT IS A MEASURED SENTENCE RATHER THAN A REASSURING ONE. M6-12 bills on a
	// headcount, so a create form owes the manager the truth about it — and the truth
	// is more specific than "you will be charged": the billing predicate
	// (tappa_employee_is_billable, migration 0016) requires activated_at IS NOT NULL,
	// and somebody added here is born 'invited' with that column NULL. So adding
	// somebody does NOT move the bill; ACTIVATING them does. Measured directly
	// against the database rather than read off the migration.
	BillingNote string
}
