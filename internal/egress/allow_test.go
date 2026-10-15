package egress

import (
	"net/netip"
	"strings"
	"testing"
)

// allow_test.go -- ADR 0028 §7's allow list, measured address by address against the registry.

// registryCSV is the first two columns of IANA's two special-purpose registries, VERBATIM from their
// CSV exports as downloaded on 2026-10-15 (both "updated 2025-10-09"), footnote markers included. It
// is the test's own transcription: production's table (ianaSpecialPurpose) is compared with it line
// by line, so dropping an entry from the code -- even one another entry covers -- is red.
const registryCSV = `0.0.0.0/8,"""This network"""
0.0.0.0/32,"""This host on this network"""
10.0.0.0/8,Private-Use
100.64.0.0/10,Shared Address Space
127.0.0.0/8,Loopback
169.254.0.0/16,Link Local
172.16.0.0/12,Private-Use
192.0.0.0/24 [2],IETF Protocol Assignments
192.0.0.0/29,IPv4 Service Continuity Prefix
192.0.0.8/32,IPv4 dummy address
192.0.0.9/32,Port Control Protocol Anycast
192.0.0.10/32,Traversal Using Relays around NAT Anycast
"192.0.0.170/32, 192.0.0.171/32"
192.0.2.0/24,Documentation (TEST-NET-1)
192.31.196.0/24,AS112-v4
192.52.193.0/24,AMT
192.88.99.0/24,Deprecated (6to4 Relay Anycast)
192.88.99.2/32,6a44-relay anycast address
192.168.0.0/16,Private-Use
192.175.48.0/24,Direct Delegation AS112 Service
198.18.0.0/15,Benchmarking
198.51.100.0/24,Documentation (TEST-NET-2)
203.0.113.0/24,Documentation (TEST-NET-3)
240.0.0.0/4,Reserved
255.255.255.255/32,Limited Broadcast
::1/128,Loopback Address
::/128,Unspecified Address
::ffff:0:0/96,IPv4-mapped Address
64:ff9b::/96,IPv4-IPv6 Translat.
64:ff9b:1::/48,IPv4-IPv6 Translat.
100::/64,Discard-Only Address Block
100:0:0:1::/64,Dummy IPv6 Prefix
2001::/23,IETF Protocol Assignments
2001::/32,TEREDO
2001:1::1/128,Port Control Protocol Anycast
2001:1::2/128,Traversal Using Relays around NAT Anycast
2001:1::3/128,DNS-SD Service Registration Protocol Anycast
2001:2::/48,Benchmarking
2001:3::/32,AMT
2001:4:112::/48,AS112-v6
2001:10::/28,Deprecated (previously ORCHID)
2001:20::/28,ORCHIDv2
2001:30::/28,Drone Remote ID Protocol Entity Tags (DETs) Prefix
2001:db8::/32,Documentation
2002::/16 [3],6to4
2620:4f:8000::/48,Direct Delegation AS112 Service
3fff::/20,Documentation
5f00::/16,Segment Routing (SRv6) SIDs
fc00::/7,Unique-Local
fe80::/10,Link-Local Unicast`

// registryEntries parses registryCSV into prefixes: the block column, the footnote marker dropped, a
// quoted "a, b" row split into its two.
func registryEntries(t *testing.T) []netip.Prefix {
	t.Helper()
	var out []netip.Prefix
	for _, line := range strings.Split(registryCSV, "\n") {
		block := line
		if strings.HasPrefix(line, `"`) {
			block = strings.Trim(line, `"`)
		} else if i := strings.Index(line, ","); i >= 0 {
			block = line[:i]
		}
		for _, b := range strings.Split(block, ",") {
			b = strings.TrimSpace(b)
			if i := strings.Index(b, " ["); i >= 0 {
				b = b[:i]
			}
			p, err := netip.ParsePrefix(b)
			if err != nil {
				t.Fatalf("registry line %q: %v", line, err)
			}
			out = append(out, p)
		}
	}
	return out
}

// lastOf is the highest address of p.
func lastOf(p netip.Prefix) netip.Addr {
	b := p.Masked().Addr().AsSlice()
	for i := p.Bits(); i < len(b)*8; i++ {
		b[i/8] |= 0x80 >> (i % 8)
	}
	a, _ := netip.AddrFromSlice(b)
	return a
}

// productionGate is the gate New builds, with no deployment list (the registry rules alone).
func productionGate() *gate { return &gate{port: deliveryPort} }

func on443(a netip.Addr) netip.AddrPort { return netip.AddrPortFrom(a, 443) }

// testReachable is the rule restated by the TEST, independently of allow.go: in no registry entry
// (the test's transcription), IPv4 not multicast, IPv6 inside 2000::/3 minus the three carve-outs.
func testReachable(entries []netip.Prefix, a netip.Addr) bool {
	a = a.Unmap()
	for _, p := range entries {
		if p.Contains(a) {
			return false
		}
	}
	if a.Is4() {
		return !netip.MustParsePrefix("224.0.0.0/4").Contains(a)
	}
	if !netip.MustParsePrefix("2000::/3").Contains(a) {
		return false
	}
	for _, c := range []string{"2001::/23", "3fff::/20", "2002::/16"} {
		if netip.MustParsePrefix(c).Contains(a) {
			return false
		}
	}
	return true
}

// TestEgress_TheTableIsTheRegistry: production's table is the registry's two exports, entry by entry,
// in their order -- nothing chosen, nothing left out (ADR 0028 §7: "birebir alınır, elle seçilmez").
//
// MUTANT (measured): any one line of ianaSpecialPurpose deleted -> red here, naming the entry.
func TestEgress_TheTableIsTheRegistry(t *testing.T) {
	want := registryEntries(t)
	if len(want) != 51 {
		t.Fatalf("CONTROL: the transcription parsed %d entries, want 51 (25 IPv4 rows, one of them two entries, + 25 IPv6)", len(want))
	}
	got := ianaSpecialPurpose
	for i := 0; i < max(len(want), len(got)); i++ {
		var w, g string
		if i < len(want) {
			w = want[i].String()
		}
		if i < len(got) {
			g = got[i].String()
		}
		if w != g {
			t.Fatalf("entry %d: the registry says %q, the table says %q", i, w, g)
		}
	}
}

// TestEgress_RefusesEveryIANASpecialPurposeEntry: for EVERY registry entry its first and its last
// address (and its middle) are refused on port 443, Unmap()ed or not; the address just outside each
// end is accepted whenever the rule, restated by this test, says it is public -- the list refuses the
// registry and not a neighbour. Then named literals either side of the well-known blocks.
//
// MUTANTS (measured): an entry not covered by another one removed from ianaSpecialPurpose (e.g.
// 192.31.196.0/24, 2620:4f:8000::/48, 5f00::/16) -> red here; the IPv4 multicast rule removed -> red
// (224.0.0.0); the rule replaced by IsGlobalUnicast -> red (TestEgress_IsGlobalUnicastIsNotTheRule
// too).
func TestEgress_RefusesEveryIANASpecialPurposeEntry(t *testing.T) {
	g := productionGate()
	entries := registryEntries(t)
	accepted := 0
	for _, p := range entries {
		first, last := p.Masked().Addr(), lastOf(p)
		for _, a := range []netip.Addr{first, last} {
			if g.allowed(on443(a)) {
				t.Errorf("%s (of %s) is allowed", a, p)
			}
			if a.Is4() {
				if mapped := netip.AddrFrom16(a.As16()); g.allowed(on443(mapped)) {
					t.Errorf("%s (v4-mapped, of %s) is allowed: Unmap() is not applied", mapped, p)
				}
			}
		}
		for _, n := range []netip.Addr{first.Prev(), last.Next()} {
			if !n.IsValid() {
				continue
			}
			want := testReachable(entries, n)
			if got := g.allowed(on443(n)); got != want {
				t.Errorf("neighbour %s of %s: allowed %v, want %v", n, p, got, want)
			}
			if want {
				accepted++
			}
		}
	}
	if accepted < 30 {
		t.Fatalf("CONTROL: only %d neighbours were public; the boundary half is not measuring", accepted)
	}
	for _, c := range []struct {
		addr string
		ok   bool
	}{
		{"8.8.8.8", true}, {"1.1.1.1", true}, {"9.255.255.255", true}, {"11.0.0.0", true},
		{"100.63.255.255", true}, {"100.128.0.0", true}, {"126.255.255.255", true}, {"128.0.0.0", true},
		{"169.253.255.255", true}, {"169.255.0.0", true}, {"172.15.255.255", true}, {"172.32.0.0", true},
		{"192.167.255.255", true}, {"192.169.0.0", true}, {"198.17.255.255", true}, {"198.20.0.0", true},
		{"223.255.255.255", true}, {"224.0.0.0", false}, {"239.255.255.255", false}, {"169.254.169.254", false},
		{"100.100.100.200", false}, {"2606:4700:4700::1111", true}, {"2a00:1450::1", true},
		{"1fff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", false}, {"4000::", false}, {"ff02::1", false},
		{"fd00:ec2::254", false}, {"2001:200::1", true}, {"2001:1ff:ffff::", false},
	} {
		if got := g.allowed(on443(netip.MustParseAddr(c.addr))); got != c.ok {
			t.Errorf("%s: allowed %v, want %v", c.addr, got, c.ok)
		}
	}
}

// TestEgress_IPv6OnlyGlobalUnicastMinusCarveOuts: outside 2000::/3 nothing is allowed; inside it the
// three carve-outs are refused end to end; and every form that embeds an IPv4 address -- v4-mapped,
// IPv4-compatible, NAT64 (both prefixes), 6to4, Teredo -- is refused even when the IPv4 it carries is
// public (it is the private one an attacker would use; the public one is refused all the same).
func TestEgress_IPv6OnlyGlobalUnicastMinusCarveOuts(t *testing.T) {
	g := productionGate()
	for _, s := range []string{
		"::", "::1", "::a00:1", "::808:808", // unspecified, loopback, IPv4-compatible (10.0.0.1, 8.8.8.8)
		"::ffff:10.0.0.1", "::ffff:127.0.0.1", "::ffff:169.254.169.254", // v4-mapped
		"64:ff9b::a00:1", "64:ff9b::808:808", "64:ff9b:1::a00:1", // NAT64
		"2002:a00:1::", "2002:808:808::1", // 6to4 of 10.0.0.1 and of 8.8.8.8
		"2001:0:4136:e378:8000:63bf:f5ff:fffe",             // Teredo
		"2001::", "2001:1ff:ffff:ffff:ffff:ffff:ffff:ffff", // 2001::/23 end to end
		"3fff::", "3fff:fff:ffff:ffff:ffff:ffff:ffff:ffff", // 3fff::/20 end to end
		"2002::", "2002:ffff:ffff:ffff:ffff:ffff:ffff:ffff", // 2002::/16 end to end
		"fc00::1", "fe80::1", "ff05::2", "100::1", "5f00::1", "2001:db8::1",
		"1fff::1", "4000::1", "8000::1", "e000::1",
	} {
		if g.allowed(on443(netip.MustParseAddr(s))) {
			t.Errorf("%s is allowed", s)
		}
	}
	// A zone (a link-scoped address) is refused whatever the address.
	if g.allowed(on443(netip.MustParseAddr("2606:4700::1").WithZone("eth0"))) {
		t.Error("a zoned address is allowed")
	}
	for _, s := range []string{"2000::1", "2001:200::1", "2003::1", "2620:4f:7fff:ffff::1", "2620:4f:8001::",
		"3ffe:ffff::1", "3fff:1000::", "3fff:ffff::1"} {
		if !g.allowed(on443(netip.MustParseAddr(s))) {
			t.Errorf("%s is refused: it is global unicast outside every carve-out", s)
		}
	}
}

// TestEgress_IsGlobalUnicastIsNotTheRule: netip.Addr.IsGlobalUnicast says true for addresses the rule
// refuses -- NAT64, 6to4, Teredo, documentation, the private IPv4 ranges. This pins the reason the rule
// does not lean on it (ADR 0028 §7) and is the measurement behind the IsGlobalUnicast mutant.
func TestEgress_IsGlobalUnicastIsNotTheRule(t *testing.T) {
	g := productionGate()
	n := 0
	for _, s := range []string{"64:ff9b::a00:1", "2002:a00:1::", "2001::1", "2001:db8::1", "10.0.0.1",
		"192.168.1.1", "100.64.0.1", "fc00::1", "3fff::1", "198.18.0.1"} {
		a := netip.MustParseAddr(s)
		if !a.IsGlobalUnicast() {
			continue
		}
		n++
		if g.allowed(on443(a)) {
			t.Errorf("%s: IsGlobalUnicast says true and the gate agrees -- the gate must refuse it", s)
		}
	}
	if n < 8 {
		t.Fatalf("CONTROL: IsGlobalUnicast said true for only %d of the samples", n)
	}
}

// TestEgress_NodeAndIngressAddressesRefused: the deployment's own addresses are refused even though
// they are public -- a single address and a range, IPv4 and IPv6, and a v4-mapped spelling of a
// configured IPv4 address; an address next to them is not.
func TestEgress_NodeAndIngressAddressesRefused(t *testing.T) {
	c, err := New(Options{Blocked: []netip.Prefix{
		netip.MustParsePrefix("51.15.20.30/32"), netip.MustParsePrefix("2a01:4f8:1::/64"),
		netip.MustParsePrefix("::ffff:62.210.0.0/112"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"51.15.20.30", "::ffff:51.15.20.30", "2a01:4f8:1::1", "2a01:4f8:1:0:ffff::",
		"62.210.0.1", "62.210.255.255"} {
		if c.gate.allowed(on443(netip.MustParseAddr(s))) {
			t.Errorf("%s is allowed: it is the deployment's own", s)
		}
	}
	for _, s := range []string{"51.15.20.31", "2a01:4f8:2::1", "62.211.0.1"} {
		if !c.gate.allowed(on443(netip.MustParseAddr(s))) {
			t.Errorf("%s is refused: CONTROL -- it is not on the list", s)
		}
	}
}

// TestEgress_EmptyNodeListRefusesToStart: New refuses an empty list unless the caller says this is
// development; config.Load refuses it outside dev (TestConfig_WebhookSettingsAreRequiredOutsideDev).
func TestEgress_EmptyNodeListRefusesToStart(t *testing.T) {
	if _, err := New(Options{}); err != ErrNoBlockedList {
		t.Fatalf("an empty list: %v", err)
	}
	c, err := New(Options{DevelopmentWithoutBlockedList: true})
	if err != nil || c == nil {
		t.Fatalf("CONTROL development: %v", err)
	}
	if c.gate.allowed(on443(netip.MustParseAddr("127.0.0.1"))) {
		t.Fatal("development without a list allows loopback")
	}
}

// TestEgress_OnlyPort443: the same public address on any other port is refused.
func TestEgress_OnlyPort443(t *testing.T) {
	g := productionGate()
	a := netip.MustParseAddr("8.8.8.8")
	if !g.allowed(netip.AddrPortFrom(a, 443)) {
		t.Fatal("CONTROL: 8.8.8.8:443 refused")
	}
	for _, p := range []uint16{0, 80, 444, 8443, 5432, 6443} {
		if g.allowed(netip.AddrPortFrom(a, p)) {
			t.Errorf("port %d allowed", p)
		}
	}
}
