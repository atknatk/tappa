package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/domain/claim"
)

// The SIGNED CLAIM CONTEXT: what the result screen's claim card hands to POST /api/claim (ADR
// 0032 §C.3, M15-17). tapcontext.go's design, for the second value this surface signs:
//
//	payload  1|<kind>|<anchor>|<anchor at>|<from>|<from incl>|<to>|<to incl>|<prefilled>|<tap at>|<zone>|<issued at>
//	mac      HMAC-SHA256(k', "tappa/claim-context/v1|" || payload || "|" || <session id>)
//
// 🔴 IT IS WHAT ENFORCES K4 AT THE SEND (M15-16 security lens, handed over): Offer is the only
// place the "at most two showings" rule is applied, and Submit accepts any record of the person's
// that still misses its half. So /api/claim takes the anchor and the kind from THIS value and
// from nowhere else -- the form carries no anchor, no kind and no prefill -- and a value is only
// ever minted for the card Offer just returned, on the screen of the tap that showed it. A record
// past its two showings, a record another phone was shown, or a kind the card did not ask, is not
// a value anybody can produce.
//
// THE SESSION ID IS AUTHENTICATED, NOT CARRIED (tapcontext.go's reasoning): a context minted for
// one phone does not verify on another, and the id never reaches the page.
//
// WHAT ELSE IT CARRIES, all server-produced at the card's render and none of it a §4.7 value:
//   - the window (ADR 0032 §C.3) and the tenant's zone: the handler turns the person's wall clock
//     into an instant against them -- the dates the window touches, and of an autumn hour that
//     happens twice the EARLIER candidate inside the window. Submit judges the instant against the
//     window it computes itself under the lock; this one only resolves the clock.
//   - the prefill (Card.Prefilled), for ONE purpose: an unchanged default goes back as the very
//     instant it came from. The prefill of a shift ending in the repeated autumn hour is that
//     hour's LATER instant (tap.ShiftSpanAt builds it with time.Date), so resolving "02:30" to the
//     earlier candidate would make "default not changed" false on the one value the server chose.
//     Submit still computes the prefill itself and compares (M15-16): a forged one could only make
//     the flag false, and a forged one is not a value anybody can produce either.
//   - the anchor's instant and the tap's, for the card's sentence when the form is shown again.
//
// The anchor is a record id (opaque); the instants are microseconds since the epoch, so the window
// and the prefill round-trip exactly. The zone is an IANA name, which has no '|'.
//
// ITS OWN KEY AND ITS OWN LABEL (domain separation, tapContextMACLabel's reason): a tap context and
// a claim context are signed under different derived keys with different labels, so neither verifies
// as the other -- the cross-label negative tests post each to the other endpoint.

const (
	claimContextVersion  = "1"
	claimContextMACLabel = "tappa/claim-context/v1|"
	// claimContextKeyLabel derives this value's key from the session HMAC key, for
	// tapContextKeyLabel's reason: nothing here is stored, it lives fifteen minutes in one field.
	claimContextKeyLabel = "tappa/claim-context/v1/key-derivation"
	// claimContextTTL is the card's lifetime (ADR 0032 §C.3: 15 min). Past it the answer is "this
	// question has expired"; nothing was sent and the tap's own record is untouched.
	claimContextTTL = 15 * time.Minute
)

var (
	errClaimContextMalformed = errors.New("handler: claim context is malformed")
	errClaimContextSignature = errors.New("handler: claim context signature does not match")
	errClaimContextExpired   = errors.New("handler: claim context has expired")
)

// claimContext is what the card established: which half of which record, the window and zone the
// answer is read against, the prefill, and the two instants the card's sentence names.
type claimContext struct {
	Kind      claim.Kind
	AnchorID  uuid.UUID
	AnchorAt  time.Time
	Window    claim.Window
	Prefilled *time.Time
	TapAt     time.Time
	Zone      string
	IssuedAt  time.Time
}

// claimContexts mints and parses signed claim contexts; tapContexts' shape.
type claimContexts struct {
	key []byte
	ttl time.Duration
	now func() time.Time
}

func newClaimContexts(cfg *config.Config) (claimContexts, error) {
	if cfg == nil {
		return claimContexts{}, errors.New("handler: nil config")
	}
	if len(cfg.SessionHMACKey) != 32 {
		return claimContexts{}, fmt.Errorf("handler: session hmac key must be 32 bytes, got %d", len(cfg.SessionHMACKey))
	}
	m := hmac.New(sha256.New, cfg.SessionHMACKey)
	_, _ = m.Write([]byte(claimContextKeyLabel)) // hash writes never fail (documented)
	return claimContexts{key: m.Sum(nil), ttl: claimContextTTL}, nil
}

func (c claimContexts) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func claimMicros(t time.Time) string { return strconv.FormatInt(t.UnixMicro(), 10) }

func claimFlag(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func (c claimContexts) payload(v claimContext) string {
	prefilled := ""
	if v.Prefilled != nil {
		prefilled = claimMicros(*v.Prefilled)
	}
	return strings.Join([]string{
		claimContextVersion,
		string(v.Kind),
		v.AnchorID.String(),
		claimMicros(v.AnchorAt),
		claimMicros(v.Window.From.At), claimFlag(v.Window.From.Inclusive),
		claimMicros(v.Window.To.At), claimFlag(v.Window.To.Inclusive),
		prefilled,
		claimMicros(v.TapAt),
		v.Zone,
		strconv.FormatInt(v.IssuedAt.Unix(), 10),
	}, "|")
}

func (c claimContexts) sign(payload string, sessionID uuid.UUID) []byte {
	m := hmac.New(sha256.New, c.key)
	_, _ = m.Write([]byte(claimContextMACLabel))
	_, _ = m.Write([]byte(payload))
	_, _ = m.Write([]byte("|"))
	_, _ = m.Write([]byte(sessionID.String()))
	return m.Sum(nil)
}

// mint stamps v with the server clock and returns the form-field value.
func (c claimContexts) mint(v claimContext, sessionID uuid.UUID) (string, error) {
	if len(c.key) == 0 {
		return "", errors.New("handler: claim contexts were not configured")
	}
	if sessionID == uuid.Nil {
		return "", errors.New("handler: refusing to sign a claim context with no session")
	}
	if _, ok := claim.ParseKind(string(v.Kind)); !ok || v.AnchorID == uuid.Nil || v.Zone == "" || strings.Contains(v.Zone, "|") {
		return "", errors.New("handler: refusing to sign an incomplete claim context")
	}
	v.IssuedAt = c.clock().Truncate(time.Second)
	p := c.payload(v)
	return base64.RawURLEncoding.EncodeToString([]byte(p)) + "." +
		base64.RawURLEncoding.EncodeToString(c.sign(p, sessionID)), nil
}

// parse verifies a context against sessionID: shape, SIGNATURE, expiry, then the fields --
// tapContexts.parse's order, so no field of an unauthenticated value is ever read.
func (c claimContexts) parse(v string, sessionID uuid.UUID) (claimContext, error) {
	if len(c.key) == 0 {
		return claimContext{}, errors.New("handler: claim contexts were not configured")
	}
	if sessionID == uuid.Nil {
		return claimContext{}, errClaimContextSignature
	}
	encoded, sig, ok := strings.Cut(v, ".")
	if !ok || encoded == "" || sig == "" {
		return claimContext{}, errClaimContextMalformed
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return claimContext{}, errClaimContextMalformed
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return claimContext{}, errClaimContextMalformed
	}
	if subtle.ConstantTimeCompare(got, c.sign(string(raw), sessionID)) != 1 {
		return claimContext{}, errClaimContextSignature
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 12 || parts[0] != claimContextVersion {
		return claimContext{}, errClaimContextMalformed
	}
	issued, err := strconv.ParseInt(parts[11], 10, 64)
	if err != nil {
		return claimContext{}, errClaimContextMalformed
	}
	issuedAt := time.Unix(issued, 0).UTC()
	now := c.clock()
	if now.Sub(issuedAt) > c.ttl || issuedAt.Sub(now) > tapContextFutureSkew {
		return claimContext{}, errClaimContextExpired
	}
	kind, ok := claim.ParseKind(parts[1])
	if !ok {
		return claimContext{}, errClaimContextMalformed
	}
	anchor, err := uuid.Parse(parts[2])
	if err != nil {
		return claimContext{}, errClaimContextMalformed
	}
	var instants [4]time.Time
	for i, p := range []string{parts[3], parts[4], parts[6], parts[9]} {
		n, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return claimContext{}, errClaimContextMalformed
		}
		instants[i] = time.UnixMicro(n).UTC()
	}
	incl := func(s string) (bool, bool) { return s == "1", s == "0" || s == "1" }
	fromIncl, ok1 := incl(parts[5])
	toIncl, ok2 := incl(parts[7])
	if !ok1 || !ok2 {
		return claimContext{}, errClaimContextMalformed
	}
	var prefilled *time.Time
	if parts[8] != "" {
		n, err := strconv.ParseInt(parts[8], 10, 64)
		if err != nil {
			return claimContext{}, errClaimContextMalformed
		}
		p := time.UnixMicro(n).UTC()
		prefilled = &p
	}
	if _, err := time.LoadLocation(parts[10]); err != nil || parts[10] == "" {
		return claimContext{}, errClaimContextMalformed
	}
	return claimContext{
		Kind: kind, AnchorID: anchor, AnchorAt: instants[0],
		Window:    claim.Window{From: claim.Bound{At: instants[1], Inclusive: fromIncl}, To: claim.Bound{At: instants[2], Inclusive: toIncl}},
		Prefilled: prefilled, TapAt: instants[3], Zone: parts[10], IssuedAt: issuedAt,
	}, nil
}
