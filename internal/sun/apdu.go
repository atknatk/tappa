package sun

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// THE COMMAND LAYER OF THE PERSONALISATION FLOW (M8-05 FAZ B2b).
//
// ev2.go and changekey.go seal bodies. This file builds the FRAMES those bodies
// travel in, plus the three commands that run BEFORE a session exists: the ISO
// SELECT of the NDEF application, GetVersion (which is where the UID comes from)
// and the two halves of AuthenticateEV2First. ADR 0017 §5.1's normative sequence
// is the index to this file:
//
//	1. ISO SELECT (NDEF application)         ISOSelectNDEFApplication
//	2. GetVersion -> 7-byte UID              GetVersionCommands / ParseGetVersion
//	3. [server] key -> Wrap -> tags row      not here (turn 2c)
//	4. AuthenticateEV2First(key 0)           AuthenticateEV2FirstCommand + …Part2Command
//	5. WriteData -> NDEF URL template        EV2WriteDataCommand (+ ndef.go)
//	6. ChangeKey(0x01)                       changekey.go (FAZ B2a)
//	7. ChangeFileSettings                    filesettings.go
//	8. ChangeKey(0x00)                       changekey.go (FAZ B2a)
//	9. [server] Zero + mark the row          not here (turn 2c)
//
// 🔴 WHAT THIS FILE IS NOT — the same boundary FAZ B2a drew. There is no HTTP, no
// DB, no transport, no relay, no context, no goroutine and no stateful session
// object here. Every function takes bytes and returns bytes; nothing here sends
// anything anywhere. CmdCtr stays an explicit argument, so no function in this
// package can mis-count on a caller's behalf (ADR 0017 §6 md. 7 — the session's
// TTL, its sweeper and the zeroisation rule are still turn 2c's).
//
// 🔴 NOTHING HERE HANDLES KEY MATERIAL, WHICH IS WHY THERE IS NOT A SINGLE Zero
// IN THIS FILE. What passes through is a UID (public — printed on the plaque and
// carried in every tap URL), a template URL (public), file settings (public
// configuration) and ciphertext produced by ev2.go, which wipes its own plaintext
// buffers. The claim is mechanical, not editorial: TestEV2_ZeroDisciplineIsInventoried
// now scans this file too and requires it to hold ZERO deferred wipes, so the day
// a plaintext key buffer appears here the inventory has to be edited in the same
// change.
//
// SOURCES, with revisions spelled out because AN12196 renumbered its chapters
// (rev. 1.8 §6 "Personalization example" is rev. 2.0 §5; rev. 2.0's §6 is
// "Special functionalities"). The mapping table lives in an12196_kat_test.go.
//
//	NT4H2421Gx rev. 3.0, 31 Jan 2019 — the normative protocol.
//	AN12196 rev. 1.8, 17 Nov 2020 / rev. 2.0, 4 Mar 2025 — worked examples.
//
// 🔴 M2-08's RULE, APPLIED, AND THIS ROUND HAD LESS DOCUMENT TO STAND ON THAN THE
// LAST. Byte orders and offsets below are never derived from structure. Where a
// value is pinned by a published vector this file says which one; where it is
// read from a document and NO vector exercises it, it says DERIVED, NOT
// TRANSCRIBED and says how the reading was constrained. The precedent is
// an12196_kat_test.go's katT5URLCtr.

// ISO 7816-4 framing constants for the "wrapped native" command set.
//
// TRANSCRIBED SHAPE — every C-APDU AN12196 prints for a native command has the
// form 90 <INS> 00 00 [Lc <data>] 00. The published examples this repository
// already carries show both variants: AN12196 rev. 2.0 §5.6 Table 14 sends
// 9071000002 <KeyNo> <LenCap> 00 (with data) and §5.5 Table 12's GetVersion sends
// 9060000000 (without).
const (
	// apduCLA is the class byte of the wrapped native command set.
	apduCLA = 0x90
	// apduP1, apduP2 are always zero for wrapped native commands; the command's
	// own parameters travel in the data field as CmdHeader.
	apduP1 = 0x00
	apduP2 = 0x00
	// apduLe asks for the full response. It is present on EVERY wrapped native
	// C-APDU in the documents, including the ones with no data field.
	apduLe = 0x00
	// apduMaxLc is the largest data field a short-form Lc can express. Longer
	// bodies need either chunking or extended-length APDUs; neither is
	// implemented, and a body that needs one is refused rather than truncated —
	// a truncated ChangeFileSettings would be a half-configured chip.
	apduMaxLc = 0xFF
)

// Instruction bytes. Each is transcribed at its own definition site so the
// citation cannot drift away from the value.
const (
	// INSISOSelectFile — ISO/IEC 7816-4 SELECT. NT4H2421Gx rev. 3.0 §10.9.1
	// Table 84 (p. 77), ISOSelectFile: "INS A4h". This is the ONE command here
	// that uses the plain ISO class (00h) rather than the wrapped-native 90h.
	//
	// ⚠️ CITATION CORRECTED (2026-08-21, third-eye audit). This said §10.3.1;
	// §10.3 is "Status word" and has nothing to do with SELECT. The BYTE was
	// right and the pointer was wrong, which is the worse half: a wrong citation
	// sends the next reader to a page that cannot confirm or refute it.
	INSISOSelectFile = 0xA4
	// INSGetVersion — NT4H2421Gx rev. 3.0 §10.5.2 (GetVersion): "Cmd 60h".
	INSGetVersion = 0x60
	// INSAdditionalFrame — the "give me the next frame" continuation, and also
	// the second half of AuthenticateEV2First. NT4H2421Gx rev. 3.0 §10.5.2 and
	// §10.4.1; AN12196 rev. 2.0 §5.6 Table 14 step 14 sends 90AF000020 …
	INSAdditionalFrame = 0xAF
	// INSAuthenticateEV2First — NT4H2421Gx rev. 3.0 §10.4.1: "Cmd 71h".
	INSAuthenticateEV2First = 0x71
	// INSWriteData — NT4H2421Gx rev. 3.0 Table 81 (p. 75): "Cmd 8Dh". Pinned by AN12196
	// rev. 2.0 §5.8.2 Table 17 (rev. 1.8 §6.8.2 Table 18), whose command byte is
	// the one ev2_kat_test.go's katT17Cmd already carries.
	INSWriteData = 0x8D
	// INSChangeFileSettings — NT4H2421Gx rev. 3.0 §10.7.1: "Cmd 5Fh". Pinned by
	// AN12196 rev. 2.0 §5.9 Table 18 (katT18Cmd).
	INSChangeFileSettings = 0x5F
	// INSGetFileSettings — NT4H2421Gx rev. 3.0 §10.7.2 Table 72 (p. 69): "Cmd F5h".
	INSGetFileSettings = 0xF5
	// INSGetCardUID — NT4H2421Gx rev. 3.0 Table 22 (p. 44): CLA 90, INS 51h,
	// CommMode.Full. Pinned by AN12196 rev. 2.0 §6.3 Table 28 (katT28Cmd).
	INSGetCardUID = 0x51
	// INSChangeKey is spelled ONCE, in changekey.go, next to its Table 63
	// citation; this alias exists so a caller framing a ChangeKey body does not
	// have to reach for an unexported name.
	INSChangeKey = changeKeyINS
)

// StatusWord is the two-byte SW1SW2 trailer of an R-APDU, SW1 in the high byte.
//
// 🔴 A STATUS WORD IS NOT A SECRET AND IS DELIBERATELY PRINTABLE. §4.7 protects
// key material; a return code is the one thing a half-written chip tells us about
// itself (ADR 0017 §5.3's probes are entirely status-word reading), so hiding it
// would cost diagnosis and buy nothing.
type StatusWord uint16

const (
	// SWSuccess — the wrapped native "OK" trailer, 9100h. AN12196 rev. 2.0 §5.9
	// Table 18 step 17 ends its R-APDU with 9100.
	SWSuccess StatusWord = 0x9100
	// SWAdditionalFrame — 91AFh, "more data follows, send AFh to continue".
	// AN12196 rev. 2.0 §5.6 Table 14 step 6 and §5.5 Table 12's first two
	// GetVersion frames both end this way.
	SWAdditionalFrame StatusWord = 0x91AF
	// SWISOSuccess — 9000h, the ISO 7816-4 trailer the plain SELECT returns.
	SWISOSuccess StatusWord = 0x9000
)

// swLen is the size of the SW1SW2 trailer.
const swLen = 2

// String renders a status word as four hex digits so an error message can name
// it without a caller writing its own formatting.
func (s StatusWord) String() string { return fmt.Sprintf("%04X", uint16(s)) }

// SplitResponse separates an R-APDU into its data field and its status word.
//
// 🔴 CALL THIS BEFORE EV2UnwrapResponseFull, ALWAYS. NT4H2421Gx rev. 3.0 §9.1.10:
// on any error "the authentication state is immediately lost and the error return
// code is sent without a MAC appended". An error frame therefore has NO MAC to
// verify, and feeding one to the unwrapper produces a length complaint that reads
// like a cryptographic failure and is not one. The session is dead at that point
// and the correct move is ADR 0017 §5.3's recovery, not a retry.
func SplitResponse(resp []byte) ([]byte, StatusWord, error) {
	if len(resp) < swLen {
		return nil, 0, fmt.Errorf("sun: apdu: response must carry at least the %d status bytes, got %d", swLen, len(resp))
	}
	n := len(resp) - swLen
	return resp[:n], StatusWord(resp[n])<<8 | StatusWord(resp[n+1]), nil
}

// RequireStatus reports whether got is want, as an error naming both.
func RequireStatus(got, want StatusWord) error {
	if got != want {
		return fmt.Errorf("sun: apdu: chip returned status %s, expected %s", got, want)
	}
	return nil
}

// NativeAPDU wraps a native command's ISO 7816 data field into a complete C-APDU.
//
// It is the framing half of the split this package keeps: the EV2 builders
// (EV2ChangeKeyCommand, EV2WriteDataCommand, EV2ChangeFileSettingsCommand) return
// the DATA FIELD, and this turns any of them into the bytes an APDU relay pushes
// at the chip (ADR 0017 §2.1: the phone "hiçbir baytı üretmez"). Keeping them
// apart is what let ev2_kat_test.go check four published data fields against the
// document without ever having to agree with it about the envelope.
//
// A nil or empty field produces the no-data form 90 <INS> 00 00 00, which is what
// GetVersion and the AF continuations use.
func NativeAPDU(ins byte, field []byte) ([]byte, error) {
	if len(field) > apduMaxLc {
		// Names a length, never a byte (§4.7) — and the field may be ChangeKey
		// ciphertext.
		return nil, fmt.Errorf("sun: apdu: data field is %d bytes, more than the %d a short Lc can express", len(field), apduMaxLc)
	}
	if len(field) == 0 {
		return []byte{apduCLA, ins, apduP1, apduP2, apduLe}, nil
	}
	out := make([]byte, 0, 5+len(field)+1)
	out = append(out, apduCLA, ins, apduP1, apduP2, byte(len(field)))
	out = append(out, field...)
	return append(out, apduLe), nil
}

// --- Step 1: ISO SELECT --------------------------------------------------------

// ndefApplicationAID is the NFC Forum Type 4 Tag NDEF Tag Application identifier.
// NT4H2421Gx rev. 3.0 §8.2.1: the NDEF application is selected by its DF name
// D2760000850101h — the same seven bytes NFC Forum's T4T Operation Specification
// v3.0 §5.4.2 fixes for every Type 4 tag.
var ndefApplicationAID = []byte{0xD2, 0x76, 0x00, 0x00, 0x85, 0x01, 0x01}

// ISO SELECT parameters. P1 = 04h selects by DF name; P2 = 0Ch asks for no
// response data (NT4H2421Gx rev. 3.0 §10.9.1 Table 84, p. 77). The class byte is
// 00h — this command is a real ISO 7816-4 SELECT, not a wrapped native command,
// which is why it does not go through NativeAPDU and why its success trailer is
// 9000h rather than 9100h.
const (
	isoSelectCLA = 0x00
	isoSelectP1  = 0x04
	isoSelectP2  = 0x0C
)

// ISOSelectNDEFApplication returns the C-APDU that selects the NDEF application —
// ADR 0017 §5.1 step 1, the command every later one depends on.
//
// It needs no authentication and produces no persistent side effect, which is why
// ADR 0017 §5.2 can put the tags row before it.
//
// 🔴 TRANSCRIBED, AND THE PREVIOUS VERSION OF THIS COMMENT WAS THE DEFECT.
// It said "P2 IS THE ONE BYTE HERE THAT IS A READING, NOT A MEASUREMENT … this
// repository holds no vector for either". A third-eye audit with the PDFs open
// found the vector immediately: AN12196 rev. 2.0 §5.3 Table 9 step 10 prints the
// complete C-APDU, 00A4040C07D276000085010100 — byte for byte what this function
// returns, P2 = 0Ch included. TestAPDU_ISOSelectMatchesAN12196Table9 now asserts
// it against the published string.
//
// 🔴 WHY THAT MATTERED MORE THAN A WRONG BYTE WOULD HAVE. The bytes were right.
// What shipped was an "I could not measure this" label standing in front of
// evidence that was on the shelf — the mirror image of the https-to-http mutation
// this round already caught, where a test read its expectation from the constant
// it was testing. One hides a defect that exists; the other hides the proof that
// none does, and only the first announces itself.
func ISOSelectNDEFApplication() []byte {
	out := make([]byte, 0, 5+len(ndefApplicationAID)+1)
	out = append(out, isoSelectCLA, INSISOSelectFile, isoSelectP1, isoSelectP2, byte(len(ndefApplicationAID)))
	out = append(out, ndefApplicationAID...)
	return append(out, apduLe)
}

// --- Step 2: GetVersion --------------------------------------------------------

const (
	// getVersionFrames is how many R-APDUs GetVersion answers with, and it is not
	// negotiable.
	//
	// 🔴 THREE, TRANSCRIBED — NT4H2421Gx rev. 3.0 §10.5.2 (p. 58): "The version
	// data is return over three frames." AN12196 rev. 1.8 §6.5 Table 13 (rev. 2.0
	// §5.5 Table 12) shows the exchange: 9060000000 answered with 91AF, then
	// 90AF000000 answered with 91AF, then 90AF000000 answered with 9100.
	// ADR 0017 §4's "at least ten APDU exchanges" is counted off this number.
	getVersionFrames = 3

	// getVersionInfoLen is the size of the first two frames (hardware info and
	// software info). NT4H2421Gx rev. 3.0 §10.5.2 gives each as seven bytes:
	// vendor, type, subtype, major and minor version, storage size, protocol.
	getVersionInfoLen = 7

	// getVersionThirdFrameMin and getVersionThirdFrameMax bound the third frame.
	//
	// 🔴 TRANSCRIBED — AND THE FIRST VERSION OF THIS COMMENT CLAIMED THE OPPOSITE.
	// It said "DERIVED, NOT TRANSCRIBED … this round could not measure that
	// trailer's exact length". It is published twice over:
	//
	//	AN12196 rev. 2.0 §5.5 Table 12 step 9 (rev. 1.8 §6.5 Table 13) prints the
	//	  third frame in full: 04968CAA5C5E80CD65935D4021189100 — FOURTEEN data
	//	  bytes plus the 9100 trailer, with the UID 04968CAA5C5E80 at the front.
	//	NT4H2421Gx rev. 3.0 Table 58 (p. 60) itemises it: UID 7 || BatchNo 4 ||
	//	  1 || 1 || 1, plus an OPTIONAL 1-byte FabKeyID — so 14 or 15 bytes.
	//
	// Hence a RANGE rather than a floor. The gate still refuses the mistake it was
	// written for — being handed a 7-byte hardware- or software-info frame, whose
	// bytes are identical on every chip NXP ships. That mistake would return the
	// vendor and version block as a "UID", and tags.uid is a PRIMARY KEY
	// (migration 00004): every plaque encoded that way would collide on one row,
	// which ADR 0017 §5.2 calls permanent plaque loss. The upper bound is new and
	// is the same argument in the other direction: a frame LONGER than 15 bytes is
	// not this command's third frame, so parsing a UID out of its front is a guess.
	getVersionThirdFrameMin = 14
	getVersionThirdFrameMax = 15
)

// Version is what GetVersion's three frames carry. Only the UID is interpreted;
// the rest is surfaced verbatim so a caller can log or store it without this
// package inventing a meaning for bytes it has no vector for.
type Version struct {
	// UID is the raw 7-byte chip UID. Public: it is printed on the plaque and
	// travels in every tap URL (ADR 0003 md. 1).
	UID []byte
	// Hardware and Software are the first two frames, uninterpreted.
	Hardware []byte
	Software []byte
	// Production is the third frame MINUS the UID — batch number and production
	// date. Uninterpreted, and its length is not asserted anywhere.
	Production []byte
}

// UIDHex renders the UID exactly as tags.uid stores it: 14 UPPER-CASE hex
// characters.
//
// 🔴 THE CASE IS LOAD-BEARING AND THIS IS THE WRITE SIDE OF A RULE THE READ SIDE
// ALREADY OBEYS. Migration 00013 narrowed the column to ^[0-9A-F]{14}$ (backlog
// T8) and params.go canonicalises every incoming tap URL to upper case for the
// same reason. An encode flow that inserted lower-case hex would either be
// refused by the CHECK constraint or — before 00013 — have created a second row
// for one physical chip, with its own counter and its own wrapped key.
func (v *Version) UIDHex() string {
	if v == nil {
		return ""
	}
	return strings.ToUpper(hex.EncodeToString(v.UID))
}

// GetVersionCommands returns the three C-APDUs of ADR 0017 §5.1 step 2, in order.
//
// 🔴 THE SIGNATURE IS THE POINT. Returning an array of exactly three commands,
// and requiring all three frames back in ParseGetVersion, is what makes "read the
// UID out of the wrong frame" unrepresentable rather than merely discouraged.
// GetVersion needs no authentication — NT4H2421Gx rev. 3.0 §10.5.2 (p. 58): "This
// command is freely accessible without secure messaging as soon as the PD is
// selected and there is no active authentication" — which is exactly what lets
// ADR 0017 §5.2 write the tags row before the chip's first irreversible command.
func GetVersionCommands() [getVersionFrames][]byte {
	first, _ := NativeAPDU(INSGetVersion, nil)     // no data field -> cannot fail
	next, _ := NativeAPDU(INSAdditionalFrame, nil) // idem
	return [getVersionFrames][]byte{first, next, append([]byte(nil), next...)}
}

// ParseGetVersion validates the three GetVersion frames and extracts the UID.
//
// Each frame is the R-APDU payload WITHOUT its status word: pass the data half of
// SplitResponse, having first checked that frames one and two ended in 91AF and
// frame three in 9100.
//
// TRANSCRIBED — NT4H2421Gx rev. 3.0 §10.5.2: the third frame begins with the
// 7-byte UID. Everything after it is production data this package does not read.
func ParseGetVersion(frame1, frame2, frame3 []byte) (*Version, error) {
	for i, f := range [][]byte{frame1, frame2} {
		if len(f) != getVersionInfoLen {
			return nil, fmt.Errorf("sun: getversion: frame %d must be %d bytes, got %d", i+1, getVersionInfoLen, len(f))
		}
	}
	if len(frame3) < getVersionThirdFrameMin || len(frame3) > getVersionThirdFrameMax {
		return nil, fmt.Errorf("sun: getversion: frame 3 must be %d or %d bytes, got %d",
			getVersionThirdFrameMin, getVersionThirdFrameMax, len(frame3))
	}
	if err := checkUID(frame3[:uidLen]); err != nil {
		return nil, err
	}
	return &Version{
		UID:        append([]byte(nil), frame3[:uidLen]...),
		Hardware:   append([]byte(nil), frame1...),
		Software:   append([]byte(nil), frame2...),
		Production: append([]byte(nil), frame3[uidLen:]...),
	}, nil
}

// uidManufacturerNXP is the first byte of every NTAG 424 DNA UID. NT4H2421Gx
// rev. 3.0 §8.1 (p. 8): "The ISO/IEC 14443-3 compliant UID is programmed and
// locked during production. The first byte of the double size UID is fixed to 04h,
// indicating NXP as manufacturer."
const uidManufacturerNXP = 0x04

// checkUID refuses the two UID values the DOCUMENT says are not a chip identity.
// Both were shipped as accepted until a security audit fed them in.
//
// 🔴 ALL ZEROS IS NOT "DEGENERATE-LOOKING", IT IS A DOCUMENTED STATE, AND IT IS THE
// DANGEROUS ONE. NT4H2421Gx rev. 3.0 Table 58 (p. 60) gives GetVersion's UID field
// as "All zero — if configured for RandomID". So an all-zero UID does not mean a
// broken read; it means THE CHIP IS NOT TELLING US ITS UID. Writing it into
// tags.uid — a PRIMARY KEY (migration 00004) — would take the first such plaque and
// make every later one collide, and ADR 0017 §5.2 calls a chip whose key is in no
// row permanent plaque loss.
//
// 🔴 AND RANDOM ID IS REACHABLE BY AN ATTACKER, WHICH IS WHY THIS IS A GATE AND NOT
// AN ASSERTION. ADR 0017 §5.2 argues the UID is trustworthy because "Random ID is
// disabled at delivery time" — true, and NOT the whole story: SetConfiguration
// option 00h enables it (§10.5.1, Table 50) and SetConfiguration needs only the
// AppMasterKey, which is the PUBLIC factory key on any chip whose key 0 we have not
// personalised yet (ADR 0005 risk 8). So the delivery default is a starting state,
// not a guarantee.
//
// ⚠️ WHAT THIS DOES NOT CLOSE, STATED HERE BECAUSE THE GAP IS THE POINT. ADR 0017
// §2.2 says to assume the RELAY is hostile, and a hostile relay does not have to
// send a degenerate UID — it can send a well-formed one belonging to another chip.
// Nothing in a GetVersion response is authenticated, so no check here can tell that
// apart. This is defence in depth against a broken or reconfigured chip, not
// against a lying phone; the remedy for the lying phone is named in ADR 0017 §6
// md. 12 and needs GetCardUIDCommand below.
func checkUID(uid []byte) error {
	allZero := true
	for _, c := range uid {
		if c != 0x00 {
			allZero = false
			break
		}
	}
	if allZero {
		return fmt.Errorf("sun: getversion: the chip returned an all-zero UID, which means Random ID is enabled and the real UID was not disclosed")
	}
	if uid[0] != uidManufacturerNXP {
		// Names the byte position and the expected manufacturer byte, both public.
		return fmt.Errorf("sun: getversion: UID starts with %02X, not the %02X every NXP chip is programmed with", uid[0], uidManufacturerNXP)
	}
	return nil
}

// --- Step 4: AuthenticateEV2First ----------------------------------------------

// authFirstNoPCDCap is the LenCap byte for "the PCD sends no capability bytes".
// NT4H2421Gx rev. 3.0 §10.4.1: the command data is KeyNo || LenCap || PCDcap2,
// and AN12196 rev. 2.0 §5.6 Table 14 sends 9071000002 0000 00 — Lc of 2, i.e.
// KeyNo 00h and LenCap 00h with no capability bytes following.
const authFirstNoPCDCap = 0x00

// AuthenticateEV2FirstCommand returns the FIRST C-APDU of the handshake — ADR
// 0017 §5.1 step 4. The chip answers E(K, RndB) || 91AF; feed that 16-byte data
// field to EV2AuthPart1 (ev2.go), which does the cryptography.
//
// keyNo is the application key to authenticate with: 00h on a blank chip, where
// it is still the PUBLIC factory default (NT4H2421Gx rev. 3.0 §8.2.4.2, and ADR
// 0005 risk 7 — that is precisely why the whole session is assumed observed), or
// 01h for ADR 0017 §5.3's probe 2.
func AuthenticateEV2FirstCommand(keyNo byte) ([]byte, error) {
	if keyNo > changeKeyMaxKeyNo {
		return nil, fmt.Errorf("sun: apdu: key number must be 0..%d, got %d", changeKeyMaxKeyNo, keyNo)
	}
	return NativeAPDU(INSAuthenticateEV2First, []byte{keyNo, authFirstNoPCDCap})
}

// AuthenticateEV2FirstPart2Command returns the SECOND C-APDU of the handshake:
// the 32-byte E(K, RndA || RndB') that EV2AuthPart1 produced, sent under AFh.
// AN12196 rev. 2.0 §5.6 Table 14 step 14: 90AF000020 <32 bytes> 00.
//
// The chip answers E(Kx, TI || RndA' || PDcap2 || PCDcap2) || 9100; feed that
// 32-byte data field to EV2AuthPart2.
func AuthenticateEV2FirstPart2Command(part2Data []byte) ([]byte, error) {
	if len(part2Data) != 2*ev2RandLen {
		return nil, fmt.Errorf("sun: apdu: part 2 cryptogram must be %d bytes, got %d", 2*ev2RandLen, len(part2Data))
	}
	return NativeAPDU(INSAdditionalFrame, part2Data)
}

// GetCardUIDCommand returns the C-APDU of Cmd.GetCardUID — NT4H2421Gx rev. 3.0
// Table 22 (p. 44): CLA 90, INS 51h, no data, Le 00, CommMode.Full.
//
// 🔴 IT IS HERE FOR ONE REASON AND IT IS NOT CONVENIENCE. GetVersion's UID arrives
// unauthenticated, so a hostile relay (ADR 0017 §2.2) can substitute one and the
// encode flow would write a row for a chip that is not the chip being written —
// ADR 0017 §5.2's "chip written, row missing" mode, which the whole "row first"
// decision exists to prevent. GetCardUID is CommMode.Full, so its response carries
// a MAC: re-reading the UID inside the authenticated session DETECTS the
// substitution. It does not prevent the row — it turns a silent permanent loss into
// an abort-and-clean. It is wired in by internal/encode (M8-05 FAZ B2c-1); ADR 0017
// §6 md. 12 carries it as the fourth consequence.
//
// ⚠️ THE PLACEMENT NAMED HERE WAS "the session that exists after ADR 0017 §5.1
// step 6", AND THAT IS RETRACTED (2026-08-21). A security audit measured the
// after-step-6 slot as HARMFUL: by then the chip has already accepted WriteData and
// ChangeKey, so a substituted UID leaves a personalised chip whose key is in no row —
// §5.2's permanent loss, the mode the check exists to avoid. The gate now runs at
// ADR 0017 §5.1 step 4b: after authentication, BEFORE the first irreversible command.
// Detection power is unchanged, because §10.6.1 requires key 0 for ChangeKey and
// steps 5-8 therefore share one key-0 session either way.
//
// 🔴 AND THE SENTENCE THAT USED TO BE HERE CLAIMED MORE THAN IT EARNED — CORRECTED
// 2026-08-21, MEASURED, NOT REWORDED. It said the MAC is "computed with session
// keys the relay does not have". The relay DOES have them: that session is opened
// with application key 0, which on a blank chip is the PUBLIC factory default
// (§8.2.4.2), and ADR 0005 risk 7 is precisely the finding that whoever sees the
// dump re-derives RndA, RndB and therefore both session keys. Moving the check into
// a fresh key-0x01 session does not help either — the same dump carries step 6's
// ChangeKey body, from which the same observer recovers K_0x01.
//
// What the check earns, exactly: it separates a relay that lies by EDITING A STRING
// from one that implements EV2 secure messaging. Against ADR 0017 §2.2's fully
// hostile relay it detects nothing, and it never could — §2.2's own conclusion is
// that a channel bootstrapped from a public key carries neither confidentiality nor
// authenticity.
//
// The response is unwrapped with EV2UnwrapResponseFull; ev2_kat_test.go's
// TestEV2KAT_GetCardUIDRoundTripMatchesAN12196Table28 already pins both directions
// against AN12196's published example.
func GetCardUIDCommand(auth *EV2Auth, cmdCtr uint16) ([]byte, error) {
	field, err := EV2WrapCommandFull(auth, INSGetCardUID, cmdCtr, nil, nil)
	if err != nil {
		return nil, err
	}
	return NativeAPDU(INSGetCardUID, field)
}

// GetFileSettingsCommand returns the C-APDU of Cmd.GetFileSettings — ADR 0017
// §5.3's PROBE 1, the one diagnostic that reads what the chip actually holds.
//
// TRANSCRIBED — NT4H2421Gx rev. 3.0 §10.7.2 Table 72 (p. 69): "Cmd F5h", command
// header is the file number, no command data. AN12196 rev. 2.0 §5.4 Table 10
// step 4 publishes the assembled C-APDU for the NDEF file: 90F50000010200.
//
// 🔴 IT RUNS WITHOUT AUTHENTICATION, AND THAT IS MEASURED RATHER THAN INFERRED.
// Table 22 (p. 44) lists this command as CommMode.MAC, which reads like "needs a
// session" — the same wording it gives GetVersion, whose own section then says it
// is "freely accessible without secure messaging … [when] there is no active
// authentication" (§10.5.2). AN12196 settles it for GetFileSettings by DOING it:
// its personalisation sequence calls Get File Settings at step 4, two steps BEFORE
// AuthenticateFirst at step 6, and prints a plain response. So the mode applies to
// the authenticated state, not to the command.
//
// ⚠️ AND THAT IS WHY ADR 0017 §6 md. 13's Change = 0x01 DOES NOT BREAK THIS PROBE:
// reading the settings is not a Change-governed operation at all (datasheet
// Table 9 maps Change to ChangeFileSettings only).
func GetFileSettingsCommand(fileNo byte) ([]byte, error) {
	if fileNo > fileNoMax {
		return nil, fmt.Errorf("sun: getfilesettings: file number must be 0..%d, got %d", fileNoMax, fileNo)
	}
	return NativeAPDU(INSGetFileSettings, []byte{fileNo})
}

// --- Step 5: WriteData ---------------------------------------------------------

// fileNoMax is the largest file number. NT4H2421Gx rev. 3.0 Table 72: the file
// number occupies bits 4-0, and Table 5 (p. 10) creates exactly three files,
// 01h..03h. The wider bit-field bound is used rather than 3, because a future file
// is a datasheet change and a 5-bit field is the protocol.
const fileNoMax = 0x1F

// maxUint24 is the largest value a 3-byte field can carry. Offsets and lengths in
// the NTAG 424 DNA command set are all 3 bytes wide.
const maxUint24 = 1<<24 - 1

// writeDataHeaderLen is FileNo(1) || Offset(3) || Length(3).
const writeDataHeaderLen = 1 + 3 + 3

// writeDataMaxDataField is the largest ISO 7816 data field Cmd.WriteData accepts.
//
// 🔴 TRANSCRIBED, AND IT IS TIGHTER THAN apduMaxLc — WHICH IS THE POINT.
// NT4H2421Gx rev. 3.0 Table 81 (p. 75) gives WriteData's Data field as "up to 248
// byte including secure messaging". A short Lc can express 255, so the ISO
// envelope is NOT the binding constraint and a caller who only checked it would
// build a frame the chip answers with LENGTH_ERROR.
//
// 🔴 FOUND BY AUDIT, AND IT WAS AN UNLABELLED DERIVATION RATHER THAN A WRONG BYTE
// (2026-08-21). Nothing in this package claimed a template always fits one
// WriteData; nothing checked it either. Measured by the auditor: a 239-byte NDEF
// file assembles a 255-byte data field, which NativeAPDU accepted. Harmless today
// (the shipped template is 76 bytes) and NOT harmless once Q08 picks a long host,
// because the failure would land in the MIDDLE of an encode session — step 5 of
// ADR 0017 §5.1, with the row already written and the chip half-personalised.
//
// The ceiling on plaintext follows: field = header(7) + padded + MACt(8) <= 248
// gives padded <= 233, and padded is a multiple of 16 above the plaintext, so the
// largest writable body is 223 bytes. That number is not hard-coded — the check
// below is on the ASSEMBLED field, so it stays correct if a header ever changes.
const writeDataMaxDataField = 248

// appendUint24LE appends v as three bytes, LEAST significant first.
//
// 🔴 THE BYTE ORDER IS TRANSCRIBED AND THEN MEASURED, WHICH IS THE ONLY REASON IT
// IS ALLOWED TO BE HERE. NT4H2421Gx rev. 3.0 §10.7.1 Table 69 (pp. 65-67) labels
// every mirror position "Mirror position (LSB first)". The measurement is AN12196
// rev. 2.0 §5.8.2 Table 17 (rev. 1.8 §6.8.2 Table 18), whose WriteData CmdHeader
// is 02 000000 800000 for a body of 128 bytes: 800000 read LSB-first is 128,
// read MSB-first it is 8388608. TestAPDU_WriteDataHeaderMatchesAN12196Table17
// asserts the published header and TestAPDU_ThreeByteFieldsAreLSBFirst shows the
// MSB-first spelling differs, so a reversal cannot pass unnoticed — the mutation
// M2-08 shipped for four months.
func appendUint24LE(dst []byte, v uint32) []byte {
	return append(dst, byte(v), byte(v>>8), byte(v>>16))
}

// EV2WriteDataCommand builds the ISO 7816 data field of Cmd.WriteData inside an
// established EV2 session — ADR 0017 §5.1 step 5, the command that puts the NDEF
// URL template on the chip.
//
// fileNo is the file to write (NDEFFileNo for the tap URL), offset is where in
// the file the write starts, and data is the plaintext to write. The result is
// CmdHeader || E(KSesAuthENC, data || padding) || MACt, ready for NativeAPDU.
//
// TRANSCRIBED LAYOUT — NT4H2421Gx rev. 3.0 Table 81 (p. 75): the CmdHeader is
// FileNo || Offset || Length, the two 3-byte fields LSB first. AN12196 rev. 2.0
// §5.8.2 Table 17 publishes it assembled: 02 000000 800000.
//
// ⚠️ CITATION CORRECTED (2026-08-21, third-eye audit): this said "§10.8.2
// Table 74", and Table 74 is GetFileSettings' return code. Same class as the
// ISOSelectFile pointer above — right bytes, wrong page.
//
// 🔴 THE LENGTH FIELD IS THE PLAINTEXT LENGTH, AND AN EXISTING COMMENT IN THIS
// REPOSITORY SAID OTHERWISE. ev2_kat_test.go's katT17Header note called 800000
// "the ENCRYPTED length"; it is not. Measured here: the published CmdData is 128
// bytes and the published ciphertext is 144 (128 plus a whole extra padding
// block, §9.1.4). 0x80 is 128, so the header counts the bytes being WRITTEN, not
// the bytes on the wire. The document's own steps 4 and 12 print a third number
// (530000 = 83, the length of the meaningful NDEF content inside a 128-byte
// write), which is why this needed measuring rather than reading. The comment has
// been corrected in the same change as this function.
func EV2WriteDataCommand(auth *EV2Auth, cmdCtr uint16, fileNo byte, offset uint32, data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("sun: writedata: refusing to write an empty body")
	}
	if offset > maxUint24 {
		return nil, fmt.Errorf("sun: writedata: offset must fit in 3 bytes, got %d", offset)
	}
	// Compared as an int, NOT as uint32(len(data)): the conversion would TRUNCATE a
	// length above 2^32 and let it through as a small number. Absurd here — no
	// caller has a four-gigabyte NDEF body — but a truncating bounds check is the
	// shape of defect this package is not allowed to contain.
	if len(data) > maxUint24 {
		return nil, fmt.Errorf("sun: writedata: length must fit in 3 bytes, got %d", len(data))
	}
	header := make([]byte, 0, writeDataHeaderLen)
	header = append(header, fileNo)
	header = appendUint24LE(header, offset)
	header = appendUint24LE(header, uint32(len(data)))
	field, err := EV2WrapCommandFull(auth, INSWriteData, cmdCtr, header, data)
	if err != nil {
		return nil, err
	}
	if len(field) > writeDataMaxDataField {
		// Refused rather than truncated or chunked. Chunking is a real answer and
		// a real design question (which offsets, which counter values, what happens
		// when frame two fails), and it belongs to the round that needs it — not to
		// a silent loop here.
		return nil, fmt.Errorf("sun: writedata: %d-byte body seals into a %d-byte data field, more than the %d one command carries",
			len(data), len(field), writeDataMaxDataField)
	}
	return field, nil
}
