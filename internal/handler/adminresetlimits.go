package handler

import "time"

// The PASSWORD-RECOVERY budgets — M7-04 phase B's first acceptance criterion, and
// the obligation internal/adminauth/reset.go hands forward in two separate places
// ("what bounds the attack is a PER-ADDRESS RATE LIMIT on the request endpoint,
// which is a PHASE B OBLIGATION") and ADR 0015 records twice under accepted risks.
//
// 🔴 "PER ADDRESS" MEANS THE CALLING ADDRESS IN THIS REPOSITORY, AND THE CARD'S
// PHRASE COULD BE READ THE OTHER WAY. Both readings were measured against
// adminratelimit.go's own argument, which spends its longest paragraph refusing a
// per-account gate:
//
//	GATE ON THE EMAIL      closes the whole of harm (a) from every source at once —
//	                       and IS harm (a). A budget of N per window is spent by an
//	                       attacker in N requests from anywhere, after which the
//	                       account owner's OWN recovery request is refused. The card
//	                       names harm (a) as "kurtarma reddi"; an email-keyed gate is
//	                       a recovery denial with a shorter attack and no race to
//	                       win. ELIMINATED, and it is the sibling of the class
//	                       adminratelimit.go already refused for sign-in.
//	GATE ON THE ADDRESS    bounds REPETITION from one source. An attacker with A
//	                       addresses gets A x the ceiling; nobody is ever refused
//	                       recovery on account of somebody else's traffic, except
//	                       where they genuinely share an address (one NAT), which is
//	                       the same residual every other budget in this product
//	                       carries. CHOSEN.
//
// 🔴 AND A RATE LIMIT DOES NOT CLOSE HARM (a) — IT BOUNDS ITS REPETITION. This has to
// be written down because the card's wording ("tek bir sayı iki ayrı zararı birden
// kapatıyor") reads as closure and the arithmetic does not support it. Harm (a) is
// complete in ONE request: the victim's pending link dies the moment
// CreatePasswordReset retires it (retired_count=1), and no ceiling above 1 prevents
// the first one. What the ceiling buys is that ONE address can extinguish at most
// adminResetRequestLimit pending links per window instead of unboundedly many —
// which turns "hold the victim out forever, for free" into "keep paying, from a new
// address each time, and be visible in the flood log while you do".
//
// WHAT ACTUALLY CLOSES IT is a design in which issuing does not retire siblings, and
// ADR 0015 measured the price of that (M6-05's harm: a live link in somebody's inbox
// after a newer one was issued) and chose retirement. The residual is accepted there
// and bounded here; neither file claims it is gone.
//
// 🔴 THE CARD PUTS BOTH HARMS ON ONE ENDPOINT AND THEY ARE ON TWO. Harm (a) is
// produced by Issue, on the REQUEST endpoint; harm (b) — a full cost-12 bcrypt paid
// before the token is even looked up — is produced by Consume, on the SET-A-NEW-ONE
// endpoint. One number cannot bound both, and sharing one BUCKET between them would
// be worse than either: an attacker spending the request budget would then also
// refuse the victim's attempt to FINISH a recovery, which is harm (a) reached by a
// second route. So there are two ceilings, on two keys, and the split is the design.
//
// LIMITS INHERITED FROM THE PRIMITIVE (httpx.Limiter, stated where it lives). The
// first three are the ones every budget in this product carries; the FOURTH is here
// because this file is the first to key a budget on a UUID, which is what makes it
// reachable at all:
//
//	IN-PROCESS       cleared by a restart.
//	FIXED WINDOW     a burst spanning a boundary can reach 2x the limit. Every figure
//	                 below is stated with that factor.
//	PER PROCESS      two app processes keep two sets of counters.
//	🔴 FAIL-OPEN AT 100 000 KEYS. Limiter.evictLocked drops expired windows and, if
//	                 that frees nothing, replaces the whole map — so at the cap every
//	                 live counter is FORGOTTEN rather than any request being refused.
//
// WHY THE FOURTH IS NAMED HERE AND NOT ELSEWHERE: an address-keyed budget is bounded
// by how many source addresses exist, but adminResetLinkLimit is keyed on a reset
// row's UUID, i.e. on a value an attacker can cause to be created. So this file is
// the first place where "fill the map" is a shape worth thinking about.
//
// ✅ AND THE DIRECTION IS MEASURED TO BE HARMLESS, WHICH IS WHY IT IS A NOTE RATHER
// THAN A DEFECT. Forgetting a counter forgets a SUPPRESSION: the next event is
// written instead of dropped. So the failure direction is MORE audit rows, never
// fewer — §4.6 cannot be reached this way, and the only cost is that a suppression
// window restarts early. (An attacker who wanted MORE rows in somebody's audit_log
// does not need this: they can simply stay under each budget.)
//
// AND REACHING IT IS EXPENSIVE IN THE ONE DIRECTION THAT MATTERS: 100 000 live keys
// in one window means 100 000 distinct reset rows refused in ten minutes, and every
// refusal also spends a unit of adminResetSubmitLimit — 10 per address — so it takes
// on the order of 10 000 distinct source addresses. That is a botnet spent to make
// the trail MORE complete.
const (
	// adminResetRequestLimit / adminResetRequestPeriod: how many recovery REQUESTS
	// one address may make per window. It is the ceiling that bounds harm (a).
	//
	// DERIVED FROM OPERATOR BEHAVIOUR, the way every budget in adminratelimit.go is:
	//
	//	10 admins behind one NAT (this product's office model)     10
	//	x2 for "the mail did not arrive, press it again"           20   <- the limit
	//
	// WHAT IT COSTS US AT THE CEILING. One request pays one resolver read and then,
	// per identity inside adminauth.ResetWindow, TWO tenant-scoped transactions: the
	// address read and the fused insert-and-retire. So the DATABASE OPERATION count is
	// 1 + 2k — three at this database's median (one identity per address) and
	// seventeen at a full window. MEASURED end to end over real HTTP (the probe at
	// resetRequestFloor below): 1.4-2.6 ms for k=0, 9.9-14.4 ms for k=1 and
	// 60-64 ms for k=8, medians of three runs.
	//
	//	20 x 60-64 ms = 1.2-1.3 s of request time per window per address at the
	//	                worst k, worst case 2x that across a fixed-window boundary
	//
	// WHY IT IS NOT LOWER, given that a lower ceiling bounds harm (a) more tightly:
	// because the thing it would refuse first is a real operator who cannot get into
	// their panel, and this budget is the only one on this path that can refuse them.
	// Ten is what one shared office needs; twenty is that with the one retry a person
	// actually makes. Below ten the office case starts losing.
	//
	// WHY IT IS NOT HIGHER: every unit is one more pending link an attacker can
	// extinguish, and 20 per window per address is already 2 880 per day from a
	// single source — loud, and far past anything a person does.
	//
	// ⚠️ IT REFUSES BEFORE ANY DATABASE WORK AND IT REFUSES EQUALLY. The screen a
	// refused request gets is the SAME rate-limit screen the panel already serves,
	// and it does not depend on whether the address is registered: the budget is
	// checked before the address is resolved, so a refusal cannot answer the question
	// the flow spends the rest of its length refusing to answer.
	adminResetRequestLimit  = 20
	adminResetRequestPeriod = 10 * time.Minute

	// adminResetSubmitLimit / adminResetSubmitPeriod: how many NEW-PASSWORD
	// submissions one address may make per window. It is the ceiling that bounds
	// harm (b), and the arithmetic is the argument:
	//
	//	10 submissions x ~213 ms (one cost-12 comparison, reset.go's measurement)
	//	  = ~2.1 s of CPU per 10-minute window per address = ~0.36% of one core.
	//	Worst case across a fixed-window boundary: 20 submissions, ~4.3 s, ~0.7%.
	//
	// AGAINST THE UNBOUNDED CASE THIS REPLACES: reset.go states it plainly — the
	// digest is computed BEFORE the token is resolved, on purpose, because resolving
	// first would answer "does this token exist" in microseconds and separate it from
	// a real one by 213 ms. So every submission carrying any well-formed 43-character
	// string costs a full bcrypt, and a hundred concurrent ones saturate the box.
	// That is M7-02's harm class, and it reaches §4.6 through the shared pool the tap
	// surface depends on (adminLoginWorkLimit says the same from its side).
	//
	// TEN IS GENEROUS FOR THE LEGITIMATE ACT. Setting a new password is ONE
	// submission; the form's own checks (both boxes filled, both equal, length inside
	// adminauth's bounds) are made BEFORE Consume is called, so a mistyped
	// confirmation costs no bcrypt at all and spends no unit of this budget. Reaching
	// ten means ten submissions that each carried a plausible link and a usable
	// password.
	//
	// ⚠️ IT CAN REFUSE A LEGITIMATE RECOVERY FROM A SHARED ADDRESS, which is the same
	// trade adminAttemptLimit makes and is keyed the same way — on the address, never
	// on the account or on the link — so it is not a lockout that a stranger can aim
	// at a named person.
	//
	// 🔴 AND THE DISTRIBUTED CASE IS COMPUTED HERE RATHER THAN LEFT TO THE PREAMBLE.
	// The shared preamble above says an attacker with A addresses gets A x the ceiling;
	// this budget's own arithmetic was written per-address only, so the number that
	// matters for a botnet was nowhere on the page:
	//
	//	    1 address    10 x 0.213 s =     2.1 CPU-s per window = ~0.4% of one core
	//	  500 addresses 5000 x 0.213 s =  1065   CPU-s per 600 s = ~1.8 CORES
	//	 5000 addresses                 = 10650   CPU-s per 600 s = ~17.8 CORES
	//
	// THAT IS NOT A LOOSENING AND THE COMPARISON IS THE EVIDENCE: adminLoginWorkLimit
	// allows 120 x 8 x 0.38 s = ~365 CPU-s per window PER ADDRESS, i.e. one address on
	// the sign-in path buys 174x what one address buys here. A distributed attacker is
	// bounded by nothing in-process on either surface, and adminratelimit.go already
	// names the honest mitigation for that class: it is operational (a proxy-level
	// limit, M8) rather than something a per-address counter can reach. Written down
	// instead of being papered over.
	adminResetSubmitLimit  = 10
	adminResetSubmitPeriod = 10 * time.Minute

	// adminResetAccountLimit / adminResetAccountPeriod: REQUEST-SIDE recovery events
	// written to audit_log for ONE administrator per window — `requested` and
	// `undelivered`, and nothing else. It bounds audit_log ONLY and it NEVER refuses
	// a request; adminAccountLimit's design, applied to the second unauthenticated
	// surface that can write into a named tenant's trail.
	//
	// 🔴 IT IS NEEDED HERE FOR THE M5-02 REASON, AND THE SHAPE IS SHARPER THAN ON THE
	// SIGN-IN PATH. A recovery request against a KNOWN address writes a row into the
	// tenant that owns it, from a form that needs no credential at all: unbounded,
	// that is a write primitive into somebody else's append-only table, in the one
	// table not even tappa_owner can delete from.
	//
	// 🔴🔴 AND ITS SCOPE IS NARROWER THAN IT WAS, BECAUSE THE FIRST VERSION SILENCED A
	// SUCCESSFUL PASSWORD CHANGE. Every audit row in this flow went through this one
	// counter, so eleven anonymous POSTs to the public request form burnt the budget
	// and the NEXT completed reset wrote nothing at all. A security audit exploited it
	// on the real router and printed the result:
	//
	//	after 11 anonymous POST /admin/reset (this budget 10, request ceiling 20):
	//	  admin.recovery.requested      10
	//	  admin.recovery.rate_limited    1
	//	POST /admin/reset/new -> 303, password written, sessions revoked
	//	  admin.recovery.completed       0   <- the password changed and left NO trail
	//
	// Sustainable, from anywhere, at eleven requests per window against a ceiling of
	// twenty — i.e. a stranger could make somebody else's password change invisible.
	// That is §4.6's core ("kayıt asla kaybolmaz"), and the precedent refuting it was
	// already in this package and pointing the other way: adminlogin.go writes
	// ActionAdminLoginSucceeded with a direct a.record and applies its account budget
	// only inside failLogin's FAILURE loop. "A successful state transition can never
	// be silenced" is this repository's existing rule; the recovery flow broke it.
	//
	// THE DIVIDING TEST, AND IT IS THE ONE THAT SURVIVED THE MEASUREMENT: can an
	// UNIDENTIFIED caller produce this row's volume?
	//
	//	requested / undelivered   YES — anybody types an address into a public form.
	//	                          Budgeted HERE, keyed on the account, because that is
	//	                          what bounds writing into a stranger's tenant.
	//	completed                 NO — it needs a live token, and spending one kills
	//	                          it. At most one row per link, and links are already
	//	                          bounded by adminResetRequestLimit. UNBUDGETED, by
	//	                          decision, and written with a direct record call.
	//	refused                   NO for MINTING one, YES for REPLAYING one: a dead
	//	                          link can be re-submitted forever by whoever holds
	//	                          it. Budgeted, but on the LINK (see below), never on
	//	                          the account — otherwise a stranger flooding the
	//	                          public form would silence the takeover signal of an
	//	                          account they do not hold a link for.
	//
	// PAST THE LIMIT the branch behaves identically and writes ONE final
	// "rate_limited" row (FirstOverLimit), then nothing. The count keeps climbing, so
	// the window still expires normally.
	//
	// ⚠️ IT IS STILL A TRAIL-SILENCING PRIMITIVE FOR WHAT IT COVERS, exactly as
	// adminAccountLimit is, and the residual is stated in the same words: an
	// investigator always sees THAT the account was targeted and when suppression
	// began, and cannot get the total from the database. Making the total recoverable
	// needs a write when the WINDOW CLOSES, and httpx.Limiter has no expiry hook.
	adminResetAccountLimit  = 10
	adminResetAccountPeriod = 10 * time.Minute

	// adminResetLinkLimit / adminResetLinkPeriod: refusals recorded against ONE
	// recovery link per window, keyed on the reset row's own UUID.
	//
	// IT IS ratelimit.go's `invite` BUDGET, VERBATIM IN SHAPE AND IN REASONING: "per
	// invitation (keyed by its UUID, never by the code or its hash — an in-memory map
	// key ends up in heap dumps, and a code hash is a bearer credential, §4.7)". A
	// spent or expired recovery link can be re-submitted for as long as its holder
	// likes, and each replay would otherwise append a permanent row to a table nothing
	// can delete from. Same shape, same key discipline: the reset ROW id, never the
	// token and never its hash.
	//
	// 🔴 WHY NOT THE ACCOUNT BUDGET, which is where these rows used to go. Because the
	// two counters must not be spendable by the same person: `requested` is reachable
	// by ANY anonymous caller who knows an address, so putting `refused` on that
	// counter lets a stranger silence the one signal that says "somebody is replaying
	// a link against this account" — the takeover signal — without ever holding a
	// link. Keyed on the link, the only person who can burn it is the person who has
	// one, and burning it silences nothing about any other link or any other account.
	//
	// TEN IS ratelimit.go's NUMBER AND THE SAME DERIVATION HOLDS: a person re-opening a
	// dead link a few times, or double-pressing the button, stays well inside it; a
	// loop replaying a consumed link does not. It gates nothing — the response is the
	// same refusal screen at every count.
	adminResetLinkLimit  = 10
	adminResetLinkPeriod = 10 * time.Minute

	// adminResetUnknownLimit / adminResetUnknownPeriod: unattributable recovery
	// failures logged per address per window.
	//
	// IT BOUNDS THE PROCESS LOG AND NOTHING ELSE, which is ratelimit.go's `unknown`
	// budget for the activation flow, needed here for the same mechanical reason: a
	// submitted link that resolves to NO row has no tenant, and audit_log.tenant_id
	// is NOT NULL with an FK (00005), so the only place that attempt can be recorded
	// is a log line — and a log line an anonymous caller can produce at will is an
	// unbounded disk writer. Past this ceiling the branch answers identically and
	// stops writing.
	//
	// 60 IS ratelimit.go's NUMBER AND THE SAME DERIVATION HOLDS: it is far above what
	// a person produces (one dead link opened a few times) and far below what a loop
	// does. It gates nothing.
	adminResetUnknownLimit  = 60
	adminResetUnknownPeriod = 10 * time.Minute
)

// resetRequestFloor is how long POST /admin/reset takes, whatever it did.
//
// 🔴 IT IS THE FIFTH ACCEPTANCE CRITERION'S LAST WORD — "kayıtlı ve kayıtsız adres
// için yanıt durum, gövde, ifade VE ZAMANLAMA bakımından aynı" — and it is the only
// one of the four that a body-comparison test cannot see. The work behind the two
// answers is genuinely different and cannot be made equal: an unregistered address
// costs ONE resolver read, and a registered one costs two tenant-scoped transactions
// per identity plus a delivery. Padding the WORK is not available (a database round
// trip cannot be faked without holding a pool connection to waste it), so the
// RESPONSE is held to a floor instead — the same shape internal/adminauth's
// padComparisons uses on the sign-in path, moved from CPU to the clock because there
// is no bcrypt here to hide behind.
//
// 250 ms IS CHOSEN ABOVE THE MEASURED WORST CASE WITH HEADROOM.
//
// ⚠️ THE FIRST VERSION OF THIS PARAGRAPH CARRIED THREE NUMBERS THAT HAD NOT BEEN
// TAKEN (2.1 / 6.4 / 36.7 ms). Two of them were low by 1.7x and 2.3x. They are
// replaced by a real probe — the request driven over real HTTP against real Postgres
// with this constant set to zero, six samples per arm, three runs at load average
// 3.8-4.6 — because a number that arrives before its measurement is the defect this
// repository has paid for repeatedly:
//
//	                                  median (3 runs)      slowest sample seen
//	unregistered address              1.4 / 1.6 / 2.6 ms          6.8 ms
//	registered, one identity           9.9 / 10.7 / 14.4 ms      21.9 ms
//	registered, a full ResetWindow    60.4 / 62.8 / 63.5 ms     152.4 ms
//
// SO THE FLOOR IS ABOUT 4x THE SLOWEST ARM'S MEDIAN and 1.6x the slowest single
// sample this probe saw — not the 7x the invented figures implied. It stays at 250 ms
// rather than rising: the 152 ms outlier is one sample in eighteen on a machine
// running its own test suite, and raising the floor costs held goroutines linearly
// (adminResetRequestLimit x the floor is the bound on that) while buying margin
// against a tail that the successor design below removes entirely. It does not fall,
// because the full-window arm is already a quarter of it.
//
// ⚠️ WHAT IT DOES NOT GUARANTEE, and the sentence is deliberately weaker than "the
// timing is identical": a floor equalises everything that finishes UNDER it. Work
// that overruns — a slow query, a saturated pool, and above all a real mail transport
// once Q02 is answered — leaks again, and the leak is proportional to the overrun
// rather than bounded. THE SUCCESSOR IS NAMED RATHER THAN IMPLIED: when a transport
// exists, the send belongs off the request path entirely (a detached worker with its
// own context), at which point the response time stops depending on the recipient at
// all. That is infrastructure this product does not have yet, and building it for a
// channel that does not exist would be guessing at its shape.
const resetRequestFloor = 250 * time.Millisecond
