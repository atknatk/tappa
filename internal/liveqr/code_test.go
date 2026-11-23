package liveqr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// TestPairingCode_IsSixtyFiveBitsOfCrockford: 13 symbols of Crockford's alphabet (no I, L, O, U),
// 65 bits; the panel's form is three groups, 4-4-5. Two thousand draws are all distinct and use
// every symbol -- a broken reduction (b % 32 over a biased source, a stuck byte) shows here.
//
// THE MEASUREMENT BEHIND THE CHOICE (code.go): with 100 waiting codes across every tenant and a
// botnet of 100 000 addresses at the handler's 20 failures per 15 minutes, a hit an hour is
// 8e6 · 100 / 2^65 -- computed here so the number in the comment is the number the constants give.
func TestPairingCode_IsSixtyFiveBitsOfCrockford(t *testing.T) {
	if CodeBits != 65 || len(codeAlphabet) != 32 || strings.ContainsAny(codeAlphabet, "ILOU") {
		t.Fatalf("CodeBits %d, alphabet %q", CodeBits, codeAlphabet)
	}
	shape := regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{4}-[0-9A-HJKMNP-TV-Z]{4}-[0-9A-HJKMNP-TV-Z]{5}$`)
	seen := map[string]bool{}
	symbols := map[rune]bool{}
	for range 2000 {
		c, err := NewPairingCode()
		if err != nil {
			t.Fatal(err)
		}
		shown := c.ForThePanelOnce()
		if !shape.MatchString(shown) {
			t.Fatalf("code shown as %q", shown)
		}
		if seen[shown] {
			t.Fatal("a repeated code in 2000 draws")
		}
		seen[shown] = true
		for _, r := range strings.ReplaceAll(shown, "-", "") {
			symbols[r] = true
		}
	}
	if len(symbols) != 32 {
		t.Fatalf("%d of 32 symbols drawn in 26 000", len(symbols))
	}
	perHour := 100_000.0 * 20 * 4 // addresses × failures per 15 min × 4
	p := perHour * 100 / math.Pow(2, CodeBits)
	t.Logf("guesses an hour %.0f; P(hit an hour) with 100 waiting codes = %.1e", perHour, p)
	if p > 1e-10 {
		t.Fatalf("P(hit an hour) %.1e: the code is too short for the endpoint's reach", p)
	}
}

// TestParsePairingCode_ReadsWhatAPersonTypes: case, hyphens, spaces and the look-alikes are folded
// to one canonical code, so every spelling has ONE digest; anything else is refused.
func TestParsePairingCode_ReadsWhatAPersonTypes(t *testing.T) {
	k := testKeys(t, "a")
	want, ok := ParsePairingCode("7KQ2-M9XD-4TRWZ")
	if !ok {
		t.Fatal("the canonical spelling was refused")
	}
	wantDigest, err := k.CodeDigest(want)
	if err != nil || len(wantDigest) != 64 {
		t.Fatalf("digest %q %v", wantDigest, err)
	}
	for _, typed := range []string{"7kq2m9xd4trwz", " 7KQ2 M9XD 4TRWZ ", "7kq2-m9xd-4trwz"} {
		c, ok := ParsePairingCode(typed)
		if !ok {
			t.Fatalf("%q refused", typed)
		}
		if d, _ := k.CodeDigest(c); d != wantDigest {
			t.Errorf("%q has another digest", typed)
		}
	}
	o, _ := ParsePairingCode("O0IL-0000-00000")
	if o.ForThePanelOnce() != "0011-0000-00000" {
		t.Errorf("look-alikes folded to %q", o.ForThePanelOnce())
	}
	for _, typed := range []string{"", "7KQ2-M9XD-4TRW", "7KQ2-M9XD-4TRWZZ", "7KQ2-M9XD-4TRWU", "7KQ2_M9XD_4TRWZ", strings.Repeat("A", 100), "7KQ2-M9XD-4TRWÖ"} {
		if c, ok := ParsePairingCode(typed); ok || !c.IsZero() {
			t.Errorf("%q accepted", typed)
		}
	}
	if _, err := k.CodeDigest(PairingCode{}); err == nil {
		t.Error("a zero code has a digest")
	}
}

// TestSecrets_NoPrintingPathCarriesThem: a pairing code and a screen session never come out of fmt
// (every verb, in an exported and an unexported field), slog (text and JSON) or encoding/json --
// the placeholder does. The cookie writer is the session's one egress, and it is HttpOnly, Lax
// and scoped to /screen.
func TestSecrets_NoPrintingPathCarriesThem(t *testing.T) {
	code, err := NewPairingCode()
	if err != nil {
		t.Fatal(err)
	}
	sess, err := newScreenSession()
	if err != nil {
		t.Fatal(err)
	}
	values := []string{code.reveal(), sess.reveal(), strings.ReplaceAll(code.ForThePanelOnce(), "-", "")}
	type holder struct {
		Code    PairingCode
		Session ScreenSession
		code    PairingCode
		session ScreenSession
	}
	h := holder{Code: code, Session: sess, code: code, session: sess}
	var out []string
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
		out = append(out, fmt.Sprintf(verb, h), fmt.Sprintf(verb, code), fmt.Sprintf(verb, sess), fmt.Sprintf(verb, &h))
	}
	j, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	out = append(out, string(j), fmt.Sprint(fmt.Errorf("wrapped: %v / %v", code, sess)))
	for _, asJSON := range []bool{false, true} {
		var buf bytes.Buffer
		var hd slog.Handler = slog.NewTextHandler(&buf, nil)
		if asJSON {
			hd = slog.NewJSONHandler(&buf, nil)
		}
		slog.New(hd).Info("probe", "code", code, "session", sess, "holder", h)
		out = append(out, buf.String())
	}
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, redactedCode) || !strings.Contains(joined, redactedSession) {
		t.Fatal("CONTROL: the placeholders were not printed; the probe is not reaching the values")
	}
	for _, v := range values {
		if strings.Contains(joined, v) {
			t.Fatalf("a printing path carries a secret value:\n%s", joined)
		}
	}

	rec := httptest.NewRecorder()
	if err := NewScreenCookies(true).Set(rec, sess); err != nil {
		t.Fatal(err)
	}
	ck := rec.Result().Cookies()
	if len(ck) != 1 || ck[0].Name != SessionCookieName || ck[0].Value != sess.reveal() || !ck[0].HttpOnly || !ck[0].Secure ||
		ck[0].SameSite != http.SameSiteLaxMode || ck[0].Path != "/screen" {
		t.Fatalf("cookie %+v", ck)
	}
	req := httptest.NewRequest(http.MethodGet, "/screen", nil)
	req.AddCookie(ck[0])
	k := testKeys(t, "a")
	d1, err := k.SessionDigest(sess)
	if err != nil {
		t.Fatal(err)
	}
	if d2, err := k.SessionDigest((ScreenCookies{}).Read(req)); err != nil || d2 != d1 {
		t.Fatalf("the cookie read back has another digest: %v", err)
	}
	for _, bad := range []string{"x", strings.Repeat("A", 42), strings.Repeat("*", 43)} {
		v := bad
		if _, err := k.SessionDigest(ScreenSession{v: &v}); err == nil {
			t.Errorf("session of shape %q has a digest", bad)
		}
	}
}

// TestKeys_EachUseHasItsOwnKey: the code's, the session's and the token's keys are three different
// derivations of one secret (the labels), and a different secret gives different digests.
func TestKeys_EachUseHasItsOwnKey(t *testing.T) {
	k := testKeys(t, "a")
	if string(k.token.Reveal()) == string(k.code.Reveal()) || string(k.code.Reveal()) == string(k.session.Reveal()) ||
		string(k.token.Reveal()) == string(k.session.Reveal()) {
		t.Fatal("two uses share a key")
	}
	c, _ := ParsePairingCode("7KQ2-M9XD-4TRWZ")
	a, _ := k.CodeDigest(c)
	b, _ := testKeys(t, "b").CodeDigest(c)
	if a == b {
		t.Fatal("two secrets give one digest")
	}
	if _, err := NewKeys(make([]byte, 31)); err == nil {
		t.Fatal("a 31-byte secret was accepted")
	}
}
