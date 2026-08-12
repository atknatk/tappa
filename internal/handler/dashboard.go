package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/web/templates/pages"
)

// The PANEL SHELL — M6-02. One layout over every section; the sections themselves
// are M6-03 (shipped), M6-04 (shipped), M6-05, M6-06, M6-07 and M6-09.
//
// 🔴 WHY THIS IS A METHOD ON AdminAuth RATHER THAN ITS OWN TYPE, because "the
// dashboard is not authentication" is the obvious objection and it was weighed.
// The shell needs exactly two things and AdminAuth already owns both:
//
//	a.render     which sets adminCSP, Cache-Control: no-store and nosniff. A second
//	             type would either duplicate that header block — two CSPs to keep
//	             in step, the "second representation" failure class this repo has
//	             paid for three times — or need render exported, which makes the
//	             headers optional for whoever calls it next.
//	the identity from httpx.AdminOf, which the Protect chain has already resolved.
//
// ✅ M6-03 BROUGHT THE STORE, AND THE PREDICTION ABOVE WAS RE-WEIGHED RATHER THAN
// FOLLOWED. This comment used to end "when M6-03 brings a store ... the section
// handlers should move to their own type with the store as a field". The store
// arrived (internal/domain/ledger, reached through AdminAuth.ledger) and the split
// was NOT made, because the two reasons in the paragraph above did not change: the
// transactions handlers still need a.render — which is the ONE place the panel's
// policy, cache header and nosniff are set, and which now chooses between two
// policies — and they still need the identity the Protect chain resolved. A second
// type could reach neither without either exporting render (making the headers
// optional for the next caller) or copying the header block (two policies to keep
// in step). The section handlers live in transactions.go; only the receiver is
// shared.
//
// EVERY ROUTE IS REGISTERED BY RANGING OVER pages.PanelSections, so the navigation
// and the routing table cannot disagree. A tab whose link 404s is not a bug that
// can be introduced here — it would need a section removed from the slice that
// renders the nav, which removes the link in the same edit.

// mountSections registers one GET per panel section. It is called from INSIDE
// Mount's protected group, so every route inherits the whole Protect chain
// (address shield -> identity -> session budget) rather than restating it.
//
// 🔴 M6-03 GAVE ONE SECTION A BODY, AND THE RANGE IS STILL THE ROUTING TABLE. The
// loop below has one branch in it now, and the property it existed for is
// untouched: every row of PanelSections gets a route, so a tab whose link 404s is
// still not a thing that can be introduced here. What changed is WHICH handler a
// row is bound to, not whether it is bound.
//
// THE FRAGMENT ROUTE IS REGISTERED IN THE SAME PLACE, deliberately. It is not a
// section — it has no tab and appears in no navigation — but it reads the same
// records, so it must inherit the same chain. Registering it here rather than in
// Mount is what makes that structural: anything added to this function is inside
// the protected group by construction.
func (a *AdminAuth) mountSections(r chi.Router) {
	for _, s := range pages.PanelSections {
		switch s.Tab {
		case pages.TabTransactions:
			r.Get(s.Href, a.transactionsSection)
		case pages.TabReview:
			r.Get(s.Href, a.reviewSection)
		case pages.TabEmployees:
			r.Get(s.Href, a.employeesSection)
		case pages.TabLocations:
			r.Get(s.Href, a.locationsSection)
		case pages.TabReports:
			r.Get(s.Href, a.reportsSection)
		case pages.TabAnomalies:
			r.Get(s.Href, a.anomaliesSection)
		case pages.TabPolicies:
			r.Get(s.Href, a.policiesSection)
		default:
			r.Get(s.Href, a.section(s.Tab))
		}
	}
	r.Get(docketFragmentPath, a.transactionDockets)
	// 🔴 THE CSV EXPORT IS REGISTERED HERE FOR THE SAME REASON THE FRAGMENT IS, AND
	// ITS ONE SIDE EFFECT WAS WEIGHED RATHER THAN OVERLOOKED. It is not a section —
	// it has no tab — but it reads the same week the reports section does, so it must
	// inherit the same chain, and putting it inside this function makes that
	// structural. It DOES write one audit_log row, which is the only thing in
	// mountSections that writes anything; reportsExport carries the measurement of
	// what a GET with that side effect leaves open and what bounds it.
	r.Get(reportsCSVHref, a.reportsExport)
	// 🔴 THE MANUAL-ENTRY FORM IS A GET AND IT BELONGS HERE; ITS TWO POSTs DO NOT
	// (M6-08). This route reads one employee and the tenant's clock and renders a
	// form — no record, no audit row, no confirmation minted, so a crawler following
	// the roster's link to it changes nothing. The POST that mints the confirmation
	// and the POST that appends the record are in mountWriting, because both need the
	// Origin check AHEAD of the resolver and neither can get it here.
	r.Get(manualEntryHref, a.manualEntryForm)
	// 🔴 ONE STORED POLICY VERSION, READ IN FULL (M6-09 phase B). It is a GET and it
	// belongs here: it reads one row and renders it — no policy row written, no
	// baseline materialised, no confirmation minted — so a crawler following the link
	// from a rule's history strip changes nothing. The two POSTs that change a
	// rulebook are in mountWriting, because both need the Origin check AHEAD of the
	// resolver and neither can get it here.
	r.Get(policyVersionHref, a.policyVersionPage)
}

// mountWriting registers the panel's state-changing routes: the review decision
// (M6-04), the employees section's (M6-05 phase B), the locations section's saves,
// removals and plaque acts (M6-06), and manual record entry (M6-08).
//
// 🔴 NO COUNT APPEARS IN THIS COMMENT ANY MORE, AND THAT IS THE RULE RATHER THAN A
// TIDY-UP. The numbers here went stale THREE times — "eight and four" before phase B,
// then "eleven" until M6-08 added two, and the opening sentence's own breakdown each
// time with them. A bare integer describing a set the code below OWNS is a second
// representation, and this one proved it by drifting on every single change. The
// function body is the list; read it there.
//
// 🔴 THEY SHARE ONE CHAIN AND THAT IS THE POINT OF THE FUNCTION. Every mutating
// panel route needs the Origin check ahead of the resolver, and a route registered
// anywhere else would silently get the READ chain instead — which is exactly the
// defect an audit measured on POST /admin/review (a cross-origin flood spending a
// signed-in manager's session budget, 301 unbudgeted lookups). Adding a route here
// inherits the order; adding one outside is a visible edit somewhere else.
//
// 🔴 IT IS A SEPARATE FUNCTION FROM mountSections BECAUSE IT NEEDS A DIFFERENT
// CHAIN, NOT A LONGER ONE. mountSections is called from inside Mount's protected
// group and everything in it inherits Protect(); a mutating route needs the Origin
// check to run BEFORE the resolver, which cannot be expressed by adding middleware
// inside that group — anything added there runs after. AdminAuth.ProtectWriting
// carries the whole chain and the argument for its order.
//
// WHAT THE GATE IS AND IS NOT. It refuses a cross-origin request before any
// database work happens, which is what a state-changing endpoint behind a cookie
// needs and a GET does not: every panel read is idempotent, so a cross-origin GET
// buys an attacker nothing their own browser would not. It is defence in depth
// rather than a bound — a caller who is not a browser sets an Origin header
// trivially — and it does not pretend to stop somebody who already holds the
// session cookie, who could fetch the form and read a synchronizer token too.
func (a *AdminAuth) mountWriting(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(a.ProtectWriting())
		r.Post(reviewHref, a.reviewDecision)
		r.Post(employeeInviteHref, a.employeeInvite)
		r.Post(employeeDeactivateHref, a.employeeDeactivate)
		r.Post(employeeMoveHref, a.employeeMove)
		r.Post(venueSaveHref, a.saveVenue)
		r.Post(departmentSaveHref, a.saveDepartment)
		// 🔴 THE TWO REMOVALS ARE POSTs AND THEY LIVE HERE, WHICH IS THE WHOLE OF T3.
		// A delete reachable by GET is one a link, a prefetch or a crawler can fire;
		// and a POST registered in mountSections would silently take the READ chain,
		// running the resolver before the Origin check.
		r.Post(venueDeleteHref, a.deleteVenue)
		r.Post(departmentDeleteHref, a.deleteDepartment)
		// 🔴 THE PLAQUE ACTS BELONG HERE FOR THE SAME REASON THE REMOVALS DO, and
		// one of them is the heaviest write in the panel: replacing a plaque retires
		// the thing every tap at that entrance is authenticated by (§5 row 1). A POST
		// registered in mountSections would silently take the READ chain, running the
		// resolver before the Origin check.
		r.Post(plaqueMountHref, a.mountPlaque)
		r.Post(plaqueReplaceHref, a.replacePlaque)
		r.Post(plaqueUnmountHref, a.unmountPlaque)
		// 🔴 THE TWO MANUAL-ENTRY POSTs (M6-08), AND THE FIRST OF THEM WRITES NO ROW.
		// It is here anyway: it mints a confirmation and sets its twin cookie, which is
		// state, and a POST registered in mountSections would silently take the READ
		// chain — running the resolver before the Origin check, which is the defect an
		// audit measured on POST /admin/review. The second appends a permanent,
		// undeletable attendance record, which is the heaviest write this panel has.
		r.Post(manualEntryHref, a.manualEntryReview)
		r.Post(manualRecordHref, a.manualEntryRecord)
		// 🔴 THE TWO POLICY POSTs (M6-09 phase B), AND THE FIRST OF THEM WRITES NO
		// POLICY ROW. It is here anyway for the reason the manual-entry review step is:
		// it mints a confirmation and sets its twin cookie, which is state — and it can
		// write an audit_log row when the engine REFUSES the actor, which is a write in
		// the one direction that matters most. The second changes a customer's rulebook,
		// which decides how every later tap in that business is judged.
		r.Post(policyChangeHref, a.policyChangeReview)
		r.Post(policyApplyHref, a.policyChangeApply)
	})
}

// transactionsHref is the transactions section's own URL, READ FROM THE SECTION
// TABLE rather than written out again.
//
// A SECOND LITERAL "/admin" WOULD BE A SECOND REPRESENTATION of the fact
// pages.PanelSections already owns — and this one has teeth, because it is what
// the filter form posts to and what every "show more" link is built from. If the
// section ever moves, the form and the paging links move with it; if the tab is
// removed altogether, this panics at startup rather than silently building links
// to nowhere.
var transactionsHref = mustSectionHref(pages.TabTransactions)

func mustSectionHref(tab pages.PanelTab) string {
	for _, s := range pages.PanelSections {
		if s.Tab == tab {
			return s.Href
		}
	}
	panic("handler: no panel section for tab " + string(tab))
}

// section serves one panel section.
//
// THE TAB IS BOUND AT MOUNT TIME, not read from the URL. A section that parsed its
// own path would be a section that can be asked for one that does not exist, and
// would need an error branch for it; here the only tabs reachable are the ones
// PanelSections named, and the compiler carries that from the table to the view.
func (a *AdminAuth) section(tab pages.PanelTab) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.render(w, r, http.StatusOK, pages.AdminDashboard(pages.AdminDashboardView{
			// a.chrome (review.go) reads the queue badge as well as the identity, so
			// an unbuilt section shows the same backlog number as a built one. It is
			// the one place that count is taken; see the cost measured there.
			PanelChrome: a.chrome(r, tab),
		}))
	}
}
