package sun

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
)

// EV2 SECURE MESSAGING — THE PERSONALISATION HALF OF internal/sun.
//
// Everything else in this package VERIFIES a tap: a chip signed something and we
// check it. This file does the opposite job. It speaks the NTAG 424 DNA's own
// command protocol so Tappa can WRITE a blank chip: run the crypto steps of an
// AuthenticateEV2First handshake, derive the two session keys, seal a
// CommMode.Full command body and verify the chip's response MAC. ChangeKey — the
// one command whose body has its own layout — lives in changekey.go.
//
// ADR 0017 §2.1 fixes why this belongs on the server at all: the phone is a dumb
// APDU relay that "hiçbir baytı üretmez, hiçbirini yorumlamaz", so every byte of
// crypto is produced here. CLAUDE.md §3 puts cryptography ONLY in internal/sun.
//
// 🔴 WHAT THIS FILE IS NOT. There is no HTTP, no DB, no APDU transport, no relay,
// no context, no goroutine and no stateful session machine here — those are ADR
// 0017's turn 2c. Every function takes bytes and returns bytes. EV2Auth is a
// RESULT VALUE, not a session: it holds no command counter, so nothing here can
// silently mis-count. CmdCtr is an explicit argument at every call site, exactly
// so the caller has to say which value it means (ADR 0017 §6 md. 7 — the session
// TTL, the sweeper and the zeroisation rule are still open and are turn 2c's).
//
// 🔴 SECRETS (CLAUDE.md §4.7). RndA, RndB, the session keys and every plaintext
// key buffer are key material. Nothing here logs, formats or returns them by
// accident: the errors below carry LENGTHS and fixed strings only, never bytes —
// the same shape keys.go already uses. Intermediate buffers that held key material
// are zeroised before return, and the list is written out rather than implied so
// the claim can be checked: the two session vectors and the derivation's own CMAC
// outputs (ev2SessionKeys), the rotated RndB' AND the RndA || RndB' it is copied
// into (EV2AuthPart1), the decrypted authentication response and the rotated echo
// (EV2AuthPart2), the data IV and the padded plaintext body (EV2WrapCommandFull),
// the ChangeKey body (EV2ChangeKeyCommand), and — added 2026-08-25 — the two
// untruncated CMACs, the expected response tag and truncateMACt's own copy.
// Every one of them is held by byteBufferInventory in cmac_wipe_test.go, which
// records for EACH declared identifier in this file whether it is wiped or exempt
// and why — so a deleted wipe, a wipe moved to the wrong variable, and a new
// buffer nobody classified are all red tests rather than stale sentences.
//
// ⚠️ THIS POINTER USED TO NAME TestEV2_ZeroDisciplineIsInventoried, WHICH NO LONGER
// WATCHES THIS FILE. ev2.go left that counting gate on 2026-08-25 for the stronger
// per-identifier one, because a count knows how many wipes exist and never which
// buffer — and two audit mutations exploited exactly that. A signpost pointing at
// a gate that has stopped covering you is the failure this very sentence used to
// warn about, so it is spelled out instead of quietly repaired.
//
// 🔴 THE SENTENCE THAT USED TO STAND HERE WAS "Ciphertext and MAC tags are NOT
// wiped — they are not key material, and they are what goes on the wire anyway",
// AND IT WAS DOING REAL DAMAGE: it defended three unwiped CMACs in this file for
// a month. Measured against each value it claimed to cover, rather than restated:
//
//	full (EV2WrapCommandFull, EV2UnwrapResponseFull) — SIXTEEN bytes of CMAC under
//	  KSesAuthMAC. Exactly EIGHT of them become the tag. The other eight are never
//	  transmitted in any frame, so "goes on the wire anyway" was false for half of
//	  the buffer it was excusing. WIPED.
//	want (EV2UnwrapResponseFull) — the expected tag. It is never transmitted at
//	  all: it is what the received tag is COMPARED against, and on the failing
//	  branch it is the correct MAC for a frame the relay could not produce. WIPED.
//	full (truncateMACt) — a by-value copy of the same sixteen bytes. WIPED.
//	t (EV2WrapCommandFull) — the eight bytes that DO travel. They are appended into
//	  the returned frame verbatim, so this copy is genuinely redundant with a value
//	  the caller is about to hand to a hostile relay. NOT wiped, and that is the
//	  only form of the old sentence that survives.
//	enc / out / the returned plaintext — ciphertext and caller-owned results. NOT
//	  wiped; ev2Unpad's error path already Zeroes the plaintext it is abandoning.
//
// The rule, stated so it cannot be stretched by prose again: a value is exempt
// because it IS, byte for byte, something this process is about to transmit — not
// because it is "a MAC", and not because part of the buffer it lives in travels.
// A comparison value is never exempt. verify_mac.go carries the same distinction
// for the SDM half, and the two were fixed in the same round for the same reason.
//
// ⚠️ ONE MECHANISM THIS RELIES ON, STATED SO IT IS NOT ASSUMED — AND STATED AT THE
// STRENGTH IT WAS MEASURED AT, WHICH IS NOT "EXACT". Every buffer above is
// allocated with a capacity that is ALREADY AT LEAST ITS FINAL LENGTH, so no
// append in this file ever reallocates and no half-built copy is orphaned in an
// old backing array where Zero cannot reach it.
//
// ⚠️ THIS IS STILL A HAND ARGUMENT, AND THE MECHANICAL RULE DOES NOT REPLACE IT.
// cmac_wipe_test.go's reassignedAfterItsWipe checks the no-reallocation property
// only for identifiers the inventory marks WIPED, within one function — which
// covers iv, padded, plain, rot, msg, echoed, sv1 and sv2v. It does NOT look at
// the buffers this paragraph is mostly about: ev2Pad's out, ev2MACInput's buf,
// ev2SessionVector's sv and the rotations' out are all classified exReturned, and
// no mechanism examines their capacities at all. Counted across ev2.go and
// changekey.go: TEN make([]byte, 0, …) sites, NINE of which allocate the exact
// final length; ev2Pad is the single exception and allocates an upper bound
// (len(in)+16, so a 17-byte body yields len 32 in a cap-33 array). That is
// deliberately fine — the
// unwritten tail bytes come from make and are therefore already zero, and Zero
// covers the whole length that was written. It is the NO-REALLOCATION half that
// is load-bearing, not exactness. Replacing one of those capacities with a guess
// SMALLER than the final length reintroduces exactly the leak F2 found, silently.
//
// SOURCES. Two documents, and the revision is written out at every citation
// because AN12196 renumbered its chapters (rev. 1.8 §6 "Personalization example"
// is rev. 2.0 §5; rev. 2.0's §6 is "Special functionalities" — a citation without
// its revision points at a DIFFERENT chapter). The mapping table lives in
// an12196_kat_test.go and is known-incomplete by its own heading.
//
//	NT4H2421Gx, "NTAG 424 DNA — Secure NFC T4T compliant IC"
//	  rev. 3.0, 31 January 2019 — ONE revision, no ambiguity. This is the
//	  NORMATIVE description of the protocol: §9.1.1 (TI), §9.1.2 (CmdCtr),
//	  §9.1.3 (MAC + truncation), §9.1.4 (CBC, padding, IV), §9.1.5
//	  (AuthenticateEV2First), §9.1.7 (session keys), §9.1.9 (MAC mode field
//	  order), §9.1.10 (CommMode.Full).
//	AN12196, "NTAG 424 DNA and NTAG 424 DNA TagTamper features and hints"
//	  rev. 1.8, 17 Nov 2020 / rev. 2.0, 4 Mar 2025 — WORKED EXAMPLES with every
//	  intermediate value. These are the known-answer vectors in ev2_kat_test.go.
//
// 🔴 M2-08's RULE, APPLIED. A byte order is never DERIVED from structure, only
// READ from a document. Every offset, endianness and truncation below carries the
// sentence it came from. Where a value could not be read out of a document it is
// marked DERIVED, NOT TRANSCRIBED and says how it was derived — the precedent is
// an12196_kat_test.go's katT5URLCtr.

const (
	// ev2RandLen is the size of RndA and RndB. Datasheet rev. 3.0 §9.1.5: "The
	// PICC generates a random 16-byte challenge RndB".
	ev2RandLen = 16

	// ev2TILen is the Transaction Identifier size. Datasheet rev. 3.0 §9.1.1:
	// "The size is 4 bytes and these 4 bytes can hold any value. The TI is
	// treated as a byte array, so there is no notion of MSB and LSB." That last
	// clause is why no code below ever byte-swaps TI.
	ev2TILen = 4

	// ev2CapLen is the size of PDcap2 and of PCDcap2. Datasheet rev. 3.0 §9.1.5:
	// "the PICC sends 12 bytes of capabilities to the PCD: 6 bytes of PICC
	// capabilities PDcap2 and 6 bytes of PCD capabilities PCDcap2".
	ev2CapLen = 6

	// ev2MACtLen is the transmitted MAC size. Datasheet rev. 3.0 §9.1.3: the
	// 16-byte CMAC "is truncated by using only the 8 even-numbered bytes out of
	// the 16 bytes output".
	ev2MACtLen = 8

	// ev2KeyLen is the AES-128 application/session key size.
	ev2KeyLen = 16

	// ev2AuthPart2RespLen is the plaintext length of the Part2 response:
	// TI(4) || RndA'(16) || PDcap2(6) || PCDcap2(6). Two AES blocks exactly,
	// which is why the authentication needs no padding (datasheet rev. 3.0
	// §9.1.4: "The only exception is during the authentication itself … where no
	// padding is applied at all"). Transcribed check — AN12196 rev. 2.0 §5.6
	// Table 14 step 18 (rev. 1.8 §6.6 Table 14) prints exactly 32 plaintext
	// bytes and splits them 4 / 16 / 6 / 6 in steps 19-22.
	ev2AuthPart2RespLen = ev2TILen + ev2RandLen + 2*ev2CapLen

	// ev2PadByte is ISO/IEC 9797-1 padding method 2's marker. Datasheet rev. 3.0
	// §9.1.4: "by adding always 80h followed, if required, by zero bytes until a
	// string with a length of a multiple of 16 byte is obtained. Note that if the
	// plain data is a multiple of 16 bytes already, an additional padding block
	// is added."
	ev2PadByte = 0x80
)

// The 2-byte labels. They are separate constants even though two pairs share
// their bytes, because they come from two different sentences and could in
// principle diverge; folding them would hide which document line each obeys.
var (
	// ev2LabelCmdIV / ev2LabelRespIV — datasheet rev. 3.0 §9.1.4: "a 2-byte
	// label, distinguishing the purpose of the IV: A55Ah for commands and 5AA5h
	// for responses".
	ev2LabelCmdIV  = [2]byte{0xA5, 0x5A}
	ev2LabelRespIV = [2]byte{0x5A, 0xA5}

	// ev2LabelSV1 / ev2LabelSV2 — datasheet rev. 3.0 §9.1.7 spells the session
	// vectors out byte by byte: "SV1 = A5h||5Ah||00h||01h||00h||80h||…" and
	// "SV2 = 5Ah||A5h||00h||01h||00h||80h||…".
	ev2LabelSV1 = [2]byte{0xA5, 0x5A}
	ev2LabelSV2 = [2]byte{0x5A, 0xA5}

	// ev2SVCounterAndLength is the fixed middle of both session vectors:
	// 00h||01h (the SP 800-108 counter, "fixed to 0001h as only 128-bit keys are
	// generated") followed by 00h||80h (the length, "fixed to 0080h"). Datasheet
	// rev. 3.0 §9.1.7.
	ev2SVCounterAndLength = [4]byte{0x00, 0x01, 0x00, 0x80}
)

// EV2Auth is the outcome of a completed AuthenticateEV2First handshake: the
// transaction identifier the chip minted and the two session keys both sides
// derived. It is a RESULT VALUE and deliberately not a session object — it has no
// command counter, no clock and no lifecycle, so nothing in this package can
// mis-count commands on a caller's behalf (ADR 0017 §4: the session lives in turn
// 2c's memory, and its TTL/zeroisation rule is still open, §6 md. 7).
//
// 🔴 KeyENC and KeyMAC ARE KEY MATERIAL (CLAUDE.md §4.7). On a blank chip they
// derive from the PUBLIC factory key, so they protect nothing there (ADR 0017
// §2.2 — accepted risk 7); on a personalised chip they are as sensitive as the
// plaque key itself. Never log, format or persist them. Call Zero on every exit
// path — success, error, timeout, cancellation, shutdown (ADR 0017 §3).
type EV2Auth struct {
	// TI is the 4-byte Transaction Identifier generated by the PICC
	// (datasheet rev. 3.0 §9.1.1). Not secret; it is a replay/interleaving guard.
	TI []byte
	// KeyENC is KSesAuthENC — encryption and decryption of message data.
	KeyENC []byte
	// KeyMAC is KSesAuthMAC — MACing of commands and responses.
	KeyMAC []byte
	// PDcap2 and PCDcap2 are the 12 capability bytes the PICC returns
	// (datasheet rev. 3.0 §9.1.5). They are surfaced rather than dropped because
	// the datasheet makes acting on a mismatch "the responsibility of the PCD …
	// outside the scope of this specification" — this package therefore reports
	// them and judges nothing.
	PDcap2  []byte
	PCDcap2 []byte
}

// Zero wipes both session keys (CLAUDE.md §4.7). TI and the capability bytes are
// not secret and are left alone.
func (a *EV2Auth) Zero() {
	if a == nil {
		return
	}
	Zero(a.KeyENC)
	Zero(a.KeyMAC)
}

// EV2AuthPart1 runs the PCD side of AuthenticateEV2First - Part1: it opens the
// chip's encrypted challenge, builds RndA || RndB' and encrypts it for the Part2
// command. It returns that 32-byte cryptogram AND the recovered RndB, which the
// caller must hold until EV2AuthPart2 because the session keys need both randoms.
//
// key is the 16-byte AES-128 application key being authenticated with (on a blank
// chip, the all-zero factory default — datasheet rev. 3.0 §8.2.4.2). encRndB is
// the 16 bytes E(K, RndB) the chip returned. rndA MUST be 16 FRESH crypto/rand
// bytes generated by the caller (ADR 0017 §2.1); this function never invents
// randomness, so a caller that reuses one is reusing it visibly.
//
// TRANSCRIBED STEPS — AN12196 rev. 2.0 §5.6 Table 14 (rev. 1.8 §6.6 Table 14):
//
//	step  9  D(K0, RndB)                = B9E2FC789B64BF237CCCAA20EC7E6E48
//	step 11  RndB' (rotate left 1 byte) = E2FC789B64BF237CCCAA20EC7E6E48B9
//	step 12  RndA || RndB'
//	step 13  E(K0, RndA || RndB')
//
// "rotate left by one byte" is the document's own wording (step 11 and datasheet
// rev. 3.0 §9.1.5, "rotating it left by one byte"), and the published pair above
// shows which end moves: the FIRST byte B9 leaves the front and reappears at the
// back. It is read, not derived.
//
// The cipher is AES-128 in CBC mode with an ALL-ZERO IV: datasheet rev. 3.0
// §9.1.4, "For the encryption during authentication (both AuthenticateEV2First
// and AuthenticateEV2NonFirst), the IV will be 128 bits of 0", and no padding is
// applied during authentication (same section).
//
// 🔴 The returned rndB is KEY MATERIAL (§4.7): it is half of the session-key
// input. The caller owns its lifetime and must Zero it.
func EV2AuthPart1(key, encRndB, rndA []byte) (part2Data, rndB []byte, err error) {
	if len(key) != ev2KeyLen {
		return nil, nil, fmt.Errorf("sun: ev2: application key must be %d bytes, got %d", ev2KeyLen, len(key))
	}
	if len(encRndB) != ev2RandLen {
		return nil, nil, fmt.Errorf("sun: ev2: encrypted challenge must be %d bytes, got %d", ev2RandLen, len(encRndB))
	}
	if len(rndA) != ev2RandLen {
		return nil, nil, fmt.Errorf("sun: ev2: PCD challenge must be %d bytes, got %d", ev2RandLen, len(rndA))
	}

	rndB, err = ev2CBCDecrypt(key, ev2ZeroIV(), encRndB)
	if err != nil {
		return nil, nil, err
	}

	// Step 11-12. rndB is NOT mutated in place: it is returned to the caller for
	// the session-key derivation and must stay in its original rotation.
	//
	// 🔴 THE ROTATION'S OWN BUFFER IS BOUND TO A NAME ON PURPOSE. ev2RotateLeft1
	// ALLOCATES a fresh 16 bytes and fills them with the whole of RndB; appending
	// its result inline would copy those bytes into msg and drop the original
	// unreferenced and UNWIPED, leaving a second copy of half the session-key
	// input in the heap on every handshake. It survived the first review because
	// its mirror image, ev2RotateRight1 in EV2AuthPart2, happens to be assigned to
	// a local and therefore happens to get a defer — an accident of spelling, not
	// a reasoned difference. Anything that RETURNS key material gets a name here.
	rot := ev2RotateLeft1(rndB)
	defer Zero(rot)

	msg := make([]byte, 0, 2*ev2RandLen)
	msg = append(msg, rndA...)
	msg = append(msg, rot...)
	defer Zero(msg) // holds both randoms — wiped before return (§4.7)

	part2Data, err = ev2CBCEncrypt(key, ev2ZeroIV(), msg)
	if err != nil {
		Zero(rndB)
		return nil, nil, err
	}
	return part2Data, rndB, nil
}

// EV2AuthPart2 finishes AuthenticateEV2First: it decrypts the chip's Part2
// response, VERIFIES that the chip echoed our own RndA, and derives the two
// session keys.
//
// 🔴 THE RndA CHECK IS THE AUTHENTICATION AND IS NOT SKIPPABLE. Up to this point
// nothing has proven the chip holds the key; the only evidence is that it could
// rotate and re-encrypt the RndA we sent. A mismatch is returned as an error and
// no session keys are produced. The comparison is subtle.ConstantTimeCompare —
// bytes.Equal is forbidden on this path (redline R7).
//
// encResp is the 32-byte E(Kx, TI || RndA' || PDcap2 || PCDcap2) from the chip,
// WITHOUT the 9100 status word. rndA is the value passed to EV2AuthPart1 and rndB
// is the value it returned.
//
// TRANSCRIBED STEPS — AN12196 rev. 2.0 §5.6 Table 14 (rev. 1.8 §6.6 Table 14):
//
//	step 18  D(K0, TI || RndA' || PDcap2 || PCDcap2)
//	         = 9D00C4DF C5DB8A5930439FC3DEF9A4C675360F13 000000000000 000000000000
//	step 19  TI      (4 byte)  = 9D00C4DF
//	step 20  RndA'   (16 byte) = C5DB8A5930439FC3DEF9A4C675360F13
//	step 21  PDcap2  (6 byte)  = 000000000000
//	step 22  PCDcap2 (6 byte)  = 000000000000
//	step 23  RndA (rotate right for 1 byte) = 13C5DB8A5930439FC3DEF9A4C675360F
//	step 24  PCD compares sent RndA and received RndA
//
// The 4/16/6/6 split and the ROTATE-RIGHT direction are both read off those
// steps, not inferred: step 20's value ends 0F13 and step 23's begins 13, so the
// last byte moves to the front.
//
// 🔴 The returned EV2Auth carries KEY MATERIAL; Zero it on every exit path (§4.7).
func EV2AuthPart2(key, rndA, rndB, encResp []byte) (*EV2Auth, error) {
	if len(key) != ev2KeyLen {
		return nil, fmt.Errorf("sun: ev2: application key must be %d bytes, got %d", ev2KeyLen, len(key))
	}
	if len(rndA) != ev2RandLen || len(rndB) != ev2RandLen {
		return nil, fmt.Errorf("sun: ev2: both challenges must be %d bytes, got %d and %d", ev2RandLen, len(rndA), len(rndB))
	}
	if len(encResp) != ev2AuthPart2RespLen {
		return nil, fmt.Errorf("sun: ev2: authentication response must be %d bytes, got %d", ev2AuthPart2RespLen, len(encResp))
	}

	plain, err := ev2CBCDecrypt(key, ev2ZeroIV(), encResp)
	if err != nil {
		return nil, err
	}
	defer Zero(plain) // contains RndA' — wiped before return (§4.7)

	ti := plain[:ev2TILen]
	rndAPrime := plain[ev2TILen : ev2TILen+ev2RandLen]
	pdCap := plain[ev2TILen+ev2RandLen : ev2TILen+ev2RandLen+ev2CapLen]
	pcdCap := plain[ev2TILen+ev2RandLen+ev2CapLen:]

	echoed := ev2RotateRight1(rndAPrime)
	defer Zero(echoed)
	if subtle.ConstantTimeCompare(echoed, rndA) != 1 {
		// The message names no bytes and does not say WHERE the mismatch was
		// (§4.7). A caller learns only "the chip did not prove it holds this key".
		return nil, fmt.Errorf("sun: ev2: chip did not echo the PCD challenge; authentication failed")
	}

	keyENC, keyMAC, err := ev2SessionKeys(key, rndA, rndB)
	if err != nil {
		return nil, err
	}

	// The slices are copied out of `plain`, which this function wipes on return.
	return &EV2Auth{
		TI:      append([]byte(nil), ti...),
		KeyENC:  keyENC,
		KeyMAC:  keyMAC,
		PDcap2:  append([]byte(nil), pdCap...),
		PCDcap2: append([]byte(nil), pcdCap...),
	}, nil
}

// ev2SessionKeys derives (KSesAuthENC, KSesAuthMAC) from the authentication key
// and the two randoms — datasheet rev. 3.0 §9.1.7:
//
//	SesAuthENCKey = PRF(Kx, SV1)
//	SesAuthMACKey = PRF(Kx, SV2)
//
// with PRF being "the CMAC algorithm described in NIST Special Publication
// 800-38B", i.e. this package's cmac().
//
// It is shared with AuthenticateEV2NonFirst, whose §9.1.6 says the behaviour is
// "exactly the same as for AuthenticateEV2First" apart from TI/CmdCtr/capability
// handling — which is why ev2_kat_test.go can pin this function with AN12196's
// NonFirst vector as well as its two First vectors.
func ev2SessionKeys(key, rndA, rndB []byte) (keyENC, keyMAC []byte, err error) {
	sv1 := ev2SessionVector(ev2LabelSV1, rndA, rndB)
	defer Zero(sv1)
	sv2v := ev2SessionVector(ev2LabelSV2, rndA, rndB)
	defer Zero(sv2v)

	// Both CMAC results are session keys, so both locals are wiped on EVERY path,
	// not only the error one. The deferred Zero runs after the return values are
	// evaluated, so the copies handed to the caller are already made — this wipes
	// the derivation's own scratch, which is what the file header promises.
	enc, err := cmac(key, sv1)
	defer Zero(enc[:])
	if err != nil {
		return nil, nil, fmt.Errorf("sun: ev2: derive encryption session key: %w", err)
	}
	mac, err := cmac(key, sv2v)
	defer Zero(mac[:])
	if err != nil {
		return nil, nil, fmt.Errorf("sun: ev2: derive MAC session key: %w", err)
	}
	return append([]byte(nil), enc[:]...), append([]byte(nil), mac[:]...), nil
}

// ev2SessionVector builds the 32-byte SP 800-108 input SV1 or SV2. Datasheet
// rev. 3.0 §9.1.7, transcribed:
//
//	SV1 = A5h||5Ah||00h||01h||00h||80h||RndA[15..14]||
//	      ( RndA[13..8] # RndB[15..10])||RndB[9..0]||RndA[7..0]
//	SV2 = 5Ah||A5h||00h||01h||00h||80h||   (same context)
//	with # being the XOR-operator.
//
// 🔴 THE INDEX CONVENTION IS THE WHOLE RISK HERE, SO IT IS SPELLED OUT. The
// document numbers bytes MOST-SIGNIFICANT FIRST: RndA[15] is the LEFTMOST byte of
// the 16-byte string as printed. So, writing r for the slice as it appears in
// memory, RndA[15..14] is r[0:2], RndA[13..8] is r[2:8], RndB[15..10] is r[0:6],
// RndB[9..0] is r[6:16] and RndA[7..0] is r[8:16]. That reading is not an
// inference: AN12196 rev. 2.0 §5.6 Table 14 step 25 publishes the assembled SV1
// for known randoms and it matches this and no other reading —
//
//	RndA = 13C5DB8A5930439FC3DEF9A4C675360F
//	RndB = B9E2FC789B64BF237CCCAA20EC7E6E48
//	SV1  = A55A00010080 13C5 6268A548D8FB BF237CCCAA20EC7E6E48 C3DEF9A4C675360F
//	                     ^^^^ RndA[15..14] = first two bytes
//	                          ^^^^^^^^^^^^ DB8A59 30439F XOR B9E2FC 789B64
//
// TestEV2KAT_SessionVectorsMatchAN12196Table14 asserts exactly that, and
// TestEV2KAT_SessionVectorIndexingIsDiscriminated shows the vector rejects the
// three plausible mis-readings.
//
// Lengths are validated by the callers (ev2SessionKeys' callers check them); this
// helper is unexported and always receives 16-byte randoms.
func ev2SessionVector(label [2]byte, rndA, rndB []byte) []byte {
	sv := make([]byte, 0, 32)
	sv = append(sv, label[0], label[1])
	sv = append(sv, ev2SVCounterAndLength[:]...)
	sv = append(sv, rndA[0:2]...) // RndA[15..14]
	for i := 0; i < 6; i++ {
		sv = append(sv, rndA[2+i]^rndB[i]) // RndA[13..8] XOR RndB[15..10]
	}
	sv = append(sv, rndB[6:16]...) // RndB[9..0]
	sv = append(sv, rndA[8:16]...) // RndA[7..0]
	return sv
}

// EV2WrapCommandFull seals one CommMode.Full command and returns the ISO 7816
// data field to put in the C-APDU — that is:
//
//	CmdHeader || E(KSesAuthENC, CmdData || padding) || MACt
//
// The caller supplies the command byte (INS), the header (the command's own
// parameters — for ChangeKey that is the key number; for ChangeFileSettings the
// file number) and the plaintext body. cmdCtr is the counter value for THIS
// command: datasheet rev. 3.0 §9.1.2, "The CmdCtr is reset to 0000h … after a
// successful AuthenticateEV2First" and "When a MAC on a command is calculated at
// PCD side that includes the CmdCtr, it uses the current CmdCtr. The CmdCtr is
// afterwards incremented by 1."
//
// TRANSCRIBED LAYOUT — datasheet rev. 3.0 §9.1.9 lists the MAC input fields "in
// this order": Cmd, CmdCtr, TI, CmdHeader (if present), CmdData (if present); and
// §9.1.10 says CommMode.Full is "exactly done as specified for MAC in Section
// 9.1.9, replacing the plain CmdData or RespData by the encrypted field". Four
// independent worked examples in AN12196 rev. 2.0 confirm the assembled bytes:
// §5.8.2 Table 17 (WriteData), §5.9 Table 18 (ChangeFileSettings), §5.16.1
// Table 25 and §5.16.2 Table 26 (ChangeKey) — rev. 1.8 §6.8.2 Table 18, §6.9
// Table 19, §6.16.1 Table 26, §6.16.2 Table 27.
//
// ⚠️ THE WRAPPER IS INDEPENDENT OF WHAT IT WRAPS, AND THE VECTORS ARE NOT. Table
// 18's CmdData is an ENCRYPTED-PICC SDM configuration ("FileAR.SDMMetaRead: 0x2",
// ENCPICCDataOffset); Tappa's plain SDM needs SDMMetaRead = Eh, and then the body
// carries UIDOffset and SDMReadCtrOffset in place of PICCDataOffset. That
// difference lives entirely in the CmdData bytes, which this function treats as
// opaque — so the vector pins the SEALING and says nothing about Tappa's file
// settings. That body is now built by filesettings.go (turn 2b, M8-05 FAZ B2b):
// its field order and field-presence conditions are read from NT4H2421Gx rev. 3.0
// §10.7.1 Table 69 ("Mirror position (LSB first)"), NOT from AN12196, which never
// shows a plain configuration. The adjacent open decision ADR 0017 §6 enumerated,
// md. 13 — the FileAR.Change and FileAR.ReadWrite cells the same command writes —
// was decided in that round and is recorded at TappaNDEFSettings.
//
// A command with NO body is legal: §9.1.9's "CmdData (if present)" and §9.1.10's
// bracketed "[|| E(Ke, CmdData)]" in Figure 9. In that case nothing is encrypted
// and the field is CmdHeader || MACt.
func EV2WrapCommandFull(auth *EV2Auth, cmd byte, cmdCtr uint16, cmdHeader, cmdData []byte) ([]byte, error) {
	if err := ev2CheckAuth(auth); err != nil {
		return nil, err
	}

	var enc []byte
	if len(cmdData) > 0 {
		iv, err := ev2DataIV(auth.KeyENC, ev2LabelCmdIV, auth.TI, cmdCtr)
		if err != nil {
			return nil, err
		}
		defer Zero(iv)
		padded := ev2Pad(cmdData)
		defer Zero(padded) // a ChangeKey body is key material (§4.7)
		enc, err = ev2CBCEncrypt(auth.KeyENC, iv, padded)
		if err != nil {
			return nil, err
		}
	}

	macIn := ev2MACInput(cmd, cmdCtr, auth.TI, cmdHeader, enc)
	// full is the UNTRUNCATED CMAC under KSesAuthMAC. Only its eight odd-indexed
	// bytes become t and travel; the other eight never leave this process, so the
	// "it goes on the wire anyway" argument does not cover them (see the file
	// header). t itself is NOT wiped — it is copied verbatim into out below and
	// returned, so scrubbing this copy protects nothing.
	full, err := cmac(auth.KeyMAC, macIn)
	defer Zero(full[:])
	if err != nil {
		return nil, fmt.Errorf("sun: ev2: compute command MAC: %w", err)
	}
	t := truncateMACt(full)

	out := make([]byte, 0, len(cmdHeader)+len(enc)+ev2MACtLen)
	out = append(out, cmdHeader...)
	out = append(out, enc...)
	out = append(out, t[:]...)
	return out, nil
}

// EV2UnwrapResponseFull verifies a CommMode.Full response and returns its
// plaintext data, or nil when the command has none.
//
// respField is the R-APDU payload WITHOUT the two status bytes, i.e.
// E(KSesAuthENC, RespData) || MACt. rc is SW2 — the low status byte, 0x00 for a
// 9100 success. cmdCtr is the counter value used for the COMMAND; this function
// applies the +1 itself, because datasheet rev. 3.0 §9.1.9 says the response MAC
// uses "Command Counter - CmdCtr (The already increased value)" and §9.1.2 fixes
// the increment as happening "between the command and response". Making the
// caller pre-increment would move that rule out of the file that cites it.
//
// TRANSCRIBED — AN12196 rev. 2.0 §5.9 Table 18 step 19 (rev. 1.8 §6.9 Table 19)
// prints the assembled response MAC input for a command sent with CmdCtr = 0100:
//
//	Status || CmdCounter + 1 || TI || (E(KSesAuthENC, ResponseData)) = 0002009D00C4DF
//	         ^^ 00 = SW2      ^^^^ 0200 = 2, LSB first   ^^^^^^^^ TI, no resp data
//
// 🔴 ONLY SUCCESSFUL RESPONSES BELONG HERE. Datasheet rev. 3.0 §9.1.10: on any
// error "the authentication state is immediately lost and the error return code
// is sent without a MAC appended". A caller must inspect SW1SW2 first and abandon
// the session on anything but 9100 — an error frame has no MAC to verify and
// feeding one here is a length error, not a MAC failure.
//
// The MAC comparison is subtle.ConstantTimeCompare (redline R7).
func EV2UnwrapResponseFull(auth *EV2Auth, cmdCtr uint16, rc byte, respField []byte) ([]byte, error) {
	if err := ev2CheckAuth(auth); err != nil {
		return nil, err
	}
	if len(respField) < ev2MACtLen {
		return nil, fmt.Errorf("sun: ev2: response must carry at least the %d MAC bytes, got %d", ev2MACtLen, len(respField))
	}
	respCtr, err := ev2NextCtr(cmdCtr)
	if err != nil {
		return nil, err
	}

	enc := respField[:len(respField)-ev2MACtLen]
	got := respField[len(respField)-ev2MACtLen:]

	macIn := ev2MACInput(rc, respCtr, auth.TI, nil, enc)
	full, err := cmac(auth.KeyMAC, macIn)
	defer Zero(full[:])
	if err != nil {
		return nil, fmt.Errorf("sun: ev2: compute response MAC: %w", err)
	}
	// 🔴 want IS WIPED AND t IN EV2WrapCommandFull IS NOT, AND THE DIFFERENCE IS THE
	// WHOLE POINT. t is put into the returned frame and travels. want is a
	// COMPARISON value that never leaves this frame — and on the failing branch it
	// is the CORRECT MAC for a response the relay could not produce, computed under
	// a session key, at the moment we hand the relay an error. ADR 0017 §2.2 says
	// to assume that relay is hostile. Same shape as verify_mac.go's mac.
	want := truncateMACt(full)
	defer Zero(want[:])
	if subtle.ConstantTimeCompare(want[:], got) != 1 {
		// No bytes in the message (§4.7): a caller learns only that the frame
		// did not authenticate, never how close it was.
		return nil, fmt.Errorf("sun: ev2: response did not authenticate")
	}
	if len(enc) == 0 {
		return nil, nil
	}

	iv, err := ev2DataIV(auth.KeyENC, ev2LabelRespIV, auth.TI, respCtr)
	if err != nil {
		return nil, err
	}
	defer Zero(iv)
	plain, err := ev2CBCDecrypt(auth.KeyENC, iv, enc)
	if err != nil {
		return nil, err
	}
	out, err := ev2Unpad(plain)
	if err != nil {
		Zero(plain)
		return nil, err
	}
	return out, nil
}

// ev2MACInput assembles the CMAC input shared by commands and responses.
//
// For commands (datasheet rev. 3.0 §9.1.9, "in this order"): Cmd, CmdCtr, TI,
// CmdHeader (if present), CmdData (if present) — with §9.1.10 substituting the
// encrypted body for the plain one.
//
// For responses the same section gives: RC, CmdCtr (already increased), TI,
// RespData (if present) — i.e. the identical shape with an empty header, which is
// why one function serves both and the response path passes header = nil.
//
// 🔴 CmdCtr IS TWO BYTES, LSB FIRST, AND THAT IS TRANSCRIBED, NOT DERIVED.
// Datasheet rev. 3.0 §9.1.2: "CmdCtr which is a 16-bit unsigned integer … In
// cryptographic calculations, the CmdCtr is represented LSB first." AN12196
// rev. 2.0 shows the spelling with values in one continuous session: §5.8.2
// Table 17 step 5 CmdCtr = 0000, §5.9 Table 18 step 5 CmdCtr = 0100 (i.e. one),
// §5.16.1 Table 25 step 14 CmdCtr = 0200, §5.16.2 Table 26 step 9 CmdCtr = 0300.
// A big-endian spelling would print 0001/0002/0003.
func ev2MACInput(lead byte, ctr uint16, ti, header, body []byte) []byte {
	buf := make([]byte, 0, 1+2+len(ti)+len(header)+len(body))
	buf = append(buf, lead)
	buf = binary.LittleEndian.AppendUint16(buf, ctr)
	buf = append(buf, ti...)
	buf = append(buf, header...)
	buf = append(buf, body...)
	return buf
}

// ev2DataIV builds the CBC initialisation vector for message data. Datasheet
// rev. 3.0 §9.1.4, transcribed:
//
//	IV for CmdData  = E(SesAuthENCKey; A5h || 5Ah || TI || CmdCtr || 0000000000000000h)
//	IV for RespData = E(SesAuthENCKey; 5Ah || A5h || TI || CmdCtr || 0000000000000000h)
//
// The outer E is ECB — the same section: the IV "is constructed by encrypting
// with SesAuthENCKey according to the ECB mode of NIST SP800-38A the
// concatenation of …". For a single 16-byte block ECB is one raw block encrypt,
// which is what cipher.Block.Encrypt does.
//
// The input is 2 + 4 + 2 + 8 = 16 bytes, exactly one block. AN12196 rev. 2.0
// §5.9 Table 18 steps 8-9 print both the input and the result:
//
//	E(KSesAuthENC, A55A || 9D00C4DF || 0100 || 0000000000000000)
//	  = 3E27082AB2ACC1EF55C57547934E9962
func ev2DataIV(keyENC []byte, label [2]byte, ti []byte, ctr uint16) ([]byte, error) {
	if len(ti) != ev2TILen {
		return nil, fmt.Errorf("sun: ev2: transaction identifier must be %d bytes, got %d", ev2TILen, len(ti))
	}
	block, err := aes.NewCipher(keyENC)
	if err != nil {
		return nil, fmt.Errorf("sun: ev2: invalid session key length: %w", err)
	}
	in := make([]byte, 0, aes.BlockSize)
	in = append(in, label[0], label[1])
	in = append(in, ti...)
	in = binary.LittleEndian.AppendUint16(in, ctr)
	in = append(in, make([]byte, 8)...)

	out := make([]byte, aes.BlockSize)
	block.Encrypt(out, in)
	return out, nil
}

// ev2NextCtr returns the counter value the response MAC and response IV use.
// Datasheet rev. 3.0 §9.1.2 also fixes the boundary: "If the CmdCtr holds the
// value FFFFh and a command maintaining the active authentication arrives at the
// PICC, this leads to an error response and the command is handled like the MAC
// was wrong." So FFFFh has no successor and wrapping silently to 0000h would
// forge a counter the chip will never use.
func ev2NextCtr(cmdCtr uint16) (uint16, error) {
	if cmdCtr == 0xFFFF {
		return 0, fmt.Errorf("sun: ev2: command counter is exhausted; the session must be re-authenticated")
	}
	return cmdCtr + 1, nil
}

// truncateMACt is the EV2 secure-messaging MAC truncation: 16 CMAC bytes down to
// the 8 that travel. Datasheet rev. 3.0 §9.1.3: "The MAC used in NT4H2421Gx is
// truncated by using only the 8 even-numbered bytes out of the 16 bytes output …
// when represented in most-to-least-significant order."
//
// 🔴 IT IS THE SAME SELECTION AS SDM'S, AND THAT IS READ FROM THE DOCUMENT, NOT
// ASSUMED. Datasheet rev. 3.0 §9.3.8.1 defines the SDM MAC "applying the same
// truncation as the" secure-messaging MAC. So this delegates to truncateSDMMAC
// rather than re-implementing the index arithmetic — one selection, one place to
// be wrong, and an12196_kat_test.go's Table 5 vector already guards it.
//
// The two numberings agree: "even-numbered bytes in most-to-least-significant
// order" means S14, S12, … S0 with S15 leftmost, and those sit at slice indices
// 1, 3, … 15. AN12196 rev. 2.0 §5.16.1 Table 25 steps 17-18 publish the pair —
// CMAC EA5D2E0CBFE24C0BCBCD501D21060EE6 truncates to 5D0CE20BCD1D06E6.
//
// full arrives BY VALUE, so this frame holds a third copy of the untruncated CMAC
// and wipes it. The argument to truncateSDMMAC is evaluated — a fourth copy, which
// that function wipes itself — and the [8]byte result is assigned to this
// function's UNNAMED result before any deferred call runs, so the caller gets the
// tag and this frame keeps zeros.
func truncateMACt(full [16]byte) [8]byte {
	defer Zero(full[:])
	return truncateSDMMAC(full)
}

// ev2Pad applies ISO/IEC 9797-1 padding method 2, as datasheet rev. 3.0 §9.1.4
// prescribes: always an 80h, then zeros to a multiple of 16 — "Note that if the
// plain data is a multiple of 16 bytes already, an additional padding block is
// added". That last clause is why this never returns its input unchanged.
func ev2Pad(in []byte) []byte {
	out := make([]byte, 0, len(in)+aes.BlockSize)
	out = append(out, in...)
	out = append(out, ev2PadByte)
	for len(out)%aes.BlockSize != 0 {
		out = append(out, 0x00)
	}
	return out
}

// ev2Unpad removes ISO/IEC 9797-1 method 2 padding, rejecting anything that is
// not "80h followed by zeros" within the final block. A malformed padding is an
// error rather than a best-effort trim: the chip does the same in the other
// direction — datasheet rev. 3.0 §9.1.10, "Commands without a valid padding are
// also rejected by returning INTEGRITY_ERROR".
func ev2Unpad(in []byte) ([]byte, error) {
	for i := len(in) - 1; i >= 0 && i >= len(in)-aes.BlockSize; i-- {
		switch in[i] {
		case 0x00:
			continue
		case ev2PadByte:
			return in[:i], nil
		default:
			return nil, fmt.Errorf("sun: ev2: decrypted data is not padded per ISO 9797-1 method 2")
		}
	}
	return nil, fmt.Errorf("sun: ev2: decrypted data is not padded per ISO 9797-1 method 2")
}

// ev2CBCEncrypt / ev2CBCDecrypt are AES-128 CBC, the mode datasheet rev. 3.0
// §9.1.4 names: "Encryption and decryption are calculated using AES-128 according
// to the CBC mode of NIST SP800-38A". They pad nothing — padding is the caller's
// decision, because the authentication exchange explicitly has none (same
// section) while message data always does.
func ev2CBCEncrypt(key, iv, in []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("sun: ev2: invalid key length: %w", err)
	}
	if len(in)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("sun: ev2: plaintext must be a multiple of %d bytes, got %d", aes.BlockSize, len(in))
	}
	out := make([]byte, len(in))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, in)
	return out, nil
}

func ev2CBCDecrypt(key, iv, in []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("sun: ev2: invalid key length: %w", err)
	}
	if len(in)%aes.BlockSize != 0 || len(in) == 0 {
		return nil, fmt.Errorf("sun: ev2: ciphertext must be a non-zero multiple of %d bytes, got %d", aes.BlockSize, len(in))
	}
	out := make([]byte, len(in))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, in)
	return out, nil
}

// ev2ZeroIV is the all-zero IV used during authentication (datasheet rev. 3.0
// §9.1.4). It is built fresh each call because CryptBlocks mutates the IV slice.
func ev2ZeroIV() []byte { return make([]byte, aes.BlockSize) }

// ev2RotateLeft1 moves the FIRST byte to the back — the "rotate left by 1 byte"
// of datasheet rev. 3.0 §9.1.5, pinned by AN12196 rev. 2.0 §5.6 Table 14 steps
// 9 and 11 (B9E2FC…6E48 becomes E2FC…6E48B9). It copies; the input is left alone.
func ev2RotateLeft1(in []byte) []byte {
	out := make([]byte, 0, len(in))
	out = append(out, in[1:]...)
	return append(out, in[0])
}

// ev2RotateRight1 moves the LAST byte to the front — the inverse, and the
// "rotate right for 1 byte" of AN12196 rev. 2.0 §5.6 Table 14 step 23
// (C5DB…360F13 becomes 13C5DB…360F).
func ev2RotateRight1(in []byte) []byte {
	out := make([]byte, 0, len(in))
	out = append(out, in[len(in)-1])
	return append(out, in[:len(in)-1]...)
}

// ev2CheckAuth validates the shape of an EV2Auth before it is used. It reports
// lengths only, never bytes (§4.7).
func ev2CheckAuth(a *EV2Auth) error {
	if a == nil {
		return fmt.Errorf("sun: ev2: no authenticated session")
	}
	if len(a.TI) != ev2TILen {
		return fmt.Errorf("sun: ev2: transaction identifier must be %d bytes, got %d", ev2TILen, len(a.TI))
	}
	if len(a.KeyENC) != ev2KeyLen || len(a.KeyMAC) != ev2KeyLen {
		return fmt.Errorf("sun: ev2: both session keys must be %d bytes, got %d and %d", ev2KeyLen, len(a.KeyENC), len(a.KeyMAC))
	}
	return nil
}
