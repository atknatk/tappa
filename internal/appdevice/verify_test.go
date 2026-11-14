package appdevice

// verify_test.go -- the verifier's order and its refusals, without a database: a fake resolver
// stands in for db.GetAppDeviceByID and counts whether a transaction was ever opened, so "refused
// BEFORE anything is written" is measured, not assumed. The database paths (the nonce: replay,
// expiry, another device's, another tenant's; the races) are in db_test.go against real Postgres.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
)

// --- helpers shared with db_test.go ---------------------------------------------------------

// testKey is a fresh FAKE P-256 key, made in the test and thrown away with it (§4.7: no real
// key material anywhere; the vectors' keys have no private half in the repository at all).
func testKey(t testing.TB) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return k
}

// rawPublic is k's public half in the stored form (65 bytes, 0x04 || X || Y).
func rawPublic(t testing.TB, k *ecdsa.PrivateKey) []byte {
	t.Helper()
	b, err := k.PublicKey.Bytes()
	if err != nil {
		t.Fatalf("public key bytes: %v", err)
	}
	return b
}

// parts is one request as the app would send it.
type parts struct {
	method, path string
	body         []byte
	device       uuid.UUID
	nonce        string // wire form
}

func defaultParts(device uuid.UUID, nonce string) parts {
	return parts{method: http.MethodPost, path: "/app/v1/tap", body: []byte(`{"plaque":"04A1B2C3D4E5F6","ctr":17}`), device: device, nonce: nonce}
}

// sign signs p's canonical text with k, the way a device does, and returns the header value.
func sign(t testing.TB, k *ecdsa.PrivateKey, p parts) string {
	t.Helper()
	text, err := CanonicalText(p.method, p.path, p.body, p.nonce, p.device.String())
	if err != nil {
		t.Fatalf("CanonicalText: %v", err)
	}
	return signText(t, k, text)
}

func sha256Sum(b []byte) [sha256.Size]byte { return sha256.Sum256(b) }

func signText(t testing.TB, k *ecdsa.PrivateKey, text []byte) string {
	t.Helper()
	digest := sha256Sum(text)
	sig, err := ecdsa.SignASN1(rand.Reader, k, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signatureEncoding.EncodeToString(sig)
}

// request builds the SignedRequest for p carrying sig.
func request(t testing.TB, p parts, sig string) SignedRequest {
	t.Helper()
	r, err := NewSignedRequest(p.method, p.path, p.body, p.device.String(), p.nonce, sig)
	if err != nil {
		t.Fatalf("NewSignedRequest: %v", err)
	}
	return r
}

// randomNonceWire is a well-shaped nonce this server never issued.
func randomNonceWire(t testing.TB) string {
	t.Helper()
	n, err := newNonce()
	if err != nil {
		t.Fatalf("newNonce: %v", err)
	}
	return n.Wire()
}

// --- the fake resolver ----------------------------------------------------------------------

type fakeDB struct {
	dev        db.ResolvedAppDevice
	resolveErr error
	txErr      error
	txCalls    int
}

func (f *fakeDB) GetAppDeviceByID(_ context.Context, id uuid.UUID) (db.ResolvedAppDevice, error) {
	if f.resolveErr != nil {
		return db.ResolvedAppDevice{}, f.resolveErr
	}
	d := f.dev
	d.ID = id // the resolver is keyed by the primary key: it answers for the id it was asked
	return d, nil
}

func (f *fakeDB) WithTenant(_ context.Context, _ uuid.UUID, _ db.TxFunc) error {
	f.txCalls++
	return f.txErr
}

// TestVerify_RefusalOrder: every refusal that does not need the nonce is decided before a
// transaction opens (txCalls 0) -- revoked BEFORE the signature, so a valid and a junk signature
// from a revoked device get the same answer; a well-signed request reaches the spend.
func TestVerify_RefusalOrder(t *testing.T) {
	key, other := testKey(t), testKey(t)
	device := uuid.New()
	nonce := randomNonceWire(t)
	p := defaultParts(device, nonce)
	good := sign(t, key, p)
	byOther := sign(t, other, p)
	revokedAt := time.Now()

	live := db.ResolvedAppDevice{TenantID: uuid.New(), EmployeeID: uuid.New(), PublicKey: rawPublic(t, key)}
	revoked := live
	revoked.RevokedAt = &revokedAt
	offCurve := live
	offCurve.PublicKey = append([]byte(nil), live.PublicKey...)
	offCurve.PublicKey[64] ^= 0x01

	tampered := func(mut func(*parts)) SignedRequest {
		q := p
		mut(&q)
		return request(t, q, good)
	}

	tests := []struct {
		name       string
		db         fakeDB
		req        SignedRequest
		want       error // a sentinel, or nil for "not an AuthError"
		wantStatus int
		wantTx     int
	}{
		{name: "unknown device", db: fakeDB{resolveErr: pgx.ErrNoRows}, req: request(t, p, good), want: ErrUnknownDevice, wantStatus: 401},
		{name: "revoked device, valid signature", db: fakeDB{dev: revoked}, req: request(t, p, good), want: ErrRevokedDevice, wantStatus: 401},
		{name: "revoked device, junk signature", db: fakeDB{dev: revoked}, req: request(t, p, byOther), want: ErrRevokedDevice, wantStatus: 401},
		{name: "wrong key", db: fakeDB{dev: live}, req: request(t, p, byOther), want: ErrBadSignature, wantStatus: 401},
		{name: "tampered body", db: fakeDB{dev: live}, req: tampered(func(q *parts) { q.body = []byte(`{"plaque":"04A1B2C3D4E5F6","ctr":18}`) }), want: ErrBadSignature, wantStatus: 401},
		{name: "tampered method", db: fakeDB{dev: live}, req: tampered(func(q *parts) { q.method = http.MethodPut }), want: ErrBadSignature, wantStatus: 401},
		{name: "tampered path", db: fakeDB{dev: live}, req: tampered(func(q *parts) { q.path = "/app/v1/revoke" }), want: ErrBadSignature, wantStatus: 401},
		{name: "tampered nonce", db: fakeDB{dev: live}, req: tampered(func(q *parts) { q.nonce = randomNonceWire(t) }), want: ErrBadSignature, wantStatus: 401},
		{name: "another device id under this key", db: fakeDB{dev: live}, req: tampered(func(q *parts) { q.device = uuid.New() }), want: ErrBadSignature, wantStatus: 401},
		{name: "stored key off the curve", db: fakeDB{dev: offCurve}, req: request(t, p, good), want: nil, wantStatus: 500},
		{name: "resolver outage", db: fakeDB{resolveErr: errors.New("connection refused")}, req: request(t, p, good), want: nil, wantStatus: 500},
		{name: "zero request", db: fakeDB{dev: live}, req: SignedRequest{}, want: ErrMalformed, wantStatus: 401},
		{name: "valid, spend refused", db: fakeDB{dev: live, txErr: ErrNonceRejected}, req: request(t, p, good), want: ErrNonceRejected, wantStatus: 401, wantTx: 1},
		{name: "valid, spend outage", db: fakeDB{dev: live, txErr: errors.New("connection reset")}, req: request(t, p, good), want: nil, wantStatus: 500, wantTx: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.db
			v, err := New(&f)
			if err != nil {
				t.Fatal(err)
			}
			got, err := v.Verify(context.Background(), tc.req)
			if err == nil {
				t.Fatal("Verify accepted")
			}
			if got != (Device{}) {
				t.Fatalf("a refusal carried a device: %+v", got)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("Verify = %v (%s), want %s", err, reason(err), tc.want.(*AuthError).Reason())
			}
			if tc.want == nil {
				var ae *AuthError
				if errors.As(err, &ae) {
					t.Fatalf("a server fault became a refusal: %s", ae.Reason())
				}
			}
			if s := StatusOf(err); s != tc.wantStatus {
				t.Fatalf("StatusOf = %d, want %d", s, tc.wantStatus)
			}
			if f.txCalls != tc.wantTx {
				t.Fatalf("transactions opened = %d, want %d", f.txCalls, tc.wantTx)
			}
		})
	}
	// Positive control: the same fake with a spend that succeeds returns the device's facts.
	f := fakeDB{dev: live}
	v, _ := New(&f)
	got, err := v.Verify(context.Background(), request(t, p, good))
	if err != nil {
		t.Fatalf("valid request: %v", err)
	}
	if got != (Device{ID: device, TenantID: live.TenantID, EmployeeID: live.EmployeeID}) || f.txCalls != 1 {
		t.Fatalf("valid request = %+v after %d transactions", got, f.txCalls)
	}
}

func reason(err error) string {
	var ae *AuthError
	if errors.As(err, &ae) {
		return ae.Reason()
	}
	return "not an AuthError"
}

// TestErrors_OneTextAfterTheShape: unknown, revoked, bad signature and spent nonce print the SAME
// text and the same status -- a handler that echoes err.Error() still answers identically (no
// status oracle, M16-03 review) -- while errors.Is and Reason keep them apart for the server.
func TestErrors_OneTextAfterTheShape(t *testing.T) {
	refusals := []*AuthError{ErrUnknownDevice, ErrRevokedDevice, ErrBadSignature, ErrNonceRejected}
	reasons := map[string]bool{}
	for i, e := range refusals {
		if e.Error() != refusals[0].Error() {
			t.Fatalf("%s prints %q, %s prints %q", e.Reason(), e.Error(), refusals[0].Reason(), refusals[0].Error())
		}
		if StatusOf(e) != http.StatusUnauthorized {
			t.Fatalf("%s: status %d", e.Reason(), StatusOf(e))
		}
		if reasons[e.Reason()] {
			t.Fatalf("two refusals share the reason %q", e.Reason())
		}
		reasons[e.Reason()] = true
		for j, o := range refusals {
			if i != j && errors.Is(e, o) {
				t.Fatalf("errors.Is(%s, %s) is true", e.Reason(), o.Reason())
			}
		}
	}
	if ErrMalformed.Error() == ErrUnknownDevice.Error() {
		t.Fatal("malformed should be told apart: it is decided from the caller's own input, before any device is looked up")
	}
}

// derInt encodes a non-negative big.Int as a minimal DER INTEGER.
func derInt(x *big.Int) []byte {
	b := x.Bytes()
	if len(b) == 0 {
		b = []byte{0}
	}
	if b[0]&0x80 != 0 {
		b = append([]byte{0}, b...)
	}
	return append([]byte{0x02, byte(len(b))}, b...)
}

func derSeq(items ...[]byte) []byte {
	var body []byte
	for _, it := range items {
		body = append(body, it...)
	}
	return append([]byte{0x30, byte(len(body))}, body...)
}

// TestCheckSignature_MalformedDER: VerifyASN1's DER strictness, measured -- each mangled
// encoding of a VALID (r, s) is refused, and the minimal re-encoding verifies (positive control:
// the helper builds real signatures, so a refusal is about the encoding).
func TestCheckSignature_MalformedDER(t *testing.T) {
	key := testKey(t)
	device := uuid.New()
	p := defaultParts(device, randomNonceWire(t))
	text, err := CanonicalText(p.method, p.path, p.body, p.nonce, device.String())
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256Sum(text)
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	n := elliptic.P256().Params().N
	dev := db.ResolvedAppDevice{ID: device, PublicKey: rawPublic(t, key)}
	check := func(sig []byte) error {
		q := request(t, p, base64.RawURLEncoding.EncodeToString(sig))
		return checkSignature(dev, q)
	}

	if err := check(derSeq(derInt(r), derInt(s))); err != nil {
		t.Fatalf("minimal DER of a valid signature: %v", err)
	}
	if err := check(derSeq(derInt(r), derInt(new(big.Int).Sub(n, s)))); err != nil {
		t.Fatalf("the high/low-S twin must verify too (accepted by design, key.go): %v", err)
	}
	nonMinimal := func(x *big.Int) []byte {
		b := append([]byte{0x00, 0x00}, x.Bytes()...)
		return append([]byte{0x02, byte(len(b))}, b...)
	}
	negative := func(x *big.Int) []byte { // drop the sign-padding byte: reads as negative when the top bit is set
		b := x.Bytes()
		return append([]byte{0x02, byte(len(b))}, b...)
	}
	// One of s and n-s has its top bit set; "negative s" needs that one.
	topBitSet := s
	if s.Bytes()[0]&0x80 == 0 {
		topBitSet = new(big.Int).Sub(n, s)
	}
	minimal := derSeq(derInt(r), derInt(s))
	longForm := append([]byte{0x30, 0x81, minimal[1]}, minimal[2:]...)

	tests := []struct {
		name string
		sig  []byte
	}{
		{"trailing byte", append(append([]byte(nil), minimal...), 0x00)},
		{"non-minimal r", derSeq(nonMinimal(r), derInt(s))},
		{"negative s", derSeq(derInt(r), negative(topBitSet))},
		{"r zero", derSeq(derInt(big.NewInt(0)), derInt(s))},
		{"s zero", derSeq(derInt(r), derInt(big.NewInt(0)))},
		{"s equals n", derSeq(derInt(r), derInt(n))},
		{"s plus n", derSeq(derInt(r), derInt(new(big.Int).Add(s, n)))},
		{"BER long-form length", longForm},
		{"wrong outer tag", append([]byte{0x31}, minimal[1:]...)},
		{"truncated", minimal[:len(minimal)-1]},
		{"r and s swapped", derSeq(derInt(s), derInt(r))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.sig) < minSignatureLen || len(tc.sig) > maxSignatureLen {
				if _, err := parseSignature(base64.RawURLEncoding.EncodeToString(tc.sig)); !errors.Is(err, ErrMalformed) {
					t.Fatalf("out-of-bounds length %d passed the shape gate", len(tc.sig))
				}
				return
			}
			if err := check(tc.sig); !errors.Is(err, ErrBadSignature) {
				t.Fatalf("checkSignature = %v, want ErrBadSignature", err)
			}
		})
	}
}

// TestFromHTTP: the header contract -- each header exactly once, no query, the escaped path
// signed -- and every refusal ErrMalformed before any database work.
func TestFromHTTP(t *testing.T) {
	key := testKey(t)
	device := uuid.New()
	nonce := randomNonceWire(t)
	p := defaultParts(device, nonce)
	sig := sign(t, key, p)
	build := func(target string, mut func(h http.Header)) *http.Request {
		r := httptest.NewRequest(p.method, target, strings.NewReader(string(p.body)))
		r.Header.Set(HeaderDeviceID, device.String())
		r.Header.Set(HeaderNonce, nonce)
		r.Header.Set(HeaderSignature, sig)
		if mut != nil {
			mut(r.Header)
		}
		return r
	}

	req, err := FromHTTP(build(p.path, nil), p.body)
	if err != nil {
		t.Fatalf("FromHTTP: %v", err)
	}
	dev := db.ResolvedAppDevice{ID: device, PublicKey: rawPublic(t, key)}
	if err := checkSignature(dev, req); err != nil {
		t.Fatalf("a request read from HTTP does not verify: %v", err)
	}
	if req.DeviceID() != device {
		t.Fatal("DeviceID() is not the header's device")
	}

	tests := []struct {
		name string
		r    *http.Request
	}{
		{"query string", build(p.path+"?x=1", nil)},
		{"empty query", build(p.path+"?", nil)},
		{"no device header", build(p.path, func(h http.Header) { h.Del(HeaderDeviceID) })},
		{"no nonce header", build(p.path, func(h http.Header) { h.Del(HeaderNonce) })},
		{"no signature header", build(p.path, func(h http.Header) { h.Del(HeaderSignature) })},
		{"repeated nonce header", build(p.path, func(h http.Header) { h.Add(HeaderNonce, nonce) })},
		{"repeated signature header", build(p.path, func(h http.Header) { h.Add(HeaderSignature, sig) })},
		// A literal '=': base64.URLEncoding adds none when the DER length is a multiple of 3, so
		// re-encoding with padding made this case pass by luck one run in three (measured).
		{"padded signature", build(p.path, func(h http.Header) { h.Set(HeaderSignature, sig+"=") })},
		{"oversized signature", build(p.path, func(h http.Header) { h.Set(HeaderSignature, strings.Repeat("A", 200)) })},
		{"upper-case device id", build(p.path, func(h http.Header) { h.Set(HeaderDeviceID, strings.ToUpper(device.String())) })},
		{"nil request", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := FromHTTP(tc.r, p.body); !errors.Is(err, ErrMalformed) {
				t.Fatalf("FromHTTP = %v, want ErrMalformed", err)
			}
		})
	}

	// The escaped path is what is signed: a percent-encoded route verifies only when signed as sent.
	escaped := "/app/v1/t%61p"
	pe := p
	pe.path = escaped
	r := build(escaped, func(h http.Header) { h.Set(HeaderSignature, sign(t, key, pe)) })
	got, err := FromHTTP(r, p.body)
	if err != nil {
		t.Fatalf("FromHTTP(escaped): %v", err)
	}
	if err := checkSignature(dev, got); err != nil {
		t.Fatalf("the escaped path signed as sent does not verify: %v", err)
	}
}

func mustDecode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := signatureEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestNew_RefusesNilDatabase keeps the constructor honest.
func TestNew_RefusesNilDatabase(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("New(nil) succeeded")
	}
}
