// Package brand holds the rules a tenant's brand inputs must pass before a surface
// shows them. The inputs and the surfaces are ADR 0023's (§1, §2); the accent
// colour's rules are ADR 0023 §3, and an uploaded logo image's are ADR 0024.
//
// The accent colour (accent.go): its canonical spelling, WCAG 2.x relative
// luminance and contrast, the gate (Check), the label colour drawn on the accent
// (OnColor), the ink edge it needs on the porcelain page (Edge) and the nearest
// colour that passes when it does not (Suggest). The body of the theme stylesheet
// that carries an accepted accent to a page (theme.go, ThemeCSS) is built from
// Check's answer, ADR 0023 §4.
//
// WHY A PACKAGE OF ITS OWN. ADR 0023 §3 asks the same accent gate on two sides,
// the pattern CLAUDE.md §5 names for internal/netx: the side that STORES an accent
// and the side that SERVES it. The ADR gives the reason the serving side is not
// redundant: the gate is computed from the palette, so a palette change can move a
// colour that was stored under the old one out of the accepted set, and a
// write-side check does not revisit a row stored before it.
//
// THE PALETTE HERE IS A COPY. tailwind.config.js is where the nine colour tokens
// are defined, and go:embed does not reach it from this directory: the go:embed
// rules forbid '..' in a pattern. ADR 0023 §3 therefore requires a Go copy held
// equal to the file by a test; accent.go's palette comment names that test.
package brand
