package handler

import (
	"errors"
	"net/http"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE ACCOUNT SECTION'S OTHER WRITE -- CHANGE MY OWN PASSWORD (M7-05, T73).
//
// 🔴 IT IS NOT OWNER-ONLY, AND THAT IS THE WHOLE DIFFERENCE FROM accountSave (K-A). A
// business's name, timezone and type are the OWNER's to change (each reaches outside this
// screen); a password is PERSONAL, so the gate here is "any live admin" -- owner AND
// manager -- rather than mayEditAccount. The write chain (ProtectWriting) already
// guarantees a live admin ahead of the resolver; id.Live() below is the belt, and it is
// the one the handler test drives directly.
//
// 🔴 §4.7 -- NO PASSWORD OR DIGEST PASSES THROUGH THIS FILE INTO A LOG, AN AUDIT ROW OR A
// RENDER. adminauth.ChangeOwnPassword compares and writes the credential and never hands
// it back; what reaches this handler is a sentinel (which policy failed) and a count (how
// many other sessions were revoked). The audit detail carries the outcome, a fixed reason
// token and that count -- never a field a caller's typing could travel in.
//
// 🔴 THE SUCCESS ROW AND THE REFUSAL ROW ARE BOTH WRITTEN HERE, deliberately: unlike
// accountSave's success (which the domain writes inside its own transaction), this flow's
// write lives in internal/adminauth and returns a plain count, so the audit is the
// handler's -- the same boundary the reset flow settled on. A client-side policy failure
// (weak or mismatched password) writes NOTHING: it never reached the credential, exactly
// as the reset flow's checkNewPassword refusal does not.

// accountPasswordHref is where the password form posts. It hangs off the account
// section's own URL so the two move together, and it is a SEPARATE route from accountSave
// so the owner-only gate on that one can never accidentally cover this personal one.
var accountPasswordHref = accountHref + "/password"

// Audit actions for change-my-own-password. Free text by schema (00005), so the constants
// are the vocabulary. The names follow the trail's split: `<subject>.<past participle>`
// for the act that happened, `<subject>.<verb>_<refusal>` for the one that did not.
const (
	ActionAdminPasswordChanged       = "admin.password_changed"
	ActionAdminPasswordChangeRefused = "admin.password_change_refused"
)

// passwordChangedDetail is the audit payload for a successful change. RevokedOtherSessions
// is the K3 count -- how many devices this rotation signed out, this one excepted.
type passwordChangedDetail struct {
	Outcome              string `json:"outcome"`
	RevokedOtherSessions int    `json:"revoked_other_sessions"`
}

// passwordRefusedDetail is the audit payload for a refusal the credential check produced
// (wrong current, or same as current). Reason is a FIXED token, never the input: the row
// exists to say a rotation was attempted and refused, not to record what was typed.
type passwordRefusedDetail struct {
	Outcome string `json:"outcome"`
	Reason  string `json:"reason"`
}

// problemPasswordUnavailable is the 500 for an unexpected failure of the change itself --
// a live admin whose own row could not be updated, which should not happen (a disabled
// admin cannot hold a live session). It says our side and names no secret.
var problemPasswordUnavailable = pages.ProblemView{
	Title:   "We could not change your password",
	Message: "Something went wrong on our side, so your password was not changed. Nothing else about your account was affected.",
	Hint:    "This one is ours rather than yours — please try again in a moment.",
}

// accountPasswordSave rotates the signed-in admin's own password.
func (a *AdminAuth) accountPasswordSave(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)

	// 🔴 THE GATE IS id.Live(), NOT mayEditAccount (K-A). Any live admin may change their
	// own password. There is no tenant context to attribute an audit row to when the
	// session is absent, so this path only logs and refuses.
	if !id.Live() {
		a.log.Warn("panel credential change refused: no live admin session")
		http.Error(w, "sign in", http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAccountBody)
	if err := r.ParseForm(); err != nil {
		// The parse error is classified, never printed: net/url quotes the offending
		// input, so a password could otherwise reach the log through exactly this branch.
		reason := "malformed form"
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			reason = "body over the limit"
		}
		a.log.Warn("panel credential change refused: the form could not be read",
			"limit_bytes", maxAccountBody, "reason", reason)
		a.renderAccountPasswordAgain(w, r, id, "That form could not be read, so your password was not changed.")
		return
	}

	current := r.PostFormValue("current_password")
	chosen := r.PostFormValue("new_password")
	confirm := r.PostFormValue("confirm_password")

	// 🔴 CLIENT-SIDE POLICY FIRST, AND IT WRITES NOTHING. checkNewPassword is the SAME
	// bound the reset flow and the sign-up wizard use (internal/domain/signup), reused
	// rather than restated. A failure here has not reached the credential -- no database
	// call, no audit row -- exactly as the reset flow treats it.
	if msg := checkNewPassword(chosen, confirm); msg != "" {
		a.renderAccountPasswordAgain(w, r, id, msg)
		return
	}

	revoked, err := a.admins.ChangeOwnPassword(r.Context(),
		id.TenantID(), id.Admin.AdminUserID, id.Admin.SessionID, current, chosen)
	switch {
	case err == nil:
		a.record(r.Context(), audit.Event{
			// §4.5: the tenant is the SESSION's. The target is the admin themselves --
			// this event is about a person's credential, not about the business.
			TenantID: id.TenantID(),
			ActorID:  ptr(id.Admin.AdminUserID),
			Action:   ActionAdminPasswordChanged,
			Target:   id.Admin.AdminUserID.String(),
			Detail:   passwordChangedDetail{Outcome: "ok", RevokedOtherSessions: revoked},
		})
		a.log.Info("panel credential changed",
			"actor_id", id.Admin.AdminUserID, "revoked_other_sessions", revoked)
		// 303 so a refresh does not re-submit; the flash names the K3 sign-out.
		a.redirect(w, accountReturn("password", ""))
	case errors.Is(err, adminauth.ErrWrongCurrentPassword):
		a.recordPasswordRefusal(r, id, "wrong_current_password")
		a.renderAccountPasswordAgain(w, r, id, "Your current password is not right.")
	case errors.Is(err, adminauth.ErrSameAsCurrentPassword):
		a.recordPasswordRefusal(r, id, "same_as_current")
		a.renderAccountPasswordAgain(w, r, id, "New password must be different from your current one.")
	default:
		// Includes adminauth.ErrNoSuchAdmin (disabled/unknown, unreachable with a live
		// session) and any database error. It is ours; the response never says which, and
		// the log line carries no credential.
		a.log.Error("panel: could not change the admin credential",
			"actor_id", id.Admin.AdminUserID, "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPasswordUnavailable)
	}
}

// recordPasswordRefusal writes the refusal row for a credential-check failure.
//
// It is its own transaction (a.record -> audit.Record) because nothing else is being
// written -- that is the point -- so there is no surrounding write to share a fate with.
// A failed audit write does not change the answer: the admin still gets the sentence, not
// a 500 (a.record logs the failure loudly and returns).
func (a *AdminAuth) recordPasswordRefusal(r *http.Request, id httpx.AdminIdentity, reason string) {
	a.log.Warn("panel credential change refused", "actor_id", id.Admin.AdminUserID, "reason", reason)
	a.record(r.Context(), audit.Event{
		TenantID: id.TenantID(),
		ActorID:  ptr(id.Admin.AdminUserID),
		Action:   ActionAdminPasswordChangeRefused,
		Target:   id.Admin.AdminUserID.String(),
		Detail:   passwordRefusedDetail{Outcome: "refused", Reason: reason},
	})
}

// renderAccountPasswordAgain re-renders the whole account section with one message beside
// the password form, at HTTP 200 (the response IS the form; a browser showing a form is
// not an error).
//
// IT RE-READS THE ROW rather than rebuilding the page from the form, for the reason
// renderAccountFormAgain does: the facts docket, the VAT verdict and the account form all
// carry values this POST never sent. A failed re-read is a problem page -- nothing was
// written on this path, and a form with no facts around it is worse than saying so.
func (a *AdminAuth) renderAccountPasswordAgain(w http.ResponseWriter, r *http.Request, id httpx.AdminIdentity, msg string) {
	s, err := a.accounts.Settings(r.Context(), id.TenantID())
	if err != nil {
		a.log.Error("panel: could not re-read the business account after a refused credential change", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemAccountUnreadable)
		return
	}
	v := a.accountView(r, id, s)
	v.PasswordError = msg
	a.render(w, r, http.StatusOK, pages.AdminAccount(v))
}
