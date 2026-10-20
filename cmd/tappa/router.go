package main

import (
	"log/slog"
	"net/http"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/handler"
	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/internal/httpx"
)

// appRouter is the HTTP surface's COMPOSITION — which features are mounted, and in
// which order — and the one place it is written down. run() calls it with the live
// features; TestAppRoutes_EveryNonReadRouteHasOneOfFourClasses calls it with the same
// features built over a database that answers nothing, and walks the result.
//
// 🔴 WHY IT EXISTS (ADR 0025 §3, "Hangi yönlendirici yürünür"; OP-15 phase B). The
// suspension gate covers what is mounted behind ProtectWriting by construction, and a
// POST mounted ANYWHERE ELSE is covered by nothing but a test that finds it. That test
// is only as good as the router it walks: before this function the production router
// was assembled inline inside run() and no test could reach it, so a walk would have
// walked a router somebody had rebuilt by hand — the "second representation" that
// drifts. Now the walked router and the served router are composed by this body.
//
// 🔴 EVERY FEATURE IS A NAMED PARAMETER, NOT A ...Mounter. A feature added to the
// product has to be added here (run() reaches httpx.NewRouter through nothing else —
// TestAppRouter_IsTheOnlyComposition holds that), which makes the walk's own call
// stop compiling until the feature is built there too; with a variadic list, run()
// could pass one more argument and the walk would never see it. Two are typed as
// httpx.Mounter rather than as their concrete type, because each is either a feature
// or NOTHING and NewRouter skips a nil interface (a nil *T inside one is not nil): the
// integration API (M11 API-4 -- openAPISurface answers nil without TAPPA_API_HOST) and
// the dev-only tap simulator (ADR 0026 -- nil unless TAPPA_DEV_TOOLS passes its gate).
// They are still positional and named, so a third feature is still a compile error in
// the walk.
//
// It does nothing else: no middleware, no route of its own. NewRouter owns those.
func appRouter(cfg *config.Config, log *slog.Logger,
	activation *handler.Activation,
	tap *handler.Tap,
	panel *handler.AdminAuth,
	logos *handler.BrandLogos,
	marketing *handler.Marketing,
	signupFlow *handler.Signup,
	resetFlow *handler.AdminReset,
	ready *handler.Health,
	theme *handler.BrandTheme,
	operatorSurface *operator.Surface,
	integrationAPI httpx.Mounter,
	devTools httpx.Mounter,
) http.Handler {
	return httpx.NewRouter(cfg, log, activation, tap, panel, logos, marketing, signupFlow, resetFlow, ready, theme, operatorSurface, integrationAPI, devTools)
}
