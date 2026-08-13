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
//   - A CAPABILITY THAT IS NOT MOUNTED. The sign-up wizard is M7-02 and does not
//     exist, so LandingView.SignupHref is "" and the page says so in words instead
//     of shipping a button that 404s.
//     TestMarketing_EveryInternalLinkResolves follows every link on the surface
//     against the real router.

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

// Step is one of the three things that happen before a shift is being recorded.
type Step struct {
	// Ordinal is printed as the step number. A string because it is set in the
	// data typeface beside a heading, not counted.
	Ordinal string
	Title   string
	Body    string
}

// LandingSteps is handoff §9's "3 adım".
//
// EVERY LINE IS A DESCRIPTION OF SOMETHING MOUNTED. Step 1 is the plaque
// (internal/sun reads what it writes), step 2 is the activation flow
// (handler.Activation, GET /activate), step 3 is the tap flow (handler.Tap,
// GET /t and POST /api/checkin) with the direction rule from CLAUDE.md §5.
var LandingSteps = []Step{
	{
		Ordinal: "01",
		Title:   "A plaque goes up by the door",
		Body: "It is a printed plaque with a passive chip behind it. No power, no network, " +
			"no battery to change. One at each entrance you want to record.",
	},
	{
		Ordinal: "02",
		Title:   "Each person opens their link once",
		Body: "You invite them from the dashboard. They open the link on their own phone, " +
			"read what is recorded and why, and agree. That is the only setup they ever do.",
	},
	{
		Ordinal: "03",
		Title:   "After that it is one tap",
		Body: "Hold the phone to the plaque, the browser opens, one button. Tappa knows " +
			"whether this is a way in or a way out from the last entry that is still open — " +
			"so a shift that ends at 02:00 closes the one that started at 18:00.",
	},
}

// Evidence is one of the four things Tappa weighs when it decides a record.
type Evidence struct {
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
		Label:   "The plaque's one-time code",
		Answers: "a real touch, just now",
		// ⚠️ "a key that never leaves our database" WAS THE FIRST DRAFT AND IT IS NOT
		// TRUE: the key is stored wrapped and is unwrapped IN THE SERVER to check the
		// signature, so it leaves the database every time somebody taps. What is true
		// and checkable is where it never goes — internal/sun and CLAUDE.md §4.7.
		Body: "The chip writes a fresh single-use code every time a phone reads it, signed " +
			"with a key that is never printed on the plaque, never sent to the phone and " +
			"never written to a log. Tappa checks the signature and refuses any code it " +
			"has already seen, so a copied link is worth nothing.",
	},
	{
		Label:   "The sign-in on the phone",
		Answers: "who tapped",
		Body: "Set once, when your colleague opened their link. Tappa stores a hash of it " +
			"rather than the value, so the thing in their browser cannot be read back " +
			"out of our database.",
	},
	{
		Label:   "Your venue's network address",
		Answers: "where it happened",
		Body: "If you tell Tappa the fixed address a venue's internet connection uses, a tap " +
			"arriving from it is a tap that happened there.",
	},
	{
		Label:   "The phone's position, at that moment",
		Answers: "where it happened, without a fixed address",
		// ⚠️ "shows the position on no screen" HAD TO BE NARROWED. The panel DOES show
		// coordinates — a VENUE's own, typed in by a manager (internal/handler's
		// locations screen). What no screen and no export carries is where a PERSON
		// was: internal/domain/ledger's two view types and components.DocketView have
		// no Latitude and no Longitude field, and the queries do not select them.
		Body: "If there is no address to match, the browser may offer the phone's position as " +
			"the button is pressed. Once, on a press. Tappa does not watch a location in " +
			"the background and does not draw a boundary to be alerted about, and no " +
			"screen or export in Tappa shows where a person was — only whether they were " +
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
}

// LandingFAQ is handoff §9's FAQ.
//
// EVERY ANSWER NAMES A BEHAVIOUR THAT IS IMPLEMENTED. In order: the tap page loads
// no application; the QR-shaped channel exists in internal/sun.Parse and is
// governed by the base:qr-requires-ip policy; Q18 decided the system produces no
// checkout of its own, so handler + internal/domain/manual make a person type one;
// CLAUDE.md §4.3 and migration 0005 make `transactions` append-only; §4.5 and every
// migration's row-level security isolate a tenant; §4.6 refuses to drop a record it
// cannot judge.
var LandingFAQ = []Question{
	{
		Q: "Does my team have to install anything?",
		A: "No. Holding the phone to the plaque opens a web page. There is no app, no " +
			"account to create on the phone and nothing to update.",
	},
	{
		Q: "What about phones that cannot read the plaque?",
		A: "Older phones cannot read a chip in the background. Tappa accepts a check-in that " +
			"arrives without the plaque's one-time code — but that check-in carries no proof " +
			"of a physical touch, so it needs your venue's network address to match. A " +
			"position on its own is not enough for it. Your organisation can change that rule.",
	},
	{
		Q: "What happens when somebody forgets to clock out?",
		A: "Tappa does not invent a clock-out. The entry stays open, it is listed as an " +
			"anomaly for a manager, and the hours are typed in by a person. Open entries are " +
			"left out of the totals and the report says so rather than quietly rounding.",
	},
	{
		Q: "Can a record be edited?",
		A: "No. A recorded tap is never changed and never deleted. A correction is a new " +
			"record with the manager's name on it, and both stay — that is what makes the " +
			"record usable as evidence later.",
	},
	{
		Q: "What if Tappa cannot tell where a tap happened?",
		A: "It writes the record anyway, marks it FLAGGED and puts it in a manager's queue. " +
			"A record is never dropped for want of proof, and nothing is approved silently.",
	},
	{
		Q: "Can one organisation see another's records?",
		A: "No. Every table carries the organisation it belongs to, every table enforces that " +
			"inside the database rather than only in the application, and the application " +
			"connects with a role that cannot bypass it.",
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
		Lede: "What Tappa records about the people who use it, why, how long it is kept " +
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
		Lede: "The agreement between Tappa and the organisation that subscribes: what is " +
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
		Lede: "Who runs Tappa: the registered company, where it is registered and how to " +
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
		Lede: "Every cookie Tappa sets, what it is for and how long it lasts. Tappa sets no " +
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
}

// Robots decides whether this page asks to be indexed, and it DERIVES the answer
// rather than carrying a flag.
//
// 🔴 THE DERIVATION IS THE POINT. A skeleton in a search index is an unpublished
// policy presented as the published one, so the rule is "a document is indexable
// once it has a text". Today no legal page has one and all four are private; the
// day somebody pastes the privacy policy in, this returns the other value without
// anybody remembering to flip a bool. TestLegalPages_SkeletonsAreNotIndexed pins
// both directions.
func (v LegalPageView) Robots() layout.Robots {
	if v.Page.Published() {
		return layout.RobotsPublic
	}
	return layout.RobotsPrivate
}

// Published reports whether this document carries its finished text.
//
// IT IS DEFINED AS "the list of things it is waiting for is empty". That is the
// only definition that cannot be satisfied by editing a flag: to publish a
// document somebody has to remove the facts it needs, which means having them.
func (p LegalPage) Published() bool { return len(p.Needs) == 0 }
