package db

import (
	"context"
	"fmt"
	"net"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// classOnlyError is a connection failure whose TEXT is its class and whose CHAIN is the driver's
// (backlog T208). pgconn's ConnectError renders "failed to connect to `user=... database=...`:
// host:port (host): dial error: ..." and the text is not configurable, so every statement of the
// customer pool that has to open a connection put the role name, the database name and the address
// into whatever logged its error (measured, 2026-11-08, a local TCP proxy stopped after the pool
// opened: `db: begin tx: failed to connect to user=tappa_app database=tappa: 127.0.0.1:56588 ...`).
//
// THE CHAIN STAYS, ONLY THE WORDS GO. Callers read these errors with errors.As and errors.Is: a
// refused sign-in is a *pgconn.PgError inside the ConnectError and its SQLSTATE decides Transient and
// the handlers' answers, a context outcome decides a 503 against a 500, and Transient itself asks for
// *pgconn.ConnectError by type. Unwrap hands all of that over; Error renders the class alone. It needs
// no Format: fmt calls Error for every verb on an error, and where it cannot (the value in a caller's
// unexported field, %#v) it prints the pointer, the err field an address (measured by
// renderEverywhere's matrix in TestPoolStatements_AConnectFailureCarriesOnlyItsClass, with and without
// a Format: the same). A reader that opens the chain on purpose (errors.As into a ConnectError and
// prints THAT) still gets pgx's text: this type closes the accidental path, the log line.
//
// THE CLASS IS connectFailure's (operatorpool.go), the vocabulary T84 gave the pool's opening steps:
// "connection refused", "SQLSTATE 28P01", "timed out", "the database host name does not resolve",
// "attempt 1: ...; attempt 2: ..." for a multi-host DSN. Never a server's sentence, never pgx's.
type classOnlyError struct {
	class string
	err   error
}

func (e *classOnlyError) Error() string { return e.class }
func (e *classOnlyError) Unwrap() error { return e.err }

// quietErr is the pool boundary's one rule: an error that carries a connection attempt -- a
// *pgconn.ConnectError, or a *net.OpError, whose text names the local and the remote address -- leaves
// as a classOnlyError; every other error is returned AS IT IS, the same value, so a sentinel compared
// with == and a statement's own PgError are untouched.
//
// THE *net.OpError CASE IS THE OPEN TRANSACTION'S (both auditors of T208 measured it, 2026-11-08): a
// connection the peer closes in order (FIN) fails the next statement with "conn closed" or "unexpected
// EOF", no address; one that is RESET (a failover, a NAT or load balancer timeout) fails it with
// "write failed: write tcp 10.0.0.5:49152->10.0.0.9:5432: write: broken pipe" -- pgconn wraps the
// socket's *net.OpError as it is. So WithTenant and WithTenantIn pass their transaction's errors through
// here too (tenant.go), not only the pool's.
func quietErr(err error) error {
	if c := reclassified(err); c != nil {
		return c
	}
	return err
}

// reclassified is the classOnlyError err becomes, nil when err is not a connection failure. WithTenant
// needs the answer without comparing errors: an error of an uncomparable type panics under ==.
//
// THE CLASS IS THE FAILED NODE'S, NOT THE WHOLE CHAIN'S (the orchestrator's audit, measured): a joined
// error -- WithTenantIn's restore beside its rollback -- read through connectFailure(err) looked like a
// multi-host DSN's attempts and said "an error of type *errors.errorString; attempt 2 of 2". A
// ConnectError is classed on its own node (its own joins ARE a DSN's attempts); a *net.OpError by
// classifyAttempt on that node. A chain whose every connection node is already under a classOnlyError
// is not classed again: its words (the inner step, "db: restore tenant context: ...") are already safe.
func reclassified(err error) *classOnlyError {
	switch n := rawConnectionNode(err).(type) {
	case *pgconn.ConnectError:
		return &classOnlyError{class: "cannot connect (" + connectFailure(n).reason + ")", err: err}
	case *net.OpError:
		return &classOnlyError{class: "the connection failed (" + classifyAttempt(n).reason + ")", err: err}
	}
	return nil
}

// rawConnectionNode is the first *pgconn.ConnectError or *net.OpError on err's tree, depth first, that
// no classOnlyError covers; nil when there is none.
func rawConnectionNode(err error) error {
	switch e := err.(type) {
	case nil, *classOnlyError:
		return nil
	case *pgconn.ConnectError, *net.OpError:
		return err
	case interface{ Unwrap() []error }:
		for _, c := range e.Unwrap() {
			if n := rawConnectionNode(c); n != nil {
				return n
			}
		}
		return nil
	case interface{ Unwrap() error }:
		return rawConnectionNode(e.Unwrap())
	}
	return nil
}

// inTransaction is fn's error as WithTenant and WithTenantIn return it: VERBATIM, so callers can
// errors.Is/As it and compare sentinels, unless it carries a connection failure -- then the step and the
// class, with the chain kept. fn's own words go in that case (they wrap the driver's text, which cannot
// be cut out of them); "db: tenant transaction" says where it failed.
func inTransaction(err error) error {
	if c := reclassified(err); c != nil {
		return fmt.Errorf("db: tenant transaction: %w", c)
	}
	return err
}

// quietPool is the customer pool as this package's statements see it: the three calls they make, each
// error through quietErr. d.pool itself is read only by the functions TestPoolStatements_GoThroughQuiet
// allows (Ping, whose caller classifies the driver's error itself; Close; the two opening steps, which
// T84's poolStepErr already reduces to a class; and quiet). A statement added on d.pool directly is red
// there.
type quietPool struct{ p *pgxpool.Pool }

func (d *DB) quiet() quietPool { return quietPool{p: d.pool} }

func (q quietPool) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := q.p.Begin(ctx)
	if err != nil {
		return nil, quietErr(err)
	}
	return tx, nil
}

func (q quietPool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return quietRow{r: q.p.QueryRow(ctx, sql, args...)}
}

// Query keeps pgxpool's shape: a non-nil Rows even on an error, so a caller that closes before it
// checks does not panic.
func (q quietPool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	rows, err := q.p.Query(ctx, sql, args...)
	return quietRows{Rows: rows}, quietErr(err)
}

type quietRow struct{ r pgx.Row }

func (r quietRow) Scan(dest ...any) error { return quietErr(r.r.Scan(dest...)) }

// quietRows: a connection that fails after Query returned reaches the caller through Err (pgx.CollectRows
// reads it last) or through Scan.
type quietRows struct{ pgx.Rows }

func (r quietRows) Err() error             { return quietErr(r.Rows.Err()) }
func (r quietRows) Scan(dest ...any) error { return quietErr(r.Rows.Scan(dest...)) }
