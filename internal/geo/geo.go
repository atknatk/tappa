// Package geo computes great-circle distance between two GPS coordinates and
// answers whether one lies within a given radius of another. It is the
// arithmetic behind proof-of-place (CLAUDE.md §5 row 6, "GPS < 150 m"): the tap
// decision engine (internal/domain/tap, M4-02) feeds it the tapped GPS, the
// location's registered GPS and the tenant's radius, and uses the boolean it
// returns as one of the four evidences.
//
// It is a PURE package: no DB, no HTTP, no clock, no randomness — and, crucially,
// it EMITS no logging. GPS is read only at the moment of a tap (CLAUDE.md §4.2)
// and a full coordinate is a secret that must never be logged (CLAUDE.md §4.7).
// Callers own coordinates and callers decide what (never the raw value) reaches a
// log line.
//
// ⚠️ THAT SENTENCE USED TO END "this package therefore imports nothing but math,
// which the import list proves", AND M8-03 HAD TO RETRACT THE PROOF. The import
// list proved this package could not CALL a logger; it never had anything to say
// about a caller PASSING a Point to one, which is the direction the leak actually
// runs — and a measurement found nothing anywhere stopping that: a log call
// carrying a live coordinate returned exit 0 from scripts/redline-check.sh, the
// only §4.7 class of the six with no mechanism at all. Point's five redaction
// methods close that direction, and they cost this package three stdlib imports it
// uses for TYPES and for writing a fixed placeholder — never for output of its
// own. The weaker claim is the true one.
//
// float64 is the right type here: this is trigonometry, not money or worked
// hours — those forbid float (CLAUDE.md §6) and live elsewhere (M4-05 uses
// time.Duration / integer minutes). A metre of rounding on a 150 m fence is
// irrelevant to the yes/no it produces.
package geo

import (
	"encoding"
	"fmt"
	"log/slog"
	"math"
)

// earthRadiusM is the mean Earth radius in metres used for the haversine
// distance. We use the IUGG arithmetic mean radius R1 = (2a + b) / 3 of the
// WGS-84 ellipsoid, 6371008.8 m — the standard "mean radius" and the most
// physically grounded single-sphere choice (the alternative 6371000 m differs by
// 8.8 m, i.e. ~1.4e-6 relative, which at a 150 m fence is ~0.0002 m — far below
// anything that changes a within-radius verdict). Tests assert distances against
// this constant; independent reference values were computed with the same R.
const earthRadiusM = 6371008.8

// degToRad converts degrees to radians. Coordinates arrive in decimal degrees
// (as stored in locations.gps_lat / gps_lng); the haversine formula works in
// radians.
const degToRad = math.Pi / 180

// Point is a single WGS-84 coordinate in decimal degrees. The fields are NAMED
// Lat and Lng on purpose (CLAUDE.md §7, M4-01 card): a bare Distance(lat1, lng1,
// lat2, lng2 float64) signature invites the classic latitude/longitude swap,
// which silently returns a plausible-but-wrong distance. Making the axes types
// forces every caller to say which is which, and the swap-resistance test proves
// the formula itself respects the distinction.
type Point struct {
	// Lat is latitude in decimal degrees, north positive (range -90..90).
	Lat float64
	// Lng is longitude in decimal degrees, east positive (range -180..180).
	Lng float64
}

// redactedPoint is what a coordinate looks like anywhere it is rendered.
const redactedPoint = "[REDACTED gps]"

// The FIVE redaction methods, and the compile-time proof that all five are
// present (CLAUDE.md §4.7, M8-03). If a refactor drops one, the build breaks
// here rather than a coordinate silently appearing in a log line.
//
// 🔴 IT IS FIVE BECAUSE ROUND 4 MEASURED THAT ONE WAS NOT THE SHAPE THIS
// REPOSITORY USES, WHILE THE COMMENT HERE CLAIMED IT WAS. Until then this type
// implemented LogValue ALONE and said it was "the same shape session.Token,
// invite.Code, adminauth.Token, adminauth.ResetToken and db.PasswordHash already
// use". Those five types implement FIVE methods each. A security audit walked the
// difference and every one of these leaked the full coordinate: `%v`, `%+v`,
// `%#v`, `%v` on a pointer, json.Marshal, a Point held BY VALUE inside a printed
// struct, a []Point, a map[string]Point. Two of those — `fmt.Sprintf("%v", *fix)`
// and the by-value struct field — went through BOTH nets at once, because
// redline-check.sh's coordinate rule sees no axis name in either. Format alone
// closes both, which is exactly what closes them for invite.Code.
//
// 🔴 THE RECEIVER IS THE VALUE, AND THE REASON IS METHOD SETS, NOT A SURVEY OF
// CALLERS. An earlier version of this comment justified it by claiming
// "tap.Input holds them by value"; measured, that is FALSE — internal/domain/tap
// (types.go: GPS, LocationGPS), internal/domain/checkin (Request.GPS,
// facts.locationGPS) and internal/handler/checkin.go (parseFix) are *geo.Point
// WITHOUT EXCEPTION, so there is no by-value carrier in production at all. The
// choice is still right and now rests on something true: Go gives *Point every
// method Point has but not the reverse, so a value receiver covers both spellings
// while a pointer receiver would leave a by-value Point — the shape a future
// caller, a test, or a slice element takes — printing its fields.
//
// ⚠️ WHAT THE FIVE DO NOT COVER, COUNTED AND MEASURED:
//
//  1. THE AXES TAKEN OUT FIRST. A caller that reads the two float64s out of the
//     struct and logs them under axis-named keys never hands anything a Point.
//     That is what R7c's coordinate pattern in scripts/redline-check.sh is for,
//     and it is a text match with the usual evasions. Neither is a proof.
//  2. A Point IN AN UNEXPORTED FIELD OF SOMEBODY ELSE'S STRUCT, under `%v`/`%+v`.
//     fmt only consults Formatter for a value it may hand to an interface, and
//     reflect refuses that for an unexported field, so fmt falls through to plain
//     reflection and prints Lat and Lng. session.Token closes this with a *string
//     field; that trick is NOT available here — Lat and Lng are float64 arithmetic
//     inputs that Distance, the panel form and pgx all read directly, and boxing
//     them would trade a real feature for a rendering guarantee.
//     TestPoint_AnUnexportedFieldStillLeaks pins it as a known hole rather than
//     letting the next reader assume it is closed.
//
// (These paragraphs deliberately DESCRIBE the bad shape instead of writing it
// out. The literal would match R7c's own pattern and make the scan red on the
// comment explaining it — the self-match R1 already carries a waiver for.)
var (
	_ fmt.Formatter          = Point{}
	_ fmt.Stringer           = Point{}
	_ fmt.GoStringer         = Point{}
	_ slog.LogValuer         = Point{}
	_ encoding.TextMarshaler = Point{}
)

// Format implements fmt.Formatter, which fmt consults BEFORE Stringer and for
// EVERY verb — so %v, %+v, %#v, %x, %q and any future verb are covered, on a
// Point, a *Point, a slice, a map and an EXPORTED struct field alike. It is the
// widest of the five.
func (Point) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redactedPoint)) }

// String implements fmt.Stringer for direct .String() calls and for the string
// conversions fmt does not route through Format.
func (Point) String() string { return redactedPoint }

// GoString covers %#v for anything that reaches GoStringer without going through
// Format — text/template's printf and %#v inside a package that shadows fmt.
func (Point) GoString() string { return redactedPoint }

// LogValue implements slog.LogValuer so log/slog renders the placeholder even
// when a Point is passed as an attribute value or nested in a logged struct.
func (Point) LogValue() slog.Value { return slog.StringValue(redactedPoint) }

// MarshalText implements encoding.TextMarshaler, which encoding/json prefers for
// a value type — so a Point reached through a JSON response, an audit detail or a
// structured log encoder serialises to the placeholder too.
//
// MEASURED SAFE ON THIS TREE: nothing serialises a Point. The coordinate reaches
// the database as a SIX-DECIMAL STRING built with strconv.FormatFloat from the
// fields (internal/domain/checkin, internal/domain/tenant's numericFrom), and the
// audit trail stores it as an already-formatted string field, so neither path
// goes through encoding/json on this type.
func (Point) MarshalText() ([]byte, error) { return []byte(redactedPoint), nil }

// Distance returns the great-circle distance between a and b in METRES, using
// the haversine formula on a sphere of radius earthRadiusM. It is symmetric
// (Distance(a, b) == Distance(b, a)), returns 0 for identical points and
// ~π·earthRadiusM (~20015 km) for antipodes. haversine (rather than the simpler
// spherical law of cosines) is chosen because it stays numerically accurate for
// the small distances that matter here — two points a few metres apart.
func Distance(a, b Point) float64 {
	lat1 := a.Lat * degToRad
	lat2 := b.Lat * degToRad
	dLat := (b.Lat - a.Lat) * degToRad
	dLng := (b.Lng - a.Lng) * degToRad

	sinDLat := math.Sin(dLat / 2)
	sinDLng := math.Sin(dLng / 2)
	h := sinDLat*sinDLat + math.Cos(lat1)*math.Cos(lat2)*sinDLng*sinDLng

	// atan2(√h, √(1-h)) is stable across the full range, including the antipodal
	// h→1 case where asin(√h) would lose precision.
	return earthRadiusM * 2 * math.Atan2(math.Sqrt(h), math.Sqrt(1-h))
}

// WithinRadius reports whether b lies within radiusM metres of a. The radius is
// a PARAMETER, never a constant baked into this package (M4-01 card): production
// supplies the tenant's configured value via config.GPSRadiusMeters
// (TAPPA_GPS_RADIUS_M, default 150, bounded 25..1000 by ADR 0004 §11).
//
// The boundary is STRICT: the comparison is `<`, so a point at exactly radiusM
// metres is OUTSIDE the radius, not inside. This is deliberate and matches the
// normative wording of CLAUDE.md §5 row 6 ("GPS < 150 m") — at exactly 150 m the
// GPS evidence does not count. Callers wanting inclusive behaviour must widen the
// radius, not rely on the boundary.
func WithinRadius(a, b Point, radiusM float64) bool {
	return Distance(a, b) < radiusM
}
