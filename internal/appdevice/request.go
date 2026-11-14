package appdevice

import (
	"crypto/sha256"
	"encoding"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
)

// The three request headers a signed app request carries (the wire half of ADR 0045 §2.3; the
// /app/v1 surface, M16-07, reads them through FromHTTP). No Authorization header and no cookie:
// the app holds no bearer credential -- a copied request or a stolen file cannot make a second
// request, because each one spends a nonce and only the device's key signs the next.
const (
	// HeaderDeviceID carries the device id, canonical lower-case form.
	HeaderDeviceID = "App-Device-Id"
	// HeaderNonce carries the nonce the server issued, as issued (43 base64url characters).
	HeaderNonce = "App-Nonce"
	// HeaderSignature carries the ECDSA P-256 / SHA-256 signature over the canonical text: the
	// ASN.1 DER (X9.62) form both platforms emit -- iOS SecKeyCreateSignature with
	// .ecdsaSignatureMessageX962SHA256, Android Signature "SHA256withECDSA" -- in unpadded
	// base64url (strict).
	HeaderSignature = "App-Signature"
)

const (
	// minSignatureLen / maxSignatureLen bound the DER signature: a P-256 SEQUENCE of two
	// INTEGERs is at most 2+2+33+2+33 = 72 bytes, and no valid one is under 8. The bound is a
	// cheap shape gate before the database; DER validity itself is ecdsa.VerifyASN1's.
	minSignatureLen = 8
	maxSignatureLen = 72
	// redactedRequest is what every printing path emits instead of a SignedRequest.
	redactedRequest = "appdevice.SignedRequest(redacted)"
)

var signatureEncoding = base64.RawURLEncoding.Strict()

// SignedRequest is one app request, parsed at the handler boundary (CLAUDE.md §7: external input
// is validated there) and ready for Verifier.Verify. It holds the canonical text's parts and the
// signature -- NOT the body: the body enters as its SHA-256 at construction and is not retained,
// so it cannot reach a log through this value.
//
// Built like Nonce (and session.Token): redacting methods plus indirection, so neither a direct
// print nor a print of a caller's unexported field renders the signature, the nonce or the body
// digest.
//
// 🔴 THE FIELD IS A POINTER TO A POINTER, AND A SINGLE POINTER WAS MEASURED TO LEAK. Printing a
// caller's struct holding a SignedRequest in an unexported field with a verb fmt does not
// support for pointers (%s, %d, %q) takes fmt's bad-verb path, which prints the pointer's target
// at depth 0 -- and at depth 0 fmt DEREFERENCES a pointer to a struct, array, slice or map
// ("%!s(*appdevice.signedRequest=&{POST /app/v1/tap [6 216 ...] ... [171 205 239 ...]})": the
// signature's bytes in decimal). A pointer to a POINTER is printed as an address on every path.
// session.Token's *string is safe for the same reason (a string is not dereferenced there);
// leak_external_test.go holds both shapes.
type SignedRequest struct{ p **signedRequest }

type signedRequest struct {
	method     string
	path       string
	bodyDigest [sha256.Size]byte
	deviceID   uuid.UUID
	nonce      Nonce
	signature  []byte
}

var (
	_ fmt.Formatter          = SignedRequest{}
	_ fmt.Stringer           = SignedRequest{}
	_ fmt.GoStringer         = SignedRequest{}
	_ slog.LogValuer         = SignedRequest{}
	_ encoding.TextMarshaler = SignedRequest{}
)

// NewSignedRequest validates a request's parts as they travel -- the method, the escaped path,
// the raw body, and the three header values -- and returns the request Verify takes. Every
// refusal is ErrMalformed and happens before any database work; no value is echoed.
func NewSignedRequest(method, path string, body []byte, deviceID, nonce, signature string) (SignedRequest, error) {
	if !validMethod(method) || !validPath(path) {
		return SignedRequest{}, ErrMalformed
	}
	id, err := parseDeviceID(deviceID)
	if err != nil {
		return SignedRequest{}, err
	}
	n, err := ParseNonce(nonce)
	if err != nil {
		return SignedRequest{}, err
	}
	sig, err := parseSignature(signature)
	if err != nil {
		return SignedRequest{}, err
	}
	r := &signedRequest{
		method:     method,
		path:       path,
		bodyDigest: sha256.Sum256(body),
		deviceID:   id,
		nonce:      n,
		signature:  sig,
	}
	return SignedRequest{p: &r}, nil
}

// FromHTTP reads a signed request from r and the body the handler already read (with its own
// size limit -- this package does not read bodies). A request with a query string is
// ErrMalformed: the canonical text signs the path alone (canonical.go), so a query would be
// unsigned. Each header must appear exactly once: two values would leave "which one was signed"
// to the order a proxy kept.
func FromHTTP(r *http.Request, body []byte) (SignedRequest, error) {
	if r == nil || r.URL == nil || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return SignedRequest{}, ErrMalformed
	}
	device, ok1 := single(r.Header, HeaderDeviceID)
	nonce, ok2 := single(r.Header, HeaderNonce)
	sig, ok3 := single(r.Header, HeaderSignature)
	if !ok1 || !ok2 || !ok3 {
		return SignedRequest{}, ErrMalformed
	}
	return NewSignedRequest(r.Method, r.URL.EscapedPath(), body, device, nonce, sig)
}

func single(h http.Header, name string) (string, bool) {
	v := h.Values(name)
	if len(v) != 1 {
		return "", false
	}
	return v[0], true
}

// parseSignature decodes the header value: strict unpadded base64url of 8..72 bytes.
func parseSignature(s string) ([]byte, error) {
	if len(s) == 0 || len(s) > signatureEncoding.EncodedLen(maxSignatureLen) {
		return nil, ErrMalformed
	}
	raw, err := signatureEncoding.DecodeString(s)
	if err != nil || len(raw) < minSignatureLen || len(raw) > maxSignatureLen {
		return nil, ErrMalformed
	}
	return raw, nil
}

// DeviceID is the device the request names. It is a CLAIM until Verify returns: the id is not a
// credential (the signature is), so nothing may be attributed to it before then.
func (s SignedRequest) DeviceID() uuid.UUID {
	if r := s.get(); r != nil {
		return r.deviceID
	}
	return uuid.Nil
}

// get is the request's parts, nil for the zero SignedRequest.
func (s SignedRequest) get() *signedRequest {
	if s.p == nil {
		return nil
	}
	return *s.p
}

// canonicalText is the request's canonical text, through the one builder.
func (s SignedRequest) canonicalText() []byte {
	r := s.get()
	return canonical(r.method, r.path, r.bodyDigest, r.nonce.wire(), r.deviceID)
}

// Format implements fmt.Formatter for every verb.
func (SignedRequest) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redactedRequest)) }

// String implements fmt.Stringer.
func (SignedRequest) String() string { return redactedRequest }

// GoString covers %#v.
func (SignedRequest) GoString() string { return redactedRequest }

// LogValue implements slog.LogValuer.
func (SignedRequest) LogValue() slog.Value { return slog.StringValue(redactedRequest) }

// MarshalText implements encoding.TextMarshaler.
func (SignedRequest) MarshalText() ([]byte, error) { return []byte(redactedRequest), nil }
