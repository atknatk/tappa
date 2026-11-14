package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ------------------------------------------------------------- issue #37 --
//
// The OAuth app registry's operator surface (migration 00107; ADR 0047 §10.1-§10.2, ADR 0021 §2).
// Four one-phase writes in CreateHolding's shape -- free functions over an OperatorConn, one
// statement each, so the change and the operator's row commit together or not at all -- and the
// two-phase read in Holdings' shape. The operator screen (ADR 0047 §10.3) is a later phase.
//
// 🔴 THE SECRET NEVER ENTERS THIS PACKAGE. The caller mints it, HMACs it under
// TAPPA_OAUTH_HMAC_KEY and shows it once (ADR 0047 §7.2, §10.2); these functions take the hex hash
// and the hint. Neither the hash nor any argument reaches an error: operatorErr carries the
// SQLSTATE and nothing else.
//
// 🔴 NOTHING HERE TELLS WHAT THE REGISTRY HELD. Rotate, SetRedirects and Disable return nil for an
// app that exists, one that is disabled and an id that names nothing alike (ADR 0021 §2 v 7); the
// result is read back with OAuthClients. A refused argument -- a name, a redirect list, a scope
// set, a hash or a hint the rules refuse -- is a database error carrying SQLSTATE 22023, and
// nothing is written; a dead session is ErrOperatorRefused.

// oauthClientsReadKind is 00107's read kind -- a member of operator_read_tickets_kind_check. Its
// parameter object is the paged lists' {page_number, page_size} (legalVersionsParams' shape).
const oauthClientsReadKind = "oauth_clients"

// The four kinds of migration 00107 (CHECK order: after tenant_reinstated): the operator's row of
// every accepted call of the four registry writers. Each names the app in its detail
// ({"client_id": "<uuid>"}) and nothing else -- no tenant (operator_audit_log_oauth_client_shape).
const (
	OperatorAuditOAuthClientRegistered       OperatorAuditKind = "oauth_client_registered"
	OperatorAuditOAuthClientSecretRotated    OperatorAuditKind = "oauth_client_secret_rotated"
	OperatorAuditOAuthClientRedirectsChanged OperatorAuditKind = "oauth_client_redirects_changed"
	OperatorAuditOAuthClientDisabled         OperatorAuditKind = "oauth_client_disabled"
)

// The five statements; phase one of the read is beginOperatorReadSQL. Mirrored in
// db/queries/operator.sql.
const (
	registerOAuthClientSQL     = `SELECT public.op_register_oauth_client($1, $2, $3, $4, $5, $6)`
	rotateOAuthClientSecretSQL = `SELECT public.op_rotate_oauth_client_secret($1, $2, $3, $4)`
	setOAuthClientRedirectsSQL = `SELECT public.op_set_oauth_client_redirects($1, $2, $3)`
	disableOAuthClientSQL      = `SELECT public.op_disable_oauth_client($1, $2)`
	readOAuthClientsSQL        = `SELECT client_id, name, redirect_uris, allowed_scopes, secret_hint, secret_rotated_at,
       created_at, created_by, created_by_name, disabled_at
FROM public.op_read_oauth_clients($1, $2, $3, $4)`
)

// OAuthClientsPerPage is the registry page's size; op_begin_read refuses one outside 1..200 and
// op_read_oauth_clients bounds it in its body as well.
const OAuthClientsPerPage = 200

// OAuthClientRow is one row of op_read_oauth_clients -- a fixed column list (ADR 0021 §2 ii). The
// secret's hash is never in it; SecretHint is what the screen shows.
type OAuthClientRow struct {
	ClientID        uuid.UUID
	Name            string
	RedirectURIs    []string
	AllowedScopes   []string
	SecretHint      string
	SecretRotatedAt *time.Time
	CreatedAt       time.Time
	CreatedBy       uuid.UUID
	CreatedByName   *string
	DisabledAt      *time.Time
}

// RegisterOAuthClient is op_register_oauth_client: a new app and the operator's
// 'oauth_client_registered' row naming it, in one statement. The answer is the new app's id (its
// public client_id is "app_" + the id). Scopes are stored sorted and without repeats.
func RegisterOAuthClient(ctx context.Context, c OperatorConn, sessionHash, name string, redirectURIs, scopes []string,
	secretHash, secretHint string) (uuid.UUID, error) {
	var id uuid.UUID
	if err := c.QueryRow(ctx, registerOAuthClientSQL, sessionHash, name, redirectURIs, scopes, secretHash, secretHint).Scan(&id); err != nil {
		return uuid.Nil, operatorErr("register oauth client", err)
	}
	return id, nil
}

// RotateOAuthClientSecret is op_rotate_oauth_client_secret: the app's secret is replaced -- the old
// one is refused from the next request on -- unless the app is disabled; the operator's row on every
// accepted call. Issued tokens are not touched.
func RotateOAuthClientSecret(ctx context.Context, c OperatorConn, sessionHash string, clientID uuid.UUID, secretHash, secretHint string) error {
	_, err := c.Exec(ctx, rotateOAuthClientSecretSQL, sessionHash, clientID, secretHash, secretHint)
	return operatorErr("rotate oauth client secret", err)
}

// SetOAuthClientRedirects is op_set_oauth_client_redirects: the app's whole redirect list is
// replaced unless the app is disabled; the operator's row on every accepted call.
func SetOAuthClientRedirects(ctx context.Context, c OperatorConn, sessionHash string, clientID uuid.UUID, redirectURIs []string) error {
	_, err := c.Exec(ctx, setOAuthClientRedirectsSQL, sessionHash, clientID, redirectURIs)
	return operatorErr("set oauth client redirects", err)
}

// DisableOAuthClient is op_disable_oauth_client: the app is closed for every business, one way;
// the operator's row on every accepted call. No tenant row is written: every grant of the app stops
// resolving through the token resolver's client_disabled_at.
func DisableOAuthClient(ctx context.Context, c OperatorConn, sessionHash string, clientID uuid.UUID) error {
	_, err := c.Exec(ctx, disableOAuthClientSQL, sessionHash, clientID)
	return operatorErr("disable oauth client", err)
}

// OAuthClients is the two-phase read of ADR 0021 §2 v for the registry: op_begin_read writes the
// read's 'read' row (target_scope 'oauth_clients', the page, no tenant, detail {}) and a ticket bound
// to the session and the page; op_read_oauth_clients consumes it and returns the page, newest app
// first. page counts from 1. A dead session is ErrOperatorRefused, and so is a pgx.Tx (the two phases
// are two transactions only on a pool, as for Holdings).
func OAuthClients(ctx context.Context, c OperatorConn, sessionHash string, page int32) ([]OAuthClientRow, error) {
	params, err := json.Marshal(legalVersionsParams{PageNumber: page, PageSize: OAuthClientsPerPage})
	if err != nil {
		return nil, fmt.Errorf("db: oauth clients: encode the page: %w", err)
	}
	t, err := beginOperatorRead(ctx, c, sessionHash, oauthClientsReadKind, params)
	if err != nil {
		return nil, err
	}
	rows, err := c.Query(ctx, readOAuthClientsSQL, sessionHash, t.reveal(), page, int32(OAuthClientsPerPage))
	if err != nil {
		return nil, operatorErr("read oauth clients", err)
	}
	defer rows.Close()
	var out []OAuthClientRow
	for rows.Next() {
		var r OAuthClientRow
		if err := rows.Scan(&r.ClientID, &r.Name, &r.RedirectURIs, &r.AllowedScopes, &r.SecretHint, &r.SecretRotatedAt,
			&r.CreatedAt, &r.CreatedBy, &r.CreatedByName, &r.DisabledAt); err != nil {
			return nil, operatorErr("read oauth clients", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, operatorErr("read oauth clients", err)
	}
	return out, nil
}
