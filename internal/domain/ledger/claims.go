package ledger

// claims.go -- the READ side of the panel's claim queue (M15-18; ADR 0032 §C.4): the punch claims of
// this business nobody has decided yet, for the Spike's second section, "Time claims".
//
// IT IS A LIST AND NOTHING MORE. Whether a claim can still be approved, the interval it would make,
// the shift and the deviation are internal/domain/claim's (Inspect), read for the ONE claim in focus
// -- the same function Decide judges with, so the queue cannot show one thing and the decision do
// another. This file adds one SELECT and keeps the package's rule: no store call here is a write.
//
// TENANT SCOPE (§4.5, belt + braces) is the package's: the read runs inside WithTenant (RLS) AND the
// statement carries the tenant on every table it reads; TestPanelQueries_CarryAnExplicitTenantPredicate
// derives its subjects from the store calls this package makes, so this one is checked because it is
// called.

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// ClaimQueueCap is how many undecided claims one view lists. The list is oldest first and the queue
// shrinks as claims are decided, so a list that stops here says so (ClaimQueue.More) and the next
// view shows the rest; a keyset pager is not built for a queue that should rarely hold more than a
// handful (one claim per forgotten tap, ever).
const ClaimQueueCap = 50

// PendingClaim is one undecided claim, as the queue's link shows it.
type PendingClaim struct {
	ID         uuid.UUID
	EmployeeID uuid.UUID
	// EmployeeName is "" when the roster no longer holds the person; the screen prints a word.
	EmployeeName string
	// Kind is the missing half the claim names: "in" or "out" (migration 00068's vocabulary).
	Kind      string
	ClaimedAt time.Time
	// SentAt is when the claim was sent (punch_claims.created_at, the database's clock).
	SentAt   time.Time
	AnchorAt time.Time
}

// ClaimQueue is one read of the queue.
type ClaimQueue struct {
	// Queried is set ONLY after the database answered: an empty unqueried queue is not "nothing is
	// waiting" (the anti-fabrication flag every panel read carries).
	Queried bool
	Zone    *time.Location
	Claims  []PendingClaim
	// More: more claims are waiting than ClaimQueueCap lists.
	More bool
}

// Claims reads the undecided claims, oldest first, with the tenant's zone from the same snapshot.
func (r *Reader) Claims(ctx context.Context, tenantID uuid.UUID) (ClaimQueue, error) {
	var out ClaimQueue
	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		clock, err := q.GetTenantClock(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("load tenant clock: %w", err)
		}
		rows, err := q.ListPendingPunchClaims(ctx, store.ListPendingPunchClaimsParams{TenantID: tenantID, RowLimit: ClaimQueueCap + 1})
		if err != nil {
			return fmt.Errorf("list pending punch claims: %w", err)
		}
		out = ClaimQueue{Queried: true, Zone: r.loadZone(clock.Timezone)}
		if len(rows) > ClaimQueueCap {
			rows, out.More = rows[:ClaimQueueCap], true
		}
		out.Claims = make([]PendingClaim, 0, len(rows))
		for _, row := range rows {
			out.Claims = append(out.Claims, PendingClaim{ID: row.ID, EmployeeID: row.EmployeeID, EmployeeName: deref(row.EmployeeName),
				Kind: row.Kind, ClaimedAt: row.ClaimedAt, SentAt: row.CreatedAt, AnchorAt: row.AnchorAt})
		}
		return nil
	})
	if err != nil {
		return ClaimQueue{}, fmt.Errorf("ledger: claims: %w", err)
	}
	return out, nil
}
