package db

import (
	"context"
	"fmt"
	"strconv"
	"time"

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

// WithTenantIn runs fn in a SAVEPOINT of tx, a transaction the caller holds in the context of
// outer, under tenantID's context, and gives tx outer's context back before the savepoint is
// released (M11 API-11 fix 1). It is WithTenant for code that must not open a second transaction:
// the sandbox's simulated tap runs the tap path -- whose steps each ask for a tenant's transaction
// (the plaque's for the counter, the employee's for the record) -- inside the /v1 write runner's
// one transaction, so the claim, the tap and the ledger's result commit together and each request
// holds ONE pool connection.
//
// The context is set the way WithTenant sets it, set_config(..., true): local to the TRANSACTION,
// not to the savepoint -- so a released savepoint would leave tenantID's context on tx for the
// statements after it, and the restore to outer is what prevents that. On fn's error (or a panic)
// the savepoint is rolled back, which undoes fn's writes and the set_config with them; tx stays
// usable and in outer's context. The row locks and transaction-scoped advisory locks fn takes are
// the TRANSACTION's: they are held until tx ends, not until the savepoint does.
func WithTenantIn(ctx context.Context, tx pgx.Tx, outer, tenantID uuid.UUID, fn TxFunc) (err error) {
	if tenantID == uuid.Nil || outer == uuid.Nil {
		return fmt.Errorf("db: WithTenantIn: refusing to scope to the nil tenant")
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: savepoint: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = sp.Rollback(context.Background())
			panic(p)
		}
		if err != nil {
			_ = sp.Rollback(context.Background())
		}
	}()
	if _, err = sp.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID.String()); err != nil {
		return fmt.Errorf("db: set tenant context: %w", err)
	}
	if err = fn(ctx, sp); err != nil {
		return err
	}
	if _, err = sp.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", outer.String()); err != nil {
		return fmt.Errorf("db: restore tenant context: %w", err)
	}
	if err = sp.Commit(ctx); err != nil {
		return fmt.Errorf("db: release savepoint: %w", err)
	}
	return nil
}

// BoundStatements gives every later statement of tx a statement timeout of d -- LOCAL, so it ends
// with the transaction and never reaches the pooled connection's next user (set_config's third
// argument, the same rule as app.tenant_id above). The integration API's reads take it as their
// first statement (internal/domain/tenant: APIReadTimeout); a statement past it is SQLSTATE 57014
// and the request's 503, not a backend kept busy for the rest of the request's life.
//
// It also turns JIT off for the rest of tx, LOCAL the same way (M11 API-8). The API's reads are short
// index walks; the planner prices a definer's body by its guesses -- holding_directory's peer set is
// estimated at a third of all tenants -- and at 21 000 tenants that estimate crossed jit_above_cost and
// every directory page paid 250-320 ms of compilation for a plan that reads a few thousand rows
// (measured, ADR 0028's API-8 note). JIT changes no row a statement reads.
func BoundStatements(ctx context.Context, tx pgx.Tx, d time.Duration) error {
	if d <= 0 {
		return fmt.Errorf("db: a statement bound must be positive, not %v", d)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('statement_timeout', $1, true), set_config('jit', 'off', true)",
		strconv.FormatInt(d.Milliseconds(), 10)); err != nil {
		return fmt.Errorf("db: bound statements: %w", err)
	}
	return nil
}
