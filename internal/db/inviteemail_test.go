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

// inviteemail_test.go -- M10 EM-7B's two reading statements (GetInviteRecipient and
// CountRecentInvites in db/queries/invites.sql) against real Postgres, section 4.5's
// two halves measured SEPARATELY, as employeeemail_test.go does for EM-6:
//
//	braces (RLS)   tappa_app in A's context, the statements given B's OWN tenant id —
//	               the explicit predicate would match, so only RLS can answer.
//	belt           tappa_owner (a superuser, RLS does not apply) given A's tenant id —
//	               only the statement's own predicates can answer. Rolled back.
//
// The third statement, LockTenantForInviteLimits, reads no table (an advisory lock
// keyed on the tenant id); internal/invite's concurrency test measures what it does.
//
// WHAT ONE RUN LEAVES BEHIND: two fresh tenants per test (named "em6-fixture" —
// emailTenant's, reused from the EM-6 file), each with a venue, one employee, one
// administrator and two invitation rows, all under fresh random ids and example.test
// addresses. tappa_app cannot delete them; the owner's
// probe writes nothing that survives (it is rolled back).

// inviteTenant commits a tenant whose employee holds address and whose administrator
// holds adminAddress, plus two invitation rows for the employee.
func inviteTenant(t *testing.T, d *DB, address, adminAddress string) (tenantID, employeeID uuid.UUID) {
	t.Helper()
	tenantID, employeeID = emailTenant(t, d, address)
	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
			 VALUES ($1, $2, 'em7b admin', $3, $4, 'owner', 'active')`,
			uuid.New(), tenantID, adminAddress, "$2a$04$"+strings.Repeat("b", 53)); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO employee_invites (tenant_id, employee_id, code_hash, expires_at)
			 SELECT $1, $2, md5(random()::text || g::text) || md5(random()::text || g::text), now() + interval '7 days'
			 FROM generate_series(1, 2) g`, tenantID, employeeID)
		return e
	})
	if err != nil {
		t.Fatalf("inviteTenant: %v", err)
	}
	return tenantID, employeeID
}

// TestRLS_InviteRecipient_AnotherTenantsRowsAreNeitherReadNorCounted is the braces.
func TestRLS_InviteRecipient_AnotherTenantsRowsAreNeitherReadNorCounted(t *testing.T) {
	d := appDB(t)
	assertAppRole(t, d)
	ctx := context.Background()
	theirs := em6Address("b")
	a, _ := inviteTenant(t, d, em6Address("a"), em6Address("a-admin"))
	b, bEmp := inviteTenant(t, d, theirs, em6Address("b-admin"))

	err := d.WithTenant(ctx, a, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if _, e := q.GetInviteRecipient(ctx, store.GetInviteRecipientParams{TenantID: b, EmployeeID: bEmp}); !errors.Is(e, pgx.ErrNoRows) {
			t.Errorf("A read B's recipient through GetInviteRecipient(B's tenant, B's employee): %v, want no row", e)
		}
		n, e := q.CountRecentInvites(ctx, store.CountRecentInvitesParams{TenantID: b, EmployeeID: bEmp})
		if e != nil {
			return e
		}
		if n.TenantLastDay != 0 || n.TenantLastHour != 0 || n.EmployeeLastHour != 0 {
			t.Errorf("A counted B's invitations: %+v", n)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("A's transaction: %v", err)
	}
	// POSITIVE CONTROL, in B's own context.
	err = d.WithTenant(ctx, b, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		row, e := q.GetInviteRecipient(ctx, store.GetInviteRecipientParams{TenantID: b, EmployeeID: bEmp})
		if e != nil {
			return e
		}
		if row.Email == nil || *row.Email != theirs {
			t.Errorf("control: B's own recipient is not B's address")
		}
		n, e := q.CountRecentInvites(ctx, store.CountRecentInvitesParams{TenantID: b, EmployeeID: bEmp})
		if e != nil {
			return e
		}
		if n.TenantLastDay != 2 || n.TenantLastHour != 2 || n.EmployeeLastHour != 2 {
			t.Errorf("control: B counts %+v of its own two invitations", n)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("B's control: %v", err)
	}
}

// TestInviteEmailQueries_CarryTheirOwnTenantPredicate is the belt: as a superuser, the
// statements given A's tenant id see nothing of B — not B's employee, not B's
// invitations, and not B's administrator, whose address is set EQUAL to A's
// employee's (other capitals) so the admin sub-query's own predicate is the only thing
// keeping is_admin_address false. Controls: the same superuser reads B's row with B's
// tenant and A's own count is A's two. One transaction, rolled back.
func TestInviteEmailQueries_CarryTheirOwnTenantPredicate(t *testing.T) {
	app := appDB(t)
	owner := ownerDB(t)
	assertOwnerRole(t, owner)
	ctx := context.Background()
	shared := em6Address("shared")
	a, aEmp := inviteTenant(t, app, shared, em6Address("a-admin"))
	b, bEmp := inviteTenant(t, app, em6Address("b"), strings.ToLower(shared))
	errRollback := errors.New("rollback: this probe keeps nothing")

	err := owner.WithTenant(ctx, a, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if _, e := q.GetInviteRecipient(ctx, store.GetInviteRecipientParams{TenantID: a, EmployeeID: bEmp}); !errors.Is(e, pgx.ErrNoRows) {
			t.Errorf("GetInviteRecipient(A, B's employee) as a superuser: %v, want no row — its tenant predicate is gone", e)
		}
		row, e := q.GetInviteRecipient(ctx, store.GetInviteRecipientParams{TenantID: a, EmployeeID: aEmp})
		if e != nil {
			return e
		}
		if row.IsAdminAddress {
			t.Error("A's employee's address counts as an administrator's because ANOTHER business's administrator holds it: " +
				"the admin sub-query lost its tenant predicate")
		}
		n, e := q.CountRecentInvites(ctx, store.CountRecentInvitesParams{TenantID: a, EmployeeID: bEmp})
		if e != nil {
			return e
		}
		if n.TenantLastDay != 2 || n.EmployeeLastHour != 0 {
			t.Errorf("CountRecentInvites(A, B's employee) as a superuser: %+v, want A's two and none of B's", n)
		}
		row, e = q.GetInviteRecipient(ctx, store.GetInviteRecipientParams{TenantID: b, EmployeeID: bEmp})
		if e != nil || row.Email == nil {
			t.Errorf("control: the superuser cannot read B's own recipient with B's tenant (%v)", e)
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("owner probe: %v", err)
	}
}

// TestInviteRecipient_TheAdministratorTestIsCaseInsensitive: is_admin_address compares
// as citext — the column's own equality — so the administrator's address in other
// capitals is the administrator's address; a different address is not.
func TestInviteRecipient_TheAdministratorTestIsCaseInsensitive(t *testing.T) {
	d := appDB(t)
	ctx := context.Background()
	admin := em6Address("admin")
	same, sameEmp := inviteTenant(t, d, strings.ToLower(admin), admin)
	other, otherEmp := inviteTenant(t, d, em6Address("someone"), admin)
	for _, tc := range []struct {
		tenant, emp uuid.UUID
		want        bool
	}{{same, sameEmp, true}, {other, otherEmp, false}} {
		if err := d.WithTenant(ctx, tc.tenant, func(ctx context.Context, tx pgx.Tx) error {
			row, e := store.New(tx).GetInviteRecipient(ctx, store.GetInviteRecipientParams{TenantID: tc.tenant, EmployeeID: tc.emp})
			if e != nil {
				return e
			}
			if row.IsAdminAddress != tc.want {
				t.Errorf("is_admin_address = %v, want %v", row.IsAdminAddress, tc.want)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}
