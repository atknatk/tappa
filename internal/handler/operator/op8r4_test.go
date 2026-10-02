package operator_test

// op8r4_test.go -- OP-8's fourth round: the enrollment page's weak-password notice (B7).

import (
	"encoding/base32"
	"encoding/base64"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestEnroll_TheWeakPasswordNoticeNamesBothLimits: operatorauth refuses a new password
// that is shorter than 14 characters, longer than 72 bytes or not UTF-8 with ONE error
// (ErrWeakPassword), so the page's notice names both limits and none of the four arms
// below is called "too short" (4th round, B7: the notice said "That password is too
// short" for all three). Arms: 13 characters; 73 one-byte characters; 37 two-byte
// characters (74 bytes, 37 characters); 20 bytes that are not UTF-8. CONTROL: a password
// the rule accepts, with a wrong code, gets the code notice and not this one.
func TestEnroll_TheWeakPasswordNoticeNamesBothLimits(t *testing.T) {
	g := newRig(t)
	ad := &addrs{}
	pending := uuid.New()
	page := g.do(req{method: http.MethodGet, host: opHost, path: "/operator/enroll?id=" + pending.String(), remote: ad.next() + ":1",
		header: map[string]string{"Sec-Fetch-Site": "same-origin"}})
	km := regexp.MustCompile(`<p class="op-key">([A-Z2-7 ]+)</p>`).FindStringSubmatch(page.Body.String())
	bm := regexp.MustCompile(`name="blob" value="([^"]+)"`).FindStringSubmatch(page.Body.String())
	if km == nil || bm == nil {
		t.Fatal("PREMISE: the enrollment page carries no key or blob")
	}
	key, err := base32.StdEncoding.DecodeString(strings.ReplaceAll(km[1], " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	token := base64.RawURLEncoding.EncodeToString(randBytes(t, 32))
	g.store.mu.Lock()
	g.store.tokens[pending] = token
	g.store.mu.Unlock()
	post := func(password, code string) (int, string) {
		w := g.post2(ad.next()+":1", "/operator/enroll", url.Values{"id": {pending.String()}, "token": {token}, "blob": {html.UnescapeString(bm[1])},
			"password": {password}, "password_again": {password}, "code": {code}})
		return w.Code, w.Body.String()
	}
	const heading, rule = "Choose another password", "Use at least 14 characters and at most 72 bytes."
	for name, pw := range map[string]string{
		"13 characters":               strings.Repeat("a", 13),
		"73 one-byte characters":      strings.Repeat("b", 73),
		"37 two-byte characters":      strings.Repeat("ż", 37),
		"20 bytes that are not UTF-8": strings.Repeat("\xff", 20),
	} {
		code, body := post(pw, totpAt(key, g.now))
		if code != http.StatusBadRequest || !strings.Contains(body, heading) || !strings.Contains(body, rule) || strings.Contains(body, "too short") {
			t.Errorf("%s: %d, notice %v, rule %v, says too short %v", name, code, strings.Contains(body, heading), strings.Contains(body, rule), strings.Contains(body, "too short"))
		}
	}
	code, body := post(strings.Repeat("c", 14), wrongCodeAt(key, g.now))
	if code != http.StatusUnauthorized || strings.Contains(body, heading) || !strings.Contains(body, "That code was not accepted") {
		t.Fatalf("CONTROL: an accepted password with a wrong code = %d, weak notice %v", code, strings.Contains(body, heading))
	}
}

// post2 is post from a given client address.
func (g *rig) post2(remote, path string, form url.Values) *httptest.ResponseRecorder {
	g.t.Helper()
	return g.do(req{method: http.MethodPost, host: opHost, path: path, form: form, origin: opOrigin, remote: remote})
}
