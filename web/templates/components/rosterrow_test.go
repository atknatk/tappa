package components

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

// The M12-49 screenshot tour found shapes that broke between the tablet and a wide desk (ORTA-3
// on People, DUSUK-7 on The Spike). They cannot be measured without a browser, so these tests pin
// the markup that makes them hold: the classes a regression would have to take away. Each was
// proven red by putting the old markup back.

func renderComponent(t *testing.T, render func(*strings.Builder) error) string {
	t.Helper()
	var b strings.Builder
	if err := render(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// TestRosterRow_TheNameKeepsItsWidthAndThePillsStartAtOneEdge pins the People row (ORTA-3).
// At a 1200 px desk with the person card open the list is 500 px wide, and the row used to
// give the name column what three fixed columns left -- about 88 px, so "Waiter · KF St J…"
// and "Manager · KF Ham…" lost the venue. From the tablet up the name column now has the
// board's 160 px basis and the three figures are ONE block that moves to a second line
// (right-aligned) when the row cannot hold both; and the pill sits in a slot as wide as
// DEACTIVATED, so the three statuses start at one edge instead of three.
// MUTANTS: the basis removed from the name column (the venue truncates again); the pill taken
// out of its slot (DEACTIVATED starts 32 px left of ACTIVE); the figures unwrapped back into
// the row (they cannot move as one block, so a narrow row squeezes the name again).
func TestRosterRow_TheNameKeepsItsWidthAndThePillsStartAtOneEdge(t *testing.T) {
	nameCol := regexp.MustCompile(`<span class="flex min-w-0 flex-1 flex-col gap-0.5 md:basis-40"><strong`)
	// The block: opens with the pill's slot, closes after the signed-in figure, and nothing
	// but the chevron follows it inside the row.
	block := regexp.MustCompile(`(?s)<span class="flex flex-none items-center gap-3.5 md:ml-auto"><span class="flex-none md:w-\[104px\]"><span class="pill pill--[a-z]+ flex-none">[A-Z]+</span></span>(.*?)</span>\s*<span aria-hidden="true" class="roster-chevron"></span></a>`)
	for _, status := range []string{"invited", "active", "deactivated"} {
		for _, lastTap := range []string{"", "14:31 today"} {
			p := RosterRowView{
				Name: "Maria Borg", Initials: "MB", Role: "Waiter", Venue: "KF St Julians",
				Status: status, LastTap: lastTap, Devices: "1 signed in", ManageHref: "/admin/employees?manage=x",
			}
			got := renderComponent(t, func(b *strings.Builder) error { return RosterRow(p).Render(context.Background(), b) })
			if !nameCol.MatchString(got) {
				t.Errorf("%s/%q: the name column has no tablet basis:\n%s", status, lastTap, got)
			}
			m := block.FindStringSubmatch(got)
			if m == nil {
				t.Fatalf("%s/%q: the pill is not in its slot at the head of the figures block:\n%s", status, lastTap, got)
			}
			if !strings.Contains(m[1], "1 signed in") {
				t.Errorf("%s/%q: the signed-in figure is outside the block that wraps as one", status, lastTap)
			}
			if lastTap != "" && !strings.Contains(m[1], lastTap) {
				t.Errorf("%s/%q: the last tap is outside the block that wraps as one", status, lastTap)
			}
		}
	}
}
