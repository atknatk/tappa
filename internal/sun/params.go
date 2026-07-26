package sun

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

// Plain SDM URL parsing (ADR 0003 §1). A tap arrives as
//
//	https://<host>/t?tag=<uid>&ctr=<ctr>&cmac=<mac>
//
// and this file turns those raw query strings into validated, typed values for
// the verifier (M2-04/M2-07). It performs NO cryptography — it only shapes and
// canonicalises input. Nothing here ever panics on hostile input (FuzzParse
// proves it); every malformed parameter returns a *ParseError.

// SUN URL parameter names, defined ONCE (ADR 0003 §1) so the verifier and the
// encode runbook (M8-05) share a single source of truth. They are exactly
// `tag`, `ctr`, `cmac` — plain SDM mirroring, never the encrypted-PICC `picc`
// nor `uid`.
const (
	paramTag  = "tag"
	paramCtr  = "ctr"
	paramCMAC = "cmac"
)

// Expected lengths, in hex characters, of each parameter (ADR 0003 §1):
// UID 7 bytes, ctr 3 bytes (big-endian), truncated SDM MAC 8 bytes.
const (
	uidHexLen  = 14 // 7-byte tag UID
	ctrHexLen  = 6  // 3-byte read counter
	cmacHexLen = 16 // 8-byte truncated SDM MAC
)

// Channel records how a tap URL reached us — the shared vocabulary from
// CLAUDE.md §5 and the tappa-sun skill (`nfc | qr | manual`). Parse only ever
// produces nfc or qr; manual taps never carry a SUN URL and are tagged by their
// own handler path.
type Channel string

const (
	// ChannelNFC is a physical NTAG 424 DNA tap: the chip rewrote the URL with a
	// fresh ctr and CMAC, so full SUN parameters are present (proof of moment).
	ChannelNFC Channel = "nfc"
	// ChannelQR is the static QR fallback printed beside the plaque. Nothing
	// rewrites it, so it carries no ctr/cmac and no proof of moment.
	ChannelQR Channel = "qr"
	// ChannelManual is a manager-entered record; it never flows through Parse.
	// Declared here so the channel vocabulary has one home (CLAUDE.md §5).
	ChannelManual Channel = "manual"
)

// Params is a parsed, well-formed SUN URL. Every field is already validated, so
// the domain layer (CLAUDE.md §7) receives only valid data.
type Params struct {
	// UID is the CANONICAL uppercase 14-hex tag id used for the DB lookup.
	// resolve_tag_by_uid compares uid as TEXT (internal/db/resolve.go) and the
	// seed/DB store uppercase hex (test/fixtures/seed.sql, e.g. 04AC7E55000601).
	// hex decoding is case-insensitive, so a chip that emitted lowercase would
	// decode fine yet resolve to ZERO rows on the text comparison — a valid tap
	// silently rejected. Canonicalising to uppercase here closes that gap while
	// keeping the resolver a plain TEXT equality (making the resolver
	// case-insensitive would be an M1-08 schema change, out of scope). Use
	// UIDBytes, not this, for cryptography.
	UID string
	// UIDBytes is the RAW 7-byte UID (hex-decoded). SV2 and the GCM AAD (ADR 0003
	// §4/§6, M2-04) are built over the raw bytes, which are case-agnostic; kept
	// separate from the canonical text UID above.
	UIDBytes []byte
	// Ctr is the 3-byte read counter decoded BIG-ENDIAN (ADR 0003 §1). Reading it
	// little-endian would silently produce plausible-but-wrong counters and break
	// replay ordering. Zero for QR (no counter present).
	Ctr uint32
	// CMAC is the raw 8-byte truncated SDM MAC from the chip. Nil for QR. It is a
	// value to verify, never to log (CLAUDE.md §4.7).
	CMAC []byte
	// Channel is nfc (full SUN present) or qr (no ctr/cmac). See HasSUN.
	Channel Channel
}

// HasSUN reports whether proof-of-moment parameters are present. It is false for
// QR, which forces sun_valid=false downstream (baseline base:qr-requires-ip then
// demands an IP match, CLAUDE.md §5). For NFC it is true, meaning "SUN params
// present — proceed to CMAC verification"; the tap is only actually sun_valid
// once M2-04/M2-07 verify the MAC.
func (p Params) HasSUN() bool { return p.Channel == ChannelNFC }

// UserParseError is the generic, information-free message shown to the person
// tapping when a SUN URL cannot be parsed. It never says which field failed —
// that detail lives only in the (secret-free) logged error.
const UserParseError = "Couldn't verify that tap — try again."

// Failure kinds. These are safe, value-free category labels — never the input
// itself — so a ParseError built from them leaks nothing (CLAUDE.md §4.7).
const (
	kindMissing  = "missing"
	kindLength   = "wrong length"
	kindEncoding = "invalid hex"
)

// ParseError explains why an SDM URL was rejected. Its Error() text is SAFE to
// log: it names the offending parameter and the failure KIND but never a
// parameter VALUE — so no CMAC bytes and no raw UID leak (CLAUDE.md §4.7, §7).
// Callers log the error itself and show UserMessage() to the user.
type ParseError struct {
	// Field is the parameter name ("tag"/"ctr"/"cmac"), never its value.
	Field string
	// kind is the value-free failure category (missing / wrong length / bad hex).
	kind string
}

func (e *ParseError) Error() string {
	// Format string carries no secret; %s fills in the parameter NAME and the
	// value-free kind, so this line is safe to log (CLAUDE.md §4.7).
	return fmt.Sprintf("sun: parse param %s: %s", e.Field, e.kind)
}

// UserMessage returns the generic user-facing string for any parse failure.
func (e *ParseError) UserMessage() string { return UserParseError }

// Parse converts the raw SUN URL query parameters (ADR 0003 §1) into validated
// typed values. It never panics on hostile input (proven by FuzzParse): every
// missing, over/under-length or non-hex parameter returns a *ParseError whose
// Error() is safe to log and whose UserMessage() is generic.
//
// Two shapes are valid:
//   - NFC: tag + ctr + cmac all present          → Channel=nfc, HasSUN()=true
//   - QR:  tag present, ctr AND cmac both absent  → Channel=qr,  HasSUN()=false
//
// QR is a VALID state, not an error (CLAUDE.md §5): it simply carries no proof
// of moment, so sun_valid stays false and base:qr-requires-ip takes over. Any
// other combination — exactly one of ctr/cmac present — is malformed, because a
// real NFC tap always emits both. Mixed-case hex is accepted (tags.uid CHECK
// allows both cases) and canonicalised; an empty-valued parameter (`?ctr=`) is
// treated the same as an absent one.
func Parse(q url.Values) (Params, error) {
	uidHex := q.Get(paramTag)
	if uidHex == "" {
		return Params{}, &ParseError{Field: paramTag, kind: kindMissing}
	}
	uidBytes, reason := decodeFixedHex(uidHex, uidHexLen)
	if reason != "" {
		return Params{}, &ParseError{Field: paramTag, kind: reason}
	}

	p := Params{
		// Canonical uppercase from the decoded bytes — independent of the input's
		// letter case, matching how seed/DB store the uid.
		UID:      strings.ToUpper(hex.EncodeToString(uidBytes)),
		UIDBytes: uidBytes,
	}

	ctrHex := q.Get(paramCtr)
	cmacHex := q.Get(paramCMAC)

	switch {
	case ctrHex == "" && cmacHex == "":
		// QR fallback: neither SUN field present. Valid — no proof of moment.
		p.Channel = ChannelQR
		return p, nil
	case ctrHex == "":
		// Exactly one present → malformed (a real NFC tap emits both).
		return Params{}, &ParseError{Field: paramCtr, kind: kindMissing}
	case cmacHex == "":
		return Params{}, &ParseError{Field: paramCMAC, kind: kindMissing}
	}

	ctrBytes, reason := decodeFixedHex(ctrHex, ctrHexLen)
	if reason != "" {
		return Params{}, &ParseError{Field: paramCtr, kind: reason}
	}
	cmacBytes, reason := decodeFixedHex(cmacHex, cmacHexLen)
	if reason != "" {
		return Params{}, &ParseError{Field: paramCMAC, kind: reason}
	}

	p.Channel = ChannelNFC
	p.Ctr = beUint24(ctrBytes)
	p.CMAC = cmacBytes
	return p, nil
}

// decodeFixedHex validates that s is exactly hexLen hex characters and decodes
// it. It returns a value-free failure KIND ("" on success) — never s itself — so
// the caller can build a ParseError without leaking the value (CLAUDE.md §4.7).
// hex decoding is case-insensitive, so mixed-case input is accepted.
func decodeFixedHex(s string, hexLen int) ([]byte, string) {
	if len(s) != hexLen {
		return nil, kindLength
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		// err may name the offending byte; discard it and report only the
		// category so no input value reaches a log line.
		return nil, kindEncoding
	}
	return b, ""
}

// beUint24 reads a 3-byte BIG-ENDIAN counter into a uint32 (ADR 0003 §1). Big-
// endian is normative: little-endian would silently produce plausible but wrong
// counters. b is exactly 3 bytes (decodeFixedHex validated the 6-hex length).
func beUint24(b []byte) uint32 {
	return uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
}
