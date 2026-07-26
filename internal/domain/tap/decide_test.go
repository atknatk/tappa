package tap

import (
	"net/netip"
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
