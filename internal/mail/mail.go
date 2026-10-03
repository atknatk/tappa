package mail

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultTimeout bounds one Send (both attempts and the pause between them)
	// when Config.Timeout is zero. The context's own deadline, when earlier, wins.
	DefaultTimeout = 15 * time.Second
	// DefaultRetryDelay is the pause before the single retry of a 4xx when
	// Config.RetryDelay is zero.
	DefaultRetryDelay = 2 * time.Second
	// helloName is the EHLO argument: net/smtp's own default, stated.
	helloName = "localhost"
	// maxReplyBytes caps what ONE attempt reads from the relay — every byte off
	// the raw connection, before and after TLS (the TLS records are counted as
	// they arrive, so the handshake counts too). Why it must exist (security
	// audit, measured): net/textproto reads a line with an unbounded append, and a
	// 120 MiB greeting without a line end took the process to a 545-562 MiB Sys
	// before the deadline, against a 512Mi pod limit; anyone answering TCP at the
	// relay's address can send one, no certificate needed. Why 256 KiB: an honest
	// relay sends a greeting, two EHLO replies and a handful of reply lines — a
	// few hundred bytes — plus one TLS server flight, whose bulk is the
	// certificate chain (a few KiB; a long RSA-4096 chain stays under 16 KiB). So
	// the cap is >10x what a legitimate attempt reads and bounds what a hostile
	// one can make Send hold; TestSend_CapsWhatTheRelayCanMakeItRead measures
	// both sides. Exceeding it ends the attempt as network — EXCEPT when the line
	// it cuts is the 250 reply to the end of data: that code was read, so the
	// message was accepted and Send reports success with an empty MessageID
	// (TestSend_ACutReplyToTheEndOfDataIsAcceptance; bufio.Reader.ReadLine hands
	// back a cut line without its error, and nothing is read after the 250).
	maxReplyBytes = 256 << 10
)

// errReplyTooLarge is cappedConn's refusal; classify maps it to ClassNetwork.
var errReplyTooLarge = errors.New("mail: the relay sent more than the reply cap")

// cappedConn counts the bytes read from the relay and refuses past
// maxReplyBytes. It wraps the RAW connection, so TLS sits on top of it and the
// one counter covers the whole attempt.
type cappedConn struct {
	net.Conn
	left int
}

func (c *cappedConn) Read(p []byte) (int, error) {
	if c.left <= 0 {
		return 0, errReplyTooLarge
	}
	if len(p) > c.left {
		p = p[:c.left]
	}
	n, err := c.Conn.Read(p)
	c.left -= n
	return n, err
}

// redactedMessage is what a Message prints as.
const redactedMessage = "mail.Message(redacted)"

// Message is one transactional e-mail. The package knows nothing of what it is
// for: invitations and resets are the callers' words, not this package's.
//
// ⚠️ A MESSAGE CARRIES THE LINK AND THE ADDRESS IN PLAIN STRINGS, AND IS NOT TO BE
// LOGGED — like invite.Delivery and the reset path's ResetDelivery, it is a
// declared exception to the log rule (ADR 0022 §10), alive only between the
// caller's template and Send. Its own printing paths are redacted (Format for
// every verb, String, GoString, LogValue, MarshalText —
// TestMessage_PrintsAsAPlaceholder), but the fields are exported strings: a
// Message read out of an UNEXPORTED field of the caller's struct is printed by
// fmt's reflection, fields and all (TestMessage_KnownLimitIsAnUnexportedField),
// and a caller that logs m.Text or m.To logs them. Neither is closed here.
type Message struct {
	// To is one bare ASCII address (validRecipient): no display name, no list.
	To string
	// Subject is plain UTF-8 text; it is RFC 2047-encoded when it is not ASCII.
	Subject string
	// Text and HTML are the two alternatives; both are required, both UTF-8.
	Text string
	HTML string
	// Ref is the caller's optional correlation label (an invite or reset id), of
	// 1..128 [A-Za-z0-9._:-]. It is for the CALLER's log line, next to the
	// receipt's message id (ADR 0022 §2): it is shape-checked here and goes
	// nowhere — not into the message, not to the relay.
	Ref string
}

var (
	_ fmt.Formatter          = Message{}
	_ fmt.Stringer           = Message{}
	_ fmt.GoStringer         = Message{}
	_ slog.LogValuer         = Message{}
	_ encoding.TextMarshaler = Message{}
)

// Format implements fmt.Formatter for every verb, %#v included.
func (Message) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redactedMessage)) }

// String implements fmt.Stringer.
func (Message) String() string { return redactedMessage }

// GoString implements fmt.GoStringer.
func (Message) GoString() string { return redactedMessage }

// LogValue implements slog.LogValuer.
func (Message) LogValue() slog.Value { return slog.StringValue(redactedMessage) }

// MarshalText implements encoding.TextMarshaler, which encoding/json prefers.
func (Message) MarshalText() ([]byte, error) { return []byte(redactedMessage), nil }

// Receipt is what an accepted send returns.
type Receipt struct {
	// MessageID is the provider's id from the reply to the end of DATA, or "" when
	// that reply carried none that passed messageID's shape and echo rules.
	MessageID string
}

// Config is what New needs. It is the shape of the TAPPA_SMTP_* / TAPPA_MAIL_*
// settings; reading and validating the environment is internal/config's job.
type Config struct {
	Host     string // the relay; the TLS certificate is verified against it
	Port     int
	Username Credential // for SES an access key id: redacted like the password
	Password Credential
	From     string // "Name <addr>" or a bare address; also the envelope sender
	ReplyTo  string // optional; ONE BARE address under the recipient rule (no name)
	// RootCAs verifies the relay's certificate; nil means the system roots.
	RootCAs *x509.CertPool
	// Timeout bounds one Send; zero means DefaultTimeout.
	Timeout time.Duration
	// RetryDelay is the pause before the single 4xx retry; zero means
	// DefaultRetryDelay.
	RetryDelay time.Duration
}

// SMTP sends Messages through one relay. Build it with New; it is immutable and
// safe for concurrent use (every Send dials its own connection).
type SMTP struct {
	addr, host     string
	username       Credential
	password       Credential
	envelopeFrom   string
	fromHeader     string
	fromDomain     string
	replyToHeader  string
	rootCAs        *x509.CertPool
	timeout, delay time.Duration
}

// New validates c and returns the sender. Its errors name the field and never
// quote a value (an address, a host or the password).
func New(c Config) (*SMTP, error) {
	if c.Host == "" || !printableASCII(c.Host, false) {
		return nil, errors.New("mail: Host is empty or not printable ASCII")
	}
	if c.Port < 1 || c.Port > 65535 {
		return nil, errors.New("mail: Port is not in 1..65535")
	}
	// A control character in either credential is refused, NUL above all: AUTH
	// PLAIN separates its fields with NUL, so one inside a value would shift them.
	if u := c.Username.reveal(); u == "" || !validHeaderText(u) {
		return nil, errors.New("mail: Username is empty or carries a control character")
	}
	if pw := c.Password.reveal(); pw == "" || !validHeaderText(pw) {
		return nil, errors.New("mail: Password is empty or carries a control character")
	}
	if c.Timeout < 0 || c.RetryDelay < 0 {
		return nil, errors.New("mail: Timeout and RetryDelay must not be negative")
	}
	from, ok := parseSender(c.From)
	if !ok {
		return nil, errors.New("mail: From is not one address with an optional display name")
	}
	s := &SMTP{
		addr:         net.JoinHostPort(c.Host, strconv.Itoa(c.Port)),
		host:         c.Host,
		username:     c.Username,
		password:     c.Password,
		envelopeFrom: from.Address,
		fromHeader:   from.String(),
		fromDomain:   from.Address[strings.LastIndexByte(from.Address, '@')+1:],
		rootCAs:      c.RootCAs,
		timeout:      c.Timeout,
		delay:        c.RetryDelay,
	}
	// Reply-To is where a recipient's answer goes, so it is held to the RECIPIENT
	// rule (ADR 0022 §5): one bare address, no display name.
	if c.ReplyTo != "" {
		if !validRecipient(c.ReplyTo) {
			return nil, errors.New("mail: ReplyTo is not one bare address under the recipient rule")
		}
		s.replyToHeader = c.ReplyTo
	}
	if s.timeout == 0 {
		s.timeout = DefaultTimeout
	}
	if s.delay == 0 {
		s.delay = DefaultRetryDelay
	}
	return s, nil
}

// parseSender accepts the From value: no control character and no "=?" in the
// raw text (refused, not cleaned — validSubject's rule), one address that
// net/mail parses, whose addr-spec passes validRecipient. A display name is
// allowed; net/mail re-renders it (quoted, or RFC 2047-encoded when it is not
// ASCII).
func parseSender(v string) (*mail.Address, bool) {
	if !validSubject(v) {
		return nil, false
	}
	a, err := mail.ParseAddress(v)
	if err != nil || !validRecipient(a.Address) {
		return nil, false
	}
	return a, true
}

// Send composes m, then delivers it: dial with the deadline → STARTTLS (required)
// → AUTH PLAIN → MAIL/RCPT → DATA → QUIT. A 4xx reply is retried once, after
// RetryDelay, on a new connection — only when the deadline leaves time for the
// pause and the context is not done; the retry's outcome is final. There is no
// queue: a message that is not accepted here is the caller's to report.
//
// Every error is a *SendError, except ErrNotConfigured. Invalid input is refused
// before any connection is made.
func (s *SMTP) Send(ctx context.Context, m Message) (Receipt, error) {
	if s == nil || s.addr == "" {
		return Receipt{}, ErrNotConfigured
	}
	now := time.Now()
	raw, err := s.compose(m, now)
	if err != nil {
		return Receipt{}, err
	}
	deadline := now.Add(s.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	// The echo list for messageID (ADR 0022 §2's window rule), and ONLY this list:
	// the composed message as sent; To, From, Subject, Text and HTML in the form
	// the caller gave them (the bodies before quoted-printable, which splits a
	// link with soft line breaks and writes '=' as =3D; the subject before RFC
	// 2047); the Ref, which travels nowhere but is the caller's log label; the
	// username and password; and the AUTH PLAIN argument as base64. Other bytes
	// of the conversation — the EHLO argument, the host name in the TLS SNI — are
	// not on it: neither is secret.
	user, pass := s.username.reveal(), s.password.reveal()
	sent := []string{string(raw), m.To, s.fromHeader, m.Subject, m.Ref, m.Text, m.HTML,
		user, pass, base64.StdEncoding.EncodeToString([]byte("\x00" + user + "\x00" + pass))}

	r, se := s.attempt(ctx, deadline, raw, m.To, sent)
	if se == nil {
		return r, nil
	}
	if se.Class != ClassThrottled || !s.pause(ctx, deadline) {
		return Receipt{}, se
	}
	r, se = s.attempt(ctx, deadline, raw, m.To, sent)
	if se == nil {
		return r, nil
	}
	return Receipt{}, se
}

// pause waits RetryDelay before the retry. It refuses (false) when the deadline
// would not outlast the pause, and gives up the moment the context is done.
func (s *SMTP) pause(ctx context.Context, deadline time.Time) bool {
	if time.Until(deadline) <= s.delay {
		return false
	}
	t := time.NewTimer(s.delay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// attempt is one connection's conversation.
//
// NET/SMTP IS FROZEN ("not accepting new features") but inside Go's security
// policy. Used: NewClient, Client.Hello, Client.Extension, Client.StartTLS,
// Client.Auth with PlainAuth, Client.Mail, Client.Rcpt, and Client.Text (the
// exported *textproto.Conn) for DATA and QUIT. NOT used, and why:
//   - smtp.SendMail: no context or deadline; STARTTLS is OPPORTUNISTIC there (if
//     the server does not advertise it the conversation continues in plain text);
//     its tls.Config sets no MinVersion.
//   - Client.Data: its Close reads the reply to the end of data and DISCARDS the
//     text, so the provider's message id is unreachable through it. DATA is
//     therefore written by hand on Client.Text: the 354, a DotWriter (which
//     dot-stuffs: a body line starting with "." is sent as "..", RFC 5321
//     §4.5.2, and the terminating ".\r\n" is written by Close), then the 250.
//   - Client.Quit: it waits for the 221 (see the end of this function).
//   - PlainAuth's own guard ("TLS, or a host named localhost/127.0.0.1/::1") is
//     NOT the TLS gate here: the STARTTLS check below is. The guard is off for
//     exactly the loopback hosts the tests use, which is why the tests measure
//     this function's gate and not net/smtp's.
//
// TWO MECHANISMS BOUND THE TIME, and they overlap: the deadline is set on the RAW
// connection, and context.AfterFunc moves it to the past when the attempt's
// context ends — which happens at the same deadline (the context carries it) or
// at a cancellation. So every read and write, the TLS handshake included, returns
// by then. MEASURED: removing AfterFunc turns the cancellation rows of the timing
// test red; removing the SetDeadline line alone leaves every test green, because
// AfterFunc also fires at the deadline. The SetDeadline is kept as the direct
// form ADR 0022 §2 names, and it is NOT separately pinned (doc.go, KNOWN LIMITS).
// Failure paths close the RAW connection, not the TLS one: crypto/tls's Close
// sends close_notify under its own five-second write deadline.
func (s *SMTP) attempt(ctx context.Context, deadline time.Time, raw []byte, to string, sent []string) (Receipt, *SendError) {
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return Receipt{}, classify(ctx, stageConnect, err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(deadline); err != nil {
		return Receipt{}, classify(ctx, stageConnect, err)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Unix(1, 0)) })
	defer stop()

	// Everything read from here on — greeting, EHLO, the TLS handshake and every
	// reply after it — passes through the one counter (maxReplyBytes).
	c, err := smtp.NewClient(&cappedConn{Conn: conn, left: maxReplyBytes}, s.host)
	if err != nil {
		return Receipt{}, classify(ctx, stageConnect, err)
	}
	if err := c.Hello(helloName); err != nil {
		return Receipt{}, classify(ctx, stageConnect, err)
	}
	// 🔴 THE TLS GATE. No STARTTLS in the EHLO reply → stop here: no fallback to
	// plain text, and so no AUTH (the credentials never leave in clear).
	if ok, _ := c.Extension("STARTTLS"); !ok {
		return Receipt{}, &SendError{Class: ClassTLS}
	}
	if err := c.StartTLS(&tls.Config{
		ServerName: s.host,
		MinVersion: tls.VersionTLS12,
		RootCAs:    s.rootCAs,
	}); err != nil {
		return Receipt{}, classify(ctx, stageTLS, err)
	}
	if err := c.Auth(smtp.PlainAuth("", s.username.reveal(), s.password.reveal(), s.host)); err != nil {
		return Receipt{}, classify(ctx, stageAuth, err)
	}
	if err := c.Mail(s.envelopeFrom); err != nil {
		return Receipt{}, classify(ctx, stageMessage, err)
	}
	if err := c.Rcpt(to); err != nil {
		return Receipt{}, classify(ctx, stageMessage, err)
	}
	reply, err := data(c, raw)
	if err != nil {
		return Receipt{}, classify(ctx, stageMessage, err)
	}
	// The message was accepted at the 250 above. QUIT is written but its 221 is
	// NOT awaited: neither the reply nor a failure can un-accept the message, and
	// waiting would let a server that never answers QUIT hold a delivered send
	// until the deadline (TestSend_AServerThatNeverAnswersQUITDoesNotHoldTheSend).
	_, _ = c.Text.Cmd("QUIT")
	return Receipt{MessageID: messageID(reply, sent)}, nil
}

// data is DATA by hand (attempt's comment says why): it returns the text of the
// reply to the end of data.
func data(c *smtp.Client, raw []byte) (string, error) {
	id, err := c.Text.Cmd("DATA")
	if err != nil {
		return "", err
	}
	c.Text.StartResponse(id)
	_, _, err = c.Text.ReadResponse(354)
	c.Text.EndResponse(id)
	if err != nil {
		return "", err
	}
	w := c.Text.DotWriter()
	if _, err := w.Write(raw); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	_, reply, err := c.Text.ReadResponse(250)
	return reply, err
}
