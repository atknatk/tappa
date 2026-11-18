package api

// timesheets_db_test.go -- GET /v1/timesheets (M11 API-14; ADR 0046; issue #36's acceptance) over the
// REAL chain, the real ledger reader and Postgres: a manual record counts, an open entry is open and
// not worked, a night shift is attributed by the report's rule, a flag waiting for approval is not
// worked, another business's employees never appear (RLS), a holding partner's employee is the
// employer's, a walk pages every employee once under a bound cursor -- and the figures are the panel
// report's for the same week (TestTimesheets_TheWeekIsThePanelReportsWeek).
//
// THE WEEK is Monday 2 to Sunday 8 November 2026 in Europe/Malta (CET, UTC+1), the punch world's
// businesses' zone. Every record is written as tappa_app in its employer's context with a declared
// occurred_at; the evidence columns of a tap row carry the world's canary, so a leak into an answer
// is visible.

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/ledger"
)

// tsWeek is the period the cases read: Monday 2 November 2026 to Sunday 8 November, inclusive.
const tsWeek = "from=2026-11-02&to=2026-11-08"

// tsAt is a Malta wall clock of November 2026 as a UTC instant.
func tsAt(t *testing.T, day, hh, mm int) time.Time {
	t.Helper()
	zone, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	return time.Date(2026, time.November, day, hh, mm, 0, 0, zone).UTC()
}

// tsPerson gives b one more employee, bound to ref, with a first and a last name.
func (w *punchWorld) tsPerson(b punchBiz, ref, first, last string) punchBiz {
	w.r.t.Helper()
	id := uuid.New()
	w.exec(`INSERT INTO employees (id, tenant_id, external_ref, employee_code, first_name, last_name, full_name, status, invited_at)
	        VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', now())`, id, b.id, ref, "C-"+ref, first, last, first+" "+last)
	b.emp = id
	return b
}

// tsRecord is one record of the week: a direction, a verdict, a channel and the declared instant.
type tsRecord struct {
	kind, verdict, channel string
	at                     time.Time
	// dayStart is the record's frozen business day (business_day_start); nil as on a row before M15.
	dayStart *time.Time
	// review, when set, is the employer's owner's decision on a flag.
	review string
}

func tsIn(at time.Time) tsRecord { return tsRecord{kind: "in", verdict: "ok", channel: "nfc", at: at} }
func tsOut(at time.Time) tsRecord {
	return tsRecord{kind: "out", verdict: "ok", channel: "nfc", at: at}
}

// tsWrite writes b.emp's records, each in its own transaction in b's context, in the order given.
func (w *punchWorld) tsWrite(b punchBiz, recs ...tsRecord) {
	w.r.t.Helper()
	for _, rec := range recs {
		var id uuid.UUID
		err := w.r.d.WithTenant(w.ctx, b.id, func(ctx context.Context, tx pgx.Tx) error {
			var tag, note, ip, enteredBy any
			var lat, lng any
			if rec.channel == "manual" {
				enteredBy = b.admin
			} else {
				tag, note, ip, lat, lng = b.tag, w.canary+"-note", "198.51.100.213", punchGPSLat, punchGPSLng
			}
			return tx.QueryRow(ctx, `
				INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, verdict, channel, type,
				                          entered_by, note, source_ip, gps_lat, gps_lng, trust, business_day_start)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::inet, $12, $13, 77, $14)
				RETURNING id`,
				b.id, b.emp, b.venue, tag, rec.at, rec.verdict, rec.channel, rec.kind, enteredBy, note, ip, lat, lng, rec.dayStart).Scan(&id)
		})
		if err != nil {
			w.r.t.Fatalf("write %s/%s/%s at %v: %v", rec.channel, rec.verdict, rec.kind, rec.at, err)
		}
		if rec.review != "" {
			w.review(b, id, rec.review)
		}
	}
}

// tsGet is one GET of /v1/timesheets with the raw query.
func (w *punchWorld) tsGet(raw, query string) *httptest.ResponseRecorder {
	return w.r.do(http.MethodGet, "/v1/timesheets?"+query, raw, "")
}

// tsPageOf decodes a page, refusing any key the contract does not name, and checks that no evidence
// of the world reached it.
func (w *punchWorld) tsPageOf(rec *httptest.ResponseRecorder) timesheetPage {
	w.r.t.Helper()
	if rec.Code != http.StatusOK {
		w.r.t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	for _, leak := range []string{w.canary, strings.ToLower(w.canary), "198.51.100.213", "87.654321", "165.432198", "@"} {
		if strings.Contains(rec.Body.String(), leak) {
			w.r.t.Fatalf("the answer carries %q -- evidence or an address reached the timesheet: %s", leak, rec.Body.String())
		}
	}
	var p timesheetPage
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		w.r.t.Fatalf("the answer %q is not a timesheet page: %v", rec.Body.String(), err)
	}
	return p
}

// tsOf finds one employee's entry; nil when the page does not list them.
func tsOf(p timesheetPage, emp uuid.UUID) *timesheetBody {
	for i := range p.Data {
		if p.Data[i].Employee.ID == employeeIDPrefix+emp.String() {
			return &p.Data[i]
		}
	}
	return nil
}

// tsDay is an entry's day by date.
func tsDay(t *testing.T, e *timesheetBody, date string) timesheetDayBody {
	t.Helper()
	for _, d := range e.Days {
		if d.Date == date {
			return d
		}
	}
	t.Fatalf("no day %s among %d", date, len(e.Days))
	return timesheetDayBody{}
}

// tsCrew is KF's week: one employee per rule, each written through tsWrite.
type tsCrew struct {
	alex, manny, olive, nina, fay, cara punchBiz
}

func (w *punchWorld) tsCrewWeek(t *testing.T) tsCrew {
	t.Helper()
	b := w.kf
	c := tsCrew{
		alex: w.tsPerson(b, "T-ALEX", "Alex", "Sample"), manny: w.tsPerson(b, "T-MANNY", "Manny", "Typed"),
		olive: w.tsPerson(b, "T-OLIVE", "Olive", "Open"), nina: w.tsPerson(b, "T-NINA", "Nina", "Night"),
		fay: w.tsPerson(b, "T-FAY", "Fay", "Flagged"), cara: w.tsPerson(b, "T-CARA", "Cara", "Missing"),
	}
	// Alex: two day shifts, the second 30 seconds over eight hours (floored to 480).
	w.tsWrite(c.alex, tsIn(tsAt(t, 2, 9, 0)), tsOut(tsAt(t, 2, 17, 0)),
		tsIn(tsAt(t, 3, 9, 0)), tsOut(tsAt(t, 3, 17, 0).Add(30*time.Second)))
	// Manny: a tapped Monday; Tuesday tapped in and its checkout typed by a manager (a manual SHIFT
	// with no typed arrival -- the day mark the panel Roll's Manual does not set); and Wednesday typed
	// by a manager at both ends.
	w.tsWrite(c.manny, tsIn(tsAt(t, 2, 8, 0)), tsOut(tsAt(t, 2, 12, 0)),
		tsIn(tsAt(t, 3, 9, 0)), tsRecord{kind: "out", verdict: "ok", channel: "manual", at: tsAt(t, 3, 13, 0)},
		tsRecord{kind: "in", verdict: "ok", channel: "manual", at: tsAt(t, 4, 9, 0)},
		tsRecord{kind: "out", verdict: "ok", channel: "manual", at: tsAt(t, 4, 17, 0)})
	// Olive: a Monday shift, then a Thursday check-in nobody closed.
	w.tsWrite(c.olive, tsIn(tsAt(t, 2, 10, 0)), tsOut(tsAt(t, 2, 14, 0)), tsIn(tsAt(t, 5, 9, 0)))
	// Nina: the night before the week (the previous period's), Tuesday night, and Sunday night --
	// the week's last day, whose checkout is Monday 9 November.
	w.tsWrite(c.nina, tsIn(tsAt(t, 1, 22, 0)), tsOut(tsAt(t, 2, 6, 0)),
		tsIn(tsAt(t, 3, 22, 0)), tsOut(tsAt(t, 4, 6, 0)),
		tsIn(tsAt(t, 8, 20, 0)), tsOut(tsAt(t, 9, 4, 0)))
	// Fay: an approved flag (counted), a refused one (neither counted nor waiting), and a flag nobody
	// has decided (waiting).
	w.tsWrite(c.fay,
		tsRecord{kind: "in", verdict: "flag", channel: "nfc", at: tsAt(t, 2, 9, 0), review: "approved"}, tsOut(tsAt(t, 2, 17, 0)),
		tsRecord{kind: "in", verdict: "flag", channel: "nfc", at: tsAt(t, 3, 9, 0), review: "rejected"}, tsOut(tsAt(t, 3, 13, 0)),
		tsRecord{kind: "in", verdict: "flag", channel: "nfc", at: tsAt(t, 6, 9, 0)}, tsOut(tsAt(t, 6, 17, 0)))
	// Cara: a Tuesday shift, then a Saturday checkout of a business day (05:00) begun inside the week
	// with no check-in before it -- ADR 0032 §A.6's (c): a missing check-in.
	sat := tsAt(t, 7, 5, 0)
	w.tsWrite(c.cara, tsIn(tsAt(t, 3, 11, 0)), tsOut(tsAt(t, 3, 15, 0)),
		tsRecord{kind: "out", verdict: "ok", channel: "nfc", at: tsAt(t, 7, 12, 0), dayStart: &sat})
	return c
}

// TestTimesheets_AManualRecordIsCounted (issue #36 acceptance): a shift a manager typed at both ends
// is in workedMinutes and in manualShifts, and its day is marked manual; so is a tapped check-in whose
// checkout a manager typed (the day the panel Roll's typed-arrival mark leaves unmarked); a tapped
// shift is not.
func TestTimesheets_AManualRecordIsCounted(t *testing.T) {
	w := newPunchWorld(t, unmetered)
	c := w.tsCrewWeek(t)
	m := tsOf(w.tsPageOf(w.tsGet(w.kf.raw, tsWeek)), c.manny.emp)
	if m == nil {
		t.Fatal("Manny is not listed")
	}
	if m.WorkedMinutes != 4*60+4*60+8*60 || m.Shifts != 3 || m.ManualShifts != 2 {
		t.Errorf("worked %d in %d shifts, %d manual; want 960 in 3, 2 manual", m.WorkedMinutes, m.Shifts, m.ManualShifts)
	}
	if d := tsDay(t, m, "2026-11-03"); d.WorkedMinutes != 240 || !d.Manual {
		t.Errorf("Tuesday (tapped in, typed out) %+v, want 240 and manual", d)
	}
	if d := tsDay(t, m, "2026-11-04"); d.WorkedMinutes != 480 || !d.Manual {
		t.Errorf("Wednesday %+v, want 480 and manual", d)
	}
	if d := tsDay(t, m, "2026-11-02"); d.WorkedMinutes != 240 || d.Manual {
		t.Errorf("Monday %+v, want 240 and not manual", d)
	}
}

// TestTimesheets_AnOpenEntryIsOpenNotWorked (issue #36 acceptance): a check-in with no checkout is
// openEntries and its day open; it adds nothing to workedMinutes, and the page is not complete.
func TestTimesheets_AnOpenEntryIsOpenNotWorked(t *testing.T) {
	w := newPunchWorld(t, unmetered)
	c := w.tsCrewWeek(t)
	p := w.tsPageOf(w.tsGet(w.kf.raw, tsWeek))
	o := tsOf(p, c.olive.emp)
	if o == nil {
		t.Fatal("Olive is not listed")
	}
	if o.WorkedMinutes != 240 || o.OpenEntries != 1 || o.Shifts != 1 {
		t.Errorf("worked %d, open %d, shifts %d; want 240, 1, 1", o.WorkedMinutes, o.OpenEntries, o.Shifts)
	}
	if d := tsDay(t, o, "2026-11-05"); d.WorkedMinutes != 0 || !d.Open {
		t.Errorf("Thursday %+v, want 0 minutes and open", d)
	}
	if p.Complete {
		t.Error("a page with an open entry says complete")
	}
	// The control: Alex's week alone (a page of one, in id order) is complete only when nothing on it
	// is open -- read it through a walk of one-employee pages.
	for cursor, pages := "", 0; ; pages++ {
		if pages > 2*len(p.Data) {
			t.Fatalf("the walk of one-employee pages did not end after %d pages", pages)
		}
		q := tsWeek + "&limit=1"
		if cursor != "" {
			q += "&cursor=" + url.QueryEscape(cursor)
		}
		page := w.tsPageOf(w.tsGet(w.kf.raw, q))
		if len(page.Data) == 1 && page.Data[0].Employee.ID == employeeIDPrefix+c.alex.emp.String() && !page.Complete {
			t.Errorf("Alex's page (nothing open, nothing waiting, nothing missing) says incomplete: %+v", page.Data[0])
		}
		if !page.HasMore {
			break
		}
		cursor = *page.NextCursor
	}
}

// TestTimesheets_ANightShiftIsAttributedByTheReportsRule (issue #36 acceptance): a shift crossing
// midnight counts on the day it started; one started on the week's last day counts in full in this
// week (the read's tail finds its Monday checkout); the night before the week is the previous
// period's; and the next week does not count Sunday's shift a second time.
func TestTimesheets_ANightShiftIsAttributedByTheReportsRule(t *testing.T) {
	w := newPunchWorld(t, unmetered)
	c := w.tsCrewWeek(t)
	n := tsOf(w.tsPageOf(w.tsGet(w.kf.raw, tsWeek)), c.nina.emp)
	if n == nil {
		t.Fatal("Nina is not listed")
	}
	if n.WorkedMinutes != 960 || n.Shifts != 2 || n.OpenEntries != 0 || n.CheckInMissing != 0 {
		t.Errorf("worked %d in %d shifts, open %d, missing %d; want 960 in 2, 0, 0", n.WorkedMinutes, n.Shifts, n.OpenEntries, n.CheckInMissing)
	}
	for date, want := range map[string]int64{"2026-11-02": 0, "2026-11-03": 480, "2026-11-04": 0, "2026-11-08": 480} {
		if d := tsDay(t, n, date); d.WorkedMinutes != want {
			t.Errorf("%s: %d minutes, want %d", date, d.WorkedMinutes, want)
		}
	}
	// The previous week holds the night of the 1st, and the next week does not hold the 8th's.
	prev := tsOf(w.tsPageOf(w.tsGet(w.kf.raw, "from=2026-10-26&to=2026-11-01")), c.nina.emp)
	if prev == nil || prev.WorkedMinutes != 480 || tsDay(t, prev, "2026-11-01").WorkedMinutes != 480 {
		t.Errorf("the previous week's answer for Nina %+v, want the night of the 1st (480 on the 1st)", prev)
	}
	if next := tsOf(w.tsPageOf(w.tsGet(w.kf.raw, "from=2026-11-09&to=2026-11-15")), c.nina.emp); next != nil && next.WorkedMinutes != 0 {
		t.Errorf("the next week counts %d minutes of Nina's Sunday night again", next.WorkedMinutes)
	}
}

// TestTimesheets_AnAwaitingFlagIsNotWorked (issue #36 acceptance): a flag nobody has decided is
// awaitingMinutes and never workedMinutes; an approved one is worked; a refused one is neither.
func TestTimesheets_AnAwaitingFlagIsNotWorked(t *testing.T) {
	w := newPunchWorld(t, unmetered)
	c := w.tsCrewWeek(t)
	p := w.tsPageOf(w.tsGet(w.kf.raw, tsWeek))
	f := tsOf(p, c.fay.emp)
	if f == nil {
		t.Fatal("Fay is not listed")
	}
	if f.WorkedMinutes != 480 || f.AwaitingMinutes != 480 || f.Shifts != 1 {
		t.Errorf("worked %d, awaiting %d, shifts %d; want 480 (the approved flag), 480 (the undecided one), 1", f.WorkedMinutes, f.AwaitingMinutes, f.Shifts)
	}
	if d := tsDay(t, f, "2026-11-06"); d.WorkedMinutes != 0 {
		t.Errorf("Friday (waiting) has %d worked minutes", d.WorkedMinutes)
	}
	if d := tsDay(t, f, "2026-11-03"); d.WorkedMinutes != 0 {
		t.Errorf("Tuesday (refused) has %d worked minutes", d.WorkedMinutes)
	}
	if p.Complete {
		t.Error("a page with time awaiting approval says complete")
	}
	// The missing check-in is the same kind of signal.
	if cara := tsOf(p, c.cara.emp); cara == nil || cara.CheckInMissing != 1 || !tsDay(t, cara, "2026-11-07").CheckInMissing || cara.WorkedMinutes != 240 {
		t.Errorf("Cara %+v, want one missing check-in on Saturday and her 240 minutes", cara)
	}
}

// TestTimesheets_AnotherTenantsEmployeesNeverAppear (issue #36 acceptance; CLAUDE.md §4.5): CC, a
// business of another holding, has its own employee and week; KF's timesheet lists KF's employees
// only, CC's lists CC's only. And the isolation is RLS's, not only the statement's filter: in KF's
// context a read with NO tenant predicate sees none of CC's records or employees (§6: an isolation
// test must not carry the filter, or its zero proves nothing).
func TestTimesheets_AnotherTenantsEmployeesNeverAppear(t *testing.T) {
	w := newPunchWorld(t, unmetered)
	c := w.tsCrewWeek(t)
	cc := w.tsPerson(w.c, "T-CC", "Cleo", "Elsewhere")
	w.tsWrite(cc, tsIn(tsAt(t, 3, 9, 0)), tsOut(tsAt(t, 3, 17, 0)))

	kf := w.tsPageOf(w.tsGet(w.kf.raw, tsWeek))
	if len(kf.Data) != 6 {
		t.Errorf("KF lists %d employees, want its crew of 6", len(kf.Data))
	}
	if tsOf(kf, cc.emp) != nil {
		t.Fatal("KF's timesheet lists CC's employee")
	}
	other := w.tsPageOf(w.tsGet(w.c.raw, tsWeek))
	if len(other.Data) != 1 || tsOf(other, cc.emp) == nil || tsOf(other, c.alex.emp) != nil {
		t.Errorf("CC's timesheet %+v, want CC's employee alone", other.Data)
	}

	var seen, people int
	if err := w.r.d.WithTenant(w.ctx, w.kf.id, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM transactions WHERE employee_id = $1`, cc.emp).Scan(&seen); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM employees WHERE id = $1`, cc.emp).Scan(&people)
	}); err != nil {
		t.Fatal(err)
	}
	var owned int
	if err := w.r.owner.QueryRow(w.ctx, `SELECT count(*) FROM transactions WHERE employee_id = $1`, cc.emp).Scan(&owned); err != nil {
		t.Fatal(err)
	}
	if owned != 2 {
		t.Fatalf("anti-vacuity: the owner sees %d of CC's records, want 2", owned)
	}
	if seen != 0 || people != 0 {
		t.Errorf("in KF's context, unfiltered: %d of CC's records and %d of its employees are visible -- RLS did not hold", seen, people)
	}
}

// TestTimesheets_APartnersEmployeeIsTheEmployersNotTheVenues (issue #36 acceptance; guide §6.5,
// ADR 0027 §9): KM's employee starts a shift at KF's plaque and ends it at KM's. KF's timesheet does
// not list them -- the time is their employer's -- and KM's counts the whole shift.
func TestTimesheets_APartnersEmployeeIsTheEmployersNotTheVenues(t *testing.T) {
	w := newPunchWorld(t, unmetered)
	w.tsCrewWeek(t)
	in, out := tsAt(t, 4, 9, 0), tsAt(t, 4, 15, 0)
	w.write(punchRow{employer: w.km, site: &w.kf, verdict: "ok", channel: "nfc", kind: "in", at: &in})
	w.write(punchRow{employer: w.km, verdict: "ok", channel: "nfc", kind: "out", at: &out})

	if tsOf(w.tsPageOf(w.tsGet(w.kf.raw, tsWeek)), w.km.emp) != nil {
		t.Error("KF's timesheet lists KM's employee, who only touched KF's plaque")
	}
	km := tsOf(w.tsPageOf(w.tsGet(w.km.raw, tsWeek)), w.km.emp)
	if km == nil || km.WorkedMinutes != 360 || km.Shifts != 1 || tsDay(t, km, "2026-11-04").WorkedMinutes != 360 {
		t.Errorf("KM's answer for its own employee %+v, want the 360-minute shift begun at KF", km)
	}
}

// TestTimesheets_TheWalkPagesEveryEmployeeOnceAndTheCursorIsBound: pages of two walk the crew in id
// order, each employee once, with nextCursor exactly when hasMore -- the same entries as one page --
// and a cursor taken to another range or to another business is 400, never a different page.
func TestTimesheets_TheWalkPagesEveryEmployeeOnceAndTheCursorIsBound(t *testing.T) {
	w := newPunchWorld(t, unmetered)
	w.tsCrewWeek(t)
	whole := w.tsPageOf(w.tsGet(w.kf.raw, tsWeek))
	if whole.HasMore || whole.NextCursor != nil || len(whole.Data) != 6 {
		t.Fatalf("one page: %d entries, hasMore %v, nextCursor %v", len(whole.Data), whole.HasMore, whole.NextCursor)
	}
	var walked []timesheetBody
	var firstCursor string
	cursor := ""
	for pages := 1; ; pages++ {
		q := tsWeek + "&limit=2"
		if cursor != "" {
			q += "&cursor=" + url.QueryEscape(cursor)
		}
		p := w.tsPageOf(w.tsGet(w.kf.raw, q))
		if (p.NextCursor != nil) != p.HasMore {
			t.Fatalf("page %d: nextCursor %v with hasMore %v", pages, p.NextCursor, p.HasMore)
		}
		walked = append(walked, p.Data...)
		if !p.HasMore {
			break
		}
		if pages > 10 {
			t.Fatal("the walk does not end")
		}
		cursor = *p.NextCursor
		if firstCursor == "" {
			firstCursor = cursor
		}
	}
	a, _ := json.Marshal(walked)
	b, _ := json.Marshal(whole.Data)
	if !bytes.Equal(a, b) {
		t.Errorf("the walk's entries are not the one page's:\nwalk %s\npage %s", a, b)
	}
	for i := 1; i < len(walked); i++ {
		if walked[i-1].Employee.ID >= walked[i].Employee.ID {
			t.Errorf("entry %d (%s) is not after entry %d (%s)", i, walked[i].Employee.ID, i-1, walked[i-1].Employee.ID)
		}
	}
	for _, c := range []struct{ name, raw, query string }{
		{"another range", w.kf.raw, "from=2026-11-02&to=2026-11-09&cursor=" + url.QueryEscape(firstCursor)},
		{"another business", w.km.raw, tsWeek + "&cursor=" + url.QueryEscape(firstCursor)},
	} {
		rec := w.tsGet(c.raw, c.query)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", c.name, rec.Code, rec.Body.String())
			continue
		}
		if p := problemOf(t, rec); p.Code != CodeInvalidRequest || p.Details["parameter"] != fieldCursor {
			t.Errorf("%s: %+v, want invalid_request on cursor", c.name, p)
		}
	}
}

// TestTimesheets_TheWeekIsThePanelReportsWeek (issue #36 acceptance): for the same week, every
// figure the API answers is the panel report's (ledger.Reader.Hours -- the Reports section's read):
// the same people, and per person the worked and waiting minutes, the shifts, the open entries, the
// missing check-ins, the manual shifts and every day's minutes and marks. The comparison is against
// the report's exact durations, floored here, so a drift in either direction is red.
func TestTimesheets_TheWeekIsThePanelReportsWeek(t *testing.T) {
	w := newPunchWorld(t, unmetered)
	w.tsCrewWeek(t)
	reader, err := ledger.NewReader(w.r.d, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	report, err := reader.Hours(w.ctx, w.kf.id, ledger.ReportFilter{Day: ledger.Date{Year: 2026, Month: time.November, Day: 5}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Period.FirstDay.String() != "2026-11-02" || report.Truncated {
		t.Fatalf("the report's week starts %s (truncated %v), want 2026-11-02", report.Period.FirstDay, report.Truncated)
	}
	page := w.tsPageOf(w.tsGet(w.kf.raw, tsWeek))
	if len(page.Data) != len(report.People) || len(report.People) != 6 {
		t.Fatalf("the API lists %d people, the report %d (want 6)", len(page.Data), len(report.People))
	}
	floor := func(d time.Duration) int64 { return int64(d / time.Minute) }
	compared := 0
	for _, ph := range report.People {
		e := tsOf(page, ph.EmployeeID)
		if e == nil {
			t.Errorf("%s is in the report and not in the API's answer", ph.Name)
			continue
		}
		if e.Employee.FullName != ph.Name {
			t.Errorf("%s: the API names them %q", ph.Name, e.Employee.FullName)
		}
		if e.WorkedMinutes != floor(ph.Worked) || e.AwaitingMinutes != floor(ph.Awaiting) || e.Shifts != ph.Shifts ||
			e.OpenEntries != ph.Open || e.CheckInMissing != ph.OutWithoutIn || e.ManualShifts != ph.ManualShifts {
			t.Errorf("%s: the API %+v, the report worked %v awaiting %v shifts %d open %d missing %d manual %d", ph.Name, *e,
				ph.Worked, ph.Awaiting, ph.Shifts, ph.Open, ph.OutWithoutIn, ph.ManualShifts)
		}
		if len(e.Days) != len(ph.Daily) || len(ph.Daily) != ledger.ReportDays {
			t.Errorf("%s: %d days in the API, %d in the report", ph.Name, len(e.Days), len(ph.Daily))
			continue
		}
		for i, d := range e.Days {
			mk := ph.Marks[i]
			if d.WorkedMinutes != floor(ph.Daily[i]) || d.Manual != mk.ManualShift || d.Open != mk.Open || d.CheckInMissing != mk.InMissing {
				t.Errorf("%s, %s: the API %+v, the report %v %+v", ph.Name, d.Date, d, ph.Daily[i], mk)
			}
			compared++
		}
	}
	// Anti-vacuity: the week carries every shape the cases name, so the equality is not of zeros.
	var worked, awaiting int64
	var open, missing, manual int
	for _, e := range page.Data {
		worked, awaiting, open, missing, manual = worked+e.WorkedMinutes, awaiting+e.AwaitingMinutes, open+e.OpenEntries, missing+e.CheckInMissing, manual+e.ManualShifts
	}
	if compared != 42 || worked == 0 || awaiting == 0 || open == 0 || missing == 0 || manual == 0 {
		t.Errorf("anti-vacuity: %d days compared, worked %d, awaiting %d, open %d, missing %d, manual %d", compared, worked, awaiting, open, missing, manual)
	}
	if worked > floor(report.Totals.Worked) {
		t.Errorf("the API's minutes add up to %d, more than the report's total %v", worked, report.Totals.Worked)
	}
}
