package sun

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"hash/crc32"
	"strings"
	"testing"
)

// KNOWN-ANSWER TESTS FOR Cmd.ChangeKey (M8-05 FAZ B2a).
//
// The provenance rules of ev2_kat_test.go apply here unchanged: every expected
// value below is transcribed from NXP's published worked examples, none of them
// was produced by changekey.go, and the two values that could NOT be read out of
// a document are labelled DERIVED, NOT TRANSCRIBED where they appear.
//
// SOURCES
//
//	AN12196 rev. 2.0 §5.16.1 Table 25 / rev. 1.8 §6.16.1 Table 26  — case 1
//	AN12196 rev. 2.0 §5.16.2 Table 26 / rev. 1.8 §6.16.2 Table 27  — case 2
//	NT4H2421Gx rev. 3.0 §10.6.1 Table 63, p. 62                    — the body layout
//
// ⚠️ THE TWO EXAMPLES ARE ONE SESSION, AND THAT IS LOAD-BEARING. They share the TI
// 7614281A (minted in rev. 2.0 §5.10 Table 19 step 19) and both session keys
// (derived in rev. 2.0 §5.14 Table 23 steps 23-24); only the counter moves,
// 0200 -> 0300. So the document shows with VALUES that changing a non-zero key
// number does not end the session — the measurement ADR 0017 §5.0 decision 1
// rests on.

const (
	// --- rev. 2.0 §5.16.1 Table 25: KeyNo 0x02 changed while authenticated with
	// KeyNo 0x00 (Table 63's "key 1 to 4" body).
	katT25Cmd    = 0xC4
	katT25Header = "02"   // step 13 Key No, carried as the CmdHeader (step 15)
	katT25Ctr    = 0x0002 // step 14 CmdCtr = 0200, LSB first

	katT25OldKey  = "00000000000000000000000000000000" // step 3
	katT25NewKey  = "F3847D627727ED3BC9C4CC050489B966" // step 4
	katT25KeyVer  = 0x01                               // step 5
	katT25XOR     = "F3847D627727ED3BC9C4CC050489B966" // step 6  Old Key XOR New Key
	katT25CRC     = "789DFADC"                         // step 7  CRC32 (NewKey)
	katT25IVInput = "A55A7614281A02000000000000000000" // step 9
	katT25IV      = "307EDE1814707F30CFE603DD6CA62353" // step 10 IVe

	// step 11, WITHOUT the padding the document prints after it. The full cell is
	//   F3847D627727ED3BC9C4CC050489B96601789DFADC8000000000000000000000
	// i.e. 21 bytes of KeyData then 80 00×10 — Table 63's 21-byte form padded to
	// two blocks per NT4H2421Gx rev. 3.0 §9.1.4.
	katT25KeyData = "F3847D627727ED3BC9C4CC050489B966" + "01" + "789DFADC"

	katT25Ciphertext = "2CF362B7BF4311FF3BE1DAA295E8C68D" + // step 12
		"E09050560D19B9E16C2393AE9CD1FAC7"
	katT25MACInput = "C402007614281A02" + // step 15
		"2CF362B7BF4311FF3BE1DAA295E8C68DE09050560D19B9E16C2393AE9CD1FAC7"
	katT25Trunc     = "5D0CE20BCD1D06E6" // step 18 CMACt
	katT25RespField = "203BB55D1089D587" // step 20 R-APDU minus the 9100

	// --- rev. 2.0 §5.16.2 Table 26: KeyNo 0x00 changed while authenticated with
	// KeyNo 0x00 (Table 63's "key 0" body).
	katT26Cmd    = 0xC4
	katT26Header = "00"   // step 17's C-APDU carries 00 as the CmdHeader
	katT26Ctr    = 0x0003 // step 9  CmdCtr = 0300

	katT26OldKey = "00000000000000000000000000000000" // step 1
	katT26NewKey = "5004BF991F408672B1EF00F08F9E8647" // step 2
	katT26KeyVer = 0x01                               // step 3
	katT26IV     = "01602D579423B2797BE8B478B0B4D27B" // step 12 IVc

	// step 11 "Plaintext Input (NewKey || KeyVer || Padding)", with the padding
	// of step 10 (800000000000000000000000000000) removed: 17 bytes.
	katT26KeyData = "5004BF991F408672B1EF00F08F9E8647" + "01"

	katT26Ciphertext = "C0EB4DEEFEDDF0B513A03A95A7549181" + // step 13
		"8580503190D4D05053FF75668A01D6FD"
	katT26MACInput = "C403007614281A00" + // step 14
		"C0EB4DEEFEDDF0B513A03A95A75491818580503190D4D05053FF75668A01D6FD"
	katT26Trunc = "A6610234BDED6432" // step 16 CMACt
)

// TestChangeKeyKAT_BodiesMatchAN12196Tables25And26 checks the two KeyData layouts
// against the document alone, before any encryption, so a layout regression is
// reported as a layout regression. Both expectations are the document's own
// step-11 cells with the padding removed.
func TestChangeKeyKAT_BodiesMatchAN12196Tables25And26(t *testing.T) {
	for _, tc := range []struct {
		name           string
		keyNo          byte
		oldKey, newKey string
		keyVer         byte
		want           string
		wantLen        int
	}{
		{"case1_keyno_02", 0x02, katT25OldKey, katT25NewKey, katT25KeyVer, katT25KeyData, changeKeyDataLenOther},
		{"case2_keyno_00", 0x00, katT26OldKey, katT26NewKey, katT26KeyVer, katT26KeyData, changeKeyDataLenSame},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ChangeKeyData(tc.keyNo, hexBytes(t, tc.oldKey), hexBytes(t, tc.newKey), tc.keyVer)
			if err != nil {
				t.Fatalf("ChangeKeyData: %v", err)
			}
			if len(got) != tc.wantLen {
				t.Fatalf("body is %d bytes, want %d (NT4H2421Gx rev. 3.0 §10.6.1 Table 63)", len(got), tc.wantLen)
			}
			if !bytes.Equal(got, hexBytes(t, tc.want)) {
				t.Fatalf("body does not match the published KeyData\n got %X\nwant %s", got, tc.want)
			}
		})
	}

	// Table 25 also publishes the XOR half on its own (step 6) and the CRC on its
	// own (step 7), so the three fields of the 21-byte form are checked against
	// three separate document cells rather than one concatenation.
	case1, err := ChangeKeyData(0x02, hexBytes(t, katT25OldKey), hexBytes(t, katT25NewKey), katT25KeyVer)
	if err != nil {
		t.Fatalf("ChangeKeyData: %v", err)
	}
	if !bytes.Equal(case1[:ev2KeyLen], hexBytes(t, katT25XOR)) {
		t.Fatalf("XOR half does not match Table 25 step 6\n got %X\nwant %s", case1[:ev2KeyLen], katT25XOR)
	}
	if case1[ev2KeyLen] != katT25KeyVer {
		t.Fatalf("version byte is %02X, want %02X (Table 25 step 5)", case1[ev2KeyLen], katT25KeyVer)
	}
	if !bytes.Equal(case1[ev2KeyLen+1:], hexBytes(t, katT25CRC)) {
		t.Fatalf("CRC field does not match Table 25 step 7\n got %X\nwant %s", case1[ev2KeyLen+1:], katT25CRC)
	}
}

// TestChangeKeyKAT_CRC32SpellingIsDiscriminated closes the one half of ADR 0017
// §6 md. 6 that CAN be closed. NT4H2421Gx rev. 3.0 §10.6.1 Table 63 footnote [1]
// says only "computed according to IEEE Std 802.3-2008 (FCS Field) over NewKey",
// which leaves two free choices — final complement or not, and which end of the
// 32-bit word goes first — hence four defensible spellings. AN12196 rev. 2.0
// §5.16.1 Table 25 step 7 publishes ONE of them as a value.
//
// The four candidates below are computed here, from hash/crc32 directly, WITHOUT
// touching crc32NK; the test then requires that exactly one of them equals the
// published constant, that it is the uncomplemented LSB-first one, and only THEN
// that the production function agrees. The document picks the spelling; this file
// does not describe it and hope.
func TestChangeKeyKAT_CRC32SpellingIsDiscriminated(t *testing.T) {
	newKey := hexBytes(t, katT25NewKey)
	want := hexBytes(t, katT25CRC)

	complemented := crc32.ChecksumIEEE(newKey) // the FCS value Go returns
	raw := ^complemented                       // the register before the final XOR

	le := func(v uint32) []byte {
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, v)
		return b
	}
	be := func(v uint32) []byte {
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, v)
		return b
	}

	candidates := []struct {
		name string
		sel  []byte
	}{
		{"raw_lsb_first", le(raw)},
		{"raw_msb_first", be(raw)},
		{"complemented_lsb_first", le(complemented)},
		{"complemented_msb_first", be(complemented)},
	}

	var winners []string
	for _, c := range candidates {
		if bytes.Equal(c.sel, want) {
			winners = append(winners, c.name)
		}
	}
	if len(winners) != 1 {
		t.Fatalf("expected exactly one of the four spellings to reproduce AN12196 "+
			"rev. 2.0 §5.16.1 Table 25 step 7, got %d: %v", len(winners), winners)
	}
	if winners[0] != "raw_lsb_first" {
		t.Fatalf("the published value selects %q, not the uncomplemented LSB-first "+
			"spelling changekey.go implements", winners[0])
	}

	got := crc32NK(newKey)
	if !bytes.Equal(got[:], want) {
		t.Fatalf("crc32NK does not reproduce the published CRC32(NewKey)\n got %X\nwant %s", got, katT25CRC)
	}
}

// TestChangeKeyKAT_IVsMatchAN12196Tables25And26 pins the CommMode.Full IV in the
// ChangeKey session specifically — two counters (0200 and 0300) under one TI, so
// a dropped or mis-ordered counter cannot pass both.
func TestChangeKeyKAT_IVsMatchAN12196Tables25And26(t *testing.T) {
	keyENC := hexBytes(t, katT23KeyENC)
	ti := hexBytes(t, katT19TI)
	for _, tc := range []struct {
		name string
		ctr  uint16
		want string
	}{
		{"table25_ctr_0200", katT25Ctr, katT25IV},
		{"table26_ctr_0300", katT26Ctr, katT26IV},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ev2DataIV(keyENC, ev2LabelCmdIV, ti, tc.ctr)
			if err != nil {
				t.Fatalf("ev2DataIV: %v", err)
			}
			if !bytes.Equal(got, hexBytes(t, tc.want)) {
				t.Fatalf("IV does not match the published value\n got %X\nwant %s", got, tc.want)
			}
		})
	}
}

// TestChangeKeyKAT_IVInputMatchesAN12196Table25 checks the PRE-image of the IV,
// which Table 25 step 9 prints literally (A55A || TI || CmdCtr || zeros) — so a
// label or counter-order regression is caught before the AES call hides it.
func TestChangeKeyKAT_IVInputMatchesAN12196Table25(t *testing.T) {
	want := hexBytes(t, katT25IVInput)
	got := append([]byte{ev2LabelCmdIV[0], ev2LabelCmdIV[1]}, hexBytes(t, katT19TI)...)
	got = binary.LittleEndian.AppendUint16(got, katT25Ctr)
	got = append(got, make([]byte, 8)...)
	if !bytes.Equal(got, want) {
		t.Fatalf("IV pre-image does not match AN12196 rev. 2.0 §5.16.1 Table 25 step 9\n got %X\nwant %s", got, katT25IVInput)
	}
}

// TestChangeKeyKAT_MACInputsMatchAN12196Tables25And26 checks both assembled MAC
// inputs against the strings the document prints in full (steps 15 and 14).
func TestChangeKeyKAT_MACInputsMatchAN12196Tables25And26(t *testing.T) {
	ti := hexBytes(t, katT19TI)
	for _, tc := range []struct {
		name           string
		ctr            uint16
		header, cipher string
		want           string
	}{
		{"table25_case1", katT25Ctr, katT25Header, katT25Ciphertext, katT25MACInput},
		{"table26_case2", katT26Ctr, katT26Header, katT26Ciphertext, katT26MACInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ev2MACInput(changeKeyINS, tc.ctr, ti, hexBytes(t, tc.header), hexBytes(t, tc.cipher))
			if !bytes.Equal(got, hexBytes(t, tc.want)) {
				t.Fatalf("MAC input mismatch\n got %X\nwant %s", got, tc.want)
			}
		})
	}
}

// TestChangeKeyKAT_CommandsMatchAN12196Tables25And26 is the end-to-end one: the
// production entry point is given the key numbers, keys and versions NXP
// published and must emit the exact ISO 7816 data field of each C-APDU —
// CmdHeader || ciphertext || MACt. Nothing in these calls was computed by Tappa.
func TestChangeKeyKAT_CommandsMatchAN12196Tables25And26(t *testing.T) {
	auth := katAuth(t, katT19TI, katT23KeyENC, katT23KeyMAC)
	for _, tc := range []struct {
		name           string
		ctr            uint16
		keyNo          byte
		oldKey, newKey string
		keyVer         byte
		header, cipher string
		trunc          string
	}{
		{"case1_keyno_02", katT25Ctr, 0x02, katT25OldKey, katT25NewKey, katT25KeyVer, katT25Header, katT25Ciphertext, katT25Trunc},
		{"case2_keyno_00", katT26Ctr, 0x00, katT26OldKey, katT26NewKey, katT26KeyVer, katT26Header, katT26Ciphertext, katT26Trunc},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EV2ChangeKeyCommand(auth, tc.ctr, tc.keyNo,
				hexBytes(t, tc.oldKey), hexBytes(t, tc.newKey), tc.keyVer)
			if err != nil {
				t.Fatalf("EV2ChangeKeyCommand: %v", err)
			}
			want := append(append(hexBytes(t, tc.header), hexBytes(t, tc.cipher)...), hexBytes(t, tc.trunc)...)
			if !bytes.Equal(got, want) {
				t.Fatalf("command field does not match the published C-APDU body\n got %X\nwant %X", got, want)
			}
		})
	}
}

// katDerivedNonZeroOldKey / katDerivedXOR are 🔴 DERIVED, NOT TRANSCRIBED — the
// only two values in this file that are, and the reason is ADR 0017 §6 md. 6.
//
// NO PUBLISHED VECTOR EXERCISES THE XOR. Both of AN12196's ChangeKey examples
// change a key whose OLD value is all zeros, so Old ⊕ New == New and an
// implementation that ignored OldKey entirely reproduces both. That is harmless
// on a blank chip (datasheet rev. 3.0 §8.2.4.2: the transport value of all five
// keys is 16 bytes of 00h) and NOT harmless on ADR 0017 §5.3's recovery path,
// where the old key is the one already on the plaque.
//
// HOW THESE WERE DERIVED, so the next reader can redo it rather than trust it:
//   - katDerivedNonZeroOldKey is a synthetic 01..10 byte ramp. It is not from any
//     document; it is chosen to have no zero bytes at all, so EVERY byte of the
//     expected result differs from NewKey.
//   - katDerivedXOR is that ramp XORed with AN12196's published NewKey, computed
//     once by hand outside this package and pasted as a literal. It is deliberately
//     NOT recomputed here with the same expression the implementation uses; a
//     second copy of one expression is what M2-08 was.
//
// THIS IS NOT A PUBLISHED VECTOR AND MUST NEVER BE PRESENTED AS ONE. What it
// pins is narrow and worth stating: that OldKey reaches the output at all, and
// that CRC32NK is computed over NewKey rather than over the XOR result — the
// second of which Table 63 footnote [1] states in words but no vector can show,
// because with an all-zero OldKey the two inputs are the same bytes.
//
// 🔴 ADR 0017 §6 md. 6 IS NOW MEASURED, NOT ONLY ARGUED (2026-08-21). The XOR was
// deleted from ChangeKeyData — `newKey[i]^oldKey[i]` replaced by `newKey[i]` — and
// the package's tests re-run. EXACTLY ONE test went red: this one. Every published
// vector in this file and in ev2_kat_test.go stayed green, including both
// end-to-end ChangeKey C-APDUs. That is the ADR's claim reproduced as an
// experiment rather than repeated as prose, and it is why deleting this test would
// leave the recovery path (ADR 0017 §5.3, where OldKey is ours and is not zero)
// with no coverage at all.
const (
	katDerivedNonZeroOldKey = "0102030405060708090A0B0C0D0E0F10"
	katDerivedXOR           = "F2867E667221EA33C0CEC7090987B676"
)

// TestChangeKey_XORAppliesToANonZeroOldKey exercises the half of Table 63 that no
// published vector can reach. See katDerivedXOR's comment for the provenance of
// the expectation — it is DERIVED, NOT TRANSCRIBED.
func TestChangeKey_XORAppliesToANonZeroOldKey(t *testing.T) {
	newKey := hexBytes(t, katT25NewKey)
	oldKey := hexBytes(t, katDerivedNonZeroOldKey)

	body, err := ChangeKeyData(0x01, oldKey, newKey, 0x01)
	if err != nil {
		t.Fatalf("ChangeKeyData: %v", err)
	}
	if len(body) != changeKeyDataLenOther {
		t.Fatalf("body is %d bytes, want %d", len(body), changeKeyDataLenOther)
	}

	// 1. The XOR actually happened: the first 16 bytes are neither NewKey (which
	//    is what a missing XOR would emit) nor OldKey.
	if bytes.Equal(body[:ev2KeyLen], newKey) {
		t.Fatal("the first 16 bytes equal NewKey with a NON-ZERO OldKey; the XOR of " +
			"NT4H2421Gx rev. 3.0 §10.6.1 Table 63 is not being applied")
	}
	if !bytes.Equal(body[:ev2KeyLen], hexBytes(t, katDerivedXOR)) {
		t.Fatalf("XOR half mismatch\n got %X\nwant %s (DERIVED)", body[:ev2KeyLen], katDerivedXOR)
	}

	// 2. The version byte still sits between the XOR and the CRC.
	if body[ev2KeyLen] != 0x01 {
		t.Fatalf("version byte is %02X, want 01", body[ev2KeyLen])
	}

	// 3. CRC32NK is over NEW KEY, not over the XOR result. Table 63 footnote [1]
	//    says "over NewKey"; with a zero OldKey the two are identical bytes, so
	//    only a non-zero OldKey can tell them apart. The expected value is the
	//    document's own CRC32 from Table 25 step 7 — transcribed, not derived —
	//    which is exactly why NewKey is held fixed across the two tests.
	if !bytes.Equal(body[ev2KeyLen+1:], hexBytes(t, katT25CRC)) {
		t.Fatalf("CRC field is %X, want the published CRC32(NewKey) %s — it must not "+
			"move when OldKey changes", body[ev2KeyLen+1:], katT25CRC)
	}
}

// TestChangeKey_Case2BodyDoesNotDependOnOldKey pins the other side of the same
// split: for key 0, NT4H2421Gx rev. 3.0 §10.6.1 Table 63's body is NewKey ||
// KeyVer with no OldKey term at all, so changing OldKey must change nothing.
// Without this, an implementation that XORed in BOTH cases would still pass the
// published case-2 vector, whose OldKey is zero.
func TestChangeKey_Case2BodyDoesNotDependOnOldKey(t *testing.T) {
	newKey := hexBytes(t, katT26NewKey)
	withZero, err := ChangeKeyData(0x00, hexBytes(t, katT26OldKey), newKey, katT26KeyVer)
	if err != nil {
		t.Fatalf("ChangeKeyData: %v", err)
	}
	withOther, err := ChangeKeyData(0x00, hexBytes(t, katDerivedNonZeroOldKey), newKey, katT26KeyVer)
	if err != nil {
		t.Fatalf("ChangeKeyData: %v", err)
	}
	if !bytes.Equal(withZero, withOther) {
		t.Fatalf("the key-0 body changed when OldKey changed; Table 63's 17-byte "+
			"form has no OldKey term\n zero %X\nother %X", withZero, withOther)
	}
	if !bytes.Equal(withZero, hexBytes(t, katT26KeyData)) {
		t.Fatal("the key-0 body no longer matches AN12196 rev. 2.0 §5.16.2 Table 26 step 11")
	}
}

// TestChangeKey_SwitchesOnKeyNumberNotOnSession is ADR 0017 §5.1's normative
// sentence made mechanical: "kod NUMARAYA bakmalıdır, oturuma değil". Key 0 gets
// the 17-byte form and keys 1..4 the 21-byte form, regardless of anything about
// the session — the two lengths of Table 63 are the observable.
func TestChangeKey_SwitchesOnKeyNumberNotOnSession(t *testing.T) {
	oldKey := hexBytes(t, katDerivedNonZeroOldKey)
	newKey := hexBytes(t, katT25NewKey)
	for keyNo := byte(0); keyNo <= changeKeyMaxKeyNo; keyNo++ {
		body, err := ChangeKeyData(keyNo, oldKey, newKey, 0x01)
		if err != nil {
			t.Fatalf("keyNo %d: %v", keyNo, err)
		}
		want := changeKeyDataLenOther
		if keyNo == 0 {
			want = changeKeyDataLenSame
		}
		if len(body) != want {
			t.Fatalf("keyNo %d produced a %d-byte body, want %d", keyNo, len(body), want)
		}
	}
}

// TestChangeKey_RefusesTheAllZeroTransportKey closes a gap a security audit
// measured on 2026-08-21: ChangeKeyData validated LENGTHS and never VALUES, so a
// 16-byte all-zero newKey produced a body that is flawless under NT4H2421Gx
// rev. 3.0 §10.6.1 Table 63 and that the chip would happily execute.
//
// 🔴 WHY THAT IS THE WORST SHAPE AVAILABLE, not merely a weak key. Datasheet
// rev. 3.0 §8.2.4.2 says the transport value of all five keys IS 16 bytes of 00h,
// so writing it changes nothing while marking the plaque encoded — and ADR 0017
// §5.3's probe 2 ("AuthenticateEV2First with the key from the row") would then
// SUCCEED, reporting a correctly personalised plaque. The half-write diagnosis
// cannot see it. ADR 0005 risk 8's phishing vector, arriving with no detection
// signal.
//
// The gate lives in internal/sun because this is the only layer that knows what
// those sixteen bytes MEAN (CLAUDE.md §3, §4.7).
func TestChangeKey_RefusesTheAllZeroTransportKey(t *testing.T) {
	zeros := make([]byte, ev2KeyLen)
	real := hexBytes(t, katT25NewKey)

	// Both body classes: key 0 (17-byte form) and keys 1..4 (21-byte XOR form).
	for keyNo := byte(0); keyNo <= changeKeyMaxKeyNo; keyNo++ {
		body, err := ChangeKeyData(keyNo, real, zeros, 0x01)
		if err == nil {
			t.Fatalf("keyNo %d: the all-zero transport key was accepted", keyNo)
		}
		if body != nil {
			t.Fatalf("keyNo %d: returned a body alongside an error", keyNo)
		}
		assertNoKeyBytesInMessage(t, err.Error(), real)
	}

	// And through the sealing entry point, so no caller can route around it.
	auth := katAuth(t, katT19TI, katT23KeyENC, katT23KeyMAC)
	if _, err := EV2ChangeKeyCommand(auth, 2, 0x01, real, zeros, 0x01); err == nil {
		t.Fatal("EV2ChangeKeyCommand sealed an all-zero transport key")
	}

	// CONTROL: the gate is about the NEW key only. An all-zero OLD key is the
	// normal case — a blank chip — and must still work, which is exactly what
	// AN12196's two published vectors do.
	if _, err := ChangeKeyData(0x02, zeros, real, 0x01); err != nil {
		t.Fatalf("an all-zero OLD key (a blank chip, the normal case) was rejected: %v", err)
	}
}

// TestChangeKey_RefusesAKeyWriteThatChangesNothing covers the second value gate.
// new == old is accepted by the chip and does nothing, so it cannot corrupt a
// plaque — but it means the caller has lost track of which state the plaque is in,
// and ADR 0017 §5.3 is explicit that recovery must NOT call ChangeKey once probe 2
// has already succeeded. Failing here makes that mistake visible instead of
// returning a silent success.
//
// ⚠️ THIS IS A JUDGEMENT CALL AND IS REVERSIBLE. It is a guard rail, not protocol:
// Table 63 permits the write. It was measured against every legitimate caller in
// the tree first — see the report — and blocks none of them.
func TestChangeKey_RefusesAKeyWriteThatChangesNothing(t *testing.T) {
	same := hexBytes(t, katT25NewKey)
	for keyNo := byte(0); keyNo <= changeKeyMaxKeyNo; keyNo++ {
		body, err := ChangeKeyData(keyNo, same, same, 0x01)
		if err == nil {
			t.Fatalf("keyNo %d: a no-op key write was accepted", keyNo)
		}
		if body != nil {
			t.Fatalf("keyNo %d: returned a body alongside an error", keyNo)
		}
		assertNoKeyBytesInMessage(t, err.Error(), same)
	}

	// CONTROL: one byte of difference is enough to make it a real write again, so
	// the gate is an equality test and not something coarser.
	almost := append([]byte{}, same...)
	almost[len(almost)-1] ^= 0x01
	if _, err := ChangeKeyData(0x01, almost, same, 0x01); err != nil {
		t.Fatalf("keys differing in one bit were rejected: %v", err)
	}
}

// TestChangeKey_RejectsInvalidArguments covers the guard rails: Table 63 allows
// key numbers 0h..4h only, and every key is AES-128. The messages must carry
// lengths, never bytes (CLAUDE.md §4.7) — asserted here so a future "helpful"
// error cannot start printing key material.
// ⚠️ THE KEY-NUMBER CASES USE TWO DIFFERENT KEYS ON PURPOSE (2026-08-21). They
// used to pass the same key as old and new, which meant the value gate added by
// F3 ("new key equals the old key") would ALSO have fired — so both subtests
// stayed green whether or not the key-NUMBER check still existed. They pass today
// only because the number check runs first, which is an ordering accident, not an
// assertion. With distinct keys the number gate is the only one that can fire, so
// deleting it turns these red.
func TestChangeKey_RejectsInvalidArguments(t *testing.T) {
	good := hexBytes(t, katT25NewKey)
	other := hexBytes(t, katT26NewKey) // distinct and non-zero: clears both value gates
	for _, tc := range []struct {
		name           string
		keyNo          byte
		oldKey, newKey []byte
	}{
		{"key_number_above_four", 0x05, other, good},
		{"key_number_far_out", 0xFF, other, good},
		{"short_new_key", 0x01, good, good[:15]},
		{"short_old_key", 0x01, good[:8], good},
		{"nil_old_key", 0x01, nil, good},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ChangeKeyData(tc.keyNo, tc.oldKey, tc.newKey, 0x01)
			if err == nil {
				t.Fatal("expected an error")
			}
			assertNoKeyBytesInMessage(t, err.Error(), good)
		})
	}
}

// assertNoKeyBytesInMessage is the §4.7 leak check used by the error tests: an
// error message may name lengths and fixed strings, never key material, in any
// spelling this package could plausibly produce. Same shape as keys_test.go's
// TestNoPlaintextLeak, applied to the EV2 and ChangeKey error paths.
func assertNoKeyBytesInMessage(t *testing.T, msg string, key []byte) {
	t.Helper()
	h := hex.EncodeToString(key)
	for _, spelling := range []string{h, strings.ToUpper(h), string(key)} {
		if spelling != "" && strings.Contains(msg, spelling) {
			t.Fatalf("error message leaks key material (CLAUDE.md §4.7): %q", msg)
		}
	}
}
