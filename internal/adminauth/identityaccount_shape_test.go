package adminauth

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
)

// identityaccount_shape_test.go -- the account acts' comparison counts (M14-18's third eye, O2): the
// work an answer costs must not say whether a row or an identity exists. Counted through
// Manager.compareFn (shapeManager) with the session row read through rowFn -- no database.

// accountShape is a Manager over shapeDB whose session row is s's (address, digest of "rowpw").
func accountShape(t *testing.T, d *shapeDB, linkedTo *uuid.UUID) (*Manager, *int, Resolved) {
	t.Helper()
	m, n := shapeManager(t, d)
	s := Resolved{SessionID: uuid.New(), TenantID: uuid.New(), AdminUserID: uuid.New(), Role: "owner"}
	if linkedTo != nil {
		s.Linked = true
		s.switchBucket = m.switchBucketOf(linkedTo)
	}
	m.rowFn = func(Resolved) (rowFacts, error) {
		return rowFacts{email: "person@example.test", digest: "digest:rowpw", status: "active"}, nil
	}
	return m, n, s
}

func shapeIdentity(id uuid.UUID) *db.ResolvedIdentity {
	return &db.ResolvedIdentity{ID: id, PasswordHash: db.NewPasswordHash("digest:idpw"), HasActiveMembership: true, Email: "person@example.test"}
}

// TestWindowActShape_UnlinkAndRelinkPayOne: Unlink and Relink, the address naming the session's
// identity or none, the password wrong, compare exactly once.
func TestWindowActShape_UnlinkAndRelinkPayOne(t *testing.T) {
	id := uuid.New()
	for _, known := range []bool{true, false} {
		d := &shapeDB{}
		if known {
			d.ident = shapeIdentity(id)
		}
		m, n, s := accountShape(t, d, &id)
		_, _, _ = m.Unlink(context.Background(), s, "wrong", uuid.New(), newCountingBudget(100))
		if *n != 1 {
			t.Errorf("Unlink, address known %v: %d comparisons, want 1", known, *n)
		}
		*n = 0
		_, _ = m.Relink(context.Background(), s, "wrong", uuid.New(), newCountingBudget(100))
		if *n != 1 {
			t.Errorf("Relink, address known %v: %d comparisons, want 1", known, *n)
		}
	}
}
