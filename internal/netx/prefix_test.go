package netx

import (
	"fmt"
	"math/big"
	"net/netip"
	"testing"
)

// theComplementOf10Slash8 is the eight prefixes that cover every IPv4 address
// EXCEPT 10.0.0.0/8, and theComplementOf25Slash8 the same eight for 25.0.0.0/8.
// They differ by one digit and by nothing else, which is the whole finding: the
// round-4 predicate refused the first (because 10/8 was one of thirteen names it
// carried) and ACCEPTED the second.
var (
	theComplementOf10Slash8 = []string{
		"11.0.0.0/8", "8.0.0.0/7", "12.0.0.0/6", "0.0.0.0/5",
		"16.0.0.0/4", "32.0.0.0/3", "64.0.0.0/2", "128.0.0.0/1",
	}
	theComplementOf25Slash8 = []string{
		"128.0.0.0/1", "64.0.0.0/2", "32.0.0.0/3", "0.0.0.0/4",
		"16.0.0.0/5", "28.0.0.0/6", "26.0.0.0/7", "24.0.0.0/8",
	}
	// theComplementOf192Dot0Dot2Slash24 leaves out exactly 192.0.2.0/24 — RFC 5737
	// TEST-NET-1, which is never routed and which no client can present. Twenty-four
	// lines, and the panel's cap is 32 (handler.maxStaticRanges, in
	// internal/handler/locations.go), so the field holds it with room to spare. Under
	// the round-4 predicate this eliminated NOBODY and was accepted.
	theComplementOf192Dot0Dot2Slash24 = []string{
		"0.0.0.0/1", "128.0.0.0/2", "224.0.0.0/3", "208.0.0.0/4",
		"200.0.0.0/5", "196.0.0.0/6", "194.0.0.0/7", "193.0.0.0/8",
		"192.128.0.0/9", "192.64.0.0/10", "192.32.0.0/11", "192.16.0.0/12",
		"192.8.0.0/13", "192.4.0.0/14", "192.2.0.0/15", "192.1.0.0/16",
		"192.0.128.0/17", "192.0.64.0/18", "192.0.32.0/19", "192.0.16.0/20",
		"192.0.8.0/21", "192.0.4.0/22", "192.0.0.0/23", "192.0.3.0/24",
	}
)

func mustPrefixes(t *testing.T, ss ...string) []netip.Prefix {
	t.Helper()
	out := make([]netip.Prefix, 0, len(ss))
	for _, s := range ss {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			t.Fatalf("fixture %q: %v", s, err)
		}
		out = append(out, p)
	}
	return out
}

// TestTooWideForProofOfPlace is the table for backlog T40: a venue whose address
// ranges match essentially everybody writes "network proof of place" into an
// IMMUTABLE transaction row for a tap that could have come from anywhere
// (CLAUDE.md §4.3, §5 row 6).
//
// The rows are organised as the check is: one entry that is already too wide, a
// TOTAL that gets there across entries, the three measured exploit spellings, the
// boundary either side of the limit, and — as the positive control that keeps the
// check from being a blanket refusal — the ordinary ranges a real venue is
// configured with, which must all still be accepted.
//
// 🔴 SEVEN ROWS CHANGED ANSWER IN THE 5th ROUND, AND THAT IS THE HONEST SIGNAL THAT
// THE PREDICATE MOVED. The question used to be "does this list leave any address
// out?" (round 3: the whole address space; round 4: the space minus thirteen named
// blocks). It is now "is this list wider than a venue's network can be?" — see the
// limit on maxProofHostBitsV4. Under the old question, "one half of IPv4 alone",
// "three quarters", "31 of 32 fifths", "the same half four times" and "all of IPv6
// global unicast" were all ACCEPTED, because each one leaves something out. Under
// the new one they are refused, and a list that leaves NOTHING out is simply the far
// end of the same scale rather than a separate rule. Each flipped row is kept with
// its answer changed and its reason written down, because a deleted row is a
// property nobody can see was traded away.
func TestTooWideForProofOfPlace(t *testing.T) {
	t.Parallel()

	// everyFifth is the 32 blocks of a /5 partition of IPv4, in order. Taking all 32
	// is exactly the whole space; taking 31 is one block short of it.
	everyFifth := make([]string, 0, 32)
	for i := 0; i < 32; i++ {
		everyFifth = append(everyFifth, fmt.Sprintf("%d.0.0.0/5", i*8))
	}
	// everyFifthBut56 is the same partition with 56.0.0.0/5 (56.0.0.0 – 63.255.255.255)
	// removed — 134 217 728 ordinary public addresses.
	everyFifthBut56 := make([]string, 0, 31)
	for i, s := range everyFifth {
		if i != 7 {
			everyFifthBut56 = append(everyFifthBut56, s)
		}
	}

	for _, tc := range []struct {
		name string
		in   []string
		want bool
		// wantNamed is the entry the refusal points at, or "" when it takes the whole
		// list to get over the limit and no single entry can be blamed.
		wantNamed string
	}{
		// ---- a single entry that is already too wide -------------------------
		{name: "0.0.0.0/0", in: []string{"0.0.0.0/0"}, want: true, wantNamed: "0.0.0.0/0"},
		{name: "::/0", in: []string{"::/0"}, want: true, wantNamed: "::/0"},
		{
			name: "a real range NEXT TO a default route is still a refusal",
			in:   []string{"192.168.1.0/24", "0.0.0.0/0"},
			want: true, wantNamed: "0.0.0.0/0",
		},
		{
			// 🔴 FLIPPED IN THE 5th ROUND, AND IT IS THE SAME FLIP AS THE UNION BELOW.
			// Half of IPv4 leaves the other half out, so the coverage question accepted
			// it ("wide, and deliberately still accepted"). Half of IPv4 is 2 147 483 648
			// addresses — 64 times the limit and 8 million times the widest range any of
			// 331 499 venues actually stores. It is not a venue.
			name: "one half of IPv4 alone",
			in:   []string{"0.0.0.0/1"},
			want: true, wantNamed: "0.0.0.0/1",
		},
		{
			// FLIPPED for the same reason: three quarters is one quarter short of
			// everything and 96 times the limit.
			name: "three quarters of IPv4",
			in:   []string{"0.0.0.0/2", "64.0.0.0/2", "128.0.0.0/2"},
			want: true, wantNamed: "0.0.0.0/2",
		},

		// ---- the exploit spellings, all three, measured on this tree ---------
		{
			// backlog T2's spelling, closed in round 3. Each /1 is over the limit on
			// its own now, so the manager is pointed at a LINE rather than at "the list".
			name: "the two halves of IPv4 — backlog T2's spelling",
			in:   []string{"0.0.0.0/1", "128.0.0.0/1"},
			want: true, wantNamed: "0.0.0.0/1",
		},
		{
			// EXPLOIT 1 OF 3, closed in round 4 because 10.0.0.0/8 was one of the
			// thirteen names the predicate carried.
			name: "the eight lines that leave out only 10.0.0.0/8",
			in:   theComplementOf10Slash8,
			// Named 12.0.0.0/6 and not 11.0.0.0/8 or 8.0.0.0/7: those two are AT or
			// under the limit, which is the naming loop behaving as its comment says.
			want: true, wantNamed: "12.0.0.0/6",
		},
		{
			// 🔴 EXPLOIT 2 OF 3 — THE SAME EIGHT LINES ONE DIGIT APART, AND THE ONE THAT
			// SURVIVED ROUND 4. Measured before this repair: parseRanges accepted all
			// eight, SaveVenue stored them, and taps from 203.0.113.7 / 8.8.8.8 /
			// 81.4.1.9 / 195.158.75.3 came back verdict=ok ip_match=true trust=70 on NFC
			// AND on QR — base:qr-requires-ip switched off by a panel field.
			name: "the eight lines that leave out only 25.0.0.0/8",
			in:   theComplementOf25Slash8,
			want: true, wantNamed: "128.0.0.0/1",
		},
		{
			// 🔴 EXPLOIT 3 OF 3 — THE ONE THAT ELIMINATES NOBODY AT ALL. The single
			// omitted block is RFC 5737 TEST-NET-1, which is never routed, so this list
			// matches every client on earth in 24 lines and no "/0" anywhere.
			name: "the 24 lines that leave out only 192.0.2.0/24 (TEST-NET-1)",
			in:   theComplementOf192Dot0Dot2Slash24,
			want: true, wantNamed: "0.0.0.0/1",
		},
		{
			// THE LARGEST PARTITION A VENUE COULD SPELL: the panel's cap is 32
			// (handler.maxStaticRanges), so 32 × /5 is exactly the widest union the
			// panel can hold.
			name: "all 32 blocks of a /5 partition",
			in:   everyFifth,
			want: true, wantNamed: "0.0.0.0/5",
		},
		{
			// FLIPPED. One block short of the address space, and 128 times the limit.
			name: "31 blocks, and the missing one holds no client",
			in:   everyFifth[:31],
			want: true, wantNamed: "0.0.0.0/5",
		},
		{
			// FLIPPED, AND THIS IS THE ROW THAT USED TO CARRY "one block short is still
			// accepted". Omitting 134 million real clients still leaves 4.16 BILLION,
			// which is the point the width question makes and the coverage question
			// could not: "leaves somebody out" was never the property that mattered.
			name: "31 blocks, and the missing one is 134 million real clients",
			in:   everyFifthBut56,
			want: true, wantNamed: "0.0.0.0/5",
		},
		{
			name: "the two halves of IPv6",
			in:   []string{"::/1", "8000::/1"},
			want: true, wantNamed: "::/1",
		},
		{
			// The families are counted apart: a v6 list over its own limit is refused
			// whatever the v4 side says, and vice versa.
			name: "all of v6 plus one ordinary v4 subnet",
			in:   []string{"::/1", "8000::/1", "10.0.0.0/8"},
			want: true, wantNamed: "::/1",
		},
		{
			name: "an ordinary v4 subnet beside a v6 range over the v6 limit",
			in:   []string{"192.168.1.0/24", "2001:db8::/30"},
			want: true, wantNamed: "2001:db8::/30",
		},
		{
			// FLIPPED. 2000::/3 is every routable address in IPv6 — 2^28 times the
			// limit. The coverage question accepted it because v6's unassigned space
			// is not covered; the width question does not care what is left over.
			name: "all of IPv6 global unicast",
			in:   []string{"2000::/3"},
			want: true, wantNamed: "2000::/3",
		},

		// ---- the TOTAL, where no single entry can be blamed -------------------
		{
			// 🔴 THE ROW THAT KEEPS THE SUM ALIVE. Three /8s: each one is at or under the
			// limit, so the naming loop finds nothing, and only the total (50 331 648)
			// gets over. Without the sum this list would pass, and it is the shape every
			// exploit above degrades into once the obvious lines are gone.
			name: "three ISP allocations added together",
			in:   []string{"10.0.0.0/8", "11.0.0.0/8", "12.0.0.0/8"},
			want: true,
		},
		{
			// AND ITS BOUNDARY: two /8s are exactly the limit and must be accepted, or
			// the row above is measuring the limit rather than the sum.
			name: "two ISP allocations are exactly the limit",
			in:   []string{"10.0.0.0/8", "11.0.0.0/8"},
			want: false,
		},

		// ---- the boundary, either side, per family ---------------------------
		{name: "a /7 is exactly the v4 limit", in: []string{"0.0.0.0/7"}, want: false},
		{
			name: "a /6 is one bit over it", in: []string{"0.0.0.0/6"},
			want: true, wantNamed: "0.0.0.0/6",
		},
		{name: "a /31 is exactly the v6 limit", in: []string{"2001:db8::/31"}, want: false},
		{
			name: "a /30 is one bit over it", in: []string{"2001:db8::/30"},
			want: true, wantNamed: "2001:db8::/30",
		},

		// ---- double counting: the containment rule still has to hold ---------
		{
			// A DUPLICATE MUST NOT ADD UP TWICE. Five copies of one /8 sum to 83 886 080
			// without the containment rule — over the limit — and to 16 777 216 with it.
			// Refusing here would refuse a PASTE rather than a claim.
			name: "the same ISP allocation five times is still one allocation",
			in: []string{
				"10.0.0.0/8", "10.0.0.0/8", "10.0.0.0/8", "10.0.0.0/8", "10.0.0.0/8",
			},
			want: false,
		},
		{
			// NESTED ENTRIES MUST NOT DOUBLE-COUNT EITHER: two /8s are exactly the
			// limit, and a /9 sitting inside one of them adds nothing. Without the
			// containment rule this would be 2^25 + 2^23 and refused.
			name: "a nested subnet inside one of two allocations",
			in:   []string{"10.0.0.0/8", "11.0.0.0/8", "10.0.0.0/9"},
			want: false,
		},

		// ---- positive control: the ordinary configurations must survive ------
		{name: "empty is a configuration, not a claim", in: nil, want: false},
		{name: "a single office address", in: []string{"192.168.1.5/32"}, want: false},
		{
			// MEASURED: the widest range list any of 331 499 venues stores
			// ({81.240.16.8/29, 192.168.1.0/24} = 264 addresses, 42 venues).
			name: "the widest list any real venue in the data stores",
			in:   []string{"81.240.16.8/29", "192.168.1.0/24"},
			want: false,
		},
		{name: "an ISP block — the /29 the T40 repair must not break", in: []string{"81.240.16.8/29"}, want: false},
		{name: "an ordinary subnet", in: []string{"192.168.1.0/24"}, want: false},
		{
			name: "several ordinary subnets",
			in:   []string{"203.0.113.0/24", "198.51.100.0/24", "192.0.2.0/24"},
			want: false,
		},
		{name: "a /16", in: []string{"172.20.0.0/16"}, want: false},
		{
			// THE UNIT THE LIMIT IS DERIVED FROM: a /8, an ISP's whole allocation, is
			// the widest single thing this package has ever been willing to call a
			// venue, and it stays accepted.
			name: "a /8 is wide and is NOT refused",
			in:   []string{"10.0.0.0/8"},
			want: false,
		},
		{
			// 🔴 THE SHAPE THE DOUBLING EXISTS FOR. A wholly private deployment — a
			// corporate 10/8 beside a 192.168/16 guest LAN — covers 16 842 752
			// addresses, which is 65 536 MORE than a bare /8. With the limit at a /8 it
			// would be refused by a rounding, and a refusal here is a save the manager
			// cannot complete.
			name: "a fully private deployment: 10/8 beside 192.168/16",
			in:   []string{"10.0.0.0/8", "192.168.0.0/16"},
			want: false,
		},
		{
			// AND ALL OF RFC 1918, which is the widest shape anyone argues is
			// legitimate: 17 891 328 addresses, still inside the limit.
			name: "the whole of RFC 1918 is accepted",
			in:   []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
			want: false,
		},
		{
			// The v6 unit the v6 limit is derived from: a /32 is the minimum an RIR
			// allocates to an LIR, and it is the v6 range this repository's fixtures use.
			name: "one LIR allocation of IPv6 is accepted",
			in:   []string{"2001:db8::/32"},
			want: false,
		},
		{
			name: "several venues' worth of real ranges",
			in:   []string{"192.168.1.0/24", "10.0.0.0/8", "81.240.16.8/29", "2001:db8::/32"},
			want: false,
		},
		{
			// MEASURED (2026-08-19), in Go and in Postgres: a 4-in-6 prefix contains
			// no ordinary v4 client, so it is inert rather than wide. It cannot
			// manufacture proof, and at 2^32 addresses it is far under the v6 limit.
			name: "the 4-in-6 spelling is inert, not wide",
			in:   []string{"::ffff:0.0.0.0/96"},
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			named, got := TooWideForProofOfPlace(mustPrefixes(t, tc.in...))
			if got != tc.want {
				t.Fatalf("TooWideForProofOfPlace(%v) = %v, want %v", tc.in, got, tc.want)
			}
			if !got {
				return
			}
			if tc.wantNamed == "" {
				if named.IsValid() {
					t.Errorf("a total refusal blamed the single entry %v; no one entry is over the "+
						"limit here", named)
				}
				return
			}
			if named.String() != tc.wantNamed {
				t.Errorf("the refusal named %v, want %v — the manager is pointed at the wrong line",
					named, tc.wantNamed)
			}
		})
	}
}

// TestProofWidthLimitsSitBetweenTheMeasuredExtremes is the RATCHET on the two
// numbers, and it is the half a table of cases cannot carry.
//
// 🔴 WHY IT EXISTS. maxProofHostBitsV4/V6 are the only numbers in this package, and
// a number that no gate binds is a number that drifts (this repository has paid for
// that at least four times — see the Makefile's SKIP-count paragraph). The table
// above would stay green under a limit raised to 2^31: every listed exploit is at
// or above 2^32 − 256 only because the exploits are TOTAL covers. This test states
// the two ends explicitly instead — the widest thing that must be accepted and the
// narrowest thing that must be refused — so widening the limit past the exploit
// floor, or narrowing it below the legitimate ceiling, is a red test with a sentence
// attached.
//
// 🔴 AND IT RECORDS WHERE IN THE GAP THE LIMIT SITS, WHICH IS A DECISION AND NOT AN
// ACCIDENT. The gap runs from 17 891 328 (all of RFC 1918) to 4 278 190 080 (the
// narrowest measured exploit) — a factor of 239. The limit is the smallest power of
// two above the LEGITIMATE ceiling, not the middle of the gap, because the two
// errors are not symmetric: accepting a list that is too wide writes a false
// sentence into an IMMUTABLE row (§4.3) and nobody ever sees it, while refusing a
// legitimate one is a save failure a manager reads on screen and can report.
func TestProofWidthLimitsSitBetweenTheMeasuredExtremes(t *testing.T) {
	t.Parallel()

	pow := func(n uint) *big.Int { return new(big.Int).Lsh(big.NewInt(1), n) }
	sum := func(ss ...string) *big.Int {
		out := new(big.Int)
		for _, s := range ss {
			out.Add(out, blockSize(netip.MustParsePrefix(s)))
		}
		return out
	}

	for _, tc := range []struct {
		name   string
		bitLen int
		size   *big.Int
		// accept says the limit must be at least this wide; otherwise it must be
		// strictly narrower than this.
		accept bool
	}{
		{
			name:   "all of RFC 1918 — the widest shape argued to be legitimate",
			bitLen: 32, size: sum("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"), accept: true,
		},
		{
			name:   "a corporate /8 beside a 192.168/16 guest LAN",
			bitLen: 32, size: sum("10.0.0.0/8", "192.168.0.0/16"), accept: true,
		},
		{
			name:   "the widest list any of 331 499 venues actually stores (2026-08-20)",
			bitLen: 32, size: sum("81.240.16.8/29", "192.168.1.0/24"), accept: true,
		},
		{
			name:   "the narrowest measured exploit: everything but one /8",
			bitLen: 32, size: new(big.Int).Sub(pow(32), pow(24)), accept: false,
		},
		{
			name:   "everything but one /24 (TEST-NET-1)",
			bitLen: 32, size: new(big.Int).Sub(pow(32), big.NewInt(256)), accept: false,
		},
		{name: "half of IPv4", bitLen: 32, size: pow(31), accept: false},
		{
			name:   "one LIR allocation of IPv6 (a /32)",
			bitLen: 128, size: pow(96), accept: true,
		},
		{
			name:   "an LIR allocation beside a site's own ULA /48",
			bitLen: 128, size: sum("2001:db8::/32", "fd00:1234:5678::/48"), accept: true,
		},
		{name: "all of IPv6 global unicast (2000::/3)", bitLen: 128, size: pow(125), accept: false},
		{name: "all of IPv6", bitLen: 128, size: pow(128), accept: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			limit := maxProofAddresses(tc.bitLen)
			switch {
			case tc.accept && limit.Cmp(tc.size) < 0:
				t.Errorf("the /%d limit is %v, below %v — this shape is legitimate and a venue "+
					"spelling it could not save at all", tc.bitLen, limit, tc.size)
			case !tc.accept && limit.Cmp(tc.size) >= 0:
				t.Errorf("the /%d limit is %v, at or above %v — a list of that width would be "+
					"accepted and every tap at that venue would be recorded as proven by the "+
					"network (§4.3: the row can never be corrected)", tc.bitLen, limit, tc.size)
			}
		})
	}
}

// TestBlockSizeCountsTheAddressesAPrefixHolds pins the one piece of arithmetic every
// other assertion in this file rests on. A blockSize that came out LOW would let a
// list that covers the world read as if it fitted, and the failure would look
// exactly like a correct acceptance.
func TestBlockSizeCountsTheAddressesAPrefixHolds(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in   string
		want *big.Int
	}{
		{"0.0.0.0/0", new(big.Int).Lsh(big.NewInt(1), 32)},
		{"0.0.0.0/7", new(big.Int).Lsh(big.NewInt(1), 25)},
		{"10.0.0.0/8", new(big.Int).Lsh(big.NewInt(1), 24)},
		{"192.168.1.0/24", big.NewInt(256)},
		{"81.240.16.8/29", big.NewInt(8)},
		{"192.168.1.5/32", big.NewInt(1)},
		{"::/0", new(big.Int).Lsh(big.NewInt(1), 128)},
		{"2001:db8::/31", new(big.Int).Lsh(big.NewInt(1), 97)},
		{"2001:db8::/32", new(big.Int).Lsh(big.NewInt(1), 96)},
		{"2001:db8::1/128", big.NewInt(1)},
	} {
		if got := blockSize(netip.MustParsePrefix(tc.in)); got.Cmp(tc.want) != 0 {
			t.Errorf("blockSize(%s) = %v, want %v", tc.in, got, tc.want)
		}
	}

	// AND THE LIMITS ARE THE PREFIXES THE COMMENT NAMES, asserted rather than
	// described: a /7 in IPv4 and a /31 in IPv6. A comment that says "/7" beside a
	// constant that says 26 is how the previous three rounds' sentences went stale.
	if got, want := maxProofAddresses(32), blockSize(netip.MustParsePrefix("0.0.0.0/7")); got.Cmp(want) != 0 {
		t.Errorf("the IPv4 limit is %v, but the comment says a /7 (%v)", got, want)
	}
	if got, want := maxProofAddresses(128), blockSize(netip.MustParsePrefix("2001:db8::/31")); got.Cmp(want) != 0 {
		t.Errorf("the IPv6 limit is %v, but the comment says a /31 (%v)", got, want)
	}
}
