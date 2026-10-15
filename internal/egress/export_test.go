package egress

import (
	"crypto/x509"
	"net/netip"
	"time"
)

// export_test.go -- the dial seams, for the EXTERNAL test package of this directory (egress_test) and
// nothing else: a file named *_test.go is compiled only into this package's own test binary, so the
// seams stay closed to every production importer (ADR 0028's API-9 note, item 12). M11 API-10's worker
// tests (webhook_worker_db_test.go) drive internal/webhook/delivery through a REAL Client -- real DNS
// answers from the in-process server, real TLS, real dial-time refusals -- and those tests live here
// because this is the one place the seams reach.

// FakeDNS is the in-process DNS server (dns_test.go).
type FakeDNS = fakeDNS

// NewFakeDNS answers from answer (name, query type, how many times asked before).
func NewFakeDNS(answer func(name string, qtype uint16, n int) ([]netip.Addr, bool)) *FakeDNS {
	return newFakeDNS(answer)
}

// Asked reports how many A and AAAA questions name drew.
func (d *fakeDNS) Asked(name string) int { return d.count(name, typeA) + d.count(name, typeAAAA) }

// SeamClient is a Client whose gate also allows allow (a loopback test server standing in for a public
// address -- the blocked list and the registry still apply to everything else), dials port instead of
// 443, resolves through d and trusts roots. observe, when set, sees every dial-time decision.
func SeamClient(allow []netip.Prefix, port uint16, d *FakeDNS, roots *x509.CertPool, timeout time.Duration,
	observe func(netip.AddrPort, bool)) *Client {
	g := &gate{port: port, testAllow: allow, observe: observe}
	return build(g, d.resolver(), roots, timeout)
}
