package email

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// letter is one e-mail's words in the order both parts show them. The HTML
// component and the text writer read the SAME strings, so the two parts cannot
// drift apart. Names are already through nameShown when they get here.
type letter struct {
	subject string   // the Subject header, and the HTML <title>
	heading string   // the first line of the body
	before  []string // paragraphs above the action
	action  string   // the button's label; the text part prints it above the link
	after   []string // paragraphs below the action
	closing string   // the last, quieter line
}

// footer closes every message, in both parts. It names the brand and no address:
// a domain written here would be a second linkifiable run in the body.
const footer = "Taptime — punchless time & attendance"

// Neutral words for a withheld name. They read as a sentence on their own, so a
// withheld name leaves no gap.
const (
	greetingWithoutName = "Hello,"
	inviterWithoutName  = "Your employer"
)

// invitationLetter is the invitation. ADR 0022 §8 asks it to say that the link
// is opened in the phone's MAIN browser: the plaque opens the DEFAULT browser,
// and a phone activated in another one (an e-mail app's built-in view included)
// is not recognised at the plaque (state.md, the end-to-end pilot paragraph —
// whether iOS mail apps' built-in views share cookies with Safari is EM-8's
// measurement, so the copy tells the reader what to do rather than why). The
// words name the default by what it does ("the one that opens when you tap a
// link") rather than by a brand or by "own": on an iPhone whose default is not
// Safari, "the phone's own browser" can be read as Safari.
func invitationLetter(employeeName, tenantName, life string) letter {
	greeting := greetingWithoutName
	if n, ok := nameShown(employeeName); ok {
		greeting = "Hello " + n + ","
	}
	inviter := inviterWithoutName
	if n, ok := nameShown(tenantName); ok {
		inviter = n
	}
	return letter{
		subject: subjectInvitation,
		heading: "You're invited to Taptime",
		before: []string{
			greeting,
			inviter + " has invited you to Taptime, so you can clock in and out by touching " +
				"your phone to the plaque at work. There is no app to install.",
		},
		action: "Activate your phone",
		after: []string{
			"Open the link on the phone you will clock in with, in your phone's main browser — " +
				"the one that opens when you tap a link. " +
				"If your email app opens it inside itself, choose \"Open in browser\" first: Taptime " +
				"remembers your phone in the browser you activate it in.",
			"The link is yours alone and works once. It stays valid for " + life +
				" — after that, ask your manager for a new one.",
		},
		closing: "If this email was not meant for you, you can ignore it.",
	}
}

// resetLetter is the password reset. It names nobody (ResetView says why) and
// repeats what the reset page itself promises: nothing changes until the link is
// used, and using it signs every device out.
func resetLetter(life string) letter {
	return letter{
		subject: subjectReset,
		heading: "Reset your password",
		before: []string{
			"Someone asked to reset the password of a Taptime administrator account that uses " +
				"this email address. If it was you, use the link below.",
		},
		action: "Set a new password",
		after: []string{
			"The link works once and stays valid for " + life + ". Asking again replaces it.",
			"Nothing has changed yet: your current password keeps working until you use the " +
				"link. Setting a new one signs you out of Taptime on every device.",
		},
		closing: "If you did not ask for this, you can ignore this email — your password stays as it is.",
	}
}

// nameMarks is the punctuation a shown name may carry besides letters, marks,
// digits, spaces and a sentence-ending ".".
const nameMarks = "&'’-–—,()!"

// maxShownNameRunes matches the longest name the product stores: signup's
// MaxCompanyNameRunes and staff's MaxEmployeeNameRunes are both 120
// (TestNames_TheGateTakesEveryStoredLength holds the two together).
const maxShownNameRunes = 120

// nameShown decides whether a tenant or employee name may appear in an e-mail.
//
// 🔴 THE PROBLEM IT ANSWERS (ADR 0022 §8, counted limit 22): signup is open and
// unverified, so a tenant name — and an employee name its manager types — is
// attacker-chosen text. Escaping stops it being MARKUP; it does not stop it being
// read, and mail clients commonly turn an address-shaped run of plain text into a
// link (common behaviour, not measured here). The e-mail is DKIM-signed for
// Taptime's domain, so such a name would be a clickable, authenticated phishing
// link that no template literal wrote.
//
// THE RULE IS AN ALLOWLIST, NOT A LIST OF BAD SHAPES. A name is shown only when
// every rune is a letter, a combining mark, a digit, a space (U+0020) or one of
// nameMarks, at least one is a letter, and a "." ends the name or is followed by
// ' ', ',' or ')' ("Ltd.", "Co., Ltd." and "(Malta Ltd.)" pass; "evil.example"
// does not). Each address shape the tests' detector counts needs a character
// outside that set — ":" for a scheme, "/" for a path or "//", "@" for an
// address, a dot BETWEEN two labels for a domain or an IP literal. The other
// dots are not on the list at all: every code point other than "." whose NFKC
// form holds "." or U+3002 (34 in Unicode 16.0, measured with Python's
// unicodedata; Python's IDNA 2003 codec turns "evil" + c + "example" into
// "evil.example" for c in U+3002, U+FF0E, U+FF61, U+FE52 and U+2024) is
// punctuation, a symbol or an "other number" — never a letter, a mark or a
// decimal digit (the test re-measures the 34 against the running Go's tables).
// Counting bad shapes instead is the approach this repository has watched fail
// one spelling at a time (CLAUDE.md §5's address-range history).
//
// WHAT IT DOES NOT STOP, measured (TestNames_KnownLimitIsALookalikeDotAndPlainProse):
// a character that LOOKS like a dot but is a letter, a mark or a digit — U+A4F8
// (Lm), U+0323 (Mn), U+0660 and U+06F0 (Nd) — is an ordinary name character, so
// "evil<it>example" is shown: it reads like an address to a person, and the
// detector does not join labels with it. Digits (a phone number, full-width
// digits included) and prose are shown too. Look-alike dots that are PUNCTUATION
// (U+00B7, U+30FB, U+2027) are withheld, because punctuation is not on the list.
//
// A NAME THAT FAILS IS WITHHELD, NOT CLEANED. Rewriting it would put a string in
// the e-mail that its owner never typed; dropping it leaves a correct e-mail with
// neutral words. Ordinary names pass (TestNames_AnOrdinaryNameIsShownVerbatim
// reads every name in the seed), and an unusual one costs only its own
// appearance. The same rule also keeps control characters, line breaks, U+2028
// and bidi overrides out of the text part, which has no escaping at all.
func nameShown(s string) (string, bool) {
	if s == "" || !utf8.ValidString(s) || utf8.RuneCountInString(s) > maxShownNameRunes {
		return "", false
	}
	letters := 0
	for i, r := range s {
		switch {
		case unicode.IsLetter(r):
			letters++
		case unicode.IsMark(r), unicode.IsDigit(r), r == ' ', strings.ContainsRune(nameMarks, r):
		case r == '.':
			// A dot may end the name or be followed by ' ', ',' or ')': none of the
			// three can continue a label, so "Co., Ltd." and "(Malta Ltd.)" pass and
			// "evil.example" does not.
			if next := i + 1; next < len(s) && s[next] != ' ' && s[next] != ',' && s[next] != ')' {
				return "", false
			}
		default:
			return "", false
		}
	}
	if letters == 0 {
		return "", false
	}
	return s, true
}
