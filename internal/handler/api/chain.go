package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/apikey"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/oauth"
)

// THE BEARER CHAIN (ADR 0028 §3 -- the order is normative, and each step's place is measured by
// TestApiAuth_IPBucketBeforeResolver). Since issue #37 (ADR 0047 §8.1) it takes TWO credentials, told
// apart by their shape with no database: an API key (tt_live_ + 43) and an OAuth access token
// (tt_at_live_ + 43; the sandbox build's _test_ twins). Steps 2-5 differ by kind; the key's are
// UNCHANGED.
//
//	                          API key                              OAuth access token
//	1 Authorization: Bearer   exactly one header; apikey.ParseBearer, no database
//	2 the shape               Keychain.Check                       Grants.CheckAccess
//	                          -- neither shape (the other build's prefix included): 401 HERE
//	3 the address bucket      Charge: every request counts         Reserve (T90: the charge and the
//	                          (unchanged)                          decision in ONE locked step), on the
//	                                                               SAME limiter -- full: 429, no resolver
//	4 the resolver            Keychain.Resolve: unknown/revoked    Grants.ResolveAccess: unknown, expired,
//	                          401, the API closed 403              revoked, of a revoked grant or of a
//	                                                               disabled app 401 invalid_token; the
//	                                                               operator closed the API 403 -- both
//	                                                               KEEP the reservation; accepted: REFUND
//	5 the identity bucket     per key (600/min)                    per grant (600/min) AND per app
//	                                                               (6000/min)
//	6 last_used_at            Keychain.Touch                       Grants.TouchGrant -- at most once a
//	                                                               minute
//	7 K-19                    the tenant enters the worker's set
//	8 the scope               passes (Full)                        scopeGate: the token's OWN scopes
//
// Unknown and revoked answer the same 401 with the same body: the response is no oracle for which
// credentials once existed. After the chain the Authorization header is DELETED from the request, so
// nothing downstream -- a handler, a log line, a panic dump -- can carry it.
//
// THE ADDRESS BUCKET'S BOUND, FOR BOTH KINDS (ADR 0047 §8.7): a refused credential is charged and
// never given back, so in one period at most addressLimit refused credentials of one address reach a
// resolver -- keys and tokens TOGETHER, it is one limiter. An accepted token is given back: the shared
// egress address of an app connected to many businesses does not run out on its successful traffic,
// which the grant and app buckets bound instead.

type principalKey struct{}

// PrincipalOf is the resolved credential of an authenticated request: its tenant is the ONE authority
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
		var p apikey.Principal
		if access, err := s.grants.CheckAccess(tok.OAuth()); err == nil {
			var ok bool
			if p, ok = s.resolveAccess(w, r, access); !ok {
				return
			}
		} else if !errors.Is(err, oauth.ErrNotAnAccessToken) {
			s.unavailable(w, r, "checking the access token failed", err)
			return
		} else {
			var ok bool
			if p, ok = s.resolveKey(w, r, tok); !ok {
				return
			}
		}
		s.work.Notice(p.TenantID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	})
}

// resolveKey is steps 2-6 for an API key -- ADR 0028 §3's chain as it was.
func (s *Surface) resolveKey(w http.ResponseWriter, r *http.Request, tok apikey.Token) (apikey.Principal, bool) {
	checked, err := s.keys.Check(tok)
	if err != nil {
		s.refuse(w, r, err)
		return apikey.Principal{}, false
	}
	if !s.chargeAddress(w, r) {
		return apikey.Principal{}, false
	}
	p, err := s.keys.Resolve(r.Context(), checked)
	if err != nil {
		s.refuse(w, r, err)
		return apikey.Principal{}, false
	}
	if n := s.byKey.Charge(p.KeyID.String()); n > s.byKey.Limit() {
		writeProblem(w, rateLimited(s.byKey.Period()))
		return apikey.Principal{}, false
	}
	if _, err := s.keys.Touch(r.Context(), p); err != nil {
		s.unavailable(w, r, "recording the key's last use failed", err)
		return apikey.Principal{}, false
	}
	return p, true
}

// resolveAccess is steps 3-6 for an OAuth access token (ADR 0047 §8.1, §8.7).
func (s *Surface) resolveAccess(w http.ResponseWriter, r *http.Request, c oauth.CheckedAccess) (apikey.Principal, bool) {
	res, within, _, _ := s.byAddress.Reserve(httpx.RateKey(httpx.ClientIP(r)))
	if !within {
		writeProblem(w, rateLimited(s.byAddress.Period()))
		return apikey.Principal{}, false
	}
	a, err := s.grants.ResolveAccess(r.Context(), c)
	switch {
	case errors.Is(err, oauth.ErrInvalidToken):
		res.Keep()
		writeProblem(w, problemInvalidToken)
		return apikey.Principal{}, false
	case errors.Is(err, oauth.ErrAPIClosed):
		res.Keep()
		// The key's own refusal, through the key's mapping: one 403 api_disabled for both kinds.
		s.refuse(w, r, apikey.ErrAPIClosed)
		return apikey.Principal{}, false
	case err != nil:
		// Not the caller's fault: given back.
		res.Refund()
		s.unavailable(w, r, "resolving the access token failed", err)
		return apikey.Principal{}, false
	}
	res.Refund()
	if a.GrantID == uuid.Nil {
		// An accepted token that names no grant is the resolver's fault, not a credential: never a
		// principal (it would be neither a key nor a grant -- apikey.Principal.Full fails closed on it,
		// and this refuses it before it gets that far).
		s.unavailable(w, r, "the access token resolved to no grant", errors.New("zero grant id"))
		return apikey.Principal{}, false
	}
	if n := s.byGrant.Charge(a.GrantID.String()); n > s.byGrant.Limit() {
		writeProblem(w, rateLimited(s.byGrant.Period()))
		return apikey.Principal{}, false
	}
	if n := s.byClient.Charge(a.ClientID.String()); n > s.byClient.Limit() {
		if s.byClient.FirstOverLimit(n) {
			s.log.WarnContext(r.Context(), "api: an app's bucket is full", "client_id", oauth.ClientIDOf(a.ClientID))
		}
		writeProblem(w, rateLimited(s.byClient.Period()))
		return apikey.Principal{}, false
	}
	if _, err := s.grants.TouchGrant(r.Context(), a.TenantID, a.GrantID); err != nil {
		s.unavailable(w, r, "recording the grant's last use failed", err)
		return apikey.Principal{}, false
	}
	return apikey.Principal{TenantID: a.TenantID, GrantID: a.GrantID, ClientID: a.ClientID, Scopes: a.Scopes}, true
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

// Compile-time: the shipped chain is a Keychain, the authorization server the Grants and the shipped
// runner a Writer.
var (
	_ Keychain = (*apikey.Authenticator)(nil)
	_ Grants   = (*oauth.Server)(nil)
	_ Writer   = (*apikey.Writes)(nil)
)
