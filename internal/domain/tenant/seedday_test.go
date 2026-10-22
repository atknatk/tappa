package tenant

// seedday_test.go -- the seed's two business days (Q30, test/fixtures/seed.sql) driven through
// the engine itself (M15-08). seedday_db_test.go reads them back from the database.

import (
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/policy"
)

// The seed's business days (test/fixtures/seed.sql, "tenants"; Q30, 2026-10-21).
// TestDayCutoffDB_TheSeedCutoffsAreQ30s reads them back so this pair cannot drift from the seed.
var (
	seedKFCutoff = hm(5, 0)
	seedKMCutoff = hm(23, 0)
)

// seedDayTap is one on-site NFC tap at a KM or KF venue, decided by the engine with the
// business's cutoff (the switch on: production's mode after T2).
func seedDayTap(now time.Time, cutoff time.Duration, open *tap.Transaction) tap.Decision {
	venue := uuid.New()
	return tap.Decide(tap.Input{
		Now:      now,
		Channel:  tap.ChannelNFC,
		SUN:      tap.SUNResult{Valid: true},
		Tag:      &tap.Tag{Status: tap.TagActive, Location: venue},
		Employee: &tap.Employee{Status: tap.EmployeeActive, Location: &venue, ActivatedAt: now.Add(-48 * time.Hour)},
		// The seed's documentation range for KM (198.51.100.0/24): the IP half of row 6.
		SourceIP:    netip.MustParseAddr("198.51.100.9"),
		LocationIPs: []netip.Prefix{netip.MustParsePrefix("198.51.100.8/29")},
		GPSRadiusM:  150,
		Debounce:    60 * time.Second,
		PolicySet: policy.Set{
			Guardrails: policy.DefaultGuardrails(),
			Baseline:   policy.BaselinePolicies(func(name string) uuid.UUID { return uuid.NewSHA1(uuid.Nil, []byte(name)) }),
		},
		DayCutoff:  &tap.DayCutoff{Cutoff: cutoff, Timezone: "Europe/Malta"},
		LastOpenIn: open,
	})
}

// seedDay walks one shift on one Malta calendar day through the engine: the earliest expected
// clock-in (CutoffBandBefore before the start, plus a minute -- the band's lower end is open)
// and the latest expected clock-out (CutoffBandAfter after the end -- its upper end is
// closed). It answers the two directions and whether both landed in one business day.
func seedDay(t *testing.T, day time.Time, s CutoffShift, cutoff time.Duration) (in, out tap.Type, oneDay bool) {
	t.Helper()
	malta, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	// A WALL clock, minutes past that day's midnight: time.Date normalises the minutes on the
	// calendar, so a DST day keeps 03:01 at 03:01 (adding a duration to midnight would not).
	wall := func(base time.Time, d time.Duration) time.Time {
		return time.Date(base.Year(), base.Month(), base.Day(), 0, int(d/time.Minute), 0, 0, malta)
	}
	arrive := wall(day, s.Start-CutoffBandBefore+time.Minute)
	end := s.End
	if s.Overnight || s.End <= s.Start {
		end += 24 * time.Hour
	}
	leave := wall(day, end+CutoffBandAfter)
	first := seedDayTap(arrive.UTC(), cutoff, nil)
	if first.Verdict != tap.VerdictOK || first.Type == nil {
		t.Fatalf("%s %s: the clock-in at %s is %q via %q", s.Owner, day.Format("2006-01-02"), arrive, first.Verdict, first.MatchedSid)
	}
	second := seedDayTap(leave.UTC(), cutoff, &tap.Transaction{ID: uuid.New(), OccurredAt: arrive.UTC(), Direction: *first.Type})
	if second.Verdict != tap.VerdictOK || second.Type == nil {
		t.Fatalf("%s %s: the clock-out at %s is %q via %q", s.Owner, day.Format("2006-01-02"), leave, second.Verdict, second.MatchedSid)
	}
	oneDay = first.BusinessDayStart != nil && second.BusinessDayStart != nil && first.BusinessDayStart.Equal(*second.BusinessDayStart)
	return *first.Type, *second.Type, oneDay
}

// TestSeedBusinessDay_KMsDayKeepsItsDirectionsAtItsSeedCutoff is the card's criterion "KM's day
// -- the bakery's 04:00-12:00 included -- passes with the seed's cutoff in the right
// directions": every KM shift (the plant's and the five departments') on every day of 2026,
// both DST changes included, at KM's seed cutoff 23:00 -- the earliest expected clock-in is
// `in`, the latest expected clock-out closes it (`out`) in the same business day. KF's shifts,
// Rusty Bar's night included, pass the same way at KF's 05:00.
//
// The contrast is measured too, so the test has teeth: at the migration's default 05:00 --
// what KM would have had without Q30 -- the bakery's 03:01 clock-in and its 14:00 clock-out
// fall in two business days and the clock-out is recorded as a clock-in; the same for Meat
// Production.
func TestSeedBusinessDay_KMsDayKeepsItsDirectionsAtItsSeedCutoff(t *testing.T) {
	days := 0
	for day := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC); day.Year() == 2026; day = day.AddDate(0, 0, 1) {
		days++
		for _, c := range []struct {
			shifts []CutoffShift
			cutoff time.Duration
		}{
			{[]CutoffShift{seedKMPlant, seedKMGeneral, seedKMMeat, seedKMWarehouse, seedKMKitchen, seedKMPastry}, seedKMCutoff},
			{[]CutoffShift{seedRustyBar, seedKF1022, seedKFPaceville, seedKFHQ}, seedKFCutoff},
		} {
			for _, s := range c.shifts {
				in, out, oneDay := seedDay(t, day, s, c.cutoff)
				if in != tap.TypeIn || out != tap.TypeOut || !oneDay {
					t.Fatalf("%s on %s at %s: clock-in %q, clock-out %q, one business day %v; want in, out, true",
						s.Owner, day.Format("2006-01-02"), ClockText(&c.cutoff), in, out, oneDay)
				}
			}
		}
	}
	if days != 365 {
		t.Fatalf("walked %d days of 2026", days)
	}
	for _, s := range []CutoffShift{seedKMPastry, seedKMMeat} {
		day := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
		if in, out, oneDay := seedDay(t, day, s, hm(5, 0)); in != tap.TypeIn || out != tap.TypeIn || oneDay {
			t.Errorf("%s at 05:00: clock-in %q, clock-out %q, one business day %v; the contrast wants the clock-out split off as an in",
				s.Owner, in, out, oneDay)
		}
	}
}
