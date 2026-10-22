package tenant

import (
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/domain/tap"
)

func hm(h, m int) time.Duration { return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute }

// The seed's shifts (test/fixtures/seed.sql, "locations" and "departments"), as the card's
// acceptance criterion names them. TestDayCutoffDB_TheSeedShiftsAreTheOnesThePureTestNames
// reads them back from the database so this table cannot drift from the seed.
var (
	seedRustyBar      = CutoffShift{Owner: "Rusty Bar", Start: hm(18, 0), End: hm(2, 0), Overnight: true}
	seedKF1022        = CutoffShift{Owner: "KF St Julians", Start: hm(10, 0), End: hm(22, 0)}
	seedKFPaceville   = CutoffShift{Owner: "KF Paceville", Start: hm(11, 0), End: hm(23, 0)}
	seedKFHQ          = CutoffShift{Owner: "KF Headquarter", Start: hm(9, 0), End: hm(17, 0)}
	seedKMPlant       = CutoffShift{Owner: "KM Plant", Start: hm(9, 0), End: hm(17, 0)}
	seedKMGeneral     = CutoffShift{Owner: "KM General", Start: hm(9, 0), End: hm(17, 0)}
	seedKMMeat        = CutoffShift{Owner: "KM Meat Production", Start: hm(5, 0), End: hm(13, 0)}
	seedKMWarehouse   = CutoffShift{Owner: "KM Warehouse & Supply", Start: hm(8, 0), End: hm(16, 0)}
	seedKMKitchen     = CutoffShift{Owner: "KM Central Kitchen", Start: hm(6, 0), End: hm(14, 0)}
	seedKMPastry      = CutoffShift{Owner: "KM Pastry & Bakery", Start: hm(4, 0), End: hm(12, 0)}
	factoryNightShift = CutoffShift{Owner: "Factory nights", Start: hm(22, 0), End: hm(6, 0), Overnight: true}
)

// TestCutoffInBand_TheSeedShiftsAtTheCardsCutoffs is the card's acceptance table: with the
// cutoff at 05:00, Rusty Bar, KF 10-22, KM Central Kitchen 06-14, KM General and KM Warehouse
// are clear; KM Meat Production, KM Pastry & Bakery and a 22-06 factory night are cut; and a
// 10-22 shift is cut by 00:00 (the band's upper end is included). Around each end the
// neighbouring minute is measured too, so reversing either end of (start − 1 h, end + 2 h] is
// red, and so is comparing an overnight shift's two clocks flat.
func TestCutoffInBand_TheSeedShiftsAtTheCardsCutoffs(t *testing.T) {
	tests := []struct {
		name   string
		shift  CutoffShift
		cutoff time.Duration
		want   bool
	}{
		{"05:00 / Rusty Bar 18-02", seedRustyBar, hm(5, 0), false},
		{"05:00 / KF 10-22", seedKF1022, hm(5, 0), false},
		{"05:00 / KF Paceville 11-23", seedKFPaceville, hm(5, 0), false},
		{"05:00 / KF HQ 09-17", seedKFHQ, hm(5, 0), false},
		{"05:00 / KM Plant 09-17", seedKMPlant, hm(5, 0), false},
		{"05:00 / KM Central Kitchen 06-14 (lower end excluded)", seedKMKitchen, hm(5, 0), false},
		{"05:00 / KM General 09-17", seedKMGeneral, hm(5, 0), false},
		{"05:00 / KM Warehouse 08-16", seedKMWarehouse, hm(5, 0), false},
		{"05:00 / KM Meat Production 05-13", seedKMMeat, hm(5, 0), true},
		{"05:00 / KM Pastry & Bakery 04-12", seedKMPastry, hm(5, 0), true},
		{"05:00 / factory 22-06 overnight", factoryNightShift, hm(5, 0), true},
		{"00:00 / KF 10-22 (upper end included)", seedKF1022, 0, true},
		{"00:01 / KF 10-22 (past the upper end)", seedKF1022, hm(0, 1), false},
		{"05:01 / KM Central Kitchen (inside the lower end)", seedKMKitchen, hm(5, 1), true},
		{"01:00 / Rusty Bar (inside the night, past midnight)", seedRustyBar, hm(1, 0), true},
		{"04:00 / Rusty Bar (end + 2 h, included)", seedRustyBar, hm(4, 0), true},
		{"04:01 / Rusty Bar (past end + 2 h)", seedRustyBar, hm(4, 1), false},
		{"17:00 / Rusty Bar (start − 1 h, excluded)", seedRustyBar, hm(17, 0), false},
		{"23:00 / factory nights (inside, before midnight)", factoryNightShift, hm(23, 0), true},
		{"14:00 / factory nights (outside)", factoryNightShift, hm(14, 0), false},
		{"03:00 / a stored end before its start reads as the next day", CutoffShift{Owner: "x", Start: hm(20, 0), End: hm(4, 0)}, hm(3, 0), true},
		{"12:00 / a band of a whole day holds every cutoff", CutoffShift{Owner: "x", Start: 0, End: hm(21, 0)}, hm(22, 30), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := CutoffInBand(tc.cutoff, tc.shift); got != tc.want {
				t.Fatalf("CutoffInBand(%s, %s %s-%s) = %v, want %v", ClockText(&tc.cutoff), tc.shift.Owner,
					ClockText(&tc.shift.Start), ClockText(&tc.shift.End), got, tc.want)
			}
		})
	}
}

// TestSuggestCutoff_LandsOutsideEveryBand: for each of KM's five departments alone, for the
// five together with the plant, and for KF's venues, the suggestion is a quarter hour inside
// no band; for KF the business default 05:00 is already outside every band. A day with no
// free minute has no suggestion, and the middle of a single gap is its midpoint.
func TestSuggestCutoff_LandsOutsideEveryBand(t *testing.T) {
	km := []CutoffShift{seedKMGeneral, seedKMMeat, seedKMWarehouse, seedKMKitchen, seedKMPastry}
	sets := map[string][]CutoffShift{
		"KM General":                         {seedKMGeneral},
		"KM Meat Production":                 {seedKMMeat},
		"KM Warehouse & Supply":              {seedKMWarehouse},
		"KM Central Kitchen":                 {seedKMKitchen},
		"KM Pastry & Bakery":                 {seedKMPastry},
		"KM, every department and the plant": append(append([]CutoffShift(nil), km...), seedKMPlant),
		"KF, every venue":                    {seedKF1022, seedKFPaceville, seedKFHQ, seedRustyBar},
	}
	for name, shifts := range sets {
		t.Run(name, func(t *testing.T) {
			got, ok := SuggestCutoff(shifts)
			if !ok {
				t.Fatalf("no suggestion for %v", shifts)
			}
			if got%CutoffSuggestionStep != 0 || !ValidDayCutoff(got) {
				t.Fatalf("suggested %s, not a quarter hour of the day", ClockText(&got))
			}
			if c := CutoffConflicts(got, shifts); len(c) != 0 {
				t.Fatalf("suggested %s falls inside %v", ClockText(&got), c)
			}
			t.Logf("%s: suggested %s", name, ClockText(&got))
		})
	}
	t.Run("KF: 05:00 is already outside every band", func(t *testing.T) {
		if c := CutoffConflicts(hm(5, 0), sets["KF, every venue"]); len(c) != 0 {
			t.Fatalf("05:00 cuts %v", c)
		}
	})
	t.Run("a single 09-17 shift: the middle of (19:00, 08:00]", func(t *testing.T) {
		got, ok := SuggestCutoff([]CutoffShift{seedKMGeneral})
		if !ok || got != hm(1, 30) {
			t.Fatalf("got %s %v, want 01:30", ClockText(&got), ok)
		}
	})
	t.Run("no free minute: no suggestion", func(t *testing.T) {
		if got, ok := SuggestCutoff([]CutoffShift{{Owner: "x", Start: 0, End: hm(21, 0)}}); ok {
			t.Fatalf("suggested %s for a day with no free minute", ClockText(&got))
		}
		if got, ok := SuggestCutoff([]CutoffShift{seedKMPastry, {Owner: "y", Start: hm(14, 0), End: hm(1, 0), Overnight: true}}); ok {
			t.Fatalf("suggested %s although the two bands meet end to end", ClockText(&got))
		}
	})
	t.Run("no shift: no suggestion", func(t *testing.T) {
		if _, ok := SuggestCutoff(nil); ok {
			t.Fatal("suggested a cutoff with no shift to keep clear of")
		}
	})
}

// TestSplitsOpenCheckIn_TheCardsCases are the card's open check-in cases in Malta, in a week
// with no clock change (Monday 2 November 2026): 05:00 → 14:00 splits a 12:00 entry; 05:00 →
// 01:00 splits a 22:00 entry; 05:00 → 23:00 splits an 08:00 entry (the new day starts the same
// evening); 05:00 → 03:00 does not split an 08:00 entry (03:00 next morning is past its 18
// hours); and the edge -- Monday 11:00, 07:00 → 05:00: B_new is Tuesday 05:00, exactly 18
// hours later, and splits, which a formula written with < misses.
func TestSplitsOpenCheckIn_TheCardsCases(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	mon := func(h, m int) time.Time { return time.Date(2026, 11, 2, h, m, 0, 0, loc) }
	cut := func(d time.Duration) tap.DayCutoff { return tap.DayCutoff{Cutoff: d, Timezone: "Europe/Malta"} }
	tests := []struct {
		name     string
		in       time.Time
		from, to time.Duration
		want     bool
	}{
		{"12:00 entry, 05:00 -> 14:00", mon(12, 0), hm(5, 0), hm(14, 0), true},
		{"22:00 entry, 05:00 -> 01:00", mon(22, 0), hm(5, 0), hm(1, 0), true},
		{"08:00 entry, 05:00 -> 23:00", mon(8, 0), hm(5, 0), hm(23, 0), true},
		{"08:00 entry, 05:00 -> 03:00", mon(8, 0), hm(5, 0), hm(3, 0), false},
		{"Monday 11:00 entry, 07:00 -> 05:00 (B_new == in + 18 h)", mon(11, 0), hm(7, 0), hm(5, 0), true},
		{"Monday 11:01 entry, 07:00 -> 05:00 (B_new one minute inside 18 h)", mon(11, 1), hm(7, 0), hm(5, 0), true},
		{"Monday 10:59 entry, 07:00 -> 05:00 (B_new past 18 h)", mon(10, 59), hm(7, 0), hm(5, 0), false},
		{"no change", mon(12, 0), hm(5, 0), hm(5, 0), false},
		{"a later cutoff does not end the day earlier: 05:00 -> 06:00, 12:00 entry", mon(12, 0), hm(5, 0), hm(6, 0), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SplitsOpenCheckIn(tc.in, cut(tc.from), cut(tc.to)); got != tc.want {
				t.Fatalf("SplitsOpenCheckIn = %v, want %v", got, tc.want)
			}
		})
	}
	t.Run("an unresolvable zone splits nothing", func(t *testing.T) {
		bad := tap.DayCutoff{Cutoff: hm(14, 0), Timezone: "Not/AZone"}
		if SplitsOpenCheckIn(mon(12, 0), cut(hm(5, 0)), bad) {
			t.Fatal("an unresolvable cutoff split an entry")
		}
	})
}

// TestDayCutoff_TheClosedVocabularies: the four levels and nothing else; a cutoff is a whole
// minute of the day, and 24:00 is not one (00064's CHECK).
func TestDayCutoff_TheClosedVocabularies(t *testing.T) {
	for _, s := range []string{"business", "branch", "department", "person"} {
		if _, ok := ParseCutoffLevel(s); !ok {
			t.Errorf("%q is refused", s)
		}
	}
	for _, s := range []string{"", "tenant", "Business", "location"} {
		if _, ok := ParseCutoffLevel(s); ok {
			t.Errorf("%q is accepted", s)
		}
	}
	for d, want := range map[time.Duration]bool{0: true, hm(23, 59): true, hm(24, 0): false, -time.Minute: false, hm(5, 0) + time.Second: false} {
		if ValidDayCutoff(d) != want {
			t.Errorf("ValidDayCutoff(%v) = %v, want %v", d, !want, want)
		}
	}
	five := hm(5, 0)
	if got := ClockText(&five); got != "05:00" {
		t.Errorf("ClockText = %q", got)
	}
	if got := ClockText(nil); got != "" {
		t.Errorf("ClockText(nil) = %q, want empty (inherit, never 00:00)", got)
	}
}
