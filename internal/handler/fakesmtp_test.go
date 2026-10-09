package handler

// fakesmtp_test.go — an in-process SMTP relay on 127.0.0.1 for the reset e-mail
// channel's tests (M10 EM-5). It is internal/mail's test fake cut down to what this
// package needs: the real transport (mail.SMTP) talks to it over real TCP with real
// STARTTLS, so the channel is measured end to end. Its key and self-signed
// certificate are generated in memory on every run; no key or certificate FILE
// exists in the repository. It is NOT a mail service: nothing is added to the
// repository's compose or deploy files (EM-K9).
//
// It records what a relay would see — the envelope and the message as received —
// and can misbehave on purpose: reply with any code, delay the reply to the end of
// data, or stop answering at the greeting or inside the TLS handshake.

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"io"
	"log/slog"
	"math/big"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	netmail "net/mail"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/mail"
)

// relayMessageID is the fake's default id in the reply to the end of data: the shape
// of an SES id, sharing no 8-character run with anything the tests send.
const relayMessageID = "0107018fake0000a1-0000b2c3-d4e5-4f60-8a9b-0c1d2e3f4a5b-000000"

// relayScript scripts every connection. An empty reply means the positive default.
// hang is "greeting" (never greet) or "handshake" (answer STARTTLS, then never
// complete the handshake).
type relayScript struct {
	noSTARTTLS bool
	authReply  string
	rcptReply  string
	endReply   string                      // the reply to the end of data
	endReplyFn func(message string) string // overrides endReply; sees the message
	endDelay   time.Duration               // waited before the reply to the end of data
	hang       string
}

type relaySession struct {
	done     chan struct{}
	rcptTo   []string
	mailFrom string
	message  string // un-stuffed
	authed   bool
}

type fakeRelay struct {
	t      *testing.T
	ln     net.Listener
	cert   tls.Certificate
	pool   *x509.CertPool
	script relayScript

	mu       sync.Mutex
	conns    []net.Conn
	sessions []*relaySession
	wg       sync.WaitGroup
}

func newFakeRelay(t *testing.T, script relayScript) *fakeRelay {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "fake relay"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(parsed)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeRelay{t: t, ln: ln, cert: tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool: pool, script: script}
	f.wg.Add(1)
	go f.accept()
	t.Cleanup(func() {
		_ = ln.Close()
		f.mu.Lock()
		for _, c := range f.conns {
			_ = c.Close()
		}
		f.mu.Unlock()
		f.wg.Wait()
	})
	return f
}

// relayCredentials are drawn at run time: no credential literal is in the
// repository, and a leak scan can search the log for the exact values.
func relayCredentials(t *testing.T) (user, pass string) {
	t.Helper()
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString(b)
	return "usr" + enc[:13], "pw" + enc[13:]
}

// sender is a real mail.SMTP for this relay, trusting its certificate.
func (f *fakeRelay) sender(t *testing.T, user, pass string, timeout time.Duration) *mail.SMTP {
	t.Helper()
	s, err := mail.New(mail.Config{
		Host:       "127.0.0.1",
		Port:       f.ln.Addr().(*net.TCPAddr).Port,
		Username:   mail.NewCredential(user),
		Password:   mail.NewCredential(pass),
		From:       "Taptime <no-reply@taptime.test>",
		RootCAs:    f.pool,
		Timeout:    timeout,
		RetryDelay: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("mail.New: %v", err)
	}
	return s
}

// breaker is sender behind a fresh process-wide breaker (M10 EM-7A), which is what
// NewEmailResetChannel takes: the shape cmd/tappa builds, with the real clock and a
// discarded log. A test of the breaker's own behaviour builds its own.
func (f *fakeRelay) breaker(t *testing.T, user, pass string, timeout time.Duration) *mail.Breaker {
	t.Helper()
	return newTestBreaker(t, f.sender(t, user, pass, timeout))
}

// newTestBreaker wraps s in a breaker on the real clock with a discarded log.
func newTestBreaker(t *testing.T, s *mail.SMTP) *mail.Breaker {
	t.Helper()
	b, err := mail.NewBreaker(s, mail.BreakerConfig{Log: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatalf("mail.NewBreaker: %v", err)
	}
	return b
}

func (f *fakeRelay) accept() {
	defer f.wg.Done()
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		s := &relaySession{done: make(chan struct{})}
		f.mu.Lock()
		f.conns = append(f.conns, conn)
		f.sessions = append(f.sessions, s)
		f.mu.Unlock()
		f.wg.Add(1)
		go func() {
			defer f.wg.Done()
			defer close(s.done)
			defer conn.Close()
			f.serve(conn, s)
		}()
	}
}

// waitAccepted fails the test unless the relay has accepted at least n connections
// within five seconds — a test uses it to know a send is IN the relay.
func (f *fakeRelay) waitAccepted(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		got := len(f.sessions)
		f.mu.Unlock()
		if got >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the relay never accepted %d connection(s)", n)
}

// completed returns the sessions that have ended, so their records are race-free.
func (f *fakeRelay) completed() []*relaySession {
	f.mu.Lock()
	all := append([]*relaySession(nil), f.sessions...)
	f.mu.Unlock()
	var out []*relaySession
	for _, s := range all {
		select {
		case <-s.done:
			out = append(out, s)
		case <-time.After(5 * time.Second):
			f.t.Fatal("a relay session did not end")
		}
	}
	return out
}

func (f *fakeRelay) serve(raw net.Conn, s *relaySession) {
	sc := f.script
	hang := func() { _, _ = io.Copy(io.Discard, raw) }
	if sc.hang == "greeting" {
		hang()
		return
	}
	tp := textproto.NewConn(raw)
	write := func(lines ...string) bool {
		for _, l := range lines {
			if err := tp.PrintfLine("%s", l); err != nil {
				return false
			}
		}
		return true
	}
	or := func(got, def string) string {
		if got == "" {
			return def
		}
		return got
	}
	note := func(fn func()) {
		f.mu.Lock()
		fn()
		f.mu.Unlock()
	}
	if !write("220 fake.test ESMTP") {
		return
	}
	secure := false
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		fields := strings.Fields(line)
		verb := ""
		if len(fields) > 0 {
			verb = strings.ToUpper(fields[0])
		}
		ok := true
		switch verb {
		case "EHLO":
			lines := []string{"250-fake.test"}
			if !secure && !sc.noSTARTTLS {
				lines = append(lines, "250-STARTTLS")
			}
			ok = write(append(lines, "250 AUTH PLAIN LOGIN")...)
		case "STARTTLS":
			if !write("220 2.0.0 ready to start TLS") {
				return
			}
			if sc.hang == "handshake" {
				hang()
				return
			}
			tc := tls.Server(raw, &tls.Config{Certificates: []tls.Certificate{f.cert}, MinVersion: tls.VersionTLS12})
			if err := tc.Handshake(); err != nil {
				return
			}
			secure = true
			tp = textproto.NewConn(tc)
		case "AUTH":
			note(func() { s.authed = secure })
			ok = write(or(sc.authReply, "235 2.7.0 Authentication successful"))
		case "MAIL":
			note(func() { s.mailFrom = line })
			ok = write("250 2.1.0 Ok")
		case "RCPT":
			note(func() { s.rcptTo = append(s.rcptTo, line) })
			ok = write(or(sc.rcptReply, "250 2.1.5 Ok"))
		case "DATA":
			if !write("354 End data with <CR><LF>.<CR><LF>") {
				return
			}
			msg, ok2 := readData(tp.R)
			if !ok2 {
				return
			}
			note(func() { s.message = msg })
			if sc.endDelay > 0 {
				time.Sleep(sc.endDelay)
			}
			reply := or(sc.endReply, "250 Ok "+relayMessageID)
			if sc.endReplyFn != nil {
				reply = sc.endReplyFn(msg)
			}
			ok = write(strings.Split(reply, "\n")...)
		case "QUIT":
			write("221 2.0.0 Bye")
			return
		default:
			ok = write("500 5.5.2 unrecognised command")
		}
		if !ok {
			return
		}
	}
}

// readData reads DATA lines up to the terminating ".\r\n", un-stuffed.
func readData(r *bufio.Reader) (string, bool) {
	var msg strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return "", false
		}
		if line == ".\r\n" {
			return msg.String(), true
		}
		msg.WriteString(strings.TrimPrefix(line, "."))
	}
}

// relayedMessage is one received e-mail, parsed: its headers and its two parts
// decoded from quoted-printable.
type relayedMessage struct {
	header     netmail.Header
	text, html string
}

func parseRelayed(t *testing.T, raw string) relayedMessage {
	t.Helper()
	m, err := netmail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("the relayed message does not parse: %v", err)
	}
	mediaType, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/alternative" {
		t.Fatalf("Content-Type %q (%v), want multipart/alternative", m.Header.Get("Content-Type"), err)
	}
	out := relayedMessage{header: m.Header}
	mr := multipart.NewReader(m.Body, params["boundary"])
	for {
		p, err := mr.NextRawPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("multipart: %v", err)
		}
		body, err := io.ReadAll(quotedprintable.NewReader(p))
		if err != nil {
			t.Fatalf("quoted-printable: %v", err)
		}
		switch ct := p.Header.Get("Content-Type"); {
		case strings.HasPrefix(ct, "text/plain"):
			out.text = string(body)
		case strings.HasPrefix(ct, "text/html"):
			out.html = string(body)
		}
	}
	if out.text == "" || out.html == "" {
		t.Fatalf("the relayed message lacks a part (text %d bytes, html %d bytes)", len(out.text), len(out.html))
	}
	return out
}
