package mail

import (
	"bytes"
	"crypto/rand"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// maxAddressLen is RFC 5321 §4.5.3.1.3's 256-octet path minus its two angle
	// brackets.
	maxAddressLen = 254
	// maxHeaderLine is RFC 5322 §2.1.1's hard line limit, CRLF excluded. The
	// composer never folds, so one header is one line and must fit.
	maxHeaderLine = 998
	// maxRefLen bounds Message.Ref.
	maxRefLen = 128
)

// validRecipient is the recipient rule of the design (ADR 0022): exactly one bare
// ASCII address of at most 254 bytes, which net/mail parses back to ITSELF.
//
// "Back to itself" is the whole shape gate, and it implies an empty display name:
// a display name, angle brackets, a comment, surrounding whitespace, a quoted
// local part (net/mail unquotes it) or a second address all make Address differ
// from the input or fail the parse. The ASCII check is separate and load-bearing:
// net/mail accepts UTF-8 local parts (RFC 6532), and such an address equals itself.
// "=?" is refused for validSubject's reason, measured on the To header: an
// address shaped like an encoded word parses back to itself, goes out raw, and
// mime.WordDecoder decodes it to a CR LF and a fake Bcc line.
func validRecipient(to string) bool {
	if to == "" || len(to) > maxAddressLen || !printableASCII(to, false) || strings.Contains(to, "=?") {
		return false
	}
	a, err := mail.ParseAddress(to)
	return err == nil && a.Address == to
}

// printableASCII reports whether every byte of s is 0x21..0x7e, or 0x20..0x7e when
// space is allowed. Everything else — CR, LF, TAB, NUL, DEL, any byte >= 0x80 — is
// refused, never stripped.
func printableASCII(s string, space bool) bool {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b == ' ' && space {
			continue
		}
		if b < 0x21 || b > 0x7e {
			return false
		}
	}
	return true
}

// validHeaderText is the rule for a free-text header value before it is encoded
// (the Subject): non-empty valid UTF-8 with no control character (C0 including TAB,
// CR and LF; DEL; C1) and no Unicode line or paragraph separator.
//
// DECISION, U+2028/U+2029: after RFC 2047 encoding they are inert bytes (=E2=80=A8)
// and cannot end a header line, so this is not the injection gate — the gate is
// that CR and LF are refused rather than encoded (mime's Q encoder WOULD encode
// them, which is exactly "cleaning"). They are refused because a subject never
// legitimately carries one and some clients render them as a line break.
func validHeaderText(s string) bool {
	if s == "" || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

// validSubject is validHeaderText plus one rule: no "=?" anywhere. mime's Q
// encoder leaves printable ASCII untouched, so an ASCII subject SHAPED like an
// RFC 2047 encoded word would go out raw, and mime.WordDecoder decodes it
// (measured) — a subject written as =?utf-8?q?a=0D=0ABcc:_b?= decodes to a CR LF
// and a fake Bcc line (FuzzCompose found this: its oracle requires the Subject
// header to decode to the input). Refused, like CR and LF themselves, rather than
// forced into an encoded word. The same rule holds for the From display name
// (parseSender) and for a recipient (validRecipient).
func validSubject(s string) bool {
	return validHeaderText(s) && !strings.Contains(s, "=?")
}

// validRef: Ref is optional; when set it is 1..128 of [A-Za-z0-9._:-]. The set has
// no '@', '/', '?', '=' or space, so a Ref cannot be an address or a URL — the
// caller logs it next to message_id (ADR 0022 §10's invite_id/reset_id), and a
// Ref that could carry an address or a link would carry it into that log line.
func validRef(s string) bool {
	if len(s) > maxRefLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !idByte(s[i]) && s[i] != ':' {
			return false
		}
	}
	return true
}

// idByte is [A-Za-z0-9._-].
func idByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' ||
		b == '.' || b == '_' || b == '-'
}

// headerBlock writes header lines and REFUSES a value it cannot write verbatim:
// every byte must be 0x20..0x7e and the line must fit maxHeaderLine. This is the
// second, final gate (the first is validRecipient/validHeaderText on the inputs):
// whatever reaches it already encoded must still be one printable line, or the
// whole message is refused. It never strips, folds or re-encodes.
type headerBlock struct {
	buf    bytes.Buffer
	broken bool
}

func (h *headerBlock) add(name, value string) {
	if !printableASCII(value, true) || len(name)+2+len(value) > maxHeaderLine {
		h.broken = true
		return
	}
	h.buf.WriteString(name + ": " + value + "\r\n")
}

// compose validates m and builds the wire message: headers, then a
// multipart/alternative body with a text/plain and a text/html part, both UTF-8
// and quoted-printable. Every refusal happens here, before any dial.
//
// QUOTED-PRINTABLE, NOT 8BIT OR BASE64 (decision): the HTML part is written with
// inline styles and can carry lines far past RFC 5321's 1000-octet line limit;
// quoted-printable (mime/quotedprintable) soft-wraps at 76 characters and makes
// the whole body 7-bit ASCII — printable characters, with SPACE and TAB kept
// literal inside a line (FuzzCompose found the TAB) — so nothing depends on the relay's
// 8BITMIME/SMTPUTF8 support and no body byte can be a bare CR or LF on the wire
// (line breaks are canonicalised to LF first and written as CRLF — alternative
// says why the encoder alone is not trusted with that). Base64 would give the same
// safety but turns the plain-text part into an opaque blob in a raw view and costs
// a third more. CONSEQUENCE for tests on the wire: '=' is written as "=3D" and a
// long link is soft-broken with "=\r\n", so a test that looks for a link must
// decode the part first.
func (s *SMTP) compose(m Message, now time.Time) ([]byte, error) {
	if !validRecipient(m.To) {
		return nil, &SendError{Class: ClassInvalidAddress}
	}
	if !validSubject(m.Subject) || !validRef(m.Ref) ||
		m.Text == "" || !utf8.ValidString(m.Text) || m.HTML == "" || !utf8.ValidString(m.HTML) {
		return nil, &SendError{Class: ClassInvalidMessage}
	}
	body, boundary, err := alternative(m.Text, m.HTML)
	if err != nil {
		return nil, &SendError{Class: ClassInvalidMessage}
	}

	var h headerBlock
	h.add("Date", now.UTC().Format(time.RFC1123Z))
	h.add("From", s.fromHeader)
	if s.replyToHeader != "" {
		h.add("Reply-To", s.replyToHeader)
	}
	h.add("To", m.To)
	// mime's Q encoder returns printable ASCII unchanged and encodes anything else
	// as =?utf-8?q?...?= words; validHeaderText has already refused what it would
	// otherwise have encoded instead of refusing (CR, LF, controls).
	h.add("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	h.add("Message-ID", "<"+rand.Text()+"@"+s.fromDomain+">")
	h.add("MIME-Version", "1.0")
	h.add("Content-Type", "multipart/alternative; boundary="+boundary)
	if h.broken {
		return nil, &SendError{Class: ClassInvalidMessage}
	}
	h.buf.WriteString("\r\n")
	h.buf.Write(body)
	return h.buf.Bytes(), nil
}

// alternative is the multipart/alternative body. multipart.NewWriter draws its
// boundary from crypto/rand (60 hex characters), so message text cannot predict
// and close it.
//
// Line breaks are canonicalised to LF BEFORE the quoted-printable writer sees
// them (CRLF, CR and LF each become one CRLF on the wire). Measured reason, found
// by FuzzCompose: mime/quotedprintable's writer clears its "previous byte was CR"
// flag only on a byte it copies literally, not on one it encodes, so "\r\x1d\n"
// went out as one line break instead of two (its write method, Go 1.27).
func alternative(text, html string) ([]byte, string, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, p := range [2][2]string{
		{"text/plain; charset=utf-8", text},
		{"text/html; charset=utf-8", html},
	} {
		w, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {p[0]},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err == nil {
			qp := quotedprintable.NewWriter(w)
			lf := strings.ReplaceAll(strings.ReplaceAll(p[1], "\r\n", "\n"), "\r", "\n")
			if _, err = qp.Write([]byte(lf)); err == nil {
				err = qp.Close()
			}
		}
		if err != nil {
			return nil, "", err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return body.Bytes(), mw.Boundary(), nil
}

// messageID reads the provider's message id out of the reply to the end of DATA:
// the LAST field of the reply's last line, after an optional RFC 3463 enhanced
// status code, when at least one other field precedes it ("Ok <id>" from SES,
// "Ok: queued as <id>" from Postfix and Mailpit). A reply with nothing after its
// status word ("250 Ok", "250 2.0.0 Ok") yields "".
//
// The id is the one server-chosen string this package hands back for logging, so
// it is held to a shape and an echo rule, and dropped ("") if it fails either:
//   - shape: 1..128 of [A-Za-z0-9._-] — no '@' (not an address), no '/', '?',
//     ':', '=' or space (not a URL, not a query, not reply prose);
//   - echo, in BOTH directions (ADR 0022 §2's window rule): the id is dropped if
//     it shares any echoWindow-character run with a value of Send's echo list,
//     or (for an id shorter than the window) occurs inside one. "Is the id inside
//     a sent value" alone missed an id that EMBEDS a secret — measured:
//     q.<link value>, <link value>-0, x<password> and maria.borg-1 all passed it.
//
// What this is measured to drop, by form: TestSend_ReadsTheMessageIDFromThe250Reply
// lists them (a link value with a prefix or a suffix, the password with a prefix,
// the AUTH argument verbatim and a run of it, the username as a prefix, a
// recipient local part with a suffix, a fragment present only on the wire, a
// fragment split by a quoted-printable soft break, a subject fragment changed by
// RFC 2047, the Ref) and the realistic SES id it keeps.
func messageID(reply string, sent []string) string {
	lines := strings.Split(reply, "\n")
	f := strings.Fields(lines[len(lines)-1])
	if len(f) > 0 && enhancedStatus(f[0]) {
		f = f[1:]
	}
	if len(f) < 2 {
		return ""
	}
	id := f[len(f)-1]
	if len(id) > 128 {
		return ""
	}
	for i := 0; i < len(id); i++ {
		if !idByte(id[i]) {
			return ""
		}
	}
	for _, s := range sent {
		if strings.Contains(s, id) {
			return ""
		}
		for i := 0; i+echoWindow <= len(id); i++ {
			if strings.Contains(s, id[i:i+echoWindow]) {
				return ""
			}
		}
	}
	return id
}

// echoWindow is the shared-run length that makes an id an echo. Eight is short
// enough that any CONTIGUOUS run of eight or more characters of a 43-character
// link value, a 44-character SES SMTP password or a recipient local part is
// caught (an echo cut by a separator at least every 7 characters, or reordered,
// is not — doc.go, KNOWN LIMITS), and
// long enough that an honest provider id survives: an SES id is hex and dashes,
// and an 8-character run shared by chance with the bodies, the 60-hex-digit
// boundary or the base32 Message-ID header is improbable (a shared hex run with
// the boundary alone is about 16^-8 per position pair). The cost of a false
// match is only an empty message_id in the caller's log.
const echoWindow = 8

// enhancedStatus reports whether f is an RFC 3463 status code: class.subject.detail
// with a class of 2, 4 or 5 and one to three digits in each other part.
func enhancedStatus(f string) bool {
	parts := strings.Split(f, ".")
	if len(parts) != 3 || (parts[0] != "2" && parts[0] != "4" && parts[0] != "5") {
		return false
	}
	for _, p := range parts[1:] {
		if len(p) < 1 || len(p) > 3 {
			return false
		}
		for i := 0; i < len(p); i++ {
			if p[i] < '0' || p[i] > '9' {
				return false
			}
		}
	}
	return true
}
