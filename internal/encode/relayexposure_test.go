package encode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ADR 0017 §2.1's CENTRAL SECURITY CLAIM, MEASURED — and until this file it never had
// been (M8-05 FAZ B2c-2b).
//
// The claim: "Telefon … hiçbir baytı ÜRETMEZ, hiçbirini YORUMLAMAZ" and, in §3, the
// plain key "asla: log'a, HTTP yanıtına, hata mesajına, dosyaya, TELEFONA" leaves the
// process. What actually crosses the process boundary on the encode path is exactly
// one thing: the C-APDU each step hands back in Progress.Command, which
// internal/handler hex-encodes into a response body and nothing else.
//
// 🔴 SO THE CLAIM IS FINITE AND THEREFORE TESTABLE, which is the only reason this file
// can exist. It is not "could a key leak somewhere" — that question has an unbounded
// answer space and B2c-2a lost five designs to it. It is: OVER A COMPLETE ROUND, DOES
// ANY BYTE STRING THIS PROCESS CONSIDERS SECRET APPEAR INSIDE ANY C-APDU. The secrets
// are enumerated by keyInventory (six slots, already ratcheted both ways by
// TestSession_TheKeyInventoryIsTheOneADR0017Lists), plus the wrapped envelope that
// reached the row. The commands are enumerated by the round itself.
//
// ⚠️ WHAT IT DOES NOT PROVE, AND §2.2 ALREADY SAYS SO. It does not prove the round is
// CONFIDENTIAL against somebody holding the APDU dump. On a blank chip the
// authentication key is the public factory default, so an observer can re-derive the
// session keys and decrypt the ChangeKey body — ADR 0017 §2.2 counts six ways to close
// that and all six fall. This test is about the PROCESS BOUNDARY: the bytes we hand
// the relay are sealed ones, not plain ones. Those are different claims and only the
// second is ours to keep.

// wireLog captures every C-APDU a round produced, in order.
type wireLog struct {
	commands [][]byte
}

func (w *wireLog) add(capdu []byte) {
	w.commands = append(w.commands, append([]byte(nil), capdu...))
}

// contains reports which captured command carries needle, or -1.
//
// 🔴 IT IS A SUBSTRING SEARCH RATHER THAN AN EQUALITY, deliberately: a key smuggled
// into a longer body is the shape that matters, and an equality test would miss every
// one of them.
func (w *wireLog) contains(needle []byte) int {
	for i, c := range w.commands {
		if bytes.Contains(c, needle) {
			return i
		}
	}
	return -1
}

// TestRelay_NoPlaintextKeyMaterialEverReachesTheWire is the test the paragraph above
// describes.
func TestRelay_NoPlaintextKeyMaterialEverReachesTheWire(t *testing.T) {
	h := newHarness(t)
	chip := newFakeChip(t)
	ctx := context.Background()

	wire := &wireLog{}

	id, p, err := h.st.Begin(ctx, testTenant, uuid.Nil, "operator-relay")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	wire.add(p.Command)

	// COPIES of the live key material, taken between exchanges. Copies matter twice
	// over: sun.Zero clears in place, so a reference would read as all-zero by the end
	// and every assertion below would be vacuous.
	secrets := map[string][]byte{}
	snapshot := func() {
		s := liveSession(t, h.st, id)
		for _, name := range s.ring.filled() {
			if _, done := secrets[name]; done {
				continue
			}
			b, err := s.ring.peek(name)
			if err != nil {
				t.Fatalf("peek %s: %v", name, err)
			}
			secrets[name] = append([]byte(nil), b...)
		}
	}

	for i := 0; ; i++ {
		if i > len(roundSteps)+2 {
			t.Fatalf("the round did not terminate after %d exchanges", i)
		}
		p, err = h.st.Step(ctx, id, chip.Transceive(p.Command))
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if p.Done {
			break
		}
		// Take the snapshot BEFORE the next command goes out, so every secret that
		// exists by then is in the set the command is checked against.
		snapshot()
		if p.Command == nil {
			t.Fatalf("step %d produced no command and did not finish", i)
		}
		wire.add(p.Command)
	}

	// --- anti-vacuity, both halves --------------------------------------------------

	if got := len(wire.commands); got != len(roundSteps) {
		t.Fatalf("captured %d C-APDU(s) for a %d-exchange round; the capture is incomplete "+
			"and every assertion below is weaker than it looks", got, len(roundSteps))
	}
	// The five slots that are filled during a shipped round. K_AppMaster is declared
	// and never filled (ADR 0017 §6 md. 5 blocks step 8), so requiring it would make
	// this test fail for the wrong reason — and NOT naming it would let the day it
	// starts being filled pass unnoticed. It is asserted ABSENT instead.
	var names []string
	for name := range secrets {
		names = append(names, name)
	}
	sort.Strings(names)
	want := []string{"KSesAuthENC", "KSesAuthMAC", "K_SDMFileRead", "RndA", "RndB"}
	if len(names) != len(want) {
		t.Fatalf("the round produced key material %v, want %v.\n"+
			"If K_AppMaster is in that list, ADR 0017 §5.1 step 8 has shipped and this test "+
			"is now checking one more secret — which is correct; update `want`.", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("the round produced key material %v, want %v", names, want)
		}
	}
	for name, b := range secrets {
		if len(b) == 0 || allZero(b) {
			t.Fatalf("the captured copy of %s is %d bytes and all-zero; the search below "+
				"would find it in any buffer of zeros and prove nothing", name, len(b))
		}
	}

	// --- the claim ------------------------------------------------------------------

	for _, name := range names {
		if at := wire.contains(secrets[name]); at >= 0 {
			t.Errorf("the plaintext %s (%d bytes) appears in C-APDU %d (%s). ADR 0017 §2.1: "+
				"the relay is handed SEALED bytes and nothing else — the plain key must not "+
				"cross this process boundary", name, len(secrets[name]), at, roundSteps[at].name)
		}
	}

	// The wrapped envelope is not key material, but it is the one thing that reaches
	// the ROW and it has no business on the wire either (ADR 0003 md. 4).
	wrapped, ok := h.rows.inserted[p.UIDHex]
	if !ok || len(wrapped) != 44 {
		t.Fatalf("no 44-byte envelope was stored for %s; the assertion below is vacuous", p.UIDHex)
	}
	if at := wire.contains(wrapped); at >= 0 {
		t.Errorf("the KEK envelope appears in C-APDU %d (%s)", at, roundSteps[at].name)
	}

	// --- THE POSITIVE CONTROL, and it is what makes the run above worth anything -----
	//
	// A search that cannot find a planted needle is a search that would report success
	// over an endpoint sending the key in the clear. Every secret is planted into a
	// synthetic command and must be found.
	//
	// ✅ AND THE ASSERTION ITSELF IS LIVE ON THE REAL WIRE, WHICH THIS TEST'S AUTHOR
	// UNDER-CLAIMED (corrected 2026-08-24 by an audit, in the SAFE direction for once).
	// The first version of this note said the assertion could not be reached by a
	// realistic mutation — that appending a key to a command breaks the fake chip's
	// length check first, so the test goes red "for the wrong reason", and the real
	// value here is only a ratchet. MEASURED, and false: an auditor overwrote a real
	// secret into a real captured frame WITHOUT changing its length and got
	//
	//	the plaintext K_SDMFileRead (16 bytes) appears in C-APDU 5 (authenticate.2)
	//
	// i.e. this assertion fires on its own, on the shipped capture, independently of
	// any length check. The claim is stronger than it was written down as. Both things
	// are true — a LENGTHENING mutation is caught earlier by the chip, and a
	// LENGTH-PRESERVING one is caught here — and only the second one needed saying.
	for _, name := range names {
		planted := &wireLog{}
		planted.add([]byte{0x90, 0xC4, 0x00, 0x00, byte(len(secrets[name]))})
		planted.add(append([]byte{0x90, 0xC4, 0x00, 0x00}, secrets[name]...))
		if at := planted.contains(secrets[name]); at != 1 {
			t.Fatalf("the search cannot find a PLANTED %s (got %d, want 1); every negative "+
				"result above is meaningless", name, at)
		}
	}
	if !t.Failed() {
		t.Logf("%d C-APDUs checked against %d secrets and one envelope; positive control passed",
			len(wire.commands), len(names))
	}
}

// TestRelay_TheOnlyBytesAStepHandsBackAreTheCommand pins the other half of the
// boundary: Progress carries exactly one byte slice.
//
// 🔴 A FIELD, NOT A VALUE. The test above walks the bytes a shipped round produces; if
// Progress grew a second []byte — a diagnostic dump, "the last response", a nonce for
// an operator screen — that walk would not see it, because it only inspects Command.
// This one refuses the SHAPE, so a new byte-carrying field is a red test before
// anybody has to reason about what it holds.
func TestRelay_TheOnlyBytesAStepHandsBackAreTheCommand(t *testing.T) {
	// Reflection over the struct, so the assertion is about the type rather than about
	// one instance.
	typ := reflect.TypeOf(Progress{})
	fields := map[string]string{}
	byteSlices := 0
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		fields[f.Name] = f.Type.String()
		if f.Type.Kind() != reflect.Slice {
			continue
		}
		byteSlices++
		if f.Name != "Command" {
			t.Errorf("Progress.%s is a slice (%s). ADR 0017 §2.1 gives the relay exactly "+
				"one thing — the sealed C-APDU — and a second byte-carrying field is a "+
				"second channel nobody has argued for", f.Name, f.Type)
		}
	}
	if byteSlices != 1 {
		t.Fatalf("Progress carries %d slice field(s), want exactly 1 (Command). fields=%v",
			byteSlices, fields)
	}

	// 🔴 AND THE FIELD SET ITSELF, BECAUSE THE COUNT WAS PUBLISHED IN THREE PLACES AND
	// BOUND TO NOTHING (audit, sixth round). "Progress carries three fields" drifted to
	// four without anything noticing, was corrected by hand in three copies, and a
	// FIFTH field (a plain string) still left internal/encode, internal/handler and
	// cmd/tappa all green. A number that three documents quote is a number a test owes.
	//
	// ⚠️ WHAT THIS DOES AND DOES NOT BUY, at its honest size: a new Progress field
	// cannot reach the wire on its own — encodeReply's field list is pinned separately
	// and the two writers build their own literal — so what escaped was the COUNT, not
	// a value. This turns the count red instead of stale.
	want := []string{"Command", "Done", "Step", "UIDHex"}
	var names []string
	for i := 0; i < typ.NumField(); i++ {
		names = append(names, typ.Field(i).Name)
	}
	sort.Strings(names)
	sorted := append([]string(nil), want...)
	sort.Strings(sorted)
	if !reflect.DeepEqual(names, sorted) {
		t.Fatalf("Progress has fields %v, want %v.\n"+
			"A new one is not automatically wrong — but the field count is quoted in "+
			"internal/handler/plaqueencode.go, ADR 0017 §6 md. 14 and the M8-05 card, and all "+
			"three have to be re-read when it moves", names, sorted)
	}
}

// TestDriver_TheRowIsWrittenOnTheExchangeThisNumberNames measures
// RequestsBeforeTheRowIsWritten against a real round instead of trusting its
// derivation.
//
// 🔴 IT EXISTS BECAUSE internal/handler SIZES A RATE LIMIT ON IT. ADR 0017 §6 md. 12's
// hazard is a squatted uid, and a uid is squatted the moment the row lands — six
// exchanges before the round ends. An audit named the figures that depended on this
// (55 rows per session, 750 per address) as bound to nothing; they are derived from
// this function now, and this test is what makes the function a measurement.
func TestDriver_TheRowIsWrittenOnTheExchangeThisNumberNames(t *testing.T) {
	h := newHarness(t)
	chip := newFakeChip(t)
	ctx := context.Background()

	id, p, err := h.st.Begin(ctx, testTenant, uuid.Nil, "operator-rowcount")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	requests := 1 // Begin is the first HTTP exchange.
	for i := 0; ; i++ {
		if i > len(roundSteps)+2 {
			t.Fatalf("the round did not write a row in %d exchanges", requests)
		}
		p, err = h.st.Step(ctx, id, chip.Transceive(p.Command))
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		requests++
		if len(h.rows.inserted) > 0 {
			break
		}
		if p.Done {
			t.Fatalf("the round finished without ever inserting a row; the assertion below " +
				"would be vacuous")
		}
	}
	if got := RequestsBeforeTheRowIsWritten(); got != requests {
		t.Fatalf("RequestsBeforeTheRowIsWritten() = %d, but a real round inserted its row on "+
			"exchange %d. internal/handler divides a rate limit by this; a wrong value quietly "+
			"loosens the only bound ADR 0017 §6 md. 12 has", got, requests)
	}
	if requests >= RequestsPerRound() {
		t.Fatalf("the row is written on exchange %d of a %d-exchange round; if those were equal "+
			"the whole distinction this function exists for would be gone",
			requests, RequestsPerRound())
	}
	t.Logf("the row lands on exchange %d of %d", requests, RequestsPerRound())
}

// --- the tenant axis --------------------------------------------------------------

// TestStore_OneTenantCannotTakeTheWholeStore is the gate an audit found missing
// (2026-08-24, tappa-security-auditor F2).
//
// 🔴 THE STORE IS ONE PROCESS-WIDE OBJECT SHARED BY EVERY BUSINESS, and until this
// round nothing bounded a business's share of it. Per-actor is 3 and the actor is a
// caller-supplied label keyed on an admin id, so a tenant with enough admin rows
// reaches DefaultMaxLive on its own — and from that moment every OTHER tenant's Begin
// answers ErrTooManySessions. The operator standing at a wall with a plaque gets a
// 409 and no screen explaining it.
func TestStore_OneTenantCannotTakeTheWholeStore(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	greedy := uuid.MustParse("20000000-0000-0000-0000-000000000001")
	victim := uuid.MustParse("20000000-0000-0000-0000-000000000002")

	// One business, as many operators as it likes.
	opened := 0
	for i := 0; i < DefaultMaxLive; i++ {
		actor := fmt.Sprintf("admin:%d", i/DefaultMaxPerActor)
		if _, _, err := h.st.Begin(ctx, greedy, uuid.Nil, actor); err != nil {
			if !errors.Is(err, ErrTooManySessions) {
				t.Fatalf("Begin %d: %v", i, err)
			}
			break
		}
		opened++
	}
	if opened != DefaultMaxPerTenant {
		t.Fatalf("one tenant opened %d rounds, want DefaultMaxPerTenant=%d. Without the "+
			"per-tenant gate it would reach DefaultMaxLive=%d and every other business "+
			"would be refused", opened, DefaultMaxPerTenant, DefaultMaxLive)
	}

	// THE POINT: another business is still served.
	if _, _, err := h.st.Begin(ctx, victim, uuid.Nil, "admin:victim"); err != nil {
		t.Fatalf("a second business was refused (%v) while the first held %d rounds. "+
			"That is the exhaustion this gate exists to refuse", err, opened)
	}
	t.Logf("greedy tenant capped at %d of %d live slots; a second tenant still opens a round",
		opened, DefaultMaxLive)
}

// TestStore_TheTenantCounterIsReleasedByAbortAndBySweep. A counter that leaked would
// refuse a business for ever — worse than the exhaustion the cap exists to prevent.
//
// ⚠️ THE NAME USED TO SAY "OnEveryExit" AND THE BODY DROVE ONE (audit N8, 2026-08-24).
// There are eleven retireLocked call sites; this drove Abort. That is this task
// family's defect pattern number two — a test whose NAME claims what its body does not
// exercise — and the repair is both halves: the name now says which exits, and the
// body now drives the second one that matters, the TTL sweep. The other nine
// (cancellation, Close, the panic branch, Begin's failure path, the deadline arm …)
// are covered for the RING by assertWiped's own suite; what is asserted here is the
// COUNTER, on the two paths a live deployment actually takes.
func TestStore_TheTenantCounterIsReleasedByAbortAndBySweep(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tenant := uuid.MustParse("20000000-0000-0000-0000-000000000003")

	for round := 0; round < 3; round++ {
		var ids []ID
		for i := 0; i < DefaultMaxPerTenant; i++ {
			id, _, err := h.st.Begin(ctx, tenant, uuid.Nil, fmt.Sprintf("admin:%d", i/DefaultMaxPerActor))
			if err != nil {
				t.Fatalf("round %d, session %d: %v", round, i, err)
			}
			ids = append(ids, id)
		}
		if _, _, err := h.st.Begin(ctx, tenant, uuid.Nil, "admin:extra"); !errors.Is(err, ErrTooManySessions) {
			t.Fatalf("round %d: the cap did not bind (%v)", round, err)
		}
		for _, id := range ids {
			h.st.Abort(id)
		}
		if n := h.st.Live(); n != 0 {
			t.Fatalf("round %d: %d session(s) survived Abort", round, n)
		}
	}
	// A fourth full round proves the counter really returned to zero each time; a leak
	// would have refused on round two.
	after, err := func() (ID, error) {
		id, _, e := h.st.Begin(ctx, tenant, uuid.Nil, "admin:after")
		return id, e
	}()
	if err != nil {
		t.Fatalf("after three full cycles the tenant is still refused: %v", err)
	}
	h.st.Abort(after)
	if n := h.st.Live(); n != 0 {
		t.Fatalf("the store holds %d session(s) entering the sweep arm; it must start clean "+
			"or the fill below measures the wrong thing", n)
	}

	// 🔴 AND THE SECOND EXIT: THE TTL SWEEP, which is the one an ABANDONED round takes
	// and the one no caller ever asks for. Abort is driven above; a counter released
	// only by Abort would strand every tenant whose operator simply walked away.
	var swept []ID
	for i := 0; i < DefaultMaxPerTenant; i++ {
		id, _, err := h.st.Begin(ctx, tenant, uuid.Nil, fmt.Sprintf("admin:s%d", i/DefaultMaxPerActor))
		if err != nil {
			t.Fatalf("filling for the sweep: %v", err)
		}
		swept = append(swept, id)
	}
	if _, _, err := h.st.Begin(ctx, tenant, uuid.Nil, "admin:sweepextra"); !errors.Is(err, ErrTooManySessions) {
		t.Fatalf("the cap did not bind before the sweep (%v)", err)
	}
	h.clock.Advance(DefaultTTL + time.Second)
	h.clock.tick(t)
	if n := h.st.Live(); n != 0 {
		t.Fatalf("%d session(s) survived the sweep", n)
	}
	// The counter must have gone with them, or the tenant is refused for ever.
	if _, _, err := h.st.Begin(ctx, tenant, uuid.Nil, "admin:aftersweep"); err != nil {
		t.Fatalf("after the TTL sweep the tenant is still refused (%v). retireLocked releases "+
			"the counter on every path; a sweep that reaped the sessions but not the count "+
			"would lock a business out until the process restarted", err)
	}
	_ = swept
}

// TestSession_TheTenantCapSitsBetweenTheActorCapAndTheStore binds the number.
//
// A bare cap would be the "number with no gate" this repository deletes on sight. It
// is derived from DefaultMaxLive, and both directions are what make the derivation
// meaningful rather than arithmetic.
func TestSession_TheTenantCapSitsBetweenTheActorCapAndTheStore(t *testing.T) {
	// 🔴 THE THREE ARMS ARE CHECKED IN THIS ORDER AND NONE OF THEM IS FATAL, WHICH IS A
	// REPAIR (audit N7, 2026-08-24). The derivation used to be asserted FIRST with
	// t.Fatalf, and because MaxLive/8 <= MaxLive/4 holds for every positive integer, the
	// ceiling arm below could not be reached by ANY mutation — measured: setting
	// MaxPerTenant to MaxLive/2 reddened the DERIVATION arm, never the ceiling. Dead
	// code in a test is worse than no test: the card and the ADR both said "pinned from
	// both directions" and only the floor was.
	//
	// The order is now floor, ceiling, derivation — the two PROPERTIES first, the
	// arithmetic identity last — and t.Errorf lets every failing arm report itself.
	if DefaultMaxPerTenant < 2*DefaultMaxPerActor {
		t.Errorf("DefaultMaxPerTenant (%d) is below 2 x DefaultMaxPerActor (%d): two operators "+
			"of one business could not both hold a full retry sequence, so the tenant cap would "+
			"refuse legitimate work before the actor cap did",
			DefaultMaxPerTenant, 2*DefaultMaxPerActor)
	}
	if DefaultMaxPerTenant > DefaultMaxLive/4 {
		t.Errorf("DefaultMaxPerTenant (%d) is more than a quarter of DefaultMaxLive (%d); "+
			"'one business cannot take the product down' stops meaning anything",
			DefaultMaxPerTenant, DefaultMaxLive)
	}
	if DefaultMaxPerTenant != DefaultMaxLive/8 {
		t.Errorf("DefaultMaxPerTenant = %d, want DefaultMaxLive/8 = %d — it is DERIVED so the "+
			"three caps cannot drift apart", DefaultMaxPerTenant, DefaultMaxLive/8)
	}
	// Guarded, like session_test.go's inventory log: a summary printed under a FAILING
	// run reads as a result and sends the next reader to the wrong line.
	if !t.Failed() {
		t.Logf("caps: per-actor %d, per-tenant %d, store %d",
			DefaultMaxPerActor, DefaultMaxPerTenant, DefaultMaxLive)
	}
}
