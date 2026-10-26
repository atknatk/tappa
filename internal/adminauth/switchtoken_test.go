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

// refSwitchToken is ADR 0031 §5.4's synchronizer written out from the ADR, independently of
// switchtoken.go: K from the RAW token under the session key's own label, the message the
// prefix and the session id.
func refSwitchToken(sessionKey []byte, raw string, session uuid.UUID) string {
	mac := func(key []byte, msg string) []byte {
		m := hmac.New(sha256.New, key)
		m.Write([]byte(msg))
		return m.Sum(nil)
	}
	k := mac(mac(sessionKey, "tappa/admin-switch/v1/key-derivation"), raw)
	return base64.RawURLEncoding.EncodeToString(mac(k, "tappa/admin-switch/v1|"+session.String()))
}

func mustSwitchTokens(t *testing.T) SwitchTokens {
	t.Helper()
	s, err := NewSwitchTokens(testKey())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestSwitchTokens_IsTheADRsMACOverTheRawToken pins the value to ADR 0031 §5.4: red if the key
// is derived from token_hash (what a database reader holds), if the prefix or the session id
// leaves the message, or if it is the form binding under another name.
func TestSwitchTokens_IsTheADRsMACOverTheRawToken(t *testing.T) {
	s := mustSwitchTokens(t)
	tok := mustToken(t)
	session := uuid.New()
	got, err := s.Mint(tok, session)
	if err != nil {
		t.Fatal(err)
	}
	if want := refSwitchToken(testKey(), tok.reveal(), session); got != want {
		t.Fatalf("Mint = %q, want the ADR's MAC %q", got, want)
	}
	if len(got) != SwitchTokenLen {
		t.Errorf("a switch token is %d characters, SwitchTokenLen says %d", len(got), SwitchTokenLen)
	}
	h, err := tok.hash(mustDerive(t))
	if err != nil {
		t.Fatal(err)
	}
	if refSwitchToken(testKey(), h, session) == got {
		t.Fatal("the switch token can be computed from token_hash, which the database holds")
	}
	// Domain separation: the form binding of the same session over the same uuid is another value.
	if b, err := mustBinder(t).Bind(tok, session); err != nil || b == got {
		t.Fatalf("the switch token equals the form binding of the same id (%v)", err)
	}
}

// TestSwitchTokens_OnlyTheSessionCanProduceIt is the synchronizer property: a token matches its
// own (raw token, session id) pair and nothing else -- not another session's cookie, not another
// session id under the same cookie (a switch's new session), not another server key, not a
// truncated, empty or one-character-off value.
func TestSwitchTokens_OnlyTheSessionCanProduceIt(t *testing.T) {
	s := mustSwitchTokens(t)
	other, err := NewSwitchTokens(bytes.Repeat([]byte{0x2c}, 32))
	if err != nil {
		t.Fatal(err)
	}
	const n = 12
	toks := make([]Token, n)
	ids := make([]uuid.UUID, n)
	for i := range toks {
		toks[i], ids[i] = mustToken(t), uuid.New()
	}
	for i := range toks {
		v, err := s.Mint(toks[i], ids[i])
		if err != nil {
			t.Fatal(err)
		}
		for k := range toks {
			for l := range ids {
				if want := k == i && l == i; s.Matches(toks[k], ids[l], v) != want {
					t.Fatalf("token of %d checked against (cookie %d, session %d): %v, want %v", i, k, l, !want, want)
				}
			}
		}
		if other.Matches(toks[i], ids[i], v) {
			t.Fatal("another server key accepts the token")
		}
		flipped := []byte(v)
		flipped[0] ^= 1
		for _, bad := range []string{"", v[:len(v)-1], v + "A", string(flipped)} {
			if s.Matches(toks[i], ids[i], bad) {
				t.Fatalf("a malformed token %q matched", bad)
			}
		}
	}
}

// TestSwitchTokens_TheZeroValueAndBadInputsFailClosed: no key, a nil session, a key of the wrong
// length and a token this package could not have issued all refuse.
func TestSwitchTokens_TheZeroValueAndBadInputsFailClosed(t *testing.T) {
	tok := mustToken(t)
	session := uuid.New()
	if _, err := (SwitchTokens{}).Mint(tok, session); err == nil {
		t.Error("the zero SwitchTokens minted a token")
	}
	v, err := mustSwitchTokens(t).Mint(tok, session)
	if err != nil {
		t.Fatal(err)
	}
	if (SwitchTokens{}).Matches(tok, session, v) {
		t.Error("the zero SwitchTokens accepted a token")
	}
	if _, err := mustSwitchTokens(t).Mint(tok, uuid.Nil); err == nil {
		t.Error("a token was minted for the nil session")
	}
	if _, err := NewSwitchTokens([]byte("short")); err == nil {
		t.Error("a short session key was accepted")
	}
	if _, err := mustSwitchTokens(t).Mint(wrap("not-a-token"), session); err == nil {
		t.Error("a token this package could not have issued was accepted")
	}
}

// TestSwitchBucket_IsThePersonsAndNamesNobody (ADR 0031 §5.4, bulgu 8): the switch's rate bucket
// is the same for every session of one identity -- whatever business or session it is in --
// different for another identity, absent for an unlinked row, and carries neither the identity
// id nor anything that renders it.
//
// MUTATION (measured): switchBucketOf keyed by its label alone (not the identity) -> the "another
// identity" check is red.
func TestSwitchBucket_IsThePersonsAndNamesNobody(t *testing.T) {
	m := &Manager{hmacKey: mustDerive(t)}
	one, two := uuid.New(), uuid.New()
	a := Resolved{SessionID: uuid.New(), TenantID: uuid.New(), Linked: true, switchBucket: m.switchBucketOf(&one)}
	b := Resolved{SessionID: uuid.New(), TenantID: uuid.New(), Linked: true, switchBucket: m.switchBucketOf(&one)}
	c := Resolved{SessionID: uuid.New(), TenantID: a.TenantID, Linked: true, switchBucket: m.switchBucketOf(&two)}
	ka, oka := a.SwitchBucket()
	kb, okb := b.SwitchBucket()
	kc, okc := c.SwitchBucket()
	if !oka || !okb || !okc || ka == "" {
		t.Fatal("a linked session has no switch bucket")
	}
	if ka != kb {
		t.Error("two sessions of one identity in two businesses have different buckets")
	}
	if ka == kc {
		t.Error("two identities share a bucket")
	}
	if _, ok := (Resolved{SessionID: uuid.New()}).SwitchBucket(); ok {
		t.Error("an unlinked row has a bucket")
	}
	if _, ok := (Resolved{switchBucket: m.switchBucketOf(nil)}).SwitchBucket(); ok {
		t.Error("a nil identity has a bucket")
	}
	for _, rendering := range []string{ka, fmt.Sprintf("%+v", a), fmt.Sprintf("%#v", a)} {
		if strings.Contains(strings.ToLower(rendering), strings.ReplaceAll(one.String(), "-", "")) ||
			strings.Contains(rendering, one.String()) {
			t.Errorf("the bucket or the Resolved names the identity: %s", rendering)
		}
	}
}
