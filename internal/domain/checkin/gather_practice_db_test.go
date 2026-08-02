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
//
// 🔴 UPDATE (M5-11, ADR 0008) — WHAT THE TWO GUARDS NEVER COVERED. Both of them
// answer "is THIS row a practice row"; neither could answer "is there a real open
// check-in UNDERNEATH it", because the query handed over exactly one row. So a
// practice row that merely sorted newest hid a real open check-in and the next tap
// came out as an `in`. GetLastOpenTransaction now carries `AND NOT t.practice`,
// which means the guard in gather can no longer be false and the paragraph above
// describes a REDUNDANCY, not the enforcement. The enforcement is the predicate,
// and it is pinned by TestGatherDB_APracticeRowDoesNotHideAnOlderOpenCheckIn below
// — a mutation that drops it turns that test red, which is what the two guards
// could never do for each other.

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

			// gather now runs INSIDE the caller's transaction (ADR 0006 layer 4:
			// lock, gather, decide and write share one), so the test supplies one.
			var facts tapFacts
			err := data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
				var e error
				facts, e = svc.gather(ctx, tx, Request{
					SessionTenantID: tenantID, EmployeeID: employeeID,
				}, locationID)
				return e
			})
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

// chainFixture commits a tenant's location, an activated employee and the rows
// described by `rows`, in the order given. It writes them directly rather than
// through the engine because the point is to present gather with HISTORY SHAPES,
// including ones the engine cannot currently produce (see the "unreachable today"
// row below) — a fixture that could only build reachable shapes could not ask the
// question this file is about.
func chainFixture(t *testing.T, data *db.DB, tenantID uuid.UUID, rows []chainRow) (locationID, employeeID uuid.UUID) {
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
			 VALUES ($1, $2, $3, 'Rita Zammit', 'active', now(), now())`,
			employeeID, tenantID, locationID); e != nil {
			return e
		}
		now := time.Now().UTC()
		for _, r := range rows {
			if _, e := tx.Exec(ctx,
				`INSERT INTO transactions
				   (tenant_id, employee_id, location_id, type, occurred_at, verdict, channel, practice)
				 VALUES ($1, $2, $3, $4, $5, 'ok', 'nfc', $6)`,
				tenantID, employeeID, locationID, r.direction, now.Add(r.ago), r.practice); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return locationID, employeeID
}

// chainRow is one history row: how long ago it happened, its direction, and
// whether it is a training tap.
type chainRow struct {
	ago       time.Duration // negative: in the past
	direction string
	practice  bool
}

// TestGatherDB_APracticeRowDoesNotHideAnOlderOpenCheckIn is the pin for ADR 0008,
// and the reason it is a SEPARATE test from the one above is that the question is
// different: not "is a practice row handed on" (no) but "is the REAL open check-in
// UNDERNEATH one still found" (it now is).
//
// MEASURED BEFORE THE FIX, over plain HTTP, three rows per arm, the only difference
// being the practice row's occurred_at:
//
//	practice 4 h ago (below the real 'in')   third tap 'out', 0 open check-ins
//	practice 100 s ago (above it)            third tap 'in',  2 open check-ins
//
// The second arm is the defect: the checkout became an `in`, the real entry never
// closed, and nothing signalled it — verdict ok, no note, no flag. Both arms now
// answer the same, which is the whole claim of the fix.
//
// THE LAST ROW IS DELIBERATELY UNREACHABLE THROUGH THE ENGINE. Two practice rows in
// a row cannot happen today: isPracticeTap requires the person to have NO record at
// all, so the practice run is spent by the first row whatever it is (measured over
// HTTP in TestSeedDB_ASecondActivationIsNotASecondPracticeRun — a re-activated
// employee's next record is practice=false, and ConsumeInviteAndActivate does not
// even move activated_at). It is asserted anyway because the DATABASE can hold that
// shape — an import, a backfill, or M9-01's offline queue writing out of order —
// and "the query cannot be handed this" is a claim about today's writers, not about
// the query.
func TestGatherDB_APracticeRowDoesNotHideAnOlderOpenCheckIn(t *testing.T) {
	data, tenantID, sets := newPolicyHarness(t)
	trail, err := audit.New(data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	svc := &Service{data: data, trail: trail, policies: sets, log: quietLogger()}

	const realIn = -3 * time.Hour

	tests := []struct {
		name       string
		rows       []chainRow
		wantOpenIn bool
		wantAgo    time.Duration // which row gather must hand on
		why        string
	}{
		{
			name: "control: the practice row sorts BELOW the real check-in",
			rows: []chainRow{
				{ago: -4 * time.Hour, direction: "in", practice: true},
				{ago: realIn, direction: "in", practice: false},
			},
			wantOpenIn: true, wantAgo: realIn,
			why: "the arm that always worked: ORDER BY occurred_at DESC surfaced the real row first",
		},
		{
			name: "the defect: the practice row sorts ABOVE the real check-in",
			rows: []chainRow{
				{ago: realIn, direction: "in", practice: false},
				{ago: -100 * time.Second, direction: "in", practice: true},
			},
			wantOpenIn: true, wantAgo: realIn,
			why: "before ADR 0008 the practice row won the LIMIT 1, gather discarded it, and " +
				"the real entry became invisible — the next tap was an `in` and never closed it",
		},
		{
			name: "two practice rows above it (unreachable through the engine, writable in SQL)",
			rows: []chainRow{
				{ago: realIn, direction: "in", practice: false},
				{ago: -200 * time.Second, direction: "in", practice: true},
				{ago: -100 * time.Second, direction: "in", practice: true},
			},
			wantOpenIn: true, wantAgo: realIn,
			why: "a Go-side `skip one practice row` fix would pass the row above and fail here",
		},
		{
			name: "the real check-in was closed: nothing is open beneath the practice row",
			rows: []chainRow{
				{ago: -4 * time.Hour, direction: "in", practice: false},
				{ago: realIn, direction: "out", practice: false},
				{ago: -100 * time.Second, direction: "in", practice: true},
			},
			wantOpenIn: false,
			why: "the POSITIVE CONTROL for `nil`: without it, a query that returned nothing at " +
				"all would satisfy every row above",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			locationID, employeeID := chainFixture(t, data, tenantID, tc.rows)

			var facts tapFacts
			err := data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
				var e error
				facts, e = svc.gather(ctx, tx, Request{
					SessionTenantID: tenantID, EmployeeID: employeeID,
				}, locationID)
				return e
			})
			if err != nil {
				t.Fatalf("gather: %v", err)
			}
			if facts.lastForPerson == nil {
				t.Fatal("the person's history was not read at all: this case is measuring nothing")
			}
			if got := facts.lastOpenIn != nil; got != tc.wantOpenIn {
				t.Fatalf("lastOpenIn present = %v, want %v — %s", got, tc.wantOpenIn, tc.why)
			}
			if !tc.wantOpenIn {
				return
			}
			if facts.lastOpenIn.Practice {
				t.Fatalf("gather handed on a PRACTICE row as the open check-in — %s", tc.why)
			}
			// Which row, not merely "a row": the defect returned a row too (the
			// practice one), so identity is the assertion that separates the arms.
			gap := time.Since(facts.lastOpenIn.OccurredAt) + tc.wantAgo
			if gap < -time.Minute || gap > time.Minute {
				t.Fatalf("the open check-in handed on is %s old, want ~%s — %s",
					time.Since(facts.lastOpenIn.OccurredAt).Round(time.Second), -tc.wantAgo, tc.why)
			}
		})
	}
}
