package httpx

import (
	"net/http"
	"net/netip"
	"strings"
	"testing"
)

// prefixes is the trusted set most cases use: an internal range and loopback.
func prefixes(t *testing.T, ss ...string) []netip.Prefix {
	t.Helper()
	out := make([]netip.Prefix, 0, len(ss))
	for _, s := range ss {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			t.Fatalf("ParsePrefix(%q): %v", s, err)
		}
		out = append(out, p)
	}
	return acceptPrefixes(out)
}

func header(values ...string) http.Header {
	h := http.Header{}
	for _, v := range values {
		h.Add("X-Forwarded-For", v)
	}
	return h
}

// TestResolveClientIP is the decision table. Every row states the peer, what the
// caller sent, and the address we are willing to believe.
func TestResolveClientIP(t *testing.T) {
	t.Parallel()
	trusted := prefixes(t, "10.0.0.0/8", "127.0.0.1/32")

	tests := []struct {
		name    string
		peer    string
		header  http.Header
		trusted []netip.Prefix
		want    string // "" means: no address at all
	}{
		{
			// The core rule. A request that reached us DIRECTLY carries whatever
			// header its sender felt like writing, so the header is not read.
			name:    "untrusted peer: the header is ignored entirely",
			peer:    "198.51.100.7:44321",
			header:  header("1.2.3.4"),
			trusted: trusted,
			want:    "198.51.100.7",
		},
		{
			name:    "trusted proxy, one entry: that entry is the client",
			peer:    "10.0.0.1:9000",
			header:  header("203.0.113.9"),
			trusted: trusted,
			want:    "203.0.113.9",
		},
		{
			// THE ONE THAT MATTERS. A left-to-right reader would answer
			// 1.1.1.1 — the value the CLIENT wrote. Walking from the right
			// skips our own hop and stops at the address that hop observed.
			name:    "forged left end does not win",
			peer:    "10.0.0.1:9000",
			header:  header("1.1.1.1, 203.0.113.9, 10.0.0.2"),
			trusted: trusted,
			want:    "203.0.113.9",
		},
		{
			// A caller cannot escape the walk by splitting its forgery across
			// header lines: Go keeps them in arrival order and RFC 9110 §5.3
			// makes that equivalent to one comma-joined value.
			name:    "two X-Forwarded-For headers are one chain, in order",
			peer:    "10.0.0.1:9000",
			header:  header("1.1.1.1", "203.0.113.9, 10.0.0.2"),
			trusted: trusted,
			want:    "203.0.113.9",
		},
		{
			name:    "every entry trusted: the leftmost is as far as the chain goes",
			peer:    "10.0.0.1:9000",
			header:  header("10.9.9.9, 10.0.0.2"),
			trusted: trusted,
			want:    "10.9.9.9",
		},
		{
			name:    "trusted proxy sent no header: the proxy itself",
			peer:    "10.0.0.1:9000",
			header:  http.Header{},
			trusted: trusted,
			want:    "10.0.0.1",
		},
		{
			// Empty TAPPA_TRUSTED_PROXIES is today's shipped default.
			name:    "no trusted proxies configured: RemoteAddr, verbatim",
			peer:    "10.0.0.1:9000",
			header:  header("203.0.113.9"),
			trusted: nil,
			want:    "10.0.0.1",
		},
		{
			name:    "IPv6 peer and IPv6 client",
			peer:    "[2001:db8::1]:443",
			header:  header("2001:db8:beef::9"),
			trusted: prefixes(t, "2001:db8::/64"),
			want:    "2001:db8:beef::9",
		},
		{
			name:    "bracketed IPv6 with a port in the chain",
			peer:    "10.0.0.1:9000",
			header:  header("[2001:db8:beef::9]:51000, 10.0.0.2"),
			trusted: trusted,
			want:    "2001:db8:beef::9",
		},
		{
			name:    "bracketed IPv6 without a port in the chain",
			peer:    "10.0.0.1:9000",
			header:  header("[2001:db8:beef::9]"),
			trusted: trusted,
			want:    "2001:db8:beef::9",
		},
		{
			// Some proxies copy RemoteAddr verbatim, port and all.
			name:    "IPv4 with a port in the chain",
			peer:    "10.0.0.1:9000",
			header:  header("203.0.113.9:51000"),
			trusted: trusted,
			want:    "203.0.113.9",
		},
		{
			// ::ffff:203.0.113.9 and 203.0.113.9 are the same host; two keys
			// for one caller would be two budgets for one caller.
			name:    "IPv4-mapped IPv6 in the chain is unmapped",
			peer:    "10.0.0.1:9000",
			header:  header("::ffff:203.0.113.9"),
			trusted: trusted,
			want:    "203.0.113.9",
		},
		{
			// Same canonicalisation on the PEER side: without it the 4-in-6
			// address misses the IPv4 trusted range and the header is dropped.
			name:    "IPv4-mapped IPv6 peer still matches an IPv4 trusted range",
			peer:    "[::ffff:10.0.0.1]:9000",
			header:  header("203.0.113.9"),
			trusted: trusted,
			want:    "203.0.113.9",
		},
		{
			// A zone would let one host mint unlimited distinct keys.
			name:    "zone is dropped",
			peer:    "10.0.0.1:9000",
			header:  header("fe80::1%eth0"),
			trusted: trusted,
			want:    "fe80::1",
		},
		{
			name:    "garbage entry ends the walk at the last trusted hop",
			peer:    "10.0.0.1:9000",
			header:  header("203.0.113.9, not-an-ip"),
			trusted: trusted,
			want:    "10.0.0.1",
		},
		{
			name:    "obfuscated RFC 7239 identifier is not guessed at",
			peer:    "10.0.0.1:9000",
			header:  header("203.0.113.9, _hidden"),
			trusted: trusted,
			want:    "10.0.0.1",
		},
		{
			name:    "trailing comma is malformed, so the walk stops",
			peer:    "10.0.0.1:9000",
			header:  header("203.0.113.9,"),
			trusted: trusted,
			want:    "10.0.0.1",
		},
		{
			name:    "empty header value is treated as no header",
			peer:    "10.0.0.1:9000",
			header:  header(""),
			trusted: trusted,
			want:    "10.0.0.1",
		},
		{
			name:    "whitespace around entries is tolerated",
			peer:    "10.0.0.1:9000",
			header:  header("  203.0.113.9  ,  10.0.0.2  "),
			trusted: trusted,
			want:    "203.0.113.9",
		},
		{
			name:    "a hostname is not an address",
			peer:    "10.0.0.1:9000",
			header:  header("evil.example.com"),
			trusted: trusted,
			want:    "10.0.0.1",
		},
		{
			name:    "unparseable RemoteAddr yields no address at all",
			peer:    "not-an-address",
			header:  header("203.0.113.9"),
			trusted: trusted,
			want:    "",
		},
		{
			name:    "bare RemoteAddr without a port is still parsed",
			peer:    "198.51.100.7",
			header:  http.Header{},
			trusted: trusted,
			want:    "198.51.100.7",
		},
		{
			// The cap is only reachable by spoofing addresses INSIDE our own
			// proxy range, and it stops at the innermost trusted hop.
			name:    "a chain longer than the hop cap stops at a trusted hop",
			peer:    "10.0.0.1:9000",
			header:  header("203.0.113.9, " + strings.Repeat("10.0.0.2, ", maxForwardedHops+10) + "10.0.0.3"),
			trusted: trusted,
			want:    "10.0.0.2",
		},
		{
			// A trusted range written in v4-mapped form is DROPPED, so nothing is
			// trusted and the header is not read: the peer answers. config
			// refuses this spelling at startup, which is where an operator finds
			// out; here it just fails closed. See
			// TestAcceptPrefixes_DropsTheSecondSpellingOfARange for why the
			// earlier convenience (unmapping it) was a vulnerability.
			name:    "4-in-6 trusted prefix trusts nobody",
			peer:    "10.0.0.1:9000",
			header:  header("203.0.113.9"),
			trusted: prefixes(t, "::ffff:10.0.0.0/104"),
			want:    "10.0.0.1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := resolveClientIP(tc.peer, tc.header, tc.trusted)
			if tc.want == "" {
				if got.IsValid() {
					t.Fatalf("want no address, got %v", got)
				}
				return
			}
			if !got.IsValid() || got.String() != tc.want {
				t.Fatalf("want %s, got %v", tc.want, got)
			}
		})
	}
}

// TestResolveClientIP_LeftmostIsNeverTheAnswer pins the direction of the walk on
// its own, so that a future "simplification" to strings.Split(...)[0] fails
// loudly rather than quietly handing every caller a chosen address.
func TestResolveClientIP_LeftmostIsNeverTheAnswer(t *testing.T) {
	t.Parallel()
	trusted := prefixes(t, "10.0.0.0/8")
	const forged = "1.1.1.1"
	got := resolveClientIP("10.0.0.1:9000", header(forged+", 203.0.113.9, 10.0.0.2"), trusted)
	if got.String() == forged {
		t.Fatal("the leftmost (client-written) entry was believed")
	}
	if got.String() != "203.0.113.9" {
		t.Fatalf("want the address our trusted hop observed, got %v", got)
	}
}

// TestResolveClientIP_DefaultRouteReturnsTheClientsOwnClaim pins the one
// configuration under which this file gives the caller what it asked for.
//
// It is NOT a bug in the walk: with every address trusted there is no untrusted
// hop to stop at, so there is no honest answer to give. It is a configuration
// that inverts the defence, which is why config.Load refuses a default route in
// production and warns about it everywhere else
// (TestLoad_TrustedProxiesDefaultRoute). The behaviour is pinned here so that
// the gate over there has something concrete to point at, and so that nobody
// later "fixes" the walk instead of the configuration.
func TestResolveClientIP_DefaultRouteReturnsTheClientsOwnClaim(t *testing.T) {
	t.Parallel()
	for _, route := range []string{"0.0.0.0/0", "::/0"} {
		trusted := prefixes(t, route, "10.0.0.0/8")
		got := resolveClientIP("10.0.0.1:9000", header("1.2.3.4, 5.6.7.8"), trusted)
		if route == "0.0.0.0/0" && got.String() != "1.2.3.4" {
			t.Fatalf("with %s the leftmost claim was expected, got %v", route, got)
		}
	}
	// The IPv6 default route does not launder IPv4 traffic: families are
	// distinct, so ::/0 leaves an IPv4 chain resolving normally. Stated because
	// "a default route" is two values and only one of them bites per family.
	got := resolveClientIP("10.0.0.1:9000", header("1.2.3.4, 203.0.113.9"), prefixes(t, "::/0", "10.0.0.0/8"))
	if got.String() != "203.0.113.9" {
		t.Fatalf("::/0 changed the answer for an IPv4 chain: %v", got)
	}
}

// TestResolveClientIP_ForgeryCannotBeatTheTruth is the property the whole file
// exists for: whatever a caller puts in the header, the answer is the address it
// actually connected from — unless a trusted hop vouched for something else.
func TestResolveClientIP_ForgeryCannotBeatTheTruth(t *testing.T) {
	t.Parallel()
	trusted := prefixes(t, "10.0.0.0/8")
	forgeries := []string{
		"10.0.0.99",             // pretending to be internal
		"127.0.0.1",             // pretending to be local
		"203.0.113.9, 10.0.0.2", // a whole fake chain
		"::ffff:10.0.0.99",      // the same, 4-in-6
		strings.Repeat("10.0.0.2,", 80) + "203.0.113.9",
	}
	for _, f := range forgeries {
		got := resolveClientIP("198.51.100.7:44321", header(f), trusted)
		if got.String() != "198.51.100.7" {
			t.Fatalf("header %q changed the answer to %v", f, got)
		}
	}
}

func TestRateKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"IPv4 is keyed exactly", "203.0.113.9", "203.0.113.9"},
		{"IPv6 is keyed by /64", "2001:db8:1:2:3:4:5:6", "2001:db8:1:2::/64"},
		{"IPv4-mapped is keyed as IPv4", "::ffff:203.0.113.9", "203.0.113.9"},
		{"the zero address is named, not empty", "", "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var a netip.Addr
			if tc.in != "" {
				var err error
				if a, err = netip.ParseAddr(tc.in); err != nil {
					t.Fatalf("ParseAddr: %v", err)
				}
			}
			if got := RateKey(a); got != tc.want {
				t.Fatalf("RateKey(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestRateKey_IPv6RotationSharesOneBucket is the reason /64 exists: a host that
// changes its address every request must not get a fresh budget every request.
func TestRateKey_IPv6RotationSharesOneBucket(t *testing.T) {
	t.Parallel()
	first := RateKey(netip.MustParseAddr("2001:db8:1:2::1"))
	for _, s := range []string{"2001:db8:1:2::2", "2001:db8:1:2:aaaa:bbbb:cccc:dddd", "2001:db8:1:2:ffff::1"} {
		if got := RateKey(netip.MustParseAddr(s)); got != first {
			t.Fatalf("%s keyed as %q, want %q", s, got, first)
		}
	}
	// A DIFFERENT /64 is a different caller — the positive control that keeps
	// this from passing because everything collapses to one key.
	if RateKey(netip.MustParseAddr("2001:db8:1:3::1")) == first {
		t.Fatal("two distinct /64s share a bucket")
	}
}

// TestAcceptPrefixes_DropsTheSecondSpellingOfARange is the regression for the
// audit's blocking finding.
//
// The v4-mapped form used to be UNMAPPED here as a convenience, which gave every
// range two spellings — and config's default-route gate was checking one while
// this walk used the other, so ::ffff:0.0.0.0/96 passed the gate as Bits()==96
// and behaved as 0.0.0.0/0. Dropping it means the ambiguous form cannot reach the
// walk no matter what a caller constructs RealIP with; config refuses it loudly
// so nobody has to discover it here.
func TestAcceptPrefixes_DropsTheSecondSpellingOfARange(t *testing.T) {
	t.Parallel()
	in := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),          // the one true spelling
		netip.MustParsePrefix("::ffff:10.0.0.0/104"), // v4-mapped: dropped
		netip.MustParsePrefix("::ffff:0.0.0.0/96"),   // the dangerous one: dropped
		netip.MustParsePrefix("::ffff:10.0.0.1/96"),  // ditto, host bits set
		{}, // the zero Prefix: dropped
	}
	got := acceptPrefixes(in)
	if len(got) != 1 || got[0] != netip.MustParsePrefix("10.0.0.0/8") {
		t.Fatalf("want only 10.0.0.0/8, got %v", got)
	}

	// The property that matters, stated as a property: NOTHING acceptPrefixes
	// keeps may be a v4-mapped prefix, so no entry can mean one range here and a
	// different one to a check that did not unmap.
	for _, p := range got {
		if p.Addr().Is4In6() {
			t.Fatalf("a v4-mapped prefix survived: %v", p)
		}
	}

	// END TO END: the dangerous spelling, alone, must not trust anybody. Before
	// the fix this made an ordinary caller's forged header authoritative.
	only4in6 := acceptPrefixes([]netip.Prefix{netip.MustParsePrefix("::ffff:0.0.0.0/96")})
	if len(only4in6) != 0 {
		t.Fatalf("::ffff:0.0.0.0/96 survived as %v", only4in6)
	}
	got2 := resolveClientIP("198.51.100.7:44321",
		header("192.0.2.55, 1.1.1.1"),
		acceptPrefixes([]netip.Prefix{netip.MustParsePrefix("::ffff:0.0.0.0/96")}))
	if got2.String() != "198.51.100.7" {
		t.Fatalf("the caller chose its own address: %v", got2)
	}
}
