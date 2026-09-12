package pages

import "github.com/atknatk/tappa/web/templates/layout"

// The MARKETING surface's view models (M7-01): the landing page and the four
// legal pages under it.
//
// 🔴 THE RULE THIS FILE IS WRITTEN UNDER IS NOT THE USUAL ONE. Everywhere else in
// this package a view model exists to keep a secret off a screen (AdminLoginView
// has no password field, DocketView has no coordinate). Here the hazard is the
// opposite one and it is this repository's signature defect: A LANDING PAGE IS MADE
// ENTIRELY OF CLAIMS, and a claim the product does not keep is a lie that ships.
//
// So every sentence below is answerable with "which code, schema or test provides
// this?", and the answer is written beside it. Three kinds of sentence were
// deliberately NOT written:
//
//   - A NUMBER NOBODY MEASURED. There is no accuracy figure, no uptime figure and
//     no "99%" anywhere on this surface. TestLanding_MakesNoUnmeasuredNumericClaim
//     scans the rendered page for one.
//   - A SUPERIORITY VERDICT. CLAUDE.md §4.1's ban is a FACT about Tappa and a good
//     reason to buy it; "safer than a fingerprint terminal" is a comparison nobody
//     here has run. The comparison table below compares mechanisms and says so in
//     its own footnote.
//   - A CAPABILITY THAT IS NOT MOUNTED. This one has a DATE on it, because the
//     example changed: M7-01 shipped with the sign-up wizard unbuilt, so
//     LandingView.SignupHref was "" and the page said so in words rather than
//     shipping a button that 404s. M7-02 mounted it, handler.NewMarketing now sets
//     the field to signupPath, and the page renders the button at three stops —
//     which is why the guard is a TEST and not this paragraph:
//     TestMarketing_EveryInternalLinkResolves follows every link on the surface
//     against the real router, and TestLanding_OffersTheWizardItMounts drives BOTH
//     branches, including the empty-href one a deployment without the wizard falls
//     back to.
//     The 2026-09-07 rebuild's own instance of this rule is recorded on
//     LandingRules: a draft line said the debounce window is the customer's to set,
//     and no such control exists (policy.Params is one process-wide value read from
//     the environment; the panel prints it and offers no form).

// LandingView is the public landing page.
//
// FOUR FIELDS, AND THREE OF THEM CARRY A NUMBER THE PRODUCT ALSO ENFORCES
// SOMEWHERE ELSE. That duplication is the risk this type exists to bound: a
// marketing price that drifts from the price the database charges is the worst
// kind of wrong page, because both halves look right on their own.
// internal/handler builds these from named constants and
// TestLanding_PriceMatchesTheSchemaItIsCharged re-reads migration 00016 to
// compare.
type LandingView struct {
	// SignupHref is where "Start free" points, or "" while self-service sign-up
	// is not mounted. M7-02 sets it; nothing else about this page changes.
	//
	// IT IS NOT A BOOL. A bool would say "the wizard exists" and still leave the
	// template holding a hard-coded path, which is the shape that produces a
	// button pointing at a route somebody renamed.
	SignupHref string
	// SignInHref is the panel's sign-in page. Passed in rather than written into
	// the template because internal/handler owns where the panel is mounted.
	SignInHref string
	// PricePerEmployeeMonth is the published price, formatted, without a currency
	// symbol — "1.50". The symbol is in the template because it is typography.
	PricePerEmployeeMonth string
	// FreeMonths is the founding offer's free period. The PRODUCT enforces this:
	// migration 00016's tappa_first_chargeable_month adds `interval '3 months'`
	// for plan='founding', and the panel's billing screen reads it.
	FreeMonths int
}

// NavLink is one in-page link in the landing page's own navigation bar.
type NavLink struct {
	Label string
	// Href is an in-page anchor — "#how" — and nothing else. The page's sections
	// carry their ids in marketingSection's first argument, and
	// TestLandingNav_EveryLinkPointsAtASectionOnThePage follows each one against the
	// rendered page, so a section renamed without this list going with it is a red
	// test rather than a link that scrolls nowhere.
	Href string
}

// LandingNav is the sticky bar's three in-page links (2026-09-12, the user's own
// design carried across). The two OTHER things in the bar — "Sign in" and "Start
// free" — are not here because they are not in-page: they come from
// LandingView.SignInHref and LandingView.SignupHref for the reason those fields
// record, and the header renders them from the view.
var LandingNav = []NavLink{
	{Label: "How it works", Href: "#how"},
	{Label: "Security", Href: "#proof"},
	{Label: "Pricing", Href: "#pricing"},
}

// Step is one of the three things that happen before a shift is being recorded.
type Step struct {
	// Ordinal is printed as the step number. A string because it is set in the
	// data typeface beside a heading, not counted.
	Ordinal string
	Title   string
	Body    string
	// Facts are the product facts this step rests on — see Fact. Never empty.
	// Added 2026-09-12; until then Step carried no pin and
	// marketing_claims_test.go said so out loud ("RENDERING ONLY, AND THE GAP IS
	// STATED"). TestLandingSteps_EveryStepRestsOnAProductFact closes it.
	Facts []Fact
}

// LandingSteps is handoff §9's "3 adım".
//
// EVERY LINE IS A DESCRIPTION OF SOMETHING MOUNTED. Step 1 is the plaque
// (internal/sun reads what it writes), step 2 is the activation flow
// (handler.Activation, GET /activate), step 3 is the tap flow (handler.Tap,
// GET /t and POST /api/checkin) with the direction rule from CLAUDE.md §5.
//
// ⚠️ STEP 3's "SIZED FOR A WET OR FLOURY HAND" IS THE USER'S OWN PHRASE (2026-09-12)
// AND IT IS A DESIGN RULE, NOT A MEASUREMENT: skill tappa-brand fixes the tap
// button at a 64px minimum so that a gloved or wet finger can press it, and
// input.css's .tap-button rule carries that floor. Nobody has timed anybody's
// hands; the sentence says what the button is sized FOR, which is what the rule
// says, and FactTapButtonSizedForAWetHand reads the rule. "Under two seconds"
// from the same draft was NOT carried, because it is a number nobody measured.
var LandingSteps = []Step{
	{
		Ordinal: "01",
		Title:   "A plaque goes up by the door",
		Body: "It is a printed plaque with a passive chip behind it. No power, no network, " +
			"no battery to change. One at each entrance you want to record.",
		Facts: []Fact{FactPlaqueIsPassive},
	},
	{
		Ordinal: "02",
		Title:   "Each person opens their link once",
		Body: "You invite them from the dashboard. They open the link on their own phone, " +
			"read what is recorded and why, and agree. That is the only setup they ever do.",
		Facts: []Fact{FactPeopleAreInvitedFromTheDashboard},
	},
	{
		Ordinal: "03",
		Title:   "After that it is one tap",
		Body: "Hold the phone to the plaque, the browser opens, one button — sized for a wet " +
			"or floury hand. Taptime knows whether this is a way in or a way out from the " +
			"last entry that is still open — so a shift that ends at 02:00 closes the one " +
			"that started at 18:00.",
		Facts: []Fact{FactTapPageNeedsNoApp, FactTapButtonSizedForAWetHand, FactDirectionTogglesOnLastOpenEntry},
	},
}

// Line is one sentence of copy paired with the product facts it rests on. It is
// what a new marketing sentence has to be, as of 2026-09-12: the text and its
// answer to "which code provides this?" in one value, rendered from here rather
// than typed into the template.
type Line struct {
	Text  string
	Facts []Fact
}

// LandingSetupLede sits under the "how it works" heading. The user's draft read
// "There is nothing to install, enroll, or maintain. The plaque is passive — no
// battery, no software, no internet of its own." — "enroll" was dropped, because
// opening the invitation link once IS an enrolment of a kind and the sentence two
// lines below it says so; the rest is carried, and each half is pinned.
var LandingSetupLede = Line{
	Text: "Nothing to install, and nothing on the wall to maintain: the plaque is passive — " +
		"no battery, no software, no internet of its own.",
	Facts: []Fact{FactTapPageNeedsNoApp, FactPlaqueIsPassive},
}

// LandingPricingHeading is the user's own line, carried word for word because both
// halves are true and checkable: the price is per employee (migration 00016's
// price_per_employee_month, which TestLanding_PriceMatchesTheSchemaItIsCharged
// pins to the number printed), and there is no device to charge for because the
// plaque is a passive chip.
var LandingPricingHeading = Line{
	Text:  "Pay per person. Nothing per device — there are none.",
	Facts: []Fact{FactPricePerEmployee, FactPlaqueIsPassive},
}

// LandingPricingIncludes is the price card's list, and EVERY LINE IS SOMETHING THE
// PRODUCT DOES TODAY. The user's draft listed five; three were not carried, and
// the reasons are the reasons this file is written under:
//
//   - "Branded wall plaques included & replaced free" — the pricing card's prose
//     already says plaques are included (handoff §11, unchanged since M7-01), and
//     repeating it as a checklist item beside features the dashboard serves today
//     would present a plaque nobody can yet order (M8-05 B3 is blocked on hardware)
//     as a delivered feature. Said once, in the prose, as the pricing term it is.
//   - "Daily reports, CSV export & API for your payroll flow" — the CSV half is
//     real and is line three below; the API half is NOT. The only /api routes are
//     the tap form's own POSTs (FactNoIntegrationAPI is the tripwire).
//   - "Runs alongside your current system during trial" — not a product feature at
//     all, and "Unlimited locations & departments" was narrowed too: nothing caps
//     how many a tenant creates, but the panel's lists are capped at
//     venuePageLimit, so "unlimited" would be a word the screen contradicts.
var LandingPricingIncludes = []Line{
	{
		Text: "Venues and departments, each on its own hours — and each venue with its own " +
			"network address.",
		Facts: []Fact{FactVenuesAndDepartmentsCarryOwnHours},
	},
	{
		Text:  "The dashboard, with a queue of FLAGGED records for a manager to decide.",
		Facts: []Fact{FactFlaggedQueueDecidedByAManager},
	},
	{
		Text:  "Hours and the monthly headcount as CSV files, downloaded from the dashboard.",
		Facts: []Fact{FactHoursExportAsCSV, FactBillingExportAsCSV},
	},
	{
		Text: "Manual entries for a day the phone stayed at home — with the manager's name " +
			"on them, and marked as typed rather than tapped.",
		Facts: []Fact{FactManualEntryNamesAManager},
	},
}

// --- the two blocks the 2026-09-07 redesign added ----------------------------
//
// 🔴 BOTH ARE MECHANISM, AND BOTH WERE WRITTEN UNDER THE RULE AT THE TOP OF THIS
// FILE. The page this replaces was measured as honest and unillustrated: it carried
// no drawing of any kind and eight sections of the same bordered box, so it read as
// a well-written DOCUMENT rather than as a piece of engineering. The remedy is not
// decoration — it is that the two things this product actually does, PROVING a tap
// and JUDGING one, are now shown rather than summarised, and every line of both is
// a description of code in this repository.
//
// WHY THEY ARE DATA HERE RATHER THAN MARKUP IN THE TEMPLATE: it is where the
// answers to "which code provides this?" are recorded, which is what the header of
// this file demands of a marketing sentence.

// Source is a product fact one MECHANISM line rests on — the counterpart of Anchor
// for the two blocks the 2026-09-07 rebuild added.
//
// 🔴 IT IS A SEPARATE TYPE FROM Anchor ON PURPOSE, and the reason is a test rather
// than taxonomy. TestLandingAudiences_EveryClaimRestsOnAProductAnchor requires every
// declared Anchor to be claimed by a LandingAudiences sentence — an anchor nothing
// claims is a pin guarding nothing — so pinning these lines with Anchor constants
// would have turned that test red for a reason that is not a defect. Two vocabularies,
// two derivation maps, the same discipline.
//
// 🔴 AND THIS BLOCK EXISTS BECAUSE ITS ABSENCE SHIPPED A FALSE SENTENCE IN THE SAME
// ROUND THAT WROTE THIS FILE. LandingRules line three read "it is the one path in the
// product that writes no record"; measured, at least four paths write none —
// sys:tenant-mismatch redirects and lands in NEITHER organisation
// (TestCheckinDB_ForeignTenantTapIsRefusedAndWritesNOTHING), and the tap surface has
// three more refusals that tell the employee nothing was recorded. Nothing was red,
// because nothing read these two slices at all: of the seven claim-carrying values in
// this file only LandingComparison and LandingAudiences were pinned. The mechanism
// below is a RATCHET AGAINST DRIFT and not a proof of truth — the same limit written
// on Anchor applies word for word — but a line whose sid disappears now fails, and a
// line with no source does not compile past the test.
type Source string

const (
	// The five guardrails §5's rows 1-5 name, in the order internal/policy runs
	// them. Derived from policy.Guardrails(policy.DefaultParams()): the sid must be
	// present, carry the effect the row's stamp claims, and sit in this order.
	SourceTagNotActive        Source = "policy:sys:tag-not-active"
	SourceSUNInvalid          Source = "policy:sys:sun-invalid"
	SourceNoSession           Source = "policy:sys:no-session"
	SourceEmployeeDeactivated Source = "policy:sys:employee-deactivated"
	SourcePersonDebounce      Source = "policy:sys:person-debounce"
	// The baseline statements §5's rows 6-7 name. Derived from policy.Baseline()'s
	// documents rather than from a source scan, so a baseline that changed an effect
	// would fail even with the sid still present.
	SourceIPOrGPSOK        Source = "policy:base:ip-or-gps-ok"
	SourceGPSOnlyAllow     Source = "policy:base:gps-only-allow"
	SourceQRRequiresIP     Source = "policy:base:qr-requires-ip"
	SourceNoEvidenceReview Source = "policy:base:no-evidence-review"
	// The chip's URL carries a counter AND a signature. Derived by reflection over
	// sun.Params: the two fields the chip rewrites on every read.
	SourceSUNURLCarriesCounterAndSignature Source = "type:sun.Params.Ctr+CMAC"
	// The tap page is a web page at a route, which is the whole of "nothing to
	// install". Derived from handler.TapPath being registered with a GET.
	SourceTapIsAWebPage Source = "route:GET handler.TapPath"
	// The signature is checked in the server. Derived from sun.Verifier carrying a
	// Verify method — the one place CLAUDE.md §3 allows the cryptography to live.
	SourceSignatureIsVerified Source = "method:sun.Verifier.Verify"
	// The counter moves forward in ONE statement that refuses to move it backwards
	// (§4.4). Derived from db/queries/tags.sql's AdvanceTagCounter carrying the
	// strict `< @ctr` guard, which is the line that makes the second copy of a link
	// lose.
	SourceCounterAdvanceIsGuarded Source = "query:AdvanceTagCounter"
	// A phone whose session belongs to ANOTHER organisation is refused rather than
	// invited to activate (§4.5). Derived from the outcome switch in
	// internal/handler/checkin.go: the foreign-tenant arm answers with a forbidden
	// problem page and a fixed set of words, and it is the ACTIVATION arm — the one
	// line three is otherwise about — that redirects.
	//
	// 🔴 IT EXISTS BECAUSE THIS IS THE HALF OF LINE THREE NOTHING WAS HOLDING. The
	// line named sys:no-session, that sid was present, and the sentence beside it
	// described a SECOND path the sid says nothing about — so the page could claim
	// the two are handled alike while the handler pulled them apart, and did.
	//
	// ⚠️ ITS LIMIT IS THE ONE WRITTEN AT THE TOP OF THIS BLOCK, and one more besides:
	// it reads handler SOURCE rather than running the handler, so it fails when the
	// arm is rewritten and not when the arm is reached by something else.
	SourceForeignTenantIsRefusedNotActivated Source = "handler:checkin.OutcomeForeignTenant"
)

// Fact is a product fact one of the 2026-09-12 additions rests on: the FAQ, the
// price card's list, the three steps and the two lede lines carried over from the
// user's own design.
//
// 🔴 IT IS A THIRD VOCABULARY, AND THE REASON IS THE SAME ONE Source GIVES FOR
// BEING SEPARATE FROM Anchor — a test, not taxonomy. Both older vocabularies are
// CLOSED by tests that this task may not edit: every declared Anchor must be
// claimed by LandingAudiences and derived in anchorDerivations, every declared
// Source by LandingRules or LandingTapFlow and sourceDerivations. A new constant in
// either block turns one of those tests red for a reason that is not a defect. So
// the new claims get their own block, their own derivation map
// (internal/handler/marketing_facts_test.go) and their own closure test — and
// where a fact IS one the older vocabularies already derive, the derivation
// delegates to theirs rather than re-reading the product a second way.
//
// ⚠️ THE LIMIT IS THE ONE WRITTEN ON Anchor, WORD FOR WORD: naming a Fact does
// not make a sentence true, and the test is blind to a sentence bolted onto an
// unrelated but healthy Fact. Every sentence below was still read by a person
// against the product. Three of the facts are TRIPWIRES for an ABSENCE — no bulk
// import, no integration API, no edit of a record — because the user's draft
// promised all three and the honest sentence is the one that says they are not
// there; a tripwire fails the day one appears, which is the day that sentence
// has to be rewritten.
type Fact string

const (
	// The tap page is a web page at a route, which is the whole of "nothing to
	// install". Delegates to SourceTapIsAWebPage.
	FactTapPageNeedsNoApp Fact = "fact:tap-is-a-web-page"
	// The plaque is a passive NTAG 424 DNA chip that rewrites its own URL (skill
	// tappa-sun; internal/sun.Parse reads the ctr and cmac it writes). Delegates to
	// SourceSUNURLCarriesCounterAndSignature. ITS LIMIT, STATED: no Go code can
	// prove a chip has no battery; what the derivation holds is that the product
	// reads a counter and a signature the plaque itself wrote, which is the
	// mechanism a passive NFC tag provides and a powered reader does not.
	FactPlaqueIsPassive Fact = "fact:plaque-writes-its-own-url"
	// The tap button carries a 64px minimum (input.css .tap-button, min-h-16),
	// which skill tappa-brand fixes so a gloved or wet finger can press it. Derived
	// by reading the rule.
	FactTapButtonSizedForAWetHand Fact = "css:.tap-button min-h-16"
	// In or out is decided by toggling against the person's last OPEN check-in,
	// not the calendar day (CLAUDE.md §5, internal/domain/tap.resolveDirection over
	// Input.LastOpenIn). Derived by reflection over tap.Input and a scan for the
	// function.
	FactDirectionTogglesOnLastOpenEntry Fact = "tap:resolveDirection(Input.LastOpenIn)"
	// People are added one at a time from the dashboard (POST employeeAddHref in
	// internal/handler/dashboard.go) and open their invitation on their own phone
	// (GET /activate in internal/handler/activate.go). Derived from both
	// registrations.
	FactPeopleAreInvitedFromTheDashboard Fact = "route:POST employeeAddHref+GET /activate"
	// TRIPWIRE: there is no file import. Derived as an ABSENCE — no non-test Go
	// source under internal/ or cmd/ reads a multipart upload (FormFile,
	// MultipartReader, ParseMultipartForm). The FAQ says "there is no file import
	// today"; the day one exists this fails and the sentence is rewritten.
	FactNoBulkImport Fact = "absent:multipart-upload"
	// TRIPWIRE: there is no integration API. Derived as an ABSENCE — the only
	// "/api…" literals in non-test Go source are the tap form's own two POSTs
	// (/api/checkin, /api/activate). The user's draft promised "a clean API feed";
	// the FAQ says "There is no API" instead.
	FactNoIntegrationAPI Fact = "absent:/api beyond the tap form"
	// A typed record names the administrator who typed it and is marked apart from
	// a tapped one: manual.Entry.EnteredBy (required, from the signed panel
	// session), migration 0005's channel CHECK admitting 'manual' with entered_by
	// beside it, and components.DocketView.Manual, which the panel renders as
	// "Entered by a manager". Derived from all three.
	FactManualEntryNamesAManager Fact = "domain:manual.Entry.EnteredBy+DocketView.Manual"
	// The hours report is downloadable as CSV: GET reportsCSVHref is registered in
	// internal/handler/dashboard.go and reportscsv.go answers it as an attachment.
	FactHoursExportAsCSV Fact = "route:GET reportsCSVHref"
	// The monthly headcount is downloadable as CSV: GET billingCSVHref, same shape.
	FactBillingExportAsCSV Fact = "route:GET billingCSVHref"
	// A record with too little evidence is written, marked and queued for a person
	// (base:no-evidence-review carries EffectReview) and the panel takes the
	// decision (POST reviewHref → reviewDecision). Delegates to
	// SourceNoEvidenceReview for the first half and reads the registration for the
	// second.
	FactFlaggedQueueDecidedByAManager Fact = "route:POST reviewHref+policy:base:no-evidence-review"
	// A copied link loses: the counter is advanced under a strict `<` and a code
	// whose signature or counter does not check out is refused. Delegates to
	// SourceCounterAdvanceIsGuarded and SourceSUNInvalid.
	FactCopiedLinkIsRefused Fact = "fact:replayed-link-is-refused"
	// A check-in that arrives without the plaque's one-time code needs the venue's
	// network address; a position alone is not enough for it (§5's QR sentence,
	// base:qr-requires-ip carries EffectReview). Delegates to SourceQRRequiresIP.
	//
	// 🔴 THIS IS THE FACT THE USER'S FAQ GOT WRONG — "requires the IP OR GPS proof
	// instead" — and it is a decision-engine claim of the exact class that produced
	// three REDs on this page. The address is REQUIRED for the codeless channel;
	// GPS on its own ends in FLAGGED. Both FAQ answers that touch it say so.
	FactCodelessTapNeedsAddress Fact = "fact:codeless-tap-needs-the-address"
	// A venue carries its own hours and its own addresses, a department its own
	// hours. Delegates to AnchorVenueShiftAndAddress and AnchorDepartmentShift.
	FactVenuesAndDepartmentsCarryOwnHours Fact = "fact:venue-and-department-hours"
	// The price is per employee per month: migration 00016's
	// tenants.price_per_employee_month. Derived from the migration.
	FactPricePerEmployee Fact = "schema:tenants.price_per_employee_month"
	// An open entry is listed, not closed for you: ledger.Report carries Open
	// []OpenEntry and the panel mounts an anomalies section over it. Derived by
	// reflection and from the section table.
	FactOpenEntriesAreListedNotClosed Fact = "type:ledger.Report.Open+section:anomalies"
	// TRIPWIRE: a recorded tap is never changed and never deleted. Derived from
	// migration 0005's GRANT on transactions naming SELECT and INSERT and nothing
	// else for the application role (§4.3) — an UPDATE or DELETE grant appearing
	// there fails this.
	FactRecordsAreAppendOnly Fact = "grant:transactions SELECT,INSERT only"
	// Every table is isolated inside the database: each CREATE TABLE in
	// db/migrations has a matching FORCE ROW LEVEL SECURITY, and internal/db's
	// readRole refuses a role that could bypass it (§4.5). Derived from the
	// migrations and the pool source.
	FactEveryTableIsIsolated Fact = "schema:every-table-forces-rls"
	// The report totals per person and per venue. Delegates to
	// AnchorPerVenueReport.
	FactReportPerPersonAndVenue Fact = "fact:report-per-person-and-venue"
)

// Stage is one step in the life of a single tap, from the chip to a written record.
type Stage struct {
	// Ordinal is printed as the step number, in the data typeface.
	Ordinal string
	Title   string
	Body    string
	// Sources are the product facts this stage rests on. A slice, and never empty:
	// a stage with two halves rests on two facts, and pinning only the first half is
	// how half a claim goes unchecked. The test refuses a Stage with none.
	Sources []Source
}

// LandingTapFlow is what happens between a phone touching the plaque and a record
// existing. It is internal/sun's contract, written for somebody who has not read it.
//
// EVERY SENTENCE HAS A SOURCE, IN ORDER:
//   - 01: the NTAG 424 DNA chip rewrites its own URL on every read (skill tappa-sun,
//     internal/sun.Parse reads the ctr and cmac it writes).
//   - 02: layout.PageWithScript renders the tap page; there is no application to
//     install and the phone creates no account of its own.
//   - 03: internal/sun verifies the AES-CMAC, and CLAUDE.md §4.7 is where the key
//     never goes — not the plaque, not the phone, not a log.
//   - 04: CLAUDE.md §4.4's single statement, whose WHERE clause is the mechanism:
//     `UPDATE tags SET last_ctr = $2 WHERE uid = $1 AND last_ctr < $2`. Zero rows
//     affected means the counter did not move forward, which is what makes the
//     second copy of one link lose. (KEEP THAT STATEMENT ON ONE LINE — R4 in
//     scripts/redline-check.sh is line-local, and a wrapped one reads as an
//     unconditional counter write.)
//
// ⚠️ NO COUNTER VALUE AND NO PLAQUE ID IS PRINTED ANYWHERE ON THIS PAGE, and the
// drawing beside this block says `n` and `n+1` for exactly the reason
// landingSampleDocket gives about the record card: an invented identifier printed in
// the shape of a real one is the one thing a public page must not do.
var LandingTapFlow = []Stage{
	{
		Ordinal: "01",
		Title:   "The chip writes a new code",
		Body: "Every time a phone reads the plaque, the chip rewrites its own link: a counter one " +
			"higher than the last read, and a signature over it. It holds nothing about your team " +
			"and it needs no power to do this.",
		Sources: []Source{SourceSUNURLCarriesCounterAndSignature},
	},
	{
		Ordinal: "02",
		Title:   "The phone opens a page",
		Body: "That link opens in the browser the phone already has. Nothing is installed, no " +
			"account is created on the phone, and there is nothing to keep up to date.",
		Sources: []Source{SourceTapIsAWebPage},
	},
	{
		Ordinal: "03",
		Title:   "Taptime checks the signature",
		Body: "The key that signs the code is never printed on the plaque, never sent to the phone " +
			"and never written to a log. A code whose signature does not check out is refused.",
		Sources: []Source{SourceSignatureIsVerified},
	},
	{
		Ordinal: "04",
		Title:   "And refuses a repeat",
		Body: "The counter is moved forward by a single database statement that will not move it " +
			"backwards. If one link is opened twice, the second one changes nothing and is refused.",
		Sources: []Source{SourceCounterAdvanceIsGuarded},
	},
}

// Stamp is the verdict a decision line ends in — the closed vocabulary the rubber
// stamp is rendered from.
//
// IT IS A TYPE AND NOT A CLASS NAME, and that is a Tailwind fact rather than
// tidiness. The CLI scans .templ files as raw text and emits a component rule only
// where it sees the literal class, so a class assembled in Go ("stamp--" + x) would
// compile to a stamp with no frame and no ground. The template switches on these
// values and writes the five class names out in full; see landing.templ.
type Stamp string

const (
	// StampNone is the one line that ends in no record at all.
	StampNone     Stamp = ""
	StampApproved Stamp = "approved"
	StampFlagged  Stamp = "flagged"
	StampRejected Stamp = "rejected"
	StampIgnored  Stamp = "ignored"
)

// Rule is one line of the decision order: when it matches, and what the answer is.
type Rule struct {
	// Ordinal is the line's position, printed in the data typeface. The ORDER is the
	// substance of this table — the first line that matches wins — so the number is
	// part of the claim rather than decoration.
	Ordinal string
	// When is the condition, written as a person would say it.
	When string
	// Verdict is the stamp this line ends in, or StampNone for the one line that
	// records nothing.
	Verdict Stamp
	// Body is what it means, and it is where the line's limit is stated.
	Body string
	// Sources are the engine facts this line rests on — see Source. Never empty.
	Sources []Source
}

// LandingRules is CLAUDE.md §5's decision order, which is also internal/policy's:
// rows 1-5 are guardrails in internal/policy/guardrails.go (sys:tag-not-active,
// sys:sun-invalid, sys:no-session, sys:employee-deactivated, sys:person-debounce, in
// that order), rows 6-7 are the baseline (base:ip-or-gps-ok / base:gps-only-allow,
// then base:no-evidence-review). internal/domain/tap.Decide makes exactly one call
// into the engine and applies what comes back.
//
// 🔴 IT IS A SELECTION AND THE PAGE SAYS SO. The tap guardrails number more than
// seven — a session tapping another organisation's plaque, a page left open too
// long, a declared time outside the tolerance — and a table that showed seven and
// implied "these are all of them" would be the shape of claim this file exists to
// refuse. The note under the table names two of the ones not shown.
//
// ⚠️ ROW 4 SAYS WHAT IS ACTUALLY DONE, WHICH IS NOT WHAT §5's WORDING SUGGESTS. §5
// calls it a "güvenlik uyarısı" and the first draft here read "a manager is told" —
// that is a notification, and this product has none. What exists (M8-03,
// internal/domain/checkin) is the refused record, a row in the audit trail and a
// warning in the server's log, so that is what the line claims.
var LandingRules = []Rule{
	{
		Ordinal: "1",
		When:    "The plaque is retired, or reported lost",
		Verdict: StampRejected,
		Body:    "A plaque that is out of service decides nothing, whoever is holding the phone.",
		Sources: []Source{SourceTagNotActive},
	},
	{
		Ordinal: "2",
		When:    "The one-time code does not check out, or has been seen before",
		Verdict: StampRejected,
		Body:    "A link somebody copied carries a counter Taptime has already moved past.",
		Sources: []Source{SourceSUNInvalid},
	},
	{
		Ordinal: "3",
		// 🔴 "NEVER BEEN SET UP" WAS TOO NARROW FOR WHAT FIRES THIS LINE. The guardrail
		// is reached from httpx.SessionAbsent AND httpx.SessionRevoked (handler/tap.go's
		// SessionAbsent, SessionRevoked arm), and the second one is a phone that WAS set
		// up and has since been signed out — a stolen handset, a second device. A line
		// that named only the first left the commoner half of its own trigger undescribed.
		When:    "This phone has not been set up, or has been signed out",
		Verdict: StampNone,
		// 🔴 THE SECOND SENTENCE USED TO SAY THE FOREIGN-ORGANISATION TAP IS "TURNED AWAY
		// THE SAME WAY", AND IT IS NOT. Measured: sys:no-session ends in a 303 to the
		// activation page (handler/tap.go's SessionAbsent, SessionRevoked arm), while
		// sys:tenant-mismatch lets the page open, is decided when the button is pressed
		// and answers 403 with a fixed refusal — handler/checkin.go's
		// checkin.OutcomeForeignTenant arm, whose own comment says it is NOT the
		// activation page "because their session is perfectly good". Since this line's
		// premise was "the person is shown their invitation", "the same way" told the
		// reader that a stranger's phone is offered an invitation to activate, which is
		// the one thing §4.5 refuses. What the two paths genuinely share is the half that
		// was already true and is worth keeping: neither writes a record
		// (TestCheckinDB_ForeignTenantTapIsRefusedAndWritesNOTHING).
		//
		// 🔴 AND THE FIRST SENTENCE WAS WRONG IN ITS OWN RIGHT, WHICH ONLY SHOWED UP
		// WHILE CHECKING THE SECOND. "Shown their invitation" describes a page this
		// product does not serve: handler/tap.go's redirectToActivation sends the phone
		// to /activate with NO code, and handler/activate.go answers that with
		// problemNoLink — "You need your activation link ... This page opens from the
		// personal link your workplace sent you". The page ASKS FOR the invitation; it
		// does not show one. A sentence that promised the opposite would have read as a
		// small kindness the product does not perform.
		Body: "The person is sent to the activation page, which asks for the personal link " +
			"their workplace sent them. Nothing is recorded — not because the evidence is " +
			"thin, which is line seven's business, but because there is nobody to write a " +
			"record against. A phone signed in to another organisation is not sent there: " +
			"that session is real, so the tap is refused at the button instead, in words " +
			"that name neither employer. It lands in neither organisation's records either.",
		Sources: []Source{SourceNoSession, SourceForeignTenantIsRefusedNotActivated},
	},
	{
		Ordinal: "4",
		When:    "The person's account has been switched off",
		Verdict: StampRejected,
		Body: "Refused, and still written down: the record, an entry in the audit trail, and a " +
			"warning in the server's log.",
		Sources: []Source{SourceEmployeeDeactivated},
	},
	{
		Ordinal: "5",
		When:    "The same person tapped a moment ago",
		Verdict: StampIgnored,
		// "IN THE LUNCH RUSH" IS THE USER'S PHRASE (2026-09-12) AND THIS IS THE ONE LINE
		// IT IS TRUE OF: the window is keyed to the PERSON, so a queue of different
		// people at one door is not debounced into one record. It is not a claim about
		// speed, which is what the same phrase meant in the draft's setup heading and
		// why it was not carried there.
		Body: "Their earlier tap stands. The window is per person and not per plaque, so a queue " +
			"at one door in the lunch rush still records everybody in it.",
		Sources: []Source{SourcePersonDebounce},
	},
	{
		Ordinal: "6",
		When:    "The venue's network address matches, or the phone is close enough",
		Verdict: StampApproved,
		Body: "Either half is enough on its own. A check-in that arrives without the plaque's " +
			"one-time code is no proof of a touch, so for that one the address is required.",
		Sources: []Source{SourceIPOrGPSOK, SourceGPSOnlyAllow, SourceQRRequiresIP},
	},
	{
		Ordinal: "7",
		When:    "Neither of them",
		Verdict: StampFlagged,
		Body: "The record is written, marked, and put in a manager's queue. No tap is dropped " +
			"for want of proof and nothing is approved silently.",
		Sources: []Source{SourceNoEvidenceReview},
	},
}

// Evidence is one of the four things Tappa weighs when it decides a record.
type Evidence struct {
	// Proof is the user's own label for the piece (2026-09-12): "Proof of moment",
	// "Proof of person", "Proof of place" and — for the position — "Backup proof of
	// place", which is CLAUDE.md §5's own word for it ("yedek nerede"). Printed as
	// the card's eyebrow; Answers stays as the sentence under the name.
	Proof string
	// Label is the short name, printed in the data typeface.
	Label string
	// Answers is the question this piece of evidence answers — the four-word
	// summary CLAUDE.md §5 opens with.
	Answers string
	Body    string
}

// LandingEvidence is CLAUDE.md §5's "Dört kanıt", written for somebody who has not
// read it.
//
// 🔴 THE FOURTH ENTRY IS THE ONE THAT HAD TO BE WRITTEN CAREFULLY. §4.2 permits
// reading a position AT THE MOMENT OF A TAP and forbids every other shape of it,
// and the honest sentence is neither "we never know where you are" (false — the
// coordinate is read, and migration 0005 stores it) nor a shrug. What is true and
// checkable: it is read once, on a press; it is never read in the background; and
// no screen or export in this product renders it — internal/domain/ledger's two
// view types have no Latitude and no Longitude field, and neither does
// components.DocketView.
var LandingEvidence = []Evidence{
	{
		Proof:   "Proof of moment",
		Label:   "The plaque's one-time code",
		Answers: "a real touch, just now",
		// ⚠️ "a key that never leaves our database" WAS THE FIRST DRAFT AND IT IS NOT
		// TRUE: the key is stored wrapped and is unwrapped IN THE SERVER to check the
		// signature, so it leaves the database every time somebody taps. What is true
		// and checkable is where it never goes — internal/sun and CLAUDE.md §4.7.
		Body: "The chip writes a fresh single-use code every time a phone reads it, signed " +
			"with a key that is never printed on the plaque, never sent to the phone and " +
			"never written to a log. Taptime checks the signature and refuses any code it " +
			"has already seen, so a copied link is worth nothing.",
	},
	{
		Proof:   "Proof of person",
		Label:   "The sign-in on the phone",
		Answers: "who tapped",
		Body: "Set once, when your colleague opened their link. Taptime stores a hash of it " +
			"rather than the value, so the thing in their browser cannot be read back " +
			"out of our database.",
	},
	{
		Proof:   "Proof of place",
		Label:   "Your venue's network address",
		Answers: "where it happened",
		Body: "If you tell Taptime the fixed address a venue's internet connection uses, a tap " +
			"arriving from it is a tap that happened there.",
	},
	{
		Proof:   "Backup proof of place",
		Label:   "The phone's position, at that moment",
		Answers: "where it happened, without a fixed address",
		// ⚠️ "shows the position on no screen" HAD TO BE NARROWED. The panel DOES show
		// coordinates — a VENUE's own, typed in by a manager (internal/handler's
		// locations screen). What no screen and no export carries is where a PERSON
		// was: internal/domain/ledger's two view types and components.DocketView have
		// no Latitude and no Longitude field, and the queries do not select them.
		Body: "If there is no address to match, the browser may offer the phone's position as " +
			"the button is pressed. Once, on a press. Taptime does not watch a location in " +
			"the background and does not draw a boundary to be alerted about, and no " +
			"screen or export in Taptime shows where a person was — only whether they were " +
			"close enough.",
	},
}

// ComparisonRow is one line of the comparison table: a question, and what each
// approach answers.
type ComparisonRow struct {
	Aspect   string
	Terminal string
	Tappa    string
}

// LandingComparison is handoff §9's comparison table.
//
// 🔴 IT COMPARES MECHANISMS AND DECLARES NO WINNER, and that restriction is the
// whole design of this table rather than modesty. Tappa's half is checkable in this
// repository. The other half is NOT — nobody here has benchmarked a device — so
// every cell in the Terminal column is restricted to what the approach is BY
// DEFINITION: a reader that compares fingers must read one and hold something to
// compare it against, and an electronic reader on a wall must be powered. Anything
// beyond that (accuracy, failure rates, hygiene, cost of ownership) is a
// measurement, and the page carries none.
//
// The footnote saying so is rendered, not just written here:
// TestLanding_ComparesMechanismsAndDeclaresNoWinner.
var LandingComparison = []ComparisonRow{
	{
		Aspect:   "What it reads",
		Terminal: "a finger",
		Tappa:    "a one-time code the plaque generates",
	},
	{
		Aspect:   "What is kept about the body",
		Terminal: "a template of the finger, to compare against",
		// Worded like pages.Activate's and the footer's, for the reason recorded on
		// marketingChrome: the same promise in the same words everywhere, and it stays
		// inside the red-line scanner's ORIGINAL exemption because the denial it already
		// recognises appears on THIS line. Keep it on one line.
		Tappa: "nothing — no fingerprints, no face, no voice, no biometric data of any kind",
	},
	{
		Aspect:   "At the door",
		Terminal: "a powered device on the wall",
		Tappa:    "a printed plaque with no power and no network",
	},
	{
		Aspect:   "On the phone",
		Terminal: "not involved",
		Tappa:    "no app to install — the browser opens the page",
	},
	{
		Aspect:   "Where a record can be corrected",
		Terminal: "depends on the device",
		Tappa:    "a manager types a new record; the original is never edited or deleted",
	},
}

// Question is one FAQ entry.
type Question struct {
	Q string
	A string
	// Facts are the product facts the answer rests on — see Fact. Never empty.
	// Added 2026-09-12, when the FAQ grew four questions from the user's own draft;
	// until then Question carried no pin and marketing_claims_test.go said so.
	Facts []Fact
}

// LandingFAQ is handoff §9's FAQ, plus four of the user's questions (2026-09-12).
//
// EVERY ANSWER NAMES A BEHAVIOUR THAT IS IMPLEMENTED, and since 2026-09-12 every
// answer also NAMES ITS FACT. In order: the tap page loads no application; the
// QR-shaped channel exists in internal/sun.Parse and is governed by the
// base:qr-requires-ip policy; Q18 decided the system produces no checkout of its
// own, so handler + internal/domain/manual make a person type one; CLAUDE.md §4.3
// and migration 0005 make `transactions` append-only; §4.5 and every migration's
// row-level security isolate a tenant; §4.6 refuses to drop a record it cannot
// judge.
//
// 🔴 THE FOUR NEW ONES WERE THE MOST DANGEROUS COPY IN THE USER'S DRAFT, because
// every one of them answered with something the product does not do, and one of
// them mis-stated the decision engine:
//
//   - "in ten seconds" (the manual entry) — a number nobody measured; dropped.
//   - "Every plaque also carries a QR code … requires the IP OR GPS proof instead"
//     — plaques carry no printed code yet (M8-05), and the rule is WRONG: the
//     codeless channel REQUIRES the address, and a position alone ends in FLAGGED
//     (§5, base:qr-requires-ip). The existing "cannot read the plaque" answer
//     already says it right and is kept; the "from home" answer says it again.
//   - "IP + GPS have to agree" — wrong the other way: §5's line six is a
//     disjunction, either half is enough. The "from home" answer does not say
//     "agree".
//   - "Import your staff list from a CSV … run both systems side by side … Most
//     sites switch in one pay period" — there is no import (FactNoBulkImport is
//     the tripwire), and the rest is a process claim nobody measured. The answer
//     says what is there: one person at a time, from the dashboard.
//   - "a clean API feed your payroll or HR system directly" — there is no API
//     (FactNoIntegrationAPI is the tripwire). The answer says so. The sentence
//     "Taptime is the source of truth for in and out times — it does not try to
//     replace your payroll" is the user's and is kept, because it is true.
var LandingFAQ = []Question{
	{
		Q: "Does my team have to install anything?",
		A: "No. Holding the phone to the plaque opens a web page. There is no app, no " +
			"account to create on the phone and nothing to update.",
		Facts: []Fact{FactTapPageNeedsNoApp},
	},
	{
		Q: "What about phones that cannot read the plaque?",
		A: "Older phones cannot read a chip in the background. Taptime accepts a check-in that " +
			"arrives without the plaque's one-time code — but that check-in carries no proof " +
			"of a physical touch, so it needs your venue's network address to match. A " +
			"position on its own is not enough for it. Your organisation can change that rule.",
		Facts: []Fact{FactCodelessTapNeedsAddress},
	},
	{
		Q: "What if somebody does not have their phone that day?",
		// "vouched for" IS THE USER'S WORD AND IT IS THE RIGHT ONE: a typed record
		// carries no evidence of a touch, only a named manager standing behind it —
		// which is exactly what manual.Entry.EnteredBy being REQUIRED means.
		A: "A manager types the entry from the dashboard. It is stored with that manager's " +
			"name on it and marked as entered by hand rather than tapped, so a record that " +
			"was vouched for never looks like one that was proved.",
		Facts: []Fact{FactManualEntryNamesAManager},
	},
	{
		Q: "What happens when somebody forgets to clock out?",
		A: "Taptime does not invent a clock-out. The entry stays open, it is listed as an " +
			"anomaly for a manager, and the hours are typed in by a person. Open entries are " +
			"left out of the totals and the report says so rather than quietly rounding.",
		Facts: []Fact{FactOpenEntriesAreListedNotClosed, FactManualEntryNamesAManager},
	},
	{
		Q: "Can somebody clock in from home?",
		// 🔴 READ THE THREE SENTENCES AGAINST §5 BEFORE TOUCHING THEM. A link with the
		// plaque's code only exists once a phone has read the plaque, and the second
		// opening of it loses (rows 2 and the counter guard). A check-in WITHOUT the
		// code is the codeless channel: it needs the venue's address, and from anywhere
		// else it is FLAGGED — recorded, queued, not approved. "Not without a manager
		// seeing it" is therefore the honest answer, and "No" would have been too
		// strong: a flagged record IS written.
		A: "Not without a manager seeing it. The plaque's chip signs every tap with a one-time " +
			"code, so such a link only exists once a phone has read the plaque — and a copied " +
			"link, or yesterday's, carries a counter Taptime has already moved past and is " +
			"refused. A check-in that arrives without the code at all needs your venue's " +
			"network address to be approved; from anywhere else it is written down, marked " +
			"FLAGGED and put in a manager's queue rather than approved.",
		Facts: []Fact{FactCopiedLinkIsRefused, FactCodelessTapNeedsAddress, FactFlaggedQueueDecidedByAManager},
	},
	{
		Q: "Can a record be edited?",
		A: "No. A recorded tap is never changed and never deleted. A correction is a new " +
			"record with the manager's name on it, and both stay — that is what makes the " +
			"record usable as evidence later.",
		Facts: []Fact{FactRecordsAreAppendOnly, FactManualEntryNamesAManager},
	},
	{
		Q: "What if Taptime cannot tell where a tap happened?",
		A: "It writes the record anyway, marks it FLAGGED and puts it in a manager's queue. " +
			"A record is never dropped for want of proof, and nothing is approved silently.",
		Facts: []Fact{FactFlaggedQueueDecidedByAManager},
	},
	{
		Q: "How do we move over from the device we have now?",
		A: "One person at a time, and by hand: you add each person from the dashboard and " +
			"send them their invitation link, which they open once on their own phone. There " +
			"is no file import today. The device you have now is yours to switch off whenever " +
			"you are ready — Taptime does not talk to it.",
		Facts: []Fact{FactPeopleAreInvitedFromTheDashboard, FactNoBulkImport, FactNoIntegrationAPI},
	},
	{
		Q: "Where does the data go?",
		A: "Into your dashboard: hours per person and per venue, open entries listed as " +
			"anomalies, and the monthly headcount you are billed on. Hours and headcount come " +
			"out as CSV files. There is no API. Taptime is the source of truth for in and out " +
			"times — it does not try to replace your payroll.",
		Facts: []Fact{FactReportPerPersonAndVenue, FactOpenEntriesAreListedNotClosed, FactHoursExportAsCSV, FactBillingExportAsCSV, FactNoIntegrationAPI},
	},
	{
		Q: "Can one organisation see another's records?",
		A: "No. Every table carries the organisation it belongs to, every table enforces that " +
			"inside the database rather than only in the application, and the application " +
			"connects with a role that cannot bypass it.",
		Facts: []Fact{FactEveryTableIsIsolated},
	},
}

// Anchor names a product capability a marketing sentence depends on.
//
// 🔴 IT EXISTS BECAUSE A SENTENCE ON THIS PAGE SHIPPED THAT THE PRODUCT DOES NOT
// DO. "Reports and the monthly headcount split by department" was rendered on /
// and was wrong in BOTH halves, measured: the reports section breaks down per
// person, per venue and by open entries and contains the word "department" zero
// times (ledger.Report has People and Venues and no department dimension; there is
// no Department field on PersonHours), and the whole billing surface — the domain,
// the handler, the CSV, the view and the template — contains it zero times too.
//
// 🔴 THE STRUCTURAL CAUSE WAS NOT THE SENTENCE, IT WAS THE ABSENCE OF A PIN. The
// price, the free months, the cookie list, the slogan and the comparison table were
// all held against something in the product; this block was held against nobody's
// memory. So the fix is not a corrected sentence — it is that a claim in this block
// CANNOT COMPILE without naming the capability it rests on, and
// internal/handler's TestLandingAudiences_EveryClaimRestsOnAProductAnchor DERIVES
// each one from the schema, the generated query types, the policy baseline or the
// domain source.
//
// ⚠️ WHY NOT A TEST THAT MATCHES THE TEXT. Because a hand-kept list of expected
// sentences is the thing that goes stale, in this repository repeatedly: it agrees
// with whatever is written next to it, including a lie. A derivation disagrees.
//
// 🔴 WHAT NAMING AN ANCHOR DOES **NOT** DO — READ THIS BEFORE ADDING A SENTENCE.
// It does not make the sentence true, and it does not check that the sentence is
// ABOUT the anchor it names. The mechanism catches a claim whose capability
// DISAPPEARS; it is structurally blind to a claim for a capability that was never
// there, and to a sentence bolted onto an unrelated anchor. An audit proved both at
// once by pairing a passing anchor with an invented sentence ("Tappa emails every
// manager a nightly summary and works with no internet at the door") and watching
// the package stay green. This is a RATCHET AGAINST DRIFT, not a proof of truth: a
// new sentence still has to be checked by a person against the product, which is
// exactly what did not happen to the one that shipped wrong.
type Anchor string

const (
	// AnchorPlaqueBelongsToVenue — every plaque is mounted at exactly one venue.
	// Derived from migration 00004: tags.location_id is NOT NULL.
	AnchorPlaqueBelongsToVenue Anchor = "schema:tags.location_id"
	// AnchorVenueShiftAndAddress — a venue carries its own hours and its own
	// allowed addresses. Derived from migration 00002's locations table:
	// shift_start/shift_end and static_ips cidr[].
	AnchorVenueShiftAndAddress Anchor = "schema:locations.shift+static_ips"
	// AnchorCrossVenueNotPenalised — tapping away from your home venue is normal
	// and is never held against you. Derived from the policy baseline itself: the
	// statement with sid base:cross-location-note carries EffectAllow.
	AnchorCrossVenueNotPenalised Anchor = "policy:base:cross-location-note=allow"
	// AnchorPerVenueReport — the report totals each venue separately. Derived by
	// reflection over ledger.Report, which carries Venues []VenueHours.
	AnchorPerVenueReport Anchor = "type:ledger.Report.Venues"
	// AnchorDepartmentShift — a department carries its own shift, overnight
	// included. Derived from migration 00002's departments table.
	AnchorDepartmentShift Anchor = "schema:departments.shift+overnight"
	// AnchorLatenessFollowsDepartment — the shift a person is judged against is
	// their department's when they have one. Derived from the generated query row
	// (it selects BOTH the department's and the location's shift) plus the order
	// the two are tested in inside ledger's resolveShift.
	AnchorLatenessFollowsDepartment Anchor = "ledger:department-shift-wins"
	// AnchorDepartmentOnEveryRecord — the department travels on the record and is
	// a filter on the transactions list. Derived from migration 00005
	// (transactions.department_id) and components.FilterBarView.DepartmentID.
	AnchorDepartmentOnEveryRecord Anchor = "schema:transactions.department_id+filter"
)

// Claim is one sentence paired with the capability it asserts.
//
// Anchors is a SLICE because a sentence with two halves rests on two capabilities,
// and pinning only the first half is how half a claim goes unchecked. The test
// refuses a Claim with none.
type Claim struct {
	Text    string
	Anchors []Anchor
}

// Audience is one of the two shapes a customer comes in.
type Audience struct {
	Title string
	Lede  string
	// Points are what changes for this shape. Each is a behaviour, not a benefit,
	// and each names the product capability it rests on.
	Points []Claim
}

// LandingAudiences is handoff §9's "chain vs facility" — the two shapes the data
// model was built around and the two design partners it was built with.
//
// BOTH COLUMNS DESCRIBE THE SAME SCHEMA. A chain is several `locations`, a facility
// is one location and several `departments`, and the difference a person actually
// feels is which shift their lateness is measured against: CLAUDE.md §5 takes the
// department's shift when there is one and otherwise the shift of the location that
// was TAPPED — not the one on their profile, because moving between branches is
// normal and being marked late for it would be wrong.
var LandingAudiences = []Audience{
	{
		Title: "Several venues",
		Lede:  "A chain: each door is its own place, and people move between them.",
		Points: []Claim{
			{
				Text:    "One plaque per entrance, each tied to the venue it is mounted at.",
				Anchors: []Anchor{AnchorPlaqueBelongsToVenue},
			},
			{
				Text:    "Each venue keeps its own opening hours and its own network address.",
				Anchors: []Anchor{AnchorVenueShiftAndAddress},
			},
			{
				// ⚠️ THIS SENTENCE USED TO SAY "recorded as that branch's, NOTED, and
				// shown separately in the report". The note half was dropped rather
				// than pinned: base:cross-location-note's Reason only reaches a
				// record's note when that statement is the one that MATCHED, and a
				// tap with an address match is decided by a different statement. The
				// two halves that are unconditionally true are kept and pinned.
				Text: "A tap at another branch is judged on that branch's evidence and hours and is " +
					"never counted against the person for being away — and the report totals " +
					"each venue on its own.",
				Anchors: []Anchor{AnchorCrossVenueNotPenalised, AnchorPerVenueReport},
			},
		},
	},
	{
		Title: "One site, several departments",
		Lede:  "A production floor or a kitchen: one address, teams on different hours.",
		Points: []Claim{
			{
				Text:    "Departments carry their own shift, including one that runs past midnight.",
				Anchors: []Anchor{AnchorDepartmentShift},
			},
			{
				Text:    "Lateness is measured against the person's own department, not the site's.",
				Anchors: []Anchor{AnchorLatenessFollowsDepartment},
			},
			{
				// 🔴 THE REPLACEMENT FOR THE CLAIM THAT WAS WRONG. What departments
				// actually drive on the panel today is the record itself and the way a
				// manager narrows the list — not the report's breakdowns and not the
				// monthly headcount, neither of which has a department dimension at all.
				Text: "Every record carries the person's department, and the transactions list " +
					"filters down to one.",
				Anchors: []Anchor{AnchorDepartmentOnEveryRecord},
			},
		},
	},
}

// --- the legal surface ------------------------------------------------------

// LegalPage is one document under /legal. It is the ROUTE TABLE as well as the
// content skeleton: internal/handler mounts one GET per entry rather than
// repeating the paths, which is the shape PanelSections already uses for the
// panel.
type LegalPage struct {
	// Path is the URL. It is the identity of the entry — the handler mounts it and
	// the footer links to it, so there is one string rather than two.
	Path string
	// Title is the document's name, and the browser tab's.
	Title string
	// NavLabel is the short form used in the footer.
	NavLabel string
	// Lede says what the document will contain, so a visitor who arrives at a
	// skeleton learns something rather than nothing.
	Lede string
	// Needs are the concrete facts that must be supplied before the text can be
	// written. THEY ARE RENDERED, not kept in a planning document: a page that
	// says "not published yet" and stops is indistinguishable from a broken page,
	// and a reader who is entitled to this document deserves to see what it is
	// waiting on.
	Needs []string
}

// LegalPages is Q23's four documents. The card requires them reachable from the
// footer, and they are: marketingChrome renders this exact slice.
//
// 🔴 ALL FOUR SHIP AS SKELETONS AND THAT IS A DECISION, NOT AN OMISSION. A privacy
// policy names a controller, an imprint names a registered company and an address,
// and retention periods are commitments somebody is held to. None of those facts
// exists in this repository, and inventing them would produce a document that is
// legally wrong AND looks finished — the worse of the two failure modes. What ships
// is the route, the skeleton, the footer link and, on each page, the list of what
// it is waiting for.
//
// ⚠️ THE COOKIE NOTICE IS THE PARTIAL EXCEPTION and is treated as one. Which cookies
// this product sets, what each is for, how long it lasts and how it is scoped are
// facts IN THE CODE, so that half is real content built from the constants
// themselves (internal/handler's cookieNotice). The legal framing around it — who
// is asking, and under what basis — is still waiting with the rest.
var LegalPages = []LegalPage{
	{
		Path:     "/legal/privacy",
		Title:    "Privacy policy",
		NavLabel: "Privacy",
		Lede: "What Taptime records about the people who use it, why, how long it is kept " +
			"and how somebody exercises their rights over it.",
		Needs: []string{
			"The controller: the registered company acting as data controller, with its address.",
			"A contact point for data protection requests, and a data protection officer if one is appointed.",
			"The retention period for attendance records, audit entries and sessions — the deployment configures this, so the published number has to be the deployed one.",
			"Where the data is hosted and processed, and the processors involved.",
			"The legal basis relied on for each category, and the transfer position if any processor sits outside the EU.",
			"The supervisory authority a complaint goes to.",
		},
	},
	{
		Path:     "/legal/terms",
		Title:    "Terms of service",
		NavLabel: "Terms",
		Lede: "The agreement between Taptime and the organisation that subscribes: what is " +
			"provided, what it costs, and how either side ends it.",
		Needs: []string{
			"The contracting entity and the governing law.",
			"Billing terms: when an invoice is issued, payment period, VAT treatment, and what happens on non-payment.",
			"The exact wording of the founding offer — the free period and the price lock — and the conditions attached to it.",
			"Notice period and what happens to recorded data after an account ends.",
			"Any service commitment that is actually being made, or an explicit statement that none is.",
		},
	},
	{
		Path:     "/legal/imprint",
		Title:    "Company details",
		NavLabel: "Company details",
		Lede: "Who runs Taptime: the registered company, where it is registered and how to " +
			"reach it.",
		Needs: []string{
			"Registered company name, legal form and registration number.",
			"Registered office address.",
			"VAT identification number.",
			"A contact email address and, if there is one, a telephone number.",
			"The names of the people authorised to represent the company.",
		},
	},
	{
		Path:     "/legal/cookies",
		Title:    "Cookies",
		NavLabel: "Cookies",
		Lede: "Every cookie Taptime sets, what it is for and how long it lasts. Taptime sets no " +
			"advertising or analytics cookie and embeds nothing from another site.",
		Needs: []string{
			"The controller and contact point, as for the privacy policy.",
			"Confirmation of the legal basis: these cookies are the ones without which the service cannot be delivered, which is the basis this document will state.",
		},
	},
}

// CookieRow is one line of the cookie notice.
//
// 🔴 IT IS BUILT FROM THE CONSTANTS THAT WRITE THE COOKIES, never typed out. Every
// field below has a source in internal/session, internal/adminauth or
// internal/handler, and TestCookieNotice_ListsExactlyTheCookiesTheProductSets
// derives the set of cookie names from those packages' source and compares. A
// cookie added in a later task fails that test rather than silently going
// undisclosed, which is the whole reason this table is not prose.
type CookieRow struct {
	Name string
	// Purpose is what it is for, in a sentence a visitor can act on.
	Purpose string
	// Lifetime is the Max-Age the server asks for, in words.
	Lifetime string
	// Scope is the cookie's Path — the part of the site it is sent to. It is shown
	// because it is the difference between the two session cookies that matters to
	// a reader: one accompanies the tap pages, the other only the dashboard.
	Scope string
	// Flags is the attribute set, rendered as written: HttpOnly, SameSite and
	// whether Secure is set.
	Flags string
}

// LegalPageView is one legal document as rendered.
type LegalPageView struct {
	Page LegalPage
	// SignInHref is the panel's sign-in page, for the shared chrome. Same reason
	// LandingView carries it: internal/handler owns where the panel is mounted.
	SignInHref string
	// Cookies is the measured inventory. Only the cookie notice has one; for the
	// other three it is nil and the template renders none.
	Cookies []CookieRow

	// Body is the PUBLISHED text, already split into paragraphs by
	// internal/domain/legal.Paragraphs (M7-06). Empty means this document has no
	// text yet and the skeleton renders instead.
	//
	// 🔴 IT IS []string AND NOT HTML, AND THAT IS THE WHOLE SAFETY ARGUMENT. Every
	// element goes through templ's `{ }` interpolation, i.e. html.EscapeString, so a
	// legal text containing <script> renders as the characters somebody typed. The
	// alternative — building <p> tags in Go and passing them to templ.Raw — is
	// measured as a blind spot in this repository: templ.Raw has zero call sites and
	// not one of the twelve tests that scan .templ files would see a first one
	// appear.
	Body []string
	// PublishedAt is when the current version was published, already formatted, or
	// "" when there is none. It is shown because a legal document with no date is a
	// legal document nobody can cite.
	PublishedAt string
}

// Robots decides whether this page asks to be indexed, and it DERIVES the answer
// rather than carrying a flag.
//
// 🔴 THE DERIVATION IS THE POINT. A skeleton in a search index is an unpublished
// policy presented as the published one, so the rule is "a document is indexable
// once it has a text". Before M7-06 no legal page could have one and all four were
// private; now the day somebody pastes the privacy policy into the operator screen,
// this returns the other value for THAT page without anybody remembering to flip a
// bool. TestLegalPages_SkeletonsAreNotIndexedAndSayWhatTheyAreWaitingFor pins both directions.
//
// ⚠️ IT IS PER DOCUMENT, WHICH IS WHAT A PARTLY-FILLED DEPLOYMENT NEEDS. Publishing
// the privacy policy makes /legal/privacy indexable and leaves /legal/terms private
// in the same request — there is no "the legal pages are live" state, because there
// is no moment at which one exists.
func (v LegalPageView) Robots() layout.Robots {
	if v.Published() {
		return layout.RobotsPublic
	}
	return layout.RobotsPrivate
}

// Published reports whether THIS RENDER carries a finished text.
//
// 🔴 THE DEFINITION MOVED IN M7-06 AND THE OLD ONE IS WORTH RECORDING, because it
// was right for a product that had no way to publish. It used to be
// `len(p.Needs) == 0` on LegalPage — "to publish a document somebody has to remove
// the facts it needs, which means having them" — which was the only definition that
// could not be satisfied by flipping a bool while the texts lived in Go source.
// They no longer do (migration 00020), so the definition is now the stronger one it
// was standing in for: A DOCUMENT IS PUBLISHED WHEN SOMEBODY HAS PUBLISHED A TEXT.
// Needs survives as what the skeleton PRINTS while waiting, which is the job it was
// always doing on the page.
//
// ⚠️ THE LIMIT, STATED: this is "somebody deliberately published something", not
// "the text is adequate". Nothing in this product can judge a privacy policy, and a
// page that claimed otherwise would be making exactly the kind of guarantee it does
// not hold. What it does guarantee is that the page stops saying "not in force"
// only because a person went to the operator screen and published.
func (v LegalPageView) Published() bool { return len(v.Body) > 0 }
