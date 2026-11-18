package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/appdevice"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/invite"
)

// /app/v1 -- THE NATIVE APP'S ONE SURFACE (ADR 0045 §2.3, §2.4; M16-07). It is the browser flow's
// TWIN, not a second decision path: every tap it accepts goes through the same
// checkin.Service.Record, the same tap.Decide and policy engine, the same person lock and the same
// transactions INSERT as POST /api/checkin; every activation through the same invitation
// statement as the wizard's; every preview through the same non-advancing sun preview as GET /t.
// What differs is only HOW THE PERSON IS KNOWN: a browser carries a session cookie, the app signs
// each request with a key that never leaves the phone (internal/appdevice).
//
//	POST /app/v1/activate/lookup    unsigned   the invitation's facts for the consent screen
//	                                           (GET /activate's twin; spends nothing)
//	POST /app/v1/activate/bind      unsigned   consent + public key -> challenge (POST
//	                                           /api/activate's twin; spends nothing)
//	POST /app/v1/activate/complete  signed by  the bound key over the challenge; the first
//	                                the bound  genuine NFC read's SUN query -> the device
//	                                key        (CompleteByTap's order; NO transactions row)
//	POST /app/v1/nonce              read-signed  one single-use nonce (the cap drops the oldest)
//	POST /app/v1/tap/start          read-signed  the SUN query -> name, venue, brand, predicted
//	                                             direction, signed tap context (GET /t's twin;
//	                                             the counter does NOT advance)
//	POST /app/v1/tap                nonce-signed the context + want + fix + mock flag -> the
//	                                             record (POST /api/checkin's twin)
//	GET  /app/v1/me                 read-signed  name, brand, predicted direction (Q41)
//	POST /app/v1/revoke             nonce-signed the device's own sign-out
//
// THE TWO UNSIGNED ROUTES AND THE ONE WITHOUT A NONCE are the card's two named exceptions plus
// the consent screen's read: lookup and bind come before any key is registered (a nonce belongs
// to a device row, 00108), and their replay is bounded by the invitation -- a second bind MOVES
// the binding, so an earlier one completes nothing (ADR 0026 "Güvenlik değerlendirmesi");
// complete comes from a key with no device row, signs over the server's challenge, and its replay
// guard is the SUN counter's atomic advance (§4.4) and the invitation's one-statement consumption.
// No device is given a nonce before it is active.
//
// WHAT NO RESPONSE CARRIES (§7, CLAUDE.md "Asla loglanmaz"): a signature, a nonce (except the
// nonce route's own answer and bind's challenge -- each the value's one egress, to the phone that
// asked), a public key, an invite code, a SUN parameter, a coordinate. Error bodies are the closed
// vocabulary below and echo nothing the request carried. Every refusal of a signed request is
// 401 with an EMPTY body: unknown device, revoked device, bad signature, spent nonce and stale
// timestamp are one answer (appdevice's no-oracle rule, M16-03 review).
//
// ROUTING (M16-05 review): nothing in front of appdevice.FromHTTP / ReadFromHTTP rewrites
// r.URL.Path or RawPath -- no StripSlashes, CleanPath or StripPrefix on this group or on the
// router (httpx.NewRouter mounts none on a feature's routes; TestApp_TheSignedPathIsTheWirePath
// drives a request through it). The body the verifier hashes is the RAW bytes read off the wire
// (readAppBody) -- nothing decompresses it -- and Content-Type and Content-Encoding, which are not
// signed, are not read at all.

// Consumer-side interfaces (§7).
type (
	// appDevices is the slice of internal/appdevice this surface needs.
	appDevices interface {
		VerifyRead(ctx context.Context, req appdevice.SignedRead) (appdevice.Device, error)
		Verify(ctx context.Context, req appdevice.SignedRequest) (appdevice.Device, error)
		IssueNonce(ctx context.Context, deviceID uuid.UUID) (appdevice.IssuedNonce, error)
		Revoke(ctx context.Context, dev appdevice.Device) error
	}
	// appInvites is the slice of internal/invite the app's activation needs: the wizard's
	// lookup and consent, and the app's own consumption (ActivateDevice).
	appInvites interface {
		Lookup(ctx context.Context, c invite.Code) (invite.Context, error)
		RecordConsent(ctx context.Context, c invite.Code, b invite.Binding) (invite.Context, error)
		ActivateDevice(ctx context.Context, c invite.Code, b invite.Binding, d invite.DeviceParams) (invite.DeviceActivation, error)
		// ConsentBound is complete's non-advancing pre-check: is THIS key's binding the one recorded.
		ConsentBound(ctx context.Context, c invite.Code, b invite.Binding) (bool, error)
	}
	// appDirectory is the tap page's directory plus the app's home screen (Q41).
	appDirectory interface {
		tapDirectory
		AppHome(ctx context.Context, tenantID, employeeID uuid.UUID) (tenant.AppHomeFacts, error)
	}
)

// App serves /app/v1. Dependencies are struct fields (§7).
type App struct {
	devices   appDevices
	invites   appInvites
	sun       activationVerifier
	directory appDirectory
	checkins  checkinRecorder
	// activation is the wizard's handler, reached for what the two flows SHARE: the per-address
	// flood ceiling and the per-invitation budget (one budget per invitation, whichever client
	// presents it), the activation audit rows and their vocabulary, and the brand reader.
	activation *Activation
	audit      auditRecorder
	contexts   tapContexts
	// limiter is the tap surface's shield for this surface: its address bucket in front of every
	// route, its session-sized bucket per VERIFIED device (httpx.TapLimiter.AllowDevice). An
	// instance of its own -- its refusals answer JSON, not the tap page's HTML -- so the app's
	// traffic and the browser's are metered by two buckets of the same size.
	limiter *httpx.TapLimiter
	log     *slog.Logger
}

// NewApp wires the surface. Every dependency is required.
func NewApp(devices appDevices, invites appInvites, verifier activationVerifier, dir appDirectory, checkins checkinRecorder, activation *Activation, rec auditRecorder, cfg *config.Config, log *slog.Logger) (*App, error) {
	switch {
	case isNil(devices):
		return nil, errors.New("handler: nil app device verifier")
	case isNil(invites):
		return nil, errors.New("handler: nil invite manager")
	case isNil(verifier):
		return nil, errors.New("handler: nil sun verifier")
	case isNil(dir):
		return nil, errors.New("handler: nil directory")
	case isNil(checkins):
		return nil, errors.New("handler: nil checkin service")
	case activation == nil:
		return nil, errors.New("handler: nil activation flow")
	case isNil(rec):
		return nil, errors.New("handler: nil audit recorder")
	case cfg == nil:
		return nil, errors.New("handler: nil config")
	}
	if log == nil {
		log = slog.Default()
	}
	ctxs, err := newTapContexts(cfg)
	if err != nil {
		return nil, err
	}
	a := &App{
		devices: devices, invites: invites, sun: verifier, directory: dir, checkins: checkins,
		activation: activation, audit: rec, contexts: ctxs, log: log,
	}
	a.limiter = httpx.NewTapLimiter(httpx.TapLimitParams{Audit: rec, Log: log, Refused: writeAppTooMany})
	return a, nil
}

// App route paths. Constants because the signed canonical text carries the path: the app and the
// router must name the same bytes.
const (
	AppActivateLookupPath   = "/app/v1/activate/lookup"
	AppActivateBindPath     = "/app/v1/activate/bind"
	AppActivateCompletePath = "/app/v1/activate/complete"
	AppNoncePath            = "/app/v1/nonce"
	AppTapStartPath         = "/app/v1/tap/start"
	AppTapPath              = "/app/v1/tap"
	AppMePath               = "/app/v1/me"
	AppRevokePath           = "/app/v1/revoke"
)

// appPrefix is the surface's root; every App*Path starts with it.
const appPrefix = "/app/v1"

// Mount registers /app/v1 behind the address bucket, as a sub-router of its own so that an unknown
// path or a wrong method UNDER /app/v1 answers the closed vocabulary (not_found /
// method_not_allowed, no-store) instead of the site's HTML or chi's plain text (M16-07 review).
//
// chi's Route mounts by its routing context, NOT by rewriting r.URL: the path the verifier signs
// is still the whole wire path (TestApp_TheSignedPathIsTheWirePath drives it through the router).
func (a *App) Mount(r chi.Router) {
	sub := func(p string) string { return strings.TrimPrefix(p, appPrefix) }
	r.Route(appPrefix, func(r chi.Router) {
		r.Use(a.limiter.ByAddress)
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) { writeAppError(w, appErrNotFound) })
		r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) { writeAppError(w, appErrMethodNotAllowed) })
		r.Post(sub(AppActivateLookupPath), a.ActivateLookup)
		r.Post(sub(AppActivateBindPath), a.ActivateBind)
		r.Post(sub(AppActivateCompletePath), a.ActivateComplete)
		r.Post(sub(AppNoncePath), a.Nonce)
		r.Post(sub(AppTapStartPath), a.TapStart)
		r.Post(sub(AppTapPath), a.Tap)
		r.Get(sub(AppMePath), a.Me)
		r.Post(sub(AppRevokePath), a.Revoke)
	})
}

// appMaxBody bounds every /app/v1 body. The largest is tap's: a signed context (~200 bytes), two
// coordinates, a direction, a flag and a timestamp; complete's carries a code, a key and a SUN
// query. 8 KiB is POST /api/checkin's ceiling.
const appMaxBody = 8 << 10

// appError is the CLOSED error vocabulary of /app/v1 bodies ({"error": <code>}). A type, so a
// handler cannot write a free-text error; no code carries a value from the request. Each code has
// ONE status (appErrorStatus) -- the wire contract the apps build on (card M16-07, "Tel sözleşmesi").
type appError string

const (
	appErrBadRequest           appError = "bad_request"            // a body this surface cannot read
	appErrTooMany              appError = "rate_limited"           // a budget is spent; Retry-After says how long
	appErrServer               appError = "server_error"           // nothing was recorded or spent by us
	appErrInvalidInvite        appError = "invalid_invite"         // unknown / expired / used / cancelled / not activatable -- ONE code (§4.7)
	appErrConsentRequired      appError = "consent_required"       // bind without consent
	appErrInvalidKey           appError = "invalid_key"            // the public key is not a P-256 point
	appErrNewKeyRequired       appError = "new_key_required"       // the key is already registered -- generate a new pair and bind again
	appErrActivationNotReady   appError = "activation_not_ready"   // the binding moved (another bind since) -- bind again
	appErrActivationNeedsTouch appError = "activation_needs_touch" // a QR read cannot activate
	appErrActivationTapFailed  appError = "activation_tap_failed"  // unknown / foreign / out-of-service plaque, bad or replayed SUN -- ONE code (§4.7)
	appErrStaleContext         appError = "stale_context"          // the tap context expired, was tampered with or is another device's
	appErrUnknownPlaque        appError = "unknown_plaque"         // the plaque is not ours
	appErrForeignTenant        appError = "foreign_tenant"         // sys:tenant-mismatch
	appErrTryAgain             appError = "try_again"              // ADR 0027 §6's race; nothing was written
	appErrNotFound             appError = "not_found"              // no /app/v1 route at this path
	appErrMethodNotAllowed     appError = "method_not_allowed"     // the route exists, not with this method
)

// appErrorStatus is the one status each code is answered with.
var appErrorStatus = map[appError]int{
	appErrBadRequest:           http.StatusBadRequest,
	appErrTooMany:              http.StatusTooManyRequests,
	appErrServer:               http.StatusInternalServerError,
	appErrInvalidInvite:        http.StatusBadRequest,
	appErrConsentRequired:      http.StatusBadRequest,
	appErrInvalidKey:           http.StatusBadRequest,
	appErrNewKeyRequired:       http.StatusConflict,
	appErrActivationNotReady:   http.StatusConflict,
	appErrActivationNeedsTouch: http.StatusBadRequest,
	appErrActivationTapFailed:  http.StatusBadRequest,
	appErrStaleContext:         http.StatusBadRequest,
	appErrUnknownPlaque:        http.StatusNotFound,
	appErrForeignTenant:        http.StatusForbidden,
	appErrTryAgain:             http.StatusConflict,
	appErrNotFound:             http.StatusNotFound,
	appErrMethodNotAllowed:     http.StatusMethodNotAllowed,
}

// appErrors is the vocabulary as a list, for the tests that hold it closed.
var appErrors = []appError{
	appErrBadRequest, appErrTooMany, appErrServer, appErrInvalidInvite, appErrConsentRequired,
	appErrInvalidKey, appErrNewKeyRequired, appErrActivationNotReady, appErrActivationNeedsTouch,
	appErrActivationTapFailed, appErrStaleContext, appErrUnknownPlaque, appErrForeignTenant, appErrTryAgain,
	appErrNotFound, appErrMethodNotAllowed,
}

// setAppHeaders: every /app/v1 answer is no-store (the card), nosniff, and never framed.
func setAppHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
}

// writeAppJSON writes one JSON answer. The value is a response struct of this package with
// exactly the fields the app shows -- never a domain or store type.
func (a *App) writeAppJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		a.log.ErrorContext(r.Context(), "app: encoding a response failed", "err", err)
		writeAppError(w, appErrServer)
		return
	}
	setAppHeaders(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		a.log.InfoContext(r.Context(), "app: writing a response failed", "err", err)
	}
}

// writeAppError writes {"error": code} with the code's one status. Its body is a constant per
// code: it cannot echo input. A code missing from appErrorStatus is a 500 (the test holds the map
// complete).
func writeAppError(w http.ResponseWriter, code appError) {
	status, ok := appErrorStatus[code]
	if !ok {
		status = http.StatusInternalServerError
	}
	setAppHeaders(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + string(code) + `"}` + "\n"))
}

// writeAppUnauthenticated answers every refused signed request: 401, EMPTY body (the card).
func writeAppUnauthenticated(w http.ResponseWriter) {
	setAppHeaders(w)
	w.WriteHeader(http.StatusUnauthorized)
}

// writeAppTooMany is the TapLimiter's 429 body for this surface (Retry-After, no-store and
// nosniff are already set by the limiter).
func writeAppTooMany(w http.ResponseWriter, _ *http.Request) {
	writeAppError(w, appErrTooMany)
}

// readAppBody reads the RAW body under appMaxBody: the bytes the signature's digest is over.
// Nothing decodes or decompresses it first. false means it has answered 400 (too big, or the
// connection failed mid-body).
func readAppBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, appMaxBody))
	if err != nil {
		writeAppError(w, appErrBadRequest)
		return nil, false
	}
	return body, true
}

// decodeAppJSON reads body into v STRICTLY: one JSON object, no unknown field, nothing after it.
// An empty body reads as {} -- the bodiless reads (me, nonce, revoke) may send none. The error is
// not returned to the client (encoding/json quotes values in its messages).
func decodeAppJSON(body []byte, v any) bool {
	if len(bytes.TrimSpace(body)) == 0 {
		body = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return false
	}
	return dec.Decode(&struct{}{}) == io.EOF
}

// authRead parses and verifies a signed read. It answers the request itself (and returns false)
// on every refusal: 401 empty for any appdevice refusal, 500 for a database failure. channel is
// what the route can say about the tap before anything is verified (signedOutTap's argument): the
// SUN query's channel for tap/start, "" for a route that is not a tap.
func (a *App) authRead(w http.ResponseWriter, r *http.Request, body []byte, where, channel string) (appdevice.Device, bool) {
	req, err := appdevice.ReadFromHTTP(r, body)
	if err == nil {
		var dev appdevice.Device
		dev, err = a.devices.VerifyRead(r.Context(), req)
		if err == nil {
			if !a.limiter.AllowDevice(w, r, dev.ID, dev.TenantID, dev.EmployeeID) {
				return appdevice.Device{}, false
			}
			return dev, true
		}
	}
	a.refuseSigned(w, r, err, where, req.DeviceID(), channel)
	return appdevice.Device{}, false
}

// authRequest is authRead for a NONCE-signed request: Verify spends the nonce.
func (a *App) authRequest(w http.ResponseWriter, r *http.Request, body []byte, where, channel string) (appdevice.Device, bool) {
	req, err := appdevice.FromHTTP(r, body)
	if err == nil {
		var dev appdevice.Device
		dev, err = a.devices.Verify(r.Context(), req)
		if err == nil {
			if !a.limiter.AllowDevice(w, r, dev.ID, dev.TenantID, dev.EmployeeID) {
				return appdevice.Device{}, false
			}
			return dev, true
		}
	}
	a.refuseSigned(w, r, err, where, req.DeviceID(), channel)
	return appdevice.Device{}, false
}

// signedOutAppTap is the WARN line of §5 row 3's twin for the app (main-session decision,
// 2026-11-14; M16-07 owns it -- M16-09's card points here): a REVOKED device's tap -- tap/start or
// tap -- is answered with the same 401 as an unknown device, and leaves this line.
//
// 🔴 NO TENANT AND NO EMPLOYEE, ON PURPOSE. A revoked device is refused BEFORE its signature is
// checked (appdevice's no-oracle order), so the device id is only the request's unsigned header:
// attributing the line from it would let anybody who knows (or guesses at) an id write log lines
// naming another business's employee. The line carries the channel the request itself declares
// (or "unverified") and the first 16 hex characters of SHA-256 of the presented id -- enough to
// group repeats of one phone in the log, not the id itself.
const signedOutAppTap = "a signed-out app device tapped"

// presentedDeviceDigest is the log's handle on a CLAIMED device id: 16 hex characters of its
// SHA-256 (the canonical string form the request carried).
func presentedDeviceDigest(id uuid.UUID) string {
	sum := sha256.Sum256([]byte(id.String()))
	return hex.EncodeToString(sum[:])[:16]
}

// refuseSigned answers a signed request appdevice refused. The log carries the route and the
// refusal's fixed Reason() -- never the device id, the nonce, the signature or a body. channel is
// non-empty on the tap routes (tap/start, tap), which are the ones a revoked device's refusal is
// logged for as signedOutAppTap.
func (a *App) refuseSigned(w http.ResponseWriter, r *http.Request, err error, where string, presented uuid.UUID, channel string) {
	ctx := r.Context() // see checkin.go: one level of paren depth for redline R7.
	var ae *appdevice.AuthError
	if !errors.As(err, &ae) {
		a.log.ErrorContext(ctx, "app: authenticating a signed request failed", "at", where, "err", err)
		writeAppError(w, appErrServer)
		return
	}
	if errors.Is(err, appdevice.ErrRevokedDevice) && channel != "" && presented != uuid.Nil {
		// §5 ROW 3'S TWIN: a signed-out phone touched a plaque. No record, no counter spent.
		a.log.WarnContext(ctx, signedOutAppTap, "channel", channel, "device_digest", presentedDeviceDigest(presented))
	} else {
		a.log.InfoContext(ctx, "app: refused a signed request", "at", where, "reason", ae.Reason())
	}
	writeAppUnauthenticated(w)
}

// appBrand is the tap screen's two brand slots for the app: the logo (by digest; the business's
// name is its alt) and the accent. Both are absent for Taptime's own look.
type appBrand struct {
	Name   string   `json:"name,omitempty"`
	Logo   *appLogo `json:"logo,omitempty"`
	Accent string   `json:"accent,omitempty"`
}

type appLogo struct {
	SHA256 string `json:"sha256"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// appBrandOf maps the domain's page brand onto the app's slots -- tapBrandOf's mapping.
func appBrandOf(b tenant.PageBrand) appBrand {
	var out appBrand
	if b.HasLogo {
		out.Name = b.Name
		out.Logo = &appLogo{SHA256: b.Logo.SHA256, Width: b.Logo.Width, Height: b.Logo.Height}
	}
	if b.HasAccent {
		out.Accent = b.Accent.Hex()
	}
	return out
}
