package handler

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/web/templates/components"
)

// DevTap is the DEVELOPMENT-ONLY plaque-tap simulator (ADR 0025, "Geliştirme
// aracı"): POST /dev/simulate-tap picks a plaque, mints the URL its next read
// would produce, and 303s the browser to it. GET /t then runs EXACTLY as for a
// real tap — CompleteByTap with sun.Verify and the atomic counter advance for a
// pending activation, the ordinary tap page for a live session. Nothing here
// skips a check; it replaces the one thing a desktop browser cannot do, a touch.
//
// THREE GATES, any one of which keeps it out of a real deployment:
//
//  1. cmd/tappa only constructs and mounts it when DevToolsEnabled(cfg).
//  2. Mount registers no route unless DevToolsEnabled(cfg) — so even a caller
//     that mounts it unconditionally gets a 404 elsewhere.
//  3. The handler refuses (404) on every request unless DevToolsEnabled(cfg).
//
// DevToolsEnabled is NOT "TAPPA_ENV is dev" alone, because an UNSET TAPPA_ENV
// falls back to dev (internal/config, IsProd's note). It requires the explicit
// TAPPA_DEV_TOOLS=1 opt-in (refused at startup outside dev) AND a loopback base
// URL. And the minting itself needs a Verifier made with
// sun.WithDevelopmentMinting, which checks the same config again.
type DevTap struct {
	enabled bool
	baseURL string
	minter  devMinter
	plaques devPlaques
	invites inviteManager
	session sessionVerifier
	cookies session.Cookies
	codes   codeCookies
	log     *slog.Logger
}

type (
	devMinter interface {
		MintNextTapForDevelopment(ctx context.Context, uid string) (string, error)
	}
	devPlaques interface {
		Screen(ctx context.Context, tenantID uuid.UUID) (tenant.PlaqueScreen, error)
	}
)

// DevToolsEnabled is the gate: TAPPA_DEV_TOOLS=1 on a "dev" deployment whose base
// URL is a loopback address (localhost, 127.0.0.0/8, ::1).
func DevToolsEnabled(cfg *config.Config) bool {
	// THE EXPLICIT OPT-IN COMES FIRST (security audit): TAPPA_DEV_TOOLS=1, which
	// config.Load refuses outside TAPPA_ENV=dev. Env and base URL alone both have
	// DEFAULTS (dev, localhost), so a deployment that forgot them must not get the
	// simulator by omission.
	if cfg == nil || !cfg.DevTools || cfg.Env != config.EnvDev {
		return false
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return false
	}
	return isLoopbackHost(u.Hostname())
}

// NewDevTap wires the simulator. It is safe to construct anywhere: on a
// deployment that is not DevToolsEnabled it mounts nothing and serves nothing.
func NewDevTap(minter devMinter, plaques devPlaques, inv inviteManager, sess sessionVerifier, cfg *config.Config, log *slog.Logger) (*DevTap, error) {
	switch {
	case isNil(minter), isNil(plaques), inv == nil, sess == nil, cfg == nil:
		return nil, errors.New("handler: dev tap: every dependency is required")
	}
	if log == nil {
		log = slog.Default()
	}
	return &DevTap{
		enabled: DevToolsEnabled(cfg),
		baseURL: originOf(cfg.BaseURL),
		minter:  minter, plaques: plaques, invites: inv, session: sess,
		cookies: session.NewCookies(cfg), codes: newCodeCookies(cfg), log: log,
	}, nil
}

// Mount registers the route — on a development deployment only (gate 2).
func (d *DevTap) Mount(r chi.Router) {
	if !d.enabled {
		return
	}
	r.Post(DevSimulateTapPath, d.Simulate)
}

// DevSimulateTapPath is the route. It equals components.DevSimulateTapPath, which
// the strip posts to (pinned by TestDevSimulateTapPath_MatchesTheStrip); it is a
// literal here so the route scanners in this package can read it.
const DevSimulateTapPath = "/dev/simulate-tap"

// Simulate serves POST /dev/simulate-tap.
//
// WHOSE TAP: a browser holding a pending activation taps for THAT invitation's
// employer (preferring the invited employee's own location); otherwise one with
// a live session taps for its employee's employer and location. An optional
// `tag` form value picks a specific plaque of that employer.
func (d *DevTap) Simulate(w http.ResponseWriter, r *http.Request) {
	if !d.enabled { // gate 3
		http.NotFound(w, r)
		return
	}
	if !d.sameOrigin(r) {
		http.Error(w, "dev tap: cross-origin request refused", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "dev tap: unreadable form", http.StatusBadRequest)
		return
	}
	tenantID, locationID, ok := d.whoseTap(w, r)
	if !ok {
		return
	}
	screen, err := d.plaques.Screen(r.Context(), tenantID)
	if err != nil {
		d.log.Error("dev tap: listing plaques failed", "err", err)
		http.Error(w, "dev tap: could not list plaques", http.StatusInternalServerError)
		return
	}
	uid := pickPlaque(screen.Plaques, locationID, strings.ToUpper(strings.TrimSpace(r.PostFormValue("tag"))))
	if uid == "" {
		http.Error(w, "dev tap: this employer has no active plaque on a wall", http.StatusConflict)
		return
	}
	path, err := d.minter.MintNextTapForDevelopment(r.Context(), uid)
	if err != nil {
		d.log.Error("dev tap: minting failed", "tag_uid", uid, "err", err)
		http.Error(w, "dev tap: could not mint a tap for that plaque", http.StatusInternalServerError)
		return
	}
	// The path carries a MAC: it goes to the browser and nowhere else (§4.7).
	d.log.Info("dev tap: simulated a plaque tap", "tag_uid", uid)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, path, http.StatusSeeOther)
}

// whoseTap resolves the employer (and preferred wall) the simulated tap is for.
func (d *DevTap) whoseTap(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	if st, ok := d.codes.read(r); ok && st.pending() {
		ictx, err := d.invites.Lookup(r.Context(), st.code)
		if err != nil && ictx.TenantID == uuid.Nil {
			http.Error(w, "dev tap: the pending activation's invitation does not resolve", http.StatusConflict)
			return uuid.Nil, uuid.Nil, false
		}
		// A dead invitation still names its employer; tapping lets the REAL flow
		// say why it cannot complete, which is what is being tested.
		return ictx.TenantID, ictx.LocationID, true
	}
	tok, err := d.cookies.Read(r)
	if err == nil {
		res, verr := d.session.Verify(r.Context(), tok)
		if verr == nil {
			loc := uuid.Nil
			if ictx, cerr := d.invites.ActivationContext(r.Context(), res.TenantID, res.EmployeeID); cerr == nil {
				loc = ictx.LocationID
			}
			return res.TenantID, loc, true
		}
		if !errors.Is(verr, session.ErrNoSession) && !errors.Is(verr, session.ErrRevoked) {
			d.log.Error("dev tap: verifying the session failed", "err", verr)
			http.Error(w, "dev tap: session lookup failed", http.StatusInternalServerError)
			return uuid.Nil, uuid.Nil, false
		}
	}
	http.Error(w, "dev tap: open an activation link (and consent) or sign in first — there is nobody to tap for",
		http.StatusConflict)
	return uuid.Nil, uuid.Nil, false
}

// pickPlaque chooses an active, mounted plaque: the one asked for, else one at the
// preferred location, else the first of the employer's. "" when none qualifies.
func pickPlaque(plaques []tenant.Plaque, preferred uuid.UUID, asked string) string {
	usable := func(p tenant.Plaque) bool { return p.Status == "active" && p.LocationID != nil }
	if asked != "" {
		for _, p := range plaques {
			if p.UID == asked && usable(p) {
				return p.UID
			}
		}
		return ""
	}
	first := ""
	for _, p := range plaques {
		if !usable(p) {
			continue
		}
		if preferred != uuid.Nil && *p.LocationID == preferred {
			return p.UID
		}
		if first == "" {
			first = p.UID
		}
	}
	return first
}

// sameOrigin is strict: a POST from this origin, or one whose fetch metadata says
// same-origin. An absent Origin with no metadata is refused.
//
// LOOPBACK-EQUIVALENT ORIGINS ARE ONE ORIGIN HERE: browsing the dev server as
// 127.0.0.1:8080 while TAPPA_BASE_URL says localhost:8080 is the same machine and
// port, so it is accepted — same scheme and port, both hosts loopback. This only
// ever runs behind DevToolsEnabled, whose base URL is itself loopback.
func (d *DevTap) sameOrigin(r *http.Request) bool {
	if o := r.Header.Get("Origin"); o != "" && o != "null" {
		if strings.EqualFold(strings.TrimRight(o, "/"), strings.TrimRight(d.baseURL, "/")) {
			return true
		}
		return sameLoopbackOrigin(o, d.baseURL)
	}
	return r.Header.Get("Sec-Fetch-Site") == "same-origin"
}

// sameLoopbackOrigin reports whether two origins name loopback hosts on the same
// scheme and port.
func sameLoopbackOrigin(a, b string) bool {
	ua, err1 := url.Parse(a)
	ub, err2 := url.Parse(b)
	if err1 != nil || err2 != nil || !strings.EqualFold(ua.Scheme, ub.Scheme) || ua.Port() != ub.Port() {
		return false
	}
	return isLoopbackHost(ua.Hostname()) && isLoopbackHost(ub.Hostname())
}

func isLoopbackHost(h string) bool {
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// devToolsContext marks a render context for the dev strip when the deployment is
// DevToolsEnabled; otherwise it returns ctx untouched.
func devToolsContext(ctx context.Context, enabled bool) context.Context {
	if !enabled {
		return ctx
	}
	return components.WithDevTools(ctx)
}
