package encode

import (
	"errors"
	"fmt"

	"github.com/atknatk/tappa/internal/sun"
)

// KEKWrapper is the production Wrapper: sun.Wrap under the process tag KEK
// (ADR 0003 md. 4, ADR 0017 §3's "Sarmalama" row).
//
// 🔴 IT IS DELIBERATELY THIN, AND THE THINNESS IS THE SECURITY PROPERTY. CLAUDE.md
// §4.7 puts cryptography in internal/sun and nowhere else, so this type computes
// nothing: it holds the KEK, checks that a call is well formed, and forwards. Every
// choice that matters — AES-256-GCM, the fresh per-call nonce, the RAW 7-byte UID as
// AAD, the 44-byte layout — belongs to sun.Wrap and is asserted in sun's own tests.
// Anything more here would be a second place to get the envelope wrong.
//
// 🔴 THE KEK IS HELD, NOT COPIED, AND THAT IS THE M2-05 RULE KEPT RATHER THAN BENT.
// internal/sun's own header states the discipline: "The KEK is passed in as an
// explicit PARAMETER on every call … the only long-lived copy of the KEK is
// config's." This struct stores the caller's SLICE HEADER, so the bytes it points at
// are still config's bytes — one long-lived copy, not two. Copying them would make a
// second one and would leave it unreachable from any wipe.
//
// ⚠️ THE COUNTED CONSEQUENCE OF NOT COPYING, because aliasing cuts both ways: a
// caller that mutates or zeroes the slice it handed in breaks every subsequent
// WrapKey, and this type cannot notice. Nothing in this repository does that —
// config.Load builds the slice once and hands it out — and the alternative (a copy)
// trades a bug nobody has for a key copy §4.7 would rather not have.
//
// ⚠️ AND WHAT IT IS NOT: there is no cache of anything. M2-05's decision was that no
// plaintext TAG key is ever cached; this type never sees one for longer than the
// call, never stores one, and never returns one.
type KEKWrapper struct {
	kek []byte
}

// NewKEKWrapper builds a Wrapper over kek — internal/config's Config.TagKEK.
//
// The length is checked HERE as well as inside sun.Wrap, and the duplication is
// bought deliberately: a wrong-sized KEK is a CONFIGURATION fault, and finding it at
// construction turns it into a startup failure instead of an error in the middle of
// a round with the chip already selected. That is the same reasoning NewStore gives
// for building the NDEF template once. The message reports a LENGTH and never a byte
// (§4.7).
func NewKEKWrapper(kek []byte) (*KEKWrapper, error) {
	if len(kek) != sun.KEKLen {
		return nil, fmt.Errorf("encode: the tag KEK must be %d bytes, got %d", sun.KEKLen, len(kek))
	}
	return &KEKWrapper{kek: kek}, nil
}

// WrapKey implements Wrapper.
//
// 🔴 THE AAD IS THE RAW UID AND IT IS FORWARDED UNTOUCHED — no hex, no upper-casing,
// no canonicalisation. ADR 0003 md. 4 binds a wrapped key to its own row through
// exactly those 7 bytes, and driver.go passes s.uid, which is what
// sun.ParseGetVersion decoded from the chip. Re-deriving it here from s.uidHex would
// introduce a second spelling of one value, which is the defect migration 00013 Part
// 1 exists to describe: two spellings of one uid are two rows with the SAME AAD.
//
// plainKey is NOT retained and NOT wiped: it belongs to the caller's keyring, which
// is the only thing in this package that calls sun.Zero (see keyring), and sun.Wrap's
// own contract says the same ("the caller owns its lifetime"). Wiping it here would
// pull a buffer out from under the ring that still has it registered.
func (w *KEKWrapper) WrapKey(uid []byte, plainKey []byte) ([]byte, error) {
	if w == nil || len(w.kek) != sun.KEKLen {
		// Reachable only through a zero-value KEKWrapper{} built without the
		// constructor. Refusing beats forwarding an empty KEK to sun.Wrap, whose
		// error would be about the KEK's size and would send a reader looking at
		// configuration.
		return nil, errors.New("encode: this wrapper was not built with NewKEKWrapper")
	}
	wrapped, err := sun.Wrap(w.kek, uid, plainKey)
	if err != nil {
		// sun.Wrap's errors carry lengths only (its own header states and its tests
		// assert that), so wrapping them adds context without adding bytes.
		return nil, fmt.Errorf("encode: wrap the plaque key: %w", err)
	}
	return wrapped, nil
}
