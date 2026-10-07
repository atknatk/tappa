package operatorauth

// passwordok_test.go -- OP-14 phase C's 'password_ok' writer, the parts that are THIS
// process's and need no database: its per-operator cap is one locked step under racers,
// it is keyed by the operator and not the address, it restarts with its window, and its
// crossing is ONE WARN line per window carrying the operator's id and the numbers alone.
// What the database receives and keeps is passwordok_db_test.go's.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
)

// trailCapMsg is recordPasswordOK's WARN line (flow.go). The message names no
// credential: redline R7 reads every log call's arguments for the word "password".
const trailCapMsg = "operator first-factor audit rows suppressed for this operator for the rest of the window"

// trailRefusedMsg is recordPasswordOK's fail-closed line (flow.go, refused; 3rd round, F4).
const trailRefusedMsg = "operator first-factor audit row not written; this operator's sign-in step was refused"

// trailStore is the in-memory Store of these tests: active operators by address, and the
// pre-session rows the flow asked for, by kind and operator. Every other method is
// nopStore's -- it fails the test if called.
type trailStore struct {
	nopStore
	mu       sync.Mutex
	accounts map[string]db.OperatorAccount
	rows     map[db.OperatorAuthEvent]map[uuid.UUID]int
	// byAddress counts the rows asked for WITH an address: 'password_ok' names its
	// operator by id alone (00031 refuses the other shape with 22023).
	byAddress int
	// attempts numbers the writes asked for, from 1. beforeWrite runs ahead of the n-th,
	// OUTSIDE the store's lock (a test may sign in again from it); fail, if it returns an
	// error, refuses the n-th and records nothing -- a database that refused the row.
	attempts    int
	beforeWrite func(n int)
	fail        func(n int) error
}

func newTrailStore(t *testing.T, accounts map[string]uuid.UUID) *trailStore {
	s := &trailStore{nopStore: nopStore{t}, accounts: map[string]db.OperatorAccount{}, rows: map[db.OperatorAuthEvent]map[uuid.UUID]int{}}
	for email, id := range accounts {
		s.accounts[email] = db.OperatorAccount{ID: id, Digest: db.NewPasswordHash("not compared: compareFn is replaced")}
	}
	return s
}

func (s *trailStore) OperatorByEmail(_ context.Context, email string) (db.OperatorAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.accounts[email]; ok {
		return a, nil
	}
	return db.OperatorAccount{}, db.ErrNoOperator
}

func (s *trailStore) RecordOperatorAuthEvent(_ context.Context, kind db.OperatorAuthEvent, email string, id uuid.UUID) error {
	s.mu.Lock()
	s.attempts++
	n, hook, fail := s.attempts, s.beforeWrite, s.fail
	s.mu.Unlock()
	if hook != nil {
		hook(n)
	}
	if fail != nil {
		if err := fail(n); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if email != "" {
		s.byAddress++
	}
	if s.rows[kind] == nil {
		s.rows[kind] = map[uuid.UUID]int{}
	}
	s.rows[kind][id]++
	return nil
}

func (s *trailStore) count(kind db.OperatorAuthEvent, id uuid.UUID) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rows[kind][id]
}

// newTrailAuth is an Authenticator on the constructor path (build) over s, its clock
// read through *clock (a test moves the windows by moving it), its comparison replaced
// by one that accepts: the comparison is not these tests' subject, and a cost-12 bcrypt
// per racer would make the race test minutes long.
func newTrailAuth(t *testing.T, s Store, clock *time.Time, logs *strings.Builder) *Authenticator {
	t.Helper()
	dummy, err := sharedDummy()
	if err != nil {
		t.Fatalf("dummy digest: %v", err)
	}
	a := build(s, Config{
		TOTPKEK: NewKey(testKey(t)), TokenHMACKey: NewKey(testKey(t)),
		Now: func() time.Time { return *clock }, Log: newTestLogger(logs),
	}, dummy)
	a.compareFn = func([]byte, []byte) error { return nil }
	return a
}

// firstFactorLineRE is the shape of both first-factor WARN lines in the text handler: the
// message, the kind, the operator's id, the cap and the window -- nothing else, and in
// particular nothing the request carried and no error text.
var firstFactorLineRE = regexp.MustCompile(`^time=\S+ level=WARN msg="(` + regexp.QuoteMeta(trailCapMsg) + `|` +
	regexp.QuoteMeta(trailRefusedMsg) + `)" kind=password_ok operator_id=([0-9a-f-]{36}) cap=10 window=10m0s$`)

// checkFirstFactorLog requires every line logged to be one of the two first-factor lines
// and the operators each names to be exactly capIDs and refusedIDs, in any order: a line
// of any other shape -- an Info line per right password (OP-6 12c's, which the durable row
// replaced), a WARN per suppressed row or per refusal, an attribute added -- is red. A
// line is never printed: one that is not these can carry anything.
func checkFirstFactorLog(t *testing.T, logText string, capIDs, refusedIDs []uuid.UUID) {
	t.Helper()
	got := map[string][]string{}
	for _, l := range strings.Split(logText, "\n") {
		if l == "" {
			continue
		}
		m := firstFactorLineRE.FindStringSubmatch(l)
		if m == nil {
			t.Errorf("a log line that is not one of the operator's first-factor lines (%d characters)", len(l))
			continue
		}
		got[m[1]] = append(got[m[1]], m[2])
	}
	for msg, ids := range map[string][]uuid.UUID{trailCapMsg: capIDs, trailRefusedMsg: refusedIDs} {
		want := make([]string, 0, len(ids))
		for _, id := range ids {
			want = append(want, id.String())
		}
		sort.Strings(got[msg])
		sort.Strings(want)
		if strings.Join(got[msg], ",") != strings.Join(want, ",") {
			t.Errorf("%d line(s) %q, want %d, one per window, naming those operators", len(got[msg]), msg, len(want))
		}
	}
}

// checkOnlyTrailCapLines is checkFirstFactorLog with no refusal expected.
func checkOnlyTrailCapLines(t *testing.T, logText string, ids ...uuid.UUID) {
	t.Helper()
	checkFirstFactorLog(t, logText, ids, nil)
}

// TestPasswordOK_TheCapIsOneLockedStepUnderRacers: racers, more than the cap, present
// ONE operator's right password at the same instant, each from its own address (so the
// per-address work budget refuses none of them), round after round on a fresh
// Authenticator. Every round writes EXACTLY firstFactorLimit rows. A racer past the cap is
// served without a row only once the cap's rows are WRITTEN; one that comes while some
// are still being written is refused fail-closed (3rd round, F1) and gives its charge
// back -- so every racer is either served or refused with errFirstFactorPending, the
// counter holds the charges of the served, and each line appears at most once. The
// decision and the charge are one locked step (limits.go, take): a read of the window
// followed by a charge -- httpx.Limiter's "Allowed, then Charge", backlog T90's class --
// lets racers that all read "room left" before any of them charged write past the cap.
func TestPasswordOK_TheCapIsOneLockedStepUnderRacers(t *testing.T) {
	const rounds, racers = 200, 4 * firstFactorLimit
	id := uuid.New()
	clock := time.Unix(1_800_000_000, 0)
	for r := 0; r < rounds; r++ {
		st := newTrailStore(t, map[string]uuid.UUID{"racer@example.test": id})
		var logs strings.Builder
		a := newTrailAuth(t, st, &clock, &logs)
		var served, pending atomic.Int64
		var ready, done sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < racers; i++ {
			ready.Add(1)
			done.Add(1)
			go func(addr string) {
				defer done.Done()
				ready.Done()
				<-start
				c, err := a.Password(context.Background(), addr, "racer@example.test", "any")
				switch {
				case err == nil && c.reveal() != "":
					served.Add(1)
				case errors.Is(err, errFirstFactorPending) && c.reveal() == "":
					pending.Add(1)
				default:
					t.Errorf("round %d: a racer ended with %v", r, err)
				}
			}("198.51.100." + itoa(i+1))
		}
		ready.Wait()
		close(start)
		done.Wait()
		rows, spent := st.count(db.OperatorPasswordOK, id), spentIn(a.limits.firstFactor, id.String())
		capLines, refusedLines := strings.Count(logs.String(), trailCapMsg), strings.Count(logs.String(), trailRefusedMsg)
		if rows != firstFactorLimit || served.Load()+pending.Load() != racers || int64(spent) != served.Load() ||
			capLines != min(int(served.Load())-firstFactorLimit, 1) || refusedLines != min(int(pending.Load()), 1) {
			t.Fatalf("round %d of %d: %d racers wrote %d row(s); %d served, %d refused pending, %d charged; %d cap and %d refusal line(s); want %d rows, the counter at the served, one line of each kind that happened",
				r+1, rounds, racers, rows, served.Load(), pending.Load(), spent, capLines, refusedLines, firstFactorLimit)
		}
	}
}

// TestPasswordOK_TheCapIsPerOperatorAndRestartsWithItsWindow: one operator's right
// password from firstFactorLimit+2 different addresses writes firstFactorLimit rows -- the
// cap is the operator's, the address buys nothing -- serves all of them and logs ONE
// line; another operator's row is still written (the key is the operator); a full
// period later the first operator's window has restarted -- a row again, and a second
// line only once that window is crossed too. No row is ever asked for with an address.
func TestPasswordOK_TheCapIsPerOperatorAndRestartsWithItsWindow(t *testing.T) {
	x, y := uuid.New(), uuid.New()
	st := newTrailStore(t, map[string]uuid.UUID{"x@example.test": x, "y@example.test": y})
	clock := time.Unix(1_800_000_000, 0)
	var logs strings.Builder
	a := newTrailAuth(t, st, &clock, &logs)
	n := 0
	sign := func(email string) {
		t.Helper()
		n++
		c, err := a.Password(context.Background(), "203.0.113."+itoa(n), email, "any")
		if err != nil || c.reveal() == "" {
			t.Fatalf("request %d (%s) was not served: %v", n, email, err)
		}
	}
	for i := 0; i < firstFactorLimit+2; i++ {
		sign("x@example.test")
		if w := strings.Count(logs.String(), trailCapMsg); i < firstFactorLimit && w != 0 {
			t.Fatalf("right password %d of %d, within the cap: %d WARN line(s), want 0 -- the line is for the first row NOT written",
				i+1, firstFactorLimit, w)
		}
	}
	if got, w := st.count(db.OperatorPasswordOK, x), strings.Count(logs.String(), trailCapMsg); got != firstFactorLimit || w != 1 {
		t.Fatalf("%d right passwords of one operator from as many addresses: %d row(s), %d line(s); want %d and 1",
			firstFactorLimit+2, got, w, firstFactorLimit)
	}
	sign("y@example.test")
	if got := st.count(db.OperatorPasswordOK, y); got != 1 {
		t.Fatalf("another operator, while the first is past its cap: %d row(s), want 1", got)
	}
	clock = clock.Add(firstFactorPeriod)
	sign("x@example.test")
	if got, w := st.count(db.OperatorPasswordOK, x), strings.Count(logs.String(), trailCapMsg); got != firstFactorLimit+1 || w != 1 {
		t.Fatalf("a period later: %d row(s), %d line(s); want %d (the window restarted) and still 1", got, w, firstFactorLimit+1)
	}
	for i := 0; i < firstFactorLimit; i++ {
		sign("x@example.test")
	}
	if got, w := st.count(db.OperatorPasswordOK, x), strings.Count(logs.String(), trailCapMsg); got != 2*firstFactorLimit || w != 2 {
		t.Fatalf("the second window crossed: %d row(s), %d line(s); want %d and 2", got, w, 2*firstFactorLimit)
	}
	if st.byAddress != 0 {
		t.Fatalf("%d row(s) asked for with an address; 'password_ok' names its operator by id alone", st.byAddress)
	}
	checkOnlyTrailCapLines(t, logs.String(), x, x)
	for i := 1; i <= n; i++ {
		if strings.Contains(logs.String(), "203.0.113."+itoa(i)) {
			t.Fatal("the log carries a client address")
		}
	}
	if strings.Contains(logs.String(), "@example.test") {
		t.Fatal("the log carries an address of an operator")
	}
}

// errRefusedWrite stands for a database that refused the row (internal/db's shape: a
// fixed text and a SQLSTATE, nothing the request carried).
var errRefusedWrite = errors.New("db: record operator auth event: database error (SQLSTATE 25006)")

// TestPasswordOK_EveryWriteErrorFailsClosedAndGivesTheChargeBack: whatever error the row's
// write returns -- the database's refusal, its 28000 (db.ErrOperatorRefused), a cancelled
// or expired context, a broken connection -- the password step FAILS: an error that is
// none of the sentinels a caller answers with a page, no challenge, no row; and the cap's
// charge is GIVEN BACK (the operator's counter is where it was). After the write, the
// request's own cancellation no longer reaches it (it is detached), so the context arms
// are built at the write itself. 3rd round (F4): two refusals in a window name the
// operator in ONE line, in the closed shape -- no address, no error text.
func TestPasswordOK_EveryWriteErrorFailsClosedAndGivesTheChargeBack(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"the database refuses the row (25006)", errRefusedWrite},
		{"28000, the definer's refusal", db.ErrOperatorRefused},
		{"the write's context cancelled", fmt.Errorf("db: record operator auth event: %w", context.Canceled)},
		{"the write's context expired", fmt.Errorf("db: record operator auth event: %w", context.DeadlineExceeded)},
		{"the connection broke", errors.New("db: record operator auth event: conn closed")},
		{"22023, a database without 00031", errors.New("db: record operator auth event: database error (SQLSTATE 22023)")},
	}
	for _, tc := range tests {
		id := uuid.New()
		st := newTrailStore(t, map[string]uuid.UUID{"x@example.test": id})
		st.fail = func(int) error { return tc.err }
		clock := time.Unix(1_800_000_000, 0)
		var logs strings.Builder
		a := newTrailAuth(t, st, &clock, &logs)
		c, err := a.Password(context.Background(), "192.0.2.50", "x@example.test", "any")
		if err == nil || c.reveal() != "" {
			t.Errorf("%s: the step was served (challenge minted=%v); want an error and no challenge", tc.name, c.reveal() != "")
			continue
		}
		// A second refusal in the same window: the operator is named ONCE per window.
		if _, again := a.Password(context.Background(), "192.0.2.51", "x@example.test", "any"); again == nil {
			t.Errorf("%s: the second request was served", tc.name)
		}
		checkFirstFactorLog(t, logs.String(), nil, []uuid.UUID{id})
		for _, sentinel := range []error{ErrRefused, ErrThrottled, ErrChallenge, ErrCodeRejected, ErrLocked, ErrNoSession} {
			if errors.Is(err, sentinel) {
				t.Errorf("%s: answered as %v; a refused trail is a server failure", tc.name, sentinel)
			}
		}
		if got, spent := st.count(db.OperatorPasswordOK, id), spentIn(a.limits.firstFactor, id.String()); got != 0 || spent != 0 {
			t.Errorf("%s: %d row(s), the operator's cap at %d; want 0 and 0 (the charge given back)", tc.name, got, spent)
		}
		if strings.Contains(logs.String(), "192.0.2.5") || strings.Contains(logs.String(), "x@example.test") || strings.Contains(err.Error(), id.String()) ||
			strings.Contains(logs.String(), "SQLSTATE") || strings.Contains(logs.String(), "conn closed") || strings.Contains(logs.String(), "context") {
			t.Errorf("%s: the log or the error carries an address or the operator's id", tc.name)
		}
	}
}

// TestPasswordOK_AFailedWriteGivesItsOwnChargeBack: a write the database refuses gives
// back ITS charge -- and only its own.
//   - firstFactorLimit refused writes in a row leave the cap untouched: the next
//     firstFactorLimit right passwords all write, the one after is past the cap.
//   - a refusal in the middle of a window gives back one charge, not the window's: five
//     rows, one refusal, then the cap still ends at firstFactorLimit rows in all.
//   - a refusal whose window has EXPIRED while the write ran gives nothing back to the
//     window that replaced it, which holds another request's charge.
//   - the window's one WARN line stays one when a refund brings the count back under the
//     cap and a later request crosses it again.
func TestPasswordOK_AFailedWriteGivesItsOwnChargeBack(t *testing.T) {
	id := uuid.New()
	sign := func(t *testing.T, a *Authenticator, n int) error {
		t.Helper()
		_, err := a.Password(context.Background(), "198.51.100."+itoa(n), "x@example.test", "any")
		return err
	}
	t.Run("a run of refusals spends nothing", func(t *testing.T) {
		st := newTrailStore(t, map[string]uuid.UUID{"x@example.test": id})
		st.fail = func(n int) error {
			if n <= firstFactorLimit {
				return errRefusedWrite
			}
			return nil
		}
		clock := time.Unix(1_800_000_000, 0)
		var logs strings.Builder
		a := newTrailAuth(t, st, &clock, &logs)
		for i := 1; i <= firstFactorLimit; i++ {
			if sign(t, a, i) == nil {
				t.Fatalf("refused write %d: the step was served", i)
			}
		}
		if spent := spentIn(a.limits.firstFactor, id.String()); spent != 0 {
			t.Fatalf("%d refused writes left the cap at %d, want 0", firstFactorLimit, spent)
		}
		for i := 1; i <= firstFactorLimit+1; i++ {
			if err := sign(t, a, 100+i); err != nil {
				t.Fatalf("right password %d after the refusals: %v", i, err)
			}
		}
		if got, w := st.count(db.OperatorPasswordOK, id), strings.Count(logs.String(), trailCapMsg); got != firstFactorLimit || w != 1 {
			t.Fatalf("after %d refusals and %d right passwords: %d row(s), %d line(s); want %d and 1",
				firstFactorLimit, firstFactorLimit+1, got, w, firstFactorLimit)
		}
	})
	t.Run("a refusal gives back one charge, not the window's", func(t *testing.T) {
		st := newTrailStore(t, map[string]uuid.UUID{"x@example.test": id})
		st.fail = func(n int) error {
			if n == 6 {
				return errRefusedWrite
			}
			return nil
		}
		clock := time.Unix(1_800_000_000, 0)
		var logs strings.Builder
		a := newTrailAuth(t, st, &clock, &logs)
		for i := 1; i <= 2*firstFactorLimit; i++ {
			_ = sign(t, a, i)
		}
		if got, spent := st.count(db.OperatorPasswordOK, id), spentIn(a.limits.firstFactor, id.String()); got != firstFactorLimit || spent != 2*firstFactorLimit-1 {
			t.Fatalf("five rows, a refusal, then the rest: %d row(s), the cap at %d; want %d and %d",
				got, spent, firstFactorLimit, 2*firstFactorLimit-1)
		}
	})
	t.Run("a refusal never reaches the window that replaced its own", func(t *testing.T) {
		st := newTrailStore(t, map[string]uuid.UUID{"x@example.test": id})
		clock := time.Unix(1_800_000_000, 0)
		var logs strings.Builder
		a := newTrailAuth(t, st, &clock, &logs)
		st.beforeWrite = func(n int) {
			if n == 1 {
				// While the first write runs its window expires and another right password
				// opens the next one and writes its row.
				clock = clock.Add(firstFactorPeriod)
				if err := sign(t, a, 200); err != nil {
					t.Errorf("the request in the next window: %v", err)
				}
			}
		}
		st.fail = func(n int) error {
			if n == 1 {
				return errRefusedWrite
			}
			return nil
		}
		if sign(t, a, 201) == nil {
			t.Fatal("the refused write was served")
		}
		if got, spent := st.count(db.OperatorPasswordOK, id), spentIn(a.limits.firstFactor, id.String()); got != 1 || spent != 1 {
			t.Fatalf("a refusal from the expired window: %d row(s), the new window at %d; want 1 and 1 (its charge is the other request's)", got, spent)
		}
	})
	t.Run("one line of each kind per window across a refund", func(t *testing.T) {
		st := newTrailStore(t, map[string]uuid.UUID{"x@example.test": id})
		clock := time.Unix(1_800_000_000, 0)
		var logs strings.Builder
		a := newTrailAuth(t, st, &clock, &logs)
		for i := 1; i < firstFactorLimit; i++ {
			if err := sign(t, a, i); err != nil {
				t.Fatalf("right password %d: %v", i, err)
			}
		}
		// The window's last slot: its write runs while another request finds the count past
		// the cap with only nine rows written -- refused fail-closed (3rd round, F1), not
		// served without a row -- and is then refused itself: the count goes back under the
		// cap. Then one more row is written and the requests after it are served past the
		// cap: ONE suppression line, ONE refusal line, in the window.
		st.beforeWrite = func(n int) {
			if n == firstFactorLimit {
				if err := sign(t, a, 300); !errors.Is(err, errFirstFactorPending) {
					t.Errorf("the request past the cap while a slot's write runs: %v, want errFirstFactorPending", err)
				}
			}
		}
		st.fail = func(n int) error {
			if n == firstFactorLimit {
				return errRefusedWrite
			}
			return nil
		}
		if sign(t, a, 301) == nil {
			t.Fatal("the refused write was served")
		}
		st.beforeWrite = nil
		for i := 0; i < 3; i++ {
			if err := sign(t, a, 400+i); err != nil {
				t.Fatalf("right password %d after the refund: %v", i+1, err)
			}
		}
		if got := st.count(db.OperatorPasswordOK, id); got != firstFactorLimit {
			t.Fatalf("%d row(s), want %d", got, firstFactorLimit)
		}
		checkFirstFactorLog(t, logs.String(), []uuid.UUID{id}, []uuid.UUID{id})
	})
}

// TestPasswordOK_RefundsUnderRacersNeverOverspendTheCap: racers, more than the cap, one
// operator, every third write refused by the database -- round after round. The rows
// never exceed the cap; every racer is answered (a row, a suppression, a refused trail's
// error, or -- 3rd round, F1 -- a refusal because the cap's rows were not all written);
// the counter ends at exactly the charges nobody gave back (never below them, never below
// zero); a racer is served WITHOUT a row only in a round whose rows reached the cap; and
// each line appears at most once.
func TestPasswordOK_RefundsUnderRacersNeverOverspendTheCap(t *testing.T) {
	const rounds, racers = 100, 4 * firstFactorLimit
	id := uuid.New()
	clock := time.Unix(1_800_000_000, 0)
	for r := 0; r < rounds; r++ {
		st := newTrailStore(t, map[string]uuid.UUID{"racer@example.test": id})
		var refused atomic.Int64
		st.fail = func(n int) error {
			if n%3 == 0 {
				refused.Add(1)
				return errRefusedWrite
			}
			return nil
		}
		var logs strings.Builder
		a := newTrailAuth(t, st, &clock, &logs)
		var served, failed, pending atomic.Int64
		var ready, done sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < racers; i++ {
			ready.Add(1)
			done.Add(1)
			go func(addr string) {
				defer done.Done()
				ready.Done()
				<-start
				_, err := a.Password(context.Background(), addr, "racer@example.test", "any")
				switch {
				case err == nil:
					served.Add(1)
				case errors.Is(err, errFirstFactorPending):
					pending.Add(1)
					failed.Add(1)
				default:
					failed.Add(1)
				}
			}("198.51.100." + itoa(i+1))
		}
		ready.Wait()
		close(start)
		done.Wait()
		rows, spent := st.count(db.OperatorPasswordOK, id), spentIn(a.limits.firstFactor, id.String())
		capLines, refusedLines := strings.Count(logs.String(), trailCapMsg), strings.Count(logs.String(), trailRefusedMsg)
		if rows > firstFactorLimit || served.Load()+failed.Load() != racers || failed.Load() != refused.Load()+pending.Load() ||
			int64(spent) != racers-failed.Load() || capLines > 1 || refusedLines != min(int(failed.Load()), 1) ||
			(served.Load() > int64(rows) && rows != firstFactorLimit) {
			t.Fatalf("round %d of %d: %d row(s) (cap %d), %d served + %d failed (%d refused writes, %d pending) of %d, the counter at %d (want %d), %d cap and %d refusal line(s)",
				r+1, rounds, rows, firstFactorLimit, served.Load(), failed.Load(), refused.Load(), pending.Load(), racers, spent, racers-failed.Load(), capLines, refusedLines)
		}
	}
}

// TestPasswordOK_AHangingWriteIsBoundedByItsGrace (3rd round, F3): a 'password_ok' write
// that never returns on its own -- a pool that never hands out a connection -- is bounded
// by FirstFactorRecordGrace alone (the request's cancellation and deadline are dropped by
// the detach; here the request's own context has an hour). The step fails after about
// that long: no challenge, the charge given back, the operator named once.
func TestPasswordOK_AHangingWriteIsBoundedByItsGrace(t *testing.T) {
	id := uuid.New()
	st := &hangStore{trailStore: newTrailStore(t, map[string]uuid.UUID{"x@example.test": id})}
	clock := time.Unix(1_800_000_000, 0)
	var logs strings.Builder
	a := newTrailAuth(t, st, &clock, &logs)
	rctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	type result struct {
		served bool
		err    error
	}
	done := make(chan result, 1)
	start := time.Now()
	go func() {
		c, err := a.Password(rctx, "192.0.2.77", "x@example.test", "any")
		done <- result{c.reveal() != "", err}
	}()
	select {
	case r := <-done:
		took := time.Since(start)
		if r.served || r.err == nil || took < FirstFactorRecordGrace-time.Second || took > FirstFactorRecordGrace+2*time.Second {
			t.Fatalf("a hanging write: served=%v err=%v after %v; want refused after about %v", r.served, r.err, took.Round(10*time.Millisecond), FirstFactorRecordGrace)
		}
	case <-time.After(FirstFactorRecordGrace + 5*time.Second):
		t.Fatalf("UNBOUNDED: the write is still hanging %v after the step began (FirstFactorRecordGrace %v)", time.Since(start).Round(time.Second), FirstFactorRecordGrace)
	}
	if spent := spentIn(a.limits.firstFactor, id.String()); spent != 0 {
		t.Fatalf("the timed-out write kept its charge: %d", spent)
	}
	checkFirstFactorLog(t, logs.String(), nil, []uuid.UUID{id})
}

// hangStore's row write waits for its context to end, as a pool that never hands out a
// connection does, and returns that context's error.
type hangStore struct{ *trailStore }

func (h *hangStore) RecordOperatorAuthEvent(ctx context.Context, _ db.OperatorAuthEvent, _ string, _ uuid.UUID) error {
	<-ctx.Done()
	return fmt.Errorf("db: record operator auth event: %w", ctx.Err())
}

// TestPasswordOK_ACapFullOfUnwrittenRowsServesNoChallenge (3rd round, F1, in memory; the
// database's half is TestPasswordOK_DB_ACapFullOfUnwrittenRowsServesNoChallenge):
// firstFactorLimit right passwords whose writes are held in flight, and one more right
// password while they are. The extra one is REFUSED (errFirstFactorPending, no challenge,
// its charge given back) whether the held writes then fail or succeed; once they have
// succeeded, the next one is served past the cap without a row.
func TestPasswordOK_ACapFullOfUnwrittenRowsServesNoChallenge(t *testing.T) {
	for _, writesFail := range []bool{true, false} {
		id := uuid.New()
		st := newTrailStore(t, map[string]uuid.UUID{"x@example.test": id})
		gate, held := make(chan struct{}), make(chan struct{}, firstFactorLimit)
		st.beforeWrite = func(int) { held <- struct{}{}; <-gate }
		if writesFail {
			st.fail = func(int) error { return errRefusedWrite }
		}
		clock := time.Unix(1_800_000_000, 0)
		var logs strings.Builder
		a := newTrailAuth(t, st, &clock, &logs)
		var wg sync.WaitGroup
		var served atomic.Int64
		for i := 0; i < firstFactorLimit; i++ {
			wg.Add(1)
			go func(addr string) {
				defer wg.Done()
				if c, err := a.Password(context.Background(), addr, "x@example.test", "any"); err == nil && c.reveal() != "" {
					served.Add(1)
				}
			}("198.51.100." + itoa(i+1))
		}
		for i := 0; i < firstFactorLimit; i++ {
			<-held
		}
		// The extra request runs aside, bounded: one that writes a row of its own would wait at
		// the gate (a mutant without the cap does), and the test must say so, not hang.
		type answer struct {
			c   Challenge
			err error
		}
		extra := make(chan answer, 1)
		go func() {
			c, err := a.Password(context.Background(), "198.51.100.200", "x@example.test", "any")
			extra <- answer{c, err}
		}()
		var got answer
		select {
		case got = <-extra:
		case <-time.After(10 * time.Second):
			close(gate)
			t.Fatalf("writes fail=%v: the request past a cap of writes in flight did not return -- it waits on a write of its own", writesFail)
		}
		if !errors.Is(got.err, errFirstFactorPending) || got.c.reveal() != "" {
			t.Fatalf("writes fail=%v: the request past a cap of writes in flight: %v (challenge minted=%v); want errFirstFactorPending and none", writesFail, got.err, got.c.reveal() != "")
		}
		close(gate)
		wg.Wait()
		rows, spent := st.count(db.OperatorPasswordOK, id), spentIn(a.limits.firstFactor, id.String())
		if writesFail {
			if rows != 0 || spent != 0 || served.Load() != 0 {
				t.Fatalf("writes fail: %d row(s), the counter at %d, %d served; want 0, 0, 0", rows, spent, served.Load())
			}
			checkFirstFactorLog(t, logs.String(), nil, []uuid.UUID{id})
			continue
		}
		if rows != firstFactorLimit || spent != firstFactorLimit {
			t.Fatalf("writes succeed: %d row(s), the counter at %d; want %d and %d", rows, spent, firstFactorLimit, firstFactorLimit)
		}
		if c, err := a.Password(context.Background(), "198.51.100.201", "x@example.test", "any"); err != nil || c.reveal() == "" {
			t.Fatalf("writes succeed: the request past a cap of WRITTEN rows: %v; want it served", err)
		}
		checkFirstFactorLog(t, logs.String(), []uuid.UUID{id}, []uuid.UUID{id})
	}
}

// windowState is a copy of key's window in b, read under b's lock, and whether b holds one
// at all: the written count and the line flags show in no answer the step gives.
func windowState(b *budget, key string) (budgetWindow, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	w, ok := b.windows[key]
	if !ok {
		return budgetWindow{}, false
	}
	return *w, true
}

// firstFactorAnswer is one Password call's result, carried out of the goroutine a test
// runs it in.
type firstFactorAnswer struct {
	c   Challenge
	err error
}

// passwordAside runs one right password aside and waits for it at most 10 s: a request
// that writes a row of its own would wait at a held write's gate (a mutant does), and the
// test must say so, not hang.
func passwordAside(t *testing.T, a *Authenticator, addr, email, given string) firstFactorAnswer {
	t.Helper()
	out := make(chan firstFactorAnswer, 1)
	go func() {
		c, err := a.Password(context.Background(), addr, email, given)
		out <- firstFactorAnswer{c, err}
	}()
	select {
	case got := <-out:
		return got
	case <-time.After(10 * time.Second):
		t.Fatal("the request did not return -- it waits on a write of its own")
	}
	return firstFactorAnswer{}
}

// TestPasswordOK_RowsThatLandAfterTheirWindowCountInTheirOwn (5th round, the closing
// audit's D1, its Q2): a written row counts into the window whose charge it spent, never
// into the operator's CURRENT window. firstFactorLimit right passwords' writes are held;
// the window turns; firstFactorLimit more are held in the new window; then the first ones
// LAND. A right password in the new window -- none of whose own rows is written -- is
// refused fail-closed (errFirstFactorPending, no challenge). Counted into the new window,
// the old window's late rows would make it look written and serve that request a
// challenge with no row of its own: F1 back at a window boundary. Then the new window's
// writes fail: firstFactorLimit rows in all, the new window's counter and written count at
// zero, one refusal line.
func TestPasswordOK_RowsThatLandAfterTheirWindowCountInTheirOwn(t *testing.T) {
	id := uuid.New()
	st := newTrailStore(t, map[string]uuid.UUID{"x@example.test": id})
	clock := time.Unix(1_800_000_000, 0)
	var logs strings.Builder
	a := newTrailAuth(t, st, &clock, &logs)
	oldGate, newGate := make(chan struct{}), make(chan struct{})
	var oldOnce, newOnce sync.Once
	releaseOld := func() { oldOnce.Do(func() { close(oldGate) }) }
	releaseNew := func() { newOnce.Do(func() { close(newGate) }) }
	defer releaseOld()
	defer releaseNew()
	held := make(chan struct{}, 2*firstFactorLimit)
	st.beforeWrite = func(n int) {
		switch {
		case n <= firstFactorLimit:
			held <- struct{}{}
			<-oldGate
		case n <= 2*firstFactorLimit:
			held <- struct{}{}
			<-newGate
		}
	}
	st.fail = func(n int) error {
		if n > firstFactorLimit && n <= 2*firstFactorLimit {
			return errRefusedWrite
		}
		return nil
	}
	// sign starts firstFactorLimit right passwords and returns once all their writes are
	// held: every one has charged its window before the caller moves the clock.
	sign := func(wg *sync.WaitGroup, served *atomic.Int64, first int) {
		for i := 0; i < firstFactorLimit; i++ {
			wg.Add(1)
			go func(addr string) {
				defer wg.Done()
				if c, err := a.Password(context.Background(), addr, "x@example.test", "any"); err == nil && c.reveal() != "" {
					served.Add(1)
				}
			}("198.51.100." + itoa(first+i))
		}
		for i := 0; i < firstFactorLimit; i++ {
			<-held
		}
	}
	var oldWG, newWG sync.WaitGroup
	var oldServed, newServed atomic.Int64
	sign(&oldWG, &oldServed, 1)
	clock = clock.Add(firstFactorPeriod)
	sign(&newWG, &newServed, 50)
	releaseOld()
	oldWG.Wait()
	got := passwordAside(t, a, "198.51.100.200", "x@example.test", "any")
	if !errors.Is(got.err, errFirstFactorPending) || got.c.reveal() != "" {
		t.Fatalf("the request in a window none of whose rows is written, after the previous window's rows landed: %v (challenge minted=%v); want errFirstFactorPending and none",
			got.err, got.c.reveal() != "")
	}
	releaseNew()
	newWG.Wait()
	rows, spent := st.count(db.OperatorPasswordOK, id), spentIn(a.limits.firstFactor, id.String())
	w, _ := windowState(a.limits.firstFactor, id.String())
	if oldServed.Load() != firstFactorLimit || newServed.Load() != 0 || rows != firstFactorLimit || spent != 0 || w.written != 0 {
		t.Fatalf("old window served %d, new window served %d; %d row(s); the new window's counter %d, written %d; want %d, 0, %d, 0, 0",
			oldServed.Load(), newServed.Load(), rows, spent, w.written, firstFactorLimit, firstFactorLimit)
	}
	checkFirstFactorLog(t, logs.String(), nil, []uuid.UUID{id})
}

// TestPasswordOK_WrongPasswordsLeaveTheOperatorsWindowUntouched (5th round, D2, the closing
// audit's Q5; the in-memory neighbour of TestPasswordOK_WrongPasswordsToItsAddressNeverSpendTheCap):
// a wrong password to the operator's address opens no first-factor window -- it neither
// charges the cap nor counts as a written row. firstFactorLimit+2 wrong ones, then
// firstFactorLimit right ones whose writes are held and then fail, and one more right one
// while they are held: that one is refused (errFirstFactorPending, no challenge), and the
// window ends with no row and no charge. Had the wrong ones counted as written rows, it
// would be served past the cap with no row at all.
func TestPasswordOK_WrongPasswordsLeaveTheOperatorsWindowUntouched(t *testing.T) {
	id := uuid.New()
	st := newTrailStore(t, map[string]uuid.UUID{"x@example.test": id})
	clock := time.Unix(1_800_000_000, 0)
	var logs strings.Builder
	a := newTrailAuth(t, st, &clock, &logs)
	a.compareFn = func(_, given []byte) error {
		if string(given) == "right" {
			return nil
		}
		return errors.New("operatorauth test: not the right one")
	}
	for i := 0; i < firstFactorLimit+2; i++ {
		if _, err := a.Password(context.Background(), "192.0.2."+itoa(100+i), "x@example.test", "wrong"); !errors.Is(err, ErrRefused) {
			t.Fatalf("PREMISE: wrong one %d: %v", i+1, err)
		}
	}
	if w, ok := windowState(a.limits.firstFactor, id.String()); ok {
		t.Fatalf("%d wrong ones opened a first-factor window (counter %d, written %d); want none",
			firstFactorLimit+2, w.count, w.written)
	}
	st.mu.Lock()
	before := st.attempts
	st.mu.Unlock()
	gate, held := make(chan struct{}), make(chan struct{}, firstFactorLimit)
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	defer release()
	st.beforeWrite = func(n int) {
		if n > before {
			held <- struct{}{}
			<-gate
		}
	}
	st.fail = func(n int) error {
		if n > before {
			return errRefusedWrite
		}
		return nil
	}
	var wg sync.WaitGroup
	var served atomic.Int64
	for i := 0; i < firstFactorLimit; i++ {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			if c, err := a.Password(context.Background(), addr, "x@example.test", "right"); err == nil && c.reveal() != "" {
				served.Add(1)
			}
		}("198.51.100." + itoa(1+i))
	}
	for i := 0; i < firstFactorLimit; i++ {
		<-held
	}
	got := passwordAside(t, a, "198.51.100.99", "x@example.test", "right")
	if !errors.Is(got.err, errFirstFactorPending) || got.c.reveal() != "" {
		t.Fatalf("after %d wrong ones, the request past a cap of writes in flight: %v (challenge minted=%v); want errFirstFactorPending and none",
			firstFactorLimit+2, got.err, got.c.reveal() != "")
	}
	release()
	wg.Wait()
	rows, spent := st.count(db.OperatorPasswordOK, id), spentIn(a.limits.firstFactor, id.String())
	w, _ := windowState(a.limits.firstFactor, id.String())
	if rows != 0 || spent != 0 || w.written != 0 || served.Load() != 0 {
		t.Fatalf("%d row(s), the counter at %d, written %d, %d served; want 0, 0, 0, 0", rows, spent, w.written, served.Load())
	}
	checkFirstFactorLog(t, logs.String(), nil, []uuid.UUID{id})
}

// TestPasswordOK_TheRefusalLineNamesEachOperatorOncePerWindow (5th round, D3, the closing
// audit's Q6): F4's line is once per window PER OPERATOR. Every write fails. Two operators
// refused in one window -- the first of them twice -- give two lines, one naming each; the
// first refused again a full period later gives a third. A flag every operator shared
// would name only the first; one carried into the next window would never name it again.
func TestPasswordOK_TheRefusalLineNamesEachOperatorOncePerWindow(t *testing.T) {
	x, y := uuid.New(), uuid.New()
	st := newTrailStore(t, map[string]uuid.UUID{"x@example.test": x, "y@example.test": y})
	st.fail = func(int) error { return errRefusedWrite }
	clock := time.Unix(1_800_000_000, 0)
	var logs strings.Builder
	a := newTrailAuth(t, st, &clock, &logs)
	n := 0
	refuse := func(email string) {
		t.Helper()
		n++
		if c, err := a.Password(context.Background(), "198.51.100."+itoa(n), email, "any"); err == nil || c.reveal() != "" {
			t.Fatalf("request %d: served while every write fails", n)
		}
	}
	refuse("x@example.test")
	refuse("y@example.test")
	refuse("x@example.test")
	if got := strings.Count(logs.String(), trailRefusedMsg); got != 2 {
		t.Fatalf("two operators refused in one window, the first twice: %d refusal line(s), want 2 -- one per operator", got)
	}
	clock = clock.Add(firstFactorPeriod)
	refuse("x@example.test")
	checkFirstFactorLog(t, logs.String(), nil, []uuid.UUID{x, y, x})
}
