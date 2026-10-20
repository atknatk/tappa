package tenant

// staffsummary.go -- one person's summary for the People card (M12-20, panel redesign D3): the
// job title, the lifecycle stamps, the live browser sessions, and whether an activation link is
// still outstanding.
//
// 🔴 §4.7 -- WHAT A PersonSummary CANNOT CARRY. No e-mail address, no device label, no session id
// or token, no invitation id or code: GetPanelPersonSummary selects none of them and
// ListPendingInvitesForEmployee hands back an id this file drops. The activation link itself is
// never stored (only its hash is), so no read here could produce it; the card says when one was
// created and until when it works, and nothing that could be used.
//
// 🔴 §4.6 -- NEVER 0 FOR SOMETHING NOT MEASURED. Each part carries a Reading: the summary and the
// pending-link read are separate statements in one transaction, the second in its own savepoint,
// so a failed link read costs the activation box's date and never the summary.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// Reading is how far one figure's read got. internal/domain/ledger declares the same three states
// for the panel's other figures; they are spelled again here rather than imported for the reason
// the lifecycle words are (staff.go): a domain package importing another for one enum is a
// dependency the two do not otherwise have.
type Reading uint8

const (
	// NotQueried is the zero value: nobody asked, and the figure claims nothing.
	NotQueried Reading = iota
	// ReadFailed is a read that was asked and did not answer -- "could not be read", never 0.
	ReadFailed
	// Measured is a figure the database answered.
	Measured
)

// Known reports whether the figure was measured -- the only state in which a screen may print it.
func (r Reading) Known() bool { return r == Measured }

// PersonSummary is what the People card says about one person beyond their name and placement
// (tenant.Person carries those).
type PersonSummary struct {
	State Reading
	// Role is the job title (employees.role), "" when none was typed.
	Role string
	// Status is the lifecycle word, read in the same snapshot as the stamps so Since agrees
	// with it.
	Status string
	// Offered, Activated and Deactivated are employees.invited_at, activated_at and
	// deactivated_at, nil when never written. Offered is named for what it means because the
	// §4.7 field walker refuses any field holding "invite" (that is where a code would travel).
	//
	// ⚠️ Offered IS nil FOR EVERY PERSON ADDED IN THE PANEL: no statement of this product writes
	// invited_at (db/queries/employees.sql, CreateEmployee). The card prints the pending link's
	// own date (Link) or nothing -- never a date for an invitation that does not exist.
	Offered     *time.Time
	Activated   *time.Time
	Deactivated *time.Time
	// LiveDevices is how many of this person's browser sessions are not revoked -- the roster
	// row's count. A deactivated person keeps theirs (deactivating does not sign phones out,
	// user decision 2026-08-07).
	LiveDevices int
	// FirstSignedIn is when the oldest live session was created, LastUsed when a live one was
	// last used; both nil with no live session, and LastUsed nil too for a session never used.
	FirstSignedIn *time.Time
	LastUsed      *time.Time
	// Link is the outstanding activation link, read in its own savepoint.
	Link ActivationLink
}

// Since is the lifecycle stamp that matches Status, nil when it was never written -- the
// roster's rule (ledger.lifecycleStamp): one date, the one that belongs to the word beside it.
func (p PersonSummary) Since() *time.Time {
	switch p.Status {
	case statusInvited:
		return p.Offered
	case statusActive:
		return p.Activated
	case statusDeactivated:
		return p.Deactivated
	default:
		return nil
	}
}

// ActivationLink is whether an activation link of this person could still be used right now
// (ListPendingInvitesForEmployee: not consumed, not cancelled, not expired).
type ActivationLink struct {
	State Reading
	// Pending is how many are usable. A new one retires the earlier ones (migration 00012), so
	// more than one is a race the next issue closes; the count is carried rather than assumed.
	Pending int
	// Created and Expires are the NEWEST pending link's, nil when Pending is 0.
	Created *time.Time
	Expires *time.Time
}

// Summary reads one person's summary.
//
// THE ERROR IS ErrUnknownEmployee AND NOTHING ELSE: a foreign or invented id is "not on this
// roster", which the card already answers from Person. A read that FAILS is not an error here --
// it is State ReadFailed, logged with the tenant (§7), so the card stands without the summary.
func (s *Staff) Summary(ctx context.Context, tenantID, employeeID uuid.UUID) (PersonSummary, error) {
	if tenantID == uuid.Nil || employeeID == uuid.Nil {
		return PersonSummary{}, ErrUnknownEmployee
	}
	var out PersonSummary
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, err := store.New(tx).GetPanelPersonSummary(ctx, store.GetPanelPersonSummaryParams{
			TenantID:   tenantID,
			EmployeeID: employeeID,
		})
		if err != nil {
			return err
		}
		out = PersonSummary{
			State:         Measured,
			Role:          deref(row.Role),
			Status:        row.Status,
			Offered:       row.InvitedAt,
			Activated:     row.ActivatedAt,
			Deactivated:   row.DeactivatedAt,
			LiveDevices:   int(row.LiveSessions),
			FirstSignedIn: row.FirstSessionAt,
			LastUsed:      row.SessionLastUsedAt,
		}
		out.Link = s.activationLink(ctx, tx, tenantID, employeeID)
		return nil
	})
	switch {
	case err == nil:
		return out, nil
	case errors.Is(err, pgx.ErrNoRows):
		return PersonSummary{}, ErrUnknownEmployee
	default:
		s.log.Error("tenant: could not read a person's summary; the card will say so",
			"err", err, "tenant_id", tenantID)
		return PersonSummary{State: ReadFailed}, nil
	}
}

// activationLink reads the pending links inside a savepoint, so its failure cannot abort the
// summary's transaction (the COMMIT would otherwise turn it into a failed summary).
func (s *Staff) activationLink(ctx context.Context, tx pgx.Tx, tenantID, employeeID uuid.UUID) ActivationLink {
	link, err := func() (ActivationLink, error) {
		sp, err := tx.Begin(ctx) // SAVEPOINT
		if err != nil {
			return ActivationLink{}, fmt.Errorf("savepoint: %w", err)
		}
		rows, err := store.New(sp).ListPendingInvitesForEmployee(ctx, store.ListPendingInvitesForEmployeeParams{
			TenantID:   tenantID,
			EmployeeID: employeeID,
		})
		if err != nil {
			// A background context, for db.WithTenant's reason: ctx may be cancelled, and a
			// skipped ROLLBACK TO SAVEPOINT leaves the outer transaction aborted.
			_ = sp.Rollback(context.Background()) // ROLLBACK TO SAVEPOINT
			return ActivationLink{}, fmt.Errorf("list pending links: %w", err)
		}
		if err := sp.Commit(ctx); err != nil { // RELEASE SAVEPOINT
			return ActivationLink{}, fmt.Errorf("release savepoint: %w", err)
		}
		out := ActivationLink{State: Measured, Pending: len(rows)}
		if len(rows) > 0 {
			// The statement orders newest first.
			created, expires := rows[0].CreatedAt, rows[0].ExpiresAt
			out.Created, out.Expires = &created, &expires
		}
		return out, nil
	}()
	if err != nil {
		s.log.Error("tenant: could not read a person's pending activation link; the card will say so",
			"err", err, "tenant_id", tenantID)
		return ActivationLink{State: ReadFailed}
	}
	return link
}
