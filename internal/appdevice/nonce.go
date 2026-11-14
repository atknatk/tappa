package appdevice

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
)

const (
	// nonceBytes is the server's randomness behind one nonce (M16-03 review: 32 bytes of
	// crypto/rand). 256 bits is why nonce_hash can be a plain SHA-256 (00104's header): there is
	// nothing to guess, so a key would add nothing.
	nonceBytes = 32
	// nonceLen is the wire length: 32 bytes in unpadded base64url, ceil(256/6) = 43.
	nonceLen = 43
	// redactedNonce is what every printing path emits instead of the value.
	redactedNonce = "appdevice.Nonce(redacted)"
)

// nonceEncoding is unpadded base64url in STRICT mode: the last character's unused bits must be
// zero, so one nonce has exactly one wire string (a lax decoder maps several strings to the
// same bytes, and the canonical text carries the string).
var nonceEncoding = base64.RawURLEncoding.Strict()

// Nonce is a single-use request nonce in transit: from IssueNonce to the response body, or from
// the request header to the consumption. The database holds only its hash (nonce_hash, 00104).
//
// §7 lists the request nonce among the values never logged. The type is built like
// session.Token for the same reasons (internal/session/token.go's header): the redacting
// methods cover every printing path that can reach a Nonce as an interface, and the *string
// field covers the one they cannot -- a Nonce inside a caller's UNEXPORTED struct field, which
// fmt prints by reflection. Through a *string every verb prints an address.
//
// ⚠️ IT HOLDS THE WIRE STRING, NOT THE BYTES, ON PURPOSE: a *[32]byte was measured to leak.
// fmt's bad-verb path (%s, %d on a caller's unexported field) prints a pointer's target at depth
// 0, and there it dereferences a pointer to an ARRAY -- the 32 bytes came out in decimal
// (leak_external_test.go). A pointer to a string is not dereferenced on any path.
//
// The zero Nonce is safe to hold and print; it is never valid (hash and wire refuse it).
type Nonce struct{ w *string }

var (
	_ fmt.Formatter          = Nonce{}
	_ fmt.Stringer           = Nonce{}
	_ fmt.GoStringer         = Nonce{}
	_ slog.LogValuer         = Nonce{}
	_ encoding.TextMarshaler = Nonce{}
)

// newNonce draws nonceBytes from crypto/rand -- the server's, never the client's: a nonce the
// client chose would let a recorded request choose its own freshness. A randomness failure is
// returned, never ignored (§7).
func newNonce() (Nonce, error) {
	var b [nonceBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return Nonce{}, fmt.Errorf("appdevice: read randomness: %w", err)
	}
	w := nonceEncoding.EncodeToString(b[:])
	return Nonce{w: &w}, nil
}

// ParseNonce reads a nonce as it arrives in a request: exactly 43 characters of strict unpadded
// base64url, decoding to 32 bytes. Anything else is ErrMalformed; the value is never echoed.
//
// Parsing does not make a nonce acceptable -- only its consumption does (Verifier.Verify), in
// one statement that also proves this server issued it to this device and it is unspent.
func ParseNonce(s string) (Nonce, error) {
	if len(s) != nonceLen {
		return Nonce{}, ErrMalformed
	}
	raw, err := nonceEncoding.DecodeString(s)
	if err != nil || len(raw) != nonceBytes {
		return Nonce{}, ErrMalformed
	}
	// The ROUND TRIP is the canonical-form check, not the length: Go's base64 decoder skips CR
	// and LF even in Strict mode, so a 43-character string carrying a line break around 42
	// characters of base64 can decode (M16-05 review). Only the string this server would have
	// written is accepted -- which also keeps every LF out of the canonical text.
	if nonceEncoding.EncodeToString(raw) != s {
		return Nonce{}, ErrMalformed
	}
	return Nonce{w: &s}, nil
}

// Wire is the nonce's one egress: the string the issue response hands the device, which the
// device copies into its canonical text and its request header unchanged. Do not log it.
func (n Nonce) Wire() string { return n.wire() }

func (n Nonce) wire() string {
	if n.w == nil {
		return ""
	}
	return *n.w
}

// hash is the value stored in and matched against app_request_nonces.nonce_hash: hex SHA-256
// of the 32 raw bytes (00104's CHECK: ^[0-9a-f]{64}$). The raw bytes, not the wire string: the
// strict decoder makes the two one-to-one, and the bytes are what the server drew. "" for the
// zero Nonce, which matches no row and fails the issue's CHECK -- fail closed.
func (n Nonce) hash() string {
	if n.w == nil {
		return ""
	}
	raw, err := nonceEncoding.DecodeString(*n.w)
	if err != nil || len(raw) != nonceBytes {
		return "" // unreachable: both constructors validated the string
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Format implements fmt.Formatter for every verb.
func (Nonce) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redactedNonce)) }

// String implements fmt.Stringer.
func (Nonce) String() string { return redactedNonce }

// GoString covers %#v.
func (Nonce) GoString() string { return redactedNonce }

// LogValue implements slog.LogValuer.
func (Nonce) LogValue() slog.Value { return slog.StringValue(redactedNonce) }

// MarshalText implements encoding.TextMarshaler, which encoding/json prefers -- so a Nonce
// reached by a JSON encoder serialises to the placeholder. The issue response must write
// Wire() into its body explicitly.
func (Nonce) MarshalText() ([]byte, error) { return []byte(redactedNonce), nil }
