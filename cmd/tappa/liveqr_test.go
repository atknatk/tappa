package main

import (
	"slices"
	"testing"

	"github.com/atknatk/tappa/internal/handler"
	"github.com/atknatk/tappa/internal/liveqr"
)

// TestLiveQR_TheTapPageIsMountedExactlyWhenTheScreensAre is M16-20's hand-off to M16-21: a
// deployment whose TAPPA_LIVE_QR_KEY is set shows codes on its screens, and every one of those
// codes is a URL to GET /q -- so a router with the screen page and without /q would answer every
// scan 404. Both are turned on by ONE function (openLiveQR) from ONE key; this walks the router
// run() composes, off and on, and holds the two together. CONTROL: the walk does see /t and
// /screen, so a missing /q is the router's and not the walk's. M16-22: the app's live QR read is
// on exactly when /q is.
func TestLiveQR_TheTapPageIsMountedExactlyWhenTheScreensAre(t *testing.T) {
	t.Parallel()
	q := "GET " + liveqr.LiveQRTapPath
	screen := "GET " + handler.LiveScreenPath
	for _, on := range []bool{false, true} {
		cfg := walkConfig(t, on)
		if cfg.LiveQRConfigured() != on {
			t.Fatalf("on=%v: the walk's configuration says LiveQRConfigured=%v", on, cfg.LiveQRConfigured())
		}
		f := buildAppFeatures(t, cfg, on)
		routes := walkOf(t, f.router(cfg))
		if !slices.Contains(routes, "GET "+handler.TapPath) {
			t.Fatalf("CONTROL: the walk does not see GET %s", handler.TapPath)
		}
		hasQ, hasScreen := slices.Contains(routes, q), slices.Contains(routes, screen)
		if hasScreen != on {
			t.Fatalf("on=%v: %s mounted=%v", on, screen, hasScreen)
		}
		if hasQ != cfg.LiveQRConfigured() {
			t.Fatalf("LiveQRConfigured=%v but %s mounted=%v: the screens show codes nothing can read", cfg.LiveQRConfigured(), q, hasQ)
		}
		// M16-22: the app's tap/start reads the same codes, turned on by the same call.
		if got := f.app.ServesLiveQR(); got != cfg.LiveQRConfigured() {
			t.Fatalf("LiveQRConfigured=%v but the app reads live QR codes=%v: the browser and the app disagree", cfg.LiveQRConfigured(), got)
		}
	}
}
