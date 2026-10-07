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
// built by build -- the constructor path New takes -- against the real database). The
// two ROW ceilings -- auditCap and firstFactor (OP-14 phase C) -- are each charged where
// its row would be written. Past auditCap the request is served without the row. Past
// firstFactor it is served without the row only if the window's WRITTEN rows have
// reached the cap; while they have not, the step fails closed (flow.go, recordPasswordOK:
// the 3rd round's F1; this sentence narrowed in the 5th, D4).
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
	// four IPv6 /64s (httpx's RateKey buckets IPv6 by /64, so one /48 holds 65 536 keys).
	// OP-8 (2026-10-02) KEPT these numbers and left the limit counted: what a distributed
	// caller buys is refused enrollments -- a one-time step one to three people take at a
	// time they choose -- and ten enrollment_failed rows per window, not an account;
	// the remedy is an IP restriction on the operator host at the ingress (K4, OP-9's user
	// decision), not a number here (m10-platform.md, OP-8 card correction).
	enrollAddrLimit  = 3
	enrollAddrPeriod = 10 * time.Minute

	// firstFactorLimit: 'password_ok' rows (00031; OP-14 phase C) -- the durable trail of a
	// password the step ACCEPTED, written before the second factor is asked for -- per
	// OPERATOR (the account the lookup returned), independent of the client address.
	//
	// WHO CAN SPEND IT: only a CORRECT password writes one, so only someone who holds the
	// operator's password. Without a ceiling that holder writes one row per comparison
	// the work budget lets through -- 20 per address per window, times every address
	// they hold, i.e. unbounded across a botnet, into a table nothing can prune.
	//
	//	legitimate: one operator, 2 devices x ~2 password steps each        ~4 per window
	//	            (a challenge that expires -- challengeTTL, 5 min -- before the
	//	            code is typed is a fresh password step; a mistyped code is not)
	//	x 2.5 headroom                                                         10
	//
	// ROWS: 10 per window = 1 440 a day = 525 600 a year per operator; at auditCapLimit's
	// measured band for a row WITH a target id (174.7-188.4 bytes, same table, same three
	// indexes) that is ~92-99 MB a year per operator, ~275-297 MB for three -- the order
	// of the password-less cap's worst case, and reachable only with a password.
	//
	// 🔴 NOT UNDER auditCapLimit, ON PURPOSE (the OP-14 card's C1 -- OP-6 md. 9's measured
	// conflict, one kind later). auditCap is filled by password-LESS junk anyone can send;
	// with 'password_ok' under it, a flood of unknown addresses would leave a right
	// password with no row -- the "password known, device missing" signal silenced by
	// someone who knows neither (TestPasswordOK_PasswordlessJunkCannotSilenceIt). Under its
	// own per-operator cap the signal cannot be silenced, only its REPETITION, and exactly
	// this far (3rd round): a right password is served WITHOUT a row only in a window that
	// already holds firstFactorLimit WRITTEN rows of that operator (budgetWindow.written).
	// While the cap is held by writes still in flight, or by writes about to fail and be
	// given back, a right password past it is REFUSED fail-closed -- no challenge
	// (recordPasswordOK, errFirstFactorPending).
	//
	// ⚠️ HOW THE CLAIM WAS WRONG TWICE, MEASURED:
	//   - 1st round (the third eye's B1, over TCP and on the real database): the charge was
	//     taken and the row written with the REQUEST's context; a client that hung up
	//     during the comparison spent a charge and nothing was sent -- ten aborted right
	//     passwords emptied the window's trail, the eleventh got a challenge and no row.
	//     Fixed in the 2nd round: the write runs DETACHED from the request's cancellation,
	//     on its own bound (FirstFactorRecordGrace), and a write that fails GIVES ITS CHARGE
	//     BACK (budget.refund: the same lock, its own window only, never below zero).
	//   - 2nd round (the security audit's F1, real Postgres): ten right passwords whose
	//     writes were in flight and then failed (25006) held the count at the cap while an
	//     eleventh was served past it -- a challenge and, once the ten were given back,
	//     zero rows. Fixed in the 3rd: "past the cap" is served without a row only when
	//     the window's WRITTEN rows have reached it.
	// What remains, by name: a process that dies between the charge and the write leaves
	// no row -- and no challenge, since the request dies with it; and a request that finds
	// the cap held by writes not yet done is refused (a 503 a person retries) rather than
	// made to wait.
	//
	// PAST A CAP OF WRITTEN ROWS THE REQUEST IS STILL SERVED; ONLY THE ROW IS NOT WRITTEN
	// (auditCapLimit's argument: the sign-in's gates are the work budget, the account
	// budget and the lock, not the trail's ceiling). One WARN line per window per operator
	// says the rows stopped, naming the operator by id (ADR 0020 §5) and nothing from the
	// request; a fail-closed refusal names the operator in its own line, once per window.
	firstFactorLimit  = 10
	firstFactorPeriod = 10 * time.Minute
)

// budgetMaxKeys bounds each budget's map, httpx's limiterMaxKeys for the same reason:
// a caller rotating source addresses must not grow memory without bound. Past it,
// expired windows are dropped and, if that frees nothing, the map is reset -- the
// fail-OPEN direction httpx chose deliberately (refusing new keys would let one
// caller lock everybody out). The two process-wide budgets have one key, and the
// account and firstFactor budgets one per operator (firstFactor's only for an operator
// whose right password was presented), so only flood, work and enrollAddr can ever
// reach it.
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
	// written counts the charges whose row was WRITTEN (wrote; OP-14 phase C, 3rd round):
	// count is charges -- rows written, writes in flight and writes that will fail and be
	// given back -- and only written says how many rows the window really holds.
	written int
	// warned is take's record that this window's first request served past the limit
	// without a row has been reported. A refund can bring a count back under the limit, so
	// "count == limit+1" can happen twice in one window; the flag keeps the one line.
	warned bool
	// refusedWarned is firstRefusal's record that this window's first fail-closed refusal
	// has been reported (the 3rd round's F4): one line per window, not one per request.
	refusedWarned bool
}

func newBudget(limit int, period time.Duration, now func() time.Time) *budget {
	return &budget{windows: map[string]*budgetWindow{}, limit: limit, period: period, now: now}
}

// charge records one event against key and returns the count AFTER charging. The
// count keeps climbing past the limit, so the window still expires on time and
// count == limit+1 happens exactly once per window (the one WARN line).
//
// 🔴 THE DECISION AND THE CHARGE ARE ONE LOCKED STEP (httpx.Limiter.TryCharge's rule,
// M10 EM-5A; backlog T90): every caller decides on the count THIS call returned. A
// read of the window followed by a charge is two locked steps, and N racers can all
// read "room left" before any of them charges -- so a ceiling on rows becomes a
// ceiling on nothing (TestPasswordOK_TheCapIsOneLockedStepUnderRacers).
func (b *budget) charge(key string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.chargeLocked(key).count
}

// chargeLocked is charge's step, returning the window the event went into; b.mu must
// be held. A window that has expired is REPLACED by a new one (a new pointer), never
// reset in place -- refund tells its own window from a later one by that identity.
func (b *budget) chargeLocked(key string) *budgetWindow {
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
		w = &budgetWindow{count: 1, start: now}
		b.windows[key] = w
		return w
	}
	w.count++
	return w
}

// take is charge for a budget whose charge can be GIVEN BACK and whose events are
// WRITTEN (OP-14 phase C, the first-factor cap). From ONE locked step it returns the count
// after charging; how many of the window's charges are rows actually written (wrote); the
// window the charge went into -- the token refund and wrote need; and whether this is the
// window's first charge past the limit THAT IS SERVED WITHOUT A ROW -- which is only so
// when the window's written rows have reached the limit (the 3rd round's F1: a count held
// by writes in flight or about to fail is not a trail). That last is the one WARN line --
// a flag, not "count == limit+1", because a refund can bring the count back under the
// limit.
func (b *budget) take(key string) (count, written int, window *budgetWindow, firstOver bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	w := b.chargeLocked(key)
	if w.count > b.limit && w.written >= b.limit && !w.warned {
		w.warned = true
		firstOver = true
	}
	return w.count, w.written, w, firstOver
}

// wrote records, under the same lock as every charge, that window's charge became a row.
// It counts into the charge's OWN window even if that one has since been replaced: the
// row belongs to the window it was charged in.
func (b *budget) wrote(window *budgetWindow) {
	b.mu.Lock()
	defer b.mu.Unlock()
	window.written++
}

// firstRefusal reports, once per window, the first fail-closed refusal of a request
// charged into window (the 3rd round's F4: the operator is named once per window when
// its sign-in step is refused for want of its row).
func (b *budget) firstRefusal(window *budgetWindow) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if window.refusedWarned {
		return false
	}
	window.refusedWarned = true
	return true
}

// refund gives back ONE event that take charged into window, under the same lock as
// every charge: it is decremented only while it is still key's current window -- a
// window that has expired and been replaced holds other requests' charges, never this
// one's -- and never below zero. It reports whether anything was given back. Each
// refund answers exactly one take, so within a window the count stays at least the
// charges nobody gave back (TestPasswordOK_AFailedWriteGivesItsOwnChargeBack).
func (b *budget) refund(key string, window *budgetWindow) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if w, ok := b.windows[key]; !ok || w != window || w.count == 0 {
		return false
	}
	window.count--
	return true
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
	flood, work, account, auditCap, enroll, enrollAddr, firstFactor *budget
}

func newLimits(now func() time.Time) *limits {
	return &limits{
		flood:       newBudget(floodLimit, floodPeriod, now),
		work:        newBudget(workLimit, workPeriod, now),
		account:     newBudget(accountLimit, accountPeriod, now),
		auditCap:    newBudget(auditCapLimit, auditCapPeriod, now),
		enroll:      newBudget(enrollLimit, enrollPeriod, now),
		enrollAddr:  newBudget(enrollAddrLimit, enrollAddrPeriod, now),
		firstFactor: newBudget(firstFactorLimit, firstFactorPeriod, now),
	}
}

// AllowRequest is the flood budget for OP-8's floodGate: one charge per operator-surface
// request from addr (the client's rate key -- httpx's resolution of the real address).
func (a *Authenticator) AllowRequest(addr string) bool { return a.limits.flood.spend(addr) }
