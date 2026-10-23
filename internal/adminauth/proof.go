package adminauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
)

// Proof binds a Verified to the digest its password verified against (ADR 0031 §5.2,
// identity_unlink_backfilled's row; M14-07).
//
// WHY IT EXISTS. A session is written by CreateAdminSession, whose INSERT ... SELECT now takes the
// row only while `a.password_hash = <the verified digest>` holds under the row's share lock: a
// membership refused from an identity gets a SENTINEL digest in the refusing transaction, a password
// change or a reset gets a new one, and tappa_app cannot read unlinked_at (R16) -- so "the digest is
// still the one the password verified" is the one test the session write can make. Issue therefore
// needs THE VERIFIED DIGEST, and between the comparison and Issue a Verified can travel through the
// picker's signed cookie (internal/handler's second producer, see Verified) for up to its TTL. A
// digest must never go into a cookie; this MAC of it can: it is keyed by a key derived from the
// session HMAC key and held by this package alone, so the handler can carry it but not compute it,
// and it says nothing about the digest to anyone without that key.
//
// WHAT Issue DOES WITH IT: it reads the row's CURRENT digest inside the session's transaction,
// checks the proof against it in constant time, and hands that digest to CreateAdminSession. The
// proof covers the time from the comparison to that read; the statement's predicate and FOR NO KEY UPDATE
// cover the time from that read to the write.
//
// IT IS A VALUE TYPE (an array, no pointer), so two Verified values compare equal exactly when
// their fields do -- the handler's round-trip tests compare them with ==. It prints redacted on every
// path, like Token: a proof is not a password, but it is derived from a digest and nothing needs to
// read it in a log.
type Proof struct {
	mac [sha256.Size]byte
	set bool
}

const (
	// proofKeyLabel derives the proof key from the session HMAC key -- token.go's argument for
	// deriving rather than adding a variable applies verbatim, and the label keeps a proof from ever
	// equalling a session token hash computed under the same root.
	proofKeyLabel = "tappa/admin-verified-digest/v1/key-derivation"
	// proofLabel prefixes the MAC input, so the same key could never authenticate a different shape.
	proofLabel = "tappa/admin-verified-digest/v1|"
	// proofRedacted is every printing path's output.
	proofRedacted = "adminauth.Proof(redacted)"
)

// errProofStale is Issue's refusal when the row's digest is no longer the one the proof binds:
// the password changed, the row was reset, or the membership was refused from an identity since
// the password verified. It wraps nothing and names nothing.
var errProofStale = errors.New("adminauth: the row's password is no longer the one that verified")

// errProofMalformed is ParseProof's refusal.
var errProofMalformed = errors.New("adminauth: proof has the wrong shape")

var (
	_ fmt.Formatter          = Proof{}
	_ fmt.Stringer           = Proof{}
	_ fmt.GoStringer         = Proof{}
	_ slog.LogValuer         = Proof{}
	_ encoding.TextMarshaler = Proof{}
)

func (Proof) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(proofRedacted)) }

func (Proof) String() string { return proofRedacted }

func (Proof) GoString() string { return proofRedacted }

func (Proof) LogValue() slog.Value { return slog.StringValue(proofRedacted) }

func (Proof) MarshalText() ([]byte, error) { return []byte(proofRedacted), nil }

// IsZero reports a Verified that carries no proof -- one Issue refuses.
func (p Proof) IsZero() bool { return !p.set }

// Encode is the proof's wire form for a signed carrier (the picker cookie): 64 lowercase hex
// characters, or "" for the zero proof. It is deliberately not String.
func (p Proof) Encode() string {
	if !p.set {
		return ""
	}
	return hex.EncodeToString(p.mac[:])
}

// ParseProof is Encode's inverse. "" is the zero proof (Issue refuses it); anything that is not
// exactly 64 lowercase hex characters is an error. It authenticates nothing: the carrier's own
// signature does that, and Issue's check against the row's digest decides.
func ParseProof(s string) (Proof, error) {
	if s == "" {
		return Proof{}, nil
	}
	if len(s) != 2*sha256.Size {
		return Proof{}, errProofMalformed
	}
	var p Proof
	if _, err := hex.Decode(p.mac[:], []byte(s)); err != nil || hex.EncodeToString(p.mac[:]) != s {
		return Proof{}, errProofMalformed
	}
	p.set = true
	return p, nil
}

// deriveProofKey derives the proof key (32 bytes) from the session HMAC key.
func deriveProofKey(sessionKey []byte) ([]byte, error) {
	if len(sessionKey) != hmacKeyLen {
		return nil, fmt.Errorf("adminauth: hmac key must be %d bytes, got %d", hmacKeyLen, len(sessionKey))
	}
	m := hmac.New(sha256.New, sessionKey)
	_, _ = m.Write([]byte(proofKeyLabel)) // hash writes never fail (documented)
	return m.Sum(nil), nil
}

// proofOf is the MAC of (admin, tenant, digest): bound to the row as well as to the digest, so a
// proof minted for one row never fits another that happens to carry the same digest (an identity's
// linked rows all carry its digest).
func (m *Manager) proofOf(admin, tenant uuid.UUID, digest string) Proof {
	mac := hmac.New(sha256.New, m.proofKey)
	// hash writes never fail (documented); the digest is written as-is and never formatted.
	_, _ = mac.Write([]byte(proofLabel))
	_, _ = mac.Write([]byte(admin.String()))
	_, _ = mac.Write([]byte("|"))
	_, _ = mac.Write([]byte(tenant.String()))
	_, _ = mac.Write([]byte("|"))
	_, _ = mac.Write([]byte(digest))
	p := Proof{set: true}
	copy(p.mac[:], mac.Sum(nil))
	return p
}

// proofHolds reports, in constant time, whether p is the proof of (admin, tenant, digest). A zero
// proof never holds.
func (m *Manager) proofHolds(p Proof, admin, tenant uuid.UUID, digest string) bool {
	if !p.set || len(m.proofKey) == 0 {
		return false
	}
	want := m.proofOf(admin, tenant, digest)
	return subtle.ConstantTimeCompare(p.mac[:], want.mac[:]) == 1
}
