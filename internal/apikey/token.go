// Package apikey is the integration API's key (M11 API-3, ADR 0028 §3, §13): its shape, its
// minting, its keyed hash, the owner's panel acts on it (keys.go) and the building blocks of the
// Bearer chain (auth.go). The /v1 HTTP surface that strings those blocks together -- with the
// address bucket between the shape check and the resolver, and the key bucket after it -- is
// API-4's.
//
// 🔴 THIS PACKAGE LOGS NOTHING AND RETURNS NO SECRET IN AN ERROR (ADR 0028 §3, §13; CLAUDE.md §7).
// The key, its hash and the Authorization header never reach a log line, an error's text or an
// audit row's detail: the key travels as Token and the hash as Checked, both of which print a
// placeholder through every rendering path (leak_external_test.go measures each), and the error
// values are fixed sentinels that carry no input. A caller that logs logs ids.
//
// THE SHAPE (K-10): a prefix -- tt_live_ for a production-like deployment, tt_test_ for the
// sandbox build (API-11) -- and 43 characters of unpadded base64url, the canonical encoding of 32
// random bytes from crypto/rand. One deployment accepts ONE prefix; a key of the other shape, or
// of no known shape, is refused before anything reaches the database (Check).
//
// THE STORED VALUE is the hex HMAC-SHA256 of a domain label and the whole key under
// TAPPA_API_KEY_HMAC_KEY, a key of its own (internal/config refuses it equal to any other key): a
// dump of api_keys alone cannot be tried against the API, and the same string under the session
// or invite key hashes to something else.
package apikey

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
)

const (
	// LivePrefix is the prefix of every key a production-like deployment issues and accepts --
	// every build but the sandbox's (ADR 0028 §11, K-10).
	LivePrefix = "tt_live_"
	// SandboxPrefix is the sandbox build's prefix (API-11 wires it). A production deployment never
	// accepts it, and the sandbox never accepts LivePrefix.
	SandboxPrefix = "tt_test_"

	// randomBytes is the entropy behind one key: 256 bits. The table carries no lockout state,
	// which is sound only because a key cannot be guessed -- shrinking this breaks that.
	randomBytes = 32
	// bodyLen is randomBytes in unpadded base64url: ceil(32*8/6) = 43.
	bodyLen = 43
	// hintLen is how much of the body prefix_hint keeps: 4 characters, 24 of the 256 bits. It
	// tells an owner's few keys apart in a list; it leaves 232 unknown bits, so knowing it does
	// not bring a guess any nearer (ADR 0028's API-3 note).
	hintLen = 4

	// hmacKeyLen is TAPPA_API_KEY_HMAC_KEY's size; config.Load enforces it, and this package
	// re-checks so a hand-built value cannot MAC under a short or empty key.
	hmacKeyLen = 32

	// domainLabel is prefixed to the MAC input, internal/invite's reason: a second, independent
	// separation from every other keyed hash in the product even if two variables were filled
	// from one value. It ends in a byte base64url cannot hold, so label||key is unambiguous.
	// Changing it invalidates every stored hash; a new scheme is "tappa/apikey/v2|".
	domainLabel = "tappa/apikey/v1|"

	// redacted is what every printing path of Token emits. It holds no part of the value.
	redacted = "apikey.Token(redacted)"
	// redactedHash is Checked's placeholder.
	redactedHash = "apikey.Checked(redacted)"
)

// strict is base64url without padding that refuses non-canonical trailing bits: two strings
// that decode to the same 32 bytes would otherwise both be "the key", and only one of them is
// what was issued.
var strict = base64.RawURLEncoding.Strict()

// Token is a raw integration key in transit: from Mint to the one response that shows it to the
// owner, or from the Authorization header to Check. Never stored; only its hash is.
//
// The field is a POINTER for internal/invite.Code's reason: fmt reaches an unexported field by
// reflection, where the methods below cannot intervene, and a pointer prints an address there.
// Comparison is identity, not value -- nothing in this package compares two Tokens.
type Token struct{ v *string }

// Compile-time proof that every redaction interface is implemented.
var (
	_ fmt.Formatter          = Token{}
	_ fmt.Stringer           = Token{}
	_ fmt.GoStringer         = Token{}
	_ slog.LogValuer         = Token{}
	_ encoding.TextMarshaler = Token{}
)

// ParseToken wraps an untrusted string -- the Bearer credential -- in a Token. It does not
// validate; Check does, once.
func ParseToken(v string) Token { return Token{v: &v} }

func (t Token) reveal() string {
	if t.v == nil {
		return ""
	}
	return *t.v
}

// RevealToTheOwnerOnce returns the raw key for the ONE place it is ever shown: the response to
// the owner's "create a key" (K-10: "ham anahtar yalnız oluşturma yanıtında, bir kez"). It greps
// to that one call site (internal/handler/apikeys.go); every other path renders the placeholder.
func (t Token) RevealToTheOwnerOnce() string { return t.reveal() }

// Format implements fmt.Formatter for every verb.
func (Token) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redacted)) }

// String implements fmt.Stringer.
func (Token) String() string { return redacted }

// GoString covers %#v.
func (Token) GoString() string { return redacted }

// LogValue implements slog.LogValuer.
func (Token) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalText implements encoding.TextMarshaler, which encoding/json prefers.
func (Token) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// mint draws randomBytes from crypto/rand and returns the key under prefix. A crypto/rand failure
// is returned, never ignored (§7).
func mint(prefix string) (Token, error) {
	b := make([]byte, randomBytes)
	if _, err := rand.Read(b); err != nil {
		return Token{}, fmt.Errorf("api key: read randomness: %w", err)
	}
	return ParseToken(prefix + base64.RawURLEncoding.EncodeToString(b)), nil
}

// shape reports whether v is prefix + the canonical base64url of 32 bytes. It allocates only the
// decode buffer and does no keyed work, so junk costs nothing.
func shape(v, prefix string) bool {
	if len(v) != len(prefix)+bodyLen || !strings.HasPrefix(v, prefix) {
		return false
	}
	b, err := strict.DecodeString(v[len(prefix):])
	return err == nil && len(b) == randomBytes
}

// hint is prefix_hint: the prefix and the first hintLen characters of the body. Only for a value
// that passed shape.
func hint(v, prefix string) string { return v[:len(prefix)+hintLen] }

// keyedHash is the stored value: hex HMAC-SHA256(key, domainLabel || v) -- api_keys.token_hash,
// whose CHECK is ^[0-9a-f]{64}$. The STRING is hashed, not its decoded bytes (strict decoding
// already makes the two one-to-one). Errors carry a length at most, never key bytes.
func keyedHash(key []byte, v string) (string, error) {
	if len(key) != hmacKeyLen {
		return "", fmt.Errorf("api key: hmac key must be %d bytes, got %d", hmacKeyLen, len(key))
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(domainLabel))
	m.Write([]byte(v))
	return hex.EncodeToString(m.Sum(nil)), nil
}

// validPrefix is the closed set a deployment may be built with.
func validPrefix(p string) error {
	if p != LivePrefix && p != SandboxPrefix {
		return fmt.Errorf("api key: the prefix must be %q or %q", LivePrefix, SandboxPrefix)
	}
	return nil
}
