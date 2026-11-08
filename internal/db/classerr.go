package db

import (
	"context"
	"errors"
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
// with == and a statement's own PgError are untouched. Measured after the connection was up (the
// proxy cut it in flight, between statements and before COMMIT): pgx says "conn closed" or
// "unexpected EOF", no address, and those pass through.
func quietErr(err error) error {
	var connect *pgconn.ConnectError
	var op *net.OpError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &connect):
		return &classOnlyError{class: "cannot connect (" + connectFailure(err).reason + ")", err: err}
	case errors.As(err, &op):
		return &classOnlyError{class: "the connection failed (" + connectFailure(err).reason + ")", err: err}
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
