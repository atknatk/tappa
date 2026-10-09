package handler

import (
	"encoding/hex"
	"testing"

	"github.com/atknatk/tappa/internal/sun"
)

// signedTapURL builds a GENUINE plain-SUN tap URL — the one a chip would emit for
// (uid, ctr) under tagKey — so a DB test can drive the activating tap (ADR 0026)
// through the real sun.Verify, CMAC and atomic counter advance included.
//
// SINCE THE DEV TOOL (ADR 0026, "Geliştirme aracı") the construction lives in
// internal/sun as MintTapPath, pinned there to AN12196 (mint_test.go), and this
// helper simply calls it — the test-only second copy this file used to carry is
// gone. The pin below stays as the handler package's own check that the helper
// it builds every DB test on agrees with NXP's numbers.
func signedTapURL(t *testing.T, tagKeyHex, uid string, ctr uint32) string {
	t.Helper()
	key, err := hex.DecodeString(tagKeyHex)
	if err != nil {
		t.Fatalf("tag key: %v", err)
	}
	uidBytes, err := hex.DecodeString(uid)
	if err != nil {
		t.Fatalf("uid %q is not hex", uid)
	}
	path, err := sun.MintTapPath(key, uidBytes, ctr)
	if err != nil {
		t.Fatalf("sun.MintTapPath: %v", err)
	}
	return path
}

// TestSignedTapURL_MatchesTheKnownAnswer pins this helper to AN12196's published
// vector (the one internal/sun's own KAT uses), so a DB test that passes with it
// is passing against the chip's arithmetic, not against a private mistake.
func TestSignedTapURL_MatchesTheKnownAnswer(t *testing.T) {
	// AN12196 rev 1.8 §4.4.1: K_SDMFileRead = 00..00, UID 04DE5F1EACC040,
	// SDMReadCtr 0x00003D, SDMMAC 94EED9EE65337086.
	got := signedTapURL(t, "00000000000000000000000000000000", "04DE5F1EACC040", 0x3D)
	want := "/t?tag=04DE5F1EACC040&ctr=00003D&cmac=94EED9EE65337086"
	if got != want {
		t.Fatalf("signedTapURL = %s, want %s", got, want)
	}
}
