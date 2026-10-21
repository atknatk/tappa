package handler

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// THE PUBLIC SITE'S STYLESHEET (LP-01): the `.lp` block at the end of input.css, which
// the landing page and every public page after it draw with. Three properties are held
// here, all read from the SOURCE (input.css) rather than the compiled app.css, because
// the source is what a person edits and the minifier merges and reorders rules:
//
//   - every selector in the block is scoped under `.lp` (or `html:has(.lp …)`), and the
//     block sits outside every @layer — a bare selector there would reach the panel, the
//     tap screen and the sign-up wizard, which share the stylesheet;
//   - the one focus ring clears 3:1 against every ground it is drawn on, computed from
//     the tokens the block declares, not from numbers typed here;
//   - every class a visitor presses reserves the 44px floor.

// landingCSSMarker opens the block: the reference landing's header comment.
const landingCSSMarker = "THE PUBLIC LANDING PAGE (/)"

// landingCSSComponentsMarker is the LP-01 components section's header; the scans must
// see it, or they would pass over a block that lost the rules they exist for.
const landingCSSComponentsMarker = "THE PUBLIC SITE'S COMPONENTS"

// landingCSSBlock is input.css from the block's header comment to the end of the file,
// comments removed.
func landingCSSBlock(t *testing.T) string {
	t.Helper()
	css := repoFile(t, "web", "static", "css", "input.css")
	at := strings.Index(css, landingCSSMarker)
	if at < 0 {
		t.Fatalf("PREMISE: input.css has no %q header", landingCSSMarker)
	}
	start := strings.LastIndex(css[:at], "/*")
	if !strings.Contains(css[start:], landingCSSComponentsMarker) {
		t.Fatalf("PREMISE: the .lp block carries no %q section", landingCSSComponentsMarker)
	}
	// Outside @layer: every brace opened before the block is closed before it.
	before := cssCommentRE.ReplaceAllString(css[:start], "")
	if open := strings.Count(before, "{") - strings.Count(before, "}"); open != 0 {
		t.Fatalf("the .lp block starts inside %d open block(s) — inside an @layer it would be "+
			"tree-shaken against the template scan", open)
	}
	return cssCommentRE.ReplaceAllString(css[start:], "")
}

// lpCSSRule is one style rule: its selector list, its declarations, and whether it is a
// keyframe step (whose "selectors" are from/to/percentages, not selectors).
type lpCSSRule struct {
	selectors []string
	decls     string
	keyframe  bool
}

// flatCSSRules lists every style rule in css (comments already removed), descending into
// at-rule blocks. It is enough for this hand-written file: no braces inside strings.
func flatCSSRules(css string) []lpCSSRule {
	var rules []lpCSSRule
	var open []string
	start := 0
	for i, c := range css {
		switch c {
		case '{':
			open = append(open, strings.TrimSpace(css[start:i]))
			start = i + 1
		case '}':
			if len(open) == 0 {
				start = i + 1
				continue
			}
			prelude := open[len(open)-1]
			open = open[:len(open)-1]
			if !strings.HasPrefix(prelude, "@") {
				kf := false
				for _, p := range open {
					if strings.HasPrefix(p, "@keyframes") {
						kf = true
					}
				}
				rules = append(rules, lpCSSRule{selectors: splitCSSSelectors(prelude), decls: css[start:i], keyframe: kf})
			}
			start = i + 1
		}
	}
	return rules
}

// splitCSSSelectors splits a selector list on its top-level commas and collapses
// whitespace, so `.lp details.menu > summary` reads the same however it was indented.
func splitCSSSelectors(list string) []string {
	var out []string
	depth, from := 0, 0
	for i, c := range list {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.Join(strings.Fields(list[from:i]), " "))
				from = i + 1
			}
		}
	}
	return append(out, strings.Join(strings.Fields(list[from:]), " "))
}

// scopedUnderLp reports whether sel starts at the .lp root: `.lp` itself or a
// descendant/state of it, or `html:has(.lp …)`. `.lp-button` is NOT scoped — it is a
// different class that merely starts with the same letters.
func scopedUnderLp(sel string) bool {
	for _, root := range []string{".lp", "html:has(.lp"} {
		if !strings.HasPrefix(sel, root) {
			continue
		}
		rest := sel[len(root):]
		if rest == "" || strings.ContainsRune(" :.[>+~)", rune(rest[0])) {
			return true
		}
	}
	return false
}

// unscopedSelectors is every selector in css that does not start at the .lp root.
func unscopedSelectors(css string) []string {
	var out []string
	for _, r := range flatCSSRules(css) {
		if r.keyframe {
			continue
		}
		for _, s := range r.selectors {
			if !scopedUnderLp(s) {
				out = append(out, s)
			}
		}
	}
	return out
}

func TestLandingCSS_EverySelectorIsScopedUnderLp(t *testing.T) {
	t.Parallel()
	block := landingCSSBlock(t)
	rules := flatCSSRules(block)
	if len(rules) < 100 {
		t.Fatalf("PREMISE: the scan read %d rules from the .lp block; the parser lost it", len(rules))
	}
	for _, s := range unscopedSelectors(block) {
		t.Errorf("the public site's block carries %q, which does not start at .lp — it would "+
			"reach the panel, the tap screen and the wizard, which share this stylesheet", s)
	}
}

// TestLandingCSS_ScopeScanRefusesAnUnscopedSelector is the scan's negative control.
func TestLandingCSS_ScopeScanRefusesAnUnscopedSelector(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, css, want string }{
		{"a bare element", ".lp a { color: red; } nav { position: sticky; }", "nav"},
		{"a look-alike class", ".lp-button { min-height: 48px; }", ".lp-button"},
		{"inside @media", "@media (min-width: 768px) { .lp .x { } .menu { display: none; } }", ".menu"},
		{"second of a list", ".lp .a, .b { }", ".b"},
		{"html without .lp", "html { scroll-padding-top: 72px; }", "html"},
	} {
		got := unscopedSelectors(tc.css)
		if len(got) != 1 || got[0] != tc.want {
			t.Errorf("%s: the scan reported %q, want exactly %q", tc.name, got, tc.want)
		}
	}
	scoped := "@keyframes k { from { opacity: 0; } 50% { opacity: .5; } to { opacity: 1; } } " +
		".lp { } .lp :focus-visible { } .lp.x { } html:has(.lp) { } html:has(.lp .mbar) { } .lp > nav { }"
	if got := unscopedSelectors(scoped); len(got) != 0 {
		t.Errorf("the scan refused scoped selectors or keyframe steps: %q", got)
	}
}

var cssCustomPropRE = regexp.MustCompile(`--([a-z][a-z-]*)\s*:\s*([^;]+)`)

// rgbOfHex reads #RRGGBB.
func rgbOfHex(t *testing.T, hex string) [3]float64 {
	t.Helper()
	hex = strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(hex) != 6 {
		t.Fatalf("%q is not a #RRGGBB token", hex)
	}
	var out [3]float64
	for i := range out {
		v, err := strconv.ParseUint(hex[2*i:2*i+2], 16, 8)
		if err != nil {
			t.Fatalf("%q: %v", hex, err)
		}
		out[i] = float64(v)
	}
	return out
}

// landingCSSRulesFor is every rule in rules whose selector list names sel exactly.
func landingCSSRulesFor(rules []lpCSSRule, sel string) []lpCSSRule {
	var out []lpCSSRule
	for _, r := range rules {
		for _, s := range r.selectors {
			if s == sel {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// cssVarRE reads a value that is exactly one token, `var(--name)`.
var cssVarRE = regexp.MustCompile(`^var\(--([a-z][a-z-]*)\)$`)

// groundToken is the token sel paints its background with, or "".
func groundToken(rules []lpCSSRule, sel string) string {
	bgRE := regexp.MustCompile(`(?:^|;)\s*background\s*:\s*([^;]+)`)
	for _, r := range landingCSSRulesFor(rules, sel) {
		if m := bgRE.FindStringSubmatch(r.decls); m != nil {
			if v := cssVarRE.FindStringSubmatch(strings.TrimSpace(m[1])); v != nil {
				return v[1]
			}
		}
	}
	return ""
}

// TestLandingCSS_FocusRingClearsThreeToOneOnEveryGround computes the ring's contrast
// from the block's own tokens: ink on the light grounds, paper on every ground that
// switches it. The four pairs are the States-Public board's (14.32 / 16.17 / 7.73 /
// 16.17); the reference's saffron ring measured 2.32:1 on porcelain.
func TestLandingCSS_FocusRingClearsThreeToOneOnEveryGround(t *testing.T) {
	t.Parallel()
	rules := flatCSSRules(landingCSSBlock(t))

	tokens := map[string]string{}
	for _, r := range landingCSSRulesFor(rules, ".lp") {
		for _, m := range cssCustomPropRE.FindAllStringSubmatch(r.decls, -1) {
			tokens[m[1]] = strings.TrimSpace(m[2])
		}
	}
	color := func(name string) [3]float64 {
		v, ok := tokens[name]
		if !ok {
			t.Fatalf("the .lp root declares no --%s", name)
		}
		return rgbOfHex(t, v)
	}

	// The translucent tones are a token at an alpha; each channel triplet must be the
	// hex token it stands for, or "ink at 85%" would quietly be another colour.
	for _, name := range []string{"ink", "green", "porcelain"} {
		want := color(name)
		got := strings.Fields(tokens[name+"-ch"])
		if len(got) != 3 {
			t.Fatalf("--%s-ch is %q, want three channels", name, tokens[name+"-ch"])
		}
		for i, g := range got {
			if g != strconv.Itoa(int(want[i])) {
				t.Errorf("--%s-ch is %q, but --%s is %s", name, tokens[name+"-ch"], name, tokens[name])
				break
			}
		}
	}

	ring := landingCSSRulesFor(rules, ".lp :focus-visible")
	if len(ring) != 1 {
		t.Fatalf("want exactly one `.lp :focus-visible` rule, found %d", len(ring))
	}
	decls := strings.Join(strings.Fields(ring[0].decls), " ")
	for _, want := range []string{"outline: 2px solid var(--ring)", "outline-offset: 2px"} {
		if !strings.Contains(decls, want) {
			t.Errorf("the ring rule is {%s}; want %q", decls, want)
		}
	}
	rootRing := cssVarRE.FindStringSubmatch(tokens["ring"])
	if rootRing == nil {
		t.Fatalf("the .lp root's --ring is %q, want one token", tokens["ring"])
	}

	pair := func(fg, ground string) float64 { return contrastRatio(color(fg), color(ground)) }
	board := map[[2]string]string{
		{"ink", "porcelain"}: "14.32", {"ink", "paper"}: "16.17",
		{"paper", "green"}: "7.73", {"paper", "ink"}: "16.17",
	}
	checked := map[[2]string]bool{}
	check := func(where, fg, ground string) {
		got := pair(fg, ground)
		if got < 3 {
			t.Errorf("%s: the %s ring on %s is %.2f:1, below the 3:1 a focus indicator needs", where, fg, ground, got)
		}
		k := [2]string{fg, ground}
		checked[k] = true
		if want, ok := board[k]; ok && fmt.Sprintf("%.2f", got) != want {
			t.Errorf("%s: the %s ring on %s is %.2f:1; the board printed %s:1", where, fg, ground, got, want)
		}
	}

	// The light grounds keep the root's ring.
	if g := groundToken(rules, ".lp"); g != "porcelain" {
		t.Fatalf("PREMISE: the .lp root's ground is %q, want porcelain", g)
	}
	check(".lp", rootRing[1], "porcelain")
	for _, sel := range []string{".lp .band--paper", ".lp .band--porcelain"} {
		g := groundToken(rules, sel)
		if g == "" {
			t.Fatalf("%s paints no token ground", sel)
		}
		check(sel, rootRing[1], g)
	}

	// Every rule that switches the ring: the switch must be needed (the root's ring
	// fails on that ground) and must work.
	switched := map[string]bool{}
	for _, r := range rules {
		m := cssCustomPropRE.FindAllStringSubmatch(r.decls, -1)
		for _, d := range m {
			if d[1] != "ring" || (len(r.selectors) == 1 && r.selectors[0] == ".lp") {
				continue
			}
			v := cssVarRE.FindStringSubmatch(strings.TrimSpace(d[2]))
			if v == nil {
				t.Fatalf("%v sets --ring to %q, want one token", r.selectors, d[2])
			}
			for _, sel := range r.selectors {
				switched[sel] = true
				g := groundToken(rules, sel)
				if g == "" {
					t.Errorf("%s switches the ring but paints no token ground to measure it on", sel)
					continue
				}
				if pair(rootRing[1], g) >= 3 {
					t.Errorf("%s switches the ring although the root's %s ring already clears 3:1 on %s", sel, rootRing[1], g)
				}
				check(sel, v[1], g)
			}
		}
	}
	for _, sel := range []string{".lp .band--ink", ".lp .ondark"} {
		if !switched[sel] {
			t.Errorf("%s does not switch the ring; the root's ink ring would vanish on its ground", sel)
		}
	}
	for k, want := range board {
		if !checked[k] {
			t.Errorf("no ground in the block measured the board's %s-on-%s pair (%s:1)", k[0], k[1], want)
		}
	}
}

var cssMinHeightRE = regexp.MustCompile(`(?:^|;)\s*min-height\s*:\s*(\d+)px`)

// TestLandingCSS_EveryPressableClassReservesTheTouchFloor: each class a visitor
// presses reserves 44px in its own rule; the full-size buttons and the menu's links 48.
func TestLandingCSS_EveryPressableClassReservesTheTouchFloor(t *testing.T) {
	t.Parallel()
	rules := flatCSSRules(landingCSSBlock(t))
	for sel, floor := range map[string]int{
		".lp .lp-button":             48,
		".lp .hbtn":                  44,
		".lp .mbar .hbtn":            48,
		".lp .navlink":               44,
		".lp details.menu > summary": 44,
		".lp .mlink":                 48,
		".lp .more":                  44,
		".lp .flink":                 44,
		".lp .fcol a":                44,
		".lp details.qa > summary":   44,
	} {
		best := 0
		for _, r := range landingCSSRulesFor(rules, sel) {
			if m := cssMinHeightRE.FindStringSubmatch(r.decls); m != nil {
				n, err := strconv.Atoi(m[1])
				if err != nil {
					t.Fatalf("%s: min-height %q: %v", sel, m[1], err)
				}
				if n > best {
					best = n
				}
			}
		}
		if best < floor {
			t.Errorf("%s reserves min-height %dpx; a pressable class here reserves %dpx", sel, best, floor)
		}
	}
}
