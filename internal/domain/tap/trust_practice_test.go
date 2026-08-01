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

// TestDecide_ThePracticeRunIsSpentByANYPriorRecord is the measurement the mini
// tour's third slide (M5-07) is written against, and it exists because that slide
// makes a CLAIM ABOUT THE SYSTEM to somebody who cannot check it.
//
// The claim under test is "your first tap is a practice run". What the engine
// actually does is narrower: a tap is practice only when the person has NO PRIOR
// RECORD, and §4.6 records EVERY decided tap — GetLastTransactionForEmployee
// carries no verdict predicate and no channel predicate (db/queries/transactions.sql
// says so at the query). So a first tap that is REJECTED, or an `ignored`
// duplicate, or a manual row a manager entered, all leave a record and the run is
// gone.
//
// ⚠️ THE TABLE IS NOT "EXHAUSTIVE", AND THE FIRST VERSION OF THIS SENTENCE SAID IT
// WAS. It cannot be, in an interesting way: tap.Transaction carries NO verdict and
// NO channel, so from the engine's side every row below is the SAME INPUT — one
// non-nil predecessor. What the rows enumerate is the SITUATIONS a reader would
// otherwise assume are different, and the finding is that none of them is. (The
// version an audit caught also omitted the most ordinary predecessor of all, an
// earlier `ok`; it is row one now.) pages.Tour's words are chosen so that every
// row here leaves them true.
//
// WHY A TABLE AND NOT FOUR TESTS: these are the same question asked of different
// predecessors, and the answer that would be a bug — "some verdicts do not count"
// — is only visible when they are read side by side.
func TestDecide_ThePracticeRunIsSpentByANYPriorRecord(t *testing.T) {
	t.Parallel()

	// A predecessor far outside the debounce window, so nothing below is `ignored`
	// for the wrong reason.
	priorAt := func(in Input) *Transaction {
		return &Transaction{OccurredAt: in.Now.Add(-300 * time.Second)}
	}

	tests := []struct {
		name string
		// prior describes the record the engine can see, if any. The VERDICT of that
		// record is deliberately absent from Transaction: the query does not filter
		// on it, so the engine cannot tell an `ok` predecessor from a `reject` one —
		// which is the whole finding.
		prior        func(in Input) *Transaction
		wantPractice bool
		why          string
	}{
		{
			name:         "no_prior_record_at_all",
			prior:        func(Input) *Transaction { return nil },
			wantPractice: true,
			why:          "the case the tour's slide is about",
		},
		{
			name:         "after_an_ordinary_earlier_ok",
			prior:        priorAt,
			wantPractice: false,
			why: "the most common predecessor there is, and it was missing from the first " +
				"version of this table — the engine cannot tell it from any other row",
		},
		{
			name:         "after_a_rejected_first_tap",
			prior:        priorAt,
			wantPractice: false,
			why: "a reject IS a record (§4.6), so it spends the practice run: the tour " +
				"may not promise the NEXT tap will be a practice one",
		},
		{
			name:         "after_an_ignored_duplicate",
			prior:        priorAt,
			wantPractice: false,
			why:          "an ignored duplicate is recorded too, and spends it the same way",
		},
		{
			name:         "after_a_manual_row_a_manager_entered",
			prior:        priorAt,
			wantPractice: false,
			why:          "channel is not read here either (M6-04 will make this reachable)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := onSiteInput()
			in.LastForPerson = tc.prior(in)
			got := Decide(in)
			if got.Verdict != VerdictOK {
				t.Fatalf("precondition: want ok, got %q via %q", got.Verdict, got.MatchedSid)
			}
			if got.Practice != tc.wantPractice {
				t.Errorf("Practice = %v, want %v — %s", got.Practice, tc.wantPractice, tc.why)
			}
		})
	}
}

// TestDecide_TheFirstTapCanNeverBeIgnored is the other half of the promise: the
// person-debounce needs a PREVIOUS tap to measure a gap against, and on a first
// tap there is none. So of the four verdicts, only three can befall the tap the
// tour is talking about — ok and flag are practice, reject is not.
//
// It matters because "ignored" is the one verdict whose screen says nothing at all
// about hours; if a first tap could land there, the tour would be promising a
// TRAINING mark on a screen that never shows one.
func TestDecide_TheFirstTapCanNeverBeIgnored(t *testing.T) {
	t.Parallel()
	in := onSiteInput() // LastForPerson nil -> SecondsSincePersonLastTap is not set
	got := Decide(in)
	if got.Verdict == VerdictIgnored {
		t.Fatalf("a first tap has no predecessor to be a duplicate of; got ignored via %q", got.MatchedSid)
	}
	if !got.Practice {
		t.Fatalf("precondition: the first tap after activation is the practice run")
	}
}

// TestDecide_APracticeTapCanStillFlag pins the combination pages.Tour and
// pages.Result both have to survive: practice is set on the SAME ok/flag gate as
// direction, so a training tap at a venue with no evidence is `flag` AND practice.
// The tour says a TRAINING mark means the tap does not count toward hours, and
// that has to stay true of the flagged one too — which is why result.templ drops
// "before it counts" from the flag sentence when Practice is set.
func TestDecide_APracticeTapCanStillFlag(t *testing.T) {
	t.Parallel()
	in := baseInput() // no IP, no GPS
	got := Decide(in)
	if got.Verdict != VerdictFlag {
		t.Fatalf("precondition: want flag, got %q via %q", got.Verdict, got.MatchedSid)
	}
	if !got.Practice {
		t.Errorf("a flagged first tap is still the practice run")
	}
	if got.Type == nil || *got.Type != TypeIn {
		t.Errorf("a flagged practice tap still carries a direction; Type = %v", got.Type)
	}
}

// TestDecide_QRFirstTapIsStillThePracticeRun: the channel is not read by
// isPracticeTap, so somebody whose phone cannot read the chip (M5-08's iPhone X
// note) gets the same practice run as everybody else — with or without an IP
// match, i.e. on both the `ok` and the `flag` the QR baseline can produce.
func TestDecide_QRFirstTapIsStillThePracticeRun(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		mutate  func(in *Input)
		wantVer Verdict
	}{
		{"qr_with_an_ip_match", withIP, VerdictOK},
		{"qr_without_one", func(*Input) {}, VerdictFlag},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := baseInput()
			in.Channel = ChannelQR
			in.SUN = SUNResult{Valid: false} // a static QR carries no chip signature
			tc.mutate(&in)
			got := Decide(in)
			if got.Verdict != tc.wantVer {
				t.Fatalf("precondition: want %q, got %q via %q", tc.wantVer, got.Verdict, got.MatchedSid)
			}
			if !got.Practice {
				t.Errorf("a QR first tap must still be the practice run")
			}
		})
	}
}
