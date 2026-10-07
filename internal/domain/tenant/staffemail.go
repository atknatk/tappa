package tenant

// staffemail.go -- the address on file for one employee (M10 EM-6): read it,
// change it, clear it. Design: ADR 0022 §7 ("E-postayı değiştir") and its EM-6 note.
//
// 🔴 WHY A CHANGED ADDRESS RETIRES INVITATIONS. An activation link is a credential
// (whoever opens it signs a phone in as this person), and once invitations are
// e-mailed (EM-7) the address IS where that credential goes. A code sent to the old
// address must die with the address: ChangeEmail retires every invitation of this
// person that could still be spent, in the SAME transaction as the write and the
// trail row, so "the address changed" and "the old links are dead" are one fact.
//
// 🔴 WHAT THIS FILE DOES NOT DO:
//   - It does not SEND anything. Delivering an invitation by e-mail is EM-7's.
//   - It does not decide WHO may change an address. That is the panel's role gate
//     (internal/handler, owner only — the argument is written there), because the
//     gate needs the resolved session and this package never sees one.
//   - It does not normalise the address. The rule is internal/mail's recipient rule
//     (ADR 0022 §4), asked through mail.ValidRecipient so there is no second copy:
//     surrounding whitespace is trimmed (the reset form's precedent), and anything
//     else the rule refuses — a non-ASCII byte (so a NON-ASCII look-alike letter such
//     as a Cyrillic or full-width one), a display name, CR or LF, more than 254 bytes
//     — is REFUSED, never cleaned. Look-alikes INSIDE ASCII (rn/m, l/I/1, 0/O) are
//     ordinary characters and pass. Case is kept as typed; the column is citext, so
//     uniqueness ignores it.
//   - It does not write the address into the trail or the log. audit_log is
//     append-only and a GDPR erasure (Q13) is an UPDATE on `employees` that would
//     never reach it (addDetail's argument), and a process log has no retention
//     story at all. The row says THAT the address changed and whether there is one
//     now; the value lives only in `employees`.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/mail"
	"github.com/atknatk/tappa/internal/store"
)

// ActionEmployeeEmailChanged is the trail row for a written address change. It
// follows the `<subject>.<past participle>` shape the other employee acts use.
const ActionEmployeeEmailChanged = "employee.email_changed"

// ErrSameEmail: the requested address is byte-for-byte the one on file (or both are
// "no address"). Nothing is written, no invitation is retired and no trail row is
// added — ErrSamePlacement's argument: a trail that records "changed from A to A" on
// every double submission stops being readable, and "that is already their address"
// is the true sentence.
//
// ⚠️ IT IS BYTE EQUALITY, NOT THE COLUMN'S. A change that only alters capitals IS a
// write (the stored text changes, and the manager typed it on purpose), and it retires
// pending invitations like any other change — the safe direction.
var ErrSameEmail = errors.New("tenant: that is already this employee's address on file")

// Email returns the address on file for one employee, or "" when there is none.
//
// pgx.ErrNoRows becomes ErrUnknownEmployee: a foreign or invented id is "not on this
// roster", never a database failure and never another tenant's person.
func (s *Staff) Email(ctx context.Context, tenantID, employeeID uuid.UUID) (string, error) {
	if tenantID == uuid.Nil || employeeID == uuid.Nil {
		return "", ErrUnknownEmployee
	}
	var out string
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, err := store.New(tx).GetEmployeeEmail(ctx, store.GetEmployeeEmailParams{
			TenantID: tenantID,
			ID:       employeeID,
		})
		if err != nil {
			return err
		}
		out = deref(row.Email)
		return nil
	})
	switch {
	case err == nil:
		return out, nil
	case errors.Is(err, pgx.ErrNoRows):
		return "", ErrUnknownEmployee
	default:
		return "", fmt.Errorf("tenant: load employee address: %w", err)
	}
}

// EmailCommand is an address change as a manager submitted it.
type EmailCommand struct {
	TenantID   uuid.UUID
	EmployeeID uuid.UUID
	// ActorID is the admin making the change, from the signed panel session and
	// NEVER from the request (requireIDs refuses the zero value).
	ActorID uuid.UUID
	// Email is RAW. "" (after trimming) means "remove the address on file".
	Email string
}

// EmailChange is what a written change did.
type EmailChange struct {
	EmployeeID uuid.UUID
	// HadEmail and HasEmail are whether an address was on file before and after.
	HadEmail bool
	HasEmail bool
	// Retired is how many invitations the change retired (cancelled_at), counted
	// across both of ChangeEmail's cancellations.
	Retired int
}

// emailChangeDetail is the trail row's jsonb payload.
//
// 🔴 NO ADDRESS, OLD OR NEW — ADR 0022 §7: the row shows THAT the address changed,
// not what it is. The booleans answer "was there one before, is there one now" and
// the count answers "did this kill a link somebody was holding" without storing the
// address. Explicit values, no omitempty: a key that is absent cannot be told apart
// from a false or a zero (deletedDetail's lesson).
type emailChangeDetail struct {
	HadEmail bool `json:"had_email"`
	HasEmail bool `json:"has_email"`
	Retired  int  `json:"retired_invitations"`
}

// ChangeEmail writes (or clears) the address on file, retires every invitation of
// this person that could still be spent, and records the act — in ONE transaction.
//
// 🔴 THE ORDER OF THE FIVE STEPS IS THE CONCURRENCY DESIGN, and each step is there
// for a measured reason (staffemail_db_test.go drives each one):
//
//	1 retire pending invitations   takes the invitation rows' locks FIRST, the order
//	                               the activation statement uses (its CTE locks the
//	                               invitation, then it updates the employee). Locking
//	                               the employee first and the invitations second
//	                               DEADLOCKS against an activation in flight.
//	2 lock the employee FOR UPDATE the mode that conflicts with the FOR KEY SHARE a
//	                               concurrent CreateInvite's foreign-key check holds,
//	                               so this waits for an invitation that is being
//	                               inserted right now to commit (or roll back).
//	3 retire pending invitations   AGAIN, now that step 2 has waited: an invitation
//	                               committed while step 1 could not see it is retired
//	                               here. Without it that invitation survives the change.
//	4 write the address            compared against the value step 2 LOCKED, so two
//	                               managers saving the same address produce one write.
//	5 the trail row                RecordTx, inside the transaction (Trail's rule):
//	                               a change with no row, or a row with no change, are
//	                               both false statements on an append-only table.
//
// Any refusal after step 1 returns an error, and WithTenant rolls the whole
// transaction back — including step 1's retirements — so a refused change retires
// nothing.
//
// ⚠️ WHAT IT DOES NOT CLOSE, COUNTED (ADR 0022's EM-6 note): an invitation INSERTED
// after this transaction commits is not retired by it — it was issued after the
// change, and which address it is sent to is EM-7's to decide (read the address
// inside the issuing transaction, after the insert). And an invitation issued AND
// spent inside this transaction's few statements would deadlock with step 3; one of
// the two is aborted and nothing half-done is kept.
func (s *Staff) ChangeEmail(ctx context.Context, c EmailCommand) (EmailChange, error) {
	if err := requireIDs(c.TenantID, c.EmployeeID, c.ActorID); err != nil {
		return EmailChange{}, err
	}
	email, err := deliverableEmail(c.Email)
	if err != nil {
		return EmailChange{}, err
	}

	out := EmailChange{EmployeeID: c.EmployeeID, HasEmail: email != nil}
	err = s.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		retire := store.CancelPendingInvitesForEmployeeParams{
			TenantID:   c.TenantID,
			EmployeeID: c.EmployeeID,
		}
		first, err := q.CancelPendingInvitesForEmployee(ctx, retire)
		if err != nil {
			return fmt.Errorf("retire invitations: %w", err)
		}
		before, err := q.LockEmployeeForEmailChange(ctx, store.LockEmployeeForEmailChangeParams{
			TenantID: c.TenantID,
			ID:       c.EmployeeID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnknownEmployee
			}
			return fmt.Errorf("lock employee: %w", err)
		}
		if sameAddress(before.Email, email) {
			return ErrSameEmail
		}
		second, err := q.CancelPendingInvitesForEmployee(ctx, retire)
		if err != nil {
			return fmt.Errorf("retire invitations: %w", err)
		}
		if _, err := q.SetEmployeeEmail(ctx, store.SetEmployeeEmailParams{
			Email:    email,
			TenantID: c.TenantID,
			ID:       c.EmployeeID,
		}); err != nil {
			if isEmailTaken(err) {
				return ErrEmailTaken
			}
			if errors.Is(err, pgx.ErrNoRows) {
				// Unreachable after step 2 found and locked the row; answered as the
				// sentinel rather than a 500 should a future edit make it reachable.
				return ErrUnknownEmployee
			}
			return fmt.Errorf("set email: %w", err)
		}
		out.HadEmail = before.Email != nil
		out.Retired = len(first) + len(second)

		if _, err := s.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID,
			ActorID:  &c.ActorID,
			Action:   ActionEmployeeEmailChanged,
			Target:   c.EmployeeID.String(),
			Detail: emailChangeDetail{
				HadEmail: out.HadEmail,
				HasEmail: out.HasEmail,
				Retired:  out.Retired,
			},
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return EmailChange{}, wrap("change email", err)
	}
	// 🔴 THE ADDRESS IS NOT LOGGED, only whether there is one (Add's rule, §4.7/§7).
	s.log.Info("employee email changed",
		"employee_id", c.EmployeeID, "actor_id", c.ActorID,
		"has_email", out.HasEmail, "retired_invitations", out.Retired)
	return out, nil
}

// deliverableEmail cleans a submitted address for STORAGE UNDER THE SEND RULE.
// "" (after trimming) means "no address" and yields nil, which reaches the column as
// NULL — optionalEmail's fold, for its reason: the unique index is partial, so ”
// would collide and NULL does not.
//
// 🔴 IT IS STRICTER THAN optionalEmail ON PURPOSE. optionalEmail (the add form, M6-13)
// keeps the weak storage rule ADR 0022 §4 records as accepted; this path exists for
// the address an invitation will be sent to, so it refuses exactly what
// internal/mail's Send would refuse — via mail.ValidRecipient, the same function, not
// a copy. TestValidRecipient_AgreesWithSend holds that function to Send's answer.
func deliverableEmail(raw string) (*string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, nil
	}
	if !mail.ValidRecipient(s) {
		return nil, ErrEmployeeEmail
	}
	return &s, nil
}

// sameAddress is byte equality over "no address" and an address (see ErrSameEmail).
func sameAddress(onFile, requested *string) bool {
	switch {
	case onFile == nil && requested == nil:
		return true
	case onFile == nil || requested == nil:
		return false
	default:
		return *onFile == *requested
	}
}
