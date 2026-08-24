package encode

// ports_test.go -- the proofs for the three ports' SHAPES and for the two
// implementations that need no database (KEKWrapper and systemClock). The
// database-backed half of Rows is in rows_db_test.go.

import (
	"context"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// --- KARAR 2: the tenant is the ROUND's, and it reaches both ports -------------

// TestDriver_TheRowPortReceivesTheRoundsOwnTenantAndActor.
//
// 🔴 THIS IS THE TEST THE SIGNATURE CHANGE EXISTS FOR, AND IT ASSERTS THREE
// VALUES SEPARATELY RATHER THAN ONE. The hand-over list's item 4 asks for the
// tenant to be an EXPLICIT parameter so CLAUDE.md §4.5's belt cannot be forgotten;
// the compiler enforces that it is PASSED, and only a test can say that what is
// passed is the round's own value rather than some other string that happened to
// be in scope. Two of the three arguments are strings (uidHex, actor), so a
// TRANSPOSED port would compile — which is exactly the mutation this drives.
//
// MUTATION-CHECKED, and the results are in the task's mutation table: passing
// s.actor where s.uidHex belongs turns this red on the uid assertion; passing
// uuid.Nil, uuid.New() or any tenant other than the one Begin was given turns it
// red on the tenant assertion.
func TestDriver_TheRowPortReceivesTheRoundsOwnTenantAndActor(t *testing.T) {
	h := newHarness(t)
	chip := newFakeChip(t)

	const actor = "operator-with-a-recognisable-label"
	if _, err := h.run(t, chip, actor); err != nil {
		t.Fatalf("the round did not complete: %v", err)
	}

	// Derived HERE from the fake chip's own bytes rather than read back from the
	// production path: an assertion against sun's UIDHex would agree with the code
	// under test by construction.
	wantUID := strings.ToUpper(hex.EncodeToString(chip.uid))

	calls := h.rows.calls()
	if len(calls) != 2 {
		t.Fatalf("Rows was called %d time(s), want 2 (insert then mark): %+v", len(calls), calls)
	}
	if calls[0].op != "insert" || calls[1].op != "mark" {
		t.Fatalf("port call order = %s, %s; want insert then mark (ADR 0017 §5.1 steps 3 and 9)",
			calls[0].op, calls[1].op)
	}
	for _, c := range calls {
		if c.tenant != testTenant {
			t.Errorf("%s received tenant %s, want the tenant Begin was given (%s). §4.5's belt is "+
				"only a belt if the value in the statement is the round's own", c.op, c.tenant, testTenant)
		}
		if c.uid != wantUID {
			t.Errorf("%s received uid %q, want the chip's %q -- a transposed argument would "+
				"still compile", c.op, c.uid, wantUID)
		}
		if c.actor != actor {
			t.Errorf("%s received actor %q, want %q", c.op, c.actor, actor)
		}
	}
}

// TestStore_BeginRefusesARoundWithNoTenant.
//
// uuid.Nil is a VALID uuid literal, so without this check it would travel all the
// way to the INSERT and fail there on the tenants FK -- after the chip has been
// selected and its version read, i.e. three exchanges into a round. db.WithTenant
// refuses the nil tenant for the same reason and says so in its own comment.
func TestStore_BeginRefusesARoundWithNoTenant(t *testing.T) {
	h := newHarness(t)
	_, _, err := h.st.Begin(context.Background(), uuid.Nil, "operator-1")
	if err == nil {
		t.Fatal("Begin accepted the nil tenant; a round scoped to a tenant that does not exist " +
			"would fail at the foreign key three exchanges later")
	}
	if !strings.Contains(err.Error(), "tenant") {
		t.Errorf("the refusal is %q, which does not tell the caller which argument was wrong", err)
	}
	if h.st.Live() != 0 {
		t.Errorf("%d session(s) survived a refused Begin; a refused round must hold nothing", h.st.Live())
	}
}

// TestStore_BeginBoundsTheActorLabelBecauseItIsPersistedNow.
//
// 🔴 THE BOUND IS NEW IN B2c-2a AND THE REASON IS NEW TOO. In B2c-1 the actor was
// a map key and nothing else, so its length cost one map entry. It is now copied
// into audit_log.detail (rows.go), and audit_log is append-only for EVERY role
// including tappa_owner (00005's trigger, 00021's TRUNCATE guard) -- so an
// unbounded caller-supplied string would be an unbounded, unremovable write.
//
// REFUSED RATHER THAN TRUNCATED: a truncated label is a value nobody supplied, and
// it would sit in an append-only table looking like something an operator typed.
func TestStore_BeginBoundsTheActorLabelBecauseItIsPersistedNow(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	// POSITIVE CONTROL: exactly at the bound is accepted. Without it, a mutation
	// that refused every actor would pass the negative half.
	atLimit := strings.Repeat("a", MaxActorLen)
	if _, _, err := h.st.Begin(ctx, testTenant, atLimit); err != nil {
		t.Fatalf("an actor of exactly MaxActorLen (%d) was refused: %v", MaxActorLen, err)
	}

	tooLong := strings.Repeat("a", MaxActorLen+1)
	_, _, err := h.st.Begin(ctx, testTenant, tooLong)
	if err == nil {
		t.Fatal("Begin accepted an actor label one byte over MaxActorLen")
	}
	// The message must report a LENGTH, not the label itself: an error that echoed
	// a caller-supplied string back into a log line is how unbounded input becomes
	// unbounded logging.
	if strings.Contains(err.Error(), tooLong) {
		t.Errorf("the refusal echoes the whole label back: %q", err)
	}
}

// TestDBRows_EveryStatementThisPackageCallsNamesTheTenantItself.
//
// 🔴 THIS IS A SOURCE CHECK BECAUSE THE PROPERTY IS INVISIBLE FROM BEHAVIOUR, AND
// THAT WAS MEASURED RATHER THAN ASSUMED — but the FIRST version of this paragraph
// skipped a step, and an audit measured the step (2026-08-24).
//
// It said: "deleting `AND tenant_id = @tenant_id` from MarkTagEncoded leaves the
// whole suite GREEN". Try it literally and the tree does NOT compile: with one
// parameter left, sqlc collapses MarkTagEncodedParams into a bare `string`, and
// rows.go's struct literal stops building. So THE COMPILER TAKES THE FIRST SWING,
// loudly, and that deserves saying rather than being quietly claimed for this test.
//
// WHAT SURVIVES THE COMPILER IS THE MUTATION THAT KEEPS THE ARITY:
//
//	WHERE uid = @uid AND (@tenant_id::uuid IS NOT NULL)
//
// That builds, and THIS package stays green — RLS's USING clause hides the other
// tenant's row anyway, so the statement still matches nothing and still returns
// pgx.ErrNoRows. That is what "belt and braces" means: while the braces hold, the
// belt is unobservable FROM HERE.
//
// 🔴 BUT "THE WHOLE SUITE IS GREEN" WAS WRONG, AND THE CORRECTION IS A GAIN RATHER
// THAN A GAP (audit, 2026-08-24). Measured, that exact mutation:
//
//	internal/encode         ok      <- this gate really does not see it
//	internal/domain/tenant  FAIL    <- query_test.go:332
//	cmd/tappa               ok
//
// TestTagsQueries_CarryAnExplicitTenantPredicate reads db/queries/tags.sql, names
// MarkTagEncoded, and reports "a WHERE clause that does not bind tenant_id to
// @tenant_id as a TOP-LEVEL CONJUNCT" — which is exactly this shape, and its own
// message already says a predicate hidden in an OR or moved to a JOIN … ON does not
// count. THE TREE CATCHES IT; only this file does not.
//
// ⚠️ AND THE PARAGRAPH THAT USED TO FOLLOW ARGUED AGAINST BUILDING A PREDICATE
// PARSER — "the wall two files away spent two audits proving a shape-recogniser over
// SQL cannot be finished; a predicate checker here would be the same bet". That
// argument was made against a parser THAT ALREADY RUNS, over THE SAME FILE, two
// packages away. It is deleted rather than softened: an argument for not building
// something that exists reads as a licence to ship the hole it covers.
//
// WHAT THIS GATE IS, stated at its own size: a cheap, local check that the two
// statements this package calls NAME @tenant_id at all. It catches the parameter
// DISAPPEARING. The parameter being made INERT is caught by internal/domain/tenant.
//
// THIS PACKAGE CALLS TWO STATEMENTS, so the derivation is small: read the file,
// find the two `-- name:` blocks, require each to mention @tenant_id in its BODY.
//
// 🔴 IT IS A SUBSTRING CHECK, NOT A PARSER, AND IT **UNDER**-REPORTS — the previous
// version of this line said "it over-reports rather than under" AND THEN GAVE AN
// UNDER-REPORT AS ITS OWN EXAMPLE, which is the same defect this round corrected in
// capital letters two files away (plaque_db_test.go's key wall). Measured, and the
// mutation is the one somebody would actually write, in sqlc's NAMED-parameter
// syntax rather than the positional form:
//
//	WHERE uid = @uid AND (@tenant_id::uuid IS NOT NULL)
//
// That compiles, keeps the params struct, leaves the suite green — AND SATISFIES
// THIS CHECK, because the string is present. So the honest statement is: this
// catches the parameter DISAPPEARING, not the parameter being made inert. Naming
// a stronger property would be inventing one.
//
// ⚠️ WHY IT IS NOT PARSED ANYWAY: the wall two files away spent two audits proving
// that a shape-recogniser over SQL cannot be finished. A predicate checker here
// would be the same bet. What is bought instead is the cheap half — the deletion —
// with the limit stated rather than papered over.
//
// ANTI-VACUITY: both names must be found. A renamed query makes this fail loudly
// instead of checking nothing, which is the failure mode every scanner in this
// repository has hit at least once.
func TestDBRows_EveryStatementThisPackageCallsNamesTheTenantItself(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../db/queries/tags.sql")
	if err != nil {
		t.Fatalf("reading db/queries/tags.sql: %v", err)
	}
	// The two statements DBRows calls. Named as literals rather than derived from
	// the generated code because the point is to pin THESE two.
	want := map[string]bool{"InsertUnassigned": false, "MarkTagEncoded": false}

	name := ""
	body := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(trimmed, "-- name:"); ok {
			name = strings.Fields(rest + " ")[0]
			continue
		}
		if strings.HasPrefix(trimmed, "--") {
			continue // a comment may name the parameter; only the STATEMENT counts
		}
		if name != "" {
			body[name] += " " + trimmed
		}
	}
	// 🔴 THERE IS NO SEPARATE ANTI-VACUITY COUNTER HERE, AND THE REASON IS THAT TWO
	// OF THEM IN A ROW COULD NOT FIRE (fourth audit, 2026-08-24).
	//
	// The first was `for q, seen := range want` reporting anything "never reached",
	// with `want[q] = true` set inside the loop above it -- every entry was already
	// true by then. I replaced it with `reached++` and `if reached != len(want)`, and
	// THAT WAS THE SAME SHAPE: `reached++` is unconditional on every surviving
	// iteration, and the only way not to survive is the `!found` Fatalf, which
	// Goexits. Measured both times by renaming MarkTagEncoded in db/queries/tags.sql:
	// the failure came from the `!found` Fatalf, never from the counter.
	//
	// 🔴 THE TEST FOR AN ANTI-VACUITY LINE IS ONE QUESTION -- is there an input that
	// makes it fire? If not, the line does not exist, and writing it twice is worse
	// than not writing it, because the second one looks like a fix.
	//
	// WHAT ACTUALLY GUARDS THIS SCAN, and it is derived rather than typed: `want` is
	// built from the two statements THIS PACKAGE CALLS, and every one of them must be
	// found in the file or the Fatalf below fires. That IS the vacuity check -- a
	// renamed or deleted query is exactly what would make this test check nothing, and
	// it is precisely what `!found` catches. MUTATION-CHECKED (M39b, M44): renaming
	// either query turns it red, and it turns red at the Fatalf, which is the honest
	// place.
	for q := range want {
		stmt, found := body[q]
		if !found {
			t.Fatalf("db/queries/tags.sql declares no %s; this scan is reading the wrong file "+
				"or the query was renamed without updating this list. THIS IS ALSO THE VACUITY "+
				"CHECK: if a statement this package calls is not in the file, everything below "+
				"is asserting about nothing", q)
		}
		if !strings.Contains(stmt, "@tenant_id") {
			t.Errorf("%s does not name @tenant_id in its statement. CLAUDE.md §4.5 asks for a "+
				"BELT beside RLS's braces, and the belt is unobservable from behaviour: with the "+
				"policy in place the statement still matches nothing, so no test can see this. "+
				"Statement: %s", q, strings.TrimSpace(stmt))
		}
	}
}

// TestDBRows_AMisSizedEnvelopeIsRefusedBeforeTheDatabaseIsTouched.
//
// 🔴 THE POINT IS *WHERE* THE REFUSAL HAPPENS, NOT THAT IT HAPPENS. Migration
// 00021's tags_aes_key_ref_is_kek_envelope would refuse a 43-byte value too, so a
// test that only asserted "an error came back" stays GREEN with the Go check
// deleted — measured, as mutation M11. What the Go check buys is that the CHECK is
// never reached.
//
// ⚠️ AND THE REASON THAT USED TO BE GIVEN FOR WHY THAT MATTERS IS NARROWED (audit,
// 2026-08-24). It cited 00021's finding that a CHECK violation's DETAIL line is the
// whole failing tuple in the server log. Measured: for a role that is neither
// rolsuper nor rolbypassrls — which is what this path runs as — PostgreSQL emits NO
// DETAIL at all, with RLS active. The DETAIL channel is real for tappa_owner and not
// for tappa_app. What the local check still buys is a fast, local failure with a
// LENGTH in the message and no transaction opened.
//
// So the assertion is that WithTenant is never called at all.
func TestDBRows_AMisSizedEnvelopeIsRefusedBeforeTheDatabaseIsTouched(t *testing.T) {
	t.Parallel()

	var db countingDB
	rows, err := NewDBRows(&db, stubTrail{})
	if err != nil {
		t.Fatalf("NewDBRows: %v", err)
	}
	tenant := uuid.New()
	for _, n := range []int{0, 43, 45, 16} {
		if err := rows.InsertUnassigned(context.Background(), tenant, "04968CAA5C5E80", bytesOf(n, 0x9), "op"); err == nil {
			t.Errorf("a %d-byte envelope was accepted", n)
		}
	}
	if db.calls != 0 {
		t.Errorf("the database was opened %d time(s) for a value that cannot be stored; "+
			"00021's CHECK would then print the whole failing tuple, aes_key_ref included, "+
			"into the server log", db.calls)
	}

	// POSITIVE CONTROL: a correctly sized envelope DOES reach the database. Without
	// it, a port that refused everything would pass the assertion above.
	if err := rows.InsertUnassigned(context.Background(), tenant, "04968CAA5C5E80", bytesOf(sun.WrappedKeyLen, 0x9), "op"); err != nil {
		t.Fatalf("the positive control failed: %v", err)
	}
	if db.calls != 1 {
		t.Errorf("a correct envelope opened the database %d time(s), want 1", db.calls)
	}
}

// countingDB counts WithTenant calls and runs nothing.
type countingDB struct{ calls int }

func (c *countingDB) WithTenant(_ context.Context, _ uuid.UUID, _ db.TxFunc) error {
	c.calls++
	return nil // the callback is deliberately NOT run: there is no tx to give it
}

type stubTrail struct{}

func (stubTrail) RecordTx(context.Context, pgx.Tx, audit.Event) (uuid.UUID, error) {
	return uuid.Nil, nil
}

// --- The Wrapper implementation -------------------------------------------------

// TestKEKWrapper_SealsTheADR0003EnvelopeAgainstTheRawUID.
//
// It is a ROUND TRIP rather than a shape check, because the property that matters
// is not "44 bytes came back" but "these 44 bytes open, under this KEK, against
// THIS uid, to exactly the key that went in" (ADR 0003 md. 4).
func TestKEKWrapper_SealsTheADR0003EnvelopeAgainstTheRawUID(t *testing.T) {
	kek := bytesOf(sun.KEKLen, 0x5C)
	w, err := NewKEKWrapper(kek)
	if err != nil {
		t.Fatalf("NewKEKWrapper: %v", err)
	}
	uid := []byte{0x04, 0xAC, 0x7E, 0x55, 0x00, 0x06, 0x01}
	key := bytesOf(16, 0x11)

	ref, err := w.WrapKey(uid, key)
	if err != nil {
		t.Fatalf("WrapKey: %v", err)
	}
	if len(ref) != sun.WrappedKeyLen {
		t.Fatalf("the envelope is %d bytes, tags.aes_key_ref takes %d", len(ref), sun.WrappedKeyLen)
	}

	back, err := sun.Unwrap(kek, uid, ref)
	if err != nil {
		t.Fatalf("the envelope does not open under the same KEK and uid: %v", err)
	}
	if string(back) != string(key) {
		t.Fatal("the unwrapped key is not the key that was wrapped")
	}

	// 🔴 THE AAD IS THE BINDING AND THIS IS WHAT PROVES IT IS THE **RAW** UID.
	// A wrapper that hashed, hex-encoded or upper-cased the uid before passing it
	// as AAD would still round-trip against itself; it would only diverge from
	// sun.Unwrap, which the tap path uses. So the assertion above (Unwrap with the
	// raw bytes) is the load-bearing one, and this is its negative half.
	other := []byte{0x04, 0xAC, 0x7E, 0x55, 0x00, 0x06, 0x02}
	if _, err := sun.Unwrap(kek, other, ref); err == nil {
		t.Fatal("the envelope opened against a DIFFERENT uid; ADR 0003 md. 4's portability guard " +
			"is what stops one plaque's key from being moved onto another plaque's row")
	}

	// The plain key must not appear in any error this type produces, and the one
	// error it can produce here is a length one.
	if _, err := w.WrapKey(uid, bytesOf(15, 0x11)); err == nil {
		t.Error("a 15-byte plaque key was accepted")
	} else if strings.Contains(err.Error(), "\x11\x11") {
		t.Errorf("the error carries key bytes: %q", err)
	}
}

// TestKEKWrapper_RefusesAMisSizedKEKAtConstruction.
//
// A configuration fault must be a STARTUP failure, not an error in the middle of a
// round with the chip already selected -- the same reasoning NewStore gives for
// building the NDEF template once. The zero value is refused too, because a
// KEKWrapper{} built without the constructor would otherwise forward an empty KEK
// to sun.Wrap and produce an error about configuration from inside the round.
func TestKEKWrapper_RefusesAMisSizedKEKAtConstruction(t *testing.T) {
	for _, n := range []int{0, 16, 24, 31, 33} {
		if _, err := NewKEKWrapper(bytesOf(n, 0x01)); err == nil {
			t.Errorf("a %d-byte KEK was accepted; ADR 0003 md. 4 fixes AES-256, and aes.NewCipher "+
				"would silently take 16 or 24 and downgrade the envelope", n)
		}
	}
	if _, err := NewKEKWrapper(nil); err == nil {
		t.Error("a nil KEK was accepted")
	}

	var zero KEKWrapper
	if _, err := zero.WrapKey([]byte{1, 2, 3, 4, 5, 6, 7}, bytesOf(16, 0x02)); err == nil {
		t.Error("a zero-value KEKWrapper wrapped a key; it holds no KEK")
	}

	// POSITIVE CONTROL.
	if _, err := NewKEKWrapper(bytesOf(sun.KEKLen, 0x01)); err != nil {
		t.Errorf("a correctly sized KEK was refused: %v", err)
	}
}

// TestKEKWrapper_HoldsConfigsSliceRatherThanACopy.
//
// 🔴 IT PINS A SECURITY DECISION THAT LOOKS LIKE AN IMPLEMENTATION DETAIL.
// internal/sun's header states the rule -- "the only long-lived copy of the KEK is
// config's" -- and a constructor that copied the slice would make a SECOND
// long-lived copy, unreachable from anything that wipes the first. The observable
// consequence of NOT copying is that a mutation of the caller's slice is visible
// here, which is what this asserts.
//
// ⚠️ IT IS ALSO THE COUNTED COST, and the test is the honest record of it: a caller
// that zeroes the slice it handed in breaks every subsequent WrapKey and this type
// cannot notice. Nothing in this repository does that (config.Load builds the slice
// once and hands it out), and the alternative trades a bug nobody has for a key
// copy §4.7 would rather not have.
func TestKEKWrapper_HoldsConfigsSliceRatherThanACopy(t *testing.T) {
	kek := bytesOf(sun.KEKLen, 0x5C)
	w, err := NewKEKWrapper(kek)
	if err != nil {
		t.Fatalf("NewKEKWrapper: %v", err)
	}
	uid := []byte{0x04, 0xAC, 0x7E, 0x55, 0x00, 0x06, 0x01}
	key := bytesOf(16, 0x11)

	kek[0] ^= 0xFF // the caller's slice, mutated after construction
	ref, err := w.WrapKey(uid, key)
	if err != nil {
		t.Fatalf("WrapKey: %v", err)
	}
	if _, err := sun.Unwrap(bytesOf(sun.KEKLen, 0x5C), uid, ref); err == nil {
		t.Fatal("the envelope opened under the ORIGINAL KEK bytes, so the wrapper kept a COPY. " +
			"internal/sun's rule is that config's is the only long-lived copy")
	}
	if _, err := sun.Unwrap(kek, uid, ref); err != nil {
		t.Fatalf("the envelope does not open under the caller's CURRENT KEK bytes either: %v", err)
	}
}

// --- The Clock implementation ---------------------------------------------------

// TestSystemClock_IsTheProductionClockAndItReallyTicks.
//
// 🔴 MEASURED BEFORE WRITING: systemClock is NOT new in this round. It shipped in
// FAZ B2c-1 (session.go) and NewStore installs it whenever Config.Clock is nil, so
// "the production Clock" was already the default. What it did NOT have was a test:
// every other test in this package injects fakeClock, so both methods of the real
// one were unexercised, and a NewTicker that returned a nil channel or a nil stop
// would have been invisible until a server shut down.
//
// It uses REAL time deliberately and is bounded to a few milliseconds. That is the
// opposite of the reason Clock is an interface at all (a 90 s TTL cannot be slept
// through), and it is the whole point: the interface exists so PRODUCTION time can
// be replaced in tests, which leaves exactly one thing that has to be checked
// against the real clock -- the adapter itself.
func TestSystemClock_IsTheProductionClockAndItReallyTicks(t *testing.T) {
	var c Clock = systemClock{}

	before := time.Now()
	got := c.Now()
	if got.Before(before) || got.After(time.Now().Add(time.Second)) {
		t.Errorf("Now() returned %v, which is not between two readings of time.Now()", got)
	}

	ticks, stop := c.NewTicker(time.Millisecond)
	if ticks == nil {
		t.Fatal("NewTicker returned a nil channel; sweepLoop would block on it for ever")
	}
	if stop == nil {
		t.Fatal("NewTicker returned a nil stop; Store.Close calls it unconditionally")
	}
	select {
	case <-ticks:
	case <-time.After(5 * time.Second):
		t.Fatal("the ticker never fired; the sweeper is the only thing that wipes an ABANDONED " +
			"session's plain plaque key (ADR 0017 §6 md. 7)")
	}
	stop()

	// The contract this package relies on, restated as an assertion: Stop must be
	// safe to call exactly once and must NOT close the channel -- sweepLoop's
	// `case _, ok := <-ticks` would otherwise spin on a closed channel, and its
	// quit channel is what actually ends it.
	select {
	case _, ok := <-ticks:
		if !ok {
			t.Error("stop() CLOSED the ticker channel; Clock's contract says it does not, and " +
				"sweepLoop distinguishes the two")
		}
	case <-time.After(20 * time.Millisecond):
		// Expected: stopped, not closed.
	}

	// And the default really is this type, which is what makes the test about
	// PRODUCTION rather than about an unused struct.
	st, err := NewStore(Config{Rows: newRecordingRows(), Wrapper: testWrapper{kek: bytesOf(32, 0x2A)}, BaseURL: testBaseURL})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()
	if _, isSystem := st.clock.(systemClock); !isSystem {
		t.Errorf("a Store built with no Clock uses %T, not systemClock", st.clock)
	}
}
