package adminauth

import (
	"context"
	"slices"
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

func legacyRows(k int, digest string) []db.ResolvedAdmin {
	out := make([]db.ResolvedAdmin, 0, k)
	for range k {
		out = append(out, db.ResolvedAdmin{ID: uuid.New(), TenantID: uuid.New(), PasswordHash: db.NewPasswordHash(digest), Status: "active"})
	}
	return out
}

// TestLinkBusinessShape_ForwardAlwaysPaysOnePlusMaxCandidates: a linked sign-in's "Link a business"
// with 0, 1, 8 and 9 unlinked rows of the address, the other password wrong or right, compares
// exactly 1 + MaxCandidates times -- the window padded (Manager.pad). MUTATION: the pad removed -> red.
func TestLinkBusinessShape_ForwardAlwaysPaysOnePlusMaxCandidates(t *testing.T) {
	id := uuid.New()
	for _, k := range []int{0, 1, 8, 9} {
		for _, other := range []string{"wrong", "old"} {
			d := &shapeDB{ident: shapeIdentity(id), rows: legacyRows(k, "digest:old")}
			m, n, s := accountShape(t, d, &id)
			_, _ = m.LinkBusiness(context.Background(), s, "idpw", other, newCountingBudget(100))
			if *n != 1+MaxCandidates {
				t.Errorf("%d rows, other %q: %d comparisons, want %d", k, other, *n, 1+MaxCandidates)
			}
		}
	}
}

// TestLinkBusinessShape_ReverseAlwaysPaysTwoAndOneEventStep: an unlinked sign-in's reverse link -- its
// own password right -- with the address naming an identity or none, the identity's password wrong
// or right, compares exactly twice; and a failure with and without an identity makes the SAME
// database calls (the event step runs for both -- recordFailure, D3). MUTATION: the dummy comparison
// of an address with no identity removed (proveIdentity) -> red.
func TestLinkBusinessShape_ReverseAlwaysPaysTwoAndOneEventStep(t *testing.T) {
	var failures [][]string
	for _, present := range []bool{true, false} {
		for _, other := range []string{"wrong", "idpw"} {
			d := &shapeDB{}
			if present {
				d.ident = shapeIdentity(uuid.New())
			}
			m, n, s := accountShape(t, d, nil)
			_, _ = m.LinkBusiness(context.Background(), s, "rowpw", other, newCountingBudget(100))
			if *n != 2 {
				t.Errorf("identity %v, other %q: %d comparisons, want 2", present, other, *n)
			}
			if other == "wrong" {
				failures = append(failures, slices.Clone(d.ops))
			}
		}
	}
	if len(failures) != 2 || !slices.Equal(failures[0], failures[1]) {
		t.Errorf("a failure with an identity calls %v, without one %v; want one sequence", failures[0], failures[1])
	}
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
