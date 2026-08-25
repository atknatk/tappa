package encode

import (
	"context"
	"encoding/hex"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"time"

	"github.com/atknatk/tappa/internal/sun"
)

// --- source-reading invariants --------------------------------------------------

// productionFiles is this package's non-test source, parsed once per test that
// needs it. The precedent and its counted limit are internal/sun's
// TestEV2_ZeroDisciplineIsInventoried: a source-reading test reads TEXT, not
// semantics, so it holds the mutation somebody actually makes (an edit in place)
// and not a wholesale redesign.
func productionFiles(t *testing.T) map[string]*ast.File {
	t.Helper()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	out := map[string]*ast.File{}
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		out[name] = f
	}
	// CONTROL: the walk found real code. Without it every assertion below passes on
	// an empty set, which is exactly the state a rename or a moved file produces.
	if len(out) < 2 {
		t.Fatalf("only %d production files found; the scan has gone blind", len(out))
	}
	return out
}

// enclosingFunc reports which FuncDecl a position falls inside, as
// "receiver.name" or "name".
func enclosingFunc(f *ast.File, pos token.Pos) string {
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || pos < fd.Pos() || pos > fd.End() {
			continue
		}
		recv := ""
		if fd.Recv != nil && len(fd.Recv.List) > 0 {
			switch rt := fd.Recv.List[0].Type.(type) {
			case *ast.StarExpr:
				if id, ok := rt.X.(*ast.Ident); ok {
					recv = id.Name + "."
				}
			case *ast.Ident:
				recv = rt.Name + "."
			}
		}
		return recv + fd.Name.Name
	}
	return ""
}

// TestSession_OnlyTheKeyringWipes is the first half of ADR 0017 §6 md. 7 item 3's
// MECHANICAL answer: every buffer this package allocates is registered in the
// keyring, because the keyring is the only thing allowed to end one.
//
// 🔴 WHY THIS SHAPE AND NOT "COUNT THE WIPES". internal/sun already learned what a
// per-file count of `defer Zero(...)` proves and does not: a security audit deleted
// all eleven of them and the suite stayed green, so the count was added — and that
// count catches DELETIONS while being blind to OMISSIONS. The rule here is the
// complementary one. It does not count wipes at all; it says there is exactly one
// PLACE a wipe can happen, so a new key buffer cannot be given a private, forgotten
// one at its own call site. Omission is then caught behaviourally, by
// TestStore_TheDrivenExitPathsWipeEveryKeyBuffer, which holds the caller's own slices.
func TestSession_OnlyTheKeyringWipes(t *testing.T) {
	files := productionFiles(t)

	var offenders []string
	found := 0
	for name, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Zero" {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "sun" {
				return true
			}
			found++
			fn := enclosingFunc(f, call.Pos())
			if !strings.HasPrefix(fn, "keyring.") {
				offenders = append(offenders, name+":"+fn)
			}
			return true
		})
	}

	if found == 0 {
		t.Fatalf("no sun.Zero call was found anywhere in the package; either the wipe is gone " +
			"(the §4.7 leak this test exists for) or the scan stopped seeing it")
	}
	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Errorf("sun.Zero is called outside the keyring at %v. Every key buffer must be handed to "+
			"keyring.add, which owns its end; a private wipe at a call site is a wipe that the next "+
			"exit path will forget", offenders)
	}
	t.Logf("%d sun.Zero call sites, all inside keyring methods", found)
}

// TestSession_TheRingIsWipedFromExactlyOnePlace is the second half: ONE exit.
//
// If zeroAll gains a second caller, "every exit path wipes" stops being provable by
// reading one function and becomes a claim about a set. Lowering or raising the
// number is allowed — in the same change that explains the new exit.
func TestSession_TheRingIsWipedFromExactlyOnePlace(t *testing.T) {
	files := productionFiles(t)

	var callers []string
	for name, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "zeroAll" {
				callers = append(callers, name+":"+enclosingFunc(f, call.Pos()))
			}
			return true
		})
	}

	sort.Strings(callers)
	want := []string{"session.go:Store.retireLocked"}
	if !reflect.DeepEqual(callers, want) {
		t.Fatalf("keyring.zeroAll is called from %v, want exactly %v. ADR 0017 §6 md. 7 item 3 is "+
			"answered by there being ONE exit; a second call site means the guarantee has to be "+
			"re-argued over a set of them", callers, want)
	}
}

// TestSession_NoExportedAPIHandsOutASession pins the custody rule Store's comment
// states: a caller cannot hold a session, so it cannot drop one unwiped.
func TestSession_NoExportedAPIHandsOutASession(t *testing.T) {
	files := productionFiles(t)

	mentionsSession := func(fl *ast.FieldList) bool {
		if fl == nil {
			return false
		}
		for _, f := range fl.List {
			expr := f.Type
			if star, ok := expr.(*ast.StarExpr); ok {
				expr = star.X
			}
			if id, ok := expr.(*ast.Ident); ok && id.Name == "Session" {
				return true
			}
		}
		return false
	}

	checked := 0
	var leaks []string
	for name, f := range files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || !fd.Name.IsExported() {
				continue
			}
			// Methods ON Session are exported API of an unexported-by-custody type;
			// there are none, and this loop would catch one appearing.
			checked++
			if mentionsSession(fd.Type.Params) || mentionsSession(fd.Type.Results) {
				leaks = append(leaks, name+":"+fd.Name.Name)
			}
			if fd.Recv != nil && mentionsSession(fd.Recv) {
				leaks = append(leaks, name+":Session."+fd.Name.Name)
			}
		}
	}
	if checked == 0 {
		t.Fatalf("no exported function was examined; the scan has gone blind")
	}
	if len(leaks) > 0 {
		sort.Strings(leaks)
		t.Fatalf("these exported functions pass a Session across the package boundary: %v. "+
			"The store must be the only holder, or 'wiped on every exit path' becomes a claim "+
			"about callers rather than about the type", leaks)
	}
	t.Logf("%d exported functions examined, none touches a *Session", checked)
}

// --- the inventory ---------------------------------------------------------------

// TestSession_TheKeyInventoryIsTheOneADR0017Lists holds ADR 0017 §6 md. 7 item 5:
// the session's key material, written down in ONE place.
//
// It ratchets both ways. A slot disappearing means something stopped being wiped; a
// slot appearing without a line here means a new secret arrived and nobody said what
// it is.
func TestSession_TheKeyInventoryIsTheOneADR0017Lists(t *testing.T) {
	want := []string{
		// ADR 0017 §4's four:
		"KSesAuthENC", "KSesAuthMAC",
		// ... TI and CmdCtr are the other two, and they are NOT key material — see
		// the keyring's "what is deliberately not in it" list.
		"RndA", "RndB",
		// ADR 0017 §3 and §5.1 step 9's PLURAL: two plain plaque keys, of which the
		// second is blocked on §6 md. 5's schema decision and is never filled today.
		"K_SDMFileRead", "K_AppMaster",
	}
	if !reflect.DeepEqual(keyInventory, want) {
		t.Fatalf("keyInventory = %v, want %v", keyInventory, want)
	}

	ring := newKeyring()
	var got []string
	for _, s := range ring.slots {
		got = append(got, s.name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("a fresh keyring has slots %v, want %v", got, want)
	}

	// A name outside the inventory cannot be registered at all: there is no way to
	// smuggle an uninventoried secret into a session.
	if err := ring.add("SomeOtherKey", bytesOf(16, 0x01)); err == nil {
		t.Fatalf("an uninventoried key name was accepted")
	}
}

// TestSession_TheEV2AuthShapeIsTheOneThisPackageInventories is DESIGN DECISION (a),
// mechanised.
//
// The decision: sun.EV2Auth stays a RESULT VALUE with five exported fields and no
// `authenticated` flag, and authentication state lives in the session's step index
// instead. internal/sun is not modified by this round.
//
// This test is what stops that decision from rotting quietly. If EV2Auth grows a
// sixth field it is either (i) a flag, which contradicts the decision, or (ii) new
// state, which may be key material and would then be missing from the keyring
// inventory above. Both need a human; both turn this red.
func TestSession_TheEV2AuthShapeIsTheOneThisPackageInventories(t *testing.T) {
	typ := reflect.TypeOf(sun.EV2Auth{})
	var fields []string
	for i := 0; i < typ.NumField(); i++ {
		fields = append(fields, typ.Field(i).Name)
	}
	want := []string{"TI", "KeyENC", "KeyMAC", "PDcap2", "PCDcap2"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("sun.EV2Auth has fields %v, want %v.\n"+
			"This package decided (M8-05 FAZ B2c-1) that authentication state lives in the SESSION's "+
			"step index, not in a flag on EV2Auth, and that the two secret fields here are exactly the "+
			"two the keyring inventories. A new field breaks one of those two statements", fields, want)
	}

	// The two secret ones are the two the ring names. Stated here so the mapping is
	// not only in a comment.
	for _, name := range []string{"KeyENC", "KeyMAC"} {
		if _, ok := typ.FieldByName(name); !ok {
			t.Fatalf("EV2Auth has no %s to inventory", name)
		}
	}
}

// TestSession_TheTTLCoversTheRoundAndNotMuchMore binds DefaultTTL to something.
//
// A bare duration is exactly the "number with no gate" this repository keeps having
// to delete. The floor is derived from the step table rather than repeated, so
// adding an exchange (step 8, when ADR 0017 §6 md. 5 lands) fails this test until
// the TTL is reconsidered — which is the review step that is the point.
func TestSession_TheTTLCoversTheRoundAndNotMuchMore(t *testing.T) {
	floor := time.Duration(len(roundSteps)) * exchangeBudget
	if DefaultTTL < floor {
		t.Fatalf("DefaultTTL is %v but the round is %d exchanges at %v each = %v; "+
			"a legitimate round would be cut off", DefaultTTL, len(roundSteps), exchangeBudget, floor)
	}
	if DefaultTTL > 2*floor {
		// 🔴 THE JUSTIFICATION THAT USED TO BE IN THIS MESSAGE WAS RETRACTED IN THE
		// CODE AND LEFT STANDING HERE — the exact "the mechanism moved, the prose
		// stayed" defect this project keeps recording. It said the chip "drops its own
		// authentication state the moment the field goes away (NT4H2421Gx rev. 3.0
		// p. 28)". Measured twice, by two people: "authentication state" occurs
		// exactly twice in that document and both are about a COMMAND ERROR; the
		// document says nothing about the field at all.
		t.Fatalf("DefaultTTL is %v, more than twice the %v the round needs. Every extra second is a "+
			"second an ABANDONED session holds its plain plaque key. The ceiling is a DESIGN "+
			"JUDGEMENT about exposure, not a reading of the datasheet — see DefaultTTL's comment",
			DefaultTTL, floor)
	}
	// ⚠️ NO CROSS-CHECK IS ASSERTED HERE, AND THAT IS THE CORRECTION. This used to
	// say "ADR 0017 §4 counts the same number off §5.1 independently of this table"
	// and then asserted len(roundSteps) >= 10 on that basis. The ADR's ten counts
	// ChangeKey TWICE (step 8, which is not shipped) and has no GetCardUID: a
	// different composition that totals the same by coincidence. The floor above is
	// derived from THIS table and from nothing else.
}

// --- every exit path -------------------------------------------------------------

// liveSession fetches a session for inspection. Tests are in-package precisely so
// this is possible; no exported API offers it (see
// TestSession_NoExportedAPIHandsOutASession).
func liveSession(t *testing.T, st *Store, id ID) *Session {
	t.Helper()
	st.mu.Lock()
	defer st.mu.Unlock()
	s, ok := st.live[id]
	if !ok {
		t.Fatalf("session is not live")
	}
	return s
}

// armed drives a fresh round far enough that the keyring is FULL — the plaque key
// from step 3 and both session keys from step 4 — and returns the handle, the
// chip, the session and the caller's own copies of the slice headers.
//
// 🔴 HOLDING THE SLICE HEADERS IS WHAT MAKES THE ASSERTION REAL. sun.Zero clears in
// place, so a test that kept the same backing arrays can see whether the bytes went
// to zero. A buffer that was never registered in the ring will still be non-zero
// here, which is the OMISSION the source-reading tests above cannot see.
func armed(t *testing.T, h *harness, chip *fakeChip, actor string) (ID, Progress, *Session, [][]byte) {
	t.Helper()
	ctx := context.Background()
	id, p, err := h.st.Begin(ctx, testTenant, uuid.Nil, actor)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	for i := 0; i < 6; i++ { // select + three GetVersion frames + both auth halves
		p, err = h.st.Step(ctx, id, chip.Transceive(p.Command))
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	s := liveSession(t, h.st, id)

	var bufs [][]byte
	names := s.ring.filled()
	for _, n := range names {
		b, err := s.ring.peek(n)
		if err != nil {
			t.Fatalf("peek %s: %v", n, err)
		}
		bufs = append(bufs, b)
	}
	// The five that must be live at this point. Fewer means something is not being
	// registered; the assertion below would then be vacuously green without it.
	wantNames := []string{"KSesAuthENC", "KSesAuthMAC", "RndA", "RndB", "K_SDMFileRead"}
	sort.Strings(names)
	sort.Strings(wantNames)
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("mid-round the ring holds %v, want %v", names, wantNames)
	}
	for _, b := range bufs {
		if allZero(b) {
			t.Fatalf("a registered buffer was already zero before the exit; the assertion would be vacuous")
		}
	}
	return id, p, s, bufs
}

func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

func assertWiped(t *testing.T, st *Store, id ID, bufs [][]byte) {
	t.Helper()
	for i, b := range bufs {
		if !allZero(b) {
			t.Fatalf("key buffer %d survived the exit path with %d non-zero bytes. "+
				"ADR 0017 §6 md. 7 item 3: sun.Zero must run on EVERY exit — success, error, "+
				"timeout, cancellation, shutdown", i, len(b))
		}
	}
	st.mu.Lock()
	_, still := st.live[id]
	st.mu.Unlock()
	if still {
		t.Fatalf("the session is still in the store after its exit path ran")
	}
}

// assertRetired adds the flag half of a retire, which nothing held: removing
// `s.finished = true` from retireLocked survived the suite (A18). The flag is what
// stops advance from running on a session object a caller somehow still holds, so it
// is belt to the store's braces — and belt that nothing checks is not belt.
func assertRetired(t *testing.T, s *Session) {
	t.Helper()
	if !s.finished {
		t.Fatalf("a retired session is not marked finished; advance would still run on it")
	}
}

// TestStore_TheDrivenExitPathsWipeEveryKeyBuffer is ADR 0017 §6 md. 7 item 3, driven.
//
// ⚠️ RENAMED FROM ...EveryExitPath... (2026-08-21, second audit): the name asserted
// a universal and the body drives a LIST, which is the same name-versus-body defect
// this file's own b11 correction is about. What it drives is named here and counted:
// SEVEN sub-tests here — success, step error, timeout swept by the goroutine, timeout
// seen lazily, cancellation, shutdown, abort — plus an eighth path, a panic inside a
// step, which is driven NEXT DOOR in TestStore_APanicInAStepStillWipesTheSession. The
// earlier version of this line said "EIGHT paths" while the body held seven, counting
// the neighbour without saying it was a neighbour; the numbers now match what each
// file actually runs. The five ADR 0017 §6 md. 7 item 3 names are all among the eight.
//
// Two further retire reasons exist and are driven ELSEWHERE rather than here, which
// is why the universal name was wrong: the deadline passing while a step runs
// (TestDriver_ASlowRowWriteThatOutlivesTheTTLEndsTheRound) and Begin failing to build
// its first command. And step 3's own failure paths, where the key is not yet part
// of a full ring, are measured on the KEY'S BYTES in
// TestDriver_TheFreshPlaqueKeyIsWipedOnBothReachableFailurePathsOfStep3.
func TestStore_TheDrivenExitPathsWipeEveryKeyBuffer(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		h := newHarness(t)
		chip := newFakeChip(t)
		id, p, _, bufs := armed(t, h, chip, "operator-1")
		ctx := context.Background()
		for p.Command != nil {
			var err error
			p, err = h.st.Step(ctx, id, chip.Transceive(p.Command))
			if err != nil {
				t.Fatalf("step: %v", err)
			}
			if p.Done {
				break
			}
		}
		if !p.Done {
			t.Fatalf("the round did not finish")
		}
		assertWiped(t, h.st, id, bufs)
	})

	t.Run("error", func(t *testing.T) {
		h := newHarness(t)
		chip := newFakeChip(t)
		id, p, _, bufs := armed(t, h, chip, "operator-1")
		chip.failNextWithSW = 0x911E // INTEGRITY_ERROR
		if _, err := h.st.Step(context.Background(), id, chip.Transceive(p.Command)); err == nil {
			t.Fatalf("the chip error was accepted")
		}
		assertWiped(t, h.st, id, bufs)
	})

	t.Run("timeout_swept_by_the_goroutine", func(t *testing.T) {
		h := newHarness(t)
		chip := newFakeChip(t)
		id, _, _, bufs := armed(t, h, chip, "operator-1")

		// Nobody ever calls Step again — this IS the abandoned session. Only the
		// sweeper can reach it.
		h.clock.Advance(DefaultTTL + time.Second)
		h.clock.tick(t)
		assertWiped(t, h.st, id, bufs)
		if h.st.Live() != 0 {
			t.Fatalf("%d sessions live after the sweep", h.st.Live())
		}
	})

	t.Run("timeout_seen_lazily_on_lookup", func(t *testing.T) {
		h := newHarness(t)
		chip := newFakeChip(t)
		id, p, _, bufs := armed(t, h, chip, "operator-1")

		h.clock.Advance(DefaultTTL + time.Second)
		// No tick: the sweeper has not run. The lookup must still refuse and wipe.
		if _, err := h.st.Step(context.Background(), id, chip.Transceive(p.Command)); !errors.Is(err, ErrUnknownSession) {
			t.Fatalf("an expired session was resumed (err = %v)", err)
		}
		assertWiped(t, h.st, id, bufs)
	})

	t.Run("cancellation", func(t *testing.T) {
		h := newHarness(t)
		chip := newFakeChip(t)
		id, p, _, bufs := armed(t, h, chip, "operator-1")

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := h.st.Step(ctx, id, chip.Transceive(p.Command)); err == nil {
			t.Fatalf("a cancelled step succeeded")
		}
		assertWiped(t, h.st, id, bufs)
	})

	t.Run("shutdown", func(t *testing.T) {
		h := newHarness(t)
		chip := newFakeChip(t)
		id, _, _, bufs := armed(t, h, chip, "operator-1")
		s := liveSession(t, h.st, id)
		if err := h.st.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		assertWiped(t, h.st, id, bufs)
		assertRetired(t, s)
	})

	t.Run("abort", func(t *testing.T) {
		h := newHarness(t)
		chip := newFakeChip(t)
		id, _, _, bufs := armed(t, h, chip, "operator-1")
		h.st.Abort(id)
		assertWiped(t, h.st, id, bufs)
	})
}

// TestStore_TheSweeperIsAGoroutineAndItRuns isolates the claim that answers
// ADR 0017 §6 md. 7 item 2.
//
// 🔴 THE ASSERTION IS THAT NOBODY ASKED. No Step, no Abort, no Close — the only
// thing that happens between arming the session and finding it wiped is a tick.
// Deleting the `go st.sweepLoop(ticks)` line makes fakeClock.tick block and this
// test fail on its own timeout, which is the mutation the test exists for: lazy
// expiry alone would leave an abandoned session's plain plaque key in memory for as
// long as the process lives.
func TestStore_TheSweeperIsAGoroutineAndItRuns(t *testing.T) {
	h := newHarness(t)
	chip := newFakeChip(t)
	id, _, _, bufs := armed(t, h, chip, "operator-1")

	// POSITIVE CONTROL: a tick BEFORE the deadline must not touch anything.
	h.clock.Advance(DefaultTTL / 2)
	h.clock.tick(t)
	if h.st.Live() != 1 {
		t.Fatalf("the sweeper reaped a session that had not expired")
	}
	for _, b := range bufs {
		if allZero(b) {
			t.Fatalf("a live session's keys were wiped early")
		}
	}

	h.clock.Advance(DefaultTTL)
	h.clock.tick(t)
	assertWiped(t, h.st, id, bufs)
}

// --- concurrency limits ----------------------------------------------------------

// TestStore_ConcurrencyLimitsReallyLimit — ADR 0017 §6 md. 7 item 4, under -race.
func TestStore_ConcurrencyLimitsReallyLimit(t *testing.T) {
	t.Run("per_actor", func(t *testing.T) {
		h := newHarness(t, func(c *Config) { c.MaxLive = 1000 })

		const goroutines = 40
		var wg sync.WaitGroup
		var mu sync.Mutex
		ok, refused := 0, 0
		wg.Add(goroutines)
		for i := 0; i < goroutines; i++ {
			go func() {
				defer wg.Done()
				_, _, err := h.st.Begin(context.Background(), testTenant, uuid.Nil, "one-operator")
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					ok++
				case errors.Is(err, ErrTooManySessions):
					refused++
				default:
					t.Errorf("unexpected error: %v", err)
				}
			}()
		}
		wg.Wait()

		if ok != DefaultMaxPerActor {
			t.Fatalf("%d of %d Begins succeeded for one actor, want %d", ok, goroutines, DefaultMaxPerActor)
		}
		if refused != goroutines-DefaultMaxPerActor {
			t.Fatalf("%d refusals, want %d", refused, goroutines-DefaultMaxPerActor)
		}
		if h.st.Live() != DefaultMaxPerActor {
			t.Fatalf("%d sessions live, want %d", h.st.Live(), DefaultMaxPerActor)
		}
	})

	t.Run("store_wide", func(t *testing.T) {
		h := newHarness(t, func(c *Config) { c.MaxLive = 5; c.MaxPerActor = 1000 })

		const goroutines = 40
		var wg sync.WaitGroup
		var mu sync.Mutex
		ok := 0
		wg.Add(goroutines)
		for i := 0; i < goroutines; i++ {
			go func(n int) {
				defer wg.Done()
				_, _, err := h.st.Begin(context.Background(), testTenant, uuid.Nil, "operator-"+string(rune('a'+n%26)))
				if err == nil {
					mu.Lock()
					ok++
					mu.Unlock()
				}
			}(i)
		}
		wg.Wait()

		if ok != 5 {
			t.Fatalf("%d of %d Begins succeeded, want the store-wide cap of 5", ok, goroutines)
		}
	})

	t.Run("a_freed_slot_is_reusable", func(t *testing.T) {
		// POSITIVE CONTROL for both caps: the limit is a limit, not a one-way latch.
		h := newHarness(t, func(c *Config) { c.MaxPerActor = 1; c.MaxLive = 1 })
		id, _, err := h.st.Begin(context.Background(), testTenant, uuid.Nil, "operator-1")
		if err != nil {
			t.Fatalf("first Begin: %v", err)
		}
		if _, _, err := h.st.Begin(context.Background(), testTenant, uuid.Nil, "operator-1"); !errors.Is(err, ErrTooManySessions) {
			t.Fatalf("the second Begin returned %v, want ErrTooManySessions", err)
		}
		h.st.Abort(id)
		if _, _, err := h.st.Begin(context.Background(), testTenant, uuid.Nil, "operator-1"); err != nil {
			t.Fatalf("a slot freed by Abort was not reusable: %v", err)
		}
	})

	t.Run("expired_sessions_do_not_hold_a_slot", func(t *testing.T) {
		// An operator whose reads dropped must not be locked out for a whole TTL by
		// sessions that are already dead.
		h := newHarness(t, func(c *Config) { c.MaxPerActor = 1; c.MaxLive = 1 })
		if _, _, err := h.st.Begin(context.Background(), testTenant, uuid.Nil, "operator-1"); err != nil {
			t.Fatalf("first Begin: %v", err)
		}
		h.clock.Advance(DefaultTTL + time.Second)
		if _, _, err := h.st.Begin(context.Background(), testTenant, uuid.Nil, "operator-1"); err != nil {
			t.Fatalf("Begin after the previous session expired: %v", err)
		}
	})
}

// TestStore_ConcurrentStepsOnOneSessionAreSerialised. Two commands sharing one
// CmdCtr would break the session on the chip, and the failure would read as a MAC
// problem — skill tappa-sun's named trap.
func TestStore_ConcurrentStepsOnOneSessionAreSerialised(t *testing.T) {
	h := newHarness(t)
	chip := newFakeChip(t)
	id, p, _, _ := armed(t, h, chip, "operator-1")
	resp := chip.Transceive(p.Command)

	// The first step is allowed to run; a second concurrent one must be refused
	// rather than interleaved. Driven sequentially here because the refusal is what
	// is under test, and the race detector covers the shared state in every other
	// concurrent test in this file.
	s := liveSession(t, h.st, id)
	h.st.mu.Lock()
	s.busy = true
	h.st.mu.Unlock()
	if _, err := h.st.Step(context.Background(), id, resp); !errors.Is(err, ErrBusy) {
		t.Fatalf("a concurrent step returned %v, want ErrBusy", err)
	}
	h.st.mu.Lock()
	s.busy = false
	h.st.mu.Unlock()
	if _, err := h.st.Step(context.Background(), id, resp); err != nil {
		t.Fatalf("the step failed once the session was free: %v", err)
	}
}

// --- store lifecycle -------------------------------------------------------------

func TestStore_RefusesToBuildWithoutItsPorts(t *testing.T) {
	rows := newRecordingRows()
	wrapper := testWrapper{kek: bytesOf(32, 0x2A)}
	for _, tc := range []struct {
		name string
		cfg  Config
	}{
		{"no_rows", Config{Wrapper: wrapper, BaseURL: testBaseURL}},
		{"no_wrapper", Config{Rows: rows, BaseURL: testBaseURL}},
		{"no_base_url", Config{Rows: rows, Wrapper: wrapper}},
		{"base_url_with_a_query", Config{Rows: rows, Wrapper: wrapper, BaseURL: "https://tap.example.com/t?x=1"}},
		{"base_url_not_https", Config{Rows: rows, Wrapper: wrapper, BaseURL: "http://tap.example.com/t"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if st, err := NewStore(tc.cfg); err == nil {
				if cerr := st.Close(); cerr != nil {
					t.Errorf("Close: %v", cerr)
				}
				t.Fatalf("NewStore accepted %s", tc.name)
			}
		})
	}

	// POSITIVE CONTROL: the well-formed configuration is accepted, so the five
	// refusals above are about their inputs and not about NewStore refusing
	// everything.
	st, err := NewStore(Config{Rows: rows, Wrapper: wrapper, BaseURL: testBaseURL})
	if err != nil {
		t.Fatalf("a valid configuration was refused: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestStore_AfterCloseNothingStarts(t *testing.T) {
	h := newHarness(t)
	if err := h.st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, _, err := h.st.Begin(context.Background(), testTenant, uuid.Nil, "operator-1"); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("Begin after Close returned %v", err)
	}
	if _, err := h.st.Step(context.Background(), "whatever", sw(0x9100)); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("Step after Close returned %v", err)
	}
	// Close is idempotent: a second one must not panic on a closed channel.
	if err := h.st.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestStore_AnActorIsRequired(t *testing.T) {
	h := newHarness(t)
	if _, _, err := h.st.Begin(context.Background(), testTenant, uuid.Nil, ""); err == nil {
		t.Fatalf("an empty actor was accepted; every empty actor would share one budget")
	}
}

// TestStore_AnUnknownHandleIsIndistinguishableFromAnExpiredOne. The handle is a
// bearer credential; telling the two apart would turn a guess into an oracle.
func TestStore_AnUnknownHandleIsIndistinguishableFromAnExpiredOne(t *testing.T) {
	h := newHarness(t)
	chip := newFakeChip(t)
	id, p, err := h.st.Begin(context.Background(), testTenant, uuid.Nil, "operator-1")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	h.clock.Advance(DefaultTTL + time.Second)
	expired := errString(h.st.Step(context.Background(), id, chip.Transceive(p.Command)))
	unknown := errString(h.st.Step(context.Background(), "0123456789abcdef0123456789abcdef", sw(0x9100)))
	if expired != unknown {
		t.Fatalf("expired says %q and unknown says %q", expired, unknown)
	}
}

func errString(_ Progress, err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

// --- §4.7: nothing here formats a secret ------------------------------------------

// TestSession_NoErrorMessageCarriesKeyMaterial drives the error paths this package
// can produce and asserts none of them prints bytes.
//
// ⚠️ COUNTED LIMIT, stated because the alternative is over-claiming: this checks
// the messages these paths actually produce. It is not a proof about every possible
// message, which is what scripts/redline-check.sh's R7 grep is for, from the other
// side.
func TestSession_NoErrorMessageCarriesKeyMaterial(t *testing.T) {
	ring := newKeyring()
	secret := bytesOf(16, 0xAB)
	if err := ring.add(keyNameSesENC, secret); err != nil {
		t.Fatalf("add: %v", err)
	}

	var msgs []string
	if err := ring.add(keyNameSesENC, bytesOf(16, 0xCD)); err != nil {
		msgs = append(msgs, err.Error())
	}
	if err := ring.add("NotAKey", bytesOf(16, 0xEF)); err != nil {
		msgs = append(msgs, err.Error())
	}
	if _, err := ring.take(keyNameRndA); err != nil {
		msgs = append(msgs, err.Error())
	}
	if _, err := (&Session{cmdCtr: 0xFFFF}).useCtr(); err != nil {
		msgs = append(msgs, err.Error())
	}
	msgs = append(msgs, (&RelayMismatchError{RowUID: "04112233445566", ChipUID: "04968CAA5C5E80"}).Error())

	if len(msgs) < 5 {
		t.Fatalf("only %d error paths produced a message; the scan is not exercising them", len(msgs))
	}
	for _, m := range msgs {
		for _, forbidden := range []string{"ABABAB", "CDCDCD", "EFEFEF", "abababab"} {
			if strings.Contains(m, forbidden) {
				t.Fatalf("an error message carries key bytes: %q", m)
			}
		}
	}

	// ⚠️ A VACUOUS ASSERTION USED TO SIT HERE AND IT IS DELETED, NOT REPAIRED (tenth
	// audit, 2026-08-21). It read `if !allZero(bytesOf(0, 0))` under the comment "the
	// refused buffers are wiped by add, so nothing is left behind either" —
	// bytesOf(0, 0) is an EMPTY slice, so allZero is trivially true, and the two
	// buffers this function hands to add are never bound to a variable, so checking
	// them here was not even possible. The sentence claimed something the code could
	// not carry: the sixth instance of that pattern in this task.
	//
	// The property IS held, next door and for real: TestSession_RefusedRegistrationsAreWiped
	// binds each refused buffer and asserts allZero on it, and an auditor's mutations
	// against add's two wipe paths both turn it red. Deleting a claim that measures
	// nothing is better than repairing it in a place that has no access to the values.
}

// TestSession_RefusedRegistrationsAreWiped is the property the comment on
// keyring.add claims: a buffer the ring turns away does not survive as an orphan.
func TestSession_RefusedRegistrationsAreWiped(t *testing.T) {
	ring := newKeyring()
	if err := ring.add(keyNameSesENC, bytesOf(16, 0x11)); err != nil {
		t.Fatalf("add: %v", err)
	}

	dup := bytesOf(16, 0x22)
	if err := ring.add(keyNameSesENC, dup); err == nil {
		t.Fatalf("a duplicate registration was accepted")
	}
	if !allZero(dup) {
		t.Fatalf("the refused duplicate was not wiped")
	}

	unknown := bytesOf(16, 0x33)
	if err := ring.add("NotInTheInventory", unknown); err == nil {
		t.Fatalf("an uninventoried name was accepted")
	}
	if !allZero(unknown) {
		t.Fatalf("the refused buffer was not wiped")
	}

	// POSITIVE CONTROL: an ACCEPTED buffer is not wiped on the way in.
	accepted := bytesOf(16, 0x44)
	if err := ring.add(keyNameSesMAC, accepted); err != nil {
		t.Fatalf("add: %v", err)
	}
	if allZero(accepted) {
		t.Fatalf("add wiped a buffer it accepted")
	}
}

// TestSession_TheStoreFileHasNoLoggerAtAll. This package handles a plain plaque
// key and a bearer handle, and the cheapest way to keep them out of a log line is
// to have no logger in the package at all (CLAUDE.md §4.7's never-log list).
// A future round that needs one must add it deliberately, and this is where that
// decision becomes visible.
func TestSession_TheStoreFileHasNoLoggerAtAll(t *testing.T) {
	for name := range productionFiles(t) {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if strings.Contains(string(src), "log/slog") {
			t.Fatalf("%s imports log/slog. This package holds a plain plaque key and a bearer "+
				"session handle; adding a logger needs a deliberate decision about what it may say", name)
		}
	}
}

// --- the default clock ------------------------------------------------------------

// TestSession_TheSystemClockIsTheProductionOne.
//
// 🔴 EVERY OTHER TEST IN THIS PACKAGE INJECTS A FAKE CLOCK, AND PRODUCTION USES
// NEITHER OF THEM. Without this, the code path a real deployment runs — the default
// systemClock — would be the one thing here nobody exercised, which is the same
// class of gap as a config value nothing constructs.
func TestSession_TheSystemClockIsTheProductionOne(t *testing.T) {
	var c Clock = systemClock{}

	before := time.Now()
	got := c.Now()
	after := time.Now()
	if got.Before(before) || got.After(after) {
		t.Fatalf("systemClock.Now returned %v, outside [%v, %v]", got, before, after)
	}

	ticks, stop := c.NewTicker(time.Millisecond)
	select {
	case <-ticks:
	case <-time.After(5 * time.Second):
		t.Fatalf("a 1ms system ticker did not fire")
	}
	stop()

	// And a store built with no Clock uses it rather than panicking on a nil.
	st, err := NewStore(Config{
		Rows:    newRecordingRows(),
		Wrapper: testWrapper{kek: bytesOf(32, 0x2A)},
		BaseURL: testBaseURL,
	})
	if err != nil {
		t.Fatalf("NewStore with the default clock: %v", err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()
	if _, _, err := st.Begin(context.Background(), testTenant, uuid.Nil, "operator-1"); err != nil {
		t.Fatalf("Begin on a default-clock store: %v", err)
	}
	if st.Live() != 1 {
		t.Fatalf("%d sessions live", st.Live())
	}
}

// --- small guards -----------------------------------------------------------------

func TestSession_KeyringEdges(t *testing.T) {
	ring := newKeyring()

	if _, err := ring.peek(keyNameSesENC); err == nil {
		t.Fatalf("peek returned a buffer for an empty slot")
	}
	if _, err := ring.peek("NotInTheInventory"); err == nil {
		t.Fatalf("peek accepted a name outside the inventory")
	}
	if _, err := ring.take("NotInTheInventory"); err == nil {
		t.Fatalf("take accepted a name outside the inventory")
	}
	if err := ring.add(keyNameSesENC, nil); err == nil {
		t.Fatalf("an empty buffer was registered; a zero-length slot would make zeroAll a no-op that looks like a wipe")
	}
	if names := ring.filled(); len(names) != 0 {
		t.Fatalf("a fresh ring reports %v as filled", names)
	}
	// zeroAll over an empty ring is a no-op rather than a panic; the single exit
	// path calls it unconditionally, including for a session that never got past
	// step 1.
	ring.zeroAll()
}

func TestStore_AbortIsSafeOnHandlesItDoesNotHold(t *testing.T) {
	h := newHarness(t)
	// An unknown handle: silence, not an oracle.
	h.st.Abort("0123456789abcdef0123456789abcdef")

	chip := newFakeChip(t)
	id, _, _, bufs := armed(t, h, chip, "operator-1")

	// A BUSY session cannot be wiped from under the goroutine that owns its ring;
	// Abort expires it instead, and the next custody check finishes the job.
	s := liveSession(t, h.st, id)
	h.st.mu.Lock()
	s.busy = true
	h.st.mu.Unlock()
	h.st.Abort(id)
	for _, b := range bufs {
		if allZero(b) {
			t.Fatalf("Abort wiped a busy session's keys under the goroutine using them")
		}
	}
	h.st.mu.Lock()
	s.busy = false
	h.st.mu.Unlock()
	if _, err := h.st.Step(context.Background(), id, sw(0x9100)); !errors.Is(err, ErrUnknownSession) {
		t.Fatalf("an aborted session was resumed: %v", err)
	}
	assertWiped(t, h.st, id, bufs)
}

func TestStore_RetiringNothingIsSafe(t *testing.T) {
	h := newHarness(t)
	h.st.mu.Lock()
	h.st.retireLocked(nil)
	h.st.mu.Unlock()
}

// TestStore_BeginRefusesACancelledContext keeps the store from minting a session —
// and therefore a keyring — for a caller that has already gone away.
func TestStore_BeginRefusesACancelledContext(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := h.st.Begin(ctx, testTenant, uuid.Nil, "operator-1"); err == nil {
		t.Fatalf("Begin accepted a cancelled context")
	}
	if h.st.Live() != 0 {
		t.Fatalf("%d sessions live after a refused Begin", h.st.Live())
	}
}

// TestStore_AVeryShortTTLStillGetsASweepPeriod. TTL/sweepDivisor rounds to zero for
// any TTL under six units; a zero period is not a valid ticker interval and would
// panic time.NewTicker. The floor keeps a store built with an aggressive TTL — a
// test, or a deployment that decides ninety seconds is too generous — working.
func TestStore_AVeryShortTTLStillGetsASweepPeriod(t *testing.T) {
	st, err := NewStore(Config{
		Rows:    newRecordingRows(),
		Wrapper: testWrapper{kek: bytesOf(32, 0x2A)},
		BaseURL: testBaseURL,
		Clock:   newFakeClock(),
		TTL:     time.Nanosecond,
	})
	if err != nil {
		t.Fatalf("NewStore with a 1ns TTL: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestStore_TheSweeperSkipsASessionThatIsMidStep. Wiping a keyring out from under
// the goroutine using it would be a data race and an error nobody could read; the
// step's own completion path catches it one exchange later instead.
func TestStore_TheSweeperSkipsASessionThatIsMidStep(t *testing.T) {
	h := newHarness(t)
	chip := newFakeChip(t)
	id, _, _, bufs := armed(t, h, chip, "operator-1")

	s := liveSession(t, h.st, id)
	h.st.mu.Lock()
	s.busy = true
	h.st.mu.Unlock()

	h.clock.Advance(DefaultTTL + time.Second)
	h.clock.tick(t)

	for _, b := range bufs {
		if allZero(b) {
			t.Fatalf("the sweeper wiped a session that was mid-step")
		}
	}
	if h.st.Live() != 1 {
		t.Fatalf("the busy session was removed by the sweeper")
	}

	// POSITIVE CONTROL: once it is no longer busy the very next sweep takes it.
	h.st.mu.Lock()
	s.busy = false
	h.st.mu.Unlock()
	h.clock.tick(t)
	assertWiped(t, h.st, id, bufs)
}

// TestStore_ACancelledCallerCannotWipeASessionMidStep. Cancellation is an exit
// path, but not one that may reach into a ring another goroutine is using: the
// session is expired instead, and the step in flight retires it.
func TestStore_ACancelledCallerCannotWipeASessionMidStep(t *testing.T) {
	h := newHarness(t)
	chip := newFakeChip(t)
	id, _, _, bufs := armed(t, h, chip, "operator-1")

	s := liveSession(t, h.st, id)
	h.st.mu.Lock()
	s.busy = true
	h.st.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.st.Step(ctx, id, sw(0x9100)); err == nil {
		t.Fatalf("a cancelled step on a busy session succeeded")
	}
	for _, b := range bufs {
		if allZero(b) {
			t.Fatalf("a busy session's keys were wiped under the goroutine using them")
		}
	}

	h.st.mu.Lock()
	s.busy = false
	h.st.mu.Unlock()
	if _, err := h.st.Step(context.Background(), id, sw(0x9100)); !errors.Is(err, ErrUnknownSession) {
		t.Fatalf("the cancelled session was resumable: %v", err)
	}
	assertWiped(t, h.st, id, bufs)
}

// --- b7: the session handle is the only authority, so it must be unguessable --------

// TestSession_TheHandleIsUnpredictable.
//
// 🔴 WHY THIS IS NOT COSMETIC. Step and Abort check the ID and NOTHING ELSE — they do
// not check the actor. ⚠️ The reason used to be "md. 10's gate is still open"; that
// gate CLOSED in FAZ B2c-2b, and the property survives its retired justification: the
// gate authorises who may OPEN a round, and this package still binds a live round to
// its HANDLE alone (internal/handler's plaqueEncodeStep counts that as a named limit).
// So the handle IS the authority over a live round: whoever has it can drive the
// remaining exchanges of somebody else's encode, or abort it. Two other decisions
// lean on the same assumption and say so: the store's refusal to distinguish
// "unknown" from "expired" is justified as "the ID is a bearer handle", and the
// keyring's inventory excuses the ID from being wiped on the grounds that its
// exposure is bounded by the TTL. Both are arguments about an UNPREDICTABLE handle,
// and until now nothing held that.
//
// Replacing newID's crypto/rand with a counter left the whole suite green.
func TestSession_TheHandleIsUnpredictable(t *testing.T) {
	const samples = 64
	ids := make([]ID, 0, samples)
	seen := map[ID]bool{}
	for i := 0; i < samples; i++ {
		id, err := newID()
		if err != nil {
			t.Fatalf("newID: %v", err)
		}
		if len(id) != 32 {
			t.Fatalf("a handle is %d characters, want 32 hex characters (16 bytes)", len(id))
		}
		if _, err := hex.DecodeString(string(id)); err != nil {
			t.Fatalf("a handle is not hex: %v", err)
		}
		if seen[id] {
			t.Fatalf("two handles collided in %d samples", samples)
		}
		seen[id] = true
		ids = append(ids, id)
	}

	// 🔴 DISTINCTNESS IS NOT ENOUGH AND THAT IS THE POINT: a counter produces 64
	// distinct handles too. What separates random from sequential is the per-BYTE
	// spread. For 64 draws of a uniform byte the expected number of distinct values
	// is about 57; a counter leaves the leading bytes CONSTANT, i.e. 1. The
	// threshold is set at 30 — far below anything random will produce (the
	// probability of dipping there is negligible) and far above what any counter,
	// timestamp or PID-derived scheme can reach in its high bytes.
	const minDistinctPerByte = 30
	for pos := 0; pos < 16; pos++ {
		values := map[byte]bool{}
		for _, id := range ids {
			raw, err := hex.DecodeString(string(id))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			values[raw[pos]] = true
		}
		if len(values) < minDistinctPerByte {
			t.Fatalf("byte %d of the session handle takes only %d distinct values across %d samples "+
				"(want at least %d). A predictable handle is a predictable AUTHORITY: Step and Abort "+
				"check the ID and nothing else — md. 10's gate authorises who may OPEN a round, "+
				"not who may drive one",
				pos, len(values), samples, minDistinctPerByte)
		}
	}
}

// --- b8: the two bare numbers ------------------------------------------------------

// TestStore_TheSweepCadenceIsTheOneItsCommentClaims.
//
// sweepDivisor's comment makes a QUANTITATIVE claim — "an abandoned session's keys
// survive the TTL by at most a sixth of it" — and nothing measured it: changing the
// divisor from 6 to 1 left the suite green, which would mean an abandoned session's
// plain plaque key outlives its deadline by a whole further TTL. This reads the
// period the store actually asks its clock for.
func TestStore_TheSweepCadenceIsTheOneItsCommentClaims(t *testing.T) {
	h := newHarness(t)
	got := h.clock.tickerPeriod()

	if want := DefaultTTL / sweepDivisor; got != want {
		t.Fatalf("the store asked for a %v sweep period, want %v", got, want)
	}
	// The claim the comment makes, as a bound rather than as an equality, so the
	// divisor may be tuned without editing this test — but not to 1.
	if got > DefaultTTL/4 {
		t.Fatalf("the sweep period is %v, more than a quarter of the %v TTL: an abandoned session's "+
			"plain plaque keys would outlive their deadline by that much", got, DefaultTTL)
	}
	// AND THE OTHER DIRECTION, WHICH WAS MISSING (n2, second audit): only the
	// widening mutation (6 -> 1) was killed; the narrowing one (6 -> 12) survived,
	// so the comment's trade-off was unbounded on the side it got backwards. A larger
	// divisor buys a narrower window and pays in wakeups against a store that is
	// empty almost all the time — encoding is a rare, manual job (ADR 0017 §4).
	if got < DefaultTTL/8 {
		t.Fatalf("the sweep period is %v, under an eighth of the %v TTL: that is a wakeup budget "+
			"a store which is idle almost always does not need", got, DefaultTTL)
	}
	if got < time.Second {
		t.Fatalf("the sweep period is %v; that is a busy loop", got)
	}
}

// TestStore_TheGlobalCapBoundsKeyMaterialAndNotJustMemory.
//
// DefaultMaxLive was a bare 64: raising it to 100000 left the suite green. The cap's
// actual job is stated in its comment — bound how much KEY MATERIAL the store can be
// driven into holding — so that is what is asserted, in bytes.
func TestStore_TheGlobalCapBoundsKeyMaterialAndNotJustMemory(t *testing.T) {
	// Two plain plaque keys per session, once ADR 0017 §6 md. 5 lands and K_AppMaster
	// is filled; the session keys are a further two. Four 16-byte secrets is the
	// worst case per live session.
	const secretsPerSession = 4
	worst := DefaultMaxLive * secretsPerSession * plaqueKeyLen
	if worst > 4096 {
		t.Fatalf("at DefaultMaxLive = %d the store can be driven into holding %d bytes of key "+
			"material. The pilot is one branch and encoding is manual (ADR 0017 §4): this is a "+
			"memory ceiling on SECRETS, not a capacity plan", DefaultMaxLive, worst)
	}
	if DefaultMaxLive < DefaultMaxPerActor {
		t.Fatalf("the store-wide cap (%d) is below the per-actor cap (%d), so one operator could "+
			"never reach their own budget", DefaultMaxLive, DefaultMaxPerActor)
	}
}

// TestSession_TheDriverReadsOnlyTheUIDOutOfGetVersion.
//
// 🔴 THIS EXISTS TO KEEP A MUTATION SURVIVOR LEGITIMATE, WHICH IS A DIFFERENT JOB
// FROM KILLING ONE. Swapping GetVersion's first two frames at the parse call site
// survives this package's suite, and that is CORRECT rather than a gap: internal/sun
// takes the UID out of frame THREE, checks frames one and two only for length, and
// this package then reads nothing but Version.UID. Frames one and two are the
// hardware and software blocks, identical on every chip NXP ships, so swapping them
// changes no value this package acts on.
//
// The legitimacy is CONDITIONAL, so the condition is what gets pinned: the moment
// anything here starts reading Hardware or Software — a diagnostic surface, an audit
// field — the survivor stops being legitimate and this test says so, in the change
// that introduced it rather than in a later audit.
func TestSession_TheDriverReadsOnlyTheUIDOutOfGetVersion(t *testing.T) {
	files := productionFiles(t)

	var reads []string
	for name, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if sel.Sel.Name == "Hardware" || sel.Sel.Name == "Software" || sel.Sel.Name == "Production" {
				reads = append(reads, name+":"+sel.Sel.Name)
			}
			return true
		})
	}
	if len(reads) > 0 {
		sort.Strings(reads)
		t.Fatalf("this package now reads %v out of GetVersion. Until now it read only the UID, which "+
			"is why swapping frames one and two is a harmless mutation; if those blocks start "+
			"carrying meaning, the frame order needs its own assertion", reads)
	}
}

// TestSession_TheDeadlineIsATotalBudgetAndIsNeverExtended — n3.
//
// 🔴 NEITHER READING WAS PINNED, WHICH IS WHY THE COMMENT COULD DRIFT. DefaultTTL's
// first line said "how long a session may live WITHOUT PROGRESS" while the code sets
// the deadline once in Begin and never moves it forward; a mutation that turned it
// into a real idle timeout (refreshing the deadline in checkout) survived the suite.
// The two readings differ operationally: an idle timeout lets an arbitrarily long
// round continue, a total budget does not, and the floor derivation
// (len(roundSteps) * exchangeBudget) only makes sense for the second.
func TestSession_TheDeadlineIsATotalBudgetAndIsNeverExtended(t *testing.T) {
	h := newHarness(t)
	chip := newFakeChip(t)
	ctx := context.Background()

	id, p, err := h.st.Begin(ctx, testTenant, uuid.Nil, "operator-1")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	started := h.clock.Now()
	deadline := liveSession(t, h.st, id).deadline
	if want := started.Add(DefaultTTL); !deadline.Equal(want) {
		t.Fatalf("the deadline is %v, want Begin + TTL = %v", deadline, want)
	}

	// Make PROGRESS, repeatedly, with time passing between each exchange. An idle
	// timeout would keep pushing the deadline out; a total budget does not move.
	for i := 0; i < 4; i++ {
		h.clock.Advance(DefaultTTL / 8)
		p, err = h.st.Step(ctx, id, chip.Transceive(p.Command))
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if got := liveSession(t, h.st, id).deadline; !got.Equal(deadline) {
			t.Fatalf("after %d successful exchanges the deadline moved from %v to %v. "+
				"DefaultTTL is documented as a TOTAL round budget, and its floor is derived as "+
				"len(roundSteps) * exchangeBudget on exactly that basis", i+1, deadline, got)
		}
	}

	// And it really does end the round at Begin + TTL despite continuous progress.
	h.clock.Advance(DefaultTTL)
	if _, err := h.st.Step(ctx, id, chip.Transceive(p.Command)); !errors.Is(err, ErrUnknownSession) {
		t.Fatalf("a session that kept making progress outlived its total budget: %v", err)
	}
}

// TestSession_EveryWriterOfTheDeadlineMovesItEarlier — fifth audit, B2.
//
// 🔴 DefaultTTL's comment says the deadline "is never extended; the writes to it all
// move it EARLIER", and that was a CLOSED COUNT that was one short: it named
// cancellation and Abort, while Close's retireIdleLocked was a third writer — and
// unguarded, so it could move a deadline LATER. Measured on a session whose budget
// had expired a minute earlier: Close pushed it forward by exactly one TTL.
//
// The paragraph's other named test drives only the progress reading. This drives all
// three writers, which is what a sentence about "every write" needs.
func TestSession_EveryWriterOfTheDeadlineMovesItEarlier(t *testing.T) {
	// A session whose deadline is ALREADY in the past is the discriminating input:
	// any writer that sets the deadline to "now" unconditionally moves it later.
	arm := func(t *testing.T, h *harness) (ID, *Session) {
		t.Helper()
		chip := newFakeChip(t)
		id, _, _, _ := armed(t, h, chip, "operator-1")
		s := liveSession(t, h.st, id)
		h.st.mu.Lock()
		s.deadline = h.clock.Now().Add(-time.Minute)
		h.st.mu.Unlock()
		return id, s
	}

	t.Run("close", func(t *testing.T) {
		h := newHarness(t, func(c *Config) { c.CloseGrace = 20 * time.Millisecond })
		_, s := arm(t, h)
		h.st.mu.Lock()
		s.busy = true
		before := s.deadline
		h.st.mu.Unlock()

		_ = h.st.Close()

		h.st.mu.Lock()
		after := s.deadline
		s.busy = false
		h.st.mu.Unlock()
		if after.After(before) {
			t.Fatalf("Close moved the deadline LATER, by %v. DefaultTTL's comment says every write "+
				"moves it earlier; an unguarded assignment to now does the opposite on a session "+
				"that is already past its budget", after.Sub(before))
		}
		if cerr := h.st.Close(); cerr != nil {
			t.Fatalf("second Close: %v", cerr)
		}
	})

	t.Run("abort", func(t *testing.T) {
		h := newHarness(t)
		id, s := arm(t, h)
		h.st.mu.Lock()
		s.busy = true
		before := s.deadline
		h.st.mu.Unlock()

		h.st.Abort(id)

		h.st.mu.Lock()
		after := s.deadline
		s.busy = false
		h.st.mu.Unlock()
		if after.After(before) {
			t.Fatalf("Abort moved the deadline LATER, by %v", after.Sub(before))
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		h := newHarness(t)
		id, s := arm(t, h)
		h.st.mu.Lock()
		s.busy = true
		before := s.deadline
		h.st.mu.Unlock()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := h.st.Step(ctx, id, sw(0x9100)); err == nil {
			t.Fatalf("a cancelled step succeeded")
		}

		h.st.mu.Lock()
		after := s.deadline
		s.busy = false
		h.st.mu.Unlock()
		if after.After(before) {
			t.Fatalf("cancellation moved the deadline LATER, by %v", after.Sub(before))
		}
	})
}

// TestStore_ABusySessionIsNeverWipedUnderItsOwner — B-1, the blocking one.
//
// 🔴 THE DEFECT: checkout's expiry branch retired unconditionally, while all three of
// its siblings guard against exactly this. Measured on the defect: all five key
// buffers of a busy session, K_SDMFileRead included, were zeroed while the first step
// was still inside advance — which runs OUTSIDE st.mu, so this is a DATA RACE on live
// key material, and the perUID slot was released mid-round.
//
// ⚠️ NARROWED (third audit): an earlier version of this comment said the wipe
// "installs a ZEROED key on the chip". It does not, and the reason is program order —
// zeroAll wipes K_SDMFileRead last while EV2ChangeKeyCommand reads it first, and both
// concrete outcomes are fail-closed (sun.ChangeKeyData refuses an all-zero key; a
// torn one gets 911E from the chip). The race and the released slot are the earned
// claims. The persistent-corruption version of this defect lives at Close, where the
// wipe lands inside sun.Wrap — see TestStore_CloseDoesNotCommitAnAllZeroKey.
func TestStore_ABusySessionIsNeverWipedUnderItsOwner(t *testing.T) {
	h := newHarness(t)
	chip := newFakeChip(t)
	id, _, _, bufs := armed(t, h, chip, "operator-1")

	s := liveSession(t, h.st, id)
	h.st.mu.Lock()
	s.busy = true
	h.st.mu.Unlock()

	// The deadline passes while the owner is still inside its step.
	h.clock.Advance(DefaultTTL + time.Second)

	if _, err := h.st.Step(context.Background(), id, sw(0x9100)); !errors.Is(err, ErrUnknownSession) {
		t.Fatalf("a second caller on an expired busy session got %v, want ErrUnknownSession", err)
	}
	for i, b := range bufs {
		if allZero(b) {
			t.Fatalf("key buffer %d of a BUSY session was wiped by another goroutine. "+
				"advance runs outside st.mu and cmdChangeKeySDMFileRead passes the plaque key "+
				"slice to EV2ChangeKeyCommand unlocked, so this is a data race on live key "+
				"material; the same wipe at Close lands inside sun.Wrap and commits an all-zero "+
				"aes_key_ref", i)
		}
	}
	// The plaque slot must NOT have been released mid-round either — that is the
	// interleaved-CmdCtr mode the per-plaque limit exists to prevent.
	h.st.mu.Lock()
	_, held := h.st.perUID[s.uidHex]
	h.st.mu.Unlock()
	if !held {
		t.Fatalf("the plaque slot was released while a round was still in flight")
	}

	// POSITIVE CONTROL: once the owner is done, the session really does die — the
	// guard is a deferral, not an exemption.
	h.st.mu.Lock()
	s.busy = false
	h.st.mu.Unlock()
	if _, err := h.st.Step(context.Background(), id, sw(0x9100)); !errors.Is(err, ErrUnknownSession) {
		t.Fatalf("the expired session was resumable once idle: %v", err)
	}
	assertWiped(t, h.st, id, bufs)
}

// TestStore_RetiringOneSessionDoesNotFreeAnotherPlaqueSlot — n5.
//
// The identity guard in retireLocked (`ok && id == s.id`) is what stops a second
// retire of a stale session object from deleting the perUID entry a DIFFERENT live
// round now owns. Removing it survived the suite — and B-1 made double-retire
// reachable, so the two findings compound.
func TestStore_RetiringOneSessionDoesNotFreeAnotherPlaqueSlot(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	chipA := newFakeChip(t)
	idA, pA, err := h.st.Begin(ctx, testTenant, uuid.Nil, "operator-1")
	if err != nil {
		t.Fatalf("Begin A: %v", err)
	}
	for i := 0; i < 4; i++ {
		pA, err = h.st.Step(ctx, idA, chipA.Transceive(pA.Command))
		if err != nil {
			t.Fatalf("A step %d: %v", i, err)
		}
	}
	stale := liveSession(t, h.st, idA)

	// A finishes; its slot is released.
	h.st.Abort(idA)

	// A NEW round takes the same plaque. The row double enforces the PRIMARY KEY, so
	// the previous row is cleared first — this test is about the in-memory perUID
	// slot, not about the database, and a real re-encode reaches this state through
	// ADR 0003 md. 5's retire + replace.
	h.rows.mu.Lock()
	delete(h.rows.inserted, stale.uidHex)
	h.rows.mu.Unlock()

	chipB := newFakeChip(t)
	idB, pB, err := h.st.Begin(ctx, testTenant, uuid.Nil, "operator-2")
	if err != nil {
		t.Fatalf("Begin B: %v", err)
	}
	for i := 0; i < 4; i++ {
		pB, err = h.st.Step(ctx, idB, chipB.Transceive(pB.Command))
		if err != nil {
			t.Fatalf("B step %d: %v", i, err)
		}
	}
	if got := h.st.perUID[stale.uidHex]; got != idB {
		t.Fatalf("the plaque slot is held by %q, want the new round", got)
	}

	// Now retire the STALE session object a second time. Without the identity guard
	// this deletes B's slot, and a third concurrent round for that plaque becomes
	// possible while B is mid-flight.
	h.st.mu.Lock()
	h.st.retireLocked(stale)
	held := h.st.perUID[stale.uidHex]
	h.st.mu.Unlock()

	if held != idB {
		t.Fatalf("retiring a stale session released the plaque slot of a DIFFERENT live round "+
			"(slot now %q). That reopens the interleaved-CmdCtr mode the per-plaque limit exists "+
			"to prevent", held)
	}
}

// TestSession_ASessionIsOnlyEverConstructedOnce pins the precondition that makes one
// of this package's mutation SURVIVORS legitimate rather than hidden.
//
// Removing `s.cmdCtr = 0` from acceptAuthenticate2 survives the suite. That is
// correct: cmdCtr is a plain field on a Session that Begin allocates fresh, so it is
// already the zero value, and the explicit assignment defends only against a Session
// being REUSED for a second round. Nothing does that — and "nothing does that" is a
// claim about the code, so it gets measured instead of asserted: a Session composite
// literal may appear in exactly one production function.
//
// The day a session is pooled or reset, this goes red and the defensive assignment
// stops being redundant, in the same change.
func TestSession_ASessionIsOnlyEverConstructedOnce(t *testing.T) {
	files := productionFiles(t)

	var sites []string
	for name, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if id, ok := lit.Type.(*ast.Ident); ok && id.Name == "Session" {
				sites = append(sites, name+":"+enclosingFunc(f, lit.Pos()))
			}
			return true
		})
	}
	sort.Strings(sites)
	want := []string{"session.go:Store.Begin"}
	if !reflect.DeepEqual(sites, want) {
		t.Fatalf("Session literals are built at %v, want exactly %v. A session that can be reused "+
			"makes the explicit cmdCtr reset in acceptAuthenticate2 load-bearing, and it is "+
			"currently recorded as a legitimate mutation survivor on the grounds that it is not", sites, want)
	}
}

// --- B-A: Close is the FOURTH sibling ---------------------------------------------

// TestStore_CloseDrainsInFlightStepsInsteadOfWipingUnderThem.
//
// 🔴 THE DEFECT, MEASURED BEFORE THE FIX: Close retired every live session with no
// busy check, so all five key buffers of a mid-step session were zeroed. End to end,
// with a Wrapper that takes real time, the wipe landed INSIDE sun.Wrap and the row
// committed an aes_key_ref that unwraps to sixteen zero bytes — ADR 0003 md. 3
// defeated silently, permanently, in a column tappa_app may never rewrite.
//
// This drives the three branches of the decision Close now makes.
func TestStore_CloseDrainsInFlightStepsInsteadOfWipingUnderThem(t *testing.T) {
	t.Run("an_idle_session_is_wiped_immediately", func(t *testing.T) {
		// POSITIVE CONTROL: the common case still works, so the branches below are
		// about being busy and not about Close having stopped wiping.
		h := newHarness(t)
		chip := newFakeChip(t)
		id, _, _, bufs := armed(t, h, chip, "operator-1")
		if err := h.st.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		assertWiped(t, h.st, id, bufs)
	})

	t.Run("a_busy_session_is_never_wiped_under_its_owner", func(t *testing.T) {
		h := newHarness(t, func(c *Config) { c.CloseGrace = 20 * time.Millisecond })
		chip := newFakeChip(t)
		id, _, _, bufs := armed(t, h, chip, "operator-1")

		s := liveSession(t, h.st, id)
		h.st.mu.Lock()
		s.busy = true
		h.st.mu.Unlock()

		err := h.st.Close()
		if err == nil {
			t.Fatalf("Close reported success while a session was still mid-step")
		}
		for i, b := range bufs {
			if allZero(b) {
				t.Fatalf("key buffer %d was wiped while its owner was still inside advance. "+
					"Measured on the original defect: that wipe lands inside sun.Wrap and commits "+
					"an all-zero aes_key_ref (buffer %d)", i, i)
			}
		}
		// The residue is REPORTED, not swallowed: the count comes back to the caller.
		if !strings.Contains(err.Error(), "mid-step") {
			t.Fatalf("Close's error does not name the residue: %v", err)
		}
		// And it names a COUNT, never a handle or a byte (§4.7).
		if strings.Contains(err.Error(), string(id)) {
			t.Fatalf("Close's error leaks a session handle")
		}

		// Release the synthetic flag so the harness's own t.Cleanup Close can finish
		// the job. That Cleanup is now MEANINGFUL — a second Close no longer returns
		// nil over a live session — so a test that parks a session must un-park it or
		// it fails its own cleanup. That is the assertion working, not noise.
		h.st.mu.Lock()
		s.busy = false
		h.st.mu.Unlock()
		if cerr := h.st.Close(); cerr != nil {
			t.Fatalf("a second Close still reported a residue after the owner was released: %v", cerr)
		}
		for i, b := range bufs {
			if !allZero(b) {
				t.Fatalf("key buffer %d was never wiped, even after the owner was released "+
					"and Close was called again", i)
			}
		}
	})

	t.Run("a_step_that_finishes_within_the_grace_is_waited_for_and_then_wiped", func(t *testing.T) {
		// The branch that makes waiting worth doing: the owner comes back, releases
		// the ring, and Close wipes it properly and reports success.
		h := newHarness(t, func(c *Config) { c.CloseGrace = 5 * time.Second })
		chip := newFakeChip(t)
		id, p, _, bufs := armed(t, h, chip, "operator-1")

		s := liveSession(t, h.st, id)
		h.st.mu.Lock()
		s.busy = true
		h.st.mu.Unlock()

		// Release the session shortly after Close starts waiting, the way a real
		// in-flight step would when its port call returns.
		go func() {
			h.st.mu.Lock()
			s.busy = false
			h.st.signalDrainLocked()
			h.st.mu.Unlock()
		}()

		if err := h.st.Close(); err != nil {
			t.Fatalf("Close did not wait for a step that drained: %v", err)
		}
		assertWiped(t, h.st, id, bufs)
		_ = p
	})
}

// TestStore_CloseDoesNotCommitAnAllZeroKey pins the PERSISTENT consequence of the
// security audit's blocking finding: a wipe landing inside sun.Wrap commits a row
// whose aes_key_ref unwraps to sixteen zero bytes.
//
// 🔴 THE FIRST VERSION OF THIS TEST WAS VACUOUS AND ASSERTED NOTHING (found by the
// fourth audit). Its body ranged over h.rows.inserted — which was measured EMPTY on
// 30 of 30 runs, because Close was called immediately after the goroutine started and
// the round never reached step 3 at all. Its own comment claimed it closed
// "repeatedly, so the shutdown can land in any exchange including the one that wraps
// the key"; there was no repetition, no synchronisation and no way to reach the
// wrapping step. Reverting the production fix left it 60/60 GREEN.
//
// It is deterministic now: blockingWrapper parks the round INSIDE WrapKey — which is
// exactly the window the defect needed — and Close runs while it is parked. The
// empty-set control below is what stops it from ever going vacuous again.
func TestStore_CloseDoesNotCommitAnAllZeroKey(t *testing.T) {
	w := &blockingWrapper{
		kek:     bytesOf(32, 0x2A),
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	h := newHarness(t, func(c *Config) {
		c.Wrapper = w
		c.CloseGrace = 20 * time.Millisecond
	})
	chip := newFakeChip(t)
	ctx := context.Background()

	id, p, err := h.st.Begin(ctx, testTenant, uuid.Nil, "operator-1")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	// Three exchanges take the round to GetVersion frame 3, whose accept mints the
	// key, wraps it and writes the row.
	for i := 0; i < 3; i++ {
		p, err = h.st.Step(ctx, id, chip.Transceive(p.Command))
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}

	stepDone := make(chan struct{})
	go func() {
		defer close(stepDone)
		_, _ = h.st.Step(ctx, id, chip.Transceive(p.Command))
	}()

	<-w.entered // parked inside WrapKey, holding the live plaque key
	_ = h.st.Close()
	close(w.release)
	<-stepDone

	// CONTROL, and it is the reason this test can no longer pass by doing nothing:
	// the round must actually have written a row.
	if len(h.rows.inserted) == 0 {
		t.Fatalf("no row was written at all; this assertion would be vacuous — which is exactly " +
			"how the first version of this test passed against the defect it was written for")
	}

	for uid, ref := range h.rows.inserted {
		opened, uerr := sun.Unwrap(h.kek, chip.uid, ref)
		if uerr != nil {
			t.Fatalf("the row for %s does not open under the chip's own UID: %v", uid, uerr)
		}
		if allZero(opened) {
			t.Fatalf("the row for %s committed an aes_key_ref that unwraps to an all-zero key. "+
				"ADR 0003 md. 3 requires a per-plaque random key, and tappa_app may never rewrite "+
				"that column (migration 00013), so this is permanent and silent", uid)
		}
	}
}

// TestSession_TakeAndPeekReturnTheRingsOWNBuffer — N2/A4.
//
// 🔴 THE ASYMMETRY THIS CLOSES WAS MEASURABLE. Every behavioural exit-path assertion
// in this package reaches the ring through peek, so making peek return a COPY turns
// six tests red. Nothing was anchored to take — and take is what acceptAuthenticate2
// uses for RndA and RndB, which the keyring's own inventory calls key material
// ("whoever holds RndA and RndB and the authentication key holds the session"). A
// take that copied would hand a duplicate to EV2AuthPart2, and that duplicate escapes
// the ring entirely: zeroAll can never reach it, on any exit path.
//
// Aliasing is asserted by MUTATION rather than by pointer comparison, because that is
// the property that matters: writing through what the ring handed out must be visible
// in the ring's own slot, and vice versa.
func TestSession_TakeAndPeekReturnTheRingsOWNBuffer(t *testing.T) {
	for _, tc := range []struct {
		name string
		get  func(*keyring, string) ([]byte, error)
	}{
		{"take", (*keyring).take},
		{"peek", (*keyring).peek},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ring := newKeyring()
			own := bytesOf(16, 0x11)
			if err := ring.add(keyNameRndA, own); err != nil {
				t.Fatalf("add: %v", err)
			}

			got, err := tc.get(ring, keyNameRndA)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if len(got) != len(own) {
				t.Fatalf("%s returned %d bytes, want %d", tc.name, len(got), len(own))
			}

			// Same backing array, both directions.
			got[0] = 0x99
			if own[0] != 0x99 {
				t.Fatalf("%s returned a COPY: a write through it is invisible to the ring. The copy "+
					"escapes the inventory, so zeroAll can never wipe it on any exit path", tc.name)
			}
			own[1] = 0x77
			if got[1] != 0x77 {
				t.Fatalf("%s returned a copy: a write to the ring's slot is invisible through it", tc.name)
			}

			// And the decisive property: zeroAll must reach what was handed out.
			ring.zeroAll()
			if !allZero(got) {
				t.Fatalf("what %s handed out survived zeroAll", tc.name)
			}
		})
	}
}

// retireCallSites is the inventory the prose used to be.
//
// 🔴 THIS EXISTS BECAUSE THE SAME DEFECT APPEARED THREE TIMES IN THIS PACKAGE: a
// CLOSED COUNT written from memory and short by one. B4's "ADR §4 counts the same
// ten" (a different ten) · B-1's "three siblings" (Close was the fourth) · and then
// "exactly the eleven call sites of retireLocked", which did not count Close's
// timeout residue. Each time the number was the thing that was wrong, and each time
// a reader had to re-derive it by hand. So the count is mechanical now.
//
// It ratchets both ways, for the reason cmd/tappa's inventories give: a call site
// disappearing means a retire path was removed, and one appearing means a new way out
// of a session exists that nobody has reasoned about.
// deadlineWriters is expireLocked's counterpart to retireCallSites.
//
// 🔴 IT EXISTS BECAUSE THE "EVERY WRITER GOES THROUGH expireLocked" CLAIM WAS STILL
// PROSE (security audit F4). The three sub-tests of
// TestSession_EveryWriterOfTheDeadlineMovesItEarlier drive the writers that exist;
// nothing caught a FOURTH raw assignment appearing — which is exactly how the last
// closed-count defect happened, twice. The property holds today (measured: one raw
// assignment, inside expireLocked, plus Begin's composite literal at birth), so this
// pins it rather than repairing it.
var deadlineWriters = map[string]int{
	// TWO sites, and both are counted: the raw assignment inside expireLocked, and
	// Begin's composite-literal field. Begin's is birth rather than a write, but it is
	// counted anyway — a scan that skipped it is exactly the scan an auditor walked a
	// deadline-EXTENDING composite literal past.
	"session.go": 2,
}

// ⚠️ WHAT THIS RATCHET MEASURES AND WHAT IT DOES NOT — written beside it because the
// last closed count was replaced by a count that only LOOKED closed. It matches two
// syntactic shapes: `x.deadline = …` (AssignStmt with a SelectorExpr on the left) and
// `deadline: …` inside a composite literal. It does NOT see a write through a pointer
// alias, a write from another package (there is none; the field is unexported), or
// reflection. Those are out of reach of a source scan, and the behavioural half —
// that whatever writes, writes EARLIER — is TestSession_EveryWriterOfTheDeadlineMovesItEarlier.
func TestSession_TheDeadlineHasExactlyOneRawWriter(t *testing.T) {
	files := productionFiles(t)

	got := map[string]int{}
	for name, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, lhs := range as.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "deadline" {
					continue
				}
				got[name]++
			}
			return true
		})
		// 🔴 COMPOSITE LITERALS TOO, AND THEIR ABSENCE WAS A MEASURED HOLE (sixth
		// audit). The AssignStmt walk above matches `x.deadline = …` and nothing else,
		// so an auditor added a SECOND writer — one that EXTENDS the deadline — as a
		// `&Session{… deadline: …}` literal and this test stayed GREEN. What caught it
		// was a different ratchet entirely (TestSession_ASessionIsOnlyEverConstructedOnce),
		// i.e. this one had been credited with a guarantee it did not provide.
		ast.Inspect(f, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "deadline" {
				got[name]++
			}
			return true
		})
	}

	if got["session.go"] == 0 {
		t.Fatalf("no assignment to a deadline field was found at all; the scan has gone blind")
	}
	for name, want := range deadlineWriters {
		if have := got[name]; have != want {
			t.Errorf("%s assigns to .deadline %d time(s), inventory says %d. Every write must go "+
				"through expireLocked, which is what makes DefaultTTL's 'never extended' a "+
				"property instead of a count over scattered sites — and that count has already "+
				"come up short twice", name, have, want)
		}
	}
	for name, n := range got {
		if _, ok := deadlineWriters[name]; !ok {
			t.Errorf("%s assigns to .deadline %d time(s) and is not inventoried", name, n)
		}
	}
	t.Logf("%d raw .deadline assignment(s); every other writer routes through expireLocked",
		got["session.go"])
}

// retireCallSites is the exit-path inventory. Its own reasoning is below; the
// paragraph above belongs to deadlineWriters.
//
// 🔴 IT EXISTS BECAUSE THE SAME DEFECT APPEARED THREE TIMES IN THIS PACKAGE: a CLOSED
// COUNT written from memory and short by one. B4's "ADR §4 counts the same ten" (a
// different ten) · B-1's "three siblings" (Close was the fourth) · and "exactly the
// eleven call sites of retireLocked", which did not count Close's timeout residue.
// Each time the number was the thing that was wrong.
//
// ⚠️ WHAT IT MEASURES: calls spelled `…retireLocked(` in this package's production
// files. NOT: a retire reached through a function value, nor whether each call site
// is correctly guarded — that is the behavioural half, spread across the exit-path
// tests.
var retireCallSites = map[string]int{
	// sweepLocked 1 · Close's retireIdleLocked 1 · checkout ctx 1 · checkout expiry 1 ·
	// finishLocked 4 (error, done, ctx, deadline) · Begin's first-command failure 1 ·
	// Step's panic branch 1 · Abort 1.
	"session.go": 11,
}

func TestSession_TheRetireCallSitesAreInventoried(t *testing.T) {
	files := productionFiles(t)

	got := map[string]int{}
	for name, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "retireLocked" {
				got[name]++
			}
			return true
		})
	}

	// CONTROL: the scan reached real code.
	if got["session.go"] == 0 {
		t.Fatalf("no retireLocked call site found at all; the scan has gone blind")
	}
	for name, want := range retireCallSites {
		if have := got[name]; have != want {
			t.Errorf("%s has %d retireLocked call site(s), inventory says %d. Every one of them is a "+
				"way a session's keys end; if you added one, say HERE which exit it is, and if you "+
				"removed one, say which exit stopped existing", name, have, want)
		}
	}
	for name, n := range got {
		if _, ok := retireCallSites[name]; !ok {
			t.Errorf("%s performs %d retireLocked call(s) and is not inventoried", name, n)
		}
	}
	t.Logf("%d retireLocked call sites across %d inventoried file(s)", got["session.go"], len(retireCallSites))
}

// TestStore_ACloseThatTimesOutStillGetsTheSessionWipedByItsOwner — the blocking
// finding of the security audit, driven end to end with no synthetic flags.
//
// 🔴 THE DEFECT: Close's busy branch counted and continued without touching the
// deadline, so a session that outlived CloseGrace was never wiped — not at the
// timeout, and NOT when its owner returned, because the only remaining retire reason
// for a busy session is finishLocked's deadline check. The sweeper is dead by then,
// so nothing inside the package could ever reach it. Measured on the defect: the
// plain plaque key survived ten times the TTL.
func TestStore_ACloseThatTimesOutStillGetsTheSessionWipedByItsOwner(t *testing.T) {
	w := &blockingWrapper{
		kek:     bytesOf(32, 0x2A),
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	h := newHarness(t, func(c *Config) {
		c.Wrapper = w
		c.CloseGrace = 20 * time.Millisecond
	})
	chip := newFakeChip(t)
	ctx := context.Background()

	id, p, err := h.st.Begin(ctx, testTenant, uuid.Nil, "operator-1")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	// Up to the exchange that writes the row — the one that wraps the key.
	for i := 0; i < 3; i++ {
		p, err = h.st.Step(ctx, id, chip.Transceive(p.Command))
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}

	stepDone := make(chan struct{})
	go func() {
		defer close(stepDone)
		_, _ = h.st.Step(ctx, id, chip.Transceive(p.Command))
	}()

	<-w.entered // the owner is inside WrapKey, holding the ring

	closeErr := h.st.Close()
	if closeErr == nil {
		t.Fatalf("Close reported success while a step was blocked past its grace")
	}
	// The message must describe the design, not the hole: these are wiped by their
	// owner, not abandoned.
	if !strings.Contains(closeErr.Error(), "wiped by their own owner") {
		t.Fatalf("Close's error still describes the residue as abandoned: %v", closeErr)
	}

	close(w.release)
	<-stepDone

	h.st.mu.Lock()
	live := len(h.st.live)
	h.st.mu.Unlock()
	if live != 0 {
		t.Fatalf("%d session(s) survived Close AND their owner's return. Nothing inside this "+
			"package can reach them: the sweeper is stopped and no deadline was moved, so the "+
			"plain plaque key stays for the life of the process", live)
	}
	if len(w.seen) != 1 {
		t.Fatalf("the wrapper saw %d keys", len(w.seen))
	}
	if !allZero(w.seen[0]) {
		t.Fatalf("the plaque key was never wiped: Close timed out and the owner's return did not " +
			"retire the session either")
	}
}

// blockingWrapper holds one WrapKey call open until it is released, so a step can be
// parked mid-flight without a synthetic busy flag. It is the only way to drive
// Close's timeout branch against a REAL in-flight step.
type blockingWrapper struct {
	kek      []byte
	entered  chan struct{}
	release  chan struct{}
	once     bool
	seen     [][]byte
	panicOnR bool
}

func (w *blockingWrapper) WrapKey(uid, plainKey []byte) ([]byte, error) {
	w.seen = append(w.seen, plainKey)
	if !w.once {
		w.once = true
		close(w.entered)
		<-w.release
		if w.panicOnR {
			// A port that blows up mid-step: somebody else's code, which this package
			// must not turn into a key that never gets wiped or a false alarm.
			panic("a port blew up while the store was closing")
		}
	}
	return sun.Wrap(w.kek, uid, plainKey)
}

// TestSession_BothSessionKeysAreRegisteredEvenIfTheFirstAddFails — security audit L2.
//
// 🔴 WRITTEN AS TWO EARLY RETURNS, a failure on the ENC add meant KeyMAC was never
// registered at all: an orphaned session key that zeroAll can never reach, because
// keyring.add only wipes the buffer it itself refuses.
//
// ⚠️ THE PATH IS NOT REACHABLE THROUGH THE STORE TODAY, and the audit measured that:
// advance only advances stepIdx on success and retires the session on error, and
// acceptAuthenticate2 runs at most once per session, so neither slot can already be
// filled. This test therefore reaches it DIRECTLY — which is the honest way to pin a
// defensive branch, and the branch is worth pinning because ADR 0017 §5.1 step 8
// repeats exactly this pattern with two plaque keys the day §6 md. 5 lands.
func TestSession_BothSessionKeysAreRegisteredEvenIfTheFirstAddFails(t *testing.T) {
	h := newHarness(t)
	rndA := bytesOf(16, 0x31)
	rndB := bytesOf(16, 0x32)

	s := &Session{ring: newKeyring()}
	if err := s.ring.add(keyNameRndA, append([]byte(nil), rndA...)); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := s.ring.add(keyNameRndB, append([]byte(nil), rndB...)); err != nil {
		t.Fatalf("add: %v", err)
	}
	// Occupy the ENC slot so its add is refused, exactly as a second call would.
	if err := s.ring.add(keyNameSesENC, bytesOf(16, 0x33)); err != nil {
		t.Fatalf("add: %v", err)
	}

	// The chip's Part 2 response for these randoms, built the way chip_test.go does.
	plain := make([]byte, 0, 32)
	plain = append(plain, 0x9D, 0x00, 0xC4, 0xDF)
	plain = append(plain, rotateLeft1(rndA)...)
	plain = append(plain, make([]byte, 12)...)
	data := cbcEncrypt(t, factoryKey(), make([]byte, 16), plain)

	err := acceptAuthenticate2(context.Background(), h.st, s, data)
	if err == nil {
		t.Fatalf("the occupied ENC slot was accepted")
	}

	mac, perr := s.ring.peek(keyNameSesMAC)
	if perr != nil {
		t.Fatalf("KeyMAC was never registered when the ENC add failed (%v). It is then an ORPHAN: "+
			"keyring.add wipes only what it refuses, so zeroAll can never reach it on any exit "+
			"path. ADR 0017 §5.1 step 8 repeats this pattern with two plaque keys", perr)
	}
	if allZero(mac) {
		t.Fatalf("the registered KeyMAC is all zero; the assertion would be vacuous")
	}

	// And the ring really does end it, which is the point of registering at all.
	s.ring.zeroAll()
	if !allZero(mac) {
		t.Fatalf("the registered KeyMAC survived zeroAll")
	}
}

// TestStore_RetiringAlwaysClearsBusyAndSignalsTheDrain — security audit L1.
//
// 🔴 THE PANIC BRANCH RETIRES WITHOUT GOING THROUGH finishLocked, so before this it
// left s.busy TRUE on a session that was already gone and raised no drain signal. A
// concurrent Close then waited its ENTIRE grace for a signal that could never come
// and returned "0 encode session(s) were still mid-step … NOT wiped" — a §4.7 residue
// alarm over nothing, at a moment (shutdown) when an operator has the least time to
// work out that it is spurious.
func TestStore_RetiringAlwaysClearsBusyAndSignalsTheDrain(t *testing.T) {
	t.Run("the_unit_property", func(t *testing.T) {
		h := newHarness(t)
		chip := newFakeChip(t)
		id, _, _, _ := armed(t, h, chip, "operator-1")
		s := liveSession(t, h.st, id)

		h.st.mu.Lock()
		s.busy = true
		// Drain any signal already queued, so the assertion below is about THIS retire.
		select {
		case <-h.st.drain:
		default:
		}
		h.st.retireLocked(s)
		busy := s.busy
		h.st.mu.Unlock()

		if busy {
			t.Fatalf("a retired session is still marked busy; the sweeper will skip it for ever " +
				"and Close will wait its whole grace for a drain that already happened")
		}
		select {
		case <-h.st.drain:
		default:
			t.Fatalf("retiring a busy session raised no drain signal; a waiting Close cannot learn " +
				"that the thing it was waiting for is gone")
		}
	})

	t.Run("a_panicking_step_does_not_make_Close_report_a_false_residue", func(t *testing.T) {
		// The behavioural half, sequenced so Close is already waiting when the panic
		// happens — which is the only ordering in which the false alarm appears.
		w := &blockingWrapper{
			kek:      bytesOf(32, 0x2A),
			entered:  make(chan struct{}),
			release:  make(chan struct{}),
			panicOnR: true,
		}
		h := newHarness(t, func(c *Config) {
			c.Wrapper = w
			// 30 s, not 5: the grace is the margin against a scheduler stall producing
			// a FALSE FAILURE (see the sleep below), and it is also long enough that a
			// missing drain signal is unmistakable.
			c.CloseGrace = 30 * time.Second
		})
		chip := newFakeChip(t)
		ctx := context.Background()

		id, p, err := h.st.Begin(ctx, testTenant, uuid.Nil, "operator-1")
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		for i := 0; i < 3; i++ {
			p, err = h.st.Step(ctx, id, chip.Transceive(p.Command))
			if err != nil {
				t.Fatalf("step %d: %v", i, err)
			}
		}

		stepDone := make(chan struct{})
		go func() {
			defer close(stepDone)
			defer func() { _ = recover() }()
			_, _ = h.st.Step(ctx, id, chip.Transceive(p.Command))
		}()
		<-w.entered

		closeDone := make(chan error, 1)
		go func() { closeDone <- h.st.Close() }()

		// 🔴 THE ORDERING IS ESTABLISHED WITH A SLEEP, AND THE FIRST VERSION OF THIS
		// TEST HAD NONE — so the panic almost always won the race, Close never got as
		// far as waiting, and deleting the L1 fix left this sub-test 20/20 GREEN. It
		// was catching only the total disappearance of the drain signal, not L1.
		//
		// There is no observable edge for "Close is now inside its wait": exposing one
		// would mean adding a production hook for a test. A sleep buys the ordering
		// instead, and its adequacy is MEASURED rather than assumed — with it, the L1
		// mutation is 5/5 red.
		//
		// 🔴 THE JUSTIFICATION THAT USED TO BE HERE CLAIMED AN IMPOSSIBILITY AND WAS
		// FALSIFIED WITHOUT MUTATING ANY PRODUCTION CODE (fifth audit, 2026-08-21). It
		// said the sleep "cannot flake in the failing direction". Half of that is
		// true — a sleep that is too SHORT only proves less. The other half is false:
		// any pause between the Close goroutine starting and the release that exceeds
		// CloseGrace fails the test, and the auditor produced exactly that by raising
		// the sleep above the grace. So this is a probability, not an impossibility.
		//
		// The margin is what makes it acceptable, and it is now a COUNTED one rather
		// than a denied one: CloseGrace in this sub-test is 30 s against a 200 ms
		// sleep — 150x — so the scheduler would have to stall this goroutine for
		// thirty seconds to produce a false failure. If that ever happens the machine
		// has bigger problems than this test, but the sentence no longer pretends it
		// cannot.
		time.Sleep(200 * time.Millisecond)

		// Now blow the step up, with Close already waiting for a drain signal that
		// only retireLocked can raise.
		close(w.release)
		<-stepDone

		select {
		case cerr := <-closeDone:
			if cerr != nil {
				t.Fatalf("Close reported a residue after the only session had already been retired "+
					"by the panic path: %v", cerr)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("Close never woke up: the panic path retired the session without signalling " +
				"the drain, so Close is waiting out its full grace for nothing")
		}
	})
}

// TestSession_TheCloseGraceIsBoundedByItsOwnComment — audit N1.
//
// 🔴 DefaultCloseGrace WAS THE ONLY DIMENSIONAL CONSTANT IN THIS PACKAGE WITH NO
// GATE: 5s -> 90s and 5s -> 1ns both survived. Its comment makes two RELATIONAL
// claims — "generous relative to ONE exchange (exchangeBudget)" and "short relative
// to a whole round (DefaultTTL)" — so those are what get asserted, rather than the
// literal. The precedent is this package's own: sweepDivisor and DefaultMaxLive were
// pinned for exactly this reason two audits ago, and this constant was missed.
//
// The 90s mutation matters operationally, not just as a number: it makes shutdown
// wait for an entire round, so a rolling deploy would block on one parked encode.
func TestSession_TheCloseGraceIsBoundedByItsOwnComment(t *testing.T) {
	if DefaultCloseGrace < exchangeBudget {
		t.Fatalf("DefaultCloseGrace is %v, under the %v budget for ONE exchange: Close would give "+
			"up before the step it is waiting for could plausibly return, which turns the normal "+
			"case into the timeout case", DefaultCloseGrace, exchangeBudget)
	}
	if DefaultCloseGrace >= DefaultTTL {
		t.Fatalf("DefaultCloseGrace is %v, not short relative to the %v round budget. Close waits "+
			"for a STEP to come back, not for a round to finish; at this size a shutdown blocks "+
			"on one parked session for as long as the session itself may live",
			DefaultCloseGrace, DefaultTTL)
	}
	// And comfortably short: a shutdown that waits a third of a round is not "short".
	if DefaultCloseGrace > DefaultTTL/3 {
		t.Fatalf("DefaultCloseGrace is %v, more than a third of the %v round budget",
			DefaultCloseGrace, DefaultTTL)
	}
}

// cancellingClock cancels a context from inside Now().
//
// 🔴 IT HAS TO BE THE CLOCK, AND THAT IS A PROPERTY OF checkout RATHER THAN A TRICK.
// checkout reads ctx.Err() BEFORE it reads the clock, so a context cancelled by any
// earlier means is caught at custody and the step never runs. Cancelling inside the
// clock read lands the cancellation in the one window that matters: after checkout
// has let the step through, while advance is completing the exchange.
type cancellingClock struct {
	inner  *fakeClock
	cancel func()
	armed  *bool
}

func (c cancellingClock) Now() time.Time {
	if *c.armed {
		c.cancel()
	}
	return c.inner.Now()
}

func (c cancellingClock) NewTicker(d time.Duration) (<-chan time.Time, func()) {
	return c.inner.NewTicker(d)
}

// TestStore_ACancellationOnTheLastExchangeStillReportsDone — eighth audit, B1.
//
// 🔴 THE DEFECT: Step's return ladder checked ctxErr before Done, so a context that
// died during the TENTH exchange produced Progress{} with Done FALSE and "context
// canceled" — while the chip was fully personalised (key 01 installed, NDEF written,
// file settings changed) and MarkEncoded had never been called.
//
// That contradicts Progress.Done's own rule ("READ Done BEFORE READING THE ERROR …
// must NOT be re-run") and contradicts finishLocked, which orders `case done:` ahead
// of `case ctxErr != nil:` in this same function pair.
//
// 🔴 AND THE CHAIN IT OPENS IS THE ONE F3 DOCUMENTS: a caller told "not done" re-runs,
// the re-run dies at the row insert on the PRIMARY KEY, that reads like stale
// inventory, and the obvious fix is deleting the row — which destroys the only stored
// copy of the plaque key. The trigger is ordinary for an HTTP relay: the phone posts
// the final R-APDU and the request context dies.
//
// ⚠️ THE EXISTING CANCELLATION TEST CANCELS AT EXCHANGE 4 OF 10 (inside the row
// insert). This window had never been driven.
func TestStore_ACancellationOnTheLastExchangeStillReportsDone(t *testing.T) {
	rows := newRecordingRows()
	fc := newFakeClock()
	armed := false
	ctx, cancel := context.WithCancel(context.Background())

	st, err := NewStore(Config{
		Rows:    rows,
		Wrapper: testWrapper{kek: bytesOf(32, 0x2A)},
		BaseURL: testBaseURL,
		Clock:   cancellingClock{inner: fc, cancel: cancel, armed: &armed},
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() {
		if cerr := st.Close(); cerr != nil {
			t.Errorf("Close: %v", cerr)
		}
	})

	chip := newFakeChip(t)
	id, p, err := st.Begin(ctx, testTenant, uuid.Nil, "operator-1")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	// Nine of the ten exchanges cleanly.
	for i := 0; i < len(roundSteps)-1; i++ {
		p, err = st.Step(ctx, id, chip.Transceive(p.Command))
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if p.Done {
			t.Fatalf("the round finished early, at exchange %d", i)
		}
	}

	armed = true // the context dies inside the final exchange
	p, err = st.Step(ctx, id, chip.Transceive(p.Command))

	// CONTROL first: the chip really was personalised, so the assertion below is
	// about reporting rather than about a round that failed honestly.
	if allZero(chip.keys[0x01]) || len(chip.files[0x02]) == 0 || len(chip.fileSettingsBody) == 0 {
		t.Fatalf("the chip was not personalised; this test would then prove nothing "+
			"(key01-changed=%v ndef=%d settings=%d)",
			!allZero(chip.keys[0x01]), len(chip.files[0x02]), len(chip.fileSettingsBody))
	}

	if !p.Done {
		t.Fatalf("the chip is personalised and Step reported Done=false (err=%v). Progress.Done's "+
			"own rule is that Done is read BEFORE the error: a caller told 'not done' re-runs, the "+
			"re-run dies on the PRIMARY KEY, and the obvious fix is deleting the row that holds the "+
			"only stored copy of the plaque key", err)
	}

	// A late cancellation still costs the marker, and that is the DOCUMENTED shape:
	// Done true, plus an error saying not to re-run.
	if err == nil {
		t.Logf("MarkEncoded succeeded despite the cancellation; acceptable, the port decides")
	} else if !strings.Contains(err.Error(), "do NOT re-run") {
		t.Fatalf("the round finished but the error does not warn against re-running: %v", err)
	}
	if st.Live() != 0 {
		t.Fatalf("%d sessions live after a completed round", st.Live())
	}
}

// sessionFields is the inventory of Session's fields.
//
// 🔴 IT EXISTS BECAUSE THE go/ast PAIR CANNOT SEE A BUFFER THAT NEVER REACHED THE
// RING, AND THE COMMENT THAT CLAIMED OTHERWISE WAS FALSIFIED (eighth audit). Two
// mutations stashed live key material into an existing byte-slice field and stayed
// green on every exit path. This closes the half that IS mechanisable: a NEW field
// appearing on Session — which is the shape ADR 0017 §5.1 step 8 will take when it
// brings a second plaque key.
//
// ⚠️ WHAT IT DOES NOT CATCH, stated so the count is honest: reuse of one of the
// existing non-secret slices below to hold a secret. Nothing here sees that, and the
// keyring's own doc records it as a counted gap rather than a closed one.
var sessionFields = map[string]string{
	"id":            "bearer handle, not key material (see keyInventory's exclusions)",
	"actor":         "exposure-budget key only, never an authorisation input",
	"tenantID":      "uuid; a SCOPE, not a secret. It IS an authorisation as of FAZ B2c-2b — internal/handler derives it from the resolved panel session (ADR 0017 §6 md. 10, closed) — but nothing in THIS package checks it",
	"adminID":       "uuid; the RESOLVED admin, not a secret. It becomes audit_log.actor_id (internal/encode/rows.go's actorIDOf) and is deliberately separate from `actor`, which is a caller-supplied label",
	"deadline":      "time; every writer routes through expireLocked",
	"busy":          "ownership flag, guarded by st.mu",
	"ring":          "THE key material; everything secret must live here",
	"auth":          "*sun.EV2Auth; its two secret fields are registered in the ring",
	"cmdCtr":        "counter, not secret",
	"lastCtr":       "counter, not secret",
	"uid":           "PUBLIC (ADR 0003 md. 1)",
	"uidHex":        "PUBLIC, canonical form of the above",
	"versionFrames": "GetVersion frames 1 and 2; vendor/version blocks, identical on every chip",
	"authPart2":     "wire value on its way to the phone; see keyring's exclusions",
	"ndef":          "the tap template; internal/sun's TapNDEF says nothing in it is secret",
	"stepIdx":       "position in roundSteps",
	"rowWritten":    "§5.2 runtime guard",
	"finished":      "retired flag",
}

func TestSession_TheSessionFieldsAreInventoried(t *testing.T) {
	typ := reflect.TypeOf(Session{})
	got := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		got[typ.Field(i).Name] = true
	}

	for name := range got {
		if _, ok := sessionFields[name]; !ok {
			t.Errorf("Session has a field %q that is not inventoried. If it holds key material it "+
				"must go through the keyring — nothing else in this package wipes anything — and if "+
				"it does not, say so HERE. ADR 0017 §5.1 step 8's second plaque key is exactly this "+
				"shape", name)
		}
	}
	for name := range sessionFields {
		if !got[name] {
			t.Errorf("sessionFields lists %q, which Session no longer has; lower the inventory in "+
				"the same change", name)
		}
	}
	if len(got) == 0 {
		t.Fatalf("Session has no fields; the scan has gone blind")
	}
	// 🔴 THE SUMMARY IS GUARDED, AND ITS ABSENCE WAS A REAL CONFUSION (ninth audit).
	// t.Logf runs after t.Errorf, so a failing run printed BOTH "field %q is not
	// inventoried" and "N Session fields, all inventoried" — two contradictory lines
	// for whoever reads the failure next.
	if !t.Failed() {
		t.Logf("%d Session fields, all inventoried", len(got))
	}
}

// TestStore_AFinishedProgressCarriesNoCommand — tenth audit, N6.
//
// Progress documents the invariant "Command is the next C-APDU … or nil when the
// round is over", and nothing held it: attaching a Command to a Done progress was
// green. It is the same class this task pinned for s.rowWritten — a documented
// invariant that no test reads.
//
// It matters at the relay: a caller that trusts the field would push one more APDU
// at a chip whose round has finished, in a session the store has already retired.
func TestStore_AFinishedProgressCarriesNoCommand(t *testing.T) {
	h := newHarness(t)
	chip := newFakeChip(t)

	p, err := h.run(t, chip, "operator-1")
	if err != nil {
		t.Fatalf("round: %v", err)
	}
	if !p.Done {
		t.Fatalf("the round did not finish")
	}
	if p.Command != nil {
		t.Fatalf("a Done progress carries a %d-byte Command. Progress documents Command as nil "+
			"once the round is over, and a caller that believes it would push another APDU at a "+
			"chip whose round has finished", len(p.Command))
	}

	// POSITIVE CONTROL: an unfinished progress DOES carry one, so the assertion above
	// is about the finished state and not about Command being always nil.
	ctx := context.Background()
	chip2 := newFakeChip(t)
	chip2.uid = []byte{0x04, 0x96, 0x8C, 0xAA, 0x5C, 0x5E, 0x77}
	id, mid, err := h.st.Begin(ctx, testTenant, uuid.Nil, "operator-2")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if mid.Command == nil {
		t.Fatalf("Begin returned no first command")
	}
	mid, err = h.st.Step(ctx, id, chip2.Transceive(mid.Command))
	if err != nil {
		t.Fatalf("step: %v", err)
	}
	if mid.Done || mid.Command == nil {
		t.Fatalf("a mid-round progress must carry a command (Done=%v, Command=%v)", mid.Done, mid.Command != nil)
	}
}

// TestSource_ACompensatingTrailWriteNeverUsesTheRequestContext generalises a one-site
// fix into the rule that picked the site out.
//
// 🔴 THE CLASS, STATED FOR **DATABASE** WRITES AND NOT ONLY TRAIL WRITES — the ninth
// audit caught the count silently narrowing. A write belongs to the class exactly when
// it is THE ONLY RECORD OF AN IRREVERSIBLE CHANGE ALREADY MADE OUTSIDE THE DATABASE.
// By that test tags.encoded_at qualifies as squarely as any audit row does, and the
// eighth round's count excluded it by ASSERTION ("it is fine for the marker to fail
// here") rather than by derivation. Both are now detached, and the membership list is:
//
//	InsertUnassigned / plaque.loaded   NOT in the class. The database LEADS the chip
//	                                   here, and cmdWriteNDEF's s.rowWritten guard
//	                                   enforces the lead by refusing the chip's first
//	                                   irreversible command without a committed row.
//	Two RecordTx writes                NOT in the class. They live inside the
//	                                   transaction they document; dying with it is
//	                                   correct.
//	MarkEncoded / tags.encoded_at      IN. Detached in markEncoded (ninth round).
//	The compensating Record            IN. Detached in rows.go (eighth round).
//
// ⚠️ THE HONEST SCOPE OF THIS GATE, because the eighth round overstated it and audits
// have now measured the overstatement three times. Every known hole is named here
// rather than left for the next reader to find:
//
//	ONE PACKAGE       productionFiles globs internal/encode only, so the tree's other
//	                  Record calls are outside it. An auditor inspected those by hand:
//	                  none are in the class. The MEMBERSHIP claim holds; the
//	                  ENFORCEMENT claim never covered them.
//	ONE OF TWO        🔴 the class table above has TWO members and this gate inspects
//	MEMBERS           ONE. It scans calls named Record; nothing here checks
//	                  markEncoded's detach, so removing that detach — the ninth
//	                  round's entire product — leaves this test GREEN. Measured
//	                  (tenth audit). The database test is what turns red.
//	ANY ASSIGNMENT    🔴 and the rule is weaker than "first assignment wins", which is
//	MENTIONING IT     how this row read until an audit corrected it. detachedInside
//	                  asks whether the name appears on the LEFT of ANY assignment
//	                  whose RIGHT side mentions WithoutCancel ANYWHERE. So a later
//	                  re-assignment (trailCtx = ctx on the next line) satisfies it for
//	                  ever. Measured: gate PASS, database test FAIL.
//	MULTI-ASSIGN      🔴 THE FIFTH HOLE, AND IT IS NOT A SUBSET OF THE OTHERS
//	IS UNPAIRED       (eleventh audit). detachedInside walks Lhs and Rhs as two
//	                  independent loops with no index, so it never pairs them:
//	                      detachedCtx, trailCtx := context.WithoutCancel(ctx), ctx
//	                  binds trailCtx to the REQUEST while the gate sees WithoutCancel
//	                  somewhere on the right and passes. Not "first assignment wins"
//	                  (there is no valid binding at all) and not "text, not semantics"
//	                  (the gate is wrong about a purely SYNTACTIC fact). Measured:
//	                  gate PASS, database test FAIL.
//	TEXT, NOT         it cannot know what a context MEANS. Three attacks it does hold
//	SEMANTICS         were measured: assigning to another variable and passing the
//	                  request's, discarding to _, and WithCancel(WithoutCancel(ctx)).
//
// What actually holds the property is the pair of database tests — every hole above
// was found by an attack that turned a DATABASE test red while this gate stayed green,
// which is the mitigation working rather than failing. This gate exists to make the
// ORDINARY edit in this package fail loudly.
//
// 🔴 THE TABLE IS THE PRICE OF SAYING THAT HONESTLY, AND IT IS CHEAPER THAN A FIFTH
// DESIGN. "How does a request context reach this call" has an unbounded answer space;
// this task hit that wall five times before it started counting instead, and each
// round since has produced one more spelling nobody had imagined. Counting is the
// stopping rule, and it only works while the count is right — hence five rows.
//
// 🔴 IT ASKS A POSITIVE QUESTION, WHICH IS THE FIX FOR THE FIRST DESIGN. Version one
// forbade one identifier NAME ("ctx"), and the audit beat it by renaming the parameter
// to reqCtx: the gate went green while the database test went red. A blocklist of
// spellings has an unbounded answer space — the same wall this task hit five times.
// This version requires the argument to be DERIVED, in the enclosing function, from
// context.WithoutCancel. A parameter passed straight through fails whatever it is
// called, because it is not derived from anything.
func TestSource_ACompensatingTrailWriteNeverUsesTheRequestContext(t *testing.T) {
	var checked int
	for name, f := range productionFiles(t) {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			// RecordTx is deliberately excluded: it is scoped to a transaction that
			// the same cancellation is entitled to kill.
			if !ok || sel.Sel.Name != "Record" || len(call.Args) == 0 {
				return true
			}
			checked++
			fn := enclosingFuncDecl(f, call.Pos())
			if fn == nil {
				return true
			}
			if detachedInside(fn, call.Args[0]) {
				return true
			}
			t.Errorf("%s: the non-transactional Record in %s is handed a context that is not "+
				"derived from context.WithoutCancel in that function.\n"+
				"This write exists because the operation beside it failed, so it must not be "+
				"able to fail for the same reason. plaqueEncodeStep drives this path with "+
				"r.Context(): a relay that hangs up would take the evidence with the thing it "+
				"is evidence of, and the resulting database is byte-identical to a round that "+
				"never touched a chip — whose recovery instruction is the OPPOSITE one.\n"+
				"Shape: context.WithTimeout(context.WithoutCancel(ctx), DefaultRepairGrace), as "+
				"rows.go and internal/handler/health.go both do.",
				name, enclosingFunc(f, call.Pos()))
			return true
		})
	}
	// CONTROL: the walk found the call it exists for. Renaming Record, or moving it
	// behind a wrapper, would otherwise leave this test green over nothing.
	if checked == 0 {
		t.Fatal("no non-transactional Record call found; this gate has gone blind")
	}
}

// enclosingFuncDecl is enclosingFunc's sibling: the declaration rather than its name.
func enclosingFuncDecl(f *ast.File, pos token.Pos) *ast.FuncDecl {
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if ok && fd.Body != nil && fd.Pos() <= pos && pos <= fd.End() {
			return fd
		}
	}
	return nil
}

// detachedInside reports whether arg is an identifier that the function binds to an
// expression mentioning context.WithoutCancel.
//
// Deliberately textual and deliberately shallow — internal/sun's
// TestEV2_ZeroDisciplineIsInventoried is the precedent for that choice. It holds the
// edit somebody actually makes (pass the parameter through, or rename it) rather than
// a wholesale redesign, and it says so instead of implying more.
func detachedInside(fn *ast.FuncDecl, arg ast.Expr) bool {
	ident, ok := arg.(*ast.Ident)
	if !ok {
		// Not a bare identifier: an inline context.WithTimeout(context.WithoutCancel(..))
		// or similar. Accept only if the expression itself mentions WithoutCancel.
		return mentionsWithoutCancel(arg)
	}
	var found bool
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range as.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || id.Name != ident.Name {
				continue
			}
			for _, rhs := range as.Rhs {
				if mentionsWithoutCancel(rhs) {
					found = true
				}
			}
		}
		return true
	})
	return found
}

func mentionsWithoutCancel(e ast.Expr) bool {
	var found bool
	ast.Inspect(e, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "WithoutCancel" {
			found = true
		}
		return true
	})
	return found
}

// TestSession_TheCheapestExhaustionIsEightTenantsAndTwentyFourAdmins MEASURES the
// residual that DefaultMaxPerTenant's comment prices, instead of restating it.
//
// 🔴 THAT COMMENT HAS NOW BEEN WRONG TWICE BY BEING WRITTEN RATHER THAN RUN. Version
// one quoted the arithmetic of the world BEFORE the cap ("twenty-two tenants at three
// sessions each"). Version two said "the cheapest N is EIGHT sign-ups" and stopped
// there — true about tenants, silent about the fact that one tenant cannot reach its
// own cap of 8 with fewer than ceil(8/3) = 3 actor labels, and that through the
// endpoint an actor label costs an ADMIN ACCOUNT because plaqueEncodeGrantOf derives
// it from the session cookie rather than the body. This test spends the real price
// and reports what it actually took.
func TestSession_TheCheapestExhaustionIsEightTenantsAndTwentyFourAdmins(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	var tenants, admins int
	for h.st.Live() < DefaultMaxLive {
		tenant := uuid.New()
		tenants++
		// Spend the CHEAPEST number of admins for this tenant: keep reusing one label
		// until the per-actor cap refuses it, and only then buy another account.
		for filled := 0; filled < DefaultMaxPerTenant; {
			admin := uuid.New()
			admins++
			actor := "admin:" + admin.String()
			for {
				if _, _, err := h.st.Begin(ctx, tenant, admin, actor); err != nil {
					break // this label is spent, or the store is full
				}
				filled++
				if filled == DefaultMaxPerTenant {
					break
				}
			}
			if h.st.Live() >= DefaultMaxLive {
				break
			}
		}
	}

	if tenants != 8 || admins != 24 {
		t.Errorf("exhausting the store took %d tenant(s) and %d admin account(s); the constants "+
			"imply 8 and 24 (DefaultMaxLive=%d / DefaultMaxPerTenant=%d tenants, each needing "+
			"ceil(%d/%d) actor labels at DefaultMaxPerActor=%d).\n"+
			"If a cap moved, the DefaultMaxPerTenant comment is now quoting a price nobody pays.",
			tenants, admins, DefaultMaxLive, DefaultMaxPerTenant,
			DefaultMaxPerTenant, DefaultMaxPerActor, DefaultMaxPerActor)
	}

	// The store is genuinely full, and a NINTH tenant with a FRESH admin is refused —
	// which is what makes the count above a ceiling rather than a coincidence.
	if live := h.st.Live(); live != DefaultMaxLive {
		t.Fatalf("%d live rounds, want the full %d", live, DefaultMaxLive)
	}
	if _, _, err := h.st.Begin(ctx, uuid.New(), uuid.New(), "admin:"+uuid.NewString()); !errors.Is(err, ErrTooManySessions) {
		t.Errorf("a fresh tenant on a full store got %v, want ErrTooManySessions", err)
	}
}

// TestSession_EveryRefusedOrPanickingRoundReleasesBothCounters drives the two exits
// from a round that retireLocked owns but no test had ever taken.
//
// 🔴 perTenant AND perActor HAD ZERO MENTIONS IN THIS FILE (security audit, second
// pass). The counters were asserted only through their REFUSALS — "the ninth round is
// rejected" — which is the arm that fires when a counter is too HIGH. Nothing drove
// the arm where a counter is never given BACK, and that failure is strictly worse than
// the exhaustion the caps exist to prevent: an exhausted store recovers at the TTL,
// whereas a leaked counter refuses that tenant, or that admin, until the process is
// restarted. retireLocked's own comment claims "every exit releases every counter";
// these are the two exits that claim had never been tested on.
func TestSession_EveryRefusedOrPanickingRoundReleasesBothCounters(t *testing.T) {
	// --- Exit 1: Begin's own error arm ---------------------------------------
	//
	// ⚠️ MEASURED HONESTLY, AND THE HONEST WORD IS "UNREACHABLE TODAY": roundSteps[0]
	// is cmdSelect, which is `return sun.ISOSelectNDEFApplication(), nil` and has no
	// failing branch at all. So this arm is defensive, and the only way to drive it is
	// to bend the table. That is worth doing rather than skipping — the counters are
	// incremented three lines above the call, so a future first step that CAN fail
	// would leak both, and it would leak them at the point where the operator has
	// nothing in their hand yet and will simply press the button again.
	func() {
		h := newHarness(t)
		saved := roundSteps[0].command
		defer func() { roundSteps[0].command = saved }()
		roundSteps[0].command = func(context.Context, *Store, *Session) ([]byte, error) {
			return nil, errors.New("the first step refused")
		}

		tenant, admin := uuid.New(), uuid.New()
		actor := "admin:" + admin.String()
		if _, _, err := h.st.Begin(context.Background(), tenant, admin, actor); err == nil {
			t.Fatal("Begin succeeded although the first step returned an error")
		}
		assertNoCountersHeld(t, h.st, tenant, actor, "a Begin whose first step failed")

		// POSITIVE CONTROL: the tenant and the actor are still usable afterwards. This
		// is the property an operator would actually notice, and it is what a leaked
		// counter takes away.
		roundSteps[0].command = saved
		if _, _, err := h.st.Begin(context.Background(), tenant, admin, actor); err != nil {
			t.Fatalf("the tenant/actor was refused AFTER a failed Begin (%v); the counters "+
				"leaked and this pair is now locked out until the process restarts", err)
		}
	}()

	// --- Exit 2: the panic arm in Step ---------------------------------------
	//
	// Reached through the rows port, which is the one seam in a round that runs
	// arbitrary caller code. The panic is re-raised by design, so the test catches it.
	func() {
		h := newHarness(t)
		chip := newFakeChip(t)
		h.rows.beforeInsert = func() { panic("the row port exploded mid-round") }

		tenant, admin := uuid.New(), uuid.New()
		actor := "admin:" + admin.String()

		var panicked bool
		func() {
			defer func() {
				if r := recover(); r != nil {
					panicked = true
				}
			}()
			ctx := context.Background()
			id, p, err := h.st.Begin(ctx, tenant, admin, actor)
			if err != nil {
				t.Fatalf("Begin: %v", err)
			}
			for i := 0; i < len(roundSteps)+2 && p.Command != nil; i++ {
				p, err = h.st.Step(ctx, id, chip.Transceive(p.Command))
				if err != nil {
					return
				}
			}
		}()
		if !panicked {
			t.Fatal("the round never panicked; the arm under test was not entered")
		}
		assertNoCountersHeld(t, h.st, tenant, actor, "a round that panicked mid-step")
	}()
}

// assertNoCountersHeld reads the three maps directly, because the point is the
// counter and not the refusal it eventually causes.
func assertNoCountersHeld(t *testing.T, st *Store, tenant uuid.UUID, actor, what string) {
	t.Helper()
	st.mu.Lock()
	defer st.mu.Unlock()
	if n := st.perTenant[tenant]; n != 0 {
		t.Errorf("%s left perTenant[%s] = %d, want 0. A counter that is never given back "+
			"refuses this business until the process restarts — worse than the exhaustion the "+
			"cap exists to prevent, because an exhausted store recovers at the TTL", what, tenant, n)
	}
	if n := st.perActor[actor]; n != 0 {
		t.Errorf("%s left perActor[%q] = %d, want 0", what, actor, n)
	}
	if n := len(st.live); n != 0 {
		t.Errorf("%s left %d live session(s), want 0", what, n)
	}
}
