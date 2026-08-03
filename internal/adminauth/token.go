package adminauth

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

// The admin session token: the panel's half of "who is this request".
//
// IT IS A COPY OF internal/session's Token, ON PURPOSE, and the duplication is
// argued rather than apologised for. The two types differ in exactly the things
// that must differ — the HMAC key (derived, below), the table the hash is stored
// in (admin_sessions, not sessions) and the cookie that carries it — and sharing
// one type would mean one key and one shape for two credentials that migration
// 00011 and M1-11 went to some trouble to keep structurally apart. A shared
// generic would also have to be parameterised on the redaction placeholder, which
// is the one string the leak tests grep for.
//
// KIRMIZI CIZGI section 4.7. The raw value must not reach a log line, an error
// string or a serialised payload, and the two mechanisms are internal/session's,
// verbatim, for the reasons measured there:
//
//  1. REDACTING METHODS (fmt.Formatter for every verb, Stringer, GoStringer,
//     slog.LogValuer, encoding.TextMarshaler) cover every path where a printer
//     reaches the value as an interface.
//  2. INDIRECTION (a *string field) covers the path the methods CANNOT: fmt skips
//     Formatter/Stringer for a value read out of an UNEXPORTED struct field
//     (reflect CanInterface() == false) and falls through to printing the field's
//     contents. A handler writing `struct{ tok Token }` and logging it with %+v is
//     exactly that shape.
//
// KNOWN, ACCEPTED LIMIT — not "impossible". Deliberate reflection, a debugger and
// a core dump all still read it, and reveal does it in one line inside this
// package. The guarantee is about ACCIDENTAL rendering and is tested from an
// external test package (leak_external_test.go), which is where the equivalent
// claim on session.Token broke in M5-01.

const (
	// tokenBytes is the entropy behind one panel session token. 32 bytes = 256
	// bits, the size at which guessing stops being a threat model.
	tokenBytes = 32
	// tokenLen is the exact character length of tokenBytes in unpadded base64url:
	// ceil(32*8/6) = 43. A cheap shape gate before any HMAC work runs on an
	// untrusted cookie value.
	tokenLen = 43
	// hmacKeyLen is the required TAPPA_SESSION_HMAC_KEY size, re-checked here so a
	// hand-built Config cannot produce hashes under a short or empty key.
	hmacKeyLen = 32
	// redacted is what every printing path emits. It contains neither the value,
	// nor a prefix of it, nor its length.
	//
	// IT IS DELIBERATELY DIFFERENT FROM session.Token's placeholder. A leak test
	// that greps for "session.Token(redacted)" would pass over an adminauth value
	// that leaked, and vice versa; two distinct strings keep the two suites from
	// vouching for each other.
	redacted = "adminauth.Token(redacted)"

	// adminTokenKeyLabel derives THIS credential's HMAC key from the session HMAC
	// key.
	//
	// WHY DERIVE RATHER THAN ADD A FOURTH REQUIRED ENV VAR. The repo already
	// derives one value this way (handler.tapContextKeyLabel) and argues the case:
	// a new required variable means every deployment, and CI, refusing to boot
	// until an operator sets it. HMAC is one-way, so this is not key sharing —
	// holding this derived key does not reveal TAPPA_SESSION_HMAC_KEY and cannot
	// forge an employee session hash.
	//
	// WHAT DERIVING BUYS, concretely and testably: the SAME raw token string hashes
	// to a DIFFERENT value for the panel than for the employee flow, so a row in
	// `sessions` can never be matched by admin_sessions' resolver even if somebody
	// copied the value across (TestAdminAndEmployeeHashes_Differ pins it). Without
	// the label the two hashes would be identical and the separation would rest
	// entirely on the two tables never being queried by the wrong function.
	//
	// LIMIT, in the honest direction: the implication runs one way. Whoever holds
	// TAPPA_SESSION_HMAC_KEY can derive this one. A leaked session key is already a
	// total compromise of employee identity, so a derivable admin key is not the
	// first thing to worry about — but the two are NOT independent, and a future
	// credential that needs real independence should get its own variable rather
	// than another label here.
	adminTokenKeyLabel = "tappa/admin-session/v1/key-derivation"
)

// errMalformed is the internal shape error for a cookie value that cannot be a
// token we issued. Callers never see it: Verify maps it to ErrNoSession, so an
// unparseable cookie and an absent cookie are the same thing to the panel — and
// so no error string can ever quote the value.
var errMalformed = errors.New("adminauth: cookie value has the wrong shape")

// Token is a raw admin session token in transit: between newToken and the
// Set-Cookie write, or between reading the request cookie and hashing it. Never
// longer, and never in the database.
//
// ⚠️ COMPARISON IS IDENTITY, NOT VALUE. The type still compiles as comparable, but
// the POINTER is compared, so two Tokens wrapping the same string are `!=`.
// Consequences are fail-CLOSED (a false non-match, never a false match) and
// nothing in this package compares one. Equality of session values is decided by
// the DATABASE, on token_hash.
//
// The zero Token is safe to hold and to print: every method tolerates a nil field,
// hash rejects it as errMalformed, and Cookies.Set refuses it.
type Token struct{ v *string }

// Compile-time proof that each redaction interface is implemented — all FIVE. If
// a Go upgrade or a refactor drops one, the build breaks here rather than a token
// silently appearing in a log line.
var (
	_ fmt.Formatter          = Token{}
	_ fmt.Stringer           = Token{}
	_ fmt.GoStringer         = Token{}
	_ slog.LogValuer         = Token{}
	_ encoding.TextMarshaler = Token{}
)

// wrap builds a Token around an untrusted string (a request cookie value). The
// parameter is taken by value and the Token points at that copy, so the caller's
// storage is never aliased.
func wrap(v string) Token { return Token{v: &v} }

// reveal returns the raw value, or "" for the zero Token. Deliberately UNEXPORTED
// and deliberately the only reader: its call sites are hash, Cookies.Set (the
// single egress, into a Set-Cookie header) and this package's own tests.
func (t Token) reveal() string {
	if t.v == nil {
		return ""
	}
	return *t.v
}

// Format implements fmt.Formatter, which fmt consults BEFORE Stringer and for
// EVERY verb — %x, %q, %d and any future verb included.
func (Token) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redacted)) }

// String implements fmt.Stringer for direct .String() calls and the string
// conversions fmt does not route through Format.
func (Token) String() string { return redacted }

// GoString covers %#v, which uses GoStringer rather than Stringer.
func (Token) GoString() string { return redacted }

// LogValue implements slog.LogValuer so log/slog renders the placeholder even
// when the value is an attribute or nested in a logged struct.
func (Token) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalText implements encoding.TextMarshaler, which encoding/json prefers for
// a value type.
func (Token) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// newToken draws tokenBytes of cryptographic randomness and encodes it as
// unpadded base64url: URL- and cookie-safe, fixed length, no '=' to re-encode.
// A crypto/rand failure is returned, never ignored (section 7).
func newToken() (Token, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return Token{}, fmt.Errorf("adminauth: read randomness: %w", err)
	}
	return wrap(base64.RawURLEncoding.EncodeToString(b)), nil
}

// deriveKey produces this package's HMAC key from the session HMAC key.
func deriveKey(sessionKey []byte) ([]byte, error) {
	if len(sessionKey) != hmacKeyLen {
		return nil, fmt.Errorf("adminauth: hmac key must be %d bytes, got %d", hmacKeyLen, len(sessionKey))
	}
	m := hmac.New(sha256.New, sessionKey)
	_, _ = m.Write([]byte(adminTokenKeyLabel)) // hash writes never fail (documented)
	return m.Sum(nil), nil
}

// hash returns the hex-encoded HMAC-SHA256 of the token under key — the value
// stored in admin_sessions.token_hash.
//
// HMAC, not a bare SHA-256: a bare digest in a stolen dump could be attacked
// offline by anyone, while an HMAC is unforgeable and unsearchable without the
// key, which lives only in the process environment.
//
// The value is hashed as its STRING bytes, not its decoded bytes, so the mapping
// cookie-value -> token_hash is injective (base64 permits non-canonical trailing
// bits, and decoding first would let two distinct cookie strings share one hash).
// DecodeString is still used as a charset gate.
//
// SHAPE GATE FIRST: an untrusted cookie is length- and charset-checked before any
// HMAC work, so a huge or junk cookie costs nothing and never reaches the
// database. Errors carry the key length at most, never key or token bytes.
func (t Token) hash(key []byte) (string, error) {
	if len(key) != hmacKeyLen {
		return "", fmt.Errorf("adminauth: hmac key must be %d bytes, got %d", hmacKeyLen, len(key))
	}
	v := t.reveal()
	if len(v) != tokenLen {
		return "", errMalformed
	}
	if _, err := base64.RawURLEncoding.DecodeString(v); err != nil {
		return "", errMalformed
	}
	mac := hmac.New(sha256.New, key)
	// hash.Hash.Write never returns an error (documented); the value is written
	// as-is with no formatting call, so it cannot reach a log through here.
	_, _ = mac.Write([]byte(v))
	return hex.EncodeToString(mac.Sum(nil)), nil
}
