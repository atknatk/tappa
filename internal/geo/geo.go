// Package geo computes great-circle distance between two GPS coordinates and
// answers whether one lies within a given radius of another. It is the
// arithmetic behind proof-of-place (CLAUDE.md §5 row 6, "GPS < 150 m"): the tap
// decision engine (internal/domain/tap, M4-02) feeds it the tapped GPS, the
// location's registered GPS and the tenant's radius, and uses the boolean it
// returns as one of the four evidences.
//
// It is a PURE package: no DB, no HTTP, no clock, no randomness — and, crucially,
// NO logging. GPS is read only at the moment of a tap (CLAUDE.md §4.2) and a full
// coordinate is a secret that must never be logged (CLAUDE.md §4.7); this package
// therefore imports nothing but "math", which the import list proves. Callers own
// coordinates and callers decide what (never the raw value) reaches a log line.
//
// float64 is the right type here: this is trigonometry, not money or worked
// hours — those forbid float (CLAUDE.md §6) and live elsewhere (M4-05 uses
// time.Duration / integer minutes). A metre of rounding on a 150 m fence is
// irrelevant to the yes/no it produces.
package geo

import "math"

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
