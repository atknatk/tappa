package sun

import (
	"encoding"
	"fmt"
	"log/slog"
)

// WipeableKey is key material that prints redacted AND can be wiped: an EV2 session key, and everything
// in internal/encode's keyring (backlog T205, CLAUDE.md §4.7).
//
// 🔴 WHY NOT config.Key. A config.Key holds a Go string, which nothing can zero, and ADR 0017 §3 makes
// the wipe of these buffers a requirement on every exit path (EV2Auth.Zero, keyring.zeroAll). Moving
// the session keys into one would have traded a %+v that nothing calls for a wipe that stops existing.
//
// WHY A CLOSURE AND NOT A POINTER. config.Key's measurement holds here too: over `[]byte` no method
// reaches a value in a caller's unexported field, and fmt's badVerb opens a pointer to a slice, an array
// or a struct under %s %q %e %f %t %c %U. It does not open a func: every verb prints an address
// (TestWipeableKey_PrintsNoByteOnEveryPath). The bytes live in the closure, where reflection does not
// reach them either.
//
// THE COST. EV2WrapCommandFull, interleaved A/B over eight rounds on the same loaded machine (2026-11-06):
// 1653.5 ns/op before, 1651.5 ns/op after, 13 allocs/op both, so the indirect call does not show above the
// noise of the AES and CMAC around it, and the field type stays. A key costs one closure, at the handshake.
//
// KNOWN, ACCEPTED LIMIT, config.Key's: Reveal hands the buffer to whoever calls it, and a debugger and a
// core dump still read it.
type WipeableKey struct{ live func() []byte }

const wipeableKeyRedacted = "sun.WipeableKey(redacted)"

var (
	_ fmt.Formatter          = WipeableKey{}
	_ fmt.Stringer           = WipeableKey{}
	_ fmt.GoStringer         = WipeableKey{}
	_ slog.LogValuer         = WipeableKey{}
	_ encoding.TextMarshaler = WipeableKey{}
)

// NewWipeableKey takes OWNERSHIP of b, with no copy: a copy would be a second buffer nothing wipes. A
// nil b is the zero WipeableKey.
func NewWipeableKey(b []byte) WipeableKey {
	if b == nil {
		return WipeableKey{}
	}
	return WipeableKey{live: func() []byte { return b }}
}

// Reveal returns the LIVE buffer, not a copy (config.Key's Reveal is a copy, because that one cannot be
// wiped): writing through it is how the key is wiped, and it is nil for the zero WipeableKey. The name
// is config.Key's, so `grep -rn '\.Reveal()'` stays the full list of places a secret leaves its wrapper.
func (k WipeableKey) Reveal() []byte {
	if k.live == nil {
		return nil
	}
	return k.live()
}

// Len is the key's length in bytes; nothing has to reveal it to check a size.
func (k WipeableKey) Len() int { return len(k.Reveal()) }

// IsSet reports whether a buffer is held, wiped or not. A wiped key is still set: its bytes are zero.
func (k WipeableKey) IsSet() bool { return k.live != nil }

func (WipeableKey) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte(wipeableKeyRedacted)) }
func (WipeableKey) String() string               { return wipeableKeyRedacted }
func (WipeableKey) GoString() string             { return wipeableKeyRedacted }
func (WipeableKey) LogValue() slog.Value         { return slog.StringValue(wipeableKeyRedacted) }
func (WipeableKey) MarshalText() ([]byte, error) { return []byte(wipeableKeyRedacted), nil }
