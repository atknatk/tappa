package sun

import (
	"bytes"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"testing"
)

// A known-good NFC parameter set reused across tests. seedUID is a REAL uid from
// test/fixtures/seed.sql (the current St Julians plaque); demoCMAC is arbitrary
// hex — Parse performs no cryptography, so its value is never verified here.
const (
	seedUID  = "04AC7E55000601"   // uppercase, exactly as seed/DB store it
	demoCtr  = "000641"           // 3-byte big-endian → 1601
	demoCMAC = "8F2A4C19D3B05E77" // 8-byte truncated MAC (opaque to the parser)
)

// values builds an url.Values from optional tag/ctr/cmac. An empty string means
// "do not set the key" so tests can exercise truly-absent parameters.
func values(tag, ctr, cmac string) url.Values {
	q := url.Values{}
	if tag != "" {
		q.Set(paramTag, tag)
	}
	if ctr != "" {
		q.Set(paramCtr, ctr)
	}
	if cmac != "" {
		q.Set(paramCMAC, cmac)
	}
	return q
}

// TestParse_ValidNFC pins the full happy path: a complete NFC tap parses into
// the canonical uid, raw 7-byte uid, big-endian counter, raw 8-byte MAC, and the
// nfc channel with HasSUN true.
func TestParse_ValidNFC(t *testing.T) {
	p, err := Parse(values(seedUID, demoCtr, demoCMAC))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Channel != ChannelNFC {
		t.Errorf("channel = %q, want %q", p.Channel, ChannelNFC)
	}
	if !p.HasSUN() {
		t.Error("HasSUN() = false, want true for a full NFC tap")
	}
	if p.UID != seedUID {
		t.Errorf("UID = %q, want %q", p.UID, seedUID)
	}
	wantBytes, _ := hex.DecodeString(seedUID)
	if !bytes.Equal(p.UIDBytes, wantBytes) {
		t.Errorf("UIDBytes = %x, want %x", p.UIDBytes, wantBytes)
	}
	if len(p.UIDBytes) != 7 {
		t.Errorf("UIDBytes len = %d, want 7", len(p.UIDBytes))
	}
	if p.Ctr != 1601 {
		t.Errorf("Ctr = %d, want 1601", p.Ctr)
	}
	wantMAC, _ := hex.DecodeString(demoCMAC)
	if !bytes.Equal(p.CMAC, wantMAC) {
		t.Errorf("CMAC = %x, want %x", p.CMAC, wantMAC)
	}
}

// TestParse_QR proves that the absence of ctr/cmac is a VALID state (CLAUDE.md
// §5), not an error: the channel is qr, HasSUN() is false, and no proof-of-moment
// bytes are carried. Empty-valued parameters are treated the same as absent.
func TestParse_QR(t *testing.T) {
	cases := []struct {
		name string
		q    url.Values
	}{
		{"both_absent", values(seedUID, "", "")},
		// Present-but-empty parameters behave like absent ones.
		{"both_empty_valued", url.Values{paramTag: {seedUID}, paramCtr: {""}, paramCMAC: {""}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Parse(tc.q)
			if err != nil {
				t.Fatalf("QR must not error: %v", err)
			}
			if p.Channel != ChannelQR {
				t.Errorf("channel = %q, want %q", p.Channel, ChannelQR)
			}
			if p.HasSUN() {
				t.Error("HasSUN() = true, want false for QR (no proof of moment)")
			}
			if p.Ctr != 0 {
				t.Errorf("Ctr = %d, want 0 for QR", p.Ctr)
			}
			if p.CMAC != nil {
				t.Errorf("CMAC = %x, want nil for QR", p.CMAC)
			}
			if p.UID != seedUID {
				t.Errorf("UID = %q, want %q", p.UID, seedUID)
			}
		})
	}
}

// TestParse_Errors is the table of every malformed shape: missing, too long, too
// short, non-hex, and half-present SUN. All must return a *ParseError (never
// panic) whose UserMessage() is the generic string, and whose logged Error()
// never contains the input value (§4.7 is checked separately below).
func TestParse_Errors(t *testing.T) {
	cases := []struct {
		name      string
		q         url.Values
		wantField string
		wantKind  string
	}{
		{"missing_tag", values("", demoCtr, demoCMAC), paramTag, kindMissing},
		{"tag_too_short", values("04AC7E5500060", demoCtr, demoCMAC), paramTag, kindLength},
		{"tag_too_long", values("04AC7E550006011", demoCtr, demoCMAC), paramTag, kindLength},
		{"tag_non_hex", values("04AC7E550006ZZ", demoCtr, demoCMAC), paramTag, kindEncoding},
		{"ctr_missing", values(seedUID, "", demoCMAC), paramCtr, kindMissing},
		{"ctr_too_short", values(seedUID, "0641", demoCMAC), paramCtr, kindLength},
		{"ctr_too_long", values(seedUID, "00006411", demoCMAC), paramCtr, kindLength},
		{"ctr_non_hex", values(seedUID, "00zz41", demoCMAC), paramCtr, kindEncoding},
		{"cmac_missing", values(seedUID, demoCtr, ""), paramCMAC, kindMissing},
		{"cmac_too_short", values(seedUID, demoCtr, "8F2A4C19D3B05E7"), paramCMAC, kindLength},
		{"cmac_too_long", values(seedUID, demoCtr, "8F2A4C19D3B05E770"), paramCMAC, kindLength},
		{"cmac_non_hex", values(seedUID, demoCtr, "8F2A4C19D3B05EZZ"), paramCMAC, kindEncoding},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Parse(tc.q)
			if err == nil {
				t.Fatalf("want error, got Params %+v", p)
			}
			var pe *ParseError
			if !errors.As(err, &pe) {
				t.Fatalf("error type = %T, want *ParseError", err)
			}
			if pe.Field != tc.wantField {
				t.Errorf("Field = %q, want %q", pe.Field, tc.wantField)
			}
			if pe.kind != tc.wantKind {
				t.Errorf("kind = %q, want %q", pe.kind, tc.wantKind)
			}
			if got := pe.UserMessage(); got != UserParseError {
				t.Errorf("UserMessage() = %q, want generic %q", got, UserParseError)
			}
		})
	}
}

// TestParse_MixedCaseCanonical is the SILENT-ZERO-ROW proof. resolve_tag_by_uid
// compares uid as TEXT and seed/DB store uppercase (04AC7E55000601). Every letter
// case of the same physical uid must decode to identical raw bytes AND normalise
// to the exact seed string — otherwise a lowercase-emitting chip would resolve to
// zero rows and a valid tap would be silently rejected.
func TestParse_MixedCaseCanonical(t *testing.T) {
	forms := []string{
		"04AC7E55000601", // uppercase (seed form)
		"04ac7e55000601", // lowercase (a chip could emit this)
		"04Ac7E55000601", // mixed
	}
	wantBytes, _ := hex.DecodeString(seedUID)
	for _, form := range forms {
		t.Run(form, func(t *testing.T) {
			p, err := Parse(values(form, demoCtr, demoCMAC))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// Canonical text equals the exact uid the DB stores.
			if p.UID != seedUID {
				t.Errorf("canonical UID = %q, want %q (seed/DB form)", p.UID, seedUID)
			}
			// Raw bytes are case-agnostic and identical across forms.
			if !bytes.Equal(p.UIDBytes, wantBytes) {
				t.Errorf("UIDBytes = %x, want %x", p.UIDBytes, wantBytes)
			}
			// Canonical output is uppercase regardless of input case.
			if p.UID != strings.ToUpper(p.UID) {
				t.Errorf("canonical UID %q is not uppercase", p.UID)
			}
		})
	}
}

// TestParse_CtrBigEndian pins the byte order (ADR 0003 §1). The 010000 case is
// decisive: big-endian reads it as 65536, little-endian would read the same
// bytes {0x01,0x00,0x00} as 1 — a plausible-but-wrong counter. Asserting 65536
// proves the reading is big-endian.
func TestParse_CtrBigEndian(t *testing.T) {
	cases := []struct {
		ctr  string
		want uint32
	}{
		{"000000", 0},
		{"000001", 1},
		{"000100", 256},
		{"010000", 65536},    // little-endian would give 1 here
		{"000641", 1601},     // matches the demo set
		{"FFFFFF", 16777215}, // 24-bit ceiling (ADR 0003 §4b wrap point)
	}
	for _, tc := range cases {
		t.Run(tc.ctr, func(t *testing.T) {
			p, err := Parse(values(seedUID, tc.ctr, demoCMAC))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.Ctr != tc.want {
				t.Errorf("Ctr(%s) = %d, want %d", tc.ctr, p.Ctr, tc.want)
			}
		})
	}
}

// TestParse_ErrorDoesNotLeak enforces §4.7: a parse failure's logged Error() must
// contain neither the offending value (e.g. a bad CMAC) nor the raw UID. It may
// name the parameter, but never its bytes.
func TestParse_ErrorDoesNotLeak(t *testing.T) {
	// A distinctive, non-hex CMAC value that must NOT appear in the log line.
	const badMAC = "DEADBEEFDEADBEZZ"
	_, err := Parse(values(seedUID, demoCtr, badMAC))
	if err == nil {
		t.Fatal("want error for non-hex cmac")
	}
	logLine := err.Error()
	if strings.Contains(logLine, badMAC) {
		t.Errorf("Error() leaks the input value: %q", logLine)
	}
	// The raw UID hex must not appear either.
	if strings.Contains(logLine, seedUID) {
		t.Errorf("Error() leaks the raw UID: %q", logLine)
	}

	// A bad tag must likewise not echo the input.
	const badTag = "FEEDFACEZZZZ00"
	_, err = Parse(values(badTag, demoCtr, demoCMAC))
	if err == nil {
		t.Fatal("want error for non-hex tag")
	}
	if strings.Contains(err.Error(), badTag) {
		t.Errorf("Error() leaks the tag value: %q", err.Error())
	}
}

// FuzzParse feeds arbitrary tag/ctr/cmac strings and asserts Parse never panics
// and always upholds its invariants on any input it accepts. Run a short round
// with: go test -run=xxx -fuzz=FuzzParse -fuzztime=20s ./internal/sun/...
func FuzzParse(f *testing.F) {
	f.Add(seedUID, demoCtr, demoCMAC)                     // valid NFC
	f.Add(seedUID, "", "")                                // valid QR
	f.Add("", "", "")                                     // missing tag
	f.Add("04ac7e55000601", "000641", "8f2a4c19d3b05e77") // lowercase
	f.Add("zz", "000641", demoCMAC)                       // non-hex short tag
	f.Add(seedUID, "0641", demoCMAC)                      // short ctr
	f.Add(seedUID, demoCtr, "")                           // half-present SUN
	f.Add("%zz", "&=", "  ")                              // junk

	f.Fuzz(func(t *testing.T, tag, ctr, cmac string) {
		q := url.Values{}
		q.Set(paramTag, tag)
		q.Set(paramCtr, ctr)
		q.Set(paramCMAC, cmac)

		// Contract: never panics. Validity is asserted by the table tests; here
		// we only prove robustness and that accepted results are well-formed.
		p, err := Parse(q)
		if err != nil {
			return
		}
		if len(p.UIDBytes) != 7 {
			t.Fatalf("accepted uid must be 7 bytes, got %d", len(p.UIDBytes))
		}
		if p.UID != strings.ToUpper(p.UID) {
			t.Fatalf("canonical uid %q not uppercase", p.UID)
		}
		switch p.Channel {
		case ChannelNFC:
			if len(p.CMAC) != 8 {
				t.Fatalf("nfc CMAC must be 8 bytes, got %d", len(p.CMAC))
			}
		case ChannelQR:
			if p.CMAC != nil || p.Ctr != 0 {
				t.Fatalf("qr must carry no ctr/cmac, got ctr=%d cmac=%x", p.Ctr, p.CMAC)
			}
		default:
			t.Fatalf("unexpected channel %q", p.Channel)
		}
	})
}
