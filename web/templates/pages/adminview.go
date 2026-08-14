package pages

import "strconv"

// itoa is strconv.Itoa under a shorter name, used by the badge helpers below. It
// is here rather than inline so the two helpers read as sentences.
func itoa(n int) string { return strconv.Itoa(n) }

// The PANEL screens' view models (M6-01 phase B).
//
// SAME RULE AS THE REST OF THIS PACKAGE, and here it does more work than usual:
// a template cannot accidentally render a password, a digest or a session token,
// because no view below has a field one could travel in. The login form takes an
// email and a password from the visitor and gives NEITHER back — not even the
// email, which is why a failed sign-in clears the box.
//
// ⚠️ THE EMAIL IS DELIBERATELY NOT ECHOED, and it is worth saying why, since
// re-filling the field is the friendlier thing to do. Two reasons, in order of
// weight: a reflected value is a value that has to be escaped correctly forever,
// on the one form in the product that an unauthenticated stranger can post to; and
// an operator who mistyped their address should retype it rather than be shown a
// form that looks right and is not. The cost is one extra field to type after a
// failed attempt, on a screen people see once a day.

// AdminLoginView is the panel sign-in form.
//
// TWO FIELDS, AND THE ABSENCE OF A THIRD IS THE SPEC. There is no Error string,
// no Reason and no ErrorKind: migration 00011's PHASE B OBLIGATION 1 requires an
// unknown email, a wrong password and a disabled account to be indistinguishable,
// and a view model with a message field is how they stop being. Failed is a BOOL —
// it can say THAT something failed and has no way to say WHAT.
type AdminLoginView struct {
	// CSRFToken is the synchronizer token echoed by the form. It is rendered ON
	// PURPOSE and is not a section 4.7 secret: it exists precisely so that a page
	// an attacker cannot read is the only page that can produce a valid submission.
	CSRFToken string
	// Failed re-renders the form after a refused sign-in, with one fixed sentence
	// that names no cause.
	Failed bool

	// ResetHref points at the recovery form (M7-04 phase B). It is passed in rather
	// than written into the template so the route and the link are the same string
	// at compile time — the reason internal/handler's adminLoginPath became a
	// constant, applied in the other direction.
	//
	// ⚠️ ITS ABSENCE HIDES THE LINK, and that is a THIRD state rather than a bug:
	// a deployment that does not mount the recovery routes must not put a dead
	// button on the one screen an operator reaches when they are already stuck.
	ResetHref string

	// Recovered says the visitor has just set a new password and needs to sign in
	// again (internal/handler's adminResetDoneQuery).
	//
	// 🔴 IT IS NOT A CLAIM ABOUT THE VISITOR AND CANNOT BE ONE. Anybody can put that
	// query on this URL; all it does is put one sentence on the page. The reason it
	// exists is that the alternative — landing on a bare form after a completed
	// recovery — leaves somebody unsure whether it worked, on the screen where
	// guessing wrong spends an attempt budget.
	Recovered bool
}

// AdminBusiness is one row of the "which business?" picker.
//
// IT CARRIES A TENANT ID AND THAT IS SAFE HERE, which is worth stating because
// ADR 0002 madde 7 spends a page on why a tenant must never be an INPUT. It is not
// one: the id posted back is checked for membership of the server-signed,
// password-verified set before it authorises anything (internal/handler's
// selectVerified). What is rendered is an opaque database id the operator has
// already authenticated against — not a claim the server will believe.
type AdminBusiness struct {
	TenantID   string
	TenantName string
	// Role is 'owner' or 'manager' — this operator's role IN THIS BUSINESS. Shown
	// because the same person can be an owner of one and a manager of another, and
	// that is exactly the situation the picker exists for.
	Role string
}

// AdminChooseView is the picker: which of the businesses this password verified
// against should this session belong to.
//
// 🔴 EVERY ROW HERE HAS ALREADY VERIFIED — and it is worth being exact about HOW,
// because an earlier version of this comment was not.
//
// It said the handler builds Businesses from adminauth.Authentication.Verified().
// That is true of the FIRST step only. On the picker's own request the identities
// come back from adminChoices.parse — the signed cookie — and internal/adminauth's
// Verified type carries an explicit warning against restating this as "nothing else
// can build one", because an audit refuted exactly that wording. Round 2 corrected
// the sentence in manager.go and did not sweep the file that RENDERS the picker.
//
// THE GUARANTEE, in the two legs it actually stands on (PHASE B OBLIGATION 5):
//  1. only a PASSWORD COMPARISON can create an identity, and Verified() ANDs
//     "matched" with "active" in one place;
//  2. the picker's set is reconstructed from a payload THIS DEPLOYMENT signed
//     (HMAC-SHA256), and POST /admin/login/choose re-proves membership of it
//     (internal/handler's selectVerified) before anything is issued.
//
// A view model can enforce neither; this comment only points at where they live.
type AdminChooseView struct {
	CSRFToken  string
	Businesses []AdminBusiness
}

// adminRoleOwner is the role a PanelSection's OwnerOnly flag is compared against.
//
// ⚠️ IT IS A SECOND SPELLING OF A WORD internal/handler ALSO SPELLS, AND THAT IS
// STATED RATHER THAN SMUGGLED. The authority is the SCHEMA — migration 00006's
// CHECK (role IN ('owner','manager')) — and this package cannot import internal/handler
// (the dependency runs the other way), so the choice was a constant here or a bool
// computed by every handler and passed in. The constant is narrower: a section's
// visibility rule lives beside the section table it applies to, and this value is
// compared against PanelChrome.Role, which internal/adminauth filled from the
// admin_users row during session resolution — never from a form.
const adminRoleOwner = "owner"

// PanelTab identifies one section of the dashboard.
//
// IT IS A NAMED TYPE RATHER THAN A string so a handler cannot pass a section that
// does not exist by mistyping it — the compiler will take an untyped constant, but
// every call site in the product reads from PanelSections below and therefore
// cannot invent one at all.
type PanelTab string

const (
	TabTransactions PanelTab = "transactions"
	TabReview       PanelTab = "review"
	TabEmployees    PanelTab = "employees"
	TabLocations    PanelTab = "locations"
	TabReports      PanelTab = "reports"
	TabAnomalies    PanelTab = "anomalies"
	TabPolicies     PanelTab = "policies"
	TabBilling      PanelTab = "billing"
	// TabLegal is the ONLY section that is not about a customer's business (M7-06):
	// it publishes TAPPA's own privacy policy, terms, company details and cookie
	// notice. See PanelSection.OperatorOnly for why it is a tab here rather than a
	// separate application.
	TabLegal PanelTab = "legal"
)

// PanelSection is one row of the panel's navigation AND one route.
//
// 🔴 THE ROUTE AND THE TAB ARE THE SAME FACT, which is why Href lives here rather
// than in the router. internal/handler mounts by RANGING over PanelSections, so a
// section added to this slice is a section that is navigable, and a section
// removed from it is a route that stops existing. The failure this shape makes
// unavailable is a tab whose link 404s — the shape M6-03..M6-09 would otherwise
// each have a chance to produce.
type PanelSection struct {
	Tab   PanelTab
	Label string
	Href  string
	// Task is the roadmap task that fills this section. It is rendered, on
	// purpose: until the section has content, the honest thing for it to say is
	// which piece of work is missing rather than a blank panel that looks broken.
	Task string
	// Blurb is what will be here, in the brand's voice. It is NOT a promise the
	// system already keeps — the empty state says "not built yet" beside it.
	Blurb string
	// OwnerOnly hides this section's TAB from a manager (M6-12).
	//
	// 🔴 IT CHANGES WHAT IS DRAWN AND NOT WHAT IS ROUTED, WHICH IS THE WHOLE OF THE
	// DESIGN. internal/handler mounts by RANGING over the full PanelSections, so every
	// row still gets a route and "a tab whose link 404s" stays structurally
	// unavailable — the property this table exists for. What this flag removes is the
	// LINK, and the server refuses the route on its own (billingactions.go's
	// mayManageBilling). Both halves are required and neither substitutes for the
	// other, which is the argument locationactions.go's mayRemove already makes for
	// the panel's other role gate.
	//
	// ⚠️ THE ALTERNATIVE WAS WEIGHED: leave the tab and let the section explain
	// itself. It was rejected because a tab a manager can see and cannot open is
	// "clickable but forbidden" — it teaches somebody a control exists, costs them a
	// page load to find out it does not apply to them, and does it on every visit. The
	// refusal page still exists for anybody who arrives by a shared link or a stale
	// bookmark, and §4.6 makes it say why rather than 404.
	OwnerOnly bool

	// OperatorOnly hides this section's TAB from everybody who is not on the
	// deployment's operator allow-list (M7-06).
	//
	// 🔴 IT IS A SECOND FLAG RATHER THAN A WIDENING OF OwnerOnly, BECAUSE IT ASKS A
	// DIFFERENT QUESTION. OwnerOnly asks "which role within THIS BUSINESS" —
	// admin_users.role, a per-tenant fact, and every tenant has an owner.
	// OperatorOnly asks "is this person one of the handful who run the deployment",
	// which no column answers and no tenant grants: it comes from
	// TAPPA_OPERATOR_ADMIN_IDS, an allow-list of admin_users.id values. Folding them
	// into one bool would have made "the owner of
	// any business" and "the person who runs Tappa" the same permission, which is the
	// widest possible reading of the narrowest possible flag.
	//
	// 🔴 AND IT CHANGES WHAT IS DRAWN, NOT WHAT IS ROUTED — the same design OwnerOnly
	// documents above, and for the same reason: the route stays mounted so a tab
	// whose link 404s remains structurally unavailable, and the handler refuses on
	// its own (legaladmin.go's mayPublishLegal). Hiding the link is the courtesy
	// half; the server refusing the route is the guarantee, and neither substitutes
	// for the other.
	OperatorOnly bool
}

// PanelSections is the panel's sections, in the order CLAUDE.md §9 and
// docs/handoff.md name them.
//
// TRANSACTIONS IS /admin RATHER THAN /admin/transactions, and that is a decision
// rather than an accident: /admin is where sign-in lands (completeLogin), so
// giving the default section its own second URL would mean either a redirect on
// every sign-in or two URLs rendering the same page. The tab's link points at
// /admin and there is exactly one URL per section.
//
// 🔴 M6-04 ADDED A SIXTH, AND IT IS A SECTION RATHER THAN A CONTROL ON THE FIRST
// ONE. The transactions list is a READ of a day; the review queue is a WORKLIST
// that spans every day and shrinks as it is worked. Putting approve buttons on the
// day view would have meant one screen that is read-only for most of its rows and
// writable for some of them, with the §4.3 boundary invisible in the markup. Two
// URLs keep it visible: /admin has no form that posts, /admin/review has nothing
// else.
var PanelSections = []PanelSection{
	{
		Tab: TabTransactions, Label: "Transactions", Href: "/admin", Task: "M6-03",
		Blurb: "Every tap of the day as a docket card — who, where, in or out, the trust score, and the stamp that says how it was judged.",
	},
	{
		Tab: TabReview, Label: "Review queue", Href: "/admin/review", Task: "M6-04",
		Blurb: "The taps the engine could not judge on evidence alone, waiting for somebody who was there to say yes or no.",
	},
	{
		Tab: TabEmployees, Label: "Employees", Href: "/admin/employees", Task: "M6-05",
		Blurb: "The people who tap: who is active, who has been invited, and who has stopped working here.",
	},
	{
		Tab: TabLocations, Label: "Locations & Wall Tags", Href: "/admin/locations", Task: "M6-06",
		Blurb: "Your venues and the plaque on each wall — which tag is mounted where, and which one needs replacing.",
	},
	{
		Tab: TabReports, Label: "Reports", Href: "/admin/reports", Task: "M6-07",
		Blurb: "Hours worked per person per week, and the CSV your accountant asks for.",
	},
	{
		// 🔴 IT SITS AFTER REPORTS AND BEFORE POLICIES, WHICH IS WHERE ITS READER IS.
		// This section is a second reading of the same week the reports section totals
		// — a manager who has just noticed a short total is one click from "why" — and
		// it is emphatically NOT a rule screen: it changes nothing and decides nothing.
		Tab: TabAnomalies, Label: "Anomalies", Href: "/admin/anomalies", Task: "M6-11",
		Blurb: "The week's odd shapes — GPS-only taps, plaque counter gaps, people who always tap together, and check-ins nobody closed.",
	},
	{
		Tab: TabPolicies, Label: "Policies", Href: "/admin/policies", Task: "M6-09",
		Blurb: "The rules this business is allowed to change — shown beside the ones no business can.",
	},
	{
		// 🔴 IT IS LAST, AND THE ORDER IS AN ARGUMENT RATHER THAN AN APPEND. Every one
		// of the seven rows above is about the BUSINESS's own operations — its taps, its
		// queue, its people, its places, its hours, its odd shapes, its rules. This one
		// is the only row about the arrangement BETWEEN the business and Tappa, and it is
		// read once a month rather than once a shift. Dropping it between two operational
		// sections would break a run a manager navigates by habit; last keeps that run
		// intact and puts the once-a-month screen where a once-a-month screen belongs.
		//
		// ⚠️ IT IS NOT PLACED HERE BECAUSE IT IS "THE OWNER'S TAB". Its READ is every
		// admin's — a manager may look at what the month costs — and only the one
		// irreversible control on it is owner-only (billingactions.go's mayCloseBilling).
		// Ordering by who may WRITE would have put Policies here too, and Policies is
		// sixth.
		Tab: TabBilling, Label: "Billing", Href: "/admin/billing", Task: "M6-12",
		Blurb:     "What each month costs: how many people were on the books, at what price, and the figure you can freeze once the month has ended.",
		OwnerOnly: true,
	},
	{
		// 🔴 IT IS AFTER BILLING BECAUSE IT IS NOT THIS BUSINESS'S AT ALL. Billing is
		// the last row about the arrangement between the customer and Tappa; this one
		// is about TAPPA, and almost nobody signed into this panel will ever see it.
		// Putting it anywhere among the operational sections would suggest a customer
		// has something to do here.
		Tab: TabLegal, Label: "Tappa legal texts", Href: "/admin/legal", Task: "M7-06",
		Blurb: "The privacy policy, terms, company details and cookie notice that /legal publishes — Tappa's own documents, not this business's.",
		// The route is mounted for everybody and the handler refuses everybody who is
		// not on the allow-list; this only decides whether the link is drawn.
		OperatorOnly: true,
	},
}

// PanelChrome is everything the panel draws AROUND a section: who is signed in,
// and which section they are looking at.
//
// 🔴 IT WAS EXTRACTED IN M6-03 AND THE REASON IS THE FAILURE CLASS THIS REPO KEEPS
// PAYING FOR. Until M6-03 there was one panel page, so the wordmark, the "signed
// in as" block, the sign-out form and the tab bar lived inside it. M6-03 gives ONE
// of the sections real content, and the obvious move — a second page template
// with the same chrome pasted above it — would have made the navigation, the
// sign-out button and the brand lockup a SECOND REPRESENTATION, i.e. two places
// for them to drift, with the drift invisible (a page missing sign-out looks
// exactly like a page that has it). One shell renders both; only the body differs.
//
// Tab is a PanelTab, so the template can only be pointed at a section that exists.
type PanelChrome struct {
	FullName string
	Role     string
	Tab      PanelTab

	// Operator is true when this session's admin id is on the deployment's operator
	// allow-list (M7-06). It decides whether the "Tappa legal texts" link is drawn and
	// NOTHING else — the handler asks the same question again for itself.
	//
	// 🔴 IT IS A BOOL AND NOT AN IDENTIFIER. This package renders whatever it is given
	// into HTML, so carrying the id here would put it one careless `{ c.OperatorID }`
	// away from every panel page. The answer is computed in internal/handler and only
	// the answer travels.
	Operator bool

	// Pending is the approval queue's size, shown as a badge on the review tab so
	// a manager sees the backlog from whichever section they are in (M6-04).
	//
	// 🔴 THE THREE-WAY SHAPE IS §4.6 AND NOT PEDANTRY. "Nothing is waiting" and "we
	// could not count" are different facts, and a plain int makes them the same
	// number. Known is false when the count query failed, and the badge then says
	// so instead of saying zero — the class M5-11 closed: the product may not state
	// something it has not measured.
	Pending PendingBadge
}

// PendingBadge is what the navigation knows about the approval queue.
type PendingBadge struct {
	// Known is true only after the database has answered.
	Known bool
	// N is the count, exact when Capped is false and a FLOOR when it is true.
	N int
	// Capped means the count stopped at its cap, so the badge reads "N+".
	// internal/domain/ledger.PendingCap says why an exact count is not taken.
	Capped bool
}

// Label is what the badge prints: "" when nothing is waiting, "?" when the count
// could not be taken, "12" or "100+" otherwise.
//
// THE EMPTY STRING IS "NOTHING WAITING", WHICH IS A MEASURED CLAIM, and it is only
// reachable through Known. An unknown count renders "?" rather than disappearing,
// because a badge that vanishes on a failed read tells the manager the queue is
// clear.
func (p PendingBadge) Label() string {
	switch {
	case !p.Known:
		return "?"
	case p.N <= 0:
		return ""
	case p.Capped:
		return itoa(p.N) + "+"
	default:
		return itoa(p.N)
	}
}

// Describe is the badge's accessible text — the sentence a screen reader gets
// instead of a bare number, since the number alone says nothing about what it
// counts.
func (p PendingBadge) Describe() string {
	switch {
	case !p.Known:
		return "the number waiting for review could not be read"
	case p.N <= 0:
		return ""
	case p.Capped:
		return "more than " + itoa(p.N) + " records waiting for review"
	case p.N == 1:
		return "1 record waiting for review"
	default:
		return itoa(p.N) + " records waiting for review"
	}
}

// Section returns the section this view is showing.
//
// IT RETURNS ok=false FOR AN UNKNOWN TAB RATHER THAN A ZERO SECTION. A zero
// PanelSection would render a nameless tab with an empty href — a link to the
// current page, which looks like a working control and is not. The template
// renders nothing at all instead, and the handler cannot reach that branch because
// it only ever passes a tab it read from PanelSections.
func (c PanelChrome) Section() (PanelSection, bool) {
	for _, s := range PanelSections {
		if s.Tab == c.Tab {
			return s, true
		}
	}
	return PanelSection{}, false
}

// SectionHref is one section's URL, READ FROM THE SECTION TABLE.
//
// IT EXISTS SO A TEMPLATE CAN SEND A READER TO ANOTHER SECTION WITHOUT TYPING ITS
// PATH. A literal "/admin/locations" in a .templ file is a second representation of
// something PanelSections already owns, and the copy does not move when the section
// does — it becomes a link to nothing, which looks like a working control.
//
// AN UNKNOWN TAB YIELDS "", NOT A GUESS, and callers are expected to render no link
// at all in that case (M7-01's rule: a dead href is worse than a missing button).
func SectionHref(tab PanelTab) string {
	for _, s := range PanelSections {
		if s.Tab == tab {
			return s.Href
		}
	}
	return ""
}

// SectionLabel is one section's tab label, read from the same table SectionHref
// reads. A sentence that names another section must name it the way the navigation
// does, or the reader is sent looking for a tab that is not there.
func SectionLabel(tab PanelTab) string {
	for _, s := range PanelSections {
		if s.Tab == tab {
			return s.Label
		}
	}
	return ""
}

// CurrentHref is the href TabBar compares against. An unknown tab yields "",
// which marks no tab current rather than marking the first one — see Section().
func (c PanelChrome) CurrentHref() string {
	s, ok := c.Section()
	if !ok {
		return ""
	}
	return s.Href
}

// AdminDashboardView is a section that has NO CONTENT YET: the chrome, and an
// empty state naming the task that fills it.
//
// 🔴 IT STILL CARRIES NO SECTION DATA, and after M6-03 that is a sharper statement
// than it was. Four of the SIX sections are still empty and this is the view they
// render; Transactions has its own view model (TransactionsView) with its own
// fields. So "a view model with a data field is how a skeleton quietly becomes a
// half-built dashboard" is enforced by there being no data field HERE — a section
// cannot half-fill itself, it has to be given a view of its own, which is an edit
// somebody makes on purpose.
type AdminDashboardView struct {
	PanelChrome
}
