package api

import (
	"context"
	"net/http"

	"github.com/atknatk/tappa/internal/apikey"
)

// THE BEARER CHAIN (ADR 0028 §3 -- the order is normative, and each step's place is measured by
// TestApiAuth_IPBucketBeforeResolver):
//
//	1 Authorization: Bearer   exactly one header; apikey.ParseBearer                  no database
//	2 the shape               Keychain.Check: this deployment's prefix + 43 base64url  no database
//	                          -- a key of the other build's prefix is 401 HERE
//	3 the address bucket      before the resolver: a flood of well-shaped junk keys
//	                          reaches the database at most addressLimit times a period
//	4 the seventh resolver    Keychain.Resolve: unknown/revoked 401, the business's API
//	                          closed 403 -- decided on THIS request, nothing cached
//	5 the key bucket          per key, whatever the address
//	6 last_used_at            Keychain.Touch, at most once a minute per key
//	7 K-19                    the key's tenant enters the worker's set
//
// Unknown and revoked answer the same 401 with the same body: the response is no oracle for
// which keys once existed. After the chain the Authorization header is DELETED from the request,
// so nothing downstream -- a handler, a log line, a panic dump -- can carry it.

type principalKey struct{}

// PrincipalOf is the resolved key of an authenticated request: its tenant is the ONE authority
// every query of the request is scoped to (WithTenant + an explicit tenant_id filter) -- never a
// tenant the request names.
func PrincipalOf(r *http.Request) (apikey.Principal, bool) {
	p, ok := r.Context().Value(principalKey{}).(apikey.Principal)
	return p, ok
}

// authorizationHeader is the one header the chain reads.
const authorizationHeader = "Authorization"

func (s *Surface) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vals := r.Header.Values(authorizationHeader)
		r.Header.Del(authorizationHeader)
		if len(vals) != 1 {
			writeProblem(w, problemUnauthorized)
			return
		}
		tok, err := apikey.ParseBearer(vals[0])
		if err != nil {
			writeProblem(w, problemUnauthorized)
			return
		}
		checked, err := s.keys.Check(tok)
		if err != nil {
			s.refuse(w, r, err)
			return
		}
		if !s.chargeAddress(w, r) {
			return
		}
		p, err := s.keys.Resolve(r.Context(), checked)
		if err != nil {
			s.refuse(w, r, err)
			return
		}
		if n := s.byKey.Charge(p.KeyID.String()); n > s.byKey.Limit() {
			writeProblem(w, rateLimited(s.byKey.Period()))
			return
		}
		if _, err := s.keys.Touch(r.Context(), p); err != nil {
			s.unavailable(w, r, "recording the key's last use failed", err)
			return
		}
		s.work.Notice(p.TenantID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	})
}

// refuse maps a chain error: 401 and 403 by apikey.StatusOf, anything else 503 with the cause in
// the log.
func (s *Surface) refuse(w http.ResponseWriter, r *http.Request, err error) {
	switch apikey.StatusOf(err) {
	case http.StatusUnauthorized:
		writeProblem(w, problemUnauthorized)
	case http.StatusForbidden:
		writeProblem(w, problemAPIDisabled)
	default:
		s.unavailable(w, r, "resolving the credential failed", err)
	}
}

// Compile-time: the shipped chain is a Keychain and the shipped runner a Writer.
var (
	_ Keychain = (*apikey.Authenticator)(nil)
	_ Writer   = (*apikey.Writes)(nil)
)
