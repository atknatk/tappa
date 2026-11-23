package liveqr

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// THE TOKEN -- what the screen's QR carries (the M16-21 token contract; docs/plan/m16-mobil.md,
// M16-20's card note). Binary, then unpadded base64url:
//
//	offset  size  field
//	0       1     version, tokenVersion (1)
//	1       16    the screen's id      (live_qr_displays.id)
//	17      16    the screen's location (live_qr_displays.location_id)
//	33      8     issued at, Unix seconds, big-endian -- the SERVER's clock
//	41      2     lifetime, seconds, big-endian (TokenLifetime when this card minted it)
//	43      16    tag: HMAC-SHA256(token key, bytes 0..42), first 16 bytes
//
// 59 bytes, 79 characters. The URL is <TAPPA_BASE_URL>/q?t=<token> (TapURL): /q is linked to the
// native app (internal/handler appLinkPaths), and the token is in the QUERY because the access log
// records the route pattern and never the query (internal/httpx AccessLog) -- the CMAC's and the
// invite code's reason.
//
// WHY THIS SHAPE, FIELD BY FIELD:
//   - The screen id is what M16-21 resolves (resolve_live_qr_display_by_id): the tenant, the
//     location, the revocation and the location's state come from THE ROW, not from the token --
//     a token cannot see a revocation (00111).
//   - The location is signed too (the card: "token lokasyonu bağlar"), so M16-21 can hold the
//     row's location to the token's -- a screen's location never changes (00111's one-way state),
//     so a mismatch is a forgery or a bug, never a move.
//   - Issued-at AND lifetime, not an expiry alone: the validity window travels with the token, so
//     M16-21 judges a token by the rule it was minted under even if the rotation changes later,
//     and adds its own network grace (Q39: "pencere ~20 sn + ağ payı; pay değeri M16-21'de
//     ölçülür") on top.
//   - A 128-bit tag (HMAC-SHA256 truncated, RFC 2104 §5 / NIST SP 800-107 allow it): forging one
//     online is a 2^-128 guess per try, and the 16 bytes it saves keep the QR a size smaller.
//   - Q39 IS ANSWERABLE FROM THIS SHAPE: Open says ErrMalformed (not a token at all), ErrSignature
//     (a token whose tag does not verify -- Q39 (a): reject) or returns the Claims of a genuine
//     token, whose window the caller compares with the server's clock (Q39 (b): expired -> flag).
//     The signature is checked BEFORE any field is believed, so an expired-looking forgery is a
//     forgery, never "expired".
const (
	tokenVersion   = 1
	tokenTagLen    = 16
	tokenBodyLen   = 1 + 16 + 16 + 8 + 2
	tokenLen       = tokenBodyLen + tokenTagLen
	tokenEncodeLen = (tokenLen*8 + 5) / 6 // 79
)

// TokenLifetime is how long a minted token is valid, and ~ how often the screen shows a new one
// (ADR 0045 §2.5: "~20 sn"). The screen asks for the next token before this runs out
// (internal/handler's refresh) and hides the code when it has run out without one.
const TokenLifetime = 20 * time.Second

// LiveQRTapPath and TokenParam are where the QR points: <base>/q?t=<token>. M16-21 mounts the
// handler; internal/handler links the path to the app.
const (
	LiveQRTapPath = "/q"
	TokenParam    = "t"
)

// Claims are a genuine token's fields. Nothing in them has been checked against the database or
// the clock: that is the caller's (M16-21).
type Claims struct {
	DisplayID  uuid.UUID
	LocationID uuid.UUID
	IssuedAt   time.Time
	Lifetime   time.Duration
}

// ExpiresAt is the end of the window the token was minted with.
func (c Claims) ExpiresAt() time.Time { return c.IssuedAt.Add(c.Lifetime) }

// The two refusals of Open. Fixed sentences: neither repeats the token or a byte of it.
var (
	ErrMalformed = errors.New("liveqr: not a live QR token")
	ErrSignature = errors.New("liveqr: the live QR token's signature does not verify")
)

// Mint signs a token for one screen at one location, issued at `at` (the server's clock) and valid
// for lifetime (whole seconds, at most 65 535). The token is not a secret in the way the code and
// the session are -- anyone in the room can read it off the screen -- but it is a credential for
// ~20 seconds, so it is not logged either (§7).
func (k *Keys) Mint(displayID, locationID uuid.UUID, at time.Time, lifetime time.Duration) (string, error) {
	if k == nil || k.token.Len() != sha256.Size {
		return "", errNoValue
	}
	secs := lifetime / time.Second
	if displayID == uuid.Nil || locationID == uuid.Nil || secs <= 0 || secs > 0xFFFF || at.Unix() <= 0 {
		return "", errors.New("liveqr: mint: a screen, a location, a positive issue time and a lifetime of 1 to 65535 seconds are required")
	}
	b := make([]byte, tokenLen)
	b[0] = tokenVersion
	copy(b[1:17], displayID[:])
	copy(b[17:33], locationID[:])
	binary.BigEndian.PutUint64(b[33:41], uint64(at.Unix()))
	binary.BigEndian.PutUint16(b[41:43], uint16(secs))
	copy(b[tokenBodyLen:], k.tag(b[:tokenBodyLen]))
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Open reads a token: ErrMalformed for anything that is not the shape above, ErrSignature for a
// shape whose tag does not verify (compared in constant time, hmac.Equal), else its Claims. It
// does not look at the clock.
func (k *Keys) Open(token string) (Claims, error) {
	if k == nil || k.token.Len() != sha256.Size {
		return Claims{}, errNoValue
	}
	if len(token) != tokenEncodeLen {
		return Claims{}, ErrMalformed
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(b) != tokenLen {
		return Claims{}, ErrMalformed
	}
	if b[0] != tokenVersion {
		return Claims{}, ErrMalformed
	}
	if !hmac.Equal(b[tokenBodyLen:], k.tag(b[:tokenBodyLen])) {
		return Claims{}, ErrSignature
	}
	var c Claims
	copy(c.DisplayID[:], b[1:17])
	copy(c.LocationID[:], b[17:33])
	issued := binary.BigEndian.Uint64(b[33:41])
	if issued == 0 || issued > 1<<62 {
		return Claims{}, ErrMalformed // signed by us, so unreachable; a bound before time.Unix
	}
	c.IssuedAt = time.Unix(int64(issued), 0).UTC()
	c.Lifetime = time.Duration(binary.BigEndian.Uint16(b[41:43])) * time.Second
	return c, nil
}

func (k *Keys) tag(body []byte) []byte {
	m := hmac.New(sha256.New, k.token.Reveal())
	_, _ = m.Write(body)
	return m.Sum(nil)[:tokenTagLen]
}

// TapURL is the URL the QR encodes: base (TAPPA_BASE_URL, validated by internal/config) + /q?t=.
func TapURL(base, token string) string {
	return strings.TrimRight(base, "/") + LiveQRTapPath + "?" + TokenParam + "=" + url.QueryEscape(token)
}
