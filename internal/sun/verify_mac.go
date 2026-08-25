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
//	1. SV2       = 3C C3 00 01 00 80 || UID(7) || ctr(3, LSB-first — see sv2)
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
// the URL (Params.CtrBytes), i.e. in the URL's BIG-ENDIAN order — sv2 reverses
// them into SV2's LSB-first order, so do NOT pre-reverse them here. chipCMAC is the raw 8-byte
// truncated MAC from the tap (Params.CMAC) — a value to verify, never to log
// (CLAUDE.md §4.7).
//
// The only error path is an invalid AES key length for kSDMFileRead, which is an
// upstream programming error (M2-05 always yields 16 bytes); it is surfaced, not
// swallowed (CLAUDE.md §7). A cryptographic mismatch is (false, nil), never an
// error — a wrong CMAC is a normal reject, not a failure.
//
// 🔴 ZEROISATION (§4.7, backlog T64). sessionKey, full AND mac are all wiped on
// every exit path, error returns included, by deferring. kSDMFileRead is the
// CALLER's unwrapped key — verify.go:250-256 unwraps it and Zeroes it — so wiping
// it here would destroy a buffer this function does not own; uid, ctrBytes and
// chipCMAC are the caller's too.
//
// 🔴 WHY mac IS WIPED, AND WHY THE FIRST VERSION OF THIS SENTENCE WAS WRONG. It
// said mac "is bit-for-bit the value the chip puts on the wire, which the attacker
// already has", and cited ev2.go's rule that MAC tags need no wipe. Both halves
// fail HERE. mac is eight bytes of a CMAC computed under the per-tap SDM session
// key: key-derived by definition. And ev2.go's tag is written into an outgoing
// command and genuinely leaves the process; this one is a COMPARISON value that
// never goes anywhere.
//
// The gap that opens is the interesting one, and it is on the REJECT path.
// verify.go:199-206 returns before AdvanceCounter whenever the MAC did not verify,
// so a refused tap leaves the counter unmoved — and the mac still sitting in this
// frame is the CORRECT SDM MAC for a (uid, ctr) pair the sender demonstrably did
// NOT have, at the exact moment key, sessionKey and full have all been zeroed.
// That is T64's own item (a), one layer further down: the last unwiped copy of a
// value the rest of the chain took care to destroy.
func verifyMAC(kSDMFileRead, uid, ctrBytes, chipCMAC []byte) (bool, error) {
	// Step 1+2: derive the per-tap session key from SV2.
	sessionKey, err := cmac(kSDMFileRead, sv2(uid, ctrBytes))
	defer Zero(sessionKey[:])
	if err != nil {
		return false, fmt.Errorf("sun: derive session key: %w", err)
	}

	// Step 3: the SDM MAC is computed over an EMPTY message. ADR 0003 madde 2:
	// with UID+counter mirroring and no SDMMACInputOffset, sdmMacInput is a
	// zero-length slice. Adding UID/ctr here (a tempting "fix") makes every
	// verification fail — the chip MACs the empty input. sessionKey[:] is a
	// valid 16-byte AES key, so this cmac cannot fail on key length.
	full, err := cmac(sessionKey[:], nil)
	defer Zero(full[:])
	if err != nil {
		return false, fmt.Errorf("sun: compute sdm mac: %w", err)
	}

	// Step 4: truncate to the 8 odd-indexed bytes the chip actually emits. The
	// deferred wipe is safe because the ONLY read of mac is inside the return
	// expression below, and return values are evaluated before any defer runs.
	mac := truncateSDMMAC(full)
	defer Zero(mac[:])

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
//	SV2 = 3C C3 00 01 00 80 || UID(7 bytes) || SDMReadCtr(3 bytes, LSB FIRST)
//
// 🔴 THE COUNTER IS REVERSED ON THE WAY IN, AND THAT IS THE WHOLE POINT OF THIS
// FUNCTION. ctrBytes is the counter as it appears in the tap URL, which ADR 0003
// §1 fixes as BIG-ENDIAN text (`ctr=000001` means one). SV2 wants the SAME value
// spelled LSB-FIRST. The two spellings are byte-reverses of each other, so the
// URL bytes must be reversed before they enter SV2.
//
// AN12196 says both halves of that in one place — rev. 1.8 §4.3 Table 2, page 10
// (rev. 2.0 §3.3 Table 1, page 9 — the table is RENUMBERED across revisions, not
// merely moved, so a citation without its revision points at a different table),
// for UID 04C767F2066180:
//
//	step 4: SDMReadCtr = 010000   "(LSB first as per [Section 3.1])"
//	step 7: SV2        = 3CC3 0001 0080 04C767F2066180 010000
//
// and §4.4.1's plain-URL example for that same UID (rev. 1.8 page 11) publishes
// the counter as `ctr=000001` — MSB-first. Same read, two byte orders: the URL
// text is big-endian, the SV2 input is little-endian.
//
// WHY THE PREVIOUS VERSION WAS WRONG, in the exact words it used. It appended
// ctrBytes VERBATIM and justified that with a STRUCTURAL claim: "the NTAG 424
// mirrors SDMReadCtr into the URL with the SAME byte order it feeds into SV2 — so
// the URL bytes ARE the SV2 bytes." That sentence is false, and the document above
// is the refutation: 010000 into SV2, 000001 into the URL. Feeding the URL bytes
// verbatim reproduces the published session key for NO counter except a
// palindrome.
//
// HOW BADLY, MEASURED. A 3-byte counter is a palindrome in 65 536 of 16 777 216
// cases (1/256), and NONE of the values 1..255 qualify — so a real chip's FIRST
// 255 TAPS would every one of them have been rejected. Nothing caught it because
// every test vector in this package was minted by this same sv2(), so the bug
// signed its own homework (test/fixtures/sun_vectors.json _warning). The external
// known-answer vectors in an12196_kat_test.go — a SEPARATE file, not the one this
// package's other MAC tests live in — are the fix for THAT, not just for this
// line: they come from AN12196's published numbers and cannot move with us.
//
// The value axis that used to be flagged here is now CLOSED, not merely untested.
// Reading the URL's counter big-endian (params.go beUint24) is correct, because
// the URL text is MSB-first — the same §4.4.1-vs-Table-2 pair above establishes
// it, and icedevml/sdm-backend's validate_plain_sun independently agrees: it
// reverses the URL bytes for SV2 and reads their VALUE with '>I' (big-endian).
// The two axes are therefore no longer independent — one byte order, read one way
// for the value and reversed once for the crypto.
func sv2(uid, ctrBytes []byte) []byte {
	buf := make([]byte, 0, len(sdmSV2Prefix)+len(uid)+len(ctrBytes))
	buf = append(buf, sdmSV2Prefix[:]...)
	buf = append(buf, uid...)
	// Reverse the URL's big-endian counter into SV2's LSB-first order. Written as
	// an explicit descending copy rather than an in-place swap because ctrBytes is
	// the caller's slice (Params.CtrBytes) and must not be mutated.
	for i := len(ctrBytes) - 1; i >= 0; i-- {
		buf = append(buf, ctrBytes[i])
	}
	return buf
}

// truncateSDMMAC extracts the 8-byte SDM MAC the NTAG 424 DNA emits: the
// ODD-indexed bytes of the full 16-byte CMAC — full[1], full[3], … full[15]
// (AN12196 §SDM, ADR 0003 madde 6). This is the single most error-prone step of
// SDM: the chip does not emit the full CMAC, so comparing against the full 16
// bytes (or the first 8) fails EVERY time and misleadingly looks like a wrong
// key (skill tappa-sun, m2-sun.md M2-04 Tuzaklar). full[2*i+1] for i in 0..7
// yields exactly indices 1,3,5,7,9,11,13,15.
//
// full arrives BY VALUE, so this frame holds its own copy of the untruncated CMAC
// — including the eight EVEN-indexed bytes the chip never transmits — and wipes it
// on return. mac is wiped too: it is a CMAC under a session key, and the result of
// this function is UNNAMED, so `return mac` has already copied the eight bytes out
// by the time the deferred wipe runs. Naming the result would make that defer zero
// what the caller gets — every known-answer vector in the package fires on it.
func truncateSDMMAC(full [16]byte) [8]byte {
	defer Zero(full[:])
	var mac [8]byte
	defer Zero(mac[:])
	for i := 0; i < 8; i++ {
		mac[i] = full[2*i+1]
	}
	return mac
}
