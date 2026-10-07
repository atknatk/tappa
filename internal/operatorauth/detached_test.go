package operatorauth

// detached_test.go -- OP-14 phase E, the parts that need no database: the sign-in's rows
// and statements run DETACHED from the request (detach), and a client that leaves changes
// neither the process-wide cap on password-less rows nor the bound of each statement. The
// store here answers a done context the way pgxpool's Acquire and pgconn's Exec do --
// nothing is done, the context's error is returned -- so a statement still on the
// request's context fails exactly where it would against PostgreSQL. What the database
// keeps is detached_db_test.go's; the answer on the wire is detached_external_test.go's.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/sun"
)

// memStore is the in-memory Store of these tests: active operators by address and by id,
// the rows asked for, the sessions opened, the methods named in hang, which wait for their
// context to end -- a pool that never hands out a connection -- and the methods named in
// delay, which take that long unless their context ends first -- a slow pool.
type memStore struct {
	mu       sync.Mutex
	byEmail  map[string]db.OperatorAccount
	byID     map[uuid.UUID]db.OperatorAccount
	rows     []memRow
	sessions int
	hang     map[string]bool
	delay    map[string]time.Duration
}

type memRow struct {
	kind      db.OperatorAuthEvent
	byAddress bool
	id        uuid.UUID
}

func newMemStore() *memStore {
	return &memStore{byEmail: map[string]db.OperatorAccount{}, byID: map[uuid.UUID]db.OperatorAccount{}, hang: map[string]bool{},
		delay: map[string]time.Duration{}}
}

func (s *memStore) add(email string, acc db.OperatorAccount) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byEmail[email], s.byID[acc.ID] = acc, acc
}

// enter is every method's first step: a hanging method waits for its context, a slow one
// for its delay or its context, and a done context is the method's error before anything is
// done.
func (s *memStore) enter(ctx context.Context, method string) error {
	s.mu.Lock()
	h, d := s.hang[method], s.delay[method]
	s.mu.Unlock()
	if h {
		<-ctx.Done()
	}
	if d > 0 {
		select {
		case <-time.After(d):
		case <-ctx.Done():
		}
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("db: %s: %w", method, err)
	}
	return nil
}

func (s *memStore) OperatorByEmail(ctx context.Context, email string) (db.OperatorAccount, error) {
	if err := s.enter(ctx, "OperatorByEmail"); err != nil {
		return db.OperatorAccount{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.byEmail[email]; ok {
		return a, nil
	}
	return db.OperatorAccount{}, db.ErrNoOperator
}

func (s *memStore) OperatorByID(ctx context.Context, id uuid.UUID) (db.OperatorAccount, error) {
	if err := s.enter(ctx, "OperatorByID"); err != nil {
		return db.OperatorAccount{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.byID[id]; ok {
		return a, nil
	}
	return db.OperatorAccount{}, db.ErrNoOperator
}

func (s *memStore) RecordOperatorAuthEvent(ctx context.Context, kind db.OperatorAuthEvent, email string, id uuid.UUID) error {
	if err := s.enter(ctx, "RecordOperatorAuthEvent"); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = append(s.rows, memRow{kind: kind, byAddress: email != "", id: id})
	return nil
}

func (s *memStore) OpenOperatorSession(ctx context.Context, _ uuid.UUID, _ string, _ int64) error {
	if err := s.enter(ctx, "OpenOperatorSession"); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions++
	return nil
}

func (s *memStore) CompleteOperatorEnrollment(ctx context.Context, _ uuid.UUID, _, _ string, _ []byte, _ int64, _ string) error {
	if err := s.enter(ctx, "CompleteOperatorEnrollment"); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions++
	return nil
}

func (s *memStore) TouchOperatorSession(context.Context, string) (db.OperatorSession, error) {
	return db.OperatorSession{}, errors.New("memStore: no session check in these tests")
}

func (s *memStore) CloseOperatorSession(context.Context, string) error {
	return errors.New("memStore: no sign-out in these tests")
}

func (s *memStore) rowCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rows)
}

// capLineRE is recordPasswordless's one WARN line in the text handler: the message, the
// kind, the cap and the window -- nothing the request carried.
var capLineRE = regexp.MustCompile(`^time=\S+ level=WARN msg="operator pre-session audit rows suppressed for the rest of the window" kind=(login_failed|unknown_email) cap=30 window=10m0s$`)

// TestDetachedSignIn_ThePasswordlessCapHoldsWhenClientsLeave is the OP-14 E card's md. 3:
// the process-wide cap on password-less rows behaves as it did, now that its rows are
// written detached. Every request's client hangs up WHILE its comparison runs (the request's
// context is cancelled inside the comparer -- what net/http does to a half-closed
// connection). auditCapLimit wrong passwords and unknown addresses from as many client
// addresses each leave their row and log nothing; the two after them are answered alike
// (ErrRefused), write nothing, and log ONE line in the closed shape; a full period later
// the window has restarted and a row is written again, with no second line. No line carries
// a client address or an operator's address.
func TestDetachedSignIn_ThePasswordlessCapHoldsWhenClientsLeave(t *testing.T) {
	st := newMemStore()
	known := uuid.New()
	st.add("known@example.test", db.OperatorAccount{ID: known, Digest: db.NewPasswordHash("not compared: compareFn is replaced")})
	clock := time.Unix(1_800_000_000, 0)
	var logs strings.Builder
	a := newTrailAuth(t, st, &clock, &logs)
	var hangUp context.CancelFunc
	a.compareFn = func([]byte, []byte) error {
		hangUp()
		return errors.New("operatorauth test: not the right one")
	}
	send := func(i int) {
		t.Helper()
		rctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		hangUp = cancel
		email := "known@example.test"
		if i%2 == 1 {
			email = "nobody-" + itoa(i) + "@example.test"
		}
		c, err := a.Password(rctx, "203.0.113."+itoa(i), email, "wrong")
		if rctx.Err() == nil {
			t.Fatalf("PREMISE: request %d's context was not cancelled during its comparison", i)
		}
		if !errors.Is(err, ErrRefused) || c.reveal() != "" {
			t.Fatalf("request %d, its client gone: %v (challenge minted=%v); want ErrRefused", i, err, c.reveal() != "")
		}
	}
	for i := 1; i <= auditCapLimit; i++ {
		send(i)
		if got := st.rowCount(); got != i || logs.Len() != 0 {
			t.Fatalf("aborted wrong password %d, within the cap: %d row(s), %d log byte(s); want %d and none", i, got, logs.Len(), i)
		}
	}
	send(auditCapLimit + 1)
	send(auditCapLimit + 2)
	if got := st.rowCount(); got != auditCapLimit {
		t.Fatalf("past the cap: %d row(s), want %d -- the cap stopped nothing", got, auditCapLimit)
	}
	clock = clock.Add(auditCapPeriod)
	send(auditCapLimit + 3)
	if got := st.rowCount(); got != auditCapLimit+1 {
		t.Fatalf("a period later: %d row(s), want %d (the window restarted)", got, auditCapLimit+1)
	}
	lines := 0
	for _, l := range strings.Split(logs.String(), "\n") {
		if l == "" {
			continue
		}
		lines++
		if !capLineRE.MatchString(l) {
			t.Errorf("a log line that is not the cap's line (%d characters)", len(l))
		}
	}
	if lines != 1 {
		t.Fatalf("%d log line(s), want ONE: the window's first suppressed row", lines)
	}
	if strings.Contains(logs.String(), "@example.test") || strings.Contains(logs.String(), "203.0.113.") {
		t.Fatal("the log carries an address")
	}
	for _, r := range st.rows {
		if r.kind != db.OperatorLoginFailed && r.kind != db.OperatorUnknownEmail {
			t.Fatalf("a %s row from a password-less arm", r.kind)
		}
	}
}

// TestDetachedSignIn_NoComparisonWithoutItsRow pins the password step's split (Password's
// comment): only the lookup follows the client. A client gone BEFORE the step is refused at
// the lookup -- no comparison, no row, an error that is no sentinel (503 on the surface, for
// a right password and a wrong one alike); a client gone DURING the comparison gets its row
// and ErrRefused. In both, every comparison of a wrong password has its row.
func TestDetachedSignIn_NoComparisonWithoutItsRow(t *testing.T) {
	st := newMemStore()
	st.add("x@example.test", db.OperatorAccount{ID: uuid.New(), Digest: db.NewPasswordHash("not compared: compareFn is replaced")})
	clock := time.Unix(1_800_000_000, 0)
	var logs strings.Builder
	a := newTrailAuth(t, st, &clock, &logs)
	comparisons := 0
	var during context.CancelFunc
	a.compareFn = func([]byte, []byte) error {
		comparisons++
		if during != nil {
			during()
		}
		return errors.New("operatorauth test: not the right one")
	}
	for i, email := range []string{"x@example.test", "nobody@example.test"} {
		gone, leave := context.WithCancel(context.Background())
		leave()
		if c, err := a.Password(gone, "192.0.2."+itoa(10+i), email, "wrong"); err == nil || isSentinel(err) || c.reveal() != "" {
			t.Fatalf("%s, its client gone before the step: %v; want the lookup's error and no challenge", email, err)
		}
		if comparisons != 0 || st.rowCount() != 0 {
			t.Fatalf("%s, its client gone before the step: %d comparison(s), %d row(s); want 0 and 0", email, comparisons, st.rowCount())
		}
	}
	for i, email := range []string{"x@example.test", "nobody@example.test"} {
		rctx, cancel := context.WithCancel(context.Background())
		during = cancel
		_, err := a.Password(rctx, "192.0.2."+itoa(20+i), email, "wrong")
		during = nil
		cancel()
		if !errors.Is(err, ErrRefused) || comparisons != i+1 || st.rowCount() != i+1 {
			t.Fatalf("%s, its client gone during the comparison: %v, %d comparison(s), %d row(s); want ErrRefused and one row per comparison",
				email, err, comparisons, st.rowCount())
		}
	}
	if logs.Len() != 0 {
		t.Fatalf("%d log byte(s), want none", logs.Len())
	}
}

// TestDetachedSignIn_AHangingStatementIsBoundedByItsGrace (the OP-14 E card's "asılı yazım
// süreyle sınırlı"): each statement the sign-in runs detached, made to hang -- a pool that
// never hands out a connection -- ends the step after about SignInStatementGrace, with an
// error that is none of the sentinels a caller answers with a page (so the surface answers
// 503), no challenge and no session. The request's own context has an hour: the detach drops
// its deadline with its cancellation, so the grace is the only bound. The five arms run at
// once, so the test takes one grace, not five; it runs in parallel with
// TestDetachedSignIn_ASlowLookupLeavesTheRowItsOwnBound, the package's other wait.
func TestDetachedSignIn_AHangingStatementIsBoundedByItsGrace(t *testing.T) {
	t.Parallel()
	type result struct {
		name   string
		served bool
		err    error
		took   time.Duration
	}
	clock := time.Unix(1_800_000_000, 0)
	key := []byte("op14e hang TOTP key!")
	arm := func(name, hang string, run func(*testing.T, *Authenticator, *memStore, uuid.UUID) (bool, error)) func(chan<- result) {
		st := newMemStore()
		var logs strings.Builder
		a := newTrailAuth(t, st, &clock, &logs)
		a.compareFn = func([]byte, []byte) error { return errors.New("operatorauth test: not the right one") }
		a.digestFn = func(string) (string, error) { return "not stored: the statement hangs", nil }
		id := uuid.New()
		sealed, err := sun.Seal(a.keys.kek.bytes(), id[:], key)
		if err != nil {
			t.Fatal(err)
		}
		st.add("x@example.test", db.OperatorAccount{ID: id, Digest: db.NewPasswordHash("not compared"), Sealed: db.NewSealedSecret(sealed)})
		st.hang[hang] = true
		return func(out chan<- result) {
			start := time.Now()
			served, err := run(t, a, st, id)
			out <- result{name, served, err, time.Since(start)}
		}
	}
	rctx := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), time.Hour)
	}
	arms := []func(chan<- result){
		arm("a password-less row", "RecordOperatorAuthEvent", func(t *testing.T, a *Authenticator, _ *memStore, _ uuid.UUID) (bool, error) {
			ctx, cancel := rctx()
			defer cancel()
			c, err := a.Password(ctx, "192.0.2.80", "x@example.test", "wrong")
			return c.reveal() != "", err
		}),
		arm("the code step's lookup", "OperatorByID", func(t *testing.T, a *Authenticator, _ *memStore, id uuid.UUID) (bool, error) {
			ctx, cancel := rctx()
			defer cancel()
			iss, err := a.TOTP(ctx, challengeFor(t, a, fixtureAccount{id: id}), codeAt(key, clock))
			return iss.Token.reveal() != "", err
		}),
		arm("the failure row", "RecordOperatorAuthEvent", func(t *testing.T, a *Authenticator, _ *memStore, id uuid.UUID) (bool, error) {
			ctx, cancel := rctx()
			defer cancel()
			iss, err := a.TOTP(ctx, challengeFor(t, a, fixtureAccount{id: id}), wrongCodeAt(key, clock))
			return iss.Token.reveal() != "", err
		}),
		arm("op_open_session", "OpenOperatorSession", func(t *testing.T, a *Authenticator, _ *memStore, id uuid.UUID) (bool, error) {
			ctx, cancel := rctx()
			defer cancel()
			iss, err := a.TOTP(ctx, challengeFor(t, a, fixtureAccount{id: id}), codeAt(key, clock))
			return iss.Token.reveal() != "", err
		}),
		arm("op_complete_enrollment", "CompleteOperatorEnrollment", func(t *testing.T, a *Authenticator, _ *memStore, _ uuid.UUID) (bool, error) {
			ctx, cancel := rctx()
			defer cancel()
			pending := uuid.New()
			pend, err := a.BeginEnrollment(pending)
			if err != nil {
				return false, fmt.Errorf("PREMISE: begin: %w", err)
			}
			iss, err := a.CompleteEnrollment(ctx, "192.0.2.81", pending, strings.Repeat("T", 43), pend.Blob,
				strings.Repeat("op14e hanging ", 2), codeAt(pend.Secret.reveal(), clock))
			return iss.Token.reveal() != "", err
		}),
	}
	out := make(chan result, len(arms))
	for _, run := range arms {
		go run(out)
	}
	deadline := time.After(SignInStatementGrace + 10*time.Second)
	for range arms {
		select {
		case r := <-out:
			sentinel := false
			for _, s := range []error{ErrRefused, ErrThrottled, ErrChallenge, ErrCodeRejected, ErrLocked, ErrNoSession, ErrEnrollment, ErrWeakPassword} {
				sentinel = sentinel || errors.Is(r.err, s)
			}
			if r.served || r.err == nil || sentinel || r.took < SignInStatementGrace-time.Second || r.took > SignInStatementGrace+3*time.Second {
				t.Errorf("%s hanging: served=%v err=%v after %v; want an error that is no sentinel after about %v",
					r.name, r.served, r.err, r.took.Round(10*time.Millisecond), SignInStatementGrace)
			}
		case <-deadline:
			t.Fatalf("UNBOUNDED: a hanging statement is still running %v after the steps began (SignInStatementGrace %v)",
				SignInStatementGrace+10*time.Second, SignInStatementGrace)
		}
	}
}

// TestDetachedSignIn_ASlowLookupLeavesTheRowItsOwnBound (2nd round, the third eye's F3) pins
// SignInStatementGrace's "one bound per statement, not per step": the code step's lookup
// takes 4 s and the totp_failed row's write 2 s -- together longer than one grace, each
// shorter. The wrong code is still answered ErrCodeRejected and its row is written, after
// about 6 s. Under one bound for the whole step the row would have 1 s left, its write
// would end at the step's deadline, and a checked guess would go uncounted (the step's
// error instead of the refusal). It runs in parallel with the hanging-statement test.
func TestDetachedSignIn_ASlowLookupLeavesTheRowItsOwnBound(t *testing.T) {
	t.Parallel()
	const lookup, write = 4 * time.Second, 2 * time.Second
	if lookup >= SignInStatementGrace || write >= SignInStatementGrace || lookup+write <= SignInStatementGrace {
		t.Fatalf("PREMISE: %v and %v must each fit one grace (%v) and together outlast it", lookup, write, SignInStatementGrace)
	}
	clock := time.Unix(1_800_000_000, 0)
	key := []byte("op14e slow TOTP key!")
	st := newMemStore()
	var logs strings.Builder
	a := newTrailAuth(t, st, &clock, &logs)
	id := uuid.New()
	sealed, err := sun.Seal(a.keys.kek.bytes(), id[:], key)
	if err != nil {
		t.Fatal(err)
	}
	st.add("x@example.test", db.OperatorAccount{ID: id, Digest: db.NewPasswordHash("not compared"), Sealed: db.NewSealedSecret(sealed)})
	st.delay["OperatorByID"], st.delay["RecordOperatorAuthEvent"] = lookup, write
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	start := time.Now()
	_, err = a.TOTP(ctx, challengeFor(t, a, fixtureAccount{id: id}), wrongCodeAt(key, clock))
	took := time.Since(start)
	if !errors.Is(err, ErrCodeRejected) || st.rowCount() != 1 || st.rows[0].kind != db.OperatorTOTPFailed {
		t.Fatalf("a %v lookup and a %v write: %v after %v, %d row(s); want ErrCodeRejected and the totp_failed row -- each statement its own %v",
			lookup, write, err, took.Round(10*time.Millisecond), st.rowCount(), SignInStatementGrace)
	}
	if took < lookup+write-500*time.Millisecond {
		t.Fatalf("PREMISE: the step took %v, less than the two delays (%v)", took.Round(10*time.Millisecond), lookup+write)
	}
}
