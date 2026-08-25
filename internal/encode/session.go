// Package encode is the SERVER half of ADR 0017's plaque personalisation relay:
// it holds the live EV2 session, owns every byte of key material that session
// needs, and drives ADR 0017 §5.1's numbered steps one APDU at a time.
//
// The phone is a pipe. It pushes the C-APDU this package returns at the chip and
// posts the R-APDU back (ADR 0017 §2.1). Nothing here trusts it: ADR 0017 §2.2
// says to assume the relay is hostile, and driver.go's step table says, per gate,
// exactly which lies that assumption does and does not catch.
//
// WHAT THIS PACKAGE IS NOT, TODAY:
//
//   - There is NO HTTP here. The three ports below are still declared HERE because
//     this package is their CONSUMER (CLAUDE.md §7), but two of them now have
//     shipped implementations in this same package — rows.go (Postgres + the audit
//     trail) and wrapper.go (sun.Wrap under the tag KEK) — and the third, Clock,
//     has had one since B2c-1 (systemClock, below).
//
//     ⚠️ "NOTHING IMPORTS THIS PACKAGE YET" WAS TRUE UNTIL 2026-08-24 AND IS NOW
//     FALSE — FAZ B2c-2b wired it. The command is kept because the shape of it was
//     the lesson (audit, 2026-08-24): the first version was
//     `grep -rl "internal/encode" --include=*.go . | grep -v "^./internal/encode/"`
//     and had two faults, either alone fatal to a published measurement — under zsh
//     the unquoted --include=*.go is GLOBBED and the command dies with "no matches
//     found", and where it does run, grep -rl does not print a "./" prefix, so the
//     filter removes nothing. The command that reproduces today's claim:
//
//     grep -rn "atknatk/tappa/internal/encode" --include='*.go' . | grep -v "internal/encode/"
//
//     which returns FOUR lines on this tree — internal/handler/plaqueencode.go (the
//     relay endpoint), cmd/tappa/main.go (its wiring), and two test files:
//     internal/handler/plaqueencode_test.go and cmd/tappa/shutdownbudget_test.go.
//     Test lines are counted rather than filtered away: a published measurement has
//     to be reproducible verbatim.
//
//     ⚠️ THIS NUMBER HAS NOW BEEN WRONG TWICE, IN OPPOSITE ROUNDS. It said "two"
//     (test file uncounted), was corrected to "three", and went stale the same day a
//     LATER round added cmd/tappa/shutdownbudget_test.go — which imports this package
//     to read DefaultCloseGrace. A count published beside the command that produces it
//     has to be re-run when anything imports the package, and twice it was not.
//
//   - THE AUTHORISATION GATE IS NOT HERE, AND THAT IS STILL TRUE OF THIS PACKAGE.
//     ADR 0017 §6 md. 10 ("who may encode for which tenant") is answered ONE LAYER
//     UP, in internal/handler/plaqueencode.go, where a request has an identity to
//     answer it with: the tenant comes off the resolved panel session and never off
//     the request body. Nothing in THIS package checks anything — the actor string
//     below bounds EXPOSURE, it does not grant anything, and Begin still writes
//     wherever it is told. A second caller that skipped the handler would skip the
//     gate, which is why the gate is pinned at the one call site rather than
//     described here.
//
//   - NO CHIP HAS BEEN ENCODED (ADR 0017 §6 md. 1). Everything below is measured
//     against documents and a test double, and the test double is written in this
//     repository by the same hand as the code — chip_test.go says in its own
//     header what that can and cannot prove.
package encode

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/atknatk/tappa/internal/sun"
	"github.com/google/uuid"
)

// --- Ports: the outside world, declared by its consumer ------------------------

// Clock is this package's whole view of time.
//
// 🔴 IT IS AN INTERFACE FOR A TEST REASON AND THE TEST REASON IS A CORRECTNESS
// REASON. A TTL of ninety seconds cannot be exercised by a test that sleeps: the
// suite would either take minutes or assert on a race. Both alternatives end the
// same way — the sweeper stops being tested, and the sweeper is the only thing
// that wipes an ABANDONED session's plain plaque key (ADR 0017 §6 md. 7, which
// rejects "the process will exit eventually" as an answer). NewTicker is part of
// the interface for the same reason: a fake clock can fire the sweeper's tick
// synchronously, so "the goroutine really sweeps" is an assertion rather than a
// hope.
type Clock interface {
	Now() time.Time
	// NewTicker returns a channel that fires roughly every d and a function that
	// releases it. The contract matches time.NewTicker's: stop must be safe to
	// call exactly once, and it does not close the channel.
	NewTicker(d time.Duration) (c <-chan time.Time, stop func())
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) NewTicker(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTicker(d)
	return t.C, t.Stop
}

// Wrapper seals a plain plaque key for storage — sun.Wrap under the tag KEK
// (ADR 0003 md. 4).
//
// It is injected rather than called directly because the KEK lives in
// internal/config, and a package that reached for it would have to be constructed
// with the process's secrets to be tested at all. The consumer-side shape also
// keeps this package honest about what leaves it: the ONLY key-shaped value that
// crosses this interface in either direction is the 44-byte wrapped blob going
// out to Rows (ADR 0017 §3, "kalıcılaşan tek anahtar-benzeri şey sarmalıdır").
//
// ✅ THE PRODUCTION IMPLEMENTATION IS KEKWrapper (wrapper.go), shipped in FAZ
// B2c-2a. It is a two-line adapter over sun.Wrap, which is the point: the
// cryptography stays in internal/sun (CLAUDE.md §4.7) and this package only names
// what it needs.
type Wrapper interface {
	// WrapKey seals plainKey against the RAW 7-byte uid as AAD and returns the
	// 44 bytes tags.aes_key_ref holds. It must not retain plainKey.
	WrapKey(uid []byte, plainKey []byte) ([]byte, error)
}

// Rows is the persistence port — ADR 0017 §5.2 (the row is written before the
// chip's first irreversible command) and §3.1 (it is written by tappa_app, so
// this is an ordinary application-role INSERT).
//
// ✅ THE PRODUCTION IMPLEMENTATION IS DBRows (rows.go), shipped in FAZ B2c-2a.
//
// 🔴 tenantID IS AN EXPLICIT PARAMETER ON BOTH METHODS, AND THAT IS THE ROUND'S
// BLOCKING CONDITION MADE STRUCTURAL (the M8-05 card's hand-over list, item 4).
// The alternative — leaving the tenant implicit in whatever the connection's
// SET LOCAL app.tenant_id happens to say — was refused for a measured reason and
// not a stylistic one: tags.tenant_id is `uuid NOT NULL REFERENCES tenants (id)`
// with NO DEFAULT (migration 00004), so a statement that names no tenant does not
// "land in the session's tenant", it FAILS with a not-null violation. CLAUDE.md
// §4.5 asks for a BELT (an explicit tenant predicate) beside RLS's BRACES (the
// policy's WITH CHECK), and an interface that does not carry the tenant leaves the
// belt entirely to the implementer's memory. Carried here, omitting it is a
// COMPILE ERROR.
//
// ⚠️ WHAT THAT DOES NOT BUY, said in the same breath as the claim: it forces the
// caller to name A tenant, never the RIGHT one. Nothing in this package decides
// which tenant an operator may encode for. ⚠️ THAT GATE — ADR 0017 §6 md. 10 — IS
// CLOSED AS OF FAZ B2c-2b, ONE LAYER UP, and this paragraph said "still open" for a
// round after it shut. It belongs with the endpoint, exactly as predicted:
// internal/handler derives the tenant from the resolved panel session. What is still
// true of THIS package is the first sentence — nothing here decides anything, so a
// second caller that skipped the handler would skip the gate.
// What refuses a WRONG tenant at the database is unchanged: the tags policy carries
// WITH CHECK as well as USING (00004) and tappa_app is NOBYPASSRLS, so a row stamped
// with a tenant other than the transaction's is refused. Against a caller who names a
// tenant that is genuinely not theirs, both of those are silent — which is why the
// gate had to exist somewhere, and now does.
type Rows interface {
	// InsertUnassigned writes the tags row: uidHex canonical upper-case hex
	// (migration 00013's CHECK), the wrapped key, status 'unassigned', no
	// location. It must fail rather than overwrite if the UID already exists —
	// tags.uid is a PRIMARY KEY and a second row for one chip is not a state this
	// flow can produce (ADR 0017 §6 md. 12).
	//
	// 🔴 IT ALSO OWES A TRAIL ENTRY, AND IT MUST SHARE THE ROW'S FATE. ADR 0017
	// §5.2 says the round itself belongs in audit_log and §6 md. 8 left the event
	// undefined; FAZ B2c-2a defines it as ActionPlaqueLoaded, written in the SAME
	// transaction as the INSERT. The reason is audit.RecordTx's own documented
	// rule — "use it when the event is only true if the surrounding change is
	// true". An entry written in its own transaction could survive a failed
	// INSERT and would describe a plaque that does not exist.
	//
	// actor is the operator label from Begin. ⚠️ IT IS NOT AN IDENTITY (see Begin),
	// so an implementation must NOT put it in audit_log.actor_id, which is read on
	// the panel as "who did this".
	//
	// ⚠️ AND THE HONEST HISTORY OF THIS PARAGRAPH, because it is the more useful
	// record: a security audit raised the missing tenant, the round REPORTED it
	// closed, and it was not — the batched edit that was supposed to write it
	// aborted before writing. A fourth audit caught it with one command
	// (`grep -c tenant_id internal/encode/session.go` returned 1, and that one hit
	// was about md. 10). That is why a closure claim in this package has to carry
	// the command that proves it, and why the closure this time is a SIGNATURE
	// rather than a sentence: a signature cannot be reported closed while absent.
	InsertUnassigned(ctx context.Context, tenantID, adminID uuid.UUID, uidHex string, wrappedKey []byte, actor string) error

	// MarkEncoded records that the chip completed the round — ADR 0017 §5.1
	// step 9, "satırı 'encode edildi' olarak işaretle".
	//
	// ✅ THE SCHEMA QUESTION THIS COMMENT USED TO STATE IS CLOSED (FAZ B2c-2a).
	// It said `tags` HAS NO SUCH COLUMN, which was true against 00004 + 00013:
	// there was no value to move, because status must STAY 'unassigned' (writing
	// 'active' would show a boxed plaque as in service, and would additionally
	// violate 00013's tags_active_requires_location). Migration 00022 adds
	// `encoded_at timestamptz`, nullable and write-once, and the mark is that
	// column. The distinction it buys is the one §5.2 creates by writing the row
	// BEFORE the chip's first irreversible command: "the row exists" now means
	// "we intended to encode this", so without a second mark a half-finished round
	// and a completed one are the same row.
	//
	// It must be IDEMPOTENT. Step 9 runs after finishLocked has already wiped the
	// keys, it runs even on a cancelled context (see Step), and Progress.Done tells
	// the caller not to re-run — so the one repeat this flow can produce is a
	// retried marker, and a retry must not look like a failure or move the
	// timestamp.
	MarkEncoded(ctx context.Context, tenantID, adminID uuid.UUID, uidHex string, actor string) error
}

// --- The key inventory: ONE place, ADR 0017 §6 md. 7 item 5 --------------------

// The names of every buffer a live session may hold. ADR 0017 §4 asks for this
// inventory in one place and §6 md. 7 makes it an acceptance criterion, so it is a
// LIST rather than a set of struct fields: a list can be asserted, and adding to it
// costs one line in one place and buys the wipe for free (see keyring.zeroAll).
//
// 🔴 EVERY ONE OF THESE IS KEY MATERIAL (CLAUDE.md §4.7) AND NONE OF THEM MAY BE
// LOGGED, FORMATTED OR PERSISTED. The two randoms are on the list because they are
// the session keys' entire input (datasheet rev. 3.0 §9.1.7): whoever holds RndA
// and RndB and the authentication key holds the session.
const (
	keyNameSesENC      = "KSesAuthENC"
	keyNameSesMAC      = "KSesAuthMAC"
	keyNameRndA        = "RndA"
	keyNameRndB        = "RndB"
	keyNameSDMFileRead = "K_SDMFileRead"
	keyNameAppMaster   = "K_AppMaster"
)

// keyInventory is the declared order of the slots every keyring is born with.
//
// 🔴 K_AppMaster IS DECLARED AND NEVER FILLED TODAY, AND THAT IS THE POINT.
// ADR 0017 §5.0 decision 2 makes personalising application key 0 NORMATIVE and
// §5.1 step 8 puts it last; §6 md. 5 then blocks SHIPPING it, because `tags`
// carries exactly one aes_key_ref (00004) and ADR 0003 md. 4 fixes it at 44 bytes
// — one AES-128 key. A second key needs a column and a migration, and this round
// writes neither. Declaring the slot now means the day that schema decision lands,
// the wipe, the inventory and the exit paths already cover it: nobody has to
// remember to add a Zero. Until then driver.go emits no ChangeKey for key 0 and
// TestDriver_NoChangeKeyIsEverEmittedForApplicationKeyZero holds that shut.
//
// ⚠️ THIS IS WHY ADR 0017 §5.1 STEP 9 SAYS Zero(anahtarlar), PLURAL: two plain
// plaque keys, one of them not yet reachable.
var keyInventory = []string{
	keyNameSesENC,
	keyNameSesMAC,
	keyNameRndA,
	keyNameRndB,
	keyNameSDMFileRead,
	keyNameAppMaster,
}

// WHAT IS DELIBERATELY *NOT* IN THE KEYRING, counted so the omissions are choices
// rather than oversights:
//
//   - TI. The 4-byte transaction identifier is NOT secret — internal/sun's own
//     EV2Auth comment calls it a replay/interleaving guard — so wiping it would
//     assert a sensitivity it does not have.
//   - CmdCtr. A uint16 counter, not secret and not a buffer. Its accounting rule
//     is in Session.useCtr.
//   - The session ID. It is a BEARER HANDLE: whoever has it can drive the round.
//     It is never logged and never appears in an error. ⚠️ COUNTED LIMIT: it is a
//     Go string, so it CANNOT be wiped — strings are immutable and the runtime may
//     hold copies. Its exposure is bounded by the TTL and by nothing else.
//   - Anything already on the wire. The Part2 cryptogram, the sealed command
//     fields and the wrapped 44-byte blob are all handed to the caller, so wiping
//     this side's copy would protect a byte the phone already has. Claiming
//     otherwise would be the exact shape of over-claim three separate ADR 0017
//     audits kept finding.
//   - The plaintext UID. Public: printed on the plaque, carried in every tap URL
//     (ADR 0003 md. 1).

type keySlot struct {
	name string
	buf  []byte
	// consumed is what makes RndA REUSE UNREPRESENTABLE rather than merely
	// discouraged. See keyring.take.
	consumed bool
}

// keyring is the session's key material, and the ONLY thing in this package that
// calls sun.Zero.
//
// 🔴 THAT EXCLUSIVITY IS THE MECHANISM, NOT A CONVENTION.
// TestSession_OnlyTheKeyringWipes reads this package's source with go/ast and
// fails if sun.Zero is called anywhere outside a keyring method, and
// TestSession_TheRingIsWipedFromExactlyOnePlace fails if zeroAll gains a second
// call site. Together they say: every key buffer this package allocates is
// registered here, and there is exactly ONE line in the package that can end its
// life. ADR 0017 §6 md. 7 asks how the wipe is guaranteed on every exit path; the
// answer is that there is only one exit, Store.retireLocked, and it is the only
// caller of zeroAll.
//
// ⚠️ WHAT THAT PAIR DOES NOT PROVE, WRITTEN DOWN BECAUSE internal/sun's own
// inventory test learned it the hard way: it proves the wipe EXISTS and is
// SINGULAR. It cannot prove a buffer was REGISTERED in the first place.
//
// 🔴 AND THE SENTENCE THAT USED TO FINISH THIS PARAGRAPH CLAIMED THAT GAP WAS CLOSED,
// WHICH IS MEASURABLY FALSE (eighth audit, 2026-08-21). It said an unregistered buffer
// "fails there, not here", pointing at TestStore_TheDrivenExitPathsWipeEveryKeyBuffer.
// Two mutations disproved it, both GREEN: stashing auth.KeyENC into an existing
// byte-slice field after the session keys are registered, and stashing the plaque key
// into one before the row is written. Neither is wiped on any exit path.
//
// The reason is structural and worth naming: armed() builds its slice headers from
// ring.filled() and pins wantNames to five KNOWN names, so it catches a known buffer
// that STOPPED being registered and can never see a NEW one.
//
// SO THE GAP IS STATED WITHOUT COUNTING PLACES, AND THAT PHRASING IS ITSELF THE FIX.
//
// 🔴 THE HONEST STATEMENT: an unregistered buffer is seen by NOTHING. Exactly one
// shape is mechanised — a NEW FIELD ON Session, caught by
// TestSession_TheSessionFieldsAreInventoried, which is the shape ADR 0017 §5.1 step 8's
// second plaque key will take. EVERY other resting place is uncaught: a new or reused
// field on any other type, a package variable, a closure capture, a map value — and a
// MAP KEY, which is categorically worse than the rest and is named separately below.
//
// 🔴 WHY IT IS WORDED THIS WAY RATHER THAN ENUMERATED, AND THIS IS THE ACTUAL LESSON.
// Three successive audits each measured one more shape the enumeration had missed: a
// new field on another type (Store.stash), then an EXISTING field of another type
// (a plaque key used as a map key), then a closure capture holding a copy in a
// goroutine — every one of them green. Each time the list was extended, and each time
// the next audit found the extension short. A list of hiding places cannot be
// completed, so counting them produced a number that was wrong on arrival. Stating it
// without places cannot be short: there is nothing left to enumerate.
//
// 🔴 THE MAP KEY IS WORSE THAN "UNCAUGHT" AND MUST BE SAID PLAINLY. Key bytes used as
// a map key enter a Go STRING. A string cannot be zeroed — sun.Zero has nothing to
// write to, retireLocked's delete removes the entry but not the backing bytes, and
// neither the sweeper nor Close can reach them. For that shape ADR 0017 §6 md. 7
// item 3 is not merely unenforced; it is PERMANENTLY false.
//
// This package leans on stopping rule 2 — "a counted gap is safer than a gap CLAIMED
// closed" — and that only holds while the count is right. A short count is a claim
// wearing a count's clothes. Handed on as ITEM 14 of the card's handover list, in this
// same place-independent form.
//
// ⚠️ THAT NUMBER WAS 10 AND POINTED AT AN EMPTY ENTRY (tenth audit, 2026-08-21). The
// card carried a contentless "see item 15" stub at 10 — the same defect a previous
// round had deleted one instance of and left its twin two rows away — so this forward
// reference landed on nothing. The stub is gone, the list is FIFTEEN items, and the
// pointer is verified by command rather than by memory:
//
//	grep -n "ITEM 14 of the card" internal/encode/session.go
//	sed -n '/^> 14\. /,/^> 15\. /p' docs/plan/m8-deploy-pilot.md
//
// 🔴 WHICH LOCK OWNS A RING — WRITTEN DOWN BECAUSE IT WAS NOT, AND THE OBVIOUS GUESS
// IS WRONG (security audit F5, 2026-08-21).
//
// A session's ring is owned by THE GOROUTINE THAT HOLDS s.busy, NOT by st.mu.
// advance — and through it keyring.add — runs OUTSIDE st.mu by design, so holding the
// store mutex does not make a ring safe to touch: an auditor's probe that peeked a
// busy session's ring WHILE HOLDING st.mu produced a real -race report between peek
// and add.
//
// Not reachable in production today (all eleven retireLocked call sites are either
// the owner itself or busy-guarded), and the exception proves the rule: Step's panic
// branch touches the ring on the owner's own goroutine.
//
// ⚠️ IT MATTERS FOR B2c-2. An operator health surface, or a "what is this session
// holding" endpoint, that takes st.mu and reads a ring would be racing live key
// material while feeling safe — and the armed() test helper already uses exactly that
// shape. Anything that needs a ring must go through the busy handshake, not the mutex.
type keyring struct {
	slots []keySlot
}

func newKeyring() *keyring {
	k := &keyring{slots: make([]keySlot, 0, len(keyInventory))}
	for _, name := range keyInventory {
		k.slots = append(k.slots, keySlot{name: name})
	}
	return k
}

func (k *keyring) find(name string) (*keySlot, error) {
	for i := range k.slots {
		if k.slots[i].name == name {
			return &k.slots[i], nil
		}
	}
	// Names a slot name, which is a compile-time constant in this file, never a byte.
	return nil, fmt.Errorf("encode: %q is not in the key inventory", name)
}

// add hands a buffer to the ring, which owns it from that moment.
//
// Refusing a second add for the same name is not tidiness: two buffers under one
// name means one of them is not on the list zeroAll walks, i.e. an unwiped copy —
// the precise defect internal/sun's F2 audit found in ev2RotateLeft1.
//
// 🔴 A REFUSED BUFFER IS WIPED HERE, BY THIS METHOD, AND THAT IS WHY THE
// "only the keyring calls sun.Zero" RULE CAN BE ABSOLUTE. The alternative shape —
// returning an error and leaving the caller to wipe — puts a sun.Zero at every
// call site, which is the state internal/sun's F1 audit measured and found to be
// a sentence rather than a mechanism (deleting all eleven of them left the suite
// green). Owning the failure here means there is no orphan for a caller to forget.
func (k *keyring) add(name string, buf []byte) error {
	s, err := k.find(name)
	if err != nil {
		sun.Zero(buf)
		return err
	}
	if s.buf != nil {
		sun.Zero(buf)
		return fmt.Errorf("encode: key slot %q is already filled", name)
	}
	if len(buf) == 0 {
		return fmt.Errorf("encode: refusing to register an empty buffer as %q", name)
	}
	s.buf = buf
	return nil
}

// peek reads a registered buffer WITHOUT consuming it — for a key that is used
// more than once in a round (K_SDMFileRead is read by step 6, and by step 8 the
// day ADR 0017 §6 md. 5 lets step 8 exist). It is separate from take so that
// "read again" and "must never be read again" are different verbs at the call
// site rather than a comment.
func (k *keyring) peek(name string) ([]byte, error) {
	s, err := k.find(name)
	if err != nil {
		return nil, err
	}
	if s.buf == nil {
		return nil, fmt.Errorf("encode: key slot %q is empty", name)
	}
	return s.buf, nil
}

// take moves a buffer out for its ONE use.
//
// 🔴 THIS IS THE STRUCTURAL HALF OF THE RndA RULE (ADR 0017, FAZ B2a's F4 hand-off).
// The buffer stays in the ring — so zeroAll still reaches it on every exit path —
// but the slot is marked consumed, and a second take returns an error instead of
// bytes. There is therefore no expression in this package that yields the same
// RndA twice; reuse is not "avoided", it is unrepresentable.
//
// WHY THAT MATTERS, and it is not confidentiality: a recorded
// (E(K,RndB), E(K,TI||RndA'||caps)) pair replayed against a session that reuses
// RndA passes internal/sun's RndA-echo check WITHOUT THE KEY, so ADR 0017 §5.3's
// probe 2 — "authenticate with the key in the row; success means step 6 ran" —
// would report a FALSE SUCCESS. The cost of a repeat is a wrong diagnosis of a
// half-written chip, which is how a plaque gets marked good and shipped.
func (k *keyring) take(name string) ([]byte, error) {
	s, err := k.find(name)
	if err != nil {
		return nil, err
	}
	if s.consumed {
		return nil, fmt.Errorf("encode: key slot %q has already been consumed and cannot be used twice", name)
	}
	if s.buf == nil {
		return nil, fmt.Errorf("encode: key slot %q is empty", name)
	}
	s.consumed = true
	return s.buf, nil
}

// zeroAll wipes every registered buffer. Idempotent: calling it twice is a no-op,
// which is what lets the single exit path be unconditional.
func (k *keyring) zeroAll() {
	for i := range k.slots {
		sun.Zero(k.slots[i].buf)
	}
}

// filled reports the names that currently hold a buffer. Used by the exit-path
// tests to state what a session was holding when it died.
func (k *keyring) filled() []string {
	var out []string
	for i := range k.slots {
		if k.slots[i].buf != nil {
			out = append(out, k.slots[i].name)
		}
	}
	return out
}

// --- Sizing decisions: ADR 0017 §6 md. 7 items 1, 2 and 4 ----------------------

const (
	// exchangeBudget is the wall-clock allowance for ONE relayed APDU exchange:
	// server -> phone over HTTPS, phone -> chip over NFC, and back.
	//
	// ⚠️ IT IS A BUDGET, NOT A MEASUREMENT, AND SAYING SO IS THE POINT. Nothing in
	// this repository has ever timed a relayed exchange: ADR 0017 §6 md. 2 records
	// that the Android side does not exist and "röle turunun uçtan uca gecikmesi
	// ölçülmedi". The only number anywhere is deploy/README's own back-of-envelope
	// ~150 ms per exchange, which the ADR itself labels a rough estimate. Five
	// seconds is that estimate with a 33x margin, which is what a bad mobile link
	// deserves and what a budget standing in for a missing measurement should look
	// like. When FAZ B3 finally times one on silicon, this is the constant to
	// revisit, and DefaultTTL follows it automatically.
	exchangeBudget = 5 * time.Second

	// DefaultTTL is a session's TOTAL budget, from Begin to the last exchange.
	//
	// ⚠️ IT IS NOT AN IDLE TIMEOUT, AND THE FIRST WORD OF THIS COMMENT USED TO SAY IT
	// WAS ("how long a session may live WITHOUT PROGRESS") — corrected 2026-08-21,
	// second audit. The deadline is set once in Begin and is never extended; the
	// THREE writes to it all move it EARLIER (cancellation in checkout, Abort, and
	// Close's retireIdleLocked). ⚠️ That count was "two" until a fifth audit measured
	// the third and found it UNGUARDED — it could push a deadline later. It is
	// guarded now, so the "never extended" half is a property rather than a claim,
	// and TestSession_EveryWriterOfTheDeadlineMovesItEarlier drives all three (an
	// earlier version of this line named TestSession_...IsNeverExtended, which drives
	// the progress reading and none of the three writers — an in-place name
	// substitution that no mechanical check can see, because both names resolve).
	// The paragraph's own floor
	// derivation requires exactly that reading: len(roundSteps) * exchangeBudget is a
	// budget for the WHOLE round, which would be the wrong bound for a per-step idle
	// timer. TestSession_TheDeadlineIsATotalBudgetAndIsNeverExtended pins it, because
	// neither reading was pinned and a mutation that turned it into an idle timeout
	// survived.
	//
	// 🔴 THE OPERATIONAL CONSEQUENCE, WRITTEN DOWN BECAUSE IT IS A REAL COST: a
	// legitimate relayed round that takes longer than 90 s dies mid-flight, possibly
	// AFTER step 6 — leaving a chip whose K_SDMFileRead is ours but whose SDM is
	// still off (ADR 0017 §5.3's recoverable "step 6 ran, step 7 did not"). That is
	// the fail-closed direction, and it is recoverable, but it is not free. The number
	// to revisit when FAZ B3 finally times a relayed exchange is exchangeBudget.
	//
	// 🔴 IT IS BOUNDED FROM BOTH SIDES AND BOTH BOUNDS ARE ASSERTED
	// (TestSession_TheTTLCoversTheRoundAndNotMuchMore), because a bare number in
	// this repository is a defect by itself.
	//
	// FLOOR — the round must fit. driver.go's step table is ten exchanges, so the
	// floor is len(roundSteps) * exchangeBudget = 50 s, and the test derives it from
	// the table rather than from a repeated literal. ⚠️ THAT TEN IS THIS TABLE'S,
	// AND NOTHING CONFIRMS IT INDEPENDENTLY — see driver.go's roundSteps, where an
	// earlier version of this comment claimed ADR 0017 §4 counted "the same ten" and
	// was wrong: the ADR's ten has a different composition and agrees by coincidence.
	//
	// CEILING — twice the floor. The cost of a long TTL is an ABANDONED session
	// holding its plain plaque key for exactly that long (ADR 0017 §6 md. 7: "süreç
	// ölmediği sürece terk edilmiş bir oturum düz anahtarı TTL boyunca tutar"), and
	// nothing is bought in exchange, because a round that has stopped progressing is
	// a round that is not coming back.
	//
	// ⚠️ THAT SENTENCE SAID "TWO plain plaque keys" AND THE SHIPPED CODE HOLDS ONE.
	// Measured: `grep "ring.add(" internal/encode/*.go` outside tests gives exactly
	// five sites — K_SDMFileRead, RndA, RndB, KSesAuthENC, KSesAuthMAC — and
	// keyNameAppMaster is never passed to add at all, which keyInventory's own comment
	// says ("DECLARED AND NEVER FILLED TODAY") and armed() pins by expecting exactly
	// one plaque key. It becomes two the day ADR 0017 §6 md. 5 lands and step 8 ships.
	// Over-stating exposure is the safe direction, but a written count that is wrong
	// for the shipped code is a count — and this one was carrying a TTL bound.
	//
	// 🔴 THE SPREAD WAS THE REAL DEFECT AND IT TOOK TWO ROUNDS TO MEASURE. A seventh
	// audit corrected three copies; an eighth measured SEVEN occurrences, of which SIX
	// were unqualified — one in production code and two inside t.Fatalf messages,
	// which is where somebody reads while deciding. The correction that reported
	// "three" was itself a count, and it was short.
	//
	// ⚠️ AND THE RE-MEASUREMENT NEEDS ITS OWN QUALIFIER, because the naive command
	// counts ITSELF: this very sentence contains the search string, so a repo-wide
	// grep returns three hits, of which two are THIS retraction and the card's record
	// of it. The claim that survives is the narrow one: exactly ONE live use of the
	// phrase remains, in keyInventory's comment, in the FUTURE tense, about step 8 —
	// and it is qualified there. A grep whose result includes the grep is not a
	// measurement of the code.
	//
	// 🔴 DERIVED, NOT TRANSCRIBED — AND THE PREVIOUS VERSION OF THIS COMMENT WORE A
	// "MEASURED" LABEL IT HAD NOT EARNED. It said the datasheet gives the chip's
	// session as dying "when the field drops", citing p. 28. Re-measured against the
	// PDF (97 pages per pdfinfo, read with pdftotext -layout): the phrase
	// "authentication state" occurs
	// EXACTLY TWICE — §9.1.9 (p. 28) and §9.1.10 (p. 29) — and both are about a
	// COMMAND ERROR: "The authentication state is immediately lost and the error
	// return code is sent without a MAC appended." Searching for the half the claim
	// actually rested on returns nothing at all: "RF field", "field is removed",
	// "power off", "deselect", "HALT", "leaves the field" — zero occurrences each.
	// The document says nothing about the field dropping, so neither may this
	// comment.
	//
	// What IS transcribed, and what it does and does not support: any command error
	// kills the chip-side session, so a round that has FAILED is unresumable and its
	// server session is pure liability. What happens when the phone simply walks away
	// is NOT in the document; the ceiling therefore rests on the ADR's own exposure
	// argument above, which is a design judgement, not a reading. Ninety seconds sits
	// inside a floor of 50 s and a ceiling of 100 s.
	DefaultTTL = 90 * time.Second

	// sweepDivisor sets the sweeper's period as DefaultTTL/6 = 15 s, so an
	// abandoned session's keys survive their deadline by at most a sixth of the TTL.
	//
	// ⚠️ THE TRADE-OFF SENTENCE HERE WAS WRITTEN BACKWARDS AND IS CORRECTED
	// (2026-08-21, second audit) — in the very block a previous audit had just made
	// me justify, which is its own lesson. It said "smaller divisors cost a wakeup …
	// larger ones widen the window". The arithmetic is period = ttl / sweepDivisor,
	// so it is the other way round: a SMALLER divisor means a LONGER period, i.e. a
	// WIDER window in which a dead session still holds a plain key, and FEWER
	// wakeups. A LARGER divisor buys a narrower window and pays for it in wakeups
	// against a store that is empty almost all the time (encoding is a rare, manual
	// job). Six is the middle of that, and both directions are now bounded by
	// TestStore_TheSweepCadenceIsTheOneItsCommentClaims rather than by this sentence.
	sweepDivisor = 6

	// DefaultMaxPerActor bounds how many live sessions one operator may hold.
	//
	// An operator has ONE phone and can hold it against ONE plaque, so the working
	// number is 1. The cap is 3 because an abandoned round is not cleaned up until
	// the TTL expires: "tap, miss, retry, retry" leaves two dead sessions behind a
	// live one, and a cap of 1 would lock a real operator out for 90 s after every
	// dropped read. Three is that sequence plus nothing.
	//
	// 🔴 IT BOUNDS AN ACCIDENT, NOT AN ADVERSARY, AND AN EARLIER VERSION OF THIS
	// COMMENT INVITED THE WRONG READING (security audit L4, 2026-08-21). It said this
	// bounds what "one actor — buggy, or HOSTILE with a valid handle — can pile up".
	// Measured: actor is a caller-supplied STRING used for nothing but this counter's
	// key — no decision, no log, no persistence — so anyone who can call Begin can
	// vary it and mint a fresh budget each time. Against a hostile caller the only
	// real ceiling is DefaultMaxLive, which is why THAT constant carries the
	// key-material bound and is the one
	// TestStore_TheGlobalCapBoundsKeyMaterialAndNotJustMemory asserts. What this cap
	// holds is a buggy or retrying client, which is the case it was chosen for.
	DefaultMaxPerActor = 3

	// DefaultCloseGrace is how long Close waits for in-flight steps to drain.
	//
	// It is generous relative to ONE exchange (exchangeBudget) and short relative to
	// a whole round (DefaultTTL): Close is not trying to let a round finish, only to
	// let the step that is currently inside advance come back and release its ring.
	// Five seconds is exchangeBudget, and the bound exists because a shutdown that
	// can hang for ever is not a shutdown.
	DefaultCloseGrace = 5 * time.Second

	// DefaultRepairGrace bounds EACH write that finishes a round after the request
	// that started it has gone away.
	//
	// 🔴 IT IS EXPORTED SO A GATE CAN SEE IT (ninth audit, 2026-08-24). The number it
	// replaced was an unexported constant in rows.go whose comment asserted a
	// relationship — "5s nests inside httpShutdownGrace's 20s" — that NOTHING held:
	// cmd/tappa could not read the identifier, so httpShutdownGrace could have been
	// lowered to 3s and the nesting would have broken in silence. This task's own rule
	// is that a number is bound to a gate, dated, or deleted; this one is now bound, by
	// cmd/tappa's TestShutdownBudget_TheDetachedRepairsNestInsideTheHTTPGrace.
	//
	// ONE constant for BOTH detached writes rather than two numbers, because the second
	// write happens only when the first failed and their budgets therefore ADD. The
	// gate asserts the sum: 2 x DefaultRepairGrace <= httpShutdownGrace.
	//
	// ⚠️ THE SECOND WRITE STILL GETS A FULL BUDGET, not the remainder — context
	// .WithoutCancel drops the parent's DEADLINE as well as its cancellation, so the
	// compensating write is not starved by the marking that just timed out. That is
	// precisely the case it exists for.
	//
	// 🔴 AND THE SUM IS NOT THEORETICAL — MEASURED UNDER pool_max_conns=1 (tenth audit,
	// 2026-08-25), which is what makes this number worth having a gate for:
	//
	//	cancelled ctx, before the detach   5.000 s
	//	detached ctx, as shipped          10.002 s
	//
	// A dead request occupies its handler goroutine for TEN SECONDS under pool
	// pressure. That is the real cost of detaching, it lands inside the in-flight
	// request srv.Shutdown already waits for, and 10 <= httpShutdownGrace's 20 is the
	// exact relationship the gate holds. The two writes are SEQUENTIAL and SYNCHRONOUS
	// on the plaqueEncodeStep -> Step -> markEncoded chain, which is why they add to
	// each other and nest inside the drain rather than extending it.
	DefaultRepairGrace = 5 * time.Second

	// DefaultMaxLive bounds the store as a whole, across all actors.
	//
	// The pilot is one branch and encoding is manual (ADR 0017 §4), so the real
	// number is 1. Sixty-four is a memory ceiling, not a capacity plan: it is what
	// stops the store from being driven into holding an unbounded amount of key
	// material, which is the only failure mode of an in-memory session table that
	// costs more than a retry.
	DefaultMaxLive = 64

	// DefaultMaxPerTenant bounds one BUSINESS's share of the store — the axis the
	// first two caps do not have.
	//
	// 🔴 IT IS DERIVED FROM DefaultMaxLive RATHER THAN CHOSEN, so the three numbers
	// cannot drift apart: an eighth of the store. Both directions are asserted by
	// TestSession_TheTenantCapSitsBetweenTheActorCapAndTheStore —
	//
	//	FLOOR    it must clear 2 x DefaultMaxPerActor, so two operators of the same
	//	         business can each hold a full retry sequence ("tap, miss, retry,
	//	         retry") without the business's own cap refusing either of them.
	//	CEILING  it must stay at or under a quarter of the store, or "one business
	//	         cannot take the product down" stops being true in any useful sense.
	//
	// 🔴 WHAT IT DOES NOT DO, AND THE FIRST VERSION OF THIS PARAGRAPH PRICED THE
	// RESIDUAL 2.75x TOO HIGH. It said "twenty-two tenants at three sessions each still
	// reach 66 >= DefaultMaxLive" — the arithmetic of the world BEFORE this cap, quoted
	// as if it described the world after. MEASURED against the shipped store: EIGHT
	// distinct tenants, each filling its own cap of eight, hold 64 live rounds and the
	// store then refuses everyone.
	//
	// So the cap does not stop N DISTINCT tenants from exhausting the store between
	// them. The cheapest price is EIGHT sign-ups AND TWENTY-FOUR ADMIN ACCOUNTS, and
	// the second half was missing from the first three versions of this paragraph
	// (security audit, second pass): eight tenants is what the TENANT cap alone
	// implies, but DefaultMaxPerActor is 3, so filling one tenant's eight takes
	// ceil(8/3) = 3 distinct actor labels, and through the endpoint an actor label is
	// not a free string — plaqueEncodeGrantOf builds it as "admin:"+AdminUserID from
	// the session cookie, so a distinct label costs a distinct ADMIN ACCOUNT. 8 x 3 =
	// 24. TestSession_TheCheapestExhaustionIsEightTenantsAndTwentyFourAdmins measures
	// both numbers off the shipped constants rather than restating them, because this
	// sentence has now been wrong twice by being written rather than run.
	//
	// What it removes is the SINGLE-tenant
	// version, which is cheaper still — one sign-up, one address. The N-tenant version
	// is bounded by nothing here; the honest place for it is the endpoint's own budgets
	// and, ultimately, an operational limit. ADR 0017 §4's "a rollout window loses a
	// round" paragraph is the right precedent: the residual is written down rather than
	// papered over — and at its REAL price, because a residual quoted too HIGH is still
	// a wrong count.
	DefaultMaxPerTenant = DefaultMaxLive / 8

	// MaxActorLen bounds the operator label Begin accepts.
	//
	// It is a BYTE length rather than a rune count, deliberately and unlike
	// tenant.MaxVenueNameRunes: this is not a human-facing name that Maltese
	// diacritics would unfairly shorten (00013 Part 3b's reasoning), it is a
	// machine-ish handle whose only jobs are to key a counter and to sit in an
	// append-only jsonb field. What must be bounded is what lands in the column,
	// and that is bytes.
	//
	// 200 is chosen against what the label actually is — an operator or device
	// handle, tens of bytes — with room for a uuid, an email and a separator
	// (~90) and again as much slack.
	//
	// ⚠️ THE SENTENCE THAT FOLLOWED THIS ONE EXPIRED ON 2026-08-24 (FAZ B2c-2b). It
	// said "no endpoint supplies it". internal/handler's encode relay now does, and
	// what it supplies is `admin:<uuid>` — 42 bytes, well inside this bound, and
	// derived from the resolved panel session rather than from the request body.
	MaxActorLen = 200
)

// RequestsPerRound is how many HTTP exchanges one COMPLETE encode round costs a
// relay: one Begin, then one Step per entry in the step table.
//
// 🔴 IT IS EXPORTED FOR ONE REASON AND THAT REASON IS A RULE, NOT A CONVENIENCE.
// internal/handler has to size a rate-limit budget against "how many plaques may an
// operator encode in a window", and this repository's standing rule is that a number
// is either bound to a gate, dated, or deleted (docs/plan/agent-brief.md). Written
// out as `11` in the handler it would be a second representation of this table, and
// this repository has paid for that shape often enough to have a name for it. Derived
// from here it MOVES on its own the day ADR 0017 §5.1 step 8 ships (§6 md. 5): the
// TABLE goes from ten exchanges to eleven, so this function goes from 11 to 12.
// ⚠️ THE TWO ELEVENS ARE ONE APART AND USED TO SIT IN ONE SENTENCE WITHOUT SAYING SO
// — len(roundSteps) and this function's result are never the same number, and an
// auditor read the handler's copy of that sentence as arithmetic that does not close.
//
// ⚠️ IT IS A FUNCTION AND NOT A CONSTANT, and not by preference: roundSteps is a
// slice, so len() over it is not a constant expression. Anything derived from it is
// therefore a var, which is why internal/handler pins the derived budget in a test of
// its own rather than relying on adminratelimit.go's constant pin.
//
// 🔴 A FUNCTION AND NOT AN EXPORTED var, WHICH IS A CORRECTION (audit, 2026-08-24).
// It shipped as an exported package-level variable, and an exported var is
// ASSIGNABLE by any importer — a package could set it to 1 and silently multiply the
// encode rate limit by eleven. Harmless in the shipped tree (internal/handler reads it
// once at package init) and a shape worth not leaving lying about: a function returns
// a value nobody can rebind.
//
// ⚠️ WHAT IT DOES NOT COUNT, because a budget written against the wrong denominator
// is worse than none: an ABORT (one more request), a round that dies part-way (fewer),
// and — the one that matters for ADR 0017 §6 md. 12 — the number of requests it takes
// to WRITE A ROW — which is RequestsBeforeTheRowIsWritten() below, and is NOT counted
// here on purpose. A budget sized on a whole round therefore permits
// RequestsPerRound/RequestsBeforeTheRowIsWritten() times as many inventory rows as it
// does completed plaques.
//
// 🔴 THIS PARAGRAPH SAID "FOUR (Begin plus three Steps)" AND WAS THE FOURTH PUBLISHED
// COPY OF AN OFF-BY-ONE — twenty lines above the function that exists to correct it,
// and written in the same round that corrected the other three. The right number is
// FIVE and the reason is one line down. No digit is written here now: the function is
// the answer, and it is measured against a real round by
// TestDriver_TheRowIsWrittenOnTheExchangeThisNumberNames.
func RequestsPerRound() int { return 1 + len(roundSteps) }

// RequestsBeforeTheRowIsWritten is how many HTTP exchanges a relay must complete
// before an inventory row exists for the plaque — ADR 0017 §5.1 step 3.
//
// 🔴 IT IS THE DENOMINATOR ADR 0017 §6 md. 12 CARES ABOUT, AND IT IS NOT
// RequestsPerRound. The hazard that item counts is a SQUATTED uid, not a finished
// plaque: tags.uid is a global PRIMARY KEY, so the damage is done the moment the row
// lands, six exchanges before the round ends. A rate limit divided by the length of a
// whole round therefore overstates how much it bounds squatting by a factor of nearly
// three, and internal/handler sizes its budget against THIS number for that reason.
//
// ⚠️ IT IS DERIVED FROM THE STEP TABLE RATHER THAN COUNTED BY HAND, and the FIRST
// DERIVATION WAS OFF BY ONE — caught by the test below rather than by reading, which is
// exactly why the test exists. It said `1 + rowWriteStepIndex()` = 4. The row is
// written by the ACCEPT of the step at that index, and an accept runs when the response
// is FED BACK IN, which is the (index+1)-th Step call. So: one Begin, plus index+1
// Steps, = index + 2 = **5**.
//
// 🔴 THE OFF-BY-ONE WAS ALREADY PUBLISHED IN THREE PLACES AS "/4", giving 55 rows per
// session and 750 per address. Both were wrong in the UNSAFE direction — they made the
// gate look weaker than it is (44 and 600) — but a bound written from an unmeasured
// denominator is a bound nobody has checked, whichever way it errs.
func RequestsBeforeTheRowIsWritten() int { return rowWriteStepIndex() + 2 }

// rowWriteStepIndex finds the exchange whose accept writes the tags row.
//
// It matches on the step's own ADR reference rather than on its name: stepDef.adr is
// the field that exists to keep the table and the document from drifting, and §5.1
// step 3 has exactly one home in the sequence. A rename of the exchange does not move
// the row; a change to WHICH exchange writes it does, and that is what should move
// this number.
func rowWriteStepIndex() int {
	for i, s := range roundSteps {
		if strings.Contains(s.adr, "then step 3") {
			return i
		}
	}
	// Unreachable while the table carries step 3, and a panic is right: a silent zero
	// would hand internal/handler a denominator of one and quietly loosen a rate limit.
	panic("encode: no step in roundSteps writes the tags row (ADR 0017 §5.1 step 3)")
}

// --- Errors --------------------------------------------------------------------

var (
	// ErrUnknownSession is returned for an ID that is not live — never seen,
	// already finished, expired, or swept. The four are DELIBERATELY not
	// distinguished: the ID is a bearer handle, and telling a caller which of those
	// it is turns a guess into an oracle.
	ErrUnknownSession = errors.New("encode: no such session")

	// ErrBusy means another step for this session is already running. A relay is
	// sequential by construction, so this is either a duplicate post or a second
	// client on one handle; both are refused rather than interleaved, because two
	// commands sharing one CmdCtr would break the session on the chip and the
	// failure would be diagnosed as a MAC error.
	ErrBusy = errors.New("encode: a step is already in flight for this session")

	// ErrStoreClosed is returned once Close has run.
	ErrStoreClosed = errors.New("encode: the session store is closed")

	// ErrTooManySessions and ErrPlaqueBusy are the two concurrency limits.
	ErrTooManySessions = errors.New("encode: too many live encode sessions")
	ErrPlaqueBusy      = errors.New("encode: another encode round is already live for this plaque")
)

// RelayMismatchError is ADR 0017 §6 md. 12's FOURTH CONSEQUENCE, caught.
//
// The relay reported one UID at §5.1 step 2 (GetVersion, which is unauthenticated
// — nothing in that frame is signed) and the chip returned a different one at
// GetCardUID, whose response IS sealed with the session keys. So a row was written
// for RowUID while the session was in fact talking to the chip that calls itself
// ChipUID.
//
// 🔴 WHAT THIS COSTS, AT ITS CURRENT SIZE: a PHANTOM INVENTORY ROW. The row for
// RowUID exists and describes no chip; it sits at status 'unassigned' with
// location_id NULL, so it never appears in service. Nothing has been written to any
// chip.
//
// ⚠️ AND THIS HEADER USED TO CLAIM THE OTHER, MUCH WORSE OUTCOME AS A FACT — "the
// chip now holds a plaque key that appears in NO ROW under its own UID … permanent
// plaque loss … Detecting it does not undo it" — WHICH THE SHIPPED ORDER CAN NO
// LONGER PRODUCE (sixth audit, 2026-08-21). Security audit F1 moved the UID gate
// ahead of WriteData and ChangeKey, precisely so a mismatch lands on the SAFE half of
// §5.2's asymmetry; TestDriver_ARelayThatLiesAboutTheUIDIsCaught asserts the chip is
// unwritten, its key still factory, and that no irreversible command reached it. The
// sentence survived its own refutation by three paragraphs, and it announced itself
// as "named rather than softened", which is the shape that makes a stale claim read
// like a measured one.
//
// 🔴 THE OLD OUTCOME IS STILL REACHABLE BY ANOTHER ROUTE, so it is named rather than
// deleted: ADR 0017 §5.3's recovery flow re-enters at step 5 on a half-written chip.
// A mismatch discovered there would leave a personalised chip whose key is filed
// under the wrong UID — recoverable, see item 2 below, but not free.
//
// 🔴 THE CLEANUP PATH, NAMED because ADR 0017 §6 md. 12 requires it to be written
// down and because there is no automatic one:
//
//  1. The row for RowUID must be retired — ADR 0003 md. 5's normative
//     `retire + replace`. tappa_app may set status/retired_at (migration 00013's
//     column-scoped GRANT) but may NEVER rewrite aes_key_ref and may not DELETE.
//  2. The physical chip is RECOVERABLE, and an earlier version of this list said it
//     was "scrap … its key cannot be recovered". 🔴 MEASURED, AND FALSE (security
//     audit F2, 2026-08-21). The wrapped key in the phantom row is sealed with
//     AAD = RowUID (driver.go passes s.uid, the UID the relay reported), and RowUID
//     is PUBLIC — this error prints it. So sun.Unwrap(kek, RowUID, ref) opens it, and
//     it is byte-for-byte the key now on the chip. Application key 0 is also still at
//     its factory value, because ADR 0017 §5.1 step 8 is not shipped.
//     The recovery is therefore: unwrap from the phantom row and either re-drive the
//     chip with that same key, or re-key it under the still-public key 0. What must
//     NOT happen is what the old sentence invited — binning a personalised chip that
//     is still carrying a real Tappa plaque key.
//     ⚠️ Since the UID gate moved ahead of the first irreversible command (see
//     roundSteps), this scenario needs a chip that was written before the mismatch,
//     which the shipped order no longer produces — it is kept because a half-written
//     chip is still reachable through ADR 0017 §5.3's recovery flow.
//  3. Both are MANUAL today. This driver performs neither: it aborts, wipes and
//     reports. ADR 0017 §6 md. 12 says the only cleanup available is a tappa_owner
//     operator acting by hand, and inventing a second path here would be inventing
//     schema this round is not allowed to write.
//
// Both UIDs are public (ADR 0003 md. 1) and are therefore safe to name.
type RelayMismatchError struct {
	RowUID  string
	ChipUID string
}

func (e *RelayMismatchError) Error() string {
	// The message no longer says "the chip is scrap": measured, the key IS recoverable
	// from the row it names (see this type's doc), and telling an operator to bin a
	// personalised chip is a production instruction pointed the wrong way.
	//
	// 🔴 AND THE REASON IT GIVES FOR KEEPING THE ROW IS CORRECTED TOO (sixth audit).
	// It said "it holds the only copy of the key" — inherited from the pre-F1 world.
	// With the gate ahead of the first irreversible command, the phantom row's key is
	// on NO chip, so "the only copy" is true and irrelevant. The row must survive for
	// the reason ADR 0017 §5.2 actually gives: a failed round does not delete its row
	// ("sessiz temizlik yoktur"), the row is the inventory trace of what happened, and
	// tappa_app cannot rewrite aes_key_ref anyway (migration 00013).
	return fmt.Sprintf("encode: the relay reported UID %s but the chip authenticated as %s; "+
		"retire the row for %s by hand — do NOT delete it, a failed round keeps its inventory trace",
		e.RowUID, e.ChipUID, e.RowUID)
}

// --- The session ---------------------------------------------------------------

// ID is a session handle. It is a bearer credential: never log it, never put it in
// an error message.
type ID string

func newID() (ID, error) {
	var b [16]byte
	// The error is checked even though crypto/rand.Read on go1.26.7 documents
	// itself as never returning one ("It never returns an error, and always fills
	// b entirely" — it crashes instead). CLAUDE.md §7 forbids `_ = err`, and a
	// checked-but-unreachable branch costs one line and survives a stdlib change.
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("encode: mint session id: %w", err)
	}
	return ID(hex.EncodeToString(b[:])), nil
}

// Session is one live encode round.
//
// 🔴 IT HAS NO EXPORTED FIELDS AND NO EXPORTED METHODS, AND THAT IS DESIGN
// DECISION (a) FROM THIS ROUND'S BRIEF, MADE CONCRETE.
//
// internal/sun's EV2Auth exports all five of its fields and carries NO
// `authenticated bool` — deliberately, because it is a RESULT VALUE: anybody can
// write &sun.EV2Auth{...} and all they get is a byte builder that opens no door.
// The question that shape leaves open is where authentication state then lives.
// The answer, decided here: IN THE STEP INDEX OF A SESSION THE STORE OWNS.
//
// A session is authenticated exactly when its step index has passed
// stepAuthenticate2 and auth is non-nil, and both of those are unexported fields of
// an unexported-in-practice type: there is no exported function in this package
// that accepts or returns a *Session, so a caller cannot construct one, cannot
// mutate one, and cannot hold one after the store has retired it.
// TestSession_NoExportedAPIHandsOutASession asserts that with go/ast, and
// TestSession_TheEV2AuthShapeIsTheOneThisPackageInventories asserts EV2Auth still
// has exactly the five fields this decision was made about — so growing it a sixth
// (a flag, or a new secret) is a red test rather than a silent change of meaning.
//
// internal/sun/ev2.go is NOT modified by this round. It did not need to be: the
// missing state was never EV2Auth's to hold.
type Session struct {
	id    ID
	actor string
	// tenantID is the business this round's row and trail entries belong to. It is
	// NOT key material and is registered as such in sessionFields.
	//
	// ⚠️ IT IS THE CALLER'S CLAIM, NOT A VERIFIED FACT. Begin takes it, nothing
	// here checks that the caller may encode for it — ⚠️ and the "open item that
	// would" sentence that used to end this paragraph is STALE: ADR 0017 §6 md. 10
	// CLOSED in FAZ B2c-2b, one layer up, where a request has an identity to answer
	// it with. What this field buys is unchanged: the value the ports receive is the
	// value the round was opened with, rather than whatever a pooled connection last
	// happened to be set to.
	tenantID uuid.UUID

	// adminID is the RESOLVED admin who opened the round — httpx.AdminOf's, never a
	// caller's claim — and it becomes audit_log.actor_id. It is a separate field from
	// `actor` on purpose: actor is a label the caller supplies and this is not.
	// uuid.Nil means "no admin", which is how every non-HTTP caller (tests) drives it
	// and which audit reads as the system.
	adminID  uuid.UUID
	deadline time.Time

	// busy is held by the one goroutine currently running a step. Guarded by the
	// STORE's mutex, not by the session, because it is the store that hands the
	// session out.
	busy bool

	ring *keyring
	auth *sun.EV2Auth

	// cmdCtr is the NEXT command counter to use. See useCtr for the accounting
	// rule and why this package keeps it rather than internal/sun.
	cmdCtr uint16
	// lastCtr is the counter the most recently built sealed command used, kept so
	// the response can be verified against the same value.
	lastCtr uint16

	// Public, per ADR 0003 md. 1: the chip's identity travels in every tap URL.
	uid    []byte
	uidHex string

	// versionFrames holds GetVersion's first two frames until the third arrives;
	// internal/sun refuses to parse a UID out of anything but all three.
	versionFrames [2][]byte

	// authPart2 is the AuthenticateEV2First Part 2 cryptogram, held for exactly one
	// step: acceptAuthenticate1 builds it, cmdAuthenticate2 sends it and clears the
	// field. It is NOT in the keyring — it is on its way to the phone verbatim, so
	// wiping this copy would protect a byte the relay already has.
	authPart2 []byte

	// ndef is the template written at step 5 and described to the chip at step 7.
	// Nothing in it is secret (internal/sun's TapNDEF says so).
	ndef *sun.TapNDEF

	stepIdx    int
	rowWritten bool
	finished   bool
}

// useCtr returns the CmdCtr for one new sealed command and advances the counter.
//
// 🔴 THIS IS DESIGN DECISION (b) FROM THIS ROUND'S BRIEF. Before this package,
// NOTHING in Go held the command counter: every internal/sun builder takes cmdCtr
// as an explicit argument, and the only brake on a wrong value was the CHIP, which
// answers a wrong counter with a wrong MAC (datasheet rev. 3.0 §9.1.2). That is a
// real brake but a late one — it fails mid-session, after step 5 or 6 has already
// touched the chip. The counter is now the session's, there is no setter, and the
// value is never supplied by a caller.
//
// THE ACCOUNTING RULE, transcribed: §9.1.2 — "The CmdCtr is reset to 0000h … after
// a successful AuthenticateEV2First" and a command MAC "uses the current CmdCtr.
// The CmdCtr is afterwards incremented by 1", the response using the increased
// value. internal/sun applies the +1 for responses itself; this side must not.
//
// 🔴 THE EVIDENCE SENTENCE THAT USED TO FOLLOW WAS WRONG TWICE (corrected
// 2026-08-21, third-eye audit; re-measured from the PDF). It said AN12196 publishes
// "0, 1, 2, 3 across ONE session", citing Tables 17, 18, 25 and 26. Measured by TI:
// Tables 17 and 18 carry TI = 9D00C4DF, Tables 25 and 26 carry TI = 7614281A — TWO
// SESSIONS, not one. And neither prints a contiguous 0,1,2,3.
//
// What the two published traces DO pin, stated at their own size:
//
//	session A (TI 9D00C4DF): commands at CmdCtr 0000 (§5.8.2 Table 17, WriteData)
//	  and 0100 (§5.9 Table 18, ChangeFileSettings) — CONSECUTIVE, so this is where
//	  "one command, one increment" is anchored.
//	session B (TI 7614281A): commands at 0000 (§5.12 Table 21), then 0200 (§5.16.1
//	  Table 25, ChangeKey on a non-zero key) and 0300 (§5.16.2 Table 26). The 0100
//	  command is NOT printed — the only 0100 in that session is a RESPONSE MAC input
//	  (Table 21 step 18, "Status || CmdCounter + 1 || TI"). What session B pins is
//	  the other half of ADR 0017 §5.0 decision 1: changing a non-zero key leaves the
//	  session and its counter alive, 0200 -> 0300.
//
// FFFFh has no successor — same section: a command arriving with the counter at
// FFFFh "leads to an error response and the command is handled like the MAC was
// wrong". Wrapping to 0000h would forge a counter the chip will never accept, so
// it is refused here rather than being allowed to look like a MAC failure.
func (s *Session) useCtr() (uint16, error) {
	if s.cmdCtr == 0xFFFF {
		return 0, errors.New("encode: the command counter is exhausted; this session cannot issue another command")
	}
	c := s.cmdCtr
	s.cmdCtr++
	s.lastCtr = c
	return c, nil
}

// Progress is what one relayed step produced.
type Progress struct {
	// Command is the next C-APDU for the phone to push at the chip, or nil when
	// the round is over. It contains no secret: a sealed command field is exactly
	// the bytes that go on the wire.
	Command []byte

	// Done reports that the CHIP IS PERSONALISED — every step of ADR 0017 §5.1 the
	// driver ships has completed and been verified.
	//
	// 🔴 READ Done BEFORE READING THE ERROR. Step 9 wipes the keys and then marks
	// the row, in that order (ADR 0017 §5.1). If the marker fails, Step returns
	// Done = true AND a non-nil error: the round SUCCEEDED on silicon and must NOT
	// be re-run.
	//
	// 🔴 WHY NOT, MEASURED — AND THE REASON THIS COMMENT USED TO GIVE WAS A CHAIN THE
	// CODE CANNOT PRODUCE (security audit F3, 2026-08-21). It said a re-run "would
	// call ChangeKey with the factory key as the old key against a chip that no longer
	// holds it, which fails". Measured: a second round against the same chip dies FOUR
	// EXCHANGES EARLIER, at the row insert — "duplicate key value violates unique
	// constraint" — because tags.uid is a PRIMARY KEY (migration 00004) and
	// Rows.InsertUnassigned's own contract requires failing rather than overwriting.
	// ChangeKey is never reached by any contract-abiding Rows implementation.
	//
	// 🔴 IT MATTERS BECAUSE IT CHANGES WHAT THE OPERATOR SEES AND WHAT THEY REACH FOR.
	// Not a chip fault — a DATABASE error that reads like stale inventory, whose
	// obvious "fix" is to DELETE THE ROW. That row holds the only copy of the plaque
	// key (ADR 0017 §5.2): deleting it turns a completed plaque into permanent loss.
	// The conclusion is unchanged — do not re-run — but the reason, and the warning
	// that belongs beside it, are these.
	Done bool

	// Step names the step that just completed, for an operator UI and for tests.
	// It carries a step name and nothing else.
	Step string

	// UIDHex is the chip's UID once GetVersion has disclosed it, in the canonical
	// upper-case form tags.uid stores. Public (ADR 0003 md. 1).
	UIDHex string
}

// --- The store -----------------------------------------------------------------

// Config configures a Store. Rows, Wrapper and BaseURL are required.
type Config struct {
	Rows    Rows
	Wrapper Wrapper

	// BaseURL is the tap URL's scheme, host and path with no query — what
	// sun.BuildTapNDEF turns into the plaque's NDEF template.
	//
	// ⚠️ Q08 IS STILL OPEN (ADR 0017 §6 md. 11) AND THIS FIELD DOES NOT CLOSE IT.
	// A plaque encoded with the wrong host is a physical plaque swap, not a config
	// change. The rule lives above this layer: no production plaque is encoded
	// until Q08 picks the host.
	BaseURL string

	// Clock defaults to the system clock.
	Clock Clock
	// TTL defaults to DefaultTTL.
	TTL time.Duration
	// MaxPerActor defaults to DefaultMaxPerActor, MaxLive to DefaultMaxLive.
	MaxPerActor int
	// MaxPerTenant defaults to DefaultMaxPerTenant.
	MaxPerTenant int
	MaxLive      int

	// CloseGrace is how long Close waits for in-flight steps to drain before it
	// gives up. Defaults to DefaultCloseGrace. See Close for why it waits at all.
	CloseGrace time.Duration
}

// Store owns every live encode session.
//
// 🔴 THERE IS NO WAY TO GET A SESSION OUT OF IT. Begin, Step and Abort take an ID
// and return bytes; none of them returns a *Session and none of them takes one. A
// caller therefore cannot hold a session, cannot drop one on the floor, and cannot
// keep one alive past a retire.
//
// ⚠️ THE QUALIFIER, ADDED AFTER AN AUDIT REMOVED THE ONE THAT WAS MISSING. This
// custody rule is what makes "sun.Zero runs on every exit path" (ADR 0017 §6 md. 7
// item 3) a property of THIS TYPE rather than a promise about callers — but only
// over the exits this type controls, and the first version of this comment said "a
// property of the type" with no such limit.
//
// ⚠️ AND THE REPLACEMENT QUALIFIER GAVE A CLOSED COUNT — "the eight enumerated at
// Step and Close" — WHICH WAS ALSO WRONG, in the same shape as B4's two tens
// (corrected 2026-08-21, second audit). MEASURED instead of counted from memory:
// `grep -n "st.retireLocked(" session.go` reports ELEVEN call sites. Two of the
// reasons were outside the "eight": finishLocked's "the deadline passed WHILE the
// step ran" — which its own comment names as distinct from lazy expiry — and Begin's
// failure to build the very first command, which is in neither Step nor Close.
//
// 🔴 AND THE REPLACEMENT WAS *ALSO* A CLOSED COUNT — "exactly the eleven call sites"
// — WHICH IS THE THIRD TIME THIS DEFECT HAS APPEARED IN THIS PACKAGE (after B4's two
// tens and Close's three siblings). It was wrong the same way: it did not count
// Close's timeout residue, which is a path this type controls too. So the number is
// no longer prose at all — retireCallSites below is an inventory and
// TestSession_TheRetireCallSitesAreInventoried counts them with go/ast. If a closed
// count is worth writing, the counting is worth mechanising; otherwise it should not
// be written. (retireCallSites lives in session_test.go, not in this file — an
// earlier version of this sentence said "below", which sent a reader of session.go
// looking for something that is not here.)
//
// What the custody rule does NOT cover, named rather than implied: SIGKILL, an OS
// core dump, the runtime paging a keyring to swap, an owner goroutine that never
// returns (see Close), and anything a caller does with the bytes Step hands back.
type Store struct {
	mu       sync.Mutex
	live     map[ID]*Session
	perActor map[string]int
	// perTenant bounds one BUSINESS's share of a store every business shares. See
	// Begin's third gate for the exhaustion it refuses and for what it does not refuse.
	perTenant map[uuid.UUID]int
	perUID    map[string]ID
	closed    bool

	rows    Rows
	wrapper Wrapper
	baseURL string

	clock        Clock
	ttl          time.Duration
	maxPerActor  int
	maxPerTenant int
	maxLive      int

	closeGrace  time.Duration
	drain       chan struct{}
	stopSweeper func()
	quit        chan struct{}
	sweeperDone chan struct{}
}

// NewStore builds a store and STARTS ITS SWEEPER.
//
// 🔴 THE SWEEPER IS NOT OPTIONAL AND THERE ARE TWO OF IT — ADR 0017 §6 md. 7
// item 2 asks who reaps an expired session, and the honest answer is "both, and
// neither alone is enough":
//
//   - LAZY expiry, in checkout: a session past its deadline is never handed back,
//     so no expired session can be resumed even in the gap between two ticks.
//     Alone it is USELESS for the case that matters — an ABANDONED session is by
//     definition one nobody looks up again, so lazy expiry never fires for it and
//     its plain plaque key sits in memory until the process dies. ADR 0017 §6
//     md. 7 rejects "the process will die eventually" in those words.
//   - The SWEEPER goroutine, here: it wipes on a timer whether or not anyone asks.
//     Alone it leaves a window of up to one tick in which an expired session is
//     still in the map, and checkout would hand it out.
//
// Close stops the goroutine and wipes whatever is still live.
func NewStore(cfg Config) (*Store, error) {
	if cfg.Rows == nil {
		return nil, errors.New("encode: no Rows port")
	}
	if cfg.Wrapper == nil {
		return nil, errors.New("encode: no Wrapper port")
	}
	if cfg.BaseURL == "" {
		return nil, errors.New("encode: no base URL for the tap template")
	}
	// Built once, at construction, so a bad host is a startup failure rather than a
	// failure in the middle of a round with the row already written.
	if _, err := sun.BuildTapNDEF(cfg.BaseURL); err != nil {
		return nil, fmt.Errorf("encode: base URL: %w", err)
	}

	st := &Store{
		live:         map[ID]*Session{},
		perActor:     map[string]int{},
		perTenant:    map[uuid.UUID]int{},
		perUID:       map[string]ID{},
		rows:         cfg.Rows,
		wrapper:      cfg.Wrapper,
		baseURL:      cfg.BaseURL,
		clock:        cfg.Clock,
		ttl:          cfg.TTL,
		maxPerActor:  cfg.MaxPerActor,
		maxPerTenant: cfg.MaxPerTenant,
		maxLive:      cfg.MaxLive,
		closeGrace:   cfg.CloseGrace,
		drain:        make(chan struct{}, 1),
		quit:         make(chan struct{}),
		sweeperDone:  make(chan struct{}),
	}
	if st.closeGrace <= 0 {
		st.closeGrace = DefaultCloseGrace
	}
	if st.clock == nil {
		st.clock = systemClock{}
	}
	if st.ttl <= 0 {
		st.ttl = DefaultTTL
	}
	if st.maxPerActor <= 0 {
		st.maxPerActor = DefaultMaxPerActor
	}
	if st.maxLive <= 0 {
		st.maxLive = DefaultMaxLive
	}
	if st.maxPerTenant <= 0 {
		st.maxPerTenant = DefaultMaxPerTenant
	}

	period := st.ttl / sweepDivisor
	if period <= 0 {
		period = time.Millisecond
	}
	ticks, stop := st.clock.NewTicker(period)
	st.stopSweeper = stop
	go st.sweepLoop(ticks)
	return st, nil
}

// sweepLoop runs until Close.
//
// ⚠️ THE quit CHANNEL IS NOT DECORATION AND ITS ABSENCE WAS A LEAK. A plain
// `for range ticks` never returns when the ticker is merely STOPPED — time.Ticker.Stop
// does not close its channel — so every Store built in a long-lived process would
// have left a goroutine parked forever, holding this store, its map and whatever
// was in it.
func (st *Store) sweepLoop(ticks <-chan time.Time) {
	defer close(st.sweeperDone)
	for {
		select {
		case <-st.quit:
			return
		case _, ok := <-ticks:
			if !ok {
				return
			}
			st.mu.Lock()
			if st.closed {
				st.mu.Unlock()
				return
			}
			st.sweepLocked()
			st.mu.Unlock()
		}
	}
}

// sweepLocked retires every expired session that is not currently mid-step.
//
// A BUSY session is skipped rather than retired under the goroutine that is using
// it: the step in flight owns the keyring, and pulling it out from underneath
// would be a data race and an unclear error. Its own completion path checks the
// deadline (see finishLocked), so a session that expires while a step runs is
// retired the moment that step returns — one step later, never never.
func (st *Store) sweepLocked() {
	now := st.clock.Now()
	for _, s := range st.live {
		if s.busy {
			continue
		}
		if !now.Before(s.deadline) {
			st.retireLocked(s)
		}
	}
}

// expireLocked brings a session's deadline forward to now, and NEVER pushes it out.
//
// 🔴 IT EXISTS BECAUSE "EVERY WRITE MOVES IT EARLIER" WAS A CLAIM ABOUT THREE
// SCATTERED ASSIGNMENTS, AND MEASUREMENT BROKE IT TWICE (fifth audit, 2026-08-21).
// An audit found Close's write unguarded — on a session already past its budget it
// moved the deadline a full TTL LATER. Guarding that one site then exposed a fourth:
// the test written for the audit's finding immediately failed on checkout's
// cancellation branch, which had the same shape. Patching sites was losing to
// counting sites, which is this package's recurring defect.
//
// So the rule is one function and every writer goes through it. DefaultTTL's comment
// can then state the property instead of enumerating call sites, and
// TestSession_EveryWriterOfTheDeadlineMovesItEarlier drives all three.
//
// Must be called with st.mu held.
func (st *Store) expireLocked(s *Session) {
	if now := st.clock.Now(); now.Before(s.deadline) {
		s.deadline = now
	}
}

// retireLocked is THE ONE EXIT. Every path out of a session — success, error,
// timeout, cancellation, abort, shutdown — ends here, and this is the only caller
// of keyring.zeroAll in the package (TestSession_TheRingIsWipedFromExactlyOnePlace).
//
// It must be called with st.mu held.
func (st *Store) retireLocked(s *Session) {
	if s == nil {
		return
	}
	s.ring.zeroAll()
	s.auth = nil
	s.finished = true
	// 🔴 CLEARED AND SIGNALLED HERE SO EVERY RETIRE PATH IS THE SAME SHAPE (security
	// audit L1, 2026-08-21). The panic branch in Step retires without going through
	// finishLocked, so it left s.busy TRUE on a session that was already gone: a
	// concurrent Close then waited its whole grace for a drain signal that never came
	// and returned "0 encode session(s) were still mid-step … NOT wiped" — a §4.7
	// residue alarm raised over nothing, when everything had in fact been wiped.
	s.busy = false
	st.signalDrainLocked()

	delete(st.live, s.id)
	if n := st.perActor[s.actor]; n <= 1 {
		delete(st.perActor, s.actor)
	} else {
		st.perActor[s.actor] = n - 1
	}
	// The tenant counter is released the same way and in the same place, so "every
	// exit releases every counter" stays a property of THIS function rather than a
	// second thing to remember. A counter that leaked here would refuse a tenant for
	// ever, which is worse than the exhaustion it exists to prevent.
	if n := st.perTenant[s.tenantID]; n <= 1 {
		delete(st.perTenant, s.tenantID)
	} else {
		st.perTenant[s.tenantID] = n - 1
	}
	if s.uidHex != "" {
		if id, ok := st.perUID[s.uidHex]; ok && id == s.id {
			delete(st.perUID, s.uidHex)
		}
	}
}

// signalDrainLocked nudges a waiting Close that a session has just gone idle. The
// channel is buffered so a signal raised between Close's check and its select is
// not lost. Must be called with st.mu held.
func (st *Store) signalDrainLocked() {
	select {
	case st.drain <- struct{}{}:
	default:
	}
}

// retireIdleLocked retires every live session that is not mid-step, EXPIRES the ones
// that are, and returns how many were busy. Must be called with st.mu held.
//
// 🔴 THE EXPIRY IS THE WHOLE FIX FOR THE HOLE THIS FUNCTION USED TO HAVE, AND IT IS
// ONE LINE (security audit, 2026-08-21). The busy branch previously did nothing but
// count and continue, so a session that outlived Close's grace was never wiped —
// not then, and NOT WHEN ITS OWNER CAME BACK EITHER, because the only remaining
// retire reason for a busy session is finishLocked's deadline check and nothing had
// moved the deadline. Measured on the defect, with a real round held inside a
// blocking Wrapper: Close timed out, the owner's Step then SUCCEEDED, and the plain
// AES-128 plaque key was still in memory with the sweeper already dead — after ten
// times the TTL. No path inside the package could reach it.
//
// 🔴 AND THE DILEMMA THE OLD Close COMMENT ARGUED FROM WAS FALSE. It framed the
// choice as "wipe under a live owner" versus "never wipe", and this file already
// uses the third option in two other places: Abort and checkout's ctx branch both
// bring a busy session's deadline forward to NOW and leave the wipe to the owner's
// own finishLocked. Doing the same here means no wipe ever races an owner AND no
// session escapes — which is what those two call sites were already demonstrating.
func (st *Store) retireIdleLocked() int {
	busy := 0
	for _, s := range st.live {
		if s.busy {
			busy++
			// Same move as Abort and checkout's ctx branch: expire it, let its owner
			// retire it on the way out.
			//
			// 🔴 GUARDED SO IT ONLY EVER MOVES THE DEADLINE EARLIER (fifth audit,
			// 2026-08-21). Unguarded, this was a THIRD writer of s.deadline and it
			// could move it LATER: measured on a session whose budget had expired a
			// minute earlier, Close pushed the deadline forward by exactly one TTL.
			// The key-lifetime consequence was small — only at shutdown, and the
			// owner's finishLocked still retired it — but DefaultTTL's own comment
			// claims the deadline "is never extended; the only writes to it move it
			// EARLIER", and that sentence named two writers when there were three.
			// The guard makes the sentence true instead of narrowing it, which is the
			// better of the two repairs.
			st.expireLocked(s)
			continue
		}
		st.retireLocked(s)
	}
	return busy
}

// Close stops the sweeper and retires every session it can.
//
// This is the PROCESS SHUTDOWN exit path of ADR 0017 §6 md. 7 item 3, and it is a
// real one rather than a formality: "the process is going away so the memory goes
// with it" is exactly the answer the ADR refuses.
//
// 🔴 IT WAITS, AND THE WAIT IS A DECISION RATHER THAN A COPY OF ITS SIBLINGS
// (2026-08-21, third audit). Close is the FOURTH place that has to answer "what
// about a session that is mid-step", and an earlier version was the only one that
// did not: sweepLocked skips a busy session, Abort only brings its deadline forward,
// checkout's ctx branch defers to the owner, and checkout's expiry branch was fixed
// to do the same. Close wiped unconditionally.
//
// MEASURED, on the defect: with a session busy, all five of its key buffers were
// zeroed — and end to end, with a Wrapper that takes real time, the wipe landed
// INSIDE sun.Wrap, so the row committed an aes_key_ref that unwraps to SIXTEEN ZERO
// BYTES. Nothing refuses that: sun.Wrap has no zero check, InsertUnassigned has
// none, and the round's only all-zero gate is in sun.ChangeKeyData, which runs three
// exchanges LATER. ADR 0003 md. 3 — "per-plaque random, derived from nothing" —
// defeated silently and permanently, in an append-only column the application role
// may never rewrite.
//
// WHY THE SIBLINGS' ANSWER DOES NOT TRANSFER: they can say "skip it, the owner will
// finish" because an owner exists and will reach finishLocked. At shutdown the owner
// is going away too, so "skip" would mean the keys die with the process — which is
// the answer §6 md. 7 rejects.
//
// THE DECISION, in three parts:
//
//  1. Retire every IDLE session immediately. That is the common case and it is
//     unambiguous.
//  2. EXPIRE every session that is mid-step, by bringing its deadline forward to now
//     — the same move Abort and checkout's ctx branch already make. Nothing is wiped
//     under a live owner, and the owner's own finishLocked retires it the moment it
//     returns. This is what makes the guarantee hold WITHOUT racing anybody.
//  3. WAIT, up to CloseGrace, for those owners to come back, so that in the normal
//     case Close returns with everything already wiped.
//
// 🔴 THE THIRD PART USED TO BE SOMETHING ELSE AND IT WAS A HOLE — corrected after a
// security audit measured it (2026-08-21). It said: if the grace expires, do not wipe
// at all, and return an error. That framing rested on a FALSE DILEMMA ("wipe under a
// live owner" versus "never wipe") which this very file already refutes twice, and
// its consequence was worse than the thing it avoided: because step 2 did not exist,
// the session was never wiped AT ALL — not at timeout, and not when its owner
// returned, since the only remaining retire reason for a busy session is a deadline
// that nothing had moved. The sweeper is dead by then, so no path inside the package
// could reach it. Measured: a plain AES-128 plaque key still in memory after ten
// times the TTL.
//
// ⚠️ WHAT THE TIMEOUT NOW MEANS, at its own size: not "these keys are abandoned" but
// "these keys are not wiped YET". Every timed-out session is expired, so it is wiped
// by its own owner on the way out, and a LATER Close sweeps up any that have since
// gone idle.
//
// ⚠️ THE RESIDUE IS ONE ITEM AND IT IS NAMED, and the previous version of this
// paragraph said "one item" while there were two — the second being a repeat Close
// returning nil over a live session, which is now fixed rather than documented. What
// remains: an owner that NEVER returns — a port call blocked for ever — keeps its
// ring alive, exactly as any permanently stuck goroutine keeps whatever it holds.
// That is the limit of ADR 0017 §6 md. 7 item 3 on this path; it is not bounded by
// CloseGrace or by anything else this package controls.
//
// ⚠️ AND THE SENTENCE THAT USED TO END THIS COMMENT WAS FALSE — "when Close returns
// … nothing else in this process holds a session". Measured: after Close returned,
// the owning goroutine was still inside WrapKey. That is now true only when Close
// returns nil.
//
// The grace is real wall-clock rather than the injected Clock, deliberately: it
// bounds a SHUTDOWN, not a session lifetime, and a shutdown that waited on a fake
// clock would hang in production. Tests that need the timeout branch set a tiny
// CloseGrace.
func (st *Store) Close() error {
	st.mu.Lock()
	// 🔴 THE EARLY RETURN THAT USED TO BE HERE MADE A SECOND Close LIE, AND THE LIE
	// WAS ROUTINE RATHER THAN EXOTIC (fourth audit, 2026-08-21). It was
	// `if st.closed { return nil }`. Measured: Close #1 times out and reports its
	// residue; Close #2 returns nil while the session is still LIVE, still busy, and
	// its ring still holds an unwiped plain plaque key. And every test in this package
	// double-closes, because newHarness registers an unconditional t.Cleanup — so the
	// tests that call Close explicitly were all getting the nil.
	//
	// Only the one-shot TEARDOWN is guarded now; the retire-and-drain half runs on
	// EVERY call. That makes Close idempotent AND honest: a later call sweeps up
	// whatever the earlier one left expired, and its nil means what it says.
	first := !st.closed
	st.closed = true
	busy := st.retireIdleLocked()
	st.mu.Unlock()

	if first {
		close(st.quit)
		st.stopSweeper()
		<-st.sweeperDone
	}

	if busy == 0 {
		return nil
	}

	timer := time.NewTimer(st.closeGrace)
	defer timer.Stop()
	for {
		select {
		case <-st.drain:
			st.mu.Lock()
			busy = st.retireIdleLocked()
			st.mu.Unlock()
			if busy == 0 {
				return nil
			}
		case <-timer.C:
			// 🔴 THE COUNT IS RE-DERIVED FROM retireIdleLocked, NOT FROM len(st.live),
			// AND THAT CLOSES A FALSE-ALARM WINDOW BY CONSTRUCTION (fourth audit, N4).
			// Go picks randomly when both this timer and the drain signal are ready, so
			// reading len(st.live) here counts whatever is in the map at that instant
			// rather than what is BUSY.
			//
			// ⚠️ THE MECHANISM THIS COMMENT FIRST NAMED CANNOT HAPPEN, AND SAYING SO IS
			// THE POINT (fifth audit, 2026-08-21). It said len(st.live) "could report
			// sessions that had ALREADY been retired" — it cannot: retireLocked does
			// delete(st.live, s.id), so a retired session is not in the map to be
			// counted. What len(st.live) could over-count is an IDLE-but-unswept
			// session, and after st.closed that is unreachable under st.mu. So the
			// hardening below is correct and the story attached to it was not. Re-running the sweep means the number
			// is BUSY sessions at this instant, and if that is zero the shutdown
			// succeeded and says so.
			//
			// ⚠️ MEASURED RATHER THAN ASSUMED, IN BOTH DIRECTIONS. The audit that
			// raised this could not reproduce the window and labelled it UNVERIFIED —
			// a code reading, not an observation. Against the fixed code a probe drove
			// this branch 400 times with a drain signal released one millisecond into a
			// one-millisecond grace: ZERO false "0 encode session(s)" alarms. That is
			// not proof the window never existed; it is why the count is derived from a
			// fresh sweep rather than from len(st.live), which removes it by
			// construction instead of by luck.
			st.mu.Lock()
			stuck := st.retireIdleLocked()
			st.mu.Unlock()
			if stuck == 0 {
				return nil
			}
			// A count and a duration, never an identifier and never a byte (§4.7).
			//
			// The message says "not wiped YET" because that is now true: step 2
			// expired every one of these, so each is wiped by its own owner when the
			// in-flight step returns. An earlier version said "deliberately NOT
			// wiped", which described the hole rather than the design.
			//
			// ⚠️ AND THE MESSAGE IS TERSE ON PURPOSE. A first version explained the
			// whole decision here and named the column it protects; that spelling
			// tripped scripts/redline-check.sh's R7 (the `aes_?key` trigger), which
			// was a FALSE POSITIVE — the arguments are an int and a Duration — but the
			// right answer was not an exemption. An error is not the place for a
			// paragraph, the reasoning already lives in this function's doc comment,
			// and a §4.7 scanner that has to be argued with is a scanner that gets
			// argued with again.
			return fmt.Errorf("encode: %d encode session(s) were still mid-step after %v; "+
				"they are expired and are wiped by their own owner when it returns — see Store.Close",
				stuck, st.closeGrace)
		}
	}
}

// checkout takes a session out for one step: it enforces closure, existence, the
// deadline and exclusivity, all under one lock.
func (st *Store) checkout(ctx context.Context, id ID) (*Session, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.closed {
		return nil, ErrStoreClosed
	}
	s, ok := st.live[id]
	if !ok {
		return nil, ErrUnknownSession
	}
	if err := ctx.Err(); err != nil {
		// CANCELLATION IS AN EXIT PATH, not a refusal. The ctx check is deliberately
		// AFTER the lookup rather than before it: checking first would return early
		// and leave the session — and its plain plaque key — alive until the TTL,
		// which is the "abandoned session" ADR 0017 §6 md. 7 is about. A cancelled
		// caller is not coming back, so the round ends here.
		if s.busy {
			// Another goroutine owns the ring; its own finishLocked will see this.
			st.expireLocked(s)
		} else {
			st.retireLocked(s)
		}
		return nil, fmt.Errorf("encode: %w", err)
	}
	if !st.clock.Now().Before(s.deadline) {
		// 🔴 THE BUSY CHECK COMES FIRST, AND ITS ABSENCE WAS A RACE AND A KEY-LOSS
		// PATH (found by audit, 2026-08-21). This branch used to retire
		// unconditionally, while its three siblings all guard: sweepLocked skips a
		// busy session, Abort only brings the deadline forward, and the ctx branch
		// above does the same. Only the expiry branch wiped under the owner.
		//
		// Measured on the defect: all five key buffers of a busy session — including
		// K_SDMFileRead — were zeroed while the first step was still inside advance,
		// which runs OUTSIDE st.mu. So this is a DATA RACE on live key material, and
		// it released the perUID slot mid-round, which is the interleaved-CmdCtr mode
		// the per-plaque limit exists to prevent. Both are real.
		//
		// ⚠️ AND THE CONSEQUENCE THIS COMMENT NAMED WAS TOO STRONG — NARROWED
		// 2026-08-21 AFTER A THIRD AUDIT MEASURED IT. It said the wipe "installs a
		// zeroed key on the chip while the row keeps the real one". Not reachable
		// HERE, and program order is why: zeroAll walks keyInventory in order, so
		// K_SDMFileRead (slot 5) is wiped LAST, while EV2ChangeKeyCommand reads the
		// plaque key FIRST (ChangeKeyData) and the session keys after
		// (EV2WrapCommandFull). For the chip to adopt a zero key the wipe would have
		// to reach slot 5 before slots 1-2, which that order forbids. Both concrete
		// attempts are fail-closed anyway: a fully zeroed key is refused by
		// sun.ChangeKeyData ("refusing to install the all-zero transport key"), and a
		// torn key with wiped session keys makes the chip answer 911E.
		//
		// The place where a wipe under a live owner IS measurably catastrophic is
		// Close — see there: it lands inside sun.Wrap and commits an all-zero
		// aes_key_ref. Same class, different call site, and only that one reaches the
		// persistent consequence.
		//
		// The session is still doomed — the in-flight step's own finishLocked sees
		// the same deadline and retires it one step later — so the caller is told the
		// same thing either way.
		if s.busy {
			return nil, ErrUnknownSession
		}
		// Lazy expiry. The session dies here rather than being handed back, and
		// ErrUnknownSession rather than a distinct "expired" keeps the handle from
		// becoming an oracle.
		st.retireLocked(s)
		return nil, ErrUnknownSession
	}
	if s.busy {
		return nil, ErrBusy
	}
	s.busy = true
	return s, nil
}

// finishLocked returns a checked-out session, retiring it if this step was its
// last — for ANY of the four reasons a step can be last.
//
// It returns a non-nil error only for the reason the caller has not been told about
// by any other means: the deadline passing WHILE the step ran. That case has to be
// reported, because advance has by then already built the NEXT command, and handing
// a live-looking C-APDU back for a session that no longer exists would send the
// operator to the chip for an exchange whose answer nothing can accept.
func (st *Store) finishLocked(s *Session, done bool, stepErr error, ctxErr error) error {
	s.busy = false
	st.signalDrainLocked()
	switch {
	case stepErr != nil:
		// FAIL-CLOSED, and it is the chip's rule rather than a preference:
		// NT4H2421Gx rev. 3.0 §9.1.10 — on any error "the authentication state is
		// immediately lost". There is no session left on the chip to continue, so
		// keeping ours would keep keys alive for nothing. Recovery is ADR 0017
		// §5.3's three probes, not a retry.
		st.retireLocked(s)
	case done:
		st.retireLocked(s)
	case ctxErr != nil:
		st.retireLocked(s)
	case !st.clock.Now().Before(s.deadline):
		// Expired WHILE the step ran; the sweeper skipped it because it was busy.
		st.retireLocked(s)
		return ErrUnknownSession
	}
	return nil
}

// Begin opens a round and returns its handle and the first C-APDU — ADR 0017 §5.1
// step 1, the ISO SELECT.
//
// tenantID is the business the plaque is being loaded for. It is REQUIRED and it
// is carried, unchanged, to both Rows methods — see Rows for why the port takes it
// explicitly rather than inheriting it from a connection setting.
//
// actor identifies whoever is driving; it bounds how many sessions one operator
// may hold. ⚠️ NEITHER ARGUMENT IS AN AUTHORISATION DECISION. ADR 0017 §6 md. 10
// ("who may encode for which tenant") is still open and this round does not close
// it: a caller supplies whatever string and whatever tenant id it likes. What
// actor bounds is exposure; what tenantID buys is that the value is NAMED rather
// than inherited.
//
// ⚠️ AND A SENTENCE THAT USED TO END THIS PARAGRAPH WAS SCHEMA-WRONG — "the row
// lands in whatever tenant app.tenant_id names". Measured against migration 00004
// and the live schema: tags.tenant_id is NOT NULL with no DEFAULT, so a row does not
// "land" in a tenant by omission; an INSERT that supplies none FAILS. That is the
// measurement this signature is built on.
func (st *Store) Begin(ctx context.Context, tenantID, adminID uuid.UUID, actor string) (ID, Progress, error) {
	if err := ctx.Err(); err != nil {
		return "", Progress{}, fmt.Errorf("encode: %w", err)
	}
	if tenantID == uuid.Nil {
		// The nil UUID is a valid uuid literal, so passing it would scope the row to
		// a tenant that does not exist and fail at the FK — deep inside the round,
		// after the chip has been selected and read. db.WithTenant refuses it for the
		// same reason; refusing it HERE means the round never opens.
		return "", Progress{}, errors.New("encode: an encode round needs a tenant")
	}
	if actor == "" {
		// An empty actor would share one counter with every other empty actor, which
		// is a limit that limits nothing.
		return "", Progress{}, errors.New("encode: an encode round needs an actor")
	}
	if len(actor) > MaxActorLen {
		// 🔴 BOUNDED BECAUSE IT IS PERSISTED NOW, WHICH IT WAS NOT IN B2c-1. The
		// label is copied into audit_log.detail (see rows.go), and audit_log is
		// append-only for every role including tappa_owner (00005's trigger) — so an
		// unbounded caller-supplied string would be an unbounded, unremovable write.
		// CLAUDE.md §7 puts input validation at the boundary; this is the boundary.
		// Refusing is preferred to truncating: a truncated label is a value nobody
		// supplied.
		return "", Progress{}, fmt.Errorf("encode: the actor label is %d bytes, at most %d are accepted", len(actor), MaxActorLen)
	}

	id, err := newID()
	if err != nil {
		return "", Progress{}, err
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	if st.closed {
		return "", Progress{}, ErrStoreClosed
	}
	// Sweep first so the caps are measured against LIVE sessions, not against
	// expired ones that merely have not been reaped yet. Without this an operator
	// whose last three reads dropped would be refused for a further TTL.
	st.sweepLocked()

	if len(st.live) >= st.maxLive {
		return "", Progress{}, ErrTooManySessions
	}
	if st.perActor[actor] >= st.maxPerActor {
		return "", Progress{}, ErrTooManySessions
	}
	// 🔴 THE THIRD GATE, AND IT IS THE ONE THIS STORE SHIPPED WITHOUT (security audit,
	// 2026-08-24). The other two bound an ACTOR and the STORE; neither bounds a
	// BUSINESS, and the store is one process-wide object shared by every tenant. So
	// one tenant could hold every slot the product has: with per-actor 3, twenty-two
	// admin ids reach 66 ≥ DefaultMaxLive, and from that moment EVERY tenant's Begin
	// answers ErrTooManySessions — including the operator standing at a wall with a
	// plaque in their hand, who gets a 409 and no screen that explains it.
	//
	// ⚠️ THE LIMITS WERE CHOSEN IN B2c-1 WHEN NOTHING CALLED THIS PACKAGE. What made
	// them reachable is the HTTP endpoint, so the multi-tenant axis belongs to the
	// round that opened it. ADR 0017 §6 md. 7 settled three numbers without weighing
	// this one.
	if st.perTenant[tenantID] >= st.maxPerTenant {
		return "", Progress{}, ErrTooManySessions
	}

	now := st.clock.Now()
	s := &Session{
		id:       id,
		actor:    actor,
		tenantID: tenantID,
		adminID:  adminID,
		deadline: now.Add(st.ttl),
		ring:     newKeyring(),
	}
	st.live[id] = s
	st.perActor[actor]++
	st.perTenant[tenantID]++

	cmd, err := roundSteps[0].command(ctx, st, s)
	if err != nil {
		st.retireLocked(s)
		return "", Progress{}, err
	}
	return id, Progress{Command: cmd, Step: "begin"}, nil
}

// Step feeds one R-APDU in and returns the next C-APDU.
//
// The whole state machine is driver.go's roundSteps table; this function is only
// the custody: check out, advance, hand back, and retire on any of the four ways a
// round can end here.
func (st *Store) Step(ctx context.Context, id ID, rapdu []byte) (Progress, error) {
	s, err := st.checkout(ctx, id)
	if err != nil {
		return Progress{}, err
	}

	var (
		p       Progress
		stepErr error
	)
	func() {
		// 🔴 THE EIGHTH EXIT PATH, AND IT IS A PANIC. Found by audit (2026-08-21),
		// and it is worse than the abandoned session ADR 0017 §6 md. 7 is written
		// about. Without this defer, a panic inside advance skips finishLocked and
		// leaves s.busy TRUE for ever: sweepLocked deliberately skips a busy session
		// (it cannot wipe a ring another goroutine owns) and Abort only brings the
		// deadline forward, so the session, its plain plaque key and its perUID
		// slot survive until Close. In a serving process behind a recovering HTTP
		// middleware, "until Close" means indefinitely — precisely the "the process
		// will die eventually" answer §6 md. 7 rejects.
		//
		// The panic is RE-RAISED, not swallowed: this is a wipe, not a recovery. The
		// process's own middleware still decides what a panic means.
		//
		// ⚠️ MEASURED HONESTLY: the audit could NOT produce a real panic from
		// relay-controlled bytes — internal/sun's length gates hold — so this closes
		// a path nothing is known to reach, not a live defect.
		defer func() {
			if r := recover(); r != nil {
				st.mu.Lock()
				st.retireLocked(s)
				st.mu.Unlock()
				panic(r)
			}
		}()
		p, stepErr = s.advance(ctx, st, rapdu)
	}()
	ctxErr := ctx.Err()

	st.mu.Lock()
	lateErr := st.finishLocked(s, p.Done, stepErr, ctxErr)
	st.mu.Unlock()

	if stepErr != nil {
		return Progress{}, stepErr
	}

	// 🔴 Done IS CHECKED BEFORE ctxErr AND lateErr, AND THE ORDER USED TO BE THE OTHER
	// WAY ROUND — WHICH BROKE Progress.Done's OWN RULE (eighth audit, 2026-08-21).
	//
	// Measured on the defect: cancel the context during the TENTH exchange (a clock
	// that cancels inside checkout's Now(), because checkout reads ctx.Err() BEFORE it
	// reads the clock) and advance still completes the round — key 01 installed, NDEF
	// written, file settings changed — while Step returned Progress{} with Done FALSE
	// and "context canceled", and MarkEncoded was never called at all.
	//
	// That is exactly what Progress.Done forbids two hundred lines above: "READ Done
	// BEFORE READING THE ERROR … the round SUCCEEDED on silicon and must NOT be
	// re-run."
	//
	// ⚠️ THIS FIX NEEDS NO PRECEDENT, AND TWO ATTEMPTS TO GIVE IT ONE WERE BOTH WRONG.
	//
	// The first said finishLocked "already orders it correctly", so the two ladders
	// disagreed. Measured: swapping those two cases leaves the suite green — both
	// bodies are the same statement. The replacement then said "the only load-bearing
	// order in that switch is `case done:` against the deadline case". Measured too,
	// and also false: moving `case done:` below the deadline case is green as well.
	//
	// 🔴 THE MEASURED TRUTH IS THAT NO ORDER IN THAT SWITCH IS LOAD-BEARING, and the
	// structure says why: finishLocked has exactly ONE caller — Step — its return is
	// bound to lateErr, and Step returns through markEncoded WITHOUT READING lateErr
	// whenever p.Done is true. So when done is true the switch's return is
	// unobservable, and three of its four arms run the same single statement (the
	// fourth, the deadline arm, additionally returns ErrUnknownSession — and THAT arm
	// is load-bearing: deleting it turns the suite red).
	//
	// ⚠️ NO grep IS PUBLISHED HERE, DELIBERATELY. An earlier version printed
	// `grep -rn "finishLocked(" internal/encode/` → one, and the command returns THREE
	// lines: the declaration, the one real call, and the sentence quoting itself. The
	// claim was right and the evidence offered for it was not — the same self-counting
	// trap this file records one paragraph earlier for a different sweep.
	//
	// ⚠️ AND THERE IS NO CLEAN COMMAND TO SUBSTITUTE, WHICH IS WORTH SAYING RATHER THAN
	// WORKING AROUND: any textual search for the identifier also matches THIS comment,
	// including the narrower one. A prose file that discusses a symbol cannot be
	// grepped for that symbol and get a count of the code. The count here was
	// established by reading the file, and a reader checking it should do the same.
	//
	// The fix stands on Progress.Done's own rule, which is sufficient on its own, and
	// on M71 — reverting this ladder turns TestStore_ACancellationOnTheLastExchangeStillReportsDone
	// red. Reaching for a symmetry elsewhere is what produced two false sentences in
	// two consecutive rounds; a correct conclusion does not need a borrowed reason.
	//
	// 🔴 THE COST IS THE CHAIN F3 DOCUMENTS, END TO END. A caller told "not done"
	// re-runs; the re-run dies at the row insert with "duplicate key value violates
	// unique constraint"; that reads like stale inventory; the obvious fix is deleting
	// the row; the row holds the only stored copy of the plaque key. And the trigger is
	// ORDINARY for an HTTP relay: the phone posts the last R-APDU and the request
	// context dies.
	//
	// 🔴 THE MARKER NO LONGER FAILS HERE, AND TWO SENTENCES THAT SAID OTHERWISE ARE
	// RETRACTED (ninth audit, 2026-08-24). They read: "the marker below still runs on a
	// cancelled context, it will usually fail there, which is correct" and "it is fine
	// for the marker to fail here". Both were true of the shipped code and both were
	// arguing for the wrong thing — that the round should FILE the damage rather than
	// avoid it.
	//
	// markEncoded now runs on a context detached from the request, so the ordinary
	// hang-up is REPAIRED: encoded_at is stamped, no plaque.unmarked row is written,
	// and the round ends in the state the chip is actually in. Measured, cancelled ctx:
	// err=false, encoded_at set, unmarked=0.
	//
	// WHAT SURVIVES FROM THE OLD PARAGRAPH, because it was never about the marker: the
	// ladder below reads Done BEFORE any error, and that rule is the one thing standing
	// between a caller and the re-run chain described above. Done's own rule is
	// sufficient on its own; the marker's fate is no longer part of the argument.
	if p.Done {
		return st.markEncoded(ctx, s, p)
	}

	if ctxErr != nil {
		return Progress{}, fmt.Errorf("encode: %w", ctxErr)
	}
	if lateErr != nil {
		return Progress{}, lateErr
	}
	return p, nil
}

// markEncoded is ADR 0017 §5.1 step 9's second half, split out so Step's return
// ladder reads as the priority order it now is.
//
// The document's order is Zero(keys) FIRST — done by finishLocked — and only then
// mark the row. The marker is bookkeeping; the keys are not, so the keys go first
// even though it means the marker runs with no session left to hold.
func (st *Store) markEncoded(ctx context.Context, s *Session, p Progress) (Progress, error) {
	// 🔴 DETACHED FROM THE REQUEST, AND THIS IS A REPAIR RATHER THAN A RECORD OF DAMAGE
	// (ninth audit, 2026-08-24). The eighth round detached the trail entry that
	// DOCUMENTS a failed marking; the ninth observed that the same one-line mechanism,
	// at this call site, stops the failure from happening at all. Measured against real
	// Postgres, cancelled request context both ways:
	//
	//	SHIPPED (request ctx)  err=true   encoded_at=<nil>  plaque.unmarked=1
	//	DETACHED (this)        err=false  encoded_at=set    plaque.unmarked=0
	//
	// The trigger this repairs is the one the code above calls ORDINARY for an HTTP
	// relay: the phone posts the last R-APDU and hangs up. A round should not answer
	// that by filing damage it could simply have avoided.
	//
	// 🔴 WHY IT IS SAFE TO IGNORE THE CANCELLATION HERE, which is the question a
	// detached write always owes: cancellation normally means "the caller stopped
	// caring", and honouring it is right while work can still be abandoned cleanly.
	// Past Progress.Done nothing can be abandoned cleanly — the CHIP is already
	// personalised, irreversibly, outside the database. The only question left is
	// whether the database is allowed to catch up with a fact that is already true in
	// the physical world. tags.encoded_at is that fact's only record.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), DefaultRepairGrace)
	defer cancel()
	if err := st.rows.MarkEncoded(ctx, s.tenantID, s.adminID, s.uidHex, s.actor); err != nil {
		// Done stays TRUE. See Progress.Done: the chip is personalised and the round
		// must not be re-run.
		return p, fmt.Errorf("encode: the chip completed but the row for %s could not be marked; do NOT re-run this round: %w", s.uidHex, err)
	}
	return p, nil
}

// Abort ends a round on the caller's say-so — the operator cancelled, the phone
// went away, the UI navigated. It wipes exactly like every other exit.
//
// It is deliberately quiet about whether the ID was live: an unknown handle and a
// just-swept one produce the same nil, so Abort cannot be used to probe.
func (st *Store) Abort(id ID) {
	st.mu.Lock()
	defer st.mu.Unlock()
	s, ok := st.live[id]
	if !ok {
		return
	}
	if s.busy {
		// A step is running and owns the ring. Expiring the deadline makes its own
		// finishLocked retire it one step later; wiping under it would race.
		st.expireLocked(s)
		return
	}
	st.retireLocked(s)
}

// Live reports how many sessions are currently held. It exists for the operator
// health surface and for tests; it discloses a count and nothing else.
func (st *Store) Live() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return len(st.live)
}
