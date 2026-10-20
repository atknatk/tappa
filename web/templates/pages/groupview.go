package pages

// GroupView is the Group section (GRP-1; ADR 0027's GRP-1 note): the holding this business is a live
// member of, read only.
//
// 🔴 THE FIELDS THAT ARE NOT HERE ARE THE SECTION'S BOUNDARY (K-1d, K-1f, K-1h, K-39). A member is a
// name, a company code and a date -- there is no field for another business's VAT number, address,
// e-mail, headcount, plan or API switch, nor for a member that has left. A partner's record is the
// reports section's ReportPartnerView: who, when, where, which way and the RESULT -- no reason, no
// evidence, no hours. And nothing here posts: there is no form, because the holding is run from the
// operator's side.
type GroupView struct {
	PanelChrome
	// Queried is set only after the database answered (§4.6).
	Queried bool
	// Member is false for a business in no holding (or whose membership has ended): the page then
	// says so and shows nothing else.
	Member bool
	Groups []GroupHoldingView
	// WindowLabel names the period of the two record lists and the clock its dates are in.
	WindowLabel string
	// Partners and PartnerManual are a partner business's staff's records at this business's plaques
	// and venues in the window, the latest first (PartnerManual: typed by their employer).
	Partners      []ReportPartnerView
	PartnerManual []ReportPartnerView
	// PartnersMore says the window held more records than one page lists.
	PartnersMore bool
	// ReportsHref is the Reports section, which lists the same records week by week.
	ReportsHref string
	// AtPartnerVenues is how many of this business's own records in the window name a partner's
	// venue -- a figure (mono), not a list: they are this business's own records, in Transactions.
	AtPartnerVenues string
}

// GroupHoldingView is one holding: its name, this business's start in it, its live members.
type GroupHoldingView struct {
	Name    string
	Since   string
	Members []GroupMemberView
}

// GroupMemberView is one live member: a name, a company code ("" when it has none) and the day it
// joined; Self marks this business.
type GroupMemberView struct {
	Name   string
	Code   string
	Joined string
	Self   bool
}
