package liveqr

import (
	"crypto/rand"
	"encoding"
	"fmt"
	"log/slog"
	"strings"
)

// THE PAIRING CODE -- 13 characters of Crockford base32, 65 bits from crypto/rand.
//
// WHY 65 BITS AND NOT A SHORT CODE (SEÇİM, M16-20; the card's two options and the M16-19 handover).
// The pairing endpoint answers WITHOUT an identity and ACROSS TENANTS: resolve_live_qr_display_by_
// pairing_code_hash looks in every business's waiting screens. So what a guess wins is any waiting
// screen of any business, and what bounds guessing must hold against a caller with many addresses.
//
//	SHORT CODE (8 chars, 40 bits) + LIMITS. N waiting codes across every tenant, G the global
//	    budget per hour: P(hit per hour) = G·N / 2^40. With G = 600 and N = 100 that is 5.5e-5 an
//	    hour -- one expected hit in about two years -- and the global budget is itself a lever: an
//	    attacker who spends it locks every business out of pairing. A failed guess also has NO
//	    TENANT to write the audit row the card asks for: audit_log.tenant_id is NOT NULL (00005;
//	    internal/audit's Event), and an unknown code names nobody.
//	65 BITS. A botnet of 100 000 addresses, each at this package's per-address budget (20 failures
//	    per 15 minutes, internal/handler), makes 8e6 guesses an hour: 8e6 · 100 / 2^65 = 2e-11 an
//	    hour. No global budget is needed (none can be spent to lock others out), and the residue of
//	    the tenant-scoped sweep -- another tenant's expired codes kept in the global UNIQUE until
//	    that tenant acts (00111) -- collides with a fresh code with probability count / 2^65.
//
// THE TYPING COST, WEIGHED (screens may be a TV, a tablet or an old phone): 13 characters, shown
// in three groups (4-4-5), case-insensitive, with the look-alikes folded (O→0, I and L→1) and the
// hyphens and spaces ignored, so what a person can misread is accepted as what they meant. It is
// typed once per screen, at the screen, within PairingLifetime.
const (
	codeLen      = 13
	codeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ" // Crockford's: no I, L, O, U
)

// CodeBits is the code's entropy (codeLen × 5), for the measurement above and its test.
const CodeBits = codeLen * 5

// PairingCode is one pairing code in transit: between drawing it and the panel's one render of it,
// or between the screen's form and its digest. Never in the database (only CodeDigest is), never
// in a log.
//
// THE FIELD IS A POINTER and every printing path renders a placeholder: internal/session's Token
// shape (token.go there) and for its reason -- fmt prints an UNEXPORTED field of a caller's struct
// by reflection, past Stringer, and a pointer prints an address instead of the value.
type PairingCode struct{ v *string }

const redactedCode = "liveqr.PairingCode(redacted)"

var (
	_ fmt.Formatter          = PairingCode{}
	_ fmt.Stringer           = PairingCode{}
	_ fmt.GoStringer         = PairingCode{}
	_ slog.LogValuer         = PairingCode{}
	_ encoding.TextMarshaler = PairingCode{}
)

func (PairingCode) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte(redactedCode)) }
func (PairingCode) String() string               { return redactedCode }
func (PairingCode) GoString() string             { return redactedCode }
func (PairingCode) LogValue() slog.Value         { return slog.StringValue(redactedCode) }
func (PairingCode) MarshalText() ([]byte, error) { return []byte(redactedCode), nil }

// IsZero reports a code that holds nothing (the zero value, or a refused parse).
func (c PairingCode) IsZero() bool { return c.v == nil }

func (c PairingCode) reveal() string {
	if c.v == nil {
		return ""
	}
	return *c.v
}

// ForThePanelOnce is the code as the manager reads it, in its three groups ("7KQ2-M9XD-4TRWZ").
// It is the ONE egress of the value: the panel page that answers "Add a live QR screen" renders it
// and nothing else may (the name is the review hook). "" for the zero code.
func (c PairingCode) ForThePanelOnce() string {
	v := c.reveal()
	if len(v) != codeLen {
		return ""
	}
	return v[:4] + "-" + v[4:8] + "-" + v[8:]
}

// NewPairingCode draws a code: codeLen bytes of crypto/rand, each reduced to the alphabet with
// b & 31 -- exact, because 256 is a multiple of 32, so every symbol is equally likely. A
// randomness failure is returned, never papered over (§7).
func NewPairingCode() (PairingCode, error) {
	b := make([]byte, codeLen)
	if _, err := rand.Read(b); err != nil {
		return PairingCode{}, fmt.Errorf("liveqr: read randomness: %w", err)
	}
	out := make([]byte, codeLen)
	for i, x := range b {
		out[i] = codeAlphabet[x&31]
	}
	s := string(out)
	return PairingCode{v: &s}, nil
}

// ParsePairingCode reads what a person typed: case folded, spaces and hyphens dropped, the
// look-alikes O→0 and I, L→1 folded (Crockford's rule), anything else outside the alphabet refused.
// ok is false for anything that is not exactly codeLen symbols afterwards -- the caller answers one
// sentence for every refusal and never echoes the input.
func ParsePairingCode(typed string) (PairingCode, bool) {
	if len(typed) > 4*codeLen { // a bound before any work; real input is 13 to 15 bytes
		return PairingCode{}, false
	}
	var b strings.Builder
	for _, r := range strings.ToUpper(typed) {
		switch {
		case r == ' ' || r == '-' || r == '\t':
			continue
		case r == 'O':
			r = '0'
		case r == 'I' || r == 'L':
			r = '1'
		}
		if !strings.ContainsRune(codeAlphabet, r) {
			return PairingCode{}, false
		}
		b.WriteRune(r)
	}
	if b.Len() != codeLen {
		return PairingCode{}, false
	}
	s := b.String()
	return PairingCode{v: &s}, true
}

// CodeDigest is the value stored in live_qr_displays.pairing_code_hash: hex HMAC-SHA256 of the
// canonical code under the code key. Two spellings of one code ("7kq2 m9xd…" and "7KQ2-M9XD-…")
// have one digest, because ParsePairingCode canonicalises before this runs.
func (k *Keys) CodeDigest(c PairingCode) (string, error) {
	if k == nil || c.IsZero() {
		return "", errNoValue
	}
	return digest(k.code, c.reveal())
}

// errNoValue refuses a digest of nothing (a zero code or session, or zero Keys).
var errNoValue = fmt.Errorf("liveqr: nothing to digest")
