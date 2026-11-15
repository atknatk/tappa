package oauth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// THE DATABASE WORK of the authorization server (ADR 0047 §4.5, §5.2, §5.3, §6): the consent's code,
// the code exchange, the refresh rotation and the revocation. Every statement after a resolver runs
// inside WithTenant(the tenant the credential resolved to -- or the panel session's) with an explicit
// tenant_id filter (db/queries/oauth.sql); the tenant is never a request's.
//
// 🔴 EVERY "IT HAPPENED" WRITE IS ONE CONDITIONAL STATEMENT (CLAUDE.md §4.4's principle): the code is
// spent by ConsumeOAuthCode, a refresh token used by RotateOAuthRefreshToken, each answering whether
// THIS call did it; nothing reads a state and writes it in a second step.
//
// 🔴 TWO CONTEXTS ON THE TOKEN ENDPOINT (§5.3, "D"): the work runs under ctx -- detached from the
// client and bounded by the endpoint's D = 10 s, so a disconnect cannot cut a statement in half --
// and live is the client's own request context. A transaction that MINTS (a new pair, a new grant) is
// rolled back when either has ended before its commit: no token is born and no used_at is written
// for a client that is gone. A transaction that only REFUSES (a spent code, a reuse revocation)
// commits under ctx alone, so the one-shot rule of a code (§5.2 step 3) and the revocation of a
// replayed credential cannot be undone by dropping the connection.

// Error is one of the token endpoint's refusals (RFC 6749 §5.2): the error word and the HTTP status
// it is answered with. A fixed value -- never the credential, never an input.
type Error struct {
	Code   string
	Status int
}

func (e *Error) Error() string { return "oauth: " + e.Code }

// The token endpoint's refusals. errors.Is identifies them; anything else is a 503.
var (
	ErrInvalidRequest       = &Error{"invalid_request", http.StatusBadRequest}
	ErrInvalidClient        = &Error{"invalid_client", http.StatusUnauthorized}
	ErrInvalidGrant         = &Error{"invalid_grant", http.StatusBadRequest}
	ErrUnsupportedGrantType = &Error{"unsupported_grant_type", http.StatusBadRequest}
	ErrInvalidScope         = &Error{"invalid_scope", http.StatusBadRequest}
)

// The consent's refusals (§4.4, §11.2, §11.4): the approver is not an active owner, the business is
// suspended, the operator turned its integrations off.
var (
	ErrNotOwner        = errors.New("oauth: only an active owner of the business approves an app")
	ErrSuspended       = errors.New("oauth: the business is suspended")
	ErrIntegrationsOff = errors.New("oauth: the operator turned this business's integrations off")
)

// errClientGone is a minting transaction's rollback because the client left or D ran out (never
// answered: the client is not there to read it, or the endpoint answers 503).
var errClientGone = errors.New("oauth: the request ended before its commit")

// ErrEnded is what the token endpoint gets when a transaction was rolled back because the request's
// context ended (the client left or D elapsed): nothing was minted.
var ErrEnded = errClientGone

// The audit actions (ADR 0047 §5.2 step 7, §5.3 step 4, §6). Free text by schema (00005).
const (
	ActionGrantCreated      = "oauth.grant_created"
	ActionGrantRevoked      = "oauth.grant_revoked"
	ActionRefreshReuse      = "oauth.refresh_reuse_detected"
	ActionCodeReuseDetected = "oauth.code_reuse_detected"
)

// The grant revocation reasons a person does not write (00104's closed set; 'owner' is the panel's
// Disconnect -- access.go's reasonOwner).
const (
	reasonClient       = "client"
	reasonSuperseded   = "superseded"
	reasonRefreshReuse = "refresh_reuse"
	reasonCodeReuse    = "code_reuse"
)

// viaOAuth marks the trail rows of this package (the API's detail.via is "api", §8.7; these rows are
// written by the authorization server itself).
const viaOAuth = "oauth"

// liveGrantIndex is the partial UNIQUE (tenant_id, client_id) WHERE revoked_at IS NULL (00104, O-8):
// two exchanges racing for one (business, app) meet there and the loser rolls back.
const liveGrantIndex = "oauth_grants_live_client_key"

// Store is the slice of internal/db the server needs (*db.DB), declared at the consumer (§7).
type Store interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
	GetOAuthClient(ctx context.Context, clientID uuid.UUID, secretHash *string) (db.ResolvedOAuthClient, error)
	GetOAuthCodeByHash(ctx context.Context, codeHash string) (db.ResolvedOAuthCode, error)
	GetOAuthTokenByHash(ctx context.Context, tokenHash string) (db.ResolvedOAuthToken, error)
}

// Trail is the audit recorder's in-transaction write (*audit.Recorder).
type Trail interface {
	RecordTx(ctx context.Context, tx pgx.Tx, e audit.Event) (uuid.UUID, error)
}

// Server is the authorization server's database work. Dependencies are fields (§7).
type Server struct {
	hasher *Hasher
	store  Store
	trail  Trail
	log    *slog.Logger
	// afterConsume runs inside the exchange's transaction right after this call spent a code (nil in
	// production): the test that holds the row lock open to make the "concurrent presentation is not
	// a reuse" branch deterministic (export_test.go).
	afterConsume func()
}

// NewServer builds the server over TAPPA_OAUTH_HMAC_KEY. A nil dependency is refused.
func NewServer(hmacKey []byte, s Store, t Trail, log *slog.Logger) (*Server, error) {
	h, err := NewHasher(hmacKey)
	if err != nil {
		return nil, err
	}
	if s == nil || t == nil {
		return nil, errors.New("oauth: nil store or trail")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Server{hasher: h, store: s, trail: t, log: log}, nil
}

// Hasher is the server's keyed hash -- the consent page signs its pending request with it.
func (s *Server) Hasher() *Hasher { return s.hasher }

// clientOf maps a resolver row.
func clientOf(r db.ResolvedOAuthClient) Client {
	return Client{ID: r.ID, Name: r.Name, RedirectURIs: r.RedirectURIs, AllowedScopes: Canonical(r.AllowedScopes), Disabled: r.DisabledAt != nil}
}

// LookupClient resolves a client by its registry id with no secret (the consent flow's V1). A
// disabled client is returned with Disabled set; no row is ErrNoClient.
func (s *Server) LookupClient(ctx context.Context, id uuid.UUID) (Client, error) {
	r, err := s.store.GetOAuthClient(ctx, id, nil)
	if errors.Is(err, pgx.ErrNoRows) {
		return Client{}, ErrNoClient
	}
	if err != nil {
		return Client{}, fmt.Errorf("oauth: lookup client: %w", err)
	}
	return clientOf(r), nil
}

// AuthenticateClient is the token endpoint's client authentication (§5.1): the secret's shape (no
// database), then its hash compared by the resolver (secret_ok -- the stored hash never leaves the
// database). Unknown client, wrong secret and disabled client are one answer: ErrInvalidClient.
func (s *Server) AuthenticateClient(ctx context.Context, id uuid.UUID, secret Value) (Client, error) {
	if !secret.Is(KindSecret) {
		return Client{}, ErrInvalidClient
	}
	h, err := s.hasher.Hash(KindSecret, secret)
	if err != nil {
		return Client{}, err
	}
	hash := h.reveal()
	r, err := s.store.GetOAuthClient(ctx, id, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Client{}, ErrInvalidClient
	}
	if err != nil {
		return Client{}, fmt.Errorf("oauth: authenticate client: %w", err)
	}
	if r.SecretOK == nil || !*r.SecretOK || r.DisabledAt != nil {
		return Client{}, ErrInvalidClient
	}
	return clientOf(r), nil
}

// operatorClosed is ADR 0047 §11.4's reading of K-22's two columns: the operator closed the
// business's API when api_enabled_at is NULL and api_code is not.
func operatorClosed(g store.GetTenantAPIGateRow) bool {
	return g.ApiEnabledAt == nil && g.ApiCode != nil
}

// activeOwner reads the membership in the transaction (§4.4: the role is re-read where it is used).
func activeOwner(ctx context.Context, q *store.Queries, tenantID, adminID uuid.UUID) (bool, error) {
	a, err := q.GetAdminByID(ctx, store.GetAdminByIDParams{ID: adminID, TenantID: tenantID})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return a.Role == "owner" && a.Status == "active", nil
}

// IntegrationsOff reports whether the operator closed the business's API (§11.4) -- the consent page's
// "Integrations are turned off for this business by Taptime". A read in the business's own context.
func (s *Server) IntegrationsOff(ctx context.Context, tenantID uuid.UUID) (bool, error) {
	var off bool
	err := s.store.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		g, err := store.New(tx).GetTenantAPIGate(ctx, tenantID)
		if err != nil {
			return err
		}
		off = operatorClosed(g)
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("oauth: read the integration gate: %w", err)
	}
	return off, nil
}

// Approve is the consent POST's one write (§4.5, the "Uygulama notları"): inside the panel session's
// tenant, the approving membership is re-read (an active owner, item 10), the business must not be
// suspended (the suspension ring has already refused -- this is the belt) nor closed by the operator
// (§11.4), and the code is inserted -- 60 seconds, bound to the client, the matched registered URI,
// the scopes, the challenge and the approver. The caller has re-run V1-V7 against the live client.
func (s *Server) Approve(ctx context.Context, tenantID, adminID uuid.UUID, r Request) (Value, error) {
	code, err := Mint(KindCode)
	if err != nil {
		return Value{}, err
	}
	h, err := s.hasher.Hash(KindCode, code)
	if err != nil {
		return Value{}, err
	}
	err = s.store.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		owner, err := activeOwner(ctx, q, tenantID, adminID)
		if err != nil {
			return fmt.Errorf("read the approver: %w", err)
		}
		if !owner {
			return ErrNotOwner
		}
		suspended, err := q.GetTenantSuspended(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("read the suspension: %w", err)
		}
		if suspended {
			return ErrSuspended
		}
		gate, err := q.GetTenantAPIGate(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("read the integration gate: %w", err)
		}
		if operatorClosed(gate) {
			return ErrIntegrationsOff
		}
		if _, err := q.InsertOAuthCode(ctx, store.InsertOAuthCodeParams{
			TenantID: tenantID, CodeHash: h.reveal(), ClientID: r.ClientID, CreatedBy: adminID,
			RedirectUri: r.RedirectURI, Scopes: r.Scopes, CodeChallenge: r.Challenge,
		}); err != nil {
			return fmt.Errorf("insert the code: %w", err)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrNotOwner) || errors.Is(err, ErrSuspended) || errors.Is(err, ErrIntegrationsOff) {
			return Value{}, err
		}
		return Value{}, fmt.Errorf("oauth: approve: %w", err)
	}
	return code, nil
}

// Tokens is a freshly minted pair (§5.4).
type Tokens struct {
	Access    Value
	Refresh   Value
	ExpiresIn int64
	Scopes    []string
}

// mintPair inserts one access token (scopes: the given set) and one refresh token (scopes NULL: it
// always carries the grant's -- §5.3) for the grant.
func (s *Server) mintPair(ctx context.Context, q *store.Queries, tenantID, grantID uuid.UUID, scopes []string) (Tokens, error) {
	at, err := Mint(KindAccess)
	if err != nil {
		return Tokens{}, err
	}
	rt, err := Mint(KindRefresh)
	if err != nil {
		return Tokens{}, err
	}
	ah, err := s.hasher.Hash(KindAccess, at)
	if err != nil {
		return Tokens{}, err
	}
	rh, err := s.hasher.Hash(KindRefresh, rt)
	if err != nil {
		return Tokens{}, err
	}
	row, err := q.InsertOAuthToken(ctx, store.InsertOAuthTokenParams{TenantID: tenantID, GrantID: grantID, Kind: "access", TokenHash: ah.reveal(), Scopes: scopes})
	if err != nil {
		return Tokens{}, fmt.Errorf("insert the access row: %w", err)
	}
	if _, err := q.InsertOAuthToken(ctx, store.InsertOAuthTokenParams{TenantID: tenantID, GrantID: grantID, Kind: "refresh", TokenHash: rh.reveal()}); err != nil {
		return Tokens{}, fmt.Errorf("insert the refresh row: %w", err)
	}
	return Tokens{Access: at, Refresh: rt, ExpiresIn: max(row.ExpiresIn, 0), Scopes: scopes}, nil
}

// beforeMintCommit is the D check of a minting transaction: its error rolls the transaction back.
func beforeMintCommit(ctx, live context.Context) error {
	if ctx.Err() != nil || (live != nil && live.Err() != nil) {
		return errClientGone
	}
	return nil
}

// grantDetail is the trail row of a grant's birth or end. Ids and fixed words only.
type grantDetail struct {
	ClientID   string   `json:"client_id"`
	Scopes     []string `json:"scopes,omitempty"`
	Superseded []string `json:"superseded,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	Via        string   `json:"via"`
}

// revokeGrant ends a grant for a reason no person writes and, when THIS statement ended it, leaves
// the trail row action names.
func (s *Server) revokeGrant(ctx context.Context, tx pgx.Tx, tenantID, grantID, clientID uuid.UUID, reason, action string) error {
	n, err := store.New(tx).RevokeOAuthGrant(ctx, store.RevokeOAuthGrantParams{Reason: reason, TenantID: tenantID, ID: grantID})
	if err != nil {
		return fmt.Errorf("revoke the grant: %w", err)
	}
	if n == 0 {
		return nil
	}
	_, err = s.trail.RecordTx(ctx, tx, audit.Event{
		TenantID: tenantID, Action: action, Target: grantID.String(),
		Detail: grantDetail{ClientID: ClientIDOf(clientID), Reason: reason, Via: viaOAuth},
	})
	return err
}

// ExchangeCode is grant_type=authorization_code (§5.2), for the AUTHENTICATED client c.
//
//	1 the code's shape                                   invalid_grant, no database
//	2 resolve_oauth_code_by_hash                         unknown -> invalid_grant
//	3 the code's client is c                             else invalid_grant and NOTHING is touched:
//	                                                     another registered app holding a leaked
//	                                                     code cannot spend it or revoke the grant it
//	                                                     bore (the 1st phase's security finding)
//	4 ConsumeOAuthCode, one statement                    row locked (a concurrent exchange) ->
//	                                                     invalid_grant, nothing touched; free and
//	                                                     already spent with a grant -> that grant is
//	                                                     revoked (code_reuse), committed; spent now ->
//	5 redirect_uri (when sent), PKCE                     mismatch -> the consumption commits,
//	6 suspended, operator-closed, approver not an        invalid_grant (one shot per code)
//	  active owner
//	7 supersede, insert the grant, bind the code, the    a racing exchange for the same (business,
//	  pair, the trail row                                app) loses on the partial UNIQUE: rolled back
func (s *Server) ExchangeCode(ctx, live context.Context, c Client, code Value, verifier string, redirectURI *string) (Tokens, error) {
	if !code.Is(KindCode) {
		return Tokens{}, ErrInvalidGrant
	}
	h, err := s.hasher.Hash(KindCode, code)
	if err != nil {
		return Tokens{}, err
	}
	rc, err := s.store.GetOAuthCodeByHash(ctx, h.reveal())
	if errors.Is(err, pgx.ErrNoRows) {
		return Tokens{}, ErrInvalidGrant
	}
	if err != nil {
		return Tokens{}, fmt.Errorf("oauth: exchange: %w", err)
	}
	if rc.ClientID != c.ID {
		return Tokens{}, ErrInvalidGrant
	}
	var out Tokens
	var refusal error
	err = s.store.WithTenant(ctx, rc.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		row, err := q.ConsumeOAuthCode(ctx, store.ConsumeOAuthCodeParams{CodeID: rc.CodeID, TenantID: rc.TenantID})
		if err != nil {
			return fmt.Errorf("consume the code: %w", err)
		}
		if !row.Reached {
			// A concurrent exchange of the same code holds the row: not a reuse, nothing touched.
			return ErrInvalidGrant
		}
		if !row.Spent {
			refusal = ErrInvalidGrant
			if row.PriorConsumedAt != nil && row.PriorGrantID != nil {
				// A sequential presentation of a code that bore a grant (§5.2 step 4, RFC 6749
				// §4.1.2's SHOULD): the grant ends, and it commits.
				return s.revokeGrant(ctx, tx, rc.TenantID, *row.PriorGrantID, c.ID, reasonCodeReuse, ActionCodeReuseDetected)
			}
			return nil
		}
		if s.afterConsume != nil {
			s.afterConsume()
		}
		// From here every refusal COMMITS the consumption (one shot per code).
		if row.ClientID == nil || *row.ClientID != c.ID || row.RedirectUri == nil || row.CodeChallenge == nil || row.CreatedBy == nil {
			refusal = ErrInvalidGrant
			return nil
		}
		if redirectURI != nil && *redirectURI != *row.RedirectUri {
			refusal = ErrInvalidGrant
			return nil
		}
		if !VerifyS256(verifier, *row.CodeChallenge) {
			refusal = ErrInvalidGrant
			return nil
		}
		suspended, err := q.GetTenantSuspended(ctx, rc.TenantID)
		if err != nil {
			return fmt.Errorf("read the suspension: %w", err)
		}
		gate, err := q.GetTenantAPIGate(ctx, rc.TenantID)
		if err != nil {
			return fmt.Errorf("read the integration gate: %w", err)
		}
		owner, err := activeOwner(ctx, q, rc.TenantID, *row.CreatedBy)
		if err != nil {
			return fmt.Errorf("read the approver: %w", err)
		}
		if suspended || operatorClosed(gate) || !owner {
			refusal = ErrInvalidGrant
			return nil
		}
		superseded, err := q.SupersedeLiveOAuthGrants(ctx, store.SupersedeLiveOAuthGrantsParams{TenantID: rc.TenantID, ClientID: c.ID})
		if err != nil {
			return fmt.Errorf("supersede: %w", err)
		}
		g, err := q.InsertOAuthGrant(ctx, store.InsertOAuthGrantParams{TenantID: rc.TenantID, ClientID: c.ID, Scopes: row.Scopes, CreatedBy: *row.CreatedBy})
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == liveGrantIndex {
			// Two codes of one (business, app) raced; this one lost. Rolled back: the code stays
			// unconsumed and may be tried again within its 60 seconds (§5.2 step 7).
			return ErrInvalidGrant
		}
		if err != nil {
			return fmt.Errorf("insert the grant: %w", err)
		}
		if n, err := q.SetOAuthCodeGrant(ctx, store.SetOAuthCodeGrantParams{GrantID: g.ID, TenantID: rc.TenantID, CodeID: rc.CodeID}); err != nil || n != 1 {
			if err == nil {
				err = errors.New("the code was not bound to its grant")
			}
			return fmt.Errorf("bind the code: %w", err)
		}
		out, err = s.mintPair(ctx, q, rc.TenantID, g.ID, row.Scopes)
		if err != nil {
			return err
		}
		for _, old := range superseded {
			if _, err := s.trail.RecordTx(ctx, tx, audit.Event{
				TenantID: rc.TenantID, Action: ActionGrantRevoked, Target: old.String(),
				Detail: grantDetail{ClientID: ClientIDOf(c.ID), Reason: reasonSuperseded, Via: viaOAuth},
			}); err != nil {
				return err
			}
		}
		supersededIDs := make([]string, 0, len(superseded))
		for _, old := range superseded {
			supersededIDs = append(supersededIDs, old.String())
		}
		if _, err := s.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: rc.TenantID, ActorID: row.CreatedBy, Action: ActionGrantCreated, Target: g.ID.String(),
			Detail: grantDetail{ClientID: ClientIDOf(c.ID), Scopes: row.Scopes, Superseded: supersededIDs, Via: viaOAuth},
		}); err != nil {
			return err
		}
		if _, err := q.DeleteExpiredOAuthCodes(ctx, rc.TenantID); err != nil {
			return fmt.Errorf("lazy delete: %w", err)
		}
		return beforeMintCommit(ctx, live)
	})
	switch {
	case errors.Is(err, ErrInvalidGrant):
		return Tokens{}, ErrInvalidGrant
	case errors.Is(err, errClientGone):
		return Tokens{}, errClientGone
	case err != nil:
		return Tokens{}, fmt.Errorf("oauth: exchange: %w", err)
	case refusal != nil:
		return Tokens{}, refusal
	}
	return out, nil
}

// Refresh is grant_type=refresh_token (§5.3), for the AUTHENTICATED client c. scope nil: the new
// access token carries the grant's scopes; else the requested set (already parsed -- non-empty, known
// words), which must be within the grant's.
//
//	1 shape, resolve; a refresh token of c's            else invalid_grant, nothing touched
//	2 the grant's row, FOR UPDATE                       revoked -> invalid_grant
//	3 RotateOAuthRefreshToken, one statement            a row -> the new pair (normal rotation)
//	4 used, within 30 s of its first use                every OTHER live token of the grant is
//	                                                    revoked, one new pair (the tolerance)
//	  used, past 30 s                                   the grant is revoked (refresh_reuse) and
//	                                                    its trail row written -- committed;
//	                                                    invalid_grant
//	5 revoked or expired                                invalid_grant, the grant lives
func (s *Server) Refresh(ctx, live context.Context, c Client, refresh Value, scope []string) (Tokens, error) {
	if !refresh.Is(KindRefresh) {
		return Tokens{}, ErrInvalidGrant
	}
	h, err := s.hasher.Hash(KindRefresh, refresh)
	if err != nil {
		return Tokens{}, err
	}
	rt, err := s.store.GetOAuthTokenByHash(ctx, h.reveal())
	if errors.Is(err, pgx.ErrNoRows) {
		return Tokens{}, ErrInvalidGrant
	}
	if err != nil {
		return Tokens{}, fmt.Errorf("oauth: refresh: %w", err)
	}
	if rt.Kind != "refresh" || rt.ClientID != c.ID {
		return Tokens{}, ErrInvalidGrant
	}
	var out Tokens
	var refusal error
	// tolerated is the tolerance branch's revocation count, logged only once the transaction has
	// COMMITTED (2nd phase review, DÜŞÜK): a wide scope or a client gone rolls the revocation back,
	// and a line saying "revoked N" for a rollback would be a false record.
	tolerated := int64(-1)
	err = s.store.WithTenant(ctx, rt.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		tolerated = -1
		q := store.New(tx)
		g, err := q.LockLiveOAuthGrant(ctx, store.LockLiveOAuthGrantParams{TenantID: rt.TenantID, GrantID: rt.GrantID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidGrant
		}
		if err != nil {
			return fmt.Errorf("lock the grant: %w", err)
		}
		_, err = q.RotateOAuthRefreshToken(ctx, store.RotateOAuthRefreshTokenParams{TenantID: rt.TenantID, TokenID: rt.TokenID})
		switch {
		case err == nil:
			// Normal rotation: the old access token lives out its hour (in-flight requests).
		case errors.Is(err, pgx.ErrNoRows):
			st, err := q.GetOAuthRefreshTokenState(ctx, store.GetOAuthRefreshTokenStateParams{TenantID: rt.TenantID, TokenID: rt.TokenID})
			if err != nil {
				return fmt.Errorf("read the refresh row: %w", err)
			}
			switch {
			case st.WithinTolerance:
				n, err := q.RevokeOtherLiveOAuthTokens(ctx, store.RevokeOtherLiveOAuthTokensParams{TenantID: rt.TenantID, GrantID: rt.GrantID})
				if err != nil {
					return fmt.Errorf("the tolerance's revocation: %w", err)
				}
				tolerated = n
			case st.UsedAt != nil && st.RevokedAt == nil:
				refusal = ErrInvalidGrant
				s.log.WarnContext(ctx, "oauth: a used refresh credential was presented past the tolerance; the grant is revoked",
					"tenant_id", rt.TenantID, "grant_id", rt.GrantID, "client_id", ClientIDOf(c.ID))
				return s.revokeGrant(ctx, tx, rt.TenantID, rt.GrantID, c.ID, reasonRefreshReuse, ActionRefreshReuse)
			default:
				return ErrInvalidGrant
			}
		default:
			return fmt.Errorf("rotate: %w", err)
		}
		// The scope is checked AFTER the reuse branches: a replayed token past the window ends the
		// grant whatever scope it asks for (a wide scope must not buy a replay an invalid_scope that
		// leaves the grant alive). A refusal here rolls the rotation back -- nothing is used.
		accessScopes := Canonical(g.Scopes)
		if scope != nil {
			if !SubsetOf(scope, g.Scopes) {
				return ErrInvalidScope
			}
			accessScopes = Canonical(scope)
		}
		out, err = s.mintPair(ctx, q, rt.TenantID, rt.GrantID, accessScopes)
		if err != nil {
			return err
		}
		if _, err := q.DeleteExpiredOAuthTokensForGrant(ctx, store.DeleteExpiredOAuthTokensForGrantParams{TenantID: rt.TenantID, GrantID: rt.GrantID}); err != nil {
			return fmt.Errorf("lazy delete: %w", err)
		}
		return beforeMintCommit(ctx, live)
	})
	switch {
	case errors.Is(err, ErrInvalidGrant):
		return Tokens{}, ErrInvalidGrant
	case errors.Is(err, ErrInvalidScope):
		return Tokens{}, ErrInvalidScope
	case errors.Is(err, errClientGone):
		return Tokens{}, errClientGone
	case err != nil:
		return Tokens{}, fmt.Errorf("oauth: refresh: %w", err)
	case refusal != nil:
		return Tokens{}, refusal
	}
	if tolerated >= 0 {
		s.log.InfoContext(ctx, "oauth: a refresh credential was presented again within the tolerance",
			"tenant_id", rt.TenantID, "grant_id", rt.GrantID, "client_id", ClientIDOf(c.ID), "revoked", tolerated)
	}
	return out, nil
}

// Revoke is RFC 7009 for the AUTHENTICATED client c (§6). Every outcome the client can see is "200":
// an unknown, malformed or already revoked token, and another client's token alike -- refusing the
// last would tell c that the token exists elsewhere (§18.2 item 14). A refresh token ends its grant
// (every token of it, 'client', the trail row); an access token ends alone -- the connection lives
// (§18.2 item 13). An error is returned only for a database failure.
func (s *Server) Revoke(ctx context.Context, c Client, tok Value) error {
	k, ok := tok.KindOf()
	if !ok || (k != KindAccess && k != KindRefresh) {
		return nil
	}
	h, err := s.hasher.Hash(k, tok)
	if err != nil {
		return err
	}
	rt, err := s.store.GetOAuthTokenByHash(ctx, h.reveal())
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("oauth: revoke: %w", err)
	}
	if rt.ClientID != c.ID || rt.Kind != map[Kind]string{KindAccess: "access", KindRefresh: "refresh"}[k] {
		return nil
	}
	err = s.store.WithTenant(ctx, rt.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		if k == KindRefresh {
			return s.revokeGrant(ctx, tx, rt.TenantID, rt.GrantID, c.ID, reasonClient, ActionGrantRevoked)
		}
		_, err := store.New(tx).RevokeOAuthAccessToken(ctx, store.RevokeOAuthAccessTokenParams{TenantID: rt.TenantID, TokenID: rt.TokenID})
		return err
	})
	if err != nil {
		return fmt.Errorf("oauth: revoke: %w", err)
	}
	return nil
}
