package adminauth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
)

// workgate_test.go -- backlog T86: the process-wide ceiling on password work.

func quietGate(t *testing.T, slots int, wait time.Duration) *WorkGate {
	t.Helper()
	g, err := NewWorkGate(slots, wait, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// fullGate is a one-slot gate whose slot the test holds: every enter waits wait and is refused.
func fullGate(t *testing.T, wait time.Duration) *WorkGate {
	t.Helper()
	g := quietGate(t, 1, wait)
	release, err := g.enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	return g
}

// TestWorkGate_AtMostSlotsRunAtOnce: N operations racing for K slots never run more than K at a time,
// and every one whose wait fits is served.
func TestWorkGate_AtMostSlotsRunAtOnce(t *testing.T) {
	t.Parallel()
	const slots, n = 2, 2 + 2*workQueuePerSlot // every racer fits the slots or the queue
	g := quietGate(t, slots, 10*time.Second)
	var running, peak, served atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			release, err := g.enter(context.Background())
			if err != nil {
				return
			}
			if now := running.Add(1); now > peak.Load() {
				peak.Store(now)
			}
			time.Sleep(3 * time.Millisecond)
			running.Add(-1)
			served.Add(1)
			release()
		}()
	}
	close(start)
	wg.Wait()
	if peak.Load() > slots || served.Load() != int64(n) {
		t.Fatalf("%d ran at once (slots %d); %d of %d served", peak.Load(), slots, served.Load(), n)
	}
}

// TestWorkGate_RefusesInTwoTimeClasses: with the slot held, a waiter is refused after the wait; with
// the queue full too, the next is refused at once. Both are ErrBusy and neither depends on the caller.
func TestWorkGate_RefusesInTwoTimeClasses(t *testing.T) {
	t.Parallel()
	const wait = 150 * time.Millisecond
	g := fullGate(t, wait)
	var wg sync.WaitGroup
	waited := make([]error, workQueuePerSlot)
	for i := range waited {
		wg.Add(1)
		go func(i int) { defer wg.Done(); _, waited[i] = g.enter(context.Background()) }(i)
	}
	// Let the waiters take the queue.
	deadline := time.Now().Add(2 * time.Second)
	for len(g.queue) < cap(g.queue) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	start := time.Now()
	_, err := g.enter(context.Background())
	if at := time.Since(start); !errors.Is(err, ErrBusy) || at > wait/2 {
		t.Fatalf("queue full: %v after %v, want ErrBusy at once", err, at)
	}
	wg.Wait()
	for i, err := range waited {
		if !errors.Is(err, ErrBusy) {
			t.Fatalf("waiter %d: %v, want ErrBusy after the wait", i, err)
		}
	}
	start = time.Now()
	_, err = g.enter(context.Background())
	if at := time.Since(start); !errors.Is(err, ErrBusy) || at < wait {
		t.Fatalf("slot held: %v after %v, want ErrBusy after %v", err, at, wait)
	}
}

// countingResolver is fakeResolver counting the address lookups.
type countingResolver struct {
	fakeResolver
	lookups atomic.Int64
}

func (c *countingResolver) GetAdminByEmail(ctx context.Context, email string) ([]db.ResolvedAdmin, error) {
	c.lookups.Add(1)
	return c.fakeResolver.GetAdminByEmail(ctx, email)
}

func (c *countingResolver) ResolveIdentityByEmail(context.Context, string) (db.ResolvedIdentity, error) {
	c.lookups.Add(1)
	return db.ResolvedIdentity{}, errNoIdentityData
}

// TestAuthenticate_AFullGateAnswersEveryAddressAlike: with the gate full, a registered address and an
// unregistered one get the same ErrBusy after the same wait, with no lookup and no comparison -- the
// gate is entered before anything about the address is read (ADR 0031 §5.6). Both sign-in paths.
func TestAuthenticate_AFullGateAnswersEveryAddressAlike(t *testing.T) {
	t.Parallel()
	const wait = 120 * time.Millisecond
	registered := []db.ResolvedAdmin{{ID: uuid.New(), TenantID: uuid.New(), Status: "active",
		PasswordHash: db.NewPasswordHash(cheapDigest(t, "right"))}}
	for _, path := range []string{"legacy", "identity"} {
		for _, rows := range [][]db.ResolvedAdmin{registered, nil} {
			data := &countingResolver{fakeResolver: fakeResolver{rows: rows}}
			m, err := New(data, testConfig(), WithWorkGate(fullGate(t, wait)))
			if err != nil {
				t.Fatal(err)
			}
			var compares atomic.Int64
			m.compareFn = func(string, string) bool { compares.Add(1); return false }
			start := time.Now()
			if path == "legacy" {
				_, err = m.Authenticate(context.Background(), "probe@example.test", "right")
			} else {
				_, err = m.AuthenticatePerson(context.Background(), "probe@example.test", "right", uuid.Nil, newCountingBudget(10))
			}
			took := time.Since(start)
			if !errors.Is(err, ErrBusy) || data.lookups.Load() != 0 || compares.Load() != 0 {
				t.Fatalf("%s, %d row(s): %v, %d lookup(s), %d comparison(s); want ErrBusy and none",
					path, len(rows), err, data.lookups.Load(), compares.Load())
			}
			// One time class: the wait, never a lookup's or a comparison's time on top.
			if took < wait || took > wait+100*time.Millisecond {
				t.Fatalf("%s, %d row(s): refused after %v, want the wait (%v)", path, len(rows), took, wait)
			}
		}
	}
}

// untouchable stands in for a database the gated methods must not reach before the gate: every
// method of the embedded nil interface panics.
type untouchableManagerDB struct{ Database }
type untouchableResetDB struct{ ResetDatabase }
type untouchableClaimDB struct{ SignupClaimDatabase }

// liveInviteDB answers the invitation link as usable (AcceptNew checks the link before the gate) and
// nothing else.
type liveInviteDB struct{ InviteDatabase }

func (liveInviteDB) GetAdminInviteByTokenHash(context.Context, string) (db.ResolvedAdminInvite, error) {
	return db.ResolvedAdminInvite{ID: uuid.New(), TenantID: uuid.New(), ExpiresAt: time.Now().Add(time.Hour)}, nil
}

// TestWorkGate_EveryPasswordMethodEntersTheGate: each method that runs bcrypt, given a full gate,
// answers ErrBusy having run no comparison and no digest and touched no database past what it reads
// before the gate. The list is every production caller of compare / hash / digest in this package
// (Authenticate and AuthenticatePerson have their own test above).
func TestWorkGate_EveryPasswordMethodEntersTheGate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const wait = 5 * time.Millisecond
	var work atomic.Int64
	compare := func(string, string) bool { work.Add(1); return false }
	digest := func(string) (string, error) { work.Add(1); return "", errors.New("unreached") }

	m, err := New(untouchableManagerDB{}, testConfig(), WithWorkGate(fullGate(t, wait)))
	if err != nil {
		t.Fatal(err)
	}
	m.compareFn, m.hashFn = compare, digest
	resets, err := NewResets(untouchableResetDB{}, testConfig(), WithWorkGate(fullGate(t, wait)))
	if err != nil {
		t.Fatal(err)
	}
	resets.digest = digest
	claims, err := NewSignupClaims(untouchableClaimDB{}, testConfig(), WithWorkGate(fullGate(t, wait)))
	if err != nil {
		t.Fatal(err)
	}
	claims.digest, claims.compare = digest, compare
	invites, err := NewAdminInvites(liveInviteDB{}, testConfig(), WithWorkGate(fullGate(t, wait)))
	if err != nil {
		t.Fatal(err)
	}
	invites.digest = digest
	resetTok, err := newResetToken()
	if err != nil {
		t.Fatal(err)
	}
	inviteTok, err := newInviteToken()
	if err != nil {
		t.Fatal(err)
	}
	linked := Resolved{Linked: true}

	for name, call := range map[string]func() error{
		"Manager.ChangeOwnPassword": func() error {
			_, err := m.ChangeOwnPassword(ctx, uuid.New(), uuid.New(), uuid.New(), Token{}, "current", "next one")
			return err
		},
		"Manager.Unlink": func() error {
			_, _, err := m.Unlink(ctx, linked, "pw", uuid.New(), newCountingBudget(10))
			return err
		},
		"Manager.Relink": func() error {
			_, err := m.Relink(ctx, linked, "pw", uuid.New(), newCountingBudget(10))
			return err
		},
		"Resets.Consume": func() error {
			_, _, err := resets.Consume(ctx, resetTok, "a new password")
			return err
		},
		"SignupClaims.Open": func() error {
			_, err := claims.Open(ctx, "maria@example.mt", "a password", []byte(`{}`))
			return err
		},
		"SignupClaims.ProveWithPassword": func() error {
			_, err := claims.ProveWithPassword(ctx, SignupClaim{}, "a password")
			return err
		},
		"SignupClaims.ProveWithIdentityPassword": func() error {
			_, err := claims.ProveWithIdentityPassword(ctx, SignupClaim{IdentityPassword: true}, "a password", newCountingBudget(10))
			return err
		},
		"AdminInvites.AcceptNew": func() error {
			_, err := invites.AcceptNew(ctx, inviteTok, "a password")
			return err
		},
	} {
		before := work.Load()
		if err := call(); !errors.Is(err, ErrBusy) || work.Load() != before {
			t.Errorf("%s with a full gate: %v and %d bcrypt step(s); want ErrBusy and none", name, err, work.Load()-before)
		}
	}
}

// TestWorkGate_OneSourceHoldsAtMostOnePlace (the 2026-11-07 audit's fairness rule): N simultaneous
// requests from ONE source hold at most one place -- the rest are refused at once -- while another
// source still gets the queue and then the slot, and a context with no source is not under the rule.
func TestWorkGate_OneSourceHoldsAtMostOnePlace(t *testing.T) {
	t.Parallel()
	const wait = 2 * time.Second
	g := quietGate(t, 1, wait)
	a := WithWorkSource(context.Background(), "198.51.100.7")
	b := WithWorkSource(context.Background(), "2001:db8:1:2::/64")

	held, err := g.enter(a)
	if err != nil {
		t.Fatal(err)
	}
	const n = 20
	var wg sync.WaitGroup
	var refused atomic.Int64
	start := time.Now()
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := g.enter(a); errors.Is(err, ErrBusy) {
				refused.Add(1)
			}
		}()
	}
	wg.Wait()
	if took := time.Since(start); refused.Load() != n || took > wait/2 {
		t.Fatalf("source A's %d extra requests: %d refused in %v, want all, at once", n, refused.Load(), took)
	}
	if len(g.queue) != 0 {
		t.Fatalf("source A holds %d waiting place(s) besides its slot, want 0", len(g.queue))
	}

	// Source B waits in the queue and gets the slot when A lets go.
	got := make(chan error, 1)
	go func() {
		release, err := g.enter(b)
		if err == nil {
			release()
		}
		got <- err
	}()
	deadline := time.Now().Add(wait)
	for len(g.queue) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if _, err := g.enter(b); !errors.Is(err, ErrBusy) {
		t.Fatalf("source B's second request while its first waits: %v, want ErrBusy at once", err)
	}
	held()
	if err := <-got; err != nil {
		t.Fatalf("source B behind source A: %v, want the slot", err)
	}
	// Released places are forgotten: A can come back.
	release, err := g.enter(a)
	if err != nil {
		t.Fatalf("source A after releasing: %v", err)
	}
	release()
	if len(g.held) != 0 {
		t.Fatalf("the gate still counts %d source(s) after everything was released", len(g.held))
	}
}

// TestWorkGate_TheQueueIsWhatTheWaitCanServe pins the shipped queue: WorkWait / signInHold = 2 waiters
// per slot (the 2026-11-07 audit: 8 waiters, of whom about three could be served and the rest waited the
// whole WorkWait for a 503). Raising it, or WorkWait, is a decision that comes with a new measurement.
func TestWorkGate_TheQueueIsWhatTheWaitCanServe(t *testing.T) {
	if workQueuePerSlot != 2 || WorkWait != 10*time.Second || signInHold != 3420*time.Millisecond {
		t.Fatalf("queue %d per slot, wait %v, sign-in hold %v; shipped 2, 10s, 3.42s (ADR 0039)", workQueuePerSlot, WorkWait, signInHold)
	}
	if g := quietGate(t, 1, WorkWait); cap(g.queue) != workQueuePerSlot {
		t.Fatalf("a one-slot gate waits %d, want %d", cap(g.queue), workQueuePerSlot)
	}
}
