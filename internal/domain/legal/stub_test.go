package legal_test

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
)

// stubDB exists ONLY so the constructor test can pass a non-nil dependency. It cannot
// be called: this package's read needs a real pgx.Tx, so the behaviour is measured
// against a real Postgres in legal_db_test.go (CLAUDE.md §8 — a fake database cannot
// test RLS, and a fake transaction cannot test an append-only table).
type stubDB struct{}

func (stubDB) WithTenant(context.Context, uuid.UUID, db.TxFunc) error {
	return errors.New("stub: this double is for constructor checks only")
}
