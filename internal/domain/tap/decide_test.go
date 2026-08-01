package tap

import (
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/geo"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/google/uuid"
)

// This file proves the M4-03 body of Decide: every CLAUDE.md §5 row is resolved by
// DELEGATING to policy.Evaluate (rows 1-5 guardrails, 6-7 baseline), the effect is
// mapped to the right verdict, the no-session redirect writes NOTHING, row 7 is
// never a reject (§4.6), and the guardrail ORDER is policy's — Decide does not
// short-circuit (R8). The exhaustive table lives in M4-07; this is the mapping +
// the load-bearing invariants.

// testSet builds the production-shaped decision Set: the ten immutable guardrails
// (default 60 s debounce) plus the Tappa-managed baseline, stamped with stable,
// deterministic version ids (a test stands in for M7-03's real per-tenant ids).
func testSet() policy.Set {
	return policy.Set{
		Guardrails: policy.DefaultGuardrails(),
		Baseline:   policy.BaselinePolicies(func(name string) uuid.UUID { return uuid.NewSHA1(uuid.Nil, []byte(name)) }),
	}
}

// baseInput is a fully valid, evidence-less NFC tap: active tag, valid SUN, an
// active session, no debounce, no IP/GPS. On its own it lands on §5 row 7 (flag);
// each test mutates the one field its row is about.
func baseInput() Input {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	loc := uuid.New()
	dept := uuid.New()
	return Input{
		Now:     now,
		Channel: ChannelNFC,
		SUN:     SUNResult{Valid: true, CtrGap: 0},
		Tag:     &Tag{Status: TagActive, Location: loc},
		Employee: &Employee{
			Status:      EmployeeActive,
			Location:    &loc, // home == tapped location: not a cross-location tap
			Department:  &dept,
			ActivatedAt: now.Add(-48 * time.Hour),
		},
		GPSRadiusM: 150,
		Debounce:   60 * time.Second,
		PolicySet:  testSet(),
	}
}

// --- §5 rows 1-7, as one delegation-mapping table --------------------------------

func TestDecide_Section5Rows(t *testing.T) {
	t.Parallel()

	// A location IP range and a matching / far source IP for the row-6 evidence.
	locPrefix := netip.MustParsePrefix("203.0.113.0/24")
	onNet := netip.MustParseAddr("203.0.113.7")
	// GPS: same point (distance 0 < 150 -> match) and a far point (> 150 m).
	here := geo.Point{Lat: 35.8989, Lng: 14.5146}
	far := geo.Point{Lat: 35.9100, Lng: 14.5300} // ~2 km away

	tests := []struct {
		name       string
		mutate     func(in *Input)
		wantVerd   Verdict
		wantSid    string
		wantSecure bool
		wantIP     bool
		wantGPS    bool
		redirect   Redirect
	}{
		{
			name:       "row1_lost_tag_rejects_with_alert",
			mutate:     func(in *Input) { in.Tag.Status = TagLost },
			wantVerd:   VerdictReject,
			wantSid:    "sys:tag-not-active",
			wantSecure: true, // a tag reported lost is in use
		},
		{
			name:     "row1_retired_tag_rejects_no_alert",
			mutate:   func(in *Input) { in.Tag.Status = TagRetired },
			wantVerd: VerdictReject,
			wantSid:  "sys:tag-not-active", // retired is routine lifecycle -> no alert
		},
		{
			name:     "row2_sun_invalid_rejects",
			mutate:   func(in *Input) { in.SUN = SUNResult{Valid: false} },
			wantVerd: VerdictReject,
			wantSid:  "sys:sun-invalid",
		},
		{
			name:     "row3_no_session_redirects",
			mutate:   func(in *Input) { in.Employee = nil },
			wantVerd: "", // NO record: verdict left empty, Redirect carries it (§5.3)
			wantSid:  "sys:no-session",
			redirect: RedirectActivation,
		},
		{
			name:       "row4_deactivated_rejects_with_alert",
			mutate:     func(in *Input) { in.Employee.Status = EmployeeDeactivated },
			wantVerd:   VerdictReject,
			wantSid:    "sys:employee-deactivated",
			wantSecure: true,
		},
		{
			name: "row5_person_debounce_ignored",
			mutate: func(in *Input) {
				in.LastForPerson = &Transaction{OccurredAt: in.Now.Add(-20 * time.Second), Direction: TypeIn}
			},
			wantVerd: VerdictIgnored,
			wantSid:  "sys:person-debounce",
		},
		{
			name: "row6_ip_match_ok",
			mutate: func(in *Input) {
				in.SourceIP = onNet
				in.LocationIPs = []netip.Prefix{locPrefix}
			},
			wantVerd: VerdictOK,
			wantSid:  "base:ip-or-gps-ok",
			wantIP:   true,
		},
		{
			name: "row6_gps_only_ok",
			mutate: func(in *Input) {
				in.GPS = &here
				in.LocationGPS = &here
			},
			wantVerd: VerdictOK,
			wantSid:  "base:gps-only-allow",
			wantGPS:  true,
		},
		{
			name:     "row7_no_evidence_flags",
			mutate:   func(in *Input) {}, // baseInput already carries no IP and no GPS
			wantVerd: VerdictFlag,
			wantSid:  "base:no-evidence-review",
		},
		{
			// Y-E: GPS present but far while IP still matches = on-site proxy. review
			// (base:gps-conflict-review) out-restricts the IP allow at equal specificity.
			name: "gps_conflict_flags_even_with_ip",
			mutate: func(in *Input) {
				in.SourceIP = onNet
				in.LocationIPs = []netip.Prefix{locPrefix}
				in.GPS = &far
				in.LocationGPS = &here
			},
			wantVerd: VerdictFlag,
			wantSid:  "base:gps-conflict-review",
			wantIP:   true,
			wantGPS:  false,
		},
		{
			// Q21: a positive ctr gap sends an otherwise-fine IP tap to review.
			name: "ctr_gap_flags_even_with_ip",
			mutate: func(in *Input) {
				in.SUN = SUNResult{Valid: true, CtrGap: 3}
				in.SourceIP = onNet
				in.LocationIPs = []netip.Prefix{locPrefix}
			},
			wantVerd: VerdictFlag,
			wantSid:  "base:ctr-gap-review",
			wantIP:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := baseInput()
			tc.mutate(&in)

			got := Decide(in)

			if got.Verdict != tc.wantVerd {
				t.Errorf("Verdict = %q, want %q", got.Verdict, tc.wantVerd)
			}
			if got.MatchedSid != tc.wantSid {
				t.Errorf("MatchedSid = %q, want %q", got.MatchedSid, tc.wantSid)
			}
			if got.Security != tc.wantSecure {
				t.Errorf("Security = %v, want %v", got.Security, tc.wantSecure)
			}
			if got.IPMatch != tc.wantIP {
				t.Errorf("IPMatch = %v, want %v", got.IPMatch, tc.wantIP)
			}
			if got.GPSMatch != tc.wantGPS {
				t.Errorf("GPSMatch = %v, want %v", got.GPSMatch, tc.wantGPS)
			}
			if got.Redirect != tc.redirect {
				t.Errorf("Redirect = %q, want %q", got.Redirect, tc.redirect)
			}
			// Every decision explains itself (M3-07): a non-redirect decision carries
			// a verdict AND a deciding sid; both must be set.
			if tc.redirect == RedirectNone && got.MatchedSid == "" {
				t.Errorf("non-redirect decision must carry a MatchedSid")
			}
		})
	}
}

// TestDecide_NoSessionWritesNoRecord is the §4.6/§5-line-3 SOLE exception, called
// out on its own so the "no record" property cannot be lost in the table above: a
// tap with no session redirects and produces NEITHER a verdict NOR a direction.
func TestDecide_NoSessionWritesNoRecord(t *testing.T) {
	t.Parallel()
	in := baseInput()
	in.Employee = nil

	got := Decide(in)

	if got.Redirect != RedirectActivation {
		t.Fatalf("Redirect = %q, want %q (activation page)", got.Redirect, RedirectActivation)
	}
	if got.Verdict != "" {
		t.Errorf("no-session must write NO record: Verdict = %q, want empty", got.Verdict)
	}
	if got.Type != nil {
		t.Errorf("no-session carries no direction: Type = %v, want nil", got.Type)
	}
}

// TestDecide_Row7NeverRejects nails §4.6: when evidence is insufficient the tap is
// FLAGGED (recorded, queued for approval), never REJECTED (dropped). Proven across
// several no-evidence shapes so a future change cannot quietly turn any into reject.
func TestDecide_Row7NeverRejects(t *testing.T) {
	t.Parallel()
	shapes := []struct {
		name   string
		mutate func(in *Input)
	}{
		{"no_ip_no_gps", func(in *Input) {}},
		{"ip_present_but_no_match", func(in *Input) {
			in.SourceIP = netip.MustParseAddr("198.51.100.9")
			in.LocationIPs = []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}
		}},
		{"gps_present_but_far", func(in *Input) {
			gps := geo.Point{Lat: 35.99, Lng: 14.60}
			loc := geo.Point{Lat: 35.8989, Lng: 14.5146}
			in.GPS = &gps
			in.LocationGPS = &loc
		}},
	}
	for _, s := range shapes {
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			in := baseInput()
			s.mutate(&in)
			got := Decide(in)
			if got.Verdict == VerdictReject {
				t.Fatalf("row 7 must NEVER reject (§4.6); got reject via %q", got.MatchedSid)
			}
			if got.Verdict != VerdictFlag {
				t.Errorf("insufficient evidence must FLAG; got %q via %q", got.Verdict, got.MatchedSid)
			}
			if got.Redirect != RedirectNone {
				t.Errorf("a flagged tap is a record, not a redirect: %q", got.Redirect)
			}
		})
	}
}

// TestDecide_DelegatesOrderToPolicy is the R8 proof: Decide must NOT short-circuit
// with its own §5 order before calling policy.Evaluate. If it did (e.g. an early
// "deactivated -> reject + alert" or "debounce -> ignored"), these cases would
// decide differently than the guardrail slice does. Because the ORDER is policy's,
// sys:sun-invalid (slice position 3) PRE-EMPTS both sys:employee-deactivated (7)
// and sys:person-debounce (8): an invalid SUN denies WITHOUT leaking the account
// state (no alert) and WITHOUT the replay being swallowed by debounce (§4.4).
func TestDecide_DelegatesOrderToPolicy(t *testing.T) {
	t.Parallel()

	t.Run("sun_invalid_preempts_deactivated_alert", func(t *testing.T) {
		t.Parallel()
		in := baseInput()
		in.SUN = SUNResult{Valid: false}
		in.Employee.Status = EmployeeDeactivated

		got := Decide(in)

		if got.MatchedSid != "sys:sun-invalid" {
			t.Fatalf("MatchedSid = %q, want sys:sun-invalid (order must be policy's, not Decide's)", got.MatchedSid)
		}
		if got.Verdict != VerdictReject {
			t.Errorf("Verdict = %q, want reject", got.Verdict)
		}
		if got.Security {
			t.Errorf("Security must be false: a forged SUN must not manufacture the deactivated alert (R8 info leak / push flood)")
		}
	})

	t.Run("sun_invalid_preempts_debounce", func(t *testing.T) {
		t.Parallel()
		in := baseInput()
		in.SUN = SUNResult{Valid: false}
		in.LastForPerson = &Transaction{OccurredAt: in.Now.Add(-5 * time.Second), Direction: TypeIn}

		got := Decide(in)

		if got.MatchedSid != "sys:sun-invalid" {
			t.Fatalf("MatchedSid = %q, want sys:sun-invalid (a replay must not be swallowed by debounce, §4.4)", got.MatchedSid)
		}
		if got.Verdict != VerdictReject {
			t.Errorf("Verdict = %q, want reject", got.Verdict)
		}
	})
}

// TestDecide_PersonDebounceIsPerPerson proves the debounce is PERSON-based, not
// tag-based (§5): a first tap by a person with no prior tap is never ignored, and a
// repeat by the SAME person within the window is. (Different people at one plaque
// is an M4-07 multi-person scenario; here we prove the nil-gap safe zero value.)
func TestDecide_PersonDebounceIsPerPerson(t *testing.T) {
	t.Parallel()

	// First tap ever (LastForPerson nil): must NOT be ignored — it proceeds to the
	// evidence rules (row 7 flag here, since baseInput has no IP/GPS).
	first := baseInput()
	if got := Decide(first); got.Verdict == VerdictIgnored {
		t.Fatalf("a first tap (no prior) must never be ignored; got ignored via %q", got.MatchedSid)
	}

	// Same person, 20 s later: within the 60 s window -> ignored.
	repeat := baseInput()
	repeat.LastForPerson = &Transaction{OccurredAt: repeat.Now.Add(-20 * time.Second), Direction: TypeIn}
	if got := Decide(repeat); got.Verdict != VerdictIgnored {
		t.Fatalf("a repeat within the window must be ignored; got %q via %q", got.Verdict, got.MatchedSid)
	}

	// Same person, 90 s later: outside the window -> recorded (not ignored).
	stale := baseInput()
	stale.LastForPerson = &Transaction{OccurredAt: stale.Now.Add(-90 * time.Second), Direction: TypeIn}
	if got := Decide(stale); got.Verdict == VerdictIgnored {
		t.Fatalf("a repeat outside the window must NOT be ignored; got ignored via %q", got.MatchedSid)
	}
}

// TestDecide_QRRequiresIP proves the QR channel wiring (Q15, base:qr-requires-ip):
// a QR tap carries no proof-of-moment, so GPS alone is not enough — it flags — but
// a QR tap with an IP match is ok. (sys:sun-invalid does NOT fire for QR: it is
// NFC-only, so a legitimate SUN-less QR tap is never denied.)
func TestDecide_QRRequiresIP(t *testing.T) {
	t.Parallel()
	here := geo.Point{Lat: 35.8989, Lng: 14.5146}
	locPrefix := netip.MustParsePrefix("203.0.113.0/24")

	// QR + GPS only (no IP) -> flag, NOT ok, and NOT a sun-invalid reject.
	qrGPS := baseInput()
	qrGPS.Channel = ChannelQR
	qrGPS.SUN = SUNResult{Valid: false} // QR has no SUN
	qrGPS.GPS = &here
	qrGPS.LocationGPS = &here
	if got := Decide(qrGPS); got.Verdict != VerdictFlag {
		t.Fatalf("QR without IP (GPS only) must flag; got %q via %q", got.Verdict, got.MatchedSid)
	}

	// QR + IP match -> ok.
	qrIP := baseInput()
	qrIP.Channel = ChannelQR
	qrIP.SUN = SUNResult{Valid: false}
	qrIP.SourceIP = netip.MustParseAddr("203.0.113.7")
	qrIP.LocationIPs = []netip.Prefix{locPrefix}
	if got := Decide(qrIP); got.Verdict != VerdictOK {
		t.Fatalf("QR with an IP match must be ok; got %q via %q", got.Verdict, got.MatchedSid)
	}
}

// TestDecide_CarriesExplainability proves the record can explain itself (M3-07): a
// baseline decision carries a non-nil PolicyVersionID and the baseline layer; a
// guardrail decision carries a nil version id and the guardrail layer.
func TestDecide_CarriesExplainability(t *testing.T) {
	t.Parallel()

	// Baseline decision (row 6 IP allow): version id present, layer = baseline.
	base := baseInput()
	base.SourceIP = netip.MustParseAddr("203.0.113.7")
	base.LocationIPs = []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}
	bd := Decide(base)
	if bd.Layer != policy.LayerBaseline {
		t.Errorf("baseline decision Layer = %q, want %q", bd.Layer, policy.LayerBaseline)
	}
	if bd.PolicyVersionID == nil {
		t.Errorf("baseline decision must carry a PolicyVersionID (M3-07)")
	}

	// Guardrail decision (row 2 sun-invalid): no stored version, layer = guardrail.
	guard := baseInput()
	guard.SUN = SUNResult{Valid: false}
	gd := Decide(guard)
	if gd.Layer != policy.LayerGuardrail {
		t.Errorf("guardrail decision Layer = %q, want %q", gd.Layer, policy.LayerGuardrail)
	}
	if gd.PolicyVersionID != nil {
		t.Errorf("guardrail decision must carry a nil PolicyVersionID (code, not DB); got %v", gd.PolicyVersionID)
	}
}

// TestDecide_ZeroSetFailsToReview proves the fail-safe: with an empty PolicySet (no
// rules at all) a tap:record does not silently pass — it falls to the built-in
// fail-to-review default and is FLAGGED (§4.6, never a silent ok).
func TestDecide_ZeroSetFailsToReview(t *testing.T) {
	t.Parallel()
	in := baseInput()
	in.PolicySet = policy.Set{} // no guardrails, no baseline
	got := Decide(in)
	if got.Verdict != VerdictFlag {
		t.Fatalf("empty PolicySet must fail-to-review (flag); got %q via %q", got.Verdict, got.MatchedSid)
	}
	if got.MatchedSid != "default" {
		t.Errorf("MatchedSid = %q, want default", got.MatchedSid)
	}
}

// --- M4-04: direction (in/out) ---------------------------------------------------
//
// Direction is a pure TOGGLE against the person's last OPEN check-in (Input.LastOpenIn),
// never calendar day (§5, M4-04). These tests fix Now so the overnight-shift cases are
// deterministic (a time.Now() test would break at midnight — M4-07 trap).

// onSiteInput is a baseInput whose source IP is on the location's registered range,
// so the tap lands on §5 row 6 (ok) and therefore CARRIES a direction. Direction
// tests use it to assert Type without the verdict being the variable under test.
func onSiteInput() Input {
	in := baseInput()
	in.SourceIP = netip.MustParseAddr("203.0.113.7")
	in.LocationIPs = []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}
	return in
}

// TestDecide_DirectionInWhenNoOpenCheckIn: no open check-in -> this tap is IN.
func TestDecide_DirectionInWhenNoOpenCheckIn(t *testing.T) {
	t.Parallel()
	in := onSiteInput() // LastOpenIn is nil in baseInput
	got := Decide(in)
	if got.Verdict != VerdictOK {
		t.Fatalf("precondition: want ok, got %q via %q", got.Verdict, got.MatchedSid)
	}
	if got.Type == nil || *got.Type != TypeIn {
		t.Fatalf("no open check-in must yield IN; Type = %v", got.Type)
	}
}

// TestDecide_DirectionOutWhenOpenCheckIn: an open check-in exists -> this tap is OUT.
func TestDecide_DirectionOutWhenOpenCheckIn(t *testing.T) {
	t.Parallel()
	in := onSiteInput()
	in.LastOpenIn = &Transaction{ID: uuid.New(), OccurredAt: in.Now.Add(-3 * time.Hour), Direction: TypeIn}
	got := Decide(in)
	if got.Type == nil || *got.Type != TypeOut {
		t.Fatalf("open check-in must yield OUT; Type = %v", got.Type)
	}
	if strings.Contains(got.Note, staleOpenInNote) {
		t.Errorf("a 3h-old open check-in is not stale; Note = %q", got.Note)
	}
}

// TestDecide_DirectionRustyBarOvernight is the load-bearing overnight case: an 18:05
// check-in is closed by a 02:10 tap the NEXT calendar day. It must pair (-> out) with
// no "stale" note — proving there is no calendar-day window (the classic night-shift
// bug that would refuse to pair across midnight).
func TestDecide_DirectionRustyBarOvernight(t *testing.T) {
	t.Parallel()
	evening := time.Date(2026, 7, 26, 18, 5, 0, 0, time.UTC) // shift start, the open in
	morning := time.Date(2026, 7, 27, 2, 10, 0, 0, time.UTC) // shift end, this tap

	in := onSiteInput()
	in.Now = morning
	in.LastOpenIn = &Transaction{ID: uuid.New(), OccurredAt: evening, Direction: TypeIn}

	got := Decide(in)
	if got.Type == nil || *got.Type != TypeOut {
		t.Fatalf("02:10 tap must close the previous evening's 18:05 check-in (out), across midnight; Type = %v", got.Type)
	}
	if strings.Contains(got.Note, staleOpenInNote) {
		t.Errorf("an ~8h overnight shift is not a forgotten checkout; Note = %q", got.Note)
	}
}

// TestDecide_DirectionMultipleInOutPairsToggle: several in/out pairs in one day
// sequence correctly. Decide is stateless per call, so the test plays the caller's
// role — an IN opens the chain, an OUT closes it — and feeds LastOpenIn accordingly.
func TestDecide_DirectionMultipleInOutPairsToggle(t *testing.T) {
	t.Parallel()
	base := onSiteInput()
	day := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)
	times := []time.Time{
		day.Add(8 * time.Hour),  // in
		day.Add(12 * time.Hour), // out (lunch)
		day.Add(13 * time.Hour), // in
		day.Add(17 * time.Hour), // out
	}
	want := []Type{TypeIn, TypeOut, TypeIn, TypeOut}

	var openIn *Transaction // the caller's running "last open check-in"
	for i, ts := range times {
		in := base
		in.Now = ts
		in.LastOpenIn = openIn
		got := Decide(in)
		if got.Type == nil {
			t.Fatalf("tap %d at %s: Type is nil, want %q", i, ts, want[i])
		}
		if *got.Type != want[i] {
			t.Fatalf("tap %d at %s: Type = %q, want %q", i, ts, *got.Type, want[i])
		}
		if *got.Type == TypeIn {
			openIn = &Transaction{ID: uuid.New(), OccurredAt: ts, Direction: TypeIn}
		} else {
			openIn = nil
		}
	}
}

// TestDecide_DirectionStaleOpenInProducesOutWithNote: an open check-in far older than
// staleOpenInThreshold (forgotten checkout) still resolves to OUT — never silently in
// (§5) — but is annotated so the report shows the anomaly.
func TestDecide_DirectionStaleOpenInProducesOutWithNote(t *testing.T) {
	t.Parallel()
	in := onSiteInput()
	in.LastOpenIn = &Transaction{ID: uuid.New(), OccurredAt: in.Now.Add(-72 * time.Hour), Direction: TypeIn} // 3 days
	got := Decide(in)
	if got.Type == nil || *got.Type != TypeOut {
		t.Fatalf("stale open check-in must still yield OUT, never silently IN; Type = %v", got.Type)
	}
	if !strings.Contains(got.Note, staleOpenInNote) {
		t.Errorf("stale open check-in must be noted for the anomaly report; Note = %q", got.Note)
	}
}

// TestDecide_DirectionStaleThresholdBoundary pins the boundary: just under the
// threshold is not stale, just over it is.
func TestDecide_DirectionStaleThresholdBoundary(t *testing.T) {
	t.Parallel()

	under := onSiteInput()
	under.LastOpenIn = &Transaction{ID: uuid.New(), OccurredAt: under.Now.Add(-(staleOpenInThreshold - time.Minute)), Direction: TypeIn}
	if got := Decide(under); strings.Contains(got.Note, staleOpenInNote) {
		t.Errorf("just under threshold must NOT be stale; Note = %q", got.Note)
	}

	over := onSiteInput()
	over.LastOpenIn = &Transaction{ID: uuid.New(), OccurredAt: over.Now.Add(-(staleOpenInThreshold + time.Minute)), Direction: TypeIn}
	if got := Decide(over); !strings.Contains(got.Note, staleOpenInNote) {
		t.Errorf("just over threshold must be stale; Note = %q", got.Note)
	}
}

// TestDecide_DirectionPracticeOpenInDoesNotCloseChain proves a PRACTICE record never
// holds the chain open (§5, M4-04). Even with an "open in" present, a practice one is
// ignored -> this tap is IN. Defense in depth: the caller's query already excludes
// practice, but a caller bug must not keep a real check-in open behind a training tap
// (the M4-06 hours-inflation exploit).
func TestDecide_DirectionPracticeOpenInDoesNotCloseChain(t *testing.T) {
	t.Parallel()
	in := onSiteInput()
	in.LastOpenIn = &Transaction{ID: uuid.New(), OccurredAt: in.Now.Add(-2 * time.Hour), Direction: TypeIn, Practice: true}
	got := Decide(in)
	if got.Type == nil || *got.Type != TypeIn {
		t.Fatalf("a practice open-in must not close the chain; want IN, Type = %v", got.Type)
	}
}

// TestDecide_DirectionSetForFlaggedRecord: a flagged tap is still a real record that
// joins the chain, so it carries a direction too (not only ok).
func TestDecide_DirectionSetForFlaggedRecord(t *testing.T) {
	t.Parallel()
	in := baseInput() // no IP/GPS -> §5 row 7 flag
	got := Decide(in)
	if got.Verdict != VerdictFlag {
		t.Fatalf("precondition: want flag, got %q via %q", got.Verdict, got.MatchedSid)
	}
	if got.Type == nil || *got.Type != TypeIn {
		t.Fatalf("a flagged record still joins the chain and carries a direction; Type = %v", got.Type)
	}
}

// TestDecide_DirectionNilForNonRecordVerdicts: reject, ignored and the no-session
// redirect carry NO direction — Type stays nil even when an open check-in exists.
func TestDecide_DirectionNilForNonRecordVerdicts(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(in *Input)
	}{
		{"reject_lost_tag", func(in *Input) { in.Tag.Status = TagLost }},
		{"reject_sun_invalid", func(in *Input) { in.SUN = SUNResult{Valid: false} }},
		{"reject_deactivated", func(in *Input) { in.Employee.Status = EmployeeDeactivated }},
		{"ignored_debounce", func(in *Input) {
			in.LastForPerson = &Transaction{OccurredAt: in.Now.Add(-10 * time.Second), Direction: TypeIn}
		}},
		{"redirect_no_session", func(in *Input) { in.Employee = nil }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			in := baseInput()
			// An open check-in is present, yet a non-record verdict must NOT carry a
			// direction: it never pairs with anything.
			in.LastOpenIn = &Transaction{ID: uuid.New(), OccurredAt: in.Now.Add(-2 * time.Hour), Direction: TypeIn}
			c.mutate(&in)
			got := Decide(in)
			if got.Type != nil {
				t.Errorf("%s: Type = %q, want nil (only ok/flag carry a direction)", c.name, *got.Type)
			}
		})
	}
}

// TestDecide_DebounceMeasuresBothTheDeclaredAndTheServerClock is ADR 0006's
// table. §5 row 5 asks "did this person tap again within the window", and the
// honest answer needs a fact the caller cannot steer.
//
// 🔴 THE HOLE HAD THREE LAYERS AND THE FIRST FIX ONLY REACHED ONE. All measured
// end to end (internal/handler/qr_db_test.go), one scanned QR context each time:
//
//	DISTANCE   the declared occurred_at inflated the gap        20 counted rows
//	SELECTION  the predecessor is CHOSEN by occurred_at order   20 counted rows
//	SIGN       a future-dated predecessor made the gap negative 20 counted `ok`s
//
// Adding a created_at column to the predecessor fixed DISTANCE and left the other
// two untouched, because both are about WHICH row is read, not what it says. The
// server leg therefore comes from a separately ordered fact, whose AGE is computed
// in SQL (SecondsSinceLastRecordedTap) so no Go clock enters it.
func TestDecide_DebounceMeasuresBothTheDeclaredAndTheServerClock(t *testing.T) {
	t.Parallel()
	now := baseInput().Now
	// ago(x) is "the server wrote this person's last tap x ago", the shape the
	// database now computes.
	ago := func(seconds float64) *float64 { return &seconds }

	tests := []struct {
		name        string
		last        *Transaction
		recordedAt  *float64
		wantIgnored bool
		why         string
	}{
		{
			name:        "no history at all",
			wantIgnored: false,
			why:         "a first tap is never a duplicate (§4.6's safe zero value)",
		},
		{
			name:        "both legs far: an ordinary later tap",
			last:        &Transaction{Direction: TypeIn, OccurredAt: now.Add(-90 * time.Second)},
			recordedAt:  ago(90),
			wantIgnored: false,
			why:         "90 s on both clocks is outside the 60 s window",
		},
		{
			name:        "both legs near: the plain duplicate §5 row 5 was always about",
			last:        &Transaction{Direction: TypeIn, OccurredAt: now.Add(-20 * time.Second)},
			recordedAt:  ago(20),
			wantIgnored: true,
			why:         "20 s on both clocks is inside the window",
		},
		{
			// LAYER 1 (DISTANCE) and LAYER 2 (SELECTION) look identical from here,
			// and that is the point: whether the caller inflated the predecessor's
			// claim or froze which row IS the predecessor, the declared leg reads
			// far and the server leg reads seconds.
			name:        "declared far, recorded seconds ago: backdating and freezing both land here",
			last:        &Transaction{Direction: TypeIn, OccurredAt: now.Add(-71 * time.Hour)},
			recordedAt:  ago(1),
			wantIgnored: true,
			why:         "a declared past cannot manufacture distance the server clock denies",
		},
		{
			// The mirror, and why min is not simply "use created_at": a genuinely
			// queued tap (M9-01) is written late and its DECLARED time is the real
			// one. Written far, declared near, is still a repeat.
			name:        "declared near, recorded long ago",
			last:        &Transaction{Direction: TypeIn, OccurredAt: now.Add(-10 * time.Second)},
			recordedAt:  ago(10800),
			wantIgnored: true,
			why:         "the smaller distance decides",
		},
		{
			// 🔴 LAYER 3 (SIGN). sys:occurred-at-bound REFUSES a future stamp but
			// §4.6 still records the row, which then wins the declared ordering and
			// makes the declared distance negative. The guardrail matches on
			// `gap >= 0`, so a negative gap used to switch it off entirely.
			name:        "future-dated predecessor: the negative leg must not disable the guardrail",
			last:        &Transaction{Direction: TypeIn, OccurredAt: now.Add(48 * time.Hour)},
			recordedAt:  ago(2),
			wantIgnored: true,
			why:         "a claim about the future is not evidence of distance; the server leg answers",
		},
		{
			// The same poison with NOTHING recorded to fall back on. Dropping the
			// negative leg is fail-OPEN here, deliberately: there is no evidence of
			// a recent tap, and inventing one would swallow a real tap for as long
			// as the future stamp lasts.
			name:        "future-dated predecessor and no recorded tap: not debounced",
			last:        &Transaction{Direction: TypeIn, OccurredAt: now.Add(48 * time.Hour)},
			wantIgnored: false,
			why:         "nothing measurable; a self-inflicted stamp must not cost this person hours",
		},
		{
			name:        "no recorded tap, declared far: the manual/bulk-entry shape",
			last:        &Transaction{Direction: TypeIn, OccurredAt: now.Add(-8 * time.Hour)},
			wantIgnored: false,
			why:         "a manager's entry is excluded from the server leg by the query predicate",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := baseInput()
			in.LastForPerson = tc.last
			in.SecondsSinceLastRecordedTap = tc.recordedAt
			got := Decide(in)
			gotIgnored := got.Verdict == VerdictIgnored
			if gotIgnored != tc.wantIgnored {
				t.Fatalf("ignored = %v, want %v (%s); verdict %q via %q",
					gotIgnored, tc.wantIgnored, tc.why, got.Verdict, got.MatchedSid)
			}
			if gotIgnored && got.MatchedSid != "sys:person-debounce" {
				t.Fatalf("ignored via %q, want sys:person-debounce", got.MatchedSid)
			}
		})
	}
}

// TestDebounceGap_TakesTheSmallerDistance drives debounceGap DIRECTLY, and it
// exists because driving it only through Decide could not kill two mutations:
// each guard is redundant with a check somewhere else (the guardrail already
// refuses a negative gap), and a guard no test can kill is either dead or
// unproven. Asserting the function on its own terms keeps its answer meaningful
// instead of outsourced to a predicate in another package — the "proven in A,
// consumed in B" gap this repo has paid for three times.
func TestDebounceGap_TakesTheSmallerDistance(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	// ago(x) is "the server wrote this person's last tap x ago", the shape the
	// database now computes.
	ago := func(seconds float64) *float64 { return &seconds }
	f := func(v float64) *float64 { return &v }

	tests := []struct {
		name       string
		last       *Transaction
		recordedAt *float64
		want       *float64
	}{
		{"neither leg -> nil, which never debounces", nil, nil, nil},
		{"declared only", &Transaction{OccurredAt: now.Add(-30 * time.Second)}, nil, f(30)},
		{"recorded only", nil, ago(30), f(30)},
		{"declared far, recorded near -> the server clock wins",
			&Transaction{OccurredAt: now.Add(-10 * time.Minute)}, ago(2), f(2)},
		{"declared near, recorded far -> the declared time wins",
			&Transaction{OccurredAt: now.Add(-10 * time.Second)}, ago(10800), f(10)},
		{"declared NEGATIVE (future-dated predecessor) -> dropped, the server leg answers",
			&Transaction{OccurredAt: now.Add(48 * time.Hour)}, ago(2), f(2)},
		{"declared negative with nothing recorded -> nil, not a clamped zero",
			&Transaction{OccurredAt: now.Add(48 * time.Hour)}, nil, nil},
		{"recorded in the future (clock skew) -> dropped, never returned as negative",
			&Transaction{OccurredAt: now.Add(-30 * time.Second)}, ago(-9), f(30)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := baseInput()
			in.Now = now
			in.LastForPerson = tc.last
			in.SecondsSinceLastRecordedTap = tc.recordedAt
			got := debounceGap(in)
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("debounceGap = %v, want nil", *got)
			case tc.want != nil && got == nil:
				t.Fatalf("debounceGap = nil, want %v", *tc.want)
			case tc.want != nil && *got != *tc.want:
				t.Fatalf("debounceGap = %v, want %v", *got, *tc.want)
			}
		})
	}
}
