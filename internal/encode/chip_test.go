package encode

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/atknatk/tappa/internal/sun"
)

// chip_test.go — an NTAG 424 DNA good enough to answer the ten exchanges of
// ADR 0017 §5.1, and a note about exactly what a green round trip against it does
// and does not prove.
//
// 🔴 WHAT IT PROVES. This double VERIFIES every command MAC before acting and
// COMPUTES every response MAC itself, from its own copy of the secure-messaging
// rules (the AES-CMAC below is an independent RFC 4493 implementation, not
// internal/sun's). So a round that completes is evidence about the DRIVER's side of
// the protocol conversation:
//   - the command counter really is 0, 1, 2, 3 in that order, incremented exactly
//     once per sealed command, and the response really is verified against the
//     increased value (mutate either and the chip's MAC check fails);
//   - TI really is the one the chip minted;
//   - the CmdHeader/CmdData split really is the one each command's table gives — for
//     WriteData the double now decodes all three header fields (FileNo, Offset,
//     Length) and checks the declared length against the body, so a wrong file, a
//     wrong offset or a mis-declared length is refused. ⚠️ For ChangeKey and
//     ChangeFileSettings it still only checks the header's LENGTH and, for ChangeKey,
//     reads the key number; their header CONTENTS beyond that are not validated here.
//   - the ten commands really are emitted in the table's order, because the chip
//     refuses a command it is not in a state to answer.
//
// 🔴 WHAT IT DOES *NOT* PROVE, and this is the half a test double is always asked
// to overstate:
//   - NOTHING ABOUT SILICON. ADR 0017 §6 md. 1 still stands: no chip has been
//     encoded. This file is a reading of two documents, written in the same
//     repository, by the same hand as the code it exercises. If both share a
//     misreading, both are green.
//   - NOTHING ABOUT THE KDF. The double asks internal/sun to derive the session
//     keys (EV2AuthPart2) rather than deriving them again here, so the round trip
//     cannot disagree about SV1/SV2. That derivation is already pinned from
//     OUTSIDE, by AN12196's published vectors in internal/sun/ev2_kat_test.go, which
//     is where that evidence belongs.
//   - NOTHING ABOUT WHETHER A COMMAND WAS ALLOWED AT ALL — the double enforces no
//     access right and no CommMode rule (see the access-rights item below). It DOES
//     now check ChangeKey's BODY FORMAT against Table 63's KEY-NUMBER split (key 0
//     takes the 17-byte form, keys 1..4 the 21-byte one) and Table 65's requirement
//     that ChangeKey run under an AppMasterKey session. That gap was found third in a
//     series (file number, WriteData offset/length, body format), and it is the one
//     ADR 0017 §5.1 step 8 will walk straight into. ⚠️ The first version of that gate
//     used the WRONG criterion (keyNo == AuthKey) and cited Table 63 for it; see
//     applyChangeKey.
//   - 🔴 UNRESOLVED, NOT CLEAN — WHETHER THE SESSION SURVIVES A ChangeKey ON THE
//     AUTHENTICATED KEY. This double leaves c.authed = true after ANY ChangeKey,
//     including key 0, which is the key every session authenticates with. An auditor
//     raised it and COULD NOT SETTLE IT FROM THE DOCUMENTS, and neither can this
//     comment: §10.6.1 and Tables 63/64/65 say nothing about the session's fate after
//     the authenticated key changes, and the ADR's only evidence (AN12196 session B,
//     CmdCtr 0200 -> 0300) is about a NON-ZERO key. internal/sun/changekey.go reaches
//     the opposite guess for its own callers — "CASE 2 ENDS THE SESSION" — and cites
//     AN12196's bare 9100 for it.
//     Unreachable today because ADR 0017 §5.1 step 8 is not shipped; it becomes
//     load-bearing the day it is, which is also the day §6 md. 5 lands. Recorded here
//     rather than resolved, and handed to that round.
//   - NOTHING ABOUT ChangeKey's CRC32NK. The chip below does not recompute it,
//     because doing so with the same spelling internal/sun uses would be a second
//     copy of one reading rather than a check. That value is anchored to the
//     document in internal/sun/changekey_test.go.
//   - NOTHING ABOUT THE WIRE ENVELOPE beyond what it parses: it accepts the 90/00/00
//     framing internal/sun builds without independently deriving it.
//   - 🔴 NOTHING ABOUT ACCESS RIGHTS OR CommMode RULES, AND THAT OMISSION IS LOAD-
//     BEARING RATHER THAN INCIDENTAL. This double enforces neither, so it accepts the
//     one combination driver.go's own roundSteps header records as an OPEN question:
//     a CommMode.Full WriteData to file 02h while that file is still at Table 8's
//     delivery rights (Write = ReadWrite = Eh), which datasheet §8.2.3.3 says should
//     be CommMode.Plain. Every green run of this suite passes through that
//     combination. The general "nothing about silicon" item above covers it, but a
//     header that names its other limits one by one should name the one that sits
//     under an open question rather than leave it to the catch-all.

const (
	swSuccessLo = 0x00 // SW2 of 9100, the value the response MAC is computed over
	macTLen     = 8
)

// --- an independent AES-CMAC (RFC 4493) ----------------------------------------

func cmacSubkeys(b cipher.Block) (k1, k2 []byte) {
	l := make([]byte, aes.BlockSize)
	b.Encrypt(l, make([]byte, aes.BlockSize))
	k1 = shl1(l)
	k2 = shl1(k1)
	return k1, k2
}

// shl1 is a one-bit left shift with RFC 4493's Rb = 0x87 reduction.
func shl1(in []byte) []byte {
	out := make([]byte, len(in))
	var carry byte
	for i := len(in) - 1; i >= 0; i-- {
		out[i] = in[i]<<1 | carry
		carry = in[i] >> 7
	}
	if in[0]&0x80 != 0 {
		out[len(out)-1] ^= 0x87
	}
	return out
}

func aesCMAC(t *testing.T, key, msg []byte) []byte {
	t.Helper()
	b, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("chip: aes: %v", err)
	}
	k1, k2 := cmacSubkeys(b)

	var last []byte
	n := (len(msg) + aes.BlockSize - 1) / aes.BlockSize
	if len(msg) > 0 && len(msg)%aes.BlockSize == 0 {
		last = xorBytes(msg[(n-1)*aes.BlockSize:], k1)
	} else {
		if n == 0 {
			n = 1
		}
		tail := append([]byte(nil), msg[(n-1)*aes.BlockSize:]...)
		tail = append(tail, 0x80)
		for len(tail) < aes.BlockSize {
			tail = append(tail, 0x00)
		}
		last = xorBytes(tail, k2)
	}

	x := make([]byte, aes.BlockSize)
	for i := 0; i < n-1; i++ {
		x = xorBytes(x, msg[i*aes.BlockSize:(i+1)*aes.BlockSize])
		b.Encrypt(x, x)
	}
	x = xorBytes(x, last)
	b.Encrypt(x, x)
	return x
}

func xorBytes(a, b []byte) []byte {
	out := make([]byte, len(a))
	for i := range a {
		out[i] = a[i] ^ b[i]
	}
	return out
}

// truncMAC is the datasheet's "8 even-numbered bytes out of the 16" (rev. 3.0
// §9.1.3), i.e. slice indices 1, 3, ... 15.
func truncMAC(full []byte) []byte {
	out := make([]byte, macTLen)
	for i := 0; i < macTLen; i++ {
		out[i] = full[2*i+1]
	}
	return out
}

// --- AES helpers ---------------------------------------------------------------

func cbcEncrypt(t *testing.T, key, iv, in []byte) []byte {
	t.Helper()
	b, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("chip: aes: %v", err)
	}
	out := make([]byte, len(in))
	cipher.NewCBCEncrypter(b, iv).CryptBlocks(out, in)
	return out
}

func cbcDecrypt(t *testing.T, key, iv, in []byte) []byte {
	t.Helper()
	b, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("chip: aes: %v", err)
	}
	out := make([]byte, len(in))
	cipher.NewCBCDecrypter(b, iv).CryptBlocks(out, in)
	return out
}

func ecbBlock(t *testing.T, key, in []byte) []byte {
	t.Helper()
	b, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("chip: aes: %v", err)
	}
	out := make([]byte, aes.BlockSize)
	b.Encrypt(out, in)
	return out
}

func iso9797Pad(in []byte) []byte {
	out := append(append([]byte(nil), in...), 0x80)
	for len(out)%aes.BlockSize != 0 {
		out = append(out, 0x00)
	}
	return out
}

func iso9797Unpad(t *testing.T, in []byte) []byte {
	t.Helper()
	for i := len(in) - 1; i >= 0; i-- {
		switch in[i] {
		case 0x00:
			continue
		case 0x80:
			return in[:i]
		default:
			t.Fatalf("chip: malformed padding")
		}
	}
	t.Fatalf("chip: all-zero padding")
	return nil
}

func rotateLeft1(in []byte) []byte {
	return append(append([]byte(nil), in[1:]...), in[0])
}

// --- the chip ------------------------------------------------------------------

type chipPhase int

const (
	phaseIdle chipPhase = iota
	phaseGetVersion2
	phaseGetVersion3
	phaseAuth2
)

type fakeChip struct {
	t *testing.T

	uid  []byte
	keys map[byte][]byte

	selected bool
	phase    chipPhase

	// live session
	authed    bool
	authKeyNo byte
	ti        []byte
	kenc      []byte
	kmac      []byte
	ctr       uint16
	rndA      []byte
	rndB      []byte

	// what the chip ended up holding.
	//
	// 🔴 files IS KEYED BY FILE NUMBER, AND THAT WAS A REAL DOUBLE WEAKNESS UNTIL
	// 2026-08-21. It used to be a single `ndef []byte` that every WriteData
	// overwrote regardless of the file it addressed, so a driver writing the tap
	// template into the PROPRIETARY file 03h looked identical to one writing it into
	// the NDEF file 02h — and the mutation that does exactly that survived. On
	// silicon that plaque emits nothing at all: file 02h stays empty and the SDM
	// mirror offsets step 7 configures point into it.
	files            map[byte][]byte
	fileSettingsBody []byte

	// rndBSeed makes each handshake's RndB distinct without needing a real RNG:
	// the chip is a test double and its randomness is not what is under test.
	rndBSeed byte

	// --- observation, for the assertions -------------------------------------
	// insSeen is every command byte the driver emitted, in order.
	insSeen []byte
	// rndAsSeen is every PCD challenge the driver produced, recovered by the chip
	// from the Part 2 cryptogram. TestDriver_RndAIsFreshOnEveryRound reads it.
	rndAsSeen [][]byte

	// --- knobs, for the negative cases ---------------------------------------
	// failNextWithSW makes the next command answer with this status word.
	failNextWithSW uint16
	// getVersionUIDLie replaces the UID in GetVersion's third frame ONLY — the
	// "relay that lies with a string edit" of ADR 0017 §6 md. 12's fourth
	// consequence. GetCardUID still answers with the real UID, sealed.
	getVersionUIDLie []byte
	// injectRespData makes a command answer with a sealed body it should not have,
	// or with one of the wrong length. Both are shapes a real chip would not
	// produce and a hostile relay could not forge — the point of exercising them is
	// that the driver must not accept them from a chip that has gone strange.
	injectRespData map[byte][]byte
	// selectExtraData makes the ISO SELECT answer 9000 WITH a body, which P2 = 0Ch
	// says it must not.
	selectExtraData []byte
	// corruptRespMACFor flips a bit in the response MAC of one command, so the
	// driver's verification of a SUCCESSFUL-looking frame can be exercised. A real
	// chip cannot do this; a broken transport can.
	corruptRespMACFor map[byte]bool
}

func newFakeChip(t *testing.T) *fakeChip {
	t.Helper()
	return &fakeChip{
		t:     t,
		uid:   []byte{0x04, 0x96, 0x8C, 0xAA, 0x5C, 0x5E, 0x80},
		files: map[byte][]byte{},
		keys: map[byte][]byte{
			0x00: make([]byte, 16),
			0x01: make([]byte, 16),
			0x02: make([]byte, 16),
			0x03: make([]byte, 16),
			0x04: make([]byte, 16),
		},
	}
}

func sw(code uint16) []byte { return []byte{byte(code >> 8), byte(code)} }

// Transceive is the phone: bytes in, bytes out, no interpretation.
//
// ⚠️ IT CALLS t.Fatalf, AND IT IS REACHABLE FROM NON-TEST GOROUTINES (tenth audit,
// 2026-08-21, flagged and handed on). The concurrency tests drive rounds from spawned
// goroutines, and testing.T documents Fatalf as valid only from the goroutine running
// the test. It does not fire today — every Fatalf here guards a frame a conforming
// driver never produces — and go vet does not see this shape. If one ever does fire
// off-goroutine the failure will be reported oddly rather than cleanly, which is a
// diagnosis cost, not a correctness one. Item 9 of the card's handover list.
func (c *fakeChip) Transceive(capdu []byte) []byte {
	t := c.t
	t.Helper()
	if len(capdu) < 4 {
		t.Fatalf("chip: runt C-APDU (%d bytes)", len(capdu))
	}
	if c.failNextWithSW != 0 {
		code := c.failNextWithSW
		c.failNextWithSW = 0
		// Datasheet rev. 3.0 §9.1.10: an error frame carries NO MAC and the
		// authentication state is lost.
		c.authed = false
		return sw(code)
	}

	// ISO SELECT is the one command that is not wrapped-native.
	if capdu[0] == 0x00 && capdu[1] == 0xA4 {
		c.insSeen = append(c.insSeen, 0xA4)
		c.selected = true
		return append(append([]byte(nil), c.selectExtraData...), sw(0x9000)...)
	}
	if capdu[0] != 0x90 {
		t.Fatalf("chip: unexpected class byte %02X", capdu[0])
	}
	if !c.selected {
		return sw(0x916E)
	}

	ins := capdu[1]
	c.insSeen = append(c.insSeen, ins)
	body := nativeBody(t, capdu)

	switch ins {
	case 0x60: // GetVersion
		c.phase = phaseGetVersion2
		return append(bytesOf(7, 0x04), sw(0x91AF)...)

	case 0xAF: // AdditionalFrame: GetVersion continuation OR auth part 2
		switch c.phase {
		case phaseGetVersion2:
			c.phase = phaseGetVersion3
			return append(bytesOf(7, 0x05), sw(0x91AF)...)
		case phaseGetVersion3:
			c.phase = phaseIdle
			uid := c.uid
			if c.getVersionUIDLie != nil {
				uid = c.getVersionUIDLie
			}
			frame := append(append([]byte(nil), uid...), bytesOf(7, 0x11)...)
			return append(frame, sw(0x9100)...)
		case phaseAuth2:
			return c.authPart2(body)
		default:
			t.Fatalf("chip: AF with nothing pending")
		}

	case 0x71: // AuthenticateEV2First
		return c.authPart1(body)

	case 0x8D, 0xC4, 0x51, 0x5F:
		return c.sealed(ins, body)
	}

	t.Fatalf("chip: unimplemented command %02X", ins)
	return nil
}

// nativeBody strips 90 INS 00 00 [Lc data] 00 down to the data field.
func nativeBody(t *testing.T, capdu []byte) []byte {
	t.Helper()
	if len(capdu) == 5 {
		return nil
	}
	lc := int(capdu[4])
	if len(capdu) != 5+lc+1 {
		t.Fatalf("chip: Lc says %d but the frame is %d bytes", lc, len(capdu))
	}
	return capdu[5 : 5+lc]
}

func bytesOf(n int, fill byte) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = fill
	}
	return out
}

func (c *fakeChip) authPart1(body []byte) []byte {
	t := c.t
	if len(body) != 2 {
		t.Fatalf("chip: AuthenticateEV2First body is %d bytes, expected KeyNo||LenCap", len(body))
	}
	keyNo := body[0]
	if _, ok := c.keys[keyNo]; !ok {
		return sw(0x9140)
	}
	c.rndBSeed++
	c.rndB = bytesOf(16, c.rndBSeed)
	c.authKeyNo = keyNo
	c.phase = phaseAuth2
	return append(cbcEncrypt(t, c.keys[keyNo], make([]byte, 16), c.rndB), sw(0x91AF)...)
}

func (c *fakeChip) authPart2(body []byte) []byte {
	t := c.t
	if len(body) != 32 {
		t.Fatalf("chip: auth part 2 body is %d bytes, expected 32", len(body))
	}
	key := c.keys[c.authKeyNo]
	plain := cbcDecrypt(t, key, make([]byte, 16), body)
	rndA := plain[:16]
	if got, want := string(plain[16:]), string(rotateLeft1(c.rndB)); got != want {
		// The PCD failed to rotate our challenge: authentication fails and the chip
		// drops everything (datasheet §9.1.10).
		c.phase = phaseIdle
		return sw(0x91AE)
	}
	c.rndA = append([]byte(nil), rndA...)
	c.rndAsSeen = append(c.rndAsSeen, append([]byte(nil), rndA...))

	c.ti = []byte{0x9D, 0x00, 0xC4, 0xDF}
	resp := make([]byte, 0, 32)
	resp = append(resp, c.ti...)
	// RndA' is RndA rotated LEFT by one byte; the PCD rotates RIGHT to recover it
	// (AN12196 rev. 2.0 §5.6 Table 14 steps 20 and 23 — step 20's value ends 0F13
	// and step 23's begins 13).
	resp = append(resp, rotateLeft1(rndA)...)
	resp = append(resp, make([]byte, 12)...) // PDcap2 || PCDcap2
	enc := cbcEncrypt(t, key, make([]byte, 16), resp)

	// The double borrows internal/sun's KDF rather than re-deriving SV1/SV2 —
	// see this file's header for why that is deliberate and what it costs.
	auth, err := sunAuthFromChip(key, rndA, c.rndB, enc)
	if err != nil {
		t.Fatalf("chip: deriving session keys: %v", err)
	}
	c.kenc = auth.KeyENC
	c.kmac = auth.KeyMAC
	c.authed = true
	c.ctr = 0
	c.phase = phaseIdle
	return append(enc, sw(0x9100)...)
}

// headerLen is each sealed command's CmdHeader size, from its own table:
// WriteData FileNo||Offset||Length (Table 81), ChangeKey KeyNo (Table 63),
// ChangeFileSettings FileNo (§10.7.1), GetCardUID none (Table 60).
func headerLen(ins byte) int {
	switch ins {
	case 0x8D:
		return 7
	case 0xC4, 0x5F:
		return 1
	default:
		return 0
	}
}

func (c *fakeChip) sealed(ins byte, body []byte) []byte {
	t := c.t
	if !c.authed {
		return sw(0x91AE) // AUTHENTICATION_ERROR
	}
	hl := headerLen(ins)
	if len(body) < hl+macTLen {
		t.Fatalf("chip: %02X body is %d bytes, too short for header+MAC", ins, len(body))
	}
	header := body[:hl]
	enc := body[hl : len(body)-macTLen]
	gotMAC := body[len(body)-macTLen:]

	macIn := make([]byte, 0, 1+2+len(c.ti)+len(header)+len(enc))
	macIn = append(macIn, ins)
	macIn = binary.LittleEndian.AppendUint16(macIn, c.ctr)
	macIn = append(macIn, c.ti...)
	macIn = append(macIn, header...)
	macIn = append(macIn, enc...)
	if want := truncMAC(aesCMAC(t, c.kmac, macIn)); string(want) != string(gotMAC) {
		// A wrong counter, a wrong TI or a wrong field split all land here — which
		// is exactly what makes this double useful as a gate.
		c.authed = false
		return sw(0x911E) // INTEGRITY_ERROR
	}

	var plain []byte
	if len(enc) > 0 {
		iv := ecbBlock(t, c.kenc, ivInput(0xA5, 0x5A, c.ti, c.ctr))
		plain = iso9797Unpad(t, cbcDecrypt(t, c.kenc, iv, enc))
	}

	var respData []byte
	switch ins {
	case 0x8D: // WriteData
		// header is FileNo || Offset(3) || Length(3) — Table 81, both 3-byte fields
		// LSB first.
		//
		// 🔴 THE OFFSET AND THE LENGTH USED TO BE IGNORED HERE, AND THAT WAS THE SAME
		// DOUBLE WEAKNESS AS THE FILE NUMBER ONE FOUND A ROUND EARLIER, ONE FIELD OVER
		// (audit, 2026-08-21). This line was `c.files[header[0]] = plain`, so a driver
		// writing the template at offset 5 instead of 0 looked identical to one
		// writing it at 0 — and the mutation that does exactly that survived. On
		// silicon that plaque emits nothing usable: step 7's SDM mirror offsets are
		// absolute file positions, so a shifted template points them at the wrong
		// bytes and every tap fails its CMAC forever.
		off := uint32(header[1]) | uint32(header[2])<<8 | uint32(header[3])<<16
		length := uint32(header[4]) | uint32(header[5])<<8 | uint32(header[6])<<16
		if int(length) != len(plain) {
			t.Fatalf("chip: WriteData declared %d bytes and sent %d", length, len(plain))
		}
		file := c.files[header[0]]
		for len(file) < int(off)+len(plain) {
			file = append(file, 0x00)
		}
		copy(file[off:], plain)
		c.files[header[0]] = file
	case 0xC4: // ChangeKey
		c.applyChangeKey(header[0], plain)
	case 0x5F: // ChangeFileSettings
		c.fileSettingsBody = append([]byte(nil), plain...)
	case 0x51: // GetCardUID
		respData = append([]byte(nil), c.uid...)
	}
	if injected, ok := c.injectRespData[ins]; ok {
		respData = append([]byte(nil), injected...)
	}

	c.ctr++
	out := c.sealResponse(respData)
	if c.corruptRespMACFor[ins] {
		out[len(out)-3] ^= 0x01
	}
	return out
}

func (c *fakeChip) applyChangeKey(keyNo byte, plain []byte) {
	t := c.t
	old, ok := c.keys[keyNo]
	if !ok {
		t.Fatalf("chip: ChangeKey for unknown key %02X", keyNo)
	}

	// 🔴 THE BODY FORMAT IS A FUNCTION OF THE KEY NUMBER, AND THE VERSION OF THIS GATE
	// SHIPPED ONE ROUND AGO ENCODED THE OPPOSITE RULE (fifth audit, 2026-08-21;
	// re-measured here from the PDF, md5 dcf5319c…, 97 pp.). It compared
	// keyNo == c.authKeyNo and cited "Table 63" for it. Table 63 (p. 62) says no such
	// thing — its two KeyData rows read, verbatim:
	//
	//	full range (17-byte length)  if key 0 is to be changed
	//	  NewKey || KeyVer
	//	full range (21-byte length)  if key 1 to 4 are to be changed
	//	  (NewKey XOR OldKey) || KeyVer || CRC32NK
	//
	// The "KeyNo == AuthKey" framing is AN12196's SECTION-HEADING language (§5.16.1 /
	// §5.16.2), not the datasheet's rule. This repository already had it right and was
	// not consulted: internal/sun/changekey.go switches on appMasterKeyNo and says
	// "THE SWITCH IS ON THE KEY NUMBER … a future flow that authenticates with
	// something other than key 0 must not silently pick case 2", and ADR 0017 §5.1
	// says "kod NUMARAYA bakmalıdır, oturuma değil".
	//
	// 🔴 AND IT WAS NOT ONLY A CITATION: the wrong rule made this double REJECT
	// ADR-CONFORMING CODE. Authenticating with key 0x01 — the exact shape of ADR 0017
	// §5.3's probe 2, which B2c-2 will run — and changing key 0x01 produces Table 63's
	// correct 21-byte body, and the old gate failed the test for it.
	//
	// TABLE 65's PRECONDITION IS ENFORCED TOO, AND THAT IS A DECISION. Table 65 (p. 63)
	// gives AUTHENTICATION_ERROR AEh as "At application level, missing active
	// authentication with AppMasterKey while targeting any AppKey", and §8.2.4.1 says
	// "A successful authentication with the AppMasterKey is required to change any
	// application key including the AppMasterKey itself". Modelling it costs three
	// lines and buys the thing the old gate was groping for: because ChangeKey is only
	// legal under a key-0 session, "keyNo == AuthKey" and "keyNo == 0" COINCIDE on real
	// silicon — which is precisely why picking the wrong one was invisible, and why
	// ADR 0017 §5.1 warns that the code must still read the number.
	// ⚠️ AND ENFORCING TABLE 65 MAKES THE WRONG CRITERION UNREACHABLE, WHICH IS BOTH
	// THE POINT AND A MEASURED LIMIT ON WHAT ANY TEST HERE CAN PROVE. Once ChangeKey
	// is only legal under a key-0 session, "keyNo == AuthKey" and "keyNo == 0" pick
	// the same branch for every input a caller can construct — so a mutation that
	// swaps this switch back to the session comparison SURVIVES the suite, and that
	// survival is legitimate rather than a gap. It is also exactly what ADR 0017 §5.1
	// predicted: "Kimlik doğrulama daima anahtar 0 ile yapıldığı için ikisi aynı
	// kapıya çıkar — ama kod NUMARAYA bakmalıdır, oturuma değil." The number is what
	// is read below, because the day step 8 or a probe-2 flow changes which key
	// authenticates, the two stop coinciding and only one of them stays correct.
	if c.authKeyNo != 0x00 {
		// Table 65, AEh.
		//
		// ⚠️ MODELLED AS A TEST FAILURE, NOT AS THE FRAME REAL SILICON RETURNS, AND
		// THAT IS A COUNTED LIMIT OF THIS DOUBLE (ninth audit, 2026-08-21). A real chip
		// answers AUTHENTICATION_ERROR AEh and, per §9.1.10, DROPS its authentication
		// state — which is what the driver's RequireStatus would see and what its
		// fail-closed path is written for. Here the harness simply dies, so the header's
		// claim that Table 65 is "enforced" is true of the CHECK and not of the
		// BEHAVIOUR. Unreachable today (nothing authenticates with a non-zero key);
		// it becomes reachable with ADR 0017 §5.1 step 8 or §5.3's probe 2, and it is
		// handed on as item 9 of the card's handover list.
		t.Fatalf("chip: ChangeKey for key %02X in a session authenticated with %02X; "+
			"§8.2.4.1 requires the AppMasterKey", keyNo, c.authKeyNo)
	}
	switch len(plain) {
	case 21: // Table 63: keys 1..4
		if keyNo == 0x00 {
			t.Fatalf("chip: ChangeKey sent the 21-byte body for key 00; Table 63 gives key 0 the " +
				"17-byte NewKey || KeyVer form")
		}
		c.keys[keyNo] = xorBytes(plain[:16], old)
	case 17: // Table 63: key 0
		if keyNo != 0x00 {
			t.Fatalf("chip: ChangeKey sent the 17-byte body for key %02X; Table 63 gives keys 1..4 "+
				"the 21-byte (NewKey XOR OldKey) || KeyVer || CRC32NK form", keyNo)
		}
		c.keys[keyNo] = append([]byte(nil), plain[:16]...)
	default:
		t.Fatalf("chip: ChangeKey body is %d bytes, expected 17 or 21", len(plain))
	}
}

func (c *fakeChip) sealResponse(respData []byte) []byte {
	t := c.t
	var enc []byte
	if len(respData) > 0 {
		iv := ecbBlock(t, c.kenc, ivInput(0x5A, 0xA5, c.ti, c.ctr))
		enc = cbcEncrypt(t, c.kenc, iv, iso9797Pad(respData))
	}
	macIn := make([]byte, 0, 1+2+len(c.ti)+len(enc))
	macIn = append(macIn, swSuccessLo)
	macIn = binary.LittleEndian.AppendUint16(macIn, c.ctr)
	macIn = append(macIn, c.ti...)
	macIn = append(macIn, enc...)
	out := append(enc, truncMAC(aesCMAC(t, c.kmac, macIn))...)
	return append(out, sw(0x9100)...)
}

func ivInput(a, b byte, ti []byte, ctr uint16) []byte {
	in := make([]byte, 0, aes.BlockSize)
	in = append(in, a, b)
	in = append(in, ti...)
	in = binary.LittleEndian.AppendUint16(in, ctr)
	return append(in, make([]byte, 8)...)
}

// sunAuthFromChip derives the session keys for the chip side.
//
// It calls internal/sun's PCD-side finisher with the chip's own inputs, which is
// legal because §9.1.7's derivation is symmetric — both sides compute the same
// SV1/SV2 from the same two randoms. See this file's header for why the double
// borrows the KDF instead of writing a second one.
func sunAuthFromChip(key, rndA, rndB, encResp []byte) (*sun.EV2Auth, error) {
	auth, err := sun.EV2AuthPart2(key, rndA, rndB, encResp)
	if err != nil {
		return nil, fmt.Errorf("chip: %w", err)
	}
	return auth, nil
}

// TestChip_ChangeKeySelectsTheBodyFormByKeyNumberNotBySession pins the double's own
// rule, because a double that encodes the wrong rule rejects correct code.
//
// 🔴 IT EXISTS BECAUSE THE PREVIOUS GATE BOUGHT NOTHING. An audit measured it:
// reverting the whole comparison to a bare `len(plain) == 17` left the entire suite
// green, so the only thing the added line did was encode a WRONG rule — one that
// failed ADR-conforming code the moment a session authenticated with key 0x01
// (ADR 0017 §5.3's probe 2 shape). A gate nothing exercises is not a gate.
//
// Table 63 (p. 62) is the authority and it splits by NUMBER: key 0 takes
// NewKey || KeyVer (17), keys 1..4 take (NewKey XOR OldKey) || KeyVer || CRC32NK (21).
// Table 65 adds that ChangeKey needs an AppMasterKey session at all.
func TestChip_ChangeKeySelectsTheBodyFormByKeyNumberNotBySession(t *testing.T) {
	// The four combinations of (key number, body length), driven straight at the
	// double so the rule is measured rather than inferred from a happy path.
	for _, tc := range []struct {
		name     string
		authKey  byte
		keyNo    byte
		bodyLen  int
		accepted bool
	}{
		{"key0_takes_the_17_byte_form", 0x00, 0x00, 17, true},
		{"key0_refuses_the_21_byte_form", 0x00, 0x00, 21, false},
		{"key1_takes_the_21_byte_form", 0x00, 0x01, 21, true},
		{"key1_refuses_the_17_byte_form", 0x00, 0x01, 17, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := changeKeyAccepted(t, tc.authKey, tc.keyNo, tc.bodyLen)
			if got != tc.accepted {
				t.Fatalf("key %02X with a %d-byte body: accepted=%v, want %v. Table 63 splits on "+
					"the KEY NUMBER (\"if key 0 is to be changed\" / \"if key 1 to 4 are to be "+
					"changed\"), not on whether the key equals the authenticated one",
					tc.keyNo, tc.bodyLen, got, tc.accepted)
			}
		})
	}

	// 🔴 THE DISCRIMINATING CASE, and the one the old rule got backwards: a session
	// authenticated with key 0x01 changing key 0x01. The old gate demanded the
	// 17-byte form here; Table 63 demands the 21-byte one. Table 65's precondition
	// makes the whole situation illegal anyway, which is what this asserts — and that
	// is the honest resolution, because it is why the two criteria coincide on real
	// silicon and why the wrong one was invisible.
	t.Run("changekey_outside_an_appmasterkey_session_is_refused", func(t *testing.T) {
		if changeKeyAccepted(t, 0x01, 0x01, 21) {
			t.Fatalf("ChangeKey was accepted in a session authenticated with key 01. Table 65: " +
				"AUTHENTICATION_ERROR AEh, \"missing active authentication with AppMasterKey while " +
				"targeting any AppKey\"")
		}
		if changeKeyAccepted(t, 0x01, 0x01, 17) {
			t.Fatalf("the 17-byte form was accepted outside an AppMasterKey session")
		}
	})
}

// changeKeyAccepted runs applyChangeKey against a scratch chip and reports whether it
// was accepted, converting the double's t.Fatalf into a boolean.
func changeKeyAccepted(t *testing.T, authKey, keyNo byte, bodyLen int) (accepted bool) {
	t.Helper()
	sub := &testing.T{}
	done := make(chan bool, 1)
	go func() {
		ok := false
		defer func() { done <- ok && !sub.Failed() }()
		c := newFakeChip(t)
		c.t = sub
		c.authKeyNo = authKey
		body := make([]byte, bodyLen)
		for i := range body {
			body[i] = byte(0x40 + i)
		}
		c.applyChangeKey(keyNo, body)
		ok = true
	}()
	return <-done
}
