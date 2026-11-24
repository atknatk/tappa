package tenant

// staffdeletion_db_test.go -- M16-37 against real Postgres: [G37-a] an active person's request
// revokes their sessions and trails one row, the People card reads it, and no record is deleted;
// [G37-b] a deactivated person's credentials stay live and the request is still trailed.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (f *staffFixture) transactionsOf(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM transactions WHERE employee_id = $1`, f.employeeID).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestStaffDB_ADeletionRequestSignsOutAndTellsTheEmployer(t *testing.T) {
	f := newStaffFixture(t)
	f.seedSessions(t, f.tenantID, f.employeeID, 2)
	before := f.transactionsOf(t)
	got, err := f.staff.RequestDeletion(context.Background(), DeletionRequest{TenantID: f.tenantID, EmployeeID: f.employeeID, AppDeviceID: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
	if got.Sessions != 2 || got.Deactivated {
		t.Fatalf("got %+v, want 2 sessions revoked", got)
	}
	if live := f.liveSessions(t, f.tenantID, f.employeeID); live != 0 {
		t.Fatalf("%d live sessions after the request", live)
	}
	if rows := f.auditRows(t, f.tenantID, ActionEmployeeDeletionRequested, f.employeeID); rows != 1 {
		t.Fatalf("%d trail rows, want 1", rows)
	}
	if after := f.transactionsOf(t); after != before {
		t.Fatalf("transactions %d -> %d: a deletion request deletes no record", before, after)
	}
	sum, err := f.staff.Summary(context.Background(), f.tenantID, f.employeeID)
	if err != nil || sum.DeletionRequested == nil {
		t.Fatalf("the card does not see the request: %+v %v", sum.DeletionRequested, err)
	}
	if st := f.statusOf(t, f.tenantID, f.employeeID); st != "active" {
		t.Fatalf("status %q: the request neither deletes nor deactivates the person", st)
	}
}

func TestStaffDB_ADeactivatedPersonsDeletionRequestRevokesNothing(t *testing.T) {
	f := newStaffFixture(t)
	f.seedSessions(t, f.tenantID, f.employeeID, 1)
	if _, err := f.staff.Deactivate(context.Background(), DeactivateCommand{TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID}); err != nil {
		t.Fatal(err)
	}
	got, err := f.staff.RequestDeletion(context.Background(), DeletionRequest{TenantID: f.tenantID, EmployeeID: f.employeeID, AppDeviceID: uuid.New()})
	if err != nil || !got.Deactivated || got.Sessions != 0 {
		t.Fatalf("got %+v %v, want deactivated and nothing revoked", got, err)
	}
	if live := f.liveSessions(t, f.tenantID, f.employeeID); live != 1 {
		t.Fatalf("%d live sessions, want the 1 kept (§5 row 4)", live)
	}
	if rows := f.auditRows(t, f.tenantID, ActionEmployeeDeletionRequested, f.employeeID); rows != 1 {
		t.Fatalf("%d trail rows, want 1", rows)
	}
}
