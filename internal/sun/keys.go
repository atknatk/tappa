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

// KEKLen and WrappedKeyLen are the two sizes callers OUTSIDE this package have to
// agree with, exported in M8-05 FAZ B2c-2a so that agreement is a reference rather
// than a second copy of a number.
//
// 🔴 THE ALTERNATIVE WAS MEASURED AND IT IS WHY THESE EXIST. internal/encode has to
// refuse a mis-sized KEK at CONSTRUCTION (a configuration fault should be a startup
// failure, not an error in the middle of a round with a chip already selected) and a
// mis-sized envelope BEFORE the INSERT (migration 00021's
// tags_aes_key_ref_is_kek_envelope rejects it, and a CHECK violation's DETAIL line is
// the whole failing tuple — §4.7). Both checks need the number. Writing `32` and `44`
// over there would put this schema's shape in a third and fourth place; internal/config's
// key32 already holds a second 32, and that one is about the ENV VAR's shape rather
// than the envelope's.
//
// ⚠️ WHAT THEY ARE NOT: they are not a licence to build an envelope elsewhere.
// Wrap/Unwrap remain the only constructors, and the cryptography stays here
// (CLAUDE.md §4.7).
const (
	// KEKLen is the required TAPPA_TAG_KEK size — 32 bytes, i.e. AES-256.
	KEKLen = kekLen
	// WrappedKeyLen is the exact tags.aes_key_ref length, ADR 0003 md. 4's 44.
	WrappedKeyLen = wrappedKeyLen
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

// UnwrapAny opens ref under the FIRST of keks that authenticates it, and exists
// for exactly one situation: a KEK rotation (cmd/rotatekek, and the "KEK
// döndürme" runbook in deploy/README.md).
//
// WHY IT HAS TO EXIST. Nothing in the schema records which KEK a row is sealed
// under — there is no version column on tags, deliberately (adding one would
// still require both keys to be present in the process, so it buys a migration
// and no safety). Re-sealing 33 000 rows is therefore a period during which the
// park is MIXED, and a process holding one KEK answers 500 to every tap on the
// other half. That is §4.6 failing beneath the product: the record is not
// flagged, it is never taken.
//
// ORDER IS A COST DECISION AND NOTHING MORE, and this paragraph used to claim
// otherwise in its own first sentence while denying it in its last. The claim was
// tested and removed rather than reworded: a given ref authenticates under exactly
// ONE key, so reversing this loop changes which attempt succeeds first and changes
// no outcome — an auditor's mutation that reversed it left every test green, and
// correctly so. keks[0] is simply the key the caller expects to win (during a
// rotation, the NEW one), which saves one GCM open per tap once most of the park
// has moved.
//
// WHAT IS LOAD-BEARING is the CONTENT of the list, not its order: every key in it
// is equally trusted, so a leaked key stays valid for exactly as long as it stays
// in this slice. That is why the rotation's last step is to empty it again, and
// why cmd/tappa announces an open window for as long as one is configured.
//
// The error names NO key and does not say WHICH position failed: a caller learns
// "no configured KEK opened this", never "the second one nearly worked" (§4.7).
// An empty list is an error rather than a silent failure — a caller that lost its
// keys must not look like a caller holding a corrupt ref.
func UnwrapAny(keks [][]byte, uid, ref []byte) ([]byte, error) {
	if len(keks) == 0 {
		return nil, fmt.Errorf("sun: unwrap: no KEK configured")
	}
	var firstErr error
	for _, kek := range keks {
		key, err := Unwrap(kek, uid, ref)
		if err == nil {
			return key, nil
		}
		if firstErr == nil {
			// The FIRST error is kept rather than the last, so a caller reading a
			// log sees the failure of the key that was supposed to work. Which
			// position produced it is not reported.
			firstErr = err
		}
	}
	return nil, firstErr
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
