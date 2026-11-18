package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/tenant"
)

// THE TIMESHEET (M11 API-14; ADR 0046; issue #36):
//
//	GET /v1/timesheets?from=YYYY-MM-DD&to=YYYY-MM-DD[&cursor=tms_…][&limit=…]
//
// A pay period's counted worked time for the key's business's OWN employees, per local day, computed
// by the panel report's engine (internal/domain/ledger: Reader.Timesheet runs events() and
// accumulate(), Reader.Hours' two functions) -- the numbers the Taptime report shows, not a pairing of
// punches (the feed has no manual records -- guide §6.1, §6.3). Read-only: nothing is written.
//
// THE QUERY PARAMETERS are API-7's rule: from, to, cursor and limit exactly, each at most once; any
// other name, another case, a repeated one or a query string that does not parse is 400, the value
// never echoed. from and to are both required: local calendar days in the business's time zone,
// YYYY-MM-DD, both inclusive, to on or after from, at most ledger.TimesheetMaxDays (31) days. limit
// counts EMPLOYEES, 1..1000, 500 by default.
//
// THE ANSWER is {period, complete, truncated, data, nextCursor, hasMore}. data is in employee id
// order; a page holds at most limit employees and may hold fewer while hasMore is true (the read's
// budget, ledger.Timesheet). nextCursor is set exactly when hasMore is (the directory's rule). complete
// is this PAGE's: no employee on it has awaitingMinutes, openEntries or checkInMissing above zero, and
// it is not truncated -- a period is complete when every page of its walk says so.
//
// THE CURSOR is tms_<employee id>_<binding>: the last employee of the page and a digest of the key's
// business, from and to -- a cursor sent by another business or with another range is 400, never a
// silently different page (the history and directory cursors' rule).
//
// MINUTES ARE WHOLE AND FLOORED: each figure is its own exact duration divided by time.Minute in
// integer arithmetic (§6: no float), so the days of a person may sum to less than workedMinutes by
// under a minute each.
//
// NEVER ON THE WIRE (CLAUDE.md §4, ADR 0028 §3, ADR 0046 §4): an IP, a coordinate, a note, a rule's
// reason, a trust score, an e-mail address -- the statements select none and timesheetBody has no
// field for one. Open while the business is suspended (§10): a read. Every statement runs under
// tenant.APIReadTimeout.

// Timesheets is the timesheet's reader (*ledger.Reader), declared at the consumer (§7).
type Timesheets interface {
	Timesheet(ctx context.Context, tenantID uuid.UUID, q ledger.TimesheetQuery) (ledger.TimesheetPage, error)
}

var _ Timesheets = (*ledger.Reader)(nil)

const (
	timesheetsPath        = "/timesheets"
	timesheetCursorPrefix = "tms_"
	// timesheetDate is a query string's local date: the layout and the shape it must have before it is
	// parsed (time.Parse alone takes no other width, but the shape check keeps the rule readable).
	timesheetDate = "2006-01-02"
)

var timesheetParameters = []string{paramFrom, paramTo, fieldCursor, paramLimit}

var timesheetDateShape = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

var (
	problemTimesheetParameter = &Problem{Code: CodeInvalidRequest,
		Message: "This endpoint takes the query parameters from, to, cursor and limit only, each at most once, in a well-formed query string.",
		Details: map[string]any{"allowed": timesheetParameters}}
	problemTimesheetRange = &Problem{Code: CodeInvalidRequest,
		Message: "from and to are both required: two calendar dates (YYYY-MM-DD) in the business's time zone, both inclusive, to on or after from and at most 31 days in all.",
		Details: map[string]any{"parameters": []string{paramFrom, paramTo}, "maxDays": ledger.TimesheetMaxDays}}
	problemTimesheetCursor = &Problem{Code: CodeInvalidRequest,
		Message: "cursor must be the nextCursor of an earlier page of this business's timesheet, sent with the same from and to.",
		Details: map[string]any{"parameter": fieldCursor}}
)

// timesheetRequest is a parsed query string.
type timesheetRequest struct {
	from, to ledger.Date
	limit    int
	after    *uuid.UUID
}

// parseLocalDate reads one YYYY-MM-DD: the shape, then a real calendar day (no 31st of April).
func parseLocalDate(v string) (ledger.Date, bool) {
	if !timesheetDateShape.MatchString(v) {
		return ledger.Date{}, false
	}
	t, err := time.Parse(timesheetDate, v)
	if err != nil || t.Year() < 1 {
		return ledger.Date{}, false
	}
	return ledger.Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}, true
}

// timesheetBinding is the digest a timesheet cursor carries of the walk it was issued for: the key's
// business and the range.
func timesheetBinding(tenantID uuid.UUID, from, to ledger.Date) string {
	sum := sha256.Sum256([]byte("tappa/v1-timesheets\x00" + tenantID.String() + "\x00" + from.String() + "\x00" + to.String()))
	return hex.EncodeToString(sum[:8])
}

func formatTimesheetCursor(after uuid.UUID, bind string) string {
	return timesheetCursorPrefix + after.String() + "_" + bind
}

// parseTimesheetCursor reads tms_<canonical uuid>_<16 lower-case hex>.
func parseTimesheetCursor(v string) (uuid.UUID, string, bool) {
	rest, ok := strings.CutPrefix(v, timesheetCursorPrefix)
	if !ok {
		return uuid.Nil, "", false
	}
	id, bind, ok := strings.Cut(rest, "_")
	if !ok || len(bind) != 16 || strings.ToLower(bind) != bind {
		return uuid.Nil, "", false
	}
	if _, err := hex.DecodeString(bind); err != nil {
		return uuid.Nil, "", false
	}
	u, err := uuid.Parse(id)
	// The canonical text alone: uuid.Parse also takes braces, a urn: prefix, upper case and no hyphens.
	if err != nil || u.String() != id {
		return uuid.Nil, "", false
	}
	return u, bind, true
}

// timesheetQuery reads the query string under the rule above.
func timesheetQuery(r *http.Request, tenantID uuid.UUID) (timesheetRequest, *Problem) {
	var req timesheetRequest
	q, prob := readQuery(r, timesheetParameters, problemTimesheetParameter)
	if prob != nil {
		return req, prob
	}
	f, okF := q[paramFrom]
	t, okT := q[paramTo]
	if !okF || !okT {
		return req, problemTimesheetRange
	}
	from, okF := parseLocalDate(f[0])
	to, okT := parseLocalDate(t[0])
	if !okF || !okT {
		return req, problemTimesheetRange
	}
	if n := ledger.DaysBetween(from, to) + 1; n < 1 || n > ledger.TimesheetMaxDays {
		return req, problemTimesheetRange
	}
	req.from, req.to = from, to
	if req.limit, prob = parseLimit(q); prob != nil {
		return req, prob
	}
	if c, present := q[fieldCursor]; present {
		after, bind, ok := parseTimesheetCursor(c[0])
		if !ok || bind != timesheetBinding(tenantID, from, to) {
			return req, problemTimesheetCursor
		}
		req.after = &after
	}
	return req, nil
}

// The wire shapes.
type (
	timesheetPeriodBody struct {
		From     string `json:"from"`
		To       string `json:"to"`
		TimeZone string `json:"timeZone"`
	}
	timesheetEmployeeBody struct {
		ID           string  `json:"id"`
		ExternalRef  *string `json:"externalRef"`
		EmployeeCode *string `json:"employeeCode"`
		FirstName    *string `json:"firstName"`
		LastName     *string `json:"lastName"`
		FullName     string  `json:"fullName"`
	}
	timesheetDayBody struct {
		Date           string `json:"date"`
		WorkedMinutes  int64  `json:"workedMinutes"`
		Manual         bool   `json:"manual"`
		Open           bool   `json:"open"`
		CheckInMissing bool   `json:"checkInMissing"`
	}
	timesheetBody struct {
		Employee        timesheetEmployeeBody `json:"employee"`
		WorkedMinutes   int64                 `json:"workedMinutes"`
		AwaitingMinutes int64                 `json:"awaitingMinutes"`
		Shifts          int                   `json:"shifts"`
		OpenEntries     int                   `json:"openEntries"`
		CheckInMissing  int                   `json:"checkInMissing"`
		ManualShifts    int                   `json:"manualShifts"`
		Days            []timesheetDayBody    `json:"days"`
	}
	timesheetPage struct {
		Period     timesheetPeriodBody `json:"period"`
		Complete   bool                `json:"complete"`
		Truncated  bool                `json:"truncated"`
		Data       []timesheetBody     `json:"data"`
		NextCursor *string             `json:"nextCursor"`
		HasMore    bool                `json:"hasMore"`
	}
)

// minutes floors a duration to whole minutes in integer arithmetic (§6). A negative duration -- none
// is built: an interval ends after it starts -- would floor toward zero, which is the safe side.
func minutes(d time.Duration) int64 { return int64(d / time.Minute) }

// timesheetOf builds one employee's answer, field by field.
func timesheetOf(p ledger.TimesheetPerson, period ledger.Period, zone *time.Location) timesheetBody {
	h := p.Hours
	out := timesheetBody{
		Employee: timesheetEmployeeBody{ID: employeeIDPrefix + p.Employee.ID.String(), ExternalRef: p.Employee.ExternalRef,
			EmployeeCode: p.Employee.EmployeeCode, FirstName: p.Employee.FirstName, LastName: p.Employee.LastName,
			FullName: p.Employee.FullName},
		WorkedMinutes: minutes(h.Worked), AwaitingMinutes: minutes(h.Awaiting), Shifts: h.Shifts,
		OpenEntries: h.Open, CheckInMissing: h.OutWithoutIn, ManualShifts: h.ManualShifts,
		Days: make([]timesheetDayBody, 0, period.Days()),
	}
	for i, day := range period.DayLabels(zone) {
		d := timesheetDayBody{Date: day.Format(timesheetDate)}
		if i < len(h.Daily) {
			d.WorkedMinutes = minutes(h.Daily[i])
		}
		if i < len(h.Marks) {
			d.Manual, d.Open, d.CheckInMissing = h.Marks[i].ManualShift, h.Marks[i].Open, h.Marks[i].InMissing
		}
		out.Days = append(out.Days, d)
	}
	return out
}

func (s *Surface) listTimesheets(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalOf(r)
	req, prob := timesheetQuery(r, p.TenantID)
	if prob != nil {
		writeProblem(w, prob)
		return
	}
	page, err := s.timesheets.Timesheet(r.Context(), p.TenantID, ledger.TimesheetQuery{From: req.from, To: req.to, After: req.after,
		Limit: req.limit, Bound: tenant.APIReadTimeout})
	switch {
	case errors.Is(err, ledger.ErrTimesheetRange):
		// Unreachable: the range was checked above with the same arithmetic. Answered as the refusal
		// it is rather than as an outage, should the two ever part.
		writeProblem(w, problemTimesheetRange)
		return
	case err != nil:
		s.unavailable(w, r, "reading the timesheet failed", err)
		return
	}
	zone := page.Zone
	if zone == nil {
		zone = time.UTC
	}
	out := timesheetPage{
		Period:   timesheetPeriodBody{From: req.from.String(), To: req.to.String(), TimeZone: zone.String()},
		Complete: !page.Truncated, Truncated: page.Truncated, HasMore: page.HasMore,
		Data: make([]timesheetBody, 0, len(page.People)),
	}
	for _, person := range page.People {
		// complete reads the EXACT durations: half a minute waiting for approval is under one
		// awaitingMinutes but it is not nothing.
		if h := person.Hours; h.Awaiting > 0 || h.Open > 0 || h.OutWithoutIn > 0 {
			out.Complete = false
		}
		out.Data = append(out.Data, timesheetOf(person, page.Period, zone))
	}
	if page.HasMore && page.Next != nil {
		c := formatTimesheetCursor(*page.Next, timesheetBinding(p.TenantID, req.from, req.to))
		out.NextCursor = &c
	} else {
		// nextCursor is set exactly when hasMore is: a page with more and no position to continue
		// from cannot be built (ledger.Timesheet sets Next whenever HasMore), and is not claimed.
		out.HasMore = false
	}
	writeJSON(w, http.StatusOK, out)
}
