package handler

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// adminloginidentity.go -- the identity path of POST /admin/login (ADR 0031 §5.6, M14-09).
//
// WHILE adminauth.SurfaceLoginIdentity IS OPEN (TAPPA_ADMIN_IDENTITY on, §8's first row) a sign-in
// asks adminauth.AuthenticatePerson instead of Authenticate. Three outcomes:
//
//	an identity session   the person lands in the business asked for (none from this form), else
//	                      the one used last, else the oldest active membership -- K-4, decided by
//	                      identity_open_session. No picker on this path.
//	the legacy arm        no identity session, but UNLINKED rows the same password verified: the
//	                      person signs in as today -- Issue for one, the picker for several, until
//	                      contract A (§5.6: K-4's "no picker" belongs to the identity path only).
//	a failure             ErrBadCredentials, one value for every reason (00011 OBLIGATION 1): the
//	                      same body, the same status, today's failLogin.
//
// Everything in front of it -- the flood ceiling, Origin, the synchronizer token, the attempt and
// work budgets -- is Login's, the same for both paths. While the row is closed nothing here runs and
// the sign-in is today's, byte for byte (TestAdminLogin_OneVerifiedBusinessSkipsThePicker,
// TestAdminChoose_RefusesATenantOutsideTheSignedSet run with the flag off, unchanged).

// THE ACCOUNT LIMITER IS THE IDENTITY'S FAILURE BUDGET (ADR 0031 §5.2 named exception 3, §5.6:
// adminAccountLimit keyed by identity_id on this path). adminauth declares the interface at its
// consumer; this line is the compile-time proof that the limiter handed over satisfies it.
var _ adminauth.FailureBudget = (*httpx.Limiter)(nil)

// problemAdminSignInAgain is the answer when the credential a sign-in verified no longer held as
// its session was written (adminauth.SignInLapsed) -- BEFORE any session was opened. Nothing was
// signed in and nothing is broken; "Something went wrong on our side" would be false, and a 500 would
// page somebody for a password that was changed on purpose. It does not say "nothing was changed":
// on the identity path a lapse can follow proof sessions that were opened and revoked.
var problemAdminSignInAgain = pages.ProblemView{
	Title:   "Please sign in again",
	Message: "Your sign-in could not be completed, so you were not signed in.",
	Hint:    "Start again from the sign-in page.",
}

// problemAdminSignInAbandoned is the answer when the identity session was OPENED and the sign-in
// failed after that (adminauth.PersonSignIn.Abandoned, M14-09's audit): a server-side failure, told
// as one, and without problemAdminServer's "nothing was changed" -- the session and its success row
// were committed (and the session revoked).
var problemAdminSignInAbandoned = pages.ProblemView{
	Title:   "We could not finish signing you in",
	Message: "Something went wrong on our side while signing you in, so you were not signed in.",
	Hint:    "Try again in a minute.",
}

// identityPath reports whether this process signs people in through their identity: §8's
// login row, read from the one table (adminauth.IdentityGate), never from the flag directly.
func (a *AdminAuth) identityPath() bool {
	return a.identityGate.Open(adminauth.SurfaceLoginIdentity)
}

// loginAsPerson is Login's identity path (this file's header). It answers the request.
func (a *AdminAuth) loginAsPerson(w http.ResponseWriter, r *http.Request, ip string, st adminLoginState, email, password string) {
	// uuid.Nil: the sign-in form names no business; identity_open_session lands on the one used
	// last, else the oldest active membership (K-4). The budget is the account limiter: the
	// identity's failure rows are metered per identity, under a key the account rows never use
	// (adminauth.identityBudgetKey).
	in, err := a.admins.AuthenticatePerson(r.Context(), email, password, uuid.Nil, a.accountLimiter)
	if err != nil && in.Abandoned != nil {
		// AFTER the open, whatever the error (a 40P01 included): not a lapse to sign in again from.
		a.abandonedLogin(w, r, *in.Abandoned, err)
		return
	}
	if err != nil && !errors.Is(err, adminauth.ErrBadCredentials) {
		if adminauth.SignInLapsed(err) {
			// identity_open_session and the legacy proof sessions handle their own refusals; what
			// reaches here is a write that lost a deadlock to a change of the same person.
			a.signInAgain(w, r, uuid.Nil, err)
			return
		}
		a.log.Error("panel sign-in failed", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemAdminServer)
		return
	}
	if err == nil && in.Session != nil {
		a.completePersonLogin(w, r, ip, *in.Session, in.Linked)
		return
	}
	if err == nil && len(in.Legacy.Verified()) == 0 {
		// AuthenticatePerson's contract makes this unreachable (no session and nothing verified
		// comes with ErrBadCredentials); if it ever were reached, it is a failure -- never a picker
		// with nothing in it, never a session.
		err = adminauth.ErrBadCredentials
	}
	a.concludeLogin(w, r, ip, st, in.Legacy, err)
}

// completePersonLogin writes the identity session's cookie and sends the person to the panel.
//
// 🔴 IT WRITES NO admin.login.succeeded ROW: identity_open_session wrote it, in the business the
// session landed in, byte for byte the handler's (adminauth.PersonSignIn.Session). A second one here
// would double the trail (TestPanelIdentityLogin_OneMembershipSignsStraightIn counts exactly one).
func (a *AdminAuth) completePersonLogin(w http.ResponseWriter, r *http.Request, ip string, issued adminauth.Issued, linked int) {
	s := issued.Session
	if err := a.cookies.Set(w, issued.Token); err != nil {
		// completeLogin's unreachable branch, with its trail: a live session nobody can use.
		a.log.Error("panel sign-in: writing the session cookie failed",
			"admin_user_id", s.AdminUserID, "err", err)
		a.record(r.Context(), audit.Event{
			TenantID: s.TenantID,
			ActorID:  ptr(s.AdminUserID),
			Action:   ActionAdminLoginFailed,
			Target:   s.AdminUserID.String(),
			Detail: adminLoginDetail{
				Outcome: "session_cookie_failed",
				Reason:  "the session was created but its cookie could not be written",
			},
		})
		a.renderProblem(w, r, http.StatusInternalServerError, problemAdminServer)
		return
	}
	a.resetLogin(w)
	// linked is a number for this line, never for an audit row (R21).
	a.log.Info("panel sign-in", "admin_user_id", s.AdminUserID, "ip", ip, "path", "identity", "linked", linked)
	a.redirect(w, "/admin")
}

// abandonedLogin answers a sign-in whose identity session was opened -- committed, with the
// business's admin.login.succeeded row -- and then failed (adminauth revoked it). The business gets
// the rest of the trail in completeLogin's session_cookie_failed shape: a failed-sign-in row naming
// the same session, saying whether the revocation held. The person is told only what is true.
func (a *AdminAuth) abandonedLogin(w http.ResponseWriter, r *http.Request, s adminauth.AbandonedSession, err error) {
	reason := "the session was opened but the sign-in could not complete, so it was revoked"
	if !s.Revoked {
		reason = "the session was opened but the sign-in could not complete, and revoking it failed"
	}
	a.log.Error("panel sign-in: the identity session was opened and the sign-in failed after it",
		"admin_user_id", s.Session.AdminUserID, "revoked", s.Revoked, "err", err)
	a.record(r.Context(), audit.Event{
		TenantID: s.Session.TenantID,
		ActorID:  ptr(s.Session.AdminUserID),
		Action:   ActionAdminLoginFailed,
		Target:   s.Session.AdminUserID.String(),
		Detail: adminLoginDetail{
			Outcome:   "session_abandoned",
			Reason:    reason,
			SessionID: s.Session.ID.String(),
		},
	})
	a.resetLogin(w)
	a.renderProblem(w, r, http.StatusInternalServerError, problemAdminSignInAbandoned)
}

// signInAgain answers a sign-in whose verified credential lapsed before its session was written
// (adminauth.SignInLapsed): nothing was signed in, the short-lived cookies are cleared, and the
// person starts again. admin is the row the session was for (uuid.Nil when the identity path cannot
// name one).
func (a *AdminAuth) signInAgain(w http.ResponseWriter, r *http.Request, admin uuid.UUID, err error) {
	a.log.Warn("panel sign-in: the verified credential changed before the session was written; asked to sign in again",
		"admin_user_id", admin, "ip", clientIP(r), "err", err)
	a.resetLogin(w)
	a.renderProblem(w, r, http.StatusConflict, problemAdminSignInAgain)
}

// loginDetail is every audit payload of this flow that can carry a count or a tenant: on the
// identity path it drops the four fields that tell a business about the person's OTHER businesses
// (ADR 0031 §5.6, R21) -- how many businesses the password verified (VerifiedBusiness), how many
// rows the address resolved to and how many were compared (Resolved, Compared), and the business a
// refused choice named (AttemptedTenant). Those numbers live in admin_identity_events only. While
// the path is closed the payload is today's, unchanged.
//
// The PICKER is part of the identity path while it is open: a choice blob is minted only by the
// legacy arm of AuthenticatePerson then, so ChoosePage's and Choose's rows go through here too.
//
// THE REASON TEXTS TOO (M14-09's audit, DÜŞÜK-2): three fixed sentences of this flow speak of
// businesses in the plural ("registered in more businesses", "every business was disabled", "not in
// the verified set") -- the counts in words. On the identity path each becomes its single-business
// twin (identityReasons); a reason without a twin is already one account's ("wrong password").
func (a *AdminAuth) loginDetail(d adminLoginDetail) adminLoginDetail {
	if a.identityPath() {
		d.VerifiedBusiness, d.Resolved, d.Compared, d.AttemptedTenant = 0, 0, 0, ""
		if twin, ok := identityReasons[d.Reason]; ok {
			d.Reason = twin
		}
	}
	return d
}

// identityReasons maps the flag-off reason of a row loginDetail shapes to the identity path's.
var identityReasons = map[string]string{
	reasonTruncated:        "the per-request candidate cap was reached on this sign-in",
	reasonDisabledByChoice: "password verified but the account was disabled before the choice",
	reasonChoiceRefused:    "the posted choice was refused",
}

// The flag-off reasons identityReasons rewrites, named once so the rows and the map cannot drift.
const (
	reasonTruncated = "the address is registered in more businesses than one sign-in compares, " +
		"and this attempt authenticated against a subset"
	reasonDisabledByChoice = "password verified but every business was disabled before the choice"
	reasonChoiceRefused    = "chosen business was not in the verified set"
)
