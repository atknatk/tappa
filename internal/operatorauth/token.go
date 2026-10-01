package operatorauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
)

// The operator session token (ADR 0020 §2): 256 random bits in the cookie, and in
// platform_sessions.token_hash only its HMAC-SHA256 under TAPPA_OPERATOR_TOKEN_HMAC_KEY
// as 64 lower-case hex characters (00026's CHECK `^[0-9a-f]{64}$`).
//
// A DELIBERATE THIRD COPY of internal/session.Token and internal/adminauth.Token --
// adminauth's header argues the duplication, and ADR 0020 §2 adds the two things that
// must differ here: the KEY is its own variable, never a label on the session key
// (internal/adminauth/token.go's own warning, `adminTokenKeyLabel`: a derived key is
// not independent of its source, and "a future credential that needs real
// independence should get its own variable"), and the PLACEHOLDER is different, so a
// leak test that greps for one type's placeholder cannot pass over another type's
// leaked value (token.go's `redacted` comment in internal/adminauth).

const (
	// tokenBytes is 256 bits (adminauth's tokenBytes; ADR 0021 sınır 3 leans on it:
	// guessing a live session hash is a search of 2^256).
	tokenBytes = 32
	// tokenLen is tokenBytes in unpadded base64url: ceil(256/6) = 43. The shape gate
	// that runs before any HMAC on an untrusted cookie.
	tokenLen = 43
	// sessionRedacted is what the five methods emit (every path of the leak test's
	// matrix that reaches a method; the rest print an address). It names THIS type and
	// is not adminauth's "adminauth.Token(redacted)" nor internal/session's
	// (TestSessionToken_PlaceholderIsNotAnotherCredentialsPlaceholder derives both
	// from the other packages rather than restating them).
	sessionRedacted = "operatorauth.SessionToken(redacted)"
)

// errMalformed is the shape error for a cookie value that cannot be a token this
// package issued. It never leaves the package: Verify and Logout map it to
// ErrNoSession, so a junk cookie and no cookie are the same thing -- and no error can
// quote the value.
var errMalformed = errors.New("operatorauth: value has the wrong shape")

// SessionToken is a raw operator session token in transit: from newSessionToken to
// the Set-Cookie write, or from the request cookie to its hash. Never stored.
//
// ⚠️ COMPARISON IS IDENTITY: the pointer is compared, so two tokens over the same
// string are `!=`. Fail-closed, and nothing compares one; the DATABASE matches hashes.
type SessionToken struct{ v *string }

var (
	_ fmt.Formatter          = SessionToken{}
	_ fmt.Stringer           = SessionToken{}
	_ fmt.GoStringer         = SessionToken{}
	_ slog.LogValuer         = SessionToken{}
	_ encoding.TextMarshaler = SessionToken{}
)

func wrapSessionToken(v string) SessionToken { return SessionToken{v: &v} }

// reveal is the raw value or "". Its readers: hash, SetSessionCookie (the single
// egress, into a Set-Cookie header) and this package's tests.
func (t SessionToken) reveal() string {
	if t.v == nil {
		return ""
	}
	return *t.v
}

func (SessionToken) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte(sessionRedacted)) }
func (SessionToken) String() string               { return sessionRedacted }
func (SessionToken) GoString() string             { return sessionRedacted }
func (SessionToken) LogValue() slog.Value         { return slog.StringValue(sessionRedacted) }
func (SessionToken) MarshalText() ([]byte, error) { return []byte(sessionRedacted), nil }

// newSessionToken draws tokenBytes from crypto/rand as unpadded base64url.
func newSessionToken() (SessionToken, error) {
	v, err := randomToken()
	if err != nil {
		return SessionToken{}, err
	}
	return wrapSessionToken(v), nil
}

// randomToken is the one generator of the 43-character credentials this package
// issues (session tokens, enrollment tokens).
func randomToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", errors.New("operatorauth: read randomness failed")
	}
	v := base64.RawURLEncoding.EncodeToString(b)
	clear(b)
	return v, nil
}

// wellFormed is the shape gate: exactly tokenLen characters of base64url. Cheap, and
// it runs before any HMAC or database work on an untrusted value.
func wellFormed(v string) bool {
	if len(v) != tokenLen {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(v)
	return err == nil
}

// hash is the value stored in platform_sessions.token_hash: hex(HMAC-SHA256(key, the
// token's STRING bytes)). Over the string rather than the decoded bytes so the map
// cookie -> hash is injective (base64 allows non-canonical trailing bits; decoding
// first would let two cookie strings share one hash) -- adminauth's reasoning.
func (t SessionToken) hash(key []byte) (string, error) {
	if len(key) != keyLen {
		return "", fmt.Errorf("operatorauth: hmac key must be %d bytes, got %d", keyLen, len(key))
	}
	v := t.reveal()
	if !wellFormed(v) {
		return "", errMalformed
	}
	m := hmac.New(sha256.New, key)
	_, _ = m.Write([]byte(v)) // hash writes never fail (documented)
	return hex.EncodeToString(m.Sum(nil)), nil
}
