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

func NewRouter(cfg *config.Config) http.Handler {
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

	// Roadmap: GET /t (tap page), POST /api/checkin, /api/activate,
	// dashboard routes. See docs/handoff.md §8.

	return r
}
