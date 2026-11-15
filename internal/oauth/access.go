package oauth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/store"
)

// THE /v1 HALF OF THE AUTHORIZATION SERVER (issue #37 phase 3; ADR 0047 §8.1, §11.4, §12): an access
// token's shape and its resolution for the Bearer chain, the grant's last use, and the owner's
// "Connected apps" list and Disconnect.
//
// CARRIED, NOT APPLIED -- then decided HERE, on every request, with no cache (§7.3: a Disconnect, a
// revocation and the operator closing the app are in force on the next request):
//
//	the shape (no database)                   not this build's tt_at_<env>_ + 43 -> ErrNotAnAccessToken
//	resolve_oauth_token_by_hash               unknown, not an access token, the token or its grant
//	                                          revoked, the app disabled -> ErrInvalidToken (401)
//	CheckOAuthAccessTokenLive (in the tenant)  expired by the DATABASE's clock -> ErrInvalidToken
//	the operator closed the API (§11.4)       api_enabled_at NULL and api_code set -> ErrAPIClosed
//	                                          (403 api_disabled). A never-opened K-22 is NOT read
//	                                          (OA-1): the grant is its own gate.
//
// 401 outranks 403 (the API key's rule: a dead credential is no credential at all).

// The access token's refusals (ADR 0047 §8.1 step 4). Fixed values -- never the token, its hash or the
// header.
var (
	// ErrNotAnAccessToken: the bearer value does not have this build's access token shape -- the
	// chain tries the API key's shape instead. Decided without the database.
	ErrNotAnAccessToken = errors.New("oauth: not an access token of this build's shape")
	// ErrInvalidToken: unknown, expired, revoked, of a revoked grant or of a disabled app -- one
	// answer for all (401 error="invalid_token"; no oracle for which tokens once existed).
	ErrInvalidToken = errors.New("oauth: the access token is invalid, expired or revoked")
	// ErrAPIClosed: the token is live and the operator closed this business's API (§11.4).
	ErrAPIClosed = errors.New("oauth: the operator closed this business's integration API")
)

// CheckedAccess is a bearer value of the access token's shape, carried as its keyed hash -- the
// resolver's input, as good as the token to anybody who can call the resolver, so it prints a
// placeholder (Digest).
type CheckedAccess struct{ d Digest }

// Access is who a live access token speaks for: the grant's tenant -- the ONE authority of every later
// query -- the grant, the app, the token and the token's OWN scopes (§8.1 step 8: a refresh may have
// narrowed them below the grant's).
type Access struct {
	TenantID uuid.UUID
	GrantID  uuid.UUID
	ClientID uuid.UUID
	TokenID  uuid.UUID
	Scopes   []string
}

// CheckAccess is the shape gate: this build's tt_at_<env>_ and 43 characters of canonical base64url,
// or ErrNotAnAccessToken -- with NO database work. It returns the keyed hash for ResolveAccess.
func (s *Server) CheckAccess(v Value) (CheckedAccess, error) {
	if !v.Is(KindAccess) {
		return CheckedAccess{}, ErrNotAnAccessToken
	}
	d, err := s.hasher.Hash(KindAccess, v)
	if err != nil {
		return CheckedAccess{}, err
	}
	return CheckedAccess{d: d}, nil
}

// ResolveAccess runs the token resolver and decides (the table above). A database failure is returned
// wrapped, naming nothing of the credential.
func (s *Server) ResolveAccess(ctx context.Context, c CheckedAccess) (Access, error) {
	if c.d.h == nil {
		return Access{}, ErrInvalidToken
	}
	t, err := s.store.GetOAuthTokenByHash(ctx, c.d.reveal())
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Access{}, ErrInvalidToken
	case err != nil:
		return Access{}, fmt.Errorf("oauth: resolve the access credential: %w", err)
	case t.Kind != "access", t.TokenRevokedAt != nil, t.GrantRevokedAt != nil, t.ClientDisabledAt != nil, len(t.TokenScopes) == 0:
		return Access{}, ErrInvalidToken
	}
	var live bool
	err = s.store.WithTenant(ctx, t.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		live, e = store.New(tx).CheckOAuthAccessTokenLive(ctx, store.CheckOAuthAccessTokenLiveParams{TenantID: t.TenantID, TokenID: t.TokenID})
		if errors.Is(e, pgx.ErrNoRows) {
			live = false
			return nil
		}
		return e
	})
	if err != nil {
		return Access{}, fmt.Errorf("oauth: check the access credential's lifetime: %w", err)
	}
	if !live {
		return Access{}, ErrInvalidToken
	}
	if t.APIEnabledAt == nil && t.APICode != nil {
		return Access{}, ErrAPIClosed
	}
	return Access{TenantID: t.TenantID, GrantID: t.GrantID, ClientID: t.ClientID, TokenID: t.TokenID, Scopes: Canonical(t.TokenScopes)}, nil
}

// TouchGrant writes the grant's last_used_at -- at most once a minute (TouchOAuthGrant's own guard),
// in the grant's tenant, never for a revoked grant. It reports whether it wrote.
func (s *Server) TouchGrant(ctx context.Context, tenantID, grantID uuid.UUID) (bool, error) {
	var n int64
	err := s.store.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		n, e = store.New(tx).TouchOAuthGrant(ctx, store.TouchOAuthGrantParams{TenantID: tenantID, ID: grantID})
		return e
	})
	if err != nil {
		return false, fmt.Errorf("oauth: touch the grant: %w", err)
	}
	return n == 1, nil
}

// ----------------------------------------------------------------------------- Connected apps --

// ErrUnknownGrant: the Disconnect names no live grant of the session's business (another business's
// id finds no row -- RLS and the explicit filter).
var ErrUnknownGrant = errors.New("oauth: no such connection in this business")

// reasonOwner is the panel's Disconnect (00104's closed set).
const reasonOwner = "owner"

// ConnectedApp is one live grant of the business as the owner's list shows it (ADR 0047 §12).
type ConnectedApp struct {
	GrantID  uuid.UUID
	ClientID uuid.UUID
	// AppName is the registry's name now ("" when the app cannot be resolved any more).
	AppName string
	// AppDisabled: the operator turned the app off -- "This app was turned off by Taptime" (§11.4).
	AppDisabled bool
	Scopes      []string
	// ApprovedBy is the approving membership's name; ApproverDisabled its "Approver disabled" pill
	// (the grant is the business's, not the person's -- §12, DÜŞÜK-7).
	ApprovedBy       string
	ApproverDisabled bool
	ConnectedAt      time.Time
	LastUsedAt       *time.Time
	// Expired: no live refresh token is left -- "Expired -- reconnect from the app" (the "Uygulama
	// notları").
	Expired bool
}

// ConnectedApps is the owner's list: the business's live grants and whether the operator closed the
// business's integrations (§11.4's banner). Owner-only: the role is re-read in the transaction.
type ConnectedApps struct {
	Apps []ConnectedApp
	// IntegrationsOff: "Integrations are turned off for this business by Taptime".
	IntegrationsOff bool
}

// ListConnectedApps reads the list in the session's tenant; ErrNotOwner for anybody but an active
// owner. The apps' names and states come from resolve_oauth_client (tappa_app reads nothing of
// oauth_clients), once per distinct app, after the transaction.
func (s *Server) ListConnectedApps(ctx context.Context, tenantID, adminID uuid.UUID) (ConnectedApps, error) {
	var out ConnectedApps
	err := s.store.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		owner, err := activeOwner(ctx, q, tenantID, adminID)
		if err != nil {
			return fmt.Errorf("read the actor: %w", err)
		}
		if !owner {
			return ErrNotOwner
		}
		gate, err := q.GetTenantAPIGate(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("read the integration gate: %w", err)
		}
		out.IntegrationsOff = operatorClosed(gate)
		rows, err := q.ListConnectedApps(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("list: %w", err)
		}
		out.Apps = make([]ConnectedApp, 0, len(rows))
		for _, r := range rows {
			out.Apps = append(out.Apps, ConnectedApp{
				GrantID: r.ID, ClientID: r.ClientID, Scopes: Canonical(r.Scopes), ApprovedBy: r.ApproverName,
				ApproverDisabled: r.ApproverStatus != "active", ConnectedAt: r.CreatedAt, LastUsedAt: r.LastUsedAt,
				Expired: !r.HasLiveRefresh,
			})
		}
		return nil
	})
	if errors.Is(err, ErrNotOwner) {
		return ConnectedApps{}, ErrNotOwner
	}
	if err != nil {
		return ConnectedApps{}, fmt.Errorf("oauth: connected apps: %w", err)
	}
	seen := map[uuid.UUID]ResolvedName{}
	for i, a := range out.Apps {
		n, ok := seen[a.ClientID]
		if !ok {
			n = s.nameOf(ctx, a.ClientID)
			seen[a.ClientID] = n
		}
		out.Apps[i].AppName, out.Apps[i].AppDisabled = n.Name, n.Disabled
	}
	return out, nil
}

// ResolvedName is an app as a list names it.
type ResolvedName struct {
	Name     string
	Disabled bool
}

// nameOf resolves one app for a list. A failed lookup is logged and leaves the name empty: the
// connection is still shown (and can still be disconnected) -- a list that dropped a row because the
// registry could not be read would hide a live connection.
func (s *Server) nameOf(ctx context.Context, id uuid.UUID) ResolvedName {
	r, err := s.store.GetOAuthClient(ctx, id, nil)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			s.log.WarnContext(ctx, "oauth: an app of the connected apps list could not be named", "client_id", ClientIDOf(id), "err", err)
		}
		return ResolvedName{}
	}
	return ResolvedName{Name: r.Name, Disabled: r.DisabledAt != nil}
}

// Disconnect is the owner's Disconnect (§12): in the session's tenant, the actor re-read as an active
// owner, the grant ended by ONE conditional statement (DisconnectOAuthGrant: reason 'owner', the
// membership that pressed it) -- every token of it stops resolving at once -- and the trail row
// oauth.grant_revoked with the membership as its actor, in the same transaction.
func (s *Server) Disconnect(ctx context.Context, tenantID, adminID, grantID uuid.UUID) error {
	err := s.store.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		owner, err := activeOwner(ctx, q, tenantID, adminID)
		if err != nil {
			return fmt.Errorf("read the actor: %w", err)
		}
		if !owner {
			return ErrNotOwner
		}
		g, err := q.GetOAuthGrantForPanel(ctx, store.GetOAuthGrantForPanelParams{TenantID: tenantID, ID: grantID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnknownGrant
		}
		if err != nil {
			return fmt.Errorf("read the connection: %w", err)
		}
		actor := adminID
		n, err := q.DisconnectOAuthGrant(ctx, store.DisconnectOAuthGrantParams{Actor: actor, ID: grantID, TenantID: tenantID})
		if err != nil {
			return fmt.Errorf("disconnect: %w", err)
		}
		if n == 0 {
			// Ended between the read and the write (a refresh reuse, the app's revoke).
			return ErrUnknownGrant
		}
		_, err = s.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: tenantID, ActorID: &actor, Action: ActionGrantRevoked, Target: grantID.String(),
			Detail: grantDetail{ClientID: ClientIDOf(g.ClientID), Reason: reasonOwner, Via: "panel"},
		})
		return err
	})
	switch {
	case errors.Is(err, ErrNotOwner), errors.Is(err, ErrUnknownGrant):
		return err
	case err != nil:
		return fmt.Errorf("oauth: disconnect: %w", err)
	}
	return nil
}
