package tenant

// staff_db_test.go -- M6-05 phase B's write path against REAL Postgres.
//
// 🔴 A FAKE CANNOT TEST ANY OF THIS. What is under test is exactly the part a mock
// would agree with unconditionally: that a foreign id writes nothing (§4.5, RLS plus
// the explicit predicate), that the audit row and the employee change SHARE A
// TRANSACTION (§4.6, measured by breaking each side in turn and counting rows), that
// a second press writes neither a second stamp nor a second trail entry, and that two
// managers pressing at once produce exactly one act.
//
// FIXTURES ARE NOT CLEANED UP: tappa_app has REVOKE DELETE on employees (00003) and
// audit_log is append-only (00005). Fresh random ids keep runs from colliding, and
// `make db-reset` clears the development database.

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
)

// staffFixture is one tenant with two venues, one department and one employee, plus
// a SECOND tenant to attack it from.
type staffFixture struct {
	data  *db.DB
	staff *Staff
	trail *audit.Recorder

	tenantID     uuid.UUID
	locationID   uuid.UUID
	otherVenue   uuid.UUID
	departmentID uuid.UUID
	employeeID   uuid.UUID
	actorID      uuid.UUID

	// The other tenant, with its own venue and its own employee.
	foreignTenant   uuid.UUID
	foreignVenue    uuid.UUID
	foreignEmployee uuid.UUID
}

func newStaffFixture(t *testing.T) *staffFixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping employee write tests (real Postgres required)")
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
	staff, err := NewStaff(data, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewStaff: %v", err)
	}
	f := &staffFixture{
		data: data, staff: staff, trail: trail,
		tenantID: uuid.New(), locationID: uuid.New(), otherVenue: uuid.New(),
		departmentID: uuid.New(), employeeID: uuid.New(), actorID: uuid.New(),
		foreignTenant: uuid.New(), foreignVenue: uuid.New(), foreignEmployee: uuid.New(),
	}
	f.seedTenant(t, f.tenantID, f.locationID, f.otherVenue, f.departmentID, f.employeeID)
	f.seedTenant(t, f.foreignTenant, f.foreignVenue, uuid.New(), uuid.New(), f.foreignEmployee)
	return f
}

func (f *staffFixture) seedTenant(t *testing.T, tenantID, venue, otherVenue, department, employee uuid.UUID) {
	t.Helper()
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Kebab Factory Ltd', $2, 'restaurant', 'multi')`,
			tenantID, "VAT-"+tenantID.String()[:8]); e != nil {
			return e
		}
		for _, v := range []struct {
			id   uuid.UUID
			name string
		}{{venue, "St Julians"}, {otherVenue, "Sliema"}} {
			if _, e := tx.Exec(ctx,
				`INSERT INTO locations (id, tenant_id, name, gps_lat, gps_lng)
				 VALUES ($1, $2, $3, 35.918, 14.489)`, v.id, tenantID, v.name); e != nil {
				return e
			}
		}
		// A department belongs to a LOCATION as well as to a tenant -- migration 00002
		// -- and it is attached to the SECOND venue on purpose: the move test sends
		// somebody to that venue and that department together, the ordinary shape.
		if _, e := tx.Exec(ctx,
			`INSERT INTO departments (id, tenant_id, location_id, name)
			 VALUES ($1, $2, $3, 'Kitchen')`,
			department, tenantID, otherVenue); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at)
			 VALUES ($1, $2, $3, 'Maria Borg', 'active', now())`,
			employee, tenantID, venue)
		return e
	})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
}

// statusOf reads one employee's lifecycle state IN ITS OWN TENANT's context, so RLS
// scopes the read exactly as production's does.
func (f *staffFixture) statusOf(t *testing.T, tenantID, employeeID uuid.UUID) string {
	t.Helper()
	var status string
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT status FROM employees WHERE tenant_id = $1 AND id = $2`,
			tenantID, employeeID).Scan(&status)
	})
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	return status
}

func (f *staffFixture) placementOf(t *testing.T, employeeID uuid.UUID) (uuid.UUID, *uuid.UUID) {
	t.Helper()
	var loc uuid.UUID
	var dep *uuid.UUID
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT location_id, department_id FROM employees WHERE tenant_id = $1 AND id = $2`,
			f.tenantID, employeeID).Scan(&loc, &dep)
	})
	if err != nil {
		t.Fatalf("read placement: %v", err)
	}
	return loc, dep
}

// auditRows counts trail entries for one action ABOUT one employee, in the tenant's
// own context. Both predicates matter: audit_log is append-only and shared by every
// run against this database, so a count without the target would drift with the
// suite's own residue.
func (f *staffFixture) auditRows(t *testing.T, tenantID uuid.UUID, action string, target uuid.UUID) int {
	t.Helper()
	var n int
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM audit_log
			 WHERE tenant_id = $1 AND action = $2 AND target = $3`,
			tenantID, action, target.String()).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	return n
}

func (f *staffFixture) auditDetail(t *testing.T, tenantID uuid.UUID, action string, target uuid.UUID) (string, *uuid.UUID) {
	t.Helper()
	var detail string
	var actor *uuid.UUID
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT detail::text, actor_id FROM audit_log
			 WHERE tenant_id = $1 AND action = $2 AND target = $3
			 ORDER BY at DESC LIMIT 1`,
			tenantID, action, target.String()).Scan(&detail, &actor)
	})
	if err != nil {
		t.Fatalf("read audit detail: %v", err)
	}
	return detail, actor
}

// brokenTrail is a Trail that fails on demand. It is how the SHARED FATE is measured
// in the direction a passing test cannot otherwise reach.
type brokenTrail struct{ err error }

func (b brokenTrail) RecordTx(context.Context, pgx.Tx, audit.Event) (uuid.UUID, error) {
	return uuid.Nil, b.err
}

// TestStaffDB_DeactivationAndItsTrailShareOneTransaction is the card's "every action
// is written to audit_log", measured in BOTH directions rather than asserted in one.
//
// 🔴 THE FAILING-TRAIL ARM IS THE POINT. A version of this code that used
// audit.Record (its own transaction) instead of RecordTx passes every ordinary test:
// the employee is deactivated and a row appears. What it cannot survive is the trail
// failing — that version leaves somebody deactivated with no record of who did it,
// which is the §4.6 loss the criterion exists to prevent.
func TestStaffDB_DeactivationAndItsTrailShareOneTransaction(t *testing.T) {
	f := newStaffFixture(t)
	ctx := context.Background()

	// ARM ONE: the trail fails. Nothing may survive.
	broken, err := NewStaff(f.data, brokenTrail{err: errors.New("audit is down")},
		slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewStaff: %v", err)
	}
	if _, err := broken.Deactivate(ctx, DeactivateCommand{
		TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID,
	}); err == nil {
		t.Fatal("a failing trail was reported as success")
	}
	if got := f.statusOf(t, f.tenantID, f.employeeID); got != statusActive {
		t.Fatalf("status = %q after a FAILED deactivation, want %q — the employee change "+
			"must roll back with the trail row", got, statusActive)
	}
	if n := f.auditRows(t, f.tenantID, ActionEmployeeDeactivated, f.employeeID); n != 0 {
		t.Fatalf("%d audit row(s) after a failed write, want 0", n)
	}

	// ARM TWO: the real trail. Both rows, exactly once.
	if _, err := f.staff.Deactivate(ctx, DeactivateCommand{
		TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID,
	}); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	if got := f.statusOf(t, f.tenantID, f.employeeID); got != statusDeactivated {
		t.Fatalf("status = %q, want %q", got, statusDeactivated)
	}
	if n := f.auditRows(t, f.tenantID, ActionEmployeeDeactivated, f.employeeID); n != 1 {
		t.Fatalf("%d audit row(s), want exactly 1", n)
	}
	detail, actor := f.auditDetail(t, f.tenantID, ActionEmployeeDeactivated, f.employeeID)
	if actor == nil || *actor != f.actorID {
		t.Errorf("audit actor_id = %v, want the admin who acted (%s)", actor, f.actorID)
	}
	if want := `"previous_status": "active"`; !containsJSONField(detail, "previous_status", "active") {
		t.Errorf("audit detail = %s, want it to carry %s", detail, want)
	}
	// §4.7: the trail carries ids and a lifecycle word, never the person's name.
	if containsAny(detail, "Maria", "Borg") {
		t.Errorf("audit detail carries the employee's name: %s", detail)
	}
}

// TestStaffDB_ASecondDeactivationWritesNothing. A double submission, two managers
// pressing one after the other, a refreshed form: all land here. §4.6 wants the
// caller told, and the trail must not gain a second entry for an act that did not
// happen twice.
func TestStaffDB_ASecondDeactivationWritesNothing(t *testing.T) {
	f := newStaffFixture(t)
	ctx := context.Background()
	cmd := DeactivateCommand{TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID}

	if _, err := f.staff.Deactivate(ctx, cmd); err != nil {
		t.Fatalf("first Deactivate: %v", err)
	}
	var firstStamp string
	if err := f.data.WithTenant(ctx, f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT deactivated_at::text FROM employees WHERE tenant_id = $1 AND id = $2`,
			f.tenantID, f.employeeID).Scan(&firstStamp)
	}); err != nil {
		t.Fatalf("read stamp: %v", err)
	}

	_, err := f.staff.Deactivate(ctx, cmd)
	if !errors.Is(err, ErrAlreadyDeactivated) {
		t.Fatalf("second Deactivate returned %v, want ErrAlreadyDeactivated", err)
	}
	if n := f.auditRows(t, f.tenantID, ActionEmployeeDeactivated, f.employeeID); n != 1 {
		t.Errorf("%d audit row(s) after two presses, want 1", n)
	}
	var secondStamp string
	if err := f.data.WithTenant(ctx, f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT deactivated_at::text FROM employees WHERE tenant_id = $1 AND id = $2`,
			f.tenantID, f.employeeID).Scan(&secondStamp)
	}); err != nil {
		t.Fatalf("read stamp: %v", err)
	}
	if secondStamp != firstStamp {
		t.Errorf("deactivated_at moved from %q to %q; the stamp answers WHEN this ended "+
			"and rewriting it is a quiet history edit", firstStamp, secondStamp)
	}
}

// TestStaffDB_TwoManagersPressingAtOnceProduceOneAct is the concurrency shape the
// card's §4.6 question asks about. Exactly one of N callers may write, and the trail
// must agree with the row.
func TestStaffDB_TwoManagersPressingAtOnceProduceOneAct(t *testing.T) {
	f := newStaffFixture(t)
	const racers = 8

	var wg sync.WaitGroup
	results := make([]error, racers)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, results[i] = f.staff.Deactivate(context.Background(), DeactivateCommand{
				TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID,
			})
		}(i)
	}
	close(start)
	wg.Wait()

	winners := 0
	for i, err := range results {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, ErrAlreadyDeactivated):
		default:
			t.Errorf("racer %d failed with an unexpected error: %v", i, err)
		}
	}
	if winners != 1 {
		t.Errorf("%d of %d concurrent deactivations succeeded, want exactly 1", winners, racers)
	}
	if n := f.auditRows(t, f.tenantID, ActionEmployeeDeactivated, f.employeeID); n != 1 {
		t.Errorf("%d audit row(s) after %d concurrent presses, want 1", n, racers)
	}
}

// TestStaffDB_MoveAndItsTrailShareOneTransaction is the same measurement for the
// second write, plus the FROM/TO facts the trail is supposed to carry.
func TestStaffDB_MoveAndItsTrailShareOneTransaction(t *testing.T) {
	f := newStaffFixture(t)
	ctx := context.Background()

	broken, err := NewStaff(f.data, brokenTrail{err: errors.New("audit is down")},
		slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewStaff: %v", err)
	}
	if _, err := broken.Move(ctx, MoveCommand{
		TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID,
		LocationID: f.otherVenue, DepartmentID: &f.departmentID,
	}); err == nil {
		t.Fatal("a failing trail was reported as success")
	}
	if loc, dep := f.placementOf(t, f.employeeID); loc != f.locationID || dep != nil {
		t.Fatalf("placement moved to %s/%v despite a failed trail write", loc, dep)
	}

	if _, err := f.staff.Move(ctx, MoveCommand{
		TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID,
		LocationID: f.otherVenue, DepartmentID: &f.departmentID,
	}); err != nil {
		t.Fatalf("Move: %v", err)
	}
	loc, dep := f.placementOf(t, f.employeeID)
	if loc != f.otherVenue || dep == nil || *dep != f.departmentID {
		t.Fatalf("placement = %s/%v, want %s/%s", loc, dep, f.otherVenue, f.departmentID)
	}
	if n := f.auditRows(t, f.tenantID, ActionEmployeeMoved, f.employeeID); n != 1 {
		t.Fatalf("%d audit row(s), want exactly 1", n)
	}
	detail, actor := f.auditDetail(t, f.tenantID, ActionEmployeeMoved, f.employeeID)
	if actor == nil || *actor != f.actorID {
		t.Errorf("audit actor_id = %v, want %s", actor, f.actorID)
	}
	for field, want := range map[string]string{
		"from_location_id":   f.locationID.String(),
		"to_location_id":     f.otherVenue.String(),
		"from_department_id": "",
		"to_department_id":   f.departmentID.String(),
	} {
		if !containsJSONField(detail, field, want) {
			t.Errorf("audit detail %s does not carry %s = %q", detail, field, want)
		}
	}
}

// TestStaffDB_MovingToTheSamePlaceWritesNothing. §4.6 in its quietest form: nothing
// happened, so the trail must not say something did and the caller must be told.
func TestStaffDB_MovingToTheSamePlaceWritesNothing(t *testing.T) {
	f := newStaffFixture(t)
	_, err := f.staff.Move(context.Background(), MoveCommand{
		TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID,
		LocationID: f.locationID,
	})
	if !errors.Is(err, ErrSamePlacement) {
		t.Fatalf("Move to the same venue returned %v, want ErrSamePlacement", err)
	}
	if n := f.auditRows(t, f.tenantID, ActionEmployeeMoved, f.employeeID); n != 0 {
		t.Errorf("%d audit row(s) for a move that changed nothing, want 0", n)
	}
}

// TestStaffDB_ADeactivatedPersonCanStillBeMoved is §4.6's repair path: correcting
// where somebody worked has to stay possible months after they left.
func TestStaffDB_ADeactivatedPersonCanStillBeMoved(t *testing.T) {
	f := newStaffFixture(t)
	ctx := context.Background()
	if _, err := f.staff.Deactivate(ctx, DeactivateCommand{
		TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID,
	}); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	if _, err := f.staff.Move(ctx, MoveCommand{
		TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID,
		LocationID: f.otherVenue,
	}); err != nil {
		t.Fatalf("Move after deactivation: %v", err)
	}
	if got := f.statusOf(t, f.tenantID, f.employeeID); got != statusDeactivated {
		t.Errorf("status = %q after a move, want %q — a move must not touch the lifecycle",
			got, statusDeactivated)
	}
}

// TestStaffDB_AForeignEmployeeIsNeverTouched is §4.5, measured with two real tenants.
//
// 🔴 IT ASSERTS THE OTHER TENANT'S ROW IS UNCHANGED, not merely that an error came
// back. An implementation that returned an error AFTER writing would satisfy the
// first half.
func TestStaffDB_AForeignEmployeeIsNeverTouched(t *testing.T) {
	f := newStaffFixture(t)
	ctx := context.Background()

	_, err := f.staff.Deactivate(ctx, DeactivateCommand{
		TenantID: f.tenantID, EmployeeID: f.foreignEmployee, ActorID: f.actorID,
	})
	if !errors.Is(err, ErrUnknownEmployee) {
		t.Fatalf("deactivating another tenant's employee returned %v, want ErrUnknownEmployee", err)
	}
	if got := f.statusOf(t, f.foreignTenant, f.foreignEmployee); got != statusActive {
		t.Fatalf("the other tenant's employee is now %q", got)
	}
	if n := f.auditRows(t, f.tenantID, ActionEmployeeDeactivated, f.foreignEmployee); n != 0 {
		t.Errorf("%d audit row(s) were written about another tenant's employee", n)
	}
	if n := f.auditRows(t, f.foreignTenant, ActionEmployeeDeactivated, f.foreignEmployee); n != 0 {
		t.Errorf("%d audit row(s) appeared in the other tenant", n)
	}

	// POSITIVE CONTROL: the same command against this tenant's own employee works, so
	// the zeroes above are about the tenant boundary and not about a broken fixture.
	if _, err := f.staff.Deactivate(ctx, DeactivateCommand{
		TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID,
	}); err != nil {
		t.Fatalf("the control deactivation failed: %v", err)
	}
}

// TestStaffDB_AForeignVenueIsRefused is §4.5 on the OTHER id a client contributes.
// The composite foreign key would refuse it structurally (00003); this measures that
// the caller gets a sentence rather than a 23503.
func TestStaffDB_AForeignVenueIsRefused(t *testing.T) {
	f := newStaffFixture(t)
	ctx := context.Background()

	_, err := f.staff.Move(ctx, MoveCommand{
		TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID,
		LocationID: f.foreignVenue,
	})
	if !errors.Is(err, ErrUnknownPlacement) {
		t.Fatalf("moving to another tenant's venue returned %v, want ErrUnknownPlacement", err)
	}
	if loc, _ := f.placementOf(t, f.employeeID); loc != f.locationID {
		t.Fatalf("the employee moved to %s", loc)
	}
	if n := f.auditRows(t, f.tenantID, ActionEmployeeMoved, f.employeeID); n != 0 {
		t.Errorf("%d audit row(s) for a refused move, want 0", n)
	}

	// AND A DEPARTMENT FROM ANOTHER TENANT IS REFUSED THE SAME WAY. It is a separate
	// case because the department is optional and travels through a LEFT JOIN, where
	// "did not resolve" and "was not asked for" look alike unless the guard is right.
	foreignDepartment := uuid.New()
	if err := f.data.WithTenant(ctx, f.foreignTenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO departments (id, tenant_id, location_id, name)
			 VALUES ($1, $2, $3, 'Bar')`,
			foreignDepartment, f.foreignTenant, f.foreignVenue)
		return e
	}); err != nil {
		t.Fatalf("seed foreign department: %v", err)
	}
	_, err = f.staff.Move(ctx, MoveCommand{
		TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID,
		LocationID: f.otherVenue, DepartmentID: &foreignDepartment,
	})
	if !errors.Is(err, ErrUnknownPlacement) {
		t.Fatalf("moving into another tenant's department returned %v, want ErrUnknownPlacement", err)
	}
	if loc, dep := f.placementOf(t, f.employeeID); loc != f.locationID || dep != nil {
		t.Fatalf("the employee moved to %s/%v on a refused request", loc, dep)
	}
}

// TestStaffDB_ADepartmentOfAnotherVenueIsRefused is section 5 rather than section
// 4.5, and it is the defect a security audit measured being accepted SILENTLY:
// "MOVE venueA with deptB (which belongs to venueB) -> 303; stored location=venueA
// department=deptB".
//
// 🔴 WHY IT IS NOT COSMETIC. A department carries a SHIFT, and lateness is computed
// from the department's shift when there is one (section 5, GetDepartmentShift);
// policies can also be scoped to `department/<id>`. So the stored pair decides which
// clock and which rules a person is judged against, and a department of another
// branch judges them against that branch's morning.
//
// THE FIXTURE IS THE EXACT SHAPE: the department belongs to otherVenue, and the move
// sends the person to locationID -- this tenant's OTHER venue -- with it.
func TestStaffDB_ADepartmentOfAnotherVenueIsRefused(t *testing.T) {
	f := newStaffFixture(t)
	ctx := context.Background()

	// POSITIVE CONTROL FIRST: the department DOES pair with its own venue, so the
	// refusal below is about the pairing and not about a department the query cannot
	// see at all.
	if _, err := f.staff.Move(ctx, MoveCommand{
		TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID,
		LocationID: f.otherVenue, DepartmentID: &f.departmentID,
	}); err != nil {
		t.Fatalf("the control move (venue and its OWN department) failed: %v", err)
	}

	// NOW THE MISMATCH: the same department, the other venue of the same tenant.
	_, err := f.staff.Move(ctx, MoveCommand{
		TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID,
		LocationID: f.locationID, DepartmentID: &f.departmentID,
	})
	if !errors.Is(err, ErrUnknownPlacement) {
		t.Fatalf("moving to a venue with ANOTHER venue's department returned %v, want "+
			"ErrUnknownPlacement", err)
	}
	loc, dep := f.placementOf(t, f.employeeID)
	if loc != f.otherVenue || dep == nil || *dep != f.departmentID {
		t.Errorf("the refused move changed the placement to %s/%v; it must leave the row "+
			"exactly as the control left it", loc, dep)
	}
	if n := f.auditRows(t, f.tenantID, ActionEmployeeMoved, f.employeeID); n != 1 {
		t.Errorf("%d audit row(s); the refused move must not add one (the control wrote "+
			"the only legitimate one)", n)
	}
}

// TestStaffDB_AnUnknownIdIsARefusalNotAFailure. An invented uuid is the ordinary
// case of a stale page, and it must read as "not on this roster".
func TestStaffDB_AnUnknownIdIsARefusalNotAFailure(t *testing.T) {
	f := newStaffFixture(t)
	ctx := context.Background()
	ghost := uuid.New()

	if _, err := f.staff.Person(ctx, f.tenantID, ghost); !errors.Is(err, ErrUnknownEmployee) {
		t.Errorf("Person returned %v, want ErrUnknownEmployee", err)
	}
	if _, err := f.staff.Deactivate(ctx, DeactivateCommand{
		TenantID: f.tenantID, EmployeeID: ghost, ActorID: f.actorID,
	}); !errors.Is(err, ErrUnknownEmployee) {
		t.Errorf("Deactivate returned %v, want ErrUnknownEmployee", err)
	}
	if _, err := f.staff.Move(ctx, MoveCommand{
		TenantID: f.tenantID, EmployeeID: ghost, ActorID: f.actorID, LocationID: f.otherVenue,
	}); !errors.Is(err, ErrUnknownEmployee) {
		t.Errorf("Move returned %v, want ErrUnknownEmployee", err)
	}
}

// TestStaffDB_PersonReadsThePlacementNames is what the action card renders, and it
// is here rather than in a unit test because the LEFT JOINs are the thing under
// test: a person whose venue name did not come back gets "could not be read" on the
// screen, which must be a data defect rather than the ordinary case.
func TestStaffDB_PersonReadsThePlacementNames(t *testing.T) {
	f := newStaffFixture(t)
	p, err := f.staff.Person(context.Background(), f.tenantID, f.employeeID)
	if err != nil {
		t.Fatalf("Person: %v", err)
	}
	if p.Name != "Maria Borg" || p.LocationName != "St Julians" {
		t.Errorf("Person = %q at %q, want Maria Borg at St Julians", p.Name, p.LocationName)
	}
	if p.DepartmentName != "" {
		t.Errorf("DepartmentName = %q, want empty for somebody with no department", p.DepartmentName)
	}
	if !p.Invitable() || p.Deactivated() {
		t.Errorf("an active employee reads as invitable=%v deactivated=%v", p.Invitable(), p.Deactivated())
	}
}

// containsJSONField is a substring check that tolerates pgx's jsonb spacing. It is
// deliberately not a full unmarshal: the assertion is about what the DATABASE stored,
// and re-decoding it into the same struct that wrote it would prove the round trip
// rather than the content.
func containsJSONField(detail, field, want string) bool {
	for _, form := range []string{
		`"` + field + `": "` + want + `"`,
		`"` + field + `":"` + want + `"`,
	} {
		if containsAny(detail, form) {
			return true
		}
	}
	return false
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && len(s) >= len(sub) && indexOf(s, sub) >= 0 {
			return true
		}
	}
	return false
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
