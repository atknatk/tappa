package handler

import (
	"sync"
	"time"
)

// A NARROW, ACTIVATION-ONLY rate limiter.
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
// activating, because clientIP deliberately does not read X-Forwarded-For yet
// (M5-03) and behind a reverse proxy every request shares one key. So the
// anonymous flood and the in-flight valid activation no longer share a bucket:
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
// everyone from the same source address, and until M5-03 resolves the real
// client IP through cfg.TrustedProxies, "the same source address" behind a proxy
// means EVERYONE. That residual is named and handed over in the M5-02 card and
// again at clientIP.
//
// EVERY REFUSAL IS CHARGED — including the ones this file itself produces. An
// audit measured the cost of the earlier design, where a 429 wrote an audit_log
// row and charged nothing: 400 POSTs against one dead invitation produced 390
// refusals AND 400 rows in a table that cannot be deleted from, even by
// tappa_owner. A limiter whose own refusals are free is not a limiter; it is a
// cheaper way to reach the thing it was protecting.
//
// LIMITS, measured against the real deployment rather than assumed:
//
//   - IN-PROCESS. Tappa is one binary on one VPS today, so a map is the whole
//     mechanism. The moment there are two instances this becomes per-instance
//     and the effective limit doubles; a shared store belongs with that change
//     (M8), not before it.
//   - PER SOURCE IP, and a whole venue shares one — see the M5-03 hand-off above.
//   - NOT A COUNTER ACROSS RESTARTS. Restarting clears the windows. The durable
//     trail is audit_log, which is why a tripped invite budget writes there once.
//   - A FIXED WINDOW, NOT A SLIDING ONE — so the true worst case is 2x the limit
//     in a short burst: `limit` at the very end of one window and `limit` more
//     immediately after it rolls over. Acceptable here because none of the three
//     is a cryptographic defence (the 256-bit code is), and the cost of a burst
//     is bounded by what a failure can do, which is nothing. A sliding window or
//     token bucket is the fix if that stops being true; it belongs with the
//     shared store in M8, since both are about a limiter meaning the same thing
//     across time and across instances.
type limiter struct {
	mu      sync.Mutex
	windows map[string]*window
	limit   int
	period  time.Duration
	// now is injectable so tests can advance time without sleeping.
	now func() time.Time
	// maxKeys bounds memory against an attacker rotating source addresses. When
	// the map exceeds it, expired entries are dropped; if that is not enough the
	// map is reset wholesale. Resetting FORGETS attempts, which is the
	// fail-OPEN direction — deliberately, because the alternative (refusing new
	// keys) would let one attacker lock every venue out of activating.
	maxKeys int
}

type window struct {
	count int
	start time.Time
}

// Activation limiter settings. Both are per FAILED attempt (see the type doc).
//
//	ipFailureLimit / ipFailurePeriod
//	  60 failures in 10 minutes from one address. A venue onboarding a crew
//	  produces zero failures on the happy path, so this is reachable only by
//	  someone feeding bad links. At 6 per minute a 256-bit code needs about
//	  10^70 years, which is the point: the limit exists to keep the database
//	  quiet, not to make guessing hard — the code length does that.
//
//	inviteFailureLimit / inviteFailurePeriod
//	  10 failures in 10 minutes against ONE invitation. A person re-opening a
//	  dead link a few times, or double-tapping the button, stays well inside it;
//	  a loop replaying a consumed code does not.
const (
	// floodLimit / floodPeriod: 600 requests in 10 minutes from one address, i.e.
	// one per second sustained. A venue onboarding fifteen people costs about 45
	// requests; a curious employee reloading costs a handful. Reaching 600 takes
	// deliberate automation, which is the only case where refusing everything
	// from that address is the right answer.
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

	limiterMaxKeys = 20000
)

func newLimiter(limit int, period time.Duration) *limiter {
	return &limiter{
		windows: make(map[string]*window),
		limit:   limit,
		period:  period,
		maxKeys: limiterMaxKeys,
	}
}

func (l *limiter) clock() time.Time {
	if l.now != nil {
		return l.now()
	}
	return time.Now()
}

// allowed reports whether key is still under its budget. It does NOT count the
// request: counting happens in fail, so success costs nothing.
func (l *limiter) allowed(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.windows[key]
	if !ok {
		return true
	}
	if l.clock().Sub(w.start) >= l.period {
		delete(l.windows, key)
		return true
	}
	return w.count < l.limit
}

// charge records one chargeable event against key and returns the count AFTER
// charging.
//
// The count is returned so a caller can act on the TRANSITION rather than on
// every request past the limit: `charge(k) == l.limit+1` is true exactly once per
// window, which is how the audit row for a tripped invitation and the "further
// lines suppressed" log line are written once instead of forever. That is the
// mechanism that bounds writes into an append-only table.
func (l *limiter) charge(key string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock()
	w, ok := l.windows[key]
	if !ok || now.Sub(w.start) >= l.period {
		if len(l.windows) >= l.maxKeys {
			l.evictLocked(now)
		}
		l.windows[key] = &window{count: 1, start: now}
		return 1
	}
	w.count++
	return w.count
}

// firstOverLimit reports whether this charge is the one that crossed the line.
func (l *limiter) firstOverLimit(count int) bool { return count == l.limit+1 }

// evictLocked drops expired windows, and if that frees nothing, everything. The
// caller holds the lock.
func (l *limiter) evictLocked(now time.Time) {
	for k, w := range l.windows {
		if now.Sub(w.start) >= l.period {
			delete(l.windows, k)
		}
	}
	if len(l.windows) >= l.maxKeys {
		l.windows = make(map[string]*window)
	}
}
