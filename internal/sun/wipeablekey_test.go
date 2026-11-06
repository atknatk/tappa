package sun

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
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
// must hit the copy and not the Key: the next call still opens. TestUnwrapAndWipe_WipesOnEveryPath is the wipe.
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

// TestUnwrapAndWipe_WipesOnEveryPath looks at the buffer itself, on the path that opens and the path that
// does not.
//
// MUTANTS (measured, 2026-11-06, reverted): the Zero moved to the error branch -- the opening path red
// here; the Zero deleted -- red here.
func TestUnwrapAndWipe_WipesOnEveryPath(t *testing.T) {
	kek := canaryKey(32, 'K')
	uid := canaryKey(7, 4)
	key := canaryKey(16, 'T')
	ref, err := Wrap(kek, uid, key)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		kek  []byte
		ok   bool
	}{
		{"the KEK that opens", canaryKey(32, 'K'), true},
		{"a KEK that does not", canaryKey(32, 'W'), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := unwrapAndWipe(tc.kek, uid, ref)
			if (err == nil) != tc.ok {
				t.Fatalf("err = %v, want opened = %v", err, tc.ok)
			}
			if tc.ok && !bytes.Equal(got, key) {
				t.Fatal("opened the wrong key")
			}
			if !bytes.Equal(tc.kek, make([]byte, 32)) {
				t.Fatal("the KEK copy survived the call")
			}
		})
	}
}

// TestParams_PrintsNoCMAC (backlog T209): a preview verifies a CMAC and advances nothing, so the value in
// a Params is live. %+v of one used to print it.
//
// MUTANT (measured, 2026-11-06, reverted): config.Key over a plain string -- red here.
func TestParams_PrintsNoCMAC(t *testing.T) {
	p := mustParse(t, "04968A0A7A6B80", "0000AB", "83C0104D8AC39077")
	if p.CMAC.Len() != 8 {
		t.Fatalf("CONTROL: the parsed CMAC is %d bytes", p.CMAC.Len())
	}
	assertKeyHidden(t, "a Params", p, p.CMAC.Reveal())
	assertKeyHidden(t, "a *Params", &p, p.CMAC.Reveal())
}

// TestUnwrapAny_EveryAttemptGoesThroughTheHelper: what TestUnwrapAndWipe_WipesOnEveryPath proves holds only
// if UnwrapAny calls it. A Key hands out a fresh copy on each Reveal, so no run can see a copy that was left
// behind; this reads the source.
//
// MUTANT (measured, 2026-11-06, reverted): `Unwrap(k.Reveal(), uid, ref)` in UnwrapAny -- green in every
// behavioural test, red here.
func TestUnwrapAny_EveryAttemptGoesThroughTheHelper(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "keys.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string]int{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "UnwrapAny" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if id, ok := c.Fun.(*ast.Ident); ok {
					calls[id.Name]++
				}
			}
			return true
		})
	}
	if calls["unwrapAndWipe"] != 1 || calls["Unwrap"] != 0 {
		t.Fatalf("UnwrapAny calls unwrapAndWipe %d time(s) and Unwrap %d: a revealed KEK copy would outlive its attempt",
			calls["unwrapAndWipe"], calls["Unwrap"])
	}
}
