package handler

import (
	"fmt"
	"html"
	"regexp"
	"slices"
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
// legal switcher is utility classes, held by
// TestPublicPages_UtilityControlsCarryTheirDrawnStates; sign-up's choices, fields and
// submit are constants of signup.templ, held by
// TestSignupSource_EveryFieldAndButtonCarriesItsStates; the ring of every control in those
// bodies is PublicBodyRing, held by TestPublicPages_UtilityBodiesDrawTheOneRing).
//
// THE PRIMARY BUTTON'S DISABLED CELL IS UNREACHABLE. The board draws it on a form submit
// only; the public site's one form is sign-up's, whose submit is the product's .btn, and
// with no script on that page it is never disabled. So no rule draws a disabled state,
// and the .lp block may declare none (drawnStateProblems).
var drawnMatrix = []drawnRow{
	{"Primary button", []drawnCell{
		{"default", "", ".lp .lp-button--green", []string{"background: var(--green)", drawnPaperWord, "box-shadow: 0 4px 14px rgb(var(--green-ch) / .22)"}},
		{"hover", "", ".lp .lp-button--green:hover", []string{drawnInkGround, drawnPaperWord}},
		drawnRing,
		{"pressed", "", ".lp .lp-button--green:active", []string{drawnInkGround, drawnPaperWord, drawnGreenEdge, drawnDown}},
	}, []string{".lp .lp-button--green:disabled", ".lp .lp-button--green[aria-current]"}},
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
// a visited-link rule, or any disabled rule (the board's one disabled cell is unreachable).
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
			if strings.Contains(s, ":disabled") {
				out = append(out, fmt.Sprintf("%q draws a disabled state; the board draws one only on a form submit, and the public site's one submit is never disabled", s))
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

// TestPublicPages_UtilityControlsCarryTheirDrawnStates (LP-14): the legal document
// switcher, drawn in utility classes outside the .lp roots, carries the drawn hover,
// pressed and current states, and no public page writes a visited variant. CONTROL: the
// switcher's class list without its pressed state is refused.
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

// utilityBodyClass is the class list of the first element inside <main> — the element a
// page with its own roots (sign-up, the legal documents, the guide) draws its body in —
// and of the element right inside that one, unescaped.
func utilityBodyClass(body string) (outer, inner string) {
	main := between(body, `<main id="main">`, "</main>")
	ms := htmlClassRE.FindAllStringSubmatch(main, 2)
	if len(ms) < 2 {
		return "", ""
	}
	return html.UnescapeString(ms[0][1]), html.UnescapeString(ms[1][1])
}

// TestPublicPages_UtilityBodiesDrawTheOneRing (LP-14): every page whose body is written
// in utility classes puts pages.PublicBodyRing on the element that holds the body, so
// every control inside it — the legal switcher, sign-up's fields, choices and buttons, the
// guide's converted links, code blocks and tables — draws the public site's one ring
// (2 px ink, 2 px out) instead of the browser's; and the ring is the .lp block's ring in
// utilities. CONTROL: a body without it, and a ring of another width, are refused.
func TestPublicPages_UtilityBodiesDrawTheOneRing(t *testing.T) {
	t.Parallel()
	ringProblems := func(ring string) []string {
		var out []string
		for _, want := range []string{"[&_:focus-visible]:outline", "[&_:focus-visible]:outline-2",
			"[&_:focus-visible]:outline-offset-2", "[&_:focus-visible]:outline-ink"} {
			if !slices.Contains(strings.Fields(ring), want) {
				out = append(out, fmt.Sprintf("the body ring %q lacks %q", ring, want))
			}
		}
		return out
	}
	for _, p := range ringProblems(pages.PublicBodyRing) {
		t.Error(p)
	}
	got, found := map[string]bool{}, false
	for _, r := range cssRules(landingCSSBlock(t)) {
		if r.media == "" && drawnHasSelector(r.selectors, drawnRingRule) {
			found = true
			for _, d := range strings.Split(r.decls, ";") {
				got[drawnNormDecl(d)] = true
			}
		}
	}
	if !found || !got["outline: 2px solid var(--ring)"] || !got["outline-offset: 2px"] {
		t.Errorf("PREMISE: %q is not the 2 px ring 2 px out that PublicBodyRing spells", drawnRingRule)
	}
	r := marketingRouter(t)
	bodyProblem := func(u, body string) string {
		outer, _ := utilityBodyClass(body)
		if !strings.Contains(outer, pages.PublicBodyRing) {
			return fmt.Sprintf("GET %s: the body (%q) does not carry pages.PublicBodyRing", u, outer)
		}
		return ""
	}
	var legal string
	for _, u := range append([]string{signupPath}, chromeOnlyURLs()...) {
		body := mustFetchMarketing(t, r, u)
		if u == pages.LegalPages[0].Path {
			legal = body
		}
		if p := bodyProblem(u, body); p != "" {
			t.Error(p)
		}
	}
	if bodyProblem("cut", strings.Replace(legal, html.EscapeString(pages.PublicBodyRing), "", 1)) == "" {
		t.Error("CONTROL: a body without the ring passed")
	}
	if len(ringProblems(strings.Replace(pages.PublicBodyRing, "outline-2", "outline-4", 1))) == 0 {
		t.Error("CONTROL: a 4 px ring passed")
	}
}

// signupSourceProblems says which field or primary button of signup.templ's source does
// not carry its states constant, and which constant lacks a drawn state. Read from the
// source because three of the wizard's screens are reached only by posting the one before.
func signupSourceProblems(src string) []string {
	var out []string
	for i, line := range strings.Split(src, "\n") {
		if strings.Contains(line, `"field-input`) && !strings.Contains(line, "+ signupFieldRing") {
			out = append(out, fmt.Sprintf("signup.templ:%d: a field without signupFieldRing", i+1))
		}
		if strings.Contains(line, "btn btn--primary") && !strings.Contains(line, "+ signupSubmitStates") {
			out = append(out, fmt.Sprintf("signup.templ:%d: a primary button without signupSubmitStates", i+1))
		}
	}
	consts := map[string][]string{
		"signupFieldRing":    {"focus:ring-0", "hover:[&:not([aria-invalid=true])]:border-ink", "focus:[&:not([aria-invalid=true])]:border-ink"},
		"signupSubmitStates": {"focus:ring-0", "active:bg-ink", "active:shadow-[inset_0_0_0_2px_theme(colors.tappa-green)]", "active:translate-y-px"},
		"signupChipClass": {"hover:border-ink", "active:border-ink", "active:bg-green-lite", "peer-focus-visible:outline",
			"peer-focus-visible:outline-2", "peer-focus-visible:outline-offset-2", "peer-focus-visible:outline-ink"},
		"signupPlaceCardClass": {"hover:border-ink", "active:border-ink", "active:bg-green-lite", "peer-focus-visible:outline",
			"peer-focus-visible:outline-2", "peer-focus-visible:outline-offset-2", "peer-focus-visible:outline-ink"},
	}
	for name, wants := range consts {
		at := strings.Index(src, "const "+name+" = ")
		if at < 0 {
			out = append(out, "signup.templ declares no "+name)
			continue
		}
		decl := src[at:]
		decl = decl[:strings.Index(decl, "\n\n")]
		value := strings.Join(regexp.MustCompile(`"([^"]*)"`).FindAllString(decl, -1), "")
		value = strings.ReplaceAll(value, `"`, "")
		for _, w := range wants {
			if !slices.Contains(strings.Fields(value), w) {
				out = append(out, fmt.Sprintf("%s lacks %q", name, w))
			}
		}
	}
	return out
}

// TestSignupSource_EveryFieldAndButtonCarriesItsStates (LP-14): every field of the
// wizard's four screens carries signupFieldRing, every primary button signupSubmitStates
// (pressed as the board draws the green button; no disabled state, the cell being
// unreachable), and the chip and place-card constants their hover, pressed and ring.
// CONTROL: the password field without its constant, and a submit constant without its
// pressed fill, are refused.
func TestSignupSource_EveryFieldAndButtonCarriesItsStates(t *testing.T) {
	t.Parallel()
	src := repoFile(t, "web", "templates", "pages", "signup.templ")
	for _, p := range signupSourceProblems(src) {
		t.Error(p)
	}
	password := `autocomplete="new-password"`
	at := strings.Index(src, password)
	if at < 0 {
		t.Fatal("PREMISE: no password field in signup.templ")
	}
	field := `class={ "field-input field-input--mono " + signupFieldRing }`
	next := strings.Index(src[at:], field)
	if next < 0 {
		t.Fatal("PREMISE: the password field's class is not where the control expects it")
	}
	cut := src[:at+next] + `class="field-input field-input--mono"` + src[at+next+len(field):]
	if len(signupSourceProblems(cut)) == 0 {
		t.Error("CONTROL: the password field without signupFieldRing passed")
	}
	if len(signupSourceProblems(strings.Replace(src, "active:bg-ink ", "", 1))) == 0 {
		t.Error("CONTROL: a submit without its pressed fill passed")
	}
}
