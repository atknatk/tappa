package operatorauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
)

// The LOGIN CHALLENGE: what the password step leaves behind for the TOTP step (ADR
// 0020 §3.1 -- "kısa ömürlü, imzalı bir ara çerez; yalnız TOTP adımına izin verir").
// The ADR left its lifetime and its key open (ADR 0020 "Karar verilmedi"); both are
// decided here and argued below.
//
// WHAT IT IS. A value the SERVER signed saying "the password of operator X verified at
// time T". It carries no session, opens no route, and the only function that accepts it
// is Authenticator.TOTP -- the session gate hashes a cookie value and asks the
// database, where a challenge matches nothing. It is a BEARER value for TOTP attempts
// on one account: whoever holds it may try codes, and every try is spent against that
// account's budget BEFORE the code is checked (limits.go) and counted by the
// database's lock.
//
// LAYOUT, FIXED LENGTH (so the MAC input is unambiguous without separators):
//
//	payload = version(1) || operator id(16) || issued at, unix seconds, big-endian(8) || nonce(16)
//	value   = base64url(payload) "." base64url(HMAC-SHA256(challengeKey, challengeMACLabel || payload))
//
// The nonce makes two challenges for the same operator in the same second distinct;
// it is not a use count (no server state -- see "not single-use" below).

const (
	challengeVersion = 1
	challengeIDOff   = 1
	challengeTimeOff = challengeIDOff + 16
	challengeNonce   = challengeTimeOff + 8
	challengeLen     = challengeNonce + 16

	// challengeTTL is FIVE MINUTES: one authenticator code is on screen for at most
	// PeriodSeconds, the window accepts one step either side, and a person needs time
	// to unlock a phone and type six digits. internal/handler's panel login choice
	// (adminChoiceTTL) is the same five minutes for the same kind of value -- a
	// post-password bearer credential -- and the reasoning there applies. What a
	// longer life would buy an attacker holding the value is NOT more guesses: those
	// are bounded by the account budget and the database lock, not by this TTL.
	challengeTTL = 5 * time.Minute

	// challengeFutureSkew tolerates a challenge that appears to come from the future:
	// both stamps are this process's clock, so only a clock stepping backwards gets
	// here; beyond the tolerance the value is treated as expired (fail-closed). The
	// honest bearer window is TTL + skew = SIX minutes (internal/handler measured the
	// same arithmetic for its own blob).
	challengeFutureSkew = time.Minute

	// challengeMACLabel prefixes the MAC input: domain separation from every other
	// HMAC this deployment computes.
	challengeMACLabel = "taptime/operator/login-challenge/v1|"

	// challengeKeyLabel derives the signing key FROM TAPPA_OPERATOR_TOKEN_HMAC_KEY.
	//
	// WHY NOT A FIFTH VARIABLE, AND WHY THIS IS NOT THE DERIVATION ADR 0020 §2 BANS.
	// The ban is on deriving the OPERATOR's key from the CUSTOMER's session key: a
	// derived key is not independent of its source, and the operator identity must be
	// independent of every customer credential. This derivation stays INSIDE the
	// operator's own key family -- whoever holds TAPPA_OPERATOR_TOKEN_HMAC_KEY can
	// derive it, and that holder can already verify session hashes; nothing about a
	// customer key reaches it. A separate variable would add one more secret to
	// tappa-secrets and to OP-7's separation check for a value that lives five
	// minutes and is stored nowhere (internal/handler's adminChoiceKeyLabel makes the
	// same argument for its own five-minute blob).
	//
	// WHY DERIVED AND NOT THE RAW KEY: the raw key hashes session tokens; a derived
	// key means a MAC over a challenge payload can never be a session hash and the
	// other way round, whatever the two inputs look like.
	challengeKeyLabel = "taptime/operator/login-challenge/v1/key-derivation"

	challengeRedacted = "operatorauth.Challenge(redacted)"
)

// Challenge is a login challenge in transit -- from Password to SetChallengeCookie,
// or from the request cookie to TOTP. Redacted like the session token -- the five
// methods, the value behind a *string; measured on the leak test's matrix -- because
// it is a post-password credential.
type Challenge struct{ v *string }

var (
	_ fmt.Formatter          = Challenge{}
	_ fmt.Stringer           = Challenge{}
	_ fmt.GoStringer         = Challenge{}
	_ slog.LogValuer         = Challenge{}
	_ encoding.TextMarshaler = Challenge{}
)

func wrapChallenge(v string) Challenge { return Challenge{v: &v} }

func (c Challenge) reveal() string {
	if c.v == nil {
		return ""
	}
	return *c.v
}

func (Challenge) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte(challengeRedacted)) }
func (Challenge) String() string               { return challengeRedacted }
func (Challenge) GoString() string             { return challengeRedacted }
func (Challenge) LogValue() slog.Value         { return slog.StringValue(challengeRedacted) }
func (Challenge) MarshalText() ([]byte, error) { return []byte(challengeRedacted), nil }

// strictB64 decodes what this package encoded with base64.RawURLEncoding and refuses
// every NON-CANONICAL spelling of it. The lenient decoder ignores the unused low bits of
// the last character, so flipping one of them yields a DIFFERENT string that decodes to
// the SAME bytes and verifies -- two valid spellings of one challenge. Harmless for the
// MAC, but it is a malleable credential and it made "any changed character is refused"
// true only by luck. Measured (2026-09-26, 200 000 random fields each, the low bit of
// the last character flipped): the lenient decoder returned the ORIGINAL bytes for
// 37 375 of the 41-byte payloads and 37 466 of the 32-byte MACs (0.187 = 3/16, the
// share of last characters whose low bit is padding); this decoder for none.
var strictB64 = base64.RawURLEncoding.Strict()

var (
	errChallengeShape   = errors.New("operatorauth: challenge is malformed")
	errChallengeMAC     = errors.New("operatorauth: challenge signature does not match")
	errChallengeExpired = errors.New("operatorauth: challenge has expired")
)

func deriveChallengeKey(tokenKey []byte) []byte {
	m := hmac.New(sha256.New, tokenKey)
	_, _ = m.Write([]byte(challengeKeyLabel)) // hash writes never fail (documented)
	return m.Sum(nil)
}

func (a *Authenticator) challengeMAC(payload []byte) []byte {
	m := hmac.New(sha256.New, a.keys.challengeKey.bytes())
	_, _ = m.Write([]byte(challengeMACLabel))
	_, _ = m.Write(payload)
	return m.Sum(nil)
}

// mintChallenge signs "operator id's password verified now".
func (a *Authenticator) mintChallenge(id uuid.UUID) (Challenge, error) {
	var p [challengeLen]byte
	p[0] = challengeVersion
	copy(p[challengeIDOff:challengeTimeOff], id[:])
	binary.BigEndian.PutUint64(p[challengeTimeOff:challengeNonce], uint64(a.now().Unix()))
	if _, err := rand.Read(p[challengeNonce:]); err != nil {
		return Challenge{}, errors.New("operatorauth: read randomness for a challenge failed")
	}
	mac := a.challengeMAC(p[:])
	return wrapChallenge(base64.RawURLEncoding.EncodeToString(p[:]) + "." + base64.RawURLEncoding.EncodeToString(mac)), nil
}

// verifyChallenge returns the operator id a challenge vouches for, or an error. The
// MAC is checked (constant-time) before the payload is believed; the version and the
// time are read only from an authenticated payload.
//
// ⚠️ NOT SINGLE-USE. Within its window the same challenge may be presented again, and
// each presentation may try a code. Making it single-use needs server state (a table
// or an in-process seen-set that a restart forgets) and would buy nothing the account
// budget and the database lock do not already bound: at most accountLimit attempts per
// window per account, and the lock after N failures. Written down so the TTL is not
// read as a use count (internal/handler's logincontext.go carries the same note for
// its own blob, measured).
func (a *Authenticator) verifyChallenge(c Challenge) (uuid.UUID, error) {
	v := c.reveal()
	dot := strings.IndexByte(v, '.')
	if dot < 0 || len(v) > 256 {
		return uuid.Nil, errChallengeShape
	}
	payload, err := strictB64.DecodeString(v[:dot])
	if err != nil || len(payload) != challengeLen {
		return uuid.Nil, errChallengeShape
	}
	mac, err := strictB64.DecodeString(v[dot+1:])
	if err != nil {
		return uuid.Nil, errChallengeShape
	}
	want := a.challengeMAC(payload)
	if subtle.ConstantTimeCompare(want, mac) != 1 {
		return uuid.Nil, errChallengeMAC
	}
	if payload[0] != challengeVersion {
		return uuid.Nil, errChallengeShape
	}
	issued := time.Unix(int64(binary.BigEndian.Uint64(payload[challengeTimeOff:challengeNonce])), 0)
	now := a.now()
	if issued.After(now.Add(challengeFutureSkew)) || now.Sub(issued) >= challengeTTL {
		return uuid.Nil, errChallengeExpired
	}
	var id uuid.UUID
	copy(id[:], payload[challengeIDOff:challengeTimeOff])
	if id == uuid.Nil {
		return uuid.Nil, errChallengeShape
	}
	return id, nil
}
