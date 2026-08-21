package sun

import (
	"bytes"
	"strings"
	"testing"
)

// EXTERNALLY ANCHORED TESTS FOR THE COMMAND LAYER (M8-05 FAZ B2b).
//
// 🔴 WHY THIS FILE IS SEPARATE FROM commands_test.go. Same reason
// an12196_kat_test.go is separate from verify_mac_test.go and ev2_kat_test.go from
// ev2_test.go: a value the implementation produced can never be the value the
// implementation is checked against. Every expectation below comes from NXP's
// published worked examples — the constants already transcribed in
// ev2_kat_test.go (katT17*, katT18*) — or is MEASURED out of one published body
// and asserted against another. Nothing here was minted by apdu.go, ndef.go or
// filesettings.go.
//
// 🔴 AND THE ANCHOR THIS ROUND MOST NEEDED DOES NOT EXIST, WHICH IS WHY THE FILE
// IS ARRANGED THE WAY IT IS. There is no published ChangeFileSettings example for
// PLAIN mirroring in either revision of AN12196 (filesettings.go's header says it
// at length, ADR 0017 §6 counts it). So the strategy is: make ONE builder produce
// the ENCRYPTED-PICC configuration the document DOES publish, byte for byte, and
// then note exactly how few bytes separate that from Tappa's. The shared structure
// — field order, both rights compositions, the option bytes, the offset encoding,
// the sealing — is externally pinned by that reproduction. What remains derived is
// listed in filesettings.go and nowhere else, so the list cannot quietly shrink.
//
// SOURCES (revisions spelled out; AN12196 renumbered its chapters):
//
//	rev 1.8                     rev 2.0                what it is
//	§6.8.2 Table 18 p.32        §5.8.2 Table 17 p.29   WriteData, CommMode.Full
//	§6.9   Table 19             §5.9   Table 18        ChangeFileSettings, encrypted PICC
//	§6.5   Table 13 p.27        §5.5   Table 12 p.25   GetVersion, three frames
//
// Both of the first two rows describe the SAME chip in the SAME session, which is
// the fact this file leans on hardest: the offsets in one command must index the
// bytes of the other, so measuring one and asserting the other is a cross-check
// between two independently published byte strings rather than a restatement.

// --- The one published ChangeFileSettings body ---------------------------------

// AN12196 rev. 2.0 §5.9 Table 18 step 3 describes the configuration whose CmdData
// step 7 publishes as katT18CmdData. Transcribed field by field:
//
//	FileOption        40h  — SDM enabled, CommMode plain
//	FileAR.Read       Eh   — free
//	FileAR.Write      0h   — key 0
//	FileAR.ReadWrite  0h   — key 0
//	FileAR.Change     0h   — key 0
//	SDMOptions        C1h  — UID mirror, SDM read counter, ASCII encoding
//	FileAR.SDMMetaRead  2h — a key number, i.e. ENCRYPTED PICCData (this is the
//	                        single value that makes the example not ours)
//	FileAR.SDMFileRead  1h
//	FileAR.SDMCtrRet    1h
//
// ⚠️ SDMCtrRet's VALUE IS READ OFF THE BYTES; ITS POSITION IS NOT A GUESS, AND AN
// EARLIER VERSION OF THIS NOTE SAID IT WAS. The published SDMAccessRights bytes
// are F1 21 with RFU = Fh and SDMMetaRead = 2h, so the remaining 1h could sit in
// either of two nibbles as far as THESE BYTES go — but the documents name them:
// NT4H2421Gx rev. 3.0 §10.7.1 Table 69 (p. 66) gives bits 3-0 as SDMCtrRet, and
// this very step's own text labels the nibbles in wire order ("RFU: 0xF,
// FileAR.SDMCtrRet = 0x1, FileAR.SDMMetaRead: 0x2, FileAR.SDMFileRead: 0x1").
// TestFileSettings_NibblePositionsAreTheOnesTable69Names asserts the layout with
// every nibble distinct, which is what this vector cannot do.
const (
	katT18Read      = ARFree
	katT18Write     = 0x00
	katT18ReadWrite = 0x00
	katT18Change    = 0x00
	katT18MetaRead  = 0x02
	katT18FileRead  = 0x01
	katT18CtrRet    = 0x01
)

// mirrorRunStart returns the file offset of the first character AFTER marker in
// the published WriteData body — i.e. where a mirror placeholder run begins.
//
// This is the measurement half of the cross-check: it reads positions out of
// AN12196 rev. 2.0 §5.8.2 Table 17's body WITHOUT decoding any offset field, so
// the numbers it produces are independent of how Table 18 spells them.
func mirrorRunStart(t *testing.T, body []byte, marker string) uint32 {
	t.Helper()
	i := bytes.Index(body, []byte(marker))
	if i < 0 {
		t.Fatalf("marker %q is not in the published WriteData body; the transcription changed", marker)
	}
	return uint32(i + len(marker))
}

// TestFileSettingsKAT_ReproducesAN12196Table18 is the external anchor of this
// whole round: the production builder is handed the encrypted-PICC configuration
// the document describes and must produce the 15 bytes it publishes.
//
// 🔴 THE TWO OFFSETS ARE MEASURED, NOT COPIED, WHICH IS WHAT MAKES THIS NON-
// CIRCULAR. If the test fed in 32 and 67 by reading them out of Table 18's own
// bytes, it would only be checking that a number survives a round trip through
// appendUint24LE. Instead they are located in Table 17's WriteData body — the
// same chip, same session, different published string — so the assertion binds
// three separate readings at once: that mirror offsets count from the START of
// the file (including the two-byte NLEN), that they are written LSB first, and
// that Table 69's field order is the one being built.
func TestFileSettingsKAT_ReproducesAN12196Table18(t *testing.T) {
	writeBody := hexBytes(t, katT17CmdData)
	piccOffset := mirrorRunStart(t, writeBody, "?e=")
	macOffset := mirrorRunStart(t, writeBody, "&c=")

	got, err := ChangeFileSettingsData(SDMFileSettings{
		FileNo:        NDEFFileNo,
		CommMode:      CommModePlain,
		Read:          katT18Read,
		Write:         katT18Write,
		ReadWrite:     katT18ReadWrite,
		Change:        katT18Change,
		SDMEnabled:    true,
		UIDMirror:     true,
		ReadCtrMirror: true,
		ASCIIEncoding: true,
		SDMMetaRead:   katT18MetaRead,
		SDMFileRead:   katT18FileRead,
		SDMCtrRet:     katT18CtrRet,
		// UIDOffset and ReadCtrOffset are deliberately left at zero: Table 69 says
		// they are ABSENT when SDMMetaRead names a key, and the published body's
		// length proves it — 15 bytes, not 21. If the presence conditions were
		// wrong, these zeros would appear in the output and the assertion would
		// fail loudly rather than silently agreeing.
		PICCDataOffset: piccOffset,
		MACInputOffset: macOffset,
		MACOffset:      macOffset,
	})
	if err != nil {
		t.Fatalf("ChangeFileSettingsData: %v", err)
	}
	if want := hexBytes(t, katT18CmdData); !bytes.Equal(got, want) {
		t.Fatalf("body does not match AN12196 rev. 2.0 §5.9 Table 18 step 7\n got %X\nwant %X", got, want)
	}
}

// TestFileSettingsKAT_SealedCommandMatchesAN12196Table18 runs the same
// configuration through the whole command path — build, encrypt, MAC — and
// requires the published ciphertext and MAC. It is the end-to-end version of the
// test above: if the body were right and the CmdHeader or the command byte wrong,
// only this one would go red.
func TestFileSettingsKAT_SealedCommandMatchesAN12196Table18(t *testing.T) {
	writeBody := hexBytes(t, katT17CmdData)
	auth := katAuth(t, katT14TI, katT14KeyENC, katT14KeyMAC)

	field, err := EV2ChangeFileSettingsCommand(auth, katT18Ctr, SDMFileSettings{
		FileNo:         NDEFFileNo,
		CommMode:       CommModePlain,
		Read:           katT18Read,
		Write:          katT18Write,
		ReadWrite:      katT18ReadWrite,
		Change:         katT18Change,
		SDMEnabled:     true,
		UIDMirror:      true,
		ReadCtrMirror:  true,
		ASCIIEncoding:  true,
		SDMMetaRead:    katT18MetaRead,
		SDMFileRead:    katT18FileRead,
		SDMCtrRet:      katT18CtrRet,
		PICCDataOffset: mirrorRunStart(t, writeBody, "?e="),
		MACInputOffset: mirrorRunStart(t, writeBody, "&c="),
		MACOffset:      mirrorRunStart(t, writeBody, "&c="),
	})
	if err != nil {
		t.Fatalf("EV2ChangeFileSettingsCommand: %v", err)
	}
	want := append(hexBytes(t, katT18Header), hexBytes(t, katT18Ciphertext)...)
	want = append(want, hexBytes(t, katT18Trunc)...)
	if !bytes.Equal(field, want) {
		t.Fatalf("sealed command field does not match Table 18\n got %X\nwant %X", field, want)
	}
}

// TestFileSettingsKAT_OffsetByteOrderIsDiscriminated is the negative half that
// makes the anchor mean something. It writes the same two offsets MSB-first by
// hand and requires the result to differ from the published body — i.e. the
// document really does pin the byte order, and the green test above is not green
// because both spellings happen to agree.
//
// 🔴 THIS IS THE MUTATION M2-08 SHIPPED, IN A DIFFERENT FIELD. A counter went into
// SV2 in the wrong byte order and every test stayed green because every expected
// value had been produced by the same wrong code. Here the expected value is NXP's.
func TestFileSettingsKAT_OffsetByteOrderIsDiscriminated(t *testing.T) {
	writeBody := hexBytes(t, katT17CmdData)
	piccOffset := mirrorRunStart(t, writeBody, "?e=")
	macOffset := mirrorRunStart(t, writeBody, "&c=")
	published := hexBytes(t, katT18CmdData)

	// The body as it would be if the three-byte positions were MSB-first. Built by
	// hand, never through appendUint24LE, so it cannot follow a change to it.
	be := func(v uint32) []byte { return []byte{byte(v >> 16), byte(v >> 8), byte(v)} }
	wrong := []byte{0x40, 0x00, 0xE0, 0xC1, 0xF1, 0x21}
	wrong = append(wrong, be(piccOffset)...)
	wrong = append(wrong, be(macOffset)...)
	wrong = append(wrong, be(macOffset)...)

	if bytes.Equal(wrong, published) {
		t.Fatal("both byte orders assemble to the published body; this vector cannot " +
			"discriminate and proves nothing")
	}
	if len(wrong) != len(published) {
		t.Fatalf("the two spellings differ in LENGTH (%d vs %d), so this compares "+
			"shapes rather than byte order", len(wrong), len(published))
	}
}

// TestNDEFKAT_MirrorOffsetsAreAbsoluteFilePositions states the cross-document
// measurement on its own, so a failure says WHICH reading broke instead of
// surfacing as a wrong body.
//
// Table 17's WriteData body starts at file offset 0 (its CmdHeader is
// 02 000000 800000). Table 18's PICCDataOffset and SDMMACOffset must therefore be
// positions inside that same byte string. Measured: the encrypted-PICC placeholder
// run begins at 32 and the MAC run at 67, and the published offsets decode to
// exactly those. The alternative reading — offsets counted from the NDEF MESSAGE,
// i.e. skipping the two-byte NLEN — would be 30 and 65, and is refuted here.
func TestNDEFKAT_MirrorOffsetsAreAbsoluteFilePositions(t *testing.T) {
	writeBody := hexBytes(t, katT17CmdData)
	settings := hexBytes(t, katT18CmdData)

	// Decode the two published offsets WITHOUT the production helper: bytes 6-8 and
	// 12-14 of a 15-byte body whose first six are FileOption, AccessRights,
	// SDMOptions and SDMAccessRights.
	le := func(b []byte) uint32 { return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 }
	gotPICC, gotMAC := le(settings[6:9]), le(settings[12:15])

	wantPICC := mirrorRunStart(t, writeBody, "?e=")
	wantMAC := mirrorRunStart(t, writeBody, "&c=")

	if gotPICC != wantPICC {
		t.Fatalf("Table 18's PICCDataOffset is %d but Table 17's placeholder run starts at %d", gotPICC, wantPICC)
	}
	if gotMAC != wantMAC {
		t.Fatalf("Table 18's SDMMACOffset is %d but Table 17's placeholder run starts at %d", gotMAC, wantMAC)
	}
	// The refuted alternative, spelled out so it cannot be re-derived by the next
	// reader: nlenLen fewer bytes would be the message-relative reading.
	if gotPICC == wantPICC-nlenLen {
		t.Fatal("the offsets are message-relative after all; ndefPrefixLen is wrong")
	}
	// And the document's own MAC-input configuration is Tappa's (ADR 0003 md. 2):
	// input offset equals MAC offset, i.e. a zero-length MAC input.
	if inputOffset := le(settings[9:12]); inputOffset != gotMAC {
		t.Fatalf("Table 18's SDMMACInputOffset is %d, not equal to its SDMMACOffset %d", inputOffset, gotMAC)
	}
}

// TestAPDU_WriteDataHeaderMatchesAN12196Table17 pins the CmdHeader of WriteData —
// FileNo, offset and length — against the published seven bytes, and with them the
// meaning of the length field.
//
// 🔴 IT ALSO SETTLES A CONTRADICTION IN THE DOCUMENT AND CORRECTS A COMMENT IN
// THIS REPOSITORY. AN12196 prints THREE numbers for one field: steps 4 and 12 show
// the length as 530000 (83, the meaningful NDEF content), the C-APDU of step 15
// shows 800000 (128), and the ciphertext is 144 bytes. ev2_kat_test.go's note used
// to call 800000 "the ENCRYPTED length"; measured here, it is neither 83 nor 144
// but exactly len(CmdData) — the plaintext being written, padding block excluded.
func TestAPDU_WriteDataHeaderMatchesAN12196Table17(t *testing.T) {
	body := hexBytes(t, katT17CmdData)
	auth := katAuth(t, katT14TI, katT14KeyENC, katT14KeyMAC)

	// The three candidate numbers, so the assertion below is visibly a choice.
	if len(body) != 128 {
		t.Fatalf("the published CmdData is %d bytes, not 128; the transcription changed", len(body))
	}
	if got := len(hexBytes(t, katT17Ciphertext)); got != 144 {
		t.Fatalf("the published ciphertext is %d bytes, not 144", got)
	}

	field, err := EV2WriteDataCommand(auth, katT17Ctr, NDEFFileNo, 0, body)
	if err != nil {
		t.Fatalf("EV2WriteDataCommand: %v", err)
	}
	header := hexBytes(t, katT17Header)
	if !bytes.HasPrefix(field, header) {
		t.Fatalf("assembled CmdHeader does not match AN12196 rev. 2.0 §5.8.2 Table 17\n got %X\nwant %X",
			field[:min(len(field), len(header))], header)
	}
	want := append(append([]byte{}, header...), hexBytes(t, katT17Ciphertext)...)
	want = append(want, hexBytes(t, katT17Trunc)...)
	if !bytes.Equal(field, want) {
		t.Fatalf("sealed WriteData field does not match Table 17\n got %X\nwant %X", field, want)
	}
}

// TestAPDU_ThreeByteFieldsAreLSBFirst is the discriminator for appendUint24LE
// itself, on the WriteData header rather than on a settings body — a second,
// independent published string carrying the same encoding.
func TestAPDU_ThreeByteFieldsAreLSBFirst(t *testing.T) {
	published := hexBytes(t, katT17Header)

	// 02 || offset 0 || length 128, MSB-first. By hand.
	wrong := []byte{NDEFFileNo, 0x00, 0x00, 0x00, 0x00, 0x00, 0x80}
	if bytes.Equal(wrong, published) {
		t.Fatal("both spellings of the length assemble to the published header; " +
			"this vector cannot discriminate byte order")
	}
	// And the LSB-first spelling must be the published one.
	got := appendUint24LE(append([]byte{NDEFFileNo}, 0x00, 0x00, 0x00), 128)
	if !bytes.Equal(got, published) {
		t.Fatalf("LSB-first spelling does not reproduce the published header\n got %X\nwant %X", got, published)
	}
}

// --- The two vectors this round shipped WITHOUT, and should not have ------------
//
// 🔴 BOTH OF THESE WERE PUBLISHED THE WHOLE TIME. The first version of apdu.go
// carried "this repository holds no vector for either" over the ISO SELECT
// parameters and "this round could not measure that trailer's exact length" over
// GetVersion's third frame. A third-eye audit with both PDFs open found each in
// one lookup. The bytes were right; what shipped was a pair of labels telling the
// next reader that evidence did not exist.
//
// The two commands they cover are the two that produce tags.uid — a PRIMARY KEY
// (migration 00004) whose collision ADR 0017 §5.2 calls permanent plaque loss. So
// the gap was not decorative: the riskiest identifier in the encode flow was
// anchored by nothing.

const (
	// AN12196 rev. 2.0 §5.3 Table 9 step 10 — the complete ISO SELECT C-APDU for
	// the NDEF application.
	//
	// ⚠️ THE rev. 1.8 COUNTERPART OF §5.3 IS NOT MEASURED HERE AND IS DELIBERATELY
	// NOT GUESSED. an12196_kat_test.go's mapping table now carries the row with the
	// rev 1.8 half marked unmeasured; inventing a table number is exactly what that
	// table exists to prevent ("the table number cannot be derived").
	katT9ISOSelect = "00A4040C07D276000085010100"

	// AN12196 rev. 2.0 §5.5 Table 12 step 9 (rev. 1.8 §6.5 Table 13) — the THIRD
	// GetVersion frame, R-APDU including its status word.
	katT12Frame3RAPDU = "04968CAA5C5E80CD65935D4021189100"
	// The UID at the front of it. NT4H2421Gx rev. 3.0 Table 58 (p. 60) itemises
	// the rest as BatchNo(4) || 1 || 1 || 1, with an optional 1-byte FabKeyID —
	// hence a 14- or 15-byte frame, which is the range ParseGetVersion enforces.
	katT12UID = "04968CAA5C5E80"
)

// TestAPDU_ISOSelectMatchesAN12196Table9 pins the whole SELECT — class, INS, P1,
// P2, Lc, the AID and Le — against the published C-APDU.
func TestAPDU_ISOSelectMatchesAN12196Table9(t *testing.T) {
	got := ISOSelectNDEFApplication()
	if want := hexBytes(t, katT9ISOSelect); !bytes.Equal(got, want) {
		t.Fatalf("ISO SELECT does not match AN12196 rev. 2.0 §5.3 Table 9 step 10\n got %X\nwant %X", got, want)
	}
	// P2 named on its own, because it is the byte the old comment said could not be
	// checked: 0Ch, "no response data", not 00h.
	if got[3] != 0x0C {
		t.Fatalf("P2 is %02X; the published SELECT uses 0C", got[3])
	}
}

// TestGetVersionKAT_ThirdFrameMatchesAN12196Table12 runs the published third frame
// through the real path — split off the status word, then parse — and requires the
// published UID back.
//
// ⚠️ WHAT IS TRANSCRIBED AND WHAT IS NOT, because only one of the three frames is.
// Frames one and two are NOT published in the material available here; the values
// used for them are plausible 7-byte info blocks and nothing asserts them. The
// third frame, the UID, and the fact that the frame is 14 bytes are the document's.
func TestGetVersionKAT_ThirdFrameMatchesAN12196Table12(t *testing.T) {
	data, sw, err := SplitResponse(hexBytes(t, katT12Frame3RAPDU))
	if err != nil {
		t.Fatalf("SplitResponse: %v", err)
	}
	if sw != SWSuccess {
		t.Fatalf("the published third frame ends in %s, expected %s", sw, SWSuccess)
	}
	if len(data) != 14 {
		t.Fatalf("the published third frame carries %d data bytes, want 14", len(data))
	}

	info := hexBytes(t, "04010100011805") // NOT transcribed; see the note above.
	v, err := ParseGetVersion(info, info, data)
	if err != nil {
		t.Fatalf("ParseGetVersion rejected a frame NXP published: %v", err)
	}
	if !bytes.Equal(v.UID, hexBytes(t, katT12UID)) {
		t.Fatalf("UID does not match AN12196 rev. 2.0 §5.5 Table 12 step 9\n got %X\nwant %s", v.UID, katT12UID)
	}
	if v.UIDHex() != katT12UID {
		t.Fatalf("UIDHex is %q, want the upper-case spelling tags.uid stores", v.UIDHex())
	}
	// The trailer is carried through and NOT interpreted: 14 - 7 bytes of it.
	if len(v.Production) != 7 {
		t.Fatalf("production data is %d bytes, want 7", len(v.Production))
	}
}

// TestFileSettings_NibblePositionsAreTheOnesTable69Names is the assertion this
// round first refused to write, on the ground that the document could not settle
// the nibble order. It can, and this is what that costs to write down.
//
// 🔴 DERIVED FROM NAMED LAYOUTS, NOT TRANSCRIBED FROM A VECTOR — the distinction
// matters and the labels are the evidence:
//
//	NT4H2421Gx rev. 3.0 Table 7            AccessRights = ReadWrite | Change | Read | Write
//	NT4H2421Gx rev. 3.0 §10.7.1 Table 69   SDMAccessRights bits 15-12 SDMMetaRead,
//	                                       11-8 SDMFileRead, 7-4 RFU, 3-0 SDMCtrRet
//	AN12196 rev. 2.0 §5.9 Table 18 step 7  labels F121h in wire order as
//	                                       RFU, SDMCtrRet, SDMMetaRead, SDMFileRead
//
// The expected bytes below are those layouts applied to values chosen to be all
// DIFFERENT, which is the whole point: the published vector sets three file rights
// to 0h and two SDM nibbles to 1h, so it cannot discriminate them. A configuration
// where every field differs can.
//
// 🔴 IT CLOSES A MUTATION THAT SURVIVED. Swapping ReadWrite with Change inside
// their shared byte changed no byte any test asserted; it now produces 32E1 where
// the document says 23E1.
func TestFileSettings_NibblePositionsAreTheOnesTable69Names(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		read, write, readWrite, change byte
		metaRead, fileRead, ctrRet     byte
		want                           string
	}{
		{
			// Every one of the seven nibbles a different value.
			name: "all_seven_nibbles_distinct",
			read: ARFree, write: 0x01, readWrite: 0x02, change: 0x03,
			metaRead: ARFree, fileRead: 0x01, ctrRet: 0x02,
			//   40 FileOption
			//   23 (ReadWrite 2 << 4) | Change 3
			//   E1 (Read E << 4) | Write 1
			//   C1 SDMOptions
			//   F2 (RFU F << 4) | SDMCtrRet 2
			//   E1 (SDMMetaRead E << 4) | SDMFileRead 1
			want: "4023E1C1F2E1" + "110000" + "220000" + "440000" + "440000",
		},
		{
			// The published example's SDM nibbles, but with SDMCtrRet moved off
			// SDMFileRead's value — the exact pair the old comment called
			// indistinguishable. It lands in the LOW nibble of the first byte.
			name: "published_sdm_nibbles_with_a_distinct_ctrret",
			read: ARFree, write: 0x00, readWrite: 0x00, change: 0x00,
			metaRead: 0x02, fileRead: 0x01, ctrRet: 0x03,
			want: "4000E0C1F321" + "330000" + "440000" + "440000",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ChangeFileSettingsData(SDMFileSettings{
				FileNo: NDEFFileNo, CommMode: CommModePlain,
				Read: tc.read, Write: tc.write, ReadWrite: tc.readWrite, Change: tc.change,
				SDMEnabled: true, UIDMirror: true, ReadCtrMirror: true, ASCIIEncoding: true,
				SDMMetaRead: tc.metaRead, SDMFileRead: tc.fileRead, SDMCtrRet: tc.ctrRet,
				UIDOffset: 0x11, ReadCtrOffset: 0x22, PICCDataOffset: 0x33,
				MACInputOffset: 0x44, MACOffset: 0x44,
			})
			if err != nil {
				t.Fatalf("ChangeFileSettingsData: %v", err)
			}
			if want := hexBytes(t, tc.want); !bytes.Equal(got, want) {
				t.Fatalf("body\n got %X\nwant %X", got, want)
			}
		})
	}
}

// --- ADR 0017 §5.3 probe 1: GetFileSettings ------------------------------------

const (
	// AN12196 rev. 2.0 §5.4 Table 10 step 4 — the assembled C-APDU for the NDEF
	// file, sent at personalisation step 4, i.e. BEFORE the step-6 authentication.
	// That ordering is the measurement behind "this probe needs no session".
	katT10GetFileSettings = "90F50000010200"

	// The response, assembled from AN12196 rev. 2.0 §5.4 Table 11's FIELD LIST
	// rather than from the R-APDU string printed one page earlier.
	//
	// 🔴 WHY THE FIELD LIST AND NOT THE STRING — the document contradicts itself
	// and only one side is corroborated. Table 10 step 5 prints
	// 004300E0000100C1F1212000004300009100: 18 bytes including the status word,
	// with FileOption 43h and only three offset fields' worth of tail. Table 11 on
	// the next page lists FileType 00, FileOption 40, AccessRights 00E0, FileSize
	// 000100, SDMOptions C1, SDMAccessRights F121 and the offsets — 19 bytes, with
	// FileOption 40h. The tie-breaker is that Table 11's values ARE §5.9 Table 18's
	// CmdData, a string this package reproduces byte for byte
	// (TestFileSettingsKAT_ReproducesAN12196Table18), plus Table 5's 256-byte file
	// size. The printed string matches nothing.
	//
	// ⚠️ Table 11 also MISLABELS its own offsets — it calls them UIDOffset and
	// SDMReadCtrOffset, while its SDMMetaRead is 2h, under which NT4H2421Gx
	// Table 73's presence conditions say those two fields are absent and the three
	// present ones are PICCDataOffset, SDMMACInputOffset and SDMMACOffset. The
	// datasheet is followed; this is recorded because it is the second labelling
	// error in the same two pages.
	katT11FileType = "00"
	katT11FileSize = "000100" // 256d, LSB first — and Table 5 (p. 10) agrees
)

// TestFileSettingsKAT_ProbeOneMatchesAN12196Table10 pins the command of ADR 0017
// §5.3's first probe against the published C-APDU.
func TestFileSettingsKAT_ProbeOneMatchesAN12196Table10(t *testing.T) {
	got, err := GetFileSettingsCommand(NDEFFileNo)
	if err != nil {
		t.Fatalf("GetFileSettingsCommand: %v", err)
	}
	if want := hexBytes(t, katT10GetFileSettings); !bytes.Equal(got, want) {
		t.Fatalf("GetFileSettings C-APDU does not match AN12196 rev. 2.0 §5.4 Table 10 step 4\n got %X\nwant %X", got, want)
	}
}

// TestFileSettingsKAT_ProbeOneParsesBackIntoThePublishedCommandBody is the round
// trip that makes probe 1 usable: a GetFileSettings response describing the
// document's own chip must parse into exactly the settings whose
// ChangeFileSettings body the document publishes.
//
// 🔴 THE TWO ENDS ARE BOTH PUBLISHED AND THE MIDDLE IS OURS. The response is built
// from Table 11's field values; the expected re-encoding is §5.9 Table 18's
// CmdData. If the parser split a field one byte wrong, or applied the presence
// conditions in the other order, the re-encoding would differ from a string NXP
// printed. It also proves the two directions agree about SDMMACInputOffset coming
// before SDMMACOffset, which is the order NT4H2421Gx states twice (Tables 69 and
// 73) and AN12196's step-7 label list states backwards.
func TestFileSettingsKAT_ProbeOneParsesBackIntoThePublishedCommandBody(t *testing.T) {
	cmdData := hexBytes(t, katT18CmdData)
	// Response = FileType || FileOption || AccessRights || FileSize || <SDM tail>,
	// and the SDM tail is exactly the CmdData with its first three bytes removed.
	resp := append(hexBytes(t, katT11FileType), cmdData[:3]...)
	resp = append(resp, hexBytes(t, katT11FileSize)...)
	resp = append(resp, cmdData[3:]...)

	fs, err := ParseFileSettings(NDEFFileNo, resp)
	if err != nil {
		t.Fatalf("ParseFileSettings: %v", err)
	}
	if fs.FileSize != ndefFileSize {
		t.Fatalf("file size parsed as %d, want the %d Table 5 gives the NDEF file", fs.FileSize, ndefFileSize)
	}
	// The named values of Table 11, asserted individually so a failure says which
	// field moved rather than only that the re-encoding differs.
	for _, c := range []struct {
		name string
		got  byte
		want byte
	}{
		{"read", fs.Read, ARFree},
		{"write", fs.Write, 0x00},
		{"readwrite", fs.ReadWrite, 0x00},
		{"change", fs.Change, 0x00},
		{"sdm meta read", fs.SDMMetaRead, katT18MetaRead},
		{"sdm file read", fs.SDMFileRead, katT18FileRead},
		{"sdm counter retrieval", fs.SDMCtrRet, katT18CtrRet},
	} {
		if c.got != c.want {
			t.Errorf("%s parsed as %#x, want %#x", c.name, c.got, c.want)
		}
	}
	if !fs.SDMEnabled || !fs.UIDMirror || !fs.ReadCtrMirror || !fs.ASCIIEncoding {
		t.Fatal("the SDM option bits did not survive the round trip")
	}
	if fs.MACInputOffset != fs.MACOffset {
		t.Fatalf("MAC input offset %d and MAC offset %d differ; the published example sets both to 67",
			fs.MACInputOffset, fs.MACOffset)
	}

	back, err := ChangeFileSettingsData(fs.SDMFileSettings)
	if err != nil {
		t.Fatalf("re-encoding the parsed settings: %v", err)
	}
	if !bytes.Equal(back, cmdData) {
		t.Fatalf("parsed settings do not re-encode to AN12196 rev. 2.0 §5.9 Table 18's CmdData\n got %X\nwant %X", back, cmdData)
	}
}

// TestGetVersionKAT_TheUIDGatesAreTheDocumentsOwnValues checks the two UID
// refusals against the sentences that justify them, and — the half that matters —
// against the one they CANNOT catch.
func TestGetVersionKAT_TheUIDGatesAreTheDocumentsOwnValues(t *testing.T) {
	info := hexBytes(t, "04010100011805")
	tail := hexBytes(t, "CD65935D402118")

	for _, tc := range []struct {
		name, uid string
		wantErr   bool
		why       string
		// says is a fragment the message must carry. It is only set for the
		// all-zero case, and 🔴 A MUTATION IS WHY: an all-zero UID also fails the
		// manufacturer-byte gate (00h is not 04h), so deleting the Random ID check
		// entirely left every test green. The two refusals are not
		// interchangeable — one tells an operator the chip was RECONFIGURED and the
		// other sends them looking at a wrong byte — so the DIAGNOSIS is what is
		// asserted, not merely that an error came back.
		says string
	}{
		// NT4H2421Gx rev. 3.0 Table 58 (p. 60): the UID field is "All zero — if
		// configured for RandomID". Not a broken read: a chip declining to say.
		{"all_zero_means_random_id_is_on", "00000000000000", true,
			"an all-zero UID is Table 58's Random ID state, not an identity", "Random ID"},
		// §8.1 (p. 8): "The first byte of the double size UID is fixed to 04h".
		{"not_an_nxp_manufacturer_byte", "FFFFFFFFFFFFFF", true, "FFh is not 04h", ""},
		{"first_byte_off_by_one", "05968CAA5C5E80", true, "05h is not 04h", ""},
		{"the_published_uid", katT12UID, false, "", ""},
		// 🔴 THE GATE'S OWN LIMIT, ASSERTED SO IT CANNOT BE FORGOTTEN: a hostile
		// relay (ADR 0017 §2.2) does not have to send a malformed UID. This one is
		// a real plaque's, taken from the repository's own seed data, and it is
		// ACCEPTED — correctly, because nothing in an unauthenticated GetVersion
		// response could tell it from the chip in front of us. The remedy is
		// GetCardUIDCommand inside the key-0x01 session (ADR 0017 §6 md. 12).
		{"a_valid_uid_belonging_to_another_chip", "04AC7E55000601", false,
			"defence in depth against a broken chip is not defence against a lying phone", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ParseGetVersion(info, info, append(hexBytes(t, tc.uid), tail...))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("accepted a UID that must be refused: %s", tc.why)
				}
				if v != nil {
					t.Fatal("returned a version alongside an error")
				}
				if tc.says != "" && !strings.Contains(err.Error(), tc.says) {
					t.Fatalf("the refusal does not name %q, so an operator is sent to the wrong "+
						"cause: %v", tc.says, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("refused a well-formed UID: %v", err)
			}
		})
	}
}
