package operatorauth

import (
	"encoding"
	"fmt"
	"log/slog"
)

// Key is a key in the clear: TAPPA_OPERATOR_TOTP_KEK and TAPPA_OPERATOR_TOKEN_HMAC_KEY
// as OP-7 hands them over (Config), and the Authenticator's own copies, derived key and
// dummy digest. It is held as every redacting type in this repository holds its
// secret: the five printing methods, and the bytes behind a *string in a one-field
// struct.
//
// WHY A TYPE AND WHY *string (OP-6 verification, 7th round, the 6th auditor
// measured): with the keys as plain []byte fields, a Config VALUE in a caller's
// unexported struct field, a Config under %p, and a Config or *Config under %w
// (Sprintf and Errorf) printed both keys by reflection -- fmt reaches no method on
// those paths. A type over []byte (`type Key []byte`) closes none of them; a struct over
// *[]byte leaks under %s %q %e %f %t %c %U, where fmt's badVerb opens a pointer once
// and a *[]byte opens to the bytes. A *string is not opened (fmt opens a pointer to an
// array, a slice, a struct or a map only): it prints as an address.
//
// There is no exported accessor. The operator's two keys sit in internal/config's Config
// as raw []byte like every other key there (internal/db imports internal/config, so
// config cannot import this package and cannot hold a Key); OP-7's start-up refusal
// "these keys differ from every other key" runs there, on the raw values
// (config.keySeparation's precedent), and cmd/tappa's wiring converts them with NewKey.
// Nothing outside this package needs to read a Key back.
type Key struct{ v *string }

const keyRedacted = "operatorauth.Key(redacted)"

var (
	_ fmt.Formatter          = Key{}
	_ fmt.Stringer           = Key{}
	_ fmt.GoStringer         = Key{}
	_ slog.LogValuer         = Key{}
	_ encoding.TextMarshaler = Key{}
)

// NewKey wraps a COPY of b; the caller's slice is not aliased and may be cleared.
func NewKey(b []byte) Key {
	v := string(b)
	return Key{v: &v}
}

// bytes returns a fresh copy of the key for ONE use, at the call that needs a []byte
// (an HMAC, sun.Seal/Open, a bcrypt comparison), or nil for the zero Key. The key
// itself lives for the process in the Authenticator (the custody internal/config has
// for every key), so a transient copy adds no exposure the string does not already
// have.
func (k Key) bytes() []byte {
	if k.v == nil {
		return nil
	}
	return []byte(*k.v)
}

// size is the key's length in bytes, for New's size check.
func (k Key) size() int {
	if k.v == nil {
		return 0
	}
	return len(*k.v)
}

func (Key) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte(keyRedacted)) }
func (Key) String() string               { return keyRedacted }
func (Key) GoString() string             { return keyRedacted }
func (Key) LogValue() slog.Value         { return slog.StringValue(keyRedacted) }
func (Key) MarshalText() ([]byte, error) { return []byte(keyRedacted), nil }
