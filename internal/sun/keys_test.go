package sun

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

// Tests for KEK envelope encryption (M2-05, ADR 0003 madde 4). One row per
// acceptance criterion: round-trip, fresh nonce, AAD=UID portability guard,
// wrong-KEK reject, corrupt/short ref reject, length-before-KEK ordering, and
// the load-bearing §4.7 leak test (no plaintext key or KEK in any error or in
// the wrapped blob).
//
// ALL key material below is FAKE and labelled as such (CLAUDE.md §4.7). The real
// KEK is TAPPA_TAG_KEK, absent from the repo; real per-tag keys are random (ADR
// 0003 madde 3) and stored only KEK-wrapped in the DB. hexBytes is the shared
// helper from cmac_test.go (same package).

const (
	// fakeKEKHex is a FAKE 32-byte AES-256 KEK.
	fakeKEKHex = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	// fakeKEK2Hex is a DIFFERENT fake 32-byte KEK, for the wrong-KEK test.
	fakeKEK2Hex = "ffeeddccbbaa99887766554433221100ffeeddccbbaa99887766554433221100"
	// fakeWrapKeyHex is a FAKE 16-byte AES-128 per-tag key with a distinctive,
	// searchable byte pattern so the leak test can look for it verbatim.
	fakeWrapKeyHex = "deadbeefcafebabe0123456789abcdef"
	// fakeUID2Hex is a second FAKE 7-byte UID (one byte off fakeUIDHex, which is
	// defined in verify_mac_test.go), for the AAD-mismatch test.
	fakeUID2Hex = "04AC7E55000602"
)

// TestWrap_RoundTrip: Unwrap(uid, Wrap(uid, key)) == key, and the ref is exactly
// 44 bytes with the ADR 0003 layout (nonce 12 || ciphertext 16 || tag 16).
func TestWrap_RoundTrip(t *testing.T) {
	kek := hexBytes(t, fakeKEKHex)
	uid := hexBytes(t, fakeUIDHex)
	key := hexBytes(t, fakeWrapKeyHex)

	ref, err := Wrap(kek, uid, key)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if len(ref) != wrappedKeyLen {
		t.Fatalf("ref length = %d, want %d (nonce12||ct16||tag16)", len(ref), wrappedKeyLen)
	}

	got, err := Unwrap(kek, uid, ref)
	if err != nil {
		t.Fatalf("Unwrap: %v", err)
	}
	if !bytes.Equal(got, key) {
		t.Fatalf("round-trip mismatch\n got %x\nwant %x", got, key)
	}
}

// TestWrap_FreshNoncePerCall: wrapping the SAME (uid, key) twice must yield
// DIFFERENT refs (fresh crypto/rand nonce), and both must still unwrap. A reused
// nonce would make the two refs identical.
func TestWrap_FreshNoncePerCall(t *testing.T) {
	kek := hexBytes(t, fakeKEKHex)
	uid := hexBytes(t, fakeUIDHex)
	key := hexBytes(t, fakeWrapKeyHex)

	ref1, err := Wrap(kek, uid, key)
	if err != nil {
		t.Fatalf("Wrap 1: %v", err)
	}
	ref2, err := Wrap(kek, uid, key)
	if err != nil {
		t.Fatalf("Wrap 2: %v", err)
	}
	if bytes.Equal(ref1, ref2) {
		t.Fatal("two wraps of the same (uid,key) produced identical refs — nonce reuse")
	}
	// The nonce prefixes specifically must differ.
	if bytes.Equal(ref1[:gcmNonceLen], ref2[:gcmNonceLen]) {
		t.Fatal("nonce prefix repeated across wraps")
	}
	for i, ref := range [][]byte{ref1, ref2} {
		got, err := Unwrap(kek, uid, ref)
		if err != nil {
			t.Fatalf("Unwrap ref%d: %v", i+1, err)
		}
		if !bytes.Equal(got, key) {
			t.Fatalf("ref%d did not round-trip", i+1)
		}
	}
}

// TestUnwrap_WrongUIDFails is the AAD=UID portability-guard proof (ADR 0003
// madde 4). A key wrapped for uidA must NOT open under uidB: a wrapped ref moved
// from one tag row to another (tappa_app has UPDATE on tags) fails to
// authenticate. This is the whole reason AAD=UID is mandatory in v1.
func TestUnwrap_WrongUIDFails(t *testing.T) {
	kek := hexBytes(t, fakeKEKHex)
	uidA := hexBytes(t, fakeUIDHex)
	uidB := hexBytes(t, fakeUID2Hex)
	key := hexBytes(t, fakeWrapKeyHex)

	ref, err := Wrap(kek, uidA, key)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	got, err := Unwrap(kek, uidB, ref) // same KEK, different UID
	if err == nil {
		t.Fatal("ref wrapped for uidA opened under uidB — AAD=UID guard broken")
	}
	if got != nil {
		t.Fatal("expected nil key on AAD mismatch")
	}
}

// TestUnwrap_WrongKEKFails: a ref sealed under KEK A must not open under KEK B —
// a GCM authentication error, never a panic and never a garbage key.
func TestUnwrap_WrongKEKFails(t *testing.T) {
	kekA := hexBytes(t, fakeKEKHex)
	kekB := hexBytes(t, fakeKEK2Hex)
	uid := hexBytes(t, fakeUIDHex)
	key := hexBytes(t, fakeWrapKeyHex)

	ref, err := Wrap(kekA, uid, key)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	got, err := Unwrap(kekB, uid, ref)
	if err == nil {
		t.Fatal("ref sealed under KEK A opened under KEK B, want error")
	}
	if got != nil {
		t.Fatal("expected nil key on wrong-KEK failure")
	}
}

// TestUnwrap_TamperedRefFails: flipping a single bit in any region of the ref
// (nonce, ciphertext or tag) must fail authentication. No region is unprotected.
func TestUnwrap_TamperedRefFails(t *testing.T) {
	kek := hexBytes(t, fakeKEKHex)
	uid := hexBytes(t, fakeUIDHex)
	key := hexBytes(t, fakeWrapKeyHex)

	ref, err := Wrap(kek, uid, key)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	for _, tc := range []struct {
		name string
		idx  int
	}{
		{"nonce_region", 0},
		{"ciphertext_region", gcmNonceLen + 1},
		{"tag_region", wrappedKeyLen - 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := append([]byte(nil), ref...)
			bad[tc.idx] ^= 0x01
			got, err := Unwrap(kek, uid, bad)
			if err == nil {
				t.Fatalf("tampered %s accepted, want error", tc.name)
			}
			if got != nil {
				t.Fatal("expected nil key on tampered ref")
			}
		})
	}
}

// TestUnwrap_WrongLengthRefFails: a ref that is not exactly 44 bytes is rejected
// with an error and never panics.
func TestUnwrap_WrongLengthRefFails(t *testing.T) {
	kek := hexBytes(t, fakeKEKHex)
	uid := hexBytes(t, fakeUIDHex)

	for _, tc := range []struct {
		name string
		ref  []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"short_43", make([]byte, wrappedKeyLen-1)},
		{"long_45", make([]byte, wrappedKeyLen+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Unwrap(kek, uid, tc.ref)
			if err == nil {
				t.Fatalf("ref length %d accepted, want error", len(tc.ref))
			}
			if got != nil {
				t.Fatal("expected nil key on wrong-length ref")
			}
		})
	}
}

// TestUnwrap_LengthCheckedBeforeKEK proves ADR 0003 madde 4's ordering: a
// malformed ref is rejected WITHOUT touching the KEK. We pass a deliberately
// INVALID KEK (5 bytes) together with a short ref; the error must be about the
// ref length, NOT the KEK — which can only happen if the length check runs first.
func TestUnwrap_LengthCheckedBeforeKEK(t *testing.T) {
	badKEK := make([]byte, 5) // would fail aead() if it were reached
	uid := hexBytes(t, fakeUIDHex)
	shortRef := make([]byte, wrappedKeyLen-4)

	_, err := Unwrap(badKEK, uid, shortRef)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "wrapped ref must be") {
		t.Fatalf("expected ref-length error (KEK untouched), got: %v", err)
	}
	if strings.Contains(err.Error(), "KEK") {
		t.Fatalf("KEK was consulted before the ref length check: %v", err)
	}
}

// TestWrap_WrongUIDLength: a uid that is not 7 bytes is a programming error,
// surfaced (§7), not panicked.
func TestWrap_WrongUIDLength(t *testing.T) {
	kek := hexBytes(t, fakeKEKHex)
	key := hexBytes(t, fakeWrapKeyHex)
	for _, n := range []int{0, 6, 8, 14} {
		if _, err := Wrap(kek, make([]byte, n), key); err == nil {
			t.Fatalf("uid length %d accepted, want error", n)
		}
	}
}

// TestWrap_WrongKeyLength: only a 16-byte AES-128 key may be wrapped (keeps the
// ref at a fixed 44 bytes).
func TestWrap_WrongKeyLength(t *testing.T) {
	kek := hexBytes(t, fakeKEKHex)
	uid := hexBytes(t, fakeUIDHex)
	for _, n := range []int{0, 15, 17, 32} {
		if _, err := Wrap(kek, uid, make([]byte, n)); err == nil {
			t.Fatalf("key length %d accepted, want error", n)
		}
	}
}

// TestKEKLengthEnforced: the KEK must be exactly 32 bytes (AES-256). Crucially,
// a 16- or 24-byte KEK — which aes.NewCipher would ACCEPT as AES-128/192 —
// must be REJECTED, so the envelope cannot silently downgrade below AES-256
// (ADR 0003 madde 4). Covers both Wrap and Unwrap.
func TestKEKLengthEnforced(t *testing.T) {
	uid := hexBytes(t, fakeUIDHex)
	key := hexBytes(t, fakeWrapKeyHex)
	// A valid-length ref so Unwrap reaches the KEK check rather than the ref
	// length check.
	validRef := make([]byte, wrappedKeyLen)

	for _, n := range []int{0, 16, 24, 31, 33} {
		kek := make([]byte, n)
		if _, err := Wrap(kek, uid, key); err == nil {
			t.Fatalf("Wrap accepted %d-byte KEK, want error", n)
		}
		if _, err := Unwrap(kek, uid, validRef); err == nil {
			t.Fatalf("Unwrap accepted %d-byte KEK, want error", n)
		}
	}
}

// TestNoPlaintextLeak is the §4.7 acceptance test — and a MUTATION test: it
// asserts that the plaintext key and the KEK appear in NO error message and NOT
// in the wrapped blob. If anyone changes a message to format a key/KEK (e.g.
// fmt.Errorf("...%x", key) or "...%s", kek), one of the assertions below turns
// red. Both raw bytes and hex (lower and upper) are searched, covering the two
// common leak forms (%s and %x).
func TestNoPlaintextLeak(t *testing.T) {
	kek := hexBytes(t, fakeKEKHex)
	kek2 := hexBytes(t, fakeKEK2Hex)
	uid := hexBytes(t, fakeUIDHex)
	uid2 := hexBytes(t, fakeUID2Hex)
	key := hexBytes(t, fakeWrapKeyHex)

	ref, err := Wrap(kek, uid, key)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}

	// The wrapped blob is ciphertext: the plaintext key must NOT appear in it.
	if bytes.Contains(ref, key) {
		t.Fatal("plaintext key found inside aes_key_ref — not actually encrypted")
	}
	// The KEK must never appear in the blob either.
	if bytes.Contains(ref, kek) {
		t.Fatal("KEK bytes found inside aes_key_ref")
	}

	// Collect every error path.
	var errs []error
	push := func(_ any, e error) { errs = append(errs, e) }
	push(Unwrap(kek2, uid, ref))                  // wrong KEK
	push(Unwrap(kek, uid2, ref))                  // wrong UID (AAD)
	push(Unwrap(kek, uid, ref[:wrappedKeyLen-4])) // short ref
	tampered := append([]byte(nil), ref...)       //
	tampered[gcmNonceLen] ^= 0x01                 //
	push(Unwrap(kek, uid, tampered))              // tampered ref
	push(Wrap(kek, uid, make([]byte, 15)))        // wrong key length
	push(Wrap(kek, make([]byte, 6), key))         // wrong uid length
	push(Wrap(make([]byte, 5), uid, key))         // bad KEK length

	secrets := map[string][]byte{"key": key, "KEK": kek}
	for i, e := range errs {
		if e == nil {
			t.Fatalf("error path %d unexpectedly returned nil error", i)
		}
		msg := e.Error()
		for name, secret := range secrets {
			if strings.Contains(msg, string(secret)) {
				t.Fatalf("error %d leaks raw %s bytes: %q", i, name, msg)
			}
			h := hex.EncodeToString(secret)
			if strings.Contains(msg, h) || strings.Contains(msg, strings.ToUpper(h)) {
				t.Fatalf("error %d leaks hex %s: %q", i, name, msg)
			}
		}
	}
}

// TestZero wipes a key buffer in place (§4.7 caller hygiene helper).
func TestZero(t *testing.T) {
	key := hexBytes(t, fakeWrapKeyHex)
	Zero(key)
	for i, b := range key {
		if b != 0 {
			t.Fatalf("Zero left byte %d = %#x, want 0", i, b)
		}
	}
}
