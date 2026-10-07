package components

import "context"

// devToolsKey marks a render context as belonging to a DEVELOPMENT deployment
// (ADR 0025, "Geliştirme aracı"). Only internal/handler sets it, and only when
// handler.DevToolsEnabled says the deployment is dev on a loopback address; every
// other render — production, staging, and every test that renders a page with a
// plain context — leaves it unset, so the dev strip cannot appear there.
type devToolsKey struct{}

// WithDevTools returns ctx marked for development rendering.
func WithDevTools(ctx context.Context) context.Context {
	return context.WithValue(ctx, devToolsKey{}, true)
}

// DevToolsOn reports whether ctx was marked by WithDevTools.
func DevToolsOn(ctx context.Context) bool {
	on, _ := ctx.Value(devToolsKey{}).(bool)
	return on
}

// DevSimulateTapPath is the dev-only route the strip posts to. It is mounted only
// on a development deployment (internal/handler/devtap.go).
const DevSimulateTapPath = "/dev/simulate-tap"
