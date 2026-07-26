package geo

import (
	"math"
	"testing"
)

// Reference coordinates are the Malta seed locations (test/fixtures/seed.sql).
// Using the real fixture coordinates keeps the distance tests anchored to data
// the rest of the system actually sees.
var (
	stJulians = Point{Lat: 35.918000, Lng: 14.489000} // KF St Julians
	paceville = Point{Lat: 35.925000, Lng: 14.490000} // KF Paceville
	hamrun    = Point{Lat: 35.885000, Lng: 14.488000} // KF Hamrun
	msida     = Point{Lat: 35.895000, Lng: 14.489000} // KF Msida
)

// stJulians -> paceville, computed INDEPENDENTLY with Python's math module
// (haversine, R = 6371008.8) to cross-check the Go implementation rather than
// asserting the code against itself:
//
//	R=6371008.8; math.radians; a=sin(dphi/2)^2 + cos(p1)cos(p2)sin(dl/2)^2;
//	R*2*atan2(sqrt(a),sqrt(1-a))  ->  783.5570309985226
//
// The pair is deliberately ASYMMETRIC (Δlat = 0.007°, Δlng = 0.001°, and
// lat ≈ 35.9 differs a lot from lng ≈ 14.5) so that swapping latitude and
// longitude changes the result — see TestDistance_LatLngNotSwapped.
const (
	stJuliansPacevilleM = 783.5570309985226
	// hamrun -> msida, same independent method -> 1115.5938858223842.
	hamrunMsidaM = 1115.5938858223842
)

// TestDistance covers the known-distance, zero and antipodal cases (M4-01
// acceptance criteria).
func TestDistance(t *testing.T) {
	tests := []struct {
		name   string
		a, b   Point
		wantM  float64
		tolM   float64
		reason string
	}{
		{
			name:   "known_malta_st_julians_to_paceville",
			a:      stJulians,
			b:      paceville,
			wantM:  stJuliansPacevilleM,
			tolM:   0.01,
			reason: "independently computed in Python; identical IEEE-754 haversine must agree",
		},
		{
			name:   "known_malta_hamrun_to_msida",
			a:      hamrun,
			b:      msida,
			wantM:  hamrunMsidaM,
			tolM:   0.01,
			reason: "second independent Malta reference",
		},
		{
			name:   "zero_identical_point",
			a:      stJulians,
			b:      stJulians,
			wantM:  0,
			tolM:   0, // must be exactly zero: dLat=dLng=0 -> h=0 -> distance 0
			reason: "same coordinate is exactly 0 m",
		},
		{
			name:   "antipodal_half_circumference",
			a:      Point{Lat: 0, Lng: 0},
			b:      Point{Lat: 0, Lng: 180}, // antipode of (0,0)
			wantM:  math.Pi * earthRadiusM,  // 20015114.44 m ≈ 20015 km
			tolM:   1e-3,
			reason: "half the great circle = pi*R; edge case where h->1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Distance(tc.a, tc.b)
			if math.Abs(got-tc.wantM) > tc.tolM {
				t.Fatalf("Distance = %.6f m, want %.6f m (±%g) — %s",
					got, tc.wantM, tc.tolM, tc.reason)
			}
		})
	}
}

// TestDistance_Symmetric proves Distance(a,b) == Distance(b,a); an asymmetric
// result would signal a sign/ordering bug in the formula.
func TestDistance_Symmetric(t *testing.T) {
	ab := Distance(stJulians, paceville)
	ba := Distance(paceville, stJulians)
	if ab != ba {
		t.Fatalf("not symmetric: Distance(a,b)=%.9f, Distance(b,a)=%.9f", ab, ba)
	}
}

// TestDistance_LatLngNotSwapped guards the named-field contract: if the formula
// treated Lat as longitude (or vice-versa), the asymmetric St Julians/Paceville
// pair would give a materially different distance. The correctly-oriented value
// must match the independent reference, and the swapped value must differ from it
// by ~21.8 m — so a swap could not pass unnoticed.
func TestDistance_LatLngNotSwapped(t *testing.T) {
	correct := Distance(stJulians, paceville)
	if math.Abs(correct-stJuliansPacevilleM) > 0.01 {
		t.Fatalf("correct orientation = %.6f m, want %.6f m", correct, stJuliansPacevilleM)
	}

	swap := func(p Point) Point { return Point{Lat: p.Lng, Lng: p.Lat} }
	swapped := Distance(swap(stJulians), swap(paceville))

	// Independent reference for the swapped orientation is 761.7677 m.
	const swappedRefM = 761.7677422709666
	if math.Abs(swapped-swappedRefM) > 0.01 {
		t.Fatalf("swapped orientation = %.6f m, want %.6f m", swapped, swappedRefM)
	}
	if math.Abs(correct-swapped) < 10 {
		t.Fatalf("swap-insensitive: correct=%.4f swapped=%.4f differ by only %.4f m; "+
			"an asymmetric pair must separate lat from lng",
			correct, swapped, math.Abs(correct-swapped))
	}
}

// pointNorth returns a point exactly m metres due north of p. With Δlng = 0 the
// haversine reduces to distance = R·Δφ, so Δφ = m/R radians places the point at
// an exact known distance — the basis for the boundary table below. This is a
// test helper only (never shipped), and it is verified against Distance itself
// inside the boundary test.
func pointNorth(p Point, m float64) Point {
	dPhiDeg := (m / earthRadiusM) / degToRad
	return Point{Lat: p.Lat + dPhiDeg, Lng: p.Lng}
}

// TestWithinRadius_Boundary pins the STRICT `<` boundary decision (M4-01: "is
// exactly 150 m inside or outside?"). Answer: OUTSIDE, matching CLAUDE.md §5
// "GPS < 150 m". Points are built at exactly 149/150/151 m north of St Julians;
// the 150 m boundary is asserted against the point's OWN measured distance
// (radius == distance) so the strict-`<` verdict is deterministic despite
// floating point, instead of relying on a fragile literal 150.0 comparison.
func TestWithinRadius_Boundary(t *testing.T) {
	const radius = 150.0

	p149 := pointNorth(stJulians, 149)
	p150 := pointNorth(stJulians, 150)
	p151 := pointNorth(stJulians, 151)

	// Construction sanity: the helper really places points at 149/150/151 m.
	for _, c := range []struct {
		p    Point
		want float64
	}{{p149, 149}, {p150, 150}, {p151, 151}} {
		if d := Distance(stJulians, c.p); math.Abs(d-c.want) > 1e-6 {
			t.Fatalf("pointNorth off: got %.9f m, want %.0f m", d, c.want)
		}
	}

	// 149 m: clearly inside the 150 m fence (1 m margin -> robust).
	if !WithinRadius(stJulians, p149, radius) {
		t.Errorf("149 m point must be within a 150 m radius")
	}
	// 151 m: clearly outside (1 m margin -> robust).
	if WithinRadius(stJulians, p151, radius) {
		t.Errorf("151 m point must be outside a 150 m radius")
	}
	// 150 m boundary, deterministic: at radius == the point's own distance the
	// strict `<` must report OUTSIDE.
	d150 := Distance(stJulians, p150)
	if WithinRadius(stJulians, p150, d150) {
		t.Errorf("point at exactly its own distance must be OUTSIDE (strict `<`)")
	}
	// And just past the boundary it flips to inside.
	if !WithinRadius(stJulians, p150, math.Nextafter(d150, math.Inf(1))) {
		t.Errorf("radius one ULP above the distance must be within (strict `<`)")
	}
}

// TestWithinRadius_RadiusIsParameter proves the radius is a caller-supplied
// parameter, not a value hard-coded in the package (M4-01: production supplies
// config.GPSRadiusMeters / TAPPA_GPS_RADIUS_M). The same ~783 m pair is inside
// or outside purely as a function of the radius argument.
func TestWithinRadius_RadiusIsParameter(t *testing.T) {
	tests := []struct {
		name    string
		radiusM float64
		want    bool
	}{
		{"default_150_excludes_783m_pair", 150, false},
		{"500m_still_excludes", 500, false},
		{"1000m_includes", 1000, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := WithinRadius(stJulians, paceville, tc.radiusM); got != tc.want {
				t.Fatalf("WithinRadius(radius=%.0f) = %v, want %v (distance ≈ %.1f m)",
					tc.radiusM, got, tc.want, stJuliansPacevilleM)
			}
		})
	}
}

// TestWithinRadius_SamePoint: a coordinate is within any positive radius of
// itself (distance 0 < r) but NOT within a zero radius (0 < 0 is false).
func TestWithinRadius_SamePoint(t *testing.T) {
	if !WithinRadius(stJulians, stJulians, 1) {
		t.Errorf("a point must be within a 1 m radius of itself")
	}
	if WithinRadius(stJulians, stJulians, 0) {
		t.Errorf("zero radius admits nothing, not even the point itself (strict `<`)")
	}
}
