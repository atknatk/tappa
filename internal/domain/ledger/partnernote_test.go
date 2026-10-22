package ledger

import (
	"testing"

	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/policy"
)

// TestPartnerNote_KeepsTheEnginesAnnotations is ADR 0029 (T125) on the employer's
// partner-venue rows, without a database: the evidence rule's sentence is replaced
// (K-39) and every annotation the engine appended after it -- the cross-location
// note, the direction's -- is kept, in order. The rows are not added to
// holding_db_test.go's partnerNoteCases because that table already fills the day
// list's one page (25 rows); partnerNote is pure, and the readers' use of it is what
// that DB test proves.
// MUTANT: partnerKeptAnnotations without the policy notes -> the cross-location
// rows lose their sentence.
// crossNote is base:cross-location-note's sentence, asserted equal to the shipped one.
const crossNote = "tapped at a location other than the employee's home location (recorded as cross-location)"

func TestPartnerNote_KeepsTheEnginesAnnotations(t *testing.T) {
	var shipped string
	for _, d := range policy.Baseline() {
		if st := d.Document.Statements[0]; st.Sid == policy.SidCrossLocationNote {
			shipped = st.Reason
		}
	}
	if shipped != crossNote {
		t.Fatalf("crossNote = %q, the shipped note says %q", crossNote, shipped)
	}
	cases := []struct{ sid, note, want string }{
		{"base:gps-only-allow", "verified via GPS only — no network proof of place", PartnerVenueNote},
		{"base:gps-only-allow", "verified via GPS only — no network proof of place; " + crossNote,
			PartnerVenueNote + "; " + crossNote},
		{"own:ip-or-gps-ok", "network proof of place: the source IP matches the location; " + crossNote + "; " + tap.StaleOpenInNote,
			PartnerVenueNote + "; " + crossNote + "; " + tap.StaleOpenInNote},
		{"base:ctr-gap-review", "the tag counter jumped: reads happened that the server never saw; " + crossNote,
			"the tag counter jumped: reads happened that the server never saw; " + crossNote},
	}
	for _, c := range cases {
		if got := partnerNote(Record{Note: c.note, RuleSID: c.sid}); got != c.want {
			t.Errorf("%s %q: got %q, want %q", c.sid, c.note, got, c.want)
		}
	}
}

// TestPartnerNote_KeepsTheLeftOpenAnnotation: since ADR 0032 the direction's annotation on a
// new record is tap.LeftOpenNote (an `in` that left the previous check-in open), and a partner
// venue's record keeps it after PartnerVenueNote exactly as it keeps the historic stale note.
// MUTANT: partnerKeptAnnotations without tap.LeftOpenNote -> both rows lose the sentence.
func TestPartnerNote_KeepsTheLeftOpenAnnotation(t *testing.T) {
	cases := []struct{ sid, note, want string }{
		{"base:ip-or-gps-ok", "network proof of place: the source IP matches the location; " + tap.LeftOpenNote,
			PartnerVenueNote + "; " + tap.LeftOpenNote},
		{"base:gps-only-allow", "verified via GPS only — no network proof of place; " + crossNote + "; " + tap.LeftOpenNote,
			PartnerVenueNote + "; " + crossNote + "; " + tap.LeftOpenNote},
	}
	for _, c := range cases {
		if got := partnerNote(Record{Note: c.note, RuleSID: c.sid}); got != c.want {
			t.Errorf("%s %q: got %q, want %q", c.sid, c.note, got, c.want)
		}
	}
}
