package fixtures

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestGiveFarDayCutoff_WritesThePersonTierOnly measures what GiveFarDayCutoff writes, against a
// real Postgres (M15-04's rule: the person tier, never a shared tenant's or branch's): a tenant
// and a branch of the test's own with cutoffs of their own (05:00, 06:00), an employee of that
// branch; after the helper ran as tappa_app in the tenant's context, the employee carries the
// returned cutoff and the tenant's and the branch's are what they were. And a seeded employee is
// refused through the same path — in a transaction that is rolled back whatever happens.
func TestGiveFarDayCutoff_WritesThePersonTierOnly(t *testing.T) {
	appDSN, ownerDSN := os.Getenv("DATABASE_URL"), os.Getenv("DATABASE_MIGRATE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("DATABASE_URL and DATABASE_MIGRATE_URL are both required (real Postgres)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Close(context.Background()) }()
	app, err := pgx.Connect(ctx, appDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Close(context.Background()) }()

	tenant, branch, emp := uuid.New(), uuid.New(), uuid.New()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO tenants (id, name, vat_number, business_type, structure, day_cutoff) VALUES ($1, 'm15-04 fixture', $2, 'restaurant', 'multi', '05:00')`,
			[]any{tenant, "VAT-M1504-" + tenant.String()}},
		{`INSERT INTO locations (id, tenant_id, name, day_cutoff) VALUES ($1, $2, 'm15-04 branch', '06:00')`, []any{branch, tenant}},
		{`INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at) VALUES ($1, $2, $3, 'm15-04 person', 'active', now())`,
			[]any{emp, tenant, branch}},
	} {
		if _, err := owner.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	cutoffs := func(tenantID, employeeID uuid.UUID) (ten, loc, person pgtype.Time) {
		t.Helper()
		if err := owner.QueryRow(ctx, `SELECT t.day_cutoff, l.day_cutoff, e.day_cutoff
		                               FROM employees e JOIN tenants t ON t.id = e.tenant_id
		                               LEFT JOIN locations l ON l.id = e.location_id AND l.tenant_id = e.tenant_id
		                               WHERE e.tenant_id = $1 AND e.id = $2`, tenantID, employeeID).Scan(&ten, &loc, &person); err != nil {
			t.Fatal(err)
		}
		return
	}
	// give runs the helper as tappa_app in the tenant's context; commit says whether the
	// transaction is kept. The seeded case below is ALWAYS rolled back, so even a regression of the
	// refusal cannot leave a cutoff on a seeded person in the shared database.
	give := func(tenantID, employeeID uuid.UUID, commit bool) (time.Duration, error) {
		t.Helper()
		tx, err := app.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID.String()); err != nil {
			t.Fatal(err)
		}
		c, err := GiveFarDayCutoff(ctx, tx, tenantID, employeeID, time.Now())
		if err != nil || !commit {
			return c, err
		}
		return c, tx.Commit(ctx)
	}

	tenBefore, locBefore, _ := cutoffs(tenant, emp)
	c, err := give(tenant, emp, true)
	if err != nil {
		t.Fatalf("GiveFarDayCutoff: %v", err)
	}
	tenAfter, locAfter, person := cutoffs(tenant, emp)
	if tenAfter != tenBefore || locAfter != locBefore {
		t.Errorf("the tenant's cutoff %v -> %v, the branch's %v -> %v: the helper wrote a shared tier",
			tenBefore, tenAfter, locBefore, locAfter)
	}
	if !person.Valid || time.Duration(person.Microseconds)*time.Microsecond != c {
		t.Errorf("the employee's cutoff = %+v, want the returned %v", person, c)
	}

	_, _, seededBefore := cutoffs(TenantKF, EmpKFMariaBorg)
	if _, err := give(TenantKF, EmpKFMariaBorg, false); err == nil {
		t.Error("a seeded employee was accepted")
	}
	if _, _, seededAfter := cutoffs(TenantKF, EmpKFMariaBorg); seededAfter != seededBefore {
		t.Errorf("the seeded employee's cutoff moved %+v -> %+v", seededBefore, seededAfter)
	}
}
