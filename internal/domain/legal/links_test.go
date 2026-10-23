package legal_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/domain/legal"
)

// TestParagraphs_LinksTheClosedGrammarAndNothingElse pins the link grammar (ADR 0033)
// span by span: what becomes a link, with which href, and what stays text.
func TestParagraphs_LinksTheClosedGrammarAndNothingElse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want []legal.Paragraph
	}{
		{
			// THE USER'S OWN IMPRINT, as they asked to publish it (2026-10-23).
			"the user's imprint",
			"Taptime is operated by Ozcan Duzce\n\nContact:\n\ninfo@taptime.mt +356 9931 0184\n\nhttps://codeveralabs.com",
			[]legal.Paragraph{
				{{Text: "Taptime is operated by Ozcan Duzce"}},
				{{Text: "Contact:"}},
				{
					{Text: "info@taptime.mt", Href: "mailto:info@taptime.mt"},
					{Text: " "},
					{Text: "+356 9931 0184", Href: "tel:+35699310184"},
				},
				{{Text: "https://codeveralabs.com", Href: "https://codeveralabs.com"}},
			},
		},
		{
			"explicit link with its own label",
			"See [our site](https://x.mt/a) today.",
			[]legal.Paragraph{{{Text: "See "}, {Text: "our site", Href: "https://x.mt/a"}, {Text: " today."}}},
		},
		{
			"explicit mailto and tel targets",
			"[write](mailto:a@b.mt) or [call](tel:+35699310184)",
			[]legal.Paragraph{{{Text: "write", Href: "mailto:a@b.mt"}, {Text: " or "}, {Text: "call", Href: "tel:+35699310184"}}},
		},
		{
			"explicit target with balanced parentheses",
			"[Foo](https://en.wikipedia.org/wiki/Foo_(bar))",
			[]legal.Paragraph{{{Text: "Foo", Href: "https://en.wikipedia.org/wiki/Foo_(bar)"}}},
		},
		{
			// 🔴 THE WHOLE CONSTRUCT STAYS LITERAL, label and target, when the target fails
			// the allow-list.
			"explicit javascript target stays literal",
			"[label](javascript:alert(1))",
			[]legal.Paragraph{{{Text: "[label](javascript:alert(1))"}}},
		},
		{
			"explicit data target stays literal",
			"[x](data:text/html,https://evil.example)",
			[]legal.Paragraph{{{Text: "[x](data:text/html,https://evil.example)"}}},
		},
		{
			"explicit protocol-relative target stays literal",
			"[x](//evil.example/a)",
			[]legal.Paragraph{{{Text: "[x](//evil.example/a)"}}},
		},
		{
			"explicit relative target stays literal",
			"[x](/legal/privacy)",
			[]legal.Paragraph{{{Text: "[x](/legal/privacy)"}}},
		},
		{
			"explicit vbscript target, any case, stays literal",
			"[x](VBScript:msgbox(1))",
			[]legal.Paragraph{{{Text: "[x](VBScript:msgbox(1))"}}},
		},
		{
			"bare javascript is not linked",
			"javascript:alert(1)",
			[]legal.Paragraph{{{Text: "javascript:alert(1)"}}},
		},
		{
			"bare data is not linked",
			"data:text/html;base64,PHNjcmlwdD4=",
			[]legal.Paragraph{{{Text: "data:text/html;base64,PHNjcmlwdD4="}}},
		},
		{
			"trailing sentence punctuation is not part of the url",
			"Visit https://x.mt/terms. Or http://y.mt/a?b=1, or https://z.mt!",
			[]legal.Paragraph{{
				{Text: "Visit "}, {Text: "https://x.mt/terms", Href: "https://x.mt/terms"},
				{Text: ". Or "}, {Text: "http://y.mt/a?b=1", Href: "http://y.mt/a?b=1"},
				{Text: ", or "}, {Text: "https://z.mt", Href: "https://z.mt"}, {Text: "!"},
			}},
		},
		{
			"url in square brackets",
			"[see https://x.mt]",
			[]legal.Paragraph{{{Text: "[see "}, {Text: "https://x.mt", Href: "https://x.mt"}, {Text: "]"}}},
		},
		{
			"url with its own brackets keeps them",
			"https://x.mt/a[1]",
			[]legal.Paragraph{{{Text: "https://x.mt/a[1]", Href: "https://x.mt/a[1]"}}},
		},
		{
			// Text pasted from a word processor carries no-break spaces; one ENDS the URL.
			"a no-break space ends a url",
			"https://x.mt/\u00a0b and https://y.mt\u202fc and https://z.mt\u2007d",
			[]legal.Paragraph{{
				{Text: "https://x.mt/", Href: "https://x.mt/"}, {Text: "\u00a0b and "},
				{Text: "https://y.mt", Href: "https://y.mt"}, {Text: "\u202fc and "},
				{Text: "https://z.mt", Href: "https://z.mt"}, {Text: "\u2007d"},
			}},
		},
		{
			"a no-break space ends an email",
			"a@b.mt\u00a0x",
			[]legal.Paragraph{{{Text: "a@b.mt", Href: "mailto:a@b.mt"}, {Text: "\u00a0x"}}},
		},
		{
			"a no-break space inside an explicit target: no explicit link, the bare url before it still links",
			"[x](https://x.mt/\u00a0b)",
			[]legal.Paragraph{{{Text: "[x]("}, {Text: "https://x.mt/", Href: "https://x.mt/"}, {Text: "\u00a0b)"}}},
		},
		{
			"url in parentheses",
			"(see https://x.mt/a)",
			[]legal.Paragraph{{{Text: "(see "}, {Text: "https://x.mt/a", Href: "https://x.mt/a"}, {Text: ")"}}},
		},
		{
			"url with its own parentheses keeps them",
			"https://en.wikipedia.org/wiki/Foo_(bar).",
			[]legal.Paragraph{{{Text: "https://en.wikipedia.org/wiki/Foo_(bar)", Href: "https://en.wikipedia.org/wiki/Foo_(bar)"}, {Text: "."}}},
		},
		{
			"scheme in capitals is still a link",
			"HTTPS://X.MT",
			[]legal.Paragraph{{{Text: "HTTPS://X.MT", Href: "HTTPS://X.MT"}}},
		},
		{
			"a scheme with no host is text",
			"https://. and https:///path",
			[]legal.Paragraph{{{Text: "https://. and https:///path"}}},
		},
		{
			"email in a sentence",
			"Write to privacy@taptime.mt.",
			[]legal.Paragraph{{{Text: "Write to "}, {Text: "privacy@taptime.mt", Href: "mailto:privacy@taptime.mt"}, {Text: "."}}},
		},
		{
			"a handle is not an email",
			"@taptime and a@b",
			[]legal.Paragraph{{{Text: "@taptime and a@b"}}},
		},
		{
			"phone with plus and hyphens",
			"Call +356-9931-0184.",
			[]legal.Paragraph{{{Text: "Call "}, {Text: "+356-9931-0184", Href: "tel:+35699310184"}, {Text: "."}}},
		},
		{
			"phone grouped with no-break spaces",
			"+356\u00a09931\u00a00184",
			[]legal.Paragraph{{{Text: "+356\u00a09931\u00a00184", Href: "tel:+35699310184"}}},
		},
		{
			// phoneSpaceStop: a space ends the number once it has 11 digits, so a year
			// after it is not swallowed into a 15-digit tel:.
			"a number after a complete phone is not absorbed",
			"+356 9931 0184 2024 and +1 415 555 2671 10",
			[]legal.Paragraph{{
				{Text: "+356 9931 0184", Href: "tel:+35699310184"}, {Text: " 2024 and "},
				{Text: "+1 415 555 2671", Href: "tel:+14155552671"}, {Text: " 10"},
			}},
		},
		{
			"longer numbers cross their last space below 11 digits",
			"+44 20 7946 0958, +49 1511 2345678, +86 138 0013 8000",
			[]legal.Paragraph{{
				{Text: "+44 20 7946 0958", Href: "tel:+442079460958"}, {Text: ", "},
				{Text: "+49 1511 2345678", Href: "tel:+4915112345678"}, {Text: ", "},
				{Text: "+86 138 0013 8000", Href: "tel:+8613800138000"},
			}},
		},
		{
			"hyphens do not stop at 11 digits, the 15-digit cap still applies",
			"+356-9931-0184-2024 and +356-9931-0184-20245",
			[]legal.Paragraph{{
				{Text: "+356-9931-0184-2024", Href: "tel:+356993101842024"},
				{Text: " and +356-9931-0184-20245"},
			}},
		},
		{
			"phone without plus is not linked",
			"Call 9931 0184 or 0035699310184.",
			[]legal.Paragraph{{{Text: "Call 9931 0184 or 0035699310184."}}},
		},
		{
			"a date is not a phone",
			"Effective 2026-10-23, company C 12345, VAT MT12345678.",
			[]legal.Paragraph{{{Text: "Effective 2026-10-23, company C 12345, VAT MT12345678."}}},
		},
		{
			"a plus with too few digits is not a phone",
			"+356 99 and 5+12345678",
			[]legal.Paragraph{{{Text: "+356 99 and 5+12345678"}}},
		},
		{
			"two spaces between digit groups break the number",
			"+356  9931 0184",
			[]legal.Paragraph{{{Text: "+356  9931 0184"}}},
		},
		{
			"markup stays text",
			"<script>alert(1)</script>",
			[]legal.Paragraph{{{Text: "<script>alert(1)</script>"}}},
		},
		{
			// Typed markup is not interpreted: the anchor's characters stay text, and the
			// address inside them is just a bare URL like any other (the URL stops at `"`).
			"a typed anchor is text around a bare url",
			"<a href=\"https://x.mt\">x</a>",
			[]legal.Paragraph{{{Text: "<a href=\""}, {Text: "https://x.mt", Href: "https://x.mt"}, {Text: "\">x</a>"}}},
		},
		{
			"an address followed by another label is not cut short",
			"a@b.mt.x and a@b.mt.",
			[]legal.Paragraph{{{Text: "a@b.mt.x and "}, {Text: "a@b.mt", Href: "mailto:a@b.mt"}, {Text: "."}}},
		},
		{
			"an email inside a url is part of the url",
			"https://x.mt/?to=a@b.mt",
			[]legal.Paragraph{{{Text: "https://x.mt/?to=a@b.mt", Href: "https://x.mt/?to=a@b.mt"}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := legal.Paragraphs(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Paragraphs(%q)\n got  %#v\n want %#v", tc.in, got, tc.want)
			}
		})
	}
}

// TestParagraphs_LinkingNeverLosesACharacter: whatever the grammar does, the visible
// text of every paragraph is the operator's text (an explicit link shows its label
// instead of its target, and that is the only difference).
func TestParagraphs_LinkingNeverLosesACharacter(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"info@taptime.mt +356 9931 0184 https://codeveralabs.com.",
		"(see https://x.mt/a), [bad](javascript:x) [also bad](ftp://x.mt)",
		"+356 9931 0184 2026-10-23 a@b.c x@y.mt",
	} {
		got := legal.Paragraphs(in)
		if len(got) != 1 || got[0].Text() != in {
			t.Errorf("Paragraphs(%q) shows %q", in, got)
		}
	}
}

// TestSafeHref_AllowsOnlyTheFourSchemes is the allow-list every href passes. templ.URL
// is a second fence, but it is not ours and it admits ftp; this one is the contract.
func TestSafeHref_AllowsOnlyTheFourSchemes(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{
		"https://x.mt", "http://x.mt/a?b=c#d", "HTTPS://X.MT", "mailto:a@b.mt", "MailTo:a@b.mt", "tel:+35699310184",
	} {
		if got, allowed := legal.SafeHref(ok); !allowed || got != ok {
			t.Errorf("SafeHref(%q) = %q, %v; want it allowed unchanged", ok, got, allowed)
		}
	}
	for _, no := range []string{
		"", "javascript:alert(1)", "JavaScript:alert(1)", "java\tscript:alert(1)", " javascript:alert(1)",
		"data:text/html,x", "vbscript:x", "file:///etc/passwd", "ftp://x.mt", "//evil.example",
		"/legal/privacy", "legal/privacy", "#top", "https:", "https://", "https:x.mt", "http:/x.mt",
		"https:\\\\evil.example", "https://x.mt/a b", "https://x.mt/\x00", "https://x.mt/ ",
		"mailto:", "tel:", "x.mt",
	} {
		if got, allowed := legal.SafeHref(no); allowed {
			t.Errorf("SafeHref(%q) = %q, true; it must not become an href", no, got)
		}
	}
}

// TestParagraphs_EveryHrefPassesTheAllowList is the property behind the cases: no
// input produces an href SafeHref would refuse.
func TestParagraphs_EveryHrefPassesTheAllowList(t *testing.T) {
	t.Parallel()
	in := strings.Join([]string{
		"[a](javascript:x) [b](https://ok.mt) https://ok.mt/x) x@y.mt +356 9931 0184",
		"[c](JAVASCRIPT:x) [d](data:x) [e](tel:+1) [f](mailto:) https:// http://a",
	}, "\n\n")
	links := 0
	for _, p := range legal.Paragraphs(in) {
		for _, s := range p {
			if s.Href == "" {
				continue
			}
			links++
			if _, ok := legal.SafeHref(s.Href); !ok {
				t.Errorf("span %q carries href %q, which the allow-list refuses", s.Text, s.Href)
			}
			if strings.HasPrefix(strings.ToLower(s.Href), "javascript:") || strings.HasPrefix(strings.ToLower(s.Href), "data:") {
				t.Errorf("span %q carries href %q", s.Text, s.Href)
			}
		}
	}
	// ANTI-VACUITY: the input has links that must survive.
	if links < 5 {
		t.Errorf("found %d link(s); the input carries at least five allowed ones", links)
	}
}
