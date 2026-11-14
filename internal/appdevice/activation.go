package appdevice

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"

	"github.com/google/uuid"
)

// THE APP'S ACTIVATION, the key half (ADR 0045 §2.3; M16-07). Two steps, ADR 0026's shape:
//
//	bind      POST /app/v1/activate/bind: the invite code, the consent and the PUBLIC key. The
//	          server draws an ActivationChallenge -- an activation id and a challenge, both its
//	          own randomness -- and records on the invitation a consent binding derived from the
//	          challenge, the id and the key (ActivationBinding). It spends nothing and creates no
//	          device: "a bound but not active device" is an invitation carrying a binding (00104's
//	          header). The challenge goes back to the phone and nowhere else.
//	complete  POST /app/v1/activate/complete: a request signed by the BOUND key in
//	          SignedRequest's own format, with the activation id in App-Device-Id and the
//	          challenge in App-Nonce. VerifyActivation checks the signature under the key the
//	          body names and returns the binding that key yields; the invitation's consuming
//	          statement matches only that binding (invite.Manager.ActivateDevice).
//
// WHY THIS IS THE PROOF OF POSSESSION THE M16-03 REVIEW ASKED FOR: CreateAppDevice runs only
// after a signature by the key over a text that carries a SERVER-ISSUED challenge -- a value the
// signer could not have chosen, which the invitation commits to before the signature exists. A
// caller holding somebody else's public key (it is public: it travels in every bind) cannot make
// that signature, so it cannot register that key and lock its owner out through
// app_devices_key_digest_key's global UNIQUE.
//
// WHY NO NONCE ROW AND NO DEVICE ROW BEFORE THE END: a nonce is bound to an existing device
// (00104), and the device is born at the end, ACTIVE. The challenge's single use is the
// invitation's: the consuming statement spends the binding with the code, once -- and the
// tap's atomic counter (sun.Verify, §4.4) is the replay guard of the request itself. A second
// bind moves the binding (ADR 0026 "Güvenlik değerlendirmesi"), so the earlier challenge then
// completes nothing.

// bindingLabel separates this derivation from every other hash in the product.
const bindingLabel = "tappa-app-activation-bind-v1"

// ActivationChallenge is what bind hands the phone: the id it puts in App-Device-Id and the
// challenge it puts in App-Nonce when it completes. Both are this server's randomness.
//
// The Challenge is a Nonce, so it prints redacted on every path; Wire() is its one egress, the
// bind response. The id is not a device id -- no device exists yet -- and is never stored: it is
// committed to inside the binding and comes back in the signed text.
type ActivationChallenge struct {
	ID        uuid.UUID
	Challenge Nonce
}

// NewActivationChallenge draws a fresh challenge: 32 bytes of crypto/rand (newNonce) and a
// version-4 id.
func NewActivationChallenge() (ActivationChallenge, error) {
	n, err := newNonce()
	if err != nil {
		return ActivationChallenge{}, err
	}
	id, err := uuid.NewRandom()
	if err != nil {
		return ActivationChallenge{}, err
	}
	return ActivationChallenge{ID: id, Challenge: n}, nil
}

// ActivationBinding is the consent binding the invitation stores for an app bind: the unpadded
// base64url SHA-256 of the label, the activation id, the challenge and the key's hex, each line
// ended by LF -- 43 characters, the shape invite.Binding requires. It is a bearer-strength value
// (the challenge is 256 bits of the server's), so it is handed straight to invite.ParseBinding
// and printed nowhere. The key is validated on the curve first: a bind cannot commit to a key
// that could never sign (ErrInvalidPublicKey).
func ActivationBinding(c ActivationChallenge, publicKey []byte) (string, error) {
	if c.ID == uuid.Nil || c.Challenge.w == nil {
		return "", ErrMalformed
	}
	if _, err := ParsePublicKey(publicKey); err != nil {
		return "", err
	}
	return bindingOf(c.ID, c.Challenge.wire(), publicKey), nil
}

func bindingOf(id uuid.UUID, challengeWire string, publicKey []byte) string {
	h := sha256.New()
	for _, field := range []string{bindingLabel, id.String(), challengeWire, hex.EncodeToString(publicKey)} {
		_, _ = h.Write([]byte(field)) // hash writes never fail (documented)
		_, _ = h.Write([]byte{'\n'})
	}
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// VerifyActivation checks the complete request's proof of possession -- PURE, no database: the
// key the body names is a P-256 point, and the request's signature verifies under it over the
// request's canonical text (App-Device-Id = the activation id, App-Nonce = the challenge). It
// returns the binding that key and that challenge yield, which only the bind that issued the
// challenge for that key recorded.
//
// Refusals: ErrMalformed (a zero request), ErrInvalidPublicKey, ErrBadSignature. None names a
// value. A good signature proves the KEY; whether the binding is the invitation's is the
// consuming statement's question, not this function's.
func VerifyActivation(req SignedRequest, publicKey []byte) (string, error) {
	r := req.get()
	if r == nil {
		return "", ErrMalformed
	}
	pub, err := ParsePublicKey(publicKey)
	if err != nil {
		return "", err
	}
	if !verifySignature(pub, req.canonicalText(), r.signature) {
		return "", ErrBadSignature
	}
	if r.nonce.w == nil {
		return "", errors.New("appdevice: a parsed request has no challenge")
	}
	return bindingOf(r.deviceID, r.nonce.wire(), publicKey), nil
}
