package operatorauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/sun"
)

// Enrollment (ADR 0020 §3, ADR 0021 §1 op_complete_enrollment): the link cmd/opadmin
// prints carries the account id and a one-time token; the page shows a fresh TOTP
// secret; the operator sets a password and types the first code; one definer call
// consumes the token, writes the credentials and opens the first session.

// ---------------------------------------------------------------- the token --

// EnrollmentTokenHash is THE enrollment-token hash, the one definition both ends use:
// lower-case hex of a KEYLESS SHA-256 over the token's UTF-8 text exactly as it
// travels in the link. It is what 00026 compares --
// `encode(sha256(convert_to(p_token, 'UTF8')), 'hex')` -- and what cmd/opadmin (OP-9)
// writes into platform_admins.enroll_token_hash. Keyless on purpose (ADR 0020 §3):
// the CLI holds no server key, and a 256-bit random input has no preimage to search.
//
// It hashes the TEXT, not decoded bytes, so the map link-text -> hash is the
// database's map; TestEnrollmentTokenHash_IsTheDatabasesHash pins the two against
// each other on the live server, non-ASCII included.
func EnrollmentTokenHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// EnrollmentToken is a freshly minted enrollment token, for cmd/opadmin: 256 random
// bits as 43 characters of base64url -- the same shape as a session token, so the same
// shape gate refuses anything else before the database sees it. Redacted like the
// session token (measured on the leak test's matrix); the raw value leaves only
// through RevealForLink.
type EnrollmentToken struct{ v *string }

const enrollRedacted = "operatorauth.EnrollmentToken(redacted)"

var (
	_ fmt.Formatter          = EnrollmentToken{}
	_ fmt.Stringer           = EnrollmentToken{}
	_ fmt.GoStringer         = EnrollmentToken{}
	_ slog.LogValuer         = EnrollmentToken{}
	_ encoding.TextMarshaler = EnrollmentToken{}
)

// NewEnrollmentToken mints one.
func NewEnrollmentToken() (EnrollmentToken, error) {
	v, err := randomToken()
	if err != nil {
		return EnrollmentToken{}, err
	}
	return EnrollmentToken{v: &v}, nil
}

// Hash is EnrollmentTokenHash of this token -- the value the SQL writes.
func (t EnrollmentToken) Hash() string { return EnrollmentTokenHash(t.RevealForLink()) }

// RevealForLink is the raw token, for the one place it may go: the enrollment link,
// printed once (ADR 0020 §6; NOT in a query string -- the link's shape is OP-9's).
func (t EnrollmentToken) RevealForLink() string {
	if t.v == nil {
		return ""
	}
	return *t.v
}

func (EnrollmentToken) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte(enrollRedacted)) }
func (EnrollmentToken) String() string               { return enrollRedacted }
func (EnrollmentToken) GoString() string             { return enrollRedacted }
func (EnrollmentToken) LogValue() slog.Value         { return slog.StringValue(enrollRedacted) }
func (EnrollmentToken) MarshalText() ([]byte, error) { return []byte(enrollRedacted), nil }

// ------------------------------------------------ the secret between two requests --

// WHERE THE PLAIN TOTP SECRET LIVES BETWEEN "SHOW" AND "CONFIRM" (ADR 0021 "Karar
// verilmedi": process memory, or an encrypted and authenticated short-lived value in
// the browser; the ADR warned that memory keyed by an unauthenticated GET is a memory
// allocation primitive and must then be capped in count and age).
//
// DECIDED: NOWHERE ON THE SERVER. BeginEnrollment seals the secret it generates into a
// PENDING BLOB the page carries in a hidden form field, and CompleteEnrollment opens it.
// The server holds the plaintext only inside each of the two requests. Consequences,
// each deliberate:
//
//   - No server state, so no count and no age to cap: the GET costs a crypto/rand read
//     and one AES-GCM seal and allocates nothing that outlives it. The capacity
//     question the ADR attached to the memory option does not arise.
//   - The form a person submits is the page they saw: two tabs, a reload, a back
//     button each carry their own secret, instead of the last GET silently replacing
//     the secret an app already scanned.
//   - It survives a process restart between the two requests.
//
// THE BLOB IS NOT THE STORED ENVELOPE, and must not be able to become it. Its AAD is
// pendingLabel || account id || expiry, not the id alone (ADR 0020 §1 md.2's stored
// AAD). So a blob copied into platform_admins.totp_secret_sealed does not open at
// sign-in, and a stored envelope presented as a blob does not open at enrollment
// (TestPendingBlob_IsNotTheStoredEnvelope, both directions). The secret reaches the
// database only after the first code was verified, re-sealed under the stored AAD.
//
// Its expiry is authenticated (it is in the AAD) and carried in the clear in front of
// the envelope so the AAD can be rebuilt. The lifetime is ADR 0020 §3's enrollment
// link TTL: a page is never worth more than the link that opened it, and the DATABASE
// enforces the link's own expiry independently (op_complete_enrollment, wall clock).
const (
	pendingTTL   = 30 * time.Minute
	pendingLabel = "taptime/operator/enrollment-pending/v1\x00"
	// pendingKeyLabel derives the key a pending blob is sealed under FROM the TOTP KEK
	// (challengeKeyLabel's pattern, inside the KEK's own family): BeginEnrollment is
	// reachable without any credential, and sealing under the stored envelopes' own KEK
	// would make it an encryption oracle for that key (OP-6 12c, the security audit's
	// LOW finding). The stored envelope stays under the KEK itself (ADR 0020 §1).
	pendingKeyLabel = "taptime/operator/enrollment-pending/v1/key-derivation"
	pendingExpOff   = 8
	pendingRedacted = "operatorauth.PendingBlob(redacted)"
)

// PendingBlob is the sealed in-flight secret, for the enrollment form's hidden field.
// It is an envelope (ADR 0020 §5's never-log list) and is redacted like one.
type PendingBlob struct{ v *string }

var (
	_ fmt.Formatter          = PendingBlob{}
	_ fmt.Stringer           = PendingBlob{}
	_ fmt.GoStringer         = PendingBlob{}
	_ slog.LogValuer         = PendingBlob{}
	_ encoding.TextMarshaler = PendingBlob{}
)

// PendingBlobFromForm wraps the value a form posted.
func PendingBlobFromForm(v string) PendingBlob { return PendingBlob{v: &v} }

// RevealForForm is the blob's text for the hidden field -- its one egress.
func (b PendingBlob) RevealForForm() string {
	if b.v == nil {
		return ""
	}
	return *b.v
}

func (PendingBlob) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte(pendingRedacted)) }
func (PendingBlob) String() string               { return pendingRedacted }
func (PendingBlob) GoString() string             { return pendingRedacted }
func (PendingBlob) LogValue() slog.Value         { return slog.StringValue(pendingRedacted) }
func (PendingBlob) MarshalText() ([]byte, error) { return []byte(pendingRedacted), nil }

// Pending is what the enrollment screen renders: the secret (Base32 / URI for display;
// Zero it once rendered) and the blob for the form.
type Pending struct {
	Secret Secret
	Blob   PendingBlob
}

var errPendingBlob = errors.New("operatorauth: enrollment page is not valid")

func derivePendingKey(kek []byte) []byte {
	m := hmac.New(sha256.New, kek)
	_, _ = m.Write([]byte(pendingKeyLabel)) // hash writes never fail (documented)
	return m.Sum(nil)
}

func pendingAAD(id uuid.UUID, expiry int64) []byte {
	aad := make([]byte, 0, len(pendingLabel)+16+8)
	aad = append(aad, pendingLabel...)
	aad = append(aad, id[:]...)
	return binary.BigEndian.AppendUint64(aad, uint64(expiry))
}

// BeginEnrollment generates a secret for account id and seals it into a pending blob.
// It reads no database and writes nothing: tappa_operator can see neither a pending
// account nor its token hash (00026), so whether the link is genuine is decided at
// CompleteEnrollment, by op_complete_enrollment, and only there.
func (a *Authenticator) BeginEnrollment(id uuid.UUID) (Pending, error) {
	if id == uuid.Nil {
		return Pending{}, errPendingBlob
	}
	sec, err := NewSecret()
	if err != nil {
		return Pending{}, err
	}
	exp := a.now().Add(pendingTTL).Unix()
	// reveal hands out a COPY of the secret; the copy is wiped here (the Key copy is not:
	// the KEK lives for the process -- key.go).
	pt := sec.reveal()
	defer sun.Zero(pt)
	env, err := sun.Seal(a.keys.pendingKey.bytes(), pendingAAD(id, exp), pt)
	if err != nil {
		sec.Zero()
		return Pending{}, errors.New("operatorauth: sealing the enrollment page failed")
	}
	raw := binary.BigEndian.AppendUint64(make([]byte, 0, pendingExpOff+len(env)), uint64(exp))
	raw = append(raw, env...)
	return Pending{Secret: sec, Blob: PendingBlobFromForm(base64.RawURLEncoding.EncodeToString(raw))}, nil
}

// openPending returns the secret a blob holds for account id, or errPendingBlob for an
// expired, foreign, tampered or malformed blob -- one error for all four. The caller
// wipes the returned bytes.
func (a *Authenticator) openPending(id uuid.UUID, b PendingBlob) ([]byte, error) {
	v := b.RevealForForm()
	if v == "" || len(v) > 512 {
		return nil, errPendingBlob
	}
	raw, err := strictB64.DecodeString(v)
	if err != nil || len(raw) <= pendingExpOff+sun.SealOverhead {
		return nil, errPendingBlob
	}
	exp := int64(binary.BigEndian.Uint64(raw[:pendingExpOff]))
	key, err := sun.Open(a.keys.pendingKey.bytes(), pendingAAD(id, exp), raw[pendingExpOff:])
	if err != nil {
		return nil, errPendingBlob
	}
	// Checked AFTER authentication: an unauthenticated expiry is not a fact.
	if a.now().Unix() >= exp || len(key) != SecretBytes {
		sun.Zero(key)
		return nil, errPendingBlob
	}
	return key, nil
}
