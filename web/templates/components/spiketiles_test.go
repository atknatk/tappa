package components

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

// TestSpikeDocket_TheTilesAreTwoByTwoOrOneRowNeverThreeAndOne pins the evidence tiles
// (DUSUK-7). An auto-fit grid of 150 px tracks put three tiles in the 496 px the record has at
// a 1200 px desk and the fourth (LOCATION) alone on a second row. Now: two columns, and four
// only from the window width at which each of four gets 150 px. A partner-venue record has
// three tiles; its merged one spans two columns, so neither count leaves a tile alone.
// MUTANTS: the auto-fit grid back (no fixed counts); the merged tile not spanning (2 + 1 on
// the desk, 3 + an empty slot on a wide window).
func TestSpikeDocket_TheTilesAreTwoByTwoOrOneRowNeverThreeAndOne(t *testing.T) {
	const grid = `<div class="grid grid-cols-2 gap-2 md:gap-2.5 min-[1340px]:grid-cols-4">`
	tile := regexp.MustCompile(`<div class="(ev-tile [^"]*)">`)
	for _, tc := range []struct {
		name  string
		d     DocketView
		tiles int
		wide  int
	}{
		{"own venue", ownDocket(), 4, 0},
		{"partner venue", partnerDocket(), 3, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := renderComponent(t, func(b *strings.Builder) error {
				return SpikeDocket(SpikeFocus(tc.d)).Render(context.Background(), b)
			})
			if !strings.Contains(got, grid) {
				t.Fatalf("the tile grid is not two-or-four columns:\n%s", got)
			}
			if strings.Contains(got, "auto-fit") {
				t.Error("the tile grid still lets the browser choose three columns")
			}
			classes := tile.FindAllStringSubmatch(got, -1)
			if len(classes) != tc.tiles {
				t.Fatalf("%d tiles, want %d", len(classes), tc.tiles)
			}
			wide, cols := 0, 0
			for _, c := range classes {
				if strings.Contains(" "+c[1]+" ", " col-span-2 ") {
					wide++
					cols += 2
				} else {
					cols++
				}
			}
			if wide != tc.wide {
				t.Errorf("%d tiles span two columns, want %d", wide, tc.wide)
			}
			// Full rows at both counts: the columns the tiles take divide by four (and so by two).
			if cols%4 != 0 {
				t.Errorf("the tiles take %d columns, which leaves a row part-filled", cols)
			}
		})
	}
}
