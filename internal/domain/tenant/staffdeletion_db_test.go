package tenant

// staffdeletion_db_test.go -- M16-37 against real Postgres: [G37-a] an active person's request
// revokes their sessions and trails one row, the People card reads it, and no record is deleted;
// [G37-b] a deactivated person's credentials stay live and the request is still trailed; and a
// deactivated person's repeat inside DeletionRepeatWindow writes nothing, also when it races.

import (
	"context"
	"sync"
	"testing"
	"time"

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
	if !sum.DeletionSignedOut || sum.DeletionWhileDeactivated {
		t.Fatalf("the card reads signed out %v, deactivated %v from the trail row; want true, false", sum.DeletionSignedOut, sum.DeletionWhileDeactivated)
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

// TestStaffDB_ADeactivatedPersonsRepeatWritesNothing: a deactivated person keeps their device (T59), so
// they can ask again; a repeat inside DeletionRepeatWindow answers Repeat and writes no second trail
// row. The card reads "nothing was signed out" from the row (not from the status now).
func TestStaffDB_ADeactivatedPersonsRepeatWritesNothing(t *testing.T) {
	f := newStaffFixture(t)
	if _, err := f.staff.Deactivate(context.Background(), DeactivateCommand{TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID}); err != nil {
		t.Fatal(err)
	}
	req := DeletionRequest{TenantID: f.tenantID, EmployeeID: f.employeeID, AppDeviceID: uuid.New()}
	first, err := f.staff.RequestDeletion(context.Background(), req)
	if err != nil || first.Repeat {
		t.Fatalf("first = %+v %v, want a written request", first, err)
	}
	second, err := f.staff.RequestDeletion(context.Background(), req)
	if err != nil || !second.Repeat || !second.Deactivated {
		t.Fatalf("second = %+v %v, want a deactivated repeat", second, err)
	}
	if rows := f.auditRows(t, f.tenantID, ActionEmployeeDeletionRequested, f.employeeID); rows != 1 {
		t.Fatalf("%d trail rows after a repeat, want 1", rows)
	}
	sum, err := f.staff.Summary(context.Background(), f.tenantID, f.employeeID)
	if err != nil || sum.DeletionRequested == nil || sum.DeletionSignedOut || !sum.DeletionWhileDeactivated {
		t.Fatalf("summary %+v %v, want the request read as deactivated and nothing signed out", sum, err)
	}
	// CONTROL: an ACTIVE person's request is not a repeat, whatever came before (here: the same
	// person's deactivated request, reactivated since).
	f.exec(t, f.tenantID, `UPDATE employees SET status = 'active', deactivated_at = NULL WHERE tenant_id = $1 AND id = $2`, f.tenantID, f.employeeID)
	again, err := f.staff.RequestDeletion(context.Background(), req)
	if err != nil || again.Repeat || again.Deactivated {
		t.Fatalf("an active person's request = %+v %v, want a written one", again, err)
	}
	if rows := f.auditRows(t, f.tenantID, ActionEmployeeDeletionRequested, f.employeeID); rows != 2 {
		t.Fatalf("%d trail rows, want 2: the active person's request is trailed", rows)
	}
}

// TestStaffDB_ConcurrentDeactivatedRequestsWriteOneRow: N simultaneous requests of one deactivated
// person -> exactly one written (LockEmployeeForDeletionRequest), the rest repeats. -race. Each request
// that passes the repeat check holds its transaction open a moment (afterDeletionChecked), so without
// the lock the others reach the same check meanwhile and write too (measured: the lock removed, this
// is red).
func TestStaffDB_ConcurrentDeactivatedRequestsWriteOneRow(t *testing.T) {
	f := newStaffFixture(t)
	f.staff.afterDeletionChecked = func(context.Context) { time.Sleep(300 * time.Millisecond) }
	if _, err := f.staff.Deactivate(context.Background(), DeactivateCommand{TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID}); err != nil {
		t.Fatal(err)
	}
	const n = 8
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		written int
		errs    []error
	)
	start := make(chan struct{})
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got, err := f.staff.RequestDeletion(context.Background(), DeletionRequest{TenantID: f.tenantID, EmployeeID: f.employeeID, AppDeviceID: uuid.New()})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			if !got.Repeat {
				written++
			}
		}()
	}
	close(start)
	wg.Wait()
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	if written != 1 {
		t.Fatalf("%d of %d concurrent requests were written, want exactly 1", written, n)
	}
	if rows := f.auditRows(t, f.tenantID, ActionEmployeeDeletionRequested, f.employeeID); rows != 1 {
		t.Fatalf("%d trail rows, want 1", rows)
	}
}
