package tenant

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// THE PUNCH FEED'S READ END (M11 API-7; ADR 0028 §2, §4, §5): a page of one business's punch
// events, in the business's own feed order (seq), for one scope.
//
// The events are the database's (00049's triggers write them, into the TARGET tenant's feed); this
// reads them in the key's tenant alone -- WithTenant + an explicit tenant_id in both statements --
// so a read never crosses a tenant line (§2: "okuma tenant sınırını aşmaz"). A holding punch is two
// rows written at the event, each in the tenant that reads it, with the other side's identities
// copied in (K-32); nothing here joins across tenants.
//
// THE CURSOR IS A POSITION, NOTHING ELSE: the last seq the reader has seen. It names no tenant and
// no scope -- both come from the key and the request every time -- so a cursor carried to another
// tenant or another scope is just a position in THAT feed: it reads that feed's events after it,
// never the first tenant's. A position the feed has not reached is refused (ErrCursorAhead) rather
// than waited out: after a restore that skipped the runbook's counter step (ADR 0028 §5), the feed
// would otherwise sit silently below an integrator's cursor and hand it nothing until its own
// counter passed it.
//
// WHY A PAGE CAN BE SHORT, EVEN EMPTY, WITH MORE TO COME: the scope is a filter on the feed's order
// (a role the row carries), and a business whose feed is mostly the other scope's rows -- an
// employer whose people work at a peer's plaques, read with scope=location -- would make a page walk
// arbitrarily far to fill itself. A page reads at most PunchFeedWindow positions of the feed
// (starting at the first event after the cursor), so its cost is bounded whatever the mix; the
// window's end is the next cursor, and HasMore says the feed goes on.

// FeedRole is a scope: the role an event carries in its target's feed (ADR 0028 §4).
type FeedRole string

// The two scopes.
const (
	// FeedEmployer: the punch's employee is this business's -- its own punches anywhere.
	FeedEmployer FeedRole = "employer"
	// FeedLocation: the plaque is this business's -- every punch at its locations, a peer's
	// employees' included.
	FeedLocation FeedRole = "location"
)

// PunchFeedWindow is how many feed positions one page reads at most (see the header).
const PunchFeedWindow = 10000

// ErrCursorAhead: the cursor is past the last event of this business's feed -- not a position this
// feed ever handed out.
var ErrCursorAhead = errors.New("tenant: the cursor is past this business's feed")

// PunchEvent is one event as the API answers with it: the punch's identity and times and the
// snapshot written at the event (K-32). No address, coordinate, note, policy context or verdict --
// the table has none.
type PunchEvent struct {
	Seq        int64
	PunchID    uuid.UUID
	Version    int32
	Status     string
	PunchTime  time.Time
	RecordedAt time.Time
	Channel    string
	Direction  string

	EmployeeID      uuid.UUID
	EmployeeRef     *string
	EmployeeCode    *string
	EmployeeCompany *string

	LocationID      *uuid.UUID
	LocationRef     *string
	LocationName    *string
	LocationCompany *string

	TagUID *string
}

// PunchPage is one page: the events, the position to read after next, and whether the feed held
// more than this page at the moment it was read.
type PunchPage struct {
	Events  []PunchEvent
	Next    int64
	HasMore bool
}

// PunchFeed reads the feed.
type PunchFeed struct {
	data   Database
	window int64
}

// NewPunchFeed wires the reader.
func NewPunchFeed(data Database) (*PunchFeed, error) {
	if data == nil {
		return nil, errors.New("tenant: nil database")
	}
	return &PunchFeed{data: data, window: PunchFeedWindow}, nil
}

// PunchPage reads the events of role after the position after, at most limit of them.
func (f *PunchFeed) PunchPage(ctx context.Context, tenantID uuid.UUID, role FeedRole, after int64, limit int) (PunchPage, error) {
	switch {
	case tenantID == uuid.Nil:
		return PunchPage{}, errors.New("tenant: tenant id is required")
	case role != FeedEmployer && role != FeedLocation:
		return PunchPage{}, fmt.Errorf("tenant: unknown feed scope %q", role)
	case after < 0:
		return PunchPage{}, errors.New("tenant: a feed position is not negative")
	case limit < 1 || limit >= 1<<30:
		return PunchPage{}, errors.New("tenant: a page needs a size")
	}
	var page PunchPage
	err := f.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		// 1. The head FIRST (punchfeed.sql: the order is the proof).
		head, err := q.GetPunchFeedHead(ctx, store.GetPunchFeedHeadParams{TenantID: tenantID, AfterSeq: after})
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("the business has no feed counter row (ADR 0028 §5 says every business has one)")
		}
		if err != nil {
			return err
		}
		if after > head.LastSeq {
			return ErrCursorAhead
		}
		if head.NextSeq == 0 || head.NextSeq > head.LastSeq {
			// Nothing after the cursor that this snapshot's counter covers: caught up. (NextSeq above
			// LastSeq is impossible by §5 -- an event and its counter commit together -- and is read
			// as "not yet" rather than handed out past the counter.)
			page = PunchPage{Events: []PunchEvent{}, Next: head.LastSeq}
			return nil
		}
		// The window starts at the first event after the cursor (a restore's counter gap or a
		// long stretch is crossed in one step) and ends at the counter at the latest.
		upto := min(head.LastSeq, head.NextSeq-1+f.window)
		rows, err := q.ListPunchEvents(ctx, store.ListPunchEventsParams{
			TenantID: tenantID, AfterSeq: after, UptoSeq: upto, Role: string(role), PageSize: int32(limit + 1),
		})
		if err != nil {
			return err
		}
		page.Events = make([]PunchEvent, 0, min(len(rows), limit))
		for _, r := range rows[:min(len(rows), limit)] {
			page.Events = append(page.Events, punchEventOf(r))
		}
		if len(rows) > limit {
			// The window holds more of this scope: the next page starts after the last one handed out.
			page.Next, page.HasMore = page.Events[limit-1].Seq, true
			return nil
		}
		// The whole window was read: its end is the next position, and the feed goes on past it
		// when the counter does.
		page.Next, page.HasMore = upto, upto < head.LastSeq
		return nil
	})
	if errors.Is(err, ErrCursorAhead) {
		return PunchPage{}, ErrCursorAhead
	}
	if err != nil {
		return PunchPage{}, fmt.Errorf("tenant: read the punch feed: %w", err)
	}
	return page, nil
}

func punchEventOf(r store.ListPunchEventsRow) PunchEvent {
	return PunchEvent{
		Seq: r.Seq, PunchID: r.PunchID, Version: r.Version, Status: r.Status, PunchTime: r.PunchTime, RecordedAt: r.RecordedAt,
		Channel: r.Channel, Direction: r.Direction,
		EmployeeID: r.EmployeeID, EmployeeRef: r.EmployeeRef, EmployeeCode: r.EmployeeCode, EmployeeCompany: r.EmployeeCompany,
		LocationID: r.LocationID, LocationRef: r.LocationRef, LocationName: r.LocationName, LocationCompany: r.LocationCompany,
		TagUID: r.TagUid,
	}
}
