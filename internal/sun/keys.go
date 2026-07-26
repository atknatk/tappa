package sun

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
)

// Envelope encryption for per-tag NTAG 424 DNA AES keys (ADR 0003 madde 4,
// CLAUDE.md §4.7). A tag's 16-byte AES-128 SDMFileRead key is NEVER stored,
// logged or checked into the repo in the clear: it lives KEK-wrapped in
// tags.aes_key_ref and is opened into memory only for the duration of a single
// SUN verification.
//
// Wrap/Unwrap use AES-256-GCM under the process KEK (TAPPA_TAG_KEK — 32 bytes,
// validated and held by internal/config as Config.TagKEK). The wrapped blob is:
//
//	tags.aes_key_ref = nonce(12) || ciphertext(16) || gcm_tag(16)   = 44 bytes
//
// which is exactly what gcm.Seal(nonce, nonce, key, aad) emits — no extra
// serialisation. The GCM AAD is the tag's RAW 7-byte UID: this BINDS the wrapped
// key to its own tag row, so tappa_app's UPDATE grant on tags cannot move one
// tag's wrapped key onto another tag row and still have it open (ADR 0003 madde
// 4 — the portability guard; same belt-and-braces ethos as the composite FKs of
// M1-05). A wrong KEK, a wrong UID (AAD mismatch) or any tampering of the blob
// all surface as a GCM authentication error — never a panic, never a silent
// garbage key.
//
// KEK HANDLING (§7, and the "no cached plaintext key" trap in m2-sun.md M2-05).
// The KEK is passed in as an explicit PARAMETER on every call, exactly as the
// card sanctions ("Wrap/Unwrap KEK'i parametre/bağımlılık olarak alsın"). The
// package therefore holds NO KEK state at all — no package-level global, no
// cached struct field — and never retains an unwrapped tag key either. Callers
// pass Config.TagKEK; the only long-lived copy of the KEK is config's.
//
// Cryptography lives ONLY in internal/sun (CLAUDE.md §4.7). This file logs
// nothing; its errors carry lengths and fixed strings, never key or KEK bytes,
// so no plaintext can reach a log line (proven by the leak tests in
// keys_test.go, which fail red if a message ever formats a key/KEK).

const (
	// kekLen is the required KEK size: 32 bytes = AES-256. Enforced explicitly,
	// because aes.NewCipher would ALSO accept 16/24 bytes (AES-128/192) and
	// silently downgrade the envelope below the ADR-mandated AES-256.
	kekLen = 32
	// tagKeyLen is the per-tag AES-128 SDMFileRead key size (ADR 0003 madde 3).
	tagKeyLen = 16
	// uidLen is the raw UID length used as GCM AAD (ADR 0003 madde 4/6).
	uidLen = 7
	// gcmNonceLen is the standard 96-bit GCM nonce.
	gcmNonceLen = 12
	// gcmTagLen is the GCM authentication tag length.
	gcmTagLen = 16
	// wrappedKeyLen is the FIXED tags.aes_key_ref length for a 16-byte tag key:
	// nonce || ciphertext (GCM is a stream cipher — no padding, so ciphertext is
	// the plaintext length) || tag = 12 + 16 + 16 = 44.
	wrappedKeyLen = gcmNonceLen + tagKeyLen + gcmTagLen
)

// Wrap seals a 16-byte per-tag AES key under kek (the 32-byte AES-256 KEK),
// authenticating it against uid (the RAW 7-byte tag UID) as GCM AAD, and returns
// the 44-byte value for tags.aes_key_ref: nonce(12) || ciphertext(16) || tag(16)
// (ADR 0003 madde 4).
//
// The nonce is fresh crypto/rand bytes on every call, so wrapping the SAME
// (uid, key) twice yields DIFFERENT outputs — there is no nonce reuse under the
// single long-lived KEK.
//
// Errors report only lengths (uid, key or kek size), never bytes (§4.7), and are
// returned, not panicked (§7). The input key belongs to the caller: Wrap neither
// stores nor zeroizes it (the caller owns its lifetime).
func Wrap(kek, uid, key []byte) ([]byte, error) {
	if len(uid) != uidLen {
		return nil, fmt.Errorf("sun: wrap: uid must be %d bytes, got %d", uidLen, len(uid))
	}
	if len(key) != tagKeyLen {
		return nil, fmt.Errorf("sun: wrap: tag key must be %d bytes, got %d", tagKeyLen, len(key))
	}
	g, err := aead(kek)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcmNonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("sun: wrap: read nonce: %w", err)
	}

	// gcm.Seal(nonce, nonce, key, uid) appends ciphertext||tag onto the nonce
	// slice, producing nonce||ciphertext||tag natively — the exact ADR 0003
	// layout, no manual concatenation. uid is the AAD: authenticated but NOT
	// written into the blob, so the length stays a fixed 44 regardless.
	return g.Seal(nonce, nonce, key, uid), nil
}

// Unwrap opens a 44-byte tags.aes_key_ref, authenticating it against uid (the
// RAW 7-byte tag UID) as GCM AAD, and returns the plaintext 16-byte per-tag key.
//
// The lengths are checked FIRST: a uid that is not 7 bytes or a ref that is not
// exactly 44 bytes is rejected WITHOUT touching the KEK (ADR 0003 madde 4). A
// wrong KEK, a wrong UID (AAD mismatch — the portability guard: a ref moved to
// another tag row will not open) or any tampering of the ref all fail as a GCM
// authentication error — returned, never panicked (§7). On that failure GCM
// leaves no plaintext behind (crypto/cipher zeroes its output before returning).
//
// The returned key is the ONLY plaintext; Unwrap keeps no copy. Zeroizing it
// once the verification completes is the CALLER's duty (§4.7) — use Zero.
func Unwrap(kek, uid, ref []byte) ([]byte, error) {
	if len(uid) != uidLen {
		return nil, fmt.Errorf("sun: unwrap: uid must be %d bytes, got %d", uidLen, len(uid))
	}
	if len(ref) != wrappedKeyLen {
		// Length checked before the KEK is ever built: a malformed ref never
		// reaches the cipher (ADR 0003 madde 4).
		return nil, fmt.Errorf("sun: unwrap: wrapped ref must be %d bytes, got %d", wrappedKeyLen, len(ref))
	}
	g, err := aead(kek)
	if err != nil {
		return nil, err
	}

	nonce := ref[:gcmNonceLen]
	sealed := ref[gcmNonceLen:] // ciphertext || tag
	// dst=nil so gcm.Open allocates the plaintext fresh; no intermediate copy of
	// the key is made here, so nothing internal needs zeroizing (the return
	// value is the sole plaintext, the caller's responsibility).
	key, err := g.Open(nil, nonce, sealed, uid)
	if err != nil {
		// crypto/cipher's error is the fixed string "cipher: message
		// authentication failed" — it carries no key or KEK bytes, so wrapping
		// it with %w is safe (§4.7) and preserves the chain (§7). It does NOT
		// distinguish wrong-KEK from wrong-UID from tampering, by design: a
		// caller learns only "did not authenticate".
		return nil, fmt.Errorf("sun: unwrap: %w", err)
	}
	return key, nil
}

// Zero wipes a plaintext key buffer once the caller is done with it (§4.7). The
// unwrapped key must live only for a single verification; the caller MUST Zero
// it afterwards so it does not linger in memory (a dump must not yield the park).
// clear sets every byte to zero.
func Zero(key []byte) { clear(key) }

// aead builds the AES-256-GCM AEAD from the KEK for a single Wrap/Unwrap. It is
// built per call — never cached — so no KEK-derived key schedule outlives the
// operation. The explicit length check enforces AES-256 (kekLen); aes.NewCipher
// alone would accept 16/24 bytes and silently downgrade the envelope. Errors
// report only the length, never the KEK bytes (§4.7).
func aead(kek []byte) (cipher.AEAD, error) {
	if len(kek) != kekLen {
		return nil, fmt.Errorf("sun: KEK must be %d bytes, got %d", kekLen, len(kek))
	}
	block, err := aes.NewCipher(kek)
	if err != nil {
		// Unreachable given the length check above (32 is a valid AES key size);
		// kept as defence. aes.NewCipher's error names the size, never the bytes.
		return nil, fmt.Errorf("sun: init KEK cipher: %w", err)
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		// Unreachable for AES (a 16-byte block always yields a valid GCM); kept
		// as defence. NewGCM's error carries no key bytes.
		return nil, fmt.Errorf("sun: init GCM: %w", err)
	}
	return g, nil
}
