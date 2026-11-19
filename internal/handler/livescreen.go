package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/liveqr"
	"github.com/atknatk/tappa/internal/qrcode"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE LIVE QR SCREEN PAGE (M16-20, ADR 0045 §2.5): what a venue's tablet, TV or old phone opens.
// No app, no account: the manager presses "Add a live QR screen" in the panel, types the code it
// shows here once, and from then on this browser is the screen -- it holds one HttpOnly cookie
// (internal/liveqr's ScreenSession) and nothing else, and it shows a QR code the server signs and
// rotates every ~20 seconds.
//
//	GET  /screen       the pairing form, or -- for a paired screen -- the QR page
//	POST /screen/pair  the code -> a paired screen (303 to /screen), or the form again
//	GET  /screen/code  the QR page's poll: the next code's drawing as JSON (no token in text)
//
// MOUNTED ONLY WHILE TAPPA_LIVE_QR_KEY IS SET (cmd/tappa): unset, nothing here exists and the paths
// answer the router's plain 404 ("screen" is one of plainNotFoundRoots).
//
// 🔴 THE PAGE SHOWS NO PERSONAL DATA (the card): the venue's name, the code, one sentence. No
// employee, no list, no count -- a screen is a location's, and anyone in the room can read it.
//
// 🔴 AN OFFLINE SCREEN STOPS SHOWING ITS CODE (the card): the poll's answer says how long the code
// it carries is valid, and the page's script hides the code once that has run out without a newer
// one (web/static/js/livescreen.js). The page itself carries no code: it is drawn only from a poll
// answer, so a page loaded from a cache or kept open on a dead network has nothing stale to show.
//
// 🔴 NOTHING HERE LOGS A CODE, A SESSION OR A TOKEN (§7): the log lines carry fixed words, the
// screen's id and the venue's, never a value the request brought or the poll answers.
type LiveScreen struct {
	screens liveScreenService
	cookies liveqr.ScreenCookies
	base    string
	// origin is base's scheme://host[:port], what a same-origin POST's Origin header says.
	origin string
	log    *slog.Logger
	// pairFailures meters refused pairing attempts per client address; reqs meters every request
	// to the page per address. Both are in-process windows (httpx.Limiter's limits).
	pairFailures *httpx.Limiter
	reqs         *httpx.Limiter
}

// liveScreenService is the slice of *liveqr.Screens this page uses, declared at the consumer.
type liveScreenService interface {
	Pair(ctx context.Context, code liveqr.PairingCode) (liveqr.Screen, liveqr.ScreenSession, error)
	Resolve(ctx context.Context, sess liveqr.ScreenSession) (liveqr.Screen, error)
	Token(sc liveqr.Screen) (string, time.Time, error)
}

// The page's paths. LiveScreenPath is liveqr.ScreenCookiePath -- the cookie is scoped to this tree --
// written as a literal so the employee-route scan resolves it (TestLiveScreen_TheFormPostsToThePairRoute
// holds the two equal).
const (
	LiveScreenPath     = "/screen"
	LiveScreenPairPath = LiveScreenPath + "/pair"
	LiveScreenCodePath = LiveScreenPath + "/code"
)

// The two budgets, per client address (httpx.RateKey: an IPv4 address, an IPv6 /64).
//
// PAIRING FAILURES: 20 per 15 minutes. The code's 65 bits are the defence (internal/liveqr's
// code.go measures it against this budget); the budget bounds cost and keeps a mistyping person
// several tries. A success is free.
//
// REQUESTS: 600 per 10 minutes -- a screen polls about every 17 s, 35 times in 10 minutes, so one
// address can carry about fifteen screens (a venue's NAT) with room for reloads.
const (
	liveScreenPairFailureLimit  = 20
	liveScreenPairFailurePeriod = 15 * time.Minute
	liveScreenRequestLimit      = 600
	liveScreenRequestPeriod     = 10 * time.Minute
)

// liveScreenRefreshLead is how long before a code runs out the page asks for the next one, so the
// new code is on the screen before the old one stops counting.
const liveScreenRefreshLead = 3 * time.Second

// maxLiveScreenBody bounds the pairing POST: one field of about fifteen characters.
const maxLiveScreenBody = 1 << 10

// liveScreenQRLevel is the QR's error correction: M, the printed plaque's (plaqueQRLevel), for a
// screen seen at an angle, behind glare or a smudge.
const liveScreenQRLevel = qrcode.M

// liveScreenCSP is the page's content policy: the tap screen's (tapCSP -- one stylesheet, one
// script, our own fonts, no frame, no form elsewhere) plus connect-src 'self' for the poll, the
// activation waiting screen's addition (activationCSP). No img-src: the code is inline SVG.
const liveScreenCSP = tapCSP + "; connect-src 'self'"

// NewLiveScreen builds the page. secure is the cookie's Secure flag (session.NewCookies(cfg)
// .Secure(): always outside a plain-http development server); base is TAPPA_BASE_URL, the host the
// QR's URL names.
func NewLiveScreen(screens liveScreenService, base string, secure bool, log *slog.Logger) (*LiveScreen, error) {
	if isNil(screens) || base == "" {
		return nil, errors.New("handler: live screen: the screens service and the base URL are required")
	}
	if log == nil {
		log = slog.Default()
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, errors.New("handler: live screen: the base URL has no scheme or host")
	}
	return &LiveScreen{
		origin:       u.Scheme + "://" + u.Host,
		screens:      screens,
		cookies:      liveqr.NewScreenCookies(secure),
		base:         base,
		log:          log,
		pairFailures: httpx.NewLimiter(liveScreenPairFailureLimit, liveScreenPairFailurePeriod),
		reqs:         httpx.NewLimiter(liveScreenRequestLimit, liveScreenRequestPeriod),
	}, nil
}

// Mount registers the three routes. A nil *LiveScreen mounts nothing (AppLinks.Mount's reason).
func (s *LiveScreen) Mount(r chi.Router) {
	if s == nil {
		return
	}
	r.Get(LiveScreenPath, s.page)
	r.Post(LiveScreenPairPath, s.pair)
	r.Get(LiveScreenCodePath, s.code)
}

// metered charges the request budget and answers 429 when it is spent.
func (s *LiveScreen) metered(w http.ResponseWriter, r *http.Request, asJSON bool) bool {
	if n := s.reqs.Charge(httpx.RateKey(httpx.ClientIP(r))); n > s.reqs.Limit() {
		if s.reqs.FirstOverLimit(n) {
			s.log.Warn("live QR screen: request budget reached", "limit", s.reqs.Limit(), "period", s.reqs.Period().String())
		}
		w.Header().Set("Retry-After", "60")
		if asJSON {
			writeScreenJSON(w, http.StatusTooManyRequests, screenState{State: "busy"})
			return false
		}
		s.render(w, r, http.StatusTooManyRequests, pages.LiveScreenPair(pages.LiveScreenPairView{
			Problem: pages.LiveScreenProblemBusy,
		}), tapCSP)
		return false
	}
	return true
}

// page is GET /screen.
func (s *LiveScreen) page(w http.ResponseWriter, r *http.Request) {
	if !s.metered(w, r, false) {
		return
	}
	sess := s.cookies.Read(r)
	if sess.IsZero() {
		s.renderPairForm(w, r, http.StatusOK, "")
		return
	}
	sc, err := s.screens.Resolve(r.Context(), sess)
	switch {
	case errors.Is(err, liveqr.ErrNoScreen):
		s.cookies.Clear(w)
		s.renderPairForm(w, r, http.StatusOK, "")
	case errors.Is(err, liveqr.ErrRevoked):
		s.log.Info("live QR screen: a removed screen opened its page", "display_id", sc.ID)
		s.cookies.Clear(w)
		s.renderPairForm(w, r, http.StatusOK, pages.LiveScreenProblemRemoved)
	case errors.Is(err, liveqr.ErrLapsed):
		s.log.Info("live QR screen: a screen whose pairing ran out opened its page", "display_id", sc.ID)
		s.cookies.Clear(w)
		s.renderPairForm(w, r, http.StatusOK, pages.LiveScreenProblemLapsed)
	case err != nil:
		s.log.Error("live QR screen: could not resolve the screen", "err", err)
		s.renderPairForm(w, r, http.StatusServiceUnavailable, pages.LiveScreenProblemUnavailable)
	default:
		// The page is drawn in every state of a paired screen -- a switched-off venue and a suspended
		// business included -- so its script keeps asking and the code comes back by itself.
		s.render(w, r, http.StatusOK, pages.LiveScreen(pages.LiveScreenView{
			VenueName: sc.LocationName,
			State:     screenPause(sc),
			CodeURL:   LiveScreenCodePath,
		}), liveScreenCSP)
	}
}

// screenPause is why a paired screen shows no code now, or "": the operator's suspension of the
// business (ADR 0025 §1 -- the security lens's ORTA-2: a suspended business's screens mint nothing)
// before a venue switched off in the panel. The word is all the screen says: no reason, no name.
func screenPause(sc liveqr.Screen) string {
	switch {
	case sc.Suspended:
		return pages.LiveScreenStatePaused
	case !sc.LocationActive:
		return pages.LiveScreenStateInactive
	}
	return ""
}

// pair is POST /screen/pair.
func (s *LiveScreen) pair(w http.ResponseWriter, r *http.Request) {
	if !s.metered(w, r, false) {
		return
	}
	// A pairing started by another site is refused before any work: the form is only ever posted
	// from this page. Sec-Fetch-Site decides when the browser sends it; without it, an Origin that is
	// not TAPPA_BASE_URL's is refused too (a browser too old for Fetch Metadata still sends Origin on
	// a POST). Neither header at all is a non-browser client, which the code itself has to beat.
	if !s.sameOriginPost(r) {
		s.log.Warn("live QR screen: a pairing from another site was refused")
		s.renderPairForm(w, r, http.StatusForbidden, pages.LiveScreenProblemRefused)
		return
	}
	// THE FAILURE BUDGET IS RESERVED, NOT READ (backlog T90): the attempt holds a unit from the moment
	// it is admitted, so simultaneous guesses cannot all pass a check made before any of them failed.
	// A refusal keeps the unit; a success, or our own failure, gives it back.
	res, within, first, _ := s.pairFailures.Reserve(httpx.RateKey(httpx.ClientIP(r)))
	if !within {
		if first {
			s.log.Warn("live QR screen: pairing failure budget reached", "limit", s.pairFailures.Limit(),
				"period", s.pairFailures.Period().String())
		}
		w.Header().Set("Retry-After", "900")
		s.renderPairForm(w, r, http.StatusTooManyRequests, pages.LiveScreenProblemBusy)
		return
	}
	defer res.Refund() // a no-op after Keep
	r.Body = http.MaxBytesReader(w, r.Body, maxLiveScreenBody)
	if err := r.ParseForm(); err != nil {
		res.Keep()
		s.renderPairForm(w, r, http.StatusBadRequest, pages.LiveScreenProblemRefused)
		return
	}
	code, ok := liveqr.ParsePairingCode(r.PostFormValue("code"))
	if !ok {
		res.Keep()
		s.refusePairing(w, r, "unreadable")
		return
	}
	sc, sess, err := s.screens.Pair(r.Context(), code)
	switch {
	case errors.Is(err, liveqr.ErrCodeRefused):
		res.Keep()
		s.refusePairing(w, r, "refused")
		return
	case err != nil:
		s.log.Error("live QR screen: pairing failed", "err", err)
		s.renderPairForm(w, r, http.StatusServiceUnavailable, pages.LiveScreenProblemUnavailable)
		return
	}
	if err := s.cookies.Set(w, sess); err != nil {
		s.log.Error("live QR screen: the paired screen's cookie could not be set", "err", err, "display_id", sc.ID)
		s.renderPairForm(w, r, http.StatusInternalServerError, pages.LiveScreenProblemUnavailable)
		return
	}
	s.log.Info("live QR screen paired", "display_id", sc.ID, "tenant_id", sc.TenantID, "location_id", sc.LocationID)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", LiveScreenPath)
	w.WriteHeader(http.StatusSeeOther)
}

// sameOriginPost reports whether a pairing POST may have come from this page: Sec-Fetch-Site, when
// present, must not say same-site or cross-site; when absent, an Origin, when present, must be the
// base URL's own origin ("null" included in the refusals).
func (s *LiveScreen) sameOriginPost(r *http.Request) bool {
	if site := strings.ToLower(r.Header.Get("Sec-Fetch-Site")); site != "" {
		return site != "cross-site" && site != "same-site"
	}
	origin := r.Header.Get("Origin")
	return origin == "" || origin == s.origin
}

// refusePairing logs the refusal (the reason word only) and answers the form again with one sentence
// for every reason. The failure budget was settled by the caller (Keep).
func (s *LiveScreen) refusePairing(w http.ResponseWriter, r *http.Request, reason string) {
	s.log.Info("live QR screen: pairing refused", "reason", reason)
	s.renderPairForm(w, r, http.StatusBadRequest, pages.LiveScreenProblemRefused)
}

// screenCode is the poll's answer: the drawing of the next code (an SVG path and its side) and two
// durations in whole seconds -- how long the drawn code counts, and when to ask again. The token
// itself is not in it: only its drawing is, which is what the screen shows anyway.
type screenCode struct {
	Path      string `json:"d"`
	Side      int    `json:"side"`
	ExpiresIn int    `json:"expiresIn"`
	RefreshIn int    `json:"refreshIn"`
}

// screenState is the poll's answer when there is no code to draw: "unpaired", "removed" and
// "lapsed" (401/410 -- the script reloads the page, which says so in its own words), "inactive" and
// "paused" (409 -- the script shows the matching sentence and asks again every minute, so the code
// comes back by itself), "busy" or "unavailable" (429/5xx -- it tries again shortly).
type screenState struct {
	State string `json:"state"`
}

// code is GET /screen/code.
func (s *LiveScreen) code(w http.ResponseWriter, r *http.Request) {
	if !s.metered(w, r, true) {
		return
	}
	sess := s.cookies.Read(r)
	if sess.IsZero() {
		writeScreenJSON(w, http.StatusUnauthorized, screenState{State: "unpaired"})
		return
	}
	sc, err := s.screens.Resolve(r.Context(), sess)
	switch {
	case errors.Is(err, liveqr.ErrNoScreen):
		s.cookies.Clear(w)
		writeScreenJSON(w, http.StatusUnauthorized, screenState{State: "unpaired"})
		return
	case errors.Is(err, liveqr.ErrRevoked):
		s.log.Info("live QR screen: a removed screen asked for a code", "display_id", sc.ID)
		s.cookies.Clear(w)
		writeScreenJSON(w, http.StatusGone, screenState{State: "removed"})
		return
	case errors.Is(err, liveqr.ErrLapsed):
		s.log.Info("live QR screen: a screen whose pairing ran out asked for a code", "display_id", sc.ID)
		s.cookies.Clear(w)
		writeScreenJSON(w, http.StatusGone, screenState{State: "lapsed"})
		return
	case err != nil:
		s.log.Error("live QR screen: could not resolve the screen", "err", err)
		writeScreenJSON(w, http.StatusServiceUnavailable, screenState{State: "unavailable"})
		return
	}
	// A suspended business's screen and a switched-off venue's mint nothing (ORTA-2; ADR 0025's
	// exception list is not touched -- nothing here is a panel write).
	if pause := screenPause(sc); pause != "" {
		writeScreenJSON(w, http.StatusConflict, screenState{State: pause})
		return
	}
	tok, expires, err := s.screens.Token(sc)
	if err != nil {
		s.log.Error("live QR screen: could not mint the next code", "err", err, "display_id", sc.ID)
		writeScreenJSON(w, http.StatusInternalServerError, screenState{State: "unavailable"})
		return
	}
	qr, err := qrcode.Encode([]byte(liveqr.TapURL(s.base, tok)), liveScreenQRLevel)
	if err != nil {
		// qrcode's errors name lengths and levels, never content (its package doc).
		s.log.Error("live QR screen: could not draw the next code", "err", err, "display_id", sc.ID)
		writeScreenJSON(w, http.StatusInternalServerError, screenState{State: "unavailable"})
		return
	}
	life := time.Until(expires)
	writeScreenJSON(w, http.StatusOK, screenCode{
		Path:      qr.SVGPath(qrcode.QuietZone),
		Side:      qr.SVGSide(qrcode.QuietZone),
		ExpiresIn: int(life / time.Second),
		RefreshIn: int((life - liveScreenRefreshLead) / time.Second),
	})
}

func (s *LiveScreen) renderPairForm(w http.ResponseWriter, r *http.Request, status int, problem string) {
	s.render(w, r, status, pages.LiveScreenPair(pages.LiveScreenPairView{Problem: problem}), tapCSP)
}

func (s *LiveScreen) render(w http.ResponseWriter, r *http.Request, status int, c templ.Component, csp string) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", csp)
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		s.log.Error("live QR screen: rendering the page failed", "err", err)
	}
}

func writeScreenJSON(w http.ResponseWriter, status int, body any) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	w.WriteHeader(status)
	// A failed write is the screen gone; there is nobody left to answer.
	_ = json.NewEncoder(w).Encode(body)
}
