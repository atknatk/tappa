package httpx

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
)

// The PANEL's request boundary — the twin of identity.go, and deliberately NOT
// the same middleware.
//
// WHY IT IS A GATE WHERE Identify IS NOT. identity.go is emphatic that it "never
// writes a response and never refuses a request", because on the tap path the
// absence of a session is a DECISION with a record attached (section 5 rows 3 and
// 4) and a middleware that answered would collapse the two. None of that applies
// here: a panel page load by somebody who is not signed in has no attendance
// record to write, no employee to attribute anything to, and exactly one correct
// answer — the login page. Making it a gate is what lets M6-02 mount a dashboard
// route without every handler re-checking.
//
// WHAT IT STILL REFUSES TO DO: it does not authorise. Role (owner vs manager) is
// carried in AdminIdentity and read by whoever needs it; deciding what a role may
// do is internal/policy's job, and the M6-01 card only asks that the FIELD exist.

// AdminState says what a request's panel cookie resolved to.
//
// THE ZERO VALUE IS "UNRESOLVED", NOT "ABSENT" — the same polarity choice
// identity.go makes and for the same reason: the state you get by DOING NOTHING
// (a var, an unwritten struct field, a sub-router that forgot to mount the
// middleware, a hand-built request in a test) must be the harmless one. Here
// "harmless" means "not authenticated", and both AdminUnresolved and AdminAbsent
// are refused by RequireAdmin, so there is no zero value that authenticates.
type AdminState int

const (
	// AdminUnresolved is the zero value: nobody looked, or looking failed.
	// AdminIdentity.Err says which.
	AdminUnresolved AdminState = iota
	// AdminAbsent means the cookie was missing, malformed, unknown, revoked, or
	// belonged to an admin who is no longer active. The panel treats all five
	// identically (adminauth.ErrNoSession), because all five mean "sign in".
	AdminAbsent
	// AdminLive means the cookie resolved to a live session whose admin is active.
	AdminLive
)

// AdminIdentity is what the panel's request boundary learned about the caller.
type AdminIdentity struct {
	State AdminState
	// Admin is populated for AdminLive and empty otherwise.
	Admin adminauth.Resolved
	// Err is the failure behind AdminUnresolved (a database outage, typically) and
	// nil in every other state. It is carried rather than swallowed: a caller that
	// cannot tell "signed out" from "cannot tell" answers an outage with an
	// infinite redirect loop to a login page that will fail the same way.
	Err error
}

// Live reports whether the request carries a usable panel session. False for
// every other state INCLUDING AdminUnresolved, so a failure to resolve never
// reads as authenticated.
func (a AdminIdentity) Live() bool { return a.State == AdminLive }

// TenantID is the tenant this panel request acts in, uuid.Nil when none resolved.
//
// IT IS THE OUTPUT OF RESOLUTION, NEVER AN INPUT (ADR 0002 madde 7, and the
// alternative migration 00011 rejects at length). No header, path, subdomain or
// form field on the panel names a tenant; this value comes from the session row
// that the cookie's HASH matched, i.e. from a secret the caller must possess. A
// middleware that accepted a tenant claim would let the caller name its own
// tenant, which is the M5-03 failure section 4.5 exists to prevent.
func (a AdminIdentity) TenantID() uuid.UUID {
	if a.State != AdminLive {
		return uuid.Nil
	}
	return a.Admin.TenantID
}

// AdminUserID is the admin this request acts as, uuid.Nil when none resolved.
func (a AdminIdentity) AdminUserID() uuid.UUID {
	if a.State != AdminLive {
		return uuid.Nil
	}
	return a.Admin.AdminUserID
}

// AdminVerifier is the slice of adminauth.Manager this package needs, declared
// HERE at the consumer (section 7).
type AdminVerifier interface {
	Verify(ctx context.Context, t adminauth.Token) (adminauth.Resolved, error)
}

type adminIdentityKey struct{}

// RequireAdmin resolves the panel cookie and REFUSES the request when it does not
// identify a live admin.
//
// onUnauthenticated is supplied by the caller rather than hard-coded, so this
// package does not have to know the login page's address — internal/handler owns
// its own routes. A nil handler falls back to a bare 401 rather than to letting
// the request through: the failure mode of a mis-wired gate must be "nobody gets
// in", not "everybody does".
//
// IT COSTS ONE RESOLVER READ PLUS ONE UPDATE per authenticated request
// (adminauth.Manager.Verify explains why the write is deliberate here and not on
// the tap path). Mount it on the panel route group only — never globally.
func RequireAdmin(codec adminauth.Cookies, v AdminVerifier, onUnauthenticated http.Handler) func(http.Handler) http.Handler {
	if onUnauthenticated == nil {
		onUnauthenticated = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "sign in", http.StatusUnauthorized)
		})
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := resolveAdminIdentity(r, codec, v)
			r = r.WithContext(context.WithValue(r.Context(), adminIdentityKey{}, id))
			if !id.Live() {
				// An outage and a missing cookie land on the same screen. They are
				// distinguished in the CONTEXT (Err is set for one and not the
				// other), so the login handler can log the difference without
				// telling the visitor two different stories.
				onUnauthenticated.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func resolveAdminIdentity(r *http.Request, codec adminauth.Cookies, v AdminVerifier) AdminIdentity {
	if v == nil {
		return AdminIdentity{Err: errors.New("httpx: no admin verifier")}
	}
	t, err := codec.Read(r)
	if err != nil {
		if errors.Is(err, adminauth.ErrNoSession) {
			return AdminIdentity{State: AdminAbsent}
		}
		return AdminIdentity{Err: err}
	}
	res, err := v.Verify(r.Context(), t)
	switch {
	case err == nil:
		return AdminIdentity{State: AdminLive, Admin: res}
	case errors.Is(err, adminauth.ErrNoSession):
		return AdminIdentity{State: AdminAbsent}
	default:
		// A database failure is NOT "signed out". Returning AdminAbsent here would
		// turn an outage into a redirect loop and would make every panel user
		// believe their password had stopped working.
		return AdminIdentity{Err: err}
	}
}

// AdminOf returns what RequireAdmin resolved for r, or the zero AdminIdentity
// (AdminUnresolved) when it did not run.
func AdminOf(r *http.Request) AdminIdentity {
	if a, ok := r.Context().Value(adminIdentityKey{}).(AdminIdentity); ok {
		return a
	}
	return AdminIdentity{}
}
