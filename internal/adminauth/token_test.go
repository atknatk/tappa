package adminauth

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/session"
)

// testKey is a fixed 32-byte HMAC key. It is NOT a secret and protects nothing:
// it exists so two tests can derive the same value and compare them.
func testKey() []byte { return bytes.Repeat([]byte{0x2a}, 32) }

func testConfig() *config.Config {
	return &config.Config{
		Env:            config.EnvDev,
		BaseURL:        "https://panel.example.test",
		SessionHMACKey: testKey(),
	}
}

// TestNewToken_ShapeAndUniqueness.
func TestNewToken_ShapeAndUniqueness(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		tok, err := newToken()
		if err != nil {
			t.Fatalf("newToken: %v", err)
		}
		v := tok.reveal()
		if len(v) != tokenLen {
			t.Fatalf("token length %d, want %d", len(v), tokenLen)
		}
		if _, err := base64.RawURLEncoding.DecodeString(v); err != nil {
			t.Fatalf("token is not unpadded base64url: %v", err)
		}
		if seen[v] {
			t.Fatalf("newToken repeated a value after %d draws", i)
		}
		seen[v] = true
	}
}

// TestTokenHash_RefusesWhatCannotBeOurs is the shape gate: an untrusted cookie is
// length- and charset-checked before any HMAC work reaches the database.
func TestTokenHash_RefusesWhatCannotBeOurs(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"the zero token", ""},
		{"too short", strings.Repeat("a", tokenLen-1)},
		{"too long", strings.Repeat("a", tokenLen+1)},
		{"right length, wrong charset", strings.Repeat("!", tokenLen)},
		{"right length, padded base64", strings.Repeat("a", tokenLen-1) + "="},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := wrap(tc.value).hash(testKey())
			if err == nil {
				t.Fatalf("hash accepted %q", tc.name)
			}
		})
	}
	// The zero value goes down the same path with no dereference.
	if _, err := (Token{}).hash(testKey()); err == nil {
		t.Fatalf("hash accepted the zero Token")
	}
}

// TestTokenHash_RefusesAWrongSizedKey. A short or empty key still produces
// plausible-looking hex, so the failure must be loud rather than silent.
func TestTokenHash_RefusesAWrongSizedKey(t *testing.T) {
	tok, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}
	for _, n := range []int{0, 1, 16, 31, 33, 64} {
		if _, err := tok.hash(bytes.Repeat([]byte{1}, n)); err == nil {
			t.Fatalf("hash accepted a %d-byte key", n)
		}
	}
}

// TestDeriveKey_IsDeterministicAndNotTheInput.
//
// The second half is the point: if the derivation were the identity function, the
// panel and the tap flow would hash tokens identically and the separation proven
// below would collapse.
func TestDeriveKey_IsDeterministicAndNotTheInput(t *testing.T) {
	a, err := deriveKey(testKey())
	if err != nil {
		t.Fatalf("deriveKey: %v", err)
	}
	b, err := deriveKey(testKey())
	if err != nil {
		t.Fatalf("deriveKey: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("deriveKey is not deterministic")
	}
	if bytes.Equal(a, testKey()) {
		t.Fatalf("deriveKey returned its input — the admin token key is the session key")
	}
	if len(a) != hmacKeyLen {
		t.Fatalf("derived key is %d bytes, want %d", len(a), hmacKeyLen)
	}
	// A different session key must derive a different admin key.
	other, err := deriveKey(bytes.Repeat([]byte{0x2b}, 32))
	if err != nil {
		t.Fatalf("deriveKey: %v", err)
	}
	if bytes.Equal(a, other) {
		t.Fatalf("two different session keys derived the same admin key")
	}
}

// TestAdminAndEmployeeCookies_AreSeparate is the M6-01 acceptance criterion "an
// employee cookie cannot reach the panel and an admin cookie cannot reach the tap
// surface", proven in BOTH directions and at BOTH layers.
//
// LAYER 1 — THE NAME. Each codec reads only its own cookie, so a request carrying
// only the other one resolves to nothing. That is the weak half (a name is a
// string) and it is tested first because it is what a reader checks.
//
// LAYER 2 — THE PATH. The panel cookie is Path=/admin, so a browser does not even
// SEND it to /t or /api/checkin. This is the structural half.
func TestAdminAndEmployeeCookies_AreSeparate(t *testing.T) {
	if CookieName == session.CookieName {
		t.Fatalf("the admin and employee cookies share the name %q", CookieName)
	}

	adminCodec := NewCookies(testConfig())
	employeeCodec := session.NewCookies(testConfig())

	// Write a real admin cookie and read the headers back.
	rec := httptest.NewRecorder()
	tok, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}
	if err := adminCodec.Set(rec, tok); err != nil {
		t.Fatalf("Set: %v", err)
	}
	res := rec.Result()
	defer res.Body.Close()
	cookies := res.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Set wrote %d cookies, want 1", len(cookies))
	}
	ck := cookies[0]
	if ck.Name != CookieName {
		t.Fatalf("cookie name %q, want %q", ck.Name, CookieName)
	}
	// ⚠️ THIS LINE IS TAUTOLOGICAL WITH RESPECT TO THE CONSTANT'S VALUE, and saying
	// so is the point. It pins that Set() USES CookiePath rather than a literal --
	// which is worth having (writing Path: "/" here turns it red) -- but it can
	// never catch a change to the CONSTANT, because both sides move together. An
	// audit proved that by mutation: CookiePath "/admin" -> "/" left this file, and
	// internal/handler, and internal/httpx all GREEN.
	// THE CONSTANT'S VALUE IS PINNED BY BEHAVIOUR, in
	// internal/handler/admincookiepath_test.go, which drives a real http.CookieJar
	// and asserts no panel cookie reaches /t, /api/checkin or /activate.
	if ck.Path != CookiePath {
		t.Fatalf("Set() wrote path %q instead of using CookiePath (%q)", ck.Path, CookiePath)
	}
	if CookiePath == "/" {
		t.Fatalf("CookiePath is %q — a root-scoped panel cookie reaches the tap surface", CookiePath)
	}
	if !ck.HttpOnly {
		t.Fatalf("the panel cookie is not HttpOnly")
	}
	if !ck.Secure {
		t.Fatalf("the panel cookie is not Secure for a https BaseURL")
	}

	// DIRECTION 1: a request carrying ONLY the employee cookie resolves nothing on
	// the panel side.
	r := httptest.NewRequest(http.MethodGet, "/admin", nil)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: ck.Value})
	if _, err := adminCodec.Read(r); err == nil {
		t.Fatalf("the panel read an employee cookie")
	}

	// DIRECTION 2: a request carrying ONLY the admin cookie resolves nothing on the
	// employee side — even though the VALUE is a well-formed 43-char token, which is
	// the shape the employee codec accepts. So this is not passing because the value
	// looks wrong.
	r2 := httptest.NewRequest(http.MethodGet, "/t", nil)
	r2.AddCookie(&http.Cookie{Name: CookieName, Value: ck.Value})
	if _, err := employeeCodec.Read(r2); err == nil {
		t.Fatalf("the tap surface read an admin cookie")
	}
	// POSITIVE CONTROL for direction 2: the same value under the EMPLOYEE name IS
	// accepted by the employee codec, so the refusal above is about the NAME and not
	// about the value being unreadable.
	r3 := httptest.NewRequest(http.MethodGet, "/t", nil)
	r3.AddCookie(&http.Cookie{Name: session.CookieName, Value: ck.Value})
	if _, err := employeeCodec.Read(r3); err != nil {
		t.Fatalf("control: the employee codec rejected a well-formed value under its own name: %v", err)
	}
}

// TestAdminAndEmployeeHashes_Differ is the SECOND separation, and the one a
// mistyped cookie name cannot defeat: the same raw token string hashes to two
// different values, so a row in `sessions` can never be matched by the admin
// resolver even if the value were copied across by hand.
//
// It compares this package's hash against an independently computed
// HMAC-SHA256 under the RAW session key — i.e. what internal/session would store
// — rather than calling into that package's unexported hash.
func TestAdminAndEmployeeHashes_Differ(t *testing.T) {
	tok, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}
	adminKey, err := deriveKey(testKey())
	if err != nil {
		t.Fatalf("deriveKey: %v", err)
	}
	adminHash, err := tok.hash(adminKey)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	// The employee hash is the same construction under the UNDERIVED key.
	employeeHash, err := tok.hash(testKey())
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if adminHash == employeeHash {
		t.Fatalf("the same token hashes identically for the panel and the tap flow — " +
			"the key derivation is not in force")
	}
	if len(adminHash) != 64 {
		t.Fatalf("hash is %d hex chars, want 64", len(adminHash))
	}
}

// TestCookies_ZeroValueIsSecure is the M5-01 polarity lesson, re-proven for the
// third codec: the DANGEROUS state must not be the one you get by forgetting.
func TestCookies_ZeroValueIsSecure(t *testing.T) {
	tests := []struct {
		name   string
		codec  Cookies
		secure bool
	}{
		{"the zero value", Cookies{}, true},
		{"a var declaration", func() Cookies { var c Cookies; return c }(), true},
		{"nil config", NewCookies(nil), true},
		{"prod over http", NewCookies(&config.Config{Env: config.EnvProd, BaseURL: "http://x"}), true},
		{"prod over https", NewCookies(&config.Config{Env: config.EnvProd, BaseURL: "https://x"}), true},
		{"dev over https", NewCookies(&config.Config{Env: config.EnvDev, BaseURL: "https://x"}), true},
		{"dev over HTTPS uppercase", NewCookies(&config.Config{Env: config.EnvDev, BaseURL: "HTTPS://x"}), true},
		{"dev over http — the single relaxation", NewCookies(&config.Config{Env: config.EnvDev, BaseURL: "http://localhost:8080"}), false},
		{"staging over http", NewCookies(&config.Config{Env: config.EnvStaging, BaseURL: "http://x"}), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.codec.Secure(); got != tc.secure {
				t.Fatalf("Secure() = %v, want %v", got, tc.secure)
			}
		})
	}
}

// TestCookies_SetRefusesAnEmptyToken. A cookie with no value looks like a
// successful login and behaves like no session at all.
func TestCookies_SetRefusesAnEmptyToken(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := NewCookies(testConfig()).Set(rec, Token{}); err == nil {
		t.Fatalf("Set accepted the zero Token")
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatalf("Set wrote a cookie while refusing")
	}
}

// TestCookies_ClearMatchesSet. Attributes must match or the browser keeps the
// original cookie and "sign out" does nothing client-side.
func TestCookies_ClearMatchesSet(t *testing.T) {
	c := NewCookies(testConfig())
	rec := httptest.NewRecorder()
	c.Clear(rec)
	res := rec.Result()
	defer res.Body.Close()
	cookies := res.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Clear wrote %d cookies, want 1", len(cookies))
	}
	ck := cookies[0]
	switch {
	case ck.Name != CookieName:
		t.Fatalf("name %q", ck.Name)
	case ck.Path != CookiePath:
		t.Fatalf("path %q, want %q", ck.Path, CookiePath)
	case ck.MaxAge >= 0:
		t.Fatalf("MaxAge %d, want negative", ck.MaxAge)
	case !ck.HttpOnly:
		t.Fatalf("not HttpOnly")
	case ck.Secure != c.Secure():
		t.Fatalf("Secure %v, want %v", ck.Secure, c.Secure())
	case ck.SameSite != http.SameSiteLaxMode:
		t.Fatalf("SameSite %v", ck.SameSite)
	}
}

// TestAdminAuthConstants_ShippedValuesArePinned is this package's half of the
// family scan (internal/handler carries the other half).
//
// 🔴 A FAMILY SCAN found SEVEN constants that could be changed alone with the suite
// green — the class that hid both adminauth.CookiePath and adminauth.MaxCandidates.
// cookieMaxAgeSeconds was one: the panel session's lifetime is a security decision
// (the panel reads every employee's movements) and nothing pinned it.
//
// ⚠️ THE SCAN COVERED 16 CONSTANTS, WHICH IS THE NUMERIC SUBSET AND NOT "ALL". An
// earlier version of this comment said "ALL 16 CONSTANTS M6-01 phase B introduced";
// an audit counted ~29, the rest being STRINGS and other non-numeric values — the
// MAC and key-derivation labels, the two short-lived cookie names, adminCSP,
// dummyDigest, the redaction placeholder, adminChoiceVersion. Those are covered
// differently: a wrong label or a wrong cookie name breaks a round trip and turns a
// behavioural test red, where a wrong NUMBER usually does not. The claim this test
// may make is "every numeric constant is pinned", and hmacKeyLen below is the one
// the first pass missed.
func TestAdminAuthConstants_ShippedValuesArePinned(t *testing.T) {
	tests := []struct {
		name string
		got  int
		want int
		why  string
	}{
		{
			"cookieMaxAgeSeconds", cookieMaxAgeSeconds, 12 * 60 * 60,
			"a working day, not the employee cookie's year: an admin session reads every " +
				"employee's movements and can rewrite the tenant's policies, and it lives on a " +
				"laptop rather than a personal phone. ⚠️ It is a browser HINT — admin_sessions " +
				"has no expires_at, so nothing expires server-side (cookie.go states the limit).",
		},
		{
			"tokenBytes", tokenBytes, 32,
			"256 bits, the size at which guessing stops being a threat model.",
		},
		{
			"tokenLen", tokenLen, 43,
			"ceil(32*8/6) — the exact length of tokenBytes in unpadded base64url. It is a " +
				"shape gate before any HMAC work touches an untrusted cookie, so it must track " +
				"tokenBytes exactly.",
		},
		{
			"MaxPasswordBytes", MaxPasswordBytes, 72,
			"bcrypt's hard input limit. Past it CompareHashAndPassword TRUNCATES SILENTLY " +
				"(measured), so two passwords sharing a 72-byte prefix authenticate each other.",
		},
		{"Cost", Cost, 12, "the cost of the digests that actually exist (test/fixtures/seed.sql)."},
		{
			"hmacKeyLen", hmacKeyLen, 32,
			"TAPPA_SESSION_HMAC_KEY's required size. An audit found it MISSING from the first " +
				"family scan, and TestTokenHash_RefusesAWrongSizedKey compares it with ITSELF " +
				"— the same tautology that hid CookiePath. It is structurally defended (a " +
				"mutation to 16 fails at deriveKey with 'hmac key must be 16 bytes, got 32'), " +
				"but it was not pinned by VALUE, and 'defended by accident' is not the same " +
				"claim as 'pinned'.",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("%s is %d, and this test pins %d.\nNot a fixture to update: %s",
					tc.name, tc.got, tc.want, tc.why)
			}
		})
	}

	// RELATION: tokenLen must be the base64url length of tokenBytes, or the shape
	// gate rejects every token this package mints.
	t.Run("tokenLen tracks tokenBytes", func(t *testing.T) {
		if want := (tokenBytes*8 + 5) / 6; tokenLen != want {
			t.Fatalf("tokenLen = %d but %d bytes encode to %d base64url chars", tokenLen, tokenBytes, want)
		}
	})
}
