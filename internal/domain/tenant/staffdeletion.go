package tenant

// staffdeletion.go -- "Delete my account" from the staff app (M16-37; Apple 5.1.1(v), GDPR Art. 17).
//
// 🔴 WHAT IT IS, STATED: a REQUEST to the employer, plus "signed out everywhere". The person's
// clock-in records are legal records the employer controls (§4.3: transactions are never deleted),
// so nothing of them is touched: not transactions, not punch_claims, not audit_log, not the
// employees row (neither deleted nor deactivated -- ADR 0010). What happens, in ONE transaction:
// every live browser session and app device of the person is revoked (SignOutPhones' order:
// sessions, then devices, then their nonces), and one trail row says the person asked. The panel's
// employee card reads that row ("Asked to delete their account on …"), and the owners are e-mailed off
// the request path (internal/handler's ownerNotices) from what this returns.
//
// A DEACTIVATED PERSON keeps their credentials (T59's rule, ErrSignOutDeactivated's reason: their
// attempts must stay refused-and-recorded, §5 row 4); the request is still trailed, so it still
// reaches the employer.

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

// ActionEmployeeDeletionRequested is the trail row of a person's own deletion request.
const ActionEmployeeDeletionRequested = "employee.deletion_requested"

// DeletionRequest is who asked, from which device (the signed request's).
type DeletionRequest struct {
	TenantID, EmployeeID, AppDeviceID uuid.UUID
}

// DeletionRequested is what the request closed (counts only, §7) and what the owners' notice says
// about it (M16-37): the person's name, the business's zone and when it was recorded. No address.
type DeletionRequested struct {
	Sessions, AppDevices int
	// Deactivated says the person was deactivated, so nothing was revoked.
	Deactivated bool
	// EmployeeName is the person's name as the business stores it; Timezone the business's zone name
	// (tenants.timezone, read in the same transaction); At the moment the transaction committed, read
	// on this server's clock after it.
	EmployeeName string
	Timezone     string
	At           time.Time
}

type deletionDetail struct {
	AppDeviceID     string `json:"app_device_id"`
	RevokedSessions int    `json:"revoked_sessions"`
	RevokedDevices  int    `json:"revoked_devices"`
	Deactivated     bool   `json:"deactivated"`
}

// RequestDeletion revokes the person's credentials (unless deactivated) and trails the request,
// in one transaction; a deadlock is retried as a whole (SignOutPhones' rule).
func (s *Staff) RequestDeletion(ctx context.Context, r DeletionRequest) (DeletionRequested, error) {
	if err := requireIDs(r.TenantID, r.EmployeeID, r.AppDeviceID); err != nil {
		return DeletionRequested{}, err
	}
	var (
		out DeletionRequested
		err error
	)
	for attempt := 1; ; attempt++ {
		out, err = s.requestDeletionOnce(ctx, r)
		if !isDeadlock(err) || attempt == signOutAttempts {
			break
		}
		s.log.Warn("employee deletion request deadlocked; retrying", "employee_id", r.EmployeeID, "attempt", attempt)
	}
	if errors.Is(err, ErrUnknownEmployee) {
		return DeletionRequested{}, err
	}
	if err != nil {
		return DeletionRequested{}, fmt.Errorf("tenant: deletion request: %w", err)
	}
	s.log.Info("employee asked to delete their account",
		"employee_id", r.EmployeeID, "sessions_revoked", out.Sessions, "app_devices_revoked", out.AppDevices, "deactivated", out.Deactivated)
	return out, nil
}

func (s *Staff) requestDeletionOnce(ctx context.Context, r DeletionRequest) (DeletionRequested, error) {
	var out DeletionRequested
	err := s.data.WithTenant(ctx, r.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		who, err := q.GetPanelEmployeeForAction(ctx, store.GetPanelEmployeeForActionParams{TenantID: r.TenantID, ID: r.EmployeeID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnknownEmployee
			}
			return fmt.Errorf("load employee: %w", err)
		}
		clock, err := q.GetTenantClock(ctx, r.TenantID)
		if err != nil {
			return fmt.Errorf("load business zone: %w", err)
		}
		out = DeletionRequested{Deactivated: who.Status == statusDeactivated, EmployeeName: who.FullName, Timezone: clock.Timezone}
		if !out.Deactivated {
			sessions, err := q.RevokeSessionsForEmployee(ctx, store.RevokeSessionsForEmployeeParams{TenantID: r.TenantID, EmployeeID: r.EmployeeID})
			if err != nil {
				return fmt.Errorf("revoke sessions: %w", err)
			}
			devices, err := q.RevokeAppDevicesForEmployee(ctx, store.RevokeAppDevicesForEmployeeParams{TenantID: r.TenantID, EmployeeID: r.EmployeeID})
			if err != nil {
				return fmt.Errorf("revoke app devices: %w", err)
			}
			out.Sessions, out.AppDevices = len(sessions), len(devices)
		}
		_, err = s.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: r.TenantID,
			Action:   ActionEmployeeDeletionRequested,
			Target:   r.EmployeeID.String(),
			Detail: deletionDetail{AppDeviceID: r.AppDeviceID.String(), RevokedSessions: out.Sessions,
				RevokedDevices: out.AppDevices, Deactivated: out.Deactivated},
		})
		return err
	})
	out.At = time.Now()
	return out, err
}
