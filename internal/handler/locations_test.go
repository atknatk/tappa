package handler

// locations_test.go -- the LOCATIONS section (M6-06 phase A) at the HTTP boundary.
//
// 🔴 WHAT THIS FILE CAN PROVE AND WHAT IT CANNOT, stated first because the fake it
// leans on will agree with anything. It can prove that the BOUNDARY turns a form
// into the right command, that an empty box becomes an ABSENCE rather than a zero,
// that a refusal reaches the manager as a sentence, that the tenant and the actor
// come from the session, and that every control the page offers is served. It cannot
// prove that a NULL shift reaches the column as NULL, that the audit row shares the
// write's transaction, or anything about section 4.5 -- those need real Postgres and
// live in internal/domain/tenant/venue_db_test.go.
//
// 🔴 THE TWO MOST EXPENSIVE MISTAKES THIS SECTION COULD MAKE ARE BOTH SILENT, which
// is why they get the longest tests here. Folding an empty shift box into 00:00 marks
// every arrival at that venue late forever, with no error anywhere (CLAUDE.md section
// 5, M4-05). Folding an empty coordinate into 0,0 registers the venue in the Gulf of
// Guinea and quietly costs the GPS half of proof of place (section 5 row 6). Neither
// fails a request, neither shows red, and the schema cannot refuse either -- 00:00 is
// a real time and 0,0 passes locations_gps_pair.

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/web/templates/components"
	"github.com/atknatk/tappa/web/templates/pages"
)

// --- harness -------------------------------------------------------------------

// newAdminRouterWithVenues wires the REAL router, the REAL middleware chain and the
// REAL budgets with a venues side the caller controls. Every test below drives that
// router rather than calling a handler directly -- the M5-04 lesson, which is that a
// test building its own chain measures its own chain and says nothing about the one
// that ships.
func newAdminRouterWithVenues(t *testing.T, admins *fakeAdmins, venues panelVenues) http.Handler {
	t.Helper()
	return newAdminRouterWithPlaques(t, admins, venues, &fakePlaques{})
}

// newAdminRouterWithPlaques is the same harness with the PLAQUE side under the
// caller's control too (M6-06 phase B). It is a second constructor rather than a
// widened one because every existing caller is about venues and departments, and
// making them all name a plaque fake would bury the one thing each is testing.
func newAdminRouterWithPlaques(t *testing.T, admins *fakeAdmins, venues panelVenues, plaques panelPlaques) http.Handler {
	t.Helper()
	records := newFakeLedger()
	h, err := NewAdminAuth(admins, &fakeTrail{}, records, records, &fakeReviewer{},
		&fakeStaff{}, &fakeInviter{}, venues, plaques, &fakeRecorder{}, newFakeRules(), newFakeScribe(), adminTestConfig(), discardLogger())
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	return r
}

// venueBrowser is a signed-in manager of panelTestTenant looking at a section backed
// by the given fake.
func venueBrowser(t *testing.T, venues panelVenues) *browser {
	t.Helper()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: "owner", FullName: "KF Owner",
		}, nil
	}}
	b := newBrowser(t, newAdminRouterWithVenues(t, admins, venues))
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

// twoVenues is the ordinary fixture: one venue with proof of place and one without,
// plus a department in the first.
//
// THE SECOND VENUE IS THE INTERESTING ONE. A venue with neither address ranges nor a
// coordinate is the state every tap at which is recorded as `flag` (section 5 row 7),
// and it is reached by doing nothing at all -- so it is the default shape of a venue
// somebody just added, not an exotic case.
func twoVenues() *fakeVenues {
	withProof := uuid.New()
	bare := uuid.New()
	dept := uuid.New()
	return &fakeVenues{screen: tenant.VenueScreen{
		Venues: []tenant.Venue{
			{
				ID:        withProof,
				Name:      "KF St Julians",
				StaticIPs: []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")},
				GPS:       &tenant.GPS{Lat: 35.917270, Lng: 14.489540},
				Shift: &tenant.Shift{
					Start: 8 * time.Hour, End: 17 * time.Hour,
				},
				WiFiSSID:        "KF-Staff",
				DepartmentCount: 1,
			},
			{ID: bare, Name: "KF Rusty Bar"},
		},
		Departments: []tenant.Department{
			{ID: dept, LocationID: withProof, LocationName: "KF St Julians", Name: "Kitchen"},
		},
	}}
}

func (f *fakeVenues) firstVenueID() uuid.UUID      { return f.screen.Venues[0].ID }
func (f *fakeVenues) firstDepartmentID() uuid.UUID { return f.screen.Departments[0].ID }

// --- the boundary: an empty box is an ABSENCE, never a zero ----------------------

// TestParseShift_EmptyIsNilNotMidnight is the test internal/handler/locations.go and
// internal/domain/tenant/venue.go both name, and it is the one that would catch the
// single most destructive edit either file could receive.
//
// 🔴 THE POSITIVE CONTROL IS HALF OF IT. Asserting only that an empty pair yields nil
// would stay green if the function returned nil for EVERYTHING, so the "00:00" case
// asserts that a real midnight shift is still storable and still distinct. The two
// together are what pin the distinction rather than the absence.
func TestParseShift_EmptyIsNilNotMidnight(t *testing.T) {
	tests := []struct {
		name      string
		start     string
		end       string
		overnight bool
		want      *tenant.Shift
		wantMsg   bool
	}{
		{name: "both empty is no shift at all", want: nil},
		{name: "whitespace is empty too", start: "  ", end: "\t", want: nil},
		{
			// 🔴 THE CONTROL. Midnight IS expressible and IS different from absent. If
			// this line ever returned nil the case above would still pass and the
			// distinction the whole file rests on would be gone.
			name:  "an explicit midnight shift is a real shift",
			start: "00:00", end: "00:00",
			want: &tenant.Shift{Start: 0, End: 0},
		},
		{
			name: "an ordinary day", start: "08:30", end: "17:00",
			want: &tenant.Shift{Start: 8*time.Hour + 30*time.Minute, End: 17 * time.Hour},
		},
		{
			// Browsers send seconds when a step attribute asks for them, and a value
			// that is valid HTML must not be a validation failure.
			name: "seconds are accepted", start: "08:30:15", end: "17:00:00",
			want: &tenant.Shift{
				Start: 8*time.Hour + 30*time.Minute + 15*time.Second,
				End:   17 * time.Hour,
			},
		},
		{
			name: "the Rusty Bar shift", start: "18:00", end: "02:00", overnight: true,
			want: &tenant.Shift{Start: 18 * time.Hour, End: 2 * time.Hour, Overnight: true},
		},
		{
			// 🔴 OVERNIGHT WITHOUT A SHIFT IS DROPPED, NOT STORED. tenant.Shift keeps
			// Overnight inside the pointer precisely so "overnight with no shift" is
			// unrepresentable; the boundary must not try to express it anyway.
			name: "overnight alone says nothing", overnight: true, want: nil,
		},
		{name: "a start with no end is refused", start: "08:00", wantMsg: true},
		{name: "an end with no start is refused", end: "17:00", wantMsg: true},
		{name: "a non-time is refused", start: "morning", end: "17:00", wantMsg: true},
		{name: "an out-of-range hour is refused", start: "25:00", end: "17:00", wantMsg: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, msg := parseShift(tc.start, tc.end, tc.overnight)
			if tc.wantMsg {
				if msg == "" {
					t.Fatalf("parseShift(%q, %q) accepted a half-filled or unreadable pair", tc.start, tc.end)
				}
				if got != nil {
					t.Errorf("a refused shift still produced %+v; nothing may be stored", *got)
				}
				return
			}
			if msg != "" {
				t.Fatalf("parseShift(%q, %q) refused a valid input: %s", tc.start, tc.end, msg)
			}
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("an empty shift box became %+v. A shift of 00:00 means the shift "+
					"STARTS AT MIDNIGHT and would mark every arrival at this venue late, "+
					"forever, with no error anywhere (CLAUDE.md section 5, M4-05). NULL means "+
					"lateness is not computed.", *got)
			case tc.want != nil && got == nil:
				t.Fatalf("a filled shift box became nil, which stores NULL and turns lateness " +
					"reporting OFF for this venue")
			case tc.want != nil && *got != *tc.want:
				t.Errorf("parseShift = %+v, want %+v", *got, *tc.want)
			}
		})
	}
}

// TestParseGPS_EmptyIsNilNotZero. 0,0 is a real point in the Gulf of Guinea, it
// passes the locations_gps_pair CHECK, and it would put every tap at that venue about
// 5 000 km from where somebody is standing -- so the GPS half of proof of place would
// never match and the venue would silently fall to IP-only (section 5 row 6).
func TestParseGPS_EmptyIsNilNotZero(t *testing.T) {
	tests := []struct {
		name    string
		lat     string
		lng     string
		want    *tenant.GPS
		wantMsg bool
	}{
		{name: "both empty is no coordinate", want: nil},
		{name: "whitespace is empty too", lat: " ", lng: "  ", want: nil},
		{
			// 🔴 THE CONTROL. The null island IS storable and IS different from absent.
			name: "an explicit zero coordinate is a real coordinate",
			lat:  "0", lng: "0", want: &tenant.GPS{Lat: 0, Lng: 0},
		},
		// 🔴 THE INCOMPARABLE FLOATS. strconv.ParseFloat("NaN", 64) SUCCEEDS, and every
		// comparison against NaN is false -- so the old guard, written as `f < -90 ||
		// f > 90`, admitted it. It then reached Postgres as the numeric NaN and came
		// back as a 23514, which a signed-in manager saw as "the panel is unavailable"
		// plus the loss of the other seven fields they had typed. Both spellings, both
		// fields: "out of range" does NOT cover this, and believing it did was the
		// finding.
		{name: "NaN latitude is refused", lat: "NaN", lng: "10", wantMsg: true},
		{name: "lower-case nan latitude is refused", lat: "nan", lng: "10", wantMsg: true},
		{name: "NaN longitude is refused", lat: "10", lng: "NaN", wantMsg: true},
		{name: "Inf latitude is refused", lat: "Inf", lng: "10", wantMsg: true},
		{name: "+Inf latitude is refused", lat: "+Inf", lng: "10", wantMsg: true},
		{name: "-Inf latitude is refused", lat: "-Inf", lng: "10", wantMsg: true},
		{name: "Inf longitude is refused", lat: "10", lng: "Inf", wantMsg: true},
		{name: "-Inf longitude is refused", lat: "10", lng: "-Inf", wantMsg: true},
		// 🔴 AND THE SYNTAXES ParseFloat ACCEPTS THAT A COORDINATE NEVER HAS. The one
		// that matters is "1e-400": it UNDERFLOWS TO 0 WITH NO ERROR, so it would
		// silently register the venue at 0,0 in the Gulf of Guinea -- this file's own
		// defect arriving through a different door.
		{name: "an underflowing exponent is refused rather than becoming zero",
			lat: "1e-400", lng: "10", wantMsg: true},
		{name: "a hex float is refused", lat: "0x1p3", lng: "10", wantMsg: true},
		{name: "a hex float longitude is refused", lat: "10", lng: "0X1P-2", wantMsg: true},
		{name: "underscore digit separators are refused", lat: "1_0.5", lng: "10", wantMsg: true},
		{name: "scientific notation is refused", lat: "1e2", lng: "10", wantMsg: true},
		// ...while ordinary partial typing still works, because Postgres numeric takes
		// all three and refusing them would be pedantry about a keystroke.
		{name: "a trailing point is accepted", lat: "35.", lng: "14.",
			want: &tenant.GPS{Lat: 35, Lng: 14}},
		{name: "a leading point is accepted", lat: ".9", lng: ".5",
			want: &tenant.GPS{Lat: 0.9, Lng: 0.5}},
		{name: "an explicit plus is accepted", lat: "+35.9", lng: "+14.4",
			want: &tenant.GPS{Lat: 35.9, Lng: 14.4}},
		{
			name: "St Julians", lat: "35.917270", lng: "14.489540",
			want: &tenant.GPS{Lat: 35.917270, Lng: 14.489540},
		},
		{name: "a negative pair", lat: "-33.8688", lng: "-70.6693",
			want: &tenant.GPS{Lat: -33.8688, Lng: -70.6693}},
		{name: "a latitude with no longitude is refused", lat: "35.9", wantMsg: true},
		{name: "a longitude with no latitude is refused", lng: "14.4", wantMsg: true},
		{name: "a non-number is refused", lat: "north", lng: "14.4", wantMsg: true},
		{name: "a latitude past the pole is refused", lat: "91", lng: "14.4", wantMsg: true},
		{name: "a longitude past the meridian is refused", lat: "35.9", lng: "181", wantMsg: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, msg := parseGPS(tc.lat, tc.lng)
			if tc.wantMsg {
				if msg == "" {
					t.Fatalf("parseGPS(%q, %q) accepted an unusable pair", tc.lat, tc.lng)
				}
				if got != nil {
					t.Errorf("a refused coordinate still produced %+v", *got)
				}
				return
			}
			if msg != "" {
				t.Fatalf("parseGPS(%q, %q) refused a valid input: %s", tc.lat, tc.lng, msg)
			}
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("an empty coordinate box became %+v. 0,0 is a real place and passes "+
					"the gps_pair CHECK; NULL means no coordinate is registered.", *got)
			case tc.want != nil && got == nil:
				t.Fatalf("a filled coordinate box became nil, which clears the GPS half of " +
					"proof of place")
			case tc.want != nil && *got != *tc.want:
				t.Errorf("parseGPS = %+v, want %+v", *got, *tc.want)
			}
		})
	}
}

// TestParseRanges_AcceptsWhatPostgresWouldAndRefusesWhatItWouldNot.
//
// 🔴 THE HOST-BITS CASE IS THE ONE THIS FUNCTION EXISTS FOR. Go PARSES
// "192.168.1.5/24" happily -- netip.ParsePrefix returns it and only Masked() differs
// -- while Postgres REFUSES it: `invalid cidr value ... Value has bits set to right
// of mask`. Without the equality check a plausible typo reaches the driver and comes
// back to the manager as a 500 with a driver string in it.
func TestParseRanges_AcceptsWhatPostgresWouldAndRefusesWhatItWouldNot(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []string
		wantMsg string // a substring the refusal must carry
	}{
		{name: "empty is no evidence, which is a configuration", raw: "", want: []string{}},
		{name: "whitespace only is the same", raw: " \n\t ", want: []string{}},
		{
			// A manager typing a single office address is the ordinary case. netip
			// REFUSES a bare address as a prefix; Postgres widens it to /32. The
			// boundary does the widening so the stored value is the one this code
			// decided on rather than one two layers negotiated.
			name: "a bare address becomes its own /32",
			raw:  "192.168.1.5", want: []string{"192.168.1.5/32"},
		},
		{name: "a bare v6 address becomes its own /128",
			raw: "2001:db8::1", want: []string{"2001:db8::1/128"}},
		{name: "an ordinary subnet", raw: "192.168.1.0/24", want: []string{"192.168.1.0/24"}},
		{name: "one per line", raw: "192.168.1.0/24\n10.0.0.0/8",
			want: []string{"192.168.1.0/24", "10.0.0.0/8"}},
		{name: "commas are accepted too", raw: "192.168.1.0/24, 10.0.0.0/8",
			want: []string{"192.168.1.0/24", "10.0.0.0/8"}},
		{
			// Two identical ranges match exactly what one matches, so refusing would be
			// pedantry about a paste and storing both would grow an array EVERY TAP is
			// evaluated against.
			name: "a duplicate is dropped rather than refused",
			raw:  "192.168.1.0/24\n192.168.1.0/24", want: []string{"192.168.1.0/24"},
		},
		{
			name: "host bits set is refused with the masked form named",
			raw:  "192.168.1.5/24", wantMsg: "192.168.1.0/24",
		},
		{name: "nonsense is refused", raw: "not-an-address", wantMsg: "not an address"},
		{name: "a bad mask is refused", raw: "192.168.1.0/99", wantMsg: "not an address range"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, msg := parseRanges(tc.raw)
			if tc.wantMsg != "" {
				if msg == "" {
					t.Fatalf("parseRanges(%q) accepted a value Postgres would refuse", tc.raw)
				}
				if !strings.Contains(msg, tc.wantMsg) {
					t.Errorf("refusal %q does not carry %q, so the manager is not told what "+
						"they probably meant", msg, tc.wantMsg)
				}
				return
			}
			if msg != "" {
				t.Fatalf("parseRanges(%q) refused a valid list: %s", tc.raw, msg)
			}
			// 🔴 NEVER nil. static_ips is NOT NULL DEFAULT '{}', so a nil slice would be
			// a NULL parameter the column refuses -- "the manager cleared every range"
			// would be a 500 instead of the configuration change it is.
			if got == nil {
				t.Fatal("parseRanges returned a nil slice; static_ips is NOT NULL and a nil " +
					"parameter would make an emptied list a 500")
			}
			if len(got) != len(tc.want) {
				t.Fatalf("parseRanges(%q) = %v, want %v", tc.raw, got, tc.want)
			}
			for i := range got {
				if got[i].String() != tc.want[i] {
					t.Errorf("range %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestParseRanges_BoundsTheArrayEveryTapIsEvaluatedAgainst. static_ips is cidr[] with
// no length limit in the schema, and GetLocationByIP evaluates `src <<= ANY(...)`
// against the whole array on EVERY TAP -- so this is the one field on the screen
// whose size is paid for on the decision path rather than only where it is stored.
func TestParseRanges_BoundsTheArrayEveryTapIsEvaluatedAgainst(t *testing.T) {
	// 🔴 THE OCTETS ARE FORMATTED, NOT CONCATENATED. A first draft of this fixture
	// built "10.00.0.0/16" by writing two digits, and Go's own parser refuses a
	// leading zero in an IPv4 octet — so the test failed on its FIXTURE and would have
	// been "fixed" by loosening the bound it exists to hold.
	var b strings.Builder
	for i := range maxStaticRanges {
		fmt.Fprintf(&b, "10.%d.0.0/16\n", i)
	}
	atLimit := b.String()
	if got, msg := parseRanges(atLimit); msg != "" || len(got) != maxStaticRanges {
		t.Fatalf("exactly %d ranges was refused (%d parsed, msg %q); the bound must admit "+
			"its own limit or it is off by one", maxStaticRanges, len(got), msg)
	}
	if _, msg := parseRanges(atLimit + "10.99.0.0/16\n"); msg == "" {
		t.Errorf("%d ranges were accepted; the array every tap is evaluated against is "+
			"unbounded", maxStaticRanges+1)
	}
}

// TestParseName_RefusesRatherThanTruncates.
//
// 🔴 THE COLUMN DOES NOT BOUND THIS. locations.name and departments.name are
// unbounded `text` (migration 00002) -- the same shape employees.full_name has, where
// a security audit measured what it cost: a 605-rune name on a page boundary served
// page one forever. These two names are worse placed, because they are rendered into
// the LOCATION and DEPARTMENT dropdowns of three other sections' filter bars.
//
// AND IT REFUSES RATHER THAN CUTTING, which is the opposite of what the roster's name
// FILTER does. Cutting a search term only widens a search; cutting a name somebody is
// SAVING stores a name they did not type and says nothing about it.
func TestParseName_RefusesRatherThanTruncates(t *testing.T) {
	// A Maltese letter, spelled by code point so this file stays ASCII on disk. It is
	// two bytes in UTF-8, which is exactly why the bound is counted in RUNES: a byte
	// bound would refuse ordinary Maltese names at four fifths of the stated length.
	const hBar = string(rune(0x0127))

	tests := []struct {
		name    string
		raw     string
		want    string
		wantMsg bool
	}{
		{name: "an ordinary name", raw: "KF St Julians", want: "KF St Julians"},
		{name: "surrounding space is trimmed", raw: "  Rusty Bar\n", want: "Rusty Bar"},
		{name: "empty is refused", raw: "", wantMsg: true},
		{name: "whitespace only is refused", raw: "   \t\n", wantMsg: true},
		{
			// PostgreSQL text cannot hold a zero byte: it answers "invalid byte sequence
			// for encoding UTF8" and the request becomes a 500. It is stripped, not
			// refused -- a NUL is a transport artefact, not something a manager typed.
			name: "a NUL byte is removed rather than exploded on",
			raw:  "Rusty\x00 Bar", want: "Rusty Bar",
		},
		{
			name: "exactly the bound is accepted",
			raw:  strings.Repeat("a", tenant.MaxVenueNameRunes),
			want: strings.Repeat("a", tenant.MaxVenueNameRunes),
		},
		{
			name:    "one rune over is refused",
			raw:     strings.Repeat("a", tenant.MaxVenueNameRunes+1),
			wantMsg: true,
		},
		{
			// 🔴 RUNES, NOT BYTES. MaxVenueNameRunes of a two-byte letter is twice that
			// many bytes and must still pass, or Maltese venues are second-class.
			name: "the bound counts runes and not bytes",
			raw:  strings.Repeat(hBar, tenant.MaxVenueNameRunes),
			want: strings.Repeat(hBar, tenant.MaxVenueNameRunes),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, msg := parseName(tc.raw, "venue")
			if tc.wantMsg {
				if msg == "" {
					t.Fatalf("parseName accepted a %d-rune name; the column has no bound of "+
						"its own", len([]rune(tc.raw)))
				}
				if got != "" {
					t.Errorf("a refused name still produced %q -- it must not be TRUNCATED "+
						"and stored", got)
				}
				return
			}
			if msg != "" {
				t.Fatalf("parseName(%q) refused a valid name: %s", tc.raw, msg)
			}
			if got != tc.want {
				t.Errorf("parseName = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestParseSSID_BoundsBytesNotRunes. Migration 00010's CHECK is octet_length 1..32 --
// the 802.11 SSID field's own limit -- so checking runes here would let a Maltese
// name through that the column then refuses as a 23514, which the manager sees as a
// 500 for a value they were told was fine.
func TestParseSSID_BoundsBytesNotRunes(t *testing.T) {
	const hBar = string(rune(0x0127)) // 2 bytes in UTF-8

	if got, msg := parseSSID(""); msg != "" || got != "" {
		t.Fatalf(`an empty network name must be "" and not an error (00010: NULL means `+
			`"no network to show"); got %q, %q`, got, msg)
	}
	if got, msg := parseSSID("  "); msg != "" || got != "" {
		t.Errorf("whitespace-only should trim to empty, got %q, %q", got, msg)
	}
	full := strings.Repeat("a", maxSSIDBytes)
	if got, msg := parseSSID(full); msg != "" || got != full {
		t.Errorf("exactly %d bytes was refused: %q, %q", maxSSIDBytes, got, msg)
	}
	if _, msg := parseSSID(strings.Repeat("a", maxSSIDBytes+1)); msg == "" {
		t.Errorf("%d bytes was accepted; the column's CHECK would refuse it as a 23514",
			maxSSIDBytes+1)
	}
	// 16 two-byte letters are EXACTLY 32 octets and must pass.
	half := strings.Repeat(hBar, maxSSIDBytes/2)
	if len(half) != maxSSIDBytes {
		t.Fatalf("fixture is wrong: %d bytes, want %d", len(half), maxSSIDBytes)
	}
	if got, msg := parseSSID(half); msg != "" || got != half {
		t.Errorf("a 32-octet multi-byte name was refused: %q", msg)
	}
	// 🔴 THE CONTROL FOR "BYTES, NOT RUNES". 17 of them are 34 octets: a rune-counting
	// bound would accept this and the column would then answer 23514.
	over := strings.Repeat(hBar, maxSSIDBytes/2+1)
	if _, msg := parseSSID(over); msg == "" {
		t.Errorf("a %d-rune / %d-octet name was accepted; the bound is counting runes, "+
			"and the column counts octets", len([]rune(over)), len(over))
	}
}

// --- the section ------------------------------------------------------------------

// TestLocationsSection_EveryControlLeadsSomewhereThatExists reads every posting form
// and every in-product link OFF THE RENDERED PAGE and drives each one through the
// SAME router the page came from.
//
// 🔴 "A SCREEN OFFERS SOMETHING THE SERVER DOES NOT DO" IS THIS REPOSITORY'S MOST
// EXPENSIVE CLASS OF MISTAKE, and a test that lists the routes it expects cannot
// catch it -- it would agree with whatever the author remembered. This one starts
// from the HTML.
func TestLocationsSection_EveryControlLeadsSomewhereThatExists(t *testing.T) {
	f := twoVenues()
	b := venueBrowser(t, f)

	// Every card the section can open, so the forms are on the page to be found.
	pages := []string{
		locationsHref,
		locationsHref + "?venue=new",
		locationsHref + "?venue=" + f.firstVenueID().String(),
		locationsHref + "?department=new",
		locationsHref + "?department=" + f.firstDepartmentID().String(),
	}
	// 🔴 SIGN-OUT IS DRIVEN BY NOBODY, AND SKIPPING IT IS NOT A HOLE. It comes from
	// the shared chrome rather than from this section, it is already driven by its own
	// tests, and POSTing it here would END THE SESSION every later assertion depends
	// on — which is exactly how the first draft of this test failed, with a 303 on the
	// second page that looks nothing like the defect it was reporting.
	const chromeLogout = "/admin/logout"

	seenForms, seenLinks := 0, 0
	targets := map[string]bool{}
	for _, page := range pages {
		rec := b.do(http.MethodGet, page, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d, want 200", page, rec.Code)
		}
		body := htmlOf(t, rec)

		for _, m := range postFormRE.FindAllStringSubmatch(body, -1) {
			action := attrValueRE("action").FindStringSubmatch(m[1])
			if len(action) != 2 || action[1] == "" {
				t.Fatalf("%s carries a posting form with no action", page)
			}
			target := htmlUnescaper.Replace(action[1])
			targets[target] = true
			if target == chromeLogout {
				continue
			}
			seenForms++
			// An EMPTY post is enough: what is under test is that the route is MOUNTED,
			// not what it does with a valid body. 404 or 405 means the page offers a
			// control the router does not serve.
			got := b.do(http.MethodPost, target, url.Values{})
			if got.Code == http.StatusNotFound || got.Code == http.StatusMethodNotAllowed {
				t.Errorf("%s offers a form posting to %s, which the router answers %d",
					page, target, got.Code)
			}
		}
		for _, href := range sameOriginHrefs(body) {
			seenLinks++
			got := b.do(http.MethodGet, href, nil)
			if got.Code == http.StatusNotFound || got.Code == http.StatusMethodNotAllowed {
				t.Errorf("%s offers a link to %s, which the router answers %d",
					page, href, got.Code)
			}
		}
	}

	// AND THE SET OF POSTING TARGETS IS EXACTLY WHAT THIS SECTION REGISTERS. The loop
	// above proves each target is served by SOMETHING; this proves the section is not
	// quietly posting at another section's route.
	want := map[string]bool{chromeLogout: true, venueSaveHref: true, departmentSaveHref: true}
	for target := range targets {
		if !want[target] {
			t.Errorf("the locations section posts to %s, which is not one of its routes", target)
		}
	}
	for target := range want {
		if !targets[target] {
			t.Errorf("no form on this section posts to %s", target)
		}
	}
	// ANTI-VACUITY. With a blinded scanner or an empty page every assertion above
	// passes without measuring anything, which is exactly how this test would rot.
	if seenForms < 2 {
		t.Fatalf("found %d posting form(s) across %d pages; this section has two write "+
			"routes, so the scanner or the fixture is broken and the loop proved nothing",
			seenForms, len(pages))
	}
	if seenLinks < 4 {
		t.Fatalf("found %d in-product link(s); the section renders an Add link, an Edit "+
			"link per row and a Cancel link, so the scanner proved nothing", seenLinks)
	}
}

// TestLocationsSection_OffersNoActionTheServerWouldRefuse.
//
// 🔴 THE DEPARTMENT'S VENUE IS NOT EDITABLE AND THE CONTROL IS ABSENT RATHER THAN
// DISABLED. UpdateDepartment does not name location_id (db/queries/departments.sql
// carries the argument: moving a department would leave every employee in it holding
// a (venue, department) pair MoveEmployee refuses, judged against another branch's
// shift). A disabled dropdown would still say "this is a thing you could do here if
// only"; its absence plus a sentence says the true thing.
func TestLocationsSection_OffersNoActionTheServerWouldRefuse(t *testing.T) {
	f := twoVenues()
	b := venueBrowser(t, f)

	// THE POSITIVE CONTROL FIRST. A NEW department must be given a venue, so the
	// control EXISTS on that form -- without this, the assertion below would pass over
	// a template that never renders a venue control at all.
	newRec := b.do(http.MethodGet, locationsHref+"?department=new", nil)
	if newRec.Code != http.StatusOK {
		t.Fatalf("GET the new-department form: %d, want 200", newRec.Code)
	}
	if newBody := htmlOf(t, newRec); !strings.Contains(newBody, `name="location_id"`) {
		t.Fatal("the NEW department form has no venue control, so the assertion below " +
			"would be vacuous -- and a department must sit in a venue")
	}

	editRec := b.do(http.MethodGet,
		locationsHref+"?department="+f.firstDepartmentID().String(), nil)
	if editRec.Code != http.StatusOK {
		t.Fatalf("GET the edit-department form: %d, want 200", editRec.Code)
	}
	body := htmlOf(t, editRec)
	if strings.Contains(body, `name="location_id"`) {
		t.Error("the EDIT department form offers a venue control. UpdateDepartment does " +
			"not name location_id, so the server would silently ignore whatever is " +
			"chosen -- the manager would be told a move happened that did not.")
	}
	// The absence has to be EXPLAINED, or it reads as a missing feature and somebody
	// adds the control back.
	if !strings.Contains(body, "cannot be moved to another venue") {
		t.Error("the edit form omits the venue control without saying why; section 4.6 " +
			"asks the screen to say what it will not do")
	}
	// AND THE VENUE IS STILL NAMED -- an edit card that does not say which venue a
	// department belongs to is a card about an unidentified thing.
	if !strings.Contains(body, "KF St Julians") {
		t.Error("the edit form does not name the department's venue")
	}
}

// TestLocationsSection_LoadsNoScript. The panel's widened Content-Security-Policy is
// pinned to exactly one url by TestPanelScreens_ScriptsAndPolicyAgreeAndReachNoThirdParty,
// and this section spends nothing on it: the one place a script would have helped is
// the address-range repeater, which is a textarea instead.
func TestLocationsSection_LoadsNoScript(t *testing.T) {
	b := venueBrowser(t, twoVenues())
	for _, page := range []string{locationsHref, locationsHref + "?venue=new"} {
		rec := b.do(http.MethodGet, page, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d, want 200", page, rec.Code)
		}
		if body := strings.ToLower(htmlOf(t, rec)); strings.Contains(body, "<script") {
			t.Errorf("%s loads a script. Adding one is allowed but must be argued for in %s.",
				page, "TestPanelScreens_ScriptsAndPolicyAgreeAndReachNoThirdParty")
		}
	}
}

// TestLocationsSection_SaysWhenAVenueHasNoProofOfPlace.
//
// 🔴 THE ONE SENTENCE THIS SCREEN EXISTS TO SAY (section 5, rows 6 and 7). A venue
// with neither an address range nor a coordinate has no proof of place, so every tap
// there is still RECORDED -- nothing is lost -- but it is judged `flag` and waits for
// somebody to approve it by hand. That state is reached by doing NOTHING, so the card
// has to volunteer it.
func TestLocationsSection_SaysWhenAVenueHasNoProofOfPlace(t *testing.T) {
	b := venueBrowser(t, twoVenues())
	body := htmlOf(t, b.do(http.MethodGet, locationsHref, nil))

	if !strings.Contains(body, "No proof of place") {
		t.Error("a venue with no address range and no coordinate is rendered with no " +
			"warning; every tap there goes to the approval queue and the screen is the " +
			"only place a manager could learn that")
	}
	if !strings.Contains(body, "review queue") {
		t.Error("the warning does not say what actually happens (the taps are recorded " +
			"and queued for approval), which is the difference between a scold and a fact")
	}
	// ANTI-VACUITY: the CONFIGURED venue must not carry the same warning, or the
	// assertion above would pass on a template that prints it unconditionally.
	if n := strings.Count(body, "No proof of place"); n != 1 {
		t.Errorf("the warning appears %d times over one bare venue and one configured "+
			"venue; want exactly 1", n)
	}
	// AND THE ABSENT SHIFT IS NOT RENDERED AS A ZERO. "None" would read as a shift of
	// zero length; a NULL shift means lateness is NOT MEASURED.
	if !strings.Contains(body, "Lateness not measured") {
		t.Error("a venue with no shift does not say lateness is not measured for it")
	}
}

// TestLocationsSection_ACappedListNamesItsOwnCount.
//
// 🔴 THIS TEST EXISTS BECAUSE THE SENTENCE WAS FALSE. One count was rendered into
// both notices, so a business with a few venues and many departments was told "only
// the first <number of venues> departments are shown" -- a §4.6 claim about the
// business that is wrong and that no reader could tell from a right one. A truncated
// list that lies about how much it truncated is worse than one that stays silent,
// because it looks like it complied.
func TestLocationsSection_ACappedListNamesItsOwnCount(t *testing.T) {
	f := &fakeVenues{screen: tenant.VenueScreen{
		Venues: []tenant.Venue{
			{ID: uuid.New(), Name: "Venue A"},
			{ID: uuid.New(), Name: "Venue B"},
			{ID: uuid.New(), Name: "Venue C"},
		},
		VenuesCapped:      true,
		DepartmentsCapped: true,
	}}
	for i := range 7 {
		f.screen.Departments = append(f.screen.Departments, tenant.Department{
			ID: uuid.New(), LocationID: f.screen.Venues[0].ID,
			LocationName: "Venue A", Name: "Dept " + string(rune('A'+i)),
		})
	}
	b := venueBrowser(t, f)
	body := htmlOf(t, b.do(http.MethodGet, locationsHref, nil))

	if !strings.Contains(body, "Only the first 3 venues") {
		t.Errorf("the venue notice does not name the 3 venues actually shown")
	}
	if !strings.Contains(body, "Only the first 7 departments") {
		t.Errorf("the department notice does not name the 7 departments actually shown. " +
			"It used to print the VENUE count, which is a false statement about the " +
			"business in the exact place section 4.6 asks the product to be true.")
	}
	if strings.Contains(body, "Only the first 3 departments") {
		t.Error("the department notice is printing the venue count")
	}
}

// TestLocationsSection_ACappedListStillNamesItsCountAfterARejectedForm is the second
// half of the same repair. The rejected-form path builds the same view through a
// different function, and that copy set NO count at all -- so a capped list there
// rendered "Only the first  venues are shown", a sentence with a hole in it.
func TestLocationsSection_ACappedListStillNamesItsCountAfterARejectedForm(t *testing.T) {
	f := &fakeVenues{screen: tenant.VenueScreen{
		Venues:            []tenant.Venue{{ID: uuid.New(), Name: "Venue A"}, {ID: uuid.New(), Name: "Venue B"}},
		VenuesCapped:      true,
		DepartmentsCapped: true,
		Departments: []tenant.Department{
			{ID: uuid.New(), LocationID: uuid.New(), LocationName: "Venue A", Name: "Kitchen"},
		},
	}}
	b := venueBrowser(t, f)

	// An empty name is refused at the boundary, so this re-renders rather than saving.
	rec := b.do(http.MethodPost, venueSaveHref, url.Values{"name": {""}})
	if rec.Code != http.StatusOK {
		t.Fatalf("a rejected form answered %d, want 200 with the form back", rec.Code)
	}
	body := htmlOf(t, rec)
	if !strings.Contains(body, "Only the first 2 venues") {
		t.Error("after a rejected form the capped notice does not name its count; it read " +
			`"Only the first  venues are shown" before this was repaired`)
	}
	if !strings.Contains(body, "Only the first 1 departments") {
		t.Error("after a rejected form the capped department notice does not name its count")
	}
}

// TestLocationsSection_AFailedReadIsNeverAnEmptyList. Section 4.6: "this business has
// no venues" is a claim about a business, and a timeout is not evidence for it.
func TestLocationsSection_AFailedReadIsNeverAnEmptyList(t *testing.T) {
	f := &fakeVenues{screenErr: errors.New("connection refused")}
	b := venueBrowser(t, f)

	rec := b.do(http.MethodGet, locationsHref, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("a failed read answered %d, want 500", rec.Code)
	}
	if body := htmlOf(t, rec); strings.Contains(body, "No venues yet") {
		t.Error("a failed read rendered the EMPTY STATE, which tells a manager their " +
			"business has no venues on the strength of a database that did not answer")
	}
	// The same must hold on the re-render path, or a rejected form over a broken read
	// shows a form floating over a page that claims an empty business.
	rec = b.do(http.MethodPost, venueSaveHref, url.Values{"name": {""}})
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("a rejected form over a failed read answered %d, want 500", rec.Code)
	}
}

// TestLocationsSection_AnEmptyStateIsGatedOnHavingAsked is the positive control for
// the test above: when the read DID happen and returned nothing, the empty state is
// exactly what should render.
func TestLocationsSection_AnEmptyStateIsGatedOnHavingAsked(t *testing.T) {
	b := venueBrowser(t, &fakeVenues{})
	rec := b.do(http.MethodGet, locationsHref, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d, want 200", locationsHref, rec.Code)
	}
	body := htmlOf(t, rec)
	if !strings.Contains(body, "No venues yet") {
		t.Error("a tenant with no venues gets no empty state, so the test above proves " +
			"nothing about gating")
	}
	// 🔴 AND NO "ADD A DEPARTMENT" CONTROL, because a department must sit in a venue
	// (departments.location_id is NOT NULL) -- offering it would lead to a form whose
	// only submission the server must refuse.
	if strings.Contains(body, "Add a department") {
		t.Error("a business with no venues is offered an Add a department control, which " +
			"can only lead to a refusal")
	}
	if !strings.Contains(body, "Add a venue first") {
		t.Error("the departments empty state does not say why there is no control")
	}
}

// TestLocationsSection_AnUnresolvedCardIsAnswered. A stale bookmark, a hand-edited URL
// or another tenant's id all land here, and section 4.6 asks for the difference
// between "no card" and "no card plus a sentence".
func TestLocationsSection_AnUnresolvedCardIsAnswered(t *testing.T) {
	f := twoVenues()
	b := venueBrowser(t, f)

	tests := []struct {
		name  string
		query string
		want  string
	}{
		{"a venue id of another business", "?venue=" + uuid.NewString(), "could not find that venue"},
		{"a department id of another business", "?department=" + uuid.NewString(), "could not find that department"},
		// 🔴 "NOT A UUID" IS A DIFFERENT ANSWER FROM "NOT YOURS", and the section's
		// vocabulary already reserved a word for it: "unreadable" is defined as
		// "nothing was looked up", which is exactly what happens when the id does not
		// parse. Saying "that venue is not one of this business's" was a claim about a
		// lookup that never ran -- the same class as the two sentences below.
		{"a venue id that is not a uuid", "?venue=nonsense", "could not read that"},
		{"a department id that is not a uuid", "?department=nonsense", "could not read that"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := htmlOf(t, b.do(http.MethodGet, locationsHref+tc.query, nil))
			if !strings.Contains(body, tc.want) {
				t.Errorf("%s rendered no sentence; the manager sees a page that silently "+
					"did not do what they asked", tc.query)
			}
		})
	}

	// 🔴 AN UNVERIFIED REMOVAL WORD MUST NOT SILENCE THE REFUSAL, AND IT DID. The
	// unresolved-card check runs only when nothing else is being said, and a removal
	// word survived in v.Done even with no trail entry behind it — so the two combined
	// produced NEITHER an acknowledgement NOR the §4.6 sentence. Measured before the
	// repair, same session, same browser:
	//
	//	?venue=<foreign id>                           -> "We could not find that venue"
	//	?done=venue-deleted&id=<x>&venue=<foreign id>  -> NO BANNER AT ALL
	//
	// Only a hand-edited URL reaches it — which was also true of the defect that made
	// round 6 red, so it is closed rather than counted.
	for _, tc := range []struct{ name, query, want string }{
		{"an unverified removal word beside a foreign venue",
			"?done=venue-deleted&id=" + uuid.NewString() + "&venue=" + uuid.NewString(),
			"could not find that venue"},
		{"an unverified removal word beside a foreign department",
			"?done=department-deleted&id=" + uuid.NewString() + "&department=" + uuid.NewString(),
			"could not find that department"},
		{"an unverified removal word beside an unreadable venue id",
			"?done=venue-deleted&id=" + uuid.NewString() + "&venue=nonsense",
			"could not read that"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := htmlOf(t, b.do(http.MethodGet, locationsHref+tc.query, nil))
			if !strings.Contains(body, tc.want) {
				t.Errorf("%s rendered no sentence at all; an unbacked removal word "+
					"swallowed the refusal the section is required to make", tc.query)
			}
			// AND IT STILL SAYS NOTHING ABOUT A REMOVAL, because nothing was removed.
			if strings.Contains(strings.ToLower(body), "removed") {
				t.Errorf("%s printed a removal claim as well", tc.query)
			}
		})
	}

	// 🔴 THE ONE CASE THAT IS NOT AN ERROR: ?department=new with no venues. The form is
	// legitimately withheld and the empty state already explains it, so reporting
	// "unknown" would blame the manager for a state the screen explains.
	empty := venueBrowser(t, &fakeVenues{})
	body := htmlOf(t, empty.do(http.MethodGet, locationsHref+"?department=new", nil))
	if strings.Contains(body, "could not find that department") {
		t.Error(`"add a department" with no venues yet is reported as an unknown ` +
			`department; it is an explained state, not a mistake the manager made`)
	}
}

// TestLocationsSection_ARowPastTheCeilingIsStillEditable.
//
// 🔴 A CAP IS NOT A DISAPPEARANCE, AND THE PRODUCT USED TO SAY IT WAS. venueForms
// resolved ?venue=<id> by scanning the slice Screen had already TRUNCATED, so a venue
// past venuePageLimit could not be opened — and the page said "That venue is not one
// of this business's. Nothing was changed." Every clause of that is false: the row IS
// the business's, it is in the database, and RLS admits it. §4.6 is that a record is
// never lost; describing it wrongly is worse than staying silent.
//
// THIS TEST AND THE ONE BELOW ARE SEPARATE ON PURPOSE. Together they would pass if
// only one held, and which one held is the whole question.
func TestLocationsSection_ARowPastTheCeilingIsStillEditable(t *testing.T) {
	beyondVenue := uuid.New()
	beyondDept := uuid.New()
	f := twoVenues()
	// The row EXISTS in the business but is NOT in the capped list — the exact state
	// the ceiling produces.
	f.screen.VenuesCapped = true
	f.screen.DepartmentsCapped = true
	f.beyondVenues = map[uuid.UUID]tenant.Venue{
		beyondVenue: {
			ID: beyondVenue, Name: "KF Beyond The Cap",
			StaticIPs: []netip.Prefix{netip.MustParsePrefix("10.20.30.0/24")},
		},
	}
	f.beyondDepts = map[uuid.UUID]tenant.Department{
		beyondDept: {
			ID: beyondDept, LocationID: beyondVenue,
			LocationName: "KF Beyond The Cap", Name: "Pastry",
		},
	}
	b := venueBrowser(t, f)

	// ANTI-VACUITY: it really is absent from the rendered list, or "it opens" would
	// prove nothing about the fallback.
	list := htmlOf(t, b.do(http.MethodGet, locationsHref, nil))
	if strings.Contains(list, "KF Beyond The Cap") {
		t.Fatal("the fixture row is in the list, so the fallback is never exercised")
	}

	rec := b.do(http.MethodGet, locationsHref+"?venue="+beyondVenue.String(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET the beyond-cap venue: %d, want 200", rec.Code)
	}
	body := htmlOf(t, rec)
	if strings.Contains(body, "not one of this business's") {
		t.Error("a venue past the ceiling is reported as another business's. It is this " +
			"business's, it is in the database, and RLS admits it — the sentence is false.")
	}
	if !strings.Contains(body, "KF Beyond The Cap") || !strings.Contains(body, "10.20.30.0/24") {
		t.Error("the edit card for a venue past the ceiling did not open, so the row is " +
			"unreachable by browsing AND by direct link")
	}

	rec = b.do(http.MethodGet, locationsHref+"?department="+beyondDept.String(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET the beyond-cap department: %d, want 200", rec.Code)
	}
	body = htmlOf(t, rec)
	if strings.Contains(body, "not one of this business's") {
		t.Error("a department past the ceiling is reported as another business's")
	}
	if !strings.Contains(body, "Pastry") {
		t.Error("the edit card for a department past the ceiling did not open")
	}
}

// TestLocationsSection_AnotherBusinessIdIsStillRefused is the other half, and it has
// to be measured separately: the fallback added above must not turn "not yours" into
// "here you are". The fake answers ErrUnknownVenue for anything it was not given,
// which is what the tenant-scoped query does for a foreign id.
func TestLocationsSection_AnotherBusinessIdIsStillRefused(t *testing.T) {
	f := twoVenues()
	f.screen.VenuesCapped = true
	// Deliberately EMPTY: no id resolves through the fallback.
	f.beyondVenues = map[uuid.UUID]tenant.Venue{}
	f.beyondDepts = map[uuid.UUID]tenant.Department{}
	b := venueBrowser(t, f)

	foreign := uuid.New()
	body := htmlOf(t, b.do(http.MethodGet, locationsHref+"?venue="+foreign.String(), nil))
	if !strings.Contains(body, "not one of this business's") {
		t.Error("another business's venue id no longer gets the refusal sentence; the " +
			"fallback has widened what this section will open")
	}
	if strings.Contains(body, `name="static_ips"`) {
		t.Error("an edit card opened for a foreign venue id")
	}

	body = htmlOf(t, b.do(http.MethodGet, locationsHref+"?department="+uuid.NewString(), nil))
	if !strings.Contains(body, "not one of this business's") {
		t.Error("another business's department id no longer gets the refusal sentence")
	}
}

// TestLocationsSection_TheFallbackIsNotPaidForOnTheCommonPath. The whole estate is on
// the page for every tenant that exists today (measured: largest is 9 locations
// against a ceiling of 200), so opening a card must not cost a second round trip.
func TestLocationsSection_TheFallbackIsNotPaidForOnTheCommonPath(t *testing.T) {
	f := twoVenues()
	b := venueBrowser(t, f)

	b.do(http.MethodGet, locationsHref+"?venue="+f.firstVenueID().String(), nil)
	b.do(http.MethodGet, locationsHref+"?department="+f.firstDepartmentID().String(), nil)
	if f.byIDCalls != 0 {
		t.Errorf("opening two cards already on the page cost %d by-id read(s); the slice "+
			"scan is supposed to answer them", f.byIDCalls)
	}
	// THE CONTROL: a miss DOES reach the fallback, or the assertion above would pass
	// on a section that never calls it at all.
	b.do(http.MethodGet, locationsHref+"?venue="+uuid.NewString(), nil)
	if f.byIDCalls != 1 {
		t.Errorf("a miss cost %d by-id read(s), want 1", f.byIDCalls)
	}
}

// TestLocationsSection_AFailedFallbackReadIsAnErrorNotASentence (§4.6). "We could not
// find that venue" would be a claim about the business built on a database that did
// not answer — the same rule the section's list read already follows.
func TestLocationsSection_AFailedFallbackReadIsAnErrorNotASentence(t *testing.T) {
	f := twoVenues()
	f.byIDErr = errors.New("connection refused")
	b := venueBrowser(t, f)

	rec := b.do(http.MethodGet, locationsHref+"?venue="+uuid.NewString(), nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("a failed fallback read answered %d, want 500", rec.Code)
	}
	if body := htmlOf(t, rec); strings.Contains(body, "not one of this business's") {
		t.Error("a failed read was reported as 'that venue is not yours', which is a " +
			"claim about the business made on the strength of a database that did not answer")
	}
}

// TestLocationsSection_AVenueSaysHowManyDepartmentsItReallyHas.
//
// 🔴 THE COUNT WAS MEASURED AND THROWN AWAY WHILE A POSSIBLY-SHORT LIST WAS SHOWN.
// ListPanelLocations counts each venue's departments with its own tenant-scoped
// subquery; the NAMES on the card come from screen.Departments, which the ceiling may
// have cut. Rendering only the names lets a venue quietly read as smaller than it is.
func TestLocationsSection_AVenueSaysHowManyDepartmentsItReallyHas(t *testing.T) {
	venue := uuid.New()
	f := &fakeVenues{screen: tenant.VenueScreen{
		Venues: []tenant.Venue{{
			ID: venue, Name: "KF Big",
			StaticIPs:       []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")},
			DepartmentCount: 7, // what the database counted
		}},
		// ...but only two names survived the cap.
		Departments: []tenant.Department{
			{ID: uuid.New(), LocationID: venue, LocationName: "KF Big", Name: "Kitchen"},
			{ID: uuid.New(), LocationID: venue, LocationName: "KF Big", Name: "Bar"},
		},
		DepartmentsCapped: true,
	}}
	b := venueBrowser(t, f)
	body := htmlOf(t, b.do(http.MethodGet, locationsHref, nil))

	if !strings.Contains(body, "Departments (7)") {
		t.Error("the venue card does not print the count the database measured, so a " +
			"reader takes the two names shown for the whole of it")
	}
	if !strings.Contains(body, "only 2 named here") {
		t.Error("the card lists fewer names than it counts and does not say so (§4.6)")
	}

	// THE CONTROL: when they agree, the card does NOT nag.
	f2 := twoVenues()
	f2.screen.Venues[0].DepartmentCount = 1
	b2 := venueBrowser(t, f2)
	body2 := htmlOf(t, b2.do(http.MethodGet, locationsHref, nil))
	if !strings.Contains(body2, "Departments (1)") {
		t.Error("control: a venue with one department does not print its count")
	}
	if strings.Contains(body2, "named here") {
		t.Error("control: a complete list is described as incomplete")
	}
}

// TestLocationsSection_SaysNothingAboutACardItNeverLookedFor.
//
// 🔴 A SENTENCE ABOUT A SEARCH THAT NEVER RAN IS THE SAME DEFECT AS A SENTENCE ABOUT
// A ROW THAT EXISTS. venueForms opens at most ONE card and takes ?venue= first, so
// with both parameters present the department was never looked for — yet the section
// reported "That department is not one of this business's", with no department in the
// request to be missing. Measured on the real router: ?venue=new&department=new
// rendered the Add-a-venue form under that sentence.
func TestLocationsSection_SaysNothingAboutACardItNeverLookedFor(t *testing.T) {
	f := twoVenues()
	b := venueBrowser(t, f)

	for _, q := range []string{
		"?venue=new&department=new",
		"?venue=new&department=" + uuid.NewString(),
		"?department=new&venue=new",
		"?venue=" + f.firstVenueID().String() + "&department=new",
	} {
		t.Run(q, func(t *testing.T) {
			rec := b.do(http.MethodGet, locationsHref+q, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s: %d, want 200", q, rec.Code)
			}
			body := htmlOf(t, rec)
			if strings.Contains(body, "could not find that department") {
				t.Error("the section reports a department it never searched for; ?venue= " +
					"wins and opens the only card, so nothing about a department was asked")
			}
			// ANTI-VACUITY: the venue card really did open, or "no complaint" would be
			// true of a page that did nothing at all.
			if !strings.Contains(body, `name="static_ips"`) {
				t.Error("no venue card opened, so this case proves nothing")
			}
		})
	}
}

// --- the writes -------------------------------------------------------------------

// TestSaveVenue_TheTenantAndTheActorComeFromTheSession (§4.5, §4.7).
//
// 🔴 THE FORM IS GIVEN FIELDS FOR BOTH AND THEY MUST BE IGNORED. There is no control
// on the screen for either, but "the template does not render it" is a statement
// about a template; what is under test is that the HANDLER does not read one.
func TestSaveVenue_TheTenantAndTheActorComeFromTheSession(t *testing.T) {
	f := twoVenues()
	b := venueBrowser(t, f)

	foreignTenant, foreignActor := uuid.New(), uuid.New()
	rec := b.do(http.MethodPost, venueSaveHref, url.Values{
		"name":       {"KF Sliema"},
		"static_ips": {"192.168.2.0/24"},
		// The forged fields.
		"tenant_id": {foreignTenant.String()},
		"actor_id":  {foreignActor.String()},
		"TenantID":  {foreignTenant.String()},
		"ActorID":   {foreignActor.String()},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("a valid save answered %d, want 303", rec.Code)
	}
	venues, _ := f.saved()
	if len(venues) != 1 {
		t.Fatalf("the section built %d command(s), want 1", len(venues))
	}
	if venues[0].TenantID != panelTestTenant {
		t.Errorf("the command carries tenant %s; the session's tenant is %s and the "+
			"request must not be able to name another", venues[0].TenantID, panelTestTenant)
	}
	if venues[0].ActorID != panelTestAdmin {
		t.Errorf("the command carries actor %s; the trail row would be attributed to "+
			"somebody who did not do it", venues[0].ActorID)
	}
	if venues[0].VenueID != uuid.Nil {
		t.Errorf("a form with no id built an UPDATE of %s rather than a create", venues[0].VenueID)
	}
}

// TestSaveVenue_AnEmptyBoxReachesTheDomainAsAnAbsence carries the parse-level property
// all the way through the handler, because the boundary functions being right says
// nothing about whether saveVenue passes their answers along.
func TestSaveVenue_AnEmptyBoxReachesTheDomainAsAnAbsence(t *testing.T) {
	f := twoVenues()
	b := venueBrowser(t, f)

	rec := b.do(http.MethodPost, venueSaveHref, url.Values{
		"name": {"KF Bare"},
		// static_ips, gps and the shift are all left empty, which is the state a
		// manager reaches by adding a venue and pressing Save.
		"static_ips": {""}, "gps_lat": {""}, "gps_lng": {""},
		"shift_start": {""}, "shift_end": {""}, "wifi_ssid": {""},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("answered %d, want 303", rec.Code)
	}
	venues, _ := f.saved()
	if len(venues) != 1 {
		t.Fatalf("built %d command(s), want 1", len(venues))
	}
	c := venues[0]
	if c.Shift != nil {
		t.Errorf("an empty shift reached the domain as %+v; NULL means lateness is not "+
			"computed and 00:00 means every arrival is late", *c.Shift)
	}
	if c.GPS != nil {
		t.Errorf("an empty coordinate reached the domain as %+v; 0,0 is a real place", *c.GPS)
	}
	if c.StaticIPs == nil {
		t.Error("an empty address list reached the domain as nil rather than as an empty " +
			"slice; static_ips is NOT NULL and a nil parameter is a 500")
	}
	if len(c.StaticIPs) != 0 {
		t.Errorf("an empty address list became %v", c.StaticIPs)
	}
	if c.WiFiSSID != "" {
		t.Errorf("an empty network name became %q", c.WiFiSSID)
	}
}

// TestSaveVenue_ARejectedFormKeepsTheManagersTyping.
//
// 🔴 THIS SECTION BREAKS THE PANEL'S OWN REDIRECT PATTERN AND THAT IS THE POINT. Every
// other panel write answers 303 with a word from a closed vocabulary, which works when
// the input is one id and one verb. This form carries eight fields, and a redirect
// would throw away everything the manager typed to report that one of them was wrong.
func TestSaveVenue_ARejectedFormKeepsTheManagersTyping(t *testing.T) {
	f := twoVenues()
	b := venueBrowser(t, f)

	rec := b.do(http.MethodPost, venueSaveHref, url.Values{
		"name":       {"KF Msida"},
		"static_ips": {"192.168.9.0/24"},
		"gps_lat":    {"35.9"}, // a latitude with no longitude -- the refusal
		"wifi_ssid":  {"KF-Msida-Staff"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("a rejected form answered %d; a redirect would lose the other seven "+
			"fields", rec.Code)
	}
	body := htmlOf(t, rec)
	for _, kept := range []string{"KF Msida", "192.168.9.0/24", "35.9", "KF-Msida-Staff"} {
		if !strings.Contains(body, kept) {
			t.Errorf("the re-rendered form lost %q, so the manager retypes eight fields "+
				"to fix one", kept)
		}
	}
	if !strings.Contains(body, "needs both a latitude and a longitude") {
		t.Error("the re-rendered form does not say WHICH field was wrong or why")
	}
	if !strings.Contains(body, `aria-invalid="true"`) {
		t.Error("no control is marked aria-invalid, so a manager using a screen reader " +
			"is told nothing")
	}
	// 🔴 AND NOTHING WAS WRITTEN. A form that re-renders having already saved would be
	// worse than one that redirects.
	if venues, _ := f.saved(); len(venues) != 0 {
		t.Errorf("a refused form still built %d command(s)", len(venues))
	}
}

// TestSaveVenue_AnIncomparableCoordinateNeverReachesTheDatabase.
//
// 🔴 THE PARSE-LEVEL TEST IS NOT ENOUGH HERE, because the defect was only VISIBLE end
// to end. parseGPS returning a *GPS carrying NaN looks harmless in isolation; what
// happened next was FormatFloat -> "NaN" -> pgtype.Numeric (which accepts it) ->
// Postgres -> `violates check constraint "locations_gps_lat_check"` (23514) -> a 500
// rendered as "the panel is unavailable". A signed-in manager who typed NaN was told
// OUR SYSTEM was broken, not their input, and lost the other seven fields.
//
// SO THIS ASSERTS THE THREE THINGS THE MANAGER EXPERIENCES: not a 500, a sentence
// about the field, and their typing still on the page.
func TestSaveVenue_AnIncomparableCoordinateNeverReachesTheDatabase(t *testing.T) {
	for _, tc := range []struct{ name, lat, lng string }{
		{"NaN latitude", "NaN", "14.4"},
		{"nan latitude", "nan", "14.4"},
		{"NaN longitude", "35.9", "NaN"},
		{"Inf latitude", "Inf", "14.4"},
		{"underflowing latitude", "1e-400", "14.4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := twoVenues()
			b := venueBrowser(t, f)
			rec := b.do(http.MethodPost, venueSaveHref, url.Values{
				"name": {"KF Msida"}, "static_ips": {"192.168.9.0/24"},
				"gps_lat": {tc.lat}, "gps_lng": {tc.lng},
				"shift_start": {"08:00"}, "shift_end": {"17:00"},
				"wifi_ssid": {"KF-Msida-Staff"},
			})
			if rec.Code == http.StatusInternalServerError {
				t.Fatalf("%s/%s answered 500. The manager is told the PANEL is broken "+
					"rather than that their coordinate is, and every other field they "+
					"typed is gone.", tc.lat, tc.lng)
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("answered %d, want 200 with the form back", rec.Code)
			}
			body := htmlOf(t, rec)
			// The refusal names the FORMAT, because that is what is wrong: NaN and
			// 1e-400 are not numbers a coordinate box means, and neither is out of range.
			if !strings.Contains(body, "must be a plain number") {
				t.Error("no sentence names the coordinate field and what to do about it")
			}
			if strings.Contains(body, "must be between") {
				t.Error("an unreadable coordinate was reported as OUT OF RANGE; the value " +
					"has no range, and the manager is sent to fix the wrong thing")
			}
			for _, kept := range []string{"KF Msida", "192.168.9.0/24", "KF-Msida-Staff"} {
				if !strings.Contains(body, kept) {
					t.Errorf("the re-rendered form lost %q", kept)
				}
			}
			// 🔴 AND NOTHING REACHED THE DOMAIN. §7: the domain sees data that is
			// already good, so a GPS carrying NaN must not exist past this boundary.
			venues, _ := f.saved()
			if len(venues) != 0 {
				t.Fatalf("§7 VIOLATED: the domain was handed %+v", venues[0].GPS)
			}
		})
	}
}

// TestWithinRange_IsFalseForEveryIncomparableFloat pins the RANGE guard on its own.
//
// 🔴 IT EXISTS BECAUSE AN AUDIT SHOWED THE GUARD WAS UNPINNED. The claim was that
// writing the bound as an inclusion is "the whole repair" for NaN — but plainDecimalRE
// rejects "NaN" before withinRange is ever reached, so reverting withinRange to the
// old `f < -limit || f > limit` left the whole suite GREEN. A defence whose removal no
// test notices is not a defence. This calls it DIRECTLY, with the values no string can
// carry past the syntax gate, so the two guards are pinned where each one acts.
func TestWithinRange_IsFalseForEveryIncomparableFloat(t *testing.T) {
	tests := []struct {
		name  string
		value float64
		limit float64
		want  bool
	}{
		// 🔴 THE CASES THE OLD SPELLING ADMITTED. Every comparison against NaN is
		// false, so `f < -90 || f > 90` was false for it and the value passed.
		{"NaN against the latitude bound", math.NaN(), 90, false},
		{"NaN against the longitude bound", math.NaN(), 180, false},
		{"negative NaN is still NaN", -math.NaN(), 90, false},
		{"positive infinity", math.Inf(1), 90, false},
		{"negative infinity", math.Inf(-1), 90, false},
		// THE CONTROLS: a guard that answered false for everything would satisfy the
		// rows above and be useless.
		{"a real latitude", 35.917270, 90, true},
		{"a negative latitude", -33.8688, 90, true},
		{"zero is inside", 0, 90, true},
		{"exactly the bound is inside", 90, 90, true},
		{"exactly the negative bound is inside", -90, 90, true},
		{"just past the bound is outside", 90.000001, 90, false},
		{"just past the negative bound is outside", -90.000001, 90, false},
		{"a longitude the latitude bound refuses", 120, 90, false},
		{"the same longitude inside its own bound", 120, 180, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := withinRange(tc.value, tc.limit); got != tc.want {
				t.Errorf("withinRange(%v, %v) = %v, want %v. An incomparable float that "+
					"passes here reaches numeric(9,6) as NaN, which pgtype accepts and the "+
					"column refuses as a 23514 — a 500 the manager reads as 'the panel is "+
					"broken'.", tc.value, tc.limit, got, tc.want)
			}
		})
	}
}

// TestParseGPS_SaysWhetherTheSHAPEOrTheVALUEIsWrong.
//
// 🔴 ONE MESSAGE FOR BOTH TOLD MANAGERS SOMETHING FALSE. "35,9" is a perfectly good
// latitude written the way Malta and Türkiye write it — the VALUE is inside the range
// — and the product answered "must be a number between -90 and 90". Somebody typed
// the right number and was told it was out of bounds, with no hint about what to
// change. The field carries inputmode="decimal", so a phone offers whatever separator
// the locale has.
func TestParseGPS_SaysWhetherTheSHAPEOrTheVALUEIsWrong(t *testing.T) {
	formatCases := []struct{ name, lat, lng string }{
		{"a degree sign", "35.9°", "14.4"},
		{"a compass letter", "35.9 N", "14.4"},
		{"degrees, minutes and seconds", `35° 54' 3"`, "14.4"},
		{"a thousands separator", "35,917,270", "14.4"},
		{"a European thousands-and-decimal pair", "1.234,56", "14.4"},
		{"a pasted coordinate PAIR", "35,9,14,4", "14.4"},
		{"a full-width digit", "\uFF13\uFF15.9", "14.4"},
		{"an Arabic-Indic digit", "\u0663\u0665.9", "14.4"},
		{"a bare separator", ",", "14.4"},
		{"a bare sign", "-", "14.4"},
		{"a NUL byte", "35.9\x00", "14.4"},
		{"NaN", "NaN", "14.4"},
		{"a hex float", "0x1p3", "14.4"},
	}
	for _, tc := range formatCases {
		t.Run("format/"+tc.name, func(t *testing.T) {
			_, msg := parseGPS(tc.lat, tc.lng)
			if msg == "" {
				t.Fatalf("%q/%q was accepted", tc.lat, tc.lng)
			}
			if !strings.Contains(msg, "plain number") {
				t.Errorf("the refusal for %q does not name the FORMAT: %q", tc.lat, msg)
			}
			// 🔴 AND IT MUST NOT CLAIM THE VALUE IS OUT OF RANGE, which for "35,9" is
			// simply untrue and sends the manager to change a correct number.
			if strings.Contains(msg, "must be between") {
				t.Errorf("%q was reported as out of range; it is a FORMAT problem and the "+
					"value is inside the range: %q", tc.lat, msg)
			}
			// The message has to say what to do, or it is a scold.
			// 🔴 THE SENTENCE IS CHECKED AGAINST THE PARSER, NOT AGAINST A STRING.
			// An earlier version asserted `!strings.Contains(msg, "no comma")`, and an
			// audit defeated it in one edit: a message reading "commas are rejected,
			// use a full stop" — forbidding something the code ACCEPTS — left the
			// package green. Absence of a phrase is not truth. So the two claims are
			// derived from ONE source below: every example the sentence offers must
			// PARSE, and every separator it forbids must really be refused.
			assertMessageAgreesWithTheParser(t, msg)
		})
	}

	rangeCases := []struct{ name, lat, lng, want string }{

		{"a latitude past the pole", "91", "14.4", "Latitude must be between -90 and 90."},
		{"a negative latitude past the pole", "-90.5", "14.4", "Latitude must be between -90 and 90."},
		{"a longitude past the meridian", "35.9", "181", "Longitude must be between -180 and 180."},
	}
	for _, tc := range rangeCases {
		t.Run("range/"+tc.name, func(t *testing.T) {
			_, msg := parseGPS(tc.lat, tc.lng)
			if msg != tc.want {
				t.Errorf("parseGPS(%q, %q) said %q, want %q", tc.lat, tc.lng, msg, tc.want)
			}
		})
	}
}

// TestParseGPS_NormalisesTheSeparatorsAPhoneOffers (user decision, 2026-08-08).
//
// 🔴 THE DECISION RESTS ON A PROPERTY OF COORDINATES, NOT ON A PROPERTY OF COMMAS. A
// comma is ambiguous in general — 1,234 is either 1.234 or 1234 — but a latitude or
// longitude carries AT MOST THREE INTEGER DIGITS, so a thousands separator has no
// legitimate appearance in this field. The half of the property that has to be
// measured rather than reasoned about is that nothing which used to be refused
// becomes accepted, so the reject list below is longer than the accept list.
func TestParseGPS_NormalisesTheSeparatorsAPhoneOffers(t *testing.T) {
	accepted := []struct {
		name string
		lat  string
		lng  string
		want tenant.GPS
	}{
		{"a decimal comma, as Malta and Türkiye write it", "35,9", "14,4",
			tenant.GPS{Lat: 35.9, Lng: 14.4}},
		{"full precision with commas", "35,917270", "14,489540",
			tenant.GPS{Lat: 35.917270, Lng: 14.489540}},
		{"a comma with an ASCII minus", "-33,8688", "-70,6693",
			tenant.GPS{Lat: -33.8688, Lng: -70.6693}},
		// U+2212 MINUS SIGN: macOS and word processors substitute it for the hyphen
		// silently, and it is invisible in a form field.
		{"a Unicode minus with a full stop", "−33.8688", "−70.6693",
			tenant.GPS{Lat: -33.8688, Lng: -70.6693}},
		{"a Unicode minus with a comma", "−33,8688", "−70,6693",
			tenant.GPS{Lat: -33.8688, Lng: -70.6693}},
		// The shapes that already worked must keep working.
		{"a full stop still works", "35.917270", "14.489540",
			tenant.GPS{Lat: 35.917270, Lng: 14.489540}},
		{"surrounding whitespace", "  35.9  ", "\t14.4\n", tenant.GPS{Lat: 35.9, Lng: 14.4}},
	}
	for _, tc := range accepted {
		t.Run("accepted/"+tc.name, func(t *testing.T) {
			got, msg := parseGPS(tc.lat, tc.lng)
			if msg != "" {
				t.Fatalf("parseGPS(%q, %q) was refused: %s", tc.lat, tc.lng, msg)
			}
			if got == nil {
				t.Fatal("a filled coordinate became nil")
			}
			if *got != tc.want {
				t.Errorf("parseGPS(%q, %q) = %+v, want %+v", tc.lat, tc.lng, *got, tc.want)
			}
		})
	}

	// 🔴 NOTHING BELOW MAY BECOME ACCEPTABLE. Every one of these is refused today and
	// must still be refused AFTER normalising — the ones with two separators are the
	// reason the decision is safe, so they are the ones that would silently break it.
	refused := []struct{ name, value string }{
		{"a pasted coordinate pair", "35,9,14,4"},
		{"a European thousands-and-decimal pair", "1.234,56"},
		{"a thousands separator", "35,917,270"},
		{"a Google Maps clipboard pair", "35.917270, 14.489540"},
		{"a degree sign", "35.9°"},
		{"a compass letter", "35.9 N"},
		{"degrees, minutes and seconds", `35° 54' 3"`},
		{"full-width digits", "３５.9"},
		{"Arabic-Indic digits", "٣٥.9"},
		{"an underflowing exponent", "1e-400"},
		{"a hex float", "0x1p3"},
		{"an upper-case hex float", "0X1P-2"},
		{"underscore digit separators", "1_0.5"},
		{"NaN", "NaN"},
		{"lower-case nan", "nan"},
		{"positive infinity", "+Inf"},
		{"negative infinity", "-Inf"},
		{"a bare minus", "-"},
		{"a bare plus", "+"},
		{"a bare full stop", "."},
		{"a bare comma", ","},
		{"a NUL byte", "35.9\x00"},
	}
	for _, tc := range refused {
		t.Run("refused/"+tc.name, func(t *testing.T) {
			if got, msg := parseGPS(tc.value, "14.4"); msg == "" {
				t.Errorf("latitude %q was ACCEPTED as %+v after normalising", tc.value, got)
			}
			// BOTH FIELDS, because the replacer is applied per coordinate and a fix
			// that reached only the latitude would be invisible here otherwise.
			if got, msg := parseGPS("35.9", tc.value); msg == "" {
				t.Errorf("longitude %q was ACCEPTED as %+v after normalising", tc.value, got)
			}
		})
	}
}

// TestCoordinateNormalisation_TouchesNoOtherField.
//
// 🔴 A COMMA MEANS SOMETHING DIFFERENT IN EVERY OTHER FIELD ON THIS FORM, so the
// replacer must not leak out of the coordinate boxes. A venue may be called "Kebab
// Factory, Ltd"; an SSID may carry a comma; parseRanges uses the comma as the
// SEPARATOR between address ranges; and a shift is a clock, where "08,00" is not a
// time and must be refused rather than quietly read as one.
func TestCoordinateNormalisation_TouchesNoOtherField(t *testing.T) {
	t.Run("a venue name keeps its comma", func(t *testing.T) {
		const name = "Kebab Factory, Ltd"
		got, msg := parseName(name, "venue")
		if msg != "" {
			t.Fatalf("parseName(%q) was refused: %s", name, msg)
		}
		if got != name {
			t.Errorf("parseName rewrote the name to %q; a manager's punctuation is theirs", got)
		}
	})

	t.Run("an SSID keeps its comma", func(t *testing.T) {
		const ssid = "KF,Staff"
		got, msg := parseSSID(ssid)
		if msg != "" || got != ssid {
			t.Errorf("parseSSID(%q) = %q, %q; the network name was rewritten", ssid, got, msg)
		}
	})

	t.Run("the replacer is referenced from exactly one function in the whole package", func(t *testing.T) {
		// 🔴 THIS SUBCASE HAS BEEN WRONG TWICE AND BOTH FAILURES WERE SCOPE FAILURES.
		// It began as a behavioural check that "08,00" is REFUSED as a shift — which
		// parseClock does on its own — and an audit applied the replacer inside
		// parseShift with the case still GREEN. It was then rewritten as a hand-written
		// line scanner over locations.go, and a second audit leaked through the FILE
		// boundary: `Name: coordinateSeparators.Replace(r.PostFormValue("name"))` in
		// locationactions.go silently turned "Kebab Factory, Ltd" into "Kebab Factory.
		// Ltd" — on the rejected-form path, whose entire purpose is to give a manager
		// back what they typed — and the package stayed green.
		//
		// 🔴 SO IT IS go/ast OVER THE WHOLE PACKAGE, WHICH IS THE SHAPE THE §4.5 BELT
		// NET ALREADY USES (internal/domain/tenant/query_test.go) AND FOR THE SAME
		// REASON: resolving *ast.Ident leaves SCOPE to the language instead of to a
		// string match, and the two defects above were both a hand-rolled scanner
		// disagreeing with the language about what "here" means.
		//
		// ⚠️ WHAT STILL ESCAPES IT, MEASURED RATHER THAN CLAIMED SHUT — and an earlier
		// draft of this very paragraph got its own limits wrong, which is why each line
		// below names the mutation that produced it.
		//
		//	CAUGHT   the replacer used in another function, in another FILE
		//	         (S1: `Name: coordinateSeparators.Replace(...)` in
		//	         locationactions.go) -> RED. This is the defect an audit found.
		//	CAUGHT   an ALIAS taken in a disallowed function
		//	         (S4: `sneaky := coordinateSeparators` inside parseName) -> RED,
		//	         because the identifier still appears there.
		//	ESCAPES  an alias taken INSIDE parseCoordinate and stored in a package
		//	         variable (S2) -> GREEN. Every reference the net can see is the
		//	         legal one; the value travels as data.
		//	ESCAPES  the METHOD VALUE taken inside parseCoordinate
		//	         (S3: `leakedReplace = coordinateSeparators.Replace`) -> GREEN,
		//	         same reason.
		//
		// So the honest boundary is: the net holds against USE anywhere in the package,
		// and does not hold against EXPORT from the one function allowed to touch it.
		// Both escapes require an edit inside parseCoordinate — the single function a
		// reviewer of this rule would read — and neither is reachable by editing any
		// other file, which is where the real defect happened. A counted hole is safer
		// than one claimed shut.
		fset := token.NewFileSet()
		pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
			return !strings.HasSuffix(fi.Name(), "_test.go")
		}, 0)
		if err != nil {
			t.Fatalf("parsing the package: %v", err)
		}
		if len(pkgs) == 0 {
			t.Fatal("go/ast parsed no non-test package here; this net is reading nothing")
		}

		// enclosing maps a position onto the function declaration that contains it, so
		// a reference can be attributed without any assumption about layout.
		type ref struct{ fn, file string }
		var refs []ref
		files := 0
		for _, pkg := range pkgs {
			for name, f := range pkg.Files {
				files++
				base := filepath.Base(name)
				// Walk every function, then the file's remaining top-level declarations.
				ast.Inspect(f, func(n ast.Node) bool {
					fd, ok := n.(*ast.FuncDecl)
					if !ok {
						return true
					}
					ast.Inspect(fd, func(inner ast.Node) bool {
						if id, ok := inner.(*ast.Ident); ok && id.Name == coordinateReplacerIdent {
							refs = append(refs, ref{fn: fd.Name.Name, file: base})
						}
						return true
					})
					return false // its body is walked above
				})
				for _, d := range f.Decls {
					gd, ok := d.(*ast.GenDecl)
					if !ok {
						continue
					}
					ast.Inspect(gd, func(inner ast.Node) bool {
						if id, ok := inner.(*ast.Ident); ok && id.Name == coordinateReplacerIdent {
							refs = append(refs, ref{fn: "<file scope>", file: base})
						}
						return true
					})
				}
			}
		}

		// ANTI-VACUITY, TWO WAYS. The parse saw the whole package, and it found the
		// declaration plus the one legitimate call — a net that found nothing would
		// pass this rule over any leak at all.
		if files < 5 {
			t.Fatalf("go/ast parsed %d file(s) of internal/handler; it is not reading the "+
				"package and this net proves nothing", files)
		}
		if len(refs) < 2 {
			t.Fatalf("the net found %d reference(s) to %s (%v); it should see at least its "+
				"declaration and its one caller", len(refs), coordinateReplacerIdent, refs)
		}

		allowed := map[string]bool{
			"parseCoordinate": true, // the one caller
			"<file scope>":    true, // its own declaration
		}
		for _, r := range refs {
			if !allowed[r.fn] {
				t.Errorf("%s is referenced in %s (%s). A comma means something DIFFERENT in "+
					"every other field this package handles — a venue may be named \"Kebab "+
					"Factory, Ltd\", parseRanges SPLITS on commas, and a shift is a clock. "+
					"Normalising anywhere but the coordinate boxes silently rewrites a "+
					"manager's own data, and the rejected-form path is where that is worst: "+
					"it exists to hand back exactly what they typed.",
					coordinateReplacerIdent, r.fn, r.file)
			}
		}
	})

	t.Run("a comma-separated shift is still refused by the shift's own sentence", func(t *testing.T) {
		// The behavioural half, kept because it pins the MESSAGE rather than the leak:
		// a manager who types 08,00 must be told it is not a time of day.
		got, msg := parseShift("08,00", "17,00", false)
		if msg == "" {
			t.Fatalf("a comma-separated shift was ACCEPTED as %+v", got)
		}
		if got != nil {
			t.Errorf("a refused shift produced %+v", *got)
		}
		if !strings.Contains(msg, "time of day") {
			t.Errorf("the refusal is not the shift's own sentence: %q", msg)
		}
	})

	t.Run("the comma stays a separator in the address list", func(t *testing.T) {
		// parseRanges SPLITS on commas. If normalisation reached it, "192.168.1.0/24,
		// 10.0.0.0/8" would become one unparseable token instead of two ranges.
		got, msg := parseRanges("192.168.1.0/24, 10.0.0.0/8")
		if msg != "" {
			t.Fatalf("parseRanges was refused: %s", msg)
		}
		if len(got) != 2 {
			t.Fatalf("parseRanges returned %d range(s), want 2: %v", len(got), got)
		}
	})
}

// TestSaveVenue_ARefusalFromTheDomainBecomesASentence. Section 4.6: a refused action
// is never silent, and it must not be a 500 either -- "no such venue in this business"
// is an answer, not a failure.
func TestSaveVenue_ARefusalFromTheDomainBecomesASentence(t *testing.T) {
	f := twoVenues()
	f.saveErr = tenant.ErrUnknownVenue
	b := venueBrowser(t, f)

	rec := b.do(http.MethodPost, venueSaveHref, url.Values{
		"id":   {uuid.NewString()},
		"name": {"Somebody else's venue"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("answered %d, want 303 to a page carrying the sentence", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "problem=unknown-venue") {
		t.Fatalf("redirected to %q, which carries no explanation", loc)
	}
	body := htmlOf(t, b.do(http.MethodGet, loc, nil))
	if !strings.Contains(body, "not one of this business's") {
		t.Error("the page the refusal redirects to says nothing about it")
	}
}

// TestSaveDepartment_AForeignVenueSendsTheManagerToTheVenueList. "unknown-venue" and
// "unknown-department" are separate words because they send a manager to different
// places: one to the venue list, one to the department they were editing.
func TestSaveDepartment_AForeignVenueSendsTheManagerToTheVenueList(t *testing.T) {
	f := twoVenues()
	f.deptErr = tenant.ErrUnknownVenue
	b := venueBrowser(t, f)

	rec := b.do(http.MethodPost, departmentSaveHref, url.Values{
		"location_id": {uuid.NewString()},
		"name":        {"Kitchen"},
	})
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "problem=unknown-venue") {
		t.Errorf("a department aimed at a foreign venue redirected to %q, want the "+
			"unknown-venue word", loc)
	}

	f2 := twoVenues()
	f2.deptErr = tenant.ErrUnknownDepartment
	b2 := venueBrowser(t, f2)
	rec2 := b2.do(http.MethodPost, departmentSaveHref, url.Values{
		"id":   {uuid.NewString()},
		"name": {"Kitchen"},
	})
	if loc := rec2.Header().Get("Location"); !strings.Contains(loc, "problem=unknown-department") {
		t.Errorf("an edit of a foreign department redirected to %q, want the "+
			"unknown-department word", loc)
	}
}

// TestSaveDepartment_TheVenueIsReadOnlyOnCreate. UpdateDepartment does not name
// location_id, so accepting one on an edit would be accepting a field the server
// ignores -- the manager would be told a move happened that did not.
func TestSaveDepartment_TheVenueIsReadOnlyOnCreate(t *testing.T) {
	f := twoVenues()
	b := venueBrowser(t, f)
	deptID := f.firstDepartmentID()
	elsewhere := f.screen.Venues[1].ID

	rec := b.do(http.MethodPost, departmentSaveHref, url.Values{
		"id":          {deptID.String()},
		"location_id": {elsewhere.String()}, // the field the server must not read
		"name":        {"Kitchen"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("answered %d, want 303", rec.Code)
	}
	_, depts := f.saved()
	if len(depts) != 1 {
		t.Fatalf("built %d command(s), want 1", len(depts))
	}
	if depts[0].LocationID != uuid.Nil {
		t.Errorf("an EDIT carried LocationID %s into the domain. UpdateDepartment has no "+
			"location_id in its SET list, so this value can only ever be ignored -- "+
			"reading it invites somebody to start honouring it.", depts[0].LocationID)
	}
	if depts[0].DepartmentID != deptID {
		t.Errorf("the command edits %s, want %s", depts[0].DepartmentID, deptID)
	}

	// THE CONTROL: a CREATE does read it, or nothing could ever be created.
	f2 := twoVenues()
	b2 := venueBrowser(t, f2)
	venue := f2.firstVenueID()
	if rec := b2.do(http.MethodPost, departmentSaveHref, url.Values{
		"location_id": {venue.String()}, "name": {"Bar"},
	}); rec.Code != http.StatusSeeOther {
		t.Fatalf("a create answered %d, want 303", rec.Code)
	}
	if _, depts := f2.saved(); len(depts) != 1 || depts[0].LocationID != venue {
		t.Fatalf("a create did not carry the chosen venue; the assertion above would be "+
			"vacuous. got %+v", depts)
	}
}

// TestSaveDepartment_ARejectedEditStillNamesItsVenue.
//
// 🔴 THE WHOLE REJECTED-DEPARTMENT PATH HAD NO TEST AT ALL, AND IT WAS BROKEN. Three
// tests drove the rejected VENUE form and none drove this one. What they missed: on an
// edit the card is VenueFixed and renders NO location_id control, so the POST cannot
// carry one — yet the rebuild looked the venue name up by r.PostFormValue
// ("location_id"). The lookup missed every time and the card printed "Venue could not
// be read", a string venueview.go documents as meaning the JOIN MISSED — a repair
// problem. So a manager who mistyped a name was told the database was broken.
func TestSaveDepartment_ARejectedEditStillNamesItsVenue(t *testing.T) {
	// Every way a department edit can be refused at the boundary.
	refusals := []struct {
		name string
		form url.Values
	}{
		{"an empty name", url.Values{"name": {""}}},
		{"an over-long name", url.Values{"name": {strings.Repeat("a", tenant.MaxVenueNameRunes+1)}}},
		{"a half-filled shift", url.Values{"name": {"Kitchen"}, "shift_start": {"07:00"}}},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			f := twoVenues()
			b := venueBrowser(t, f)
			deptID := f.firstDepartmentID()

			form := url.Values{"id": {deptID.String()}}
			for k, v := range tc.form {
				form[k] = v
			}
			rec := b.do(http.MethodPost, departmentSaveHref, form)
			if rec.Code != http.StatusOK {
				t.Fatalf("a rejected department edit answered %d, want 200 with the form back",
					rec.Code)
			}
			body := htmlOf(t, rec)
			if strings.Contains(body, "Venue could not be read") {
				t.Error(`the rejected edit says "Venue could not be read". That string means ` +
					`THE JOIN MISSED (venueview.go) — so somebody who mistyped a name is told ` +
					`the database is broken. The venue is a property of the DEPARTMENT and the ` +
					`form renders no control for it, so the body can never carry it.`)
			}
			if !strings.Contains(body, "KF St Julians") {
				t.Error("the rejected edit does not name the department's venue at all")
			}
			// The heading names the ROW, not the draft, so the card still says which
			// department is open.
			if !strings.Contains(body, "Edit Kitchen") {
				t.Error(`the card fell back to a generic "Edit department" heading and no ` +
					`longer says which one is open`)
			}
			// And the manager keeps whatever they typed.
			if msg := tc.form.Get("name"); msg != "" && !strings.Contains(body, msg) {
				t.Errorf("the re-rendered form lost the name the manager typed")
			}
		})
	}
}

// TestSaveDepartment_ARejectedEditResolvesAVenuePastTheCeiling is the same property
// for a department the capped screen list does not contain. The fast path (scan the
// slice) cannot answer it, so this is what proves the fallback is wired on the WRITE
// side too — venueForms closed the same gap for the GET side one function along.
func TestSaveDepartment_ARejectedEditResolvesAVenuePastTheCeiling(t *testing.T) {
	beyondDept := uuid.New()
	beyondVenue := uuid.New()
	f := twoVenues()
	f.screen.DepartmentsCapped = true
	f.beyondDepts = map[uuid.UUID]tenant.Department{
		beyondDept: {
			ID: beyondDept, LocationID: beyondVenue,
			LocationName: "KF Beyond The Cap", Name: "Pastry",
		},
	}
	b := venueBrowser(t, f)

	// ANTI-VACUITY: the department really is absent from the page's own list.
	if list := htmlOf(t, b.do(http.MethodGet, locationsHref, nil)); strings.Contains(list, "Pastry") {
		t.Fatal("the fixture department is in the list, so the fallback is never exercised")
	}

	rec := b.do(http.MethodPost, departmentSaveHref, url.Values{
		"id": {beyondDept.String()}, "name": {""},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("answered %d, want 200 with the form back", rec.Code)
	}
	body := htmlOf(t, rec)
	if strings.Contains(body, "Venue could not be read") {
		t.Error("a rejected edit of a department past the ceiling claims the join missed")
	}
	if !strings.Contains(body, "KF Beyond The Cap") {
		t.Error("the card does not name the venue of a department past the ceiling")
	}
}

// TestSaveDepartment_ARejectedCreateKeepsTheChosenVenue is the CREATE half of the same
// root cause. Here the venue IS the manager's choice and does come from the body — but
// the dropdown is rebuilt from the capped list, so a venue past the ceiling would
// vanish from it and a DIFFERENT one would be selected. Silently moving the department
// somebody is adding is worse than refusing it.
func TestSaveDepartment_ARejectedCreateKeepsTheChosenVenue(t *testing.T) {
	beyondVenue := uuid.New()
	f := twoVenues()
	f.screen.VenuesCapped = true
	f.beyondVenues = map[uuid.UUID]tenant.Venue{
		beyondVenue: {ID: beyondVenue, Name: "KF Beyond The Cap"},
	}
	b := venueBrowser(t, f)

	rec := b.do(http.MethodPost, departmentSaveHref, url.Values{
		"location_id": {beyondVenue.String()},
		"name":        {""}, // the refusal
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("answered %d, want 200 with the form back", rec.Code)
	}
	body := htmlOf(t, rec)
	if !strings.Contains(body, beyondVenue.String()) {
		t.Error("the rebuilt dropdown does not contain the venue the manager chose, so " +
			"pressing Save again would create the department somewhere else")
	}
	if !strings.Contains(body, "KF Beyond The Cap") {
		t.Error("the rebuilt dropdown does not NAME the chosen venue")
	}

	// AND A FOREIGN VENUE IS STILL REFUSED rather than added to the dropdown — the
	// fallback must not widen what this form will offer.
	f2 := twoVenues()
	f2.screen.VenuesCapped = true
	f2.beyondVenues = map[uuid.UUID]tenant.Venue{}
	b2 := venueBrowser(t, f2)
	rec2 := b2.do(http.MethodPost, departmentSaveHref, url.Values{
		"location_id": {uuid.NewString()}, "name": {""},
	})
	if loc := rec2.Header().Get("Location"); !strings.Contains(loc, "problem=unknown-venue") {
		t.Errorf("a rejected create aimed at a foreign venue answered %d / %q; it must be "+
			"refused, not re-offered", rec2.Code, loc)
	}
}

// TestSaveDepartment_ARejectedEditOfAForeignDepartmentIsRefused. The rebuild resolves
// the department from the database, so an id that is not this business's cannot be
// used to open an edit card — the refusal is the same word the save path uses.
func TestSaveDepartment_ARejectedEditOfAForeignDepartmentIsRefused(t *testing.T) {
	f := twoVenues()
	f.beyondDepts = map[uuid.UUID]tenant.Department{}
	b := venueBrowser(t, f)

	rec := b.do(http.MethodPost, departmentSaveHref, url.Values{
		"id": {uuid.NewString()}, "name": {""},
	})
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "problem=unknown-department") {
		t.Errorf("a rejected edit of a foreign department answered %d / %q", rec.Code, loc)
	}
	if body := htmlOf(t, rec); strings.Contains(body, "Venue could not be read") {
		t.Error("a foreign department id rendered the join-missed sentence")
	}
}

// TestVenueWrites_AreOnTheWritingChain.
//
// 🔴 THE ORIGIN CHECK RUNS BEFORE THE RESOLVER, AND THAT ORDERING IS THE WHOLE REASON
// mountWriting EXISTS. M6-04 shipped POST /admin/review inside the READ group and an
// audit measured what it bought: 300 cross-origin posts spent a signed-in manager's
// whole session budget and locked them out of their own panel for ten minutes,
// charging 301 unbudgeted session lookups on the way. A route registered anywhere but
// mountWriting silently inherits the read chain, and nothing about the response body
// would show it -- so what is measured here is that the RESOLVER WAS NOT CALLED.
func TestVenueWrites_AreOnTheWritingChain(t *testing.T) {
	for _, route := range []string{venueSaveHref, departmentSaveHref} {
		t.Run(route, func(t *testing.T) {
			resolved := 0
			admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
				resolved++
				return adminauth.Resolved{
					SessionID: panelTestSession, TenantID: panelTestTenant,
					AdminUserID: panelTestAdmin, Role: "owner", FullName: "KF Owner",
				}, nil
			}}
			f := twoVenues()
			b := newBrowser(t, newAdminRouterWithVenues(t, admins, f))
			b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
			b.origin = "https://evil.example"

			rec := b.do(http.MethodPost, route, url.Values{"name": {"forged"}})
			// ⚠️ THE STATUS CODE IS NOT THE MEASUREMENT AND A FIRST DRAFT OF THIS TEST
			// GOT THAT WRONG. sameOriginGate REFUSES with a 303 to /admin, and this
			// section ACCEPTS with a 303 to /admin/locations — the same code for opposite
			// outcomes. What separates them is where it points and, above all, what the
			// request COST: the middleware exists to refuse before the resolver runs.
			if loc := rec.Header().Get("Location"); loc != "/admin" {
				t.Errorf("a cross-origin POST to %s redirected to %q; the same-origin "+
					"refusal sends a caller to /admin, so this one was served", route, loc)
			}
			if resolved != 0 {
				t.Errorf("a cross-origin POST to %s cost %d session lookup(s). The Origin "+
					"check must run BEFORE the resolver, which is what ProtectWriting gives "+
					"and Protect does not -- registering this route in mountSections would "+
					"look identical from the outside.", route, resolved)
			}
			if venues, depts := f.saved(); len(venues) != 0 || len(depts) != 0 {
				t.Errorf("a cross-origin POST built %d venue and %d department command(s)",
					len(venues), len(depts))
			}
		})
	}
}

// TestVenueWrites_RefuseAnOversizedBodyWithoutParsingIt. A bare ParseForm inherits
// net/http's 10 MB, and a valid session may spend adminSessionLimit requests per
// window -- so the unbounded shape is 300 x 10 MB of allocation per session per ten
// minutes from somebody who has merely signed in.
func TestVenueWrites_RefuseAnOversizedBodyWithoutParsingIt(t *testing.T) {
	f := twoVenues()
	b := venueBrowser(t, f)

	body := "name=" + strings.Repeat("a", maxVenueBody+1024)
	rec := b.doRaw(http.MethodPost, venueSaveHref, body)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("an oversized body answered %d, want a 303 carrying the refusal", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "problem=unreadable") {
		t.Errorf("redirected to %q; section 4.6 asks a refused action to say so", loc)
	}
	if venues, _ := f.saved(); len(venues) != 0 {
		t.Errorf("an oversized body still built %d command(s)", len(venues))
	}
	// THE CONTROL: a body just under the ceiling is accepted, or the assertion above
	// would pass on a route that refuses everything.
	f2 := twoVenues()
	b2 := venueBrowser(t, f2)
	// A name field of a legal length, padded with a field the parser ignores.
	ok := "name=KF+Padded&pad=" + strings.Repeat("a", maxVenueBody-64)
	if rec := b2.doRaw(http.MethodPost, venueSaveHref, ok); rec.Code != http.StatusSeeOther {
		t.Fatalf("a body inside the ceiling answered %d, want 303", rec.Code)
	}
	if venues, _ := f2.saved(); len(venues) != 1 {
		t.Fatalf("a body inside the ceiling built %d command(s), want 1", len(venues))
	}
}

// TestVenueReturn_CannotCarryAnythingTheClientChose. Nothing the client posted is
// echoed into the Location header, and there is no field a caller could put a URL in,
// so this cannot become an open redirect.
func TestVenueReturn_CannotCarryAnythingTheClientChose(t *testing.T) {
	hostile := []string{
		"https://evil.example", "//evil.example", "venue-saved&x=1",
		"../../etc/passwd", "unknown-venue\nSet-Cookie: a=b", "",
	}
	for _, h := range hostile {
		if got := venueReturn(h, ""); !strings.HasPrefix(got, locationsHref) ||
			strings.ContainsAny(got, "\r\n") || strings.Contains(got, "evil.example") {
			t.Errorf("venueReturn(done=%q) = %q", h, got)
		}
		if got := venueReturn("", h); !strings.HasPrefix(got, locationsHref) ||
			strings.ContainsAny(got, "\r\n") || strings.Contains(got, "evil.example") {
			t.Errorf("venueReturn(problem=%q) = %q", h, got)
		}
	}
	// THE CONTROL: the legitimate words DO survive, or the filter is just "return the
	// bare href" and proves nothing.
	for _, w := range venueDoneWords {
		if got := venueReturn(w, ""); got != locationsHref+"?done="+w {
			t.Errorf("venueReturn(%q) = %q, want the word to survive", w, got)
		}
	}
	for _, w := range venueProblemWords {
		if got := venueReturn("", w); got != locationsHref+"?problem="+w {
			t.Errorf("venueReturn(problem=%q) = %q", w, got)
		}
	}
}

// TestVenueVocabulary_HoldsOnlyWordsTheServerEmits.
//
// 🔴 AN UNREACHABLE WORD IN A CLOSED VOCABULARY IS NOT DEAD CODE -- it is a sentence
// about our own system that ONLY a stranger can trigger, and that is false whenever
// they do. This section shipped with "venue-unavailable" in the list and nothing
// redirecting with it (a failed read renders a problem page instead), so pasting
// ?problem=venue-unavailable printed "We could not load your venues. Nothing was
// changed" directly above the venues it had just loaded. That is the M6-04 defect in
// miniature: a URL claiming something that never happened.
//
// THE TEST READS THE SOURCE rather than listing what it expects, because a list here
// would be a second copy of the vocabulary and would agree with whatever somebody
// added to the first one.
func TestVenueVocabulary_HoldsOnlyWordsTheServerEmits(t *testing.T) {
	// 🔴 THE SCANNER READS FOUR FILES, NOT ONE, AND THAT IS A REPAIR RATHER THAN A
	// WIDENING. It read locationactions.go alone until M6-06 phase B put the plaque
	// acts in their own files -- and this net immediately reported EIGHT live words as
	// unreachable, which is the net doing its job on a scanner whose SCOPE had gone
	// stale. "A net's coverage is its scanner's coverage" is this repository's own
	// lesson; the fix is to widen the scan, never to exempt the words.
	var src []byte
	for _, name := range []string{"locationactions.go", "plaqueactions.go", "plaques.go"} {
		part, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		src = append(append(src, part...), '\n')
	}
	emitted := map[string]bool{}
	// THE PLAQUE ACTS' OWN REDIRECT HELPER, read exactly like venueRemovalReturn's.
	for _, m := range plaqueReturnCallRE.FindAllStringSubmatch(string(src), -1) {
		if m[1] != "" {
			emitted[m[1]] = true
		}
	}
	// 🔴 AND THE PLAQUE REFUSALS ARE DERIVED, NOT LISTED. plaqueRefusal maps a typed
	// domain error onto a word and the handlers pass its RESULT to venueReturn, so the
	// literals never appear at a call site -- the same blind spot the hand-maintained
	// list below exists for. Deriving them from the `return "word"` statements keeps
	// the check honest: a word removed from that function stops being emitted here
	// too, which is precisely what this test is for.
	// ⚠️ SCOPED TO plaqueRefusal's OWN BODY, not to the three files. The first version
	// scanned everything and swept up any `return "word"` it met — which only ever
	// WEAKENED the assertion (it can add words to `emitted`, never remove them), but a
	// net that answers a wider question than it claims is the shape this task has
	// corrected twice already.
	if body := functionBody(t, string(src), "func plaqueRefusal("); body != "" {
		for _, m := range plaqueRefusalWordRE.FindAllStringSubmatch(body, -1) {
			emitted[m[1]] = true
		}
	} else {
		t.Fatal("plaqueRefusal was not found in the scanned sources; the scan is reading " +
			"the wrong files and every assertion below is vacuous")
	}
	for _, m := range venueReturnCallRE.FindAllStringSubmatch(string(src), -1) {
		if m[1] != "" {
			emitted[m[1]] = true
		}
		if m[2] != "" {
			emitted[m[2]] = true
		}
	}
	// 🔴 THE SECOND EMITTER. A completed removal redirects through
	// venueRemovalReturn, which carries the word AND the id the acknowledgement is
	// verified against (user decision, 2026-08-09, reading C'). Missing it here made
	// this net report two live words as unreachable — which is the net doing its job
	// on a scanner that had gone stale, and is why it reads BOTH call shapes now.
	for _, m := range venueRemovalReturnCallRE.FindAllStringSubmatch(string(src), -1) {
		if m[1] != "" {
			emitted[m[1]] = true
		}
	}
	// ANTI-VACUITY: the scanner found the call sites at all. A regexp that matched
	// nothing would make every assertion below pass over an empty set.
	if len(emitted) < 3 {
		t.Fatalf("the scanner found %d literal word(s) at venueReturn call sites (%v); "+
			"it is broken and this test proves nothing", len(emitted), emitted)
	}
	// The words the handlers pass through a VARIABLE rather than a literal. The
	// scanner reads `venueReturn("x", "y")` call sites, so these are invisible to it
	// and are named here instead — and naming them is the point: each entry is a
	// CLAIM that some path really does emit it, made by hand, which is the cost of
	// the scanner's blind spot rather than a way around it.
	//
	//	venue-saved / venue-added / department-saved / department-added
	//	    saveVenue and saveDepartment choose the word into `done` and pass that.
	//	confirm-required / confirm-stale
	//	    deleteVenue and deleteDepartment pass confirmationRefusal's return
	//	    straight through, so the literals live in employeeactions.go.
	for _, viaVariable := range []string{"venue-saved", "venue-added",
		"department-saved", "department-added", "confirm-required", "confirm-stale"} {
		emitted[viaVariable] = true
	}
	// ⚠️ AND THE HAND-MAINTAINED LIST IS ITSELF CHECKED, so it cannot become a way to
	// wave a word through: every entry above must appear in this package's source as a
	// literal SOMEWHERE, even if not at a venueReturn call site.
	pkg, err := os.ReadFile("employeeactions.go")
	if err != nil {
		t.Fatalf("read employeeactions.go: %v", err)
	}
	whole := string(src) + string(pkg)
	for _, w := range []string{"confirm-required", "confirm-stale"} {
		if !strings.Contains(whole, `"`+w+`"`) {
			t.Errorf("%q is declared as emitted-via-variable but appears nowhere as a "+
				"literal; the exemption list is being used to wave a word through", w)
		}
	}

	for _, w := range append(append([]string{}, venueDoneWords...), venueProblemWords...) {
		if !emitted[w] {
			t.Errorf("%q is in the section's closed vocabulary but no path emits it. "+
				"The only way to reach it is a hand-edited URL, and the sentence it turns "+
				"on is therefore false every time it appears. Remove the word or wire it.", w)
		}
	}
}

// venueReturnCallRE reads the literal words out of `venueReturn("x", "y")` calls.
//
// ⚠️ IT MATCHES venueReturn ONLY, and venueRemovalReturn is a separate pattern below
// rather than a loosened version of this one: `venueReturn\w*` would also match a
// future helper nobody has checked, which is the kind of quiet widening this file has
// twice had to undo.
var venueReturnCallRE = regexp.MustCompile(`\bvenueReturn\("([^"]*)",\s*"([^"]*)"\)`)

// venueRemovalReturnCallRE reads the word out of `venueRemovalReturn("x", id)` calls.
var venueRemovalReturnCallRE = regexp.MustCompile(`\bvenueRemovalReturn\("([^"]*)",`)

// plaqueReturnCallRE reads the word out of `plaqueReturn("x", uid)` calls -- the
// plaque acts' own redirect helper (M6-06 phase B). A SEPARATE pattern rather than a
// loosened one, for the reason venueRemovalReturnCallRE gives.
var plaqueReturnCallRE = regexp.MustCompile(`\bplaqueReturn\("([^"]*)",`)

// plaqueRefusalWordRE reads the words plaqueRefusal RETURNS, because the handlers
// pass its result through a variable and no literal reaches a venueReturn call site.
//
// ⚠️ IT IS DELIBERATELY NARROW: a lower-case hyphenated literal on a `return` line.
// `return ""` and `return "", false` do not match, so the empty answer -- which means
// "this is not a refusal" -- cannot smuggle a blank word into the set.
var plaqueRefusalWordRE = regexp.MustCompile(`\breturn "([a-z][a-z-]+)"\n`)

// functionBody returns the source between `decl` and the next top-level closing
// brace, or "" when the declaration is absent.
//
// IT IS A BRACE COUNT RATHER THAN A REGEXP because a regexp over a function body is
// the shape this repository has twice had to replace with go/ast; a brace count is
// what makes "this word is returned by THAT function" checkable without a parser for
// one call site.
func functionBody(t *testing.T, src, decl string) string {
	t.Helper()
	i := strings.Index(src, decl)
	if i < 0 {
		return ""
	}
	depth := 0
	for j := i; j < len(src); j++ {
		switch src[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[i : j+1]
			}
		}
	}
	return ""
}

// TestLocationsSection_ReflectsNothingFromTheQueryString. A hand-edited URL may turn
// one of a fixed set of sentences on; it may not put a sentence of its own on the
// screen.
func TestLocationsSection_ReflectsNothingFromTheQueryString(t *testing.T) {
	b := venueBrowser(t, twoVenues())
	const marker = "zzmarkerzz"
	for _, q := range []string{"?problem=" + marker, "?done=" + marker,
		"?problem=<script>alert(1)</script>" + marker} {
		body := htmlOf(t, b.do(http.MethodGet, locationsHref+q, nil))
		if strings.Contains(body, marker) {
			t.Errorf("%s put its own text on the page", q)
		}
	}
}

// TestVenueFormView_IsRebuiltFromWhatWasPostedNotFromWhatParsed.
//
// 🔴 THE SUBMISSION IS ONLY PARTLY FILLED WHEN VALIDATION STOPPED EARLY -- the value
// that was WRONG is by definition not in it -- so rebuilding the form from the parsed
// struct would silently blank the one field the manager needs to fix.
func TestVenueFormView_IsRebuiltFromWhatWasPostedNotFromWhatParsed(t *testing.T) {
	form := url.Values{
		"name": {"KF Test"}, "static_ips": {"192.168.1.5/24"}, // host bits: refused
		"gps_lat": {"35.9"}, "gps_lng": {"14.4"},
		"shift_start": {"08:00"}, "shift_end": {"17:00"}, "overnight": {"1"},
		"wifi_ssid": {"KF-Test"},
	}
	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.ParseForm(); err != nil {
		t.Fatalf("ParseForm: %v", err)
	}
	sub, field, msg := parseVenueForm(r)
	if msg == "" || field != "static_ips" {
		t.Fatalf("the fixture no longer fails on static_ips (field %q, msg %q)", field, msg)
	}
	// The parsed submission has NOTHING past the failing field...
	if sub.GPS != nil || sub.Shift != nil {
		t.Fatal("parseVenueForm kept parsing past the failure; this test's premise is gone")
	}
	// ...and the rebuilt form has all of it.
	got := venueFormFromSubmission(sub, r)
	want := components.VenueFormView{
		Heading: "Add a venue", Name: "KF Test", Ranges: "192.168.1.5/24",
		GPSLat: "35.9", GPSLng: "14.4", ShiftStart: "08:00", ShiftEnd: "17:00",
		Overnight: true, WiFiSSID: "KF-Test", Submit: "Add venue",
	}
	if got != want {
		t.Errorf("the rebuilt form is\n %+v\nwant\n %+v", got, want)
	}
}

// TestLocationsSection_NeverClaimsARemovalItCannotSee.
//
// 🔴 THIS IS THE M6-05 DEFECT, MEASURED A SECOND TIME ON THIS SECTION AND CLOSED. The
// section briefly carried "venue-deleted" and "department-deleted", and a stranger's
// URL — in a browser that had never posted anything — rendered "Venue removed. The
// venue has been removed." employees.templ allows an action claim on exactly ONE
// condition: that the handler verifies it against a row THE SAME REQUEST READ.
//
// A DELETION IS THE ONE ACT WHERE THE ROW ITSELF IS GONE — so what is read instead is
// the AUDIT ENTRY, which is append-only and carries the actor. The claim is therefore
// made and CHECKED (user decision, 2026-08-09, reading C'), and what this test holds
// is the half that matters: no ?done= word may produce a removal sentence on its own.
//
// ⚠️ THE FIRST LOOP USED TO SKIP THE TWO REMOVAL WORDS ("covered below"), AND THAT
// EXEMPTION IS GONE. It made the strongest sentence in this file — "no word in the
// vocabulary may assert a removal" — untrue of exactly the two words it was about, so
// the net read wider than it held. They are now driven here like every other word: a
// bare ?done=venue-deleted, with no receipt behind it, must render nothing.
//
// THE TEST DRIVES THE VOCABULARY ITSELF rather than a fixed list of words, so a word
// added tomorrow is checked without anybody remembering to add it here.
func TestLocationsSection_NeverClaimsARemovalItCannotSee(t *testing.T) {
	b := venueBrowser(t, twoVenues())

	// 1. NO WORD IN THE VOCABULARY MAY ASSERT A REMOVAL ON ITS OWN. Every accepted
	// ?done= word is rendered and read back — the two removal words included, because
	// this browser's fake holds no receipt for anything.
	removalWords := []string{"removed", "deleted", "has been removed"}
	for _, w := range venueDoneWords {
		body := htmlOf(t, b.do(http.MethodGet, locationsHref+"?done="+w, nil))
		for _, claim := range removalWords {
			if strings.Contains(strings.ToLower(body), claim) {
				t.Errorf("?done=%s renders %q in a browser that has removed nothing. A "+
					"removal is acknowledged only against a VERIFIED trail entry; the "+
					"word alone must say nothing.", w, claim)
			}
		}
	}

	// 2. AND THE TWO REMOVAL WORDS RENDER NOTHING ON THEIR OWN. They are back in the
	// vocabulary (user decision, 2026-08-09, reading C') but they are no longer
	// self-backing: without a verified trail receipt they must stay silent, with and
	// without an id in the address.
	for _, w := range []string{"venue-deleted", "department-deleted"} {
		for _, q := range []string{
			"?done=" + w,
			"?done=" + w + "&id=" + uuid.NewString(),
			"?done=" + w + "&id=nonsense",
		} {
			body := htmlOf(t, b.do(http.MethodGet, locationsHref+q, nil))
			if strings.Contains(strings.ToLower(body), "removed") {
				t.Errorf("%s renders a removal claim with no verified receipt", q)
			}
		}
	}

	// 3. ANTI-VACUITY: the banner mechanism WORKS for the words that are legitimate,
	// or every assertion above would pass over a page that renders no banner at all.
	saved := htmlOf(t, b.do(http.MethodGet, locationsHref+"?done=venue-saved", nil))
	if !strings.Contains(saved, "as it now stands") {
		t.Fatal("?done=venue-saved renders no banner, so this test proves nothing about " +
			"the removal words")
	}
	// AND THAT SURVIVING BANNER IS ITSELF A STATE CLAIM, not an event one: it describes
	// the row rendered beneath it, which the same request read.
	if strings.Contains(strings.ToLower(saved), "has been saved") {
		t.Error("the surviving banner makes an EVENT claim; every word in this " +
			"vocabulary must name what the page SHOWS")
	}
}

// --- removal (user decision, 2026-08-08: option (b), unreferenced rows only) --------

// venueWithRefs is a browser whose one venue carries the given references.
func venueWithRefs(t *testing.T, refs tenant.References) (*fakeVenues, *browser, uuid.UUID) {
	t.Helper()
	f := twoVenues()
	id := f.firstVenueID()
	f.refs = map[uuid.UUID]tenant.References{id: refs}
	return f, venueBrowser(t, f), id
}

// TestVenueRemoval_IsOfferedOnlyWhenNothingPointsAtTheVenue.
//
// 🔴 THE CONTROL IS ABSENT WHEN IT WOULD FAIL, RATHER THAN PRESENT AND REFUSED. Six
// foreign keys reference locations and departments and all six are ON DELETE RESTRICT,
// so a venue in use can be neither removed NOR retired — there is no archive column to
// fall back on. Offering a button and then refusing the press is the class this
// section has spent four review rounds closing.
func TestVenueRemoval_IsOfferedOnlyWhenNothingPointsAtTheVenue(t *testing.T) {
	t.Run("nothing points at it, so the control is offered", func(t *testing.T) {
		_, b, id := venueWithRefs(t, tenant.References{})
		body := htmlOf(t, b.do(http.MethodGet, locationsHref+"?venue="+id.String(), nil))
		if !strings.Contains(body, "Remove this venue") {
			t.Error("an empty venue offers no way to remove it")
		}
		if strings.Contains(body, "cannot be removed") {
			t.Error("an empty venue is described as un-removable")
		}
	})

	t.Run("something points at it, so the control is absent and counted", func(t *testing.T) {
		_, b, id := venueWithRefs(t, tenant.References{
			Departments: 2, Employees: 3, Tags: 1, Transactions: 9,
		})
		body := htmlOf(t, b.do(http.MethodGet, locationsHref+"?venue="+id.String(), nil))
		// 🔴 NO FORM POSTING TO THE DELETE ROUTE AT ALL. "Disabled" is not enough: a
		// control that exists says "this is a thing you could do here if only".
		for _, m := range postFormRE.FindAllStringSubmatch(body, -1) {
			if c := attrValueRE("action").FindStringSubmatch(m[1]); len(c) == 2 &&
				htmlUnescaper.Replace(c[1]) == venueDeleteHref {
				t.Error("a venue in use still renders a form posting to the delete route")
			}
		}
		if !strings.Contains(body, "cannot be removed") {
			t.Error("the card does not say the venue cannot be removed")
		}
		// 🔴 AND THE REASON CARRIES NUMBERS. "Still in use" alone leaves somebody
		// hunting; the counts say how much work it is and where to start.
		for _, want := range []string{"2 departments", "3 employees", "1 plaque", "9 recorded taps"} {
			if !strings.Contains(body, want) {
				t.Errorf("the reason does not name %q", want)
			}
		}
		// 🔴 AND THE PIECES ARE SEPARATED. They used to be joined by a bare space, so
		// the card read "1 employee 2 plaques" — two facts running into one another,
		// and the card's own documentation claimed a middle dot.
		if !strings.Contains(body, "·") {
			t.Error("the counted reasons are not separated; they run together into one " +
				"unreadable sentence")
		}
	})

	t.Run("the singular is not printed as a plural", func(t *testing.T) {
		_, b, id := venueWithRefs(t, tenant.References{Employees: 1, Transactions: 1})
		body := htmlOf(t, b.do(http.MethodGet, locationsHref+"?venue="+id.String(), nil))
		if !strings.Contains(body, "1 employee") || strings.Contains(body, "1 employees") {
			t.Error("the reason reads '1 employees'")
		}
		if !strings.Contains(body, "1 recorded tap") || strings.Contains(body, "1 recorded taps") {
			t.Error("the reason reads '1 recorded taps'")
		}
	})

	t.Run("an ADD card offers no removal at all", func(t *testing.T) {
		f := twoVenues()
		b := venueBrowser(t, f)
		body := htmlOf(t, b.do(http.MethodGet, locationsHref+"?venue=new", nil))
		if strings.Contains(body, "Remove this venue") {
			t.Error("the add-a-venue card offers to remove something that does not exist yet")
		}
	})
}

// TestDepartmentRemoval_FollowsTheSameRule. Symmetry is the requirement: a manager
// must not learn two different logics from two lists on one screen.
func TestDepartmentRemoval_FollowsTheSameRule(t *testing.T) {
	f := twoVenues()
	id := f.firstDepartmentID()
	f.refs = map[uuid.UUID]tenant.References{id: {Employees: 4}}
	b := venueBrowser(t, f)

	body := htmlOf(t, b.do(http.MethodGet, locationsHref+"?department="+id.String(), nil))
	if !strings.Contains(body, "cannot be removed") || !strings.Contains(body, "4 employees") {
		t.Error("a department in use does not say so with numbers")
	}

	f2 := twoVenues()
	id2 := f2.firstDepartmentID()
	f2.refs = map[uuid.UUID]tenant.References{id2: {}}
	b2 := venueBrowser(t, f2)
	body2 := htmlOf(t, b2.do(http.MethodGet, locationsHref+"?department="+id2.String(), nil))
	if !strings.Contains(body2, "Remove this department") {
		t.Error("an empty department offers no way to remove it")
	}
}

// TestVenueRemoval_AsksBeforeItRemoves.
//
// 🔴 NO IRREVERSIBLE WRITE FROM ONE PRESS IN A LIST. The first step is a LINK that
// asks the server for a warning; only the response to that link carries the form and
// the server-minted confirmation. It is the roster's mechanism reused rather than a
// second one invented — see removalView.
func TestVenueRemoval_AsksBeforeItRemoves(t *testing.T) {
	f, b, id := venueWithRefs(t, tenant.References{})

	// STEP ONE: the plain card has no posting form for the delete route.
	plain := htmlOf(t, b.do(http.MethodGet, locationsHref+"?venue="+id.String(), nil))
	if strings.Contains(plain, `name="`+confirmField+`"`) {
		t.Error("the plain edit card already carries a confirmation; the warning was skipped")
	}

	// STEP TWO: the warning arms the real form.
	armed := htmlOf(t, b.do(http.MethodGet, locationsHref+"?venue="+id.String()+"&confirm=remove", nil))
	if !strings.Contains(armed, "cannot be undone") {
		t.Error("the warning does not say the removal is irreversible")
	}
	if !strings.Contains(armed, `name="`+confirmField+`"`) {
		t.Fatal("the warning carries no confirmation value, so the POST could not succeed")
	}

	// AND A POST WITHOUT ONE IS REFUSED, with nothing removed.
	rec := b.do(http.MethodPost, venueDeleteHref, url.Values{"id": {id.String()}})
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "problem=confirm-required") {
		t.Errorf("an unconfirmed removal answered %q", loc)
	}
	if got := f.removals(); len(got) != 0 {
		t.Errorf("an unconfirmed removal still reached the domain: %+v", got)
	}
}

// TestVenueRemoval_ConfirmedRemovalReachesTheDomainWithTheSessionsActor (§4.5, §4.7).
func TestVenueRemoval_ConfirmedRemovalReachesTheDomainWithTheSessionsActor(t *testing.T) {
	f, b, id := venueWithRefs(t, tenant.References{})

	armed := htmlOf(t, b.do(http.MethodGet, locationsHref+"?venue="+id.String()+"&confirm=remove", nil))
	token := confirmTokenFrom(t, armed)

	rec := b.do(http.MethodPost, venueDeleteHref, url.Values{
		"id": {id.String()}, confirmField: {token},
		// forged, and both must be ignored
		"tenant_id": {uuid.NewString()}, "actor_id": {uuid.NewString()},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("a confirmed removal answered %d, want 303", rec.Code)
	}
	// 🔴 THE REDIRECT CARRIES THE WORD **AND THE ID**, and the id is what makes the
	// word checkable (user decision, 2026-08-09, reading C'). A word on its own was
	// measured printing a claim in a browser that had never posted anything; the id
	// lets the section find the actor's own audit entry before it says a thing.
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "done=venue-deleted") {
		t.Errorf("the removal redirected to %q with no acknowledgement word", loc)
	}
	if !strings.Contains(loc, "id="+id.String()) {
		t.Errorf("the removal redirected to %q without naming the row it removed, so "+
			"the acknowledgement could not be verified against the trail", loc)
	}
	// AND THE WORD ALONE STILL PROVES NOTHING: this fake holds no receipt, so the
	// page that redirect lands on must stay silent.
	if body := htmlOf(t, b.do(http.MethodGet, loc, nil)); strings.Contains(
		strings.ToLower(body), "removed") {
		t.Error("the redirect target printed a removal claim with no verified receipt")
	}
	got := f.removals()
	if len(got) != 1 {
		t.Fatalf("the section built %d removal(s), want 1", len(got))
	}
	if got[0].TenantID != panelTestTenant || got[0].ActorID != panelTestAdmin {
		t.Errorf("the command carries tenant %s / actor %s; both must come from the "+
			"session", got[0].TenantID, got[0].ActorID)
	}
	if got[0].ID != id {
		t.Errorf("the command removes %s, want %s", got[0].ID, id)
	}
}

// TestVenueRemoval_TheRaceIsASentenceAndNotAFailure.
//
// 🔴 THE COUNT DECIDES WHETHER TO OFFER; THE FOREIGN KEYS DECIDE WHETHER IT HAPPENS.
// Between the screen counting zero and the press landing, another manager can hire
// somebody into the venue — Postgres answers 23503, the row survives, and the manager
// must be told THAT rather than shown a 500. internal/domain/tenant translates the
// code; this is the half that proves the panel does not turn it into a crash.
func TestVenueRemoval_TheRaceIsASentenceAndNotAFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		action string
		err    error
		word   string
	}{
		{"a venue filled after the screen rendered", venueDeleteHref, tenant.ErrVenueInUse, "venue-in-use"},
		{"a department filled after the screen rendered", departmentDeleteHref, tenant.ErrDepartmentInUse, "department-in-use"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := twoVenues()
			id := f.firstVenueID()
			if tc.action == departmentDeleteHref {
				id = f.firstDepartmentID()
			}
			f.refs = map[uuid.UUID]tenant.References{id: {}} // empty WHEN RENDERED
			f.deleteErr = tc.err                             // and full when it lands
			b := venueBrowser(t, f)

			param := "venue"
			if tc.action == departmentDeleteHref {
				param = "department"
			}
			armed := htmlOf(t, b.do(http.MethodGet,
				locationsHref+"?"+param+"="+id.String()+"&confirm=remove", nil))
			token := confirmTokenFrom(t, armed)

			rec := b.do(http.MethodPost, tc.action, url.Values{
				"id": {id.String()}, confirmField: {token},
			})
			if rec.Code == http.StatusInternalServerError {
				t.Fatal("the FK race answered 500. Nothing is wrong with the request and " +
					"nothing was lost — the manager is told OUR system broke instead of " +
					"that somebody was just placed there.")
			}
			if loc := rec.Header().Get("Location"); !strings.Contains(loc, "problem="+tc.word) {
				t.Fatalf("redirected to %q, want the %s word", loc, tc.word)
			}
			body := htmlOf(t, b.do(http.MethodGet, rec.Header().Get("Location"), nil))
			if !strings.Contains(body, "no longer empty") {
				t.Error("the page does not explain what happened")
			}
		})
	}
}

// TestVenueRemoval_AForeignIdIsRefused (§4.5). The sentence is TRUE here: the domain
// really did look, and really found nothing in this business.
func TestVenueRemoval_AForeignIdIsRefused(t *testing.T) {
	f := twoVenues()
	id := f.firstVenueID()
	f.refs = map[uuid.UUID]tenant.References{id: {}}
	f.deleteErr = tenant.ErrUnknownVenue
	b := venueBrowser(t, f)

	armed := htmlOf(t, b.do(http.MethodGet, locationsHref+"?venue="+id.String()+"&confirm=remove", nil))
	token := confirmTokenFrom(t, armed)
	rec := b.do(http.MethodPost, venueDeleteHref, url.Values{
		"id": {id.String()}, confirmField: {token},
	})
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "problem=unknown-venue") {
		t.Errorf("a foreign id answered %q", loc)
	}
}

// TestVenueRemoval_AnUnreadableIdSaysNothingWasLookedUp. "unreadable" is the section's
// word for "no query ran", which is exactly what an unparseable id means.
func TestVenueRemoval_AnUnreadableIdSaysNothingWasLookedUp(t *testing.T) {
	f, b, _ := venueWithRefs(t, tenant.References{})
	rec := b.do(http.MethodPost, venueDeleteHref, url.Values{"id": {"nonsense"}})
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "problem=unreadable") {
		t.Errorf("an unreadable id answered %q", loc)
	}
	if got := f.removals(); len(got) != 0 {
		t.Errorf("an unreadable id still reached the domain: %+v", got)
	}
}

// TestVenueRemoval_IsOnTheWritingChain (T3). A removal reachable by GET is one a link,
// a prefetch or a crawler can fire.
func TestVenueRemoval_IsOnTheWritingChain(t *testing.T) {
	for _, route := range []string{venueDeleteHref, departmentDeleteHref} {
		t.Run(route, func(t *testing.T) {
			f, b, id := venueWithRefs(t, tenant.References{})

			// GET MUST NOT REMOVE ANYTHING — the route is POST-only.
			if rec := b.do(http.MethodGet, route, nil); rec.Code != http.StatusMethodNotAllowed &&
				rec.Code != http.StatusNotFound {
				t.Errorf("GET %s answered %d; a removal must not be reachable by GET", route, rec.Code)
			}
			if got := f.removals(); len(got) != 0 {
				t.Fatalf("a GET removed something: %+v", got)
			}

			// AND A CROSS-ORIGIN POST IS REFUSED BEFORE THE RESOLVER RUNS.
			resolved := 0
			admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
				resolved++
				return adminauth.Resolved{
					SessionID: panelTestSession, TenantID: panelTestTenant,
					AdminUserID: panelTestAdmin, Role: "owner", FullName: "KF Owner",
				}, nil
			}}
			f2 := twoVenues()
			f2.refs = map[uuid.UUID]tenant.References{id: {}}
			b2 := newBrowser(t, newAdminRouterWithVenues(t, admins, f2))
			b2.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
			b2.origin = "https://evil.example"

			rec := b2.do(http.MethodPost, route, url.Values{"id": {id.String()}})
			if loc := rec.Header().Get("Location"); loc != "/admin" {
				t.Errorf("a cross-origin removal redirected to %q, want /admin", loc)
			}
			if resolved != 0 {
				t.Errorf("a cross-origin removal cost %d session lookup(s)", resolved)
			}
			if got := f2.removals(); len(got) != 0 {
				t.Errorf("a cross-origin removal reached the domain: %+v", got)
			}
		})
	}
}

// confirmTokenFrom lifts the confirmation out of a rendered warning. It is a value the
// page is SUPPOSED to expose — the cookie is its twin — so a test may read it.
func confirmTokenFrom(t *testing.T, html string) string {
	t.Helper()
	marker := `name="` + confirmField + `" value="`
	i := strings.Index(html, marker)
	if i < 0 {
		t.Fatal("no confirmation field in the rendered warning")
	}
	rest := html[i+len(marker):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		t.Fatal("the confirmation field is not closed")
	}
	return htmlUnescaper.Replace(rest[:j])
}

// assertMessageAgreesWithTheParser checks a format refusal against the parser it
// describes, so the sentence and the code cannot drift into two separate claims.
//
// 🔴 IT READS THE EXAMPLES OUT OF THE MESSAGE AND FEEDS THEM BACK IN. If the sentence
// says "like 35.917270 or 35,917270", both must be values parseGPS accepts — a
// message that offers an example the parser refuses is worse than no example. And if
// the sentence forbids a separator by name, that separator must actually be refused:
// the audit's counter-example ("commas are rejected, use a full stop") fails here
// because a comma IS accepted, which is exactly what the old string check missed.
func assertMessageAgreesWithTheParser(t *testing.T, msg string) {
	t.Helper()

	examples := messageExampleRE.FindAllString(msg, -1)
	// ANTI-VACUITY: a message with no example would make the loop below prove nothing,
	// and this section's rule is that a refusal says what to do instead.
	if len(examples) == 0 {
		t.Fatalf("the format message offers no example of an acceptable value: %q", msg)
	}
	for _, ex := range examples {
		if _, bad := parseGPS(ex, "14.4"); bad != "" {
			t.Errorf("the message offers %q as an example, and the parser REFUSES it "+
				"(%s). The sentence and the code disagree.", ex, bad)
		}
	}

	// EVERY SEPARATOR THE SENTENCE NAMES AS FORBIDDEN MUST REALLY BE FORBIDDEN.
	for _, f := range []struct {
		phrase string
		probe  string
	}{
		{"no comma", "35,9"},
		{"comma", "35,9"},
		{"full stop for the decimal", "35,9"},
	} {
		if !strings.Contains(strings.ToLower(msg), f.phrase) {
			continue
		}
		if _, bad := parseGPS(f.probe, "14.4"); bad == "" {
			t.Errorf("the message says %q, but the parser ACCEPTS %q. A sentence that "+
				"forbids what the code allows sends a manager to change a correct value.",
				f.phrase, f.probe)
		}
	}
}

// messageExampleRE lifts the numeric examples out of a refusal. It matches the shapes
// a coordinate example can take — digits with a full stop OR a comma — so an example
// written either way is checked.
var messageExampleRE = regexp.MustCompile(`[0-9]+[.,][0-9]+`)

// TestConfirmation_IsBoundToTheACTIONAndNotOnlyToTheSubject.
//
// 🔴 BEFORE THE ACTION WAS IN THE SIGNATURE, A VENUE-REMOVAL CONFIRMATION OPENED THE
// DEACTIVATION GATE. An audit measured it: the value verified cleanly at
// /admin/employees/deactivate and the request then failed in the DOMAIN, because a
// location id is not an employee id. The gate was held by the uuid spaces merely not
// overlapping — two accidents deep, and the token itself knew nothing about it. A
// binding that holds by luck is one a schema change can remove silently.
func TestConfirmation_IsBoundToTheACTIONAndNotOnlyToTheSubject(t *testing.T) {
	c, err := newAdminConfirm(adminTestConfig())
	if err != nil {
		t.Fatalf("newAdminConfirm: %v", err)
	}
	subject, session := uuid.NewString(), uuid.New()

	// 🔴 THE ACTION SET IS DERIVED FROM deactivateconfirm.go'S OWN CONSTANTS, AND THAT
	// IS THE SEVENTH TIME THIS TASK HAS PAID FOR A HAND-MAINTAINED LIST — this time
	// INSIDE THE TEST THAT EXISTS TO CATCH EXACTLY THAT CLASS. The slice was written by
	// hand with three names, grew to four, and stopped there while the file declared
	// FIVE; the missing one was confirmActionUnmountPlaque.
	//
	// ⚠️ AND THE UNCOVERED PAIR WAS THE ONE THE MATRIX EXISTS FOR. plaque.unmount and
	// plaque.replace are the ONLY two gates whose subject is the same KIND of value (a
	// 14-hex plaque uid), so they are the only pair where the SUBJECT binding does not
	// accidentally do the action binding's job — which is precisely the defect that
	// created this test: a gate held up by uuid spaces merely not overlapping.
	actions := confirmActionsFromSource(t)
	sort.Strings(actions)
	t.Logf("derived %d confirmation action(s): %v", len(actions), actions)

	// ANTI-VACUITY: the names really are distinct, or the matrix compares a value with
	// itself and passes for the wrong reason. The count is NOT asserted — the set is
	// derived, and a floor below it could not notice a deletion anyway (the diagonal
	// would simply shrink).
	seen := map[string]bool{}
	for _, a := range actions {
		if a == "" || seen[a] {
			t.Fatalf("the action vocabulary is degenerate (%q); this test would be vacuous", a)
		}
		seen[a] = true
	}

	pairs, diagonal := 0, 0
	for _, minted := range actions {
		token, err := c.mint(minted, subject, session)
		if err != nil {
			t.Fatalf("mint(%s): %v", minted, err)
		}
		for _, gate := range actions {
			pairs++
			err := c.parse(token, gate, subject, session)
			if minted == gate {
				diagonal++
				// THE POSITIVE CONTROL, inside the same loop: the value must still open
				// its OWN gate, or the binding is just a way of refusing everything.
				if err != nil {
					t.Errorf("a %s confirmation does not open the %s gate: %v", minted, gate, err)
				}
				continue
			}
			if err == nil {
				t.Errorf("a confirmation minted for %s OPENED the %s gate. The subject and "+
					"the session match, so nothing else stands in the way — the action must "+
					"be part of the signature.", minted, gate)
			}
			// AND THE REFUSAL IS THE COLLAPSED ONE, never "that was for another person":
			// the second sentence would tell a prober which binding they missed.
			if !errors.Is(err, errConfirmInvalid) {
				t.Errorf("a %s confirmation at the %s gate was refused as %v, want the "+
					"collapsed errConfirmInvalid", minted, gate, err)
			}
		}
	}
	if pairs != len(actions)*len(actions) || diagonal != len(actions) {
		t.Fatalf("walked %d pair(s) with %d on the diagonal, want %d and %d",
			pairs, diagonal, len(actions)*len(actions), len(actions))
	}
}

// confirmActionsFromSource reads the confirmation gate names out of
// deactivateconfirm.go's own const block.
//
// 🔴 DERIVED FOR THE REASON THE PLAQUE ACTION VOCABULARY IS: a list a human keeps in
// step is a list that is correct until somebody is busy. It reads const AND var, and
// FAILS LOUDLY on a value it cannot evaluate — both lessons measured one layer over,
// where a `var` declaration and a `"a" + "b"` value each walked through a scan that
// silently skipped them.
func confirmActionsFromSource(t *testing.T) []string {
	t.Helper()
	const src = "deactivateconfirm.go"
	file, err := parser.ParseFile(token.NewFileSet(), src, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", src, err)
	}
	var out []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || (gen.Tok != token.CONST && gen.Tok != token.VAR) {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, ident := range vs.Names {
				if !strings.HasPrefix(ident.Name, "confirmAction") {
					continue
				}
				if i >= len(vs.Values) {
					t.Fatalf("%s declares %s with no value this scan can read", src, ident.Name)
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					t.Fatalf("%s declares %s as a %T rather than a string literal; skipping "+
						"it silently is how a gate slips out of the matrix", src, ident.Name, vs.Values[i])
				}
				out = append(out, strings.Trim(lit.Value, `"`))
			}
		}
	}
	// ANTI-VACUITY: this floor guards a BROKEN SCAN — a rename, a moved file — not a
	// deletion, which would simply shrink the matrix and is caught by the tests that
	// drive each gate.
	if len(out) < 2 {
		t.Fatalf("derived %d confirmation action(s) from %s (%v); the scan is reading "+
			"the wrong file and this matrix is vacuous", len(out), src, out)
	}
	return out
}

// TestVenueRemoval_AConfirmationForOneRowDoesNotOpenAnothersGate.
//
// ⚠️ WHAT THIS PROVES IS THE WIRING, NOT THE BINDING, and the sentence used to claim
// more. It said it "drives the same property through the REAL router"; an audit then
// deleted the action check inside parse and this test stayed GREEN. The reason is
// structural and worth keeping: the SUBJECT binding already refuses this pairing (a
// venue id is not the department id the gate names), so the request is turned away
// whether or not the ACTION is checked.
//
// So the claim is lowered to what it measures — that each gate is wired to its own
// action constant and that a value from one card cannot be spent from another card's
// form — and the assertion is tightened to the EXACT word rather than the
// `problem=confirm` prefix it used to accept, because that prefix is also satisfied
// by `confirm-stale`. Substring matching has now made a net look wider than it is
// twice in this task.
//
// THE BINDING ITSELF is held by TestConfirmation_IsBoundToTheACTIONAndNotOnlyToThe
// Subject, which went RED under that same mutation across all six pairs.
func TestVenueRemoval_AConfirmationForOneRowDoesNotOpenAnothersGate(t *testing.T) {
	f := twoVenues()
	venueID, deptID := f.firstVenueID(), f.firstDepartmentID()
	f.refs = map[uuid.UUID]tenant.References{venueID: {}, deptID: {}}
	b := venueBrowser(t, f)

	// Mint for the VENUE removal.
	armed := htmlOf(t, b.do(http.MethodGet, locationsHref+"?venue="+venueID.String()+"&confirm=remove", nil))
	token := confirmTokenFrom(t, armed)

	// ...and try to spend it at the DEPARTMENT gate.
	rec := b.do(http.MethodPost, departmentDeleteHref, url.Values{
		"id": {deptID.String()}, confirmField: {token},
	})
	// 🔴 THE EXACT WORD. `problem=confirm` is a PREFIX of both "confirm-required" and
	// "confirm-stale", so the old assertion passed on either — and only one of them is
	// the refusal this test is about.
	loc := rec.Header().Get("Location")
	if !strings.HasSuffix(loc, "?problem=confirm-stale") &&
		!strings.HasSuffix(loc, "?problem=confirm-required") {
		t.Errorf("a venue confirmation was spent at the department gate: %q", loc)
	}
	if got := f.removals(); len(got) != 0 {
		t.Errorf("a cross-action confirmation removed something: %+v", got)
	}

	// THE POSITIVE CONTROL: the same value at its OWN gate works.
	rec = b.do(http.MethodPost, venueDeleteHref, url.Values{
		"id": {venueID.String()}, confirmField: {token},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the venue confirmation did not open its own gate: %d", rec.Code)
	}
	if got := f.removals(); len(got) != 1 {
		t.Fatalf("the control removed %d row(s), want 1", len(got))
	}
}

// ⚠️ funcNameRE AND topLevelDeclRE USED TO LIVE HERE AND ARE DELETED WITH THE LINE
// SCANNER THEY SERVED. They attributed a reference to its enclosing function by
// pattern-matching column-0 declarations — a job go/ast now does by resolving scope,
// which is the whole point of the rewrite. Keeping them "in case" would leave two
// ways to answer the same question, and the discarded one is the one that was wrong
// twice.

// coordinateReplacerIdent is the identifier the net above tracks. It is a CONSTANT
// rather than a literal so that renaming the value is a compile error here instead of
// a net that silently starts finding nothing.
const coordinateReplacerIdent = "coordinateSeparators"

// --- the verified removal acknowledgement (user decision, 2026-08-09, reading C') ---

// removalBrowser is a signed-in manager whose trail already contains one removal.
func removalBrowser(t *testing.T, action string, target uuid.UUID, name string) (*fakeVenues, *browser) {
	t.Helper()
	f := twoVenues()
	f.receipts = map[string]tenant.RemovalReceipt{
		action + "|" + target.String(): {Found: true, Name: name},
	}
	return f, venueBrowser(t, f)
}

// TestRemovalAcknowledgement_IsPrintedOnlyAgainstAVerifiedTrailRow.
//
// 🔴 THIS IS THE F1 DEFECT'S PERMANENT ANSWER. A `?done=venue-deleted` word was
// measured printing "The venue has been removed" in a browser that had never posted
// anything. M6-05's rule permits an action claim only when the handler checks it
// against a row THE SAME REQUEST READ, and after a deletion the only surviving row is
// the audit entry — so that is what is checked.
func TestRemovalAcknowledgement_IsPrintedOnlyAgainstAVerifiedTrailRow(t *testing.T) {
	venue := uuid.New()
	_, b := removalBrowser(t, tenant.ActionLocationDeleted, venue, "KF Sliema")

	// 1. WITH the receipt: the heading appears, and it NAMES THE ROW FROM THE TRAIL.
	body := htmlOf(t, b.do(http.MethodGet,
		locationsHref+"?done=venue-deleted&id="+venue.String(), nil))
	if !strings.Contains(body, "Venue removed") {
		t.Fatal("a verified removal renders no acknowledgement at all")
	}
	if !strings.Contains(body, "KF Sliema") {
		t.Error("the heading does not name the removed venue; the trail is the only " +
			"surviving source of that name")
	}
	if !strings.Contains(body, "audit trail") {
		t.Error("the sentence does not say the removal itself is recorded")
	}

	// 2. WITHOUT it — same word, same shape of address, no matching trail row. This is
	// the stranger, the stale bookmark and the replayed URL, and it is the case that
	// used to render a claim.
	other := venueBrowser(t, twoVenues()) // no receipts at all
	for _, q := range []string{
		"?done=venue-deleted&id=" + venue.String(),
		"?done=venue-deleted&id=" + uuid.NewString(),
		"?done=venue-deleted",
		"?done=department-deleted&id=" + uuid.NewString(),
	} {
		plain := htmlOf(t, other.do(http.MethodGet, locationsHref+q, nil))
		if strings.Contains(strings.ToLower(plain), "removed") {
			t.Errorf("%s printed a removal claim with no trail row behind it", q)
		}
	}

	// 3. THE ID IS A KEY, NOT A SOURCE OF TEXT. A receipt whose trail row carries a
	// different name must show the TRAIL's name — never anything derived from the URL.
	if strings.Contains(body, venue.String()) {
		t.Error("the acknowledgement echoes the id from the address bar")
	}
}

// TestRemovalAcknowledgement_TheLookupIsScopedToTheSessionsTenantAndActor (§4.5).
func TestRemovalAcknowledgement_TheLookupIsScopedToTheSessionsTenantAndActor(t *testing.T) {
	venue := uuid.New()
	f, b := removalBrowser(t, tenant.ActionLocationDeleted, venue, "KF Sliema")

	b.do(http.MethodGet, locationsHref+"?done=venue-deleted&id="+venue.String(), nil)
	n, seen := f.confirmations()
	if n != 1 {
		t.Fatalf("the section made %d trail lookup(s), want 1", n)
	}
	got := seen[0]
	if got[0] != panelTestTenant.String() {
		t.Errorf("the lookup was scoped to tenant %s, want the session's %s", got[0], panelTestTenant)
	}
	if got[1] != panelTestAdmin.String() {
		t.Errorf("the lookup was scoped to actor %s, want the session's %s. Without the "+
			"actor bound, one manager would be told about a COLLEAGUE's removal.",
			got[1], panelTestAdmin)
	}
	if got[2] != tenant.ActionLocationDeleted {
		t.Errorf("the lookup asked for action %q, want %q", got[2], tenant.ActionLocationDeleted)
	}
	if got[3] != venue.String() {
		t.Errorf("the lookup asked for target %q, want the id in the address", got[3])
	}
}

// TestRemovalAcknowledgement_TheFastPathPaysNothing.
//
// 🔴 COUNTED IN QUERIES, NOT IN A FLAG. The same claim was made twice in this task
// with an assertion that could not see the cost; here the fake counts the lookups.
func TestRemovalAcknowledgement_TheFastPathPaysNothing(t *testing.T) {
	f := twoVenues()
	b := venueBrowser(t, f)

	// Everything a manager does that is NOT a removal acknowledgement.
	b.do(http.MethodGet, locationsHref, nil)
	b.do(http.MethodGet, locationsHref+"?venue=new", nil)
	b.do(http.MethodGet, locationsHref+"?done=venue-saved", nil)
	b.do(http.MethodGet, locationsHref+"?done=department-added", nil)
	b.do(http.MethodGet, locationsHref+"?problem=unknown-venue", nil)
	b.do(http.MethodGet, locationsHref+"?venue="+f.firstVenueID().String(), nil)
	if n, _ := f.confirmations(); n != 0 {
		t.Errorf("six ordinary requests cost %d trail lookup(s); the acknowledgement "+
			"query must run only for the two words that need it", n)
	}

	// A removal word with an UNREADABLE id also costs nothing: nothing was looked up.
	b.do(http.MethodGet, locationsHref+"?done=venue-deleted&id=nonsense", nil)
	b.do(http.MethodGet, locationsHref+"?done=venue-deleted", nil)
	if n, _ := f.confirmations(); n != 0 {
		t.Errorf("a removal word with no readable id cost %d lookup(s)", n)
	}

	// THE CONTROL: a well-formed one DOES pay exactly one, or the assertions above
	// would pass over a section that never looks anything up.
	b.do(http.MethodGet, locationsHref+"?done=venue-deleted&id="+uuid.NewString(), nil)
	if n, _ := f.confirmations(); n != 1 {
		t.Errorf("a readable removal acknowledgement cost %d lookup(s), want 1", n)
	}
}

// TestRemovalAcknowledgement_IsNoOracle.
//
// 🔴 FOUR DIFFERENT INPUTS MUST BE ONE ANSWER. Another business's genuinely removed
// id, a colleague's removal inside this business, a uuid that never existed and a
// malformed one must be indistinguishable — otherwise the banner becomes a way to ask
// the database questions about rows the asker may not see.
func TestRemovalAcknowledgement_IsNoOracle(t *testing.T) {
	// The fake holds NO receipt for any of these, which is what the tenant+actor
	// scoped query returns for all four (venue_db_test.go proves that against real
	// Postgres; here the property under test is that the SECTION cannot tell them
	// apart).
	f := twoVenues()
	f.receipts = map[string]tenant.RemovalReceipt{}
	b := venueBrowser(t, f)

	probes := []struct{ name, id string }{
		{"another business's removed id", uuid.NewString()},
		{"a colleague's removal in this business", uuid.NewString()},
		{"a uuid that never existed", uuid.NewString()},
		{"a malformed uuid", "not-a-uuid"},
	}
	var bodies []string
	var codes []int
	for _, p := range probes {
		rec := b.do(http.MethodGet, locationsHref+"?done=venue-deleted&id="+p.id, nil)
		codes = append(codes, rec.Code)
		bodies = append(bodies, htmlOf(t, rec))
	}
	for i := 1; i < len(probes); i++ {
		if codes[i] != codes[0] {
			t.Errorf("%s answered %d and %s answered %d", probes[i].name, codes[i],
				probes[0].name, codes[0])
		}
		if bodies[i] != bodies[0] {
			t.Errorf("%s renders a DIFFERENT page from %s; the acknowledgement is an "+
				"oracle for whether an id was removed", probes[i].name, probes[0].name)
		}
	}
	// ANTI-VACUITY: a page that really does differ when it should is detectable by the
	// same comparison, or the loop above would pass on any two identical error pages.
	venue := uuid.New()
	_, b2 := removalBrowser(t, tenant.ActionLocationDeleted, venue, "KF Sliema")
	found := htmlOf(t, b2.do(http.MethodGet,
		locationsHref+"?done=venue-deleted&id="+venue.String(), nil))
	if found == bodies[0] {
		t.Fatal("a VERIFIED removal renders the same page as an unverified one; the " +
			"comparison above cannot tell anything apart")
	}
}

// TestRemovalAcknowledgement_AFailedLookupIsSilenceNotAnOutage. The trail read decides
// a SENTENCE, not a permission, so its failure must not cost the manager the screen.
func TestRemovalAcknowledgement_AFailedLookupIsSilenceNotAnOutage(t *testing.T) {
	f := twoVenues()
	f.confirmErr = errors.New("connection refused")
	b := venueBrowser(t, f)

	rec := b.do(http.MethodGet, locationsHref+"?done=venue-deleted&id="+uuid.NewString(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("a failed trail read answered %d; it decides whether a sentence is "+
			"printed, not whether the page may be served", rec.Code)
	}
	body := htmlOf(t, rec)
	if strings.Contains(strings.ToLower(body), "removed") {
		t.Error("a failed trail read printed the acknowledgement anyway")
	}
	// The list itself is still there.
	if !strings.Contains(body, "KF St Julians") {
		t.Error("a failed trail read cost the manager the venue list")
	}
}

// TestRemovalWords_AgreeBetweenHandlerAndTemplate. The handler decides which words
// need verifying and the template decides which words are gated on the receipt; two
// lists that disagree would let a word be printed unverified again.
func TestRemovalWords_AgreeBetweenHandlerAndTemplate(t *testing.T) {
	if len(venueRemovalWords) == 0 {
		t.Fatal("the handler names no removal words; this test would be vacuous")
	}
	for _, w := range venueDoneWords {
		_, handlerSays := venueRemovalWords[w]
		templateSays := pages.ClaimsARemoval(w)
		if handlerSays != templateSays {
			t.Errorf("%q: the handler says needs-verification=%v and the template says "+
				"%v. A word the template renders on Done alone is a word that can be "+
				"printed by a stranger's URL.", w, handlerSays, templateSays)
		}
	}
	// AND EVERY WORD THE HANDLER GATES MUST BE IN THE VOCABULARY, or it gates nothing.
	for w := range venueRemovalWords {
		if oneOfWords(w, venueDoneWords...) == "" {
			t.Errorf("%q is gated by the handler but is not an accepted word", w)
		}
	}
}

// --- removal is owner-only (user decision, 2026-08-09) ------------------------------

// managerBrowser is a signed-in admin whose role is `manager` rather than `owner`.
func managerBrowser(t *testing.T, venues panelVenues) *browser {
	t.Helper()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: "manager", FullName: "KF Shift Manager",
		}, nil
	}}
	b := newBrowser(t, newAdminRouterWithVenues(t, admins, venues))
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

// TestRemoval_IsOwnerOnly_TheControlIsAbsentForAManager.
//
// 🔴 THE SCREEN AND THE SERVER ARE TWO SEPARATE OBLIGATIONS AND THIS IS THE FIRST.
// Hiding the control is the courtesy; the handler guard is the guarantee. They are
// tested apart because a change can break either one alone — and a screen that offers
// a control the server refuses is the defect this section has spent four review rounds
// closing.
func TestRemoval_IsOwnerOnly_TheControlIsAbsentForAManager(t *testing.T) {
	f := twoVenues()
	venue, dept := f.firstVenueID(), f.firstDepartmentID()
	// EMPTY, so the ONLY thing withholding the control is the role.
	f.refs = map[uuid.UUID]tenant.References{venue: {}, dept: {}}
	b := managerBrowser(t, f)

	for _, tc := range []struct{ name, query, kind string }{
		{"a venue", "?venue=" + venue.String(), "venue"},
		{"a department", "?department=" + dept.String(), "department"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := htmlOf(t, b.do(http.MethodGet, locationsHref+tc.query, nil))

			// NO FORM POSTING TO EITHER REMOVAL ROUTE, and no link to the warning.
			for _, m := range postFormRE.FindAllStringSubmatch(body, -1) {
				if c := attrValueRE("action").FindStringSubmatch(m[1]); len(c) == 2 {
					target := htmlUnescaper.Replace(c[1])
					if target == venueDeleteHref || target == departmentDeleteHref {
						t.Errorf("a manager is offered a form posting to %s", target)
					}
				}
			}
			if strings.Contains(body, "confirm=remove") {
				t.Error("a manager is offered the link that arms the removal")
			}
			// 🔴 AND THE REASON IS STATED. An absent control with no explanation reads
			// as a missing feature and sends somebody hunting.
			if !strings.Contains(body, "reserved for an owner") {
				t.Error("the card withholds the control without saying why")
			}
			// IT MUST NOT BE CONFUSED WITH "still in use" — different sentence, different
			// fix. The venue here is empty.
			if strings.Contains(body, "cannot be removed while anything still belongs") {
				t.Error("a role refusal is rendered as an in-use refusal, which sends the " +
					"manager to move staff around to unlock a button that was never theirs")
			}
		})
	}
}

// TestRemoval_IsOwnerOnly_TheServerRefusesAManagersPost is the second obligation: a
// POST that skips the screen entirely.
func TestRemoval_IsOwnerOnly_TheServerRefusesAManagersPost(t *testing.T) {
	for _, tc := range []struct{ name, route string }{
		{"a venue", venueDeleteHref},
		{"a department", departmentDeleteHref},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := twoVenues()
			id := f.firstVenueID()
			f.refs = map[uuid.UUID]tenant.References{id: {}}

			// The confirmation is minted by an OWNER, so the only thing standing between
			// this POST and a deletion is the role gate.
			owner := venueBrowser(t, f)
			armed := htmlOf(t, owner.do(http.MethodGet,
				locationsHref+"?venue="+id.String()+"&confirm=remove", nil))
			token := confirmTokenFrom(t, armed)

			m := managerBrowser(t, f)
			m.cookies[adminConfirmCookieName] = token
			rec := m.do(http.MethodPost, tc.route, url.Values{
				"id": {id.String()}, confirmField: {token},
			})
			if loc := rec.Header().Get("Location"); !strings.HasSuffix(loc, "?problem=not-permitted") {
				t.Errorf("a manager's POST to %s answered %d / %q; it must be refused by "+
					"the SERVER, not only hidden by the screen", tc.route, rec.Code, loc)
			}
			if got := f.removals(); len(got) != 0 {
				t.Fatalf("a manager's POST removed something: %+v", got)
			}
			// The page it lands on explains it and points somewhere.
			body := htmlOf(t, m.do(http.MethodGet, rec.Header().Get("Location"), nil))
			if !strings.Contains(body, "reserved for an owner") {
				t.Error("the refusal page does not explain who may do this")
			}
		})
	}
}

// TestRemoval_IsOwnerOnly_AManagerCannotMintTheConfirmation.
//
// 🔴 WITHOUT THIS THE GATE WOULD BE TWO-STAGE WITH THE FIRST STAGE OPEN. A manager who
// could mint a confirmation would hold a server-signed value for an act they may not
// perform — and the cookie it sets is the same one the roster's deactivation uses, so
// a stray mint would also displace a legitimate warning.
func TestRemoval_IsOwnerOnly_AManagerCannotMintTheConfirmation(t *testing.T) {
	f := twoVenues()
	id := f.firstVenueID()
	f.refs = map[uuid.UUID]tenant.References{id: {}}
	m := managerBrowser(t, f)

	rec := m.do(http.MethodGet, locationsHref+"?venue="+id.String()+"&confirm=remove", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET the warning as a manager: %d, want 200", rec.Code)
	}
	body := htmlOf(t, rec)
	if strings.Contains(body, `name="`+confirmField+`"`) {
		t.Error("a manager was handed a signed confirmation for an act they may not perform")
	}
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == adminConfirmCookieName && ck.Value != "" {
			t.Error("a manager's request set the confirmation cookie, which would also " +
				"displace a legitimate deactivation warning in the same browser")
		}
	}
}

// TestRemoval_IsOwnerOnly_AnOwnerIsUnaffected is the positive control, and without it
// every assertion above is satisfied by a section that refuses everybody.
func TestRemoval_IsOwnerOnly_AnOwnerIsUnaffected(t *testing.T) {
	f := twoVenues()
	id := f.firstVenueID()
	f.refs = map[uuid.UUID]tenant.References{id: {}}
	b := venueBrowser(t, f) // role: owner

	body := htmlOf(t, b.do(http.MethodGet, locationsHref+"?venue="+id.String(), nil))
	if !strings.Contains(body, "Remove this venue") {
		t.Fatal("an owner is not offered the control; the refusals above prove nothing")
	}
	if strings.Contains(body, "reserved for an owner") {
		t.Error("an owner is told the act is reserved")
	}

	armed := htmlOf(t, b.do(http.MethodGet,
		locationsHref+"?venue="+id.String()+"&confirm=remove", nil))
	token := confirmTokenFrom(t, armed)
	rec := b.do(http.MethodPost, venueDeleteHref, url.Values{
		"id": {id.String()}, confirmField: {token},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("an owner's removal answered %d, want 303", rec.Code)
	}
	if got := f.removals(); len(got) != 1 {
		t.Fatalf("an owner's removal built %d command(s), want 1", len(got))
	}

	// 🔴 AND THE SCOPE IS REMOVAL ONLY. An owner-only rule that quietly spread to
	// saving would be the silent widening the working rules forbid, so a manager's
	// ordinary edit is driven here.
	f2 := twoVenues()
	m := managerBrowser(t, f2)
	if rec := m.do(http.MethodPost, venueSaveHref, url.Values{
		"name": {"KF Manager Edit"},
	}); rec.Code != http.StatusSeeOther {
		t.Fatalf("a manager's SAVE answered %d; only removal is owner-only", rec.Code)
	}
	if venues, _ := f2.saved(); len(venues) != 1 {
		t.Errorf("a manager's save built %d command(s), want 1", len(venues))
	}
	if rec := m.do(http.MethodPost, departmentSaveHref, url.Values{
		"location_id": {f2.firstVenueID().String()}, "name": {"Bar"},
	}); rec.Code != http.StatusSeeOther {
		t.Errorf("a manager's department save answered %d; only removal is owner-only", rec.Code)
	}
}

// TestRemoval_TheCardNamesOnlyWhatItsQueryCounted.
//
// 🔴 ONE SENTENCE WAS SERVING TWO DIFFERENT CHECKS. Both cards printed "No
// departments, employees, plaques or records belong to this ..." — true of a venue,
// whose query counts four kinds, and FALSE of a department, whose query counts two.
// departments_location_fk and tags_location_fk point at LOCATIONS, so a department has
// neither sub-departments nor plaques of its own; the card was naming a search that
// never ran, which is the exact shape unresolvedCardProblem refuses elsewhere in the
// same file.
func TestRemoval_TheCardNamesOnlyWhatItsQueryCounted(t *testing.T) {
	f := twoVenues()
	venue, dept := f.firstVenueID(), f.firstDepartmentID()
	f.refs = map[uuid.UUID]tenant.References{venue: {}, dept: {}}
	b := venueBrowser(t, f)

	venueBody := htmlOf(t, b.do(http.MethodGet, locationsHref+"?venue="+venue.String(), nil))
	deptBody := htmlOf(t, b.do(http.MethodGet, locationsHref+"?department="+dept.String(), nil))

	// A VENUE'S QUERY COUNTS FOUR KINDS, so its card names four.
	for _, want := range venueCountedNames {
		if !strings.Contains(venueBody, want) {
			t.Errorf("the venue card does not name %q, which its query counts", want)
		}
	}
	// A DEPARTMENT'S COUNTS TWO, so its card names two — and must NOT name the other
	// two, which nothing looked for.
	for _, want := range departmentCountedNames {
		if !strings.Contains(deptBody, want) {
			t.Errorf("the department card does not name %q, which its query counts", want)
		}
	}
	for _, mustNot := range []string{"departments", "plaques"} {
		if strings.Contains(deptBody, "No ") && strings.Contains(deptBody,
			mustNot+" belong to this department") {
			t.Errorf("the department card claims nothing about %q belongs to it, but its "+
				"query never counts them — a department has no sub-departments and no "+
				"plaques of its own", mustNot)
		}
	}
	// 🔴 THE DECISIVE ASSERTION: the exact sentence must differ between the two cards.
	// A shared hard-coded string passes every loop above.
	const venueClaim = "No departments, employees, plaques or recorded taps belong to this venue"
	const deptClaim = "No employees or recorded taps belong to this department"
	if !strings.Contains(venueBody, venueClaim) {
		t.Errorf("the venue card does not read %q", venueClaim)
	}
	if !strings.Contains(deptBody, deptClaim) {
		t.Errorf("the department card does not read %q", deptClaim)
	}

	// AND THE LISTS ARE THE QUERIES'. venueReasons/departmentReasons render exactly the
	// kinds their CountXReferences statement selects, so a kind added to one without
	// the other shows up here rather than in a sentence nobody re-reads.
	full := tenant.References{Departments: 1, Employees: 1, Tags: 1, Transactions: 1}
	if got := len(venueReasons(full)); got != len(venueCountedNames) {
		t.Errorf("venueReasons renders %d kind(s) but the card names %d", got, len(venueCountedNames))
	}
	if got := len(departmentReasons(full)); got != len(departmentCountedNames) {
		t.Errorf("departmentReasons renders %d kind(s) but the card names %d",
			got, len(departmentCountedNames))
	}
}

// TestRemoval_AManagerPaysNoReferenceQuery.
//
// 🔴 COUNTED IN QUERIES. The role gate used to run AFTER the reference count and
// removalView simply discarded the answer — so a manager paid a four-count read
// (~30 ms, the transactions predicate has no index) for a control they were never
// going to be offered. The comment claimed the check came "before the reference count
// is even consulted", which was true of the reasoning and false of the statements.
func TestRemoval_AManagerPaysNoReferenceQuery(t *testing.T) {
	f := twoVenues()
	venue, dept := f.firstVenueID(), f.firstDepartmentID()
	f.refs = map[uuid.UUID]tenant.References{venue: {}, dept: {}}
	m := managerBrowser(t, f)

	m.do(http.MethodGet, locationsHref+"?venue="+venue.String(), nil)
	m.do(http.MethodGet, locationsHref+"?department="+dept.String(), nil)
	if n := f.referenceCalls(); n != 0 {
		t.Errorf("a manager opening two cards cost %d reference query/queries; the role "+
			"is a local fact and is cheaper to ask first", n)
	}

	// THE CONTROL: an owner DOES pay, or the assertion above would pass over a section
	// that never counts anything.
	f2 := twoVenues()
	v2 := f2.firstVenueID()
	f2.refs = map[uuid.UUID]tenant.References{v2: {}}
	o := venueBrowser(t, f2)
	o.do(http.MethodGet, locationsHref+"?venue="+v2.String(), nil)
	if n := f2.referenceCalls(); n != 1 {
		t.Errorf("an owner opening one card cost %d reference query/queries, want 1", n)
	}
}
