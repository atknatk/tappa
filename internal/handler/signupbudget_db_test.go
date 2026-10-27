package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/signup"
	"github.com/atknatk/tappa/internal/httpx"
)

// signupbudget_db_test.go -- M14-17b's two audits of 78a33f5c (the security audit's ORTA finding, the
// third eye's ORTA-1; the user's decision "İkisini birden"): on an installation that shows the
// registration link ON SCREEN (sandbox, explicit dev) anybody can mint links for a victim's address, so the
// per-link gate is worth nothing there and the IDENTITY's brake is the whole defence. That brake is the
// sign-in's own: AdminAuth's account limiter under the identity's key, handed to the wizard by
// SignupSharesIdentityBudget. Real Postgres, the real adminauth.SignupClaims, the real panel sign-in.

// countingProvisioner counts the proofs that reached it and creates nothing.
type countingProvisioner struct {
	mu sync.Mutex
	n  int
}

func (c *countingProvisioner) Provision(context.Context, signup.Claim) (signup.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	return signup.Result{}, signup.ErrClaimRefused
}

// TestSignupConfirm_OnScreenGuessesAcrossLinksAndAddressesSpendTheSignInsBudget: an identity with no
// active membership, its address typed by a stranger on the on-screen installation. Four failed panel
// sign-ins of the identity, then six wrong passwords on the registration's password branch -- each on a
// NEW link, from a NEW address, so neither the per-address nor the per-link gate ever binds -- are
// answered normally (the sign-in's one failure; 422); together they spend the identity's budget
// (adminAccountLimit = 10, the sign-in's own). The RIGHT password, on yet another link from yet another
// address, is then refused with the paused page (429) and never reaches the provisioner. The trail is the
// sign-in's: ten login_failed rows and one login_limited.
//
// MUTATION (measured): the registration branch's charge removed (adminauth's chargeIdentityFailure call on
// the wrong password) -> the right password reaches the provisioner, red.
func TestSignupConfirm_OnScreenGuessesAcrossLinksAndAddressesSpendTheSignInsBudget(t *testing.T) {
	p := newIDPanel(t, panelHarnessOptions{})
	email := "m14-17b-" + uuid.NewString() + "@identity.example"
	const right = "the sign-in's own password"
	digest, err := bcrypt.GenerateFromPassword([]byte(right), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	ident := p.identity(t, email, string(digest))
	t.Cleanup(func() {
		release := holdPanelTablesLock(t)
		defer release()
		if _, err := p.owner.Exec(context.Background(), `DELETE FROM admin_signup_claims WHERE email = $1`, email); err != nil {
			t.Errorf("remove the drafts: %v", err)
		}
	})

	cfg := signupTestConfig()
	claims, err := adminauth.NewSignupClaims(p.data, cfg)
	if err != nil {
		t.Fatal(err)
	}
	prov := &countingProvisioner{}
	wizard, err := NewSignup(prov, claims, nil, nil, cfg, slog.New(slog.DiscardHandler), SignupSharesIdentityBudget(p.auth))
	if err != nil {
		t.Fatal(err)
	}
	if wizard.delivery != signupOnScreen {
		t.Fatal("PREMISE: the installation does not show the link on screen")
	}
	if wizard.identityBudget != p.auth.accountLimiter {
		t.Fatal("the wizard's identity budget is not the sign-in's account limiter")
	}
	now := time.Now()
	wizard.state.now = func() time.Time { return now }
	wizard.floor = 0
	router := httpx.NewRouter(nil, nil, wizard)

	// One visitor per attempt: a fresh cookie jar, a fresh address, a fresh link.
	attempt := func(i int, password string) int {
		t.Helper()
		d := &wizardDriver{t: t, router: router, jar: map[string]string{}, clock: &now, remote: fmt.Sprintf("198.51.100.%d:4321", i+1)}
		csrf := d.walkToAccount(t)
		d.advance(time.Minute)
		form := accountForm(csrf)
		form.Set("email", email)
		if res := d.post(signupAccountPath, form); res.Code != http.StatusSeeOther {
			t.Fatalf("visitor %d: POST %s = %d", i, signupAccountPath, res.Code)
		}
		m := screenLinkRE.FindStringSubmatch(d.get(signupSentPath).Body.String())
		if m == nil {
			t.Fatalf("visitor %d: no link on the on-screen installation's page", i)
		}
		u, err := url.Parse(m[1])
		if err != nil {
			t.Fatal(err)
		}
		d.get(u.RequestURI())
		page := d.get(signupConfirmPath)
		if !strings.Contains(page.Body.String(), "Your Taptime password") {
			t.Fatalf("visitor %d: the link's page is not the sign-in-less form (%d)", i, page.Code)
		}
		return d.post(signupConfirmPath, url.Values{"csrf": {d.token(page)}, "password": {password}}).Code
	}

	for i := 0; i < 4; i++ {
		res, _ := p.login(t, email, "a sign-in guess "+uuid.NewString())
		if res.StatusCode == http.StatusSeeOther {
			t.Fatalf("sign-in guess %d signed in", i+1)
		}
	}
	for i := 0; i < 6; i++ {
		if code := attempt(i, "a registration guess "+uuid.NewString()); code != http.StatusUnprocessableEntity {
			t.Fatalf("registration guess %d: %d, want 422", i+1, code)
		}
	}
	if code := attempt(6, right); code != http.StatusTooManyRequests {
		t.Fatalf("the right password after the shared budget was spent: %d, want 429", code)
	}
	if prov.n != 0 {
		t.Fatalf("%d proof(s) reached the provisioner past the budget", prov.n)
	}
	if n := p.count(t, `SELECT count(*) FROM admin_identity_events WHERE identity_id = $1 AND event = 'login_failed'`, ident); n != 10 {
		t.Fatalf("%d login_failed rows from four sign-in and six registration guesses, want 10", n)
	}
	if n := p.count(t, `SELECT count(*) FROM admin_identity_events WHERE identity_id = $1 AND event = 'login_limited'`, ident); n != 1 {
		t.Fatalf("%d login_limited rows, want 1", n)
	}
}
