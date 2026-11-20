// Package pairing is the manager-approved activation's six-digit pairing code (M16-23) as a type
// that prints redacted on every path but Digits. It is a leaf package so that internal/invite (the
// phone's half) and internal/domain/tenant (the panel's half) can both carry it without one
// importing the other -- invite's tests already import tenant.
package pairing

import (
	"encoding"
	"fmt"
	"log/slog"
)

// Code is the six digits the phone shows and the panel lists. It is NOT a credential -- the
// approval is the manager's press -- but the card keeps it out of every log line and audit detail,
// so it prints redacted on every path and Digits is its one egress (the phone's screen, the panel's
// row).
type Code struct{ v *string }

var (
	_ fmt.Formatter          = Code{}
	_ fmt.Stringer           = Code{}
	_ fmt.GoStringer         = Code{}
	_ slog.LogValuer         = Code{}
	_ encoding.TextMarshaler = Code{}
)

const redacted = "invite.PairingCode(redacted)"

// Parse wraps six digits read from the database.
func Parse(v string) Code { return Code{v: &v} }

func (Code) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte(redacted)) }
func (Code) String() string               { return redacted }
func (Code) GoString() string             { return redacted }
func (Code) LogValue() slog.Value         { return slog.StringValue(redacted) }
func (Code) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// Digits is the code's one egress: the screen that shows it.
func (c Code) Digits() string {
	if c.v == nil {
		return ""
	}
	return *c.v
}
