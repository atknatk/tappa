package operator

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/operatorauth"
)

// mount registers the configured surface (M10 OP-8): the routes below, in one sub-router
// at Prefix, behind two middlewares --
//
//	hostGate        http.NotFound when httpx.OnHost(r, s.host) is false; it runs before
//	                the sub-router matches, so a wrong method on a registered path answers
//	                a foreign host 404, not 405
//	securityHeaders the four response headers listed at securityHeaders
//
// -- and then ADR 0020 §4's chain per group, in the ADR's order ("kimliksiz ret'ler
// çözümleyiciden önce (ucuz), oturum bütçesi kimlikten sonra"):
//
//	sign-in, TOTP step, enrollment     floodGate -> sameOriginGate(false)
//	the console (GET /operator), the   floodGate -> sameOriginGate(true) -> requireOperator -> sessionGate
//	legal texts (GET, POST
//	/operator/legal; OP-10), the
//	tenants (GET, POST
//	/operator/tenants, GET
//	/operator/tenants/{id}; OP-11),
//	a tenant's plaques (GET
//	/operator/tenants/{id}/plaques;
//	OP-13) and the audit log (GET,
//	POST /operator/audit; OP-14)
//	sign-out (POST /operator/logout)   sameOriginGate(false) -> requireOperator -> logoutGate
//
// The reads behind sessionGate -- the legal page, the tenant list and search, the overview,
// the plaques and the audit log -- each charge the read budget once more in their handler
// (spendRead).
//
// Sign-out is its own group, after the panel's measured lesson (adminlogin.go's sign-out
// group): no flood gate is in front of it, and a sign-out with a session cookie charges
// the flood budget without obeying it; its own ceiling (signOutLimit) refuses it. The
// invariant this keeps is the panel's WEAKER one, measured:
//
//	3 001 cookieless sign-outs from one address: no store call; the operator's sign-out
//	from that address afterwards revokes the session
//	(TestLogout_ACookielessFloodCostsNothingAndCannotRefuseIt)
//	3 000 cookie-bearing sign-outs from one address: the operator's sign-out from there
//	answers 429 and clears the session cookie, and the session is still live in the
//	store (TestLogout_PastTheCeilingTheBrowserStillForgetsTheSession; card limit L16)
//	500 cross-origin sign-outs from one address: 403, no store call; the operator's
//	sign-out afterwards revokes the session
//	(TestSameOriginGate_RefusalsWriteOneWarnPerWindowAtTheShippedLevel)
//
// The sign-out route runs no session predicate of its own: op_close_session applies the
// predicate in its statement.
func (s *Surface) mount(r chi.Router) {
	r.Route(Prefix, func(r chi.Router) {
		r.Use(s.hostGate, s.securityHeaders)

		r.With(s.sameOriginGate(false), s.requireOperator, s.logoutGate).Post("/logout", s.logout)

		r.Group(func(r chi.Router) {
			r.Use(s.floodGate, s.sameOriginGate(false))
			r.Get("/login", s.signInPage)
			r.Post("/login", s.signIn)
			r.Get("/login/totp", s.codePage)
			r.Post("/login/totp", s.code)
			r.Get("/enroll", s.enrollPage)
			r.Post("/enroll", s.enroll)
		})

		r.Group(func(r chi.Router) {
			r.Use(s.floodGate, s.sameOriginGate(true), s.requireOperator, s.sessionGate)
			r.Get("/", s.home)
			r.Get("/legal", s.legalPage)
			r.Post("/legal", s.publishLegal)
			r.Get("/tenants", s.tenantList)
			r.Post("/tenants", s.searchTenants)
			r.Get("/tenants/{id}", s.tenantOverview)
			r.Get("/tenants/{id}/plaques", s.tenantPlaques)
			r.Get("/audit", s.auditLog)
			r.Post("/audit", s.filterAudit)
		})
	})
}

// The redirect targets and the problem pages' ways back. The templates spell their form
// actions and links as literals; TestOperatorScreens_EveryActionAndLinkIsAMountedRoute
// holds the links of the renders screens() makes to the routes mount registers.
const (
	pathConsole = Prefix
	pathSignIn  = Prefix + "/login"
	pathCode    = Prefix + "/login/totp"
	pathLegal   = Prefix + "/legal"
	pathTenants = Prefix + "/tenants"
	pathAudit   = Prefix + "/audit"
)

// hostGate is the OPERATOR half of ADR 0020 §4's two-way host gate: when
// httpx.OnHost(r, s.host) is false it answers http.NotFound -- the handler the router uses
// for a path it does not know (ADR 0020 §4: "ana host'ta operatör yüzeyi yokmuş gibi
// görünür"). Measured against the router's own 404 (status, body, Content-Type, nosniff;
// no CSP, Location or cookie) on the hosts the shipped ingress names plus six more, under
// the methods and operator paths that test lists (a tenant's plaques among them since
// OP-13, the audit log since OP-14): TestHostGate_OperatorRoutesAnswerTheRoutersOwn404OnEveryOtherHost.
func (s *Surface) hostGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !httpx.OnHost(r, s.host) {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// securityHeaders sets four response headers before the handler runs:
//
//	Content-Security-Policy  operatorCSP (render.go); renderEnroll sets enrollCSP instead
//	Cache-Control: no-store  an operator response carries a session, a challenge, a TOTP
//	                         key or a refusal about one
//	X-Content-Type-Options   nosniff
//	Referrer-Policy          no-referrer: the enrollment page's URL carries the account id
//	                         (the document head's meta tag says the same for navigations)
//
// Measured: TestOperatorHeaders_FortyResponseClassesCarryThePolicy (C1-C40),
// TestOperatorHeaders_TheWrongMethodAndOversizedClassesCarryThePolicy (C41-C48),
// TestOperatorHeaders_TheLegalClassesCarryThePolicy (OP-10, C49-C66),
// TestOperatorHeaders_TheTenantClassesCarryThePolicy (OP-11, C67-C93),
// TestOperatorHeaders_ThePlaqueClassesCarryThePolicy (OP-13, C94-C103) and
// TestOperatorHeaders_TheAuditClassesCarryThePolicy (OP-14, C104 on) drive the response
// classes of the tests' classRoutes with hostile request headers and hold the response
// headers AT WriteHeader (the recorder's snapshot) to the designed names and values;
// TestOperatorHeaders_TheRecorderSnapshotIsWhatTheWireCarries measures that snapshot
// equal to the header section a real server sends for three classes (C1, C18, C28),
// Content-Length and Date aside; trailers are not compared. The tests' headers list the
// hostile headers and the designed values.
func (s *Surface) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", operatorCSP)
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// rateKey is the client's budget key: httpx's resolution of the real address (RealIP,
// bounded by TAPPA_TRUSTED_PROXIES), bucketed as httpx.RateKey buckets it. This package
// uses it as a budget key; it passes it to the budgets and to operatorauth's Password and
// CompleteEnrollment, which take it as one.
//
// Measured: the address (G15) is on none of the leak test's four surfaces (S1-S4) on its
// 30 arms (A1-A30) -- TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor. Pinned:
// TestClientAddress_TheListedReadsFeedOnlyTheBudgets catches the list in its header. The
// access record carries no address (requestlog.go); attribution of a flood is the ingress
// log's.
func rateKey(r *http.Request) string { return httpx.RateKey(httpx.ClientIP(r)) }

// floodGate is operatorauth's per-address flood budget (ADR 0020 §4's floodGate, OP-6's
// AllowRequest): a request in the sign-in and console groups charges it, and a request
// past it is answered 429 here. Measured on the console:
// TestFloodGate_AnExhaustedAddressReachesNoConsolePredicate (an exhausted address with a
// live session cookie gets 429 and makes 0 store calls). It writes no log line; the access
// record of the 429 is the trace. Sign-out is not behind it (mount).
func (s *Surface) floodGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.auth.AllowRequest(rateKey(r)) {
			s.problem(w, r, http.StatusTooManyRequests, problemTooMany(false))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// logoutGate is the budget of a sign-out that carries a session cookie (requireOperator,
// in front of it, has answered the cookieless ones): it charges the flood budget without
// obeying it, and refuses past sign-out's own ceiling (signOutLimit) with 429 -- clearing
// the session cookie in the browser (mount states what that leaves; card limit L16).
func (s *Surface) logoutGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := rateKey(r)
		_ = s.auth.AllowRequest(key) // metered, deliberately not obeyed (above)
		if n := s.signOuts.Charge(key); n > signOutLimit {
			if s.signOuts.FirstOverLimit(n) {
				s.log.WarnContext(r.Context(), "operator sign-out ceiling reached",
					"limit", signOutLimit, "period", signOutPeriod.String())
			}
			operatorauth.ClearSessionCookie(w)
			s.problem(w, r, http.StatusTooManyRequests, problemSignOutThrottled)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sameOriginGate refuses, with 403, an unsafe-method request that does not come from the
// operator origin, before the handler reads the form or calls the store (ADR 0020 §4). A
// refusal charges no budget of its own and calls no store method and no bcrypt
// (TestSameOriginGate_ACrossOriginPostReachesNoStore, TestSurface_ACrossOriginPostPaysNothing).
//
// ITS LOG (4th round, B6): the first refusal of a 10-minute window, process-wide, is one
// WARN record (the method; no address); the others are Debug records. Measured at Info
// (production's level, deploy/k8s/05-config.yaml): 801 refusals from two addresses, one
// WARN record (TestSameOriginGate_RefusalsWriteOneWarnPerWindowAtTheShippedLevel). On the sign-in and
// console groups the flood gate is in front of it; on sign-out it is the first link (mount).
//
// UNSAFE METHODS (all but GET, HEAD, OPTIONS): the Origin header must equal the operator
// origin (case aside). With no Origin, or "null", Sec-Fetch-Site must be same-origin --
// not same-site, which the panel's fallback accepts: taptime.mt and ops.taptime.mt are one
// site. With neither, refused. A browser takes the fallback here: the pages' referrer
// policy is no-referrer, and the OP-8 auditor measured in headless Chrome (2026-10-02) that
// the operator's form POSTs arrived as `Origin: null` + `Sec-Fetch-Site: same-origin`.
// TestSameOriginGate_ACrossOriginPostReachesNoStore drives both branches, accepted and
// refused.
//
// SAFE METHODS, when guardReads is set (the console): a request whose Sec-Fetch-Site is
// same-site or cross-site gets the sign-in redirect and no store call, so a page on a
// sibling host does not make the session predicate run by embedding the console's URL in
// a browser that sends fetch metadata (TestSessionGate_ASameSiteReadDoesNotTouchTheSession;
// a browser that sends none is the card's counted limit L5). A request without fetch
// metadata passes.
//
// The headers are a client's to set; the budgets bound what a non-browser client can make
// the database do.
func (s *Surface) sameOriginGate(guardReads bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if safeMethod(r.Method) {
				if guardReads {
					switch strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))) {
					case "same-site", "cross-site":
						s.redirect(w, pathSignIn)
						return
					}
				}
				next.ServeHTTP(w, r)
				return
			}
			if !s.sameOrigin(r) {
				// The window's first refusal is a WARN, the others Debug (the doc
				// comment above).
				if s.originRefusals.FirstOverLimit(s.originRefusals.Charge(originRefusalKey)) {
					s.log.WarnContext(r.Context(), "operator request refused: not from the operator origin",
						"method", r.Method, "first_in_window", originRefusalPeriod.String())
				} else {
					s.log.DebugContext(r.Context(), "operator request refused: not from the operator origin",
						"method", r.Method)
				}
				s.problem(w, r, http.StatusForbidden, problemCrossOrigin)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// sameOrigin is the unsafe-method rule above. A browser serialises Origin as
// scheme://host[:port], lower case, default port omitted -- the form operatorOrigin builds.
func (s *Surface) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" || origin == "null" {
		return strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")), "same-origin")
	}
	return strings.EqualFold(origin, s.origin)
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// identityKey carries the resolved operator session through the request; tokenKey
// carries the session cookie's token from requireOperator to sessionGate.
type (
	identityKey struct{}
	tokenKey    struct{}
)

// sessionTokenOf is the session token requireOperator lifted, for the links behind it.
func sessionTokenOf(r *http.Request) (operatorauth.SessionToken, bool) {
	tok, ok := r.Context().Value(tokenKey{}).(operatorauth.SessionToken)
	return tok, ok
}

// operatorOf is the session sessionGate resolved, for the handlers behind it.
func operatorOf(r *http.Request) (operatorauth.Identity, bool) {
	id, ok := r.Context().Value(identityKey{}).(operatorauth.Identity)
	return id, ok
}

// storeSession is the session sessionGate resolved and its hash for the store -- the
// argument of every op_* call a console screen makes (the legal texts, OP-10; the
// tenants, OP-11; the plaques, OP-13; the audit log, OP-14). Through mount both are in
// place; a route mounted outside the chain by mistake answers the sign-in.
func (s *Surface) storeSession(w http.ResponseWriter, r *http.Request) (operatorauth.Identity, string, bool) {
	id, ok := operatorOf(r)
	tok, hasToken := sessionTokenOf(r)
	if !ok || !hasToken {
		s.redirect(w, pathSignIn)
		return operatorauth.Identity{}, "", false
	}
	hash, err := s.auth.SessionHash(tok)
	if err != nil {
		s.redirect(w, pathSignIn)
		return operatorauth.Identity{}, "", false
	}
	return id, hash, true
}

// requireOperator is the cheap half of the identity check: a request without an operator
// session cookie (or with an empty one) gets the sign-in redirect and no store call. The
// token it read is handed on through the request context (sessionTokenOf); sessionGate and
// the sign-out handler take it from there and redirect when it is missing
// (TestSessionGate_NoLiveSessionIsASignInRedirect's live control, the sign-out tests).
// TestSessionCookieReads_TheListedFormsOccurOnlyInRequireOperator catches the list in its
// header.
func (s *Surface) requireOperator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, err := operatorauth.ReadSessionCookie(r)
		if err != nil {
			s.redirect(w, pathSignIn)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), tokenKey{}, tok)))
	})
}

// sessionGate runs the session predicate (Authenticator.Verify -> op_touch_session: 8 h
// absolute, 30 min idle, MFA stamped, not revoked, operator active, by the database's
// clock, last_used_at advanced in the same statement) and then the per-session budget.
//
// A refused session -- a junk cookie included -- gets the sign-in redirect, and the cookie
// is cleared. A DATABASE FAILURE gets the same redirect and a different log line (the
// panel's requireLogin), and the cookie is kept, because the session may be alive.
func (s *Surface) sessionGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, ok := sessionTokenOf(r)
		if !ok {
			// Mounted without requireOperator in front: fail closed.
			s.redirect(w, pathSignIn)
			return
		}
		id, err := s.auth.Verify(r.Context(), tok)
		switch {
		case errors.Is(err, operatorauth.ErrNoSession):
			operatorauth.ClearSessionCookie(w)
			s.redirect(w, pathSignIn)
			return
		case err != nil:
			s.log.ErrorContext(r.Context(), "operator: could not check the operator session", "err", err)
			s.redirect(w, pathSignIn)
			return
		}
		if !s.spendSession(w, r, id) {
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
	})
}

// spendSession charges one unit of the session's request budget (sessionLimit,
// surface.go) and, past it, answers 429 -- the window's first refusal logged at WARN with
// the session's id. sessionGate calls it once for every request past the predicate; a
// read's second unit is spendRead's, below.
func (s *Surface) spendSession(w http.ResponseWriter, r *http.Request, id operatorauth.Identity) bool {
	if n := s.sessions.Charge(id.SessionID.String()); n > sessionLimit {
		if s.sessions.FirstOverLimit(n) {
			s.log.WarnContext(r.Context(), "operator session budget reached",
				"session_id", id.SessionID.String(), "limit", sessionLimit, "period", sessionPeriod.String())
		}
		s.problem(w, r, http.StatusTooManyRequests, problemTooMany(true))
		return false
	}
	return true
}

// spendRead charges one unit of the session's READ budget (readLimit, surface.go) and,
// past it, answers 429 with the same page -- the window's first refusal logged at WARN
// with the session's id. The read handlers call it once, after their own refusals and
// before the store (legalPage; tenants.go's listTenants and tenantOverview; plaques.go's
// tenantPlaques; audit.go's readAudit). The count Charge returns is the one the refusal is
// decided on: there is no separate read of the budget to race (surface.go, readLimit).
func (s *Surface) spendRead(w http.ResponseWriter, r *http.Request, id operatorauth.Identity) bool {
	if n := s.reads.Charge(id.SessionID.String()); n > readLimit {
		if s.reads.FirstOverLimit(n) {
			s.log.WarnContext(r.Context(), "operator read budget reached",
				"session_id", id.SessionID.String(), "limit", readLimit, "period", readPeriod.String())
		}
		s.problem(w, r, http.StatusTooManyRequests, problemTooMany(true))
		return false
	}
	return true
}
