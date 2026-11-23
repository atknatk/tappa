package claim

// open.go -- the staff app's "Missing times" (M16-43, ADR 0050 (i); Q50 (1) (a), (2) (a), (4) (a)):
// every record of the person's last 7 days that would take a claim now, as the SAME Card the
// browser's result screen builds, and the person's own recent claims with their state.
//
// THE SAME RULES AS THE CARD, minus K4 (Q50 (2) (a)): K4 limits how often the result screen ASKS;
// a list the person opens is not asking. What K4 protected indirectly -- the still-open check-in is
// never asked about -- is kept explicitly (Chain.StillClosable): Open does not list it and Submit
// refuses it under the lock (ErrStillOpen).
//
// No lock and no row: Open only reads. Submit re-checks everything under the person's lock.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

const (
	// OpenLookback is how far back the list looks (Q50 (4) (a)): a week covers a weekly read.
	OpenLookback = 7 * 24 * time.Hour
	// OpenLimit bounds the list: newest first.
	OpenLimit = 20
	// SentLookback is how far back the person's own claims are shown.
	SentLookback = 30 * 24 * time.Hour
)

// SentState is a sent claim's state as the PERSON sees it -- not the manager queue's State.
type SentState string

const (
	SentPending  SentState = "pending"
	SentApproved SentState = "approved"
	SentRejected SentState = "rejected"
)

// SentClaim is one of the person's own claims.
type SentClaim struct {
	Kind       Kind
	AnchorAt   time.Time
	ClaimedAt  time.Time
	State      SentState
	ApprovedAt *time.Time
}

// Open lists the records of the last OpenLookback that take a claim now, newest first, at most
// OpenLimit. Empty for a person who is not active.
func (s *Service) Open(ctx context.Context, tenantID, employeeID uuid.UUID) ([]Card, error) {
	if tenantID == uuid.Nil || employeeID == uuid.Nil {
		return nil, ErrUnknownAnchor
	}
	now := s.clock()
	var out []Card
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		p, err := s.readPerson(ctx, q, tenantID, employeeID, now)
		if err != nil {
			return fmt.Errorf("read the person: %w", err)
		}
		if p.status != "active" {
			return nil
		}
		from := now.Add(-OpenLookback)
		// chainBefore before the first anchor, so each anchor's predecessor is in the chain (Offer's
		// read).
		chain, err := readChain(ctx, q, tenantID, employeeID, from.Add(-chainBefore), now)
		if err != nil {
			return err
		}
		ids := make([]uuid.UUID, 0, len(chain.events))
		for _, e := range chain.events {
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
		for i := len(chain.events) - 1; i >= 0 && len(out) < OpenLimit; i-- {
			e := chain.events[i]
			if e.At.Before(from) {
				break
			}
			if claimed[e.ID] || e.DayStart == nil {
				continue
			}
			kind, ok := chain.Missing(i)
			if !ok || kind == "" || chain.DayCutShort(i, p.cutoff) || chain.StillClosable(i, p.cutoff, now) {
				continue
			}
			w, ok := chain.WindowOf(i, p.cutoff, now)
			if !ok || w.Empty() {
				continue
			}
			a, err := s.readAnchor(ctx, q, tenantID, employeeID, e.ID, p.zone)
			if err != nil {
				return fmt.Errorf("read the anchor: %w", err)
			}
			if a.practice || a.direction == nil {
				continue
			}
			out = append(out, Card{Kind: kind, AnchorID: a.id, AnchorAt: a.at, Window: w,
				Prefilled: utcOrNil(Prefill(kind, a.at, a.shift, w)), Zone: p.zone})
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnknownAnchor
		}
		return nil, fmt.Errorf("claim: open: %w", err)
	}
	return out, nil
}

// Sent lists the person's own claims of the last SentLookback, newest first, with the business's
// zone for their clocks. ApprovedAt is kept only when showHours is true (Q50 (5) (b): the approved
// hour is a record and follows the owner's "show staff their hours" switch; the decision itself is
// the answer to the person's request).
func (s *Service) Sent(ctx context.Context, tenantID, employeeID uuid.UUID, showHours bool) ([]SentClaim, string, error) {
	if tenantID == uuid.Nil || employeeID == uuid.Nil {
		return nil, "", ErrUnknownAnchor
	}
	now := s.clock()
	var (
		out  []SentClaim
		zone string
	)
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		p, err := s.readPerson(ctx, q, tenantID, employeeID, now)
		if err != nil {
			return fmt.Errorf("read the person: %w", err)
		}
		zone = p.zone
		rows, err := q.ListOwnPunchClaims(ctx, store.ListOwnPunchClaimsParams{
			TenantID: tenantID, EmployeeID: employeeID, Since: now.Add(-SentLookback),
		})
		if err != nil {
			return fmt.Errorf("read the claims: %w", err)
		}
		for _, r := range rows {
			c := SentClaim{Kind: Kind(r.Kind), AnchorAt: r.AnchorAt, ClaimedAt: r.ClaimedAt, State: SentPending}
			if r.Outcome != nil {
				switch *r.Outcome {
				case "approved":
					c.State = SentApproved
					if showHours {
						c.ApprovedAt = utcOrNil(r.ApprovedAt)
					}
				case "rejected":
					c.State = SentRejected
				}
			}
			out = append(out, c)
		}
		return nil
	})
	if err != nil {
		return nil, "", fmt.Errorf("claim: sent: %w", err)
	}
	return out, zone, nil
}
