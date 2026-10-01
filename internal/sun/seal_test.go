package sun

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// Tests for the GENERAL envelope, Seal/Open (M10 OP-6, ADR 0020 §1). The operator's
// TOTP secret is its first user: 160 bits sealed under TAPPA_OPERATOR_TOTP_KEK with
// AAD = the 16 bytes of platform_admins.id.
//
// Key material here is FAKE: the two KEKs are keys_test.go's labelled constants, the
// plaintexts and AADs are derived at run time from fixed labels (sha256), so no new
// key-shaped literal enters the repository.

// sealFixture returns a fake 20-byte "secret" and a fake 16-byte "row id", both
// derived from labels so a failure can be reproduced exactly.
func sealFixture(label string) (plaintext, aad []byte) {
	p := sha256.Sum256([]byte("tappa/sun/seal_test/plaintext/" + label))
	a := sha256.Sum256([]byte("tappa/sun/seal_test/aad/" + label))
	return p[:20], a[:16]
}

// TestSeal_RoundTripAndTheOperatorEnvelopeSize: Open(Seal(x)) == x, the layout is
// nonce || ciphertext || tag with no framing, and the operator's 160-bit secret
// seals to 48 bytes -- inside 00026's floor (>= 44, the 128-bit minimum).
func TestSeal_RoundTripAndTheOperatorEnvelopeSize(t *testing.T) {
	kek := hexBytes(t, fakeKEKHex)
	pt, aad := sealFixture("roundtrip")

	env, err := Seal(kek, aad, pt)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if SealOverhead != 28 || len(env) != SealOverhead+len(pt) {
		t.Fatalf("envelope is %d bytes (overhead %d) for a %d-byte plaintext, want 12+%d+16",
			len(env), SealOverhead, len(pt), len(pt))
	}
	if len(env) != 48 || len(env) < 44 {
		t.Fatalf("a 160-bit plaintext sealed to %d bytes; 00026 says 48 and bounds the column at >= 44", len(env))
	}
	got, err := Open(kek, aad, env)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, pt) {
		t.Fatal("round trip changed the plaintext")
	}
	// The nonce is not a derivation of anything the caller passed: it is random, so
	// the envelope must not contain the plaintext and must not start with the aad.
	if bytes.Contains(env, pt) || bytes.HasPrefix(env, aad) {
		t.Fatal("the envelope carries the plaintext or the aad in the clear")
	}
}

// TestSeal_FreshNoncePerCall: two seals of the same (kek, aad, plaintext) differ in
// their nonce and both open. A fixed nonce under a long-lived KEK is the classic
// GCM catastrophe (a repeated nonce reveals the XOR of the two plaintexts and the
// authentication key).
func TestSeal_FreshNoncePerCall(t *testing.T) {
	kek := hexBytes(t, fakeKEKHex)
	pt, aad := sealFixture("nonce")
	seen := map[string]bool{}
	for i := 0; i < 32; i++ {
		env, err := Seal(kek, aad, pt)
		if err != nil {
			t.Fatalf("Seal %d: %v", i, err)
		}
		n := hex.EncodeToString(env[:gcmNonceLen])
		if seen[n] {
			t.Fatalf("nonce repeated after %d seals", i)
		}
		seen[n] = true
		if _, err := Open(kek, aad, env); err != nil {
			t.Fatalf("envelope %d does not open: %v", i, err)
		}
	}
}

// TestOpen_AWrongKEKCannotOpen is the OP-6 acceptance "yanlış KEK açamaz" at the
// envelope level. The wrong keys are chosen to catch a TRUNCATED key schedule, not
// only a different one: a KEK differing in its LAST byte only, and one differing in
// its FIRST byte only. A cipher that silently used half the KEK would open one of
// them.
func TestOpen_AWrongKEKCannotOpen(t *testing.T) {
	kek := hexBytes(t, fakeKEKHex)
	pt, aad := sealFixture("wrongkek")
	env, err := Seal(kek, aad, pt)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	lastByte := append([]byte(nil), kek...)
	lastByte[len(lastByte)-1] ^= 0x01
	firstByte := append([]byte(nil), kek...)
	firstByte[0] ^= 0x80
	for name, wrong := range map[string][]byte{
		"a different KEK":                     hexBytes(t, fakeKEK2Hex),
		"the KEK with its last byte flipped":  lastByte,
		"the KEK with its first byte flipped": firstByte,
	} {
		got, err := Open(wrong, aad, env)
		if err == nil || got != nil {
			t.Errorf("%s opened the envelope (err=%v)", name, err)
		}
	}
	// CONTROL: the right KEK still opens it, so the refusals above are the KEK.
	if _, err := Open(kek, aad, env); err != nil {
		t.Fatalf("CONTROL: the sealing KEK cannot open its own envelope: %v", err)
	}
}

// TestOpen_TheAADIsBoundUncut is ADR 0020 §1 md.2: the envelope is bound to the
// row's id, all sixteen bytes of it. Another id, the id cut by one byte, the id with
// one byte appended, and a tag-uid-shaped 7-byte prefix all fail.
func TestOpen_TheAADIsBoundUncut(t *testing.T) {
	kek := hexBytes(t, fakeKEKHex)
	pt, aad := sealFixture("aad")
	_, other := sealFixture("another row")
	env, err := Seal(kek, aad, pt)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	flipped := append([]byte(nil), aad...)
	flipped[len(flipped)-1] ^= 0x01
	for name, wrong := range map[string][]byte{
		"another row's id":                 other,
		"the id cut to 15 bytes":           aad[:15],
		"the id with a byte appended":      append(append([]byte(nil), aad...), 0x00),
		"the id's first 7 bytes":           aad[:7],
		"the id with its last bit flipped": flipped,
	} {
		if got, err := Open(kek, wrong, env); err == nil || got != nil {
			t.Errorf("an envelope sealed to one id opened under %s", name)
		}
	}
	if _, err := Open(kek, aad, env); err != nil {
		t.Fatalf("CONTROL: the envelope does not open under its own id: %v", err)
	}
}

// TestOpen_EveryByteIsAuthenticated: flipping any single bit of the envelope --
// nonce, ciphertext or tag -- fails. Walked over every position, not three samples.
func TestOpen_EveryByteIsAuthenticated(t *testing.T) {
	kek := hexBytes(t, fakeKEKHex)
	pt, aad := sealFixture("tamper")
	env, err := Seal(kek, aad, pt)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	for i := range env {
		bad := append([]byte(nil), env...)
		bad[i] ^= 0x01
		if got, err := Open(kek, aad, bad); err == nil || got != nil {
			t.Fatalf("a flipped bit at byte %d of %d was accepted", i, len(env))
		}
	}
}

// TestOpen_ShapeIsCheckedBeforeTheKEK is Unwrap's ordering for the general envelope:
// a malformed envelope or an empty aad is refused WITHOUT the KEK being consulted --
// shown by pairing each with a KEK that aead() would refuse, and asserting the error
// is about the shape.
func TestOpen_ShapeIsCheckedBeforeTheKEK(t *testing.T) {
	badKEK := make([]byte, 5)
	_, aad := sealFixture("shape")
	for _, n := range []int{0, 1, SealOverhead - 1, SealOverhead} {
		_, err := Open(badKEK, aad, make([]byte, n))
		if err == nil || !strings.Contains(err.Error(), "envelope must be longer") || strings.Contains(err.Error(), "KEK") {
			t.Errorf("a %d-byte envelope: %v, want the shape error before any KEK check", n, err)
		}
	}
	_, err := Open(badKEK, nil, make([]byte, 48))
	if err == nil || !strings.Contains(err.Error(), "aad must not be empty") {
		t.Errorf("an empty aad: %v, want the aad error before any KEK check", err)
	}
}

// TestSeal_RefusesAnUnboundOrEmptyEnvelope: an envelope bound to nothing (empty aad)
// is portable to every row, and one holding nothing satisfies a length floor while
// protecting no value; both are refused, by Seal and by Open.
func TestSeal_RefusesAnUnboundOrEmptyEnvelope(t *testing.T) {
	kek := hexBytes(t, fakeKEKHex)
	pt, aad := sealFixture("refuse")
	if _, err := Seal(kek, nil, pt); err == nil {
		t.Error("Seal accepted a nil aad")
	}
	if _, err := Seal(kek, []byte{}, pt); err == nil {
		t.Error("Seal accepted an empty aad")
	}
	if _, err := Seal(kek, aad, nil); err == nil {
		t.Error("Seal accepted an empty plaintext")
	}
	env, err := Seal(kek, aad, pt)
	if err != nil {
		t.Fatalf("CONTROL: %v", err)
	}
	if _, err := Open(kek, []byte{}, env); err == nil {
		t.Error("Open accepted an empty aad")
	}
}

// TestSeal_KEKLengthEnforced: only a 32-byte KEK (AES-256). aes.NewCipher alone
// would accept 16 or 24 bytes and silently seal under AES-128/192.
func TestSeal_KEKLengthEnforced(t *testing.T) {
	pt, aad := sealFixture("keklen")
	valid := make([]byte, 48)
	for _, n := range []int{0, 16, 24, 31, 33} {
		if _, err := Seal(make([]byte, n), aad, pt); err == nil {
			t.Errorf("Seal accepted a %d-byte KEK", n)
		}
		if _, err := Open(make([]byte, n), aad, valid); err == nil {
			t.Errorf("Open accepted a %d-byte KEK", n)
		}
	}
}

// TestSeal_NoPlaintextOrKEKInAnyError is §4.7 for the general envelope: the
// plaintext and the KEK appear in no error, raw or hex (both cases).
func TestSeal_NoPlaintextOrKEKInAnyError(t *testing.T) {
	kek := hexBytes(t, fakeKEKHex)
	pt, aad := sealFixture("leak")
	env, err := Seal(kek, aad, pt)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	tampered := append([]byte(nil), env...)
	tampered[len(tampered)-1] ^= 0x01
	var errs []error
	push := func(_ any, e error) { errs = append(errs, e) }
	push(Open(hexBytes(t, fakeKEK2Hex), aad, env))
	push(Open(kek, aad[:15], env))
	push(Open(kek, aad, tampered))
	push(Open(kek, aad, env[:SealOverhead]))
	push(Open(kek, nil, env))
	push(Seal(kek, nil, pt))
	push(Seal(kek, aad, nil))
	push(Seal(kek[:31], aad, pt))
	for i, e := range errs {
		if e == nil {
			t.Fatalf("error path %d returned no error", i)
		}
		msg := e.Error()
		for name, v := range map[string][]byte{"plaintext": pt, "KEK": kek} {
			h := hex.EncodeToString(v)
			if strings.Contains(msg, string(v)) || strings.Contains(msg, h) || strings.Contains(msg, strings.ToUpper(h)) {
				t.Fatalf("error %d carries the %s: %q", i, name, msg)
			}
		}
	}
}

// TestSeal_WrapIsTheSameFormat pins a FACT this design leans on, so that it cannot
// change unnoticed: a Wrap ref IS a Seal envelope (uid as the aad, a 16-byte key as
// the plaintext), in both directions. What keeps a plaque key and an operator TOTP
// secret apart is therefore NOT the format -- it is the KEK (TAPPA_TAG_KEK vs
// TAPPA_OPERATOR_TOTP_KEK, whose inequality is OP-7's start-up check) and the aad's
// length (7 vs 16 bytes, which can never be equal).
func TestSeal_WrapIsTheSameFormat(t *testing.T) {
	kek := hexBytes(t, fakeKEKHex)
	uid := hexBytes(t, fakeUIDHex)
	key := hexBytes(t, fakeWrapKeyHex)
	ref, err := Wrap(kek, uid, key)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	got, err := Open(kek, uid, ref)
	if err != nil || !bytes.Equal(got, key) {
		t.Fatalf("a Wrap ref does not open with Open(uid): %v", err)
	}
	env, err := Seal(kek, uid, key)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if got, err := Unwrap(kek, uid, env); err != nil || !bytes.Equal(got, key) {
		t.Fatalf("a Seal envelope of a tag-shaped key does not Unwrap: %v", err)
	}
}
