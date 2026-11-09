package tenant

// staffsignout_db_test.go -- T59's "Sign out their phones" against real Postgres: the revoke and its
// trail share one transaction, the count is the rows changed, a second press changes nothing, a
// foreign or a deactivated person is never touched, and concurrent presses revoke each session once.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// seedSessions gives one employee n live sessions. The hash is random hex: this test never
// resolves a cookie, it counts rows.
func (f *staffFixture) seedSessions(t *testing.T, tenantID, employeeID uuid.UUID, n int) {
	t.Helper()
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		for i := 0; i < n; i++ {
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO sessions (tenant_id, employee_id, token_hash) VALUES ($1, $2, $3)`,
				tenantID, employeeID, hex.EncodeToString(b)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed sessions: %v", err)
	}
}

// liveSessions counts with no tenant_id in the WHERE beyond RLS's own: the context names the
// tenant, so a foreign row could only appear if RLS let it.
func (f *staffFixture) liveSessions(t *testing.T, tenantID, employeeID uuid.UUID) int {
	t.Helper()
	var n int
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM sessions WHERE employee_id = $1 AND revoked_at IS NULL`,
			employeeID).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count live sessions: %v", err)
	}
	return n
}

func (f *staffFixture) signOut() SignOutCommand {
	return SignOutCommand{TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID}
}

func TestStaffDB_SignOutRevokesEveryLiveSessionAndTrailsTheCount(t *testing.T) {
	f := newStaffFixture(t)
	f.seedSessions(t, f.tenantID, f.employeeID, 3)

	n, err := f.staff.SignOutPhones(context.Background(), f.signOut())
	if err != nil {
		t.Fatalf("SignOutPhones: %v", err)
	}
	if n != 3 {
		t.Errorf("revoked %d session(s), want 3", n)
	}
	if live := f.liveSessions(t, f.tenantID, f.employeeID); live != 0 {
		t.Errorf("%d live session(s) after the sign-out, want 0", live)
	}
	if rows := f.auditRows(t, f.tenantID, ActionEmployeePhonesSignedOut, f.employeeID); rows != 1 {
		t.Fatalf("%d trail row(s), want 1", rows)
	}
	detail, actor := f.auditDetail(t, f.tenantID, ActionEmployeePhonesSignedOut, f.employeeID)
	if !containsAny(detail, `"sessions_revoked": 3`, `"sessions_revoked":3`) {
		t.Errorf("the trail detail is %s, want sessions_revoked 3", detail)
	}
	if actor == nil || *actor != f.actorID {
		t.Errorf("the trail's actor is %v, want the admin %s", actor, f.actorID)
	}
	// The detail is the count and nothing else -- no session id, no hash.
	if strings.Count(detail, ":") != 1 || containsAny(detail, "hash", "token", "session_id") {
		t.Errorf("the trail detail carries more than the count: %s", detail)
	}
}

func TestStaffDB_ASecondSignOutWritesNothing(t *testing.T) {
	f := newStaffFixture(t)
	f.seedSessions(t, f.tenantID, f.employeeID, 2)
	if _, err := f.staff.SignOutPhones(context.Background(), f.signOut()); err != nil {
		t.Fatalf("first SignOutPhones: %v", err)
	}
	n, err := f.staff.SignOutPhones(context.Background(), f.signOut())
	if !errors.Is(err, ErrNoLiveSessions) || n != 0 {
		t.Fatalf("the second press answered (%d, %v), want (0, ErrNoLiveSessions)", n, err)
	}
	if rows := f.auditRows(t, f.tenantID, ActionEmployeePhonesSignedOut, f.employeeID); rows != 1 {
		t.Errorf("%d trail row(s) after two presses, want 1 -- a press that revokes nothing is no act", rows)
	}
}

func TestStaffDB_SignOutAndItsTrailShareOneTransaction(t *testing.T) {
	f := newStaffFixture(t)
	f.seedSessions(t, f.tenantID, f.employeeID, 2)
	broken, err := NewStaff(f.data, brokenTrail{err: errors.New("trail down")}, nil)
	if err != nil {
		t.Fatalf("NewStaff: %v", err)
	}
	if _, err := broken.SignOutPhones(context.Background(), f.signOut()); err == nil {
		t.Fatal("a sign-out whose trail failed reported success")
	}
	if live := f.liveSessions(t, f.tenantID, f.employeeID); live != 2 {
		t.Errorf("%d live session(s) after a failed trail, want 2 -- the revoke must roll back with it", live)
	}
}

func TestStaffDB_SignOutNeverTouchesAnotherBusinessesPerson(t *testing.T) {
	f := newStaffFixture(t)
	f.seedSessions(t, f.foreignTenant, f.foreignEmployee, 2)
	f.seedSessions(t, f.tenantID, f.employeeID, 1) // the control's own session

	_, err := f.staff.SignOutPhones(context.Background(), SignOutCommand{
		TenantID: f.tenantID, EmployeeID: f.foreignEmployee, ActorID: f.actorID,
	})
	if !errors.Is(err, ErrUnknownEmployee) {
		t.Fatalf("signing out another business's person answered %v, want ErrUnknownEmployee", err)
	}
	if live := f.liveSessions(t, f.foreignTenant, f.foreignEmployee); live != 2 {
		t.Errorf("the other business's person has %d live session(s), want 2", live)
	}
	if rows := f.auditRows(t, f.tenantID, ActionEmployeePhonesSignedOut, f.foreignEmployee); rows != 0 {
		t.Errorf("%d trail row(s) about another business's person", rows)
	}
	// CONTROL: the same call on this business's own person works.
	if n, err := f.staff.SignOutPhones(context.Background(), f.signOut()); err != nil || n != 1 {
		t.Fatalf("the control sign-out answered (%d, %v), want (1, nil)", n, err)
	}
}

// A deactivated person's sessions stay live so their attempts reach §5 row 4 and are recorded.
func TestStaffDB_ADeactivatedPersonsPhonesAreNotSignedOut(t *testing.T) {
	f := newStaffFixture(t)
	f.seedSessions(t, f.tenantID, f.employeeID, 1)
	if _, err := f.staff.Deactivate(context.Background(), DeactivateCommand{
		TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID,
	}); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	_, err := f.staff.SignOutPhones(context.Background(), f.signOut())
	if !errors.Is(err, ErrSignOutDeactivated) {
		t.Fatalf("signing out a deactivated person answered %v, want ErrSignOutDeactivated", err)
	}
	if live := f.liveSessions(t, f.tenantID, f.employeeID); live != 1 {
		t.Errorf("%d live session(s), want 1 -- the session must stay so the next tap is recorded", live)
	}
	if rows := f.auditRows(t, f.tenantID, ActionEmployeePhonesSignedOut, f.employeeID); rows != 0 {
		t.Errorf("%d trail row(s) for a refused sign-out", rows)
	}
}

// Concurrent presses: each session is revoked by exactly one of them, the counts the winners
// answer add up to the sessions there were, and the trail rows match the winners. (Postgres may
// split the rows between two racers; what must hold is that no session is counted twice.)
func TestStaffDB_ConcurrentSignOutsRevokeEachSessionOnce(t *testing.T) {
	f := newStaffFixture(t)
	const sessions, racers = 3, 8
	f.seedSessions(t, f.tenantID, f.employeeID, sessions)

	var wg sync.WaitGroup
	counts := make([]int, racers)
	errs := make([]error, racers)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			counts[i], errs[i] = f.staff.SignOutPhones(context.Background(), f.signOut())
		}(i)
	}
	close(start)
	wg.Wait()

	total, winners := 0, 0
	for i, err := range errs {
		switch {
		case err == nil:
			winners++
			total += counts[i]
		case errors.Is(err, ErrNoLiveSessions):
		default:
			t.Errorf("racer %d failed: %v", i, err)
		}
	}
	if total != sessions || winners < 1 {
		t.Errorf("winners revoked %d session(s) in total across %d winner(s), want %d", total, winners, sessions)
	}
	if rows := f.auditRows(t, f.tenantID, ActionEmployeePhonesSignedOut, f.employeeID); rows != winners {
		t.Errorf("%d trail row(s) for %d winner(s)", rows, winners)
	}
	if live := f.liveSessions(t, f.tenantID, f.employeeID); live != 0 {
		t.Errorf("%d live session(s) left", live)
	}
}
