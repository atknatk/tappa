package tap

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// This file is ADR 0032 §A.3 on the business-day boundary: S(at) (BusinessDayStart)
// and Next(S) (NextBusinessDayStart), proven by an exhaustive sweep over the zones
// whose DST rules broke earlier versions of the algorithm, and by named pins for
// the instants the sweep found.

// sweepZones are the twelve zones ADR 0032 §A.3 names: Malta (the product's), the
// two that spring forward AT MIDNIGHT (Santiago, Havana — the one-candidate rule's
// counterexamples), the half-hour and 45-minute offsets (St Johns, Tehran, Chatham),
// the 30-minute DST step (Lord Howe), the 2-hour step (Troll), the zones whose DST
// moves at odd hours or by decree (Beirut, Asuncion, Scoresbysund, Casablanca).
var sweepZones = []string{
	"Europe/Malta", "Asia/Beirut", "America/Asuncion", "Australia/Lord_Howe",
	"Pacific/Chatham", "America/Scoresbysund", "Antarctica/Troll", "Africa/Casablanca",
	"America/St_Johns", "Asia/Tehran", "America/Santiago", "America/Havana",
}

// TestDecide_BusinessDayBoundaryHoldsInEveryZoneEveryCutoffEveryFiveMinutes is the
// property test: for each of the twelve zones, every cutoff on a 15-minute grid, and
// `at` on a 5-minute grid across 2025-01-01 .. 2027-12-31 inclusive (UTC):
//
//	S(at) ≤ at < Next(S(at))      the tap is inside its own business day
//	S is monotone in at           a later tap never has an earlier day
//	S(S) = S                      the start is a fixed point (the start instant
//	                              belongs to the day it starts)
//	22 h ≤ Next(S) − S ≤ 26 h     no day is lost or doubled across DST
//	S's wall clock is the cutoff  unless the cutoff falls in a spring gap — no
//	                              instant within 3 h of S reads it
//	no earlier instant has S's    a repeated (autumn) wall clock starts the day
//	wall clock                    at its EARLIER instant
//
// The last two are what the first four cannot see: a cutoff shifted by a minute, or
// the later of two autumn instants chosen consistently, still makes a self-consistent
// chain of days. They are checked by brute force — every instant within 3 h of S on
// the 15-minute UTC grid (every offset of these zones, and every cutoff swept, is a
// multiple of 15 minutes, and no DST step exceeds 2 h) — not by the code under test.
//
// 0 violations is the claim; on failure the count per property is reported with the
// first counterexample.
//
// WHICH DAYS. With TAPPA_TEST_FULL_DST_SWEEP=1 every day of the three years is
// swept — the full grid of ADR 0032 §A.3 (3.6 × 10^8 points; measured 2026-10-21:
// ~20 s on 16 cores without the race detector, 0 violations; run it when this file
// or BusinessDayStart changes, and at a phase close). By default the sweep covers
// every UTC day that starts less than four days from one of the zone's UTC-offset
// changes in the span — where every counterexample ever measured lives (Santiago's
// and Havana's midnight gap, Malta's repeated hour) and where the candidates of
// d − 1 .. d + 1 can see a change — plus the first day of every month as the
// ordinary-day control (~2 s). Every other day has one offset over all of its
// candidates: the zone behaves as a fixed offset there.
//
// WHY Next AND S(S) ARE CHECKED ONCE PER DAY, NOT PER POINT. Both depend on S alone;
// they run whenever S changes, so every distinct S the sweep produces is checked.
// The per-point check (S ≤ at < Next) is made at every point.
func TestDecide_BusinessDayBoundaryHoldsInEveryZoneEveryCutoffEveryFiveMinutes(t *testing.T) {
	t.Parallel()
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC)
	full := os.Getenv("TAPPA_TEST_FULL_DST_SWEEP") == "1"
	for _, zone := range sweepZones {
		zone := zone
		t.Run(zone, func(t *testing.T) {
			t.Parallel()
			loc, err := time.LoadLocation(zone)
			if err != nil {
				t.Fatalf("load %s: %v", zone, err)
			}
			days := sweepDays(loc, from, to, full)
			violations := map[string]int{}
			first := map[string]string{}
			points := 0
			for cutMin := 0; cutMin < 24*60; cutMin += 15 {
				cut := time.Duration(cutMin) * time.Minute
				fail := func(name string, at, s, next time.Time) {
					violations[name]++
					if _, seen := first[name]; !seen {
						first[name] = fmt.Sprintf("cutoff %v, at %s: S %s, Next %s", cut,
							at.In(loc).Format("2006-01-02 15:04 -07"), s.In(loc).Format("2006-01-02 15:04 -07"),
							next.In(loc).Format("2006-01-02 15:04 -07"))
					}
				}
				var prevS, next time.Time
				for _, day := range days {
					for at := day; at.Before(day.Add(24 * time.Hour)); at = at.Add(5 * time.Minute) {
						points++
						s, ok := businessDayStartIn(at, cut, loc)
						if !ok {
							fail("no S", at, s, next)
							continue
						}
						if !s.Equal(prevS) {
							if !prevS.IsZero() && s.Before(prevS) {
								fail("S not monotone", at, s, next)
							}
							if ss, _ := businessDayStartIn(s, cut, loc); !ss.Equal(s) {
								fail("S(S) != S", at, s, next)
							}
							if !wallClockIsTheCutoff(s, cut, loc) {
								fail("S's wall clock is not the cutoff (and no gap)", at, s, next)
							}
							if earlierInstantWithTheSameWallClock(s, loc) {
								fail("an earlier instant has S's wall clock", at, s, next)
							}
							next, _ = nextBusinessDayStartIn(s, cut, loc)
							if l := next.Sub(s); l < 22*time.Hour || l > 26*time.Hour {
								fail("day length outside 22-26 h", at, s, next)
							}
							prevS = s
						}
						if s.After(at) {
							fail("S > at", at, s, next)
						}
						if !at.Before(next) {
							fail("at >= Next", at, s, next)
						}
					}
				}
			}
			for name, n := range violations {
				t.Errorf("%s: %d violations of %q; first: %s", zone, n, name, first[name])
			}
			if points == 0 {
				t.Fatal("the sweep checked nothing")
			}
			// The default sweep is dense only around offset changes; a zone with DST in
			// the span must have had its change days found, or the default would be the
			// monthly control days alone and prove nothing about DST.
			if hasNoDST := zone == "Asia/Tehran" || zone == "America/Asuncion"; !hasNoDST && len(days) <= 36 {
				t.Fatalf("%s: only %d days swept — no offset change found in 2025-2027", zone, len(days))
			}
			t.Logf("%s: %d days, %d points checked", zone, len(days), points)
		})
	}
}

// wallClockIsTheCutoff reports whether s reads cut on loc's wall clock, or — the one
// allowed exception — cut is a wall clock that no instant within 3 h of s reads (a
// spring gap: s is where time.Date normalised the missing wall clock to).
func wallClockIsTheCutoff(s time.Time, cut time.Duration, loc *time.Location) bool {
	if localTimeOfDay(s.In(loc)) == cut {
		return true
	}
	// The grid is the UTC 15-minute grid, NOT one relative to s: a wrong s (a cutoff
	// shifted by a minute) would otherwise put every probe off the grid the cutoff
	// lives on and the scan would find nothing.
	for u := s.Add(-3 * time.Hour).Truncate(15 * time.Minute); !u.After(s.Add(3 * time.Hour)); u = u.Add(15 * time.Minute) {
		if localTimeOfDay(u.In(loc)) == cut {
			return false
		}
	}
	return true
}

// earlierInstantWithTheSameWallClock reports whether an instant up to 3 h before s
// reads the same local date and time as s does.
func earlierInstantWithTheSameWallClock(s time.Time, loc *time.Location) bool {
	local := s.In(loc)
	y, m, d := local.Date()
	for u := s.Add(-15 * time.Minute); !u.Before(s.Add(-3 * time.Hour)); u = u.Add(-15 * time.Minute) {
		l := u.In(loc)
		if ly, lm, ld := l.Date(); ly == y && lm == m && ld == d && localTimeOfDay(l) == localTimeOfDay(local) {
			return true
		}
	}
	return false
}

// sweepDays is the UTC days the sweep covers: all of them when full, otherwise the
// days starting less than four days from one of loc's offset changes in [from, to)
// and the first day of every month. Each zone in sweepZones except Tehran (no DST since 2022) and
// Asuncion (permanent −03 since 2024) has offset changes in the span; the test pins
// that the default is not silently the control days alone.
func sweepDays(loc *time.Location, from, to time.Time, full bool) []time.Time {
	var changes []time.Time
	for t := from; ; {
		_, end := t.In(loc).ZoneBounds()
		if end.IsZero() || !end.Before(to) {
			break
		}
		changes = append(changes, end)
		t = end
	}
	var days []time.Time
	for day := from; day.Before(to); day = day.Add(24 * time.Hour) {
		keep := full || day.Day() == 1
		for _, c := range changes {
			if d := c.Sub(day); d > -4*24*time.Hour && d < 4*24*time.Hour {
				keep = true
				break
			}
		}
		if keep {
			days = append(days, day)
		}
	}
	return days
}

// TestDecide_BusinessDayPinsTheMeasuredInstants names the instants the sweep's
// properties rest on, so a change in Go's wall-clock normalisation, or in the
// algorithm, fails with the instant rather than a count:
//
//   - Santiago and Havana, cutoff 00:00, at 23:00 the evening before a midnight DST
//     start (ADR 0032 §A.3's counterexample): the missing 00:00 is normalised back
//     to 23:00 −04/−05, which IS `at`, so S = at — d + 1's candidate. The
//     one-candidate rule answered 00:00 that morning and a Next equal to `at`.
//   - Malta's autumn fall-back (2026-10-25, 03:00 CEST → 02:00 CET): 05:00 is one
//     instant and the day before is 25 h; 02:30 and 02:00 happen twice and S is the
//     EARLIER instant; Go's own time.Date picks the later (pinned here, so the reason
//     for the explicit code is held, not remembered).
//   - Lord Howe's autumn step of 30 minutes (2025-04-06): 01:45 happens twice, S is
//     the earlier instant, Go picks the later.
//   - Malta's spring-forward (2027-03-28, 02:00 CET → 03:00 CEST): 05:00's day before
//     is 23 h; 02:30 does not exist and lands where time.Date normalises it (01:30Z).
func TestDecide_BusinessDayPinsTheMeasuredInstants(t *testing.T) {
	t.Parallel()
	utc := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		return v
	}
	tests := []struct {
		name     string
		zone     string
		cutoff   time.Duration
		at       string
		wantS    string
		wantNext string
	}{
		{"santiago_midnight_gap_d_plus_one", "America/Santiago", 0, "2025-09-07T03:00:00Z", "2025-09-07T03:00:00Z", "2025-09-08T03:00:00Z"},
		{"santiago_one_minute_before", "America/Santiago", 0, "2025-09-07T02:59:00Z", "2025-09-06T04:00:00Z", "2025-09-07T03:00:00Z"},
		{"havana_midnight_gap_d_plus_one", "America/Havana", 0, "2025-03-09T04:00:00Z", "2025-03-09T04:00:00Z", "2025-03-10T04:00:00Z"},
		{"malta_autumn_0500_long_day_before", "Europe/Malta", 5 * time.Hour, "2026-10-25T03:59:00Z", "2026-10-24T03:00:00Z", "2026-10-25T04:00:00Z"},
		{"malta_autumn_0500", "Europe/Malta", 5 * time.Hour, "2026-10-25T10:00:00Z", "2026-10-25T04:00:00Z", "2026-10-26T04:00:00Z"},
		{"malta_autumn_0230_earlier_instant", "Europe/Malta", 2*time.Hour + 30*time.Minute, "2026-10-25T00:45:00Z", "2026-10-25T00:30:00Z", "2026-10-26T01:30:00Z"},
		{"malta_autumn_0230_just_before", "Europe/Malta", 2*time.Hour + 30*time.Minute, "2026-10-25T00:29:00Z", "2026-10-24T00:30:00Z", "2026-10-25T00:30:00Z"},
		{"malta_autumn_0200_earlier_instant", "Europe/Malta", 2 * time.Hour, "2026-10-25T00:30:00Z", "2026-10-25T00:00:00Z", "2026-10-26T01:00:00Z"},
		// Lord Howe's autumn step is 30 minutes (2025-04-06 02:00 +11 -> 01:30 +10:30):
		// 01:45 happens at 14:45Z (+11) and 15:15Z (+10:30); S is the earlier, and that
		// business day is 24.5 h long.
		{"lord_howe_autumn_0145_earlier_instant", "Australia/Lord_Howe", time.Hour + 45*time.Minute, "2025-04-05T15:00:00Z", "2025-04-05T14:45:00Z", "2025-04-06T15:15:00Z"},
		{"lord_howe_autumn_0145_just_before", "Australia/Lord_Howe", time.Hour + 45*time.Minute, "2025-04-05T14:44:00Z", "2025-04-04T14:45:00Z", "2025-04-05T14:45:00Z"},
		{"malta_spring_0500_short_day_before", "Europe/Malta", 5 * time.Hour, "2027-03-28T02:59:00Z", "2027-03-27T04:00:00Z", "2027-03-28T03:00:00Z"},
		{"malta_spring_0500", "Europe/Malta", 5 * time.Hour, "2027-03-28T10:00:00Z", "2027-03-28T03:00:00Z", "2027-03-29T03:00:00Z"},
		{"malta_spring_0230_missing", "Europe/Malta", 2*time.Hour + 30*time.Minute, "2027-03-28T01:30:00Z", "2027-03-28T01:30:00Z", "2027-03-29T00:30:00Z"},
		{"malta_spring_0230_just_before", "Europe/Malta", 2*time.Hour + 30*time.Minute, "2027-03-28T01:29:00Z", "2027-03-27T01:30:00Z", "2027-03-28T01:30:00Z"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := DayCutoff{Cutoff: tc.cutoff, Timezone: tc.zone}
			s, ok := BusinessDayStart(utc(tc.at), c)
			if !ok || !s.Equal(utc(tc.wantS)) {
				t.Fatalf("S(%s) = %s, %v; want %s", tc.at, s.Format(time.RFC3339), ok, tc.wantS)
			}
			if s.Location() != time.UTC {
				t.Errorf("S is in %v, want UTC (§6)", s.Location())
			}
			next, ok := NextBusinessDayStart(s, c)
			if !ok || !next.Equal(utc(tc.wantNext)) {
				t.Fatalf("Next(%s) = %s, %v; want %s", tc.wantS, next.Format(time.RFC3339), ok, tc.wantNext)
			}
		})
	}

	// Go's own choice for a repeated wall clock: the LATER instant. If this changes,
	// wallClockOn's explicit search is still right, but the comment that justifies it
	// is not.
	malta, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	if got := time.Date(2026, 10, 25, 2, 30, 0, 0, malta).UTC(); !got.Equal(utc("2026-10-25T01:30:00Z")) {
		t.Errorf("time.Date picks %s for Malta's repeated 02:30; this file says Go picks the later instant (01:30Z)", got)
	}
	lordHowe, err := time.LoadLocation("Australia/Lord_Howe")
	if err != nil {
		t.Fatal(err)
	}
	if got := time.Date(2025, 4, 6, 1, 45, 0, 0, lordHowe).UTC(); !got.Equal(utc("2025-04-05T15:15:00Z")) {
		t.Errorf("time.Date picks %s for Lord Howe's repeated 01:45; this file says Go picks the later instant (15:15Z)", got)
	}
}

// TestDecide_BusinessDayUnresolvableCutoffIsNoBusinessDay: an empty or unknown zone
// and a cutoff outside [0, 24h) give no S — never UTC, never a guessed 05:00.
func TestDecide_BusinessDayUnresolvableCutoffIsNoBusinessDay(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 7, 27, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		c    DayCutoff
	}{
		{"empty_zone", DayCutoff{Cutoff: 5 * time.Hour}},
		{"unknown_zone", DayCutoff{Cutoff: 5 * time.Hour, Timezone: "Mars/Olympus_Mons"}},
		{"cutoff_24h", DayCutoff{Cutoff: 24 * time.Hour, Timezone: "Europe/Malta"}},
		{"cutoff_negative", DayCutoff{Cutoff: -time.Minute, Timezone: "Europe/Malta"}},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if s, ok := BusinessDayStart(at, tc.c); ok {
				t.Errorf("BusinessDayStart = %s, ok; want not ok", s)
			}
			if s, ok := NextBusinessDayStart(at, tc.c); ok {
				t.Errorf("NextBusinessDayStart = %s, ok; want not ok", s)
			}
		})
	}
	// The edges of the valid range resolve.
	for _, cut := range []time.Duration{0, 24*time.Hour - time.Minute} {
		if _, ok := BusinessDayStart(at, DayCutoff{Cutoff: cut, Timezone: "Europe/Malta"}); !ok {
			t.Errorf("cutoff %v did not resolve", cut)
		}
	}
}
