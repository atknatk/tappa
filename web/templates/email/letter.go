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
//
// 🔴 NAMES ONLY FOR A VERIFIED BUSINESS (user decision 2026-10-09, ADR 0022 counted
// limit 22, the EM-7B note). nameShown keeps an address-shaped name out of the body,
// but it shows plain attacker prose and digits ("Your account is suspended Call
// 21234567 now!") — measured, TestNames_KnownLimitIsALookalikeDotAndPlainProse. So
// unless VIES confirmed the business's VAT number, NEITHER name is shown, whatever it
// is: the neutral words say "Your employer has invited you to Taptime". When it did,
// nameShown still decides each name on top. VIES proves the number EXISTS, not that
// whoever signed up OWNS it — the bar is higher, the risk is not closed.
func invitationLetter(employeeName, tenantName string, verified bool, life string) letter {
	greeting := greetingWithoutName
	inviter := inviterWithoutName
	if verified {
		if n, ok := nameShown(employeeName); ok {
			greeting = "Hello " + n + ","
		}
		if n, ok := nameShown(tenantName); ok {
			inviter = n
		}
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

// adminInvitationLetter is the manager invitation (M14-15). The business is named only for a VIES-
// verified business through nameShown (invitationLetter's rule); no person is named. It says what the
// link does on both sides of R18 -- a person with a Taptime sign-in signs in and accepts, a new one
// chooses a password -- without knowing which the reader is, and it promises nothing the page does not
// do: accepting gives no session (the reader signs in afterwards).
func adminInvitationLetter(tenantName string, verified bool, life string) letter {
	business := "A business"
	if verified {
		if n, ok := nameShown(tenantName); ok {
			business = n
		}
	}
	return letter{
		subject: subjectAdminInvitation,
		heading: "You're invited to manage a business",
		before: []string{
			greetingWithoutName,
			business + " has invited you to Taptime as a manager, so you can review time records, " +
				"approve flagged check-ins and run reports in its panel.",
		},
		action: "Accept the invitation",
		after: []string{
			"If you already sign in to Taptime with this email address, sign in first and then open the " +
				"link to accept. If you are new to Taptime, open the link and choose a password.",
			"The link is yours alone and works once. It stays valid for " + life +
				" — after that, ask whoever invited you for a new one.",
		},
		closing: "If you did not expect this, you can ignore this email — nothing happens unless the link is used.",
	}
}

// resetLetter is the password reset. It names nobody (ResetView says why) and
// repeats what the reset page itself promises: nothing changes until the link is
// used, and using it signs every device out.
//
// "ASKING AGAIN" SAYS WHAT THE FLOW DOES SINCE M10 EM-7C: a request for an account
// that holds a live link sends nothing (internal/adminauth's IssueForEmail — one live
// link per account), so this e-mail must not promise that asking again replaces it.
// The recovery page says the same (web/templates/pages/adminreset.templ);
// TestReset_AskingAgainSaysWhatTheFlowDoes pins this sentence and internal/handler's
// TestAdminReset_TheSentPageSaysWhatAskingAgainDoes the page's.
//
// everyBusiness (M14-14) is the per-identity link: one password for every business the person signs
// in to with this address, so the opening sentence says that instead -- still naming no business.
func resetLetter(life string, everyBusiness bool) letter {
	opening := "Someone asked to reset the password of a Taptime administrator account that uses " +
		"this email address. If it was you, use the link below."
	if everyBusiness {
		opening = resetEveryBusiness
	}
	return letter{
		subject: subjectReset,
		heading: "Reset your password",
		before:  []string{opening},
		action:  "Set a new password",
		after: []string{
			"The link works once and stays valid for " + life + ". " + resetAskAgain,
			"Nothing has changed yet: your current password keeps working until you use the " +
				"link. Setting a new one signs you out of Taptime on every device.",
		},
		closing: "If you did not ask for this, you can ignore this email — your password stays as it is.",
	}
}

// resetAskAgain is the reset e-mail's sentence about asking again (M10 EM-7C). True
// in both of the cases the recovery page's sentence once got wrong: a link that timed
// out on its way and was withdrawn no longer works, so asking again sends a new one;
// a link that arrived late still works only as long as the e-mail itself states.
const resetAskAgain = "Asking again sends no new link while this one still works."

// resetEveryBusiness opens the per-identity reset e-mail (M14-14): the one link sets the password of the
// sign-in -- every business LINKED to it, which is what identity_consume_reset writes. Not "every
// business you manage": an account of the same address that is not linked keeps its own password and
// sessions and gets its own, per-account e-mail (M14-14's third-eye audit).
const resetEveryBusiness = "Someone asked to reset the password of your Taptime sign-in that uses this email " +
	"address. The new password will work for every business this sign-in is linked to. If it was you, use " +
	"the link below."

// changedEveryBusiness opens the per-identity notice (M14-14, EM-9): one notice for the sign-in, true for
// exactly the businesses identity_consume_reset wrote and signed out.
const changedEveryBusiness = "The password of your Taptime sign-in that uses this email address was just " +
	"changed for every business this sign-in is linked to, and every device signed in to those businesses " +
	"was signed out."

// passwordChangedLetter is the "your password was changed" notice (M10 EM-9). It
// takes NO argument, so its words are the same for every account: it names nobody
// (PasswordChangedView says why) and it carries no instant, no device and no
// address. Its one action is the sign-in page, which changes nothing by being opened.
//
// THE SENTENCES ARE TRUE ON BOTH PATHS THAT SEND IT. A completed recovery signs the
// account out everywhere and a change from the Account section signs out every
// session but the one that made it, so "any other device ... was signed out" holds
// for both. The advice names the sign-in page's forgotten-password link by what it
// does rather than by its label, so a relabelled link does not make the e-mail wrong.
//
// everyBusiness (M14-14) is the person's notice after a per-identity reset: the same e-mail but for its
// opening, which says the change holds for every business -- the person gets ONE notice, not one per
// business.
func passwordChangedLetter(everyBusiness bool) letter {
	opening := "The password of a Taptime administrator account that uses this email address was just " +
		"changed, and any other device signed in to that account was signed out."
	if everyBusiness {
		opening = changedEveryBusiness
	}
	return letter{
		subject: subjectPasswordChanged,
		heading: "Your password was changed",
		before:  []string{opening},
		action:  "Go to the sign-in page",
		after: []string{
			"If you made this change, there is nothing more to do.",
			"If you did not, someone else may have your password. Open the sign-in page and use its " +
				"forgotten-password link to set a new one straight away.",
		},
		closing: "This email is only a notice. Its one link opens the sign-in page and changes nothing.",
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
