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
// third eye's ORTA-1; the user's decisions "İkisini birden" and "Ret sayacını ayır"): on an installation
// that shows the registration link ON SCREEN (sandbox, explicit dev) anybody can mint links for a victim's
// address, so the per-link gate is worth nothing there and the IDENTITY's counter of the branch is the whole
// defence -- its OWN counter (claimIdentityLimiter), which the sign-in's failures never spend. Real Postgres,
// the real adminauth.SignupClaims, the real panel sign-in.

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

// TestSignupConfirm_OnScreenGuessesAcrossLinksAndAddressesSpendTheBranchsCounter: an identity with no
// active membership, its address typed by strangers on the on-screen installation. Ten failed panel
// sign-ins spend the SIGN-IN's identity budget whole (ten login_failed; the panel's per-address gate stops
// an eleventh from one address) -- and pause nothing here. Then ten wrong passwords on the registration's password branch -- each on a NEW link, from a NEW
// address, so neither the per-address nor the per-link gate binds -- are each compared and answered 422;
// they spend the branch's own counter (signupClaimIdentityLimit). The RIGHT password, on yet another link
// from yet another address, is refused with the paused page (429) and never reaches the provisioner. The
// trail is shared: twenty login_failed rows and the branch's one login_limited.
//
// MUTATION (measured): adminauth's reservation removed -> the right password reaches the provisioner, red.
func TestSignupConfirm_OnScreenGuessesAcrossLinksAndAddressesSpendTheBranchsCounter(t *testing.T) {
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
	wizard, err := NewSignup(prov, claims, nil, nil, cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if wizard.delivery != signupOnScreen {
		t.Fatal("PREMISE: the installation does not show the link on screen")
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

	for i := 0; i < adminAccountLimit; i++ {
		res, _ := p.login(t, email, "a sign-in guess "+uuid.NewString())
		if res.StatusCode == http.StatusSeeOther {
			t.Fatalf("sign-in guess %d signed in", i+1)
		}
	}
	for i := 0; i < signupClaimIdentityLimit; i++ {
		if code := attempt(i, "a registration guess "+uuid.NewString()); code != http.StatusUnprocessableEntity {
			t.Fatalf("registration guess %d after the sign-in budget was spent: %d, want 422 (compared)", i+1, code)
		}
	}
	if code := attempt(signupClaimIdentityLimit, right); code != http.StatusTooManyRequests {
		t.Fatalf("the right password after the branch's counter was spent: %d, want 429", code)
	}
	if prov.n != 0 {
		t.Fatalf("%d proof(s) reached the provisioner past the counter", prov.n)
	}
	if n := p.count(t, `SELECT count(*) FROM admin_identity_events WHERE identity_id = $1 AND event = 'login_failed'`, ident); n != 20 {
		t.Fatalf("%d login_failed rows from ten counted sign-in and ten registration guesses, want 20", n)
	}
	if n := p.count(t, `SELECT count(*) FROM admin_identity_events WHERE identity_id = $1 AND event = 'login_limited'`, ident); n != 1 {
		t.Fatalf("%d login_limited rows, want the branch's one", n)
	}
}
