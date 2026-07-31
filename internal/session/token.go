package session

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

// Token generation and keyed hashing — the two halves of "proof of person"
// (CLAUDE.md §5): a value only the right phone holds, and a database column that
// is useless to whoever reads it.
//
// KIRMIZI ÇİZGİ §4.7 — the raw value must not reach a log line, an error string
// or a serialised payload. Two mechanisms, and it is worth being precise about
// what each one actually buys, because an earlier version of this comment
// overclaimed and an independent audit proved it wrong:
//
//  1. REDACTING METHODS (fmt.Formatter for every verb, Stringer, GoStringer,
//     slog.LogValuer, encoding.TextMarshaler). These work whenever the printer
//     can reach the Token as an interface: a bare Token, a *Token, a slice or
//     map of Tokens, an EXPORTED struct field.
//
//  2. INDIRECTION (the field is a *string, not a string). This is what covers
//     the case the methods CANNOT: fmt only consults Formatter/Stringer for a
//     value it may hand to an interface, and a value read out of an UNEXPORTED
//     struct field may not be (reflect.Value.CanInterface() == false). fmt then
//     falls through to plain reflection and prints the field's contents. With a
//     string field, `%+v` on a handler-local `struct{ tok Token }` printed the
//     session value verbatim — ordinary Go, and exactly what M5-02/M5-03
//     handlers will write. With a *string, that same path prints an ADDRESS,
//     which carries none of the value.
//
// KNOWN, ACCEPTED LIMIT — do not restate this as "impossible". Deliberate
// reflection still reads it: reflect.ValueOf(tok).Field(0).Elem().String()
// returns the value, because reflect's typed getters work on read-only Values.
// A debugger or a core dump reads it too. Closing that would need the value
// masked with a process-wide random pad, i.e. package-level mutable state, which
// §7 forbids ("paket seviyesi singleton yok") — and it would buy nothing against
// the actual threat, which is ACCIDENTAL rendering, not a developer who has
// decided to extract a secret (inside this package, `reveal` already does that
// in one line). The guarantee this file makes is therefore precise: no ordinary
// rendering path — fmt with any verb, log/slog, encoding/json, text/template,
// error wrapping, panic recovery — emits the value, whether the Token sits in an
// exported field, an unexported field, a slice, a map or an error chain. That
// guarantee is tested from inside the package AND from an external test package
// (leak_external_test.go), which is where the earlier claim broke.

const (
	// tokenBytes is the entropy behind one session token. The card requires
	// >= 32 bytes of cryptographic randomness; 32 bytes = 256 bits is the size
	// at which guessing is not a threat model any more.
	tokenBytes = 32
	// tokenLen is the exact character length of a tokenBytes token in
	// unpadded base64url: ceil(32*8/6) = 43. Used as a cheap, allocation-free
	// shape gate before any hashing work happens on an untrusted cookie value.
	tokenLen = 43
	// hmacKeyLen is the required TAPPA_SESSION_HMAC_KEY size. internal/config
	// already enforces it at startup; Manager re-checks so a hand-built Config
	// (tests, future wiring) cannot silently produce HMACs under a short or
	// empty key.
	hmacKeyLen = 32
	// redacted is what every printing path emits instead of the value. It does
	// NOT contain the value, a prefix of it, or its length.
	redacted = "session.Token(redacted)"
)

// errMalformed is the internal shape error for a cookie value that cannot be a
// token we issued. Callers never see it: Verify maps it to ErrNoSession, because
// an unparseable cookie and an absent cookie are the same thing to the tap flow
// (§5 row 3) — and because surfacing a parse error risks quoting the value.
var errMalformed = errors.New("session: cookie value has the wrong shape")

// Token is a raw session token in transit. It exists between newToken and the
// Set-Cookie write, or between reading the request cookie and hashing it — never
// longer, and never in the database.
//
// The field is a POINTER on purpose (file header, mechanism 2): it is what keeps
// `%+v` on a CALLER's unexported field from printing the value.
//
// ⚠️ COMPARISON IS IDENTITY, NOT VALUE. The type still COMPILES as comparable —
// `==` and `map[Token]…` are legal — but the pointer is what gets compared, so
// two Tokens wrapping the SAME string are `!=` and occupy two map entries
// (measured: `wrap("x") == wrap("x")` is false, and a map keyed on both has
// len 2). Before the indirection they were equal and collapsed to one entry.
// Consequences are fail-CLOSED (a false non-match, never a false match), so this
// is a trap rather than a hole — but do not compare Tokens to decide anything.
// Equality of session values is decided by the DATABASE, on token_hash: hash the
// value and look it up (Manager.Verify). This package compares no Token anywhere.
//
// The zero Token is safe to hold and to print: every method below tolerates a nil
// field, hash rejects it as errMalformed, and Cookies.Set refuses it.
type Token struct{ v *string }

// Compile-time proof that each redaction interface is actually implemented — all
// FIVE, encoding.TextMarshaler included. If a Go upgrade or a refactor drops one,
// the build breaks here rather than a value silently appearing in a log line
// (§4.7). These assertions cover mechanism 1 only; the unexported-field case is
// covered by the pointer field above, and both are exercised from an external
// test package in leak_external_test.go.
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

// reveal returns the raw value, or "" for the zero Token. Deliberately
// UNEXPORTED and deliberately the only reader: its call sites are hash (below),
// Cookies.Set (the single egress, into a Set-Cookie header) and this package's
// own tests. Nothing outside internal/session can call it.
func (t Token) reveal() string {
	if t.v == nil {
		return ""
	}
	return *t.v
}

// Format implements fmt.Formatter, which fmt consults BEFORE Stringer and for
// EVERY verb — so %x, %q, %d and any future verb are covered, not just %v/%s.
// This is the widest of the redaction hooks, and it applies whenever fmt can
// reach the Token as an interface value.
func (Token) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redacted)) }

// String implements fmt.Stringer for direct .String() calls and for the string
// conversions fmt does not route through Format.
func (Token) String() string { return redacted }

// GoString covers %#v, which uses GoStringer rather than Stringer.
func (Token) GoString() string { return redacted }

// LogValue implements slog.LogValuer so log/slog renders the placeholder even
// when a Token is passed as an attribute value or nested in a logged struct.
func (Token) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalText implements encoding.TextMarshaler, which encoding/json prefers for
// a value type — so a Token reached through a JSON response or a structured log
// encoder serialises to the placeholder too.
func (Token) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// newToken draws tokenBytes of cryptographic randomness and encodes it as
// unpadded base64url: URL- and cookie-safe, no '=' to be re-encoded, fixed
// length. crypto/rand is the only source used; math/rand would make sessions
// predictable from one observed token.
//
// A crypto/rand failure is returned, never ignored (§7): issuing a session on a
// degraded entropy source would be worse than failing the activation.
func newToken() (Token, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return Token{}, fmt.Errorf("session: read randomness: %w", err)
	}
	return wrap(base64.RawURLEncoding.EncodeToString(b)), nil
}

// hash returns the hex-encoded HMAC-SHA256 of the token under key — the value
// stored in sessions.token_hash. HMAC, not a bare SHA-256: a bare digest of a
// stolen database dump could be attacked offline by anyone, while an HMAC is
// unforgeable and unsearchable without TAPPA_SESSION_HMAC_KEY, which lives only
// in the process environment.
//
// The value is hashed as its STRING bytes, not as its decoded bytes, so the
// mapping cookie-value -> token_hash is injective: base64 permits non-canonical
// trailing bits, and decoding first would let two distinct cookie strings share
// one hash. DecodeString is still used as a charset gate.
//
// SHAPE GATE FIRST: an untrusted cookie is length-checked and charset-checked
// before any HMAC work, so a huge or junk cookie costs nothing and never reaches
// the database.
//
// Errors carry the key length at most, never key or token bytes (§4.7).
func (t Token) hash(key []byte) (string, error) {
	if len(key) != hmacKeyLen {
		return "", fmt.Errorf("session: hmac key must be %d bytes, got %d", hmacKeyLen, len(key))
	}
	// reveal() is "" for the zero Token, which fails the length gate below — a nil
	// field can never reach the decoder or the MAC.
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
