package sun

import (
	"bytes"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

// Tests for the SDM MAC verifier (M2-04). One vector table row per acceptance
// criterion in m2-sun.md M2-04.
//
// SCOPE OF ASSURANCE — READ THIS, IT CHANGED IN M2-08.
//
// Most valid MACs here are SELF-CONSISTENT: recomputed with the same
// sv2()+cmac()+truncateSDMMAC() chain that verifyMAC uses, keyed by FAKE test
// keys (never real tag keys, CLAUDE.md §4.7). Such vectors prove the
// compare/truncation/reject logic and pin the internal layout against
// regressions — but they CANNOT prove the chain matches the chip, because the
// bug and the expectation move together. That is not hypothetical: it is exactly
// how sv2() shipped for months appending the counter in the wrong byte order
// while every test here stayed green (M2-08).
//
// THE FIX FOR THAT IS TestSDMKAT_*, WHICH LIVES IN an12196_kat_test.go — a
// SEPARATE file in this same package, not further down this one. (This sentence
// said "BELOW" and sent readers hunting through the wrong file.) They are
// known-answer vectors whose expected values come from NXP AN12196's published
// tables, NOT from our code; they are the only tests in this package that would
// have failed on the day the defect was written, and they must never be rewritten
// to call referenceMAC.
//
// The two counter axes this file used to keep apart are now ONE axis, both
// pinned by the same document (see sv2's comment): the URL carries the counter
// MSB-first (Params.Ctr reads it big-endian) and SV2 takes it LSB-first, so sv2
// reverses exactly once.

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
// with the counter bytes REVERSED (URL 00 06 41 -> SV2 41 06 00).
//
// ⚠️ A FROZEN VALUE IS ONLY AS GOOD AS THE DAY IT WAS FROZEN. This constant was
// re-frozen in M2-08 because the old one pinned the WRONG chain; it is the
// TestSDMKAT_* vectors, not this one, that anchor the algorithm to the chip.
func TestVerifyMAC_Golden(t *testing.T) {
	key := hexBytes(t, fakeTagKey)
	uid := hexBytes(t, fakeUIDHex)
	ctr := hexBytes(t, fakeCtrHex)

	// Manual SV2 = 3C C3 00 01 00 80 || UID || ctr bytes REVERSED (41 06 00).
	wantSV2 := append([]byte{0x3C, 0xC3, 0x00, 0x01, 0x00, 0x80}, uid...)
	wantSV2 = append(wantSV2, 0x41, 0x06, 0x00)
	if got := sv2(uid, ctr); !bytes.Equal(got, wantSV2) {
		t.Fatalf("sv2 layout drift\n got %x\nwant %x", got, wantSV2)
	}

	// Frozen truncated MAC for (fakeTagKey, fakeUIDHex, fakeCtrHex) with REVERSED
	// SV2 counter. Regenerate ONLY if the algorithm intentionally changes.
	const wantMACHex = "250464074beca2dd"
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

// TestSV2_CounterIsReversedIntoLSBFirstOrder pins the byte order sv2 applies to
// the counter: the URL carries it MSB-first, SV2 needs it LSB-first, so the 3
// counter bytes inside SV2 must be the REVERSE of the URL bytes.
//
// 🔴 THIS TEST REPLACES ONE THAT ASSERTED THE OPPOSITE. Its predecessor was named
// for "verbatim" and demanded SV2's counter bytes EQUAL the URL's; it passed for
// months, and it was wrong (AN12196 rev. 1.8 §4.3 Table 2 p.10 vs §4.4.1 p.11 —
// see sv2). A test can pin a defect just as firmly as it pins a feature, and the
// only thing that separates the two is a source outside the code: here,
// TestSDMKAT_*.
//
// Every counter below is NON-PALINDROMIC on purpose. A palindrome passes under
// either byte order and would prove nothing — which is also why the defect
// survived: 1 counter in 256 is a palindrome, and NONE of 1..255 are, so a real
// chip's first 255 taps would all have been rejected.
func TestSV2_CounterIsReversedIntoLSBFirstOrder(t *testing.T) {
	uid := hexBytes(t, fakeUIDHex)
	const prefixLen = 6
	for _, tc := range []struct{ urlHex, wantSV2Hex string }{
		{"000641", "410600"},
		{"0100FF", "ff0001"},
		{"A1B2C3", "c3b2a1"},
	} {
		t.Run(tc.urlHex, func(t *testing.T) {
			ctr := hexBytes(t, tc.urlHex)
			out := sv2(uid, ctr)
			// Counter occupies the last 3 bytes: after the 6-byte prefix and the
			// 7-byte UID.
			gotCtr := out[prefixLen+len(uid):]
			if hex.EncodeToString(gotCtr) != tc.wantSV2Hex {
				t.Fatalf("SV2 counter bytes = %x, want %s (URL bytes were %x)",
					gotCtr, tc.wantSV2Hex, ctr)
			}
			// And explicitly NOT the URL order — the exact assertion the old test made.
			if bytes.Equal(gotCtr, ctr) {
				t.Fatalf("SV2 counter bytes equal the URL bytes %x; the reversal is gone", ctr)
			}
			// sv2 must not mutate the caller's slice (Params.CtrBytes is reused).
			if hex.EncodeToString(ctr) != strings.ToLower(tc.urlHex) {
				t.Fatalf("sv2 mutated its input: %x, want %s", ctr, tc.urlHex)
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

// TestSV2_Layout pins SV2 = prefix || UID || ctr(3 bytes REVERSED) byte for
// byte, using a non-palindromic counter so the byte order is unambiguous.
func TestSV2_Layout(t *testing.T) {
	uid := hexBytes(t, fakeUIDHex)
	ctr := hexBytes(t, "A1B2C3") // non-palindromic
	got := sv2(uid, ctr)
	want := append([]byte{0x3C, 0xC3, 0x00, 0x01, 0x00, 0x80}, uid...)
	want = append(want, 0xC3, 0xB2, 0xA1) // LSB-first: the reverse of the URL order
	if !bytes.Equal(got, want) {
		t.Fatalf("SV2 layout\n got %x\nwant %x", got, want)
	}
	if len(got) != 6+7+3 {
		t.Fatalf("SV2 length = %d, want 16", len(got))
	}
}

// TestVerifyMAC_ComparisonStaysConstantTime is the gate behind step 5, and it
// exists because there was none.
//
// ⚠️ THE MUTATIONS BELOW ARE DESCRIBED, NOT SPELLED OUT, AND THAT IS DELIBERATE.
// Writing the byte-comparison call verbatim in this comment put TWO permanent
// false positives into scripts/redline-check.sh's R7 output (measured: a clean
// tree went from zero R7 lines to two, both of them prose). The sibling file
// an12196_kat_test.go already names a struct field `sel` instead of `mac` for
// exactly this reason — "a tolerated noisy warning is how a net stops being read".
// Doing the opposite one file over would erode the same net.
//
// 🔴 MEASURED, TWICE, BEFORE THIS TEST WAS WRITTEN. Replacing the constant-time
// compare on the last line of verifyMAC with the stdlib byte-slice equality helper
// (or with a string conversion and ==) left `go test ./internal/sun/` GREEN and
// `scripts/redline-check.sh` at exit 0 — R7 prints WARN for the first spelling, and
// `make audit` does not fall over on a WARN. The constant-time property is
// invisible to every behavioural test in this package by construction: all three
// spellings return the same boolean for every input, so no vector can separate
// them. Only the source can.
//
// 🔴 WHY THIS IS AN ALLOWLIST AND NOT A DENYLIST — ELIMINATED BY MEASUREMENT, not
// preferred. The obvious alternative was to promote redline-check's R7 scan for
// that one helper from WARN to FAIL under internal/sun. It was tried and it fails
// to close the class: the string-conversion mutation produced ZERO hits from that
// pattern and exit 0, while being just as leaky. A denylist closes the spellings
// someone already thought of; requiring the constant-time call to BE THERE closes
// every replacement, including ones nobody has spelled yet.
//
// COUNTED LIMIT: this reads text, so it pins the CALL, not the semantics. Moving
// the comparison into a helper in another file, or shadowing subtle, would pass
// here. What it does guarantee is that the compare in verifyMAC's own body is the
// constant-time one, which is where the four measured mutations landed. Reading
// source in a test is not new here — cmd/rotatekek/script_test.go and
// cmd/tappa/testnames_test.go both do it, for the same reason: some properties
// are properties of the text.
func TestVerifyMAC_ComparisonStaysConstantTime(t *testing.T) {
	const file = "verify_mac.go"
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("reading %s: %v", file, err)
	}
	text := string(src)

	if !strings.Contains(text, `"crypto/subtle"`) {
		t.Errorf("%s no longer imports crypto/subtle; the MAC compare cannot be constant time without it", file)
	}

	// Scan the FUNCTION BODY only. The doc comment above verifyMAC legitimately
	// names bytes.Equal and == as the forbidden spellings, and a scan that cannot
	// tell prose from code reports the explanation as the defect — the mistake
	// cmd/rotatekek/script_test.go made three separate times.
	const marker = "func verifyMAC("
	i := strings.Index(text, marker)
	if i < 0 {
		t.Fatalf("could not locate %q in %s; the scan has gone blind", marker, file)
	}
	body := text[i:]
	if j := strings.Index(body, "\n}\n"); j >= 0 {
		body = body[:j]
	}
	// Drop the signature line: it names chipCMAC as a PARAMETER, which is not a
	// comparison. Leaving it in made this test fail on the correct code — measured
	// on the first run, and exactly the false-positive shape the scanners in
	// cmd/rotatekek/script_test.go kept producing.
	if k := strings.Index(body, "\n"); k >= 0 {
		body = body[k+1:]
	}

	// Every CODE line that touches the chip's value must run it through the
	// constant-time compare. Today there is exactly one such line; the control
	// below proves the scan found it rather than finding nothing.
	compares := 0
	for _, ln := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "//") || !strings.Contains(trimmed, "chipCMAC") {
			continue
		}
		compares++
		if !strings.Contains(trimmed, "subtle.ConstantTimeCompare(") {
			t.Errorf("verifyMAC compares the chip's CMAC without subtle.ConstantTimeCompare. A "+
				"byte-by-byte compare exits early and leaks how much of a forged MAC was correct "+
				"(CLAUDE.md §4.7):\n  %s", trimmed)
		}
	}
	if compares == 0 {
		t.Fatal("no code line in verifyMAC mentions chipCMAC; the scan has gone blind and would pass " +
			"on a function that never compares anything")
	}

	// AND the verdict must BE that compare, not merely contain it somewhere.
	if !strings.Contains(body, "return subtle.ConstantTimeCompare(") {
		t.Error("verifyMAC's returned verdict is not subtle.ConstantTimeCompare(...); step 5 of " +
			"ADR 0003 madde 6 requires it")
	}
	// == 1 is not decoration: ConstantTimeCompare returns int, and `!= 0`, `> 0`
	// or a bare truthiness rewrite would each be a different (and in some
	// spellings wrong) predicate.
	if !strings.Contains(body, "subtle.ConstantTimeCompare(mac[:], chipCMAC) == 1") {
		t.Error("the compare is no longer spelled `subtle.ConstantTimeCompare(mac[:], chipCMAC) == 1`; " +
			"if the rename is deliberate, update this assertion in the SAME commit and say why")
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
