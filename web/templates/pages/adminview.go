package pages

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

// PanelTab identifies one section of the dashboard.
//
// IT IS A NAMED TYPE RATHER THAN A string so a handler cannot pass a section that
// does not exist by mistyping it — the compiler will take an untyped constant, but
// every call site in the product reads from PanelSections below and therefore
// cannot invent one at all.
type PanelTab string

const (
	TabTransactions PanelTab = "transactions"
	TabEmployees    PanelTab = "employees"
	TabLocations    PanelTab = "locations"
	TabReports      PanelTab = "reports"
	TabPolicies     PanelTab = "policies"
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
}

// PanelSections is the panel's five sections, in the order CLAUDE.md §9 and
// docs/handoff.md name them.
//
// TRANSACTIONS IS /admin RATHER THAN /admin/transactions, and that is a decision
// rather than an accident: /admin is where sign-in lands (completeLogin), so
// giving the default section its own second URL would mean either a redirect on
// every sign-in or two URLs rendering the same page. The tab's link points at
// /admin and there is exactly one URL per section.
var PanelSections = []PanelSection{
	{
		Tab: TabTransactions, Label: "Transactions", Href: "/admin", Task: "M6-03",
		Blurb: "Every tap of the day as a docket card — who, where, in or out, the trust score, and the stamp that says how it was judged.",
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
		Tab: TabPolicies, Label: "Policies", Href: "/admin/policies", Task: "M6-09",
		Blurb: "The rules this business is allowed to change — shown beside the ones no business can.",
	},
}

// PanelChrome is everything the panel draws AROUND a section: who is signed in,
// and which section they are looking at.
//
// 🔴 IT WAS EXTRACTED IN M6-03 AND THE REASON IS THE FAILURE CLASS THIS REPO KEEPS
// PAYING FOR. Until M6-03 there was one panel page, so the wordmark, the "signed
// in as" block, the sign-out form and the tab bar lived inside it. M6-03 gives ONE
// of the five sections real content, and the obvious move — a second page template
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
// than it was. Four of the five sections are still empty and this is the view they
// render; Transactions has its own view model (TransactionsView) with its own
// fields. So "a view model with a data field is how a skeleton quietly becomes a
// half-built dashboard" is enforced by there being no data field HERE — a section
// cannot half-fill itself, it has to be given a view of its own, which is an edit
// somebody makes on purpose.
type AdminDashboardView struct {
	PanelChrome
}
