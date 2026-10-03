package brand

import "strconv"

// ThemeCSS is the whole body of the theme stylesheet for one accent: what
// GET /brand/theme/{HEX}.css answers (ADR 0023 §4). It returns Check's
// ErrAccentIllegible, and no body, for an accent Check refuses -- the serving side
// of ADR 0023 §3's "same function on both sides".
//
// The body is one rule on :root declaring three custom properties, each in the
// "R G B" decimal form the three Tailwind tokens read through
// `rgb(var(--…) / <alpha-value>)` (tailwind.config.js):
//
//	--brand-accent     the tap button's fill: the accent
//	--brand-on-accent  its label: Fill.Text, paper or ink (OnColor)
//	--brand-edge       its 2 px edge: ink when Fill.Edge, otherwise the word none
//
// ThemeCSS takes a Color, not the string the route was asked for: the body is this
// function's literals and the decimal digits of the Fill's bytes.
//
// WHY none WHEN THERE IS NO EDGE (ADR 0023 §4, WL-5 note). Today (WL-5) the rule in
// input.css that reads --brand-edge is .tap-button's inset box-shadow. none is not a
// colour, so with it that declaration is invalid at computed-value time and
// box-shadow takes its initial value, none: what the button had before the token
// existed (measured in Chrome 154, WL-5 card). The other value that draws no visible
// edge -- the accent's own colour -- changed the computed box-shadow and 36 pixels
// at the button's rounded corners (measured, WL-5 card).
func ThemeCSS(a Color) (string, error) {
	f, err := Check(a)
	if err != nil {
		return "", err
	}
	edge := themeNoEdge
	if f.Edge {
		edge = themeTriple(paletteColor(paletteInkHex))
	}
	return ":root{--brand-accent:" + themeTriple(f.Accent) +
		";--brand-on-accent:" + themeTriple(f.Text) +
		";--brand-edge:" + edge + "}", nil
}

// themeNoEdge is --brand-edge's value for a fill without an edge (ThemeCSS).
const themeNoEdge = "none"

// themeTriple is c in the form rgb() takes inside var(): "31 92 65".
func themeTriple(c Color) string {
	return strconv.Itoa(int(c.R)) + " " + strconv.Itoa(int(c.G)) + " " + strconv.Itoa(int(c.B))
}
