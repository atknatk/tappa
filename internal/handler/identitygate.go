package handler

import (
	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/adminauth"
)

// identitygate.go -- how a panel route binds to TAPPA_ADMIN_IDENTITY (ADR 0031 §8, M14-08).
//
// The decision is adminauth.IdentityGate's table, by row (adminauth/flag.go). This is the HTTP half,
// in one place: the routes of an identity surface (/admin/switch -- M14-10, invitations -- M14-15, ...)
// are mounted through a.mountGated(r, <its Surface>, func(r chi.Router) { ... }), and while that row is
// closed they are NOT MOUNTED AT ALL. The flag is read once, at start-up (config.Load), so nothing
// needs deciding per request -- and an unmounted route is the only thing that answers exactly what a
// path the panel never had answers, for EVERY method: a route bound with r.Get / r.Post and refused
// at request time still made the router answer HEAD, PUT, DELETE, OPTIONS and PATCH with 405 and an
// Allow header (M14-08's audit, measured) -- the closed surface's existence, leaked. So a closed
// surface cannot be told from a missing one by anybody: not by a signed-in session (linked or not --
// R4), not by a signed-out visitor, not by any method.
//
// TestMountGated_AClosedSurfaceIsAPathThePanelNeverHad holds the bytes to the router's, method by
// method.

// mountGated mounts the routes mount registers when s is open under the panel's identity flag, and
// nothing at all when it is closed. mount receives r itself (no sub-router, no path prefix): a
// surface's routes keep their full /admin/... paths and their chains, exactly as an ungated mount.
func (a *AdminAuth) mountGated(r chi.Router, s adminauth.Surface, mount func(chi.Router)) {
	if !a.identityGate.Open(s) {
		return
	}
	mount(r)
}
