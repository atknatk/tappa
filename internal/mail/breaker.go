package mail

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// THE PROCESS-WIDE BREAKER (M10 EM-7A; ADR 0022 §9 and its "EM-7A note").
//
// 🔴 WHY IT EXISTS: every e-mail this process sends — an invitation, a recovery link,
// a "your password was changed" notice — goes out through ONE relay account (SES), and
// that account's reputation (bounce and complaint rates) is shared by every tenant. The
// anonymous recovery form alone can ask for ~960 sends an hour from one source address
// (ADR 0022 §9, B9) and the open signup can grow the invitation path without a lifetime
// ceiling (B17). The breaker is the one ceiling over ALL of them: at most BreakerLimit
// sends are let through in any BreakerWindow, and past it a refusable send is refused
// here, before anything is dialled. It turns a reputation risk into an availability
// one, on purpose: while it refuses, every tenant's recovery and invitation e-mail
// stops (ADR 0022 counted limit 6).
//
// IT IS A DECORATOR AROUND THE ONE TRANSPORT: it is handed the *SMTP (anything with
// Send) and offers the same Send, so a caller that only needs Send cannot tell it is
// there — a refused send reaches it as an ordinary *SendError of ClassBreaker, with no
// reply code. Two more methods exist for the one caller that needs them, the recovery
// flow's e-mail channel (internal/handler/resetmail.go):
//   - SendExempt is COUNTED LIKE EVERY SEND AND NEVER REFUSED. It is for the "your
//     password was changed" notice: the same anonymous traffic that can fill the window
//     must not be able to silence the one message that tells an account holder their
//     account was taken over (ADR 0022 EM-9 note, limit 11). This package does not know
//     what a notice is; the channel chooses the method.
//   - Refusing answers "would Send refuse now?" without sending or recording a send, so
//     the recovery flow can record a grant undelivered at once instead of queueing it
//     (ADR 0022 EM-7A note, decision K7A-4). A true answer IS a refusal — the caller
//     drops the message — so it is counted and may be the one that logs the trip.
//
// THE WINDOW SLIDES, EXACTLY: a refusable send is refused when BreakerLimit sends were
// recorded in the BreakerWindow ending now. It keeps only the most recent BreakerLimit
// send times, because that is enough: at least BreakerLimit sends lie inside the window
// exactly when the BreakerLimit-th most recent one does. A slot frees when that send
// is BreakerWindow old — not when a clock hour turns, so there is no boundary at which
// twice the limit can pass. Exempt sends are recorded too and can push the count past
// the limit; they only ever make the breaker refuse sooner.
//
// WHAT IT COUNTS: every send it LETS THROUGH, at the moment it lets it through, before
// the relay is reached — the decision and the record are one locked step, so N
// concurrent callers see exactly BreakerLimit admissions. An attempt that then fails
// (the transport refusing the address before any dial, a timeout, a 5xx) keeps its
// slot: the breaker counts what it let through, not how it went. A refused send is
// not recorded, so refusals alone never keep it shut.
//
// TWO LOG LINES, AND NEVER ONE PER REFUSAL: a Warn when it refuses for the first time
// (the trip), and an Info when a whole BreakerWindow has passed with no refusal (the
// close), written by the first call after that — carrying only the limit, the window
// and the number refused in between. Never an address, a subject, a body, a tenant or
// anything else of a message: it never reads one. Under a load that stays above the
// limit it trips once and stays tripped, so a load hovering at the limit cannot make it
// write a pair of lines per freed slot.
//
// THE CLAIM, IN THREE PARTS (agent-brief, M10 OP-6/OP-7). Threat model: these pins hold
// against ACCIDENTAL drift in this file, in the channel that chooses its methods and in
// cmd/tappa's wiring; code written on purpose to get past a pin is the subject of code
// review.
//
// PART I — TODAY'S CODE, MEASURED (breaker_test.go, with an injected clock and a
// recording sender, no network):
//   - the 300th send in an hour passes and the 301st is refused as breaker with no
//     code, the wrapped sender never reached; still refused 1 ns before the hour, let
//     through at the hour — TestBreaker_The300thPassesAndThe301stIsRefusedUntilTheHourPasses;
//     the window SLIDES: with 100 sends at t0 and 200 at t0+30m, exactly 100 pass at
//     t0+1h and exactly 200 more at t0+1h30m — TestBreaker_TheWindowSlidesOneSendAtATime;
//   - the shipped numbers are 300 and one hour, as literals —
//     TestBreaker_TheShippedLimitIs300AnHour;
//   - an exempt send is never refused, even past the limit, and is counted: 300 exempt
//     sends shut it for refusable ones, 299 sends and one exempt send do too —
//     TestBreaker_AnExemptSendIsCountedAndNeverRefused;
//   - 1000 concurrent Sends, 20 runs: exactly 300 reach the wrapped sender, every
//     other one is a breaker refusal — TestBreaker_ExactlyTheLimitPassesUnderConcurrency
//     (under -race);
//   - Refusing records nothing (1000 calls leave room for 300 sends), answers true when
//     full and is counted as a refusal — TestBreaker_RefusingRecordsNothingAndIsARefusal;
//   - one Warn line at the first refusal and one Info line after a whole quiet window,
//     whatever the number of refusals in between; each line's keys exactly time, level,
//     msg, limit, window (+ refused on the close line); none of the messages' address,
//     subject, bodies, ref or tenant name in the log —
//     TestBreaker_LogsTheTripOnceAndTheCloseOnce; three hours of load at twice the
//     limit write ONE trip line and, after a quiet hour, one close line —
//     TestBreaker_ASustainedOverloadTripsOnce;
//   - NewBreaker refuses a nil sender; a nil or zero Breaker is not configured —
//     TestNewBreaker_RefusesANilSender.
//
// PART II — NAMED PINS AND EXACTLY WHAT EACH CATCHES (the M10 EM-7A card's mutation
// table): the limit test — a limit of 301 or 299, the comparison off by one, the window
// not sliding (a send never ages out); the slide test — a fixed window that resets
// every hour; the shipped-values test — the constants changed together with the tests'
// loops; the exempt test — an exempt send refused, an exempt send not recorded; the
// concurrency test — the decision and the record split by an unlock, the mutex
// removed; the Refusing test — Refusing recording a send, Refusing not counted as a
// refusal; the log tests — a line per refusal, the trip line dropped, the close line
// dropped, the close written while refusals continue, a message field in a line; the
// class tests in internal/handler — ClassBreaker's value emptied. WHAT THEY DO NOT
// CATCH: a clock that goes BACKWARDS (production uses time.Now's monotonic reading; an
// injected clock that steps back would leave older times ahead of newer ones in the
// ring); a second Breaker built around the same transport somewhere other than run()
// (cmd/tappa's source pin reads run() only).
//
// PART III — Any form not listed above is the subject of code review — no
// completeness claim.
//
// COUNTED LIMITS (ADR 0022's EM-7A note): the window lives in this PROCESS's memory —
// the deployment runs one replica (deploy/k8s/20-app.yaml, replicas: 1), a rolling
// update briefly runs two (maxSurge: 1) and every restart starts an empty window, so
// the ceiling is per process, not per account; it is not a per-recipient ceiling
// (ADR 0022 EM-5A note, limit 13) — for invitations that is RecipientCap
// (recipientcap.go, M10 EM-7C: per business and mailbox, asked before this breaker),
// for recovery links internal/adminauth's one live link per account; exempt sends are
// bounded only by their callers' own caps.

// The shipped ceiling (ADR 0022 §9's starting number, decided in its EM-7A note).
const (
	// BreakerLimit is how many sends the breaker lets through in any BreakerWindow.
	BreakerLimit = 300
	// BreakerWindow is how far back the breaker counts.
	BreakerWindow = time.Hour
)

// sender is what the Breaker needs from what it wraps — *SMTP in production — declared
// here, at its consumer (CLAUDE.md §7).
type sender interface {
	Send(ctx context.Context, m Message) (Receipt, error)
}

// BreakerConfig is what NewBreaker takes beyond the sender it wraps.
type BreakerConfig struct {
	// Now is the clock; nil means time.Now (whose monotonic reading a wall-clock
	// change does not move). Tests inject one so a window passes without waiting.
	Now func() time.Time
	// Log receives the two lines; nil means slog.Default().
	Log *slog.Logger
}

// Breaker is the process-wide send ceiling around one transport. Build it with
// NewBreaker; it is safe for concurrent use, and every caller of the process must
// share the ONE instance (cmd/tappa builds it once — its source pin is
// TestBreakerWiring_TheTransportReachesTheChannelsOnlyThroughOneBreaker).
type Breaker struct {
	next sender
	now  func() time.Time
	log  *slog.Logger

	// mu makes each decision and its record one step.
	mu sync.Mutex
	// stamps holds the times of the most recent len(stamps) recorded sends: the first
	// filled entries until it is full, then a ring whose oldest entry is at head.
	stamps       []time.Time
	head, filled int
	// tripped is true from a refusal until a whole window has passed without one;
	// lastRefusal and refused describe that stretch, for the two lines.
	tripped     bool
	lastRefusal time.Time
	refused     int
}

// NewBreaker wraps next. Its error quotes no value.
func NewBreaker(next sender, c BreakerConfig) (*Breaker, error) {
	if next == nil {
		return nil, errors.New("mail: the breaker needs a sender to wrap")
	}
	b := &Breaker{next: next, now: c.Now, log: c.Log, stamps: make([]time.Time, BreakerLimit)}
	if b.now == nil {
		b.now = time.Now
	}
	if b.log == nil {
		b.log = slog.Default()
	}
	return b, nil
}

// Send lets m through to the wrapped sender, or — when BreakerLimit sends were let
// through in the last BreakerWindow — refuses it with a *SendError of ClassBreaker
// and no reply code, without reaching the wrapped sender or reading m.
func (b *Breaker) Send(ctx context.Context, m Message) (Receipt, error) {
	if b == nil || b.next == nil {
		return Receipt{}, ErrNotConfigured
	}
	if !b.step(useSend) {
		return Receipt{}, &SendError{Class: ClassBreaker}
	}
	return b.next.Send(ctx, m)
}

// SendExempt counts m like every send and lets it through ALWAYS — the one send the
// breaker never refuses (the package comment says which message, and why).
func (b *Breaker) SendExempt(ctx context.Context, m Message) (Receipt, error) {
	if b == nil || b.next == nil {
		return Receipt{}, ErrNotConfigured
	}
	b.step(useExempt)
	return b.next.Send(ctx, m)
}

// Refusing reports whether Send would refuse a message now. It records no send. A
// true answer is counted as a refusal (the caller drops its message instead of
// sending it) and can be the call that logs the trip. A nil Breaker answers false:
// its Send says ErrNotConfigured, which is the truer report of that programmer error.
func (b *Breaker) Refusing() bool {
	if b == nil || b.next == nil {
		return false
	}
	return !b.step(useAsk)
}

// use is what a caller of step is doing.
type use int

const (
	useSend   use = iota // a refusable send: recorded when let through
	useExempt            // a send that is recorded and never refused
	useAsk               // Refusing: nothing is recorded
)

// step is the breaker's one locked decision. It reports whether the caller may go on
// (for useAsk: whether a send would be let through).
//
// The lines are written INSIDE the lock, so a close and the next trip can never be
// logged in the wrong order by two racing callers; there are at most two per window,
// so holding the lock across them costs nothing that matters.
func (b *Breaker) step(u use) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if b.tripped && now.Sub(b.lastRefusal) >= BreakerWindow {
		b.log.Info("mail breaker: no send refused for a whole window, sending normally again",
			"limit", BreakerLimit, "window", BreakerWindow.String(), "refused", b.refused)
		b.tripped, b.refused = false, 0
	}
	full := b.filled == len(b.stamps) && now.Sub(b.stamps[b.head]) < BreakerWindow
	switch {
	case u == useExempt, u == useSend && !full:
		b.record(now)
		return true
	case u == useAsk && !full:
		return true
	}
	b.refused++
	b.lastRefusal = now
	if !b.tripped {
		b.tripped = true
		b.log.Warn("mail breaker: the process-wide sending limit is reached, refusing sends",
			"limit", BreakerLimit, "window", BreakerWindow.String())
	}
	return false
}

// record notes one send at now, dropping the oldest once the ring is full.
func (b *Breaker) record(now time.Time) {
	if b.filled < len(b.stamps) {
		b.stamps[b.filled] = now
		b.filled++
		return
	}
	b.stamps[b.head] = now
	b.head = (b.head + 1) % len(b.stamps)
}
