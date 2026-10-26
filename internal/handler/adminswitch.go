package handler

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// SWITCHING BUSINESSES WITHOUT SIGNING OUT (M14-10, ADR 0031 §5.4, §8).
//
//	GET  /admin/switch   the person's businesses, one switch form per other business. The route's
//	                     minimum: M14-13 designs the page.
//	POST /admin/switch   tenant_id + csrf: a NEW session in the target business, the old one
//	                     revoked -- one definer call, one transaction (adminauth.Switch,
//	                     identity_switch_session, 00071), then a 303 into the panel.
//
// 🔴 FLAG-GATED, BOTH ROUTES. They exist only while TAPPA_ADMIN_IDENTITY is on (mountGated,
// adminauth.SurfaceSwitch): closed, /admin/switch is a path the panel never had, for every method.
//
// 🔴 THE CHAIN IS protectWritingThroughSuspension (ADR 0031 §5.4; ADR 0025's M14 note): flood ->
// Origin (ahead of the resolver) -> identity -> session budget -> the tenant binding of the form's
// action (§5.5, formbinding.go) -- and NOT the suspension ring. Moving into or out of a suspended
// business must stay possible (sign-in and reading are open during a suspension, ADR 0025 §4,
// K-10); the business switched INTO is read-only afterwards because its next write meets the
// suspension gate like any other.
//
// 🔴 PLUS A SYNCHRONIZER TOKEN OF ITS OWN, unlike every other panel write: the form's csrf field
// is HMAC(key from the RAW session token, "tappa/admin-switch/v1|" + session_id)
// (adminauth.SwitchTokens), compared in constant time BEFORE the budget is charged and before the
// database is asked anything. A forged switch moves the browser rather than a record, so the
// write that changes WHICH business every later press lands in carries the strongest proof the
// panel has that the request came from a page this session drew.
//
// 🔴 THE BUDGET IS THE PERSON'S (+ the address), NOT THE SESSION'S: every switch is a new
// session, so sessionGate's per-session count restarts on each hop. adminSwitchLimit carries
// the arithmetic; a refusal there writes nothing -- no session, no audit row.
//
// 🔴 ONE REFUSAL, AND IT TOUCHES NO COOKIE. "No such business", "not a member", "disabled", "the
// current business" and "a concurrent switch of the same session won" are one answer
// (adminauth.ErrSwitchRefused) with one page -- and that answer writes no Set-Cookie: the cookie
// in the browser stays whatever it is, so a loser of a concurrent race cannot overwrite or delete
// the winner's (ADR 0031 §5.4, bulgu 29).
//
// 🔴 THE AUDIT ROWS ARE THE DATABASE'S. identity_switch_session writes the business left an
// ordinary sign-out row and the business entered an ordinary sign-in row, neither naming the
// other; this handler writes no row on any path (a second row would double each trail, and a
// row that said "switch" would tell one business the person has another).

// adminSwitchHref is the switch's one path, both methods. Under /admin, the panel cookie's path.
const adminSwitchHref = "/admin/switch"

// adminSwitchMaxBody bounds the switch form's body: two fields, a uuid and a 43-character token,
// are under 200 bytes; 4 KiB leaves room for a browser's encoding and nothing a flood can use.
const adminSwitchMaxBody = 4 << 10

// The switch's problem pages.
var (
	// problemSwitchRefused is the ONE refusal of the target (see above): it must read the same
	// whatever the reason.
	problemSwitchRefused = pages.ProblemView{
		Title:   "You can't switch to that business",
		Message: "You are still in the business you were in. Nothing was changed.",
		Hint:    "Open the list of your businesses again and pick one from there.",
	}
	// problemSwitchForged answers a POST without this session's synchronizer token: a page drawn
	// for another session (one that has since switched or signed out), or a request no page of
	// ours drew.
	problemSwitchForged = pages.ProblemView{
		Title:   "That switch came from an old page",
		Message: "It was opened before you last signed in or switched, so nothing was changed.",
		Hint:    "Open the list of your businesses again and pick one from there.",
	}
	// problemSwitchTooMany is the budget's 429.
	problemSwitchTooMany = pages.ProblemView{
		Title:   "Too many switches",
		Message: "You have switched businesses many times in a few minutes, so we have paused switching from here.",
		Hint:    "Wait a few minutes and try again. You are still in the business you were in.",
	}
	// problemSwitchUnavailable is the database failing: one transaction, so nothing was switched,
	// and the cookie is kept.
	problemSwitchUnavailable = pages.ProblemView{
		Title:   "We could not switch you",
		Message: "Something went wrong on our side, so nothing was changed. You are still in the business you were in.",
		Hint:    "This one is ours rather than yours — please try again in a moment.",
	}
)

// adminSwitchKey is the switch budget's key: the person (adminauth.Resolved.SwitchBucket -- an
// opaque value, never the identity id) and the address. An unlinked row has no person; its
// switch is refused by the database anyway, and it is keyed by its session, which a refused
// switch does not replace.
func adminSwitchKey(id httpx.AdminIdentity, addr string) string {
	if b, ok := id.Admin.SwitchBucket(); ok {
		return "identity:" + b + "|" + addr
	}
	return "session:" + id.Admin.SessionID.String() + "|" + addr
}

// adminSwitchPage serves GET /admin/switch.
func (a *AdminAuth) adminSwitchPage(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !id.Live() {
		a.requireLogin(w, r)
		return
	}
	// The session's own token, read again from the cookie the gate just verified: the list and
	// the synchronizer are keyed by it. It is never logged.
	token, err := a.cookies.Read(r)
	if err != nil {
		a.log.Error("panel: the verified session's cookie could not be read again", "actor_id", id.Admin.AdminUserID)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	csrf, err := a.switches.Mint(token, id.Admin.SessionID)
	if err != nil {
		a.log.Error("panel: could not mint the switch synchronizer", "actor_id", id.Admin.AdminUserID, "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	memberships, err := a.admins.Memberships(r.Context(), token)
	if err != nil {
		a.log.Error("panel: could not list the person's businesses", "actor_id", id.Admin.AdminUserID, "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	v := pages.AdminSwitchView{CSRFToken: csrf, SwitchHref: adminSwitchHref, BackHref: transactionsHref}
	for _, m := range memberships {
		if m.Current {
			v.Current = m.TenantName
			continue
		}
		v.Others = append(v.Others, pages.AdminSwitchBusiness{TenantID: m.TenantID.String(), TenantName: m.TenantName, Role: m.Role})
	}
	a.render(w, r, http.StatusOK, pages.AdminSwitch(v))
}

// adminSwitch serves POST /admin/switch.
func (a *AdminAuth) adminSwitch(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	// The writing chain guarantees a live admin ahead of this; id.Live() is the belt.
	if !id.Live() {
		a.log.Warn("panel switch refused: no live admin session")
		http.Error(w, "sign in", http.StatusUnauthorized)
		return
	}
	token, err := a.cookies.Read(r)
	if err != nil {
		a.log.Error("panel: the verified session's cookie could not be read again", "actor_id", id.Admin.AdminUserID)
		a.renderProblem(w, r, http.StatusInternalServerError, problemSwitchUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, adminSwitchMaxBody)
	if err := r.ParseForm(); err != nil {
		a.renderProblem(w, r, http.StatusBadRequest, problemSwitchForged)
		return
	}
	// THE SYNCHRONIZER, FIRST: nothing below -- the budget, the database -- is reached by a
	// request no page of this session drew. The value is never logged.
	if !a.switches.Matches(token, id.Admin.SessionID, r.PostForm.Get("csrf")) {
		a.log.Warn("panel switch refused: synchronizer missing or wrong", "actor_id", id.Admin.AdminUserID)
		a.renderProblem(w, r, http.StatusBadRequest, problemSwitchForged)
		return
	}
	if n := a.switchLimiter.Charge(adminSwitchKey(id, clientIP(r))); n > adminSwitchLimit {
		if a.switchLimiter.FirstOverLimit(n) {
			a.log.Warn("panel switch budget reached", "actor_id", id.Admin.AdminUserID,
				"limit", adminSwitchLimit, "period", adminSwitchPeriod.String())
		}
		a.renderProblem(w, r, http.StatusTooManyRequests, problemSwitchTooMany)
		return
	}
	// A target that is not a uuid is the same refusal as one that is not a membership.
	target, err := uuid.Parse(r.PostForm.Get("tenant_id"))
	if err != nil {
		target = uuid.Nil
	}
	issued, err := a.admins.Switch(r.Context(), token, target)
	if errors.Is(err, adminauth.ErrSwitchRefused) || errors.Is(err, adminauth.ErrNoSession) {
		// A STABLE EVENT NAME (ADR 0031 §5.15: the switch-refusal counter is computed from it). No
		// target in the line: it would name another business beside this one's actor.
		a.log.Warn("panel switch refused", "actor_id", id.Admin.AdminUserID)
		a.renderProblem(w, r, http.StatusForbidden, problemSwitchRefused)
		return
	}
	if err != nil {
		a.log.Error("panel: switching business failed", "actor_id", id.Admin.AdminUserID, "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemSwitchUnavailable)
		return
	}
	if err := a.cookies.Set(w, issued.Token); err != nil {
		// Unreachable in practice (Set fails only on an empty token, and Switch never returns
		// one): the switch committed and the old session is revoked, so the browser is signed out
		// and the sign-in page is the honest way on.
		a.log.Error("panel: writing the switched session's cookie failed", "actor_id", id.Admin.AdminUserID, "err", err)
		a.redirect(w, adminLoginPath)
		return
	}
	a.log.Info("panel switched business", "actor_id", id.Admin.AdminUserID)
	// POST-redirect-GET into the panel's home: no binding, no target in the address.
	a.redirect(w, transactionsHref)
}
