package webhook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"testing"
)

// TestWebhookSignature_KnownAnswer is K-11's signature against a vector computed OUTSIDE Go (Python's
// hmac module, 2026-10-15): the key is the guide's deliberately fake one -- the prefix and 43 'A's,
// i.e. 32 zero bytes, which redline R7d does not count as a key (padding) -- the body the ping's
// shape, then the empty body. The value is "sha256=" + lower-case hex, nothing else.
//
// MUTANTS (measured): the HMAC keyed by the decoded 32 bytes instead of the key's text -> red; upper-case
// hex -> red; the prefix dropped -> red.
func TestWebhookSignature_KnownAnswer(t *testing.T) {
	k, err := ParseSigningKey(SigningKeyPrefix + strings.Repeat("A", 43))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ body, want string }{
		{`{"event":"ping"}`, "sha256=1deefe0009103810c89395f2d078e1ba71edb003b3692a3ebca6d0866bf99935"},
		{``, "sha256=88d05ff68b9d0e7585161401b08cb679fbf1701b3e28f683a3e96e4c33653576"},
	} {
		if got := k.Sign([]byte(c.body)); got != c.want {
			t.Errorf("body %q: %s, want %s", c.body, got, c.want)
		}
	}
	if SignatureHeader != "X-Taptime-Signature" || SignaturePrefix != "sha256=" {
		t.Fatal("the header or the prefix changed: the guide (API-12) names both")
	}
}

// TestSigningKey_ShapeAndRedaction: a minted key is the prefix + 43 canonical base64url characters
// and parses back; two mints differ; a wrong prefix, length, alphabet or non-canonical tail is refused;
// and the key prints a placeholder through every fmt verb, slog and encoding/json.
func TestSigningKey_ShapeAndRedaction(t *testing.T) {
	shape := regexp.MustCompile(`^tt_whsec_[A-Za-z0-9_-]{43}$`)
	a, err := MintSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := MintSigningKey()
	if !shape.MatchString(a.RevealToTheOwnerOnce()) || a.RevealToTheOwnerOnce() == b.RevealToTheOwnerOnce() {
		t.Fatal("minted keys: wrong shape or equal")
	}
	if _, err := ParseSigningKey(a.RevealToTheOwnerOnce()); err != nil {
		t.Fatalf("a minted key does not parse: %v", err)
	}
	if string(a.SealInput()) != a.RevealToTheOwnerOnce() {
		t.Fatal("the sealed text is not the key")
	}
	for _, bad := range []string{"", "tt_live_" + strings.Repeat("A", 43), SigningKeyPrefix + strings.Repeat("A", 42),
		SigningKeyPrefix + strings.Repeat("A", 44), SigningKeyPrefix + strings.Repeat("A", 42) + "+",
		SigningKeyPrefix + strings.Repeat("A", 42) + "B" /* non-canonical last character */} {
		if _, err := ParseSigningKey(bad); err != ErrSigningKeyShape {
			t.Errorf("%q parsed", bad)
		}
	}
	raw := a.RevealToTheOwnerOnce()
	var out []string
	holder, list := struct{ K SigningKey }{a}, []SigningKey{a}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d"} {
		out = append(out, fmt.Sprintf(verb, a), fmt.Sprintf(verb, holder), fmt.Sprintf(verb, list))
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", "key", a)
	slog.New(slog.NewTextHandler(&buf, nil)).Info("x", "key", a)
	j, _ := json.Marshal(map[string]any{"k": a})
	out = append(out, buf.String(), string(j))
	for _, o := range out {
		if strings.Contains(o, raw[len(SigningKeyPrefix):len(SigningKeyPrefix)+12]) || strings.Contains(o, SigningKeyPrefix) {
			t.Fatalf("the key printed itself: %s", o)
		}
	}
}
