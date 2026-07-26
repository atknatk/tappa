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

	k1, k2 := subkeys(block)

	// Number of blocks; an empty message still has one (padded) block.
	n := (len(msg) + cmacBlockSize - 1) / cmacBlockSize
	if n == 0 {
		n = 1
	}

	// The trailing block: the last cmacBlockSize bytes of msg, or the short
	// remainder, or empty. lastBlock decides the K1/K2 branch and padding.
	tail := msg[(n-1)*cmacBlockSize:]
	last := lastBlock(tail, k1, k2)

	// CBC-MAC with IV = 0: X_i = AES_K(X_{i-1} XOR M_i). RFC 4493 §2.4.
	var x [16]byte
	for i := 0; i < n-1; i++ {
		var m [16]byte
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
func lastBlock(tail []byte, k1, k2 [16]byte) [16]byte {
	var b [16]byte
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
func subkeys(block cipher.Block) (k1, k2 [16]byte) {
	var zero, l [16]byte
	block.Encrypt(l[:], zero[:])
	k1 = dbl(l)
	k2 = dbl(k1)
	return k1, k2
}

// dbl doubles a 128-bit value in GF(2^128): a left shift by one bit, then a
// conditional XOR with Rb when the pre-shift high bit was set (RFC 4493 §2.3).
func dbl(in [16]byte) [16]byte {
	out := shiftLeft(in)
	if in[0]&0x80 != 0 {
		out[cmacBlockSize-1] ^= rb
	}
	return out
}

// shiftLeft shifts a 16-byte big-endian value left by one bit.
func shiftLeft(in [16]byte) [16]byte {
	var out [16]byte
	var carry byte
	for i := cmacBlockSize - 1; i >= 0; i-- {
		out[i] = in[i]<<1 | carry
		carry = in[i] >> 7
	}
	return out
}

// xor16 XORs src into dst in place.
func xor16(dst, src *[16]byte) {
	for i := range dst {
		dst[i] ^= src[i]
	}
}
