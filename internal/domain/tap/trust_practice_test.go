package tap

import (
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

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
// STRUCTURALLY: Input offers NO place for a client to declare practice (and since
// ADR 0026 the server derives none either). If a future change adds a
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

// TestDecide_FirstTapAfterActivationIsNotPractice is ADR 0026's change to §5's
// "practice tap" rule, pinned: the activating NFC tap replaced the training tap, so
// the first RECORD after activation — the shape that used to be practice — is an
// ordinary one, on both of the verdicts that record attendance.
func TestDecide_FirstTapAfterActivationIsNotPractice(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		in      func() Input
		wantVer Verdict
	}{
		{"first_tap_ok", onSiteInput, VerdictOK},   // IP match; LastForPerson nil; ActivatedAt set
		{"first_tap_flag", baseInput, VerdictFlag}, // no evidence; still the first record
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := tc.in()
			if in.Employee == nil || in.Employee.ActivatedAt.IsZero() || in.LastForPerson != nil {
				t.Fatalf("precondition: want the old practice shape (activated, no prior record)")
			}
			got := Decide(in)
			if got.Verdict != tc.wantVer {
				t.Fatalf("precondition: want %q, got %q via %q", tc.wantVer, got.Verdict, got.MatchedSid)
			}
			if got.Practice {
				t.Errorf("the first tap after activation must be an ordinary record since ADR 0026; Practice=true")
			}
			if got.Type == nil || *got.Type != TypeIn {
				t.Errorf("a first record is a check-IN; Type = %v", got.Type)
			}
		})
	}
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

// TestDecide_TheFirstTapCanNeverBeIgnored: the person-debounce needs a PREVIOUS
// tap to measure a gap against, and on a first tap there is none. Still worth
// pinning after ADR 0026: the activation screen promises "to check in, tap the
// plaque again", and that next tap — the employee's first record — must be able to
// land as a real ok/flag rather than a silent duplicate.
func TestDecide_TheFirstTapCanNeverBeIgnored(t *testing.T) {
	t.Parallel()
	in := onSiteInput() // LastForPerson nil -> SecondsSincePersonLastTap is not set
	got := Decide(in)
	if got.Verdict == VerdictIgnored {
		t.Fatalf("a first tap has no predecessor to be a duplicate of; got ignored via %q", got.MatchedSid)
	}
}

// TestDecide_NoNewRecordIsEverPractice is the property ADR 0026 leaves behind: the
// engine never produces a practice record, whatever the history, activation,
// evidence or channel. It checks the PROPERTY over every combination rather than a
// list (the M5-10 lesson), so a branch that started setting Practice again fails
// here without anybody remembering to add a case.
//
// HISTORIC PRACTICE ROWS ARE STILL IN THE TABLE (immutable, §4.3), so the history
// dimension keeps the open-practice-in shapes: a practice row may still be READ as
// LastOpenIn, and must still never close a chain (ADR 0008). The `out` counter is
// the non-vacuity control — an engine that stopped producing `out` at all would
// make "no practice" trivially true of a table that no longer exercises direction.
func TestDecide_NoNewRecordIsEverPractice(t *testing.T) {
	t.Parallel()

	prior := func(in Input) *Transaction {
		return &Transaction{ID: uuid.New(), OccurredAt: in.Now.Add(-300 * time.Second)}
	}
	openIn := func(in Input, practice bool) *Transaction {
		return &Transaction{
			ID: uuid.New(), OccurredAt: in.Now.Add(-3 * time.Hour),
			Direction: TypeIn, Practice: practice,
		}
	}

	type dim struct {
		name  string
		apply func(in *Input)
	}
	history := []dim{
		{"no_history", func(*Input) {}},
		{"prior_tap_only", func(in *Input) { in.LastForPerson = prior(*in) }},
		{"open_real_in", func(in *Input) {
			in.LastForPerson, in.LastOpenIn = prior(*in), openIn(*in, false)
		}},
		{"open_historic_practice_in", func(in *Input) {
			in.LastForPerson, in.LastOpenIn = prior(*in), openIn(*in, true)
		}},
		{"open_real_in_without_a_prior_tap", func(in *Input) { in.LastOpenIn = openIn(*in, false) }},
	}
	activation := []dim{
		{"activated", func(*Input) {}},
		{"never_activated", func(in *Input) { in.Employee.ActivatedAt = time.Time{} }},
	}
	evidence := []dim{
		{"on_site", func(in *Input) {
			in.SourceIP = netip.MustParseAddr("203.0.113.7")
			in.LocationIPs = []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}
		}},
		{"no_evidence", func(*Input) {}},
	}
	channels := []dim{
		{"nfc", func(*Input) {}},
		{"qr", func(in *Input) { in.Channel, in.SUN = ChannelQR, SUNResult{Valid: false} }},
		{"manual", func(in *Input) { in.Channel, in.SUN = ChannelManual, SUNResult{Valid: false} }},
	}

	var combos, recorded, sawOut int
	for _, h := range history {
		for _, a := range activation {
			for _, e := range evidence {
				for _, c := range channels {
					name := h.name + "/" + a.name + "/" + e.name + "/" + c.name
					in := baseInput()
					h.apply(&in)
					a.apply(&in)
					e.apply(&in)
					c.apply(&in)

					got := Decide(in)
					combos++
					if got.Verdict == VerdictOK || got.Verdict == VerdictFlag {
						recorded++
					}
					if got.Practice {
						t.Errorf("%s: Decide produced a practice record; ADR 0026 retired the practice tap", name)
					}
					if got.Type != nil && *got.Type == TypeOut {
						sawOut++
					}
				}
			}
		}
	}
	if recorded == 0 || sawOut == 0 {
		t.Fatalf("%d combinations: %d recorded, %d check-outs — the table no longer exercises "+
			"the records practice used to be set on", combos, recorded, sawOut)
	}
	t.Logf("%d combinations: %d recorded, %d check-outs, 0 practice", combos, recorded, sawOut)
}
