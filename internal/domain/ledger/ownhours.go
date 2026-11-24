package ledger

// ownhours.go -- the staff app's My hours (M16-31): ONE person's week, read with the panel report's
// arithmetic, inside the window the business's owner chose (tenants.staff_hours_weeks, M16-30).
//
// 🔴 ONE ARITHMETIC. ListWorkedShiftEventsForEmployee selects ListWorkedShiftEvents' columns exactly;
// each row is converted to a ListWorkedShiftEventsRow and summed by events() and accumulate() over
// the report's own window [From, To + closingTail) -- so the person's total is the panel's figure
// for them, by the code that computes it for the panel.
//
// 🔴 NOTHING IS WRITTEN (§4.3), nothing is cached. TENANT AND PERSON SCOPE (§4.5): one WithTenant
// transaction, and both ids are explicit predicates in every statement; they come from the
// verified device row, never from the request. No coordinate, address, trust or note is read
// (§4.7; ListOwnRecords' header).

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// OwnPageSize is how many records one My hours page lists.
const OwnPageSize = 200

// ErrOwnHoursOff: the business has not turned My hours on.
var ErrOwnHoursOff = errors.New("ledger: my hours is off for this business")

// ErrOwnWeek: the week asked for is not a Monday, is before the window or is after this week.
var ErrOwnWeek = errors.New("ledger: my hours: that week is not one the window shows")

// ErrOwnCursor: the cursor does not lie in the week asked for.
var ErrOwnCursor = errors.New("ledger: my hours: the cursor is not this week's")

// OwnCursor is the last record of a page.
type OwnCursor struct {
	At time.Time
	ID uuid.UUID
}

// OwnQuery is what the staff app asks for.
type OwnQuery struct {
	EmployeeID uuid.UUID
	// Week is the Monday asked for; zero is this week.
	Week  Date
	After *OwnCursor
	// Cap bounds the totals read (ReportEventCap in production); a read past it is a floor.
	Cap int32
}

// OwnRecord is one of the person's records, as the staff app lists it.
type OwnRecord struct {
	At time.Time
	// Direction is "in", "out" or "" (a refused or ignored tap has none -- 00014).
	Direction string
	Channel   string
	// State: approved, waiting, rejected or repeat.
	State string
	// Claim is the state of a claim anchored on the record: "" (none), pending, approved, rejected.
	Claim string
	Venue string
}

// OwnWeek is one page of the person's week.
type OwnWeek struct {
	Period Period
	Zone   *time.Location
	// Weeks is the business's window (1, 2, 4 or 13).
	Weeks            int
	HasPrev, HasNext bool
	Worked, Awaiting time.Duration
	Open, InMissing  int
	Daily            []time.Duration
	Truncated        bool
	Records          []OwnRecord
	Next             *OwnCursor
	// WaitingRecords and WaitingClaims are the whole window's undecided flags and claims.
	WaitingRecords, WaitingClaims int
}

// Complete is the timesheet's rule: nothing awaiting, nothing open, no check-in missing, no floor.
func (w OwnWeek) Complete() bool {
	return w.Awaiting == 0 && w.Open == 0 && w.InMissing == 0 && !w.Truncated
}

// OwnHours loads one page of the person's week.
func (r *Reader) OwnHours(ctx context.Context, tenantID uuid.UUID, q OwnQuery) (OwnWeek, error) {
	if q.Cap < 1 {
		return OwnWeek{}, fmt.Errorf("ledger: my hours: a read bound of %d", q.Cap)
	}
	emp := q.EmployeeID
	var out OwnWeek
	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		qs := store.New(tx)
		cfg, err := qs.GetTenantStaffHours(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("load the my hours setting: %w", err)
		}
		if cfg.StaffHoursWeeks == nil {
			return ErrOwnHoursOff
		}
		out.Weeks = int(*cfg.StaffHoursWeeks)
		zone := r.loadZone(cfg.Timezone)
		out.Zone = zone
		now := r.now()
		local := now.In(zone)
		this := WeekOf(Date{Year: local.Year(), Month: local.Month(), Day: local.Day()}, zone)
		first := WeekOf(dateOf(this.From.AddDate(0, 0, -7*(out.Weeks-1))), zone)

		week := this
		if !q.Week.Zero() {
			week = WeekOf(q.Week, zone)
			if week.FirstDay != q.Week || week.From.Before(first.From) || week.From.After(this.From) {
				return ErrOwnWeek
			}
		}
		out.Period = week
		out.HasPrev = week.From.After(first.From)
		out.HasNext = week.From.Before(this.From)
		if q.After != nil && (q.After.At.Before(week.From) || !q.After.At.Before(week.To)) {
			return ErrOwnCursor
		}

		got, err := qs.ListWorkedShiftEventsForEmployee(ctx, store.ListWorkedShiftEventsForEmployeeParams{
			TenantID:   tenantID,
			EmployeeID: &emp,
			FromAt:     week.From,
			UntilAt:    week.To.Add(closingTail),
			RowLimit:   readLimit(q.Cap),
		})
		if err != nil {
			return fmt.Errorf("list the person's shift events: %w", err)
		}
		out.Truncated = len(got) > int(q.Cap)
		if out.Truncated {
			got = got[:q.Cap]
		}
		rows := make([]store.ListWorkedShiftEventsRow, len(got))
		need := false
		for i := range got {
			rows[i] = store.ListWorkedShiftEventsRow(got[i])
			need = need || rows[i].SiteLocationID != nil
		}

		page := store.ListOwnRecordsParams{
			TenantID:   tenantID,
			EmployeeID: &emp,
			FromAt:     week.From,
			ToAt:       week.To,
			RowLimit:   OwnPageSize + 1,
		}
		if q.After != nil {
			at, id := q.After.At, q.After.ID
			page.AfterAt, page.AfterID = &at, &id
		}
		recs, err := qs.ListOwnRecords(ctx, page)
		if err != nil {
			return fmt.Errorf("list the person's records: %w", err)
		}
		if len(recs) > OwnPageSize {
			recs = recs[:OwnPageSize]
			last := recs[len(recs)-1]
			out.Next = &OwnCursor{At: last.OccurredAt, ID: last.ID}
		}
		for _, rec := range recs {
			need = need || rec.SiteLocationID != nil
		}

		sites, err := siteVenuesFor(ctx, tx, need)
		if err != nil {
			return err
		}
		evs, _ := events(rows, cfg.Timezone, sites)
		body := accumulate(evs, week, zone, now)
		out.Daily = make([]time.Duration, week.Days())
		for _, p := range body.People {
			if p.EmployeeID != q.EmployeeID {
				continue
			}
			out.Worked, out.Awaiting, out.Open, out.InMissing = p.Worked, p.Awaiting, p.Open, p.OutWithoutIn
			copy(out.Daily, p.Daily)
		}

		out.Records = make([]OwnRecord, 0, len(recs))
		for _, rec := range recs {
			venue := deref(rec.LocationName)
			if rec.SiteLocationID != nil {
				venue = sites.name(*rec.SiteLocationID)
			}
			out.Records = append(out.Records, OwnRecord{
				At:        rec.OccurredAt,
				Direction: deref(rec.Type),
				Channel:   rec.Channel,
				State:     ownState(rec.Verdict, deref(rec.ReviewOutcome)),
				Claim:     ownClaim(rec.ClaimID, rec.ClaimOutcome),
				Venue:     venue,
			})
		}

		waiting, err := qs.CountOwnWaiting(ctx, store.CountOwnWaitingParams{
			TenantID:   tenantID,
			EmployeeID: &emp,
			FromAt:     first.From,
			ToAt:       now,
		})
		if err != nil {
			return fmt.Errorf("count the person's waiting records: %w", err)
		}
		out.WaitingRecords, out.WaitingClaims = int(waiting.Records), int(waiting.Claims)
		return nil
	})
	if errors.Is(err, ErrOwnHoursOff) || errors.Is(err, ErrOwnWeek) || errors.Is(err, ErrOwnCursor) {
		return OwnWeek{}, err
	}
	if err != nil {
		return OwnWeek{}, fmt.Errorf("ledger: my hours: %w", err)
	}
	return out, nil
}

func dateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date{Year: y, Month: m, Day: d}
}

// ownState is a record's state in the person's words: the engine's verdict and, for a flag, the
// manager's decision.
func ownState(verdict, review string) string {
	switch verdict {
	case "ok":
		return "approved"
	case "flag":
		switch review {
		case "approved":
			return "approved"
		case "rejected":
			return "rejected"
		}
		return "waiting"
	case "ignored":
		return "repeat"
	}
	return "rejected"
}

func ownClaim(id *uuid.UUID, outcome *string) string {
	if id == nil {
		return ""
	}
	if outcome == nil {
		return "pending"
	}
	return *outcome
}
