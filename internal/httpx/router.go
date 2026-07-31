// Package httpx wires the HTTP surface: router, middleware and static assets.
// Business rules live in internal/domain; handlers live in internal/handler.
package httpx

import (
	"net/http"
	"net/netip"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/web"
)

// Mounter is a feature that registers its own routes. Declared HERE at the
// consumer (CLAUDE.md §7) so this package does not import every handler it
// serves, and so a new screen is added by passing it in rather than by editing
// this file.
type Mounter interface {
	Mount(r chi.Router)
}

// NewRouter builds the HTTP surface. Features are mounted in the order given.
func NewRouter(cfg *config.Config, features ...Mounter) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	// The client address is resolved ONCE, here, for the whole surface, and
	// bounded by cfg.TrustedProxies (realip.go). It is deliberately NOT chi's
	// middleware.RealIP, which believes X-Forwarded-For / True-Client-IP /
	// X-Real-IP unconditionally and reads the chain from the client-controlled
	// end. An IP match is worth 50 trust points in the tap decision engine
	// (CLAUDE.md §5) and is the key every abuse budget is metered on, so a
	// forgeable address is worse than none.
	//
	// It is global because it is pure and cheap — no database, no allocation
	// past one context value — and because a route that resolved a DIFFERENT
	// client address than its neighbour would be a bug waiting to be written.
	// r.RemoteAddr is left alone.
	//
	// httpx.ClientIP is where the client address comes from. That is a RULE, not
	// something this file enforces: RealIP strips the forwarding headers it knows
	// about, and a header it does not know about still arrives. realip.go states
	// the boundary precisely — reading any header for an address is a new bug,
	// not a fallback. (An earlier version of this comment said "the single
	// authority" flatly; the same over-claim next door had already been reduced,
	// and leaving its twin here would have left the repo saying two things.)
	var trusted []netip.Prefix
	if cfg != nil {
		trusted = cfg.TrustedProxies
	}
	r.Use(RealIP(trusted))
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})

	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(web.Static()))))

	for _, f := range features {
		if f != nil {
			f.Mount(r)
		}
	}

	// Roadmap: GET /t (tap page), POST /api/checkin, dashboard routes.
	// See docs/handoff.md §8. /activate, /activate/done and /api/activate are
	// mounted by internal/handler.Activation (M5-02).
	//
	// The tap routes arrive with their own middleware group — the identity
	// resolver and the two-sided rate limit — and NOT here: both cost something
	// (a database query, a counter) that /healthz and /static must not pay, and
	// mounting a shield in front of routes that do not exist would be a claim
	// this file cannot keep. TapLimiter's doc carries the exact order M5-04 and
	// M5-05 must use.

	return r
}
