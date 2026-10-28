package tap

import (
	"strings"
	"testing"
	"time"
)

// TestDecide_TheAskedNoteIsBuiltFromThePartsWithoutTheLeftOpenOne is the engine's half of
// M15-17's note suppression (ADR 0032 §C.1, product lens N6, the 2026-10-23 correction): the
// result screen leaves the left-open sentence out when its claim card asks about that very
// check-in, and it does so STRUCTURALLY -- Decision.EmployeeNoteAsked is assembled from the same
// parts, in the same order, without the left-open one -- so the separator, the order and every
// other part ("you chose to clock in") survive exactly. The record's Note keeps LeftOpenNote in
// every shape, and with nothing left open the two screen strings are one.
func TestDecide_TheAskedNoteIsBuiltFromThePartsWithoutTheLeftOpenOne(t *testing.T) {
	t.Parallel()
	split := func() Input {
		in := onSiteInput()
		in.Now = mt(t, 27, 18, 0)
		in.DayCutoff = maltaCutoff(5 * time.Hour)
		in.LastOpenIn = openIn(mt(t, 27, 11, 0))
		in.RequestedDirection = dirPtr(TypeIn)
		return in
	}
	yesterday := func() Input {
		in := onSiteInput()
		in.Now = mt(t, 27, 9, 0)
		in.DayCutoff = maltaCutoff(5 * time.Hour)
		in.LastOpenIn = openIn(mt(t, 26, 9, 12))
		return in
	}
	tests := []struct {
		name       string
		in         func() Input
		leftOpen   bool
		wantScreen string // EmployeeNote
		wantAsked  string // EmployeeNoteAsked
	}{
		{"chosen in leaves the open check-in: the choice survives", split, true,
			ruleIP + "; " + LeftOpenEmployeeNote + "; " + ChoseClockInEmployeeNote,
			ruleIP + "; " + ChoseClockInEmployeeNote},
		{"a previous business day left open: the rule's sentence alone", yesterday, true,
			ruleIP + "; " + LeftOpenEmployeeNote, ruleIP},
		{"chosen out: nothing left open, the two are one", func() Input {
			in := onSiteInput()
			in.Now = mt(t, 27, 9, 0)
			in.DayCutoff = maltaCutoff(5 * time.Hour)
			in.RequestedDirection = dirPtr(TypeOut)
			return in
		}, false, ruleIP + "; " + ChoseClockOutEmployeeNote, ruleIP + "; " + ChoseClockOutEmployeeNote},
		{"no open check-in", onSiteInput, false, ruleIP, ruleIP},
		{"a refusal carries no direction", func() Input {
			in := yesterday()
			in.Tag.Status = TagLost
			return in
		}, false, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Decide(tc.in())
			if (got.LeftOpen != nil) != tc.leftOpen {
				t.Fatalf("LeftOpen = %+v, want left open %v", got.LeftOpen, tc.leftOpen)
			}
			if tc.wantScreen == "" {
				if got.EmployeeNote != got.EmployeeNoteAsked || got.Type != nil {
					t.Fatalf("refusal: EmployeeNote %q, asked %q, type %v; want one string and no direction",
						got.EmployeeNote, got.EmployeeNoteAsked, got.Type)
				}
				return
			}
			if got.EmployeeNote != tc.wantScreen {
				t.Errorf("EmployeeNote = %q, want %q", got.EmployeeNote, tc.wantScreen)
			}
			if got.EmployeeNoteAsked != tc.wantAsked {
				t.Errorf("EmployeeNoteAsked = %q, want %q", got.EmployeeNoteAsked, tc.wantAsked)
			}
			if strings.Contains(got.EmployeeNoteAsked, LeftOpenEmployeeNote) {
				t.Errorf("EmployeeNoteAsked %q still carries the left-open part", got.EmployeeNoteAsked)
			}
			if tc.leftOpen && !strings.Contains(got.Note, LeftOpenNote) {
				t.Errorf("the record's Note %q lost LeftOpenNote: only the screen chooses", got.Note)
			}
		})
	}
}
