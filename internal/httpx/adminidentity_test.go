package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
)

type fakeAdminVerifier struct {
	res adminauth.Resolved
	err error
}

func (f fakeAdminVerifier) Verify(context.Context, adminauth.Token) (adminauth.Resolved, error) {
	return f.res, f.err
}

// TestRequireAdmin_Table walks every state the panel boundary can be in.
//
// THE POLARITY IS THE POINT: only ONE row reaches the protected handler, and every
// way of failing to resolve — including "nobody looked" and "the database is down"
// — lands on the refusal. A gate whose failure mode is "let them in" is worse than
// no gate.
func TestRequireAdmin_Table(t *testing.T) {
	live := adminauth.Resolved{
		SessionID: uuid.New(), TenantID: uuid.New(), AdminUserID: uuid.New(),
		Role: "owner", FullName: "KF Owner",
	}

	tests := []struct {
		name        string
		cookie      string
		verifier    AdminVerifier
		wantThrough bool
		wantState   AdminState
	}{
		{"no cookie", "", fakeAdminVerifier{err: adminauth.ErrNoSession}, false, AdminAbsent},
		{"an empty cookie", "", fakeAdminVerifier{err: adminauth.ErrNoSession}, false, AdminAbsent},
		{"a cookie that resolves to nothing", "abc", fakeAdminVerifier{err: adminauth.ErrNoSession}, false, AdminAbsent},
		{"a database failure", "abc", fakeAdminVerifier{err: errors.New("connection refused")}, false, AdminUnresolved},
		{"no verifier wired at all", "abc", nil, false, AdminUnresolved},
		{"a live session", "abc", fakeAdminVerifier{res: live}, true, AdminLive},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var reached bool
			var seen AdminIdentity
			refused := false

			next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				reached = true
				seen = AdminOf(r)
			})
			onFail := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				refused = true
				seen = AdminOf(r)
				w.WriteHeader(http.StatusSeeOther)
			})

			h := RequireAdmin(adminauth.Cookies{}, tc.verifier, onFail)(next)
			req := httptest.NewRequest(http.MethodGet, adminauth.CookiePath, nil)
			if tc.cookie != "" {
				req.AddCookie(&http.Cookie{Name: adminauth.CookieName, Value: tc.cookie})
			}
			h.ServeHTTP(httptest.NewRecorder(), req)

			if reached != tc.wantThrough {
				t.Fatalf("reached the protected handler = %v, want %v", reached, tc.wantThrough)
			}
			if refused == tc.wantThrough {
				t.Fatalf("refused = %v while reached = %v", refused, reached)
			}
			if seen.State != tc.wantState {
				t.Fatalf("state = %v, want %v", seen.State, tc.wantState)
			}
			if tc.wantThrough {
				if seen.TenantID() != live.TenantID || seen.AdminUserID() != live.AdminUserID {
					t.Fatalf("identity = %+v, want %+v", seen.Admin, live)
				}
			} else if seen.TenantID() != uuid.Nil || seen.AdminUserID() != uuid.Nil {
				t.Fatalf("a refused request carries ids: %+v", seen)
			}
			// The identity is in the context on BOTH branches, so the refusal
			// handler can log an outage without telling the visitor about it.
			if tc.wantState == AdminUnresolved && seen.Err == nil {
				t.Fatalf("AdminUnresolved carries no Err — a caller cannot tell 'signed out' from 'cannot tell'")
			}
		})
	}
}

// TestRequireAdmin_NilRefusalHandlerStillRefuses. A mis-wired gate must fail
// CLOSED: the fallback is a 401, never a pass-through.
func TestRequireAdmin_NilRefusalHandlerStillRefuses(t *testing.T) {
	var reached bool
	h := RequireAdmin(adminauth.Cookies{}, fakeAdminVerifier{err: adminauth.ErrNoSession}, nil)(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, adminauth.CookiePath, nil))

	if reached {
		t.Fatalf("a nil refusal handler let the request through")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", rec.Code)
	}
}
