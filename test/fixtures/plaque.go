package fixtures

// plaque.go -- T76 step 2: a test plaque is reached the way the product reaches it.
//
// Migration 00100 (tags_is_born_in_stock) refuses a tags row that is not BORN IN STOCK: status
// 'unassigned', no location, unstamped, counter 0, never retired -- the row ADR 0017 §5.1 step 3
// writes. Everything else a plaque becomes it becomes by an UPDATE, so LoadPlaque walks the
// product's own statements, from internal/store, in the product's order:
//
//	store.InsertUnassigned       the encode flow's load (step 3)
//	store.MarkTagEncoded         the encode flow's stamp (step 9)            unless Unstamped
//	store.AssignTagToLocation    the panel's mount (00025 lets only a stamped plaque go active)
//	store.AdvanceTagCounter      the tap path's counter, for a test that wants one already moved
//	lost / retired               an UPDATE of status (RetireTagForReplacement when a successor is named)
//
// so a fixture cannot drift into a shape the product never writes, and a fixture that needs the one
// shape the product can no longer reach -- incident A-1's, on a wall and never stamped -- says so by
// calling LoadLegacyUnstampedActive.
//
// Every statement carries the tenant explicitly (CLAUDE.md §4.5), so q may be a tappa_app
// transaction inside the tenant's context or an owner connection without one.

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// The four plaque states (00013's tags_status_check).
const (
	PlaqueUnassigned = "unassigned"
	PlaqueActive     = "active"
	PlaqueLost       = "lost"
	PlaqueRetired    = "retired"
)

// FakeKeyRef is the placeholder aes_key_ref most fixtures load: 44 bytes (00021's envelope length,
// VALID since 00089) of 0xDEAD -- decode(repeat('dead', 22), 'hex'). It wraps nothing; a tap that
// reaches sun.Unwrap with it fails.
func FakeKeyRef() []byte { return bytes.Repeat([]byte{0xDE, 0xAD}, 22) }

// Plaque is one tags row a test needs.
type Plaque struct {
	UID      string
	TenantID uuid.UUID
	// Wall is the location the plaque is mounted on; uuid.Nil is a plaque in stock.
	Wall uuid.UUID
	// Status is the state to reach. Empty: active on a wall, unassigned without one.
	Status string
	// LastCtr is the counter a test wants already moved (by AdvanceTagCounter); 0 leaves it.
	LastCtr int32
	// Key is aes_key_ref (nil: FakeKeyRef); AppKey is app_key_ref (nil: no key 0 minted).
	Key, AppKey []byte
	// Unstamped leaves encoded_at NULL: the encode round never recorded its finish (ADR 0017
	// §5.2's "row, no chip"). Such a plaque cannot be mounted; for one already on a wall see
	// LoadLegacyUnstampedActive.
	Unstamped bool
	// ReplacedBy is a retired plaque's successor (its uid, already on a wall in the same tenant).
	ReplacedBy string
}

func (p Plaque) status() string {
	switch {
	case p.Status != "":
		return p.Status
	case p.Wall == uuid.Nil:
		return PlaqueUnassigned
	default:
		return PlaqueActive
	}
}

// ErrUnreachableShape is a Plaque no statement of the product can reach.
var ErrUnreachableShape = errors.New("plaque shape no product statement reaches")

// LoadPlaque writes p through the product's statements (the file comment) and returns the first
// error, wrapped with the step that failed.
func LoadPlaque(ctx context.Context, q store.DBTX, p Plaque) error {
	status := p.status()
	switch {
	case status == PlaqueUnassigned && (p.Wall != uuid.Nil || p.ReplacedBy != ""):
		return fmt.Errorf("load plaque %s: an unassigned plaque has no wall and no successor: %w", p.UID, ErrUnreachableShape)
	case (status == PlaqueActive || status == PlaqueRetired) && p.Wall == uuid.Nil:
		return fmt.Errorf("load plaque %s: a %s plaque is on a wall: %w", p.UID, status, ErrUnreachableShape)
	case status == PlaqueActive && p.Unstamped:
		return fmt.Errorf("load plaque %s: an unstamped plaque on a wall is incident A-1's legacy row "+
			"(LoadLegacyUnstampedActive): %w", p.UID, ErrUnreachableShape)
	case p.ReplacedBy != "" && status != PlaqueRetired:
		return fmt.Errorf("load plaque %s: only a retired plaque names a successor: %w", p.UID, ErrUnreachableShape)
	case status != PlaqueUnassigned && status != PlaqueActive && status != PlaqueLost && status != PlaqueRetired:
		return fmt.Errorf("load plaque %s: unknown status %q: %w", p.UID, status, ErrUnreachableShape)
	}
	key := p.Key
	if key == nil {
		key = FakeKeyRef()
	}
	s := store.New(q)
	if _, err := s.InsertUnassigned(ctx, store.InsertUnassignedParams{
		Uid: p.UID, TenantID: p.TenantID, AesKeyRef: key, AppKeyRef: p.AppKey,
	}); err != nil {
		return fmt.Errorf("load plaque %s: %w", p.UID, err)
	}
	if !p.Unstamped {
		if _, err := s.MarkTagEncoded(ctx, store.MarkTagEncodedParams{Uid: p.UID, TenantID: p.TenantID}); err != nil {
			return fmt.Errorf("stamp plaque %s: %w", p.UID, err)
		}
	}
	if p.Wall != uuid.Nil {
		if p.Unstamped {
			// A lost or retired plaque on a wall whose encode was never recorded: the product reached
			// it from an A-1 row. Only the move INTO 'active' is gated (00025), so one UPDATE lands
			// the same row without passing through the shape 00100 no longer lets be born.
			if _, err := q.Exec(ctx, `UPDATE tags SET location_id = $3, status = $4
			                           WHERE uid = $1 AND tenant_id = $2 AND status = 'unassigned'`,
				p.UID, p.TenantID, p.Wall, status); err != nil {
				return fmt.Errorf("place unstamped plaque %s: %w", p.UID, err)
			}
		} else if _, err := s.AssignTagToLocation(ctx, store.AssignTagToLocationParams{
			LocationID: p.Wall, TenantID: p.TenantID, Uid: p.UID,
		}); err != nil {
			return fmt.Errorf("mount plaque %s: %w", p.UID, err)
		}
	}
	if p.LastCtr > 0 {
		if _, err := s.AdvanceTagCounter(ctx, store.AdvanceTagCounterParams{
			Ctr: p.LastCtr, TenantID: p.TenantID, Uid: p.UID,
		}); err != nil {
			return fmt.Errorf("advance plaque %s to %d: %w", p.UID, p.LastCtr, err)
		}
	}
	switch {
	case status == PlaqueLost:
		if _, err := q.Exec(ctx, `UPDATE tags SET status = 'lost' WHERE uid = $1 AND tenant_id = $2`,
			p.UID, p.TenantID); err != nil {
			return fmt.Errorf("lose plaque %s: %w", p.UID, err)
		}
	case status == PlaqueRetired && p.ReplacedBy != "" && !p.Unstamped:
		if _, err := s.RetireTagForReplacement(ctx, store.RetireTagForReplacementParams{
			ReplacedBy: p.ReplacedBy, TenantID: p.TenantID, Uid: p.UID,
		}); err != nil {
			return fmt.Errorf("retire plaque %s: %w", p.UID, err)
		}
	case status == PlaqueRetired:
		if _, err := q.Exec(ctx, `UPDATE tags SET status = 'retired', retired_at = now(), replaced_by = $3
		                           WHERE uid = $1 AND tenant_id = $2`,
			p.UID, p.TenantID, nullIfEmpty(p.ReplacedBy)); err != nil {
			return fmt.Errorf("retire plaque %s: %w", p.UID, err)
		}
	}
	return nil
}

// LoadLegacyUnstampedActive writes incident A-1's row (2026-09-24): on a wall, in service, encode
// never recorded. Production still holds such rows -- born before 00100 -- and the panel and the
// operator read must keep telling them apart (ADR 0017 §5.2: repaired by Replace), but since 00100
// no statement reaches the shape: a row is born in stock and 00025 refuses an unstamped mount. So the
// fixture does what a data-only restore does: tx must be a SUPERUSER's transaction (tappa_owner in
// development and CI), and session_replication_role = replica switches the database's ordinary
// triggers off for the one INSERT, then back on. Nothing outside tx is affected, and no catalogue
// lock is taken (ALTER TABLE ... DISABLE TRIGGER would lock tags against every tap in every package
// running beside it). The row's foreign keys are not checked in replica mode -- the caller names a
// tenant and a wall that exist.
func LoadLegacyUnstampedActive(ctx context.Context, tx pgx.Tx, p Plaque) error {
	if p.Wall == uuid.Nil || (p.Status != "" && p.Status != PlaqueActive) || !p.Unstamped || p.ReplacedBy != "" {
		return fmt.Errorf("legacy plaque %s: only the A-1 shape (active, on a wall, Unstamped): %w", p.UID, ErrUnreachableShape)
	}
	key := p.Key
	if key == nil {
		key = FakeKeyRef()
	}
	if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
		return fmt.Errorf("legacy plaque %s: switch triggers off (a superuser transaction is required): %w", p.UID, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, app_key_ref, last_ctr, status)
	                           VALUES ($1, $2, $3, $4, $5, $6, 'active')`,
		p.UID, p.TenantID, p.Wall, key, p.AppKey, p.LastCtr); err != nil {
		return fmt.Errorf("legacy plaque %s: %w", p.UID, err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = origin`); err != nil {
		return fmt.Errorf("legacy plaque %s: switch triggers back on: %w", p.UID, err)
	}
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
