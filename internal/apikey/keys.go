package apikey

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/store"
)

// THE OWNER'S PANEL ACTS (ADR 0028 §3): create (the raw key returned ONCE), list, revoke one,
// revoke all. Each runs in the actor's tenant context, filters by tenant_id explicitly (§4.5),
// re-reads the actor's role from admin_users inside its own transaction -- the handler's gate is
// not taken on its word (internal/domain/tenant's mayEditBrand precedent) -- and writes its audit
// row in the SAME transaction (RecordTx): a key revoked with no trail, or a trail saying a key
// was revoked while it still resolves, would both be false statements.

// Audit actions. detail.via is "panel" for these; an act through the API itself is "api" (K-23,
// Via).
const (
	ActionCreated = "api_key.created"
	ActionRevoked = "api_key.revoked"
	viaPanel      = "panel"
	// NameLimit is the longest name a key may have, in characters (00051's CHECK).
	NameLimit = 64
	ownerRole = "owner"
	liveAdmin = "active"
	// liveNameIndex is 00051's partial UNIQUE index: two live keys of a business cannot share a
	// name. The violation is recognised by this name.
	liveNameIndex = "api_keys_live_name_key"
)

// The panel acts' refusals.
var (
	// ErrNotPermitted: the actor is not an active owner of this business.
	ErrNotPermitted = errors.New("api key: only an active owner manages integration keys")
	// ErrName: the name is empty, too long or holds a control character.
	ErrName = errors.New("api key: a key's name is 1 to 64 characters with no control character")
	// ErrNameTaken: a live key of this business already has the name (case aside) -- also what a
	// re-posted creation form gets, so a reload issues no second key.
	ErrNameTaken = errors.New("api key: a live key of this business already has that name")
	// ErrUnknownKey: no key of this business has that id.
	ErrUnknownKey = errors.New("api key: no such key in this business")
	// ErrAlreadyRevoked: the key was revoked before this request -- by another request or earlier.
	ErrAlreadyRevoked = errors.New("api key: the key was already revoked")
)

// Trail is RecordTx, declared at the consumer (§7).
type Trail interface {
	RecordTx(ctx context.Context, tx pgx.Tx, e audit.Event) (uuid.UUID, error)
}

// Keys is the panel acts' service.
type Keys struct {
	prefix string
	key    []byte
	data   Database
	trail  Trail
}

// NewKeys builds the service for the deployment's prefix and HMAC key.
func NewKeys(prefix string, hmacKey []byte, data Database, trail Trail) (*Keys, error) {
	if err := validPrefix(prefix); err != nil {
		return nil, err
	}
	if len(hmacKey) != hmacKeyLen {
		return nil, fmt.Errorf("api key: hmac key must be %d bytes, got %d", hmacKeyLen, len(hmacKey))
	}
	if data == nil || trail == nil {
		return nil, errors.New("api key: nil database or trail")
	}
	return &Keys{prefix: prefix, key: append([]byte(nil), hmacKey...), data: data, trail: trail}, nil
}

// Key is one row of the owner's list. No hash, no secret: the hint is all that names it.
type Key struct {
	ID         uuid.UUID
	Name       string
	PrefixHint string
	CreatedBy  uuid.UUID
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

// Listing is the section's read: the business's keys, newest first, and whether the operator has
// opened its integration API (K-22) -- a new key needs it open, a revocation never does.
type Listing struct {
	Keys       []Key
	APIEnabled bool
}

// CreateCommand is the owner's "create a key". The actor and the tenant come from the session.
type CreateCommand struct {
	TenantID uuid.UUID
	ActorID  uuid.UUID
	Name     string
}

// Created is the one answer that carries the raw key. The handler shows Key to the owner once and
// nothing stores it.
type Created struct {
	Key        Token
	ID         uuid.UUID
	Name       string
	PrefixHint string
	CreatedAt  time.Time
}

// RevokeCommand is the owner's "revoke this key".
type RevokeCommand struct {
	TenantID uuid.UUID
	ActorID  uuid.UUID
	KeyID    uuid.UUID
}

// Revoked is one key a revocation stopped.
type Revoked struct {
	ID         uuid.UUID
	Name       string
	PrefixHint string
	RevokedAt  time.Time
}

// createdDetail and revokedDetail are the trail rows' payloads: the key's id, name and hint --
// never the key, never its hash (ADR 0028 §3) -- and the K-23 attribution.
type createdDetail struct {
	Name       string `json:"name"`
	PrefixHint string `json:"prefix_hint"`
	Via        string `json:"via"`
}

type revokedDetail struct {
	Name       string `json:"name"`
	PrefixHint string `json:"prefix_hint"`
	Bulk       bool   `json:"bulk"`
	Via        string `json:"via"`
}

// ValidName trims and checks a key's name; ErrName when it is not one.
func ValidName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > NameLimit {
		return "", ErrName
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", ErrName
		}
	}
	return name, nil
}

// mayManage is the domain's role gate: an active owner of THIS tenant, read inside the write's
// own transaction and context. Another tenant's admin id finds no row.
func mayManage(ctx context.Context, q *store.Queries, tenantID, actorID uuid.UUID) error {
	a, err := q.GetAdminByID(ctx, store.GetAdminByIDParams{ID: actorID, TenantID: tenantID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotPermitted
	}
	if err != nil {
		return fmt.Errorf("read the actor: %w", err)
	}
	if a.Role != ownerRole || a.Status != liveAdmin {
		return ErrNotPermitted
	}
	return nil
}

// List reads the business's keys and its API gate. Owner-only, like every act here.
func (k *Keys) List(ctx context.Context, tenantID, actorID uuid.UUID) (Listing, error) {
	var out Listing
	err := k.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if err := mayManage(ctx, q, tenantID, actorID); err != nil {
			return err
		}
		enabled, err := q.GetTenantAPIEnabledAt(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("read the gate: %w", err)
		}
		out.APIEnabled = enabled != nil
		rows, err := q.ListAPIKeys(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("list: %w", err)
		}
		out.Keys = make([]Key, 0, len(rows))
		for _, r := range rows {
			out.Keys = append(out.Keys, Key{ID: r.ID, Name: r.Name, PrefixHint: r.PrefixHint, CreatedBy: r.CreatedBy,
				CreatedAt: r.CreatedAt, LastUsedAt: r.LastUsedAt, RevokedAt: r.RevokedAt})
		}
		return nil
	})
	if err != nil {
		return Listing{}, pass(err, "list")
	}
	return out, nil
}

// Create issues a key: the name checked, the actor an active owner, the business's API open
// (K-22: "kapı her yolda"), a new random key minted and only its keyed hash and hint stored, and
// the trail row -- all in one transaction. The raw key is in the answer and nowhere else.
func (k *Keys) Create(ctx context.Context, c CreateCommand) (Created, error) {
	name, err := ValidName(c.Name)
	if err != nil {
		return Created{}, err
	}
	tok, err := mint(k.prefix)
	if err != nil {
		return Created{}, err
	}
	v := tok.reveal()
	h, err := keyedHash(k.key, v)
	if err != nil {
		return Created{}, err
	}
	out := Created{Key: tok, Name: name, PrefixHint: hint(v, k.prefix)}
	err = k.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if err := mayManage(ctx, q, c.TenantID, c.ActorID); err != nil {
			return err
		}
		enabled, err := q.GetTenantAPIEnabledAt(ctx, c.TenantID)
		if err != nil {
			return fmt.Errorf("read the gate: %w", err)
		}
		if enabled == nil {
			return ErrAPIClosed
		}
		row, err := q.InsertAPIKey(ctx, store.InsertAPIKeyParams{
			TenantID: c.TenantID, Name: name, TokenHash: h, PrefixHint: out.PrefixHint, CreatedBy: c.ActorID,
		})
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == liveNameIndex {
			return ErrNameTaken
		}
		if err != nil {
			// The driver's error names the constraint at most; the hash is a bind parameter and
			// pgx does not quote parameters into its errors.
			return fmt.Errorf("insert: %w", err)
		}
		out.ID, out.CreatedAt = row.ID, row.CreatedAt
		_, err = k.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID, ActorID: &c.ActorID, Action: ActionCreated, Target: row.ID.String(),
			Detail: createdDetail{Name: name, PrefixHint: out.PrefixHint, Via: viaPanel},
		})
		return err
	})
	if err != nil {
		return Created{}, pass(err, "create")
	}
	return out, nil
}

// Revoke stops one key, at once and for good (the next request carrying it is 401). Only the
// request whose UPDATE revoked it writes the trail row: a second, racing revocation matches no row
// (`revoked_at IS NULL`, 00011's BOUNDARY 2) and answers ErrAlreadyRevoked without writing one.
// The business's API gate is not consulted -- revoking is a defence and stays open (K-26).
func (k *Keys) Revoke(ctx context.Context, c RevokeCommand) (Revoked, error) {
	var out Revoked
	err := k.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if err := mayManage(ctx, q, c.TenantID, c.ActorID); err != nil {
			return err
		}
		row, err := q.RevokeAPIKey(ctx, store.RevokeAPIKeyParams{TenantID: c.TenantID, ID: c.KeyID})
		if errors.Is(err, pgx.ErrNoRows) {
			at, err := q.GetAPIKeyRevokedAt(ctx, store.GetAPIKeyRevokedAtParams{TenantID: c.TenantID, ID: c.KeyID})
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				return ErrUnknownKey
			case err != nil:
				return fmt.Errorf("read the key: %w", err)
			case at != nil:
				return ErrAlreadyRevoked
			}
			// A live key the guarded UPDATE did not match cannot happen under one tenant
			// context; say so rather than claim a revocation.
			return errors.New("api key: the key is live and the revocation matched no row")
		}
		if err != nil {
			return fmt.Errorf("revoke: %w", err)
		}
		out = revoked(row.ID, row.Name, row.PrefixHint, row.RevokedAt)
		return k.recordRevoked(ctx, tx, c.TenantID, c.ActorID, out, false)
	})
	if err != nil {
		return Revoked{}, pass(err, "revoke")
	}
	return out, nil
}

// RevokeAll stops every live key of the business -- the incident button -- with one trail row
// per key THIS request revoked. A key a racing revocation stopped first is not among them and
// gets no second row.
func (k *Keys) RevokeAll(ctx context.Context, tenantID, actorID uuid.UUID) ([]Revoked, error) {
	var out []Revoked
	err := k.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if err := mayManage(ctx, q, tenantID, actorID); err != nil {
			return err
		}
		rows, err := q.RevokeAllAPIKeys(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("revoke all: %w", err)
		}
		out = make([]Revoked, 0, len(rows))
		for _, r := range rows {
			rv := revoked(r.ID, r.Name, r.PrefixHint, r.RevokedAt)
			if err := k.recordRevoked(ctx, tx, tenantID, actorID, rv, true); err != nil {
				return err
			}
			out = append(out, rv)
		}
		return nil
	})
	if err != nil {
		return nil, pass(err, "revoke all")
	}
	return out, nil
}

func revoked(id uuid.UUID, name, hint string, at *time.Time) Revoked {
	r := Revoked{ID: id, Name: name, PrefixHint: hint}
	if at != nil {
		r.RevokedAt = *at
	}
	return r
}

func (k *Keys) recordRevoked(ctx context.Context, tx pgx.Tx, tenantID, actorID uuid.UUID, r Revoked, bulk bool) error {
	_, err := k.trail.RecordTx(ctx, tx, audit.Event{
		TenantID: tenantID, ActorID: &actorID, Action: ActionRevoked, Target: r.ID.String(),
		Detail: revokedDetail{Name: r.Name, PrefixHint: r.PrefixHint, Bulk: bulk, Via: viaPanel},
	})
	return err
}

// pass returns a refusal sentinel as it is and wraps anything else.
func pass(err error, act string) error {
	for _, s := range []error{ErrNotPermitted, ErrName, ErrNameTaken, ErrUnknownKey, ErrAlreadyRevoked, ErrAPIClosed} {
		if errors.Is(err, s) {
			return s
		}
	}
	return fmt.Errorf("api key: %s: %w", act, err)
}
