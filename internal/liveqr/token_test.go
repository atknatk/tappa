package liveqr

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/qrcode"
)

// testKeys derives keys from a secret made in code from a label: obviously fake, never a value
// anyone could hold in a deployment (the public repository's rule for test keys).
func testKeys(t *testing.T, label string) *Keys {
	t.Helper()
	secret := sha256.Sum256([]byte("liveqr test secret, not a key: " + label))
	k, err := NewKeys(secret[:])
	if err != nil {
		t.Fatal(err)
	}
	return k
}

var tokenAt = time.Date(2026, 11, 19, 14, 3, 22, 0, time.UTC)

// TestToken_RoundTripsTheContract: Open returns exactly what Mint signed -- the screen, the
// location, the issue second and the lifetime -- and ExpiresAt is their sum. The encoded length is
// the contract's 79 characters.
func TestToken_RoundTripsTheContract(t *testing.T) {
	k := testKeys(t, "a")
	display, location := uuid.New(), uuid.New()
	tok, err := k.Mint(display, location, tokenAt.Add(700*time.Millisecond), TokenLifetime)
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 79 || tokenEncodeLen != 79 || tokenLen != 59 {
		t.Fatalf("token is %d characters (%d bytes); the contract says 79 (59)", len(tok), tokenLen)
	}
	c, err := k.Open(tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.DisplayID != display || c.LocationID != location || !c.IssuedAt.Equal(tokenAt) || c.Lifetime != 20*time.Second {
		t.Fatalf("claims %+v", c)
	}
	if !c.ExpiresAt().Equal(tokenAt.Add(20 * time.Second)) {
		t.Fatalf("ExpiresAt %v", c.ExpiresAt())
	}
}

// TestToken_Q39SeparatesForgeryFromAge: the two outcomes M16-21 must tell apart (Q39 (a) reject,
// (b) flag) are separable from the token alone. Every byte flipped is ErrSignature -- including
// the issue time, so an "expired-looking" forgery is a forgery, never "expired"; a token of another
// key is ErrSignature; a genuine old token OPENS (its age is the caller's comparison); anything that
// is not the shape is ErrMalformed.
func TestToken_Q39SeparatesForgeryFromAge(t *testing.T) {
	k := testKeys(t, "a")
	display, location := uuid.New(), uuid.New()
	tok, err := k.Mint(display, location, tokenAt, TokenLifetime)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(raw); i++ { // byte 0 is the version: a flip there is "not this format"
		b := append([]byte(nil), raw...)
		b[i] ^= 0x01
		if _, err := k.Open(base64.RawURLEncoding.EncodeToString(b)); !errors.Is(err, ErrSignature) {
			t.Fatalf("byte %d flipped: %v, want ErrSignature", i, err)
		}
	}
	if _, err := testKeys(t, "b").Open(tok); !errors.Is(err, ErrSignature) {
		t.Fatalf("another key: %v, want ErrSignature", err)
	}
	old, err := k.Mint(display, location, tokenAt.Add(-time.Hour), TokenLifetime)
	if err != nil {
		t.Fatal(err)
	}
	if c, err := k.Open(old); err != nil || !c.ExpiresAt().Before(tokenAt) {
		t.Fatalf("a genuine old token: %+v %v -- it must open, and its window must be in the past", c, err)
	}
	ver := append([]byte(nil), raw...)
	ver[0] = 2
	for name, s := range map[string]string{
		"empty":        "",
		"short":        tok[:78],
		"long":         tok + "A",
		"not base64":   strings.Repeat("*", 79),
		"padded":       tok[:77] + "==",
		"next version": base64.RawURLEncoding.EncodeToString(ver),
	} {
		if _, err := k.Open(s); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: %v, want ErrMalformed", name, err)
		}
	}
}

// TestToken_MintRefusesWhatCannotBeAWindow: no screen, no location, no lifetime, a lifetime past
// the field's 16 bits, the zero time -- each refused.
func TestToken_MintRefusesWhatCannotBeAWindow(t *testing.T) {
	k := testKeys(t, "a")
	d, l := uuid.New(), uuid.New()
	for name, tc := range map[string]struct {
		d, l uuid.UUID
		at   time.Time
		life time.Duration
	}{
		"no screen":     {uuid.Nil, l, tokenAt, TokenLifetime},
		"no location":   {d, uuid.Nil, tokenAt, TokenLifetime},
		"no lifetime":   {d, l, tokenAt, 0},
		"sub-second":    {d, l, tokenAt, 500 * time.Millisecond},
		"over 16 bits":  {d, l, tokenAt, 65536 * time.Second},
		"the zero time": {d, l, time.Time{}, TokenLifetime},
	} {
		if _, err := k.Mint(tc.d, tc.l, tc.at, tc.life); err == nil {
			t.Errorf("%s: minted", name)
		}
	}
	var zero *Keys
	if _, err := zero.Mint(d, l, tokenAt, TokenLifetime); err == nil {
		t.Error("nil keys minted")
	}
}

// TestToken_TheQRIsVersionSixAtLevelM: the URL the screen encodes on the production host is 102
// characters and draws as a version 6 symbol (41 modules) at level M -- measured here, so a longer
// token or host is a visible change of the drawing's size.
func TestToken_TheQRIsVersionSixAtLevelM(t *testing.T) {
	k := testKeys(t, "a")
	tok, err := k.Mint(uuid.New(), uuid.New(), tokenAt, TokenLifetime)
	if err != nil {
		t.Fatal(err)
	}
	u := TapURL("https://taptime.mt/", tok)
	if !strings.HasPrefix(u, "https://taptime.mt/q?t=") || strings.Contains(u, "%") {
		t.Fatalf("URL %q", u)
	}
	code, err := qrcode.Encode([]byte(u), qrcode.M)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("URL %d characters -> QR version %d (%d modules) at level M", len(u), code.Version(), code.Size())
	if len(u) != 102 || code.Version() != 6 {
		t.Fatalf("URL %d characters -> version %d; measured 102 -> 6", len(u), code.Version())
	}
}
