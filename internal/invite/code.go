package invite

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

// Activation-code generation and keyed hashing. This file is the deliberate twin
// of internal/session/token.go and repeats its two redaction mechanisms; the
// reasoning there applies verbatim and is not re-argued here, only summarised:
//
//  1. REDACTING METHODS (fmt.Formatter for every verb, Stringer, GoStringer,
//     slog.LogValuer, encoding.TextMarshaler) cover every path where a printer
//     can reach the value as an interface.
//  2. INDIRECTION (the field is a *string) covers the path the methods CANNOT:
//     a value read out of an UNEXPORTED struct field is not interface-able
//     (reflect.Value.CanInterface() == false), so fmt falls through to plain
//     reflection. With a string field, `%+v` on a handler-local
//     `struct{ code Code }` printed the value; with a *string it prints an
//     address. That was a measured audit finding on session.Token (M5-01, round
//     2), and activation handlers are exactly the code that hits it.
//
// KNOWN, ACCEPTED LIMIT — do not restate as "impossible": deliberate reflection
// (reflect.ValueOf(c).Field(0).Elem().String()), a debugger and a core dump all
// still read the value. The guarantee is about ACCIDENTAL rendering, and it is
// measured from an external test package in leak_external_test.go.
//
// TWO KINDS OF SECRET LIVE IN THIS FLOW AND BOTH ARE §4.7 MATERIAL: the raw code
// and its hash. The hash is a BEARER credential in this design — hash → resolver
// → tenant → consumption (migration 00009) — so logging the hash is exactly as
// bad as logging the code. That is why hash() returns a plain string only to its
// two call sites inside this package and never travels through an error value.

const (
	// codeBytes is the entropy behind one activation code: 32 bytes = 256 bits,
	// drawn from crypto/rand.
	//
	// THIS CONSTANT IS THE ACCEPTANCE CRITERION. The M5-02 card allows two
	// branches: a code of >= 128 bits, OR a short human-typed code WITH strict
	// lockout state (attempt counter + lock stamp) that would need a new
	// migration. Migration 00009 carries NO lockout state, which is sound only
	// under the first branch — and the card says plainly that NOTHING MECHANICAL
	// enforces it: the code_hash CHECK tests the hash's shape, and the hash of a
	// 6-digit code is 64 hex characters just like this one's. So the obligation
	// is here, in a named constant with this comment attached: shrinking it, or
	// replacing the generator with a human-readable alphabet, breaks the schema's
	// stated assumption and requires the lockout migration first.
	codeBytes = 32

	// codeLen is the exact character length of codeBytes in unpadded base64url:
	// ceil(32*8/6) = 43. A cheap, allocation-free shape gate before any HMAC work
	// happens on an untrusted query parameter or cookie.
	codeLen = 43

	// hmacKeyLen is the required TAPPA_INVITE_HMAC_KEY size. config.Load already
	// enforces it; Manager re-checks so a hand-built Config cannot silently MAC
	// under a short or empty key.
	hmacKeyLen = 32

	// domainLabel is prefixed to the MAC input so an invite MAC and a session MAC
	// over the SAME 43-character string are different values.
	//
	// WHY BOTH A LABEL AND A SEPARATE KEY (the brief asked for one; this uses
	// both, and the reason each is there is different):
	//
	//   * The SEPARATE KEY (TAPPA_INVITE_HMAC_KEY, config.Load) is the
	//     cryptographic answer: with independent keys, holding one MAC oracle or
	//     one leaked key says nothing about the other credential type. A shared
	//     key would make the two systems' security a single event.
	//   * The LABEL is the answer to the OPERATIONAL failure the key cannot
	//     cover: two env vars filled from the same `openssl rand -base64 32`
	//     output by copy-paste. config.keySeparation rejects the exact-equality
	//     case at startup, but a label makes the collapse harmless even if some
	//     future path derives both keys from one source. Without it, session and
	//     invite hashing would be the SAME FUNCTION, and a value that is a valid
	//     session token would hash to a valid invite lookup key — the substitution
	//     the brief names.
	//
	// The label ends with a byte that cannot occur in base64url, so
	// label||code is unambiguous (no length-extension of the label into the code).
	// Changing this string invalidates every stored code_hash, which is why it
	// carries a version: a future scheme becomes "tappa/invite/v2|".
	domainLabel = "tappa/invite/v1|"

	// redacted is what every printing path emits instead of the value. It does
	// NOT contain the value, a prefix of it, or its length.
	redacted = "invite.Code(redacted)"
)

// errMalformed is the internal shape error for a value that cannot be a code we
// issued. Callers never see it: Manager maps it to ErrUnknownCode, because a
// junk code and an unknown code are the same thing to the activation page — and
// because surfacing a parse error risks quoting the value.
var errMalformed = errors.New("invite: code has the wrong shape")

// Code is a raw activation code in transit. It exists between newCode and the
// delivery channel, or between reading the request and hashing it — never
// longer, and never in the database (only its hash is stored, 00009).
//
// The field is a POINTER on purpose (file header, mechanism 2).
//
// ⚠️ COMPARISON IS IDENTITY, NOT VALUE — same trap as session.Token. The type
// compiles as comparable, but the POINTER is compared, so two Codes wrapping the
// same string are `!=`. Consequences are fail-CLOSED (a false non-match), and
// this package compares no Code anywhere: equality is decided by the DATABASE on
// code_hash.
//
// The zero Code is safe to hold and to print: every method tolerates a nil
// field and hash rejects it as errMalformed.
type Code struct{ v *string }

// Compile-time proof that each redaction interface is implemented. If a Go
// upgrade or a refactor drops one, the build breaks here rather than a code
// silently appearing in a log line (§4.7).
var (
	_ fmt.Formatter          = Code{}
	_ fmt.Stringer           = Code{}
	_ fmt.GoStringer         = Code{}
	_ slog.LogValuer         = Code{}
	_ encoding.TextMarshaler = Code{}
)

// ParseCode wraps an untrusted string (a query parameter or a cookie value) in a
// Code. It does NOT validate: the shape gate lives in hash, so there is exactly
// one place that decides what a well-formed code looks like. The parameter is
// taken by value and the Code points at that copy, so the caller's storage is
// never aliased.
//
// Exported because the HTTP layer must be able to build a Code from a request
// without this package importing net/http.
func ParseCode(v string) Code { return Code{v: &v} }

// reveal returns the raw value, or "" for the zero Code. Deliberately
// UNEXPORTED: its call sites are hash (below), the delivery channel (channel.go,
// the single egress) and this package's own tests.
func (c Code) reveal() string {
	if c.v == nil {
		return ""
	}
	return *c.v
}

// Format implements fmt.Formatter, which fmt consults BEFORE Stringer and for
// EVERY verb — %x, %q, %d and any future verb included.
func (Code) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redacted)) }

// String implements fmt.Stringer for direct .String() calls.
func (Code) String() string { return redacted }

// GoString covers %#v, which uses GoStringer rather than Stringer.
func (Code) GoString() string { return redacted }

// LogValue implements slog.LogValuer so log/slog renders the placeholder.
func (Code) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalText implements encoding.TextMarshaler, which encoding/json prefers for
// a value type.
func (Code) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// newCode draws codeBytes of cryptographic randomness and encodes it as unpadded
// base64url: URL-safe (it travels in a link), cookie-safe, no '=' to re-encode,
// fixed length. crypto/rand is the only source; math/rand would make every
// pending invitation predictable from one observed code.
//
// A crypto/rand failure is returned, never ignored (§7).
func newCode() (Code, error) {
	b := make([]byte, codeBytes)
	if _, err := rand.Read(b); err != nil {
		return Code{}, fmt.Errorf("invite: read randomness: %w", err)
	}
	return ParseCode(base64.RawURLEncoding.EncodeToString(b)), nil
}

// hash returns the hex-encoded HMAC-SHA256 of domainLabel||code under key — the
// value stored in employee_invites.code_hash, whose CHECK requires exactly
// ^[0-9a-f]{64}$ (00009).
//
// HMAC, not a bare digest: a stolen database dump of bare SHA-256 hashes could
// be attacked offline by anyone, while an HMAC is unforgeable and unsearchable
// without TAPPA_INVITE_HMAC_KEY, which lives only in the process environment.
//
// The value is hashed as its STRING bytes, not its decoded bytes, so the mapping
// code -> code_hash is injective: base64 permits non-canonical trailing bits, and
// decoding first would let two distinct strings share one hash. DecodeString is
// still used as a charset gate.
//
// SHAPE GATE FIRST: an untrusted value is length- and charset-checked before any
// HMAC work, so junk costs nothing and never reaches the database.
//
// Errors carry the key length at most, never key or code bytes (§4.7).
func (c Code) hash(key []byte) (string, error) {
	if len(key) != hmacKeyLen {
		return "", fmt.Errorf("invite: hmac key must be %d bytes, got %d", hmacKeyLen, len(key))
	}
	// reveal() is "" for the zero Code, which fails the length gate below — a nil
	// field can never reach the decoder or the MAC.
	v := c.reveal()
	if len(v) != codeLen {
		return "", errMalformed
	}
	if _, err := base64.RawURLEncoding.DecodeString(v); err != nil {
		return "", errMalformed
	}
	mac := hmac.New(sha256.New, key)
	// hash.Hash.Write never returns an error (documented). The label goes in
	// FIRST: this is the whole domain separation, and writing it after the value
	// would make the construction depend on the value's length.
	_, _ = mac.Write([]byte(domainLabel))
	_, _ = mac.Write([]byte(v))
	return hex.EncodeToString(mac.Sum(nil)), nil
}
