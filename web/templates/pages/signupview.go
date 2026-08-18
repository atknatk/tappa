package pages

// The SIGN-UP WIZARD's view models (M7-02).
//
// 🔴 THIS FILE IS WRITTEN UNDER **BOTH** OF THIS PACKAGE'S RULES AT ONCE, which no
// other view file in it is. view.go's rule is the §4.7 one — a view model exists so
// a template cannot render a secret, because there is no field for one to travel in.
// landingview.go's rule is the opposite hazard — a marketing page is made entirely
// of claims, so every sentence must be answerable with "which code provides this?".
// The wizard is both: it takes a PASSWORD (view.go's hazard) and it is the page that
// sells the product (landingview.go's hazard).
//
// SO, EXPLICITLY:
//
//   - THERE IS NO Password FIELD ANYWHERE BELOW. The account form's input is written
//     as a literal in the template with no value attribute, so a re-rendered form
//     comes back EMPTY and the person types it again. A password echoed into a body
//     is a password in the back/forward cache and in "save page".
//   - THERE IS NO TenantID, NO AdminUserID AND NO EMAIL ON THE DONE SCREEN. The
//     confirmation names the business, its venues and its VAT number — things the
//     visitor typed — and nothing the server minted.
//   - THE PRICE AND THE FREE PERIOD ARE NOT REPEATED HERE. The landing page carries
//     them, pinned against migration 00016 by
//     TestLanding_PriceMatchesTheSchemaItIsCharged; printing them a second time on
//     the wizard would be a second copy of the two numbers a customer holds us to,
//     and the wizard is not where a price is agreed.

// SignupChoice is one selectable option — a business-type chip.
//
// THE VALUES COME FROM internal/domain/signup.BusinessTypes, which is itself pinned
// against migration 00001's CHECK. This type carries no default and no "other"
// fallback of its own: a chip the schema would refuse must not be renderable.
type SignupChoice struct {
	Value string
	Label string
}

// SignupBusinessView is step one: who is registering.
//
// Errors is keyed by FIELD NAME so a message renders beside the input it is about.
// The brand's rule is that an error says what to do rather than blaming, and a
// message floating at the top of a form cannot say which field it means.
type SignupBusinessView struct {
	CSRFToken string
	Types     []SignupChoice

	// The values as the SERVER normalised them, not as they were typed. A
	// re-rendered form therefore shows the VAT number the way it will be stored,
	// which is the one representation internal/domain/signup keeps.
	Name         string
	VATNumber    string
	BusinessType string
	Structure    string

	Errors map[string]string

	// SignInHref is set ONLY on the "this VAT number is already registered" answer,
	// where the useful next act is to sign in rather than to register again. It is
	// empty on every other render, so the wizard does not offer a sign-in link to
	// somebody who is halfway through creating an account.
	SignInHref string
}

// SignupPlacesView is step two: the doors.
type SignupPlacesView struct {
	CSRFToken    string
	BusinessName string

	// Multi is true when step one chose several places. IT DECIDES WHETHER THE FORM
	// CAN GROW: a `single` business gets exactly one row and no "add another"
	// control, because the server refuses a second one (internal/domain/signup's
	// ValidateVenues) and a button whose result is a refusal is a screen
	// contradicting itself.
	Multi bool
	// MaxVenues is printed so the ceiling is visible before somebody hits it, and it
	// comes from the domain rather than being typed here.
	MaxVenues int

	// Venues is what to pre-fill, one entry per row. At least one entry is always
	// present so the form is usable on first arrival.
	Venues []string

	Errors map[string]string
	// BackHref returns to step one. The wizard's steps are plain links backwards and
	// posts forwards, so a back button behaves.
	BackHref string
}

// SignupAccountView is step three: the first operator.
//
// IT CARRIES THE BUSINESS NAME AND THE VENUE LIST as a read-back — the last chance
// somebody has to notice a typo before two permanent rows exist. `transactions` is
// immutable and a tenant cannot be deleted (§4.6), so the cheapest possible moment
// to catch "Kebab Facotry" is here.
type SignupAccountView struct {
	CSRFToken    string
	BusinessName string
	Venues       []string

	// HoneypotField is the NAME of the input no human fills in. It is passed in
	// rather than written into the template so that the server's check and the
	// form's field are the same string — internal/handler/signupratelimit.go owns
	// the name, and a second copy here is exactly the drift that would make the
	// defence silently inert.
	HoneypotField string

	// What was typed, so a refused submission does not lose it. NOTE WHAT IS ABSENT:
	// there is no Password field, deliberately — see this file's header.
	FullName string
	Email    string

	Errors map[string]string
}

// SignupDoneView is the confirmation — the APPROVED docket the M7-02 card asks for.
//
// 🔴 THE STAMP IS A CLAIM AND IT IS A TRUE ONE. This screen is reached only after
// internal/domain/signup.Provision has COMMITTED: the tenant, its venues and its
// first operator exist, in one transaction, or none of them do. That is the whole
// reason this task provisions rather than collecting — an APPROVED stamp over a
// registration that had not happened would be the "screen promising a capability
// that is not mounted" defect M7-01's card warns about, printed in the brand's most
// emphatic component.
type SignupDoneView struct {
	BusinessName string
	Venues       []string
	VATNumber    string

	// The VIES outcome, as the THREE states migration 00017 stores rather than as a
	// bool. Both false means "we could not check", which is a different sentence
	// from "the register says no" and must not be shown as one.
	VATVerified bool
	VATRefused  bool

	// SignInBlocked is the one state in which this screen must NOT tell somebody to
	// sign in, because it was MEASURED that they cannot.
	//
	// 🔴 IT IS THE OTHER HALF OF A TRADE MIGRATION 00017 MADE AND DID NOT BUILD.
	// Ordering the login resolver by created_at protects an existing customer from
	// being displaced and, in exchange, puts a NEW registration last — so an address
	// already carrying adminauth.MaxCandidates identities produces an account nobody
	// can sign into. 00017 called that acceptable because the customer "finds out
	// immediately, at a moment when they are on our page and can be told something",
	// and an audit measured that the product did no such thing: APPROVED, "Sign in",
	// then 401.
	//
	// FALSE IS THE DEFAULT AND ALSO THE ANSWER WHEN THE CHECK COULD NOT BE TAKEN. A
	// read failure on our side must not tell a customer their account is broken.
	SignInBlocked bool

	// SignInHref is where the wizard ends. It is a link to the panel's sign-in page
	// rather than a session: internal/handler/signup.go's Done says why the wizard
	// deliberately does not sign anybody in.
	SignInHref string
}

// VATNote is the one sentence the confirmation says about the VAT check.
//
// THREE OUTCOMES, THREE SENTENCES, AND NONE OF THEM IS SILENCE. §4.6's shape applied
// to a check rather than to a record: a number the European Commission refused must
// not look like one it confirmed, and a check that never completed must not look
// like either. The "could not check" wording is deliberately about US — it is our
// outbound call that failed, not the customer's number.
//
// 🔴 THE FIRST VERSION OF BOTH SENTENCES POINTED AT A DASHBOARD THAT COULD NOT ANSWER,
// AND THE HISTORY IS KEPT BECAUSE IT IS THE REASON THIS PARAGRAPH EXISTS. They said
// "check the number on your dashboard before your first invoice" and "it will show as
// unverified on your dashboard" while NO PANEL SCREEN READ tenants.vat_verified or
// tenants.vat_checked_at — M7-01's blocking finding (a screen declaring a capability
// that is not mounted) repeated one task later, under the brand's most emphatic
// component. The stamp was right; the note was not. So the sentences were rewritten to
// describe only what the product did that day.
//
// ✅ AND M7-05 BUILT THE SURFACE, SO THEY POINT AT IT AGAIN — this time measured.
// /admin/account reads both columns and prints one of four states permanently;
// db/queries/tenants.sql's GetTenantAccount is the query, and three generated row types
// in internal/store now carry the columns (up from one, which was CreateTenant's
// RETURNING echoing its own INSERT).
//
// THE CONSTRAINT RELEASED ITSELF, WHICH IS WHAT IT WAS BUILT TO DO.
// TestSignupDone_PromisesNoPanelSurfaceForTheVATCheck derives the count from the
// GENERATED queries rather than from a hand list: while nothing read the columns it
// forbade these sentences naming a panel, and the moment something did it logged "this
// test no longer constrains the wording" and stood down. If a later change removes that
// query, the count falls and the tripwire re-arms against exactly this paragraph.
//
// ⚠️ WHAT IS STILL NOT TRUE, AND IS THEREFORE STILL NOT PROMISED: a RE-CHECK. Migration
// 00017 withholds UPDATE on both columns and M7-05 measured that the grant should stay
// withheld, so the sentences say where the answer can be READ and never that it can be
// asked again. TestAccountDB_TheAppRoleHoldsNoUpdateOnTheVATColumnsOrTheTerms pins the
// privilege side of that.
//
// THE SECTION IS NAMED THROUGH SectionLabel RATHER THAN AS A LITERAL, so a renamed tab
// renames it here too; a customer sent looking for a tab that no longer exists is the
// same defect in a smaller size.
//
// ⚠️ THE "COULD NOT REACH" SENTENCE PROMISES WHERE THE NUMBER STANDS AND NOT WHAT THE
// REGISTER SAID, WHICH IS A CORRECTION. It read "always shows what the register last
// said" — but for THIS cohort the register said nothing, and checkedAt stores NULL in
// both columns for VATUnknown, so the account screen shows "not checked". The two
// screens described one event in contradictory words until the account sentence was
// made to name this cohort explicitly, and
// TestAccount_TheNoAnswerCohortIsCalledTheSameThingOnBothScreens is what holds the two
// screens together.
func (v SignupDoneView) VATNote() string {
	switch {
	case v.VATVerified:
		return "We checked this number against the EU VAT register and it came back valid."
	case v.VATRefused:
		return "The EU VAT register did not recognise this number. Your account works either " +
			"way, and we have recorded that the check did not pass — but the number is what " +
			"your invoices will carry, so it is worth checking against your VAT certificate. " +
			"The " + SectionLabel(TabAccount) + " page of your dashboard shows what the " +
			"register said, whenever you want to look."
	default:
		return "We could not reach the EU VAT register just now, so this number has not been " +
			"checked yet. Nothing is blocked and nothing is wrong with your account; the " +
			"number is stored exactly as you typed it. The " + SectionLabel(TabAccount) +
			" page of your dashboard always shows where this number stands."
	}
}

// SignupProblemView is every "this did not work" screen in the wizard.
//
// IT IS SEPARATE FROM ProblemView, which renders inside layout.Page — the narrow
// column sized for a phone held at a plaque. Somebody halfway through registering a
// business is on the MARKETING surface, on a laptop, and dropping them into the
// employee shell would lose the navigation, the footer and the sign-in link they
// need next.
//
// ONE TYPE FOR SEVERAL OUTCOMES, deliberately, and for the reason adminlogin.go's
// problem family gives: an expired wizard, a tampered state and a blocked cookie are
// distinguishable to US and must not be distinguishable in a body.
type SignupProblemView struct {
	Title   string
	Message string
	// Hint is the "what do I do now" line — the brand's rule that an error message
	// never blames and always says what to do next.
	Hint string
	// Retry is the path to start again, or "" when starting again is not the answer
	// (a rate-limited caller must not be handed a button that spends more budget).
	Retry string
}
