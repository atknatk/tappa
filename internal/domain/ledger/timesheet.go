package ledger

// timesheet.go -- the READ behind GET /v1/timesheets (M11 API-14, ADR 0046): a pay period's counted
// worked time per employee and per local day, for an integrator's payroll.
//
// 🔴 IT IS THE REPORT'S ARITHMETIC, NOT A SECOND ONE. The rows are mapped by events() and summed by
// accumulate(), the two functions Reader.Hours runs for the panel, over the same window
// ([From, To + closingTail)) with the same row shape: ListTimesheetShiftEvents selects
// ListWorkedShiftEvents' columns exactly, and each row is CONVERTED to a ListWorkedShiftEventsRow,
// which compiles only while the two generated types are field-for-field the same. So a shift crossing
// midnight is attributed to the day it started, a pending flag is Awaiting, a manual record is an
// ordinary end, an unclosed check-in is Open -- by the code that decides it for the panel.
//
// WHAT IS DIFFERENT IS THE SHAPE OF THE READ: a range of 1..TimesheetMaxDays local days (RangeOf)
// rather than a week, and a PAGE OF EMPLOYEES rather than the whole business. A person's chain is
// per person -- accumulate resets at every new employee -- so reading a page of people gives each of
// them exactly the figures the whole-business read gives them; TestTimesheets_TheWeekIsThePanelReportsWeek
// (internal/handler/api) measures that against Reader.Hours.
//
// 🔴 NOTHING IS WRITTEN (§4.3) and nothing is stored: every figure is recomputed from transactions
// on every request, so a correction (a new row) changes the answer without anything being rewritten.
// 🔴 NO FLOAT (§6): durations stay time.Duration; the API floors them to whole minutes.
// TENANT SCOPE (§4.5): one WithTenant transaction (RLS) and the tenant as an explicit predicate in
// both statements. The tenant is the API key's, never a request's.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// TimesheetMaxDays is the longest range one timesheet covers: a calendar month (ADR 0046).
const TimesheetMaxDays = 31

// ErrTimesheetRange is a range RangeOf refuses: the last day before the first, a zero date, or more
// than TimesheetMaxDays days.
var ErrTimesheetRange = errors.New("ledger: a timesheet covers 1 to 31 whole local days, the last on or after the first")

// ErrTimesheetUnbounded is a TimesheetQuery without a statement bound.
var ErrTimesheetUnbounded = errors.New("ledger: a timesheet read needs a statement bound")

// RangeOf is the period of the local days first..last (both inclusive) in the tenant's zone.
//
// THE BOUNDARIES ARE LOCAL MIDNIGHTS, built the way WeekOf builds a week's -- time.Date for the first
// and AddDate for the end, never a multiple of 24 hours, because a DST day is 23 or 25 hours long.
// For a Monday..Sunday range it is WeekOf's period exactly (TestRangeOf_AWeekIsWeekOf).
func RangeOf(first, last Date, zone *time.Location) (Period, error) {
	if zone == nil {
		zone = time.UTC
	}
	if first.Zero() || last.Zero() {
		return Period{}, ErrTimesheetRange
	}
	days := DaysBetween(first, last) + 1
	if days < 1 || days > TimesheetMaxDays {
		return Period{}, ErrTimesheetRange
	}
	from := time.Date(first.Year, first.Month, first.Day, 0, 0, 0, 0, zone)
	y, m, d := from.Date()
	return Period{
		From:     from,
		To:       from.AddDate(0, 0, days),
		FirstDay: Date{Year: y, Month: m, Day: d},
		days:     days,
	}, nil
}

// DaysBetween is last minus first in calendar days (negative when last is earlier). Both are rebuilt
// as UTC midnights purely as a day counter, dayIndex's method: UTC has no transitions.
func DaysBetween(first, last Date) int {
	a := time.Date(first.Year, first.Month, first.Day, 0, 0, 0, 0, time.UTC)
	b := time.Date(last.Year, last.Month, last.Day, 0, 0, 0, 0, time.UTC)
	return int(b.Sub(a) / (24 * time.Hour))
}

// TimesheetQuery is one page of a timesheet.
type TimesheetQuery struct {
	// From and To are the range's first and last local days, inclusive.
	From, To Date
	// After is the last employee of the previous page (nil: the first page).
	After *uuid.UUID
	// Limit is the most employees the page holds (1..; the API's 1..1000).
	Limit int
	// Bound is every statement's timeout (the API's tenant.APIReadTimeout). It is REQUIRED: a
	// timesheet reads up to ReportEventCap + 1 rows for a caller outside the panel, and a read with
	// no bound is refused rather than run unbounded (ErrTimesheetUnbounded).
	Bound time.Duration
}

// TimesheetEmployee is who a timesheet row is about, on the API's identifiers. It carries no e-mail
// address and no home venue (ListTimesheetEmployees selects neither).
type TimesheetEmployee struct {
	ID                                             uuid.UUID
	ExternalRef, EmployeeCode, FirstName, LastName *string
	FullName                                       string
}

// TimesheetPerson is one employee's period: who, and the report's PersonHours for them.
type TimesheetPerson struct {
	Employee TimesheetEmployee
	Hours    PersonHours
}

// TimesheetPage is one page of a timesheet.
type TimesheetPage struct {
	Period Period
	Zone   *time.Location
	// People are in employee id order -- the cursor's order, not the report's by-name order.
	People []TimesheetPerson
	// HasMore says there are employees after this page; Next is the last one on it (the cursor), set
	// exactly when HasMore is.
	HasMore bool
	Next    *uuid.UUID
	// Truncated says ONE employee's records alone exceeded the read's budget (ReportEventCap): that
	// person's figures on this page are a floor, not a total. A page that merely ran out of budget
	// part-way through the people ends before the person it would have cut instead (HasMore).
	Truncated bool
}

// Timesheet loads one page of a timesheet: at most q.Limit employees after q.After, each with the
// report's figures for the range.
//
// THE PAGE AND THE BUDGET, AS ONE RULE: ListTimesheetShiftEvents is asked for q.Limit + 1 employees
// and ReportEventCap + 1 rows. Rows come grouped by employee, so only the LAST employee of a read can
// be cut. If the read hit the row budget, that employee is dropped from the page (a half chain is a
// wrong total -- report.go's ReportEventCap) and HasMore is set, unless it is the page's only employee:
// then its rows up to the budget are reported as a floor (Truncated) rather than dropped (§4.6) and
// the page moves past it. If the read did not hit the budget, the (q.Limit + 1)th employee only says
// that there is more.
func (r *Reader) Timesheet(ctx context.Context, tenantID uuid.UUID, q TimesheetQuery) (TimesheetPage, error) {
	if q.Limit < 1 {
		return TimesheetPage{}, fmt.Errorf("ledger: timesheet: a page of %d employees", q.Limit)
	}
	if q.Bound <= 0 {
		return TimesheetPage{}, ErrTimesheetUnbounded
	}
	var out TimesheetPage
	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if err := db.BoundStatements(ctx, tx, q.Bound); err != nil {
			return err
		}
		qs := store.New(tx)
		clock, err := qs.GetTenantClock(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("load tenant clock: %w", err)
		}
		zone := r.loadZone(clock.Timezone)
		period, err := RangeOf(q.From, q.To, zone)
		if err != nil {
			return err
		}
		out.Period, out.Zone = period, zone

		got, err := qs.ListTimesheetShiftEvents(ctx, store.ListTimesheetShiftEventsParams{
			TenantID:      tenantID,
			FromAt:        period.From,
			ToAt:          period.To,
			UntilAt:       period.To.Add(closingTail),
			AfterEmployee: q.After,
			PageSize:      int32(q.Limit + 1),
			RowLimit:      readLimit(ReportEventCap),
		})
		if err != nil {
			return fmt.Errorf("list timesheet shift events: %w", err)
		}
		rows := make([]store.ListWorkedShiftEventsRow, len(got))
		for i := range got {
			rows[i] = store.ListWorkedShiftEventsRow(got[i])
		}

		order, rows, truncated, more := timesheetPage(rows, q.Limit)
		out.Truncated, out.HasMore = truncated, more
		if len(order) == 0 {
			return nil
		}
		if more {
			last := order[len(order)-1]
			out.Next = &last
		}

		need := false
		for _, row := range rows {
			need = need || row.SiteLocationID != nil
		}
		sites, err := siteVenuesFor(ctx, tx, need)
		if err != nil {
			return err
		}
		evs, _ := events(rows, clock.Timezone, sites)
		body := accumulate(evs, period, zone, r.now())

		who, err := qs.ListTimesheetEmployees(ctx, store.ListTimesheetEmployeesParams{TenantID: tenantID, EmployeeIds: order})
		if err != nil {
			return fmt.Errorf("list timesheet employees: %w", err)
		}
		names := make(map[uuid.UUID]store.ListTimesheetEmployeesRow, len(who))
		for _, w := range who {
			names[w.ID] = w
		}
		out.People = make([]TimesheetPerson, 0, len(body.People))
		for _, ph := range body.People {
			e := TimesheetEmployee{ID: ph.EmployeeID, FullName: ph.Name}
			if w, ok := names[ph.EmployeeID]; ok {
				e.ExternalRef, e.EmployeeCode, e.FirstName, e.LastName, e.FullName = w.ExternalRef, w.EmployeeCode, w.FirstName, w.LastName, w.FullName
			}
			out.People = append(out.People, TimesheetPerson{Employee: e, Hours: ph})
		}
		sort.Slice(out.People, func(i, j int) bool {
			return out.People[i].Employee.ID.String() < out.People[j].Employee.ID.String()
		})
		return nil
	})
	if err != nil {
		return TimesheetPage{}, fmt.Errorf("ledger: timesheet: %w", err)
	}
	return out, nil
}

// timesheetPage applies Timesheet's page rule to a read: the employees on the page in read order,
// the rows that belong to them, whether one employee's figures are a floor, and whether there is more.
// It is a function of its own so the rule is tested without a database (timesheet_test.go).
//
// The read is cut when it returned ReportEventCap + 1 rows. The extra row names the employee the cut
// fell in: when that is the last employee of the kept rows, their chain may be partial and they go to
// the next page; when it is somebody after them, every kept chain is whole.
func timesheetPage(rows []store.ListWorkedShiftEventsRow, limit int) (order []uuid.UUID, kept []store.ListWorkedShiftEventsRow, truncated, more bool) {
	var extra *uuid.UUID
	if truncatedBy(len(rows)) {
		extra = rows[ReportEventCap].EmployeeID
		rows = rows[:ReportEventCap]
	}
	var people []uuid.UUID
	var ends []int
	for i, row := range rows {
		if row.EmployeeID == nil {
			continue // the page's JOIN admits none; events() would count one rather than drop it
		}
		if len(people) == 0 || people[len(people)-1] != *row.EmployeeID {
			people = append(people, *row.EmployeeID)
			ends = append(ends, i+1)
		}
		ends[len(ends)-1] = i + 1
	}
	if len(people) == 0 {
		return nil, nil, false, extra != nil
	}
	keep := len(people)
	if extra != nil {
		more = true
		if *extra == people[keep-1] {
			if keep == 1 {
				// One person's records alone fill the budget: reported as a floor (§4.6), and the
				// walk moves past them.
				return people, rows, true, true
			}
			keep--
		}
	}
	if keep > limit {
		keep, more = limit, true
	}
	return people[:keep], rows[:ends[keep-1]], false, more
}
