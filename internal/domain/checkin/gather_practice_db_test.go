package checkin

// The CALLER SIDE of the practice rule, against a real Postgres.
//
// 🔴 WHY THIS FILE EXISTS, stated as the measurement that produced it. §5's
// "a practice record never holds the chain open" is enforced TWICE — once here in
// gather (`if !open.Practice`) and once in internal/domain/tap's resolveDirection
// (`open == nil || open.Practice`). Two guards is what checkin.go asks for, and
// it is the right call for a failure mode that inflates somebody's hours. But it
// has a measured consequence for TESTING: because the two produce the SAME
// observable outcome, deleting EITHER one on its own left the whole suite green.
//
// Both outcomes really are identical from outside:
//
//	guard here removed  -> LastOpenIn is the practice row -> tap resolves it to IN
//	                       anyway, and practice stays false because LastForPerson
//	                       is already non-nil.
//	guard there removed -> LastOpenIn was never the practice row, so there is
//	                       nothing for the missing guard to get wrong.
//
// So no black-box test can tell the two apart, and a redundancy that nothing pins
// is a redundancy that quietly becomes a single point of failure the day somebody
// removes "the other one". This file pins THIS half at its own boundary: what
// gather hands to the decision engine. tap's half is pinned by
// TestDecide_DirectionPracticeOpenInDoesNotCloseChain.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
)

// practiceFixture commits a tenant, a location, an activated employee and ONE
// transaction: an open check-IN whose practice flag the caller chooses.
//
// The policy columns are left NULL, which is branch (a) of migration 00008's
// consistency CHECK ("no policy decision recorded on this row") — the shape the
// RLS fixtures already use. Nothing here needs a materialised policy layer,
// because gather reads history and never decides.
func practiceFixture(t *testing.T, data *db.DB, tenantID uuid.UUID, practice bool) (locationID, employeeID uuid.UUID) {
	t.Helper()
	locationID, employeeID = uuid.New(), uuid.New()
	err := data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name, static_ips, gps_lat, gps_lng)
			 VALUES ($1, $2, 'St Julians', '{203.0.113.0/24}', 35.918, 14.489)`,
			locationID, tenantID); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at, activated_at)
			 VALUES ($1, $2, $3, 'Joseph Camilleri', 'active', now(), now())`,
			employeeID, tenantID, locationID); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO transactions
			   (tenant_id, employee_id, location_id, type, occurred_at, verdict, channel, practice)
			 VALUES ($1, $2, $3, 'in', $4, 'ok', 'nfc', $5)`,
			tenantID, employeeID, locationID, time.Now().UTC().Add(-2*time.Hour), practice)
		return e
	})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return locationID, employeeID
}

// TestGatherDB_APracticeCheckInIsNeverHandedOnAsAnOpenOne is the caller-side half
// of §5's practice rule, asserted where it is decided rather than where it shows.
//
// The two rows differ in ONE column. With practice=false the open check-in is
// passed on, so the next tap closes it; with practice=true it is withheld, so the
// next tap is still an `in` — which is what keeps a training tap from turning
// somebody's real arrival into a departure and inflating their hours (the M4-06
// exploit).
//
// BOTH DIRECTIONS ARE ASSERTED ON PURPOSE. "lastOpenIn is nil" is also what a
// broken query returns, so the practice=false row is the positive control that
// says the fixture, the RLS context and the query are all live.
func TestGatherDB_APracticeCheckInIsNeverHandedOnAsAnOpenOne(t *testing.T) {
	data, tenantID, sets := newPolicyHarness(t)
	trail, err := audit.New(data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	svc := &Service{data: data, trail: trail, policies: sets, log: quietLogger()}

	tests := []struct {
		name        string
		practice    bool
		wantOpenIn  bool
		explanation string
	}{
		{
			name: "an ordinary open check-in is handed on", practice: false, wantOpenIn: true,
			explanation: "the positive control: without it, 'nil' below would also be what a broken query returns",
		},
		{
			name: "a practice check-in is withheld", practice: true, wantOpenIn: false,
			explanation: "§5/M4-06: a training tap must not make the next real tap a check-OUT",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			locationID, employeeID := practiceFixture(t, data, tenantID, tc.practice)

			facts, err := svc.gather(context.Background(), Request{
				SessionTenantID: tenantID, EmployeeID: employeeID,
			}, locationID)
			if err != nil {
				t.Fatalf("gather: %v", err)
			}

			if facts.lastForPerson == nil {
				t.Fatal("the person's last tap was not read at all: this test is measuring nothing")
			}
			if got := facts.lastOpenIn != nil; got != tc.wantOpenIn {
				t.Errorf("lastOpenIn present = %v, want %v — %s", got, tc.wantOpenIn, tc.explanation)
			}
			if tc.wantOpenIn && facts.lastOpenIn != nil && facts.lastOpenIn.Practice {
				t.Error("the row handed on is marked practice; the two fixtures are the wrong way round")
			}
		})
	}
}
