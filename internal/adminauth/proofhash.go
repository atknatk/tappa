package adminauth

import (
	"encoding"
	"fmt"
	"log/slog"
)

// proofHash is a token's keyed hash that an identity function takes as PROOF: a registration draft's
// (identity_claim_signup), a legacy session's (identity_link_legacy). The definer finds its row by this
// value alone, so for anybody who can call it the hash is as good as the token -- apikey.Checked's
// reason, and its shape: one *string field and the five printing methods, so a ClaimProof, a
// SignupClaim or a proofSession printed whole, or held in a caller's unexported field, prints the
// placeholder (backlog T209: the T205 gate's name pattern had no `hash`, and these three fields were
// plain strings it could not see).
type proofHash struct{ v *string }

const proofHashRedacted = "adminauth.proofHash(redacted)"

var (
	_ fmt.Formatter          = proofHash{}
	_ fmt.Stringer           = proofHash{}
	_ fmt.GoStringer         = proofHash{}
	_ slog.LogValuer         = proofHash{}
	_ encoding.TextMarshaler = proofHash{}
)

// newProofHash wraps h; "" is the zero proofHash, which proves nothing.
func newProofHash(h string) proofHash {
	if h == "" {
		return proofHash{}
	}
	return proofHash{v: &h}
}

// reveal is the hash for the one call that takes it, "" for the zero value.
func (p proofHash) reveal() string {
	if p.v == nil {
		return ""
	}
	return *p.v
}

func (proofHash) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte(proofHashRedacted)) }
func (proofHash) String() string               { return proofHashRedacted }
func (proofHash) GoString() string             { return proofHashRedacted }
func (proofHash) LogValue() slog.Value         { return slog.StringValue(proofHashRedacted) }
func (proofHash) MarshalText() ([]byte, error) { return []byte(proofHashRedacted), nil }
