package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/audit"
)

// Limiter is a fixed-window counter keyed by an opaque string. It is the shared
// primitive: internal/handler meters the ACTIVATION flow with it (three narrow
// budgets, see that package) and TapLimiter below meters the TAP endpoints. The
// two are deliberately separate meters over one mechanism — the M5-02 card is
// explicit that /api/activate must not fall under the wide tap limit.
//
// LIMITS, measured against the real deployment rather than assumed:
//
//   - IN-PROCESS. Tappa is one binary on one VPS today, so a map is the whole
//     mechanism. With two instances this becomes per-instance and the effective
//     limit doubles; a shared store belongs with that change (M8), not before.
//   - NOT A COUNTER ACROSS RESTARTS. Restarting clears every window.
//   - A FIXED WINDOW, NOT A SLIDING ONE, so the true worst case is 2x the limit
//     in a short burst: `limit` at the very end of one window and `limit` more
//     immediately after it rolls over. A sliding window or token bucket is the
//     fix if that stops being acceptable; it belongs with the shared store in
//     M8, since both are about a limiter meaning the same thing across time and
//     across instances.
//   - NOT A CRYPTOGRAPHIC DEFENCE. It bounds cost, not guessing. What makes an
//     activation code unguessable is its 256 bits (internal/invite), and what
//     makes a tap unforgeable is the SUN CMAC plus the atomic counter
//     (internal/sun). A limiter that had to be tight to be safe would be in the
//     wrong place.
type Limiter struct {
	mu      sync.Mutex
	windows map[string]*window
	limit   int
	period  time.Duration
	// now is injectable so tests can advance time without sleeping.
	now func() time.Time
	// maxKeys bounds memory against a caller rotating source addresses. When the
	// map exceeds it, expired entries are dropped; if that frees nothing the map
	// is reset wholesale. Resetting FORGETS attempts, which is the fail-OPEN
	// direction — deliberately, because the alternative (refusing new keys)
	// would let one attacker lock every venue out.
	//
	// ⚠️ THE PRESSURE ON THIS WENT UP IN M5-03, which is why the ceiling moved.
	// Before real client addresses were resolved, every request behind a reverse
	// proxy shared ONE key and this could not be reached at all. Now keys are per
	// caller (RateKey: an IPv4 address, an IPv6 /64), so a caller holding a
	// routed prefix can mint them. No in-process fixed window survives a truly
	// distributed source — that is what the shared store in M8 is for — but the
	// bar is now 100k distinct /64s per window instead of 20k.
	maxKeys int
}

type window struct {
	count int
	start time.Time
}

// limiterMaxKeys bounds the window map. MEASURED, not estimated: 100k distinct
// IPv4-shaped keys each pointing at a live window cost 7.9 MB of heap on this
// toolchain (go1.26, darwin/amd64, runtime.MemStats before/after a GC). That is
// affordable on the target VPS and is reclaimed as windows expire.
const limiterMaxKeys = 100_000

// NewLimiter builds a limiter allowing `limit` chargeable events per key per
// period.
func NewLimiter(limit int, period time.Duration) *Limiter {
	return &Limiter{
		windows: make(map[string]*window),
		limit:   limit,
		period:  period,
		maxKeys: limiterMaxKeys,
	}
}

// Limit and Period report the configuration, for log lines and Retry-After.
func (l *Limiter) Limit() int            { return l.limit }
func (l *Limiter) Period() time.Duration { return l.period }

func (l *Limiter) clock() time.Time {
	if l.now != nil {
		return l.now()
	}
	return time.Now()
}

// Allowed reports whether key is still under its budget. It does NOT count the
// request: counting happens in Charge, so a caller can decide that success is
// free.
func (l *Limiter) Allowed(key string) bool {
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

// Charge records one chargeable event against key and returns the count AFTER
// charging.
//
// The count is returned so a caller can act on the TRANSITION rather than on
// every request past the limit: Charge(k) == limit+1 is true exactly once per
// window, which is how an audit row or a log line is written once instead of
// forever. That is the mechanism that bounds writes into an append-only table —
// an audit measured what its absence costs (M5-02: 400 refused requests, 400
// permanent rows).
func (l *Limiter) Charge(key string) int {
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

// FirstOverLimit reports whether this charge is the one that crossed the line.
func (l *Limiter) FirstOverLimit(count int) bool { return count == l.limit+1 }

// evictLocked drops expired windows, and if that frees nothing, everything. The
// caller holds the lock.
func (l *Limiter) evictLocked(now time.Time) {
	for k, w := range l.windows {
		if now.Sub(w.start) >= l.period {
			delete(l.windows, k)
		}
	}
	if len(l.windows) >= l.maxKeys {
		l.windows = make(map[string]*window)
	}
}

// ---------------------------------------------------------------- tap limits --

// TapLimiter is the abuse shield for the tap endpoints (GET /t, POST
// /api/checkin — M5-04 and M5-05).
//
// IT IS A SHIELD, NOT A FILTER. Refusing a tap is refusing an attendance record,
// and §4.6 does not allow a record to go missing because a shift change was busy.
// Rate-limiting is not how repeated taps are handled either — that is the
// 60-second debounce, and it lives in the guardrails where it produces a RECORDED
// `ignored` decision rather than an HTTP error.
//
// ⚠️ THE CARD SAYS "a limit a legitimate tap can never reach", AND THAT SENTENCE
// NEEDS THE SAME SPLIT M5-02 LEARNED THE HARD WAY. It is true of a shift's OWN
// CONTRIBUTION: the arithmetic below leaves a 300-person shift change three times
// the room it needs, and one phone cannot approach the session budget. It is NOT
// true of whether that shift is SERVED, because tapAddressLimit is keyed on an
// address a whole venue shares:
//
//	🔴 RESIDUAL, NAMED FOR M5-04/M5-05 BEFORE THEY MOUNT THIS. A hostile device on
//	a venue's own wifi — or any device behind the same NAT — can spend that
//	venue's 3000 and the venue's LEGITIMATE TAPS THEN GET 429. And a 429 from
//	this middleware produces NEITHER a transaction NOR a `flag`: the request never
//	reaches the decision engine, so the shift-change taps in that window leave no
//	attendance record at all. That is a real tension with §4.6, which answers
//	"the evidence is bad" with a recorded `flag` rather than with silence —
//	shedding load is the one case where nothing is recorded, and it is a
//	deliberate trade (a server that writes a record per request under flood
//	protects nothing). It is bounded rather than resolved: the ceiling is high
//	enough that reaching it takes sustained deliberate traffic, the WARN line
//	fires, and an identified caller leaves an audit row. What would actually
//	resolve it is a shared store plus per-venue rather than per-address keying
//	(M8), and neither belongs here.
//
// The activation limiter carries the same residual for its own flood ceiling and
// says so (internal/handler/ratelimit.go); the difference is that an activation
// refused now can be retried in ten minutes, while a clock-in refused now is a
// missing hour on a payslip.
//
// TWO BUDGETS BECAUSE THERE ARE TWO ATTACKS, and they are two middlewares
// because they must sit on OPPOSITE SIDES of identity resolution:
//
//	ByAddress   keyed by the resolved client address (RateKey). Runs BEFORE
//	            Identify, because a pre-database shield that has already done a
//	            database query has shed nothing. Charged on every request.
//	BySession   keyed by the session id. Runs AFTER Identify, and only meters
//	            requests that carry a session. This is the budget that survives
//	            a caller changing address. Under the order below it is also the
//	            only one whose refusals can name a tenant — not because ByAddress
//	            is forbidden to (refuse() checks the identity either way) but
//	            because nothing has resolved one yet when it runs.
//
// THE ORDER M5-04 / M5-05 MUST MOUNT (the routes do not exist yet, so this is
// the contract rather than a description of live code):
//
//	tap := httpx.NewTapLimiter(httpx.TapLimitParams{Audit: rec, Log: log})
//	r.Group(func(r chi.Router) {
//	    r.Use(tap.ByAddress)             // shed before any database work
//	    r.Use(httpx.Identify(cookies, sessions))
//	    r.Use(tap.BySession)             // meter the identified caller
//	    r.Get("/t", tapPage)
//	    r.Post("/api/checkin", checkin)
//	})
//
// WHAT A REFUSAL LEAVES BEHIND — the card asks for an audit_log row and that is
// only sometimes possible; the difference is measured, not assumed.
// audit_log.tenant_id is NOT NULL with an FK to tenants (00005), so an event
// with no tenant CANNOT be stored, and no "system tenant" is invented to paper
// over it (db/queries/audit.sql; M5-02 hit the same wall). A tap request has no
// tenant until its session resolves. So:
//
//   - refusal with a resolved session  → one audit_log row, written ONCE per
//     window (FirstOverLimit), plus a WARN line;
//   - refusal without one              → WARN line only, also once per window.
//
// Both are bounded on purpose. audit_log cannot be deleted from even by
// tappa_owner, so a refusal path that wrote a row per request would be a cheaper
// way to attack the trail than attacking the endpoint (M5-02, blocking finding
// #1). "I refused it" is not the same as "it was free".
type TapLimiter struct {
	addr *Limiter
	sess *Limiter
	// audit may be nil: refusals then leave a log line and nothing else. It is
	// optional rather than required because the limiter must be constructible
	// before the database is (tests, and a future read-only surface), and because
	// a nil recorder cannot silently drop an ATTENDANCE record — this path never
	// writes one.
	audit AuditRecorder
	log   *slog.Logger
}

// AuditRecorder is the slice of audit.Recorder this package needs (§7).
type AuditRecorder interface {
	Record(ctx context.Context, e audit.Event) (uuid.UUID, error)
}

// ActionTapRateLimited is the audit action a refused tap writes.
const ActionTapRateLimited = "tap.rate_limited"

// Tap budgets. The arithmetic is written down because "generous" is not a
// number, and because the card's criterion — a legitimate tap never reaches it —
// can only be checked against a worst case someone stated out loud.
//
//	tapAddressLimit
//	  3000 requests per 10 minutes from one address (an IPv6 /64), i.e. 5 per
//	  second sustained. A tap costs two requests: GET /t then POST /api/checkin.
//	  The whole venue shares one address, so the worst LEGITIMATE case is the
//	  biggest crew arriving at once: 300 people clocking in inside ten minutes is
//	  600 requests, and generous room for reloads and retries puts it near 900 —
//	  a factory shift change, several times the largest site in the seed
//	  fixture (Kebab Factory's reference crew is 15). 3000 leaves 3x headroom
//	  above that, and still costs an attacker a sustained 5 requests/second
//	  before anything is shed.
//
//	tapSessionLimit
//	  300 requests per 10 minutes from one SESSION, i.e. one every two seconds
//	  sustained. One employee taps a handful of times a day, and the guardrail
//	  debounce already turns anything under 60 seconds into a RECORDED `ignored`
//	  decision rather than an error — so this number is not about repeat taps at
//	  all, it is about a phone stuck in a reload loop. A reload every five
//	  seconds for the whole window is 120 requests and fits with 2.5x to spare
//	  (the arithmetic is a test, not a comment:
//	  TestTapLimiter_DefaultsAreWideEnoughForAShiftChange — an earlier draft said
//	  120 and that test caught that 120 is exactly the number a 5-second reload
//	  loop produces). Past this the caller is a script holding a stolen cookie,
//	  and it is the one budget an attacker cannot escape by changing network.
const (
	tapAddressLimit  = 3000
	tapAddressPeriod = 10 * time.Minute
	tapSessionLimit  = 300
	tapSessionPeriod = 10 * time.Minute
)

// TapLimitParams configures NewTapLimiter. The zero value of each budget field
// means "the constant above"; tests set them small so they do not have to send
// three thousand requests to prove a limit exists.
type TapLimitParams struct {
	Audit         AuditRecorder
	Log           *slog.Logger
	AddressLimit  int
	AddressPeriod time.Duration
	SessionLimit  int
	SessionPeriod time.Duration
}

// NewTapLimiter builds the shield.
func NewTapLimiter(p TapLimitParams) *TapLimiter {
	pick := func(v, def int) int {
		if v <= 0 {
			return def
		}
		return v
	}
	pickd := func(v, def time.Duration) time.Duration {
		if v <= 0 {
			return def
		}
		return v
	}
	log := p.Log
	if log == nil {
		log = slog.Default()
	}
	return &TapLimiter{
		addr:  NewLimiter(pick(p.AddressLimit, tapAddressLimit), pickd(p.AddressPeriod, tapAddressPeriod)),
		sess:  NewLimiter(pick(p.SessionLimit, tapSessionLimit), pickd(p.SessionPeriod, tapSessionPeriod)),
		audit: p.Audit,
		log:   log,
	}
}

// ByAddress meters every request by resolved client address. Mount it FIRST, in
// front of any database work.
func (t *TapLimiter) ByAddress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n := t.addr.Charge(RateKey(ClientIP(r))); n > t.addr.Limit() {
			t.refuse(w, r, t.addr, n, "address")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// BySession meters requests that carry a resolved session. Mount it AFTER
// Identify.
//
// THE ORDER IS ENFORCED, NOT MERELY DOCUMENTED. An audit measured the earlier
// version: with Identify missing, IdentityOf returns the zero Identity, its
// Session.ID is uuid.Nil, and 100 of 100 requests sailed through UNMETERED while
// the file called the order "the contract". A contract nothing checks is a
// comment. SessionUnresolved is now answered with 500 and an ERROR log, because
// it can only mean one of two things — this middleware was mounted without
// Identify, or resolution failed — and both are conditions under which metering
// is not happening. Shedding is the fail-closed answer; passing is not.
//
// ⚠️ SessionAbsent IS NOT THAT CASE and must not be confused with it. A tap with
// no session is LEGITIMATE (§5 row 3: serve the activation page, write no
// record), so it passes through UNMETERED here — not because it is harmless, but
// because ByAddress already meters it and because the alternative (one shared
// "anonymous" bucket) is the exact lockout M5-02 measured: unattributable traffic
// starving attributable traffic. The polarity of SessionState is what makes the
// two distinguishable at all, and this is the consumer that uses it.
func (t *TapLimiter) BySession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := IdentityOf(r)
		if id.State == SessionUnresolved {
			t.log.Error("tap rate limit: no resolved identity on the request",
				"hint", "mount httpx.Identify before TapLimiter.BySession",
				"err", id.Err, "path", r.URL.Path)
			writeUnavailable(w)
			return
		}
		if id.Session.ID == uuid.Nil {
			next.ServeHTTP(w, r)
			return
		}
		if n := t.sess.Charge(id.Session.ID.String()); n > t.sess.Limit() {
			t.refuse(w, r, t.sess, n, "session")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// refuse answers 429 and leaves the trail described on TapLimiter: an audit row
// when a tenant is known, a WARN line either way, each written once per window.
func (t *TapLimiter) refuse(w http.ResponseWriter, r *http.Request, l *Limiter, count int, scope string) {
	if l.FirstOverLimit(count) {
		id := IdentityOf(r)
		// The address is in the LOG, never in the audit detail: audit_log is
		// permanent and a source address is personal data we have no reason to
		// keep for years. The activation flow draws the same line.
		t.log.Warn("tap rate limit reached", "scope", scope,
			"ip", RateKey(ClientIP(r)), "limit", l.Limit(), "period", l.Period().String(),
			"employee_id", id.EmployeeID())
		if t.audit != nil && id.TenantID() != uuid.Nil {
			if _, err := t.audit.Record(r.Context(), audit.Event{
				TenantID: id.TenantID(),
				Action:   ActionTapRateLimited,
				Target:   id.EmployeeID().String(),
				Detail: tapLimitDetail{
					Scope:  scope,
					Limit:  l.Limit(),
					Period: l.Period().String(),
				},
			}); err != nil {
				// Never swallowed (§7): a trail that cannot be written means
				// §4.6 is not being kept, even though the refusal itself stands.
				t.log.Error("audit write failed", "action", ActionTapRateLimited, "err", err)
			}
		}
	}
	writeTooManyRequests(w, l.Period())
}

// tapLimitDetail is the hand-written detail for the audit row. Typed, with known
// fields, per internal/audit's rule: never a dumped request or params struct.
type tapLimitDetail struct {
	Scope  string `json:"scope"`
	Limit  int    `json:"limit"`
	Period string `json:"period"`
}

// writeUnavailable answers a request the server cannot meter. It says "try
// again" rather than naming the wiring fault: the fault is ours, the caller can
// do nothing with it, and an error page is not the place to describe the
// middleware stack.
func writeUnavailable(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte("Something went wrong on our side. Please tap again.\n"))
}

// writeTooManyRequests answers 429 in plain text.
//
// PLAIN TEXT, NOT A BRANDED PAGE, deliberately. This is an abuse shield, not a
// screen: designing one would mean a templ view and the tappa-brand skill
// (CLAUDE.md §9), and the employee tap screen it would sit beside does not exist
// yet. M5-04 may replace this with a branded page; the status code, the
// Retry-After and the no-store are the parts that matter and they do not change.
//
// Retry-After is the whole window rather than the time left in it: a fixed
// window would have to expose its start to say anything tighter, and overstating
// the wait is the polite direction.
func writeTooManyRequests(w http.ResponseWriter, retryAfter time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write([]byte("Too many requests. Please wait a moment and tap again.\n"))
}
