package liveqr

import (
	"crypto/rand"
	"encoding"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
)

// THE SCREEN SESSION -- what a paired screen holds: one HttpOnly cookie, 256 bits from
// crypto/rand. The database keeps only its digest (live_qr_displays.session_hash, HMAC-SHA256 under
// the session key), which tappa_app cannot even SELECT (00111); the screen resolves through
// resolve_live_qr_display_by_session_hash.

const (
	sessionBytes = 32
	sessionLen   = 43 // unpadded base64url of 32 bytes
	// SessionCookieName is the screen's cookie. Its Path is ScreenCookiePath, so it is sent to
	// the screen page and to nothing else -- never to /t, /q or the panel.
	SessionCookieName = "tappa_screen"
	// ScreenCookiePath is the screen page's root (internal/handler mounts the page under it).
	ScreenCookiePath = "/screen"
	// SessionMaxAge is ScreenSessionLifetime in seconds (180 days): the browser forgets the cookie
	// when the server would refuse it anyway. Exported for the cookie notice (/legal/cookies).
	SessionMaxAge = 180 * 24 * 60 * 60
)

// ScreenSession is a screen's cookie value in transit -- PairingCode's shape and reasons.
type ScreenSession struct{ v *string }

const redactedSession = "liveqr.ScreenSession(redacted)"

var (
	_ fmt.Formatter          = ScreenSession{}
	_ fmt.Stringer           = ScreenSession{}
	_ fmt.GoStringer         = ScreenSession{}
	_ slog.LogValuer         = ScreenSession{}
	_ encoding.TextMarshaler = ScreenSession{}
)

func (ScreenSession) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte(redactedSession)) }
func (ScreenSession) String() string               { return redactedSession }
func (ScreenSession) GoString() string             { return redactedSession }
func (ScreenSession) LogValue() slog.Value         { return slog.StringValue(redactedSession) }
func (ScreenSession) MarshalText() ([]byte, error) { return []byte(redactedSession), nil }

// IsZero reports a session that holds nothing.
func (s ScreenSession) IsZero() bool { return s.v == nil }

func (s ScreenSession) reveal() string {
	if s.v == nil {
		return ""
	}
	return *s.v
}

func newScreenSession() (ScreenSession, error) {
	b := make([]byte, sessionBytes)
	if _, err := rand.Read(b); err != nil {
		return ScreenSession{}, fmt.Errorf("liveqr: read randomness: %w", err)
	}
	v := base64.RawURLEncoding.EncodeToString(b)
	return ScreenSession{v: &v}, nil
}

// SessionDigest is the value stored in live_qr_displays.session_hash. A value that cannot be a
// session this package issued (wrong length or alphabet) is refused before any MAC work.
func (k *Keys) SessionDigest(s ScreenSession) (string, error) {
	if k == nil || s.IsZero() {
		return "", errNoValue
	}
	v := s.reveal()
	if len(v) != sessionLen {
		return "", errNoValue
	}
	if _, err := base64.RawURLEncoding.Strict().DecodeString(v); err != nil {
		return "", errNoValue
	}
	return digest(k.session, v)
}

// ScreenCookies writes and reads the screen's cookie. Secure follows the employee session's rule
// (internal/session.NewCookies: always outside a plain-http development server); the caller passes
// that answer (NewScreenCookies) so this package does not read the configuration twice.
type ScreenCookies struct{ secure bool }

// NewScreenCookies is the codec, Secure or not.
func NewScreenCookies(secure bool) ScreenCookies { return ScreenCookies{secure: secure} }

// Secure reports the hardening this codec applies.
func (c ScreenCookies) Secure() bool { return c.secure }

// Set writes the cookie: HttpOnly (no script on the page can read it), SameSite=Lax (every cookie on
// /legal/cookies carries the same flags; Lax still keeps it off every cross-site POST, and the page
// has no state-changing GET), Path=/screen (never sent to /t, /q or the panel).
func (c ScreenCookies) Set(w http.ResponseWriter, s ScreenSession) error {
	if s.IsZero() {
		return errNoValue
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    s.reveal(),
		Path:     ScreenCookiePath,
		MaxAge:   SessionMaxAge,
		HttpOnly: true,
		Secure:   c.Secure(),
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// Clear expires the cookie (a revoked or unknown screen).
func (c ScreenCookies) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     ScreenCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   c.Secure(),
		SameSite: http.SameSiteLaxMode,
	})
}

// Read is the request's screen session, or the zero session when there is none. The value is
// wrapped as it is read; nothing about it is checked here (SessionDigest refuses a bad shape).
func (ScreenCookies) Read(r *http.Request) ScreenSession {
	ck, err := r.Cookie(SessionCookieName)
	if err != nil || ck.Value == "" || len(ck.Value) > 4*sessionLen {
		return ScreenSession{}
	}
	v := ck.Value
	return ScreenSession{v: &v}
}
