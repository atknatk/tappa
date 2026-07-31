// Package httpx_test exercises the middleware from OUTSIDE the package, the way
// a handler will. The M5-01 audits are the reason: a claim about what a caller
// can or cannot do has to be tried from where that caller actually stands.
package httpx_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/httpx"
)

// seen records what a handler observed about the client address. Header survival
// is measured separately, by TestRealIP_StripsEveryKnownForwardingHeader, which
// owns the list.
type seen struct {
	addr netip.Addr
}

func probe(into *seen) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		into.addr = httpx.ClientIP(r)
		w.WriteHeader(http.StatusOK)
	}
}

func request(t *testing.T, peer string, headers map[string][]string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/probe", nil)
	r.RemoteAddr = peer
	for k, vs := range headers {
		for _, v := range vs {
			r.Header.Add(k, v)
		}
	}
	return r
}

func TestRealIP_ResolvesThroughATrustedProxy(t *testing.T) {
	t.Parallel()
	var got seen
	h := httpx.RealIP([]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")})(probe(&got))
	h.ServeHTTP(httptest.NewRecorder(), request(t, "10.0.0.1:9000", map[string][]string{
		"X-Forwarded-For": {"1.1.1.1", "203.0.113.9, 10.0.0.2"},
	}))
	if got.addr.String() != "203.0.113.9" {
		t.Fatalf("client address = %v, want 203.0.113.9", got.addr)
	}
}

func TestRealIP_IgnoresHeadersFromAnUntrustedPeer(t *testing.T) {
	t.Parallel()
	var got seen
	h := httpx.RealIP([]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")})(probe(&got))
	h.ServeHTTP(httptest.NewRecorder(), request(t, "198.51.100.7:44321", map[string][]string{
		"X-Forwarded-For": {"10.0.0.99"},
		"X-Real-IP":       {"10.0.0.99"},
		"True-Client-IP":  {"10.0.0.99"},
	}))
	if got.addr.String() != "198.51.100.7" {
		t.Fatalf("client address = %v, want the TCP peer 198.51.100.7", got.addr)
	}
}

// headerCorpus is every header a scanner, a CDN or a proxy has been seen to use
// for a client address, plus four that are deliberately left alone. `stripped`
// says which side of RealIP's criterion each one falls on.
//
// TWO AUDITS SHAPED THIS LIST. The first found that a three-name list let NINE
// headers through, including RFC 7239's `Forwarded`. The second measured the
// follow-up over a live TCP socket and found 23 of 36 still arriving — the
// classic Client-IP / Proxy-Client-IP / WL-Proxy-Client-IP family among them,
// which a list claiming to hold "what is commonly understood to carry a client
// address" cannot omit. Before: SURVIVED 23 of 36. After: 4, and those four are
// the positive control below.
var headerCorpus = []struct {
	name     string
	stripped bool
}{
	{"X-Forwarded-For", true}, {"Forwarded", true}, {"Forwarded-For", true},
	{"X-Forwarded", true}, {"X-Original-Forwarded-For", true},
	{"X-Http-Forwarded-For", true}, {"X-Original-For", true},
	{"X-Forwarded-Client-Ip", true}, {"X-Real-IP", true}, {"X-Real-Ip-Orig", true},
	{"True-Client-IP", true}, {"X-True-Client-IP", true}, {"CF-Connecting-IP", true},
	{"CF-Connecting-IPv6", true}, {"CF-Pseudo-IPv4", true}, {"Fastly-Client-IP", true},
	{"X-Akamai-Client-Ip", true}, {"Ali-Cdn-Real-Ip", true}, {"Cdn-Src-Ip", true},
	{"X-Client-IP", true}, {"Client-IP", true}, {"Proxy-Client-IP", true},
	{"WL-Proxy-Client-IP", true}, {"X-Remote-IP", true}, {"X-Remote-Addr", true},
	{"Remote-Addr", true}, {"X-Coming-From", true}, {"X-ProxyUser-Ip", true},
	{"X-Envoy-External-Address", true}, {"X-Cluster-Client-IP", true},
	{"X-Azure-ClientIP", true}, {"X-Appengine-User-IP", true},

	// THE POSITIVE CONTROL. None of these carries a client address, so RealIP
	// leaves them alone; without them, "delete every header" would pass the
	// assertion above for the wrong reason. Via names intermediaries (RFC 9110
	// §7.6.3), X-Forwarded-Host/Proto carry a host and a scheme, and Origin is
	// load-bearing for the CSRF checks in internal/handler — stripping THAT one
	// would break the activation flow, which is the sharpest reason this list
	// needs a criterion rather than a reflex.
	{"Via", false}, {"X-Forwarded-Host", false}, {"X-Forwarded-Proto", false},
	{"Origin", false},
}

// TestRealIP_HeaderCorpusOverARealSocket checks the denylist THE WAY A CALLER
// REACHES IT: bytes on a TCP connection, parsed by net/http, rather than an
// http.Header built in memory. The audit made that distinction and it is worth
// keeping — a hand-built map cannot show what the wire parser does with odd
// casing or with a header repeated across lines.
//
// It is deliberately NOT named "no handler can read a client address from a
// header". That claim was made once, was false, and now lives in realip.go as a
// RULE. A vendor header nobody has heard of yet would pass this test and still
// be a bug to read.
func TestRealIP_HeaderCorpusOverARealSocket(t *testing.T) {
	t.Parallel()
	var (
		mu       sync.Mutex
		survived []string
	)
	srv := httptest.NewServer(httpx.RealIP([]netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			for _, c := range headerCorpus {
				if r.Header.Get(c.name) != "" {
					survived = append(survived, c.name)
				}
			}
			w.WriteHeader(http.StatusOK)
		})))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	var req strings.Builder
	fmt.Fprintf(&req, "GET /probe HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n", addr)
	for _, c := range headerCorpus {
		fmt.Fprintf(&req, "%s: 1.2.3.4\r\n", c.name)
	}
	req.WriteString("\r\n")
	if _, err := conn.Write([]byte(req.String())); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := io.ReadAll(conn); err != nil {
		t.Fatalf("read: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	got := map[string]bool{}
	for _, n := range survived {
		got[n] = true
	}
	for _, c := range headerCorpus {
		switch {
		case c.stripped && got[c.name]:
			t.Errorf("%s reached the handler; it claims to carry a client address", c.name)
		case !c.stripped && !got[c.name]:
			t.Errorf("%s was stripped; RealIP has no authority over it", c.name)
		}
	}
}

// TestClientIP_WithoutTheMiddlewareFallsBackToThePeer measures the degradation
// path, and measures that it is a degradation and not a hole: no header is read.
func TestClientIP_WithoutTheMiddlewareFallsBackToThePeer(t *testing.T) {
	t.Parallel()
	var got seen
	probe(&got).ServeHTTP(httptest.NewRecorder(), request(t, "10.0.0.1:9000", map[string][]string{
		"X-Forwarded-For": {"203.0.113.9"},
	}))
	if got.addr.String() != "10.0.0.1" {
		t.Fatalf("client address = %v, want the peer 10.0.0.1", got.addr)
	}
}

// mountFunc lets a test satisfy httpx.Mounter without a handler package.
type mountFunc func(chi.Router)

func (f mountFunc) Mount(r chi.Router) { f(r) }

// TestNewRouter_ResolvesTheClientAddress proves the wiring, not just the
// component: a feature mounted through NewRouter gets the resolved address
// without doing anything itself.
func TestNewRouter_ResolvesTheClientAddress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		trusted []netip.Prefix
		peer    string
		want    string
	}{
		{
			name:    "configured proxy: the header is honoured",
			trusted: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
			peer:    "10.0.0.1:9000",
			want:    "203.0.113.9",
		},
		{
			// The shipped default (TAPPA_TRUSTED_PROXIES empty).
			name:    "no proxy configured: the peer, and the header is not read",
			trusted: nil,
			peer:    "10.0.0.1:9000",
			want:    "10.0.0.1",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got seen
			r := httpx.NewRouter(
				&config.Config{TrustedProxies: tc.trusted},
				mountFunc(func(r chi.Router) { r.Get("/probe", probe(&got)) }),
			)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, request(t, tc.peer, map[string][]string{
				"X-Forwarded-For": {"203.0.113.9"},
			}))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d", w.Code)
			}
			if got.addr.String() != tc.want {
				t.Fatalf("client address = %v, want %s", got.addr, tc.want)
			}
		})
	}
}

// TestNewRouter_NilConfigTrustsNothing: a router built without configuration
// must not become one that believes headers.
func TestNewRouter_NilConfigTrustsNothing(t *testing.T) {
	t.Parallel()
	var got seen
	r := httpx.NewRouter(nil, mountFunc(func(r chi.Router) { r.Get("/probe", probe(&got)) }))
	r.ServeHTTP(httptest.NewRecorder(), request(t, "10.0.0.1:9000", map[string][]string{
		"X-Forwarded-For": {"203.0.113.9"},
	}))
	if got.addr.String() != "10.0.0.1" {
		t.Fatalf("client address = %v, want the peer 10.0.0.1", got.addr)
	}
}

// setEnvForLoad gives config.Load a valid environment so a test can vary exactly
// one variable and reach the check it cares about.
func setEnvForLoad(t *testing.T, env, trustedProxies string) {
	t.Helper()
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("DATABASE_URL", "postgres://app@localhost/tappa")
	t.Setenv("DATABASE_MIGRATE_URL", "")
	t.Setenv("TAPPA_SESSION_HMAC_KEY", key)
	t.Setenv("TAPPA_TAG_KEK", key)
	t.Setenv("TAPPA_INVITE_HMAC_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	t.Setenv("TAPPA_RETENTION_YEARS", "2")
	t.Setenv("TAPPA_GPS_RADIUS_M", "")
	t.Setenv("TAPPA_DEBOUNCE_SECONDS", "")
	t.Setenv("TAPPA_ENV", env)
	t.Setenv("TAPPA_TRUSTED_PROXIES", trustedProxies)
}

// resolveThroughRealWiring runs one request through config.Load + NewRouter —
// the real path, not a hand-built prefix list — and reports what the handler was
// told the client address is.
func resolveThroughRealWiring(t *testing.T, peer, xff string) (string, error) {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	var got string
	r := httpx.NewRouter(cfg, mountFunc(func(r chi.Router) {
		r.Get("/probe", func(_ http.ResponseWriter, req *http.Request) { got = httpx.ClientIP(req).String() })
	}))
	req := request(t, peer, map[string][]string{"X-Forwarded-For": {xff}})
	r.ServeHTTP(httptest.NewRecorder(), req)
	return got, nil
}

// TestEndToEnd_NoSpellingOfEverybodyReachesTheResolver is the regression for the
// security audit's blocking finding, written the way the audit measured it:
// through the REAL wiring, in production mode, with an ORDINARY INTERNET CALLER
// as the peer — not a proxy.
//
// The finding was that a check and its consumer looked at two representations of
// one value. ::ffff:0.0.0.0/96 read as Bits()==96 to config's default-route gate
// and behaved as 0.0.0.0/0 in the resolver, so that single line of configuration
// let every caller on earth choose its own address — no error, no warning, and
// proof of place is 50 of 100 trust points (§5).
//
// Measured before the fix, exactly these rows:
//
//	0.0.0.0/0                       -> REFUSED
//	::/0                            -> REFUSED
//	::ffff:0.0.0.0/96               -> loaded, client=192.0.2.55  (the forged value)
//	::ffff:10.0.0.1/96              -> loaded, client=192.0.2.55
//	127.0.0.1/32,::ffff:0.0.0.0/96  -> loaded, client=192.0.2.55
//
// After: every row refused at startup.
func TestEndToEnd_NoSpellingOfEverybodyReachesTheResolver(t *testing.T) {
	for _, spelling := range []string{
		"0.0.0.0/0",
		"::/0",
		"::ffff:0.0.0.0/96",              // the bypass: unmapped to 0.0.0.0/0 by the resolver
		"::ffff:10.0.0.1/96",             // same range, host bits set
		"::ffff:0.0.0.0/0",               // /0 already, in the mapped spelling
		"127.0.0.1/32,::ffff:0.0.0.0/96", // hidden in an otherwise sane list
		"1.2.3.4/0",                      // /0 with host bits: still everybody
	} {
		setEnvForLoad(t, config.EnvProd, spelling)
		got, err := resolveThroughRealWiring(t, "198.51.100.7:44321", "192.0.2.55, 1.1.1.1")
		if err == nil {
			t.Errorf("TAPPA_TRUSTED_PROXIES=%q loaded in production and resolved client=%s", spelling, got)
			continue
		}
		if !strings.Contains(err.Error(), "TAPPA_TRUSTED_PROXIES") {
			t.Errorf("%q: the error does not name the variable: %v", spelling, err)
		}
	}
}

// TestEndToEnd_RealIngressListStillWorks is the positive control for the test
// above: if the gate refused everything, the regression would pass for the wrong
// reason. A production deployment with a real proxy list must load AND behave —
// honouring the chain from the proxy, ignoring it from anybody else.
func TestEndToEnd_RealIngressListStillWorks(t *testing.T) {
	setEnvForLoad(t, config.EnvProd, "127.0.0.1/32,10.0.0.0/8")

	// From the trusted proxy: the address our hop observed, not the forged left end.
	got, err := resolveThroughRealWiring(t, "10.0.0.1:9000", "1.1.1.1, 192.0.2.55")
	if err != nil {
		t.Fatalf("a real ingress list must load in production: %v", err)
	}
	if got != "192.0.2.55" {
		t.Fatalf("client = %s, want 192.0.2.55", got)
	}

	// From an ordinary caller: its own address, whatever it claims.
	got, err = resolveThroughRealWiring(t, "198.51.100.7:44321", "192.0.2.55, 1.1.1.1")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if got != "198.51.100.7" {
		t.Fatalf("client = %s, want the peer 198.51.100.7", got)
	}
}
