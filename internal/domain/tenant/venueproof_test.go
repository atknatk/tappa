package tenant

import (
	"net/netip"
	"testing"
)

// TestVenue_ProofOfPlaceUsesTheReadSideRule is M12-03: a stored address list the
// decision engine would not count as evidence is not proof of place on the panel
// either. The wide list is the one CLAUDE.md §5 measured ({0.0.0.0/1,
// 128.0.0.0/1}); the engine's half is TestDecide_AStoredRangeTooWideForProofOfPlaceDoesNotMatch.
func TestVenue_ProofOfPlaceUsesTheReadSideRule(t *testing.T) {
	t.Parallel()
	wide := []netip.Prefix{netip.MustParsePrefix("0.0.0.0/1"), netip.MustParsePrefix("128.0.0.0/1")}
	narrow := []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}
	gps := &GPS{Lat: 35.917270, Lng: 14.489540}

	tests := []struct {
		name         string
		venue        Venue
		wantNetwork  bool
		wantAnyProof bool
	}{
		{"nothing configured", Venue{}, false, false},
		{"a wide list and no GPS is no proof at all", Venue{StaticIPs: wide}, false, false},
		{"a wide list beside GPS: proof, but not from the network", Venue{StaticIPs: wide, GPS: gps}, false, true},
		{"a narrow list is network proof", Venue{StaticIPs: narrow}, true, true},
		{"a narrow list beside GPS", Venue{StaticIPs: narrow, GPS: gps}, true, true},
		{"GPS alone", Venue{GPS: gps}, false, true},
		// The rule is the whole list's: a /24 beside the /0 does not rescue it, which is
		// how the engine reads it too.
		{"a narrow entry inside a wide list", Venue{StaticIPs: append([]netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}, narrow...)}, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.venue.HasNetworkProof(); got != tc.wantNetwork {
				t.Errorf("HasNetworkProof() = %v, want %v", got, tc.wantNetwork)
			}
			if got := tc.venue.HasProofOfPlace(); got != tc.wantAnyProof {
				t.Errorf("HasProofOfPlace() = %v, want %v", got, tc.wantAnyProof)
			}
		})
	}
}

// TestAPIVenueRow_ProofOfPlaceIsTheVenueRule pins the API path's audit field to the
// same rule (M12-03 security audit): apiVenueRow.hasProofOfPlace writes the same
// lost_proof_of_place key as SaveVenue and used to keep its own copy of the old,
// any-non-empty-list rule.
func TestAPIVenueRow_ProofOfPlaceIsTheVenueRule(t *testing.T) {
	t.Parallel()
	wide := []netip.Prefix{netip.MustParsePrefix("0.0.0.0/1"), netip.MustParsePrefix("128.0.0.0/1")}
	narrow := []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}
	tests := []struct {
		name string
		ips  []netip.Prefix
		want bool
	}{
		{"no list", nil, false},
		{"a wide list", wide, false},
		{"a narrow list", narrow, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// GPS columns are NULL (zero pgtype.Numeric): the address list decides.
			if got := (apiVenueRow{StaticIps: tc.ips}).hasProofOfPlace(); got != tc.want {
				t.Errorf("hasProofOfPlace() = %v, want %v", got, tc.want)
			}
		})
	}
}
