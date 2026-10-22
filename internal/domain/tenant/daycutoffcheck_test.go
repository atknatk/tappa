package tenant

// daycutoffcheck_test.go -- M15-08's business day check over a snapshot built by hand. The same
// check against real Postgres -- a business of the test's own, RLS -- is in
// daycutoffcheck_db_test.go.

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestBusinessDayCheck_EveryLevelIsCheckedAtItsValueOnFile: one business with a level of
// each kind setting a value, each read at that value for the people it reaches (bands are
// (start − 1 h, end + 2 h]):
//
//	business 05:00         Bakery's member (04-12, band (03:00, 14:00]: cut) and Nights, a
//	                       department nobody is in at an unset venue (22-06, (21:00, 08:00]:
//	                       cut -- ownShifts' business chain); empty Rusty Bar (18-02,
//	                       (17:00, 04:00]) is checked and not cut
//	venue Gzira 01:00      its profile person, who tapped at Rusty Bar (cut: the touched
//	                       venue), and Gzira's two empty departments with no value -- Early
//	                       03-11 ((02:00, 13:00]: not cut) and Late 00:30-08:00 ((23:30,
//	                       10:00]: cut) -- the case only the check reaches
//	department Kitchen 14:00  its member (06-14, (05:00, 16:00]: cut)
//	person 10:00           their profile venue Plant's 10-22 ((09:00, 00:00]: cut)
//
// CutShifts counts distinct shifts. The same business with every value moved outside every
// band reports none.
func TestBusinessDayCheck_EveryLevelIsCheckedAtItsValueOnFile(t *testing.T) {
	tenantID := uuid.New()
	plant, gzira, rusty := uuid.New(), uuid.New(), uuid.New()
	bakery, nights, early, late, kitchen := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	baker, gziraPerson, cook, ownPerson := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	shift := func(owner string, s, e time.Duration, overnight bool) *CutoffShift {
		return &CutoffShift{Owner: owner, Start: s, End: e, Overnight: overnight}
	}
	w := cutoffWorld{
		name: "Check Ltd", zone: "Europe/Malta", business: hm(5, 0),
		venues: map[uuid.UUID]cutoffPlace{
			plant: {id: plant, venue: plant, name: "Plant", shift: shift("Plant", hm(10, 0), hm(22, 0), false)},
			gzira: {id: gzira, venue: gzira, name: "Gzira", cutoff: dur(1, 0)},
			rusty: {id: rusty, venue: rusty, name: "Rusty Bar", shift: shift("Rusty Bar", hm(18, 0), hm(2, 0), true)},
		},
		departments: map[uuid.UUID]cutoffPlace{
			bakery:  {id: bakery, venue: plant, name: "Bakery", shift: shift("Bakery", hm(4, 0), hm(12, 0), false)},
			nights:  {id: nights, venue: plant, name: "Nights", shift: shift("Nights", hm(22, 0), hm(6, 0), true)},
			early:   {id: early, venue: gzira, name: "Gzira Early", shift: shift("Gzira Early", hm(3, 0), hm(11, 0), false)},
			late:    {id: late, venue: gzira, name: "Gzira Late", shift: shift("Gzira Late", 30*time.Minute, hm(8, 0), false)},
			kitchen: {id: kitchen, venue: plant, name: "Kitchen", cutoff: dur(14, 0), shift: shift("Kitchen", hm(6, 0), hm(14, 0), false)},
		},
		people: []cutoffPerson{
			{id: baker, branch: &plant, department: &bakery},
			{id: gziraPerson, branch: &gzira},
			{id: cook, branch: &plant, department: &kitchen},
			{id: ownPerson, branch: &plant, own: dur(10, 0)},
		},
	}
	got := w.check(tenantID, map[uuid.UUID][]uuid.UUID{gziraPerson: {rusty}})

	type line struct {
		level  CutoffLevel
		value  time.Duration
		people int
		cut    []string
	}
	owners := func(cs []CutoffConflict) []string {
		var out []string
		for _, c := range cs {
			out = append(out, c.Shift.Owner)
		}
		return out
	}
	want := []line{
		{CutoffBusiness, hm(5, 0), 1, []string{"Bakery", "Nights"}},
		{CutoffBranch, hm(1, 0), 1, []string{"Gzira Late", "Rusty Bar"}},
		{CutoffDepartment, hm(14, 0), 1, []string{"Kitchen"}},
		{CutoffPerson, hm(10, 0), 1, []string{"Plant"}},
	}
	if len(got.Levels) != len(want) {
		t.Fatalf("levels = %+v, want %d", got.Levels, len(want))
	}
	for i, wl := range want {
		g := got.Levels[i]
		if g.Level != wl.level || g.Value != wl.value || g.People != wl.people || !equalStrings(owners(g.Conflicts), wl.cut) {
			t.Errorf("level %d = %s %s people %d cut %v, want %s %s people %d cut %v", i,
				g.Level, ClockText(&g.Value), g.People, owners(g.Conflicts), wl.level, ClockText(&wl.value), wl.people, wl.cut)
		}
		for _, c := range g.Conflicts {
			if c.Cutoff != wl.value {
				t.Errorf("level %s: %s is checked at %s, want the value on file %s", g.Level, c.Shift.Owner, ClockText(&c.Cutoff), ClockText(&wl.value))
			}
		}
		if g.Suggested != nil {
			for _, c := range g.Conflicts {
				if CutoffInBand(*g.Suggested, c.Shift) {
					t.Errorf("level %s: the suggestion %s is inside %s's band", g.Level, ClockText(g.Suggested), c.Shift.Owner)
				}
			}
		}
	}
	if got.Levels[0].SubjectID != tenantID || got.Levels[0].SubjectName != "Check Ltd" || got.Levels[1].SubjectName != "Gzira" ||
		got.Levels[3].SubjectID != ownPerson || got.Levels[3].SubjectName != "" {
		t.Errorf("the subjects are %+v", got.Levels)
	}
	if got.CutShifts != 6 || !got.Cuts() || got.Zone != "Europe/Malta" {
		t.Errorf("CutShifts = %d, Cuts = %v, Zone = %q; want 6 distinct shifts, true, Europe/Malta", got.CutShifts, got.Cuts(), got.Zone)
	}

	// Nothing cut: the same business with every value moved outside every band.
	clear := w
	clear.business = hm(15, 0)
	clear.venues = map[uuid.UUID]cutoffPlace{plant: w.venues[plant], rusty: w.venues[rusty],
		gzira: {id: gzira, venue: gzira, name: "Gzira", cutoff: dur(15, 0)}}
	clear.departments = map[uuid.UUID]cutoffPlace{bakery: w.departments[bakery], nights: w.departments[nights],
		early: w.departments[early], late: w.departments[late],
		kitchen: {id: kitchen, venue: plant, name: "Kitchen", cutoff: dur(23, 0), shift: w.departments[kitchen].shift}}
	clear.people = []cutoffPerson{w.people[0], w.people[1], w.people[2]}
	if c := clear.check(tenantID, nil); c.Cuts() || c.CutShifts != 0 {
		for _, l := range c.Levels {
			t.Logf("%s %s cuts %v", l.Level, ClockText(&l.Value), owners(l.Conflicts))
		}
		t.Errorf("a business with every value outside every band reports %d cut shifts", c.CutShifts)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
