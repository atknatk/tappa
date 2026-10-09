package invite

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// TestBinding_NeverPrintsItsValue: the consent binding is a bearer credential
// paired with the code (ADR 0026), so it carries the code's redaction through
// every printing interface — including from inside a wrapping struct, the shape a
// handler writes when it logs its own state.
func TestBinding_NeverPrintsItsValue(t *testing.T) {
	const raw = "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_AbCd"
	b := ParseBinding(raw)
	wrapped := struct{ B Binding }{b}
	for _, got := range []string{
		fmt.Sprintf("%v", b), fmt.Sprintf("%+v", b), fmt.Sprintf("%#v", b), fmt.Sprintf("%s", b),
		fmt.Sprintf("%q", b), fmt.Sprintf("%+v", wrapped), fmt.Sprintf("%#v", wrapped),
	} {
		if strings.Contains(got, raw) {
			t.Errorf("binding leaked through fmt: %q", got)
		}
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", "binding", b, "wrapped", wrapped)
	if strings.Contains(buf.String(), raw) {
		t.Errorf("binding leaked through slog: %s", buf.String())
	}
}

// TestBinding_HashIsDomainSeparatedFromTheCode: the same key hashes codes and
// bindings, so the same 43 characters must not produce the same stored value as
// both — a code hash must never be accepted as a binding hash or the reverse.
func TestBinding_HashIsDomainSeparatedFromTheCode(t *testing.T) {
	c, err := newCode()
	if err != nil {
		t.Fatalf("newCode: %v", err)
	}
	ch, err := c.hash(testKeyA)
	if err != nil {
		t.Fatalf("code hash: %v", err)
	}
	bh, err := ParseBinding(c.reveal()).hash(testKeyA)
	if err != nil {
		t.Fatalf("binding hash: %v", err)
	}
	if ch == bh {
		t.Fatal("a value hashes to the same stored form as a code and as a binding")
	}
	if len(bh) != 64 {
		t.Fatalf("binding hash has %d chars, want 64 (the column's shape CHECK)", len(bh))
	}
	if _, err := ParseBinding("short").hash(testKeyA); err == nil {
		t.Fatal("a short binding hashed; it must be refused as malformed")
	}
}
