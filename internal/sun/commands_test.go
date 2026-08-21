package sun

import (
	"bytes"
	"net/url"
	"strings"
	"testing"
)

// GUARD RAILS FOR THE COMMAND LAYER (M8-05 FAZ B2b).
//
// commands_kat_test.go proves the bytes against NXP's published examples. This
// file proves the things a document cannot: that malformed input is refused
// rather than half-built, that no error message carries key material (CLAUDE.md
// §4.7), that the mirror offsets this package computes are the ones its OWN
// verifier reads, and that the FileAR decision of ADR 0017 §6 md. 13 is actually
// in the bytes rather than only in a comment.
//
// 🔴 NOTHING HERE IS A KNOWN-ANSWER TEST AGAINST A DOCUMENT, AND NOTHING HERE MAY
// BECOME ONE. Where a body is spelled out below it is Tappa's own configuration,
// for which no published vector exists (filesettings.go), and it is labelled
// DERIVED with its arithmetic written out so a reviewer can redo it by hand. The
// M2-08 rule stands: a value the implementation produced can never be the value
// the implementation is checked against, so these goldens are derived from the
// DECISION, not captured from a run.
//
// 🔴 THE MUTATION LEDGER, BECAUSE A GREEN SUITE IS NOT EVIDENCE THAT IT COULD GO
// RED. 40 single-edit mutations have been applied to apdu.go, ndef.go and
// filesettings.go with the suite re-run against each; 39 turn a test red. The
// count grew in three rounds and each round's survivors are the interesting part:
// 27 mutations / 2 survivors, then five more from the third-eye audit which also
// closed the ReadWrite-Change survivor, then eight more from the security audit —
// of which THREE survived on the first pass and all three were real gaps, closed
// here. The
// list, so the next reader can re-run it rather than trust it: the 3-byte offset
// encoding reversed · the two AccessRights bytes swapped · Read and Write swapped
// inside their byte · the two SDMAccessRights bytes swapped · SDMMetaRead and
// SDMFileRead swapped · the RFU nibble changed · each of the three field-presence
// conditions broken · UIDOffset and SDMReadCtrOffset emitted in the other order ·
// the FileOption SDM bit moved · two SDMOptions bits moved · NLEN written LSB
// first · offsets counted from the NDEF message instead of the file · WriteData's
// offset and length swapped · its length counting the padded body · GetVersion
// accepting a 7-byte third frame · the UID taken from the FIRST frame · the ISO
// SELECT P2 byte · NativeAPDU dropping its trailing Le · the https abbreviation
// code changed to http · the record payload length excluding the prefix byte · a
// mirror offset off by one · the ChangeFileSettings CmdHeader losing its file
// number · the AID's last byte · RFU swapped with SDMCtrRet · and each of the four
// gates the audit added removed in turn (the SDMCtrRet range, WriteData's 248-byte
// data field, the tearing-protected NDEF size, GetVersion's third-frame upper
// bound) · GetFileSettings' INS and its file-number header · the parser reading
// AccessRights nibbles, the file size, or the two MAC offsets the other way round ·
// each of the two UID gates defeated separately.
//
// 🔴 ONE OF THEM FOUND A REAL HOLE AND IT IS THE M2-08 SHAPE EXACTLY. The
// https-to-http mutation SURVIVED the first run, because the assertion read its
// expected value out of the constant being mutated. It is now written as the
// literal the document gives. A mutation run is how that class is found; reading
// is how it is missed.
//
// 🔴 TWO MUTATIONS SURVIVED THAT RUN AND THE ROUND CALLED BOTH OF THEM LEGITIMATE.
// A third-eye audit with the PDFs open showed ONE OF THEM WAS AN ESCAPE, and the
// difference is worth keeping because it is the difference between "no evidence
// exists" and "I did not look":
//
//   - ReadWrite swapped with Change — NOT legitimate, and now RED. NT4H2421Gx rev. 3.0 Table 7
//     names the AccessRights layout and AN12196's Table 18 step 7 labels its own
//     nibbles, so a document-derived expectation closes it. The survivor existed
//     only because no test configured those fields with DIFFERENT values.
//     TestFileSettings_NibblePositionsAreTheOnesTable69Names does, and the
//     mutation is now red. The test that used to bless this survivor
//     ("…AreHarmless", which asserted permutations changed nothing) has been
//     DELETED rather than reworded: its premise was the false claim.
//   - SDMMACInputOffset swapped with SDMMACOffset — still the one survivor, but
//     the REASON given here was wrong and a later round with the PDFs open said so.
//     It claimed no published source arbitrates. NT4H2421Gx does, twice: Table 69
//     for the command body and Table 73 for the GetFileSettings response, both
//     input-first. AN12196 §5.9 Table 18 step 7's label list is simply backwards
//     (it mislabels the offsets on the neighbouring page too). The mutation
//     survives because the BUILDER refuses every body where the two differ (ADR
//     0003 md. 2 — a zero-length MAC input), not because the order is unknown; the
//     PARSER, which has no such gate, is pinned by
//     TestFileSettings_ParseRejectsResponsesItCannotDescribe.
//
// 🔴 AND THE THREE THAT SURVIVED THE SECURITY AUDIT'S ROUND WERE ALL THE SAME
// SHAPE, WHICH IS THE ONE M2-08 WAS: a published value that cannot tell two
// readings apart. The NDEF file size is 000100 — 256 whichever end you read it
// from, a palindrome exactly like the counter that shipped wrong for four months;
// every published MAC-offset pair is equal; and an all-zero UID trips the
// manufacturer-byte gate before the Random ID gate can speak. Each is closed with
// a case whose values BREAK the symmetry: a 32-byte file, two different offsets,
// and an assertion on which diagnosis comes back.

// --- ISO 7816 framing ----------------------------------------------------------

// TestAPDU_NativeFramingIsTheWrappedNativeShape covers both forms of the envelope
// and the one refusal, in a table.
func TestAPDU_NativeFramingIsTheWrappedNativeShape(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ins     byte
		field   []byte
		want    string
		wantErr bool
	}{
		{"no_data_field", INSGetVersion, nil, "9060000000", false},
		{"empty_data_field", INSAdditionalFrame, []byte{}, "90AF000000", false},
		{"one_byte_field", INSChangeFileSettings, []byte{0x02}, "905F0000010200", false},
		{"max_length_field", INSWriteData, bytes.Repeat([]byte{0xAA}, 255),
			"908D0000FF" + strings.Repeat("AA", 255) + "00", false},
		{"one_byte_too_long", INSWriteData, bytes.Repeat([]byte{0xAA}, 256), "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NativeAPDU(tc.ins, tc.field)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				if got != nil {
					t.Fatal("returned an APDU alongside an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("NativeAPDU: %v", err)
			}
			if !bytes.Equal(got, hexBytes(t, tc.want)) {
				t.Fatalf("APDU\n got %X\nwant %s", got, tc.want)
			}
		})
	}
}

// TestAPDU_ISOSelectAddressesTheNDEFApplication pins the one command that does not
// use the wrapped-native envelope: a real ISO 7816-4 SELECT by DF name.
func TestAPDU_ISOSelectAddressesTheNDEFApplication(t *testing.T) {
	got := ISOSelectNDEFApplication()
	want := hexBytes(t, "00A4040C07D276000085010100")
	if !bytes.Equal(got, want) {
		t.Fatalf("ISO SELECT\n got %X\nwant %X", got, want)
	}
	// The AID is the NFC Forum's, not ours to invent; assert it separately so a
	// change to it is reported as a change to it.
	if !bytes.Contains(got, ndefApplicationAID) || len(ndefApplicationAID) != 7 {
		t.Fatal("the NDEF Tag Application identifier is not the 7-byte D2760000850101")
	}
	// The class byte MUST NOT be the wrapped-native 90h here: this command's
	// success trailer is 9000h, and a caller checking for 9100h on a 90h-class
	// SELECT would abandon a healthy chip.
	if got[0] == apduCLA {
		t.Fatal("ISO SELECT is being framed as a wrapped native command")
	}
}

// TestAPDU_SplitResponseSeparatesTheStatusWord covers the R-APDU split, including
// the two-byte-only frame that every successful ChangeKey case 2 produces.
func TestAPDU_SplitResponseSeparatesTheStatusWord(t *testing.T) {
	for _, tc := range []struct {
		name     string
		resp     string
		wantData string
		wantSW   StatusWord
		wantErr  bool
	}{
		{"status_only", "9100", "", SWSuccess, false},
		{"additional_frame", "0401010001180591AF", "04010100011805", SWAdditionalFrame, false},
		{"iso_select_ok", "9000", "", SWISOSuccess, false},
		{"data_then_status", "AABBCC9100", "AABBCC", SWSuccess, false},
		{"one_byte", "91", "", 0, true},
		{"empty", "", "", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, sw, err := SplitResponse(hexBytes(t, tc.resp))
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("SplitResponse: %v", err)
			}
			if !bytes.Equal(data, hexBytes(t, tc.wantData)) {
				t.Fatalf("data\n got %X\nwant %s", data, tc.wantData)
			}
			if sw != tc.wantSW {
				t.Fatalf("status word %s, want %s", sw, tc.wantSW)
			}
		})
	}
}

// TestAPDU_RequireStatusReportsTheMismatch checks the small helper callers use to
// abandon a session, and that its message names both status words — a return code
// is diagnostic, not secret (apdu.go).
func TestAPDU_RequireStatusReportsTheMismatch(t *testing.T) {
	if err := RequireStatus(SWSuccess, SWSuccess); err != nil {
		t.Fatalf("a matching status word errored: %v", err)
	}
	err := RequireStatus(SWAdditionalFrame, SWSuccess)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"91AF", "9100"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not name %s", err, want)
		}
	}
}

// --- GetVersion ----------------------------------------------------------------

// TestGetVersion_ThreeCommandsAndAUIDFromTheThirdFrame is the positive case:
// exactly three C-APDUs, and the UID taken from the front of the last frame.
func TestGetVersion_ThreeCommandsAndAUIDFromTheThirdFrame(t *testing.T) {
	cmds := GetVersionCommands()
	if len(cmds) != 3 {
		t.Fatalf("GetVersion issues %d commands, want 3 (NT4H2421Gx rev. 3.0 §10.5.2)", len(cmds))
	}
	if !bytes.Equal(cmds[0], hexBytes(t, "9060000000")) {
		t.Fatalf("first command %X, want 9060000000", cmds[0])
	}
	for i := 1; i < 3; i++ {
		if !bytes.Equal(cmds[i], hexBytes(t, "90AF000000")) {
			t.Fatalf("command %d is %X, want the AF continuation 90AF000000", i+1, cmds[i])
		}
	}
	// The three commands must not alias one backing array: a relay that rewrites a
	// buffer in place would otherwise corrupt the others.
	cmds[1][1] = 0x00
	if cmds[2][1] != INSAdditionalFrame {
		t.Fatal("the two continuation commands share a backing array")
	}

	// A plausible third frame: the 7-byte UID, then production data (batch number,
	// week and year). Its VALUES are invented — no vector for GetVersion's third
	// frame is transcribed anywhere in this repository — and nothing asserts them;
	// what is asserted is that the UID comes off the front and the rest is carried
	// through untouched.
	v, err := ParseGetVersion(
		hexBytes(t, "04010100011805"),
		hexBytes(t, "04010101011805"),
		hexBytes(t, "04DE5F1EACC040"+"C58E1A5F13"+"19"+"25"),
	)
	if err != nil {
		t.Fatalf("ParseGetVersion: %v", err)
	}
	if !bytes.Equal(v.UID, hexBytes(t, "04DE5F1EACC040")) {
		t.Fatalf("UID %X, want 04DE5F1EACC040", v.UID)
	}
	// The spelling tags.uid stores (migration 00013's ^[0-9A-F]{14}$).
	if got := v.UIDHex(); got != "04DE5F1EACC040" {
		t.Fatalf("UIDHex %q, want the 14 upper-case hex characters tags.uid holds", got)
	}
	if len(v.UIDHex()) != uidHexLen {
		t.Fatalf("UIDHex is %d characters, want %d", len(v.UIDHex()), uidHexLen)
	}
	if !bytes.Equal(v.Production, hexBytes(t, "C58E1A5F131925")) {
		t.Fatalf("production data %X was not carried through verbatim", v.Production)
	}
	if (*Version)(nil).UIDHex() != "" {
		t.Fatal("UIDHex on a nil version must not panic or invent a value")
	}
}

// TestGetVersion_RefusesFramesThatWouldPoisonThePrimaryKey is the gate that
// matters most in this file.
//
// 🔴 THE FAILURE IT EXISTS FOR IS NOT A CRASH. GetVersion answers three frames and
// the first two are seven bytes of vendor and version data — the SAME for every
// chip NXP ships. A flow that read a "UID" out of frame one would write one
// identical value into tags.uid for every plaque; the column is a PRIMARY KEY
// (migration 00004), so the second plaque would collide, and ADR 0017 §5.2 calls
// a chip whose key is not in a row permanent plaque loss. Requiring all three
// frames, with the first two at exactly the info length and the third longer than
// one, makes that mistake unrepresentable rather than merely unlikely.
func TestGetVersion_RefusesFramesThatWouldPoisonThePrimaryKey(t *testing.T) {
	info := hexBytes(t, "04010100011805")
	third := hexBytes(t, "04DE5F1EACC040C58E1A5F131925")

	for _, tc := range []struct {
		name       string
		f1, f2, f3 []byte
		explains   string
	}{
		{"info_frame_handed_in_as_the_third", info, info, info,
			"a 7-byte hardware-info block would become a UID shared by every plaque"},
		{"third_frame_truncated_to_a_uid", info, info, third[:7],
			"a frame carrying only 7 bytes cannot be told apart from an info block"},
		// The upper bound arrived with the vector: AN12196 rev. 2.0 §5.5 Table 12
		// step 9 prints 14 data bytes and NT4H2421Gx Table 58 allows a 15th
		// (FabKeyID). Sixteen is not this command's third frame, so taking a UID
		// off its front would be a guess.
		{"third_frame_longer_than_the_document_allows", info, info,
			append(append([]byte{}, third...), 0x00, 0x00),
			"a 16-byte frame is not GetVersion's third frame"},
		{"first_frame_short", info[:6], info, third, "a short frame means the exchange desynchronised"},
		{"first_frame_long", append(append([]byte{}, info...), 0x00), info, third, "idem"},
		{"second_frame_missing", info, nil, third, "idem"},
		{"third_frame_empty", info, info, nil, "idem"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ParseGetVersion(tc.f1, tc.f2, tc.f3)
			if err == nil {
				t.Fatalf("accepted frames that must be refused: %s", tc.explains)
			}
			if v != nil {
				t.Fatal("returned a version alongside an error")
			}
		})
	}
}

// --- AuthenticateEV2First frames -----------------------------------------------

// TestAPDU_AuthenticateFramesMatchThePublishedShape checks both halves of the
// handshake envelope against the shape AN12196 rev. 2.0 §5.6 Table 14 sends.
func TestAPDU_AuthenticateFramesMatchThePublishedShape(t *testing.T) {
	first, err := AuthenticateEV2FirstCommand(0x00)
	if err != nil {
		t.Fatalf("AuthenticateEV2FirstCommand: %v", err)
	}
	if want := hexBytes(t, "9071000002000000"); !bytes.Equal(first, want) {
		t.Fatalf("part 1 command\n got %X\nwant %X", first, want)
	}
	// ADR 0017 §5.3 probe 2 authenticates with key 0x01, so the key number must
	// really reach the wire rather than being a fixed zero.
	probe, err := AuthenticateEV2FirstCommand(SDMFileReadKeyNo)
	if err != nil {
		t.Fatalf("AuthenticateEV2FirstCommand: %v", err)
	}
	if want := hexBytes(t, "9071000002010000"); !bytes.Equal(probe, want) {
		t.Fatalf("part 1 command for key 1\n got %X\nwant %X", probe, want)
	}

	part2, err := AuthenticateEV2FirstPart2Command(hexBytes(t, katT14Part2Cmd))
	if err != nil {
		t.Fatalf("AuthenticateEV2FirstPart2Command: %v", err)
	}
	want := append(hexBytes(t, "90AF000020"), hexBytes(t, katT14Part2Cmd)...)
	want = append(want, 0x00)
	if !bytes.Equal(part2, want) {
		t.Fatalf("part 2 command\n got %X\nwant %X", part2, want)
	}
}

// TestAPDU_CommandBuildersRejectMalformedInput covers every length and range gate
// in apdu.go in one table, and asserts none of the messages carries the bytes it
// was handed (CLAUDE.md §4.7).
func TestAPDU_CommandBuildersRejectMalformedInput(t *testing.T) {
	auth := katAuth(t, katT14TI, katT14KeyENC, katT14KeyMAC)
	cryptogram := hexBytes(t, katT14Part2Cmd)

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"auth_key_number_out_of_range", func() error {
			_, err := AuthenticateEV2FirstCommand(0x05)
			return err
		}},
		{"auth_part2_short", func() error {
			_, err := AuthenticateEV2FirstPart2Command(cryptogram[:16])
			return err
		}},
		{"auth_part2_nil", func() error {
			_, err := AuthenticateEV2FirstPart2Command(nil)
			return err
		}},
		{"writedata_empty_body", func() error {
			_, err := EV2WriteDataCommand(auth, 0, NDEFFileNo, 0, nil)
			return err
		}},
		{"writedata_offset_too_big", func() error {
			_, err := EV2WriteDataCommand(auth, 0, NDEFFileNo, maxUint24+1, []byte{0x01})
			return err
		}},
		{"writedata_no_session", func() error {
			_, err := EV2WriteDataCommand(nil, 0, NDEFFileNo, 0, []byte{0x01})
			return err
		}},
		{"native_apdu_field_too_long", func() error {
			_, err := NativeAPDU(INSWriteData, bytes.Repeat([]byte{0x01}, 256))
			return err
		}},
		// 🔴 THE CEILING AN AUDIT FOUND MISSING. Table 81 caps WriteData's data
		// field at 248 bytes "including secure messaging", which is TIGHTER than
		// the 255 a short Lc can express — so checking the ISO envelope alone let
		// a body through that the chip answers with LENGTH_ERROR, in the middle of
		// an encode session. 224 plaintext bytes seal into 224+16 padding, plus a
		// 7-byte header and an 8-byte MAC: 255.
		{"writedata_body_over_the_command_ceiling", func() error {
			_, err := EV2WriteDataCommand(auth, 0, NDEFFileNo, 0, bytes.Repeat([]byte{0xAA}, 224))
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatal("expected an error")
			}
			assertNoKeyBytesInMessage(t, err.Error(), cryptogram)
			assertNoKeyBytesInMessage(t, err.Error(), auth.KeyENC)
			assertNoKeyBytesInMessage(t, err.Error(), auth.KeyMAC)
		})
	}

	// The largest body that DOES fit must still build, or the ceiling is a smaller
	// number with no argument behind it.
	//
	// ⚠️ THE CEILING IS NEVER HIT EXACTLY AND THAT IS ARITHMETIC, NOT SLACK. The
	// field is 7 header + padded + 8 MAC, and padded is a multiple of 16, so the
	// field can only be 15 more than a multiple of 16: 239, then 255. 248 sits
	// between them. 223 plaintext bytes give 239; 224 give 255 and are refused.
	t.Run("writedata_at_the_ceiling_still_builds", func(t *testing.T) {
		field, err := EV2WriteDataCommand(auth, 0, NDEFFileNo, 0, bytes.Repeat([]byte{0xAA}, 223))
		if err != nil {
			t.Fatalf("the largest body Table 81 allows was refused: %v", err)
		}
		if len(field) > writeDataMaxDataField {
			t.Fatalf("the ceiling body seals into %d bytes, over the %d limit", len(field), writeDataMaxDataField)
		}
		if _, err := EV2WriteDataCommand(auth, 0, NDEFFileNo, 0, bytes.Repeat([]byte{0xAA}, 224)); err == nil {
			t.Fatal("one byte more than the largest writable body was accepted")
		}
	})
}

// --- The NDEF template ---------------------------------------------------------

// TestNDEF_TemplateIsTheADR0003URLWithMirrorRuns walks the template's shape for
// three different hosts, so nothing below can be right only for one length.
func TestNDEF_TemplateIsTheADR0003URLWithMirrorRuns(t *testing.T) {
	for _, tc := range []struct {
		name, base string
		wantUID    uint32
	}{
		// ndefPrefixLen(7) + len(rest) + len("?tag=")(5)
		{"short_host", "https://a/t", 7 + 3 + 5},
		{"realistic_host", "https://tap.example.com/t", 7 + 17 + 5},
		{"long_path", "https://tap.example.com/encode/tap/t", 7 + 28 + 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tpl, err := BuildTapNDEF(tc.base)
			if err != nil {
				t.Fatalf("BuildTapNDEF: %v", err)
			}
			if tpl.UIDOffset != tc.wantUID {
				t.Fatalf("UID offset %d, want %d", tpl.UIDOffset, tc.wantUID)
			}
			// The other two follow from the parameter order ADR 0003 md. 1 fixes.
			if want := tc.wantUID + uidHexLen + 5; tpl.ReadCtrOffset != want {
				t.Fatalf("read counter offset %d, want %d", tpl.ReadCtrOffset, want)
			}
			if want := tpl.ReadCtrOffset + ctrHexLen + 6; tpl.MACOffset != want {
				t.Fatalf("MAC offset %d, want %d", tpl.MACOffset, want)
			}
			// NLEN is the record length, big-endian, and the file is exactly the
			// record plus that prefix.
			nlen := int(tpl.File[0])<<8 | int(tpl.File[1])
			if nlen+nlenLen != len(tpl.File) {
				t.Fatalf("NLEN says %d bytes of record but the file is %d", nlen, len(tpl.File))
			}
			if got := tpl.File[2:6]; !bytes.Equal(got, []byte{ndefRecordD1, ndefTypeLen, byte(nlen - ndefURIRecordHeaderLen), ndefTypeU}) {
				t.Fatalf("record header %X is not D1 01 <payload len> 55", got)
			}
			// 🔴 THE LITERAL 04 IS DELIBERATE AND A MUTATION RUN IS WHY. This line
			// used to compare against ndefURIPrefixHTTPS, which meant changing the
			// constant to 03h ("http://") changed the expectation with it and NO test
			// went red — a plaque that sends an employee's session cookie in the
			// clear, shipped green. That is the M2-08 shape exactly: the value under
			// test was also the value it was tested against. 04h is NFC Forum URI RTD
			// 1.0 §3.2.2 Table 3's code for https://, written out here so the
			// document is the expectation.
			if tpl.File[6] != 0x04 {
				t.Fatalf("URI abbreviation code %02X, want 04 (https://). 03 is http:// "+
					"and would put a tap's session cookie on the wire in the clear", tpl.File[6])
			}
			if !strings.HasPrefix(tpl.URL, "https://") {
				t.Fatalf("template URL %q is not https", tpl.URL)
			}
			if tpl.URL != tc.base+"?tag="+strings.Repeat("0", uidHexLen)+
				"&ctr="+strings.Repeat("0", ctrHexLen)+
				"&cmac="+strings.Repeat("0", cmacHexLen) {
				t.Fatalf("template URL is not the ADR 0003 shape: %s", tpl.URL)
			}
		})
	}
}

// TestNDEF_TheTearingProtectedBudgetIsReallySixtyNineBytes holds the number
// ndefTearingProtectedWrite's comment promises. A limit whose budget nobody
// measured is a limit that quietly becomes wrong when a neighbouring constant
// moves — the parameter names, the prefix length or the scheme.
func TestNDEF_TheTearingProtectedBudgetIsReallySixtyNineBytes(t *testing.T) {
	tpl, err := BuildTapNDEF("https://" + strings.Repeat("a", 69))
	if err != nil {
		t.Fatalf("the documented 69-byte host-and-path budget does not fit: %v", err)
	}
	if len(tpl.File) != ndefTearingProtectedWrite {
		t.Fatalf("a 69-byte host builds a %d-byte file; the budget is not %d bytes",
			len(tpl.File), ndefTearingProtectedWrite)
	}
}

// TestNDEF_RefusesBasesThatCannotBecomeAPlaque is the fail-closed half. Every case
// here would, if accepted, produce a plaque whose only repair is physical
// replacement.
func TestNDEF_RefusesBasesThatCannotBecomeAPlaque(t *testing.T) {
	for _, tc := range []struct{ name, base, why string }{
		{"cleartext_scheme", "http://tap.example.com/t",
			"an employee's session cookie would travel in the clear"},
		{"no_scheme", "tap.example.com/t", "idem"},
		{"scheme_only", "https://", "there is no host to tap"},
		{"already_has_a_query", "https://tap.example.com/t?x=1",
			"our three parameters would sit behind somebody else's"},
		{"has_a_fragment", "https://tap.example.com/t#x", "idem"},
		{"has_an_ampersand", "https://tap.example.com/t&x", "idem"},
		{"space_in_host", "https://tap example.com/t",
			"a byte that is not one printable character shifts every mirror"},
		{"non_ascii", "https://tap.exämple.com/t", "idem"},
		{"too_long_for_a_short_record", "https://" + strings.Repeat("a", 300) + "/t",
			"a short NDEF record declares its payload length in one byte"},
		// 198 host bytes plus the 52 the three parameters add is a 250-byte URI, so
		// the payload (251) still fits a short record but the file (257) does not.
		// The two ceilings are separate and both are load-bearing.
		{"fits_a_short_record_but_not_the_file", "https://" + strings.Repeat("a", 198),
			"the record would not fit the 256-byte NDEF file"},
		// 🔴 THE TEARING BOUNDARY (datasheet §8.2.3.1). 70 host bytes puts the file
		// at 129 — one over the largest single write the chip protects against
		// tearing, and a torn write leaves exactly the half-written NDEF that ADR
		// 0017 §5.3's probes cannot tell from an untouched chip.
		{"one_byte_past_the_tearing_protected_write", "https://" + strings.Repeat("a", 70),
			"a torn write would leave a partial NDEF nothing can diagnose"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tpl, err := BuildTapNDEF(tc.base)
			if err == nil {
				t.Fatalf("accepted a base URL that must be refused: %s", tc.why)
			}
			if tpl != nil {
				t.Fatal("returned a template alongside an error")
			}
		})
	}
}

// TestNDEF_ChipMirrorRoundTripsThroughOurOwnVerifier is this round's INTERNAL
// consistency anchor, and it is labelled that way on purpose.
//
// 🔴 WHAT IT PROVES: that the three offsets filesettings.go hands the chip point
// at exactly the characters params.go later reads back, and that a MAC computed
// over the mirrored values verifies through this package's own verify_mac.go. It
// simulates the chip by stamping NXP's published UID, counter and SDM MAC (AN12196
// rev. 1.8 §4.4.4.2.1 Table 5 — the values an12196_kat_test.go already
// transcribes) into the template AT THOSE OFFSETS, then parses the URL out of the
// raw file bytes.
//
// 🔴 WHAT IT DOES NOT PROVE, said plainly: that the CHIP agrees. The offsets are
// our reading of Table 69, and no published example configures plain mirroring.
// Encode and decode agreeing with each other is exactly the shape of evidence
// M2-08 shipped for four months. The external half of the evidence is
// commands_kat_test.go; this is the half that would catch an offset that is
// internally inconsistent, and silicon is still the arbiter (ADR 0017 §6 md. 1).
func TestNDEF_ChipMirrorRoundTripsThroughOurOwnVerifier(t *testing.T) {
	for _, tc := range []struct{ name, base, uid, ctr, mac string }{
		{"table5_values_upper", "https://tap.example.com/t", katT5UID, katT5URLCtr, katT5SDMMAC},
		{"table5_values_lower", "https://t.example/t",
			strings.ToLower(katT5UID), katT5URLCtr, strings.ToLower(katT5SDMMAC)},
		{"table2_values", "https://ntag.example.com/424", katT2UID, katT2URLCtr, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tpl, err := BuildTapNDEF(tc.base)
			if err != nil {
				t.Fatalf("BuildTapNDEF: %v", err)
			}
			file := append([]byte(nil), tpl.File...)

			// The chip's job: stamp ASCII hex over the three placeholder runs.
			mac := tc.mac
			if mac == "" {
				mac = strings.Repeat("A", cmacHexLen)
			}
			copy(file[tpl.UIDOffset:], tc.uid)
			copy(file[tpl.ReadCtrOffset:], tc.ctr)
			copy(file[tpl.MACOffset:], mac)

			// Read the URI back out of the FILE, not out of tpl.URL: the point is the
			// offsets, so the text has to come from the bytes they indexed.
			payloadLen := int(file[4])
			uri := ndefHTTPSScheme + string(file[ndefPrefixLen:ndefPrefixLen+payloadLen-1])
			u, err := url.Parse(uri)
			if err != nil {
				t.Fatalf("the mirrored file is not a parsable URL (%q): %v", uri, err)
			}
			p, err := Parse(u.Query())
			if err != nil {
				t.Fatalf("our own parser rejected a URL our own offsets produced: %v", err)
			}
			if p.Channel != ChannelNFC || !p.HasSUN() {
				t.Fatal("the mirrored URL did not parse as a full SUN tap")
			}
			if got := p.UID; got != strings.ToUpper(tc.uid) {
				t.Fatalf("UID round-tripped as %q, want %q", got, strings.ToUpper(tc.uid))
			}
			if got := strings.ToUpper(string(file[tpl.ReadCtrOffset : int(tpl.ReadCtrOffset)+ctrHexLen])); got != strings.ToUpper(tc.ctr) {
				t.Fatalf("counter round-tripped as %q, want %q", got, tc.ctr)
			}

			if tc.mac == "" {
				return
			}
			// And the published SDM MAC verifies over the values that came back out.
			ok, err := verifyMAC(hexBytes(t, katT5Key), p.UIDBytes, p.CtrBytes[:], p.CMAC)
			if err != nil {
				t.Fatalf("verifyMAC: %v", err)
			}
			if !ok {
				t.Fatal("the published SDM MAC did not verify after a round trip through " +
					"the template's own mirror offsets")
			}
		})
	}
}

// --- The file settings Tappa actually sends ------------------------------------

// tappaBodyForKnownTemplate is Tappa's plain-SDM ChangeFileSettings body for one
// specific base URL.
//
// 🔴 DERIVED, NOT TRANSCRIBED — no published vector configures plain mirroring
// (filesettings.go). The arithmetic is written out so a reviewer can redo it
// rather than trust it, for base "https://tap.example.com/t":
//
//	rest              = "tap.example.com/t"                    17 bytes
//	UIDOffset         = 7 + 17 + len("?tag=")   = 29 = 1Dh
//	ReadCtrOffset     = 29 + 14 + len("&ctr=")  = 48 = 30h
//	MACOffset         = 48 +  6 + len("&cmac=") = 60 = 3Ch
//
//	40      FileOption      SDM enabled (bit 6), CommMode plain
//	11      AccessRights    (ReadWrite 1h << 4) | Change 1h
//	E1                      (Read Eh << 4) | Write 1h
//	C1      SDMOptions      UID mirror, read counter mirror, ASCII — the SAME byte
//	                        the published example carries
//	F1      SDMAccessRights (RFU Fh << 4) | SDMCtrRet 1h
//	E1                      (SDMMetaRead Eh << 4) | SDMFileRead 1h
//	1D0000  UIDOffset       29, LSB first
//	300000  SDMReadCtrOffset 48
//	3C0000  SDMMACInputOffset 60   — equal to the next field: zero-length MAC input
//	3C0000  SDMMACOffset     60
const tappaBodyForKnownTemplate = "4011E1C1F1E11D0000300000" + "3C0000" + "3C0000"

// TestFileSettings_TappaBodyIsTheDecidedConfiguration is the byte-level statement
// of the decision that closes ADR 0017 §6 md. 13. If somebody later changes an
// access right, this is the test that says so.
func TestFileSettings_TappaBodyIsTheDecidedConfiguration(t *testing.T) {
	tpl, err := BuildTapNDEF("https://tap.example.com/t")
	if err != nil {
		t.Fatalf("BuildTapNDEF: %v", err)
	}
	s, err := TappaNDEFSettings(tpl)
	if err != nil {
		t.Fatalf("TappaNDEFSettings: %v", err)
	}

	// The decision, asserted as values before it is asserted as bytes, so a failure
	// names the right rather than an offset.
	if s.Read != ARFree {
		t.Fatal("the NDEF file is not freely readable; no anonymous browser could tap it")
	}
	for _, r := range []struct {
		name string
		v    byte
	}{{"write", s.Write}, {"readwrite", s.ReadWrite}, {"change", s.Change}} {
		if r.v != SDMFileReadKeyNo {
			t.Fatalf("%s right is %#x, want the per-plaque key %#x (ADR 0017 §6 md. 13, "+
				"decided 2026-08-21). Key 0 is the PUBLIC factory default until §6 md. 5 "+
				"lands, and Eh would leave the phishing vector of ADR 0005 risk 8 open",
				r.name, r.v, SDMFileReadKeyNo)
		}
	}
	if s.MACInputOffset != s.MACOffset {
		t.Fatal("the SDM MAC input is not zero-length (ADR 0003 md. 2)")
	}
	// SDMCtrRet is a DECISION (datasheet Table 9 maps it to GetFileCounters), not a
	// value forced by an ambiguity — which is what the deleted "…AreHarmless" test
	// used to imply. Free access here would let anyone holding the plaque read how
	// often that door has been tapped.
	if s.SDMCtrRet != SDMFileReadKeyNo {
		t.Fatalf("SDM counter retrieval is %#x, want the per-plaque key %#x", s.SDMCtrRet, SDMFileReadKeyNo)
	}
	if s.SDMMetaRead != SDMMetaReadPlain {
		t.Fatal("the configuration is not plain mirroring (ADR 0003 md. 1, Q05)")
	}

	got, err := ChangeFileSettingsData(s)
	if err != nil {
		t.Fatalf("ChangeFileSettingsData: %v", err)
	}
	if want := hexBytes(t, tappaBodyForKnownTemplate); !bytes.Equal(got, want) {
		t.Fatalf("Tappa's body\n got %X\nwant %X", got, want)
	}
	// 18 bytes, three more than the published encrypted-PICC body: PICCDataOffset
	// gone, UIDOffset and SDMReadCtrOffset in its place.
	if len(got) != len(hexBytes(t, katT18CmdData))+3 {
		t.Fatalf("Tappa's body is %d bytes; the plain configuration replaces one offset with two", len(got))
	}
	if _, err := TappaNDEFSettings(nil); err == nil {
		t.Fatal("TappaNDEFSettings accepted a nil template")
	}
}

// TestFileSettings_TappaOptionsByteEqualsThePublishedOne keeps the second
// undiscriminated reading mechanical: which SDMOptions bit means which mirror is
// not settled by the document, but Tappa enables the same three options the
// published example does, so the byte must come out identical to the published C1.
func TestFileSettings_TappaOptionsByteEqualsThePublishedOne(t *testing.T) {
	tpl, err := BuildTapNDEF("https://tap.example.com/t")
	if err != nil {
		t.Fatalf("BuildTapNDEF: %v", err)
	}
	s, err := TappaNDEFSettings(tpl)
	if err != nil {
		t.Fatalf("TappaNDEFSettings: %v", err)
	}
	got, err := ChangeFileSettingsData(s)
	if err != nil {
		t.Fatalf("ChangeFileSettingsData: %v", err)
	}
	published := hexBytes(t, katT18CmdData)
	if got[3] != published[3] {
		t.Fatalf("SDMOptions is %02X, the published example's is %02X; Tappa enables the "+
			"same three options, so any bit assignment must yield the same byte", got[3], published[3])
	}
	if got[0] != published[0] {
		t.Fatalf("FileOption is %02X, the published example's is %02X", got[0], published[0])
	}
}

// TestFileSettings_PresenceConditionsFollowTable69 walks the field-presence rules
// as a table: which offsets appear is decided by SDMMetaRead and SDMFileRead, not
// by whether a caller filled a field in.
func TestFileSettings_PresenceConditionsFollowTable69(t *testing.T) {
	full := SDMFileSettings{
		FileNo: NDEFFileNo, CommMode: CommModePlain,
		Read: ARFree, Write: SDMFileReadKeyNo, ReadWrite: SDMFileReadKeyNo, Change: SDMFileReadKeyNo,
		SDMEnabled: true, UIDMirror: true, ReadCtrMirror: true, ASCIIEncoding: true,
		SDMMetaRead: SDMMetaReadPlain, SDMFileRead: SDMFileReadKeyNo, SDMCtrRet: SDMFileReadKeyNo,
		UIDOffset: 0x11, ReadCtrOffset: 0x22, PICCDataOffset: 0x33,
		MACInputOffset: 0x44, MACOffset: 0x44,
	}
	for _, tc := range []struct {
		name  string
		edit  func(s SDMFileSettings) SDMFileSettings
		want  string
		wantN int
	}{
		{"plain_all_mirrors", func(s SDMFileSettings) SDMFileSettings { return s },
			"4011E1C1F1E1" + "110000" + "220000" + "440000" + "440000", 18},
		{"plain_without_uid_mirror", func(s SDMFileSettings) SDMFileSettings {
			s.UIDMirror = false
			return s
		}, "4011E141F1E1" + "220000" + "440000" + "440000", 15},
		{"plain_without_counter_mirror", func(s SDMFileSettings) SDMFileSettings {
			s.ReadCtrMirror = false
			return s
		}, "4011E181F1E1" + "110000" + "440000" + "440000", 15},
		{"encrypted_picc_drops_both_mirror_offsets", func(s SDMFileSettings) SDMFileSettings {
			s.SDMMetaRead = 0x02
			return s
		}, "4011E1C1F121" + "330000" + "440000" + "440000", 15},
		{"no_sdm_mac_drops_both_mac_offsets", func(s SDMFileSettings) SDMFileSettings {
			s.SDMFileRead = ARNever
			return s
		}, "4011E1C1F1EF" + "110000" + "220000", 12},
		{"sdm_disabled_stops_after_the_access_rights", func(s SDMFileSettings) SDMFileSettings {
			s.SDMEnabled = false
			return s
		}, "0011E1", 3}, // 18 -> 3: Table 69 emits nothing after AccessRights
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ChangeFileSettingsData(tc.edit(full))
			if err != nil {
				t.Fatalf("ChangeFileSettingsData: %v", err)
			}
			if len(got) != tc.wantN {
				t.Fatalf("body is %d bytes, want %d: %X", len(got), tc.wantN, got)
			}
			if want := hexBytes(t, tc.want); !bytes.Equal(got, want) {
				t.Fatalf("body\n got %X\nwant %X", got, want)
			}
		})
	}
}

// TestFileSettings_RejectsMalformedOrForbiddenSettings covers every gate in
// ChangeFileSettingsData, including the two VALUE gates that refuse a body the
// chip would happily accept.
func TestFileSettings_RejectsMalformedOrForbiddenSettings(t *testing.T) {
	good := SDMFileSettings{
		FileNo: NDEFFileNo, CommMode: CommModePlain,
		Read: ARFree, Write: SDMFileReadKeyNo, ReadWrite: SDMFileReadKeyNo, Change: SDMFileReadKeyNo,
		SDMEnabled: true, UIDMirror: true, ReadCtrMirror: true, ASCIIEncoding: true,
		SDMMetaRead: SDMMetaReadPlain, SDMFileRead: SDMFileReadKeyNo, SDMCtrRet: SDMFileReadKeyNo,
		UIDOffset: 29, ReadCtrOffset: 48, MACInputOffset: 60, MACOffset: 60,
	}
	for _, tc := range []struct {
		name string
		edit func(s SDMFileSettings) SDMFileSettings
		why  string
	}{
		{"sdm_read_bound_to_the_master_key", func(s SDMFileSettings) SDMFileSettings {
			s.SDMFileRead = 0x00
			return s
		}, "key 0 is the AppMasterKey; ADR 0017 §5.0 karar 1 forbids it"},
		{"meta_read_bound_to_the_master_key", func(s SDMFileSettings) SDMFileSettings {
			s.SDMMetaRead = 0x00
			return s
		}, "idem"},
		{"access_right_is_not_a_nibble", func(s SDMFileSettings) SDMFileSettings {
			s.Read = 0x1E
			return s
		}, "a value that does not fit a nibble would corrupt its neighbour"},
		{"access_right_names_a_key_that_does_not_exist", func(s SDMFileSettings) SDMFileSettings {
			s.Write = 0x07
			return s
		}, "only 0..4, Eh and Fh exist"},
		{"sdm_file_read_names_a_key_that_does_not_exist", func(s SDMFileSettings) SDMFileSettings {
			s.SDMFileRead = 0x07
			return s
		}, "idem"},
		{"sdm_meta_read_names_a_key_that_does_not_exist", func(s SDMFileSettings) SDMFileSettings {
			s.SDMMetaRead = 0x0D
			return s
		}, "idem"},
		{"sdm_access_right_is_not_a_nibble", func(s SDMFileSettings) SDMFileSettings {
			s.SDMCtrRet = 0x10
			return s
		}, "idem"},
		{"communication_mode_is_reserved", func(s SDMFileSettings) SDMFileSettings {
			s.CommMode = 0x02
			return s
		}, "02h is not a communication mode"},
		{"mac_input_past_the_mac", func(s SDMFileSettings) SDMFileSettings {
			s.MACInputOffset = s.MACOffset + 1
			return s
		}, "the MAC would cover a negative span"},
		// 🔴 THIS CASE EXISTS BECAUSE A MUTATION SURVIVED. Swapping the order in
		// which SDMMACInputOffset and SDMMACOffset are emitted turned no test red:
		// every evidenced configuration sets them equal, so neither our tests nor
		// AN12196's published body can tell the two positions apart. The builder
		// therefore refuses the only bodies where the order could matter, rather
		// than asserting an order nothing can check (filesettings.go).
		{"mac_input_before_the_mac", func(s SDMFileSettings) SDMFileSettings {
			s.MACInputOffset = s.MACOffset - 1
			return s
		}, "a non-empty MAC input depends on a field order no document here discriminates"},
		{"offset_does_not_fit_three_bytes", func(s SDMFileSettings) SDMFileSettings {
			s.UIDOffset = maxUint24 + 1
			return s
		}, "the field is three bytes wide"},
		// 🔴 THE GATE THREE SIBLINGS HAD AND THIS ONE DID NOT. An audit probe fed
		// SDMCtrRet = 07h and got a body back; the chip answers PARAMETER_ERROR
		// (Table 71) and §9.1.10 kills the authentication with it — at step 7, with
		// steps 5 and 6 already on the chip.
		{"sdm_counter_retrieval_names_a_key_that_does_not_exist", func(s SDMFileSettings) SDMFileSettings {
			s.SDMCtrRet = 0x07
			return s
		}, "only 0..4, Eh and Fh exist"},
		{"picc_data_offset_does_not_fit_three_bytes", func(s SDMFileSettings) SDMFileSettings {
			s.SDMMetaRead = 0x02
			s.PICCDataOffset = maxUint24 + 1
			return s
		}, "the field is three bytes wide"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := ChangeFileSettingsData(tc.edit(good))
			if err == nil {
				t.Fatalf("accepted settings that must be refused: %s", tc.why)
			}
			if body != nil {
				t.Fatal("returned a body alongside an error")
			}
		})
	}

	// The same refusals must survive the sealing wrapper rather than being skipped
	// by it, and no session must be needed to reach them.
	bad := good
	bad.SDMFileRead = 0x00
	if _, err := EV2ChangeFileSettingsCommand(katAuth(t, katT14TI, katT14KeyENC, katT14KeyMAC), 0, bad); err == nil {
		t.Fatal("EV2ChangeFileSettingsCommand sealed a body its builder refuses")
	}
}

// TestFileSettings_ParseRejectsResponsesItCannotDescribe covers every refusal in
// ParseFileSettings. A probe that returns a half-understood answer is worse than
// one that fails: ADR 0017 §5.3 uses this to decide whether step 7 ran, and a
// wrong answer sends a recovery down the wrong branch.
func TestFileSettings_ParseRejectsResponsesItCannotDescribe(t *testing.T) {
	// The published NDEF-file response, assembled the way the KAT assembles it.
	good := func(t *testing.T) []byte {
		t.Helper()
		cmdData := hexBytes(t, katT18CmdData)
		out := append([]byte{0x00}, cmdData[:3]...)
		out = append(out, hexBytes(t, "000100")...)
		return append(out, cmdData[3:]...)
	}
	if _, err := ParseFileSettings(NDEFFileNo, good(t)); err != nil {
		t.Fatalf("the control response does not parse: %v", err)
	}

	for _, tc := range []struct {
		name string
		edit func(b []byte) []byte
		why  string
	}{
		{"truncated_before_the_file_size", func(b []byte) []byte { return b[:6] }, "the fixed prefix is 7 bytes"},
		{"empty", func(b []byte) []byte { return nil }, "idem"},
		{"not_a_standard_data_file", func(b []byte) []byte { b[0] = 0x01; return b }, "Table 73 has one file type"},
		{"file_option_sets_a_reserved_bit", func(b []byte) []byte { b[1] |= 0x04; return b },
			"a set RFU bit means the response is not the shape this parser splits"},
		{"sdm_off_but_a_tail_follows", func(b []byte) []byte { b[1] &^= fileOptionSDMBit; return b },
			"Table 73 ends the response after the file size when SDM is off"},
		{"sdm_on_with_no_tail", func(b []byte) []byte { return b[:7] }, "SDM on requires options and rights"},
		{"an_unmodelled_sdm_option", func(b []byte) []byte { b[7] |= sdmOptionENCFileData; return b },
			"SDMENCFileData adds two offsets this package cannot represent"},
		{"reserved_sdm_nibble_is_not_f", func(b []byte) []byte { b[8] = 0x21; return b }, "Table 69 fixes it at Fh"},
		{"an_offset_is_missing", func(b []byte) []byte { return b[:len(b)-3] }, "the options promise one more"},
		{"a_byte_too_many", func(b []byte) []byte { return append(b, 0x00) }, "nothing follows the last offset"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs, err := ParseFileSettings(NDEFFileNo, tc.edit(good(t)))
			if err == nil {
				t.Fatalf("accepted a response that must be refused: %s", tc.why)
			}
			if fs != nil {
				t.Fatal("returned settings alongside an error")
			}
		})
	}

	// A plain (SDM-disabled) response is legal and must parse — the delivery state
	// ADR 0017 §5.3 probe 1 reports as "step 7 has not run". Its access rights are
	// Table 8's E0EE for the NDEF file, which is a second reading of the same
	// two-byte composition: ReadWrite Eh, Change 0h, Read Eh, Write Eh.
	t.Run("delivery_state_parses", func(t *testing.T) {
		fs, err := ParseFileSettings(NDEFFileNo, hexBytes(t, "00"+"00"+"E0EE"+"000100"))
		if err != nil {
			t.Fatalf("the delivery settings do not parse: %v", err)
		}
		if fs.SDMEnabled {
			t.Fatal("SDM reads as enabled at delivery; the datasheet says it is disabled")
		}
		if fs.Read != ARFree || fs.Write != ARFree || fs.ReadWrite != ARFree || fs.Change != 0x00 {
			t.Fatalf("delivery access rights parsed as read %#x write %#x readwrite %#x change %#x, want Table 8's Eh/Eh/Eh/0h",
				fs.Read, fs.Write, fs.ReadWrite, fs.Change)
		}
	})

	// And the command builder's own gate.
	if _, err := GetFileSettingsCommand(0x20); err == nil {
		t.Fatal("GetFileSettingsCommand accepted a file number wider than its 5-bit field")
	}

	// 🔴 TWO CASES BELOW EXIST BECAUSE A MUTATION SURVIVED, AND BOTH SURVIVED FOR
	// THE SAME REASON THE M2-08 COUNTER DID: the published example's values cannot
	// tell two readings apart. Neither gap was visible by reading.
	t.Run("file_size_is_lsb_first_and_the_published_value_cannot_show_it", func(t *testing.T) {
		// 000100 is a PALINDROME across the two byte orders — 256 either way — so
		// the NDEF file's own size proves nothing about the encoding. Table 5
		// (p. 10) gives file 01h as 32 bytes, whose LSB-first spelling 200000 reads
		// as 8388608 the other way round.
		fs, err := ParseFileSettings(0x01, hexBytes(t, "00"+"00"+"E000"+"200000"))
		if err != nil {
			t.Fatalf("ParseFileSettings: %v", err)
		}
		if fs.FileSize != 32 {
			t.Fatalf("file size parsed as %d, want the 32 bytes Table 5 gives the CC file", fs.FileSize)
		}
	})

	t.Run("mac_input_offset_is_read_before_the_mac_offset", func(t *testing.T) {
		// NT4H2421Gx rev. 3.0 Table 73 (p. 70) lists SDMMACInputOffset before
		// SDMMACOffset — the same order Table 69 gives the command body, and the
		// OPPOSITE of AN12196 §5.9 Table 18 step 7's label list. Every published
		// example sets the two equal, so only a body with DIFFERENT values can show
		// which reading is in the code. This one is derived from Table 73's order,
		// not transcribed from a vector, and it says so.
		//
		// The builder deliberately refuses to EMIT such a body (ADR 0003 md. 2), so
		// this is the only side of the pair a test can pin. That asymmetry is
		// recorded rather than hidden: the emit order mirrors this list.
		fs, err := ParseFileSettings(NDEFFileNo,
			hexBytes(t, "00"+"40"+"11E1"+"000100"+"C1"+"F1E1"+"1D0000"+"300000"+"110000"+"3C0000"))
		if err != nil {
			t.Fatalf("ParseFileSettings: %v", err)
		}
		if fs.MACInputOffset != 0x11 {
			t.Fatalf("MAC input offset parsed as %d, want 17 — the field Table 73 puts FIRST", fs.MACInputOffset)
		}
		if fs.MACOffset != 0x3C {
			t.Fatalf("MAC offset parsed as %d, want 60 — the field Table 73 puts SECOND", fs.MACOffset)
		}
	})
}

// TestAPDU_GetCardUIDCommandIsTheSealedHeaderlessFrame checks the constructor that
// exists so turn 2c can DETECT a relay that lied about the UID (ADR 0017 §6
// md. 12). The sealed field itself is pinned by
// TestEV2KAT_GetCardUIDRoundTripMatchesAN12196Table28; what this adds is the
// envelope around it.
func TestAPDU_GetCardUIDCommandIsTheSealedHeaderlessFrame(t *testing.T) {
	auth := katAuth(t, katT28TI, katT28KeyENC, katT28KeyMAC)
	got, err := GetCardUIDCommand(auth, katT28Ctr)
	if err != nil {
		t.Fatalf("GetCardUIDCommand: %v", err)
	}
	want := append(hexBytes(t, "9051000008"), hexBytes(t, katT28CmdTrunc)...)
	want = append(want, 0x00)
	if !bytes.Equal(got, want) {
		t.Fatalf("GetCardUID C-APDU\n got %X\nwant %X", got, want)
	}
	if _, err := GetCardUIDCommand(nil, 0); err == nil {
		t.Fatal("GetCardUIDCommand built a command with no session")
	}
}
