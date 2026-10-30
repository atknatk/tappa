package handler

// panelwide_test.go -- the wide-screen pass (2026-10-30, user decision): on a 1x 4K screen the
// panel's content was a 1152 px island centred in 3592 px of porcelain. The claims held here,
// each read from the source (web/static/css/input.css and the .templ files), with a CONTROL per
// claim that breaks the source and must turn the reading red:
//
//   - the content well stands against the sidebar and stops at 100rem: its rule centres
//     nothing (no auto margin) and caps at nothing else;
//   - a form or a text page keeps the boards' width: the well stops at 70rem when its body
//     carries the .panel-measure marker (or is the Rules page's pinned .policy-section), and
//     the marker is written exactly where this file lists it -- the data pages carry none;
//   - Account's cards and Odd shapes' middle cards flow down columns from the desk width, and
//     only they do; Odd's paragraphs carry the 70rem prose measure;
//   - past a 2400 px window the whole frame is zoomed 1.25, past 3200 px 1.5, nothing below,
//     and each step restates the two viewport heights divided by its zoom.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// panelCSSRule is one rule of input.css with the at-rules around it, outermost first.
type panelCSSRule struct {
	at       []string // e.g. ["@layer components", "@media (min-width: 2400px)"]
	selector string   // whitespace collapsed
	body     string   // whitespace collapsed, without the braces
}

// readPanelCSSRules walks input.css's braces: every block that holds no block of its own is a rule,
// and the preludes of the blocks around it are its at-rules. Comments go first, so a brace or a
// selector in a comment is not read.
func readPanelCSSRules(css string) []panelCSSRule {
	src := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(css, "")
	norm := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	var (
		rules   []panelCSSRule
		stack   []string
		prelude strings.Builder
		start   []int
		leaf    []bool
	)
	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '{':
			stack = append(stack, norm(prelude.String()))
			prelude.Reset()
			start = append(start, i+1)
			if len(leaf) > 0 {
				leaf[len(leaf)-1] = false
			}
			leaf = append(leaf, true)
		case '}':
			if len(stack) == 0 {
				continue
			}
			n := len(stack) - 1
			if leaf[n] {
				rules = append(rules, panelCSSRule{at: slices.Clone(stack[:n]), selector: stack[n], body: norm(src[start[n]:i])})
			}
			stack, start, leaf = stack[:n], start[:n], leaf[:n]
			prelude.Reset()
		case ';':
			// a statement at block level (an @import, a declaration of an outer block) is not a prelude
			prelude.Reset()
		default:
			prelude.WriteByte(src[i])
		}
	}
	return rules
}

// panelRulesNaming are the rules whose selector list names the class -- every spelling, so a second rule
// for the same element somewhere else in the 2 800 lines is counted rather than missed.
func panelRulesNaming(rules []panelCSSRule, class string) []panelCSSRule {
	word := regexp.MustCompile(regexp.QuoteMeta(class) + `($|[^A-Za-z0-9_-])`)
	var out []panelCSSRule
	for _, r := range rules {
		if word.MatchString(r.selector) {
			out = append(out, r)
		}
	}
	return out
}

// applied is the utilities of a rule's @apply, nil when the body is not one @apply.
func panelApplied(body string) []string {
	m := regexp.MustCompile(`^@apply ([^;]+);$`).FindStringSubmatch(body)
	if m == nil {
		return nil
	}
	return strings.Fields(m[1])
}

// readPanelShellWidth is the reading of the well's two rules: one finding per decision broken.
func readPanelShellWidth(css string) []string {
	rules := readPanelCSSRules(css)
	var findings []string
	well := panelRulesNaming(rules, ".panel-well")
	var base, measured []panelCSSRule
	for _, r := range well {
		switch r.selector {
		case ".panel-well":
			base = append(base, r)
		case ".panel-well:has(.panel-measure, .policy-section)":
			measured = append(measured, r)
		default:
			findings = append(findings, fmt.Sprintf("a rule `%s` also styles the well: the well's width has two rules, the base and the measure", r.selector))
		}
	}
	if len(base) != 1 {
		findings = append(findings, fmt.Sprintf("%d `.panel-well` rules, want 1", len(base)))
	} else {
		us := panelApplied(base[0].body)
		if len(base[0].at) != 1 || base[0].at[0] != "@layer components" {
			findings = append(findings, fmt.Sprintf("`.panel-well` sits in %v, want the components layer alone (no width may restyle it)", base[0].at))
		}
		for _, want := range []string{"w-full", "max-w-[100rem]"} {
			if !slices.Contains(us, want) {
				findings = append(findings, "`.panel-well` does not apply "+want+": the well is as wide as the box up to 100rem")
			}
		}
		for _, u := range us {
			if strings.HasPrefix(u, "mx-") || strings.HasPrefix(u, "ml-") || strings.HasPrefix(u, "mr-") || strings.HasPrefix(u, "m-") ||
				strings.Contains(u, ":mx-") || strings.Contains(u, ":ml-") {
				findings = append(findings, "`.panel-well` applies "+u+": the well stands against the sidebar, it is not centred or pushed")
			}
			if strings.Contains(u, "max-w-") && u != "max-w-[100rem]" {
				findings = append(findings, "`.panel-well` applies "+u+": the only cap is 100rem")
			}
		}
	}
	if len(measured) != 1 {
		findings = append(findings, fmt.Sprintf("%d `.panel-well:has(.panel-measure, .policy-section)` rules, want 1", len(measured)))
	} else if measured[0].body != "max-width: 70rem;" {
		findings = append(findings, fmt.Sprintf("the measured well declares %q, want exactly max-width: 70rem", measured[0].body))
	}
	if n := len(panelRulesNaming(rules, ".panel-measure")); n != 1 {
		findings = append(findings, fmt.Sprintf("%d rules name .panel-measure, want 1 (the marker draws nothing of its own)", n))
	}
	return findings
}

func TestPanelWide_TheWellStandsAgainstTheSidebarAndStopsAt100rem(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "web", "static", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	for _, f := range readPanelShellWidth(css) {
		t.Error(f)
	}
	for _, c := range []struct{ what, from, to string }{
		{"the well centred again", "@apply flex w-full max-w-[100rem] flex-col gap-6;", "@apply mx-auto flex w-full max-w-[100rem] flex-col gap-6;"},
		{"the old 72rem cap", "@apply flex w-full max-w-[100rem] flex-col gap-6;", "@apply flex w-full max-w-6xl flex-col gap-6;"},
		{"no cap", "@apply flex w-full max-w-[100rem] flex-col gap-6;", "@apply flex w-full flex-col gap-6;"},
		{"a second rule for the well", ".panel-head { @apply", ".panel-well { margin-left: auto; }\n  .panel-head { @apply"},
		{"a width step that recentres it", ".panel-head { @apply", "@media (min-width: 2000px) { .panel-well { margin-inline: auto; } }\n  .panel-head { @apply"},
		{"the measure widened", "  .panel-well:has(.panel-measure, .policy-section) {\n    max-width: 70rem;", "  .panel-well:has(.panel-measure, .policy-section) {\n    max-width: 80rem;"},
		{"the measure losing the Rules page", ".panel-well:has(.panel-measure, .policy-section) {", ".panel-well:has(.panel-measure) {"},
		{"the measure gone", ".panel-well:has(.panel-measure, .policy-section) {", ".panel-gone {"},
	} {
		mutated := strings.Replace(css, c.from, c.to, 1)
		if mutated == css {
			t.Fatalf("CONTROL %s: %q is not in input.css", c.what, c.from)
		}
		if len(readPanelShellWidth(mutated)) == 0 {
			t.Errorf("CONTROL: %s is not a finding; the reading is blind", c.what)
		}
	}
}

// panelMeasuredTemplates is every .templ that writes the .panel-measure marker, and how many times:
// the bodies a reader fills in or reads through. Odd shapes, The Pass, the ledger, The Spike,
// People, the venues list, The Roll and the Account cards are data and take the full well.
var panelMeasuredTemplates = map[string]int{
	"components/staffform.templ": 1, // New person
	"components/venue.templ":     2, // the venue form and the department form
	"pages/manualentry.templ":    1, // a record typed by hand
	"pages/billing.templ":        2, // Billing and freezing a month
	"pages/apikeys.templ":        1, // Developers
	"pages/admininvites.templ":   1, // Managers
	"pages/linkbusinesses.templ": 1, // Your other businesses
	"pages/policychange.templ":   2, // a rule change and a rule version
	"pages/employees.templ":      2, // the invitation, shown and e-mailed
	"pages/daycutoff.templ":      1, // reviewing a business day change
}

// readPanelMeasureMarkers counts the marker in every template against the list above.
func readPanelMeasureMarkers(files map[string]string) []string {
	marker := regexp.MustCompile(`class="([^"]*\s)?panel-measure(\s[^"]*)?"`)
	var findings []string
	for name, src := range files {
		got := len(marker.FindAllString(src, -1))
		if want := panelMeasuredTemplates[name]; got != want {
			findings = append(findings, fmt.Sprintf("%s writes the measure marker %d time(s), want %d", name, got, want))
		}
	}
	for name := range panelMeasuredTemplates {
		if _, ok := files[name]; !ok {
			findings = append(findings, name+" is not among the templates")
		}
	}
	return findings
}

func readPanelTemplates(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join("..", "..", "web", "templates")
	files := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".templ") {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		files[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestPanelWide_FormAndTextPagesCarryTheMeasureAndDataPagesDoNot(t *testing.T) {
	files := readPanelTemplates(t)
	for _, f := range readPanelMeasureMarkers(files) {
		t.Error(f)
	}
	for _, c := range []struct{ what, file, from, to string }{
		{"the staff form unmeasured", "components/staffform.templ", `class="panel-measure flex`, `class="flex`},
		{"a data page measured", "pages/reports.templ", `<div class="`, `<div class="panel-measure `},
		{"the marker spelled into a longer class", "pages/apikeys.templ", `class="panel-measure flex`, `class="panel-measured flex`},
	} {
		mutated := map[string]string{}
		for k, v := range files {
			mutated[k] = v
		}
		next := strings.Replace(files[c.file], c.from, c.to, 1)
		if next == files[c.file] {
			t.Fatalf("CONTROL %s: %q is not in %s", c.what, c.from, c.file)
		}
		mutated[c.file] = next
		if len(readPanelMeasureMarkers(mutated)) == 0 {
			t.Errorf("CONTROL: %s is not a finding; the reading is blind", c.what)
		}
	}
}

// readPanelCardFlow is the reading of the card flow and the prose measure in input.css, and of the
// two templates that use the flow: one finding per decision broken.
func readPanelCardFlow(css string, files map[string]string) []string {
	rules := readPanelCSSRules(css)
	var findings []string
	desk := []string{"@layer components", "@screen desk"}
	want := map[string]string{
		".card-flow":                        "columns: 25rem 3;",
		".card-flow > :not(.card-flow-col)": "break-inside: avoid; margin-bottom: 1.25rem;",
		".card-flow-col > *":                "break-inside: avoid; margin-bottom: 1rem;",
	}
	seen := map[string]int{}
	for _, r := range append(panelRulesNaming(rules, ".card-flow"), panelRulesNaming(rules, ".card-flow-col")...) {
		seen[r.selector]++
		w, ok := want[r.selector]
		switch {
		case !ok:
			findings = append(findings, fmt.Sprintf("a rule `%s` also styles the card flow", r.selector))
		case !slices.Equal(r.at, desk):
			findings = append(findings, fmt.Sprintf("`%s` sits in %v, want %v: the flow exists from the desk width only", r.selector, r.at, desk))
		case r.body != w:
			findings = append(findings, fmt.Sprintf("`%s` declares %q, want %q", r.selector, r.body, w))
		}
	}
	for sel := range want {
		// ".card-flow > :not(.card-flow-col)" names both classes, so it is collected twice
		n := 1
		if strings.Contains(sel, ".card-flow-col") && strings.HasPrefix(sel, ".card-flow ") {
			n = 2
		}
		if seen[sel] != n {
			findings = append(findings, fmt.Sprintf("%d `%s` rule(s), want 1", seen[sel]/n, sel))
		}
	}
	prose := panelRulesNaming(rules, ".panel-prose")
	if len(prose) != 1 || prose[0].selector != ".panel-prose" || prose[0].body != "max-width: 70rem;" {
		findings = append(findings, fmt.Sprintf("the prose measure is %d rule(s) %v, want one `.panel-prose { max-width: 70rem; }`", len(prose), prose))
	}

	// The templates: Account's cards and Odd shapes' middle flow, and nothing else does.
	flowBox := regexp.MustCompile(`class="card-flow ([^"]*)"`)
	flowCol := regexp.MustCompile(`class="card-flow-col ([^"]*)"`)
	for name, src := range files {
		boxes, cols := flowBox.FindAllStringSubmatch(src, -1), flowCol.FindAllStringSubmatch(src, -1)
		switch name {
		case "pages/account.templ":
			if len(boxes) != 1 || len(cols) != 0 {
				findings = append(findings, fmt.Sprintf("account.templ: %d flow box(es), %d column wrapper(s), want 1 and 0", len(boxes), len(cols)))
				continue
			}
			us := strings.Fields(boxes[0][1])
			if !slices.Contains(us, "desk:block") || slices.ContainsFunc(us, func(u string) bool { return strings.HasPrefix(u, "desk:grid-cols-") }) {
				findings = append(findings, "account.templ: the cards' box is not a block from the desk width ("+boxes[0][1]+"): the row grid is back")
			}
			if strings.Join(us[:3], " ") != "grid grid-cols-1 items-start" {
				findings = append(findings, "account.templ: the phone and tablet stack changed ("+boxes[0][1]+")")
			}
		case "pages/anomalies.templ":
			if len(boxes) != 1 || len(cols) != 2 {
				findings = append(findings, fmt.Sprintf("anomalies.templ: %d flow box(es), %d column wrapper(s), want 1 and 2", len(boxes), len(cols)))
				continue
			}
			if boxes[0][1] != "contents md:grid md:grid-cols-2 md:items-start md:gap-4 desk:block" {
				findings = append(findings, "anomalies.templ: the flow box is "+boxes[0][1]+": the phone's dissolve and the tablet's two columns stay, the desk is a block")
			}
			for _, c := range cols {
				if c[1] != "contents md:flex md:min-w-0 md:flex-col md:gap-4 desk:contents" {
					findings = append(findings, "anomalies.templ: a column wrapper is "+c[1]+": it dissolves on the phone and from the desk width")
				}
			}
		default:
			if len(boxes)+len(cols) > 0 {
				findings = append(findings, name+" uses the card flow: only Account and Odd shapes do")
			}
		}
	}
	return findings
}

func TestPanelWide_AccountAndOddShapesFlowDownColumns(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "web", "static", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	files := readPanelTemplates(t)
	for _, f := range readPanelCardFlow(css, files) {
		t.Error(f)
	}
	for _, c := range []struct{ what, file, from, to string }{
		{"two columns at most", "", ".card-flow { columns: 25rem 3; }", ".card-flow { columns: 25rem 2; }"},
		{"a column that may be narrower", "", ".card-flow { columns: 25rem 3; }", ".card-flow { columns: 3; }"},
		{"a card broken across columns", "", ".card-flow > :not(.card-flow-col) { break-inside: avoid; margin-bottom: 1.25rem; }", ".card-flow > :not(.card-flow-col) { margin-bottom: 1.25rem; }"},
		{"Odd's cards without their gap", "", ".card-flow-col > * { break-inside: avoid; margin-bottom: 1rem; }", ".card-flow-col > * { break-inside: avoid; }"},
		{"the flow on the tablet too", "", "  @screen desk {\n    .card-flow { columns: 25rem 3; }", "  @screen md {\n    .card-flow { columns: 25rem 3; }"},
		{"the prose measure widened", "", "  .panel-prose {\n    max-width: 70rem;", "  .panel-prose {\n    max-width: 90rem;"},
		{"Account's row grid back", "pages/account.templ", `class="card-flow grid grid-cols-1 items-start gap-5 desk:block"`, `class="card-flow grid grid-cols-1 items-start gap-5 desk:grid-cols-2"`},
		{"Account's flow gone", "pages/account.templ", `class="card-flow grid`, `class="grid`},
		{"Odd's tablet columns lost", "pages/anomalies.templ", `class="card-flow contents md:grid md:grid-cols-2`, `class="card-flow contents md:block md:grid-cols-2`},
		{"an Odd column wrapper kept at the desk", "pages/anomalies.templ", `md:gap-4 desk:contents">`, `md:gap-4">`},
		{"the flow on another page", "pages/reports.templ", `<div class="`, `<div class="card-flow `},
	} {
		mcss, mfiles := css, map[string]string{}
		for k, v := range files {
			mfiles[k] = v
		}
		if c.file == "" {
			mcss = strings.Replace(css, c.from, c.to, 1)
			if mcss == css {
				t.Fatalf("CONTROL %s: %q is not in input.css", c.what, c.from)
			}
		} else {
			mfiles[c.file] = strings.Replace(files[c.file], c.from, c.to, 1)
			if mfiles[c.file] == files[c.file] {
				t.Fatalf("CONTROL %s: %q is not in %s", c.what, c.from, c.file)
			}
		}
		if len(readPanelCardFlow(mcss, mfiles)) == 0 {
			t.Errorf("CONTROL: %s is not a finding; the reading is blind", c.what)
		}
	}
}

// panelZoomSteps are the frame's zoom steps: the window width each starts at and its factor, as
// input.css writes them. Each starts where the zoomed layout is no narrower than an unzoomed 1920.
var panelZoomSteps = map[string]string{"(min-width: 2400px)": "1.25", "(min-width: 3200px)": "1.5"}

// readPanelZoom is the reading of the zoom steps: one finding per decision broken.
func readPanelZoom(css string) []string {
	rules := readPanelCSSRules(css)
	var findings []string
	steps := map[string]int{}
	for _, r := range rules {
		if !strings.Contains(r.body, "zoom") {
			continue
		}
		media := ""
		if n := len(r.at); n > 0 && strings.HasPrefix(r.at[n-1], "@media ") {
			media = strings.TrimPrefix(r.at[n-1], "@media ")
		}
		z, ok := panelZoomSteps[media]
		if !ok || r.selector != ".panel-frame" {
			findings = append(findings, fmt.Sprintf("`%s` in %v declares a zoom: only the two steps zoom, and only the frame", r.selector, r.at))
			continue
		}
		steps[media]++
		if want := fmt.Sprintf("zoom: %s; min-height: calc(100vh / %s);", z, z); r.body != want {
			findings = append(findings, fmt.Sprintf("the frame at %s declares %q, want %q: a vh inside the zoom is %s screens", media, r.body, want, z))
		}
	}
	for media, z := range panelZoomSteps {
		if steps[media] != 1 {
			findings = append(findings, fmt.Sprintf("%d zoomed frame rule(s) at %s, want 1", steps[media], media))
		}
		var side []panelCSSRule
		for _, r := range rules {
			if r.selector == ".panel-frame .panel-side" && len(r.at) > 0 && r.at[len(r.at)-1] == "@media "+media {
				side = append(side, r)
			}
		}
		if want := fmt.Sprintf("height: calc(100vh / %s);", z); len(side) != 1 || side[0].body != want {
			findings = append(findings, fmt.Sprintf("the sidebar at %s is %v, want one `.panel-frame .panel-side { %s }`: its full height is a screen, not %s", media, side, want, z))
		}
		if w, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(media, "(min-width: "), "px)")); err != nil || w <= 1920 {
			findings = append(findings, "a zoom step starts at "+media+": at 1920 and below nothing is zoomed")
		}
	}
	// The override is needed only because the sidebar is a full-height sticky column.
	for _, r := range panelRulesNaming(rules, ".panel-side") {
		if r.selector == ".panel-side" {
			us := panelApplied(r.body)
			if !slices.Contains(us, "md:sticky") || !slices.Contains(us, "md:h-screen") {
				findings = append(findings, "`.panel-side` is no longer a full-height sticky column: revisit the zoom steps' height")
			}
		}
	}
	return findings
}

func TestPanelWide_AVeryWideScreenZoomsTheFrameInTwoSteps(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "web", "static", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	for _, f := range readPanelZoom(css) {
		t.Error(f)
	}
	for _, c := range []struct{ what, from, to string }{
		{"a step at 1920", "@media (min-width: 2400px) {\n    .panel-frame { zoom: 1.25;", "@media (min-width: 1920px) {\n    .panel-frame { zoom: 1.25;"},
		{"the frame's minimum left a zoomed screen", ".panel-frame { zoom: 1.5; min-height: calc(100vh / 1.5); }", ".panel-frame { zoom: 1.5; }"},
		{"the sidebar 1.25 screens tall", ".panel-frame .panel-side { height: calc(100vh / 1.25); }", ".panel-frame .panel-side { height: 100vh; }"},
		{"a height divided by the other step", ".panel-frame .panel-side { height: calc(100vh / 1.5); }", ".panel-frame .panel-side { height: calc(100vh / 1.25); }"},
		{"a third step", "  .panel-side {\n    @apply", "  @media (min-width: 5000px) { .panel-frame { zoom: 2; } }\n  .panel-side {\n    @apply"},
		{"a zoom on the well", ".panel-well:has(.panel-measure, .policy-section) {\n    max-width: 70rem;", ".panel-well:has(.panel-measure, .policy-section) {\n    zoom: 1.1; max-width: 70rem;"},
		{"the sidebar no longer sticky", "md:sticky md:top-0 md:h-screen", "md:top-0 md:h-screen"},
	} {
		mutated := strings.Replace(css, c.from, c.to, 1)
		if mutated == css {
			t.Fatalf("CONTROL %s: %q is not in input.css", c.what, c.from)
		}
		if len(readPanelZoom(mutated)) == 0 {
			t.Errorf("CONTROL: %s is not a finding; the reading is blind", c.what)
		}
	}
}
