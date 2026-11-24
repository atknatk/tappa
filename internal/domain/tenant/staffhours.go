package tenant

// staffhours.go -- the owner's My hours setting (M16-30; Q48 (a)): whether the staff app shows a
// person their own records and weekly totals, and how many whole weeks back. NULL is off. The
// server enforces it on every read (internal/handler/apphours.go); it changes nothing the engine
// decides.

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/store"
)

// ActionStaffHoursUpdated is the trail's name for the setting's change.
const ActionStaffHoursUpdated = "tenant.staff_hours_updated"

// StaffHoursOptions are the windows an owner may pick, in whole weeks (00117's CHECK).
var StaffHoursOptions = []int{1, 2, 4, 13}

// ErrStaffHoursNotPermitted: the actor is not an active owner of this business.
var ErrStaffHoursNotPermitted = errors.New("tenant: the My hours setting is reserved for an active owner")

// ErrStaffHoursOption: a window outside StaffHoursOptions.
var ErrStaffHoursOption = errors.New("tenant: My hours shows 1, 2, 4 or 13 weeks")

// ValidStaffHoursWeeks says whether n is one of StaffHoursOptions.
func ValidStaffHoursWeeks(n int) bool {
	for _, o := range StaffHoursOptions {
		if n == o {
			return true
		}
	}
	return false
}

type staffHoursDetail struct {
	Before *int `json:"before"`
	After  *int `json:"after"`
}

// SetStaffHoursWeeks writes the setting (nil = off) and its audit row in ONE transaction, the
// owner read from admin_users inside it.
func (a *Accounts) SetStaffHoursWeeks(ctx context.Context, tenantID, actorID uuid.UUID, weeks *int) error {
	if err := requireActor(tenantID, actorID); err != nil {
		return err
	}
	if weeks != nil && !ValidStaffHoursWeeks(*weeks) {
		return ErrStaffHoursOption
	}
	err := a.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if err := mayEditBrand(ctx, q, tenantID, actorID); err != nil {
			if errors.Is(err, ErrBrandNotPermitted) {
				return ErrStaffHoursNotPermitted
			}
			return err
		}
		prev, err := q.GetTenantStaffHoursWeeksForUpdate(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("lock the setting: %w", err)
		}
		var arg *int16
		if weeks != nil {
			v := int16(*weeks)
			arg = &v
		}
		n, err := q.SetTenantStaffHoursWeeks(ctx, store.SetTenantStaffHoursWeeksParams{StaffHoursWeeks: arg, TenantID: tenantID})
		if err != nil {
			return fmt.Errorf("write the setting: %w", err)
		}
		if n != 1 {
			return fmt.Errorf("write the setting: %d rows changed, want 1", n)
		}
		_, err = a.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: tenantID,
			ActorID:  &actorID,
			Action:   ActionStaffHoursUpdated,
			Target:   tenantID.String(),
			Detail:   staffHoursDetail{Before: weeksOf(prev), After: weeks},
		})
		return err
	})
	if errors.Is(err, ErrStaffHoursNotPermitted) {
		return ErrStaffHoursNotPermitted
	}
	if err != nil {
		return fmt.Errorf("tenant: My hours setting: %w", err)
	}
	a.log.Info("tenant My hours setting updated", "tenant_id", tenantID, "actor_id", actorID, "weeks", weeksOrZero(weeks))
	return nil
}

func weeksOf(v *int16) *int {
	if v == nil {
		return nil
	}
	n := int(*v)
	return &n
}

func weeksOrZero(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}
