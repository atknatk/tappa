package ledger

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/store"
)

// TestTapeOf_CountsPeopleOnShiftPerVenuePerHour is M12-14's arithmetic, one case per rule in
// tape.go's header. Times are Malta wall clocks on 2 September 2026 unless a case says otherwise.
func TestTapeOf_CountsPeopleOnShiftPerVenuePerHour(t *testing.T) {
	zone, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	at := func(day, clock string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", day+" "+clock, zone)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	today := func(clock string) time.Time { return at("2026-09-02", clock) }
	bar := tapeVenueRef{id: uuid.MustParse("00000000-0000-4000-8000-0000000000b1"), name: "Bar"}
	kitchen := tapeVenueRef{id: uuid.MustParse("00000000-0000-4000-8000-0000000000c1"), name: "Kitchen"}
	partner := tapeVenueRef{id: uuid.MustParse("00000000-0000-4000-8000-0000000000d1"), name: "Another's door", partner: true}
	ann := uuid.MustParse("00000000-0000-4000-8000-00000000a001")
	bob := uuid.MustParse("00000000-0000-4000-8000-00000000a002")
	in := func(who uuid.UUID, v tapeVenueRef, when time.Time) tapeEvent {
		return tapeEvent{who: who, at: when, in: true, venue: v}
	}
	out := func(who uuid.UUID, v tapeVenueRef, when time.Time) tapeEvent {
		return tapeEvent{who: who, at: when, venue: v}
	}
	// hoursOn is a row's expected counts: 1 for each listed hour index, 0 elsewhere.
	hoursOn := func(n int, on ...int) []int {
		row := make([]int, n)
		for _, h := range on {
			row[h]++
		}
		return row
	}

	tests := []struct {
		name     string
		evs      []tapeEvent
		midnight time.Time
		now      time.Time
		hours    int
		want     map[tapeVenueRef][]int
	}{
		{
			name:  "an open entry runs to the hour holding now",
			evs:   []tapeEvent{in(ann, bar, today("09:15"))},
			now:   today("11:30"),
			hours: 12,
			want:  map[tapeVenueRef][]int{bar: hoursOn(12, 9, 10, 11)},
		},
		{
			name:  "overnight: an entry from before midnight shows from 00:00",
			evs:   []tapeEvent{in(ann, bar, at("2026-09-01", "22:00")), out(ann, bar, today("02:10"))},
			now:   today("03:00"),
			hours: 4,
			want:  map[tapeVenueRef][]int{bar: hoursOn(4, 0, 1, 2)},
		},
		{
			name:  "an open entry older than tap.StaleOpenIn is not on shift",
			evs:   []tapeEvent{in(ann, bar, today("12:30").Add(-tap.StaleOpenIn-time.Minute))},
			now:   today("12:30"),
			hours: 13,
			want:  map[tapeVenueRef][]int{},
		},
		{
			name:  "an open entry exactly tap.StaleOpenIn old still is",
			evs:   []tapeEvent{in(ann, bar, today("12:30").Add(-tap.StaleOpenIn))},
			now:   today("12:30"),
			hours: 13,
			want:  map[tapeVenueRef][]int{bar: hoursOn(13, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12)},
		},
		{
			name:  "a checkout further than tap.StaleOpenIn closes nothing and is no shift",
			evs:   []tapeEvent{in(ann, bar, at("2026-09-01", "06:30")), out(ann, bar, today("01:00"))},
			now:   today("02:00"),
			hours: 3,
			want:  map[tapeVenueRef][]int{},
		},
		{
			name:  "a second check-in leaves the first uncounted",
			evs:   []tapeEvent{in(ann, bar, today("08:00")), in(ann, kitchen, today("10:00"))},
			now:   today("11:10"),
			hours: 12,
			want:  map[tapeVenueRef][]int{kitchen: hoursOn(12, 10, 11)},
		},
		{
			name: "one person twice in one hour counts once",
			evs: []tapeEvent{
				in(ann, bar, today("09:00")), out(ann, bar, today("09:20")),
				in(ann, bar, today("09:40")), out(ann, bar, today("10:30")),
			},
			now:   today("11:00"),
			hours: 12,
			want:  map[tapeVenueRef][]int{bar: hoursOn(12, 9, 10)},
		},
		{
			name: "two people, two venues, a partner venue",
			evs: []tapeEvent{
				in(ann, bar, today("01:30")), out(ann, bar, today("02:30")),
				in(bob, kitchen, today("02:00")), out(bob, kitchen, today("02:05")),
				in(bob, partner, today("02:40")),
			},
			now:   today("03:15"),
			hours: 4,
			want: map[tapeVenueRef][]int{
				bar:     hoursOn(4, 1, 2),
				kitchen: hoursOn(4, 2),
				partner: hoursOn(4, 2, 3),
			},
		},
		{
			name:  "no future hour: at 05:00 sharp the tape is six hours long",
			evs:   []tapeEvent{in(ann, bar, today("04:59"))},
			now:   today("05:00"),
			hours: 6,
			want:  map[tapeVenueRef][]int{bar: hoursOn(6, 4)},
		},
		{
			name:  "a zero-length interval is nobody's hour",
			evs:   []tapeEvent{in(ann, bar, today("03:10")), out(ann, bar, today("03:10"))},
			now:   today("04:00"),
			hours: 5,
			want:  map[tapeVenueRef][]int{},
		},
		{
			name:     "the October DST day has 25 hours",
			evs:      []tapeEvent{in(ann, bar, at("2026-10-25", "22:00"))},
			midnight: at("2026-10-25", "00:00"),
			now:      at("2026-10-25", "23:30"),
			hours:    25,
			want:     map[tapeVenueRef][]int{bar: hoursOn(25, 23, 24)},
		},
		{
			name:     "the March DST day has 23 hours",
			evs:      []tapeEvent{},
			midnight: at("2026-03-29", "00:00"),
			now:      at("2026-03-29", "23:30"),
			hours:    23,
			want:     map[tapeVenueRef][]int{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			midnight := tt.midnight
			if midnight.IsZero() {
				midnight = today("00:00")
			}
			hours, venues := tapeOf(tt.evs, midnight, tt.now)
			if len(hours) != tt.hours {
				t.Fatalf("%d hours, want %d", len(hours), tt.hours)
			}
			if !hours[0].Equal(midnight) || hours[len(hours)-1].After(tt.now) {
				t.Errorf("the hours run %v .. %v; want local midnight .. the hour holding now (%v)", hours[0], hours[len(hours)-1], tt.now)
			}
			if len(venues) != len(tt.want) {
				t.Fatalf("venues %+v, want %d rows", venues, len(tt.want))
			}
			for _, v := range venues {
				ref := tapeVenueRef{id: v.LocationID, name: v.Name, partner: v.Partner, unplaced: v.Unplaced}
				want, ok := tt.want[ref]
				if !ok {
					t.Errorf("unexpected venue row %+v", v)
					continue
				}
				if len(v.People) != len(want) {
					t.Fatalf("%s: %d hours, want %d", v.Name, len(v.People), len(want))
				}
				for i := range want {
					if v.People[i] != want[i] {
						t.Errorf("%s: %v, want %v", v.Name, v.People, want)
						break
					}
				}
			}
		})
	}
}

// TestTapeOf_OrdersOwnVenuesByNameThenPartnersThenTheUnplaced pins the row order the screen draws.
func TestTapeOf_OrdersOwnVenuesByNameThenPartnersThenTheUnplaced(t *testing.T) {
	midnight := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	now := midnight.Add(90 * time.Minute)
	refs := []tapeVenueRef{
		{unplaced: true},
		{id: uuid.New(), name: "A partner", partner: true},
		{id: uuid.New(), name: "Zebra"},
		{id: uuid.New(), name: "Alpha"},
	}
	var evs []tapeEvent
	for _, r := range refs {
		evs = append(evs, tapeEvent{who: uuid.New(), at: midnight, in: true, venue: r})
	}
	_, venues := tapeOf(evs, midnight, now)
	var got []string
	for _, v := range venues {
		switch {
		case v.Unplaced:
			got = append(got, "(unplaced)")
		default:
			got = append(got, v.Name)
		}
	}
	want := []string{"Alpha", "Zebra", "A partner", "(unplaced)"}
	if len(got) != len(want) {
		t.Fatalf("order %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order %v, want %v", got, want)
		}
	}
}

// TestTapeEvents_ACutReadDropsTheLastPersonWhole: when the read hit its cap the last person's chain
// may be missing its checkout, so that person is left out rather than shown on shift until now.
func TestTapeEvents_ACutReadDropsTheLastPersonWhole(t *testing.T) {
	ann, bob := uuid.New(), uuid.New()
	venue := uuid.New()
	name := "Door"
	in, out := "in", "out"
	at := time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC)
	rows := []store.ListWorkedShiftEventsRow{
		{EmployeeID: &ann, OccurredAt: at, Type: &in, LocationID: &venue, LocationName: &name},
		{EmployeeID: &ann, OccurredAt: at.Add(time.Hour), Type: &out, LocationID: &venue, LocationName: &name},
		{EmployeeID: &bob, OccurredAt: at, Type: &in, LocationID: &venue, LocationName: &name},
	}
	if got := tapeEvents(rows, siteVenues{}, false); len(got) != 3 {
		t.Fatalf("CONTROL: an uncut read keeps %d of 3 events", len(got))
	}
	got := tapeEvents(rows, siteVenues{}, true)
	if len(got) != 2 || got[0].who != ann || got[1].who != ann {
		t.Errorf("a cut read kept %+v, want ann's two events only", got)
	}
}

// TestTapeEvents_TheReadArrivesByPersonThenTime pins the order tapeEvents' cut rule rests on. A cut
// read drops the LAST person whole, which is only "the one person whose chain may be short" if
// ListWorkedShiftEvents hands the rows grouped by person and, within a person, by time -- and
// tapeOf walks each chain in that order. Reordered (by time first, say), the dropped tail would be
// the latest taps of several people and the kept rows would still hold half chains, which is the
// over-count the rule exists to prevent. The body is read comment-free (findQuery), so an ORDER BY
// mentioned in prose cannot satisfy it.
func TestTapeEvents_TheReadArrivesByPersonThenTime(t *testing.T) {
	_, body, ok := findQuery(t, "ListWorkedShiftEvents")
	if !ok {
		t.Fatal("ListWorkedShiftEvents was not found in db/queries; this pin is reading nothing")
	}
	flat := strings.Join(strings.Fields(body), " ")
	orders := regexp.MustCompile(`(?i)\bORDER BY ([^;]*?)(?: LIMIT\b|;|$)`).FindAllStringSubmatch(flat, -1)
	if len(orders) != 1 {
		t.Fatalf("ListWorkedShiftEvents has %d ORDER BY clause(s), want exactly one: %q", len(orders), flat)
	}
	if got, want := strings.TrimSpace(orders[0][1]), "t.employee_id, t.occurred_at, t.id"; got != want {
		t.Errorf("ListWorkedShiftEvents orders by %q, want %q: tapeEvents drops the last PERSON of a cut "+
			"read whole, and tapeOf walks each person's chain by time", got, want)
	}
}
