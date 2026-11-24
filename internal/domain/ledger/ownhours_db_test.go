package ledger

// ownhours_db_test.go -- M16-31's My hours read against real Postgres (CLAUDE.md §8), in a tenant of
// its own (cpNewWorld): the person's total is the panel report's figure for them, the window and
// the setting are enforced, another person's rows never appear, the record states are the plain
// words, and the read bound is a floor.

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// setWeeks sets the business's My hours window as the owner (nil = off).
func (w *cpWorld) setWeeks(n *int) {
	w.t.Helper()
	if _, err := w.owner.Exec(w.ctx, `UPDATE tenants SET staff_hours_weeks = $2 WHERE id = $1`, w.tenant, n); err != nil {
		w.t.Fatal(err)
	}
}

// row writes one record with a chosen verdict (and direction, "" for none) as the owner.
func (w *cpWorld) row(person uuid.UUID, at time.Time, verdict, dir string) uuid.UUID {
	w.t.Helper()
	id := uuid.New()
	var d, day any
	if dir != "" {
		d, day = dir, w.s(at) // a refused or ignored tap has no direction and no business day (00014, 00064)
	}
	tx, err := w.owner.Begin(w.ctx)
	if err != nil {
		w.t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(w.ctx) }()
	if _, err := tx.Exec(w.ctx, `SELECT set_config('app.tenant_id', $1, true)`, w.tenant.String()); err != nil {
		w.t.Fatal(err)
	}
	if _, err := tx.Exec(w.ctx, `INSERT INTO transactions (id, tenant_id, employee_id, location_id, occurred_at, created_at,
	                                                      verdict, channel, type, business_day_start)
	                             VALUES ($1, $2, $3, $4, $5, $5, $6, 'nfc', $7, $8)`,
		id, w.tenant, person, w.venue, at, verdict, d, day); err != nil {
		w.t.Fatalf("write %s %s: %v", verdict, dir, err)
	}
	if err := tx.Commit(w.ctx); err != nil {
		w.t.Fatal(err)
	}
	return id
}

func weeksPtr(n int) *int { return &n }

func (w *cpWorld) ownWeek(person uuid.UUID, week Date) (OwnWeek, error) {
	return w.r.OwnHours(w.ctx, w.tenant, OwnQuery{EmployeeID: person, Week: week, Cap: ReportEventCap})
}

// TestOwnHours_TheTotalIsThePanelReportsFigure [G31-a] [G31-c] [G31-i]: two closed shifts, a
// refused and an ignored tap of the person, and a second person's shift -- the person's total is
// Reader.Hours' PersonHours.Worked for them, floored to minutes; the other person's rows are not
// listed; refusals are listed in plain words and add nothing.
func TestOwnHours_TheTotalIsThePanelReportsFigure(t *testing.T) {
	w := cpNewWorld(t)
	w.setWeeks(weeksPtr(13))
	me, other := w.employee("Me"), w.employee("Other")
	w.row(me, w.day(0, 9, 0), "ok", "in")
	w.row(me, w.day(0, 17, 31), "ok", "out")
	w.row(me, w.day(0, 17, 31).Add(20*time.Second), "ignored", "")
	w.row(me, w.day(2, 18, 0), "ok", "in")
	w.row(me, w.day(3, 2, 15), "ok", "out")
	w.row(me, w.day(4, 8, 0), "reject", "")
	w.row(other, w.day(1, 9, 0), "ok", "in")
	w.row(other, w.day(1, 12, 0), "ok", "out")

	got, err := w.ownWeek(me, w.week.FirstDay)
	if err != nil {
		t.Fatal(err)
	}
	panel, err := w.r.Hours(w.ctx, w.tenant, ReportFilter{Day: w.week.FirstDay})
	if err != nil {
		t.Fatal(err)
	}
	var want time.Duration
	for _, p := range panel.People {
		if p.EmployeeID == me {
			want = p.Worked
		}
	}
	if want == 0 || got.Worked != want {
		t.Fatalf("worked %v, the panel's %v", got.Worked, want)
	}
	if got.Worked.Truncate(time.Minute) != 8*time.Hour+31*time.Minute+8*time.Hour+15*time.Minute {
		t.Fatalf("worked %v", got.Worked)
	}
	if len(got.Records) != 6 {
		t.Fatalf("records %d, want the person's 6 (not the other's)", len(got.Records))
	}
	states := map[string]int{}
	for _, r := range got.Records {
		states[r.State]++
	}
	if states["approved"] != 4 || states["repeat"] != 1 || states["rejected"] != 1 {
		t.Fatalf("states %v", states)
	}
	if !got.Complete() || got.Daily[0] != 8*time.Hour+31*time.Minute || got.Daily[2] != 8*time.Hour+15*time.Minute {
		t.Fatalf("complete %v daily %v", got.Complete(), got.Daily)
	}
}

// TestOwnHours_TheSettingAndTheWindowAreEnforced [G31-d] [G31-e]: off is ErrOwnHoursOff; a
// one-week window refuses the week the rows are in (two weeks ago) and has no previous week; a
// day that is not a Monday is refused.
func TestOwnHours_TheSettingAndTheWindowAreEnforced(t *testing.T) {
	w := cpNewWorld(t)
	me := w.employee("Me")
	w.row(me, w.day(0, 9, 0), "ok", "in")
	if _, err := w.ownWeek(me, Date{}); !errors.Is(err, ErrOwnHoursOff) {
		t.Fatalf("off: %v", err)
	}
	w.setWeeks(weeksPtr(1))
	if _, err := w.ownWeek(me, w.week.FirstDay); !errors.Is(err, ErrOwnWeek) {
		t.Fatalf("two weeks ago in a one-week window: %v", err)
	}
	this, err := w.ownWeek(me, Date{})
	if err != nil {
		t.Fatal(err)
	}
	if this.HasPrev || this.HasNext || len(this.Records) != 0 {
		t.Fatalf("this week: prev %v next %v records %d", this.HasPrev, this.HasNext, len(this.Records))
	}
	w.setWeeks(weeksPtr(4))
	tuesday := Date{Year: w.week.FirstDay.Year, Month: w.week.FirstDay.Month, Day: w.week.FirstDay.Day + 1}
	if _, err := w.ownWeek(me, dateOf(time.Date(tuesday.Year, tuesday.Month, tuesday.Day, 12, 0, 0, 0, w.zone))); !errors.Is(err, ErrOwnWeek) {
		t.Fatalf("a Tuesday: %v", err)
	}
	past, err := w.ownWeek(me, w.week.FirstDay)
	if err != nil {
		t.Fatal(err)
	}
	if !past.HasPrev || !past.HasNext || len(past.Records) != 1 {
		t.Fatalf("four-week window: prev %v next %v records %d", past.HasPrev, past.HasNext, len(past.Records))
	}
}

// TestOwnHours_AnOpenEntryAndAWaitingFlag [G31-i] [G31-m]: an unclosed check-in makes the week
// incomplete; an undecided flag is "waiting" in the list and counted in the window's waiting line.
func TestOwnHours_AnOpenEntryAndAWaitingFlag(t *testing.T) {
	w := cpNewWorld(t)
	w.setWeeks(weeksPtr(13))
	me := w.employee("Me")
	w.row(me, w.day(0, 9, 0), "flag", "in")
	w.row(me, w.day(0, 17, 0), "ok", "out")
	w.row(me, w.day(1, 9, 0), "ok", "in")
	got, err := w.ownWeek(me, w.week.FirstDay)
	if err != nil {
		t.Fatal(err)
	}
	if got.Complete() || got.Open != 1 || got.Awaiting != 8*time.Hour || got.Worked != 0 {
		t.Fatalf("complete %v open %d awaiting %v worked %v", got.Complete(), got.Open, got.Awaiting, got.Worked)
	}
	if got.Records[0].State != "waiting" || got.WaitingRecords != 1 || got.WaitingClaims != 0 {
		t.Fatalf("state %q waiting %d/%d", got.Records[0].State, got.WaitingRecords, got.WaitingClaims)
	}
}

// TestOwnHours_TheReadBoundIsAFloor [G31-l]: cap 3 over four directed rows is truncated and not
// complete; cap 4 is neither.
func TestOwnHours_TheReadBoundIsAFloor(t *testing.T) {
	w := cpNewWorld(t)
	w.setWeeks(weeksPtr(13))
	me := w.employee("Me")
	w.row(me, w.day(0, 9, 0), "ok", "in")
	w.row(me, w.day(0, 12, 0), "ok", "out")
	w.row(me, w.day(1, 9, 0), "ok", "in")
	w.row(me, w.day(1, 12, 0), "ok", "out")
	for _, tc := range []struct {
		cap       int32
		truncated bool
	}{{3, true}, {4, false}} {
		got, err := w.r.OwnHours(w.ctx, w.tenant, OwnQuery{EmployeeID: me, Week: w.week.FirstDay, Cap: tc.cap})
		if err != nil {
			t.Fatal(err)
		}
		if got.Truncated != tc.truncated || got.Complete() == tc.truncated {
			t.Fatalf("cap %d: truncated %v complete %v", tc.cap, got.Truncated, got.Complete())
		}
	}
}

// TestOwnHours_PagesKeepTheTotals [G31-j]: 201 records in a week are two pages with the same
// totals, the second after the first's cursor; a cursor from another week is refused.
func TestOwnHours_PagesKeepTheTotals(t *testing.T) {
	w := cpNewWorld(t)
	w.setWeeks(weeksPtr(13))
	me := w.employee("Me")
	w.row(me, w.day(0, 9, 0), "ok", "in")
	w.row(me, w.day(0, 17, 0), "ok", "out")
	for i := 0; i < OwnPageSize-1; i++ {
		w.row(me, w.day(1, 8, 0).Add(time.Duration(i)*time.Minute), "ignored", "")
	}
	first, err := w.ownWeek(me, w.week.FirstDay)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Records) != OwnPageSize || first.Next == nil {
		t.Fatalf("first page %d next %v", len(first.Records), first.Next)
	}
	second, err := w.r.OwnHours(w.ctx, w.tenant, OwnQuery{EmployeeID: me, Week: w.week.FirstDay, After: first.Next, Cap: ReportEventCap})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Records) != 1 || second.Next != nil || second.Worked != first.Worked || second.Worked != 8*time.Hour {
		t.Fatalf("second page %d next %v worked %v/%v", len(second.Records), second.Next, second.Worked, first.Worked)
	}
	stray := &OwnCursor{At: w.week.From.AddDate(0, 0, 7), ID: uuid.New()}
	if _, err := w.r.OwnHours(w.ctx, w.tenant, OwnQuery{EmployeeID: me, Week: w.week.FirstDay, After: stray, Cap: ReportEventCap}); !errors.Is(err, ErrOwnCursor) {
		t.Fatalf("another week's cursor: %v", err)
	}
}
