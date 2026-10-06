// Package email renders Taptime's transactional e-mails (M10 EM-4; normative
// source ADR 0022 §8): the invitation and the password reset. Each renderer
// returns a mail.Message with Subject, Text and HTML filled; To and Ref stay
// empty and are the caller's: internal/handler's emailResetChannel for the reset
// (EM-5), EM-7 for the invitation (RenderInvitation has no caller yet).
//
// ONE STRUCTURE, TWO MESSAGES. Both e-mails are a letter (letter.go): a heading,
// paragraphs, one action and a closing line, rendered by ONE templ component
// (message.templ) and by ONE plain-text writer (text.go). So the properties below
// hold for every message by construction of the shared code, and the HTML and the
// text part cannot say different things — they print the same strings. A third
// message (EM-9's "your password was changed") is a third letter value plus its
// own link check; it adds no markup.
//
// WHY THE LINK IS A PARAMETER AND NOT A FIELD. The link carries the activation
// code or the reset token (§4.7 material). Taking it as an argument, rather than
// inside a view struct, means this package adds no new carrier that could be
// printed: the link lives in the caller's variable and then in the returned
// mail.Message, whose own printing paths are redacted (internal/mail). Every
// VALIDATION error below (ErrBaseURL, ErrLink, ErrLifetime) is a sentinel that
// quotes no value; the one other error, from render, wraps templ's rendering
// error, which comes from the writer (a strings.Builder) and carries no text of
// the message.
//
// THE CLAIM, IN THREE PARTS AND ONLY THREE (agent-brief, M10 OP-6/OP-7).
//
// PART I — TODAY'S CODE, MEASURED (email_test.go):
//   - each part carries exactly ONE absolute URL and it is the link: in the HTML
//     the only scheme-or-"//" occurrence is the one href, whose value is the link;
//     in the text part the link stands once on its own line. Both renderers, a
//     plain and a hostile name set — TestRender_EachPartCarriesExactlyOneAbsoluteURL;
//   - the link is the expected address: BaseURL (trailing slashes trimmed, as
//     internal/invite and the reset handler trim it) + the path + "?<param>=" +
//     1..128 of [A-Za-z0-9_-]. The table refuses, with ErrLink or ErrBaseURL and
//     an empty Message: another host, path or parameter, a second parameter
//     ("&next=x"), a fragment, a quote, a space, an escape, an empty or long
//     value; a base that is relative, of another scheme, with user info, a query,
//     a fragment, an escape, a "//" path, a quote, a space, an apostrophe or an
//     ampersand in its path, no host name (":443", "-"), a label that starts or
//     ends with '-', an empty label, a 64-byte label, a 254-byte host (63- and
//     253-byte controls pass), an empty or out-of-range port, an IPv6 literal, or
//     plain http on a host that is not loopback — a public name or address, a
//     private 10/8 or 192.168/16 address, a name that merely starts with "127."
//     or "127.0.0.1.", or ends in "localhost" without the dot
//     (http://localhost:8080, 127.0.0.1 and *.localhost pass); the host rule is
//     syntax only, so "999.999.999.999" and "1.2.3" pass (known-limit rows) —
//     TestRender_RefusesALinkOutsideTheExpectedAddress; the reset link
//     internal/adminauth mints is accepted — TestRender_AcceptsTheResetLinkAdminauthMints;
//   - nothing remote: the HTML holds no <img, <link, <script, <style, @font-face,
//     @import, url( and no src/srcset/background attribute, its elements are
//     exactly html, head, meta, title, body, div, p, h1 and a, and its attributes
//     only lang, charset, name, content, style and href —
//     TestRender_LoadsNothingAndUsesOnlyTheseElements;
//   - names: a tenant or employee name is shown only when every rune is a
//     letter, a mark, a digit, a space or one of & ' ’ - – — , ( ) !, it has a
//     letter, and each "." ends it or is followed by ' ', ',' or ')' (nameShown).
//     Counted over all printable ASCII: between two letters exactly the 62
//     letters and digits and & ' - , ( ) ! are shown —
//     TestNames_EveryASCIICharacterBetweenTwoLetters; after a "." exactly ' ',
//     ')' and ',' (0x20..0x7E) — TestNames_TheDotRule, with its table. Over the
//     LISTED hostile
//     set — URLs, bare and dotted domains, each of the 34 code points whose NFKC
//     form holds a dot, three punctuation look-alike dots (U+00B7, U+30FB,
//     U+2027), an address with "@", an IP literal, "javascript:", markup, a bidi
//     override, a zero-width space, CR, LF, U+2028/U+2029, NEL, TAB, a name with
//     no letter — the name is withheld, the neutral words stand in, and the
//     visible HTML text holds no linkifiable run while the text part holds none
//     outside the link — TestNames_AnAddressShapedNameIsWithheld (the detector's
//     own control: TestLinkifiable_CatchesWhatItExistsToCatch); a name with a
//     line break never adds a line to the text part —
//     TestNames_ALineBreakNeverReachesTheTextPart; ordinary names — Maltese
//     ċ ġ ħ ż, "Ltd.", "&", apostrophes, and every tenant and employee name in
//     test/fixtures/seed.sql (read one per row, the row count checked by a second
//     anchor: 2 and 36 today) — are shown, verbatim in the text part and escaped
//     in the HTML — TestNames_AnOrdinaryNameIsShownVerbatim;
//   - escaping: whatever text reaches the template is HTML-escaped (<script>,
//     quotes, &), measured with the gate bypassed — TestTemplate_EscapesWhateverReachesIt;
//   - the subject: a constant, printable ASCII, no "=?", unchanged by
//     mime.QEncoding (S9) while a Maltese string is encoded to "=?utf-8?q?", and
//     the whole Message passes internal/mail's composer (Send answers timeout,
//     not invalid_message, before any connection) —
//     TestSubject_IsFixedASCIIAndPassesTheMailComposer;
//   - colour: every text node of the HTML sits on an explicit palette ground in
//     an explicit palette colour and every pair clears WCAG AA 4.5:1; every
//     "#"-run of hex digits is six digits and a palette token; every colour-
//     bearing declaration (color, *color*, background*, border*, outline*,
//     text-decoration*, column-rule*, -webkit-text-stroke*, text-emphasis*,
//     shadows, fill, stroke) holds only palette hexes, lengths and line-style
//     keywords; no opacity, filter, backdrop-filter or blend mode is declared —
//     TestContrast_EveryTextOnItsGroundClearsAA (its controls:
//     TestContrast_MathIsNotVacuous, TestContrast_ColourReaderCatchesWhatItExistsToCatch);
//   - the lifetime phrase never overstates the duration it is given —
//     TestLifetime_NeverOverstates; the two parts say the same sentences —
//     TestRender_TheTwoPartsSayTheSameWords; the invitation says to open the link
//     in the phone's main browser, "the one that opens when you tap a link", and
//     never "own browser" — TestInvitation_SaysToUseThePhonesMainBrowser; the
//     user-facing brand only — TestRender_SaysTaptimeNotTheCodeName; Space Grotesk
//     named first (a local font, no @font-face) on the wordmark, the heading and
//     the button — TestRender_DisplayFaceOnWordmarkHeadingAndButton.
//
// PART II — NAMED PINS, AND EXACTLY WHAT EACH CATCHES (each mutation was run;
// the M10 EM-4 card lists them):
//   - TestRender_EachPartCarriesExactlyOneAbsoluteURL: a second link (in the
//     footer, or the link as the button's visible text), a remote image or style,
//     the link dropped from the text part or sharing a line with its label, a
//     greeting that bypasses nameShown;
//   - TestRender_RefusesALinkOutsideTheExpectedAddress: checkLink accepting any
//     link, its byte rule or length bound dropped, '%' or '&' and '=' admitted;
//     validBase admitting another scheme, no host name, any byte, a "//" path,
//     any port, plain http anywhere, or dropping its equality with its own parse;
//     loopbackHost widened to a "127." prefix, to a "localhost" suffix without
//     the dot, or to private addresses; validHostName's 63- or 253-byte bound
//     dropped, or a leading '-' admitted;
//   - TestRender_LoadsNothingAndUsesOnlyTheseElements: any new element (an
//     image, a font link, a script, a style block), the listed strings, any new
//     attribute (bgcolor=, …), the UTF-8 declaration removed;
//   - TestNames_AnAddressShapedNameIsWithheld: nameShown admitting everything,
//     the dot rule dropped, ':', '/', '@', U+3002 or U+00B7 admitted, a name with
//     no letter admitted; TestNames_EveryASCIICharacterBetweenTwoLetters: any
//     one ASCII character added to the list (';', '?', '=', '%', '_', '<', '*'
//     measured); TestNames_TheDotRule: any other ASCII character admitted after a
//     '.', or ',' and ')' refused after it;
//   - TestNames_ALineBreakNeverReachesTheTextPart: a control character or any
//     Unicode space admitted;
//   - TestNames_AnOrdinaryNameIsShownVerbatim: the gate tightened so that a
//     "." inside a name ("Co. Ltd.") is withheld (the cost side), and — through
//     seedNames — a seed row the reader skips;
//   - TestTemplate_EscapesWhateverReachesIt: a templ.Raw in the component;
//   - TestSubject_IsFixedASCIIAndPassesTheMailComposer: a name in the subject, a
//     non-ASCII subject;
//   - TestContrast_EveryTextOnItsGroundClearsAA: a label colour below 4.5:1 on
//     its ground, an off-palette six-digit hex, a named colour ("red") in a
//     border or in the button's own text-decoration, an 8-digit hex, a filter,
//     the page grounds removed so text would sit on the client's own;
//   - TestLifetime_NeverOverstates: rounding up, exactly one hour read as
//     minutes, a lifetime under a minute accepted;
//   - TestRender_TheTwoPartsSayTheSameWords: a sentence dropped from one part;
//     TestInvitation_SaysToUseThePhonesMainBrowser: the main-browser sentence
//     dropped or turned back into "own browser";
//     TestNames_TheGateTakesEveryStoredLength: the bound below a stored length;
//     TestRender_DisplayFaceOnWordmarkHeadingAndButton: the button's stack
//     without Space Grotesk first;
//   - the measured limits: TestNames_KnownLimitIsALookalikeDotAndPlainProse
//     (every dot look-alike that is a letter, a mark or a digit — U+A4F8, U+0323,
//     U+0660, U+06F0 measured — digits, full-width digits and call-back prose are
//     shown) and TestNames_KnownLimitIsAnUnusualNameWithheld (the cost: "J.B.
//     Bar", "Fish/Chips" and the like are withheld) each turn red when their
//     limit stops being true, so the text stating it is updated.
//
// PART III — Any form not listed above is the subject of code review — no
// completeness claim.
//
// KNOWN LIMITS: what e-mail clients autolink is NOT measured — the detector is
// ours and deliberately broad (ADR 0022 counted limit 22); every character that
// looks like a dot but is a letter, a mark or a digit (not a label separator for
// the detector) is shown, so such a name reads like an address to a person; a
// phone number (data detectors are not measured), full-width digits and plain
// attacker prose are shown — whether an invitation may show attacker-chosen
// prose at all is a PRODUCT decision EM-7 must have before it switches e-mail on
// (the card's EM-7 hand-off); a real but unusual name with a character outside
// the allowlist is withheld; the lifetime phrase is the caller's duration
// floored, not the time left when the e-mail is read; how a client's dark mode
// repaints the inline colours is not measured; an element's own colour may be
// dropped where an ancestor sets the same one, since the contrast test reads the
// nearest explicit colour (mutation M27 of the card, green — that is the claim,
// not a gap in it).
package email

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/atknatk/tappa/internal/mail"
)

// The fixed subjects (ADR 0022 §4, §8): printable ASCII, no "=?", no name. mime's
// Q encoder returns them unchanged, so they travel as written.
const (
	subjectInvitation = "Your Taptime invitation"
	subjectReset      = "Reset your Taptime password"
)

// Where each link must point, relative to the configured BaseURL. They are the
// routes the producers build today: internal/invite's activationURL writes
// "/activate?" + url.Values{"code": …}, and the reset handler hands
// internal/adminauth's Link the base "/admin/reset/new", which appends "?t=".
const (
	activatePath  = "/activate"
	activateParam = "code"
	resetPath     = "/admin/reset/new"
	resetParam    = "t"
)

// maxLinkValue bounds the code or token in a link. Both producers write 43
// characters of unpadded base64url (32 random bytes); the bound leaves room for a
// longer value and refuses an absurd one.
const maxLinkValue = 128

var (
	// ErrBaseURL: the configured base is not an absolute http(s) origin with an
	// optional plain path.
	ErrBaseURL = errors.New("email: the base URL is not an absolute http or https origin with a plain path")
	// ErrLink: the link is not the expected address under the base.
	ErrLink = errors.New("email: the link is not the expected address under the base URL")
	// ErrLifetime: the link's lifetime is under a minute.
	ErrLifetime = errors.New("email: the link's lifetime is under a minute")
)

// InvitationView is what the invitation says besides its link. It carries no
// secret.
type InvitationView struct {
	// BaseURL is the deployment's configured origin (cfg.BaseURL). The link must
	// be BaseURL + "/activate?code=" + the code.
	BaseURL string
	// EmployeeName and TenantName appear in the body only, and only when
	// nameShown accepts them; otherwise neutral words stand in.
	EmployeeName string
	TenantName   string
	// ValidFor is the link's lifetime: the invitation's expires_at − created_at.
	ValidFor time.Duration
}

// ResetView is what the reset e-mail says besides its link.
//
// IT NAMES NOBODY, ON PURPOSE. The reset delivery carries no name today
// (ResetDelivery: recipient, link, expiry), and a business name here would put
// signup-chosen text into an e-mail that anyone can trigger for any address
// (ADR 0022 counted limit 22). The cost — one address that belongs to several
// administrator accounts gets one e-mail per account that look alike — was EM-5's
// to weigh, and EM-5 kept the e-mail nameless (internal/handler's
// emailResetChannel); a name added later goes through nameShown.
type ResetView struct {
	// BaseURL is cfg.BaseURL; the link must be BaseURL + "/admin/reset/new?t=" +
	// the token.
	BaseURL string
	// ValidFor is the link's lifetime (adminauth.ResetTTL today).
	ValidFor time.Duration
}

// RenderInvitation renders the invitation for the activation link. The returned
// Message has Subject, Text and HTML; the caller sets To and Ref.
func RenderInvitation(ctx context.Context, link string, v InvitationView) (mail.Message, error) {
	if err := checkLink(v.BaseURL, activatePath, activateParam, link); err != nil {
		return mail.Message{}, err
	}
	life, err := lifetime(v.ValidFor)
	if err != nil {
		return mail.Message{}, err
	}
	return render(ctx, invitationLetter(v.EmployeeName, v.TenantName, life), link)
}

// RenderPasswordReset renders the reset e-mail for the reset link. The returned
// Message has Subject, Text and HTML; the caller sets To and Ref.
func RenderPasswordReset(ctx context.Context, link string, v ResetView) (mail.Message, error) {
	if err := checkLink(v.BaseURL, resetPath, resetParam, link); err != nil {
		return mail.Message{}, err
	}
	life, err := lifetime(v.ValidFor)
	if err != nil {
		return mail.Message{}, err
	}
	return render(ctx, resetLetter(life), link)
}

func render(ctx context.Context, l letter, link string) (mail.Message, error) {
	var html strings.Builder
	if err := messageHTML(l, link).Render(ctx, &html); err != nil {
		// templ's error comes from the writer (a strings.Builder does not fail)
		// and carries no text of the message.
		return mail.Message{}, fmt.Errorf("email: render: %w", err)
	}
	return mail.Message{Subject: l.subject, Text: messageText(l, link), HTML: html.String()}, nil
}

// checkLink holds the link to exactly one shape: base + path + "?" + param + "="
// + 1..maxLinkValue of [A-Za-z0-9_-]. That shape is why each part can carry ONE
// absolute URL at the expected address: the value cannot hold a second scheme, a
// "//", a quote, a space, a "&" or a "#".
func checkLink(baseURL, path, param, link string) error {
	base := strings.TrimRight(baseURL, "/")
	if !validBase(base) {
		return ErrBaseURL
	}
	prefix := base + path + "?" + param + "="
	if !strings.HasPrefix(link, prefix) {
		return ErrLink
	}
	v := link[len(prefix):]
	if v == "" || len(v) > maxLinkValue {
		return ErrLink
	}
	for i := 0; i < len(v); i++ {
		if !tokenByte(v[i]) {
			return ErrLink
		}
	}
	return nil
}

// validBase accepts an absolute https URL — or http on a loopback host — with a
// host name, an optional port and an optional plain path, and nothing else.
//
//   - The byte set [A-Za-z0-9.:/_~-] leaves no room for user info ('@'), a query
//     ('?'), a fragment ('#'), an escape ('%'), a quote, a space or an IPv6
//     literal's brackets.
//   - The host passes a SYNTAX rule (validHostName: labels of [A-Za-z0-9-], 1..63
//     bytes, no edge hyphen, 1..253 bytes in all) and the port, when written, is
//     1..65535 — so ":443" or "-" alone is not a host. An IPv4-SHAPED host is not
//     checked as an address: "999.999.999.999" and "1.2.3" pass the syntax rule
//     (measured, known limit; https only — on http such a host is not loopback).
//   - PLAIN HTTP ONLY ON LOOPBACK (decision, security review of EM-4): on any other
//     host an http link would carry the code or the token in clear text across
//     the network; the development base (http://localhost:8080) stays usable.
//     Loopback is "localhost", a name under ".localhost" and an IPv4 address in
//     127.0.0.0/8 (netip's IsLoopback).
//   - No "//" inside the path: a second "//" would be a second absolute-URL start
//     in the e-mail.
//   - The base must equal its own parse, which refuses the forms url.Parse
//     normalises (an upper-case scheme).
func validBase(base string) bool {
	for i := 0; i < len(base); i++ {
		if !baseByte(base[i]) {
			return false
		}
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		!validHostName(u.Hostname()) || strings.Contains(u.Path, "//") {
		return false
	}
	if strings.Contains(u.Host, ":") {
		if p, err := strconv.Atoi(u.Port()); err != nil || p < 1 || p > 65535 {
			return false
		}
	}
	if u.Scheme == "http" && !loopbackHost(u.Hostname()) {
		return false
	}
	return base == u.Scheme+"://"+u.Host+u.Path
}

// validHostName is a syntax rule: 1..253 bytes of dot-separated labels, each
// 1..63 of [A-Za-z0-9-] that neither starts nor ends with '-'. It does not tell a
// DNS name from an address: an IPv4 literal passes as numeric labels, and so does
// an IPv4-shaped string that is not an address ("999.999.999.999", "1.2.3").
func validHostName(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			b := label[i]
			if !(b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-') {
				return false
			}
		}
	}
	return true
}

// loopbackHost reports whether h names this machine: "localhost", a name under
// ".localhost" (RFC 6761 §6.3), or an IPv4 loopback address.
func loopbackHost(h string) bool {
	h = strings.ToLower(h)
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	a, err := netip.ParseAddr(h)
	return err == nil && a.Is4() && a.IsLoopback()
}

func baseByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' ||
		b == '.' || b == ':' || b == '/' || b == '_' || b == '~' || b == '-'
}

// tokenByte is unpadded base64url's alphabet, [A-Za-z0-9_-].
func tokenByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' ||
		b == '_' || b == '-'
}

// lifetime says how long the link lasts, as a LENGTH (the panel's expiryPhrase
// makes the same choice: no instant, so no time zone). It FLOORS to the largest
// whole unit, so it never promises more time than it was given: 7 days less a
// second reads "6 days". Under a minute is refused — no link the product mints is
// that short, and "0 minutes" would read as a link that is already dead.
func lifetime(d time.Duration) (string, error) {
	const day = 24 * time.Hour
	switch {
	case d >= 2*day:
		return strconv.FormatInt(int64(d/day), 10) + " days", nil
	case d >= day:
		return "1 day", nil
	case d >= 2*time.Hour:
		return strconv.FormatInt(int64(d/time.Hour), 10) + " hours", nil
	case d >= time.Hour:
		return "1 hour", nil
	case d >= 2*time.Minute:
		return strconv.FormatInt(int64(d/time.Minute), 10) + " minutes", nil
	case d >= time.Minute:
		return "1 minute", nil
	}
	return "", ErrLifetime
}
