package sun

import (
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"hash/crc32"
)

// Cmd.ChangeKey — the one personalisation command whose body has its own layout,
// and the only one where getting a byte wrong destroys the plaque rather than
// erroring out. ADR 0017 §5.1 calls it twice: step 6 writes K_SDMFileRead
// (application key 0x01) and step 8 writes the AppMasterKey (key 0x00).
//
// 🔴 THE TWO CASES ARE DIFFERENT COMMANDS WEARING ONE NAME, AND THE SWITCH IS ON
// THE KEY NUMBER. NT4H2421Gx rev. 3.0 §10.6.1 Table 63 (p. 62), transcribed:
//
//	KeyData   17 or 21   full range (17-byte length)  if key 0 is to be changed
//	                       NewKey || KeyVer
//	                     full range (21-byte length)  if key 1 to 4 are to be changed
//	                       (NewKey XOR OldKey) || KeyVer || CRC32NK          [1]
//	[1] The CRC32NK is the 4-byte CRC value computed according to IEEE Std
//	    802.3-2008 (FCS Field) over NewKey
//
// AN12196 names the same split by session ("KeyNo to be changed does not equal
// AuthKey" / "…equals AuthKey"), and because personalisation always authenticates
// with key 0 the two namings coincide. ADR 0017 §5.1 says which one the code must
// follow: "kod NUMARAYA bakmalıdır, oturuma değil" — a future flow that
// authenticates with something other than key 0 must not silently pick case 2.
//
// 🔴 NO PUBLISHED VECTOR DISTINGUISHES THE XOR (ADR 0017 §6 md. 6). Both of
// AN12196's ChangeKey examples change a key whose OLD value is all zeros, so
// Old ⊕ New == New and an implementation that dropped the XOR entirely would
// reproduce both. That is harmless during a first encode (a blank chip really is
// all-zero — datasheet rev. 3.0 §8.2.4.2) and NOT harmless during ADR 0017 §5.3's
// recovery, where the old key is ours and is not zero. changekey_test.go
// therefore carries a non-zero-old-key case that is explicitly DERIVED, NOT
// TRANSCRIBED, and says so.

const (
	// changeKeyINS is the command code. NT4H2421Gx rev. 3.0 §10.6.1 Table 63:
	// "Cmd 1 C4h Command code."
	changeKeyINS = 0xC4

	// changeKeyMaxKeyNo is the highest application key number. Table 63:
	// "Bit 5-0 … 0h..4h The application key number".
	changeKeyMaxKeyNo = 0x04

	// appMasterKeyNo is application key 0 — the AppMasterKey. Datasheet rev. 3.0
	// §8.2.4.1: "A successful authentication with the AppMasterKey is required to
	// change any application key including the AppMasterKey itself with the
	// ChangeKey command."
	appMasterKeyNo = 0x00

	// changeKeyDataLenSame / changeKeyDataLenOther are Table 63's two lengths.
	// They are asserted on the way out rather than assumed, because the chip
	// rejects a wrong length with LENGTH_ERROR (Table 65) and a length bug would
	// otherwise be invisible until silicon.
	changeKeyDataLenSame  = ev2KeyLen + 1                 // NewKey || KeyVer
	changeKeyDataLenOther = ev2KeyLen + 1 + crc32NKLength // XOR || KeyVer || CRC32NK

	// crc32NKLength — Table 63 footnote [1]: "the 4-byte CRC value".
	crc32NKLength = 4
)

// ChangeKeyData builds the CmdData ("KeyData") of Cmd.ChangeKey for keyNo, per
// NT4H2421Gx rev. 3.0 §10.6.1 Table 63.
//
// newKey is the 16-byte AES-128 key to install. oldKey is the key currently held
// in that slot — for a blank chip, 16 zero bytes (datasheet rev. 3.0 §8.2.4.2:
// "The transport value of these 5 keys is 16 bytes of 00h"). keyVer is the
// version byte stored alongside the key (datasheet rev. 3.0 §8.2.4.6 "Key
// version": "A 1-byte key version (00h to FFh) is assigned to each key … The key
// version is set with the ChangeKey command and can be retrieved using the
// GetVersion command").
//
// ⚠️ THAT LAST WORD IS THE DOCUMENT'S AND IT LOOKS WRONG — FLAGGED, NOT SILENTLY
// CORRECTED. §8.2.4.6 says "GetVersion", but GetVersion (§10.5.2) returns the
// hardware/software/production data and the UID; the command that returns a KEY
// version is GetKeyVersion (§10.6.2 Table 66-68, INS 64h, "retrieves the current
// key version of any key"). Nothing in this file depends on which one it is —
// ChangeKeyData only WRITES the version byte — so the discrepancy is recorded
// rather than acted on. A turn that needs to READ a key version back (ADR 0017
// §5.3's probes are the likely caller) must resolve it against silicon.
//
// ⚠️ FOR keyNo 0 THE BODY DOES NOT CONTAIN oldKey AT ALL — Table 63's 17-byte
// form is NewKey || KeyVer, full stop. oldKey is still validated for length here
// so callers cannot get into the habit of passing nothing, and it is still the
// key the session must have authenticated with, but it is not read into the
// output. That asymmetry is the document's, not a shortcut.
//
// 🔴 The returned slice IS KEY MATERIAL (§4.7): for keyNo 0 it literally contains
// the new plaque key. Zero it as soon as it has been encrypted. EV2ChangeKeyCommand
// does that for callers who do not need the raw body.
func ChangeKeyData(keyNo byte, oldKey, newKey []byte, keyVer byte) ([]byte, error) {
	if keyNo > changeKeyMaxKeyNo {
		return nil, fmt.Errorf("sun: changekey: key number must be 0..%d, got %d", changeKeyMaxKeyNo, keyNo)
	}
	if len(newKey) != ev2KeyLen {
		return nil, fmt.Errorf("sun: changekey: new key must be %d bytes, got %d", ev2KeyLen, len(newKey))
	}
	if len(oldKey) != ev2KeyLen {
		return nil, fmt.Errorf("sun: changekey: old key must be %d bytes, got %d", ev2KeyLen, len(oldKey))
	}

	// 🔴 TWO VALUE GATES, AND THEY BELONG HERE FOR ONE REASON: this is the only
	// layer that knows what "16 bytes of 00h" MEANS. Table 63 checks lengths and
	// nothing else, so both of the bodies below are perfectly well-formed protocol
	// and the chip would accept either.
	//
	// GATE 1 — THE TRANSPORT VALUE. Datasheet rev. 3.0 §8.2.4.2: "The transport
	// value of these 5 keys is 16 bytes of 00h", i.e. the PUBLIC factory default.
	// Writing it as a NEW key produces a plaque that is marked encoded, carries a
	// wrapped all-zero key in its row, and is still openable by anyone who has read
	// the data sheet. Worse, it is INVISIBLE to the half-write diagnosis: ADR 0017
	// §5.3's probe 2 authenticates with "the key in the row", which would SUCCEED,
	// so the plaque looks correctly personalised and is not. That is ADR 0005 risk
	// 8 (the phishing vector) arriving with no detection signal at all.
	//
	// GATE 2 — THE NO-OP. new == old writes a key back onto itself. On a 1..4 body
	// that means an all-zero XOR half; on the key-0 body it means re-sending the
	// same 17 bytes. Both are accepted by the chip and both change nothing, so a
	// caller that reached here has lost track of which state the plaque is in —
	// and ADR 0017 §5.3 is explicit that recovery must NOT call ChangeKey when
	// probe 2 already succeeded ("kurtarma hiç ChangeKey çağırmaz"). Failing is
	// how that mistake becomes visible instead of becoming a silent success.
	//
	// Both use subtle.ConstantTimeCompare rather than bytes.Equal. There is no
	// attacker input on this path — newKey is our own crypto/rand output — so this
	// is the lighter, secret-to-secret class cmd/tappa/constanttime_test.go already
	// documents for internal/config; it is spelled this way because "the ones we
	// think are safe" is not a category that survives editing.
	if subtle.ConstantTimeCompare(newKey, make([]byte, ev2KeyLen)) == 1 {
		// Names no bytes (§4.7) — and none are needed: the value is public.
		return nil, fmt.Errorf("sun: changekey: refusing to install the all-zero transport key")
	}
	if subtle.ConstantTimeCompare(newKey, oldKey) == 1 {
		return nil, fmt.Errorf("sun: changekey: new key equals the old key; this command would change nothing")
	}

	if keyNo == appMasterKeyNo {
		// Case 2 — 17 bytes: NewKey || KeyVer. No XOR, no CRC.
		// AN12196 rev. 2.0 §5.16.2 Table 26 step 11 (rev. 1.8 §6.16.2 Table 27)
		// publishes it before padding:
		//   5004BF991F408672B1EF00F08F9E8647 01 || 800000…
		out := make([]byte, 0, changeKeyDataLenSame)
		out = append(out, newKey...)
		out = append(out, keyVer)
		return out, nil
	}

	// Case 1 — 21 bytes: (NewKey XOR OldKey) || KeyVer || CRC32NK.
	// AN12196 rev. 2.0 §5.16.1 Table 25 step 11 (rev. 1.8 §6.16.1 Table 26)
	// publishes it before padding, for an all-zero OldKey:
	//   F3847D627727ED3BC9C4CC050489B966 01 789DFADC || 800000…
	// which pins the ORDER of the three fields and the CRC's spelling, but not
	// the XOR itself (see the file header, ADR 0017 §6 md. 6).
	out := make([]byte, 0, changeKeyDataLenOther)
	for i := range newKey {
		out = append(out, newKey[i]^oldKey[i])
	}
	out = append(out, keyVer)
	crc := crc32NK(newKey)
	out = append(out, crc[:]...)
	return out, nil
}

// crc32NK computes CRC32NK — NT4H2421Gx rev. 3.0 §10.6.1 Table 63 footnote [1]:
// "the 4-byte CRC value computed according to IEEE Std 802.3-2008 (FCS Field)
// over NewKey".
//
// 🔴 THAT SENTENCE DOES NOT DETERMINE THE BYTES, AND THE DOCUMENT'S OWN VALUE
// DOES. "IEEE 802.3 FCS" leaves at least two free choices — whether the register
// is complemented at the end (the FCS is, the bare CRC register is not) and which
// end of the 32-bit word goes on the wire first — so four spellings are equally
// defensible from the prose. AN12196 rev. 2.0 §5.16.1 Table 25 step 7 (rev. 1.8
// §6.16.1 Table 26 step 7) publishes one of them as a value:
//
//	New Key       = F3847D627727ED3BC9C4CC050489B966
//	CRC32(NewKey) = 789DFADC
//
// and exactly one of the four reproduces it: the UNCOMPLEMENTED register,
// serialised LSB-first. TestChangeKeyKAT_CRC32SpellingIsDiscriminated computes
// all four independently of this function and asserts that only one matches, so
// the choice is anchored to the document rather than to this comment.
//
// crc32.ChecksumIEEE returns the FCS-style complemented value, hence the ^.
func crc32NK(newKey []byte) [crc32NKLength]byte {
	raw := ^crc32.ChecksumIEEE(newKey)
	var out [crc32NKLength]byte
	binary.LittleEndian.PutUint32(out[:], raw)
	return out
}

// EV2ChangeKeyCommand builds the complete ISO 7816 data field for Cmd.ChangeKey
// inside an established EV2 session: the key number as CmdHeader, the encrypted
// KeyData, and the truncated command MAC.
//
// It is EV2WrapCommandFull applied to ChangeKey's body, and that is the point —
// ChangeKey is CommMode.Full like every other personalisation command
// (NT4H2421Gx rev. 3.0 §10.6.1: "CommMode.Full is applied for this command"), so
// nothing bespoke happens to the sealing. AN12196 rev. 2.0 §5.16.1 Table 25 step
// 15 shows the CmdHeader is the key number:
//
//	Input for CMAC = C4 0200 7614281A 02 || E(KSesAuthENC, KeyData)
//	                 ^^ Cmd  ^^^^ ctr    ^^ KeyNo as CmdHeader
//
// cmdCtr is the counter for THIS command (see EV2WrapCommandFull).
//
// ⚠️ CASE 2 ENDS THE SESSION AND THE CALLER MUST KNOW. When keyNo equals the key
// the session authenticated with, the chip answers 9100 with NO response MAC —
// AN12196 rev. 2.0 §5.16.2 Table 26 step 18 prints a bare "9100", while the case
// 1 response in Table 25 step 20 carries "203BB55D1089D5879100". Do not feed a
// case 2 response to EV2UnwrapResponseFull; the session key it would verify with
// no longer exists on the chip. ADR 0017 §5.1 puts key 0 LAST for exactly this
// reason — "sona koymak soruyu ortadan kaldırır".
//
// The plaintext body is zeroised here, so a caller that only needs the APDU field
// never holds the new key twice (§4.7).
func EV2ChangeKeyCommand(auth *EV2Auth, cmdCtr uint16, keyNo byte, oldKey, newKey []byte, keyVer byte) ([]byte, error) {
	body, err := ChangeKeyData(keyNo, oldKey, newKey, keyVer)
	if err != nil {
		return nil, err
	}
	defer Zero(body)
	return EV2WrapCommandFull(auth, changeKeyINS, cmdCtr, []byte{keyNo}, body)
}
