package invite

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
)

// Binding is the consent binding (ADR 0026): a random token minted when the
// employee agrees to the GDPR notice, kept ONLY in that browser's HttpOnly
// activation cookie, and stored server-side as an HMAC on the invitation
// (employee_invites.consent_binding_hash, migration 00035).
//
// WHY IT EXISTS. Activation now completes on the first NFC tap, which arrives as a
// plain GET carrying whatever cookie the browser holds. A cross-site navigation can
// plant a CODE in somebody else's browser (internal/handler/cookies.go, measure 3)
// but it cannot plant a binding — a binding is minted only by the CSRF-protected
// consent POST. So "this browser holds the code" is not enough to activate; "this
// browser is the one that agreed" is.
//
// It is a bearer credential paired with the code, so it gets the code's
// redaction: every printing interface returns a placeholder, and the raw value
// reaches a string only inside this package's hash.
type Binding struct{ v *string }

var (
	_ fmt.Formatter          = Binding{}
	_ fmt.Stringer           = Binding{}
	_ fmt.GoStringer         = Binding{}
	_ slog.LogValuer         = Binding{}
	_ encoding.TextMarshaler = Binding{}
)

const (
	bindingRedacted = "invite.Binding(redacted)"
	// bindingLabel separates this HMAC from the code's: the same key hashes both,
	// and a value valid as one must never be accepted as the other.
	bindingLabel = "tappa/invite-consent/v1|"
)

// ParseBinding wraps a raw value from a cookie (or one the handler just minted).
func ParseBinding(v string) Binding { return Binding{v: &v} }

func (Binding) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(bindingRedacted)) }
func (Binding) String() string             { return bindingRedacted }
func (Binding) GoString() string           { return bindingRedacted }
func (Binding) LogValue() slog.Value       { return slog.StringValue(bindingRedacted) }
func (Binding) MarshalText() ([]byte, error) {
	return []byte(bindingRedacted), nil
}

// hash is the stored form. It requires the code's exact shape (43 base64url
// characters, 256 bits): a shorter binding would be a guessable credential.
func (b Binding) hash(key []byte) (string, error) {
	if len(key) != hmacKeyLen {
		return "", fmt.Errorf("invite: hmac key must be %d bytes, got %d", hmacKeyLen, len(key))
	}
	if b.v == nil || len(*b.v) != codeLen {
		return "", errMalformed
	}
	if _, err := base64.RawURLEncoding.DecodeString(*b.v); err != nil {
		return "", errMalformed
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(bindingLabel))
	_, _ = mac.Write([]byte(*b.v))
	return hex.EncodeToString(mac.Sum(nil)), nil
}
