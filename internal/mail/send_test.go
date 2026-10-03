package mail_test

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	netmail "net/mail"
	"net/smtp"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/mail"
)

// errShape is the whole text a SendError may have: the package prefix, a class
// word and at most a three-digit code.
var errShape = regexp.MustCompile(`^mail: [a-z_]+( \(smtp [0-9]{3}\))?$`)

// sendError asserts err is a *mail.SendError of the given class and code, and
// that its text has errShape — on every failure any test in this package drives.
func sendError(t *testing.T, err error, class mail.Class, code int) {
	t.Helper()
	var se *mail.SendError
	if !errors.As(err, &se) {
		t.Fatalf("error %v (%T) is not a *mail.SendError", err, err)
	}
	if se.Class != class || se.SMTPCode != code {
		t.Fatalf("got class %q code %d, want %q %d", se.Class, se.SMTPCode, class, code)
	}
	if !errShape.MatchString(err.Error()) {
		t.Fatalf("error text %q is not of the shape %s", err.Error(), errShape)
	}
}

// newSender builds the product object through New (the only constructor) for
// server f, with a password drawn at run time.
func newSender(t *testing.T, f *fakeSMTP, edit func(*mail.Config)) (*mail.SMTP, string) {
	t.Helper()
	pw := rand.Text()
	c := f.config(pw)
	if edit != nil {
		edit(&c)
	}
	s, err := mail.New(c)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, pw
}

func validMessage() mail.Message {
	return mail.Message{
		To:      "maria.borg@example.test",
		Subject: "Activate your Taptime account",
		Text:    "Hello Maria,\n\nOpen this link on your phone:\nhttps://taptime.test/activate?code=abc\n",
		HTML:    `<p style="color:#152219">Hello Maria,</p><p><a href="https://taptime.test/activate?code=abc">Activate</a></p>`,
		Ref:     "invite:0b7e4c1a-2f9d-4e1b-9a55-7d3c2b1a0f9e",
	}
}

// TestSend_DeliversOverSTARTTLSAndReturnsTheProvidersMessageID: the server sees,
// in this order, EHLO → STARTTLS → (handshake) → EHLO → AUTH → MAIL → RCPT →
// DATA → QUIT; the one AUTH arrives over TLS and carries the configured
// credentials; the envelope is the From address and the recipient; the receipt
// is the id in the 250.
func TestSend_DeliversOverSTARTTLSAndReturnsTheProvidersMessageID(t *testing.T) {
	f := newFake(t, always(fakeConfig{}))
	s, pw := newSender(t, f, nil)
	m := validMessage()
	r, _, err := sendWithin(context.Background(), t, 10*time.Second, s, m)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if r.MessageID != defaultMessageID {
		t.Fatalf("MessageID = %q, want the id of the 250 reply", r.MessageID)
	}
	if f.accepted() != 1 {
		t.Fatalf("connections = %d, want 1", f.accepted())
	}
	sess := f.session(0)
	want := "EHLO STARTTLS TLS EHLO AUTH MAIL RCPT DATA QUIT"
	if got := strings.Join(sess.verbs, " "); got != want {
		t.Fatalf("conversation = %q, want %q", got, want)
	}
	if sess.authClear != 0 || sess.authTLS != 1 {
		t.Fatalf("AUTH before TLS = %d, after = %d; want 0 and 1", sess.authClear, sess.authTLS)
	}
	if sess.authPlain != "\x00smtp-user\x00"+pw {
		t.Fatal("the AUTH PLAIN response is not the configured username and password")
	}
	if sess.mailFrom != "MAIL FROM:<no-reply@taptime.test>" &&
		!strings.HasPrefix(sess.mailFrom, "MAIL FROM:<no-reply@taptime.test> ") {
		t.Fatalf("MAIL = %q", sess.mailFrom)
	}
	if len(sess.rcptTo) != 1 || sess.rcptTo[0] != "RCPT TO:<"+m.To+">" {
		t.Fatalf("RCPT = %q", sess.rcptTo)
	}
}

// TestSend_TheMessageOnTheWire reads the message the SERVER received: exactly the
// expected headers, each once; the RFC 2047 subject decodes to the input; two
// quoted-printable parts that decode to Text and HTML; every wire line 7-bit
// printable, CRLF-terminated and at most 998 octets.
func TestSend_TheMessageOnTheWire(t *testing.T) {
	f := newFake(t, always(fakeConfig{}))
	s, _ := newSender(t, f, func(c *mail.Config) { c.ReplyTo = "support@taptime.test" })
	m := validMessage()
	m.Subject = "Ħello — ċ ġ ħ ż, your Taptime link"
	m.Text = strings.Repeat("Il-link tiegħek: https://taptime.test/a?x=1 ", 40) + "\n.\nend"
	m.HTML = "<p>" + strings.Repeat("ħ", 400) + "</p>"
	if _, _, err := sendWithin(context.Background(), t, 10*time.Second, s, m); err != nil {
		t.Fatalf("Send: %v", err)
	}
	sess := f.session(0)
	for i, l := range sess.wire {
		if !strings.HasSuffix(l, "\r\n") || strings.ContainsAny(strings.TrimSuffix(l, "\r\n"), "\r\n") {
			t.Fatalf("wire line %d is not one CRLF-terminated line: %q", i, l)
		}
		if len(l)-2 > 998 {
			t.Fatalf("wire line %d has %d octets", i, len(l)-2)
		}
		for _, b := range []byte(strings.TrimSuffix(l, "\r\n")) {
			if b < 0x20 || b > 0x7e {
				t.Fatalf("wire line %d carries byte %#x", i, b)
			}
		}
	}
	msg, err := netmail.ReadMessage(strings.NewReader(sess.message))
	if err != nil {
		t.Fatalf("the received message does not parse: %v", err)
	}
	got := map[string]int{}
	for k, v := range msg.Header {
		got[k] = len(v)
	}
	want := map[string]int{"Date": 1, "From": 1, "Reply-To": 1, "To": 1, "Subject": 1,
		"Message-Id": 1, "Mime-Version": 1, "Content-Type": 1}
	if len(got) != len(want) {
		t.Fatalf("headers = %v, want exactly %v", got, want)
	}
	for k, n := range want {
		if got[k] != n {
			t.Fatalf("headers = %v, want exactly %v", got, want)
		}
	}
	if msg.Header.Get("To") != m.To {
		t.Fatalf("To = %q", msg.Header.Get("To"))
	}
	// Ref is the caller's log label and goes nowhere (ADR 0022 §2): its id part
	// is in no line the relay received — no command line in full (EHLO, MAIL,
	// RCPT …, before and after TLS) and no line of the message.
	refID := strings.SplitN(m.Ref, ":", 2)[1]
	if len(sess.lines) < 6 {
		t.Fatalf("control: the fake recorded only %d command lines", len(sess.lines))
	}
	for _, l := range append(append([]string{}, sess.lines...), sess.message) {
		if strings.Contains(l, refID) {
			t.Fatalf("Message.Ref reached the relay in %q", l)
		}
	}
	if msg.Header.Get("Reply-To") != "support@taptime.test" {
		t.Fatalf("Reply-To = %q", msg.Header.Get("Reply-To"))
	}
	if msg.Header.Get("From") != `"Taptime" <no-reply@taptime.test>` {
		t.Fatalf("From = %q", msg.Header.Get("From"))
	}
	rawSubject := msg.Header.Get("Subject")
	if !strings.HasPrefix(rawSubject, "=?utf-8?q?") {
		t.Fatalf("a non-ASCII subject is not RFC 2047 Q-encoded: %q", rawSubject)
	}
	if dec, err := new(mime.WordDecoder).DecodeHeader(rawSubject); err != nil || dec != m.Subject {
		t.Fatalf("subject decodes to %q (%v), want %q", dec, err, m.Subject)
	}
	if !regexp.MustCompile(`^<[A-Z2-7]{26}@taptime\.test>$`).MatchString(msg.Header.Get("Message-Id")) {
		t.Fatalf("Message-ID = %q", msg.Header.Get("Message-Id"))
	}
	if _, err := msg.Header.Date(); err != nil {
		t.Fatalf("Date: %v", err)
	}
	mt, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/alternative" {
		t.Fatalf("Content-Type = %q", msg.Header.Get("Content-Type"))
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	for i, wantPart := range []struct{ ctype, body string }{
		{"text/plain; charset=utf-8", m.Text},
		{"text/html; charset=utf-8", m.HTML},
	} {
		p, err := mr.NextRawPart()
		if err != nil {
			t.Fatalf("part %d: %v", i, err)
		}
		if p.Header.Get("Content-Type") != wantPart.ctype || p.Header.Get("Content-Transfer-Encoding") != "quoted-printable" {
			t.Fatalf("part %d headers = %v", i, p.Header)
		}
		b, err := io.ReadAll(quotedprintable.NewReader(p))
		if err != nil {
			t.Fatalf("part %d: %v", i, err)
		}
		if strings.ReplaceAll(string(b), "\r\n", "\n") != wantPart.body {
			t.Fatalf("part %d decodes to %q, want %q", i, b, wantPart.body)
		}
	}
	if _, err := mr.NextRawPart(); err != io.EOF {
		t.Fatalf("a third part, or a broken end: %v", err)
	}

	// An ASCII subject goes out unchanged: mime's Q encoder encodes only when a
	// byte is outside printable ASCII (the subjects of ADR 0022 §4 are ASCII).
	m.Subject = "Your Taptime invitation"
	if _, _, err := sendWithin(context.Background(), t, 10*time.Second, s, m); err != nil {
		t.Fatalf("Send: %v", err)
	}
	ascii, err := netmail.ReadMessage(strings.NewReader(f.session(1).message))
	if err != nil {
		t.Fatal(err)
	}
	if got := ascii.Header.Get("Subject"); got != m.Subject {
		t.Fatalf("an ASCII subject went out as %q", got)
	}
}

// TestSend_DotStuffsALoneDotLine: a body line that is a lone "." would end DATA
// (RFC 5321 §4.5.2) if it were sent as is; it must arrive as ".." on the wire,
// un-stuff to ".", and leave the conversation in step (QUIT follows DATA).
func TestSend_DotStuffsALoneDotLine(t *testing.T) {
	f := newFake(t, always(fakeConfig{}))
	s, _ := newSender(t, f, nil)
	m := validMessage()
	m.Text = "first\n.\n.second starts with a dot\nRCPT TO:<other@example.test>\nlast"
	m.HTML = ".\n<p>x</p>\n."
	if _, _, err := sendWithin(context.Background(), t, 10*time.Second, s, m); err != nil {
		t.Fatalf("Send: %v", err)
	}
	sess := f.session(0)
	if got := strings.Join(sess.verbs, " "); !strings.HasSuffix(got, "DATA QUIT") || strings.Count(got, "RCPT") != 1 {
		t.Fatalf("conversation = %q: the body leaked into the command stream", got)
	}
	stuffed, lone := 0, 0
	for _, l := range sess.wire {
		switch l {
		case "..\r\n":
			stuffed++
		case ".\r\n":
			lone++
		}
	}
	if stuffed != 3 || lone != 0 {
		t.Fatalf(`wire lines ".." = %d (want 3), "." = %d (want 0)`, stuffed, lone)
	}
	if !strings.Contains(sess.message, "\r\n.\r\n.second starts with a dot\r\n") {
		t.Fatal("the un-stuffed body lost a dot line")
	}
}

// TestSend_NoTLSMeansNoAuth: every way TLS can fail to come up ends in
// tls_unavailable with ZERO AUTH commands at the server, before and after TLS,
// and no MAIL. The control row (TLS fine) shows the counter does count. Every
// row runs with Host "127.0.0.1" AND "localhost": the two names for which
// net/smtp's PlainAuth sends credentials without TLS
// (TestPlainAuth_SendsInClearForTheLoopbackNames), so a zero here is this
// package's gate and not PlainAuth's.
func TestSend_NoTLSMeansNoAuth(t *testing.T) {
	// The certificate rows pick, per dialled host, a certificate the client's pool
	// TRUSTS: one for a third name, one that lacks the dialled name, and (control)
	// one that carries exactly the dialled name and nothing else. So a client that
	// verified the chain but not the name, or verified a fixed name, would connect.
	without := map[string]string{"127.0.0.1": "dns", "localhost": "ip"}
	exactly := map[string]string{"127.0.0.1": "ip", "localhost": "dns"}
	tests := []struct {
		name      string
		cfg       fakeConfig
		certFor   map[string]string
		untrusted bool
		wantCode  int
		wantAuth  int
		wantClass mail.Class
	}{
		{name: "STARTTLS not advertised", cfg: fakeConfig{noSTARTTLS: true}, wantClass: mail.ClassTLS},
		{name: "STARTTLS refused 454", cfg: fakeConfig{starttlsReply: "454 4.7.0 TLS not available"}, wantClass: mail.ClassTLS, wantCode: 454},
		{name: "STARTTLS refused 502", cfg: fakeConfig{starttlsReply: "502 5.5.1 no"}, wantClass: mail.ClassTLS, wantCode: 502},
		{name: "certificate not trusted", cfg: fakeConfig{}, untrusted: true, wantClass: mail.ClassTLS},
		{name: "trusted certificate for another name", cfg: fakeConfig{cert: "other"}, wantClass: mail.ClassTLS},
		{name: "trusted certificate without the dialled name", certFor: without, wantClass: mail.ClassTLS},
		{name: "server offers at most TLS 1.1", cfg: fakeConfig{tlsMaxVersion: tls.VersionTLS11}, wantClass: mail.ClassTLS},
		{name: "control: TLS 1.2 accepted", cfg: fakeConfig{tlsMaxVersion: tls.VersionTLS12}, wantAuth: 1},
		{name: "control: certificate for exactly the dialled name", certFor: exactly, wantAuth: 1},
	}
	for _, host := range []string{"127.0.0.1", "localhost"} {
		for _, tt := range tests {
			t.Run(host+"/"+tt.name, func(t *testing.T) {
				if tt.certFor != nil {
					tt.cfg.cert = tt.certFor[host]
				}
				f := newFake(t, always(tt.cfg))
				s, _ := newSender(t, f, func(c *mail.Config) {
					c.Host = host
					if tt.untrusted {
						c.RootCAs = nil // the system roots do not know the fake's certificate
					}
				})
				_, _, err := sendWithin(context.Background(), t, 10*time.Second, s, validMessage())
				if tt.wantClass == "" {
					if err != nil {
						t.Fatalf("control: Send: %v", err)
					}
				} else {
					sendError(t, err, tt.wantClass, tt.wantCode)
				}
				sess := f.session(0)
				if sess.authClear != 0 || sess.authTLS != tt.wantAuth {
					t.Fatalf("AUTH before TLS = %d, after = %d; want 0 and %d", sess.authClear, sess.authTLS, tt.wantAuth)
				}
				if tt.wantClass != "" && strings.Contains(strings.Join(sess.verbs, " "), "MAIL") {
					t.Fatal("a MAIL command followed a TLS failure")
				}
				if f.accepted() != 1 {
					t.Fatalf("connections = %d, want 1 (tls_unavailable is not retried)", f.accepted())
				}
			})
		}
	}
}

// TestPlainAuth_SendsInClearForTheLoopbackNames is the control behind
// TestSend_NoTLSMeansNoAuth's two host names (ADR 0022 S3, re-measured here):
// bare net/smtp, no STARTTLS, PlainAuth for the same name the client was built
// with — the server receives AUTH in clear for "127.0.0.1" and "localhost", and
// none for a name outside net/smtp's loopback list.
func TestPlainAuth_SendsInClearForTheLoopbackNames(t *testing.T) {
	for _, tt := range []struct {
		name string
		auth int
	}{{"127.0.0.1", 1}, {"localhost", 1}, {"mail.example.test", 0}} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake(t, always(fakeConfig{noSTARTTLS: true}))
			conn, err := net.Dial("tcp", f.ln.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			c, err := smtp.NewClient(conn, tt.name)
			if err != nil {
				t.Fatal(err)
			}
			authErr := c.Auth(smtp.PlainAuth("", "smtp-user", rand.Text(), tt.name))
			_ = c.Close() // for the refused name, Auth has already sent QUIT and closed
			sess := f.session(0)
			if sess.authClear != tt.auth {
				t.Fatalf("AUTH in clear = %d, want %d (Auth returned %v)", sess.authClear, tt.auth, authErr)
			}
		})
	}
}

// TestSend_RefusesBadInputBeforeAnyDial: a recipient or a header value that
// breaks the rules is refused with its class and the server accepts ZERO
// connections. The control send afterwards is accepted and is connection number
// one — a refused send that had dialled would have been accepted before it.
func TestSend_RefusesBadInputBeforeAnyDial(t *testing.T) {
	long := strings.Repeat("a", 254-len("@example.test")+1) + "@example.test" // 255 bytes
	tests := []struct {
		name  string
		edit  func(*mail.Message)
		class mail.Class
	}{
		{"To with CRLF and a Bcc", func(m *mail.Message) { m.To = "a@example.test\r\nBcc: b@example.test" }, mail.ClassInvalidAddress},
		{"To with LF", func(m *mail.Message) { m.To = "a@example.test\nBcc: b@example.test" }, mail.ClassInvalidAddress},
		{"To with CR", func(m *mail.Message) { m.To = "a@example.test\rBcc: b@example.test" }, mail.ClassInvalidAddress},
		{"To with NUL", func(m *mail.Message) { m.To = "a@example.test\x00" }, mail.ClassInvalidAddress},
		{"To with DEL", func(m *mail.Message) { m.To = "a\x7f@example.test" }, mail.ClassInvalidAddress},
		{"To with a display name", func(m *mail.Message) { m.To = "Victim <a@example.test>" }, mail.ClassInvalidAddress},
		{"To with a display name, no space", func(m *mail.Message) { m.To = "Victim<a@example.test>" }, mail.ClassInvalidAddress},
		{"To in angle brackets", func(m *mail.Message) { m.To = "<a@example.test>" }, mail.ClassInvalidAddress},
		{"To with two recipients", func(m *mail.Message) { m.To = "a@example.test,b@example.test" }, mail.ClassInvalidAddress},
		{"To with a quoted local part", func(m *mail.Message) { m.To = `"a"@example.test` }, mail.ClassInvalidAddress},
		{"To with a quoted display name", func(m *mail.Message) { m.To = `"Victim" <a@example.test>` }, mail.ClassInvalidAddress},
		{"To with a comment", func(m *mail.Message) { m.To = "a(x)@example.test" }, mail.ClassInvalidAddress},
		{"To with a trailing comment", func(m *mail.Message) { m.To = "a@example.test(x)" }, mail.ClassInvalidAddress},
		{"To with a leading space", func(m *mail.Message) { m.To = " a@example.test" }, mail.ClassInvalidAddress},
		{"To with a trailing space", func(m *mail.Message) { m.To = "a@example.test " }, mail.ClassInvalidAddress},
		{"To with an inner space", func(m *mail.Message) { m.To = "a b@example.test" }, mail.ClassInvalidAddress},
		{"To not ASCII", func(m *mail.Message) { m.To = "ħ@example.test" }, mail.ClassInvalidAddress},
		{"To with a non-ASCII local part", func(m *mail.Message) { m.To = "ćali@example.test" }, mail.ClassInvalidAddress},
		{"To with a non-ASCII domain", func(m *mail.Message) { m.To = "ali@exämple.test" }, mail.ClassInvalidAddress},
		{"To of 255 bytes", func(m *mail.Message) { m.To = long }, mail.ClassInvalidAddress},
		{"To shaped like an encoded word", func(m *mail.Message) { m.To = "=?utf-8?q?a=0D=0ABcc=3A_victim?=@example.test" }, mail.ClassInvalidAddress},
		{"To empty", func(m *mail.Message) { m.To = "" }, mail.ClassInvalidAddress},
		{"Subject with CRLF and a Bcc", func(m *mail.Message) { m.Subject = "Hi\r\nBcc: b@example.test" }, mail.ClassInvalidMessage},
		{"Subject with LF", func(m *mail.Message) { m.Subject = "Hi\nBcc: b@example.test" }, mail.ClassInvalidMessage},
		{"Subject with CR", func(m *mail.Message) { m.Subject = "Hi\rthere" }, mail.ClassInvalidMessage},
		{"Subject with NUL", func(m *mail.Message) { m.Subject = "Hi\x00" }, mail.ClassInvalidMessage},
		{"Subject with DEL", func(m *mail.Message) { m.Subject = "Hi\x7f" }, mail.ClassInvalidMessage},
		{"Subject with TAB", func(m *mail.Message) { m.Subject = "Hi\tthere" }, mail.ClassInvalidMessage},
		{"Subject with NEL (C1)", func(m *mail.Message) { m.Subject = "Hi\u0085there" }, mail.ClassInvalidMessage},
		{"Subject with U+2028", func(m *mail.Message) { m.Subject = "Hi\u2028there" }, mail.ClassInvalidMessage},
		{"Subject with U+2029", func(m *mail.Message) { m.Subject = "Hi\u2029there" }, mail.ClassInvalidMessage},
		{"Subject invalid UTF-8", func(m *mail.Message) { m.Subject = "Hi\xff" }, mail.ClassInvalidMessage},
		{"Subject empty", func(m *mail.Message) { m.Subject = "" }, mail.ClassInvalidMessage},
		{"Subject past 998 octets", func(m *mail.Message) { m.Subject = strings.Repeat("ħ", 300) }, mail.ClassInvalidMessage},
		{"Subject shaped like an encoded word", func(m *mail.Message) { m.Subject = "=?utf-8?q?Hi=0D=0ABcc:_b@example.test?=" }, mail.ClassInvalidMessage},
		{"Ref with CRLF", func(m *mail.Message) { m.Ref = "x\r\nBcc: b@example.test" }, mail.ClassInvalidMessage},
		{"Ref with an address", func(m *mail.Message) { m.Ref = "b@example.test" }, mail.ClassInvalidMessage},
		{"Ref with a URL", func(m *mail.Message) { m.Ref = "https://x.test/?a=b" }, mail.ClassInvalidMessage},
		{"Ref of 129 bytes", func(m *mail.Message) { m.Ref = strings.Repeat("r", 129) }, mail.ClassInvalidMessage},
		{"Text empty", func(m *mail.Message) { m.Text = "" }, mail.ClassInvalidMessage},
		{"HTML empty", func(m *mail.Message) { m.HTML = "" }, mail.ClassInvalidMessage},
		{"Text invalid UTF-8", func(m *mail.Message) { m.Text = "a\xffb" }, mail.ClassInvalidMessage},
		{"HTML invalid UTF-8", func(m *mail.Message) { m.HTML = "a\xffb" }, mail.ClassInvalidMessage},
	}
	f := newFake(t, always(fakeConfig{}))
	s, _ := newSender(t, f, nil)
	for _, tt := range tests {
		m := validMessage()
		tt.edit(&m)
		_, _, err := sendWithin(context.Background(), t, 5*time.Second, s, m)
		t.Run(tt.name, func(t *testing.T) { sendError(t, err, tt.class, 0) })
	}
	// Control for the encoded-word rows: what a reader makes of such a value. The
	// address parses back to itself (so only the "=?" rule refuses it), and
	// mime.WordDecoder turns it into a CR LF and a Bcc line.
	for _, v := range []string{"=?utf-8?q?a=0D=0ABcc=3A_victim?=@example.test", "=?utf-8?q?Hi=0D=0ABcc:_b@example.test?="} {
		dec, err := new(mime.WordDecoder).DecodeHeader(v)
		if err != nil || !strings.Contains(dec, "\r\nBcc") {
			t.Fatalf("control: %q decodes to %q (%v), not to an injected line", v, dec, err)
		}
	}
	if a, err := netmail.ParseAddress("=?utf-8?q?a=0D=0ABcc=3A_victim?=@example.test"); err != nil || a.Address != "=?utf-8?q?a=0D=0ABcc=3A_victim?=@example.test" {
		t.Fatalf("control: net/mail no longer accepts the encoded-word address as itself (%v)", err)
	}
	// Controls, sent after every refusal: the longest legal address and a subject
	// just inside the line limit (so the two size rows fail on size, not shape),
	// and the shapes the rule ACCEPTS (ADR 0022 S8): plus-addressing, upper case,
	// and a domain literal — the last a counted limit of the rule, not an oversight.
	controls := []func(*mail.Message){
		func(m *mail.Message) { m.To = long[1:] }, // 254 bytes
		func(m *mail.Message) { m.Subject = strings.Repeat("a", 998-len("Subject: ")) },
		func(m *mail.Message) { m.To = "ali+payroll@example.test" },
		func(m *mail.Message) { m.To = "ALI@EXAMPLE.TEST" },
		func(m *mail.Message) { m.To = "ali@[192.0.2.1]" },
	}
	for i, edit := range controls {
		m := validMessage()
		edit(&m)
		if _, _, err := sendWithin(context.Background(), t, 10*time.Second, s, m); err != nil {
			t.Fatalf("control send %d refused: %v", i, err)
		}
		if got := f.session(i).rcptTo; len(got) != 1 || got[0] != "RCPT TO:<"+m.To+">" {
			t.Fatalf("control %d: RCPT = %q, want the address verbatim", i, got)
		}
	}
	if n := f.accepted(); n != len(controls) {
		t.Fatalf("connections = %d, want exactly the %d of the controls (a refused send dialled)", n, len(controls))
	}
}

// TestSend_ReturnsWithin100msOfTheDeadline: a server that stops answering at any
// step — the TLS handshake included — does not hold Send past its deadline by
// more than 100 ms, whether the deadline comes from the context, from
// Config.Timeout or from a cancellation. The lower bound proves the server really
// hung there (an early error would return sooner).
func TestSend_ReturnsWithin100msOfTheDeadline(t *testing.T) {
	const budget = 400 * time.Millisecond
	const slack = 100 * time.Millisecond
	stages := []struct{ hang, verb string }{
		{"greeting", ""}, {"ehlo", "EHLO"}, {"handshake", "STARTTLS"}, {"auth", "AUTH"},
		{"mail", "MAIL"}, {"rcpt", "RCPT"}, {"data", "DATA"}, {"end", "DATA"},
	}
	for _, st := range stages {
		for _, how := range []string{"context deadline", "Config.Timeout", "cancellation"} {
			t.Run(st.hang+"/"+how, func(t *testing.T) {
				t.Parallel() // each row only waits; 24 rows in series would cost ~10 s
				f := newFake(t, always(fakeConfig{hang: st.hang}))
				s, _ := newSender(t, f, func(c *mail.Config) {
					c.Timeout = time.Minute
					if how == "Config.Timeout" {
						c.Timeout = budget
					}
				})
				ctx := context.Background()
				switch how {
				case "context deadline":
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, budget)
					defer cancel()
				case "cancellation":
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					defer cancel()
					time.AfterFunc(budget, cancel)
				}
				_, took, err := sendWithin(ctx, t, 5*time.Second, s, validMessage())
				sendError(t, err, mail.ClassTimeout, 0)
				if took < budget-20*time.Millisecond || took > budget+slack {
					t.Fatalf("returned after %v; want within [%v, %v]", took, budget, budget+slack)
				}
				t.Logf("returned %v after the deadline", took-budget)
				if st.verb != "" && !strings.Contains(strings.Join(f.session(0).verbs, " "), st.verb) {
					t.Fatalf("the server never reached %s", st.verb)
				}
			})
		}
	}
}

// TestSend_AServerThatNeverAnswersQUITDoesNotHoldTheSend: the 250 to the end of
// data is the delivery; a server that then never answers QUIT costs nothing.
func TestSend_AServerThatNeverAnswersQUITDoesNotHoldTheSend(t *testing.T) {
	f := newFake(t, always(fakeConfig{hang: "quit"}))
	s, _ := newSender(t, f, func(c *mail.Config) { c.Timeout = 3 * time.Second })
	r, took, err := sendWithin(context.Background(), t, 10*time.Second, s, validMessage())
	if err != nil || r.MessageID != defaultMessageID {
		t.Fatalf("Send = %q, %v", r.MessageID, err)
	}
	if took > time.Second {
		t.Fatalf("Send took %v: it waited for the 221", took)
	}
	if v := f.session(0).verbs; v[len(v)-1] != "QUIT" {
		t.Fatalf("conversation = %v: QUIT was not sent", v)
	}
}

// TestSend_RetriesA4xxOnceOnANewConnection: one 4xx is retried after the pause
// on a NEW connection; a second 4xx is final (exactly two connections, never a
// third); a 5xx is never retried; a deadline that cannot outlast the pause and a
// context cancelled during it both end with the first 4xx after one connection.
func TestSend_RetriesA4xxOnceOnANewConnection(t *testing.T) {
	throttle := fakeConfig{rcptReply: "451 4.7.1 slow down"}
	t.Run("4xx then accepted", func(t *testing.T) {
		f := newFake(t, func(n int) fakeConfig {
			if n == 1 {
				return throttle
			}
			return fakeConfig{}
		})
		s, _ := newSender(t, f, nil)
		r, _, err := sendWithin(context.Background(), t, 10*time.Second, s, validMessage())
		if err != nil || r.MessageID != defaultMessageID {
			t.Fatalf("Send = %q, %v", r.MessageID, err)
		}
		if f.accepted() != 2 {
			t.Fatalf("connections = %d, want 2", f.accepted())
		}
	})
	t.Run("4xx twice is final", func(t *testing.T) {
		f := newFake(t, always(throttle))
		s, _ := newSender(t, f, nil)
		_, _, err := sendWithin(context.Background(), t, 10*time.Second, s, validMessage())
		sendError(t, err, mail.ClassThrottled, 451)
		time.Sleep(100 * time.Millisecond) // a third dial, if any, would land by now
		if f.accepted() != 2 {
			t.Fatalf("connections = %d, want exactly 2", f.accepted())
		}
	})
	t.Run("5xx is not retried", func(t *testing.T) {
		f := newFake(t, always(fakeConfig{rcptReply: "550 5.1.1 no such user"}))
		s, _ := newSender(t, f, nil)
		_, _, err := sendWithin(context.Background(), t, 10*time.Second, s, validMessage())
		sendError(t, err, mail.ClassRejected, 550)
		time.Sleep(100 * time.Millisecond)
		if f.accepted() != 1 {
			t.Fatalf("connections = %d, want 1", f.accepted())
		}
	})
	t.Run("no time for the pause", func(t *testing.T) {
		f := newFake(t, always(throttle))
		s, _ := newSender(t, f, func(c *mail.Config) { c.Timeout = time.Second; c.RetryDelay = 2 * time.Second })
		_, took, err := sendWithin(context.Background(), t, 10*time.Second, s, validMessage())
		sendError(t, err, mail.ClassThrottled, 451)
		if took > 500*time.Millisecond || f.accepted() != 1 {
			t.Fatalf("took %v over %d connection(s): it waited for a retry it had no time for", took, f.accepted())
		}
	})
	t.Run("cancelled during the pause", func(t *testing.T) {
		f := newFake(t, always(throttle))
		s, _ := newSender(t, f, func(c *mail.Config) { c.RetryDelay = 3 * time.Second })
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		time.AfterFunc(300*time.Millisecond, cancel)
		_, took, err := sendWithin(ctx, t, 10*time.Second, s, validMessage())
		sendError(t, err, mail.ClassThrottled, 451)
		if took > 400*time.Millisecond || f.accepted() != 1 {
			t.Fatalf("took %v over %d connection(s): the cancellation did not end the pause", took, f.accepted())
		}
	})
}

// TestSend_ClassifiesEachStep: the class and code each reply (or broken stream)
// produces, step by step, AND how many connections the send made: two for a 4xx
// row (the one retry, same reply), exactly one for every other row — a 5xx at
// any step (greeting, AUTH, MAIL, RCPT, DATA, end of data), tls_unavailable, a
// broken stream or an unexpected code is never retried (ADR 0022 §2).
func TestSend_ClassifiesEachStep(t *testing.T) {
	tests := []struct {
		name  string
		cfg   fakeConfig
		class mail.Class
		code  int
		conns int
	}{
		{"greeting 421", fakeConfig{greeting: "421 4.3.2 busy"}, mail.ClassThrottled, 421, 2},
		{"greeting 554", fakeConfig{greeting: "554 5.3.2 no service"}, mail.ClassRejected, 554, 1},
		{"greeting 250 (unexpected code)", fakeConfig{greeting: "250 hello?"}, mail.ClassNetwork, 250, 1},
		{"greeting malformed", fakeConfig{greeting: "hello"}, mail.ClassNetwork, 0, 1},
		{"dropped at EHLO", fakeConfig{dropAt: "EHLO"}, mail.ClassNetwork, 0, 1},
		{"dropped at STARTTLS", fakeConfig{dropAt: "STARTTLS"}, mail.ClassTLS, 0, 1},
		{"AUTH 535", fakeConfig{authReply: "535 5.7.8 Authentication credentials invalid"}, mail.ClassAuth, 535, 1},
		{"AUTH 454", fakeConfig{authReply: "454 4.7.0 Temporary authentication failure"}, mail.ClassThrottled, 454, 2},
		{"AUTH answered with a challenge", fakeConfig{authReply: "334 "}, mail.ClassAuth, 0, 1},
		{"dropped at AUTH", fakeConfig{dropAt: "AUTH"}, mail.ClassNetwork, 0, 1},
		{"MAIL 550", fakeConfig{mailReply: "550 5.7.1 sender rejected"}, mail.ClassRejected, 550, 1},
		{"MAIL 452", fakeConfig{mailReply: "452 4.3.1 insufficient storage"}, mail.ClassThrottled, 452, 2},
		{"RCPT 550", fakeConfig{rcptReply: "550 5.1.1 unknown"}, mail.ClassRejected, 550, 1},
		{"RCPT 600 (not a defined class)", fakeConfig{rcptReply: "600 what"}, mail.ClassNetwork, 600, 1},
		{"DATA 554", fakeConfig{dataReply: "554 5.5.1 no valid recipients"}, mail.ClassRejected, 554, 1},
		{"DATA 451", fakeConfig{dataReply: "451 4.3.0 later"}, mail.ClassThrottled, 451, 2},
		{"end of data 554", fakeConfig{endReply: "554 Message rejected"}, mail.ClassRejected, 554, 1},
		{"end of data 451", fakeConfig{endReply: "451 4.4.2 timeout"}, mail.ClassThrottled, 451, 2},
		{"dropped at RCPT", fakeConfig{dropAt: "RCPT"}, mail.ClassNetwork, 0, 1},
		{"dropped at DATA", fakeConfig{dropAt: "DATA"}, mail.ClassNetwork, 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake(t, always(tt.cfg))
			s, _ := newSender(t, f, nil)
			_, _, err := sendWithin(context.Background(), t, 10*time.Second, s, validMessage())
			sendError(t, err, tt.class, tt.code)
			// Send returns only after its last attempt, so the count is final here.
			if n := f.accepted(); n != tt.conns {
				t.Fatalf("connections = %d, want %d", n, tt.conns)
			}
		})
	}
	t.Run("connection refused", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		if err := ln.Close(); err != nil {
			t.Fatal(err)
		}
		f := newFake(t, always(fakeConfig{}))
		s, _ := newSender(t, f, func(c *mail.Config) { c.Port = port })
		_, _, err = sendWithin(context.Background(), t, 10*time.Second, s, validMessage())
		sendError(t, err, mail.ClassNetwork, 0)
	})
	t.Run("context already done", func(t *testing.T) {
		f := newFake(t, always(fakeConfig{}))
		s, _ := newSender(t, f, nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, _, err := sendWithin(ctx, t, 10*time.Second, s, validMessage())
		sendError(t, err, mail.ClassTimeout, 0)
		if n := f.accepted(); n != 0 {
			t.Fatalf("connections = %d, want 0 (the context was done before the dial)", n)
		}
	})
}

// TestSend_ReadsTheMessageIDFromThe250Reply: what the receipt carries for each
// shape of the reply to the end of data. The id is never invented: a reply
// without one, or with one that fails messageID's shape or echo rule, yields "".
// The echo rows are the forms the security audit measured getting through a
// one-way rule, plus one row per entry of Send's echo list that only that entry
// can catch — each of those also asserts its fragment is NOT in the message the
// relay received, so the row exercises that entry and not the message's copy.
func TestSend_ReadsTheMessageIDFromThe250Reply(t *testing.T) {
	type env struct {
		value, pw, authArg, refID string
	}
	// value stands for the link's query value: 43 characters, drawn at run time.
	value := (rand.Text() + rand.Text())[:43]
	message := func() mail.Message {
		m := validMessage()
		m.Text = "Hello Maria,\n\nOpen this link on your phone:\nhttps://taptime.test/activate?code=" + value + "\n"
		m.HTML = `<p>Hello Maria,</p><p><a href="https://taptime.test/activate?code=` + value + `">Activate</a></p>`
		return m
	}
	fixed := func(r string) func(env, string) string { return func(env, string) string { return r } }
	tests := []struct {
		name         string
		edit         func(*mail.Message)
		reply        func(e env, received string) string
		want         string
		notInMessage func(e env) string // a fragment that must be absent from the message the relay received
	}{
		{name: "SES", reply: fixed("250 Ok 0107018b8f6d6c3e-0d8e6c1a-1111-2222-3333-444455556666-000000"), want: "0107018b8f6d6c3e-0d8e6c1a-1111-2222-3333-444455556666-000000"},
		{name: "SES, realistic id sharing no run with the message", reply: fixed("250 Ok 0107018fa1b2c3d4-5e6f7a8b-9c0d-4e1f-a2b3-c4d5e6f7a8b9-000000"), want: "0107018fa1b2c3d4-5e6f7a8b-9c0d-4e1f-a2b3-c4d5e6f7a8b9-000000"},
		{name: "Postfix", reply: fixed("250 2.0.0 Ok: queued as 4Xyz7Q2kLmN"), want: "4Xyz7Q2kLmN"},
		{name: "multi-line, last line counts", reply: fixed("250-first line\n250 Ok q-77"), want: "q-77"},
		{name: "no id", reply: fixed("250 Ok")},
		{name: "no id, enhanced status", reply: fixed("250 2.0.0 Ok")},
		{name: "an address", reply: fixed("250 Ok queued for victim@example.test")},
		{name: "a URL", reply: fixed("250 Ok https://taptime.test/x")},
		{name: "129 characters", reply: fixed("250 Ok " + strings.Repeat("a", 129))},
		{name: "a word of the message", reply: fixed("250 Ok Maria")},
		{name: "the recipient's local part", reply: fixed("250 Ok maria.borg")},
		{name: "the recipient's local part with a suffix", reply: fixed("250 Ok maria.borg-1")},
		{name: "the link value with a prefix", reply: func(e env, _ string) string { return "250 Ok x" + e.value }},
		{name: "the link value with a dotted prefix", reply: func(e env, _ string) string { return "250 Ok q." + e.value }},
		{name: "the link value with an SES-like suffix", reply: func(e env, _ string) string { return "250 Ok " + e.value + "-000000" }},
		{name: "the link value with a dotted suffix", reply: func(e env, _ string) string { return "250 Ok " + e.value + ".1" }},
		{name: "the password", reply: func(e env, _ string) string { return "250 Ok " + e.pw },
			notInMessage: func(e env) string { return e.pw }},
		{name: "the password with a prefix", reply: func(e env, _ string) string { return "250 Ok x" + e.pw }},
		{name: "the AUTH argument verbatim", reply: func(e env, _ string) string { return "250 Ok " + e.authArg }},
		{name: "a run of the AUTH argument", reply: func(e env, _ string) string { return "250 Ok q." + idRun(e.authArg) },
			notInMessage: func(e env) string { return idRun(e.authArg) }},
		{name: "the username as a prefix", reply: fixed("250 Ok smtp-user-0001"),
			notInMessage: func(env) string { return "smtp-user" }},
		{name: "a fragment only on the wire (the MIME boundary)",
			reply: func(_ env, received string) string { return "250 Ok b." + boundaryOf(received)[:20] }},
		{name: "the link value split by a quoted-printable soft break, in Text",
			edit: func(m *mail.Message) {
				m.Text = strings.Repeat("x", 70) + value + "\n"
				m.HTML = "<p>no link in this part</p>"
			},
			reply:        func(e env, _ string) string { return "250 Ok " + e.value[2:11] },
			notInMessage: func(e env) string { return e.value[2:11] }},
		{name: "the link value split by a quoted-printable soft break, in HTML",
			edit: func(m *mail.Message) {
				m.Text = "no link in this part"
				m.HTML = strings.Repeat("y", 70) + value
			},
			reply:        func(e env, _ string) string { return "250 Ok " + e.value[2:11] },
			notInMessage: func(e env) string { return e.value[2:11] }},
		{name: "a subject fragment changed by RFC 2047",
			edit:         func(m *mail.Message) { m.Subject = "Ħ ready_to_go_now" },
			reply:        fixed("250 Ok ready_to_go"),
			notInMessage: func(env) string { return "ready_to_go" }},
		{name: "the Ref, which travels nowhere",
			reply:        func(e env, _ string) string { return "250 Ok " + e.refID },
			notInMessage: func(e env) string { return e.refID }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := message()
			if tt.edit != nil {
				tt.edit(&m)
			}
			pw := rand.Text()
			e := env{value: value, pw: pw, refID: strings.SplitN(m.Ref, ":", 2)[1],
				authArg: base64.StdEncoding.EncodeToString([]byte("\x00smtp-user\x00" + pw))}
			f := newFake(t, always(fakeConfig{endReplyFn: func(received string) string { return tt.reply(e, received) }}))
			s, err := mail.New(f.config(pw))
			if err != nil {
				t.Fatal(err)
			}
			r, _, err := sendWithin(context.Background(), t, 10*time.Second, s, m)
			if err != nil {
				t.Fatalf("Send: %v", err)
			}
			if r.MessageID != tt.want {
				t.Fatalf("MessageID = %q, want %q", r.MessageID, tt.want)
			}
			if tt.notInMessage != nil {
				if frag := tt.notInMessage(e); strings.Contains(f.session(0).message, frag) {
					t.Fatalf("control: %q IS in the message the relay received, so this row does not isolate its entry", frag)
				}
			}
		})
	}
}

// idRun is the first run of 9 or more [A-Za-z0-9._-] characters in s.
func idRun(s string) string {
	start := -1
	for i := 0; i <= len(s); i++ {
		in := i < len(s) && (s[i] >= 'a' && s[i] <= 'z' || s[i] >= 'A' && s[i] <= 'Z' ||
			s[i] >= '0' && s[i] <= '9' || s[i] == '.' || s[i] == '_' || s[i] == '-')
		switch {
		case in && start < 0:
			start = i
		case !in && start >= 0:
			if i-start >= 9 {
				return s[start:i]
			}
			start = -1
		}
	}
	panic("no run of 9 id characters in the AUTH argument; rerun (the password is random)")
}

// boundaryOf is the MIME boundary of a received message.
func boundaryOf(received string) string {
	msg, err := netmail.ReadMessage(strings.NewReader(received))
	if err != nil {
		panic(err)
	}
	_, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		panic(err)
	}
	return params["boundary"]
}

// TestNew_RefusesAnInvalidConfig: each invalid field is refused by name, and the
// error quotes no value — not the address, not the host, not the password.
func TestNew_RefusesAnInvalidConfig(t *testing.T) {
	pw := rand.Text()
	base := func() mail.Config {
		return mail.Config{Host: "smtp.example.test", Port: 587, Username: mail.NewCredential("smtp-user"),
			Password: mail.NewCredential(pw), From: "Taptime <no-reply@taptime.test>"}
	}
	if _, err := mail.New(base()); err != nil {
		t.Fatalf("control: the base config is refused: %v", err)
	}
	tests := []struct {
		name string
		edit func(*mail.Config)
	}{
		{"Host empty", func(c *mail.Config) { c.Host = "" }},
		{"Host with a space", func(c *mail.Config) { c.Host = "smtp example.test" }},
		{"Port 0", func(c *mail.Config) { c.Port = 0 }},
		{"Port 65536", func(c *mail.Config) { c.Port = 65536 }},
		{"Username zero", func(c *mail.Config) { c.Username = mail.Credential{} }},
		{"Username empty", func(c *mail.Config) { c.Username = mail.NewCredential("") }},
		{"Username with NUL", func(c *mail.Config) { c.Username = mail.NewCredential("a\x00b") }},
		{"Password zero", func(c *mail.Config) { c.Password = mail.Credential{} }},
		{"Password empty", func(c *mail.Config) { c.Password = mail.NewCredential("") }},
		{"Password with NUL", func(c *mail.Config) { c.Password = mail.NewCredential(pw + "\x00") }},
		{"From empty", func(c *mail.Config) { c.From = "" }},
		{"From with CRLF", func(c *mail.Config) { c.From = "Taptime <no-reply@taptime.test>\r\nBcc: x@example.test" }},
		{"From two addresses", func(c *mail.Config) { c.From = "a@taptime.test, b@taptime.test" }},
		{"From not ASCII", func(c *mail.Config) { c.From = "Taptime <ħ@taptime.test>" }},
		{"From name shaped like an encoded word", func(c *mail.Config) { c.From = "=?utf-8?q?a=0D=0Ab?= <no-reply@taptime.test>" }},
		{"ReplyTo with LF", func(c *mail.Config) { c.ReplyTo = "x@taptime.test\nBcc: y@example.test" }},
		{"ReplyTo with a display name", func(c *mail.Config) { c.ReplyTo = "Taptime Support <support@taptime.test>" }},
		{"ReplyTo in angle brackets", func(c *mail.Config) { c.ReplyTo = "<support@taptime.test>" }},
		{"ReplyTo shaped like an encoded word", func(c *mail.Config) { c.ReplyTo = "=?utf-8?q?a?=@taptime.test" }},
		{"Timeout negative", func(c *mail.Config) { c.Timeout = -time.Second }},
		{"RetryDelay negative", func(c *mail.Config) { c.RetryDelay = -time.Second }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := base()
			tt.edit(&c)
			s, err := mail.New(c)
			if err == nil || s != nil {
				t.Fatalf("New accepted it")
			}
			for _, v := range []string{pw, "smtp.example.test", "taptime.test", "example.test", "smtp-user"} {
				if strings.Contains(err.Error(), v) {
					t.Fatalf("the error %q quotes a configured value", err.Error())
				}
			}
		})
	}
	// A display name is allowed in From (not in ReplyTo, which is held to the
	// recipient rule), and a non-ASCII one is encoded by net/mail, not refused.
	c := base()
	c.From = "Taptime Malta ħ <no-reply@taptime.test>"
	c.ReplyTo = "support@taptime.test"
	if _, err := mail.New(c); err != nil {
		t.Fatalf("a valid From/ReplyTo is refused: %v", err)
	}
}

// TestSend_ANilOrZeroSMTPIsNotConfigured: only New builds a usable sender.
func TestSend_ANilOrZeroSMTPIsNotConfigured(t *testing.T) {
	var nilSMTP *mail.SMTP
	for _, s := range []*mail.SMTP{nilSMTP, {}} {
		if _, err := s.Send(context.Background(), validMessage()); !errors.Is(err, mail.ErrNotConfigured) {
			t.Fatalf("Send on an unbuilt SMTP = %v", err)
		}
	}
}
