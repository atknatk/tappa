package httpx

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/session"
)

// WHY THERE IS NO "TENANT MIDDLEWARE" HERE, and why that is not a gap.
//
// The M5-03 card asks for "tenant context through WithTenant; no manual SET in a
// handler". The second half is a rule about SQL and it holds — measured, by
// grepping production Go for the statement that establishes it: ONE hit,
// db.WithTenant, which binds the id as a parameter inside a transaction (ADR
// 0002 items 2 and 3). No handler, and nothing in this package, issues it.
//
// The first half cannot take the shape a classic tenant middleware takes,
// because on the tap path THE TENANT IS NOT AN INPUT TO THE REQUEST — it is the
// OUTPUT of resolving one. A tap carries a tag UID and a session cookie, and
// ADR 0002 item 7 is explicit that neither carries a tenant: app.tenant_id is
// what those two lookups PRODUCE. There is no host, path or header to read a
// tenant from, and inventing one would mean letting the caller name the tenant
// it wants to be scoped to — the exact failure §4.5 exists to prevent.
//
// So what the request boundary can honestly build is IDENTITY: resolve the
// session cookie once, carry the facts it produced (including its tenant), and
// let the handler pass them to the code that needs them. That is this file. See
// the card correction in docs/plan/m5-tap-akisi.md for the full reasoning.
//
// WHAT THIS MIDDLEWARE IS NOT:
//
//   - NOT an authorisation gate. It never writes a response and never refuses a
//     request. §5 rows 3 and 4 are decisions, and decisions belong to
//     tap.Decide / internal/policy, not to a middleware that would have to
//     choose between them without any of the context.
//   - NOT a deactivation check. Resolution deliberately cannot see
//     employees.status (migration 00003; M5-01 card correction), and the
//     authority on a deactivated employee is the sys:employee-deactivated
//     guardrail, which needs a RECORD written (§5 row 4). A middleware that
//     turned "revoked" or "deactivated" into "no session" would silently
//     downgrade row 4 to row 3 and lose the attempt (§4.6).
//     ⚠️ HAND-OFF, KEPT VISIBLE (state.md, from the M5-01 audits): any
//     authenticated surface that is NOT the tap path — a future employee page,
//     a portal — must check employees.status ITSELF. Outside the tap decision
//     engine there is no other authority, and this middleware does not become
//     one by being convenient.
//   - NOT a Touch. Refreshing last_used_at is a write, and a middleware that
//     wrote on every request would put a database write in front of the rate
//     limiter. The caller decides (session.Manager.Touch).

// SessionState says what a request's session cookie resolved to.
//
// THE ZERO VALUE IS "UNRESOLVED", NOT "ABSENT", and that choice is the M5-01
// lesson applied: pick the polarity so that the state you get by DOING NOTHING is
// the harmless one, and reaching the consequential one takes naming it. That is
// total rather than best-effort, because the zero value of an integer enum is
// whatever iota starts at: every Identity obtained without a decision — a var, a
// literal, an unwritten struct field, a missing map key, a slice element — is
// SessionUnresolved — a sub-router that forgot to mount the middleware, a
// hand-built request in a test. Had zero meant "no session", that same mistake
// would have
// looked exactly like a genuine session-less tap and taken §5 row 3: activation
// page, NO RECORD. Silently dropping a tap is the one outcome §4.6 forbids.
type SessionState int

const (
	// SessionUnresolved is the zero value: nobody looked, or looking failed.
	// Identity.Err says which.
	SessionUnresolved SessionState = iota
	// SessionAbsent means there was no cookie, or it was malformed, or it named
	// no session — §5 row 3, and the three are deliberately indistinguishable
	// (internal/session/cookie.go).
	SessionAbsent
	// SessionLive means the cookie resolved to a session that has not been
	// revoked. It says NOTHING about the employee's status.
	SessionLive
	// SessionRevoked means the cookie resolved to a REAL session that was
	// revoked. The facts are populated, because §5 row 4 needs a record written
	// for the attempt — see session.ErrRevoked for the API trap this mirrors.
	SessionRevoked
)

// Identity is what the request boundary learned about the caller. It carries
// facts; it does not carry a verdict.
type Identity struct {
	State SessionState
	// Session is populated for SessionLive AND SessionRevoked, empty otherwise.
	Session session.Resolved
	// Err is the failure behind SessionUnresolved (a database outage, typically)
	// and nil in every other state. It is carried rather than swallowed: a caller
	// that cannot tell "no session" from "cannot tell" will answer an outage with
	// a stream of activation redirects and no records (§7, §4.6).
	Err error
}

// Live reports whether the cookie resolved to a session that is not revoked.
// False for every other state INCLUDING SessionUnresolved, so a failure to
// resolve never reads as authenticated.
func (i Identity) Live() bool { return i.State == SessionLive }

// TenantID is the tenant the session resolved to, uuid.Nil when none did.
//
// 🔴 THIS IS THE SESSION HALF OF THE N5 HAND-OFF (state.md, "M4/M5'e
// devralınan"). tap.Decide is tenant-unaware because Input carries no tenant, so
// sys:tenant-mismatch never fires; the tag lookup is context-less by ADR 0002
// item 7, so RLS does NOT hide a cross-tenant tag either. M5-05 must fill
// tap.Input from BOTH sides — this value and the tenant the TAG resolved to —
// and the guardrail only bites when both are present. The tag half is M5-05's,
// because the tag is not resolved at the request boundary.
func (i Identity) TenantID() uuid.UUID {
	switch i.State {
	case SessionLive, SessionRevoked:
		return i.Session.TenantID
	default:
		return uuid.Nil
	}
}

// EmployeeID is the employee the session resolved to, uuid.Nil when none did.
func (i Identity) EmployeeID() uuid.UUID {
	switch i.State {
	case SessionLive, SessionRevoked:
		return i.Session.EmployeeID
	default:
		return uuid.Nil
	}
}

// SessionVerifier is the slice of session.Manager this package needs, declared
// HERE at the consumer (§7).
type SessionVerifier interface {
	Verify(ctx context.Context, t session.Token) (session.Resolved, error)
}

type identityKey struct{}

// Identify resolves the session cookie once per request and puts the facts in
// the context.
//
// IT IS NOT MOUNTED GLOBALLY, on purpose: it costs one database query, so it
// belongs on the routes that need an identity and not on /healthz, /static or
// the activation flow (which resolves sessions itself, for a different question
// — whose phone is this). Mount it on a route group; see TapLimiter for the
// order the tap routes need.
func Identify(codec session.Cookies, v SessionVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(
				context.WithValue(r.Context(), identityKey{}, resolveIdentity(r, codec, v)),
			))
		})
	}
}

func resolveIdentity(r *http.Request, codec session.Cookies, v SessionVerifier) Identity {
	if v == nil {
		return Identity{Err: errors.New("httpx: no session verifier")}
	}
	t, err := codec.Read(r)
	if err != nil {
		// cookie.Read returns ErrNoSession and nothing else; anything unexpected
		// is treated as unresolved rather than as "no session".
		if errors.Is(err, session.ErrNoSession) {
			return Identity{State: SessionAbsent}
		}
		return Identity{Err: err}
	}
	res, err := v.Verify(r.Context(), t)
	switch {
	case err == nil:
		return Identity{State: SessionLive, Session: res}
	case errors.Is(err, session.ErrRevoked):
		// Verify populates Resolved on this branch and the caller needs it: §5
		// row 4 wants the attempt RECORDED, not redirected. This is the API trap
		// session/manager.go names — `if err != nil { activate }` is wrong here.
		return Identity{State: SessionRevoked, Session: res}
	case errors.Is(err, session.ErrNoSession):
		return Identity{State: SessionAbsent}
	default:
		return Identity{Err: err}
	}
}

// IdentityOf returns what Identify resolved for r, or the zero Identity
// (SessionUnresolved) when it did not run.
func IdentityOf(r *http.Request) Identity {
	if i, ok := r.Context().Value(identityKey{}).(Identity); ok {
		return i
	}
	return Identity{}
}

// SessionTenantID returns the tenant the request's session resolved to. ok is
// false when no session resolved, so a caller HAS A WAY to tell "no tenant" from
// a tenant — ignoring ok and passing uuid.Nil onwards is a choice this function
// cannot prevent, only make visible. See Identity.TenantID for the N5 hand-off
// this feeds.
func SessionTenantID(r *http.Request) (uuid.UUID, bool) {
	id := IdentityOf(r).TenantID()
	return id, id != uuid.Nil
}
