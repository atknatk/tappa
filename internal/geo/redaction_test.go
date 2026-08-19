package geo_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/geo"
)

// redaction_test.go — the §4.7 proof for a coordinate (M8-03).
//
// 🔴 IT IS AN EXTERNAL TEST PACKAGE (geo_test) ON PURPOSE. The assertion is about
// what a CALLER sees when it hands a Point to a logger or a formatter, and an
// internal test can reach unexported state that a caller cannot — which is how a
// redaction test ends up proving something narrower than it claims.
//
// These are MUTATION TESTS. Round 4 of M8-03 measured what the earlier, LogValue-only
// version of this file was actually holding: deleting Format left EVERY case in
// TestPoint_EveryRenderingPathIsRedacted below leaking the full coordinate while the
// file's own comment claimed the type used "the same shape session.Token … already
// use". One method is not five.

// coordinates that must never appear. Real-looking but arbitrary: this is Valletta
// to six decimals, i.e. exactly the precision the product's numeric(9,6) columns
// hold, so a leak would print these digits.
const (
	probeLat = 35.899700
	probeLng = 14.514700
)

// leaked reports whether s carries any part of the probe coordinate or the axis
// names a reflected struct print would reveal.
func leaked(s string) string {
	for _, banned := range []string{"35.8997", "14.5147", "Lat", "Lng", "35.9", "14.5"} {
		if strings.Contains(s, banned) {
			return banned
		}
	}
	return ""
}

// TestPoint_EveryRenderingPathIsRedacted walks the paths a security audit measured
// leaking when this type carried LogValue alone.
//
// 🔴 TWO OF THEM WENT THROUGH BOTH NETS AT ONCE — the rendering guarantee AND
// scripts/redline-check.sh's R7c coordinate rule, which looks for an axis NAME and
// finds none in either: `fmt.Sprintf("%v", *fix)` (case "pointer, %v") and a Point
// held by value inside a printed struct (case "inside an exported struct field").
// fmt.Formatter closes both, which is precisely what closes them for invite.Code.
func TestPoint_EveryRenderingPathIsRedacted(t *testing.T) {
	t.Parallel()

	p := geo.Point{Lat: probeLat, Lng: probeLng}
	type carrier struct{ Fix geo.Point } // EXPORTED field: fmt can reach it as an interface

	render := map[string]func() string{
		"value, %v":  func() string { return fmt.Sprintf("%v", p) },
		"value, %+v": func() string { return fmt.Sprintf("%+v", p) },
		"value, %#v": func() string { return fmt.Sprintf("%#v", p) },
		"value, %s":  func() string { return fmt.Sprintf("%s", p) },
		"value, %q":  func() string { return fmt.Sprintf("%q", p) },
		// 🔴 A VERB fmt DOES NOT ROUTE THROUGH Stringer. handleMethods consults
		// Stringer only for v, s, q, x and X, so with String() alone a %d falls back
		// to reflection and prints {%!d(float64=35.8997) …} — the coordinate, verbatim.
		// This case is what makes deleting Format observable; measured, without it the
		// deletion left this file GREEN.
		"value, %d":                  func() string { return fmt.Sprintf("%d", p) },
		"value, %f":                  func() string { return fmt.Sprintf("%f", p) },
		"pointer, %d":                func() string { return fmt.Sprintf("%d", &p) },
		"slice, %d":                  func() string { return fmt.Sprintf("%d", []geo.Point{p}) },
		"pointer, %v":                func() string { return fmt.Sprintf("%v", &p) },
		"pointer, %+v":               func() string { return fmt.Sprintf("%+v", &p) },
		"dereferenced pointer, %v":   func() string { fix := &p; return fmt.Sprintf("%v", *fix) },
		"String()":                   func() string { return p.String() },
		"GoString()":                 func() string { return p.GoString() },
		"error wrapping":             func() string { return fmt.Errorf("tap rejected: %v", p).Error() },
		"json.Marshal, value":        func() string { b, _ := json.Marshal(p); return string(b) },
		"json.Marshal, pointer":      func() string { b, _ := json.Marshal(&p); return string(b) },
		"inside an exported field":   func() string { return fmt.Sprintf("%+v", carrier{Fix: p}) },
		"slice, %v":                  func() string { return fmt.Sprintf("%v", []geo.Point{p, p}) },
		"map, %v":                    func() string { return fmt.Sprintf("%v", map[string]geo.Point{"tap": p}) },
		"json.Marshal, slice":        func() string { b, _ := json.Marshal([]geo.Point{p}); return string(b) },
		"separate axes are NOT here": func() string { return "(see the counted limit below)" },
	}

	for name, f := range render {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out := f()
			if banned := leaked(out); banned != "" {
				t.Fatalf("this rendering path leaks the coordinate (%q appears): %s\n"+
					"§4.7 lists a full GPS coordinate among the values that must never be "+
					"rendered; the five redaction methods on Point are what close it.", banned, out)
			}
		})
	}
}

// TestPoint_IsUnloggable drives BOTH slog handlers, because they take different
// paths through a value and only one of them was ever going to be checked by
// hand: TextHandler falls back to fmt for an unknown type, JSONHandler falls back
// to encoding/json. A LogValuer short-circuits both, and a redaction that only
// covered one would look correct in a developer's terminal and leak in production
// — which, since M8-03 sets json in the cluster, is the wrong way round.
func TestPoint_IsUnloggable(t *testing.T) {
	t.Parallel()

	handlers := map[string]func(*bytes.Buffer) slog.Handler{
		"text": func(b *bytes.Buffer) slog.Handler { return slog.NewTextHandler(b, nil) },
		"json": func(b *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(b, nil) },
	}

	// Both spellings. MEASURED, and the correction of a claim this file used to
	// make: every production carrier is a *geo.Point (internal/domain/tap's Input,
	// internal/domain/checkin's Request and facts, internal/handler's parseFix) —
	// there is NO by-value carrier in the product. The value receiver is chosen for
	// the method-set reason geo.go now gives, not for a caller that does not exist,
	// and the by-value case is still tested because a test, a slice element or a
	// future caller can produce one.
	p := geo.Point{Lat: probeLat, Lng: probeLng}
	values := map[string]any{"value": p, "pointer": &p}

	for hname, newHandler := range handlers {
		for vname, v := range values {
			t.Run(hname+"/"+vname, func(t *testing.T) {
				var buf bytes.Buffer
				slog.New(newHandler(&buf)).Info("tap", "gps", v)
				out := buf.String()
				if banned := leaked(out); banned != "" {
					t.Fatalf("a coordinate reached the log: %q appears in %q", banned, out)
				}
				if !strings.Contains(out, "REDACTED") {
					t.Fatalf("no redaction marker in %q", out)
				}
			})
		}
	}
}

// TestPoint_AnUnexportedFieldStillLeaks is a COUNTED LIMIT asserted rather than
// merely written down — the one rendering path the five methods cannot close.
//
// fmt consults Formatter only for a value it may hand to an interface, and
// reflect refuses that for an unexported field (CanInterface() == false), so fmt
// falls through to plain reflection and prints Lat and Lng. session.Token closes
// this class with a *string field; that trick is NOT available here, because Lat
// and Lng are arithmetic inputs Distance, the panel form and pgx all read
// directly.
//
// It is pinned so the hole cannot be forgotten and then rediscovered as a bug. If
// a future Go release or a change here DOES close it, this test goes red and
// whoever sees it must delete the limit from geo.go's comment with it.
func TestPoint_AnUnexportedFieldStillLeaks(t *testing.T) {
	t.Parallel()

	type hidden struct{ fix geo.Point } //nolint:unused // read by reflection below
	out := fmt.Sprintf("%+v", hidden{fix: geo.Point{Lat: probeLat, Lng: probeLng}})

	if leaked(out) == "" {
		t.Fatalf("a Point in an UNEXPORTED field no longer leaks under %%+v (%s).\n"+
			"That is an IMPROVEMENT, and it makes the counted limit in internal/geo/geo.go "+
			"stale. Rewrite that comment, then delete this test.", out)
	}
}

// TestPoint_RedactionDoesNotBreakTheArithmetic is the other half, and it exists
// because the cheapest way to pass the tests above would be to stop carrying the
// numbers at all. Distance still has to be right — proof-of-place (§5 row 6) is
// what the type is for.
func TestPoint_RedactionDoesNotBreakTheArithmetic(t *testing.T) {
	t.Parallel()

	a := geo.Point{Lat: probeLat, Lng: probeLng}
	b := geo.Point{Lat: probeLat, Lng: probeLng}
	if d := geo.Distance(a, b); d != 0 {
		t.Fatalf("Distance(a, a) = %v, want 0", d)
	}
	// ~100 m north. 0.0009 degrees of latitude is 100.05 m at this radius.
	north := geo.Point{Lat: probeLat + 0.0009, Lng: probeLng}
	d := geo.Distance(a, north)
	if d < 95 || d > 105 {
		t.Fatalf("Distance 0.0009 deg north = %v m, want ~100", d)
	}
	// The fields are compared, NOT printed: %+v on a Point is the placeholder now,
	// so a failure message built from it would say nothing about what went wrong.
	if a.Lat != probeLat || a.Lng != probeLng {
		t.Fatalf("the redaction methods must not touch the fields: Lat=%g Lng=%g", a.Lat, a.Lng)
	}
}

// TestPoint_LogValueIsWhatSlogActuallyCalls closes a gap this file HAD, and the gap
// is the reason it is written as a separate test.
//
// 🔴 MEASURED BY MUTATION: deleting Point.LogValue left TestPoint_IsUnloggable
// GREEN. Both handlers still produced the placeholder — TextHandler because it falls
// back to fmt and finds Format, JSONHandler because it falls back to encoding/json
// and finds MarshalText. So the output test could not see the fifth method go
// missing, and "all five are present" rested on the compile-time assertion alone.
//
// This asserts the MECHANISM instead of the output: slog resolves a LogValuer to a
// String value before any handler sees it, so the Kind is what says whether the hook
// fired. It matters beyond tidiness — a handler that inspects Kind, or any future
// handler that does not fall back to fmt, gets the redaction only from here.
func TestPoint_LogValueIsWhatSlogActuallyCalls(t *testing.T) {
	t.Parallel()

	p := geo.Point{Lat: probeLat, Lng: probeLng}
	for name, v := range map[string]any{"value": p, "pointer": &p} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			resolved := slog.AnyValue(v).Resolve()
			if resolved.Kind() != slog.KindString {
				t.Fatalf("slog resolved a %s to Kind %v, not a string: LogValuer did not fire, and "+
					"the placeholder in the output test is coming from a fmt/json FALLBACK instead",
					name, resolved.Kind())
			}
			if leaked(resolved.String()) != "" {
				t.Fatalf("the resolved value carries the coordinate: %q", resolved.String())
			}
		})
	}
}
