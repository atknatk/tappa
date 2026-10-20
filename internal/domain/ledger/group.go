package ledger

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// group.go -- GRP-1: the panel's Group section (the user's decision of 2026-10-19; ADR 0027's GRP-1
// note). A business that is a LIVE member of a holding sees, read only: the holding's name and its
// own start, the live members (name, company code, joined), the partner staff's records on its own
// plaques and venues (the reports section's projection reader, the latest first) and how many of its
// own records name a partner's venue. Running the holding -- creating it, adding or removing a
// member -- stays on the operator's side.
//
// 🔴 THE HOLDING ITSELF IS READ THROUGH ONE CLOSED-LIST FUNCTION, holding_group (migration 00062):
// a name, a code and a date of a member, nothing else (K-1d) -- no VAT number, no address, no e-mail,
// no headcount, no plan, no API switch, and no member that has left. The two other reads are this
// business's OWN rows under its own RLS: site_punches (the projection of a partner's taps on its
// plaques -- the outcome, never the reason: K-1h) and transactions (its staff at a partner's venue).
//
// 🔴 THE SECTION EXISTS ONLY FOR A LIVE MEMBER. InGroup is the chrome's question (is the tab drawn);
// a business in no holding, and one whose membership has ended, is answered "no" with one statement
// and reads nothing else.

// GroupWindowDays is how far back the section's two lists of records reach.
const GroupWindowDays = 30

// GroupMember is one live member of the holding, as the section lists it.
type GroupMember struct {
	Name string
	// Code is the member's company code, "" when it has none (K-22).
	Code   string
	Joined time.Time
	// Self marks this business's own row.
	Self bool
}

// Group is one holding the business is a live member of -- in practice one (op_add_holding_member
// refuses a second live membership); the screen lists whatever the function answers.
type Group struct {
	Name string
	// Since is this business's own live membership's start.
	Since   time.Time
	Members []GroupMember
}

// GroupScreen is the Group section's whole read.
type GroupScreen struct {
	Queried bool
	// Member is false for a business in no holding (or whose membership has ended): nothing else
	// below was read.
	Member bool
	Groups []Group
	Zone   *time.Location
	// Window is the period the two record figures cover: the last GroupWindowDays days, to now.
	Window Period
	// Partners is a partner business's staff's records at this business's plaques and venues in the
	// window, the latest first, at most PartnerTapCap listed (More past it) -- the reports section's
	// reader and fold. Hours are their employer's: nothing here is summed.
	Partners PartnerTaps
	// AtPartnerVenues is how many of this business's own records in the window name a partner's
	// venue (its staff at a peer's plaque, or its manager's record for one).
	AtPartnerVenues int64
}

// InGroup says whether the business is a live member of a holding: the chrome's question (the Group
// tab is drawn only for a member). One statement; a failed read is an error, never a "no".
func (r *Reader) InGroup(ctx context.Context, tenantID uuid.UUID) (bool, error) {
	var in bool
	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := db.HoldingOn(tx).Group(ctx)
		if err != nil {
			return err
		}
		in = len(rows) > 0
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("ledger: in group: %w", err)
	}
	return in, nil
}

// Group loads the Group section. ONE TRANSACTION: the holding first; a business that is no live
// member stops there (Member false, no other statement), a member's two record reads follow.
func (r *Reader) Group(ctx context.Context, tenantID uuid.UUID) (GroupScreen, error) {
	var out GroupScreen
	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := db.HoldingOn(tx).Group(ctx)
		if err != nil {
			return err
		}
		out.Queried = true
		if len(rows) == 0 {
			return nil
		}
		out.Member = true
		out.Groups = groupsOf(rows)

		q := store.New(tx)
		clock, err := q.GetTenantClock(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("load tenant clock: %w", err)
		}
		out.Zone = r.loadZone(clock.Timezone)
		now := time.Now()
		out.Window = Period{From: now.AddDate(0, 0, -GroupWindowDays), To: now}
		if out.Partners, err = partnerTaps(ctx, q, tenantID, out.Window, true); err != nil {
			return err
		}
		n, err := q.CountRecordsAtPartnerVenues(ctx, store.CountRecordsAtPartnerVenuesParams{
			TenantID: tenantID,
			FromAt:   out.Window.From,
			UntilAt:  out.Window.To,
		})
		if err != nil {
			return fmt.Errorf("count the records at partner venues: %w", err)
		}
		out.AtPartnerVenues = n
		return nil
	})
	if err != nil {
		return GroupScreen{}, fmt.Errorf("ledger: group: %w", err)
	}
	return out, nil
}

// groupsOf folds holding_group's rows (ordered by holding, the caller's own row first) into one Group
// per holding, in the order the rows arrive.
func groupsOf(rows []db.HoldingGroupMember) []Group {
	var out []Group
	at := map[uuid.UUID]int{}
	for _, r := range rows {
		i, ok := at[r.HoldingID]
		if !ok {
			i = len(out)
			at[r.HoldingID] = i
			out = append(out, Group{Name: r.HoldingName})
		}
		m := GroupMember{Name: r.MemberName, Joined: r.JoinedAt, Self: r.IsSelf}
		if r.APICode != nil {
			m.Code = *r.APICode
		}
		if r.IsSelf {
			out[i].Since = r.JoinedAt
		}
		out[i].Members = append(out[i].Members, m)
	}
	return out
}
