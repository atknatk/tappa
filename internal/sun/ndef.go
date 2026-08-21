package sun

import (
	"fmt"
	"strings"
)

// THE NDEF URL TEMPLATE — the bytes ADR 0017 §5.1 step 5 writes to the chip, and
// the three mirror offsets step 7 then points the SDM engine at.
//
// This file is the hinge between the two halves of the product. The template it
// builds is what a phone will read off the plaque; the offsets it computes are
// what tells the chip where to stamp the UID, the read counter and the SDM MAC;
// and the URL that comes out has to be one internal/sun's OWN verifier can parse
// (params.go) and check (verify_mac.go). That last property is asserted, not
// asserted-about: TestNDEF_ChipMirrorRoundTripsThroughOurOwnVerifier writes NXP's
// published mirror values into the offsets this file computes, reads the URL back
// out of the file bytes, parses it and verifies the published MAC.
//
// ⚠️ AND THAT ROUND TRIP IS AN INTERNAL-CONSISTENCY ANCHOR, NOT EXTERNAL EVIDENCE.
// It proves our encode side and our decode side agree about where the parameters
// sit. It cannot prove the CHIP agrees, because no published example configures
// plain mirroring — AN12196's only ChangeFileSettings example is encrypted-PICC
// (filesettings.go says what that costs). Silicon is still the arbiter, and ADR
// 0017 §6 md. 1 is still true: no chip has been encoded.
//
// URL SHAPE — ADR 0003 md. 1, normative:
//
//	https://<host>/t?tag=<14 hex>&ctr=<6 hex>&cmac=<16 hex>
//
// The three parameter NAMES come from params.go's constants rather than being
// re-typed here. deploy/README's settings table says why: "Kodda tek yerde sabit
// … Başka bir ad = ayrıştırma hatası." A template built with a fourth spelling of
// those names would produce plaques the verifier rejects, one plaque at a time,
// and the only repair would be physical replacement.

const (
	// NDEFFileNo is the NDEF file. NT4H2421Gx rev. 3.0 §8.2.3 Table 5 (p. 10)
	// gives the three static files as 01h (CC), 02h (NDEF) and 03h (proprietary);
	// AN12196's WriteData and ChangeFileSettings examples both address 02h — the
	// CmdHeader of ev2_kat_test.go's katT17Header and katT18Header.
	//
	// ⚠️ CITATION CORRECTED (2026-08-21, third-eye audit): this said §8.2.2, which
	// is "Application Name and ID".
	NDEFFileNo = 0x02

	// ndefFileSize is the NDEF file's capacity in bytes.
	//
	// 🔴 TRANSCRIBED — NT4H2421Gx rev. 3.0 §8.2.3 Table 5 (p. 10) gives file 02h
	// as 256 bytes, and §8.2.3.2 repeats it inside the CC file's contents.
	//
	// ⚠️ THE LABEL THAT WAS HERE WAS WRONG IN THE EXPENSIVE DIRECTION. It said
	// "THIS NUMBER IS A READING … Nothing in this repository measures it". An
	// unearned "measured" claim invites over-trust; an unearned "unmeasured" claim
	// tells the next reader not to bother looking, which is how a document that is
	// ON THE SHELF stays unread. Both are defects; only the first one has a name.
	ndefFileSize = 256

	// ndefTearingProtectedWrite is the largest single WriteData the chip protects
	// against tearing — NT4H2421Gx rev. 3.0 §8.2.3.1 (p. 10): "The writing
	// operations of single frames up to 128 bytes with a WriteData or
	// ISOUpdateBinary command are also tearing protected."
	//
	// 🔴 WHAT THE 128 MEASURES, BECAUSE THE GATE IS CALIBRATED ON IT. It is the
	// bytes WRITTEN, not the sealed frame, and the evidence is the datasheet's own
	// contrast: where it means the sealed size it says so, in the very next command
	// description — Table 81 (p. 75) caps WriteData's Data field at "up to 248 byte
	// INCLUDING SECURE MESSAGING". §8.2.3.1 carries no such phrase and speaks of
	// both WriteData and ISOUpdateBinary, and ISOUpdateBinary is CommMode.Plain
	// (Table 22, p. 44), where written bytes and frame bytes are the same number.
	// A limit that had to mean two different things for the two commands it names
	// would be the odd reading. So this gate is on the FILE length, which is what a
	// single-frame write of the template writes.
	//
	// 🔴 A TEMPLATE LARGER THAN THIS IS REFUSED, AND THAT IS A DECISION THIS ROUND
	// MADE (2026-08-21) RATHER THAN A PROTOCOL LIMIT. Above 128 bytes a torn write
	// can leave a PARTIALLY written NDEF file, and ADR 0017 §5.3 already names the
	// one pair of states its three probes cannot tell apart: "never touched" and
	// "only step 5 was written". Staying inside the tearing-protected size removes
	// that pair instead of adding a fourth probe for it.
	//
	// What it costs, so the limit is a counted one: the host plus path may be up to
	// 69 bytes (128 - ndefPrefixLen - the 52 the three parameters add). Today's
	// realistic hosts are half that, and Q08 has not picked one yet — so the budget
	// is a constraint on a decision not yet made, which is the cheapest moment to
	// impose one.
	ndefTearingProtectedWrite = 128

	// nlenLen is the NDEF message length prefix a Type 4 tag file carries.
	//
	// TRANSCRIBED BY MEASUREMENT — NFC Forum T4T Operation Specification v3.0
	// §5.5.2 defines the two-byte NLEN field, and AN12196 rev. 2.0 §5.8.2 Table 17
	// confirms both its size and its BYTE ORDER: the published body begins 0051
	// and its NDEF record is 81 bytes long. 0x0051 read MSB-first is 81; read
	// LSB-first it would be 20736. So NLEN is big-endian while every offset in the
	// same command is little-endian, and that asymmetry is real rather than a
	// transcription slip — the two fields belong to two different specifications.
	nlenLen = 2

	// ndefURIRecordHeaderLen is D1 || 01 || <payload length> || 55 — the NFC Forum
	// short-record header for a URI record. AN12196 rev. 2.0 §5.8.2 Table 17's
	// body carries exactly those four bytes after NLEN: D1 01 4D 55.
	ndefURIRecordHeaderLen = 4

	// ndefRecordD1 is the record header byte: MB=1, ME=1, SR=1, TNF=001
	// (NFC Forum well-known type). Table 17's body: D1.
	ndefRecordD1 = 0xD1
	// ndefTypeLen is the length of the type field, and ndefTypeU is the type
	// itself — 'U', the URI RTD. Table 17: 01 … 55.
	ndefTypeLen = 0x01
	ndefTypeU   = 0x55

	// ndefURIPrefixHTTPS is the URI abbreviation code for "https://".
	//
	// ⚠️ READ FROM A THIRD DOCUMENT — NFC Forum URI Record Type Definition 1.0
	// §3.2.2 Table 3, where 04h is "https://". This label is deliberately left
	// standing after the round that removed two others like it: the difference is
	// that the two removed ones pointed at NXP documents this project HAS, while
	// the URI RTD is a document nobody here has opened. What IS corroborated is
	// narrower and worth stating: AN12196 rev. 2.0 §5.8.2 Table 17's body writes
	// 04h as the payload's first byte for a URL, so the byte is the one NXP's own
	// example uses. The abbreviation table is not in either NXP document.
	//
	// A mutation flipping this to 03h ("http://") is checked by
	// TestNDEF_TemplateIsTheADR0003URLWithMirrorRuns, which spells 04 as a literal
	// rather than reading it back out of this constant — the first version did the
	// latter and the mutation survived.
	//
	// 🔴 ONLY 04h IS IMPLEMENTED, ON PURPOSE. 03h ("http://") is a valid code and
	// is deliberately unreachable: a plaque encoded with a cleartext scheme would
	// send an employee's tap — session cookie included — over a channel anyone in
	// the room can read, and the fix would be physical replacement (Q08's rule for
	// a wrong host, applied to a wrong scheme).
	ndefURIPrefixHTTPS = 0x04

	// ndefHTTPSScheme is the prefix a caller's base URL must carry, and the eight
	// characters the abbreviation code replaces.
	ndefHTTPSScheme = "https://"

	// ndefMirrorFill is the character the mirror placeholders are made of. The chip
	// overwrites these positions on every read; until it does, the plaque emits an
	// all-zero uid and an all-zero MAC.
	//
	// ⚠️ THE REASON THAT IS SAFE WAS WRITTEN WRONG AND THE CORRECTION IS SMALLER
	// THAN IT LOOKS. This said the all-zero uid "resolves to no row" — which is only
	// true while nobody has inserted a row for fourteen zeros, and tags.uid is a
	// GLOBAL primary key any tenant can occupy (ADR 0017 §6 md. 12). The guarantee
	// that does not depend on that is the MAC: an unmirrored plaque carries sixteen
	// zeros where the SDM MAC belongs, and verify_mac.go compares it against a real
	// CMAC. Fail-closed does not rest on the lookup missing.
	ndefMirrorFill = '0'

	// ndefMaxShortPayload is the largest payload a short record (SR=1) can
	// declare, its length field being one byte.
	ndefMaxShortPayload = 0xFF
)

// ndefPrefixLen is the number of bytes that precede the URI text inside the file:
// NLEN(2) || D1 01 <len> 55 (4) || the abbreviation code (1).
//
// 🔴 THIS IS THE ARITHMETIC THE OFFSETS REST ON, AND IT IS MEASURED AGAINST A
// PUBLISHED PAIR RATHER THAN ASSERTED. AN12196 rev. 2.0 §5.8.2 Table 17 (the
// WriteData body) and §5.9 Table 18 (the ChangeFileSettings body) describe the
// SAME chip in the SAME session, so the offsets in one must index the bytes of
// the other. Measured: Table 17's URI text starts at file byte 7, its "?e="
// placeholder run starts at byte 32, and Table 18's PICCDataOffset is 200000 —
// 32, LSB first. Its "&c=" run starts at byte 67 and Table 18's SDMMACOffset is
// 430000 — 67. So mirror offsets are absolute positions from the START OF THE
// FILE, counting the NLEN prefix, and this constant is the 7 that follows.
// TestNDEFKAT_MirrorOffsetsAreAbsoluteFilePositions re-runs that measurement and
// names the refuted alternative (message-relative offsets, i.e. two lower).
const ndefPrefixLen = nlenLen + ndefURIRecordHeaderLen + 1

// TapNDEF is a ready-to-write NDEF file plus the three mirror positions that
// describe it. Nothing in it is secret: the URL is printed on the plaque and the
// offsets are readable from the chip with GetFileSettings (ADR 0017 §5.3 probe 1).
type TapNDEF struct {
	// File is the complete NDEF file content, to be written at offset 0.
	File []byte
	// URL is the template as a reader would see it before the chip has ever
	// mirrored anything — the placeholders are still zeros.
	URL string
	// UIDOffset, ReadCtrOffset and MACOffset are absolute byte positions in File,
	// counted from File[0]. They go into ChangeFileSettings verbatim.
	UIDOffset     uint32
	ReadCtrOffset uint32
	MACOffset     uint32
}

// BuildTapNDEF turns a base URL into the NDEF file ADR 0017 §5.1 step 5 writes
// and the mirror offsets step 7 configures.
//
// base is the scheme, host and path with NO query string — for example
// "https://tap.example.com/t". The three SUN parameters are appended here, in the
// order and with the names ADR 0003 md. 1 fixes, so no caller can invent a fourth
// spelling.
//
// ⚠️ THE HOST IS NOT DECIDED (Q08) AND THIS FUNCTION DOES NOT DECIDE IT. It takes
// whatever it is given, because the rule that matters lives above this layer:
// "Q08 kapanmadan üretim plaketi encode edilmez" (ADR 0017 §6 md. 11). A plaque
// encoded with the wrong host is a plaque swap in the field, not a config change.
func BuildTapNDEF(base string) (*TapNDEF, error) {
	if !strings.HasPrefix(base, ndefHTTPSScheme) {
		return nil, fmt.Errorf("sun: ndef: base URL must start with the https scheme")
	}
	rest := base[len(ndefHTTPSScheme):]
	if rest == "" {
		return nil, fmt.Errorf("sun: ndef: base URL carries no host")
	}
	// The URI text is indexed by BYTE offset, so anything that is not a single
	// printable ASCII byte would shift the mirrors. A query separator would also
	// put our three parameters behind somebody else's, which params.go tolerates
	// but a plaque cannot be corrected into.
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if c <= 0x20 || c >= 0x7F {
			return nil, fmt.Errorf("sun: ndef: base URL byte %d is not printable ASCII", i)
		}
		if c == '?' || c == '#' || c == '&' {
			return nil, fmt.Errorf("sun: ndef: base URL must carry no query or fragment")
		}
	}

	uri := rest +
		"?" + paramTag + "=" + strings.Repeat(string(ndefMirrorFill), uidHexLen) +
		"&" + paramCtr + "=" + strings.Repeat(string(ndefMirrorFill), ctrHexLen) +
		"&" + paramCMAC + "=" + strings.Repeat(string(ndefMirrorFill), cmacHexLen)

	payloadLen := 1 + len(uri) // the abbreviation code plus the text
	if payloadLen > ndefMaxShortPayload {
		return nil, fmt.Errorf("sun: ndef: URI is %d bytes, more than a short record can declare", len(uri))
	}
	recordLen := ndefURIRecordHeaderLen + payloadLen
	fileLen := nlenLen + recordLen
	if fileLen > ndefFileSize {
		return nil, fmt.Errorf("sun: ndef: file would be %d bytes, more than the %d the NDEF file holds", fileLen, ndefFileSize)
	}
	if fileLen > ndefTearingProtectedWrite {
		return nil, fmt.Errorf("sun: ndef: file would be %d bytes, more than the %d a single tearing-protected write carries",
			fileLen, ndefTearingProtectedWrite)
	}

	file := make([]byte, 0, fileLen)
	// NLEN, big-endian (see nlenLen). recordLen is bounded by ndefFileSize above.
	file = append(file, byte(recordLen>>8), byte(recordLen))
	file = append(file, ndefRecordD1, ndefTypeLen, byte(payloadLen), ndefTypeU)
	file = append(file, ndefURIPrefixHTTPS)
	file = append(file, uri...)

	uidOff := uint32(ndefPrefixLen + len(rest) + len("?"+paramTag+"="))
	ctrOff := uidOff + uint32(uidHexLen) + uint32(len("&"+paramCtr+"="))
	macOff := ctrOff + uint32(ctrHexLen) + uint32(len("&"+paramCMAC+"="))

	t := &TapNDEF{
		File:          file,
		URL:           ndefHTTPSScheme + uri,
		UIDOffset:     uidOff,
		ReadCtrOffset: ctrOff,
		MACOffset:     macOff,
	}

	// 🔴 THE ARITHMETIC ABOVE IS CHECKED AGAINST THE BYTES IT DESCRIBES, HERE,
	// EVERY CALL — not only in a test. An off-by-one in an offset does not fail
	// loudly on silicon: the chip happily stamps hex over whatever is at the
	// position it was given, the URL still parses, and the MAC then fails for
	// every tap of that plaque forever. That failure is indistinguishable from a
	// wrong key (skill tappa-sun's named trap), so it would be diagnosed in the
	// wrong place. A run of placeholder characters of exactly the right length is
	// cheap evidence that the position is the one the template laid down.
	for _, m := range []struct {
		off uint32
		n   int
	}{{uidOff, uidHexLen}, {ctrOff, ctrHexLen}, {macOff, cmacHexLen}} {
		if int(m.off)+m.n > len(file) {
			return nil, fmt.Errorf("sun: ndef: computed mirror position %d runs past the %d-byte file", m.off, len(file))
		}
		for i := 0; i < m.n; i++ {
			if file[int(m.off)+i] != ndefMirrorFill {
				return nil, fmt.Errorf("sun: ndef: computed mirror position %d does not point at a placeholder", m.off)
			}
		}
	}
	return t, nil
}
