package api

// timesheets_test.go -- GET /v1/timesheets' query rule and wire shape without a database (M11 API-14,
// ADR 0046 §3). The database half -- the figures, the tenant and the partner scope, the panel
// cross-check -- is timesheets_db_test.go.

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/tenant"
)

// TestTimesheetsQuery_RefusesEveryMalformedRequest is the parameter rule, row by row: each request
// either parses to the expected range and page or is refused with exactly the expected problem --
// never a different refusal, and never a silent default for a parameter that was sent.
func TestTimesheetsQuery_RefusesEveryMalformedRequest(t *testing.T) {
	t.Parallel()
	tenant, other := uuid.New(), uuid.New()
	last := uuid.New()
	nov1, nov30 := ledger.Date{Year: 2026, Month: time.November, Day: 1}, ledger.Date{Year: 2026, Month: time.November, Day: 30}
	good := formatTimesheetCursor(last, timesheetBinding(tenant, nov1, nov30))
	otherRange := formatTimesheetCursor(last, timesheetBinding(tenant, nov1, ledger.Date{Year: 2026, Month: time.November, Day: 29}))
	otherTenant := formatTimesheetCursor(last, timesheetBinding(other, nov1, nov30))
	q := url.QueryEscape
	tests := []struct {
		name      string
		query     string
		want      *Problem
		from, to  ledger.Date
		limit     int
		withAfter bool
	}{
		{name: "a month", query: "from=2026-11-01&to=2026-11-30", from: nov1, to: nov30, limit: DefaultLimit},
		{name: "one day", query: "from=2026-11-01&to=2026-11-01", from: nov1, to: nov1, limit: DefaultLimit},
		{name: "31 days across a year end", query: "from=2026-12-15&to=2027-01-14",
			from: ledger.Date{Year: 2026, Month: time.December, Day: 15}, to: ledger.Date{Year: 2027, Month: time.January, Day: 14}, limit: DefaultLimit},
		{name: "a leap day", query: "from=2028-02-29&to=2028-02-29",
			from: ledger.Date{Year: 2028, Month: time.February, Day: 29}, to: ledger.Date{Year: 2028, Month: time.February, Day: 29}, limit: DefaultLimit},
		{name: "a limit and the cursor of the same walk", query: "from=2026-11-01&to=2026-11-30&limit=2&cursor=" + q(good),
			from: nov1, to: nov30, limit: 2, withAfter: true},
		{name: "the parameters in another order", query: "cursor=" + q(good) + "&to=2026-11-30&limit=1000&from=2026-11-01",
			from: nov1, to: nov30, limit: 1000, withAfter: true},

		{name: "no parameter at all", query: "", want: problemTimesheetRange},
		{name: "from alone", query: "from=2026-11-01", want: problemTimesheetRange},
		{name: "to alone", query: "to=2026-11-30", want: problemTimesheetRange},
		{name: "an empty from", query: "from=&to=2026-11-30", want: problemTimesheetRange},
		{name: "32 days", query: "from=2026-11-01&to=2026-12-02", want: problemTimesheetRange},
		{name: "a year", query: "from=2026-01-01&to=2026-12-31", want: problemTimesheetRange},
		{name: "to before from", query: "from=2026-11-02&to=2026-11-01", want: problemTimesheetRange},
		{name: "the 31st of November", query: "from=2026-11-01&to=2026-11-31", want: problemTimesheetRange},
		{name: "a 29th of February outside a leap year", query: "from=2027-02-29&to=2027-02-29", want: problemTimesheetRange},
		{name: "month 13", query: "from=2026-13-01&to=2026-13-02", want: problemTimesheetRange},
		{name: "an instant rather than a date", query: "from=" + q("2026-11-01T00:00:00Z") + "&to=2026-11-30", want: problemTimesheetRange},
		{name: "a date without its zeros", query: "from=2026-11-1&to=2026-11-30", want: problemTimesheetRange},
		{name: "another separator", query: "from=2026%2F11%2F01&to=2026-11-30", want: problemTimesheetRange},
		{name: "a space around the date", query: "from=" + q(" 2026-11-01") + "&to=2026-11-30", want: problemTimesheetRange},
		{name: "a five-digit year", query: "from=12026-11-01&to=12026-11-02", want: problemTimesheetRange},
		{name: "year zero", query: "from=0000-01-01&to=0000-01-02", want: problemTimesheetRange},
		{name: "full-width digits", query: "from=" + q("２０２６-11-01") + "&to=2026-11-30", want: problemTimesheetRange},

		{name: "limit 0", query: "from=2026-11-01&to=2026-11-30&limit=0", want: problemLimit},
		{name: "limit 1001", query: "from=2026-11-01&to=2026-11-30&limit=1001", want: problemLimit},
		{name: "a signed limit", query: "from=2026-11-01&to=2026-11-30&limit=%2B5", want: problemLimit},

		{name: "a cursor of another range", query: "from=2026-11-01&to=2026-11-30&cursor=" + q(otherRange), want: problemTimesheetCursor},
		{name: "a cursor of another business", query: "from=2026-11-01&to=2026-11-30&cursor=" + q(otherTenant), want: problemTimesheetCursor},
		{name: "a directory cursor", query: "from=2026-11-01&to=2026-11-30&cursor=" + q("dir_1_1_"+last.String()+"_0123456789abcdef"), want: problemTimesheetCursor},
		{name: "an employee id as the cursor", query: "from=2026-11-01&to=2026-11-30&cursor=" + q("emp_"+last.String()), want: problemTimesheetCursor},
		{name: "the cursor in upper case", query: "from=2026-11-01&to=2026-11-30&cursor=" + q(strings.ToUpper(good)), want: problemTimesheetCursor},
		{name: "an empty cursor", query: "from=2026-11-01&to=2026-11-30&cursor=", want: problemTimesheetCursor},

		{name: "an unknown parameter", query: "from=2026-11-01&to=2026-11-30&scope=employer", want: problemTimesheetParameter},
		{name: "another case", query: "From=2026-11-01&to=2026-11-30", want: problemTimesheetParameter},
		{name: "from twice", query: "from=2026-11-01&from=2026-11-02&to=2026-11-30", want: problemTimesheetParameter},
		{name: "a query string that does not parse", query: "from=2026-11-01&to=2026-11-30&%zz", want: problemTimesheetParameter},
		{name: "a semicolon between parameters", query: "from=2026-11-01;to=2026-11-30", want: problemTimesheetParameter},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest("GET", "http://"+apiHost+"/v1/timesheets?"+tc.query, nil)
			got, prob := timesheetQuery(r, tenant)
			if prob != tc.want {
				t.Fatalf("problem %v, want %v", prob, tc.want)
			}
			if tc.want != nil {
				return
			}
			if got.from != tc.from || got.to != tc.to || got.limit != tc.limit {
				t.Errorf("parsed %+v, want from %v to %v limit %d", got, tc.from, tc.to, tc.limit)
			}
			if (got.after != nil) != tc.withAfter || (got.after != nil && *got.after != last) {
				t.Errorf("after %v, want the cursor's employee: %v", got.after, tc.withAfter)
			}
		})
	}
}

// TestTimesheetsQuery_TheRangeRuleIsTheEngines: every span the handler admits is one RangeOf builds,
// and every span it refuses RangeOf refuses -- the 31-day bound is one number with two readers.
func TestTimesheetsQuery_TheRangeRuleIsTheEngines(t *testing.T) {
	t.Parallel()
	first := ledger.Date{Year: 2026, Month: time.March, Day: 1}
	start := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	for n := -2; n <= ledger.TimesheetMaxDays+2; n++ {
		end := start.AddDate(0, 0, n)
		last := ledger.Date{Year: end.Year(), Month: end.Month(), Day: end.Day()}
		r := httptest.NewRequest("GET", "http://"+apiHost+"/v1/timesheets?from="+first.String()+"&to="+last.String(), nil)
		_, prob := timesheetQuery(r, uuid.New())
		_, err := ledger.RangeOf(first, last, time.UTC)
		if (prob == nil) != (err == nil) {
			t.Errorf("to = from + %d: the handler says %v, RangeOf says %v", n, prob, err)
		}
		if want := n >= 0 && n < ledger.TimesheetMaxDays; (prob == nil) != want {
			t.Errorf("to = from + %d: admitted %v, want %v", n, prob == nil, want)
		}
	}
}

// TestTimesheets_TheWireShapeIsTheContract: one employee's answer carries exactly the contract's keys
// at every level -- nothing more (no evidence column has a field to fill) -- with the minutes
// floored and one day entry per day of the period.
func TestTimesheets_TheWireShapeIsTheContract(t *testing.T) {
	t.Parallel()
	period, err := ledger.RangeOf(ledger.Date{Year: 2026, Month: time.November, Day: 2}, ledger.Date{Year: 2026, Month: time.November, Day: 4}, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	h := ledger.PersonHours{Worked: 8*time.Hour + 59*time.Second, Awaiting: 59 * time.Second, Shifts: 1, ManualShifts: 1,
		Daily: []time.Duration{8*time.Hour + 59*time.Second, 0, 0}, Marks: []ledger.DayMarks{{ManualShift: true, Manual: true}, {Open: true}, {InMissing: true}}}
	body := timesheetOf(ledger.TimesheetPerson{Employee: ledger.TimesheetEmployee{ID: uuid.New(), FullName: "Panel Made"}, Hours: h}, period, time.UTC)
	if body.WorkedMinutes != 480 || body.AwaitingMinutes != 0 || body.Days[0].WorkedMinutes != 480 {
		t.Errorf("minutes %d/%d/%d, want 480/0/480 (floored)", body.WorkedMinutes, body.AwaitingMinutes, body.Days[0].WorkedMinutes)
	}
	if len(body.Days) != 3 || body.Days[0].Date != "2026-11-02" || body.Days[2].Date != "2026-11-04" {
		t.Errorf("days %+v, want the three days of the period", body.Days)
	}
	if !body.Days[0].Manual || !body.Days[1].Open || !body.Days[2].CheckInMissing || body.Days[1].Manual {
		t.Errorf("day marks %+v", body.Days)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	keys := func(v any) string {
		var out []string
		for k := range v.(map[string]any) {
			out = append(out, k)
		}
		sortStrings(out)
		return strings.Join(out, ",")
	}
	if got, want := keys(m), "awaitingMinutes,checkInMissing,days,employee,manualShifts,openEntries,shifts,workedMinutes"; got != want {
		t.Errorf("the entry's keys %s, want %s", got, want)
	}
	if got, want := keys(m["employee"]), "employeeCode,externalRef,firstName,fullName,id,lastName"; got != want {
		t.Errorf("the employee's keys %s, want %s", got, want)
	}
	if got, want := keys(m["days"].([]any)[0]), "checkInMissing,date,manual,open,workedMinutes"; got != want {
		t.Errorf("a day's keys %s, want %s", got, want)
	}
	if e := m["employee"].(map[string]any); e["firstName"] != nil || e["externalRef"] != nil {
		t.Errorf("a panel-made employee's names and ref must be null, not absent or empty: %v", e)
	}
}

// fixedTimesheets answers one page whatever is asked, and remembers the query it was asked with.
type fixedTimesheets struct {
	page ledger.TimesheetPage
	got  *ledger.TimesheetQuery
}

func (f fixedTimesheets) Timesheet(_ context.Context, _ uuid.UUID, q ledger.TimesheetQuery) (ledger.TimesheetPage, error) {
	if f.got != nil {
		*f.got = q
	}
	return f.page, nil
}

// cleanPerson is one employee's week with nothing open, waiting or missing.
func cleanPerson(t *testing.T, period ledger.Period) ledger.TimesheetPerson {
	t.Helper()
	h := ledger.PersonHours{Worked: 8 * time.Hour, Shifts: 1, Daily: make([]time.Duration, period.Days()), Marks: make([]ledger.DayMarks, period.Days())}
	h.Daily[0] = 8 * time.Hour
	return ledger.TimesheetPerson{Employee: ledger.TimesheetEmployee{ID: uuid.New(), FullName: "Clean Week"}, Hours: h}
}

// getTimesheet sends one authenticated GET through the shipped chain and decodes the page.
func getTimesheet(t *testing.T, fake fixedTimesheets) timesheetPage {
	t.Helper()
	r := newRig(t, known(uuid.New(), uuid.New()), func(c *Config) { c.Timesheets = fake })
	rec := r.do("GET", "/v1/timesheets?from=2026-11-02&to=2026-11-08", "Bearer "+liveKey(t), "", nil)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var p timesheetPage
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestTimesheets_CompleteFallsForEachSignalAlone pins every arm of `complete` on its own (the
// API-14 review's ORTA-2): a page whose one employee is clean but for exactly ONE signal -- time
// waiting for approval (half a minute: under one awaitingMinutes, and still not nothing), an open
// entry, a missing check-in, or the read's truncation -- is not complete, and the clean page is.
// Each arm was measured going red when removed from listTimesheets (the API-14 fix round's report).
func TestTimesheets_CompleteFallsForEachSignalAlone(t *testing.T) {
	t.Parallel()
	period, err := ledger.RangeOf(ledger.Date{Year: 2026, Month: time.November, Day: 2}, ledger.Date{Year: 2026, Month: time.November, Day: 8}, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		change    func(p *ledger.TimesheetPerson, page *ledger.TimesheetPage)
		complete  bool
		truncated bool
	}{
		{"nothing open, waiting or missing", func(*ledger.TimesheetPerson, *ledger.TimesheetPage) {}, true, false},
		{"only half a minute waiting for approval", func(p *ledger.TimesheetPerson, _ *ledger.TimesheetPage) { p.Hours.Awaiting = 30 * time.Second }, false, false},
		{"only an open entry", func(p *ledger.TimesheetPerson, _ *ledger.TimesheetPage) { p.Hours.Open = 1 }, false, false},
		{"only a missing check-in", func(p *ledger.TimesheetPerson, _ *ledger.TimesheetPage) { p.Hours.OutWithoutIn = 1 }, false, false},
		{"only the read's truncation", func(_ *ledger.TimesheetPerson, page *ledger.TimesheetPage) { page.Truncated = true }, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			person := cleanPerson(t, period)
			page := ledger.TimesheetPage{Period: period, Zone: time.UTC}
			tc.change(&person, &page)
			page.People = []ledger.TimesheetPerson{person}
			got := getTimesheet(t, fixedTimesheets{page: page})
			if got.Complete != tc.complete || got.Truncated != tc.truncated {
				t.Errorf("complete %v, truncated %v; want %v, %v", got.Complete, got.Truncated, tc.complete, tc.truncated)
			}
			if len(got.Data) != 1 || got.Data[0].AwaitingMinutes != 0 {
				t.Errorf("the entry %+v: the half minute waiting is under one awaitingMinutes", got.Data)
			}
		})
	}
}

// TestTimesheets_TheDayMarkIsTheShiftsNotTheArrivals pins the handler's day mapping (the review's
// DÜŞÜK-4): days[].manual is DayMarks.ManualShift -- a counted shift with a typed end -- and NOT
// DayMarks.Manual, the panel Roll's typed-ARRIVAL mark. A tapped check-in closed by a typed checkout
// sets the first and not the second; a typed check-in nobody closed sets the second and not the first.
// And the bound the handler hands the reader is the API reads' (tenant.APIReadTimeout).
func TestTimesheets_TheDayMarkIsTheShiftsNotTheArrivals(t *testing.T) {
	t.Parallel()
	period, err := ledger.RangeOf(ledger.Date{Year: 2026, Month: time.November, Day: 2}, ledger.Date{Year: 2026, Month: time.November, Day: 8}, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	person := cleanPerson(t, period)
	person.Hours.ManualShifts = 1
	person.Hours.Marks[0] = ledger.DayMarks{ManualShift: true}        // tapped in, typed out
	person.Hours.Marks[1] = ledger.DayMarks{Manual: true, Open: true} // typed in, never out
	person.Hours.Open = 1
	var q ledger.TimesheetQuery
	got := getTimesheet(t, fixedTimesheets{page: ledger.TimesheetPage{Period: period, Zone: time.UTC, People: []ledger.TimesheetPerson{person}}, got: &q})
	days := got.Data[0].Days
	if !days[0].Manual || days[1].Manual || !days[1].Open {
		t.Errorf("days %+v: manual must be the shift's mark (Monday yes, Tuesday no)", days[:2])
	}
	if q.Bound != tenant.APIReadTimeout || q.Limit != DefaultLimit {
		t.Errorf("the reader was asked with bound %v, limit %d; want %v, %d", q.Bound, q.Limit, tenant.APIReadTimeout, DefaultLimit)
	}
}
