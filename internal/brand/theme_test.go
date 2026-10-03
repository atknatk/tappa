package brand

// theme_test.go -- M10 WL-5: the theme stylesheet's body (ThemeCSS) and three reads
// of the COMPILED stylesheet for ADR 0023's claims B and C: two through a CSS parser
// (where the accent is read and in which property; what the defaults are) and one
// that parses nothing (TestCompiledCSS_BrandNamesOccurOnlyInTheGolden: where the
// accent's names occur at all).
//
// The compiled-stylesheet tests read web/static/css/app.css as the binary embeds
// it. That file is built by `make css` and is gitignored: CI builds it before `make
// check` (.github/workflows/ci.yml, "Build the stylesheet (make css)"), so there
// they run; a local run without a prior `make css` SKIPs them, and a skip is not a
// pass. Their negative controls, TestThemeSlotScan_RefusesEachShapeItExistsFor and
// TestThemeBrandScan_RefusesEachShapeItExistsFor, need no app.css: they feed the
// reads the compiled tests apply the shapes listed in them, so a read that reports
// one of those shapes as clean turns its control red.

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/atknatk/tappa/web"
)

// themeBodyRE is ADR 0023 claim A's body: one :root rule, the three properties in
// this order, each an "R G B" decimal triple; --brand-edge may instead be none.
var themeBodyRE = regexp.MustCompile(`^:root\{` +
	`--brand-accent:(\d{1,3}) (\d{1,3}) (\d{1,3});` +
	`--brand-on-accent:(\d{1,3}) (\d{1,3}) (\d{1,3});` +
	`--brand-edge:(?:(\d{1,3}) (\d{1,3}) (\d{1,3})|none)\}$`)

// themeDecimal is the canonical decimal of one channel, written by this test with
// fmt rather than by themeTriple.
func themeDecimal(c Color) string { return fmt.Sprintf("%d %d %d", c.R, c.G, c.B) }

// themeHexColour reads a "RRGGBB" with strconv, not with accentDecode.
func themeHexColour(t *testing.T, hex string) Color {
	t.Helper()
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil || len(hex) != 6 {
		t.Fatalf("PREMISE: %q is not six hex digits", hex)
	}
	return accentColor(uint32(v))
}

// TestTheme_TheBodyIsTheGatesFill asks ThemeCSS about ADR 0023 §3's seven table
// colours, every 4099th 24-bit colour from black and the 256 greys. For each it
// asserts: an accent Check refuses gets no body, only ErrAccentIllegible; an accepted
// one gets exactly the grammar of claim A, its own three bytes as --brand-accent,
// OnColor's paper or ink as --brand-on-accent, and ink when Edge says so, otherwise
// none, as --brand-edge. For the table rows it also asserts that the gate agrees
// with the table's label and edge.
//
// PART II -- what it catches: a body for a refused accent; a body outside the
// grammar (a fourth property, a trailing byte, another order, a non-canonical
// decimal); a wrong channel, label or edge on a visited colour, including an edge
// drawn for a visited no-edge accent in any colour (the accent's own included) --
// the greys are visited because the 4099 stride meets no grey but black.
// PART III: a form not on that list is code review's -- no completeness claim.
func TestTheme_TheBodyIsTheGatesFill(t *testing.T) {
	t.Parallel()
	paper, ink := paletteColor(palettePaperHex), paletteColor(paletteInkHex)

	check := func(where string, c Color, body string) {
		t.Helper()
		m := themeBodyRE.FindStringSubmatch(body)
		if m == nil {
			t.Errorf("%s: %q is not ADR 0023's three properties on :root", where, body)
			return
		}
		for _, g := range m[1:] {
			if g == "" {
				continue
			}
			if n, err := strconv.Atoi(g); err != nil || n > 255 || strconv.Itoa(n) != g {
				t.Errorf("%s: %q is not a canonical 0-255 decimal", where, g)
			}
		}
		if got := strings.Join(m[1:4], " "); got != themeDecimal(c) {
			t.Errorf("%s: --brand-accent %q, want %q", where, got, themeDecimal(c))
		}
		if got, want := strings.Join(m[4:7], " "), themeDecimal(OnColor(c)); got != want {
			t.Errorf("%s: --brand-on-accent %q, want %q", where, got, want)
		}
		edge := "none"
		if m[7] != "" {
			edge = strings.Join(m[7:10], " ")
		}
		want := "none"
		if Edge(c) {
			want = themeDecimal(ink)
		}
		if edge != want {
			t.Errorf("%s: --brand-edge %q, want %q", where, edge, want)
		}
	}

	// ADR 0023 §3's table, as the table says it: label and edge.
	for _, row := range []struct {
		hex    string
		ok     bool
		text   Color
		edge   bool
		source string
	}{
		{"808080", false, Color{}, false, "red"},
		{"E0457B", false, Color{}, false, "red"},
		{"DA291C", true, paper, false, "paper text"},
		{"FFC72C", true, ink, true, "ink text + ink edge"},
		{"1F5C41", true, paper, false, "tappa-green, the default"},
		{"BE3D2A", true, paper, false, "tomato, paper text"},
		{"D98E2B", true, ink, true, "saffron, ink text + ink edge"},
	} {
		c := themeHexColour(t, row.hex)
		body, err := ThemeCSS(c)
		if !row.ok {
			if !errors.Is(err, ErrAccentIllegible) || body != "" {
				t.Errorf("%s (%s): ThemeCSS = %q, %v; want no body and ErrAccentIllegible", row.hex, row.source, body, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s (%s): %v", row.hex, row.source, err)
			continue
		}
		if OnColor(c) != row.text || Edge(c) != row.edge {
			t.Errorf("PREMISE %s: the gate disagrees with ADR 0023 section 3's row (%s)", row.hex, row.source)
		}
		check(row.hex+" ("+row.source+")", c, body)
	}

	// The sweep: every 4099th 24-bit colour from black, then the 256 greys. Each part
	// must meet all four of the gate's answers (refused, paper label, ink label, ink
	// label with edge).
	visit := func(seen map[string]int, c Color) {
		t.Helper()
		body, err := ThemeCSS(c)
		if _, gate := Check(c); gate != nil {
			if !errors.Is(err, ErrAccentIllegible) || body != "" {
				t.Fatalf("%s: Check refuses, ThemeCSS = %q, %v", c.Hex(), body, err)
			}
			seen["refused"]++
			return
		}
		if err != nil {
			t.Fatalf("%s: Check accepts, ThemeCSS: %v", c.Hex(), err)
		}
		check(c.Hex(), c, body)
		seen[fmt.Sprintf("text=%s edge=%v", OnColor(c).Hex(), Edge(c))]++
	}
	stride, greys := map[string]int{}, map[string]int{}
	for x := uint32(0); x < 1<<24; x += 4099 {
		visit(stride, accentColor(x))
	}
	for v := 0; v < 256; v++ {
		visit(greys, Color{R: uint8(v), G: uint8(v), B: uint8(v)})
	}
	if len(stride) != 4 || len(greys) != 4 {
		t.Fatalf("PREMISE: the stride met %d and the greys %d of the four answers (%v, %v)", len(stride), len(greys), stride, greys)
	}
	t.Logf("stride: %v; greys: %v", stride, greys)
}

// --- the compiled stylesheet -------------------------------------------------

// themeVariableProperty is ADR 0023 §3's property rule: the property each variable may
// be read in. The accent is a fill, its label a text colour and its edge the
// inset box shadow input.css draws it with (a mechanism that does not change the
// button's box, §3). Its keys are held equal to the variables ThemeCSS declares.
var themeVariableProperty = map[string]string{
	"--brand-accent":    "background-color",
	"--brand-on-accent": "color",
	"--brand-edge":      "box-shadow",
}

// themeSlots is ADR 0023 §2's accent slots as they exist in input.css, each with
// the variables it reads. The tap button's one class serves the tap screen and the
// Account preview (§2: the preview renders the tap screen's own button). The panel's
// stripe is a slot too and has no class yet (WL-8); it enters here when it does,
// with --brand-accent only. A listed (slot, variable) pair the compiled stylesheet
// does not read in its property is reported (rule 4 of themeSlotViolations).
var themeSlots = map[string][]string{
	".tap-button": {"--brand-accent", "--brand-on-accent", "--brand-edge"},
}

// themeCSSRule is one rule of a compiled stylesheet: its enclosing at-rules, its
// prelude (a selector list or an at-rule), its declarations, and -- for a rule with a
// declaration block -- its text as written: the prelude without comments, then the
// block from '{' to '}' byte for byte. Selector, properties and values are kept as
// written; themeSlotViolations decodes CSS escapes before it reads them.
type themeCSSRule struct {
	context  []string
	selector string
	decls    [][2]string // property, value
	raw      string
}

type themeCSSParser struct {
	src string
	i   int
}

// themeParseCSS reads a stylesheet into flat rules. It knows comments, strings and
// parentheses, and it returns an error for the malformed shapes it recognises -- an
// unterminated comment or string, an unclosed block, text outside a rule, a
// declaration without a colon -- rather than skipping them.
func themeParseCSS(src string) ([]themeCSSRule, error) {
	p := &themeCSSParser{src: src}
	rules, err := p.list(nil)
	if err != nil {
		return nil, err
	}
	if p.i != len(src) {
		return nil, fmt.Errorf("unbalanced '}' at byte %d", p.i)
	}
	return rules, nil
}

// CSS's comment delimiters, together on two lines. Spelled as constants because
// cmd/tappa's TestEveryNamedTestExists strips Go block comments with a regexp that
// does not know Go strings: an opener left alone in a string literal here hid the
// test declarations between it and the next closer (measured on this file's first
// draft: three declared tests read as missing).
const (
	themeCommentOpen  = "/*"
	themeCommentClose = "*/"
)

// The kinds of atom.
const (
	themeNoAtom = iota
	themeComment
	themeString
)

// atom moves past the comment or string at p.i, if there is one, and says which.
func (p *themeCSSParser) atom() (int, error) {
	s := p.src
	if strings.HasPrefix(s[p.i:], themeCommentOpen) {
		end := strings.Index(s[p.i+2:], themeCommentClose)
		if end < 0 {
			return 0, fmt.Errorf("unterminated comment at byte %d", p.i)
		}
		p.i += end + 4
		return themeComment, nil
	}
	if q := s[p.i]; q == '"' || q == '\'' {
		j := p.i + 1
		for j < len(s) && s[j] != q {
			if s[j] == '\\' {
				j++
			}
			j++
		}
		if j >= len(s) {
			return 0, fmt.Errorf("unterminated string at byte %d", p.i)
		}
		p.i = j + 1
		return themeString, nil
	}
	return themeNoAtom, nil
}

// themeWithoutComments is s with its comments removed; strings are kept whole.
func themeWithoutComments(s string) (string, error) {
	var b strings.Builder
	p := &themeCSSParser{src: s}
	for p.i < len(s) {
		from := p.i
		kind, err := p.atom()
		if err != nil {
			return "", err
		}
		switch kind {
		case themeComment:
		case themeString:
			b.WriteString(s[from:p.i])
		default:
			b.WriteByte(s[p.i])
			p.i++
		}
	}
	return b.String(), nil
}

// scan moves p.i to the first byte of stops at parenthesis depth 0 outside comments,
// strings and escapes, and returns that byte (0 at the end of the text). scan does
// not treat a backslash or the byte after it as structural: `\;` or `\}` is part of
// a name.
func (p *themeCSSParser) scan(stops string) (byte, error) {
	depth := 0
	for p.i < len(p.src) {
		if kind, err := p.atom(); err != nil {
			return 0, err
		} else if kind != themeNoAtom {
			continue
		}
		c := p.src[p.i]
		switch {
		case c == '\\':
			p.i += 2
			continue
		case c == '(':
			depth++
		case c == ')':
			depth--
		case depth == 0 && strings.IndexByte(stops, c) >= 0:
			return c, nil
		}
		p.i++
	}
	return 0, nil
}

func (p *themeCSSParser) list(ctx []string) ([]themeCSSRule, error) {
	var out []themeCSSRule
	for {
		for p.i < len(p.src) && strings.IndexByte(" \t\r\n", p.src[p.i]) >= 0 {
			p.i++
		}
		if p.i == len(p.src) || p.src[p.i] == '}' {
			return out, nil
		}
		start := p.i
		stop, err := p.scan("{;}")
		if err != nil {
			return nil, err
		}
		// A comment is not part of the selector; a --brand- in one is counted by
		// themeSlotViolations as outside the declarations (its total includes comments).
		prelude, err := themeWithoutComments(p.src[start:p.i])
		if err != nil {
			return nil, err
		}
		prelude = strings.TrimSpace(prelude)
		switch stop {
		case ';':
			out = append(out, themeCSSRule{context: ctx, selector: prelude})
			p.i++
			continue
		case 0, '}':
			if prelude == "" { // only comments before the end of the block
				return out, nil
			}
			return nil, fmt.Errorf("text outside a rule at byte %d: %q", start, prelude)
		}
		p.i++ // '{'
		// A block holds declarations unless a '{' comes before its own '}'.
		open := p.i
		inner, err := p.scan("{}")
		if err != nil {
			return nil, err
		}
		if inner == '{' {
			p.i = open
			nested, err := p.list(append(slices.Clip(ctx), prelude))
			if err != nil {
				return nil, err
			}
			out = append(out, nested...)
			if p.i == len(p.src) {
				return nil, fmt.Errorf("unclosed block %q", prelude)
			}
			p.i++ // '}'
			continue
		}
		if inner != '}' {
			return nil, fmt.Errorf("unclosed block %q", prelude)
		}
		rule := themeCSSRule{context: ctx, selector: prelude, raw: prelude + p.src[open-1:p.i+1]}
		body := &themeCSSParser{src: p.src[open:p.i]}
		for body.i < len(body.src) {
			from := body.i
			if _, err := body.scan(";"); err != nil {
				return nil, err
			}
			// A comment is not part of a declaration (a definition behind one is still a
			// definition), and a --brand- inside one is then counted as outside the
			// declarations (rule 1 of themeSlotViolations).
			decl, err := themeWithoutComments(body.src[from:body.i])
			if err != nil {
				return nil, err
			}
			decl = strings.TrimSpace(decl)
			body.i++
			if decl == "" {
				continue
			}
			prop, value, ok := themeCutColon(decl)
			if !ok {
				return nil, fmt.Errorf("a declaration without a colon in %q: %q", prelude, decl)
			}
			rule.decls = append(rule.decls, [2]string{strings.TrimSpace(prop), strings.TrimSpace(value)})
		}
		out = append(out, rule)
		p.i++ // '}'
	}
}

// themeCutColon splits a declaration at its first colon that is not escaped.
func themeCutColon(decl string) (string, string, bool) {
	for i := 0; i < len(decl); i++ {
		switch decl[i] {
		case '\\':
			i++
		case ':':
			return decl[:i], decl[i+1:], true
		}
	}
	return decl, "", false
}

// themeEscapeAt reads the CSS escape whose backslash is s[i] the way a browser reads
// one in a name (CSS Syntax Level 3, "consume an escaped code point") and returns the
// code point it stands for and the bytes it spans: a backslash with one to six hex
// digits and at most one following whitespace (CRLF as one) is that code point (0, a
// surrogate or past U+10FFFF is U+FFFD); a backslash before a newline stands for
// nothing (-1); a backslash before any other character is that character; a backslash
// at the end of s is U+FFFD.
func themeEscapeAt(s string, i int) (rune, int) {
	if i+1 == len(s) {
		return utf8.RuneError, 1
	}
	isHex := func(c byte) bool {
		return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
	}
	j := i + 1
	for j < len(s) && j-i-1 < 6 && isHex(s[j]) {
		j++
	}
	if j > i+1 {
		cp, err := strconv.ParseUint(s[i+1:j], 16, 32)
		if err != nil || cp == 0 || cp > unicode.MaxRune || (cp >= 0xD800 && cp <= 0xDFFF) {
			cp = utf8.RuneError
		}
		switch {
		case strings.HasPrefix(s[j:], "\r\n"):
			j += 2
		case j < len(s) && strings.IndexByte(" \t\n\r\f", s[j]) >= 0:
			j++
		}
		return rune(cp), j - i
	}
	if s[i+1] == '\n' {
		return -1, 2
	}
	r, size := utf8.DecodeRuneInString(s[i+1:])
	return r, 1 + size
}

// themeUnescape decodes the CSS escapes in s (themeEscapeAt). Without this,
// `--brand\-accent` and `--brand\2d accent` are the accent to a browser and not to a
// substring search (the 4th-round audit measured both resolving to the accent in
// Chrome 154).
func themeUnescape(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			i++
			continue
		}
		r, n := themeEscapeAt(s, i)
		if r >= 0 {
			b.WriteRune(r)
		}
		i += n
	}
	return b.String()
}

// themeTopLevelCommas splits a value at the commas outside parentheses: a
// box-shadow list into its shadows.
func themeTopLevelCommas(value string) []string {
	var out []string
	depth, from := 0, 0
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, value[from:i])
				from = i + 1
			}
		}
	}
	return append(out, value[from:])
}

// themeVarRE is one --brand-* name inside a value or a property.
var themeVarRE = regexp.MustCompile(`--brand-[A-Za-z0-9_-]*`)

// themeSlotViolations lists the violations of four rules for the three variables in a
// stylesheet. It reads the RULES the stylesheet holds, not the classes a template
// uses, and it decodes escapes first (themeUnescape):
//
//  1. every --brand- in the text is inside a declaration (not in a selector, an
//     at-rule's prelude or a comment);
//  2. among the declarations this parser reads, each variable is DEFINED exactly
//     once, in a top-level `:root` rule (where the theme stylesheet overrides them;
//     §4), and no other --brand- name is defined;
//  3. a declaration that READS a variable reads a known one, in the one property
//     themeVariableProperty allows it, in a rule every selector of which is a slot
//     of themeSlots that may read that variable; and a shadow that reads the edge
//     is an inset one (ADR 0023 §3: the edge must not draw outside the button's box);
//  4. every (slot, variable) pair of themeSlots is read at least once in its
//     property.
func themeSlotViolations(css string) []string {
	rules, err := themeParseCSS(css)
	if err != nil {
		return []string{"unreadable stylesheet: " + err.Error()}
	}
	var out []string
	// Names are read decoded (themeUnescape), and so is the count they are reconciled
	// with: the escaped spellings the negative control feeds are the same names to
	// this scan as their plain spelling.
	total, inDecls := strings.Count(themeUnescape(css), "--brand-"), 0
	defined := map[string]int{}
	read := map[string]bool{}
	for _, r := range rules {
		selector := themeUnescape(r.selector)
		where := strings.Join(append(slices.Clip(r.context), selector), " ")
		for _, d := range r.decls {
			prop, value := themeUnescape(d[0]), themeUnescape(d[1])
			inDecls += strings.Count(prop, "--brand-") + strings.Count(value, "--brand-")
			if strings.HasPrefix(prop, "--brand-") {
				defined[prop]++
				switch {
				case themeVariableProperty[prop] == "":
					out = append(out, fmt.Sprintf("%s defines %s, which is not one of the three variables", where, prop))
				case len(r.context) != 0 || selector != ":root":
					out = append(out, fmt.Sprintf("%s defines %s outside the top-level :root rule", where, prop))
				}
			}
			for _, v := range themeVarRE.FindAllString(value, -1) {
				want, known := themeVariableProperty[v]
				if !known {
					out = append(out, fmt.Sprintf("%s reads %s, which is not one of the three variables", where, v))
					continue
				}
				if prop != want {
					out = append(out, fmt.Sprintf("%s reads %s in %s; it may be read only in %s", where, v, prop, want))
				}
				if prop == "box-shadow" && v == "--brand-edge" {
					for _, shadow := range themeTopLevelCommas(value) {
						if strings.Contains(shadow, v) && !slices.Contains(strings.Fields(shadow), "inset") {
							out = append(out, fmt.Sprintf("%s draws %s in a shadow that is not inset: %q", where, v, shadow))
						}
					}
				}
				for _, sel := range strings.Split(selector, ",") {
					sel = strings.TrimSpace(sel)
					if !slices.Contains(themeSlots[sel], v) {
						out = append(out, fmt.Sprintf("%s reads %s, and %q is not a slot that may read it", where, v, sel))
						continue
					}
					if prop == want {
						read[sel+" "+v] = true
					}
				}
			}
		}
	}
	if inDecls != total {
		out = append(out, fmt.Sprintf("%d of the stylesheet's %d occurrences of --brand- are outside any declaration", total-inDecls, total))
	}
	for v := range themeVariableProperty {
		if defined[v] != 1 {
			out = append(out, fmt.Sprintf("%s is defined %d times; it is defined once, on :root", v, defined[v]))
		}
	}
	for slot, vars := range themeSlots {
		for _, v := range vars {
			if !read[slot+" "+v] {
				out = append(out, fmt.Sprintf("slot %s never reads %s in %s", slot, v, themeVariableProperty[v]))
			}
		}
	}
	sort.Strings(out)
	return out
}

// themeCompiledCSS is app.css as the binary embeds it, or a skip.
func themeCompiledCSS(t *testing.T) string {
	t.Helper()
	raw, err := fs.ReadFile(web.Static(), "css/app.css")
	if err != nil {
		t.Skipf("no compiled stylesheet to read (%v) -- run `make css`. THIS IS NOT A PASS.", err)
	}
	return string(raw)
}

// TestCompiledCSS_BrandVariablesOnlyInTheirSlots is ADR 0023 claim C's test, the one
// the ADR named "the slot test" before it had a name. It answers where the accent is
// read and in which property, for the rules its parser reads (themeParseCSS). It
// asserts that themeSlotViolations reports nothing for the compiled app.css: there,
// after escapes are decoded and comments dropped, the three variables are read by no
// selector outside themeSlots and in no property but the one themeVariableProperty
// names, the edge's shadow is inset, each variable is defined once among the
// declarations the parser reads, on the top-level :root, and each listed slot reads
// its variables. Where the word brand occurs in the file as text is
// TestCompiledCSS_BrandNamesOccurOnlyInTheGolden's assertion.
//
// IT READS THE RULES OF app.css, NOT TEMPLATES. A colour utility for the accent
// written into a template compiles into app.css as a rule of its own whose selector
// is the utility, which is not in themeSlots, and the test reports it (measured: a
// class attribute, a template comment and an existing utility -- C3, C4, S2 on the
// WL-5 card). A second stylesheet or a style set from script is not in app.css and
// not read here.
//
// PART II -- what it catches, each a shape TestThemeSlotScan_RefusesEachShapeItExistsFor
// feeds the same scan: a variable read in another property (the accent as a text
// colour on .tap-button, the edge as a border or ring colour); the edge drawn by an
// outset shadow; a variable read by a rule whose selector is not a slot (the accent's
// background on .stamp, a bare background utility), including with the name spelled
// through the escapes the control lists; a variable defined outside the top-level
// :root or defined twice; an unknown --brand- name; --brand- in a selector, an
// at-rule's prelude or a comment (inside a block too); a definition behind a comment;
// a slot that does not read one of its variables. It does not see an element's
// classes, the cascade, or opacity (the limits TestCompiledCSS_StampWordIsInk's
// comment lists for its own scan are of the same kind), nor any stylesheet but
// app.css.
// PART III: a form not on that list is code review's -- no completeness claim.
func TestCompiledCSS_BrandVariablesOnlyInTheirSlots(t *testing.T) {
	t.Parallel()
	css := themeCompiledCSS(t)
	for _, v := range themeSlotViolations(css) {
		t.Error(v)
	}
}

// themeRootDefaults is the top-level :root rule of a stylesheet that defines --brand-
// variables -- its text as written (themeCSSRule.raw) and its declarations decoded --
// or a reason it cannot be read. Selector and property names are decoded before they
// are matched, so an escaped spelling is found too.
func themeRootDefaults(css string) (string, map[string]string, error) {
	rules, err := themeParseCSS(css)
	if err != nil {
		return "", nil, err
	}
	var found []themeCSSRule
	for _, r := range rules {
		if len(r.context) == 0 && themeUnescape(r.selector) == ":root" &&
			slices.ContainsFunc(r.decls, func(d [2]string) bool { return strings.HasPrefix(themeUnescape(d[0]), "--brand-") }) {
			found = append(found, r)
		}
	}
	if len(found) != 1 {
		return "", nil, fmt.Errorf("%d top-level :root rules define --brand- variables, want 1", len(found))
	}
	values := map[string]string{}
	for _, d := range found[0].decls {
		values[themeUnescape(d[0])] = themeUnescape(d[1])
	}
	return found[0].raw, values, nil
}

// TestCompiledCSS_RootDefaultsAreTheTappaGreenTheme is ADR 0023 claim B's compiled-CSS
// test. It answers what the defaults are, for the rules its parser reads. It asserts,
// in the compiled app.css (names decoded before they are matched): the parser finds
// exactly one top-level :root rule defining --brand- variables; its --brand-accent and
// --brand-on-accent are tappa-green and paper (the Go palette's own constants, not a
// hex written here); Edge(tappa-green) is false and its --brand-edge is none; and the
// rule's text as written -- selector without comments, then the block from '{' to
// '}' -- equals ThemeCSS(tappa-green) byte for byte. (That a page without the theme
// link and one with tappa-green's link then compute the same three values is the
// cascade's doing, measured once in Chrome, not asserted here. How many times and
// where each variable is defined is rule 2 of themeSlotViolations.)
//
// PART II -- what it catches: a default accent or label other than the palette's
// tappa-green and paper; an edge default other than what Edge(tappa-green) asks
// for; a fourth property, another order, another spelling, a space or a doubled
// semicolon in the defaults rule; the rule missing, or defined in two top-level
// :root rules.
// PART III: a form not on that list is code review's -- no completeness claim.
func TestCompiledCSS_RootDefaultsAreTheTappaGreenTheme(t *testing.T) {
	t.Parallel()
	css := themeCompiledCSS(t)
	rule, values, err := themeRootDefaults(css)
	if err != nil {
		t.Fatal(err)
	}
	green, paper := paletteColor(paletteTappaGreenHex), paletteColor(palettePaperHex)
	if got := values["--brand-accent"]; got != themeDecimal(green) {
		t.Errorf("--brand-accent defaults to %q; the palette's tappa-green is %q", got, themeDecimal(green))
	}
	if got := values["--brand-on-accent"]; got != themeDecimal(paper) {
		t.Errorf("--brand-on-accent defaults to %q; the palette's paper is %q", got, themeDecimal(paper))
	}
	if Edge(green) {
		t.Fatal("PREMISE: Edge(tappa-green) is true; ADR 0023 section 4's default is no edge")
	}
	if got := values["--brand-edge"]; got != themeNoEdge {
		t.Errorf("--brand-edge defaults to %q, want %q (no edge on tappa-green)", got, themeNoEdge)
	}
	want, err := ThemeCSS(green)
	if err != nil {
		t.Fatalf("PREMISE: ThemeCSS(tappa-green): %v", err)
	}
	if rule != want {
		t.Errorf("app.css's defaults are\n  %s\nthe theme route's body for tappa-green is\n  %s", rule, want)
	}
}

// TestThemeSlotScan_RefusesEachShapeItExistsFor is the negative control of the two
// compiled-CSS tests; it needs no app.css. It asserts: the variable names the
// property rule knows equal the ones ThemeCSS declares; the shipped shape passes both
// reads; each shape on the slot list is reported with the violation text it names;
// each shape on the defaults list does not read as tappa-green's theme; and a
// stylesheet with two top-level :root rules defining the variables is refused by the
// defaults read (a separate assertion after the lists). The shapes the two tests'
// PART II comments name are among those (read, not asserted).
func TestThemeSlotScan_RefusesEachShapeItExistsFor(t *testing.T) {
	t.Parallel()
	green, err := ThemeCSS(paletteColor(paletteTappaGreenHex))
	if err != nil {
		t.Fatal(err)
	}
	declared := []string{}
	for _, m := range regexp.MustCompile(`(--brand-[a-z-]+):`).FindAllStringSubmatch(green, -1) {
		declared = append(declared, m[1])
	}
	keys := make([]string, 0, len(themeVariableProperty))
	for k := range themeVariableProperty {
		keys = append(keys, k)
	}
	sort.Strings(declared)
	sort.Strings(keys)
	if !slices.Equal(declared, keys) {
		t.Fatalf("ThemeCSS declares %v; the property rule knows %v", declared, keys)
	}

	// The shape `make css` ships today (WL-5), from input.css's two .tap-button rules.
	const slot = `.tap-button{border-radius:.125rem;--tw-bg-opacity:1;background-color:rgb(var(--brand-accent)/var(--tw-bg-opacity,1));--tw-text-opacity:1;color:rgb(var(--brand-on-accent)/var(--tw-text-opacity,1))}` +
		`.tap-button{box-shadow:inset 0 0 0 2px rgb(var(--brand-edge)/1)}` +
		`@media (prefers-reduced-motion:reduce){.tap-button{transition-property:none}}`
	shipped := `/*! header */` + green + `.stamp{color:rgb(21 34 25/var(--tw-text-opacity,1))}` + slot
	if v := themeSlotViolations(shipped); len(v) != 0 {
		t.Fatalf("CONTROL: the shipped shape is refused: %v", v)
	}
	if rule, _, err := themeRootDefaults(shipped); err != nil || rule != green {
		t.Fatalf("CONTROL: the shipped defaults read as %q, %v", rule, err)
	}

	for _, tc := range []struct {
		name, css, want string
	}{
		{"the accent as .tap-button's text colour (ADR 0023 section 3's mutation)",
			shipped + `.tap-button{color:rgb(var(--brand-accent))}`, "reads --brand-accent in color"},
		{"the accent's background on .stamp (ADR 0023 section 6's mutation)",
			shipped + `.stamp{--tw-bg-opacity:1;background-color:rgb(var(--brand-accent)/var(--tw-bg-opacity,1))}`, `".stamp" is not a slot`},
		{"a bare background utility",
			shipped + `.bg-brand{--tw-bg-opacity:1;background-color:rgb(var(--brand-accent)/var(--tw-bg-opacity,1))}`, `".bg-brand" is not a slot`},
		{"a slot in a selector list with a non-slot",
			shipped + `.tap-button,.docket{background-color:rgb(var(--brand-accent))}`, `".docket" is not a slot`},
		{"the edge as a border colour",
			shipped + `.tap-button{border-color:rgb(var(--brand-edge))}`, "reads --brand-edge in border-color"},
		{"the edge as an outset shadow",
			strings.Replace(shipped, "box-shadow:inset 0 0 0 2px", "box-shadow:0 0 0 2px", 1), "in a shadow that is not inset"},
		{"the edge outset in a list with an inset shadow",
			strings.Replace(shipped, "box-shadow:inset 0 0 0 2px rgb(var(--brand-edge)/1)", "box-shadow:inset 0 0 0 1px rgb(0 0 0/1),0 0 0 2px rgb(var(--brand-edge)/1)", 1), "in a shadow that is not inset"},
		{"the edge through Tailwind's ring colour",
			shipped + `.tap-button{--tw-ring-color:rgb(var(--brand-edge)/1)}`, "reads --brand-edge in --tw-ring-color"},
		{"the label as a background",
			shipped + `.tap-button{background-color:rgb(var(--brand-on-accent))}`, "reads --brand-on-accent in background-color"},
		{"a variable defined on another selector",
			shipped + `.docket{--brand-accent:255 0 0}`, "defines --brand-accent outside the top-level :root rule"},
		{"a variable defined inside an at-rule",
			shipped + `@media print{:root{--brand-accent:255 0 0}}`, "defines --brand-accent outside the top-level :root rule"},
		{"a variable defined twice on :root",
			shipped + `:root{--brand-edge:21 34 25}`, "--brand-edge is defined 2 times"},
		{"an unknown variable read",
			shipped + `.tap-button{background-color:rgb(var(--brand-second))}`, "reads --brand-second, which is not one"},
		{"an unknown variable defined",
			shipped + `:root{--brand-second:1 2 3}`, "defines --brand-second, which is not one"},
		{"--brand- in a selector",
			shipped + `[style*="--brand-accent"]{color:red}`, "outside any declaration"},
		{"--brand- in a comment",
			shipped + `/* --brand-accent */`, "outside any declaration"},
		{"--brand- in a preserved comment inside a block (K2, 5th-round audit)",
			shipped + `.docket{color:red;/*! --brand-accent */}`, "outside any declaration"},
		{"a definition behind a comment on :root (B1c5, 5th-round audit)",
			shipped + `:root{color-scheme:light; /*! x */--brand-accent:255 0 0}`, "--brand-accent is defined 2 times"},
		{"a definition behind a comment on another selector (B1c3, 5th-round audit)",
			shipped + `.docket{color:red; /*! x */--brand-accent:255 0 0}`, "defines --brand-accent outside the top-level :root rule"},
		{"--brand- in an at-rule's prelude",
			shipped + `@supports (--brand-accent:0){.x{color:red}}`, "outside any declaration"},
		{"an escaped hyphen in the read (4th-round audit)",
			shipped + `.stamp{background-color:rgb(var(--brand\-accent))}`, `".stamp" is not a slot`},
		{"a hex-escaped hyphen in the read (4th-round audit)",
			shipped + `.stamp{background-color:rgb(var(--brand\2d accent))}`, `".stamp" is not a slot`},
		{"a hex-escaped letter in the read",
			shipped + `.stamp{background-color:rgb(var(--\62 rand-accent))}`, `".stamp" is not a slot`},
		{"both leading hyphens hex-escaped in the read",
			shipped + `.stamp{background-color:rgb(var(\2d\2d brand-accent))}`, `".stamp" is not a slot`},
		{"an escaped hyphen in a definition",
			shipped + `.docket{--brand\-accent:255 0 0}`, "defines --brand-accent outside the top-level :root rule"},
		{"an escaped semicolon hiding a second declaration",
			shipped + `.tap-button{color:x\;background-color:rgb(var(--brand-accent))}`, "reads --brand-accent in color"},
		{"the slot reads no accent",
			strings.Replace(shipped, "background-color:rgb(var(--brand-accent)", "background-color:rgb(31 92 65", 1), "never reads --brand-accent"},
		{"the defaults are gone",
			slot, "--brand-accent is defined 0 times"},
		{"an unreadable stylesheet",
			shipped + `.x{color:red`, "unreadable stylesheet"},
	} {
		v := themeSlotViolations(tc.css)
		if !slices.ContainsFunc(v, func(s string) bool { return strings.Contains(s, tc.want) }) {
			t.Errorf("%s: the scan reports %q, none containing %q", tc.name, v, tc.want)
		}
	}

	for _, tc := range []struct{ name, css string }{
		{"another default accent", strings.Replace(shipped, "--brand-accent:31 92 65", "--brand-accent:31 92 66", 1)},
		{"the label defaulting to ink", strings.Replace(shipped, "--brand-on-accent:255 253 244", "--brand-on-accent:21 34 25", 1)},
		{"an edge drawn in the accent's colour", strings.Replace(shipped, "--brand-edge:none", "--brand-edge:31 92 65", 1)},
		{"a fourth property", strings.Replace(shipped, ";--brand-edge:none}", ";--brand-edge:none;--brand-x:0}", 1)},
		{"another order", strings.Replace(shipped, "--brand-accent:31 92 65;--brand-on-accent:255 253 244", "--brand-on-accent:255 253 244;--brand-accent:31 92 65", 1)},
		{"another spelling", strings.Replace(shipped, "--brand-accent:31 92 65", "--brand-accent:31,92,65", 1)},
		{"the rule missing", slot},
		{"spaces in the rule", strings.Replace(shipped, "--brand-accent:31 92 65", "--brand-accent: 31 92 65", 1)},
		{"a doubled semicolon", strings.Replace(shipped, "--brand-on-accent:255 253 244;", "--brand-on-accent:255 253 244;;", 1)},
	} {
		rule, _, err := themeRootDefaults(tc.css)
		if err == nil && rule == green {
			t.Errorf("%s: the defaults still read as tappa-green's theme", tc.name)
		}
	}
	if _, _, err := themeRootDefaults(shipped + green); err == nil {
		t.Error("two top-level :root rules defining the variables read as one")
	}
}

// themeBrandSite is one place "brand" occurs in a stylesheet's text: the brace depth
// there (the '{' before it minus the '}' before it) and the text around it, from the
// '}' before it (or the start) to the '}' after it (or the end), lower-cased.
type themeBrandSite struct {
	depth int
	rule  string
}

// themeBrandSites counts each occurrence of "brand", in any letter case, in text by
// site. It parses no CSS: it finds a substring and the braces around it. What that
// counts and what it does not is measured, not reasoned: the shapes on the WL-5
// card's mutation table that turn the pin red, and the ones (a deliberately balanced
// brace in a comment, F5) that do not.
//
// "brand" and not "--brand-": the wider word also counts the token utilities'
// own names (.bg-brand, .shadow-brand-edge) and, in the text as written, the
// escaped-hyphen spellings (--brand\-accent, \2d\2d brand-accent). Its cost is one
// site that is not a variable (the landing's .p-brand rule), measured and listed.
func themeBrandSites(text string) map[themeBrandSite]int {
	s := strings.ToLower(text)
	out := map[themeBrandSite]int{}
	for i := 0; ; {
		j := strings.Index(s[i:], "brand")
		if j < 0 {
			return out
		}
		at := i + j
		to := len(s)
		if k := strings.IndexByte(s[at:], '}'); k >= 0 {
			to = at + k + 1
		}
		site := themeBrandSite{
			depth: strings.Count(s[:at], "{") - strings.Count(s[:at], "}"),
			rule:  s[strings.LastIndexByte(s[:at], '}')+1 : to],
		}
		out[site]++
		i = at + len("brand")
	}
}

// themeBrandNeutral is css with each escape whose code point is a brace -- `\{`, `\}`,
// `\7d ` -- replaced by U+FFFD; every other byte, other escapes included, is kept. To a
// browser an escaped brace is a character of a name, not a block's edge; left in, it
// moved a window and a depth so that an accent read on .stamp kept the golden's site
// (the audit of round 5; E1 and F4 on the WL-5 card). U+FFFD is what themeUnescape
// writes for an invalid code point, holds no brace or backslash, and cannot be part
// of "brand".
func themeBrandNeutral(css string) string {
	var b strings.Builder
	for i := 0; i < len(css); {
		if css[i] != '\\' {
			b.WriteByte(css[i])
			i++
			continue
		}
		r, n := themeEscapeAt(css, i)
		if r == '{' || r == '}' {
			b.WriteRune(utf8.RuneError)
		} else {
			b.WriteString(css[i : i+n])
		}
		i += n
	}
	return b.String()
}

// themeBrandGoldenEntry is one site of the golden and how many times "brand" occurs
// there.
type themeBrandGoldenEntry struct {
	depth, count int
	rule         string
}

// themeBrandGolden is every occurrence of "brand" in the compiled app.css, read with
// `make css` on 2026-10-03 (WL-5): the :root defaults (three definitions), the tap
// button's rule (its fill and its label), the tap button's inset edge, and the
// landing's .p-brand rule, which is not a variable. The same four sites were measured
// in a build with WL-6's templates and stylesheet merged in (WL-5 card). A change that
// adds or rewrites a site -- WL-8's panel stripe is the next, and any new use of the
// word in app.css is one -- edits this list in the same change; that edit is the
// review.
var themeBrandGolden = []themeBrandGoldenEntry{
	{depth: 1, count: 3, rule: `:root{--brand-accent:31 92 65;--brand-on-accent:255 253 244;--brand-edge:none}`},
	{depth: 1, count: 2, rule: `.tap-button{border-radius:.125rem;min-height:4rem;width:100%;--tw-bg-opacity:1` +
		`;background-color:rgb(var(--brand-accent)/var(--tw-bg-opacity,1))` +
		`;font-family:space grotesk,system-ui,sans-serif;font-size:1.25rem` +
		`;letter-spacing:-.025em;line-height:1.75rem;--tw-text-opacity:1` +
		`;color:rgb(var(--brand-on-accent)/var(--tw-text-opacity,1))` +
		`;transition-duration:.15s;transition-property:transform` +
		`;transition-timing-function:cubic-bezier(.4,0,.2,1)}`},
	{depth: 1, count: 1, rule: `.tap-button{box-shadow:inset 0 0 0 2px rgb(var(--brand-edge)/1)}`},
	{depth: 0, count: 1, rule: `.lp .plaque .p-brand{color:#9db3a5;font-family:var(--mono);font-size:10px` +
		`;letter-spacing:3px;margin-bottom:18px}`},
}

// themeBrandPin lists the differences between the golden and the sites of "brand" in
// css. It neutralises the escaped braces once (themeBrandNeutral) and reads the result
// twice: as written, and with the remaining escapes decoded (themeUnescape). The
// decoded read sees an escaped letter (--\62 rand-accent is --brand-accent to a
// browser); the read as written sees what decoding would fold into another code point
// (\brand decodes to U+000B and "rand"). Neutralising first keeps both: it changes
// only the escapes that decode to a brace, so the as-written read keeps \brand and
// the decoded read still decodes \62.
func themeBrandPin(css string, golden []themeBrandGoldenEntry) []string {
	want := map[themeBrandSite]int{}
	for _, g := range golden {
		want[themeBrandSite{depth: g.depth, rule: g.rule}] += g.count
	}
	var out []string
	neutral := themeBrandNeutral(css)
	for _, read := range []struct{ name, text string }{
		{"as written", neutral},
		{"with escapes decoded", themeUnescape(neutral)},
	} {
		got := themeBrandSites(read.text)
		for site, n := range got {
			if want[site] != n {
				out = append(out, fmt.Sprintf("%s: %d occurrence(s) at depth %d in %q; the golden lists %d",
					read.name, n, site.depth, site.rule, want[site]))
			}
		}
		for site, n := range want {
			if _, ok := got[site]; !ok {
				out = append(out, fmt.Sprintf("%s: 0 occurrences at depth %d in %q; the golden lists %d",
					read.name, site.depth, site.rule, n))
			}
		}
	}
	sort.Strings(out)
	return out
}

// TestCompiledCSS_BrandNamesOccurOnlyInTheGolden pins where the word "brand" occurs in
// the compiled app.css. It asserts that themeBrandPin reports nothing: with escaped
// braces neutralised, in the text as written and in the text with escapes decoded,
// the multiset of (depth, surrounding text) over the occurrences of "brand", in any
// letter case, equals themeBrandGolden.
//
// It holds the sites, not their meaning: where a site reads the accent and in which
// property, and where the variables are defined, is
// TestCompiledCSS_BrandVariablesOnlyInTheirSlots's question; what the defaults are is
// TestCompiledCSS_RootDefaultsAreTheTappaGreenTheme's. Its use is the shapes those two
// parser-based tests did not read: a fifth audit round found a comment before a
// definition hidden from both, and that shape turns this pin red.
//
// PART II -- what it catches (each measured, WL-5 card): a definition behind a comment
// on :root or on another selector; --brand-accent in a preserved comment inside a
// block; a second inset read of the edge on .tap-button in @media print; the accent's
// background on .stamp, plainly and through \2d; a listed rule moved into an at-rule
// after another rule, plainly or behind an escaped closing brace that evens the depth;
// a listed rule given a selector list that adds .stamp behind escaped braces; a golden
// entry deleted with app.css unchanged.
// PART III: a form not on that list is code review's -- no completeness claim.
func TestCompiledCSS_BrandNamesOccurOnlyInTheGolden(t *testing.T) {
	t.Parallel()
	for _, d := range themeBrandPin(themeCompiledCSS(t), themeBrandGolden) {
		t.Error("app.css " + d)
	}
}

// TestThemeBrandScan_RefusesEachShapeItExistsFor is the golden pin's negative control;
// it needs no app.css. It builds the shipped shape from the golden itself (the listed
// rules, one after another) and asserts: no golden site is listed twice; themeBrandPin
// reports nothing for the shipped shape; the golden holds the tap button's edge rule
// (five shapes below edit it); the pin reports a difference for each shape on the
// list below, and for the shipped shape against the golden with any one entry left out.
func TestThemeBrandScan_RefusesEachShapeItExistsFor(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	listed := map[themeBrandSite]bool{}
	for _, g := range themeBrandGolden {
		site := themeBrandSite{depth: g.depth, rule: g.rule}
		if listed[site] {
			t.Fatalf("the golden lists %q at depth %d twice", g.rule, g.depth)
		}
		listed[site] = true
		b.WriteString(g.rule)
	}
	shipped := b.String()
	if d := themeBrandPin(shipped, themeBrandGolden); len(d) != 0 {
		t.Fatalf("CONTROL: the golden's own rules differ from the golden: %q", d)
	}
	const edge = ".tap-button{box-shadow:inset 0 0 0 2px rgb(var(--brand-edge)/1)}"
	if !strings.Contains(shipped, edge) {
		t.Fatalf("PREMISE: the golden has no %q", edge)
	}
	for _, tc := range []struct{ name, css string }{
		{"a definition behind a comment on :root (B1c5, 5th-round audit)",
			shipped + `:root{color-scheme:light; /*! x */--brand-accent:255 0 0}`},
		{"a definition behind a comment on another selector (B1c3)",
			shipped + `.docket{color:red; /*! x */--brand-accent:255 0 0}`},
		{"the name in a preserved comment inside a block (K2)",
			shipped + `.docket{color:red;/*! --brand-accent */}`},
		{"a second inset read of the edge in @media print (X5a)",
			shipped + `@media print{.tap-button{box-shadow:inset 0 0 0 1px rgb(var(--brand-edge))}}`},
		{"the accent's background on .stamp",
			shipped + `.stamp{background-color:rgb(var(--brand-accent))}`},
		{"the accent's background on .stamp through \\2d",
			shipped + `.stamp{background-color:rgb(var(--brand\2d accent))}`},
		{"an escaped letter, seen decoded",
			shipped + `.stamp{background-color:rgb(var(--\62 rand-accent))}`},
		{"a spelling decoding folds away, seen as written",
			shipped + `/*! \brand */`},
		{"the edge rule moved into an at-rule after another rule",
			strings.Replace(shipped, edge, "@media print{.x{color:red}"+edge+"}", 1)},
		{"the edge rule moved to another selector",
			strings.Replace(shipped, edge, strings.Replace(edge, ".tap-button", ".stamp", 1), 1)},
		{"the edge rule removed",
			strings.Replace(shipped, edge, "", 1)},
		{"the edge read by .stamp behind escaped braces (E1c)",
			strings.Replace(shipped, edge, `.stamp,.x\{\}`+edge, 1)},
		{"the edge moved into an at-rule behind an escaped closing brace (F4)",
			strings.Replace(shipped, edge, `@media print{.x{color:red}.y\}`+edge+`}\{`, 1)},
	} {
		if d := themeBrandPin(tc.css, themeBrandGolden); len(d) == 0 {
			t.Errorf("%s: the pin reports nothing", tc.name)
		}
	}
	for i := range themeBrandGolden {
		less := slices.Delete(slices.Clone(themeBrandGolden), i, i+1)
		if d := themeBrandPin(shipped, less); len(d) == 0 {
			t.Errorf("the golden without %q: the pin reports nothing", themeBrandGolden[i].rule)
		}
	}
}
