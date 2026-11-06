package config

import (
	"encoding"
	"fmt"
	"log/slog"
)

// Key and DSN are the two shapes a secret takes in Config (backlog T80, CLAUDE.md §4.7): a key in
// the clear, and a connection string that carries a password. They are operatorauth.Key's and
// mail.Credential's pattern: the five printing methods, and the value behind a *string in a
// one-field struct. operatorauth.Key cannot be reused here: internal/db imports this package and
// operatorauth imports internal/db.
//
// 🔴 WHY THE FIELDS AND NOT Config ITSELF (measured with a stand-alone copy of the shape, 2026-11-06,
// 76 renderings). Format + String + GoString + LogValue + MarshalJSON on the struct, over raw
// []byte and string fields, leaked in 28 of them, because fmt reaches no method on these paths:
// a Config in a caller's UNEXPORTED field (any verb), %p, %w (Sprintf and Errorf, value and pointer),
// and one field read out and printed on its own (`slog.Any("k", cfg.TagKEK)`), which no method on
// the struct can see at all. The same renderings over these two field types: 0 of 76.
// TestConfig_NoRenderingCarriesASecret keeps the table.
//
// WHY *string (operatorauth.Key's measurement): a `type Key []byte` closes none of those paths, a
// struct over *[]byte leaks under %s %q %e %f %t %c %U because fmt's badVerb opens a pointer to a
// slice, and fmt does not open a *string: it prints an address.
//
// The redacted text is a constant: no prefix, no length, no host or user part of a DSN. Parsing a
// DSN to show its harmless half would put a parser between the password and the log, and pgconn's
// own redactor misses a `?password=` (TestNew_AnUnparseableDSNCarriesNoPassword measures it).
//
// KNOWN, ACCEPTED LIMIT -- do not restate as "impossible": Reveal hands the value to whoever calls
// it, and deliberate reflection, a debugger and a core dump still read it. The name exists so that
// `grep -rn '\.Reveal()'` is the full list of places a secret leaves its wrapper.
type (
	Key struct{ v *string }
	DSN struct{ v *string }
)

const (
	keyRedacted = "config.Key(redacted)"
	dsnRedacted = "config.DSN(redacted)"
)

var (
	_ fmt.Formatter          = Key{}
	_ fmt.Stringer           = Key{}
	_ fmt.GoStringer         = Key{}
	_ slog.LogValuer         = Key{}
	_ encoding.TextMarshaler = Key{}

	_ fmt.Formatter          = DSN{}
	_ fmt.Stringer           = DSN{}
	_ fmt.GoStringer         = DSN{}
	_ slog.LogValuer         = DSN{}
	_ encoding.TextMarshaler = DSN{}
)

// NewKey wraps a COPY of b, so the caller's slice is not aliased. A nil or empty b is the zero Key,
// which is how an optional key reads as unset.
func NewKey(b []byte) Key {
	if len(b) == 0 {
		return Key{}
	}
	v := string(b)
	return Key{v: &v}
}

// Reveal returns a fresh copy of the key for one use, at the call that needs a []byte (an HMAC, a
// seal, a separation check), or nil for the zero Key. A fresh copy is why a caller that writes into
// the result cannot change the process's key.
func (k Key) Reveal() []byte {
	if k.v == nil {
		return nil
	}
	return []byte(*k.v)
}

// Len is the key's length in bytes, for a size check and for "is it set"; no caller has to reveal
// the key to ask either.
func (k Key) Len() int {
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

// NewDSN wraps a connection string. An empty one is the zero DSN.
func NewDSN(s string) DSN {
	if s == "" {
		return DSN{}
	}
	return DSN{v: &s}
}

// Reveal returns the connection string, or "" for the zero DSN. Its callers are the two pool
// constructors; pgx parses the value and nothing else reads it.
func (d DSN) Reveal() string {
	if d.v == nil {
		return ""
	}
	return *d.v
}

func (DSN) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte(dsnRedacted)) }
func (DSN) String() string               { return dsnRedacted }
func (DSN) GoString() string             { return dsnRedacted }
func (DSN) LogValue() slog.Value         { return slog.StringValue(dsnRedacted) }
func (DSN) MarshalText() ([]byte, error) { return []byte(dsnRedacted), nil }
