package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/oauth"
)

// THE SCOPES (issue #37, ADR 0047 §8.2, §8.3): every authenticated /v1 route is bound to the OAuth
// scope it needs, here and nowhere else. An API key passes every route (Full -- ADR 0028's behaviour,
// unchanged: a key never gets insufficient_scope). An OAuth access token passes a route only when ITS
// OWN scope set (oauth_tokens.scopes -- a refresh may have narrowed it below the grant's) holds the
// route's scope; there is no implied scope (employees:write does not hold employees:read).
//
// 🔴 CLOSED BY DEFAULT. A route the table does not name -- the sandbox build's POST /v1/sandbox/taps,
// a test's route, a route somebody adds to Mount and forgets here -- answers an OAuth token 403
// insufficient_scope, never 2xx. TestScopes_EveryAuthenticatedRouteIsInTheTable walks the shipped
// router and turns red on an authenticated route that is missing from the table (or a table row with
// no route), so the forgetting is caught before it ships; the default only decides what an unlisted
// route does in the meantime.
//
// The table is keyed by the method and the route's FULL pattern (chi's RoutePattern -- the gate runs
// after routing, as every middleware of the authenticated group does).

// anyScope is GET /v1/business's requirement: any scope at all (A7 -- "her kapsam okur"; a grant
// always carries at least one, 00104's CHECK).
const anyScope = "*"

// routeScopes is the table. ADR 0047 §8.2, row by row.
var routeScopes = map[string]string{
	http.MethodGet + " " + Prefix + businessPath:    anyScope,
	http.MethodGet + " " + Prefix + timesheetsPath:  oauth.ScopeTimesheetsRead,
	http.MethodGet + " " + Prefix + employeesPath:   oauth.ScopeEmployeesRead,
	http.MethodGet + " " + Prefix + employeePath:    oauth.ScopeEmployeesRead,
	http.MethodPut + " " + Prefix + employeePath:    oauth.ScopeEmployeesWrite,
	http.MethodDelete + " " + Prefix + employeePath: oauth.ScopeEmployeesWrite,
	http.MethodPost + " " + Prefix + employeeResend: oauth.ScopeEmployeesWrite,
	http.MethodGet + " " + Prefix + directoryPath:   oauth.ScopeEmployeesRead,
	http.MethodPut + " " + Prefix + locationPath:    oauth.ScopeLocationsWrite,
	http.MethodGet + " " + Prefix + punchesPath:     oauth.ScopePunchesRead,
}

// RouteScope is the scope an OAuth token needs for the authenticated route method+pattern, and
// whether the route is open to OAuth tokens at all. "*" is any scope.
func RouteScope(method, pattern string) (string, bool) {
	s, ok := routeScopes[method+" "+pattern]
	return s, ok
}

// scopeGate is the chain's step 8 (ADR 0047 §8.1): after the credential, before the handler.
func (s *Surface) scopeGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := PrincipalOf(r)
		if !ok {
			s.unavailable(w, r, "a route reached the scope gate without a resolved credential", errors.New("no principal"))
			return
		}
		if p.Full() {
			next.ServeHTTP(w, r)
			return
		}
		need, listed := RouteScope(r.Method, chi.RouteContext(r.Context()).RoutePattern())
		switch {
		case !listed, need == anyScope && len(p.Scopes) == 0:
			writeProblem(w, problemNotForOAuth)
			return
		case need != anyScope && !p.HasScope(need):
			writeProblem(w, insufficientScope(need))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ------------------------------------------------------------------------- GET /v1/business --

// businessPath is GET /v1/business (ADR 0047 §8.4, A7).
const businessPath = "/business"

// businessIDPrefix is the business's external id prefix (K-8's emp_/loc_ pattern).
const businessIDPrefix = "biz_"

// Business is the business card's reader (*tenant.BusinessCard), declared at the consumer (§7).
type Business interface {
	APIBusiness(ctx context.Context, tenantID uuid.UUID) (tenant.APIBusiness, error)
}

var _ Business = (*tenant.BusinessCard)(nil)

// businessBody is the answer: {"id": "biz_…", "name": "…", "companyCode": "KF" | null}. companyCode is
// tenants.api_code, null until the operator opens the API (OA-2).
type businessBody struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	CompanyCode *string `json:"companyCode"`
}

// getBusiness is GET /v1/business: the credential's own business -- an API key's or a grant's, every
// scope (A7). A read: open while the business is suspended (ADR 0028 §10). It takes no query.
func (s *Surface) getBusiness(w http.ResponseWriter, r *http.Request) {
	if _, prob := readQuery(r, nil, problemNoQuery); prob != nil {
		writeProblem(w, prob)
		return
	}
	p, _ := PrincipalOf(r)
	b, err := s.business.APIBusiness(r.Context(), p.TenantID)
	if err != nil {
		s.unavailable(w, r, "reading the business failed", err)
		return
	}
	writeJSON(w, http.StatusOK, businessBody{ID: businessIDPrefix + b.ID.String(), Name: b.Name, CompanyCode: b.APICode})
}
