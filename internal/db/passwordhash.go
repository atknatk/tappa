package db

import (
	"encoding"
	"fmt"
	"log/slog"
)

// PasswordHash is the stored KDF digest of an admin's password, in transit between
// the login resolver and the one comparison that may read it. It is the THIRD
// application of the redaction pattern this repo already carries twice --
// internal/session.Token (M5-01) and internal/invite.Code (M5-02) -- and it repeats
// their two mechanisms rather than inventing a variation:
//
//  1. REDACTING METHODS (fmt.Formatter for EVERY verb, Stringer, GoStringer,
//     slog.LogValuer, encoding.TextMarshaler). These cover every path where a
//     printer can reach the value as an interface: a bare PasswordHash, a pointer,
//     a slice or map of them, an EXPORTED struct field -- which is what
//     ResolvedAdmin.PasswordHash is.
//  2. INDIRECTION (the field is a *string, not a string). This covers the path the
//     methods CANNOT. fmt only consults Formatter/Stringer for a value it may hand
//     to an interface, and a value reached through an UNEXPORTED struct field may
//     not be (reflect.Value.CanInterface() == false); fmt then falls through to
//     plain reflection and prints the field's contents. That was a MEASURED audit
//     finding on session.Token in M5-01 round 2, not a theory, and a phase-B login
//     handler writing `struct{ admin db.ResolvedAdmin }` and logging it with %+v is
//     exactly the shape that hits it. With a *string it prints an address, which
//     carries none of the value.
//
// WHY THIS TYPE AT ALL -- WHAT LEAKS AND WHAT IT COSTS. The value is a bcrypt
// digest of the panel OWNER's password. A digest in a log file is an OFFLINE
// cracking target: no rate limit applies, no lockout, no audit trail, and success
// yields the account that can read every employee's movements and rewrite the
// tenant's policies. That is a different economy from a leaked session token, which
// expires and can be revoked. Six ordinary rendering paths printed the raw digest
// while this was a bare `string` (%+v · %v over a slice · %#v · fmt.Errorf ·
// through an unexported field · slog); all six are measured, in both directions,
// in resolve_leak_external_test.go.
//
// KNOWN, ACCEPTED LIMIT -- do not restate this as "impossible", which is the exact
// overclaim an audit refuted on session.Token. Deliberate reflection
// (reflect.ValueOf(h).Field(0).Elem().String()), a debugger and a core dump all
// still read it, and RevealForPasswordComparison reads it in one line by design.
// The guarantee is precise and about ACCIDENTAL rendering: no ordinary printing
// path -- fmt with any verb, log/slog, encoding/json, text/template, error
// wrapping, panic recovery -- emits the digest, whether the value sits in an
// exported field, an unexported field, a slice, a map or an error chain.
//
// ⚠️ COMPARISON IS IDENTITY, NOT VALUE -- and it now infects ResolvedAdmin, which
// embeds this type. Both still COMPILE as comparable, but the POINTER is what `==`
// compares, so two PasswordHash values over the same digest are `!=` and two
// ResolvedAdmin values scanned from the same row are `!=`. Consequences are
// fail-CLOSED (a false non-match, never a false match), so this is a trap rather
// than a hole -- but nothing may decide anything by comparing these values, and
// nothing does. Password equality is decided by the KDF, in phase B.
//
// THE ZERO VALUE IS THE SAFE VALUE, which is the M5-01 polar lesson (a dangerous
// state must not be the state you get by forgetting): the zero PasswordHash carries
// a nil pointer, every method tolerates it, and RevealForPasswordComparison returns
// "" -- and "" is not a valid digest under any KDF, so a comparison against it
// FAILS and the login is refused. A zero ResolvedAdmin is likewise inert end to
// end: its ID and TenantID are uuid.Nil, so CreateAdminSession matches no row and
// returns pgx.ErrNoRows (measured, TestResolvedAdmin_ZeroValueFailsClosed). There
// is no path on which forgetting to populate this struct authenticates anyone.
type PasswordHash struct{ v *string }

// redacted is what every printing path emits instead of the digest. It does NOT
// contain the value, a prefix of it, or its length.
const redacted = "db.PasswordHash(redacted)"

// Compile-time proof that each redaction interface is actually implemented -- all
// FIVE, encoding.TextMarshaler included. If a Go upgrade or a refactor drops one,
// the build breaks here rather than a digest silently appearing in a log line
// (CLAUDE.md section 4.7 / section 7).
var (
	_ fmt.Formatter          = PasswordHash{}
	_ fmt.Stringer           = PasswordHash{}
	_ fmt.GoStringer         = PasswordHash{}
	_ slog.LogValuer         = PasswordHash{}
	_ encoding.TextMarshaler = PasswordHash{}
)

// NewPasswordHash wraps a digest string. Two legitimate callers, and no third:
//
//   - this package's scanner (GetAdminByEmail), which is why it exists at all;
//   - phase B's DUMMY digest -- the fixed constant it must compare against when the
//     resolver returns zero rows, so the response TIME does not answer the question
//     the response BODY refuses to (PHASE B OBLIGATION 2). That constant is a
//     digest like any other and must travel wrapped, not bare.
//
// The parameter is taken by value and the PasswordHash points at that copy, so the
// caller's storage is never aliased.
func NewPasswordHash(v string) PasswordHash { return PasswordHash{v: &v} }

// RevealForPasswordComparison returns the raw digest, or "" for the zero value.
//
// IT IS DELIBERATELY THE ONLY READER AND DELIBERATELY UGLY TO TYPE. Every other
// way out of this type is redacted, so a grep for this name lists every place the
// digest is in the clear -- which must stay at one: the KDF comparison in the
// phase-B login handler. The comparison itself is NOT here on purpose: bcrypt is
// phase B's dependency (go.mod carries no KDF today) and an authentication
// decision does not belong in the data-access layer.
//
// The "" of the zero value is safe rather than convenient: it is not a valid digest
// under any KDF, so a comparison against it fails and the login is refused.
func (h PasswordHash) RevealForPasswordComparison() string {
	if h.v == nil {
		return ""
	}
	return *h.v
}

// Format implements fmt.Formatter, which fmt consults BEFORE Stringer and for EVERY
// verb -- so %x, %q, %d and any future verb are covered, not just %v/%s.
func (PasswordHash) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redacted)) }

// String implements fmt.Stringer for direct .String() calls and for the string
// conversions fmt does not route through Format.
func (PasswordHash) String() string { return redacted }

// GoString covers %#v, which uses GoStringer rather than Stringer.
func (PasswordHash) GoString() string { return redacted }

// LogValue implements slog.LogValuer so log/slog renders the placeholder even when
// the value is passed as an attribute or nested inside a logged struct.
func (PasswordHash) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalText implements encoding.TextMarshaler, which encoding/json prefers for a
// value type -- so a digest reached through a JSON response or a structured log
// encoder serialises to the placeholder too.
func (PasswordHash) MarshalText() ([]byte, error) { return []byte(redacted), nil }
