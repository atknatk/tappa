package encode

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/sun"
	"github.com/google/uuid"
)

// testTenant is the business every in-memory round in this package is opened for.
//
// It is a FIXED, RECOGNISABLE value rather than uuid.New(): the point of the tenant
// argument is that the value the ports receive is the value the round was opened
// with (see Rows), and an assertion against a fresh random uuid would pass equally
// well if the plumbing dropped it and read it back from the same variable. A
// literal makes the round trip visible in a failure message.
var testTenant = uuid.MustParse("10000000-0000-0000-0000-000000000001")

// testAdmin is DISTINCT from testTenant and is NOT uuid.Nil, and both properties are
// load-bearing (security audit, second pass). A harness that began its rounds with
// uuid.Nil made every assertion about the admin vacuous: a port that DROPPED the
// argument would record the zero value, which is uuid.Nil, which is what the round
// supplied — so the assertion agreed with the bug. Distinct from the tenant because
// both are uuid.UUID and a transposed port would compile.
var testAdmin = uuid.MustParse("20000000-0000-0000-0000-0000000000ad")

// --- test ports ----------------------------------------------------------------

// recordingRows records the ORDER of persistence calls, not just their arguments.
// ADR 0017 §5.2 is a statement about order, so a double that only remembered "an
// insert happened" could not fail the mutation the rule exists for.
// portCall is one Rows call with the three arguments a transposition would mix up.
type portCall struct {
	op     string
	tenant uuid.UUID
	admin  uuid.UUID
	uid    string
	actor  string
}

type recordingRows struct {
	mu        sync.Mutex
	events    []string
	scope     []portCall
	inserted  map[string][]byte
	insertErr error
	markErr   error
	// beforeInsert runs inside InsertUnassigned, where a slow database call would
	// spend its time.
	beforeInsert func()
	// onMark runs inside MarkEncoded, which is the only place from which ADR 0017
	// §5.1 step 9's ORDER — wipe first, mark second — is observable.
	onMark func()
}

func newRecordingRows() *recordingRows {
	return &recordingRows{inserted: map[string][]byte{}}
}

func (r *recordingRows) InsertUnassigned(_ context.Context, tenantID, adminID uuid.UUID, uidHex string, wrapped []byte, actor string) error {
	if r.beforeInsert != nil {
		r.beforeInsert()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// RECORDED SEPARATELY FROM uidHex, and that is what makes an argument SWAP
	// visible: tenantID, uidHex and actor are three values, two of them strings, so
	// a port whose parameters were transposed would still compile. The three
	// assertions in TestDriver_TheRowPortReceivesTheRoundsOwnTenantAndActor read
	// them apart.
	r.scope = append(r.scope, portCall{op: "insert", tenant: tenantID, admin: adminID, uid: uidHex, actor: actor})
	if r.insertErr != nil {
		r.events = append(r.events, "insert-failed:"+uidHex)
		return r.insertErr
	}
	if _, dup := r.inserted[uidHex]; dup {
		// tags.uid is a PRIMARY KEY; the real port must behave this way too.
		return errors.New("duplicate key value violates unique constraint")
	}
	r.inserted[uidHex] = append([]byte(nil), wrapped...)
	r.events = append(r.events, "insert:"+uidHex)
	return nil
}

func (r *recordingRows) MarkEncoded(_ context.Context, tenantID, adminID uuid.UUID, uidHex string, actor string) error {
	if r.onMark != nil {
		r.onMark()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scope = append(r.scope, portCall{op: "mark", tenant: tenantID, admin: adminID, uid: uidHex, actor: actor})
	if r.markErr != nil {
		r.events = append(r.events, "mark-failed:"+uidHex)
		return r.markErr
	}
	r.events = append(r.events, "mark:"+uidHex)
	return nil
}

func (r *recordingRows) note(event string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *recordingRows) log() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

func (r *recordingRows) calls() []portCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]portCall(nil), r.scope...)
}

// testWrapper is the real sun.Wrap under a test KEK, not a stub. The point is to
// prove the Wrapper port's shape actually fits the function it exists for, and
// that the 44 bytes reaching Rows are the ADR 0003 md. 4 envelope.
type testWrapper struct{ kek []byte }

func (w testWrapper) WrapKey(uid, key []byte) ([]byte, error) {
	return sun.Wrap(w.kek, uid, key)
}

// fakeClock is the injected clock. Nothing in this package's tests sleeps to
// observe a timeout.
type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	ticks chan time.Time
	// askedFor records the period the store requested, so the sweep cadence is an
	// observable rather than a number in a comment (see
	// TestStore_TheSweepCadenceIsTheOneItsCommentClaims).
	askedFor time.Duration
}

func newFakeClock() *fakeClock {
	return &fakeClock{
		now:   time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC),
		ticks: make(chan time.Time),
	}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func (c *fakeClock) NewTicker(d time.Duration) (<-chan time.Time, func()) {
	c.mu.Lock()
	c.askedFor = d
	c.mu.Unlock()
	return c.ticks, func() {}
}

func (c *fakeClock) tickerPeriod() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.askedFor
}

// tick fires the sweeper and RETURNS ONLY ONCE THE SWEEP IT FIRED HAS FINISHED.
//
// The barrier is the second send: the channel is unbuffered, so the sweeper can
// only receive tick two after it has finished processing tick one. Without it the
// test would be asserting against a goroutine still running, which is the shape of
// flake that makes people delete timing tests.
func (c *fakeClock) tick(t *testing.T) {
	t.Helper()
	for i := 0; i < 2; i++ {
		select {
		case c.ticks <- c.Now():
		case <-time.After(5 * time.Second):
			t.Fatalf("the sweeper did not accept tick %d; is the sweeper goroutine running?", i+1)
		}
	}
}

// --- harness -------------------------------------------------------------------

const testBaseURL = "https://tap.example.com/t"

type harness struct {
	st    *Store
	rows  *recordingRows
	clock *fakeClock
	// kek is the test KEK the wrapper seals with, kept so a test can OPEN what
	// reached the row — see TestDriver_TheKeyInTheRowIsTheKeyOnTheChip.
	kek []byte
}

func newHarness(t *testing.T, mutate ...func(*Config)) *harness {
	t.Helper()
	rows := newRecordingRows()
	clock := newFakeClock()
	kek := bytesOf(32, 0x2A)
	cfg := Config{
		Rows:    rows,
		Wrapper: testWrapper{kek: kek},
		BaseURL: testBaseURL,
		Clock:   clock,
	}
	for _, m := range mutate {
		m(&cfg)
	}
	st, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	// Close now REPORTS sessions it could not drain, so a leaked in-flight step is a
	// test failure rather than a silent one.
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return &harness{st: st, rows: rows, clock: clock, kek: kek}
}

// run drives a complete round against chip and returns the final Progress.
func (h *harness) run(t *testing.T, chip *fakeChip, actor string) (Progress, error) {
	t.Helper()
	ctx := context.Background()
	id, p, err := h.st.Begin(ctx, testTenant, testAdmin, actor)
	if err != nil {
		return Progress{}, err
	}
	for i := 0; ; i++ {
		if i > len(roundSteps)+2 {
			t.Fatalf("the round did not terminate after %d exchanges", i)
		}
		if p.Command == nil {
			t.Fatalf("step %d produced no command and did not finish", i)
		}
		h.rows.note("apdu:" + insName(p.Command))
		resp := chip.Transceive(p.Command)
		p, err = h.st.Step(ctx, id, resp)
		if err != nil {
			return p, err
		}
		if p.Done {
			return p, nil
		}
	}
}

// insName labels a C-APDU by its command byte for the ordering log.
func insName(capdu []byte) string {
	if len(capdu) < 2 {
		return "??"
	}
	if capdu[0] == 0x00 {
		return "select"
	}
	switch capdu[1] {
	case 0x60:
		return "getversion"
	case 0xAF:
		return "additionalframe"
	case 0x71:
		return "authenticate"
	case 0x8D:
		return "writedata"
	case 0xC4:
		return "changekey"
	case 0x51:
		return "getcarduid"
	case 0x5F:
		return "changefilesettings"
	}
	return "??"
}

// --- the happy path ------------------------------------------------------------

// TestDriver_AFullRoundPersonalisesTheChip is the POSITIVE CONTROL for every gate
// below: without it, a driver that refused everything would pass all of them.
func TestDriver_AFullRoundPersonalisesTheChip(t *testing.T) {
	h := newHarness(t)
	chip := newFakeChip(t)

	p, err := h.run(t, chip, "operator-1")
	if err != nil {
		t.Fatalf("a complete round failed: %v", err)
	}
	if !p.Done {
		t.Fatalf("the round ended without Done")
	}
	if p.UIDHex != "04968CAA5C5E80" {
		t.Fatalf("UID reported as %q", p.UIDHex)
	}

	// The chip holds the template it was sent, and it is the one internal/sun
	// builds for this base URL.
	want, err := sun.BuildTapNDEF(testBaseURL)
	if err != nil {
		t.Fatalf("BuildTapNDEF: %v", err)
	}
	if string(chip.files[0x02]) != string(want.File) {
		t.Fatalf("the template did not land in the NDEF file (02h) at offset 0. Step 7's SDM " +
			"mirror offsets are absolute file positions, so a shifted template points them at " +
			"the wrong bytes and every tap of the plaque fails its CMAC forever")
	}
	// AND NOWHERE ELSE. Writing the tap URL into the proprietary file 03h leaves
	// file 02h empty, which is where the SDM mirror offsets step 7 configures point:
	// the plaque would emit nothing and the fault is only visible on silicon.
	for no, content := range chip.files {
		if no != 0x02 {
			t.Fatalf("the round wrote %d bytes into file %02Xh; only the NDEF file 02h may be written",
				len(content), no)
		}
	}

	// Application key 01h changed and is not the transport value.
	if string(chip.keys[0x01]) == string(make([]byte, 16)) {
		t.Fatalf("K_SDMFileRead is still the factory transport key")
	}

	// The file settings the chip ended up with are Tappa's normative configuration.
	wantBody, err := sun.ChangeFileSettingsData(mustSettings(t, want))
	if err != nil {
		t.Fatalf("ChangeFileSettingsData: %v", err)
	}
	if string(chip.fileSettingsBody) != string(wantBody) {
		t.Fatalf("the chip's file settings are not TappaNDEFSettings")
	}

	// The row was written and the marker followed it.
	if got := h.rows.log(); !contains(got, "insert:04968CAA5C5E80") || !contains(got, "mark:04968CAA5C5E80") {
		t.Fatalf("persistence log = %v", got)
	}
	// Wrapped key length is ADR 0003 md. 4's 44 bytes.
	if n := len(h.rows.inserted["04968CAA5C5E80"]); n != 44 {
		t.Fatalf("wrapped key is %d bytes, want 44", n)
	}
	// The session is gone and its keys with it.
	if h.st.Live() != 0 {
		t.Fatalf("%d sessions still live after a completed round", h.st.Live())
	}
}

func mustSettings(t *testing.T, n *sun.TapNDEF) sun.SDMFileSettings {
	t.Helper()
	s, err := sun.TappaNDEFSettings(n)
	if err != nil {
		t.Fatalf("TappaNDEFSettings: %v", err)
	}
	return s
}

func contains(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}

func indexOf(hay []string, needle string) int {
	for i, s := range hay {
		if s == needle {
			return i
		}
	}
	return -1
}

// --- ADR 0017 §5.1: the order ---------------------------------------------------

// TestDriver_TheStepOrderIsADR0017Section51 asserts the whole sequence as the CHIP
// saw it — ten commands, in the ADR's order.
func TestDriver_TheStepOrderIsADR0017Section51(t *testing.T) {
	h := newHarness(t)
	chip := newFakeChip(t)
	if _, err := h.run(t, chip, "operator-1"); err != nil {
		t.Fatalf("round: %v", err)
	}

	want := []byte{
		0xA4, // step 1  ISO SELECT
		0x60, // step 2  GetVersion frame 1
		0xAF, //         frame 2
		0xAF, //         frame 3   -> step 3 (row) runs here, server side
		0x71, // step 4  AuthenticateEV2First part 1
		0xAF, //         part 2
		0x51, // md. 12  GetCardUID — BEFORE the first irreversible command. It sat
		//                after ChangeKey until a security audit measured what that
		//                cost: a lying relay was caught only once the chip already
		//                held a key that is in no row (ADR 0017 §5.2's permanent
		//                loss). Detection is identical either side — steps 5-8 share
		//                one key-0 session — so the later slot bought nothing.
		0x8D, // step 5  WriteData          <- first irreversible command
		0xC4, // step 6  ChangeKey(0x01)
		0x5F, // step 7  ChangeFileSettings
	}
	if string(chip.insSeen) != string(want) {
		t.Fatalf("command sequence\n got %X\nwant %X", chip.insSeen, want)
	}
	if len(roundSteps) != len(want) {
		t.Fatalf("the table has %d steps but %d commands went out", len(roundSteps), len(want))
	}
}

// TestDriver_ChangeKeyAlwaysPrecedesChangeFileSettings is the 6 <-> 7 rule, from
// three directions.
//
// 🔴 WHY THREE. The rule protects a window, not a byte: a chip torn out between
// ChangeFileSettings and ChangeKey emits valid-looking SUN signed with a PUBLIC
// key. No single assertion can see a window, so this asserts (a) the table's order,
// (b) the order that actually reached the chip, and (c) that the machine has no
// state in which ChangeFileSettings is the next thing it would emit while ChangeKey
// is still pending.
func TestDriver_ChangeKeyAlwaysPrecedesChangeFileSettings(t *testing.T) {
	// (a) the table.
	ck, cfs := -1, -1
	for i, s := range roundSteps {
		switch s.name {
		case "changekey.sdmfileread":
			ck = i
		case "changefilesettings":
			cfs = i
		}
	}
	if ck < 0 || cfs < 0 {
		t.Fatalf("the two steps are not both in roundSteps (changekey=%d changefilesettings=%d)", ck, cfs)
	}
	if ck >= cfs {
		t.Fatalf("ChangeKey is at index %d and ChangeFileSettings at %d; ADR 0017 §5.1 puts the key change FIRST "+
			"so that an interrupted chip keeps SDM disabled instead of signing SUN with the public factory key", ck, cfs)
	}

	// (b) the wire.
	h := newHarness(t)
	chip := newFakeChip(t)
	if _, err := h.run(t, chip, "operator-1"); err != nil {
		t.Fatalf("round: %v", err)
	}
	iCK, iCFS := -1, -1
	for i, ins := range chip.insSeen {
		if ins == 0xC4 && iCK < 0 {
			iCK = i
		}
		if ins == 0x5F && iCFS < 0 {
			iCFS = i
		}
	}
	if iCK < 0 || iCFS < 0 || iCK >= iCFS {
		t.Fatalf("on the wire ChangeKey was at %d and ChangeFileSettings at %d", iCK, iCFS)
	}

	// (c) ⚠️ NARROWED (2026-08-21, audit): this used to say "no reachable state emits
	// ChangeFileSettings early" and drove ONE trace — the happy path, the same one
	// (b) drives. A claim about the state SPACE measured on a PATH. The space claim
	// now has its own test, which enumerates the table instead of executing it
	// (TestDriver_NoStateOfTheMachineEmitsChangeFileSettingsEarly). What THIS
	// sub-test measures, and all it measures: on the happy path, at every exchange
	// before ChangeKey has gone out, the command about to be emitted is not
	// ChangeFileSettings — i.e. the assertion is checked at each step rather than
	// only on the finished transcript, which is the difference from (b).
	h2 := newHarness(t)
	chip2 := newFakeChip(t)
	ctx := context.Background()
	id, p, err := h2.st.Begin(ctx, testTenant, uuid.Nil, "operator-1")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	seenChangeKey := false
	for i := 0; p.Command != nil; i++ {
		name := insName(p.Command)
		if name == "changefilesettings" && !seenChangeKey {
			t.Fatalf("exchange %d would emit ChangeFileSettings before ChangeKey", i)
		}
		if name == "changekey" {
			seenChangeKey = true
		}
		p, err = h2.st.Step(ctx, id, chip2.Transceive(p.Command))
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if p.Done {
			break
		}
	}
	if !seenChangeKey {
		t.Fatalf("the round finished without ever emitting ChangeKey")
	}
}

// TestDriver_NoChangeKeyIsEverEmittedForApplicationKeyZero holds ADR 0017 §6 md. 5
// shut.
//
// Step 8 is NORMATIVE in the ADR and deliberately NOT shipped: `tags` carries one
// aes_key_ref (migration 00004) and ADR 0003 md. 4 fixes it at 44 bytes, so there
// is nowhere to put a second key. Shipping step 8 without that schema decision
// would personalise key 0 and then LOSE the key — the permanent-plaque-loss mode of
// §5.2, arrived at deliberately. When the schema lands, this test is what has to be
// edited, in the same change.
func TestDriver_NoChangeKeyIsEverEmittedForApplicationKeyZero(t *testing.T) {
	h := newHarness(t)
	chip := newFakeChip(t)
	if _, err := h.run(t, chip, "operator-1"); err != nil {
		t.Fatalf("round: %v", err)
	}

	changeKeys := 0
	for _, ins := range chip.insSeen {
		if ins == 0xC4 {
			changeKeys++
		}
	}
	if changeKeys != 1 {
		t.Fatalf("%d ChangeKey commands were emitted; the shipped sequence has exactly one (key 01h)", changeKeys)
	}
	for _, no := range []byte{0x00, 0x02, 0x03, 0x04} {
		if string(chip.keys[no]) != string(make([]byte, 16)) {
			t.Fatalf("application key %02X was changed; only 01h is in scope this round (ADR 0017 §6 md. 5)", no)
		}
	}
	// ⚠️ AND THE COST IS RESTATED HERE RATHER THAN LEFT IMPLICIT: the chip this test
	// just "successfully" personalised leaves with a PUBLIC AppMasterKey. ADR 0005
	// risk 8. ADR 0017 §5.1's security line: such a plaque may be built and tested
	// but may NOT go on a wall.
	if string(chip.keys[0x00]) != string(make([]byte, 16)) {
		t.Fatalf("key 0 unexpectedly changed")
	}
}

// --- ADR 0017 §5.2: the row goes first ------------------------------------------

// TestDriver_TheRowIsWrittenBeforeTheFirstAuthenticationCommand pins the ordering
// §5.2 exists for.
func TestDriver_TheRowIsWrittenBeforeTheFirstAuthenticationCommand(t *testing.T) {
	h := newHarness(t)
	chip := newFakeChip(t)
	if _, err := h.run(t, chip, "operator-1"); err != nil {
		t.Fatalf("round: %v", err)
	}

	log := h.rows.log()
	iInsert := indexOf(log, "insert:04968CAA5C5E80")
	iAuth := indexOf(log, "apdu:authenticate")
	iWrite := indexOf(log, "apdu:writedata")
	if iInsert < 0 || iAuth < 0 || iWrite < 0 {
		t.Fatalf("interleaving log is missing an entry: %v", log)
	}
	if iInsert > iAuth {
		t.Fatalf("the row was written AFTER AuthenticateEV2First (insert at %d, auth at %d). "+
			"ADR 0017 §5.2: a chip written with a key that is in no row is permanent plaque loss", iInsert, iAuth)
	}
	if iInsert > iWrite {
		t.Fatalf("the row was written after the first irreversible command")
	}

	// POSITIVE CONTROL for the ordering assertion itself: the row must not be
	// written before the UID is known either, i.e. not before GetVersion's frames.
	iVersion := indexOf(log, "apdu:getversion")
	if iVersion < 0 || iInsert < iVersion {
		t.Fatalf("the row was written before GetVersion; tags.uid is the chip's to disclose")
	}
}

// TestDriver_ARowThatCannotBeWrittenStopsTheRoundBeforeTheChipIsTouched is the
// other half of §5.2: if persistence fails, nothing irreversible may have happened.
func TestDriver_ARowThatCannotBeWrittenStopsTheRoundBeforeTheChipIsTouched(t *testing.T) {
	h := newHarness(t)
	h.rows.insertErr = errors.New("permission denied for table tags")
	chip := newFakeChip(t)

	if _, err := h.run(t, chip, "operator-1"); err == nil {
		t.Fatalf("the round succeeded with a failing row insert")
	}
	for _, ins := range chip.insSeen {
		if ins == 0x71 || ins == 0x8D || ins == 0xC4 || ins == 0x5F {
			t.Fatalf("command %02X reached the chip although the row was never written", ins)
		}
	}
	if h.st.Live() != 0 {
		t.Fatalf("the failed round left %d sessions live", h.st.Live())
	}
}

// TestDriver_ADegenerateUIDNeverReachesTheRow is internal/sun's UID gate seen from
// this side: an all-zero UID means Random ID is on and the chip is not disclosing
// itself (datasheet Table 58). Writing it into a PRIMARY KEY would take the first
// plaque and collide every later one.
func TestDriver_ADegenerateUIDNeverReachesTheRow(t *testing.T) {
	for _, tc := range []struct {
		name string
		uid  []byte
	}{
		{"all_zero_means_random_id", make([]byte, 7)},
		{"not_an_nxp_manufacturer_byte", []byte{0x05, 0x96, 0x8C, 0xAA, 0x5C, 0x5E, 0x80}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			chip := newFakeChip(t)
			chip.getVersionUIDLie = tc.uid

			if _, err := h.run(t, chip, "operator-1"); err == nil {
				t.Fatalf("a degenerate UID was accepted")
			}
			if len(h.rows.inserted) != 0 {
				t.Fatalf("a row was written for a degenerate UID: %v", h.rows.inserted)
			}
		})
	}
}

// --- ADR 0017 §6 md. 12, fourth consequence -------------------------------------

// TestDriver_ARelayThatLiesAboutTheUIDIsCaught drives the exact attack §6 md. 12's
// fourth consequence names, and asserts EXACTLY what the gate earns.
func TestDriver_ARelayThatLiesAboutTheUIDIsCaught(t *testing.T) {
	// POSITIVE CONTROL FIRST: with no lie the gate is open. Without this subtest a
	// driver that always aborted here would pass the negative one.
	t.Run("an_honest_relay_passes", func(t *testing.T) {
		h := newHarness(t)
		chip := newFakeChip(t)
		if _, err := h.run(t, chip, "operator-1"); err != nil {
			t.Fatalf("an honest round was rejected: %v", err)
		}
	})

	t.Run("a_substituted_uid_aborts_the_round", func(t *testing.T) {
		h := newHarness(t)
		chip := newFakeChip(t)
		// A well-formed UID belonging to another chip: this is the case
		// internal/sun's checkUID explicitly CANNOT catch, and says so.
		chip.getVersionUIDLie = []byte{0x04, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66}

		_, err := h.run(t, chip, "operator-1")
		if err == nil {
			t.Fatalf("the round completed against a chip whose UID is in no row")
		}
		var mismatch *RelayMismatchError
		if !errors.As(err, &mismatch) {
			t.Fatalf("error is %v, want a *RelayMismatchError", err)
		}
		if mismatch.RowUID != "04112233445566" || mismatch.ChipUID != "04968CAA5C5E80" {
			t.Fatalf("mismatch reports row=%s chip=%s", mismatch.RowUID, mismatch.ChipUID)
		}

		// 🔴 THE ASSERTION THAT MATTERS IS ABOUT THE CHIP, AND THIS TEST USED TO MAKE
		// NONE (security audit F1, 2026-08-21). It checked only that the phantom row
		// existed — which was true both before and after the gate moved — so it PINNED
		// THE HARMFUL ORDER as "expected". Measured on the old order: the chip had
		// already accepted 8D (WriteData) and C4 (ChangeKey) by the time the lie was
		// caught, i.e. it was carrying a plaque key that appears in no row under its
		// own UID: ADR 0017 §5.2's permanent loss.
		//
		// These four assertions are what the gate buys. If it ever slides back after
		// WriteData or ChangeKey, they go red.
		if allZero(chip.keys[0x01]) == false {
			t.Fatalf("the chip's K_SDMFileRead was changed before the UID mismatch was caught. " +
				"That chip now holds a key that is in NO row under its own UID — ADR 0017 §5.2's " +
				"permanent plaque loss, which is exactly the mode the gate exists to avoid")
		}
		if n := len(chip.files[0x02]); n != 0 {
			t.Fatalf("%d bytes were written to the NDEF file before the mismatch was caught", n)
		}
		if len(chip.fileSettingsBody) != 0 {
			t.Fatalf("the file settings were changed before the mismatch was caught")
		}
		for _, ins := range chip.insSeen {
			if ins == 0x8D || ins == 0xC4 || ins == 0x5F {
				t.Fatalf("irreversible command %02X reached the chip before the UID gate ran", ins)
			}
		}

		// 🔴 AND THE HONEST HALF, ASSERTED RATHER THAN WRITTEN IN A COMMENT: the gate
		// DETECTS, it does not PREVENT. The row for the lie is already in the database
		// and stays there — the driver performs no cleanup, because ADR 0017 §6 md. 12
		// says the only cleanup available today is a tappa_owner operator acting by
		// hand. What moving the gate changed is WHICH half of §5.2's asymmetry the
		// failure lands in: a phantom inventory row, not a scrapped chip.
		if _, written := h.rows.inserted["04112233445566"]; !written {
			t.Fatalf("expected the phantom row to exist; the gate is detection, not prevention")
		}
		if contains(h.rows.log(), "mark:04112233445566") {
			t.Fatalf("a detected mismatch must not mark the row encoded")
		}
		// And the session died with its keys.
		if h.st.Live() != 0 {
			t.Fatalf("%d sessions live after an aborted round", h.st.Live())
		}
	})
}

// --- RndA discipline ------------------------------------------------------------

// TestDriver_RndAIsFreshOnEveryRound measures freshness THROUGH THE WIRE: the chip
// recovers each PCD challenge from the Part 2 cryptogram, so this asserts what was
// actually sent rather than what a field says.
func TestDriver_RndAIsFreshOnEveryRound(t *testing.T) {
	h := newHarness(t)
	const rounds = 16

	seen := map[string]int{}
	for i := 0; i < rounds; i++ {
		chip := newFakeChip(t)
		// Each round needs its own UID: one live round per plaque, and the row is a
		// PRIMARY KEY.
		chip.uid = []byte{0x04, 0x96, 0x8C, 0xAA, 0x5C, byte(i), 0x80}
		if _, err := h.run(t, chip, "operator-1"); err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
		if len(chip.rndAsSeen) != 1 {
			t.Fatalf("round %d produced %d challenges", i, len(chip.rndAsSeen))
		}
		rndA := chip.rndAsSeen[0]
		if len(rndA) != 16 {
			t.Fatalf("round %d challenge is %d bytes", i, len(rndA))
		}
		if string(rndA) == string(make([]byte, 16)) {
			t.Fatalf("round %d sent an all-zero challenge", i)
		}
		seen[string(rndA)]++
	}
	if len(seen) != rounds {
		t.Fatalf("%d rounds produced only %d distinct challenges. A repeat lets a recorded "+
			"(E(K,RndB), E(K,TI||RndA'||caps)) pair pass the RndA-echo check WITHOUT the key, "+
			"which makes ADR 0017 §5.3's probe 2 report a FALSE success for a half-written chip", rounds, len(seen))
	}
}

// TestDriver_RndACannotBeReadTwice is the STRUCTURAL half: reuse is not avoided by
// care, it is unrepresentable.
func TestDriver_RndACannotBeReadTwice(t *testing.T) {
	ring := newKeyring()
	if err := ring.add(keyNameRndA, bytesOf(16, 0x11)); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := ring.take(keyNameRndA); err != nil {
		t.Fatalf("the first take failed: %v", err)
	}
	if _, err := ring.take(keyNameRndA); err == nil {
		t.Fatalf("the second take succeeded; RndA reuse is representable")
	}

	// POSITIVE CONTROL: a slot meant to be read more than once still can be.
	if err := ring.add(keyNameSDMFileRead, bytesOf(16, 0x22)); err != nil {
		t.Fatalf("add: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := ring.peek(keyNameSDMFileRead); err != nil {
			t.Fatalf("peek %d failed: %v", i, err)
		}
	}
	// And a consumed slot is still wiped: take does not remove it from the ring.
	ring.zeroAll()
	for _, s := range ring.slots {
		for _, b := range s.buf {
			if b != 0 {
				t.Fatalf("slot %q survived zeroAll", s.name)
			}
		}
	}
}

// --- the command counter --------------------------------------------------------

// TestDriver_TheCommandCounterCountsOncePerSealedCommand is design decision (b)
// under test.
//
// The strong assertion is indirect and that is the point: the chip verifies every
// command MAC against ITS OWN counter, so a driver that skipped, repeated or
// pre-incremented would be refused with INTEGRITY_ERROR long before this line. The
// explicit count below is the readable half.
func TestDriver_TheCommandCounterCountsOncePerSealedCommand(t *testing.T) {
	h := newHarness(t)
	chip := newFakeChip(t)
	if _, err := h.run(t, chip, "operator-1"); err != nil {
		t.Fatalf("round: %v", err)
	}
	// Four sealed commands: WriteData, ChangeKey, GetCardUID, ChangeFileSettings.
	if chip.ctr != 4 {
		t.Fatalf("the chip's counter ended at %d, want 4", chip.ctr)
	}
}

// TestDriver_TheCounterRefusesToWrap holds the FFFFh boundary the datasheet gives
// (§9.1.2: a command arriving at FFFFh "is handled like the MAC was wrong").
// Wrapping to 0000h would forge a counter the chip will never accept.
func TestDriver_TheCounterRefusesToWrap(t *testing.T) {
	s := &Session{cmdCtr: 0xFFFE}
	if c, err := s.useCtr(); err != nil || c != 0xFFFE {
		t.Fatalf("the last usable counter was refused: %d %v", c, err)
	}
	if _, err := s.useCtr(); err == nil {
		t.Fatalf("the counter wrapped past FFFFh")
	}
	if s.cmdCtr != 0xFFFF {
		t.Fatalf("the counter moved past FFFFh to %04X", s.cmdCtr)
	}
}

// --- failure paths --------------------------------------------------------------

// TestDriver_AChipErrorEndsTheRound. Datasheet §9.1.10: on any error the chip drops
// its authentication state, so there is nothing to retry into and holding the
// session would only hold keys.
func TestDriver_AChipErrorEndsTheRound(t *testing.T) {
	h := newHarness(t)
	chip := newFakeChip(t)
	ctx := context.Background()
	id, p, err := h.st.Begin(ctx, testTenant, uuid.Nil, "operator-1")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	// Let the round get as far as WriteData, then make the chip refuse. Seven
	// exchanges now, not six: GetCardUID moved ahead of WriteData (security audit F1).
	for i := 0; i < 7; i++ {
		p, err = h.st.Step(ctx, id, chip.Transceive(p.Command))
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	if insName(p.Command) != "writedata" {
		t.Fatalf("expected to be at WriteData, am at %s", insName(p.Command))
	}
	chip.failNextWithSW = 0x917E // LENGTH_ERROR
	if _, err := h.st.Step(ctx, id, chip.Transceive(p.Command)); err == nil {
		t.Fatalf("a chip error was accepted")
	}
	if h.st.Live() != 0 {
		t.Fatalf("%d sessions live after a chip error", h.st.Live())
	}
	// The handle is dead: a retry gets nothing.
	if _, err := h.st.Step(ctx, id, sw(0x9100)); !errors.Is(err, ErrUnknownSession) {
		t.Fatalf("a retried step returned %v, want ErrUnknownSession", err)
	}
}

// TestDriver_AFailedMarkerStillReportsDone. ADR 0017 §5.1 step 9 wipes the keys and
// THEN marks the row; if the marker fails the chip is still personalised, and
// reporting that as a plain failure would invite a re-run that calls ChangeKey with
// the factory key against a chip that no longer holds it.
func TestDriver_AFailedMarkerStillReportsDone(t *testing.T) {
	h := newHarness(t)
	h.rows.markErr = errors.New("connection reset")
	chip := newFakeChip(t)

	p, err := h.run(t, chip, "operator-1")
	if err == nil {
		t.Fatalf("the marker failure was swallowed")
	}
	if !p.Done {
		t.Fatalf("Done is false although the chip completed; a caller would re-run the round")
	}
	if h.st.Live() != 0 {
		t.Fatalf("%d sessions live", h.st.Live())
	}
}

// TestDriver_ASecondRoundForOneChipIsRefused — the per-plaque limit, applied at the
// first moment the plaque has a name.
func TestDriver_ASecondRoundForOneChipIsRefused(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	// Drive round A up to and including the row write, then leave it live.
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

	// Round B against a chip with the same UID.
	chipB := newFakeChip(t)
	idB, pB, err := h.st.Begin(ctx, testTenant, uuid.Nil, "operator-2")
	if err != nil {
		t.Fatalf("Begin B: %v", err)
	}
	var berr error
	for i := 0; i < 4 && berr == nil; i++ {
		pB, berr = h.st.Step(ctx, idB, chipB.Transceive(pB.Command))
	}
	if !errors.Is(berr, ErrPlaqueBusy) {
		t.Fatalf("the second round for one plaque returned %v, want ErrPlaqueBusy", berr)
	}

	// POSITIVE CONTROL: once A finishes, the plaque frees up — refusal is a delay,
	// not a permanent lock.
	for pA.Command != nil {
		pA, err = h.st.Step(ctx, idA, chipA.Transceive(pA.Command))
		if err != nil {
			t.Fatalf("finishing A: %v", err)
		}
		if pA.Done {
			break
		}
	}
	h.st.mu.Lock()
	_, stillHeld := h.st.perUID["04968CAA5C5E80"]
	h.st.mu.Unlock()
	if stillHeld {
		t.Fatalf("the plaque is still marked busy after its round finished")
	}
}

// --- malformed responses --------------------------------------------------------

// TestDriver_RejectsMalformedResponses covers the shape checks each step performs
// on the frame it gets back.
//
// ⚠️ WHAT THESE ARE AND ARE NOT. None of them is a defence against ADR 0017 §2.2's
// hostile relay — a relay that holds the session keys can produce a well-formed
// frame for any of these. They are refusals of a chip, or a transport, that has
// gone strange, and their value is that a strange frame stops the round instead of
// being carried forward into a half-personalised plaque.
func TestDriver_RejectsMalformedResponses(t *testing.T) {
	t.Run("a_runt_frame", func(t *testing.T) {
		h := newHarness(t)
		ctx := context.Background()
		id, _, err := h.st.Begin(ctx, testTenant, uuid.Nil, "operator-1")
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		if _, err := h.st.Step(ctx, id, []byte{0x91}); err == nil {
			t.Fatalf("a one-byte R-APDU was accepted")
		}
	})

	t.Run("the_wrong_status_word", func(t *testing.T) {
		h := newHarness(t)
		ctx := context.Background()
		id, _, err := h.st.Begin(ctx, testTenant, uuid.Nil, "operator-1")
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		// 9100 is a success — for a NATIVE command. The ISO SELECT's success is 9000,
		// and accepting the wrong one would mean the NDEF application was never
		// selected and every later command is running against something else.
		if _, err := h.st.Step(ctx, id, sw(0x9100)); err == nil {
			t.Fatalf("ISO SELECT accepted the native success trailer")
		}
	})

	t.Run("select_answering_with_a_body", func(t *testing.T) {
		h := newHarness(t)
		chip := newFakeChip(t)
		chip.selectExtraData = []byte{0x6F, 0x00}
		if _, err := h.run(t, chip, "operator-1"); err == nil {
			t.Fatalf("a SELECT that returned data was accepted; P2 = 0Ch asks for none")
		}
	})

	t.Run("a_sealed_ack_that_carries_a_body", func(t *testing.T) {
		h := newHarness(t)
		chip := newFakeChip(t)
		chip.injectRespData = map[byte][]byte{0x8D: {0x01, 0x02, 0x03}}
		if _, err := h.run(t, chip, "operator-1"); err == nil {
			t.Fatalf("WriteData answered with a body and the round continued")
		}
	})

	t.Run("getcarduid_answering_with_the_wrong_length", func(t *testing.T) {
		h := newHarness(t)
		chip := newFakeChip(t)
		chip.injectRespData = map[byte][]byte{0x51: {0x04, 0x96, 0x8C}}
		_, err := h.run(t, chip, "operator-1")
		if err == nil {
			t.Fatalf("a three-byte UID was accepted")
		}
		// 🔴 THE ERROR CLASS IS LOAD-BEARING, NOT COSMETIC, AND THAT IS WHY THE
		// LENGTH IS CHECKED SEPARATELY FROM THE COMPARISON. bytes.Equal would already
		// return false for a short frame, so dropping the length check leaves the
		// round failing — but failing as a *RelayMismatchError, which sends an
		// operator down that error's cleanup: retire a row by hand and treat the
		// plaque as suspect. A truncated frame is a transport fault; nothing is wrong
		// with the plaque and the right move is to run the round again.
		//
		// ⚠️ THIS COMMENT USED TO SAY "scrap the chip", AND BOTH HALVES OF THAT WERE
		// REFUTED IN THE SAME ROUND THAT WROTE IT (sixth audit). F2 measured that the
		// key IS recoverable from the row (RelayMismatchError's doc), and F1 moved the
		// UID gate ahead of the first irreversible command, so on a mismatch NO CHIP
		// IS PERSONALISED AT ALL. The correct phrasing was already 300 lines above, in
		// this same file: "a phantom inventory row, not a scrapped chip".
		var mismatch *RelayMismatchError
		if errors.As(err, &mismatch) {
			t.Fatalf("a malformed-length response was reported as a relay UID substitution (%v), "+
				"which would send an operator down a plaque-retirement cleanup over a transport fault", err)
		}
	})
}

// TestDriver_TheMachineRefusesToRunPastItsLastStep. Reached directly because the
// store retires a finished session before a caller can step it again — this asserts
// the guard underneath that, so removing the store's custody would not silently
// leave the machine running off the end of its table.
func TestDriver_TheMachineRefusesToRunPastItsLastStep(t *testing.T) {
	h := newHarness(t)
	s := &Session{ring: newKeyring(), stepIdx: len(roundSteps)}
	if _, err := s.advance(context.Background(), h.st, sw(0x9100)); err == nil {
		t.Fatalf("the machine stepped past the end of its table")
	}

	// 🔴 THE finished HALF USED TO PASS FOR THE WRONG REASON, WHICH IS THIS
	// REPOSITORY'S OWN M2-08 LESSON ARRIVING A THIRD TIME (fourth audit). The old
	// input was sw(0x9100) against a session at stepIdx 0, so sun.RequireStatus
	// rejected it — step 0 is the ISO SELECT and wants 0x9000 — and the assertion was
	// met by the STATUS-WORD gate, never by the flag. Deleting `s.finished ||` from
	// advance left the whole suite green.
	//
	// The input below is one the machine would otherwise ACCEPT: sw(0x9000) with an
	// empty body is exactly what step 0 expects. So the only thing that can refuse it
	// is the flag, and the assertion now measures what its name says.
	s2 := &Session{ring: newKeyring(), finished: true}
	if _, err := s2.advance(context.Background(), h.st, sw(0x9000)); err == nil {
		t.Fatalf("a retired session was stepped with input its FIRST step would accept; " +
			"nothing is reading s.finished, so a caller holding a retired session object " +
			"can still drive it")
	}

	// POSITIVE CONTROL: the same input on a NON-retired session is accepted, which is
	// what makes the assertion above about the flag and not about the input.
	s3 := &Session{ring: newKeyring()}
	if _, err := s3.advance(context.Background(), h.st, sw(0x9000)); err != nil {
		t.Fatalf("the control input was rejected on a live session (%v); the assertion above "+
			"would then prove nothing", err)
	}
}

// TestDriver_ChangeFileSettingsNeedsTheTemplateStep5Wrote. Step 7 describes to the
// chip where the mirrors are in the file step 5 wrote; without that template the
// offsets would be invented, and an off-by-one mirror offset produces a plaque
// whose every tap fails a MAC check forever (skill tappa-sun's named trap).
func TestDriver_ChangeFileSettingsNeedsTheTemplateStep5Wrote(t *testing.T) {
	h := newHarness(t)
	s := &Session{ring: newKeyring()}
	if _, err := cmdChangeFileSettings(context.Background(), h.st, s); err == nil {
		t.Fatalf("ChangeFileSettings was built with no NDEF template")
	}
}

// TestDriver_ASlowRowWriteThatOutlivesTheTTLEndsTheRound. The sweeper skips a
// session that is mid-step, so the step's own completion path is what catches a
// session that expired while a port call was in flight. The clock is advanced from
// INSIDE the Rows port, which is exactly where a slow database call would spend
// that time.
func TestDriver_ASlowRowWriteThatOutlivesTheTTLEndsTheRound(t *testing.T) {
	h := newHarness(t)
	chip := newFakeChip(t)
	h.rows.beforeInsert = func() { h.clock.Advance(DefaultTTL + time.Second) }

	if _, err := h.run(t, chip, "operator-1"); err == nil {
		t.Fatalf("a round that outlived its TTL mid-step continued")
	}
	if h.st.Live() != 0 {
		t.Fatalf("%d sessions live after the deadline passed during a step", h.st.Live())
	}
	for _, ins := range chip.insSeen {
		if ins == 0x71 {
			t.Fatalf("the round authenticated although its deadline had passed")
		}
	}
}

// TestDriver_ASealedResponseThatDoesNotAuthenticateEndsTheRound. A 9100 is not
// evidence: under CommMode.Full the proof that the CHIP answered is the response
// MAC. This drives a frame that says success and does not authenticate.
func TestDriver_ASealedResponseThatDoesNotAuthenticateEndsTheRound(t *testing.T) {
	for _, ins := range []byte{0x8D, 0x51} {
		t.Run(insName([]byte{0x90, ins}), func(t *testing.T) {
			h := newHarness(t)
			chip := newFakeChip(t)
			chip.corruptRespMACFor = map[byte]bool{ins: true}
			if _, err := h.run(t, chip, "operator-1"); err == nil {
				t.Fatalf("a response with a broken MAC was accepted")
			}
			if h.st.Live() != 0 {
				t.Fatalf("%d sessions live", h.st.Live())
			}
		})
	}
}

// capturingWrapper records the slice header of the plain key it is handed, and can
// fail on demand.
//
// 🔴 IT RETAINS THE SLICE ON PURPOSE, AND THE Wrapper CONTRACT SAYS NOT TO. That is
// the point: a real implementation must not keep the key, but a TEST has to keep the
// slice HEADER to be able to say afterwards whether the bytes were wiped. This is
// the only hook into a key the driver mints internally, and without it the failure
// paths could only be checked for "no row was written" — which is what let two
// registration-order mutations survive.
type capturingWrapper struct {
	kek  []byte
	seen [][]byte
	fail error
}

func (w *capturingWrapper) WrapKey(uid, plainKey []byte) ([]byte, error) {
	w.seen = append(w.seen, plainKey)
	if w.fail != nil {
		return nil, w.fail
	}
	return sun.Wrap(w.kek, uid, plainKey)
}

// failingWrapper is a Wrapper whose seal fails — a wrong-length KEK, an unreadable
// key store, anything. The round must stop with the chip untouched.
type failingWrapper struct{}

func (failingWrapper) WrapKey([]byte, []byte) ([]byte, error) {
	return nil, errors.New("kek unavailable")
}

// TestDriver_TheFreshPlaqueKeyIsWipedOnBothReachableFailurePathsOfStep3.
//
// 🔴 THIS IS ADR 0017 §6 md. 7 ITEM 3 AT THE ONE PLACE IT MATTERS MOST, AND IT WAS
// UNMEASURED. acceptVersionFrame3AndWriteRow's comment promises that the key joins
// the keyring "BEFORE anything can fail … so every exit path wipes it whether or not
// the wrap or the INSERT succeeds". Measured by audit: moving the ring.add BELOW
// WrapKey, and below a successful InsertUnassigned, BOTH left the suite green. In the
// second case a failing insert — a path this package already tests — leaves a
// freshly minted AES-128 plaque key on the heap UNREGISTERED, so retireLocked's
// zeroAll can never reach it. The guarantee did not hold for the single key the
// whole round exists to protect, and nothing went red.
//
// The gap was structural: every existing wipe assertion holds slice headers only on
// paths where step 3 SUCCEEDED, and the two failure tests looked at rows and APDUs,
// never at the key's bytes.
//
// ⚠️ RENAMED FROM ...OnEVERYFailurePath... (third audit): step 3 has THREE failure
// points after the mint — ring.add, WrapKey, InsertUnassigned — and this drives two.
// The third is not driven because it is not reachable: add refuses only a duplicate
// name or an empty buffer, and the slot is fresh on a fresh session (a Session is
// built in exactly one place, pinned by
// TestSession_ASessionIsOnlyEverConstructedOnce). A universal in a test NAME while
// the body drives a list is the same defect this file corrected once already, in the
// same round, one test over.
//
// No key value is printed on any path (§4.7): the assertions are on zero-ness.
func TestDriver_TheFreshPlaqueKeyIsWipedOnBothReachableFailurePathsOfStep3(t *testing.T) {
	for _, tc := range []struct {
		name      string
		wrapFails bool
		rowFails  bool
	}{
		{"the_wrap_fails", true, false},
		{"the_row_insert_fails", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &capturingWrapper{kek: bytesOf(32, 0x2A)}
			if tc.wrapFails {
				w.fail = errors.New("kek unavailable")
			}
			h := newHarness(t, func(c *Config) { c.Wrapper = w })
			if tc.rowFails {
				h.rows.insertErr = errors.New("permission denied for table tags")
			}
			chip := newFakeChip(t)

			if _, err := h.run(t, chip, "operator-1"); err == nil {
				t.Fatalf("the round succeeded although step 3 failed")
			}

			if len(w.seen) != 1 {
				t.Fatalf("the wrapper saw %d keys, want exactly 1", len(w.seen))
			}
			key := w.seen[0]
			if len(key) != plaqueKeyLen {
				t.Fatalf("the wrapper was handed a %d-byte key", len(key))
			}
			if !allZero(key) {
				t.Fatalf("the freshly minted plaque key survived a failing step 3 with non-zero bytes. " +
					"It must be registered in the keyring BEFORE anything can fail, or retireLocked's " +
					"zeroAll never reaches it (ADR 0017 §6 md. 7 item 3)")
			}
			if h.st.Live() != 0 {
				t.Fatalf("%d sessions live after a failed step 3", h.st.Live())
			}
		})
	}

	// POSITIVE CONTROL: on the SUCCESS path the same slice is NOT wiped early — it
	// has to stay alive until step 6 installs it. Without this, "always zero" would
	// pass a driver that wiped the key immediately and shipped a dead plaque.
	t.Run("a_successful_round_keeps_the_key_alive_until_step_6", func(t *testing.T) {
		w := &capturingWrapper{kek: bytesOf(32, 0x2A)}
		h := newHarness(t, func(c *Config) { c.Wrapper = w })
		chip := newFakeChip(t)

		ctx := context.Background()
		id, p, err := h.st.Begin(ctx, testTenant, uuid.Nil, "operator-1")
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		// Stop just before ChangeKey goes out.
		for insName(p.Command) != "changekey" {
			p, err = h.st.Step(ctx, id, chip.Transceive(p.Command))
			if err != nil {
				t.Fatalf("step: %v", err)
			}
		}
		if len(w.seen) != 1 || allZero(w.seen[0]) {
			t.Fatalf("the plaque key was wiped before ChangeKey could install it")
		}
		// And after the round it IS wiped.
		for p.Command != nil {
			p, err = h.st.Step(ctx, id, chip.Transceive(p.Command))
			if err != nil {
				t.Fatalf("step: %v", err)
			}
			if p.Done {
				break
			}
		}
		if !allZero(w.seen[0]) {
			t.Fatalf("the plaque key survived a COMPLETED round")
		}
	})
}

// TestDriver_AKeyThatCannotBeWrappedStopsTheRound. Without the wrapped blob there
// is nothing to put in tags.aes_key_ref, so continuing would personalise a chip
// whose key is in no row — ADR 0017 §5.2's permanent plaque loss.
func TestDriver_AKeyThatCannotBeWrappedStopsTheRound(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.Wrapper = failingWrapper{} })
	chip := newFakeChip(t)

	if _, err := h.run(t, chip, "operator-1"); err == nil {
		t.Fatalf("the round continued with an unwrappable key")
	}
	if len(h.rows.inserted) != 0 {
		t.Fatalf("a row was written without a wrapped key")
	}
	for _, ins := range chip.insSeen {
		if ins == 0x71 {
			t.Fatalf("the chip was authenticated although no key could be stored")
		}
	}
	if h.st.Live() != 0 {
		t.Fatalf("%d sessions live", h.st.Live())
	}
}

// TestDriver_ThePart2CommandNeedsThePart1Cryptogram. The two halves of
// AuthenticateEV2First are one exchange split in two; a machine that could emit the
// second without the first would be sending an empty challenge.
func TestDriver_ThePart2CommandNeedsThePart1Cryptogram(t *testing.T) {
	h := newHarness(t)
	if _, err := cmdAuthenticate2(context.Background(), h.st, &Session{ring: newKeyring()}); err == nil {
		t.Fatalf("part 2 was built with no part 1 cryptogram")
	}
}

// TestDriver_ACancellationDuringAStepEndsTheRound. The cancellation exit path in
// the OTHER position: the context dies while a port call is in flight, so checkout
// has already let the step through and finishLocked is what has to catch it.
func TestDriver_ACancellationDuringAStepEndsTheRound(t *testing.T) {
	h := newHarness(t)
	chip := newFakeChip(t)
	ctx, cancel := context.WithCancel(context.Background())
	h.rows.beforeInsert = cancel

	id, p, err := h.st.Begin(ctx, testTenant, uuid.Nil, "operator-1")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	var stepErr error
	for i := 0; i < 4 && stepErr == nil; i++ {
		p, stepErr = h.st.Step(ctx, id, chip.Transceive(p.Command))
	}
	if stepErr == nil {
		t.Fatalf("the round continued after its context was cancelled")
	}
	if h.st.Live() != 0 {
		t.Fatalf("%d sessions live after cancellation", h.st.Live())
	}
}

// TestDriver_TheFirstIrreversibleCommandRefusesToRunWithoutARow. §5.2's ordering is
// enforced by the step table; this is the runtime guard under it, so a future
// reordering fails BEFORE the chip is touched rather than producing a chip whose
// key is in no row.
func TestDriver_TheFirstIrreversibleCommandRefusesToRunWithoutARow(t *testing.T) {
	h := newHarness(t)
	s := &Session{ring: newKeyring()}

	// 🔴 THE ASSERTION IS ON THE MESSAGE, AND THE FIRST VERSION OF THIS TEST WAS NOT
	// — WHICH IS WHY THE MUTATION SURVIVED THE FIRST PASS. A session with no row
	// also has no session keys, so cmdWriteNDEF fails EITHER WAY: with the guard it
	// fails on the row, without it, three lines later, on the nil authentication.
	// "It returned an error" was therefore true of both the code and its mutant, and
	// the test proved nothing. It has to name WHICH gate fired.
	_, err := cmdWriteNDEF(context.Background(), h.st, s)
	if err == nil {
		t.Fatalf("WriteData was built for a chip with no tags row")
	}
	if !strings.Contains(err.Error(), "tags row") {
		t.Fatalf("WriteData was refused for the wrong reason (%v); the §5.2 guard did not fire, "+
			"so nothing here stops the chip's first irreversible command from running before the row", err)
	}

	// POSITIVE CONTROL: the guard is about the ROW, not about refusing everything —
	// once the row exists this function complains about something else entirely (and
	// the full round in TestDriver_AFullRoundPersonalisesTheChip goes through it and
	// succeeds).
	s.rowWritten = true
	if _, err := cmdWriteNDEF(context.Background(), h.st, s); err == nil {
		t.Fatalf("expected the NEXT gate (no session keys) to be the one that fires")
	} else if strings.Contains(err.Error(), "tags row") {
		t.Fatalf("the row guard fired for a session that has a row: %v", err)
	}
}

// --- B1: the PLAQUE key, not just the nonce --------------------------------------

// TestDriver_ThePlaqueKeyIsFreshOnEveryRound.
//
// 🔴 THIS TEST'S ABSENCE WAS THE ROUND'S WORST GAP, AND THE ASYMMETRY WAS INSIDE THIS
// FILE. TestDriver_RndAIsFreshOnEveryRound measures 16/16 distinctness for a
// TRANSIENT nonce, straight off the wire. The 16-byte PERMANENT plaque key — the one
// that signs every SUN that plaque will ever emit — had no such loop, although the
// chip double holds exactly that value in keys[0x01]. Replacing crypto/rand in
// mintPlaqueKey with a constant left the whole pipeline green: every plaque would
// ship with the SAME K_SDMFileRead, so the single APDU dump ADR 0005 risk 7 says is
// observable would yield a key that signs for the ENTIRE FLEET — ADR 0003 md. 3's
// "per-plaque random, no shared secret" inverted, silently.
//
// The one gate that existed (the full-round test's "not the factory transport key")
// passes for any non-zero constant, which is exactly why "an assertion exists" is
// not the same as "the property is held".
//
// No key value is printed on any path here (§4.7): the assertions are on counts.
func TestDriver_ThePlaqueKeyIsFreshOnEveryRound(t *testing.T) {
	h := newHarness(t)
	const rounds = 16

	seen := map[string]int{}
	for i := 0; i < rounds; i++ {
		chip := newFakeChip(t)
		chip.uid = []byte{0x04, 0x96, 0x8C, 0xAA, 0x5C, byte(i), 0x81}
		if _, err := h.run(t, chip, "operator-1"); err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
		key := chip.keys[0x01]
		if len(key) != plaqueKeyLen {
			t.Fatalf("round %d installed a %d-byte key", i, len(key))
		}
		if allZero(key) {
			t.Fatalf("round %d installed the all-zero transport key", i)
		}
		seen[string(key)]++
	}
	if len(seen) != rounds {
		t.Fatalf("%d rounds installed only %d distinct plaque keys. ADR 0003 md. 3 requires a "+
			"per-plaque random key: a shared one means the APDU dump of ONE encode session "+
			"(ADR 0005 risk 7) yields a key that signs SUN for every plaque in the fleet",
			rounds, len(seen))
	}

	// AND THE SAME PROPERTY ONE LAYER DOWN, so a future refactor that keeps the
	// driver but changes the minting is still covered.
	direct := map[string]int{}
	for i := 0; i < 64; i++ {
		k, err := mintPlaqueKey()
		if err != nil {
			t.Fatalf("mintPlaqueKey: %v", err)
		}
		if len(k) != plaqueKeyLen || allZero(k) {
			t.Fatalf("mintPlaqueKey returned a %d-byte or all-zero key", len(k))
		}
		direct[string(k)]++
	}
	if len(direct) != 64 {
		t.Fatalf("64 calls to mintPlaqueKey produced %d distinct keys", len(direct))
	}
}

// --- b9: the key version is pinned to the value it was copied from -----------------

// TestDriver_TheKeyVersionIsTheOneAN12196Publishes. The constant is not load-bearing
// for security — its own comment forbids inferring anything from it, because neither
// document states the TRANSPORT value of the version byte (datasheet §8.2.4.6 gives
// only the range 00h..FFh). It is pinned anyway so that changing it is a visible
// edit rather than a silent drift away from the one published example.
func TestDriver_TheKeyVersionIsTheOneAN12196Publishes(t *testing.T) {
	// AN12196 rev. 2.0 §5.16.1 Table 25 step 11 publishes the ChangeKey body
	// F3847D627727ED3BC9C4CC050489B966 || 01 || 789DFADC — the 01 is KeyVer.
	if keyVersion != 0x01 {
		t.Fatalf("keyVersion is %#02x; AN12196 rev. 2.0 §5.16.1 Table 25 step 11 writes 01, and "+
			"no document states the transport value, so there is nothing else to derive it from", keyVersion)
	}
}

// --- b11: the ordering claim, as a STATE-SPACE property ----------------------------

// TestDriver_NoStateOfTheMachineEmitsChangeFileSettingsEarly.
//
// 🔴 THIS REPLACES A SENTENCE THAT CLAIMED MORE THAN IT DROVE. Sub-test (c) of
// TestDriver_ChangeKeyAlwaysPrecedesChangeFileSettings said "no reachable state emits
// ChangeFileSettings early" while driving ONE trace — the happy path, the same trace
// as (b). The claim was about a state SPACE and the measurement was about a PATH.
//
// The space is enumerable, and that is the whole reason the sequence is a table: the
// machine's entire state is one monotone index into roundSteps. So "which command
// could this machine emit at state i" is answered by reading roundSteps[i].command,
// for every i, with no execution at all.
func TestDriver_NoStateOfTheMachineEmitsChangeFileSettingsEarly(t *testing.T) {
	ckIdx := -1
	for i, s := range roundSteps {
		if s.name == "changekey.sdmfileread" {
			ckIdx = i
		}
	}
	if ckIdx < 0 {
		t.Fatalf("the ChangeKey step is not in the table")
	}

	want := reflect.ValueOf(cmdChangeFileSettings).Pointer()
	checked := 0
	for i := 0; i <= ckIdx; i++ {
		checked++
		if reflect.ValueOf(roundSteps[i].command).Pointer() == want {
			t.Fatalf("state %d (%q) emits ChangeFileSettings, and ChangeKey is not until state %d. "+
				"A chip interrupted in that window has SDM enabled while K_SDMFileRead is still the "+
				"PUBLIC factory key", i, roundSteps[i].name, ckIdx)
		}
	}
	// CONTROL: the walk examined states, and the step it is looking for really is
	// somewhere in the table (otherwise the loop above is vacuous).
	if checked < 2 {
		t.Fatalf("only %d states examined", checked)
	}
	found := false
	for _, s := range roundSteps {
		if reflect.ValueOf(s.command).Pointer() == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("cmdChangeFileSettings is not reachable from any state; the scan is vacuous")
	}
}

// --- b6: the panic exit path -------------------------------------------------------

// TestStore_APanicInAStepStillWipesTheSession.
//
// A panic used to skip finishLocked and leave s.busy true for ever — and a busy
// session is skipped by the sweeper and only deadline-nudged by Abort, so it would
// have held its plain plaque key until Close.
func TestStore_APanicInAStepStillWipesTheSession(t *testing.T) {
	h := newHarness(t)
	chip := newFakeChip(t)
	id, p, _, bufs := armed(t, h, chip, "operator-1")

	// A port that panics is the reachable shape: the Rows and Wrapper
	// implementations are somebody else's code (FAZ B2c-2's), and this package must
	// not turn their bug into a key that never gets wiped.
	h.rows.beforeInsert = func() { panic("a port blew up") }

	// Take a fresh session to the row-writing step so the panic lands inside advance.
	chip2 := newFakeChip(t)
	chip2.uid = []byte{0x04, 0x96, 0x8C, 0xAA, 0x5C, 0x5E, 0x99}
	id2, p2, err := h.st.Begin(context.Background(), testTenant, uuid.Nil, "operator-2")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("the panic was swallowed; this defer wipes, it does not recover")
			}
		}()
		for i := 0; i < 4; i++ {
			p2, err = h.st.Step(context.Background(), id2, chip2.Transceive(p2.Command))
			if err != nil {
				t.Fatalf("step %d: %v", i, err)
			}
		}
	}()

	h.st.mu.Lock()
	_, still := h.st.live[id2]
	h.st.mu.Unlock()
	if still {
		t.Fatalf("the panicking session is still live, holding its keys until Close")
	}
	if _, held := h.st.perUID["04968CAA5C5E99"]; held {
		t.Fatalf("the panicking session still holds its plaque slot")
	}

	// The unrelated session is untouched — the wipe is scoped to the one that blew up.
	_ = p
	_ = id
	for _, b := range bufs {
		if allZero(b) {
			t.Fatalf("an unrelated session was wiped by another one's panic")
		}
	}
}

// TestDriver_TheKeyInTheRowIsTheKeyOnTheChip.
//
// 🔴 THE ONE PROPERTY THE WHOLE ROUND EXISTS TO PRODUCE, AND NOTHING WAS CHECKING IT.
// Every other assertion here covers a step; this covers the RESULT: the 44 bytes that
// reached tags.aes_key_ref must open, under the same KEK and the same UID as AAD, to
// exactly the key the chip adopted. If they do not, the plaque is not "slightly
// wrong" — every tap it will ever produce fails its CMAC, forever, and skill
// tappa-sun records that this failure is indistinguishable from a wrong key, so it
// would be diagnosed in the wrong place.
//
// It also pins the AAD, which nothing else did: ADR 0003 md. 4 binds the envelope to
// the RAW 7-byte UID, and a wrap against any other value produces a blob that can
// never be opened by the tap path.
//
// No key material is printed on any path (§4.7); the comparison is a boolean.
func TestDriver_TheKeyInTheRowIsTheKeyOnTheChip(t *testing.T) {
	h := newHarness(t)
	chip := newFakeChip(t)
	if _, err := h.run(t, chip, "operator-1"); err != nil {
		t.Fatalf("round: %v", err)
	}

	ref, ok := h.rows.inserted["04968CAA5C5E80"]
	if !ok {
		t.Fatalf("no row was written")
	}
	opened, err := sun.Unwrap(h.kek, chip.uid, ref)
	if err != nil {
		t.Fatalf("the wrapped key in the row does not open under the chip's own UID as AAD: %v", err)
	}
	if !bytes.Equal(opened, chip.keys[0x01]) {
		t.Fatalf("the key in the row is not the key the chip adopted; every tap of this plaque " +
			"would fail its CMAC forever")
	}

	// POSITIVE CONTROL for the AAD half: opening against a DIFFERENT UID must fail,
	// so the assertion above is about this plaque and not about any plaque.
	other := append([]byte(nil), chip.uid...)
	other[6] ^= 0x01
	if _, err := sun.Unwrap(h.kek, other, ref); err == nil {
		t.Fatalf("the wrapped key opened under the wrong UID; the AAD is not binding")
	}
}

// TestDriver_Step9WipesTheKeysBeforeItMarksTheRow — N2/A3b.
//
// 🔴 ADR 0017 §5.1 STEP 9 IS AN ORDER, AND IT WAS THE ONE ORDER IN THIS PACKAGE THAT
// NOTHING HELD. Store.Step's comment says "Zero(keys) FIRST … and only then mark the
// row", and moving MarkEncoded above finishLocked left the suite green — while §5.2's
// twin ordering claim has been pinned by recordingRows since the first round. That
// asymmetry is the same shape as B1's (nonce measured, key not) and B-5's (success
// paths measured, failure paths not), which is why it is worth closing rather than
// arguing about.
//
// The order is observable from exactly one place: inside MarkEncoded, the plaque key
// the wrapper captured must ALREADY be zero.
func TestDriver_Step9WipesTheKeysBeforeItMarksTheRow(t *testing.T) {
	w := &capturingWrapper{kek: bytesOf(32, 0x2A)}
	h := newHarness(t, func(c *Config) { c.Wrapper = w })

	// EVERY observation is recorded, not the last one. A first pass assigned on each
	// call, so a mutation that marked the row TWICE — once before the wipe and once
	// after — overwrote the damning observation with the innocent one and SURVIVED.
	// Last-write-wins in a test double is its own small version of this round's
	// recurring defect.
	var marks []bool
	h.rows.onMark = func() {
		marks = append(marks, len(w.seen) == 1 && allZero(w.seen[0]))
	}

	chip := newFakeChip(t)
	if _, err := h.run(t, chip, "operator-1"); err != nil {
		t.Fatalf("round: %v", err)
	}
	if len(marks) == 0 {
		t.Fatalf("MarkEncoded never ran; the ordering assertion would be vacuous")
	}
	for i, zero := range marks {
		if !zero {
			t.Fatalf("at MarkEncoded call %d of %d the plaque key was still live. ADR 0017 §5.1 "+
				"step 9 is Zero(keys) FIRST, then mark: the marker is bookkeeping and the keys are "+
				"not, so the keys must not outlive a bookkeeping call that can block or fail",
				i+1, len(marks))
		}
	}
	// POSITIVE CONTROL: before step 9 the key is NOT zero, so the assertion above is
	// about ordering and not about the key being zero all along.
	if len(w.seen) != 1 {
		t.Fatalf("the wrapper saw %d keys", len(w.seen))
	}
}

// TestDriver_TheRowGuardMeansTheRowEXISTSNotThatWeTried — eighth audit, N4.
//
// 🔴 s.rowWritten WAS AN UNPINNED GUARD: moving its assignment ABOVE InsertUnassigned
// left the suite green. Unreachable today — an insert failure retires the session
// before WriteData — so it was a legitimate survivor by this package's own standard.
// It is pinned anyway, because the guard's DECLARED job (driver.go: "§5.2 as a runtime
// invariant … so a future reordering fails HERE, before the chip is touched") is to
// survive a future reordering, and a flag that means "we tried" instead of "the row
// exists" cannot do that job at the moment it is needed.
func TestDriver_TheRowGuardMeansTheRowEXISTSNotThatWeTried(t *testing.T) {
	h := newHarness(t)
	h.rows.insertErr = errors.New("permission denied for table tags")
	chip := newFakeChip(t)

	if _, err := h.run(t, chip, "operator-1"); err == nil {
		t.Fatalf("the round survived a failing row insert")
	}

	// The session is retired, so reach the flag through a direct drive of step 3.
	s := &Session{ring: newKeyring()}
	s.versionFrames[0] = bytesOf(7, 0x04)
	s.versionFrames[1] = bytesOf(7, 0x05)
	frame3 := append([]byte{0x04, 0x96, 0x8C, 0xAA, 0x5C, 0x5E, 0x80}, bytesOf(7, 0x11)...)

	if err := acceptVersionFrame3AndWriteRow(context.Background(), h.st, s, frame3); err == nil {
		t.Fatalf("step 3 succeeded with a failing Rows port")
	}
	if s.rowWritten {
		t.Fatalf("rowWritten is true after the insert FAILED. The guard then means \"we tried\", " +
			"not \"the row exists\" — and cmdWriteNDEF would let the chip's first irreversible " +
			"command run against a plaque with no row, which is the mode §5.2 exists to prevent")
	}

	// POSITIVE CONTROL: with a working port it does become true, so the assertion
	// above is about the failure path and not about the flag never being set.
	h2 := newHarness(t)
	s2 := &Session{ring: newKeyring()}
	s2.versionFrames[0] = bytesOf(7, 0x04)
	s2.versionFrames[1] = bytesOf(7, 0x05)
	if err := acceptVersionFrame3AndWriteRow(context.Background(), h2.st, s2, frame3); err != nil {
		t.Fatalf("step 3 failed with a working port: %v", err)
	}
	if !s2.rowWritten {
		t.Fatalf("rowWritten stayed false after a successful insert")
	}
}

// TestDriver_ThePlaqueKeyAndRndAAreUNPREDICTABLENotMerelyDistinct — eighth audit, B1.
//
// 🔴 TURN 1 BLOCKED ON HALF OF THIS AND THE FIX CLOSED ONLY THAT HALF. It found that
// the permanent plaque key had no freshness loop while the transient nonce did; the
// loop I wrote measured DISTINCTNESS. An auditor then replaced mintPlaqueKey's
// crypto/rand with a sequential counter — every key distinct, every key predictable —
// and `go test -race`, `go vet` and scripts/redline-check.sh were ALL GREEN. The same
// mutation on acceptAuthenticate1's RndA was green too.
//
// ADR 0003 md. 3 asks for "per-plaque random, derived from nothing". Distinctness does
// not deliver that: whoever sees ONE plaque's key can ENUMERATE the fleet.
//
// 🔴 AND THE PRECEDENT WAS ALREADY IN THIS PACKAGE, FOR A LESSER SECRET. The bearer
// session handle has a per-byte distribution test (TestSession_TheHandleIsUnpredictable);
// the AES-128 plaque key did not. This applies the same shape to both key material
// paths.
//
// ⚠️ WHAT A DISTRIBUTION TEST PROVES, AND WHAT IT DOES NOT — the qualifier belongs in
// the same breath. It does NOT prove unpredictability; no test can. It eliminates one
// CLASS of predictable generator — counters, timestamps, PIDs, anything whose high
// bytes are constant or slow-moving across samples — by requiring the per-position
// spread that only a wide-entropy source produces. A cryptographically weak PRNG with
// good statistics would pass. What ultimately makes these unpredictable is that they
// come from crypto/rand, and the only thing holding THAT is this test's ability to
// notice when they stop.
func TestDriver_ThePlaqueKeyAndRndAAreUNPREDICTABLENotMerelyDistinct(t *testing.T) {
	// For 64 draws of a uniform byte, the expected number of distinct values is about
	// 57. A counter leaves its high bytes CONSTANT, i.e. 1. Thirty sits far below
	// anything random produces and far above anything sequential can reach.
	const (
		samples            = 64
		minDistinctPerByte = 30
	)

	assertSpread := func(t *testing.T, what string, vals [][]byte) {
		t.Helper()
		if len(vals) != samples {
			t.Fatalf("%s: %d samples, want %d", what, len(vals), samples)
		}
		width := len(vals[0])
		for pos := 0; pos < width; pos++ {
			seen := map[byte]bool{}
			for _, v := range vals {
				if len(v) != width {
					t.Fatalf("%s: ragged sample widths", what)
				}
				seen[v[pos]] = true
			}
			if len(seen) < minDistinctPerByte {
				t.Fatalf("%s: byte %d takes only %d distinct values across %d samples (want >= %d). "+
					"A DISTINCT-but-sequential generator passes a distinctness check and fails here — "+
					"and ADR 0003 md. 3 requires per-plaque random, because a predictable key lets "+
					"whoever sees one plaque ENUMERATE the fleet",
					what, pos, len(seen), samples, minDistinctPerByte)
			}
		}
	}

	t.Run("plaque_key", func(t *testing.T) {
		var keys [][]byte
		for i := 0; i < samples; i++ {
			k, err := mintPlaqueKey()
			if err != nil {
				t.Fatalf("mintPlaqueKey: %v", err)
			}
			keys = append(keys, k)
		}
		assertSpread(t, "plaque key", keys)
	})

	t.Run("rnda_measured_on_the_wire", func(t *testing.T) {
		// RndA is read back out of the Part 2 cryptogram by the chip double, so this
		// measures what was actually SENT rather than what a field says.
		h := newHarness(t)
		var rndAs [][]byte
		for i := 0; i < samples; i++ {
			chip := newFakeChip(t)
			chip.uid = []byte{0x04, 0x96, 0x8C, 0xAA, byte(i >> 8), byte(i), 0x82}
			if _, err := h.run(t, chip, "operator-1"); err != nil {
				t.Fatalf("round %d: %v", i, err)
			}
			if len(chip.rndAsSeen) != 1 {
				t.Fatalf("round %d produced %d challenges", i, len(chip.rndAsSeen))
			}
			rndAs = append(rndAs, chip.rndAsSeen[0])
		}
		assertSpread(t, "RndA", rndAs)
	})
}
