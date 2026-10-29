package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/email"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE LINK BY E-MAIL (M14-22; ADR 0031 §5.2's and §5.7's notes of 2026-10-29). The user's decisions:
// "E-posta koduyla, tek tık" -- the proof that another business's sign-in is the person's is their
// MAILBOX, the mainstream pattern (Slack's "find your workspaces" confirmation; one account with a
// workspace switcher in Notion and Linear) -- and the banner names no business before that proof.
//
//	the banner        every panel section of a LINKED session whose address holds an active row of
//	                  another business that is on no sign-in (chrome, panelAddBusiness): "Another
//	                  business uses your email. Add it to this sign-in." -- one button
//	"Add it"          POST accountLinkBusinessesHref: ONE mailed link to the identity's registered
//	                  address (24 hours, single use), the per-person budget charged BEFORE anything is
//	                  minted; the answer is "Check your inbox", naming nothing
//	the link's GET    linkLandingPath: the token into a short-lived cookie, a redirect -- it spends
//	                  NOTHING (R19: a mail client's prefetch must not)
//	the page          GET accountLinkBusinessesHref: in the browser holding the link, the one press
//	the press         POST accountLinkBusinessesConfirmHref: identity_link_by_email with the link AND this
//	                  browser's live session of the same identity (R2) -- every active, unrefused,
//	                  unlinked row of the address joins the sign-in in one transaction; then ONE notice
//	                  e-mail ("N businesses were added")
//
// 🔴 FLAG-GATED (adminauth.SurfaceAccountIdentity, ADR 0031 §8): with TAPPA_ADMIN_IDENTITY off every
// route is a path the panel never had and the banner is never drawn.
//
// 🔴 EVERY WRITE IS IN THE SUSPENSION GROUP AND TENANT-BOUND (ADR 0025's M14 note, M14-10a): the act is
// the person's own -- it changes no business's data and a suspension keeps sign-in open.
//
// 🔴 §7 -- no address, no token, no identity id and no COUNT in a log line or an audit row: the number of
// businesses linked would tell this business's log about the person's others (the audit rows are the
// definer's, one per linked business, naming nothing else).

var (
	// accountLinkBusinessesHref is the page (GET) and the request (POST, "Add it");
	// accountLinkBusinessesConfirmHref is the press that spends the link (never the link's GET).
	accountLinkBusinessesHref        = accountHref + "/link-businesses"
	accountLinkBusinessesConfirmHref = accountHref + "/link-businesses/confirm"
)

// linkLandingPath is the e-mailed link's landing: adminauth.LinkPath (under the panel cookie's path, so
// the session reaches the page it redirects to).
const linkLandingPath = adminauth.LinkPath

const (
	// accountLinkCookieName holds the e-mailed link's raw token between its GET and the press (the
	// verification link's pattern): the address bar and history lose it after one hop. HttpOnly, the
	// panel path, half an hour.
	accountLinkCookieName   = "tappa_admin_link"
	accountLinkCookieMaxAge = 30 * 60
	// linkBucketPrefix keeps the link's budget apart from the verification's in the same limiter: the
	// same number per person per hour (adminVerifyRequestLimit), counted on its own.
	linkBucketPrefix = "link|"
)

// The page's outcome words (closed tables; a word in neither prints nothing).
var (
	linkBusinessesDone = map[string]string{
		"added": "Done. Every business that uses your email address is now on this sign-in.",
		"none":  "There was no other business to add. Nothing was changed.",
	}
	linkBusinessesProblem = map[string]string{
		"refused":     "That link did not work in this browser. It may have been used or have expired, or it was opened where you are not signed in. Ask for a new one and open it here.",
		"unsent":      "The email could not be sent just now. Try again in a few minutes.",
		"no-mail":     "This Taptime does not send emails, so no link was sent.",
		"mail-down":   "Our email service is not accepting messages right now, so nothing was sent. Try again in a few minutes.",
		"too-many":    "You asked for several links in the last hour. Use the newest email, or wait an hour and ask again.",
		"unavailable": "That could not be done from this sign-in.",
	}
)

// linkBusinessesReturn is the page's address with an outcome word (POST-redirect-GET: no binding in it).
func linkBusinessesReturn(done, problem string) string {
	switch {
	case done != "":
		return accountLinkBusinessesHref + "?done=" + done
	case problem != "":
		return accountLinkBusinessesHref + "?problem=" + problem
	}
	return accountLinkBusinessesHref
}

// addBusinessBanner is the shell's banner for this request (chromeInGroup calls it): drawn for a live,
// LINKED session while the flag is on and adminauth answers that another business of the address is on
// no sign-in. A failed read draws no banner and is logged -- the section is still worth rendering.
func (a *AdminAuth) addBusinessBanner(r *http.Request, id httpx.AdminIdentity) pages.PanelAddBusiness {
	if !a.identityGate.Open(adminauth.SurfaceAccountIdentity) || !id.Live() || !id.Admin.Linked {
		return pages.PanelAddBusiness{}
	}
	add, err := a.admins.HasBusinessToAdd(r.Context(), id.Admin)
	if err != nil {
		a.log.Error("panel: could not read whether another business uses the address", "actor_id", id.Admin.AdminUserID, "err", err)
		return pages.PanelAddBusiness{}
	}
	if !add {
		return pages.PanelAddBusiness{}
	}
	b := pages.PanelAddBusiness{Href: accountLinkBusinessesHref}
	if ck, err := r.Cookie(accountLinkCookieName); err == nil && ck.Value != "" {
		b.Finish = true
	}
	return b
}

// linkLanding serves GET linkLandingPath?t=…: the e-mailed link. It links NOTHING (R19): the value goes
// into a short-lived cookie and the browser is sent to the page, which offers the press when a session
// is there (without one the panel sends it to the sign-in first; the cookie waits half an hour, and the
// banner then leads back to the page).
func (a *AdminAuth) linkLanding(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	t := r.URL.Query().Get(adminauth.LinkParam)
	// A transport check only (verifyLinkLanding's): what can be put in a cookie header.
	if t == "" || strings.ContainsAny(t, ".") || len(t) > 200 || !cookieSafe(t) {
		a.redirect(w, linkBusinessesReturn("", "refused"))
		return
	}
	a.short.set(w, accountLinkCookieName, t, accountLinkCookieMaxAge)
	a.redirect(w, accountLinkBusinessesHref)
}

// linkBusinessesPage serves GET accountLinkBusinessesHref. It writes nothing.
func (a *AdminAuth) linkBusinessesPage(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !id.Live() {
		a.requireLogin(w, r)
		return
	}
	a.renderLinkBusinesses(w, r, http.StatusOK, id,
		strings.TrimSpace(r.URL.Query().Get("done")), strings.TrimSpace(r.URL.Query().Get("problem")))
}

// renderLinkBusinesses draws the page in one of its states, with the outcome words given.
func (a *AdminAuth) renderLinkBusinesses(w http.ResponseWriter, r *http.Request, status int, id httpx.AdminIdentity, done, problem string) {
	v := pages.LinkBusinessesView{PanelChrome: a.chrome(r, pages.TabAccount)}
	// The page IS the banner's destination: the banner is not drawn on it.
	v.AddBusiness = pages.PanelAddBusiness{}
	v.Title = "Your other businesses"
	v.Kicker = "One sign-in for every business you run"
	v.Crumbs = []pages.Crumb{{Label: "Account", Href: accountHref}, {Label: "Your other businesses"}}
	v.Done = linkBusinessesDone[done]
	v.Problem = linkBusinessesProblem[problem]
	switch {
	case !id.Admin.Linked:
		v.Unlinked = true
	case hasLinkCookie(r):
		v.ConfirmHref = accountLinkBusinessesConfirmHref
	case done == "sent":
		v.Sent = true
	case done == "added":
		v.SwitchHref = adminSwitchHref
	default:
		add, err := a.admins.HasBusinessToAdd(r.Context(), id.Admin)
		if err != nil {
			a.log.Error("panel: could not read whether another business uses the address", "actor_id", id.Admin.AdminUserID, "err", err)
			a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
			return
		}
		if add {
			v.RequestHref = accountLinkBusinessesHref
		}
	}
	a.renderPanel(w, r, status, v.PanelChrome, pages.AdminLinkBusinesses(v))
}

func hasLinkCookie(r *http.Request) bool {
	ck, err := r.Cookie(accountLinkCookieName)
	return err == nil && ck.Value != ""
}

// accountLinkRequest serves POST accountLinkBusinessesHref ("Add it"): one link for the person's
// identity, mailed to its registered address.
func (a *AdminAuth) accountLinkRequest(w http.ResponseWriter, r *http.Request) {
	id, token, ok := a.accountIdentityBegin(w, r, "link_request")
	if !ok {
		return
	}
	// AN UNLINKED ROW IS REFUSED BEFORE ANY CHARGE (M14-18's rule): it is not the identity, and charging
	// the bucket its address names would let it spend the real person's.
	if !id.Admin.Linked {
		a.redirect(w, linkBusinessesReturn("", "unavailable"))
		return
	}
	bucket, err := a.admins.AccountBucket(r.Context(), id.Admin)
	if err != nil {
		a.log.Error("panel: the link's bucket could not be read", "actor_id", id.Admin.AdminUserID, "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	// THE PER-PERSON BUDGET, BEFORE ANY WORK: every request mints a link and sends an e-mail. One locked
	// step (TryCharge): N concurrent presses cannot all pass a check before any is counted.
	if within, n := a.verifyLimiter.TryCharge(linkBucketPrefix + bucket); !within {
		if a.verifyLimiter.FirstOverLimit(n) {
			a.log.Warn("panel link by e-mail rate limited", "actor_id", id.Admin.AdminUserID)
		}
		a.renderLinkBusinesses(w, r, http.StatusTooManyRequests, id, "", "too-many")
		return
	}
	if a.inviteMail == nil {
		a.redirect(w, linkBusinessesReturn("", "no-mail"))
		return
	}
	if a.inviteMail.sender.Refusing() {
		a.log.Warn("panel link by e-mail refused: the mail breaker is open", "actor_id", id.Admin.AdminUserID)
		a.redirect(w, linkBusinessesReturn("", "mail-down"))
		return
	}
	issued, err := a.admins.RequestLink(r.Context(), id.Admin, token)
	switch {
	case errors.Is(err, adminauth.ErrNothingToAdd):
		a.redirect(w, linkBusinessesReturn("none", ""))
		return
	case errors.Is(err, adminauth.ErrNotLinked), errors.Is(err, adminauth.ErrNoSession):
		a.redirect(w, linkBusinessesReturn("", "unavailable"))
		return
	case err != nil:
		a.log.Error("panel: could not write the link", "actor_id", id.Admin.AdminUserID, "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	base := strings.TrimRight(a.inviteMail.baseURL, "/")
	m, err := email.RenderLinkBusinesses(r.Context(), issued.Link(base), email.LinkBusinessesView{
		BaseURL: a.inviteMail.baseURL, ValidFor: time.Until(issued.ExpiresAt).Round(time.Minute),
	})
	if err != nil {
		a.log.Error("panel: could not render the link e-mail", "verification_id", issued.ID, "err_type", fmt.Sprintf("%T", err))
		a.redirect(w, linkBusinessesReturn("", "unsent"))
		return
	}
	m.To = issued.Recipient
	m.Ref = issued.ID.String()
	receipt, err := a.inviteMail.sender.Send(r.Context(), id.TenantID().String(), m)
	if err != nil {
		class, code, _ := sendErrorClass(err)
		a.log.Warn("panel link by e-mail: the e-mail was not sent", "verification_id", issued.ID,
			"class", class, "smtp_code", code, "err_type", fmt.Sprintf("%T", err))
		a.redirect(w, linkBusinessesReturn("", "unsent"))
		return
	}
	a.log.Info("panel link by e-mail: the relay accepted the e-mail", "verification_id", issued.ID, "message_id", receipt.MessageID)
	a.redirect(w, linkBusinessesReturn("sent", ""))
}

// accountLinkConfirm serves POST accountLinkBusinessesConfirmHref: the link this browser carries, spent
// with THIS browser's session (identity_link_by_email: the same identity's live session, any of its
// businesses -- R2, S16). The cookie is cleared whatever the answer. An unlinked session reaches the
// database and is refused there like any other wrong session (no budget is charged for it).
func (a *AdminAuth) accountLinkConfirm(w http.ResponseWriter, r *http.Request) {
	id, token, ok := a.accountIdentityBegin(w, r, "link_confirm")
	if !ok {
		return
	}
	ck, err := r.Cookie(accountLinkCookieName)
	a.short.clear(w, accountLinkCookieName)
	if err != nil || ck.Value == "" {
		a.redirect(w, linkBusinessesReturn("", "refused"))
		return
	}
	res, err := a.admins.LinkByEmail(r.Context(), ck.Value, token)
	if errors.Is(err, adminauth.ErrLinkRefused) {
		a.log.Info("panel link by e-mail refused", "actor_id", id.Admin.AdminUserID)
		a.redirect(w, linkBusinessesReturn("", "refused"))
		return
	}
	if err != nil {
		a.log.Error("panel: could not add the businesses", "actor_id", id.Admin.AdminUserID, "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	// A STABLE EVENT NAME and no count (ADR 0031 §5.15; R21): the number would tell this business's log
	// about the person's others.
	if res.Linked == 0 {
		a.log.Info("panel link by e-mail", "actor_id", id.Admin.AdminUserID, "outcome", "none", "first", res.FirstVerification)
		a.redirect(w, linkBusinessesReturn("none", ""))
		return
	}
	a.log.Info("panel link by e-mail", "actor_id", id.Admin.AdminUserID, "outcome", "linked", "first", res.FirstVerification)
	a.noticeBusinessesAdded(context.WithoutCancel(r.Context()), id, res)
	a.redirect(w, linkBusinessesReturn("added", ""))
}

// noticeBusinessesAdded sends the person ONE notice: how many businesses were added to the sign-in (the
// mailbox owner's own fact; no name). Best effort AFTER the commit: a notice that cannot be sent is
// logged, never a failed press -- the businesses are added either way.
func (a *AdminAuth) noticeBusinessesAdded(ctx context.Context, id httpx.AdminIdentity, res adminauth.LinkedBusinesses) {
	if a.inviteMail == nil || res.Recipient == "" {
		return
	}
	m, err := email.RenderBusinessesAdded(ctx, signInLink(a.inviteMail.baseURL), email.BusinessesAddedView{
		BaseURL: a.inviteMail.baseURL, Count: res.Linked,
	})
	if err != nil {
		a.log.Error("panel: could not render the businesses-added notice", "actor_id", id.Admin.AdminUserID, "err_type", fmt.Sprintf("%T", err))
		return
	}
	m.To = res.Recipient
	if _, err := a.inviteMail.sender.Send(ctx, id.TenantID().String(), m); err != nil {
		class, code, _ := sendErrorClass(err)
		a.log.Warn("panel link by e-mail: the notice was not sent", "actor_id", id.Admin.AdminUserID,
			"class", class, "smtp_code", code, "err_type", fmt.Sprintf("%T", err))
	}
}
