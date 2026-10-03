package brand

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// The reference. Everything in this block is the test's OWN implementation of
// ADR 0023 §3: the palette is read from tailwind.config.js (not from the Go copy)
// and luminance, contrast and the four boundaries are computed here from the
// WCAG 2.x formula. The functions in this block do not call accent.go, so a test that compares the two
// compares two implementations, not one implementation with itself.
// ---------------------------------------------------------------------------

// paletteRepoRoot is the repository root, from this package's directory.
var paletteRepoRoot = filepath.Join("..", "..")

// paletteReadConfig returns tailwind.config.js's text.
func paletteReadConfig(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(paletteRepoRoot, "tailwind.config.js"))
	if err != nil {
		t.Fatalf("reading tailwind.config.js: %v", err)
	}
	return string(raw)
}

var (
	// paletteBlockOpenRE is the line that opens the colors block.
	paletteBlockOpenRE = regexp.MustCompile(`^colors\s*:\s*\{$`)
	// paletteEntryRE is one whole line of that block in the expected shape; a line
	// in another shape is a parse error, not a line to skip
	// (TestPalette_TheParserRefusesWhatItCannotRead lists the shapes it measures).
	paletteEntryRE = regexp.MustCompile(`^'?([a-z][a-z-]*)'?\s*:\s*'#([0-9A-Fa-f]{6})',?$`)
)

// paletteParseConfig reads the colors block of a tailwind.config.js text into
// name -> 0xRRGGBB. It is strict on purpose: a line it does not recognise inside
// the block is an error, so a change to the file's shape turns the palette test
// red rather than letting the parse find fewer tokens and compare those.
// Whole-line // comments inside the block are skipped (they are not tokens).
func paletteParseConfig(text string) (map[string]uint32, error) {
	lines := strings.Split(text, "\n")
	start := -1
	for i, line := range lines {
		if paletteBlockOpenRE.MatchString(strings.TrimSpace(line)) {
			if start >= 0 {
				return nil, fmt.Errorf("two colors blocks (lines %d and %d)", start+1, i+1)
			}
			start = i
		}
	}
	if start < 0 {
		return nil, errors.New("no colors block")
	}
	out := map[string]uint32{}
	for i := start + 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		switch {
		case line == "}" || line == "},":
			if len(out) == 0 {
				return nil, errors.New("the colors block has no entries")
			}
			return out, nil
		case line == "" || strings.HasPrefix(line, "//"):
			continue
		}
		m := paletteEntryRE.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("line %d of the colors block is not a colour entry: %q", i+1, line)
		}
		if _, dup := out[m[1]]; dup {
			return nil, fmt.Errorf("colour %q defined twice", m[1])
		}
		v, err := strconv.ParseUint(m[2], 16, 32)
		if err != nil {
			return nil, fmt.Errorf("colour %q: %w", m[1], err)
		}
		out[m[1]] = uint32(v)
	}
	return nil, errors.New("the colors block is not closed")
}

// paletteDiff lists the disagreements between the Go copy (paletteTokens) and a
// parsed config, of three kinds: a value that differs, a name one side lacks, a
// name the Go copy repeats. Empty means equal.
func paletteDiff(config map[string]uint32) []string {
	var out []string
	inCopy := map[string]bool{}
	for _, tok := range paletteTokens {
		if inCopy[tok.name] {
			out = append(out, fmt.Sprintf("%s: twice in the Go copy", tok.name))
		}
		inCopy[tok.name] = true
		v, ok := config[tok.name]
		switch {
		case !ok:
			out = append(out, fmt.Sprintf("%s: in the Go copy, not in tailwind.config.js", tok.name))
		case v != tok.hex:
			out = append(out, fmt.Sprintf("%s: Go copy %06X, tailwind.config.js %06X", tok.name, tok.hex, v))
		}
	}
	for name := range config {
		if !inCopy[name] {
			out = append(out, fmt.Sprintf("%s: in tailwind.config.js, not in the Go copy", name))
		}
	}
	sort.Strings(out)
	return out
}

// contrastReference is the test's WCAG 2.x arithmetic over the palette it read.
type contrastReference struct {
	linear [256]float64
	// luminances of the three palette colours the gate uses
	paper, ink, porcelain float64
	// ADR 0023 §3's four boundaries, from the formulas printed in its table
	paperBound float64 // paper text reaches 4.5:1 for L <= this
	inkBound   float64 // ink text reaches 4.5:1 for L >= this
	edgeBound  float64 // the accent reaches 3:1 against porcelain for L <= this
	crossover  float64 // paper and ink text have equal contrast here
}

func contrastReferenceFrom(t *testing.T, config map[string]uint32) *contrastReference {
	t.Helper()
	r := &contrastReference{}
	for v := range r.linear {
		c := float64(v) / 255
		if c <= 0.04045 {
			r.linear[v] = c / 12.92
		} else {
			r.linear[v] = math.Pow((c+0.055)/1.055, 2.4)
		}
	}
	need := func(name string) float64 {
		hex, ok := config[name]
		if !ok {
			t.Fatalf("tailwind.config.js has no %q token; the reference is not built", name)
		}
		return r.luminance(hex)
	}
	r.paper, r.ink, r.porcelain = need("paper"), need("ink"), need("porcelain")
	r.paperBound = (r.paper+0.05)/4.5 - 0.05
	r.inkBound = 4.5*(r.ink+0.05) - 0.05
	r.edgeBound = (r.porcelain+0.05)/3 - 0.05
	r.crossover = math.Sqrt((r.paper+0.05)*(r.ink+0.05)) - 0.05
	return r
}

func contrastReferenceFromRepo(t *testing.T) *contrastReference {
	t.Helper()
	config, err := paletteParseConfig(paletteReadConfig(t))
	if err != nil {
		t.Fatalf("parsing tailwind.config.js: %v", err)
	}
	return contrastReferenceFrom(t, config)
}

func (r *contrastReference) luminance(hex uint32) float64 {
	return 0.2126*r.linear[uint8(hex>>16)] + 0.7152*r.linear[uint8(hex>>8)] + 0.0722*r.linear[uint8(hex)]
}

func contrastOf(la, lb float64) float64 {
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func accentColor(hex uint32) Color {
	return Color{R: uint8(hex >> 16), G: uint8(hex >> 8), B: uint8(hex)}
}

func accentHexOf(c Color) uint32 {
	return uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
}

// ---------------------------------------------------------------------------
// The palette copy
// ---------------------------------------------------------------------------

// TestPalette_TheGoCopyEqualsTailwindConfig is ADR 0023 §3's pin on the copy: the
// nine tokens of tailwind.config.js's colors block and the Go copy are the same
// names with the same values, in both directions. A colour changed on either side
// turns this red; so does a token added to or removed from either side.
func TestPalette_TheGoCopyEqualsTailwindConfig(t *testing.T) {
	t.Parallel()
	config, err := paletteParseConfig(paletteReadConfig(t))
	if err != nil {
		t.Fatalf("parsing tailwind.config.js: %v", err)
	}
	if len(config) != len(paletteTokens) {
		t.Errorf("tailwind.config.js has %d colour tokens, the Go copy %d", len(config), len(paletteTokens))
	}
	for _, d := range paletteDiff(config) {
		t.Errorf("palette copy drifted: %s", d)
	}
}

// TestPalette_TheComparisonSeesEveryKindOfDrift is the negative control for the
// test above: run on the real file text with ONE change made to it, the
// comparison must report that change. Without this, an empty diff could mean
// "equal" or "the comparison is vacuous".
func TestPalette_TheComparisonSeesEveryKindOfDrift(t *testing.T) {
	t.Parallel()
	text := paletteReadConfig(t)
	if _, err := paletteParseConfig(text); err != nil {
		t.Fatalf("parsing tailwind.config.js: %v", err)
	}

	// One variant per token: the token's hex with its last digit changed.
	for _, tok := range paletteTokens {
		hex := fmt.Sprintf("%06X", tok.hex)
		changed := hex[:5] + string("0123456789ABCDEF"[(strings.IndexByte("0123456789ABCDEF", hex[5])+1)%16])
		mutated := strings.Replace(text, "'#"+hex+"'", "'#"+changed+"'", 1)
		if mutated == text {
			t.Errorf("%s: the hex %s is not in tailwind.config.js as written; the variant is identical to the file", tok.name, hex)
			continue
		}
		config, err := paletteParseConfig(mutated)
		if err != nil {
			t.Errorf("%s: parsing the variant: %v", tok.name, err)
			continue
		}
		diff := paletteDiff(config)
		if len(diff) != 1 || !strings.HasPrefix(diff[0], tok.name+":") {
			t.Errorf("%s changed to %s: diff = %q, want exactly one entry naming %s", tok.name, changed, diff, tok.name)
		}
	}

	// A token removed, a token added, a token renamed.
	removeLine := regexp.MustCompile(`(?m)^\s*line: '#[0-9A-Fa-f]{6}',\n`)
	others := []struct {
		name    string
		mutated string
		want    string
	}{
		{"removed", removeLine.ReplaceAllString(text, ""), "line: in the Go copy, not in tailwind.config.js"},
		{"added", strings.Replace(text, "        line:", "        plum: '#5B2C6F',\n        line:", 1), "plum: in tailwind.config.js, not in the Go copy"},
		{"renamed", strings.Replace(text, "        tomato:", "        tomatoes:", 1), "tomato: in the Go copy, not in tailwind.config.js"},
	}
	for _, o := range others {
		if o.mutated == text {
			t.Errorf("%s: the variant is identical to the file", o.name)
			continue
		}
		config, err := paletteParseConfig(o.mutated)
		if err != nil {
			t.Errorf("%s: parsing the variant: %v", o.name, err)
			continue
		}
		diff := paletteDiff(config)
		found := false
		for _, d := range diff {
			found = found || d == o.want
		}
		if !found {
			t.Errorf("%s: diff = %q, want it to contain %q", o.name, diff, o.want)
		}
	}
}

// TestPalette_TheParserRefusesWhatItCannotRead runs the config parser on its own
// degenerate inputs. Each refusal is a shape the parser would otherwise have to
// guess about; guessing is how a palette test ends up comparing fewer tokens than
// the file has.
//
// 🔴 A MALFORMED LINE SITS BETWEEN TWO VALID ONES, AND THE ERROR MUST NAME IT. The
// first version put each malformed shape alone in its block, and an audit showed
// what that measured: a parser that SKIPS a line it does not recognise also failed
// there — on "the colors block has no entries", a different rule — so the table
// was green for the lenient parser too. With a valid ink line before and a valid
// paper line after, a skipping parser returns {ink, paper} without an error, and
// the assertion on the error's line number tells a line refusal from the block-level
// refusals.
func TestPalette_TheParserRefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()
	const head = "module.exports = {\n  theme: {\n    extend: {\n      colors: {\n"
	const tail = "      },\n    },\n  },\n}\n"
	wrap := func(body string) string { return head + body + tail }
	// The colors block opens on line 4 of head; body line k is file line 4+k.
	const firstBodyLine = 5

	// Shapes the parser must refuse as a LINE, each placed as body line 2 between
	// ink (line 1) and paper (line 3).
	malformed := []struct{ name, line string }{
		{"double-quoted hex", `        plum: "#5B2C6F",`},
		{"trailing comment", `        plum: '#5B2C6F', // brand plum`},
		{"block comment", `        /* plum: '#5B2C6F', */`},
		{"two tokens on a line", `        plum: '#5B2C6F', fig: '#6B3A5B',`},
		{"nested object", `        green: {`},
		{"three-digit hex", `        plum: '#5B2',`},
		{"eight-digit hex", `        plum: '#5B2C6FFF',`},
		{"rgb value", `        plum: 'rgb(91 44 111)',`},
		{"upper-case name", `        Plum: '#5B2C6F',`},
		{"unquoted hex", `        plum: #5B2C6F,`},
	}
	for _, m := range malformed {
		t.Run("line: "+m.name, func(t *testing.T) {
			text := wrap("        ink: '#152219',\n" + m.line + "\n        paper: '#FFFDF4',\n")
			got, err := paletteParseConfig(text)
			if err == nil {
				t.Fatalf("parsed %v, want the line refused", got)
			}
			want := fmt.Sprintf("line %d of the colors block is not a colour entry", firstBodyLine+1)
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error %q, want it to say %q", err, want)
			}
		})
	}

	tests := []struct {
		name    string
		text    string
		want    map[string]uint32 // nil: a parse error is expected
		wantErr string            // the error must contain this
	}{
		{"one token", wrap("        ink: '#152219',\n"), map[string]uint32{"ink": 0x152219}, ""},
		{"quoted name", wrap("        'tappa-green': '#1F5C41',\n"), map[string]uint32{"tappa-green": 0x1F5C41}, ""},
		{"lower-case hex is the same value", wrap("        ink: '#15221a',\n"), map[string]uint32{"ink": 0x15221A}, ""},
		{"no trailing comma", wrap("        ink: '#152219'\n"), map[string]uint32{"ink": 0x152219}, ""},
		{"whole-line comment skipped", wrap("        ink: '#152219',\n        // plum: '#5B2C6F',\n        paper: '#FFFDF4',\n"),
			map[string]uint32{"ink": 0x152219, "paper": 0xFFFDF4}, ""},
		{"blank line skipped", wrap("\n        ink: '#152219',\n\n        paper: '#FFFDF4',\n"),
			map[string]uint32{"ink": 0x152219, "paper": 0xFFFDF4}, ""},
		{"no colors block", "module.exports = { theme: {} }\n", nil, "no colors block"},
		{"two colors blocks", wrap("        ink: '#152219',\n") + wrap("        ink: '#152219',\n"), nil, "two colors blocks"},
		{"empty block", wrap(""), nil, "has no entries"},
		{"unclosed block", "      colors: {\n        ink: '#152219',\n", nil, "not closed"},
		{"name defined twice", wrap("        ink: '#152219',\n        paper: '#FFFDF4',\n        ink: '#000000',\n"), nil, `"ink" defined twice`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := paletteParseConfig(tc.text)
			if tc.want == nil {
				if err == nil {
					t.Fatalf("parsed %v, want an error", got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q, want it to say %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Spelling
// ---------------------------------------------------------------------------

// TestAccent_ParseAcceptsOneSpellingPerColour: ParseAccent takes six upper-case
// hex digits (ADR 0023 §1's `^[0-9A-F]{6}$`); the table lists the refusals it
// measures. The exhaustive round trip (each of the 2^24 colours' Hex parses back to
// it) is in
// TestAccent_EveryColourAgreesWithTheBoundariesThePaletteImplies.
func TestAccent_ParseAcceptsOneSpellingPerColour(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want uint32
		ok   bool
	}{
		{"1F5C41", 0x1F5C41, true},
		{"000000", 0x000000, true},
		{"FFFFFF", 0xFFFFFF, true},
		{"0A0B0C", 0x0A0B0C, true},
		{"9ABCDE", 0x9ABCDE, true},
		{"1f5c41", 0, false},
		{"1F5c41", 0, false},
		{"abcdef", 0, false},
		{"#1F5C41", 0, false},
		{"1F5C4", 0, false},
		{"1F5C411", 0, false},
		{"", 0, false},
		{" 1F5C41", 0, false},
		{"1F5C41 ", 0, false},
		{"1F5C41\n", 0, false},
		{"1F5C4G", 0, false},
		{"0x1F5C", 0, false},
		{"+1F5C4", 0, false},
		{"-1F5C4", 0, false},
		{"1F_5C4", 0, false},
		{"1F5C4\x00", 0, false},
		// CSS's three-digit shorthand is a second spelling of 0xAABBCC; refused, so it
		// does not become one (an audit's mutant expanding it stayed green before
		// these four rows).
		{"ABC", 0, false},
		{"abc", 0, false},
		{"#ABC", 0, false},
		{"#abc", 0, false},
		{"\xef\xbc\x91F5C", 0, false}, // a full-width one (three bytes) and three digits: six bytes
		{"1F5C\xc3\xa9", 0, false},    // a two-byte letter: six bytes
		{"1F5C4:", 0, false},          // ':' is '9'+1
		{"1F5C4@", 0, false},          // '@' is 'A'-1
		{"1F5C4/", 0, false},          // '/' is '0'-1
	}
	for _, tc := range tests {
		got, err := ParseAccent(tc.in)
		switch {
		case tc.ok && err != nil:
			t.Errorf("ParseAccent(%q): %v, want %06X", tc.in, err, tc.want)
		case tc.ok && accentHexOf(got) != tc.want:
			t.Errorf("ParseAccent(%q) = %06X, want %06X", tc.in, accentHexOf(got), tc.want)
		case tc.ok && got.Hex() != tc.in:
			t.Errorf("ParseAccent(%q).Hex() = %q, want the input back", tc.in, got.Hex())
		case !tc.ok && !errors.Is(err, ErrAccentSyntax):
			t.Errorf("ParseAccent(%q) = %06X, %v; want ErrAccentSyntax", tc.in, accentHexOf(got), err)
		case !tc.ok && got != (Color{}):
			t.Errorf("ParseAccent(%q) refused but returned %06X", tc.in, accentHexOf(got))
		}
	}
}

// TestAccent_NormalizeTakesWhatTheColourInputSends: the editor's
// <input type="color"> sends "#rrggbb" in lower case; NormalizeAccent admits that
// and the canonical spelling; the table lists the refusals it measures. Its
// output's Hex is canonical.
func TestAccent_NormalizeTakesWhatTheColourInputSends(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string // "" = refused
	}{
		{"#e0457b", "E0457B"},
		{"e0457b", "E0457B"},
		{"#E0457B", "E0457B"},
		{"E0457B", "E0457B"},
		{"#E0457b", "E0457B"},
		{"#000000", "000000"},
		{"#ffffff", "FFFFFF"},
		{"##e0457b", ""},
		{"#e0457", ""},
		{"#e0457b0", ""},
		{" #e0457b", ""},
		{"#e0457b ", ""},
		{"#ge0457", ""},
		{"0xE0457B", ""},
		{"e0457b#", ""},
		{"#", ""},
		{"", ""},
		{"#e04", ""},
		// CSS's three-digit shorthand, bare and with '#', in both cases: refused,
		// not expanded (see the same rows in the ParseAccent table).
		{"e04", ""},
		{"E04", ""},
		{"ABC", ""},
		{"abc", ""},
		{"#ABC", ""},
		{"#abc", ""},
		{"#E0457B\n", ""},
	}
	for _, tc := range tests {
		got, err := NormalizeAccent(tc.in)
		if tc.want == "" {
			if !errors.Is(err, ErrAccentSyntax) {
				t.Errorf("NormalizeAccent(%q) = %s, %v; want ErrAccentSyntax", tc.in, got.Hex(), err)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeAccent(%q): %v, want %s", tc.in, err, tc.want)
			continue
		}
		if got.Hex() != tc.want {
			t.Errorf("NormalizeAccent(%q).Hex() = %s, want %s", tc.in, got.Hex(), tc.want)
		}
		if back, err := ParseAccent(got.Hex()); err != nil || back != got {
			t.Errorf("NormalizeAccent(%q): its Hex %s does not ParseAccent back (%v)", tc.in, got.Hex(), err)
		}
	}
}

// ---------------------------------------------------------------------------
// The gate
// ---------------------------------------------------------------------------

// TestAccent_TheDesignTableHolds puts ADR 0023 §3's example table, the skill's
// ratios and the ratios the comments in accent.go state through the production
// functions. Values are the ones those documents print; L is checked to the six
// decimals printed, ratios to ±0.01 (WL-2's acceptance). A NaN means the source
// prints no value for that cell.
func TestAccent_TheDesignTableHolds(t *testing.T) {
	t.Parallel()
	nan := math.NaN()
	paper, ink := paletteColor(palettePaperHex), paletteColor(paletteInkHex)
	porcelain := paletteColor(palettePorcelainHex)
	tests := []struct {
		name      string
		hex       uint32
		lum       float64 // relative luminance as printed (six decimals)
		onPaper   float64 // paper text on the accent
		onInk     float64 // ink text on the accent
		porcelain float64 // the accent against porcelain
		ok        bool
		text      Color // the label colour (OnColor), also for a refused accent
		edge      bool
	}{
		// ADR 0023 §3, the example table
		{"grey #808080 refused", 0x808080, 0.215861, 3.88, 4.17, nan, false, ink, false},
		{"pink #E0457B refused", 0xE0457B, 0.215336, 3.88, 4.16, nan, false, ink, false},
		{"red #DA291C paper text", 0xDA291C, 0.165751, 4.78, 3.39, nan, true, paper, false},
		{"yellow #FFC72C ink text and edge", 0xFFC72C, 0.622887, 1.53, 10.56, 1.36, true, ink, true},
		{"tappa-green, the default", paletteTappaGreenHex, 0.083273, 7.73, 2.09, nan, true, paper, false},
		{"tomato paper text", paletteTomatoHex, 0.144518, 5.30, 3.05, nan, true, paper, false},
		{"saffron ink text and edge", paletteSaffronHex, 0.342721, 2.62, 6.16, nan, true, ink, true},
		// ADR 0023, accepted limit 3: invisible as a stripe, edged as a button
		{"#EEEEEE ink text and edge", 0xEEEEEE, nan, nan, nan, 1.01, true, ink, true},
		// accent.go's Suggest comment: black passes with paper text
		{"black paper text", 0x000000, 0, 20.61, nan, nan, true, paper, false},
		// ADR 0023 §3: refused colours a truncated boundary would admit
		{"#008384 just short of 4.5", 0x008384, nan, 4.50, nan, nan, false, paper, false},
		{"#22864B just short of 4.5", 0x22864B, nan, 4.50, nan, nan, false, paper, false},
		{"#1E93A0 just short of 4.5", 0x1E93A0, nan, nan, 4.50, nan, false, ink, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := accentColor(tc.hex)
			if !math.IsNaN(tc.lum) {
				if got := accentLuminance(c); math.Abs(got-tc.lum) > 5e-7 {
					t.Errorf("L = %.7f, want %.6f", got, tc.lum)
				}
			}
			for _, r := range []struct {
				what string
				got  float64
				want float64
			}{
				{"paper on it", contrastRatio(paper, c), tc.onPaper},
				{"ink on it", contrastRatio(ink, c), tc.onInk},
				{"it on porcelain", contrastRatio(c, porcelain), tc.porcelain},
			} {
				if !math.IsNaN(r.want) && math.Abs(r.got-r.want) > 0.01 {
					t.Errorf("%s: %.4f:1, want %.2f:1 (+/-0.01)", r.what, r.got, r.want)
				}
			}
			fill, err := Check(c)
			if tc.ok != (err == nil) {
				t.Fatalf("Check: err = %v, want accepted = %v", err, tc.ok)
			}
			if !tc.ok {
				if !errors.Is(err, ErrAccentIllegible) {
					t.Errorf("Check: err = %v, want ErrAccentIllegible", err)
				}
				if best := math.Max(contrastRatio(paper, c), contrastRatio(ink, c)); best >= accentTextMinimum {
					t.Errorf("refused, but its better text reaches %.7f:1", best)
				}
			}
			if got := OnColor(c); got != tc.text {
				t.Errorf("OnColor = %s, want %s", got.Hex(), tc.text.Hex())
			}
			if got := Edge(c); got != tc.edge {
				t.Errorf("Edge = %v, want %v", got, tc.edge)
			}
			if tc.ok {
				want := Fill{Accent: c, Text: tc.text, Edge: tc.edge}
				if fill != want {
					t.Errorf("Fill = %+v, want %+v", fill, want)
				}
			}
		})
	}
	// Edge's comment: ink on porcelain is 14.32:1.
	if got := contrastRatio(ink, porcelain); math.Abs(got-14.32) > 0.01 {
		t.Errorf("ink on porcelain = %.4f:1, want 14.32:1", got)
	}
}

// contrastNeighbours is the colour nearest a boundary on each side of it.
type contrastNeighbours struct {
	atOrBelow, above       uint32
	dBelow, dAbove         float64
	foundBelow, foundAbove bool
}

// contrastNearestTo finds, over all 2^24 colours, the one whose reference
// luminance is the largest at or below the boundary and the smallest above it.
func contrastNearestTo(ref *contrastReference, bound float64) contrastNeighbours {
	n := contrastNeighbours{dBelow: math.Inf(1), dAbove: math.Inf(1)}
	for x := uint32(0); x < 1<<24; x++ {
		l := ref.luminance(x)
		if d := bound - l; d >= 0 && d < n.dBelow {
			n.atOrBelow, n.dBelow, n.foundBelow = x, d, true
		}
		if d := l - bound; d > 0 && d < n.dAbove {
			n.above, n.dAbove, n.foundAbove = x, d, true
		}
	}
	return n
}

// TestAccent_TheNearestHexEitherSideOfEachComputedBoundary is WL-2's boundary
// acceptance: the three luminance boundaries are computed from the palette (no
// literal), the nearest 8-bit colour on each side of each is found by scanning
// the 2^24 colours, and the production gate is asked about those six colours. No
// 8-bit colour lies ON a boundary (the smallest distance is logged and bounded in
// TestAccent_EveryColourAgreesWithTheBoundariesThePaletteImplies), so "the
// boundary itself passes" is tested by the colour nearest to it on the passing
// side.
//
// The six neighbours are also compared with the six ADR 0023 §3's "WL-2 notu"
// records (6E7B44 / 7D5BEC, 1E93A0 / 8D76DA, B268EC / 8F8A7A), and the test reads
// that note to check that each of the six hexes appears in it. Measured scope: a
// scanned neighbour that differs from this table, and a table hex that is changed
// in or removed from the note, turn the test red. Which hex the note puts on which
// side (passes / refused) is NOT read — swapping two of them there stays green.
func TestAccent_TheNearestHexEitherSideOfEachComputedBoundary(t *testing.T) {
	t.Parallel()
	ref := contrastReferenceFromRepo(t)
	paper, ink := paletteColor(palettePaperHex), paletteColor(paletteInkHex)
	note := accentADRWL2Note(t, accentReadADR0023(t))

	type side struct {
		ok     bool
		text   Color
		edge   bool
		record uint32 // the nearest colour ADR 0023 §3's WL-2 note records
	}
	tests := []struct {
		name         string
		bound        float64
		below, above side
	}{
		{"paper text boundary", ref.paperBound,
			side{ok: true, text: paper, edge: false, record: 0x6E7B44},
			side{ok: false, text: paper, edge: false, record: 0x7D5BEC}},
		{"ink text boundary", ref.inkBound,
			side{ok: false, text: ink, edge: false, record: 0x1E93A0},
			side{ok: true, text: ink, edge: false, record: 0x8D76DA}},
		{"porcelain edge boundary", ref.edgeBound,
			side{ok: true, text: ink, edge: false, record: 0xB268EC},
			side{ok: true, text: ink, edge: true, record: 0x8F8A7A}},
	}
	for _, tc := range tests {
		n := contrastNearestTo(ref, tc.bound)
		if !n.foundBelow || !n.foundAbove {
			t.Errorf("%s (L = %.12f): no colour on one side of it", tc.name, tc.bound)
			continue
		}
		t.Logf("%s: L = %.12f; at or below %06X (%.3g under), above %06X (%.3g over)",
			tc.name, tc.bound, n.atOrBelow, n.dBelow, n.above, n.dAbove)
		for _, s := range []struct {
			where string
			hex   uint32
			want  side
		}{
			{"at or below", n.atOrBelow, tc.below},
			{"above", n.above, tc.above},
		} {
			c := accentColor(s.hex)
			_, err := Check(c)
			if (err == nil) != s.want.ok {
				t.Errorf("%s, %s: Check(%06X) err = %v, want accepted = %v", tc.name, s.where, s.hex, err, s.want.ok)
			}
			if got := OnColor(c); got != s.want.text {
				t.Errorf("%s, %s: OnColor(%06X) = %s, want %s", tc.name, s.where, s.hex, got.Hex(), s.want.text.Hex())
			}
			if got := Edge(c); got != s.want.edge {
				t.Errorf("%s, %s: Edge(%06X) = %v, want %v", tc.name, s.where, s.hex, got, s.want.edge)
			}
			if s.hex != s.want.record {
				t.Errorf("%s, %s: nearest colour is %06X, the record says %06X", tc.name, s.where, s.hex, s.want.record)
			}
			if spelled := fmt.Sprintf("`%06X`", s.want.record); !strings.Contains(note, spelled) {
				t.Errorf("%s, %s: the WL-2 note of ADR 0023 does not mention %s", tc.name, s.where, spelled)
			}
		}
	}
}

// accentReadADR0023 returns ADR 0023's text.
func accentReadADR0023(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(paletteRepoRoot, "docs", "adr", "0023-tenant-markasi-ve-arayuz-kurali.md"))
	if err != nil {
		t.Fatalf("reading ADR 0023: %v", err)
	}
	return string(raw)
}

// accentADRWL2Note is the "WL-2 notu" paragraph of ADR 0023 §3: from its bold
// heading to the next bold paragraph heading at the start of a line.
func accentADRWL2Note(t *testing.T, adr string) string {
	t.Helper()
	start := strings.Index(adr, "\n**WL-2 notu")
	if start < 0 {
		t.Fatal("ADR 0023 has no WL-2 note")
	}
	rest := adr[start+1:]
	end := strings.Index(rest[2:], "\n**")
	if end < 0 {
		t.Fatal("ADR 0023's WL-2 note has no paragraph after it")
	}
	return rest[:end+2]
}

// TestAccent_TheADRPrintsTheBoundariesThePaletteYields reads ADR 0023 §3's
// boundary table and its refused-colour count and compares them with what the
// palette in tailwind.config.js yields. The ADR prints the boundaries TRUNCATED
// (bold "0,<digits>…"), so each printed digit string must be the truncation of the
// computed value — which is how this test pins those values without writing one.
func TestAccent_TheADRPrintsTheBoundariesThePaletteYields(t *testing.T) {
	t.Parallel()
	ref := contrastReferenceFromRepo(t)
	raw := accentReadADR0023(t)
	lines := strings.Split(raw, "\n")
	printed := regexp.MustCompile(`\*\*0,(\d+)\x{2026}\*\*`)
	rows := []struct {
		prefix string
		value  float64
	}{
		{"| paper metin", ref.paperBound},
		{"| ink metin", ref.inkBound},
		{"| OnColor ", ref.crossover},
		{"| porcelain'e ", ref.edgeBound},
	}
	for _, row := range rows {
		var hits []string
		for _, line := range lines {
			if strings.HasPrefix(line, row.prefix) {
				hits = append(hits, line)
			}
		}
		if len(hits) != 1 {
			t.Errorf("%q: %d table rows start with it, want 1", row.prefix, len(hits))
			continue
		}
		m := printed.FindStringSubmatch(hits[0])
		if m == nil {
			t.Errorf("%q: no bold truncated value in %q", row.prefix, hits[0])
			continue
		}
		digits := m[1]
		want := strconv.FormatInt(int64(math.Floor(row.value*math.Pow10(len(digits)))), 10)
		want = strings.Repeat("0", len(digits)-len(want)) + want
		if digits != want {
			t.Errorf("%q: ADR prints 0,%s..., the palette yields %.15f (truncated: 0,%s)", row.prefix, digits, row.value, want)
		}
	}

	// "orada en iyi kontrast **4,0208**": the better text contrast at the crossover.
	best := regexp.MustCompile(`en iyi kontrast \*\*4,(\d{4})\*\*`).FindStringSubmatch(raw)
	atCrossover := contrastOf(ref.paper, ref.crossover)
	if best == nil {
		t.Error("ADR 0023 no longer prints the best contrast at the crossover")
	} else if got := fmt.Sprintf("%04d", int(math.Round((atCrossover-4)*1e4))); got != best[1] {
		t.Errorf("best contrast at the crossover: ADR prints 4,%s, the palette yields %.6f", best[1], atCrossover)
	}

	// The sentence that says the gate refuses "**1 949 736**" of the 24-bit colours.
	count := regexp.MustCompile(`renklerin \*\*([0-9 ]+)\*\*`).FindStringSubmatch(raw)
	if count == nil {
		t.Fatal("ADR 0023 no longer prints the refused-colour count")
	}
	printedCount, err := strconv.Atoi(strings.ReplaceAll(count[1], " ", ""))
	if err != nil {
		t.Fatalf("refused-colour count %q: %v", count[1], err)
	}
	refused := 0
	for x := uint32(0); x < 1<<24; x++ {
		l := ref.luminance(x)
		if l > ref.paperBound && l < ref.inkBound {
			refused++
		}
	}
	if refused != printedCount {
		t.Errorf("ADR prints %d refused colours, the palette's band holds %d", printedCount, refused)
	}
}

// accentTally collects one shard's results of the exhaustive scan.
type accentTally struct {
	colours, refused       int
	violations             map[string]int
	example                map[string]uint32
	minBoundaryMargin      float64 // smallest |L - boundary| over the three gate boundaries
	minCrossoverMargin     float64
	minPaperClassPorcelain float64 // smallest porcelain contrast among paper-text accents
}

func accentNewTally() *accentTally {
	return &accentTally{
		violations: map[string]int{}, example: map[string]uint32{},
		minBoundaryMargin: math.Inf(1), minCrossoverMargin: math.Inf(1), minPaperClassPorcelain: math.Inf(1),
	}
}

func (a *accentTally) fail(property string, x uint32) {
	if a.violations[property] == 0 {
		a.example[property] = x
	}
	a.violations[property]++
}

func (a *accentTally) merge(b *accentTally) {
	a.colours += b.colours
	a.refused += b.refused
	for k, v := range b.violations {
		if a.violations[k] == 0 {
			a.example[k] = b.example[k]
		}
		a.violations[k] += v
	}
	a.minBoundaryMargin = math.Min(a.minBoundaryMargin, b.minBoundaryMargin)
	a.minCrossoverMargin = math.Min(a.minCrossoverMargin, b.minCrossoverMargin)
	a.minPaperClassPorcelain = math.Min(a.minPaperClassPorcelain, b.minPaperClassPorcelain)
}

// accentCheckColour asks the production functions (Check, OnColor, Edge, Hex and
// ParseAccent, Suggest, accentSuggestStep, the path) about one colour and records
// each disagreement with the reference under a property name.
func accentCheckColour(ref *contrastReference, x uint32, tl *accentTally) {
	paper, ink := paletteColor(palettePaperHex), paletteColor(paletteInkHex)
	c := accentColor(x)
	l := ref.luminance(x)
	onPaper := contrastOf(ref.paper, l)
	onInk := contrastOf(l, ref.ink)
	direct := math.Max(onPaper, onInk) >= 4.5
	byBounds := l <= ref.paperBound || l >= ref.inkBound

	tl.colours++
	tl.minBoundaryMargin = math.Min(tl.minBoundaryMargin, math.Min(math.Abs(l-ref.paperBound),
		math.Min(math.Abs(l-ref.inkBound), math.Abs(l-ref.edgeBound))))
	tl.minCrossoverMargin = math.Min(tl.minCrossoverMargin, math.Abs(l-ref.crossover))

	fill, err := Check(c)
	accepted := err == nil
	if !accepted {
		tl.refused++
	}
	if accepted != direct {
		tl.fail("Check disagrees with the direct 4.5:1 test", x)
	}
	if direct != byBounds {
		tl.fail("the computed boundaries disagree with the direct 4.5:1 test", x)
	}
	if !accepted && (!errors.Is(err, ErrAccentIllegible) || fill != (Fill{})) {
		tl.fail("a refusal is not (Fill{}, ErrAccentIllegible)", x)
	}
	text := OnColor(c)
	if (text == paper) != (onPaper >= onInk) || (text != paper && text != ink) {
		tl.fail("OnColor is not the higher-contrast of paper and ink", x)
	}
	edgeDirect := contrastOf(l, ref.porcelain) < 3
	if Edge(c) != edgeDirect {
		tl.fail("Edge disagrees with the direct 3:1 test", x)
	}
	if edgeDirect != (l > ref.edgeBound) {
		tl.fail("the computed edge boundary disagrees with the direct 3:1 test", x)
	}
	if accepted {
		if fill != (Fill{Accent: c, Text: text, Edge: Edge(c)}) {
			tl.fail("Fill is not (the accent, OnColor, Edge)", x)
		}
		// ADR 0023 §3's three classes of an accepted accent
		switch {
		case l <= ref.paperBound:
			if fill.Text != paper || fill.Edge {
				tl.fail("class 1 (L <= paper boundary) is not paper text without an edge", x)
			}
			tl.minPaperClassPorcelain = math.Min(tl.minPaperClassPorcelain, contrastOf(l, ref.porcelain))
		case l <= ref.edgeBound:
			if fill.Text != ink || fill.Edge {
				tl.fail("class 2 (ink boundary <= L <= edge boundary) is not ink text without an edge", x)
			}
		default:
			if fill.Text != ink || !fill.Edge {
				tl.fail("class 3 (L > edge boundary) is not ink text with an edge", x)
			}
		}
	}

	if back, err := ParseAccent(c.Hex()); err != nil || back != c {
		tl.fail("Hex does not ParseAccent back to the colour", x)
	}

	s := Suggest(c)
	if _, err := Check(s); err != nil {
		tl.fail("Suggest returned a colour Check refuses", x)
	}
	if s.R > c.R || s.G > c.G || s.B > c.B {
		tl.fail("Suggest raised a channel", x)
	}
	if accepted && s != c {
		tl.fail("Suggest changed a colour that passes", x)
	}
	p := accentPathOf(c)
	if p.at(p.top) != c {
		tl.fail("the path's top step is not the colour itself", x)
	}
	if !accepted {
		q, u := accentSuggestStep(c)
		if q.at(u) != s {
			tl.fail("Suggest is not the colour at the bisection's step", x)
		}
		if u+1 > q.top {
			tl.fail("the bisection's step is the refused colour itself", x)
		} else if _, err := Check(q.at(u + 1)); err == nil {
			tl.fail("the next brighter step on the path also passes", x)
		}
	}
}

// accentScanStride is how far apart the exhaustive scan's colours are: the scan
// visits x = k*stride for k = 0, 1, ... below 2^24. The stride is 1 (2^24
// iterations, x = 0 .. 2^24-1) unless the test binary carries coverage counters,
// then 61 (275 037 iterations; a step below 256 and odd, so the sample includes
// each value of each channel).
//
// WHY. Measured on the development machine (darwin/amd64, Intel, 16 hardware
// threads, Go 1.27.1), 2026-10-02/03; the duration depends on the shard count
// (GOMAXPROCS) and on what else the machine is running, so each figure carries
// its condition:
//
//	plain, 16 shards ............................ 1.5 s
//	-race, 16 shards, 1-min load average 2.5 .... 24.8 s
//	-race, 4 shards (GOMAXPROCS=4), load ~8 ..... 18.9 s (16.3 s in an earlier run)
//	-race, 16 shards, load ~8 / ~54 ............. 24.3 s / 34.6 s (WL-2 audit, round 1)
//
// Under -race -cover — which makes coverage mode "atomic", an atomic increment per
// basic block — 2^17 colours alone took 8.2 s on one goroutine (0.34 s under -race
// alone), and the full scan did not finish inside go test's 10-minute default
// timeout. `make test`, which CI runs, has no -cover, so CI's run makes the 2^24
// iterations (the test asserts the ITERATION count of a run without coverage; it
// does not count distinct colours); `make cover` and `go test -cover` scan the
// sample.
func accentScanStride() int {
	if testing.CoverMode() != "" {
		return 61
	}
	return 1
}

// TestAccent_EveryColourAgreesWithTheBoundariesThePaletteImplies runs the colours
// x = k*stride (2^24 iterations, x = 0 .. 2^24-1, at stride 1) through the
// production functions and the reference, and counts disagreements per property.
// Each count must be 0. What is compared, for each scanned colour:
//   - Check against the direct "better text reaches 4.5:1" test, and both against
//     the boundaries computed from the palette (paper boundary < L < ink boundary
//     is the refused band);
//   - OnColor against "the higher contrast of paper and ink", Edge against the
//     direct 3:1 test and against the computed edge boundary;
//   - an accepted colour's Fill, and the class ADR 0023 §3 puts it in;
//   - Hex parsing back to the colour;
//   - Suggest: Check accepts its output, it raises no channel, it returns a passing
//     colour unchanged, it is the bisection's step, and the step one grid point
//     brighter is refused (the step is the brightest passing one); the path's top
//     step is the colour itself.
//
// It also measures the smallest distance from a scanned colour's luminance to a
// gate boundary and requires it above 1e-12, so that a float64 rounding difference
// (a few 1e-17 per operation at this magnitude) is shown too small to move a
// scanned colour across one.
//
// 🔴 2^24 ITERATIONS, EXCEPT UNDER COVERAGE INSTRUMENTATION — see accentScanStride for
// the measurement. The log line says which run this was.
func TestAccent_EveryColourAgreesWithTheBoundariesThePaletteImplies(t *testing.T) {
	t.Parallel()
	ref := contrastReferenceFromRepo(t)
	started := time.Now()
	stride := accentScanStride()
	const total = 1 << 24
	samples := (total + stride - 1) / stride
	shards := runtime.GOMAXPROCS(0)
	per := (samples + shards - 1) / shards
	tallies := make([]*accentTally, shards)
	var wg sync.WaitGroup
	for i := 0; i < shards; i++ {
		tallies[i] = accentNewTally()
		lo := i * per
		hi := min(lo+per, samples)
		wg.Add(1)
		go func(tl *accentTally, lo, hi int) {
			defer wg.Done()
			for k := lo; k < hi; k++ {
				accentCheckColour(ref, uint32(k*stride), tl)
			}
		}(tallies[i], lo, hi)
	}
	wg.Wait()
	all := accentNewTally()
	for _, tl := range tallies {
		all.merge(tl)
	}

	t.Logf("%d colours (every %d; coverage mode %q) in %v over %d shards; refused %d; smallest distance to a gate boundary %.3g, to the crossover %.3g; smallest porcelain contrast of a paper-text accent %.4f:1",
		all.colours, stride, testing.CoverMode(), time.Since(started).Round(time.Millisecond), shards, all.refused,
		all.minBoundaryMargin, all.minCrossoverMargin, all.minPaperClassPorcelain)
	if all.colours != samples {
		t.Fatalf("scanned %d colours, want %d", all.colours, samples)
	}
	// The test's name and WL-2's acceptance rest on CI's run making 2^24 iterations;
	// CI runs without coverage, so a run without coverage must have made them. This
	// counts iterations (accentCheckColour calls), not distinct colours: the colour
	// of iteration k is k*stride, which is distinct per k by construction, and that
	// construction is what this assertion does not re-check.
	if testing.CoverMode() == "" && all.colours != total {
		t.Fatalf("coverage is off, yet the scan made %d iterations, not the %d it exists for", all.colours, total)
	}
	names := make([]string, 0, len(all.violations))
	for k := range all.violations {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		t.Errorf("%s: %d colour(s), e.g. %06X", k, all.violations[k], all.example[k])
	}
	if all.refused == 0 || all.refused == samples {
		t.Errorf("refused %d of %d colours; the scan is not exercising the gate", all.refused, samples)
	}
	if all.minBoundaryMargin < 1e-12 {
		t.Errorf("a colour lies %.3g from a gate boundary; float64 rounding could move it across", all.minBoundaryMargin)
	}
	// ADR 0023 §3: an accepted paper-text accent is at least 3,98:1 against porcelain.
	if math.Floor(all.minPaperClassPorcelain*100)/100 < 3.98 {
		t.Errorf("a paper-text accent is %.4f:1 against porcelain; ADR 0023 section 3 says >= 3,98", all.minPaperClassPorcelain)
	}
}

// ---------------------------------------------------------------------------
// Suggest
// ---------------------------------------------------------------------------

// TestSuggest_TheDesignExamples: the suggestions for the ADR's refused examples and
// the colours either side of the boundaries. The expected values were computed
// INDEPENDENTLY of accent.go, with exact rationals (Python's fractions): the
// lightnesses where a channel of the HSL path crosses n + 1/2 enumerated, the colour
// on each interval rounded half up, the brightest one whose better text contrast
// reaches 4.5:1 kept. #808080 also checks by hand: a grey's path is grey, and
// 0x75 = 117 is the brightest grey level whose luminance is below the paper
// boundary (0x76 is 0.1811). A colour that passes comes back unchanged.
func TestSuggest_TheDesignExamples(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want uint32
	}{
		{0x808080, 0x757575},
		{0xE0457B, 0xDC2A68},
		{0xFF0000, 0xEC0000},
		{0x1E93A0, 0x1A818D},
		{0x7D5BEC, 0x7D5AEC},
		{0x007EC6, 0x007AC0},
		{0x0085D1, 0x007AC0},
		{0x0088A8, 0x007F9D},
		{0x008A2E, 0x00882D},
		// passing colours
		{0xDA291C, 0xDA291C},
		{0xFFC72C, 0xFFC72C},
		{paletteTappaGreenHex, paletteTappaGreenHex},
		{0x000000, 0x000000},
		{0xFFFFFF, 0xFFFFFF},
	}
	for _, tc := range tests {
		got := Suggest(accentColor(tc.in))
		if accentHexOf(got) != tc.want {
			t.Errorf("Suggest(%06X) = %s, want %06X", tc.in, got.Hex(), tc.want)
		}
	}
}

// accentTextbookHSL is the usual RGB -> HSL conversion (hue in degrees). It is
// written the way references print it and does not call accentPath.
func accentTextbookHSL(c Color) (h, s, l float64) {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	hi, lo := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	l = (hi + lo) / 2
	if hi == lo {
		return 0, 0, l
	}
	d := hi - lo
	if l > 0.5 {
		s = d / (2 - hi - lo)
	} else {
		s = d / (hi + lo)
	}
	switch hi {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	return h * 60, s, l
}

// accentTextbookChannels is the usual HSL -> RGB conversion, unrounded (0..1).
func accentTextbookChannels(h, s, l float64) [3]float64 {
	chroma := (1 - math.Abs(2*l-1)) * s
	hp := h / 60
	x := chroma * (1 - math.Abs(math.Mod(hp, 2)-1))
	var r, g, b float64
	switch {
	case hp < 1:
		r, g, b = chroma, x, 0
	case hp < 2:
		r, g, b = x, chroma, 0
	case hp < 3:
		r, g, b = 0, chroma, x
	case hp < 4:
		r, g, b = 0, x, chroma
	case hp < 5:
		r, g, b = x, 0, chroma
	default:
		r, g, b = chroma, 0, x
	}
	m := l - chroma/2
	return [3]float64{r + m, g + m, b + m}
}

// accentTextbookRGB is accentTextbookChannels rounded half up. tie reports a
// channel within 1e-6 of a rounding boundary, where float64 error may round it
// either way.
func accentTextbookRGB(h, s, l float64) (c Color, tie bool) {
	var out [3]uint8
	for i, v := range accentTextbookChannels(h, s, l) {
		v *= 255
		if frac := v - math.Floor(v); math.Abs(frac-0.5) < 1e-6 {
			tie = true
		}
		out[i] = uint8(math.Max(0, math.Min(255, math.Floor(v+0.5))))
	}
	return Color{R: out[0], G: out[1], B: out[2]}, tie
}

// accentExactPath lists, darkest first, the colours the HSL path at or below c
// takes, found WITHOUT a grid: on that path each channel is piecewise linear in
// lightness (one piece each side of 1/2), so the lightnesses where a channel
// crosses n + 1/2 are solved for directly, and the colour on each interval between
// two consecutive ones is read at the interval's midpoint, half an interval away
// from the nearest crossing. shortest is the shortest interval seen; tie reports a
// midpoint that nevertheless landed within 1e-6 of a rounding boundary, where
// float64 error could round it the other way (the enumeration would then be
// untrustworthy).
func accentExactPath(c Color) (colours []Color, shortest float64, tie bool) {
	h, s, l := accentTextbookHSL(c)
	half := accentTextbookChannels(h, s, 0.5)
	points := []float64{0, l}
	for i := range half {
		a := half[i] - 0.5 // channel(l') = l' + rho(l') * a
		for n := 0; n < 256; n++ {
			target := (float64(n) + 0.5) / 255
			if k := 1 + 2*a; k > 0 {
				if lp := target / k; lp > 0 && lp <= 0.5 && lp < l {
					points = append(points, lp)
				}
			}
			if k := 1 - 2*a; k != 0 {
				if lp := (target - 2*a) / k; lp > 0.5 && lp < l {
					points = append(points, lp)
				}
			}
		}
	}
	sort.Float64s(points)
	// Coincident crossings (0x007EC6's G and B at 35/36) come out of float64 a few
	// ulps apart; distinct ones are at least 1/260100^2 ~ 1.5e-11 apart (accent.go).
	merged := points[:1]
	for _, p := range points[1:] {
		if p-merged[len(merged)-1] > 1e-14 {
			merged = append(merged, p)
		}
	}
	shortest = math.Inf(1)
	for i := 1; i < len(merged); i++ {
		lo, hi := merged[i-1], merged[i]
		shortest = math.Min(shortest, hi-lo)
		col, t := accentTextbookRGB(h, s, lo+(hi-lo)/2)
		tie = tie || t
		if len(colours) == 0 || colours[len(colours)-1] != col {
			colours = append(colours, col)
		}
	}
	if len(colours) == 0 || colours[len(colours)-1] != c {
		colours = append(colours, c)
	}
	return colours, shortest, tie
}

// accentTextbookSuggest is Suggest's bisection done in float64 on the textbook
// conversion — the implementation accent.go's "WHY INTEGERS" paragraph declines.
func accentTextbookSuggest(c Color) Color {
	if _, err := Check(c); err == nil {
		return c
	}
	h, s, l := accentTextbookHSL(c)
	lo, hi := 0.0, l
	for {
		mid := lo + (hi-lo)/2
		if mid <= lo || mid >= hi {
			break
		}
		cand, _ := accentTextbookRGB(h, s, mid)
		if _, err := Check(cand); err == nil {
			lo = mid
		} else {
			hi = mid
		}
	}
	out, _ := accentTextbookRGB(h, s, lo)
	return out
}

// TestSuggest_ThePathIsTextbookHSL shows that accentPath is HSL lightness with hue
// and saturation held: on a sample of colours (every 4099th) and 257 lightness
// steps of each, the integer path and the textbook
// float conversion give the same colour wherever the float one is not within 1e-6
// of a rounding tie; along the steps no channel decreases. It then pins
// the tie accent.go's "WHY INTEGERS" paragraph names: for 0x007EC6 the float
// bisection suggests 0x007AC1, a colour that passes but that the exact path does
// not take (one grid step before t = 35/36 it is 0x007AC0, at it 0x007BC1).
func TestSuggest_ThePathIsTextbookHSL(t *testing.T) {
	t.Parallel()
	const steps = 256
	compared, skipped, mismatched, decreasing := 0, 0, 0, 0
	for x := uint32(0); x < 1<<24; x += 4099 {
		c := accentColor(x)
		p := accentPathOf(c)
		h, s, _ := accentTextbookHSL(c)
		prev := Color{}
		for k := int64(0); k <= steps; k++ {
			u := p.top / steps * k
			if k == steps {
				u = p.top
			}
			got := p.at(u)
			if got.R < prev.R || got.G < prev.G || got.B < prev.B {
				decreasing++
				if decreasing <= 3 {
					t.Errorf("%06X: step %d is %s, darker in a channel than step %d's %s", x, k, got.Hex(), k-1, prev.Hex())
				}
			}
			prev = got
			want, tie := accentTextbookRGB(h, s, float64(u)/float64(accentPathScale))
			if tie {
				skipped++
				continue
			}
			compared++
			if got != want {
				mismatched++
				if mismatched <= 3 {
					t.Errorf("%06X at lightness %d/U: integer path %s, textbook %s", x, u, got.Hex(), want.Hex())
				}
			}
		}
	}
	t.Logf("compared %d points, skipped %d near a rounding tie", compared, skipped)
	if mismatched != 0 || decreasing != 0 {
		t.Errorf("%d mismatches, %d decreasing steps", mismatched, decreasing)
	}
	if compared < 500000 {
		t.Errorf("only %d points compared; the sample is not exercising the path", compared)
	}

	// Black and white, the two colours accentPathOf gives d = 1: their paths are
	// greys. White at half lightness is 127.5 per channel, rounded half up.
	for _, g := range []struct {
		from, step string
		u          int64
		want       uint32
	}{
		{"000000", "top", 0, 0x000000},
		{"FFFFFF", "top", accentPathScale, 0xFFFFFF},
		{"FFFFFF", "half", accentPathScale / 2, 0x808080},
		{"FFFFFF", "zero", 0, 0x000000},
	} {
		c, err := ParseAccent(g.from)
		if err != nil {
			t.Fatal(err)
		}
		if got := accentPathOf(c).at(g.u); got != accentColor(g.want) {
			t.Errorf("path of %s at %s: %s, want %06X", g.from, g.step, got.Hex(), g.want)
		}
	}

	// The tie: 0x007EC6 has lightness 198/510, below one half, so its path scales
	// the three channels by t. G = 126t reaches 122.5 and B = 198t reaches 192.5 at the
	// same t = 35/36, i.e. u = 35/36 * top = 192.5 * 2^shift = 385 * 2^(shift-1).
	c := accentColor(0x007EC6)
	p := accentPathOf(c)
	tieStep := int64(385) << (accentPathShift - 1)
	if p.top/36*35 != tieStep {
		t.Fatalf("35/36 of the top step is %d, want %d", p.top/36*35, tieStep)
	}
	if got := p.at(tieStep - 1); got != accentColor(0x007AC0) {
		t.Errorf("one step before the tie: %s, want 007AC0", got.Hex())
	}
	if got := p.at(tieStep); got != accentColor(0x007BC1) {
		t.Errorf("at the tie: %s, want 007BC1", got.Hex())
	}
	if got := Suggest(c); got != accentColor(0x007AC0) {
		t.Errorf("Suggest(007EC6) = %s, want 007AC0", got.Hex())
	}
	if got := accentTextbookSuggest(c); got != accentColor(0x007AC1) {
		t.Errorf("the float bisection suggests %s; accent.go's comment says 007AC1", got.Hex())
	}
	if _, err := Check(accentColor(0x007AC1)); err != nil {
		t.Errorf("007AC1 is refused; the comment's point is that the float path offers a passing colour off the exact path")
	}
	if _, err := Check(accentColor(0x007BC1)); err == nil {
		t.Errorf("007BC1 passes; then 007AC0 would not be the brightest passing colour on the path")
	}
}

// TestSuggest_IsTheBrightestPassingColourOnTheExactPath checks "the nearest" of
// ADR 0023 §3 against an enumeration that shares neither accent.go's integer grid
// nor its bisection: for a sample of refused colours (every 2003rd colour, the
// refused ones among them), accentExactPath lists the colours the path takes,
// the colours that pass must be a prefix of that list (the path darkens
// monotonically), and Suggest must return the last of them.
//
// It also measures the shortest interval a sampled path holds a colour for and
// requires it to be wider than accent.go's grid step 1/accentPathScale: the grid
// step being finer than each interval is what lets the bisection reach each
// colour (accent.go, "WHY THIS FINE"), and this is the measured side of that
// arithmetic, on the sample. The arithmetic side is pinned first: the grid must be
// finer than 1/260100^2, the smallest gap the comment derives between two
// different crossings. (The measured shortest interval is far wider than that
// bound, so the sample by itself does not tell a grid at the bound from a coarser one;
// the pin is what holds the comment's guarantee.)
func TestSuggest_IsTheBrightestPassingColourOnTheExactPath(t *testing.T) {
	t.Parallel()
	const crossingDenominator = 510 * 510 // accent.go: a crossing is p/q with q <= 510*510
	if accentPathScale <= crossingDenominator*crossingDenominator {
		t.Errorf("accentPathScale = %d is not above %d^2 = %d: two crossings can fall between grid steps",
			accentPathScale, crossingDenominator, crossingDenominator*crossingDenominator)
	}
	checked, shortestSeen := 0, math.Inf(1)
	for x := uint32(0); x < 1<<24; x += 2003 {
		c := accentColor(x)
		if _, err := Check(c); err == nil {
			continue
		}
		path, shortest, tie := accentExactPath(c)
		if tie {
			t.Errorf("%06X: a midpoint landed on a rounding tie; the enumeration is not trustworthy here", x)
			continue
		}
		shortestSeen = math.Min(shortestSeen, shortest)
		want, failedOnce := Color{}, false
		for _, p := range path {
			_, err := Check(p)
			switch {
			case err == nil && failedOnce:
				t.Errorf("%06X: %s passes after a darker colour on the path failed", x, p.Hex())
			case err == nil:
				want = p
			default:
				failedOnce = true
			}
		}
		if got := Suggest(c); got != want {
			t.Errorf("Suggest(%06X) = %s; the brightest passing colour on the exact path is %s", x, got.Hex(), want.Hex())
		}
		checked++
	}
	step := 1 / float64(accentPathScale)
	t.Logf("%d refused colours; shortest interval on their paths %.3g, grid step %.3g", checked, shortestSeen, step)
	if checked < 500 {
		t.Errorf("only %d refused colours checked; the sample is not exercising Suggest", checked)
	}
	if shortestSeen <= step {
		t.Errorf("a path holds a colour for %.3g of lightness, not wider than the grid step %.3g: the bisection can step over it", shortestSeen, step)
	}
}

// TestSuggest_IsDeterministicUnderConcurrency: the same refused colour gets the same
// suggestion from 32 goroutines at once, and from a sequential call before them.
// Under -race this is also the check that Suggest shares no mutable state.
func TestSuggest_IsDeterministicUnderConcurrency(t *testing.T) {
	t.Parallel()
	inputs := []uint32{0x808080, 0xE0457B, 0xFF0000, 0x1E93A0, 0x7D5BEC, 0x007EC6, 0x22864B, 0x008384}
	want := make([]Color, len(inputs))
	for i, x := range inputs {
		want[i] = Suggest(accentColor(x))
	}
	var wg sync.WaitGroup
	errs := make(chan string, 32*len(inputs))
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i, x := range inputs {
				if got := Suggest(accentColor(x)); got != want[i] {
					errs <- fmt.Sprintf("Suggest(%06X) = %s, earlier %s", x, got.Hex(), want[i].Hex())
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}
