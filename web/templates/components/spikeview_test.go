package components

import (
	"strings"
	"testing"
)

// TestSpikeFocus_TheTilesSayOnlyWhatTheRecordShows pins the tile words and tones (M12-28): the
// signs map one to one (yes -> ok, no -> bad, n/a -> none); a QR scan is neutral; a counter gap is
// named from the rule; and a partner-venue record -- whose rule the ledger blanked (K-39) -- gets a
// NEUTRAL physical-tap tile, never the green "chip signature valid" (a partner NFC record flagged
// for a counter gap would otherwise show a ✓ beside a note saying the counter jumped), and one
// merged tile instead of the network and the location.
// MUTANT: the partner case removed from physicalTapTile -> the partner rows read "chip signature
// valid" with an ok tone.
func TestSpikeFocus_TheTilesSayOnlyWhatTheRecordShows(t *testing.T) {
	type tile struct{ label, value, sub, tone string }
	tests := []struct {
		name string
		d    DocketView
		want []tile
	}{
		{"nfc, no network, location yes", DocketView{Channel: "nfc", Who: "A", IPSign: "no", GPSSign: "yes", RuleSID: "base:no-evidence-review"}, []tile{
			{"Physical tap", "yes", "chip signature valid", "ok"},
			{"Who", "A", "signed in on this phone", "ok"},
			{"Network", "no", "address not in the venue's range", "bad"},
			{"Location", "yes", "placed the tap at the venue", "ok"},
		}},
		{"qr, signs not checked", DocketView{Channel: "qr", Who: "A", IPSign: "n/a", GPSSign: "n/a", RuleSID: "own:qr-requires-ip"}, []tile{
			{"Physical tap", "QR scan", "no chip signature on a QR scan", "none"},
			{"Who", "A", "signed in on this phone", "ok"},
			{"Network", "n/a", "not checked", "none"},
			{"Location", "n/a", "not checked", "none"},
		}},
		{"counter gap", DocketView{Channel: "nfc", Who: "A", IPSign: "yes", GPSSign: "no", RuleSID: "base:ctr-gap-review"}, []tile{
			{"Physical tap", "yes", "counter gap", "bad"},
			{"Who", "A", "signed in on this phone", "ok"},
			{"Network", "yes", "address in the venue's range", "ok"},
			{"Location", "no", "did not place the tap at the venue", "bad"},
		}},
		{"partner venue, nfc (rule blanked by the ledger)", DocketView{Channel: "nfc", Who: "A", PartnerVenue: true, IPSign: "n/a", GPSSign: "n/a"}, []tile{
			{"Physical tap", "yes", "chip read", "none"},
			{"Who", "A", "signed in on this phone", "ok"},
			{"Network · Location", "not shown", "not shown at a partner venue", "none"},
		}},
		{"partner venue, qr", DocketView{Channel: "qr", Who: "A", PartnerVenue: true, IPSign: "n/a", GPSSign: "n/a"}, []tile{
			{"Physical tap", "QR scan", "no chip signature on a QR scan", "none"},
			{"Who", "A", "signed in on this phone", "ok"},
			{"Network · Location", "not shown", "not shown at a partner venue", "none"},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SpikeFocus(tc.d).Tiles
			if len(got) != len(tc.want) {
				t.Fatalf("%d tiles, want %d: %+v", len(got), len(tc.want), got)
			}
			for i, w := range tc.want {
				g := got[i]
				if g.Label != w.label || g.Value != w.value || g.Sub != w.sub || g.Tone != w.tone {
					t.Errorf("tile %d = %+v, want %+v", i, g, w)
				}
			}
			if tc.d.PartnerVenue {
				for _, g := range got {
					if strings.Contains(g.Sub, "signature valid") || strings.Contains(g.Sub, "counter") {
						t.Errorf("a partner-venue tile says %q", g.Sub)
					}
				}
			}
		})
	}
}
