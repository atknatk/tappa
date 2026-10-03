package email

// The tests of ADR 0022 §8 and its Claim H (email.go's PART I and PART II name
// each one).
//
// 🔴 NO TEST HERE PRINTS A LINK, A CODE, A TOKEN OR A RENDERED BODY. The link is
// §4.7 material and every rendered part contains it, so a failure message names
// the case, a count or a colour — never the string it inspected. The codes are
// drawn fresh from crypto/rand on every run; none is written in this file.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"html"
	"math"
	"mime"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/signup"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/mail"
)

const testBase = "https://app.taptime.test"

// host253 and host254 are host names of four labels, none longer than 63 bytes,
// one byte inside and one byte past validHostName's 253-byte bound (their lengths
// are asserted in TestRender_RefusesALinkOutsideTheExpectedAddress).
var (
	host253 = strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 61)
	host254 = strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 62)
)

// randomValue is a fresh code-shaped value: 32 random bytes as unpadded
// base64url, the shape both producers mint.
func randomValue(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("crypto/rand: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// rendered is one message with the link it was rendered for.
type rendered struct {
	kind string
	link string
	msg  mail.Message
}

// renderBoth renders the invitation (with the two names) and the reset.
func renderBoth(t *testing.T, employeeName, tenantName string) []rendered {
	t.Helper()
	ctx := context.Background()
	inv := testBase + activatePath + "?" + activateParam + "=" + randomValue(t)
	mi, err := RenderInvitation(ctx, inv, InvitationView{
		BaseURL: testBase, EmployeeName: employeeName, TenantName: tenantName, ValidFor: 7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("invitation for employee %q, tenant %q: %v", employeeName, tenantName, err)
	}
	rst := testBase + resetPath + "?" + resetParam + "=" + randomValue(t)
	mr, err := RenderPasswordReset(ctx, rst, ResetView{BaseURL: testBase, ValidFor: time.Hour})
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	return []rendered{{"invitation", inv, mi}, {"reset", rst, mr}}
}

// hostileNames are names an open signup lets an attacker choose. Each must be
// withheld. The comment on each says which rule refuses it.
var hostileNames = []string{
	"https://evil.example/login",   // ':' and '/'
	"evil.example",                 // a dot between two labels
	"Pay at www.evil.example now",  // dotted run inside prose
	"evil .example",                // a dot followed by a letter
	"evil.-example",                // a dot followed by '-'
	"192.0.2.1",                    // IP literal (and no letter)
	"Kebab 192.0.2.1",              // IP literal inside a name
	"help@evil.example",            // '@'
	"javascript:alert(1)",          // ':'
	"<script>alert(1)</script>",    // '<', '>', '/'
	"<img src=x onerror=alert(1)>", // '<', '=', '>'
	`Bar "The Spot"`,               // '"'
	"Kebab \u202eelpmaxe.live",     // bidi override (and a dot)
	"evil\u200bexample",            // zero-width space (a format character)
	"Line\r\nBcc: x@evil.example",  // CR, LF
	"Line\u2028Two",                // U+2028
	"Line\u2029Two",                // U+2029
	"Line\u0085Two",                // NEL
	"Tab\tName",                    // TAB
	"\u00a0Spaced\u00a0",           // NBSP is not U+0020
	"evil\u3002example",            // IDEOGRAPHIC FULL STOP
	"evil\uff0eexample",            // FULLWIDTH FULL STOP
	"evil\uff61example",            // HALFWIDTH IDEOGRAPHIC FULL STOP
	"evil\ufe52example",            // SMALL FULL STOP
	"evil\u2024example",            // ONE DOT LEADER
	// Look-alike dots that are punctuation (not a label separator for the detector
	// or for IDNA) — withheld only because punctuation is not on the list.
	"evil" + string(rune(0x00B7)) + "example", // MIDDLE DOT
	"evil" + string(rune(0x30FB)) + "example", // KATAKANA MIDDLE DOT
	"evil" + string(rune(0x2027)) + "example", // HYPHENATION POINT
	"...",                                    // no letter
	"- 21 (!)",                               // no letter, and nothing else wrong
	strings.Repeat("a", maxShownNameRunes+1), // longer than any stored name
	"Kebab\u00adFactory",                     // soft hyphen (a format character)
	"Ka\xffbab",                              // invalid UTF-8
}

// nfkcDots are the code points other than "." whose NFKC form holds "." or
// U+3002 — measured over Unicode 16.0 with Python's unicodedata (M10 EM-4 card).
// Python's IDNA 2003 codec turns "evil<c>example" into "evil.example" for five of
// them; nameShown must withhold every one between two labels.
var nfkcDots = []rune{
	0x2024, 0x2025, 0x2026,
	0x2488, 0x2489, 0x248A, 0x248B, 0x248C, 0x248D, 0x248E, 0x248F, 0x2490, 0x2491,
	0x2492, 0x2493, 0x2494, 0x2495, 0x2496, 0x2497, 0x2498, 0x2499, 0x249A, 0x249B,
	0x3002, 0x33C2, 0x33C7, 0x33D8, 0xFE12, 0xFE19, 0xFE30, 0xFE52, 0xFF0E, 0xFF61,
	0x1F100,
}

// dotClass is "." and the five characters Python's IDNA 2003 codec folds into a
// label separator (measured: it turns "evil"+c+"example" into "evil.example" for
// each).
const dotClass = `[.\x{3002}\x{FF0E}\x{FF61}\x{FE52}\x{2024}]`

// linkifiers is OUR detector of text a mail client might turn into a link. It is
// deliberately broad — any scheme, any "//", any "@", and any run of two labels
// joined by a dot-like character — because what clients actually link is not
// measured (ADR 0022 counted limit 22). Its own control:
// TestLinkifiable_CatchesWhatItExistsToCatch.
var linkifiers = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:[a-z][a-z0-9+.-]*:)?//`),
	regexp.MustCompile(`(?i)\b(?:mailto|tel|sms|callto|data|javascript|vbscript|file|ftp|ftps|news|irc|xmpp|geo|maps|whatsapp|skype):`),
	regexp.MustCompile(`[\p{L}\p{N}_-]+(?:` + dotClass + `[\p{L}\p{N}_-]+)+`),
	regexp.MustCompile(`@`),
}

func linkifiableRuns(s string) int {
	n := 0
	for _, re := range linkifiers {
		n += len(re.FindAllStringIndex(s, -1))
	}
	return n
}

// absoluteURL counts absolute and protocol-relative URL starts.
var absoluteURL = regexp.MustCompile(`(?i)(?:\b[a-z][a-z0-9+.-]*:)?//`)

// --- a small walker over the HTML this package renders ---------------------------

type element struct {
	name  string
	attrs map[string]string
}

var (
	tagRE         = regexp.MustCompile(`<(/?)([a-zA-Z][a-zA-Z0-9]*)([^>]*)>`)
	declarationRE = regexp.MustCompile(`<![^>]*>`)
	attrRE        = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9-]*)="([^"]*)"`)
)

var voidElements = map[string]bool{"meta": true, "br": true, "img": true, "link": true, "input": true, "hr": true}

// walkHTML calls text for every non-blank text node with its open elements, and
// returns how many times each element was opened. templ escapes '<' and '>' in
// text and attribute values, so a '>' only ever ends a tag here.
func walkHTML(t *testing.T, doc string, text func(stack []element, s string)) map[string]int {
	t.Helper()
	opened := map[string]int{}
	var stack []element
	// The doctype is a declaration, not an element or text.
	doc = declarationRE.ReplaceAllString(doc, "")
	pos := 0
	emit := func(s string) {
		if strings.TrimSpace(s) != "" && text != nil {
			text(stack, html.UnescapeString(s))
		}
	}
	for _, m := range tagRE.FindAllStringSubmatchIndex(doc, -1) {
		emit(doc[pos:m[0]])
		pos = m[1]
		closing := doc[m[2]:m[3]] == "/"
		name := strings.ToLower(doc[m[4]:m[5]])
		rest := doc[m[6]:m[7]]
		if closing {
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].name == name {
					stack = stack[:i]
					break
				}
			}
			continue
		}
		opened[name]++
		attrs := map[string]string{}
		for _, a := range attrRE.FindAllStringSubmatch(rest, -1) {
			attrs[strings.ToLower(a[1])] = html.UnescapeString(a[2])
		}
		if voidElements[name] || strings.HasSuffix(strings.TrimSpace(rest), "/") {
			continue
		}
		stack = append(stack, element{name, attrs})
	}
	emit(doc[pos:])
	return opened
}

// visibleText is the HTML's text nodes, unescaped, outside <head>.
func visibleText(t *testing.T, doc string) string {
	t.Helper()
	var b strings.Builder
	walkHTML(t, doc, func(stack []element, s string) {
		for _, e := range stack {
			if e.name == "head" {
				return
			}
		}
		b.WriteString(s)
		b.WriteString("\n")
	})
	return b.String()
}

func styleOf(e element) map[string]string {
	out := map[string]string{}
	for _, decl := range strings.Split(e.attrs["style"], ";") {
		k, v, ok := strings.Cut(decl, ":")
		if ok {
			out[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		}
	}
	return out
}

// hrefs returns every href value in the document.
func hrefs(t *testing.T, doc string) []string {
	t.Helper()
	var out []string
	for _, m := range tagRE.FindAllStringSubmatch(doc, -1) {
		for _, a := range attrRE.FindAllStringSubmatch(m[3], -1) {
			if strings.EqualFold(a[1], "href") {
				out = append(out, html.UnescapeString(a[2]))
			}
		}
	}
	return out
}

// --- the URL ------------------------------------------------------------------------

// TestRender_EachPartCarriesExactlyOneAbsoluteURL — ADR 0022 §8: exactly one
// literal absolute URL, the action link. DECISION: the parts are counted
// SEPARATELY, and each must carry the link exactly once — the HTML in its one
// href (the visible text holds no URL; the text part is where the link can be
// copied), the text part on its own line.
func TestRender_EachPartCarriesExactlyOneAbsoluteURL(t *testing.T) {
	names := append([]string{"Maria Borg", "Ħal Għaxaq Kebabs Ltd."}, hostileNames...)
	for _, name := range names {
		for _, r := range renderBoth(t, name, name) {
			if !strings.HasPrefix(r.link, testBase+"/") {
				t.Fatalf("%s: the test built a link outside its own base", r.kind)
			}
			if n := len(absoluteURL.FindAllString(r.msg.HTML, -1)); n != 1 {
				t.Errorf("%s, name %q: the HTML carries %d absolute URL(s), want exactly 1", r.kind, name, n)
			}
			if n := len(absoluteURL.FindAllString(r.msg.Text, -1)); n != 1 {
				t.Errorf("%s, name %q: the text part carries %d absolute URL(s), want exactly 1", r.kind, name, n)
			}
			hs := hrefs(t, r.msg.HTML)
			if len(hs) != 1 || hs[0] != r.link {
				t.Errorf("%s, name %q: %d href(s); want exactly one, equal to the link", r.kind, name, len(hs))
			}
			if n := strings.Count(r.msg.HTML, r.link); n != 1 {
				t.Errorf("%s, name %q: the link occurs %d time(s) in the HTML, want 1 (the href)", r.kind, name, n)
			}
			if n := strings.Count(r.msg.Text, r.link); n != 1 {
				t.Errorf("%s, name %q: the link occurs %d time(s) in the text part, want 1", r.kind, name, n)
			}
			if !strings.Contains(r.msg.Text, "\n"+r.link+"\n") {
				t.Errorf("%s: the link does not stand alone on its own line in the text part", r.kind)
			}
		}
	}
}

// TestRender_RefusesALinkOutsideTheExpectedAddress — the "only at the expected
// root" half: the link must be BaseURL + path + "?<param>=" + a base64url value.
func TestRender_RefusesALinkOutsideTheExpectedAddress(t *testing.T) {
	ctx := context.Background()
	v := randomValue(t)
	if len(host253) != 253 || len(host254) != 254 {
		t.Fatalf("the length fixtures are %d and %d bytes, want 253 and 254", len(host253), len(host254))
	}
	inv := func(base, link string) error {
		_, err := RenderInvitation(ctx, link, InvitationView{BaseURL: base, EmployeeName: "Maria", TenantName: "Kebab Factory", ValidFor: 7 * 24 * time.Hour})
		return err
	}
	rst := func(base, link string) error {
		_, err := RenderPasswordReset(ctx, link, ResetView{BaseURL: base, ValidFor: time.Hour})
		return err
	}
	tests := []struct {
		name string
		call func(base, link string) error
		base string
		link string
		want error // nil: accepted
	}{
		// controls: accepted shapes
		{"invitation", inv, testBase, testBase + "/activate?code=" + v, nil},
		{"reset", rst, testBase, testBase + "/admin/reset/new?t=" + v, nil},
		{"base with a trailing slash", inv, testBase + "/", testBase + "/activate?code=" + v, nil},
		{"base with a path", inv, testBase + "/app", testBase + "/app/activate?code=" + v, nil},
		{"plain http dev base with a port", inv, "http://localhost:8080", "http://localhost:8080/activate?code=" + v, nil},
		{"plain http on 127.0.0.1", inv, "http://127.0.0.1:8080", "http://127.0.0.1:8080/activate?code=" + v, nil},
		{"plain http under .localhost", inv, "http://app.localhost", "http://app.localhost/activate?code=" + v, nil},
		{"https with a port", rst, testBase + ":8443", testBase + ":8443/admin/reset/new?t=" + v, nil},
		// the link
		{"another host", inv, testBase, "https://evil.example/activate?code=" + v, ErrLink},
		{"a look-alike host", inv, testBase, testBase + ".evil.example/activate?code=" + v, ErrLink},
		{"another path", inv, testBase, testBase + "/admin/reset/new?code=" + v, ErrLink},
		{"the invitation's path on the reset", rst, testBase, testBase + "/activate?t=" + v, ErrLink},
		{"another parameter", inv, testBase, testBase + "/activate?t=" + v, ErrLink},
		// Only '&' and '=' are outside base64url here, so this row is refused for the
		// second parameter itself and nothing else.
		{"a second parameter", inv, testBase, testBase + "/activate?code=" + v + "&next=x", ErrLink},
		{"a fragment", inv, testBase, testBase + "/activate?code=" + v + "#x", ErrLink},
		{"a quote", inv, testBase, testBase + "/activate?code=" + v + `"`, ErrLink},
		{"markup", inv, testBase, testBase + "/activate?code=" + v + "<b>", ErrLink},
		{"a space", inv, testBase, testBase + "/activate?code=" + v + " x", ErrLink},
		{"a percent escape", inv, testBase, testBase + "/activate?code=" + v + "%2F", ErrLink},
		{"a second URL in the value", inv, testBase, testBase + "/activate?code=https://evil.example", ErrLink},
		{"an empty value", inv, testBase, testBase + "/activate?code=", ErrLink},
		{"a value past the bound", inv, testBase, testBase + "/activate?code=" + strings.Repeat("a", maxLinkValue+1), ErrLink},
		{"a relative link", inv, testBase, "/activate?code=" + v, ErrLink},
		{"upper-case scheme in the link only", inv, testBase, "HTTPS://app.taptime.test/activate?code=" + v, ErrLink},
		// the base
		{"an empty base", inv, "", "/activate?code=" + v, ErrBaseURL},
		{"a relative base", inv, "app.taptime.test", "app.taptime.test/activate?code=" + v, ErrBaseURL},
		{"another scheme", inv, "ftp://app.taptime.test", "ftp://app.taptime.test/activate?code=" + v, ErrBaseURL},
		{"javascript", inv, "javascript:alert(1)", "javascript:alert(1)/activate?code=" + v, ErrBaseURL},
		{"user info", inv, "https://user@app.taptime.test", "https://user@app.taptime.test/activate?code=" + v, ErrBaseURL},
		{"a query in the base", inv, testBase + "?x=1", testBase + "?x=1/activate?code=" + v, ErrBaseURL},
		{"an empty query in the base", inv, testBase + "/a?", testBase + "/a?/activate?code=" + v, ErrBaseURL},
		{"a fragment in the base", inv, testBase + "#x", testBase + "#x/activate?code=" + v, ErrBaseURL},
		{"a double slash in the path", inv, testBase + "//x", testBase + "//x/activate?code=" + v, ErrBaseURL},
		{"an escaped path", inv, testBase + "/a%2Fb", testBase + "/a%2Fb/activate?code=" + v, ErrBaseURL},
		{"upper-case scheme", inv, "HTTPS://app.taptime.test", "HTTPS://app.taptime.test/activate?code=" + v, ErrBaseURL},
		{"a quote in the base", inv, testBase + `"`, testBase + `"/activate?code=` + v, ErrBaseURL},
		// url.Parse accepts these in a path and the parse equals the input, so only
		// the byte set refuses them: a space would cut the copied link in two.
		{"a space in the base path", inv, testBase + "/a b", testBase + "/a b/activate?code=" + v, ErrBaseURL},
		{"an apostrophe in the base path", inv, testBase + "/a'b", testBase + "/a'b/activate?code=" + v, ErrBaseURL},
		{"an ampersand in the base path", inv, testBase + "/a&b", testBase + "/a&b/activate?code=" + v, ErrBaseURL},
		{"a scheme with no host", inv, "https:///x", "https:///x/activate?code=" + v, ErrBaseURL},
		// The host is a name, not merely a non-empty Host field (security review).
		{"a port and no host name", inv, "https://:443", "https://:443/activate?code=" + v, ErrBaseURL},
		{"a lone hyphen as the host", inv, "https://-", "https://-/activate?code=" + v, ErrBaseURL},
		{"a label ending in a hyphen", inv, "https://app-.taptime.test", "https://app-.taptime.test/activate?code=" + v, ErrBaseURL},
		{"an empty label", inv, "https://app..taptime.test", "https://app..taptime.test/activate?code=" + v, ErrBaseURL},
		{"an underscore in the host", inv, "https://app_x.taptime.test", "https://app_x.taptime.test/activate?code=" + v, ErrBaseURL},
		{"an empty port", inv, testBase + ":", testBase + ":/activate?code=" + v, ErrBaseURL},
		{"port zero", inv, testBase + ":0", testBase + ":0/activate?code=" + v, ErrBaseURL},
		{"a port past 65535", inv, testBase + ":65536", testBase + ":65536/activate?code=" + v, ErrBaseURL},
		// Plain http only on loopback (security review decision): elsewhere the code
		// would cross the network in clear text.
		{"plain http on a public name", inv, "http://taptime.mt", "http://taptime.mt/activate?code=" + v, ErrBaseURL},
		{"plain http on a public address", rst, "http://192.0.2.1", "http://192.0.2.1/admin/reset/new?t=" + v, ErrBaseURL},
		{"plain http on a name that only starts with localhost", inv, "http://localhost.evil.example", "http://localhost.evil.example/activate?code=" + v, ErrBaseURL},
		// "Loopback" is exactly localhost, a name UNDER ".localhost", or an IPv4
		// address in 127.0.0.0/8 — not a name that merely starts with "127.", not
		// one that ends in "localhost" without the dot, and not a private address.
		{"plain http on a name that starts with a loopback address", inv, "http://127.0.0.1.evil.example", "http://127.0.0.1.evil.example/activate?code=" + v, ErrBaseURL},
		{"plain http on a name that starts with 127.", inv, "http://127.evil.example", "http://127.evil.example/activate?code=" + v, ErrBaseURL},
		{"plain http on a name ending in localhost without the dot", inv, "http://evillocalhost", "http://evillocalhost/activate?code=" + v, ErrBaseURL},
		{"plain http on a private 10/8 address", inv, "http://10.0.0.1", "http://10.0.0.1/activate?code=" + v, ErrBaseURL},
		{"plain http on a private 192.168/16 address with a port", rst, "http://192.168.1.10:8080", "http://192.168.1.10:8080/admin/reset/new?t=" + v, ErrBaseURL},
		// The label rules, each at its own bound (controls with the bound itself
		// above the refusals would let an off-by-one through; see the two accepted
		// rows below).
		{"a label starting with a hyphen", inv, "https://-app.taptime.test", "https://-app.taptime.test/activate?code=" + v, ErrBaseURL},
		{"a 64-byte label", inv, "https://" + strings.Repeat("a", 64) + ".taptime.test", "https://" + strings.Repeat("a", 64) + ".taptime.test/activate?code=" + v, ErrBaseURL},
		{"a 254-byte host of 63-byte labels", inv, "https://" + host254, "https://" + host254 + "/activate?code=" + v, ErrBaseURL},
		{"a 63-byte label (control)", inv, "https://" + strings.Repeat("a", 63) + ".taptime.test", "https://" + strings.Repeat("a", 63) + ".taptime.test/activate?code=" + v, nil},
		{"a 253-byte host (control)", inv, "https://" + host253, "https://" + host253 + "/activate?code=" + v, nil},
		// KNOWN LIMIT, measured: the host rule is a SYNTAX rule; an IPv4-shaped name
		// is not checked as an address. Turns red if that changes, so the text is
		// updated with it.
		{"known limit: an out-of-range IPv4-shaped host", inv, "https://999.999.999.999", "https://999.999.999.999/activate?code=" + v, nil},
		{"known limit: a three-part IPv4-shaped host", inv, "https://1.2.3", "https://1.2.3/activate?code=" + v, nil},
		{"an IPv6 literal (brackets are outside the byte set)", inv, "http://[::1]:8080", "http://[::1]:8080/activate?code=" + v, ErrBaseURL},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call(tc.base, tc.link)
			switch {
			case tc.want == nil && err != nil:
				t.Fatalf("refused a valid link: %v", err)
			case tc.want != nil && !errors.Is(err, tc.want):
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	// A refusal returns an empty Message, never a partly filled one.
	m, err := RenderInvitation(ctx, "https://evil.example/activate?code="+v, InvitationView{BaseURL: testBase, ValidFor: time.Hour})
	if err == nil || m != (mail.Message{}) {
		t.Fatal("a refused link did not return an error and an empty Message")
	}
}

// TestRender_AcceptsTheResetLinkAdminauthMints drives the producer the reset
// e-mail will receive its link from: the base the reset handler builds
// (BaseURL trimmed + "/admin/reset/new") through adminauth's own Link.
func TestRender_AcceptsTheResetLinkAdminauthMints(t *testing.T) {
	for _, base := range []string{testBase, testBase + "/"} {
		link := adminauth.IssuedReset{Token: adminauth.ParseResetToken(randomValue(t))}.
			Link(strings.TrimRight(base, "/") + "/admin/reset/new")
		if _, err := RenderPasswordReset(context.Background(), link, ResetView{BaseURL: base, ValidFor: adminauth.ResetTTL}); err != nil {
			t.Errorf("base with trailing slash %v: the link adminauth mints was refused: %v",
				strings.HasSuffix(base, "/"), err)
		}
	}
}

// --- nothing remote -----------------------------------------------------------------

// TestRender_LoadsNothingAndUsesOnlyTheseElements — §8: no image, no remote font,
// no external URL, no tracking pixel. The element set is the whole vocabulary:
// anything new (an <img>, a <link>, a <style>, a <script>) turns it red.
func TestRender_LoadsNothingAndUsesOnlyTheseElements(t *testing.T) {
	want := []string{"a", "body", "div", "h1", "head", "html", "meta", "p", "title"}
	forbidden := []string{"<img", "<link", "<script", "<style", "<iframe", "<object", "<embed",
		"<svg", "<video", "<audio", "<form", "<base", "@font-face", "@import", "url(",
		" src=", " srcset=", " background=", " poster=", " action="}
	for _, r := range renderBoth(t, "<script>alert(1)</script>", "<img src=x>") {
		low := strings.ToLower(r.msg.HTML)
		for _, f := range forbidden {
			if n := strings.Count(low, f); n != 0 {
				t.Errorf("%s: the HTML carries %q %d time(s)", r.kind, f, n)
			}
		}
		opened := walkHTML(t, r.msg.HTML, nil)
		var got []string
		for name := range opened {
			got = append(got, name)
		}
		sort.Strings(got)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: elements %v, want exactly %v", r.kind, got, want)
		}
		if opened["a"] != 1 {
			t.Errorf("%s: %d anchors, want 1", r.kind, opened["a"])
		}
		if !strings.Contains(r.msg.HTML, `<meta charset="utf-8">`) {
			t.Errorf("%s: the HTML does not declare UTF-8", r.kind)
		}
		// The attribute vocabulary is closed too: a colour, an image or a URL in an
		// attribute this list does not name (bgcolor=, background=, src=, …) fails.
		allowed := map[string]bool{"lang": true, "charset": true, "name": true, "content": true, "style": true, "href": true}
		for _, a := range attributeNames(r.msg.HTML) {
			if !allowed[a] {
				t.Errorf("%s: the HTML carries the attribute %q", r.kind, a)
			}
		}
	}
}

// attributeNames lists every attribute name written in the document's tags,
// valued or not.
func attributeNames(doc string) []string {
	var out []string
	quoted := regexp.MustCompile(`="[^"]*"`)
	for _, m := range tagRE.FindAllStringSubmatch(doc, -1) {
		for _, f := range strings.Fields(quoted.ReplaceAllString(m[3], "")) {
			if f = strings.ToLower(strings.TrimSuffix(f, "/")); f != "" {
				out = append(out, f)
			}
		}
	}
	return out
}

// --- names ----------------------------------------------------------------------------

// TestNames_AnAddressShapedNameIsWithheld — ADR 0022 §8's DKIM decision (counted
// limit 22, handed to EM-4): a name that could become a link is not shown, the
// neutral words stand in, and the body holds no linkifiable run except the link
// in the text part.
func TestNames_AnAddressShapedNameIsWithheld(t *testing.T) {
	names := append([]string{}, hostileNames...)
	for _, r := range nfkcDots {
		// nameShown's argument (letter.go): none of these is a character it admits,
		// under the Unicode tables of the Go that runs this test.
		if unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsDigit(r) {
			t.Errorf("%U folds to a dot under NFKC and is a letter, mark or digit here", r)
		}
		names = append(names, "evil"+string(r)+"example")
	}
	for _, name := range names {
		if _, ok := nameShown(name); ok {
			t.Errorf("nameShown accepted %q", name)
		}
		for _, r := range renderBoth(t, name, name) {
			if strings.Contains(r.msg.Text, name) || strings.Contains(r.msg.HTML, name) ||
				strings.Contains(r.msg.HTML, html.EscapeString(name)) {
				t.Errorf("%s: the name %q reached the body", r.kind, name)
			}
			if n := linkifiableRuns(visibleText(t, r.msg.HTML)); n != 0 {
				t.Errorf("%s, name %q: the visible HTML text holds %d linkifiable run(s), want 0", r.kind, name, n)
			}
			if n := linkifiableRuns(strings.Replace(r.msg.Text, r.link, "", 1)); n != 0 {
				t.Errorf("%s, name %q: the text part holds %d linkifiable run(s) besides the link", r.kind, name, n)
			}
			if r.kind == "invitation" {
				for _, part := range []string{r.msg.Text, visibleText(t, r.msg.HTML)} {
					if !strings.Contains(part, greetingWithoutName) || !strings.Contains(part, inviterWithoutName+" has invited you") {
						t.Errorf("name %q: the neutral words did not stand in", name)
					}
				}
			}
		}
	}
}

// TestLinkifiable_CatchesWhatItExistsToCatch is the detector's negative control:
// each hostile shape, written raw into a sentence, IS counted. Without it a broken
// detector would leave the test above green over an e-mail full of links.
func TestLinkifiable_CatchesWhatItExistsToCatch(t *testing.T) {
	shapes := []string{"https://evil.example/login", "evil.example", "www.evil.example",
		"192.0.2.1", "help@evil.example", "javascript:alert(1)", "//evil.example",
		"mailto:x", "tel:21234567"}
	for _, r := range []rune{0x3002, 0xFF0E, 0xFF61, 0xFE52, 0x2024} {
		shapes = append(shapes, "evil"+string(r)+"example")
	}
	for _, s := range shapes {
		if linkifiableRuns("Hello "+s+", welcome.") == 0 {
			t.Errorf("the detector missed %q", s)
		}
	}
	for _, s := range []string{"Kebab Factory Ltd. has invited you", "Hello Ġużeppi,", "evil. example", "Ta' Marija & Sons (Sliema)"} {
		if n := linkifiableRuns(s); n != 0 {
			t.Errorf("the detector counts %d run(s) in the harmless %q", n, s)
		}
	}
}

// seedNames reads every tenant name and employee full name out of the seed.
//
// 🔴 IT IS HELD TO THE SEED'S OWN ROW COUNT, NOT TO A THRESHOLD. The first version
// required whitespace after every comma and silently skipped the three employee
// rows written without it (an audit measured 33 read of 36, and a "J.B." name put
// on one of the skipped rows left the test green). So each INSERT block is also
// counted by a SECOND, independent anchor — a row starts with "('<uuid>'" — and
// the names read must be exactly that many.
func seedNames(t *testing.T) (tenants, employees []string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "seed.sql"))
	if err != nil {
		t.Fatalf("reading the seed: %v", err)
	}
	blocks := func(table string) []string {
		out := regexp.MustCompile(`(?s)INSERT INTO `+table+`\b.*?ON CONFLICT`).FindAllString(string(raw), -1)
		if len(out) == 0 {
			t.Fatalf("the seed has no INSERT INTO %s block", table)
		}
		return out
	}
	rowStart := regexp.MustCompile(`(?m)^\s*\('[0-9a-f-]{36}'`)
	unq := func(s string) string { return strings.ReplaceAll(s, "''", "'") }
	read := func(table string, name *regexp.Regexp) []string {
		var names []string
		rows := 0
		for _, b := range blocks(table) {
			rows += len(rowStart.FindAllString(b, -1))
			for _, m := range name.FindAllStringSubmatch(b, -1) {
				names = append(names, unq(m[1]))
			}
		}
		if rows == 0 || len(names) != rows {
			t.Fatalf("seed %s: read %d name(s) from %d row(s); the reader skips rows", table, len(names), rows)
		}
		return names
	}
	tenants = read("tenants", regexp.MustCompile(`\('[0-9a-f-]{36}',\s*'((?:[^']|'')*)',\s*'MT\d+'`))
	employees = read("employees", regexp.MustCompile(`(?m)^\s*'((?:[^']|'')+)',\s*'(?:[^']|'')+',\s*(?:'[^']*'(?:::citext)?|NULL),\s*'(?:active|invited|deactivated)'\)`))
	return tenants, employees
}

// TestNames_AnOrdinaryNameIsShownVerbatim — the cost side of the gate, measured:
// real names pass, Maltese letters arrive intact (UTF-8, ċ ġ ħ ż), the text part
// carries them as typed and the HTML carries them escaped.
func TestNames_AnOrdinaryNameIsShownVerbatim(t *testing.T) {
	// seedNames fails unless it read exactly one name per seed row.
	tenants, employees := seedNames(t)
	ordinary := []string{
		"Ħal Għaxaq Kebabs", "Ġużeppi Ċaruana", "Żejtun Bakery", "Ta' Marija & Sons",
		"O’Brien", "Kebab Factory – Sliema", "Kebab Factory Ltd.", "Kebab Manufacturing Co. Ltd.",
		"Yo! Sushi (Valletta)", "Bar 21", "Smith, Jones", "José Müller", "Nguyễn Văn An",
	}
	var withheld []string
	for _, n := range append(append(append([]string{}, tenants...), employees...), ordinary...) {
		if _, ok := nameShown(n); !ok {
			withheld = append(withheld, n)
		}
	}
	if len(withheld) != 0 {
		t.Errorf("ordinary names withheld: %q", withheld)
	}
	t.Logf("names shown: %d seed tenant, %d seed employee, %d listed", len(tenants), len(employees), len(ordinary))

	// The two characters a shown name may carry that HTML escapes: ' and &.
	employee, business := "Ġużeppi O'Neill", "Ħal Għaxaq Kebabs & Sons"
	r := renderBoth(t, employee, business)[0]
	if !utf8.ValidString(r.msg.Text) || !utf8.ValidString(r.msg.HTML) {
		t.Fatal("a part is not valid UTF-8")
	}
	for _, want := range []string{"Hello " + employee + ",", business + " has invited you"} {
		if !strings.Contains(r.msg.Text, want) {
			t.Errorf("the text part does not carry %q verbatim", want)
		}
		if !strings.Contains(r.msg.HTML, html.EscapeString(want)) {
			t.Errorf("the HTML does not carry %q, escaped", want)
		}
	}
	if strings.Contains(r.msg.HTML, business) || strings.Contains(r.msg.HTML, employee) {
		t.Error("the HTML carries a raw ' or & of a name; it must be escaped")
	}
	// The four Maltese letters, both cases, as UTF-8 bytes in both parts.
	r2 := renderBoth(t, "Ċensu Ġorġ Ħabib Żammit", "ċ ġ ħ ż Kebabs")[0]
	for _, letter := range []string{"ċ", "ġ", "ħ", "ż", "Ċ", "Ġ", "Ħ", "Ż"} {
		if strings.Count(r2.msg.Text, letter) == 0 || strings.Count(r2.msg.HTML, letter) == 0 {
			t.Errorf("a part lost %q", letter)
		}
	}
}

// TestNames_ALineBreakNeverReachesTheTextPart — the text part has no escaping, so
// a name with a line break would forge lines in it. Each such name is withheld,
// and the text part has the same number of lines as with no name at all.
func TestNames_ALineBreakNeverReachesTheTextPart(t *testing.T) {
	baseline := strings.Count(renderBoth(t, "", "")[0].msg.Text, "\n")
	for _, name := range []string{"A\nB", "A\rB", "A\r\nB", "A\u2028B", "A\u2029B", "A\u0085B", "A\vB", "A\fB", "A\x00B", "A\u202eB"} {
		if _, ok := nameShown(name); ok {
			t.Errorf("nameShown accepted %q", name)
		}
		r := renderBoth(t, name, name)[0]
		if got := strings.Count(r.msg.Text, "\n"); got != baseline {
			t.Errorf("name %q: the text part has %d line break(s), want %d", name, got, baseline)
		}
		for _, c := range []string{"\r", "\u2028", "\u2029", "\u0085", "\v", "\f", "\x00", "\u202e"} {
			if strings.Contains(r.msg.Text, c) || strings.Contains(r.msg.HTML, c) {
				t.Errorf("name %q: a part carries %q", name, c)
			}
		}
	}
}

// TestNames_KnownLimitIsALookalikeDotAndPlainProse measures what the gate does
// NOT stop, so the limit is a number and not a hope: EVERY character that looks
// like a dot but is a letter, a mark or a digit — not a label separator for the
// detector — is an ordinary name character, so "evil<it>example" is shown and
// reads like an address to a person. Measured with U+A4F8 (LISU LETTER TONE MYA
// TI, Lm), U+0323 (COMBINING DOT BELOW, Mn), U+0660 (ARABIC-INDIC DIGIT ZERO, Nd)
// and U+06F0 (EXTENDED ARABIC-INDIC DIGIT ZERO, Nd). Digits — a phone number for
// a client's data detector (not measured), full-width digits included — and plain
// call-back prose are ordinary name characters too. All are shown. If the gate
// starts withholding one, this goes red and the limit's text is updated.
func TestNames_KnownLimitIsALookalikeDotAndPlainProse(t *testing.T) {
	lookalike := func(r rune) string { return "evil" + string(r) + "example" }
	fullWidth := "Call " + string([]rune{0xFF12, 0xFF11, 0xFF12, 0xFF13, 0xFF14, 0xFF15, 0xFF16, 0xFF17}) + " now"
	for _, name := range []string{lookalike(0x0323), lookalike(0x0660), lookalike(0x06F0), fullWidth,
		"Your account is suspended Call 21234567 now!", "evil\ua4f8example", "Call 21234567 to confirm", "Your account is locked reply now"} {
		if _, ok := nameShown(name); !ok {
			t.Errorf("%q is withheld: the counted limit no longer holds, update its text", name)
		}
		r := renderBoth(t, name, name)[0]
		if !strings.Contains(r.msg.Text, name) {
			t.Errorf("%q is not in the text part", name)
		}
		if n := linkifiableRuns(visibleText(t, r.msg.HTML)); n != 0 {
			t.Errorf("%q: the detector counts %d run(s) in the visible HTML", name, n)
		}
	}
}

// TestNames_TheDotRule — a '.' may end the name or be followed by ' ', ',' or ')'
// (none of the three can continue a label); followed by anything else it
// withholds the name. A shown row is also rendered, and the detector must count
// nothing in its visible text.
func TestNames_TheDotRule(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		shown bool
	}{
		{"ends the name", "Kebab Factory Ltd.", true},
		{"followed by a space", "Kebab Co. Ltd", true},
		{"followed by a comma", "Kebab Co., Ltd.", true},
		{"followed by a closing parenthesis", "Kebab Factory (Malta Ltd.)", true},
		{"a comma between two labels", "evil.,example", true},
		{"a closing parenthesis between two labels", "evil.)example", true},
		{"followed by a letter", "evil.example", false},
		{"followed by an upper-case letter", "evil.Example", false},
		{"followed by a Maltese letter", "evil.ċom", false},
		{"followed by a digit", "Bar 1.5", false},
		{"followed by a hyphen", "evil.-example", false},
		{"followed by an opening parenthesis", "evil.(example", false},
		{"followed by an apostrophe", "evil.'example", false},
		{"followed by an ampersand", "evil.&example", false},
		{"followed by another dot", "evil..example", false},
		{"followed by an exclamation mark", "evil.!example", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := nameShown(tc.in); ok != tc.shown {
				t.Fatalf("nameShown(%q) shown = %v, want %v", tc.in, ok, tc.shown)
			}
			if !tc.shown {
				return
			}
			r := renderBoth(t, tc.in, tc.in)[0]
			if !strings.Contains(r.msg.Text, tc.in) {
				t.Errorf("%q is not in the text part", tc.in)
			}
			if n := linkifiableRuns(visibleText(t, r.msg.HTML)); n != 0 {
				t.Errorf("%q: the detector counts %d run(s) in the visible HTML", tc.in, n)
			}
		})
	}
	// THE WHOLE PRINTABLE ASCII RANGE AFTER A DOT (0x20..0x7E): "a.<c>b" is shown
	// for exactly ' ', ',' and ')', and withheld for every other character — so
	// widening the rule by any one ASCII character turns this red (the table above
	// names a dozen; this counts all ninety-five).
	shown := ""
	for c := byte(0x20); c <= 0x7E; c++ {
		if _, ok := nameShown("a." + string(rune(c)) + "b"); ok {
			shown += string(rune(c))
		}
	}
	// Byte order: ' ' (0x20), ')' (0x29), ',' (0x2C).
	if shown != " )," {
		t.Errorf("after a '.', these ASCII characters keep the name: %q, want exactly \" ),\"", shown)
	}
}

// TestNames_EveryASCIICharacterBetweenTwoLetters pins the allowlist's ASCII
// count: each printable character 0x21..0x7E between two letters. A letter or a
// digit is shown, and of the rest exactly & ' - , ( ) ! — written out HERE, not
// read from nameMarks, so a character added to the list turns this red. '.' is
// withheld in this position (a letter follows it; TestNames_TheDotRule covers the
// rest of its rule).
func TestNames_EveryASCIICharacterBetweenTwoLetters(t *testing.T) {
	const marks = "&'-,()!"
	shown := 0
	for c := byte(0x21); c <= 0x7E; c++ {
		alnum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		want := alnum || strings.IndexByte(marks, c) >= 0
		if _, ok := nameShown("a" + string(rune(c)) + "b"); ok != want {
			t.Errorf("%q between two letters: shown = %v, want %v", string(rune(c)), ok, want)
		}
		if want {
			shown++
		}
	}
	// 26 + 26 letters, 10 digits, 7 marks.
	if shown != 69 {
		t.Fatalf("expected %d shown characters, the sweep's own arithmetic says 69", shown)
	}
}

// TestNames_KnownLimitIsAnUnusualNameWithheld measures the gate's COST: real but
// unusual names that carry a character outside the allowlist are withheld (the
// e-mail says "Hello," or "Your employer" instead). None is in the seed. If the
// gate learns to show one safely, this goes red and the limit's text is updated.
func TestNames_KnownLimitIsAnUnusualNameWithheld(t *testing.T) {
	for _, name := range []string{"J.B. Bar", "Fish/Chips", "Wine+Dine", "Bar #1", `The "Spot"`, "Ristorante: Da Mario"} {
		if _, ok := nameShown(name); ok {
			t.Errorf("%q is shown: the counted limit no longer holds, update its text", name)
		}
	}
}

// TestNames_TheGateTakesEveryStoredLength: a name as long as the longest the
// product stores is shown; one rune more is not. If signup or staff raises its
// cap, this goes red, and the gate follows.
func TestNames_TheGateTakesEveryStoredLength(t *testing.T) {
	for what, stored := range map[string]int{
		"signup.MaxCompanyNameRunes":  signup.MaxCompanyNameRunes,
		"tenant.MaxEmployeeNameRunes": tenant.MaxEmployeeNameRunes,
	} {
		if stored > maxShownNameRunes {
			t.Errorf("%s is %d, past the gate's %d: a stored name would be withheld for its length", what, stored, maxShownNameRunes)
		}
	}
	if _, ok := nameShown(strings.Repeat("ħ", maxShownNameRunes)); !ok {
		t.Error("a name at the bound is withheld")
	}
	if _, ok := nameShown(strings.Repeat("ħ", maxShownNameRunes+1)); ok {
		t.Error("a name past the bound is shown")
	}
}

// --- escaping --------------------------------------------------------------------------

// TestTemplate_EscapesWhateverReachesIt measures the SECOND layer alone: the gate
// is bypassed and hostile text is put straight into the letter. templ must escape
// it in every slot (title, heading, paragraphs, action, closing).
func TestTemplate_EscapesWhateverReachesIt(t *testing.T) {
	const hostile = `<script>alert(1)</script> "q" 'a' & <img src=x>`
	l := letter{
		subject: hostile, heading: hostile, before: []string{hostile},
		action: hostile, after: []string{hostile}, closing: hostile,
	}
	link := testBase + activatePath + "?" + activateParam + "=" + randomValue(t)
	var b strings.Builder
	if err := messageHTML(l, link).Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := b.String()
	for _, raw := range []string{"<script", "<img", `"q"`, "'a'", "</script>"} {
		if strings.Contains(out, raw) {
			t.Errorf("the HTML carries the unescaped %q", raw)
		}
	}
	if n := strings.Count(out, html.EscapeString(hostile)); n != 6 {
		t.Errorf("the escaped text appears %d time(s), want 6 (one per slot)", n)
	}
}

// --- the subject ------------------------------------------------------------------------

// TestSubject_IsFixedASCIIAndPassesTheMailComposer — §4/§8 and the EM-4 criterion
// as corrected by EM-1 (correction 4): the subjects are constants, printable
// ASCII without "=?", so internal/mail's Q encoder leaves them as written, while a
// Maltese string WOULD be encoded to "=?utf-8?q?". And each whole Message passes
// internal/mail's own composer: Send, with a context already cancelled, answers
// timeout — the class that comes AFTER composing — and never invalid_message,
// and the listener it points at accepts no connection.
func TestSubject_IsFixedASCIIAndPassesTheMailComposer(t *testing.T) {
	a, b := renderBoth(t, "Maria", "Kebab Factory"), renderBoth(t, "Ġużeppi", "Ħal Għaxaq")
	for i := range a {
		s := a[i].msg.Subject
		if s != b[i].msg.Subject {
			t.Errorf("%s: the subject changes with the names", a[i].kind)
		}
		for _, n := range []string{"Maria", "Kebab", "Ġużeppi", "Ħal"} {
			if strings.Contains(s, n) || strings.Contains(b[i].msg.Subject, n) {
				t.Errorf("%s: the subject carries a name", a[i].kind)
			}
		}
		for j := 0; j < len(s); j++ {
			if s[j] < 0x20 || s[j] > 0x7e {
				t.Errorf("%s: the subject has the byte %#x", a[i].kind, s[j])
			}
		}
		if s == "" || strings.Contains(s, "=?") {
			t.Errorf("%s: the subject is empty or holds \"=?\"", a[i].kind)
		}
		if got := mime.QEncoding.Encode("utf-8", s); got != s {
			t.Errorf("%s: mime.QEncoding changed the subject to %q", a[i].kind, got)
		}
	}
	if got := mime.QEncoding.Encode("utf-8", "Ħal Għaxaq ċ ġ ħ ż"); !strings.HasPrefix(got, "=?utf-8?q?") {
		t.Errorf("CONTROL: a Maltese subject would encode to %q, want an =?utf-8?q? word", got)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	sender, err := mail.New(mail.Config{
		Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port,
		Username: mail.NewCredential("probe-user"), Password: mail.NewCredential(rand.Text()),
		From: "Taptime <no-reply@taptime.test>",
	})
	if err != nil {
		t.Fatalf("mail.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	class := func(m mail.Message) mail.Class {
		m.To = "maria@example.test"
		_, err := sender.Send(ctx, m)
		var se *mail.SendError
		if !errors.As(err, &se) {
			t.Fatalf("Send returned a %T, want *mail.SendError", err)
		}
		return se.Class
	}
	for _, r := range append(a, b...) {
		if got := class(r.msg); got != mail.ClassTimeout {
			t.Errorf("%s: Send answered %s, want timeout (composed, then stopped by the cancelled context)", r.kind, got)
		}
	}
	// CONTROL: the same path DOES refuse a subject the composer refuses.
	bad := a[0].msg
	bad.Subject = "=?utf-8?q?x?="
	if got := class(bad); got != mail.ClassInvalidMessage {
		t.Errorf("CONTROL: an encoded-word subject answered %s, want invalid_message", got)
	}
	if err := ln.(*net.TCPListener).SetDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	if c, err := ln.Accept(); err == nil {
		c.Close()
		t.Error("a connection reached the listener; the probe must not dial")
	}
}

// --- contrast -----------------------------------------------------------------------------

// palette reads the nine tokens from tailwind.config.js, the palette's source.
func palette(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "tailwind.config.js"))
	if err != nil {
		t.Fatalf("reading tailwind.config.js: %v", err)
	}
	out := map[string]string{}
	for _, m := range regexp.MustCompile(`(?m)^\s*'?([a-z][a-z-]*)'?\s*:\s*'#([0-9A-Fa-f]{6})',?$`).FindAllStringSubmatch(string(raw), -1) {
		out["#"+strings.ToUpper(m[2])] = m[1]
	}
	if len(out) != 9 {
		t.Fatalf("read %d palette token(s) from tailwind.config.js, want 9", len(out))
	}
	return out
}

func channel(v uint64) float64 {
	c := float64(v) / 255
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func luminance(hex string) float64 {
	n, err := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	if err != nil || len(hex) != 7 {
		return math.NaN()
	}
	return 0.2126*channel(n>>16&0xFF) + 0.7152*channel(n>>8&0xFF) + 0.0722*channel(n&0xFF)
}

func contrast(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

const aaNormalText = 4.5

// styleAttributes returns every style attribute value in the document, unescaped.
func styleAttributes(doc string) []string {
	var out []string
	for _, m := range tagRE.FindAllStringSubmatch(doc, -1) {
		for _, a := range attrRE.FindAllStringSubmatch(m[3], -1) {
			if strings.EqualFold(a[1], "style") {
				out = append(out, html.UnescapeString(a[2]))
			}
		}
	}
	return out
}

// colourProperty: the CSS properties whose value can carry a colour, by name.
//
// 🔴 THE SHORTHANDS COUNT. An audit put "text-decoration:underline red" on the
// button — the template's OWN text-decoration declaration — and the reader, which
// then looked only at *color*, background*, border* and outline*, passed it.
// text-decoration, column-rule, -webkit-text-stroke and text-emphasis are
// shorthands that take a colour, so each prefix is read.
func colourProperty(prop string) bool {
	return strings.Contains(prop, "color") || strings.HasPrefix(prop, "background") ||
		strings.HasPrefix(prop, "border") || strings.HasPrefix(prop, "outline") ||
		strings.HasPrefix(prop, "text-decoration") || strings.HasPrefix(prop, "column-rule") ||
		strings.HasPrefix(prop, "-webkit-text-stroke") || strings.HasPrefix(prop, "text-emphasis") ||
		prop == "box-shadow" || prop == "text-shadow" || prop == "fill" || prop == "stroke"
}

// blendingProperty: properties that repaint every colour under them — a palette
// hex drawn through opacity, a filter or a blend is no longer that hex, and the
// contrast arithmetic above would be measuring colours nobody sees.
func blendingProperty(prop string) bool {
	switch prop {
	case "opacity", "filter", "-webkit-filter", "backdrop-filter", "-webkit-backdrop-filter",
		"mix-blend-mode", "background-blend-mode":
		return true
	}
	return false
}

var (
	// hexRun is a '#' and the WHOLE run of hex digits after it, so a 3-, 4- or
	// 8-digit form is read at its true length.
	hexRun       = regexp.MustCompile(`#([0-9A-Fa-f]+)`)
	cssLength    = regexp.MustCompile(`^-?(?:\d+|\d*\.\d+)(?:px|em|rem|pt|%)?$`)
	cssLineStyle = map[string]bool{"none": true, "solid": true, "dashed": true, "dotted": true, "double": true}
)

// colourValueProblem returns the first token of a colour-bearing value that is
// not a palette hex, a length or a line-style keyword, or "" when there is none.
func colourValueProblem(val string, pal map[string]string) string {
	if strings.ContainsAny(val, "()") {
		return val
	}
	for _, tok := range strings.FieldsFunc(val, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' }) {
		switch {
		case strings.HasPrefix(tok, "#"):
			if _, ok := pal[strings.ToUpper(tok)]; !ok || len(tok) != 7 {
				return tok
			}
		case cssLength.MatchString(tok), cssLineStyle[strings.ToLower(tok)]:
		default:
			return tok
		}
	}
	return ""
}

// TestContrast_EveryTextOnItsGroundClearsAA — skill tappa-brand: "Kontrast AA —
// hesapla". Every text node's colour and ground are read from the inline styles of
// the node's own element or its nearest ancestor that sets one; a node with
// either missing is a failure (it would take the client's default), and so is a
// colour outside the palette. The pairs and their ratios are logged.
func TestContrast_EveryTextOnItsGroundClearsAA(t *testing.T) {
	pal := palette(t)
	pairs := map[[2]string]float64{}
	for _, r := range renderBoth(t, "Maria Borg", "Kebab Factory Ltd.") {
		nodes := 0
		walkHTML(t, r.msg.HTML, func(stack []element, s string) {
			for _, e := range stack {
				if e.name == "head" {
					return
				}
			}
			nodes++
			var fg, bg string
			for i := len(stack) - 1; i >= 0 && (fg == "" || bg == ""); i-- {
				st := styleOf(stack[i])
				if fg == "" {
					fg = st["color"]
				}
				if bg == "" {
					bg = st["background-color"]
				}
			}
			if len(stack) == 0 || fg == "" || bg == "" {
				t.Errorf("%s: a text node has no explicit colour or ground (open elements: %d)", r.kind, len(stack))
				return
			}
			fg, bg = strings.ToUpper(fg), strings.ToUpper(bg)
			if _, ok := pal[fg]; !ok {
				t.Errorf("%s: text colour %s is not a palette token", r.kind, fg)
			}
			if _, ok := pal[bg]; !ok {
				t.Errorf("%s: ground %s is not a palette token", r.kind, bg)
			}
			ratio := contrast(fg, bg)
			pairs[[2]string{pal[fg], pal[bg]}] = ratio
			if !(ratio >= aaNormalText) {
				t.Errorf("%s: %s on %s is %.2f:1, below AA %.1f:1", r.kind, pal[fg], pal[bg], ratio, aaNormalText)
			}
		})
		if nodes < 8 {
			t.Fatalf("%s: walked %d text node(s); the message has more, the walker is blind", r.kind, nodes)
		}
		// Every colour is a palette hex, measured two ways. 🔴 The first version
		// scanned for 6- or 3-digit hexes and an audit measured it blind to
		// "border:1px solid red" and to the 8-digit "#C9D2C880" (\b does not hold
		// inside a longer run). Now:
		//   (1) every "#"-run of hex digits in the unescaped document is exactly
		//       six digits and a palette token (so 3-, 4- and 8-digit forms fail);
		//   (2) every declaration whose property can carry a colour has a value
		//       made only of palette hexes, lengths and line-style keywords —
		//       a named colour (red, transparent, currentColor …), a function
		//       (rgb(, hsl(, var( …) or any other word fails (colourValueProblem).
		// A colour outside a style attribute (bgcolor= and the like) is closed by
		// the attribute allowlist in TestRender_LoadsNothingAndUsesOnlyTheseElements.
		for _, m := range hexRun.FindAllStringSubmatch(html.UnescapeString(r.msg.HTML), -1) {
			if _, ok := pal["#"+strings.ToUpper(m[1])]; len(m[1]) != 6 || !ok {
				t.Errorf("%s: #%s is not a six-digit palette token", r.kind, m[1])
			}
		}
		decls := 0
		for _, style := range styleAttributes(r.msg.HTML) {
			for _, decl := range strings.Split(style, ";") {
				prop, val, ok := strings.Cut(decl, ":")
				prop = strings.ToLower(strings.TrimSpace(prop))
				if ok && blendingProperty(prop) {
					t.Errorf("%s: %s:%s repaints the colours under it; only plain palette hexes are allowed", r.kind, prop, val)
				}
				if !ok || !colourProperty(prop) {
					continue
				}
				decls++
				if bad := colourValueProblem(val, pal); bad != "" {
					t.Errorf("%s: %s:%s carries %q, which is not a palette hex", r.kind, prop, val, bad)
				}
			}
		}
		if decls < 10 {
			t.Fatalf("%s: read %d colour declaration(s); the message has more, the reader is blind", r.kind, decls)
		}
	}
	if len(pairs) < 3 {
		t.Fatalf("found %d colour pair(s); the message has more", len(pairs))
	}
	keys := make([][2]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i][0]+keys[i][1] < keys[j][0]+keys[j][1] })
	for _, k := range keys {
		t.Logf("%s on %s: %.2f:1", k[0], k[1], pairs[k])
	}
}

// TestContrast_ColourReaderCatchesWhatItExistsToCatch is the colour reader's
// negative control: each off-palette way of writing a colour is refused, and the
// palette's own forms pass.
func TestContrast_ColourReaderCatchesWhatItExistsToCatch(t *testing.T) {
	pal := palette(t)
	for _, v := range []string{"red", "transparent", "currentColor", "inherit", "white",
		"rgb(21,34,25)", "rgba(21,34,25,.5)", "hsl(0,0%,0%)", "var(--x)", "#C9D2C880", "#CCC",
		"#C9D2C8F", "#CCCCCC", "1px solid red", "1px solid #C9D2C880", "#152219 !important"} {
		if colourValueProblem(v, pal) == "" {
			t.Errorf("the reader passed %q", v)
		}
	}
	for _, v := range []string{"#152219", "#1f5c41", "1px solid #C9D2C8", "0", "2px dashed #EDF0EA"} {
		if bad := colourValueProblem(v, pal); bad != "" {
			t.Errorf("the reader refused the palette form %q at %q", v, bad)
		}
	}
	for _, p := range []string{"color", "background", "background-color", "border", "border-top-color",
		"outline", "box-shadow", "text-decoration", "text-decoration-color", "column-rule",
		"-webkit-text-stroke", "text-emphasis", "caret-color", "accent-color"} {
		if !colourProperty(p) {
			t.Errorf("%s is not read as a colour property", p)
		}
	}
	// The template's own button declaration, as the audit wrote it, is refused.
	if colourValueProblem("underline red", pal) == "" {
		t.Error("the reader passed text-decoration's \"underline red\"")
	}
	for _, p := range []string{"opacity", "filter", "-webkit-filter", "backdrop-filter", "mix-blend-mode", "background-blend-mode"} {
		if !blendingProperty(p) {
			t.Errorf("%s is not refused as a blending property", p)
		}
	}
	// The document-wide hex scan sees past a six-digit prefix.
	if got := hexRun.FindStringSubmatch("border:1px solid #C9D2C880"); len(got) < 2 || len(got[1]) != 8 {
		t.Errorf("the hex scan read %v, want the whole eight digits", got)
	}
}

// TestContrast_MathIsNotVacuous is the arithmetic's control: known ratios come
// out, and the brand's measured failures still fail.
func TestContrast_MathIsNotVacuous(t *testing.T) {
	for _, tc := range []struct {
		fg, bg string
		want   float64
	}{
		{"#000000", "#FFFFFF", 21},
		{"#FFFFFF", "#FFFFFF", 1},
		{"#152219", "#FFFDF4", 16.17}, // ink on paper (the skill's table)
		{"#1F5C41", "#FFFDF4", 7.73},  // tappa-green on paper (the skill's table)
		{"#D98E2B", "#FFFDF4", 2.62},  // saffron on paper: the skill's measured failure
		{"#C9D2C8", "#FFFDF4", 1.52},  // line on paper: likewise
	} {
		if got := contrast(tc.fg, tc.bg); math.Abs(got-tc.want) > 0.02 {
			t.Errorf("%s on %s = %.2f:1, want %.2f:1", tc.fg, tc.bg, got, tc.want)
		}
	}
}

// --- the words ------------------------------------------------------------------------------

// TestLifetime_NeverOverstates: the phrase floors, so it never promises more time
// than it was given, and a lifetime under a minute is refused.
func TestLifetime_NeverOverstates(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{7 * 24 * time.Hour, "7 days"},
		{7*24*time.Hour - time.Second, "6 days"},
		{30 * 24 * time.Hour, "30 days"},
		{48 * time.Hour, "2 days"},
		{48*time.Hour - time.Nanosecond, "1 day"},
		{24 * time.Hour, "1 day"},
		{24*time.Hour - time.Second, "23 hours"},
		{2 * time.Hour, "2 hours"},
		{time.Hour, "1 hour"},
		{time.Hour - time.Second, "59 minutes"},
		{2 * time.Minute, "2 minutes"},
		{time.Minute, "1 minute"},
	} {
		got, err := lifetime(tc.d)
		if err != nil || got != tc.want {
			t.Errorf("lifetime(%v) = %q, %v; want %q", tc.d, got, err, tc.want)
		}
	}
	for _, d := range []time.Duration{time.Minute - time.Nanosecond, 0, -time.Hour} {
		if _, err := lifetime(d); !errors.Is(err, ErrLifetime) {
			t.Errorf("lifetime(%v) = %v, want ErrLifetime", d, err)
		}
	}
	ctx := context.Background()
	v := randomValue(t)
	if _, err := RenderInvitation(ctx, testBase+"/activate?code="+v, InvitationView{BaseURL: testBase, ValidFor: 0}); !errors.Is(err, ErrLifetime) {
		t.Errorf("a zero lifetime rendered an invitation: %v", err)
	}
	if _, err := RenderPasswordReset(ctx, testBase+"/admin/reset/new?t="+v, ResetView{BaseURL: testBase, ValidFor: -time.Hour}); !errors.Is(err, ErrLifetime) {
		t.Errorf("a negative lifetime rendered a reset: %v", err)
	}
	r := renderBoth(t, "Maria", "Kebab Factory")
	for i, want := range []string{"stays valid for 7 days", "stays valid for 1 hour"} {
		if !strings.Contains(r[i].msg.Text, want) || !strings.Contains(visibleText(t, r[i].msg.HTML), want) {
			t.Errorf("%s: a part does not say %q", r[i].kind, want)
		}
	}
}

// TestRender_TheTwoPartsSayTheSameWords: every sentence of the letter is in the
// text part verbatim and in the HTML escaped — one source, two renderings.
func TestRender_TheTwoPartsSayTheSameWords(t *testing.T) {
	letters := map[string]letter{
		"invitation": invitationLetter("Maria Borg", "Kebab Factory Ltd.", "7 days"),
		"reset":      resetLetter("1 hour"),
	}
	r := renderBoth(t, "Maria Borg", "Kebab Factory Ltd.")
	for i, kind := range []string{"invitation", "reset"} {
		l := letters[kind]
		all := append([]string{l.heading, l.action, l.closing, footer}, l.before...)
		for _, s := range append(all, l.after...) {
			if !strings.Contains(r[i].msg.Text, s) {
				t.Errorf("%s: the text part lacks %q", kind, s)
			}
			if !strings.Contains(r[i].msg.HTML, html.EscapeString(s)) {
				t.Errorf("%s: the HTML lacks %q", kind, s)
			}
		}
		if !strings.Contains(r[i].msg.HTML, "<title>"+html.EscapeString(l.subject)+"</title>") {
			t.Errorf("%s: the HTML title is not the subject", kind)
		}
	}
}

// TestInvitation_SaysToUseThePhonesMainBrowser — §8: the invitation says to open
// the link in the phone's MAIN browser, named by what it does (the one a tapped
// link opens: the DEFAULT browser, which is the trap measured in state.md's
// end-to-end paragraph), and to leave an e-mail app's built-in view. It must not
// say "own browser", which on an iPhone whose default is not Safari can be read
// as Safari.
func TestInvitation_SaysToUseThePhonesMainBrowser(t *testing.T) {
	r := renderBoth(t, "Maria", "Kebab Factory")[0]
	visible := visibleText(t, r.msg.HTML)
	for _, want := range []string{"in your phone's main browser", "the one that opens when you tap a link",
		`choose "Open in browser" first`} {
		if !strings.Contains(r.msg.Text, want) || !strings.Contains(visible, want) {
			t.Errorf("a part does not say %q", want)
		}
	}
	for _, part := range []string{r.msg.Text, visible} {
		if strings.Contains(part, "own web browser") || strings.Contains(part, "own browser") {
			t.Error("a part still says \"own browser\"")
		}
	}
}

// TestRender_DisplayFaceOnWordmarkHeadingAndButton — skill tappa-brand: Space
// Grotesk is the display face (title, button, brand). It is named first, as a
// local font only (no @font-face — TestRender_LoadsNothingAndUsesOnlyTheseElements),
// on the wordmark, the heading and the button; the body text keeps the system
// stack.
func TestRender_DisplayFaceOnWordmarkHeadingAndButton(t *testing.T) {
	for _, r := range renderBoth(t, "Maria", "Kebab Factory") {
		got := map[string]bool{}
		walkHTML(t, r.msg.HTML, func(stack []element, s string) {
			e := stack[len(stack)-1]
			display := strings.HasPrefix(styleOf(e)["font-family"], "'Space Grotesk',")
			switch {
			case e.name == "a", e.name == "h1", strings.TrimSpace(s) == "taptime":
				got[e.name+":"+strings.TrimSpace(s)] = display
			}
		})
		if len(got) != 3 {
			t.Fatalf("%s: found %d of the three display-face slots", r.kind, len(got))
		}
		for slot, display := range got {
			if !display {
				t.Errorf("%s: %s is not set in Space Grotesk first", r.kind, strings.SplitN(slot, ":", 2)[0])
			}
		}
	}
}

// TestRender_SaysTaptimeNotTheCodeName: the user-facing brand is Taptime; the
// internal code name never reaches a recipient.
func TestRender_SaysTaptimeNotTheCodeName(t *testing.T) {
	for _, r := range renderBoth(t, "Maria", "Kebab Factory") {
		for _, part := range []string{r.msg.Subject, r.msg.Text, r.msg.HTML} {
			if strings.Contains(strings.ToLower(part), "tappa") {
				t.Errorf("%s: a part shows the internal code name", r.kind)
			}
			if !strings.Contains(part, "Taptime") {
				t.Errorf("%s: a part does not name Taptime", r.kind)
			}
		}
		if !strings.Contains(r.msg.HTML, ">taptime</p>") || !strings.HasPrefix(r.msg.Text, "taptime\n") {
			t.Errorf("%s: the text wordmark is missing", r.kind)
		}
	}
}
