package handler

import (
	"net/http"

	"github.com/atknatk/tappa/internal/adminauth"
)

// identitygate.go -- how a panel route binds to TAPPA_ADMIN_IDENTITY (ADR 0031 §8, M14-08).
//
// The decision is adminauth.IdentityGate's table, by row (adminauth/flag.go). This is the HTTP half,
// in one place: a route of an identity surface (/admin/switch -- M14-10, invitations -- M14-15, ...)
// is mounted as a.gated(<its Surface>, <its whole chain>), and while that row is closed the request
// answers EXACTLY what a path the panel never had answers -- the router's own 404, before any gate of
// the chain runs. So a closed surface cannot be told from a missing one by anybody: not by a signed-in
// session (linked or not -- R4), not by a signed-out visitor, not by a forged POST (the chain's
// flood, origin and session gates would each answer something of their own).
//
// TestGated_AClosedRouteIsAPathThePanelNeverHad holds the bytes to the router's: the day the panel's
// router gets a NotFound of its own, that test is red until this follows it.

// gated is h while s is open under the panel's identity flag, and the 404 of an unmounted path while
// it is closed. It wraps the WHOLE chain -- mount it outermost.
func (a *AdminAuth) gated(s adminauth.Surface, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.identityGate.Open(s) {
			http.NotFound(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
}
