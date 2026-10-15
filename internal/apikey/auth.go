package apikey

import (
	"context"
	"encoding"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// THE BEARER CHAIN'S BLOCKS (ADR 0028 §3 -- the order is normative):
//
//	Authorization: Bearer   ParseBearer        no database
//	the shape               Authenticator.Check no database; a wrong prefix or shape is 401 HERE
//	the address bucket      API-4               between the two, so a flood of junk keys never
//	                                            reaches the database
//	the seventh resolver    Authenticator.Resolve
//	the key bucket          API-4
//	last_used_at            Authenticator.Touch at most once a minute
//
// The decision is the application's and is taken on EVERY request, from what the resolver
// carries: unknown or revoked -> 401, the tenant's API not opened by the operator -> 403. Nothing
// is cached, so a revocation or a closing is in force on the next request.

// AuthError is a refusal of the chain. It carries the HTTP status the /v1 surface answers with and
// a fixed reason -- never the credential, never its hash, never the header.
type AuthError struct {
	status int
	reason string
}

func (e *AuthError) Error() string { return "api key: " + e.reason }

// Status is the HTTP status the refusal is answered with: 401 or 403.
func (e *AuthError) Status() int { return e.status }

// The chain's refusals. Each is a fixed value; errors.Is identifies them.
var (
	// ErrMalformed: no Bearer credential, or one that is not this deployment's key shape (the
	// other build's prefix included). Answered before the database.
	ErrMalformed = &AuthError{http.StatusUnauthorized, "no credential of this deployment's key shape"}
	// ErrUnknown: the shape is right and no key has that hash.
	ErrUnknown = &AuthError{http.StatusUnauthorized, "unknown credential"}
	// ErrRevoked: the owner revoked the key. A revoked key is no credential at all, so this
	// outranks a closed API.
	ErrRevoked = &AuthError{http.StatusUnauthorized, "revoked credential"}
	// ErrAPIClosed: the key is live and the operator has not opened -- or has closed -- this
	// tenant's integration API (K-22, tenants.api_enabled_at NULL).
	ErrAPIClosed = &AuthError{http.StatusForbidden, "the integration API is not enabled for this business"}
)

// StatusOf is the HTTP status a chain error is answered with, and 500 for anything else (a
// database failure is not the caller's fault and is not a 401).
func StatusOf(err error) int {
	var ae *AuthError
	if errors.As(err, &ae) {
		return ae.Status()
	}
	return http.StatusInternalServerError
}

// ParseBearer reads an Authorization header value: the scheme "Bearer" in any case (RFC 7235),
// one space, and a credential with no whitespace. Anything else is ErrMalformed. The header is
// never echoed.
func ParseBearer(header string) (Token, error) {
	const scheme = "bearer "
	if len(header) <= len(scheme) || !strings.EqualFold(header[:len(scheme)], scheme) {
		return Token{}, ErrMalformed
	}
	v := header[len(scheme):]
	if strings.ContainsAny(v, " \t\r\n") {
		return Token{}, ErrMalformed
	}
	return ParseToken(v), nil
}

// Checked is a credential that has the deployment's shape, carried as its keyed hash -- the
// resolver's input. The hash is as good as the key to anybody who can call the resolver, so it
// prints a placeholder exactly as Token does.
type Checked struct{ hash *string }

var (
	_ fmt.Formatter          = Checked{}
	_ fmt.Stringer           = Checked{}
	_ fmt.GoStringer         = Checked{}
	_ slog.LogValuer         = Checked{}
	_ encoding.TextMarshaler = Checked{}
)

func (Checked) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte(redactedHash)) }
func (Checked) String() string               { return redactedHash }
func (Checked) GoString() string             { return redactedHash }
func (Checked) LogValue() slog.Value         { return slog.StringValue(redactedHash) }
func (Checked) MarshalText() ([]byte, error) { return []byte(redactedHash), nil }

// Resolver is the slice of internal/db the chain needs, declared here at the consumer (§7).
type Resolver interface {
	GetAPIKeyByHash(ctx context.Context, tokenHash string) (db.ResolvedAPIKey, error)
}

// Database is WithTenant, for the one in-context write the chain makes (Touch).
type Database interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
}

// Authenticator holds the deployment's prefix and the HMAC key. Dependencies are fields (§7).
type Authenticator struct {
	prefix   string
	key      []byte
	resolver Resolver
	data     Database
}

// NewAuthenticator builds the chain's blocks for one prefix -- LivePrefix everywhere but the
// sandbox build. A wrong prefix, a key that is not 32 bytes or a nil dependency is refused.
func NewAuthenticator(prefix string, hmacKey []byte, resolver Resolver, data Database) (*Authenticator, error) {
	if err := validPrefix(prefix); err != nil {
		return nil, err
	}
	if len(hmacKey) != hmacKeyLen {
		return nil, fmt.Errorf("api key: hmac key must be %d bytes, got %d", hmacKeyLen, len(hmacKey))
	}
	if resolver == nil || data == nil {
		return nil, errors.New("api key: nil resolver or database")
	}
	return &Authenticator{prefix: prefix, key: append([]byte(nil), hmacKey...), resolver: resolver, data: data}, nil
}

// Check is the shape gate: this deployment's prefix and 43 characters of canonical base64url,
// or ErrMalformed -- with NO database work (ApiKeyAuth_WrongPrefixNeverReachesTheDatabase counts
// it). It returns the keyed hash for Resolve.
func (a *Authenticator) Check(t Token) (Checked, error) {
	v := t.reveal()
	if !shape(v, a.prefix) {
		return Checked{}, ErrMalformed
	}
	h, err := keyedHash(a.key, v)
	if err != nil {
		return Checked{}, err
	}
	return Checked{hash: &h}, nil
}

// Principal is who a resolved key speaks for: the key's tenant -- the ONE authority every later
// query is scoped to (WithTenant + an explicit tenant_id filter), never a tenant the request names
// -- and the key's id.
type Principal struct {
	TenantID uuid.UUID
	KeyID    uuid.UUID
}

// Via is detail.via of every audit row an API request writes (K-23).
const Via = "api"

// Attribution is embedded in the detail of every audit row an API request writes (K-23): the
// row's actor_id is the key's id (Actor) and its detail says the act came through the API.
type Attribution struct {
	Via string `json:"via"`
}

// Actor is the audit row's actor_id for an act of this key (K-23: actor_id = api_keys.id).
func (p Principal) Actor() *uuid.UUID {
	id := p.KeyID
	return &id
}

// Attribution is the K-23 half of the detail.
func (p Principal) Attribution() Attribution { return Attribution{Via: Via} }

// Resolve runs the seventh resolver and decides, on this request: unknown -> ErrUnknown, revoked
// -> ErrRevoked, the tenant's API closed -> ErrAPIClosed, else the Principal. A database failure
// is returned wrapped (500), naming nothing about the credential.
func (a *Authenticator) Resolve(ctx context.Context, c Checked) (Principal, error) {
	if c.hash == nil {
		return Principal{}, ErrMalformed
	}
	k, err := a.resolver.GetAPIKeyByHash(ctx, *c.hash)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Principal{}, ErrUnknown
	case err != nil:
		return Principal{}, fmt.Errorf("api key: resolve: %w", err)
	case k.RevokedAt != nil:
		return Principal{}, ErrRevoked
	case k.APIEnabledAt == nil:
		return Principal{}, ErrAPIClosed
	}
	return Principal{TenantID: k.TenantID, KeyID: k.KeyID}, nil
}

// Touch writes last_used_at -- at most once a minute per key (ADR 0028 §3), in the key's own
// tenant context, never for a revoked key. It reports whether it wrote.
func (a *Authenticator) Touch(ctx context.Context, p Principal) (bool, error) {
	var n int64
	err := a.data.WithTenant(ctx, p.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		n, e = store.New(tx).TouchAPIKey(ctx, store.TouchAPIKeyParams{TenantID: p.TenantID, ID: p.KeyID})
		return e
	})
	if err != nil {
		return false, fmt.Errorf("api key: touch: %w", err)
	}
	return n == 1, nil
}
