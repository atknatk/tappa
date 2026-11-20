package tenant

// staffapproval.go -- the PANEL'S HALF of the manager-approved activation (M16-23; migration 00115).
// A phone that cannot tap a plaque opened a request (internal/invite approval.go, the phone's half);
// the panel lists the open requests with their six-digit pairing codes, and an owner or a manager
// approves or rejects one. The phone collects an approval on its next status read.
//
// WHO MAY (SEÇİM, the card): any admin of the business, owner or manager -- the act is an activation
// link's completion, and sending a link is open to both roles (employeeactions.go's invite). A
// manager runs the venue the phone is standing in; base:authz-manager is not involved (like the live
// QR screens and the plaque acts, these never went through the policy engine).
//
// 🔴 NOBODY APPROVES THEIR OWN PHONE (ADR 0037's spirit, the card; SEÇİM: no owner exemption). The
// deciding account's address (admin_users.email, the identity's mirror for a linked account) equal
// to the person's address (employees.email) is "self", read inside the decision's locked statement,
// and refused -- approval AND rejection, ADR 0037's table. ADR 0037 exempts a sole OWNER from its
// record rule because a flagged record has nobody else to decide it; a phone has another way in
// (a plaque tap, or a live QR screen the owner can pair), so the exemption's reason does not hold
// here. The check sees only what is written: a person with no address on file, or a manager whose
// account carries another address, is not caught (ADR 0037 §3 option C's named weakness).
//
// ONE WAY: a request is decided once (00115's trigger); the decision and its trail row share one
// transaction, so either both exist or neither. The pairing code is never in the trail or a log.

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

// The trail rows of a decision, under `activation.%` with the phone's own rows.
const (
	ActionActivationApproved = "activation.approved"
	ActionActivationRejected = "activation.rejected"
	// ActionActivationApprovalRefused is a refused decision that changed nothing: the self-approval
	// rule. §4.6 wants the attempt visible, so it is written -- the other refusals (no such request,
	// no longer open) are ordinary stale pages and write nothing.
	ActionActivationApprovalRefused = "activation.approval_refused"
)

// Decision refusals.
var (
	// ErrUnknownApproval: no request with that id in this business.
	ErrUnknownApproval = errors.New("tenant: no such activation request")
	// ErrApprovalClosed: the request was decided, withdrawn by a newer one, ran out of time, or its
	// invitation can no longer be spent by the phone that asked (used, cancelled, consent moved).
	ErrApprovalClosed = errors.New("tenant: the activation request is no longer open")
	// ErrSelfApproval: the deciding account is the person's own (the address rule above).
	ErrSelfApproval = errors.New("tenant: an administrator cannot decide their own phone")
)

// PendingApproval is one open request as the panel lists it. PairingCode is shown and only shown.
type PendingApproval struct {
	ID           uuid.UUID
	EmployeeID   uuid.UUID
	EmployeeName string
	// LocationName is the person's home venue, "" for none (K-14).
	LocationName string
	// Client is "browser" or "app".
	Client      string
	PairingCode string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// PendingApprovals lists the business's open requests, oldest first.
func (s *Staff) PendingApprovals(ctx context.Context, tenantID uuid.UUID) ([]PendingApproval, error) {
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant: pending approvals: tenant is required")
	}
	var rows []store.ListOpenActivationRequestsRow
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		rows, e = store.New(tx).ListOpenActivationRequests(ctx, tenantID)
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("tenant: pending approvals: %w", err)
	}
	out := make([]PendingApproval, 0, len(rows))
	for _, r := range rows {
		p := PendingApproval{
			ID: r.ID, EmployeeID: r.EmployeeID, EmployeeName: r.FullName, Client: r.Client,
			PairingCode: r.PairingCode, CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt,
		}
		if r.LocationName != nil {
			p.LocationName = *r.LocationName
		}
		out = append(out, p)
	}
	return out, nil
}

// DecideApprovalCommand is one press of Approve or Reject.
type DecideApprovalCommand struct {
	TenantID  uuid.UUID
	RequestID uuid.UUID
	ActorID   uuid.UUID
	Approve   bool
}

// DecidedApproval is what a decision did.
type DecidedApproval struct {
	EmployeeID uuid.UUID
	Approved   bool
}

// approvalDetail is the trail's detail: ids, the decision and the client -- never the pairing code,
// never the invitation's code or binding (§7, the card).
type approvalDetail struct {
	RequestID string `json:"request_id"`
	Decision  string `json:"decision,omitempty"`
	Client    string `json:"client,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// DecideApproval approves or rejects one open request, and trails it, in ONE transaction: the
// request is read FOR UPDATE (two managers pressing at once queue, the second is ErrApprovalClosed),
// the self-approval rule and the request's state are judged on that locked read, and the one-way
// UPDATE and the audit row follow.
func (s *Staff) DecideApproval(ctx context.Context, c DecideApprovalCommand) (DecidedApproval, error) {
	if c.RequestID == uuid.Nil {
		return DecidedApproval{}, ErrUnknownApproval
	}
	if err := requireIDs(c.TenantID, c.RequestID, c.ActorID); err != nil {
		return DecidedApproval{}, err
	}
	decision := "rejected"
	action := ActionActivationRejected
	if c.Approve {
		decision, action = "approved", ActionActivationApproved
	}
	var (
		out     DecidedApproval
		refusal error
	)
	err := s.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		row, e := q.LockActivationRequestForDecision(ctx, store.LockActivationRequestForDecisionParams{
			AdminUserID: c.ActorID, TenantID: c.TenantID, ID: c.RequestID,
		})
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrUnknownApproval
		}
		if e != nil {
			return fmt.Errorf("lock request: %w", e)
		}
		out.EmployeeID = row.EmployeeID
		if row.Self {
			// Refused, and the refusal is the one decision outcome that is trailed without a
			// change: an administrator trying to let their own phone in is worth a row (§4.6).
			refusal = ErrSelfApproval
			_, e = s.trail.RecordTx(ctx, tx, audit.Event{
				TenantID: c.TenantID,
				ActorID:  &c.ActorID,
				Action:   ActionActivationApprovalRefused,
				Target:   row.EmployeeID.String(),
				Detail:   approvalDetail{RequestID: row.ID.String(), Decision: decision, Client: row.Client, Reason: "self_approval"},
			})
			return e
		}
		if row.Decision != nil || row.WithdrawnAt != nil || row.Expired || !row.InviteUsable {
			return ErrApprovalClosed
		}
		done, e := q.DecideActivationRequest(ctx, store.DecideActivationRequestParams{
			Decision: decision, DecidedBy: &c.ActorID, TenantID: c.TenantID, ID: c.RequestID,
		})
		if errors.Is(e, pgx.ErrNoRows) {
			// Locked a moment ago and open: the expiry crossed between the read and the write.
			return ErrApprovalClosed
		}
		if e != nil {
			return fmt.Errorf("decide: %w", e)
		}
		out.Approved = c.Approve
		_, e = s.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID,
			ActorID:  &c.ActorID,
			Action:   action,
			Target:   done.EmployeeID.String(),
			Detail:   approvalDetail{RequestID: done.ID.String(), Decision: decision, Client: row.Client},
		})
		return e
	})
	switch {
	case err != nil && (errors.Is(err, ErrUnknownApproval) || errors.Is(err, ErrApprovalClosed)):
		return DecidedApproval{}, err
	case err != nil:
		return DecidedApproval{}, fmt.Errorf("tenant: decide approval: %w", err)
	case refusal != nil:
		s.log.Warn("activation approval refused: the deciding account is the person's own",
			"employee_id", out.EmployeeID, "actor_id", c.ActorID, "request_id", c.RequestID)
		return DecidedApproval{EmployeeID: out.EmployeeID}, refusal
	}
	s.log.Info("activation request decided", "employee_id", out.EmployeeID, "actor_id", c.ActorID,
		"request_id", c.RequestID, "decision", decision)
	return out, nil
}
