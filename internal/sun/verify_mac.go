package sun

import (
	"crypto/subtle"
	"fmt"
)

// SDM MAC verification (NXP AN12196 SDM, ADR 0003 madde 6). Given a tag's
// plaintext SDMFileRead key (unwrapped by M2-05), the raw 7-byte UID and the
// read counter parsed from the tap URL (M2-03), this file recomputes the
// truncated CMAC the NTAG 424 DNA emits and compares it to the chip's value in
// constant time. It is the crypto core of the "proof of moment" (CLAUDE.md §5).
//
// Cryptography lives ONLY here (CLAUDE.md §4.7). This file logs nothing: it is a
// pure function returning a boolean, so no key, session key, SV2 or CMAC byte
// can ever reach a log line. It touches no DB and makes no decision — the
// counter advance is M2-06 and the verdict is internal/domain/tap.
//
// The algorithm is the five normative steps of ADR 0003 madde 6:
//
//	1. SV2       = 3C C3 00 01 00 80 || UID(7) || ctr(3)
//	2. K_session = AES-CMAC(K_sdmfileread, SV2)
//	3. full      = AES-CMAC(K_session, sdmMacInput)   // sdmMacInput empty
//	4. mac       = full[1], full[3], full[5], … full[15]   // odd-indexed 8 bytes
//	5. subtle.ConstantTimeCompare(mac, chipCMAC)

// sdmSV2Prefix is the fixed 6-byte SV2 label for SDM session-key derivation
// (AN12196 §SDM): 3C C3 00 01 00 80. It precedes the UID and read counter.
var sdmSV2Prefix = [6]byte{0x3C, 0xC3, 0x00, 0x01, 0x00, 0x80}

// verifyMAC reports whether chipCMAC is the genuine truncated SDM MAC for this
// (kSDMFileRead, uid, ctrBytes) triple — i.e. whether the tap was produced by
// the real chip holding this key at this counter (steps 1-5 above).
//
// kSDMFileRead is the 16-byte AES-128 per-tag key (ADR 0003 madde 3), already
// unwrapped in memory by M2-05. uid is the RAW 7-byte UID (Params.UIDBytes), not
// the hex text. ctrBytes is the RAW 3 counter bytes exactly as they appeared in
// the URL (Params.CtrBytes), used VERBATIM in SV2 — NOT the numeric Params.Ctr,
// which exists only for replay ordering (see sv2). chipCMAC is the raw 8-byte
// truncated MAC from the tap (Params.CMAC) — a value to verify, never to log
// (CLAUDE.md §4.7).
//
// The only error path is an invalid AES key length for kSDMFileRead, which is an
// upstream programming error (M2-05 always yields 16 bytes); it is surfaced, not
// swallowed (CLAUDE.md §7). A cryptographic mismatch is (false, nil), never an
// error — a wrong CMAC is a normal reject, not a failure.
func verifyMAC(kSDMFileRead, uid, ctrBytes, chipCMAC []byte) (bool, error) {
	// Step 1+2: derive the per-tap session key from SV2.
	sessionKey, err := cmac(kSDMFileRead, sv2(uid, ctrBytes))
	if err != nil {
		return false, fmt.Errorf("sun: derive session key: %w", err)
	}

	// Step 3: the SDM MAC is computed over an EMPTY message. ADR 0003 madde 2:
	// with UID+counter mirroring and no SDMMACInputOffset, sdmMacInput is a
	// zero-length slice. Adding UID/ctr here (a tempting "fix") makes every
	// verification fail — the chip MACs the empty input. sessionKey[:] is a
	// valid 16-byte AES key, so this cmac cannot fail on key length.
	full, err := cmac(sessionKey[:], nil)
	if err != nil {
		return false, fmt.Errorf("sun: compute sdm mac: %w", err)
	}

	// Step 4: truncate to the 8 odd-indexed bytes the chip actually emits.
	mac := truncateSDMMAC(full)

	// Step 5: constant-time compare. subtle.ConstantTimeCompare returns 1 only
	// when the lengths match AND the bytes are equal, and it does so without a
	// data-dependent early exit — so a byte-by-byte timing side channel cannot
	// leak how much of a forged MAC was correct (CLAUDE.md §4.7). bytes.Equal or
	// == would leak that and are forbidden here (redline R7).
	return subtle.ConstantTimeCompare(mac[:], chipCMAC) == 1, nil
}

// sv2 builds the SDM session-key derivation input (AN12196 §SDM, ADR 0003
// madde 6):
//
//	SV2 = 3C C3 00 01 00 80 || UID(7 bytes) || SDMReadCtr(3 bytes)
//
// SDMReadCtr goes in VERBATIM: ctrBytes is the raw 3-byte counter exactly as it
// appeared (hex-decoded) in the tap URL, appended in URL order without any
// re-serialisation. This is correct regardless of the chip's absolute
// endianness, because the NTAG 424 mirrors SDMReadCtr into the URL with the SAME
// byte order it feeds into SV2 — so the URL bytes ARE the SV2 bytes. Building SV2
// from the numeric Params.Ctr instead would reintroduce a parse-then-serialise
// reversal (the original M2-04 defect: URL big-endian parse + little-endian
// re-serialise flipped every non-palindromic counter and rejected valid taps).
//
// FLAG — this fixes the URL<->SV2 REVERSAL structurally. A SEPARATE axis remains
// open: whether the counter's numeric VALUE (Params.Ctr, decoded big-endian) is
// interpreted with the right endianness for M2-06's monotonic replay ordering.
// That value-endian question does NOT affect this CMAC (which is verbatim) and is
// still to be pinned by a real-chip / AN12196 vector in M2-07. Do not conflate
// the two axes: SV2 is verbatim here; value-endian is confirmed in M2-07.
func sv2(uid, ctrBytes []byte) []byte {
	buf := make([]byte, 0, len(sdmSV2Prefix)+len(uid)+len(ctrBytes))
	buf = append(buf, sdmSV2Prefix[:]...)
	buf = append(buf, uid...)
	buf = append(buf, ctrBytes...) // verbatim: same bytes, same order as the URL
	return buf
}

// truncateSDMMAC extracts the 8-byte SDM MAC the NTAG 424 DNA emits: the
// ODD-indexed bytes of the full 16-byte CMAC — full[1], full[3], … full[15]
// (AN12196 §SDM, ADR 0003 madde 6). This is the single most error-prone step of
// SDM: the chip does not emit the full CMAC, so comparing against the full 16
// bytes (or the first 8) fails EVERY time and misleadingly looks like a wrong
// key (skill tappa-sun, m2-sun.md M2-04 Tuzaklar). full[2*i+1] for i in 0..7
// yields exactly indices 1,3,5,7,9,11,13,15.
func truncateSDMMAC(full [16]byte) [8]byte {
	var mac [8]byte
	for i := 0; i < 8; i++ {
		mac[i] = full[2*i+1]
	}
	return mac
}
