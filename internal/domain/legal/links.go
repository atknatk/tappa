package legal

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Span is one run of a paragraph: plain text, or a link when Href is set.
//
// 🔴 IT IS DATA, NOT MARKUP. Text is the operator's own characters and the template
// interpolates it with `{ }` (html.EscapeString); Href is a target that has passed
// SafeHref and the template passes it through templ.URL into an attribute. Nothing
// here is HTML and nothing here needs templ.Raw.
type Span struct {
	Text string
	// Href is "" for plain text. When set it is an http, https, mailto or tel URL
	// that SafeHref accepted — never anything else.
	Href string
}

// Paragraph is one paragraph of a published text, as a sequence of spans.
type Paragraph []Span

// Text is what a reader sees: every span's text, concatenated. A link contributes
// its label, not its target.
func (p Paragraph) Text() string {
	var b strings.Builder
	for _, s := range p {
		b.WriteString(s.Text)
	}
	return b.String()
}

// THE LINK GRAMMAR (ADR 0033). It is CLOSED and it is not markdown: four shapes,
// recognised inside one paragraph's text, and nothing else is interpreted.
//
//  1. `[label](target)` — label is plain text (no brackets inside, so no nesting),
//     target has no whitespace and at most one level of balanced parentheses. If the
//     target fails SafeHref the WHOLE `[label](target)` stays literal text.
//  2. A bare `http://` or `https://` URL, label as typed. It ends at any whitespace,
//     Unicode included (a no-break space pasted from a word processor ends it rather
//     than spoiling it), at a control character, or at `<`, `>` or `"`. Trailing
//     sentence punctuation and an unmatched closing `)` or `]` are not part of it.
//  3. A bare e-mail address (a conservative shape) → `mailto:`.
//  4. A bare international phone number that STARTS WITH `+`: digits with optional
//     single separators between them (a hyphen or one space, a no-break space
//     included), 8 to 15 digits (E.164's maximum) → `tel:+<digits>`, label as typed.
//     Without the `+` nothing is linked, because dates, amounts, registration numbers
//     and VAT ids are digit runs too. ONE MORE RULE, because a number is often followed
//     by another number in prose ("call +356 9931 0184 2024 hours a year"): a SPACE
//     ends the number once it already has phoneSpaceStop (11) digits. Hyphens do not.
//     11 is the length of the common international forms (+356 XXXX XXXX,
//     +1 XXX XXX XXXX, +33 X XX XX XX XX, +61 X XXXX XXXX) and every common
//     grouping of a longer number (+44 20 7946 0958, +49 1511 2345678,
//     +86 138 0013 8000) crosses its last space with fewer than 11 digits behind it.
//
// Go's regexp is RE2 (linear time), and this runs once per publication, not per
// request (see Doc.Paragraphs), so an attacker-shaped body costs nothing per view.
var (
	// \p{Cc} and \p{Z} together are every rune unicode.IsSpace or unicode.IsControl
	// reports for a URL's purposes: Go's \s is ASCII-only, and a no-break space must END
	// a URL rather than end up inside one (where SafeHref would refuse the whole thing).
	explicitLinkRE = regexp.MustCompile(`\[([^\[\]]+)\]\(((?:[^\p{Cc}\p{Z}()]|\([^\p{Cc}\p{Z}()]*\))+)\)`)
	bareURLRE      = regexp.MustCompile(`(?i)\bhttps?://[^\p{Cc}\p{Z}<>"]+`)
	emailRE        = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)*\.[A-Za-z]{2,}`)
	phoneRE        = regexp.MustCompile(`\+[0-9](?:[\p{Zs}-]?[0-9])+`)
)

const (
	minPhoneDigits = 8
	maxPhoneDigits = 15
	// phoneSpaceStop is the digit count after which a space ends a phone number (see
	// the grammar above).
	phoneSpaceStop = 11
)

// match kinds, in the order that wins a tie on the same start position.
const (
	kindExplicit = iota
	kindURL
	kindEmail
	kindPhone
)

type linkMatch struct {
	kind       int
	start, end int
}

// linkify turns one paragraph's text into spans. Overlaps are settled left to right:
// the match that starts first wins and anything starting inside it is ignored, so a
// URL inside an explicit link's target, or an e-mail inside a URL, is not linked twice.
func linkify(text string) Paragraph {
	var ms []linkMatch
	for _, k := range []struct {
		kind int
		re   *regexp.Regexp
	}{{kindExplicit, explicitLinkRE}, {kindURL, bareURLRE}, {kindEmail, emailRE}, {kindPhone, phoneRE}} {
		for _, loc := range k.re.FindAllStringIndex(text, -1) {
			ms = append(ms, linkMatch{kind: k.kind, start: loc[0], end: loc[1]})
		}
	}
	sort.Slice(ms, func(i, j int) bool {
		if ms[i].start != ms[j].start {
			return ms[i].start < ms[j].start
		}
		return ms[i].kind < ms[j].kind
	})

	// THE TEXT BETWEEN LINKS IS ONE CONTIGUOUS SLICE OF text, so it is cut once, when a
	// link (or the end) closes it, rather than grown piece by piece. Growing it with +=
	// was quadratic: 256 KiB of refused `[a](//x)` cost 0.6 s (fix round, 2026-10-23).
	var out Paragraph
	textFrom, cursor := 0, 0
	for _, m := range ms {
		if m.start < cursor {
			continue
		}
		span, end, ok := resolve(text, m)
		if !ok {
			// Not a link after all. For an explicit link whose target failed the
			// allow-list the whole construct stays literal (end covers it); for the
			// bare shapes nothing is consumed and later matches still get their turn.
			if end > m.start {
				cursor = end
			}
			continue
		}
		if m.start > textFrom {
			out = append(out, Span{Text: text[textFrom:m.start]})
		}
		out = append(out, span)
		textFrom, cursor = end, end
	}
	if textFrom < len(text) {
		out = append(out, Span{Text: text[textFrom:]})
	}
	return out
}

// resolve checks one candidate. It returns the link span and where it ends, or
// ok=false with the end of the text that must stay literal (m.start when nothing
// is consumed).
func resolve(text string, m linkMatch) (Span, int, bool) {
	raw := text[m.start:m.end]
	switch m.kind {
	case kindExplicit:
		sub := explicitLinkRE.FindStringSubmatch(raw)
		label, target := sub[1], sub[2]
		href, ok := SafeHref(target)
		if !ok || strings.TrimSpace(label) == "" {
			return Span{}, m.end, false
		}
		return Span{Text: label, Href: href}, m.end, true
	case kindURL:
		u := trimURLTail(raw)
		href, ok := SafeHref(u)
		if !ok {
			return Span{}, m.start, false
		}
		return Span{Text: u, Href: href}, m.start + len(u), true
	case kindEmail:
		if !boundaryBefore(text, m.start, isEmailChar) || !emailEndsHere(text, m.end) {
			return Span{}, m.start, false
		}
		href, ok := SafeHref("mailto:" + raw)
		if !ok {
			return Span{}, m.start, false
		}
		return Span{Text: raw, Href: href}, m.end, true
	case kindPhone:
		// A `+` glued to a word or a number ("C++", "5+12345678") is arithmetic or a
		// name, not the start of a phone number.
		if !boundaryBefore(text, m.start, isWordChar) {
			return Span{}, m.start, false
		}
		var digits strings.Builder
		for i, r := range raw {
			if unicode.Is(unicode.Zs, r) && digits.Len() >= phoneSpaceStop {
				raw = raw[:i]
				break
			}
			if r >= '0' && r <= '9' {
				digits.WriteRune(r)
			}
		}
		if n := digits.Len(); n < minPhoneDigits || n > maxPhoneDigits {
			return Span{}, m.start, false
		}
		href, ok := SafeHref("tel:+" + digits.String())
		if !ok {
			return Span{}, m.start, false
		}
		return Span{Text: raw, Href: href}, m.start + len(raw), true
	}
	return Span{}, m.start, false
}

// trimURLTail drops what a sentence puts after a URL: trailing punctuation, and a
// closing parenthesis or bracket that has no opening one inside the URL
// ("(see https://x.mt)", "[see https://x.mt]").
//
// THE BRACKETS ARE COUNTED ONCE AND DECREMENTED AS THE TAIL IS TRIMMED. Recounting per
// trimmed character was quadratic: "https://a.mt/" followed by 256 KiB of ")" cost
// 1.4 s (fix round, 2026-10-23).
func trimURLTail(u string) string {
	openParen, closeParen := strings.Count(u, "("), strings.Count(u, ")")
	openBracket, closeBracket := strings.Count(u, "["), strings.Count(u, "]")
	for len(u) > 0 {
		switch last := u[len(u)-1]; {
		case strings.IndexByte(".,;:!?'", last) >= 0:
		case last == ')' && openParen < closeParen:
			closeParen--
		case last == ']' && openBracket < closeBracket:
			closeBracket--
		default:
			return u
		}
		u = u[:len(u)-1]
	}
	return u
}

func isWordChar(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '+' }

func isEmailChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("._%+-@", r)
}

func boundaryBefore(text string, i int, inWord func(rune) bool) bool {
	if i == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(text[:i])
	return !inWord(r)
}

// emailEndsHere reports whether an address match really ends at i: the next
// character is not one an address could continue with. A full stop is the end of a
// sentence unless another label follows it ("a@b.mt.x" is not linked as "a@b.mt").
func emailEndsHere(text string, i int) bool {
	if i == len(text) {
		return true
	}
	r, n := utf8.DecodeRuneInString(text[i:])
	if r == '.' {
		next, _ := utf8.DecodeRuneInString(text[i+n:])
		return i+n == len(text) || !(unicode.IsLetter(next) || unicode.IsDigit(next))
	}
	return !isEmailChar(r)
}

// SafeHref is THE allow-list every link target passes before it can become an href:
// it returns the target unchanged and true only for an http or https URL with a
// host, or a mailto or tel URL with a body. The scheme check ignores case.
//
// 🔴 EVERYTHING ELSE IS TEXT: javascript:, data:, vbscript:, file:, ftp:, a relative
// path, a protocol-relative `//host`, and any target carrying a control character or
// whitespace (a browser strips some of those before reading the scheme, which is how
// "java\tscript:" gets past naive checks).
//
// templ.URL sanitises href too (it allows http, https, mailto, tel, ftp and ftps), but
// that is a second fence we do not own and do not test; this function is the one this
// package's tests pin (TestSafeHref_AllowsOnlyTheFourSchemes).
func SafeHref(target string) (string, bool) {
	if target == "" || strings.HasPrefix(target, "//") {
		return "", false
	}
	for _, r := range target {
		if unicode.IsControl(r) || unicode.IsSpace(r) || r == '\\' {
			return "", false
		}
	}
	u, err := url.Parse(target)
	if err != nil {
		return "", false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Host == "" || u.Opaque != "" {
			return "", false
		}
	case "mailto", "tel":
		if u.Opaque == "" {
			return "", false
		}
	default:
		return "", false
	}
	return target, true
}
