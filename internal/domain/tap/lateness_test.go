package tap

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// This file proves M4-05: shift resolution + lateness and the cross-location fact.
//
// Lateness is a REPORT output that NEVER changes the verdict (§5): a late tap is
// still ok. Every case fixes Now (UTC) so the tests are deterministic across
// midnight and DST (the package never calls time.Now() — M4-05/M4-07 trap). The
// DST expectations below are computed INDEPENDENTLY from the Europe/Malta offsets
// (winter CET = UTC+1, summer CEST = UTC+2; 2026 spring-forward 2026-03-29
// 02:00->03:00, fall-back 2026-10-25 03:00->02:00), not read back from the code.

// withShift returns an on-site (IP-matching -> ok, so it carries a direction) tap
// at the given UTC instant with the given shift attached.
func withShift(now time.Time, s *Shift) Input {
	in := onSiteInput()
	in.Now = now
	in.Shift = s
	return in
}

// wantLate asserts MinutesLate is non-nil and equals want.
func wantLate(t *testing.T, got *int, want int) {
	t.Helper()
	if got == nil {
		t.Fatalf("MinutesLate = nil, want %d", want)
	}
	if *got != want {
		t.Fatalf("MinutesLate = %d, want %d", *got, want)
	}
}

// TestDecide_LatenessDepartmentShift proves lateness is measured against the
// employee's OWN shift (Input.Shift) — the caller resolves department > location
// (§5/M4-05); here we prove the SAME tap yields DIFFERENT lateness under the KM
// Pastry (04:00) vs KM General (09:00) shift, so it is evaluated against 04:00,
// NOT 09:00.
func TestDecide_LatenessDepartmentShift(t *testing.T) {
	t.Parallel()
	// 04:15 Malta local, summer (UTC+2) -> 02:15 UTC.
	now := time.Date(2026, 7, 26, 2, 15, 0, 0, time.UTC)

	pastry := &Shift{Start: 4 * time.Hour, End: 12 * time.Hour, Timezone: "Europe/Malta"}
	general := &Shift{Start: 9 * time.Hour, End: 17 * time.Hour, Timezone: "Europe/Malta"}

	// Pastry 04:00 start = 02:00 UTC; tap 02:15 UTC -> 15 min late.
	gotPastry := Decide(withShift(now, pastry))
	if gotPastry.Verdict != VerdictOK {
		t.Fatalf("precondition: want ok, got %q via %q", gotPastry.Verdict, gotPastry.MatchedSid)
	}
	wantLate(t, gotPastry.MinutesLate, 15)

	// General 09:00 start = 07:00 UTC; the SAME 02:15 UTC tap is 285 min EARLY
	// (-285), proving the two shifts produce different results — the tap is judged
	// against its own shift, not the other department's.
	gotGeneral := Decide(withShift(now, general))
	wantLate(t, gotGeneral.MinutesLate, -285)
	if *gotPastry.MinutesLate == *gotGeneral.MinutesLate {
		t.Fatalf("Pastry and General shifts must yield different lateness for the same tap")
	}
}

// TestDecide_LatenessOvernightRustyBar proves the overnight shift (Rusty Bar
// 18:00->02:00, Overnight=true) computes lateness correctly across midnight:
//   - 18:10 check-IN -> 10 min late,
//   - 02:00 check-OUT -> NOT late (nil; a checkout is not "late"),
//   - a 01:00 after-midnight check-IN pairs with the PREVIOUS day's 18:00 start.
func TestDecide_LatenessOvernightRustyBar(t *testing.T) {
	t.Parallel()
	rustyBar := &Shift{Start: 18 * time.Hour, End: 2 * time.Hour, Overnight: true, Timezone: "Europe/Malta"}

	// 18:10 Malta local, summer (UTC+2) -> 16:10 UTC; start 18:00 local = 16:00 UTC.
	in := withShift(time.Date(2026, 7, 26, 16, 10, 0, 0, time.UTC), rustyBar)
	got := Decide(in) // no open check-in -> IN
	if got.Type == nil || *got.Type != TypeIn {
		t.Fatalf("precondition: 18:10 tap must be IN; Type = %v", got.Type)
	}
	wantLate(t, got.MinutesLate, 10)

	// 02:00 Malta local next day, summer (UTC+2) -> 2026-07-27 00:00 UTC. An open
	// check-in makes this an OUT -> lateness NOT computed (nil). Were it computed
	// naively (02:00 - 18:00) it would show a spurious ~480 min "late"; it must not.
	out := withShift(time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC), rustyBar)
	out.LastOpenIn = &Transaction{ID: uuid.New(), OccurredAt: in.Now, Direction: TypeIn}
	gotOut := Decide(out)
	if gotOut.Type == nil || *gotOut.Type != TypeOut {
		t.Fatalf("precondition: 02:00 tap must be OUT; Type = %v", gotOut.Type)
	}
	if gotOut.MinutesLate != nil {
		t.Fatalf("a check-OUT is not late; MinutesLate = %d, want nil", *gotOut.MinutesLate)
	}

	// 01:00 Malta local (after midnight), summer (UTC+2) -> 2026-07-26 23:00 UTC.
	// A fresh check-IN in the after-midnight tail belongs to the PREVIOUS day's
	// 18:00 start (2026-07-26 16:00 UTC) -> 23:00 - 16:00 = 420 min late.
	tail := withShift(time.Date(2026, 7, 26, 23, 0, 0, 0, time.UTC), rustyBar)
	gotTail := Decide(tail)
	if gotTail.Type == nil || *gotTail.Type != TypeIn {
		t.Fatalf("precondition: 01:00 fresh tap must be IN; Type = %v", gotTail.Type)
	}
	wantLate(t, gotTail.MinutesLate, 420)
}

// TestDecide_LatenessDSTSpringForward is the March transition (2026-03-29,
// 02:00->03:00, clocks lose an hour). A 09:00 shift start on that day is CEST
// (UTC+2) = 07:00 UTC; a 09:15 local tap is 07:15 UTC -> 15 min late.
//
// INDEPENDENT CHECK: 09:00 on 2026-03-29 is AFTER the 02:00 spring-forward, so the
// offset is +2, giving 07:00 UTC. The naive "local-midnight + 9h" bug would give
// 2026-03-28 23:00 UTC (midnight is still CET, +1) + 9h = 08:00 UTC, so a 07:15 UTC
// tap would wrongly read as 45 min EARLY. This test therefore catches that bug.
func TestDecide_LatenessDSTSpringForward(t *testing.T) {
	t.Parallel()
	shift := &Shift{Start: 9 * time.Hour, End: 17 * time.Hour, Timezone: "Europe/Malta"}
	now := time.Date(2026, 3, 29, 7, 15, 0, 0, time.UTC) // 09:15 CEST
	got := Decide(withShift(now, shift))
	if got.Verdict != VerdictOK {
		t.Fatalf("precondition: want ok, got %q", got.Verdict)
	}
	wantLate(t, got.MinutesLate, 15)
}

// TestDecide_LatenessDSTFallBack is the October transition (2026-10-25,
// 03:00->02:00, clocks gain an hour). A 09:00 shift start on that day is CET
// (UTC+1) = 08:00 UTC; a 09:20 local tap is 08:20 UTC -> 20 min late.
//
// INDEPENDENT CHECK: 09:00 on 2026-10-25 is AFTER the 03:00 fall-back, so the
// offset is +1, giving 08:00 UTC. The naive "local-midnight + 9h" bug would give
// 2026-10-24 22:00 UTC (midnight is still CEST, +2) + 9h = 07:00 UTC, so a 08:20 UTC
// tap would wrongly read as 80 min late. This test therefore catches that bug.
func TestDecide_LatenessDSTFallBack(t *testing.T) {
	t.Parallel()
	shift := &Shift{Start: 9 * time.Hour, End: 17 * time.Hour, Timezone: "Europe/Malta"}
	now := time.Date(2026, 10, 25, 8, 20, 0, 0, time.UTC) // 09:20 CET
	got := Decide(withShift(now, shift))
	if got.Verdict != VerdictOK {
		t.Fatalf("precondition: want ok, got %q", got.Verdict)
	}
	wantLate(t, got.MinutesLate, 20)
}

// TestDecide_LatenessTimezoneIsNotFixedOffset proves the zone (not a baked offset)
// drives the conversion: the SAME 09:10 local check-in is 10 min late in BOTH
// winter and summer, but the underlying UTC instants differ by an hour (UTC+1 vs
// UTC+2). A fixed-offset implementation would misreport one of the two.
func TestDecide_LatenessTimezoneIsNotFixedOffset(t *testing.T) {
	t.Parallel()
	shift := &Shift{Start: 9 * time.Hour, End: 17 * time.Hour, Timezone: "Europe/Malta"}

	// Winter: 09:10 CET (UTC+1) -> 08:10 UTC; start 08:00 UTC -> 10 late.
	winter := Decide(withShift(time.Date(2026, 1, 15, 8, 10, 0, 0, time.UTC), shift))
	wantLate(t, winter.MinutesLate, 10)

	// Summer: 09:10 CEST (UTC+2) -> 07:10 UTC; start 07:00 UTC -> 10 late.
	summer := Decide(withShift(time.Date(2026, 7, 15, 7, 10, 0, 0, time.UTC), shift))
	wantLate(t, summer.MinutesLate, 10)
}

// TestDecide_LatenessDoesNotAffectVerdict is the load-bearing invariant (§5,
// M4-05): a badly-late tap is STILL ok. A three-hours-late check-in reports
// MinutesLate = 180 but keeps VerdictOK.
func TestDecide_LatenessDoesNotAffectVerdict(t *testing.T) {
	t.Parallel()
	shift := &Shift{Start: 9 * time.Hour, End: 17 * time.Hour, Timezone: "Europe/Malta"}
	// 12:00 Malta local, summer (UTC+2) -> 10:00 UTC; start 09:00 local = 07:00 UTC
	// -> 180 min late.
	got := Decide(withShift(time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC), shift))
	if got.Verdict != VerdictOK {
		t.Fatalf("a late tap must stay ok; Verdict = %q via %q", got.Verdict, got.MatchedSid)
	}
	wantLate(t, got.MinutesLate, 180)
}

// TestDecide_CrossLocationNotPenalised proves Q17: a tap at a location OTHER than
// the employee's home location is still allowed, flagged as CrossLocation for the
// report, and — evaluated against the TAPPED location's shift — is NOT spuriously
// "late". A same-location control confirms the flag is not always set.
func TestDecide_CrossLocationNotPenalised(t *testing.T) {
	t.Parallel()
	// Tapped location's shift starts 11:00; the worker taps exactly on time.
	shift := &Shift{Start: 11 * time.Hour, End: 19 * time.Hour, Timezone: "Europe/Malta"}
	// 11:00 Malta local, summer (UTC+2) -> 09:00 UTC.
	now := time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC)

	cross := withShift(now, shift)
	home := uuid.New() // a DIFFERENT home than the tapped Tag.Location (onSiteInput)
	cross.Employee.Location = &home
	got := Decide(cross)

	if got.Verdict != VerdictOK {
		t.Fatalf("cross-location tap must still be allowed (Q17); Verdict = %q via %q", got.Verdict, got.MatchedSid)
	}
	if !got.CrossLocation {
		t.Fatalf("a tap away from home must set CrossLocation = true")
	}
	// Evaluated against the TAPPED location's 11:00 shift, an on-time tap is not
	// late: no "late stamp" for being away from home.
	if got.MinutesLate == nil || *got.MinutesLate > 0 {
		t.Fatalf("cross-location on-time tap must not be late; MinutesLate = %v", got.MinutesLate)
	}

	// Control: same home == tapped location -> not cross-location.
	same := withShift(now, shift)
	if Decide(same).CrossLocation {
		t.Fatalf("a home-location tap must NOT set CrossLocation")
	}
}

// TestDecide_LatenessNotComputed covers every "not computed" path (nil), so a
// missing shift or an unresolvable zone never crashes and never fabricates a
// lateness (§6 no-guess): no shift, empty timezone, unknown timezone, and a
// check-OUT.
func TestDecide_LatenessNotComputed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		build func() Input
	}{
		{"no shift", func() Input { return withShift(baseInput().Now, nil) }},
		{"empty timezone", func() Input {
			return withShift(baseInput().Now, &Shift{Start: 9 * time.Hour, Timezone: ""})
		}},
		{"unknown timezone", func() Input {
			return withShift(baseInput().Now, &Shift{Start: 9 * time.Hour, Timezone: "Not/AZone"})
		}},
		{"check-out is never late", func() Input {
			in := withShift(baseInput().Now, &Shift{Start: 9 * time.Hour, End: 17 * time.Hour, Timezone: "Europe/Malta"})
			in.LastOpenIn = &Transaction{ID: uuid.New(), OccurredAt: in.Now.Add(-3 * time.Hour), Direction: TypeIn}
			return in
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := Decide(c.build())
			if got.MinutesLate != nil {
				t.Fatalf("%s: MinutesLate = %d, want nil (not computed)", c.name, *got.MinutesLate)
			}
		})
	}
}
