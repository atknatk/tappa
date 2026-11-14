package appdevice

// read_test.go -- the signed read (read.go) and the activation's proof of possession
// (activation.go), without a database: the fake resolver counts transactions, so "a read writes
// nothing" is measured.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TestVerifyRead_Table: a valid read verifies and opens NO transaction; the window is the
// server's clock both ways; every refusal after the shape prints the shared text; revoked and
// unknown are decided before the signature.
func TestVerifyRead_Table(t *testing.T) {
	key, other := testKey(t), testKey(t)
	device := uuid.New()
	now := time.Unix(1794700800, 0)
	body := []byte(`{"sun":"tag=04A1B2C3D4E5F6&ctr=000011&cmac=0000000000000000"}`)
	mk := func(ts int64, k bool) SignedRead {
		signer := key
		if !k {
			signer = other
		}
		text, err := CanonicalReadText(http.MethodPost, "/app/v1/tap/start", body, strconv.FormatInt(ts, 10), device.String())
		if err != nil {
			t.Fatal(err)
		}
		r, err := NewSignedRead(http.MethodPost, "/app/v1/tap/start", body, device.String(), strconv.FormatInt(ts, 10), signText(t, signer, text))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	revoked := now
	tests := []struct {
		name    string
		req     SignedRead
		dev     func(*fakeDB)
		want    error
		wantDev bool
	}{
		{name: "valid", req: mk(now.Unix(), true), wantDev: true},
		{name: "valid at the window's past edge", req: mk(now.Add(-ReadWindow).Unix(), true), wantDev: true},
		{name: "valid at the window's future edge", req: mk(now.Add(ReadWindow).Unix(), true), wantDev: true},
		{name: "stale", req: mk(now.Add(-ReadWindow-time.Second).Unix(), true), want: ErrStaleTimestamp},
		{name: "from the future", req: mk(now.Add(ReadWindow+time.Second).Unix(), true), want: ErrStaleTimestamp},
		{name: "wrong key", req: mk(now.Unix(), false), want: ErrBadSignature},
		{name: "revoked, valid signature", req: mk(now.Unix(), true), dev: func(f *fakeDB) { f.dev.RevokedAt = &revoked }, want: ErrRevokedDevice},
		{name: "revoked, junk signature", req: mk(now.Unix(), false), dev: func(f *fakeDB) { f.dev.RevokedAt = &revoked }, want: ErrRevokedDevice},
		{name: "unknown", req: mk(now.Unix(), true), dev: func(f *fakeDB) { f.resolveErr = pgx.ErrNoRows }, want: ErrUnknownDevice},
		{name: "database down", req: mk(now.Unix(), true), dev: func(f *fakeDB) { f.resolveErr = errors.New("conn refused") }},
		{name: "zero request", req: SignedRead{}, want: ErrMalformed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeDB{}
			f.dev.TenantID, f.dev.EmployeeID, f.dev.PublicKey = uuid.New(), uuid.New(), rawPublic(t, key)
			if tc.dev != nil {
				tc.dev(f)
			}
			v, err := New(f)
			if err != nil {
				t.Fatal(err)
			}
			v.now = func() time.Time { return now }
			dev, err := v.VerifyRead(context.Background(), tc.req)
			switch {
			case tc.wantDev:
				if err != nil || dev.ID != device || dev.TenantID != f.dev.TenantID {
					t.Fatalf("VerifyRead = %+v, %v; want the device", dev, err)
				}
			case tc.want != nil:
				if !errors.Is(err, tc.want) || dev != (Device{}) {
					t.Fatalf("VerifyRead = %+v, %v; want %v and no device", dev, err, tc.want)
				}
				if tc.want != ErrMalformed && err.Error() != notAuthenticated {
					t.Fatalf("refusal text %q, want the shared one", err.Error())
				}
				if StatusOf(err) != http.StatusUnauthorized {
					t.Fatalf("StatusOf = %d", StatusOf(err))
				}
			default:
				if err == nil || StatusOf(err) != http.StatusInternalServerError {
					t.Fatalf("a database failure = %v (status %d), want 500", err, StatusOf(err))
				}
			}
			if f.txCalls != 0 {
				t.Fatalf("a read opened %d transactions; it writes nothing", f.txCalls)
			}
		})
	}
}

// TestReadFromHTTP: headers once each, no query, no nonce header beside the timestamp, the
// timestamp in its one spelling.
func TestReadFromHTTP(t *testing.T) {
	key := testKey(t)
	device := uuid.New()
	ts := "1794700800"
	text, err := CanonicalReadText(http.MethodGet, "/app/v1/me", nil, ts, device.String())
	if err != nil {
		t.Fatal(err)
	}
	sig := signText(t, key, text)
	base := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/app/v1/me", nil)
		r.Header.Set(HeaderDeviceID, device.String())
		r.Header.Set(HeaderTimestamp, ts)
		r.Header.Set(HeaderSignature, sig)
		return r
	}
	if _, err := ReadFromHTTP(base(), nil); err != nil {
		t.Fatalf("PREMISE: a well-formed read: %v", err)
	}
	for name, mutate := range map[string]func(*http.Request){
		"query":             func(r *http.Request) { r.URL.RawQuery = "a=1" },
		"nonce header too":  func(r *http.Request) { r.Header.Set(HeaderNonce, randomNonceWire(t)) },
		"two timestamps":    func(r *http.Request) { r.Header.Add(HeaderTimestamp, ts) },
		"no timestamp":      func(r *http.Request) { r.Header.Del(HeaderTimestamp) },
		"leading zero":      func(r *http.Request) { r.Header.Set(HeaderTimestamp, "0"+ts) },
		"signed":            func(r *http.Request) { r.Header.Set(HeaderTimestamp, "+"+ts) },
		"zero":              func(r *http.Request) { r.Header.Set(HeaderTimestamp, "0") },
		"fraction":          func(r *http.Request) { r.Header.Set(HeaderTimestamp, ts+".5") },
		"too long":          func(r *http.Request) { r.Header.Set(HeaderTimestamp, strings.Repeat("9", 20)) },
		"upper-case device": func(r *http.Request) { r.Header.Set(HeaderDeviceID, strings.ToUpper(device.String())) },
	} {
		r := base()
		mutate(r)
		if _, err := ReadFromHTTP(r, nil); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: %v, want ErrMalformed", name, err)
		}
	}
	// And a nonce-signed request refuses a timestamp beside its nonce.
	r := httptest.NewRequest(http.MethodPost, "/app/v1/tap", nil)
	r.Header.Set(HeaderDeviceID, device.String())
	r.Header.Set(HeaderNonce, randomNonceWire(t))
	r.Header.Set(HeaderSignature, sig)
	r.Header.Set(HeaderTimestamp, ts)
	if _, err := FromHTTP(r, nil); !errors.Is(err, ErrMalformed) {
		t.Errorf("FromHTTP with a timestamp too: %v, want ErrMalformed", err)
	}
}

// TestRead_ASignatureDoesNotCrossSchemes: the two prefixes separate the schemes -- a read's
// signature does not verify as a nonce-signed request over the same parts and vice versa.
func TestRead_ASignatureDoesNotCrossSchemes(t *testing.T) {
	key := testKey(t)
	device := uuid.New()
	pub, err := ParsePublicKey(rawPublic(t, key))
	if err != nil {
		t.Fatal(err)
	}
	nonce := randomNonceWire(t)
	reqText, err := CanonicalText(http.MethodPost, "/app/v1/tap", nil, nonce, device.String())
	if err != nil {
		t.Fatal(err)
	}
	readText, err := CanonicalReadText(http.MethodPost, "/app/v1/tap", nil, "1794700800", device.String())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(readText), ReadPrefix+"\n") || !strings.HasPrefix(string(reqText), CanonicalPrefix+"\n") {
		t.Fatal("PREMISE: the prefixes")
	}
	sig := mustDecode(t, signText(t, key, readText))
	if verifySignature(pub, reqText, sig) {
		t.Fatal("a read's signature verified over a request's text")
	}
	sig = mustDecode(t, signText(t, key, reqText))
	if verifySignature(pub, readText, sig) {
		t.Fatal("a request's signature verified over a read's text")
	}
}

// TestSignedRead_NoLeakOnAnyRenderingPath: SignedRead prints redacted on every verb, directly
// and inside a caller's unexported field (the measured fmt bad-verb path, request.go).
func TestSignedRead_NoLeakOnAnyRenderingPath(t *testing.T) {
	key := testKey(t)
	device := uuid.New()
	ts := "1794700800"
	text, err := CanonicalReadText(http.MethodGet, "/app/v1/me", nil, ts, device.String())
	if err != nil {
		t.Fatal(err)
	}
	sig := signText(t, key, text)
	r, err := NewSignedRead(http.MethodGet, "/app/v1/me", nil, device.String(), ts, sig)
	if err != nil {
		t.Fatal(err)
	}
	type holder struct{ r SignedRead }
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%d", "%q", "%x"} {
		for _, v := range []any{r, holder{r}, &holder{r}} {
			out := fmt.Sprintf(verb, v)
			if strings.Contains(out, sig) || strings.Contains(out, ts) || strings.Contains(out, device.String()) {
				t.Errorf("%s of %T leaked: %s", verb, v, out)
			}
		}
	}
}

// TestVerifyActivation: the proof of possession -- the body's key verifies the request's
// signature over the challenge, and yields EXACTLY the binding bind recorded for that key and that
// challenge; another key, another challenge or another activation id yields another binding or no
// verification; an invalid key is refused by name.
func TestVerifyActivation(t *testing.T) {
	key, other := testKey(t), testKey(t)
	ch, err := NewActivationChallenge()
	if err != nil {
		t.Fatal(err)
	}
	pub := rawPublic(t, key)
	bound, err := ActivationBinding(ch, pub)
	if err != nil {
		t.Fatal(err)
	}
	if len(bound) != 43 {
		t.Fatalf("binding is %d characters, want invite.Binding's 43", len(bound))
	}
	body := []byte(`{"code":"x","platform":"ios"}`)
	req := func(signer interface{}, id uuid.UUID, challenge string) SignedRequest {
		p := parts{method: http.MethodPost, path: "/app/v1/activate/complete", body: body, device: id, nonce: challenge}
		k := key
		if signer != nil {
			k = other
		}
		return request(t, p, sign(t, k, p))
	}
	got, err := VerifyActivation(req(nil, ch.ID, ch.Challenge.Wire()), pub)
	if err != nil || got != bound {
		t.Fatalf("VerifyActivation = %v (binding equal: %v), want the bound binding", err, got == bound)
	}
	if _, err := VerifyActivation(req(other, ch.ID, ch.Challenge.Wire()), pub); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("another key's signature: %v, want ErrBadSignature", err)
	}
	if got, err := VerifyActivation(req(other, ch.ID, ch.Challenge.Wire()), rawPublic(t, other)); err != nil || got == bound {
		t.Fatalf("another key, self-consistent: err %v, same binding %v -- want a different binding", err, got == bound)
	}
	ch2, err := NewActivationChallenge()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := VerifyActivation(req(nil, ch.ID, ch2.Challenge.Wire()), pub); err != nil || got == bound {
		t.Fatalf("another challenge: err %v, same binding %v", err, got == bound)
	}
	if got, err := VerifyActivation(req(nil, ch2.ID, ch.Challenge.Wire()), pub); err != nil || got == bound {
		t.Fatalf("another activation id: err %v, same binding %v", err, got == bound)
	}
	bad := append([]byte(nil), pub...)
	bad[64] ^= 1 // off the curve
	if _, err := VerifyActivation(req(nil, ch.ID, ch.Challenge.Wire()), bad); !errors.Is(err, ErrInvalidPublicKey) {
		t.Fatalf("an off-curve key: %v, want ErrInvalidPublicKey", err)
	}
	if _, err := ActivationBinding(ch, bad); !errors.Is(err, ErrInvalidPublicKey) {
		t.Fatalf("binding an off-curve key: %v, want ErrInvalidPublicKey", err)
	}
	if _, err := VerifyActivation(SignedRequest{}, pub); !errors.Is(err, ErrMalformed) {
		t.Fatalf("a zero request: %v", err)
	}
	if s := fmt.Sprintf("%v %+v", ch, ch); strings.Contains(s, ch.Challenge.Wire()) {
		t.Fatal("an ActivationChallenge prints its challenge")
	}
}
