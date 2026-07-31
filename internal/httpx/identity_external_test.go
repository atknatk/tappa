package httpx_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/session"
)

// The real producer must satisfy the consumer-side interface. This is a
// compile-time check rather than a runtime one because constructing a Manager
// needs a database, and what can go wrong here is a SIGNATURE drifting — which
// the compiler catches for free.
var _ httpx.SessionVerifier = (*session.Manager)(nil)

// fakeVerifier answers with whatever the test needs, including the shape
// session.Manager.Verify actually produces on the revoked branch: a POPULATED
// Resolved alongside ErrRevoked.
type fakeVerifier struct {
	res session.Resolved
	err error
}

func (f fakeVerifier) Verify(context.Context, session.Token) (session.Resolved, error) {
	return f.res, f.err
}

func withCookie(r *http.Request) *http.Request {
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: "some-opaque-cookie-value"})
	return r
}

func identityOf(t *testing.T, v httpx.SessionVerifier, r *http.Request) httpx.Identity {
	t.Helper()
	var got httpx.Identity
	h := httpx.Identify(session.Cookies{}, v)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = httpx.IdentityOf(r)
	}))
	h.ServeHTTP(httptest.NewRecorder(), r)
	return got
}

func TestIdentify(t *testing.T) {
	t.Parallel()
	tenant, employee, sess := uuid.New(), uuid.New(), uuid.New()
	revokedAt := time.Now()
	live := session.Resolved{ID: sess, TenantID: tenant, EmployeeID: employee}
	revoked := session.Resolved{ID: sess, TenantID: tenant, EmployeeID: employee, RevokedAt: &revokedAt}

	tests := []struct {
		name       string
		verifier   httpx.SessionVerifier
		cookie     bool
		wantState  httpx.SessionState
		wantLive   bool
		wantTenant uuid.UUID
		wantErr    bool
	}{
		{
			name:      "no cookie is §5 row 3, not a failure",
			verifier:  fakeVerifier{err: session.ErrNoSession},
			cookie:    false,
			wantState: httpx.SessionAbsent,
		},
		{
			name:      "unknown or malformed cookie is indistinguishable from none",
			verifier:  fakeVerifier{err: session.ErrNoSession},
			cookie:    true,
			wantState: httpx.SessionAbsent,
		},
		{
			name:       "live session carries tenant and employee",
			verifier:   fakeVerifier{res: live},
			cookie:     true,
			wantState:  httpx.SessionLive,
			wantLive:   true,
			wantTenant: tenant,
		},
		{
			// The M5-01 API trap. `if err != nil { activation }` would send this
			// request to §5 row 3 and write NO record; row 4 wants the attempt
			// recorded, so the facts must survive the error.
			name:       "revoked session keeps its facts",
			verifier:   fakeVerifier{res: revoked, err: session.ErrRevoked},
			cookie:     true,
			wantState:  httpx.SessionRevoked,
			wantLive:   false,
			wantTenant: tenant,
		},
		{
			// An outage is not "logged out". Turning it into SessionAbsent would
			// answer a database failure with a stream of activation redirects and
			// no records at all.
			name:      "database failure is unresolved, never absent",
			verifier:  fakeVerifier{err: errors.New("connection refused")},
			cookie:    true,
			wantState: httpx.SessionUnresolved,
			wantErr:   true,
		},
		{
			name:      "a nil verifier fails closed",
			verifier:  nil,
			cookie:    true,
			wantState: httpx.SessionUnresolved,
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodGet, "/t", nil)
			if tc.cookie {
				r = withCookie(r)
			}
			got := identityOf(t, tc.verifier, r)
			if got.State != tc.wantState {
				t.Fatalf("state = %v, want %v", got.State, tc.wantState)
			}
			if got.Live() != tc.wantLive {
				t.Fatalf("Live() = %v, want %v", got.Live(), tc.wantLive)
			}
			if got.TenantID() != tc.wantTenant {
				t.Fatalf("TenantID() = %v, want %v", got.TenantID(), tc.wantTenant)
			}
			if (got.Err != nil) != tc.wantErr {
				t.Fatalf("Err = %v, want error: %v", got.Err, tc.wantErr)
			}
		})
	}
}

// TestIdentity_ZeroValueIsUnresolvedNotAbsent is the M5-01 lesson applied to
// this type: the dangerous state must not be the one you get by doing nothing.
//
// If the zero value meant "no session", then a route that forgot to mount
// Identify would look exactly like a genuine session-less tap and take §5 row 3
// — activation page, NO RECORD. Every way of obtaining an Identity without the
// middleware is tried here, from outside the package.
func TestIdentity_ZeroValueIsUnresolvedNotAbsent(t *testing.T) {
	t.Parallel()
	var declared httpx.Identity
	zeros := map[string]httpx.Identity{
		"var":     declared,
		"literal": {},
		"unwritten field": struct {
			Other int
			ID    httpx.Identity
		}{Other: 1}.ID,
		"from a nil map": map[string]httpx.Identity{}["missing"],
		"from a slice":   make([]httpx.Identity, 1)[0],
	}
	for name, id := range zeros {
		if id.State != httpx.SessionUnresolved {
			t.Fatalf("%s: state = %v, want SessionUnresolved", name, id.State)
		}
		if id.Live() {
			t.Fatalf("%s: the zero Identity reported a live session", name)
		}
		if id.TenantID() != uuid.Nil || id.EmployeeID() != uuid.Nil {
			t.Fatalf("%s: the zero Identity produced ids", name)
		}
	}
}

// TestIdentityOf_WithoutTheMiddleware: a handler reached without Identify must
// not be told "there is no session" — it must be told nobody looked.
func TestIdentityOf_WithoutTheMiddleware(t *testing.T) {
	t.Parallel()
	r := withCookie(httptest.NewRequest(http.MethodGet, "/t", nil))
	got := httpx.IdentityOf(r)
	if got.State != httpx.SessionUnresolved {
		t.Fatalf("state = %v, want SessionUnresolved", got.State)
	}
	if _, ok := httpx.SessionTenantID(r); ok {
		t.Fatal("SessionTenantID reported a tenant nobody resolved")
	}
}

// TestSessionTenantID is the N5 hand-off surface: M5-05 fills tap.Input from
// this, and must be able to tell "no tenant" from uuid.Nil.
func TestSessionTenantID(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	live := session.Resolved{ID: uuid.New(), TenantID: tenant, EmployeeID: uuid.New()}

	var withSession, withoutSession *http.Request
	h := httpx.Identify(session.Cookies{}, fakeVerifier{res: live})(
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { withSession = r }))
	h.ServeHTTP(httptest.NewRecorder(), withCookie(httptest.NewRequest(http.MethodGet, "/t", nil)))

	h2 := httpx.Identify(session.Cookies{}, fakeVerifier{err: session.ErrNoSession})(
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { withoutSession = r }))
	h2.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/t", nil))

	if id, ok := httpx.SessionTenantID(withSession); !ok || id != tenant {
		t.Fatalf("resolved tenant = %v (ok=%v), want %v", id, ok, tenant)
	}
	if id, ok := httpx.SessionTenantID(withoutSession); ok || id != uuid.Nil {
		t.Fatalf("session-less request reported tenant %v (ok=%v)", id, ok)
	}
}

// TestIdentify_NeverAnswersTheRequest: it is a fact carrier, not a gate. A
// middleware that refused a revoked session here would turn §5 row 4 (record +
// alert) into row 3 (redirect, no record) and lose the attempt (§4.6).
func TestIdentify_NeverAnswersTheRequest(t *testing.T) {
	t.Parallel()
	revokedAt := time.Now()
	for _, v := range []httpx.SessionVerifier{
		fakeVerifier{err: session.ErrNoSession},
		fakeVerifier{err: errors.New("db down")},
		fakeVerifier{res: session.Resolved{ID: uuid.New(), TenantID: uuid.New(), RevokedAt: &revokedAt}, err: session.ErrRevoked},
	} {
		reached := false
		w := httptest.NewRecorder()
		httpx.Identify(session.Cookies{}, v)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			reached = true
			w.WriteHeader(http.StatusTeapot)
		})).ServeHTTP(w, withCookie(httptest.NewRequest(http.MethodGet, "/t", nil)))
		if !reached {
			t.Fatal("the middleware answered the request itself")
		}
		if w.Code != http.StatusTeapot {
			t.Fatalf("status = %d, want the handler's own 418", w.Code)
		}
	}
}
