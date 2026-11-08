package signup

import (
	"encoding"
	"fmt"
	"log/slog"
)

// Address is the account step's e-mail address (Account.Email). CLAUDE.md §7 puts an e-mail address
// on the never-log list (ADR 0022 §10), and a Draft or an Account printed whole carried it: the T205
// audit counted the address in 503 of 538 renderings (backlog T209). It is config.Key's shape for the
// same measured reasons (internal/config/secret.go): the five printing methods on a one-field struct
// over a *string, so a value in a caller's unexported field, %p, %w and a field printed on its own all
// print the placeholder or an address, never the bytes. Methods on Account alone would have left
// those paths open.
//
// Reveal is the address for the places that use it -- validation, the form echoed back, the draft's
// own column through adminauth.SignupClaims.Open -- so `grep -rn 'Email.Reveal()'` lists every place
// it leaves the wrapper.
type Address struct{ v *string }

const addressRedacted = "signup.Address(redacted)"

var (
	_ fmt.Formatter          = Address{}
	_ fmt.Stringer           = Address{}
	_ fmt.GoStringer         = Address{}
	_ slog.LogValuer         = Address{}
	_ encoding.TextMarshaler = Address{}
)

// NewAddress wraps s; "" is the zero Address.
func NewAddress(s string) Address {
	if s == "" {
		return Address{}
	}
	return Address{v: &s}
}

// Reveal returns the address, or "" for the zero Address.
func (a Address) Reveal() string {
	if a.v == nil {
		return ""
	}
	return *a.v
}

func (Address) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte(addressRedacted)) }
func (Address) String() string               { return addressRedacted }
func (Address) GoString() string             { return addressRedacted }
func (Address) LogValue() slog.Value         { return slog.StringValue(addressRedacted) }
func (Address) MarshalText() ([]byte, error) { return []byte(addressRedacted), nil }
