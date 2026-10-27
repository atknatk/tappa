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
// membership, the branch's counter at 3. Three wrong passwords, each on a NEW registration link -> three
// ErrBadCredentials and three login_failed rows (empty detail: no address, no digest); the fourth, on a
// fourth link -> ErrClaimBudgetSpent and ONE login_limited row; then the RIGHT password, on a fifth link
// -> still ErrClaimBudgetSpent and no row: new links do not reset the counter and a spent counter refuses.
// CONTROL: with an unspent counter the right password proves the draft.
//
// MUTATION (measured): the reservation removed -> the fourth guess is compared and the right password
// proves at the end, red.
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

// TestSignupClaims_SignInFailuresNeverPauseTheBranch (the user's decision of 2026-10-27, "Ret sayacını
// ayır"): ONE limiter (limit 3) handed to both, so only the KEYS keep them apart. Five failed sign-ins of the
// identity spend its sign-in budget (three login_failed rows and the one login_limited) -- and the
// registration's branch still compares: a wrong password is ErrBadCredentials (its own login_failed row,
// the shared trail), the right one proves the draft. Before the decision anybody who knew the address
// could keep the branch paused with failed sign-ins.
//
// MUTATION (measured): the branch keyed with the sign-in's identityBudgetKey -> ErrClaimBudgetSpent, red.
func TestSignupClaims_SignInFailuresNeverPauseTheBranch(t *testing.T) {
	w := newIDWorld(t, ownerConn(t), true)
	s, link := budgetClaims(t, w)
	m := testManager(t, testDB(t))
	ctx := context.Background()
	budget := newCountingBudget(3)

	for i := 0; i < 5; i++ {
		if _, err := m.AuthenticatePerson(ctx, w.email, "guess-"+uuid.NewString(), uuid.Nil, budget); !errors.Is(err, ErrBadCredentials) {
			t.Fatalf("failed sign-in %d: %v", i+1, err)
		}
	}
	if f, l := w.events("login_failed"), w.events("login_limited"); f != 3 || l != 1 {
		t.Fatalf("PREMISE: the sign-in budget is not spent (%d login_failed, %d login_limited)", f, l)
	}
	if _, err := s.ProveWithIdentityPassword(ctx, link(), "guess-"+uuid.NewString(), budget); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("the branch's wrong password after the sign-in budget was spent: %v, want ErrBadCredentials", err)
	}
	if n := w.events("login_failed"); n != 4 {
		t.Fatalf("%d login_failed rows, want the branch's fourth on the shared trail", n)
	}
	if p, err := s.ProveWithIdentityPassword(ctx, link(), w.pw, budget); err != nil || p.hash == "" {
		t.Fatalf("the right password after failed sign-ins: %v, want a proof", err)
	}
}
