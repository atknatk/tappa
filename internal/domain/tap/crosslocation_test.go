package tap

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/policy"
	"github.com/google/uuid"
)

// This file is backlog T125 / ADR 0029 on the DECISION: a tap at another of the
// business's locations is decided exactly as the same evidence is decided at the
// employee's home location, and only NOTED -- "Farklı şubede tap nota işlenir,
// raporda ayrı görünür" (CLAUDE.md §5). base:cross-location-note is a note
// (policy.EffectNote) since 2026-10-18; it used to be an allow, which let a GPS-only
// tap at another branch through after a tenant switched base:gps-only-allow off.

// setWithoutGPSOnly is testSet() with base:gps-only-allow switched off -- Q16's
// "require IP" lever (a disabled policy is absent from the Set).
func setWithoutGPSOnly() policy.Set {
	set := testSet()
	var kept []policy.Policy
	for _, p := range set.Baseline {
		if p.Document.Statements[0].Sid != policy.SidGPSOnlyAllow {
			kept = append(kept, p)
		}
	}
	set.Baseline = kept
	return set
}

// crossLocationReason is the shipped note's sentence.
func crossLocationReason(t *testing.T) string {
	t.Helper()
	for _, d := range policy.Baseline() {
		if st := d.Document.Statements[0]; st.Sid == policy.SidCrossLocationNote {
			return st.Reason
		}
	}
	t.Fatal("no shipped cross-location note")
	return ""
}

// TestDecide_CrossLocationGetsTheHomeOutcomePlusANote is the acceptance table: six
// evidence shapes (NFC/QR × IP / GPS only / none) × gps-only-allow on/off. For each,
// the tap at ANOTHER location and the tap at HOME agree on verdict, rule, layer,
// version, trust, direction and lateness; the other-location note is the home note
// with the cross-location sentence inserted after the rule's sentence, and the
// report fact (CrossLocation, policy_context's employee:crossLocation) is set.
func TestDecide_CrossLocationGetsTheHomeOutcomePlusANote(t *testing.T) {
	t.Parallel()
	reason := crossLocationReason(t)
	// The tapped location's shift starts 11:00 Malta (09:00 UTC); the tap is 10 min late.
	shift := &Shift{Start: 11 * time.Hour, End: 19 * time.Hour, Timezone: "Europe/Malta"}
	now := time.Date(2026, 7, 26, 9, 10, 0, 0, time.UTC)

	type shape struct {
		name     string
		qr       bool
		ip, gps  bool
		wantOn   Verdict // gps-only-allow on
		sidOn    string
		wantOff  Verdict // gps-only-allow off (require IP)
		sidOff   string
		wantTrus int
	}
	shapes := []shape{
		{"nfc_ip", false, true, false, VerdictOK, policy.SidIPOrGPSOK, VerdictOK, policy.SidIPOrGPSOK, 70},
		{"nfc_gps_only", false, false, true, VerdictOK, policy.SidGPSOnlyAllow, VerdictFlag, "default", 50},
		{"nfc_no_evidence", false, false, false, VerdictFlag, policy.SidNoEvidenceReview, VerdictFlag, policy.SidNoEvidenceReview, 20},
		{"qr_ip", true, true, false, VerdictOK, policy.SidIPOrGPSOK, VerdictOK, policy.SidIPOrGPSOK, 70},
		{"qr_gps_only", true, false, true, VerdictFlag, policy.SidQRRequiresIP, VerdictFlag, policy.SidQRRequiresIP, 50},
		{"qr_no_evidence", true, false, false, VerdictFlag, policy.SidNoEvidenceReview, VerdictFlag, policy.SidNoEvidenceReview, 20},
	}
	for _, sh := range shapes {
		for _, gpsOnlyOn := range []bool{true, false} {
			sh, gpsOnlyOn := sh, gpsOnlyOn
			want, wantSid := sh.wantOn, sh.sidOn
			if !gpsOnlyOn {
				want, wantSid = sh.wantOff, sh.sidOff
			}
			t.Run(fmt.Sprintf("%s_gps_only_allow_%v", sh.name, gpsOnlyOn), func(t *testing.T) {
				t.Parallel()
				build := func(cross bool) Input {
					in := baseInput()
					in.Now = now
					in.Shift = shift
					if sh.qr {
						in.Channel, in.SUN = ChannelQR, SUNResult{Valid: false}
					}
					if sh.ip {
						withIP(&in)
					}
					if sh.gps {
						withGPSMatch(&in)
					}
					if !gpsOnlyOn {
						in.PolicySet = setWithoutGPSOnly()
					}
					if cross {
						home := uuid.New()
						in.Employee.Location = &home
					}
					return in
				}
				home, other := Decide(build(false)), Decide(build(true))

				if home.Verdict != want || home.MatchedSid != wantSid {
					t.Fatalf("home: %q via %q, want %q via %q", home.Verdict, home.MatchedSid, want, wantSid)
				}
				if other.Verdict != home.Verdict || other.MatchedSid != home.MatchedSid || other.Layer != home.Layer ||
					!sameID(other.PolicyVersionID, home.PolicyVersionID) {
					t.Fatalf("another location decided differently: home %q via %q, other %q via %q",
						home.Verdict, home.MatchedSid, other.Verdict, other.MatchedSid)
				}
				if other.Trust != home.Trust || home.Trust != sh.wantTrus {
					t.Errorf("trust: home %d, other %d, want %d for both", home.Trust, other.Trust, sh.wantTrus)
				}
				if home.Type == nil || other.Type == nil || *home.Type != *other.Type || *home.Type != TypeIn {
					t.Errorf("direction: home %v, other %v, want in for both", home.Type, other.Type)
				}
				if home.MinutesLate == nil || other.MinutesLate == nil || *home.MinutesLate != 10 || *other.MinutesLate != 10 {
					t.Errorf("lateness: home %v, other %v, want 10 for both", home.MinutesLate, other.MinutesLate)
				}

				// "nota işlenir": the other-location record says so, the home one does not.
				if strings.Contains(home.Note, reason) {
					t.Errorf("home note %q carries the cross-location sentence", home.Note)
				}
				// No direction note here (no open check-in), so the home note is the
				// rule's sentence alone and the note follows it.
				if strings.Contains(home.Note, staleOpenInNote) {
					t.Fatalf("precondition: home note %q carries a direction note", home.Note)
				}
				if wantNote := appendNote(home.Note, reason); other.Note != wantNote {
					t.Errorf("other note = %q, want %q", other.Note, wantNote)
				}
				// "raporda ayrı görünür": the report reads the fact, not the note.
				if !other.CrossLocation || home.CrossLocation {
					t.Errorf("CrossLocation: home %v, other %v", home.CrossLocation, other.CrossLocation)
				}
				if other.PolicyContext[policy.CtxEmployeeCrossLocation] != true {
					t.Errorf("policy_context employee:crossLocation = %v, want true", other.PolicyContext[policy.CtxEmployeeCrossLocation])
				}
			})
		}
	}
}

// TestDecide_CrossLocationDoesNotTouchTheGuardrails: debounce (§5 row 5) and the
// other guardrails are terminal, so a guardrail decision at another location
// carries no note and the same outcome as at home.
func TestDecide_CrossLocationDoesNotTouchTheGuardrails(t *testing.T) {
	t.Parallel()
	reason := crossLocationReason(t)
	cases := map[string]func(in *Input){
		"debounce": func(in *Input) {
			in.LastForPerson = &Transaction{OccurredAt: in.Now.Add(-20 * time.Second), Direction: TypeIn}
		},
		"retired_tag": func(in *Input) { in.Tag.Status = TagRetired },
		"deactivated": func(in *Input) { in.Employee.Status = EmployeeDeactivated },
	}
	for name, mutate := range cases {
		mutate := mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			at := func(cross bool) Decision {
				in := onSiteInput()
				in.PolicySet = setWithoutGPSOnly()
				mutate(&in)
				if cross {
					home := uuid.New()
					in.Employee.Location = &home
				}
				return Decide(in)
			}
			home, other := at(false), at(true)
			if other.Verdict != home.Verdict || other.MatchedSid != home.MatchedSid || other.Layer != policy.LayerGuardrail {
				t.Fatalf("home %q via %q, other %q via %q (layer %q)", home.Verdict, home.MatchedSid, other.Verdict, other.MatchedSid, other.Layer)
			}
			if strings.Contains(other.Note, reason) {
				t.Fatalf("a guardrail decision carries the note: %q", other.Note)
			}
		})
	}
}

func sameID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// TestDecide_CrossLocationNoteSitsBetweenTheRuleAndTheDirection pins the note's
// place in the record's note: "<rule>; <cross-location>; <direction>". The order is
// what ledger.partnerNote relies on to keep the direction's annotation (a suffix).
func TestDecide_CrossLocationNoteSitsBetweenTheRuleAndTheDirection(t *testing.T) {
	t.Parallel()
	in := onSiteInput()
	home := uuid.New()
	in.Employee.Location = &home
	in.LastOpenIn = &Transaction{ID: uuid.New(), OccurredAt: in.Now.Add(-72 * time.Hour), Direction: TypeIn}
	got := Decide(in)
	want := "network proof of place: the source IP matches the location; " + crossLocationReason(t) + "; " + staleOpenInNote
	if got.Note != want || got.MatchedSid != policy.SidIPOrGPSOK {
		t.Fatalf("Note = %q via %q, want %q via %s", got.Note, got.MatchedSid, want, policy.SidIPOrGPSOK)
	}
}
