package handler

// adminloginidentity_test.go -- the identity path of POST /admin/login (M14-09) against a FAKE
// adminauth.Manager: which path the flag chooses, what the handler writes and does not write, which
// budget it hands over, the one failure body, the "sign in again" answer, and the audit payloads
// that may not name the person's other businesses (R21). The database-backed half is
// adminloginidentity_db_test.go.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/adminauth"
)

// newLoginAuth is newAdminRouterWithActions with TAPPA_ADMIN_IDENTITY set to on, returning the
// *AdminAuth too (a test compares what it handed over with its own fields).
func newLoginAuth(t *testing.T, admins *fakeAdmins, trail *fakeTrail, on bool) (*AdminAuth, http.Handler) {
	t.Helper()
	cfg := adminTestConfig()
	cfg.AdminIdentity = on
	records := newFakeLedger()
	h, err := NewAdminAuth(admins, trail, records, records, &fakeReviewer{}, &fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(), newFakeAccount(), newFakeBrands(), newFakeBrandWriter(), nil, &fakeNotices{}, cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	registerPanel(r, h)
	return h, r
}

// signInWith loads the form and posts it once.
func signInWith(t *testing.T, b *browser, password string) *httptest.ResponseRecorder {
	t.Helper()
	csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, "/admin/login", nil)))
	return b.do(http.MethodPost, "/admin/login", url.Values{
		"csrf": {csrf}, "email": {"person@example.test"}, "password": {password},
	})
}

// legacyOf is an Authentication of the legacy arm: one Attempt per row given.
func legacyOf(rows ...adminauth.Attempt) adminauth.Authentication {
	return adminauth.Authentication{Resolved: len(rows), Attempts: rows}
}

// TestAdminLoginIdentity_TheFlagChoosesThePath: flag on, the sign-in asks AuthenticatePerson and
// never Authenticate; flag off, the reverse. MUTATION: Login's `if a.identityPath()` made `true`
// (or `false`) -> one of the two rows is red.
func TestAdminLoginIdentity_TheFlagChoosesThePath(t *testing.T) {
	for _, on := range []bool{false, true} {
		admins := &fakeAdmins{}
		_, h := newLoginAuth(t, admins, &fakeTrail{}, on)
		rec := signInWith(t, newBrowser(t, h), "whatever")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("flag %v: status %d, want 401", on, rec.Code)
		}
		wantPerson, wantAuth := 0, 1
		if on {
			wantPerson, wantAuth = 1, 0
		}
		if admins.personCalls != wantPerson || admins.authCalls != wantAuth {
			t.Errorf("flag %v: AuthenticatePerson %d call(s), Authenticate %d; want %d and %d",
				on, admins.personCalls, admins.authCalls, wantPerson, wantAuth)
		}
	}
}

// TestAdminLoginIdentity_AnIdentitySessionWritesNoSecondLoginRow: the identity session's cookie is
// set and the person goes to /admin -- and the handler records NO admin.login.succeeded row, because
// identity_open_session wrote it (ADR 0031 §5.6; the M14-07 audit's carry-over). Issue is not asked.
// MUTATION: completePersonLogin records adminLoginSucceededEvent -> red.
func TestAdminLoginIdentity_AnIdentitySessionWritesNoSecondLoginRow(t *testing.T) {
	tenant, admin := uuid.New(), uuid.New()
	admins := &fakeAdmins{person: func(string, string, uuid.UUID, adminauth.FailureBudget) (adminauth.PersonSignIn, error) {
		return adminauth.PersonSignIn{Session: &adminauth.Issued{
			Session: adminauth.Session{ID: uuid.New(), TenantID: tenant, AdminUserID: admin},
			Token:   fakeIssuedToken(),
		}, Linked: 2}, nil
	}}
	trail := &fakeTrail{}
	_, h := newLoginAuth(t, admins, trail, true)
	b := newBrowser(t, h)
	rec := signInWith(t, b, "right")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin" {
		t.Fatalf("an identity session: %d to %q, want 303 to /admin", rec.Code, rec.Header().Get("Location"))
	}
	if b.cookies[adminauth.CookieName] == "" {
		t.Fatal("no panel session cookie was set")
	}
	if n := trail.count(ActionAdminLoginSucceeded); n != 0 {
		t.Errorf("the handler wrote %d admin.login.succeeded row(s); identity_open_session already wrote the one", n)
	}
	if trail.total() != 0 {
		t.Errorf("the handler wrote %d audit row(s) for an identity sign-in, want none", trail.total())
	}
	if len(admins.issuedFor) != 0 {
		t.Errorf("Issue was asked for %d session(s) on top of the identity's", len(admins.issuedFor))
	}
	if _, ok := b.cookies[adminChoiceCookieName]; ok {
		t.Error("a choice blob is left in the browser after an identity sign-in")
	}
}

// TestAdminLoginIdentity_TheAccountLimiterIsTheIdentitysBudget: the budget AuthenticatePerson meters
// the identity's failure rows with is the panel's own account limiter (ADR 0031 §5.6: adminAccountLimit
// keyed by identity_id on this path -- adminauth namespaces the key), and the sign-in names no
// business (uuid.Nil: K-4 lands it). MUTATION: hand it a fresh limiter -> red.
func TestAdminLoginIdentity_TheAccountLimiterIsTheIdentitysBudget(t *testing.T) {
	want := uuid.New()
	admins := &fakeAdmins{person: func(_, _ string, w uuid.UUID, _ adminauth.FailureBudget) (adminauth.PersonSignIn, error) {
		want = w
		return adminauth.PersonSignIn{}, adminauth.ErrBadCredentials
	}}
	a, h := newLoginAuth(t, admins, &fakeTrail{}, true)
	signInWith(t, newBrowser(t, h), "wrong")
	if len(admins.personBudgets) != 1 {
		t.Fatalf("AuthenticatePerson was called %d time(s), want 1", len(admins.personBudgets))
	}
	if admins.personBudgets[0] != adminauth.FailureBudget(a.accountLimiter) {
		t.Errorf("the identity's failure budget is %T %p, not the panel's account limiter %p",
			admins.personBudgets[0], admins.personBudgets[0], a.accountLimiter)
	}
	if want != uuid.Nil {
		t.Errorf("the sign-in asked for business %s; the form names none", want)
	}
}

// TestAdminLoginIdentity_NothingVerifiedIsAFailure: a sign-in that opened no identity session and
// verified no legacy row is the failure, even without ErrBadCredentials (AuthenticatePerson's
// contract makes the shape unreachable; the handler does not lean on it) -- never an empty picker,
// never a session. MUTATION: loginAsPerson's fallback removed -> 303 to the picker, red.
func TestAdminLoginIdentity_NothingVerifiedIsAFailure(t *testing.T) {
	admins := &fakeAdmins{person: func(string, string, uuid.UUID, adminauth.FailureBudget) (adminauth.PersonSignIn, error) {
		return adminauth.PersonSignIn{Legacy: legacyOf(adminauth.Attempt{AdminUserID: uuid.New(), TenantID: uuid.New()})}, nil
	}}
	_, h := newLoginAuth(t, admins, &fakeTrail{}, true)
	if rec := signInWith(t, newBrowser(t, h), "x"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("nothing verified and no error: %d to %q, want the 401 failure", rec.Code, rec.Header().Get("Location"))
	}
	if len(admins.issuedFor) != 0 {
		t.Fatal("a session was issued for nothing verified")
	}
}

// TestAdminLoginIdentity_EveryFailureIsTheSameBody: on the identity path every failure -- nothing
// verified anywhere, a wrong password on a legacy row, the right one on a disabled legacy row --
// is one status and one body, byte for byte in one browser; and that body is the flag-off failure's
// once the synchronizer token (the one named field) is normalised.
func TestAdminLoginIdentity_EveryFailureIsTheSameBody(t *testing.T) {
	tenant, admin := uuid.New(), uuid.New()
	outcomes := []adminauth.Authentication{
		{},
		legacyOf(adminauth.Attempt{AdminUserID: admin, TenantID: tenant, PasswordMatched: false, Active: true}),
		legacyOf(adminauth.Attempt{AdminUserID: admin, TenantID: tenant, PasswordMatched: true, Active: false}),
	}
	var current int
	admins := &fakeAdmins{person: func(string, string, uuid.UUID, adminauth.FailureBudget) (adminauth.PersonSignIn, error) {
		return adminauth.PersonSignIn{Legacy: outcomes[current]}, adminauth.ErrBadCredentials
	}}
	_, h := newLoginAuth(t, admins, &fakeTrail{}, true)
	b := newBrowser(t, h)
	csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, "/admin/login", nil)))
	var bodies []string
	for current = range outcomes {
		rec := b.do(http.MethodPost, "/admin/login", url.Values{"csrf": {csrf}, "email": {"person@example.test"}, "password": {"x"}})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("outcome %d: status %d, want 401", current, rec.Code)
		}
		bodies = append(bodies, htmlOf(t, rec))
	}
	for i := 1; i < len(bodies); i++ {
		if bodies[i] != bodies[0] {
			t.Fatalf("failure %d differs from failure 0 in one browser (%d vs %d bytes)", i, len(bodies[i]), len(bodies[0]))
		}
	}
	if !strings.Contains(bodies[0], "We could not sign you in") {
		t.Fatalf("the failure body carries no failure notice")
	}

	off := &fakeAdmins{}
	_, hOff := newLoginAuth(t, off, &fakeTrail{}, false)
	offBody := htmlOf(t, signInWith(t, newBrowser(t, hOff), "x"))
	norm := func(s string) string { return strings.ReplaceAll(s, csrfFrom(t, s), "<CSRF>") }
	if norm(offBody) != norm(bodies[0]) {
		t.Fatalf("the identity path's failure body differs from today's beyond the synchronizer token")
	}
}

// TestAdminLoginIdentity_LegacyRowsSignInAsToday: no identity session, legacy rows verified (§5.6's
// transition arm): one row -> Issue and the handler's own success row; two -> the picker, and the
// choice issues the chosen row. K-4's "no picker" is the identity path's alone (bulgu 10).
func TestAdminLoginIdentity_LegacyRowsSignInAsToday(t *testing.T) {
	a := adminauth.Attempt{AdminUserID: uuid.New(), TenantID: uuid.New(), PasswordMatched: true, Active: true}
	c := adminauth.Attempt{AdminUserID: uuid.New(), TenantID: uuid.New(), PasswordMatched: true, Active: true}

	t.Run("one row", func(t *testing.T) {
		admins := &fakeAdmins{person: func(string, string, uuid.UUID, adminauth.FailureBudget) (adminauth.PersonSignIn, error) {
			return adminauth.PersonSignIn{Legacy: legacyOf(a)}, nil
		}}
		trail := &fakeTrail{}
		_, h := newLoginAuth(t, admins, trail, true)
		rec := signInWith(t, newBrowser(t, h), "right")
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin" {
			t.Fatalf("one legacy row: %d to %q, want 303 to /admin", rec.Code, rec.Header().Get("Location"))
		}
		if len(admins.issuedFor) != 1 || admins.issuedFor[0].AdminUserID != a.AdminUserID {
			t.Fatalf("Issue was asked for %+v, want the one legacy row", admins.issuedFor)
		}
		if n := trail.count(ActionAdminLoginSucceeded); n != 1 {
			t.Errorf("%d admin.login.succeeded rows for a legacy sign-in, want the handler's one", n)
		}
	})

	t.Run("two rows", func(t *testing.T) {
		admins := &fakeAdmins{person: func(string, string, uuid.UUID, adminauth.FailureBudget) (adminauth.PersonSignIn, error) {
			return adminauth.PersonSignIn{Legacy: legacyOf(a, c)}, nil
		}}
		_, h := newLoginAuth(t, admins, &fakeTrail{}, true)
		b := newBrowser(t, h)
		rec := signInWith(t, b, "right")
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login/choose" {
			t.Fatalf("two legacy rows: %d to %q, want 303 to the picker", rec.Code, rec.Header().Get("Location"))
		}
		page := htmlOf(t, b.do(http.MethodGet, "/admin/login/choose", nil))
		for _, at := range []adminauth.Attempt{a, c} {
			if !strings.Contains(page, at.TenantID.String()) {
				t.Fatalf("the picker does not offer %s", at.TenantID)
			}
		}
		rec = b.do(http.MethodPost, "/admin/login/choose", url.Values{"csrf": {csrfFrom(t, page)}, "tenant_id": {c.TenantID.String()}})
		if rec.Code != http.StatusSeeOther || len(admins.issuedFor) != 1 || admins.issuedFor[0].TenantID != c.TenantID {
			t.Fatalf("the choice: %d, issued %+v; want the chosen row", rec.Code, admins.issuedFor)
		}
	})
}

// TestAdminLogin_ALapsedSignInAsksToSignInAgain: Issue's three lapses -- the stale proof, no row to
// write for, a lost deadlock -- answer "Please sign in again" (409), set no session cookie and clear
// the short-lived ones, with the flag off and on (completeLogin is shared); any other failure is
// still the 500. On the identity path a deadlock AuthenticatePerson returns is the same lapse. The
// M14-07 audit's carry-over. MUTATION: completeLogin's SignInLapsed branch removed -> the three are
// 500; the deadlock dropped from SignInLapsed -> that row is red.
func TestAdminLogin_ALapsedSignInAsksToSignInAgain(t *testing.T) {
	deadlock := &pgconn.PgError{Code: "40P01", Message: "deadlock detected"}
	cases := []struct {
		name   string
		err    error
		status int
	}{
		{"stale proof", fmt.Errorf("adminauth: issue: %w", adminauth.ErrProofStale), http.StatusConflict},
		{"no row to write for", fmt.Errorf("adminauth: issue: %w", pgx.ErrNoRows), http.StatusConflict},
		{"a lost deadlock", fmt.Errorf("adminauth: issue: %w", deadlock), http.StatusConflict},
		{"CONTROL: the database failing", errors.New("adminauth: issue: connection reset"), http.StatusInternalServerError},
	}
	row := adminauth.Attempt{AdminUserID: uuid.New(), TenantID: uuid.New(), PasswordMatched: true, Active: true}
	for _, on := range []bool{false, true} {
		for _, c := range cases {
			t.Run(fmt.Sprintf("flag %v, %s", on, c.name), func(t *testing.T) {
				admins := &fakeAdmins{
					authenticate: func(string, string) (adminauth.Authentication, error) { return legacyOf(row), nil },
					person: func(string, string, uuid.UUID, adminauth.FailureBudget) (adminauth.PersonSignIn, error) {
						return adminauth.PersonSignIn{Legacy: legacyOf(row)}, nil
					},
					issue: func(adminauth.Verified) (adminauth.Issued, error) { return adminauth.Issued{}, c.err },
				}
				_, h := newLoginAuth(t, admins, &fakeTrail{}, on)
				b := newBrowser(t, h)
				rec := signInWith(t, b, "right")
				checkSignInAgain(t, b, rec, c.status)
			})
		}
	}
	t.Run("identity path, AuthenticatePerson lost a deadlock", func(t *testing.T) {
		admins := &fakeAdmins{person: func(string, string, uuid.UUID, adminauth.FailureBudget) (adminauth.PersonSignIn, error) {
			return adminauth.PersonSignIn{}, fmt.Errorf("adminauth: authenticate person: %w", deadlock)
		}}
		_, h := newLoginAuth(t, admins, &fakeTrail{}, true)
		b := newBrowser(t, h)
		checkSignInAgain(t, b, signInWith(t, b, "right"), http.StatusConflict)
	})
}

// TestAdminLoginIdentity_AnAbandonedSessionIsToldTheTruth (M14-09's audit, DÜŞÜK-1): the identity
// session was opened and the sign-in failed after it -- here as a lost deadlock, which BEFORE the open
// would be "sign in again". The answer is the 500 that does not claim "nothing was changed", no session
// cookie is set, and the business gets one failed-sign-in row naming the same session and whether its
// revocation held (completeLogin's session_cookie_failed shape). MUTATION: loginAsPerson's Abandoned
// branch removed -> the 409 "sign in again" and no row, red.
func TestAdminLoginIdentity_AnAbandonedSessionIsToldTheTruth(t *testing.T) {
	for _, revoked := range []bool{true, false} {
		tenant, admin, session := uuid.New(), uuid.New(), uuid.New()
		admins := &fakeAdmins{person: func(string, string, uuid.UUID, adminauth.FailureBudget) (adminauth.PersonSignIn, error) {
			return adminauth.PersonSignIn{Abandoned: &adminauth.AbandonedSession{
				Session: adminauth.Session{ID: session, TenantID: tenant, AdminUserID: admin},
				Revoked: revoked,
			}}, fmt.Errorf("adminauth: authenticate person: %w", &pgconn.PgError{Code: "40P01"})
		}}
		trail := &fakeTrail{}
		_, h := newLoginAuth(t, admins, trail, true)
		b := newBrowser(t, h)
		rec := signInWith(t, b, "right")
		body := htmlOf(t, rec)
		if rec.Code != http.StatusInternalServerError || !strings.Contains(body, problemAdminSignInAbandoned.Title) {
			t.Fatalf("revoked=%v: %d; want 500 and %q", revoked, rec.Code, problemAdminSignInAbandoned.Title)
		}
		if strings.Contains(body, "nothing was changed") {
			t.Errorf("revoked=%v: the answer claims nothing was changed; the session and its row were committed", revoked)
		}
		if b.cookies[adminauth.CookieName] != "" {
			t.Errorf("revoked=%v: a panel session cookie was set", revoked)
		}
		events := trail.eventsSnapshot()
		if len(events) != 1 || events[0].Action != ActionAdminLoginFailed || events[0].TenantID != tenant {
			t.Fatalf("revoked=%v: trail %+v; want one failed-sign-in row in the session's business", revoked, events)
		}
		d, ok := events[0].Detail.(adminLoginDetail)
		if !ok || d.Outcome != "session_abandoned" || d.SessionID != session.String() || strings.Contains(d.Reason, "failed") == revoked {
			t.Errorf("revoked=%v: detail %+v", revoked, events[0].Detail)
		}
	}
}

func checkSignInAgain(t *testing.T, b *browser, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status %d, want %d", rec.Code, status)
	}
	if b.cookies[adminauth.CookieName] != "" {
		t.Fatal("a panel session cookie was set")
	}
	body := htmlOf(t, rec)
	if status != http.StatusConflict {
		if strings.Contains(body, problemAdminSignInAgain.Title) {
			t.Fatal("CONTROL: a database failure is told to sign in again")
		}
		return
	}
	if !strings.Contains(body, problemAdminSignInAgain.Title) {
		t.Fatalf("the answer is not the sign-in-again problem")
	}
	if _, ok := b.cookies[adminLoginCookieName]; ok {
		t.Error("the sign-in's short-lived cookie was left behind")
	}
}

// r21Keys are the audit detail keys that tell a business about the person's other businesses
// (ADR 0031 §5.6, R21): VerifiedBusiness, Resolved, Compared, AttemptedTenant.
var r21Keys = []string{`"verified_businesses"`, `"resolved"`, `"compared"`, `"attempted_tenant"`}

// r21Words are the same counts in words: the flag-off reason texts that speak of businesses in the
// plural (M14-09's audit, DÜŞÜK-2). Matched lower-cased against a row's detail; the identity path's
// reasons name no business at all.
var r21Words = []string{"businesses", "every business", "verified set"}

// TestAdminLoginIdentity_AuditRowsNameNoOtherBusiness is R21's text scan, over every row of the flow
// that can carry one of the four fields: a truncated legacy sign-in's probe row, a choice refused
// for a business outside the verified set, the picker whose every business was disabled, and the
// legacy failures. Flag on: none of the four keys and none of the plural reason texts (r21Words) in
// any row. CONTROL, flag off: the same flows write all of them (the scan sees them). MUTATIONS:
// loginDetail returns its argument unchanged -> red; loginDetail keeps the reason -> red.
func TestAdminLoginIdentity_AuditRowsNameNoOtherBusiness(t *testing.T) {
	rows := make([]adminauth.Attempt, adminauth.MaxCandidates)
	for i := range rows {
		rows[i] = adminauth.Attempt{AdminUserID: uuid.New(), TenantID: uuid.New(), PasswordMatched: i < 2, Active: true}
	}
	truncated := adminauth.Authentication{Resolved: adminauth.MaxCandidates + 1, Attempts: rows}
	for _, on := range []bool{true, false} {
		trail := &fakeTrail{}
		var outcome adminauth.Authentication
		var outErr error
		var noChoices bool
		admins := &fakeAdmins{
			authenticate: func(string, string) (adminauth.Authentication, error) { return outcome, outErr },
			person: func(string, string, uuid.UUID, adminauth.FailureBudget) (adminauth.PersonSignIn, error) {
				return adminauth.PersonSignIn{Legacy: outcome}, outErr
			},
			choices: func(v []adminauth.Verified) ([]adminauth.Choice, error) {
				if noChoices {
					return nil, nil
				}
				out := make([]adminauth.Choice, 0, len(v))
				for _, one := range v {
					out = append(out, adminauth.Choice{AdminUserID: one.AdminUserID, TenantID: one.TenantID, TenantName: "B", Role: "owner"})
				}
				return out, nil
			},
		}
		_, h := newLoginAuth(t, admins, trail, on)

		// 1. a truncated legacy sign-in, two rows verified: the picker, and the probe row.
		outcome, outErr = truncated, nil
		b := newBrowser(t, h)
		if rec := signInWith(t, b, "right"); rec.Code != http.StatusSeeOther {
			t.Fatalf("flag %v: truncated sign-in %d", on, rec.Code)
		}
		// 2. a choice outside the verified set: refused, with its row.
		page := htmlOf(t, b.do(http.MethodGet, "/admin/login/choose", nil))
		if rec := b.do(http.MethodPost, "/admin/login/choose", url.Values{"csrf": {csrfFrom(t, page)}, "tenant_id": {uuid.NewString()}}); rec.Code != http.StatusBadRequest {
			t.Fatalf("flag %v: a refused choice answered %d", on, rec.Code)
		}
		// 3. the picker whose every business was disabled before the choice.
		b = newBrowser(t, h)
		signInWith(t, b, "right")
		noChoices = true
		b.do(http.MethodGet, "/admin/login/choose", nil)
		noChoices = false
		// 4. the legacy failures.
		outcome, outErr = legacyOf(rows[2], rows[3]), adminauth.ErrBadCredentials
		signInWith(t, newBrowser(t, h), "wrong")

		for _, action := range []string{ActionAdminLoginTruncated, ActionAdminLoginRefused, ActionAdminLoginFailed} {
			if trail.count(action) == 0 {
				t.Fatalf("flag %v: no %s row -- the scan below would be vacuous", on, action)
			}
		}
		seen := map[string]bool{}
		for _, e := range trail.eventsSnapshot() {
			raw, err := json.Marshal(e.Detail)
			if err != nil {
				t.Fatal(err)
			}
			for _, k := range append(append([]string(nil), r21Keys...), r21Words...) {
				if strings.Contains(strings.ToLower(string(raw)), k) {
					seen[k] = true
					if on {
						t.Errorf("identity path: %s row carries %s: %s", e.Action, k, raw)
					}
				}
			}
		}
		if !on && len(seen) != len(r21Keys)+len(r21Words) {
			t.Fatalf("CONTROL: with the flag off the flows wrote only %v of %v and %v -- the scan cannot see what it forbids", seen, r21Keys, r21Words)
		}
	}
}

// TestSignInLapsed_IsTheThreeLapsesAndNothingElse pins adminauth.SignInLapsed's set from the
// handler's side: wrapped or joined, the three lapses are recognised; a context error, a unique
// violation and a plain failure are not.
func TestSignInLapsed_IsTheThreeLapsesAndNothingElse(t *testing.T) {
	for _, c := range []struct {
		err  error
		want bool
	}{
		{fmt.Errorf("x: %w", adminauth.ErrProofStale), true},
		{fmt.Errorf("x: %w", pgx.ErrNoRows), true},
		{errors.Join(errors.New("revoke"), fmt.Errorf("x: %w", &pgconn.PgError{Code: "40P01"})), true},
		{&pgconn.PgError{Code: "23505"}, false},
		{fmt.Errorf("x: %w", context.Canceled), false},
		{adminauth.ErrBadCredentials, false},
		{errors.New("boom"), false},
	} {
		if got := adminauth.SignInLapsed(c.err); got != c.want {
			t.Errorf("SignInLapsed(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}
