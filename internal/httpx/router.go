// Package httpx wires the HTTP surface: router, middleware and static assets.
// Business rules live in internal/domain; handlers live in internal/handler.
package httpx

import (
	"io/fs"
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

	// LIVENESS. It lives HERE, on the router, rather than in a feature, and that
	// placement is the guarantee (M8-01): NewRouter is handed no database, no
	// session codec and no audit sink, so this endpoint CANNOT touch any of them —
	// which is precisely what liveness means. "Is this process running and
	// answering HTTP?" must stay answerable while every dependency is down, or a
	// deployment restarts a healthy process because its database is unreachable.
	// READINESS is the opposite question and is a different endpoint with a
	// different owner: /readyz, internal/handler.Health, which is given a pool.
	r.Get(healthPath, live)
	// 🔴 HEAD, BECAUSE THE CLIENTS THAT WATCH THIS URL SEND HEAD. Measured before
	// this line: chi's r.Get registers GET alone, so HEAD /healthz answered 405
	// with `Allow: GET`, and an uptime monitor reads 405 as "the service is
	// broken" — an alert about the check rather than about the service. M7-01 fixed
	// the same defect for the marketing surface and deliberately left the rest of
	// the product alone (backlog T29) because the tap and panel routes meter and
	// resolve identity, so admitting a method there has a blast radius. This one
	// does not: it is the same two bytes to anyone who asks, with no identity, no
	// budget and no query behind it.
	r.Head(healthPath, live)

	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(noListing{http.FS(web.Static())})))

	for _, f := range features {
		if f != nil {
			f.Mount(r)
		}
	}

	// Roadmap: POST /api/checkin (M5-05), dashboard routes. See docs/handoff.md
	// §8. /activate, /activate/done and /api/activate are mounted by
	// internal/handler.Activation (M5-02); GET /t by internal/handler.Tap
	// (M5-04).
	//
	// The tap routes arrive with their OWN middleware group — the identity
	// resolver and the two-sided rate limit — and not from here: both cost
	// something (a database query, a counter) that /healthz and /static must not
	// pay. handler.Tap.Mount applies them in the order TapLimiter's doc fixes as
	// a contract (ByAddress -> Identify -> BySession), and POST /api/checkin
	// belongs in that same group when it lands.

	return r
}

// healthPath is the liveness endpoint. It is a constant so the route and every
// test that drives it are the same string at compile time.
const healthPath = "/healthz"

// live answers the liveness probe.
//
// ⚠️ THE BODY IS TWO BYTES AND CARRIES NO BUILD IDENTITY, WHICH IS A DECISION. This
// process knows exactly which commit it is running (internal/buildinfo, logged at
// start-up), and printing that here would be genuinely convenient for a deploy
// script. It is not printed because this URL is unauthenticated and unmetered: a
// commit hash tells a stranger precisely which code — and therefore which known
// defects — is deployed, and this is the one endpoint every scanner finds. The
// operator has two channels that do not publish it: the start-up log line, and
// `go version -m` on the artifact itself.
//
// no-store, because a cached "ok" answers for a process that has since died — the
// one failure a liveness probe exists to catch.
func live(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte("ok"))
}

// noListing serves files and refuses DIRECTORIES.
//
// http.FileServer renders an index page for any directory it can open, and an
// audit measured what that meant once M5-04 added real asset directories:
// GET /static/fonts/ answered 200 with a complete file listing. Nothing secret
// is in there — the fonts are public assets and their names are already in
// app.css — so this is surface reduction rather than a leak being closed. The
// reason to close it anyway is that a listing is a free inventory of what a
// deployment ships, and the directory only grows.
//
// Refusing the OPEN (rather than filtering the response) keeps it in one place:
// FileServer treats fs.ErrNotExist as a 404, so a directory request answers
// exactly as a missing file does, with no separate code path to keep in sync.
type noListing struct{ fsys http.FileSystem }

func (n noListing) Open(name string) (http.File, error) {
	f, err := n.fsys.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if info.IsDir() {
		_ = f.Close()
		return nil, fs.ErrNotExist
	}
	return f, nil
}
