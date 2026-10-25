package claim

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/domain/manual"
	"github.com/atknatk/tappa/internal/store"
)

// Claim is what the employee sent, after the handler validated it (§7): their tenant and identity
// from the session, the anchor and the kind from the signed claim context, the moment from the form
// (the earlier candidate of a repeated autumn hour -- the handler's, §C.3).
type Claim struct {
	TenantID   uuid.UUID
	EmployeeID uuid.UUID
	AnchorID   uuid.UUID
	Kind       Kind
	ClaimedAt  time.Time
}

// Submitted is the claim as written.
type Submitted struct {
	ID        uuid.UUID
	CreatedAt time.Time
	// Prefilled is the prefill the SERVER computed (the shift's real end or start inside the
	// window), nil without one; DefaultUnchanged is Prefilled == ClaimedAt.
	Prefilled        *time.Time
	DefaultUnchanged bool
	AuditID          uuid.UUID
}

// submitDetail is claim.submitted's audit detail: ids, a kind, instants the server holds. No note
// (a claim has none), no name, no address.
type submitDetail struct {
	ClaimID          string `json:"claim_id"`
	AnchorID         string `json:"anchor_id"`
	Kind             string `json:"kind"`
	ClaimedAt        string `json:"claimed_at"`
	PrefilledAt      string `json:"prefilled_at,omitempty"`
	DefaultUnchanged bool   `json:"default_unchanged"`
	CreatedAt        string `json:"created_at"`
}

// Submit writes the employee's claim (ADR 0032 §C.3, §C.5), under the person's tap lock, in one
// transaction with its audit row:
//
//	the person is active                                   else ErrNotActive
//	the anchor is this person's record                     else ErrUnknownAnchor
//	no claim on it yet (read, then the 23505 UNIQUE)       else ErrAlreadySent
//	it still misses exactly that half (the reader's class) else ErrResolved
//	the moment is inside its window                        else ErrOutsideWindow
//	the moment is within manual.MaxBackdate                else ErrExpired
//
// The prefill is computed here, from the anchor's shift and the window, whatever the form showed.
// The audit row's ActorID is nil: the employee acts on their own behalf (internal/audit's Event).
//
// THE LOCK IS THE TAP'S (LockEmployeeForTap), so a tap of the person's is decided either before the
// claim's checks or after its commit, never between: the claim is judged on the chain as it stands
// when it is written.
func (s *Service) Submit(ctx context.Context, c Claim) (Submitted, error) {
	switch {
	case c.TenantID == uuid.Nil:
		return Submitted{}, errors.New("claim: tenant id is required")
	case c.EmployeeID == uuid.Nil, c.AnchorID == uuid.Nil:
		return Submitted{}, ErrUnknownAnchor
	case c.ClaimedAt.IsZero():
		return Submitted{}, ErrOutsideWindow
	}
	if _, ok := ParseKind(string(c.Kind)); !ok {
		return Submitted{}, ErrResolved
	}
	now := s.clock()
	var out Submitted
	err := s.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if err := q.LockEmployeeForTap(ctx, store.LockEmployeeForTapParams{TenantID: c.TenantID, EmployeeID: c.EmployeeID}); err != nil {
			return fmt.Errorf("lock employee: %w", err)
		}
		p, err := s.readPerson(ctx, q, c.TenantID, c.EmployeeID, now)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnknownAnchor
		}
		if err != nil {
			return fmt.Errorf("read the person: %w", err)
		}
		if p.status != "active" {
			return ErrNotActive
		}
		a, err := s.readAnchor(ctx, q, c.TenantID, c.EmployeeID, c.AnchorID, p.zone)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnknownAnchor
		}
		if err != nil {
			return fmt.Errorf("read the anchor: %w", err)
		}
		// A record that already carries a claim answers "Sent" whatever the second form says (§C.5:
		// a second send is the Sent screen, never a refusal of its moment); the UNIQUE below is the
		// braces for two sends that race past this read -- the lock makes that unreachable.
		sent, err := q.ListPunchClaimedAnchors(ctx, store.ListPunchClaimedAnchorsParams{
			TenantID: c.TenantID, EmployeeID: c.EmployeeID, AnchorIds: []uuid.UUID{a.id},
		})
		if err != nil {
			return fmt.Errorf("read the claims on the anchor: %w", err)
		}
		if len(sent) > 0 {
			return ErrAlreadySent
		}
		chain, err := chainAround(ctx, q, c.TenantID, c.EmployeeID, a.at)
		if err != nil {
			return err
		}
		i := chain.Index(a.id)
		if i < 0 {
			return ErrResolved // not a directed, non-practice record: it misses nothing
		}
		kind, ok := chain.Missing(i)
		if !ok || kind != c.Kind {
			return ErrResolved
		}
		w, ok := chain.WindowOf(i, p.cutoff, now)
		if !ok || !w.Contains(c.ClaimedAt) {
			return ErrOutsideWindow
		}
		if now.Sub(c.ClaimedAt) > manual.MaxBackdate {
			return ErrExpired
		}
		prefilled := Prefill(kind, a.at, a.shift, w)
		if s.afterCheck != nil {
			s.afterCheck()
		}
		row, err := q.InsertPunchClaim(ctx, store.InsertPunchClaimParams{
			Kind: string(kind), ClaimedAt: c.ClaimedAt.UTC(), PrefilledAt: utcOrNil(prefilled),
			AnchorTransactionID: a.id, TenantID: c.TenantID, EmployeeID: c.EmployeeID,
		})
		if err != nil {
			return insertClaimError(err)
		}
		out = Submitted{ID: row.ID, CreatedAt: row.CreatedAt, Prefilled: utcOrNil(prefilled),
			DefaultUnchanged: prefilled != nil && prefilled.Equal(c.ClaimedAt)}
		d := submitDetail{ClaimID: row.ID.String(), AnchorID: a.id.String(), Kind: string(kind),
			ClaimedAt: rfc3339(c.ClaimedAt), DefaultUnchanged: out.DefaultUnchanged, CreatedAt: rfc3339(row.CreatedAt)}
		if prefilled != nil {
			d.PrefilledAt = rfc3339(*prefilled)
		}
		out.AuditID, err = s.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID, ActorID: nil, Action: ActionSubmitted, Target: c.EmployeeID.String(), Detail: d,
		})
		return err
	})
	if err != nil {
		if isTyped(err) {
			return Submitted{}, err
		}
		return Submitted{}, fmt.Errorf("claim: submit: %w", err)
	}
	s.log.Info("punch claim submitted", "claim_id", out.ID, "anchor_id", c.AnchorID, "employee_id", c.EmployeeID,
		"kind", string(c.Kind), "default_unchanged", out.DefaultUnchanged)
	return out, nil
}

// insertClaimError maps InsertPunchClaim's refusals (db/queries/punchclaims.sql).
func insertClaimError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnknownAnchor
	}
	switch code, constraint, _ := pgCode(err); {
	case code == "23505" && constraint == "punch_claims_one_per_anchor":
		return ErrAlreadySent
	case code == "23514":
		return ErrRefused
	}
	return err
}

func utcOrNil(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// isTyped reports whether err is one of this package's sentences (or manual's venue refusal).
func isTyped(err error) bool {
	for _, e := range []error{ErrUnknownAnchor, ErrUnknownClaim, ErrNotActive, ErrResolved, ErrOutsideWindow, ErrExpired,
		ErrAlreadySent, ErrAlreadyDecided, ErrSelfDecision, ErrNeedsConfirmation, ErrRefused, manual.ErrVenue} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}
