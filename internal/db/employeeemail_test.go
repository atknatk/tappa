package db

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// employeeemail_test.go -- M10 EM-6's three statements (GetEmployeeEmail,
// LockEmployeeForEmailChange, SetEmployeeEmail in db/queries/employees.sql) against
// real Postgres, the two halves of section 4.5 measured SEPARATELY (CLAUDE.md §6):
//
//	braces (RLS)   tappa_app in A's context, the statements given B's OWN tenant id —
//	               so the explicit predicate would MATCH and only RLS can answer "no
//	               row". Plus a WHERE-less probe, which a predicate cannot explain.
//	belt           tappa_owner (a superuser: RLS does not apply) given A's tenant id
//	               and B's employee — only the statement's own predicate can answer
//	               "no row". Rolled back: the owner writes nothing that survives.
//
// Each negative has its positive control in the owning context. Fixtures are not
// cleaned up (rls_test.go's reasoning: tappa_app holds no DELETE on employees).

// emailTenant commits a tenant, a venue and one employee holding address, as
// tappa_app in the tenant's own context.
func emailTenant(t *testing.T, d *DB, address string) (tenantID, employeeID uuid.UUID) {
	t.Helper()
	tenantID, employeeID = uuid.New(), uuid.New()
	venue := uuid.New()
	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'em6-fixture', $2, 'bar', 'single')`,
			tenantID, "VAT-"+tenantID.String()); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name, gps_lat, gps_lng)
			 VALUES ($1, $2, 'em6 venue', 35.9, 14.5)`, venue, tenantID); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status, email)
			 VALUES ($1, $2, $3, 'em6 person', 'active', $4)`,
			employeeID, tenantID, venue, address)
		return e
	})
	if err != nil {
		t.Fatalf("emailTenant: %v", err)
	}
	return tenantID, employeeID
}

// em6Address is MIXED-CASE on purpose (EM-6 round 2): the positive controls below
// compare the address read through the generated statement with the one written, byte
// for byte, which an all-lower-case fixture cannot distinguish from a read that
// lower-cases the column (the audit's A18).
func em6Address(label string) string {
	return label + "." + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:12]) + "@EM6.example.test"
}

// TestRLS_EmployeeEmail_AnotherTenantsAddressIsNeitherReadNorWritten is the braces.
func TestRLS_EmployeeEmail_AnotherTenantsAddressIsNeitherReadNorWritten(t *testing.T) {
	d := appDB(t)
	assertAppRole(t, d)
	ctx := context.Background()
	theirs := em6Address("b")
	a, _ := emailTenant(t, d, em6Address("a"))
	b, bEmp := emailTenant(t, d, theirs)

	err := d.WithTenant(ctx, a, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if _, e := q.GetEmployeeEmail(ctx, store.GetEmployeeEmailParams{TenantID: b, ID: bEmp}); !errors.Is(e, pgx.ErrNoRows) {
			t.Errorf("A read B's address through GetEmployeeEmail(B's tenant, B's employee): %v, want no row", e)
		}
		if _, e := q.LockEmployeeForEmailChange(ctx, store.LockEmployeeForEmailChangeParams{TenantID: b, ID: bEmp}); !errors.Is(e, pgx.ErrNoRows) {
			t.Errorf("A locked B's employee: %v, want no row", e)
		}
		next := em6Address("hijack")
		if _, e := q.SetEmployeeEmail(ctx, store.SetEmployeeEmailParams{Email: &next, TenantID: b, ID: bEmp}); !errors.Is(e, pgx.ErrNoRows) {
			t.Errorf("A wrote B's address: %v, want no row", e)
		}
		// WHERE-LESS ON THE TENANT: only the id. A 0 here cannot come from a predicate.
		var n int
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM employees WHERE id = $1`, bEmp).Scan(&n); e != nil {
			return e
		}
		if n != 0 {
			t.Errorf("A's context sees %d row(s) of B's employee with no tenant predicate at all", n)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("A's transaction: %v", err)
	}

	// POSITIVE CONTROLS, in B's own context: the row exists, is readable and still holds
	// B's address — so the zeroes above are the boundary and not an empty fixture.
	err = d.WithTenant(ctx, b, func(ctx context.Context, tx pgx.Tx) error {
		row, e := store.New(tx).GetEmployeeEmail(ctx, store.GetEmployeeEmailParams{TenantID: b, ID: bEmp})
		if e != nil {
			return e
		}
		if row.Email == nil || *row.Email != theirs {
			t.Errorf("B's address changed after A's attempts (nil %v), want %q", row.Email == nil, theirs)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("B's control read: %v", err)
	}
}

// TestEmployeeEmailQueries_CarryTheirOwnTenantPredicate is the belt: as a superuser,
// where RLS does not apply, each statement given A's tenant and B's employee finds no
// row — the explicit tenant predicate alone answers. The control: the same superuser
// reads B's address when given B's tenant. Everything runs in one transaction that is
// rolled back.
func TestEmployeeEmailQueries_CarryTheirOwnTenantPredicate(t *testing.T) {
	app := appDB(t)
	owner := ownerDB(t)
	assertOwnerRole(t, owner)
	ctx := context.Background()
	theirs := em6Address("b")
	a, _ := emailTenant(t, app, em6Address("a"))
	b, bEmp := emailTenant(t, app, theirs)
	errRollback := errors.New("rollback: this probe keeps nothing")

	err := owner.WithTenant(ctx, a, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if _, e := q.GetEmployeeEmail(ctx, store.GetEmployeeEmailParams{TenantID: a, ID: bEmp}); !errors.Is(e, pgx.ErrNoRows) {
			t.Errorf("GetEmployeeEmail(A, B's employee) as a superuser: %v, want no row — its tenant predicate is gone", e)
		}
		if _, e := q.LockEmployeeForEmailChange(ctx, store.LockEmployeeForEmailChangeParams{TenantID: a, ID: bEmp}); !errors.Is(e, pgx.ErrNoRows) {
			t.Errorf("LockEmployeeForEmailChange(A, B's employee) as a superuser: %v, want no row", e)
		}
		next := em6Address("hijack")
		if _, e := q.SetEmployeeEmail(ctx, store.SetEmployeeEmailParams{Email: &next, TenantID: a, ID: bEmp}); !errors.Is(e, pgx.ErrNoRows) {
			t.Errorf("SetEmployeeEmail(A, B's employee) as a superuser: %v, want no row", e)
		}
		row, e := q.GetEmployeeEmail(ctx, store.GetEmployeeEmailParams{TenantID: b, ID: bEmp})
		if e != nil || row.Email == nil || *row.Email != theirs {
			t.Errorf("control: the superuser cannot read B's own address with B's tenant (err %v, "+
				"matches %v); without this the no-rows above prove nothing", e, row.Email != nil && *row.Email == theirs)
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("owner probe: %v", err)
	}
}
