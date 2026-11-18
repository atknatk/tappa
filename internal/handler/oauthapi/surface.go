// Package oauthapi is the authorization server's server-to-server surface (issue #37, ADR 0047 §5,
// §6): POST /oauth/token and POST /oauth/revoke, on the main host, outside the panel's chains -- the
// shape of internal/handler/api, which it does not import. The consent half of the flow (GET
// /oauth/authorize and /admin/oauth/authorize) is the panel's: it needs the panel identity.
//
// 🔴 IT READS AND SETS NO COOKIE. A token request comes from the client's server; a browser never
// reaches it with a credential of ours, so there is no CSRF surface (ADR 0047 §3).
//
// 🔴 IT LOGS NO CREDENTIAL. The Authorization header is read once and deleted from the request; the
// client secret, the code, the verifier and the tokens travel as oauth.Value (a placeholder through
// every printing path); an error body is a fixed sentence and never echoes an input. A log line here
// carries a client id and an error at most.
//
// THE CHAIN, per request (§5.1, §8.7 -- T90's Reserve/Keep/Refund, never Allowed):
//
//	headers + body bound   no-store, Pragma: no-cache, nosniff, no-referrer; the body at most 8 KiB
//	the form               application/x-www-form-urlencoded only, no query string, no parameter twice
//	the client's shape     one Authorization: Basic, app_<uuid> and a tt_cs_<env>_ secret -- else 401
//	                       invalid_client, no database; a body client_id that differs, or any body
//	                       client_secret -> invalid_request
//	the address bucket     Reserve on 60/min/address: a full bucket is 429 temporarily_unavailable and
//	                       the resolver is not called
//	the client resolver    refused -> Keep (the unit stays counted); accepted -> Refund
//	the client bucket      600/min per client, whatever the address
//	the work               under D = 10 s, detached from the client (oauth.Server's two contexts)
package oauthapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/oauth"
)

// The surface's paths (ADR 0047 §3). AuthorizePath is the front door the app knows
// (TAPTIME_OAUTH_BASE_URL/oauth/authorize, A1); the consent itself is the panel's, under /admin
// (ConsentPath), where the panel cookie lives.
const (
	AuthorizePath = "/oauth/authorize"
	TokenPath     = "/oauth/token"
	RevokePath    = "/oauth/revoke"
	ConsentPath   = "/admin/oauth/authorize"
)

// The bounds (ADR 0047 §5.1, §5.3, §8.7). In memory, like every bucket of this single-replica product.
const (
	// MaxBody is the form's bound.
	MaxBody = 8 << 10
	// Deadline is D: every request's work runs under it, and a minting transaction that has not
	// committed when it elapses -- or when the client has left -- is rolled back.
	Deadline = 10 * time.Second

	addressLimit  = 60
	addressPeriod = time.Minute
	clientLimit   = 600
	clientPeriod  = time.Minute

	// The front door's address ceiling: the panel's flood ceiling's numbers (adminFloodLimit), for a
	// route that only redirects.
	frontLimit  = 3000
	frontPeriod = 10 * time.Minute
)

// Authority is the authorization server (*oauth.Server), declared at the consumer (§7).
type Authority interface {
	AuthenticateClient(ctx context.Context, id uuid.UUID, secret oauth.Value) (oauth.Client, error)
	ExchangeCode(ctx, live context.Context, c oauth.Client, code oauth.Value, verifier string, redirectURI *string) (oauth.Tokens, error)
	Refresh(ctx, live context.Context, c oauth.Client, refresh oauth.Value, scope []string) (oauth.Tokens, error)
	Revoke(ctx context.Context, c oauth.Client, tok oauth.Value) error
}

// Config is what New needs. The limits and D are zero in production (the shipped values).
type Config struct {
	Authority Authority
	Log       *slog.Logger
	// A test shrinks these.
	AddressLimit, ClientLimit   int
	AddressPeriod, ClientPeriod time.Duration
	Deadline                    time.Duration
}

// Surface is the token and revocation endpoints.
type Surface struct {
	auth      Authority
	log       *slog.Logger
	byAddress *httpx.Limiter
	byClient  *httpx.Limiter
	byFront   *httpx.Limiter
	deadline  time.Duration
}

// New builds the surface; a nil authority is refused.
func New(c Config) (*Surface, error) {
	if c.Authority == nil {
		return nil, errors.New("oauthapi: nil authorization server")
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
		auth:      c.Authority,
		log:       log,
		byAddress: httpx.NewLimiter(pick(c.AddressLimit, addressLimit), pickd(c.AddressPeriod, addressPeriod)),
		byClient:  httpx.NewLimiter(pick(c.ClientLimit, clientLimit), pickd(c.ClientPeriod, clientPeriod)),
		byFront:   httpx.NewLimiter(frontLimit, frontPeriod),
		deadline:  pickd(c.Deadline, Deadline),
	}, nil
}

// Mount registers the three routes, each with the surface's own middleware -- no panel middleware runs
// on them (the two POSTs are route class 3, cmd/tappa's walk).
func (s *Surface) Mount(r chi.Router) {
	r.With(s.headers).Get(AuthorizePath, s.authorizeFront)
	r.With(s.headers, s.guard).Post(TokenPath, s.token)
	r.With(s.headers, s.guard).Post(RevokePath, s.revoke)
}

// authorizeFront is GET /oauth/authorize (ADR 0047 §3, O-3): a 303 to the panel's consent with the
// query unchanged. It reads no cookie, writes none and asks no database -- the consent's own chain
// validates the request (V1-V7). Its address ceiling is a flood bound, not a credential bucket.
func (s *Surface) authorizeFront(w http.ResponseWriter, r *http.Request) {
	if ok, n := s.byFront.TryCharge(httpx.RateKey(httpx.ClientIP(r))); !ok {
		if s.byFront.FirstOverLimit(n) {
			s.log.WarnContext(r.Context(), "oauth: the authorization front door's address ceiling is reached", "limit", s.byFront.Limit())
		}
		w.Header().Set("Retry-After", strconv.Itoa(int(s.byFront.Period()/time.Second)))
		http.Error(w, "429 too many requests", http.StatusTooManyRequests)
		return
	}
	to := ConsentPath
	if r.URL.RawQuery != "" {
		to += "?" + r.URL.RawQuery
	}
	w.Header().Set("Location", to)
	w.WriteHeader(http.StatusSeeOther)
}

// headers: no caching (a response carries tokens -- RFC 6749 §5.1's Cache-Control and Pragma), no
// sniffing, no referrer.
func (s *Surface) headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("Pragma", "no-cache")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// guard answers a panic 503 temporarily_unavailable (with the stack in the log -- the Authorization
// header is already deleted, and a stack carries no header value), re-raising net/http's own abort.
func (s *Surface) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gw := &guardWriter{ResponseWriter: w}
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.log.ErrorContext(r.Context(), "oauth: a handler panicked", "panic", fmt.Sprint(v), "stack", string(debug.Stack()))
				if !gw.wrote {
					writeError(gw, errUnavailable)
				}
			}
		}()
		next.ServeHTTP(gw, r)
	})
}

type guardWriter struct {
	http.ResponseWriter
	wrote bool
}

func (g *guardWriter) WriteHeader(code int) { g.wrote = true; g.ResponseWriter.WriteHeader(code) }
func (g *guardWriter) Write(b []byte) (int, error) {
	g.wrote = true
	return g.ResponseWriter.Write(b)
}
func (g *guardWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }

// ------------------------------------------------------------------------------ the error body --

// apiError is one answer of the error contract (RFC 6749 §5.2; ADR 0047 §5.4): a word, a status and a
// fixed English sentence that never echoes an input (the client does not show it -- A4).
type apiError struct {
	code        string
	status      int
	description string
	retryAfter  time.Duration
}

var (
	errInvalidRequest = apiError{"invalid_request", http.StatusBadRequest, "The request is missing a parameter, repeats one, or carries one this endpoint does not accept.", 0}
	errInvalidClient  = apiError{"invalid_client", http.StatusUnauthorized, "The client could not be authenticated.", 0}
	errInvalidGrant   = apiError{"invalid_grant", http.StatusBadRequest, "The authorization code or refresh token is invalid, expired, revoked or was issued to another client.", 0}
	errUnsupported    = apiError{"unsupported_grant_type", http.StatusBadRequest, "Only authorization_code and refresh_token are supported.", 0}
	errInvalidScope   = apiError{"invalid_scope", http.StatusBadRequest, "The requested scope is empty, unknown or wider than the grant's.", 0}
	errUnavailable    = apiError{"temporarily_unavailable", http.StatusServiceUnavailable, "The server could not complete the request. Try again shortly.", 0}
)

// errRateLimited is 429 temporarily_unavailable with Retry-After -- a deviation from RFC 6749 §5.2's
// set, named in ADR 0047 §5.4 and §18.2 item 12.
func errRateLimited(after time.Duration) apiError {
	return apiError{"temporarily_unavailable", http.StatusTooManyRequests, "Too many requests. Try again after the time in Retry-After.", after}
}

func writeError(w http.ResponseWriter, e apiError) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	if e.status == http.StatusUnauthorized {
		h.Set("WWW-Authenticate", `Basic realm="taptime"`)
	}
	if e.retryAfter > 0 {
		h.Set("Retry-After", strconv.Itoa(int((e.retryAfter+time.Second-1)/time.Second)))
	}
	w.WriteHeader(e.status)
	_ = json.NewEncoder(w).Encode(struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}{e.code, e.description})
}

// errorOf maps the authorization server's refusals; anything else is 503.
func errorOf(err error) (apiError, bool) {
	var oe *oauth.Error
	if !errors.As(err, &oe) {
		return errUnavailable, false
	}
	switch oe {
	case oauth.ErrInvalidRequest:
		return errInvalidRequest, true
	case oauth.ErrInvalidClient:
		return errInvalidClient, true
	case oauth.ErrInvalidGrant:
		return errInvalidGrant, true
	case oauth.ErrUnsupportedGrantType:
		return errUnsupported, true
	case oauth.ErrInvalidScope:
		return errInvalidScope, true
	}
	return errUnavailable, false
}

// ------------------------------------------------------------------------ the shared front half --

// form reads the body as the endpoint's one form: the media type, no query string, the bound, every
// parameter at most once (RFC 6749 §3.2: "MUST NOT be included more than once").
func (s *Surface) form(w http.ResponseWriter, r *http.Request) (url.Values, bool) {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/x-www-form-urlencoded" || r.URL.RawQuery != "" {
		writeError(w, errInvalidRequest)
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxBody)
	if err := r.ParseForm(); err != nil {
		writeError(w, errInvalidRequest)
		return nil, false
	}
	for _, vs := range r.PostForm {
		if len(vs) > 1 {
			writeError(w, errInvalidRequest)
			return nil, false
		}
	}
	return r.PostForm, true
}

// takeAuthorization reads the Authorization header and DELETES it from the request -- first thing,
// before the body is parsed -- so nothing after it (a parse failure's answer, a handler, a log line, a
// panic) can carry it (§5.1).
func takeAuthorization(r *http.Request) []string {
	vals := r.Header.Values("Authorization")
	r.Header.Del("Authorization")
	return vals
}

// client is §5.1: the Basic credential (already taken off the request), its combinations with the
// body, the address bucket's reservation around the resolver, and the client bucket.
func (s *Surface) client(ctx context.Context, w http.ResponseWriter, r *http.Request, vals []string, form url.Values) (oauth.Client, bool) {
	if len(vals) != 1 {
		writeError(w, errInvalidClient)
		return oauth.Client{}, false
	}
	id, secret, ok := oauth.ParseBasic(vals[0])
	if !ok || !secret.Is(oauth.KindSecret) {
		writeError(w, errInvalidClient)
		return oauth.Client{}, false
	}
	if _, has := form["client_secret"]; has {
		writeError(w, errInvalidRequest)
		return oauth.Client{}, false
	}
	if v, has := form["client_id"]; has && v[0] != oauth.ClientIDOf(id) {
		writeError(w, errInvalidRequest)
		return oauth.Client{}, false
	}
	res, within, first, _ := s.byAddress.Reserve(httpx.RateKey(httpx.ClientIP(r)))
	if !within {
		if first {
			s.log.WarnContext(r.Context(), "oauth: the endpoint's address bucket is full", "limit", s.byAddress.Limit())
		}
		writeError(w, errRateLimited(s.byAddress.Period()))
		return oauth.Client{}, false
	}
	c, err := s.auth.AuthenticateClient(ctx, id, secret)
	switch {
	case errors.Is(err, oauth.ErrInvalidClient):
		res.Keep()
		writeError(w, errInvalidClient)
		return oauth.Client{}, false
	case err != nil:
		// Not the caller's fault: given back.
		res.Refund()
		s.log.ErrorContext(r.Context(), "oauth: authenticating the client failed", "err", err)
		writeError(w, errUnavailable)
		return oauth.Client{}, false
	}
	res.Refund()
	if n := s.byClient.Charge(c.ID.String()); n > s.byClient.Limit() {
		if s.byClient.FirstOverLimit(n) {
			s.log.WarnContext(r.Context(), "oauth: a client's bucket is full", "client_id", oauth.ClientIDOf(c.ID))
		}
		writeError(w, errRateLimited(s.byClient.Period()))
		return oauth.Client{}, false
	}
	return c, true
}

// work is the request's work context: detached from the client and bounded by D.
func (s *Surface) work(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(r.Context()), s.deadline)
}

// ------------------------------------------------------------------------------- the endpoints --

// tokenResponse is RFC 6749 §5.1's body (A3).
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// token is POST /oauth/token (§5.2, §5.3).
func (s *Surface) token(w http.ResponseWriter, r *http.Request) {
	auth := takeAuthorization(r)
	ctx, cancel := s.work(r)
	defer cancel()
	form, ok := s.form(w, r)
	if !ok {
		return
	}
	c, ok := s.client(ctx, w, r, auth, form)
	if !ok {
		return
	}
	var tok oauth.Tokens
	var err error
	switch form.Get("grant_type") {
	case "authorization_code":
		code, verifier := form.Get("code"), form.Get("code_verifier")
		if code == "" || verifier == "" {
			writeError(w, errInvalidRequest)
			return
		}
		var redirect *string
		if v, has := form["redirect_uri"]; has {
			redirect = &v[0]
		}
		tok, err = s.auth.ExchangeCode(ctx, r.Context(), c, oauth.ParseValue(code), verifier, redirect)
	case "refresh_token":
		rt := form.Get("refresh_token")
		if rt == "" {
			writeError(w, errInvalidRequest)
			return
		}
		var scope []string
		if v, has := form["scope"]; has {
			if scope, err = oauth.ParseScope(v[0]); err != nil {
				writeError(w, errInvalidScope)
				return
			}
		}
		tok, err = s.auth.Refresh(ctx, r.Context(), c, oauth.ParseValue(rt), scope)
	case "":
		writeError(w, errInvalidRequest)
		return
	default:
		writeError(w, errUnsupported)
		return
	}
	if err != nil {
		e, known := errorOf(err)
		if !known && !errors.Is(err, oauth.ErrEnded) {
			s.log.ErrorContext(r.Context(), "oauth: the grant request failed", "client_id", oauth.ClientIDOf(c.ID), "err", err)
		}
		writeError(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(tokenResponse{
		AccessToken: tok.Access.RevealInTokenResponse(), TokenType: "Bearer", ExpiresIn: tok.ExpiresIn,
		RefreshToken: tok.Refresh.RevealInTokenResponse(), Scope: oauth.ScopeString(tok.Scopes),
	})
}

// revoke is POST /oauth/revoke (RFC 7009, §6): 200 with no body for every token the client may name,
// its own or not, known or not; token_type_hint is ignored (the prefix says which).
func (s *Surface) revoke(w http.ResponseWriter, r *http.Request) {
	auth := takeAuthorization(r)
	ctx, cancel := s.work(r)
	defer cancel()
	form, ok := s.form(w, r)
	if !ok {
		return
	}
	c, ok := s.client(ctx, w, r, auth, form)
	if !ok {
		return
	}
	tok := form.Get("token")
	if tok == "" {
		writeError(w, errInvalidRequest)
		return
	}
	if err := s.auth.Revoke(ctx, c, oauth.ParseValue(tok)); err != nil {
		s.log.ErrorContext(r.Context(), "oauth: the revocation failed", "client_id", oauth.ClientIDOf(c.ID), "err", err)
		writeError(w, errUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}
