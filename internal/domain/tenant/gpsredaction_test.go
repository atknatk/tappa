package tenant

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// gpsredaction_test.go — the §4.7 proof for the VENUE-side coordinate (M8-03).
//
// 🔴 IT IS A SECOND TEST FOR A SECOND TYPE, AND THAT IS THE POINT. geo.Point and
// tenant.GPS are unrelated types that carry the same secret on two different
// paths — the tapped coordinate and the venue's registered one — so a redaction
// proven on one says nothing about the other. Deleting either LogValue must turn
// exactly one of these two files red, and it does.

const (
	probeLat = 35.899700
	probeLng = 14.514700
)

// gpsLeaked reports whether s carries any part of the probe coordinate or the axis
// names a reflected struct print would reveal.
func gpsLeaked(s string) string {
	for _, banned := range []string{"35.8997", "14.5147", "Lat", "Lng", "35.9", "14.5"} {
		if strings.Contains(s, banned) {
			return banned
		}
	}
	return ""
}

// TestGPS_EveryRenderingPathIsRedacted is the venue-side twin of
// TestPoint_EveryRenderingPathIsRedacted, and it exists as its OWN table rather
// than as a shared helper: a divergence in COVERAGE between the two carriers is
// exactly the kind of hole nobody can see, so both tables are written out and both
// have to be edited when one changes.
//
// 🔴 ROUND 4 MEASURED WHAT LogValue ALONE WAS HOLDING HERE: every case below leaked
// the full coordinate while this file claimed the shape matched session.Token's.
func TestGPS_EveryRenderingPathIsRedacted(t *testing.T) {
	t.Parallel()

	g := GPS{Lat: probeLat, Lng: probeLng}
	type carrier struct{ Fix GPS } // EXPORTED: fmt can reach it as an interface

	render := map[string]func() string{
		"value, %v":  func() string { return fmt.Sprintf("%v", g) },
		"value, %+v": func() string { return fmt.Sprintf("%+v", g) },
		"value, %#v": func() string { return fmt.Sprintf("%#v", g) },
		"value, %s":  func() string { return fmt.Sprintf("%s", g) },
		// A verb fmt does NOT route through Stringer — see the geo twin for the
		// measurement. Without it, deleting Format here left this file green.
		"value, %d":                func() string { return fmt.Sprintf("%d", g) },
		"value, %f":                func() string { return fmt.Sprintf("%f", g) },
		"pointer, %d":              func() string { return fmt.Sprintf("%d", &g) },
		"slice, %d":                func() string { return fmt.Sprintf("%d", []GPS{g}) },
		"pointer, %v":              func() string { return fmt.Sprintf("%v", &g) },
		"dereferenced pointer, %v": func() string { p := &g; return fmt.Sprintf("%v", *p) },
		"String()":                 func() string { return g.String() },
		"GoString()":               func() string { return g.GoString() },
		"error wrapping":           func() string { return fmt.Errorf("venue rejected: %v", g).Error() },
		"json.Marshal, value":      func() string { b, _ := json.Marshal(g); return string(b) },
		"json.Marshal, pointer":    func() string { b, _ := json.Marshal(&g); return string(b) },
		"inside an exported field": func() string { return fmt.Sprintf("%+v", carrier{Fix: g}) },
		"slice, %v":                func() string { return fmt.Sprintf("%v", []GPS{g, g}) },
		"map, %v":                  func() string { return fmt.Sprintf("%v", map[string]GPS{"venue": g}) },
	}

	for name, f := range render {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out := f()
			if banned := gpsLeaked(out); banned != "" {
				t.Fatalf("this rendering path leaks the venue coordinate (%q appears): %s", banned, out)
			}
		})
	}
}

// TestGPS_IsUnloggable drives both handlers and both spellings, for the reasons
// internal/geo/redaction_test.go states in full.
func TestGPS_IsUnloggable(t *testing.T) {
	t.Parallel()

	handlers := map[string]func(*bytes.Buffer) slog.Handler{
		"text": func(b *bytes.Buffer) slog.Handler { return slog.NewTextHandler(b, nil) },
		"json": func(b *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(b, nil) },
	}

	g := GPS{Lat: probeLat, Lng: probeLng}
	values := map[string]any{"value": g, "pointer": &g}

	for hname, newHandler := range handlers {
		for vname, v := range values {
			t.Run(hname+"/"+vname, func(t *testing.T) {
				var buf bytes.Buffer
				slog.New(newHandler(&buf)).Info("venue", "gps", v)
				out := buf.String()
				if banned := gpsLeaked(out); banned != "" {
					t.Fatalf("a coordinate reached the log: %q appears in %q", banned, out)
				}
				if !strings.Contains(out, "REDACTED") {
					t.Fatalf("no redaction marker in %q", out)
				}
			})
		}
	}
}

// TestGPS_RedactionLeavesTheAuditTrailAlone pins the boundary this redaction has
// to respect, and it is a real one rather than a formality.
//
// gpsText renders the coordinate into the audit_log detail of a venue change, and
// that is REQUIRED to stay exact: `locations` has no updated_at (venue.go says so
// at length), so the trail row is the only record anywhere that the evidence a tap
// is judged on ever moved. A database column is not a log line — internal/domain/
// checkin/insertParams draws the same line for gps_lat/gps_lng.
//
// 🔴 AND SINCE ROUND 4 THE TYPE DOES IMPLEMENT String(), SO THIS TEST IS NO LONGER
// HYPOTHETICAL. An earlier version of this comment argued that adding a Stringer
// would silently blank the trail; that was the right worry about the wrong
// mechanism. The trail is safe because gpsText builds its text from the FIELDS with
// strconv.FormatFloat and never formats the struct — so the five redaction methods
// and the exact six-decimal record coexist. Rewriting gpsText as fmt.Sprintf("%v",
// g) turns this test red, which is exactly the change it exists to refuse.
func TestGPS_RedactionLeavesTheAuditTrailAlone(t *testing.T) {
	t.Parallel()

	g := GPS{Lat: 35.8997, Lng: 14.5147}
	if got := gpsText(&g); got != "35.899700,14.514700" {
		t.Fatalf("gpsText = %q, want the six-decimal pair the column stores", got)
	}
	// The fields are compared, NOT printed: %+v on a GPS is the placeholder now.
	if g.Lat != 35.8997 || g.Lng != 14.5147 {
		t.Fatalf("the redaction methods must not touch the fields: Lat=%g Lng=%g", g.Lat, g.Lng)
	}
}

// TestGPS_TheNumericParameterIsStillExact is the DATABASE half of the same
// boundary, and it is separate because it fails for a different reason: pgx never
// sees a GPS, it sees two pgtype.Numeric values built by numericFrom. A redaction
// that reached this path would write "[REDACTED gps]" into gps_lat and every tap at
// the venue would lose the GPS half of proof of place (§5 row 6) — silently, with
// no error, which is the failure class venue.go's own header is about.
func TestGPS_TheNumericParameterIsStillExact(t *testing.T) {
	t.Parallel()

	lat, lng, err := gpsParams(&GPS{Lat: 35.8997, Lng: 14.5147})
	if err != nil {
		t.Fatalf("gpsParams: %v", err)
	}
	latF, err := lat.Float64Value()
	if err != nil {
		t.Fatalf("lat: %v", err)
	}
	lngF, err := lng.Float64Value()
	if err != nil {
		t.Fatalf("lng: %v", err)
	}
	if latF.Float64 != 35.8997 || lngF.Float64 != 14.5147 {
		t.Fatalf("the stored parameters are %g / %g, want 35.8997 / 14.5147", latF.Float64, lngF.Float64)
	}
}

// TestGPS_LogValueIsWhatSlogActuallyCalls — the venue-side twin. See the geo file
// for the mutation that made this necessary: with Format and MarshalText present,
// deleting LogValue leaves the OUTPUT tests green, because both handlers fall back.
func TestGPS_LogValueIsWhatSlogActuallyCalls(t *testing.T) {
	t.Parallel()

	g := GPS{Lat: probeLat, Lng: probeLng}
	for name, v := range map[string]any{"value": g, "pointer": &g} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			resolved := slog.AnyValue(v).Resolve()
			if resolved.Kind() != slog.KindString {
				t.Fatalf("slog resolved a %s to Kind %v, not a string: LogValuer did not fire",
					name, resolved.Kind())
			}
			if gpsLeaked(resolved.String()) != "" {
				t.Fatalf("the resolved value carries the coordinate: %q", resolved.String())
			}
		})
	}
}
