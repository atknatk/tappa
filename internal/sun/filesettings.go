package sun

import "fmt"

// Cmd.ChangeFileSettings — ADR 0017 §5.1 step 7, the command that switches SDM on
// and locks the NDEF file down. It is the highest-risk body in the encode flow and
// the reason is not difficulty, it is EVIDENCE:
//
// 🔴 NO PUBLISHED VECTOR BUILDS TAPPA'S CONFIGURATION. AN12196's only
// ChangeFileSettings example (rev. 1.8 §6.9 Table 19 / rev. 2.0 §5.9 Table 18)
// configures ENCRYPTED PICC data — "FileAR.SDMMetaRead: 0x2", a key number — and
// its body therefore carries PICCDataOffset. Tappa's plaques are PLAIN (ADR 0003
// md. 1, Q05), which means SDMMetaRead = Eh, and then Table 69 says the body
// carries UIDOffset and SDMReadCtrOffset INSTEAD. Two measurements in FAZ B1, by
// two separate auditors, found SDMMetaRead published only ever as 0x2 in either
// revision. There is no external vector for the bytes this file will actually
// send.
//
// 🔴 SO THE ANCHOR IS SPLIT IN TWO, DELIBERATELY, AND THE SPLIT IS THE WHOLE
// DESIGN OF THIS FILE. ONE builder produces BOTH configurations:
//
//	the published one   — TestFileSettingsKAT_ReproducesAN12196Table18 hands this
//	                      builder the encrypted-PICC configuration the document
//	                      describes and requires the exact 15 bytes it publishes,
//	                      4000E0C1F121200000430000430000. That EXTERNALLY pins the
//	                      field order, the FileOption and SDMOptions bit values,
//	                      both access-right byte compositions, the 3-byte LSB-first
//	                      offset encoding and the sealing.
//	Tappa's             — differs from it in a countable way: two nibble values and
//	                      which offsets are present. Everything else is
//	                      byte-identical to bytes NXP published.
//
// 🔴 WHAT IS THEREFORE STILL DERIVED, NOT TRANSCRIBED — the honest list, because
// M2-08 was exactly a derived byte order wearing a transcribed-looking comment:
//
//  1. That SDMMetaRead = Eh selects plain mirroring, and that it is what makes
//     UIDOffset and SDMReadCtrOffset present while making PICCDataOffset absent.
//     Read from NT4H2421Gx rev. 3.0 §10.7.1 Table 69 (p. 65-66), which states the
//     field-presence conditions. No vector exercises it.
//  2. The POSITION of UIDOffset and SDMReadCtrOffset in the field order — Table 69
//     lists them before SDMMACInputOffset, and the published body cannot show it
//     because neither field is present there.
//  3. That the SDM MAC covers a zero-length input when SDMMACInputOffset equals
//     SDMMACOffset. This one has a second leg: the published body sets both to
//     430000, i.e. the document's own example already MACs an empty input, which
//     is what ADR 0003 md. 2 fixes for Tappa. So it is derived for the meaning and
//     transcribed for the values.
//
// 🔴 EVERY NIBBLE POSITION IS NAMED IN A DOCUMENT, AND THIS FILE ONCE CLAIMED TWO
// OF THEM WERE UNKNOWABLE. That claim was wrong and a third-eye audit with the
// PDFs open refuted it in one page. Transcribed here so nobody re-derives it:
//
//	NT4H2421Gx rev. 3.0 §10.7.1 Table 69 (p. 66), SDMOptions:
//	  bit 7 UID · bit 6 SDMReadCtr · bit 5 SDMReadCtrLimit ·
//	  bit 4 SDMENCFileData · bits 3-1 RFU · bit 0 ASCII encoding
//	Same table, SDMAccessRights:
//	  bits 15-12 SDMMetaRead · 11-8 SDMFileRead · 7-4 RFU (Fh) · 3-0 SDMCtrRet
//	AN12196 rev. 2.0 §5.9 Table 18 step 7 labels the SAME nibbles in WIRE order:
//	  "F121h = SDMAccessRights (RFU: 0xF, FileAR.SDMCtrRet = 0x1,
//	   FileAR.SDMMetaRead: 0x2, FileAR.SDMFileRead: 0x1)"
//	NT4H2421Gx rev. 3.0 Table 7 gives the file AccessRights layout, which the same
//	  step 7 label confirms for 00E0h.
//
// The bytes this file emits were already right — the composition below reproduces
// every one of those labels. What was wrong was a COMMENT claiming the document
// could not settle it, and the cost was measurable: a mutation that swapped
// ReadWrite with Change SURVIVED, because no test asserted a configuration where
// the two differ. TestFileSettings_NibblePositionsAreTheOnesTable69Names now does,
// with distinct values in every field, and that mutation is red.
//
// ⚠️ ONE AMBIGUITY IS REAL AND IT IS A DIFFERENT ONE. AN12196's Table 18 step 7
// lists SDMMACInputOffset and SDMMACOffset in the OPPOSITE order to Table 69, and
// both offsets are 430000 in that example, so no published byte distinguishes
// their positions. That one is closed by REFUSAL — see the MAC-offset gate in
// ChangeFileSettingsData — rather than by an assertion nothing can justify.
//
// SOURCES:
//
//	NT4H2421Gx rev. 3.0, 31 Jan 2019 — §10.7.1 Table 69 (pp. 65-67) is the ONLY
//	  normative description of this body: field order, sizes, "Mirror position
//	  (LSB first)" and the field-presence conditions — those last are on p. 67,
//	  and they are what the emission gates below are built from. Table 8 (p. 12) gives the
//	  delivery access rights and Table 9 (p. 12) maps each right to the commands
//	  it governs.
//	AN12196 rev. 1.8 §6.9 Table 19 / rev. 2.0 §5.9 Table 18 — the one worked
//	  example, encrypted-PICC.

// Access-right values. NT4H2421Gx rev. 3.0 §8.2.3.3 Table 6 gives the range for a
// FILE access right — 0h..4h names an application key, Eh is free access and Fh is
// no access. (Table 69 carries the same range for the SDM rights; the file rights
// are Table 6's, and this comment used to cite the wrong one.)
const (
	// ARFree — Eh, free access, no authentication required. deploy/README's
	// settings table records the published example's FileAR.Read = 0xE as exactly
	// this ("serbest"), and it is what lets an anonymous browser read a tap URL.
	ARFree = 0x0E
	// ARNever — Fh, no access under any key. Not used by Tappa's configuration and
	// the reason is written at TappaNDEFSettings: a right nobody can exercise is
	// also a right WE cannot exercise, and Q08 is still open.
	ARNever = 0x0F
	// arMaxKeyNo is the highest key number an access right may name, matching
	// changekey.go's changeKeyMaxKeyNo.
	arMaxKeyNo = changeKeyMaxKeyNo
	// arRFU is the value Table 69 requires in SDMAccessRights' reserved nibble.
	arRFU = 0x0F
)

// CommMode values for the FileOption byte's low two bits (NT4H2421Gx rev. 3.0
// Table 69). Only Plain is used: a tap read arrives from an anonymous browser
// with no session, so anything else would make the plaque unreadable.
const (
	CommModePlain = 0x00
	CommModeMAC   = 0x01
	CommModeFull  = 0x03
)

const (
	// SDMFileReadKeyNo is the application key that signs SUN — ADR 0017 §5.0
	// karar 1 (2026-08-20), and deploy/README's settings table carries the same
	// decision. It must not be 0: key 0 is the AppMasterKey (datasheet §8.2.4.1),
	// and binding SDM reads to it would tie two authorities to one secret.
	SDMFileReadKeyNo = 0x01

	// SDMMetaReadPlain is the SDMMetaRead value that selects PLAIN mirroring —
	// UID and read counter written into the URL in the clear rather than as an
	// encrypted PICCData blob. NT4H2421Gx rev. 3.0 §10.7.1 Table 69; ADR 0003
	// md. 1 (Q05) is the decision that Tappa uses it.
	SDMMetaReadPlain = ARFree

	// fileOptionSDMBit — Table 69's FileOption bit 6, "SDM and mirroring enabled".
	// Pinned by the published body's first byte, 40h, in a configuration the
	// document describes as SDM-enabled with CommMode plain.
	fileOptionSDMBit = 0x40

	// SDMOptions bits — NT4H2421Gx rev. 3.0 §10.7.1 Table 69 (p. 66) names them:
	// bit 7 UID, bit 6 SDMReadCtr, bit 5 SDMReadCtrLimit, bit 4 SDMENCFileData,
	// bits 3-1 RFU, bit 0 ASCII encoding. Three are implemented and the published
	// body's C1h sets exactly those three.
	//
	// ⚠️ THIS SAID "WHICH BIT IS WHICH IS A READING" AND IT WAS NOT. The table
	// names every bit; the round that wrote it did not have the PDF open. Both the
	// byte and the assignment are now pinned — by
	// TestFileSettings_TappaOptionsByteEqualsThePublishedOne for the byte and by
	// TestFileSettings_NibblePositionsAreTheOnesTable69Names for the assignment.
	sdmOptionUIDMirror     = 0x80
	sdmOptionReadCtrMirror = 0x40
	sdmOptionASCII         = 0x01

	// fileTypeStandardData — NT4H2421Gx rev. 3.0 Table 73 (p. 70): "00h
	// StandardData File", every other value RFU. All three files on this chip are
	// StandardData (Table 5, p. 10).
	fileTypeStandardData = 0x00

	// fileOptionCommModeMask and fileOptionRFUMask split the FileOption byte the
	// way Table 73 describes it: bit 7 RFU, bit 6 SDM, bits 5-2 fixed at 0000b,
	// bits 1-0 CommMode.
	fileOptionCommModeMask = 0x03
	fileOptionRFUMask      = 0xBC

	// sdmOptionReadCtrLimit and sdmOptionENCFileData are the two SDMOptions bits
	// this package models only well enough to REFUSE (Table 69, p. 66).
	sdmOptionReadCtrLimit = 0x20
	sdmOptionENCFileData  = 0x10

	// changeFileSettingsMaxBody bounds the assembled body. The two configurations
	// this builder produces are 15 bytes (encrypted PICC) and 18 (plain); the
	// ceiling exists so a future field cannot silently grow past what a short Lc
	// can frame once encrypted.
	changeFileSettingsMaxBody = 64
)

// SDMFileSettings is the CmdData of Cmd.ChangeFileSettings, field for field as
// NT4H2421Gx rev. 3.0 §10.7.1 Table 69 lays it out.
//
// 🔴 THREE OF TABLE 69'S FIELDS ARE NOT MODELLED, AND THAT IS FAIL-CLOSED RATHER
// THAN INCOMPLETE. SDMENCFileData (with SDMENCOffset and SDMENCLength) and
// SDMReadCtrLimit (with its 3-byte limit) have no place in Tappa's configuration
// and no published vector here. Leaving them out makes them UNREPRESENTABLE: this
// struct cannot describe a body it would then have to build without evidence. A
// round that needs them adds the fields and the vector together.
type SDMFileSettings struct {
	// FileNo is the file whose settings change. It travels as the CmdHeader, not
	// in this body, and lives here so a caller cannot pair the wrong two.
	FileNo byte

	// CommMode is the file's communication mode: CommModePlain for a tap URL.
	CommMode byte

	// The four file access rights. Each is a key number 0..4, ARFree or ARNever.
	Read      byte
	Write     byte
	ReadWrite byte
	Change    byte

	// SDMEnabled is FileOption bit 6. When false, every field below is omitted and
	// the body is just FileOption || AccessRights.
	SDMEnabled bool

	// The three SDMOptions bits this builder models.
	UIDMirror     bool
	ReadCtrMirror bool
	ASCIIEncoding bool

	// SDMMetaRead selects plain mirroring (SDMMetaReadPlain) or names the key that
	// encrypts PICCData (0..4). SDMFileRead names the key that signs the SUN MAC,
	// or ARNever for none. SDMCtrRet is the read counter retrieval right.
	SDMMetaRead byte
	SDMFileRead byte
	SDMCtrRet   byte

	// Mirror positions, absolute byte offsets into the NDEF file (see ndef.go).
	// Which of them is emitted is decided by the presence conditions in
	// ChangeFileSettingsData, not by whether a caller filled the field in.
	UIDOffset      uint32
	ReadCtrOffset  uint32
	PICCDataOffset uint32
	MACInputOffset uint32
	MACOffset      uint32
}

// TappaNDEFSettings is the NORMATIVE Tappa plaque configuration, built from the
// template that was just written to the chip so the offsets cannot drift apart
// from the bytes they index.
//
// 🔴 THE ACCESS RIGHTS ARE A DECISION THIS ROUND MADE, AND IT CLOSES ADR 0017 §6
// md. 13. That item said FileAR.Change and FileAR.ReadWrite had never been
// decided while §5.1 step 7 was already written normatively as "lock writing".
// The decision, dated 2026-08-21:
//
//	Read      = Eh (free)          — an anonymous browser must be able to read the
//	                                 URL, or no tap ever happens.
//	Write     = ReadWrite = Change = SDMFileReadKeyNo (01h)
//
// 🔴 WHY ALL THREE, AND WHY THE SAME KEY — four measurements, in the order they
// settle the question:
//
//  1. WRITE ALONE DOES NOT LOCK WRITING. Datasheet Table 9 maps BOTH Write and
//     ReadWrite to WriteData/ISOUpdateBinary, and Table 8 gives both as Eh at
//     delivery. Locking one leaves the other open, and ADR 0017 §6 md. 13 already
//     counted that.
//  2. CHANGE MUST MOVE TOO, OR THE OTHER TWO ARE COSMETIC. Table 9 maps Change to
//     ChangeFileSettings itself. Left at its delivery value of 0h it would let
//     anyone authenticating with the PUBLIC factory key 0 set Write and ReadWrite
//     back to Eh and then write. Locking Write while leaving Change open is a
//     bolt on a door whose hinges anybody may unscrew.
//  3. KEY 01h, NOT KEY 00h — because key 0 is public TODAY. ADR 0017 §5.0 karar 2
//     personalises key 0, but shipping it is blocked on a schema decision (§6
//     md. 5), so until that lands key 0 is the factory default: known to everyone
//     who has read §8.2.4.2. Locking writes to it is deploy/README's own image,
//     the key under the doormat. Key 01h is per-plaque random (ADR 0003 md. 3)
//     and lives only KEK-wrapped in tags.aes_key_ref.
//  4. NOT Fh ("never"), THOUGH IT LOOKS STRICTLY SAFER. Fh would make the NDEF
//     immutable and irreversible — and Q08 is still open (ADR 0017 §6 md. 11): the
//     host in the URL is not decided, so a plaque may need its URL rewritten
//     before it ever reaches a wall. With key 01h we can rewrite it; with Fh every
//     Q08 correction becomes a plaque replacement. The same applies to Change: Fh
//     would freeze the SDM configuration, including ADR 0017 §5.3's recovery from
//     a half-written chip.
//
// ⚠️ ONE OBJECTION ANSWERED BEFORE IT IS RAISED: LOCKING WRITES DOES NOT SILENCE
// THE PLAQUE. SDM mirroring is the chip stamping its own file during a READ; it is
// not a WriteData and no file access right governs it. The published example is
// the evidence rather than the reasoning — AN12196's configuration locks
// FileAR.Write to a key (0h) with SDM enabled and mirroring working. What a tap
// needs is Read, and Read stays Eh.
//
// 🔴 THE ORDERING IS COMPATIBLE WITH ADR 0017 §5.1 AND THAT WAS CHECKED, NOT
// ASSUMED. Change is 0h at delivery (Table 8), so the FIRST ChangeFileSettings —
// step 7 — runs in the step-4 session, authenticated with key 0. It is the same
// command that moves Change to 01h, so it is also the LAST one that needs key 0.
// Step 5's WriteData already ran before it, under the delivery Write = Eh. Nothing
// later in the sequence needs either right: step 8 is ChangeKey, and ChangeKey is
// governed by the AppMasterKey (datasheet §8.2.4.1), not by any file access right.
//
// 🔴 RECOVERY STILL REACHES EVERY HALF-WRITTEN STATE (ADR 0017 §5.3), COUNTED ONE
// STATE AT A TIME:
//
//	steps 6,7 not run — key 0 is factory, Change is 0h: authenticate with key 0 and
//	                    run everything. Unchanged.
//	step 6 run, 7 not — Change is still 0h, so ChangeFileSettings still takes key 0,
//	                    which is still factory. Unchanged.
//	steps 6,7 run     — writing and re-configuring now need key 01h, which is in the
//	                    row (ADR 0017 §5.2 writes the row FIRST, precisely so this
//	                    is true). ChangeKey for key 0 still takes factory key 0.
//	                    Reachable.
//
// ⚠️ ONE THING IT COSTS, COUNTED RATHER THAN OMITTED. In the "chip written, row
// lost" mode — the one ADR 0017 §5.2 already calls permanent plaque loss — a chip
// with Change = 0h could in principle be salvaged by pointing SDMFileRead at a
// still-factory key slot. With Change = 01h that salvage is gone too. It is a
// scrap-value loss on a plaque the ADR already writes off, and it buys the closure
// below.
//
// 🔴 EFFECT ON ADR 0005 RISK 8, ITEMISED — AND THE FIRST WRITING OF THIS LIST WAS
// TOO BROAD IN EXACTLY THE WAY A SECURITY AUDIT CAUGHT. It said "CANNOT rewrite
// the NDEF URL" with no qualifier. The qualifier is the whole claim:
//
//	🔴 THIS LIST DESCRIBES AN ATTACKER HOLDING **ONLY THE PUBLIC FACTORY KEY 0**.
//	It does NOT describe the party ADR 0005 risk 7 already grants K_0x01 to. Risk 7
//	says every encode session and every recovery session leaks the plaque key to
//	whoever sees the APDU dump, and after this decision that key is no longer only
//	a SUN-signing key — it is also the file WRITE and RECONFIGURE authority. So the
//	same party can AuthenticateEV2First(0x01) (apdu.go), then WriteData, then
//	repoint the NDEF host: phishing, with no detection signal, because a repointed
//	plaque's tap never reaches us at all. Risk 7's own mitigation sentence — "the
//	other three evidences keep binding" — does not apply there either, for the same
//	reason. That is written into ADR 0005 risk 7 rather than left here.
//
//	The decision is still right, and the alternatives are worse rather than better:
//	key 00h is public today, delivery Eh is public to everyone, and Fh is refused
//	for the Q08 and §5.3 reasons above. Narrowing the set of people who can rewrite
//	a plaque from "anyone who has read the data sheet" to "anyone who saw one
//	specific encode session" is a real reduction; calling it closure was not.
//
// So, for an attacker with physical access and ONLY the public factory key 0:
//
//	CANNOT rewrite the NDEF URL          — WriteData needs 01h. Against THIS
//	                                       attacker the phishing vector is closed;
//	                                       against risk 7's set it is not.
//	CANNOT disable or re-point SDM       — ChangeFileSettings needs 01h.
//	CANNOT learn or set key 01h          — ChangeKey for keys 1..4 needs
//	                                       (New XOR Old) and CRC32 over New
//	                                       (Table 63); without the old key neither
//	                                       can be computed, so the chip rejects.
//	CAN    overwrite key 0 and keys 2..4 — their old value is the public zero, so
//	                                       the body is computable. That costs us
//	                                       our own future key-0 personalisation and
//	                                       the factory-key half of recovery: a
//	                                       denial of service, resolved by
//	                                       retire + replace, not a forgery.
//	CAN    still read the file           — Read is Eh by design; the URL is public.
//
// 🔴 AND THE ONE THING THIS COMMENT ORIGINALLY LEFT AS "NOT MEASURED" HAS SINCE
// BEEN MEASURED, WITH A PERMANENT ITEM INSIDE IT. Cmd.SetConfiguration needs an
// authentication with the AppMasterKey (datasheet §10.5.1) — so it is reachable
// with the public factory key 0 — and Table 50's option 05h enables LRP mode,
// which the same table calls permanent: "LRP mode cannot be disabled afterwards".
// Under LRP the SDM MAC is computed by §9.3.8.2's separate construction, i.e. a
// plaque this package's verifier could never read again. The reassuring half was
// measured too: Random ID does NOT break plain UID mirroring (datasheet p. 12), so
// it cannot silently break taps. None of it changes the CLASS — still denial of
// service, still retire + replace — but "not measured" was the wrong sentence and
// it is the same wrong sentence, twice, in one round. Full account: ADR 0005 risk 8.
//
// 🔴 AND THE SAME SCAN FOUND SOMETHING THIS DECISION DOES NOT COVER AT ALL, WHICH
// IS RECORDED HERE BECAUSE IT IS THE SAME "CANNOT" GETTING TOO BROAD. This
// decision locks FILE 02h. Table 8 (p. 12) gives file 01h — the Capability
// Container, which is what tells a phone WHERE the NDEF message lives — Write and
// ReadWrite of 0h, and file 03h (proprietary) 3h. On a chip whose keys 0 and 2..4
// are still the factory zeros (datasheet §8.2.4.2; ADR 0017 §6 md. 5 leaves keys
// 2..4 undecided), both of those are writable by anyone. Nothing here stops that,
// and it is not hypothetical plumbing: a CC file rewritten to point at file 03h,
// whose content the same public key can write, is a phishing path that never
// touches file 02h. Locking files 01h and 03h — or personalising keys 2..4 — is
// the round that closes it; this one names it.
func TappaNDEFSettings(t *TapNDEF) (SDMFileSettings, error) {
	if t == nil {
		return SDMFileSettings{}, fmt.Errorf("sun: filesettings: no NDEF template")
	}
	return SDMFileSettings{
		FileNo:        NDEFFileNo,
		CommMode:      CommModePlain,
		Read:          ARFree,
		Write:         SDMFileReadKeyNo,
		ReadWrite:     SDMFileReadKeyNo,
		Change:        SDMFileReadKeyNo,
		SDMEnabled:    true,
		UIDMirror:     true,
		ReadCtrMirror: true,
		ASCIIEncoding: true,
		SDMMetaRead:   SDMMetaReadPlain,
		SDMFileRead:   SDMFileReadKeyNo,
		// 🔴 SDMCtrRet IS A DECISION, NOT A CONSEQUENCE — AND THE COMMENT THAT USED
		// TO BE HERE SAID THE OPPOSITE. It claimed the value was forced, because the
		// published vector could not tell this nibble apart from SDMFileRead's.
		// Table 69 names the nibble outright, so there was nothing to be forced by.
		//
		// The decision, dated 2026-08-21: SDMCtrRet = key 01h. Datasheet Table 9
		// maps this right to GetFileCounters — reading the plaque's SDM read counter
		// OUT OF THE CHIP with a command. No Tappa flow uses it; the counter we act
		// on arrives in the tap URL and is checked against tags.last_ctr (CLAUDE.md
		// §4.4). So nothing is lost by locking it, and Eh (free) would hand anyone
		// holding the plaque a reading of how often that door has been tapped, which
		// is attendance information about a location.
		SDMCtrRet:     SDMFileReadKeyNo,
		UIDOffset:     t.UIDOffset,
		ReadCtrOffset: t.ReadCtrOffset,
		// ADR 0003 md. 2: SDMMACInputOffset == SDMMACOffset, so the MAC covers a
		// ZERO-LENGTH input. deploy/README warns in its own box not to "fix" this
		// by pointing the input somewhere — the freshness comes from the session
		// key derivation, which already contains the UID and the counter.
		MACInputOffset: t.MACOffset,
		MACOffset:      t.MACOffset,
	}, nil
}

// ChangeFileSettingsData builds the CmdData of Cmd.ChangeFileSettings.
//
// TRANSCRIBED FIELD ORDER — NT4H2421Gx rev. 3.0 §10.7.1 Table 69 (pp. 65-67; the
// field-presence conditions are on p. 67):
//
//	FileOption          1 byte
//	AccessRights        2 bytes
//	[SDMOptions]        1 byte   if FileOption bit 6 (SDM) is set
//	[SDMAccessRights]   2 bytes  if SDM
//	[UIDOffset]         3 bytes  if SDM and UID mirror and SDMMetaRead == Eh
//	[SDMReadCtrOffset]  3 bytes  if SDM and read-counter mirror and SDMMetaRead == Eh
//	[PICCDataOffset]    3 bytes  if SDM and SDMMetaRead is a key number (0h..4h)
//	[SDMMACInputOffset] 3 bytes  if SDM and SDMFileRead != Fh
//	[SDMMACOffset]      3 bytes  if SDM and SDMFileRead != Fh
//
// with every 3-byte position written "Mirror position (LSB first)".
//
// 🔴 THE TWO-BYTE RIGHTS FIELDS ARE COMPOSED AS BYTES, NOT AS A NUMBER, AND THAT
// IS ON PURPOSE. Writing them as a 16-bit value forces a choice of endianness on
// top of a choice of nibble order, and the two choices cancel: the layout
// [ReadWrite|Change|Read|Write] written MSB-first and the layout
// [Read|Write|ReadWrite|Change] written LSB-first produce IDENTICAL bytes, always.
// A comment that picked one would be asserting a distinction the wire cannot
// carry — the shape of claim M2-08 was. What the wire does carry, and what the
// published body pins, is: first byte (ReadWrite<<4)|Change, second byte
// (Read<<4)|Write. That reproduces 00E0 for the example's Read = Eh with the other
// three rights zero.
func ChangeFileSettingsData(s SDMFileSettings) ([]byte, error) {
	if s.CommMode != CommModePlain && s.CommMode != CommModeMAC && s.CommMode != CommModeFull {
		return nil, fmt.Errorf("sun: filesettings: communication mode must be 0, 1 or 3, got %d", s.CommMode)
	}
	for _, r := range []struct {
		name string
		v    byte
	}{{"read", s.Read}, {"write", s.Write}, {"readwrite", s.ReadWrite}, {"change", s.Change}} {
		if r.v > 0x0F {
			return nil, fmt.Errorf("sun: filesettings: %s access right must be a nibble, got %d", r.name, r.v)
		}
		if r.v > arMaxKeyNo && r.v != ARFree && r.v != ARNever {
			return nil, fmt.Errorf("sun: filesettings: %s access right must be 0..%d, %d or %d, got %d", r.name, arMaxKeyNo, ARFree, ARNever, r.v)
		}
	}

	out := make([]byte, 0, changeFileSettingsMaxBody)
	fileOption := s.CommMode
	if s.SDMEnabled {
		fileOption |= fileOptionSDMBit
	}
	out = append(out, fileOption)
	out = append(out, s.ReadWrite<<4|s.Change, s.Read<<4|s.Write)

	if !s.SDMEnabled {
		// Table 69: with SDM off the body ends here. Tappa never sends this, but
		// refusing to build it would make "turn SDM back off" impossible to express
		// for a recovery that ever needs it.
		return out, nil
	}

	// 🔴 TWO VALUE GATES, THE SAME SHAPE changekey.go USES AND FOR THE SAME REASON:
	// Table 69 checks ranges, so both of the bodies these gates refuse are
	// well-formed protocol the chip would accept. Key 0 is the AppMasterKey
	// (datasheet §8.2.4.1). Naming it as the SDM read key or the PICCData key would
	// tie two authorities to one secret — a leaked plaque key would not merely
	// forge SUN, it would open the chip's NDEF and all five of its keys. ADR 0017
	// §5.0 karar 1 decided against it; this is that decision made mechanical.
	if s.SDMMetaRead == appMasterKeyNo {
		return nil, fmt.Errorf("sun: filesettings: refusing to bind SDM meta read to the application master key")
	}
	if s.SDMFileRead == appMasterKeyNo {
		return nil, fmt.Errorf("sun: filesettings: refusing to bind SDM file read to the application master key")
	}
	if s.SDMMetaRead > 0x0F || s.SDMFileRead > 0x0F || s.SDMCtrRet > 0x0F {
		return nil, fmt.Errorf("sun: filesettings: SDM access rights must be nibbles")
	}
	if s.SDMMetaRead != SDMMetaReadPlain && s.SDMMetaRead > arMaxKeyNo {
		return nil, fmt.Errorf("sun: filesettings: SDM meta read must be 0..%d or %d, got %d", arMaxKeyNo, ARFree, s.SDMMetaRead)
	}
	if s.SDMFileRead != ARNever && s.SDMFileRead > arMaxKeyNo {
		return nil, fmt.Errorf("sun: filesettings: SDM file read must be 0..%d or %d, got %d", arMaxKeyNo, ARNever, s.SDMFileRead)
	}
	// 🔴 SDMCtrRet HAD NO RANGE GATE WHILE ALL THREE OF ITS SIBLINGS DID, AND AN
	// AUDIT PROBE WENT STRAIGHT THROUGH IT (2026-08-21): SDMCtrRet = 07h produced
	// the body 4011E1F7E1…, which is a value Table 6's range does not contain. The
	// chip answers PARAMETER_ERROR (Table 71) and, per §9.1.10, the authentication
	// state dies with it — so this would fail at ADR 0017 §5.1 step 7 with steps 5
	// and 6 already committed to the chip. Exactly the half-written state §5.3
	// exists to recover from, produced by a typo this file was supposed to make
	// unrepresentable.
	if s.SDMCtrRet != ARFree && s.SDMCtrRet != ARNever && s.SDMCtrRet > arMaxKeyNo {
		return nil, fmt.Errorf("sun: filesettings: SDM counter retrieval must be 0..%d, %d or %d, got %d", arMaxKeyNo, ARFree, ARNever, s.SDMCtrRet)
	}
	if s.SDMFileRead != ARNever && s.MACInputOffset != s.MACOffset {
		// 🔴 EQUAL, AND THE REASON IS ADR 0003 md. 2 — NOT, AS AN EARLIER VERSION OF
		// THIS COMMENT CLAIMED, BECAUSE THE FIELD ORDER IS UNKNOWABLE. That claim was
		// made from AN12196 alone, where §5.9 Table 18 step 7's label list really
		// does print "430000h = SDMMACOffset" before "430000h = SDMMACInputOffset".
		// The datasheet settles it TWICE in the other direction: Table 69 (p. 66) for
		// the command body and Table 73 (p. 70) for the GetFileSettings response both
		// put SDMMACInputOffset first. AN12196's list is a description, not a byte
		// string, and it mislabels its offsets on the neighbouring page too (see
		// ParseFileSettings). The datasheet wins, and this file emits input-first.
		//
		// What the gate is actually for: ADR 0003 md. 2 fixes a ZERO-LENGTH SDM MAC
		// input for Tappa, and deploy/README carries a whole box warning that
		// "fixing" it by pointing the input somewhere makes every tap fail in a way
		// that looks like a wrong key. Refusing the configuration is cheaper than
		// documenting it. A round that genuinely needs a non-empty MAC input can lift
		// this — the order it would emit is transcribed above, not guessed.
		//
		// ⚠️ ONE CONSEQUENCE, SO A MUTATION LEDGER IS NOT MISREAD: because every
		// body this builder produces sets the two equal, swapping their emission
		// order changes no byte and no test can catch it. That is a property of the
		// gate, not a gap in the evidence.
		return nil, fmt.Errorf("sun: filesettings: the MAC input and MAC offsets must be equal, a zero-length input (got %d and %d)", s.MACInputOffset, s.MACOffset)
	}

	sdmOptions := byte(0)
	if s.UIDMirror {
		sdmOptions |= sdmOptionUIDMirror
	}
	if s.ReadCtrMirror {
		sdmOptions |= sdmOptionReadCtrMirror
	}
	if s.ASCIIEncoding {
		sdmOptions |= sdmOptionASCII
	}
	out = append(out, sdmOptions)
	out = append(out, arRFU<<4|s.SDMCtrRet, s.SDMMetaRead<<4|s.SDMFileRead)

	// The presence conditions of Table 69, in its order. Each offset is checked
	// for range only when it is actually emitted, so an unused field left at any
	// value cannot fail a build it does not appear in.
	plainMeta := s.SDMMetaRead == SDMMetaReadPlain
	type field struct {
		emit bool
		name string
		off  uint32
	}
	for _, f := range []field{
		{plainMeta && s.UIDMirror, "UID mirror", s.UIDOffset},
		{plainMeta && s.ReadCtrMirror, "read counter mirror", s.ReadCtrOffset},
		{!plainMeta, "PICC data", s.PICCDataOffset},
		{s.SDMFileRead != ARNever, "MAC input", s.MACInputOffset},
		{s.SDMFileRead != ARNever, "MAC", s.MACOffset},
	} {
		if !f.emit {
			continue
		}
		if f.off > maxUint24 {
			return nil, fmt.Errorf("sun: filesettings: %s offset must fit in 3 bytes, got %d", f.name, f.off)
		}
		out = appendUint24LE(out, f.off)
	}

	if len(out) > changeFileSettingsMaxBody {
		return nil, fmt.Errorf("sun: filesettings: body is %d bytes, more than the %d this builder allows", len(out), changeFileSettingsMaxBody)
	}
	return out, nil
}

// EV2ChangeFileSettingsCommand builds the complete ISO 7816 data field of
// Cmd.ChangeFileSettings inside an established EV2 session: the file number as
// CmdHeader, the encrypted body, and the truncated command MAC.
//
// TRANSCRIBED — AN12196 rev. 2.0 §5.9 Table 18 step 12 prints the assembled MAC
// input for this command, 5F 0100 9D00C4DF 02 || E(KSesAuthENC, CmdData): the
// CmdHeader is the file number alone, one byte. NT4H2421Gx rev. 3.0 §10.7.1 says
// the command is CommMode.Full, which is why nothing bespoke happens to the
// sealing here — EV2WrapCommandFull does all of it.
//
// cmdCtr is the counter for THIS command (see EV2WrapCommandFull).
func EV2ChangeFileSettingsCommand(auth *EV2Auth, cmdCtr uint16, s SDMFileSettings) ([]byte, error) {
	body, err := ChangeFileSettingsData(s)
	if err != nil {
		return nil, err
	}
	return EV2WrapCommandFull(auth, INSChangeFileSettings, cmdCtr, []byte{s.FileNo}, body)
}

// --- Reading settings back: ADR 0017 §5.3's probe 1 ----------------------------

// FileSettings is the response of Cmd.GetFileSettings: the two fields that exist
// only in the response direction, plus the same configuration ChangeFileSettings
// writes.
type FileSettings struct {
	// FileType — NT4H2421Gx rev. 3.0 Table 73 (p. 70): 00h StandardData File,
	// "other values RFU".
	FileType byte
	// FileSize is the file's capacity, 3 bytes LSB first. For the NDEF file this
	// is 256 (Table 5, p. 10) and AN12196 rev. 2.0 §5.4 Table 11 prints the field
	// as 000100 with the meaning "256d" — which is a second, independent reading
	// of the same LSB-first 3-byte encoding the mirror offsets use.
	FileSize uint32
	// SDMFileSettings is byte-for-byte the configuration ChangeFileSettingsData
	// builds; FileNo is filled from the caller's request, since the response does
	// not echo it.
	SDMFileSettings
}

// ParseFileSettings decodes a Cmd.GetFileSettings response — ADR 0017 §5.3 probe 1,
// the diagnostic that answers "did step 7 run, and with the offsets we meant".
//
// data is the R-APDU payload WITHOUT its status word.
//
// TRANSCRIBED LAYOUT — NT4H2421Gx rev. 3.0 §10.7.2 Table 73 (p. 70):
//
//	FileType(1) || FileOption(1) || AccessRights(2) || FileSize(3)
//	  [|| SDMOptions(1) || SDMAccessRights(2) || UIDOffset(3) || SDMReadCtrOffset(3)
//	   || PICCDataOffset(3) || SDMMACInputOffset(3) || SDMENCOffset(3)
//	   || SDMENCLength(3) || SDMMACOffset(3) || SDMReadCtrLimit(3)]
//
// with the SAME field-presence conditions Table 69 gives for the command
// direction, spelled out again per field. That repetition is load-bearing here:
// it is the second normative statement of the order this package emits, and it
// lists SDMMACInputOffset BEFORE SDMMACOffset — see the note in
// ChangeFileSettingsData about AN12196 printing those two the other way round.
//
// 🔴 THE PUBLISHED R-APDU STRING IS NOT USED AS A VECTOR, AND THE REASON IS
// MEASURED — BUT THE ARITHMETIC WAS CREDITED TO THE WRONG TABLE AND IS CORRECTED
// HERE (2026-08-21, third audit). AN12196 rev. 2.0 §5.4 Table 10 step 5 prints
// 004300E0000100C1F1212000004300009100 — 16 data bytes plus the status word. That
// is NOT a length disagreement with its own Table 11: Table 11's value column adds
// up to 16 as well (1+1+2+3+1+2+3+3). The contradiction is with the DATASHEET:
// applying NT4H2421Gx Table 73's presence conditions to Table 11's own
// SDMAccessRights value of F121h — SDMMetaRead = 2h, a key number — requires
// PICCDataOffset, SDMMACInputOffset and SDMMACOffset, i.e. 7+3+3+3+3 = 19 data
// bytes, and only two 3-byte offsets are present. There is a second disagreement
// on top of it: where Table 11 says FileOption is 40h (CommMode.Plain) the string
// carries 43h (CommMode.Full). This is the fourth self-contradiction this
// repository has had to arbitrate in these documents (the WriteData length, the
// response MAC's Cmd byte, and Table 11's own mislabelling of the offsets are the
// others), and the rule has been the same every time: follow whichever side other
// published bytes corroborate. Here that is Table 11's field list, because its
// values are exactly the CmdData of §5.9 Table 18 — a string this package already
// reproduces byte for byte.
//
// ⚠️ AND THE PARSER'S CORRECTNESS DOES NOT REST ON THAT ARBITRATION AT ALL, WHICH
// AN EARLIER "what I doubt" note got wrong in the pessimistic direction. What this
// function IMPLEMENTS is NT4H2421Gx rev. 3.0 Figure 23 and Table 73 — the
// normative description of the chip's response — and AN12196 has no authority over
// that format. If it turned out the printed string were the honest one and Table
// 11 the typo, the CHOICE OF VECTOR here would be wrong; the SPLIT would not.
func ParseFileSettings(fileNo byte, data []byte) (*FileSettings, error) {
	const prefixLen = 1 + 1 + 2 + 3
	if len(data) < prefixLen {
		return nil, fmt.Errorf("sun: filesettings: response must be at least %d bytes, got %d", prefixLen, len(data))
	}
	fs := &FileSettings{FileType: data[0]}
	if fs.FileType != fileTypeStandardData {
		return nil, fmt.Errorf("sun: filesettings: file type %02X is not the standard data file %02X", fs.FileType, fileTypeStandardData)
	}
	fileOption := data[1]
	if fileOption&fileOptionRFUMask != 0 {
		// Table 73 fixes bits 5-2 at 0000b and bit 7 as RFU. A set bit means the
		// response is not the shape this parser was written against, and guessing
		// past it would silently mis-split every field after it.
		return nil, fmt.Errorf("sun: filesettings: file option %02X sets a reserved bit", fileOption)
	}
	fs.FileNo = fileNo
	fs.CommMode = fileOption & fileOptionCommModeMask
	fs.SDMEnabled = fileOption&fileOptionSDMBit != 0
	fs.ReadWrite, fs.Change = data[2]>>4, data[2]&0x0F
	fs.Read, fs.Write = data[3]>>4, data[3]&0x0F
	fs.FileSize = uint24LE(data[4:7])

	rest := data[prefixLen:]
	if !fs.SDMEnabled {
		if len(rest) != 0 {
			return nil, fmt.Errorf("sun: filesettings: SDM is off but %d bytes follow the file size", len(rest))
		}
		return fs, nil
	}
	if len(rest) < 3 {
		return nil, fmt.Errorf("sun: filesettings: SDM is on but only %d bytes follow the file size", len(rest))
	}
	sdmOptions := rest[0]
	if sdmOptions&(sdmOptionReadCtrLimit|sdmOptionENCFileData) != 0 {
		// Deliberately refused rather than parsed: SDMFileSettings cannot represent
		// those configurations (see its doc comment), so a parser that skipped their
		// offsets would return settings that do not describe the chip.
		return nil, fmt.Errorf("sun: filesettings: SDM options %02X select a configuration this package does not model", sdmOptions)
	}
	fs.UIDMirror = sdmOptions&sdmOptionUIDMirror != 0
	fs.ReadCtrMirror = sdmOptions&sdmOptionReadCtrMirror != 0
	fs.ASCIIEncoding = sdmOptions&sdmOptionASCII != 0
	fs.SDMCtrRet = rest[1] & 0x0F
	fs.SDMMetaRead, fs.SDMFileRead = rest[2]>>4, rest[2]&0x0F
	if rfu := rest[1] >> 4; rfu != arRFU {
		return nil, fmt.Errorf("sun: filesettings: reserved SDM nibble is %X, not %X", rfu, arRFU)
	}

	offsets := rest[3:]
	plainMeta := fs.SDMMetaRead == SDMMetaReadPlain
	targets := []struct {
		emit bool
		dst  *uint32
	}{
		{plainMeta && fs.UIDMirror, &fs.UIDOffset},
		{plainMeta && fs.ReadCtrMirror, &fs.ReadCtrOffset},
		{!plainMeta, &fs.PICCDataOffset},
		{fs.SDMFileRead != ARNever, &fs.MACInputOffset},
		{fs.SDMFileRead != ARNever, &fs.MACOffset},
	}
	for _, t := range targets {
		if !t.emit {
			continue
		}
		if len(offsets) < 3 {
			return nil, fmt.Errorf("sun: filesettings: response ends before all the mirror positions its own options require")
		}
		*t.dst = uint24LE(offsets[:3])
		offsets = offsets[3:]
	}
	if len(offsets) != 0 {
		return nil, fmt.Errorf("sun: filesettings: %d bytes follow the last mirror position", len(offsets))
	}
	return fs, nil
}

// uint24LE reads the 3-byte LSB-first encoding appendUint24LE writes.
func uint24LE(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16
}
