// Package adminauth_test is an EXTERNAL test package on purpose.
//
// M5-01 produced two REDs from this exact shape: a claim that "no caller can print
// this value" was written and tested from INSIDE the package, where the test could
// reach unexported helpers and never wrote the struct shape a real caller writes.
// Everything below is what internal/handler can actually do with these types.
package adminauth_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
)

// fakeToken is an obviously fake, searchable stand-in shaped like a real token
// (43 characters of base64url). It is NOT produced by anything and protects
// nothing; it exists so the assertions can grep for something specific.
const fakeToken = "FAKEfakeFAKEfakeFAKEfakeFAKEfakeFAKEfake123"

// bareToken is a token type with NO protection at all: no pointer indirection, no
// redacting methods. It is the permanent POSITIVE CONTROL — the thing that must
// keep leaking, so that "the real one does not leak" says something.
//
// THE FIELD IS EXPORTED, and that is a deliberate correction rather than a
// slip. An earlier version used an unexported one and the control FAILED on the
// two JSON paths — correctly, because encoding/json cannot see an unexported
// field at all and therefore cannot leak it. That measurement is worth keeping in
// words: on the JSON paths, adminauth.Token's protection is MarshalText and not
// the indirection, so the control there has to be a type whose value json can
// actually reach. On the fmt paths both mechanisms are in play and this shape
// exercises them the same way.
type bareToken struct{ V string }

// loginState is the shape that breaks redacting METHODS: a caller-local struct
// with an UNEXPORTED field carrying the token. It is what a handler naturally
// writes and what `slog.Info("issued", "state", st)` or a `%+v` in an error
// naturally renders. M5-01 measured that fmt skips Formatter/Stringer for a value
// it may not hand to an interface (reflect CanInterface() == false) and falls
// through to printing the field's contents.
type loginState struct {
	admin string
	tok   adminauth.Token
}

type bareLoginState struct {
	admin string
	tok   bareToken
}

// exportedState is the shape the ORIGINAL in-package test used, kept beside the
// unexported one because that is the whole M5-01 lesson: the exported case passed
// all along and proved nothing about the unexported one. It is also the shape
// encoding/json can see at all — json drops unexported fields, so the JSON paths
// have to go through this one to be meaningful.
type exportedState struct {
	Admin string
	Tok   adminauth.Token
}

type bareExportedState struct {
	Admin string
	Tok   bareToken
}

// externalToken builds a POPULATED adminauth.Token from outside the package,
// through the only public door that accepts one: reading a request cookie.
//
// THIS IS WHAT KEEPS THE REDACTION TEST FROM BEING VACUOUS. An earlier version of
// this file used the ZERO Token, which cannot leak because it holds nothing — so
// every "does not leak" assertion was trivially true. Cookies' zero value is
// constructible here because its only field is unexported, which is exactly how a
// caller in another package would have to obtain a Token without a database.
func externalToken(t *testing.T) adminauth.Token {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, adminauth.CookiePath, nil)
	req.AddCookie(&http.Cookie{Name: adminauth.CookieName, Value: fakeToken})
	var c adminauth.Cookies
	tk, err := c.Read(req)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return tk
}

// renderings runs every audited path over both shapes and returns them under
// identical names, so the redaction test and the positive control assert over
// exactly the same set.
func renderings(t *testing.T, redacting bool) map[string]string {
	t.Helper()

	var one, list, held, exported any
	if redacting {
		tok := externalToken(t)
		one = tok
		list = []adminauth.Token{tok, tok}
		held = loginState{admin: "kf-owner", tok: tok}
		exported = exportedState{Admin: "kf-owner", Tok: tok}
	} else {
		tok := bareToken{V: fakeToken}
		one = tok
		list = []bareToken{tok, tok}
		held = bareLoginState{admin: "kf-owner", tok: tok}
		exported = bareExportedState{Admin: "kf-owner", Tok: tok}
	}

	var text, jsonLog bytes.Buffer
	slog.New(slog.NewTextHandler(&text, nil)).Info("issued", "token", one)
	slog.New(slog.NewJSONHandler(&jsonLog, nil)).Info("issued", "state", exported)

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
		"6 fmt.Errorf":           fmt.Errorf("panel: refused: %+v", one).Error(),
		"7 unexported field %+v": fmt.Sprintf("%+v", held),
		"7 unexported field %#v": fmt.Sprintf("%#v", held),
		"8 slog text":            text.String(),
		"9 slog json (exported)": jsonLog.String(),
		"10 json.Marshal":        string(marshalled),
	}
}

// leakMarkers is the string that PROVES a leak on each path. It is per-path
// because two paths do not render a string as itself:
//
//	%x   hex-encodes every byte, so the marker is the hex of the value. An
//	     earlier version of this test looked for the raw string here and the
//	     POSITIVE CONTROL failed — correctly. That failure is why this map exists.
//	json encoding/json cannot see an UNEXPORTED field at all, so the JSON paths
//	     are driven from exportedState; there the marker is the raw value again.
func leakMarkers() map[string]string {
	hexed := hex.EncodeToString([]byte(fakeToken))
	m := map[string]string{}
	for _, k := range []string{
		"1 %+v", "2 %v (slice)", "3 %#v", "4 %q", "6 fmt.Errorf",
		"7 unexported field %+v", "7 unexported field %#v", "8 slog text",
		"9 slog json (exported)", "10 json.Marshal",
	} {
		m[k] = fakeToken
	}
	m["5 %x"] = hexed
	return m
}

// TestToken_BareStringIsThePositiveControl asserts the SAME paths DO leak when the
// field is a plain string. Without this, the redaction test could pass because
// nothing was there to leak. It is what turns a revert of the field type into a
// red build.
func TestToken_BareStringIsThePositiveControl(t *testing.T) {
	markers := leakMarkers()
	for name, got := range renderings(t, false) {
		if !strings.Contains(got, markers[name]) {
			t.Errorf("%s did NOT leak the bare string: the redaction test proves nothing (%q)", name, got)
		}
	}
}

// TestToken_RedactsOnEveryAuditedPath.
func TestToken_RedactsOnEveryAuditedPath(t *testing.T) {
	const placeholder = "adminauth.Token"
	markers := leakMarkers()
	for name, got := range renderings(t, true) {
		if strings.Contains(got, markers[name]) {
			t.Errorf("%s LEAKS: %q", name, got)
		}
		// The raw value must never appear on ANY path, whatever that path's own
		// marker is.
		if strings.Contains(got, fakeToken) {
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

// TestToken_PlaceholderIsDistinctFromTheEmployeeOne. Two suites that share a
// placeholder can vouch for each other by accident: a grep for
// "session.Token(redacted)" passes over a leaked adminauth value.
func TestToken_PlaceholderIsDistinctFromTheEmployeeOne(t *testing.T) {
	// Hoisted out of the fmt call on purpose: redline R7 flags the literal word
	// inside a fmt/slog call and the scanner is right to be blunt about it (the
	// same hoisting internal/db's leak test does).
	zero := adminauth.Token{}
	got := fmt.Sprintf("%v", zero)
	if !strings.Contains(got, "adminauth.Token") {
		t.Fatalf("placeholder = %q, want it to name this package", got)
	}
	if strings.Contains(got, "session.Token") {
		t.Fatalf("the panel token borrows internal/session's placeholder: %q", got)
	}
}

// TestToken_ZeroValueRendersThePlaceholder — every method must tolerate a nil
// field. A panic here would be a denial of service triggered by logging a struct
// somebody forgot to populate.
func TestToken_ZeroValueRendersThePlaceholder(t *testing.T) {
	var tok adminauth.Token
	tests := []struct {
		name string
		got  string
	}{
		{"%v", fmt.Sprintf("%v", tok)},
		{"%s", fmt.Sprintf("%s", tok)},
		{"%q", fmt.Sprintf("%q", tok)},
		{"%#v", fmt.Sprintf("%#v", tok)},
		{"%x", fmt.Sprintf("%x", tok)},
		{"String()", tok.String()},
		{"GoString()", tok.GoString()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tc.got, "adminauth.Token") {
				t.Fatalf("= %q, want the placeholder", tc.got)
			}
		})
	}
	b, err := tok.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText: %v", err)
	}
	if !strings.Contains(string(b), "adminauth.Token") {
		t.Fatalf("MarshalText = %q", b)
	}
	if !strings.Contains(tok.LogValue().String(), "adminauth.Token") {
		t.Fatalf("LogValue = %q", tok.LogValue().String())
	}
}

// TestPanelTypes_CarryNoSecretField is the STRUCTURAL half, and it is the one that
// survives a refactor: rather than rendering a value and grepping, it walks the
// FIELD SET of every type this package hands to a handler and fails on any field
// whose type could carry a password, a digest or a raw token.
//
// WHY A FIELD-SET TEST AND NOT ONLY A RENDERING TEST. A rendering test proves what
// TODAY'S values print. This one fails the moment somebody ADDS a `PasswordHash
// string` to Attempt "just for logging" — which is the shape the M6-01 card warns
// about (store.AdminUser.PasswordHash is a bare string and a handler that prints
// it still leaks). The check is deliberately blunt: on these types every string
// field must be one of a NAMED allow-list, so a new one is a deliberate edit here.
func TestPanelTypes_CarryNoSecretField(t *testing.T) {
	// Every string field these types are ALLOWED to have, by name. Adding to this
	// list is the visible act; forgetting to is a red test.
	allowed := map[string]map[string]bool{
		"Attempt":        {},
		"Authentication": {},
		"Verified":       {},
		"Choice":         {"TenantName": true, "FullName": true, "Role": true},
		"Session":        {},
		"Issued":         {},
		"Resolved":       {"Role": true, "FullName": true},
	}
	values := []any{
		adminauth.Attempt{},
		adminauth.Authentication{},
		adminauth.Verified{},
		adminauth.Choice{},
		adminauth.Session{},
		adminauth.Issued{},
		adminauth.Resolved{},
	}
	for _, v := range values {
		rt := reflect.TypeOf(v)
		name := rt.Name()
		t.Run(name, func(t *testing.T) {
			ok, listed := allowed[name]
			if !listed {
				t.Fatalf("type %s is not in the allow-list — add it with the string fields it may carry", name)
			}
			for i := 0; i < rt.NumField(); i++ {
				f := rt.Field(i)
				if f.Type.Kind() != reflect.String {
					continue
				}
				if !ok[f.Name] {
					t.Fatalf("%s.%s is a plain string that nothing authorised. A password, a digest "+
						"or a raw token could travel in it; if it is display data, name it in the "+
						"allow-list in this test", name, f.Name)
				}
			}
		})
	}
}

// TestPanelTypes_RenderWithoutASecret is the rendering half over the same types,
// populated with values that WOULD be visible if a field carried them.
func TestPanelTypes_RenderWithoutASecret(t *testing.T) {
	const digest = "$2a$12$FAKEfakeFAKEfakeFAKEfeFAKEfakeFAKEfakeFAKEfakeFAKEfakeFA"
	id, tenant := uuid.New(), uuid.New()
	now := time.Now()

	values := map[string]any{
		"Attempt":        adminauth.Attempt{AdminUserID: id, TenantID: tenant, PasswordMatched: true, Active: true},
		"Authentication": adminauth.Authentication{Resolved: 2, Attempts: []adminauth.Attempt{{AdminUserID: id, TenantID: tenant}}},
		"Verified":       adminauth.Verified{AdminUserID: id, TenantID: tenant},
		"Choice":         adminauth.Choice{AdminUserID: id, TenantID: tenant, TenantName: "Kebab Factory", FullName: "KF Owner", Role: "owner"},
		"Session":        adminauth.Session{ID: id, TenantID: tenant, AdminUserID: id, CreatedAt: now},
		"Issued":         adminauth.Issued{Session: adminauth.Session{ID: id}, Token: adminauth.Token{}},
		"Resolved":       adminauth.Resolved{SessionID: id, TenantID: tenant, AdminUserID: id, Role: "owner", FullName: "KF Owner"},
	}
	for name, v := range values {
		t.Run(name, func(t *testing.T) {
			var text bytes.Buffer
			slog.New(slog.NewTextHandler(&text, nil)).Info("panel", "value", v)
			for path, got := range map[string]string{
				"%+v":  fmt.Sprintf("%+v", v),
				"%#v":  fmt.Sprintf("%#v", v),
				"slog": text.String(),
			} {
				if strings.Contains(got, digest) || strings.Contains(got, fakeToken) {
					t.Fatalf("%s %s leaks: %q", name, path, got)
				}
				if got == "" {
					t.Fatalf("%s %s rendered nothing — the assertion is vacuous", name, path)
				}
			}
		})
	}
}
