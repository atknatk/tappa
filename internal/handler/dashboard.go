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
// It owns no store, no query and no domain call, so there is nothing here for a
// separate type to hold. WHEN M6-03 BRINGS A STORE that stops being true, and the
// section handlers should move to their own type with the store as a field; the
// shell (this file's renderSection) can stay.
//
// EVERY ROUTE IS REGISTERED BY RANGING OVER pages.PanelSections, so the navigation
// and the routing table cannot disagree. A tab whose link 404s is not a bug that
// can be introduced here — it would need a section removed from the slice that
// renders the nav, which removes the link in the same edit.

// mountSections registers one GET per panel section. It is called from INSIDE
// Mount's protected group, so every route inherits the whole Protect chain
// (address shield -> identity -> session budget) rather than restating it.
func (a *AdminAuth) mountSections(r chi.Router) {
	for _, s := range pages.PanelSections {
		r.Get(s.Href, a.section(s.Tab))
	}
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
			FullName: id.Admin.FullName,
			Role:     id.Admin.Role,
			Tab:      tab,
		}))
	}
}
