package handler

import (
	"net/http"
	"strings"

	"github.com/atknatk/tappa/web/templates/pages"
)

// THE MINTING GETs' FETCH-METADATA GATE (backlog T14, T19).
//
// Five panel acts are confirmed with a value minted on a GET (deactivateconfirm.go):
// deactivating an employee (?confirm=deactivate on the roster; since T59 also signing their
// phones out, ?confirm=signout), removing a venue or a
// department (?confirm=remove), and replacing or taking down a plaque (?plaque=<uid>,
// ?confirm=unmount). The two exports -- hours (reportsCSVHref) and invoice
// (billingCSVHref) -- each write an append-only row, report.exported and
// billing.exported (§4.3: never taken back). None of them is a POST, so
// none of them is behind sameOriginGate, and a page on another site could drive a
// signed-in manager's tab there with one top-level navigation: SameSite=Lax sends the
// panel cookie on exactly that. What it bought before this gate: a "Remove X?" warning
// the manager did not ask for (the value itself is useless to the attacker -- the POST
// that spends it is Origin-gated), and a trail row the manager did not cause.
//
// 🔴 THE GATE READS Sec-Fetch-Site AND NOTHING ELSE, because a navigation carries no
// Origin. It is an ALLOW-LIST:
//
//	same-origin   one of our own pages linked here -- every legitimate way in
//	none          typed, bookmarked, opened from history: the user did it
//	(absent)      a browser that sends no fetch metadata, or curl
//	anything else refused: cross-site, same-site, and a value no browser sends
//
// WHY ABSENT PASSES. Refusing it would turn "older browser" into "cannot download the
// payroll" (reportsExport records why the export stayed a GET), and on a browser that
// sends no metadata the gate could never have told a bookmark from an attack anyway.
// A browser cannot forge the header (Sec- is a forbidden request-header prefix), so a
// browser that sends it is believed; a non-browser that sends anything already holds
// the cookie and needs no navigation trick. Activation's crossSite makes the same call.
//
// WHY SAME-SITE IS REFUSED. "same-site" is a DIFFERENT ORIGIN of the same site:
// ops.taptime.mt, api.taptime.mt and www.taptime.mt beside the panel's taptime.mt
// (deploy/k8s/4*-ingress.yaml). None of them links into these URLs (read: the operator
// surface's pages and templates name no /admin path), so the only same-site arrival is
// one we did not write -- a sibling with an XSS, or a subdomain taken over -- which is
// what GATE-1 (ADR 0028 §11) refuses on every panel write for the same reason.
//
// WHAT A REFUSAL DOES: the panel's problem page with 403. The handler does not run, so
// no confirmation is minted, no confirmation cookie is written and no audit row is
// written. It is NOT "nothing else": the gate sits after the Protect chain (Protect is
// the group's middleware and runs first), so the refused request has already been
// charged to floodGate's and sessionGate's budgets and the resolver has touched the
// session's admin_sessions.last_used_at -- exactly what any panel GET does. Unlike
// sameOriginGate it is not placed ahead of the resolver: a GET's resolver read costs
// the same as an allowed GET of the same page, so the order buys nothing either way.
//
// ⚠️ IT IS NOT A WHOLE-SECTION GATE. Only a request that would mint (arms reports
// true) is refused; a cross-site link to the plain roster, the plain locations page or
// a venue card still opens, which is what keeps a link pasted into a chat working.

// problemPanelElsewhere is the answer to a minting GET that another site started.
var problemPanelElsewhere = pages.ProblemView{
	Title:   "Open this from your panel",
	Message: "This link was opened from another website, so the panel did not act on it. Nothing was prepared and nothing was recorded.",
	Hint:    "Go to the same page from inside the panel and press it there.",
}

// fetchSiteClass is the request's Sec-Fetch-Site, folded to what the gate decides on:
// "" when it may pass, otherwise the fixed word the log prints (never the raw value --
// a non-browser chooses it).
func fetchSiteClass(r *http.Request) string {
	switch v := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))); v {
	case "", "none", "same-origin":
		return ""
	case "cross-site", "same-site":
		return v
	default:
		return "unrecognised"
	}
}

// mintGate refuses a request arms says would mint or record, when it was started by
// another site. One gate for every minting GET: the routes differ only in arms.
func (a *AdminAuth) mintGate(arms func(*http.Request) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if class := fetchSiteClass(r); class != "" && arms(r) {
				a.log.Warn("panel request refused: a minting GET started by another site",
					"method", r.Method, "path", r.URL.Path, "ip", clientIP(r), "fetch_site", class)
				a.renderProblem(w, r, http.StatusForbidden, problemPanelElsewhere)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// The three arms. Each is AT LEAST as wide as the mint it guards -- it reads the same
// r.URL.Query() the handler reads, and any value of the key counts, not only the one
// the handler acts on -- so a refusal can be wider than a mint, never narrower.

// armsRosterConfirmation: rosterActions mints on ?confirm=deactivate and ?confirm=signout.
func armsRosterConfirmation(r *http.Request) bool {
	return r.URL.Query().Get("confirm") != ""
}

// armsLocationsConfirmation: removalView mints on ?confirm=remove; plaqueCard mints on
// opening ?plaque=<uid> (the replacement form is armed on the card itself) and on
// ?confirm=unmount.
func armsLocationsConfirmation(r *http.Request) bool {
	q := r.URL.Query()
	return q.Get("confirm") != "" || strings.TrimSpace(q.Get("plaque")) != ""
}

// armsAlways: each export writes its trail row on every file it serves.
func armsAlways(*http.Request) bool { return true }
