package handler

import (
	"crypto/aes"
	"encoding/hex"
	"strings"
	"testing"
)

// signedTapURL builds a GENUINE plain-SUN tap URL — the one a chip would emit for
// (uid, ctr) under tagKey — so a DB test can drive the activating tap (ADR 0025)
// through the real sun.Verify, CMAC and atomic counter advance included.
//
// WHY THIS LIVES IN A TEST FILE AND NOT IN internal/sun. Production code must
// never be able to MINT a valid tap; only the chip can, and internal/sun exposes
// verification and nothing else. This is the test-side mirror of
// internal/sun/verify_mac.go (SV2 -> session key -> CMAC over the empty message ->
// odd-indexed truncation), written from AN12196 rather than imported, so a bug in
// the production derivation cannot be cancelled out by the same bug here.
// internal/encode/chip_test.go keeps a test-only AES helper for the same reason.
//
// ⚠️ THIS REVERSES A STANCE THIS PACKAGE TOOK BEFORE, deliberately and narrowly.
// tap_db_test.go and day_db_test.go (LIMITS L1) declined a second copy of the SDM
// construction and stipulate the one CMACVerified bit on the check-in path instead —
// possible there because the check-in posts a signed context. The ACTIVATING tap
// has no such step: it is one GET that must run sun.Verify itself (ADR 0025), so a
// test that wants to show the replay guard and the counter advance at the HTTP
// boundary needs a genuine MAC. The risk that stance guarded against — a lighter
// second path in PRODUCTION — is not reopened: this file is _test.go and the
// TestSignedTapURL_MatchesTheKnownAnswer pin ties it to AN12196, not to our code.
func signedTapURL(t *testing.T, tagKeyHex, uid string, ctr uint32) string {
	t.Helper()
	key, err := hex.DecodeString(tagKeyHex)
	if err != nil {
		t.Fatalf("tag key: %v", err)
	}
	uidBytes, err := hex.DecodeString(uid)
	if err != nil || len(uidBytes) != 7 {
		t.Fatalf("uid %q is not 7 bytes of hex", uid)
	}
	// SV2 = 3C C3 00 01 00 80 || UID || ctr (LSB first).
	sv2 := []byte{0x3C, 0xC3, 0x00, 0x01, 0x00, 0x80}
	sv2 = append(sv2, uidBytes...)
	sv2 = append(sv2, byte(ctr), byte(ctr>>8), byte(ctr>>16))
	session := testCMAC(t, key, sv2)
	full := testCMAC(t, session[:], nil)
	var mac [8]byte
	for i := range mac {
		mac[i] = full[2*i+1]
	}
	// Assembled by nfcURL (seedflow_db_test.go), the one URL builder the DB tests
	// share, rather than by a format string here.
	return nfcURL(strings.ToUpper(uid), ctr, strings.ToUpper(hex.EncodeToString(mac[:])))
}

// testCMAC is AES-CMAC (RFC 4493).
func testCMAC(t *testing.T, key, msg []byte) [16]byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes: %v", err)
	}
	var l [16]byte
	block.Encrypt(l[:], l[:])
	k1 := testDbl(l)
	k2 := testDbl(k1)

	n := (len(msg) + 15) / 16
	complete := n > 0 && len(msg)%16 == 0
	if n == 0 {
		n = 1
	}
	var last [16]byte
	if complete {
		copy(last[:], msg[(n-1)*16:])
		for i := range last {
			last[i] ^= k1[i]
		}
	} else {
		tail := msg[(n-1)*16:]
		copy(last[:], tail)
		last[len(tail)] = 0x80
		for i := range last {
			last[i] ^= k2[i]
		}
	}
	var x [16]byte
	for i := 0; i < n-1; i++ {
		for j := 0; j < 16; j++ {
			x[j] ^= msg[i*16+j]
		}
		block.Encrypt(x[:], x[:])
	}
	for j := 0; j < 16; j++ {
		x[j] ^= last[j]
	}
	block.Encrypt(x[:], x[:])
	return x
}

func testDbl(in [16]byte) [16]byte {
	var out [16]byte
	var carry byte
	for i := 15; i >= 0; i-- {
		out[i] = in[i]<<1 | carry
		carry = in[i] >> 7
	}
	if in[0]&0x80 != 0 {
		out[15] ^= 0x87
	}
	return out
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
