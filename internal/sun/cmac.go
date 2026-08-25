// Package sun verifies NTAG 424 DNA SUN/SDM taps: AES-CMAC, the monotonic
// read counter and atomic replay protection. Cryptography lives ONLY here
// (CLAUDE.md §4.7); handlers never compute a CMAC.
package sun

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"
)

// AES-CMAC per RFC 4493. Built on crypto/aes only — no third-party crypto
// dependency (ADR 0001). This file produces the FULL 16-byte tag; the SDM
// truncation to 8 tag bytes is M2-04's job and never happens here.
//
// 🔴 ZEROISATION (CLAUDE.md §4.7, backlog T64). Between 2026-07-26 (2380baa) and
// 2026-08-25 this file wiped NOTHING, on every MAC path the product has —
// verify_mac.go's two calls are the live tap flow, ev2.go's four are
// personalisation. What that left behind, in descending weight:
//
//   - x, the CBC accumulator, IS the CMAC output at the moment of return, and at
//     ev2.go's ev2SessionKeys that output is literally KSesAuthENC / KSesAuthMAC.
//     ev2SessionKeys wipes its own copies carefully; this file kept a second,
//     unwiped one a layer below. Same class as the leak F2 found, one floor down.
//   - k1/k2 and subkeys' l. Weight stated at the strength it survives: K1 = dbl(L)
//     with L = AES_K(0¹²⁸) and dbl is INVERTIBLE, so leaking K1 or K2 yields
//     exactly ONE known plaintext/ciphertext pair for AES_K. That is not "equal to
//     forging CMACs" — it does not evaluate AES_K on any other block — and the
//     earlier, stronger wording was withdrawn under audit.
//   - lastBlock's b and its by-value k1/k2, and cmac's per-block scratch m.
//
// 🔴 WHAT IS COUNTED, NOT CLOSED: block. aes.NewCipher's *aes.Block is the FULLY
// EXPANDED round-key schedule, and crypto/aes offers no way to wipe it. MEASURED
// on go1.26.7 darwin/amd64 (a throwaway probe outside the repo, printing only a
// boolean and an offset): the 16 key bytes appear VERBATIM, TWICE, inside the
// Block's 488-byte allocation, the first at offset 8 — round key 0 of the
// encryption schedule is the cipher key itself (FIPS 197 §5.2). So this is not
// weaker than k1/k2, it is the STRONGEST leak in the file, and it is the one
// nothing here can fix. The limit and its five call sites are inventoried at
// TestCMAC_TheUnwipeableCipherBlocksAreCounted.
//
// The mechanism, not a sentence: TestCMAC_EveryByteBufferIsWipedOrExempt walks
// this file's AST and requires EVERY byte-sequence local, parameter and named
// result to be either wiped or exempt-with-a-reason. What that gate cannot see is
// written down in the test, measured, not guessed.
//
// 🔴 A WIPE PLACED ONE SCOPE TOO EARLY PASSES THE TESTS AND CORRUPTS PRODUCTION.
// Two rules make that concrete, and both are load-bearing below:
//   - NEVER wipe a named result. subkeys returns (k1, k2) by name; a deferred
//     Zero there runs BEFORE the caller sees them and returns zeros.
//   - NEVER wipe a slice this file did not allocate. key, msg and tail all alias
//     the caller's memory. tail in particular is msg's last block, and wiping it
//     would leave every KAT green while silently shredding the caller's buffer —
//     TestCMAC_DoesNotMutateItsInputs exists for exactly that mutation.
//
// By-value [16]byte parameters are the opposite case: they are this frame's own
// copies, so wiping them is both safe and required.

const (
	// cmacBlockSize is the AES block size in bytes. AES has a 128-bit block
	// for every key length, so this is 16 regardless of AES-128/192/256.
	cmacBlockSize = 16

	// rb is the last byte of the RFC 4493 constant Rb (0x00…0087) for a
	// 128-bit block. It is XORed in during subkey doubling when the shifted
	// value overflowed the high bit.
	rb byte = 0x87
)

// cmac returns the full 16-byte RFC 4493 AES-CMAC of msg keyed by key.
//
// key must be a valid AES key length (16/24/32 bytes); Tappa always uses
// AES-128 (the 16-byte per-tag K_sdmfileread, ADR 0003). msg may be empty —
// the SDM MAC is computed over an empty input (ADR 0003 madde 2).
//
// This returns the UNtruncated tag. M2-04 chains it twice to verify a tap:
//
//	ks, err := cmac(kSdmFileRead, sv2) // K_session = CMAC(K_sdmfileread, SV2)
//	full, err := cmac(ks[:], nil)      // full      = CMAC(K_session, "")
//
// and only then truncates full to the 8 odd-indexed bytes the chip emits.
// The aes.NewCipher error is surfaced, never swallowed (CLAUDE.md §7); it can
// only fire on an invalid key length, i.e. a programming error upstream.
func cmac(key, msg []byte) ([16]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		// aes.NewCipher only fails on an invalid key length; its error
		// reports the length, never the key bytes, so wrapping is safe.
		return [16]byte{}, fmt.Errorf("sun: invalid cipher key length: %w", err)
	}

	// block is the expanded AES round-key schedule and crypto/aes offers no way to
	// wipe it: the counted limit in the file header, not an oversight here.
	k1, k2 := subkeys(block)
	defer Zero(k1[:])
	defer Zero(k2[:])

	// Number of blocks; an empty message still has one (padded) block.
	n := (len(msg) + cmacBlockSize - 1) / cmacBlockSize
	if n == 0 {
		n = 1
	}

	// The trailing block: the last cmacBlockSize bytes of msg, or the short
	// remainder, or empty. lastBlock decides the K1/K2 branch and padding.
	// tail ALIASES msg — it is the caller's memory and is never wiped here.
	tail := msg[(n-1)*cmacBlockSize:]
	last := lastBlock(tail, k1, k2)
	defer Zero(last[:])

	// CBC-MAC with IV = 0: X_i = AES_K(X_{i-1} XOR M_i). RFC 4493 §2.4.
	//
	// x is deferred-wiped even though it is also the return value: `return x` is
	// evaluated, and the result copied, BEFORE any deferred call runs, so the
	// caller receives the tag and this frame keeps zeros. The result is
	// deliberately UNNAMED so that stays true — naming it would make this defer
	// zero what the caller gets. TestCMAC_RFC4493Vectors is the control.
	var x [16]byte
	defer Zero(x[:])

	// m is hoisted out of the loop so ONE defer covers it on every exit, panics
	// included — a defer written inside the loop would stack per iteration.
	// Hoisting is safe because every iteration copies a FULL 16-byte block (the
	// short tail is lastBlock's job), so no byte of a previous block survives.
	var m [16]byte
	defer Zero(m[:])
	for i := 0; i < n-1; i++ {
		copy(m[:], msg[i*cmacBlockSize:(i+1)*cmacBlockSize])
		xor16(&x, &m)
		block.Encrypt(x[:], x[:])
	}
	xor16(&x, &last)
	block.Encrypt(x[:], x[:])
	return x, nil
}

// lastBlock builds the final CMAC block from the trailing bytes of the
// message (RFC 4493 §2.4). A full 16-byte tail is XORed with K1; anything
// shorter — including an empty tail — gets 10* padding (a 0x80 byte then
// zeros) and is XORed with K2.
//
// k1 and k2 arrive BY VALUE, so the wipes below scrub this frame's own copies and
// the caller's subkeys are untouched. tail is the caller's slice and is NOT wiped
// — see the file header. b is returned by value, and like cmac's x the copy is
// made before the deferred wipe runs; the result stays unnamed so it stays true.
func lastBlock(tail []byte, k1, k2 [16]byte) [16]byte {
	defer Zero(k1[:])
	defer Zero(k2[:])
	var b [16]byte
	defer Zero(b[:])
	copy(b[:], tail)
	if len(tail) == cmacBlockSize {
		xor16(&b, &k1)
		return b
	}
	b[len(tail)] = 0x80
	xor16(&b, &k2)
	return b
}

// subkeys derives K1 and K2 from the block cipher (RFC 4493 §2.3):
//
//	L  = AES_K(0^128)
//	K1 = dbl(L)
//	K2 = dbl(K1)
//
// 🔴 k1 and k2 ARE NAMED RESULTS AND MUST NEVER BE WIPED HERE. A deferred Zero on
// either runs before the caller reads it and hands back sixteen zero bytes, which
// is a silent, total CMAC break. Wiping them is cmac's job, in cmac's frame.
// zero needs no wipe either, for a different reason: it is the all-zero plaintext
// block, only ever READ (Encrypt writes to l), so it never holds a secret.
func subkeys(block cipher.Block) (k1, k2 [16]byte) {
	var zero, l [16]byte
	defer Zero(l[:]) // L = AES_K(0¹²⁸) — key-derived
	block.Encrypt(l[:], zero[:])
	k1 = dbl(l)
	k2 = dbl(k1)
	return k1, k2
}

// dbl doubles a 128-bit value in GF(2^128): a left shift by one bit, then a
// conditional XOR with Rb when the pre-shift high bit was set (RFC 4493 §2.3).
//
// in is a BY-VALUE copy of L or of K1, i.e. a third and fourth copy of key-derived
// material that T64's list did not name — the AST gate below did. Wiping it is
// safe precisely because it is a copy; the caller's array is a different one. The
// deferred wipe runs after `in[0]` has been read, because defers run at return.
// out is the return value and is the caller's to wipe.
func dbl(in [16]byte) [16]byte {
	defer Zero(in[:])
	out := shiftLeft(in)
	if in[0]&0x80 != 0 {
		out[cmacBlockSize-1] ^= rb
	}
	return out
}

// shiftLeft shifts a 16-byte big-endian value left by one bit. in is again this
// frame's own copy and is wiped; out is returned.
//
// ⚠️ carry is NOT wiped and cannot be: it is a scalar byte, it lives in a register,
// and Go offers no way to scrub a register — the same class of unclosable limit as
// block, three orders of magnitude smaller (one bit of one key-derived byte at a
// time). Named here rather than left silent.
func shiftLeft(in [16]byte) [16]byte {
	defer Zero(in[:])
	var out [16]byte
	var carry byte
	for i := cmacBlockSize - 1; i >= 0; i-- {
		out[i] = in[i]<<1 | carry
		carry = in[i] >> 7
	}
	return out
}

// xor16 XORs src into dst in place. Both parameters are POINTERS into arrays owned
// by other frames, so nothing here is this function's to wipe: a Zero through dst
// or src would destroy a caller's live buffer, not scrub a copy.
func xor16(dst, src *[16]byte) {
	for i := range dst {
		dst[i] ^= src[i]
	}
}
