package tenant

// venue_db_test.go -- M6-06 phase A's write path against REAL Postgres.
//
// 🔴 A FAKE CANNOT TEST ANY OF THIS, and the three properties below are exactly the
// ones a mock would agree with unconditionally:
//
//   - an EMPTY shift box reaches the COLUMN as NULL rather than as 00:00, and an
//     empty coordinate as NULL rather than as 0,0. Both wrong values are perfectly
//     legal in the schema -- locations_shift_pair only refuses a HALF-filled pair and
//     locations_gps_pair accepts 0,0 -- so nothing but a real column can say which
//     one is there. A midnight shift marks every arrival at that venue late forever;
//     a Gulf-of-Guinea coordinate silently costs the GPS half of proof of place.
//   - the audit row SHARES THE WRITE'S TRANSACTION. `locations` has no updated_at, so
//     if the trail can be lost while the change survives, a change to the evidence a
//     tap is judged on leaves NO TRACE ANYWHERE. Measured by breaking the trail and
//     counting the venue rows that remain.
//   - section 4.5. A foreign id writes nothing, and it is REFUSED rather than
//     erroring, because the explicit tenant predicate and the RLS policy both bite.
//
// FIXTURES ARE NOT CLEANED UP, for the reason staff_db_test.go states: audit_log is
// append-only (00005) and deleting the rest would need the migration role. Fresh
// random ids keep runs from colliding, and `make db-reset` clears the development
// database.

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// venueFixture is one tenant with a venue and a department, plus a SECOND tenant to
// attack it from.
type venueFixture struct {
	data   *db.DB
	venues *Venues
	trail  *audit.Recorder

	tenantID     uuid.UUID
	locationID   uuid.UUID
	departmentID uuid.UUID
	actorID      uuid.UUID

	foreignTenant     uuid.UUID
	foreignVenue      uuid.UUID
	foreignDepartment uuid.UUID
}

func newVenueFixture(t *testing.T) *venueFixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping venue write tests (real Postgres required)")
	}
	data, err := db.New(context.Background(), &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)

	trail, err := audit.New(data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	venues, err := NewVenues(data, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewVenues: %v", err)
	}
	f := &venueFixture{
		data: data, venues: venues, trail: trail,
		tenantID: uuid.New(), locationID: uuid.New(), departmentID: uuid.New(),
		actorID:       uuid.New(),
		foreignTenant: uuid.New(), foreignVenue: uuid.New(), foreignDepartment: uuid.New(),
	}
	f.seed(t, f.tenantID, f.locationID, f.departmentID)
	f.seed(t, f.foreignTenant, f.foreignVenue, f.foreignDepartment)
	return f
}

func (f *venueFixture) seed(t *testing.T, tenantID, venue, department uuid.UUID) {
	t.Helper()
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Kebab Factory Ltd', $2, 'restaurant', 'multi')`,
			tenantID, "VAT-"+tenantID.String()[:8]); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name, static_ips, gps_lat, gps_lng,
			                        shift_start, shift_end, overnight)
			 VALUES ($1, $2, 'St Julians', '{192.168.1.0/24}', 35.917270, 14.489540,
			         '08:00', '17:00', false)`, venue, tenantID); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO departments (id, tenant_id, location_id, name, shift_start, shift_end)
			 VALUES ($1, $2, $3, 'Kitchen', '07:00', '15:00')`, department, tenantID, venue)
		return e
	})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
}

// storedShift reads the two `time` columns and says, for EACH, whether it is NULL and
// what it holds. Two facts rather than one, because "NULL" and "00:00" are the pair
// this whole file exists to keep apart and a single nullable value collapses them.
func (f *venueFixture) storedShift(t *testing.T, table string, tenantID, id uuid.UUID) (startNull, endNull bool, startMicros, endMicros int64) {
	t.Helper()
	var s, e *int64
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			// EXTRACT(EPOCH) on a `time` yields seconds since midnight; x 1e6 gives the
			// same unit pgtype.Time uses, so a stored midnight reads as 0 and a NULL
			// reads as nil. Written out rather than scanned into pgtype.Time so the
			// assertion below is about the COLUMN and not about a driver mapping.
			`SELECT (EXTRACT(EPOCH FROM shift_start) * 1000000)::bigint,
			        (EXTRACT(EPOCH FROM shift_end)   * 1000000)::bigint
			   FROM `+table+` WHERE tenant_id = $1 AND id = $2`,
			tenantID, id).Scan(&s, &e)
	})
	if err != nil {
		t.Fatalf("read shift from %s: %v", table, err)
	}
	startNull, endNull = s == nil, e == nil
	if s != nil {
		startMicros = *s
	}
	if e != nil {
		endMicros = *e
	}
	return
}

func (f *venueFixture) storedGPS(t *testing.T, tenantID, id uuid.UUID) (latNull, lngNull bool, lat, lng string) {
	t.Helper()
	var la, ln *string
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT gps_lat::text, gps_lng::text FROM locations WHERE tenant_id = $1 AND id = $2`,
			tenantID, id).Scan(&la, &ln)
	})
	if err != nil {
		t.Fatalf("read gps: %v", err)
	}
	latNull, lngNull = la == nil, ln == nil
	if la != nil {
		lat = *la
	}
	if ln != nil {
		lng = *ln
	}
	return
}

// compactJSON removes the whitespace jsonb's text rendering inserts, so an assertion
// can name a key and its value as one token.
//
// ⚠️ IT IS HERE BECAUSE THE FIRST DRAFT ASSERTED ON `"lost_proof_of_place":true` AND
// POSTGRES RENDERS `"lost_proof_of_place": true`. The tests failed on their own
// spelling while the audit rows were correct -- which is the failure mode where
// somebody "fixes" the code to match a broken assertion. Stripping the space is
// safe here because every value asserted on below is a literal, a number or a
// bracket, never a string that could contain one.
func compactJSON(s string) string {
	return strings.NewReplacer(": ", ":", ", ", ",").Replace(s)
}

// trailFor counts the audit rows for one target, and returns the newest detail with
// jsonb's rendering whitespace removed.
func (f *venueFixture) trailFor(t *testing.T, tenantID uuid.UUID, target string) (int, string) {
	t.Helper()
	var n int
	var detail *string
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if e := tx.QueryRow(ctx,
			`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND target = $2`,
			tenantID, target).Scan(&n); e != nil {
			return e
		}
		return tx.QueryRow(ctx,
			`SELECT detail::text FROM audit_log
			  WHERE tenant_id = $1 AND target = $2
			  ORDER BY at DESC, id DESC LIMIT 1`,
			tenantID, target).Scan(&detail)
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("read trail: %v", err)
	}
	if detail == nil {
		return n, ""
	}
	return n, compactJSON(*detail)
}

func (f *venueFixture) nameOf(t *testing.T, table string, tenantID, id uuid.UUID) string {
	t.Helper()
	var name string
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT name FROM `+table+` WHERE tenant_id = $1 AND id = $2`, tenantID, id).Scan(&name)
	})
	if err != nil {
		t.Fatalf("read name from %s: %v", table, err)
	}
	return name
}

// --- the absence tests -------------------------------------------------------------

// TestVenues_EmptyShiftStoresNULLNotMidnight is the test venue.go names, carried all
// the way to the COLUMN.
//
// 🔴 THE SCHEMA CANNOT CATCH THIS. locations_shift_pair refuses a HALF-filled pair and
// nothing else; 00:00 is a perfectly good `time`. So a nil *Shift that arrived at pgx
// as pgtype.Time{Microseconds: 0, Valid: true} would store midnight, mark every
// arrival at that venue late FOREVER, raise no error and show nothing on any screen.
// The only place that distinction can be measured is here.
func TestVenues_EmptyShiftStoresNULLNotMidnight(t *testing.T) {
	f := newVenueFixture(t)
	ctx := context.Background()

	noShift, err := f.venues.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, ActorID: f.actorID, Name: "No shift here",
	})
	if err != nil {
		t.Fatalf("SaveVenue: %v", err)
	}
	startNull, endNull, _, _ := f.storedShift(t, "locations", f.tenantID, noShift.ID)
	if !startNull || !endNull {
		t.Fatalf("an empty shift stored NON-NULL columns (start null=%v, end null=%v). "+
			"NULL means lateness is NOT COMPUTED for this venue; 00:00 means the shift "+
			"starts at midnight and every arrival is late (CLAUDE.md section 5, M4-05).",
			startNull, endNull)
	}
	if noShift.Shift != nil {
		t.Errorf("the returned venue carries %+v, want nil", *noShift.Shift)
	}

	// 🔴 THE POSITIVE CONTROL, WITHOUT WHICH THE ASSERTION ABOVE IS SATISFIED BY A
	// FUNCTION THAT NEVER STORES A SHIFT AT ALL. Midnight must still be expressible,
	// and it must land in the column as a real zero rather than as NULL.
	midnight, err := f.venues.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, ActorID: f.actorID, Name: "Midnight shift",
		Shift: &Shift{Start: 0, End: 0},
	})
	if err != nil {
		t.Fatalf("SaveVenue (midnight): %v", err)
	}
	startNull, endNull, startMicros, endMicros := f.storedShift(t, "locations", f.tenantID, midnight.ID)
	if startNull || endNull {
		t.Fatalf("an EXPLICIT midnight shift stored NULL (start null=%v, end null=%v); the "+
			"two states are now indistinguishable in the database", startNull, endNull)
	}
	if startMicros != 0 || endMicros != 0 {
		t.Errorf("midnight stored as %d/%d microseconds, want 0/0", startMicros, endMicros)
	}

	// AND AN ORDINARY SHIFT ROUND-TRIPS, including the overnight flag, because a shift
	// that stores but comes back wrong is the same defect one layer along.
	rusty, err := f.venues.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, ActorID: f.actorID, Name: "Rusty Bar",
		Shift: &Shift{Start: 18 * time.Hour, End: 2 * time.Hour, Overnight: true},
	})
	if err != nil {
		t.Fatalf("SaveVenue (overnight): %v", err)
	}
	if rusty.Shift == nil {
		t.Fatal("an overnight shift came back nil")
	}
	if *rusty.Shift != (Shift{Start: 18 * time.Hour, End: 2 * time.Hour, Overnight: true}) {
		t.Errorf("the overnight shift round-tripped as %+v", *rusty.Shift)
	}

	// 🔴 AND CLEARING A SHIFT ON AN EXISTING VENUE REALLY CLEARS IT. This is the
	// destructive direction and the one an UPDATE that "only writes what changed"
	// would get wrong: the fixture venue starts with 08:00-17:00.
	cleared, err := f.venues.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, VenueID: f.locationID, ActorID: f.actorID,
		Name: "St Julians", Shift: nil,
	})
	if err != nil {
		t.Fatalf("SaveVenue (clear): %v", err)
	}
	if cleared.Shift != nil {
		t.Errorf("clearing the shift returned %+v", *cleared.Shift)
	}
	if startNull, endNull, _, _ := f.storedShift(t, "locations", f.tenantID, f.locationID); !startNull || !endNull {
		t.Error("clearing a shift left the columns set; a clear that is a no-op is worse " +
			"than one that fails, because the screen says it worked")
	}
}

// TestVenues_EmptyGPSStoresNULLNotZero. 0,0 passes locations_gps_pair and is a real
// point in the Gulf of Guinea, so a venue registered there is about 5 000 km from
// where anybody taps: the GPS half of proof of place would never match and the venue
// would silently fall to IP-only (section 5, row 6).
func TestVenues_EmptyGPSStoresNULLNotZero(t *testing.T) {
	f := newVenueFixture(t)
	ctx := context.Background()

	none, err := f.venues.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, ActorID: f.actorID, Name: "No coordinate",
	})
	if err != nil {
		t.Fatalf("SaveVenue: %v", err)
	}
	latNull, lngNull, lat, lng := f.storedGPS(t, f.tenantID, none.ID)
	if !latNull || !lngNull {
		t.Fatalf("an empty coordinate stored %q/%q rather than NULL; 0,0 is a real place "+
			"and passes the gps_pair CHECK", lat, lng)
	}

	// THE POSITIVE CONTROL: the null island is storable and is not the same as absent.
	zero, err := f.venues.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, ActorID: f.actorID, Name: "Null island",
		GPS: &GPS{Lat: 0, Lng: 0},
	})
	if err != nil {
		t.Fatalf("SaveVenue (0,0): %v", err)
	}
	if latNull, lngNull, _, _ := f.storedGPS(t, f.tenantID, zero.ID); latNull || lngNull {
		t.Fatal("an EXPLICIT 0,0 stored NULL; the two states are indistinguishable")
	}

	// 🔴 AND A REAL COORDINATE KEEPS ITS SIX DECIMAL PLACES. The column is
	// numeric(9,6) -- about 11 cm -- and the value reaches it as a six-decimal STRING
	// rather than as a float parameter, so that the rounding is this code's decision
	// rather than a negotiation between two layers that disagree.
	real, err := f.venues.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, ActorID: f.actorID, Name: "St Julians precise",
		GPS: &GPS{Lat: 35.917270, Lng: 14.489540},
	})
	if err != nil {
		t.Fatalf("SaveVenue (precise): %v", err)
	}
	if _, _, lat, lng := f.storedGPS(t, f.tenantID, real.ID); lat != "35.917270" || lng != "14.489540" {
		t.Errorf("the coordinate stored as %q/%q, want 35.917270/14.489540", lat, lng)
	}
	if real.GPS == nil || real.GPS.Lat != 35.917270 || real.GPS.Lng != 14.489540 {
		t.Errorf("the coordinate round-tripped as %+v", real.GPS)
	}

	// CLEARING A COORDINATE ON AN EXISTING VENUE really clears it (the fixture has one).
	if _, err := f.venues.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, VenueID: f.locationID, ActorID: f.actorID,
		Name: "St Julians", GPS: nil,
	}); err != nil {
		t.Fatalf("SaveVenue (clear gps): %v", err)
	}
	if latNull, lngNull, _, _ := f.storedGPS(t, f.tenantID, f.locationID); !latNull || !lngNull {
		t.Error("clearing the coordinate left the columns set")
	}
}

// TestVenues_AnIncomparableCoordinateIsRefusedByTheColumn.
//
// 🔴 THIS TEST DOES NOT DEFEND ANYTHING — IT EXPLAINS WHY THE BOUNDARY MUST. The
// question it answers is what happens when a NaN coordinate gets past
// internal/handler's validation, because for a while one did: the guard was written
// as `f < -90 || f > 90`, every comparison against NaN is false, and
// strconv.ParseFloat("NaN", 64) succeeds. The value travelled as far as this layer,
// FormatFloat rendered it "NaN", pgtype.Numeric ACCEPTED it, and the column refused
// it — as a 23514, which the panel can only render as "we are unavailable".
//
// So the database IS a backstop and it is the WRONG ONE: it turns a typo into a claim
// about our own system and costs the manager the other seven fields they typed
// (internal/handler/locations_test.go asserts that half). Recording the mechanism
// here means a future edit that loosens the boundary can see what it is loosening.
func TestVenues_AnIncomparableCoordinateIsRefusedByTheColumn(t *testing.T) {
	f := newVenueFixture(t)
	ctx := context.Background()
	before := f.venueCount(t, f.tenantID)

	for _, tc := range []struct {
		name string
		gps  *GPS
	}{
		{"NaN latitude", &GPS{Lat: math.NaN(), Lng: 14.4}},
		{"NaN longitude", &GPS{Lat: 35.9, Lng: math.NaN()}},
		{"infinite latitude", &GPS{Lat: math.Inf(1), Lng: 14.4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.venues.SaveVenue(ctx, VenueCommand{
				TenantID: f.tenantID, ActorID: f.actorID, Name: "Should not exist", GPS: tc.gps,
			})
			if err == nil {
				t.Fatalf("SECTION 5: a venue was registered at %v. Proof of place would "+
					"never match there and every tap would fall to the other half.", tc.gps)
			}
			// It is a DRIVER-LEVEL failure, not a refusal a manager can act on. That is
			// the finding, so it is asserted rather than merely tolerated.
			if errors.Is(err, ErrVenueName) || errors.Is(err, ErrUnknownVenue) {
				t.Errorf("the refusal arrived as a domain sentinel (%v); if the domain can "+
					"name this, the boundary test above is measuring the wrong thing", err)
			}
		})
	}
	if after := f.venueCount(t, f.tenantID); after != before {
		t.Errorf("%d venue(s) with an unusable coordinate were stored", after-before)
	}
}

// TestVenues_ByIdReadsAreTenantScoped covers the two reads M6-06 phase A added as the
// fallback behind the capped screen lists (§4.5). They are the ONLY queries in this
// file a client can aim an arbitrary id at, so a leak here would hand another
// business's venue to an edit form.
func TestVenues_ByIdReadsAreTenantScoped(t *testing.T) {
	f := newVenueFixture(t)
	ctx := context.Background()

	// POSITIVE CONTROL FIRST: our own rows resolve, or the refusals below prove nothing.
	if v, err := f.venues.Venue(ctx, f.tenantID, f.locationID); err != nil || v.Name != "St Julians" {
		t.Fatalf("control: our own venue read back as %+v, %v", v, err)
	}
	if d, err := f.venues.Department(ctx, f.tenantID, f.departmentID); err != nil || d.Name != "Kitchen" {
		t.Fatalf("control: our own department read back as %+v, %v", d, err)
	} else if d.LocationName != "St Julians" {
		t.Errorf("control: the department did not carry its venue's name (%q); the edit "+
			"form shows that name INSTEAD of a dropdown, so an empty one is a blank card",
			d.LocationName)
	}

	// §4.5: another business's ids are not readable, and the answer is the refusal the
	// handler turns into "not one of this business's" -- which is now only ever said
	// when it is TRUE.
	if _, err := f.venues.Venue(ctx, f.tenantID, f.foreignVenue); !errors.Is(err, ErrUnknownVenue) {
		t.Errorf("SECTION 4.5: reading another business's venue answered %v", err)
	}
	if _, err := f.venues.Department(ctx, f.tenantID, f.foreignDepartment); !errors.Is(err, ErrUnknownDepartment) {
		t.Errorf("SECTION 4.5: reading another business's department answered %v", err)
	}
	// An id that exists nowhere is the same refusal, deliberately: telling the two
	// apart would answer "does this id exist elsewhere?" for anybody who can edit a URL.
	if _, err := f.venues.Venue(ctx, f.tenantID, uuid.New()); !errors.Is(err, ErrUnknownVenue) {
		t.Errorf("an unknown venue id answered %v", err)
	}
}

// TestVenues_ADepartmentsEmptyShiftIsNULLToo. A department's shift BEATS its venue's
// (section 5, Q17, M4-05), so NULL here means "this department follows the venue's
// hours" -- which is a different sentence from "this department starts at midnight",
// and it is the one the screen promises.
func TestVenues_ADepartmentsEmptyShiftIsNULLToo(t *testing.T) {
	f := newVenueFixture(t)
	ctx := context.Background()

	d, err := f.venues.SaveDepartment(ctx, DepartmentCommand{
		TenantID: f.tenantID, ActorID: f.actorID, LocationID: f.locationID, Name: "Bar",
	})
	if err != nil {
		t.Fatalf("SaveDepartment: %v", err)
	}
	if startNull, endNull, _, _ := f.storedShift(t, "departments", f.tenantID, d.ID); !startNull || !endNull {
		t.Fatal("a department created with no shift stored non-NULL columns; its people " +
			"would be judged against midnight rather than against their venue's hours")
	}

	// CLEARING the fixture department's 07:00-15:00 must really clear it.
	if _, err := f.venues.SaveDepartment(ctx, DepartmentCommand{
		TenantID: f.tenantID, DepartmentID: f.departmentID, ActorID: f.actorID,
		Name: "Kitchen", Shift: nil,
	}); err != nil {
		t.Fatalf("SaveDepartment (clear): %v", err)
	}
	if startNull, endNull, _, _ := f.storedShift(t, "departments", f.tenantID, f.departmentID); !startNull || !endNull {
		t.Error("clearing a department's shift left the columns set")
	}

	// THE CONTROL: a real shift still stores.
	if startNull, _, _, _ := f.storedShift(t, "departments", f.tenantID, d.ID); !startNull {
		t.Fatal("fixture confusion")
	}
	set, err := f.venues.SaveDepartment(ctx, DepartmentCommand{
		TenantID: f.tenantID, DepartmentID: d.ID, ActorID: f.actorID, Name: "Bar",
		Shift: &Shift{Start: 18 * time.Hour, End: 2 * time.Hour, Overnight: true},
	})
	if err != nil {
		t.Fatalf("SaveDepartment (set): %v", err)
	}
	if set.Shift == nil || !set.Shift.Overnight || set.Shift.Start != 18*time.Hour {
		t.Errorf("a department shift round-tripped as %+v", set.Shift)
	}
}

// --- static_ips, the half of proof of place a manager can empty by accident ----------

// TestVenues_StaticIPsRoundTripAndAnEmptyListIsAConfiguration.
//
// 🔴 EMPTYING THIS LIST IS A DECISION, NOT A SETTING. static_ips is the IP half of
// proof of place -- 50 of the 100 trust points (section 5, row 6) -- so a venue with
// none falls to the GPS path, and one with neither reaches row 7 and is recorded as
// `flag`. It fails nothing and raises nothing, which is why the domain records it.
func TestVenues_StaticIPsRoundTripAndAnEmptyListIsAConfiguration(t *testing.T) {
	f := newVenueFixture(t)
	ctx := context.Background()

	// A bare address arrives from the boundary already widened to /32, and a range
	// arrives masked -- Postgres refuses host bits, so this is the only shape a cidr
	// column can hold.
	withRanges, err := f.venues.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, ActorID: f.actorID, Name: "Ranged",
		StaticIPs: []netip.Prefix{
			netip.MustParsePrefix("192.168.1.0/24"),
			netip.MustParsePrefix("10.0.0.5/32"),
		},
	})
	if err != nil {
		t.Fatalf("SaveVenue: %v", err)
	}
	if len(withRanges.StaticIPs) != 2 {
		t.Fatalf("stored %d ranges, want 2: %v", len(withRanges.StaticIPs), withRanges.StaticIPs)
	}
	if !withRanges.HasProofOfPlace() {
		t.Error("a venue with address ranges reports no proof of place")
	}

	// 🔴 A nil SLICE MUST NOT BECOME A NULL PARAMETER. static_ips is NOT NULL DEFAULT
	// '{}', so "the manager cleared every range" would be a 500 rather than the
	// configuration change it is.
	emptied, err := f.venues.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, VenueID: withRanges.ID, ActorID: f.actorID,
		Name: "Ranged", StaticIPs: nil,
	})
	if err != nil {
		t.Fatalf("clearing every range failed: %v -- an empty list is migration 00002's "+
			"own '{}' and must not be a 500", err)
	}
	if len(emptied.StaticIPs) != 0 {
		t.Errorf("clearing left %v", emptied.StaticIPs)
	}
	// With no ranges and no coordinate the venue has NO proof of place, which is the
	// state every tap there is recorded as `flag` in.
	if emptied.HasProofOfPlace() {
		t.Error("a venue with no ranges and no coordinate claims proof of place")
	}

	// 🔴 AND THE TRAIL SAYS SO. `locations` has no updated_at, so this row is the only
	// place the change of judgement leaves a mark.
	_, detail := f.trailFor(t, f.tenantID, emptied.ID.String())
	if !strings.Contains(detail, `"lost_proof_of_place":true`) {
		t.Errorf("the audit detail does not record that the venue lost proof of place: %s", detail)
	}
	if !strings.Contains(detail, "192.168.1.0/24") {
		t.Errorf("the audit detail does not record the ranges that were REMOVED, so an "+
			"investigator cannot tell 'narrowed' from 'replaced': %s", detail)
	}
	if !strings.Contains(detail, `"to_static_ips":[]`) {
		t.Errorf("the audit detail does not record the resulting empty list: %s", detail)
	}
}

// --- the trail shares the write's fate ----------------------------------------------

// failingTrail is a Trail that refuses. It exists to answer one question that cannot
// be asked any other way: if the audit row cannot be written, does the CHANGE survive?
type failingTrail struct{ err error }

func (f failingTrail) RecordTx(context.Context, pgx.Tx, audit.Event) (uuid.UUID, error) {
	return uuid.Nil, f.err
}

// TestVenues_TheTrailAndTheChangeShareOneTransaction.
//
// 🔴 THIS IS THE PROPERTY §4.3's ARGUMENT NEEDS ONE LEVEL OUT. The transaction rows
// stay immutable, but the CONFIGURATION that produced their verdicts must not be able
// to change without a trace -- and `locations` has no updated_at, so the audit row is
// the only trace there is. Either half surviving alone is a false statement: a venue
// whose proof of place changed with no trail row, or a trail row saying it changed
// when it did not.
func TestVenues_TheTrailAndTheChangeShareOneTransaction(t *testing.T) {
	f := newVenueFixture(t)
	ctx := context.Background()

	broken, err := NewVenues(f.data, failingTrail{err: errors.New("trail is down")},
		slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewVenues: %v", err)
	}

	// 1. A CREATE whose trail fails must leave NO venue behind.
	before := f.venueCount(t, f.tenantID)
	if _, err := broken.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, ActorID: f.actorID, Name: "Should not exist",
	}); err == nil {
		t.Fatal("a create with a broken trail reported success")
	}
	if after := f.venueCount(t, f.tenantID); after != before {
		t.Errorf("a create whose audit row failed still left %d venue(s) behind. The "+
			"change and the trail must share one transaction, or a change to the evidence "+
			"taps are judged on can happen with no record anywhere.", after-before)
	}

	// 2. An UPDATE whose trail fails must leave the venue UNCHANGED.
	if _, err := broken.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, VenueID: f.locationID, ActorID: f.actorID,
		Name: "Renamed behind the trail's back",
	}); err == nil {
		t.Fatal("an update with a broken trail reported success")
	}
	if name := f.nameOf(t, "locations", f.tenantID, f.locationID); name != "St Julians" {
		t.Errorf("the venue is now named %q; the update survived its own audit failure", name)
	}

	// 3. THE POSITIVE CONTROL. With a working trail the SAME acts do write, and they
	// write exactly ONE audit row each -- otherwise this test would pass against a
	// SaveVenue that never writes anything at all.
	created, err := f.venues.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, ActorID: f.actorID, Name: "Really created",
	})
	if err != nil {
		t.Fatalf("control: SaveVenue: %v", err)
	}
	if n, _ := f.trailFor(t, f.tenantID, created.ID.String()); n != 1 {
		t.Fatalf("control: a create wrote %d audit row(s), want 1", n)
	}
	if _, err := f.venues.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, VenueID: created.ID, ActorID: f.actorID, Name: "Really renamed",
	}); err != nil {
		t.Fatalf("control: SaveVenue (update): %v", err)
	}
	n, detail := f.trailFor(t, f.tenantID, created.ID.String())
	if n != 2 {
		t.Errorf("control: two acts wrote %d audit row(s), want 2", n)
	}
	// THE BEFORE STATE IS READ INSIDE THE WRITE'S TRANSACTION, so the trail describes
	// the row the UPDATE actually changed rather than a snapshot from a round trip
	// earlier. audit_log is append-only: a row describing a state that never existed
	// cannot be corrected.
	if !strings.Contains(detail, `"from_name":"Really created"`) ||
		!strings.Contains(detail, `"to_name":"Really renamed"`) {
		t.Errorf("the update's trail row does not carry both sides of the rename: %s", detail)
	}
	if !strings.Contains(detail, `"location.updated"`) && !f.actionIs(t, f.tenantID, created.ID.String(), ActionLocationUpdated) {
		t.Errorf("the update's trail row does not use the %q action", ActionLocationUpdated)
	}
}

func (f *venueFixture) venueCount(t *testing.T, tenantID uuid.UUID) int {
	t.Helper()
	var n int
	if err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM locations WHERE tenant_id = $1`, tenantID).Scan(&n)
	}); err != nil {
		t.Fatalf("count venues: %v", err)
	}
	return n
}

func (f *venueFixture) actionIs(t *testing.T, tenantID uuid.UUID, target, want string) bool {
	t.Helper()
	var action string
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT action FROM audit_log WHERE tenant_id = $1 AND target = $2
			  ORDER BY at DESC, id DESC LIMIT 1`, tenantID, target).Scan(&action)
	})
	// NO TRAIL AT ALL IS AN ANSWER, NOT A CRASH. A caller asking "did a removal get
	// recorded?" about a row that has never been touched through this package wants
	// false -- and the fixture rows are seeded with raw SQL, so that is the ordinary
	// case rather than an exotic one.
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	if err != nil {
		t.Fatalf("read action: %v", err)
	}
	return action == want
}

// TestVenues_TheDepartmentTrailSharesTheTransactionToo. A department's shift beats its
// venue's, so the same argument applies with the same force.
func TestVenues_TheDepartmentTrailSharesTheTransactionToo(t *testing.T) {
	f := newVenueFixture(t)
	ctx := context.Background()

	broken, err := NewVenues(f.data, failingTrail{err: errors.New("trail is down")},
		slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewVenues: %v", err)
	}
	if _, err := broken.SaveDepartment(ctx, DepartmentCommand{
		TenantID: f.tenantID, ActorID: f.actorID, LocationID: f.locationID, Name: "Ghost",
	}); err == nil {
		t.Fatal("a department create with a broken trail reported success")
	}
	if f.departmentNamed(t, f.tenantID, "Ghost") {
		t.Error("a department whose audit row failed was still created")
	}

	// THE CONTROL.
	d, err := f.venues.SaveDepartment(ctx, DepartmentCommand{
		TenantID: f.tenantID, ActorID: f.actorID, LocationID: f.locationID, Name: "Real",
	})
	if err != nil {
		t.Fatalf("control: SaveDepartment: %v", err)
	}
	if n, _ := f.trailFor(t, f.tenantID, d.ID.String()); n != 1 {
		t.Errorf("control: a department create wrote %d audit row(s), want 1", n)
	}
}

func (f *venueFixture) departmentNamed(t *testing.T, tenantID uuid.UUID, name string) bool {
	t.Helper()
	var n int
	if err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM departments WHERE tenant_id = $1 AND name = $2`,
			tenantID, name).Scan(&n)
	}); err != nil {
		t.Fatalf("count departments: %v", err)
	}
	return n > 0
}

// --- section 4.5: a foreign id writes nothing ---------------------------------------

// TestVenues_AForeignVenueIsRefusedAndNothingIsWritten.
//
// 🔴 BELT AND BRACES (§4.5). The explicit tenant_id predicate and the RLS policy both
// bite, and the result is a REFUSAL rather than a 500 -- ErrUnknownVenue, which the
// handler turns into "that venue is not one of this business's". The other tenant's
// row must be untouched and its trail must stay empty: an act that changed nothing
// must not leave a row saying it did.
func TestVenues_AForeignVenueIsRefusedAndNothingIsWritten(t *testing.T) {
	f := newVenueFixture(t)
	ctx := context.Background()

	beforeName := f.nameOf(t, "locations", f.foreignTenant, f.foreignVenue)
	beforeTrail, _ := f.trailFor(t, f.foreignTenant, f.foreignVenue.String())

	_, err := f.venues.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, // OUR tenant
		VenueID:  f.foreignVenue,
		ActorID:  f.actorID,
		Name:     "Stolen",
	})
	if !errors.Is(err, ErrUnknownVenue) {
		t.Fatalf("editing another business's venue answered %v, want ErrUnknownVenue", err)
	}
	if got := f.nameOf(t, "locations", f.foreignTenant, f.foreignVenue); got != beforeName {
		t.Fatalf("SECTION 4.5 FAILED: the other business's venue is now named %q", got)
	}
	if n, _ := f.trailFor(t, f.foreignTenant, f.foreignVenue.String()); n != beforeTrail {
		t.Errorf("a refused act wrote %d audit row(s) into the other business's trail",
			n-beforeTrail)
	}

	// THE POSITIVE CONTROL: the SAME call against our OWN venue succeeds, so the
	// refusal above is about the tenant and not about the code path being dead.
	if _, err := f.venues.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, VenueID: f.locationID, ActorID: f.actorID, Name: "Renamed fine",
	}); err != nil {
		t.Fatalf("control: renaming our own venue failed: %v", err)
	}
	if got := f.nameOf(t, "locations", f.tenantID, f.locationID); got != "Renamed fine" {
		t.Fatalf("control: our own venue was not renamed (%q)", got)
	}
}

// TestVenues_ADepartmentCannotBeCreatedInAnotherBusinessVenue.
//
// The INSERT ... SELECT matches the location inside the caller's tenant, so a foreign
// location_id produces ZERO ROWS -- a refusal the manager can be shown a sentence
// about, rather than a 23503 the handler has to read out of a driver error string.
// departments_location_fk (a COMPOSITE key on (location_id, tenant_id)) makes the
// cross-tenant pairing structurally impossible underneath; the predicate is what makes
// it a refusal rather than an error.
func TestVenues_ADepartmentCannotBeCreatedInAnotherBusinessVenue(t *testing.T) {
	f := newVenueFixture(t)
	ctx := context.Background()

	_, err := f.venues.SaveDepartment(ctx, DepartmentCommand{
		TenantID: f.tenantID, ActorID: f.actorID,
		LocationID: f.foreignVenue, Name: "Trojan",
	})
	if !errors.Is(err, ErrUnknownVenue) {
		t.Fatalf("creating a department in another business's venue answered %v, "+
			"want ErrUnknownVenue", err)
	}
	if f.departmentNamed(t, f.tenantID, "Trojan") || f.departmentNamed(t, f.foreignTenant, "Trojan") {
		t.Fatal("SECTION 4.5 FAILED: the department was created anyway")
	}

	// And editing another business's DEPARTMENT is refused as well.
	_, err = f.venues.SaveDepartment(ctx, DepartmentCommand{
		TenantID: f.tenantID, DepartmentID: f.foreignDepartment, ActorID: f.actorID,
		Name: "Stolen",
	})
	if !errors.Is(err, ErrUnknownDepartment) {
		t.Fatalf("editing another business's department answered %v, want ErrUnknownDepartment", err)
	}
	if got := f.nameOf(t, "departments", f.foreignTenant, f.foreignDepartment); got != "Kitchen" {
		t.Fatalf("SECTION 4.5 FAILED: the other business's department is now named %q", got)
	}

	// THE POSITIVE CONTROL, both halves.
	if _, err := f.venues.SaveDepartment(ctx, DepartmentCommand{
		TenantID: f.tenantID, ActorID: f.actorID, LocationID: f.locationID, Name: "Legit",
	}); err != nil {
		t.Fatalf("control: creating a department in our own venue failed: %v", err)
	}
	if _, err := f.venues.SaveDepartment(ctx, DepartmentCommand{
		TenantID: f.tenantID, DepartmentID: f.departmentID, ActorID: f.actorID, Name: "Kitchen 2",
	}); err != nil {
		t.Fatalf("control: editing our own department failed: %v", err)
	}
}

// TestVenues_TheScreenSeesOnlyItsOwnBusiness (§4.5, the read side). A section that
// listed another business's venues would be a disclosure, and it is the read the two
// write forms are populated from -- so a leak here would also offer a manager a venue
// they must not be able to edit.
func TestVenues_TheScreenSeesOnlyItsOwnBusiness(t *testing.T) {
	f := newVenueFixture(t)
	screen, err := f.venues.Screen(context.Background(), f.tenantID)
	if err != nil {
		t.Fatalf("Screen: %v", err)
	}
	if len(screen.Venues) == 0 {
		t.Fatal("our own business has no venues on its screen; every assertion below " +
			"would be vacuous")
	}
	for _, v := range screen.Venues {
		if v.ID == f.foreignVenue {
			t.Fatal("SECTION 4.5 FAILED: another business's venue is on this screen")
		}
	}
	for _, d := range screen.Departments {
		if d.ID == f.foreignDepartment {
			t.Fatal("SECTION 4.5 FAILED: another business's department is on this screen")
		}
	}
	// AND THE DEPARTMENT COUNT IS THIS BUSINESS'S. The subquery binds tenant_id to the
	// PARAMETER rather than to the outer row, which is the difference between a scoped
	// count and one that follows whatever row it lands on.
	var found bool
	for _, v := range screen.Venues {
		if v.ID == f.locationID {
			found = true
			if v.DepartmentCount != 1 {
				t.Errorf("the fixture venue reports %d department(s), want 1", v.DepartmentCount)
			}
		}
	}
	if !found {
		t.Error("the fixture venue is missing from its own screen")
	}
}

// --- the name bound ------------------------------------------------------------------

// TestVenues_TheNameBoundIsEnforcedBeforeTheColumn. locations.name and
// departments.name are unbounded `text` (migration 00002), and a security audit
// measured what that costs on employees.full_name: a 605-rune name served page one
// forever. These names are rendered into three other sections' filter dropdowns, so
// one absurd name degrades screens this section does not own.
func TestVenues_TheNameBoundIsEnforcedBeforeTheColumn(t *testing.T) {
	f := newVenueFixture(t)
	ctx := context.Background()

	before := f.venueCount(t, f.tenantID)
	if _, err := f.venues.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, ActorID: f.actorID,
		Name: strings.Repeat("a", MaxVenueNameRunes+1),
	}); !errors.Is(err, ErrVenueName) {
		t.Fatalf("a %d-rune venue name answered %v, want ErrVenueName", MaxVenueNameRunes+1, err)
	}
	if after := f.venueCount(t, f.tenantID); after != before {
		t.Error("a refused name was stored anyway -- and it must not be TRUNCATED and " +
			"stored either, which would save a name nobody typed")
	}
	if _, err := f.venues.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, ActorID: f.actorID, Name: "   ",
	}); !errors.Is(err, ErrVenueName) {
		t.Errorf("a blank venue name answered %v, want ErrVenueName", err)
	}
	if _, err := f.venues.SaveDepartment(ctx, DepartmentCommand{
		TenantID: f.tenantID, ActorID: f.actorID, LocationID: f.locationID,
		Name: strings.Repeat("b", MaxVenueNameRunes+1),
	}); !errors.Is(err, ErrDepartmentName) {
		t.Errorf("an over-long department name answered %v, want ErrDepartmentName", err)
	}

	// THE CONTROL: exactly the bound is accepted and stored WHOLE, in runes rather
	// than bytes -- Maltese venue names carry two-byte letters and must not be refused
	// at four fifths of the stated length.
	const hBar = string(rune(0x0127)) // 2 bytes in UTF-8
	name := strings.Repeat(hBar, MaxVenueNameRunes)
	v, err := f.venues.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, ActorID: f.actorID, Name: name,
	})
	if err != nil {
		t.Fatalf("control: a %d-rune (%d-byte) name was refused: %v",
			MaxVenueNameRunes, len(name), err)
	}
	if got := f.nameOf(t, "locations", f.tenantID, v.ID); got != name {
		t.Errorf("the name was stored as %d runes, want %d -- it must not be truncated",
			len([]rune(got)), MaxVenueNameRunes)
	}
}

// TestVenues_AnActorlessActIsRefused. audit_log.actor_id is polymorphic and nullable
// (00005: the column exists for system events), so a command with no actor would
// write a trail row nobody can be held to -- which is worse than no row, because it
// looks like a record.
func TestVenues_AnActorlessActIsRefused(t *testing.T) {
	f := newVenueFixture(t)
	ctx := context.Background()
	before := f.venueCount(t, f.tenantID)

	if _, err := f.venues.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, Name: "No actor",
	}); err == nil {
		t.Error("a venue was saved with no actor; the trail row would be unattributable")
	}
	if _, err := f.venues.SaveDepartment(ctx, DepartmentCommand{
		TenantID: f.tenantID, LocationID: f.locationID, Name: "No actor",
	}); err == nil {
		t.Error("a department was saved with no actor")
	}
	if after := f.venueCount(t, f.tenantID); after != before {
		t.Errorf("an actorless act still wrote %d venue(s)", after-before)
	}
}

// --- removal (user decision, 2026-08-08: option (b), unreferenced rows only) --------

// TestVenues_ADeletableVenueIsRemovedAndRecorded.
//
// 🔴 THE AUDIT ROW IS THE ONLY THING LEFT. An update's trail names an id somebody can
// go and look at; after a delete that id resolves to nothing, forever. So the
// configuration that produced every verdict at this venue — its address ranges, its
// coordinate, its shift — is recorded at the moment it stops existing.
func TestVenues_ADeletableVenueIsRemovedAndRecorded(t *testing.T) {
	f := newVenueFixture(t)
	ctx := context.Background()

	v, err := f.venues.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, ActorID: f.actorID, Name: "Doomed",
		StaticIPs: []netip.Prefix{netip.MustParsePrefix("10.9.9.0/24")},
		GPS:       &GPS{Lat: 35.9, Lng: 14.4},
		Shift:     &Shift{Start: 8 * time.Hour, End: 17 * time.Hour},
	})
	if err != nil {
		t.Fatalf("SaveVenue: %v", err)
	}
	// Nothing points at it, which is the whole precondition.
	refs, err := f.venues.VenueReferences(ctx, f.tenantID, v.ID)
	if err != nil {
		t.Fatalf("VenueReferences: %v", err)
	}
	if refs.InUse() {
		t.Fatalf("a freshly created venue reports references %+v", refs)
	}

	if err := f.venues.DeleteVenue(ctx, DeleteCommand{
		TenantID: f.tenantID, ID: v.ID, ActorID: f.actorID,
	}); err != nil {
		t.Fatalf("DeleteVenue: %v", err)
	}
	if _, err := f.venues.Venue(ctx, f.tenantID, v.ID); !errors.Is(err, ErrUnknownVenue) {
		t.Errorf("the venue is still readable after being removed: %v", err)
	}

	// TWO trail rows on that target: the create and the delete. The delete's detail
	// carries the row itself.
	n, detail := f.trailFor(t, f.tenantID, v.ID.String())
	if n != 2 {
		t.Errorf("the venue's trail holds %d row(s), want 2 (created, deleted)", n)
	}
	for _, want := range []string{`"name":"Doomed"`, "10.9.9.0/24", "35.900000,14.400000", "08:00-17:00"} {
		if !strings.Contains(detail, want) {
			t.Errorf("the removal's trail row does not carry %s: %s", want, detail)
		}
	}
	if !f.actionIs(t, f.tenantID, v.ID.String(), ActionLocationDeleted) {
		t.Errorf("the removal did not use the %q action", ActionLocationDeleted)
	}
}

// TestVenues_AVenueInUseIsRefusedByItsForeignKeys.
//
// 🔴 THE COUNT IS THE SCREEN'S AND THE KEYS ARE THE GUARD, and this measures the
// SECOND one — the one that holds when the first is stale.
func TestVenues_AVenueInUseIsRefusedByItsForeignKeys(t *testing.T) {
	f := newVenueFixture(t)
	ctx := context.Background()

	// The fixture venue has a department and (below) an employee.
	refs, err := f.venues.VenueReferences(ctx, f.tenantID, f.locationID)
	if err != nil {
		t.Fatalf("VenueReferences: %v", err)
	}
	if refs.Departments != 1 {
		t.Fatalf("the fixture venue reports %d department(s), want 1", refs.Departments)
	}
	if !refs.InUse() {
		t.Fatal("the fixture venue reports no references, so this test proves nothing")
	}

	if err := f.venues.DeleteVenue(ctx, DeleteCommand{
		TenantID: f.tenantID, ID: f.locationID, ActorID: f.actorID,
	}); !errors.Is(err, ErrVenueInUse) {
		t.Fatalf("removing a venue in use answered %v, want ErrVenueInUse", err)
	}
	// AND THE ROW SURVIVED — a refusal that half-happened would be worse than either.
	if got := f.nameOf(t, "locations", f.tenantID, f.locationID); got != "St Julians" {
		t.Errorf("the venue is now %q", got)
	}
	// AND NO TRAIL ROW CLAIMS IT WAS REMOVED.
	if f.actionIs(t, f.tenantID, f.locationID.String(), ActionLocationDeleted) {
		t.Error("a refused removal wrote a `location.deleted` audit row")
	}
}

// TestVenues_TheRaceBetweenCountingAndDeletingIsRefusedNotCrashed.
//
// 🔴 THIS IS THE RACE ITSELF, DRIVEN RATHER THAN REASONED ABOUT. The panel counts
// zero references, and BEFORE the delete lands another manager hires somebody into the
// venue. What must come back is ErrVenueInUse — a sentence — and not a raw driver
// error the handler can only render as a 500.
func TestVenues_TheRaceBetweenCountingAndDeletingIsRefusedNotCrashed(t *testing.T) {
	f := newVenueFixture(t)
	ctx := context.Background()

	empty, err := f.venues.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, ActorID: f.actorID, Name: "Empty for now",
	})
	if err != nil {
		t.Fatalf("SaveVenue: %v", err)
	}
	// STEP 1: the screen counts, and finds nothing. The control is offered.
	refs, err := f.venues.VenueReferences(ctx, f.tenantID, empty.ID)
	if err != nil {
		t.Fatalf("VenueReferences: %v", err)
	}
	if refs.InUse() {
		t.Fatalf("the new venue already has references %+v", refs)
	}

	// STEP 2: SOMEBODY IS HIRED INTO IT. This is the whole point of the test — the
	// reference appears between the count and the delete.
	if err := f.data.WithTenant(ctx, f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status)
			 VALUES ($1, $2, $3, 'Late Arrival', 'active')`,
			uuid.New(), f.tenantID, empty.ID)
		return e
	}); err != nil {
		t.Fatalf("seed the race: %v", err)
	}

	// STEP 3: the press lands on a venue that is no longer empty.
	err = f.venues.DeleteVenue(ctx, DeleteCommand{
		TenantID: f.tenantID, ID: empty.ID, ActorID: f.actorID,
	})
	if !errors.Is(err, ErrVenueInUse) {
		t.Fatalf("the race answered %v, want ErrVenueInUse. A raw 23503 reaches the "+
			"manager as 'the panel is unavailable' for a request that was fine.", err)
	}
	// The venue survived, which is what ON DELETE RESTRICT is for.
	if got := f.nameOf(t, "locations", f.tenantID, empty.ID); got != "Empty for now" {
		t.Errorf("the venue is now %q", got)
	}
}

// TestVenues_ADepartmentInUseIsRefusedToo. Two keys rather than four, same rule — the
// symmetry a manager reads off the screen has to be real underneath it.
func TestVenues_ADepartmentInUseIsRefusedToo(t *testing.T) {
	f := newVenueFixture(t)
	ctx := context.Background()

	// An empty department goes.
	d, err := f.venues.SaveDepartment(ctx, DepartmentCommand{
		TenantID: f.tenantID, ActorID: f.actorID, LocationID: f.locationID, Name: "Pastry",
	})
	if err != nil {
		t.Fatalf("SaveDepartment: %v", err)
	}
	if refs, err := f.venues.DepartmentReferences(ctx, f.tenantID, d.ID); err != nil || refs.InUse() {
		t.Fatalf("a fresh department reports %+v, %v", refs, err)
	}
	if err := f.venues.DeleteDepartment(ctx, DeleteCommand{
		TenantID: f.tenantID, ID: d.ID, ActorID: f.actorID,
	}); err != nil {
		t.Fatalf("DeleteDepartment: %v", err)
	}
	if _, err := f.venues.Department(ctx, f.tenantID, d.ID); !errors.Is(err, ErrUnknownDepartment) {
		t.Errorf("the department is still readable: %v", err)
	}

	// One with an employee in it stays.
	inUse, err := f.venues.SaveDepartment(ctx, DepartmentCommand{
		TenantID: f.tenantID, ActorID: f.actorID, LocationID: f.locationID, Name: "Grill",
	})
	if err != nil {
		t.Fatalf("SaveDepartment: %v", err)
	}
	if err := f.data.WithTenant(ctx, f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, department_id, full_name, status)
			 VALUES ($1, $2, $3, $4, 'Grill Cook', 'active')`,
			uuid.New(), f.tenantID, f.locationID, inUse.ID)
		return e
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	refs, err := f.venues.DepartmentReferences(ctx, f.tenantID, inUse.ID)
	if err != nil {
		t.Fatalf("DepartmentReferences: %v", err)
	}
	if refs.Employees != 1 {
		t.Errorf("the department reports %d employee(s), want 1", refs.Employees)
	}
	if err := f.venues.DeleteDepartment(ctx, DeleteCommand{
		TenantID: f.tenantID, ID: inUse.ID, ActorID: f.actorID,
	}); !errors.Is(err, ErrDepartmentInUse) {
		t.Errorf("removing a department in use answered %v, want ErrDepartmentInUse", err)
	}
}

// TestVenues_DepartmentReferencesDoesNotCountTheDepartmentlessEntireBusiness.
//
// 🔴 A CAST IN THE QUERY IS WHAT MAKES THIS TRUE, AND IT WAS ONCE ABSENT.
// employees.department_id is NULLABLE, so sqlc inferred @id as *uuid.UUID and a nil
// would have counted EVERY employee not in a department — answering "in use" for a
// department nothing points at. The fixture business has exactly such an employee.
func TestVenues_DepartmentReferencesDoesNotCountTheDepartmentlessEntireBusiness(t *testing.T) {
	f := newVenueFixture(t)
	ctx := context.Background()

	// Somebody with NO department, in this business.
	if err := f.data.WithTenant(ctx, f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status)
			 VALUES ($1, $2, $3, 'No Department', 'active')`,
			uuid.New(), f.tenantID, f.locationID)
		return e
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	d, err := f.venues.SaveDepartment(ctx, DepartmentCommand{
		TenantID: f.tenantID, ActorID: f.actorID, LocationID: f.locationID, Name: "Empty one",
	})
	if err != nil {
		t.Fatalf("SaveDepartment: %v", err)
	}
	refs, err := f.venues.DepartmentReferences(ctx, f.tenantID, d.ID)
	if err != nil {
		t.Fatalf("DepartmentReferences: %v", err)
	}
	if refs.InUse() {
		t.Fatalf("an empty department reports %+v — the query is counting rows whose "+
			"department_id IS NULL, which is every unassigned employee in the business", refs)
	}
}

// TestVenues_ARemovalWhoseTrailFailsDoesNotHappen. The strongest form of the
// share-one-transaction property: a DELETE that lost its audit row leaves NOTHING —
// no row to inspect and no record that there ever was one.
func TestVenues_ARemovalWhoseTrailFailsDoesNotHappen(t *testing.T) {
	f := newVenueFixture(t)
	ctx := context.Background()

	v, err := f.venues.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, ActorID: f.actorID, Name: "Survives",
	})
	if err != nil {
		t.Fatalf("SaveVenue: %v", err)
	}
	broken, err := NewVenues(f.data, failingTrail{err: errors.New("trail is down")},
		slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewVenues: %v", err)
	}
	if err := broken.DeleteVenue(ctx, DeleteCommand{
		TenantID: f.tenantID, ID: v.ID, ActorID: f.actorID,
	}); err == nil {
		t.Fatal("a removal with a broken trail reported success")
	}
	if got := f.nameOf(t, "locations", f.tenantID, v.ID); got != "Survives" {
		t.Errorf("the venue was removed even though its audit row failed (now %q)", got)
	}
}

// TestVenues_ARemovalCannotReachAnotherBusiness (§4.5). The explicit tenant predicate
// plus RLS mean a foreign id matches no row — and the sentence the handler builds from
// ErrUnknownVenue is TRUE here, unlike the capped-list case that was repaired earlier.
func TestVenues_ARemovalCannotReachAnotherBusiness(t *testing.T) {
	f := newVenueFixture(t)
	ctx := context.Background()

	// The other business's venue HAS references, so even a leak would be refused by
	// the keys — which is why a second, unreferenced foreign venue is created here.
	var foreignEmpty uuid.UUID
	if err := f.data.WithTenant(ctx, f.foreignTenant, func(ctx context.Context, tx pgx.Tx) error {
		foreignEmpty = uuid.New()
		_, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'Foreign Empty')`,
			foreignEmpty, f.foreignTenant)
		return e
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := f.venues.DeleteVenue(ctx, DeleteCommand{
		TenantID: f.tenantID, ID: foreignEmpty, ActorID: f.actorID,
	}); !errors.Is(err, ErrUnknownVenue) {
		t.Fatalf("removing another business's venue answered %v, want ErrUnknownVenue", err)
	}
	if got := f.nameOf(t, "locations", f.foreignTenant, foreignEmpty); got != "Foreign Empty" {
		t.Fatalf("SECTION 4.5 FAILED: the other business's venue is gone")
	}

	// AND THE COUNTS ARE SCOPED TOO — a foreign id must not leak a number.
	if refs, err := f.venues.VenueReferences(ctx, f.tenantID, f.foreignVenue); err != nil {
		t.Errorf("VenueReferences on a foreign id answered %v", err)
	} else if refs.InUse() {
		t.Errorf("VenueReferences reported %+v for another business's venue", refs)
	}
}

// --- the verified removal acknowledgement (user decision, 2026-08-09, reading C') ---

// TestVenues_ConfirmRemovalFindsOnlyTheActorsOwnRecentRemoval.
//
// 🔴 THIS IS THE QUERY THAT LETS THE PANEL SAY "REMOVED" WITHOUT LYING, and everything
// worth testing about it is a NEGATIVE: the four inputs that must all come back empty.
// A fake would agree with any of them.
func TestVenues_ConfirmRemovalFindsOnlyTheActorsOwnRecentRemoval(t *testing.T) {
	f := newVenueFixture(t)
	ctx := context.Background()

	// A real removal by THIS admin.
	v, err := f.venues.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, ActorID: f.actorID, Name: "Doomed Sliema",
	})
	if err != nil {
		t.Fatalf("SaveVenue: %v", err)
	}
	if err := f.venues.DeleteVenue(ctx, DeleteCommand{
		TenantID: f.tenantID, ID: v.ID, ActorID: f.actorID,
	}); err != nil {
		t.Fatalf("DeleteVenue: %v", err)
	}

	// THE POSITIVE CONTROL FIRST: without it every refusal below is vacuous.
	got, err := f.venues.ConfirmRemoval(ctx, f.tenantID, f.actorID,
		ActionLocationDeleted, v.ID.String())
	if err != nil {
		t.Fatalf("ConfirmRemoval: %v", err)
	}
	if !got.Found {
		t.Fatal("the actor's own removal, a moment old, was not found")
	}
	// 🔴 AND THE NAME COMES OUT OF THE TRAIL. It is the only surviving copy — the row
	// is gone — and it is what the heading prints instead of anything from the URL.
	if got.Name != "Doomed Sliema" {
		t.Errorf("the receipt names %q, want the removed venue's name from the trail", got.Name)
	}

	// 🔴 THE FOUR INPUTS THAT MUST ALL BE EMPTY. Each is a different way of asking the
	// panel a question it must not answer.
	otherAdmin := uuid.New()
	tests := []struct {
		name     string
		tenantID uuid.UUID
		actorID  uuid.UUID
		action   string
		target   string
	}{
		{"another business asking about our removal",
			f.foreignTenant, f.actorID, ActionLocationDeleted, v.ID.String()},
		{"a COLLEAGUE in this business asking about it",
			f.tenantID, otherAdmin, ActionLocationDeleted, v.ID.String()},
		{"a uuid that never existed",
			f.tenantID, f.actorID, ActionLocationDeleted, uuid.NewString()},
		{"the right row but the wrong action",
			f.tenantID, f.actorID, ActionDepartmentDeleted, v.ID.String()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := f.venues.ConfirmRemoval(ctx, tc.tenantID, tc.actorID, tc.action, tc.target)
			if err != nil {
				t.Fatalf("ConfirmRemoval: %v", err)
			}
			if got.Found {
				t.Errorf("the trail answered YES (%q). This value decides whether the "+
					"panel prints 'removed', so a YES here is a claim on behalf of "+
					"somebody who did not do it — or an oracle for another business.",
					got.Name)
			}
		})
	}
}

// TestVenues_ConfirmRemovalDoesNotMatchSystemOrTargetlessRows.
//
// 🔴 audit_log.actor_id AND target ARE BOTH NULLABLE (00005: the column is polymorphic
// and system events carry NULL), so without explicit casts sqlc emits pointer
// parameters and a nil would match every SYSTEM row. That is the same defect
// CountDepartmentReferences hit on a nullable department_id. This drives the shape the
// casts exist to make impossible.
func TestVenues_ConfirmRemovalDoesNotMatchSystemOrTargetlessRows(t *testing.T) {
	f := newVenueFixture(t)
	ctx := context.Background()

	// A SYSTEM event: actor_id NULL, and an action that looks like a removal.
	if _, err := f.trail.Record(ctx, audit.Event{
		TenantID: f.tenantID,
		ActorID:  nil,
		Action:   ActionLocationDeleted,
		Target:   "",
		Detail:   map[string]string{"name": "System Ghost"},
	}); err != nil {
		t.Fatalf("seed a system event: %v", err)
	}

	// The zero actor and the empty target must both find nothing — and the domain
	// refuses them before the query, which is asserted rather than assumed.
	for _, tc := range []struct {
		name    string
		actorID uuid.UUID
		target  string
	}{
		{"a zero actor", uuid.Nil, "anything"},
		{"an empty target", f.actorID, ""},
		{"a zero actor and an empty target", uuid.Nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := f.venues.ConfirmRemoval(ctx, f.tenantID, tc.actorID,
				ActionLocationDeleted, tc.target)
			if err != nil {
				t.Fatalf("ConfirmRemoval: %v", err)
			}
			if got.Found {
				t.Errorf("matched a system/targetless row (%q); one nil parameter would "+
					"make every SYSTEM event look like this manager's own removal", got.Name)
			}
		})
	}
}

// TestVenues_ConfirmRemovalWorksForDepartmentsToo — the two gates follow one rule, and
// the symmetry a manager reads off the screen has to be real underneath it.
func TestVenues_ConfirmRemovalWorksForDepartmentsToo(t *testing.T) {
	f := newVenueFixture(t)
	ctx := context.Background()

	d, err := f.venues.SaveDepartment(ctx, DepartmentCommand{
		TenantID: f.tenantID, ActorID: f.actorID, LocationID: f.locationID, Name: "Pastry",
	})
	if err != nil {
		t.Fatalf("SaveDepartment: %v", err)
	}
	if err := f.venues.DeleteDepartment(ctx, DeleteCommand{
		TenantID: f.tenantID, ID: d.ID, ActorID: f.actorID,
	}); err != nil {
		t.Fatalf("DeleteDepartment: %v", err)
	}
	got, err := f.venues.ConfirmRemoval(ctx, f.tenantID, f.actorID,
		ActionDepartmentDeleted, d.ID.String())
	if err != nil {
		t.Fatalf("ConfirmRemoval: %v", err)
	}
	if !got.Found || got.Name != "Pastry" {
		t.Errorf("a department removal is not confirmable: %+v", got)
	}
}

// TestVenues_TheRemovalWindowIsTheDATABASES_Clock.
//
// ⚠️ WHAT THIS TEST DOES AND DOES NOT COVER, STATED BECAUSE THE GAP IS REAL. It proves
// the window is evaluated against the DATABASE's clock and that nothing a caller sends
// reaches it — removalWindow is the only input and it is a compile-time constant, so
// no request can widen it.
//
// 🔴 IT DOES NOT PROVE THAT A 31-SECOND-OLD ROW IS REFUSED, and that is an UNTESTED
// LIMIT rather than a covered case. `at` is written by the DATABASE (DEFAULT now()),
// RecordAuditEvent does not accept it as a parameter, and audit_log is append-only for
// tappa_app — SELECT and INSERT only, with a trigger that stops even tappa_owner. So
// there is no way from inside this suite to plant a row with an old stamp, and the
// alternatives are all worse than the gap: sleeping 31 seconds would add that to every
// run, and widening the grant to make the test possible would delete the append-only
// guarantee the acknowledgement rests on.
//
// WHAT IS TESTED INSTEAD is the direction that can be: a window of ZERO finds nothing
// even for a row written a moment ago, which is only true if the comparison is really
// against `at` and really evaluated per-query.
func TestVenues_TheRemovalWindowIsTheDATABASES_Clock(t *testing.T) {
	f := newVenueFixture(t)
	ctx := context.Background()

	v, err := f.venues.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, ActorID: f.actorID, Name: "Window probe",
	})
	if err != nil {
		t.Fatalf("SaveVenue: %v", err)
	}
	if err := f.venues.DeleteVenue(ctx, DeleteCommand{
		TenantID: f.tenantID, ID: v.ID, ActorID: f.actorID,
	}); err != nil {
		t.Fatalf("DeleteVenue: %v", err)
	}

	// The shipped window finds it.
	if got, err := f.venues.ConfirmRemoval(ctx, f.tenantID, f.actorID,
		ActionLocationDeleted, v.ID.String()); err != nil || !got.Found {
		t.Fatalf("the fresh removal was not found inside the window: %+v, %v", got, err)
	}

	// A ZERO window finds nothing — the row's `at` is already in the past by the time
	// the next statement runs, so this only passes if the bound is really applied.
	var found bool
	if err := f.data.WithTenant(ctx, f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).ConfirmRecentRemoval(ctx, store.ConfirmRecentRemovalParams{
			TenantID:      f.tenantID,
			ActorID:       f.actorID,
			Action:        ActionLocationDeleted,
			Target:        v.ID.String(),
			WindowSeconds: 0,
		})
		switch {
		case errors.Is(e, pgx.ErrNoRows):
			return nil
		case e != nil:
			return e
		}
		found = true
		return nil
	}); err != nil {
		t.Fatalf("zero-window probe: %v", err)
	}
	if found {
		t.Error("a zero-second window still matched a row; the `at > now() - interval` " +
			"bound is not being applied, so the window is decorative")
	}

	// AND THE CONSTANT IS WHAT THE QUERY IS GIVEN — one source, not two.
	if removalWindow != 30*time.Second {
		t.Errorf("removalWindow is %v; the card and the query comment both say 30s", removalWindow)
	}
}

// TestVenues_ARemovalTrailRecordsABSENCESAndTheCreationTIME.
//
// 🔴 AFTER A DELETION THE TRAIL ROW IS THE ONLY SURVIVING RECORD, so an omitted key is
// not a saved byte — it is a question nobody can ever answer again. A security audit
// measured `static_ips` present on 21 of 58 location.deleted rows and ABSENT on the
// other 37, because omitempty dropped the empty slice: "it had no IP evidence" and
// "that version did not record IP evidence" became the same trail. §4.6 asks the
// opposite.
//
// AND created_at WAS NOT RECORDED AT ALL. `locations` has no updated_at, so it was the
// row's only timestamp and the deletion destroyed it permanently.
func TestVenues_ARemovalTrailRecordsABSENCESAndTheCreationTIME(t *testing.T) {
	f := newVenueFixture(t)
	ctx := context.Background()

	// A venue with NOTHING configured — no ranges, no coordinate, no shift, no SSID.
	// This is the shape whose absences used to vanish from the trail.
	bare, err := f.venues.SaveVenue(ctx, VenueCommand{
		TenantID: f.tenantID, ActorID: f.actorID, Name: "Bare Venue",
	})
	if err != nil {
		t.Fatalf("SaveVenue: %v", err)
	}
	if err := f.venues.DeleteVenue(ctx, DeleteCommand{
		TenantID: f.tenantID, ID: bare.ID, ActorID: f.actorID,
	}); err != nil {
		t.Fatalf("DeleteVenue: %v", err)
	}

	// 🔴 READ THE KEYS OUT OF THE COLUMN, not out of the Go struct — what is under
	// test is what jsonb actually holds.
	var keys []string
	var createdAt *string
	if err := f.data.WithTenant(ctx, f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if e := tx.QueryRow(ctx,
			`SELECT array_agg(k ORDER BY k) FROM audit_log, jsonb_object_keys(detail) k
			  WHERE tenant_id = $1 AND target = $2 AND action = $3`,
			f.tenantID, bare.ID.String(), ActionLocationDeleted).Scan(&keys); e != nil {
			return e
		}
		return tx.QueryRow(ctx,
			`SELECT detail->>'created_at' FROM audit_log
			  WHERE tenant_id = $1 AND target = $2 AND action = $3`,
			f.tenantID, bare.ID.String(), ActionLocationDeleted).Scan(&createdAt)
	}); err != nil {
		t.Fatalf("read the trail: %v", err)
	}

	// EVERY evidence key must be present even though every value is empty.
	present := map[string]bool{}
	for _, k := range keys {
		present[k] = true
	}
	for _, want := range []string{"name", "static_ips", "gps", "shift", "wifi_ssid", "created_at"} {
		if !present[want] {
			t.Errorf("the removal trail has no %q key. An absent key cannot be told "+
				"apart from an absent VALUE, and this row is the only surviving record "+
				"of the venue. Keys found: %v", want, keys)
		}
	}

	// AND THE EMPTY LIST IS RECORDED AS AN EMPTY LIST, not as null.
	_, detail := f.trailFor(t, f.tenantID, bare.ID.String())
	if !strings.Contains(detail, `"static_ips":[]`) {
		t.Errorf("an unconfigured venue's ranges are not recorded as an empty list: %s", detail)
	}
	if !strings.Contains(detail, `"gps":""`) || !strings.Contains(detail, `"shift":""`) {
		t.Errorf("an absent coordinate or shift is not recorded as an absence: %s", detail)
	}

	// 🔴 created_at IS CHECKED BY ITS VALUE, NOT BY ITS PRESENCE — an audit showed why.
	// Deleting `CreatedAt: row.CreatedAt` from DeleteVenue leaves the zero time, and
	// `0001-01-01T00:00:00Z` satisfies BOTH "the key is there" AND time.Parse, so the
	// whole package stayed green while the field it exists for was gone. A net that
	// accepts the zero value of the thing it guards is not guarding it.
	if createdAt == nil || *createdAt == "" {
		t.Fatal("the removal trail carries no created_at; the venue's registration time " +
			"is destroyed with the row and exists nowhere else")
	}
	got, err := time.Parse(time.RFC3339Nano, *createdAt)
	if err != nil {
		t.Fatalf("created_at %q is not a parseable timestamp: %v", *createdAt, err)
	}
	if got.IsZero() {
		t.Fatal("created_at is the ZERO time; the field is being written but not filled")
	}
	// IT MUST EQUAL WHAT THE ROW ACTUALLY CARRIED. The venue was created moments ago in
	// this test, so the trail's copy and the returned row's copy are the same instant.
	if !got.Equal(bare.CreatedAt) {
		t.Errorf("the trail records created_at %s but the venue carried %s; the trail is "+
			"describing a row that never existed", got, bare.CreatedAt)
	}

	// A DEPARTMENT'S REMOVAL FOLLOWS THE SAME RULE.
	d, err := f.venues.SaveDepartment(ctx, DepartmentCommand{
		TenantID: f.tenantID, ActorID: f.actorID, LocationID: f.locationID, Name: "Bare Dept",
	})
	if err != nil {
		t.Fatalf("SaveDepartment: %v", err)
	}
	if err := f.venues.DeleteDepartment(ctx, DeleteCommand{
		TenantID: f.tenantID, ID: d.ID, ActorID: f.actorID,
	}); err != nil {
		t.Fatalf("DeleteDepartment: %v", err)
	}
	_, depDetail := f.trailFor(t, f.tenantID, d.ID.String())
	for _, want := range []string{`"shift":""`, `"static_ips":[]`, `"created_at":`} {
		if !strings.Contains(depDetail, want) {
			t.Errorf("a department's removal trail is missing %s: %s", want, depDetail)
		}
	}
}
