package egress

import (
	"errors"
	"net/netip"
)

// THE ALLOW LIST (ADR 0028 §7, "İzin listesi — normatif"). It is applied at DIAL TIME, to the address
// the dialer is about to connect to -- never to a DNS answer, never to the URL -- after Unmap().
//
//	both families  the address is in NO entry of IANA's Special-Purpose Address Registries
//	IPv4           and it is not multicast (224.0.0.0/4 is not in the special-purpose registry;
//	               ADR 0028 §7 names it among the IPv4 blocks that are refused)
//	IPv6           and it is inside 2000::/3, minus the whole of 2001::/23, 3fff::/20 and 2002::/16
//	               (the three are registry entries too; they are written again because ADR 0028
//	               §7 names them as rules of their own)
//	both           and it is not one of the node's own public addresses or the ingress's
//	               (config: TAPPA_WEBHOOK_BLOCKED_ADDRESSES)
//	both           and the port is 443
//
// 🔴 NOT netip.Addr.IsGlobalUnicast. Go answers true for NAT64 (64:ff9b::/96), the IPv4-compatible
// forms, 6to4, Teredo, the documentation blocks and every private IPv4 range -- it is a statement
// about the address's FORMAT, not about whether the internet may route it. Nothing here calls it;
// TestEgress_IsGlobalUnicastIsNotTheRule shows the addresses it would let through.

// ianaSpecialPurpose is IANA's IPv4 and IPv6 Special-Purpose Address Registries, ENTRY BY ENTRY, in
// the registries' own order. Transcribed on 2026-10-15 from the registries' CSV exports
// (https://www.iana.org/assignments/iana-ipv4-special-registry/iana-ipv4-special-registry-1.csv and
// https://www.iana.org/assignments/iana-ipv6-special-registry/iana-ipv6-special-registry-1.csv),
// both of which said "updated 2025-10-09". Nothing is chosen or left out: an entry nested in another
// (0.0.0.0/32 in 0.0.0.0/8, 2001::/32 in 2001::/23) is kept, because the point of the table is that
// it IS the registry and can be compared with it line by line. The registry's "192.0.0.170/32,
// 192.0.0.171/32" row is the two entries it names.
//
// When IANA adds an entry, it is added here and in the test's transcription
// (TestEgress_TheTableIsTheRegistry) in one change.
var ianaSpecialPurpose = []netip.Prefix{
	// IPv4 Special-Purpose Address Registry.
	netip.MustParsePrefix("0.0.0.0/8"),          // "This network" [RFC791]
	netip.MustParsePrefix("0.0.0.0/32"),         // "This host on this network" [RFC1122]
	netip.MustParsePrefix("10.0.0.0/8"),         // Private-Use [RFC1918]
	netip.MustParsePrefix("100.64.0.0/10"),      // Shared Address Space [RFC6598]
	netip.MustParsePrefix("127.0.0.0/8"),        // Loopback [RFC1122]
	netip.MustParsePrefix("169.254.0.0/16"),     // Link Local [RFC3927]
	netip.MustParsePrefix("172.16.0.0/12"),      // Private-Use [RFC1918]
	netip.MustParsePrefix("192.0.0.0/24"),       // IETF Protocol Assignments [RFC6890]
	netip.MustParsePrefix("192.0.0.0/29"),       // IPv4 Service Continuity Prefix [RFC7335]
	netip.MustParsePrefix("192.0.0.8/32"),       // IPv4 dummy address [RFC7600]
	netip.MustParsePrefix("192.0.0.9/32"),       // Port Control Protocol Anycast [RFC7723]
	netip.MustParsePrefix("192.0.0.10/32"),      // Traversal Using Relays around NAT Anycast [RFC8155]
	netip.MustParsePrefix("192.0.0.170/32"),     // NAT64/DNS64 Discovery [RFC8880][RFC7050]
	netip.MustParsePrefix("192.0.0.171/32"),     // NAT64/DNS64 Discovery [RFC8880][RFC7050]
	netip.MustParsePrefix("192.0.2.0/24"),       // Documentation (TEST-NET-1) [RFC5737]
	netip.MustParsePrefix("192.31.196.0/24"),    // AS112-v4 [RFC7535]
	netip.MustParsePrefix("192.52.193.0/24"),    // AMT [RFC7450]
	netip.MustParsePrefix("192.88.99.0/24"),     // Deprecated (6to4 Relay Anycast) [RFC7526]
	netip.MustParsePrefix("192.88.99.2/32"),     // 6a44-relay anycast address [RFC6751]
	netip.MustParsePrefix("192.168.0.0/16"),     // Private-Use [RFC1918]
	netip.MustParsePrefix("192.175.48.0/24"),    // Direct Delegation AS112 Service [RFC7534]
	netip.MustParsePrefix("198.18.0.0/15"),      // Benchmarking [RFC2544]
	netip.MustParsePrefix("198.51.100.0/24"),    // Documentation (TEST-NET-2) [RFC5737]
	netip.MustParsePrefix("203.0.113.0/24"),     // Documentation (TEST-NET-3) [RFC5737]
	netip.MustParsePrefix("240.0.0.0/4"),        // Reserved [RFC1112]
	netip.MustParsePrefix("255.255.255.255/32"), // Limited Broadcast [RFC8190][RFC919]

	// IPv6 Special-Purpose Address Registry.
	netip.MustParsePrefix("::1/128"),           // Loopback Address [RFC4291]
	netip.MustParsePrefix("::/128"),            // Unspecified Address [RFC4291]
	netip.MustParsePrefix("::ffff:0:0/96"),     // IPv4-mapped Address [RFC4291]
	netip.MustParsePrefix("64:ff9b::/96"),      // IPv4-IPv6 Translat. [RFC6052]
	netip.MustParsePrefix("64:ff9b:1::/48"),    // IPv4-IPv6 Translat. [RFC8215]
	netip.MustParsePrefix("100::/64"),          // Discard-Only Address Block [RFC6666]
	netip.MustParsePrefix("100:0:0:1::/64"),    // Dummy IPv6 Prefix [RFC9780]
	netip.MustParsePrefix("2001::/23"),         // IETF Protocol Assignments [RFC2928]
	netip.MustParsePrefix("2001::/32"),         // TEREDO [RFC4380][RFC8190]
	netip.MustParsePrefix("2001:1::1/128"),     // Port Control Protocol Anycast [RFC7723]
	netip.MustParsePrefix("2001:1::2/128"),     // Traversal Using Relays around NAT Anycast [RFC8155]
	netip.MustParsePrefix("2001:1::3/128"),     // DNS-SD Service Registration Protocol Anycast [RFC9665]
	netip.MustParsePrefix("2001:2::/48"),       // Benchmarking [RFC5180][RFC Errata 1752]
	netip.MustParsePrefix("2001:3::/32"),       // AMT [RFC7450]
	netip.MustParsePrefix("2001:4:112::/48"),   // AS112-v6 [RFC7535]
	netip.MustParsePrefix("2001:10::/28"),      // Deprecated (previously ORCHID) [RFC4843]
	netip.MustParsePrefix("2001:20::/28"),      // ORCHIDv2 [RFC7343]
	netip.MustParsePrefix("2001:30::/28"),      // Drone Remote ID Protocol Entity Tags (DETs) Prefix [RFC9374]
	netip.MustParsePrefix("2001:db8::/32"),     // Documentation [RFC3849]
	netip.MustParsePrefix("2002::/16"),         // 6to4 [RFC3056]
	netip.MustParsePrefix("2620:4f:8000::/48"), // Direct Delegation AS112 Service [RFC7534]
	netip.MustParsePrefix("3fff::/20"),         // Documentation [RFC9637]
	netip.MustParsePrefix("5f00::/16"),         // Segment Routing (SRv6) SIDs [RFC9602]
	netip.MustParsePrefix("fc00::/7"),          // Unique-Local [RFC4193][RFC8190]
	netip.MustParsePrefix("fe80::/10"),         // Link-Local Unicast [RFC4291]
}

var (
	// ipv4Multicast is the one IPv4 block ADR 0028 §7 refuses that the special-purpose registry
	// does not list (it has a registry of its own).
	ipv4Multicast = netip.MustParsePrefix("224.0.0.0/4")
	// ipv6GlobalUnicast is the only part of the IPv6 space a delivery may reach.
	ipv6GlobalUnicast = netip.MustParsePrefix("2000::/3")
	// ipv6CarveOuts are ADR 0028 §7's three named refusals inside 2000::/3: the whole IETF
	// protocol-assignment block (Teredo, the anycast addresses, benchmarking, ORCHIDv2, AMT, DETs),
	// the RFC 9637 documentation block and 6to4 (which embeds an IPv4 address).
	ipv6CarveOuts = []netip.Prefix{
		netip.MustParsePrefix("2001::/23"),
		netip.MustParsePrefix("3fff::/20"),
		netip.MustParsePrefix("2002::/16"),
	}
)

// deliveryPort is the one port a delivery may be dialled on (ADR 0028 §7).
const deliveryPort = 443

// errBlocked is what the dial-time gate answers for an address it refuses. It is a fixed sentinel:
// the dialer wraps it in a *net.OpError that names the address, and nothing in this package ever
// lets that text out (a Result carries a Class).
var errBlocked = errors.New("egress: the address is not one a delivery may reach")

// gate is the allow list of one Client: the registry rules above plus the deployment's own blocked
// addresses.
type gate struct {
	// blocked are the node's public addresses and the ingress's (config), checked first.
	blocked []netip.Prefix
	// port is deliveryPort in every Client New builds. A test may move it to a test server's port
	// (export_test.go); the rule "this one port and no other" is the same.
	port uint16
	// testAllow is nil in every Client New builds. A test sets it to the loopback address its
	// TLS server listens on, as a stand-in for "a public address" -- the only way to show a dial
	// SUCCEED without reaching the internet. The blocked list is still checked before it.
	testAllow []netip.Prefix
	// observe is nil in every Client New builds. A test sets it to see each dial-time decision --
	// which address, which answer -- of a Happy Eyeballs race or a multi-address answer.
	observe func(ap netip.AddrPort, allowed bool)
}

// allowed is the whole rule for one address and port. It reads nothing but its arguments.
func (g *gate) allowed(ap netip.AddrPort) bool {
	if ap.Port() != g.port {
		return false
	}
	addr := ap.Addr()
	if addr.Zone() != "" {
		// A zone means a link-scoped address (fe80::1%eth0) -- refused by the registry anyway; a
		// zoned address never compares equal to a prefix's address, so it is refused here first.
		return false
	}
	addr = addr.Unmap()
	if !addr.IsValid() {
		return false
	}
	for _, p := range g.blocked {
		if p.Contains(addr) {
			return false
		}
	}
	for _, p := range g.testAllow {
		if p.Contains(addr) {
			return true
		}
	}
	return globallyReachable(addr)
}

// globallyReachable is the registry half of the rule, for an address that has been Unmap()ed.
func globallyReachable(addr netip.Addr) bool {
	for _, p := range ianaSpecialPurpose {
		if p.Contains(addr) {
			return false
		}
	}
	if addr.Is4() {
		return !ipv4Multicast.Contains(addr)
	}
	if !ipv6GlobalUnicast.Contains(addr) {
		return false
	}
	for _, p := range ipv6CarveOuts {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}
