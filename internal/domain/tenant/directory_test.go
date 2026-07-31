package tenant

// Directory tests against a REAL Postgres. The behaviour under test is mostly
// ABOUT the database — that a location id belonging to another tenant returns
// no row under RLS plus the explicit tenant predicate, and that the employee's
// name survives that miss — so a fake would only re-state the expectation.
//
// Fixtures are NOT cleaned up (tappa_app has REVOKE DELETE on employees).
// Fresh random ids keep runs from colliding; `make db-reset` clears the dev
// database.

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
)

type fixture struct {
	dir        *Directory
	data       *db.DB
	tenantID   uuid.UUID
	locationID uuid.UUID
	employeeID uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping tenant directory tests (real Postgres required, Q04)")
	}
	data, err := db.New(context.Background(), &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)

	dir, err := NewDirectory(data)
	if err != nil {
		t.Fatalf("NewDirectory: %v", err)
	}
	f := &fixture{
		dir: dir, data: data,
		tenantID: uuid.New(), locationID: uuid.New(), employeeID: uuid.New(),
	}
	f.seed(t, f.tenantID, f.locationID, f.employeeID, "Maria Borg", "St Julians")
	return f
}

// seed commits one tenant + location + employee. Called again with fresh ids to
// build the "another tenant" side of the isolation cases.
func (f *fixture) seed(t *testing.T, tenantID, locationID, employeeID uuid.UUID, name, venue string) {
	t.Helper()
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Kebab Factory Ltd', $2, 'restaurant', 'multi')
			 ON CONFLICT (id) DO NOTHING`,
			tenantID, "VAT-"+tenantID.String()[:8]); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name, gps_lat, gps_lng)
			 VALUES ($1, $2, $3, 35.918, 14.489)`,
			locationID, tenantID, venue); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at)
			 VALUES ($1, $2, $3, $4, 'active', now())`,
			employeeID, tenantID, locationID, name)
		return e
	})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
}

// TestTapPage_ReturnsTheGreetingAndTheTappedVenue is the ordinary path.
func TestTapPage_ReturnsTheGreetingAndTheTappedVenue(t *testing.T) {
	f := newFixture(t)

	facts, err := f.dir.TapPage(context.Background(), f.tenantID, f.employeeID, f.locationID)
	if err != nil {
		t.Fatalf("TapPage: %v", err)
	}
	if facts.EmployeeName != "Maria Borg" {
		t.Fatalf("EmployeeName = %q, want %q", facts.EmployeeName, "Maria Borg")
	}
	if facts.LocationName != "St Julians" {
		t.Fatalf("LocationName = %q, want %q", facts.LocationName, "St Julians")
	}
}

// TestTapPage_TheVenueIsTheTappedOneNotTheProfileOne. §5 is explicit that a
// chain moves people between branches, so the screen must name the plaque in
// front of them. Here the employee's profile location and the tapped location
// are DIFFERENT rows of the same tenant, and the tapped one must win.
func TestTapPage_TheVenueIsTheTappedOneNotTheProfileOne(t *testing.T) {
	f := newFixture(t)
	otherBranch := uuid.New()
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name, gps_lat, gps_lng)
			 VALUES ($1, $2, 'Sliema', 35.911, 14.502)`, otherBranch, f.tenantID)
		return e
	})
	if err != nil {
		t.Fatalf("second branch: %v", err)
	}

	facts, err := f.dir.TapPage(context.Background(), f.tenantID, f.employeeID, otherBranch)
	if err != nil {
		t.Fatalf("TapPage: %v", err)
	}
	if facts.LocationName != "Sliema" {
		t.Fatalf("LocationName = %q, want %q — the TAPPED venue, not the employee's profile location",
			facts.LocationName, "Sliema")
	}
	if facts.EmployeeName != "Maria Borg" {
		t.Fatalf("EmployeeName = %q", facts.EmployeeName)
	}
}

// TestTapPage_ForeignLocationKeepsTheNameAndDropsTheVenue is the contract
// ErrForeignLocation makes: a POPULATED result alongside the error, because the
// caller still has to serve a page and cannot do it from a zero value — the same
// shape session.Verify uses for ErrRevoked and invite.Lookup for ErrCodeUsed.
//
// It is also the disclosure boundary: another tenant's venue NAME must not
// travel, and the assertion is that LocationName is empty rather than "Msida".
func TestTapPage_ForeignLocationKeepsTheNameAndDropsTheVenue(t *testing.T) {
	f := newFixture(t)
	otherTenant, otherLocation, otherEmployee := uuid.New(), uuid.New(), uuid.New()
	f.seed(t, otherTenant, otherLocation, otherEmployee, "Someone Else", "Msida")

	facts, err := f.dir.TapPage(context.Background(), f.tenantID, f.employeeID, otherLocation)

	if !errors.Is(err, ErrForeignLocation) {
		t.Fatalf("err = %v, want ErrForeignLocation", err)
	}
	if facts.EmployeeName != "Maria Borg" {
		t.Fatalf("EmployeeName = %q, want it POPULATED alongside the error: the caller must still render a page", facts.EmployeeName)
	}
	if facts.LocationName != "" {
		t.Fatalf("LocationName = %q, want empty — another tenant's venue name must not reach this employee's screen", facts.LocationName)
	}

	// POSITIVE CONTROL: the very same location id DOES resolve inside its own
	// tenant. Without this, "no row" would also be true of a typo'd id and the
	// test would prove nothing about isolation.
	own, err := f.dir.TapPage(context.Background(), otherTenant, otherEmployee, otherLocation)
	if err != nil {
		t.Fatalf("the same location failed inside its OWN tenant (%v): the miss above proves nothing", err)
	}
	if own.LocationName != "Msida" {
		t.Fatalf("LocationName = %q inside its own tenant, want %q", own.LocationName, "Msida")
	}
}

// TestTapPage_NilLocationIsForeignNotAnError: an employee whose tap resolved no
// location at all takes the same branch — a name, no venue, and a caller that
// can still render.
func TestTapPage_NilLocationIsForeignNotAnError(t *testing.T) {
	f := newFixture(t)

	facts, err := f.dir.TapPage(context.Background(), f.tenantID, f.employeeID, uuid.Nil)

	if !errors.Is(err, ErrForeignLocation) {
		t.Fatalf("err = %v, want ErrForeignLocation", err)
	}
	if facts.EmployeeName != "Maria Borg" || facts.LocationName != "" {
		t.Fatalf("facts = %+v", facts)
	}
}

// TestTapPage_UnknownEmployeeIsARealError. A missing employee is NOT the
// foreign-location case: there is no page to render and no name to greet, so it
// must surface rather than degrade into a half-filled screen.
func TestTapPage_UnknownEmployeeIsARealError(t *testing.T) {
	f := newFixture(t)

	facts, err := f.dir.TapPage(context.Background(), f.tenantID, uuid.New(), f.locationID)

	if err == nil {
		t.Fatal("an unknown employee resolved successfully")
	}
	if errors.Is(err, ErrForeignLocation) {
		t.Fatal("an unknown employee was reported as a foreign location: the caller would render a nameless page instead of failing")
	}
	if facts != (TapPageFacts{}) {
		t.Fatalf("facts = %+v, want zero on a real failure", facts)
	}
}

// TestTapPage_CrossTenantEmployeeDoesNotResolve is the §4.5 case from the other
// direction: a real employee id read under the WRONG tenant. RLS plus the
// explicit tenant_id predicate must return nothing.
func TestTapPage_CrossTenantEmployeeDoesNotResolve(t *testing.T) {
	f := newFixture(t)
	otherTenant, otherLocation, otherEmployee := uuid.New(), uuid.New(), uuid.New()
	f.seed(t, otherTenant, otherLocation, otherEmployee, "Someone Else", "Msida")

	if _, err := f.dir.TapPage(context.Background(), f.tenantID, otherEmployee, f.locationID); err == nil {
		t.Fatal("another tenant's employee resolved inside this tenant (§4.5)")
	}

	// POSITIVE CONTROL: that employee resolves in their own tenant.
	if _, err := f.dir.TapPage(context.Background(), otherTenant, otherEmployee, otherLocation); err != nil {
		t.Fatalf("the same employee failed in their OWN tenant (%v): the refusal above proves nothing", err)
	}
}

func TestTapPage_RequiresTenantAndEmployee(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.dir.TapPage(ctx, uuid.Nil, f.employeeID, f.locationID); err == nil {
		t.Fatal("a nil tenant was accepted: the read would run with no tenant context at all")
	}
	if _, err := f.dir.TapPage(ctx, f.tenantID, uuid.Nil, f.locationID); err == nil {
		t.Fatal("a nil employee was accepted")
	}
}

func TestNewDirectory_RefusesANilDatabase(t *testing.T) {
	if _, err := NewDirectory(nil); err == nil {
		t.Fatal("a nil database was accepted: the failure would surface as a nil dereference on the first tap")
	}
}
