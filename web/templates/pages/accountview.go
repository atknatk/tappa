package pages

// The ACCOUNT screen (M7-05) — the business's own facts, the three it may change,
// what the tax register said about its VAT number, and the sentence its staff read
// after a good tap.
//
// 🔴 WHAT IS NOT ON IT, AND WHY THE ABSENCE IS THE DESIGN. There is no `Plan`, no
// price and no amount. Migration 00016 made the commercial terms the operator's
// rather than the customer's, and M6-12 phase B closed even READING them to owners at
// /admin/billing; a settings screen with a Plan field would re-open that door for
// every manager who can open a settings page. The M7-05 card asked for "fatura
// verileri, plan görünümü" and that criterion was measured as ALREADY SHIPPED at
// /admin/billing — so this screen LINKS there instead, and only for a reader who may
// follow the link.
//
// 🔴 AND THE LINK IS CONDITIONAL FOR A REASON THE PANEL ALREADY OWNS. The billing tab
// is OwnerOnly, so it is not drawn for a manager; a settings page that pointed one at
// /admin/billing anyway would be teaching them about a control they will be refused —
// exactly what PanelSection.OwnerOnly's comment rejects ("clickable but forbidden").

// AccountView is the whole screen.
type AccountView struct {
	PanelChrome

	// --- the business's own facts, as stored ------------------------------------

	// VATNumber is shown and NEVER editable. db/queries/tenants.sql carries the
	// measurement: it is globally UNIQUE, so an edit reaches outside this tenant, and
	// tappa_app cannot UPDATE the two columns recording what VIES said about it.
	VATNumber string
	// RegisteredOn is the sign-up date, already formatted.
	//
	// 🔴 THE JUSTIFICATION WAS RE-DERIVED IN ROUND 2 AND THE FIRST ONE IS RECORDED
	// BECAUSE IT WAS THE WRONG SHAPE. It read "the date the founding offer's free window
	// is computed from, so a customer looking at their first invoice can check it" —
	// which grounds a field on a MANAGER-READABLE page in an invoice, i.e. in exactly
	// the commercial territory M6-12 phase B closed to owners. The field stays; the
	// reason is different.
	//
	// IT IS AN IDENTITY FACT, LIKE THE NAME AND THE VAT NUMBER. "Since when has Tappa
	// had this business on file" is what makes the block it sits in a RECORD rather than
	// a form, and it is the one date on the screen nobody — customer or operator — can
	// move. It names no plan, no price and no amount, and it is the same date whatever
	// the business is charged.
	//
	// ⚠️ THE FOUNDING WINDOW IS COMPUTED FROM IT AND IS STATED ON THE BILLING SCREEN,
	// NOT HERE. That screen already prints which month starts being charged and at what
	// price (fillBillingFounding), so a second telling here would be a commercial term
	// on the wrong page AND a second representation of a fact billing owns.
	//
	// MEASURED rather than asserted, AND THE CORPUS IS NOW THE CLAIM: a manager's
	// rendered body carries none of `founding`, `standard`, `per person`, `€`, `plan`,
	// `price`, `amount` or `Billing`, and TestAccount_NeverPrintsTheCommercialTerms
	// checks all eight, for BOTH roles (`Billing` on the manager's side only — the owner
	// is offered that link on purpose).
	//
	// ⚠️ THIS SENTENCE CLAIMED EIGHT WHILE THE CORPUS CHECKED FOUR, and an auditor
	// caught it. The other four were true when re-derived from source, which is exactly
	// what makes the shape dangerous: a claim wider than its measurement reads as
	// evidence and is not.
	RegisteredOn string

	// --- what the tax register said ---------------------------------------------

	// VATStateWord is the marker's word — it carries the state on its own, never the
	// colour alone (skill tappa-brand, accessibility).
	VATStateWord string
	// VATState is the state as a TOKEN, and the template turns it into classes.
	//
	// 🔴 THE CLASS NAMES ARE NOT BUILT IN GO, AND THIS FIELD EXISTS BECAUSE THE FIRST
	// DRAFT DID EXACTLY THAT AND WAS MEASURED BROKEN. Tailwind scans
	// web/templates/**/*.templ and web/static/js/**/*.js — never Go — so a class string
	// assembled in internal/handler is a class Tailwind never sees. Measured on the
	// first build of this screen: `bg-line/10` (the "not checked" tint) produced ZERO
	// rules in app.css while the three colours that happen to be used by OTHER templates
	// survived, i.e. the bug was invisible in three states out of four. view.go records
	// the identical failure for the tap screen's stamp classes, where every stamp
	// rendered unstyled.
	VATState string
	// VATLine is the sentence under the marker: what the state means and what, if
	// anything, the customer should do.
	VATLine string
	// VATCheckedAt is when the register was asked, already formatted, or "" when it
	// never was.
	VATCheckedAt string

	// --- the form ----------------------------------------------------------------

	// Form is what the BOXES hold. On a plain load it is the stored row; on a refused
	// save it is what the customer typed, so a correction is one field rather than
	// three.
	Form AccountForm
	// OnFile is what the DATABASE holds — always, including on a render answering a
	// refused save.
	//
	// 🔴 IT IS A SECOND FIELD BECAUSE THE FIRST DRAFT HAD ONLY Form AND SHIPPED A LIE.
	// renderAccountFormAgain re-read the row and then overwrote Form with the
	// submission, and the facts docket — the block headed "On file" — was reading
	// Form. Measured: posting name="NOT THE STORED NAME Ltd", timezone="Europe/Maltaa"
	// (invalid, so nothing was written) produced a page where the typed name and the
	// typed zone were printed under "On file" and the STORED name and zone appeared
	// nowhere at all. Three false facts about a business, one of them the clock every
	// date in the panel is worked out in, on the one screen whose whole job is to
	// answer "what does Tappa have on file".
	//
	// "What is on file" and "what you typed" are two different questions and this
	// screen answers both, from two different fields.
	OnFile AccountForm
	// Types is the business-type chip set, mapped from internal/domain/signup's list
	// (which is itself pinned to migration 00001's CHECK by a test). It is never a
	// literal here.
	Types []SignupChoice
	// ZoneSuggestions is a SHORT list offered to the browser as suggestions, not as
	// a constraint: the input accepts any IANA name and the server is the authority
	// on which ones load. A closed list here would be a second representation of a
	// database that gains entries every year.
	ZoneSuggestions []string
	// PostHref is where the form posts — the route constant, so the form and the
	// route are the same value at compile time.
	PostHref string
	// NameMaxLength is the browser's `maxlength`, carried from the DOMAIN's own
	// constant rather than written into the markup. A literal here would be a third
	// copy of a bound the domain already owns and the boundary already checks, and the
	// browser's copy is the one nobody would notice going stale — it fails SILENTLY,
	// by letting somebody type a name the server then refuses.
	NameMaxLength int
	// CanEdit decides whether the form is drawn at all. It is the courtesy half of
	// the gate; internal/handler refuses the POST on its own.
	CanEdit bool
	// WhyNotEdit is what a manager reads where the form would be. A control that
	// simply disappears tells nobody anything.
	WhyNotEdit string

	// --- what the staff see -------------------------------------------------------

	// BrandNote explains that the two sentences below follow the business type and
	// cannot be written by hand yet.
	BrandNote string

	// --- notices and links --------------------------------------------------------

	// Saved is the confirmation sentence after a successful save, "" otherwise. It
	// is built by the handler from a CLOSED list; nothing a caller types reaches it.
	Saved string
	// Problem is the refusal sentence, from the same closed list.
	Problem string
	// FieldError is the message beside ONE input, and Field names which one. They
	// travel as a pair so a message cannot float away from the box it is about.
	Field      string
	FieldError string
	// BillingHref is the money screen, drawn only when the reader may open it.
	BillingHref string
}

// AccountForm is the three editable fields.
//
// 🔴 THE STRUCT IS THE LIST OF WHAT MAY CHANGE. `tenants` has five columns tappa_app
// may UPDATE and this type names three; a field here is a field the form can post, so
// the two absences (vat_number, structure) are enforced by the shape rather than by
// remembering not to draw them.
type AccountForm struct {
	Name         string
	BusinessType string
	Timezone     string
}

// ErrorFor is the message for one field, or "" when the failure was elsewhere.
//
// IT EXISTS SO THE TEMPLATE ASKS PER FIELD rather than comparing strings in markup:
// a template that wrote `if v.Field == "name"` in three places would be three places
// for a typo to silently hide a message.
func (v AccountView) ErrorFor(field string) string {
	if v.Field != field {
		return ""
	}
	return v.FieldError
}

// Unsaved is the value STILL ON FILE for one field, when a refused save typed
// something different into it — and "" the rest of the time.
//
// 🔴 IT EXISTS BECAUSE KEEPING THE TYPING AND TELLING THE TRUTH ARE BOTH REQUIRED AND
// NEITHER IS ENOUGH. Throwing the submission away costs the customer their work; showing
// it without saying it was refused leaves them looking at a screen that appears to have
// saved. So the box keeps what they typed, the docket above keeps what is on file, and
// this is the sentence that names the gap between them, per field.
//
// IT RETURNS THE STORED VALUE RATHER THAN A BOOL so the template can print it: "this is
// still X on file" is actionable and "that field was not saved" is not.
//
// ⚠️ THERE IS NO `Refused` FLAG GUARDING THIS, AND ONE WAS WRITTEN AND THEN REMOVED
// BECAUSE A MUTATION MEASURED IT DEAD. Deleting the guard left every test GREEN, and the
// reason is structural rather than lucky: accountView fills Form and OnFile from the
// SAME Settings, so the two can differ on exactly one path — renderAccountFormAgain,
// which is the refused save. A flag that only ever agreed with the comparison below was
// a door measured to be always open, drawn as a door (reportscsv.go's rule), and the
// next reader would have stopped looking at what actually decides this.
func (v AccountView) Unsaved(field string) string {
	switch field {
	case "name":
		if v.Form.Name != v.OnFile.Name {
			return v.OnFile.Name
		}
	case "business_type":
		if v.Form.BusinessType != v.OnFile.BusinessType {
			// 🔴 THE LABEL, NOT THE STORED TOKEN. The <select> three lines up shows
			// "Café"; printing "cafe" underneath it would be the machine's word for a
			// value the customer has only ever seen spelled the other way — and the
			// label list is already on this view, so there is nothing to look up
			// elsewhere. typeLabel falls back to the token, which is the right answer
			// for a value the chip set does not carry: printing it is how somebody
			// finds out (planLabel's rule).
			return v.typeLabel(v.OnFile.BusinessType)
		}
	case "timezone":
		if v.Form.Timezone != v.OnFile.Timezone {
			return v.OnFile.Timezone
		}
	}
	return ""
}

// typeLabel is the customer's word for one business type, read from the SAME list the
// <select> was drawn from so the two can never disagree.
func (v AccountView) typeLabel(value string) string {
	for _, t := range v.Types {
		if t.Value == value {
			return t.Label
		}
	}
	return value
}

// DescribedBy is the aria-describedby value for one field: its help text, plus its
// ERROR when it has one.
//
// 🔴 IT EXISTS BECAUSE THE MESSAGE HAD NO MACHINE-READABLE LINK TO ITS INPUT, AND THE
// CODE CLAIMED OTHERWISE. internal/handler's validateAccountForm argues at length that
// it returns the FIELD so "a message renders beside the input it is about" — and
// nothing measured that. A message that is merely NEAR an input is beside it for a
// sighted reader and nowhere at all for a screen reader; this makes the association a
// value the browser follows and a test can name.
func (v AccountView) DescribedBy(field string) string {
	help := "account-" + field + "-help"
	if v.ErrorFor(field) == "" {
		return help
	}
	return help + " account-" + field + "-error"
}

// ShowBilling reports whether the link to the money screen is drawn.
func (v AccountView) ShowBilling() bool { return v.BillingHref != "" }
