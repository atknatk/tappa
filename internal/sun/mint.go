package sun

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Minting a SUN tap URL — what the chip does on every read — for DEVELOPMENT and
// TESTS ONLY (ADR 0025, "Geliştirme aracı").
//
// WHY IT LIVES HERE. A desktop browser cannot touch a plaque, and the activation
// flow (ADR 0025) completes only on a genuine NFC tap. Rather than add a path that
// SKIPS verification, the dev tool mints the URL the plaque would have produced
// and sends the browser to GET /t, where sun.Verify checks it exactly as it checks
// a real tap. Cryptography stays in this package (CLAUDE.md §3), and the
// construction is the same one verifyMAC checks — SV2 → session key → CMAC over
// the empty message → odd-index truncation — so a mismatch between minting and
// verifying cannot hide.
//
// WHAT KEEPS IT FROM BEING A FORGERY SERVICE IN PRODUCTION is not this file: a
// caller needs the per-tag key, which only the process KEK unwraps, and the one
// HTTP surface that calls MintNextTapForDevelopment is mounted only on a
// development deployment (internal/handler/devtap.go, DevToolsEnabled). Nothing
// here is logged: the key is wiped on return and the MAC exists only in the
// returned path.

// MintTapPath returns "/t?tag=<UID>&ctr=<6 hex>&cmac=<16 hex>" for one read of a
// plaque whose K_SDMFileRead is key. uid is the raw 7-byte UID; ctr must fit the
// chip's 24-bit counter.
func MintTapPath(key, uid []byte, ctr uint32) (string, error) {
	if len(uid) != uidLen {
		return "", fmt.Errorf("sun: mint: uid must be %d bytes, got %d", uidLen, len(uid))
	}
	if ctr > maxUint24 {
		return "", errors.New("sun: mint: counter exceeds 24 bits")
	}
	ctrBytes := []byte{byte(ctr >> 16), byte(ctr >> 8), byte(ctr)} // URL order, MSB first
	sessionKey, err := cmac(key, sv2(uid, ctrBytes))
	defer Zero(sessionKey[:])
	if err != nil {
		return "", fmt.Errorf("sun: mint: derive session key: %w", err)
	}
	full, err := cmac(sessionKey[:], nil)
	defer Zero(full[:])
	if err != nil {
		return "", fmt.Errorf("sun: mint: compute sdm mac: %w", err)
	}
	mac := truncateSDMMAC(full)
	defer Zero(mac[:])

	var b strings.Builder
	b.WriteString("/t?tag=")
	b.WriteString(strings.ToUpper(hex.EncodeToString(uid)))
	b.WriteString("&ctr=")
	b.WriteString(strings.ToUpper(hex.EncodeToString(ctrBytes)))
	b.WriteString("&cmac=")
	b.WriteString(strings.ToUpper(hex.EncodeToString(mac[:])))
	return b.String(), nil
}

// ErrNotMintable reports a plaque the dev tool must not tap: not in service, not
// on a wall, or at the end of its counter.
var ErrNotMintable = errors.New("sun: mint: plaque is not in service")

// MintNextTapForDevelopment mints the NEXT read of the plaque uid: last_ctr + 1,
// under the plaque's own unwrapped key. It advances nothing — the returned path
// still has to go through GET /t and sun.Verify, which is the point.
func (v *Verifier) MintNextTapForDevelopment(ctx context.Context, uid string) (string, error) {
	tag, err := v.tags.GetTagByUID(ctx, uid)
	if err != nil {
		return "", fmt.Errorf("sun: mint: resolve tag: %w", err)
	}
	if tag.Status != tagStatusActive || tag.LocationID == nil || tag.LastCtr < 0 || uint32(tag.LastCtr) >= maxUint24 {
		return "", ErrNotMintable
	}
	uidBytes, err := hex.DecodeString(tag.UID)
	if err != nil {
		return "", fmt.Errorf("sun: mint: uid: %w", err)
	}
	key, err := UnwrapAny(v.keks, uidBytes, tag.AESKeyRef)
	if err != nil {
		return "", fmt.Errorf("sun: mint: unwrap tag key: %w", err)
	}
	defer Zero(key)
	return MintTapPath(key, uidBytes, uint32(tag.LastCtr)+1)
}
