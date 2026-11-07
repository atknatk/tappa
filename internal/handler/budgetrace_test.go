package handler

import (
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/invite"
)

// budgetrace_test.go -- backlog T90: the sign-in, sign-up and activation budgets decide in ONE locked
// step under simultaneous requests. Each test sends its requests at once against a fake whose work
// takes a moment (the window an Allowed-then-Charge shape lost the race in) and counts what the budget
// let through: the expensive calls, the audit rows and the single rate_limited row. Repeated, with
// -race in CI's run, because a race is a probability.

// raceAll runs f(0..n-1) in n goroutines released at once.
func raceAll(n int, f func(i int)) {
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			f(i)
		}(i)
	}
	close(start)
	wg.Wait()
}

// TestAdminLogin_RacingFailuresStayInsideTheBudgets: simultaneous wrong passwords from ONE address
// reach Authenticate exactly adminAttemptLimit times; simultaneous wrong passwords against ONE account
// from many addresses write exactly adminAccountLimit failure rows and exactly one rate_limited row.
func TestAdminLogin_RacingFailuresStayInsideTheBudgets(t *testing.T) {
	t.Parallel()
	const n = 40
	t.Run("address", func(t *testing.T) {
		t.Parallel()
		for run := 0; run < 5; run++ {
			admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
				time.Sleep(5 * time.Millisecond) // the password work's moment
				return adminauth.Authentication{}, adminauth.ErrBadCredentials
			}}
			h := newAdminRouter(t, admins, &fakeTrail{})
			browsers := make([]*browser, n)
			forms := make([]url.Values, n)
			for i := range browsers {
				browsers[i] = newBrowser(t, h) // one address, newBrowser's
				forms[i] = url.Values{"csrf": {csrfFrom(t, htmlOf(t, browsers[i].do(http.MethodGet, "/admin/login", nil)))},
					"email": {"probe@example.test"}, "password": {"whatever"}}
			}
			codes := make([]int, n)
			raceAll(n, func(i int) { codes[i] = browsers[i].do(http.MethodPost, "/admin/login", forms[i]).Code })
			refused := 0
			for _, c := range codes {
				if c == http.StatusTooManyRequests {
					refused++
				}
			}
			if admins.authCalls != adminAttemptLimit || refused != n-adminAttemptLimit {
				t.Fatalf("run %d: Authenticate reached %d time(s) and %d refusal(s), want exactly %d and %d",
					run, admins.authCalls, refused, adminAttemptLimit, n-adminAttemptLimit)
			}
		}
	})
	t.Run("account", func(t *testing.T) {
		t.Parallel()
		for run := 0; run < 5; run++ {
			tenantID, adminID := uuid.New(), uuid.New()
			admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
				time.Sleep(5 * time.Millisecond)
				return adminauth.Authentication{Resolved: 1, Attempts: []adminauth.Attempt{
					{AdminUserID: adminID, TenantID: tenantID, Active: true},
				}}, adminauth.ErrBadCredentials
			}}
			trail := &fakeTrail{}
			h := newAdminRouter(t, admins, trail)
			browsers := make([]*browser, n)
			forms := make([]url.Values, n)
			for i := range browsers {
				browsers[i] = newBrowser(t, h)
				browsers[i].ip = fmt.Sprintf("198.51.100.%d:1234", i+1)
				forms[i] = url.Values{"csrf": {csrfFrom(t, htmlOf(t, browsers[i].do(http.MethodGet, "/admin/login", nil)))},
					"email": {"victim@example.test"}, "password": {"guess"}}
			}
			raceAll(n, func(i int) { browsers[i].do(http.MethodPost, "/admin/login", forms[i]) })
			if failed, limited := trail.count(ActionAdminLoginFailed), trail.count(ActionAdminLoginLimited); failed != adminAccountLimit || limited != 1 {
				t.Fatalf("run %d: %d failure row(s) and %d rate_limited row(s), want exactly %d and 1",
					run, failed, limited, adminAccountLimit)
			}
		}
	})
}

// TestSignup_RacingSubmitsStayInsideTheBudgets: simultaneous complete submits from one address open
// exactly signupCreateLimit drafts, and -- with the create budget widened by the test so it cannot
// answer first -- reach the draft exactly signupAttemptLimit times. "Charged after success" is kept:
// the drafts are the create budget's only spend.
func TestSignup_RacingSubmitsStayInsideTheBudgets(t *testing.T) {
	t.Parallel()
	run := func(t *testing.T, n int, widenCreate bool) (opened, refused int) {
		t.Helper()
		p := &fakeProvisioner{}
		d, wizard := newSignupDriver(t, p, nil)
		d.claims.openDelay = 5 * time.Millisecond
		if widenCreate {
			wizard.createLimiter = newLimiter(10*n, time.Hour)
		}
		drivers := make([]*wizardDriver, n)
		forms := make([]url.Values, n)
		for i := range drivers {
			drivers[i] = &wizardDriver{t: t, router: d.router, jar: map[string]string{}, clock: d.clock, claims: d.claims}
			forms[i] = accountForm(drivers[i].walkToAccount(t))
		}
		d.advance(time.Minute)
		codes := make([]int, n)
		raceAll(n, func(i int) { codes[i] = drivers[i].post(signupAccountPath, forms[i]).Code })
		for _, c := range codes {
			if c == http.StatusTooManyRequests {
				refused++
			}
		}
		return p.count(), refused
	}
	for i := 0; i < 5; i++ {
		if opened, refused := run(t, 8, false); opened != signupCreateLimit || refused != 8-signupCreateLimit {
			t.Fatalf("create, run %d: %d draft(s) opened and %d refusal(s), want exactly %d and %d",
				i, opened, refused, signupCreateLimit, 8-signupCreateLimit)
		}
		if opened, refused := run(t, 25, true); opened != signupAttemptLimit || refused != 25-signupAttemptLimit {
			t.Fatalf("attempt, run %d: %d draft(s) opened and %d refusal(s), want exactly %d and %d",
				i, opened, refused, signupAttemptLimit, 25-signupAttemptLimit)
		}
	}
}

// TestRateLimit_RacingFailuresStayInsideTheInviteBudget: simultaneous submits of one consumed
// invitation write exactly inviteFailureLimit activation.failed rows and exactly one
// activation.rate_limited row (the row the two-step shape could lose).
func TestRateLimit_RacingFailuresStayInsideTheInviteBudget(t *testing.T) {
	t.Parallel()
	const n = 40
	for run := 0; run < 5; run++ {
		rec := &fakeAudit{}
		inv := &fakeInvites{lookup: func(invite.Code) (invite.Context, error) {
			time.Sleep(5 * time.Millisecond) // the lookup's moment
			return okContext("invited"), invite.ErrCodeUsed
		}}
		h := newHandler(t, inv, &fakeSessions{}, rec)
		codes := make([]int, n)
		raceAll(n, func(i int) {
			codes[i] = post(t, h, consent(), codeCookie()).Code
		})
		failed, limited := 0, 0
		for _, e := range rec.events {
			switch e.Action {
			case ActionActivationFailed:
				failed++
			case ActionActivationLimited:
				limited++
			}
		}
		if failed != inviteFailureLimit || limited != 1 {
			t.Fatalf("run %d: %d activation.failed row(s) and %d rate_limited row(s), want exactly %d and 1 (codes %v)",
				run, failed, limited, inviteFailureLimit, codes)
		}
	}
}
