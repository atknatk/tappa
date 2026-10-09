package mail

import (
	"container/list"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"strings"
	"sync"
	"time"
)

// THE PER-MAILBOX CAP, PARTITIONED BY SENDER SCOPE (M10 EM-7C; ADR 0022 §9 and its
// "EM-7C note").
//
// 🔴 WHY IT EXISTS: the breaker (breaker.go) is ONE ceiling over every send of the
// process, so it does not stop a flood aimed at ONE inbox — measured by the security
// review: plus-tagged employee rows put up to 50 invitations an hour into one mailbox
// from one business (ADR 0022 EM-7B note, limit 12). The user's decision (2026-10-09,
// EM-5B precondition (b)): a ceiling per recipient. So at most RecipientHourLimit sends
// of one SCOPE reach one mailbox in any RecipientHourWindow, and at most
// RecipientDayLimit in any RecipientDayWindow; past either, the send is refused here as
// ClassRecipientCap.
//
// 🔴 THE COUNT IS PER (SCOPE, MAILBOX), NOT PER MAILBOX (EM-7C round 3, orchestrator's
// decision after a security review measured both alternatives). The scope is the
// caller's partition — the invitation route passes the business's id — and it is
// opaque here: this package still knows nothing of businesses or invitations. Why not
// one count per mailbox: a count shared by every sender is a channel between them —
// a business read EXACTLY how many invitations other businesses had sent an address
// (one of theirs made its own fifth press refused), and twenty presses a day from one
// business held a stranger's employer's invitation at 429 for a day. Partitioned, a
// business sees and spends only its own count. What crosses businesses is bounded
// elsewhere: by the open signup's own rate limit (how many businesses one source can
// own) and by the breaker. Recovery links do not come here at all: their bound is the
// one-live-link-per-account rule (internal/adminauth's IssueForEmail), because ANY
// mailbox-keyed refusal of a recovery link is a recovery denial a stranger can cause.
//
// IT IS A DECORATOR AROUND THE ONE BREAKER, which is around the one transport
// (transport → Breaker → RecipientCap → the invitation route; the recovery channel
// takes the breaker itself; cmd/tappa builds each exactly once and its source pins
// hold the shape):
//   - Send: the scope's count, THEN the breaker, in ONE locked step — a send the cap
//     refuses never reaches the breaker (it spends none of its 300), and a send the
//     breaker refuses is not recorded against the mailbox (it spends none of its 5).
//     Both ceilings are exact under concurrency: the cap's lock is held across the
//     breaker's own locked decision, always in that order (the breaker never takes this
//     lock, so the order cannot invert).
//   - Capped: "would a send of this scope to this address be refused now?" — records
//     nothing; a true answer is counted as a refusal (the caller drops the message).
//     The invitation route asks it inside its minting transaction, before anything is
//     committed.
//
// WHAT "ONE MAILBOX" MEANS (normalizeMailbox): the whole address in lower case, with a
// "+tag" dropped from the local part — so Maria@Example.test and maria+2@example.test
// are one inbox, which closes the plus-tag concentration EM-7B's limit 12 measured
// inside one business. Provider-specific aliases (Gmail's dots, one domain aliasing
// another) are NOT folded: a counted limit. The address is never stored: the key is an
// HMAC-SHA256 of the scope and the normalised address under a key drawn from
// crypto/rand when the cap is built, held only in this process's memory — not in the
// environment, not on disk, not in any log.
//
// THE WINDOWS SLIDE, EXACTLY, like the breaker's: each key keeps the times of its
// RecipientDayLimit most recent sends; it is full for the hour when its
// RecipientHourLimit-th most recent send is under RecipientHourWindow old, and full
// for the day when its RecipientDayLimit-th is under RecipientDayWindow old. A slot
// frees the moment that send reaches the window's age.
//
// MEMORY IS BOUNDED (recipientCapEntries): a key whose newest send is a day old is
// forgotten (it can no longer refuse anything), and when the map is full the key sent
// to longest ago is forgotten first — the fail-OPEN direction, httpx.Limiter's
// precedent: refusing new keys would let a flood of fresh addresses stop every
// invitation. The bound is above what the breaker can let through in a day
// (TestRecipientCap_TheBoundOutlastsADayOfTheBreaker), and the cap records only what
// the breaker let through, so nothing a send can reach forgets a key that still has a
// send inside its day.
//
// TWO LOG LINES, NEVER ONE PER REFUSAL (the breaker's rule): a Warn at the first
// refusal of a stretch and an Info when a whole RecipientHourWindow has passed without
// one — limits and a counter only. No address, no digest, no scope: they are never in
// a line.
//
// THE CLAIM, IN THREE PARTS (agent-brief, M10 OP-6/OP-7). Threat model: these pins
// hold against ACCIDENTAL drift — in this file, in the route that calls it and in
// cmd/tappa's wiring; code written on purpose to get past a pin is the subject of
// code review.
//
// PART I — TODAY'S CODE, MEASURED (recipientcap_test.go and
// recipientcap_internal_test.go, injected clock, a counting sender, no network):
//   - the 5th send of a scope to a mailbox in an hour passes, the 6th is refused as
//     recipient_cap with no code and never reaches the breaker's sender; still refused
//     1 ns before the first is an hour old, let through at the hour —
//     TestRecipientCap_TheFifthPassesAndTheSixthIsRefusedUntilTheHourPasses; the 20th
//     in a day passes, the 21st is refused while the hour has room, until the first
//     is a day old — TestRecipientCap_TheTwentiethInADayPassesAndTheTwentyFirstIsRefused;
//   - scopes never share a count, both ways: with one scope full for a mailbox,
//     another scope's question says false and it takes all five of its own —
//     TestRecipientCap_EachScopeHasItsOwnCount;
//   - the numbers are literals — TestRecipientCap_TheShippedLimitsAreFiveAnHourAndTwentyADay;
//   - upper case and a "+tag" are one mailbox, other addresses are not —
//     TestRecipientCap_CaseAndAPlusTagAreOneMailbox;
//   - a cap refusal spends no breaker slot and a breaker refusal spends no cap slot —
//     TestRecipientCap_ARefusalOfOneCeilingSpendsNothingOfTheOther;
//   - 200 concurrent sends of one scope to one mailbox, 20 runs: exactly 5 pass —
//     TestRecipientCap_ExactlyFivePassUnderConcurrency (under -race);
//   - the question records nothing, answers true when full, and is a refusal —
//     TestRecipientCap_CappedRecordsNothingAndIsARefusal;
//   - one Warn at the first refusal, one Info after a quiet hour, keys exactly
//     time, level, msg, hour_limit, day_limit (+ refused), and neither the address
//     (any case), its digest nor the scope in the log —
//     TestRecipientCap_LogsTheFirstRefusalOnceAndTheQuietHourOnce,
//     TestRecipientCap_TheDigestNeverReachesTheLog;
//   - the cap's whole state, walked by reflection, holds the digest and never the
//     address, its normalised form, its local part or the scope —
//     TestRecipientCap_HoldsADigestAndNeverTheAddress; past its bound it forgets the
//     key sent to longest ago and stays at the bound —
//     TestRecipientCap_ForgetsTheStalestMailboxPastItsBound;
//   - NewRecipientCap refuses a breaker NewBreaker did not build; a nil or zero cap
//     sends nothing — TestNewRecipientCap_RefusesAMissingBreaker.
//
// PART II — NAMED PINS AND EXACTLY WHAT EACH CATCHES (the M10 EM-7C card's mutation
// table): a limit of 6 or 4 an hour, 21 a day, the hour comparison off by one, a day
// that never reopens (threshold tests); the scope left out of the key (scope test);
// lower-casing or the "+tag" fold removed (mailbox test); the cap asked after the
// breaker, or a breaker refusal recorded (ceilings test); the lock removed from the
// decision (a DATA RACE under -race); the question recording a send (question test);
// the address kept instead of its digest (state test); the bound removed (bound test);
// a line per refusal, the quiet line dropped, or the digest in a line (log tests).
// WHAT THEY DO NOT CATCH: a clock that goes BACKWARDS (production reads time.Now's
// monotonic clock through the breaker; an injected clock that steps back would leave
// the eviction order unsorted); a second cap built outside run() (cmd/tappa's pins read
// run() only).
//
// PART III — Any form not listed above is the subject of code review — no
// completeness claim.
//
// COUNTED LIMITS (ADR 0022's EM-7C note): the windows live in this PROCESS's memory —
// every restart forgets them and N replicas allow N times the ceiling (replicas: 1 is
// measured in deploy/k8s/20-app.yaml); provider aliases are separate mailboxes; across
// scopes there is no per-mailbox ceiling here — N businesses can each send an inbox
// their five (bounded by how many businesses the open signup lets one source own, and
// by the breaker).

// The shipped ceiling (M10 EM-7C, decision K7C-1; per scope since round 3): five sends
// an hour and twenty a day per (scope, mailbox).
//
// WHY FIVE AN HOUR: an honest business sends a person one invitation and at most a
// "send it again" or two; the business's own per-person limit is three an hour, so five
// to ONE inbox only bites when several of its employee rows share that inbox (the
// plus-tag concentration). WHY TWENTY A DAY: four full hours of the above — it stops a
// business that paces itself under the hourly line.
const (
	// RecipientHourLimit is how many refusable sends of one scope one mailbox gets in
	// any RecipientHourWindow.
	RecipientHourLimit = 5
	// RecipientHourWindow is the short window.
	RecipientHourWindow = time.Hour
	// RecipientDayLimit is how many refusable sends of one scope one mailbox gets in
	// any RecipientDayWindow. It must not be below RecipientHourLimit: a key keeps the
	// times of its RecipientDayLimit most recent sends and reads both windows from them.
	RecipientDayLimit = 20
	// RecipientDayWindow is the long window.
	RecipientDayWindow = 24 * time.Hour
)

// recipientCapEntries bounds how many (scope, mailbox) keys the cap remembers. It must
// stay above what the breaker can let through in a RecipientDayWindow (BreakerLimit per
// BreakerWindow, 7 200 a day as shipped): the cap records only sends the breaker let
// through, so a key that still has a send inside its day is never the one forgotten —
// TestRecipientCap_TheBoundOutlastsADayOfTheBreaker holds the relation and logs the cost
// of a FULL map (every key with RecipientDayLimit sends) — about 5.6 MiB of heap
// (go1.27.1, darwin/amd64), against the 512Mi pod limit.
const recipientCapEntries = 8192

// mailboxKey is the HMAC-SHA256 of a scope and a normalised address: what the cap
// keeps in place of either.
type mailboxKey [sha256.Size]byte

// mailbox is one key's recent sends: a ring of the RecipientDayLimit most recent send
// times, newest at next-1.
type mailbox struct {
	key          mailboxKey
	stamps       [RecipientDayLimit]time.Time
	next, filled int
}

// recent is the n-th most recent send time (1 = the newest); n must not exceed filled.
func (b *mailbox) recent(n int) time.Time {
	return b.stamps[(b.next-n+len(b.stamps))%len(b.stamps)]
}

// full reports whether a refusable send to this key would be refused at now.
func (b *mailbox) full(now time.Time) bool {
	return (b.filled >= RecipientHourLimit && now.Sub(b.recent(RecipientHourLimit)) < RecipientHourWindow) ||
		(b.filled >= RecipientDayLimit && now.Sub(b.recent(RecipientDayLimit)) < RecipientDayWindow)
}

func (b *mailbox) record(now time.Time) {
	b.stamps[b.next] = now
	b.next = (b.next + 1) % len(b.stamps)
	if b.filled < len(b.stamps) {
		b.filled++
	}
}

// RecipientCap is the per-(scope, mailbox) ceiling around the process-wide breaker.
// Build it with NewRecipientCap; it is safe for concurrent use, and the route that
// sends through it must share the ONE instance cmd/tappa builds.
type RecipientCap struct {
	breaker *Breaker
	// hashKey keys the digests. Drawn once per process; never leaves it.
	hashKey []byte

	// mu makes each decision and its record one step; it is held across the
	// breaker's own step (never the other way round).
	mu sync.Mutex
	// boxes finds a key's entry by its digest; order holds the same entries, the one
	// sent to longest ago first (a record moves its entry to the back).
	boxes map[mailboxKey]*list.Element
	order *list.List
	// max is recipientCapEntries; a field only so a test in this package can show
	// the bound's behaviour without filling eight thousand keys.
	max int
	// tripped is true from a refusal until a whole RecipientHourWindow has passed
	// without one; lastRefusal and refused describe that stretch, for the two lines.
	tripped     bool
	lastRefusal time.Time
	refused     int
}

// NewRecipientCap wraps b, which must be a Breaker NewBreaker built. It uses b's clock
// and logger, so a test that injects one injects both. Its error quotes no value.
func NewRecipientCap(b *Breaker) (*RecipientCap, error) {
	if b == nil || b.next == nil {
		return nil, errors.New("mail: the recipient cap needs a breaker built by NewBreaker")
	}
	k := make([]byte, sha256.Size)
	if _, err := rand.Read(k); err != nil {
		return nil, errors.New("mail: the recipient cap could not draw its digest key")
	}
	return &RecipientCap{
		breaker: b,
		hashKey: k,
		boxes:   make(map[mailboxKey]*list.Element),
		order:   list.New(),
		max:     recipientCapEntries,
	}, nil
}

func (c *RecipientCap) configured() bool {
	return c != nil && c.breaker != nil && c.breaker.next != nil && c.boxes != nil
}

// Send lets m through to the transport — or refuses it with a *SendError of
// ClassRecipientCap when scope's count for m.To's mailbox is full, or of ClassBreaker
// when the breaker's window is (the cap is asked first). A refused send reaches
// nothing and is recorded nowhere. m.To is read for its digest; nothing else of m is
// looked at here. scope is the caller's partition (the invitation route: the
// business's id); sends of different scopes never share a count.
func (c *RecipientCap) Send(ctx context.Context, scope string, m Message) (Receipt, error) {
	if !c.configured() {
		return Receipt{}, ErrNotConfigured
	}
	if class := c.admit(c.digest(scope, m.To)); class != "" {
		return Receipt{}, &SendError{Class: class}
	}
	return c.breaker.next.Send(ctx, m)
}

// Capped reports whether a send of scope to `to` would be refused by the cap now. It
// records no send. A true answer is counted as a refusal (the caller drops its message
// instead of sending it) and can be the call that writes the first-refusal line. It
// does not ask the breaker. A nil or zero cap answers false: its Send says
// ErrNotConfigured.
func (c *RecipientCap) Capped(scope, to string) bool {
	if !c.configured() {
		return false
	}
	k := c.digest(scope, to)
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.breaker.now()
	c.settle(now)
	if c.full(k, now) {
		c.refuse(now)
		return true
	}
	return false
}

// admit is the one locked decision of a refusable send over BOTH ceilings: the cap
// first (a refusal here never reaches the breaker), then the breaker's own step (its
// refusal is not recorded here), then the record. It returns the refusing class, or
// "" when the send may go.
func (c *RecipientCap) admit(k mailboxKey) Class {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.breaker.now()
	c.settle(now)
	if c.full(k, now) {
		c.refuse(now)
		return ClassRecipientCap
	}
	if !c.breaker.step(useSend) {
		return ClassBreaker
	}
	c.record(k, now)
	return ""
}

// settle writes the quiet-hour line when it is due and forgets every key whose newest
// send is a whole RecipientDayWindow old. The caller holds mu.
func (c *RecipientCap) settle(now time.Time) {
	if c.tripped && now.Sub(c.lastRefusal) >= RecipientHourWindow {
		c.breaker.log.Info("mail recipient cap: no send refused for a whole hour, every address is sent to normally again",
			"hour_limit", RecipientHourLimit, "day_limit", RecipientDayLimit, "refused", c.refused)
		c.tripped, c.refused = false, 0
	}
	for e := c.order.Front(); e != nil; e = c.order.Front() {
		b := e.Value.(*mailbox)
		if now.Sub(b.recent(1)) < RecipientDayWindow {
			return
		}
		delete(c.boxes, b.key)
		c.order.Remove(e)
	}
}

// full reports whether k is full at now. The caller holds mu.
func (c *RecipientCap) full(k mailboxKey, now time.Time) bool {
	e, ok := c.boxes[k]
	return ok && e.Value.(*mailbox).full(now)
}

// refuse counts one refusal and writes the first-refusal line of a stretch. The caller
// holds mu.
func (c *RecipientCap) refuse(now time.Time) {
	c.refused++
	c.lastRefusal = now
	if !c.tripped {
		c.tripped = true
		c.breaker.log.Warn("mail recipient cap: an address reached its sending limit, refusing sends to it",
			"hour_limit", RecipientHourLimit, "day_limit", RecipientDayLimit)
	}
}

// record notes one send to k at now. A key it has not seen is added at the back; when
// the map is at its bound the key sent to longest ago is forgotten first. The caller
// holds mu.
func (c *RecipientCap) record(k mailboxKey, now time.Time) {
	if e, ok := c.boxes[k]; ok {
		e.Value.(*mailbox).record(now)
		c.order.MoveToBack(e)
		return
	}
	if len(c.boxes) >= c.max {
		if oldest := c.order.Front(); oldest != nil {
			delete(c.boxes, oldest.Value.(*mailbox).key)
			c.order.Remove(oldest)
		}
	}
	b := &mailbox{key: k}
	b.record(now)
	c.boxes[k] = c.order.PushBack(b)
}

// digest is the key of (scope, to's mailbox): HMAC-SHA256 of the scope, a NUL and
// normalizeMailbox(to) under this process's key. The NUL keeps "a"+"b@x" apart from
// "ab"+"@x"; neither a scope the route passes (a UUID) nor an address the relay takes
// can hold one. The normalised address lives only for this call.
func (c *RecipientCap) digest(scope, to string) mailboxKey {
	m := hmac.New(sha256.New, c.hashKey)
	_, _ = m.Write([]byte(scope))                // hash writes never fail (documented)
	_, _ = m.Write([]byte{0})                    // hash writes never fail (documented)
	_, _ = m.Write([]byte(normalizeMailbox(to))) // hash writes never fail (documented)
	var k mailboxKey
	m.Sum(k[:0])
	return k
}

// normalizeMailbox is the cap's idea of one inbox: the whole address in lower case,
// and the local part (before the LAST "@") cut at its first "+". It is a key, not a
// validation: a value the transport would refuse still gets one (and spends a slot,
// like a breaker slot — the attempt is what is counted).
func normalizeMailbox(addr string) string {
	a := strings.ToLower(addr)
	at := strings.LastIndexByte(a, '@')
	if at < 0 {
		return a
	}
	local, domain := a[:at], a[at:]
	if plus := strings.IndexByte(local, '+'); plus >= 0 {
		local = local[:plus]
	}
	return local + domain
}
