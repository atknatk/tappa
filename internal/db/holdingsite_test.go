package db

// holdingsite_test.go -- K-39 on the Go side (ADR 0027 §2): a HoldingSite's place evidence (the
// venue's address ranges and its coordinate) leaves the process by NO route -- fmt, slog's two
// handlers, encoding/json -- however the value is wrapped. The security review's DÜŞÜK-2 measured
// json.Marshal(site), slog's JSONHandler with a struct holding a site and with a slice of sites
// carrying the canary address and coordinate; this test is that measurement made a regression.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/netip"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestHoldingSite_EvidenceLeavesByNoRoute builds a site with canary evidence (198.51.100.77/32,
// 35.111327 / 14.222391) and sends it out every way a log line, an error or a response could:
// eight fmt verbs, slog's TextHandler and JSONHandler, json.Marshal -- the site itself, a pointer
// to it, inside an exported and an UNEXPORTED struct field, a slice and a map. No output may carry
// the address or either coordinate in any spelling (dotted, hex, the integers netip keeps, the
// numeric's digits), and each output that can print the site must print its status (anti-vacuity).
// The evidence is still READ by the accessors -- the decision's inputs, not lost. MUTANT
// (measured, ADR 0027's API-1b note): the evidence behind a pointer instead of a closure -> %s and
// %q of a site in an unexported field follow the pointer (fmt's malformed-verb path prints at
// depth 0) and carry the address's integer. LogValue, Format and MarshalJSON are not what keeps
// the evidence in: with the closure, removing any one of them leaks nothing of it (measured) --
// they keep the REST of the row (venue name, accent, logo) out of logs and JSON.
func TestHoldingSite_EvidenceLeavesByNoRoute(t *testing.T) {
	site := HoldingSite{SiteTenantID: uuid.MustParse("0b1e5a7e-0000-4000-8000-00000000c39a"), TagStatus: "active",
		LocationName: "KF St Julians",
		evidence: func() holdingEvidence {
			return holdingEvidence{
				staticIPs: []netip.Prefix{netip.MustParsePrefix("198.51.100.77/32")},
				gpsLat:    pgtype.Numeric{Int: big.NewInt(35111327), Exp: -6, Valid: true},
				gpsLng:    pgtype.Numeric{Int: big.NewInt(14222391), Exp: -6, Valid: true},
			}
		}}
	if ips := site.StaticIPs(); len(ips) != 1 || ips[0].String() != "198.51.100.77/32" {
		t.Fatalf("anti-vacuity: StaticIPs() %v", ips)
	}
	if lat, lng := site.GPS(); !lat.Valid || !lng.Valid || lat.Int.Int64() != 35111327 || lng.Int.Int64() != 14222391 {
		t.Fatalf("anti-vacuity: GPS() %v %v", lat, lng)
	}
	// The address dotted, in hex (c6.33.64.4d) and as the integers netip keeps it in (the IPv4 word
	// and its IPv4-mapped 64-bit half); each coordinate as the numeric's digits and as a decimal.
	canaries := []string{"198.51.100", "198, 51, 100", "c633644d", "C633644D", "3325256781", "281474007000141",
		"35111327", "35.111327", "14222391", "14.222391"}
	type exported struct{ S HoldingSite }
	type unexported struct{ s HoldingSite }
	// hidden: the site sits behind an unexported field, where no method of it can be called --
	// encoding/json then writes nothing of it, so only fmt's output is held to the anti-vacuity.
	values := []struct {
		name   string
		v      any
		hidden bool
	}{
		{"the site", site, false},
		{"a pointer to it", &site, false},
		{"an exported field", exported{site}, false},
		{"a pointer to an exported field", &exported{site}, false},
		{"a slice", []HoldingSite{site, site}, false},
		{"a slice of pointers", []*HoldingSite{&site}, false},
		{"a map", map[string]HoldingSite{"kf": site}, false},
		{"a map of exported fields", map[string]exported{"kf": {site}}, false},
		{"an any slice", []any{site, exported{site}}, false},
		{"an unexported field", unexported{site}, true},
		{"a pointer to an unexported field", &unexported{site}, true},
		{"unexported fields in a slice", []unexported{{site}}, true},
		{"unexported fields in a map", map[string]unexported{"kf": {site}}, true},
	}
	check := func(route, out string, printed bool) {
		t.Helper()
		for _, c := range canaries {
			if strings.Contains(out, c) {
				t.Errorf("%s carries %q: %s", route, c, out)
			}
		}
		if printed && !strings.Contains(out, "active") {
			t.Errorf("anti-vacuity: %s did not print the site at all: %s", route, out)
		}
	}
	for _, c := range values {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d"} {
			check(fmt.Sprintf("fmt %s of %s", verb, c.name), fmt.Sprintf(verb, c.v), verb != "%x" && verb != "%X" && verb != "%d" || !c.hidden)
		}
		check("an error wrapping "+c.name, fmt.Errorf("checkin: %v", c.v).Error(), true)
		for _, h := range []struct {
			name string
			mk   func(*bytes.Buffer) slog.Handler
		}{
			{"TextHandler", func(b *bytes.Buffer) slog.Handler { return slog.NewTextHandler(b, nil) }},
			{"JSONHandler", func(b *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(b, nil) }},
		} {
			var buf bytes.Buffer
			log := slog.New(h.mk(&buf))
			log.Info("tap", "v", c.v)
			log.Info("tap", slog.Any("v", c.v), slog.Group("g", slog.Any("v", c.v)))
			log.With("v", c.v).Info("tap")
			check("slog "+h.name+" of "+c.name, buf.String(), h.name == "TextHandler" || !c.hidden)
		}
		raw, err := json.Marshal(c.v)
		if err != nil {
			t.Errorf("json.Marshal of %s: %v", c.name, err)
			continue
		}
		check("json.Marshal of "+c.name, string(raw), !c.hidden)
	}
}
