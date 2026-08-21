package sun

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// GUARD RAILS FOR EV2 SECURE MESSAGING.
//
// ev2_kat_test.go proves the bytes are right against NXP's published examples.
// This file proves the things a document cannot: that malformed input is refused
// rather than half-processed, that no error message carries key material
// (CLAUDE.md §4.7), and that the two zeroisation duties ADR 0017 §3 names are
// actually performed.
//
// 🔴 NOTHING HERE IS A KNOWN-ANSWER TEST AND NOTHING HERE MAY BECOME ONE. Where a
// frame is constructed locally it is constructed to be REJECTED; no expected
// ciphertext, MAC or session key in this file was minted by ev2.go. That
// separation is the M2-08 rule: a value the implementation produced can never be
// the value the implementation is checked against.

// zeroDisciplineInventory is the number of `defer Zero(...)` statements in each
// production file of the EV2 half, and it exists because the alternative was a
// SENTENCE.
//
// 🔴 WHAT WAS MEASURED (2026-08-21, security audit). All eleven deferred wipes in
// ev2.go and changekey.go were deleted at once and `go test ./internal/sun/`
// stayed ENTIRELY GREEN. ev2.go's header claims a specific list of buffers is
// zeroised "so the claim can be checked" — and there was nothing checking it. A
// §4.7 obligation that no test can fail is a comment, not a mechanism.
//
// 🔴 IT RATCHETS BOTH WAYS, exactly like cmd/tappa's constantTimeInventory and for
// the same reason: with "at least", a deleted wipe buys itself an exemption.
// Adding one also fails until this map is edited, which is the visible-in-review
// step — a new wipe should be accompanied by somebody saying which buffer it
// covers.
//
// 🔴 WHAT THIS DOES NOT PROVE, said plainly so nobody reads more into a green run.
// It counts the PRESENCE of a deferred wipe, not that the wipe covers the right
// buffer, not that the buffer still holds what the comment says, and not that
// every buffer needing one has one — F2 (the unnamed ev2RotateLeft1 result) was a
// MISSING wipe, and a count cannot find those. It closes one mutation: somebody
// removing or "simplifying away" a wipe that is already there. Finding a missing
// one is still a reading job.
//
// ⚠️ SCOPE. Only the deferred form is counted. Zero is also called non-deferred
// (EV2AuthPart1's error path, keys.go's callers) and that is deliberately out of
// scope: the deferred form is the one that survives an early return, which is what
// ADR 0017 §3 asks for ("HER çıkışta").
//
// 🔴 THE SCANNED SET IS WIDER THAN THE INVENTORY, AND THAT ASYMMETRY IS THE POINT
// (M8-05 FAZ B2b). zeroDisciplineFiles lists every production file of the
// personalisation half; the inventory lists only the ones that hold key material.
// A scanned file that is NOT in the inventory must have ZERO deferred wipes, so
// the day apdu.go, ndef.go or filesettings.go starts handling a plaintext key, the
// inventory has to be edited in the same change — the visible-in-review step. The
// alternative, listing them with a zero, does not work: a zero is exactly what a
// deletion produces, which is the argument cmd/tappa's constantTimeInventory makes
// about internal/session.
//
// ⚠️ THE OBLIGATION THIS DOES NOT DISCHARGE: ADR 0017 §6 md. 7 — the in-memory
// session's TTL, its sweeper, and the guarantee that a CALLER invokes
// EV2Auth.Zero on every exit path — is turn 2c's and stays open. This map is about
// this package's own internal buffers only.
var zeroDisciplineFiles = []string{"ev2.go", "changekey.go", "apdu.go", "ndef.go", "filesettings.go"}

var zeroDisciplineInventory = map[string]int{
	// Two session vectors, two derivation CMAC outputs (ev2SessionKeys); the
	// rotated RndB' and the RndA||RndB' it feeds (EV2AuthPart1); the decrypted
	// auth response and the rotated echo (EV2AuthPart2); the command IV and the
	// padded body (EV2WrapCommandFull); the response IV (EV2UnwrapResponseFull).
	"ev2.go": 11,
	// The plaintext ChangeKey body (EV2ChangeKeyCommand) — for key 0 it IS the new
	// plaque key in the clear.
	"changekey.go": 1,
}

// TestEV2_ZeroDisciplineIsInventoried holds the count above. It parses the source
// rather than grepping it, so a `defer Zero(` written inside a comment or a string
// cannot inflate the number — the failure mode cmd/rotatekek/script_test.go hit
// three separate times with text scans.
func TestEV2_ZeroDisciplineIsInventoried(t *testing.T) {
	got := map[string]int{}
	for _, name := range zeroDisciplineFiles {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			d, ok := n.(*ast.DeferStmt)
			if !ok {
				return true
			}
			if id, ok := d.Call.Fun.(*ast.Ident); ok && id.Name == "Zero" {
				got[name]++
			}
			return true
		})
	}

	// CONTROL: the walk reached real code. Without this the whole test passes on an
	// empty result set if parsing ever silently stops finding anything.
	if total := got["ev2.go"] + got["changekey.go"]; total < 5 {
		t.Fatalf("only %d deferred wipes found across both files; the scan has gone blind", total)
	}

	for name, want := range zeroDisciplineInventory {
		switch have := got[name]; {
		case have == 0:
			t.Errorf("%s is in zeroDisciplineInventory expecting %d deferred Zero call(s) "+
				"and now has NONE. If key material stopped being wiped, that is the §4.7 "+
				"leak this test exists for; if it moved, lower the number HERE in the same "+
				"change", name, want)
		case have != want:
			t.Errorf("%s has %d deferred Zero call(s), inventory says %d. Growing is not "+
				"free either: say in the map WHICH buffer the new one covers", name, have, want)
		}
	}

	// The other direction: a scanned file that is not inventoried must hold no
	// wipes at all. A wipe appearing there means key material started flowing
	// through a file whose own header says it handles none.
	total := 0
	for _, name := range zeroDisciplineFiles {
		total += got[name]
		if _, inInventory := zeroDisciplineInventory[name]; inInventory {
			continue
		}
		if got[name] != 0 {
			t.Errorf("%s performs %d deferred Zero call(s) and is not in "+
				"zeroDisciplineInventory. Either it started handling key material — in "+
				"which case inventory it HERE, saying which buffer — or the wipe belongs "+
				"in another file", name, got[name])
		}
	}
	t.Logf("%d deferred key-material wipes across %d scanned files, %d of them inventoried",
		total, len(zeroDisciplineFiles), len(zeroDisciplineInventory))
}

// TestEV2_AuthPart1RejectsMalformedInput covers the three length gates of
// AuthenticateEV2First - Part1 and asserts none of them names key bytes.
func TestEV2_AuthPart1RejectsMalformedInput(t *testing.T) {
	key := hexBytes(t, katT14Key)
	enc := hexBytes(t, katT14EncRndB)
	rndA := hexBytes(t, katT19RndA) // a non-zero value, so a leak would be visible

	for _, tc := range []struct {
		name          string
		key, enc, rnd []byte
	}{
		{"short_key", key[:15], enc, rndA},
		{"long_key", append(append([]byte{}, key...), 0x00), enc, rndA},
		{"short_challenge", key, enc[:8], rndA},
		{"nil_challenge", key, nil, rndA},
		{"short_rnda", key, enc, rndA[:4]},
		{"nil_rnda", key, enc, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			part2, rndB, err := EV2AuthPart1(tc.key, tc.enc, tc.rnd)
			if err == nil {
				t.Fatal("expected an error")
			}
			if part2 != nil || rndB != nil {
				t.Fatal("returned material alongside an error")
			}
			assertNoKeyBytesInMessage(t, err.Error(), rndA)
		})
	}
}

// TestEV2_AuthPart2RejectsMalformedInput covers Part2's length gates. The
// response-length gate matters most: NT4H2421Gx rev. 3.0 §9.1.5 says "The PCD
// rejects answers from the PICC when they don't have the proper length", so a
// short or over-long frame must never reach the split.
func TestEV2_AuthPart2RejectsMalformedInput(t *testing.T) {
	key := hexBytes(t, katT14Key)
	rndA := hexBytes(t, katT14RndA)
	rndB := hexBytes(t, katT14RndB)
	resp := hexBytes(t, katT14Part2Resp)

	for _, tc := range []struct {
		name                  string
		key, rndA, rndB, resp []byte
	}{
		{"short_key", key[:8], rndA, rndB, resp},
		{"short_rnda", key, rndA[:4], rndB, resp},
		{"short_rndb", key, rndA, rndB[:4], resp},
		{"short_response", key, rndA, rndB, resp[:16]},
		{"long_response", key, rndA, rndB, append(append([]byte{}, resp...), resp...)},
		{"nil_response", key, rndA, rndB, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth, err := EV2AuthPart2(tc.key, tc.rndA, tc.rndB, tc.resp)
			if err == nil {
				t.Fatal("expected an error")
			}
			if auth != nil {
				t.Fatal("returned a session alongside an error")
			}
			assertNoKeyBytesInMessage(t, err.Error(), rndA)
		})
	}
}

// TestEV2_AuthZeroWipesBothSessionKeys is ADR 0017 §3's duty made mechanical: the
// session keys must be wipeable on every exit path, and a nil receiver must not
// panic (a deferred Zero on a failed handshake is exactly that case).
func TestEV2_AuthZeroWipesBothSessionKeys(t *testing.T) {
	auth, err := EV2AuthPart2(
		hexBytes(t, katT14Key), hexBytes(t, katT14RndA),
		hexBytes(t, katT14RndB), hexBytes(t, katT14Part2Resp),
	)
	if err != nil {
		t.Fatalf("EV2AuthPart2: %v", err)
	}
	auth.Zero()
	zeros := make([]byte, ev2KeyLen)
	if !bytes.Equal(auth.KeyENC, zeros) || !bytes.Equal(auth.KeyMAC, zeros) {
		t.Fatal("Zero left session key bytes behind")
	}
	// TI is not secret and is deliberately untouched — a caller may still need it
	// to log which transaction failed.
	if bytes.Equal(auth.TI, make([]byte, ev2TILen)) {
		t.Fatal("Zero wiped TI, which is not key material and is still needed")
	}
	var nilAuth *EV2Auth
	nilAuth.Zero() // must not panic
}

// TestEV2_WrapCommandFullRejectsBrokenSessions covers ev2CheckAuth's gates. A
// half-built EV2Auth is a programming error upstream, not a protocol event, so it
// must fail loudly rather than seal something with a short key.
func TestEV2_WrapCommandFullRejectsBrokenSessions(t *testing.T) {
	good := katAuth(t, katT14TI, katT14KeyENC, katT14KeyMAC)
	body := hexBytes(t, katT18CmdData)

	for _, tc := range []struct {
		name string
		auth *EV2Auth
	}{
		{"nil", nil},
		{"short_ti", &EV2Auth{TI: good.TI[:2], KeyENC: good.KeyENC, KeyMAC: good.KeyMAC}},
		{"nil_ti", &EV2Auth{KeyENC: good.KeyENC, KeyMAC: good.KeyMAC}},
		{"short_enc_key", &EV2Auth{TI: good.TI, KeyENC: good.KeyENC[:8], KeyMAC: good.KeyMAC}},
		{"short_mac_key", &EV2Auth{TI: good.TI, KeyENC: good.KeyENC, KeyMAC: good.KeyMAC[:8]}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := EV2WrapCommandFull(tc.auth, 0x5F, 1, []byte{0x02}, body); err == nil {
				t.Fatal("expected an error")
			}
			if _, err := EV2UnwrapResponseFull(tc.auth, 1, 0x00, hexBytes(t, katT18RespField)); err == nil {
				t.Fatal("expected an error")
			}
			if _, err := EV2ChangeKeyCommand(tc.auth, 1, 0x01,
				hexBytes(t, katT25OldKey), hexBytes(t, katT25NewKey), 0x01); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

// TestEV2_UnwrapResponseFullRejectsShortFrames pins the minimum: a CommMode.Full
// response always carries at least the 8 truncated MAC bytes. Anything shorter is
// not a MAC failure, it is a malformed frame — including the error frames
// NT4H2421Gx rev. 3.0 §9.1.10 says arrive "without a MAC appended".
func TestEV2_UnwrapResponseFullRejectsShortFrames(t *testing.T) {
	auth := katAuth(t, katT14TI, katT14KeyENC, katT14KeyMAC)
	for _, n := range []int{0, 1, 7} {
		data, err := EV2UnwrapResponseFull(auth, 1, 0x00, make([]byte, n))
		if err == nil {
			t.Fatalf("a %d-byte response frame was accepted", n)
		}
		if data != nil {
			t.Fatal("returned data alongside an error")
		}
	}
}

// TestEV2_CommandCounterRefusesToWrap covers NT4H2421Gx rev. 3.0 §9.1.2's
// boundary: FFFFh has no successor, and a silent wrap to 0000h would compute a
// response MAC against a counter the chip will never use. Both the helper and the
// caller that depends on it are checked.
func TestEV2_CommandCounterRefusesToWrap(t *testing.T) {
	if _, err := ev2NextCtr(0xFFFF); err == nil {
		t.Fatal("ev2NextCtr wrapped FFFF to 0000")
	}
	if got, err := ev2NextCtr(0xFFFE); err != nil || got != 0xFFFF {
		t.Fatalf("ev2NextCtr(FFFE) = %04X, %v; want FFFF, nil", got, err)
	}
	auth := katAuth(t, katT14TI, katT14KeyENC, katT14KeyMAC)
	if _, err := EV2UnwrapResponseFull(auth, 0xFFFF, 0x00, hexBytes(t, katT18RespField)); err == nil {
		t.Fatal("a response was verified past the counter's last value")
	}
}

// TestEV2_PaddingFollowsISO9797Method2 checks the padding rule directly, because
// its most surprising clause — a plaintext that is ALREADY a multiple of 16 gets a
// whole extra block — is the one an implementer skips. NT4H2421Gx rev. 3.0 §9.1.4
// states it; AN12196 rev. 2.0 §5.8.2 Table 17 shows it with values (128 plaintext
// bytes produce 144 ciphertext bytes).
func TestEV2_PaddingFollowsISO9797Method2(t *testing.T) {
	for _, tc := range []struct {
		name    string
		in      int
		wantLen int
	}{
		{"empty", 0, 16},
		{"one_byte", 1, 16},
		{"fifteen", 15, 16},
		{"exact_block", 16, 32},
		{"seventeen", 17, 32},
		{"two_blocks", 32, 48},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := ev2Pad(make([]byte, tc.in))
			if len(out) != tc.wantLen {
				t.Fatalf("padded %d bytes to %d, want %d", tc.in, len(out), tc.wantLen)
			}
			if out[tc.in] != ev2PadByte {
				t.Fatalf("byte %d is %02X, want the 80h marker", tc.in, out[tc.in])
			}
			back, err := ev2Unpad(out)
			if err != nil {
				t.Fatalf("ev2Unpad: %v", err)
			}
			if len(back) != tc.in {
				t.Fatalf("unpadded to %d bytes, want %d", len(back), tc.in)
			}
		})
	}
}

// TestEV2_UnpadRejectsMalformedPadding is the fail-closed half. The chip does the
// same in the other direction (NT4H2421Gx rev. 3.0 §9.1.10: "Commands without a
// valid padding are also rejected by returning INTEGRITY_ERROR"), so a best-effort
// trim here would be more permissive than the silicon.
func TestEV2_UnpadRejectsMalformedPadding(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"all_zero_block", "00000000000000000000000000000000"},
		{"no_marker", "01010101010101010101010101010101"},
		{"marker_too_far_back", "80000000000000000000000000000000" + "00000000000000000000000000000001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ev2Unpad(hexBytes(t, tc.in)); err == nil {
				t.Fatal("malformed padding was accepted")
			}
		})
	}
}

// TestEV2_UnwrapResponseFullRejectsUnpaddablePlaintext drives the padding gate
// through the real entry point. The frame is BUILT HERE to be rejected: a block
// with no 80h marker anywhere, sealed and MACed correctly so that the ONLY thing
// wrong with it is the padding. That is why it is not a known-answer test and
// carries no expected ciphertext — it asserts a refusal, not a value.
func TestEV2_UnwrapResponseFullRejectsUnpaddablePlaintext(t *testing.T) {
	auth := katAuth(t, katT28TI, katT28KeyENC, katT28KeyMAC)
	const cmdCtr = 0x0000

	respCtr, err := ev2NextCtr(cmdCtr)
	if err != nil {
		t.Fatalf("ev2NextCtr: %v", err)
	}
	iv, err := ev2DataIV(auth.KeyENC, ev2LabelRespIV, auth.TI, respCtr)
	if err != nil {
		t.Fatalf("ev2DataIV: %v", err)
	}
	plain := bytes.Repeat([]byte{0x01}, 16) // contains no 80h and does not end in 00h
	enc, err := ev2CBCEncrypt(auth.KeyENC, iv, plain)
	if err != nil {
		t.Fatalf("ev2CBCEncrypt: %v", err)
	}
	full, err := cmac(auth.KeyMAC, ev2MACInput(0x00, respCtr, auth.TI, nil, enc))
	if err != nil {
		t.Fatalf("cmac: %v", err)
	}
	sel := truncateMACt(full)

	_, err = EV2UnwrapResponseFull(auth, cmdCtr, 0x00, append(enc, sel[:]...))
	if err == nil {
		t.Fatal("a response whose plaintext is not ISO 9797-1 padded was accepted")
	}
	if !strings.Contains(err.Error(), "padded") {
		t.Fatalf("the frame was rejected for the wrong reason: %v", err)
	}
}

// TestEV2_UnwrapResponseFullRejectsAMisalignedCiphertext closes a path that a
// coverage report made look unreachable and an audit measured as REACHABLE
// (2026-08-21).
//
// 🔴 THE CLAIM THAT WAS WRONG, WRITTEN OUT SO IT IS NOT MADE AGAIN. The first
// round of this card reported every uncovered block in ev2.go as "an error branch
// behind an earlier explicit length check". For this one — the ev2CBCDecrypt call
// on the response path — that is false. The only length gate before it is
// "respField must carry at least the 8 MAC bytes"; NOTHING requires the remainder
// to be a whole number of AES blocks, and the MAC gate does not either, because a
// MAC can be computed over any length. So a frame whose "ciphertext" is 4 bytes
// and whose MAC is correct over those 4 bytes walks straight through the MAC gate
// and lands in the decrypt error.
//
// The behaviour was already right and fail-closed; what was missing was the test
// and what was wrong was the sentence. Both are fixed here. The frame is BUILT
// LOCALLY to be rejected, so this is not a known-answer test and carries no
// expected ciphertext or MAC.
//
// The second subtest is the CONTROL that makes the first mean something: with the
// MAC deliberately broken, the SAME frame is rejected EARLIER, for authentication
// rather than alignment — which is how we know the alignment error was reached
// through a passing MAC gate and not before it.
func TestEV2_UnwrapResponseFullRejectsAMisalignedCiphertext(t *testing.T) {
	auth := katAuth(t, katT28TI, katT28KeyENC, katT28KeyMAC)
	const cmdCtr = 0x0000

	respCtr, err := ev2NextCtr(cmdCtr)
	if err != nil {
		t.Fatalf("ev2NextCtr: %v", err)
	}
	stub := []byte{0xDE, 0xAD, 0xBE, 0xEF} // four bytes: not a whole AES block
	full, err := cmac(auth.KeyMAC, ev2MACInput(0x00, respCtr, auth.TI, nil, stub))
	if err != nil {
		t.Fatalf("cmac: %v", err)
	}
	sel := truncateMACt(full)

	t.Run("valid_mac_reaches_the_alignment_gate", func(t *testing.T) {
		_, err := EV2UnwrapResponseFull(auth, cmdCtr, 0x00, append(append([]byte{}, stub...), sel[:]...))
		if err == nil {
			t.Fatal("a response whose ciphertext is not a multiple of 16 bytes was accepted")
		}
		if !strings.Contains(err.Error(), "multiple of") {
			t.Fatalf("rejected for the wrong reason, so this test does not reach the "+
				"decrypt gate: %v", err)
		}
	})

	t.Run("broken_mac_stops_earlier", func(t *testing.T) {
		bad := append(append([]byte{}, stub...), sel[:]...)
		bad[len(bad)-1] ^= 0x01
		_, err := EV2UnwrapResponseFull(auth, cmdCtr, 0x00, bad)
		if err == nil {
			t.Fatal("a response with a broken MAC was accepted")
		}
		if !strings.Contains(err.Error(), "authenticate") {
			t.Fatalf("the MAC gate did not fire first; the ordering claim above is "+
				"wrong: %v", err)
		}
	})
}

// TestEV2_DataIVRejectsAMalformedTransactionIdentifier covers the last gate on the
// IV path. TI is fixed at 4 bytes (NT4H2421Gx rev. 3.0 §9.1.1) and a shorter one
// would silently shift the counter into the padding.
func TestEV2_DataIVRejectsAMalformedTransactionIdentifier(t *testing.T) {
	keyENC := hexBytes(t, katT14KeyENC)
	for _, ti := range []string{"", "9D00", "9D00C4DF00"} {
		if _, err := ev2DataIV(keyENC, ev2LabelCmdIV, hexBytes(t, ti), 1); err == nil {
			t.Fatalf("a %d-byte TI was accepted", len(ti)/2)
		}
	}
	if _, err := ev2DataIV(keyENC[:8], ev2LabelCmdIV, hexBytes(t, katT14TI), 1); err == nil {
		t.Fatal("a short session key was accepted")
	}
}

// TestEV2_CBCRejectsMisalignedInput covers the block-alignment gates. CBC has no
// opinion about padding, so these are the only guards between a caller's mistake
// and a panic inside crypto/cipher.
func TestEV2_CBCRejectsMisalignedInput(t *testing.T) {
	key := hexBytes(t, katT14Key)
	iv := ev2ZeroIV()
	if _, err := ev2CBCEncrypt(key, iv, make([]byte, 17)); err == nil {
		t.Fatal("encrypted a misaligned plaintext")
	}
	if _, err := ev2CBCEncrypt(key[:8], iv, make([]byte, 16)); err == nil {
		t.Fatal("encrypted under a short key")
	}
	if _, err := ev2CBCDecrypt(key, iv, make([]byte, 17)); err == nil {
		t.Fatal("decrypted a misaligned ciphertext")
	}
	if _, err := ev2CBCDecrypt(key, iv, nil); err == nil {
		t.Fatal("decrypted an empty ciphertext")
	}
	if _, err := ev2CBCDecrypt(key[:8], iv, make([]byte, 16)); err == nil {
		t.Fatal("decrypted under a short key")
	}
}

// TestEV2_SessionKeyDerivationRejectsAShortKey covers ev2SessionKeys' only error
// path — an authentication key that is not a valid AES size, which can only be a
// programming error upstream and must be surfaced rather than swallowed (§7).
func TestEV2_SessionKeyDerivationRejectsAShortKey(t *testing.T) {
	enc, mac, err := ev2SessionKeys(hexBytes(t, katT14Key)[:7], hexBytes(t, katT14RndA), hexBytes(t, katT14RndB))
	if err == nil {
		t.Fatal("expected an error")
	}
	if enc != nil || mac != nil {
		t.Fatal("returned session keys alongside an error")
	}
	assertNoKeyBytesInMessage(t, err.Error(), hexBytes(t, katT14RndA))
}

// TestEV2_ChangeKeyCommandRejectsAnInvalidKeyNumber checks that the ChangeKey
// wrapper refuses before it seals anything — NT4H2421Gx rev. 3.0 §10.6.1 Table 63
// allows 0h..4h only.
func TestEV2_ChangeKeyCommandRejectsAnInvalidKeyNumber(t *testing.T) {
	auth := katAuth(t, katT19TI, katT23KeyENC, katT23KeyMAC)
	newKey := hexBytes(t, katT25NewKey)
	got, err := EV2ChangeKeyCommand(auth, 2, 0x05, hexBytes(t, katT25OldKey), newKey, 0x01)
	if err == nil {
		t.Fatal("expected an error")
	}
	if got != nil {
		t.Fatal("returned a command alongside an error")
	}
	assertNoKeyBytesInMessage(t, err.Error(), newKey)
}
