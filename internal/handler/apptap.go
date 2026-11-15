package handler

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/geo"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The app's tap: nonce, tap/start (GET /t's twin), tap (POST /api/checkin's twin), me (Q41) and
// its own sign-out. See app.go for the surface's rules.

// ActionAppDeviceSignedOut is the audit action of a device's own sign-out (POST /app/v1/revoke).
const ActionAppDeviceSignedOut = "app_device.signed_out"

// appDirectionOf is what the app's button says: tapDirectionOf's rule (a direction only with a
// prediction that carries a business day) in the app's words, "" for the direction-neutral "Tap".
func appDirectionOf(pr *tap.Prediction) string {
	switch tapDirectionOf(pr) {
	case pages.TapClockIn:
		return string(tap.TypeIn)
	case pages.TapClockOut:
		return string(tap.TypeOut)
	default:
		return ""
	}
}

// --- POST /app/v1/nonce ---------------------------------------------------------------------

type appNonceResponse struct {
	Nonce     string    `json:"nonce"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// Nonce serves POST /app/v1/nonce: a signed read in, one single-use nonce out. Metered per source
// by the surface's address bucket (in front of the signature) and per device by AllowDevice
// (after it); IssueNonce itself never refuses at its cap -- it drops the oldest live nonce
// (appdevice.MaxLiveNonces). The body is empty or {}.
func (a *App) Nonce(w http.ResponseWriter, r *http.Request) {
	body, ok := readAppBody(w, r)
	if !ok {
		return
	}
	dev, ok := a.authRead(w, r, body, "nonce", "")
	if !ok {
		return
	}
	if !decodeAppJSON(body, &struct{}{}) {
		writeAppError(w, appErrBadRequest)
		return
	}
	issued, err := a.devices.IssueNonce(r.Context(), dev.ID)
	if err != nil {
		a.refuseSigned(w, r, err, "nonce", dev.ID, "")
		return
	}
	// The nonce's ONE egress (appdevice.Nonce.Wire): this body, to the device that signed for it.
	a.writeAppJSON(w, r, http.StatusOK, appNonceResponse{Nonce: issued.Nonce.Wire(), ExpiresAt: issued.ExpiresAt.UTC()})
}

// --- GET /app/v1/me -------------------------------------------------------------------------

type appMeResponse struct {
	EmployeeName string   `json:"employeeName"`
	Brand        appBrand `json:"brand"`
	// Direction is "in", "out", or "" for the direction-neutral "Tap" (Q41).
	Direction string `json:"direction"`
}

// Me serves GET /app/v1/me: the app's home screen before any plaque is read (Q41 (a): the iPhone
// 7-X screen is the tap screen, and its one button starts the NFC read). The predicted direction
// is tenant.Directory.AppHome's -- predictTap, the same function and the same person-and-switch
// gate GET /t uses (apphome.go says what the missing plaque half means).
func (a *App) Me(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context() // see checkin.go: one level of paren depth for redline R7.
	body, ok := readAppBody(w, r)
	if !ok {
		return
	}
	dev, ok := a.authRead(w, r, body, "me", "")
	if !ok {
		return
	}
	facts, err := a.directory.AppHome(ctx, dev.TenantID, dev.EmployeeID)
	switch {
	case errors.Is(err, tenant.ErrBrandUnread):
		a.log.ErrorContext(ctx, "app: reading the business's brand failed; answering Taptime's",
			"tenant_id", dev.TenantID, "employee_id", dev.EmployeeID, "err", err)
	case err != nil:
		a.log.ErrorContext(ctx, "app: loading the home screen failed", "employee_id", dev.EmployeeID, "err", err)
		writeAppError(w, appErrServer)
		return
	}
	if facts.PredictionErr != nil {
		a.log.ErrorContext(ctx, "app: reading the predicted direction failed; the button says Tap",
			"tenant_id", dev.TenantID, "employee_id", dev.EmployeeID, "err", facts.PredictionErr)
	}
	if facts.Brand.AccentRefused {
		a.log.WarnContext(ctx, "app: the stored accent does not pass the legibility gate today; no accent",
			"tenant_id", dev.TenantID)
	}
	a.writeAppJSON(w, r, http.StatusOK, appMeResponse{
		EmployeeName: facts.EmployeeName,
		Brand:        appBrandOf(facts.Brand),
		Direction:    appDirectionOf(facts.Prediction),
	})
}

// --- POST /app/v1/tap/start ---------------------------------------------------------------

type appTapStartRequest struct {
	// SUN is the plaque URL's QUERY, as the NFC read (or the QR) gave it: tag=..&ctr=..&cmac=..
	SUN string `json:"sun"`
}

type appTapStartResponse struct {
	EmployeeName string   `json:"employeeName"`
	LocationName string   `json:"locationName"`
	Brand        appBrand `json:"brand"`
	Direction    string   `json:"direction"`
	// QR is true for a QR read (the screen's QR wording, as GET /t's).
	QR bool `json:"qr"`
	// Context is the signed tap context POST /app/v1/tap carries back: GET /t's structure, bound
	// to this device.
	Context string `json:"context"`
}

// parseSUNQuery reads the SUN URL's query the way GET /t reads r.URL.Query(): url.ParseQuery, then
// sun.Parse (which never panics on hostile input and names no value in its error).
func parseSUNQuery(raw string) (sun.Params, error) {
	q, err := url.ParseQuery(raw)
	if err != nil {
		return sun.Params{}, err
	}
	return sun.Parse(q)
}

// TapStart serves POST /app/v1/tap/start -- GET /t's twin. It writes NO transactions row and does
// NOT advance the counter: sun.PreviewWithoutReplayProtection, the page's own non-advancing
// pre-check (the interface the app holds offers Verify too, for the activation; this handler does
// not call it). The answer mirrors the page: the person's name, the TAPPED venue, the brand (none
// for another business's plaque), the predicted direction behind predictionAllowed's gate, and a
// signed context for the press.
//
// ORDER: the signature first -- a caller who cannot sign learns nothing about any plaque -- then
// the SUN query, then the page's own order (pre-check, facts, mint).
func (a *App) TapStart(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context() // see checkin.go: one level of paren depth for redline R7.
	body, ok := readAppBody(w, r)
	if !ok {
		return
	}
	// The body is read BEFORE the signature only for the refusal's log line: the channel the request
	// declares (signedOutAppTap). Nothing is acted on until the read verifies.
	var in appTapStartRequest
	decoded := decodeAppJSON(body, &in)
	p, perr := parseSUNQuery(in.SUN)
	channel := "unverified"
	if decoded && perr == nil {
		channel = string(p.Channel)
	}
	dev, ok := a.authRead(w, r, body, "tap_start", channel)
	if !ok {
		return
	}
	if !decoded {
		writeAppError(w, appErrBadRequest)
		return
	}
	if err := perr; err != nil {
		a.log.InfoContext(ctx, "app tap start: unusable plaque url", "employee_id", dev.EmployeeID)
		writeAppError(w, appErrBadRequest)
		return
	}
	pv, err := a.sun.PreviewWithoutReplayProtection(ctx, p)
	if err != nil {
		if errors.Is(err, sun.ErrUnknownTag) {
			a.log.WarnContext(ctx, "app tap start: unknown plaque", "employee_id", dev.EmployeeID)
			writeAppError(w, appErrUnknownPlaque)
			return
		}
		a.log.ErrorContext(ctx, "app tap start: sun pre-check failed", "employee_id", dev.EmployeeID, "err", err)
		writeAppError(w, appErrServer)
		return
	}
	// The page renders for a dead plaque and a bad CMAC, and so does this: the refusal's RECORD is
	// written by the press (§4.6), exactly as GET /t leaves it to POST /api/checkin.
	wall := tappedWallOf(pv)
	predict := predictionAllowed(p, pv)
	facts, err := a.directory.TapPage(ctx, dev.TenantID, dev.EmployeeID, wall, predict)
	if errors.Is(err, tenant.ErrForeignLocation) && pv.TenantID != dev.TenantID {
		// A PEER'S PLAQUE (ADR 0027 §5/§10): GET /t's branch, word for word.
		peerFacts, site, perr := a.directory.PeerTapPage(ctx, dev.TenantID, dev.EmployeeID, p.UID, predict)
		switch {
		case perr != nil && !errors.Is(perr, tenant.ErrBrandUnread):
			err = perr
		case site != uuid.Nil:
			facts, err = peerFacts, perr
		}
	}
	out := appTapStartResponse{QR: p.Channel == sun.ChannelQR}
	switch {
	case errors.Is(err, tenant.ErrForeignLocation):
		a.log.WarnContext(ctx, "app tap start: plaque belongs to another tenant",
			"employee_id", dev.EmployeeID, "session_tenant_id", dev.TenantID, "tag_tenant_id", pv.TenantID)
		out.EmployeeName = facts.EmployeeName
	case errors.Is(err, tenant.ErrBrandUnread):
		a.log.ErrorContext(ctx, "app tap start: reading the business's brand failed; answering Taptime's",
			"tenant_id", dev.TenantID, "employee_id", dev.EmployeeID, "err", err)
		out.EmployeeName, out.LocationName = facts.EmployeeName, facts.LocationName
	case err != nil:
		a.log.ErrorContext(ctx, "app tap start: loading the employee failed", "employee_id", dev.EmployeeID, "err", err)
		writeAppError(w, appErrServer)
		return
	default:
		if facts.Brand.AccentRefused {
			a.log.WarnContext(ctx, "app tap start: the stored accent does not pass the legibility gate today; no accent",
				"tenant_id", dev.TenantID)
		}
		if facts.PredictionErr != nil {
			a.log.ErrorContext(ctx, "app tap start: reading the predicted direction failed; the button says Tap",
				"tenant_id", dev.TenantID, "employee_id", dev.EmployeeID, "err", facts.PredictionErr)
		}
		out.EmployeeName, out.LocationName = facts.EmployeeName, facts.LocationName
		out.Brand = appBrandOf(facts.Brand)
		out.Direction = appDirectionOf(facts.Prediction)
	}
	signed, err := a.contexts.mintForDevice(tapContext{
		UID:          p.UID,
		Ctr:          p.Ctr,
		Channel:      p.Channel,
		CMACVerified: pv.CMACValid,
		TagTenantID:  pv.TenantID,
		LocationID:   wall,
	}, dev.ID)
	if err != nil {
		a.log.ErrorContext(ctx, "app tap start: minting the tap context failed", "employee_id", dev.EmployeeID, "err", err)
		writeAppError(w, appErrServer)
		return
	}
	out.Context = signed
	a.writeAppJSON(w, r, http.StatusOK, out)
}

// --- POST /app/v1/tap -----------------------------------------------------------------------

// appTapRequest is POST /api/checkin's form, as JSON: the signed context, the press, one fix, the
// mock-location report and the optional queued time. Nothing in it names a plaque, a counter, a
// channel, a client, a verdict or an employee (ADR 0004 §8).
type appTapRequest struct {
	Context string `json:"context"`
	// Want is "in", "out" or absent: a PREFERENCE (parseWant's rule -- an unknown value is "no
	// request", never a 400).
	Want string `json:"want,omitempty"`
	// Lat / Lng: one fix read at the press (§4.2), or absent. Out of range is "no fix".
	Lat *float64 `json:"lat,omitempty"`
	Lng *float64 `json:"lng,omitempty"`
	// MockLocation is the platform's mock-provider flag for THAT fix (ADR 0045 §2.4) -- a client
	// report that can only tighten (base:mock-location-review); absent is no report.
	MockLocation *bool `json:"mockLocation,omitempty"`
	// OccurredAt is RFC 3339, for a queued tap (none today); bounded by sys:occurred-at-bound.
	OccurredAt string `json:"occurredAt,omitempty"`
}

type appTapResponse struct {
	Outcome string `json:"outcome"`
	// Verdict is ok / flag / reject / ignored -- the stamp the result screen draws.
	Verdict string `json:"verdict"`
	// Direction is "in", "out" or "".
	Direction    string `json:"direction"`
	Venue        string `json:"venue"`
	At           string `json:"at"`
	BusinessType string `json:"businessType"`
	Practice     bool   `json:"practice,omitempty"`
}

// appFix is parseFix for JSON numbers: both or none, in range, not NaN -- else no fix.
func appFix(lat, lng *float64) *geo.Point {
	if lat == nil || lng == nil {
		return nil
	}
	if *lat != *lat || *lng != *lng || *lat < -90 || *lat > 90 || *lng < -180 || *lng > 180 {
		return nil
	}
	return &geo.Point{Lat: *lat, Lng: *lng}
}

// Tap serves POST /app/v1/tap -- POST /api/checkin's twin, and the app's one writer: the SAME
// checkin.Service.Record with client = app (server-derived: this handler IS the app's endpoint)
// and MockLocation from the signed body. Every §5 row lands as in the browser: rows 1, 2, 4, 5, 6,
// 7 write a record; row 3's twin (a revoked device) is the 401 below with no record; a device id
// nobody can sign for is the same 401.
//
// ORDER: the nonce-signed request (Verify spends the nonce in its own transaction -- a tap whose
// record then fails spent a nonce, not the counter: the context stays good and the app retries
// with a new nonce), the device's bucket, the body, the context (bound to THIS device), the
// declared time, then Record.
func (a *App) Tap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context() // see checkin.go: one level of paren depth for redline R7.
	body, ok := readAppBody(w, r)
	if !ok {
		return
	}
	// The channel is inside the signed tap context, which is not read before the request verifies
	// (POST /api/checkin's "unverified" for its revoked branch).
	dev, ok := a.authRequest(w, r, body, "tap", "unverified")
	if !ok {
		return
	}
	var in appTapRequest
	if !decodeAppJSON(body, &in) {
		a.log.InfoContext(ctx, "app tap: unusable body", "employee_id", dev.EmployeeID)
		writeAppError(w, appErrBadRequest)
		return
	}
	tctx, err := a.contexts.parseForDevice(in.Context, dev.ID)
	if err != nil {
		if errors.Is(err, errTapContextExpired) {
			a.log.InfoContext(ctx, "app tap: expired tap context", "employee_id", dev.EmployeeID)
		} else {
			a.log.WarnContext(ctx, "app tap: unusable tap context", "employee_id", dev.EmployeeID, "err", err)
		}
		writeAppError(w, appErrStaleContext)
		return
	}
	occurredAt, ok := parseOccurredAt(in.OccurredAt)
	if !ok {
		a.log.InfoContext(ctx, "app tap: unusable occurred_at", "employee_id", dev.EmployeeID)
		writeAppError(w, appErrBadRequest)
		return
	}
	var wants []string
	if in.Want != "" {
		wants = []string{in.Want}
	}
	want, unusable := parseWant(wants)
	if unusable {
		a.log.WarnContext(ctx, "app tap: the want field is unknown; the engine chooses the direction",
			"employee_id", dev.EmployeeID)
	}
	res, err := a.checkins.Record(ctx, checkin.Request{
		SessionTenantID: dev.TenantID,
		EmployeeID:      dev.EmployeeID,
		TagUID:          tctx.UID,
		Ctr:             tctx.Ctr,
		Channel:         tap.Channel(tctx.Channel),
		CMACVerified:    tctx.CMACVerified,
		TagTenantID:     tctx.TagTenantID,
		// SERVER-DERIVED (ADR 0045 §2.4): this handler is the app's endpoint, so the client is the
		// app by construction -- nothing in the request is read for it.
		Client:       tap.ClientApp,
		MockLocation: in.MockLocation,
		PageIssuedAt: tctx.IssuedAt,
		SourceIP:     httpx.ClientIP(r),
		GPS:          appFix(in.Lat, in.Lng),
		OccurredAt:   occurredAt,
		// Applied by the domain only while the business-day switch is on (§B.5).
		RequestedDirection: want,
	})
	if err != nil {
		a.tapFailure(w, r, dev.EmployeeID, err)
		return
	}
	switch res.Outcome {
	case checkin.OutcomeForeignTenant:
		writeAppError(w, appErrForeignTenant)
	case checkin.OutcomeActivation:
		// §5 row 3: Record does not produce it (checkin.OutcomeActivation's note); answered as the
		// row-3 twin all the same -- no record, re-activate.
		writeAppUnauthenticated(w)
	default:
		v := resultView(res)
		// The record is written: the answer must not depend on the request's cancellation any more
		// (checkin.go's rule for the confirmation screen).
		a.writeAppJSON(w, r.WithContext(context.WithoutCancel(ctx)), http.StatusOK, appTapResponse{
			Outcome:      string(res.Outcome),
			Verdict:      v.Verdict,
			Direction:    v.Direction,
			Venue:        v.Venue,
			At:           v.At,
			BusinessType: v.BusinessType,
			Practice:     v.Practice,
		})
	}
}

// tapFailure is renderCheckinFailure for the app: every branch is a path where NOTHING was
// recorded, said plainly and logged.
func (a *App) tapFailure(w http.ResponseWriter, r *http.Request, employeeID uuid.UUID, err error) {
	ctx := r.Context() // see checkin.go: one level of paren depth for redline R7.
	switch {
	case errors.Is(err, checkin.ErrSiteMembershipEnded):
		a.log.WarnContext(ctx, "app tap: the plaque's business left the group before the record was written; nothing was written",
			"employee_id", employeeID)
		writeAppError(w, appErrTryAgain)
	case errors.Is(err, checkin.ErrUnknownTag):
		writeAppError(w, appErrUnknownPlaque)
	case errors.Is(err, checkin.ErrInvalidRequest), errors.Is(err, checkin.ErrEnteredByRequired):
		a.log.ErrorContext(ctx, "app tap: refused an invalid request", "employee_id", employeeID, "err", err)
		writeAppError(w, appErrServer)
	default:
		a.log.ErrorContext(ctx, "app tap: recording the tap failed; NOTHING was written",
			"employee_id", employeeID, "err", err)
		writeAppError(w, appErrServer)
	}
}

// --- POST /app/v1/revoke --------------------------------------------------------------------

// Revoke serves POST /app/v1/revoke: the device signs itself out. Nonce-signed, so a captured
// request cannot be replayed, and a revoked device's request is refused like any other (401).
// The revocation kills the device's nonces in the same statement (RevokeAppDevice) and is retried
// on 40P01 (appdevice.Verifier.Revoke). 204, no body; an audit row names the device.
func (a *App) Revoke(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context() // see checkin.go: one level of paren depth for redline R7.
	body, ok := readAppBody(w, r)
	if !ok {
		return
	}
	dev, ok := a.authRequest(w, r, body, "revoke", "")
	if !ok {
		return
	}
	if !decodeAppJSON(body, &struct{}{}) {
		writeAppError(w, appErrBadRequest)
		return
	}
	if err := a.devices.Revoke(ctx, dev); err != nil {
		a.log.ErrorContext(ctx, "app: a device's own sign-out failed", "employee_id", dev.EmployeeID, "err", err)
		writeAppError(w, appErrServer)
		return
	}
	actx, cancel := audit.Detached(ctx)
	defer cancel()
	if _, err := a.audit.Record(actx, audit.Event{
		TenantID: dev.TenantID,
		Action:   ActionAppDeviceSignedOut,
		Target:   dev.EmployeeID.String(),
		Detail:   appDeviceDetail{AppDeviceID: dev.ID.String(), Outcome: "ok"},
	}); err != nil {
		a.log.ErrorContext(ctx, audit.EventWriteFailed, "action", ActionAppDeviceSignedOut, "err", err)
	}
	setAppHeaders(w)
	w.WriteHeader(http.StatusNoContent)
}

// appDeviceDetail is the hand-written detail of an app-device audit row: ids and a fixed word.
type appDeviceDetail struct {
	AppDeviceID string `json:"app_device_id"`
	Outcome     string `json:"outcome"`
}
