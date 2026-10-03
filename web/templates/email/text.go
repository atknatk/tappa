package email

import "strings"

// messageText is the text/plain part: the same strings as messageHTML, one
// paragraph per line, the link alone on its own line so it can be copied whole.
//
// PLAIN GO, NOT text/template AND NOT templ (decision). templ escapes for HTML,
// which in a text part would print "&amp;" for a name's "&"; text/template adds a
// second template language for a dozen string joins and escapes nothing either.
// Nothing here CAN escape — a text part has no syntax to escape into — so the one
// control is upstream: nameShown admits no control character, line break, U+2028
// or bidi override, and checkLink admits only [A-Za-z0-9_-] in the link's value.
// Line breaks are "\n"; internal/mail's composer writes them as CRLF.
func messageText(l letter, link string) string {
	var b strings.Builder
	line := func(s string) {
		b.WriteString(s)
		b.WriteString("\n\n")
	}
	line("taptime")
	line(l.heading)
	for _, p := range l.before {
		line(p)
	}
	b.WriteString(l.action + ":\n")
	line(link)
	for _, p := range l.after {
		line(p)
	}
	line(l.closing)
	b.WriteString("-- \n" + footer + "\n")
	return b.String()
}
