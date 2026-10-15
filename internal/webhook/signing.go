package webhook

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
	"strings"
)

// THE ENDPOINT'S SIGNING KEY AND THE SIGNATURE (M11 API-9; ADR 0028 §6, K-11, K-12).
//
// THE KEY is SigningKeyPrefix ("tt_whsec_" -- chosen in API-3, ADR 0028's API-3 note item 9, so
// redline R7d's value pattern finds a leaked one) and 43 characters of unpadded base64url: the
// canonical encoding of 32 random bytes. It is stored only SEALED (sun.Seal under TAPPA_WEBHOOK_KEK,
// AAD = the endpoint's id -- internal/webhook/endpoints), shown to the owner ONCE (on creation and on
// rotation; there is no "show" -- K-12), and never logged: SigningKey prints a placeholder through
// every rendering path, apikey.Token's shape.
//
// THE SIGNATURE (K-11) is the header value "sha256=" + the lower-case hex HMAC-SHA256 of the request
// body's exact bytes, keyed by the key's whole text (the prefix included -- what the integrator holds
// is the string, and it keys its HMAC with the string). No timestamp header: a header outside the
// signature would be confidence it does not earn. The receiver recomputes it over the raw body and
// compares in constant time.

const (
	// SigningKeyPrefix marks a webhook signing key (R7d's second value pattern).
	SigningKeyPrefix = "tt_whsec_"
	// SignatureHeader carries the signature on every delivery and ping.
	SignatureHeader = "X-Taptime-Signature"
	// SignaturePrefix precedes the hex digest (K-11).
	SignaturePrefix = "sha256="

	signingKeyBytes   = 32
	signingKeyBodyLen = 43
	redactedKey       = "webhook.SigningKey(redacted)"
)

// ErrSigningKeyShape: a value that is not SigningKeyPrefix + the canonical base64url of 32 bytes.
var ErrSigningKeyShape = errors.New("webhook: not a signing key")

var strictB64 = base64.RawURLEncoding.Strict()

// SigningKey is an endpoint's signing key in memory: between its minting (or its opening from the
// seal) and the one response that shows it, or the signature it keys. The field is a pointer for
// apikey.Token's reason -- fmt reaches an unexported field by reflection and prints an address there.
type SigningKey struct{ v *string }

// Compile-time proof that every redaction interface is implemented.
var (
	_ fmt.Formatter          = SigningKey{}
	_ fmt.Stringer           = SigningKey{}
	_ fmt.GoStringer         = SigningKey{}
	_ slog.LogValuer         = SigningKey{}
	_ encoding.TextMarshaler = SigningKey{}
)

// MintSigningKey draws a new key from crypto/rand.
func MintSigningKey() (SigningKey, error) {
	b := make([]byte, signingKeyBytes)
	if _, err := rand.Read(b); err != nil {
		return SigningKey{}, fmt.Errorf("webhook: read randomness: %w", err)
	}
	v := SigningKeyPrefix + base64.RawURLEncoding.EncodeToString(b)
	return SigningKey{v: &v}, nil
}

// ParseSigningKey wraps a key read back from its seal, checking its shape (a seal that opens to
// anything else is refused, never used to sign).
func ParseSigningKey(v string) (SigningKey, error) {
	if len(v) != len(SigningKeyPrefix)+signingKeyBodyLen || !strings.HasPrefix(v, SigningKeyPrefix) {
		return SigningKey{}, ErrSigningKeyShape
	}
	b, err := strictB64.DecodeString(v[len(SigningKeyPrefix):])
	if err != nil || len(b) != signingKeyBytes {
		return SigningKey{}, ErrSigningKeyShape
	}
	return SigningKey{v: &v}, nil
}

func (k SigningKey) reveal() string {
	if k.v == nil {
		return ""
	}
	return *k.v
}

// IsZero reports whether k holds no key.
func (k SigningKey) IsZero() bool { return k.v == nil }

// RevealToTheOwnerOnce is the key's text for the one response that shows it: the answer to "create
// the endpoint" or "rotate its key" (K-12). It greps to that one call site.
func (k SigningKey) RevealToTheOwnerOnce() string { return k.reveal() }

// SealInput is the key's text for the one place it is written: sun.Seal's plaintext.
func (k SigningKey) SealInput() []byte { return []byte(k.reveal()) }

// Sign is the signature header's value for body: "sha256=" + hex HMAC-SHA256(key text, body).
func (k SigningKey) Sign(body []byte) string {
	m := hmac.New(sha256.New, []byte(k.reveal()))
	m.Write(body)
	return SignaturePrefix + hex.EncodeToString(m.Sum(nil))
}

// Format implements fmt.Formatter for every verb.
func (SigningKey) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redactedKey)) }

// String implements fmt.Stringer.
func (SigningKey) String() string { return redactedKey }

// GoString covers %#v.
func (SigningKey) GoString() string { return redactedKey }

// LogValue implements slog.LogValuer.
func (SigningKey) LogValue() slog.Value { return slog.StringValue(redactedKey) }

// MarshalText implements encoding.TextMarshaler, which encoding/json prefers.
func (SigningKey) MarshalText() ([]byte, error) { return []byte(redactedKey), nil }
