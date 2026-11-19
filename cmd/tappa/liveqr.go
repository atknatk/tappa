package main

import (
	"log/slog"
	"strings"

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
func openLiveQR(cfg *config.Config, data liveqr.Database, trail liveqr.Trail, log *slog.Logger) ([]handler.AdminAuthOption, httpx.Mounter, error) {
	if !cfg.LiveQRConfigured() {
		return nil, nil, nil
	}
	keys, err := liveqr.NewKeys(cfg.LiveQRKey.Reveal())
	if err != nil {
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
