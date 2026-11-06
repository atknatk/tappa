package sun

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
)

// everyVerb is what fmt can be asked: the ones a key would show under, and the badVerb path that opens
// a pointer to a slice.
var everyVerb = []string{"%v", "%+v", "%#v", "%T", "%s", "%q", "%x", "%X", "%d", "%o", "%O", "%b", "%e", "%E",
	"%f", "%F", "%g", "%G", "%t", "%c", "%U", "%p", "%w"}

// renderedEveryWay prints v itself, in a caller's exported and unexported fields, behind a pointer, in a
// slice and a map, through fmt, Errorf, both slog handlers and encoding/json.
func renderedEveryWay(v any) []string {
	type exported struct{ V any }
	type unexported struct{ v any }
	var out []string
	for _, form := range []any{v, exported{v}, unexported{v}, &exported{v}, &unexported{v}, []any{v}, map[string]any{"k": v}} {
		for _, verb := range everyVerb {
			out = append(out, fmt.Sprintf(verb, form), fmt.Errorf(verb, form).Error())
		}
		var text, jsonLog strings.Builder
		slog.New(slog.NewTextHandler(&text, nil)).Info("probe", "v", form)
		slog.New(slog.NewJSONHandler(&jsonLog, nil)).Info("probe", "v", form)
		out = append(out, text.String(), jsonLog.String())
		if b, err := json.Marshal(form); err == nil {
			out = append(out, string(b))
		}
	}
	return out
}

// keySpellings are the ways the bytes of key come out of fmt, %x, slog's base64 and %v of a []byte.
func keySpellings(key []byte) []string {
	bare := fmt.Sprint(key)
	return []string{string(key), hex.EncodeToString(key), strings.ToUpper(hex.EncodeToString(key)),
		base64.StdEncoding.EncodeToString(key), bare[1 : len(bare)-1]}
}

func assertKeyHidden(t *testing.T, what string, v any, keys ...[]byte) {
	t.Helper()
	for _, key := range keys {
		for _, spelling := range keySpellings(key) {
			for i, text := range renderedEveryWay(v) {
				if strings.Contains(text, spelling) {
					t.Fatalf("%s: rendering #%d carries the key's bytes", what, i)
				}
			}
		}
	}
}

func canaryKey(n int, fill byte) []byte { return bytes.Repeat([]byte{fill}, n) }

// TestRenderedEveryWay_SeesARawKey is the control: a []byte field shows, in each spelling, so a green
// below is not the search looking at nothing.
func TestRenderedEveryWay_SeesARawKey(t *testing.T) {
	key := canaryKey(16, 'C')
	for _, spelling := range keySpellings(key) {
		found := false
		for _, text := range renderedEveryWay(struct{ Raw []byte }{key}) {
			found = found || strings.Contains(text, spelling)
		}
		if !found {
			t.Errorf("no rendering of a raw []byte field carries the spelling %q", spelling[:4])
		}
	}
}

// TestWipeableKey_PrintsNoByteOnEveryPath is config.Key's measurement over the new shape (backlog T205).
//
// MUTANTS (measured, 2026-11-06, reverted): `live` replaced by a plain `b []byte` field, and by `b *[]byte`
// -- both red here. Format removed stays GREEN here, because String and the closure still keep the bytes
// out; TestInternal_EveryKeyFieldIsARedactingType is what refuses it.
func TestWipeableKey_PrintsNoByteOnEveryPath(t *testing.T) {
	key := canaryKey(16, 'C')
	assertKeyHidden(t, "a WipeableKey", NewWipeableKey(key), key)
	assertKeyHidden(t, "a struct holding one", struct{ k WipeableKey }{NewWipeableKey(key)}, key)
	assertKeyHidden(t, "a slice of them", []WipeableKey{NewWipeableKey(key)}, key)

	for _, text := range []string{NewWipeableKey(key).String(), NewWipeableKey(key).GoString(), NewWipeableKey(key).LogValue().String()} {
		if text != wipeableKeyRedacted {
			t.Errorf("a printing method said %q", text)
		}
	}
	if b, err := NewWipeableKey(key).MarshalText(); err != nil || string(b) != wipeableKeyRedacted {
		t.Errorf("MarshalText said %q, %v", b, err)
	}
}

// TestWipeableKey_RevealIsTheLiveBuffer is why this type exists beside config.Key: a wipe through Reveal
// reaches the only buffer there is.
//
// MUTANT (measured, 2026-11-06, reverted): NewWipeableKey copying b -- red here, and in internal/encode's
// TestSession_TakeAndPeekReturnTheRingsOWNBuffer and TestDriver_Step9WipesTheKeysBeforeItMarksTheRow.
func TestWipeableKey_RevealIsTheLiveBuffer(t *testing.T) {
	buf := canaryKey(16, 'C')
	k := NewWipeableKey(buf)
	if &k.Reveal()[0] != &buf[0] {
		t.Fatal("Reveal is not the buffer the key was built over: a wipe would miss it")
	}
	if k.Len() != 16 || !k.IsSet() {
		t.Fatalf("Len %d, IsSet %v", k.Len(), k.IsSet())
	}
	Zero(k.Reveal())
	if !bytes.Equal(buf, make([]byte, 16)) {
		t.Fatal("a wipe through Reveal left the bytes")
	}
	if !k.IsSet() {
		t.Fatal("a wiped key reads as never set")
	}
	var zero WipeableKey
	if zero.Reveal() != nil || zero.Len() != 0 || zero.IsSet() || NewWipeableKey(nil).IsSet() {
		t.Fatal("the zero WipeableKey is not empty")
	}
	Zero(zero.Reveal())
}

// TestEV2Auth_PrintsNoSessionKey and TestEV2Auth_ZeroWipesThroughTheType (backlog T205): the fields print
// redacted, and EV2Auth.Zero still reaches the buffers the handshake derived.
//
// MUTANT (measured, 2026-11-06, reverted): EV2Auth.Zero's two Zero calls deleted -- red in
// TestEV2Auth_ZeroWipesThroughTheType and TestEV2_AuthZeroWipesBothSessionKeys.
func TestEV2Auth_PrintsNoSessionKey(t *testing.T) {
	enc, mac := canaryKey(16, 'E'), canaryKey(16, 'M')
	auth := EV2Auth{TI: []byte{1, 2, 3, 4}, KeyENC: NewWipeableKey(enc), KeyMAC: NewWipeableKey(mac)}
	assertKeyHidden(t, "an EV2Auth", auth, enc, mac)
	assertKeyHidden(t, "a *EV2Auth", &auth, enc, mac)
}

func TestEV2Auth_ZeroWipesThroughTheType(t *testing.T) {
	enc, mac := canaryKey(16, 'E'), canaryKey(16, 'M')
	auth := &EV2Auth{KeyENC: NewWipeableKey(enc), KeyMAC: NewWipeableKey(mac)}
	auth.Zero()
	if !bytes.Equal(enc, make([]byte, 16)) || !bytes.Equal(mac, make([]byte, 16)) {
		t.Fatal("EV2Auth.Zero left a session key's bytes")
	}
}

// TestVerifier_PrintsNoKEK is the measurement that opened backlog T205: a Verifier's KEK came out under
// %v %+v %#v %x and slog's text handler.
//
// MUTANT (measured, 2026-11-06, reverted): config.Key over a plain string -- red here.
func TestVerifier_PrintsNoKEK(t *testing.T) {
	kek, previous := canaryKey(32, 'K'), canaryKey(32, 'P')
	v := NewVerifier(nil, config.NewKey(kek), config.NewKey(previous))
	assertKeyHidden(t, "a *Verifier", v, kek, previous)
	assertKeyHidden(t, "a Verifier", *v, kek, previous)
	assertKeyHidden(t, "its KEK list", v.keks, kek, previous)
}

// TestVerifier_HoldsConfigsKeyRatherThanACopy: keys.go's rule, one long-lived copy of the KEK.
//
// MUTANT (measured, 2026-11-06, reverted): `config.NewKey(kek.Reveal())` in NewVerifier -- red here.
func TestVerifier_HoldsConfigsKeyRatherThanACopy(t *testing.T) {
	kek, previous := config.NewKey(canaryKey(32, 'K')), config.NewKey(canaryKey(32, 'P'))
	v := NewVerifier(nil, kek, previous)
	if v.keks[0] != kek || v.keks[1] != previous {
		t.Fatal("the Verifier holds a different Key than it was handed: a second long-lived copy of the KEK")
	}
}

// TestUnwrapAny_TheKeySurvivesEachAttempt: UnwrapAny wipes the copy each attempt reveals, and the wipe
// must hit the copy and not the Key: the next call still opens. Whether the copy IS wiped is not
// observable from here (a Key hands out a fresh one each time); zeroCallSiteCensus counts that Zero call.
func TestUnwrapAny_TheKeySurvivesEachAttempt(t *testing.T) {
	kekBytes := canaryKey(32, 'K')
	uid := canaryKey(7, 4)
	key := canaryKey(16, 'T')
	ref, err := Wrap(kekBytes, uid, key)
	if err != nil {
		t.Fatal(err)
	}
	keks := []config.Key{config.NewKey(kekBytes)}
	for i := range 2 {
		got, err := UnwrapAny(keks, uid, ref)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if !bytes.Equal(got, key) {
			t.Fatalf("call %d opened the wrong key", i)
		}
	}
	if _, err := UnwrapAny(nil, uid, ref); err == nil {
		t.Fatal("no KEK at all must be an error")
	}
}
