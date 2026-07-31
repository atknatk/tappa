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

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/google/uuid"
)

// The SIGNED TAP CONTEXT: what GET /t hands to POST /api/checkin.
//
// THE OBVIOUS DESIGN AND WHY IT IS NOT THIS ONE. The tap page could put tag,
// ctr and cmac into hidden inputs and let the POST read them back — the M5-04
// card names that as the trap. Three things go wrong with it:
//
//  1. The POST would accept ANY (uid, ctr, cmac) triple a caller cares to send.
//     Whatever the GET step establishes — today the CMAC check, from M5-10 a
//     freshness record — would be bypassable by posting straight at the API. A
//     server-minted context inverts that: the POST accepts ONLY a value THIS
//     server put in front of a person, so the GET step sits ON the path instead
//     of beside it.
//  2. The CMAC would be page state: readable by script, kept by "save page",
//     resurrected by a shared phone's back button.
//  3. Nothing would tie the values to WHEN they were seen, which is the anchor
//     a freshness window needs.
//
// WHAT IT IS. One opaque form field, `<payload>.<mac>`, both base64url:
//
//	payload  1|<uid>|<ctr>|<channel>|<cmac verified>|<tag tenant>|<location>|<issued at>
//	mac      HMAC-SHA256(k, "tappa/tap-context/v1|" || payload || "|" || <session id>)
//
// NO CMAC, NO SESSION TOKEN, NO KEY, NO GPS — nothing on §4.7's list is in
// there. The chip's MAC is checked ONCE, on the server, while the page is
// built (sun.PreviewWithoutReplayProtection), and what crosses to the browser
// is the one-bit OUTCOME, authenticated. The counter does travel, and it is not
// a secret: it is printed in the address bar by the chip itself.
//
// THE SESSION ID IS IN THE MAC BUT NOT IN THE PAYLOAD — authenticated, not
// disclosed. The POST already knows which session it holds (its own cookie), so
// it recomputes the MAC with that id; a context minted for one phone does not
// verify on another, and the id never appears in the page.
//
// ⚠️ WHAT THAT BINDING DOES NOT BUY, so nobody reads more into it: it does
// nothing about someone handing their tap URL to a colleague. That is buddy
// punching, it is an ACCEPTED risk (ADR 0005), and signing does not touch it —
// the URL is the shareable thing, not this.
//
// 🔴 THE CONTRACT THIS IMPOSES ON M5-05, AND IT IS NOT OPTIONAL.
//
//	SUN VALID  ==  CMACVerified (this context)  AND  the atomic counter advance
//	               succeeding at POST time (store.AdvanceTagCounter).
//
// Both halves, ANDed. The first is established here and is complete on its own
// terms — a CMAC either verifies against the tag's key or it does not, and
// nothing about that answer decays. The second is the ONLY thing that
// distinguishes a fresh touch from a replay (§4.4), and this file establishes
// no part of it: a context whose CMACVerified is true says a genuine chip
// signed this (uid, ctr) pair, and says NOTHING about whether that pair has
// already been spent. A POST that advances the counter without reading
// CMACVerified would record forged taps; a POST that reads CMACVerified without
// advancing the counter would record replays. internal/sun/preview.go states
// the same requirement from the other end.
//
// HOW M5-10 BUILDS ON THIS RATHER THAN AROUND IT. The freshness window and the
// tap_page_views table are M5-10's and none of it is here. What is here is the
// hook it needs: IssuedAt is a SERVER clock reading, authenticated, carried to
// the POST. M5-10 changes the trust model rather than the field — it records
// (tag_uid, ctr, first_seen_at, source_ip) in the database and consults the
// EARLIEST row, because a signed stamp only proves when THIS context was minted
// and a caller may hold several of them for the same tap. The TTL below is a
// credential lifetime, not that policy window: it bounds how long a minted
// context can be presented at all, and it produces "tap again", not a recorded
// sys:tap-freshness reject.

const (
	// tapContextVersion prefixes the payload, and parse refuses any other value
	// (measured: the "unknown version" case in tapcontext_test.go). What that
	// buys is that a payload written under a DIFFERENT layout is rejected rather
	// than misread — PROVIDED whoever changes the layout also bumps this. That
	// last part is discipline, not enforcement: nothing here can detect a field
	// reordered under the same version number.
	tapContextVersion = "1"

	// tapContextMACLabel is prefixed to the MAC input. Domain separation is the
	// discipline internal/invite already applies, for the same reason: this
	// deployment now derives THREE things from HMAC-SHA256 (session token
	// hashes, invite code hashes, and this), and without a label a value minted
	// for one could be presented to another. The label ends in a byte that
	// cannot appear in the payload's alphabet, so label||payload is unambiguous.
	tapContextMACLabel = "tappa/tap-context/v1|"

	// tapContextKeyLabel derives THIS type's key from the session HMAC key.
	//
	// WHY DERIVE RATHER THAN ADD A FOURTH ENV VAR. internal/invite argues for an
	// independent key and is right for a value the DATABASE stores: a leaked
	// TAPPA_INVITE_HMAC_KEY must not make session hashes forgeable. Nothing here
	// is stored — a tap context lives for minutes inside one form field — and a
	// new required variable would mean every deployment, and CI, refusing to
	// boot until an operator sets a key whose only job is to sign a
	// fifteen-minute value. HMAC is one-way, so this is not key sharing: holding
	// THIS key does not reveal TAPPA_SESSION_HMAC_KEY and cannot forge a session
	// hash.
	//
	// LIMIT, in the honest direction: the implication runs the other way. Whoever
	// holds the session key can derive this one. That is acceptable because a
	// leaked session key is already a total compromise of "who" (§5) and a
	// forged tap context would be the least of it — but the two are NOT
	// independent, and a future value that needs real independence should get
	// its own variable rather than another label here.
	tapContextKeyLabel = "tappa/tap-context/v1/key-derivation"

	// tapContextTTL bounds how long a minted context may be presented.
	//
	// FIFTEEN MINUTES, chosen as a CEILING rather than as the product rule. The
	// product rule is M5-10's window (1-15 minutes, default 3, tenant-settable
	// inside a guardrail), which is tighter at every setting except the very top
	// and produces a RECORDED reject. This only stops a context from being
	// usable indefinitely. It is set to the TOP of M5-10's stated range, so
	// while that range is 1-15 this is at worst EQUAL to the window and never
	// the looser of the two — an arithmetic claim about the range as written,
	// which a later change to that range would invalidate.
	//
	// It is generous for its own job on purpose: the gap between the chip
	// rewriting the URL and a person pressing the button is seconds, but the
	// phone may need unlocking, the page may load over bad venue wifi, and the
	// person may actually read the screen. Too short and a legitimate employee
	// is refused — M5-10's own trap says the same about anything under a minute.
	tapContextTTL = 15 * time.Minute

	// tapContextFutureSkew tolerates a context that appears to come from the
	// future. Both stamps are read from THIS process's clock and the value is
	// authenticated, so the only way to get here is the clock stepping backwards
	// (an NTP correction, a suspended VM). A small tolerance keeps that from
	// refusing taps; beyond it the context is treated as expired rather than
	// believed, which is the fail-closed direction.
	tapContextFutureSkew = time.Minute
)

// Failure modes, kept separate so the handler can tell a stale page (say "tap
// again" — honest and actionable) from a value that was tampered with or was
// never ours (a generic refusal and a log line). The VISITOR is told the same
// thing either way; this distinction is for us.
var (
	errTapContextMalformed = errors.New("handler: tap context is malformed")
	errTapContextSignature = errors.New("handler: tap context signature does not match")
	errTapContextExpired   = errors.New("handler: tap context has expired")
)

// tapContext is what GET /t established about a physical touch and what POST
// /api/checkin needs in order to decide and record it.
//
// NOTHING IN HERE IS A §4.7 SECRET, which is a property of the design rather
// than of this comment: there is no field a session token, a CMAC, an AES key,
// an invite code or a GPS coordinate could travel in. That is why this type
// carries none of the redaction machinery internal/session's Token and
// internal/invite's Code need — the values are a tag id, a counter, two tenant
// ids and a timestamp, every one of which is already visible in the URL or is
// an opaque database id.
type tapContext struct {
	// UID is the canonical uppercase 14-hex tag id (sun.Params.UID).
	UID string
	// Ctr is the chip's read counter; 0 for a QR arrival, which has none. This
	// is the value POST must feed to the atomic advance.
	Ctr uint32
	// Channel is SERVER-DERIVED, never a client claim: sun.Parse decides it from
	// whether ctr/cmac were present (hand-off N2, state.md — a client must not
	// be able to say "channel=qr" and shed the SUN guardrails). It is carried so
	// M5-05 fills tap:channel from that same derivation instead of re-deriving.
	Channel sun.Channel
	// CMACVerified is the server's answer, at page-build time, to "did the
	// chip's truncated MAC verify against this tag's key".
	//
	// 🔴 IT IS HALF OF SUN VALIDITY, NEVER THE WHOLE OF IT. See the contract in
	// the file header: the other half is the atomic counter advance, which only
	// the POST can perform and which no value in this struct can substitute for.
	CMACVerified bool
	// TagTenantID is the tenant the TAG resolved to.
	//
	// 🔴 THE TAG HALF OF THE N5 HAND-OFF (state.md, "M4/M5'e devralınan"). The
	// sys:tenant-mismatch guardrail is dead until tap.Input carries BOTH
	// tenants; the session half already exists (httpx.Identity.TenantID) and
	// this is the other one. The page CARRIES it and compares nothing —
	// comparing is a decision, and decisions belong to internal/policy.
	TagTenantID uuid.UUID
	// LocationID is the TAPPED plaque's location: the venue whose IP, GPS and
	// shift the tap is matched against, which is not necessarily the employee's
	// profile location (§5 — a chain moves people between branches).
	LocationID uuid.UUID
	// IssuedAt is when GET /t minted this, from the SERVER clock — never a
	// client value (M5-10's trap says the same).
	IssuedAt time.Time
}

// tapContexts mints and parses signed contexts.
//
// The key is derived once, at construction, and held privately, so a later
// mutation of the caller's config slice cannot silently change how contexts
// sign (internal/invite's Manager takes the same precaution).
type tapContexts struct {
	key []byte
	ttl time.Duration
	// now is injectable for tests; production leaves it nil and reads the wall
	// clock.
	now func() time.Time
}

// newTapContexts derives the signing key from the session HMAC key. It refuses
// a wrong-sized key rather than degrading: a short or empty key still produces
// plausible-looking output, so the failure would stay invisible until somebody
// noticed contexts were forgeable.
func newTapContexts(cfg *config.Config) (tapContexts, error) {
	if cfg == nil {
		return tapContexts{}, errors.New("handler: nil config")
	}
	if len(cfg.SessionHMACKey) != 32 {
		return tapContexts{}, fmt.Errorf("handler: session hmac key must be 32 bytes, got %d", len(cfg.SessionHMACKey))
	}
	m := hmac.New(sha256.New, cfg.SessionHMACKey)
	_, _ = m.Write([]byte(tapContextKeyLabel)) // hash writes never fail (documented)
	return tapContexts{key: m.Sum(nil), ttl: tapContextTTL}, nil
}

func (c tapContexts) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// payload renders the authenticated fields. The separator is '|', which cannot
// occur in any of them: the uid is hex, the counter and the timestamp are
// decimal, the channel is one of two fixed words, the flag is one character and
// the ids are UUIDs. So splitting back is unambiguous with no escaping.
func (c tapContexts) payload(t tapContext) string {
	verified := "0"
	if t.CMACVerified {
		verified = "1"
	}
	return strings.Join([]string{
		tapContextVersion,
		t.UID,
		strconv.FormatUint(uint64(t.Ctr), 10),
		string(t.Channel),
		verified,
		t.TagTenantID.String(),
		t.LocationID.String(),
		strconv.FormatInt(t.IssuedAt.Unix(), 10),
	}, "|")
}

// sign computes the MAC over the label, the payload and the SESSION ID. The
// session id is additional authenticated data that never enters the payload, so
// a context is usable only from the browser it was served to while the id stays
// out of the page.
func (c tapContexts) sign(payload string, sessionID uuid.UUID) []byte {
	m := hmac.New(sha256.New, c.key)
	_, _ = m.Write([]byte(tapContextMACLabel))
	_, _ = m.Write([]byte(payload))
	_, _ = m.Write([]byte("|"))
	_, _ = m.Write([]byte(sessionID.String()))
	return m.Sum(nil)
}

// mint stamps t with the server clock and returns the form-field value.
func (c tapContexts) mint(t tapContext, sessionID uuid.UUID) (string, error) {
	if len(c.key) == 0 {
		// A zero tapContexts would sign under an empty key — a MAC anybody can
		// compute. Refuse, rather than emit a decorative signature. (Where
		// session.Cookies made its zero value SAFE, this type cannot: there is
		// no safe key to default to. So the zero value is an ERROR instead, and
		// both mint and parse check it.)
		return "", errors.New("handler: tap contexts were not configured")
	}
	if sessionID == uuid.Nil {
		// Binding to nothing would produce a context that any session could
		// spend, which is the one property this signature exists to deny.
		return "", errors.New("handler: refusing to sign a tap context with no session")
	}
	t.IssuedAt = c.clock().Truncate(time.Second)
	p := c.payload(t)
	return base64.RawURLEncoding.EncodeToString([]byte(p)) + "." +
		base64.RawURLEncoding.EncodeToString(c.sign(p, sessionID)), nil
}

// parse verifies a context against sessionID and returns what it carries.
//
// ORDER: shape, then SIGNATURE, then expiry, then field decoding. The signature
// comes before anything that interprets the payload, so no field of an
// unauthenticated value is ever acted on — and expiry comes before decoding for
// the same reason in miniature.
func (c tapContexts) parse(v string, sessionID uuid.UUID) (tapContext, error) {
	if len(c.key) == 0 {
		return tapContext{}, errors.New("handler: tap contexts were not configured")
	}
	if sessionID == uuid.Nil {
		return tapContext{}, errTapContextSignature
	}
	encoded, sig, ok := strings.Cut(v, ".")
	if !ok || encoded == "" || sig == "" {
		return tapContext{}, errTapContextMalformed
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return tapContext{}, errTapContextMalformed
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return tapContext{}, errTapContextMalformed
	}
	// Constant time, never == or bytes.Equal (redline R7). ConstantTimeCompare
	// returns 0 for differing lengths, so a truncated signature cannot match.
	if subtle.ConstantTimeCompare(got, c.sign(string(raw), sessionID)) != 1 {
		return tapContext{}, errTapContextSignature
	}

	parts := strings.Split(string(raw), "|")
	if len(parts) != 8 || parts[0] != tapContextVersion {
		// Authenticated but unreadable: minted by this deployment under a
		// different layout. Treated as malformed, never repaired.
		return tapContext{}, errTapContextMalformed
	}
	issued, err := strconv.ParseInt(parts[7], 10, 64)
	if err != nil {
		return tapContext{}, errTapContextMalformed
	}
	issuedAt := time.Unix(issued, 0).UTC()
	now := c.clock()
	if now.Sub(issuedAt) > c.ttl || issuedAt.Sub(now) > tapContextFutureSkew {
		return tapContext{}, errTapContextExpired
	}

	ctr, err := strconv.ParseUint(parts[2], 10, 32)
	if err != nil {
		return tapContext{}, errTapContextMalformed
	}
	tagTenant, err := uuid.Parse(parts[5])
	if err != nil {
		return tapContext{}, errTapContextMalformed
	}
	location, err := uuid.Parse(parts[6])
	if err != nil {
		return tapContext{}, errTapContextMalformed
	}
	channel := sun.Channel(parts[3])
	if channel != sun.ChannelNFC && channel != sun.ChannelQR {
		return tapContext{}, errTapContextMalformed
	}
	if parts[4] != "0" && parts[4] != "1" {
		return tapContext{}, errTapContextMalformed
	}
	return tapContext{
		UID:          parts[1],
		Ctr:          uint32(ctr),
		Channel:      channel,
		CMACVerified: parts[4] == "1",
		TagTenantID:  tagTenant,
		LocationID:   location,
		IssuedAt:     issuedAt,
	}, nil
}
