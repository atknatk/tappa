// Package operator is the platform operator's HTTP surface (M10; ADR 0020 §4, ADR 0021
// §3.6). OP-7 ships its two states and nothing else; the routes, the host gate and the
// screens are OP-8's.
//
// 🔴 IT IS NOT internal/handler, AND THE PACKAGE BOUNDARY IS THE POINT. ADR 0021 §3.6:
// the customer panel does not import the operator's packages. internal/handler is the
// customer panel; this package is a different one, so the rule is an import edge that
// does or does not exist rather than a convention inside one package
// (internal/handler's TestCustomerPanel_ImportsNoOperatorPackage pins the edge).
package operator

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/operatorauth"
)

// Prefix is the operator surface's path (ADR 0020 §4: /operator and everything under
// it).
const Prefix = "/operator"

// Surface is mounted on the router like every other feature (httpx.Mounter).
//
// ITS ZERO VALUE IS THE SAFE STATE: no Authenticator means "not configured", and an
// unconfigured surface answers 503 on every /operator path (Off). The configured state
// can only be reached through New, which refuses a nil Authenticator -- so a Surface
// that was declared and never filled cannot be mistaken for a working one. The third
// state, Unavailable, is also a 503: configured, but the operator's database could not
// be reached at start-up.
type Surface struct {
	// auth is the operator's Authenticator, built once by cmd/tappa and held here for
	// the life of the process (m10-platform.md, OP-4 block, OP-7 rule (3): the
	// *Authenticator is what is held; operatorauth.Config is never kept as a value).
	// OP-8 mounts the handlers that use it.
	auth *operatorauth.Authenticator
	// host is TAPPA_OPERATOR_HOST, validated by internal/config. OP-8's host gate
	// compares requests with it (ADR 0020 §4: any other host answers 404).
	host string
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

// New is the configured surface. It refuses the two values a configured surface cannot
// do without rather than degrade to Off silently.
func New(auth *operatorauth.Authenticator, host string) (*Surface, error) {
	if auth == nil {
		return nil, errors.New("operator: a configured surface needs its Authenticator")
	}
	if host == "" {
		return nil, errors.New("operator: a configured surface needs TAPPA_OPERATOR_HOST")
	}
	return &Surface{auth: auth, host: host}, nil
}

// Configured reports whether this surface was built by New.
func (s *Surface) Configured() bool { return s != nil && s.auth != nil }

// Mount registers the surface's routes.
//
// OFF OR UNAVAILABLE: Prefix and everything under it, every method, answer 503.
//
// CONFIGURED, IN OP-7: NOTHING IS REGISTERED, so the router's own 404 answers -- the
// same answer OP-8's host gate gives on any host but the operator's. The Authenticator
// exists and is held; no route reaches it until OP-8 mounts the sign-in, the session
// gate and the screens here. A 503 in this state would claim "not configured", which
// is false; a placeholder page would be a route OP-8 then has to remember to remove.
func (s *Surface) Mount(r chi.Router) {
	if s.Configured() {
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
// itself is logged once, at start-up, by cmd/tappa.
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
