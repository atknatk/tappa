package operatorauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// TOTP, standard library only (ADR 0020 §3): RFC 6238 over RFC 4226, HMAC-SHA1, six
// digits, a thirty-second step, one step of tolerance on each side. No QR library either:
// the enrollment screen draws the otpauth:// URI's QR code with internal/qrcode, written in
// this repository (ADR 0020 §3, QR note), and shows the base32 secret and the URI as text.
//
// KNOWN-ANSWER PROOF, NOT SELF-CONSISTENCY. A generator and a verifier written from
// the same misreading of the truncation or the counter's byte order agree with each
// other and with nobody else (agent-brief.md: "iç-tutarlı golden vektör byte-sırası
// hatasını yakalayamaz"). totp_test.go therefore pins the PUBLISHED tables: RFC 4226
// Appendix D (six-digit HOTP, counters 0-9) and all eighteen rows of RFC 6238 Appendix
// B (eight digits; SHA-1, SHA-256 and SHA-512 with the three seeds the RFC's errata
// gives), including the step value T the RFC prints for each time.

const (
	// Digits, PeriodSeconds and SkewSteps are ADR 0020 §3's numbers. The ±1 step is
	// also what op_open_session binds the step to (`cur - 1 .. cur + 1` by the
	// DATABASE's clock, 00026), so Go and the database accept the same window when
	// their clocks agree -- and refuse, fail-closed, when they drift by more than a
	// step (ADR 0021 sınır 13).
	Digits        = 6
	PeriodSeconds = 30
	SkewSteps     = 1

	// SecretBytes is 160 bits: RFC 4226 §4's recommended length and ADR 0020 §1 md.3.
	// Sealed, it is sun.SealOverhead + 20 = 48 bytes, above 00026's floor of 44.
	SecretBytes = 20
)

// b32 is RFC 4648 base32, standard alphabet, WITHOUT padding -- the form
// authenticator apps accept in an otpauth:// URI. 160 bits are exactly 32 characters,
// so padding would never appear anyway; NoPadding makes that a rule, not an accident.
var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// Secret is an operator's TOTP secret in the clear: between NewSecret and the seal at
// enrollment, or between sun.Open and the code check at sign-in. Never longer, never
// in the database (only its envelope is), never in a log.
//
// The redaction mechanisms are internal/session's two, for the reasons measured there:
// the five printing interfaces, and the bytes behind a *string, so that fmt's
// fall-through for a value in a caller's unexported struct field prints an address.
// A *string, not a *[]byte (OP-6 verification, 7th round, the 6th auditor measured):
// for a verb a pointer does not take (%s %q %e %f %t %c %U) fmt's badVerb opens the
// pointer once, and a *[]byte opens to the secret's bytes -- the first version printed
// the plain TOTP secret that way; a pointer to a string is not opened.
type Secret struct{ b *string }

const totpRedacted = "operatorauth.Secret(redacted)"

var (
	_ fmt.Formatter          = Secret{}
	_ fmt.Stringer           = Secret{}
	_ fmt.GoStringer         = Secret{}
	_ slog.LogValuer         = Secret{}
	_ encoding.TextMarshaler = Secret{}
)

// NewSecret draws SecretBytes from crypto/rand. A randomness failure is returned,
// never ignored (§7).
func NewSecret() (Secret, error) {
	b := make([]byte, SecretBytes)
	if _, err := rand.Read(b); err != nil {
		return Secret{}, errors.New("operatorauth: read randomness for a TOTP key failed")
	}
	v := string(b)
	clear(b)
	return Secret{b: &v}, nil
}

// reveal returns a COPY of the raw bytes for one use, or nil. Unexported: its callers
// are the seal at enrollment and the display methods below.
func (s Secret) reveal() []byte {
	if s.b == nil || *s.b == "" {
		return nil
	}
	return []byte(*s.b)
}

// Base32 is the secret as the enrollment screen shows it, for typing into an
// authenticator app. It is the plaintext -- that is its purpose -- so it goes into a
// no-store page and nowhere else (OP-8).
func (s Secret) Base32() string {
	pt := s.reveal()
	defer clear(pt) // the copy reveal handed out; the string returned is the display form
	return b32.EncodeToString(pt)
}

// URI is the Key Uri Format an authenticator app imports: otpauth://totp/ISSUER:ACCOUNT
// with the secret, issuer, algorithm, digits and period as query parameters. Like
// Base32 it carries the plaintext and is for the enrollment screen only. Spaces are
// %20 rather than '+', which not every app decodes.
func (s Secret) URI(issuer, account string) string {
	esc := func(v string) string { return strings.ReplaceAll(url.QueryEscape(v), "+", "%20") }
	return "otpauth://totp/" + esc(issuer) + ":" + esc(account) +
		"?secret=" + s.Base32() +
		"&issuer=" + esc(issuer) +
		"&algorithm=SHA1&digits=" + strconv.Itoa(Digits) +
		"&period=" + strconv.Itoa(PeriodSeconds)
}

// Zero FORGETS the secret: afterwards this value (and every copy of it) reveals
// nothing, and Base32 and URI are empty. It cannot WIPE the bytes -- they are a Go
// string, which is immutable -- and it never could have ended the plaintext's life in
// memory: the display path puts it into immutable strings anyway (Base32, URI, the
// rendered page), which the old wipe of the raw bytes never touched. The secret that is
// WIPED is the one opened at sign-in and at enrollment -- a local []byte, sun.Zero'd,
// never a Secret.
func (s Secret) Zero() {
	if s.b != nil {
		*s.b = ""
	}
}

func (Secret) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte(totpRedacted)) }
func (Secret) String() string               { return totpRedacted }
func (Secret) GoString() string             { return totpRedacted }
func (Secret) LogValue() slog.Value         { return slog.StringValue(totpRedacted) }
func (Secret) MarshalText() ([]byte, error) { return []byte(totpRedacted), nil }

// step is RFC 6238's T: whole PeriodSeconds since the Unix epoch, floored (a time
// before the epoch floors downward rather than toward zero).
func step(t time.Time) int64 {
	u := t.Unix()
	s := u / PeriodSeconds
	if u < 0 && u%PeriodSeconds != 0 {
		s--
	}
	return s
}

// hotp writes RFC 4226's HOTP value for counter into out (len(out) digits, ASCII,
// zero-padded), with h as the HMAC hash: HMAC(key, counter as 8 bytes BIG-endian),
// dynamic truncation (§5.3: offset = low nibble of the last byte, 31 bits from there,
// high bit masked), modulo 10^digits. The hash is a parameter only so RFC 6238's
// SHA-256 and SHA-512 rows can be checked; production passes sha1.New.
func hotp(h func() hash.Hash, key []byte, counter uint64, out []byte) {
	var c [8]byte
	binary.BigEndian.PutUint64(c[:], counter)
	m := hmac.New(h, key)
	_, _ = m.Write(c[:]) // hash writes never fail (documented)
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := uint64(sum[off]&0x7f)<<24 | uint64(sum[off+1])<<16 | uint64(sum[off+2])<<8 | uint64(sum[off+3])
	clear(sum)
	for i := len(out) - 1; i >= 0; i-- {
		out[i] = byte('0' + bin%10)
		bin /= 10
	}
}

// normalizeCode accepts what a person types: six ASCII digits, with spaces anywhere
// (apps display "123 456"). Anything else is not a code, and is refused before any
// HMAC runs -- the length and the alphabet are public, so this early exit is not a
// timing channel.
func normalizeCode(code string) ([Digits]byte, bool) {
	var out [Digits]byte
	n := 0
	for i := 0; i < len(code); i++ {
		c := code[i]
		switch {
		case c == ' ':
			continue
		case c >= '0' && c <= '9' && n < Digits:
			out[n] = c
			n++
		default:
			return [Digits]byte{}, false
		}
	}
	return out, n == Digits
}

// verifyCode checks code against the secret at now and returns the STEP it was
// accepted for -- the value op_open_session and op_complete_enrollment receive and
// store as totp_last_step (ADR 0020 §1: the replay guard is that step, strictly
// increasing, in the database).
//
// THE WINDOW IS WALKED WHOLE. All 2*SkewSteps+1 candidates are computed and compared
// every time, each comparison constant-time over the six digits, and nothing leaves
// the loop early -- so the time does not say which step matched or how many digits of
// a candidate were right (TestVerifyCode_TheWindowIsWalkedWholeInConstantTime reads
// this function's own source for it).
//
// IF TWO STEPS MATCH (one chance in a million per pair), THE LATER ONE IS ACCEPTED:
// the database then stores the larger step, which retires BOTH -- accepting the
// earlier would leave the same six digits valid once more at the later step.
func verifyCode(key []byte, code string, now time.Time) (int64, bool) {
	in, ok := normalizeCode(code)
	if !ok {
		return 0, false
	}
	cur := step(now)
	var (
		accepted int64
		matched  bool
		want     [Digits]byte
	)
	for d := int64(-SkewSteps); d <= SkewSteps; d++ {
		s := cur + d
		hotp(sha1.New, key, uint64(s), want[:])
		hit := subtle.ConstantTimeCompare(want[:], in[:]) == 1
		if hit {
			accepted, matched = s, true
		}
	}
	clear(want[:])
	clear(in[:])
	return accepted, matched
}
