package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
)

// accountpassword_test.go -- change-my-own-password, the handler side (M7-05, T73).
//
// WHAT EACH BLOCK PROVES, mapped to the task's acceptance criteria:
//
//	K-A (not owner-only)   TestAccountPasswordSave_ManagerMayChangeOwnPassword
//	                       (a manager, non-owner, succeeds -- and this is also the
//	                        handler's "success -> 303 + audit changed" case; that the
//	                        stored DIGEST actually moves is the Manager DB test's job,
//	                        adminauth/changepassword_db_test.go, because here
//	                        ChangeOwnPassword is a double)
//	unauth refused         TestAccountPasswordSave_UnauthenticatedIsRefused
//	client-side policy      TestAccountPasswordSave_WeakOrMismatchedIsNotWritten
//	credential refusals    TestAccountPasswordSave_CredentialRefusalReRendersAndAudits
//	cross-origin refused   TestAccountPasswordSave_CrossOriginIsRefusedBeforeTheHandler
//	no secret leaks        TestAccountPasswordSave_NeverLeaksTheCredential

// newPasswordAuth builds an AdminAuth whose panel identity resolves to a live admin of
// the given role. It takes the *fakeAdmins so a test can set the ChangeOwnPassword hook
// and read back the arguments the handler passed, and an optional log writer so the leak
// test can read what was logged.
func newPasswordAuth(t *testing.T, admins *fakeAdmins, trail *fakeTrail, role string, logw io.Writer) *AdminAuth {
	t.Helper()
	if admins.verify == nil {
		admins.verify = func() (adminauth.Resolved, error) {
			return adminauth.Resolved{
				SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
				Role: role, FullName: "Admin Person",
			}, nil
		}
	}
	logger := slog.New(slog.DiscardHandler)
	if logw != nil {
		logger = slog.New(slog.NewJSONHandler(logw, nil))
	}
	h, err := NewAdminAuth(admins, trail, newFakeLedger(), newFakeLedger(), &fakeReviewer{},
		&fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(),
		newFakeScribe(), newFakeBooks(), newFakeTexts(), newFakeAccount(), nil, adminTestConfig(), logger)
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	return h
}

// passwordBrowser mounts newPasswordAuth behind the real router (so the write chain --
// floodGate, sameOriginGate, requireAdmin, sessionGate -- is exercised) and returns a
// browser already carrying a panel cookie.
func passwordBrowser(t *testing.T, admins *fakeAdmins, trail *fakeTrail, role string, logw io.Writer) *browser {
	t.Helper()
	h := newPasswordAuth(t, admins, trail, role, logw)
	r := chi.NewRouter()
	h.Mount(r)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

// validChange is a form body whose new password passes checkNewPassword, so the request
// reaches ChangeOwnPassword.
func validChange(current, next string) url.Values {
	return url.Values{
		"current_password": {current},
		"new_password":     {next},
		"confirm_password": {next},
	}
}

// TestAccountPasswordSave_ManagerMayChangeOwnPassword is the K-A proof: a MANAGER (not an
// owner) changes their own password. accountSave is owner-only; this must NOT be, because
// a password is personal. It doubles as the success path -- 303, the done=password flash,
// and one admin.password_changed row carrying the K3 revocation count and nothing else.
func TestAccountPasswordSave_ManagerMayChangeOwnPassword(t *testing.T) {
	admins := &fakeAdmins{changePassword: func(_, _, _ uuid.UUID, _, _ string) (int, error) { return 2, nil }}
	trail := &fakeTrail{}
	b := passwordBrowser(t, admins, trail, accountTestManagerRole, nil)

	rec := b.do(http.MethodPost, accountPasswordHref, validChange("the-current-password", "a-brand-new-password"))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("a manager POST: %d, want 303 -- a manager may change their OWN password (K-A)", rec.Code)
	}
	if got := rec.Header().Get("Location"); !strings.Contains(got, "done=password") {
		t.Errorf("success sent to %q, want the done=password flash", got)
	}
	if n := admins.changeCalls(); n != 1 {
		t.Fatalf("ChangeOwnPassword reached %d time(s), want 1", n)
	}

	// §4.5 and K3: the tenant, admin and except-session all come from the SESSION.
	args := admins.lastChange(t)
	if args.tenantID != panelTestTenant || args.adminID != panelTestAdmin || args.exceptSessionID != panelTestSession {
		t.Errorf("ChangeOwnPassword got tenant/admin/except %s/%s/%s, want the session's %s/%s/%s",
			args.tenantID, args.adminID, args.exceptSessionID, panelTestTenant, panelTestAdmin, panelTestSession)
	}

	if n := trail.count(ActionAdminPasswordChanged); n != 1 {
		t.Fatalf("the trail holds %d %s row(s), want 1", n, ActionAdminPasswordChanged)
	}
	e := trail.eventsSnapshot()[0]
	if e.TenantID != panelTestTenant {
		t.Errorf("the row names tenant %s, want the SESSION's %s (§4.5)", e.TenantID, panelTestTenant)
	}
	if e.Target != panelTestAdmin.String() {
		t.Errorf("the row targets %q, want the admin themselves (a password event is about a person)", e.Target)
	}
	detail, ok := e.Detail.(passwordChangedDetail)
	if !ok {
		t.Fatalf("the changed detail is %T, want passwordChangedDetail", e.Detail)
	}
	if detail.RevokedOtherSessions != 2 {
		t.Errorf("the row records revoked_other_sessions=%d, want 2", detail.RevokedOtherSessions)
	}
}

// TestAccountPasswordSave_UnauthenticatedIsRefused drives the handler's OWN belt (id.Live())
// by calling it with no admin identity in the request context. The write chain would
// normally refuse this at the resolver, but the belt must hold on its own.
func TestAccountPasswordSave_UnauthenticatedIsRefused(t *testing.T) {
	admins := &fakeAdmins{}
	trail := &fakeTrail{}
	h := newPasswordAuth(t, admins, trail, accountTestManagerRole, nil)

	body := validChange("whatever-current", "a-valid-new-password").Encode()
	req := httptest.NewRequest(http.MethodPost, accountPasswordHref, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.accountPasswordSave(rec, req) // no identity in context -> id.Live() is false

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("an unauthenticated POST: %d, want 401", rec.Code)
	}
	if n := admins.changeCalls(); n != 0 {
		t.Errorf("ChangeOwnPassword reached %d time(s) with no session; want 0", n)
	}
	if n := trail.total(); n != 0 {
		t.Errorf("an unauthenticated refusal wrote %d audit row(s); want 0 (no tenant to attribute)", n)
	}
}

// TestAccountPasswordSave_WeakOrMismatchedIsNotWritten: a client-side policy failure
// re-renders at 200 and reaches neither the credential nor the audit trail -- it has not
// touched anything yet, exactly as the reset flow treats its own checkNewPassword refusal.
func TestAccountPasswordSave_WeakOrMismatchedIsNotWritten(t *testing.T) {
	for _, tc := range []struct{ name, next, confirm string }{
		{"too short", "short", "short"},
		{"mismatch", "a-valid-new-password", "a-different-password"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			admins := &fakeAdmins{}
			trail := &fakeTrail{}
			b := passwordBrowser(t, admins, trail, accountTestOwnerRole, nil)

			rec := b.do(http.MethodPost, accountPasswordHref, url.Values{
				"current_password": {"the-current-password"},
				"new_password":     {tc.next},
				"confirm_password": {tc.confirm},
			})
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: %d, want a 200 re-render", tc.name, rec.Code)
			}
			if n := admins.changeCalls(); n != 0 {
				t.Errorf("a client-side policy failure reached ChangeOwnPassword %d time(s); want 0", n)
			}
			if n := trail.total(); n != 0 {
				t.Errorf("a client-side policy failure wrote %d audit row(s); want 0", n)
			}
		})
	}
}

// TestAccountPasswordSave_CredentialRefusalReRendersAndAudits covers the two sentinels the
// credential check produces: each re-renders at 200 with its own message and writes ONE
// admin.password_change_refused row carrying a fixed reason token and never the password.
func TestAccountPasswordSave_CredentialRefusalReRendersAndAudits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		reason  string
		message string
	}{
		{"wrong current", adminauth.ErrWrongCurrentPassword, "wrong_current_password", "Your current password is not right."},
		{"same as current", adminauth.ErrSameAsCurrentPassword, "same_as_current", "New password must be different from your current one."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			admins := &fakeAdmins{changePassword: func(_, _, _ uuid.UUID, _, _ string) (int, error) {
				return 0, tc.err
			}}
			trail := &fakeTrail{}
			b := passwordBrowser(t, admins, trail, accountTestOwnerRole, nil)

			rec := b.do(http.MethodPost, accountPasswordHref, validChange("some-current", "a-valid-new-password"))
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: %d, want a 200 re-render", tc.name, rec.Code)
			}
			if html := htmlOf(t, rec); !strings.Contains(html, tc.message) {
				t.Errorf("the re-render does not carry %q", tc.message)
			}
			if n := trail.count(ActionAdminPasswordChangeRefused); n != 1 {
				t.Fatalf("the trail holds %d refusal row(s), want 1", n)
			}
			if n := trail.count(ActionAdminPasswordChanged); n != 0 {
				t.Errorf("a refusal wrote a %s row; it must not", ActionAdminPasswordChanged)
			}
			e := trail.eventsSnapshot()[0]
			detail, ok := e.Detail.(passwordRefusedDetail)
			if !ok {
				t.Fatalf("the refusal detail is %T, want passwordRefusedDetail", e.Detail)
			}
			if detail.Reason != tc.reason {
				t.Errorf("the refusal records reason=%q, want %q", detail.Reason, tc.reason)
			}
		})
	}
}

// TestAccountPasswordSave_CrossOriginIsRefusedBeforeTheHandler: sameOriginGate refuses a
// cross-origin POST AHEAD of the resolver and the handler, so no credential work and no
// audit row happen.
func TestAccountPasswordSave_CrossOriginIsRefusedBeforeTheHandler(t *testing.T) {
	admins := &fakeAdmins{changePassword: func(_, _, _ uuid.UUID, _, _ string) (int, error) { return 1, nil }}
	trail := &fakeTrail{}
	b := passwordBrowser(t, admins, trail, accountTestOwnerRole, nil)
	b.origin = "https://evil.example"

	rec := b.do(http.MethodPost, accountPasswordHref, validChange("the-current-password", "a-valid-new-password"))
	if n := admins.changeCalls(); n != 0 {
		t.Errorf("a cross-origin POST reached ChangeOwnPassword %d time(s); the Origin gate must refuse it first", n)
	}
	if n := trail.total(); n != 0 {
		t.Errorf("a cross-origin POST wrote %d audit row(s); want 0", n)
	}
	if got := rec.Header().Get("Location"); got == accountReturn("password", "") {
		t.Errorf("a cross-origin POST was treated as a success (Location %q)", got)
	}
}

// TestAccountPasswordSave_NeverLeaksTheCredential drives BOTH a success and a refusal and
// proves the two passwords appear in NEITHER the audit rows' detail NOR the log output.
func TestAccountPasswordSave_NeverLeaksTheCredential(t *testing.T) {
	const current = "s3kret-current-PW-777"
	const next = "s3kret-NEW-PW-8888888"

	var logbuf bytes.Buffer
	admins := &fakeAdmins{changePassword: func(_, _, _ uuid.UUID, _, _ string) (int, error) { return 1, nil }}
	trail := &fakeTrail{}
	b := passwordBrowser(t, admins, trail, accountTestOwnerRole, &logbuf)

	// A success (writes admin.password_changed and logs "panel password changed").
	if rec := b.do(http.MethodPost, accountPasswordHref, validChange(current, next)); rec.Code != http.StatusSeeOther {
		t.Fatalf("success POST: %d, want 303", rec.Code)
	}
	// A refusal (writes admin.password_change_refused and logs the refusal). ServeHTTP is
	// synchronous and single-goroutine, so reassigning the hook here is race-free.
	admins.changePassword = func(_, _, _ uuid.UUID, _, _ string) (int, error) {
		return 0, adminauth.ErrWrongCurrentPassword
	}
	b.do(http.MethodPost, accountPasswordHref, validChange(current, next))

	secrets := []string{current, next}

	// (1) audit detail
	for _, e := range trail.eventsSnapshot() {
		blob, err := json.Marshal(e.Detail)
		if err != nil {
			t.Fatalf("marshal %s detail: %v", e.Action, err)
		}
		for _, s := range secrets {
			if strings.Contains(string(blob), s) {
				t.Errorf("the %s audit detail leaks a password: %s", e.Action, blob)
			}
		}
	}

	// (2) log output
	logs := logbuf.String()
	for _, s := range secrets {
		if strings.Contains(logs, s) {
			t.Errorf("the log output leaks a password")
		}
	}
}
