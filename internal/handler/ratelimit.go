package handler

import (
	"time"

	"github.com/atknatk/tappa/internal/httpx"
)

// A NARROW, ACTIVATION-ONLY rate limiter.
//
// THE MECHANISM MOVED IN M5-03, THE BUDGETS DID NOT. The fixed-window counter
// itself is httpx.Limiter now, because CLAUDE.md §3 puts rate limiting in
// internal/httpx and because the tap endpoints need the same primitive; a second
// copy of it here would drift. What stays in this package is what is
// activation-specific: three budgets, what may charge each, and what each may
// refuse. Those are unchanged.
//
// WHY IT IS NOT M5-03's LIMITER. The M5-02 card is explicit: the tap rate limit
// M5-03 will add is deliberately WIDE — a legitimate shift must never touch it —
// and "/api/activate onun kapsamına GİRMEZ, kendi dar sınırı olur". The two guard
// different things. A tap is a routine act by a known person that happens a few
// times a day; an activation is a once-per-employee act that spends a credential.
//
// THREE BUDGETS, AND THE SPLIT IS THE WHOLE DESIGN. An earlier version had ONE
// per-IP window checked first in every handler, and an audit measured what that
// bought: 60 requests carrying UNKNOWN codes — no identity, no session, no valid
// code required — and the next request carrying a GENUINE invitation got a 429.
// One caller at 0.1 requests/second could keep a whole deployment from
// activating, because clientIP could not yet tell two callers apart behind a
// reverse proxy and every request shared one key. So the anonymous flood and the
// in-flight valid activation no longer share a bucket:
//
//	flood   per IP, charged on EVERY request, HIGH ceiling. The genuine DoS
//	        shield and the ONLY budget that can refuse a valid activation. It
//	        takes floodLimit requests in the window to get there — a real flood,
//	        not sixty bad links — and at that point shedding is the correct
//	        answer for anything from that address.
//	unknown per IP, charged ONLY by failures that carry NO tenant (an unknown
//	        code, a missing cookie, a foreign Origin, an unparseable form). Those
//	        cannot be written to audit_log at all — there is no tenant — so this
//	        budget bounds the PROCESS LOG instead: past it, the branch answers
//	        the same way and stops writing lines. It gates nothing else, which is
//	        exactly why a flood of unknown codes can no longer starve a valid one.
//	invite  per invitation (keyed by its UUID, never by the code or its hash —
//	        an in-memory map key ends up in heap dumps, and a code hash is a
//	        bearer credential, §4.7 / 00009). This is what bounds audit_log:
//	        every attributable refusal charges it, and once it trips the refusal
//	        is answered without writing another row.
//
// ONLY FAILURES ARE CHARGED TO THE LAST TWO. A successful lookup or activation
// consumes neither, so a legitimate employee's OWN traffic cannot approach them.
// ⚠️ THAT IS NOT THE SAME CLAIM AS "the flow is always served", and an earlier
// version of this comment ran the two together. What is true: the happy path
// contributes nothing to the unknown and invite budgets. What is NOT true: that
// a legitimate request can never be refused — the flood ceiling applies to
// everyone sharing one source address.
//
// ⚠️ WHAT "ONE SOURCE ADDRESS" MEANS CHANGED IN M5-03, and it is the reason the
// hand-off is closed. It used to mean "the reverse proxy", i.e. everyone on
// earth: an outsider could spend the flood ceiling and make activation
// unavailable for every venue until the window rolled. Now clientIP asks
// httpx.ClientIP, which resolves X-Forwarded-For only across hops inside
// cfg.TrustedProxies, so the ceiling is per caller. The RESIDUAL is honest and
// much smaller: a venue behind one NAT still shares an address, so a hostile
// device on the venue's own wifi can spend that venue's ceiling. Nothing keyed
// on network address can do better; what bounds it is that spending 600
// requests in ten minutes is loud and local.
//
// EVERY REFUSAL IS CHARGED — including the ones this file itself produces. An
// audit measured the cost of the earlier design, where a 429 wrote an audit_log
// row and charged nothing: 400 POSTs against one dead invitation produced 390
// refusals AND 400 rows in a table that cannot be deleted from, even by
// tappa_owner. A limiter whose own refusals are free is not a limiter; it is a
// cheaper way to reach the thing it was protecting.
//
// LIMITS: in-process, per source address, cleared by a restart, and a FIXED
// window (so a short burst can reach 2x the limit). All four are properties of
// the shared primitive and are stated where it lives, httpx.Limiter. The
// activation-specific consequence is worth repeating here: none of these three
// budgets is a cryptographic defence — the 256-bit code is — so a burst costs
// nothing that matters.
type limiter = httpx.Limiter

func newLimiter(limit int, period time.Duration) *limiter {
	return httpx.NewLimiter(limit, period)
}

// The three budgets. Each number is defended by an arithmetic worst case rather
// than chosen for feeling generous.
const (
	// floodLimit / floodPeriod: 600 requests in 10 minutes from one address, i.e.
	// one per second sustained. Reaching 600 takes deliberate automation, which is
	// the only case where refusing everything from that address is the right
	// answer.
	//
	// WHAT ONE ACTIVATION ACTUALLY COSTS, counted by a middleware in front of the
	// real router rather than by reading the flow (M5-07). There is NO single
	// number, because the mini tour can be walked or skipped and both are ordinary:
	//
	//	4 requests  the pre-tour shape (M5-02..M5-06): GET /activate?code= ->
	//	            303 -> GET /activate -> POST /api/activate -> GET /activate/done
	//	5 requests  first activation, tour SKIPPED (the redirect lands on
	//	            /activate/tour, the visitor follows the way out)
	//	7 requests  first activation, tour WALKED (two more slides)
	//
	// A second device is the 4-request shape: Submit sends it straight to the
	// confirmation. So a venue onboarding fifteen people costs 60, 75 or 105
	// requests depending on how many read the tour — against a ceiling of 600,
	// i.e. 150, 120 or 85 complete activations per window per address key.
	//
	// ⚠️ THIS PARAGRAPH USED TO SAY "about 45 requests" FOR FIFTEEN PEOPLE, and it
	// was already wrong before the tour existed: 45 implies three requests each,
	// which never counts the 303 hop from the code-bearing URL to the clean one.
	// The measured pre-tour figure is 60. A number that nobody re-derives is a
	// number that drifts, and this one had drifted by a third on its own.
	//
	// RE-EVALUATED IN M5-03, as the hand-off required, and DELIBERATELY LEFT AT
	// 600. The number was already chosen for per-address traffic; what changed is
	// that the key finally IS per address (httpx.ClientIP), so the ceiling now
	// does what its comment always said. Both directions were considered and
	// rejected:
	//   · LOWER (say 120) would fit a single phone, but the address is shared by
	//     a whole venue behind one NAT — a crew onboarding together on the shop
	//     wifi is a legitimate burst, and this is the one budget that can refuse
	//     a VALID activation. Refusing a real activation to save 480 counter
	//     increments is a bad trade.
	//   · HIGHER buys nothing: 600 already carries a fifteen-person crew reading
	//     every slide (105 requests) with more than five times the room to spare,
	//     and every failure mode past this ceiling is already bounded by the other
	//     two budgets. (This line also said "~45"; same drift, same fix.)
	// What genuinely improved is not the number but the KEY, and that was the
	// whole content of the hand-off.
	floodLimit  = 600
	floodPeriod = 10 * time.Minute

	// unknownLimit / unknownPeriod: 60 unattributable failures in 10 minutes from
	// one address. Past this the branch stops writing log lines; it does not
	// change what any caller is told and it does not gate a valid code.
	unknownLimit  = 60
	unknownPeriod = 10 * time.Minute

	// inviteFailureLimit / inviteFailurePeriod: 10 failures in 10 minutes against
	// ONE invitation. A person re-opening a dead link a few times, or
	// double-tapping the button, stays well inside it; a loop replaying a
	// consumed code does not.
	inviteFailureLimit  = 10
	inviteFailurePeriod = 10 * time.Minute
)
