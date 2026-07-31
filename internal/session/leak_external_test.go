package session_test

// leak_external_test.go -- the §4.7 leak proof from OUTSIDE the package.
//
// WHY A SEPARATE TEST PACKAGE. The first version of this package claimed that an
// unexported string field plus redacting methods made leakage structurally
// impossible. That claim was FALSE and was caught by an independent audit: fmt
// only consults Formatter/Stringer/GoStringer for a value it can hand to an
// interface, and a value read out of an UNEXPORTED struct field cannot be
// (reflect.Value.CanInterface() == false). So fmt fell through to reflection and
// printed the inner string directly. The in-package test missed it because it
// only wrapped the Token in an EXPORTED field.
//
// The idiom that breaks it is completely ordinary Go — a handler-local struct
// with lowercase fields, logged with %+v — and M5-02/M5-03 are about to write
// exactly those handlers. So the regression test lives here, in the position a
// real caller occupies, and covers the unexported-field case specifically.

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/session"
)

// fakeCookieValue is an obviously fake, searchable stand-in for a session value.
// It is NOT produced by the package and is not a real secret (agent-brief madde
// 2); it exists so the assertions can grep for something specific.
const fakeCookieValue = "FAKEfakeFAKEfakeFAKEfakeFAKEfakeFAKEfake123"

// activationState is the shape that broke: a caller-local struct with UNEXPORTED
// fields carrying a Token. This is what an activation or tap handler naturally
// writes, and what a `slog.Info("...", "state", st)` or a `%+v` in an error
// naturally renders.
type activationState struct {
	employee string
	tok      session.Token
}

// exportedState is the shape the original in-package test used. Keeping both
// side by side is the point: the exported case passed all along and proved
// nothing about the unexported one.
type exportedState struct {
	Employee string
	Tok      session.Token
}

// externalToken builds a session.Token holding a known value from outside the
// package, through the only public door that accepts one: reading a request
// cookie. session.Cookies' zero value is constructible here because its only
// field is unexported, which is exactly how a caller in another package would
// have to obtain a Token without a database.
func externalToken(t *testing.T) session.Token {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: fakeCookieValue})
	var c session.Cookies
	tk, err := c.Read(req)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return tk
}

// TestToken_NoLeakThroughUnexportedField is the regression test for the audit
// finding. Every rendering must be free of the value AND must still be produced
// (a positive control), so an empty string cannot pass vacuously.
func TestToken_NoLeakThroughUnexportedField(t *testing.T) {
	tk := externalToken(t)

	unexported := activationState{employee: "maria", tok: tk}
	exported := exportedState{Employee: "maria", Tok: tk}

	var slogText, slogJSON bytes.Buffer
	slog.New(slog.NewTextHandler(&slogText, nil)).Info("activation", "state", unexported)
	slog.New(slog.NewJSONHandler(&slogJSON, nil)).Info("activation", "state", unexported)

	var slogNested bytes.Buffer
	slog.New(slog.NewTextHandler(&slogNested, nil)).Info("activation",
		slog.Group("session", "state", unexported, "sess", tk))

	// Each case carries its own POSITIVE CONTROL: a substring the render must
	// contain, so "the value is absent" can never be trivially true of an empty
	// or unrelated string. The control differs for the JSON handler on purpose --
	// encoding/json drops unexported fields entirely, so it renders "state":{}
	// and can never mention the employee. That is a real (and welcome) property,
	// not a reason to weaken the assertion elsewhere.
	renders := []struct {
		name    string
		got     string
		control string
	}{
		{"unexported field %v", fmt.Sprintf("%v", unexported), "maria"},
		{"unexported field %+v", fmt.Sprintf("%+v", unexported), "maria"},
		{"unexported field %#v", fmt.Sprintf("%#v", unexported), "maria"},
		{"unexported pointer %+v", fmt.Sprintf("%+v", &unexported), "maria"},
		{"unexported slice %+v", fmt.Sprintf("%+v", []activationState{unexported}), "maria"},
		{"unexported map %+v", fmt.Sprintf("%+v", map[string]activationState{"a": unexported}), "maria"},
		{"unexported nested %+v", fmt.Sprintf("%+v", struct{ inner activationState }{unexported}), "maria"},
		{"unexported in error", fmt.Errorf("activation failed: %+v", unexported).Error(), "maria"},
		{"exported field %+v", fmt.Sprintf("%+v", exported), "maria"},
		{"exported field %#v", fmt.Sprintf("%#v", exported), "maria"},
		{"slog text (unexported)", slogText.String(), "maria"},
		{"slog json (unexported)", slogJSON.String(), `"state"`},
		{"slog group (unexported)", slogNested.String(), "maria"},
	}

	for _, r := range renders {
		if strings.Contains(r.got, fakeCookieValue) {
			t.Errorf("%s LEAKS the value: %q", r.name, r.got)
		}
		// A 12-character run is already a meaningful entropy leak.
		if strings.Contains(r.got, fakeCookieValue[:12]) {
			t.Errorf("%s leaks a 12-char prefix: %q", r.name, r.got)
		}
		if !strings.Contains(r.got, r.control) {
			t.Errorf("%s = %q, want it to contain %q (vacuous otherwise)", r.name, r.got, r.control)
		}
	}
}

// TestToken_RedactsWhenReachableAsAnInterface: wherever fmt CAN reach the
// redacting methods (a bare Token, a pointer, an exported field), the
// placeholder must appear. This is the half that always worked; it is kept so a
// fix for the unexported case cannot silently disable the good path.
func TestToken_RedactsWhenReachableAsAnInterface(t *testing.T) {
	tk := externalToken(t)
	exported := exportedState{Employee: "maria", Tok: tk}

	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("issued", "sess", tk)

	// Hoisted: redline R7 flags the literal type name inside a fmt call, and the
	// scanner is right to be blunt about it (M1-09 note) -- so the composite
	// literal is built outside the call rather than smuggled past the scanner.
	// A SECOND Token over the same cookie value. After the pointer indirection
	// these two are distinct keys (comparison is identity, not value — see the
	// Token type comment), so the map really does hold two entries and both must
	// render redacted.
	second := externalToken(t)
	list := []session.Token{tk, second}
	byKey := map[session.Token]string{tk: "maria", second: "maria-again"}
	if len(byKey) != 2 {
		t.Fatalf("map over two distinct Tokens has %d entries, want 2", len(byKey))
	}

	renders := map[string]string{
		"bare %v":        fmt.Sprintf("%v", tk),
		"bare %s":        fmt.Sprintf("%s", tk),
		"bare %q":        fmt.Sprintf("%q", tk),
		"bare %x":        fmt.Sprintf("%x", tk),
		"bare %#v":       fmt.Sprintf("%#v", tk),
		"pointer %v":     fmt.Sprintf("%v", &tk),
		"slice %v":       fmt.Sprintf("%v", list),
		"map key %v":     fmt.Sprintf("%v", byKey),
		"exported %+v":   fmt.Sprintf("%+v", exported),
		"slog attribute": buf.String(),
	}
	for name, got := range renders {
		if strings.Contains(got, fakeCookieValue) {
			t.Errorf("%s leaks the value: %q", name, got)
		}
		if !strings.Contains(got, "redacted") {
			t.Errorf("%s = %q, want the redaction placeholder", name, got)
		}
	}
}

// TestToken_ZeroValueIsSafe: the zero Token must never panic and must redact.
// The indirection the fix introduces makes this worth stating explicitly -- a
// nil inside must not become a nil dereference on a printing path.
func TestToken_ZeroValueIsSafe(t *testing.T) {
	var zero session.Token

	type holder struct {
		note string
		tok  session.Token
	}
	held := holder{note: "no session yet", tok: zero}

	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("zero", "sess", zero, "held", held)

	for name, got := range map[string]string{
		"bare %v":             fmt.Sprintf("%v", zero),
		"bare %s":             fmt.Sprintf("%s", zero),
		"bare %#v":            fmt.Sprintf("%#v", zero),
		"bare %x":             fmt.Sprintf("%x", zero),
		"pointer %v":          fmt.Sprintf("%v", &zero),
		"unexported %+v":      fmt.Sprintf("%+v", held),
		"unexported %#v":      fmt.Sprintf("%#v", held),
		"slog":                buf.String(),
		"String()":            zero.String(),
		"GoString()":          zero.GoString(),
		"LogValue().String()": zero.LogValue().String(),
	} {
		if got == "" {
			t.Errorf("%s produced nothing", name)
		}
	}

	// The zero Token must also be refused by the cookie writer rather than
	// silently written as an empty session.
	var c session.Cookies
	rec := httptest.NewRecorder()
	if err := c.Set(rec, zero); err == nil {
		t.Fatal("Set accepted the zero Token")
	}
	res := rec.Result()
	defer res.Body.Close()
	if n := len(res.Cookies()); n != 0 {
		t.Fatalf("refused Set still wrote %d cookies", n)
	}
}

// ---------------------------------------------------------------------------
// Cookie hardening, measured from OUTSIDE the package.
//
// SECOND AUDIT FINDING, same class as the first: cookie.go claimed "no
// configuration value, env var or caller can produce a non-Secure session
// cookie in production" without anyone ever trying it from a caller's position.
// Go forbids NAMING an unexported field from another package; it does not forbid
// `session.Cookies{}`, `var c session.Cookies`, or a struct field left
// uninitialised — all of which yield the ZERO value. If the zero value means
// "not Secure", every one of those writes a Secure-less session cookie in
// production, and the session value is a §4.7 secret whose only transport
// protection is that flag.
//
// The in-package cookie_test.go covered only the constructor path, which is
// exactly why it missed this. These cases live here, in a caller's position.
// ---------------------------------------------------------------------------

// emittedCookie runs Set on a recorder and returns the emitted cookie.
func emittedCookie(t *testing.T, c session.Cookies) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := c.Set(rec, externalToken(t)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	res := rec.Result()
	defer res.Body.Close()
	cookies := res.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	return cookies[0]
}

// TestCookies_ZeroValueIsSecure: every way a caller can obtain a Cookies value
// WITHOUT the constructor must still emit a Secure cookie. The zero value has to
// be the safe value, because Go gives the package no way to prevent it.
func TestCookies_ZeroValueIsSecure(t *testing.T) {
	// (a) `var c session.Cookies` -- the idiom this repo's own external test
	//     already used before the fix.
	var declared session.Cookies

	// (b) an explicit empty composite literal: legal from any package precisely
	//     because it names no field.
	literal := session.Cookies{}

	// (c) a struct field left uninitialised in a keyed literal. This is the
	//     realistic M5-02/M5-03 shape: forgetting a field is NOT a compile error.
	type handler struct {
		name    string
		cookies session.Cookies
	}
	h := handler{name: "activation"} // cookies never assigned

	cases := map[string]session.Cookies{
		"var declaration":            declared,
		"empty composite literal":    literal,
		"uninitialised struct field": h.cookies,
	}
	for name, c := range cases {
		if !c.Secure() {
			t.Errorf("%s: Secure() = false, want true (the zero value must fail closed)", name)
		}
		if ck := emittedCookie(t, c); !ck.Secure {
			t.Errorf("%s: emitted Set-Cookie without Secure: %+v", name, ck)
		}
		// Clear must match Set, or the browser keeps the original cookie.
		rec := httptest.NewRecorder()
		c.Clear(rec)
		res := rec.Result()
		cleared := res.Cookies()
		res.Body.Close()
		if len(cleared) != 1 || !cleared[0].Secure {
			t.Errorf("%s: Clear emitted a non-Secure cookie: %+v", name, cleared)
		}
	}
}

// TestCookies_ConstructorStillRelaxesOnlyInDev is the positive control for the
// test above: if the zero value is Secure because EVERYTHING is Secure, the
// assertion proves nothing. Local http development must still get a usable
// (non-Secure) cookie.
func TestCookies_ConstructorStillRelaxesOnlyInDev(t *testing.T) {
	dev := session.NewCookies(&config.Config{Env: "dev", BaseURL: "http://localhost:8080"})
	if dev.Secure() {
		t.Fatal("dev over http got a Secure cookie: http://localhost would stop working (vacuous test otherwise)")
	}
	if ck := emittedCookie(t, dev); ck.Secure {
		t.Fatalf("dev over http emitted Secure: %+v", ck)
	}

	prod := session.NewCookies(&config.Config{Env: "prod", BaseURL: "http://app.tappa.mt"})
	if !prod.Secure() {
		t.Fatal("prod did not get a Secure cookie")
	}
}

// TestCookies_SchemeTestIsCaseInsensitive: URL schemes are case-insensitive
// (RFC 3986 §3.1) and config.Load does not normalise BaseURL, so "HTTPS://" used
// to take the relaxation branch — a typo silently buying a weaker cookie on a
// dev/staging box. The relaxation must be asked for, never acquired by accident.
func TestCookies_SchemeTestIsCaseInsensitive(t *testing.T) {
	for _, env := range []string{config.EnvDev, config.EnvStaging} {
		for _, base := range []string{
			"https://app.tappa.mt",
			"HTTPS://app.tappa.mt",
			"Https://app.tappa.mt",
			"hTtPs://app.tappa.mt",
		} {
			c := session.NewCookies(&config.Config{Env: env, BaseURL: base})
			if !c.Secure() {
				t.Errorf("env=%s BaseURL=%q gave a non-Secure cookie; the scheme test must ignore case", env, base)
			}
			if ck := emittedCookie(t, c); !ck.Secure {
				t.Errorf("env=%s BaseURL=%q emitted Set-Cookie without Secure", env, base)
			}
		}
		// Positive control: plain http in the same environment still relaxes,
		// otherwise the assertions above would hold for the wrong reason.
		if session.NewCookies(&config.Config{Env: env, BaseURL: "http://app.tappa.mt"}).Secure() {
			t.Errorf("env=%s over http was Secure; the relaxation branch is unreachable and the test is vacuous", env)
		}
	}
}

// TestCookies_DocumentedTableIsComplete walks every row of NewCookies' doc table,
// including the staging+http row an audit found missing from it. A table in a
// comment is only worth having if something fails when it drifts.
func TestCookies_DocumentedTableIsComplete(t *testing.T) {
	cases := []struct {
		env, baseURL string
		wantSecure   bool
	}{
		{config.EnvProd, "https://app.tappa.mt", true},
		{config.EnvProd, "http://app.tappa.mt", true},
		{config.EnvProd, "", true},
		{config.EnvDev, "https://localhost:8443", true},
		{config.EnvStaging, "https://staging.internal", true},
		{config.EnvDev, "http://localhost:8080", false},
		{config.EnvStaging, "http://staging.internal", false}, // the omitted row
		{config.EnvDev, "not-a-url", false},
		{config.EnvStaging, "", false},
	}
	for _, tc := range cases {
		got := session.NewCookies(&config.Config{Env: tc.env, BaseURL: tc.baseURL}).Secure()
		if got != tc.wantSecure {
			t.Errorf("env=%s BaseURL=%q Secure=%v, want %v (doc table row)", tc.env, tc.baseURL, got, tc.wantSecure)
		}
	}

	// nil config is its own row.
	if !session.NewCookies(nil).Secure() {
		t.Error("nil config produced a non-Secure codec")
	}
}
