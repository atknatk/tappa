package tenant

// staffsignout.go -- "Sign out their phones" (backlog T59, user decision 2026-11-08): a manager
// ends every live tap session of one person, for a lost or stolen phone.
//
// WHY IT EXISTS. A tap session is a cookie, and a cookie can be copied. Until this file the only
// way to kill one was a second activation (internal/handler/activate.go), which the person has to
// walk themselves; a manager who heard "my phone was stolen" had nothing to press, and a stolen
// cookie plus a photographed QR code could keep writing records. RevokeSessionsForEmployee has
// always named "a lost or stolen phone" as one of its two legitimate callers -- this is that
// caller.
//
// WHAT HAPPENS TO THE NEXT TAP. A revoked session resolves as httpx.SessionRevoked, and both GET /t
// and POST /api/checkin send it to the activation page with NO record (CLAUDE.md §5 row 3). That
// is right here: the phone was signed out, so nobody is identified, and the remedy is a new
// activation link -- which the screen says.
//
// 🔴 A DEACTIVATED PERSON IS REFUSED, AND THAT IS THE 2026-08-07 DECISION KEPT, NOT A NEW ONE.
// Deactivation deliberately leaves sessions live so that every later attempt by that person reaches
// §5 row 4 -- refused, RECORDED, and raised as a security alert. Revoking those sessions would move
// the same attempts onto row 3, where nothing is written (§4.6). A stolen cookie of a deactivated
// person can only produce refused-and-recorded rows, so there is nothing for this act to protect
// against there and one thing for it to lose. The refusal is read inside the same transaction as
// the revoke.
//
// ⚠️ WHAT THE STATUS READ DOES NOT LOCK. A deactivation committed between the read and the
// revoke's commit leaves a deactivated person with revoked sessions -- the same end state as
// "sign out, then deactivate", which is a legitimate order (the phone was stolen, then they
// left) and is not refused either. The refusal is about the act on somebody ALREADY deactivated,
// whose card says their phones stay signed in on purpose.

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/store"
)

// ActionEmployeePhonesSignedOut is the trail row of one sign-out that revoked at least one
// session. It sits under `employee.%` with the other acts on a person (staff.go).
const ActionEmployeePhonesSignedOut = "employee.phones_signed_out"

var (
	// ErrNoLiveSessions: nothing was signed in as this person, so nothing was revoked and no
	// trail row was written. A second press, or a replayed confirmation, lands here.
	ErrNoLiveSessions = errors.New("tenant: no phone is signed in as this employee")
	// ErrSignOutDeactivated: the person is deactivated, whose sessions stay live so their
	// attempts are recorded (§5 row 4). Nothing was written.
	ErrSignOutDeactivated = errors.New("tenant: a deactivated employee's phones are not signed out")
)

// SignOutCommand is what a manager confirmed.
type SignOutCommand struct {
	TenantID   uuid.UUID
	EmployeeID uuid.UUID
	// ActorID is the admin, from the signed panel session -- never from the request.
	ActorID uuid.UUID
}

// signOutDetail is the trail row's payload: the count and nothing else. No session id, no token,
// no hash, no device label -- a count is all an investigator needs to join "how many phones" to
// the act, and the sessions table keeps the revoked rows themselves (it takes no DELETE).
type signOutDetail struct {
	SessionsRevoked int `json:"sessions_revoked"`
}

// SignOutPhones revokes every live session of one person and records that it did, in ONE
// transaction, and returns how many it revoked.
//
// The revoke is RevokeSessionsForEmployee: one UPDATE with `revoked_at IS NULL` in its WHERE, so
// the count it returns is exactly the rows this call changed, and a repeat changes none. The card's
// count reads the same predicate (GetPanelPersonSummary), so what the manager was told and what
// was revoked are the same set unless a phone signed in between the two.
func (s *Staff) SignOutPhones(ctx context.Context, c SignOutCommand) (int, error) {
	if err := requireIDs(c.TenantID, c.EmployeeID, c.ActorID); err != nil {
		return 0, err
	}
	var revoked int
	err := s.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		// THE TENANT GATE: the person must be on THIS business's roster (explicit tenant_id in
		// the query, RLS under it). A foreign or invented id is ErrUnknownEmployee, and the
		// revoke below is never reached for it.
		who, err := q.GetPanelEmployeeForAction(ctx, store.GetPanelEmployeeForActionParams{
			TenantID: c.TenantID,
			ID:       c.EmployeeID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnknownEmployee
			}
			return fmt.Errorf("load employee: %w", err)
		}
		if who.Status == statusDeactivated {
			return ErrSignOutDeactivated
		}
		ids, err := q.RevokeSessionsForEmployee(ctx, store.RevokeSessionsForEmployeeParams{
			TenantID:   c.TenantID,
			EmployeeID: c.EmployeeID,
		})
		if err != nil {
			return fmt.Errorf("revoke sessions: %w", err)
		}
		if len(ids) == 0 {
			// Nothing changed, so nothing is trailed: a second press is not a second act
			// (Deactivate's rule).
			return ErrNoLiveSessions
		}
		revoked = len(ids)
		// The trail shares the revoke's fate: if it cannot be written, no session is revoked.
		_, err = s.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID,
			ActorID:  &c.ActorID,
			Action:   ActionEmployeePhonesSignedOut,
			Target:   c.EmployeeID.String(),
			Detail:   signOutDetail{SessionsRevoked: revoked},
		})
		return err
	})
	switch {
	case err == nil:
	case errors.Is(err, ErrUnknownEmployee), errors.Is(err, ErrSignOutDeactivated), errors.Is(err, ErrNoLiveSessions):
		return 0, err
	default:
		return 0, fmt.Errorf("tenant: sign out phones: %w", err)
	}
	// Ids and a count only: no name, no session id, no token or hash (§7).
	s.log.Info("employee phones signed out",
		"employee_id", c.EmployeeID, "actor_id", c.ActorID, "sessions_revoked", revoked)
	return revoked, nil
}
