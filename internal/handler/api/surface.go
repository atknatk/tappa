// Package api is the integration API's /v1 surface (M11 API-4; ADR 0028 §3, §8, §10, §13): its own
// host, the Bearer chain, the JSON error contract, the body bound, Idempotency-Key, the suspension
// checkpoint and the hand-written OpenAPI document. The endpoints mount into the authenticated group
// Mount builds: PUT /v1/locations/{ref} (API-5, locations.go) and the employee endpoints (API-6,
// employees.go), GET /v1/punches (API-7, punches.go) and GET /v1/directory (API-8, directory.go);
// beside them, without a key, the OpenAPI document and the integration guide (API-12, guide.go).
//
// 🔴 IT IMPORTS NO PANEL. Like internal/handler/operator, this package is a surface of its own:
// it does not import internal/handler (the customer panel, the tap) and internal/handler does not
// import it (TestPackage_ImportsNoPanelAndNoPanelImportsIt). It sets no cookie and reads none -- the
// API host carries no cookie at all (the panel's and the tap's are host-bound), so there is no
// CSRF surface here (§13).
//
// 🔴 IT LOGS NO CREDENTIAL. The Authorization header is read once, by the chain, and deleted from
// the request before anything after the chain runs; the key travels as apikey.Token and its hash as
// apikey.Checked, both of which print a placeholder. A log line here carries a tenant or key id and
// an error at most (TestChain_NothingOfTheKeyReachesALogOrAResponse).
package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/apikey"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
)

// Prefix is the surface's path: httpx.APIPrefix, the one the router's half of the host gate reads.
const Prefix = httpx.APIPrefix

// The buckets (ADR 0028 §3). Both are in-memory, like every bucket of this single-replica product
// (httpx.Limiter). The ADDRESS bucket sits between the shape check and the resolver, so a flood of
// well-shaped junk keys costs the database at most addressLimit lookups per period per address; it
// also meters the unauthenticated openapi.yaml. The KEY bucket sits after the resolver: one leaked
// key, from however many addresses, is bounded too. An integration that synchronises a few thousand
// employees at the key bucket's rate takes minutes, which is the intended order of magnitude.
const (
	addressLimit  = 1200
	addressPeriod = time.Minute
	keyLimit      = 600
	keyPeriod     = time.Minute
)

// Keychain is the Bearer chain's blocks (*apikey.Authenticator), declared at the consumer (§7).
type Keychain interface {
	Check(t apikey.Token) (apikey.Checked, error)
	Resolve(ctx context.Context, c apikey.Checked) (apikey.Principal, error)
	Touch(ctx context.Context, p apikey.Principal) (bool, error)
}

// Writer is the write runner (*apikey.Writes).
type Writer interface {
	Run(ctx context.Context, req apikey.WriteRequest, work apikey.WorkFunc) (apikey.Outcome, bool, error)
}

// WorkNoticer is K-19's set of "tenants with work" (internal/webhook's worker): an authenticated
// request puts the key's tenant into it (ADR 0028 §6). It must not block.
type WorkNoticer interface {
	Notice(tenant uuid.UUID)
}

// Config is what New needs. Every field but the limits is required.
type Config struct {
	// Host is TAPPA_API_HOST: /v1 answers on it alone.
	Host       string
	Keys       Keychain
	Writes     Writer
	Suspension Suspension
	Work       WorkNoticer
	// Locations is the venue domain (*tenant.Venues -- the panel's own) behind PUT /v1/locations.
	Locations Locations
	// Employees is the employee domain (*tenant.Staff -- the panel's own) behind /v1/employees.
	Employees Employees
	// Punches is the punch feed's reader (*tenant.PunchFeed) behind GET /v1/punches.
	Punches Punches
	// Directory is the directory's reader (*tenant.EmployeeDirectory) behind GET /v1/directory.
	Directory Directory
	// DeactivationBudget is K-25's hourly budget per key; zero means tenant.DefaultDeactivationBudget
	// (20). A test lowers it.
	DeactivationBudget int
	Log                *slog.Logger
	// The buckets; zero means the shipped value. A test shrinks them.
	AddressLimit, KeyLimit   int
	AddressPeriod, KeyPeriod time.Duration
}

// Surface is the /v1 surface.
type Surface struct {
	host       string
	keys       Keychain
	writes     Writer
	suspension Suspension
	work       WorkNoticer
	locations  Locations
	employees  Employees
	punches    Punches
	directory  Directory
	budget     int
	byAddress  *httpx.Limiter
	byKey      *httpx.Limiter
	log        *slog.Logger
	// testRoutes are routes a test mounts into the authenticated group (export_test.go). nil in
	// production: the shipped surface has no route a test invented.
	testRoutes func(r chi.Router)
}

// New builds the surface. A missing dependency or an empty host is refused: an API that answered
// without its chain, its runner or its checkpoint would be worse than none.
func New(c Config) (*Surface, error) {
	switch {
	case c.Host == "":
		return nil, errors.New("api: no host (TAPPA_API_HOST); without one there is no API to build")
	case c.Keys == nil:
		return nil, errors.New("api: nil keychain")
	case c.Writes == nil:
		return nil, errors.New("api: nil write runner")
	case c.Suspension == nil:
		return nil, errors.New("api: nil suspension checkpoint")
	case c.Work == nil:
		return nil, errors.New("api: nil work set")
	case c.Locations == nil:
		return nil, errors.New("api: nil venue domain")
	case c.Employees == nil:
		return nil, errors.New("api: nil employee domain")
	case c.Punches == nil:
		return nil, errors.New("api: nil punch feed")
	case c.Directory == nil:
		return nil, errors.New("api: nil directory")
	}
	log := c.Log
	if log == nil {
		log = slog.Default()
	}
	pick := func(v, def int) int {
		if v <= 0 {
			return def
		}
		return v
	}
	pickd := func(v, def time.Duration) time.Duration {
		if v <= 0 {
			return def
		}
		return v
	}
	return &Surface{
		host: c.Host, keys: c.Keys, writes: c.Writes, suspension: c.Suspension, work: c.Work, locations: c.Locations,
		employees: c.Employees, punches: c.Punches, directory: c.Directory, budget: pick(c.DeactivationBudget, tenant.DefaultDeactivationBudget), log: log,
		byAddress: httpx.NewLimiter(pick(c.AddressLimit, addressLimit), pickd(c.AddressPeriod, addressPeriod)),
		byKey:     httpx.NewLimiter(pick(c.KeyLimit, keyLimit), pickd(c.KeyPeriod, keyPeriod)),
	}, nil
}

// Mount registers /v1. Every request passes, in order: the host gate (404 off the API host), the
// response headers, the body bound, the guard (a panic or a missed deadline is 503); then either
// the address bucket alone (the public documents: openapi.yaml and the two guides) or the whole chain
// and then the write's query rule (everything else: writesTakeNoQuery refuses a query string on every
// route that is not a GET). Unknown paths and methods answer the JSON 404.
func (s *Surface) Mount(r chi.Router) {
	r.Route(Prefix, func(r chi.Router) {
		r.Use(s.hostGate, s.headers, s.bodyBound, s.guard)
		r.NotFound(s.notFound)
		r.MethodNotAllowed(s.notFound)
		r.Group(func(r chi.Router) {
			r.Use(s.addressOnly)
			r.Get(openAPIPath, s.openAPI)
			r.Get(guidePath, s.serveGuide(guideEnglish))
			r.Get(guideTRPath, s.serveGuide(guideTurkish))
		})
		r.Group(func(r chi.Router) {
			r.Use(s.authenticate, s.writesTakeNoQuery)
			r.Put(locationPath, s.putLocation)
			r.Get(employeesPath, s.listEmployees)
			r.Get(employeePath, s.getEmployee)
			r.Put(employeePath, s.putEmployee)
			r.Delete(employeePath, s.deleteEmployee)
			r.Post(employeeResend, s.resendActivation)
			r.Get(punchesPath, s.listPunches)
			r.Get(directoryPath, s.listDirectory)
			if s.testRoutes != nil {
				s.testRoutes(r)
			}
		})
	})
}

// hostGate is the /v1 half of the two-way host gate (ADR 0028 §13; httpx/apihost.go has the other):
// off the API host, /v1 is http.NotFound -- the router's own 404, the same bytes as any path no
// feature registered, so the main host does not even reveal that an API exists.
func (s *Surface) hostGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !httpx.OnHost(r, s.host) {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// guard keeps /v1's two failures that no handler answers inside §8 (API-4 review, both lenses):
//
//	a panic     the router's Recoverer answers 500 with no body and no Content-Type; here the panic
//	            is logged WITH its stack (the request's context -- its request id; the Authorization
//	            header is already deleted by the chain, and a stack carries no header values) and
//	            answered 503 unavailable. http.ErrAbortHandler is re-raised: it is net/http's own
//	            "drop this connection", not a fault.
//	a deadline  the router's middleware.Timeout writes 504 with no body when a handler returns having
//	            written nothing after the request's deadline; here such a request is answered 503
//	            unavailable first, so Timeout's 504 is a superfluous WriteHeader.
//
// It is mounted AFTER bodyBound so http.MaxBytesReader is handed net/http's own writer (it signals a
// too-large body to the server through an unexported method a wrapper cannot carry).
//
// Either way, nothing is written when the handler already wrote a status: a second status line is
// impossible, and the access record keeps what the client got (TestGuard_PanicAndDeadlineAreSection8).
func (s *Surface) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gw := &guardWriter{ResponseWriter: w}
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.log.ErrorContext(r.Context(), "api: a handler panicked", "panic", fmt.Sprint(v), "stack", string(debug.Stack()))
				if !gw.wrote {
					writeProblem(gw, problemUnavailable)
				}
				return
			}
			if !gw.wrote && r.Context().Err() != nil {
				writeProblem(gw, problemUnavailable)
			}
		}()
		next.ServeHTTP(gw, r)
	})
}

// guardWriter records whether a status was written.
type guardWriter struct {
	http.ResponseWriter
	wrote bool
}

func (g *guardWriter) WriteHeader(code int) {
	g.wrote = true
	g.ResponseWriter.WriteHeader(code)
}

func (g *guardWriter) Write(b []byte) (int, error) {
	g.wrote = true
	return g.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (g *guardWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }

// headers sets what every /v1 response carries: no caching (a response names a tenant's data), no
// sniffing, no referrer.
func (s *Surface) headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (s *Surface) notFound(w http.ResponseWriter, _ *http.Request) { writeProblem(w, problemNotFound) }

// addressOnly meters a request by its client address (httpx.ClientIP -- the trusted-proxy rule of
// realip.go) and nothing else.
func (s *Surface) addressOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.chargeAddress(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Surface) chargeAddress(w http.ResponseWriter, r *http.Request) bool {
	if n := s.byAddress.Charge(httpx.RateKey(httpx.ClientIP(r))); n > s.byAddress.Limit() {
		writeProblem(w, rateLimited(s.byAddress.Period()))
		return false
	}
	return true
}

// unavailable answers 503 and logs the cause -- with the request's context (its request id), never
// with anything of the credential.
func (s *Surface) unavailable(w http.ResponseWriter, r *http.Request, what string, err error) {
	s.log.ErrorContext(r.Context(), "api: "+what, "err", err)
	writeProblem(w, problemUnavailable)
}
