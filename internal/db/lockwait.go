package db

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// BoundLockWaits gives every later lock wait of tx a lock_timeout of d, rounded up to a whole
// millisecond -- LOCAL, so it ends with the transaction and never reaches the pooled connection's
// next user (set_config's third argument, BoundStatements' rule). A wait past it is SQLSTATE 55P03
// and the transaction is aborted: nothing it wrote commits (LockWaitExpired).
//
// IT BOUNDS EACH WAIT, NOT THE TRANSACTION: a statement that waits for two locks may wait d for each.
// A caller that needs one budget across several transactions passes the time left of it here.
//
// d must be at least a millisecond: lock_timeout 0 is not "do not wait", it is "wait forever".
func BoundLockWaits(ctx context.Context, tx pgx.Tx, d time.Duration) error {
	if d < time.Millisecond {
		return fmt.Errorf("db: a lock wait bound must be at least 1ms, not %v", d)
	}
	ms := (d + time.Millisecond - 1) / time.Millisecond
	if _, err := tx.Exec(ctx, "SELECT set_config('lock_timeout', $1, true)", strconv.FormatInt(int64(ms), 10)); err != nil {
		return fmt.Errorf("db: bound lock waits: %w", err)
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
