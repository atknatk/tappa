package appdevice

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/google/uuid"
)

// THE CANONICAL TEXT (ADR 0045 §2.3: method, path, body digest, nonce, device id). It is the
// only thing a device signs, and BOTH platforms rebuild it byte for byte -- so it is defined in
// ONE function (canonical, below) and frozen as known-answer vectors in
// test/fixtures/appdevice_vectors.json, which the iOS and Android tests read too (M16-12,
// M16-16).
//
// Shape -- six lines, each ended by one LF (0x0A), ASCII only:
//
//	tappa-app-request-v1                                  the domain prefix
//	POST                                                  the method, upper case
//	/app/v1/tap                                           the path as sent, no query
//	<hex SHA-256 of the raw body, 64 lower-case chars>    the body digest
//	<the nonce as issued, 43 base64url chars>             the server's nonce
//	<the device id, 36 lower-case chars, hyphenated>      the device
//
// WHY EACH CHOICE.
//   - The PREFIX is domain separation: a signature made by this key over anything else this
//     protocol ever signs (a later version, the bind step, a live-QR claim) cannot be replayed
//     here, because no other message starts with these bytes. A v2 changes the prefix.
//   - LF-TERMINATED fields are unambiguous because no field may contain an LF (each is
//     validated to a closed character set below) -- so two different requests can never
//     produce the same text by shifting a boundary.
//   - The BODY enters as its digest, so the text has a fixed shape whatever the body is; the
//     digest is over the RAW bytes received, never a re-serialised JSON (two encoders disagree
//     about spacing and key order; bytes do not).
//   - The PATH is the escaped path exactly as on the wire. A QUERY IS REFUSED (FromHTTP), not
//     signed: the app's routes are fixed and carry their data in the body, and an unsigned
//     query would be the one part of a request nobody vouched for. Percent-encoding
//     normalisation differences between URL libraries are avoided by the same rule -- the
//     routes are plain ASCII.
//   - The NONCE is the wire string the server issued, not its bytes: no platform re-encodes
//     anything, it copies the value it was given.
//   - The DEVICE ID is in the canonical lower-case form only; FromHTTP refuses any other
//     spelling uuid.Parse would accept (upper case, braces, urn:) so that one device has one
//     text.

// CanonicalPrefix is the first line of every canonical text (version 1 of the format).
const CanonicalPrefix = "tappa-app-request-v1"

const (
	// maxMethodLen bounds the method: the longest standard one is 7 (OPTIONS, CONNECT).
	maxMethodLen = 16
	// maxPathLen bounds the path. The app's routes are short and fixed; the bound exists so a
	// junk header costs nothing.
	maxPathLen = 1024
)

// canonical is THE definition of the signed bytes. Every caller -- the verifier and the
// exported CanonicalText -- goes through it; the fields are already validated.
func canonical(method, path string, bodyDigest [sha256.Size]byte, nonceWire string, deviceID uuid.UUID) []byte {
	var b strings.Builder
	b.Grow(len(CanonicalPrefix) + len(method) + len(path) + 2*sha256.Size + nonceLen + 36 + 6)
	for _, field := range []string{
		CanonicalPrefix,
		method,
		path,
		hex.EncodeToString(bodyDigest[:]),
		nonceWire,
		deviceID.String(),
	} {
		b.WriteString(field)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// CanonicalText builds the canonical text from a request's parts as they travel: the method,
// the escaped path, the raw body, the nonce's wire string and the device id's string. It is the
// function the known-answer vectors pin; the verifier uses the same builder through
// SignedRequest. A part that cannot belong to a valid request is ErrMalformed.
func CanonicalText(method, path string, body []byte, nonceWire, deviceID string) ([]byte, error) {
	if !validMethod(method) || !validPath(path) {
		return nil, ErrMalformed
	}
	n, err := ParseNonce(nonceWire)
	if err != nil {
		return nil, err
	}
	id, err := parseDeviceID(deviceID)
	if err != nil {
		return nil, err
	}
	return canonical(method, path, sha256.Sum256(body), n.wire(), id), nil
}

// validMethod: 1..16 upper-case ASCII letters (RFC 9110 methods are case-sensitive tokens; the
// standard ones are upper case and the app sends nothing else).
func validMethod(m string) bool {
	if len(m) == 0 || len(m) > maxMethodLen {
		return false
	}
	for i := 0; i < len(m); i++ {
		if m[i] < 'A' || m[i] > 'Z' {
			return false
		}
	}
	return true
}

// validPath: starts with '/', 1..1024 bytes of visible ASCII (0x21-0x7E), no '?' and no '#'.
// No space and no control character, so no LF can enter the canonical text through it.
func validPath(p string) bool {
	if len(p) == 0 || len(p) > maxPathLen || p[0] != '/' {
		return false
	}
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c < 0x21 || c > 0x7E || c == '?' || c == '#' {
			return false
		}
	}
	return true
}

// parseDeviceID accepts the canonical form only: what uuid.UUID.String() prints. uuid.Parse
// also takes upper case, braces and a urn: prefix; letting those through would give one device
// several canonical texts.
func parseDeviceID(s string) (uuid.UUID, error) {
	if len(s) != 36 {
		return uuid.Nil, ErrMalformed
	}
	id, err := uuid.Parse(s)
	if err != nil || id.String() != s || id == uuid.Nil {
		return uuid.Nil, ErrMalformed
	}
	return id, nil
}
