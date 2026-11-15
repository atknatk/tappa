package oauth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// THE PENDING REQUEST (ADR 0047 §4.2, O-4): every authorization request that passed V1-V7 is written
// -- signed, short-lived -- into the tappa_oauth_request cookie, signed in or not, and the consent
// page applies THE COOKIE, never the query. The consent and refusal forms carry no request parameter:
// only the digest of the cookie the page was drawn from (rd), so a second authorization request that
// overwrote the cookie in another tab makes the first page's "Allow" refuse instead of approving the
// second app.
//
// 🔴 THE CONTENT IS NOT A SECRET (the challenge is a hash, the state is the client's) -- the
// signature only stops tampering. The VALUE is still on the never-log list (ADR 0047 §7.4): it carries
// the client's state.

// RequestCookieName is the cookie's name; RequestCookiePath its path (the panel's, like the session's)
// and RequestMaxAge its life (§7.3: ten minutes).
const (
	RequestCookieName = "tappa_oauth_request"
	RequestCookiePath = "/admin"
	RequestMaxAge     = 10 * time.Minute
	// MaxRequestCookieBytes bounds the cookie's name, "=" and value together: browsers keep a cookie
	// of about 4096 bytes and drop a larger one silently, which would lose the request between the
	// sign-in and the page. The worst case V2 and V7 admit today (a 512-byte URI, a 1024-byte state
	// of characters JSON escapes) seals to about 3745 bytes with the name; the bound refuses anything
	// past it (ErrRequestTooLarge) should a rule widen.
	MaxRequestCookieBytes = 3800
)

// ResponseTypeCode is V3's one value.
const ResponseTypeCode = "code"

// Request is one validated authorization request -- the cookie's content.
type Request struct {
	ClientID     uuid.UUID `json:"c"`
	RedirectURI  string    `json:"r"`
	ResponseType string    `json:"t"`
	Method       string    `json:"m"`
	Challenge    string    `json:"x"`
	Scopes       []string  `json:"s"`
	// State is the client's state ("" when it sent none -- an empty state is none).
	State string `json:"st,omitempty"`
	// Created is when the request was validated, the cookie's age; this process's clock, never a
	// database lifetime.
	Created int64 `json:"at"`
}

var (
	// ErrRequestInvalid is a cookie that does not open: no signature, a wrong one, not JSON, a field
	// out of its rule. One error for every cause -- the page says "start again from the app".
	ErrRequestInvalid = errors.New("oauth: the pending request does not verify")
	// ErrRequestTooLarge is a request whose sealed cookie would pass MaxRequestCookieBytes.
	ErrRequestTooLarge = errors.New("oauth: the pending request is too large for its cookie")
	// ErrRequestExpired is a cookie past RequestMaxAge (or from the future).
	ErrRequestExpired = errors.New("oauth: the pending request has expired")
)

// SealRequest is the cookie's value: base64url(JSON) "." base64url(HMAC(request label || JSON)).
// The JSON is written without HTML escaping (<, > and & stay one byte each, not six): the value is a
// cookie, never markup, and escaping would let a state of 1024 such bytes alone fill it.
func (h *Hasher) SealRequest(r Request) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(r); err != nil {
		return "", errors.New("oauth: encode the pending request")
	}
	b := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	v := base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(h.mac(labelRequest, string(b)))
	if len(RequestCookieName)+1+len(v) > MaxRequestCookieBytes {
		return "", ErrRequestTooLarge
	}
	return v, nil
}

// OpenRequest verifies a cookie value -- the signature in constant time, then the age (at most
// RequestMaxAge, not ahead of now by more than a minute), then every field against the rule that
// let it in (§4.2's list, items 4-8: V3-V7 as the cookie holds them). It does not reread the
// registry: the caller re-runs V1, V2 and V6's client half against the live client.
func (h *Hasher) OpenRequest(v string, now time.Time) (Request, error) {
	body64, sig64, ok := strings.Cut(v, ".")
	if !ok || body64 == "" || sig64 == "" {
		return Request{}, ErrRequestInvalid
	}
	body, err := base64.RawURLEncoding.Strict().DecodeString(body64)
	if err != nil {
		return Request{}, ErrRequestInvalid
	}
	sig, err := base64.RawURLEncoding.Strict().DecodeString(sig64)
	if err != nil || !hmac.Equal(sig, h.mac(labelRequest, string(body))) {
		return Request{}, ErrRequestInvalid
	}
	var r Request
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return Request{}, ErrRequestInvalid
	}
	created := time.Unix(r.Created, 0)
	if now.Sub(created) > RequestMaxAge || created.Sub(now) > time.Minute {
		return Request{}, ErrRequestExpired
	}
	if r.ClientID == uuid.Nil || r.RedirectURI == "" || r.ResponseType != ResponseTypeCode || r.Method != MethodS256 ||
		!ValidChallenge(r.Challenge) || !ValidState(r.State) {
		return Request{}, ErrRequestInvalid
	}
	if s, err := ParseScope(strings.Join(r.Scopes, " ")); err != nil || !slices.Equal(s, r.Scopes) {
		return Request{}, ErrRequestInvalid
	}
	return r, nil
}

// RequestDigest is rd: hex HMAC(request-digest label || the cookie's value) -- what the consent page
// was drawn from.
func (h *Hasher) RequestDigest(cookieValue string) string {
	return hex.EncodeToString(h.mac(labelRequestDigest, cookieValue))
}

// SameRequest reports, in constant time, whether rd is the digest of cookieValue.
func (h *Hasher) SameRequest(cookieValue, rd string) bool {
	return hmac.Equal([]byte(h.RequestDigest(cookieValue)), []byte(rd))
}

// ------------------------------------------------------------------- the authorization request --

// Client is a registry row as the authorization server uses it (resolve_oauth_client's columns,
// the secret's hash never among them).
type Client struct {
	ID            uuid.UUID
	Name          string
	RedirectURIs  []string
	AllowedScopes []string
	Disabled      bool
}

// ClientLookup resolves a client by its registry id; ErrNoClient when no row has it.
type ClientLookup func(ctx context.Context, id uuid.UUID) (Client, error)

// ErrNoClient is a client id that names no registry row.
var ErrNoClient = errors.New("oauth: no such client")

// Verdict is what an authorization request comes to.
type Verdict int

const (
	// VerdictFatal: V1 or V2 failed -- NO redirect (ADR 0047 §4.1, §16): Taptime's own error page.
	VerdictFatal Verdict = iota
	// VerdictRedirectError: V3-V7 failed -- a 303 to the REGISTERED redirect URI with error, the
	// state when it is a valid one, and iss.
	VerdictRedirectError
	// VerdictOK: the request is valid; Request holds it.
	VerdictOK
)

// The authorization endpoint's error words (RFC 6749 §4.1.2.1).
const (
	ErrorInvalidRequest          = "invalid_request"
	ErrorUnsupportedResponseType = "unsupported_response_type"
	ErrorInvalidScope            = "invalid_scope"
	ErrorAccessDenied            = "access_denied"
)

// Authorization is the verdict on one request.
type Authorization struct {
	Verdict Verdict
	// Client is the resolved client (VerdictRedirectError and VerdictOK).
	Client Client
	// RedirectURI is the registered URI the request matched (VerdictRedirectError and VerdictOK).
	RedirectURI string
	// Error is the error word of a VerdictRedirectError.
	Error string
	// State is reflected only when it passed V7 ("" otherwise -- a V7 failure is not reflected).
	State string
	// Request is the validated request (VerdictOK), Created unset -- SealRequest's caller stamps it.
	Request Request
}

// singleOrRepeated is the query's values of one name: the value when there is exactly one, and
// whether the name appears more than once.
func singleOrRepeated(q url.Values, name string) (string, bool, bool) {
	vs, present := q[name]
	switch {
	case !present:
		return "", false, false
	case len(vs) > 1:
		return "", true, true
	}
	return vs[0], true, false
}

// Authorize applies V1-V7 (ADR 0047 §4.1, in the RFC's normative order) to a raw query string. Every
// parameter appears at most once: a repeated client_id or redirect_uri is fatal (before the client is
// known, no redirect); any other repeated parameter is invalid_request (after). An error is returned
// only when the registry could not be read.
func Authorize(ctx context.Context, rawQuery string, lookup ClientLookup) (Authorization, error) {
	q, err := url.ParseQuery(rawQuery)
	if err != nil {
		return Authorization{Verdict: VerdictFatal}, nil
	}
	// V1 -- client_id: app_<uuid>, registered, not disabled.
	cid, _, rep := singleOrRepeated(q, "client_id")
	if rep {
		return Authorization{Verdict: VerdictFatal}, nil
	}
	id, ok := ParseClientID(cid)
	if !ok {
		return Authorization{Verdict: VerdictFatal}, nil
	}
	c, err := lookup(ctx, id)
	if errors.Is(err, ErrNoClient) {
		return Authorization{Verdict: VerdictFatal}, nil
	}
	if err != nil {
		return Authorization{}, err
	}
	if c.Disabled {
		return Authorization{Verdict: VerdictFatal}, nil
	}
	// V2 -- redirect_uri: required, byte for byte one of the registered list, and this build's gate.
	ru, _, rep := singleOrRepeated(q, "redirect_uri")
	if rep || ru == "" || !slices.Contains(c.RedirectURIs, ru) || !AuthorizeURIAllowed(ru) {
		return Authorization{Verdict: VerdictFatal}, nil
	}
	out := Authorization{Verdict: VerdictRedirectError, Client: c, RedirectURI: ru}
	// The state is reflected in every redirect from here on -- but only a valid one (V7's failure is
	// answered without it: the "Uygulama notları").
	state, _, stateRep := singleOrRepeated(q, "state")
	stateOK := !stateRep && ValidState(state)
	if stateOK {
		out.State = state
	}
	for name := range q {
		if name != "client_id" && name != "redirect_uri" && len(q[name]) > 1 {
			out.Error = ErrorInvalidRequest
			return out, nil
		}
	}
	// V3 -- response_type exactly code: absent is a missing required parameter (invalid_request, RFC
	// 6749 §4.1.2.1); present and not code is a type this server does not issue (unsupported_response_type).
	switch rt, has := q["response_type"]; {
	case !has || rt[0] == "":
		out.Error = ErrorInvalidRequest
		return out, nil
	case rt[0] != ResponseTypeCode:
		out.Error = ErrorUnsupportedResponseType
		return out, nil
	}
	// V4 -- code_challenge_method exactly S256 (absent means "plain" by RFC 7636 -- refused).
	if q.Get("code_challenge_method") != MethodS256 {
		out.Error = ErrorInvalidRequest
		return out, nil
	}
	// V5 -- the challenge's shape.
	challenge := q.Get("code_challenge")
	if !ValidChallenge(challenge) {
		out.Error = ErrorInvalidRequest
		return out, nil
	}
	// V6 -- the scope: not empty, every word known, the set within the client's allowed set.
	scopes, err := ParseScope(q.Get("scope"))
	if err != nil || !SubsetOf(scopes, c.AllowedScopes) {
		out.Error = ErrorInvalidScope
		return out, nil
	}
	// V7 -- the state's shape.
	if !stateOK {
		out.Error = ErrorInvalidRequest
		return out, nil
	}
	out.Verdict, out.Error = VerdictOK, ""
	out.Request = Request{
		ClientID: c.ID, RedirectURI: ru, ResponseType: ResponseTypeCode, Method: MethodS256,
		Challenge: challenge, Scopes: scopes, State: state,
	}
	return out, nil
}

// Recheck re-runs V1, V2 and V6's client half against the LIVE client for a request read from the
// cookie (§4.2 items 2, 3 and 7 -- the operator may have changed the list or disabled the client in
// between). false: the request no longer stands; the caller treats it as V1/V2 (no redirect) when the
// client or the URI went, and V6 when only the scopes did -- RecheckScopes tells which.
func Recheck(c Client, r Request) (redirectOK, scopesOK bool) {
	redirectOK = !c.Disabled && c.ID == r.ClientID && slices.Contains(c.RedirectURIs, r.RedirectURI) && AuthorizeURIAllowed(r.RedirectURI)
	scopesOK = len(r.Scopes) > 0 && SubsetOf(r.Scopes, c.AllowedScopes)
	return redirectOK, scopesOK
}

// ErrorRedirect is the 303 target of an authorization error: the registered URI with error, the
// state when one is reflected, and iss (RFC 9207).
func ErrorRedirect(redirectURI, code, state, issuer string) string {
	v := url.Values{"error": {code}, "iss": {issuer}}
	if state != "" {
		v.Set("state", state)
	}
	return WithParams(redirectURI, v)
}

// CodeRedirect is the 303 target of an approval: the registered URI with code, the state when the
// request had one, and iss (§4.5).
func CodeRedirect(redirectURI string, code Value, state, issuer string) string {
	v := url.Values{"code": {code.RevealInRedirect()}, "iss": {issuer}}
	if state != "" {
		v.Set("state", state)
	}
	return WithParams(redirectURI, v)
}
