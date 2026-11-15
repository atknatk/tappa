package appdevice

import (
	"context"
	"crypto/sha256"
	"encoding"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// SIGNED READS (M16-07; the card's "Okuma isteklerinin kimliği"). GET /app/v1/me, POST
// /app/v1/tap/start and POST /app/v1/nonce change no state the replay of which could hurt
// anybody, so they carry a SIGNED TIMESTAMP instead of a single-use nonce -- the nonce request
// cannot carry a nonce at all (it is what asks for one). Their canonical text is canonical()'s
// six lines under ReadPrefix, with the decimal Unix second in the fifth line:
//
//	tappa-app-read-v1
//	POST
//	/app/v1/nonce
//	<hex SHA-256 of the raw body>
//	1794700800
//	<the device id>
//
// WHAT A READ PROVES, AND WHAT IT DOES NOT. It proves that the holder of the device's key signed
// THIS method, path and body within ReadWindow of the server's clock. It does NOT prove the
// request is the first of its kind: inside the window a captured read can be sent again. That
// is the accepted price, and it is bounded by what a read returns -- the device's own name and
// brand (me), a tap context bound to the device that only a NONCE-signed tap can spend
// (tap/start), or a nonce whose use needs the key again (nonce). Every request that changes
// state stays nonce-signed (Verify).
//
// The window is the SERVER's clock against the signed second, both directions: a phone whose
// clock is more than a minute out cannot read until it is corrected, and says so (401).

// HeaderTimestamp carries a signed read's Unix second, decimal.
const HeaderTimestamp = "App-Timestamp"

// ReadWindow is how far a signed read's timestamp may be from the server's clock, either way.
// A minute covers a slow network and ordinary clock drift (both platforms sync time by
// default); wider would only lengthen the replay window above.
const ReadWindow = 60 * time.Second

// maxTimestampLen bounds the timestamp's digits: a positive int64 has at most 19.
const maxTimestampLen = 19

// redactedRead is what every printing path emits instead of a SignedRead.
const redactedRead = "appdevice.SignedRead(redacted)"

// ErrStaleTimestamp: a signed read's timestamp is outside ReadWindow of the server's clock. It
// is decided before the database, from the caller's own input and the server's clock alone, so
// it reveals nothing about any device -- and it prints the shared text all the same, because the
// card answers it like an unsigned read: 401, empty body.
var ErrStaleTimestamp = &AuthError{text: notAuthenticated, reason: "stale timestamp"}

// SignedRead is one signed read, parsed at the handler boundary. Built like SignedRequest (the
// pointer-to-pointer and the redacting methods, for the measured reasons written there).
type SignedRead struct{ p **signedRead }

type signedRead struct {
	method     string
	path       string
	bodyDigest [sha256.Size]byte
	deviceID   uuid.UUID
	timestamp  int64
	signature  []byte
}

var (
	_ fmt.Formatter          = SignedRead{}
	_ fmt.Stringer           = SignedRead{}
	_ fmt.GoStringer         = SignedRead{}
	_ slog.LogValuer         = SignedRead{}
	_ encoding.TextMarshaler = SignedRead{}
)

// NewSignedRead validates a read's parts as they travel. Every refusal is ErrMalformed.
func NewSignedRead(method, path string, body []byte, deviceID, timestamp, signature string) (SignedRead, error) {
	if !validMethod(method) || !validPath(path) {
		return SignedRead{}, ErrMalformed
	}
	id, err := parseDeviceID(deviceID)
	if err != nil {
		return SignedRead{}, err
	}
	ts, err := parseTimestamp(timestamp)
	if err != nil {
		return SignedRead{}, err
	}
	sig, err := parseSignature(signature)
	if err != nil {
		return SignedRead{}, err
	}
	r := &signedRead{
		method:     method,
		path:       path,
		bodyDigest: sha256.Sum256(body),
		deviceID:   id,
		timestamp:  ts,
		signature:  sig,
	}
	return SignedRead{p: &r}, nil
}

// ReadFromHTTP reads a signed read from r and the body the handler already read (raw bytes, under
// the handler's own size limit). Same rules as FromHTTP: no query, each header exactly once, and
// ONE scheme per request -- a read that also carries App-Nonce is refused rather than left to
// whichever header an endpoint looks at.
func ReadFromHTTP(r *http.Request, body []byte) (SignedRead, error) {
	if r == nil || r.URL == nil || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return SignedRead{}, ErrMalformed
	}
	if len(r.Header.Values(HeaderNonce)) != 0 {
		return SignedRead{}, ErrMalformed
	}
	device, ok1 := single(r.Header, HeaderDeviceID)
	ts, ok2 := single(r.Header, HeaderTimestamp)
	sig, ok3 := single(r.Header, HeaderSignature)
	if !ok1 || !ok2 || !ok3 {
		return SignedRead{}, ErrMalformed
	}
	return NewSignedRead(r.Method, r.URL.EscapedPath(), body, device, ts, sig)
}

// parseTimestamp accepts the one spelling strconv.FormatInt writes for a positive second: 1..19
// ASCII digits, no sign, no leading zero, no space. One second, one text.
func parseTimestamp(s string) (int64, error) {
	if len(s) == 0 || len(s) > maxTimestampLen || s[0] == '0' {
		return 0, ErrMalformed
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, ErrMalformed
		}
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v <= 0 || strconv.FormatInt(v, 10) != s {
		return 0, ErrMalformed
	}
	return v, nil
}

// CanonicalReadText builds a signed read's canonical text from its parts as they travel: the
// function the read vectors pin (test/fixtures/appdevice_vectors.json, "reads").
func CanonicalReadText(method, path string, body []byte, timestamp, deviceID string) ([]byte, error) {
	if !validMethod(method) || !validPath(path) {
		return nil, ErrMalformed
	}
	ts, err := parseTimestamp(timestamp)
	if err != nil {
		return nil, err
	}
	id, err := parseDeviceID(deviceID)
	if err != nil {
		return nil, err
	}
	return canonical(ReadPrefix, method, path, sha256.Sum256(body), strconv.FormatInt(ts, 10), id), nil
}

// DeviceID is the device the read names -- a CLAIM until VerifyRead returns.
func (s SignedRead) DeviceID() uuid.UUID {
	if r := s.get(); r != nil {
		return r.deviceID
	}
	return uuid.Nil
}

func (s SignedRead) get() *signedRead {
	if s.p == nil {
		return nil
	}
	return *s.p
}

func (s SignedRead) canonicalText() []byte {
	r := s.get()
	return canonical(ReadPrefix, r.method, r.path, r.bodyDigest, strconv.FormatInt(r.timestamp, 10), r.deviceID)
}

// VerifyRead authenticates one signed read and returns its device. It writes NOTHING: no nonce
// is spent and last_used_at is not touched (a read is not a use the panel's list should show).
//
// ORDER: the window first (pure, the server's clock -- ErrStaleTimestamp); then Verify's steps 2
// and 3 exactly (the resolver; unknown and revoked BEFORE the signature, one text; the key on
// the curve; the signature over the read's text). The outcomes are Verify's, minus the nonce's,
// plus ErrStaleTimestamp; Device is empty on every refusal.
func (v *Verifier) VerifyRead(ctx context.Context, req SignedRead) (Device, error) {
	r := req.get()
	if r == nil {
		return Device{}, ErrMalformed
	}
	now := v.clock()
	signedAt := time.Unix(r.timestamp, 0)
	if d := now.Sub(signedAt); d > ReadWindow || d < -ReadWindow {
		return Device{}, ErrStaleTimestamp
	}
	dev, err := v.resolve(ctx, r.deviceID)
	if err != nil {
		return Device{}, err
	}
	if dev.ID != r.deviceID {
		return Device{}, fmt.Errorf("appdevice: the resolver answered for another device")
	}
	pub, err := ParsePublicKey(dev.PublicKey)
	if err != nil {
		return Device{}, fmt.Errorf("appdevice: a stored device key is not a P-256 point")
	}
	if !verifySignature(pub, req.canonicalText(), r.signature) {
		return Device{}, ErrBadSignature
	}
	return Device{ID: dev.ID, TenantID: dev.TenantID, EmployeeID: dev.EmployeeID}, nil
}

// Format implements fmt.Formatter for every verb.
func (SignedRead) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redactedRead)) }

// String implements fmt.Stringer.
func (SignedRead) String() string { return redactedRead }

// GoString covers %#v.
func (SignedRead) GoString() string { return redactedRead }

// LogValue implements slog.LogValuer.
func (SignedRead) LogValue() slog.Value { return slog.StringValue(redactedRead) }

// MarshalText implements encoding.TextMarshaler.
func (SignedRead) MarshalText() ([]byte, error) { return []byte(redactedRead), nil }
