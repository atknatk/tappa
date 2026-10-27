package adminauth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/config"
)

// signupclaimbudget_db_test.go -- the registration's identity-password branch under the identity's failure
// budget (M14-17b; the 2026-10-27 security audit's ORTA finding, the user's decision "İkisini birden"):
// a wrong password is the sign-in's failure -- identity_record_failed_login, the sign-in's per-identity
// budget -- and a spent budget refuses the branch. On Postgres, through the real functions.

// budgetClaims is a SignupClaims over d with a cheap draft digest (the comparison is the identity's
// MinCost digest's) and a helper that opens and resolves a fresh link for email.
func budgetClaims(t *testing.T, w *idWorld) (*SignupClaims, func() SignupClaim) {
	t.Helper()
	d := testDB(t)
	s, err := NewSignupClaims(d, &config.Config{SessionHMACKey: []byte(strings.Repeat("b", 32))})
	if err != nil {
		t.Fatal(err)
	}
	s.digest = func(string) (string, error) { return "$2a$04$" + strings.Repeat("q", 53), nil }
	draft := []byte(`{"full_name": "Budget Owner", "business_name": "Budget", "vat_number": "MT00000001", "business_type": "restaurant", ` +
		`"structure": "single", "vat_check": "unknown", "venues": ["Front door"]}`)
	t.Cleanup(func() { w.exec(`DELETE FROM admin_signup_claims WHERE email = $1`, w.email) })
	link := func() SignupClaim {
		t.Helper()
		g, err := s.Open(context.Background(), w.email, "the draft's password", draft)
		if err != nil {
			t.Fatal(err)
		}
		c, err := s.Resolve(context.Background(), ParseSignupClaimToken(strings.TrimPrefix(g.Link(""), "?t=")))
		if err != nil || !c.IdentityPassword {
			t.Fatalf("resolve: %v, identity password %v", err, c.IdentityPassword)
		}
		return c
	}
	return s, link
}

// TestSignupClaims_WrongIdentityPasswordsSpendTheIdentitysBudgetAcrossLinks: an identity with no active
// membership, a budget of 3. Three wrong passwords, each on a NEW registration link -> three
// ErrBadCredentials and three login_failed rows (empty detail: no address, no digest); the fourth, on a
// fourth link -> ErrClaimBudgetSpent and ONE login_limited row; then the RIGHT password, on a fifth link
// -> still ErrClaimBudgetSpent and no row: new links do not reset the budget and a spent budget refuses.
// CONTROL: with an unspent budget the right password proves the draft.
//
// MUTATION (measured): the branch's charge-and-record removed (the call keeps the uuid.Nil round trip)
// -> no rows and the right password proves at the end, red.
func TestSignupClaims_WrongIdentityPasswordsSpendTheIdentitysBudgetAcrossLinks(t *testing.T) {
	w := newIDWorld(t, ownerConn(t), true)
	s, link := budgetClaims(t, w)
	ctx := context.Background()
	budget := newCountingBudget(3)

	for i := 1; i <= 3; i++ {
		if _, err := s.ProveWithIdentityPassword(ctx, link(), "guess-"+uuid.NewString(), budget); !errors.Is(err, ErrBadCredentials) {
			t.Fatalf("wrong password %d: %v, want ErrBadCredentials", i, err)
		}
		if n := w.events("login_failed"); n != i {
			t.Fatalf("after %d wrong password(s) %d login_failed rows", i, n)
		}
	}
	if n := w.int(`SELECT count(*) FROM admin_identity_events WHERE identity_id = $1
	                AND (detail IS NOT NULL AND detail <> '{}'::jsonb OR detail::text ILIKE '%' || $2 || '%' OR detail::text LIKE '%$2a$%')`,
		w.ident, w.email); n != 0 {
		t.Fatalf("%d event rows carry a detail (an address or a digest)", n)
	}
	if _, err := s.ProveWithIdentityPassword(ctx, link(), "guess-"+uuid.NewString(), budget); !errors.Is(err, ErrClaimBudgetSpent) {
		t.Fatalf("the fourth wrong password: %v, want ErrClaimBudgetSpent", err)
	}
	if n := w.events("login_limited"); n != 1 {
		t.Fatalf("%d login_limited rows past the budget, want 1", n)
	}
	before := w.int(`SELECT count(*) FROM admin_identity_events WHERE identity_id = $1`, w.ident)
	if _, err := s.ProveWithIdentityPassword(ctx, link(), w.pw, budget); !errors.Is(err, ErrClaimBudgetSpent) {
		t.Fatalf("the right password on a new link with the budget spent: %v, want ErrClaimBudgetSpent", err)
	}
	if after := w.int(`SELECT count(*) FROM admin_identity_events WHERE identity_id = $1`, w.ident); after != before {
		t.Fatal("the refused right password wrote an event row")
	}
	if p, err := s.ProveWithIdentityPassword(ctx, link(), w.pw, newCountingBudget(3)); err != nil || p.hash == "" {
		t.Fatalf("CONTROL: the right password with an unspent budget: %v", err)
	}
}

// TestSignupClaims_TheSignInAndTheRegistrationShareOneBudget: ONE budget (limit 3) handed to both, as
// internal/handler hands the sign-in's account limiter to both. Two failed sign-ins of the identity
// (AuthenticatePerson: a membership-less identity's sign-in is a failure whatever the password) and one
// wrong password on a registration link spend it; the right password on the link is then refused
// (ErrClaimBudgetSpent). Three login_failed rows, one trail.
//
// MUTATION (measured): the branch charging another key than the sign-in's (identityBudgetKey) -> the right
// password proves, red.
func TestSignupClaims_TheSignInAndTheRegistrationShareOneBudget(t *testing.T) {
	w := newIDWorld(t, ownerConn(t), true)
	s, link := budgetClaims(t, w)
	m := testManager(t, testDB(t))
	ctx := context.Background()
	budget := newCountingBudget(3)

	for i := 0; i < 2; i++ {
		if _, err := m.AuthenticatePerson(ctx, w.email, "guess-"+uuid.NewString(), uuid.Nil, budget); !errors.Is(err, ErrBadCredentials) {
			t.Fatalf("failed sign-in %d: %v", i+1, err)
		}
	}
	if _, err := s.ProveWithIdentityPassword(ctx, link(), "guess-"+uuid.NewString(), budget); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("the registration's wrong password: %v", err)
	}
	if n := w.events("login_failed"); n != 3 {
		t.Fatalf("%d login_failed rows from two sign-ins and one registration guess, want 3", n)
	}
	if _, err := s.ProveWithIdentityPassword(ctx, link(), w.pw, budget); !errors.Is(err, ErrClaimBudgetSpent) {
		t.Fatalf("the right password after the shared budget was spent: %v, want ErrClaimBudgetSpent", err)
	}
}
