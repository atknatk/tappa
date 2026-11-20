package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/liveqr"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/atknatk/tappa/web/templates/layout"
	"github.com/atknatk/tappa/web/templates/pages"
)

// GET /q -- THE LIVE QR TAP PAGE (M16-21, ADR 0045 §2.5). A phone's camera reads a venue
// screen's rotating code (M16-20) and opens <base>/q?t=<token>; this page is GET /t's twin for
// that arrival, and its button is the SAME POST /api/checkin. Nothing on the page is new: it
// is pages.Tap exactly as GET /t draws it (CLAUDE.md §9 -- the tap screen is not touched), and
// the difference is entirely in what the signed context carries.
//
// THE TOKEN IS OPENED HERE, ONCE, AND NEVER LEAVES THE SERVER. Open checks its shape and its
// tag (constant time, before any field is believed -- internal/liveqr); the screen it names is
// resolved from its ROW, context-less (resolve_live_qr_display_by_id: in a holding the screen is
// another tenant's), and the row's venue must be the token's signed one (a screen's venue never
// changes -- a difference is a forgery or a bug). What crosses to the browser is the OUTCOME,
// inside the context's MAC: the screen, "verified", the token's own expiry -- the CMAC's
// discipline (tapcontext.go). The token itself is written to no log line, no page, no error body
// and no audit detail (§7): not even the retry link GET /t offers on a 500, whose URL would
// carry it.
//
// THE PAGE RENDERS FOR A TOKEN THAT DID NOT VERIFY, as GET /t renders for a CMAC that did not:
// the refusal is a RECORDED reject (Q39 (a), sys:live-qr-unverified) and the record is written by
// the press (§4.6). A removed screen, a switched-off venue, an expired token and a peer's screen
// render too, for the same reason: the decision, and its record, are the POST's.
//
// WHAT THIS PAGE DOES NOT DO: complete an activation. A live QR activation is M16-23's; until
// then a phone without a session goes to the activation page (§5 row 3, nothing recorded), and a
// pending activation with a live session is an ordinary tap -- GET /t's rule for a static QR,
// which cannot activate either.

// LiveQRPath is the route the live QR tap page is served at. It is a LITERAL, not liveqr.LiveQRTapPath,
// because the route inventory (admincookiepath_test.go) resolves route names from this package's own
// literals; TestLiveQRPath_IsThePathTheCodesEncode holds the two to one string -- the screens' codes
// (liveqr.TapURL) and this route cannot name two paths.
const LiveQRPath = "/q"

// liveQRDisplays is the context-less screen resolver GET /q needs (§7: declared at the consumer).
type liveQRDisplays interface {
	GetLiveQRDisplayByID(ctx context.Context, displayID uuid.UUID) (db.ResolvedLiveQRDisplay, error)
}

// liveQRTap is what GET /q needs beyond GET /t's own dependencies.
type liveQRTap struct {
	keys    *liveqr.Keys
	screens liveQRDisplays
}

// ServeLiveQR turns GET /q on: the screens' token keys (the same TAPPA_LIVE_QR_KEY the screen
// page mints with) and the screen resolver. Call it before Mount. cmd/tappa's openLiveQR is the
// one caller, so the screen page (/screen) and this page are on or off together -- a key that
// shows codes and no /q to read them would be a 404 for every scan (M16-20's hand-off;
// cmd/tappa's TestLiveQR_TheTapPageIsMountedExactlyWhenTheScreensAre).
func (t *Tap) ServeLiveQR(keys *liveqr.Keys, screens liveQRDisplays) error {
	live, err := newLiveQRTap(keys, screens)
	if err != nil {
		return err
	}
	t.live = live
	return nil
}

// LivePage serves GET /q.
func (t *Tap) LivePage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context() // see checkin.go: one level of paren depth for redline R7.

	// IDENTITY FIRST, GET /t's order and its reason: an anonymous caller learns nothing about a
	// token -- not whether it opens, not which screen it names.
	id := httpx.IdentityOf(r)
	switch id.State {
	case httpx.SessionUnresolved:
		t.log.ErrorContext(ctx, "live QR page: no resolved identity on the request",
			"hint", "mount httpx.Identify in front of GET /q", "err", id.Err)
		// NO retry link: its URL would put the token into the error page (§7).
		t.renderProblem(w, r, http.StatusInternalServerError, tapProblemServer)
		return
	case httpx.SessionAbsent, httpx.SessionRevoked:
		// §5 ROW 3: no session -> the activation page, NO record. A pending activation is
		// not completed here (M16-23).
		if id.State == httpx.SessionRevoked {
			t.log.WarnContext(ctx, "live QR page: a signed-out phone scanned",
				"tenant_id", id.Session.TenantID, "employee_id", id.Session.EmployeeID, "channel", string(sun.ChannelQRLive))
		}
		t.redirectToActivation(w, r)
		return
	case httpx.SessionLive:
	default:
		t.log.ErrorContext(ctx, "live QR page: unknown session state", "state", int(id.State))
		t.renderProblem(w, r, http.StatusInternalServerError, tapProblemServer)
		return
	}

	o, ok := t.openLiveQR(ctx, w, r, id)
	if !ok {
		return
	}
	live := liveTapOf(o, id.Session.TenantID)
	facts, err := t.directory.TapPage(ctx, id.Session.TenantID, id.Session.EmployeeID, live.Context.LocationID, live.Predict)
	var (
		look      layout.Brand
		direction pages.TapDirection
	)
	switch {
	case errors.Is(err, tenant.ErrForeignLocation):
		// Another business's screen (a peer's -- refused at the press, Q43 (b) -- or one outside
		// the holding -- redirected), or no screen believed: no venue name, no brand (ADR 0023
		// §2's mismatch rule), and the decision is the POST's.
		t.log.WarnContext(ctx, "live QR page: no venue of this business to show",
			"employee_id", id.EmployeeID(), "session_tenant_id", id.TenantID(),
			"display_id", o.Screen.ID, "screen_tenant_id", live.Context.TagTenantID, "verified", o.Verified)
	case errors.Is(err, tenant.ErrBrandUnread):
		t.log.ErrorContext(ctx, "live QR page: reading the business's brand failed; drawing Taptime's",
			"tenant_id", id.TenantID(), "employee_id", id.EmployeeID(), "err", err)
	case err != nil:
		t.log.ErrorContext(ctx, "live QR page: loading the employee failed", "employee_id", id.EmployeeID(), "err", err)
		t.renderProblem(w, r, http.StatusInternalServerError, tapProblemServer)
		return
	default:
		if facts.Brand.AccentRefused {
			t.log.WarnContext(ctx, "live QR page: the stored accent does not pass the legibility gate today; the screen draws no accent",
				"tenant_id", id.TenantID())
		}
		look = tapBrandOf(facts.Brand)
		if facts.PredictionErr != nil {
			t.log.ErrorContext(ctx, "live QR page: reading the predicted direction failed; the button says Tap",
				"tenant_id", id.TenantID(), "employee_id", id.EmployeeID(), "err", facts.PredictionErr)
		}
		direction = tapDirectionOf(facts.Prediction)
	}

	signed, err := t.contexts.mint(live.Context, id.Session.ID)
	if err != nil {
		t.log.ErrorContext(ctx, "live QR page: minting the tap context failed", "err", err)
		t.renderProblem(w, r, http.StatusInternalServerError, tapProblemServer)
		return
	}
	// QR is false on purpose: it is the form's data-tap-channel="qr", which lets tap.js reload a
	// page restored from the back-forward cache (M15-14). Reloading a live QR page would open the
	// SAME token again, long expired -- a fresh code comes from the screen, not from a reload --
	// so the live page keeps the NFC behaviour. Nothing visible depends on it (view.go).
	t.render(w, r, http.StatusOK, pages.Tap(pages.TapView{
		EmployeeName: facts.EmployeeName,
		LocationName: facts.LocationName,
		TapContext:   signed,
		Direction:    direction,
	}, look), look.Logo().Drawn())
}

// openLiveQR opens the request's token and resolves the screen it names. ok=false means the
// response is already written (an infrastructure failure). o.Verified is the outcome the context
// carries: the token opened AND the screen exists AND its venue is the token's.
//
// What is logged is a KIND, never the token and never Open's error text (§7, the card's log
// rule): "malformed" or "signature", and the screen id once the token is genuine.
func (t *Tap) openLiveQR(ctx context.Context, w http.ResponseWriter, r *http.Request, id httpx.Identity) (liveQROpening, bool) {
	o, err := t.live.open(ctx, r.URL.Query().Get(liveqr.TokenParam))
	if err != nil {
		t.log.ErrorContext(ctx, "live QR page: resolving the screen failed", "display_id", o.Claims.DisplayID, "err", err)
		t.renderProblem(w, r, http.StatusInternalServerError, tapProblemServer)
		return liveQROpening{}, false
	}
	switch o.Refusal {
	case liveRefusalMalformed, liveRefusalSignature:
		t.log.WarnContext(ctx, "live QR page: the code did not verify; the press records a reject",
			"kind", o.Refusal, "employee_id", id.EmployeeID(), "tenant_id", id.TenantID())
	case liveRefusalUnknownScreen:
		t.log.WarnContext(ctx, "live QR page: a genuine code names a screen we do not hold; the press records a reject",
			"display_id", o.Claims.DisplayID, "employee_id", id.EmployeeID())
	case liveRefusalVenueMismatch:
		t.log.ErrorContext(ctx, "live QR page: a genuine code names a venue its screen is not at; the press records a reject",
			"display_id", o.Claims.DisplayID, "employee_id", id.EmployeeID())
	}
	return o, true
}

// liveTap is what both readers (GET /q and the app's tap/start) derive from one opening: the
// signed context's live half and the prediction gate. One function, so the two screens cannot
// disagree about what a press may record (M16-22 audit DÜŞÜK-2).
type liveTap struct {
	// Context is the version-2 tap context to mint: the screen, "verified", the token's own
	// expiry, the screen's tenant and venue -- all zero unless the token verified. IssuedAt is the
	// mint's.
	Context tapContext
	// Predict says whether the predicted direction may be shown: only on a screen that can record
	// an `ok` -- verified, in service, the person's own business (predictionAllowed's rule for a
	// plaque). The directory answers the rest of the gate.
	Predict bool
}

func liveTapOf(o liveQROpening, sessionTenant uuid.UUID) liveTap {
	c := tapContext{Channel: sun.ChannelQRLive, TokenVerified: o.Verified}
	if o.Verified {
		c.DisplayID, c.TokenExpiresAt = o.Screen.ID, o.Claims.ExpiresAt()
		c.TagTenantID, c.LocationID = o.Screen.TenantID, o.Screen.LocationID
	}
	return liveTap{
		Context: c,
		Predict: o.Verified && o.Screen.RevokedAt == nil && o.Screen.LocationActive && o.Screen.TenantID == sessionTenant,
	}
}

// Why a live QR token did not verify: the log's KIND (never the token, never Open's error text).
const (
	liveRefusalMalformed     = "malformed"
	liveRefusalSignature     = "signature"
	liveRefusalUnknownScreen = "unknown_screen"
	liveRefusalVenueMismatch = "venue_mismatch"
)

// liveQROpening is what opening one live QR token established: the screen's row and the token's
// claims when Verified, else Refusal names the kind (Claims still carries a GENUINE token's screen
// id for the log -- an id, never the token).
type liveQROpening struct {
	Screen   db.ResolvedLiveQRDisplay
	Claims   liveqr.Claims
	Verified bool
	Refusal  string
}

// newLiveQRTap is the one constructor of the live QR reader GET /q (Tap.ServeLiveQR) and the app's
// tap/start (App.ServeLiveQR) share: the same keys, the same context-less screen resolver.
func newLiveQRTap(keys *liveqr.Keys, screens liveQRDisplays) (*liveQRTap, error) {
	switch {
	case keys == nil:
		return nil, errors.New("handler: nil live QR keys")
	case isNil(screens):
		return nil, errors.New("handler: nil live QR screen resolver")
	}
	return &liveQRTap{keys: keys, screens: screens}, nil
}

// open is THE token check of both readers (GET /q and POST /app/v1/tap/start, M16-22): Open (shape,
// then the tag in constant time, before any field is believed), the screen resolved from its ROW
// (resolve_live_qr_display_by_id, context-less: in a holding the screen is another tenant's), and
// the row's venue equal to the token's signed one. A token that does not verify is NOT an error:
// it is Q39 (a)'s recorded reject, decided by the press. err is an infrastructure failure only.
func (l *liveQRTap) open(ctx context.Context, token string) (liveQROpening, error) {
	claims, err := l.keys.Open(token)
	if err != nil {
		kind := liveRefusalMalformed
		if errors.Is(err, liveqr.ErrSignature) {
			kind = liveRefusalSignature
		}
		return liveQROpening{Refusal: kind}, nil
	}
	screen, err := l.screens.GetLiveQRDisplayByID(ctx, claims.DisplayID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return liveQROpening{Claims: claims, Refusal: liveRefusalUnknownScreen}, nil
	case err != nil:
		return liveQROpening{Claims: claims}, err
	}
	if screen.LocationID != claims.LocationID {
		return liveQROpening{Claims: claims, Refusal: liveRefusalVenueMismatch}, nil
	}
	return liveQROpening{Screen: screen, Claims: claims, Verified: true}, nil
}
