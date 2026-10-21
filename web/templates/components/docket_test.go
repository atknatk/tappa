package components

import (
	"context"
	"strings"
	"testing"
)

// The two dockets of M12-11, rendered directly. The page-level proofs (the real
// handler, the real holding read) stay in internal/handler; these pin what the
// components themselves print for the shapes a record can take.

func renderDocket(t *testing.T, rail bool, d DocketView) string {
	t.Helper()
	var b strings.Builder
	c := TransactionDocket(d)
	if rail {
		c = RailDocket(d)
	}
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func ownDocket() DocketView {
	return DocketView{
		Venue: "KF St Julians", Who: "Maria Borg", Direction: "in", At: "14:03:22",
		Trust: "70", IPSign: "yes", GPSSign: "no", TagUID: "91AC7E5504A001", Ctr: "612",
		Channel: "nfc", Verdict: "ok",
	}
}

// partnerDocket is what ledger hands a row at a partner venue: the score and the
// signs already blanked (K-39). The signs are set anyway, so a docket that printed
// them would be caught rather than printing two empty strings.
func partnerDocket() DocketView {
	d := ownDocket()
	d.Venue, d.PartnerVenue, d.Trust = "KM Production Hall", true, ""
	d.IPSign, d.GPSSign = "no", "yes"
	return d
}

func TestRailDocket_APartnerRowSaysWhyAndPrintsNoEvidence(t *testing.T) {
	got := renderDocket(t, true, partnerDocket())
	for _, want := range []string{"KM Production Hall · partner venue", "not shown at a partner venue", "APPROVED", "Maria Borg"} {
		if !strings.Contains(got, want) {
			t.Errorf("the partner rail docket does not say %q:\n%s", want, got)
		}
	}
	for _, never := range []string{"Trust", "IP ", "GPS", "none", "n/a", "yes"} {
		if strings.Contains(got, never) {
			t.Errorf("the partner rail docket prints %q -- K-39: the outcome, never the evidence or its label", never)
		}
	}
}

func TestRailDocket_AnOwnRowCarriesTheCodesSigns(t *testing.T) {
	got := renderDocket(t, true, ownDocket())
	for _, want := range []string{"KF St Julians", "Maria Borg", ">in<", "14:03:22", "Trust 70", "IP yes · GPS no", "APPROVED", `class="docket `} {
		if !strings.Contains(got, want) {
			t.Errorf("the rail docket does not carry %q:\n%s", want, got)
		}
	}
	for _, never := range []string{"partner venue", "TRAINING", "Entered by a manager"} {
		if strings.Contains(got, never) {
			t.Errorf("an ordinary rail docket says %q", never)
		}
	}
	// Absent values are words, never blanks (§4.6).
	bare := renderDocket(t, true, DocketView{Verdict: "reject", IPSign: "n/a", GPSSign: "n/a"})
	for _, want := range []string{"No location recorded", "Nobody recognised", ">none<", "Trust none", "IP n/a · GPS n/a", "REJECTED"} {
		if !strings.Contains(bare, want) {
			t.Errorf("a bare record's rail docket does not say %q:\n%s", want, bare)
		}
	}
}

func TestRailDocket_ManualAndTrainingStayDistinguishable(t *testing.T) {
	manual := ownDocket()
	manual.Manual, manual.Channel, manual.TagUID, manual.Ctr = true, "manual", "", ""
	manual.IPSign, manual.GPSSign = "n/a", "n/a"
	practice := ownDocket()
	practice.Practice = true

	m := renderDocket(t, true, manual)
	p := renderDocket(t, true, practice)
	if !strings.Contains(m, "Entered by a manager") || strings.Contains(m, "TRAINING") {
		t.Errorf("the manual rail docket is not marked as entered by a manager and only that:\n%s", m)
	}
	if !strings.Contains(p, `stamp stamp--training">TRAINING`) || strings.Contains(p, "Entered by a manager") {
		t.Errorf("the practice rail docket is not stamped TRAINING and only that:\n%s", p)
	}
	// The component prints the signs it is handed; turning a manual record's NULL
	// evidence into "n/a" rather than "no" is the handler's sign(), and the real
	// read path is what guards it (TestTransactionDocket_EvidenceSignHasThreeStates,
	// TestManualEntry_TheRedesignsHeadingStepsAndPreview). What this component owns
	// is that a manual record keeps its evidence line, with the handler's words in it.
	if !strings.Contains(m, "IP n/a · GPS n/a") {
		t.Errorf("the manual rail docket does not print the signs it was handed (IP n/a · GPS n/a):\n%s", m)
	}
}

// TestTransactionDocket_OneComponentAtEveryWidth: the full definition list and the
// compact parts are one article, the things that must exist once do, the counter
// is the handler's own digits, and a partner row carries the phrase in both
// halves and no evidence label in either.
func TestTransactionDocket_OneComponentAtEveryWidth(t *testing.T) {
	d := ownDocket()
	d.Note, d.ReviewNote, d.Review, d.Queued, d.Manual = "engine sentence", "person sentence", "approved", true, true
	got := renderDocket(t, false, d)
	if n := strings.Count(got, "<article"); n != 1 {
		t.Fatalf("%d articles, want one docket", n)
	}
	for text, want := range map[string]int{
		"TAG 91AC7E5504A001 · #612": 1, "APPROVED": 1, "Entered by a manager": 1, "Approved by a manager": 1,
		"engine sentence": 1, "person sentence": 1, "Manager's note": 1,
		// the four short values: once in the list, once in the compact parts.
		"IP yes · GPS no": 2, "14:03:22": 2,
	} {
		if n := strings.Count(got, text); n != want {
			t.Errorf("%q appears %d times, want %d:\n%s", text, n, want, got)
		}
	}
	if strings.Contains(got, "#000612") {
		t.Error("the counter is padded; D1 prints it as the handler formats it")
	}
	// The manager's sentence never runs into the engine's.
	if strings.Contains(got, "engine sentence person sentence") || strings.Contains(got, "engine sentence</p><p class=\"text-xs") {
		t.Errorf("the manager's note is merged with the engine note:\n%s", got)
	}

	p := renderDocket(t, false, partnerDocket())
	if n := strings.Count(p, "not shown at a partner venue"); n != 2 {
		t.Errorf("the partner phrase appears %d times, want 2 (list and compact line):\n%s", n, p)
	}
	if !strings.Contains(p, "KM Production Hall · partner venue") {
		t.Error("the partner docket does not mark the venue as a partner's")
	}
	// The full list keeps its one merged label (M11); the compact line carries none.
	if n := strings.Count(p, "Trust · Evidence"); n != 1 {
		t.Errorf("the merged label appears %d times, want once (the full list only)", n)
	}
	for _, never := range []string{">Trust<", ">Evidence<", "Trust 70", "Trust none", "IP no", "GPS yes"} {
		if strings.Contains(p, never) {
			t.Errorf("the partner docket prints %q (K-39)", never)
		}
	}
	if own := renderDocket(t, false, ownDocket()); strings.Contains(own, "partner venue") {
		t.Error("an own-venue docket says partner venue")
	}
}

func TestVerdictStamp_EveryVerdictHasItsWordAndAnUnknownOneHasNone(t *testing.T) {
	for verdict, word := range map[string]string{"ok": "APPROVED", "flag": "FLAGGED", "reject": "REJECTED", "ignored": "IGNORED", "bogus": ""} {
		var b strings.Builder
		if err := VerdictStamp(verdict).Render(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		if word == "" {
			if b.Len() != 0 {
				t.Errorf("an unknown verdict renders %q", b.String())
			}
			continue
		}
		if !strings.Contains(b.String(), `class="stamp stamp--`) || !strings.Contains(b.String(), ">"+word+"<") {
			t.Errorf("%s: %q", verdict, b.String())
		}
	}
}
