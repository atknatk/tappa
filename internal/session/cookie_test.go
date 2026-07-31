package session

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// cookie_test.go -- the transport contract: httpOnly + SameSite=Lax + a long
// Max-Age, Secure forced on in production, and the Set-Cookie header as the ONLY
// place the raw value ever appears.

// setAndParse runs Set on a recorder and returns the resulting cookie.
func setAndParse(t *testing.T, c Cookies, tk Token) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := c.Set(rec, tk); err != nil {
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

// TestCookies_Attributes pins every attribute the card names.
func TestCookies_Attributes(t *testing.T) {
	tk, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}
	ck := setAndParse(t, Cookies{}, tk)

	if ck.Name != CookieName {
		t.Errorf("name = %q, want %q", ck.Name, CookieName)
	}
	if !ck.HttpOnly {
		t.Error("HttpOnly is false: page script must never reach the session value")
	}
	if !ck.Secure {
		t.Error("Secure is false on a secure codec")
	}
	if ck.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax (Strict withholds the cookie on the NFC cold navigation)", ck.SameSite)
	}
	if ck.SameSite == http.SameSiteStrictMode {
		t.Error("SameSite=Strict is the documented card trap and must not be used")
	}
	if ck.Path != "/" {
		t.Errorf("Path = %q, want /", ck.Path)
	}
	// ~1 year, and definitely not a session cookie (MaxAge 0) or an expiry.
	if ck.MaxAge != cookieMaxAgeSeconds {
		t.Errorf("MaxAge = %d, want %d", ck.MaxAge, cookieMaxAgeSeconds)
	}
	if ck.MaxAge < 300*24*60*60 {
		t.Errorf("MaxAge = %ds is shorter than the ~1 year the product promises", ck.MaxAge)
	}
}

// TestCookies_SecureIsStructural: Secure is ON for every production
// configuration, whatever the rest of the config says. The only relaxation needs
// BOTH a non-prod Env AND a non-https base URL -- and it is decided inside the
// constructor, so no caller can request it.
func TestCookies_SecureIsStructural(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		baseURL string
		want    bool
	}{
		{"prod over https", "prod", "https://app.tappa.mt", true},
		{"prod over http (misconfigured) still Secure", "prod", "http://app.tappa.mt", true},
		{"prod with empty base url still Secure", "prod", "", true},
		{"dev over https", "dev", "https://localhost:8443", true},
		{"dev over http", "dev", "http://localhost:8080", false},
		{"staging over http", "staging", "http://staging.internal", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := NewCookies(testConfig(tc.env, tc.baseURL))
			if c.Secure() != tc.want {
				t.Fatalf("Secure() = %v, want %v", c.Secure(), tc.want)
			}
			tk, err := newToken()
			if err != nil {
				t.Fatalf("newToken: %v", err)
			}
			if got := setAndParse(t, c, tk).Secure; got != tc.want {
				t.Fatalf("emitted cookie Secure = %v, want %v", got, tc.want)
			}
		})
	}

	// A nil config must fail SAFE, never into the weaker cookie.
	if !NewCookies(nil).Secure() {
		t.Fatal("NewCookies(nil) produced a non-Secure codec")
	}
}

// TestCookies_SetRefusesEmpty: an empty session cookie would look like a
// successful activation and behave like no session at all.
func TestCookies_SetRefusesEmpty(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := (Cookies{}).Set(rec, Token{}); !errors.Is(err, errEmptyCookie) {
		t.Fatalf("Set(zero) error = %v, want errEmptyCookie", err)
	}
	res := rec.Result()
	defer res.Body.Close()
	if n := len(res.Cookies()); n != 0 {
		t.Fatalf("refused Set still wrote %d cookies", n)
	}
}

// TestCookies_Clear expires the cookie with attributes matching Set (a mismatch
// leaves the original cookie in place).
func TestCookies_Clear(t *testing.T) {
	c := Cookies{}
	rec := httptest.NewRecorder()
	c.Clear(rec)
	res := rec.Result()
	defer res.Body.Close()
	cookies := res.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	ck := cookies[0]
	if ck.Value != "" {
		t.Errorf("Clear wrote value %q, want empty", ck.Value)
	}
	if ck.MaxAge >= 0 {
		t.Errorf("MaxAge = %d, want negative (expire now)", ck.MaxAge)
	}
	if ck.Name != CookieName || ck.Path != "/" || !ck.HttpOnly || !ck.Secure || ck.SameSite != http.SameSiteLaxMode {
		t.Errorf("Clear attributes do not match Set: %+v", ck)
	}
}

// TestCookies_Read maps every "no usable cookie" shape onto the single sentinel
// §5 row 3 acts on.
func TestCookies_Read(t *testing.T) {
	c := Cookies{}
	tk, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "/t", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: tk.reveal()})
	got, err := c.Read(r)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.reveal() != tk.reveal() {
		t.Fatal("Read returned a different value than the request carried")
	}

	if _, err := c.Read(httptest.NewRequest(http.MethodGet, "/t", nil)); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Read(no cookie) error = %v, want ErrNoSession", err)
	}

	empty := httptest.NewRequest(http.MethodGet, "/t", nil)
	empty.AddCookie(&http.Cookie{Name: CookieName, Value: ""})
	if _, err := c.Read(empty); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Read(empty cookie) error = %v, want ErrNoSession", err)
	}

	wrongName := httptest.NewRequest(http.MethodGet, "/t", nil)
	wrongName.AddCookie(&http.Cookie{Name: "other", Value: tk.reveal()})
	if _, err := c.Read(wrongName); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Read(other cookie) error = %v, want ErrNoSession", err)
	}
}

// TestCookies_HeaderIsTheOnlyEgress: the raw value reaches the Set-Cookie header
// (otherwise the product would not work) and NOTHING else that renders the same
// Token can show it. This is the boundary §4.7 draws.
func TestCookies_HeaderIsTheOnlyEgress(t *testing.T) {
	tk, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}
	rec := httptest.NewRecorder()
	if err := (Cookies{}).Set(rec, tk); err != nil {
		t.Fatalf("Set: %v", err)
	}
	header := rec.Header().Get("Set-Cookie")
	if !strings.Contains(header, tk.reveal()) {
		t.Fatal("Set-Cookie does not carry the value: the session would never work")
	}
	if rendered := fmt.Sprintf("%v|%s|%#v", tk, tk, tk); strings.Contains(rendered, tk.reveal()) {
		t.Fatalf("a rendered Token leaked the value: %q", rendered)
	}
}
