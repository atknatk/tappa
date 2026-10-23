package handler

import (
	"fmt"
	"strings"
	"testing"

	"github.com/atknatk/tappa/web/templates/pages"
)

// THE DRAWN STATES OF EVERY PUBLIC CONTROL (LP-14, boards States-Public and
// M-States-Public): the board's matrix, row by row, as the rules of the .lp block that
// draw it. A filled cell names the rule and the declarations that make it look as drawn;
// an n/a cell names the rule that must NOT exist. Focus-visible is the one ring
// (`.lp :focus-visible`, whose contrast on every ground is
// TestLandingCSS_FocusRingClearsThreeToOneOnEveryGround's) plus, where a control has its own
// corner, the rule that rounds the ring to it. Read from the SOURCE, as the block's other
// tests are: the minifier merges selector lists and splits declarations.
//
// The phone board draws the same rows without hover (a finger has none); the cells below
// that a phone shows are the same rules.

// drawnCell is one filled cell: the rule's at-rule context ("" at the top level), its
// selector, and declarations it must carry.
type drawnCell struct {
	state, media, sel string
	decls             []string
}

// drawnRow is one row of the matrix: its filled cells and the selectors its n/a cells
// forbid.
type drawnRow struct {
	control string
	cells   []drawnCell
	na      []string
}

const (
	drawnDesk      = "@media (min-width: 1200px)"
	drawnRingRule  = ".lp :focus-visible"
	drawnDown      = "transform: translateY(1px)"
	drawnGreenEdge = "box-shadow: inset 0 0 0 2px var(--green)"
	drawnPaperWord = "color: var(--paper)"
	drawnInkGround = "background: var(--ink)"
	drawnThicker   = "text-decoration-thickness: 2px"
)

var drawnRing = drawnCell{"focus-visible", "", drawnRingRule, []string{"outline: 2px solid var(--ring)", "outline-offset: 2px"}}

// drawnMatrix is the board, in its order. The first thirteen rows are the LP-14 card's;
// the rest are the board's further public rows whose rules live in the .lp block (the
// legal switcher and sign-up's choices and fields are utility classes, held by
// TestPublicPages_UtilityControlsCarryTheirDrawnStates; sign-up's submit is the product's
// shared .btn, which this phase does not restyle).
var drawnMatrix = []drawnRow{
	{"Primary button", []drawnCell{
		{"default", "", ".lp .lp-button--green", []string{"background: var(--green)", drawnPaperWord, "box-shadow: 0 4px 14px rgb(var(--green-ch) / .22)"}},
		{"hover", "", ".lp .lp-button--green:hover", []string{drawnInkGround, drawnPaperWord}},
		drawnRing,
		{"pressed", "", ".lp .lp-button--green:active", []string{drawnInkGround, drawnPaperWord, drawnGreenEdge, drawnDown}},
		{"disabled", "", ".lp .lp-button--green:disabled", []string{"background: rgb(var(--ink-ch) / .12)", "color: rgb(var(--ink-ch) / .6)", "cursor: not-allowed"}},
	}, []string{".lp .lp-button--green[aria-current]"}},
	{"Ghost button", []drawnCell{
		{"default", "", ".lp .lp-button--ghost", []string{"border: 2px solid var(--ink)", "color: var(--ink)"}},
		{"hover", "", ".lp .lp-button--ghost:hover", []string{drawnInkGround, drawnPaperWord}},
		drawnRing,
		{"pressed", "", ".lp .lp-button--ghost:active", []string{drawnInkGround, drawnPaperWord, drawnGreenEdge, drawnDown}},
	}, []string{".lp .lp-button--ghost:disabled", ".lp .lp-button--ghost[aria-current]"}},
	{"Header button", []drawnCell{
		{"default", "", ".lp .hbtn", []string{"min-height: 44px", "background: var(--green)", drawnPaperWord}},
		{"hover", "", ".lp .hbtn:hover", []string{drawnInkGround, drawnPaperWord}},
		drawnRing,
		{"pressed", "", ".lp .hbtn:active", []string{drawnInkGround, drawnPaperWord, drawnGreenEdge, drawnDown}},
		// The header's own .hbtn rules outweigh .hbtn:active's shadow; this puts the edge back.
		{"pressed", "", ".lp .lp-head .hbtn:active", []string{drawnGreenEdge}},
	}, []string{".lp .hbtn:disabled", ".lp .hbtn[aria-current]"}},
	{"CTA band button", []drawnCell{
		{"default", "", ".lp .lp-button--paper", []string{"background: var(--paper)", "color: var(--green)"}},
		{"hover", "", ".lp .lp-button--paper:hover", []string{drawnInkGround, drawnPaperWord}},
		drawnRing,
		{"focus-visible", "", ".lp .ondark", []string{"--ring: var(--paper)"}},
		{"pressed", "", ".lp .lp-button--paper:active", []string{drawnInkGround, drawnPaperWord, "box-shadow: inset 0 0 0 2px var(--paper)", drawnDown}},
	}, []string{".lp .lp-button--paper:disabled", ".lp .lp-button--paper[aria-current]"}},
	{"Header nav link", []drawnCell{
		{"default", "", ".lp .navlink", []string{"min-height: 44px", "color: rgb(var(--ink-ch) / .85)", "text-decoration: none"}},
		{"hover", "", ".lp .navlink:hover", []string{"color: var(--ink)"}},
		drawnRing,
		{"focus-visible", "", ".lp .navlink:focus-visible", []string{"border-radius: 6px"}},
		{"pressed", "", ".lp .navlink:active", []string{"color: var(--ink)", "box-shadow: inset 0 -2px 0 var(--ink)"}},
		{"current", "", ".lp .navlink[aria-current]", []string{"color: var(--ink)", "font-weight: 700", "box-shadow: inset 0 -2px 0 var(--green)"}},
	}, []string{".lp .navlink:disabled"}},
	{"Sign in", []drawnCell{
		{"default", "", ".lp .lp-signin", []string{"color: var(--green)", "text-decoration: underline"}},
		{"hover", "", ".lp .lp-signin:hover", []string{"color: var(--ink)"}},
		drawnRing,
		{"focus-visible", "", ".lp .lp-signin:focus-visible", []string{"border-radius: 6px"}},
		{"pressed", "", ".lp .lp-signin:active", []string{"color: var(--ink)", drawnThicker}},
	}, []string{".lp .lp-signin:disabled", ".lp .lp-signin[aria-current]"}},
	{"Footer link", []drawnCell{
		{"default", "", ".lp .fcol a", []string{"color: rgb(var(--ink-ch) / .85)", "text-decoration: none"}},
		{"hover", "", ".lp .fcol a:hover", []string{"color: var(--ink)", "text-decoration: underline"}},
		drawnRing,
		{"focus-visible", "", ".lp .fcol a:focus-visible", []string{"border-radius: 6px"}},
		{"pressed", "", ".lp .fcol a:active", []string{"color: var(--ink)", "text-decoration: underline", drawnThicker}},
	}, []string{".lp .fcol a:disabled", ".lp .fcol a[aria-current]"}},
	{"Next-step link", []drawnCell{
		{"default", "", ".lp .more", []string{"color: var(--green)", "text-decoration: underline"}},
		{"hover", "", ".lp .more:hover", []string{"color: var(--ink)"}},
		drawnRing,
		{"focus-visible", "", ".lp .more:focus-visible", []string{"border-radius: 6px"}},
		{"pressed", "", ".lp .more:active", []string{"color: var(--ink)", drawnThicker}},
	}, []string{".lp .more:disabled", ".lp .more[aria-current]"}},
	{"FAQ item", []drawnCell{
		{"default", "", ".lp details.qa", []string{"background: var(--paper)", "border: 1px solid var(--line)"}},
		{"hover", "", ".lp details.qa:hover", []string{"border-color: var(--ink)"}},
		drawnRing,
		{"focus-visible", "", ".lp details.qa > summary", []string{"border-radius: 13px"}},
		{"pressed", "", ".lp details.qa:active", []string{"border-color: var(--ink)"}},
		{"open", "", ".lp details.qa[open] .pl", []string{"transform: rotate(45deg)"}},
	}, []string{".lp details.qa:disabled", ".lp details.qa > summary:disabled"}},
	{"Phone Menu", []drawnCell{
		{"default", "", ".lp details.menu > summary", []string{"min-height: 44px", "border: 1.5px solid var(--ink)", "background: transparent"}},
		{"hover", "", ".lp details.menu > summary:hover", []string{"background: rgb(var(--ink-ch) / .08)"}},
		drawnRing,
		{"pressed", "", ".lp details.menu > summary:active", []string{drawnInkGround, drawnPaperWord}},
		{"open", "", ".lp details.menu[open] > summary", []string{drawnInkGround, drawnPaperWord}},
		{"open", "", ".lp details.menu[open] .mclose", []string{"display: inline"}},
	}, []string{".lp details.menu > summary:disabled"}},
	{"Menu link", []drawnCell{
		{"default", "", ".lp .mlink", []string{"min-height: 48px", "color: var(--ink)"}},
		{"hover", "", ".lp .mlink:hover", []string{"background: rgb(var(--ink-ch) / .05)"}},
		drawnRing,
		{"focus-visible", "", ".lp .mlink:focus-visible", []string{"border-radius: 8px"}},
		{"pressed", "", ".lp .mlink:active", []string{"background: rgb(var(--ink-ch) / .08)"}},
		{"current", "", ".lp .mlink[aria-current]", []string{"color: var(--green)", "box-shadow: inset 3px 0 0 var(--green)"}},
	}, []string{".lp .mlink:disabled"}},
	// Touch: no hover. The bar's button keeps the header button's own states (.hbtn).
	{"Sticky phone bar", []drawnCell{
		{"default", "", ".lp .mbar", []string{"position: sticky", "bottom: 0"}},
		{"default", "", ".lp .mbar .hbtn", []string{"min-height: 48px"}},
		drawnRing,
		{"pressed", "", ".lp .hbtn:active", []string{drawnInkGround, drawnGreenEdge, drawnDown}},
	}, []string{".lp .mbar .hbtn:hover", ".lp .mbar :hover", ".lp .mbar .hbtn:disabled", ".lp .mbar .hbtn[aria-current]"}},
	{"Whole-card link", []drawnCell{
		{"default", "", ".lp .card", []string{"background: var(--paper)", "border: 1px solid var(--line)"}},
		{"hover", "", ".lp .fitcard:hover", []string{"border-color: var(--ink)"}},
		drawnRing,
		{"focus-visible", "", ".lp .fitcard:focus-visible", []string{"border-radius: 18px"}},
		{"pressed", "", ".lp .fitcard:active", []string{"border-color: var(--ink)", "background: var(--porcelain)"}},
	}, []string{".lp .fitcard:disabled", ".lp .fitcard[aria-current]"}},

	{"Index entry (the slip, from the desk width)", []drawnCell{
		{"hover", drawnDesk, ".lp .lp-prodIndexLink:hover", []string{"color: var(--green)"}},
		drawnRing,
		{"focus-visible", drawnDesk, ".lp .lp-prodIndexLink:focus-visible", []string{"border-radius: 6px"}},
		{"pressed", drawnDesk, ".lp .lp-prodIndexLink:active", []string{"color: var(--green)", "background: none", "text-decoration: underline"}},
	}, []string{".lp .lp-prodIndexLink:disabled", ".lp .lp-prodIndexLink[aria-current]"}},
	{"Index chip (tablet and phone)", []drawnCell{
		{"default", "", ".lp .lp-prodIndexLink", []string{"border: 1px solid var(--line)", "background: var(--paper)"}},
		{"hover", "", ".lp .lp-prodIndexLink:hover", []string{"border-color: var(--ink)"}},
		drawnRing,
		{"focus-visible", "", ".lp .lp-prodIndexLink:focus-visible", []string{"border-radius: 999px"}},
		{"pressed", "", ".lp .lp-prodIndexLink:active", []string{"border-color: var(--ink)", "background: var(--green-lite)"}},
	}, nil},
	{"Home link (lockup)", []drawnCell{
		{"default", "", ".lp .lp-mark", []string{"min-height: 44px", "color: var(--ink)"}},
		{"hover", "", ".lp .lp-mark:hover .lp-markName", []string{"color: var(--green)"}},
		drawnRing,
		{"focus-visible", "", ".lp .lp-mark:focus-visible", []string{"border-radius: 6px"}},
		{"pressed", "", ".lp .lp-mark:active .lp-markName", []string{"color: var(--green)", "text-decoration: underline"}},
	}, []string{".lp .lp-mark:disabled", ".lp .lp-mark[aria-current]"}},
	{"Legal link (.flink)", []drawnCell{
		{"default", "", ".lp .flink", []string{"color: rgb(var(--ink-ch) / .7)", "text-decoration: underline"}},
		{"hover", "", ".lp .flink:hover", []string{"color: var(--ink)"}},
		drawnRing,
		{"focus-visible", "", ".lp .flink:focus-visible", []string{"border-radius: 6px"}},
		{"pressed", "", ".lp .flink:active", []string{"color: var(--ink)", drawnThicker}},
	}, []string{".lp .flink:disabled", ".lp .flink[aria-current]"}},
	{"Demo dialog close", []drawnCell{
		{"default", "", ".lp .demoClose", []string{"width: 44px", "height: 44px", "border: 2px solid var(--ink)"}},
		{"hover", "", ".lp .demoClose:hover", []string{drawnInkGround, drawnPaperWord}},
		drawnRing,
		{"pressed", "", ".lp .demoClose:active", []string{drawnInkGround, drawnPaperWord, drawnDown}},
	}, []string{".lp .demoClose:disabled"}},
}

// drawnNormDecl collapses a declaration's whitespace so "a:  b" and "a: b" compare equal.
func drawnNormDecl(d string) string {
	k, v, _ := strings.Cut(d, ":")
	return strings.TrimSpace(k) + ": " + strings.Join(strings.Fields(v), " ")
}

// drawnStateProblems says how css (the .lp block, comments removed) departs from the
// matrix: a filled cell without its rule or a declaration of it, a rule for an n/a cell,
// a visited-link rule, or a disabled rule anywhere but the form submit's green button.
func drawnStateProblems(css string, matrix []drawnRow) []string {
	rules := cssRules(css)
	declsOf := func(media, sel string) (map[string]bool, bool) {
		got, found := map[string]bool{}, false
		for _, r := range rules {
			if r.media != media || !drawnHasSelector(r.selectors, sel) {
				continue
			}
			found = true
			for _, d := range strings.Split(r.decls, ";") {
				if strings.TrimSpace(d) != "" {
					got[drawnNormDecl(d)] = true
				}
			}
		}
		return got, found
	}
	var out []string
	for _, row := range matrix {
		for _, c := range row.cells {
			got, found := declsOf(c.media, c.sel)
			if !found {
				out = append(out, fmt.Sprintf("%s, %s: no rule %q%s", row.control, c.state, c.sel, drawnInMedia(c.media)))
				continue
			}
			for _, d := range c.decls {
				if !got[drawnNormDecl(d)] {
					out = append(out, fmt.Sprintf("%s, %s: %q%s does not declare %q", row.control, c.state, c.sel, drawnInMedia(c.media), d))
				}
			}
		}
		for _, sel := range row.na {
			for _, r := range rules {
				if drawnHasSelector(r.selectors, sel) {
					out = append(out, fmt.Sprintf("%s: the board's cell is n/a, but %q%s is a rule", row.control, sel, drawnInMedia(r.media)))
				}
			}
		}
	}
	for _, r := range rules {
		for _, s := range splitCSSSelectors(r.selectors) {
			if strings.Contains(s, ":visited") {
				out = append(out, fmt.Sprintf("%q colours a visited link; the board keeps the default (README §6)", s))
			}
			if strings.Contains(s, ":disabled") && s != ".lp .lp-button--green:disabled" {
				out = append(out, fmt.Sprintf("%q draws a disabled state; only a form submit (the green button) has one", s))
			}
		}
	}
	return out
}

// drawnHasSelector reports whether the selector list carries sel, whitespace collapsed.
func drawnHasSelector(list, sel string) bool {
	for _, s := range splitCSSSelectors(list) {
		if s == sel {
			return true
		}
	}
	return false
}

func drawnInMedia(media string) string {
	if media == "" {
		return ""
	}
	return " in " + media
}

// TestLandingCSS_EveryDrawnStateHasItsRule (LP-14) holds the .lp block to the
// States-Public matrix: every filled cell is a rule carrying the drawn declarations, no
// n/a cell has one, no link has a visited colour, and only the form submit is ever
// disabled. The pano-to-rule table of the LP-14 report is drawnMatrix.
func TestLandingCSS_EveryDrawnStateHasItsRule(t *testing.T) {
	t.Parallel()
	for _, p := range drawnStateProblems(landingCSSBlock(t), drawnMatrix) {
		t.Error(p)
	}
}

// TestLandingCSS_DrawnStateScanRefusesAMissingOrAnExtraState is the scan's control: the
// header button's pressed edge removed (the defect LP-14 measured), a pressed declaration
// changed, a rule written for an n/a cell, a visited colour and a second disabled state
// are each refused.
func TestLandingCSS_DrawnStateScanRefusesAMissingOrAnExtraState(t *testing.T) {
	t.Parallel()
	block := landingCSSBlock(t)
	if p := drawnStateProblems(block, drawnMatrix); len(p) != 0 {
		t.Fatalf("PREMISE: the block itself is not clean: %v", p)
	}
	edge := ".lp .lp-head .hbtn:active { box-shadow: inset 0 0 0 2px var(--green); }"
	if !strings.Contains(block, edge) {
		t.Fatalf("PREMISE: the control cannot find %q", edge)
	}
	for name, css := range map[string]string{
		"the header button's pressed edge removed": strings.Replace(block, edge, "", 1),
		"the chip pressed without its fill": strings.Replace(block,
			".lp .lp-prodIndexLink:active { border-color: var(--ink); background: var(--green-lite); }",
			".lp .lp-prodIndexLink:active { border-color: var(--ink); }", 1),
		"a disabled ghost button":      block + "\n.lp .lp-button--ghost:disabled { opacity: .5; }",
		"a footer link marked current": block + "\n.lp .fcol a[aria-current] { font-weight: 700; }",
		"a visited colour":             block + "\n.lp a:visited { color: var(--ink); }",
		"a disabled next-step link":    block + "\n.lp .lp-x .more:disabled { color: var(--line); }",
	} {
		if css == block {
			t.Fatalf("CONTROL %q: the substitution did not apply", name)
		}
		if len(drawnStateProblems(css, drawnMatrix)) == 0 {
			t.Errorf("CONTROL: %s passed", name)
		}
	}
}

// TestPublicPages_UtilityControlsCarryTheirDrawnStates (LP-14): the board's rows whose
// control is drawn in utility classes outside the .lp roots — the legal document switcher,
// sign-up's two kinds of choice and its fields — carry the drawn hover and pressed states
// (the choices) and the public ring (the fields), and no
// public page writes a visited variant. CONTROL: the switcher's class list without its
// pressed state is refused.
func TestPublicPages_UtilityControlsCarryTheirDrawnStates(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	type want struct{ path, open string }
	switcher := `<nav aria-label="Legal documents"`
	problems := func(body string, page want, classes []string) []string {
		var out []string
		seg := body
		if page.open != "" {
			seg = between(body, page.open, "</nav>")
		}
		if seg == "" {
			return []string{fmt.Sprintf("GET %s: PREMISE: no %q", page.path, page.open)}
		}
		for _, c := range classes {
			if !strings.Contains(seg, c) {
				out = append(out, fmt.Sprintf("GET %s: no control carries %q", page.path, c))
			}
		}
		return out
	}
	legalBody := mustFetchMarketing(t, r, pages.LegalPages[0].Path)
	legalOpen := want{pages.LegalPages[0].Path, switcher}
	legalStates := []string{
		// a document not being read: hover inks the edge, pressed adds the paper ground
		"border border-line px-3 py-2.5 hover:border-ink active:border-ink active:bg-paper",
		// the one being read: paper with a 2 px ink edge
		"border-2 border-ink bg-paper",
	}
	for _, p := range problems(legalBody, legalOpen, legalStates) {
		t.Error(p)
	}
	signupBody := mustFetchMarketing(t, r, signupPath)
	for _, p := range problems(signupBody, want{signupPath, ""}, []string{
		// the business-type chip and the place-count card: hover inks the edge, pressed
		// fills it green-lite; checked stays the green edge and tint they already had
		"border-line bg-paper px-3.5 font-mono text-xs uppercase tracking-[0.1em] text-ink hover:border-ink active:border-ink active:bg-green-lite",
		"border-line bg-paper p-4 hover:border-ink active:border-ink active:bg-green-lite",
		// a field: the public site's ring in place of the panel's green halo (1.97:1 on paper)
		"field-input field-input--mono focus:ring-0 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink",
	}) {
		t.Error(p)
	}
	for _, u := range append(publicChromeURLs(), "/no-such-page") {
		if body := fetchMarketing(t, r, u, nil).Body.String(); strings.Contains(body, "visited:") {
			t.Errorf("GET %s writes a visited variant; the board keeps a visited link's colour", u)
		}
	}
	cut := strings.Replace(legalBody, "hover:border-ink active:border-ink active:bg-paper", "hover:border-ink", -1)
	if cut == legalBody || len(problems(cut, legalOpen, legalStates)) == 0 {
		t.Error("CONTROL: a switcher without its pressed state passed")
	}
}
