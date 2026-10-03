package mail

import (
	"encoding"
	"fmt"
	"log/slog"
)

// redacted is what every printing path emits instead of a Credential's value. It
// carries neither the value, nor a prefix of it, nor its length.
const redacted = "mail.Credential(redacted)"

// Credential is an SMTP credential in transit, from the environment to the AUTH
// PLAIN exchange: the password, and the username too — for SES the username is
// an AWS access key id. It is the deliberate twin of invite.Code
// (internal/invite/code.go) and repeats its two redaction mechanisms; the
// reasoning there applies verbatim and is only summarised here:
//
//  1. REDACTING METHODS (fmt.Formatter for every verb, Stringer, GoStringer,
//     slog.LogValuer, encoding.TextMarshaler) cover every path where a printer can
//     reach the value as an interface.
//  2. INDIRECTION (the field is a *string) covers the path the methods cannot: a
//     value read out of an UNEXPORTED struct field is not interface-able, so fmt
//     falls through to reflection and prints the field. With a *string it prints a
//     POINTER ADDRESS (measured: "%+v" of a *SMTP shows password:{v:0xc...}) — an
//     address, not the value and not a prefix of it. This package keeps its
//     Credentials in exactly such fields (SMTP), and a caller that prints its own
//     config struct with %+v reaches them the same way.
//
// What is measured, and how: TestCredential_IsRedactedInEveryRendering lists the
// renderings it drives and asserts that neither the value nor its first 8
// characters appears in any of them. KNOWN, ACCEPTED LIMIT — do not restate as
// "impossible": deliberate reflection, a debugger and a core dump still read the
// value (TestCredential_KnownLimitIsReflection measures the first).
//
// The zero Credential is safe to hold and to print; New refuses it.
type Credential struct{ v *string }

// Compile-time proof that each redaction interface is implemented: if a refactor
// drops one, the build breaks here rather than the value appearing in a log line.
var (
	_ fmt.Formatter          = Credential{}
	_ fmt.Stringer           = Credential{}
	_ fmt.GoStringer         = Credential{}
	_ slog.LogValuer         = Credential{}
	_ encoding.TextMarshaler = Credential{}
)

// NewCredential wraps a value read from the environment. The parameter is taken
// by value and the Credential points at that copy, so the caller's storage is
// never aliased.
func NewCredential(v string) Credential { return Credential{v: &v} }

// reveal returns the raw value, or "" for the zero Credential. Deliberately
// UNEXPORTED; its call sites are New's checks, the AUTH PLAIN exchange and the
// Message-ID echo list in Send — all in this package. No test calls it.
func (c Credential) reveal() string {
	if c.v == nil {
		return ""
	}
	return *c.v
}

// Format implements fmt.Formatter, which fmt consults BEFORE Stringer and
// GoStringer and for EVERY verb — %x, %q, %d and any future verb included.
func (Credential) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redacted)) }

// String implements fmt.Stringer for direct .String() calls.
func (Credential) String() string { return redacted }

// GoString implements fmt.GoStringer for direct .GoString() calls.
func (Credential) GoString() string { return redacted }

// LogValue implements slog.LogValuer: a handler that resolves the value receives
// the placeholder string, not the struct.
func (Credential) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalText implements encoding.TextMarshaler, which encoding/json prefers for
// a value type.
func (Credential) MarshalText() ([]byte, error) { return []byte(redacted), nil }
