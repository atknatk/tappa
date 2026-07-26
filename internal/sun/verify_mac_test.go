package sun

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// Tests for the SDM MAC verifier (M2-04). One vector table row per acceptance
// criterion in m2-sun.md M2-04.
//
// SCOPE OF ASSURANCE — READ THIS. Every valid MAC here is SELF-CONSISTENT: it is
// recomputed with the same sv2()+cmac()+truncateSDMMAC() chain that verifyMAC
// uses, keyed by FAKE test keys (never real tag keys, CLAUDE.md §4.7). Such
// vectors prove the compare/truncation/reject logic and pin the internal layout
// against regressions. TWO axes matter here, do not conflate them:
//
//   - SV2 counter ORDER (URL<->SV2 verbatim): proven STRUCTURALLY below
//     (TestSV2_CtrVerbatim) — the SV2 counter bytes must equal the URL bytes for
//     a non-palindromic counter, so a parse/serialise reversal cannot creep back.
//   - counter VALUE endianness (M2-06 replay ordering, Params.Ctr): NOT decided
//     here; it does not affect this verbatim CMAC and is pinned by a real-chip /
//     AN12196 known-answer vector in M2-07 (test/fixtures/sun_vectors.json).

// fakeTagKey is a FAKE 16-byte AES-128 key for tests only — sequential bytes,
// never a real per-tag K_sdmfileread (CLAUDE.md §4.7). Real keys are random
// (ADR 0003 madde 3) and live only KEK-wrapped in the DB.
const fakeTagKey = "000102030405060708090A0B0C0D0E0F"

// fakeUIDHex is a FAKE 7-byte tag UID (14 hex) for tests.
const fakeUIDHex = "04AC7E55000601"

// fakeCtrHex is a FAKE 3-byte counter (6 hex). NON-PALINDROMIC on purpose: its
// bytes (00 06 41) differ from their reverse (41 06 00), so any test that would
// pass under either byte order is worthless — these catch a reversal.
const fakeCtrHex = "000641"

// referenceMAC recomputes the truncated SDM MAC via the production chain. It is
// deliberately self-consistent with verifyMAC (same sv2/cmac/truncate), so it
// exercises the compare/reject paths — not external correctness (see file note).
func referenceMAC(t *testing.T, key, uid, ctrBytes []byte) [8]byte {
	t.Helper()
	sk, err := cmac(key, sv2(uid, ctrBytes))
	if err != nil {
		t.Fatalf("session-key cmac: %v", err)
	}
	full, err := cmac(sk[:], nil)
	if err != nil {
		t.Fatalf("mac cmac: %v", err)
	}
	return truncateSDMMAC(full)
}

// TestVerifyMAC_ValidAccepts: a MAC produced by the genuine key/uid/ctr triple
// verifies. (m2-sun.md: valid CMAC -> accept.)
func TestVerifyMAC_ValidAccepts(t *testing.T) {
	key := hexBytes(t, fakeTagKey)
	uid := hexBytes(t, fakeUIDHex)
	ctr := hexBytes(t, fakeCtrHex)

	mac := referenceMAC(t, key, uid, ctr)
	ok, err := verifyMAC(key, uid, ctr, mac[:])
	if err != nil {
		t.Fatalf("verifyMAC error: %v", err)
	}
	if !ok {
		t.Fatal("valid MAC rejected, want accept")
	}
}

// TestVerifyMAC_Golden pins the exact truncated MAC bytes for a fixed FAKE
// vector, so an accidental change to SV2 layout, the empty MAC input, the
// session-key derivation or the truncation is caught by a frozen value (a
// self-consistent compute-and-compare would silently move with the bug). The
// SV2 here is built MANUALLY (independent of sv2()) to cross-check that helper,
// with the counter bytes VERBATIM (00 06 41). This freezes the verbatim layout;
// the counter VALUE endianness is a separate axis confirmed in M2-07 (file note).
func TestVerifyMAC_Golden(t *testing.T) {
	key := hexBytes(t, fakeTagKey)
	uid := hexBytes(t, fakeUIDHex)
	ctr := hexBytes(t, fakeCtrHex)

	// Manual SV2 = 3C C3 00 01 00 80 || UID || ctr bytes VERBATIM (00 06 41).
	wantSV2 := append([]byte{0x3C, 0xC3, 0x00, 0x01, 0x00, 0x80}, uid...)
	wantSV2 = append(wantSV2, ctr...)
	if got := sv2(uid, ctr); !bytes.Equal(got, wantSV2) {
		t.Fatalf("sv2 layout drift\n got %x\nwant %x", got, wantSV2)
	}

	// Frozen truncated MAC for (fakeTagKey, fakeUIDHex, fakeCtrHex) with VERBATIM
	// SV2. Regenerate ONLY if the algorithm intentionally changes.
	const wantMACHex = "d22ca9ef3a6b3b5d"
	got := referenceMAC(t, key, uid, ctr)
	if hex.EncodeToString(got[:]) != wantMACHex {
		t.Fatalf("golden MAC drift\n got %x\nwant %s", got, wantMACHex)
	}
	// And the production entry point accepts exactly this frozen value.
	ok, err := verifyMAC(key, uid, ctr, hexBytes(t, wantMACHex))
	if err != nil {
		t.Fatalf("verifyMAC error: %v", err)
	}
	if !ok {
		t.Fatal("golden MAC rejected by verifyMAC")
	}
}

// TestSV2_CtrVerbatim is the load-bearing anti-reversal test. For a
// NON-PALINDROMIC counter, the 3 counter bytes inside SV2 must equal the raw URL
// bytes EXACTLY (same order). This structurally forbids the original defect
// (URL big-endian parse + little-endian SV2 serialise, which reversed the bytes)
// from ever returning: a reversal would make SV2's counter bytes differ from the
// URL's here.
func TestSV2_CtrVerbatim(t *testing.T) {
	uid := hexBytes(t, fakeUIDHex)
	const prefixLen = 6
	for _, ctrHex := range []string{
		"000641", // 00 06 41 (reverse 41 06 00 — differs)
		"0100FF", // 01 00 FF (reverse FF 00 01 — differs)
		"A1B2C3", // A1 B2 C3 (reverse C3 B2 A1 — differs)
	} {
		t.Run(ctrHex, func(t *testing.T) {
			ctr := hexBytes(t, ctrHex)
			out := sv2(uid, ctr)
			// Counter occupies the last 3 bytes: after the 6-byte prefix and the
			// 7-byte UID.
			gotCtr := out[prefixLen+len(uid):]
			if !bytes.Equal(gotCtr, ctr) {
				t.Fatalf("SV2 counter bytes %x != URL bytes %x (reversal?)", gotCtr, ctr)
			}
		})
	}
}

// TestVerifyMAC_WrongKeyRejects: a MAC minted under key A must not verify under
// key B. (m2-sun.md: wrong-key CMAC -> reject.)
func TestVerifyMAC_WrongKeyRejects(t *testing.T) {
	uid := hexBytes(t, fakeUIDHex)
	ctr := hexBytes(t, fakeCtrHex)

	keyA := hexBytes(t, fakeTagKey)
	keyB := hexBytes(t, "0F0E0D0C0B0A09080706050403020100") // different fake key

	mac := referenceMAC(t, keyA, uid, ctr) // signed with A
	ok, err := verifyMAC(keyB, uid, ctr, mac[:])
	if err != nil {
		t.Fatalf("verifyMAC error: %v", err)
	}
	if ok {
		t.Fatal("MAC from wrong key accepted, want reject")
	}
}

// TestVerifyMAC_SingleBitFlipRejects: flipping one bit of a genuine MAC must
// reject. (m2-sun.md: single-bit-corrupted CMAC -> reject.)
func TestVerifyMAC_SingleBitFlipRejects(t *testing.T) {
	key := hexBytes(t, fakeTagKey)
	uid := hexBytes(t, fakeUIDHex)
	ctr := hexBytes(t, fakeCtrHex)

	mac := referenceMAC(t, key, uid, ctr)
	for _, bit := range []struct {
		name      string
		idx, mask int
	}{
		{"first_byte_lsb", 0, 0x01},
		{"last_byte_msb", 7, 0x80},
		{"mid_byte", 3, 0x10},
	} {
		t.Run(bit.name, func(t *testing.T) {
			corrupt := append([]byte(nil), mac[:]...)
			corrupt[bit.idx] ^= byte(bit.mask)
			ok, err := verifyMAC(key, uid, ctr, corrupt)
			if err != nil {
				t.Fatalf("verifyMAC error: %v", err)
			}
			if ok {
				t.Fatalf("bit-flipped MAC accepted, want reject")
			}
		})
	}
}

// TestVerifyMAC_TruncationIsOddIndexed is the load-bearing M2-04 test: it proves
// the verifier compares against the ODD-indexed 8 bytes (full[1,3,…,15]) — the
// ones the NTAG 424 emits — and NOT the even-indexed bytes, the first 8 bytes,
// or the full 16. Comparing against any of those wrong slices is the classic SDM
// trap that fails every real tap and looks like a wrong key (m2-sun.md Tuzaklar).
func TestVerifyMAC_TruncationIsOddIndexed(t *testing.T) {
	key := hexBytes(t, fakeTagKey)
	uid := hexBytes(t, fakeUIDHex)
	ctr := hexBytes(t, fakeCtrHex)

	// Recompute the full 16-byte CMAC the same way verifyMAC does internally.
	sk, err := cmac(key, sv2(uid, ctr))
	if err != nil {
		t.Fatalf("session-key cmac: %v", err)
	}
	full, err := cmac(sk[:], nil)
	if err != nil {
		t.Fatalf("mac cmac: %v", err)
	}

	odd := make([]byte, 8)  // full[1],full[3],…,full[15] — what the chip emits
	even := make([]byte, 8) // full[0],full[2],…,full[14] — wrong slice
	for i := 0; i < 8; i++ {
		odd[i] = full[2*i+1]
		even[i] = full[2*i]
	}
	first8 := full[0:8] // another wrong slice (leading half)

	tests := []struct {
		name string
		mac  []byte
		want bool
	}{
		{"odd_indexed_accepts", odd, true},
		{"even_indexed_rejects", even, false},
		{"first8_rejects", first8, false},
		{"full16_rejects", full[:], false}, // length mismatch -> compare returns 0
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := verifyMAC(key, uid, ctr, tc.mac)
			if err != nil {
				t.Fatalf("verifyMAC error: %v", err)
			}
			if ok != tc.want {
				t.Fatalf("verifyMAC = %v, want %v", ok, tc.want)
			}
		})
	}
}

// TestVerifyMAC_CtrBoundToSV2: the counter enters SV2, so a MAC minted at ctr N
// must not verify at ctr N+1. This proves ctr participates in the crypto (not
// only in replay ordering) — a replayed URL with a bumped ctr cannot reuse an
// old MAC.
func TestVerifyMAC_CtrBoundToSV2(t *testing.T) {
	key := hexBytes(t, fakeTagKey)
	uid := hexBytes(t, fakeUIDHex)

	mac := referenceMAC(t, key, uid, hexBytes(t, "000641"))
	ok, err := verifyMAC(key, uid, hexBytes(t, "000642"), mac[:])
	if err != nil {
		t.Fatalf("verifyMAC error: %v", err)
	}
	if ok {
		t.Fatal("MAC for ctr N accepted at ctr N+1, want reject")
	}
}

// TestVerifyMAC_UIDBoundToSV2: the UID enters SV2, so a MAC minted for UID A
// must not verify for UID B (a stolen MAC cannot be replayed onto another tag).
func TestVerifyMAC_UIDBoundToSV2(t *testing.T) {
	key := hexBytes(t, fakeTagKey)
	ctr := hexBytes(t, fakeCtrHex)

	uidA := hexBytes(t, fakeUIDHex)
	uidB := hexBytes(t, "04AC7E55000602") // one byte different

	mac := referenceMAC(t, key, uidA, ctr)
	ok, err := verifyMAC(key, uidB, ctr, mac[:])
	if err != nil {
		t.Fatalf("verifyMAC error: %v", err)
	}
	if ok {
		t.Fatal("MAC for UID A accepted for UID B, want reject")
	}
}

// TestVerifyMAC_WrongLengthCMACRejects: a CMAC of the wrong length rejects and
// never panics. ConstantTimeCompare returns 0 on a length mismatch, so this is
// the reject path, not an error (the parser guarantees 8 bytes, but defence in
// depth matters at the crypto boundary).
func TestVerifyMAC_WrongLengthCMACRejects(t *testing.T) {
	key := hexBytes(t, fakeTagKey)
	uid := hexBytes(t, fakeUIDHex)
	ctr := hexBytes(t, fakeCtrHex)

	for _, tc := range []struct {
		name string
		mac  []byte
	}{
		{"empty", nil},
		{"too_short_7", make([]byte, 7)},
		{"too_long_9", make([]byte, 9)},
		{"too_long_16", make([]byte, 16)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := verifyMAC(key, uid, ctr, tc.mac)
			if err != nil {
				t.Fatalf("verifyMAC error: %v", err)
			}
			if ok {
				t.Fatal("wrong-length CMAC accepted, want reject")
			}
		})
	}
}

// TestVerifyMAC_InvalidKeyLengthErrors: an invalid AES key length is an upstream
// programming error and is surfaced, not swallowed (CLAUDE.md §7). It is an
// error, distinct from a cryptographic reject (false, nil).
func TestVerifyMAC_InvalidKeyLengthErrors(t *testing.T) {
	uid := hexBytes(t, fakeUIDHex)
	ctr := hexBytes(t, fakeCtrHex)
	ok, err := verifyMAC([]byte{0x00, 0x01, 0x02}, uid, ctr, make([]byte, 8))
	if err == nil {
		t.Fatal("expected error for 3-byte key, got nil")
	}
	if ok {
		t.Fatal("expected ok=false on error")
	}
}

// TestSV2_Layout pins SV2 = prefix || UID || ctr(3 bytes VERBATIM) byte for
// byte, using a non-palindromic counter so the byte order is unambiguous.
func TestSV2_Layout(t *testing.T) {
	uid := hexBytes(t, fakeUIDHex)
	ctr := hexBytes(t, "A1B2C3") // non-palindromic
	got := sv2(uid, ctr)
	want := append([]byte{0x3C, 0xC3, 0x00, 0x01, 0x00, 0x80}, uid...)
	want = append(want, 0xA1, 0xB2, 0xC3) // verbatim, same order as ctr
	if !bytes.Equal(got, want) {
		t.Fatalf("SV2 layout\n got %x\nwant %x", got, want)
	}
	if len(got) != 6+7+3 {
		t.Fatalf("SV2 length = %d, want 16", len(got))
	}
}

// TestTruncateSDMMAC_Indices proves the truncation picks exactly the odd
// indices. With full[i]=i, the result must be 1,3,5,7,9,11,13,15.
func TestTruncateSDMMAC_Indices(t *testing.T) {
	var full [16]byte
	for i := range full {
		full[i] = byte(i)
	}
	got := truncateSDMMAC(full)
	want := [8]byte{1, 3, 5, 7, 9, 11, 13, 15}
	if got != want {
		t.Fatalf("truncateSDMMAC\n got %v\nwant %v", got, want)
	}
}
