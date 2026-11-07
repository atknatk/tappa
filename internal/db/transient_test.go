package db

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestTransient is the table a retry trusts (T197). The real errors a stall and a cut connection give
// are measured over Postgres by internal/handler's TestResetOutcomeDB_ tests: a passed deadline, a
// dropped connection, a lost COMMIT answer.
func TestTransient(t *testing.T) {
	t.Parallel()
	pg := func(code string) error { return fmt.Errorf("db: commit: %w", &pgconn.PgError{Code: code}) }
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"a passed deadline", fmt.Errorf("audit: record: %w", context.DeadlineExceeded), true},
		{"a cancelled context: somebody decided to stop", fmt.Errorf("db: begin tx: %w", context.Canceled), false},
		{"a dropped connection", fmt.Errorf("x: %w", io.ErrUnexpectedEOF), true},
		{"a closed socket", fmt.Errorf("x: %w", net.ErrClosed), true},
		{"a refused connection", fmt.Errorf("x: %w", &net.OpError{Op: "dial", Err: errors.New("connection refused")}), true},
		{"a connection exception", pg("08006"), true},
		{"the server shutting down", pg("57P01"), true},
		{"the server crashing", pg("57P02"), true},
		{"the server not back yet", pg("57P03"), true},
		{"too many connections", pg("53300"), true},
		{"a server-side timeout", pg("57014"), true},
		{"a lock not available", pg("55P03"), true},
		{"a serialization failure", pg("40001"), true},
		{"a deadlock", pg("40P01"), true},
		{"a check constraint", pg("23514"), false},
		{"a unique violation", pg("23505"), false},
		{"a foreign key", pg("23503"), false},
		{"a missing privilege", pg("42501"), false},
		{"a missing function", pg("42883"), false},
		{"a value the server refuses", pg("22P02"), false},
		{"a closed transaction", fmt.Errorf("x: %w", pgx.ErrTxClosed), false},
		{"no rows", fmt.Errorf("x: %w", pgx.ErrNoRows), false},
		{"anything else", errors.New("something unnamed"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Transient(tc.err); got != tc.want {
				t.Errorf("Transient(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
