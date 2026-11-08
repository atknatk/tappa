package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/adminauth"
)

// The list of withdrawals owed (T212, adminresetrecovery.go) on fakes: its bound, its drain line, its
// expiry, its retry schedule and the failed request's hand-over. The DB twins are in
// resetrecovery_db_test.go.

// TestResetRecovery_AFullListRefusesWithAnError: resetRecoverySize withdrawals are owed, and the next
// lost one is refused with its ERROR line (§4.6: a trail, not a silence) naming the fate.
//
// MUTATION: the bound removed -> the next one is taken -> red.
func TestResetRecovery_AFullListRefusesWithAnError(t *testing.T) {
	t.Parallel()
	const known = "known@recovery.example"
	f := &flakyWrites{fakeResets: &fakeResets{}}
	var logged outcomeLog
	h := outcomeHandlerOver(t, f, &logged)
	h.clocks.recoveryFirst = time.Hour
	for i := 0; i < resetRecoverySize; i++ {
		if fate := h.owe(context.Background(), grantFor(known)); fate != recoveryRetrying {
			t.Fatalf("CONTROL: owed withdrawal %d of %d refused: %s", i+1, resetRecoverySize, fate)
		}
	}
	o := newResetOutcome(context.Background(), grantFor(known), ActionAdminResetUndelivered, "undelivered", resetReasonSendFailed)
	o.withdrawal.last = context.DeadlineExceeded
	h.loseWithdrawal(o, true)
	out := logged.String()
	if !strings.Contains(out, "level=ERROR") || logged.count("recovery="+recoveryListFull) != 1 {
		t.Errorf("want one ERROR naming the full list:\n%s", out)
	}
	if n := h.outbox.owedCount(); n != resetRecoverySize {
		t.Errorf("%d owed, want %d", n, resetRecoverySize)
	}
	if strings.Contains(out, known) {
		t.Error("a line carries the address")
	}
}

// TestResetRecovery_TheDrainCountsWhatIsStillOwed: at shutdown the list is dropped in ONE ERROR line
// that counts it; a second drain says nothing more, and a withdrawal lost after it is refused as
// closing.
//
// MUTATION: the drain's line removed -> red.
func TestResetRecovery_TheDrainCountsWhatIsStillOwed(t *testing.T) {
	t.Parallel()
	const known = "known@recovery.example"
	f := &flakyWrites{fakeResets: &fakeResets{}}
	var logged outcomeLog
	h := outcomeHandlerOver(t, f, &logged)
	h.clocks.recoveryFirst = time.Hour
	for i := 0; i < 3; i++ {
		h.owe(context.Background(), grantFor(known))
	}
	drain(t, h)
	drain(t, h)
	if n := logged.count(lineOwedAtDrain); n != 1 || logged.count("owed_withdrawals=3") != 1 {
		t.Errorf("%d drain line(s), want one counting 3:\n%s", n, logged.String())
	}
	if fate := h.owe(context.Background(), grantFor(known)); fate != recoveryClosing {
		t.Errorf("a withdrawal owed after the drain: %s, want %s", fate, recoveryClosing)
	}
	if withdraw, _ := f.tries(); withdraw != 0 {
		t.Errorf("%d tries at the drain, want none: its budget is for the outbox's rows", withdraw)
	}
}

// TestResetRecovery_AnExpiredLinkIsDroppedUntried: a link that expires while its withdrawal waits is
// dead already, so the list drops it with a line and no try.
//
// MUTATION: the expiry test removed -> one try -> red.
func TestResetRecovery_AnExpiredLinkIsDroppedUntried(t *testing.T) {
	t.Parallel()
	const known = "known@recovery.example"
	f := &flakyWrites{fakeResets: &fakeResets{}}
	var logged outcomeLog
	h := outcomeHandlerOver(t, f, &logged)
	h.clocks.recoveryFirst = 100 * time.Millisecond
	g := grantFor(known)
	g.Issued.Reset.ExpiresAt = time.Now().Add(50 * time.Millisecond)
	h.owe(context.Background(), g)
	waitUntil(t, 5*time.Second, "the expired entry dropped", func() bool { return logged.count(lineOwedExpired) == 1 })
	if withdraw, _ := f.tries(); withdraw != 0 {
		t.Errorf("%d tries of an expired link's withdrawal, want 0", withdraw)
	}
	if n := h.outbox.owedCount(); n != 0 {
		t.Errorf("%d owed, want 0", n)
	}
}

// TestResetRecovery_TheWorkerTriesUntilTheDatabaseAnswers is the schedule end to end on the worker's
// outcome: two tries fail (the outcome's), three later passes fail, the fourth works -- one line per
// failing pass, one success line -- and a refusal on a later try ends it without another.
//
// MUTATION: a transiently failed entry not put back on the list -> it is gone after the first later
// try -> red.
func TestResetRecovery_TheWorkerTriesUntilTheDatabaseAnswers(t *testing.T) {
	t.Parallel()
	stall := context.DeadlineExceeded
	const known = "known@recovery.example"
	for _, tc := range []struct {
		name                   string
		errs                   []error
		wantTries              int
		wantAgain, wantSuccess int
		wantNotRetried         int
	}{
		{"the database comes back", []error{stall, stall, stall, stall, stall}, 6, 3, 1, 0},
		{"a later try is refused", []error{stall, stall, &pgconn.PgError{Code: "42501"}}, 3, 0, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := &flakyWrites{fakeResets: &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{known: grantsFor(known, 1)}},
				withdrawErrs: tc.errs}
			var logged outcomeLog
			h, err := NewAdminReset(f, &recordingChannel{err: errSendFailed}, f, adminTestConfig(), slogOver(&logged))
			if err != nil {
				t.Fatal(err)
			}
			stopWorkerAtCleanup(t, h)
			router := mountReset(h)
			h.clocks.retryPause = time.Millisecond
			h.clocks.recoveryFirst = 20 * time.Millisecond
			h.clocks.recoveryMaxPause = 40 * time.Millisecond
			requestReset(t, newBrowser(t, router), known)
			waitUntil(t, 10*time.Second, "the list to finish", func() bool {
				withdraw, _ := f.tries()
				return withdraw == tc.wantTries && h.outbox.owedCount() == 0
			})
			out := logged.String()
			if n := strings.Count(out, lineOwedAgain); n != tc.wantAgain {
				t.Errorf("%d failing-pass line(s), want %d:\n%s", n, tc.wantAgain, out)
			}
			if n := strings.Count(out, lineOwedRecovered); n != tc.wantSuccess {
				t.Errorf("%d success line(s), want %d:\n%s", n, tc.wantSuccess, out)
			}
			if n := strings.Count(out, "recovery="+recoveryNotRetried); n != tc.wantNotRetried {
				t.Errorf("%d refused line(s), want %d:\n%s", n, tc.wantNotRetried, out)
			}
			time.Sleep(100 * time.Millisecond) // past two more passes: nothing tries again
			if withdraw, _ := f.tries(); withdraw != tc.wantTries {
				t.Errorf("%d tries after the end, want %d", withdraw, tc.wantTries)
			}
		})
	}
}

// TestResetRecovery_ThePauseDoublesToItsCap: a failing pass pushes the whole list back by the pause,
// which doubles from recoveryFirst to recoveryMaxPause and starts again after a try that worked.
//
// MUTATION: no cap -> the fourth pause is 240 s -> red.
func TestResetRecovery_ThePauseDoublesToItsCap(t *testing.T) {
	t.Parallel()
	q := newResetOutbox(1)
	now := time.Now()
	a, b := &owedWithdrawal{expires: now.Add(time.Hour)}, &owedWithdrawal{expires: now.Add(time.Hour)}
	q.owed = []*owedWithdrawal{b}
	for i, want := range []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 2 * time.Minute} {
		if !q.deferOwed(a, now, resetRecoveryFirst, resetRecoveryMaxPause) {
			t.Fatal("CONTROL: the open list refused")
		}
		if got := a.notBefore.Sub(now); got != want {
			t.Errorf("pass %d: pushed back %v, want %v", i+1, got, want)
		}
		if b.notBefore.Before(a.notBefore) {
			t.Errorf("pass %d: the other entry was not pushed back with it", i+1)
		}
		q.owed = []*owedWithdrawal{b}
	}
	q.owedRecovered(resetRecoveryFirst)
	c := &owedWithdrawal{expires: now.Add(time.Hour)}
	q.deferOwed(c, now, resetRecoveryFirst, resetRecoveryMaxPause)
	if got := c.notBefore.Sub(now); got != resetRecoveryFirst {
		t.Errorf("after a try that worked the pause is %v, want %v", got, resetRecoveryFirst)
	}
}

// TestResetRecovery_AFailedRequestsWithdrawalIsOwed is T210's hand-over: the mint failed after it
// minted, adminauth could not withdraw the link and says so in its error (adminauth.OwedWithdrawal).
// The request answers its 500 without waiting, the worker withdraws the link later, and a permanent
// failure is not tried at all.
//
// MUTATION: the hand-over removed from Request -> no later try -> red.
func TestResetRecovery_AFailedRequestsWithdrawalIsOwed(t *testing.T) {
	t.Parallel()
	const known = "person@recovery.example"
	for _, tc := range []struct {
		name      string
		werr      error
		wantTries int
		wantFate  string
	}{
		{"a stalled withdrawal", context.DeadlineExceeded, 1, recoveryRetrying},
		{"a refused withdrawal", &pgconn.PgError{Code: "42501"}, 0, recoveryNotRetried},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := identityGrantFor(known)
			g.Recipient = ""
			f := &fakeResets{issueErr: errors.Join(errors.New("injected"), &adminauth.OwedWithdrawal{Grant: g, Err: tc.werr})}
			var logged outcomeLog
			cfg := adminTestConfig()
			cfg.AdminIdentity = true
			h, err := NewAdminReset(f, &recordingChannel{}, &fakeTrail{}, cfg, slogOver(&logged))
			if err != nil {
				t.Fatal(err)
			}
			stopWorkerAtCleanup(t, h)
			router := mountReset(h)
			h.clocks.recoveryFirst = 20 * time.Millisecond
			requestReset(t, newBrowser(t, router), known)
			if tc.wantTries > 0 {
				waitUntil(t, 5*time.Second, "the later withdrawal", func() bool { return strings.Count(logged.String(), lineOwedRecovered) == 1 })
			} else {
				time.Sleep(100 * time.Millisecond)
			}
			f.mu.Lock()
			tries := f.identityWithdrawn
			f.mu.Unlock()
			if tries != tc.wantTries {
				t.Errorf("%d tries, want %d", tries, tc.wantTries)
			}
			if n := strings.Count(logged.String(), "recovery="+tc.wantFate); n != 1 {
				t.Errorf("want one line with recovery=%s:\n%s", tc.wantFate, logged.String())
			}
			if strings.Contains(logged.String(), known) {
				t.Error("a line carries the address")
			}
		})
	}
}

// TestResetRecovery_AnOwedWithdrawalCarriesNoAddress: on the worker's path the outcome still holds
// the grant as the job carried it, address included; the list waits in memory for minutes, has no use
// for the address, and does not hold it (TestResetOutcome_AQueuedOutcomeCarriesNoAddress's rule, one
// step further).
//
// MUTATION: owe keeping the Recipient -> red.
func TestResetRecovery_AnOwedWithdrawalCarriesNoAddress(t *testing.T) {
	t.Parallel()
	const known = "known@recovery.example"
	stall := context.DeadlineExceeded
	f := &flakyWrites{fakeResets: &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{known: grantsFor(known, 1)}},
		withdrawErrs: []error{stall, stall}}
	var logged outcomeLog
	h, err := NewAdminReset(f, &recordingChannel{err: errSendFailed}, f, adminTestConfig(), slogOver(&logged))
	if err != nil {
		t.Fatal(err)
	}
	stopWorkerAtCleanup(t, h)
	h.clocks.retryPause = time.Millisecond
	h.clocks.recoveryFirst = time.Hour
	requestReset(t, newBrowser(t, mountReset(h)), known)
	waitUntil(t, 5*time.Second, "the withdrawal owed", func() bool { return h.outbox.owedCount() == 1 })
	q := h.outbox
	q.mu.Lock()
	w := q.owed[0]
	recipient, printed := w.g.Recipient, fmt.Sprintf("%+v", w.g)
	q.mu.Unlock()
	if recipient != "" || strings.Contains(printed, known) {
		t.Errorf("the owed withdrawal holds the address %q", recipient)
	}
}
