package encode

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"

	"github.com/atknatk/tappa/internal/sun"
)

// driver.go — ADR 0017 §5.1's command sequence, as a TABLE.
//
// 🔴 THE TABLE IS THE ORDER. There is no branch anywhere below that can reach a
// step out of turn: advance reads roundSteps[stepIdx], and stepIdx only ever moves
// forward by one and only after the previous step's response has been accepted.
// That is what makes the §5.1 ordering a property of the data rather than of the
// control flow, and it is what
// TestDriver_ChangeKeyAlwaysPrecedesChangeFileSettings asserts it three ways — the
// table's indices, the INS bytes on the wire, and a per-exchange check that the
// command about to be emitted is never ChangeFileSettings before ChangeKey has gone
// out (its own header says "WHY THREE"); a fourth test enumerates the state space
// WITHOUT RUNNING A ROUND — by reading the table's function pointers, which was the
// entire content of that correction: a claim about a state SPACE had been measured on
// a single TRACE.
//
// 🔴 WHY THE 6 <-> 7 ORDER IS THE ONE THING HERE THAT DEPARTS FROM AN12196, AND WHY
// THE DEPARTURE IS THE SAFE DIRECTION. AN12196's own personalisation list puts
// Get/ChangeFileSettings before any application-key change. Tappa reverses that
// pair, and only that pair (ADR 0017 §5.1: "SAPMA YALNIZ 6 <-> 7 ARASINDADIR"),
// because the two interruption outcomes are not symmetric:
//
//   - ChangeFileSettings first: SDM is switched ON while K_SDMFileRead is still the
//     PUBLIC factory default. A chip torn out of the field in that window walks
//     around emitting valid-looking SUN URLs signed with a key printed in a data
//     sheet.
//   - ChangeKey first: a chip torn out in that window has SDM still OFF —
//     NT4H2421Gx rev. 3.0 p. 10, "At delivery, SDM is disabled." It emits a static
//     NDEF and produces no SUN at all.
//
// Fail-closed is the second one. AN12196 does not claim its order is normative
// ("Following steps are optional and used as an example only"), and the departure
// is free because steps 5-7 all run in the same key-0 session.
//
// 🔴 THE CITATION FOR THAT LAST CLAUSE WAS WRONG AND IS CORRECTED (2026-08-21,
// third-eye audit; re-measured here from the PDF). It said "Table 8/9: FileAR.Change
// and FileAR.Write are both key 0 at delivery". Table 8 (p. 12) gives the NDEF file
// 02h as Read = Eh, Write = Eh, ReadWrite = Eh, Change = 0h — so Write is NOT key 0,
// it is FREE. What actually holds the three steps in one session is: step 7 needs
// Change, which IS key 0 at delivery, and steps 5-6 need nothing that conflicts with
// being authenticated as key 0.
//
// 🔴 AND THE WRONG CITATION WAS HIDING A REAL TENSION, WHICH IS WHY IT MATTERED.
// Datasheet §8.2.3.3 (p. 12): "If authenticated and the only access conditions
// satisfied are the free access Eh ones, then the CommMode.Plain is to be applied."
// At step 5 the NDEF file is still at delivery rights, so Write/ReadWrite are exactly
// those free-access Eh conditions — and this driver sends a CommMode.FULL WriteData.
// Read alone, that sentence says it should be sending Plain.
//
// 🔴 THE TENSION IS OPEN. IT IS NOT RESOLVED, AND A PREVIOUS VERSION OF THIS COMMENT
// CLAIMED IT WAS — RETRACTED 2026-08-21 AFTER A SECOND AUDIT MEASURED THE CLAIM.
//
// The retracted argument was: "AN12196 §5.8.2 Table 17 publishes a CommMode.FULL
// WriteData to file 02h, positioned between §5.6 and §5.9, so the file is still at
// delivery rights and the document does the very thing §8.2.3.3 forbids." Three
// measurements kill it, all from the same PDF:
//
//   - §5.4 (p. 24), verbatim: "This step does not reflect default delivered NTAG 424
//     DNA configuration of NDEF file settings (0000E0EE00010026000CA)." The document
//     NAMES the delivery configuration and says the example chip is not in it.
//   - Table 11 (p. 24) decodes that example chip: AccessRights = 00E0, i.e.
//     FileAR.ReadWrite = 0 (key 0) and FileAR.Write = 0. Write is KEY-GATED in the
//     example, not free — so §8.2.3.3's precondition ("the ONLY access conditions
//     satisfied are the free access Eh ones") is never met there, and CommMode.Full
//     is simply what a key-based condition requires.
//   - The positional argument fails on its own terms: §5.4 comes BEFORE §5.8, so the
//     document had already read those key-0 rights back two sections earlier.
//
// ⚠️ AND ONE MORE IMPRECISION IN THE SAME RETRACTED SENTENCE, RECORDED BECAUSE IT IS
// THE VERB THE WHOLE CLAIM RESTED ON: it said Table 17 "is titled" 'Write NDEF File -
// using Cmd.WriteData, CommMode.FULL'. The table's caption is 'Write NDEF File -
// using Cmd.WriteData'; ", CommMode.FULL" belongs to the §5.8.2 SECTION heading. The
// string exists verbatim in the named section, so this was imprecision rather than
// invention — but "is titled" is exactly the kind of borrowed precision that made the
// surrounding argument sound measured.
//
// The repository's own premise said as much and was not consulted:
// internal/sun/filesettings.go — "Step 5's WriteData already ran before it, under the
// delivery Write = Eh."
//
// SO, HONESTLY: Tappa's step 5 writes to a file whose Write and ReadWrite are both
// Eh, under a key-0 session, in CommMode.Full — and no published example covers that
// combination.
//
// ⚠️ AND THE DOCUMENT IS MORE UNIFORM THAN "A TENSION" SUGGESTS — a third statement
// points the same way, and this comment did not name it (eighth audit): Table 13,
// "Default communication modes per file", gives file 02h CommMode.Plain (and 03h
// CommMode.Full). Three places, one direction. It does not change the outcome —
// silicon decides what is actually enforced — but a reader should know the count.
//
// ⚠️ WHAT THE CHIP THEN DOES IS NOT IN THE DOCUMENT EITHER, AND AN EARLIER VERSION OF
// THIS COMMENT GUESSED (narrowed 2026-08-21, third audit). It said such a chip
// "refuses it" and that the chip would therefore be "still blank". Measured: §8.2.3.3
// (p. 12) and its twin §8.2.3.5 (p. 13, "CommMode.Plain has to be applied") state
// which MODE applies; neither says the frame is REJECTED. A chip applying Plain would
// read our sealed field as plain data and WRITE it into file 02h — the round still
// fails, at the response MAC, but the file has been touched, so "still blank" is
// wrong.
//
// WHY IT IS STILL ACCEPTABLE TO SHIP, on the half that survives the correction: step 5
// runs BEFORE any ChangeKey, so whatever the chip does with that frame, no key has
// changed and the plaque is still RECOVERABLE — ADR 0017 §5.3's recovery re-runs from
// step 5 and overwrites the NDEF file, which is exactly the pair of states §5.3 says
// its probes need not distinguish. The fallback is named and sits in the same section
// of the same document: §5.8.1, "Write NDEF File - using Cmd.ISOUpdateBinary,
// CommMode.PLAIN". Measuring which one silicon accepts is a FAZ B3 job (ADR 0017 §6
// md. 1: no chip has been encoded).
//
// WHAT IS *NOT* HERE, and both absences are decisions:
//
//   - ADR 0017 §5.1 STEP 8 (ChangeKey on application key 0). Normative in the ADR,
//     BLOCKED by §6 md. 5: `tags` has one aes_key_ref and ADR 0003 md. 4 fixes it
//     at 44 bytes, i.e. one AES-128 key. Storing a second key needs a column and a
//     migration, and this round writes neither. The consequence is written down and
//     is not small — ADR 0005 risk 8: until it ships, a plaque leaves with a public
//     AppMasterKey, and ADR 0017 §5.1's own security line says such a plaque MAY
//     BE BUILT AND TESTED BUT MAY NOT GO ON A WALL.
//   - The three recovery probes of ADR 0017 §5.3. internal/sun ships their command
//     builders (GetFileSettingsCommand, ParseFileSettings,
//     AuthenticateEV2FirstCommand); driving them is a separate flow with a separate
//     entry point, not a branch of the happy path.

// keyVersion is the KeyVer byte written with every plaque key.
//
// ⚠️ IT IS A CHOICE, AND THE DOCUMENTS DO NOT MAKE IT FOR US — MEASURED. NT4H2421Gx
// rev. 3.0 §8.2.4.6 says only that a key version is "a 1-byte key version (00h to
// FFh) … assigned to each key", and NOTHING in either document states the TRANSPORT
// value of that byte: §8.2.4.2 gives the transport KEY (16 bytes of 00h) and is
// silent about its version. So 01h is picked to match the one published
// personalisation example (AN12196 rev. 2.0 §5.16.1 Table 25 step 11, KeyVer 01),
// and NO INFERENCE MAY BE DRAWN FROM IT: "version 01h therefore means Tappa
// personalised this key" is exactly the unmeasured sentence this repository keeps
// having to retract. If a version byte is ever to be used as a personalisation
// signal, the factory value has to be read off silicon first.
const keyVersion = 0x01

// plaqueKeyLen is the AES-128 plaque key size (ADR 0003 md. 3).
const plaqueKeyLen = 16

// factoryKey returns the transport value of an untouched application key —
// NT4H2421Gx rev. 3.0 §8.2.4.2: "The transport value of these 5 keys is 16 bytes
// of 00h". It is PUBLIC (ADR 0005 risk 7 is built on that fact), so it is not
// registered in the keyring and not wiped: there is nothing to protect.
func factoryKey() []byte { return make([]byte, plaqueKeyLen) }

// stepDef is one relayed exchange.
type stepDef struct {
	// name appears in errors and in Progress.Step. It is a step name and carries
	// nothing else.
	name string
	// adr is the clause this step implements, kept beside the code so the sequence
	// and the document cannot drift apart silently.
	adr string
	// want is the status word the chip must return. A step that gets a different
	// one fails the round: on any error the chip has already dropped its
	// authentication state (datasheet rev. 3.0 §9.1.10), so there is nothing to
	// retry into.
	want sun.StatusWord
	// command builds the C-APDU this step sends.
	command func(ctx context.Context, st *Store, s *Session) ([]byte, error)
	// accept consumes the response's data field, WITHOUT its status word.
	accept func(ctx context.Context, st *Store, s *Session, data []byte) error
}

// roundSteps is ADR 0017 §5.1, expanded from its numbered steps into the ten
// exchanges they actually cost.
//
// The mapping to the ADR's numbering, because they are not the same list:
//
//	§5.1 step 1  ISO SELECT ................ 1 exchange
//	§5.1 step 2  GetVersion ................ 3 exchanges (datasheet §10.5.2, p. 58,
//	                                         verbatim: "The version data is return
//	                                         over three frames.")
//	§5.1 step 3  server: key, Wrap, ROW .... 0 exchanges — it runs inside the third
//	                                         GetVersion's accept, which is the
//	                                         first moment the UID exists and is
//	                                         still BEFORE step 4 (see §5.2 below)
//	§5.1 step 4  AuthenticateEV2First ...... 2 exchanges
//	§5.1 step 4b GetCardUID ................ 1 exchange — BEFORE the first
//	                                         irreversible command, see the step's
//	                                         own comment
//	§5.1 step 5  WriteData ................. 1 exchange
//	§5.1 step 6  ChangeKey(0x01) ........... 1 exchange
//	§5.1 step 7  ChangeFileSettings ........ 1 exchange
//	§5.1 step 8  ChangeKey(0x00) ........... NOT SHIPPED (§6 md. 5, see above)
//	§5.1 step 9  server: Zero, mark row .... 0 exchanges — Store.Step's tail
//
// Ten. DefaultTTL's floor is derived from len(roundSteps) rather than from a
// repeated literal.
//
// 🔴 AND THE SENTENCE THAT USED TO BE HERE CLAIMED AN INDEPENDENT CONFIRMATION THAT
// DOES NOT EXIST (corrected 2026-08-21, third-eye audit). It said "ADR 0017 §4 counts
// the same ten independently". The ADR's ten is a DIFFERENT ten: its list is
// ISO SELECT 1 + GetVersion 3 + AuthenticateEV2First 2 + WriteData 1 + ChangeKey x2 2
// + ChangeFileSettings 1, i.e. it counts BOTH ChangeKeys (step 8 included) and NO
// GetCardUID. This table ships one ChangeKey (step 8 is blocked, §6 md. 5) and adds
// GetCardUID (§6 md. 12). The two totals agree BY COINCIDENCE — one exchange dropped,
// one added — and a coincidence is not a cross-check. The ADR's number confirms
// nothing about this table, and if step 8 ever ships this table becomes ELEVEN while
// the ADR's list stays ten.
var roundSteps = []stepDef{
	{
		name: "select", adr: "ADR 0017 §5.1 step 1", want: sun.SWISOSuccess,
		command: cmdSelect, accept: acceptSelect,
	},
	{
		name: "getversion.1", adr: "ADR 0017 §5.1 step 2", want: sun.SWAdditionalFrame,
		command: cmdGetVersion1, accept: acceptVersionFrame1,
	},
	{
		name: "getversion.2", adr: "ADR 0017 §5.1 step 2", want: sun.SWAdditionalFrame,
		command: cmdGetVersion2, accept: acceptVersionFrame2,
	},
	{
		name: "getversion.3", adr: "ADR 0017 §5.1 step 2, then step 3", want: sun.SWSuccess,
		command: cmdGetVersion3, accept: acceptVersionFrame3AndWriteRow,
	},
	{
		name: "authenticate.1", adr: "ADR 0017 §5.1 step 4", want: sun.SWAdditionalFrame,
		command: cmdAuthenticate1, accept: acceptAuthenticate1,
	},
	{
		name: "authenticate.2", adr: "ADR 0017 §5.1 step 4", want: sun.SWSuccess,
		command: cmdAuthenticate2, accept: acceptAuthenticate2,
	},
	{
		// 🔴 BEFORE THE FIRST IRREVERSIBLE COMMAND, AND IT USED TO BE AFTER TWO OF THEM
		// (security audit F1, 2026-08-21). This gate sat between ChangeKey and
		// ChangeFileSettings, so a lying relay was caught only AFTER WriteData had
		// written the NDEF file and ChangeKey had installed the plaque key — measured:
		// the chip accepted A4 60 AF AF 71 AF 8D C4 before the check ran, and ended up
		// holding a key that appears in no row under its own UID. That is ADR 0017
		// §5.2's "chip written, row missing", the permanent-loss mode the whole
		// "row first" decision exists to avoid.
		//
		// ITS ONLY JUSTIFICATION HAD ALREADY BEEN RETRACTED IN THIS SAME ROUND. The
		// placement came from ADR 0017 §6 md. 12's "after step 6, while K_0x01 is
		// ours, the relay cannot produce the response MAC" — and that sentence was
		// measured false and withdrawn (see acceptGetCardUID). The reason went; the
		// placement stayed.
		//
		// DETECTION POWER IS UNCHANGED, and that is measurable rather than hopeful:
		// ADR 0017 §5.1 runs steps 5-8 in ONE key-0 session (step 6 changes key 1 and
		// does not re-authenticate; datasheet §10.6.1 requires key 0 for ChangeKey), so
		// GetCardUID's response MAC derives from the PUBLIC factory key 0 in either
		// position. ADR 0005 risk 7's attacker can forge it either way — the later
		// slot bought nothing.
		//
		// THE GAIN IS ONE-SIDED: a mismatch here leaves only a phantom inventory row
		// (status 'unassigned', location_id NULL, never in service) instead of a
		// personalised chip whose key is in no row. That is the safe half of §5.2's
		// asymmetry. It costs no extra exchange: GetCardUID is CommMode.Full and is
		// available from authenticate.2 onward.
		// ⚠️ THE adr: FIELD TRACKS THE DOCUMENT AND HAD DRIFTED FROM IT (sixth audit).
		// ADR 0017 §5.1 now names this exchange as step 4b — it was added there in the
		// same round that moved it here — so filing it only under §6 md. 12 was the
		// silent drift stepDef.adr exists to prevent.
		name: "getcarduid", adr: "ADR 0017 §5.1 step 4b (rationale: §6 md. 12)", want: sun.SWSuccess,
		command: cmdGetCardUID, accept: acceptGetCardUID,
	},
	{
		name: "writedata", adr: "ADR 0017 §5.1 step 5", want: sun.SWSuccess,
		command: cmdWriteNDEF, accept: acceptSealedAck,
	},
	{
		name: "changekey.sdmfileread", adr: "ADR 0017 §5.1 step 6", want: sun.SWSuccess,
		command: cmdChangeKeySDMFileRead, accept: acceptSealedAck,
	},
	{
		name: "changefilesettings", adr: "ADR 0017 §5.1 step 7", want: sun.SWSuccess,
		command: cmdChangeFileSettings, accept: acceptSealedAck,
	},
}

// advance runs exactly one exchange: check this step's status word, accept its
// data, move on, and build the next command.
func (s *Session) advance(ctx context.Context, st *Store, rapdu []byte) (Progress, error) {
	if s.finished || s.stepIdx >= len(roundSteps) {
		return Progress{}, fmt.Errorf("encode: this round has already finished")
	}
	def := roundSteps[s.stepIdx]

	data, sw, err := sun.SplitResponse(rapdu)
	if err != nil {
		return Progress{}, fmt.Errorf("encode: %s: %w", def.name, err)
	}
	if err := sun.RequireStatus(sw, def.want); err != nil {
		return Progress{}, fmt.Errorf("encode: %s: %w", def.name, err)
	}
	if err := def.accept(ctx, st, s, data); err != nil {
		return Progress{}, fmt.Errorf("encode: %s: %w", def.name, err)
	}

	s.stepIdx++
	if s.stepIdx == len(roundSteps) {
		return Progress{Done: true, Step: def.name, UIDHex: s.uidHex}, nil
	}
	next := roundSteps[s.stepIdx]
	cmd, err := next.command(ctx, st, s)
	if err != nil {
		return Progress{}, fmt.Errorf("encode: %s: %w", next.name, err)
	}
	return Progress{Command: cmd, Step: def.name, UIDHex: s.uidHex}, nil
}

// --- Step 1: ISO SELECT --------------------------------------------------------

func cmdSelect(context.Context, *Store, *Session) ([]byte, error) {
	return sun.ISOSelectNDEFApplication(), nil
}

func acceptSelect(_ context.Context, _ *Store, _ *Session, data []byte) error {
	// P2 = 0Ch asks for no response data (NT4H2421Gx rev. 3.0 §10.9.1 Table 84), so
	// a SELECT that answers 9000 WITH a body did not do what we asked and the rest
	// of the round would be running against an unknown application.
	if len(data) != 0 {
		return fmt.Errorf("select returned %d bytes of data; P2=0Ch asks for none", len(data))
	}
	return nil
}

// --- Step 2: GetVersion, three frames ------------------------------------------

func cmdGetVersion1(context.Context, *Store, *Session) ([]byte, error) {
	return sun.GetVersionCommands()[0], nil
}

func cmdGetVersion2(context.Context, *Store, *Session) ([]byte, error) {
	return sun.GetVersionCommands()[1], nil
}

func cmdGetVersion3(context.Context, *Store, *Session) ([]byte, error) {
	return sun.GetVersionCommands()[2], nil
}

func acceptVersionFrame1(_ context.Context, _ *Store, s *Session, data []byte) error {
	s.versionFrames[0] = append([]byte(nil), data...)
	return nil
}

func acceptVersionFrame2(_ context.Context, _ *Store, s *Session, data []byte) error {
	s.versionFrames[1] = append([]byte(nil), data...)
	return nil
}

// acceptVersionFrame3AndWriteRow is ADR 0017 §5.1 STEPS 2 AND 3 IN ONE PLACE, and
// they are in one place on purpose.
//
// 🔴 THIS IS WHERE §5.2's ORDERING LIVES. The rule is that the tags row is written
// BEFORE the chip's first irreversible command, and the reason is asymmetric
// failure: "row, no chip" leaves a dead inventory row and a recoverable blank chip,
// while "chip, no row" leaves a chip holding a key that exists NOWHERE — permanent
// plaque loss (§5.2's table). The row cannot be written any earlier than this,
// because tags.uid is the PRIMARY KEY and the UID is the chip's to disclose; and it
// must not be written any later, because the very next exchange is
// AuthenticateEV2First. GetVersion is what makes the earlier half possible:
// datasheet §10.5.2 — "This command is freely accessible without secure messaging
// … and there is no active authentication" — so the UID is in hand with no
// persistent side effect on the chip at all.
//
// TestDriver_TheRowIsWrittenBeforeTheFirstAuthenticationCommand pins the order by
// recording the interleaving of port calls and emitted APDUs, so moving this block
// one step later turns red.
func acceptVersionFrame3AndWriteRow(ctx context.Context, st *Store, s *Session, data []byte) error {
	v, err := sun.ParseGetVersion(s.versionFrames[0], s.versionFrames[1], data)
	if err != nil {
		return err
	}
	s.uid = v.UID
	s.uidHex = v.UIDHex()

	// The per-plaque concurrency limit (ADR 0017 §6 md. 7 item 4), applied at the
	// first moment the plaque has a name.
	//
	// ONE live round per UID, and refusal rather than eviction. A chip holds ONE
	// authentication state, so two rounds against one plaque interleave their
	// CmdCtrs and break both; and the second round's row INSERT would collide on
	// the PRIMARY KEY anyway. Refusing is the fail-closed direction: evicting the
	// live round would let anyone with a handle kill an encode that is mid-flight,
	// at the cost of one plaque. The price of refusing is that a stuck round blocks
	// its plaque until the TTL expires, which is a delay rather than a loss.
	st.mu.Lock()
	if _, taken := st.perUID[s.uidHex]; taken {
		st.mu.Unlock()
		return ErrPlaqueBusy
	}
	st.perUID[s.uidHex] = s.id
	st.mu.Unlock()

	// ADR 0017 §5.1 step 3, in its three parts.
	key, err := mintPlaqueKey()
	if err != nil {
		return err
	}
	// Registered BEFORE anything can fail: from here the key is the ring's, so
	// every exit path wipes it whether or not the wrap or the INSERT succeeds.
	// add wipes what it refuses, so there is no branch here that can leak one.
	if err := s.ring.add(keyNameSDMFileRead, key); err != nil {
		return err
	}
	wrapped, err := st.wrapper.WrapKey(s.uid, key)
	if err != nil {
		return fmt.Errorf("wrap the plaque key: %w", err)
	}
	if err := st.rows.InsertUnassigned(ctx, s.tenantID, s.uidHex, wrapped, s.actor); err != nil {
		// The chip has not been touched irreversibly yet — SELECT and GetVersion
		// leave nothing behind — so failing here costs nothing but the round.
		return fmt.Errorf("write the tags row: %w", err)
	}
	s.rowWritten = true
	return nil
}

// mintPlaqueKey produces one 16-byte AES-128 plaque key from crypto/rand
// (ADR 0003 md. 3: per-plaque random, derived from nothing).
//
// The error is checked although crypto/rand.Read on go1.26.7 documents itself as
// never returning one — same reasoning as newID's.
func mintPlaqueKey() ([]byte, error) {
	key := make([]byte, plaqueKeyLen)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("encode: mint plaque key: %w", err)
	}
	return key, nil
}

// --- Step 4: AuthenticateEV2First, two exchanges -------------------------------

func cmdAuthenticate1(_ context.Context, _ *Store, _ *Session) ([]byte, error) {
	// Key 0, the AppMasterKey, still at its PUBLIC factory value on a blank chip.
	// ADR 0005 risk 7 is the whole consequence: every byte of this session is
	// derivable by anyone who sees the dump, and no design closes that (ADR 0017
	// §2.2 lists six attempts and why all six fail).
	return sun.AuthenticateEV2FirstCommand(0x00)
}

// acceptAuthenticate1 opens the chip's challenge and builds the Part 2 cryptogram.
//
// 🔴 RndA IS MINTED HERE, FROM crypto/rand, AND IT IS NEVER MINTED ANYWHERE ELSE.
// This is the only rand.Read of 16 bytes in the package that feeds a handshake, the
// buffer goes straight into the keyring, and keyring.take marks it consumed the one
// time acceptAuthenticate2 reads it. There is no expression in this package that
// can produce the same RndA twice — see keyring.take for what a repeat would cost
// (a FALSE SUCCESS from ADR 0017 §5.3's probe 2, i.e. a half-written chip
// diagnosed as good).
func acceptAuthenticate1(_ context.Context, _ *Store, s *Session, data []byte) error {
	rndA := make([]byte, 16)
	if _, err := rand.Read(rndA); err != nil {
		return fmt.Errorf("mint the PCD challenge: %w", err)
	}
	if err := s.ring.add(keyNameRndA, rndA); err != nil {
		return err
	}

	part2, rndB, err := sun.EV2AuthPart1(factoryKey(), data, rndA)
	if err != nil {
		return err
	}
	if err := s.ring.add(keyNameRndB, rndB); err != nil {
		return err
	}

	// Held for exactly one step, because the NEXT command is the one that carries
	// it. It is not registered in the ring: it is on its way to the phone verbatim,
	// so wiping this copy would protect nothing (see the keyring's "what is not in
	// it" list).
	s.authPart2 = part2
	return nil
}

func cmdAuthenticate2(_ context.Context, _ *Store, s *Session) ([]byte, error) {
	// ⚠️ UNPINNED FOR THE SAME REASON AS cmdChangeFileSettings' template guard, and
	// recorded the same way: deleting it is green, because a nil cryptogram then fails
	// sun.AuthenticateEV2FirstPart2Command's own length check. Fail-closed twice; the
	// local guard exists to name the cause rather than the symptom.
	if s.authPart2 == nil {
		return nil, fmt.Errorf("no part 2 cryptogram")
	}
	cmd, err := sun.AuthenticateEV2FirstPart2Command(s.authPart2)
	// Cleared so the field holds a wire value for exactly one step. ⚠️ Deleting this
	// line is GREEN and that is expected: the value is on its way to the phone
	// verbatim, so keeping it has no §4.7 consequence (see the keyring's exclusions).
	// Tidiness, recorded as such rather than dressed as a wipe.
	s.authPart2 = nil
	return cmd, err
}

// acceptAuthenticate2 finishes the handshake: internal/sun verifies that the chip
// echoed our RndA (that check IS the authentication) and derives the session keys.
func acceptAuthenticate2(_ context.Context, _ *Store, s *Session, data []byte) error {
	rndA, err := s.ring.take(keyNameRndA)
	if err != nil {
		return err
	}
	rndB, err := s.ring.take(keyNameRndB)
	if err != nil {
		return err
	}

	auth, err := sun.EV2AuthPart2(factoryKey(), rndA, rndB, data)
	if err != nil {
		return err
	}
	// The two session keys join the inventory the moment they exist, so from here
	// every exit path wipes them. internal/sun's EV2Auth.Zero would wipe exactly
	// these two buffers — it is not called, because a second list is a list that can
	// diverge from this one, and this one is the inventory ADR 0017 §6 md. 7
	// asked for.
	// 🔴 BOTH ADDS ARE ATTEMPTED BEFORE EITHER ERROR IS RETURNED, AND THAT IS NOT
	// STYLE (security audit L2, 2026-08-21). Written as two early returns, a failure
	// on KeyENC meant KeyMAC was NEVER REGISTERED — an orphaned session key that
	// zeroAll can never reach, because add only wipes the buffer it itself refuses.
	// ⚠️ Measured as unreachable TODAY: advance only advances stepIdx on success and
	// retires the session on error, and acceptAuthenticate2 runs at most once per
	// session, so neither slot can already be filled. It is written this way anyway
	// because ADR 0017 §5.1 step 8 repeats the pattern with TWO plaque keys the day
	// §6 md. 5 lands, and "unreachable today" is not a property a future edit
	// preserves.
	errENC := s.ring.add(keyNameSesENC, auth.KeyENC)
	errMAC := s.ring.add(keyNameSesMAC, auth.KeyMAC)
	if errENC != nil {
		return errENC
	}
	if errMAC != nil {
		return errMAC
	}
	s.auth = auth
	// Datasheet rev. 3.0 §9.1.2: the counter is reset to 0000h by a successful
	// AuthenticateEV2First. Set explicitly rather than relied upon as the zero
	// value, so a session object that is ever reused cannot inherit a stale count.
	s.cmdCtr = 0
	return nil
}

// --- Step 5: WriteData ---------------------------------------------------------

func cmdWriteNDEF(_ context.Context, st *Store, s *Session) ([]byte, error) {
	// 🔴 §5.2 AS A RUNTIME INVARIANT AND NOT ONLY AS A TABLE ORDER. WriteData is the
	// chip's FIRST IRREVERSIBLE command, so this is the last point at which "is
	// there a row for this chip?" can still be answered cheaply. The step table
	// already puts the row four exchanges earlier; this is the guard that makes a
	// future reordering fail HERE, before the chip is touched, rather than produce
	// the "chip written, row missing" mode silently.
	if !s.rowWritten {
		return nil, fmt.Errorf("refusing the chip's first irreversible command with no tags row (ADR 0017 §5.2)")
	}
	t, err := sun.BuildTapNDEF(st.baseURL)
	if err != nil {
		return nil, err
	}
	s.ndef = t
	ctr, err := s.useCtr()
	if err != nil {
		return nil, err
	}
	field, err := sun.EV2WriteDataCommand(s.auth, ctr, sun.NDEFFileNo, 0, t.File)
	if err != nil {
		return nil, err
	}
	return sun.NativeAPDU(sun.INSWriteData, field)
}

// acceptSealedAck verifies a CommMode.Full response that carries no data — the
// shape WriteData, ChangeKey (case 1) and ChangeFileSettings all answer with.
//
// The MAC verification is the point: it proves the chip, not the relay, produced
// this acknowledgement, AND it proves our command counter and TI still agree with
// the chip's. A counter that had drifted would fail here rather than three steps
// later in a way that reads like a key problem (skill tappa-sun's named trap).
func acceptSealedAck(_ context.Context, _ *Store, s *Session, data []byte) error {
	plain, err := sun.EV2UnwrapResponseFull(s.auth, s.lastCtr, 0x00, data)
	if err != nil {
		return err
	}
	if len(plain) != 0 {
		return fmt.Errorf("expected an empty response body, got %d bytes", len(plain))
	}
	return nil
}

// --- Step 6: ChangeKey on K_SDMFileRead ----------------------------------------

// cmdChangeKeySDMFileRead installs the plaque key on application key 01h.
//
// ADR 0017 §5.0 decision 1 fixes the number at 01h rather than 00h, and the direct
// consequence is the body shape: Table 63 gives keys 1..4 the 21-byte form,
// (NewKey XOR OldKey) || KeyVer || CRC32NK. ⚠️ The split is by KEY NUMBER — "if key 1
// to 4 are to be changed" — not by whether the key equals the authenticated one; an
// earlier version of this line borrowed AN12196's "KeyNo != AuthKey" section heading
// and called it Table 63's rule. That is also why ADR 0017 §6 md. 6's
// XOR risk is real here and not theoretical — no published vector distinguishes a
// missing XOR, because both published examples have an all-zero old key.
// internal/sun closed that with its own experiment (FAZ B2a: deleting the XOR turns
// exactly one test red); this layer only supplies the two keys.
func cmdChangeKeySDMFileRead(_ context.Context, _ *Store, s *Session) ([]byte, error) {
	// Read, not consumed: the key is needed again by step 8 the day §6 md. 5 lets
	// step 8 exist, and the ring — not this function — owns its end.
	//
	// ⚠️ NOTHING HOLDS THAT TODAY, AND IT IS A LIMIT RATHER THAN A GATE (tenth audit,
	// 2026-08-21). Changing this peek to take is GREEN, because step 6 is the only
	// reader in the shipped sequence — the second reader arrives with step 8. So the
	// choice of verb is a statement of intent that no test can currently distinguish.
	// Recorded rather than pinned: a test asserting "peek, not take" would only
	// restate the source, and the property it protects does not exist yet. FAIL-CLOSED
	// either way — a consumed slot makes step 8 error rather than send a wrong key.
	key, err := s.ring.peek(keyNameSDMFileRead)
	if err != nil {
		return nil, err
	}
	ctr, err := s.useCtr()
	if err != nil {
		return nil, err
	}
	field, err := sun.EV2ChangeKeyCommand(s.auth, ctr, sun.SDMFileReadKeyNo, factoryKey(), key, keyVersion)
	if err != nil {
		return nil, err
	}
	return sun.NativeAPDU(sun.INSChangeKey, field)
}

// --- ADR 0017 §6 md. 12, fourth consequence: re-read the UID --------------------

func cmdGetCardUID(_ context.Context, _ *Store, s *Session) ([]byte, error) {
	ctr, err := s.useCtr()
	if err != nil {
		return nil, err
	}
	// GetCardUIDCommand returns a COMPLETE C-APDU, not a data field.
	return sun.GetCardUIDCommand(s.auth, ctr)
}

// acceptGetCardUID compares the UID the chip returns under a session MAC against
// the UID the row was written for.
//
// 🔴 WHAT THIS CATCHES, STATED AT EXACTLY ITS OWN SIZE. GetVersion's UID arrives
// with NOTHING signed (datasheet §10.5.2: the command is freely accessible, and its
// response carries no MAC). GetCardUID is CommMode.Full, so this UID arrives inside
// a response whose MAC was computed with the session keys. A relay that swaps the
// UID in the transport — a bug, a mis-scanned second chip, an attacker editing one
// field of an HTTP body — cannot also produce a matching sealed response without
// implementing the chip's secure messaging, which is no longer relaying.
//
// 🔴 AND WHAT IT DOES *NOT* CATCH, WHICH ADR 0017 §6 md. 12 CLAIMS IT DOES.
// That clause says the response MAC "röle tarafından üretilemez" — the relay cannot
// produce it. MEASURED HERE, AND THAT SENTENCE IS TOO STRONG IN EXACTLY ONE WAY:
// this session authenticated with application key 0 at its FACTORY value, which is
// public (datasheet §8.2.4.2). ADR 0005 risk 7 spells out the consequence — a relay
// that sees the dump can re-derive RndA, RndB and therefore KSesAuthENC and
// KSesAuthMAC, so it CAN forge any MAC in this session, including this one. Moving
// the check into a fresh key-01h session would not help either: the same dump
// carries step 6's ChangeKey body, from which the same observer recovers K_0x01.
//
// So the honest claim is a RAISED COST, not a closed door: the gate separates a
// relay that lies with a string edit from one that implements EV2 secure messaging.
// Against ADR 0017 §2.2's fully hostile relay it detects nothing, and it was never
// going to — the ADR's own §2.2 says a chain bootstrapped from a public key cannot
// carry confidentiality, and authenticity from that same key is the same sentence.
//
// It does not prevent the row either, and never could: the row is written THREE
// exchanges before this gate, by design (§5.2). (The "four" that used to sit in this
// sentence alongside the three is the distance to WriteData, which driver.go states
// correctly at cmdWriteNDEF — one relation, two numbers, left over from the previous
// round's own correction of this line.)
// What it converts is a SILENT permanent loss
// into an abort with a named cleanup — see RelayMismatchError.
func acceptGetCardUID(_ context.Context, _ *Store, s *Session, data []byte) error {
	plain, err := sun.EV2UnwrapResponseFull(s.auth, s.lastCtr, 0x00, data)
	if err != nil {
		return err
	}
	if len(plain) != len(s.uid) {
		return fmt.Errorf("the chip returned a %d-byte UID, expected %d", len(plain), len(s.uid))
	}
	// bytes.Equal, NOT subtle.ConstantTimeCompare, and that is a decision rather
	// than an omission: a UID is PUBLIC (ADR 0003 md. 1 — printed on the plaque and
	// carried in every tap URL), so there is no secret whose comparison could leak
	// through timing. Spelling it constant-time would assert a sensitivity this
	// value does not have, and would have to be registered in
	// cmd/tappa/constanttime_test.go's inventory as if it protected something.
	if !bytes.Equal(plain, s.uid) {
		return &RelayMismatchError{
			RowUID: s.uidHex,
			// Spelled through internal/sun's own canonicaliser rather than a local
			// hex.EncodeToString: migration 00013 narrowed tags.uid to
			// ^[0-9A-F]{14}$ and there must be exactly one place that knows the case.
			ChipUID: (&sun.Version{UID: plain}).UIDHex(),
		}
	}
	return nil
}

// --- Step 7: ChangeFileSettings ------------------------------------------------

func cmdChangeFileSettings(_ context.Context, _ *Store, s *Session) ([]byte, error) {
	// ⚠️ THIS GUARD IS UNPINNED, AND THE TEST THAT LOOKS LIKE IT PINS IT PASSES FOR
	// ANOTHER REASON (tenth audit). Deleting it is green: with s.ndef nil the call
	// falls through to sun.TappaNDEFSettings, which refuses a nil template itself. So
	// internal/sun is a real second gate and the round still fails closed — but the
	// assertion that "proves" this guard is only asserting that SOMETHING errored.
	// The same shape as the row-guard finding, materially weaker because the second
	// gate is real. Recorded as a limit; the guard stays because it names the cause
	// (step 5 has not run) where internal/sun can only name the symptom.
	if s.ndef == nil {
		return nil, fmt.Errorf("no NDEF template; step 5 has not run")
	}
	settings, err := sun.TappaNDEFSettings(s.ndef)
	if err != nil {
		return nil, err
	}
	ctr, err := s.useCtr()
	if err != nil {
		return nil, err
	}
	field, err := sun.EV2ChangeFileSettingsCommand(s.auth, ctr, settings)
	if err != nil {
		return nil, err
	}
	return sun.NativeAPDU(sun.INSChangeFileSettings, field)
}
