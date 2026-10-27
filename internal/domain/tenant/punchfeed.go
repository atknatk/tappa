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
	// DirectionSource is who chose Direction (M15-13, migration 00067; ADR 0032 §B.4): "auto" -- the
	// direction Tappa computed -- or "employee" -- the person asked for the other one and it was
	// recorded. nil on an event the feed wrote before it recorded this (the column is never
	// back-filled). The trigger that wrote the event already applied the reader rule
	// (tap.RecordedDirectionSource), so nothing here reads it again.
	DirectionSource *string

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
		if err := boundAPIRead(ctx, tx); err != nil {
			return err
		}
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
		Channel: r.Channel, Direction: r.Direction, DirectionSource: r.DirectionSource,
		EmployeeID: r.EmployeeID, EmployeeRef: r.EmployeeRef, EmployeeCode: r.EmployeeCode, EmployeeCompany: r.EmployeeCompany,
		LocationID: r.LocationID, LocationRef: r.LocationRef, LocationName: r.LocationName, LocationCompany: r.LocationCompany,
		TagUID: r.TagUid,
	}
}

// THE HISTORY QUERY (M11 API-7b; ADR 0028 §5, plan §2 item 18 and §5 API-7b): the events whose
// punch_time is in [From, To), in (punch_time, seq) order, page by page.
//
// THE GUARANTEE IS A SNAPSHOT'S. punch_time is what the phone declared, so an event can be committed
// into a stretch of that order a reader has already passed (an offline queue's tap, an approved
// flag days later). A walk therefore answers the events of ONE snapshot: the first page reads the
// counter S0 before anything else, every page answers seq <= S0 only, and the walk is complete and
// free of repeats for that set (punchfeed.sql has the proof). An event above S0 is not this walk's:
// the feed (PunchPage) is the only read that delivers every event for certain, and the same range
// asked again later can hold more.
//
// The window bounds a page here as it does in the feed: at most window positions of the range after
// the cursor (any scope), so a sparse scope costs a bounded amount per page; a page can then be
// short, even empty, while HasMore says the range goes on.

// HistoryPosition is where a history walk stands: its snapshot S0 and the last (punch_time, seq) it
// has read -- (From, 0) before the first event.
type HistoryPosition struct {
	S0   int64
	Time time.Time
	Seq  int64
}

// HistoryQuery is one page's request. First: no cursor yet -- S0 is read now and At is ignored.
type HistoryQuery struct {
	From, To time.Time
	First    bool
	At       HistoryPosition
}

// HistoryPage is one page of a history walk.
type HistoryPage struct {
	Events  []PunchEvent
	Next    HistoryPosition
	HasMore bool
}

// MaxHistoryRange is the longest [From, To) a history query takes: ADR 0028 §5's 31 days and one
// hour, so that a local calendar month is one query in every time zone -- the month whose
// daylight-saving change gives an hour back (October in Malta: 2026-10-01T00:00+02:00 to
// 2026-11-01T00:00+01:00) is 31 days and an hour long (API-7b review, the user-facing reading of
// "31 days").
const MaxHistoryRange = 31*24*time.Hour + time.Hour

// ErrHistoryRange: From is not before To, or the range is longer than MaxHistoryRange, or the
// position is not inside it.
var ErrHistoryRange = errors.New("tenant: not a history range this query takes")

// HistoryPage reads one page of role's events in [q.From, q.To).
func (f *PunchFeed) HistoryPage(ctx context.Context, tenantID uuid.UUID, role FeedRole, q HistoryQuery, limit int) (HistoryPage, error) {
	from, to := q.From.UTC(), q.To.UTC()
	at := HistoryPosition{Time: from}
	if !q.First {
		at = HistoryPosition{S0: q.At.S0, Time: q.At.Time.UTC(), Seq: q.At.Seq}
	}
	switch {
	case tenantID == uuid.Nil:
		return HistoryPage{}, errors.New("tenant: tenant id is required")
	case role != FeedEmployer && role != FeedLocation:
		return HistoryPage{}, fmt.Errorf("tenant: unknown feed scope %q", role)
	case limit < 1 || limit >= 1<<30:
		return HistoryPage{}, errors.New("tenant: a page needs a size")
	case !from.Before(to) || to.Sub(from) > MaxHistoryRange:
		return HistoryPage{}, ErrHistoryRange
	// The position's seq may be past S0: the window counts every position (punchfeed.sql).
	case !q.First && (at.S0 < 0 || at.Seq < 0 || at.Time.Before(from) || !at.Time.Before(to)):
		return HistoryPage{}, ErrHistoryRange
	}
	var page HistoryPage
	err := f.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if err := boundAPIRead(ctx, tx); err != nil {
			return err
		}
		qs := store.New(tx)
		// 1. The counter FIRST (punchfeed.sql: S0 before the window and the page).
		last, err := qs.GetPunchFeedCounter(ctx, tenantID)
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("the business has no feed counter row (ADR 0028 §5 says every business has one)")
		}
		if err != nil {
			return err
		}
		if q.First {
			at.S0 = last
		} else if at.S0 > last {
			return ErrCursorAhead
		}
		win, err := qs.GetPunchHistoryWindow(ctx, store.GetPunchHistoryWindowParams{
			TenantID: tenantID, FromTime: from, ToTime: to, AfterTime: at.Time, AfterSeq: at.Seq,
			ScanSize: int32(f.window + 1), WindowSize: int32(f.window),
		})
		if err != nil {
			return err
		}
		if win.Scanned == 0 {
			page = HistoryPage{Events: []PunchEvent{}, Next: at}
			return nil
		}
		rows, err := qs.ListPunchHistory(ctx, store.ListPunchHistoryParams{
			TenantID: tenantID, S0: at.S0, FromTime: from, ToTime: to, AfterTime: at.Time, AfterSeq: at.Seq,
			EndTime: win.EndTime, EndSeq: win.EndSeq, Role: string(role), PageSize: int32(limit + 1),
		})
		if err != nil {
			return err
		}
		page.Events = make([]PunchEvent, 0, min(len(rows), limit))
		for _, r := range rows[:min(len(rows), limit)] {
			page.Events = append(page.Events, punchEventOf(store.ListPunchEventsRow(r)))
		}
		if len(rows) > limit {
			e := page.Events[limit-1]
			page.Next, page.HasMore = HistoryPosition{S0: at.S0, Time: e.PunchTime.UTC(), Seq: e.Seq}, true
			return nil
		}
		// The whole window was read: its end is the next position, and the range goes on past it when
		// the window's count found one more.
		page.Next = HistoryPosition{S0: at.S0, Time: win.EndTime.UTC(), Seq: win.EndSeq}
		page.HasMore = win.Scanned > f.window
		return nil
	})
	if errors.Is(err, ErrCursorAhead) {
		return HistoryPage{}, ErrCursorAhead
	}
	if err != nil {
		return HistoryPage{}, fmt.Errorf("tenant: read the punch history: %w", err)
	}
	return page, nil
}
