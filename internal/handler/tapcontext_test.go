package handler

// Signed tap context tests. The property under test is narrow and blunt: POST
// /api/checkin must act ONLY on values this server minted, for THIS session,
// recently — and the page must not carry the chip's MAC.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/sun"
)

func testContexts(t *testing.T, at time.Time) tapContexts {
	t.Helper()
	c, err := newTapContexts(tapCfg())
	if err != nil {
		t.Fatalf("newTapContexts: %v", err)
	}
	if !at.IsZero() {
		c.now = func() time.Time { return at }
	}
	return c
}

func sampleContext() tapContext {
	return tapContext{
		UID:          tapUID,
		Ctr:          0x000641,
		Channel:      sun.ChannelNFC,
		CMACVerified: true,
		TagTenantID:  tapTagTenant,
		LocationID:   tapLocation,
	}
}

func TestTapContext_RoundTrip(t *testing.T) {
	at := time.Unix(1_800_000_000, 0).UTC()
	c := testContexts(t, at)
	sid := uuid.New()

	v, err := c.mint(sampleContext(), sid)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	got, err := c.parse(v, sid)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := sampleContext()
	want.IssuedAt = at
	if got != want {
		t.Fatalf("round trip changed the context:\n got %+v\nwant %+v", got, want)
	}
}

// TestTapContext_QRRoundTrip: a QR arrival has no counter and no MAC, and that
// is a VALID state (§5), not a malformed one.
func TestTapContext_QRRoundTrip(t *testing.T) {
	c := testContexts(t, time.Unix(1_800_000_000, 0).UTC())
	sid := uuid.New()
	in := tapContext{
		UID: tapUID, Channel: sun.ChannelQR, CMACVerified: false,
		TagTenantID: tapTagTenant, LocationID: tapLocation,
	}

	v, err := c.mint(in, sid)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	got, err := c.parse(v, sid)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Channel != sun.ChannelQR || got.Ctr != 0 || got.CMACVerified {
		t.Fatalf("qr context round-tripped as %+v", got)
	}
}

// TestTapContext_BoundToTheSession. The session id is additional authenticated
// data: a context minted for one phone does not verify on another, and the id
// is not in the payload at all.
func TestTapContext_BoundToTheSession(t *testing.T) {
	c := testContexts(t, time.Unix(1_800_000_000, 0).UTC())
	mine, theirs := uuid.New(), uuid.New()

	v, err := c.mint(sampleContext(), mine)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if _, err := c.parse(v, theirs); !errors.Is(err, errTapContextSignature) {
		t.Fatalf("a context minted for another session parsed with err = %v, want a signature failure", err)
	}
	if _, err := c.parse(v, uuid.Nil); !errors.Is(err, errTapContextSignature) {
		t.Fatalf("a context parsed against no session at all: err = %v", err)
	}
	// And the id is authenticated, not disclosed.
	if payload := decodedPayloadOf(t, v); strings.Contains(payload, mine.String()) {
		t.Fatalf("the session id appears in the payload: %q", payload)
	}
}

// TestTapContext_TamperingIsRefused walks every field of the payload, changes
// it, and re-encodes WITHOUT re-signing. This is the whole reason the value is
// signed, so it is checked field by field rather than once.
func TestTapContext_TamperingIsRefused(t *testing.T) {
	c := testContexts(t, time.Unix(1_800_000_000, 0).UTC())
	sid := uuid.New()
	v, err := c.mint(sampleContext(), sid)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	encoded, sig, _ := strings.Cut(v, ".")
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	fields := strings.Split(string(raw), "|")

	tests := []struct {
		name   string
		index  int
		value  string
		reason string
	}{
		{"tag uid", 1, "AABBCCDDEEFF00", "a tap could be moved to another plaque"},
		{"counter", 2, "999999", "a counter could be pushed forward to outrun the replay guard"},
		{"channel", 3, string(sun.ChannelQR), "an NFC tap could be relabelled QR to shed the SUN guardrails"},
		{"cmac verified", 4, "0", "a passed MAC could be turned into a failed one"},
		{"tag tenant", 5, uuid.NewString(), "the N5 tenant comparison could be fed a chosen answer"},
		{"location", 6, uuid.NewString(), "a tap could claim to be at another venue"},
		{"issued at", 7, "9999999999", "an old context could be made to look fresh"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			edited := append([]string(nil), fields...)
			edited[tc.index] = tc.value
			forged := base64.RawURLEncoding.EncodeToString([]byte(strings.Join(edited, "|"))) + "." + sig

			if _, err := c.parse(forged, sid); !errors.Is(err, errTapContextSignature) {
				t.Fatalf("editing %s was accepted (err = %v): %s", tc.name, err, tc.reason)
			}
		})
	}

	// THE DANGEROUS DIRECTION GETS ITS OWN CASE, from a context that says NO.
	// The table above starts from a context whose flag is already 1, so flipping
	// it to 1 would have changed nothing and passed while measuring nothing —
	// which is exactly what the first draft of this test did.
	failed := sampleContext()
	failed.CMACVerified = false
	fv, err := c.mint(failed, sid)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	fEncoded, fSig, _ := strings.Cut(fv, ".")
	fRaw, err := base64.RawURLEncoding.DecodeString(fEncoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	fFields := strings.Split(string(fRaw), "|")
	if fFields[4] != "0" {
		t.Fatalf("a context built with CMACVerified=false encoded the flag as %q", fFields[4])
	}
	fFields[4] = "1"
	promoted := base64.RawURLEncoding.EncodeToString([]byte(strings.Join(fFields, "|"))) + "." + fSig
	if _, err := c.parse(promoted, sid); !errors.Is(err, errTapContextSignature) {
		t.Fatalf("a failed MAC was promoted to a passed one (err = %v): forged taps would be recorded as sun-valid", err)
	}

	// A signature that is merely truncated, and one that is replaced.
	for _, bad := range []string{sig[:len(sig)-4], base64.RawURLEncoding.EncodeToString(make([]byte, 32))} {
		if _, err := c.parse(encoded+"."+bad, sid); !errors.Is(err, errTapContextSignature) {
			t.Fatalf("a forged signature parsed with err = %v", err)
		}
	}
}

// TestTapContext_Expiry. A context is a credential with a lifetime; the window
// is checked at both ends because a clock that stepped backwards must not make
// a context look fresh forever.
func TestTapContext_Expiry(t *testing.T) {
	at := time.Unix(1_800_000_000, 0).UTC()
	minted := testContexts(t, at)
	sid := uuid.New()
	v, err := minted.mint(sampleContext(), sid)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	tests := []struct {
		name    string
		when    time.Time
		wantErr error
	}{
		{"immediately", at, nil},
		{"one second before the ttl", at.Add(tapContextTTL - time.Second), nil},
		{"exactly at the ttl", at.Add(tapContextTTL), nil},
		{"one second past the ttl", at.Add(tapContextTTL + time.Second), errTapContextExpired},
		{"an hour past the ttl", at.Add(time.Hour), errTapContextExpired},
		{"inside the backwards-clock tolerance", at.Add(-30 * time.Second), nil},
		{"far in the past (clock stepped back)", at.Add(-time.Hour), errTapContextExpired},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := testContexts(t, tc.when)
			_, err := c.parse(v, sid)
			if !errors.Is(err, tc.wantErr) && !(tc.wantErr == nil && err == nil) {
				t.Fatalf("at %s: err = %v, want %v", tc.when, err, tc.wantErr)
			}
		})
	}
}

func TestTapContext_MalformedShapes(t *testing.T) {
	c := testContexts(t, time.Unix(1_800_000_000, 0).UTC())
	sid := uuid.New()

	// Values that never get as far as the signature.
	for _, v := range []string{"", ".", "abc", ".sig", "payload.", "not base64!.sig"} {
		if _, err := c.parse(v, sid); err == nil {
			t.Fatalf("parse(%q) succeeded", v)
		}
	}

	// Values that are correctly SIGNED but whose payload is nonsense. These are
	// the interesting ones: the signature check passes, so what refuses them is
	// the field decoding — which repairs nothing and guesses at nothing.
	stamp := strconv.FormatInt(time.Unix(1_800_000_000, 0).Unix(), 10)
	body := func(fields ...string) string { return strings.Join(fields, "|") }
	tests := []struct{ name, payload string }{
		{"too few fields", body("1", tapUID, "1601", "nfc", "1", tapTagTenant.String())},
		{"too many fields", body("1", tapUID, "1601", "nfc", "1", tapTagTenant.String(), tapLocation.String(), stamp, "extra")},
		{"unknown version", body("2", tapUID, "1601", "nfc", "1", tapTagTenant.String(), tapLocation.String(), stamp)},
		{"unknown channel", body("1", tapUID, "1601", "carrier-pigeon", "1", tapTagTenant.String(), tapLocation.String(), stamp)},
		{"non-boolean flag", body("1", tapUID, "1601", "nfc", "yes", tapTagTenant.String(), tapLocation.String(), stamp)},
		{"non-numeric counter", body("1", tapUID, "later", "nfc", "1", tapTagTenant.String(), tapLocation.String(), stamp)},
		{"counter past 32 bits", body("1", tapUID, "99999999999999", "nfc", "1", tapTagTenant.String(), tapLocation.String(), stamp)},
		{"non-uuid tenant", body("1", tapUID, "1601", "nfc", "1", "not-a-uuid", tapLocation.String(), stamp)},
		{"non-uuid location", body("1", tapUID, "1601", "nfc", "1", tapTagTenant.String(), "not-a-uuid", stamp)},
		{"non-numeric timestamp", body("1", tapUID, "1601", "nfc", "1", tapTagTenant.String(), tapLocation.String(), "soon")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			signed := base64.RawURLEncoding.EncodeToString([]byte(tc.payload)) + "." +
				base64.RawURLEncoding.EncodeToString(c.sign(tc.payload, sid))
			if _, err := c.parse(signed, sid); err == nil {
				t.Fatalf("a correctly signed but malformed payload was accepted: %q", tc.payload)
			}
		})
	}

	// POSITIVE CONTROL: the same construction with every field well-formed IS
	// accepted, so the failures above are about the payload and not about the
	// hand-built signature.
	good := body("1", tapUID, "1601", "nfc", "1", tapTagTenant.String(), tapLocation.String(), stamp)
	signed := base64.RawURLEncoding.EncodeToString([]byte(good)) + "." +
		base64.RawURLEncoding.EncodeToString(c.sign(good, sid))
	if _, err := c.parse(signed, sid); err != nil {
		t.Fatalf("the well-formed control was refused (%v): the malformed cases above prove nothing", err)
	}
}

// TestTapContext_ZeroCodecRefuses. A tapContexts obtained without the
// constructor holds no key, and signing under an empty key produces a MAC
// anybody can compute. Both directions refuse rather than pretend.
func TestTapContext_ZeroCodecRefuses(t *testing.T) {
	var zero tapContexts
	if _, err := zero.mint(sampleContext(), uuid.New()); err == nil {
		t.Fatal("a zero codec minted a context — it would be signed under an empty key")
	}
	if _, err := zero.parse("a.b", uuid.New()); err == nil {
		t.Fatal("a zero codec parsed a context")
	}
}

func TestTapContext_RefusesToSignWithoutASession(t *testing.T) {
	c := testContexts(t, time.Unix(1_800_000_000, 0).UTC())
	if _, err := c.mint(sampleContext(), uuid.Nil); err == nil {
		t.Fatal("a context was signed with no session: any session could then spend it")
	}
}

func TestNewTapContexts_RefusesAShortKey(t *testing.T) {
	cfg := tapCfg()
	cfg.SessionHMACKey = []byte("short")
	if _, err := newTapContexts(cfg); err == nil {
		t.Fatal("a short key was accepted: it still produces plausible-looking output, so the failure would be invisible")
	}
	if _, err := newTapContexts(nil); err == nil {
		t.Fatal("a nil config was accepted")
	}
}

// TestTapContext_DomainSeparation is the measurement behind the claim in
// tapcontext.go: this deployment now derives three things from HMAC-SHA256, and
// a value minted for one must not be presentable to another.
//
// Two independent mechanisms, checked separately so a future edit that drops
// one is visible:
//
//	the KEY   is derived from the session key rather than being it;
//	the LABEL means even the derived key does not produce a bare HMAC.
func TestTapContext_DomainSeparation(t *testing.T) {
	cfg := tapCfg()
	c, err := newTapContexts(cfg)
	if err != nil {
		t.Fatalf("newTapContexts: %v", err)
	}
	sid := uuid.New()
	payload := c.payload(sampleContext())
	got := c.sign(payload, sid)

	// 1. The signing key is NOT the session key.
	if string(c.key) == string(cfg.SessionHMACKey) {
		t.Fatal("the tap context key IS the session HMAC key: a leak of one would be a leak of both")
	}
	// The derivation is one-way, so knowing the derived key must not hand over
	// the session key; the check available here is that they differ and that the
	// derived key is a full-length MAC output.
	if len(c.key) != sha256.Size {
		t.Fatalf("derived key is %d bytes, want %d", len(c.key), sha256.Size)
	}

	// 2. A bare HMAC under the SESSION key over the same payload — what an
	// invite- or session-shaped construction would produce — does not match.
	bare := hmac.New(sha256.New, cfg.SessionHMACKey)
	bare.Write([]byte(payload))
	if hmac.Equal(got, bare.Sum(nil)) {
		t.Fatal("a bare HMAC under the session key reproduces a tap context signature")
	}

	// 3. Even under the DERIVED key, dropping the label changes the value — so
	// the label is doing work rather than decorating the input.
	unlabelled := hmac.New(sha256.New, c.key)
	unlabelled.Write([]byte(payload))
	unlabelled.Write([]byte("|"))
	unlabelled.Write([]byte(sid.String()))
	if hmac.Equal(got, unlabelled.Sum(nil)) {
		t.Fatal("the domain label does not change the MAC")
	}

	// 4. A DIFFERENT deployment key produces a different signature — the point of
	// keying it at all.
	other := tapCfg()
	other.SessionHMACKey = []byte("OTHEROTHEROTHEROTHEROTHEROTHEROO")
	oc, err := newTapContexts(other)
	if err != nil {
		t.Fatalf("newTapContexts: %v", err)
	}
	if hmac.Equal(got, oc.sign(payload, sid)) {
		t.Fatal("two deployments with different session keys produce the same tap context signature")
	}
}

// TestTapContext_CarriesTheOutcomeNotTheMAC pins the design decision that keeps
// §4.7 material off the page: the struct has room for the ANSWER to "did the
// CMAC verify", and no room for the CMAC. A field added here that could hold
// one turns this red.
func TestTapContext_CarriesTheOutcomeNotTheMAC(t *testing.T) {
	rt := reflect.TypeOf(tapContext{})
	f, ok := rt.FieldByName("CMACVerified")
	if !ok {
		t.Fatal("tapContext has no CMACVerified field")
	}
	if f.Type.Kind() != reflect.Bool {
		t.Fatalf("CMACVerified is %s, want bool: the page carries the OUTCOME of the check, never the value", f.Type.Kind())
	}
	uuidType := reflect.TypeOf(uuid.UUID{})
	for i := 0; i < rt.NumField(); i++ {
		fld := rt.Field(i)
		if fld.Name == "CMACVerified" || fld.Type == uuidType {
			// uuid.UUID is itself a [16]byte and is the one array shape that
			// belongs here; every other byte field would be somewhere a MAC or
			// a key could live.
			continue
		}
		switch fld.Type.Kind() {
		case reflect.Slice, reflect.Array:
			t.Fatalf("field %s is a %s: a raw byte field here is where a MAC or a key would end up (§4.7)", fld.Name, fld.Type.Kind())
		case reflect.String:
			if strings.Contains(strings.ToLower(fld.Name), "mac") {
				t.Fatalf("field %s could carry the chip's MAC", fld.Name)
			}
		}
	}
}

// TestTapContext_MintedPayloadHasNoHexMAC is the same property measured on the
// wire rather than on the type: a minted context's payload contains no 16-hex
// run that could be a truncated SDM MAC.
func TestTapContext_MintedPayloadHasNoHexMAC(t *testing.T) {
	c := testContexts(t, time.Unix(1_800_000_000, 0).UTC())
	v, err := c.mint(sampleContext(), uuid.New())
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	payload := decodedPayloadOf(t, v)
	for _, field := range strings.Split(payload, "|") {
		if len(field) == 16 && isHex(field) {
			t.Fatalf("payload field %q has the exact shape of a truncated SDM MAC", field)
		}
	}
	if strings.Contains(strings.ToLower(payload), strings.ToLower(tapCMAC)) {
		t.Fatalf("payload carries the sample MAC: %q", payload)
	}
}

func isHex(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return len(s) > 0
}

// A tiny guard on the assumption the payload's '|' separator rests on: no
// field may contain the separator. Hex and UUIDs cannot, but the channel is a
// package constant somebody could extend.
func TestTapContext_SeparatorCannotAppearInAField(t *testing.T) {
	for _, ch := range []sun.Channel{sun.ChannelNFC, sun.ChannelQR, sun.ChannelManual} {
		if strings.Contains(string(ch), "|") {
			t.Fatalf("channel %q contains the payload separator", ch)
		}
	}
	if strings.Contains(strings.TrimSuffix(tapContextMACLabel, "|"), "|") {
		t.Fatal("the MAC label contains the separator before its terminator")
	}
}
