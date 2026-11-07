package handler

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/adminauth"
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
// is opened, and the create budget is given back -- signupCreateLimit registrations still go through.
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
	for i := 0; i < signupCreateLimit; i++ {
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
