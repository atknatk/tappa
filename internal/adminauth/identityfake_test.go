package adminauth

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
)

// noIdentityData is the identity half of Database for the fakes whose subject is Authenticate, Issue
// or Verify: none of those reaches an identity function, so every method here refuses loudly rather
// than answering something a test could mistake for the database's.
type noIdentityData struct{}

var errNoIdentityData = errors.New("fake: the identity functions must not be reached by this test")

func (noIdentityData) ResolveIdentityByEmail(context.Context, string) (db.ResolvedIdentity, error) {
	return db.ResolvedIdentity{}, errNoIdentityData
}

func (noIdentityData) OpenIdentitySession(context.Context, uuid.UUID, uuid.UUID, string, db.PasswordHash) (db.IdentitySession, error) {
	return db.IdentitySession{}, errNoIdentityData
}

func (noIdentityData) IdentitySessionDecoy(context.Context) error { return errNoIdentityData }

func (noIdentityData) LinkLegacySession(context.Context, string, string) (db.LinkedMembership, error) {
	return db.LinkedMembership{}, errNoIdentityData
}

func (noIdentityData) RecordFailedIdentityLogin(context.Context, uuid.UUID, string) (bool, error) {
	return false, errNoIdentityData
}
