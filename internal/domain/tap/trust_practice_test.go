package tap

import (
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/geo"
)

// This file proves M4-06: the trust score (20/50/70/100, INDEPENDENT of the
// verdict), the QR channel end to end (Q15, base:qr-requires-ip — GPS alone is not
// enough), the practice flag (SERVER-derived from ActivatedAt + LastForPerson,
// never a client claim — the hours-inflation exploit), and the manual channel
// (SUN is not sought). It reuses baseInput/onSiteInput from decide_test.go.

// evidence fixtures shared across the trust/QR cases.
var (
	trustLocPrefix = netip.MustParsePrefix("203.0.113.0/24")
	trustOnNet     = netip.MustParseAddr("203.0.113.7")    // inside trustLocPrefix
	trustHere      = geo.Point{Lat: 35.8989, Lng: 14.5146} // location coordinate
	trustFar       = geo.Point{Lat: 35.9100, Lng: 14.5300} // ~2 km from trustHere
)

// withIP puts the source on the location's registered network (IP match).
func withIP(in *Input) {
	in.SourceIP = trustOnNet
	in.LocationIPs = []netip.Prefix{trustLocPrefix}
}

// withGPSMatch places the device at the location coordinate (GPS match).
func withGPSMatch(in *Input) {
	in.GPS = &trustHere
	in.LocationGPS = &trustHere
}

// --- Trust: the four scores -------------------------------------------------------

// TestDecide_TrustScoreFourValues proves every value of the §5 formula
// 20 + 50(IP) + 30(GPS): 20 (no evidence), 50 (GPS only), 70 (IP only), 100 (both).
func TestDecide_TrustScoreFourValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(in *Input)
		wantTrust int
		wantIP    bool
		wantGPS   bool
	}{
		{"none_20", func(in *Input) {}, 20, false, false},
		{"gps_only_50", withGPSMatch, 50, false, true},
		{"ip_only_70", withIP, 70, true, false},
		{"ip_and_gps_100", func(in *Input) { withIP(in); withGPSMatch(in) }, 100, true, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := baseInput()
			tc.mutate(&in)
			got := Decide(in)
			if got.Trust != tc.wantTrust {
				t.Errorf("Trust = %d, want %d", got.Trust, tc.wantTrust)
			}
			if got.IPMatch != tc.wantIP || got.GPSMatch != tc.wantGPS {
				t.Errorf("IPMatch/GPSMatch = %v/%v, want %v/%v", got.IPMatch, got.GPSMatch, tc.wantIP, tc.wantGPS)
			}
		})
	}
}

// TestDecide_TrustIsIndependentOfVerdict proves the M4-06 trap: trust measures
// EVIDENCE, not outcome. A REJECT (deactivated, but on the venue network) carries
// trust 70 while an OK (GPS only) carries 50 — so a higher-trust record can be a
// reject and a lower-trust one an ok. Trust is therefore not a function of Verdict.
func TestDecide_TrustIsIndependentOfVerdict(t *testing.T) {
	t.Parallel()

	// OK with GPS only -> trust 50.
	okGPS := baseInput()
	withGPSMatch(&okGPS)
	dOK := Decide(okGPS)
	if dOK.Verdict != VerdictOK || dOK.Trust != 50 {
		t.Fatalf("GPS-only ok: Verdict=%q Trust=%d, want ok/50", dOK.Verdict, dOK.Trust)
	}

	// FLAG with an IP match but a GPS conflict -> review, yet trust 70 (IP counted).
	flagIP := baseInput()
	withIP(&flagIP)
	flagIP.GPS = &trustFar
	flagIP.LocationGPS = &trustHere
	dFlag := Decide(flagIP)
	if dFlag.Verdict != VerdictFlag || dFlag.Trust != 70 {
		t.Fatalf("IP+GPS-conflict flag: Verdict=%q Trust=%d, want flag/70", dFlag.Verdict, dFlag.Trust)
	}

	// REJECT (deactivated) on the venue network -> trust 70 despite the reject.
	rejIP := baseInput()
	rejIP.Employee.Status = EmployeeDeactivated
	withIP(&rejIP)
	dRej := Decide(rejIP)
	if dRej.Verdict != VerdictReject || dRej.Trust != 70 {
		t.Fatalf("deactivated+IP reject: Verdict=%q Trust=%d, want reject/70", dRej.Verdict, dRej.Trust)
	}

	// The load-bearing inequality: a reject scores higher than an ok, so Trust
	// cannot be derived from the verdict.
	if !(dRej.Trust > dOK.Trust) {
		t.Errorf("trust must be evidence-based, not verdict-based: reject=%d ok=%d", dRej.Trust, dOK.Trust)
	}
}

// --- QR channel end to end (Q15) --------------------------------------------------

// TestDecide_QRChannelEndToEnd proves the QR wiring uncoupled from any code path in
// Decide (it is all policy): a QR tap carries no proof of moment, so a GPS MATCH is
// deliberately NOT enough — it still flags via base:qr-requires-ip — while a QR tap
// with an IP match is ok. sys:sun-invalid never fires for QR (it is NFC-only).
func TestDecide_QRChannelEndToEnd(t *testing.T) {
	t.Parallel()

	t.Run("qr_no_ip_gps_match_still_flags", func(t *testing.T) {
		t.Parallel()
		in := baseInput()
		in.Channel = ChannelQR
		in.SUN = SUNResult{Valid: false} // QR carries no SUN
		withGPSMatch(&in)                // GPS MATCHES...
		got := Decide(in)
		if got.GPSMatch != true {
			t.Fatalf("precondition: GPS must match so the test proves GPS is not enough; GPSMatch=%v", got.GPSMatch)
		}
		if got.Verdict != VerdictFlag { // ...and yet it still flags (Q15)
			t.Fatalf("QR without IP must flag even with a GPS match; got %q via %q", got.Verdict, got.MatchedSid)
		}
		if got.MatchedSid != "base:qr-requires-ip" {
			t.Errorf("MatchedSid = %q, want base:qr-requires-ip", got.MatchedSid)
		}
		if got.Trust != 50 { // 20 + 30(GPS), no IP
			t.Errorf("Trust = %d, want 50 (GPS only)", got.Trust)
		}
	})

	t.Run("qr_with_ip_is_ok", func(t *testing.T) {
		t.Parallel()
		in := baseInput()
		in.Channel = ChannelQR
		in.SUN = SUNResult{Valid: false}
		withIP(&in)
		got := Decide(in)
		if got.Verdict != VerdictOK {
			t.Fatalf("QR with an IP match must be ok; got %q via %q", got.Verdict, got.MatchedSid)
		}
		if got.Trust != 70 { // 20 + 50(IP)
			t.Errorf("Trust = %d, want 70 (IP only)", got.Trust)
		}
	})
}

// --- Practice tap: server-derived, never client-declared --------------------------

// TestInput_HasNoClientPracticeField closes the M4-06 hours-inflation exploit
// STRUCTURALLY: Input offers NO place for a client to declare practice, so practice
// can only ever be the server derivation (isPracticeTap). If a future change adds a
// practice/isPractice field to Input, this test fails and forces a security review.
func TestInput_HasNoClientPracticeField(t *testing.T) {
	t.Parallel()
	ty := reflect.TypeOf(Input{})
	for i := 0; i < ty.NumField(); i++ {
		if strings.Contains(strings.ToLower(ty.Field(i).Name), "practice") {
			t.Errorf("Input must carry no client practice field; found %q (M4-06 exploit)", ty.Field(i).Name)
		}
	}
}

// TestDecide_PracticeIsServerDerived proves practice is derived ONLY from
// Employee.ActivatedAt + Input.LastForPerson — the first record after activation is
// practice, every later tap is not, and an unknown activation time is never
// practice. It holds for both an ok and a flag (a practice tap is still a recorded
// attendance tap); it is proven independent of any client input by the structural
// test above (Input has no practice field to set).
func TestDecide_PracticeIsServerDerived(t *testing.T) {
	t.Parallel()

	t.Run("first_tap_after_activation_is_practice_ok", func(t *testing.T) {
		t.Parallel()
		in := onSiteInput() // IP match -> ok; LastForPerson nil; ActivatedAt set
		got := Decide(in)
		if got.Verdict != VerdictOK {
			t.Fatalf("precondition: want ok, got %q via %q", got.Verdict, got.MatchedSid)
		}
		if !got.Practice {
			t.Errorf("the first tap after activation must be practice=true")
		}
	})

	t.Run("first_tap_after_activation_is_practice_flag", func(t *testing.T) {
		t.Parallel()
		in := baseInput() // no evidence -> flag; still the first recorded tap
		got := Decide(in)
		if got.Verdict != VerdictFlag {
			t.Fatalf("precondition: want flag, got %q via %q", got.Verdict, got.MatchedSid)
		}
		if !got.Practice {
			t.Errorf("a flagged first tap after activation must still be practice=true")
		}
	})

	t.Run("second_tap_is_not_practice", func(t *testing.T) {
		t.Parallel()
		in := onSiteInput()
		// A prior tap exists (well outside the debounce window so it is not ignored):
		// this is no longer the first record, so practice must be false.
		in.LastForPerson = &Transaction{OccurredAt: in.Now.Add(-90 * time.Second), Direction: TypeIn}
		got := Decide(in)
		if got.Verdict == VerdictIgnored {
			t.Fatalf("precondition: 90s gap must not debounce; got ignored")
		}
		if got.Practice {
			t.Errorf("a tap with a prior record must not be practice")
		}
	})

	t.Run("unknown_activation_time_is_not_practice", func(t *testing.T) {
		t.Parallel()
		in := onSiteInput()
		in.Employee.ActivatedAt = time.Time{} // activation time unknown
		got := Decide(in)
		if got.Practice {
			t.Errorf("an unknown activation time must not be practice (zero ActivatedAt)")
		}
	})
}

// TestDecide_CheckoutIsNeverPractice is the exploit proof (M4-06). A CHECKOUT
// necessarily has a prior tap (LastForPerson != nil), so it can never satisfy the
// server's practice condition — no client could mark a checkout practice to keep
// the check-in open and over-report hours, because Decide derives practice=false
// from the very fact that a prior tap exists. The tap still resolves to OUT.
func TestDecide_CheckoutIsNeverPractice(t *testing.T) {
	t.Parallel()
	in := onSiteInput()
	openIn := in.Now.Add(-3 * time.Hour)
	in.LastForPerson = &Transaction{OccurredAt: openIn, Direction: TypeIn}
	in.LastOpenIn = &Transaction{OccurredAt: openIn, Direction: TypeIn}
	got := Decide(in)
	if got.Type == nil || *got.Type != TypeOut {
		t.Fatalf("precondition: an open check-in must yield OUT; Type=%v", got.Type)
	}
	if got.Practice {
		t.Errorf("a checkout must never be practice (it has a prior tap): the hours-inflation exploit must stay closed")
	}
}

// TestDecide_NonRecordVerdictsAreNotPractice: only a recorded attendance tap
// (ok/flag) can be practice. reject/ignored/redirect carry no worked hours, so even
// a first-tap-after-activation shape on those verdicts is not marked practice.
func TestDecide_NonRecordVerdictsAreNotPractice(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(in *Input)
	}{
		{"reject_lost_tag", func(in *Input) { in.Tag.Status = TagLost }},
		{"reject_sun_invalid", func(in *Input) { in.SUN = SUNResult{Valid: false} }},
		{"redirect_no_session", func(in *Input) { in.Employee = nil }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			in := baseInput() // LastForPerson nil + ActivatedAt set: the practice shape
			c.mutate(&in)
			got := Decide(in)
			if got.Practice {
				t.Errorf("%s: a non-record verdict must not be practice; Practice=true", c.name)
			}
		})
	}
}

// --- Manual channel ---------------------------------------------------------------

// TestDecide_ManualChannelSkipsSUN proves the manual channel is not subject to the
// NFC-only SUN guardrail: a manager-entered record has no chip signature, so
// SUN.Valid=false must NOT reject it via sys:sun-invalid. It falls through to the
// evidence rules like any tap (here: no evidence -> flag, never reject), and a
// manual tap on the venue network is ok.
//
// DEFERRED to M5 (documented in the M4-06 card): the "manual requires entered_by"
// rule is a WRITE-PATH validation, not a decision. Decide's signature is the fixed
// pure func(Input) Decision (types_test.go) — it cannot return an error — and
// entered_by is a provenance field that changes no verdict/trust/direction, so per
// CLAUDE.md §7 it is validated at the M5-05/M6-04 handler boundary, never silently
// accepted. Input therefore carries no EnteredBy field.
func TestDecide_ManualChannelSkipsSUN(t *testing.T) {
	t.Parallel()

	noEvidence := baseInput()
	noEvidence.Channel = ChannelManual
	noEvidence.SUN = SUNResult{Valid: false} // no chip signature on a manual record
	got := Decide(noEvidence)
	if got.MatchedSid == "sys:sun-invalid" {
		t.Fatalf("manual channel must not be denied by the NFC-only sun-invalid guardrail")
	}
	if got.Verdict == VerdictReject {
		t.Fatalf("manual with no evidence must flag (§4.6), not reject; got %q via %q", got.Verdict, got.MatchedSid)
	}
	if got.Verdict != VerdictFlag {
		t.Errorf("manual with no evidence must flag; got %q via %q", got.Verdict, got.MatchedSid)
	}

	onNet := baseInput()
	onNet.Channel = ChannelManual
	onNet.SUN = SUNResult{Valid: false}
	withIP(&onNet)
	if got := Decide(onNet); got.Verdict != VerdictOK {
		t.Errorf("manual with an IP match must be ok; got %q via %q", got.Verdict, got.MatchedSid)
	}
}
