package pages

import "strconv"

// THE PRODUCT TOUR (LP-08, 2026-10-22): /product, drawn from the Product / T-Product /
// M-Product boards (docs/design/landing-redesign/boards). It joins the public site the
// way the pricing and security pages did (pricingview.go records the shape): a row of
// PublicPages, a named route in internal/handler's Mount, every sentence a value here
// with the facts it rests on, and every number handed in by internal/handler.
//
// THE PAGE WALKS THE PRODUCT IN THE ORDER A SHIFT DOES: the tap, The Pass, The Spike,
// The Roll, People, Venues & plaques and Rules (the seven entries of "On this page"),
// then a shift from the invitation to the payroll file and the integrations. Each
// section says what that screen of the product does; the drawings beside them are
// sample data, held to the product by internal/handler's tests the way the landing's
// panel drawings are (LP-05).
//
// WHERE THE BOARD SAID MORE THAN THE PRODUCT DOES, the value here says less, and the
// edit is recorded once, for the reader looking for it:
//
//   - "The Taptime guarantees come first and cannot be switched off." (README O1, U7):
//     the public site's unmeasured-claim scanner refuses the word on every page, with no
//     exception, so the bullet says "guardrails" -- the engine's own word for sys:* --
//     and the Rules drawing's label says "Taptime's guardrails" where the panel prints
//     its own heading.
//     THE TWO WORDS DIFFER ON PURPOSE: the panel's Rules screen heads the same list
//     "Taptime guarantees" (pages/policies.templ), and the public site may not print that
//     word. Aligning the panel's term is a backlog item for the orchestrator, not this
//     page's; until then the public page says "guardrails" and the panel "guarantees".
//   - "Nobody can decide on their own record." (U10) is gone. The self-review guard
//     never meets a real review today: a reviewer is an admin_users id and a record's
//     subject an employees id (LP-07's security review, backlog T148). The Spike says
//     instead that a decision is made once and stored beside the record.
//   - "The link is shown once, on the next screen. Taptime keeps no copy of it." (U5)
//     names both delivery modes: with e-mail delivery the link is mailed and not shown,
//     and what is kept is a keyed hash, not nothing.
//   - "A second tap by the same person moments later is recorded, not counted." says the
//     window and its default instead of "moments".
//   - "The answer is a stamped docket" says "a recorded tap": a refused link or a
//     broken page ends on the problem screen, not on a docket.
//   - "Pick any earlier day to read it in full." (U2) became "Step back to any earlier
//     day and read its records": the records list steps one day at a time.
//   - "When no rule decides a tap, it goes to The Spike — never approved by default."
//     says what the engine's default does, without the "never" the security review
//     refused ("never silently approved": an owner can write a rule that allows).
//   - "hand over the invite" (People, and the shift's first step) says "invite": with
//     e-mail delivery nobody hands anything over.
//   - "If neither the venue's network nor GPS can place the tap, it is still recorded —
//     flagged, never approved in silence." says "by default", as the security page does.
//   - The Rules line's "recorded with your name against it" says "against the owner who
//     made it": the trail records the account, and the page does not promise how a
//     name is shown.
//   - The Venues drawing leaves out the GPS point's coordinates the board prints: the
//     venue is a real business's (K1), and the card's own "GPS ✓" says the point is set.
//
// THE NUMBERS ARE NOT TYPED HERE: ProductFigures carries the debounce window and the
// webhook header, filled by internal/handler from the values the security page already
// holds to their source.

// ProductPath is the page's URL.
const ProductPath = "/product"

// ProductView is the product tour.
type ProductView struct {
	// SignupHref and SignInHref are the chrome's two ways in, as on LandingView.
	SignupHref string
	SignInHref string
	// FreeMonths is the founding offer's count, for the sign-off.
	FreeMonths int
	// Figures are the page's numbers; Copy is every sentence built from them, computed
	// once by internal/handler (ProductCopy).
	Figures ProductFigures
	Copy    ProductText
}

// ProductFigures is every number or product name the page prints, each read from the
// product by internal/handler.
type ProductFigures struct {
	// DebounceSeconds is the deployment's default window for the same person
	// (policy.DefaultParams, internal/config).
	DebounceSeconds int
	// WebhookHeader is the header a webhook delivery is signed in
	// (webhook.SignatureHeader).
	WebhookHeader string
}

// The facts only this page claims. Each is derived from the product in
// internal/handler/product_facts_test.go; the closure test reads this block like the
// one in landingview.go.
const (
	// A tap the engine decides is written and ends on the result screen's docket,
	// stamped with its verdict's word: APPROVED, FLAGGED, REJECTED or IGNORED. Derived
	// from pages/result.templ (the docket and its stamp's four words) and the tap
	// handler rendering pages.Result for a recorded outcome.
	FactRecordedTapEndsOnAStampedDocket Fact = "templ:Result docket+stamp's four words"
	// A counted tap's screen ends on "All done — you can close this page." and the
	// result screen carries no button, link or form. Derived from pages/result.templ.
	FactResultScreenEndsWithoutAButton Fact = "templ:Result closing, no control"
	// The tap screen is the greeting docket and one form with one button: no menu, no
	// link, no second control. Derived from pages/tap.templ's Tap, TapHeading and
	// tapForm.
	FactTapScreenIsOneButton Fact = "templ:Tap = TapHeading+tapForm(one TapButtonFace)"
	// The records list steps back one day at a time ("Previous day"), and the day is a
	// query the handler reads. Derived from pages/transactions.templ's dayStepper and
	// internal/handler's parseTransactionFilter.
	FactRecordsReadForAnyEarlierDay Fact = "templ:dayStepper Previous day+handler date filter"
	// The Spike shows the record's evidence tiles and "Why it is here": the deciding
	// rule's own sentence, which tap.Decide copies onto the record. Derived from
	// components/docket.templ's SpikeDocket and internal/domain/tap.
	FactSpikeShowsEvidenceAndTheRulesSentence Fact = "templ:SpikeDocket tiles+rule sentence"
	// A manager's decision can carry a note: The Spike's form has a note field, the
	// handler reads it, and transaction_reviews keeps it. Derived from pages/review.templ,
	// internal/handler/review.go and migration 00005. (README §10 U3.)
	FactReviewCarriesANote Fact = "templ:review note field+transaction_reviews.note"
	// A check-in with no check-out is listed on The Roll, is not counted, and the week's
	// figure says it is short by it. Derived from pages/reports.templ.
	FactOpenCheckInIsListedNotCounted Fact = "templ:reports open check-ins, not counted"
	// Lateness is measured against the department's shift when there is one, else the
	// shift of the venue the record is at (the tapped venue), and The Roll shows it.
	// Derived from AnchorLatenessFollowsDepartment, the worked-shift query's join on the
	// record's own location, and pages/reports.templ.
	FactLatenessFollowsOwnShift Fact = "ledger:department shift, else the record's venue"
	// A person has one of three statuses and People prints it as a word: INVITED, ACTIVE
	// or DEACTIVATED. Derived from components/roster.templ's lifecyclePill and the
	// employees table's status CHECK.
	FactPeopleShowOneStatus Fact = "templ:lifecyclePill+employees.status CHECK"
	// An invitation link is shown once on the screen that minted it, or e-mailed to the
	// person's own address when the deployment delivers by e-mail; the database keeps a
	// keyed hash of it. Derived from pages/employees.templ, internal/config's two
	// delivery modes, internal/invite and migration 00012. (README §10 U5.)
	FactInviteLinkShownOnceOrMailed Fact = "templ:shown once|config:email+invite code hash"
	// A record can be entered by hand from the person's own card. Derived from
	// internal/handler/employees.go (the card's RecordHref) and components/roster.templ.
	FactManualEntryStartsFromThePerson Fact = "handler:RecordHref from the person's card"
	// A venue keeps its plaques, its network ranges, a GPS point and its shift, and
	// Venues & plaques prints each. Derived from AnchorVenueShiftAndAddress,
	// AnchorPlaqueBelongsToVenue, migration 00002's GPS pair and
	// components/venuedetail.templ.
	FactVenueHoldsItsEvidenceAndHours Fact = "schema:locations ranges+gps+shift, tags.location_id"
	// A rule change is shown in words on a confirmation page before it is written, the
	// write checks that confirmation, and every change is recorded against the account
	// that made it. Derived from internal/handler/policyactions.go and
	// internal/domain/tenant's audit actions.
	FactRuleChangeIsConfirmedThenRecorded Fact = "handler:policy change review→confirm→apply with Actor"
	// Only an owner may change a rule (the guardrail sys:policy-edit-owner-only, which
	// the panel asks before it draws a control); everybody else reads the page.
	// Derived from internal/policy, internal/handler/policies.go and pages/policies.templ.
	FactOnlyAnOwnerChangesARule Fact = "policy:sys:policy-edit-owner-only+MayEdit"
	// A business has at most one webhook endpoint, and it can be set up only once the
	// business's integration API is open. Derived from migration 00048 and
	// internal/webhook/endpoints.
	FactWebhookIsOneEndpointBehindTheAPI Fact = "schema:webhook_endpoints one per tenant+ErrAPIClosed"
)

// ProductIndexEntry is one line of "On this page": the section's number, its name and
// the id it links to.
type ProductIndexEntry struct {
	No    string
	Title string
	ID    string
}

// The seven sections' ids, in the page's order.
const (
	ProductTapID    = "tap"
	ProductPassID   = "pass"
	ProductSpikeID  = "spike"
	ProductRollID   = "roll"
	ProductPeopleID = "people"
	ProductVenuesID = "venues"
	ProductRulesID  = "rules"
)

// ProductIndex is "On this page". Every entry links to an id the page renders, and a
// test follows each.
var ProductIndex = []ProductIndexEntry{
	{No: "01", Title: "The tap", ID: ProductTapID},
	{No: "02", Title: "The Pass", ID: ProductPassID},
	{No: "03", Title: "The Spike", ID: ProductSpikeID},
	{No: "04", Title: "The Roll", ID: ProductRollID},
	{No: "05", Title: "People", ID: ProductPeopleID},
	{No: "06", Title: "Venues & plaques", ID: ProductVenuesID},
	{No: "07", Title: "Rules", ID: ProductRulesID},
}

// ProductSection is one tour section's words: the line under its heading (Line has no
// words for the tap, whose paragraph is the landing's third step) and its bullets.
type ProductSection struct {
	Lines   []Line
	Bullets []Line
}

// ProductPoint is one of the tap screen's four points: a title and its sentence.
type ProductPoint struct {
	Title Line
	Body  Line
}

// ProductStep is one docket of "A shift, start to finish": who does it, what it is
// called, and what happens.
type ProductStep struct {
	No    string
	Who   string
	Title string
	Text  Line
	// Flagged draws the step's FLAGGED stamp.
	Flagged bool
}

// ProductIntegration is one item of the integrations card: its name, its sentence and
// the panel or API names it shows in the data face.
type ProductIntegration struct {
	Title string
	Text  Line
	Code  []string
}

// ProductText is every sentence of the page, built from the figures by ProductCopy.
// Every Line inside it, at any depth, is collected by internal/handler's tests by
// reflection, so a Line added here is checked for its pins and for being rendered.
type ProductText struct {
	Heading Line
	Lede    Line

	Tap         ProductSection
	TapCapTap   Line
	TapCapFlag  Line
	TeamHeading Line
	TeamLede    Line
	TeamPoints  []ProductPoint

	Pass   ProductSection
	Spike  ProductSection
	Roll   ProductSection
	People ProductSection
	Venues ProductSection
	Rules  ProductSection

	ShiftHeading Line
	ShiftLede    Line
	ShiftSteps   []ProductStep

	IntegHeading Line
	Integrations []ProductIntegration
	IntegNote    Line
}

// ProductCopy is every sentence of the page for the figures f. Each number in a
// sentence is one of f's; no number is written below.
func ProductCopy(f ProductFigures) ProductText {
	l := func(text string, facts ...Fact) Line { return Line{Text: text, Facts: facts} }
	debounce := strconv.Itoa(f.DebounceSeconds) + " seconds"
	return ProductText{
		Heading: l("From one tap to the week's hours.", FactTapPageNeedsNoApp, FactRollIsTheWeekPerPerson),
		Lede: l("Your people touch a plaque with their own phone. Managers read the record, settle the "+
			"doubtful ones and export the week. These are the screens in between.",
			FactTapPageNeedsNoApp, FactPassShowsTheDaysDocketsNewestFirst, FactFlaggedQueueDecidedByAManager,
			FactHoursExportAsCSV),

		Tap: ProductSection{
			Bullets: []Line{
				l("The phone's position is read once, at the press — never in the background.",
					FactPositionReadOnlyOnPress),
				l("If the same person taps again within "+debounce+", that tap is ignored.",
					FactDebounceIsPerPersonOnTheServerClock),
				l("A recorded tap ends on a stamped docket: APPROVED, FLAGGED, REJECTED or IGNORED.",
					FactRecordedTapEndsOnAStampedDocket),
			},
		},
		TapCapTap:   l("What opens when the phone touches the plaque.", FactTapPageNeedsNoApp, FactPlaqueIdentifiesThePlace),
		TapCapFlag:  l("When the evidence is short, the screen says so.", FactRecordedTapEndsOnAStampedDocket, FactFlaggedQueueDecidedByAManager),
		TeamHeading: l("One screen, one button.", FactTapScreenIsOneButton),
		TeamLede: l("No menu, no settings, no history to scroll: a greeting, the venue that was tapped and one button.",
			FactTapScreenIsOneButton, FactTapPageGreetsByName, FactPlaqueIdentifiesThePlace),
		TeamPoints: []ProductPoint{
			{
				Title: l("No app to install", FactTapPageNeedsNoApp),
				Body:  l("The plaque opens an ordinary web page in the phone's own browser.", FactTapPageNeedsNoApp, FactPlaqueIsPassive),
			},
			{
				Title: l("Greeted by name", FactTapPageGreetsByName),
				Body:  l("The page says hello by name and shows the venue that was tapped.", FactTapPageGreetsByName, FactPlaqueIdentifiesThePlace),
			},
			{
				Title: l("Made for wet hands", FactTapButtonSizedForAWetHand),
				Body:  l("One large button, sized for a wet or gloved finger.", FactTapButtonSizedForAWetHand, FactTapScreenIsOneButton),
			},
			{
				Title: l("Done means done", FactResultScreenEndsWithoutAButton),
				Body: l("A counted tap ends on its stamp and “All done — you can close this page.” There is no button on it.",
					FactResultScreenEndsWithoutAButton, FactRecordedTapEndsOnAStampedDocket),
			},
		},

		Pass: ProductSection{
			Lines: []Line{LandingPanelPass.Line},
			Bullets: []Line{
				l("Step back to any earlier day and read its records.", FactRecordsReadForAnyEarlierDay),
				l("A record typed in by a manager carries “Entered by a manager”.", FactManualEntryNamesAManager),
				l("Nothing here can be edited or deleted: a correction is a new record.",
					FactRecordsAreImmutable, FactManualEntryNamesAManager),
			},
		},
		Spike: ProductSection{
			Lines: []Line{LandingPanelSpike.Line},
			Bullets: []Line{
				l("Each record shows its evidence and the rule's own sentence for why it is here.",
					FactSpikeShowsEvidenceAndTheRulesSentence),
				l("A note can go on the record with the decision.", FactReviewCarriesANote),
				l("Each record is decided once, and the decision is stored beside it; the record itself never changes.",
					FactReviewIsOnceAndFinal, FactRecordsAreImmutable),
			},
		},
		Roll: ProductSection{
			Lines: []Line{LandingPanelRoll.Line},
			Bullets: []Line{
				l("A check-in with no check-out is listed, not counted, and the week's figure says it is short.",
					FactOpenCheckInIsListedNotCounted),
				l("Manual entries are marked apart from tapped ones.", FactManualEntryNamesAManager),
				l("Lateness is measured against the person's own shift: their department's if it has one, else the "+
					"shift of the venue the record is at (for a tap, the venue tapped).",
					FactLatenessFollowsOwnShift),
			},
		},
		People: ProductSection{
			Lines: []Line{l("Add somebody, invite them, and switch them off the day they leave.",
				FactPeopleAreInvitedFromTheDashboard, FactDeactivatedPersonIsRefused)},
			Bullets: []Line{
				l("Each person shows one status: INVITED, ACTIVE or DEACTIVATED.", FactPeopleShowOneStatus),
				l("The invitation link is either shown once, on the next screen, or e-mailed to the person's own "+
					"address. Taptime keeps only a keyed hash of it.", FactInviteLinkShownOnceOrMailed),
				l("Switch somebody off and their next tap is refused.", FactDeactivatedPersonIsRefused),
				l("A missed tap can be entered by hand from the person's card, marked as entered by a manager.",
					FactManualEntryStartsFromThePerson, FactManualEntryNamesAManager),
			},
		},
		Venues: ProductSection{
			Lines: []Line{l("Each venue keeps its own plaques, network addresses, GPS point and shift hours.",
				FactVenueHoldsItsEvidenceAndHours)},
			Bullets: []Line{
				l("A retired or lost plaque decides nothing: taps on it are rejected.", FactPlaqueCanBeReplaced),
				l("An address list too wide to stand for a single network is refused when you save it, and not "+
					"counted when a tap is judged.", FactWideAddressListIsNoEvidence),
				l("Departments can keep shift hours of their own.", FactVenuesAndDepartmentsCarryOwnHours),
			},
		},
		Rules: ProductSection{
			Lines: []Line{l("You can switch a Taptime baseline rule off, and write your own version of one that applies "+
				"at the venues you choose. Every change is shown to you in words before it is written, and is "+
				"recorded against the owner who made it.",
				FactBaselineCanBeChanged, FactRuleChangeIsConfirmedThenRecorded, FactOnlyAnOwnerChangesARule)},
			Bullets: []Line{
				l("Taptime's guardrails come first and cannot be switched off.", FactRulesRunInOrder, FactGuardrailsAreTheList),
				l("Only an owner can change a rule; managers see the page read-only.", FactOnlyAnOwnerChangesARule),
				l("When no rule decides a tap, the default sends it to The Spike.",
					FactUndecidedTapIsFlagged, FactFlaggedQueueDecidedByAManager),
			},
		},

		ShiftHeading: l("From the invite link to the payroll file.",
			FactPeopleAreInvitedFromTheDashboard, FactHoursExportAsCSV),
		ShiftLede: l("Who does what, from a new hire's first link to the hours your payroll reads.",
			FactPeopleAreInvitedFromTheDashboard, FactActivationEndsOnARealTap, FactHoursExportAsCSV),
		ShiftSteps: []ProductStep{
			{No: "01", Who: "Manager", Title: "Invite",
				Text: l("Add the person in People, then invite them.", FactPeopleAreInvitedFromTheDashboard)},
			{No: "02", Who: "Employee", Title: "Activation tap",
				Text: l("They open the link on their own phone and confirm. Their first tap on one of your plaques finishes the "+
					"setup — it records no hours.", FactActivationEndsOnARealTap)},
			{No: "03", Who: "Employee", Title: "Clock in",
				Text: l("From then on, a tap opens the tap screen and one press records it.",
					FactTapPageNeedsNoApp, FactTapScreenIsOneButton)},
			{No: "04", Who: "Taptime", Title: "Flag", Flagged: true,
				Text: l("If neither the venue's network nor GPS can place the tap, it is still recorded and, by "+
					"default, flagged for a manager.", FactFlaggedQueueDecidedByAManager)},
			{No: "05", Who: "Manager", Title: "Decision",
				Text: l("A manager approves or rejects it on The Spike. The decision is stored beside the record.",
					FactReviewIsApproveOrReject, FactReviewIsOnceAndFinal)},
			{No: "06", Who: "Manager", Title: "The Roll",
				Text: l("Hours add up per person on The Roll, and the week downloads as CSV for your payroll.",
					FactRollIsTheWeekPerPerson, FactHoursExportAsCSV)},
		},

		IntegHeading: l("Hand the hours to the payroll you already run.", FactHoursExportAsCSV, FactAPIFeedsEveryCountedTap),
		Integrations: []ProductIntegration{
			{
				Title: "CSV export",
				Text: l("Hours per person and per venue, and the monthly headcount you're billed on, download as CSV.",
					FactHoursExportAsCSV, FactReportPerPersonAndVenue, FactBillingExportAsCSV),
				Code: []string{ProductRollCSVLabel, ProductBillingCSVLabel},
			},
			{
				Title: "Integration API",
				Text: l("Your HR system can write your people and locations, and read every counted tap from a feed; "+
					"entries typed by a manager stay in the CSV.",
					FactAPIWritesPeopleAndLocations, FactAPIFeedsEveryCountedTap),
				Code: ProductAPIRoutes,
			},
			{
				Title: "Webhooks",
				Text: l("Each counted tap can also be sent to one endpoint of yours, signed so you can check it came from Taptime.",
					FactAPIFeedsEveryCountedTap, FactWebhooksAreSigned, FactWebhookIsOneEndpointBehindTheAPI),
				Code: []string{f.WebhookHeader + ": sha256=…"},
			},
		},
		IntegNote: l("The integration API is switched on for your business by Taptime, and the webhook can be set up once it is.",
			FactAPIEnabledPerBusinessByTaptime, FactWebhookIsOneEndpointBehindTheAPI),
	}
}

// The integrations card's names, each the product's own: the two CSV buttons' labels
// (The Roll's and the Billing screen's) and three routes of the integration API, as
// its OpenAPI document names them. internal/handler's tests hold each to its source.
const (
	ProductRollCSVLabel    = "CSV for the accountant"
	ProductBillingCSVLabel = "Download CSV"
)

// ProductAPIRoutes are the integration API's routes the card names: method and path.
var ProductAPIRoutes = []string{"PUT /v1/employees/{ref}", "PUT /v1/locations/{ref}", "GET /v1/punches"}

// --- the drawings -----------------------------------------------------------------------
//
// SAMPLE DATA, and the page says so (ProductSampleNote; every drawing's accessible name
// begins "Sample screen:"). The Pass, The Spike and The Roll are the landing's own
// drawings (LandingPanel*), already held to the engine. What is new here is held the
// same way by internal/handler: the tap phones' record is The Pass's flagged sample run
// through tap.Decide, and the result's words are what pages.Result renders for it; the
// people are invented (none is a person of the seed) and their status and signed-in
// words are the panel's; the venues and their hours are the seed's; the rules are the
// engine's, numbered and worded as the Rules screen prints them.

// ProductSampleNote is printed under the drawings. It is a label, not a claim.
const ProductSampleNote = "Drawn for this page — sample names and times."

// ProductTapSample is the record the two phones draw: The Pass's flagged docket (a real
// NFC tap with neither the venue's network nor a position).
var ProductTapSample = LandingPanelPassDockets[2]

// ProductTapPlaque is the drawn plaque's line: the venue and the plaque, as The Spike's
// record in focus names it (LandingPanelSpikeFocus).
const ProductTapPlaque = "KF MSIDA · PLAQUE 7E10"

// ProductTapNote is the sentence the result screen prints under a tap the evidence could
// not place: the deciding rule's own (base:no-evidence-review).
const ProductTapNote = "neither IP nor GPS could place this tap"

// ProductTapClosing is what the result screen says under a flagged tap
// (pages/result.templ closing).
var ProductTapClosing = []string{
	"Recorded — it needs your manager's approval before it counts toward your hours. Tell them if that looks wrong.",
	"You can close this page.",
}

// ProductPerson is one sample row of People: initials, name, home venue, the status
// (the database's word) and how many phones are signed in, with the words the list
// prints for that count.
type ProductPerson struct {
	Initials string
	Name     string
	Venue    string
	Status   string
	Devices  int
	Signed   string
}

// ProductPeople is People's sample list. The three active people are the landing
// panel's samples; the invited one has never activated, so no phone is signed in; the
// deactivated one keeps a signed-in phone, because switching somebody off does not sign
// a phone out (it only refuses the next tap).
var ProductPeople = []ProductPerson{
	{Initials: "MV", Name: "Maria Vella", Venue: "KF St Julians", Status: "active", Devices: 1, Signed: "1 signed in"},
	{Initials: "CM", Name: "Claire Mifsud", Venue: "KF Msida", Status: "active", Devices: 1, Signed: "1 signed in"},
	{Initials: "DZ", Name: "Daniel Zammit", Venue: "KF Valletta", Status: "active", Devices: 1, Signed: "1 signed in"},
	{Initials: "NA", Name: "Noel Azzopardi", Venue: "KF Valletta", Status: "invited", Devices: 0, Signed: "None signed in"},
	{Initials: "MG", Name: "Mark Galea", Venue: "KF St Julians", Status: "deactivated", Devices: 1, Signed: "1 signed in"},
}

// ProductPeopleChips are People's status filter, as the list labels it: everyone, then
// the three statuses in the ledger's order.
var ProductPeopleChips = []string{"Everyone", "Invited", "Active", "Deactivated"}

// ProductPeopleAdd is People's heading control.
const ProductPeopleAdd = "Add somebody"

// ProductStatusWord is the word People prints for a status (components lifecyclePill).
func ProductStatusWord(status string) string {
	switch status {
	case "invited":
		return "INVITED"
	case "active":
		return "ACTIVE"
	case "deactivated":
		return "DEACTIVATED"
	}
	return ""
}

// ProductVenue is one sample venue card: the seed's venue, its shift as the card prints
// it, and whether a network and a GPS point are set.
type ProductVenue struct {
	Name  string
	Shift string
	IP    bool
	GPS   bool
}

// ProductVenues is Venues & plaques' sample list: four of the seed's venues, the first in
// focus.
var ProductVenues = []ProductVenue{
	{Name: "KF St Julians", Shift: "10:00 – 22:00", IP: true, GPS: true},
	{Name: "KF Msida", Shift: "10:00 – 22:00", IP: true, GPS: true},
	{Name: "KF Valletta", Shift: "10:00 – 22:00", IP: true, GPS: true},
	{Name: "Rusty Bar", Shift: "18:00 – 02:00", IP: true, GPS: true},
}

// ProductVenueNetwork is the focused venue's network range, as the seed records it.
const ProductVenueNetwork = "203.0.113.15/32"

// ProductPlaque is one sample plaque of the focused venue: an invented id and the word
// the venue's plaque table prints for its status.
type ProductPlaque struct {
	UID    string
	Status string
}

// ProductPlaques is the focused venue's two plaques: the one on the wall and the one it
// replaced, kept on purpose.
var ProductPlaques = []ProductPlaque{
	{UID: "0491AC7E55A3C1", Status: "In service"},
	{UID: "04E41290AB3E12", Status: "Retired"},
}

// ProductRuleRow is one sample row of the Rules drawing: the engine's statement, the
// number the Rules screen gives it (guardrails only), its words there and the effect's
// label.
type ProductRuleRow struct {
	Sid    string
	No     string
	Words  string
	Effect string
}

// ProductGuardRows are three of the guardrails as the Rules screen lists them.
var ProductGuardRows = []ProductRuleRow{
	{Sid: "sys:sun-invalid", No: "03", Words: "The NFC signature is wrong, or the plaque's counter did not move", Effect: "Reject"},
	{Sid: "sys:employee-deactivated", No: "05", Words: "The person has been deactivated", Effect: "Reject"},
	{Sid: "sys:person-debounce", No: "08", Words: "The same person taps again inside the repeat window", Effect: "Ignore — recorded, counts nothing"},
}

// ProductBaselineRows are three of the baseline rules as the Rules screen lists them.
var ProductBaselineRows = []ProductRuleRow{
	{Sid: "base:ip-or-gps-ok", Words: "IP proof of place", Effect: "Approve"},
	{Sid: "base:gps-only-allow", Words: "GPS-only allowance", Effect: "Approve"},
	{Sid: "base:no-evidence-review", Words: "no evidence needs review", Effect: "Send to The Spike"},
}
