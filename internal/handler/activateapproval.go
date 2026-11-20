package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/web/templates/pages"
)

// ACTIVATING A PHONE THAT CANNOT TAP (M16-23; ADR 0026's "Bedel 3" withdrawn). The wizard's two
// steps stay -- a consent bound to this browser, then a proof that completes it -- and the proof
// no longer has to be an NFC plaque tap:
//
//	a LIVE QR screen   the camera opens a venue screen's rotating code (GET /q) in a browser that
//	                   holds a consented activation: CompleteByLiveQR, CompleteByTap's order with
//	                   the token's verification where sun.Verify stands.
//	a MANAGER          "My phone can't tap" on the waiting screen opens a request with a six-digit
//	                   code (RequestApproval) -- ONLY where no live QR screen runs at the person's
//	                   venue, refused by the server elsewhere; a manager approves it in the panel;
//	                   this browser's next look (ApprovalStatus, then CollectApproval) spends the
//	                   invitation and collects the approval in one transaction.
//
// NEITHER WRITES A `transactions` ROW (CLAUDE.md §5, "Aktivasyon dokunuşu"; §5 row 3): the
// activation is not attendance, and the next tap or scan is an ordinary check-in. Every refusal
// that has a tenant is an activation.failed row in audit_log (§4.6) -- that is where Q39's
// "a known person's attempt is recorded" lands for an activation: the person is known by the
// invitation, not by a session, so the record is the trail's, not an attendance record's.
//
//	GET  /activate/approval          the code to show, and where the request stands
//	POST /activate/approval          open a request ("My phone can't tap", "Ask again")
//	POST /activate/approval/collect  finish an approved request ("Check now", or the script)
//	GET  /activate/approval/status   the screen's poll: waiting / approved / rejected / expired / none

// The approval routes. Constants because the templates, the script's data attribute and the
// router must agree.
const (
	ActivationApprovalPath        = "/activate/approval"
	ActivationApprovalCollectPath = "/activate/approval/collect"
	ActivationApprovalStatusPath  = "/activate/approval/status"
)

// approvalOpenLimit / approvalOpenPeriod: how many requests one invitation may open per hour. A
// person asks once or twice ("Ask again" after a rejection or a lapse); a request row is never
// deleted (00115), so a loop must stop well before it fills the table.
const (
	approvalOpenLimit  = 10
	approvalOpenPeriod = time.Hour
)

// approvalOffered is the gate as the waiting screen draws it. A failed read draws no button --
// the scan line instead -- and says so in the log: offering a path the server might refuse is
// worse than pointing at the screen, and the request itself re-checks.
func (a *Activation) approvalOffered(ctx context.Context, ictx invite.Context) bool {
	ok, err := a.invites.ApprovalOffered(ctx, ictx.TenantID, ictx.LocationID)
	if err != nil {
		a.log.ErrorContext(ctx, "activation: reading whether a live QR screen runs failed; offering no approval",
			"tenant_id", ictx.TenantID, "err", err)
		return false
	}
	return ok
}

// pendingFor reads this browser's CONSENTED activation and its invitation for an approval route.
// ok=false means the response is already written: no cookie (the no-link landing), a cookie
// without consent (back to the wizard), an exhausted budget, or a dead invitation (rejectCode).
func (a *Activation) pendingFor(w http.ResponseWriter, r *http.Request, ip, where string) (activationState, invite.Context, bool) {
	st, ok := a.codes.read(r)
	if !ok {
		a.landWithoutLink(w, r)
		return activationState{}, invite.Context{}, false
	}
	if !st.pending() {
		a.redirect(w, r, "/activate")
		return activationState{}, invite.Context{}, false
	}
	ictx, err := a.invites.Lookup(r.Context(), st.code)
	if a.overInviteBudget(w, r, ip, ictx) {
		return activationState{}, invite.Context{}, false
	}
	if err != nil {
		if _, classified := inviteFailureReason(err); classified || errors.Is(err, invite.ErrUnknownCode) {
			a.codes.clear(w)
		}
		a.rejectCode(w, r, ip, ictx, err, where)
		return activationState{}, invite.Context{}, false
	}
	return st, ictx, true
}

// postedFor is pendingFor for the two POSTs, plus the Origin belt and the synchronizer token
// (Submit's measures 1 and the origin check, for the same reasons).
func (a *Activation) postedFor(w http.ResponseWriter, r *http.Request, ip, where string) (activationState, invite.Context, bool) {
	if _, ok := a.codes.read(r); ok && !a.sameOrigin(r, false) {
		a.anonFail(ip, where, "foreign origin")
		a.renderProblem(w, r, http.StatusBadRequest, problemBadLink)
		return activationState{}, invite.Context{}, false
	}
	st, ictx, ok := a.pendingFor(w, r, ip, where)
	if !ok {
		return st, ictx, false
	}
	if err := r.ParseForm(); err != nil {
		a.failAttempt(r.Context(), ip, ictx, "malformed_form", againstInvite)
		a.renderProblem(w, r, http.StatusBadRequest, problemServer)
		return activationState{}, invite.Context{}, false
	}
	if !st.csrfMatches(r.PostFormValue("csrf")) {
		a.failAttempt(r.Context(), ip, ictx, "csrf_mismatch", againstInvite)
		a.log.Warn("activation approval rejected: csrf mismatch", "ip", ip, "invite_id", nonNil(ictx.InviteID))
		a.renderProblem(w, r, http.StatusBadRequest, problemBadLink)
		return activationState{}, invite.Context{}, false
	}
	return st, ictx, true
}

// RequestApproval serves POST /activate/approval: "My phone can't tap" (and "Ask again"). It opens
// a request bound to this browser's consent and sends the browser to the code. It spends nothing.
//
// ORDER: flood ceiling · cookie (consented) · origin · lookup · per-invitation budget · form token ·
// the invitation's own request budget · OpenApproval (the gate, the binding, the withdrawal of an
// older request and the new one -- one transaction) · the trail row.
func (a *Activation) RequestApproval(w http.ResponseWriter, r *http.Request) {
	r, settle := holdInviteBudget(r)
	defer settle()
	ip := clientIP(r)
	if a.flooded(w, r, ip, "activate_approval") {
		return
	}
	st, ictx, ok := a.postedFor(w, r, ip, "activate_approval")
	if !ok {
		return
	}
	if within, n := a.approvalLimiter.TryCharge(ictx.InviteID.String()); !within {
		if a.approvalLimiter.FirstOverLimit(n) {
			a.failAttempt(r.Context(), ip, ictx, "approval_requests_limited", againstIPOnly)
		}
		a.renderProblem(w, r, http.StatusTooManyRequests, problemTooMany)
		return
	}
	ap, err := a.invites.OpenApproval(r.Context(), st.code, st.binding, invite.ApprovalClientBrowser)
	switch {
	case errors.Is(err, invite.ErrScreenAvailable):
		// THE GATE, ON THE SERVER: a screen runs where this person works. Recorded, charged to the
		// IP window only -- a person pressing a button the page did not draw for a second is not
		// an attack -- and answered with the screen to scan.
		a.failAttempt(r.Context(), ip, ap.Context, "screen_available", againstIPOnly)
		a.renderProblem(w, r, http.StatusConflict, problemApprovalScreenAvailable)
		return
	case errors.Is(err, invite.ErrConsentMissing):
		// Another browser agreed since: this one's binding is stale (finishActivation's answer).
		a.failTap(r.Context(), ip, ap.Context, "consent_missing", "")
		a.codes.rebind(w, r, "", ictx.ExpiresAt)
		a.renderProblem(w, r, http.StatusConflict, problemActivationNotReady)
		return
	case err != nil:
		a.rejectCode(w, r, ip, ap.Context, err, "activate_approval")
		return
	}
	a.record(r.Context(), audit.Event{
		TenantID: ap.TenantID,
		Action:   ActionApprovalRequested,
		Target:   ap.EmployeeID.String(),
		Detail: activationDetail{
			InviteID:  ap.InviteID.String(),
			Outcome:   "ok",
			RequestID: ap.RequestID.String(),
			Device:    coarseDevice(r.UserAgent()),
		},
	})
	a.redirect(w, r, ActivationApprovalPath)
}

// ApprovalPage serves GET /activate/approval: the code and the request's state. It writes nothing
// but the trail of a dead invitation (rejectCode, as GET /activate does).
func (a *Activation) ApprovalPage(w http.ResponseWriter, r *http.Request) {
	r, settle := holdInviteBudget(r)
	defer settle()
	ip := clientIP(r)
	if a.flooded(w, r, ip, "activate_approval_page") {
		return
	}
	st, ictx, ok := a.pendingFor(w, r, ip, "activate_approval_page")
	if !ok {
		return
	}
	ap, err := a.invites.ApprovalState(r.Context(), st.code, st.binding)
	switch {
	case errors.Is(err, invite.ErrNoApproval):
		a.redirect(w, r, "/activate?step=3")
		return
	case err != nil:
		a.rejectCode(w, r, ip, ap.Context, err, "activate_approval_page")
		return
	}
	if ap.State == invite.ApprovalCollected {
		// Spent already: the activation it completed is done (the cookie is normally gone by now).
		a.redirect(w, r, "/activate")
		return
	}
	v := pages.ApprovalView{
		EmployerName: ictx.TenantName,
		State:        ap.State,
		Minutes:      int(invite.ApprovalTTL / time.Minute),
		CSRFToken:    st.csrf,
		StatusURL:    ActivationApprovalStatusPath,
		BackURL:      "/activate?step=3",
	}
	if ap.State == invite.ApprovalWaiting {
		v.Code = groupedPairingCode(ap.PairingCode)
	}
	look := a.lookFor(r.Context(), ictx.TenantID, wizardLogo)
	a.render(w, r, http.StatusOK, pages.Approval(v, look), look.Logo().Drawn())
}

// groupedPairingCode is the code as the phone and the panel show it: "482 913".
func groupedPairingCode(p invite.PairingCode) string {
	d := p.Digits()
	if len(d) != 6 {
		return d
	}
	return d[:3] + " " + d[3:]
}

// CollectApproval serves POST /activate/approval/collect: an approved request finishes the setup
// (finishActivation, the one place a session is born from an invitation); any other state sends the
// browser back to the approval page, which draws it.
func (a *Activation) CollectApproval(w http.ResponseWriter, r *http.Request) {
	r, settle := holdInviteBudget(r)
	defer settle()
	ip := clientIP(r)
	if a.flooded(w, r, ip, "activate_approval_collect") {
		return
	}
	st, _, ok := a.postedFor(w, r, ip, "activate_approval_collect")
	if !ok {
		return
	}
	ap, err := a.invites.ApprovalState(r.Context(), st.code, st.binding)
	switch {
	case errors.Is(err, invite.ErrNoApproval):
		a.redirect(w, r, "/activate?step=3")
		return
	case err != nil:
		a.rejectCode(w, r, ip, ap.Context, err, "activate_approval_collect")
		return
	}
	if ap.State != invite.ApprovalApproved {
		a.redirect(w, r, ActivationApprovalPath)
		return
	}
	a.finishActivation(w, r, ip, st, activationTap{Proof: proofManagerApproval})
}

// ApprovalStatus serves GET /activate/approval/status -- the approval screen's poll, Status's twin:
// it writes nothing, records nothing, names nobody and is metered on the status budget.
//
//	{"state":"waiting"|"approved"|"rejected"|"expired"}  this browser's request
//	{"state":"none"}                                    nothing to wait for
func (a *Activation) ApprovalStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if n := a.statusLimiter.Charge(clientIP(r)); n > statusLimit {
		w.Header().Set("Retry-After", "600")
		a.writeStatus(w, http.StatusTooManyRequests, "slow_down")
		return
	}
	st, ok := a.codes.read(r)
	if !ok || !st.pending() {
		a.writeStatus(w, http.StatusOK, "none")
		return
	}
	ap, err := a.invites.ApprovalState(r.Context(), st.code, st.binding)
	switch {
	case err == nil:
	case errors.Is(err, invite.ErrUnknownCode), errors.Is(err, invite.ErrNoApproval):
		a.writeStatus(w, http.StatusOK, "none")
		return
	default:
		if _, classified := inviteFailureReason(err); classified {
			a.writeStatus(w, http.StatusOK, "none")
			return
		}
		a.log.Error("activation approval status: reading the request failed", "err", err)
		a.writeStatus(w, http.StatusServiceUnavailable, "unknown")
		return
	}
	switch ap.State {
	case invite.ApprovalWaiting, invite.ApprovalApproved, invite.ApprovalRejected, invite.ApprovalExpired:
		a.writeStatus(w, http.StatusOK, ap.State)
	default:
		a.writeStatus(w, http.StatusOK, "none")
	}
}

// liveQROpener opens the request's live QR token: GET /q's reader (Tap.openLiveQR's liveQRTap.open),
// handed in by the Tap handler so the activation runs it at ITS point in the order, after the
// flood ceiling and the invitation.
type liveQROpener func(ctx context.Context) (liveQROpening, error)

// CompleteByLiveQR is the activating SCAN (M16-23): GET /q from a browser holding a consented
// activation. The Tap handler calls it when Pending is true.
//
// ORDER -- CompleteByTap's, with the token's verification where sun.Verify stands (the card):
//
//	· flood ceiling       before any database work.
//	· lookup              the invitation must still be usable; a dead one clears the cookie.
//	· the token           liveQRTap.open: its shape and tag (constant time), the screen it names
//	                      resolved from its row, the row's venue equal to the token's. It advances
//	                      nothing -- a live QR has no counter; its freshness is its window.
//	· the screen          the SAME tenant as the invitation (a peer's or a stranger's screen is
//	                      refused, and its id is not written into this tenant's trail -- §4.5), in
//	                      service: not revoked and its venue active (sys:screen-not-active's
//	                      condition).
//	· freshness           the token's own expiry plus the network grace the tap path allows
//	                      (policy.LiveQRNetworkGraceSeconds, Q39 (b)'s number), measured at THIS
//	                      moment -- an activation has no page, so the server first sees the token
//	                      now (tap.Input.PageIssuedAt's zero rule). A photographed code is refused:
//	                      it is at most a static QR's evidence, and a static QR cannot activate.
//	· finishActivation    consume, revoke a replaced phone, issue, set the cookie.
//
// EVERY REFUSAL IS ONE SCREEN (problemActivationScanFailed, §4.7) and an activation.failed row
// with its reason (§4.6); none writes a `transactions` row (Q39's attendance rows are for a session
// that taps; an activation's attempt is recorded in the trail).
//
// WHAT IT DOES NOT CLOSE, stated: a live QR code is valid for anybody for its ~20 s window. A
// colleague who photographs the screen and sends the picture at once lets a consented phone
// elsewhere finish its setup inside that window -- the same exposure the live QR's check-in
// accepts (ADR 0045 §2.5), bounded here by the invitation (one person, one consumption) and the
// consent (only the browser that agreed).
func (a *Activation) CompleteByLiveQR(w http.ResponseWriter, r *http.Request, open liveQROpener) {
	r, settle := holdInviteBudget(r)
	defer settle()
	ip := clientIP(r)
	if a.flooded(w, r, ip, "activate_scan") {
		return
	}
	st, ok := a.codes.read(r)
	if !ok || !st.pending() {
		a.redirect(w, r, "/activate")
		return
	}
	ictx, err := a.invites.Lookup(r.Context(), st.code)
	if a.overInviteBudget(w, r, ip, ictx) {
		return
	}
	if err != nil {
		if _, classified := inviteFailureReason(err); classified || errors.Is(err, invite.ErrUnknownCode) {
			a.codes.clear(w)
		}
		a.rejectCode(w, r, ip, ictx, err, "activate_scan")
		return
	}
	o, err := open(r.Context())
	if err != nil {
		a.log.Error("activation scan: resolving the screen failed", "invite_id", nonNil(ictx.InviteID), "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemServer)
		return
	}
	reason, display := liveQRActivationRefusal(o, ictx.TenantID, a.clock())
	if reason != "" {
		a.failScan(r.Context(), ip, ictx, reason, display)
		a.renderProblem(w, r, http.StatusBadRequest, problemActivationScanFailed)
		return
	}
	a.finishActivation(w, r, ip, st, activationTap{
		Proof: proofLiveQR, DisplayID: o.Screen.ID, LocationID: o.Screen.LocationID,
	})
}

// liveQRActivationRefusal judges an opened token for an activation in tenant: "" when the token
// verified, names a screen of that tenant in service, and is still inside its window plus the
// network grace at now; otherwise the audit reason and -- for a screen of the SAME tenant only --
// its id. ONE function for the browser and the app (App.ActivateComplete), so the two cannot
// disagree about what may activate.
func liveQRActivationRefusal(o liveQROpening, tenantID uuid.UUID, now time.Time) (string, uuid.UUID) {
	switch {
	case !o.Verified:
		return "live_qr_" + o.Refusal, uuid.Nil
	case o.Screen.TenantID != tenantID:
		// Another business's screen: its id is not written into this tenant's trail (§4.5).
		return "foreign_tenant_screen", uuid.Nil
	case o.Screen.RevokedAt != nil || !o.Screen.LocationActive:
		return "screen_not_active", o.Screen.ID
	case now.Sub(o.Claims.ExpiresAt()) > time.Duration(policy.LiveQRNetworkGraceSeconds)*time.Second:
		return "live_qr_expired", o.Screen.ID
	}
	return "", uuid.Nil
}

// failScan is failTap for a live QR scan: the screen's id instead of a plaque's uid, written only
// for a screen of the invitation's own tenant (the caller passes uuid.Nil otherwise).
func (a *Activation) failScan(ctx context.Context, ip string, ictx invite.Context, reason string, displayID uuid.UUID) {
	if ictx.InviteID != uuid.Nil {
		a.chargeInvite(ctx, ictx.InviteID.String())
	}
	if ictx.TenantID == uuid.Nil {
		return
	}
	a.record(ctx, audit.Event{
		TenantID: ictx.TenantID,
		Action:   ActionActivationFailed,
		Target:   ictx.EmployeeID.String(),
		Detail: activationDetail{
			InviteID:  nonNil(ictx.InviteID),
			Outcome:   "rejected",
			Reason:    reason,
			Proof:     proofLiveQR,
			DisplayID: nonNil(displayID),
		},
	})
	a.log.Info("activation scan rejected", "reason", reason, "ip", ip,
		"employee_id", ictx.EmployeeID, "invite_id", nonNil(ictx.InviteID))
}
