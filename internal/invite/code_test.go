package invite

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// testKeyA / testKeyB are OBVIOUSLY FAKE 32-byte keys (agent-brief madde 2: no
// real secret is ever written to a file in this repo). They differ in one byte so
// a test can tell "the key mattered" from "the label mattered".
var (
	testKeyA = []byte("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	testKeyB = []byte("BAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
)

// codeHashShape is migration 00009's CHECK, verbatim. Keeping the pattern here
// too means a change to the encoding fails in Go before it fails as a 23514 at
// runtime.
var codeHashShape = regexp.MustCompile(`^[0-9a-f]{64}$`)

// TestNewCode_ShapeAndEntropy pins the acceptance criterion the M5-02 card says
// nothing mechanical can enforce: the code must be >= 128 bits, because migration
// 00009 carries no lockout state and is only sound under that assumption.
//
// It measures the two things a test CAN measure — the declared size and the
// encoded length that follows from it — and says plainly what it cannot: that
// crypto/rand is a good source. Swapping the generator for something short would
// break this test at codeBytes, which is exactly where the criterion lives.
func TestNewCode_ShapeAndEntropy(t *testing.T) {
	if codeBytes < 16 {
		t.Fatalf("codeBytes = %d: the M5-02 acceptance criterion requires >= 16 bytes (128 bits) OR a lockout migration that does not exist", codeBytes)
	}
	c, err := newCode()
	if err != nil {
		t.Fatalf("newCode: %v", err)
	}
	raw := c.reveal()
	if len(raw) != codeLen {
		t.Fatalf("code length = %d, want %d", len(raw), codeLen)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("code is not base64url: %v", err)
	}
	if len(decoded) != codeBytes {
		t.Fatalf("decoded entropy = %d bytes, want %d", len(decoded), codeBytes)
	}
	// Two codes in a row must differ. This does not prove randomness; it catches
	// the constant/counter generator that would pass everything above.
	other, err := newCode()
	if err != nil {
		t.Fatalf("newCode: %v", err)
	}
	if other.reveal() == raw {
		t.Fatal("two consecutive codes are identical")
	}
}

// TestHash_ShapeMatchesSchemaCheck: the stored value must satisfy 00009's
// CHECK, or every insert fails with 23514.
func TestHash_ShapeMatchesSchemaCheck(t *testing.T) {
	c, err := newCode()
	if err != nil {
		t.Fatalf("newCode: %v", err)
	}
	h, err := c.hash(testKeyA)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !codeHashShape.MatchString(h) {
		t.Fatalf("hash %q does not match employee_invites_code_hash_shape", h)
	}
	if strings.ToLower(h) != h {
		t.Fatal("hash must be lowercase hex: the CHECK rejects uppercase")
	}
}

// TestHash_DomainSeparation is the measurement behind the design decision.
//
// THE CLAIM: an invite MAC and a session MAC over the SAME value are different,
// and they stay different even if the two keys are identical.
//
// HOW IT IS MEASURED: internal/session computes hex(HMAC-SHA256(key, value)) —
// its token.go hash() does exactly that, with no prefix. This test recomputes
// that construction inline (it cannot call the unexported method from another
// package) and asserts the invite hash differs from it under the SAME key. That
// isolates the LABEL's contribution: same key, same value, different output.
//
// The second half isolates the KEY's contribution the other way round.
func TestHash_DomainSeparation(t *testing.T) {
	c, err := newCode()
	if err != nil {
		t.Fatalf("newCode: %v", err)
	}
	raw := c.reveal()

	// The construction internal/session/token.go uses, replicated here.
	sessionStyle := func(key []byte, v string) string {
		m := hmac.New(sha256.New, key)
		_, _ = m.Write([]byte(v))
		return hex.EncodeToString(m.Sum(nil))
	}

	inviteHash, err := c.hash(testKeyA)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if bare := sessionStyle(testKeyA, raw); inviteHash == bare {
		t.Fatal("invite hash equals the bare session-style HMAC: the domain label is not applied, so one credential type could substitute for the other")
	}

	// Same value, different key -> different hash. (The label alone would not
	// give this.)
	underB, err := c.hash(testKeyB)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if underB == inviteHash {
		t.Fatal("the key does not affect the hash")
	}

	// Determinism: the same code under the same key must resolve to the same row.
	again, err := ParseCode(raw).hash(testKeyA)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if again != inviteHash {
		t.Fatal("hashing is not deterministic; lookups would fail at random")
	}
}

// TestHash_RejectsMalformedBeforeAnyWork: the shape gate runs first, so junk
// never reaches the MAC or the database, and the caller gets one classified
// error rather than a message quoting the value.
func TestHash_RejectsMalformedBeforeAnyWork(t *testing.T) {
	cases := []struct {
		name string
		in   Code
	}{
		{"zero value", Code{}},
		{"empty", ParseCode("")},
		{"too short", ParseCode(strings.Repeat("a", codeLen-1))},
		{"too long", ParseCode(strings.Repeat("a", codeLen+1))},
		{"right length, wrong charset", ParseCode(strings.Repeat("!", codeLen))},
		{"standard base64 padding", ParseCode(strings.Repeat("a", codeLen-1) + "=")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, err := tc.in.hash(testKeyA)
			if !errors.Is(err, errMalformed) {
				t.Fatalf("err = %v, want errMalformed", err)
			}
			if h != "" {
				t.Fatalf("a rejected code must produce no hash, got %q", h)
			}
			if err.Error() != errMalformed.Error() {
				t.Fatal("the error text must not vary with the input: it could quote the value")
			}
		})
	}
}

// TestHash_RejectsWrongKeySize: a short or empty key still produces
// plausible-looking hex, so the failure would be invisible. It must be an error.
func TestHash_RejectsWrongKeySize(t *testing.T) {
	c, err := newCode()
	if err != nil {
		t.Fatalf("newCode: %v", err)
	}
	for _, key := range [][]byte{nil, {}, []byte("short"), append(testKeyA, 'x')} {
		if _, err := c.hash(key); err == nil {
			t.Fatalf("hash accepted a %d-byte key", len(key))
		}
	}
}

// TestCode_RedactsOnEveryVerb covers mechanism 1 from inside the package. The
// unexported-field case (mechanism 2) is measured from OUTSIDE, in
// leak_external_test.go, because that is where the equivalent session.Token bug
// actually appeared.
func TestCode_RedactsOnEveryVerb(t *testing.T) {
	c := ParseCode("SECRETsecretSECRETsecretSECRETsecretSECRET12")
	raw := c.reveal()

	for _, format := range []string{"%v", "%s", "%q", "%x", "%X", "%#v", "%+v", "%d", "%T"} {
		got := fmt.Sprintf(format, c)
		if strings.Contains(got, raw) {
			t.Fatalf("%s leaked the code", format)
		}
	}
	if s := c.String(); strings.Contains(s, raw) || s != redacted {
		t.Fatalf("String() = %q", s)
	}
	if s := c.GoString(); s != redacted {
		t.Fatalf("GoString() = %q", s)
	}
	if v := c.LogValue().String(); v != redacted {
		t.Fatalf("LogValue() = %q", v)
	}
	b, err := c.MarshalText()
	if err != nil || string(b) != redacted {
		t.Fatalf("MarshalText() = %q, %v", b, err)
	}
	// The placeholder must not carry a prefix or the length of the value.
	if strings.Contains(redacted, raw[:4]) || strings.Contains(redacted, "43") {
		t.Fatal("the placeholder reveals something about the value")
	}
}
