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
// and POST /api/checkin send it to /activate?from=signedout with NO record (CLAUDE.md §5 row 3).
// That is right here: the phone was signed out, so nobody is identified, and the remedy is a new
// activation link. The landing names no cause -- a second activation revokes too -- and says
// "This phone is no longer signed in to Taptime, so it can't record time." with "Ask your manager
// for a new activation link." (user decision, 2026-11-09).
//
// 🔴 A DEACTIVATED PERSON IS REFUSED, AND THAT IS THE 2026-08-07 DECISION KEPT, NOT A NEW ONE.
// Deactivation deliberately leaves sessions live so that every later attempt by that person reaches
// §5 row 4 -- refused, RECORDED, and raised as a security alert. Revoking those sessions would move
// the same attempts onto row 3, where nothing is written (§4.6). A stolen cookie of a deactivated
// person can only produce refused-and-recorded rows, so there is nothing for this act to protect
// against there and one thing for it to lose. The refusal is read inside the same transaction as
// the revoke.
//
// THE APP TOO (M16-09, ADR 0045 §2.3; user decision Q40 (a), 2026-11-14). A person's "who" is
// either a browser session or a native-app device, and "Sign out their phones" closes BOTH, in the
// same transaction: RevokeSessionsForEmployee, then RevokeAppDevicesForEmployee (which deletes the
// revoked devices' live nonces in its own statement), then the trail row. A revoked device's next
// request is appdevice.ErrRevokedDevice -- §5 row 3's twin: 401 / re-activation, NO record. The
// deactivated refusal below covers the devices exactly as it covers the sessions: a deactivated
// person's device stays live so their next app tap reaches row 4 and is recorded.
//
// 🔴 ALL OR NOTHING, AND WHY NOT "AS MUCH AS POSSIBLE". The two revokes and the trail share one
// transaction, so there is no partial outcome: either every live session AND every live device of
// the person is revoked and the trail says how many of each, or nothing changed and the manager is
// told the write failed (problemPanelWriteFailed). A partial success would be worse than either
// end for the case this act exists for -- a stolen phone: "signed out" on the screen while the
// thief's app (or browser) still records time. The one failure that is not the manager's to see is
// a deadlock (40P01): Postgres rolls the whole transaction back, so it is retried as a whole
// (signOutAttempts) -- M16-03's rule that the revoke's caller retries, because the measured
// deadlock victim was the revoke and a stolen phone could otherwise keep it failing.
//
// LOCK ORDER: sessions rows, then device rows, then those devices' nonce rows (the last two inside
// RevokeAppDevicesForEmployee's one statement). The app paths take a device row before its nonce
// rows (appdevice.Verifier: Touch, then consume; issue, then sweep) and never a session row, so
// they queue behind this transaction instead of crossing it. An activation that swaps the
// credential type (M16-07) should take the same order -- sessions, then devices.
//
// ⚠️ WHAT THE STATUS READ DOES NOT LOCK. A deactivation committed between the read and the
// revoke's commit leaves a deactivated person with revoked sessions and devices -- the same end state as
// "sign out, then deactivate", which is a legitimate order (the phone was stolen, then they
// left) and is not refused either. The refusal is about the act on somebody ALREADY deactivated,
// whose card says their phones stay signed in on purpose.

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/store"
)

// ActionEmployeePhonesSignedOut is the trail row of one sign-out that revoked at least one
// session or app device. It sits under `employee.%` with the other acts on a person (staff.go).
const ActionEmployeePhonesSignedOut = "employee.phones_signed_out"

// signOutAttempts bounds the deadlock retry: the first try and two more. A deadlock needs a
// concurrent transaction holding a row this one wants while wanting one this one holds; after
// a rollback the other side completes, so a second deadlock in a row is already rare and a third
// would be a fault worth a 500, not a loop.
const signOutAttempts = 3

var (
	// ErrNoLiveSessions: nothing was signed in as this person -- no live browser session and no
	// live app device -- so nothing was revoked and no trail row was written. A second press, or a
	// replayed confirmation with nothing signed in since, lands here.
	ErrNoLiveSessions = errors.New("tenant: no phone is signed in as this employee")
	// ErrSignOutDeactivated: the person is deactivated, whose sessions AND app devices stay live
	// so their attempts are recorded (§5 row 4). Nothing was written.
	ErrSignOutDeactivated = errors.New("tenant: a deactivated employee's phones are not signed out")
)

// SignOutCommand is what a manager confirmed.
type SignOutCommand struct {
	TenantID   uuid.UUID
	EmployeeID uuid.UUID
	// ActorID is the admin, from the signed panel session -- never from the request.
	ActorID uuid.UUID
}

// SignedOut is what one sign-out revoked: how many browser sessions and how many app devices.
// Counts only -- the ids stay in the database (§7).
type SignedOut struct {
	Sessions   int
	AppDevices int
}

// Total is everything the act closed.
func (s SignedOut) Total() int { return s.Sessions + s.AppDevices }

// signOutDetail is the trail row's payload: the two counts and nothing else. No session or
// device id, no token, no hash, no key, no device label -- counts are all an investigator needs
// to join "how many phones" to the act, and the sessions and app_devices tables keep the revoked
// rows themselves (neither takes a DELETE from the application).
type signOutDetail struct {
	SessionsRevoked   int `json:"sessions_revoked"`
	AppDevicesRevoked int `json:"app_devices_revoked"`
}

// SignOutPhones revokes every live browser session and every live app device of one person and
// records that it did, in ONE transaction, and returns how many of each it revoked.
//
// Both revokes are one UPDATE with `revoked_at IS NULL` in its WHERE, so the counts they return
// are exactly the rows this call changed, and a repeat changes none. The card's counts read the
// same predicates (GetPanelPersonSummary), so what the manager was told and what was revoked are
// the same set unless a phone signed in between the two.
func (s *Staff) SignOutPhones(ctx context.Context, c SignOutCommand) (SignedOut, error) {
	if err := requireIDs(c.TenantID, c.EmployeeID, c.ActorID); err != nil {
		return SignedOut{}, err
	}
	var (
		out SignedOut
		err error
	)
	for attempt := 1; ; attempt++ {
		out, err = s.signOutOnce(ctx, c)
		if !isDeadlock(err) || attempt == signOutAttempts {
			break
		}
		// The whole transaction was rolled back, so the next try starts from nothing.
		s.log.Warn("employee sign-out deadlocked; retrying",
			"employee_id", c.EmployeeID, "attempt", attempt)
	}
	switch {
	case err == nil:
	case errors.Is(err, ErrUnknownEmployee), errors.Is(err, ErrSignOutDeactivated), errors.Is(err, ErrNoLiveSessions):
		return SignedOut{}, err
	default:
		return SignedOut{}, fmt.Errorf("tenant: sign out phones: %w", err)
	}
	// Ids and counts only: no name, no session or device id, no token, hash or key (§7).
	s.log.Info("employee phones signed out",
		"employee_id", c.EmployeeID, "actor_id", c.ActorID,
		"sessions_revoked", out.Sessions, "app_devices_revoked", out.AppDevices)
	return out, nil
}

// signOutOnce is one attempt: the gate, both revokes and the trail, in one transaction.
func (s *Staff) signOutOnce(ctx context.Context, c SignOutCommand) (SignedOut, error) {
	var out SignedOut
	err := s.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		// THE TENANT GATE: the person must be on THIS business's roster (explicit tenant_id in
		// the query, RLS under it). A foreign or invented id is ErrUnknownEmployee, and neither
		// revoke below is reached for it.
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
		sessions, err := q.RevokeSessionsForEmployee(ctx, store.RevokeSessionsForEmployeeParams{
			TenantID:   c.TenantID,
			EmployeeID: c.EmployeeID,
		})
		if err != nil {
			return fmt.Errorf("revoke sessions: %w", err)
		}
		if s.afterSessionsRevoked != nil {
			s.afterSessionsRevoked(ctx)
		}
		devices, err := q.RevokeAppDevicesForEmployee(ctx, store.RevokeAppDevicesForEmployeeParams{
			TenantID:   c.TenantID,
			EmployeeID: c.EmployeeID,
		})
		if err != nil {
			return fmt.Errorf("revoke app devices: %w", err)
		}
		out = SignedOut{Sessions: len(sessions), AppDevices: len(devices)}
		if out.Total() == 0 {
			// Nothing changed, so nothing is trailed: a second press is not a second act
			// (Deactivate's rule).
			return ErrNoLiveSessions
		}
		// The trail shares the revokes' fate: if it cannot be written, nothing is revoked.
		_, err = s.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID,
			ActorID:  &c.ActorID,
			Action:   ActionEmployeePhonesSignedOut,
			Target:   c.EmployeeID.String(),
			Detail:   signOutDetail{SessionsRevoked: out.Sessions, AppDevicesRevoked: out.AppDevices},
		})
		return err
	})
	if err != nil {
		return SignedOut{}, err
	}
	return out, nil
}

// isDeadlock is 40P01 anywhere in the chain. Only a deadlock is retried: the other transient
// states (a dropped connection, a timeout) may arrive after a COMMIT reached the server
// (db.Transient's warning), and a lock_timeout is a configured answer, not a crossing.
func isDeadlock(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "40P01"
}
