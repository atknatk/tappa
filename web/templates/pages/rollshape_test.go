package pages

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The M12-49 screenshot tour's findings on The Roll (DUSUK-8) and The Pass (DUSUK-9), pinned
// by the markup and the stylesheet rules that make them hold. Each was proven red by putting
// the old class back.

func renderPage(t *testing.T, render func(*strings.Builder) error) string {
	t.Helper()
	var b strings.Builder
	if err := render(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// componentRule returns the body of one rule of input.css's component layer, e.g. the
// @apply list of ".roll-qual".
func componentRule(t *testing.T, selector string) string {
	t.Helper()
	css, err := os.ReadFile("../../static/css/input.css")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(selector) + `\s*\{([^}]*)\}`).FindSubmatch(css)
	if m == nil {
		t.Fatalf("input.css has no %s rule", selector)
	}
	return string(m[1])
}

// TestRollTallies_AWrappedQualifierIsABoxNotAnOval pins the qualifiers under a person's name on
// The Roll. The name column is narrow at 834 and 1200 (the table scrolls in its card), so
// "1 LATE ARRIVAL · 13H 05M" and "1 ARRIVAL ENTERED BY A MANAGER" wrap onto two lines; drawn
// fully round they turned into ovals. They now carry .roll-qual -- the boards' 10 px corner --
// in place of the fully round .pill-tally, and keep their tone class beside it.
// MUTANTS: .pill-tally back on the qualifiers; a full round written into .roll-qual.
func TestRollTallies_AWrappedQualifierIsABoxNotAnOval(t *testing.T) {
	p := ReportRowView{Late: "1 late arrival · 13h 05m", Unmeasured: "1 arrival with no shift", Awaiting: "8h 05m",
		Refused: "0h 30m", Manual: "1 arrival entered by a manager", Open: "1 check-in with no checkout"}
	got := renderPage(t, func(b *strings.Builder) error { return reportTallies(p).Render(context.Background(), b) })
	spans := regexp.MustCompile(`<span class="tally tally--[a-z]+ ([^"]*)">`).FindAllStringSubmatch(got, -1)
	if len(spans) != 6 {
		t.Fatalf("%d qualifiers, want 6:\n%s", len(spans), got)
	}
	for _, s := range spans {
		if s[1] != "roll-qual" {
			t.Errorf("a qualifier carries %q, want the roll's own shape roll-qual", s[1])
		}
	}
	rule := componentRule(t, ".roll-qual")
	if !strings.Contains(rule, "rounded-[10px]") || strings.Contains(rule, "rounded-full") {
		t.Errorf(".roll-qual is not the boards' 10 px corner: %s", rule)
	}
}

// TestRollCell_TheTypedMarkHasRoomFromTheTablet pins the day cell that carries the M of a
// manager-typed check-in: at the cell's base padding the M touched the open cell's dashed
// frame. From the tablet up the typed cell takes more room on each side; the phone's reflow
// keeps its own zero padding.
// MUTANT: the tablet padding removed from .roll-cell--manual.
func TestRollCell_TheTypedMarkHasRoomFromTheTablet(t *testing.T) {
	if rule := componentRule(t, ".roll-cell--manual"); !strings.Contains(rule, "md:px-2") {
		t.Errorf(".roll-cell--manual gives its M no room from the tablet: %s", rule)
	}
	got := renderPage(t, func(b *strings.Builder) error {
		return rollCell("", ReportMarkView{Open: true, Manual: true}).Render(context.Background(), b)
	})
	if !strings.Contains(got, "roll-cell--manual") || !strings.Contains(got, `<span class="roll-mark" aria-hidden="true">M</span>`) {
		t.Errorf("an open typed cell is not the typed cell with its M:\n%s", got)
	}
}
