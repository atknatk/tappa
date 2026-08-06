package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The PANEL SHELL — M6-02. Five sections, one layout, and nothing in any of them
// yet: the sections themselves are M6-03, M6-05, M6-06, M6-07 and M6-09.
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
		if s.Tab == pages.TabTransactions {
			r.Get(s.Href, a.transactionsSection)
			continue
		}
		r.Get(s.Href, a.section(s.Tab))
	}
	r.Get(docketFragmentPath, a.transactionDockets)
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
		id := httpx.AdminOf(r)
		a.render(w, r, http.StatusOK, pages.AdminDashboard(pages.AdminDashboardView{
			PanelChrome: pages.PanelChrome{
				FullName: id.Admin.FullName,
				Role:     id.Admin.Role,
				Tab:      tab,
			},
		}))
	}
}
