package ledger

// rollmarks_test.go -- M12-21: what one cell of the Roll says besides its hours (DayMarks), and the
// receipt's new counters (Totals.OtherVenue / OtherVenueUnknown / ManualArrivals / Approved). One
// case per rule; the expected values are read off the wall clocks in the case.

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/store"
)

// peerVenue is a holding PEER's venue (ADR 0027 §9): the employer's row carries it as
// site_location_id, and events() files it under that id.
var peerVenue = uuid.MustParse("cccccccc-0000-4000-8000-000000000003")

func home(id uuid.UUID) evOpt { return func(e *reportEvent) { e.Home = &id } }

func TestAccumulate_MarksTheRoll(t *testing.T) {
	t.Parallel()
	zone := malta(t)
	p := week(t, zone)
	after := at(t, zone, 2026, time.August, 12, 9, 0)
	mon := func(hh, mm int) time.Time { return at(t, zone, 2026, time.August, 3, hh, mm) }

	type marks struct{ other, manual, open []int } // the day indexes that carry each mark
	cases := []struct {
		name   string
		events []reportEvent
		want   marks
		// totals
		shifts, otherVenue, unknown, manualArrivals, approved, open int
		daily0                                                      time.Duration
	}{
		{
			name: "a shift at the home venue carries no mark",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", mon(10, 0), home(venueA)),
				ev(maria, "Maria Borg", "out", mon(18, 0), home(venueA)),
			},
			shifts: 1, daily0: 8 * time.Hour,
		},
		{
			name: "a shift at another of the business's venues is marked other",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", mon(10, 0), home(venueA), atVenue(venueB, "KF Valletta")),
				ev(maria, "Maria Borg", "out", mon(18, 0), home(venueA), atVenue(venueB, "KF Valletta")),
			},
			want:   marks{other: []int{0}},
			shifts: 1, otherVenue: 1, daily0: 8 * time.Hour,
		},
		{
			// ADR 0027 §9: a peer's venue is not the person's; durations and a mark only (K-39).
			name: "a holding peer's venue counts as another venue",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", mon(10, 0), home(venueA), atVenue(peerVenue, "KM Sliema")),
				ev(maria, "Maria Borg", "out", mon(18, 0), home(venueA), atVenue(peerVenue, "KM Sliema")),
			},
			want:   marks{other: []int{0}},
			shifts: 1, otherVenue: 1, daily0: 8 * time.Hour,
		},
		{
			// K-14: no home venue -> no claim either way; the shift is counted as NOT JUDGED.
			name: "a person with no home venue is not judged",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", mon(10, 0), atVenue(venueB, "KF Valletta")),
				ev(maria, "Maria Borg", "out", mon(18, 0), atVenue(venueB, "KF Valletta")),
			},
			shifts: 1, unknown: 1, daily0: 8 * time.Hour,
		},
		{
			name: "a record that names no venue is not judged",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", mon(10, 0), home(venueA), atVenue(uuid.Nil, "")),
				ev(maria, "Maria Borg", "out", mon(18, 0), home(venueA), atVenue(uuid.Nil, "")),
			},
			shifts: 1, unknown: 1, daily0: 8 * time.Hour,
		},
		{
			// Only COUNTED shifts are judged: an undecided flag's hours are not in the cell.
			name: "an awaiting shift at another venue carries no other mark",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", mon(10, 0), home(venueA), atVenue(venueB, "KF Valletta"), verdict("flag", "")),
				ev(maria, "Maria Borg", "out", mon(18, 0), home(venueA), atVenue(venueB, "KF Valletta")),
			},
		},
		{
			name: "a typed check-in marks its day manual",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", mon(10, 0), home(venueA), manual()),
				ev(maria, "Maria Borg", "out", mon(18, 0), home(venueA)),
			},
			want:   marks{manual: []int{0}},
			shifts: 1, manualArrivals: 1, daily0: 8 * time.Hour,
		},
		{
			// The decision recorded on DayMarks.Manual: arrivals, as ManualArrivals counts them.
			name: "a typed checkout alone carries no manual mark",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", mon(10, 0), home(venueA)),
				ev(maria, "Maria Borg", "out", mon(18, 0), home(venueA), manual()),
			},
			shifts: 1, daily0: 8 * time.Hour,
		},
		{
			// R11: a day with hours AND an open check-in -- both are on the cell.
			name: "an open check-in on a worked day marks the day open and keeps its hours",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", mon(10, 0), home(venueA)),
				ev(maria, "Maria Borg", "out", mon(14, 0), home(venueA)),
				ev(maria, "Maria Borg", "in", mon(19, 0), home(venueA)),
			},
			want:   marks{open: []int{0}},
			shifts: 1, open: 1, daily0: 4 * time.Hour,
		},
		{
			name: "a typed check-in nobody closed is manual and open, with no hours",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", mon(10, 0), home(venueA), manual()),
			},
			want:           marks{manual: []int{0}, open: []int{0}},
			manualArrivals: 1, open: 1,
		},
		{
			name: "a flag approved by a manager is approved from the spike",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", mon(10, 0), home(venueA), verdict("flag", "approved")),
				ev(maria, "Maria Borg", "out", mon(18, 0), home(venueA)),
			},
			shifts: 1, approved: 1, daily0: 8 * time.Hour,
		},
		{
			name: "both ends approved is still one shift",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", mon(10, 0), home(venueA), verdict("flag", "approved")),
				ev(maria, "Maria Borg", "out", mon(18, 0), home(venueA), verdict("flag", "approved")),
			},
			shifts: 1, approved: 1, daily0: 8 * time.Hour,
		},
		{
			name: "an ok shift is not approved from the spike, a refused one is not counted",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", mon(8, 0), home(venueA)),
				ev(maria, "Maria Borg", "out", mon(9, 0), home(venueA)),
				ev(maria, "Maria Borg", "in", mon(10, 0), home(venueA), verdict("flag", "rejected")),
				ev(maria, "Maria Borg", "out", mon(18, 0), home(venueA)),
			},
			shifts: 1, daily0: time.Hour,
		},
		{
			// §5: the Rusty Bar shift lands on the day it STARTED, and so does its mark.
			name: "an overnight shift at another venue is marked on the day it started",
			events: []reportEvent{
				ev(maria, "Maria Borg", "in", at(t, zone, 2026, time.August, 5, 18, 0), home(venueA), atVenue(venueB, "Rusty Bar"), shift(nightShift())),
				ev(maria, "Maria Borg", "out", at(t, zone, 2026, time.August, 6, 2, 0), home(venueA), atVenue(venueB, "Rusty Bar"), shift(nightShift())),
			},
			want:   marks{other: []int{2}},
			shifts: 1, otherVenue: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			rep := accumulate(c.events, p, zone, after)
			got := marks{}
			ph := find(t, rep, maria)
			if len(ph.Marks) != p.Days() {
				t.Fatalf("Marks has %d days, want %d (one per Daily column)", len(ph.Marks), p.Days())
			}
			for d, m := range ph.Marks {
				if m.Other {
					got.other = append(got.other, d)
				}
				if m.Manual {
					got.manual = append(got.manual, d)
				}
				if m.Open {
					got.open = append(got.open, d)
				}
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("marks: got other %v manual %v open %v, want other %v manual %v open %v",
					got.other, got.manual, got.open, c.want.other, c.want.manual, c.want.open)
			}
			tt := rep.Totals
			for _, f := range []struct {
				what      string
				got, want int
			}{
				{"Shifts", tt.Shifts, c.shifts}, {"OtherVenue", tt.OtherVenue, c.otherVenue},
				{"OtherVenueUnknown", tt.OtherVenueUnknown, c.unknown},
				{"ManualArrivals", tt.ManualArrivals, c.manualArrivals}, {"Approved", tt.Approved, c.approved},
				{"Open", tt.Open, c.open},
			} {
				if f.got != f.want {
					t.Errorf("Totals.%s = %d, want %d", f.what, f.got, f.want)
				}
			}
			if ph.Daily[0] != c.daily0 {
				t.Errorf("Monday's hours = %v, want %v", ph.Daily[0], c.daily0)
			}
			// The receipt's counters are subsets of what they sit beside.
			if tt.OtherVenue+tt.OtherVenueUnknown > tt.Shifts || tt.Approved > tt.Shifts {
				t.Errorf("a counter exceeds Shifts: %+v", tt)
			}
			if tt.ManualArrivals != ph.ManualArrivals {
				t.Errorf("Totals.ManualArrivals %d is not the sum of the people's %d", tt.ManualArrivals, ph.ManualArrivals)
			}
			for _, o := range rep.Open {
				if o.EmployeeID != maria {
					t.Errorf("an open entry names %s, want Maria's id -- the Roll marks the day on that row", o.EmployeeID)
				}
			}
		})
	}
}

// TestAccumulate_MarksAreOnTheRightPerson: two people at the same venue, one at home and one
// away -- the mark is on the second row only, and each open entry carries its own person.
func TestAccumulate_MarksAreOnTheRightPerson(t *testing.T) {
	t.Parallel()
	zone := malta(t)
	p := week(t, zone)
	mon := func(hh, mm int) time.Time { return at(t, zone, 2026, time.August, 3, hh, mm) }
	rep := accumulate([]reportEvent{
		ev(maria, "Maria Borg", "in", mon(10, 0), home(venueA)),
		ev(maria, "Maria Borg", "out", mon(18, 0), home(venueA)),
		ev(antoine, "Antoine Vella", "in", mon(10, 0), home(venueB)),
		ev(antoine, "Antoine Vella", "out", mon(18, 0), home(venueB)),
		ev(antoine, "Antoine Vella", "in", mon(19, 0), home(venueB)),
	}, p, zone, at(t, zone, 2026, time.August, 12, 9, 0))
	if find(t, rep, maria).Marks[0].Other {
		t.Error("Maria worked at her home venue and is marked other")
	}
	if !find(t, rep, antoine).Marks[0].Other || !find(t, rep, antoine).Marks[0].Open {
		t.Errorf("Antoine worked away from home and left one entry open: %+v", find(t, rep, antoine).Marks[0])
	}
	if len(rep.Open) != 1 || rep.Open[0].EmployeeID != antoine {
		t.Errorf("open entries %+v, want exactly Antoine's", rep.Open)
	}
}

// TestResolveShift_CannotReachTheHomeVenue is §5/Q17 kept STRUCTURAL after M12-21 read the home
// venue: the row resolveShift is handed has no field for it, because the home is read by a
// separate statement (ListRollEmployees). A home column added to ListWorkedShiftEvents turns this
// red before a lateness figure could follow the home venue instead of the tapped one.
func TestResolveShift_CannotReachTheHomeVenue(t *testing.T) {
	t.Parallel()
	rt := reflect.TypeOf(store.ListWorkedShiftEventsRow{})
	if rt.NumField() == 0 {
		t.Fatal("the worked-shift row has no fields; this scan reads the wrong type")
	}
	for i := 0; i < rt.NumField(); i++ {
		if name := rt.Field(i).Name; strings.Contains(strings.ToLower(name), "home") || name == "Role" {
			t.Errorf("ListWorkedShiftEventsRow.%s: the person's home venue reached the row resolveShift reads (§5/Q17)", name)
		}
	}
	// POSITIVE CONTROL: the roster read does carry it.
	if _, ok := reflect.TypeOf(store.ListRollEmployeesRow{}).FieldByName("HomeLocationID"); !ok {
		t.Error("ListRollEmployeesRow has no HomeLocationID; the control reads the wrong type")
	}

	// The field names above are what sqlc chose; the SQL is what reads. The statement selects
	// exactly one employees column, full_name, and touches employees nowhere else but the join's
	// own keys -- so neither a renamed column (`e.location_id AS venue`) nor a join through the
	// home venue (`l.id = e.location_id`) can bring the home into the row. Read comment-free
	// (findQuery), so a column named in prose cannot satisfy it.
	_, body, ok := findQuery(t, "ListWorkedShiftEvents")
	if !ok {
		t.Fatal("ListWorkedShiftEvents was not found in db/queries; this scan is reading nothing")
	}
	flat := strings.Join(strings.Fields(body), " ")
	sel, from := strings.Index(flat, "SELECT "), strings.Index(flat, " FROM transactions t ")
	if sel < 0 || from < sel {
		t.Fatalf("ListWorkedShiftEvents is not SELECT ... FROM transactions t: %q", flat)
	}
	column := regexp.MustCompile(`\be\.(\*|[a-z_][a-z0-9_]*)`)
	selected := column.FindAllStringSubmatch(flat[sel:from], -1)
	if len(selected) != 1 || selected[0][1] != "full_name" {
		t.Errorf("ListWorkedShiftEvents selects employees columns %v, want e.full_name alone (§5/Q17)", selected)
	}
	for _, m := range column.FindAllStringSubmatch(flat[from:], -1) {
		if m[1] != "id" && m[1] != "tenant_id" {
			t.Errorf("ListWorkedShiftEvents reads e.%s outside its select list; only the join's keys "+
				"(e.id, e.tenant_id) may appear there (§5/Q17)", m[1])
		}
	}
}
