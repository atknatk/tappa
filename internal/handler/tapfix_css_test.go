package handler

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/atknatk/tappa/web"
)

// TestCompiledCSS_TheDrawnCorrectionIsInkUnderlinedAndUnfilled pins how the public pages draw the
// tap screen's "Not right? Clock …" correction (.heroTapFix on the landing hero, .lp-prodTapFix on
// /product and /for-production): the M15 tap screen's own look -- an ink, underlined line with no
// ground and no accent (CLAUDE.md §9; the merge lens's DÜŞÜK, 2026-10-25: a mutation giving both
// classes `color: var(--green); background: var(--green)` stayed green everywhere). It reads the
// COMPILED stylesheet, the file the browser gets: every rule naming either class sets no
// background of any kind, every colour it sets is the ink token, and each class has a rule that
// sets that colour and the underline.
func TestCompiledCSS_TheDrawnCorrectionIsInkUnderlinedAndUnfilled(t *testing.T) {
	raw, err := fs.ReadFile(web.Static(), "css/app.css")
	if err != nil {
		t.Skipf("no compiled stylesheet to read (%v) — run `make css`. THIS IS NOT A PASS.", err)
	}
	css := string(raw)
	if !strings.Contains(css, ".lp{--ink:#152219") {
		t.Fatal("the public pages' ink token is not #152219 in the compiled stylesheet")
	}
	ruleRE := regexp.MustCompile(`([^{}]*)\{([^{}]*)\}`)
	colorRE := regexp.MustCompile(`(?:^|;)color:([^;]*)`)
	backgroundRE := regexp.MustCompile(`(?:^|;)background(?:-[a-z]+)?:`)
	for _, class := range []string{"heroTapFix", "lp-prodTapFix"} {
		sel := regexp.MustCompile(`\.` + regexp.QuoteMeta(class) + `\b`)
		inked, underlined := false, false
		for _, m := range ruleRE.FindAllStringSubmatch(css, -1) {
			if !sel.MatchString(m[1]) {
				continue
			}
			decls := m[2]
			if backgroundRE.MatchString(decls) {
				t.Errorf(".%s gets a ground: %s{%s} -- the correction is a line of text, never a button", class, strings.TrimSpace(m[1]), decls)
			}
			for _, c := range colorRE.FindAllStringSubmatch(decls, -1) {
				if strings.TrimSpace(c[1]) != "var(--ink)" {
					t.Errorf(".%s's colour is %q, want the ink token var(--ink) -- no accent, no status colour", class, c[1])
					continue
				}
				inked = true
			}
			if strings.Contains(decls, "text-decoration:underline") {
				underlined = true
			}
		}
		if !inked || !underlined {
			t.Errorf(".%s: inked %v, underlined %v in the compiled stylesheet; want both", class, inked, underlined)
		}
	}
}
