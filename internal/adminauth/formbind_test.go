package adminauth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// refFormBinding is ADR 0031 §5.5's binding written out from the ADR, independently of
// formbind.go: K is derived from the RAW token (under the session key's own label), the
// message is the prefix and the tenant id.
func refFormBinding(sessionKey []byte, raw string, tenant uuid.UUID) string {
	mac := func(key []byte, msg string) []byte {
		m := hmac.New(sha256.New, key)
		m.Write([]byte(msg))
		return m.Sum(nil)
	}
	k := mac(mac(sessionKey, "tappa/admin-form-tenant/v1/key-derivation"), raw)
	return base64.RawURLEncoding.EncodeToString(mac(k, "tappa/admin-form-tenant/v1|"+tenant.String()))
}

func mustBinder(t *testing.T) FormBinder {
	t.Helper()
	b, err := NewFormBinder(testKey())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustToken(t *testing.T) Token {
	t.Helper()
	tok, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// TestFormBinder_IsTheADRsMACOverTheRawToken pins the value to the ADR's definition. It is
// what turns red if the key is derived from token_hash (the value a database reader holds)
// instead of the raw cookie value, or if the prefix or the tenant leaves the message.
func TestFormBinder_IsTheADRsMACOverTheRawToken(t *testing.T) {
	b := mustBinder(t)
	tok := mustToken(t)
	tenant := uuid.New()
	got, err := b.Bind(tok, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if want := refFormBinding(testKey(), tok.reveal(), tenant); got != want {
		t.Fatalf("Bind = %q, want the ADR's MAC %q", got, want)
	}
	if len(got) != FormBindingLen {
		t.Errorf("a binding is %d characters, FormBindingLen says %d", len(got), FormBindingLen)
	}
	// What a database reader could compute: the same chain over token_hash. It must not
	// be the binding.
	h, err := tok.hash(mustDerive(t))
	if err != nil {
		t.Fatal(err)
	}
	if refFormBinding(testKey(), h, tenant) == got {
		t.Fatal("the binding can be computed from token_hash, which the database holds")
	}
}

func mustDerive(t *testing.T) []byte {
	t.Helper()
	k, err := deriveKey(testKey())
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// TestFormBinder_ASynchronizerOnlyTheSessionCanProduce is the synchronizer property: for
// many sessions and tenants, a binding matches exactly its own (session, tenant) pair --
// never another session's token, never another tenant, never another server key.
func TestFormBinder_ASynchronizerOnlyTheSessionCanProduce(t *testing.T) {
	b := mustBinder(t)
	other, err := NewFormBinder(bytes.Repeat([]byte{0x2b}, 32))
	if err != nil {
		t.Fatal(err)
	}
	const sessions, tenants = 16, 4
	toks := make([]Token, sessions)
	for i := range toks {
		toks[i] = mustToken(t)
	}
	ids := make([]uuid.UUID, tenants)
	for i := range ids {
		ids[i] = uuid.New()
	}
	seen := map[string]bool{}
	for i, tok := range toks {
		for j, tenant := range ids {
			v, err := b.Bind(tok, tenant)
			if err != nil {
				t.Fatal(err)
			}
			if seen[v] {
				t.Fatalf("session %d, tenant %d: a binding repeated", i, j)
			}
			seen[v] = true
			for k, tok2 := range toks {
				for l, tenant2 := range ids {
					if want := k == i && l == j; b.Matches(tok2, tenant2, v) != want {
						t.Fatalf("the binding of (session %d, tenant %d) checked against (session %d, tenant %d): match %v, want %v",
							i, j, k, l, !want, want)
					}
				}
			}
			if other.Matches(tok, tenant, v) {
				t.Fatalf("a binding made under one server key matches under another")
			}
		}
	}
}

// TestFormBinder_RefusesWhatIsNotABinding: the zero binder, the zero token, a malformed
// token, the nil tenant, and values of the wrong shape all fail closed.
func TestFormBinder_RefusesWhatIsNotABinding(t *testing.T) {
	b := mustBinder(t)
	tok := mustToken(t)
	tenant := uuid.New()
	good, err := b.Bind(tok, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (FormBinder{}).Bind(tok, tenant); err == nil {
		t.Error("the zero binder produced a binding")
	}
	if (FormBinder{}).Matches(tok, tenant, good) {
		t.Error("the zero binder accepted a binding")
	}
	if _, err := b.Bind(Token{}, tenant); err == nil {
		t.Error("the zero token has a binding")
	}
	if _, err := b.Bind(wrap(strings.Repeat("!", tokenLen)), tenant); err == nil {
		t.Error("a token of the wrong charset has a binding")
	}
	if _, err := b.Bind(tok, uuid.Nil); err == nil {
		t.Error("the nil tenant has a binding")
	}
	for _, v := range []string{"", good[:FormBindingLen-1], good + "A", strings.ToUpper(good), " " + good[1:]} {
		if v != good && b.Matches(tok, tenant, v) {
			t.Errorf("Matches accepted %q", v)
		}
	}
	if !b.Matches(tok, tenant, good) {
		t.Fatal("CONTROL: the binding does not match itself, so the refusals above measure nothing")
	}
	if _, err := NewFormBinder(make([]byte, 31)); err == nil {
		t.Error("a 31-byte session key built a binder")
	}
}

// TestFormBinder_NeverPrintsTheToken: the binder's errors carry no token bytes.
func TestFormBinder_NeverPrintsTheToken(t *testing.T) {
	b := mustBinder(t)
	bad := wrap(strings.Repeat("Z", tokenLen-1) + "!")
	_, err := b.Bind(bad, uuid.New())
	if err == nil {
		t.Fatal("CONTROL: a malformed token was accepted")
	}
	if strings.Contains(fmt.Sprint(err), "ZZZZ") {
		t.Errorf("the error quotes the token: %v", err)
	}
}
