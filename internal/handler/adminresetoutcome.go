package handler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
)

// What a recovery link owes the database once its fate is known (T197, ADR 0022's EM-7C
// note, limit 8): its withdrawal, when it was not delivered, and its one outcome row --
// for a per-identity link a log line instead, which costs the database nothing.
//
// 🔴 EACH WRITE HAS ITS OWN CLOCK. They shared one resetAuditGrace, so a database that
// stalled during the withdrawal took the row down with it. Measured with real locks: one
// link, 0 rows and a link live for adminauth.ResetTTL. The row is the §4.6 record and the
// withdrawal is what lets the owner ask again; neither may spend the other's time. It is
// the send-and-row lesson of EM-5 (resetAuditGrace), one step further down.
//
// 🔴 A TRANSIENT FAILURE IS TRIED ONCE MORE, A PERMANENT ONE IS NOT (db.Transient). Both
// writes are safe to send twice, and that is measured, not assumed: a withdrawal moves
// cancelled_at only from NULL, and the row carries an id of its own (audit.Event.ID), so a
// COMMIT whose answer was lost does not double it in a table nothing can delete from. The
// budget is charged once, for the outcome, not per try (admitForAdmin).
//
// 🔴 THE SECOND TRY IS NEVER PAID BY A REQUEST. A request writes a fallback outcome
// BEFORE its floor ends, on registered addresses only, so whatever it spends there is what
// the clock can read (the EM-7C note's limit 9). It therefore has one fallbackBudget for
// all its grants, of which each write may take fallbackStep. What did not finish goes to
// the outbox's worker, for the retry or for its first try when the budget is gone; the
// answer does not wait. In the healthy case nothing changes: the writes fit, and the row
// exists when the response returns. Measured with a full window whose withdrawals stall, the
// whole request timed: 40.5 s before, 1.23 s after, on one run on a loaded machine.
//
// THE WORKER HAS NO RESPONSE TO HOLD, so it retries where it stands, one grant at a time.
// A stalled database slows the queue behind it; nothing waits for the queue but mail.
//
// COUNTED LIMITS. Both tries failing is an ERROR for each write and a row lost. The link is
// live, and the owner's requests are answered "kept", until the list of withdrawals owed makes
// the withdrawal once the database answers (T212, adminresetrecovery.go); until ResetTTL only
// when that list refuses it or is lost. The link is usually in no inbox; after an uncertain send (the relay took it and the answer
// was lost) it can be live and spendable in its owner's inbox, and only theirs, with no row
// saying so -- the real cost is the trail, and availability
// (TestResetOutcomeDB_AnUncertainSendCanLeaveASpendableLinkInTheOwnersBox). So is a full or
// closed retry queue, a spent drain budget and a process that dies, which also loses what the
// retry queue holds, like the outbox itself (counted limit 1). The stall a write survives is
// bounded by its clocks: on the worker resetOutcomeGrace + resetRetryPause + resetOutcomeGrace,
// about 12 s, for a write the request never tried; for one it tried, resetFallbackStep +
// resetRetryPause + resetOutcomeGrace, about 7.5 s (an 8 s black hole lost one in an audit).
// It was 5 s for both writes.
//
// THE KEPT ROW IS HERE TOO (T211, recordKept): an outcome with no withdrawal and one row, on a
// request clock that ends with the floor, the rest on the worker. It had a resetAuditGrace of its
// own per account and no second try: 1/2/8 accounts held the answer 5.01/10.01/40.05 s with the
// row stalled, and every row was lost.

const (
	// resetFallbackBudget is everything ONE REQUEST may spend on its fallback writes,
	// however many grants it minted. A second is about twice the slowest full-window answer
	// on record for CI (540 ms, floor included: backlog T127), so the row keeps existing when
	// the response returns. It is deliberately not tied to resetRequestFloor: a budget under
	// it (200 ms and 100 ms worked in an audit) would move the row off the response on any slow
	// day. It is the one tail a stall can still add to a registered address's answer, and only
	// on this rare arm, whatever the number of accounts: the kept rows stop at the floor
	// (recordKept, T211) and the mint's lock waits are bounded inside it
	// (adminauth.ResetMintLockWait, T219).
	resetFallbackBudget = 1 * time.Second

	// resetFallbackStep is one write's share of it: half, so a stalled withdrawal leaves
	// the row its turn. That is the whole of the first grant in a stall; the others go
	// straight to the worker.
	resetFallbackStep = 500 * time.Millisecond

	// resetOutcomeGrace bounds one write on the worker, the withdrawal's and the row's
	// separately. It is resetAuditGrace's number and its reason.
	resetOutcomeGrace = resetAuditGrace

	// resetRetryPause separates a failure from the second try: a refused connection fails
	// in a millisecond, and a retry right behind it would ask a database that is still
	// restarting. The drain cuts it short (waitUntil).
	resetRetryPause = 2 * time.Second

	// resetOutcomeTries counts every try of one write, wherever it runs.
	resetOutcomeTries = 2

	// resetRetryQueueSize bounds the outcomes waiting for the worker. A request whose budget
	// ran out sends every write here untried, so a short stall fills it a full window per
	// request. At 32, an audit measured a regression against the code before this queue: a
	// 3 s stall, 16 requests of 8 links, 33 rows and 95 links live and 95 writes lost, where
	// that code gave 128 rows and 0 live (holding each request far longer). At 256 the same run
	// gave 128 rows, 0 live, 0 lost, and at most 1.22 s per request. The upper bound is what
	// resetOutboxSize's is: a window of loss if the process dies, and more than the drain can
	// write (TestResetOutboxDB_AFullRetryQueueIsAccountedForAtTheDrain). Past it each owed write
	// is abandoned with its ERROR.
	resetRetryQueueSize = 256

	// resetRetryKeptShare is how much of that queue kept rows may hold (T211's security review).
	// Kept rows joined the queue after 256 was measured, and a stall on a busy deployment makes
	// many of them -- one per account per request while a link lives -- so without a share they
	// could fill it and push a later undelivered outcome out: a row lost and a withdrawal sent to
	// the slower list. The other half is the undelivered outcomes' alone, which is the 128 a 3 s
	// stall of 16 full windows needed (above), so that measurement still fits whatever the kept
	// rows do (TestResetOutcomeDB_KeptRowsCannotCrowdOutAnUndeliveredOutcome). A kept row past its
	// share is abandoned with its ERROR: the trail of a request that changed nothing, not of a link.
	resetRetryKeptShare = resetRetryQueueSize / 2
)

// resetOutcomeClocks are the constants above and adminresetrecovery.go's, as fields so a test can
// shorten them.
type resetOutcomeClocks struct {
	fallbackBudget, fallbackStep, grace, retryPause time.Duration
	recoveryFirst, recoveryMaxPause, recoveryPass   time.Duration
}

func shippedOutcomeClocks() resetOutcomeClocks {
	return resetOutcomeClocks{
		fallbackBudget:   resetFallbackBudget,
		fallbackStep:     resetFallbackStep,
		grace:            resetOutcomeGrace,
		retryPause:       resetRetryPause,
		recoveryFirst:    resetRecoveryFirst,
		recoveryMaxPause: resetRecoveryMaxPause,
		recoveryPass:     resetRecoveryPass,
	}
}

// outcomeWrite is one write's progress.
type outcomeWrite struct {
	owed  bool
	tries int
	last  error
}

// note takes one try's result and answers whether the write has now FAILED for good: an
// error with no try left, or one a second try cannot help. A transient error with a try
// left leaves it owed and answers false.
func (w *outcomeWrite) note(err error) (failed bool) {
	w.tries++
	w.last = err
	if err != nil && w.tries < resetOutcomeTries && db.Transient(err) {
		return false
	}
	w.owed = false
	return err != nil
}

// resetOutcome is one grant's decided fate and what it still owes.
type resetOutcome struct {
	base context.Context
	g    adminauth.ResetGrant

	action, outcome, reason string

	withdrawal outcomeWrite
	row        outcomeWrite
	// admitted: the per-account budget was charged and the line or the row decided. It
	// happens once, whatever the number of tries.
	admitted bool
	event    audit.Event

	notBefore time.Time
}

func newResetOutcome(base context.Context, g adminauth.ResetGrant, action, outcome, reason string) *resetOutcome {
	o := &resetOutcome{base: base, g: g, action: action, outcome: outcome, reason: reason}
	// 🔴 AN UNDELIVERED LINK IS WITHDRAWN FIRST (M10 EM-7C), and before the budget is
	// asked, so a row the budget suppresses does not keep the link alive. One live link
	// per account or identity means a request mints nothing while a live link exists; a
	// live link no inbox holds would therefore turn one failed send into ResetTTL of
	// silence for the owner, their own requests included. Withdrawn, the next request
	// mints at once. A send that timed out may still have reached the mailbox: that link
	// dies too, and asking again gets a fresh one -- fail-closed, never a window with no
	// way to get a link.
	o.withdrawal.owed = action == ActionAdminResetUndelivered
	return o
}

func (o *resetOutcome) owed() bool { return o.withdrawal.owed || o.row.owed }

// outcomeClock is where a try's time comes from: the request's or the worker's.
type outcomeClock struct {
	step func(context.Context) (context.Context, context.CancelFunc)
	// open answers whether a try may begin at all. A try the clock refuses does not count
	// against the write: it never reached the database.
	open func() bool
}

// fallbackClock is the request's: resetFallbackStep per write, inside the one
// resetFallbackBudget of the request. The context descends from base (the request's,
// without its cancellation), not from the drain's spent budget -- dispatch says why.
func (h *AdminReset) fallbackClock(until time.Time) outcomeClock {
	return outcomeClock{
		step: func(base context.Context) (context.Context, context.CancelFunc) {
			d := time.Now().Add(h.clocks.fallbackStep)
			if until.Before(d) {
				d = until
			}
			return context.WithDeadline(base, d)
		},
		open: func() bool { return time.Now().Before(until) },
	}
}

// workerClock is the worker's: resetOutcomeGrace per write, ended the moment the
// shutdown drain's whole budget is spent (the process is about to close its pool).
func (h *AdminReset) workerClock() outcomeClock {
	return outcomeClock{
		step: func(base context.Context) (context.Context, context.CancelFunc) {
			return boundedBy(base, h.clocks.grace, h.outbox.budgetSpent)
		},
		open: func() bool { return true },
	}
}

// attemptOutcome tries what the outcome still owes, each write on a context of its own.
func (h *AdminReset) attemptOutcome(o *resetOutcome, clock outcomeClock) {
	if o.withdrawal.owed && clock.open() {
		ctx, cancel := clock.step(o.base)
		err := h.withdraw(ctx, o.g)
		cancel()
		if o.withdrawal.note(err) {
			h.loseWithdrawal(o, db.Transient(err))
		} else if err != nil {
			h.logWillRetry(o, "withdrawal", err)
		}
	}
	h.admitOutcome(o)
	if o.row.owed && clock.open() {
		ctx, cancel := clock.step(o.base)
		_, err := h.audit.Record(ctx, o.event)
		cancel()
		if o.row.note(err) {
			h.logRowLost(o)
		} else if err != nil {
			h.logWillRetry(o, "row", err)
		}
	}
}

// withdraw retires a link recorded undelivered. Both verbs move a link only from live, and
// a link already gone is not an error, so a second try is safe whatever the first did.
func (h *AdminReset) withdraw(ctx context.Context, g adminauth.ResetGrant) error {
	if g.IdentityID != uuid.Nil {
		return h.resets.WithdrawIdentity(ctx, g.Issued.Token)
	}
	return h.resets.Withdraw(ctx, g.Issued.Reset.TenantID, g.Issued.Reset.ID)
}

// admitOutcome charges the budget and decides the trail, once.
//
// A PER-IDENTITY LINK HAS NO ROW (M14-14). ADR 0031 §5.2 (identity_open_reset /
// identity_withdraw_reset): the identity has no tenant, so there is no business whose
// trail could take the row without naming the person's other businesses to it; the
// identity's own trail is the reset_requested event the opening wrote. So ONE log line
// through the per-identity budget, with ids and constant-valued keys only (§7, ADR 0031
// §5.2's M14-14 note lists them): the reset row's id, the identity's id, the outcome and a
// fixed reason -- never the address, the link or anything derived from them.
func (h *AdminReset) admitOutcome(o *resetOutcome) {
	if o.admitted {
		return
	}
	o.admitted = true
	g := o.g
	if g.IdentityID != uuid.Nil {
		if within, n := h.accountLimiter.TryCharge(identityResetBudgetKey(g.IdentityID)); !within {
			if h.accountLimiter.FirstOverLimit(n) {
				h.log.WarnContext(o.base, "panel recovery rate limited", "scope", "identity", "identity_id", g.IdentityID)
			}
			return
		}
		h.log.InfoContext(o.base, "panel recovery: identity link outcome",
			"identity_id", g.IdentityID, "reset_id", g.Issued.Reset.ID, "outcome", o.outcome, "reason", o.reason)
		return
	}
	e, ok := h.admitForAdmin(o.base, g.Issued.Reset.TenantID, g.Issued.Reset.AdminUserID, audit.Event{
		TenantID: g.Issued.Reset.TenantID,
		ActorID:  ptr(g.Issued.Reset.AdminUserID),
		Action:   o.action,
		Target:   g.Issued.Reset.AdminUserID.String(),
		Detail: adminResetDetail{
			Outcome: o.outcome,
			Reason:  o.reason,
			ResetID: g.Issued.Reset.ID.String(),
			// 🔴 THE RETIREMENT COUNT IS IN THE ROW BECAUSE IT IS THE ONLY PLACE THE
			// ACCEPTED RISK BECOMES VISIBLE. adminauth.Reset.RetiredCount is "how many
			// previously live links this request killed", and ADR 0015's harm (a) is
			// precisely somebody else's request killing yours. A takeover investigation
			// reading "requested, retired 1" one minute after "requested, retired 0"
			// is reading the attack; without the number it reads two ordinary requests.
			RetiredCount: g.Issued.Reset.RetiredCount,
			ExpiresAt:    g.Issued.Reset.ExpiresAt.UTC().Format(time.RFC3339),
		},
	})
	if ok {
		// The id is ours from here on, so both tries are one row (audit.Event.ID).
		e.ID = uuid.New()
		o.event, o.row.owed = e, true
	}
}

// keptOutcome is the kept row of an account whose live link made a new one unnecessary (T211,
// recordKept), charged now. The live link travels as a grant with no token and no address, because
// a grant is what the log lines read their ids from (grantIDs) -- and nothing else is read: a kept
// outcome owes no withdrawal (newResetOutcome sets one only for an undelivered link), so the owner's
// live link is never touched (TestResetOutcome_AKeptRowRetriedLaterLeavesTheLiveLinkAlone).
func (h *AdminReset) keptOutcome(base context.Context, k adminauth.Reset) *resetOutcome {
	o := &resetOutcome{
		base:   base,
		g:      adminauth.ResetGrant{Issued: adminauth.IssuedReset{Reset: k}},
		action: ActionAdminResetKept, outcome: "kept", reason: resetReasonLiveLink,
		admitted: true,
	}
	e, ok := h.admitForAdmin(base, k.TenantID, k.AdminUserID, audit.Event{
		TenantID: k.TenantID,
		ActorID:  ptr(k.AdminUserID),
		Action:   ActionAdminResetKept,
		Target:   k.AdminUserID.String(),
		Detail: adminResetDetail{
			Outcome:   "kept",
			Reason:    resetReasonLiveLink,
			ResetID:   k.ID.String(),
			ExpiresAt: k.ExpiresAt.UTC().Format(time.RFC3339),
		},
	})
	if ok {
		// admitOutcome's rule: the id is ours, so both tries are one row.
		e.ID = uuid.New()
		o.event, o.row.owed = e, true
	}
	return o
}

// recordFallback finishes as much of a request-kept grant's outcome as its clock allows,
// and leaves the rest to the worker (this file's header).
func (h *AdminReset) recordFallback(base context.Context, until time.Time, g adminauth.ResetGrant, reason string) {
	h.finishOnRequest(newResetOutcome(base, g, ActionAdminResetUndelivered, "undelivered", reason), until)
}

// finishOnRequest tries what o owes on the request's clock (until) and hands the rest to the worker
// without waiting for it; what the worker will not take is logged lost.
func (h *AdminReset) finishOnRequest(o *resetOutcome, until time.Time) {
	h.attemptOutcome(o, h.fallbackClock(until))
	if !o.owed() {
		return
	}
	o.notBefore = time.Now().Add(h.clocks.retryPause)
	// Neither remaining write needs the address, and this waits in memory for a while.
	o.g.Recipient = ""
	if h.outbox.offerRetry(o) {
		return
	}
	o.withdrawal.last = errors.Join(o.withdrawal.last, errResetRetryRefused)
	o.row.last = errors.Join(o.row.last, errResetRetryRefused)
	h.abandonOutcome(o)
}

// errResetRetryRefused is what an outcome's lost write says when the worker would not take it.
var errResetRetryRefused = errors.New("handler: the reset retry queue did not take the outcome")

// recordWorkerOutcome is the worker's whole try of an outcome: its own clock, the retry
// where it stands.
func (h *AdminReset) recordWorkerOutcome(base context.Context, g adminauth.ResetGrant, action, outcome, reason string) {
	h.settleOutcome(newResetOutcome(base, g, action, outcome, reason))
}

// settleOutcome runs on the worker until the outcome owes nothing. It ends because every
// pass tries every owed write (workerClock is always open) and a write has
// resetOutcomeTries.
func (h *AdminReset) settleOutcome(o *resetOutcome) {
	clock := h.workerClock()
	for {
		h.outbox.waitUntil(o.notBefore)
		h.attemptOutcome(o, clock)
		if !o.owed() {
			return
		}
		o.notBefore = time.Now().Add(h.clocks.retryPause)
	}
}

// abandonOutcome logs whatever the outcome still owes as lost. An owed withdrawal was never refused
// by the database (note would have ended it), so it goes to the list of withdrawals owed.
func (h *AdminReset) abandonOutcome(o *resetOutcome) {
	if o.withdrawal.owed {
		o.withdrawal.owed = false
		h.loseWithdrawal(o, true)
	}
	if o.row.owed {
		o.row.owed = false
		h.logRowLost(o)
	}
}

// offerRetry hands an outcome to the worker WITHOUT BLOCKING, for offer's reason: false
// means full or closing, and the caller logs what is lost.
func (q *resetOutbox) offerRetry(o *resetOutcome) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return false
	}
	// Under q.mu, so no other offer moves len between the check and the send; the worker only
	// takes, which can make the check stricter than it needs to be, never looser.
	if o.action == ActionAdminResetKept && len(q.retries) >= resetRetryKeptShare {
		return false
	}
	select {
	case q.retries <- o:
		return true
	default:
		return false
	}
}

// waitUntil waits for t, unless the drain has stopped the sends: from then on what is
// left of the budget is for writing, not for waiting.
func (q *resetOutbox) waitUntil(t time.Time) {
	d := time.Until(t)
	if d <= 0 {
		return
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-q.sendsStopped.Done():
	}
}

// The three lines a write's failure can write. Ids only: an error's text does not
// reach the first two, its Go type does (§7; deliver says why).

func (h *AdminReset) logWillRetry(o *resetOutcome, write string, err error) {
	h.log.WarnContext(o.base, "panel recovery: writing a link's outcome failed and is tried once more",
		append(grantIDs(o.g), "write", write, "err_type", fmt.Sprintf("%T", err))...)
}

// loseWithdrawal is a withdrawal that failed its tries: handed to the list of withdrawals owed when a
// later try can help (adminresetrecovery.go), and logged with its fate either way.
func (h *AdminReset) loseWithdrawal(o *resetOutcome, retryable bool) {
	fate := RecoveryNotRetried
	if retryable {
		fate = h.owe(o.base, o.g)
	}
	h.logWithdrawalLost(o.base, o.g, o.withdrawal.last, fate)
}

// logWithdrawalLost is an ERROR whatever the fate: the link is live NOW and its owner cannot ask for a
// new one until it is withdrawn or expires. recovery says which of the two to expect (T212).
func (h *AdminReset) logWithdrawalLost(base context.Context, g adminauth.ResetGrant, err error, fate string) {
	msg := "panel recovery: withdrawing an undelivered link failed"
	if g.IdentityID != uuid.Nil {
		msg = "panel recovery: withdrawing an undelivered identity link failed"
	}
	h.log.ErrorContext(base, msg, append(grantIDs(g), "err_type", fmt.Sprintf("%T", err), LogRecovery, fate)...)
}

// logRowLost is record's line for record's reason: a trail that cannot be written is an
// ERROR, because it means §4.6 is not being kept.
func (h *AdminReset) logRowLost(o *resetOutcome) {
	h.log.ErrorContext(o.base, audit.EventWriteFailed, "action", o.event.Action, "err", o.row.last)
}
