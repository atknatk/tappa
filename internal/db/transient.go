package db

import (
	"context"
	"errors"
	"io"
	"net"

	"github.com/jackc/pgx/v5/pgconn"
)

// Transient reports whether err says the database could not answer NOW, as opposed to
// having answered no: a statement that outlived its deadline, a connection that was
// refused or dropped, a server restarting or out of connections, a lock or a
// serialization conflict lost. Trying once more can help with those and only those.
//
// A CANCELLED CONTEXT IS NOT TRANSIENT: somebody decided to stop (the shutdown budget
// spent), and a second try would disobey them. ANYTHING NOT NAMED HERE IS PERMANENT,
// the side a new failure should fall on: a constraint, a privilege, a missing function,
// a value the server refuses, a closed pool will all answer the same again.
//
// ⚠️ IT SAYS NOTHING ABOUT WHETHER THE WRITE HAPPENED. A dropped connection or a passed
// deadline can arrive after the COMMIT reached the server. Whoever retries on this
// answer must write something safe to write twice.
func Transient(err error) bool {
	switch {
	case err == nil, errors.Is(err, context.Canceled):
		return false
	case errors.Is(err, context.DeadlineExceeded):
		return true
	}
	// Before the connection cases: a refused sign-in arrives as a PgError inside a
	// ConnectError, and it is the server's answer that decides.
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return transientState(pg.Code)
	}
	var connect *pgconn.ConnectError
	var network net.Error
	return errors.As(err, &connect) || errors.As(err, &network) ||
		pgconn.Timeout(err) || pgconn.SafeToRetry(err) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed)
}

// transientState is the part of the SQLSTATE table Transient trusts: class 08
// (connection exception), and by name a server going away or not yet back, too many
// connections, a statement cancelled by a server-side timeout, a lock not available, a
// serialization failure and a deadlock.
func transientState(code string) bool {
	if len(code) == 5 && code[:2] == "08" {
		return true
	}
	switch code {
	case "57P01", "57P02", "57P03", "53300", "57014", "55P03", "40001", "40P01":
		return true
	}
	return false
}
