package handler

import (
	"errors"
	"net/http"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// SIGN OUT EVERYWHERE (M14-11, ADR 0031 §5.2, §8).
//
// 🔴 THE PERSON, NOT THE BUSINESS. One press revokes every live session of the person behind the
// cookie -- this one included -- in EVERY business they are a member of, and lands on the sign-in
// form. adminauth.RevokeAll does the work, keyed by the cookie's token and nothing else: a tenant or
// an admin id from the request is never an input to the identity (ADR 0031 §5.2), so there is no
// field on this form at all.
//
// 🔴 ONE ORDINARY SIGN-OUT ROW PER REVOKED SESSION, AND NOTHING THAT NAMES ANOTHER BUSINESS. A
// linked row's revocation is identity_revoke_sessions (00071): it writes each business its own
// admin.logout rows -- byte for byte adminLoggedOutEvent's, actor and target that business's
// membership -- and none for a disabled or refused membership; this handler writes nothing on that
// path (a second row would double the business's trail). An UNLINKED row's revocation is today's
// case -- this admin, this business -- and this handler writes those rows, as Logout does for one.
// The count of revoked sessions reaches the log line alone, never an audit row: it would tell this
// business how many sessions the person holds elsewhere.
//
// 🔴 FLAG-GATED AND THROUGH A SUSPENSION. The route exists only while TAPPA_ADMIN_IDENTITY is on
// (mountGated, adminauth.SurfaceAccountIdentity -- closed, it is a path the panel never had), and
// it hangs off protectWritingThroughSuspension beside the admin's own password (ADR 0025's M14
// note): signing yourself out changes no business data, and a suspension must not keep a person
// signed in on a device they lost.
//
// NO SYNCHRONIZER TOKEN FIELD, like every other panel write: sameOriginGate, ahead of the resolver,
// is the CSRF defence (the password form's rule, account.templ).

// accountSignOutEverywhereHref is where the button posts. It hangs off the account section's URL,
// beside the password route, so the two personal acts move together.
var accountSignOutEverywhereHref = accountHref + "/sign-out-everywhere"

// problemSignOutEverywhereUnavailable is the 500 when the revocation itself failed. Both paths are
// one transaction, so nothing was signed out -- and the cookie is kept, so this page still works.
var problemSignOutEverywhereUnavailable = pages.ProblemView{
	Title:   "We could not sign you out everywhere",
	Message: "Something went wrong on our side, so nothing was signed out. You are still signed in here and on your other devices.",
	Hint:    "This one is ours rather than yours — please try again in a moment.",
}

// problemSignOutEverywhereSessionGone answers adminauth.ErrRevokeAllSessionGone: this session ended
// between the check at the door and the revocation, so nothing was signed out -- and signing out
// this business alone would let the person believe every business was. Signing in again gives a
// live session to press with.
var problemSignOutEverywhereSessionGone = pages.ProblemView{
	Title:   "Your session ended before we could sign you out everywhere",
	Message: "Nothing was signed out by that press. Your session on this device had already ended, so we stopped rather than sign out only part of your sessions.",
	Hint:    "Sign in again, then press \"Sign out everywhere\" once more.",
}

// accountSignOutEverywhere serves POST accountSignOutEverywhereHref.
func (a *AdminAuth) accountSignOutEverywhere(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	// The writing chain guarantees a live admin ahead of this; id.Live() is the belt. Without a
	// session there is no business to write a row in, so this only logs and refuses.
	if !id.Live() {
		a.log.Warn("panel sign-out everywhere refused: no live admin session")
		http.Error(w, "sign in", http.StatusUnauthorized)
		return
	}
	// THE SESSION'S OWN TOKEN, read again from the cookie the gate just verified: the identity is
	// found through it and nothing else. It is never logged.
	token, err := a.cookies.Read(r)
	if err != nil {
		a.log.Error("panel: the verified session's cookie could not be read again", "actor_id", id.Admin.AdminUserID)
		a.renderProblem(w, r, http.StatusInternalServerError, problemSignOutEverywhereUnavailable)
		return
	}
	// id.Admin.Linked: a session verified as linked must go through the identity or nowhere -- a
	// membership disabled since would otherwise reach the one-business fallback (adminauth).
	rv, err := a.admins.RevokeAll(r.Context(), id.TenantID(), id.Admin.AdminUserID, id.Admin.SessionID, id.Admin.Linked, token, false)
	if errors.Is(err, adminauth.ErrRevokeAllSessionGone) {
		// The session is dead already; its cookie is worth nothing, so it is cleared.
		a.log.Warn("panel sign-out everywhere refused: the session ended before the revocation", "actor_id", id.Admin.AdminUserID)
		a.cookies.Clear(w)
		a.renderProblem(w, r, http.StatusConflict, problemSignOutEverywhereSessionGone)
		return
	}
	if err != nil {
		// Clearing the cookie without revoking would leave live sessions behind a browser that
		// thinks it signed out -- Logout's rule.
		a.log.Error("panel: signing out everywhere failed", "actor_id", id.Admin.AdminUserID, "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemSignOutEverywhereUnavailable)
		return
	}
	if !rv.ThroughIdentity {
		// Today's case: this admin's sessions in this business, one ordinary row each (§4.5: the
		// tenant and the actor are the SESSION's).
		for _, s := range rv.Sessions {
			a.record(r.Context(), adminLoggedOutEvent(id.TenantID(), id.Admin.AdminUserID, s))
		}
	}
	a.cookies.Clear(w)
	if rv.ThroughIdentity {
		// NO COUNT HERE: on the identity path rv.Revoked spans every business, and a line carrying it
		// beside this business's actor_id would tell this business's log how many sessions the person
		// holds elsewhere (the M14-11 review; accountpassword.go keeps its count to this business too).
		a.log.Info("panel signed out everywhere", "actor_id", id.Admin.AdminUserID, "through_identity", true)
	} else {
		// A STABLE EVENT NAME, AT WARN (ADR 0031 §5.15's counter pattern): this route exists only with
		// TAPPA_ADMIN_IDENTITY on, and a fallback here is an UNLINKED row signing out -- countable. M14-19's
		// precheck does NOT promise every row is linked: its identityless count skips an address that
		// already has an identity, so a second row of such an address (M14-06's expected one: the older
		// row got the identity, this one did not) stays unlinked until the person links it on the
		// account page ("Link a business"; the runbook's mandatory smoke step) -- and so does a row the
		// person refused. The other half of the same gap: a LINKED session's "everywhere" (the branch
		// above) reaches only the rows linked to the identity, never such a row's sessions. The count is
		// this business's own.
		a.log.Warn("sign out everywhere fell back to one business",
			"actor_id", id.Admin.AdminUserID, "revoked_sessions", len(rv.Sessions))
	}
	a.redirect(w, adminLoginPath)
}
