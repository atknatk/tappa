package httpx_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/httpx"
)

// redirectCfg is the shipped shape: the base URL and the two redirect hosts.
func redirectCfg() *config.Config {
	return &config.Config{BaseURL: "https://taptime.mt", RedirectHosts: []string{"tappa.everva.com.tr", "www.taptime.mt"}}
}

// TestRedirectHosts_KeepPathAndQueryAndNeverLeaveTheBaseURL (backlog T71), through the shipped
// router and net/http's own request parsing (rawServe): on a redirect host every request -- a
// route, a tap, a POST, the liveness probe -- is answered with a redirect to the base URL that
// keeps the path and the query; 301 for GET and HEAD, 308 for everything else; and no request
// line moves the redirect to another host, which every redirect row checks on the parsed Location
// (scheme https, host taptime.mt, no user part) as well as on its exact text.
//
// MUTANTS (measured): r.URL.RequestURI() in place of the escaped path -> the opaque-form row's
// Location is https://taptime.mt@evil.example (red); the leading-slash cut removed -> the
// double-slash rows red; 301 for every method -> the POST/PUT rows red; the query dropped -> the
// query rows red; the middleware not mounted -> every redirect row red; the length bound removed ->
// the past-the-bound row red; the query measured before escaping -> the raw non-ASCII row red.
func TestRedirectHosts_KeepPathAndQueryAndNeverLeaveTheBaseURL(t *testing.T) {
	h := httpx.NewRouter(redirectCfg(), nil, stubFeatures{})
	for _, tc := range []struct {
		name, raw string
		code      int
		location  string
	}{
		{"a deep link", "GET /activate?x=1 HTTP/1.1\r\nHost: tappa.everva.com.tr\r\n\r\n",
			301, "https://taptime.mt/activate?x=1"},
		{"the bare www host", "GET / HTTP/1.1\r\nHost: www.taptime.mt\r\n\r\n", 301, "https://taptime.mt/"},
		{"a tap URL keeps its query byte for byte", "GET /t?p=AB%2FCD%3D&c=0A1B&x=a+b HTTP/1.1\r\nHost: tappa.everva.com.tr\r\n\r\n",
			301, "https://taptime.mt/t?p=AB%2FCD%3D&c=0A1B&x=a+b"},
		{"an empty query is kept", "GET /x? HTTP/1.1\r\nHost: www.taptime.mt\r\n\r\n", 301, "https://taptime.mt/x?"},
		{"an escaped path stays escaped", "GET /legal/a%20b HTTP/1.1\r\nHost: www.taptime.mt\r\n\r\n",
			301, "https://taptime.mt/legal/a%20b"},
		{"HEAD", "HEAD /admin HTTP/1.1\r\nHost: www.taptime.mt\r\n\r\n", 301, "https://taptime.mt/admin"},
		{"POST keeps its method: 308", "POST /api/checkin?k=1 HTTP/1.1\r\nHost: tappa.everva.com.tr\r\nContent-Length: 2\r\n\r\n{}",
			308, "https://taptime.mt/api/checkin?k=1"},
		{"PUT: 308", "PUT /admin/x HTTP/1.1\r\nHost: www.taptime.mt\r\nContent-Length: 0\r\n\r\n", 308, "https://taptime.mt/admin/x"},
		{"the liveness probe is not served there", "GET /healthz HTTP/1.1\r\nHost: www.taptime.mt\r\n\r\n",
			301, "https://taptime.mt/healthz"},
		// The host as OnHost reads it: port, trailing dot and case.
		{"a port", "GET /a HTTP/1.1\r\nHost: www.taptime.mt:443\r\n\r\n", 301, "https://taptime.mt/a"},
		{"a trailing dot", "GET /a HTTP/1.1\r\nHost: www.taptime.mt.\r\n\r\n", 301, "https://taptime.mt/a"},
		{"upper case", "GET /a HTTP/1.1\r\nHost: TAPPA.EVERVA.COM.TR\r\n\r\n", 301, "https://taptime.mt/a"},
		{"an absolute-URI request line names the host", "GET http://www.taptime.mt/a?b=1 HTTP/1.1\r\nHost: taptime.mt\r\n\r\n",
			301, "https://taptime.mt/a?b=1"},
		// The rows that try to move the redirect to another host.
		{"a double slash", "GET //evil.example/x?y=1 HTTP/1.1\r\nHost: www.taptime.mt\r\n\r\n",
			301, "https://taptime.mt/evil.example/x?y=1"},
		{"many slashes", "GET ////evil.example HTTP/1.1\r\nHost: tappa.everva.com.tr\r\n\r\n",
			301, "https://taptime.mt/evil.example"},
		{"a double slash in an absolute-URI line", "GET http://www.taptime.mt//evil.example/ HTTP/1.1\r\nHost: www.taptime.mt\r\n\r\n",
			301, "https://taptime.mt/evil.example/"},
		{"a backslash", "GET /\\evil.example HTTP/1.1\r\nHost: www.taptime.mt\r\n\r\n", 301, "https://taptime.mt/%5Cevil.example"},
		{"slash and backslash", "GET /\\/evil.example HTTP/1.1\r\nHost: www.taptime.mt\r\n\r\n",
			301, "https://taptime.mt/%5C/evil.example"},
		{"escaped slashes stay escaped", "GET /%2F%2Fevil.example HTTP/1.1\r\nHost: www.taptime.mt\r\n\r\n",
			301, "https://taptime.mt/%2F%2Fevil.example"},
		{"an at sign", "GET /@evil.example HTTP/1.1\r\nHost: www.taptime.mt\r\n\r\n", 301, "https://taptime.mt/@evil.example"},
		{"an opaque request target", "GET http:@evil.example HTTP/1.1\r\nHost: www.taptime.mt\r\n\r\n", 301, "https://taptime.mt/"},
		{"an opaque target with a query", "GET http:@evil.example?q=1 HTTP/1.1\r\nHost: www.taptime.mt\r\n\r\n",
			301, "https://taptime.mt/?q=1"},
		// A real server answers OPTIONS * itself (net/http's globalOptionsHandler); the row drives the
		// handler directly, which is the shape a different server in front could produce.
		{"OPTIONS *", "OPTIONS * HTTP/1.1\r\nHost: www.taptime.mt\r\n\r\n", 308, "https://taptime.mt/"},
		{"a query that names another host", "GET /?next=https://evil.example HTTP/1.1\r\nHost: www.taptime.mt\r\n\r\n",
			301, "https://taptime.mt/?next=https://evil.example"},
		// The bound: 2048 bytes of path and query are kept, one more is not.
		{"a target at the bound", "GET /" + strings.Repeat("a", 2047) + " HTTP/1.1\r\nHost: www.taptime.mt\r\n\r\n",
			301, "https://taptime.mt/" + strings.Repeat("a", 2047)},
		{"a target past the bound", "GET /?" + strings.Repeat("a", 2047) + " HTTP/1.1\r\nHost: www.taptime.mt\r\n\r\n",
			301, "https://taptime.mt/"},
		// The bound is the Location as sent: a raw non-ASCII byte in the query is three bytes there.
		{"raw non-ASCII bytes are measured escaped", "GET /?" + strings.Repeat("\xff", 700) + " HTTP/1.1\r\nHost: www.taptime.mt\r\n\r\n",
			301, "https://taptime.mt/"},
		{"a raw non-ASCII query byte is escaped", "GET /t?n=\xc3\xa7 HTTP/1.1\r\nHost: www.taptime.mt\r\n\r\n",
			301, "https://taptime.mt/t?n=%C3%A7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := rawServe(t, h, tc.raw)
			loc := w.Result().Header.Get("Location")
			if w.Code != tc.code || loc != tc.location {
				t.Fatalf("%d %q, want %d %q (body %q)", w.Code, loc, tc.code, tc.location, w.Body.String())
			}
			u, err := url.Parse(loc)
			if err != nil || u.Scheme != "https" || u.Host != "taptime.mt" || u.User != nil || u.Opaque != "" {
				t.Fatalf("Location %q leaves the base URL (%+v, %v)", loc, u, err)
			}
			if w.Body.String() == "customer" || w.Body.String() == "operator" {
				t.Fatal("a route ran on a redirect host")
			}
		})
	}
}

// TestRedirectHosts_OnlyTheListedHostsAreRedirected: the canonical host, a host that only
// resembles a redirect host, and a forwarded-host header naming one are served as before; and
// NewRouter without redirect hosts (every test config in the tree) mounts nothing.
//
// MUTANT (measured): OnHost replaced by a suffix match -> the look-alike rows red.
func TestRedirectHosts_OnlyTheListedHostsAreRedirected(t *testing.T) {
	h := httpx.NewRouter(redirectCfg(), nil, stubFeatures{})
	for _, raw := range []string{
		"GET /admin HTTP/1.1\r\nHost: taptime.mt\r\n\r\n",
		"GET /admin HTTP/1.1\r\nHost: xwww.taptime.mt\r\n\r\n",
		"GET /admin HTTP/1.1\r\nHost: evil.tappa.everva.com.tr\r\n\r\n",
		"GET /admin HTTP/1.1\r\nHost: www.taptime.mt.evil.example\r\n\r\n",
		"GET /admin HTTP/1.1\r\nHost: taptime.mt\r\nX-Forwarded-Host: www.taptime.mt\r\n\r\n",
		// An absolute-URI line names the canonical host; its Host header does not decide.
		"GET http://taptime.mt/admin HTTP/1.1\r\nHost: www.taptime.mt\r\n\r\n",
	} {
		if got := outcome(rawServe(t, h, raw)); got != "customer" {
			t.Errorf("%q: %s, want the route served", raw, got)
		}
	}
	for _, cfg := range []*config.Config{nil, {}, {BaseURL: "https://taptime.mt"}, {RedirectHosts: []string{"www.taptime.mt"}}} {
		r := httpx.NewRouter(cfg, nil, stubFeatures{})
		w := rawServe(t, r, "GET /admin HTTP/1.1\r\nHost: www.taptime.mt\r\n\r\n")
		if outcome(w) != "customer" {
			t.Errorf("config %+v: %d %q, want no redirect mounted", cfg, w.Code, w.Result().Header.Get("Location"))
		}
	}
	// A trailing slash on the base URL (validBaseURL accepts one) does not double the slash.
	r := httpx.NewRouter(&config.Config{BaseURL: "https://taptime.mt/", RedirectHosts: []string{"www.taptime.mt"}}, nil, stubFeatures{})
	if loc := rawServe(t, r, "GET /a HTTP/1.1\r\nHost: www.taptime.mt\r\n\r\n").Result().Header.Get("Location"); loc != "https://taptime.mt/a" {
		t.Errorf("base URL with a trailing slash: Location %q", loc)
	}
}
