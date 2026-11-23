// Package liveqr is the live QR screen (M16-20, ADR 0045 §2.5): a venue's tablet, TV or old phone,
// paired from the panel to ONE location, that shows a QR code the server signs and rotates every
// ~20 seconds. It owns the three secrets of that feature and the screens' rows:
//
//   - the PAIRING CODE (code.go): drawn when a manager presses "Add a live QR screen", shown once on
//     the panel, typed once on the screen, consumed by one statement (00111);
//   - the SCREEN SESSION (session.go): the screen's HttpOnly cookie after pairing -- the screen
//     holds nothing else, no key and no secret of the business;
//   - the TOKEN (token.go): what the QR carries -- the screen and its location, the instant it was
//     minted and how long it lives, signed by the server. M16-21 verifies it at the tap.
//
// ONE KEY (config.Config.LiveQRKey, TAPPA_LIVE_QR_KEY), THREE USES, EACH UNDER ITS OWN LABEL
// (NewKeys): the token's signature, the code's digest and the session's digest. The two digests are
// HMAC-SHA256 under the server's key, never a bare SHA-256 (the M16-19 handover): live_qr_displays
// is readable by a backup or a read role, and a bare digest of a typed code is the code to anyone
// who can enumerate the code space offline.
//
// WHAT IT IS NOT: plaque cryptography (internal/sun keeps AES-CMAC, SUN and the ctr -- CLAUDE.md
// §3), and not a decision. It mints and resolves; whether a tap counts is the engine's (M16-21).
//
// §7 / §4.7: no code, session value, token or key reaches a log line, an error string or an
// audit_log detail. The package writes no log at all (TestPackage_HasNoLogger), its error strings
// are fixed sentences (TestErrors_CarryNoInput), and the audit details are typed structs with no
// field such a value could travel in (TestScreens_TheTrailCarriesNoSecret).
package liveqr

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/atknatk/tappa/internal/config"
)

// keyLen is TAPPA_LIVE_QR_KEY's size; internal/config already enforces it at startup, and NewKeys
// re-checks so a hand-built Config cannot derive keys from a short or empty secret.
const keyLen = 32

// The three domain labels. A digest made under one can never be mistaken for one made under
// another, and changing a label is changing the key (every screen re-pairs; every QR on a wall
// turns invalid within one rotation).
const (
	labelToken   = "taptime live-qr token v1"
	labelCode    = "taptime live-qr pairing code v1"
	labelSession = "taptime live-qr screen session v1"
)

// Keys are the three per-use keys derived from TAPPA_LIVE_QR_KEY. The zero Keys is unusable: every
// method refuses it rather than computing under an empty key.
//
// Each is a config.Key, the redacting type every key in this repository is held in (backlog T205,
// internal/config's secretfields test): no printer, logger or encoder that reaches one sees its bytes.
type Keys struct {
	token   config.Key
	code    config.Key
	session config.Key
}

// ErrNoKey is NewKeys' refusal of a secret of the wrong size. It names the size, never a byte.
var ErrNoKey = errors.New("liveqr: the key must be 32 bytes")

// NewKeys derives the three keys: HMAC-SHA256(secret, label) for each label. The secret is copied
// into nothing; the derived keys are the only thing kept.
func NewKeys(secret []byte) (*Keys, error) {
	if len(secret) != keyLen {
		return nil, ErrNoKey
	}
	derive := func(label string) config.Key {
		m := hmac.New(sha256.New, secret)
		_, _ = m.Write([]byte(label)) // hash.Hash.Write never returns an error
		return config.NewKey(m.Sum(nil))
	}
	return &Keys{token: derive(labelToken), code: derive(labelCode), session: derive(labelSession)}, nil
}

// digest is the lower-case hex HMAC-SHA256 of v under key -- the 64-hex shape 00111's CHECKs ask
// of pairing_code_hash and session_hash.
func digest(key config.Key, v string) (string, error) {
	if key.Len() != sha256.Size {
		return "", fmt.Errorf("liveqr: digest key is %d bytes", key.Len())
	}
	m := hmac.New(sha256.New, key.Reveal())
	_, _ = m.Write([]byte(v))
	return hex.EncodeToString(m.Sum(nil)), nil
}
