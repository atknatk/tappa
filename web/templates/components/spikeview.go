package components

import "strings"

// The Spike's record in focus (M12-28, screens/spike-odd.md Part S).
//
// ONE MAPPER FROM THE SAME DocketView THE LEDGER'S DOCKETS READ, so the evidence a manager
// judges on cannot drift from the evidence The Pass prints: the tiles below are the docket's
// own three-state sign words (IPSign, GPSSign -- "yes", "no", "n/a") with a sentence under
// each, the channel the column holds, and the person the session named. Nothing here reads
// the record again, and nothing here decides what a partner venue may show: the ledger has
// already blanked a partner row's trust, signs, note and rule (K-39, nameSiteRecords), and
// the tiles only say so in one merged tile instead of printing "n/a" twice -- "n/a" would
// claim nothing was assessed.
//
// THE WORDS ARE THE CODE'S, NOT THE BOARD'S (D1). No carrier, no Wi-Fi name (the Wi-Fi name
// is not evidence), no metres, no "declined": a GPS "no" is ambiguous between "no reading"
// and "outside the radius", so the sentence under it does not choose between them.

// SpikeFocusView is the record a manager is deciding, as SpikeDocket draws it.
type SpikeFocusView struct {
	Docket DocketView
	// Tiles are the evidence tiles, in reading order: the physical tap, the person, then
	// the network and the location -- or, on a partner-venue record, one merged tile.
	Tiles []EvidenceTile
}

// EvidenceTile is one evidence tile. Tone is "ok", "bad" or "none" and picks the frame and
// the mark; the mark is drawing (aria-hidden), the Value and the Sub carry the meaning, so
// the tone is never told by colour alone.
type EvidenceTile struct {
	Label string
	Value string
	Sub   string
	Tone  string
}

// The three tones. A sign word maps onto them one to one: yes -> ok, no -> bad, n/a -> none.
const (
	toneOK   = "ok"
	toneBad  = "bad"
	toneNone = "none"
)

// SpikeFocus maps one docket onto the record in focus.
func SpikeFocus(d DocketView) SpikeFocusView {
	tiles := []EvidenceTile{physicalTapTile(d), whoTile(d)}
	if d.PartnerVenue {
		tiles = append(tiles, EvidenceTile{
			Label: "Network · Location",
			Value: "not shown",
			Sub:   "not shown at a partner venue",
			Tone:  toneNone,
		})
	} else {
		tiles = append(tiles,
			signTile("Network", d.IPSign, "address in the venue's range", "address not in the venue's range"),
			signTile("Location", d.GPSSign, "placed the tap at the venue", "did not place the tap at the venue"),
		)
	}
	return SpikeFocusView{Docket: d, Tiles: tiles}
}

// physicalTapTile says whether the record is a physical touch. A FLAGGED NFC record had a valid
// chip signature: an invalid one is rejected before any rule that can flag (§5 row 2). A QR scan
// carries no signature at all; a counter gap is named from the rule alone (its size is not shown
// -- D1).
//
// 🔴 ON A PARTNER-VENUE RECORD THE NFC TILE IS NEUTRAL: "yes" / "chip read", never the green
// "chip signature valid". The rule is blank there (K-39), so a counter gap cannot be told from
// any other flag -- and a green mark beside an engine note saying the counter jumped would be
// the tile vouching for something the screen cannot see. The tap was a chip read either way.
func physicalTapTile(d DocketView) EvidenceTile {
	t := EvidenceTile{Label: "Physical tap"}
	switch {
	case d.Channel == "qr":
		t.Value, t.Sub, t.Tone = "QR scan", "no chip signature on a QR scan", toneNone
	case d.Channel == "manual":
		t.Value, t.Sub, t.Tone = "n/a", "entered by a manager", toneNone
	case d.Channel == "nfc" && d.PartnerVenue:
		t.Value, t.Sub, t.Tone = "yes", "chip read", toneNone
	case d.Channel == "nfc" && ruleRest(d.RuleSID) == "ctr-gap-review":
		t.Value, t.Sub, t.Tone = "yes", "counter gap", toneBad
	case d.Channel == "nfc":
		t.Value, t.Sub, t.Tone = "yes", "chip signature valid", toneOK
	default:
		t.Value, t.Sub, t.Tone = "n/a", "not checked", toneNone
	}
	return t
}

// whoTile names the person. A flagged tap had a session (§5 row 3 comes before any rule that
// can flag); a record nobody was recognised on says so in words, as the dockets do.
func whoTile(d DocketView) EvidenceTile {
	t := EvidenceTile{Label: "Who", Value: docketWho(d), Tone: toneNone}
	switch {
	case d.Who == "":
	case d.Channel == "manual":
		t.Sub = "entered by a manager"
	default:
		t.Sub, t.Tone = "signed in on this phone", toneOK
	}
	return t
}

// signTile is one evidence sign as a tile: the code's word as the value, a sentence under it.
func signTile(label, sign, yes, no string) EvidenceTile {
	t := EvidenceTile{Label: label, Value: sign}
	switch sign {
	case "yes":
		t.Sub, t.Tone = yes, toneOK
	case "no":
		t.Sub, t.Tone = no, toneBad
	default:
		t.Value, t.Sub, t.Tone = "n/a", "not checked", toneNone
	}
	return t
}

// SpikeWhy is a queue item's short reason: a closed map from the deciding rule to a phrase, in
// the shipped namespace and the tenant's (a tenant rule is a shipped one with its sid moved to
// own:), and "sent for review" for any other. A partner-venue record says only where it was --
// its rule is blank by then (K-39), and nothing here is derived from evidence. No distance (D1).
func SpikeWhy(d DocketView) string {
	if d.PartnerVenue {
		return "at a partner venue"
	}
	switch ruleRest(d.RuleSID) {
	case "no-evidence-review":
		return "no network, no location"
	case "qr-requires-ip":
		return "QR without network"
	case "ctr-gap-review":
		return "counter gap"
	case "gps-conflict-review":
		return "location outside the venue"
	default:
		return "sent for review"
	}
}

// ruleRest is a base: or own: sid without its namespace, "" for any other sid.
func ruleRest(sid string) string {
	ns, rest, ok := strings.Cut(sid, ":")
	if !ok || (ns != "base" && ns != "own") {
		return ""
	}
	return rest
}

// spikeLabel is the mono line above the name: the venue (marked when it is a partner's) and the
// plaque by the last four characters of its uid, which is what the plaque itself prints.
func spikeLabel(d DocketView) string {
	v := docketVenue(d)
	if d.TagUID == "" {
		return v
	}
	uid := []rune(strings.ToUpper(d.TagUID))
	if len(uid) > 4 {
		uid = uid[len(uid)-4:]
	}
	return v + " · plaque " + string(uid)
}

// spikeLine is "IN · 20 Oct 14:26:02 · NFC": direction, the date-ful time, the channel.
func spikeLine(d DocketView) string {
	parts := []string{strings.ToUpper(docketDirection(d)), d.At}
	if d.Channel != "" {
		parts = append(parts, strings.ToUpper(d.Channel))
	}
	return strings.Join(parts, " · ")
}

// spikeRuleLine is the mono line under the engine's sentence: the rule and the score. It is not
// drawn on a partner-venue record at all (K-39: neither is shown there, and the merged tile
// already says so).
func spikeRuleLine(d DocketView) string {
	trust := "trust " + docketTrust(d)
	if d.RuleSID == "" {
		return trust
	}
	return "rule " + d.RuleSID + " · " + trust
}
