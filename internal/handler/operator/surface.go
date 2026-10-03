// Package operator is the platform operator's HTTP surface (M10; ADR 0020 §4, ADR 0021
// §3.6): the sign-in with its TOTP step, the enrollment, the sign-out and the console's
// front page (OP-8), Taptime's legal texts (OP-10, legal.go), on the operator's own
// host, and the two 503 states OP-7 shipped.
//
// 🔴 IT IS NOT internal/handler, AND THE PACKAGE BOUNDARY IS THE POINT. ADR 0021 §3.6:
// the customer panel does not import the operator's packages. internal/handler is the
// customer panel; this package is a different one, so the rule is an import edge that
// does or does not exist rather than a convention inside one package
// (internal/handler's TestCustomerPanel_ImportsNoOperatorPackage pins the edge).
//
// WHAT IT SERVES. The operator's database role has five definers in 00026 --
// op_touch_session, op_record_auth_event, op_open_session, op_complete_enrollment,
// op_close_session -- and three in 00027 -- op_begin_read, op_read_legal_versions,
// op_publish_legal; none of the eight reads a tenant (read from the two migrations). So
// the routes are the sign-in,
// the TOTP step, the enrollment, the sign-out, /operator itself and /operator/legal
// (OP-10). ADR 0020 §4's /operator/tenants/{id}, /billing, /plaques and /audit are not
// registered (TestSurface_TheScreensOfLaterTasksAreNotMounted), and the 14 renders of
// screens() do not link to them (TestOperatorScreens_EveryActionAndLinkIsAMountedRoute);
// they arrive with the definers that can serve them (OP-11, OP-14).
package operator

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/operatorauth"
)

// Prefix is the operator surface's path (ADR 0020 §4: /operator and everything under
// it). It IS internal/httpx's OperatorPrefix -- one definition, because the customer
// half of the host gate (httpx) and this surface must agree on which paths are the
// operator's (TestSurface_ThePrefixIsTheRoutersPrefix).
const Prefix = httpx.OperatorPrefix

// Surface is mounted on the router like every other feature (httpx.Mounter).
//
// ITS ZERO VALUE IS THE SAFE STATE: no Authenticator means "not configured", and an
// unconfigured surface answers 503 on every /operator path (Off). The configured state
// is reached through New, which refuses what it cannot do without -- so a Surface that
// was declared and not filled is not mistaken for a working one. The
// third state, Unavailable, is also a 503: configured, but the operator's database could
// not be reached at start-up.
type Surface struct {
	// auth is the operator's Authenticator, built once by cmd/tappa and held here for
	// the life of the process (m10-platform.md, OP-4 block, OP-7 rule (3): the
	// *Authenticator is what is held; operatorauth.Config is never kept as a value).
	auth *operatorauth.Authenticator
	// legalStore and texts are the legal screen's (legal.go): the operator database's
	// version list and publication, and the public pages' snapshot with its refresh.
	legalStore LegalStore
	texts      LegalTexts
	// host is TAPPA_OPERATOR_HOST, validated by internal/config: the host gate's
	// comparison (httpx.OnHost).
	host string
	// origin is the operator host's web origin -- the scheme and port of
	// TAPPA_BASE_URL with the operator host in it (operatorOrigin). sameOriginGate
	// compares a request's Origin with it, and the enrollment page's script-src names
	// a path under it.
	origin string
	log    *slog.Logger
	// sessions is the per-session budget (sessionGate) and signOuts sign-out's own
	// per-address ceiling (logoutGate). Both are httpx.Limiter, the panel's primitive;
	// the numbers are below Mount.
	sessions *httpx.Limiter
	signOuts *httpx.Limiter
	// originRefusals counts sameOriginGate's refusals under ONE process-wide key, so
	// the first of each window is logged at WARN and the rest at Debug.
	originRefusals *httpx.Limiter
	// unavailable distinguishes Unavailable from Off: both answer 503, and the body
	// must not say "not configured" about a surface that is.
	unavailable bool
}

// Off is the surface of a deployment with no operator configuration: every request
// under Prefix answers 503 with a named reason (ADR 0020 §4, ADR 0021 §4), and nothing
// else on the router changes.
func Off() *Surface { return &Surface{} }

// Unavailable is the surface of a deployment whose operator configuration is complete
// but whose operator database could not be reached at start-up (cmd/tappa logs why):
// /operator answers 503 as Off does, with a body that does not claim "not configured",
// and the customer product serves as usual. The process does not retry; a restart
// after the cause is fixed is what brings the surface up (deploy/README.md, the
// operator surface runbook).
func Unavailable() *Surface { return &Surface{unavailable: true} }

// New is the configured surface. It refuses the values a configured surface cannot do
// without rather than degrade to Off silently: the Authenticator, the legal screen's
// store and texts, the operator host, a base URL it can take the operator origin's
// scheme and port from, and a logger.
func New(auth *operatorauth.Authenticator, legalStore LegalStore, texts LegalTexts, host, baseURL string, log *slog.Logger) (*Surface, error) {
	if auth == nil {
		return nil, errors.New("operator: a configured surface needs its Authenticator")
	}
	if legalStore == nil {
		return nil, errors.New("operator: a configured surface needs its legal store (the operator database)")
	}
	if texts == nil {
		return nil, errors.New("operator: a configured surface needs the legal texts' snapshot")
	}
	if host == "" {
		return nil, errors.New("operator: a configured surface needs TAPPA_OPERATOR_HOST")
	}
	if log == nil {
		return nil, errors.New("operator: a configured surface needs a logger")
	}
	origin, err := operatorOrigin(baseURL, host)
	if err != nil {
		return nil, err
	}
	return &Surface{
		auth:       auth,
		legalStore: legalStore,
		texts:      texts,
		host:       host,
		origin:     origin,
		log:        log,
		sessions:   httpx.NewLimiter(sessionLimit, sessionPeriod),
		signOuts:   httpx.NewLimiter(signOutLimit, signOutPeriod),
		// limit 0: the window's first refusal is the first over the limit.
		originRefusals: httpx.NewLimiter(0, originRefusalPeriod),
	}, nil
}

// operatorOrigin is the operator host's web origin: TAPPA_BASE_URL's scheme and port
// with the operator host in place of its host -- https://ops.taptime.mt in production
// (base https://taptime.mt), http://ops.localhost:8080 in development (base
// http://localhost:8080).
//
// WHY DERIVED AND NOT A FIFTH VARIABLE. The operator host is served by the SAME process
// on the SAME listener behind the SAME ingress as the customer product, so its scheme
// and port are the customer product's; a separate origin variable would be one more
// value that can disagree with the deployment it describes. A default port (443 for
// https, 80 for http) is dropped: a browser serialises Origin without it.
//
// The base URL must parse with an http or https scheme -- config.Load does not check
// its shape (the panel's sameOrigin carries that note), so the check is here, at
// start-up, where a bad value refuses the boot.
func operatorOrigin(baseURL, host string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("operator: TAPPA_BASE_URL must be an http or https URL; the operator origin takes its " +
			"scheme and port (the value is not repeated here)")
	}
	port := u.Port()
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		port = ""
	}
	origin := u.Scheme + "://" + host
	if port != "" {
		origin += ":" + port
	}
	return origin, nil
}

// Configured reports whether this surface was built by New.
func (s *Surface) Configured() bool { return s != nil && s.auth != nil }

// Mount registers the surface's routes.
//
// OFF OR UNAVAILABLE: Prefix and the paths under it answer 503 -- OP-7's two states;
// measured on the methods, paths and hosts TestSurface_OffAnswers503UnderThePrefixAndNowhereElse
// and TestHostGate_TheUnavailableSurfaceKeepsItsAnswerOnEveryHost drive.
//
// CONFIGURED: the operator routes, each behind the chain ADR 0020 §4 orders (routes.go).
func (s *Surface) Mount(r chi.Router) {
	if s.Configured() {
		s.mount(r)
		return
	}
	body := offBody
	if s != nil && s.unavailable {
		body = unavailableBody
	}
	h := serviceUnavailable(body)
	r.Handle(Prefix, h)
	r.Handle(Prefix+"/*", h)
}

// The two 503 bodies name the fault and no value: the unconfigured state has none to
// name, and the unavailable one keeps its cause in the process log, which is the
// operator's channel -- not a page any stranger can fetch.
const (
	offBody = "503 operator surface not configured: this deployment sets none of the TAPPA_OPERATOR_* " +
		"variables, so there is no operator sign-in here. The customer product is unaffected.\n"
	unavailableBody = "503 operator surface unavailable: it is configured, but its database could not be " +
		"reached when this process started. The customer product is unaffected.\n"
)

// serviceUnavailable answers 503 with body. no-store, because a cached 503 would
// outlive the state that caused it; nosniff, because the body is text and must stay
// text.
//
// The 503 is DECLARED designed (httpx.AnswerAsDesigned, 2c): it is this state answering
// as built, not the server failing, so the access log writes no ERROR record for it and
// deploy/README.md's 5xx rule cannot be paged by anyone fetching /operator. The state
// itself is logged once, at start-up, by cmd/tappa. (The CONFIGURED surface's 503s --
// a database fault during a sign-in -- are not declared and are recorded as usual.)
func serviceUnavailable(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpx.AnswerAsDesigned(r, http.StatusServiceUnavailable)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(body))
	})
}

// THE SURFACE'S OWN BUDGETS. The flood, work, account and enrollment budgets are
// operatorauth's (limits.go, OP-6); these two are the HTTP surface's, on the panel's
// primitive (httpx.Limiter), sized for ADR 0020's population -- one to three people --
// and the window of sameOriginGate's log line.
const (
	// sessionLimit: units per operator SESSION past the session predicate -- ADR 0020
	// §4's "oturum bütçesi kimlikten sonra (anahtarı oturumdur)", the panel's
	// sessionGate. Keyed on the session's id, so spending it takes that session's
	// cookie. sessionGate charges one unit per request after the predicate, so it bounds
	// the requests one session gets past the gate to a handler -- not the predicate's
	// database work. Measured: one session, 101 console requests from 101 addresses ->
	// 100 x 200, 1 x 429 and 101 predicate calls (TestSessionGate_ABudgetPerSession).
	// The predicate's work per address is bounded by the flood gate in front: an
	// exhausted address with a live cookie gets 429 and 0 store calls
	// (TestFloodGate_AnExhaustedAddressReachesNoConsolePredicate).
	//
	// RE-DERIVED IN OP-10 (the OP-8 note asked the first screen with a read to): a
	// read costs two definer transactions (op_begin_read, op_read_*), and the read's
	// handler charges a second unit (legalPage), so a legal page view is two units; a
	// publication is one (op_publish_legal, then the snapshot's refresh on the
	// customer pool); the console page is one. Measured: one session, 50 legal page
	// views -> 50 x 200, the 51st -> 429 at the gate, 50 reads
	// (TestLegalPage_AReadCountsTwiceAgainstTheSessionBudget).
	//
	//	one operator x (~10 legal page views x 2 + ~5 console views
	//	                + a few publications)                     ~30 per window
	//	x ~3 headroom                                             100
	//
	// OP-11's tenant screens are reads too; the derivation asks each to charge its second
	// unit the same way (spendSession).
	sessionLimit  = 100
	sessionPeriod = 10 * time.Minute

	// signOutLimit: the per-address ceiling of a sign-out that carries a session cookie
	// (requireOperator answers a cookieless one first). The panel measured why sign-out
	// does not share the flood budget (internal/handler/adminlogin.go, the sign-out
	// group): a third party sharing the operator's rate key (one NAT, one IPv6 /64) who
	// spent the shared budget would make the operator's own sign-out answer 429. So
	// sign-out charges the flood budget without being refused by it, and logoutGate
	// refuses past ten times that number -- the panel's ratio and the panel's weaker
	// invariant: a third party who sends this many cookie-bearing sign-outs in a window
	// makes the operator's answer 429 (routes.go, mount;
	// TestLogout_PastTheCeilingTheBrowserStillForgetsTheSession).
	//
	//	floodLimit (operatorauth, 300 per 10 min) x 10            3000
	//
	// COST: a cookie-bearing sign-out is one definer call (op_close_session); 3000 per
	// window per address bounds it.
	signOutLimit  = 3000
	signOutPeriod = 10 * time.Minute

	// originRefusalPeriod is the window of sameOriginGate's WARN record (routes.go):
	// the window's first refusal is the WARN. The key is a constant, so the window is
	// process-wide.
	originRefusalPeriod = 10 * time.Minute
	originRefusalKey    = "origin"
)
