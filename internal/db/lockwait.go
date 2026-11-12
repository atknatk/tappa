package db

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// BoundLockWaits gives every later lock wait of tx a lock_timeout of d, rounded up to a whole
// millisecond -- LOCAL, so it ends with the transaction and never reaches the pooled connection's
// next user (set_config's third argument, BoundStatements' rule). A wait past it is SQLSTATE 55P03
// and the transaction is aborted: nothing it wrote commits (LockWaitExpired).
//
// IT BOUNDS EACH WAIT, NOT THE STATEMENT OR THE TRANSACTION: a statement that waits for two locks may
// wait d for each, and the bound stays until it is set again. A caller with one budget therefore sets
// it again before every statement that can wait, with the time left divided by how many waits that
// statement can make (internal/adminauth's lockWaitFor; a security review measured the first version,
// set once per transaction, spending a 150 ms budget twice).
//
// d must be at least a millisecond: lock_timeout 0 is not "do not wait", it is "wait forever".
func BoundLockWaits(ctx context.Context, tx pgx.Tx, d time.Duration) error {
	if d < time.Millisecond {
		return fmt.Errorf("db: a lock wait bound must be at least 1ms, not %v", d)
	}
	ms := (d + time.Millisecond - 1) / time.Millisecond
	if _, err := tx.Exec(ctx, "SELECT set_config('lock_timeout', $1, true)", strconv.FormatInt(int64(ms), 10)); err != nil {
		return fmt.Errorf("db: bound lock waits: %w", quietErr(err))
	}
	return nil
}

// LockWaitExpired reports whether err is a lock wait that ran out of its lock_timeout (SQLSTATE
// 55P03, lock_not_available): the statement waited for a lock another session held and gave up. The
// transaction it ran in is aborted, so nothing it wrote commits. Transient counts it too (a later try
// can succeed); this names the one case where the caller already knows nothing was written.
func LockWaitExpired(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "55P03"
}

// BoundAcquireWait bounds the wait for a pool connection of what runs under the returned context to d
// -- BoundLockWaits' rule for the other wait a request cannot see the end of (internal/adminauth's mint
// steps, backlog T236 (a)) -- and nothing else: once a connection is handed over (the pool's
// PrepareConn, connectionHandedOver) the bound is disarmed, and BEGIN, every statement and the COMMIT
// run under ctx's own deadline exactly as they would without it. So a cut transaction never began:
// nothing it would have written exists, there is no COMMIT whose answer could be lost (backlog T169),
// and the caller knows it from cut without reading the error. d is at least a millisecond, as for
// BoundLockWaits, so a free connection is still taken.
//
// 🔴 WHY ONLY THE ACQUIRE AND NOT THE WHOLE TRANSACTION: a deadline over the statements cuts healthy
// work on a slow machine, not just waits -- measured on this repository's tests (load average ~258):
// one uncontended mint took longer than the mint's deadline and was refused, four existing tests went
// red. A wait for a connection is never progress; a slow statement may be.
//
// cut reports, after the work, whether the bound ran out first, and releases the context: call it
// once, after the last use of the context.
func BoundAcquireWait(ctx context.Context, d time.Duration) (context.Context, func() (cut bool)) {
	if d < time.Millisecond {
		d = time.Millisecond
	}
	ctx, cancel := context.WithCancelCause(ctx)
	g := &acquireGuard{}
	g.timer = time.AfterFunc(d, func() {
		if g.state.CompareAndSwap(acquireWaiting, acquireCut) {
			cancel(errAcquireWait)
		}
	})
	return context.WithValue(ctx, acquireGuardKey{}, g), func() bool {
		g.timer.Stop()
		cut := g.state.Load() == acquireCut
		cancel(nil)
		return cut
	}
}

var errAcquireWait = errors.New("db: waited too long for a pool connection")

type acquireGuardKey struct{}

// acquireGuard is BoundAcquireWait's state: waiting until the bound runs out or a connection is
// handed over, and only the first of the two counts.
type acquireGuard struct {
	timer *time.Timer
	state atomic.Int32
}

const (
	acquireWaiting int32 = iota
	acquireCut
	acquireDone
)

// connectionHandedOver is the pool's PrepareConn: the last step of an acquire, after the pool's own
// liveness ping. It disarms a BoundAcquireWait bound on ctx, or refuses the connection if the bound
// ran out first (the connection goes back, it is not destroyed). A context without a bound passes.
//
// THE REFUSAL IS A FIXED ERROR, NOT context.Cause(ctx): the timer marks the guard cut BEFORE it cancels
// the context, and an acquire that lands between the two would read a nil cause and pass -- a
// transaction begun under a bound that cut later reports as run out (third-eye review, measured).
// Whatever the context's state, a guard marked cut refuses.
func connectionHandedOver(ctx context.Context, _ *pgx.Conn) (bool, error) {
	g, ok := ctx.Value(acquireGuardKey{}).(*acquireGuard)
	if !ok || g.state.CompareAndSwap(acquireWaiting, acquireDone) || g.state.Load() == acquireDone {
		return true, nil
	}
	return true, errAcquireWait
}
