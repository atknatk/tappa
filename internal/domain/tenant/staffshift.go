package tenant

// staffshift.go -- the owner's "these are my staff's real shifts" switch (M16-45, ADR 0050 (iv);
// Q52 (2) (c)). A stored shift may be a venue's opening window; only the owner knows, and only
// with this switch on does the staff app call it the person's shift (the usual-shift line, the
// reminder's shift arm, the lock screen's "Shift ends"). It changes nothing the engine decides.

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/store"
)

// ActionStaffShiftUpdated is the trail's name for the switch's change.
const ActionStaffShiftUpdated = "tenant.staff_shift_updated"

// ErrStaffShiftNotPermitted: the actor is not an active owner of this business (one error for a
// manager, a disabled owner and a stranger -- the brand gate's rule).
var ErrStaffShiftNotPermitted = errors.New("tenant: the staff shift switch is reserved for an active owner")

type staffShiftDetail struct {
	Before bool `json:"before"`
	After  bool `json:"after"`
}

// SetStaffShiftVisible writes the switch and its audit row in ONE transaction, the owner read
// from admin_users inside it (mayEditBrand: the domain does not take the handler's word). A press
// that changes nothing still writes its row (Accounts.Save's rule).
func (a *Accounts) SetStaffShiftVisible(ctx context.Context, tenantID, actorID uuid.UUID, on bool) error {
	if err := requireActor(tenantID, actorID); err != nil {
		return err
	}
	err := a.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if err := mayEditBrand(ctx, q, tenantID, actorID); err != nil {
			if errors.Is(err, ErrBrandNotPermitted) {
				return ErrStaffShiftNotPermitted
			}
			return err
		}
		before, err := q.GetTenantStaffShiftVisibleForUpdate(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("lock the switch: %w", err)
		}
		n, err := q.SetTenantStaffShiftVisible(ctx, store.SetTenantStaffShiftVisibleParams{StaffShiftVisible: on, TenantID: tenantID})
		if err != nil {
			return fmt.Errorf("write the switch: %w", err)
		}
		if n != 1 {
			return fmt.Errorf("write the switch: %d rows changed, want 1", n)
		}
		_, err = a.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: tenantID,
			ActorID:  &actorID,
			Action:   ActionStaffShiftUpdated,
			Target:   tenantID.String(),
			Detail:   staffShiftDetail{Before: before, After: on},
		})
		return err
	})
	if errors.Is(err, ErrStaffShiftNotPermitted) {
		return ErrStaffShiftNotPermitted
	}
	if err != nil {
		return fmt.Errorf("tenant: staff shift switch: %w", err)
	}
	a.log.Info("tenant staff shift switch updated", "tenant_id", tenantID, "actor_id", actorID, "on", on)
	return nil
}
