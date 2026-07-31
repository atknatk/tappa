// Package httpx wires the HTTP surface: router, middleware and static assets.
// Business rules live in internal/domain; handlers live in internal/handler.
package httpx

import (
	"net/http"
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
	// No client-IP middleware on purpose: r.RemoteAddr stays the raw TCP peer.
	// chi's RealIP rewrites it from X-Forwarded-For / True-Client-IP / X-Real-IP
	// whether or not our infrastructure sets them, so it hands out a forgeable
	// address. IP matching is worth 50 trust points in the tap decision engine
	// (CLAUDE.md §5), so a spoofable value is worse than no value at all. The
	// real resolver, bounded by cfg.TrustedProxies, arrives in M5-03 (docs/plan);
	// until then nothing in this router reads the client IP.
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

	return r
}
