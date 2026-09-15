package pages

import (
	"strings"

	"github.com/atknatk/tappa/web/templates/layout"
)

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
//     scans the rendered page for one, and the block-level tests refuse the exact
//     figures the user's draft carried ("30 seconds", "under two seconds", "in ten
//     seconds"). The ONE arithmetic figure on the page — what a 60-person team pays
//     a month — is computed by internal/handler from the published price and never
//     typed (LandingView.ExampleMonthly).
//   - A SUPERIORITY VERDICT. CLAUDE.md §4.1's ban is a FACT about Tappa and a good
//     reason to buy it; "less than one fingerprint terminal's yearly maintenance" is
//     a comparison nobody here has run. The comparison table below compares
//     mechanisms and says so in its own footnote.
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
//
// 🔴 2026-09-15: THE PAGE IS THE USER'S OWN DESIGN, AND THE COPY IS THE USER'S
// WITH THE SMALLEST EDITS THAT MAKE IT TRUE. The 2026-09-14 port carried the
// reference's words as literals in landing.templ and every pin in this file went
// dark — seventeen tests red, one panicking — because a pin guards only the text it
// renders. This revision moves the reference's copy back INTO the structures below,
// section by section in the page's own order (nav → hero → strip → how → ledger →
// security → fits → pricing → faq → cta → footer), so that the template renders
// what this file declares and nothing else. The markup is the reference's,
// unchanged. What changed in the WORDS is recorded beside each line, and it is all
// of one of the three kinds above: a number nobody measured, a verdict about a
// device nobody benchmarked, or a capability that is not in the product (an API, a
// file import, a live headcount, a report by department, "unlimited").

// LandingView is the public landing page.
//
// EIGHT FIELDS, AND FOUR OF THEM CARRY A NUMBER THE PRODUCT ALSO ENFORCES
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
	// ExampleHeadcount and ExampleMonthly are the price card's worked example —
	// "A 60-person chain runs on €90/month" — and the second is COMPUTED from the
	// first and the published price by internal/handler (exampleTeamMonthly), in
	// cents, never typed. The user's draft carried "about €90/month" as a literal;
	// a literal that happens to equal 60 × 1.50 today is a second copy of the price
	// that nothing compares, which is exactly the shape
	// TestLanding_PriceMatchesTheSchemaItIsCharged exists to refuse.
	//
	// ExampleMonthly is "" when the handler could not compute it, and the template
	// then renders no example at all rather than a wrong one — the same rule
	// SignupHref and DemoVideoSrc follow.
	ExampleHeadcount int
	ExampleMonthly   string
	// DemoVideoSrc is the URL of the recorded demo, or "" while there is no
	// recording. "Watch demo" in the hero opens a dialog either way; what is INSIDE
	// the dialog is what this field decides (2026-09-14).
	//
	// 🔴 IT IS A STRING FOR EXACTLY THE REASON SignupHref IS ONE, and the two
	// decisions are the same decision. A DemoVideo bool would say "a recording
	// exists" and still leave this template holding a hard-coded /static path — two
	// places to be right, one of which nobody tests, which is the shape that ships a
	// <video> pointing at a file somebody renamed. The handler stats the EMBEDDED
	// asset tree and puts the URL it found here; "" means it found nothing, and the
	// dialog then says so in words rather than rendering an element with a src that
	// answers 404.
	//
	// THE HANDLER IS THE ONLY PLACE THAT KNOWS THE PATH. This package never builds
	// one: see internal/handler's demoVideoAsset and staticAssetURL.
	DemoVideoSrc string
	// DemoPosterSrc is the poster frame's URL, or "" when there is no poster file.
	//
	// IT IS SEPARATE FROM DemoVideoSrc BECAUSE THE TWO FILES ARE SEPARATE. A
	// recording may be dropped in without a poster, and a poster without a recording
	// is nothing — so the handler only looks for this one when it found the video,
	// and the template only writes the attribute when it is non-empty. An empty
	// poster="" attribute is a request for the current document, which browsers
	// fetch and fail on; that is the defect this field's emptiness avoids.
	DemoPosterSrc string
}

// --- the three vocabularies -----------------------------------------------------
//
// Every claim-carrying value below names what it rests on in one of three
// vocabularies, and the split is a TEST FACT rather than taxonomy: each vocabulary
// is closed by its own test (every declared constant must be derived from the
// product AND claimed by a rendered line), so a constant added to the wrong block
// turns a test red for a reason that is not a defect.
//
//   - Source (internal/handler/marketing_claims_test.go) — ENGINE facts: a policy
//     statement, a query, a method. Named directly by the four evidence cards, and
//     indirectly by every Fact that delegates to one.
//   - Anchor (internal/handler/marketing_test.go) — SCHEMA and DOMAIN facts the
//     two-shapes block rests on.
//   - Fact (internal/handler/marketing_facts_test.go) — PRODUCT facts for the
//     steps, the ledger, the price card, the FAQ and the lede lines. Where a Fact
//     IS a fact one of the other two vocabularies already derives, its derivation
//     DELEGATES rather than reading the product a second way.
//
// ⚠️ THE LIMIT, WRITTEN ONCE FOR ALL THREE: naming a pin does not make a sentence
// true, and the test is blind to a sentence bolted onto an unrelated but healthy
// pin. This is a RATCHET AGAINST DRIFT — a line whose pin disappears fails, a line
// with no pin does not compile past the test — and not a proof of truth. Every
// sentence below was still read by a person against the product.

// Source is an ENGINE fact a security card rests on.
type Source string

const (
	// The guardrails §5's rows 2-4 name. Derived from
	// policy.Guardrails(policy.DefaultParams()): the sid must be present and carry
	// the effect the sentence depends on.
	SourceTagNotActive        Source = "policy:sys:tag-not-active"
	SourceSUNInvalid          Source = "policy:sys:sun-invalid"
	SourceNoSession           Source = "policy:sys:no-session"
	SourceEmployeeDeactivated Source = "policy:sys:employee-deactivated"
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
	// "within 150 m" is the deployment's default radius: internal/config reads
	// TAPPA_GPS_RADIUS_M with a default of 150 (bounded by policy.GPSRadiusMinM and
	// GPSRadiusMaxM). Derived from the config source. ITS LIMIT, STATED: a
	// deployment that sets the variable to something else makes the page's number
	// the default and not that deployment's value; the number is the offer, as the
	// price is.
	SourceGPSRadiusDefault Source = "config:TAPPA_GPS_RADIUS_M default 150"
)

// Anchor is a SCHEMA or DOMAIN fact the two-shapes block rests on.
//
// 🔴 IT EXISTS BECAUSE A SENTENCE ON THIS PAGE SHIPPED THAT THE PRODUCT DOES NOT
// DO. "Reports and the monthly headcount split by department" was rendered on /
// and was wrong in BOTH halves, measured: the reports section breaks down per
// person, per venue and by open entries and contains the word "department" zero
// times (ledger.Report has People and Venues and no department dimension; there is
// no Department field on PersonHours), and the whole billing surface — the domain,
// the handler, the CSV, the view and the template — contains it zero times too.
// The user's 2026-09-12 draft carried the same claim again ("per-department shifts
// & reports"), and the tripwire that refuses it is
// TestLandingAudiences_SayNothingAboutTheTwoBreakdownsThatDoNotExist.
type Anchor string

const (
	// AnchorPlaqueBelongsToVenue — every plaque is mounted at exactly one venue.
	// Derived from migration 00004: tags.location_id is NOT NULL.
	AnchorPlaqueBelongsToVenue Anchor = "schema:tags.location_id"
	// AnchorVenueShiftAndAddress — a venue carries its own hours and its own
	// allowed addresses. Derived from migration 00002's locations table:
	// shift_start/shift_end and static_ips cidr[].
	AnchorVenueShiftAndAddress Anchor = "schema:locations.shift+static_ips"
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
	// AnchorDirectionFollowsLastOpenEntry — in or out is decided against the
	// person's last OPEN check-in, not the calendar day (CLAUDE.md §5,
	// internal/domain/tap.resolveDirection over Input.LastOpenIn). Derived by
	// reflection over tap.Input and a scan for the function. It was a Fact until
	// 2026-09-15; the only sentence that claims it now is in the two-shapes block.
	AnchorDirectionFollowsLastOpenEntry Anchor = "tap:resolveDirection(Input.LastOpenIn)"
)

// Fact is a PRODUCT fact one of the other blocks rests on.
//
// Five of the facts are TRIPWIRES FOR AN ABSENCE — no bulk import, no integration
// API, no finger or face reader, no background position, and "no" is what the page says —
// because the user's draft promised the first two and a tripwire fails the day one
// appears, which is the day that sentence has to be rewritten.
type Fact string

const (
	// The tap page is a web page at a route, which is the whole of "no app".
	// Delegates to SourceTapIsAWebPage.
	FactTapPageNeedsNoApp Fact = "fact:tap-is-a-web-page"
	// The plaque is a passive NTAG 424 DNA chip that rewrites its own URL (skill
	// tappa-sun; internal/sun.Parse reads the ctr and cmac it writes). Delegates to
	// SourceSUNURLCarriesCounterAndSignature. ITS LIMIT, STATED: no Go code can
	// prove a chip has no battery; what the derivation holds is that the product
	// reads a counter and a signature the plaque itself wrote, which is the
	// mechanism a passive NFC tag provides and a powered reader does not.
	FactPlaqueIsPassive Fact = "fact:plaque-writes-its-own-url"
	// The plaque identifies the PLACE and not the person: every plaque is tied to
	// exactly one venue and carries nothing about anybody. Delegates to
	// AnchorPlaqueBelongsToVenue.
	FactPlaqueIdentifiesThePlace Fact = "fact:plaque-identifies-the-place"
	// The tap button carries a 64px minimum (input.css .tap-button, min-h-16),
	// which skill tappa-brand fixes so a gloved or wet finger can press it. Derived
	// by reading the rule. "Under two seconds" from the user's draft was NOT
	// carried: nobody has timed anybody's hands, and the sentence says what the
	// button is built FOR, which is what the rule says.
	FactTapButtonSizedForAWetHand Fact = "css:.tap-button min-h-16"
	// The tap page greets the person by name: pages.Tap renders EmployeeName.
	// Derived by reading the template.
	FactTapPageGreetsByName Fact = "templ:tap greets EmployeeName"
	// People are added one at a time from the dashboard (POST employeeAddHref in
	// internal/handler/dashboard.go) and open their invitation on their own phone
	// (GET /activate in internal/handler/activate.go). Derived from both
	// registrations.
	FactPeopleAreInvitedFromTheDashboard Fact = "route:POST employeeAddHref+GET /activate"
	// A person switched off from the dashboard (POST employeeDeactivateHref) is
	// refused at their next tap: sys:employee-deactivated carries EffectDeny.
	// Delegates to SourceEmployeeDeactivated for the second half and reads the
	// registration for the first.
	//
	// ⚠️ "ONE CLICK" AND "THAT SECOND" WERE THE USER'S WORDS AND NEITHER IS WHAT THE
	// PRODUCT DOES. Deactivation asks for a confirmation first
	// (internal/handler/deactivateconfirm.go), so it is two; and what ends is not
	// "access" at a moment but the NEXT TAP, which is refused. Both sentences that
	// carried the phrase now say the second thing.
	FactDeactivatedPersonIsRefused Fact = "route:POST employeeDeactivateHref+policy:sys:employee-deactivated"
	// A plaque can be replaced on the same wall from the dashboard (POST
	// plaqueReplaceHref → replacePlaque retires the old one and binds its successor),
	// and the retired one decides nothing (sys:tag-not-active). Reads the
	// registration and delegates to SourceTagNotActive. "30 seconds" from the draft
	// was not carried.
	FactPlaqueCanBeReplaced Fact = "route:POST plaqueReplaceHref+policy:sys:tag-not-active"
	// A tap with no address match is approved on the position alone when the
	// position is close enough (base:gps-only-allow carries EffectAllow), which is
	// what "phones fall back to mobile data + GPS" means. Delegates to
	// SourceGPSOnlyAllow.
	FactGPSAloneApprovesATap Fact = "fact:gps-alone-approves-a-tap"
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
	// TRIPWIRE (§4.1): nothing in the product reads a finger or a face. Derived as
	// an ABSENCE — no non-test Go source and no shipped script names the
	// platform-authenticator API (navigator.credentials, PublicKeyCredential) or a
	// reader for either. The mechanical net for the same red line is
	// scripts/redline-check.sh's R1; this is the page's own pin, so "none collected,
	// ever" fails here the day a reader appears.
	FactNoAuthenticatorAPI Fact = "absent:platform-authenticator-api"
	// TRIPWIRE (§4.2): the position is read once, on a press, and never watched.
	// Derived as an ABSENCE and a PRESENCE — no shipped script calls the
	// Geolocation API's continuous read (the call scripts/redline-check.sh's R2
	// names), and the tap page's own script asks for getCurrentPosition, which is
	// the one-shot read.
	FactPositionReadOnlyOnPress Fact = "js:getCurrentPosition, no continuous read"
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
	// Plaques are included in the price and replaced free. THIS IS A PRICING TERM,
	// NOT A CAPABILITY, and it is pinned the way the price is — to the document
	// that publishes the offer: docs/handoff.md §11, "plaketler dahil ve ücretsiz
	// değişim". Derived by reading that line. ITS LIMIT, STATED: the handoff is
	// the offer, not a stock room; what the product provides is the mount and
	// replace flows (FactPlaqueCanBeReplaced), and the plaque itself is hardware.
	FactPlaquesIncludedAndReplacedFree Fact = "handoff:§11 plaques included and replaced free"
	// The report totals per person and per venue. Delegates to
	// AnchorPerVenueReport.
	FactReportPerPersonAndVenue Fact = "fact:report-per-person-and-venue"
)

// --- nav --------------------------------------------------------------------------

// NavLink is one in-page link in the landing page's own navigation bar.
type NavLink struct {
	Label string
	// Href is an in-page anchor — "#how" — and nothing else. The page's sections
	// carry these ids, and TestLandingNav_EveryLinkPointsAtASectionOnThePage follows
	// each one against the rendered page, so a section renamed without this list
	// going with it is a red test rather than a link that scrolls nowhere.
	Href string
}

// LandingNav is the sticky bar's three in-page links. The two OTHER things in the
// bar — "Sign in" and "Start free" — are not here because they are not in-page:
// they come from LandingView.SignInHref and LandingView.SignupHref for the reason
// those fields record, and the header renders them from the view.
var LandingNav = []NavLink{
	{Label: "How it works", Href: "#how"},
	{Label: "Security", Href: "#security"},
	{Label: "Pricing", Href: "#pricing"},
}

// --- lines --------------------------------------------------------------------------

// Line is one sentence of copy paired with the product facts it rests on. It is
// what a marketing sentence has to be: the text and its answer to "which code
// provides this?" in one value, rendered from here rather than typed into the
// template.
type Line struct {
	Text  string
	Facts []Fact
}

// LandingHeroLede is the sentence under the slogan. "Verified, timestamped, on the
// right site": the plaque names the site (tags.location_id) and the phone's browser
// opens the page that records the tap.
var LandingHeroLede = Line{
	Text: "Stick one smart plaque on the wall. Your team taps their own phone on it and " +
		"they're clocked in — verified, timestamped, on the right site.",
	Facts: []Fact{FactTapPageNeedsNoApp, FactPlaqueIdentifiesThePlace},
}

// LandingHeroFoot is the small line under the hero's two buttons.
var LandingHeroFoot = Line{
	Text:  "Set up a location · works with the phone already in every pocket",
	Facts: []Fact{FactVenuesAndDepartmentsCarryOwnHours, FactTapPageNeedsNoApp},
}

// LandingSetupLede sits under the "how it works" heading, carried word for word.
// "Enroll" here is the ledger's word for enrolment AT A DEVICE ("Enrollment at the
// device"), which is the thing there is none of; opening the invitation link once is
// step two and is said there.
var LandingSetupLede = Line{
	Text: "There is nothing to install, enroll, or maintain. The plaque is passive — no " +
		"battery, no software, no internet of its own.",
	Facts: []Fact{FactTapPageNeedsNoApp, FactPlaqueIsPassive},
}

// LandingSecurityLede sits under "Four proofs on every single tap." — the four
// signals are the four cards below it, and "before it lands in your records" is
// §4.6: a tap that cannot be verified is still written, marked, and queued.
var LandingSecurityLede = Line{
	Text: "A tap isn't trusted — it's verified. Each clock-in is scored against four " +
		"independent signals before it lands in your records.",
	Facts: []Fact{FactFlaggedQueueDecidedByAManager},
}

// LandingPrivacyNote is the green box under the four cards: §4.1 and §4.2 (the
// position is read only at the moment of a tap), each pinned to a tripwire.
var LandingPrivacyNote = Line{
	Text: "Taptime stores no fingerprints, no face scans, and never tracks location — GPS " +
		"is read only at the moment of the tap. Built for GDPR from day one.",
	Facts: []Fact{FactNoAuthenticatorAPI, FactPositionReadOnlyOnPress},
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

// LandingPricingLede is the line under it: the pricing terms handoff §11 publishes.
var LandingPricingLede = Line{
	Text:  "No hardware bill, no installation fee, no maintenance contract. Plaques included.",
	Facts: []Fact{FactPricePerEmployee, FactPlaquesIncludedAndReplacedFree},
}

// --- the steps -------------------------------------------------------------------

// Span is one run of a step's body: plain, or set in bold as the reference sets it.
//
// IT IS A SLICE OF RUNS AND NOT A STRING WITH MARKUP for the reason every other
// value here is data: the template escapes each run, so nothing in this file can
// put a tag on the page. The test renders each run in order.
type Span struct {
	Text   string
	Strong bool
}

// Step is one of the three cards under "How it works".
type Step struct {
	// Ordinal is printed in the data typeface above the icon — "STEP 1".
	Ordinal string
	// Icon is the reference's own emoji for the card.
	Icon  string
	Title string
	Body  []Span
	// Facts are the product facts this step rests on — see Fact. Never empty.
	Facts []Fact
}

// LandingSteps is the reference's three steps. Two sentences changed, both for a
// number nobody measured and both recorded on the run that replaced them.
var LandingSteps = []Step{
	{
		Ordinal: "STEP 1",
		Icon:    "🏷️",
		Title:   "Stick the plaque",
		Body: []Span{
			{Text: "One branded A5 plaque per location, placed by the entrance — ideally in view of " +
				"your camera. Inside it: a cryptographic NFC chip that identifies "},
			{Text: "the place", Strong: true},
			{Text: ", not the person."},
		},
		Facts: []Fact{FactPlaqueIsPassive, FactPlaqueIdentifiesThePlace},
	},
	{
		Ordinal: "STEP 2",
		Icon:    "✉️",
		Title:   "Invite your team",
		// "30 seconds, ever" → "once, ever": the ONCE is the fact (one activation per
		// person, GET /activate), the thirty seconds is a stopwatch nobody held.
		// "switched off with one click, instantly" → "switched off from the dashboard
		// — their next tap is refused": see FactDeactivatedPersonIsRefused.
		Body: []Span{
			{Text: "Create a profile, send the invite. Each person activates on their own phone — "},
			{Text: "once, ever", Strong: true},
			{Text: ". New hires clock in on day one; leavers are switched off from the dashboard " +
				"— their next tap is refused."},
		},
		Facts: []Fact{FactPeopleAreInvitedFromTheDashboard, FactDeactivatedPersonIsRefused},
	},
	{
		Ordinal: "STEP 3",
		Icon:    "📶",
		Title:   "Tap in, tap out",
		// "Under two seconds, even with wet or floury hands." → "Built for wet or
		// floury hands.": the button's 64px floor is the rule the sentence describes
		// (FactTapButtonSizedForAWetHand); the two seconds were never measured.
		Body: []Span{
			{Text: "Hold the phone to the plaque. The page opens by itself — no app — greets them " +
				"by name, one button, done. "},
			{Text: "Built for wet or floury hands.", Strong: true},
		},
		Facts: []Fact{FactTapPageNeedsNoApp, FactTapPageGreetsByName, FactTapButtonSizedForAWetHand},
	},
}

// --- the ledger ---------------------------------------------------------------------

// ComparisonRow is one line of the comparison table: an aspect, and what each
// approach does about it.
type ComparisonRow struct {
	Aspect string
	// Terminal is the device column. Every cell is restricted to what a
	// finger or card reader is BY DEFINITION — powered, on the wall, enrolled
	// at — because nobody here has benchmarked one.
	Terminal string
	// Tappa is our column, and it is the half that is checkable here: Facts are the
	// product facts the cell rests on. Never empty.
	Tappa string
	Facts []Fact
}

// LandingComparison is the reference's six-row ledger.
//
// 🔴 IT COMPARES MECHANISMS AND DECLARES NO WINNER, and that restriction is the
// whole design of this table rather than modesty. Tappa's half is checkable in this
// repository. The other half is NOT — nobody here has benchmarked a device — so
// every cell in the Terminal column is restricted to what the approach is BY
// DEFINITION. The footnote saying so is rendered, not just written here:
// TestLanding_ComparesMechanismsAndDeclaresNoWinner.
//
// THE LAST ROW'S ASPECT IS THE USER'S WORD. scripts/redline-check.sh's R1 waives it
// on this page as the whole Go string literal, quotes included (ADR 0012, ek
// 2026-09-15) — so the literal stays on ONE line and is not built from parts; the
// scanner is line-local and a split would read as a violation.
var LandingComparison = []ComparisonRow{
	{
		Aspect:   "Hardware per site",
		Terminal: "Reader + wiring + maintenance",
		Tappa:    "One passive sticker-plaque",
		Facts:    []Fact{FactPlaqueIsPassive},
	},
	{
		Aspect:   "New-hire setup",
		Terminal: "Enrollment at the device",
		// "activates on first tap" → "activated on their own phone": activation is
		// the invitation link being opened (GET /activate), and the first tap comes
		// after it.
		Tappa: "Invite link, activated on their own phone",
		Facts: []Fact{FactPeopleAreInvitedFromTheDashboard},
	},
	{
		Aspect:   "When someone leaves",
		Terminal: "Delete from each terminal",
		// "One click — access dies that second" → see FactDeactivatedPersonIsRefused.
		Tappa: "Switch off in the dashboard — the next tap is refused",
		Facts: []Fact{FactDeactivatedPersonIsRefused},
	},
	{
		Aspect:   "If it breaks",
		Terminal: "Site goes blind until repair",
		// "Stick a spare plaque, 30 seconds" → the swap is real (replacePlaque), the
		// seconds are not measured.
		Tappa: "Stick a spare plaque and swap it in the dashboard",
		Facts: []Fact{FactPlaqueCanBeReplaced},
	},
	{
		Aspect:   "Internet down on site",
		Terminal: "Device offline, records stuck",
		Tappa:    "Phones fall back to mobile data + GPS",
		Facts:    []Fact{FactGPSAloneApprovesATap},
	},
	{
		Aspect:   "Biometric data",
		Terminal: "Fingerprints stored = GDPR weight",
		Tappa:    "None collected, ever",
		Facts:    []Fact{FactNoAuthenticatorAPI},
	},
}

// LandingComparisonNote is the footnote under the table, rendered in the ledger's
// muted tone. It is the sentence that makes the table a comparison of mechanisms
// rather than a benchmark, and TestLanding_ComparesMechanismsAndDeclaresNoWinner
// requires it on the page.
const LandingComparisonNote = "This table compares how the two approaches work. It is not a " +
	"benchmark: we have measured no device against Taptime and make no claim about " +
	"accuracy or reliability."

// --- the four proofs -----------------------------------------------------------------

// Evidence is one of the four things Tappa weighs when it decides a record.
type Evidence struct {
	// Proof is the card's eyebrow: "Proof of moment", "Proof of person", "Proof of
	// place" and — for the position — "Backup proof of place", which is CLAUDE.md
	// §5's own word for it ("yedek nerede").
	Proof string
	// Label is the card's heading.
	Label string
	Body  string
	// Sources are the engine facts this card rests on — see Source. Never empty.
	Sources []Source
}

// LandingEvidence is CLAUDE.md §5's "Dört kanıt", in the reference's words.
//
// ⚠️ TWO EDITS. The fourth eyebrow said "Backup proof" and now says "Backup proof
// of place" — §5's own name for it, and the test that holds the fallback to being
// called a backup asks for the full phrase. The third card's last clause, "and it
// resolves the location automatically", is gone: the location of a tap comes from
// the PLAQUE (tags.location_id); the address only confirms the phone is there
// (tap.Decide's ipMatches takes the plaque's venue's ranges as its input).
var LandingEvidence = []Evidence{
	{
		Proof: "Proof of moment",
		Label: "One-time signature",
		Body: "The plaque's NTAG 424 DNA chip signs every tap with a fresh counter + AES " +
			"cryptogram. Copied or replayed links are rejected — you must physically be at " +
			"the plaque, right now.",
		Sources: []Source{SourceSUNURLCarriesCounterAndSignature, SourceSignatureIsVerified,
			SourceCounterAdvanceIsGuarded, SourceSUNInvalid},
	},
	{
		Proof: "Proof of person",
		Label: "Phone session",
		Body: "Identity lives in a secure session on each employee's own phone, behind their " +
			"own screen lock. No shared PINs, no swappable cards — the phone has to be there.",
		Sources: []Source{SourceNoSession},
	},
	{
		Proof: "Proof of place",
		Label: "Site IP match",
		Body: "Requests from your site's WiFi carry its static IP — hard evidence the person " +
			"is inside the building.",
		Sources: []Source{SourceIPOrGPSOK},
	},
	{
		Proof: "Backup proof of place",
		Label: "GPS fallback",
		// "150\u00a0m" is the reference's own no-break space, so the number and its
		// unit cannot be split across a line.
		Body: "WiFi down or phone on mobile data? GPS confirms presence within 150\u00a0m. Anything " +
			"unverified is stored, flagged, and queued for manager review — never silently " +
			"approved.",
		Sources: []Source{SourceGPSOnlyAllow, SourceGPSRadiusDefault, SourceNoEvidenceReview},
	},
}

// --- the two shapes -------------------------------------------------------------------

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
	// Kicker is the small label over the heading — "Multi-location".
	Kicker string
	Title  string
	// Points are the sentences of the card's paragraph, in order. Each is a
	// behaviour, not a benefit, and each names the product capability it rests on.
	Points []Claim
	// Example is the green chip under the paragraph.
	Example Claim
}

// Paragraph is the card's sentences joined by one space, in the declared order —
// the one string the template prints, so that the paragraph's bytes are the
// reference's (templ puts whitespace of its own around expressions printed one
// per line, and the test that holds every sentence rendered would not see it).
func (a Audience) Paragraph() string {
	parts := make([]string, 0, len(a.Points))
	for _, c := range a.Points {
		parts = append(parts, c.Text)
	}
	return strings.Join(parts, " ")
}

// LandingAudiences is the reference's "Built for both shapes of business".
//
// BOTH CARDS DESCRIBE THE SAME SCHEMA. A chain is several `locations`, a facility
// is one location and several `departments`, and the difference a person actually
// feels is which shift their lateness is measured against: CLAUDE.md §5 takes the
// department's shift when there is one.
//
// ⚠️ THE TWO CHIPS CHANGED. "live headcount per location" is a capability the
// product does not have (nothing on the panel updates without a request; the
// headcount is a monthly billing figure) and it is one of the phrases
// TestLandingPricingIncludes_EveryLineIsRenderedAndRestsOnAFact refuses by name —
// the chip now names the per-venue hours the report really totals.
// "per-department shifts & reports" claimed the breakdown the Anchor block's
// header records as absent; the chip now names the two things departments really
// drive, their shift and the record filter.
var LandingAudiences = []Audience{
	{
		Kicker: "Multi-location",
		Title:  "Restaurant & retail chains",
		Points: []Claim{
			{
				Text:    "One plaque per site, each with its own network address on file.",
				Anchors: []Anchor{AnchorPlaqueBelongsToVenue, AnchorVenueShiftAndAddress},
			},
			{
				Text: "Late-night bars welcome: overnight shifts pair check-outs to the last open " +
					"check-in, not the calendar date.",
				Anchors: []Anchor{AnchorDirectionFollowsLastOpenEntry},
			},
		},
		Example: Claim{
			Text:    "9 sites · 1 dashboard · hours per location",
			Anchors: []Anchor{AnchorPerVenueReport},
		},
	},
	{
		Kicker: "Single site, many teams",
		Title:  "Production & facilities",
		Points: []Claim{
			{
				Text: "One plaque at the gate; departments resolve automatically from each " +
					"employee's profile — bakers against their 04:00 shift, meat production " +
					"against 05:00.",
				Anchors: []Anchor{AnchorDepartmentOnEveryRecord, AnchorLatenessFollowsDepartment},
			},
			{
				Text:    "No extra hardware per floor.",
				Anchors: []Anchor{AnchorPlaqueBelongsToVenue},
			},
		},
		Example: Claim{
			Text:    "5 departments · per-department shifts · records filtered by department",
			Anchors: []Anchor{AnchorDepartmentShift, AnchorDepartmentOnEveryRecord},
		},
	},
}

// --- the price card ---------------------------------------------------------------------

// LandingPricingIncludes is the price card's list, and EVERY LINE IS SOMETHING THE
// PRODUCT DOES TODAY OR A TERM THE OFFER PUBLISHES. The user's draft listed five;
// what changed, line by line:
//
//   - "Unlimited locations & departments" — nothing caps how many a tenant
//     creates, but the panel's lists are capped at venuePageLimit, so "unlimited"
//     is a word the screen contradicts. The line now says what each of them
//     carries.
//   - "Branded wall plaques included & replaced free" — kept; it is handoff §11's
//     pricing term, pinned to the handoff (FactPlaquesIncludedAndReplacedFree).
//   - "Live dashboard, flags & manager review queue" — "Live" dropped: the panel
//     renders on request and pushes nothing.
//   - "Daily reports, CSV export & API for your payroll flow" — the CSV half is
//     real; the API half is NOT (FactNoIntegrationAPI is the tripwire), and the
//     reports are by period, not daily.
//   - "Runs alongside your current system during trial" — not a product feature
//     at all; replaced by the manual entry, which is.
var LandingPricingIncludes = []Line{
	{
		Text:  "Locations & departments, each on its own hours",
		Facts: []Fact{FactVenuesAndDepartmentsCarryOwnHours},
	},
	{
		Text:  "Branded wall plaques included & replaced free",
		Facts: []Fact{FactPlaquesIncludedAndReplacedFree, FactPlaqueCanBeReplaced},
	},
	{
		Text:  "Dashboard, flags & manager review queue",
		Facts: []Fact{FactFlaggedQueueDecidedByAManager},
	},
	{
		Text:  "Hours & headcount reports, CSV export for your payroll flow",
		Facts: []Fact{FactHoursExportAsCSV, FactBillingExportAsCSV},
	},
	{
		Text:  "Manual entries, marked apart from tapped ones",
		Facts: []Fact{FactManualEntryNamesAManager},
	},
}

// --- the FAQ -------------------------------------------------------------------------------

// Question is one FAQ entry.
type Question struct {
	Q string
	A string
	// Facts are the product facts the answer rests on — see Fact. Never empty.
	Facts []Fact
}

// LandingFAQ is the reference's five questions.
//
// 🔴 FOUR OF THE FIVE ANSWERS WERE THE MOST DANGEROUS COPY IN THE DRAFT, because
// each answered with something the product does not do, and two mis-stated the
// decision engine. What changed:
//
//   - "in ten seconds" (the manual entry) — a number nobody measured; dropped.
//   - "Every plaque also carries a QR code … requires the IP OR GPS proof instead"
//     — whether a code is printed on a plaque is hardware (M8-05), and the rule is
//     WRONG: the codeless channel REQUIRES the address, and a position alone ends
//     in FLAGGED (§5, base:qr-requires-ip). The answer now says that.
//   - "No. … IP + GPS have to agree" — wrong both ways: §5's line six is a
//     disjunction (either half is enough), and "No" is too strong because a
//     flagged record IS written. The answer says what happens instead.
//   - "Import your staff list from a CSV … run both systems side by side … Most
//     sites switch in one pay period" — there is no import (FactNoBulkImport is
//     the tripwire), and the rest is a process claim nobody measured.
//   - "a clean API feed your payroll or HR system directly" — there is no API
//     (FactNoIntegrationAPI is the tripwire). The last sentence is the user's and
//     is kept, because it is true.
var LandingFAQ = []Question{
	{
		Q: "What if someone doesn't have their phone that day?",
		A: "The shift lead records the entry from the dashboard. It's stored with a " +
			"\"Entered by a manager\" label, so your reports always show exactly which records " +
			"were tapped and which were vouched for.",
		Facts: []Fact{FactManualEntryNamesAManager},
	},
	{
		Q: "What if the phone has no NFC?",
		A: "The same page opens from a QR code with the camera. A QR tap carries no " +
			"cryptographic signature — no proof of a physical touch — so it needs your " +
			"venue's network address to match before it's approved. A position on its own " +
			"is not enough: without the address it's stored, flagged and queued for review.",
		Facts: []Fact{FactTapPageNeedsNoApp, FactCodelessTapNeedsAddress, FactFlaggedQueueDecidedByAManager},
	},
	{
		Q: "Can someone clock in from home?",
		// 🔴 READ THE SENTENCES AGAINST §5 BEFORE TOUCHING THEM. A link with the
		// plaque's code only exists once a phone has read the plaque, and the second
		// opening of it loses (the counter guard). A check-in WITHOUT the code is the
		// codeless channel: it needs the venue's address, and from anywhere else it is
		// FLAGGED — recorded, queued, not approved. "Not without a manager seeing it"
		// is therefore the honest answer, and "No" would have been too strong.
		A: "Not without a manager seeing it. The plaque's chip signs each tap with a " +
			"one-time cryptogram, so the link only exists if a phone touched the plaque at " +
			"that moment. A copied link, a screenshot, or yesterday's URL is rejected by " +
			"the server. A check-in without the code needs your venue's network address to " +
			"be approved; from anywhere else it's stored, flagged and queued for a manager " +
			"rather than approved.",
		Facts: []Fact{FactCopiedLinkIsRefused, FactCodelessTapNeedsAddress, FactFlaggedQueueDecidedByAManager},
	},
	{
		Q: "How do we migrate from our current device?",
		A: "Add your people from the dashboard and hand out invite links — one profile at a " +
			"time; there is no file import today. The old box is yours to unplug whenever " +
			"you're ready — Taptime doesn't talk to it.",
		Facts: []Fact{FactPeopleAreInvitedFromTheDashboard, FactNoBulkImport, FactNoIntegrationAPI},
	},
	{
		Q: "Where does the data go?",
		A: "Into your dashboard: hours per person and per site, and the monthly headcount " +
			"you're billed on — both as CSV exports for your payroll. There is no API. " +
			"Taptime is the source of truth for in/out times — it doesn't try to replace " +
			"your payroll.",
		Facts: []Fact{FactReportPerPersonAndVenue, FactHoursExportAsCSV, FactBillingExportAsCSV, FactNoIntegrationAPI},
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
