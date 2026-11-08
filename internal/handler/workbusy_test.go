package handler

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/db"
)

// workbusy_test.go -- backlog T86: what the panel answers when adminauth's process-wide password-work
// gate refuses (adminauth.ErrBusy). The gate itself is internal/adminauth's (workgate_test.go); this is
// the boundary: one status, one page, the same bytes whatever address was typed, and no budget spent.

// TestAdminLogin_ABusyGateIsOneAnswerAndCostsNoAttempt: a refused sign-in is 503 with the busy page,
// byte-identical for a registered-looking and an unknown address, and it gives the attempt unit back --
// more refusals than adminAttemptLimit still leave a normal failure answered normally.
func TestAdminLogin_ABusyGateIsOneAnswerAndCostsNoAttempt(t *testing.T) {
	t.Parallel()
	busy := true
	admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
		if busy {
			return adminauth.Authentication{}, adminauth.ErrBusy
		}
		return adminauth.Authentication{}, adminauth.ErrBadCredentials
	}}
	b := newBrowser(t, newAdminRouter(t, admins, &fakeTrail{}))
	csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, "/admin/login", nil)))
	var first string
	for i := 0; i < adminAttemptLimit+5; i++ {
		email := "owner@registered.example.test"
		if i%2 == 1 {
			email = "nobody@unknown.example.test"
		}
		rec := b.do(http.MethodPost, "/admin/login", url.Values{"csrf": {csrf}, "email": {email}, "password": {"x"}})
		if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
			t.Fatalf("refusal %d: %d (Retry-After %q), want 503 with Retry-After", i, rec.Code, rec.Header().Get("Retry-After"))
		}
		body := rec.Body.String()
		if !strings.Contains(body, problemAdminBusy.Title) {
			t.Fatalf("refusal %d is not the busy page", i)
		}
		if first == "" {
			first = body
		} else if body != first {
			t.Fatalf("refusal %d (%s) differs from the first: the answer depends on the address", i, email)
		}
	}
	busy = false
	if rec := b.do(http.MethodPost, "/admin/login", url.Values{"csrf": {csrf}, "email": {"x@example.test"}, "password": {"x"}}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("after %d busy refusals a failure answered %d, want 401: the refusals spent the attempt budget", adminAttemptLimit+5, rec.Code)
	}
}

// TestSignup_ABusyGateOpensNothingAndSpendsNoDraft: a refused draft is 503 with the busy page, nothing
// is opened, and the create AND attempt budgets are given back -- signupAttemptLimit+1 busy submits
// (denetimde the eleventh was 429), then signupCreateLimit registrations still go through.
func TestSignup_ABusyGateOpensNothingAndSpendsNoDraft(t *testing.T) {
	t.Parallel()
	p := &fakeProvisioner{}
	d, _ := newSignupDriver(t, p, nil)
	submit := func() int {
		csrf := d.walkToAccount(t)
		d.advance(time.Minute)
		return d.post(signupAccountPath, accountForm(csrf)).Code
	}
	d.claims.mu.Lock()
	d.claims.busy = true
	d.claims.mu.Unlock()
	for i := 0; i < signupAttemptLimit+1; i++ {
		if code := submit(); code != http.StatusServiceUnavailable {
			t.Fatalf("busy submit %d answered %d, want 503", i, code)
		}
	}
	d.claims.mu.Lock()
	d.claims.busy = false
	d.claims.mu.Unlock()
	if p.count() != 0 {
		t.Fatalf("%d draft(s) opened while the gate was full", p.count())
	}
	for i := 0; i < signupCreateLimit; i++ {
		if code := submit(); code != http.StatusSeeOther {
			t.Fatalf("registration %d after the busy ones answered %d, want 303: a refusal spent the create budget", i, code)
		}
	}
}

// TestAdminResetSubmit_ABusyGateSpendsNoSubmission: the work gate's refusal compared nothing, so it gives
// the submission unit back -- adminResetSubmitLimit+1 busy submissions, then a normal one is answered.
func TestAdminResetSubmit_ABusyGateSpendsNoSubmission(t *testing.T) {
	t.Parallel()
	resets := &fakeResets{consumeErr: adminauth.ErrBusy}
	router, _ := newResetRouter(t, resets, &recordingChannel{}, &fakeTrail{})
	b := newBrowser(t, router)
	// A well-shaped link value, built rather than written (the a0-token rule); the fake reads none of it.
	busyToken := strings.Repeat("q", 43)
	for i := 0; i < adminResetSubmitLimit+1; i++ {
		if rec := submitLink(t, b, busyToken, "a-good-enough-password"); rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("busy submission %d answered %d, want 503", i, rec.Code)
		}
	}
	resets.mu.Lock()
	resets.consumeErr = adminauth.ErrResetUnusable
	resets.mu.Unlock()
	if rec := submitLink(t, b, busyToken, "a-good-enough-password"); rec.Code == http.StatusTooManyRequests {
		t.Fatalf("after %d busy refusals a submission got 429: the refusals spent the budget", adminResetSubmitLimit+1)
	}
}

// TestSignupConfirm_ABusyGateSpendsNoConfirmation: the busy proof gives the confirmation unit back --
// signupConfirmLimit+1 busy confirmations, then the right password creates the business.
func TestSignupConfirm_ABusyGateSpendsNoConfirmation(t *testing.T) {
	t.Parallel()
	p := &fakeProvisioner{}
	d, _ := newSignupDriver(t, p, nil)
	link := confirmWalk(t, d)
	d.get(link)
	csrf := d.token(d.get(signupConfirmPath))
	form := url.Values{"csrf": {csrf}, "password": {accountForm("").Get("password")}}
	d.claims.mu.Lock()
	d.claims.busy = true
	d.claims.mu.Unlock()
	for i := 0; i < signupConfirmLimit+1; i++ {
		if res := d.post(signupConfirmPath, form); res.Code != http.StatusServiceUnavailable {
			t.Fatalf("busy confirmation %d answered %d, want 503", i, res.Code)
		}
	}
	d.claims.mu.Lock()
	d.claims.busy = false
	d.claims.mu.Unlock()
	if res := d.post(signupConfirmPath, form); res.Code != http.StatusOK || len(p.provisioned) != 1 {
		t.Fatalf("after %d busy refusals the right password answered %d with %d business(es), want 200 and 1",
			signupConfirmLimit+1, res.Code, len(p.provisioned))
	}
}

// busyInvites is a usable invitation whose new-password acceptance the work gate refuses until told not to.
type busyInvites struct {
	panelAdminInvites
	busy bool
}

func (b *busyInvites) Usable(context.Context, adminauth.InviteToken) error { return nil }

func (b *busyInvites) AcceptNew(context.Context, adminauth.InviteToken, string) (db.AcceptedAdminInvite, error) {
	if b.busy {
		return db.AcceptedAdminInvite{}, adminauth.ErrBusy
	}
	return db.AcceptedAdminInvite{}, adminauth.ErrInviteNotAccepted
}

// TestAdminInvite_ABusyGateSpendsNoAcceptance: the busy acceptance gives the per-address unit back --
// adminInviteAcceptLimit+1 busy acceptances, then one more is answered, not 429.
func TestAdminInvite_ABusyGateSpendsNoAcceptance(t *testing.T) {
	t.Parallel()
	cfg := adminTestConfig()
	cfg.AdminIdentity = true
	fake := &busyInvites{busy: true}
	h, err := NewAdminAuth(&fakeAdmins{}, &fakeTrail{}, newFakeLedger(), newFakeLedger(), &fakeReviewer{}, &fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(), newFakeAccount(), newFakeBrands(), newFakeBrandWriter(), nil, &fakeNotices{}, cfg, slog.New(slog.DiscardHandler), AdminInvitations(fake))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.MountInviteAcceptance(r)
	b := newBrowser(t, r)
	b.cookies[adminInviteCookieName] = "csrf.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	form := url.Values{"csrf": {"csrf"}, "mode": {"new"}, "password": {"a-good-enough-password"}, "password_confirm": {"a-good-enough-password"}}
	for i := 0; i < adminInviteAcceptLimit+1; i++ {
		if code := b.do(http.MethodPost, adminInvitePath, form).Code; code != http.StatusServiceUnavailable {
			t.Fatalf("busy acceptance %d answered %d, want 503", i, code)
		}
	}
	fake.busy = false
	if code := b.do(http.MethodPost, adminInvitePath, form).Code; code == http.StatusTooManyRequests {
		t.Fatalf("after %d busy refusals an acceptance got 429: the refusals spent the budget", adminInviteAcceptLimit+1)
	}
}

// TestSignupConfirm_ABusyGateSpendsNothingOnTheSignInlessBranch is the identity-password twin of
// TestSignupConfirm_ABusyGateSpendsNoConfirmation (the third eye, 2026-11-07: its two refunds were held
// by no test): signupClaimLinkLimit+1 busy proofs give back both the confirmation unit and the link's,
// and the sign-in's own password then creates the business.
func TestSignupConfirm_ABusyGateSpendsNothingOnTheSignInlessBranch(t *testing.T) {
	t.Parallel()
	p := &fakeProvisioner{}
	d, _ := newSignupDriver(t, p, nil)
	email := strings.ToLower(accountForm("").Get("email"))
	d.claims.known[email] = true
	d.claims.signInless[email] = "the sign-in's own password"
	link := confirmWalk(t, d)
	d.get(link)
	csrf := d.token(d.get(signupConfirmPath))
	form := url.Values{"csrf": {csrf}, "password": {"the sign-in's own password"}}
	d.claims.mu.Lock()
	d.claims.busy = true
	d.claims.mu.Unlock()
	for i := 0; i < signupClaimLinkLimit+1; i++ {
		if res := d.post(signupConfirmPath, form); res.Code != http.StatusServiceUnavailable {
			t.Fatalf("busy proof %d answered %d, want 503", i, res.Code)
		}
	}
	d.claims.mu.Lock()
	d.claims.busy = false
	d.claims.mu.Unlock()
	if res := d.post(signupConfirmPath, form); res.Code != http.StatusOK || len(p.provisioned) != 1 {
		t.Fatalf("after %d busy refusals the sign-in's password answered %d with %d business(es), want 200 and 1",
			signupClaimLinkLimit+1, res.Code, len(p.provisioned))
	}
}
