package appdevice

// vectors_test.go -- the known-answer vectors of test/fixtures/appdevice_vectors.json, the
// contract the iOS and Android request builders share with this verifier (M16-05's card). The
// canonical texts were written with printf, the signatures made and checked by OpenSSL (the
// file's _provenance): so these tests hold the Go builder to bytes it did not produce, and the
// Go verifier to signatures it did not make.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

const vectorsPath = "../../test/fixtures/appdevice_vectors.json"

type vectorFile struct {
	Keys map[string]struct {
		PublicKeyHex string `json:"public_point_hex"`
	} `json:"keys"`
	Requests []struct {
		Name               string `json:"name"`
		Method             string `json:"method"`
		Path               string `json:"path"`
		BodyUTF8           string `json:"body_utf8"`
		BodySHA256Hex      string `json:"body_sha256_hex"`
		Nonce              string `json:"nonce"`
		DeviceID           string `json:"device_id"`
		CanonicalText      string `json:"canonical_text"`
		CanonicalSHA256Hex string `json:"canonical_sha256_hex"`
	} `json:"requests"`
	Signatures []struct {
		Name      string `json:"name"`
		Request   string `json:"request"`
		Key       string `json:"key"`
		Signature string `json:"signature_b64url"`
		Valid     bool   `json:"valid"`
	} `json:"signatures"`
	InvalidPublicKeys []struct {
		Name         string `json:"name"`
		PublicKeyHex string `json:"public_point_hex"`
	} `json:"invalid_public_keys"`
	MalformedRequests []struct {
		Name     string `json:"name"`
		Method   string `json:"method"`
		Path     string `json:"path"`
		Nonce    string `json:"nonce"`
		DeviceID string `json:"device_id"`
	} `json:"malformed_requests"`
}

func loadVectors(t *testing.T) vectorFile {
	t.Helper()
	raw, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var v vectorFile
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("parse vectors: %v", err)
	}
	// A positive control on the file itself: an emptied section would make every loop below pass.
	if len(v.Keys) < 2 || len(v.Requests) < 4 || len(v.Signatures) < 8 || len(v.InvalidPublicKeys) < 7 || len(v.MalformedRequests) < 12 {
		t.Fatalf("vectors file lost entries: %d keys, %d requests, %d signatures, %d bad keys, %d malformed",
			len(v.Keys), len(v.Requests), len(v.Signatures), len(v.InvalidPublicKeys), len(v.MalformedRequests))
	}
	return v
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("vector hex: %v", err)
	}
	return b
}

// TestCanonicalText_KnownAnswers: the builder reproduces every printf-written canonical text
// byte for byte, and the body digest and the text's digest agree with shasum's.
func TestCanonicalText_KnownAnswers(t *testing.T) {
	v := loadVectors(t)
	for _, r := range v.Requests {
		t.Run(r.Name, func(t *testing.T) {
			got, err := CanonicalText(r.Method, r.Path, []byte(r.BodyUTF8), r.Nonce, r.DeviceID)
			if err != nil {
				t.Fatalf("CanonicalText: %v", err)
			}
			if string(got) != r.CanonicalText {
				t.Fatalf("canonical text differs from the vector (%d bytes, want %d)", len(got), len(r.CanonicalText))
			}
			if sum := sha256.Sum256(got); hex.EncodeToString(sum[:]) != r.CanonicalSHA256Hex {
				t.Fatal("canonical text digest differs from the vector")
			}
			if sum := sha256.Sum256([]byte(r.BodyUTF8)); hex.EncodeToString(sum[:]) != r.BodySHA256Hex {
				t.Fatal("body digest differs from the vector")
			}
		})
	}
}

// TestVerifySignature_KnownAnswers: every OpenSSL signature verifies exactly when the vector
// says so -- the high-S one and its low-S twin included, the wrong key, the tampered body and
// the swapped text not.
func TestVerifySignature_KnownAnswers(t *testing.T) {
	v := loadVectors(t)
	texts := map[string]string{}
	for _, r := range v.Requests {
		texts[r.Name] = r.CanonicalText
	}
	valid, invalid := 0, 0
	for _, s := range v.Signatures {
		t.Run(s.Name, func(t *testing.T) {
			k, ok := v.Keys[s.Key]
			if !ok {
				t.Fatalf("vector names key %q that the file does not hold", s.Key)
			}
			text, ok := texts[s.Request]
			if !ok {
				t.Fatalf("vector names request %q that the file does not hold", s.Request)
			}
			pub, err := ParsePublicKey(mustHex(t, k.PublicKeyHex))
			if err != nil {
				t.Fatalf("ParsePublicKey: %v", err)
			}
			sig, err := parseSignature(s.Signature)
			if err != nil {
				t.Fatalf("parseSignature: %v", err)
			}
			if got := verifySignature(pub, []byte(text), sig); got != s.Valid {
				t.Fatalf("verify = %v, the vector says %v", got, s.Valid)
			}
		})
		if s.Valid {
			valid++
		} else {
			invalid++
		}
	}
	if valid == 0 || invalid == 0 {
		t.Fatalf("the vectors need both outcomes: %d valid, %d invalid", valid, invalid)
	}
}

// TestParsePublicKey_InvalidVectors: every key that is not an uncompressed point ON P-256 is
// refused -- the off-curve one passes 00104's CHECK (65 bytes, 0x04) and is the reason the
// curve check lives in Go. The two real keys parse (positive control).
func TestParsePublicKey_InvalidVectors(t *testing.T) {
	v := loadVectors(t)
	for name, k := range v.Keys {
		if _, err := ParsePublicKey(mustHex(t, k.PublicKeyHex)); err != nil {
			t.Fatalf("key %s: %v", name, err)
		}
	}
	for _, k := range v.InvalidPublicKeys {
		t.Run(k.Name, func(t *testing.T) {
			if _, err := ParsePublicKey(mustHex(t, k.PublicKeyHex)); !errors.Is(err, ErrInvalidPublicKey) {
				t.Fatalf("ParsePublicKey = %v, want ErrInvalidPublicKey", err)
			}
		})
	}
}

// TestCanonicalText_MalformedVectors: every non-canonical spelling of a part is refused by the
// builder AND by the request parser, before any database work.
func TestCanonicalText_MalformedVectors(t *testing.T) {
	v := loadVectors(t)
	validSig := v.Signatures[0].Signature
	for _, m := range v.MalformedRequests {
		t.Run(m.Name, func(t *testing.T) {
			if _, err := CanonicalText(m.Method, m.Path, nil, m.Nonce, m.DeviceID); !errors.Is(err, ErrMalformed) {
				t.Fatalf("CanonicalText = %v, want ErrMalformed", err)
			}
			if _, err := NewSignedRequest(m.Method, m.Path, nil, m.DeviceID, m.Nonce, validSig); !errors.Is(err, ErrMalformed) {
				t.Fatalf("NewSignedRequest = %v, want ErrMalformed", err)
			}
		})
	}
}
