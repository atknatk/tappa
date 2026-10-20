package pages

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

// TestFloorCard_OnTheDeskTheLinesStartAtTheTop pins The Pass's "On the floor now" card. On the
// desk it shares a grid row with the taller Spike card and wraps its two blocks; with the
// lines spread over the stretched height the label sat 61 px (1200) to 111 px (1440) below the
// card's top. Its lines now pack at the top on the desk.
// MUTANT: the desk's line packing removed from the card.
func TestFloorCard_OnTheDeskTheLinesStartAtTheTop(t *testing.T) {
	f := FloorView{State: WidgetRead, People: "1", Rows: []FloorRow{{Name: "KF St Julians", Ticks: "|", People: "1"}}}
	got := renderPage(t, func(b *strings.Builder) error { return floorCard(f).Render(context.Background(), b) })
	card := regexp.MustCompile(`<div class="(stat-card [^"]*)">`).FindStringSubmatch(got)
	if card == nil {
		t.Fatalf("no floor card:\n%s", got)
	}
	classes := " " + card[1] + " "
	for _, want := range []string{" desk:flex-wrap ", " desk:content-start "} {
		if !strings.Contains(classes, want) {
			t.Errorf("the floor card lacks%q: %s", want, card[1])
		}
	}
}
