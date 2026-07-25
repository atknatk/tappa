package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TxFunc runs inside a tenant-scoped transaction. It receives the pgx.Tx so it
// can issue queries.
//
// BINDING DECISION (M1-08): the store package now exists, so the M1-07 card's
// original shape -- func(context.Context, store.Querier) error -- is finally
// possible. We deliberately KEEP pgx.Tx. Reasons:
//  1. pgx.Tx is strictly more capable: a caller obtains the tenant-scoped store
//     with one line, store.New(tx), and still has raw access when it is needed.
//     store.Querier could not be widened back to raw the other way.
//  2. The M1-07 proof tests (leak / rollback / panic) and the M1-09 RLS
//     isolation tests require RAW SQL (CREATE TEMP TABLE, current_setting,
//     pg_backend_pid, filter-less isolation probes) that store.Querier cannot
//     express. Narrowing the callback would delete those proofs.
//  3. CLAUDE.md section 7 wants interfaces defined at the CONSUMER; store.Querier
//     is a producer-defined interface. Keeping internal/db free of a dependency
//     on it (and free of an import cycle risk) is the cleaner boundary -- the
//     handler constructs store.New(tx) where it actually runs queries.
//
// So WithTenant stays pgx.Tx; callers wrap with store.New(tx) at the call site.
type TxFunc func(ctx context.Context, tx pgx.Tx) error

// WithTenant opens a transaction, establishes the tenant context, runs fn inside
// that transaction, and commits on success. On error it rolls back; on panic it
// rolls back and re-panics. It is the single structural guarantee that every
// query runs under the right tenant (CLAUDE.md §4.5, ADR 0002 madde 2).
//
// The context is set with set_config('app.tenant_id', $1, true): the third
// argument true makes it transaction-local (the SET LOCAL equivalent that
// accepts a bound parameter), so it is cleared when the transaction ends and
// NEVER leaks back onto the pooled connection. SET LOCAL cannot bind $1, and the
// tenant id is bound as a parameter — never concatenated into SQL.
//
// RLS policies wrap current_setting('app.tenant_id', true) in NULLIF over the
// empty-string literal, so both an unset context and an empty-string context
// collapse to NULL (ADR 0002 madde 3). That is fail-closed but silent (0 rows)
// when the context is missing; WithTenant is the compensation the ADR names — it
// makes running a query without the context impossible through this API.
func (d *DB) WithTenant(ctx context.Context, tenantID uuid.UUID, fn TxFunc) (err error) {
	// Refuse the nil UUID: it is a valid uuid literal, so it would silently
	// scope every query to a non-existent "zero tenant" (0 rows via RLS)
	// instead of failing loudly. This is a programming error, caught before we
	// touch the pool.
	if tenantID == uuid.Nil {
		return fmt.Errorf("db: WithTenant: refusing to scope to the nil tenant")
	}

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: begin tx: %w", err)
	}

	// Any early return or panic past this point must roll back so the
	// connection returns to the pool clean. Rollback uses a background context
	// because ctx may already be cancelled — otherwise the rollback itself
	// would be skipped and the transaction would be released mid-flight.
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(context.Background())
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(context.Background())
		}
	}()

	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID.String()); err != nil {
		return fmt.Errorf("db: set tenant context: %w", err)
	}

	if err = fn(ctx, tx); err != nil {
		// Return fn's error verbatim so callers can errors.Is/As it; the
		// deferred rollback undoes the transaction.
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: commit tx: %w", err)
	}
	return nil
}
