package httpx

import (
	"context"
	"net/http"
	"net/netip"
	"strings"
)

// The client address is EVIDENCE, not metadata. An IP that matches the venue's
// static address is worth 50 of the 100 trust points a tap can earn (CLAUDE.md
// §5), and it is the key every abuse budget is metered on. So this file has one
// job: decide which address to believe, and refuse to believe anything a caller
// could have chosen for itself.
//
// WHY NOT chi's middleware.RealIP (the M5-03 card names it as the trap). It
// rewrites r.RemoteAddr from X-Forwarded-For, X-Real-IP or True-Client-IP
// whether or not our infrastructure sets them, and it takes the LEFTMOST entry
// of the chain. Both halves are wrong here: the headers are attacker-writable
// unless a trusted hop put them there, and the leftmost entry is the one the
// client itself supplied. A forgeable proof of place is worse than none.
//
// WHAT IS HONOURED. Only X-Forwarded-For, and only when the TCP peer is inside
// cfg.TrustedProxies. Every other forwarding header — X-Real-IP, True-Client-IP,
// RFC 7239's Forwarded, and the CDN-specific ones — is never read, and the
// removal list below explains why that is a rule rather than a guarantee. They
// carry a single value with no chain, so a proxy that set them cannot be
// distinguished from a client that did.
//
// THE WALK IS RIGHT TO LEFT, which is the whole security property. A proxy
// APPENDS the address it observed, so the rightmost entry is the one our own
// trusted hop wrote and the leftmost is whatever the client typed. Skipping
// trusted hops from the right and stopping at the first untrusted address yields
// the address the innermost trusted proxy actually saw. Reading from the left —
// the common mistake — would hand the caller a free choice of both its rate
// limit bucket and its location.
//
// ASSUMPTION THIS MAKES ABOUT DEPLOYMENT, stated because it is not enforceable
// from here: the reverse proxy must APPEND to X-Forwarded-For rather than
// replace it (nginx: proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
// Caddy and Traefik do this by default). A proxy that REPLACES the header with
// the client's claim would hand us that claim with our own hop's blessing, and
// nothing on this side can tell the difference. That is a deploy checklist item
// (M8), not a code defence.
//
// SECOND ASSUMPTION, and the sharper one: TRUSTING EVERYTHING IS THE SAME AS
// TRUSTING NOTHING, INVERTED. If cfg.TrustedProxies contains a default route
// (0.0.0.0/0 or ::/0) then every entry in the chain is "trusted", the walk never
// finds an untrusted address, and it returns the LEFTMOST entry — which is
// exactly the value the client wrote. Measured, not reasoned about: with
// trusted=0.0.0.0/0 and "X-Forwarded-For: 1.2.3.4, 5.6.7.8" the answer is
// 1.2.3.4. That is not a bug in the walk (with everything trusted there is no
// honest answer to give); it is a configuration that hands the caller its own
// address. config.Load now REFUSES a default route in production and warns
// loudly otherwise — see trustedProxySanity there. This file states the
// consequence; that file is the gate.

// forwardedForHeader is the only forwarding header this package honours.
const forwardedForHeader = "X-Forwarded-For"

// strippedHeaders are removed from every request after resolution: the headers
// that are commonly understood to carry a client address.
//
// ⚠️ THIS IS A DENYLIST, AND A DENYLIST IS NOT A GUARANTEE. Two audits made the
// point, both by measurement. The first: this comment said removal was
// "structural" and that a handler "cannot reach for a raw header even by
// accident", while the list held three names and NINE others reached the handler
// untouched — including `Forwarded`, the RFC 7239 STANDARD one, whose value is
// entirely client-controlled here because nothing in this deployment writes it.
// The second, over a live TCP socket: 23 of 36 candidates still arriving, among
// them the Client-IP / Proxy-Client-IP / WL-Proxy-Client-IP family that every
// classic list has carried for twenty years. The list below is now 32 names and
// the corpus measures 4 survivors, all deliberate — but a vendor header nobody
// has heard of yet will still survive it, and that is the nature of the thing.
//
// So the claim is reduced to what the code does, and the rule is stated as a
// RULE rather than as an enforced property:
//
//	WHAT IS TRUE: the known forwarding headers below do not reach a handler.
//	WHAT IS NOT ENFORCED: that no other header does.
//	THE RULE: the client address comes from ClientIP. Reading ANY header for it
//	is a new bug, not a fallback — an IP match is worth 50 of the 100 trust
//	points a tap can earn (§5) and is the key every abuse budget is metered on,
//	so a header-sourced address is forgery with extra steps.
//
// Today nothing in this repo reads one for an address. MEASURED, by grepping
// every request-header read in production Go: `Sec-Fetch-Site` and `Origin`
// (internal/handler, the CSRF checks) and `r.UserAgent()` (bounded into a coarse
// device label, internal/handler/device.go) — plus this file's own read of
// X-Forwarded-For, which is the point. So the list is a net under a rule, not
// the rule itself.
//
// DELIBERATELY NOT HERE, so that the criterion above stays the criterion:
// X-Forwarded-Host and X-Forwarded-Proto carry a host and a scheme, and Via
// (RFC 9110 §7.6.3) names intermediaries, not the client. None of them is an
// address, this file has no authority over how a URL gets built, and removing
// them would make the list "delete anything that smells of a proxy" — which is
// unfalsifiable and would quietly delete the positive control that keeps this
// list honest. If anything ever builds a URL from a request header instead of
// cfg.BaseURL (which originOf already uses), that is a host-header injection
// question belonging to that code and to the deploy review (M8).
var strippedHeaders = [...]string{
	// The chain forms.
	forwardedForHeader,
	"Forwarded",                // RFC 7239, the standard one — nothing here writes it
	"Forwarded-For",            // non-standard shorthand seen in the wild
	"X-Forwarded",              // ditto
	"X-Http-Forwarded-For",     // ditto
	"X-Original-Forwarded-For", // some proxies keep the pre-rewrite chain here
	"X-Original-For",           // ditto
	"X-Forwarded-Client-Ip",    // ditto

	// The single-value forms. The classic scanner list — Client-IP,
	// Proxy-Client-IP, WL-Proxy-Client-IP — is here because an audit pointed out
	// that a list claiming to hold what is "commonly understood to carry a client
	// address" cannot omit the three names every such list has carried since
	// WebLogic.
	"Client-Ip",
	"Proxy-Client-Ip",
	"Wl-Proxy-Client-Ip",
	"X-Client-Ip",    // Heroku and others
	"X-Real-Ip",      // chi's RealIP believes this
	"X-Real-Ip-Orig", // seen behind double-proxy setups
	"X-Remote-Ip",
	"X-Remote-Addr",
	"Remote-Addr", // not an HTTP header at all, which is exactly why it gets forged
	"X-Coming-From",
	"X-Proxyuser-Ip",

	// Vendor and CDN forms. Real in the deployment that sets them, forgeable in
	// every other — including ours, which sets none of them.
	"True-Client-Ip",   // Akamai, Cloudflare; chi's RealIP believes this
	"X-True-Client-Ip", // the same idea, one vendor over
	"Cf-Connecting-Ip", // Cloudflare
	"Cf-Connecting-Ipv6",
	"Cf-Pseudo-Ipv4",
	"X-Akamai-Client-Ip",
	"Fastly-Client-Ip", // Fastly
	"Ali-Cdn-Real-Ip",  // Alibaba Cloud CDN
	"Cdn-Src-Ip",
	"X-Envoy-External-Address", // Envoy
	"X-Cluster-Client-Ip",      // Rackspace and some load balancers
	"X-Azure-Clientip",         // Azure Front Door
	"X-Appengine-User-Ip",      // Google App Engine
}

// maxForwardedHops bounds the walk. Reaching it needs a chain of TRUSTED
// addresses that long, because the walk returns at the first untrusted one —
// which means either fifty real proxies (nobody's deployment) or a caller
// spoofing addresses inside our own proxy range.
//
// Hitting the cap returns the LAST HOP THE WALK REACHED: the OUTERMOST trusted
// address it got to, further from us than where it started. (An earlier comment
// said "innermost". The direction of the failure was right, the word was wrong —
// the walk moves outward, away from our own proxy.) That is the fail-closed
// direction, but be precise about what it buys: those requests are keyed to a
// TRUSTED PROXY's address. Employee taps never resolve there, because a proxy
// that appends always leaves at least one entry to its right — but traffic from
// a proxy itself with no chain at all (a health check) does, so the bucket is
// not provably empty of legitimate traffic. It is provably empty of TAPS, which
// is what the budget is protecting.
const maxForwardedHops = 50

type clientIPKey struct{}

// RealIP resolves the client address once per request and puts it in the
// context. It does NOT rewrite r.RemoteAddr: the TCP peer stays available and
// unambiguous, and code that wants the client address asks for it by name.
//
// trusted is normally cfg.TrustedProxies. EMPTY MEANS NO PROXY: the peer is used
// verbatim and no header is read at all. That is the shipped default and it is
// the safe one — a deployment that terminates TLS in front of Tappa must say so
// (TAPPA_TRUSTED_PROXIES), and until it does, every caller shares the proxy's
// address rather than getting to pick one.
func RealIP(trusted []netip.Prefix) func(http.Handler) http.Handler {
	t := acceptPrefixes(trusted)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			addr := resolveClientIP(r.RemoteAddr, r.Header, t)
			for _, h := range strippedHeaders {
				r.Header.Del(h)
			}
			if addr.IsValid() {
				r = r.WithContext(context.WithValue(r.Context(), clientIPKey{}, addr))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP returns the address RealIP resolved for r.
//
// FALLBACK WHEN THE MIDDLEWARE DID NOT RUN: the TCP peer, never a header. That
// is a degradation (behind a proxy every caller resolves to the proxy) but never
// a forgery, and it keeps a handler constructed outside NewRouter — a test, a
// future sub-router — from silently getting an empty key that merges every
// caller into one bucket. NewRouter mounts RealIP for the whole surface, so the
// fallback is not the production path.
//
// The zero Addr is returned when the peer itself is unparseable. Callers that
// need a map key use RateKey, which names that case rather than collapsing it
// into the empty string.
func ClientIP(r *http.Request) netip.Addr {
	if a, ok := r.Context().Value(clientIPKey{}).(netip.Addr); ok {
		return a
	}
	a, _ := parseHop(r.RemoteAddr)
	return a
}

// RateKey renders an address as a rate-limit bucket key.
//
// IPv6 IS BUCKETED BY /64, NOT BY ADDRESS. A single IPv6 host routinely holds
// billions of addresses from its /64 (SLAAC, privacy extensions), so a per-address
// budget is not a budget at all — it is an invitation to rotate. RFC 4291 makes
// the /64 the smallest unit an operator assigns to a link, so it is the smallest
// unit that means "one caller". IPv4 is keyed by the exact address: a /24 there
// can be dozens of unrelated customers.
//
// An invalid address becomes "unknown" rather than "": one named bucket for
// requests whose peer could not be parsed, instead of a bucket whose name is
// also the zero value of every other string in the process.
func RateKey(a netip.Addr) string {
	if !a.IsValid() {
		return "unknown"
	}
	a = a.Unmap().WithZone("")
	if a.Is4() {
		return a.String()
	}
	p, err := a.Prefix(64)
	if err != nil {
		return a.String()
	}
	return p.String()
}

// resolveClientIP is the whole decision, kept pure so it can be proved by table
// test rather than by starting a server. It returns the zero Addr when the peer
// itself cannot be parsed.
func resolveClientIP(remoteAddr string, h http.Header, trusted []netip.Prefix) netip.Addr {
	peer, ok := parseHop(remoteAddr)
	// Three ways to answer with the peer and never look at a header: it is
	// unparseable, no proxy is trusted, or the peer is not one of the trusted
	// ones. The last is the important one — a request that reached us directly
	// carries whatever X-Forwarded-For its sender felt like writing.
	if !ok || len(trusted) == 0 || !isTrusted(peer, trusted) {
		return peer
	}

	client := peer
	values := h.Values(forwardedForHeader)
	hops := 0
	// Right to left across ALL header instances. Go keeps repeated headers as
	// separate values in arrival order, and RFC 9110 §5.3 makes that equivalent
	// to one comma-joined value — so a caller cannot escape the walk by sending
	// its forgery as a second X-Forwarded-For line. Fields are cut from the right
	// instead of splitting the whole chain, so a 100k-entry header costs what the
	// walk actually reads, not what it was sent.
	for i := len(values) - 1; i >= 0; i-- {
		rest := values[i]
		for len(rest) > 0 {
			var field string
			if j := strings.LastIndexByte(rest, ','); j >= 0 {
				field, rest = rest[j+1:], rest[:j]
			} else {
				field, rest = rest, ""
			}
			if hops++; hops > maxForwardedHops {
				return client
			}
			a, ok := parseHop(field)
			if !ok {
				// A malformed entry ends the walk: everything further left is
				// behind something we cannot account for, and guessing past it
				// is how a forged entry gets promoted. The last trusted hop is
				// the honest answer.
				return client
			}
			if !isTrusted(a, trusted) {
				return a // the first untrusted address from the right IS the client
			}
			client = a
		}
	}
	return client
}

// parseHop parses one X-Forwarded-For entry or a RemoteAddr.
//
// It accepts the shapes that actually turn up: a bare address, host:port (Go's
// RemoteAddr, and proxies that copy it verbatim), and a bracketed IPv6 literal
// with or without a port. Anything else — an RFC 7239 obfuscated identifier
// ("_hidden", "unknown"), a hostname, empty space — is rejected rather than
// guessed at.
//
// The result is canonicalised: an IPv4-mapped IPv6 address (::ffff:1.2.3.4)
// becomes plain 1.2.3.4, and any zone is dropped. Both matter. netip.Prefix
// treats a 4-in-6 address as a different family, so an unmapped ::ffff:10.0.0.1
// would silently miss a 10.0.0.0/8 trusted range; and a zone would let one host
// mint unlimited distinct rate-limit keys.
func parseHop(s string) (netip.Addr, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return netip.Addr{}, false
	}
	if a, err := netip.ParseAddr(s); err == nil {
		return canonical(a), true
	}
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return canonical(ap.Addr()), true
	}
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		if a, err := netip.ParseAddr(s[1 : len(s)-1]); err == nil {
			return canonical(a), true
		}
	}
	return netip.Addr{}, false
}

func canonical(a netip.Addr) netip.Addr { return a.Unmap().WithZone("") }

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	if !a.IsValid() {
		return false
	}
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// acceptPrefixes filters the configured ranges once, at construction. It keeps
// only prefixes this package is willing to make a trust decision with.
//
// 🔴 A V4-MAPPED PREFIX (::ffff:10.0.0.0/104) IS DROPPED, NOT UNMAPPED, AND THAT
// REVERSAL IS THE FIX FOR A MEASURED VULNERABILITY. This function used to unmap
// it as a convenience, so an operator writing the form they see on a dual-stack
// box would still be trusted. The convenience created a SECOND SPELLING of every
// range, and config's default-route gate was reading the first while this walk
// used the second: ::ffff:0.0.0.0/96 passed the gate as Bits()==96 and behaved
// here as 0.0.0.0/0. A security audit measured the result end to end — with
// TAPPA_ENV=prod and an ordinary internet caller as the peer, that one spelling
// let the CALLER CHOOSE ITS OWN ADDRESS, which is the precise forgery this file
// exists to prevent (proof of place is 50 of 100 trust points, §5).
//
// The repair is to leave exactly one spelling in existence. config REFUSES the
// mapped form at startup with a message naming the IPv4 form to use — that is
// the half an operator can act on. This is the other half: whatever a caller
// constructs RealIP with, the ambiguous form never reaches the walk.
//
// DROPPING IS THE FAIL-CLOSED DIRECTION, and the reason is monotone rather than
// hopeful: removing an entry can only make isTrusted answer false in more
// places, and a false answer either skips the header entirely (untrusted peer)
// or stops the walk EARLIER — at a hop further to the right, closer to the
// address we observed ourselves. A shorter list therefore moves the answer
// towards the TCP peer and never towards the value the caller wrote.
//
// An invalid prefix is dropped for the same reason.
func acceptPrefixes(in []netip.Prefix) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(in))
	for _, p := range in {
		if !p.IsValid() || p.Addr().Is4In6() {
			continue
		}
		// Zone-stripped and masked so Contains compares what parseHop produces.
		np, err := p.Addr().WithZone("").Prefix(p.Bits())
		if err != nil {
			continue
		}
		out = append(out, np)
	}
	return out
}
