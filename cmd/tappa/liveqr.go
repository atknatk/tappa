package main

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/handler"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/liveqr"
	"github.com/atknatk/tappa/internal/session"
)

// openLiveQR builds the live QR screens (M16-20, ADR 0045 §2.5) when TAPPA_LIVE_QR_KEY is set: the
// panel's option (handler.LiveQRScreens -- the venue's screens block and its two writes) and the
// screen page (/screen). Unset, it answers no option and a NIL Mounter -- the feature is off, the
// paths answer 404, and the panel draws no block (NewAdminAuth refuses the key without the option,
// so the two cannot disagree).
//
// The screen's cookie takes the employee session's Secure rule (session.NewCookies), so a plain-http
// development server is the only place it is sent without Secure.
//
// AND THE TAP PAGE THAT READS THE CODES (M16-21): GET /q, on the tap surface (tap.ServeLiveQR), from
// the SAME keys. This is the one place TAPPA_LIVE_QR_KEY turns anything on, so a screen that shows
// codes and a server that cannot take them cannot both exist -- a key with no /q would make every
// scan a 404 (M16-20's hand-off; TestLiveQR_TheTapPageIsMountedExactlyWhenTheScreensAre). The
// native app's read of the same codes (POST /app/v1/tap/start's liveQR, M16-22) is turned on here
// too (app.ServeLiveQR), for the same reason: a key the browser reads and the app does not would
// make every app scan a bad_request.
func openLiveQR(cfg *config.Config, data liveQRData, trail liveqr.Trail, tap *handler.Tap, app *handler.App, log *slog.Logger) ([]handler.AdminAuthOption, httpx.Mounter, error) {
	if !cfg.LiveQRConfigured() {
		return nil, nil, nil
	}
	keys, err := liveqr.NewKeys(cfg.LiveQRKey.Reveal())
	if err != nil {
		return nil, nil, err
	}
	if err := tap.ServeLiveQR(keys, data); err != nil {
		return nil, nil, err
	}
	// The app reads the same codes (M16-22): POST /app/v1/tap/start's liveQR, GET /q's twin.
	if err := app.ServeLiveQR(keys, data); err != nil {
		return nil, nil, err
	}
	screens, err := liveqr.NewScreens(keys, data, trail)
	if err != nil {
		return nil, nil, err
	}
	page, err := handler.NewLiveScreen(screens, cfg.BaseURL, session.NewCookies(cfg).Secure(), log)
	if err != nil {
		return nil, nil, err
	}
	screenURL := strings.TrimRight(cfg.BaseURL, "/") + handler.LiveScreenPath
	return []handler.AdminAuthOption{handler.LiveQRScreens(screens, screenURL)}, page, nil
}

// liveQRData is what the live QR surfaces read: the screens' own resolvers (liveqr.Database) and the
// tap path's context-less screen resolver (resolve_live_qr_display_by_id, M16-21).
type liveQRData interface {
	liveqr.Database
	GetLiveQRDisplayByID(ctx context.Context, displayID uuid.UUID) (db.ResolvedLiveQRDisplay, error)
}
