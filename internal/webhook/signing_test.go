package webhook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestWebhookSignature_KnownAnswer is K-11's signature against the INTEGRATION GUIDE's known-answer
// vector -- the vector's one source (M11 API-12): docs/api/integration-guide.md's block after the
// "<!-- vector: webhook-signature -->" marker (the Turkish guide carries the same block byte for byte,
// internal/handler/api's TestGuide_TheTwoLanguagesAreOneDocument). The values were computed OUTSIDE Go:
// the ping body and the empty body with Python's hmac module (2026-10-15), the punch body -- a real
// delivery's bytes -- with Python's hmac and openssl (2026-10-16). The key is the guide's
// deliberately fake one: the prefix and 43 'A's, i.e. 32 zero bytes, which redline R7d does not
// count as a key (padding). The value is "sha256=" + lower-case hex, nothing else.
//
// MUTANTS (measured): the HMAC keyed by the decoded 32 bytes instead of the key's text -> red; upper-case
// hex -> red; the prefix dropped -> red; one hex digit of a signature in the guide edited -> red.
func TestWebhookSignature_KnownAnswer(t *testing.T) {
	vs := guideSignatureVectors(t)
	if len(vs) < 3 {
		t.Fatalf("anti-vacuity: the guide's vector block has %d known answers", len(vs))
	}
	for _, v := range vs {
		if v.key != SigningKeyPrefix+strings.Repeat("A", 43) {
			t.Fatalf("the guide's vector key is not the deliberately fake one")
		}
		k, err := ParseSigningKey(v.key)
		if err != nil {
			t.Fatal(err)
		}
		if got := k.Sign([]byte(v.body)); got != v.signature {
			t.Errorf("body %.40q: %s, the guide says %s", v.body, got, v.signature)
		}
	}
	if SignatureHeader != "X-Taptime-Signature" || SignaturePrefix != "sha256=" {
		t.Fatal("the header or the prefix changed: the guide (API-12) names both")
	}
}

type signatureVector struct{ key, body, signature string }

// guideSignatureVectors reads the guide's vector block: a key line, then body and signature lines in
// pairs ("(empty)" is the empty body).
func guideSignatureVectors(t *testing.T) []signatureVector {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "api", "integration-guide.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(b), "<!-- vector: webhook-signature -->\n```text\n")
	if !ok {
		t.Fatal("the guide has no signature vector block")
	}
	block, _, ok := strings.Cut(rest, "\n```")
	if !ok {
		t.Fatal("the guide's vector block is not closed")
	}
	var out []signatureVector
	key := ""
	for _, line := range strings.Split(block, "\n") {
		k, v, ok := strings.Cut(line, ": ")
		switch {
		case !ok:
			t.Fatalf("a vector line without a colon: %q", line)
		case k == "key":
			key = v
		case k == "body":
			if v == "(empty)" {
				v = ""
			}
			out = append(out, signatureVector{key: key, body: v})
		case k == "signature" && len(out) > 0:
			out[len(out)-1].signature = v
		default:
			t.Fatalf("a vector line %q", line)
		}
	}
	return out
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
