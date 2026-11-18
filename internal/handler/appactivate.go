package handler

import (
	"encoding/base64"
	"errors"
	"net/http"

	"github.com/atknatk/tappa/internal/appdevice"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/sun"
)

// THE APP'S ACTIVATION (ADR 0026's two steps for a device key; ADR 0045 §2.3; M16-07). The invite
// link is NOT linked to the app (Q40, M16-08): it opens the browser wizard, and the app's own
// activation screen takes the invite CODE the person types or pastes. From the code on, the app
// walks the wizard's steps through three routes:
//
//	lookup    the invitation's facts for the consent screen -- the employer (GDPR Art. 13's
//	          controller), the person, the venue, the brand. Spends nothing.
//	bind      consent + the public key: records the consent on the invitation, bound to THIS key
//	          and a challenge the server draws (appdevice.ActivationBinding). Spends nothing,
//	          creates no device. A later bind -- or a browser consent -- MOVES the binding.
//	complete  signed by the bound key over the challenge, carrying the first genuine NFC read:
//	          CompleteByTap's order exactly (below), then ONE transaction consumes the invitation,
//	          revokes the person's browser sessions and app devices (Q40 (a)) and creates the
//	          device. No transactions row: the activating touch is not attendance.
//
// THE BUDGETS ARE THE WIZARD'S (Activation's limiters): one flood ceiling per address and one
// failure budget per invitation, whichever client presents it -- the app cannot be used to get a
// second budget against the same invitation.

// appPlatforms is the closed set app_devices.platform accepts (00108's CHECK), and the coarse label
// each gets: the label is the SERVER's, from the platform -- the app sends no free text that would
// reach the panel's device list (sessions.device_info's promise: never derived from probing).
var appPlatforms = map[string]string{"ios": "iPhone", "android": "Android"}

// clientApp is the audit detail's Client for the app's rows.
const clientApp = "app"

// --- POST /app/v1/activate/lookup ------------------------------------------------------------

type appLookupRequest struct {
	Code string `json:"code"`
}

type appLookupResponse struct {
	EmployeeName string `json:"employeeName"`
	BusinessName string `json:"businessName"`
	LocationName string `json:"locationName,omitempty"`
	WiFiSSID     string `json:"wifiSsid,omitempty"`
	// SecondDevice: the person is already active -- this activation signs their other phone out
	// (the browser wizard's warning).
	SecondDevice bool     `json:"secondDevice"`
	Brand        appBrand `json:"brand"`
}

// ActivateLookup serves POST /app/v1/activate/lookup -- GET /activate's twin for the app: the
// facts the consent screen shows. Every unusable code is ONE answer (invalid_invite), recorded
// where a tenant is known, exactly as the wizard's lookup.
func (a *App) ActivateLookup(w http.ResponseWriter, r *http.Request) {
	r, settle := holdInviteBudget(r)
	defer settle()
	ctx := r.Context() // see checkin.go: one level of paren depth for redline R7.
	ip := clientIP(r)
	if a.activation.floodSpent(ip, "app_activate_lookup") {
		writeAppError(w, appErrTooMany)
		return
	}
	body, ok := readAppBody(w, r)
	if !ok {
		return
	}
	var in appLookupRequest
	if !decodeAppJSON(body, &in) || in.Code == "" {
		a.activation.anonFail(ip, "app_activate_lookup", "malformed body")
		writeAppError(w, appErrBadRequest)
		return
	}
	code := invite.ParseCode(in.Code)
	ictx, err := a.invites.Lookup(ctx, code)
	if a.activation.inviteBudgetSpent(r, ip, ictx) {
		writeAppError(w, appErrTooMany)
		return
	}
	if err != nil {
		a.rejectAppCode(w, r, ip, ictx, err, "app_activate_lookup")
		return
	}
	pb, err := a.activation.brands.ActivationBrand(ctx, ictx.TenantID)
	if err != nil {
		// §4.6: a brand never costs the screen (the wizard's rule); Taptime's look.
		a.log.ErrorContext(ctx, "app activation: reading the business's brand failed; answering Taptime's",
			"tenant_id", ictx.TenantID, "err", err)
	}
	a.writeAppJSON(w, r, http.StatusOK, appLookupResponse{
		EmployeeName: ictx.FullName,
		BusinessName: ictx.TenantName,
		LocationName: ictx.LocationName,
		WiFiSSID:     ictx.WiFiSSID,
		SecondDevice: ictx.SecondDevice(),
		Brand:        appBrandOf(pb),
	})
}

// --- POST /app/v1/activate/bind --------------------------------------------------------------

type appBindRequest struct {
	Code    string `json:"code"`
	Consent bool   `json:"consent"`
	// PublicKey is the raw uncompressed P-256 point (65 bytes), unpadded base64url.
	PublicKey string `json:"publicKey"`
}

type appBindResponse struct {
	// ActivationID goes into App-Device-Id and Challenge into App-Nonce when the app completes.
	ActivationID string `json:"activationId"`
	Challenge    string `json:"challenge"`
}

// decodePublicKey reads the body's key: strict unpadded base64url of exactly the 65-byte point.
// Its curve check is appdevice's (ParsePublicKey, through ActivationBinding / VerifyActivation).
func decodePublicKey(s string) ([]byte, bool) {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(s)
	if err != nil || len(raw) != appdevice.PublicKeyLen || base64.RawURLEncoding.EncodeToString(raw) != s {
		return nil, false
	}
	return raw, true
}

// ActivateBind serves POST /app/v1/activate/bind -- POST /api/activate's twin. It RECORDS CONSENT
// and binds it to the key; it spends nothing, creates no device and issues nothing.
//
// ORDER (Submit's, minus the browser's cookie and CSRF measures, which have no app twin -- a bind
// is not reachable by a cross-site navigation): flood ceiling · body · lookup (the tenant becomes
// known) · per-invitation budget · the key on the curve · consent · challenge + binding · record.
func (a *App) ActivateBind(w http.ResponseWriter, r *http.Request) {
	r, settle := holdInviteBudget(r)
	defer settle()
	ctx := r.Context() // see checkin.go: one level of paren depth for redline R7.
	ip := clientIP(r)
	if a.activation.floodSpent(ip, "app_activate_bind") {
		writeAppError(w, appErrTooMany)
		return
	}
	body, ok := readAppBody(w, r)
	if !ok {
		return
	}
	var in appBindRequest
	if !decodeAppJSON(body, &in) || in.Code == "" {
		a.activation.anonFail(ip, "app_activate_bind", "malformed body")
		writeAppError(w, appErrBadRequest)
		return
	}
	code := invite.ParseCode(in.Code)
	ictx, err := a.invites.Lookup(ctx, code)
	if a.activation.inviteBudgetSpent(r, ip, ictx) {
		writeAppError(w, appErrTooMany)
		return
	}
	if err != nil {
		a.rejectAppCode(w, r, ip, ictx, err, "app_activate_bind")
		return
	}
	pub, ok := decodePublicKey(in.PublicKey)
	if !ok {
		a.activation.failAttempt(ctx, ip, ictx, "app_invalid_key", againstIPOnly)
		writeAppError(w, appErrInvalidKey)
		return
	}
	if !in.Consent {
		// NOT an attack signal (Submit's consent_missing): the IP window only.
		a.activation.failAttempt(ctx, ip, ictx, "consent_missing", againstIPOnly)
		writeAppError(w, appErrConsentRequired)
		return
	}
	ch, err := appdevice.NewActivationChallenge()
	if err != nil {
		a.log.ErrorContext(ctx, "app activation: drawing the server's randomness failed", "err", err)
		writeAppError(w, appErrServer)
		return
	}
	binding, err := appdevice.ActivationBinding(ch, pub)
	if errors.Is(err, appdevice.ErrInvalidPublicKey) {
		a.activation.failAttempt(ctx, ip, ictx, "app_invalid_key", againstIPOnly)
		writeAppError(w, appErrInvalidKey)
		return
	}
	if err != nil {
		a.log.ErrorContext(ctx, "app activation: deriving the consent value failed", "err", err)
		writeAppError(w, appErrServer)
		return
	}
	consented, err := a.invites.RecordConsent(ctx, code, invite.ParseBinding(binding))
	if err != nil {
		a.rejectAppCode(w, r, ip, consented, err, "app_activate_bind")
		return
	}
	a.activation.record(ctx, audit.Event{
		TenantID: consented.TenantID,
		Action:   ActionActivationConsented,
		Target:   consented.EmployeeID.String(),
		Detail: activationDetail{
			InviteID:     consented.InviteID.String(),
			Outcome:      "ok",
			SecondDevice: consented.SecondDevice(),
			Client:       clientApp,
		},
	})
	// The challenge's ONE egress: this body, to the phone that holds the key (appdevice.Nonce.Wire).
	a.writeAppJSON(w, r, http.StatusOK, appBindResponse{ActivationID: ch.ID.String(), Challenge: ch.Challenge.Wire()})
}

// --- POST /app/v1/activate/complete ----------------------------------------------------------

type appCompleteRequest struct {
	Code      string `json:"code"`
	PublicKey string `json:"publicKey"`
	Platform  string `json:"platform"`
	// SUN is the first genuine NFC read's query (tag=..&ctr=..&cmac=..).
	SUN string `json:"sun"`
}

type appCompleteResponse struct {
	// DeviceID is the device's id from now on: App-Device-Id on every later request.
	DeviceID     string `json:"deviceId"`
	EmployeeName string `json:"employeeName"`
}

// ActivateComplete serves POST /app/v1/activate/complete -- CompleteByTap for a device key.
//
// ORDER, and why (ADR 0026 md. 3; the card's "CompleteByTap'in sırası"):
//
//	· flood ceiling       before any database work
//	· proof of possession the request is signed by the key the body names, over the server's
//	                      challenge (appdevice.VerifyActivation -- pure). Before the invitation
//	                      is read: a caller who cannot sign learns nothing. Before CreateAppDevice
//	                      by construction (the M16-03 review).
//	· invitation          still usable (lookup); the per-invitation budget
//	· NFC only            a QR read carries no proof of a touch (and advances nothing)
//	· plaque pre-check    NON-advancing: the invitation's own tenant, active, mounted -- before any
//	                      counter moves (audit R5)
//	· binding pre-check   NON-advancing: the recorded consent is THIS key's (invite.ConsentBound) --
//	                      an unbound key spends no counter (M16-07 review)
//	· sun.Verify          CMAC first, THEN the atomic ctr advance (§4.4); a replayed read fails here
//	· consume             invite.ActivateDevice: the code and THIS key's binding in one statement,
//	                      the sessions and app devices revoked, the device created -- one
//	                      transaction. A 23505 on the key rolls all of it back: new_key_required.
//
// It writes NO transactions row (§5 row 3's rule for the activating touch, ADR 0026).
func (a *App) ActivateComplete(w http.ResponseWriter, r *http.Request) {
	r, settle := holdInviteBudget(r)
	defer settle()
	ctx := r.Context() // see checkin.go: one level of paren depth for redline R7.
	ip := clientIP(r)
	if a.activation.floodSpent(ip, "app_activate_complete") {
		writeAppError(w, appErrTooMany)
		return
	}
	body, ok := readAppBody(w, r)
	if !ok {
		return
	}
	req, err := appdevice.FromHTTP(r, body)
	if err != nil {
		a.activation.anonFail(ip, "app_activate_complete", "malformed signed request")
		writeAppUnauthenticated(w)
		return
	}
	var in appCompleteRequest
	if !decodeAppJSON(body, &in) || in.Code == "" {
		a.activation.anonFail(ip, "app_activate_complete", "malformed body")
		writeAppError(w, appErrBadRequest)
		return
	}
	label, known := appPlatforms[in.Platform]
	pub, keyOK := decodePublicKey(in.PublicKey)
	if !known || !keyOK {
		a.activation.anonFail(ip, "app_activate_complete", "malformed body")
		writeAppError(w, appErrBadRequest)
		return
	}
	binding, err := appdevice.VerifyActivation(req, pub)
	if err != nil {
		// Not attributable: nothing is known about the invitation until it is read, and a request
		// that does not prove its key is not allowed to read it.
		a.activation.anonFail(ip, "app_activate_complete", "proof of possession refused")
		writeAppUnauthenticated(w)
		return
	}

	code := invite.ParseCode(in.Code)
	ictx, err := a.invites.Lookup(ctx, code)
	if a.activation.inviteBudgetSpent(r, ip, ictx) {
		writeAppError(w, appErrTooMany)
		return
	}
	if err != nil {
		a.rejectAppCode(w, r, ip, ictx, err, "app_activate_complete")
		return
	}

	p, err := parseSUNQuery(in.SUN)
	if err != nil {
		a.activation.failTap(ctx, ip, ictx, "unusable_url", "")
		writeAppError(w, appErrActivationTapFailed)
		return
	}
	if !p.HasSUN() {
		a.activation.failTap(ctx, ip, ictx, "no_sun", "")
		writeAppError(w, appErrActivationNeedsTouch)
		return
	}
	// The plaque is judged BEFORE anything advances, with ONE answer for every refusal (§4.7) --
	// CompleteByTap's branches, word for word.
	pv, err := a.sun.PreviewWithoutReplayProtection(ctx, p)
	if err != nil {
		if errors.Is(err, sun.ErrUnknownTag) {
			a.activation.failTap(ctx, ip, ictx, "unknown_tag", "")
			writeAppError(w, appErrActivationTapFailed)
			return
		}
		a.log.ErrorContext(ctx, "app activation: plaque pre-check failed", "invite_id", nonNil(ictx.InviteID), "err", err)
		writeAppError(w, appErrServer)
		return
	}
	if pv.TenantID != ictx.TenantID {
		a.activation.failTap(ctx, ip, ictx, "foreign_tenant_tag", "")
		writeAppError(w, appErrActivationTapFailed)
		return
	}
	if pv.TagStatus != "active" || pv.Location == nil {
		a.activation.failTap(ctx, ip, ictx, "tag_not_active", p.UID)
		writeAppError(w, appErrActivationTapFailed)
		return
	}
	// THE BINDING IS JUDGED BEFORE ANYTHING ADVANCES TOO (M16-07 review): a key that signs but never
	// bound -- or whose binding a later bind moved -- must not spend the plaque's counter. A read, not
	// the gate: the consuming statement re-checks the binding below.
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
	res, err := a.sun.Verify(ctx, p)
	if err != nil {
		if errors.Is(err, sun.ErrUnknownTag) {
			a.activation.failTap(ctx, ip, ictx, "unknown_tag", "")
			writeAppError(w, appErrActivationTapFailed)
			return
		}
		a.log.ErrorContext(ctx, "app activation: sun verification failed", "invite_id", nonNil(ictx.InviteID), "err", err)
		writeAppError(w, appErrServer)
		return
	}
	if res.Tag.TenantID != ictx.TenantID || res.Tag.Status != "active" || res.Location == nil {
		a.activation.failTap(ctx, ip, ictx, "tag_changed", "")
		writeAppError(w, appErrActivationTapFailed)
		return
	}
	if !res.SUNValid {
		a.activation.failTap(ctx, ip, ictx, "sun_invalid", p.UID)
		writeAppError(w, appErrActivationTapFailed)
		return
	}

	act, err := a.invites.ActivateDevice(ctx, code, invite.ParseBinding(binding), invite.DeviceParams{
		Platform: in.Platform, PublicKey: pub, Label: &label,
	})
	if err != nil {
		switch {
		case errors.Is(err, invite.ErrConsentMissing):
			// The binding moved -- another bind, or a browser consent, since this key's.
			a.activation.failTap(ctx, ip, act.Context, "consent_missing", res.Tag.UID)
			writeAppError(w, appErrActivationNotReady)
		case errors.Is(err, invite.ErrDeviceKeyTaken):
			// NOTHING was consumed (one transaction). The app generates a new key and binds again.
			a.activation.failTap(ctx, ip, act.Context, "device_key_taken", res.Tag.UID)
			writeAppError(w, appErrNewKeyRequired)
		default:
			a.rejectAppCode(w, r, ip, act.Context, err, "app_activate_complete")
		}
		return
	}

	if act.SessionsRevoked > 0 || act.AppDevicesRevoked > 0 {
		a.activation.record(ctx, audit.Event{
			TenantID: act.TenantID,
			Action:   ActionDeviceReplaced,
			Target:   act.EmployeeID.String(),
			Detail: activationDetail{
				InviteID:          act.InviteID.String(),
				Outcome:           "ok",
				SessionsRevoked:   act.SessionsRevoked,
				AppDevicesRevoked: act.AppDevicesRevoked,
				Client:            clientApp,
			},
		})
	}
	a.activation.record(ctx, audit.Event{
		TenantID: act.TenantID,
		Action:   ActionActivationCompleted,
		Target:   act.EmployeeID.String(),
		Detail: activationDetail{
			InviteID:          act.InviteID.String(),
			Outcome:           "ok",
			SecondDevice:      act.SecondDeviceReplaced,
			Device:            label,
			TagUID:            res.Tag.UID,
			LocationID:        res.Location.String(),
			Client:            clientApp,
			AppDeviceID:       act.DeviceID.String(),
			SessionsRevoked:   act.SessionsRevoked,
			AppDevicesRevoked: act.AppDevicesRevoked,
		},
	})
	a.writeAppJSON(w, r, http.StatusOK, appCompleteResponse{DeviceID: act.DeviceID.String(), EmployeeName: act.FullName})
}

// rejectAppCode is rejectCode for the app: the same trail and the same charges, and ONE answer
// for every unusable code (invalid_invite) -- 500 for a failure nobody could classify, which is
// ours and not the link's.
func (a *App) rejectAppCode(w http.ResponseWriter, r *http.Request, ip string, ictx invite.Context, err error, where string) {
	ctx := r.Context() // see checkin.go: one level of paren depth for redline R7.
	if errors.Is(err, invite.ErrUnknownCode) {
		a.activation.anonFail(ip, where, "unknown code")
		writeAppError(w, appErrInvalidInvite)
		return
	}
	reason, classified := inviteFailureReason(err)
	if !classified {
		a.activation.failAttempt(ctx, ip, ictx, "unclassified", againstInvite)
		a.log.ErrorContext(ctx, "app activation failed", "at", where, "err", err)
		writeAppError(w, appErrServer)
		return
	}
	a.activation.failAttempt(ctx, ip, ictx, reason, againstInvite)
	writeAppError(w, appErrInvalidInvite)
}
