package session

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// token_test.go -- the §4.7 proof for the value itself: it is unguessable, it is
// stored only as a keyed hash, and it cannot be printed. These assertions are
// MUTATION TESTS: drop a redaction method, hash the value with a bare digest, or
// interpolate it into an error, and one of them turns red.

// fakeHMACKey is a deterministic 32-byte key for tests ONLY. It is not a
// production key, is not read from the environment, and never leaves this
// package (agent-brief madde 2: verification without showing real secrets).
var fakeHMACKey = bytes.Repeat([]byte{0xA5}, hmacKeyLen)

var fakeHMACKey2 = bytes.Repeat([]byte{0x5A}, hmacKeyLen)

// TestNewToken_ShapeAndUniqueness: >= 32 bytes of crypto randomness, encoded to
// a fixed-length URL-safe string, never repeating.
func TestNewToken_ShapeAndUniqueness(t *testing.T) {
	const n = 512
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		tk, err := newToken()
		if err != nil {
			t.Fatalf("newToken: %v", err)
		}
		if len(tk.reveal()) != tokenLen {
			t.Fatalf("length = %d, want %d", len(tk.reveal()), tokenLen)
		}
		raw, err := base64.RawURLEncoding.DecodeString(tk.reveal())
		if err != nil {
			t.Fatalf("value is not base64url: %v", err)
		}
		if len(raw) != tokenBytes {
			t.Fatalf("entropy = %d bytes, want %d (card: >= 32)", len(raw), tokenBytes)
		}
		if tokenBytes < 32 {
			t.Fatalf("tokenBytes = %d, card requires at least 32", tokenBytes)
		}
		if _, dup := seen[tk.reveal()]; dup {
			t.Fatalf("duplicate value after %d draws -- randomness is broken", i)
		}
		seen[tk.reveal()] = struct{}{}
	}
}

// TestToken_RedactedOnEveryPrintPath covers the paths where fmt CAN reach the
// redacting methods: a bare Token, a pointer, an EXPORTED struct field.
//
// SCOPE WARNING, learned the hard way. An earlier version of this test claimed
// to prove "no accidental egress" while only ever wrapping the Token in an
// exported field — and an independent audit then produced a leak through an
// UNEXPORTED field, which fmt renders by reflection without consulting any
// method. That case is covered by TestToken_NoLeakThroughUnexportedField_InPackage
// below and, more importantly, from an external test package in
// leak_external_test.go, which is the position a real caller occupies. Do not
// re-broaden this test's claim to cover what it does not exercise.
func TestToken_RedactedOnEveryPrintPath(t *testing.T) {
	tk, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}
	raw := tk.reveal()

	type wrapper struct {
		Note string
		Tk   Token
	}
	w := wrapper{Note: "issued", Tk: tk}

	var slogText bytes.Buffer
	slog.New(slog.NewTextHandler(&slogText, nil)).Info("issued", "sess", tk)

	var slogJSON bytes.Buffer
	slog.New(slog.NewJSONHandler(&slogJSON, nil)).Info("issued", "sess", tk)

	jsonBytes, err := json.Marshal(struct {
		Tk Token `json:"tk"`
	}{tk})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	renders := map[string]string{
		"%v":            fmt.Sprintf("%v", tk),
		"%s":            fmt.Sprintf("%s", tk),
		"%q":            fmt.Sprintf("%q", tk),
		"%x":            fmt.Sprintf("%x", tk),
		"%X":            fmt.Sprintf("%X", tk),
		"%d":            fmt.Sprintf("%d", tk),
		"%#v":           fmt.Sprintf("%#v", tk),
		"%+v struct":    fmt.Sprintf("%+v", w),
		"%v struct":     fmt.Sprintf("%v", w),
		"%#v struct":    fmt.Sprintf("%#v", w),
		"%v pointer":    fmt.Sprintf("%v", &w),
		"String()":      tk.String(),
		"GoString()":    tk.GoString(),
		"slog text":     slogText.String(),
		"slog json":     slogJSON.String(),
		"json.Marshal":  string(jsonBytes),
		"slog LogValue": tk.LogValue().String(),
	}

	for name, got := range renders {
		if strings.Contains(got, raw) {
			t.Errorf("%s leaks the raw value: %q", name, got)
		}
		// Also catch a hex-encoded leak (the %x form of the underlying bytes).
		if h := hex.EncodeToString([]byte(raw)); strings.Contains(got, h) || strings.Contains(got, strings.ToUpper(h)) {
			t.Errorf("%s leaks a hex-encoded value: %q", name, got)
		}
		// A render that does not mention the placeholder means a redaction hook
		// was bypassed -- the test would otherwise pass vacuously on an empty
		// string.
		if !strings.Contains(got, redacted) {
			t.Errorf("%s = %q, want it to contain %q", name, got, redacted)
		}
	}

	// A truncated prefix is still a leak of entropy; assert no long run of the
	// value survives anywhere.
	for name, got := range renders {
		if len(raw) >= 12 && strings.Contains(got, raw[:12]) {
			t.Errorf("%s leaks a 12-char prefix of the value: %q", name, got)
		}
	}
}

// TestToken_NoLeakThroughUnexportedField_InPackage is the in-package half of the
// audit regression. fmt skips Formatter/Stringer for a value it cannot hand to an
// interface, and a value read out of an UNEXPORTED field is exactly that
// (reflect.Value.CanInterface() == false), so these renders go through plain
// reflection. They must show the pointer, never the string.
//
// The renders here deliberately do NOT assert the placeholder: no method runs on
// this path, so the placeholder cannot appear. The control is the surrounding
// struct's own field, which proves the render happened.
func TestToken_NoLeakThroughUnexportedField_InPackage(t *testing.T) {
	tk, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}
	raw := tk.reveal()

	type handlerState struct {
		employee string
		tok      Token
	}
	st := handlerState{employee: "maria", tok: tk}

	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("activation", "state", st)

	renders := map[string]string{
		"%v":      fmt.Sprintf("%v", st),
		"%+v":     fmt.Sprintf("%+v", st),
		"%#v":     fmt.Sprintf("%#v", st),
		"pointer": fmt.Sprintf("%+v", &st),
		"slice":   fmt.Sprintf("%+v", []handlerState{st}),
		"error":   fmt.Errorf("activation failed: %+v", st).Error(),
		"slog":    buf.String(),
	}
	for name, got := range renders {
		if strings.Contains(got, raw) {
			t.Errorf("%s leaks the raw value through an unexported field: %q", name, got)
		}
		if len(raw) >= 12 && strings.Contains(got, raw[:12]) {
			t.Errorf("%s leaks a 12-char prefix through an unexported field: %q", name, got)
		}
		if !strings.Contains(got, "maria") {
			t.Errorf("%s = %q, want it to mention the surrounding struct (vacuous otherwise)", name, got)
		}
	}
}

// TestToken_ComparisonIsIdentityNotValue pins the documented consequence of the
// pointer indirection, so the type comment cannot drift away from the behaviour
// again. Two Tokens over the SAME string are distinct; a Token equals itself.
//
// This exists because the previous comment claimed value semantics after the
// indirection had already made them identity semantics. The behaviour is
// fail-closed (a false non-match, never a false match) and nothing in this
// package compares Tokens — the database compares token_hash — but it is a trap
// worth failing loudly on if anyone relies on the old semantics.
func TestToken_ComparisonIsIdentityNotValue(t *testing.T) {
	// A well-formed (fake) value, so the hash comparison at the end is reachable:
	// hash applies the shape gate first, and a short literal would be rejected
	// before it could prove anything.
	same := strings.Repeat("A", tokenLen)
	a, b := wrap(same), wrap(same)

	if a.reveal() != b.reveal() {
		t.Fatal("the two Tokens do not carry the same value: the test is set up wrong")
	}
	if a == b {
		t.Fatal("== compared by VALUE; the type comment documents identity semantics")
	}
	// A COPY of a Token still equals the original: copying the struct copies the
	// pointer, so identity survives assignment and passing by value. (Written as
	// a copy rather than `a != a`, which staticcheck rightly rejects as a
	// tautology — and which would not test the property that matters here.)
	aCopy := a
	if aCopy != a {
		t.Fatal("a copied Token no longer equals the original: passing a Token by value would break identity")
	}

	m := map[Token]int{a: 1, b: 2}
	if len(m) != 2 {
		t.Fatalf("map keyed on two Tokens over the same value has %d entries, want 2 (identity keys)", len(m))
	}

	// The value-level answer callers actually need comes from the hash, which is
	// value-based and is what the database matches on.
	ha, err := a.hash(fakeHMACKey)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	hb, err := b.hash(fakeHMACKey)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if ha != hb {
		t.Fatal("equal values hashed differently: the DB lookup would not find the session")
	}
}

// TestToken_ZeroValueIsSafe_InPackage: the pointer indirection must not turn the
// zero Token into a nil dereference on any path this package owns.
func TestToken_ZeroValueIsSafe_InPackage(t *testing.T) {
	var zero Token

	if got := zero.reveal(); got != "" {
		t.Fatalf("zero.reveal() = %q, want empty", got)
	}
	if _, err := zero.hash(fakeHMACKey); !errors.Is(err, errMalformed) {
		t.Fatalf("zero.hash error = %v, want errMalformed", err)
	}
	for name, got := range map[string]string{
		"%v":         fmt.Sprintf("%v", zero),
		"%#v":        fmt.Sprintf("%#v", zero),
		"%x":         fmt.Sprintf("%x", zero),
		"String()":   zero.String(),
		"GoString()": zero.GoString(),
	} {
		if got != redacted {
			t.Errorf("%s on the zero Token = %q, want %q", name, got, redacted)
		}
	}
	if txt, err := zero.MarshalText(); err != nil || string(txt) != redacted {
		t.Fatalf("zero.MarshalText() = (%q, %v), want (%q, nil)", txt, err, redacted)
	}
}

// TestTokenHash_KeyedDeterministicAndOneWay: the stored column is an HMAC under
// the configured key -- deterministic for the same input, different under a
// different key (so a stolen database cannot be replayed against another
// deployment), and it never contains the value it came from.
func TestTokenHash_KeyedDeterministicAndOneWay(t *testing.T) {
	tk, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}

	h1, err := tk.hash(fakeHMACKey)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	h2, err := tk.hash(fakeHMACKey)
	if err != nil {
		t.Fatalf("hash (repeat): %v", err)
	}
	if h1 != h2 {
		t.Fatalf("hash is not deterministic: %q vs %q", h1, h2)
	}

	hOther, err := tk.hash(fakeHMACKey2)
	if err != nil {
		t.Fatalf("hash (other key): %v", err)
	}
	if hOther == h1 {
		t.Fatal("hash does not depend on the key -- it is a bare digest, not an HMAC")
	}

	if len(h1) != 2*32 {
		t.Fatalf("hash length = %d, want %d hex chars for SHA-256", len(h1), 2*32)
	}
	if _, err := hex.DecodeString(h1); err != nil {
		t.Fatalf("hash is not hex: %v", err)
	}
	if strings.Contains(h1, tk.reveal()) {
		t.Fatal("hash contains the raw value")
	}
	if h1 == tk.reveal() {
		t.Fatal("hash equals the raw value -- nothing was hashed")
	}

	// Two different values must not collide.
	other, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}
	hOther2, err := other.hash(fakeHMACKey)
	if err != nil {
		t.Fatalf("hash (other value): %v", err)
	}
	if hOther2 == h1 {
		t.Fatal("two distinct values hashed to the same column value")
	}
}

// TestTokenHash_RejectsMalformed: the shape gate runs before any hashing, so a
// junk cookie costs nothing and never reaches the database.
func TestTokenHash_RejectsMalformed(t *testing.T) {
	good, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}

	tests := []struct {
		name string
		val  string
	}{
		{"empty", ""},
		{"zero value", Token{}.reveal()},
		{"too short", good.reveal()[:tokenLen-1]},
		{"too long", good.reveal() + "A"},
		{"not base64url", strings.Repeat("*", tokenLen)},
		{"padded base64", strings.Repeat("A", tokenLen-1) + "="},
		{"huge", strings.Repeat("A", 1<<16)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := (wrap(tc.val)).hash(fakeHMACKey); !errors.Is(err, errMalformed) {
				t.Fatalf("hash(%s) error = %v, want errMalformed", tc.name, err)
			}
		})
	}

	// A wrong-sized key is a wiring error, not a malformed cookie: it must be a
	// distinct, loud failure so it can never be mistaken for "no session".
	for _, n := range []int{0, 16, 31, 33, 64} {
		_, err := good.hash(make([]byte, n))
		if err == nil {
			t.Fatalf("hash accepted a %d-byte key", n)
		}
		if errors.Is(err, errMalformed) {
			t.Fatalf("a %d-byte key was reported as a malformed cookie", n)
		}
	}
}

// TestTokenHash_ErrorsDoNotLeak: no error message from any hashing path may
// carry the value or the key (§4.7).
func TestTokenHash_ErrorsDoNotLeak(t *testing.T) {
	good, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}
	raw := good.reveal()

	var errs []error
	push := func(_ string, e error) { errs = append(errs, e) }
	push(good.hash(make([]byte, 3)))                              // bad key length
	push(good.hash(nil))                                          // no key
	push((wrap(raw[:10])).hash(fakeHMACKey))                      // short value
	push((wrap(raw + "zz")).hash(fakeHMACKey))                    // long value
	push((wrap(strings.Repeat("!", tokenLen))).hash(fakeHMACKey)) // bad charset

	for i, e := range errs {
		if e == nil {
			t.Fatalf("error path %d unexpectedly succeeded", i)
		}
		msg := e.Error()
		if strings.Contains(msg, raw) {
			t.Fatalf("error %d leaks the raw value: %q", i, msg)
		}
		if len(raw) >= 10 && strings.Contains(msg, raw[:10]) {
			t.Fatalf("error %d leaks a prefix of the value: %q", i, msg)
		}
		if strings.Contains(msg, string(fakeHMACKey)) || strings.Contains(msg, hex.EncodeToString(fakeHMACKey)) {
			t.Fatalf("error %d leaks the key: %q", i, msg)
		}
	}
}
