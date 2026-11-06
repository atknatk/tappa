package handler

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
)

// flakyWrites is a trail and a Resets whose first answers are scripted, counting every try.
type flakyWrites struct {
	fakeTrail
	*fakeResets

	mu            sync.Mutex
	rowErrs       []error
	withdrawErrs  []error
	rowTries      int
	withdrawTries int
	ids           []uuid.UUID
}

func (f *flakyWrites) Record(ctx context.Context, e audit.Event) (uuid.UUID, error) {
	f.mu.Lock()
	f.rowTries++
	f.ids = append(f.ids, e.ID)
	var err error
	if len(f.rowErrs) > 0 {
		err, f.rowErrs = f.rowErrs[0], f.rowErrs[1:]
	}
	f.mu.Unlock()
	if err != nil {
		return uuid.Nil, err
	}
	return f.fakeTrail.Record(ctx, e)
}

func (f *flakyWrites) Withdraw(context.Context, uuid.UUID, uuid.UUID) error { return f.nextWithdraw() }

func (f *flakyWrites) WithdrawIdentity(context.Context, adminauth.ResetToken) error {
	return f.nextWithdraw()
}

func (f *flakyWrites) nextWithdraw() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.withdrawTries++
	if len(f.withdrawErrs) > 0 {
		var err error
		err, f.withdrawErrs = f.withdrawErrs[0], f.withdrawErrs[1:]
		return err
	}
	return nil
}

func (f *flakyWrites) tries() (withdraw, row int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.withdrawTries, f.rowTries
}

// TestResetOutcome_OnlyATransientFailureIsTriedOnceMore is the whole retry table, on both paths: what a
// second try may help with gets exactly one, what will answer the same again gets none, and a try
// succeeding or failing the second time ends it.
//
// MUTATIONS: no second try -> the transient cases red. Every error retried (db.Transient always true)
// -> the permanent cases red. A third try -> the "stays down" cases red.
func TestResetOutcome_OnlyATransientFailureIsTriedOnceMore(t *testing.T) {
	t.Parallel()
	stall := context.DeadlineExceeded
	permanent := &pgconn.PgError{Code: "23514"}
	tests := []struct {
		name                  string
		withdrawErrs, rowErrs []error
		wantWithdraw, wantRow int
		wantRows              int
		wantLostWithdrawal    int
		wantLostRow           int
	}{
		{name: "nothing fails", wantWithdraw: 1, wantRow: 1, wantRows: 1},
		{name: "a stalled withdrawal", withdrawErrs: []error{stall}, wantWithdraw: 2, wantRow: 1, wantRows: 1},
		{name: "a stalled row", rowErrs: []error{stall}, wantWithdraw: 1, wantRow: 2, wantRows: 1},
		{name: "both stalled once", withdrawErrs: []error{stall}, rowErrs: []error{stall}, wantWithdraw: 2, wantRow: 2, wantRows: 1},
		{name: "a withdrawal that stays down", withdrawErrs: []error{stall, stall, stall}, wantWithdraw: 2, wantRow: 1, wantRows: 1, wantLostWithdrawal: 1},
		{name: "a row that stays down", rowErrs: []error{stall, stall, stall}, wantWithdraw: 1, wantRow: 2, wantLostRow: 1},
		{name: "a refused withdrawal", withdrawErrs: []error{permanent}, wantWithdraw: 1, wantRow: 1, wantRows: 1, wantLostWithdrawal: 1},
		{name: "a refused row", rowErrs: []error{permanent}, wantWithdraw: 1, wantRow: 1, wantLostRow: 1},
		{name: "a cancelled row: the drain's budget", rowErrs: []error{context.Canceled}, wantWithdraw: 1, wantRow: 1, wantLostRow: 1},
	}
	for _, path := range []struct {
		name string
		ch   ResetChannel
	}{
		{"the request's fallback", refusingChannel{&recordingChannel{}}},
		{"the worker's outcome", &recordingChannel{err: errSendFailed}},
	} {
		for _, tc := range tests {
			t.Run(path.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				const known = "known@outcome.example"
				f := &flakyWrites{fakeResets: &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{known: grantsFor(known, 1)}},
					withdrawErrs: tc.withdrawErrs, rowErrs: tc.rowErrs}
				var logged bytes.Buffer
				router, h := newOutboxFlow(t, f, path.ch, f, &logged)
				h.clocks.retryPause = time.Millisecond
				requestReset(t, newBrowser(t, router), known)
				drain(t, h)
				withdraw, row := f.tries()
				if withdraw != tc.wantWithdraw || row != tc.wantRow {
					t.Errorf("tries: %d withdrawal(s), %d row(s); want %d, %d", withdraw, row, tc.wantWithdraw, tc.wantRow)
				}
				if n := f.count(ActionAdminResetUndelivered); n != tc.wantRows {
					t.Errorf("%d row(s), want %d", n, tc.wantRows)
				}
				if n := strings.Count(logged.String(), lineWithdrawalLost); n != tc.wantLostWithdrawal {
					t.Errorf("%d lost-withdrawal line(s), want %d:\n%s", n, tc.wantLostWithdrawal, logged.String())
				}
				if n := strings.Count(logged.String(), lineRowLost); n != tc.wantLostRow {
					t.Errorf("%d lost-row line(s), want %d:\n%s", n, tc.wantLostRow, logged.String())
				}
				if ids := f.ids; len(ids) > 0 && (ids[0] == uuid.Nil || ids[len(ids)-1] != ids[0]) {
					t.Errorf("row ids %v: the tries of one row must carry one id, or a lost answer doubles it", ids)
				}
			})
		}
	}
}

// TestResetOutcome_TheIdentityLinkRetriesItsOneWrite: same table, the identity's one write, and never a
// row.
func TestResetOutcome_TheIdentityLinkRetriesItsOneWrite(t *testing.T) {
	t.Parallel()
	const known = "person@outcome.example"
	f := &flakyWrites{fakeResets: &fakeResets{personFor: map[string]adminauth.PersonResets{known: {Grants: []adminauth.ResetGrant{identityGrantFor(known)}}}},
		withdrawErrs: []error{context.DeadlineExceeded}}
	var logged bytes.Buffer
	router, h := newResetRouterFlag(t, f, refusingChannel{&recordingChannel{}}, &f.fakeTrail, true, &logged)
	h.clocks.retryPause = time.Millisecond
	requestReset(t, newBrowser(t, router), known)
	drain(t, h)
	if withdraw, _ := f.tries(); withdraw != 2 {
		t.Errorf("%d tries of the withdrawal, want 2", withdraw)
	}
	if n := len(f.fakeTrail.eventsSnapshot()); n != 0 {
		t.Errorf("%d row(s) for a per-identity link, want 0", n)
	}
	if n := strings.Count(logged.String(), "panel recovery: identity link outcome"); n != 1 {
		t.Errorf("%d outcome line(s), want 1:\n%s", n, logged.String())
	}
}

// TestResetOutcome_ARetryIsChargedOnce: the budget counts outcomes, not tries. Every grant's row fails
// once, and all six are written with nothing left over for a rate_limited row; the budget is 10.
//
// MUTATION: the budget asked on every try -> 12 charges -> a rate_limited row -> red.
func TestResetOutcome_ARetryIsChargedOnce(t *testing.T) {
	t.Parallel()
	const known = "known@outcome.example"
	const n = 6
	grants := grantsFor(known, n)
	admin := uuid.New()
	for i := range grants {
		grants[i].Issued.Reset.AdminUserID = admin
	}
	f := &flakyWrites{fakeResets: &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{known: grants}}}
	router, h := newOutboxFlow(t, f, refusingChannel{&recordingChannel{}}, &everyOtherFails{flakyWrites: f}, &bytes.Buffer{})
	h.clocks.retryPause = time.Millisecond
	requestReset(t, newBrowser(t, router), known)
	drain(t, h)
	if got := f.count(ActionAdminResetUndelivered); got != n {
		t.Errorf("%d row(s), want %d", got, n)
	}
	if got := f.count(ActionAdminResetLimited); got != 0 {
		t.Errorf("%d rate_limited row(s), want 0: a retry spent the budget", got)
	}
}

// everyOtherFails fails a row's first try by its id and passes its second.
type everyOtherFails struct {
	*flakyWrites
	mu   sync.Mutex
	seen map[uuid.UUID]bool
}

func (e *everyOtherFails) Record(ctx context.Context, ev audit.Event) (uuid.UUID, error) {
	e.mu.Lock()
	if e.seen == nil {
		e.seen = map[uuid.UUID]bool{}
	}
	first := !e.seen[ev.ID]
	e.seen[ev.ID] = true
	e.mu.Unlock()
	if first {
		return uuid.Nil, context.DeadlineExceeded
	}
	return e.flakyWrites.Record(ctx, ev)
}

// TestResetOutcome_ARefusedRetryIsLoggedAsLost: when the worker will not take the outcome -- the outbox
// closing, or the retry queue full while it holds a send -- the request does not wait and does not go
// quiet. Each owed write gets its ERROR line.
//
// MUTATION: a refused retry dropped silently -> no line -> red.
func TestResetOutcome_ARefusedRetryIsLoggedAsLost(t *testing.T) {
	t.Parallel()
	const known = "known@outcome.example"
	t.Run("the outbox is closed", func(t *testing.T) {
		t.Parallel()
		f := &flakyWrites{fakeResets: &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{known: grantsFor(known, 1)}},
			withdrawErrs: []error{context.DeadlineExceeded}, rowErrs: []error{context.DeadlineExceeded}}
		var logged outcomeLog
		h := outcomeHandlerOver(t, f, &logged)
		drain(t, h)
		g := f.fakeResets.grantsFor[known][0]
		h.recordFallback(context.Background(), time.Now().Add(time.Second), g, resetReasonOutboxShut)
		if withdraw, row := f.tries(); withdraw != 1 || row != 1 {
			t.Errorf("tries: %d, %d; want one of each", withdraw, row)
		}
		if logged.count(lineWithdrawalLost) != 1 || logged.count(lineRowLost) != 1 {
			t.Errorf("want one lost line per write:\n%s", logged.String())
		}
	})
	t.Run("the retry queue is full", func(t *testing.T) {
		t.Parallel()
		f := &flakyWrites{fakeResets: &fakeResets{}, withdrawErrs: []error{context.DeadlineExceeded}}
		var logged outcomeLog
		h := outcomeHandlerOver(t, f, &logged)
		defer h.outbox.stopSending() // the drain at cleanup need not wait for its write reserve
		if !fillRetries(h, known) {
			t.Fatal("CONTROL: the retry queue did not fill")
		}
		h.recordFallback(context.Background(), time.Now().Add(time.Second), grantFor(known), resetReasonOutboxFull)
		if logged.count(lineWithdrawalLost) != 1 {
			t.Errorf("want the lost withdrawal's line:\n%s", logged.String())
		}
	})
}

// fillRetries offers outcomes that owe nothing and wait an hour, until the queue refuses one: the
// worker sits in the first one's wait (the drain cuts it short), so the rest stay queued.
func fillRetries(h *AdminReset, known string) bool {
	for i := 0; i <= resetRetryQueueSize+1; i++ {
		o := newResetOutcome(context.Background(), grantFor(known), ActionAdminResetRequested, "ok", "")
		o.admitted, o.notBefore = true, time.Now().Add(time.Hour)
		if !h.outbox.offerRetry(o) {
			return true
		}
	}
	return false
}

func outcomeHandlerOver(t *testing.T, f *flakyWrites, logged *outcomeLog) *AdminReset {
	t.Helper()
	h, err := NewAdminReset(f, refusingChannel{&recordingChannel{}}, f, adminTestConfig(), slogOver(logged))
	if err != nil {
		t.Fatalf("NewAdminReset: %v", err)
	}
	stopWorkerAtCleanup(t, h)
	return h
}

// TestResetOutcome_TheDrainCutsThePause: a retry waiting out its pause when the sends stop does not
// spend the drain's write reserve waiting. The pause here is longer than the whole budget.
//
// MUTATION: waitUntil ignoring the drain -> the drain runs out its budget -> red.
func TestResetOutcome_TheDrainCutsThePause(t *testing.T) {
	t.Parallel()
	const known = "known@outcome.example"
	f := &flakyWrites{fakeResets: &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{known: grantsFor(known, 1)}},
		rowErrs: []error{context.DeadlineExceeded}}
	var logged bytes.Buffer
	router, h := newOutboxFlow(t, f, refusingChannel{&recordingChannel{}}, f, &logged)
	h.clocks.retryPause = time.Hour
	requestReset(t, newBrowser(t, router), known)
	ctx, cancel := context.WithTimeout(context.Background(), ResetDrainWriteReserve+100*time.Millisecond)
	defer cancel()
	if err := h.Drain(ctx); err != nil {
		t.Fatalf("the drain waited out the retry's pause: %v", err)
	}
	if n := f.count(ActionAdminResetUndelivered); n != 1 {
		t.Errorf("%d row(s), want the second try's 1", n)
	}
}

// TestResetOutcome_ASpentBudgetIsNotRetried: the drain's budget ending the row's context is an order to
// stop, not a stall: the worker writes its line and moves on without a second try.
//
// MUTATION: context.Canceled classified transient -> a second try -> red.
func TestResetOutcome_ASpentBudgetIsNotRetried(t *testing.T) {
	t.Parallel()
	const known = "known@outcome.example"
	f := &spentRowTrail{entered: make(chan struct{}, 8)}
	resets := &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{known: grantsFor(known, 1)}}
	var logged outcomeLog
	h, err := NewAdminReset(resets, &recordingChannel{}, f, adminTestConfig(), slogOver(&logged))
	if err != nil {
		t.Fatal(err)
	}
	requestReset(t, newBrowser(t, mountReset(h)), known)
	<-f.entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.Drain(ctx); err == nil {
		t.Fatal("CONTROL: the budget was not spent")
	}
	waitUntil(t, 5*time.Second, "the lost row's line", func() bool { return logged.count(lineRowLost) == 1 })
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tries != 1 {
		t.Errorf("%d tries of the row, want 1", f.tries)
	}
}

// spentRowTrail waits for its context to end and answers what Postgres answers.
type spentRowTrail struct {
	entered chan struct{}
	mu      sync.Mutex
	tries   int
}

func (s *spentRowTrail) Record(ctx context.Context, _ audit.Event) (uuid.UUID, error) {
	s.mu.Lock()
	s.tries++
	s.mu.Unlock()
	s.entered <- struct{}{}
	<-ctx.Done()
	return uuid.Nil, ctx.Err()
}

func slogOver(b *outcomeLog) *slog.Logger {
	return slog.New(slog.NewTextHandler(b, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// TestResetOutcome_TheBudgetIsTheRequestsNotTheGrants: on a fake that stalls every write until its
// context ends, a full window's answer is bounded by ONE budget, and each grant past it leaves without
// trying. Its DB twin measures the same with real locks.
//
// MUTATION: until computed per grant -> eight budgets -> red.
func TestResetOutcome_TheBudgetIsTheRequestsNotTheGrants(t *testing.T) {
	t.Parallel()
	const known = "known@outcome.example"
	grants := grantsFor(known, adminauth.ResetWindow)
	trail := &ctxTrail{hold: time.Hour}
	resets := &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{known: grants}}
	var logged bytes.Buffer
	router, h := newOutboxFlow(t, resets, refusingChannel{&recordingChannel{}}, trail, &logged)
	h.clocks.fallbackBudget = 200 * time.Millisecond
	h.clocks.fallbackStep = 100 * time.Millisecond
	h.clocks.grace = 10 * time.Millisecond
	h.clocks.retryPause = 10 * time.Millisecond
	b := newBrowser(t, router)
	start := time.Now()
	requestReset(t, b, known)
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("the answer took %v, far over one budget + the floor", took)
	}
}
