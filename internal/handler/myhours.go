package handler

// myhours.go -- the browser's My hours (M16-32, ADR 0045 §2.1): GET /me/hours?week=&after= on the
// main host, for the person whose phone carries the tap's session cookie. It is the app's
// POST /app/v1/hours (apphours.go) as a page: the same ledger.OwnHours read, the same window, week
// and cursor rules, the same states -- in plain words (the card's decision (b)).
//
// WHO: the tappa_session cookie, resolved by the tap surface's own chain (Mount: the address bucket,
// Identify, the session bucket). No request value names a person or a business.
//
//	no live session (none, or revoked)   a plain page, 200, nothing read: "Open this on the phone
//	                                     you clock in with."
//	the business's setting is off        the router's own 404 -- the same bytes as any unrouted path
//	                                     under /me, so the page does not say the feature exists
//	a week or a cursor the read refuses  400, a problem page
//
// Cache-Control: no-store on every answer (render). No logo, no accent: the page is Taptime's
// (CLAUDE.md §9's numbered brand slots do not grow). Nothing is written (§4.3). NOTHING ABOUT A TIME OR
// A VENUE IS LOGGED: opaque ids only (§7).

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// MyHoursPath is the browser's My hours page.
const MyHoursPath = "/me/hours"

// myHoursReader is the consumer's view of ledger.Reader (CLAUDE.md §7).
type myHoursReader interface {
	OwnHours(ctx context.Context, tenantID uuid.UUID, q ledger.OwnQuery) (ledger.OwnWeek, error)
}

// ServeMyHours turns the browser's My hours on. cmd/tappa wires it with the reader the app's My hours
// uses.
func (t *Tap) ServeMyHours(h myHoursReader) error {
	if isNil(h) {
		return errors.New("handler: nil my hours reader")
	}
	t.hours = h
	return nil
}

// myHoursBadPage answers a week or a cursor the read refuses (the app's 400).
var myHoursBadPage = pages.ProblemView{
	Title:   "That week isn't one you can see",
	Message: "My hours shows the weeks your employer chose to share.",
	Hint:    "Open My hours again to see this week.",
}

// myHoursStates are the record states in the person's words (decision (b): no stamps).
var myHoursStates = map[string]string{
	"approved": "Counted",
	"waiting":  "Waiting for your manager",
	"rejected": "Not counted",
	"repeat":   "Repeat",
}

// myHoursClaims are a claim's state on the record it is anchored on.
var myHoursClaims = map[string]string{
	"pending":  "Your missing-time request is waiting for your manager",
	"approved": "Your missing-time request was approved",
	"rejected": "Your missing-time request was not approved",
}

// MyHours serves GET /me/hours.
func (t *Tap) MyHours(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context() // see checkin.go: one level of paren depth for redline R7.
	if t.hours == nil {
		http.NotFound(w, r)
		return
	}
	id := httpx.IdentityOf(r)
	switch id.State {
	case httpx.SessionLive:
	case httpx.SessionAbsent, httpx.SessionRevoked:
		t.render(w, r, http.StatusOK, pages.MyHoursSignedOut(), false)
		return
	default:
		t.log.ErrorContext(ctx, "my hours: the session could not be resolved", "err", id.Err)
		t.renderRetryableProblem(w, r, http.StatusInternalServerError, tapProblemServer)
		return
	}
	q := ledger.OwnQuery{EmployeeID: id.EmployeeID(), Cap: ledger.ReportEventCap}
	form := r.URL.Query()
	if s := form.Get("week"); s != "" {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.renderProblem(w, r, http.StatusBadRequest, myHoursBadPage)
			return
		}
		q.Week = ledger.Date{Year: d.Year(), Month: d.Month(), Day: d.Day()}
	}
	after, ok := decodeOwnCursor(form.Get("after"))
	if !ok {
		t.renderProblem(w, r, http.StatusBadRequest, myHoursBadPage)
		return
	}
	q.After = after
	week, err := t.hours.OwnHours(ctx, id.TenantID(), q)
	switch {
	case errors.Is(err, ledger.ErrOwnHoursOff):
		// The router's own answer (writes no-store first: it is about this person's business).
		w.Header().Set("Cache-Control", "no-store")
		http.NotFound(w, r)
		return
	case errors.Is(err, ledger.ErrOwnWeek), errors.Is(err, ledger.ErrOwnCursor):
		t.renderProblem(w, r, http.StatusBadRequest, myHoursBadPage)
		return
	case err != nil:
		t.log.ErrorContext(ctx, "my hours: reading the week failed", "employee_id", id.EmployeeID(), "err", err)
		t.renderRetryableProblem(w, r, http.StatusInternalServerError, tapProblemServer)
		return
	}
	t.render(w, r, http.StatusOK, pages.MyHours(t.myHoursView(ctx, week)), false)
}

// myHoursHref is the page's own address for a week and, optionally, a cursor.
func myHoursHref(week string, cursor *string) string {
	v := url.Values{"week": {week}}
	if cursor != nil {
		v.Set("after", *cursor)
	}
	return MyHoursPath + "?" + v.Encode()
}

// myHoursView formats one page of the week in the business's zone.
func (t *Tap) myHoursView(ctx context.Context, w ledger.OwnWeek) pages.MyHoursView {
	loc := w.Zone
	if loc == nil {
		loc = time.UTC
	}
	monday := w.Period.From.In(loc)
	weekKey := w.Period.FirstDay.String()
	v := pages.MyHoursView{
		Week:  "Week of " + monday.Format("2 Jan 2006"),
		Total: hoursMinutes(w.Worked),
		Days:  make([]pages.MyHoursDay, 0, len(w.Daily)),
	}
	if w.Awaiting > 0 {
		v.Awaiting = hoursMinutes(w.Awaiting)
	}
	if w.Open > 0 {
		v.Notes = append(v.Notes, plural(w.Open, "check-in has", "check-ins have")+" no check-out yet, so it is not counted.")
	}
	if w.InMissing > 0 {
		v.Notes = append(v.Notes, plural(w.InMissing, "check-out has", "check-outs have")+" no check-in, so it is not counted.")
	}
	if w.Truncated {
		v.Notes = append(v.Notes, "This week has more records than we can add up here; the total is incomplete.")
	}
	if n := w.WaitingRecords + w.WaitingClaims; n > 0 {
		v.Waiting = plural(n, "item is", "items are") + " waiting for your manager."
	}
	if w.HasPrev {
		v.PrevHref = myHoursHref(monday.AddDate(0, 0, -7).Format("2006-01-02"), nil)
	}
	if w.HasNext {
		v.NextHref = myHoursHref(monday.AddDate(0, 0, 7).Format("2006-01-02"), nil)
	}
	if next := encodeOwnCursor(w.Next); next != nil {
		v.MoreHref = myHoursHref(weekKey, next)
	}
	byDate := map[string]int{}
	for i, d := range w.Daily {
		day := monday.AddDate(0, 0, i)
		byDate[day.Format("2006-01-02")] = i
		v.Days = append(v.Days, pages.MyHoursDay{Label: day.Format("Mon 2 Jan"), Total: hoursMinutes(d)})
	}
	for _, rec := range w.Records {
		at := rec.At.In(loc)
		i, ok := byDate[at.Format("2006-01-02")]
		if !ok {
			// Not reachable (the read lies inside the week); a record outside it is dropped from the
			// page, never from the books, and said so.
			t.log.ErrorContext(ctx, "my hours: a record fell outside its week's days")
			continue
		}
		state, ok := myHoursStates[rec.State]
		if !ok {
			state = "Not counted"
		}
		o := pages.MyHoursRecord{At: at.Format("15:04"), Venue: rec.Venue, State: state, Claim: myHoursClaims[rec.Claim]}
		switch rec.Direction {
		case "in":
			o.Direction = "In"
		case "out":
			o.Direction = "Out"
		}
		v.Days[i].Records = append(v.Days[i].Records, o)
	}
	return v
}
