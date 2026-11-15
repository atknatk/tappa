// Package oauth is the "Connected apps" authorization server's core (issue #37, ADR 0047): the
// shape, minting and keyed hash of its four secret values, PKCE, the client's HTTP Basic credentials,
// the closed scope set, the redirect URI rule, the signed pending request of the consent flow and the
// database work of the consent, the code exchange, the refresh rotation and the revocation
// (server.go). The HTTP surfaces that string these together are internal/handler (the consent page,
// GET and POST /admin/oauth/authorize) and internal/handler/oauthapi (POST /oauth/token and
// /oauth/revoke).
//
// 🔴 THIS PACKAGE LOGS NO SECRET AND RETURNS NONE IN AN ERROR (ADR 0047 §7.4; CLAUDE.md §7). A client
// secret, an authorization code, an access or refresh token, a PKCE verifier, a pending request
// cookie, the consent page's request digest and every hash of them travel as Value or Digest, both of
// which print a placeholder through every rendering path; the errors are fixed sentinels. A log line
// here carries a client id, a grant id, a token id, a tenant id and a fixed reason at most.
//
// THE SHAPE (ADR 0047 §7.1, the API key's K-10): tt_<kind>_<env>_ and 43 characters of canonical
// unpadded base64url -- 32 bytes of crypto/rand. One build accepts one environment word (build_*.go),
// and a value of the other build's word, or of another kind, is refused before the database.
//
// THE STORED VALUE (§7.2) is the hex HMAC-SHA256 of a per-kind domain label and the whole value under
// TAPPA_OAUTH_HMAC_KEY -- a key of its own (internal/config refuses it equal to any other key).
package oauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/atknatk/tappa/internal/config"
)

// Kind is one of the four secret values (ADR 0047 §7.1). The word is the one inside the prefix.
type Kind string

const (
	// KindSecret is a client secret: shown to the operator once (§10.2).
	KindSecret Kind = "cs"
	// KindCode is an authorization code: 60 seconds, one use (§7.3).
	KindCode Kind = "ac"
	// KindAccess is an access token: one hour, the /v1 Bearer (§8).
	KindAccess Kind = "at"
	// KindRefresh is a refresh token: rotated on every use, 2160 hours idle (§5.3).
	KindRefresh Kind = "rt"
)

// kinds is the closed set, in the order the R7d prefix lists them.
var kinds = []Kind{KindSecret, KindCode, KindAccess, KindRefresh}

const (
	// randomBytes is the entropy behind one value: 256 bits. Nothing here keeps a lockout count,
	// which is sound only because a value cannot be guessed.
	randomBytes = 32
	// bodyLen is randomBytes in unpadded base64url.
	bodyLen = 43
	// hintLen is how much of a client secret's body oauth_clients.secret_hint keeps (the API key's
	// prefix_hint: 24 of 256 bits).
	hintLen = 4
	// hmacKeyLen is TAPPA_OAUTH_HMAC_KEY's size; config.Load enforces it and NewHasher re-checks.
	hmacKeyLen = 32

	redacted       = "oauth.Value(redacted)"
	redactedDigest = "oauth.Digest(redacted)"
)

// The domain labels (ADR 0047 §7.2), one per use of the key. Each ends in a byte base64url cannot
// hold, so label||value is unambiguous. Changing one invalidates every stored hash of its kind.
const (
	labelSecret        = "tappa/oauth/secret/v1|"
	labelCode          = "tappa/oauth/code/v1|"
	labelAccess        = "tappa/oauth/access/v1|"
	labelRefresh       = "tappa/oauth/refresh/v1|"
	labelRequest       = "tappa/oauth/request/v1|"
	labelRequestDigest = "tappa/oauth/request-digest/v1|"
)

func labelOf(k Kind) string {
	switch k {
	case KindSecret:
		return labelSecret
	case KindCode:
		return labelCode
	case KindAccess:
		return labelAccess
	case KindRefresh:
		return labelRefresh
	}
	return ""
}

// strict refuses non-canonical trailing bits: two strings that decode to the same bytes would both be
// "the value", and only one of them was issued.
var strict = base64.RawURLEncoding.Strict()

// Prefix is kind's prefix in this build: tt_<kind>_live_ (production) or tt_<kind>_test_ (sandbox).
func Prefix(k Kind) string { return "tt_" + string(k) + "_" + Env + "_" }

// Value is one secret value in transit -- a client secret from Mint to the operator's one screen, a
// code from Mint to the consent's 303, a token from Mint to the token response, or any of them from a
// request to Check. Never stored; only its Digest is.
//
// The field is a POINTER for apikey.Token's reason: fmt reaches an unexported field by reflection,
// where the methods below cannot intervene, and a pointer prints an address there.
type Value struct{ v *string }

var (
	_ fmt.Formatter          = Value{}
	_ fmt.Stringer           = Value{}
	_ fmt.GoStringer         = Value{}
	_ slog.LogValuer         = Value{}
	_ encoding.TextMarshaler = Value{}
)

// ParseValue wraps an untrusted string -- a form field, a Basic credential -- in a Value. It does not
// validate; KindOf and Check do.
func ParseValue(v string) Value { return Value{v: &v} }

func (t Value) reveal() string {
	if t.v == nil {
		return ""
	}
	return *t.v
}

// Present reports whether the value is non-empty, without revealing it.
func (t Value) Present() bool { return t.reveal() != "" }

// RevealInTokenResponse is the access or refresh token for the ONE place it leaves the server: the
// token endpoint's JSON answer (ADR 0047 §7.2).
func (t Value) RevealInTokenResponse() string { return t.reveal() }

// RevealInRedirect is the authorization code for the ONE place it leaves the server: the consent's
// 303 to the registered redirect URI (§4.5).
func (t Value) RevealInRedirect() string { return t.reveal() }

// RevealToTheOperatorOnce is the client secret for the ONE place it is shown: the operator's register
// or rotate answer (§10.2 -- no "show again").
func (t Value) RevealToTheOperatorOnce() string { return t.reveal() }

// Format implements fmt.Formatter for every verb.
func (Value) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redacted)) }

// String implements fmt.Stringer.
func (Value) String() string { return redacted }

// GoString covers %#v.
func (Value) GoString() string { return redacted }

// LogValue implements slog.LogValuer.
func (Value) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalText implements encoding.TextMarshaler, which encoding/json prefers.
func (Value) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// Mint draws randomBytes from crypto/rand and returns a fresh value of kind k under this build's
// prefix. A crypto/rand failure is returned, never ignored.
func Mint(k Kind) (Value, error) {
	if labelOf(k) == "" {
		return Value{}, errors.New("oauth: mint: unknown kind")
	}
	b := make([]byte, randomBytes)
	if _, err := rand.Read(b); err != nil {
		return Value{}, fmt.Errorf("oauth: read randomness: %w", err)
	}
	return ParseValue(Prefix(k) + base64.RawURLEncoding.EncodeToString(b)), nil
}

// Is reports whether the value has kind k's shape in this build: the prefix and the canonical
// base64url of 32 bytes. No keyed work, no database -- junk costs nothing.
func (t Value) Is(k Kind) bool {
	v, p := t.reveal(), Prefix(k)
	if labelOf(k) == "" || len(v) != len(p)+bodyLen || !strings.HasPrefix(v, p) {
		return false
	}
	b, err := strict.DecodeString(v[len(p):])
	return err == nil && len(b) == randomBytes
}

// KindOf is the kind whose shape the value has in this build, or false (the revocation endpoint's
// "the prefix says which" -- ADR 0047 §6).
func (t Value) KindOf() (Kind, bool) {
	for _, k := range kinds {
		if t.Is(k) {
			return k, true
		}
	}
	return "", false
}

// SecretHint is oauth_clients.secret_hint for a client secret: its prefix and the first hintLen
// characters of its body (the CHECK ^tt_cs_(live|test)_[A-Za-z0-9_-]{4}$). "" for anything that is
// not a client secret of this build.
func (t Value) SecretHint() string {
	if !t.Is(KindSecret) {
		return ""
	}
	return t.reveal()[:len(Prefix(KindSecret))+hintLen]
}

// Digest is a value's keyed hash -- the resolvers' input, and as good as the value to anybody who can
// call a resolver -- so it prints a placeholder exactly as Value does.
type Digest struct{ h *string }

var (
	_ fmt.Formatter          = Digest{}
	_ fmt.Stringer           = Digest{}
	_ fmt.GoStringer         = Digest{}
	_ slog.LogValuer         = Digest{}
	_ encoding.TextMarshaler = Digest{}
)

func (d Digest) reveal() string {
	if d.h == nil {
		return ""
	}
	return *d.h
}

// RevealToTheDatabase is the hex hash for the one place it goes: a resolver's argument or an INSERT's
// column (oauth_clients.secret_hash through the operator's op_* -- internal/handler/operator).
func (d Digest) RevealToTheDatabase() string { return d.reveal() }

func (Digest) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte(redactedDigest)) }
func (Digest) String() string               { return redactedDigest }
func (Digest) GoString() string             { return redactedDigest }
func (Digest) LogValue() slog.Value         { return slog.StringValue(redactedDigest) }
func (Digest) MarshalText() ([]byte, error) { return []byte(redactedDigest), nil }

// Hasher holds TAPPA_OAUTH_HMAC_KEY and computes every keyed value of the authorization server.
type Hasher struct{ key config.Key }

// NewHasher refuses a key that is not 32 bytes: a hand-built value must not MAC under a short or
// empty key.
func NewHasher(key []byte) (*Hasher, error) {
	if len(key) != hmacKeyLen {
		return nil, fmt.Errorf("oauth: hmac key must be %d bytes, got %d", hmacKeyLen, len(key))
	}
	return &Hasher{key: config.NewKey(key)}, nil
}

// mac is HMAC-SHA256(key, label || v).
func (h *Hasher) mac(label, v string) []byte {
	m := hmac.New(sha256.New, h.key.Reveal())
	m.Write([]byte(label))
	m.Write([]byte(v))
	return m.Sum(nil)
}

// Hash is the stored hash of a value of kind k: hex, the ^[0-9a-f]{64}$ every hash column CHECKs.
// The caller has already checked the shape (Is); Hash does not, so a junk value hashes too and simply
// matches no row.
func (h *Hasher) Hash(k Kind, t Value) (Digest, error) {
	label := labelOf(k)
	if label == "" {
		return Digest{}, errors.New("oauth: hash: unknown kind")
	}
	s := hex.EncodeToString(h.mac(label, t.reveal()))
	return Digest{h: &s}, nil
}

// MintClientSecret is the operator's register and rotate (ADR 0047 §10.2): a fresh client secret and
// the hash oauth_clients.secret_hash stores. The secret is shown once by the caller; the hash goes to
// op_register_oauth_client / op_rotate_oauth_client_secret.
func (h *Hasher) MintClientSecret() (Value, Digest, error) {
	v, err := Mint(KindSecret)
	if err != nil {
		return Value{}, Digest{}, err
	}
	d, err := h.Hash(KindSecret, v)
	if err != nil {
		return Value{}, Digest{}, err
	}
	return v, d, nil
}
