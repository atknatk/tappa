package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// The three OAuth resolvers (migration 00105; ADR 0047 §14.3, ADR 0002 md.7) -- context-less like
// the seven of resolve.go, because the tenant is the RESULT: a token request carries an app's
// credentials and a code or a refresh token, a /v1 request a bearer access token, and none of them
// names a business. Each calls its SECURITY DEFINER function owned by tappa_resolver, never a table:
// tappa_app holds EXECUTE and nothing on oauth_clients nor any hash column (00104). After a resolve
// the caller reads the tenant off the result and runs everything else inside WithTenant.
//
// CARRIED, NOT APPLIED (00051's rule): an expired, used or revoked token, a revoked grant, a disabled
// app and a closed API are all RETURNED; internal/oauth decides -- on every request, with no cache.
//
// Neither the secret, the code, the token nor any hash is in a result or in an error: the hash is
// the input and is never returned, and %w wraps only the database error (CLAUDE.md §7; ADR 0047
// §7.4). The statements mirror db/queries/resolve.sql verbatim.

const getOAuthClientSQL = `SELECT id, name, redirect_uris, allowed_scopes, disabled_at, secret_ok
FROM resolve_oauth_client($1, $2)`

const getOAuthCodeByHashSQL = `SELECT tenant_id, code_id, client_id, consumed_at, expires_at, grant_id
FROM resolve_oauth_code_by_hash($1)`

const getOAuthTokenByHashSQL = `SELECT tenant_id, token_id, kind, grant_id, client_id, token_scopes, expires_at, used_at,
       token_revoked_at, grant_revoked_at, client_disabled_at, api_enabled_at, api_code
FROM resolve_oauth_token_by_hash($1)`

// ResolvedOAuthClient is resolve_oauth_client's fixed column set: an app as the consent page, the
// token endpoint and the invitation gate need it. SecretOK is nil when no secret hash was given
// (the id-only callers), else whether the hash is this app's -- the stored hash never leaves the
// database. DisabledAt non-nil: the operator closed the app (401 invalid_client, the consent page's
// error page, "turned off by Taptime").
type ResolvedOAuthClient struct {
	ID            uuid.UUID
	Name          string
	RedirectURIs  []string
	AllowedScopes []string
	DisabledAt    *time.Time
	SecretOK      *bool
}

// ResolvedOAuthCode is resolve_oauth_code_by_hash's fixed column set: whose code this is. The
// consumption is a separate one-statement write inside WithTenant(TenantID)
// (store.ConsumeOAuthCode); reading ConsumedAt here and writing later would be the TOCTOU race
// CLAUDE.md §4.4 forbids.
type ResolvedOAuthCode struct {
	TenantID   uuid.UUID
	CodeID     uuid.UUID
	ClientID   uuid.UUID
	ConsumedAt *time.Time
	ExpiresAt  time.Time
	GrantID    *uuid.UUID
}

// ResolvedOAuthToken is resolve_oauth_token_by_hash's fixed column set: whose token this is and
// every fact that decides whether its request proceeds. TokenScopes is the TOKEN's own scope set
// (nil for a refresh token) -- /v1's scope check reads it, not the grant's (ADR 0047 §8.1). The
// operator closed the business's API when APIEnabledAt is nil and APICode is not (ADR 0047 §11.4).
type ResolvedOAuthToken struct {
	TenantID         uuid.UUID
	TokenID          uuid.UUID
	Kind             string
	GrantID          uuid.UUID
	ClientID         uuid.UUID
	TokenScopes      []string
	ExpiresAt        time.Time
	UsedAt           *time.Time
	TokenRevokedAt   *time.Time
	GrantRevokedAt   *time.Time
	ClientDisabledAt *time.Time
	APIEnabledAt     *time.Time
	APICode          *string
}

// GetOAuthClient resolves an app by its id WITHOUT a tenant context. secretHash nil: the id-only
// read (SecretOK nil). Returns pgx.ErrNoRows for an id that names no app.
func (d *DB) GetOAuthClient(ctx context.Context, clientID uuid.UUID, secretHash *string) (ResolvedOAuthClient, error) {
	var c ResolvedOAuthClient
	err := d.quiet().QueryRow(ctx, getOAuthClientSQL, clientID, secretHash).Scan(
		&c.ID, &c.Name, &c.RedirectURIs, &c.AllowedScopes, &c.DisabledAt, &c.SecretOK,
	)
	if err != nil {
		return ResolvedOAuthClient{}, fmt.Errorf("db: resolve oauth client: %w", err)
	}
	return c, nil
}

// GetOAuthCodeByHash resolves an authorization code's hash to its row's facts WITHOUT a tenant
// context. Returns pgx.ErrNoRows when the hash is unknown (invalid_grant).
func (d *DB) GetOAuthCodeByHash(ctx context.Context, codeHash string) (ResolvedOAuthCode, error) {
	var c ResolvedOAuthCode
	err := d.quiet().QueryRow(ctx, getOAuthCodeByHashSQL, codeHash).Scan(
		&c.TenantID, &c.CodeID, &c.ClientID, &c.ConsumedAt, &c.ExpiresAt, &c.GrantID,
	)
	if err != nil {
		return ResolvedOAuthCode{}, fmt.Errorf("db: resolve oauth code by hash: %w", err)
	}
	return c, nil
}

// GetOAuthTokenByHash resolves an access or refresh token's hash WITHOUT a tenant context. Returns
// pgx.ErrNoRows when the hash is unknown (401 invalid_token / invalid_grant).
func (d *DB) GetOAuthTokenByHash(ctx context.Context, tokenHash string) (ResolvedOAuthToken, error) {
	var k ResolvedOAuthToken
	err := d.quiet().QueryRow(ctx, getOAuthTokenByHashSQL, tokenHash).Scan(
		&k.TenantID, &k.TokenID, &k.Kind, &k.GrantID, &k.ClientID, &k.TokenScopes, &k.ExpiresAt, &k.UsedAt,
		&k.TokenRevokedAt, &k.GrantRevokedAt, &k.ClientDisabledAt, &k.APIEnabledAt, &k.APICode,
	)
	if err != nil {
		return ResolvedOAuthToken{}, fmt.Errorf("db: resolve oauth credential by hash: %w", err)
	}
	return k, nil
}
