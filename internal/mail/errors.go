package mail

import (
	"context"
	"errors"
	"io"
	"net"
	"net/textproto"
	"os"
	"strconv"
)

// Class is the kind of a failed send. It is the ONLY description of a failure this
// package hands out (with the reply code): a caller logs the class and the code,
// and decides retry/undelivered from the class.
type Class string

const (
	// ClassInvalidAddress: Message.To breaks the recipient rule (validRecipient):
	// not one bare printable-ASCII address of at most 254 bytes that net/mail
	// parses back to itself, or it contains "=?". Decided before any dial.
	ClassInvalidAddress Class = "invalid_address"
	// ClassInvalidMessage: the Subject breaks validSubject (empty, invalid UTF-8,
	// a control character, U+2028/U+2029, or "=?"), the Ref breaks validRef, a
	// body is empty or invalid UTF-8, or a header line would exceed RFC 5322's
	// 998 octets. Decided before any dial.
	ClassInvalidMessage Class = "invalid_message"
	// ClassTLS: STARTTLS was not advertised, was refused, or the TLS handshake
	// failed (untrusted certificate, a version below 1.2, a broken stream). Nothing
	// was authenticated: AUTH is never sent without TLS.
	ClassTLS Class = "tls_unavailable"
	// ClassAuth: the server refused the credentials (a 5xx to AUTH).
	ClassAuth Class = "auth"
	// ClassRejected: the server refused the message permanently (a 5xx to the
	// greeting, MAIL, RCPT, DATA or the end of data).
	ClassRejected Class = "rejected"
	// ClassThrottled: a 4xx reply. Send retries it once; a second 4xx — or a 4xx
	// that could not be retried because the pause would not fit before the
	// deadline or the context ended during it — ends with this class and the
	// relay's code. DECISION (not timeout): what ended the send is the relay's
	// 4xx; time only decided that it was not retried. Reporting timeout would hide
	// a provider throttle, the one operational signal a caller's log needs here,
	// and would have no reply code to carry.
	ClassThrottled Class = "throttled"
	// ClassNetwork: the connection failed, the server broke the protocol (an
	// unexpected reply code, a malformed reply, a closed stream), or the server
	// sent more than maxReplyBytes in one attempt — unless the cut line is the 250
	// reply to the end of data, which is acceptance (maxReplyBytes says why).
	ClassNetwork Class = "network"
	// ClassTimeout: the deadline passed or the context was cancelled.
	ClassTimeout Class = "timeout"
)

// SendError is the only error Send returns for a send that was attempted or
// refused (ErrNotConfigured aside).
//
// 🔴 IT CARRIES NO TEXT, BY CONSTRUCTION. The server's reply text is never stored:
// an SES sandbox refusal names the recipient address in that text, a relay may
// quote the message, and the panel's invitation path logs a delivery error's whole
// chain (internal/handler/employeeactions.go, the IssueAndDeliver failure branch).
// So the type has exactly two fields — a Class drawn from the constants above and
// a reply code that textproto has already parsed as three digits — and exactly one
// method, Error. In particular there is NO Unwrap (nor Is/As): the
// *textproto.Error, whose Error() is "<code> <server text>", is dropped where it is
// classified and cannot be reached through errors.Unwrap/As from a SendError.
// The field and method sets are pinned by TestSendError_HasOnlyAClassAndACode.
type SendError struct {
	Class    Class
	SMTPCode int // the reply code that decided the class; 0 when no reply did
}

// Error is "mail: <class>" or "mail: <class> (smtp <code>)".
func (e *SendError) Error() string {
	if e.SMTPCode == 0 {
		return "mail: " + string(e.Class)
	}
	return "mail: " + string(e.Class) + " (smtp " + strconv.Itoa(e.SMTPCode) + ")"
}

// ErrNotConfigured is returned by Send on an SMTP that New did not build.
var ErrNotConfigured = errors.New("mail: SMTP is not configured (build it with New)")

// stage is the step of the SMTP conversation an error came from; the class of a
// reply code depends on it.
type stage int

const (
	stageConnect stage = iota // dial, greeting, EHLO
	stageTLS                  // STARTTLS, handshake, the EHLO after it
	stageAuth                 // AUTH PLAIN
	stageMessage              // MAIL, RCPT, DATA, end of data
)

// classify turns an error from one stage into a SendError, dropping the error's
// text. Order matters: the reply cap first (it is this package's own refusal,
// whatever stage it hit); a timeout is a timeout whatever stage it hit; a reply
// code decides next; then the TLS stage owns every remaining failure (fail
// closed: no AUTH follows); then a broken stream is the network's.
func classify(ctx context.Context, st stage, err error) *SendError {
	if errors.Is(err, errReplyTooLarge) {
		return &SendError{Class: ClassNetwork}
	}
	if ctx.Err() != nil || isTimeout(err) {
		return &SendError{Class: ClassTimeout}
	}
	var te *textproto.Error
	if errors.As(err, &te) {
		return byCode(st, te.Code)
	}
	if st == stageTLS {
		return &SendError{Class: ClassTLS}
	}
	if st == stageAuth && !isTransport(err) {
		// net/smtp's PlainAuth refusing locally, or the server answering PLAIN's
		// initial response with a challenge: the exchange, not the stream, failed.
		return &SendError{Class: ClassAuth}
	}
	return &SendError{Class: ClassNetwork}
}

// byCode classifies a reply code. textproto guarantees 100..999.
func byCode(st stage, code int) *SendError {
	switch {
	case st == stageTLS:
		return &SendError{Class: ClassTLS, SMTPCode: code}
	case code >= 400 && code <= 499:
		return &SendError{Class: ClassThrottled, SMTPCode: code}
	case code >= 500 && code <= 599 && st == stageAuth:
		return &SendError{Class: ClassAuth, SMTPCode: code}
	case code >= 500 && code <= 599:
		return &SendError{Class: ClassRejected, SMTPCode: code}
	}
	// A 1xx/2xx/3xx where another code was required, or 6xx-9xx: the server did
	// not follow the protocol.
	return &SendError{Class: ClassNetwork, SMTPCode: code}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) ||
		(errors.As(err, &ne) && ne.Timeout())
}

func isTransport(err error) bool {
	var oe *net.OpError
	var pe textproto.ProtocolError
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, net.ErrClosed) || errors.As(err, &oe) || errors.As(err, &pe)
}
