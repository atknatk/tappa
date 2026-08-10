package ledger

// report_test.go -- the WORKED-HOURS ARITHMETIC, one case per decision (§8).
//
// EVERY CASE FIXES ITS OWN CLOCK. accumulate takes `now` as a parameter for the same
// reason internal/domain/tap takes Input.Now: a test that read the wall clock would
// pass or fail depending on the hour it ran, and the one split that depends on `now`
// -- "on shift" versus "forgot to tap out" -- is exactly the one a flaky test would
// hide.
//
// THE ZONE IS Europe/Malta THROUGHOUT, because that is where the day boundary and the
// DST transitions this arithmetic has to survive actually are. Every expected value
// below is derived from the local wall clock and the known offsets (winter CET =
// UTC+1, summer CEST = UTC+2) rather than read back out of the code.

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/store"
)

func malta(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatalf("Europe/Malta: %v", err)
	}
	return loc
}

// at builds a UTC instant from a Malta wall clock, which is how every case in this
// file states its times -- a report is read in local time and stored in UTC (§6).
func at(t *testing.T, zone *time.Location, y int, m time.Month, d, hh, mm int) time.Time {
	t.Helper()
	return time.Date(y, m, d, hh, mm, 0, 0, zone).UTC()
}

// dayShift is KF St Julians': 10:00-22:00, same day.
func dayShift() *tap.Shift {
	return &tap.Shift{Start: 10 * time.Hour, End: 22 * time.Hour, Timezone: "Europe/Malta"}
}

// nightShift is the Rusty Bar's: 18:00-02:00, crossing midnight. It is the shape §5
// names by name, so it appears in this file by name too.
func nightShift() *tap.Shift {
	return &tap.Shift{Start: 18 * time.Hour, End: 2 * time.Hour, Overnight: true, Timezone: "Europe/Malta"}
}

// people fixes two employee ids so a case can talk about "the same person" and "a
// different person" without the ids moving between runs.
var (
	maria   = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	antoine = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	venueA  = uuid.MustParse("aaaaaaaa-0000-4000-8000-000000000001")
	venueB  = uuid.MustParse("bbbbbbbb-0000-4000-8000-000000000002")
)

// evOpt tunes one event away from the ordinary case (an `ok` tap at venue A on the
// day shift).
type evOpt func(*reportEvent)

func verdict(v, review string) evOpt {
	return func(e *reportEvent) { e.Verdict, e.Review = v, review }
}
func manual() evOpt            { return func(e *reportEvent) { e.Manual = true } }
func shift(s *tap.Shift) evOpt { return func(e *reportEvent) { e.Shift = s } }
func atVenue(id uuid.UUID, name string) evOpt {
	return func(e *reportEvent) { e.LocationID, e.LocationName = id, name }
}

func ev(who uuid.UUID, name, dir string, when time.Time, opts ...evOpt) reportEvent {
	e := reportEvent{
		EmployeeID:   who,
		Name:         name,
		At:           when,
		Direction:    dir,
		Verdict:      "ok",
		LocationID:   venueA,
		LocationName: "KF St Julians",
		Shift:        dayShift(),
	}
	for _, o := range opts {
		o(&e)
	}
	return e
}

// week is the Monday-to-Sunday period the cases below report on: 3 to 9 August 2026,
// Malta local. 3 August 2026 is a Monday (verified independently:
// `date -j -f %Y-%m-%d 2026-08-03 +%A` prints Monday).
func week(t *testing.T, zone *time.Location) Period {
	t.Helper()
	return WeekOf(Date{Year: 2026, Month: time.August, Day: 5}, zone)
}

// find returns one person's row, failing the test when the report has none.
func find(t *testing.T, rep Report, id uuid.UUID) PersonHours {
	t.Helper()
	for _, p := range rep.People {
		if p.EmployeeID == id {
			return p
		}
	}
	t.Fatalf("no row for %s in a report holding %d people", id, len(rep.People))
	return PersonHours{}
}

// TestWeekOf_StartsOnMondayInTheTenantsOwnZone pins the two decisions WeekOf makes:
// which weekday a week starts on, and which clock decides where a day begins.
func TestWeekOf_StartsOnMondayInTheTenantsOwnZone(t *testing.T) {
	t.Parallel()
	zone := malta(t)

	cases := []struct {
		name string
		day  Date
	}{
		{name: "the Monday itself", day: Date{2026, time.August, 3}},
		{name: "midweek", day: Date{2026, time.August, 5}},
		// 🔴 SUNDAY IS THE SEVENTH DAY, NOT THE FIRST. Go's time.Weekday counts from
		// Sunday, so the naive shift puts 9 August in the FOLLOWING week and a night
		// worker's Sunday shift lands in a report their manager is not looking at.
		{name: "the Sunday at the end", day: Date{2026, time.August, 9}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p := WeekOf(c.day, zone)
			if p.FirstDay != (Date{2026, time.August, 3}) {
				t.Fatalf("FirstDay = %s, want 2026-08-03 (the Monday)", p.FirstDay)
			}
			// August is CEST (UTC+2), so local midnight is 22:00 UTC the day before.
			wantFrom := time.Date(2026, time.August, 2, 22, 0, 0, 0, time.UTC)
			if !p.From.Equal(wantFrom) {
				t.Fatalf("From = %s, want %s -- the boundary must be a LOCAL midnight, "+
					"not a UTC one", p.From.UTC(), wantFrom)
			}
			if !p.To.Equal(wantFrom.AddDate(0, 0, 7)) {
				t.Fatalf("To = %s, want seven local days after From", p.To.UTC())
			}
		})
	}
}

// TestWeekOf_ADSTWeekIsNotSevenTimesTwentyFourHours is the reason AddDate is used
// rather than arithmetic on hours.
//
// Malta puts its clocks back on 25 October 2026 (03:00 -> 02:00), so the week of
// 19-25 October is 169 hours long. A `From.Add(7*24*time.Hour)` week would end an
// hour early and drop the last hour of Sunday night -- which at the Rusty Bar is
// somebody's checkout.
func TestWeekOf_ADSTWeekIsNotSevenTimesTwentyFourHours(t *testing.T) {
	t.Parallel()
	zone := malta(t)
	p := WeekOf(Date{2026, time.October, 21}, zone)
	if got := p.To.Sub(p.From); got != 169*time.Hour {
		t.Fatalf("the fall-back week is %v long, want 169h -- a fixed 7x24h window "+
			"would end an hour before local midnight", got)
	}
	spring := WeekOf(Date{2026, time.March, 25}, zone)
	if got := spring.To.Sub(spring.From); got != 167*time.Hour {
		t.Fatalf("the spring-forward week is %v long, want 167h", got)
	}
}

// TestAccumulate_PairsAndAttributes is the decision table: every row is one rule the
// §5/Q18 spec fixes, and the expected values are computed from the wall clocks in the
// case rather than from the implementation.
func TestAccumulate_PairsAndAttributes(t *testing.T) {
	t.Parallel()
	zone := malta(t)
	p := week(t, zone)
	// A clock well past the week, so "still on shift" is never the reason an entry is
	// open. The one case that needs a live clock sets its own.
	after := at(t, zone, 2026, time.August, 12, 9, 0)

	cases := []struct {
		name   string
		events []reportEvent
		// what the report must say about Maria
		worked   time.Duration
		awaiting time.Duration
		refused  time.Duration
		shifts   int
		open     int
		// and about the report as a whole
		totalOpen      int
		startedEarlier int
		tooLong        int
	}{
		{
			name: "an ordinary day shift",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", at(t, zone, 2026, time.August, 3, 10, 0)),
				ev(maria, "Maria Borg", "out", at(t, zone, 2026, time.August, 3, 18, 30)),
			},
			worked: 8*time.Hour + 30*time.Minute, shifts: 1,
		},
		{
			// 🔴 §5's NAMED CASE. The 02:00 exit belongs to the 18:00 entry, so this is
			// ONE interval of eight hours and not two half-days. A calendar-day filter
			// would refuse to pair them at all.
			name: "the Rusty Bar overnight shift is one interval",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", at(t, zone, 2026, time.August, 4, 18, 0), shift(nightShift())),
				ev(maria, "Maria Borg", "out", at(t, zone, 2026, time.August, 5, 2, 0), shift(nightShift())),
			},
			worked: 8 * time.Hour, shifts: 1,
		},
		{
			// 🔴 THE PRE-ADR-0008 SHAPE. Two check-ins in a row: the first was never
			// closed, so its hours are NOT guessed and it goes to the open list. Only
			// the second pairs with the checkout.
			name: "a check-in on top of an open one leaves the first open",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", at(t, zone, 2026, time.August, 3, 9, 0)),
				ev(maria, "Maria Borg", "in", at(t, zone, 2026, time.August, 3, 12, 0)),
				ev(maria, "Maria Borg", "out", at(t, zone, 2026, time.August, 3, 17, 0)),
			},
			worked: 5 * time.Hour, shifts: 1, open: 1, totalOpen: 1,
		},
		{
			// The seam, not an anomaly: the shift began before the period, so its hours
			// belong to the previous report and the checkout is merely counted.
			name: "a checkout with no check-in inside the period",
			events: []reportEvent{
				ev(maria, "Maria Borg", "out", at(t, zone, 2026, time.August, 3, 2, 0)),
				ev(maria, "Maria Borg", "in", at(t, zone, 2026, time.August, 3, 10, 0)),
				ev(maria, "Maria Borg", "out", at(t, zone, 2026, time.August, 3, 18, 0)),
			},
			worked: 8 * time.Hour, shifts: 1, startedEarlier: 1,
		},
		{
			// Q18: the system produces NO checkout. No hours are estimated.
			name: "a check-in nothing closes",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", at(t, zone, 2026, time.August, 6, 10, 0)),
			},
			open: 1, totalOpen: 1,
		},
		{
			// 🔴 LONGER THAN THE ENGINE'S OWN FORGOTTEN-CHECKOUT THRESHOLD, so it is not
			// a work interval: paying it would charge payroll for the days between.
			name: "a pair further apart than the stale-open threshold",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", at(t, zone, 2026, time.August, 3, 10, 0)),
				ev(maria, "Maria Borg", "out", at(t, zone, 2026, time.August, 5, 10, 0)),
			},
			open: 1, totalOpen: 1, tooLong: 1,
		},
		{
			name: "a flagged pair nobody has decided",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", at(t, zone, 2026, time.August, 3, 10, 0), verdict("flag", "")),
				ev(maria, "Maria Borg", "out", at(t, zone, 2026, time.August, 3, 14, 0), verdict("flag", "")),
			},
			awaiting: 4 * time.Hour,
		},
		{
			name: "a flagged pair a manager approved",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", at(t, zone, 2026, time.August, 3, 10, 0), verdict("flag", "approved")),
				ev(maria, "Maria Borg", "out", at(t, zone, 2026, time.August, 3, 14, 0), verdict("flag", "approved")),
			},
			worked: 4 * time.Hour, shifts: 1,
		},
		{
			name: "a flagged pair a manager rejected",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", at(t, zone, 2026, time.August, 3, 10, 0), verdict("flag", "rejected")),
				ev(maria, "Maria Borg", "out", at(t, zone, 2026, time.August, 3, 14, 0), verdict("flag", "rejected")),
			},
			refused: 4 * time.Hour,
		},
		{
			// 🔴 AN INTERVAL IS ONLY PAYABLE IF BOTH ENDS ARE. A clean check-in closed by
			// a tap still waiting for a manager is not half payable.
			name: "one clean end and one undecided end",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", at(t, zone, 2026, time.August, 3, 10, 0)),
				ev(maria, "Maria Borg", "out", at(t, zone, 2026, time.August, 3, 14, 0), verdict("flag", "")),
			},
			awaiting: 4 * time.Hour,
		},
		{
			// A refusal outranks an undecided flag: the manager has spoken about one end.
			name: "an undecided end and a refused end",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", at(t, zone, 2026, time.August, 3, 10, 0), verdict("flag", "")),
				ev(maria, "Maria Borg", "out", at(t, zone, 2026, time.August, 3, 14, 0), verdict("flag", "rejected")),
			},
			refused: 4 * time.Hour,
		},
		{
			// 🔴 THE CHAIN IS PER PERSON. Antoine's checkout must not close Maria's
			// check-in, which is what a single open slot for the whole tenant would do.
			name: "two people interleaved keep their own chains",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", at(t, zone, 2026, time.August, 3, 10, 0)),
				ev(maria, "Maria Borg", "out", at(t, zone, 2026, time.August, 3, 16, 0)),
				ev(antoine, "Antoine Vella", "in", at(t, zone, 2026, time.August, 3, 11, 0)),
				ev(antoine, "Antoine Vella", "out", at(t, zone, 2026, time.August, 3, 15, 0)),
			},
			worked: 6 * time.Hour, shifts: 1,
		},
		{
			// A check-in before the period closed inside it: the interval belongs to the
			// previous report, so nothing here counts it -- and it is not "open" either.
			name: "an interval that opened before the period",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", at(t, zone, 2026, time.August, 2, 20, 0)),
				ev(maria, "Maria Borg", "out", at(t, zone, 2026, time.August, 3, 2, 0)),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			rep := accumulate(c.events, p, zone, after)

			if c.worked == 0 && c.awaiting == 0 && c.refused == 0 && c.open == 0 {
				if len(rep.People) > 0 && rep.People[0].Worked != 0 {
					t.Fatalf("worked = %v, want nothing counted", rep.People[0].Worked)
				}
			} else {
				m := find(t, rep, maria)
				if m.Worked != c.worked {
					t.Errorf("worked = %v, want %v", m.Worked, c.worked)
				}
				if m.Awaiting != c.awaiting {
					t.Errorf("awaiting = %v, want %v", m.Awaiting, c.awaiting)
				}
				if m.Refused != c.refused {
					t.Errorf("refused = %v, want %v", m.Refused, c.refused)
				}
				if m.Shifts != c.shifts {
					t.Errorf("shifts = %d, want %d", m.Shifts, c.shifts)
				}
				if m.Open != c.open {
					t.Errorf("open entries = %d, want %d", m.Open, c.open)
				}
			}
			if rep.Totals.Open != c.totalOpen || len(rep.Open) != c.totalOpen {
				t.Errorf("open list = %d / tally = %d, want %d", len(rep.Open), rep.Totals.Open, c.totalOpen)
			}
			if rep.StartedEarlier != c.startedEarlier {
				t.Errorf("startedEarlier = %d, want %d", rep.StartedEarlier, c.startedEarlier)
			}
			if rep.Totals.TooLong != c.tooLong {
				t.Errorf("tooLong = %d, want %d", rep.Totals.TooLong, c.tooLong)
			}
			// 🔴 THE INVARIANT EVERY CASE MUST HOLD: an open entry contributes no hours
			// anywhere. Q18 is the rule and this is the only place a violation would be
			// silent -- an estimated end would simply make the total bigger.
			if rep.Totals.Worked+rep.Totals.Awaiting+rep.Totals.Refused != sumOf(rep) {
				t.Errorf("the tenant totals and the per-person rows disagree")
			}
		})
	}
}

// sumOf re-adds the per-person figures, so the tenant totals are checked against the
// rows they are printed beside rather than against themselves.
func sumOf(rep Report) time.Duration {
	var d time.Duration
	for _, p := range rep.People {
		d += p.Worked + p.Awaiting + p.Refused
	}
	return d
}

// TestAccumulate_AttributesAnOvernightIntervalToTheDayItSTARTED is the decision the
// M6-07 brief asked to be measured both ways and then stated.
//
// TWO READINGS WERE POSSIBLE:
//
//	count it to the check-in's day   the whole 18:00-02:00 shift lands on Tuesday
//	split it across the midnight     6h on Tuesday, 2h on Wednesday
//
// THE FIRST IS SHIPPED. A shift is what a rota, a payslip and a manager all treat as
// one unit, and splitting it would put two hours on a day the venue was shut -- KF
// St Julians opens at 10:00, so a Wednesday column showing 02:00-02:00 work is a
// column nobody can act on. The split reading is also the one that cannot be undone
// by the reader: from a split total you cannot recover which shift the hours came
// from, whereas from a shift total a split is arithmetic. The screen SAYS which
// reading it uses, because the two give different daily columns for the same week.
func TestAccumulate_AttributesAnOvernightIntervalToTheDayItSTARTED(t *testing.T) {
	t.Parallel()
	zone := malta(t)
	p := week(t, zone)
	evs := []reportEvent{
		ev(maria, "Maria Borg", "in", at(t, zone, 2026, time.August, 4, 18, 0), shift(nightShift())),
		ev(maria, "Maria Borg", "out", at(t, zone, 2026, time.August, 5, 2, 0), shift(nightShift())),
	}
	rep := accumulate(evs, p, zone, at(t, zone, 2026, time.August, 12, 9, 0))
	m := find(t, rep, maria)

	// Tuesday 4 August is index 1 (Monday 3 August is index 0).
	if m.Daily[1] != 8*time.Hour {
		t.Fatalf("Tuesday = %v, want the whole 8h shift", m.Daily[1])
	}
	if m.Daily[2] != 0 {
		t.Fatalf("Wednesday = %v, want nothing -- the shift is attributed to the day it "+
			"STARTED, and splitting it would put work on a day the venue was shut", m.Daily[2])
	}
	var daily time.Duration
	for _, d := range m.Daily {
		daily += d
	}
	if daily != m.Worked {
		t.Fatalf("the daily columns sum to %v and the week says %v -- a row whose "+
			"columns do not add up to its own total is worse than no columns", daily, m.Worked)
	}
}

// TestAccumulate_DailyColumnsSurviveTheDSTSeam is why dayIndex compares civil dates
// rather than dividing a difference by 24 hours.
//
// Malta's fall-back is Sunday 25 October 2026, so that week's Saturday-to-Sunday
// boundary is 25 hours after the Saturday's midnight. A 24-hour divisor puts the
// Sunday shift in Saturday's column.
func TestAccumulate_DailyColumnsSurviveTheDSTSeam(t *testing.T) {
	t.Parallel()
	zone := malta(t)
	p := WeekOf(Date{2026, time.October, 21}, zone) // Mon 19 - Sun 25 October
	evs := []reportEvent{
		ev(maria, "Maria Borg", "in", at(t, zone, 2026, time.October, 25, 12, 0)),
		ev(maria, "Maria Borg", "out", at(t, zone, 2026, time.October, 25, 18, 0)),
	}
	rep := accumulate(evs, p, zone, at(t, zone, 2026, time.November, 2, 9, 0))
	m := find(t, rep, maria)
	if m.Daily[6] != 6*time.Hour {
		t.Fatalf("Sunday (index 6) = %v, want 6h; the columns are %v", m.Daily[6], m.Daily)
	}
}

// TestAccumulate_OpenEntriesSplitOnShiftFromForgotten is the sentence the open list is
// allowed to say and the one it is not.
//
// Q18 requires the list; the M6-07 card requires it NOT to claim why an entry is open.
// What the report CAN measure is how long it has been open, which is the same question
// the decision engine answers with tap.StaleOpenIn -- so the two agree about one row
// by construction rather than by coincidence.
func TestAccumulate_OpenEntriesSplitOnShiftFromForgotten(t *testing.T) {
	t.Parallel()
	zone := malta(t)
	p := week(t, zone)
	in := at(t, zone, 2026, time.August, 7, 10, 0)
	evs := []reportEvent{ev(maria, "Maria Borg", "in", in)}

	onShift := accumulate(evs, p, zone, in.Add(tap.StaleOpenIn-time.Minute))
	if len(onShift.Open) != 1 || onShift.Open[0].Stale {
		t.Fatalf("an entry one minute inside the threshold is somebody ON SHIFT, not a "+
			"forgotten checkout; got %+v", onShift.Open)
	}
	forgotten := accumulate(evs, p, zone, in.Add(tap.StaleOpenIn+time.Minute))
	if len(forgotten.Open) != 1 || !forgotten.Open[0].Stale {
		t.Fatalf("an entry one minute past the threshold needs a manager; got %+v", forgotten.Open)
	}
}

// TestAccumulate_LatenessUsesTheEmployeesOwnShift is M4-05/Q17 read from the report's
// side: the department's shift when there is one, otherwise the TAPPED location's.
func TestAccumulate_LatenessUsesTheEmployeesOwnShift(t *testing.T) {
	t.Parallel()
	zone := malta(t)
	p := week(t, zone)
	after := at(t, zone, 2026, time.August, 12, 9, 0)
	arrive := at(t, zone, 2026, time.August, 3, 10, 20)

	cases := []struct {
		name       string
		shift      *tap.Shift
		lateShifts int
		lateBy     time.Duration
		unmeasured int
	}{
		{
			name:  "twenty minutes after a 10:00 start",
			shift: dayShift(), lateShifts: 1, lateBy: 20 * time.Minute,
		},
		{
			// The SAME tap against a 04:00 department shift is hours late; against a
			// 12:00 one it is early. One instant, three answers, so the shift is what
			// decides rather than the clock.
			name:  "a 12:00 start makes the same tap early",
			shift: &tap.Shift{Start: 12 * time.Hour, End: 20 * time.Hour, Timezone: "Europe/Malta"},
		},
		{
			// 🔴 NO SHIFT MEANS NOT MEASURED, WHICH IS A THIRD ANSWER RATHER THAN "on
			// time". A venue with no opening hours recorded has no yardstick, and
			// reporting 0 minutes late would tell a manager everybody is punctual.
			name: "a venue with no shift is not measured", shift: nil, unmeasured: 1,
		},
		{
			// An unloadable zone is the same refusal for the same reason (M4-05's trap:
			// time.LoadLocation("") answers UTC without an error).
			name:       "an unresolvable zone is not measured",
			shift:      &tap.Shift{Start: 10 * time.Hour, End: 22 * time.Hour, Timezone: ""},
			unmeasured: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			evs := []reportEvent{
				ev(maria, "Maria Borg", "in", arrive, shift(c.shift)),
				ev(maria, "Maria Borg", "out", at(t, zone, 2026, time.August, 3, 18, 0), shift(c.shift)),
			}
			m := find(t, accumulate(evs, p, zone, after), maria)
			if m.LateShifts != c.lateShifts || m.LateBy != c.lateBy {
				t.Errorf("late = %d shift(s) by %v, want %d by %v", m.LateShifts, m.LateBy, c.lateShifts, c.lateBy)
			}
			if m.Unmeasured != c.unmeasured {
				t.Errorf("unmeasured = %d, want %d", m.Unmeasured, c.unmeasured)
			}
			// Lateness NEVER changes the hours (§5: a late tap is still ok).
			if m.Worked != 7*time.Hour+40*time.Minute {
				t.Errorf("worked = %v, want 7h40m whatever the lateness", m.Worked)
			}
		})
	}
}

// TestAccumulate_LatenessCountsArrivalsEvenWhenTheEntryNeverCloses is the reason
// lateness is computed on the check-in rather than on the interval: somebody who
// turned up late and then forgot to tap out was still late.
func TestAccumulate_LatenessCountsArrivalsEvenWhenTheEntryNeverCloses(t *testing.T) {
	t.Parallel()
	zone := malta(t)
	p := week(t, zone)
	in := at(t, zone, 2026, time.August, 7, 10, 30)
	rep := accumulate([]reportEvent{ev(maria, "Maria Borg", "in", in)}, p, zone, in.Add(48*time.Hour))
	m := find(t, rep, maria)
	if m.LateShifts != 1 || m.LateBy != 30*time.Minute {
		t.Fatalf("late = %d by %v, want 1 by 30m", m.LateShifts, m.LateBy)
	}
	if m.Worked != 0 || m.Open != 1 {
		t.Fatalf("worked = %v open = %d: an unclosed entry contributes no hours (Q18)", m.Worked, m.Open)
	}
}

// TestAccumulate_VenueBreakdownFollowsTheCheckIn states which end of a shift decides
// the venue when somebody moves down the chain. §5 treats working at another branch
// as normal, so the shift belongs to the venue it STARTED at -- the same rule the
// daily column uses, applied to the other axis.
func TestAccumulate_VenueBreakdownFollowsTheCheckIn(t *testing.T) {
	t.Parallel()
	zone := malta(t)
	p := week(t, zone)
	evs := []reportEvent{
		ev(maria, "Maria Borg", "in", at(t, zone, 2026, time.August, 3, 10, 0), atVenue(venueA, "KF St Julians")),
		ev(maria, "Maria Borg", "out", at(t, zone, 2026, time.August, 3, 16, 0), atVenue(venueB, "KF Valletta")),
	}
	rep := accumulate(evs, p, zone, at(t, zone, 2026, time.August, 12, 9, 0))
	if len(rep.Venues) != 1 || rep.Venues[0].LocationID != venueA {
		t.Fatalf("venues = %+v, want the six hours at the venue the shift STARTED at", rep.Venues)
	}
	if rep.Venues[0].Worked != 6*time.Hour {
		t.Fatalf("venue worked = %v, want 6h", rep.Venues[0].Worked)
	}
}

// TestAccumulate_MarksManagerEnteredArrivals is the M6-07 card's "manual kayıtlar
// raporda ayrı işaretli". A manual row carries no evidence of a physical touch, so a
// report that blended it in would present a typed figure as a measured one.
func TestAccumulate_MarksManagerEnteredArrivals(t *testing.T) {
	t.Parallel()
	zone := malta(t)
	p := week(t, zone)
	evs := []reportEvent{
		ev(maria, "Maria Borg", "in", at(t, zone, 2026, time.August, 3, 10, 0), manual()),
		ev(maria, "Maria Borg", "out", at(t, zone, 2026, time.August, 3, 16, 0), manual()),
		ev(maria, "Maria Borg", "in", at(t, zone, 2026, time.August, 4, 10, 0)),
		ev(maria, "Maria Borg", "out", at(t, zone, 2026, time.August, 4, 16, 0)),
	}
	m := find(t, accumulate(evs, p, zone, at(t, zone, 2026, time.August, 12, 9, 0)), maria)
	if m.Manual != 1 {
		t.Fatalf("manual arrivals = %d, want 1 of the two shifts", m.Manual)
	}
	if m.Worked != 12*time.Hour {
		t.Fatalf("worked = %v, want 12h: marking a row does not exclude it", m.Worked)
	}
}

// TestAccumulate_TotalsAreExactUnderRepeatedOddMinutes is the arithmetic half of §6's
// "no float".
//
// A hundred shifts of 33 minutes 3 seconds is a quantity a float64 HOUR accumulator
// gets wrong: 1983/3600 has no exact binary representation, and the error accumulates.
// Measured by mutation rather than predicted: with the accumulator rewritten to add
// float64 HOURS and convert back, this case reports 55h4m59.999999996s against the
// exact 55h5m0s, i.e. four nanoseconds short over a hundred shifts.
//
// ⚠️ FOUR NANOSECONDS IS A THIN NET AND IT IS NAMED AS ONE. It catches the mutation
// that was actually run and it would not catch every float somebody could introduce --
// a float64 accumulator of NANOSECONDS, for instance, is exact until the totals pass
// 2^53. The mechanical net beside it is TestReport_NoFloatReachesTheHoursArithmetic,
// which reads the source rather than the answer.
func TestAccumulate_TotalsAreExactUnderRepeatedOddMinutes(t *testing.T) {
	t.Parallel()
	zone := malta(t)
	p := week(t, zone)
	const n = 100
	const span = 1983 * time.Second

	start := at(t, zone, 2026, time.August, 3, 0, 0)
	evs := make([]reportEvent, 0, 2*n)
	for i := 0; i < n; i++ {
		// Each pair sits in its own hour, so they never overlap and never straddle the
		// end of the week (100 hours from Monday midnight is Friday morning).
		in := start.Add(time.Duration(i) * time.Hour)
		evs = append(evs,
			ev(maria, "Maria Borg", "in", in, shift(nil)),
			ev(maria, "Maria Borg", "out", in.Add(span), shift(nil)),
		)
	}
	m := find(t, accumulate(evs, p, zone, at(t, zone, 2026, time.August, 12, 9, 0)), maria)
	if want := n * span; m.Worked != want {
		t.Fatalf("worked = %v, want exactly %v (%d x %v)", m.Worked, want, n, span)
	}
}

// TestReport_NoFloatReachesTheHoursArithmetic is §6 asserted MECHANICALLY rather than
// through an answer that happens to round correctly.
//
// 🔴 IT READS THE SOURCE, WHICH IS THE ONLY PLACE THE RULE IS VISIBLE. An arithmetic
// test can only see the ROUNDED result, and float64 rounds to the right nanosecond
// most of the time -- so a float accumulator passes almost every case one could write.
// The property §6 states is about the REPRESENTATION, and the representation is in the
// file. This is the same argument query_test.go makes for the tenant predicate.
//
// ⚠️ WHAT IT DOES NOT COVER, counted rather than claimed shut: it reads report.go and
// the two report queries. A float introduced in a helper in another file of this
// package, or in the handler that formats these values, is not seen here.
func TestReport_NoFloatReachesTheHoursArithmetic(t *testing.T) {
	t.Parallel()

	// 🔴 IT PARSES RATHER THAN GREPS, AND THE FIRST VERSION DID NOT -- WHICH FAILED
	// IMMEDIATELY AND CORRECTLY. A text scan for "float64" reddens on the PROSE that
	// explains why there is no float, so the only ways past it are to delete the
	// explanation or to loosen the scanner. Both are worse than the defect they would
	// hide: this repository has already paid once for a net weakened to get a comment
	// through. An AST sees declarations and not sentences, so the rule and the reason
	// for the rule can coexist.
	//
	// 🔴 AND THE FIRST AST VERSION LOOKED FOR THE TYPE NAME ONLY, WHICH TWO AUDITS BEAT
	// WITH THE SAME MOVE. time.Duration's OWN accessors return float64 without the word
	// appearing anywhere:
	//
	//	ph.Worked = time.Duration(ph.Worked.Seconds()*1e9 + worked.Seconds()*1e9)
	//
	// That is a genuine float accumulator, it left the whole package green, and it is
	// the exact shape TestAccumulate_TotalsAreExactUnderRepeatedOddMinutes exists to
	// catch -- which it did not, because the drift was one nanosecond and hoursMinutes
	// truncates to the minute. Invisible on this screen; NOT invisible in phase B's
	// export, which writes the same values at second resolution.
	//
	// So the scan now covers three shapes: the type NAMES, the three Duration accessors
	// that hand you a float, and any FLOAT LITERAL (1e9, 3600.0) -- because an
	// accumulator that multiplies by one of those is doing float arithmetic whatever it
	// calls the variable.
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "report.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing report.go: %v", err)
	}
	// floatAccessors are time.Duration's float-valued methods. There is no
	// integer-valued twin to confuse them with: Duration's whole-unit accessors are
	// Nanoseconds/Microseconds/Milliseconds, and division by a unit constant
	// (d / time.Hour) is the integer form this file uses throughout.
	floatAccessors := map[string]bool{"Seconds": true, "Minutes": true, "Hours": true}
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.FuncDecl:
			// ANTI-VACUITY: a scanner pointed at nothing passes forever.
			if v.Name.Name == "accumulate" {
				found = true
			}
		case *ast.BasicLit:
			if v.Kind == token.FLOAT {
				t.Errorf("report.go carries the float literal %s at %s. §6: an attendance "+
					"figure is time.Duration or whole minutes, and a float constant in this "+
					"file is arithmetic that has already left the integers.",
					v.Value, fset.Position(v.Pos()))
			}
		case *ast.SelectorExpr:
			if floatAccessors[v.Sel.Name] {
				t.Errorf("report.go calls .%s() at %s, which returns a float64 without "+
					"naming one. Measured: an accumulator built from it is off by a "+
					"nanosecond per week -- invisible on a screen that truncates to the "+
					"minute, and visible in the export phase B writes.",
					v.Sel.Name, fset.Position(v.Pos()))
			}
		case *ast.Ident:
			if v.Name == "float64" || v.Name == "float32" {
				t.Errorf("report.go names %s at %s. §6: a time/attendance figure is "+
					"time.Duration or whole minutes, never a float -- and a float that rounds "+
					"correctly today is not a fix, it is a coincidence.",
					v.Name, fset.Position(v.Pos()))
			}
		}
		return true
	})
	if !found {
		t.Fatal("report.go does not declare the accumulator; this scan is reading the wrong file")
	}
	// ⚠️ WHAT IS STILL OPEN, AND THE SENTENCE HERE USED TO BE NARROWER THAN THE HOLE.
	// It said "a float in another file, in the handler, or reached through an alias",
	// which reads as three exotic cases. The real class is one ordinary case:
	//
	//	ANY CALL THAT RETURNS float64 AND IS NOT NAMED Seconds, Minutes OR Hours,
	//	INSIDE report.go ITSELF.
	//
	// This scan matches NAMES. It cannot know what a call returns, so every float64 the
	// standard library or this package hands back under a different name walks past it.
	// Two escapes were built and re-run here rather than taken on trust; both compile,
	// both are genuine float accumulators, and both leave this test AND
	// TestAccumulate_TotalsAreExactUnderRepeatedOddMinutes green:
	//
	//	esc, _ := strconv.ParseFloat(strconv.FormatInt(int64(ph.Worked)+int64(worked), 10), 64)
	//	ph.Worked = time.Duration(esc / 60 * 60)
	//
	//	// with `func asFloat(d time.Duration) float64` in ledger.go, one file away:
	//	ph.Worked = time.Duration(asFloat(ph.Worked) + asFloat(worked))
	//
	// ⚠️ MEASURED CORRECTION TO THE SECOND ONE: written with that helper INSIDE report.go
	// it is CAUGHT, because the helper's return type is the identifier float64 in this
	// file. What defeats the scan is the helper being one file away -- which is exactly
	// why the hole is about names rather than about files.
	//
	// 🔴 CLOSING IT NEEDS go/types RATHER THAN go/ast, AND THAT IS WHY IT IS NOT CLOSED
	// HERE. An AST knows the SPELLING of a call; only a type-checked package knows its
	// RESULT TYPE, so the general form is: load the package with go/packages, walk every
	// ast.CallExpr, ask types.Info for the type of the expression, and refuse
	// types.Float64/Float32. That is a build-tagged tool with its own dependency and its
	// own runtime, not a line in this test -- so the channel is COUNTED rather than
	// claimed shut, which is what this file does everywhere else.
	//
	// WHAT THE SCAN IS STILL WORTH: it makes the CHEAP mistakes loud. Writing float64,
	// reaching for .Seconds() or typing a float literal are what somebody does by
	// accident; the two escapes above are what somebody does on purpose.
	//
	// AND Period.Days() IS DELIBERATELY NOT A FALSE ALARM: the accessor set is
	// Duration's three, and `Days` is not one of them. If Duration ever gains a Days()
	// this list is the place that has to notice.

	sqlSrc, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "queries", "transactions.sql"))
	if err != nil {
		t.Fatalf("reading transactions.sql: %v", err)
	}
	body := reportQueryBodies(t, string(sqlSrc))
	if body == "" {
		t.Fatal("neither report query was found in transactions.sql; this scan is reading nothing")
	}
	banned := regexp.MustCompile(`(?i)::\s*(float|double)|EXTRACT\s*\(\s*EPOCH|\bAVG\s*\(`)
	if m := banned.FindString(body); m != "" {
		t.Errorf("a report query uses %q. The hours arithmetic is Go's, on time.Duration; "+
			"pushing it into SQL as seconds-as-float is how a payroll total acquires a "+
			"rounding error nobody can see.", m)
	}
}

// reportQueryBodies returns the SQL of the two M6-07 queries, comments stripped so a
// float mentioned in PROSE cannot fail the scan and a float hidden in a comment-like
// string cannot pass it.
func reportQueryBodies(t *testing.T, raw string) string {
	t.Helper()
	var out []string
	for _, name := range []string{"ListWorkedShiftEvents", "CountPracticeTaps"} {
		loc := regexp.MustCompile(`(?m)^-- name: ` + name + ` `).FindStringIndex(raw)
		if loc == nil {
			continue
		}
		rest := raw[loc[1]:]
		if j := regexp.MustCompile(`(?m)^-- name: `).FindStringIndex(rest); j != nil {
			rest = rest[:j[0]]
		}
		for _, line := range strings.Split(rest, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "--") {
				continue
			}
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// TestResolveShift_PrefersTheDepartmentThenTheTAPPEDLocation is M4-05/Q17 at the row
// mapper, and it is a DIRECT test because the composition above cannot see it: every
// case in this file hands accumulate a shift that is already resolved.
//
// 🔴 THE LOCATION IS THE TAPPED ONE AND THERE IS NO OTHER TO CONFUSE IT WITH. The query
// joins on transactions.location_id -- where the plaque is -- so a person covering a
// shift at another branch is measured against THAT venue's opening time. The employee's
// home venue is not selected at all, which is why this function cannot reach it.
func TestResolveShift_PrefersTheDepartmentThenTheTAPPEDLocation(t *testing.T) {
	t.Parallel()
	pgTime := func(d time.Duration) pgtype.Time {
		return pgtype.Time{Microseconds: int64(d / time.Microsecond), Valid: true}
	}
	yes, no := true, false

	cases := []struct {
		name      string
		row       store.ListWorkedShiftEventsRow
		want      *tap.Shift
		wantWhich string
	}{
		{
			name: "a department shift wins over the venue's",
			row: store.ListWorkedShiftEventsRow{
				LocationShiftStart: pgTime(10 * time.Hour), LocationShiftEnd: pgTime(22 * time.Hour),
				LocationOvernight:    &no,
				DepartmentShiftStart: pgTime(4 * time.Hour),
				DepartmentShiftEnd:   pgTime(12 * time.Hour),
				DepartmentOvernight:  &no,
			},
			want:      &tap.Shift{Start: 4 * time.Hour, End: 12 * time.Hour, Timezone: "Europe/Malta"},
			wantWhich: "the department's",
		},
		{
			name: "the venue's when the department has none",
			row: store.ListWorkedShiftEventsRow{
				LocationShiftStart: pgTime(18 * time.Hour), LocationShiftEnd: pgTime(2 * time.Hour),
				LocationOvernight: &yes,
			},
			want:      &tap.Shift{Start: 18 * time.Hour, End: 2 * time.Hour, Overnight: true, Timezone: "Europe/Malta"},
			wantWhich: "the tapped venue's",
		},
		{
			// 🔴 nil MEANS "LATENESS IS NOT COMPUTED", which is a different answer from
			// "on time" -- the distinction internal/domain/checkin's shiftOf makes for
			// the write path and this one has to make for the read path.
			name: "no shift anywhere", row: store.ListWorkedShiftEventsRow{}, want: nil,
			wantWhich: "nothing",
		},
		{
			// A start with no end is refused by migration 0002's shift_pair CHECK, so
			// this cannot arrive through the schema; guessing an end would invent a wall
			// clock, which is the trap M4-05 names.
			name: "a half-filled venue shift is no shift",
			row: store.ListWorkedShiftEventsRow{
				LocationShiftStart: pgTime(10 * time.Hour),
			},
			want: nil, wantWhich: "nothing",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := resolveShift(c.row, "Europe/Malta")
			switch {
			case c.want == nil && got != nil:
				t.Fatalf("resolved %+v, want %s", *got, c.wantWhich)
			case c.want == nil:
				return
			case got == nil:
				t.Fatalf("resolved nothing, want %s", c.wantWhich)
			case *got != *c.want:
				t.Fatalf("resolved %+v, want %+v (%s)", *got, *c.want, c.wantWhich)
			}
		})
	}
}

// TestEvents_CountsTheRecordsNoChainCanHold is §4.6 at the mapper: migration 0005 lets
// a record exist with no employee on it, no chain can attribute hours to one, and the
// honest answer is to COUNT it rather than to drop it.
func TestEvents_CountsTheRecordsNoChainCanHold(t *testing.T) {
	t.Parallel()
	who := maria
	in := "in"
	rows := []store.ListWorkedShiftEventsRow{
		{EmployeeID: &who, Type: &in, Verdict: "ok", Channel: "nfc"},
		{Type: &in, Verdict: "ok", Channel: "nfc"},        // nobody on it
		{EmployeeID: &who, Verdict: "ok", Channel: "nfc"}, // no direction
		{EmployeeID: &who, Type: &in, Verdict: "ok", Channel: "manual"},
	}
	got, unattributable := events(rows, "Europe/Malta")
	if len(got) != 2 {
		t.Fatalf("mapped %d event(s), want 2", len(got))
	}
	if unattributable != 2 {
		t.Fatalf("counted %d unattributable row(s), want 2 -- a record that cannot be "+
			"attributed is still a record (§4.6)", unattributable)
	}
	if !got[1].Manual {
		t.Error("a channel='manual' row is not marked manual, so a typed figure would " +
			"render as a measured one")
	}
}

// THE PROPORTIONS endpointState's COMMENT ARGUES FROM come from this query, printed
// here rather than in the comment so the comment can carry the ratio and this can carry
// the way to refresh it (2026-08-10: 25 550 undecided / 2 058 approved / 476 refused of
// 28 084):
//
//	SELECT count(*) FILTER (WHERE rv.outcome IS NULL)        AS pending,
//	       count(*) FILTER (WHERE rv.outcome = 'approved')   AS approved,
//	       count(*) FILTER (WHERE rv.outcome = 'rejected')   AS refused,
//	       count(*)                                          AS total
//	FROM transactions t
//	LEFT JOIN transaction_reviews rv
//	       ON rv.tenant_id = t.tenant_id AND rv.transaction_id = t.id
//	WHERE t.verdict = 'flag' AND t.type IS NOT NULL AND NOT t.practice;

// TestEndpointState_NeverPaysAVerdictItDoesNotKNOW is the fail-safe, and it exists
// because a security audit measured the opposite.
//
// 🔴 THE OLD DEFAULT WAS "counted" AND THE ARGUMENT FOR IT WAS TRUE BUT MISPLACED.
// reject and ignored carry no direction, so the query's `type IS NOT NULL` excludes
// them — today. What enforces that is internal/domain/tap's direction gate, a CODE
// invariant, not a schema CHECK: nothing in migration 0005 forbids a directed reject.
// M6-08 adds a second writer to this table, and the audit's measurement is what the
// day after that looks like: endpointState("reject", "") answered "counted", and a
// hand-inserted refused pair produced Totals.Worked = 8h, Shifts = 1.
//
// THE TABLE IS THE CLOSED SET PLUS THE THINGS OUTSIDE IT. Both halves matter: a
// default that refused everything would also refuse `ok`.
func TestEndpointState_NeverPaysAVerdictItDoesNotKNOW(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		verdict string
		review  string
		want    HoursState
	}{
		// The payable set, named rather than derived by exclusion.
		{name: "a clean tap", verdict: "ok", want: HoursCounted},
		{name: "a flag a manager approved", verdict: "flag", review: "approved", want: HoursCounted},
		// Held back, and each with its own sentence on the screen.
		{name: "a flag nobody has decided", verdict: "flag", want: HoursAwaiting},
		{name: "a flag a manager refused", verdict: "flag", review: "rejected", want: HoursRefused},
		// 🔴 THE FOUR THE AUDIT MEASURED AS PAYABLE. None of them can reach this
		// function today; all four are one new writer away from doing so.
		{name: "an engine refusal", verdict: "reject", want: HoursAwaiting},
		{name: "a debounced duplicate", verdict: "ignored", want: HoursAwaiting},
		{name: "a verdict this build has never heard of", verdict: "void", want: HoursAwaiting},
		{name: "no verdict at all", verdict: "", want: HoursAwaiting},
		// A review outcome on a verdict that is not `flag` must not make it payable
		// either: approval is what turns a FLAG into hours, not a blank cheque.
		{name: "an approved reject", verdict: "reject", review: "approved", want: HoursAwaiting},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := endpointState(c.verdict, c.review); got != c.want {
				t.Fatalf("endpointState(%q, %q) = %q, want %q", c.verdict, c.review, got, c.want)
			}
		})
	}

	// AND THE COMPOSITION, because a safe endpointState is only useful if the interval
	// built from it is held back too. A refused pair must pay NOTHING.
	zone := malta(t)
	p := week(t, zone)
	evs := []reportEvent{
		ev(maria, "Maria Borg", "in", at(t, zone, 2026, time.August, 3, 10, 0), verdict("reject", "")),
		ev(maria, "Maria Borg", "out", at(t, zone, 2026, time.August, 3, 18, 0), verdict("reject", "")),
	}
	rep := accumulate(evs, p, zone, at(t, zone, 2026, time.August, 12, 9, 0))
	if rep.Totals.Worked != 0 || rep.Totals.Shifts != 0 {
		t.Fatalf("a refused pair paid %v over %d shift(s); the engine refused that tap",
			rep.Totals.Worked, rep.Totals.Shifts)
	}
	// AND IT IS NOT LOST EITHER (§4.6): the hours are reported, just not in payroll.
	if rep.Totals.Awaiting != 8*time.Hour {
		t.Fatalf("the refused pair's 8h are nowhere: awaiting = %v", rep.Totals.Awaiting)
	}
}

// TestReadLimit_MakesTruncationDETECTABLE is the first link of the "a truncated read is
// never presented as a total" chain, and it had no test at all.
//
// 🔴 THE CHAIN HAS THREE LINKS AND ONLY THE LAST TWO WERE MEASURED. The screen refuses
// to present a floor as a total (handler test), the domain carries the flag (fake
// reader) -- and NOTHING checked that the flag can ever become true. It becomes true
// only if the query is asked for MORE rows than the cap, which is an invariant between
// two expressions that lived on different lines of Hours.
func TestReadLimit_MakesTruncationDETECTABLE(t *testing.T) {
	t.Parallel()
	// 🔴 THE INVARIANT ITSELF: a limit at or below the cap makes truncation
	// undetectable, whatever truncatedBy says.
	if int(readLimit()) <= ReportEventCap {
		t.Fatalf("readLimit() = %d and the cap is %d: the query can never return more rows "+
			"than can be used, so Truncated is dead code and a lost half of a payroll "+
			"renders as a week's hours", readLimit(), ReportEventCap)
	}

	cases := []struct {
		name string
		rows int
		want bool
	}{
		{name: "an ordinary week", rows: 1, want: false},
		{name: "a week that exactly fills the budget", rows: ReportEventCap, want: false},
		{name: "the first row that did not fit", rows: ReportEventCap + 1, want: true},
		{name: "everything the limit allows", rows: int(readLimit()), want: true},
		{name: "an empty week", rows: 0, want: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := truncatedBy(c.rows); got != c.want {
				t.Fatalf("truncatedBy(%d) = %v, want %v", c.rows, got, c.want)
			}
		})
	}
}

// TestHours_RefusesToAnswerWhenTheDatabaseDoesNOT is what a fake CAN prove about Hours
// without standing up Postgres, and it is the §4.6 half that matters most: a failed
// read must not come back as a Report at all.
//
// ⚠️ WHAT IT DOES NOT COVER, COUNTED RATHER THAN CLAIMED: everything past WithTenant.
// Faking the success path means faking pgx.Tx and pgx.Rows for a fifteen-column scan,
// which is a second implementation of the driver -- so the query, the mapping and the
// truncation verdict are measured against real Postgres (internal/handler's DB tests)
// and by the pure tests above. Hours' own body is covered by the former.
func TestHours_RefusesToAnswerWhenTheDatabaseDoesNOT(t *testing.T) {
	t.Parallel()
	r, err := NewReader(refusingDatabase{}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	got, err := r.Hours(context.Background(), uuid.New(), ReportFilter{})
	if err == nil {
		t.Fatal("a failed read answered with a report; §4.6: a timeout is not evidence " +
			"that nobody worked")
	}
	if !strings.Contains(err.Error(), "ledger: hours:") {
		t.Errorf("the error is not wrapped by this layer: %v", err)
	}
	// 🔴 THE ANTI-FABRICATION FLAG MUST STAY FALSE. A zero Report renders identically to
	// an empty week, so the one thing a failed read may never return is Queried.
	if got.Queried {
		t.Error("a failed read came back marked as queried")
	}
}

type refusingDatabase struct{}

func (refusingDatabase) WithTenant(context.Context, uuid.UUID, db.TxFunc) error {
	return errors.New("connection refused")
}
