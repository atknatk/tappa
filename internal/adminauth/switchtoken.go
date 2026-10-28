package adminauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// switchtoken.go -- the synchronizer token of POST /admin/switch (ADR 0031 §5.4, M14-10).
//
// WHY THE SWITCH HAS ONE WHEN NO OTHER PANEL WRITE DOES. Every panel write is held by the
// Origin check ahead of the resolver and by the tenant binding of its action (formbind.go). The
// switch is the one write whose whole effect is to MOVE the browser: a forged switch that got
// past both would not change a record, it would silently put the person into another business,
// where their next press lands. ADR 0031 §5.4 therefore asks for a synchronizer of its own in the
// form's body, bound to the SESSION rather than to the tenant:
//
//	value = base64url(HMAC(K, "tappa/admin-switch/v1|" + session_id))
//	K     = HMAC(switchKey, raw session token)
//	switchKey = HMAC(TAPPA_SESSION_HMAC_KEY, switchKeyLabel)
//
// K comes from the RAW token -- the cookie value -- never from token_hash, which a database
// reader can see (formbind.go's rule, for the same reason). The session id makes the value die
// with the session: a switch issues a new session, so the page drawn for the old one cannot be
// replayed into the new one. The server key is mixed in so that a database dump plus a session id
// cannot be turned into a token either.
//
// It is never logged: it is a per-session credential.
const (
	// switchKeyLabel derives this file's key from the session key, under its own label -- a
	// switch token is never a form binding and the reverse (formBindKeyLabel's shape).
	switchKeyLabel = "tappa/admin-switch/v1/key-derivation"
	// switchPrefix is the message's domain prefix, verbatim from ADR 0031 §5.4.
	switchPrefix = "tappa/admin-switch/v1|"
	// SwitchTokenLen is the length of a switch token: 32 MAC bytes as unpadded base64url.
	SwitchTokenLen = 43
)

// errSwitchTokensUnset is what the zero SwitchTokens answers (fail closed).
var errSwitchTokensUnset = errors.New("adminauth: switch tokens have no key")

// SwitchTokens produces and checks the switch's synchronizer tokens. The zero value refuses
// everything.
type SwitchTokens struct{ key []byte }

// NewSwitchTokens derives the key from TAPPA_SESSION_HMAC_KEY.
func NewSwitchTokens(sessionKey []byte) (SwitchTokens, error) {
	if len(sessionKey) != hmacKeyLen {
		return SwitchTokens{}, fmt.Errorf("adminauth: hmac key must be %d bytes, got %d", hmacKeyLen, len(sessionKey))
	}
	m := hmac.New(sha256.New, sessionKey)
	_, _ = m.Write([]byte(switchKeyLabel)) // hash writes never fail (documented)
	return SwitchTokens{key: m.Sum(nil)}, nil
}

// Mint is the switch token of the session whose raw token is t and whose id is session. It
// refuses a token this package could not have issued and the nil session.
func (s SwitchTokens) Mint(t Token, session uuid.UUID) (string, error) {
	if len(s.key) != sha256.Size {
		return "", errSwitchTokensUnset
	}
	if session == uuid.Nil {
		return "", errors.New("adminauth: no session to mint a switch token for")
	}
	v := t.reveal()
	if len(v) != tokenLen {
		return "", errMalformed
	}
	if _, err := base64.RawURLEncoding.DecodeString(v); err != nil {
		return "", errMalformed
	}
	k := hmac.New(sha256.New, s.key)
	_, _ = k.Write([]byte(v))
	m := hmac.New(sha256.New, k.Sum(nil))
	_, _ = m.Write([]byte(switchPrefix + session.String()))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)), nil
}

// Matches reports whether got is the switch token of session under t. Constant-time
// (subtle.ConstantTimeCompare, counted by cmd/tappa's constant-time inventory); a value of the
// wrong length is refused before any MAC work, which reveals nothing but the public length.
func (s SwitchTokens) Matches(t Token, session uuid.UUID, got string) bool {
	if len(got) != SwitchTokenLen {
		return false
	}
	want, err := s.Mint(t, session)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}
