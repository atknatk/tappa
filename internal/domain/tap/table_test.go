package tap

import (
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/geo"
	"github.com/google/uuid"
)

// This file is the M4-07 deliverable: table-driven proof of every CLAUDE.md §5 row
// and every M4-07 mandatory edge case. It CLOSES the gaps the earlier M4 test files
// deliberately left and serves as the single COVERAGE LEDGER for §5. Every Now here
// is a FIXED UTC instant (the package never calls time.Now()), so nothing breaks at
// midnight (M4-07 trap). Fixtures (baseInput/onSiteInput/withShift/wantLate/
// staleOpenInNote) are reused from decide_test.go and lateness_test.go — NOT
// re-declared — so this file adds coverage without duplicating it.
//
// DUPLICATION LEDGER — §5's seven rows and the five M4-07 mandatory cases, each with
// the ONE test that proves it. A case already proven in another file is
// CROSS-REFERENCED here, not re-tested (the M4-07 card's "no silent expansion" /
// "document what already exists" rule). The two cases that were genuinely MISSING
// are marked [NEW]; two more that were only PARTIALLY proven are marked [+].
//
//	§5 table (delegation mapping proven in TestDecide_Section5Rows, decide_test.go):
//	  row 1  lost/retired -> reject ............. TestDecide_Section5Rows/row1_* (+ lost sets Security)
//	  row 2  SUN invalid  -> reject ............. TestDecide_Section5Rows/row2_sun_invalid_rejects
//	  row 3  no session   -> redirect, NO record  TestDecide_Section5Rows/row3_* + TestDecide_NoSessionWritesNoRecord
//	  row 4  deactivated  -> reject + Security ... TestDecide_Section5Rows/row4_* (Security asserted)
//	  row 5  person debounce -> ignored ......... TestDecide_Section5Rows/row5_* + TestDecide_PersonDebounceIsPerPerson
//	  row 6  IP or GPS    -> ok ................. TestDecide_Section5Rows/row6_ip_match_ok, row6_gps_only_ok
//	  row 7  neither      -> flag (never reject)  TestDecide_Section5Rows/row7_* + TestDecide_Row7NeverRejects
//
//	M4-07 mandatory extras:
//	  (a) different people, one plaque, 10s apart -> ALL ok . TestDecide_PersonDebounceDifferentPeopleSamePlaque   [NEW]
//	  (b) same person within 20s -> ignored ............... TestDecide_PersonDebounceDifferentPeopleSamePlaque   [+] (also §5 row5, 20s)
//	  (c) mobile data (no IP, GPS ok) -> ok / trust 50 /
//	      note "verified via GPS" ......................... TestDecide_MobileDataGPSOnly                          [+] (verdict/trust also in trust_practice_test; the NOTE assertion is NEW)
//	  (d) Rusty Bar night-shift full round (18:05->02:10) .. TestDecide_RustyBarNightShiftFullRound               [+] (direction in decide_test, lateness in lateness_test; this consolidates the ROUND)
//	  (e) deactivated -> reject + Security=true ............ TestDecide_Section5Rows/row4_deactivated_rejects_with_alert  (already exact; not re-tested)

// --- (a)+(b) Per-PERSON debounce, one shared plaque -------------------------------

// TestDecide_PersonDebounceDifferentPeopleSamePlaque is the load-bearing M4-07 case:
// debounce is PER PERSON, not per tag/plaque (§5 line 5). A queue of DIFFERENT people
// tapping the SAME plaque only 10 s apart must ALL record (ok) — a tag-based debounce
// would swallow everyone after the first and silently dock the second person's shift.
//
// Decide has NO per-employee state: it computes the debounce fact ONLY from
// Input.LastForPerson (the caller scopes that query per person), so "different people"
// is expressed as LastForPerson == nil for each newcomer while the tag/location is
// held identical. The proof is NON-VACUOUS because the SAME table includes the
// contrast rows: when it IS the same person tapping again within the window (their own
// LastForPerson set, 10 s and 20 s), the tap IS ignored — so "all ok above" means the
// debounce is person-scoped, not that debounce is dead.
func TestDecide_PersonDebounceDifferentPeopleSamePlaque(t *testing.T) {
	t.Parallel()

	// ONE shared plaque: the same tag/location and the same on-site network for every
	// tap in the queue (onSiteInput -> IP match, so a recorded tap is ok). Only the
	// PERSON (their own LastForPerson) varies. The tapped location is captured once and
	// asserted identical for every row, so the outcome cannot turn on the tag.
	plaque := onSiteInput()
	plaqueLoc := plaque.Tag.Location
	start := time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC)

	// The guardrail default window is 60 s; every tap below is well inside it, so a
	// (buggy) tag-based debounce would ignore all but the first.
	const step = 10 * time.Second

	tests := []struct {
		name       string
		offset     time.Duration // when this tap hits the shared plaque (queue order)
		personGap  time.Duration // THIS person's own previous tap; 0 = a different person, no prior tap
		wantVerd   Verdict
		wantReason string
	}{
		// Three DIFFERENT people, each a newcomer (no prior tap of their own), tapping
		// the one plaque 10 s apart -> every one is recorded (ok), none debounced.
		{"person_A_first", 0, 0, VerdictOK, "different person, no prior tap -> not debounced"},
		{"person_B_first_10s_later", step, 0, VerdictOK, "different person, no prior tap -> not debounced"},
		{"person_C_first_20s_later", 2 * step, 0, VerdictOK, "different person, no prior tap -> not debounced"},
		// Non-vacuous contrast on the SAME plaque and window: the same person tapping
		// again within the window IS ignored. 10 s and 20 s (the card's exact "same
		// person within 20s -> ignored" case, §5 row 5) both debounce.
		{"same_person_repeat_10s", 3 * step, 10 * time.Second, VerdictIgnored, "same person, 10s later -> ignored"},
		{"same_person_repeat_20s", 4 * step, 20 * time.Second, VerdictIgnored, "same person, 20s later -> ignored"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := plaque // struct copy; shares the Tag/Employee pointers (the plaque is fixed)
			in.Now = start.Add(tc.offset)
			if tc.personGap > 0 {
				// This person has their own recent tap -> within the window -> ignored.
				in.LastForPerson = &Transaction{OccurredAt: in.Now.Add(-tc.personGap), Direction: TypeIn}
			}
			// else: a different person, no prior tap of their own -> LastForPerson stays nil.

			got := Decide(in)

			if in.Tag.Location != plaqueLoc {
				t.Fatalf("test bug: the plaque (tapped location) must be identical for every row")
			}
			if got.Verdict != tc.wantVerd {
				t.Fatalf("%s: Verdict = %q via %q, want %q — debounce is PER PERSON, not per plaque (%s)",
					tc.name, got.Verdict, got.MatchedSid, tc.wantVerd, tc.wantReason)
			}
			if tc.wantVerd == VerdictIgnored && got.MatchedSid != "sys:person-debounce" {
				t.Errorf("%s: MatchedSid = %q, want sys:person-debounce", tc.name, got.MatchedSid)
			}
		})
	}
}

// --- (c) Mobile data: GPS-only ----------------------------------------------------

// TestDecide_MobileDataGPSOnly is the mobile-data case (§5 row 6, GPS branch): the
// phone is OFF the venue network so the source IP matches no registered range, but the
// at-tap GPS is inside the radius. The tap is ok, trust is 50 (20 base + 30 GPS, no
// IP), and — the assertion no earlier file makes — the docket Note explains the
// fallback: it contains "verified via GPS" (from base:gps-only-allow). GPSMatch is
// true and IPMatch is false, so the score cannot be coming from an accidental IP hit.
func TestDecide_MobileDataGPSOnly(t *testing.T) {
	t.Parallel()

	here := geo.Point{Lat: 35.8989, Lng: 14.5146} // Malta; device at the location coordinate
	in := baseInput()
	// A registered venue range the mobile source is NOT on (a CGNAT carrier address).
	in.SourceIP = netip.MustParseAddr("100.64.12.34")
	in.LocationIPs = []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}
	in.GPS = &here
	in.LocationGPS = &here

	got := Decide(in)

	if got.Verdict != VerdictOK {
		t.Fatalf("mobile GPS-only must be ok; got %q via %q", got.Verdict, got.MatchedSid)
	}
	if got.IPMatch {
		t.Errorf("IPMatch must be false on mobile data (off the venue network)")
	}
	if !got.GPSMatch {
		t.Errorf("GPSMatch must be true (device within the radius)")
	}
	if got.Trust != 50 {
		t.Errorf("Trust = %d, want 50 (20 base + 30 GPS, no IP)", got.Trust)
	}
	if !strings.Contains(got.Note, "verified via GPS") {
		t.Errorf("Note = %q, want it to contain %q", got.Note, "verified via GPS")
	}
	if got.MatchedSid != "base:gps-only-allow" {
		t.Errorf("MatchedSid = %q, want base:gps-only-allow", got.MatchedSid)
	}
}

// --- (d) Rusty Bar overnight full round -------------------------------------------

// TestDecide_RustyBarNightShiftFullRound is the overnight shift (Rusty Bar 18:00->
// 02:00 Malta, Overnight=true) as a FULL ROUND — an 18:05 check-IN then an 02:10
// check-OUT the NEXT calendar day — tying M4-04 (direction across midnight, no
// calendar-day window) and M4-05 (lateness on the in, none on the out) into one
// end-to-end narrative. All instants are fixed UTC, so it is deterministic (never
// time.Now(), M4-07 trap). Summer offset UTC+2 is applied INDEPENDENTLY of the code.
func TestDecide_RustyBarNightShiftFullRound(t *testing.T) {
	t.Parallel()

	rustyBar := &Shift{Start: 18 * time.Hour, End: 2 * time.Hour, Overnight: true, Timezone: "Europe/Malta"}

	// 18:05 Malta summer (UTC+2) -> 16:05 UTC; shift start 18:00 local = 16:00 UTC ->
	// 5 min late. No open check-in -> this is the IN that opens the round.
	checkIn := withShift(time.Date(2026, 7, 26, 16, 5, 0, 0, time.UTC), rustyBar)
	gotIn := Decide(checkIn)
	if gotIn.Verdict != VerdictOK {
		t.Fatalf("18:05 check-in must be ok; got %q via %q", gotIn.Verdict, gotIn.MatchedSid)
	}
	if gotIn.Type == nil || *gotIn.Type != TypeIn {
		t.Fatalf("18:05 with no open check-in must be IN; Type = %v", gotIn.Type)
	}
	wantLate(t, gotIn.MinutesLate, 5)

	// 02:10 Malta summer the NEXT day (UTC+2) -> 2026-07-27 00:10 UTC. The open 18:05
	// check-in makes this the OUT that closes it ACROSS midnight (no calendar-day
	// window), with NO "stale" note (an ~8h night is not a forgotten checkout) and NO
	// lateness (a checkout is never late). A realistic caller passes both the person's
	// last tap AND the open check-in, so this is also not practice.
	prior := &Transaction{ID: uuid.New(), OccurredAt: checkIn.Now, Direction: TypeIn}
	checkOut := withShift(time.Date(2026, 7, 27, 0, 10, 0, 0, time.UTC), rustyBar)
	checkOut.LastOpenIn = prior
	checkOut.LastForPerson = prior
	gotOut := Decide(checkOut)
	if gotOut.Verdict != VerdictOK {
		t.Fatalf("02:10 check-out must be ok; got %q via %q", gotOut.Verdict, gotOut.MatchedSid)
	}
	if gotOut.Type == nil || *gotOut.Type != TypeOut {
		t.Fatalf("02:10 must close the 18:05 check-in (OUT) across midnight; Type = %v", gotOut.Type)
	}
	if strings.Contains(gotOut.Note, staleOpenInNote) {
		t.Errorf("an ~8h overnight shift is not a forgotten checkout; Note = %q", gotOut.Note)
	}
	if gotOut.MinutesLate != nil {
		t.Errorf("a check-out is never late; MinutesLate = %d, want nil", *gotOut.MinutesLate)
	}
	if gotOut.Practice {
		t.Errorf("a checkout with a prior tap must not be practice")
	}
}

// --- isPracticeTap defense-in-depth (M4-07 hardening) -----------------------------

// TestDecide_CheckoutNotPracticeInconsistentCaller pins the M4-06 hardening carried
// into M4-07 (state.md session note) and the matching one-line change in decide.go
// (isPracticeTap now also requires LastOpenIn == nil). Even an INCONSISTENT caller —
// one that passes LastForPerson == nil (as if no prior tap) yet a non-nil LastOpenIn
// (an open check-in, which IS a prior tap) — must NOT mark the resulting checkout as
// practice. A practice checkout would leave the check-in open and inflate hours (the
// M4-06 exploit). A CONSISTENT M5 query never produces this shape; this is defense in
// depth, mirroring resolveDirection's stale-practice guard so the two agree. Without
// the decide.go change this test FAILS (isPracticeTap would return true), so it is a
// genuine, non-vacuous proof of the production hardening.
func TestDecide_CheckoutNotPracticeInconsistentCaller(t *testing.T) {
	t.Parallel()

	in := onSiteInput()                                                                                     // ok, ActivatedAt set (the practice shape)
	in.LastForPerson = nil                                                                                  // inconsistent: caller claims no prior tap...
	in.LastOpenIn = &Transaction{ID: uuid.New(), OccurredAt: in.Now.Add(-3 * time.Hour), Direction: TypeIn} // ...yet an open check-in exists

	got := Decide(in)

	if got.Type == nil || *got.Type != TypeOut {
		t.Fatalf("an open check-in must yield OUT; Type = %v", got.Type)
	}
	if got.Practice {
		t.Errorf("a checkout must never be practice, even from an inconsistent caller (LastOpenIn set); the hours-inflation exploit must stay closed in depth")
	}
}
