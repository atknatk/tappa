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

// formbind.go -- the tenant binding every panel form carries (ADR 0031 §5.5, M14-10a).
//
// THE PROBLEM IT CLOSES. The whole panel is one cookie on one path (Path=/admin). A tab
// left open on business A, submitted after the same browser moved to business B, would
// write into B: no panel write was tied to the business its page was drawn for. The
// binding is that tie, and it is read ONLY TO REFUSE -- a request whose binding does not
// match is answered 409 before its body is read; a request whose binding matches acts in
// the tenant the session resolved to, exactly as before. Nothing here names a tenant to
// act in (ADR 0031 §3: the tenant is the output of resolution, never an input).
//
// THE VALUE. base64url(HMAC(K, "tappa/admin-form-tenant/v1|" + tenant_id)), where
//
//	K = HMAC(formBindKey, raw session token)
//	formBindKey = HMAC(TAPPA_SESSION_HMAC_KEY, formBindKeyLabel)
//
// K comes from the RAW token -- the cookie value -- and never from token_hash, which a
// database reader can see (ADR 0031 §5.5). So the binding is also a synchronizer token:
// nobody who lacks the session's cookie can produce it, and a new session (a switch, a
// fresh sign-in) makes every page drawn for the old one stale. The server key is mixed in
// so that a database dump plus a guessed tenant id cannot be turned into a binding either.
//
// THE NAMED COST (ADR 0031 §5.5, §10). Rotating TAPPA_SESSION_HMAC_KEY changes formBindKey,
// so every open form stops matching. It also changes token_hash's key (token.go), so in
// practice every panel session ends first and an open form lands on the sign-in page --
// deploy/README.md carries the measurement.
const (
	// formBindKeyLabel derives this file's key from the session key, under its own label
	// (the shape of adminTokenKeyLabel and of every other derivation in this repo).
	formBindKeyLabel = "tappa/admin-form-tenant/v1/key-derivation"
	// formBindPrefix is the message's domain prefix, verbatim from ADR 0031 §5.5.
	formBindPrefix = "tappa/admin-form-tenant/v1|"
	// FormBindingLen is the length of a binding: 32 MAC bytes as unpadded base64url.
	FormBindingLen = 43
)

// errFormBinderUnset is what the zero FormBinder answers: it has no key, so it can neither
// produce nor accept a binding (fail closed -- an unwired binder refuses every form).
var errFormBinderUnset = errors.New("adminauth: form binder has no key")

// FormBinder produces and checks panel form bindings. The zero value refuses everything.
type FormBinder struct{ key []byte }

// NewFormBinder derives the binder's key from TAPPA_SESSION_HMAC_KEY.
func NewFormBinder(sessionKey []byte) (FormBinder, error) {
	if len(sessionKey) != hmacKeyLen {
		return FormBinder{}, fmt.Errorf("adminauth: hmac key must be %d bytes, got %d", hmacKeyLen, len(sessionKey))
	}
	m := hmac.New(sha256.New, sessionKey)
	_, _ = m.Write([]byte(formBindKeyLabel)) // hash writes never fail (documented)
	return FormBinder{key: m.Sum(nil)}, nil
}

// Bind is the binding of tenant under the session whose raw token is t. It refuses a
// token that is not one this package could have issued (the shape gate token.hash uses)
// and the nil tenant, so an unresolved request has no binding to render.
//
// The result is not a secret beyond the cookie (whoever holds the cookie can load a page
// and read it), but it is never logged: it is a per-session credential all the same.
func (b FormBinder) Bind(t Token, tenant uuid.UUID) (string, error) {
	if len(b.key) != sha256.Size {
		return "", errFormBinderUnset
	}
	if tenant == uuid.Nil {
		return "", errors.New("adminauth: no tenant to bind a form to")
	}
	v := t.reveal()
	if len(v) != tokenLen {
		return "", errMalformed
	}
	if _, err := base64.RawURLEncoding.DecodeString(v); err != nil {
		return "", errMalformed
	}
	k := hmac.New(sha256.New, b.key)
	_, _ = k.Write([]byte(v))
	m := hmac.New(sha256.New, k.Sum(nil))
	_, _ = m.Write([]byte(formBindPrefix + tenant.String()))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)), nil
}

// Matches reports whether got is the binding of tenant under t. The comparison is
// constant-time (subtle.ConstantTimeCompare, counted by cmd/tappa's constant-time
// inventory); a value of the wrong length is refused before any MAC work, which reveals
// nothing but the public length.
func (b FormBinder) Matches(t Token, tenant uuid.UUID, got string) bool {
	if len(got) != FormBindingLen {
		return false
	}
	want, err := b.Bind(t, tenant)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}
