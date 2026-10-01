package operatorauth

import (
	"sync"
	"time"
)

// THE OPERATOR'S BUDGETS (ADR 0020 §3: "adres (flood) · iş (attempt) · hesap (account)"
// -- the panel's three, internal/handler/adminratelimit.go -- plus ADR 0021 §1's
// process-wide ceiling on pre-session audit rows). The ADR fixed the mechanism and
// left the numbers to OP-6/OP-8; they are derived here the way adminratelimit.go
// derives the panel's, from the people who legitimately use the surface, and each one
// then reports what it costs.
//
// THE POPULATION IS NOT THE PANEL'S. The panel sizes for "10 admins behind one NAT";
// the operator surface has ONE to THREE people (Taptime staff, ADR 0020 §8: no
// multi-operator roles), each with a laptop and a phone. Every number below starts
// there.
//
// 🔴 THE ORDER IS THE PROPERTY, AND IT IS THE OP-6 ACCEPTANCE: a request a budget
// refuses pays for nothing after it and writes nothing -- no bcrypt, no row, no counter
// it would move. That is ADR 0020 §3's "limiter'ın reddettiği istek satır yazmaz" and
// "sayacı artırmaz". WHERE each budget sits, as flow.go orders it: flood, work and
// account are charged first thing on their paths, BEFORE anything is verified (the
// account budget before the code is checked); the two ENROLLMENT budgets -- enrollAddr,
// then enroll -- are charged, by design, AFTER every check Go can make (the password
// rule, the token's shape, the page, the first code, whose refusals write their own
// enrollment_failed rows) and BEFORE the digest, the seal and the database call
// (TestLimits_ARefusedRequestWritesNoRowAndMovesNoCounter drives the real budgets,
// built by build -- the constructor path New takes -- against the real database).
//
// THE PRIMITIVE IS A LOCAL COPY OF httpx.Limiter'S MECHANISM, AND THE COPY IS THE
// DEPENDENCY DIRECTION, NOT A TASTE. internal/httpx imports the panel's authentication
// (adminidentity.go: RequireAdmin takes adminauth types), so importing httpx from here
// would make this package depend on the customer panel's auth -- the separation this
// package's header states -- and ADR 0020 §4's requireOperator is the obvious next
// resident of httpx (RequireAdmin's precedent): the day OP-8 puts it there, httpx
// imports this package and an import of httpx from here is a cycle that does not
// compile. So the fixed-window counter below repeats httpx.Limiter's (one map, one
// mutex, the same maxKeys eviction) with this package's injectable clock, and it
// inherits the same stated limits: in-process, cleared by a restart, a FIXED window
// (a burst across a boundary reaches 2x). With one replica that is the whole
// deployment; a second replica doubles every ceiling here.
const (
	// floodLimit: every request to the operator surface, per client address -- OP-8's
	// floodGate, the first budget in ADR 0020 §4's chain. It bounds DATABASE work per
	// source (a session touch is one definer call).
	//
	//	3 operators x (1 sign-in = 3 requests + ~20 page views)   ~70 per window
	//	x ~4 headroom                                              300
	//
	// At the panel's measured 3-6 ms for a live-session request (resolver + UPDATE,
	// adminratelimit.go), 300 is at most ~1.8 s of database time per window per
	// address.
	floodLimit  = 300
	floodPeriod = 10 * time.Minute

	// workLimit: every request that reaches a bcrypt -- the password step and the
	// enrollment's final step -- per client address, WHATEVER ITS OUTCOME (charged
	// before the comparison, so a success spends it too: adminLoginWorkLimit's lesson
	// that a failure-only budget leaves the success path unbounded).
	//
	//	3 operators x 2 devices x 1 sign-in                          6 per window
	//	+ a few mistyped passwords                                  ~10
	//	x 2 headroom                                                 20
	//
	// COST: 20 x one cost-12 comparison. At the panel's pinned 380 ms (the security
	// budget adminauth keeps, not today's faster reading) that is ~7.6 CPU-s per window
	// per address, ~1.3% of one core. It can refuse a correct password from an address
	// that spent it -- the panel's adminLoginWorkLimit trade: keyed on the ADDRESS, so
	// nobody can spend a named operator's budget from elsewhere.
	workLimit  = 20
	workPeriod = 10 * time.Minute

	// accountLimit: every TOTP attempt, per OPERATOR (the id the signed challenge
	// names), charged BEFORE the code is checked.
	//
	// 🔴 THIS ONE GATES, AND ON THE PANEL THE ACCOUNT BUDGET DELIBERATELY DOES NOT
	// (adminratelimit.go: a per-account gate lets anyone who knows an owner's address
	// lock them out). The difference is who can spend it: a challenge is minted only by
	// a CORRECT PASSWORD, so this budget is spendable only by someone who already has
	// the operator's password -- and "parolayı bilen biri tasarım gereği kilitleyebilir"
	// (m10-platform.md, OP-5 B3). It must gate, because the alternative is worse: every
	// TOTP failure writes the totp_failed row that moves the database's lock counter,
	// and a failure that could not be recorded must not be a guess that was checked.
	// Charging first means a refused attempt is never evaluated, so no guess escapes
	// the counter.
	//
	// 10 per window is twice the database's lock threshold (N = 5, 00026): a person
	// meets the lock long before this, which is the design -- this budget bounds rows
	// and work per account, the lock bounds guesses. Rows it can write per account:
	// 10 per window = 1 440 a day.
	accountLimit  = 10
	accountPeriod = 10 * time.Minute

	// auditCapLimit: pre-session audit rows that need NO PASSWORD -- unknown_email,
	// login_failed, enrollment_failed -- PROCESS-WIDE, one key, independent of the
	// client address. ADR 0020 §3 / ADR 0021 §1 (a): op_record_auth_event is a write
	// gate reachable from the internet, and a per-address budget does not bound a
	// distributed caller. 00026 has no database-side ceiling (OP-5 measured one and
	// rejected it: it let a password-less flood switch the TOTP lock off).
	//
	//	legitimate: 3 operators x ~3 mistyped attempts               ~10 per window
	//	x 3 headroom                                                  30
	//
	// COST, MEASURED (dev Postgres 17.10, 2026-09-30, on a TEMP copy of the table --
	// same columns, defaults and three indexes -- in a rolled-back transaction; two
	// independent measurements, one summing the main fork and the indexes, one reading
	// the total relation size): heap ~85 bytes a row without a target id and ~101 with
	// one; 158.6-170.4 bytes a row in all without a target and 174.7-188.4 with one --
	// an OBSERVED RANGE that moves from run to run (the lower ends at 50 000 rows, the
	// higher at 5 000). The worst sustained case is 30 x 6 x 24 x 365 = 1 576 800 rows a
	// year, ~250-297 MB a year, on a table nothing can prune (append-only, 00026). (A
	// first reading of 139.3 bytes, ~219 MB, did not reproduce.)
	//
	// PAST THE CAP THE REQUEST IS STILL SERVED; ONLY THE ROW IS NOT WRITTEN. Refusing
	// instead would hand any botnet a switch that locks every operator out of the
	// sign-in; silence costs the trail, not the service. That is a TRAIL-SILENCING
	// primitive and it is written down as one (the panel's account budget carries the
	// same note): one WARN line per window says the rows stopped, never an address.
	// 00026's closed kind set has no "suppressed" kind to write instead -- adding one
	// is a migration and a decision.
	//
	// ⚠️ totp_failed AND locked ARE NOT UNDER THIS CAP, ON PURPOSE. The lock counter
	// moves only with a totp_failed row (00026). If password-less junk could fill this
	// cap and thereby stop totp_failed rows, it would switch the lock off -- exactly
	// what OP-5 measured against a database-side cap (m10-platform.md, OP-5 madde 10).
	// Those two kinds are bounded instead by accountLimit, which only a password holder
	// can spend, times the number of active operators: an IP-independent bound all the
	// same (TestLimits_TheAuditCapCannotSwitchTheLockOff).
	auditCapLimit  = 30
	auditCapPeriod = 10 * time.Minute

	// enrollLimit: enrollment final steps that reach bcrypt + Seal, PROCESS-WIDE (ADR
	// 0021 sınır 12: "adres başına ve süreç geneli bir oran sınırı"; workLimit is the
	// per-address half). op_complete_enrollment cannot check the token before Go has
	// paid for the digest, so an unauthenticated caller buys a cost-12 bcrypt per
	// request.
	//
	//	legitimate: one operator enrolling, a few tries              ~3 per window
	//	x ~3 headroom                                                 10
	//
	// COST: 10 x ~380 ms = ~3.8 CPU-s per window for the whole process, ~0.6% of one
	// core, whatever the number of sources.
	//
	// ⚠️ THE PRICE IS PAID BY ONE ADDRESS, NOT ONLY BY A DISTRIBUTED FLOOD (OP-6
	// verification, 2026-09-30, measured). BeginEnrollment reads no database --
	// tappa_operator can see neither a pending account nor its token hash -- so anybody
	// can open a page for a random id, read the secret it displays, type that secret's
	// valid first code and post a well-formed random token: every check Go can make
	// passes, this budget is spent, and only op_complete_enrollment refuses. workLimit
	// (20 per address) is larger than enrollLimit (10 for the process), so ten such
	// requests from a SINGLE address refuse every enrollment, from every address, for
	// the rest of the window (measured: after ten from one address, a legitimate
	// enrollment from another address got ErrThrottled). And not only for one window
	// (OP-6 verification, 3rd round, measured): each such request WRITES one
	// enrollment_failed row (ten of the audit cap's thirty per window); a NEW link does
	// not escape it, because the budget is process-wide (a fresh pending account and
	// token from a third address got ErrThrottled too); and the same address repeats it
	// in every window with half its work budget (10 of 20), for as long as it likes.
	// THE MECHANISM, stated exactly (4th round): this is a fixed window that RESETS
	// ITSELF -- the first charge after the period has passed opens a fresh window at
	// zero (charge, below). Keeping enrollments refused therefore takes a BURST of ten at
	// the start of each window, not an even stream: ten requests spread one a minute
	// would leave the budget open for most of every window. A legitimate enrollment that
	// lands in a window before that window's burst goes through. A process restart also
	// resets the counter, but the window does it anyway. No account is touched. The
	// numbers are OP-8's (ADR 0020 names them there): a per-address share
	// below enrollLimit -- e.g. 3 per address per window, which one operator's few tries
	// fit -- would make one address unable to exhaust it (four would be needed); a
	// per-id share would not help, the ids are the caller's own. (Proposed here first;
	// DECIDED in OP-6 12c -- the security audit's MEDIUM finding, the orchestrator's
	// numbers: enrollAddrLimit, below.)
	enrollLimit  = 10
	enrollPeriod = 10 * time.Minute

	// enrollAddrLimit: the per-address SHARE of enrollLimit, charged after every check Go
	// can make and BEFORE the process-wide budget (flow.go: work → enrollAddr → enroll →
	// bcrypt), so a request it refuses moves neither the process-wide counter nor
	// anything after it.
	//
	//	legitimate: one operator enrolling from one place, a few tries   ~3 per window
	//
	// One address spends at most 3 per ITS OWN window. The windows are fixed and each
	// key's opens with that key's first charge, so they are not aligned with the
	// process-wide one (measured, 12d, the closing auditor, injected clock): at a window
	// boundary one address can put up to 5 into a single process window, and two
	// addresses, after an earlier request opened the process window, can exhaust it once.
	// Keeping enrollLimit exhausted window after window takes at least FOUR rate keys.
	// COUNTED LIMIT, BY NAME: a distributed caller still can -- four IPv4 addresses, or
	// four IPv6 /64s (httpx's RateKey buckets IPv6 by /64, so one /48 holds 65 536 keys);
	// OP-8's operator-surface IP restriction (m10-platform.md, K4) or another measure is
	// OP-8's. The numbers are OP-8's to change by its arithmetic.
	enrollAddrLimit  = 3
	enrollAddrPeriod = 10 * time.Minute
)

// budgetMaxKeys bounds each budget's map, httpx's limiterMaxKeys for the same reason:
// a caller rotating source addresses must not grow memory without bound. Past it,
// expired windows are dropped and, if that frees nothing, the map is reset -- the
// fail-OPEN direction httpx chose deliberately (refusing new keys would let one
// caller lock everybody out). The two process-wide budgets have one key and the
// account budget one per operator, so only flood, work and enrollAddr can ever reach
// it.
const budgetMaxKeys = 100_000

// budget is a fixed-window counter keyed by an opaque string.
type budget struct {
	mu      sync.Mutex
	windows map[string]*budgetWindow
	limit   int
	period  time.Duration
	now     func() time.Time
}

type budgetWindow struct {
	count int
	start time.Time
}

func newBudget(limit int, period time.Duration, now func() time.Time) *budget {
	return &budget{windows: map[string]*budgetWindow{}, limit: limit, period: period, now: now}
}

// charge records one event against key and returns the count AFTER charging. The
// count keeps climbing past the limit, so the window still expires on time and
// count == limit+1 happens exactly once per window (the one WARN line).
func (b *budget) charge(key string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	w, ok := b.windows[key]
	if !ok || now.Sub(w.start) >= b.period {
		if len(b.windows) >= budgetMaxKeys {
			for k, old := range b.windows {
				if now.Sub(old.start) >= b.period {
					delete(b.windows, k)
				}
			}
			if len(b.windows) >= budgetMaxKeys {
				b.windows = map[string]*budgetWindow{}
			}
		}
		b.windows[key] = &budgetWindow{count: 1, start: now}
		return 1
	}
	w.count++
	return w.count
}

// spend charges one event and reports whether it is still within the budget.
func (b *budget) spend(key string) bool { return b.charge(key) <= b.limit }

// limits is the budget set of one Authenticator, held BEHIND A POINTER
// (Authenticator.limits): fmt reaches no method of an Authenticator value in a caller's
// unexported struct field and prints its fields by reflection; for %s %q %e %f %t %c %U
// its badVerb opens a pointer ONCE and re-prints with %v. With limits held by value, the
// *budget pointers inside were what it opened, and the budgets' maps printed their keys
// -- client addresses and operator ids (OP-6 verification, 8th round, measured: the
// 7th auditor's finding). Behind *limits it opens limits and prints the *budget
// pointers as addresses (measured on the same paths: no address appeared).
type limits struct {
	flood, work, account, auditCap, enroll, enrollAddr *budget
}

func newLimits(now func() time.Time) *limits {
	return &limits{
		flood:      newBudget(floodLimit, floodPeriod, now),
		work:       newBudget(workLimit, workPeriod, now),
		account:    newBudget(accountLimit, accountPeriod, now),
		auditCap:   newBudget(auditCapLimit, auditCapPeriod, now),
		enroll:     newBudget(enrollLimit, enrollPeriod, now),
		enrollAddr: newBudget(enrollAddrLimit, enrollAddrPeriod, now),
	}
}

// AllowRequest is the flood budget for OP-8's floodGate: one charge per operator-surface
// request from addr (the client's rate key -- httpx's resolution of the real address).
func (a *Authenticator) AllowRequest(addr string) bool { return a.limits.flood.spend(addr) }
