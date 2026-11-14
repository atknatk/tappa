package appdevice

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"errors"
)

// PublicKeyLen is the stored device key's length: the raw uncompressed P-256 point,
// 0x04 || X || Y (SEC 1 §2.3.3; 00104's app_devices_public_key_shape CHECK).
const PublicKeyLen = 65

// ErrInvalidPublicKey is a device key that is not an uncompressed point ON P-256. It is a
// registration-time refusal (M16-07's activation must check before CreateAppDevice) and never
// names the bytes.
var ErrInvalidPublicKey = errors.New("appdevice: the device key is not an uncompressed P-256 point")

// ParsePublicKey validates a device key in its stored form and returns it ready to verify with.
//
// WHY GO AND NOT THE CHECK (M16-03 review): 00104's CHECK pins the length and the 0x04 prefix and
// cannot do curve arithmetic. ecdsa.ParseUncompressedPublicKey refuses a point off the curve,
// the point at infinity, a coordinate not below the field prime and any compressed or hybrid
// form -- so an invalid-curve key never reaches VerifyASN1. The activation calls this before the
// key is stored; Verify calls it again on every request, because the row is what the database
// says, not what activation checked.
func ParsePublicKey(raw []byte) (*ecdsa.PublicKey, error) {
	if len(raw) != PublicKeyLen || raw[0] != 0x04 {
		return nil, ErrInvalidPublicKey
	}
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), raw)
	if err != nil {
		return nil, ErrInvalidPublicKey
	}
	return pub, nil
}

// verifySignature is the whole cryptographic check, pure: ECDSA P-256 over SHA-256 of the
// canonical text, the signature in ASN.1 DER.
//
// HIGH-S IS ACCEPTED, deliberately. (r, s) and (r, n-s) both verify for the same message; neither
// Secure Enclave nor Android Keystore normalises to low S, so refusing high S would refuse about
// half of every genuine request. Malleability buys an attacker nothing here: replay protection is
// the single-use NONCE, consumed in one statement (§4.4's shape), not the uniqueness of the
// signature -- a high-S twin of a spent request is refused by the nonce
// (TestVerify_HighSTwinOfASpentRequestIsRefused).
//
// DER STRICTNESS is ecdsa.VerifyASN1's (measured: trailing bytes, a non-minimal INTEGER, a
// negative INTEGER, r or s zero or not below n -- all false; TestCheckSignature_MalformedDER).
func verifySignature(pub *ecdsa.PublicKey, text, sig []byte) bool {
	digest := sha256.Sum256(text)
	return ecdsa.VerifyASN1(pub, digest[:], sig)
}
