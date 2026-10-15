package apikey

import (
	"encoding/base64"
	"strings"
	"testing"
)

// fixedMACKey is a test-only 32-byte value: the bytes 1..32.
func fixedMACKey() []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = byte(i + 1)
	}
	return b
}

// fillerBody is 43 'A's: the canonical base64url of 32 zero bytes -- a value of the key's SHAPE
// that is obviously not a key (one distinct character; R7d does not count it).
var fillerBody = strings.Repeat("A", bodyLen)

// TestKeyedHash_KnownAnswer pins the stored value's construction: hex HMAC-SHA256 under the key,
// over "tappa/apikey/v1|" and the WHOLE key, prefix included. The two answers were computed
// outside Go (Python's hmac module, 2026-10-14). The same body under the other prefix hashes to
// something else, and a key that is not 32 bytes is refused.
func TestKeyedHash_KnownAnswer(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{LivePrefix + fillerBody, "de6e8a6727b8e687b7745833d3748ef799549e43694e41f70282d95a6a7474a1"},
		{SandboxPrefix + fillerBody, "ec23e0de72a99f1de7210aa0a62af51db43a3bb6f0a056213154e201facfaf0a"},
	} {
		got, err := keyedHash(fixedMACKey(), c.in)
		if err != nil || got != c.want {
			t.Errorf("keyedHash(%s…) = %q, %v; want %q", c.in[:12], got, err, c.want)
		}
	}
	if _, err := keyedHash(make([]byte, 31), LivePrefix+fillerBody); err == nil {
		t.Error("a 31-byte MAC key was accepted")
	}
}

// TestMint_IsThePrefixAndFortyThreeCanonicalCharacters: every minted key has the deployment's
// prefix and the canonical base64url of 32 bytes; its hint is the prefix and four characters;
// 1000 draws are distinct.
func TestMint_IsThePrefixAndFortyThreeCanonicalCharacters(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range []string{LivePrefix, SandboxPrefix} {
		for range 500 {
			tok, err := mint(p)
			if err != nil {
				t.Fatal(err)
			}
			v := tok.reveal()
			if !shape(v, p) {
				t.Fatalf("a minted key does not have the %s shape (length %d)", p, len(v))
			}
			if b, err := base64.RawURLEncoding.DecodeString(v[len(p):]); err != nil || len(b) != randomBytes {
				t.Fatalf("the body does not decode to %d bytes: %v", randomBytes, err)
			}
			if h := hint(v, p); len(h) != len(p)+hintLen || !strings.HasPrefix(v, h) {
				t.Fatalf("hint %d characters, want %d and a prefix of the key", len(h), len(p)+hintLen)
			}
			if seen[v] {
				t.Fatal("two minted keys are equal")
			}
			seen[v] = true
		}
	}
}

// TestShape_RefusesEveryOtherForm: the other build's prefix, a missing or wrong prefix, one
// character short or long, a character outside base64url, padding, and a non-canonical last
// character (its two spare bits set) are not this deployment's key.
func TestShape_RefusesEveryOtherForm(t *testing.T) {
	good := LivePrefix + fillerBody
	if !shape(good, LivePrefix) {
		t.Fatal("CONTROL: the filler key is not accepted")
	}
	bad := map[string]string{
		"the sandbox prefix":       SandboxPrefix + fillerBody,
		"no prefix":                fillerBody,
		"an upper-case prefix":     strings.ToUpper(LivePrefix) + fillerBody,
		"one short":                good[:len(good)-1],
		"one long":                 good + "A",
		"a plus sign":              LivePrefix + "+" + fillerBody[1:],
		"a slash":                  LivePrefix + "/" + fillerBody[1:],
		"padding":                  LivePrefix + fillerBody[:42] + "=",
		"a space":                  LivePrefix + " " + fillerBody[1:],
		"non-canonical last char":  LivePrefix + fillerBody[:42] + "B",
		"empty":                    "",
		"the prefix alone":         LivePrefix,
		"a bearer header":          "Bearer " + good,
		"a trailing newline":       good[:len(good)-1] + "\n",
		"a multibyte character":    LivePrefix + "é" + fillerBody[2:],
		"live prefix, test filler": "tt_live" + fillerBody,
	}
	for what, v := range bad {
		if shape(v, LivePrefix) {
			t.Errorf("%s: accepted as a live key", what)
		}
	}
	if shape(good, SandboxPrefix) {
		t.Error("the sandbox accepted a live key")
	}
}

// TestParseBearer reads the scheme in any case, one space, and a credential without whitespace.
func TestParseBearer(t *testing.T) {
	good := LivePrefix + fillerBody
	for _, h := range []string{"Bearer " + good, "bearer " + good, "BEARER " + good} {
		tok, err := ParseBearer(h)
		if err != nil || tok.reveal() != good {
			t.Errorf("%q: %v", h[:8], err)
		}
	}
	for _, h := range []string{"", "Bearer", "Bearer ", "Basic " + good, "Bearer  " + good, "Bearer " + good + " x", good, "Token " + good} {
		if _, err := ParseBearer(h); err != ErrMalformed {
			t.Errorf("%q: %v, want ErrMalformed", h, err)
		}
	}
}

// TestValidName: trimmed, 1..64 characters, no control character.
func TestValidName(t *testing.T) {
	for in, want := range map[string]string{"Payroll": "Payroll", "  KF-RMS  ": "KF-RMS", strings.Repeat("ż", 64): strings.Repeat("ż", 64)} {
		if got, err := ValidName(in); err != nil || got != want {
			t.Errorf("%q: %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "   ", strings.Repeat("a", 65), "a\nb", "a\tb", "a\x00b", "\xff"} {
		if _, err := ValidName(in); err != ErrName {
			t.Errorf("%q: %v, want ErrName", in, err)
		}
	}
}

// TestPrincipal_AttributesAnAPIActToTheKey is K-23: an audit row an API request writes carries
// actor_id = the key's id and detail.via = "api".
func TestPrincipal_AttributesAnAPIActToTheKey(t *testing.T) {
	p := Principal{KeyID: [16]byte{1, 2, 3}}
	if a := p.Actor(); a == nil || *a != p.KeyID {
		t.Fatalf("actor %v, want the key's id", a)
	}
	if p.Attribution().Via != "api" || Via != "api" {
		t.Fatalf("via %q, want api", p.Attribution().Via)
	}
}
