package sun

import (
	"crypto/aes"
	"encoding/hex"
	"strings"
	"testing"
)

// hexBytes decodes a spaced hex string ("2b7e1516 28aed2a6 …") into bytes.
func hexBytes(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatalf("bad hex %q: %v", s, err)
	}
	return b
}

// hex16 decodes a spaced hex string that must be exactly 16 bytes.
func hex16(t *testing.T, s string) [16]byte {
	t.Helper()
	b := hexBytes(t, s)
	if len(b) != 16 {
		t.Fatalf("want 16 bytes, got %d from %q", len(b), s)
	}
	var out [16]byte
	copy(out[:], b)
	return out
}

// rfcKey is the AES-128 key shared by every RFC 4493 §4 example.
const rfcKey = "2b7e1516 28aed2a6 abf71588 09cf4f3c"

// rfcMsg is the 64-byte example message; each vector uses a prefix of it.
const rfcMsg = "6bc1bee2 2e409f96 e93d7e11 7393172a" +
	" ae2d8a57 1e03ac9c 9eb76fac 45af8e51" +
	" 30c81c46 a35ce411 e5fbc119 1a0a52ef" +
	" f69f2445 df4f9b17 ad2b417b e66c3710"

// TestCMAC_RFC4493Vectors runs the four official RFC 4493 §4 known-answer
// vectors verbatim. These are PUBLIC standard test vectors, not secrets:
// same key 2b7e1516…, message lengths 0 / 16 / 40 / 64 bytes. Lengths 16 and
// 64 exercise the complete-block (K1) path; 0 and 40 exercise the padded
// (K2) path — so all branches are covered here.
func TestCMAC_RFC4493Vectors(t *testing.T) {
	key := hexBytes(t, rfcKey)
	msg := hexBytes(t, rfcMsg)

	tests := []struct {
		name   string // "RFC 4493 §4 known-answer, public"
		msgLen int
		want   string
	}{
		{"example1_len0_empty", 0, "bb1d6929 e9593728 7fa37d12 9b756746"},
		{"example2_len16_fullblock", 16, "070a16b4 6b4d4144 f79bdd9d d04a287c"},
		{"example3_len40_partialblock", 40, "dfa66747 de9ae630 30ca3261 1497c827"},
		{"example4_len64_fullblock", 64, "51f0bebf 7e3b9d92 fc497417 79363cfe"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			want := hex16(t, tc.want)
			got, err := cmac(key, msg[:tc.msgLen])
			if err != nil {
				t.Fatalf("cmac returned error: %v", err)
			}
			if got != want {
				t.Errorf("cmac mismatch\n got %x\nwant %x", got, want)
			}
		})
	}
}

// TestCMAC_Subkeys_RFC4493 verifies K1/K2 subkey derivation against the
// worked example in RFC 4493 §4 (public values).
func TestCMAC_Subkeys_RFC4493(t *testing.T) {
	block, err := aes.NewCipher(hexBytes(t, rfcKey))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	// From RFC 4493 §4: L = AES_K(0), then K1 = dbl(L), K2 = dbl(K1).
	wantK1 := hex16(t, "fbeed618 35713366 7c85e08f 7236a8de")
	wantK2 := hex16(t, "f7ddac30 6ae266cc f90bc11e e46d513b")

	k1, k2 := subkeys(block)
	if k1 != wantK1 {
		t.Errorf("K1 mismatch\n got %x\nwant %x", k1, wantK1)
	}
	if k2 != wantK2 {
		t.Errorf("K2 mismatch\n got %x\nwant %x", k2, wantK2)
	}
}

// TestCMAC_Dbl covers the doubling primitive directly, including the
// conditional Rb XOR when the pre-shift high bit is set.
func TestCMAC_Dbl(t *testing.T) {
	// High bit clear → plain left shift, no Rb.
	in := hex16(t, "01000000 00000000 00000000 00000000")
	want := hex16(t, "02000000 00000000 00000000 00000000")
	if got := dbl(in); got != want {
		t.Errorf("dbl(no-carry)\n got %x\nwant %x", got, want)
	}
	// High bit set → left shift then XOR Rb (…0087) into the last byte.
	in = hex16(t, "80000000 00000000 00000000 00000000")
	want = hex16(t, "00000000 00000000 00000000 00000087")
	if got := dbl(in); got != want {
		t.Errorf("dbl(carry)\n got %x\nwant %x", got, want)
	}
}

// TestCMAC_LastBlockPadding exercises both branches of the final-block
// construction (RFC 4493 §2.4): a full block uses K1 with no padding; a
// partial or empty block gets 10* padding and uses K2. It reuses the RFC
// K1/K2 so the expected value pins both the branch choice and where the
// 0x80 padding byte lands.
func TestCMAC_LastBlockPadding(t *testing.T) {
	k1 := hex16(t, "fbeed618 35713366 7c85e08f 7236a8de")
	k2 := hex16(t, "f7ddac30 6ae266cc f90bc11e e46d513b")

	xor := func(a, b [16]byte) [16]byte {
		var out [16]byte
		for i := range out {
			out[i] = a[i] ^ b[i]
		}
		return out
	}

	t.Run("full_block_uses_k1", func(t *testing.T) {
		tail := hex16(t, "6bc1bee2 2e409f96 e93d7e11 7393172a")
		want := xor(tail, k1) // no padding on a complete block
		if got := lastBlock(tail[:], k1, k2); got != want {
			t.Errorf("full block\n got %x\nwant %x", got, want)
		}
	})

	t.Run("partial_block_pads_and_uses_k2", func(t *testing.T) {
		tail := hexBytes(t, "6bc1bee2 2e409f96") // 8 bytes
		var padded [16]byte
		copy(padded[:], tail)
		padded[len(tail)] = 0x80 // 10* padding
		want := xor(padded, k2)
		if got := lastBlock(tail, k1, k2); got != want {
			t.Errorf("partial block\n got %x\nwant %x", got, want)
		}
	})

	t.Run("empty_block_pads_and_uses_k2", func(t *testing.T) {
		var padded [16]byte
		padded[0] = 0x80 // 10* padding on an empty tail
		want := xor(padded, k2)
		if got := lastBlock(nil, k1, k2); got != want {
			t.Errorf("empty block\n got %x\nwant %x", got, want)
		}
	})
}

// TestCMAC_InvalidKeyLength proves an invalid AES key length returns an
// error and never panics (the error is surfaced, not swallowed).
func TestCMAC_InvalidKeyLength(t *testing.T) {
	if _, err := cmac([]byte{0x00, 0x01, 0x02}, nil); err == nil {
		t.Fatal("expected error for 3-byte key, got nil")
	}
}
