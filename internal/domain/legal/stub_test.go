package legal_test

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
)

// stubDB and stubTrail exist ONLY so the constructor tests can pass a non-nil
// dependency. Neither can be called: this package's read and write both need a real
// pgx.Tx, so the behaviour is measured against a real Postgres in legal_db_test.go
// (CLAUDE.md §8 — a fake database cannot test RLS, and a fake transaction cannot
// test an append-only table).
type stubDB struct{}

func (stubDB) WithTenant(context.Context, uuid.UUID, db.TxFunc) error {
	return errors.New("stub: this double is for constructor checks only")
}

type stubTrail struct{}

func (stubTrail) RecordTx(context.Context, pgx.Tx, audit.Event) (uuid.UUID, error) {
	return uuid.Nil, errors.New("stub: this double is for constructor checks only")
}
