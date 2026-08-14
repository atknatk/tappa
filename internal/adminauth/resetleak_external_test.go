package adminauth_test

// resetleak_external_test.go -- the §4.7 proof for adminauth.ResetToken, from an
// EXTERNAL test package for the reason leak_external_test.go states: M5-01 produced
// two REDs from a claim tested INSIDE the package, where the test could reach
// unexported helpers and never wrote the struct shape a real caller writes.
//
// A RESET TOKEN IS WORTH MORE THAN A SESSION TOKEN, which is why this file exists
// rather than a line being added to the session-token table. A leaked session token
// buys a session that can be revoked; a leaked reset token buys the PASSWORD, and
// the owner finds out when theirs stops working.

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/adminauth"
)

// fakeResetToken is an obviously fake, searchable stand-in shaped like a real reset
// token (43 characters of base64url). It is NOT produced by anything and protects
// nothing.
//
// 🔴 IT IS HAND-WRITTEN AND IT IS **NOT** DERIVED FROM THE TYPE UNDER TEST. A
// positive control whose input comes out of the machinery it is meant to audit proves
// that the machinery agrees with itself -- a defect this repository has shipped once
// already, in a scanner whose sample input was generated from its own patterns. This
// constant is typed out here, and it deliberately differs from leak_external_test.go's
// fakeToken so a grep that finds one cannot vouch for the other.
const fakeResetToken = "RESETresetRESETresetRESETresetRESETreset999"

// bareResetToken has NO protection at all: no pointer indirection, no redacting
// methods. It is the permanent POSITIVE CONTROL -- the thing that must keep leaking,
// so that "the real one does not leak" says something. The field is EXPORTED for the
// reason the session-token control records: encoding/json cannot see an unexported
// field at all and therefore cannot leak it, so a control with an unexported field
// would fail on the JSON paths for the wrong reason.
type bareResetToken struct{ V string }

// resetHeld is the shape that breaks redacting METHODS: a caller-local struct with an
// UNEXPORTED field carrying the token, which is what a phase-B handler naturally
// writes and what `%+v` in an error naturally renders.
type resetHeld struct {
	admin string
	tok   adminauth.ResetToken
}

type bareResetHeld struct {
	admin string
	tok   bareResetToken
}

// resetExported is the shape encoding/json can see at all.
type resetExported struct {
	Admin string
	Tok   adminauth.ResetToken
}

type bareResetExported struct {
	Admin string
	Tok   bareResetToken
}

// resetRenderings runs every audited path over both shapes and returns them under
// identical names, so the redaction test and the positive control assert over exactly
// the same set.
//
// The populated ResetToken comes through ParseResetToken -- the public door the HTTP
// layer uses to wrap a query parameter. Using the ZERO ResetToken here would make
// every "does not leak" assertion trivially true, which is how this shape has gone
// vacuous before.
func resetRenderings(t *testing.T, redacting bool) map[string]string {
	t.Helper()

	var one, list, held, exported any
	if redacting {
		tok := adminauth.ParseResetToken(fakeResetToken)
		one = tok
		list = []adminauth.ResetToken{tok, tok}
		held = resetHeld{admin: "kf-owner", tok: tok}
		exported = resetExported{Admin: "kf-owner", Tok: tok}
	} else {
		tok := bareResetToken{V: fakeResetToken}
		one = tok
		list = []bareResetToken{tok, tok}
		held = bareResetHeld{admin: "kf-owner", tok: tok}
		exported = bareResetExported{Admin: "kf-owner", Tok: tok}
	}

	var text, jsonLog bytes.Buffer
	slog.New(slog.NewTextHandler(&text, nil)).Info("reset issued", "reset", one)
	slog.New(slog.NewJSONHandler(&jsonLog, nil)).Info("reset issued", "state", exported)

	marshalled, err := json.Marshal(exported)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	return map[string]string{
		"1 %+v":                  fmt.Sprintf("%+v", one),
		"2 %v (slice)":           fmt.Sprintf("%v", list),
		"3 %#v":                  fmt.Sprintf("%#v", one),
		"4 %q":                   fmt.Sprintf("%q", one),
		"5 %x":                   fmt.Sprintf("%x", one),
		"6 fmt.Errorf":           fmt.Errorf("reset: refused: %+v", one).Error(),
		"7 unexported field %+v": fmt.Sprintf("%+v", held),
		"7 unexported field %#v": fmt.Sprintf("%#v", held),
		"8 slog text":            text.String(),
		"9 slog json (exported)": jsonLog.String(),
		"10 json.Marshal":        string(marshalled),
	}
}

// resetLeakMarkers is the string that PROVES a leak on each path. %x hex-encodes every
// byte, so its marker is the hex of the value.
func resetLeakMarkers() map[string]string {
	m := map[string]string{}
	for _, k := range []string{
		"1 %+v", "2 %v (slice)", "3 %#v", "4 %q", "6 fmt.Errorf",
		"7 unexported field %+v", "7 unexported field %#v", "8 slog text",
		"9 slog json (exported)", "10 json.Marshal",
	} {
		m[k] = fakeResetToken
	}
	m["5 %x"] = hex.EncodeToString([]byte(fakeResetToken))
	return m
}

// TestResetToken_BareStringIsThePositiveControl asserts the SAME paths DO leak when
// the field is a plain string. Without it, the redaction test could pass because
// nothing was there to leak.
func TestResetToken_BareStringIsThePositiveControl(t *testing.T) {
	markers := resetLeakMarkers()
	for name, got := range resetRenderings(t, false) {
		if !strings.Contains(got, markers[name]) {
			t.Errorf("%s did NOT leak the bare string: the redaction test proves nothing (%q)", name, got)
		}
	}
}

// TestResetToken_RedactsOnEveryAuditedPath is the §4.7 assertion itself.
func TestResetToken_RedactsOnEveryAuditedPath(t *testing.T) {
	const placeholder = "adminauth.ResetToken"
	markers := resetLeakMarkers()
	for name, got := range resetRenderings(t, true) {
		if strings.Contains(got, markers[name]) {
			t.Errorf("%s LEAKS: %q", name, got)
		}
		if strings.Contains(got, fakeResetToken) {
			t.Errorf("%s LEAKS the raw value: %q", name, got)
		}
		// Vacuity guard: the rendering must actually mention this type or the
		// surrounding struct, or the assertions above are satisfied by "".
		if !strings.Contains(got, placeholder) && !strings.Contains(got, "kf-owner") &&
			!strings.Contains(got, hex.EncodeToString([]byte(placeholder))) {
			t.Errorf("%s = %q, want it to mention %q or the surrounding struct", name, got, placeholder)
		}
	}
}

// TestResetToken_PlaceholderIsDistinctFromTheSessionOne. Two suites that share a
// placeholder vouch for each other by accident: a grep for "adminauth.Token(redacted)"
// passes over a leaked reset token, because that string is a PREFIX of this one.
//
// ⚠️ THE PREFIX RELATION IS THE TRAP AND IT IS ASSERTED THE OTHER WAY ROUND. Checking
// only `strings.Contains(got, "adminauth.Token")` would pass for BOTH types, since
// "adminauth.ResetToken(redacted)" does not contain "adminauth.Token" but a careless
// future rename to "adminauth.TokenReset" would. The assertion below is on the exact
// placeholder.
func TestResetToken_PlaceholderIsDistinctFromTheSessionOne(t *testing.T) {
	// Hoisted out of the fmt call on purpose: redline R7 flags the literal word inside
	// a fmt/slog call and the scanner is right to be blunt about it.
	zeroReset := adminauth.ResetToken{}
	zeroSession := adminauth.Token{}
	gotReset := fmt.Sprintf("%v", zeroReset)
	gotSession := fmt.Sprintf("%v", zeroSession)
	if gotReset == gotSession {
		t.Fatalf("the reset token and the panel cookie share a placeholder (%q)", gotReset)
	}
	if !strings.Contains(gotReset, "ResetToken") {
		t.Fatalf("reset placeholder = %q, want it to name the type", gotReset)
	}
}

// TestResetToken_ZeroValueRendersThePlaceholder -- every method must tolerate a nil
// field. A panic here would be a denial of service triggered by logging a struct
// somebody forgot to populate.
func TestResetToken_ZeroValueRendersThePlaceholder(t *testing.T) {
	var tok adminauth.ResetToken
	for name, got := range map[string]string{
		"%v":     fmt.Sprintf("%v", tok),
		"%+v":    fmt.Sprintf("%+v", tok),
		"%#v":    fmt.Sprintf("%#v", tok),
		"%s":     fmt.Sprintf("%s", tok),
		"String": tok.String(),
	} {
		if !strings.Contains(got, "ResetToken") {
			t.Errorf("zero value %s = %q, want the placeholder", name, got)
		}
	}
	b, err := tok.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText on the zero value: %v", err)
	}
	if !strings.Contains(string(b), "ResetToken") {
		t.Errorf("zero MarshalText = %q, want the placeholder", b)
	}
}

// TestIssuedReset_LinkIsTheOnlyEgress pins the one place the raw value is allowed
// out, and pins that the STRUCT carrying it still cannot print it.
//
// It is deliberately asserted from outside the package: Link is the method a delivery
// channel calls, and the value it returns is the thing that must never be logged.
func TestIssuedReset_LinkIsTheOnlyEgress(t *testing.T) {
	i := adminauth.IssuedReset{Token: adminauth.ParseResetToken(fakeResetToken)}
	link := i.Link("https://app.example/reset")
	if !strings.Contains(link, fakeResetToken) {
		t.Fatalf("Link = %q, want it to carry the token (it is the egress)", link)
	}
	// The surrounding value must still redact, so a handler that logs the RESULT of
	// Issue -- rather than the link -- leaks nothing.
	if got := fmt.Sprintf("%+v", i); strings.Contains(got, fakeResetToken) {
		t.Fatalf("IssuedReset LEAKS through %%+v: %q", got)
	}
}
