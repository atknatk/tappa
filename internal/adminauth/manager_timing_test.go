package adminauth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/atknatk/tappa/internal/db"
)

// WHY THIS PACKAGE TAKES ~130 s UNDER -race, IN ONE SENTENCE FOR WHOEVER ASKS:
// Authenticate pads every login to adminauth.MaxCandidates bcrypt comparisons to keep
// the candidate count out of the response time (Manager.pad), so the handful of tests
// that must run at the SHIPPED work factor — TestAuthenticate_DummyIsReallyRun and
// TestAuthenticate_TimingDoesNotCountTheCandidates — each pay eight cost-12
// comparisons per login instead of one, and under the race detector one of those is
// ~11 s.
//
// EVERY OTHER TEST HERE RUNS AT bcrypt.MinCost ON PURPOSE, with its dummy matched to
// its fixtures (newCheapFakeManager, testManager): the padding takes its cost from the
// digest it is padding against, so a mismatched dummy makes a test measure the fixture
// rather than the product. Before that was matched this package reported
// "FAIL ... 601.242s" — Go's 10-minute default, not a deadlock.
//
// fakeResolver drives Authenticate without a database, so the three outcomes of
// PHASE B OBLIGATION 1 can be timed against each other with nothing but bcrypt in
// the measurement. A real Postgres is used for the behavioural proofs
// (manager_db_test.go); it is deliberately NOT used here, because a network round
// trip is noise an order of magnitude below the signal and a second source of
// variance in a test whose whole content is a variance comparison.
type fakeResolver struct {
	rows []db.ResolvedAdmin
	err  error
}

func (f *fakeResolver) GetAdminByEmail(_ context.Context, _ string) ([]db.ResolvedAdmin, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

func (f *fakeResolver) WithTenant(_ context.Context, _ uuid.UUID, _ db.TxFunc) error {
	return errors.New("fakeResolver: WithTenant must not be reached by Authenticate")
}

func (f *fakeResolver) GetAdminSessionByTokenHash(_ context.Context, _ string) (db.ResolvedAdminSession, error) {
	return db.ResolvedAdminSession{}, pgx.ErrNoRows
}

func newFakeManager(t *testing.T, rows []db.ResolvedAdmin) *Manager {
	t.Helper()
	m, err := New(&fakeResolver{rows: rows}, testConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

// newCheapFakeManager is newFakeManager with a MinCost dummy, for the tests whose
// subject is BEHAVIOUR rather than cost.
//
// 🔴 WHY IT EXISTS. Authenticate pads every login to MaxCandidates comparisons to
// close a timing channel (Manager.pad). The padding takes its cost from the digest it
// is padding against, so a test whose fixtures are cheap gets cheap padding — EXCEPT
// when the resolver returns nothing, where there is no real digest to copy and the
// package dummy is used. That dummy is $2a$12$, so a zero-candidate test login cost
// 3.42 s. Measured across the suite that was the difference between a package that
// runs in seconds and one that runs in minutes.
//
// IT DOES NOT WEAKEN WHAT THE TESTS PROVE. The security property is that the COUNT is
// constant, and that is asserted structurally (TestAuthenticate_PadsEveryExitToTheCap)
// and behaviourally at the SHIPPED cost by TestAuthenticate_TimingDoesNotCountTheCandidates,
// which deliberately does NOT use this helper.
func newCheapFakeManager(t *testing.T, rows []db.ResolvedAdmin) *Manager {
	t.Helper()
	m := newFakeManager(t, rows)
	m.dummy = cheapDigest(t, "a discarded padding password")
	return m
}

func stats(d []time.Duration) (min, med, max time.Duration) {
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[0], s[len(s)/2], s[len(s)-1]
}

// TestAuthenticate_TimingIsFlat is CORROBORATION for PHASE B OBLIGATION 2 — no
// longer its proof.
//
// 🔴🔴 RE-AIMED IN M7-02 (2026-08-13), AND THIS IS A DELIBERATE CHANGE TO AN M6-01
// TEST — i.e. SCOPE EXPANSION, flagged rather than slipped in. Read this before the
// history below it.
//
// WHAT CHANGED IN THE PRODUCT. Authenticate now pads every login to MaxCandidates
// comparisons (Manager.pad), because the candidate COUNT was observable in the
// response time: measured with one planted row, 216 ms for an unknown address against
// 437 ms for a registered one, with no overlap. That closed the leak STRUCTURALLY —
// the comparison count is now a constant, by construction.
//
// WHY THAT MAKES A SAMPLING TEST THE WRONG INSTRUMENT, AND WHY IT ALSO MAKES IT
// UNAFFORDABLE. This test drives 5 arms x timingSamples logins, and each login went
// from ONE comparison to EIGHT. Under -race, where this file's own note puts one
// cost-12 comparison at ~11 s:
//
//	before  5 x 3 x 1 =  15 comparisons  ~165 s
//	after   5 x 3 x 8 = 120 comparisons  ~1320 s   -> past Go's 10-minute default
//
// Measured consequence before the re-aim: `go test -race ./internal/adminauth/`
// reported "FAIL ... 601.242s" with user=596.93s — burning CPU, not deadlocked.
//
// WHAT IT MEASURES NOW: the five arms still have to be indistinguishable from each
// other, but at a CHEAP, SHARED work factor (bcrypt.MinCost fixtures AND a MinCost
// dummy — see newCheapFakeManager). The RATIO between arms is what this test has
// always asserted, and padding adds the same constant to every arm, so the signal is
// untouched; only the price is.
//
// ⚠️ WHAT IT CAN NO LONGER SEE, stated exactly because that is the cost of the
// re-aim: it no longer corroborates that the SHIPPED work factor is in use. A build
// whose dummy or whose stored digests dropped to a cheap cost would still look flat
// here. THREE things cover that, all of them exact rather than statistical:
//
//	TestCost_MatchesTheDummyDigest       reads bcrypt.Cost out of the shipped dummy
//	TestSeedDigests_UseTheDeclaredCost   reads it out of the digests that exist
//	TestAuthenticate_DummyIsReallyRun    a wall-clock FLOOR at the shipped cost, so a
//	                                     login that did no real bcrypt is caught
//
// and TestAuthenticate_TimingDoesNotCountTheCandidates drives the SHIPPED cost end to
// end to show the padded arms track each other (measured RATIO 0.97; the absolute
// wall-clock figures are deliberately not quoted — they are machine state, and this
// repository has a tripwire for exactly that habit).
//
// ⚠️ NO LOGIN COUNT IS QUOTED HERE ANY MORE. This said "6 logins, not a sample" while
// that test ran TWO — it was written when the loop was three rounds and was not
// updated when the round count was cut to one. In the file whose whole subject is
// sample size, a stale sample size is the worst possible sentence, so the number is
// gone rather than corrected: read the loop.
//
// 🔴 WHY THE DEMOTION (user decision, 2026-08-03). This test used to be the
// load-bearing evidence, with a 1.50x ratio gate. On a clean tree it went RED in
// 1 of 5 full-suite runs:
//
//	unknown email                    n=3 min=4.541s med=8.398s max=10.398s
//	wrong password                   n=3 min=5.157s med=5.803s max=9.572s
//	disabled admin, correct password n=3 min=4.815s med=5.290s max=9.031s
//	median spread 1.59x (gate < 1.50x)
//
// Root cause, measured: under -race one cost-12 comparison takes 4-10 s, and the
// SAME arm's own min/max swings 2.3x while 14 packages run concurrently. At n=3 the
// median measures the scheduler, not bcrypt. A gate that reddens on noise is worse
// than no gate — M5-09's own lesson: "a setup that produces false alarms is itself
// a risk; a real violation gets lost in that noise."
//
// 🔴 WHAT ACTUALLY PROVES OBLIGATION 2 NOW — FOUR STRUCTURAL TESTS ACROSS THREE
// FAILURE SHAPES, every one of them exact and noise-free. Read them together; no
// single one is sufficient:
//
//	"the dummy RUNS"        TestAuthenticate_DummyIsReallyRun. Deleting the dummy
//	                        takes an unknown-email login from ~171 ms to 609 ns —
//	                        a measured 281957x difference. No timing gate needs to
//	                        be that sensitive; a floor catches it with five orders
//	                        of magnitude to spare.
//	"at the RIGHT COST"     TestCost_MatchesTheDummyDigest reads bcrypt.Cost out of
//	                        the shipped dummy, and TestSeedDigests_UseTheDeclaredCost
//	                        reads it out of the two digests that actually exist.
//	                        A dummy at cost 10 against cost-12 rows is the ~4x
//	                        timing leak — and it is caught EXACTLY, by comparing two
//	                        integers, not statistically.
//	"OVER-LONG PASSWORDS    TestAuthenticate_OverLongPasswordStillPaysBcrypt. Added
//	 PAY TOO"               in round 8, after a security audit found that the
//	                        third shape below was pinned ONLY by wall-clock tests
//	                        and that round 6 had removed both of them from -short.
//	                        Measured: honest ~1.42 ms against a cost-4 fixture,
//	                        broken ~66 ns.
//
// So the THREE failure shapes this file exists for are each pinned by an exact
// check — FOUR test functions across three shapes. What remains here is a
// wall-clock sanity reading that would catch a FOURTH, unanticipated shape.
//
// ⚠️ THAT SENTENCE USED TO SAY "two", AND THE THIRD SHAPE IS EXACTLY WHAT IT
// FAILED TO PREDICT. It called the residual "a third shape nobody has modelled";
// a security audit then found one — an over-long password against a REGISTERED
// address, 53x over HTTP — so the third row above is not a hypothetical, it is
// that finding turned into a check. The count is kept honest here because the
// Makefile points at this file as the canonical list.
//
// THE GATE IS SET FROM MEASURED NOISE, not from a hopeful number: see
// timingSpreadGate for the distribution it was derived from and for exactly how
// large a leak it can no longer see.
func TestAuthenticate_TimingIsFlat(t *testing.T) {
	if testing.Short() {
		// -short skips the SAMPLE, never the obligation: the two structural tests
		// named in this file's header run in every mode, including this one.
		t.Skip("-short: skipping the bcrypt wall-clock SAMPLE (5 arms x N padded " +
			"comparisons). PHASE B OBLIGATION 2 is still proven under -short by FOUR " +
			"structural tests covering THREE failure shapes: " +
			"TestAuthenticate_DummyIsReallyRun (the dummy runs) · " +
			"TestCost_MatchesTheDummyDigest + TestSeedDigests_UseTheDeclaredCost (at the " +
			"right cost) · TestAuthenticate_OverLongPasswordStillPaysBcrypt (an over-long " +
			"password against a KNOWN candidate pays too).")
	}
	// 🔴 THE DIGEST IS CHEAP NOW, AND IT USED TO BE THE SHIPPED COST. This line read
	// "the work factor IS the subject, so cheapDigest must not be used here" — see the
	// RE-AIMED block in this test's header for why that is no longer this test's
	// subject, what still checks it exactly, and what this arm can no longer see.
	//
	// EVERY ARM MUST SHARE ONE COST, INCLUDING THE PADDING. Authenticate pads to
	// MaxCandidates comparisons and takes the padding's cost from the first candidate's
	// own digest — so an arm with NO candidate pads against the manager's dummy
	// instead. newCheapFakeManager makes that dummy cheap too; without it the
	// unknown-email arm would pay cost 12 while the others paid cost 4 and this test
	// would measure the fixture, not the product.
	digest := cheapDigest(t, "the-real-password")
	tenantID, adminID := uuid.New(), uuid.New()

	unknown := newCheapFakeManager(t, nil)
	wrong := newCheapFakeManager(t, []db.ResolvedAdmin{{
		ID: adminID, TenantID: tenantID, PasswordHash: db.NewPasswordHash(digest), Status: "active",
	}})
	disabled := newCheapFakeManager(t, []db.ResolvedAdmin{{
		ID: adminID, TenantID: tenantID, PasswordHash: db.NewPasswordHash(digest), Status: "disabled",
	}})

	arms := []struct {
		name string
		m    *Manager
		pw   string
		d    []time.Duration
	}{
		{name: "unknown email", m: unknown, pw: "the-real-password"},
		{name: "wrong password", m: wrong, pw: "not-the-real-password"},
		{name: "disabled admin, correct password", m: disabled, pw: "the-real-password"},
		// 🔴 THE FOURTH ARM IS THE SHAPE THIS FILE'S HEADER PREDICTED AND MISSED.
		// It said the residual risk was "a third shape nobody has modelled"; a
		// security audit found it, and it was not a 2.5x leak but a 53x one over
		// HTTP (3203211x in this package). All three arms above use a password
		// INSIDE bcrypt's 72-byte limit, so none of them exercised the branch that
		// used to skip bcrypt entirely. This one is over the limit AND resolves a
		// candidate — the exact combination that answered "is this address an
		// administrator?" in one request.
		{name: "over-long password, known candidate", m: wrong, pw: strings.Repeat("a", 100)},
		// And its control: over-long against NO candidate, which always paid the
		// dummy and so was the slow half of the old oracle.
		{name: "over-long password, unknown email", m: unknown, pw: strings.Repeat("a", 100)},
	}

	ctx := context.Background()
	for i := 0; i < timingSamples; i++ {
		for a := range arms {
			start := time.Now()
			_, err := arms[a].m.Authenticate(ctx, "owner@example.test", arms[a].pw)
			elapsed := time.Since(start)
			if !errors.Is(err, ErrBadCredentials) {
				t.Fatalf("%s: err = %v, want ErrBadCredentials", arms[a].name, err)
			}
			arms[a].d = append(arms[a].d, elapsed)
		}
	}

	var slowest, fastest time.Duration
	for i, a := range arms {
		lo, med, hi := stats(a.d)
		t.Logf("%-34s n=%d  min=%-12v med=%-12v max=%v", a.name, len(a.d), lo, med, hi)
		if i == 0 || med > slowest {
			slowest = med
		}
		if i == 0 || med < fastest {
			fastest = med
		}
	}
	ratio := float64(slowest) / float64(fastest)
	t.Logf("median spread: slowest/fastest = %.2fx (gate: < %.2fx), samples per arm = %d",
		ratio, timingSpreadGate, timingSamples)
	if ratio >= timingSpreadGate {
		t.Fatalf("the arms are distinguishable by TIME: median spread %.2fx, gate "+
			"%.2fx. Check the FOUR structural tests first (this file's header) — they pin "+
			"the three known failure shapes exactly, so a red here with those green means "+
			"either a FOURTH shape or an unusually loaded machine",
			ratio, timingSpreadGate)
	}
}

// TestAuthenticate_DummyIsReallyRun is the CHEAP, ALWAYS-ON tripwire for the same
// property, so that deleting the dummy fails a fast test and not only the slow
// statistical one above.
//
// It asserts a floor rather than a ratio: one cost-12 comparison cannot complete
// in under 20 ms on any hardware this product runs on (measured range on this
// machine: 350-540 ms; the floor is 17x below the fastest observation, so it has
// enormous headroom and still catches "no bcrypt happened at all", which is
// microseconds).
func TestAuthenticate_DummyIsReallyRun(t *testing.T) {
	m := newFakeManager(t, nil)
	start := time.Now()
	_, err := m.Authenticate(context.Background(), "nobody@example.test", "whatever")
	elapsed := time.Since(start)
	if !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("err = %v, want ErrBadCredentials", err)
	}
	t.Logf("unknown-email login took %v (one cost-%d bcrypt)", elapsed, Cost)
	if elapsed < dummyRunFloor {
		t.Fatalf("an unknown email was answered in %v — no real bcrypt comparison ran, so the "+
			"response time answers the question the body refuses to (PHASE B OBLIGATION 2)", elapsed)
	}
}

// TestMaxCandidates_IsTheMeasuredCPUBound PINS THE VALUE, not the mechanism.
//
// 🔴 WHY A SEPARATE TEST FOR A NUMBER. An audit changed MaxCandidates from 8 to 9
// and NOTHING went red: every expectation in the cap test below was written in
// terms of the constant, so the test moved with it. That is the same tautology
// class that hid the CookiePath widening, found a second time in the same task —
// and here it matters more, because 00011 calls this bound "the COST limit,
// MEASURED, and the largest number in this file". The number IS the deliverable.
//
// THE LITERAL LIVES HERE AND THE ARITHMETIC IS THE REASON, so changing the
// constant forces somebody to re-derive it rather than update a fixture:
//
//	one cost-12 bcrypt comparison        ~380 ms   (measured, see Cost)
//	failed logins per address per window  10       (adminAttemptLimit)
//	8 x 10 x 380 ms                      ~30 s of CPU per 10-minute window
//	                                     per source address = ~5% of one core
//
// At 64 the same arithmetic gives ~243 s per window — about 40% of a core from one
// address — which is why the mechanism test alone was not enough.
func TestMaxCandidates_IsTheMeasuredCPUBound(t *testing.T) {
	const (
		pinned            = 8
		msPerComparison   = 380 // measured on this machine at Cost 12
		failuresPerWindow = 10  // handler/adminratelimit.go: adminAttemptLimit
		budgetSeconds     = 40  // the CPU-per-window ceiling this bound exists to hold
	)
	if MaxCandidates != pinned {
		t.Fatalf("MaxCandidates is %d, and this test pins %d.\n"+
			"This is not a fixture to update: the number is PHASE B OBLIGATION 4's whole "+
			"content. Re-derive it — %d candidates x %d failed attempts x %d ms = %.0f s of "+
			"CPU per 10-minute window per address — and change the literal here together with "+
			"the arithmetic in manager.go and handler/adminratelimit.go.",
			MaxCandidates, pinned,
			MaxCandidates, failuresPerWindow, msPerComparison,
			float64(MaxCandidates*failuresPerWindow*msPerComparison)/1000)
	}
	if got := float64(MaxCandidates*failuresPerWindow*msPerComparison) / 1000; got > budgetSeconds {
		t.Fatalf("the shipped bound buys %.0f s of CPU per window per address, over the %d s "+
			"ceiling it exists to hold", got, budgetSeconds)
	}
}

// TestAuthenticate_CapsTheCandidateLoop is PHASE B OBLIGATION 4's MECHANISM.
//
// ⚠️ ITS EXPECTATIONS ARE LITERALS, DELIBERATELY. They used to be written as
// `MaxCandidates` and `MaxCandidates+1`, which made the whole table move with the
// constant — an audit changed 8 to 9 and this test stayed green. The value is
// pinned by TestMaxCandidates_IsTheMeasuredCPUBound; this one pins that the loop
// really stops, and the guard below ties the two together so the literals cannot
// go stale silently.
//
// The digests are DELIBERATELY MALFORMED: Compare returns false for them
// immediately, so this test costs microseconds instead of 8 x 380 ms. It measures
// the LOOP BOUND, not the loop's cost, which is measured elsewhere.
func TestAuthenticate_CapsTheCandidateLoop(t *testing.T) {
	if MaxCandidates != 8 {
		t.Fatalf("this table is written for MaxCandidates == 8 but it is %d; update the "+
			"literals below together with TestMaxCandidates_IsTheMeasuredCPUBound", MaxCandidates)
	}
	tests := []struct {
		name          string
		candidates    int
		wantCompared  int
		wantTruncated bool
	}{
		{"one candidate, the ordinary case", 1, 1, false},
		{"two — the tenant picker's real shape", 2, 2, false},
		{"one below the cap", 7, 7, false},
		{"exactly at the cap", 8, 8, false},
		{"one past the cap", 9, 8, true},
		{"the amplification shape 00011 measured", 500, 8, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows := make([]db.ResolvedAdmin, 0, tc.candidates)
			for i := 0; i < tc.candidates; i++ {
				rows = append(rows, db.ResolvedAdmin{
					ID:           uuid.New(),
					TenantID:     uuid.New(),
					PasswordHash: db.NewPasswordHash("not-a-valid-digest"),
					Status:       "active",
				})
			}
			m := newFakeManager(t, rows)
			auth, err := m.Authenticate(context.Background(), "owner@example.test", "anything")
			if !errors.Is(err, ErrBadCredentials) {
				t.Fatalf("err = %v, want ErrBadCredentials", err)
			}
			if got := len(auth.Attempts); got != tc.wantCompared {
				t.Fatalf("compared %d candidates, want %d", got, tc.wantCompared)
			}
			if auth.Resolved != tc.candidates {
				t.Fatalf("Resolved = %d, want %d", auth.Resolved, tc.candidates)
			}
			if got := auth.Truncated(); got != tc.wantTruncated {
				t.Fatalf("Truncated() = %v, want %v", got, tc.wantTruncated)
			}
		})
	}
}

// TestAuthenticate_VerifiedIsTheAndOfMatchedAndActive is PHASE B OBLIGATION 5 at
// the type level: the ONLY producer of an identity ANDs both conditions.
func TestAuthenticate_VerifiedIsTheAndOfMatchedAndActive(t *testing.T) {
	tests := []struct {
		name         string
		matched      bool
		active       bool
		wantVerified int
	}{
		{"matched and active", true, true, 1},
		{"matched but disabled", true, false, 0},
		{"active but wrong password", false, true, 0},
		{"neither", false, false, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status := "disabled"
			if tc.active {
				status = "active"
			}
			auth := Authentication{Resolved: 1, Attempts: []Attempt{{
				AdminUserID:     uuid.New(),
				TenantID:        uuid.New(),
				PasswordMatched: tc.matched,
				Active:          status == "active",
			}}}
			if got := len(auth.Verified()); got != tc.wantVerified {
				t.Fatalf("Verified() returned %d identities, want %d", got, tc.wantVerified)
			}
		})
	}
}

// TestAuthenticate_RefusesEmptyInput.
//
// The fake's GetAdminByEmail would return a row for ANY email; if Authenticate
// reached it with an empty address, this would succeed. It must not — and it must
// still pay the dummy, which the elapsed-time assertion checks.
func TestAuthenticate_RefusesEmptyInput(t *testing.T) {
	// 🔴 CHEAP FIXTURES AND A CHEAP DUMMY (M7-02). This test's subject is that an empty
	// field is refused AND that a real bcrypt still ran — not the WORK FACTOR of that
	// bcrypt. Since padding, each of these three refusals costs MaxCandidates
	// comparisons rather than one, which at the shipped cost measured 87.4 s under
	// -race for a test whose assertion is a floor.
	//
	// THE FLOOR STILL CATCHES WHAT IT EXISTS FOR. It fires on "no bcrypt happened at
	// all", which is ~609 ns; a MinCost comparison is ~1.3 ms, still three orders of
	// magnitude above it. The SHIPPED-cost floor is kept, deliberately and separately,
	// by TestAuthenticate_DummyIsReallyRun.
	digest := cheapDigest(t, "the-real-password")
	m := newCheapFakeManager(t, []db.ResolvedAdmin{{
		ID: uuid.New(), TenantID: uuid.New(), PasswordHash: db.NewPasswordHash(digest), Status: "active",
	}})

	tests := []struct{ name, email, password string }{
		{"empty email", "", "the-real-password"},
		{"empty password", "owner@example.test", ""},
		{"both empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			auth, err := m.Authenticate(context.Background(), tc.email, tc.password)
			elapsed := time.Since(start)
			if !errors.Is(err, ErrBadCredentials) {
				t.Fatalf("err = %v, want ErrBadCredentials", err)
			}
			if len(auth.Verified()) != 0 {
				t.Fatalf("an empty field authenticated somebody")
			}
			// cheapDummyFloor rather than dummyRunFloor: the fixtures here are MinCost
			// (see the note at the top of this test), so the shipped floor would be
			// asserting a work factor this test no longer uses.
			if elapsed < cheapDummyFloor {
				t.Fatalf("answered in %v without a dummy comparison", elapsed)
			}
		})
	}
}

// TestAuthenticate_DatabaseFailureIsNotBadCredentials.
//
// Collapsing the two would turn an outage into "your password is wrong" for every
// operator at once, and would hide the outage behind a UX message.
func TestAuthenticate_DatabaseFailureIsNotBadCredentials(t *testing.T) {
	boom := errors.New("connection refused")
	m, err := New(&fakeResolver{err: boom}, testConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = m.Authenticate(context.Background(), "owner@example.test", "whatever")
	if errors.Is(err, ErrBadCredentials) {
		t.Fatalf("a database failure was reported as bad credentials")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap the database error", err)
	}
}

// 🔴 TestAuthenticate_OverLongPasswordStillPaysBcrypt is the STRUCTURAL guard for
// the third failure shape — the one this task actually shipped and had to fix.
//
// WHY IT EXISTS, AND IT IS A DEFECT OF MY OWN PREVIOUS FIX. Round 6 closed a 53x
// timing oracle (an over-long password against a REGISTERED address did no bcrypt
// at all) and pinned it with two WALL-CLOCK tests — the five-arm sample here and
// its HTTP twin. Round 6 also made both of them skip under -short. A security
// audit then reverted the fix and ran the inner loop:
//
//	$ go test -count=1 -short ./...      ->  14/14 ok, WITH THE ORACLE OPEN
//
// The commit gate still caught it, so the product was never unprotected — but the
// mode developers run dozens of times a day was blind to the exact hole this task
// exists to have closed, and the Makefile said the opposite in bold. The two
// structural tests it names (the dummy RUNS, at the RIGHT COST) genuinely pin the
// first two shapes; neither one models this third.
//
// WHY IT IS CHEAP ENOUGH TO ALWAYS RUN, measured: the fixture digest is at
// bcrypt.MinCost, so an honest comparison costs ~1.42 ms while the reverted
// early-return branch costs ~66 ns — FOUR ORDERS OF MAGNITUDE of headroom. No
// statistics, no sampling, no wall-clock gate: a floor at 200 us sits ~7x above
// the honest cost's lower bound and ~3000x above the broken one.
//
// IT DELIBERATELY USES A KNOWN CANDIDATE. The unknown-email path was never the
// broken one — it always paid the dummy. The bug needed a RESOLVED candidate plus
// an over-long password, and that is exactly the shape below.
func TestAuthenticate_OverLongPasswordStillPaysBcrypt(t *testing.T) {
	// bcrypt.MinCost keeps this runnable in every mode, -race included, without
	// weakening the assertion: what is being detected is "no bcrypt happened at
	// all", which is nanoseconds against milliseconds.
	digest, err := bcrypt.GenerateFromPassword([]byte("the-real-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword: %v", err)
	}
	m := newFakeManager(t, []db.ResolvedAdmin{{
		ID: uuid.New(), TenantID: uuid.New(),
		PasswordHash: db.NewPasswordHash(string(digest)), Status: "active",
	}})

	overLong := strings.Repeat("a", MaxPasswordBytes+28) // 100 bytes
	// ⚠️ THE INPUT'S LENGTH IS PART OF THE ASSERTION, not a detail of its
	// construction. An audit shortened this to exactly MaxPasswordBytes and the
	// test still PASSED — because "bcrypt ran" and "it did not match" are both
	// true for an ordinary wrong password, so nothing forced the over-long BRANCH
	// to be exercised at all. The test caught the regression it exists for
	// (reverting the fix turns it red), but it did not insist on driving the right
	// path. Now it does.
	if len(overLong) <= MaxPasswordBytes {
		t.Fatalf("the probe is %d bytes, which is not over the %d-byte limit — this test would "+
			"then be measuring an ordinary wrong password and would prove nothing about the "+
			"over-long branch", len(overLong), MaxPasswordBytes)
	}
	start := time.Now()
	auth, err := m.Authenticate(context.Background(), "owner@example.test", overLong)
	elapsed := time.Since(start)

	if !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("err = %v, want ErrBadCredentials", err)
	}
	// The attempt must still be ATTRIBUTABLE: the whole reason the fix lives in
	// Compare and not as an early return in Authenticate is that the loop has to
	// keep running so audit_log gets a row (section 4.6).
	if len(auth.Attempts) != 1 {
		t.Fatalf("compared %d candidates, want 1 — an over-long password must not skip the "+
			"loop, or the failed attempt becomes unattributable", len(auth.Attempts))
	}
	if auth.Attempts[0].PasswordMatched {
		t.Fatalf("an over-long password MATCHED — bcrypt truncates at %d bytes and the result "+
			"must be discarded (Q03)", MaxPasswordBytes)
	}
	t.Logf("over-long password against a KNOWN cost-%d candidate took %v", bcrypt.MinCost, elapsed)
	if elapsed < overLongFloor {
		t.Fatalf("an over-long password against a REGISTERED address was answered in %v — no "+
			"bcrypt ran, so the response time answers 'is this address an administrator?' "+
			"that the body refuses to. This is the 53x oracle of round 6 (PHASE B "+
			"OBLIGATION 2, third shape).", elapsed)
	}
}

// The wall-clock FLOORS, named so they can be pinned. They were inline literals
// until a family re-scan showed that every timing THRESHOLD in this suite could be
// loosened silently — the M5-10 class, in its most tempting form: the natural
// repair for a flaky timing test is to widen its gate, and that is exactly the
// move that disarms it.
const (
	// dummyRunFloor: an unknown-email login must take at least this long, i.e. a
	// real bcrypt must have happened. Measured margin: a cost-12 comparison is
	// 190-880 ms depending on -race and load; the branch with no bcrypt is ~600 ns.
	dummyRunFloor = 20 * time.Millisecond

	// cheapDummyFloor is dummyRunFloor's twin for the tests whose fixtures are
	// bcrypt.MinCost (M7-02). It exists because padding made a shipped-cost refusal
	// cost MaxCandidates comparisons rather than one, and the tests whose subject is
	// "a bcrypt ran at all" should not pay a production work factor to say so.
	//
	// 200 MICROSECONDS, and the headroom is the argument: the failure it catches is a
	// comparison that did not happen, measured at ~609 ns, and one MinCost comparison
	// is ~1.3 ms. The floor sits between them with three orders of magnitude on the
	// failure side and 6.5x on the safe side — the same shape dummyRunFloor has
	// against the shipped cost, at the cost these tests actually use.
	cheapDummyFloor = 200 * time.Microsecond
	// overLongFloor: same idea for the over-long-password shape, against a cost-4
	// FIXTURE digest so it stays cheap in every mode. Measured: honest ~1.4 ms,
	// broken ~66 ns.
	overLongFloor = 200 * time.Microsecond
)

// The three build-tagged constants of this suite move TOGETHER or not at all.
//
// 🔴 WHY A CLOSED SET AND NOT THREE SEPARATE PINS. An audit found that the
// threshold invariant below could be defeated by its OWN NATURAL REPAIR: a red gate
// tempts you to lower the gate, and the anchor it is compared against sits twenty
// lines away in the same file, so lowering BOTH passed. Measured by the audit:
// `worstObservedNoise 1.59 -> 1.00` alone was GREEN, and `gate -> 1.5` together
// with `anchor -> 1.00` was GREEN — restoring exactly the 1.50x gate that had been
// measured to flake 1 run in 5.
//
// That is the M5-10 class in a new costume ("the natural repair for the red test is
// to update the list, and that is precisely the wrong move"), and the repo already
// has the shape that answers it: internal/policy/guardrails_test.go checks a
// STRUCTURE and keeps a NAMED exception list with permanent negative controls.
//
// Here the structure is a TUPLE. Each build has exactly one legal
// (samples, gate, noise) triple, both are named below, and the shipped constants
// must equal one of them. Changing any single value leaves no matching row;
// changing all three still has to land on a row somebody wrote down deliberately,
// with the measurement beside it.
var legalTimingConfigs = []struct {
	build   string
	samples int
	gate    float64
	noise   float64
	why     string
}{
	{
		build: "-race (what `make test` runs)", samples: 3, gate: 2.5, noise: 1.59,
		why: "one cost-12 comparison is ~11 s under the detector, so 20 rounds per arm " +
			"would exceed Go's per-package timeout; the noise anchor is the worst median " +
			"spread ever seen from noise alone (an audit's loaded machine — 10 local " +
			"full-suite runs peaked at 1.07x)",
	},
	{
		build: "plain (`go test ./internal/adminauth`)", samples: 20, gate: 1.5, noise: 1.10,
		why: "one package at a time, no instrumentation: measured spread 1.00-1.06x, so a " +
			"tighter gate is safe here and catches the 1.91x one-cost-step case the race " +
			"build gives up",
	},
}

// nearestCaughtSignal is the weakest failure this suite's wall-clock half must
// still catch: a dummy at cost 10 against cost-12 rows, measured at 4.04x.
const nearestCaughtSignal = 4.04

// thresholdViolations is the predicate, extracted as a PURE FUNCTION so it can be
// driven with known-bad inputs — the guardrails_test.go shape. It returns one
// message per problem.
func thresholdViolations(gate, noise, signal float64) []string {
	var bad []string
	if gate <= noise {
		bad = append(bad, fmt.Sprintf(
			"gate %.2fx is at or below the worst spread NOISE alone has produced (%.2fx): it will flake",
			gate, noise))
	}
	if gate >= signal {
		bad = append(bad, fmt.Sprintf(
			"gate %.2fx is at or above the nearest signal it must still catch (%.2fx): widening it this far disarms the check",
			gate, signal))
	}
	return bad
}

// isNamedTimingConfig reports whether a (samples, gate, noise) triple is one of
// the tuples somebody wrote down. Extracted as a PURE FUNCTION so the negative
// controls can drive it with the configurations an audit actually used.
func isNamedTimingConfig(samples int, gate, noise float64) bool {
	for _, c := range legalTimingConfigs {
		if samples == c.samples && gate == c.gate && noise == c.noise {
			return true
		}
	}
	return false
}

// TestTimingConfig_IsOneOfTheNamedTuples closes the anchor hole: the shipped
// triple must be one somebody wrote down.
func TestTimingConfig_IsOneOfTheNamedTuples(t *testing.T) {
	for _, c := range legalTimingConfigs {
		if timingSamples == c.samples && timingSpreadGate == c.gate && worstObservedNoise == c.noise {
			t.Logf("build %s: samples=%d gate=%.2fx noise=%.2fx", c.build, c.samples, c.gate, c.noise)
			return
		}
	}
	var named []string
	for _, c := range legalTimingConfigs {
		named = append(named, fmt.Sprintf("{samples:%d gate:%.2f noise:%.2f} %s", c.samples, c.gate, c.noise, c.build))
	}
	t.Fatalf("the shipped timing configuration {samples:%d gate:%.2f noise:%.2f} matches no named "+
		"tuple.\nLegal tuples:\n  %s\n"+
		"These three constants are ONE decision, not three: an audit defeated the invariant "+
		"below by lowering the gate AND its own anchor together. If the configuration really "+
		"must change, add a row here with the measurement that justifies it.",
		timingSamples, timingSpreadGate, worstObservedNoise, strings.Join(named, "\n  "))
}

// TestTimingThresholds_AreBetweenTheNoiseAndTheSignal is the invariant itself.
func TestTimingThresholds_AreBetweenTheNoiseAndTheSignal(t *testing.T) {
	if bad := thresholdViolations(timingSpreadGate, worstObservedNoise, nearestCaughtSignal); len(bad) > 0 {
		t.Fatalf("the shipped timing gate is not between the noise and the signal:\n  %s",
			strings.Join(bad, "\n  "))
	}
	t.Logf("gate %.2fx sits between noise %.2fx and signal %.2fx",
		timingSpreadGate, worstObservedNoise, nearestCaughtSignal)
}

// TestTimingThresholds_PredicateRejectsTheKnownBadConfigurations is the permanent
// NEGATIVE CONTROL for the RATIO predicate.
//
// ⚠️ IT DELIBERATELY DOES NOT CONTAIN THE AUDIT'S OWN REPAIR (gate 1.5 with the
// anchor lowered to 1.00), and writing this comment is how that was noticed: the
// first version listed it here and the control FAILED, because with noise=1.00 a
// gate of 1.5 genuinely IS between the noise and the signal. That is precisely the
// auditor's point — the ratio predicate CANNOT see an edited anchor, because the
// anchor is its input. Only the closed tuple can, and
// TestTimingConfig_RejectsTheKnownBadTuples is where that case lives.
func TestTimingThresholds_PredicateRejectsTheKnownBadConfigurations(t *testing.T) {
	tests := []struct {
		name        string
		gate, noise float64
		wantBlame   string
	}{
		{"the gate that flaked 1 run in 5, against race noise", 1.5, 1.59, "will flake"},
		{"a silently widened gate", 100.0, 1.59, "disarms"},
		{"a 60% widening that an audit found passed", 4.04, 1.59, "disarms"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bad := strings.Join(thresholdViolations(tc.gate, tc.noise, nearestCaughtSignal), " | ")
			if bad == "" {
				t.Fatalf("the predicate ACCEPTED a configuration it must reject "+
					"(gate %.2f, noise %.2f)", tc.gate, tc.noise)
			}
			if !strings.Contains(bad, tc.wantBlame) {
				t.Fatalf("rejected for the wrong reason: %q, want it to mention %q", bad, tc.wantBlame)
			}
		})
	}
	// POSITIVE CONTROL: both shipped tuples must be ACCEPTED, or the predicate is
	// simply refusing everything.
	for _, c := range legalTimingConfigs {
		if bad := thresholdViolations(c.gate, c.noise, nearestCaughtSignal); len(bad) > 0 {
			t.Fatalf("the predicate rejects the named %s tuple: %v", c.build, bad)
		}
	}
}

// TestTimingFloors_AreBetweenTheBrokenAndTheHonestCost pins the two floors the
// same way: each must sit above the cost of the branch it detects and below the
// cost of the branch it permits.
func TestTimingFloors_AreBetweenTheBrokenAndTheHonestCost(t *testing.T) {
	floors := []struct {
		name                   string
		floor                  time.Duration
		honestCost, brokenCost time.Duration
		description            string
	}{
		{"dummyRunFloor", dummyRunFloor, 190 * time.Millisecond, 600 * time.Nanosecond,
			"an unknown-email login pays one full-cost dummy"},
		{"overLongFloor", overLongFloor, 1400 * time.Microsecond, 66 * time.Nanosecond,
			"an over-long password against a KNOWN candidate pays a cost-4 comparison"},
	}
	for _, f := range floors {
		t.Run(f.name, func(t *testing.T) {
			if f.floor >= f.honestCost {
				t.Fatalf("%s is %v, at or above the honest cost it permits (%v) — it will fail "+
					"on correct code: %s", f.name, f.floor, f.honestCost, f.description)
			}
			if f.floor <= f.brokenCost {
				t.Fatalf("%s is %v, at or below the cost of the BROKEN branch (%v) — it cannot "+
					"detect the thing it exists for", f.name, f.floor, f.brokenCost)
			}
			t.Logf("%-14s floor=%-8v broken=%-10v honest=%v (%.0fx above broken, %.0fx below honest)",
				f.name, f.floor, f.brokenCost, f.honestCost,
				float64(f.floor)/float64(f.brokenCost), float64(f.honestCost)/float64(f.floor))
		})
	}
}

// TestTimingConfig_RejectsTheKnownBadTuples is the permanent NEGATIVE CONTROL for
// the closed set, and it carries the exact configurations an audit used to defeat
// the ratio invariant.
func TestTimingConfig_RejectsTheKnownBadTuples(t *testing.T) {
	tests := []struct {
		name    string
		samples int
		gate    float64
		noise   float64
	}{
		{"the audit's repair: gate AND its own anchor lowered together", 3, 1.5, 1.00},
		{"the anchor lowered on its own", 3, 2.5, 1.00},
		{"the gate widened on its own (60%)", 3, 4.0, 1.59},
		{"the gate widened absurdly", 3, 100.0, 1.59},
		{"a single-sample 'median'", 1, 2.5, 1.59},
		{"the plain build's gate with the race build's samples", 3, 1.5, 1.59},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if isNamedTimingConfig(tc.samples, tc.gate, tc.noise) {
				t.Fatalf("the closed set ACCEPTED {samples:%d gate:%.2f noise:%.2f}, which no "+
					"row names", tc.samples, tc.gate, tc.noise)
			}
		})
	}
	// POSITIVE CONTROL: every named tuple must be accepted, or the closed set is
	// simply refusing everything and proves nothing.
	for _, c := range legalTimingConfigs {
		if !isNamedTimingConfig(c.samples, c.gate, c.noise) {
			t.Fatalf("the closed set rejects its own named %s tuple", c.build)
		}
	}
}

// NOTE — A FAKE-DRIVEN unlookupable-address TEST WAS HERE AND WAS DELETED,
// which is worth a paragraph because deleting a test is normally the wrong move.
//
// It drove newFakeManager, whose resolver ACCEPTS EVERY BYTE SEQUENCE. Postgres
// does not: a NUL or invalid UTF-8 in the address is `ERROR: invalid byte sequence
// for encoding "UTF8" (SQLSTATE 22021)`. So with the isLookupableEmail guard
// deleted the fake still returned zero candidates, still paid the dummy, still
// yielded ErrBadCredentials — and the test still passed. A closing audit proved
// exactly that: the guard removed, the WHOLE SUITE green, while the real stack
// answered 500 instead of 401.
//
// IT WAS NOT MERELY WEAK, IT WAS MISLEADING: it named the branch in its own title,
// so a reader counting nets would have counted it. A test that cannot fail is
// worse than no test, because it is also an argument against writing the real one.
// Its replacement is TestAuthenticate_RefusesAnUnlookupableAddress_RealDB in
// manager_db_test.go, which drives real Postgres and goes red on the same mutation.
// Removing it also returned ~10.7 s to `make test-short` (measured, -race), which
// it was spending on five cost-12 dummy comparisons that proved nothing.

// TestAuthenticate_TimingDoesNotCountTheCandidates — the M7-02 round-4 closure.
//
// 🔴 THE CHANNEL IT CLOSES, MEASURED ON THE SHIPPED CODE BEFORE THE FIX. Authenticate
// compared every candidate in the window and padded nothing, so the response time was
// linear in how many identities the address resolved to. With ONE planted row, nine
// interleaved rounds:
//
//	address UNKNOWN     median 216 ms   (208 – 226)
//	address REGISTERED  median 437 ms   (422 – 478)   — the ranges do not overlap
//
// That answered "does this address have an account" for the price of one sign-up,
// which is the question PHASE B OBLIGATION 1 spends a dummy bcrypt refusing to answer.
// The premise that let this package decline padding — "one they can only obtain for an
// address that IS registered more than once" — died when M7-02 made an attacker able
// to CREATE that condition.
//
// IT IS A RATIO ASSERTION, NOT A DELTA. The two arms now do the same NUMBER of
// comparisons (MaxCandidates), so their times track each other whatever the machine is
// doing; a mutation that removes the padding makes the unknown arm 8x faster, which no
// amount of load can imitate.
func TestAuthenticate_TimingDoesNotCountTheCandidates(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: skipping the bcrypt wall-clock SAMPLE. The COUNT this measures is " +
			"also asserted structurally by TestAuthenticate_PadsEveryExitToTheCap.")
	}
	// SHIPPED cost on both arms: the work factor is the subject here.
	digest, err := Hash("the-real-password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	one := newFakeManager(t, []db.ResolvedAdmin{{
		ID: uuid.New(), TenantID: uuid.New(),
		PasswordHash: db.NewPasswordHash(digest), Status: "active",
	}})
	none := newFakeManager(t, nil)

	measure := func(m *Manager) time.Duration {
		start := time.Now()
		_, _ = m.Authenticate(context.Background(), "probe@example.test", "not-the-password")
		return time.Since(start)
	}
	// Interleaved, so load moves both arms together; the minimum is the cleanest
	// estimator of the work actually done.
	// ⚠️ ONE ROUND PER ARM, NOT THREE (M7-02, after measurement). At three it cost
	// 200.1 s under -race — 6 logins x MaxCandidates cost-12 comparisons — which is a
	// quarter of the whole package for a check whose signal is an 8x ratio. Shrunk to
	// the smallest sample that still exercises both arms at the SHIPPED cost; the
	// five-arm comparison at a cheap shared cost is TestAuthenticate_TimingIsFlat's
	// job, and the exact, sample-free statement is
	// TestAuthenticate_PadsEveryExitToTheCap.
	//
	// WHAT THE SHRINK COSTS: with one round there is no min-of-N smoothing, so a
	// single scheduler stall can move a reading. The gate is a factor of TWO against a
	// mutation that produces a factor of EIGHT, so there is 4x of headroom for that
	// noise — and the anti-vacuity floor below independently catches "the padding did
	// not run at all".
	var minNone, minOne time.Duration
	for i := 0; i < 1; i++ {
		if d := measure(none); minNone == 0 || d < minNone {
			minNone = d
		}
		if d := measure(one); minOne == 0 || d < minOne {
			minOne = d
		}
	}
	t.Logf("0 candidates: %v · 1 candidate: %v (both must be %d comparisons)",
		minNone.Round(time.Millisecond), minOne.Round(time.Millisecond), MaxCandidates)

	ratio := float64(minOne) / float64(minNone)
	if ratio < 0.5 || ratio > 2.0 {
		t.Errorf("an unregistered address takes %v and a registered one %v (ratio %.2f). "+
			"They must be within a factor of two: a variable comparison count is a timing "+
			"oracle for 'is this address registered', measured at 216 ms vs 437 ms before "+
			"padComparisons existed.", minNone, minOne, ratio)
	}
	// ANTI-VACUITY: both arms must really be doing the full padded work. One
	// comparison alone cannot take this long at the shipped cost.
	if floor := time.Duration(MaxCandidates/2) * 100 * time.Millisecond; minNone < floor {
		t.Errorf("an unregistered address took %v, which is below the floor %d cost-%d "+
			"comparisons can take (%v) — the padding is not running",
			minNone, MaxCandidates, Cost, floor)
	}
}

// TestAuthenticate_PadsEveryExitToTheCap is the structural half. It reads the source
// rather than the clock: EVERY return path out of Authenticate must have paid
// MaxCandidates comparisons, and there are three of them (the unlookupable-email
// guard, the zero-candidate branch, and the ordinary path).
//
// ⚠️ IT IS A TEXT SCANNER, AND THE LIMIT OF THAT IS NARROWER THAN THIS COMMENT USED TO
// CLAIM. It said "so the property is still checked under -short", full stop. Measured:
// reducing pad's loop to pre-M7-02 behaviour leaves every `m.pad(` call site in place,
// so this test stays GREEN and `go test -short ./...` is green with the channel
// reopened. What catches that mutation is the wall-clock pair
// (TestAuthenticate_TimingDoesNotCountTheCandidates, TestAuthenticate_TimingIsFlat),
// both of which -short skips.
//
// SO THE HONEST STATEMENT IS: under -short this checks that the CALLS are present and
// that the loop's bound is spelled MaxCandidates; it does NOT check that the padding
// runs. That is not a hole in CI — `make check` runs `make test`, which is -race and
// includes both timing tests — but -short alone must not be read as covering this.
func TestAuthenticate_PadsEveryExitToTheCap(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("manager.go")
	if err != nil {
		t.Fatalf("reading manager.go: %v", err)
	}
	s := string(src)
	// 🔴 THE ANCHOR IS `m.pad(`, AND THIS TEST CAUGHT ITS OWN STALENESS. It was written
	// against a free function called padComparisons; the Ö1 fix made padding a METHOD
	// (it needs the manager's dummy), and this assertion went red on the rename — which
	// is the behaviour wanted from a source-level tripwire, but it is also the reason
	// the anchor is now the CALL rather than a name that can drift silently.
	if n := strings.Count(s, "m.pad("); n < 3 {
		t.Fatalf("m.pad is called %d time(s); Authenticate has THREE return paths that must "+
			"each have paid MaxCandidates comparisons (the unlookupable-email guard, the "+
			"zero-candidate branch and the ordinary path). A path that skips it is a login "+
			"whose comparison count reveals how many identities the address resolved to", n)
	}
	if !strings.Contains(s, "for i := done; i < MaxCandidates; i++ {") {
		t.Error("padComparisons no longer pads up to MaxCandidates, so the total is not " +
			"constant and the count is observable in the response time")
	}
	// CompareDummy must not be reachable outside the padding: a bare call would be a
	// second, unpadded exit.
	body := s
	if i := strings.Index(body, "func (m *Manager) pad("); i >= 0 {
		body = body[:i]
	}
	if strings.Contains(body, "CompareDummy(") {
		t.Error("Authenticate calls CompareDummy directly; every dummy must go through " +
			"padComparisons or the total stops being constant")
	}
}
