// Package netx is address-range arithmetic: the one question "is this list of
// ranges too wide to be evidence that somebody was HERE?", asked identically by the
// side that STORES a venue's ranges and the side that READS them during a tap.
//
// It is a PURE package. It imports net/netip and math/big and nothing else — no
// database, no HTTP, no clock, no logging, no configuration — so both
// internal/domain/tenant (storage, CLAUDE.md §3) and internal/domain/tap (the
// decision engine, which §3 requires to know nothing about storage) may depend on
// it without either depending on the other.
//
// 🔴 WHY A NEW PACKAGE RATHER THAN internal/geo, MEASURED RATHER THAN PREFERRED.
// internal/geo was the nearest existing pure neighbour and it was rejected on
// three counts. (1) CLAUDE.md §3's directory map defines that package as
// haversine and GPS radius checking (the map entry is Turkish; this is a
// translation, because §7 keeps Turkish characters out of code) — a map entry is a
// contract about what goes where, and IP prefix arithmetic is not that. (2) Their import sets do not
// overlap at all: geo needs {encoding, fmt, log/slog, math} for coordinates and
// their redaction, this needs {net/netip, math/big}. (3) The decisive one:
// geo.Point carries nine redaction members (Format/String/GoString/LogValue/
// MarshalText and their pointer indirections) because a full coordinate is a §4.7
// secret that must never be printable. A venue's address range is not in that
// class — it is configuration a manager typed and the panel shows back. Putting a
// non-secret type inside a package whose doc says its type must never reach a log
// line would make that package's central sentence false for half its contents,
// which is the "a sentence about a net claims more than the net does" class this
// repository has paid for repeatedly.
package netx

import (
	"math/big"
	"net/netip"
)

// The widest a venue's WHOLE range list may be, per address family, written as the
// number of host bits the limit holds: 2^25 addresses in IPv4 (a /7) and 2^97 in
// IPv6 (a /31).
//
// 🔴 WHY A SIZE AND NOT A LIST OF NAMED BLOCKS — THREE ROUNDS AND THREE SPELLINGS
// PAID FOR THIS SENTENCE. The predicate used to ask "does this list leave any
// address out?", first against the whole address space and then (4th round) against
// a hand-written list of thirteen named blocks that no client can present from
// (RFC 1918, loopback, multicast, CGNAT, …). Both are the SAME mistake: they define
// a space by ENUMERATION, so every block nobody thought to name is a way out. It was
// measured out three times, each time with a list of the same length and shape as
// the one just closed:
//
//	0.0.0.0/1 + 128.0.0.0/1                      -> closed in round 3
//	the 8 prefixes covering everything but 10/8  -> closed in round 4 (10/8 was named)
//	the 8 prefixes covering everything but 25/8  -> STILL ACCEPTED after round 4
//	the 24 prefixes covering everything but      -> STILL ACCEPTED after round 4
//	  192.0.2.0/24 (RFC 5737 TEST-NET-1)
//
// The third and fourth differ from the second by a single digit and by nothing else.
// Measured on this tree before the repair: handler.parseRanges answered
// `accepted=8 refused=""`, tenant.SaveVenue stored the list, and a tap from
// 203.0.113.7, 8.8.8.8, 81.4.1.9 or 195.158.75.3 — on NFC **and on QR** — came back
//
//	verdict=ok sid=base:ip-or-gps-ok ip_match=true trust=70
//	note="network proof of place: the source IP matches the location"
//
// The QR line is the one that matters: §5 says "QR: no SUN, so an IP match is
// REQUIRED, GPS alone is not enough → flag", and base:qr-requires-ip is the only
// brake on a photographed QR code that never expires. A list of that shape switched
// it off. And transactions are IMMUTABLE (§4.3), so every row it produced carries
// that sentence forever.
//
// 🔴 THE LIMIT IS THE ONE THIS FILE ALREADY STATED IN PROSE, NOW ENFORCED. The
// paragraph below used to read "a /8, an ISP's whole allocation, is accepted" while
// the code said "anything but those thirteen blocks". The prose was the better rule
// of the two, so the code moved to it: a list may cover at most ONE ISP ALLOCATION
// per family, doubled.
//
//   - IPv4: an ISP allocation is a /8 (2^24) — the largest single block an RIR ever
//     handed out, and the widest thing this file has ever been willing to call a
//     venue. The limit is a /7 because the DOUBLING is load-bearing rather than
//     decorative: a wholly private deployment naming "10.0.0.0/8 beside
//     192.168.0.0/16" covers 16 842 752 addresses, which exceeds a bare /8 by
//     65 536, and refusing that save would be a refusal invented out of a rounding.
//   - IPv6: an ISP allocation is a /32 (2^96) — the minimum an RIR allocates to an
//     LIR, where an end site gets a /48 and one LAN is a /64. The unit does NOT
//     transfer by prefix length (a v6 /8 is 2^88 ISP allocations) and it does not
//     transfer by fraction either (1/256 of IPv6 is absurd), which is why it is
//     re-derived from what the number MEANS. Same doubling, same reason: a /32
//     beside a site's own ULA /48 must fit.
//
// 🔴 MEASURED AGAINST EVERY VENUE, BECAUSE A FALSE POSITIVE HERE IS A SAVE THE
// MANAGER CANNOT COMPLETE. The shipped function was run over every DISTINCT range
// list on the development database, weighted by venue count. 2026-08-20: 338 463
// venues, 217 575 of them carrying a range, spelling 19 distinct lists between them.
//
//	REFUSED  4 lists /  57 venues   {0.0.0.0/0}×49 · {0.0.0.0/1,128.0.0.0/1}×5 ·
//	                                {::/0}×2 · {192.168.1.0/24,0.0.0.0/0}×1
//	ACCEPTED 15 lists / 217 518 venues
//
// All 57 refused venues are this card's own residue and they say so in their names —
// "universal" (42), "St Julians" (11), "Everywhere" (4), created in bursts across the
// audit rounds.
//
// 🔴 "A REAL VENUE" HAS TO BE DEFINED BEFORE IT CAN BE COUNTED, AND THE DRAFT THIS
// REPLACES SKIPPED THAT STEP AND SHIPPED A NUMBER THAT DOES NOT REPRODUCE. It said
// "the widest list any real venue stores is {81.240.16.8/29, 192.168.1.0/24} = 264
// addresses (44 venues) … the v4 limit is 127 100 times that widest real list". The
// ratio was arithmetic about the wrong list: the sweep above counts residue only on
// the REFUSED side and had never asked the same question about the ACCEPTED side.
// There is no production venue on this database at all — every row is seeded or
// test-written — so the honest answer is three separate facts, each dated 2026-08-20:
//
//   - SEEDED, i.e. what `make seed` puts there (test/fixtures/seed.sql, 11 venues,
//     the only rows here that stand in for production): nine KF venues at a single
//     /32 each, KM Plant at 198.51.100.0/29. Widest = 8 addresses, which is
//     1/4 194 304 of the v4 limit.
//   - PRODUCTION-SHAPED BUT TEST-WRITTEN, widest of that kind:
//     {81.240.16.8/29, 192.168.1.0/24} = 264 addresses, 52 venues, all named "Rusty
//     Bar" — an ISP /29 beside a private /24, which is the shape a real bar has, but
//     the rows are venue_db_test.go's ("a legitimate /29 was refused"). The v4 limit
//     is 127 100 times it, so the old ratio was right about THIS list and wrong about
//     the sentence it was attached to.
//   - WIDEST LIST THIS FUNCTION ACCEPTS ON THIS DATABASE TODAY, which is a different
//     row and 63 798 times wider than the one above: {10.0.0.0/8, 192.168.0.0/16} =
//     16 842 752 addresses, 10 venues, all named "Kebab Manufacturing HQ" under tenant
//     "Kebab Factory Ltd", created 2026-08-20 — this card's OWN control fixture
//     (venue_db_test.go, the "a wholly private deployment was refused" case that pins
//     the doubling argued above). The limit is 1.99 times that list, NOT 127 100 times
//     it, and any sentence claiming otherwise is measuring the seed and calling it the
//     database.
//
// What survives from the old sentence is the part that matters: no venue is refused
// that was not configured to be refused. What does not survive is "the widest thing
// here is 264 addresses" — the widest thing here is half the limit, and this card put
// it there on purpose.
//
// ⚠️ THESE COUNTS GROW WITH EVERY SUITE RUN AND ARE THEREFORE DATED OBSERVATIONS
// RATHER THAN BOUNDS — the total moved 331 499 -> 332 855 -> 338 463 across the last
// three re-measurements of this same round, because the tests write residue venues.
// What the sweep pins is the SET, not the tally: the same 4 shapes are refused as
// under the round-4 predicate, so the width rule refuses a strict SUPERSET of what the
// name list refused, and that superset contains neither a seeded venue nor a
// production-shaped list. The number that IS bound mechanically is the limit itself —
// TestProofWidthLimitsSitBetweenTheMeasuredExtremes fails if it moves below the
// legitimate ceiling or above the exploit floor.
//
// ⚠️ THE ONLY IPv6 EVIDENCE IN THAT DATA IS TWO RESIDUE "::/0" ROWS, so the v6 limit
// is derived from allocation structure and NOT from measured venues, and saying so
// is the point. What IS measured about it: 2001:db8::/32 (one LIR allocation, the v6
// range this repository's own fixtures use) sits at exactly half the limit and is
// accepted; 2000::/3, every routable address in IPv6, is 2^28 times the limit and is
// refused.
//
// ⚠️ COUNTED LIMIT, STATED RATHER THAN IMPLIED — A LIST THAT FITS CAN STILL BE THIN
// EVIDENCE, AND THE "1 IN 128" IS A UNIFORMITY FIGURE RATHER THAN A CLIENT FIGURE.
// A list summing to exactly the limit covers 1/128 of the IPv4 address SPACE. It
// becomes "roughly one client in 128 arriving from anywhere on earth" ONLY UNDER THE
// EXPLICIT CONDITION that client addresses are spread evenly across that space, and
// they are not: addresses are handed to networks, and clients sit behind the networks.
//
// The worst case that condition hides is measured and it has a name. An ISP allocation
// is a /8, and this function ACCEPTS one — measured 2026-08-20 with the shipped
// predicate: {10.0.0.0/8} and {81.240.0.0/8} both return tooWide=false; {10.0.0.0/7},
// exactly the limit, is accepted; {10.0.0.0/6} is refused. A venue configured with a
// national ISP's /8 therefore matches EVERY SUBSCRIBER OF THAT ISP — a country's worth
// of clients, not one in 128 of them. That is an ACCEPTED cost rather than a hidden
// surprise: "at most one ISP allocation per family, doubled" is exactly what the limit
// was derived to permit two paragraphs up, and the alternative is refusing the save of
// a venue whose network genuinely is that wide.
//
// So it is a bound this package can state and not a hole it can close: how narrow a
// real venue's network is is a fact about a BUILDING, and refusing a legitimate range
// would be a save failure invented out of a guess. What the limit does close is the
// whole class where the answer is "essentially everybody, WHATEVER their network" —
// the three spellings above match 5 of 5 unrelated public sources by construction and
// that statement needs no distribution at all, whereas the "5 of 640 in expectation"
// a list at the limit would give is the uniform figure again and carries the same
// condition as the 1/128.
const (
	maxProofHostBitsV4 = 25
	maxProofHostBitsV6 = 97
)

// maxProofAddresses is the limit for an address family, as a count.
func maxProofAddresses(bitLen int) *big.Int {
	shift := maxProofHostBitsV6
	if bitLen == 32 {
		shift = maxProofHostBitsV4
	}
	return new(big.Int).Lsh(big.NewInt(1), uint(shift))
}

// TooWideForProofOfPlace reports whether a list of address ranges covers more of an
// address family than a venue's network plausibly can, which makes it evidence of
// nothing. The returned prefix is the single entry that is already too wide on its
// own, or the zero Prefix when it takes the whole list to get there.
//
// 🔴 THE NAME IS THE THIRD ONE AND IT IS THE FIRST THAT DESCRIBES THE BEHAVIOUR
// (M2-08: a test's or a function's name is a LOCK). It was CoversEveryAddress, then
// CoversEveryClientAddress; both named a COVERAGE property, and both were true of
// strictly fewer lists than the function needed to refuse — so each name was a
// promise the next exploit could keep while walking through. This one names the
// property the limit above actually tests, and it is true of every list the function
// returns true for.
//
// 🔴 WHY THIS IS A REFUSAL AND NOT A WARNING (backlog T40, M8-04). A manager could
// set a venue's range to 0.0.0.0/0 from the panel, and the next tap ANYWHERE in the
// world was then recorded as:
//
//	verdict=ok  trust=70  ip_match=t
//	note="network proof of place: the source IP matches the location"
//
// There is no client address that fails to match, so that sentence declares a fact
// with nothing behind it — and transactions are IMMUTABLE (§4.3), so the row cannot
// afterwards be corrected. It is also the one configuration that turns the failure
// SILENT: with no GPS permission `tap:gpsConflict` never fires, so ADR 0005's
// detection signal for risk 4 never speaks.
//
// 🔴 AND IT NARROWS ADR 0005. That ADR accepts "a proxy left at the venue" because
// an IP is PORTABLE and the countermeasure would cost hardware. A "/0" is a
// different thing wearing the same coat: no proxy, no 20-euro phone, no physical
// trace — one panel field. What the ADR accepted was a portable IP proof, not one
// manufactured by configuration, so closing this does not reopen that risk. A dated
// note in docs/adr/0005-kabul-edilen-riskler.md says so where the risk is recorded.
//
// 🔴 IT IS THE LIST'S TOTAL AND NOT THE ENTRY'S, AND THAT GOES FURTHER THAN THIS
// REPO'S OWN PRECEDENT ON PURPOSE. internal/config's trustedProxySanity refuses a
// single "/0" and writes down that it does NOT detect "0.0.0.0/1,128.0.0.0/1",
// because "detecting coverage across entries would mean this package deciding how
// wide an ingress may be". That reasoning is exactly what this package DOES decide,
// for one narrowly-stated purpose, and the three measured spellings on the limit
// above are why: every one of them is spelled across entries, none of them contains
// a "/0", and the panel's cap on how many lines a venue may carry —
// handler.maxStaticRanges, 32, in internal/handler/locations.go — leaves room to
// spell the complement of any /24 on earth in 24 lines.
//
// ⚠️ THAT 32 IS A BOUNDARY SENTENCE, NOT AN INVARIANT, WHICH IS ONE MORE REASON THE
// RULE COUNTS ADDRESSES AND NOT ENTRIES. Measured 2026-08-20: the constant is
// unexported in package handler and enforced in exactly ONE place, handler.parseRanges
// (internal/handler/locations.go), i.e. at the HTTP boundary. tenant.SaveVenue calls
// THIS function but places no bound on len(c.StaticIPs); locations.static_ips is
// `cidr[] NOT NULL DEFAULT '{}'` (migration 00002) with no cardinality CHECK; and
// internal/domain/signup writes the column through store.CreateLocation without
// passing SaveVenue at all (today with an empty slice). So "at most 32 lines" is what
// a manager can TYPE, not what can be STORED — and a width rule phrased as a line
// count would inherit the weakest of those three paths. Address count has no such
// dependency: 2^n is the same number however the list got there.
//
// Those three paths are the WRITE side of what an unbounded list would cost, and they
// are not the whole bill: the READ side is counted at containedInAnother below,
// because this predicate is O(n²) and re-runs on every tap.
//
// 🔴 ONE PREDICATE SERVES BOTH ENDS, AND THAT IS THE WHOLE REASON THIS FUNCTION
// LIVES IN A PACKAGE OF ITS OWN. tenant.SaveVenue refuses such a list at save time;
// tap.ipMatches applies the SAME function to a list that was stored BEFORE the
// refusal existed, and treats "would be refused today" as "is not evidence". A write
// guard alone cannot help there, because transactions are immutable (§4.3) — a row
// already stored keeps producing `ip_match=t, trust=70` on every future tap forever.
// Two predicates would drift; there is one.
//
// ⚠️ "BOTH ENDS" IS TWO, BUT THE REPO CONTAINS A THIRD PREFIX-CONTAINMENT POINT, AND
// THE LIMIT IS COUNTED RATHER THAN LEFT UNSAID. policy.OpIpInPrefix
// (internal/policy/conditions.go) calls netip.Prefix.Contains with no width rule at
// all, so a tenant could write "IpInPrefix: [0.0.0.0/0]" into a policy document.
// Measured 2026-08-20 by driving exactly that document through tap.Decide, it is
// inert for two independent reasons:
//
//   - The closed context-key vocabulary (internal/policy/document.go — 24 keys) holds
//     NO address-valued key. The tap context's only prefix-shaped candidates are
//     location:id / tag:locationId / employee:locationId, and those are UUIDs. The
//     statement therefore never fires: toAddr rejects the value and the evaluator
//     reports kind=bad-context-value (sid=tenant:…, op=IpInPrefix, key=location:id)
//     rather than matching. A client address is not in the context to be tested.
//   - ip_match and trust are not policy outputs. Decision.IPMatch is assigned from
//     ipMatches (decide.go) and Trust from trustScore(ipMatch, gpsMatch), both BEFORE
//     the effect is applied, so no document can set either. The observed decision for
//     the hostile document above was ip_match=false trust=20.
//
// So the third point can move a VERDICT — which §5 rows 6-7 already put in the
// tenant's hands — but it cannot manufacture the structured evidence that this
// function exists to protect. That is why the width rule lives here and not in the
// condition evaluator.
//
// ⚠️ THE RESIDUE ROWS ARE A DATED OBSERVATION, NOT A BOUND, AND SAYING SO IS THE
// POINT. Earlier drafts of this paragraph carried counts that went stale inside the
// session that wrote them (0, then 4, then 15, then 30, then 36, then 48) because the
// test suite writes a residue venue on every run. Measured 2026-08-20: 57 venues carry
// a list this function refuses, all of them residue from this card's own mutation and
// audit rounds; they are left in place rather than deleted, because a row count on a
// shared database does not go down to make a comment tidy. 11 transactions were
// recorded at such venues and 4 carry ip_match = TRUE. Those four are exactly the
// immutable rows this repair exists for: they say "network proof of place", they
// were written while the hole was open, and §4.3 means they can never be corrected.
// The read-side half of the guard is what stops the count growing.
//
// 🔴 IT SEES THE SAME FORM ITS CONSUMERS SEE, which is the lesson internal/config
// paid three findings for. tap.ipMatches calls netip.Prefix.Contains on the STORED
// prefix, after Unmap()ing only the SOURCE. Measured, both in Go and in Postgres
// (`<<=`), a 4-in-6 prefix such as ::ffff:0.0.0.0/96 therefore matches no ordinary
// v4 client at all — it is INERT, not wide, so it cannot manufacture proof and is
// deliberately not refused here (2^32 addresses, far under the v6 limit). It can
// DESTROY proof, which is the empty-list case the panel already warns about. That is
// the opposite of internal/config's situation, where the resolver unmapped the
// PREFIX and the two disagreed.
func TooWideForProofOfPlace(ps []netip.Prefix) (netip.Prefix, bool) {
	// A SINGLE ENTRY THAT IS ALREADY OVER THE LIMIT IS NAMED, in the order the
	// manager typed it. The sum below would catch it too, but a manager can only fix
	// a LINE they can see, and "0.0.0.0/0" is a line.
	for _, p := range ps {
		if p.IsValid() && blockSize(p).Cmp(maxProofAddresses(p.Addr().BitLen())) > 0 {
			return p, true
		}
	}
	// Per family: drop every prefix CONTAINED in another, because two prefixes are
	// either nested or disjoint -- so what remains is pairwise disjoint and the sizes
	// add EXACTLY, with no interval arithmetic and no sort.
	for _, bitLen := range []int{32, 128} {
		var kept []netip.Prefix
		for _, p := range ps {
			if p.IsValid() && p.Addr().BitLen() == bitLen {
				kept = append(kept, p.Masked())
			}
		}
		if len(kept) == 0 {
			continue
		}
		total := new(big.Int)
		for i, p := range kept {
			if containedInAnother(kept, i) {
				continue
			}
			total.Add(total, blockSize(p))
		}
		if total.Cmp(maxProofAddresses(bitLen)) > 0 {
			return netip.Prefix{}, true
		}
	}
	return netip.Prefix{}, false
}

// blockSize is how many addresses a prefix holds.
func blockSize(p netip.Prefix) *big.Int {
	return new(big.Int).Lsh(big.NewInt(1), uint(p.Addr().BitLen()-p.Bits()))
}

// containedInAnother reports whether ps[i] is covered by some other entry. An exact
// DUPLICATE counts as contained for the later copy only, so a repeated range is
// summed once rather than twice -- the panel de-duplicates, a direct caller of the
// domain need not.
//
// ⚠️ IT IS O(n²) AND IT RUNS ON EVERY TAP — THE READ-SIDE COST THE 32-LINE BOUNDARY
// PARAGRAPH ABOVE ENUMERATES THREE WRITERS FOR BUT NEVER PRICED. tap.ipMatches calls
// TooWideForProofOfPlace on the STORED list before it will trust an IP, so a list's
// length is paid once per tap, not once per save. Measured 2026-08-20 through the
// exported caller (Go 1.26, darwin/amd64, Intel i9-9980HK, pairwise-disjoint /32
// entries so no pair short-circuits, `go test -bench`):
//
//	n =     32  ->    0.009 ms   <- handler.maxStaticRanges, what a manager can type
//	n =  1 000  ->    1.5   ms
//	n =  5 000  ->   32.3   ms
//	n = 20 000  ->  520.0   ms
//
// The growth is quadratic where the constants stop dominating: 5x the entries cost
// 22x the time, 4x more cost 16x more.
//
// It is UNREACHABLE today and that is why this is a counted cost and not an open bug —
// the only two writers of static_ips are tenant.SaveVenue, whose single caller is
// capped at 32 by handler.parseRanges, and internal/domain/signup, which writes an
// empty slice. The conclusion is the part worth writing down: if a THIRD writer is
// ever added without a length bound, what it buys is not a wrong verdict — the width
// rule still answers correctly at any n — it is TAP LATENCY, half a second of it at
// twenty thousand entries, on the one screen CLAUDE.md §9 calls sacred.
func containedInAnother(ps []netip.Prefix, i int) bool {
	p := ps[i]
	for j, q := range ps {
		if i == j {
			continue
		}
		if q.Bits() < p.Bits() && q.Contains(p.Addr()) {
			return true
		}
		if q == p && j < i {
			return true
		}
	}
	return false
}
