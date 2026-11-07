package handler

import (
	"context"
	"fmt"
	"time"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/db"
)

// THE WITHDRAWALS STILL OWED (T212, the user's choice of 2026-11-07; ADR 0022's EM-7C note, limit 8).
// A link recorded undelivered must be withdrawn, or one live link per account (per identity) answers
// the owner's every request "kept" until adminauth.ResetTTL while no inbox may hold it -- for an
// identity link in every business the person has. When both tries of the withdrawal fail on a
// database that cannot answer now (db.Transient), the withdrawal is not dropped: it waits here, and the
// worker tries it again once the database answers. A stall usually ends within minutes; the lockout
// then ends with it, instead of an hour later.
//
// 🔴 ONLY THE WORKER TRIES, AND ONLY WITHDRAWALS. A request hands an owed withdrawal over and never
// waits for it; nothing here mints, re-sends, writes a row or decides that a link was not delivered.
// The target is the link itself -- the reset row's id inside its tenant, or the identity link's own
// token (adminauth.Withdraw, WithdrawIdentity) -- and both verbs move a link only from live (cancelled_at
// only from NULL). So a late try kills nothing it was not handed: not a newer link of the same owner
// (one can exist only once this one is spent or already retired), and not anybody else's. ADR 0015's
// harm -- somebody's request killing your link -- gets no new road: every link here was minted by a
// request that then failed to deliver it, and the person who asked was never handed it.
//
// 🔴 A SECOND WITHDRAWAL OF THE SAME LINK IS HARMLESS, AND IT HAPPENS: a one-statement autocommitted
// identity_withdraw_reset often takes effect though the client saw an error (T197's audit: 38 of 40).
// The late try then moves nothing and succeeds.
//
// WHO HANDS ONE OVER: the worker, when an outcome's withdrawal has failed its tries
// (adminresetoutcome.go); a request, when the retry queue refused its outcome; and a request whose
// mint failed after it minted, for the links adminauth could not withdraw (adminauth.OwedWithdrawals,
// T210 -- the only connection to that package, an error type, so it does not see this worker).
//
// NO ROW FOR A LATE SUCCESS. On the healthy path the withdrawal has no row of its own either: the
// "undelivered" row (or, for an identity link, the outcome line) is the record that the link was not
// handed over and is withdrawn, and when the link stopped working is on the link itself (cancelled_at).
// A late success writes an INFO line with the ids and how long it was owed. A row would need its own
// budget and its own failure handling in a table nothing can delete from, for a fact the link's row
// already holds; and an identity link has no tenant whose trail could take it (ADR 0031 §5.2).
//
// COUNTED LIMITS. The list is in memory: a process that dies (SIGKILL, OOM) loses it, and its links
// stay live until they expire, the old outcome (ADR 0022 counted limit 1). At the shutdown drain what it
// holds is counted in one ERROR line and dropped -- the drain's budget is for the outbox's rows. A full
// list refuses with an ERROR. A link that expires while it waits is dropped without a try: an expired
// link is dead already. An error that will answer the same again (a privilege, a constraint) is not
// retried. The list is never tried after the drain begins; a try already in flight then is bounded by
// resetOutcomeGrace and the drain's budget, like every write on the worker.

const (
	// resetRecoverySize bounds the withdrawals owed. Its inflow in a stall is what the retry queue
	// hands back with both tries failed, so it is resetRetryQueueSize: one full retry queue lost in one
	// stall fits, which is the 3 s / 16 x 8 case that queue was sized for, stretched past its tries.
	// What pulls it down is the window of loss when the process dies (the same as the queue's) and
	// memory (an entry is a grant without its address, a few hundred bytes). A pass is time-bounded
	// (resetRecoveryPass), so a full list never holds the worker longer than that.
	resetRecoverySize = 256

	// resetRecoveryFirst is the wait before the first late try. The outcome has just spent its own
	// tries (about 7.5 to 12 s, adminresetoutcome.go); half a minute later is the first moment a lock
	// held by a stuck transaction or a restarting server has a fair chance to be gone.
	resetRecoveryFirst = 30 * time.Second

	// resetRecoveryMaxPause caps the doubling between failing passes: 30 s, 1 min, 2 min, 2 min...
	// A stall of minutes therefore ends in a lockout at most two minutes longer than the stall, and a
	// link's whole life (adminauth.ResetTTL, one hour) is about thirty failing passes: never a try
	// forever, because an entry leaves at its link's expiry.
	resetRecoveryMaxPause = 2 * time.Minute

	// resetRecoveryPass bounds one pass: no try begins after it, so a slow but answering database
	// cannot hold the worker (and the sends behind it) for the whole list. A pass also ends at its
	// first transient failure: the database is the common cause, and the rest of the list would only
	// spend a resetOutcomeGrace each to learn the same.
	resetRecoveryPass = time.Second
)

// owedWithdrawal is one link whose withdrawal is still owed.
type owedWithdrawal struct {
	// base carries the originating request's values (its id) for the lines, not its cancellation.
	base context.Context
	// g has no address (owe drops it).
	g         adminauth.ResetGrant
	expires   time.Time
	since     time.Time
	notBefore time.Time
	tries     int
}

// The fates a lost withdrawal's ERROR line names (logWithdrawalLost's "recovery" key).
const (
	recoveryRetrying   = "retrying"    // on the list: a later try may still withdraw it
	recoveryListFull   = "list_full"   // the list refused: the link stays live until it expires
	recoveryClosing    = "closing"     // the process is stopping: the same
	recoveryNotRetried = "not_retried" // the database answered no, or the drain stopped the write
)

// owe hands a failed withdrawal to the list WITHOUT BLOCKING and answers its fate.
func (h *AdminReset) owe(base context.Context, g adminauth.ResetGrant) string {
	if h.outbox == nil {
		return recoveryClosing
	}
	now := time.Now()
	g.Recipient = ""
	expires := g.Issued.Reset.ExpiresAt
	if expires.IsZero() {
		expires = now.Add(adminauth.ResetTTL)
	}
	accepted, closing := h.outbox.offerRecovery(&owedWithdrawal{
		base: base, g: g, expires: expires, since: now, notBefore: now.Add(h.clocks.recoveryFirst),
	})
	switch {
	case accepted:
		return recoveryRetrying
	case closing:
		return recoveryClosing
	}
	return recoveryListFull
}

// oweFailedRequest takes the withdrawals a failed mint could not make (adminauth.OwedWithdrawals): each
// one owed or lost, with its line.
func (h *AdminReset) oweFailedRequest(base context.Context, err error) {
	for _, w := range adminauth.OwedWithdrawals(err) {
		fate := recoveryNotRetried
		if db.Transient(w.Err) {
			fate = h.owe(base, w.Grant)
		}
		h.logWithdrawalLost(base, w.Grant, w.Err, fate)
	}
}

// recoverWithdrawals is one pass over the list, on the worker: due entries in order, until the list
// has none due, the pass's time is spent, or a try fails transiently.
func (h *AdminReset) recoverWithdrawals() {
	q := h.outbox
	end := time.Now().Add(h.clocks.recoveryPass)
	for time.Now().Before(end) {
		w, expired := q.nextOwed(time.Now())
		for _, x := range expired {
			h.log.WarnContext(x.base, "panel recovery: an owed withdrawal was dropped untried, its link has expired",
				append(grantIDs(x.g), "tries", x.tries)...)
		}
		if w == nil {
			return
		}
		var err error
		if h.contain(resetJob{base: w.base, g: w.g}, func() {
			ctx, cancel := h.workerClock().step(w.base)
			defer cancel()
			err = h.withdraw(ctx, w.g)
		}) {
			continue // contain wrote the line; the entry is gone with it
		}
		w.tries++
		switch {
		case err == nil:
			q.owedRecovered(h.clocks.recoveryFirst)
			h.log.InfoContext(w.base, "panel recovery: an owed withdrawal succeeded on a later try",
				append(grantIDs(w.g), "tries", w.tries, "owed_for", time.Since(w.since).Round(time.Second).String())...)
		case db.Transient(err):
			if !q.deferOwed(w, time.Now(), h.clocks.recoveryFirst, h.clocks.recoveryMaxPause) {
				h.logWithdrawalLost(w.base, w.g, err, recoveryClosing)
				return
			}
			// One line per failing pass, not per entry: the pass ends here.
			h.log.WarnContext(w.base, "panel recovery: an owed withdrawal failed again and is tried later",
				append(grantIDs(w.g), "tries", w.tries, "owed", q.owedCount(), "err_type", fmt.Sprintf("%T", err))...)
			return
		default:
			h.logWithdrawalLost(w.base, w.g, err, recoveryNotRetried)
		}
	}
}

// offerRecovery puts one owed withdrawal on the list WITHOUT BLOCKING: false when the list is full or
// closing (closing says which), and the caller logs the link as lost.
func (q *resetOutbox) offerRecovery(w *owedWithdrawal) (accepted, closing bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return false, true
	}
	if len(q.owed) >= resetRecoverySize {
		return false, false
	}
	q.owed = append(q.owed, w)
	select {
	case q.owedKick <- struct{}{}:
	default:
	}
	return true, false
}

// nextOwed takes the first entry due at now off the list, and every expired one with it.
func (q *resetOutbox) nextOwed(now time.Time) (due *owedWithdrawal, expired []*owedWithdrawal) {
	q.mu.Lock()
	defer q.mu.Unlock()
	kept := q.owed[:0]
	for _, w := range q.owed {
		switch {
		case !now.Before(w.expires):
			expired = append(expired, w)
		case due == nil && !now.Before(w.notBefore):
			due = w
		default:
			kept = append(kept, w)
		}
	}
	clear(q.owed[len(kept):])
	q.owed = kept
	return due, expired
}

// deferOwed puts a transiently failed entry back, last, and pushes the whole list back by the pause,
// which doubles up to maxPause. false: the list closed meanwhile, and the entry is lost.
func (q *resetOutbox) deferOwed(w *owedWithdrawal, now time.Time, first, maxPause time.Duration) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return false
	}
	if q.owedPause < first {
		q.owedPause = first
	}
	at := now.Add(q.owedPause)
	if q.owedPause *= 2; q.owedPause > maxPause {
		q.owedPause = maxPause
	}
	q.owed = append(q.owed, w)
	for _, x := range q.owed {
		if x.notBefore.Before(at) {
			x.notBefore = at
		}
	}
	return true
}

// owedRecovered resets the pause after a try that worked.
func (q *resetOutbox) owedRecovered(first time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.owedPause = first
}

// nextRecovery is when the next pass is due: the earliest entry's notBefore. ok is false for an
// empty list.
func (q *resetOutbox) nextRecovery() (at time.Time, ok bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, w := range q.owed {
		if !ok || w.notBefore.Before(at) {
			at, ok = w.notBefore, true
		}
	}
	return at, ok
}

// owedCount is how many withdrawals are owed.
func (q *resetOutbox) owedCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.owed)
}
