package claim

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// Card is the one claim a result screen asks for (ADR 0032 §C.1): which half, about which record,
// the window the answer must fall in, and the server's prefill (nil: the field is empty and
// required). M15-17 renders it and signs Kind and Anchor into the claim context; Submit computes the
// prefill again under the lock and never reads the form's.
type Card struct {
	Kind      Kind
	AnchorID  uuid.UUID
	AnchorAt  time.Time
	Window    Window
	Prefilled *time.Time
}

// Offer is the card for the result screen of the person's record tapID, or nil when there is none
// (ADR 0032 §C.1; Chain.Offer): no card for a refused or ignored tap (no direction -- not in the
// chain, so never a showing), none for an anchor without a business day (Ü-Y2), none once a claim
// was sent for it, none past K4's two showings, none with no window left inside manual.MaxBackdate.
//
// IT IS A READ AND ONLY A READ: no lock, no row. Its place is M15-17's -- after the tap's commit, in
// a bounded time, never inside the tap's transaction (G-O5) -- and an error from it drops the card,
// never the result screen.
func (s *Service) Offer(ctx context.Context, tenantID, employeeID, tapID uuid.UUID) (*Card, error) {
	if tenantID == uuid.Nil || employeeID == uuid.Nil || tapID == uuid.Nil {
		return nil, ErrUnknownAnchor
	}
	now := s.clock()
	var out *Card
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		p, err := s.readPerson(ctx, q, tenantID, employeeID, now)
		if err != nil {
			return fmt.Errorf("read the person: %w", err)
		}
		t, err := s.readAnchor(ctx, q, tenantID, employeeID, tapID, p.zone)
		if err != nil {
			return fmt.Errorf("read the tap: %w", err)
		}
		if t.direction == nil || t.practice {
			return nil // a refusal, an ignored tap or a training tap: no direction, no card
		}
		chain, err := readChain(ctx, q, tenantID, employeeID, t.at.Add(-chainBefore), t.at)
		if err != nil {
			return err
		}
		at := chain.Index(t.id)
		if at < 0 {
			return nil
		}
		ids := make([]uuid.UUID, 0, at+1)
		for _, e := range chain.events[:at+1] {
			ids = append(ids, e.ID)
		}
		claimedIDs, err := q.ListPunchClaimedAnchors(ctx, store.ListPunchClaimedAnchorsParams{
			TenantID: tenantID, EmployeeID: employeeID, AnchorIds: ids,
		})
		if err != nil {
			return fmt.Errorf("read the claimed records: %w", err)
		}
		claimed := make(map[uuid.UUID]bool, len(claimedIDs))
		for _, id := range claimedIDs {
			claimed[id] = true
		}
		cand, ok := chain.Offer(at, func(id uuid.UUID) bool { return claimed[id] }, p.cutoff, now)
		if !ok {
			return nil
		}
		a, err := s.readAnchor(ctx, q, tenantID, employeeID, cand.Anchor.ID, p.zone)
		if err != nil {
			return fmt.Errorf("read the anchor: %w", err)
		}
		out = &Card{Kind: cand.Kind, AnchorID: a.id, AnchorAt: a.at, Window: cand.Window,
			Prefilled: utcOrNil(Prefill(cand.Kind, a.at, a.shift, cand.Window))}
		return nil
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnknownAnchor
		}
		return nil, fmt.Errorf("claim: offer: %w", err)
	}
	return out, nil
}
