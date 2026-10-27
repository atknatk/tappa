package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/email"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE ACCOUNT PAGE'S IDENTITY SECTIONS (M14-18, ADR 0031 §5.7, §5.8, §8): "Your businesses", "Verify
// your e-mail", the confirmation window (Keep all, unlink, Undo) and "Link a business".
//
// 🔴 FLAG-GATED, EVERY ROUTE AND EVERY SECTION. Each route is mounted through mountGated
// (adminauth.SurfaceAccountIdentity): with TAPPA_ADMIN_IDENTITY off it is a path the panel never had
// (404 for every method), and accountView draws none of the sections (AccountView.Identity nil) --
// the page is today's, byte for byte.
//
// 🔴 EVERY WRITE IS IN THE SUSPENSION GROUP AND TENANT-BOUND (ADR 0031 §5.4, ADR 0025's M14 note):
// protectWritingThroughSuspension -- flood, same origin, the session, the form's tenant binding
// (M14-10a) -- and no suspension ring: each act is the person's own (an address, the links of their
// sign-in), it changes no business's data, and a suspended business must not keep a person from
// checking which businesses their sign-in reaches. The one GET outside the panel group is the
// e-mailed link's landing (verifyLinkLanding): it writes a short-lived cookie and redirects, it
// verifies nothing.
//
// 🔴 THE KEY IS THE SESSION. Every act reads the cookie's token again and hands it -- or a fresh
// session adminauth opens after the password is typed again -- to the identity functions; a tenant
// from a form is a CHOICE they accept or refuse (the window's row), never an authority.
//
// 🔴 §7 -- NO ADDRESS, NO TOKEN, NO PASSWORD IN A LOG OR A ROW. Log lines carry ids and fixed words
// (ADR 0022 §10, ADR 0031 §5.15); a link's outcome is a log line, not an audit row (it belongs to no
// business). The audit rows the identity functions write are theirs; this file writes only the
// ordinary sign-out row for each fresh session adminauth opened and revoked (the definer wrote its
// sign-in row).

var (
	// accountVerifyHref asks for a verification link; accountVerifyConfirmHref spends one (the POST
	// that verifies -- R19: never the link's GET).
	accountVerifyHref        = accountHref + "/verify-email"
	accountVerifyConfirmHref = accountHref + "/verify-email/confirm"
	// The window's three acts.
	accountKeepAllHref = accountHref + "/confirm/keep-all"
	accountUnlinkHref  = accountHref + "/confirm/unlink"
	accountUndoHref    = accountHref + "/confirm/undo"
	// accountLinkHref is "Link a business", both directions.
	accountLinkHref = accountHref + "/link"
)

// verifyLinkPath is the e-mailed link's landing: adminauth.VerifyPath, the link's own path (under
// the panel cookie's path, so the session reaches the account page it redirects to).
const verifyLinkPath = adminauth.VerifyPath

const (
	// accountVerifyCookieName holds the e-mailed link's raw token between its GET and the
	// confirmation POST (the reset link's pattern, adminreset.go): the address bar and history lose it
	// after one hop. HttpOnly, the panel path, half an hour.
	accountVerifyCookieName   = "tappa_admin_verify"
	accountVerifyCookieMaxAge = 30 * 60
	// accountIdentityMaxBody bounds every identity form: a uuid and two passwords at most.
	accountIdentityMaxBody = 8 << 10
)

// The account page's identity words (accountDoneWords / accountProblemWords take them).
var (
	accountIdentityDoneWords    = []string{"verify-sent", "verified", "kept", "unlinked", "relinked", "linked"}
	accountIdentityProblemWords = []string{"verify-refused", "verify-unsent", "no-mail", "mail-down", "already-verified", "window-refused", "too-many", "identity-unavailable"}
)

// accountIdentityNotice places a done or problem word in its card.
func accountIdentityNotice(v *pages.AccountIdentityView, done, problem string) {
	switch done {
	case "verify-sent":
		v.VerifyNotice = "We sent a confirmation link to your email address. Open it in this browser while you are signed in, then press the button it brings you back to. It works for a day."
	case "verified":
		v.VerifyNotice = "Your email address is confirmed."
	case "kept":
		v.WindowNotice = "Every business stays on your sign-in."
	case "unlinked":
		v.WindowNotice = "That business is no longer on your sign-in. You can put it back from this list until the date above."
	case "relinked":
		v.WindowNotice = "That business is back on your sign-in."
	case "linked":
		// ONE SENTENCE whether or not anything was linked (ADR 0031 §5.7 (2)).
		v.LinkNotice = "We checked both passwords. If they belong to the same person, the business is now on this sign-in and appears under Your businesses."
	}
	switch problem {
	case "verify-refused":
		v.VerifyProblem = "That confirmation link did not work in this browser. It may have been used or have expired, or it was opened where you are not signed in. Ask for a new one and open it here."
	case "verify-unsent":
		v.VerifyProblem = "The confirmation email could not be sent just now. Try again in a few minutes."
	case "no-mail":
		v.VerifyProblem = "This Taptime does not send emails, so no confirmation link was sent."
	case "mail-down":
		v.VerifyProblem = "Our email service is not accepting messages right now, so nothing was sent. Try again in a few minutes."
	case "already-verified":
		v.VerifyNotice = "Your email address is already confirmed."
	case "window-refused":
		v.WindowProblem = "That could not be done. The check may have ended, or the business is no longer on the list."
	case "too-many":
		v.LinkProblem = "Too many attempts from this sign-in. Wait a few minutes and try again."
	case "identity-unavailable":
		v.LinkProblem = "That could not be done just now. Nothing was changed; please try again."
	}
}

// accountIdentityView builds the sections for a live session (accountView calls it only while the
// surface is open). A read that fails is shown as such, never as an empty list (§4.6).
func (a *AdminAuth) accountIdentityView(r *http.Request, id httpx.AdminIdentity) *pages.AccountIdentityView {
	v := &pages.AccountIdentityView{
		Linked:            id.Admin.Linked,
		SwitchHref:        adminSwitchHref,
		LinkHref:          accountLinkHref,
		KeepAllHref:       accountKeepAllHref,
		UnlinkHref:        accountUnlinkHref,
		UndoHref:          accountUndoHref,
		VerifyConfirmHref: "",
	}
	token, err := a.cookies.Read(r)
	if err != nil {
		a.log.Error("panel: the verified session's cookie could not be read again", "actor_id", id.Admin.AdminUserID)
		v.Unreadable = true
		return v
	}
	ms, err := a.admins.Memberships(r.Context(), token)
	if err != nil {
		a.log.Error("panel: could not read the person's businesses", "actor_id", id.Admin.AdminUserID, "err", err)
		v.Unreadable = true
	}
	for _, m := range ms {
		v.Businesses = append(v.Businesses, pages.AccountBusiness{Name: m.TenantName, Role: m.Role, Current: m.Current, Suspended: m.Suspended})
	}
	if !id.Admin.Linked {
		return v
	}
	w, err := a.admins.ConfirmWindow(r.Context(), token)
	if err != nil {
		a.log.Error("panel: could not read the confirmation window", "actor_id", id.Admin.AdminUserID, "err", err)
		v.Unreadable = true
		return v
	}
	v.Verified = w.Verified
	if !w.Verified {
		v.VerifyHref = accountVerifyHref
		if ck, err := r.Cookie(accountVerifyCookieName); err == nil && ck.Value != "" {
			v.VerifyConfirmHref = accountVerifyConfirmHref
		}
	}
	if w.Open {
		v.WindowOpen = true
		v.WindowReset = w.Scope == "reset"
		v.WindowEnds = a.accountWindowEnds(r, w.OpenUntil)
		for _, row := range w.Rows {
			v.WindowRows = append(v.WindowRows, pages.AccountWindowRow{
				TenantID: row.TenantID.String(), Name: row.TenantName, Role: row.Role,
				Refused: row.Refused, LastActiveOwner: row.LastActiveOwner,
			})
		}
	}
	return v
}

// accountWindowEnds is the window's end in this business's own zone (CLAUDE.md §6: local time only in
// the render layer).
func (a *AdminAuth) accountWindowEnds(r *http.Request, at time.Time) string {
	zone := time.UTC
	if s, err := a.accounts.Settings(r.Context(), httpx.AdminOf(r).TenantID()); err == nil {
		zone = a.accountZone(s.Timezone)
	}
	return at.In(zone).Format("2 January 2006, 15:04")
}

// accountIdentityAgain renders the account page again (200, or status) with one sentence placed by
// place -- a refused password beside its form, never in the address bar.
func (a *AdminAuth) accountIdentityAgain(w http.ResponseWriter, r *http.Request, status int, place func(*pages.AccountIdentityView)) {
	id := httpx.AdminOf(r)
	s, err := a.accounts.Settings(r.Context(), id.TenantID())
	if err != nil {
		a.log.Error("panel: could not re-read the business account after a refused identity act", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemAccountUnreadable)
		return
	}
	v := a.accountView(r, id, s)
	if v.Identity != nil {
		place(v.Identity)
	}
	a.renderPanel(w, r, status, v.PanelChrome, pages.AdminAccount(v))
}

// accountIdentityBegin is every identity POST's start: a live admin, the cookie's token, the bounded
// body parsed. It returns false having answered.
func (a *AdminAuth) accountIdentityBegin(w http.ResponseWriter, r *http.Request, where string) (httpx.AdminIdentity, adminauth.Token, bool) {
	id := httpx.AdminOf(r)
	if !id.Live() {
		a.log.Warn("panel identity act refused: no live admin session", "at", where)
		http.Error(w, "sign in", http.StatusUnauthorized)
		return id, adminauth.Token{}, false
	}
	token, err := a.cookies.Read(r)
	if err != nil {
		a.log.Error("panel: the verified session's cookie could not be read again", "actor_id", id.Admin.AdminUserID, "at", where)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return id, adminauth.Token{}, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, accountIdentityMaxBody)
	if err := r.ParseForm(); err != nil {
		// Classified, never printed: net/url quotes the offending input (a password among it).
		a.log.Warn("panel identity act refused: the form could not be read", "at", where, "actor_id", id.Admin.AdminUserID)
		a.redirect(w, accountReturn("", "unreadable"))
		return id, adminauth.Token{}, false
	}
	return id, token, true
}

// accountActBudget charges the person's identity-act bucket (adminIdentityActLimit) and reports
// whether the act may go on; past the limit it has answered 429 with the page.
func (a *AdminAuth) accountActBudget(w http.ResponseWriter, r *http.Request, id httpx.AdminIdentity, where string) bool {
	bucket, err := a.admins.AccountBucket(r.Context(), id.Admin)
	if err != nil {
		a.log.Error("panel: the identity act's bucket could not be read", "actor_id", id.Admin.AdminUserID, "at", where, "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return false
	}
	if n := a.identityActLimiter.Charge(bucket); n > adminIdentityActLimit {
		if a.identityActLimiter.FirstOverLimit(n) {
			a.log.Warn("panel identity act rate limited", "actor_id", id.Admin.AdminUserID, "at", where)
		}
		a.accountIdentityAgain(w, r, http.StatusTooManyRequests, func(v *pages.AccountIdentityView) {
			accountIdentityNotice(v, "", "too-many")
			v.WindowProblem = v.LinkProblem
		})
		return false
	}
	return true
}

// recordFresh writes the ordinary sign-out row of every fresh identity session an act opened and
// revoked (its sign-in row is identity_open_session's).
func (a *AdminAuth) recordFresh(r *http.Request, act adminauth.FreshAct, where string) {
	for i, s := range act.Temporary {
		if !act.Revoked[i] {
			a.log.Error("panel identity act: a fresh session could not be revoked", "session_id", s.ID, "tenant_id", s.TenantID, "at", where)
		}
		a.record(r.Context(), adminLoggedOutEvent(s.TenantID, s.AdminUserID, s.ID))
	}
}

// ---------------------------------------------------------------- verification --

// verifyLinkLanding serves GET verifyLinkPath?t=…: the e-mailed link. It verifies NOTHING (R19 -- a
// mail client's prefetch must not spend it): the value goes into a short-lived cookie and the
// browser is sent to the account page, which offers the confirmation press when a session is there
// (without one the panel sends it to the sign-in first; the cookie waits half an hour).
func (a *AdminAuth) verifyLinkLanding(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	t := r.URL.Query().Get(adminauth.VerifyParam)
	// A transport check only (adminreset.go's three refusals): what can be put in a cookie header.
	// What a token is, is adminauth's to decide at the POST.
	if t == "" || strings.ContainsAny(t, ".") || len(t) > 200 || !cookieSafe(t) {
		a.redirect(w, accountReturn("", "verify-refused"))
		return
	}
	a.short.set(w, accountVerifyCookieName, t, accountVerifyCookieMaxAge)
	a.redirect(w, accountHref)
}

// accountVerifyRequest serves POST accountVerifyHref: one verification link for the person's
// identity, mailed to its registered address.
func (a *AdminAuth) accountVerifyRequest(w http.ResponseWriter, r *http.Request) {
	id, token, ok := a.accountIdentityBegin(w, r, "verify_request")
	if !ok {
		return
	}
	// The person's bucket (adminauth.AccountBucket: the session's person when its row is linked --
	// an unlinked row is refused below by RequestVerification itself).
	bucket, err := a.admins.AccountBucket(r.Context(), id.Admin)
	if err != nil {
		a.log.Error("panel: the verification's bucket could not be read", "actor_id", id.Admin.AdminUserID, "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	// THE PER-IDENTITY BUDGET (M14-18's inherited obligation 1): identity_request_verification mints a
	// link per call, each an e-mail. Charged before anything is minted; a refusal mints nothing.
	if n := a.verifyLimiter.Charge(bucket); n > adminVerifyRequestLimit {
		if a.verifyLimiter.FirstOverLimit(n) {
			a.log.Warn("panel e-mail verification rate limited", "actor_id", id.Admin.AdminUserID)
		}
		a.accountIdentityAgain(w, r, http.StatusTooManyRequests, func(v *pages.AccountIdentityView) {
			v.VerifyProblem = "You asked for several confirmation links in the last hour. Use the newest email, or wait an hour and ask again."
		})
		return
	}
	if a.inviteMail == nil {
		a.redirect(w, accountReturn("", "no-mail"))
		return
	}
	if a.inviteMail.sender.Refusing() {
		a.log.Warn("panel e-mail verification refused: the mail breaker is open", "actor_id", id.Admin.AdminUserID)
		a.redirect(w, accountReturn("", "mail-down"))
		return
	}
	issued, err := a.admins.RequestVerification(r.Context(), id.Admin, token)
	switch {
	case errors.Is(err, adminauth.ErrAlreadyVerified):
		a.redirect(w, accountReturn("", "already-verified"))
		return
	case errors.Is(err, adminauth.ErrNotLinked):
		a.redirect(w, accountReturn("", "unavailable"))
		return
	case err != nil:
		a.log.Error("panel: could not write the verification link", "actor_id", id.Admin.AdminUserID, "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	base := strings.TrimRight(a.inviteMail.baseURL, "/")
	m, err := email.RenderEmailVerification(r.Context(), issued.Link(base), email.EmailVerificationView{
		BaseURL: a.inviteMail.baseURL, ValidFor: time.Until(issued.ExpiresAt).Round(time.Minute),
	})
	if err != nil {
		a.log.Error("panel: could not render the verification e-mail", "verification_id", issued.ID, "err_type", fmt.Sprintf("%T", err))
		a.redirect(w, accountReturn("", "verify-unsent"))
		return
	}
	m.To = issued.Recipient
	m.Ref = issued.ID.String()
	receipt, err := a.inviteMail.sender.Send(r.Context(), id.TenantID().String(), m)
	if err != nil {
		class, code, _ := sendErrorClass(err)
		a.log.Warn("panel e-mail verification: the e-mail was not sent", "verification_id", issued.ID,
			"class", class, "smtp_code", code, "err_type", fmt.Sprintf("%T", err))
		a.redirect(w, accountReturn("", "verify-unsent"))
		return
	}
	a.log.Info("panel e-mail verification: the relay accepted the e-mail", "verification_id", issued.ID, "message_id", receipt.MessageID)
	a.redirect(w, accountReturn("verify-sent", ""))
}

// accountVerifyConfirm serves POST accountVerifyConfirmHref: the link this browser carries, spent
// with THIS browser's session (identity_verify_email: the same identity's live session, any of its
// businesses -- R2, S16). The cookie is cleared whatever the answer.
func (a *AdminAuth) accountVerifyConfirm(w http.ResponseWriter, r *http.Request) {
	id, token, ok := a.accountIdentityBegin(w, r, "verify_confirm")
	if !ok {
		return
	}
	ck, err := r.Cookie(accountVerifyCookieName)
	a.short.clear(w, accountVerifyCookieName)
	if err != nil || ck.Value == "" {
		a.redirect(w, accountReturn("", "verify-refused"))
		return
	}
	first, err := a.admins.VerifyEmail(r.Context(), ck.Value, token)
	if errors.Is(err, adminauth.ErrVerificationRefused) {
		a.log.Info("panel e-mail verification refused", "actor_id", id.Admin.AdminUserID)
		a.redirect(w, accountReturn("", "verify-refused"))
		return
	}
	if err != nil {
		a.log.Error("panel: could not verify the e-mail address", "actor_id", id.Admin.AdminUserID, "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	a.log.Info("panel e-mail address verified", "actor_id", id.Admin.AdminUserID, "first", first)
	a.redirect(w, accountReturn("verified", ""))
}

// ---------------------------------------------------------------- the window --

// accountKeepAll serves POST accountKeepAllHref: ONE click, the live session, no password (T11).
func (a *AdminAuth) accountKeepAll(w http.ResponseWriter, r *http.Request) {
	id, token, ok := a.accountIdentityBegin(w, r, "keep_all")
	if !ok {
		return
	}
	err := a.admins.KeepAll(r.Context(), token)
	if errors.Is(err, adminauth.ErrWindowRefused) {
		a.redirect(w, accountReturn("", "window-refused"))
		return
	}
	if err != nil {
		a.log.Error("panel: could not close the confirmation window", "actor_id", id.Admin.AdminUserID, "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	a.log.Info("panel confirmation window kept", "actor_id", id.Admin.AdminUserID)
	a.redirect(w, accountReturn("kept", ""))
}

// accountWindowTarget is the window form's business: a CHOICE identity_unlink_backfilled /
// identity_relink_refused accept or refuse.
func accountWindowTarget(r *http.Request) (uuid.UUID, bool) {
	t, err := uuid.Parse(strings.TrimSpace(r.PostFormValue("tenant_id")))
	return t, err == nil && t != uuid.Nil
}

// accountUnlink serves POST accountUnlinkHref: the sign-in's password again, a fresh session, the
// unlink (ADR 0031 §5.8, S4).
func (a *AdminAuth) accountUnlink(w http.ResponseWriter, r *http.Request) {
	a.accountWindowAct(w, r, "unlink")
}

// accountUndo serves POST accountUndoHref: the same for a row refused in this window (S4, T4).
func (a *AdminAuth) accountUndo(w http.ResponseWriter, r *http.Request) {
	a.accountWindowAct(w, r, "undo")
}

func (a *AdminAuth) accountWindowAct(w http.ResponseWriter, r *http.Request, act string) {
	id, _, ok := a.accountIdentityBegin(w, r, act)
	if !ok {
		return
	}
	target, ok := accountWindowTarget(r)
	if !ok {
		a.redirect(w, accountReturn("", "window-refused"))
		return
	}
	if !a.accountActBudget(w, r, id, act) {
		return
	}
	password := r.PostFormValue("password")
	var (
		fresh adminauth.FreshAct
		ended bool
		err   error
	)
	if act == "unlink" {
		fresh, ended, err = a.admins.Unlink(r.Context(), id.Admin, password, target, a.accountLimiter)
	} else {
		fresh, err = a.admins.Relink(r.Context(), id.Admin, password, target, a.accountLimiter)
	}
	a.recordFresh(r, fresh, act)
	switch {
	case errors.Is(err, adminauth.ErrBadCredentials):
		a.accountIdentityAgain(w, r, http.StatusOK, func(v *pages.AccountIdentityView) {
			for i := range v.WindowRows {
				if v.WindowRows[i].TenantID == target.String() {
					v.WindowRows[i].Error = "Your password is not right."
				}
			}
		})
		return
	case errors.Is(err, adminauth.ErrWindowRefused), errors.Is(err, adminauth.ErrNotLinked):
		a.log.Info("panel confirmation window act refused", "actor_id", id.Admin.AdminUserID, "at", act)
		a.redirect(w, accountReturn("", "window-refused"))
		return
	case err != nil:
		a.log.Error("panel: the confirmation window act failed", "actor_id", id.Admin.AdminUserID, "at", act, "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	a.log.Info("panel confirmation window act", "actor_id", id.Admin.AdminUserID, "at", act)
	if ended {
		// The unlinked business was this one: its sessions -- this cookie's among them -- are revoked.
		a.cookies.Clear(w)
		a.redirect(w, adminLoginPath)
		return
	}
	if act == "unlink" {
		a.redirect(w, accountReturn("unlinked", ""))
		return
	}
	a.redirect(w, accountReturn("relinked", ""))
}

// ---------------------------------------------------------------- link a business --

// accountLink serves POST accountLinkHref: both passwords, both sides proven in this request with
// fresh sessions, the link (ADR 0031 §5.7 (2); S6: an old session is no obstacle -- the password is
// asked again). The answer is the same for "linked" and "nothing to link" (the one redirect);
// only the person's own current password being wrong is said.
func (a *AdminAuth) accountLink(w http.ResponseWriter, r *http.Request) {
	id, _, ok := a.accountIdentityBegin(w, r, "link")
	if !ok {
		return
	}
	current, other := r.PostFormValue("current_password"), r.PostFormValue("other_password")
	if current == "" || other == "" {
		a.accountIdentityAgain(w, r, http.StatusOK, func(v *pages.AccountIdentityView) {
			v.LinkProblem = "Type both passwords."
		})
		return
	}
	if !a.accountActBudget(w, r, id, "link") {
		return
	}
	res, err := a.admins.LinkBusiness(r.Context(), id.Admin, current, other, a.accountLimiter)
	a.recordFresh(r, res.FreshAct, "link")
	switch {
	case errors.Is(err, adminauth.ErrBadCredentials):
		a.accountIdentityAgain(w, r, http.StatusOK, func(v *pages.AccountIdentityView) {
			v.LinkProblem = "Your password for this sign-in is not right."
		})
		return
	case errors.Is(err, adminauth.ErrNotLinked), errors.Is(err, adminauth.ErrWindowRefused), errors.Is(err, adminauth.ErrNoSuchAdmin):
		a.redirect(w, accountReturn("", "identity-unavailable"))
		return
	case err != nil:
		a.log.Error("panel: linking a business failed", "actor_id", id.Admin.AdminUserID, "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	// A STABLE EVENT NAME (ADR 0031 §5.15's counters), no count: the number of businesses linked would
	// tell this business's log about the person's others.
	a.log.Info("panel identity link", "actor_id", id.Admin.AdminUserID, "outcome", map[bool]string{true: "linked", false: "none"}[res.Linked > 0])
	a.redirect(w, accountReturn("linked", ""))
}
