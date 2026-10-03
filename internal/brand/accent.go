package brand

import (
	"errors"
	"math"
)

// Color is one 24-bit sRGB colour. Each value has exactly one canonical spelling,
// Hex: six upper-case hexadecimal digits and no '#'. That is the form the
// tenant_branding.accent CHECK requires (ADR 0023 §1, `^[0-9A-F]{6}$`) and the
// form the theme route carries in its path (§4).
type Color struct{ R, G, B uint8 }

// Hex is the canonical spelling: "1F5C41" for tappa-green.
func (c Color) Hex() string {
	const digits = "0123456789ABCDEF"
	b := [6]byte{
		digits[c.R>>4], digits[c.R&0x0F],
		digits[c.G>>4], digits[c.G&0x0F],
		digits[c.B>>4], digits[c.B&0x0F],
	}
	return string(b[:])
}

var (
	// ErrAccentSyntax is a string that is not a colour in the spelling the parser
	// was asked for.
	ErrAccentSyntax = errors.New("brand: accent is not six hexadecimal digits")
	// ErrAccentIllegible is Check's refusal: on this accent neither paper nor ink
	// text reaches 4.5:1 (ADR 0023 §3).
	ErrAccentIllegible = errors.New("brand: neither paper nor ink text reaches 4.5:1 on this accent")
)

// ParseAccent accepts the canonical spelling — six bytes, each 0-9 or A-F — and
// returns ErrAccentSyntax otherwise. "1f5c41", "#1F5C41" and " 1F5C41" are
// refusals, not alternative spellings, so one colour has one accepted string
// (TestAccent_ParseAcceptsOneSpellingPerColour lists the refusals it measures).
// This is the parser for a value that is already supposed to be canonical (a
// stored row, the theme route's path); a form's input goes through
// NormalizeAccent.
func ParseAccent(s string) (Color, error) {
	return accentDecode(s, false)
}

// NormalizeAccent reads what an editor submits and returns the colour, whose Hex is
// then the canonical spelling. It admits exactly two differences from canonical:
// one optional leading '#', and lower-case letters. Both are what a browser's
// <input type="color"> sends ("#e0457b" — HTML's "valid lowercase simple colour").
// Other strings — spaces, a second '#', three digits, "0x" — are ErrAccentSyntax
// (TestAccent_NormalizeTakesWhatTheColourInputSends lists the ones it measures).
func NormalizeAccent(s string) (Color, error) {
	if len(s) == 7 && s[0] == '#' {
		s = s[1:]
	}
	return accentDecode(s, true)
}

func accentDecode(s string, lowerOK bool) (Color, error) {
	if len(s) != 6 {
		return Color{}, ErrAccentSyntax
	}
	var v [6]uint8
	for i := 0; i < len(s); i++ {
		d, ok := accentHexDigit(s[i], lowerOK)
		if !ok {
			return Color{}, ErrAccentSyntax
		}
		v[i] = d
	}
	return Color{R: v[0]<<4 | v[1], G: v[2]<<4 | v[3], B: v[4]<<4 | v[5]}, nil
}

func accentHexDigit(b byte, lowerOK bool) (uint8, bool) {
	switch {
	case '0' <= b && b <= '9':
		return b - '0', true
	case 'A' <= b && b <= 'F':
		return b - 'A' + 10, true
	case lowerOK && 'a' <= b && b <= 'f':
		return b - 'a' + 10, true
	}
	return 0, false
}

// The palette: a copy of the nine colour tokens in tailwind.config.js (doc.go says
// why it is a copy). TestPalette_TheGoCopyEqualsTailwindConfig reads that file and
// compares its nine tokens with paletteTokens, in both directions.
//
// The values are untyped constants rather than variables: a constant is not
// assignable, so the colours the gate is computed from are fixed when the package
// compiles. paletteTokens is built from the SAME constants the gate reads, so the
// test that compares the table is comparing the gate's inputs, not a parallel list.
const (
	paletteInkHex         = 0x152219
	palettePorcelainHex   = 0xEDF0EA
	palettePaperHex       = 0xFFFDF4
	paletteTappaGreenHex  = 0x1F5C41
	paletteGreenLiteHex   = 0xE1EDE6
	paletteSaffronHex     = 0xD98E2B
	paletteSaffronLiteHex = 0xF7EBD6
	paletteTomatoHex      = 0xBE3D2A
	paletteLineHex        = 0xC9D2C8
)

// paletteTokens is each token under the name tailwind.config.js gives it.
var paletteTokens = [...]struct {
	name string
	hex  uint32
}{
	{"ink", paletteInkHex},
	{"porcelain", palettePorcelainHex},
	{"paper", palettePaperHex},
	{"tappa-green", paletteTappaGreenHex},
	{"green-lite", paletteGreenLiteHex},
	{"saffron", paletteSaffronHex},
	{"saffron-lite", paletteSaffronLiteHex},
	{"tomato", paletteTomatoHex},
	{"line", paletteLineHex},
}

func paletteColor(hex uint32) Color {
	return Color{R: uint8(hex >> 16), G: uint8(hex >> 8), B: uint8(hex)}
}

// The two WCAG thresholds the gate applies (ADR 0023 §3). They are thresholds on a
// CONTRAST RATIO, from the standard, and the functions below apply them to the
// ratio directly. The luminance boundaries of ADR 0023 §3's table follow from these
// thresholds and the palette constants; this file does not compute them. The tests
// derive them from tailwind.config.js and compare (accent_test.go).
const (
	// accentTextMinimum: WCAG 1.4.3, normal text. The tap button's label is 20 px at
	// weight 400, which is not "large text" (24 px, or ~18.66 px bold), so 3:1 does
	// not apply to it.
	accentTextMinimum = 4.5
	// accentEdgeMinimum: WCAG 1.4.11, the button's boundary against the porcelain
	// page it sits on.
	accentEdgeMinimum = 3.0
)

// accentLinear is WCAG 2.x's linearised channel, c = v/255 and
// c <= 0.04045 ? c/12.92 : ((c+0.055)/1.055)^2.4, for each of the 256 values of v.
// It is a table because the gate is asked about one 8-bit channel at a time
// and Suggest asks it about ~40 colours per call; the values are the formula's.
//
// The 0.04045 threshold has a known equivalent spelling for 8-bit input: WCAG 2.0
// printed 0.03928, and 10/255 = 0.0392 < both < 11/255 = 0.0431, so the two
// thresholds put the same values (0..10) on the linear branch.
var accentLinear = accentLinearize()

func accentLinearize() [256]float64 {
	var t [256]float64
	for v := range t {
		c := float64(v) / 255
		if c <= 0.04045 {
			t[v] = c / 12.92
		} else {
			t[v] = math.Pow((c+0.055)/1.055, 2.4)
		}
	}
	return t
}

// accentLuminance is WCAG 2.x relative luminance, L = 0.2126 R + 0.7152 G + 0.0722 B
// over the linearised channels.
func accentLuminance(c Color) float64 {
	return 0.2126*accentLinear[c.R] + 0.7152*accentLinear[c.G] + 0.0722*accentLinear[c.B]
}

// contrastRatio is (L1 + 0.05) / (L2 + 0.05) with L1 the lighter of the two.
func contrastRatio(a, b Color) float64 {
	la, lb := accentLuminance(a), accentLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// OnColor is the colour of the text drawn on the accent: paper or ink, whichever
// has the higher contrast with it. A tie goes to paper. The two are equal at one
// luminance, the crossover of ADR 0023 §3, which lies inside the band Check
// refuses; so the tie rule is not what decides the label of an accepted accent.
func OnColor(a Color) Color {
	paper, ink := paletteColor(palettePaperHex), paletteColor(paletteInkHex)
	if contrastRatio(paper, a) >= contrastRatio(ink, a) {
		return paper
	}
	return ink
}

// Edge reports whether a button filled with the accent needs a 2 px ink edge to be
// told apart from the porcelain page under it: the accent's contrast with porcelain
// is below 3:1 (WCAG 1.4.11; ink on porcelain is 14.32:1).
func Edge(a Color) bool {
	return contrastRatio(a, paletteColor(palettePorcelainHex)) < accentEdgeMinimum
}

// Fill is what Check accepted: the three things a surface needs to paint the tap
// button in the accent (ADR 0023 §4's --brand-accent, --brand-on-accent and
// --brand-edge).
type Fill struct {
	Accent Color // the button's background
	Text   Color // its label: OnColor(Accent), paper or ink
	Edge   bool  // Edge(Accent): draw the 2 px ink edge
}

// Check is the accent gate (ADR 0023 §3): the better of paper and ink text on the
// accent must reach 4.5:1, or the accent is refused with ErrAccentIllegible. The
// same function decides on the side that stores an accent and on the side that
// serves one (doc.go).
func Check(a Color) (Fill, error) {
	text := OnColor(a)
	if contrastRatio(text, a) < accentTextMinimum {
		return Fill{}, ErrAccentIllegible
	}
	return Fill{Accent: a, Text: text, Edge: Edge(a)}, nil
}

// Suggest returns the colour the editor offers in place of a refused accent: the
// same hue at a lower lightness, as little lower as passing Check allows (ADR 0023
// §3: same hue, lightness lowered, the nearest colour that passes; skill
// tappa-brand: a darkened version of the same hue). An accent that already passes
// is returned unchanged.
//
// "Same hue at a lower lightness" is read in HSL: hue and saturation are held and
// lightness goes down, which is Sass's darken(). Each channel is non-decreasing in
// HSL lightness at fixed hue and saturation (its derivative is
// 1 -+ 2(c - l)/rho(l) >= 0, because |c - l| <= (max - min)/2 <= rho(l)/2), so the
// colours on that path get darker one rounding step at a time, and the ones that
// pass form a prefix of it: the answer is the brightest colour of that prefix.
// Darkening ends
// at black, and black passes with paper text (20.6:1), so the prefix is not empty
// for this palette. (In a palette in which black did not pass, the bisection's
// invariant below would not hold and its answer would not be guaranteed to pass;
// the black row of TestAccent_TheDesignTableHolds expects black to pass.)
//
// The path is computed in integers (accentPath). Suggest reads its argument, the
// palette constants and the accentLinear table and keeps no state of its own, so a
// refused colour gets the same suggestion from one call to the next
// (TestSuggest_IsDeterministicUnderConcurrency calls it from 32 goroutines).
func Suggest(a Color) Color {
	if _, err := Check(a); err == nil {
		return a
	}
	p, u := accentSuggestStep(a)
	return p.at(u)
}

// accentSuggestStep is the bisection behind Suggest, for a colour Check refused:
// the path below it and the largest grid step whose colour passes. Invariant: the
// colour at lo passes (lo = 0 is black) and the colour at hi does not (hi = top is
// the refused colour itself).
func accentSuggestStep(a Color) (accentPath, int64) {
	p := accentPathOf(a)
	lo, hi := int64(0), p.top
	for hi-lo > 1 {
		mid := lo + (hi-lo)/2
		if _, err := Check(p.at(mid)); err == nil {
			lo = mid
		} else {
			hi = mid
		}
	}
	return p, lo
}

// accentPathScale is U: a point on a path is lightness u/U for an integer u. It is
// 510 * 2^accentPathShift so that a colour's own lightness, (max+min)/510, is the
// exact grid point (max+min) * 2^accentPathShift.
//
// WHY THIS FINE. A channel of the path changes value where channel*255 crosses
// n + 1/2. Solving the formula at accentPath for that lightness, on either side of
// one half, gives a rational with denominator 510 * (d +- (2c - l2)); since
// |2c - l2| <= max - min <= d <= 255, that is at most 510 * 510 = 260100, so two
// different crossings are at least 1/260100^2 ~ 1.5e-11 apart. The grid step is
// 1/U ~ 9.1e-13, about sixteen times finer, so each interval on which the exact
// path holds a colour contains a grid point, which is what lets the bisection reach
// the brightest passing one (TestSuggest_IsTheBrightestPassingColourOnTheExactPath
// pins U above 260100^2 and compares with a grid-free enumeration on a sample).
//
// WHY INTEGERS. The same path in float64 is a second, slightly different path: at a
// lightness where two channels cross their rounding boundary together, floating
// error decides which one rounds first. 0x007EC6 is one: below half lightness the
// path scales the three channels by the same t, G = 126t and B = 198t cross 122.5 and
// 192.5 at the same t = 35/36, so G = 0x7A with B = 0xC1 exists at no t — and a
// textbook float HSL bisection suggests exactly 0x007AC1 for it, where this one
// suggests 0x007AC0 (both pinned in TestSuggest_ThePathIsTextbookHSL).
const (
	accentPathShift       = 31
	accentPathScale int64 = 510 << accentPathShift
)

// accentPath is the HSL lightness path at or below one colour, with its hue and
// saturation held. In HSL, with l the colour's lightness and rho(x) = 1 - |2x - 1|,
// a channel c at lightness l' is l' + rho(l')/rho(l) * (c - l): the saturation
// cancels out of the ratio, so the code computes no hue angle or saturation. Scaled
// by 510 everything is an integer: l = (max+min)/510 = l2/510, and
// rho(l) = d/255 with d = 255 - |l2 - 255|.
type accentPath struct {
	c   [3]int64 // the colour's channels, 0..255
	l2  int64    // max + min: 510 times its lightness
	d   int64    // 255 * rho(lightness), or 1 for black and white (accentPathOf)
	top int64    // the grid step of the colour itself: l2 * 2^accentPathShift
}

// accentPathOf builds the path below a. Black and white are the two colours with
// rho = 0, and both are greys: 2c - l2 = 0 for each of the three channels, so the
// term d divides vanishes and the channel (u/U * 255, a grey) does not depend on d
// as long as d is not zero. d = 1 keeps the division in at defined for them; their
// paths are black alone and the greys below white.
func accentPathOf(a Color) accentPath {
	c := [3]int64{int64(a.R), int64(a.G), int64(a.B)}
	hi, lo := max(c[0], c[1], c[2]), min(c[0], c[1], c[2])
	l2 := hi + lo
	d := 255 - accentAbs(l2-255)
	if d == 0 {
		d = 1
	}
	return accentPath{c: c, l2: l2, d: d, top: l2 << accentPathShift}
}

// at is the path's colour at lightness u/U, each channel rounded half up:
//
//	channel*255 = 255 * (2*u*d + w*(2*c - l2)) / (2*U*d),  w = U - |2u - U| = U*rho(u/U)
//
// and round half up is floor(x + 1/2), which adds U*d to the numerator. The
// numerator is non-negative because the channel is, so integer division is floor.
// Magnitudes, with u <= top <= U ~ 1.1e12 and d, |2c - l2| <= 255: the numerator is
// below 2.2e17 and the denominator below 5.6e14, both inside int64 (9.2e18).
func (p accentPath) at(u int64) Color {
	w := accentPathScale - accentAbs(2*u-accentPathScale)
	den := 2 * accentPathScale * p.d
	var out [3]uint8
	for i, c := range p.c {
		out[i] = uint8((255*(2*u*p.d+w*(2*c-p.l2)) + accentPathScale*p.d) / den)
	}
	return Color{R: out[0], G: out[1], B: out[2]}
}

func accentAbs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
