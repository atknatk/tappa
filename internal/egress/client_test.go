package egress

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// client_test.go -- the client end to end, against httptest TLS servers on 127.0.0.1 and the
// in-process DNS of dns_test.go. Nothing reaches the network. 127.0.0.1 stands in for "a public
// address" through the gate's test-only allowance (testAllow) -- the only way to show a dial that
// SUCCEEDS -- and the gate's port is the test server's (the rule is still "this port and no other").

var loopback = netip.MustParsePrefix("127.0.0.1/32")

// recorder is a test TLS server that counts what reached it.
type recorder struct {
	srv  *httptest.Server
	hits atomic.Int64
	mu   sync.Mutex
	last http.Header
	body []byte
	// sni is the server name the last request's handshake carried (req.TLS.ServerName).
	sni string
}

func newRecorder(t *testing.T, h http.HandlerFunc) *recorder {
	t.Helper()
	r := &recorder{}
	r.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.hits.Add(1)
		b, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.last, r.body = req.Header.Clone(), b
		if req.TLS != nil {
			r.sni = req.TLS.ServerName
		}
		r.mu.Unlock()
		if h != nil {
			h(w, req)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	// The server's own handshake complaints (the untrusted-certificate case) are not this test's output.
	r.srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	r.srv.StartTLS()
	t.Cleanup(r.srv.Close)
	return r
}

func portOf(t *testing.T, addr net.Addr) uint16 {
	t.Helper()
	_, p, err := net.SplitHostPort(addr.String())
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		t.Fatal(err)
	}
	return uint16(n)
}

// clientFor is a Client whose gate allows the loopback test server, dials its port, resolves through
// d and trusts the server's certificate.
func clientFor(t *testing.T, r *recorder, d *fakeDNS, timeout time.Duration, observe func(netip.AddrPort, bool)) *Client {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(r.srv.Certificate())
	g := &gate{port: portOf(t, r.srv.Listener.Addr()), testAllow: []netip.Prefix{loopback}, observe: observe}
	return build(g, d.resolver(), roots, timeout)
}

// to is the endpoint for host -- always through ParseEndpoint, never a hand-built value.
func to(t *testing.T, raw string) Endpoint {
	t.Helper()
	e, err := ParseEndpoint(raw)
	if err != nil {
		t.Fatalf("ParseEndpoint: %v", err)
	}
	return e
}

// staticDNS answers the given names, NXDOMAIN for every other.
func staticDNS(m map[string][]string) *fakeDNS {
	return newFakeDNS(func(name string, _ uint16, _ int) ([]netip.Addr, bool) {
		ss, ok := m[name]
		if !ok {
			return nil, true
		}
		var out []netip.Addr
		for _, s := range ss {
			out = append(out, netip.MustParseAddr(s))
		}
		return out, false
	})
}

// TestEgress_PostDeliversToAnAllowedAddress is the CONTROL every other test leans on: an allowed
// address is reached, the body and the caller's headers arrive, the TLS name the receiver saw is the
// URL's host, and the answer is a Result with no class.
//
// THE NAME VERIFIED IS THE URL'S HOST, and nothing else (API-12 low round). httptest's certificate
// names example.com, *.example.com and 127.0.0.1, so a handshake that verified any of those instead
// of the URL's host passed every other test. Two halves pin it: the receiver sees the URL's host as
// the handshake's server name, and an allowed name the certificate does NOT cover (hook.example.net,
// resolved to the same server) is tls with nothing delivered.
//
// MUTANTS (measured): cfg.ServerName = "example.com" -> the receiver sees "example.com" and
// hook.example.net is delivered -> red; cfg.ServerName = the dialled IP -> no server name reaches the
// receiver and hook.example.net is delivered (the certificate names 127.0.0.1) -> red.
func TestEgress_PostDeliversToAnAllowedAddress(t *testing.T) {
	r := newRecorder(t, nil)
	c := clientFor(t, r, staticDNS(map[string][]string{
		"hook.example.com": {"127.0.0.1"},
		"hook.example.net": {"127.0.0.1"},
	}), 5*time.Second, nil)
	res := c.Post(context.Background(), to(t, "https://hook.example.com/in"), []byte(`{"event":"ping"}`),
		http.Header{"Content-Type": {"application/json"}, "X-Test": {"yes"}})
	if !res.Delivered() || res.Status != http.StatusNoContent {
		t.Fatalf("CONTROL: %+v", res)
	}
	r.mu.Lock()
	sni := r.sni
	r.mu.Unlock()
	if r.hits.Load() != 1 || string(r.body) != `{"event":"ping"}` || r.last.Get("X-Test") != "yes" {
		t.Fatalf("CONTROL: hits %d body %q", r.hits.Load(), r.body)
	}
	if sni != "hook.example.com" {
		t.Fatalf("the receiver saw the TLS server name %q, want the URL's host hook.example.com", sni)
	}
	// The same server, under a name its certificate does not carry: the handshake refuses it before a
	// byte of the request is sent.
	if res := c.Post(context.Background(), to(t, "https://hook.example.net/in"), []byte(`{"event":"ping"}`), nil); res != (Result{Class: ClassTLS}) {
		t.Fatalf("a name the certificate does not cover: %+v, want tls", res)
	}
	if n := r.hits.Load(); n != 1 {
		t.Fatalf("a name the certificate does not cover reached the receiver: %d requests, want the control's 1", n)
	}
}

// TestEgress_DNSRebindingSecondAnswerRefused: the name answers a public (allowed) address on the first
// lookup and a private one on the second. The first delivery is made; the second is refused at DIAL
// time -- the gate looks at the connection, never at an earlier answer -- and the server sees one
// request. Then a loopback, a link-local metadata and an IPv6 ULA answer, each refused.
//
// MUTANT (measured): the ControlContext check made a no-op -> the second Post reaches the dialer with
// 10.0.0.1 and fails as "refused"/"timeout", never "blocked_address" -> red.
func TestEgress_DNSRebindingSecondAnswerRefused(t *testing.T) {
	r := newRecorder(t, nil)
	d := newFakeDNS(func(name string, qtype uint16, n int) ([]netip.Addr, bool) {
		if name != "rebind.example.com" {
			return nil, true
		}
		if qtype != typeA {
			return nil, false
		}
		if n == 0 {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, false
		}
		return []netip.Addr{netip.MustParseAddr("10.0.0.1")}, false
	})
	c := clientFor(t, r, d, 5*time.Second, nil)
	ep := to(t, "https://rebind.example.com/hook")
	if res := c.Post(context.Background(), ep, []byte("{}"), nil); !res.Delivered() {
		t.Fatalf("first answer (allowed): %+v", res)
	}
	if res := c.Post(context.Background(), ep, []byte("{}"), nil); res.Class != ClassBlocked || res.Status != 0 {
		t.Fatalf("second answer (10.0.0.1): %+v, want blocked_address", res)
	}
	if n := d.count("rebind.example.com", typeA); n != 2 {
		t.Fatalf("CONTROL: the name was looked up %d times, want 2 (one per dial)", n)
	}
	if r.hits.Load() != 1 {
		t.Fatalf("the server saw %d requests, want 1", r.hits.Load())
	}
	for _, addr := range []string{"127.0.0.2", "169.254.169.254", "fd00::1", "::ffff:10.0.0.1", "64:ff9b::a00:1"} {
		d2 := staticDNS(map[string][]string{"bad.example.com": {addr}})
		c2 := clientFor(t, r, d2, 5*time.Second, nil)
		if res := c2.Post(context.Background(), to(t, "https://bad.example.com/"), []byte("{}"), nil); res.Class != ClassBlocked {
			t.Errorf("an answer of %s: %+v, want blocked_address", addr, res)
		}
	}
	if r.hits.Load() != 1 {
		t.Fatalf("the server saw %d requests after the refused answers, want 1", r.hits.Load())
	}
}

// TestEgress_EveryAddressOfAnAnswerAndBothFamiliesAreChecked: the gate runs for EACH connection attempt
// -- each address of a multi-address answer and each family of a Happy Eyeballs race.
//   - A = {10.0.0.1, 192.168.0.1}: both addresses are tried and both refused -> blocked_address, and
//     the gate saw each of them;
//   - A = {10.0.0.1, 127.0.0.1}: delivered (an answer with a private address does not taint the
//     public one; the resolver's RFC 6724 sort decides which is tried first), and 10.0.0.1 is never
//     allowed;
//   - A = {10.0.0.2}, AAAA = {fd00::2}: both families are tried and both refused -> blocked_address,
//     and the gate saw one refusal of each family;
//   - A = {127.0.0.1}, AAAA = {fd00::3}: delivered, and if the race tried fd00::3 the gate refused it.
func TestEgress_EveryAddressOfAnAnswerAndBothFamiliesAreChecked(t *testing.T) {
	r := newRecorder(t, nil)
	var mu sync.Mutex
	var seen []string
	observe := func(ap netip.AddrPort, ok bool) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, fmt.Sprintf("%s=%v", ap.Addr(), ok))
	}
	reset := func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := seen
		seen = nil
		return out
	}
	d := staticDNS(map[string][]string{
		"multi.example.com": {"10.0.0.1", "192.168.0.1"},
		"mixed.example.com": {"10.0.0.1", "127.0.0.1"},
		"both.example.com":  {"10.0.0.2", "fd00::2"},
		"race.example.com":  {"127.0.0.1", "fd00::3"},
	})
	c := clientFor(t, r, d, 5*time.Second, observe)

	if res := c.Post(context.Background(), to(t, "https://multi.example.com/"), []byte("{}"), nil); res.Class != ClassBlocked {
		t.Fatalf("multi: %+v", res)
	}
	got := strings.Join(reset(), " ")
	if !strings.Contains(got, "10.0.0.1=false") || !strings.Contains(got, "192.168.0.1=false") || strings.Contains(got, "=true") {
		t.Fatalf("multi: the gate saw %q", got)
	}

	if res := c.Post(context.Background(), to(t, "https://mixed.example.com/"), []byte("{}"), nil); !res.Delivered() {
		t.Fatalf("mixed: %+v", res)
	}
	got = strings.Join(reset(), " ")
	if strings.Contains(got, "10.0.0.1=true") || !strings.Contains(got, "127.0.0.1=true") {
		t.Fatalf("mixed: the gate saw %q", got)
	}

	if res := c.Post(context.Background(), to(t, "https://both.example.com/"), []byte("{}"), nil); res.Class != ClassBlocked {
		t.Fatalf("both families private: %+v", res)
	}
	got = strings.Join(reset(), " ")
	if !strings.Contains(got, "10.0.0.2=false") || !strings.Contains(got, "fd00::2=false") || strings.Contains(got, "=true") {
		t.Fatalf("both families: the gate saw %q", got)
	}

	if res := c.Post(context.Background(), to(t, "https://race.example.com/"), []byte("{}"), nil); !res.Delivered() {
		t.Fatalf("race: %+v", res)
	}
	got = strings.Join(reset(), " ")
	if strings.Contains(got, "fd00::3=true") || !strings.Contains(got, "127.0.0.1=true") {
		t.Fatalf("race: the gate saw %q", got)
	}
}

// TestEgress_ProxyEnvironmentIgnored: with HTTPS_PROXY (and its relatives) pointing at a proxy, the
// delivery still goes DIRECT -- the proxy sees nothing and the server sees the request. A proxy would
// move the dial, and the gate with it, to the proxy's address (ADR 0028 §7).
//
// MUTANT (measured, this test run alone -- net/http reads the proxy variables once per process):
// Transport.Proxy = http.ProxyFromEnvironment -> the dial goes to the proxy's host, the delivery fails
// -> red.
func TestEgress_ProxyEnvironmentIgnored(t *testing.T) {
	var proxied atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxied.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(proxy.Close)
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY"} {
		t.Setenv(k, proxy.URL)
	}
	t.Setenv("NO_PROXY", "")
	r := newRecorder(t, nil)
	c := clientFor(t, r, staticDNS(map[string][]string{"hook.example.com": {"127.0.0.1"}}), 5*time.Second, nil)
	res := c.Post(context.Background(), to(t, "https://hook.example.com/"), []byte("{}"), nil)
	if !res.Delivered() || r.hits.Load() != 1 || proxied.Load() != 0 {
		t.Fatalf("with a proxy in the environment: %+v, server %d, proxy %d", res, r.hits.Load(), proxied.Load())
	}
	built, err := New(Options{DevelopmentWithoutBlockedList: true})
	if err != nil {
		t.Fatal(err)
	}
	if built.http.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("New's transport has a Proxy function")
	}
}

// TestEgress_RedirectNotFollowed: a 302 to another path of the same server, and one to an internal
// address, are the answer -- class http_4xx with the status -- and the Location is never requested.
func TestEgress_RedirectNotFollowed(t *testing.T) {
	var landed atomic.Int64
	r := newRecorder(t, func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/landed":
			landed.Add(1)
			w.WriteHeader(http.StatusOK)
		case "/internal":
			w.Header().Set("Location", "http://169.254.169.254/latest/meta-data/")
			w.WriteHeader(http.StatusTemporaryRedirect)
		default:
			w.Header().Set("Location", "/landed")
			w.WriteHeader(http.StatusFound)
		}
	})
	c := clientFor(t, r, staticDNS(map[string][]string{"hook.example.com": {"127.0.0.1"}}), 5*time.Second, nil)
	if res := c.Post(context.Background(), to(t, "https://hook.example.com/start"), []byte("{}"), nil); res != (Result{Class: ClassHTTP4xx, Status: http.StatusFound}) {
		t.Fatalf("302: %+v", res)
	}
	if res := c.Post(context.Background(), to(t, "https://hook.example.com/internal"), []byte("{}"), nil); res != (Result{Class: ClassHTTP4xx, Status: http.StatusTemporaryRedirect}) {
		t.Fatalf("307: %+v", res)
	}
	if landed.Load() != 0 || r.hits.Load() != 2 {
		t.Fatalf("the redirect was followed: landed %d, hits %d", landed.Load(), r.hits.Load())
	}
}

// TestEgress_TheExchangeIsBounded: a server that answers after the budget is a timeout (the test's
// budget is 300 ms; New's is TotalTimeout, 10 s); a response header past MaxResponseHeaderBytes is
// refused, never read whole; a body is not read (the server writes a little and then never finishes:
// Post returns at once).
func TestEgress_TheExchangeIsBounded(t *testing.T) {
	release := make(chan struct{})
	r := newRecorder(t, func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/slow":
			select {
			case <-time.After(3 * time.Second):
			case <-release:
			}
			w.WriteHeader(http.StatusOK)
		case "/header":
			w.Header().Set("X-Big", strings.Repeat("a", 2*MaxResponseHeaderBytes))
			w.WriteHeader(http.StatusOK)
		case "/body":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(strings.Repeat("b", 64<<10)))
			w.(http.Flusher).Flush()
			<-release
		}
	})
	t.Cleanup(func() { close(release) })
	d := staticDNS(map[string][]string{"hook.example.com": {"127.0.0.1"}})
	c := clientFor(t, r, d, 300*time.Millisecond, nil)
	start := time.Now()
	if res := c.Post(context.Background(), to(t, "https://hook.example.com/slow"), []byte("{}"), nil); res.Class != ClassTimeout {
		t.Fatalf("slow: %+v", res)
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("slow: Post took %v with a 300 ms budget", el)
	}
	c = clientFor(t, r, d, 5*time.Second, nil)
	if res := c.Post(context.Background(), to(t, "https://hook.example.com/header"), []byte("{}"), nil); res.Delivered() || res.Class != ClassRefused {
		t.Fatalf("a header past the bound: %+v", res)
	}
	start = time.Now()
	if res := c.Post(context.Background(), to(t, "https://hook.example.com/body"), []byte("{}"), nil); !res.Delivered() {
		t.Fatalf("body: %+v", res)
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("body: Post took %v -- it waited for a body it must not read", el)
	}
	if TotalTimeout != 10*time.Second {
		t.Fatalf("TotalTimeout is %v, ADR 0028 §7 says 10 s", TotalTimeout)
	}
	built, err := New(Options{DevelopmentWithoutBlockedList: true})
	if err != nil {
		t.Fatal(err)
	}
	tr := built.http.Transport.(*http.Transport)
	if built.timeout != TotalTimeout || built.http.Timeout != TotalTimeout || tr.MaxResponseHeaderBytes != MaxResponseHeaderBytes ||
		!tr.DisableKeepAlives || drainLimit != 1<<10 {
		t.Fatalf("New's budgets: timeout %v/%v header %d keepalive-off %v", built.timeout, built.http.Timeout,
			tr.MaxResponseHeaderBytes, tr.DisableKeepAlives)
	}
	if built.tls.InsecureSkipVerify || built.tls.RootCAs != nil {
		t.Fatal("New's TLS is not Go's default verification against the system roots")
	}
	if built.http.CheckRedirect == nil || !errors.Is(built.http.CheckRedirect(nil, nil), http.ErrUseLastResponse) {
		t.Fatal("New's client follows redirects")
	}
}

// heldReceiver takes each TCP connection, reads the ClientHello and never answers -- the receiver an
// owner can point a webhook at to hold a socket. It counts the connections the client has not closed
// and reports when each one was closed (its own 10 s read limit is the only other way out).
type heldReceiver struct {
	l      net.Listener
	open   atomic.Int64
	closed chan time.Time
}

func newHeldReceiver(t *testing.T) *heldReceiver {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	h := &heldReceiver{l: l, closed: make(chan time.Time, 1024)}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			h.open.Add(1)
			go func() {
				defer func() { _ = c.Close() }()
				_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
				buf := make([]byte, 4096)
				for {
					if _, err := c.Read(buf); err != nil {
						var ne net.Error
						if errors.As(err, &ne) && ne.Timeout() {
							return // the receiver gave up: the client still held it
						}
						h.open.Add(-1)
						h.closed <- time.Now()
						return
					}
				}
			}()
		}
	}()
	return h
}

func (h *heldReceiver) client(t *testing.T, timeout time.Duration) *Client {
	t.Helper()
	d := staticDNS(map[string][]string{"hook.example.com": {"127.0.0.1"}})
	return build(&gate{port: portOf(t, h.l.Addr()), testAllow: []netip.Prefix{loopback}}, d.resolver(), nil, timeout)
}

// waitFor polls cond for up to limit.
func waitFor(limit time.Duration, cond func() bool) bool {
	for end := time.Now().Add(limit); ; time.Sleep(10 * time.Millisecond) {
		if cond() {
			return true
		}
		if time.Now().After(end) {
			return false
		}
	}
}

// TestEgress_AHeldHandshakeIsClosedWithinTheBudget (API-12 third fix): a receiver that takes the
// ClientHello and never answers sees the CLIENT CLOSE THE CONNECTION within Post's budget -- not only
// Post returning. net/http dials with a ctx detached from the request and applies no
// TLSHandshakeTimeout to a DialTLSContext, so until exchangeContext the socket and a goroutine
// outlived every such Post (measured before the fix: 20 Posts -> 20 sockets open 60 s later; 200
// concurrent -> +200 goroutines). Measured here, on the receiver's side:
//   - one Post: the connection closes within the budget and a small margin;
//   - Post's ctx cancelled mid-handshake (the worker's Drain): it closes at once, long before the budget;
//   - 20 Posts one after another and 100 at once: no connection stays open and the goroutines return
//     to where they were.
//
// MUTANTS (measured, each red): the dial's own (detached) ctx in place of Post's; the handshake under
// context.Background(); Post's ctx not carried to the dial (the fallback bounds the time but not the
// cancellation); not carried and a fallback with no limit.
func TestEgress_AHeldHandshakeIsClosedWithinTheBudget(t *testing.T) {
	const margin = 300 * time.Millisecond
	h := newHeldReceiver(t)
	base := runtime.NumGoroutine()

	const budget = 400 * time.Millisecond
	c := h.client(t, budget)
	start := time.Now()
	if res := c.Post(context.Background(), to(t, "https://hook.example.com/in"), []byte("{}"), nil); res != (Result{Class: ClassTimeout}) {
		t.Fatalf("held handshake: %+v", res)
	}
	select {
	case at := <-h.closed:
		if el := at.Sub(start); el > budget+margin {
			t.Fatalf("held handshake: the receiver saw the close %v after Post began (budget %v)", el, budget)
		}
	case <-time.After(budget + 2*time.Second):
		t.Fatalf("held handshake: the connection is still open %v after a %v budget", time.Since(start).Round(time.Millisecond), budget)
	}

	c = h.client(t, 5*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	start = time.Now()
	time.AfterFunc(150*time.Millisecond, cancel)
	if res := c.Post(ctx, to(t, "https://hook.example.com/in"), []byte("{}"), nil); res.Delivered() {
		t.Fatalf("cancelled: %+v", res)
	}
	select {
	case at := <-h.closed:
		if el := at.Sub(start); el > 150*time.Millisecond+margin {
			t.Fatalf("cancelled: the receiver saw the close %v after Post began (cancelled at 150 ms, budget 5 s)", el)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled: Post's ctx ended, and the handshake under it did not")
	}

	c = h.client(t, 150*time.Millisecond)
	for i := 0; i < 20; i++ {
		if res := c.Post(context.Background(), to(t, "https://hook.example.com/in"), []byte("{}"), nil); res.Class != ClassTimeout {
			t.Fatalf("sequential %d: %+v", i, res)
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if res := c.Post(context.Background(), to(t, "https://hook.example.com/in"), []byte("{}"), nil); res.Class != ClassTimeout {
				t.Errorf("concurrent: %+v", res)
			}
		}()
	}
	wg.Wait()
	if !waitFor(time.Second, func() bool { return h.open.Load() == 0 }) {
		t.Fatalf("after 120 Posts: %d connections still open on the receiver", h.open.Load())
	}
	// The goroutines are the process's: the margin is for the runtime's and the test server's own.
	if !waitFor(2*time.Second, func() bool { return runtime.NumGoroutine() <= base+3 }) {
		t.Fatalf("after 120 Posts: %d goroutines, %d before", runtime.NumGoroutine(), base)
	}
}

// TestEgress_OnlyHTTP11: the receiver offers HTTP/2 (ALPN "h2" before "http/1.1") and the connection
// is HTTP/1.1 with no ALPN at all -- seen on the receiver's side. The CONTROL: a client that asks for
// HTTP/2 gets it from the same server. And statically: the transport holds no TLSClientConfig (net/http
// would append "h2" to it, and a pointer shared with the handshake would offer it), Protocols is
// HTTP/1.1 alone, ForceAttemptHTTP2 is off and the handshake's NextProtos is empty (ADR 0028 §7).
//
// MUTANTS (measured, each red): NextProtos ["h2"] or ["h2","http/1.1"] in the handshake's config;
// ForceAttemptHTTP2 true; Protocols removed with ForceAttemptHTTP2 true and the handshake's config
// shared as the TLSClientConfig.
func TestEgress_OnlyHTTP11(t *testing.T) {
	var proto atomic.Value
	r := &recorder{}
	r.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		proto.Store(fmt.Sprintf("%d %q", req.ProtoMajor, req.TLS.NegotiatedProtocol))
		w.WriteHeader(http.StatusNoContent)
	}))
	r.srv.EnableHTTP2 = true
	r.srv.TLS = &tls.Config{NextProtos: []string{"h2", "http/1.1"}}
	r.srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	r.srv.StartTLS()
	t.Cleanup(r.srv.Close)

	resp, err := r.srv.Client().Get(r.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got := proto.Load(); got != `2 "h2"` {
		t.Fatalf("CONTROL: a client that asks for HTTP/2 got %v -- the server does not offer it", got)
	}

	c := clientFor(t, r, staticDNS(map[string][]string{"hook.example.com": {"127.0.0.1"}}), 5*time.Second, nil)
	if res := c.Post(context.Background(), to(t, "https://hook.example.com/in"), []byte("{}"), nil); !res.Delivered() {
		t.Fatalf("an HTTP/2 receiver: %+v", res)
	}
	if got := proto.Load(); got != `1 ""` {
		t.Fatalf("the receiver saw %v, want HTTP/1.1 and no ALPN", got)
	}

	built, err := New(Options{DevelopmentWithoutBlockedList: true})
	if err != nil {
		t.Fatal(err)
	}
	tr := built.http.Transport.(*http.Transport)
	if tr.TLSClientConfig != nil || tr.ForceAttemptHTTP2 || tr.Protocols == nil || !tr.Protocols.HTTP1() ||
		tr.Protocols.HTTP2() || tr.Protocols.UnencryptedHTTP2() || len(built.tls.NextProtos) != 0 || tr.TLSNextProto != nil {
		t.Fatalf("New's transport is not HTTP/1.1 alone: TLSClientConfig %v force %v protocols %v next %v",
			tr.TLSClientConfig != nil, tr.ForceAttemptHTTP2, tr.Protocols, built.tls.NextProtos)
	}
}

// TestEgress_FailuresAreClassesAndCarryNoText: each failure is one of the six classes -- an untrusted
// certificate, a plain-HTTP server and every failed TLS conversation (an old version, a client
// certificate required, no common cipher suite) are tls; a name that does not resolve, a port nothing
// listens on and a connection dropped under the handshake are refused; a 4xx and a 5xx are their
// classes with the status -- and the Result, printed every
// way, names neither the host, the path, the address nor Go's error words.
func TestEgress_FailuresAreClassesAndCarryNoText(t *testing.T) {
	tlsSrv := newRecorder(t, nil)
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	t.Cleanup(plain.Close)
	gone, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gonePort := portOf(t, gone.Addr())
	_ = gone.Close()
	status := newRecorder(t, func(w http.ResponseWriter, req *http.Request) {
		code, _ := strconv.Atoi(strings.TrimPrefix(req.URL.Path, "/"))
		w.WriteHeader(code)
	})
	d := staticDNS(map[string][]string{"secret-host.example.com": {"127.0.0.1"}})
	untrusted := build(&gate{port: portOf(t, tlsSrv.srv.Listener.Addr()), testAllow: []netip.Prefix{loopback}}, d.resolver(), nil, 5*time.Second)
	plainC := build(&gate{port: portOf(t, plain.Listener.Addr()), testAllow: []netip.Prefix{loopback}}, d.resolver(), x509.NewCertPool(), 5*time.Second)
	goneC := build(&gate{port: gonePort, testAllow: []netip.Prefix{loopback}}, d.resolver(), nil, 5*time.Second)
	statusC := clientFor(t, status, d, 5*time.Second, nil)
	nx := clientFor(t, status, staticDNS(nil), 5*time.Second, nil)
	// Receivers whose TLS conversation fails, each with a certificate the client trusts: nothing is
	// delivered, and the class is tls -- what the owner reads is "the secure connection failed",
	// never "could not be reached" (API-12 second fix; until then a receiver's alert arrived as
	// crypto/tls's untyped "remote error" and was refused). The guide's §9.1: TLS 1.2 or newer, and
	// Taptime presents no client certificate.
	//   - TLS 1.1 at most: the receiver's protocol_version alert, in the handshake;
	//   - a client certificate required, in TLS 1.2 (the alert in the handshake) and in TLS 1.3 (the
	//     alert after the client's half, when the answer is read);
	//   - no cipher suite in common (the receiver offers only suites the client never does).
	failing := func(cfg *tls.Config) *Client {
		r := &recorder{srv: httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))}
		r.srv.TLS = cfg
		r.srv.Config.ErrorLog = log.New(io.Discard, "", 0)
		r.srv.StartTLS()
		t.Cleanup(r.srv.Close)
		return clientFor(t, r, d, 5*time.Second, nil)
	}
	oldC := failing(&tls.Config{MinVersion: tls.VersionTLS10, MaxVersion: tls.VersionTLS11})
	mtls12 := failing(&tls.Config{MaxVersion: tls.VersionTLS12, ClientAuth: tls.RequireAnyClientCert})
	mtls13 := failing(&tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAnyClientCert})
	var weak []uint16
	for _, cs := range tls.InsecureCipherSuites() {
		weak = append(weak, cs.ID)
	}
	noCipher := failing(&tls.Config{MaxVersion: tls.VersionTLS12, CipherSuites: weak})
	// A receiver that negotiates down to TLS 1.1 instead of sending an alert (an old server): the
	// client refuses its ServerHello with an error crypto/tls does not type -- only the handshake's
	// own failure says it is TLS. And one that takes the connection and drops it under the
	// ClientHello: the endpoint could not be reached, refused like a port nothing listens on.
	downgrade := rawReceiver(t, d, func(c net.Conn) {
		_, _ = c.Read(make([]byte, 4096))
		hello := append([]byte{0x03, 0x02}, make([]byte, 32)...) // TLS 1.1, a random of zeros
		hello = append(hello, 0x00, 0x00, 0x2f, 0x00)            // no session id, AES-128-CBC-SHA, no compression
		msg := append([]byte{0x02, 0x00, 0x00, byte(len(hello))}, hello...)
		_, _ = c.Write(append([]byte{0x16, 0x03, 0x02, 0x00, byte(len(msg))}, msg...))
		_, _ = c.Read(make([]byte, 64))
	})
	dropped := rawReceiver(t, d, func(c net.Conn) { _, _ = c.Read(make([]byte, 16)) })
	// One that reads the whole ClientHello and closes cleanly: nothing is left unread, so the close is
	// a FIN, not a reset, and the client's handshake ends in io.EOF -- refused too (the case above
	// leaves bytes unread and resets).
	finished := rawReceiver(t, d, func(c net.Conn) {
		header := make([]byte, 5)
		if _, err := io.ReadFull(c, header); err != nil {
			return
		}
		_, _ = io.ReadFull(c, make([]byte, int(header[3])<<8|int(header[4])))
	})

	ep := to(t, "https://secret-host.example.com/token-path-123")
	cases := []struct {
		name string
		c    *Client
		ep   Endpoint
		want Result
	}{
		{"untrusted certificate", untrusted, ep, Result{Class: ClassTLS}},
		{"TLS older than 1.2", oldC, ep, Result{Class: ClassTLS}},
		{"a client certificate required, TLS 1.2", mtls12, ep, Result{Class: ClassTLS}},
		{"a client certificate required, TLS 1.3", mtls13, ep, Result{Class: ClassTLS}},
		{"no cipher suite in common", noCipher, ep, Result{Class: ClassTLS}},
		{"a ServerHello of TLS 1.1", downgrade, ep, Result{Class: ClassTLS}},
		{"dropped under the ClientHello", dropped, ep, Result{Class: ClassRefused}},
		{"closed cleanly after the ClientHello", finished, ep, Result{Class: ClassRefused}},
		{"plain http on the port", plainC, ep, Result{Class: ClassTLS}},
		{"nothing listening", goneC, ep, Result{Class: ClassRefused}},
		{"no such host", nx, ep, Result{Class: ClassRefused}},
		{"404", statusC, to(t, "https://secret-host.example.com/404"), Result{Class: ClassHTTP4xx, Status: 404}},
		{"410", statusC, to(t, "https://secret-host.example.com/410"), Result{Class: ClassHTTP4xx, Status: 410}},
		{"503", statusC, to(t, "https://secret-host.example.com/503"), Result{Class: ClassHTTP5xx, Status: 503}},
		{"200", statusC, to(t, "https://secret-host.example.com/200"), Result{Status: 200}},
	}
	for _, c := range cases {
		res := c.c.Post(context.Background(), c.ep, []byte("{}"), nil)
		if res != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, res, c.want)
		}
		for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
			out := fmt.Sprintf(verb, res)
			for _, leak := range []string{"secret-host", "example.com", "token-path", "127.0.0.1", "dial", "lookup", "x509", "tls:"} {
				if strings.Contains(out, leak) {
					t.Errorf("%s: %s of the Result holds %q: %s", c.name, verb, leak, out)
				}
			}
		}
	}
	if fields := reflect.TypeOf(Result{}).NumField(); fields != 2 {
		t.Fatalf("Result has %d fields: it is a Class and a Status, nothing else", fields)
	}
}

// rawReceiver is a TCP listener on the loopback that hands each connection to serve and closes it
// -- a receiver that speaks no proper TLS -- and a Client that dials it.
func rawReceiver(t *testing.T, d *fakeDNS, serve func(net.Conn)) *Client {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = c.Close() }()
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				serve(c)
			}()
		}
	}()
	return build(&gate{port: portOf(t, l.Addr()), testAllow: []netip.Prefix{loopback}}, d.resolver(), x509.NewCertPool(), 5*time.Second)
}

// TestEgress_LogsNothingAndReturnsNoText: the package's non-test files import no logger and call none
// (slog appears only as the LogValuer the Endpoint's redaction implements), and Post returns no error.
func TestEgress_LogsNothingAndReturnsNoText(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		n++
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(token.NewFileSet(), f, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, im := range file.Imports {
			if p, _ := strconv.Unquote(im.Path.Value); p == "log" {
				t.Errorf("%s imports log", f)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			sel, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "slog" {
				switch sel.Sel.Name {
				case "Value", "StringValue", "LogValuer":
				default:
					t.Errorf("%s uses slog.%s", f, sel.Sel.Name)
				}
			}
			return true
		})
	}
	if n < 3 {
		t.Fatalf("CONTROL: only %d source files read", n)
	}
	if out := reflect.TypeOf((*Client).Post).NumOut(); out != 1 || reflect.TypeOf((*Client).Post).Out(0) != reflect.TypeOf(Result{}) {
		t.Fatal("Post returns something besides a Result")
	}
}
