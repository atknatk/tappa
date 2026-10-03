package mail_test

// fakesmtp_test.go — an in-process SMTP server on 127.0.0.1 for this package's
// tests. Its TLS keys and self-signed certificates are generated in memory on
// every run (crypto/ecdsa + crypto/x509): no key or certificate FILE exists in
// the repository, and none is written anywhere.
//
// It records what a real relay would see — every command line in full and in
// order, every AUTH (before and after TLS), the envelope, the DATA lines exactly
// as they arrived on the wire (still dot-stuffed) and un-stuffed — so the tests
// assert on the server's side of the conversation, not on the client's account
// of it. It can also misbehave on purpose: stop answering, drop or reset the
// connection, or flood the client with reply bytes.

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"io"
	"math/big"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/mail"
)

// defaultMessageID is what the fake's 250 reply to the end of DATA names when a
// test does not choose: the shape of an SES id. It shares no 8-character run
// with any message the tests send.
const defaultMessageID = "0107018fake0000a1-0000b2c3-d4e5-4f60-8a9b-0c1d2e3f4a5b-000000"

// fakeConfig scripts one connection. An empty reply field means the default
// positive reply. hang names the point at which the server stops answering and
// only reads (until the client closes): "greeting", "ehlo", "handshake" (after
// its 220 to STARTTLS), "auth", "mail", "rcpt", "data", "end" (after the
// message's terminating dot), "quit". Two more end the connection instead:
// "end-drop" closes it after the terminating dot without a reply, "data-rst"
// resets it (RST) after the 354 and one line of data.
type fakeConfig struct {
	greeting      string
	noSTARTTLS    bool
	starttlsReply string
	tlsMaxVersion uint16
	cert          string // "" (127.0.0.1 and localhost), "ip", "dns" (localhost), "other" (other.test)
	authReply     string
	mailReply     string
	rcptReply     string
	dataReply     string
	endReply      string                      // may hold several lines separated by "\n"
	endReplyFn    func(message string) string // overrides endReply; sees the received message
	dropAt        string                      // the verb on which the server closes without replying
	hang          string
	// floodAt names where the server writes flood (raw bytes, built by the test
	// before it measures) and then only reads: "greeting" (instead of the
	// greeting), "ehlo" (as the pre-TLS EHLO reply), "end" (as the reply to the
	// end of data, through TLS).
	floodAt string
	flood   []byte
	// ehloPad makes the pre-TLS EHLO reply exactly this many bytes (CRLFs
	// included) of VALID reply — the usual lines plus padding extension lines —
	// and endPad puts this many bytes of valid continuation lines in front of the
	// final line of the reply to the end of data. Both leave the conversation
	// working; they only spend the client's reply budget.
	ehloPad int
	endPad  int
}

// padLines returns valid continuation lines ("<code>-PAD aaa…") totalling exactly
// n bytes with their CRLFs (n >= 12 or 0), each at most 1002 bytes.
func padLines(code string, n int) []string {
	if n <= 0 {
		return nil
	}
	count := (n + 1001) / 1002
	base, extra := n/count, n%count
	var lines []string
	for i := range count {
		size := base
		if i < extra {
			size++
		}
		prefix := code + "-PAD "
		lines = append(lines, prefix+strings.Repeat("a", size-2-len(prefix)))
	}
	return lines
}

// lineBytes is the wire size of lines written with CRLF.
func lineBytes(lines []string) int {
	n := 0
	for _, l := range lines {
		n += len(l) + 2
	}
	return n
}

type session struct {
	done      chan struct{}
	verbs     []string // first word of every command, upper-cased; "TLS" when a handshake completed
	lines     []string // every command line in full, in order, before and after TLS
	authClear int      // AUTH commands received before TLS
	authTLS   int      // AUTH commands received after TLS
	authPlain string   // the decoded PLAIN response of the last AUTH (identity NUL user NUL value)
	mailFrom  string
	rcptTo    []string
	wire      []string // DATA lines as received, CRLF included, the terminating dot excluded
	message   string   // the same lines un-stuffed and joined
	replies   []string // every reply line the server wrote
}

type fakeSMTP struct {
	t     *testing.T
	ln    net.Listener
	certs map[string]tls.Certificate
	pool  *x509.CertPool         // trusts every certificate in certs
	cfg   func(n int) fakeConfig // by 1-based connection number

	mu       sync.Mutex
	conns    []net.Conn
	sessions []*session
	wg       sync.WaitGroup
}

// selfSigned makes one self-signed certificate for the given names.
func selfSigned(t *testing.T, ips []net.IP, dns []string) (tls.Certificate, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "fake smtp relay"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  ips,
		DNSNames:     dns,
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
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, parsed
}

func newFake(t *testing.T, cfg func(n int) fakeConfig) *fakeSMTP {
	t.Helper()
	loop := []net.IP{net.IPv4(127, 0, 0, 1)}
	pool := x509.NewCertPool()
	certs := map[string]tls.Certificate{}
	for name, c := range map[string]struct {
		ips []net.IP
		dns []string
	}{
		"":      {loop, []string{"localhost"}},
		"ip":    {loop, nil},
		"dns":   {nil, []string{"localhost"}},
		"other": {nil, []string{"other.test"}},
	} {
		cert, parsed := selfSigned(t, c.ips, c.dns)
		certs[name] = cert
		pool.AddCert(parsed)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMTP{t: t, ln: ln, certs: certs, pool: pool, cfg: cfg}
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

// always is a cfg for a server that behaves the same on every connection.
func always(c fakeConfig) func(int) fakeConfig { return func(int) fakeConfig { return c } }

func (f *fakeSMTP) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

// config is a valid client Config for this server: its certificates trusted, a
// password drawn at run time (no literal), short pauses.
func (f *fakeSMTP) config(pw string) mail.Config {
	return mail.Config{
		Host:       "127.0.0.1",
		Port:       f.port(),
		Username:   mail.NewCredential("smtp-user"),
		Password:   mail.NewCredential(pw),
		From:       "Taptime <no-reply@taptime.test>",
		RootCAs:    f.pool,
		Timeout:    5 * time.Second,
		RetryDelay: 20 * time.Millisecond,
	}
}

func (f *fakeSMTP) accept() {
	defer f.wg.Done()
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		s := &session{done: make(chan struct{})}
		f.mu.Lock()
		f.conns = append(f.conns, conn)
		f.sessions = append(f.sessions, s)
		n := len(f.sessions)
		f.mu.Unlock()
		f.wg.Add(1)
		go func() {
			defer f.wg.Done()
			defer close(s.done)
			defer conn.Close()
			f.serve(f.cfg(n), conn, s)
		}()
	}
}

// accepted is the number of connections the server has accepted so far.
func (f *fakeSMTP) accepted() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sessions)
}

// session returns connection i (0-based) after its handler has returned, so its
// record is complete and race-free to read.
func (f *fakeSMTP) session(i int) *session {
	f.t.Helper()
	f.mu.Lock()
	if i >= len(f.sessions) {
		f.mu.Unlock()
		f.t.Fatalf("session %d: the server accepted only %d connection(s)", i, f.accepted())
	}
	s := f.sessions[i]
	f.mu.Unlock()
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		f.t.Fatalf("session %d did not end", i)
	}
	return s
}

func (f *fakeSMTP) serve(c fakeConfig, raw net.Conn, s *session) {
	hang := func() { _, _ = io.Copy(io.Discard, raw) }
	tp := textproto.NewConn(raw)
	// flood writes the prebuilt bytes straight through the connection's writer
	// (no formatting, so the server allocates nothing in proportion to them),
	// then only reads until the client goes.
	flood := func() {
		if _, err := tp.W.Write(c.flood); err == nil {
			_ = tp.W.Flush()
		}
		hang()
	}
	if c.hang == "greeting" {
		hang()
		return
	}
	if c.floodAt == "greeting" {
		flood()
		return
	}
	write := func(lines ...string) bool {
		for _, l := range lines {
			f.mu.Lock()
			s.replies = append(s.replies, l)
			f.mu.Unlock()
			if err := tp.PrintfLine("%s", l); err != nil {
				return false
			}
		}
		return true
	}
	reply := func(got, def string) bool {
		if got == "" {
			got = def
		}
		return write(strings.Split(got, "\n")...)
	}
	note := func(fn func()) {
		f.mu.Lock()
		fn()
		f.mu.Unlock()
	}
	if !reply(c.greeting, "220 fake.test ESMTP") {
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
		note(func() {
			s.verbs = append(s.verbs, verb)
			s.lines = append(s.lines, line)
		})
		if c.dropAt != "" && verb == c.dropAt {
			return
		}
		ok := true
		switch verb {
		case "EHLO":
			if c.hang == "ehlo" {
				hang()
				return
			}
			if c.floodAt == "ehlo" && !secure {
				flood()
				return
			}
			lines := []string{"250-fake.test"}
			if !secure && !c.noSTARTTLS {
				lines = append(lines, "250-STARTTLS")
			}
			// AUTH is advertised before TLS too: that is what a downgrading
			// man-in-the-middle would say.
			lines = append(lines, "250 AUTH PLAIN LOGIN")
			if !secure && c.ehloPad > 0 {
				pad := padLines("250", c.ehloPad-lineBytes(lines))
				lines = append(append([]string{lines[0]}, pad...), lines[1:]...)
			}
			ok = write(lines...)
		case "HELO":
			ok = write("250 fake.test")
		case "STARTTLS":
			if c.starttlsReply != "" {
				ok = write(c.starttlsReply)
				break
			}
			if !write("220 2.0.0 ready to start TLS") {
				return
			}
			if c.hang == "handshake" {
				hang()
				return
			}
			tc := tls.Server(raw, &tls.Config{
				Certificates: []tls.Certificate{f.certs[c.cert]},
				MinVersion:   tls.VersionTLS10,
				MaxVersion:   c.tlsMaxVersion,
			})
			if err := tc.Handshake(); err != nil {
				return
			}
			secure = true
			tp = textproto.NewConn(tc)
			note(func() { s.verbs = append(s.verbs, "TLS") })
		case "AUTH":
			var plain []byte
			if len(fields) == 3 {
				plain, _ = base64.StdEncoding.DecodeString(fields[2])
			}
			note(func() {
				if secure {
					s.authTLS++
				} else {
					s.authClear++
				}
				s.authPlain = string(plain)
			})
			if c.hang == "auth" {
				hang()
				return
			}
			ok = reply(c.authReply, "235 2.7.0 Authentication successful")
		case "*":
			ok = write("501 5.7.0 authentication cancelled")
		case "MAIL":
			note(func() { s.mailFrom = line })
			if c.hang == "mail" {
				hang()
				return
			}
			ok = reply(c.mailReply, "250 2.1.0 Ok")
		case "RCPT":
			note(func() { s.rcptTo = append(s.rcptTo, line) })
			if c.hang == "rcpt" {
				hang()
				return
			}
			ok = reply(c.rcptReply, "250 2.1.5 Ok")
		case "DATA":
			if c.hang == "data" {
				hang()
				return
			}
			if c.dataReply != "" {
				ok = write(c.dataReply)
				break
			}
			if !write("354 End data with <CR><LF>.<CR><LF>") {
				return
			}
			if c.hang == "data-rst" {
				_, _ = tp.R.ReadString('\n')
				if tcp, isTCP := raw.(*net.TCPConn); isTCP {
					_ = tcp.SetLinger(0) // the deferred Close then sends RST
				}
				return
			}
			if !f.readData(tp.R, s) {
				return
			}
			switch {
			case c.hang == "end":
				hang()
				return
			case c.hang == "end-drop":
				return
			case c.floodAt == "end":
				flood()
				return
			case c.endReplyFn != nil:
				f.mu.Lock()
				msg := s.message
				f.mu.Unlock()
				ok = reply(c.endReplyFn(msg), "")
			case c.endPad > 0:
				ok = write(append(padLines("250", c.endPad), "250 Ok "+defaultMessageID)...)
			default:
				ok = reply(c.endReply, "250 Ok "+defaultMessageID)
			}
		case "QUIT":
			if c.hang == "quit" {
				hang()
				return
			}
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

// readData reads DATA lines up to the terminating ".\r\n", keeping the wire form
// and the un-stuffed form.
func (f *fakeSMTP) readData(r *bufio.Reader, s *session) bool {
	var wire []string
	var msg strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return false
		}
		if line == ".\r\n" {
			break
		}
		wire = append(wire, line)
		msg.WriteString(strings.TrimPrefix(line, "."))
	}
	f.mu.Lock()
	s.wire, s.message = wire, msg.String()
	f.mu.Unlock()
	return true
}

// sendWithin runs Send and fails the test if it has not returned within limit,
// so a mutation that removes a deadline turns a test red instead of hanging it.
func sendWithin(ctx context.Context, t *testing.T, limit time.Duration, s *mail.SMTP, m mail.Message) (mail.Receipt, time.Duration, error) {
	t.Helper()
	type result struct {
		r   mail.Receipt
		err error
	}
	start := time.Now()
	ch := make(chan result, 1)
	go func() {
		r, err := s.Send(ctx, m)
		ch <- result{r, err}
	}()
	select {
	case res := <-ch:
		return res.r, time.Since(start), res.err
	case <-time.After(limit):
		t.Fatalf("Send did not return within %v", limit)
		return mail.Receipt{}, 0, nil
	}
}
