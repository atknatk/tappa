package handler

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/atknatk/tappa/internal/appdevice"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/liveqr"
)

// THE APP'S NFC-LESS ACTIVATION (M16-23): the two roads the browser has (activateapproval.go), for
// a device key. Both stand where complete's NFC read stands -- after the bind, signed by the bound
// key over the server's challenge -- and both end in the SAME device consumption, with no
// `transactions` row.
//
//	complete + liveQR   a venue screen's code read by the camera (M16-22's TapLink): the token is
//	                    opened by GET /q's reader and judged by liveQRActivationRefusal, the
//	                    browser's own rule.
//	approval            "My phone can't tap": a request bound to THIS key's binding, only where no
//	                    live QR screen runs (screen_available otherwise).
//	approval/status     the request's state; an approved one is collected IN THIS CALL
//	                    (ActivateDeviceApproved: the collection, the revocations and the device,
//	                    one transaction) and answered with the device, as complete answers.

// appApprovalRequest is both approval routes' body: the invitation, and the key the request is
// signed by (the bind's key), with its platform for the device a collection creates.
type appApprovalRequest struct {
	Code      string `json:"code"`
	PublicKey string `json:"publicKey"`
	Platform  string `json:"platform"`
}

// appApprovalResponse is the request's state. PairingCode and ExpiresAt only while waiting;
// DeviceID and EmployeeName only on the approval this call collected.
type appApprovalResponse struct {
	State        string `json:"state"`
	PairingCode  string `json:"pairingCode,omitempty"`
	ExpiresAt    string `json:"expiresAt,omitempty"`
	DeviceID     string `json:"deviceId,omitempty"`
	EmployeeName string `json:"employeeName,omitempty"`
}

// completeByLiveQR is ActivateComplete's live QR arm, after the proof of possession, the invitation
// and its budget: the token (GET /q's reader), the browser's activation rule for it, the binding's
// non-advancing pre-check, then ActivateDevice. A live QR advances no counter, so the binding is
// judged after the token here only for the order's symmetry with the browser; nothing is spent by
// either check.
func (a *App) completeByLiveQR(w http.ResponseWriter, r *http.Request, ip string, ictx invite.Context, in appCompleteRequest, label string, pub []byte, binding string) {
	ctx := r.Context() // see checkin.go: one level of paren depth for redline R7.
	if a.live == nil {
		// The live QR is not configured on this deployment (tap/start's answer): nothing to read.
		a.activation.failTap(ctx, ip, ictx, "live_qr_off", "")
		writeAppError(w, appErrBadRequest)
		return
	}
	token := ""
	if q, err := url.ParseQuery(in.LiveQR); err == nil {
		token = q.Get(liveqr.TokenParam)
	}
	o, err := a.live.open(ctx, token)
	if err != nil {
		a.log.ErrorContext(ctx, "app activation: resolving the screen failed", "invite_id", nonNil(ictx.InviteID), "err", err)
		writeAppError(w, appErrServer)
		return
	}
	if reason, display := liveQRActivationRefusal(o, ictx.TenantID, a.activation.clock()); reason != "" {
		a.activation.failScan(ctx, ip, ictx, reason, display)
		writeAppError(w, appErrActivationTapFailed)
		return
	}
	code := invite.ParseCode(in.Code)
	bound, err := a.invites.ConsentBound(ctx, code, invite.ParseBinding(binding))
	if err != nil {
		a.log.ErrorContext(ctx, "app activation: reading the consent failed", "invite_id", nonNil(ictx.InviteID), "err", err)
		writeAppError(w, appErrServer)
		return
	}
	if !bound {
		a.activation.failTap(ctx, ip, ictx, "consent_missing", "")
		writeAppError(w, appErrActivationNotReady)
		return
	}
	act, err := a.invites.ActivateDevice(ctx, code, invite.ParseBinding(binding), invite.DeviceParams{
		Platform: in.Platform, PublicKey: pub, Label: &label,
	})
	if err != nil {
		a.refuseDeviceActivation(w, r, ip, act.Context, err, "", "app_activate_complete")
		return
	}
	a.deviceActivated(w, r, act, label, activationTap{
		Proof: proofLiveQR, DisplayID: o.Screen.ID, LocationID: o.Screen.LocationID,
	})
}

// approvalCaller is what both approval routes establish before they act: the body, the platform's
// label, the key, the binding the key's signature yields, and the invitation.
type approvalCaller struct {
	in      appApprovalRequest
	label   string
	pub     []byte
	binding invite.Binding
	ictx    invite.Context
}

// boundCaller is complete's front half for the approval routes: the body, the proof of possession
// (BEFORE the invitation is read -- a caller who cannot sign learns nothing), the invitation and its
// budget. ok=false: answered. The address meter is the CALLER's (approvalFront / statusFront): the
// request is charged to the flood ceiling, the status poll to the status budget and never to the
// flood ceiling (M16-23 audit ORTA-1).
func (a *App) boundCaller(w http.ResponseWriter, r *http.Request, ip, where string) (approvalCaller, bool) {
	body, ok := readAppBody(w, r)
	if !ok {
		return approvalCaller{}, false
	}
	req, err := appdevice.FromHTTP(r, body)
	if err != nil {
		a.activation.anonFail(ip, where, "malformed signed request")
		writeAppUnauthenticated(w)
		return approvalCaller{}, false
	}
	var in appApprovalRequest
	if !decodeAppJSON(body, &in) || in.Code == "" {
		a.activation.anonFail(ip, where, "malformed body")
		writeAppError(w, appErrBadRequest)
		return approvalCaller{}, false
	}
	label, known := appPlatforms[in.Platform]
	pub, keyOK := decodePublicKey(in.PublicKey)
	if !known || !keyOK {
		a.activation.anonFail(ip, where, "malformed body")
		writeAppError(w, appErrBadRequest)
		return approvalCaller{}, false
	}
	binding, err := appdevice.VerifyActivation(req, pub)
	if err != nil {
		a.activation.anonFail(ip, where, "proof of possession refused")
		writeAppUnauthenticated(w)
		return approvalCaller{}, false
	}
	ictx, err := a.invites.Lookup(r.Context(), invite.ParseCode(in.Code))
	if a.activation.inviteBudgetSpent(r, ip, ictx) {
		writeAppError(w, appErrTooMany)
		return approvalCaller{}, false
	}
	if err != nil {
		a.rejectAppCode(w, r, ip, ictx, err, where)
		return approvalCaller{}, false
	}
	return approvalCaller{in: in, label: label, pub: pub, binding: invite.ParseBinding(binding), ictx: ictx}, true
}

// ActivateApproval serves POST /app/v1/activate/approval -- RequestApproval's twin: a request for
// a manager's approval, bound to THIS key's binding. It spends nothing and creates no device.
func (a *App) ActivateApproval(w http.ResponseWriter, r *http.Request) {
	r, settle := holdInviteBudget(r)
	defer settle()
	ctx := r.Context() // see checkin.go: one level of paren depth for redline R7.
	ip := clientIP(r)
	if a.activation.floodSpent(ip, "app_activate_approval") {
		writeAppError(w, appErrTooMany)
		return
	}
	c, ok := a.boundCaller(w, r, ip, "app_activate_approval")
	if !ok {
		return
	}
	if within, n := a.activation.approvalLimiter.TryCharge(c.ictx.InviteID.String()); !within {
		if a.activation.approvalLimiter.FirstOverLimit(n) {
			a.activation.failAttempt(ctx, ip, c.ictx, "approval_requests_limited", againstIPOnly)
		}
		writeAppError(w, appErrTooMany)
		return
	}
	ap, err := a.invites.OpenApproval(ctx, invite.ParseCode(c.in.Code), c.binding, invite.ApprovalClientApp)
	switch {
	case errors.Is(err, invite.ErrScreenAvailable):
		a.activation.failAttempt(ctx, ip, ap.Context, "screen_available", againstIPOnly)
		writeAppError(w, appErrScreenAvailable)
		return
	case errors.Is(err, invite.ErrConsentMissing):
		a.activation.failTap(ctx, ip, ap.Context, "consent_missing", "")
		writeAppError(w, appErrActivationNotReady)
		return
	case err != nil:
		a.rejectAppCode(w, r, ip, ap.Context, err, "app_activate_approval")
		return
	}
	a.activation.record(ctx, audit.Event{
		TenantID: ap.TenantID,
		Action:   ActionApprovalRequested,
		Target:   ap.EmployeeID.String(),
		Detail: activationDetail{
			InviteID:  ap.InviteID.String(),
			Outcome:   "ok",
			RequestID: ap.RequestID.String(),
			Device:    c.label,
			Client:    clientApp,
		},
	})
	// The pairing code's egress: this body, to the phone that asked (and the panel's list).
	a.writeAppJSON(w, r, http.StatusOK, appApprovalResponse{
		State:       invite.ApprovalWaiting,
		PairingCode: ap.PairingCode.Digits(),
		ExpiresAt:   ap.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

// ActivateApprovalStatus serves POST /app/v1/activate/approval/status: the request's state, and on
// an approval the device -- collected here, in ActivateDeviceApproved's one transaction. It is
// metered on the wizard's status budget per address (ApprovalStatus's, the browser twin's) and
// NEVER on the flood ceiling, so a phone waiting fifteen minutes cannot spend what a colleague's
// activation on the same network needs (audit ORTA-1: before this, the 598th poll of one phone
// left a colleague's lookup on the same address a 429 --
// TestAppNFCLessDB_StatusPollsLeaveTheFloodCeilingAlone).
func (a *App) ActivateApprovalStatus(w http.ResponseWriter, r *http.Request) {
	r, settle := holdInviteBudget(r)
	defer settle()
	ctx := r.Context() // see checkin.go: one level of paren depth for redline R7.
	ip := clientIP(r)
	if n := a.activation.statusLimiter.Charge(ip); n > statusLimit {
		writeAppError(w, appErrTooMany)
		return
	}
	c, ok := a.boundCaller(w, r, ip, "app_activate_approval_status")
	if !ok {
		return
	}
	code := invite.ParseCode(c.in.Code)
	ap, err := a.invites.ApprovalState(ctx, code, c.binding)
	switch {
	case errors.Is(err, invite.ErrNoApproval):
		a.writeAppJSON(w, r, http.StatusOK, appApprovalResponse{State: invite.ApprovalExpired})
		return
	case err != nil:
		a.rejectAppCode(w, r, ip, ap.Context, err, "app_activate_approval_status")
		return
	}
	switch ap.State {
	case invite.ApprovalWaiting:
		a.writeAppJSON(w, r, http.StatusOK, appApprovalResponse{
			State: ap.State, PairingCode: ap.PairingCode.Digits(), ExpiresAt: ap.ExpiresAt.UTC().Format(time.RFC3339),
		})
		return
	case invite.ApprovalRejected, invite.ApprovalExpired, invite.ApprovalCollected:
		// Collected by another call of this key is not reachable (the device then exists and the
		// invitation is spent -- Lookup above refuses first); "expired" is the honest word for it.
		state := ap.State
		if state == invite.ApprovalCollected {
			state = invite.ApprovalExpired
		}
		a.writeAppJSON(w, r, http.StatusOK, appApprovalResponse{State: state})
		return
	}
	act, err := a.invites.ActivateDeviceApproved(ctx, code, c.binding, invite.DeviceParams{
		Platform: c.in.Platform, PublicKey: c.pub, Label: &c.label,
	})
	if errors.Is(err, invite.ErrApprovalNotGranted) {
		// It ran out between the read and the collection; nothing was consumed.
		a.activation.failTap(ctx, ip, act.Context, "approval_not_granted", "")
		a.writeAppJSON(w, r, http.StatusOK, appApprovalResponse{State: invite.ApprovalExpired})
		return
	}
	if err != nil {
		a.refuseDeviceActivation(w, r, ip, act.Context, err, "", "app_activate_approval_status")
		return
	}
	a.deviceActivatedOnApproval(w, r, act, c.label)
}

// deviceActivatedOnApproval is deviceActivated for a collected approval, answered in the status
// route's shape.
func (a *App) deviceActivatedOnApproval(w http.ResponseWriter, r *http.Request, act invite.ApprovedDeviceActivation, label string) {
	a.recordDeviceActivation(r.Context(), act.DeviceActivation, label, activationTap{
		Proof: proofManagerApproval, RequestID: act.RequestID, ApprovedBy: act.ApprovedBy,
	})
	a.writeAppJSON(w, r, http.StatusOK, appApprovalResponse{
		State: invite.ApprovalApproved, DeviceID: act.DeviceID.String(), EmployeeName: act.FullName,
	})
}
