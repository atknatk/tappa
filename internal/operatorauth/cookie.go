package operatorauth

import (
	"errors"
	"net/http"
	"time"
)

// The operator's two cookies (ADR 0020 §2): the session, and the login challenge
// between the password and the TOTP step. Plain net/http.
//
// 🔴 BOTH ARE `__Host-` COOKIES AND THEREFORE ALWAYS Secure -- THERE IS NO INSECURE
// MODE, BY DESIGN. The prefix makes the browser refuse the cookie unless it is Secure,
// has Path=/ and has NO Domain attribute; that is what pins it to the operator's host
// (ops.taptime.mt) and keeps a sibling host from setting or shadowing it. So the panel
// cookies' `insecure` relaxation (internal/adminauth/cookie.go) cannot exist here: a
// non-Secure `__Host-` cookie is not a weaker cookie, it is no cookie. The consequence,
// stated: the operator surface works over https (and on http://localhost, which
// browsers treat as a secure context); served over plain http on any other host, the
// browser drops both cookies and nobody can sign in -- the fail-closed direction.
//
// SameSite=Strict (ADR 0020 §2) and still NOT sufficient on its own: ops.taptime.mt and
// taptime.mt are the SAME SITE, so a script on the main host sends same-site requests
// here. The origin check before the resolver is internal/handler/operator's
// sameOriginGate (OP-8; ADR 0020 §4).
const (
	// SessionCookieName carries the raw session token.
	SessionCookieName = "__Host-taptime_op"
	// ChallengeCookieName carries the login challenge. Separate from the session so
	// clearing one never disturbs the other, and so a challenge value in the session
	// cookie resolves to nothing (the session gate hashes it and the database has no
	// such row) -- and a session token in the challenge cookie fails the MAC.
	ChallengeCookieName = "__Host-taptime_op_login"

	// cookiePath is the prefix's own requirement.
	cookiePath = "/"

	// sessionCookieMaxAge is the ABSOLUTE session lifetime, 8 hours (ADR 0020 §2). A
	// hint to the browser only: the DATABASE ends the session (op_touch_session, 8 h
	// from birth and 30 min idle, by its wall clock), whatever a client keeps.
	sessionCookieMaxAge = 8 * 60 * 60

	// challengeCookieMaxAge is the challenge's TTL, derived so the two cannot drift
	// (internal/handler's adminChoiceCookieMaxAge learned that by measurement).
	challengeCookieMaxAge = int(challengeTTL / time.Second)
)

var errEmptyCookie = errors.New("operatorauth: refusing to set an empty cookie")

func setCookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     cookiePath,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

// SetSessionCookie is the ONE place an operator session token leaves the process,
// into a Set-Cookie header.
func SetSessionCookie(w http.ResponseWriter, t SessionToken) error {
	v := t.reveal()
	if v == "" {
		return errEmptyCookie
	}
	setCookie(w, SessionCookieName, v, sessionCookieMaxAge)
	return nil
}

// ClearSessionCookie expires the session cookie in the browser. Never a substitute for
// Logout: only the database's revoked_at makes a copied cookie useless.
func ClearSessionCookie(w http.ResponseWriter) { setCookie(w, SessionCookieName, "", -1) }

// ReadSessionCookie lifts the session token off a request. No cookie and an empty one
// are ErrNoSession -- the same answer a junk or dead value gets from Verify.
func ReadSessionCookie(r *http.Request) (SessionToken, error) {
	c, err := r.Cookie(SessionCookieName)
	if err != nil || c.Value == "" {
		return SessionToken{}, ErrNoSession
	}
	return wrapSessionToken(c.Value), nil
}

// SetChallengeCookie writes the login challenge (the password step's only product).
func SetChallengeCookie(w http.ResponseWriter, c Challenge) error {
	v := c.reveal()
	if v == "" {
		return errEmptyCookie
	}
	setCookie(w, ChallengeCookieName, v, challengeCookieMaxAge)
	return nil
}

// ClearChallengeCookie expires the challenge (after a session is issued, or a restart
// of the sign-in).
func ClearChallengeCookie(w http.ResponseWriter) { setCookie(w, ChallengeCookieName, "", -1) }

// ReadChallengeCookie lifts the challenge off a request; absent or empty is
// ErrChallenge.
func ReadChallengeCookie(r *http.Request) (Challenge, error) {
	c, err := r.Cookie(ChallengeCookieName)
	if err != nil || c.Value == "" {
		return Challenge{}, ErrChallenge
	}
	return wrapChallenge(c.Value), nil
}
