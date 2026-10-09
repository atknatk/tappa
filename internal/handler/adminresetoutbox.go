package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/mail"
)

// The recovery link's DELIVERY, taken off the request path (M10 EM-5, ADR 0022 §6).
//
// 🔴 WHY IT EXISTS: resetRequestFloor equalises only the work that finishes UNDER it.
// A real relay conversation (TLS + AUTH + DATA) can take seconds, so a synchronous
// send made a registered address measurably slower than an unregistered one — the
// enumeration question the whole request form refuses to answer, answered by the
// clock. Here the request MINTS synchronously (IssueForEmail) and hands
// each grant to a bounded, in-process outbox WITHOUT BLOCKING; one worker goroutine
// sends, then writes the grant's one outcome row. The response no longer depends on
// the send at all.
//
// ONE WORKER, deliberately: at the shutdown drain at most ONE send is in flight, so
// at most one link can be "sent but recorded undelivered" by the drain's cut (counted
// limit 15's window), and the rows of QUEUED grants are written in the order the
// outbox took them. A grant the outbox refused (full or closing) has its row written
// by its own request, so those rows can interleave with the worker's in any order.
//
// WHO WRITES A ROW, AND WHY THE BUDGET IS ONE STEP: the worker for a queued grant,
// the request itself for a refused one — so N goroutines can write rows for ONE
// administrator at once. The account audit budget is therefore charged with
// httpx.Limiter.TryCharge (read and write under one lock): with "Allowed, then Charge"
// the second-round audit measured up to 24 rows against a budget of 10, and the one
// rate_limited row lost.
//
// THE CLAIM, IN THREE PARTS (agent-brief, M10 OP-6/OP-7). Threat model: these pins
// hold against ACCIDENTAL drift in this file, its callers and cmd/tappa's shutdown
// sequence; code written on purpose to get past a pin is the subject of code review.
//
// PART I — TODAY'S CODE, MEASURED:
//   - with the channel taking 2 s, the registered and the unregistered medians both
//     sit in [resetRequestFloor, resetRequestFloor+50ms] (measured 250.3–251.8 ms
//     under -race) — TestAdminReset_TimingIsFlatWhileTheRelayTakesTwoSeconds; its
//     control times the synchronous path (h.deliver) at >= 2 s, outside the band;
//     the same with the real transport and an SMTP relay holding its reply 2 s —
//     TestEmailResetChannel_TimingIsFlatWithASlowRelay (resetmail_test.go);
//   - a grant INSIDE its account's audit budget ends in exactly ONE row (requested or
//     undelivered) on the five paths — sent (three accounts behind one address), send
//     failed, outbox full (one in flight + resetOutboxSize waiting + one more) or
//     closed (offered after the drain began), worker panic before the row, shutdown
//     drain (one in flight + two queued, the trail refusing writes on an ended
//     context as Postgres does) — a panic AFTER the row adds none, and past the
//     budget a grant has no row of its own: 12 failing grants of one administrator
//     give the budget's 10 rows and one rate_limited row —
//     TestResetOutbox_EveryGrantInsideTheBudgetEndsInExactlyOneRow;
//   - with the outbox full, the request still answers within floor+50ms, the
//     overflowing grant's undelivered row exists when its response returns, and the
//     grant is never sent —
//     TestAdminReset_AFullOutboxRecordsTheGrantAtOnceAndDoesNotWait;
//   - twenty requests at once for ONE administrator through the router, from twenty
//     addresses, with the outbox empty (the worker writes), closed (every request
//     writes) and full (every request writes while the worker holds a grant): one
//     page for all and EXACTLY the budget's 10 rows plus one rate_limited row —
//     TestResetOutbox_ConcurrentRequestsForOneAccountStayInsideItsBudget; the same
//     race hammered directly — 40 fallback rows for one administrator at once (100
//     runs), 60 grants racing worker and fallback (30 runs), 40 refusals of one link
//     (100 runs) — exactly the budget and one rate_limited row in every run —
//     TestResetOutbox_RacingRowWritersStayInsideTheBudget (and the primitive itself:
//     internal/httpx's TestLimiter_TryChargeIsExactUnderConcurrency);
//   - a send panicking with the recipient and the link in its value becomes an
//     undelivered row, neither reaches the log, and the same worker delivers the
//     next grant — TestResetOutbox_APanickingSendBecomesUndeliveredAndTheWorkerLives;
//     the send AND the audit writer panicking in one grant still leave the worker
//     alive and the next grant requested —
//     TestResetOutbox_ASecondFaultLeavesTheWorkerAlive;
//   - the request's context cancelled while its send is in flight cancels nothing —
//     TestResetOutbox_TheRequestEndingDoesNotCancelTheSend; a visitor who leaves while
//     the outbox is closing or full still gets the fallback row (a trail refusing
//     ended contexts) — TestResetOutbox_AClientThatLeavesStillGetsItsFallbackRow;
//   - the EIGHT lines the flow writes about a grant or a budget — the send failure in
//     both of deliver's branches (a plain error, and the *mail.SendError the e-mail
//     channel returns for every relay refusal, whose keys are also held to ADR 0022
//     §10's closed set), the account budget's and the link budget's lines, a failed
//     audit write, the panic, the refused hand-over and the e-mail channel's accepted
//     line — carry the originating request's id through the process logger's wrapper
//     — TestResetOutbox_TheGrantAndBudgetLinesCarryTheRequestsID; lines about
//     anything else (the request path's own refusals) are not measured and carry no
//     request id (ADR 0022 EM-5A note, limit 16, names them);
//   - the request, submit and process-log budgets stay exact for many callers from
//     ONE address at once (100, 100 and 160 callers, 200 runs each, every request
//     built before the start line): exactly 20 answered, 10 reaching Consume, 60
//     lines — TestAdminResetBudgets_EveryGateIsExactUnderConcurrentCallers;
//   - 200 times, 30 hand-overs racing one Drain never send on the closed queue —
//     TestResetOutbox_AnOfferRacingTheDrainNeverSendsOnAClosedQueue;
//   - with the process-wide breaker refusing (M10 EM-7A; the real e-mail channel and
//     mail.Breaker over a recording sender), each grant of a registered address has
//     its undelivered row — the breaker's reason — BEFORE the response returns, the
//     outbox holds nothing and nothing is sent, the handler writes no line for the
//     refused grants (the breaker's one trip line is the only line), and the
//     response's status, headers and body are byte-identical to the same request's
//     with the breaker passing and to an unregistered address's, each inside
//     [floor, floor+50ms] with a full adminauth.ResetWindow of grants (the most rows
//     the tripped branch writes before the floor ends) —
//     TestAdminReset_ATrippedBreakerRecordsTheGrantAtOnceAndQueuesNothing; a grant
//     that passed the question and met a full window at the worker ends undelivered
//     with the send-failed reason, its failure line naming class=breaker and
//     smtp_code=0 — TestResetOutbox_AGrantTheBreakerRefusesAtTheWorkerIsUndelivered;
//   - a drain whose whole budget passes while a row is being written returns an
//     error at the deadline and the worker is gone within a second, its rows
//     abandoned and logged; a request that mints AFTER that (still inside the HTTP
//     drain) has its fallback row written, because that row's context is the
//     request's and not the spent budget's —
//     TestResetOutbox_ASpentBudgetAbandonsTheRowBeingWritten; a context made after
//     its stopper ended is already ended when boundedBy returns —
//     TestResetOutbox_AnEndedStopperEndsTheContextAtOnce;
//   - a FULL outbox's 33 rows, written by the real audit recorder into real
//     Postgres after the sends stop, fit ResetDrainWriteReserve (measured 116–184 ms
//     by the builder and 150–294 ms by the auditor, under -race and load) — and since
//     M10 EM-7C each row is preceded by its link's withdrawal through the real
//     adminauth.Resets.Withdraw, and the two together still fit (180–512 ms, three
//     runs under -race) — TestResetOutboxDB_AFullOutboxFitsTheWriteReserve;
//   - the drain runs AT THE SAME TIME as the HTTP drain: with a 2 s request in
//     flight and a send stuck in the relay, the listener refused a new connection
//     about 0.2 ms after shutdown() was called (bound 300 ms) and the sequence took
//     2.05–2.12 s against a threshold of 3.4 s; the three queued grants each ended
//     with one undelivered row, the relay seeing only the first — cmd/tappa's
//     TestShutdown_DrainsTheResetOutboxAlongsideTheHTTPServer (ADR 0022 §6.6(b));
//     ResetDrainGrace nests inside httpShutdownGrace —
//     TestShutdownBudget_TheResetDrainNestsInsideTheHTTPGrace (§6.6(a)); the shipped
//     binary boots with "email", serves the deliverable form and exits 0 on SIGTERM —
//     TestArtifact_BootsAndStopsWithTheResetEmailChannel.
//
// PART II — NAMED PINS AND EXACTLY WHAT EACH CATCHES (each mutation was run, copy-
// mutate-write-back with a sha256 check; the M10 EM-5 card's table has them all):
//   - the two timing tests: the send moved back onto the request path (M01);
//   - the row test: a refused hand-over that writes no row (M03), the panic rule
//     applied unconditionally (M06), no row for a panic before the row (M07), the
//     "decided" mark set after the row instead of before it (M08), the drain's
//     stop-sending ignored (M09), the send in flight not cancelled at the cut
//     (M10b), Drain not closing the outbox (M12), recordOutcome bypassing the
//     account budget (M24), the row written on the send's cancelled context (M26),
//     a closing outbox reported as full (M32), the outbox taking grants after the
//     drain began (M33 — a send on the closed channel, the test binary dies), a
//     second worker (X02, with the full-outbox and shutdown tests);
//   - the concurrency and racing tests: recordOutcome bypassing the budget (M34),
//     dispatch's fallback row bypassing it (X12), TryCharge split back into "Allowed,
//     then Charge" (M39; the limiter's own test too, M39h);
//   - the full-outbox test: a blocking hand-over (M02), a silent drop (M03);
//   - the panic test: a recover that re-raises (M04 — the test binary dies) or logs
//     the value (M05), no row for the panic (M07); the second-fault test: the
//     fallback row's write left uncontained (X15 — the test binary dies);
//   - the request-ending test: the job keeping the request's cancellation (M25); the
//     client-leaves test: the fallback row on the request's own context (X10);
//   - the request-id test: the job's context dropping the request's values (X13);
//     without a context, the audit-write-failed line (M40), the account budget line
//     (M41), the SendError branch's failure line (MY07a), the refused hand-over's
//     line (MY07b), the e-mail channel's accepted line (MY07c), the link budget line
//     (MY07d), the plain-error branch's failure line (MY07e), the panic line (MY07f);
//   - the gates test: the request (MY04a), submit (MY04b) or process-log (MY04e)
//     budget split back into "Allowed, then Charge";
//   - the race-with-Drain test: offer reading "closed" without the lock (X14);
//   - the spent-budget test: a row's write not ended by the spent budget (M27), the
//     request's fallback row tied to the outbox's spent budget (M36), Drain's
//     spent-budget branch not stopping the worker (M38); the stopper test: boundedBy
//     without its synchronous check (M37);
//   - the DB reserve test: no send cut before the deadline (M11b);
//   - the shutdown test: the outbox drained after Shutdown returns (M13 — total
//     time), the outbox drained BEFORE Shutdown (X03 — the listener stays open for
//     the drain), a 400 ms wait BEFORE Shutdown (MY10 — the listener bound), a sleep
//     of drainGrace inside the sequence (M14), the drain's stop-sending ignored
//     (M09b), the send in flight not cancelled (M10), no send cut (M11);
//   - the nesting test: ResetDrainGrace above httpShutdownGrace (M15), a write reserve
//     as long as the budget (M16); TestAdminResetConstants_ShippedValuesArePinned: the
//     outbox's size and clocks changed (M28);
//   - the artifact test: run() leaving the channel nil under "email" (M30);
//   - the breaker tests (the M10 EM-7A card): the question not asked, so a refused
//     grant is queued; a line per refused grant; the refused grant answered with a
//     different page; the breaker's row given another reason; the tripped branch
//     slowed past the floor (M29). NOT caught: a slowdown that stays under the floor
//     (M29b) — invisible in the answer, which is what the floor is for.
//   WHAT THEY DO NOT CATCH: a wait added in cmd/tappa's run() OUTSIDE the shutdown
//   function; a wait inside it BEFORE Shutdown shorter than the 300 ms refusal bound
//   (MY09, a 250 ms sleep, stayed green; MY10, 400 ms, went red); an extra wait
//   inside it, after Shutdown has begun, shorter than the 600 ms margin (M14b, a
//   200 ms sleep, stayed green); a wait paid only in a state no test builds; deliver
//   without its `defer cancelSend()` (MY06 stayed green — see COUNTED LIMITS).
//
// PART III — Any form not listed above is the subject of code review — no
// completeness claim.
//
// COUNTED LIMITS: a process that DIES (SIGKILL, OOM, a panic outside a grant)
// loses the outbox: up to resetOutboxSize grants plus the one in flight end with NO
// row (ADR 0022 counted limit 1 — the link may already be in a mailbox) and, since
// M10 EM-7C, with no withdrawal either, so each of those accounts keeps a live link no
// inbox may hold and gets no new one until it expires (adminauth.ResetTTL; the EM-7C
// note's limit 8) — the same for rows a spent drain budget abandons; a send whose
// 250 was written by the relay but not read before a cancellation, and a panic
// between the 250 and the row, record undelivered for a link that went (counted limit
// 15); a panic INSIDE the audit writer leaves 0 or 1 row, unknown (the row's write
// had begun — no second one is attempted); a drain that runs out of its whole budget
// returns while the worker is still writing — its remaining writes then fail on the
// spent context and are logged, not retried; the full-outbox and panic lines are
// written once per grant with no budget of their own, so their volume is bounded
// only by the request budget per address; and deliver's `defer cancelSend()` is
// pinned by no test (MY06): it matters only for a send that PANICS, whose timer
// would otherwise live until resetSendGrace and whose stop hook on sendsStopped
// until the drain — memory held per panic, not a lost or a wrong row. And the
// breaker's question is a SHARPER form of the queue's fate channel below (ADR 0022
// EM-7A note, limit 13 — measured): an attacker who fills the window with his own
// sends, then asks for a target's address and right after for his own the moment the
// one slot he knows is freeing frees, learns from whether HIS link arrives whether
// the target is registered — a precondition of switching the ConfigMap to "email"
// (EM-5B (c)), accepted by the user's decision (ADR 0022 EM-7C note), not closed
// here. (This sentence used to add "and from the breaker's reason in his own
// tenant's trail": no surface lets a tenant read an admin.recovery.* row today —
// the only tenant-facing audit reads are the plaque history and the
// removal-confirmation query — so the trail is not a channel; the arrival is.)
//
// NOT MEASURED, AND OUTSIDE PART I's TIMING CLAIM (ADR 0022 EM-5A note, limits 12
// and 14): this queue is ONE FIFO shared by every requester, so a requester's own
// grant's fate — when it is sent, the time left its e-mail states, whether a full
// queue records it undelivered — depends on how many grants were ahead of it, which
// depends on whether earlier requests resolved to registered addresses; and a
// refused hand-over's fallback row is a synchronous insert paid BEFORE the request's
// floor ends, on registered addresses only, invisible only while it stays under the
// floor (measured against a fake trail, never against Postgres with the queue full).

const (
	// resetOutboxSize is how many grants can wait for the worker (ADR 0022 §6.2).
	//
	// THE LOWER BOUND IS adminauth.ResetWindow (8): one request's full window must
	// never overflow an empty outbox. THE UPPER BOUND IS PULLED BY TWO THINGS: every
	// grant still waiting when the drain's budget is spent costs one undelivered row
	// inside ResetDrainWriteReserve, and every grant waiting when the process DIES ends
	// with no row at all (counted limit 1), so the outbox is a window of loss. 32 is
	// four full windows. One source address can offer at most adminResetRequestLimit
	// x ResetWindow = 160 grants per window, and one worker at the worst send time
	// (resetSendGrace) clears 40 per 10 minutes — so a single address CAN fill it,
	// and a full outbox is a deliberate degradation: the overflow is recorded
	// undelivered and not sent. The drain's write cost of a full outbox is measured
	// at ResetDrainWriteReserve.
	resetOutboxSize = 32

	// resetSendGrace bounds ONE grant's delivery: the whole relay conversation,
	// both attempts and the pause between them. It is internal/mail.DefaultTimeout's
	// number (ADR 0022 §6.3); the relay's own Config.Timeout, when shorter, wins
	// inside Send.
	resetSendGrace = 15 * time.Second

	// resetAuditGrace bounds ONE outcome row's write, SEPARATELY from the send (ADR
	// 0022 §6.3): a send that spends its whole resetSendGrace must not take the row
	// down with it, which is what sharing one context did before EM-5.
	resetAuditGrace = 5 * time.Second

	// ResetDrainGrace is how long the outbox may take to drain at shutdown (ADR 0022
	// §6.6: <= 3 s). It runs AT THE SAME TIME as the HTTP drain, so it must nest
	// inside httpShutdownGrace rather than add to it —
	// TestShutdownBudget_TheResetDrainNestsInsideTheHTTPGrace holds that.
	ResetDrainGrace = 3 * time.Second

	// ResetDrainWriteReserve is the tail of ResetDrainGrace kept for WRITING the rows
	// of what could not be sent: sends stop at ResetDrainGrace - this, and every grant
	// still in flight or waiting is then recorded undelivered without a send, inside
	// the budget (ADR 0022 §6.6). One second is the reserve for a FULL outbox
	// (resetOutboxSize + 1 rows) — the measurement is in
	// TestResetOutboxDB_AFullOutboxFitsTheWriteReserve's log line and in the EM-5
	// card.
	ResetDrainWriteReserve = 1 * time.Second
)

// The fixed reasons an undelivered row carries. Each names WHY the link was not
// handed to the relay, because an investigator reading "undelivered" needs to know
// whether the relay refused, the process was busy, or it was stopping.
const (
	resetReasonSendFailed  = "the recovery link could not be handed to the delivery channel"
	resetReasonOutboxFull  = "the delivery queue was full, so the recovery link was not sent"
	resetReasonOutboxShut  = "the delivery queue was closed for shutdown, so the recovery link was not sent"
	resetReasonDrainEnded  = "the process stopped before the recovery link could be sent"
	resetReasonWorkerFault = "the delivery stopped on an internal fault; the recovery link may not have been sent"
	// resetReasonBreaker (M10 EM-7A): the process-wide sending limit (ADR 0022 §9) was
	// reached when the request asked, so the grant was never queued.
	resetReasonBreaker = "the process-wide sending limit was reached, so the recovery link was not sent"
)

// resetJob is one grant waiting for the worker.
type resetJob struct {
	// base is the request's context WITHOUT its cancellation: the request ends long
	// before the send does, and a visitor closing the tab must not cancel a link
	// that was already minted for somebody else's mailbox. Its VALUES are kept —
	// the request id among them, so the worker's lines correlate with the request.
	base context.Context
	ip   string
	g    adminauth.ResetGrant
}

// resetOutbox is the bounded hand-over between the request and the worker.
type resetOutbox struct {
	jobs chan resetJob

	// mu guards closed and the close of jobs, so an offer can never send on a
	// closed channel.
	mu     sync.Mutex
	closed bool

	// sendsStopped is done when the drain's send phase is over: an in-flight send
	// is cancelled, and every grant after it is recorded without a send.
	sendsStopped context.Context
	stopSending  context.CancelFunc
	// budgetSpent is done when the drain's whole budget is spent: a row still being
	// written is abandoned rather than held past it.
	budgetSpent context.Context
	spendBudget context.CancelFunc

	// done is closed when the worker returns, i.e. when every grant ever accepted
	// has its outcome.
	done chan struct{}
}

func newResetOutbox(size int) *resetOutbox {
	q := &resetOutbox{jobs: make(chan resetJob, size), done: make(chan struct{})}
	q.sendsStopped, q.stopSending = context.WithCancel(context.Background())
	q.budgetSpent, q.spendBudget = context.WithCancel(context.Background())
	return q
}

// offer hands one job over WITHOUT BLOCKING. accepted is false — and the caller
// records the grant undelivered — when the outbox is full or closing; closing says
// which.
func (q *resetOutbox) offer(j resetJob) (accepted, closing bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return false, true
	}
	select {
	case q.jobs <- j:
		return true, false
	default:
		return false, false
	}
}

// close stops accepting. Idempotent: a second Drain must not panic on a closed
// channel.
func (q *resetOutbox) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.closed {
		q.closed = true
		close(q.jobs)
	}
}

// sendContext bounds one grant's send by resetSendGrace and ends it the moment the
// drain stops sending.
func (q *resetOutbox) sendContext(base context.Context) (context.Context, context.CancelFunc) {
	return boundedBy(base, resetSendGrace, q.sendsStopped)
}

// auditContext bounds one outcome row's write by resetAuditGrace and ends it the
// moment the drain's whole budget is spent.
func (q *resetOutbox) auditContext(base context.Context) (context.Context, context.CancelFunc) {
	return boundedBy(base, resetAuditGrace, q.budgetSpent)
}

// boundedBy is base with a timeout of d that also ends when stopper does.
//
// 🔴 AN ALREADY-ENDED stopper ENDS THE CONTEXT HERE, SYNCHRONOUSLY. context.AfterFunc
// runs its function on a goroutine of its own even when the context is already done,
// so without the check a write or a send started AFTER the drain stopped could begin
// before that goroutine ran — measured: a mutation that put a request's fallback row
// on this context stayed green until this line existed.
func boundedBy(base context.Context, d time.Duration, stopper context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(base, d)
	stop := context.AfterFunc(stopper, cancel)
	if stopper.Err() != nil {
		cancel()
	}
	return ctx, func() { stop(); cancel() }
}

// dispatch hands one grant to the outbox, or — when it will not take it — records
// the grant undelivered HERE, before the response, and sends nothing (ADR 0022
// §6.1). Either way the request does not wait for a relay.
//
// 🔴 A GRANT THE PROCESS-WIDE BREAKER WOULD REFUSE NEVER ENTERS THE OUTBOX (M10 EM-7A,
// ADR 0022 EM-7A note, K7A-4): it is recorded undelivered at once, with the outbox's
// own fallback row and the breaker's reason, and nothing is sent. Asked BEFORE the
// offer because an offered grant cannot be taken back. Why not let the worker meet the
// refusal: a queued grant holds one of the outbox's 32 places for nothing while the
// breaker refuses, and its row would come later and from the worker. The response is
// untouched — Request renders the same page after the same floor whatever this
// decides (TestAdminReset_ATrippedBreakerRecordsTheGrantAtOnceAndQueuesNothing).
// NO LOG LINE PER REFUSED GRANT: the breaker writes one when it trips (internal/mail),
// and the row is the per-grant record.
//
// THE FALLBACK ROW IS WRITTEN ON THE REQUEST'S OWN ROW CONTEXT, NOT THE WORKER'S. A
// request still in flight during shutdown runs inside the HTTP drain
// (httpShutdownGrace), which outlasts the outbox's: tying this row to the drain's
// spent budget would drop it while the pool is still open.
//
// ⚠️ SINCE M10 EM-7C THE ROW IS PRECEDED BY THE LINK'S WITHDRAWAL (recordOutcome), ON
// THE SAME REQUEST: every grant refused here — breaker, full outbox, closing outbox —
// costs the request two transactions before its floor ends, on registered addresses
// only; a full adminauth.ResetWindow is eight of each. Measured against real Postgres
// (the real Withdraw and audit recorder): the answer stays inside [floor, floor+50ms]
// — TestResetOutboxDB_TheFallbackWithdrawalsStayUnderTheFloor. It is the shape of the
// fallback row itself (ADR 0022 EM-5A note, limit 14; the EM-7C note's limit 9).
func (h *AdminReset) dispatch(r *http.Request, ip string, g adminauth.ResetGrant) {
	base := context.WithoutCancel(r.Context())
	if h.mail.RefusingResets() {
		h.recordFallback(base, g, resetReasonBreaker)
		return
	}
	accepted, closing := h.outbox.offer(resetJob{base: base, ip: ip, g: g})
	if accepted {
		return
	}
	reason, state := resetReasonOutboxFull, "full"
	if closing {
		reason, state = resetReasonOutboxShut, "closing"
	}
	h.log.WarnContext(base, "panel recovery: the delivery queue did not take the link, so it was not sent",
		"ip", ip, "admin_user_id", g.Issued.Reset.AdminUserID, "reset_id", g.Issued.Reset.ID, "queue", state)
	h.recordFallback(base, g, reason)
}

// recordFallback writes the undelivered row of a grant the request kept from the
// worker, on the request's own row context (dispatch says why), bounded by
// resetAuditGrace.
func (h *AdminReset) recordFallback(base context.Context, g adminauth.ResetGrant, reason string) {
	ctx, cancel := context.WithTimeout(base, resetAuditGrace)
	defer cancel()
	h.recordOutcome(ctx, g, ActionAdminResetUndelivered, "undelivered", reason)
}

// work is the one worker goroutine. It returns when the outbox is closed AND empty.
func (h *AdminReset) work() {
	defer close(h.outbox.done)
	for j := range h.outbox.jobs {
		h.handle(j)
	}
}

// handle runs one grant to its one outcome row.
//
// 🔴 A PANIC ENDS THE GRANT, NEVER THE WORKER (ADR 0022 §6.4). This goroutine is not
// under the router's Recoverer, so a panic here would kill the process and every
// grant in the outbox with it (counted limit 1). The rule is CONDITIONAL: when no
// outcome row had been started, the panic becomes an undelivered row; when one had,
// nothing more is written — a second row would contradict the first in a table
// nothing can delete from. The panic value is never logged (it may carry part of the
// message or the link) and never re-raised: internal/encode re-raises because it
// runs inside a request, under the Recoverer; here re-raising IS the process dying.
func (h *AdminReset) handle(j resetJob) {
	var decided bool
	if !h.contain(j, func() { h.deliverJob(j, &decided) }) || decided {
		return
	}
	// The undelivered row is itself contained: a second fault (the audit writer
	// panicking too) is logged and the worker moves on to the next grant.
	h.contain(j, func() {
		h.recordWorkerOutcome(j.base, j.g, ActionAdminResetUndelivered, "undelivered", resetReasonWorkerFault)
	})
}

// contain runs f and reports whether it PANICKED. The panic is logged as a fixed
// sentence with the grant's ids — its value is not — and swallowed.
func (h *AdminReset) contain(j resetJob, f func()) (panicked bool) {
	defer func() {
		if recover() != nil {
			panicked = true
			h.log.ErrorContext(j.base, "panel recovery: a delivery panicked; the worker recovered and moved on",
				"admin_user_id", j.g.Issued.Reset.AdminUserID, "reset_id", j.g.Issued.Reset.ID)
		}
	}()
	f()
	return false
}

// deliverJob is one grant's turn: the existing deliver while the drain still
// allows sending, an undelivered row without a send once it does not.
func (h *AdminReset) deliverJob(j resetJob, decided *bool) {
	if h.outbox.sendsStopped.Err() != nil {
		*decided = true
		h.recordWorkerOutcome(j.base, j.g, ActionAdminResetUndelivered, "undelivered", resetReasonDrainEnded)
		return
	}
	h.deliver(j.base, j.ip, j.g, decided)
}

// Drain stops the outbox taking grants and waits for the worker to finish what it
// holds, inside ctx's deadline (cmd/tappa passes ResetDrainGrace).
//
// THE BUDGET HAS TWO PHASES (ADR 0022 §6.6). Until ResetDrainWriteReserve before the
// deadline the worker keeps SENDING what is queued; at that point the send in flight
// is cancelled and every grant after it is recorded undelivered without a send, so
// those writes fit inside the budget instead of after it. When the deadline itself
// passes, a row still being written is abandoned and Drain returns an error — the
// process is about to exit, and a drain that outlives its budget would push the
// database pool's close past Kubernetes' kill.
//
// A deployment with no channel has no outbox, and Drain returns at once. A second
// Drain returns as soon as the first one's worker is done.
func (h *AdminReset) Drain(ctx context.Context) error {
	q := h.outbox
	if q == nil {
		return nil
	}
	q.close()
	if deadline, ok := ctx.Deadline(); ok {
		// A deadline nearer than the reserve stops the sends at once (AfterFunc runs
		// a non-positive duration immediately).
		cut := time.AfterFunc(time.Until(deadline)-ResetDrainWriteReserve, q.stopSending)
		defer cut.Stop()
	}
	select {
	case <-q.done:
		return nil
	case <-ctx.Done():
		// The budget is spent: stop sending and end every row still being written,
		// HERE and before returning — not on a context.AfterFunc of ctx, whose stop
		// in a defer can race the cancellation and win, leaving both undone.
		q.stopSending()
		q.spendBudget()
		return fmt.Errorf("handler: the reset outbox did not drain within its budget: %w", ctx.Err())
	}
}

// sendErrorClass reads the only description of a failed send this flow may log: the
// class and the reply code of a *mail.SendError (ADR 0022 §3, §10). ok is false for
// any other error, which then logs only its Go type.
func sendErrorClass(err error) (class string, code int, ok bool) {
	var se *mail.SendError
	if !errors.As(err, &se) {
		return "", 0, false
	}
	return string(se.Class), se.SMTPCode, true
}
