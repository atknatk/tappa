package checkin

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
)

// TestGatherDB_TheOpenCheckInIsTheOneOpenAtTheTapsInstant pins the CALLER half of
// GetLastOpenTransaction's @at (M15-02, ADR 0032 §A.4; the plan's M-O2: the query shipped without
// its caller turns every tap into an `in`): gather hands the query the instant it is given -- Record's
// occurredAt -- and not a zero value or a clock of its own.
//
// The history is an entry 10 h ago closed 2 h ago. Asked at NOW nothing is open; asked 5 h ago the
// entry is (its out had not happened). A zero At answers "nothing" to both questions and a gather
// that read its own clock answers "nothing" to the second, so only the second arm can tell -- the
// first is the positive control that the fixture and the RLS context are live.
func TestGatherDB_TheOpenCheckInIsTheOneOpenAtTheTapsInstant(t *testing.T) {
	data, tenantID, sets := newPolicyHarness(t)
	trail, err := audit.New(data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	svc := &Service{data: data, trail: trail, policies: sets, log: quietLogger()}
	locationID, employeeID := chainFixture(t, data, tenantID, []chainRow{
		{ago: -10 * time.Hour, direction: "in"},
		{ago: -2 * time.Hour, direction: "out"},
	})
	now := time.Now().UTC()
	tests := []struct {
		name       string
		at         time.Time
		wantOpenIn bool
	}{
		{"at now: the entry is closed", now, false},
		{"5 h ago: the entry was open -- its out had not happened", now.Add(-5 * time.Hour), true},
		{"11 h ago: the entry had not happened", now.Add(-11 * time.Hour), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var facts tapFacts
			err := data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
				var e error
				facts, e = svc.gather(ctx, tx, Request{SessionTenantID: tenantID, EmployeeID: employeeID}, locationID, tc.at)
				return e
			})
			if err != nil {
				t.Fatalf("gather: %v", err)
			}
			if facts.lastForPerson == nil {
				t.Fatal("the person's history was not read at all: this case is measuring nothing")
			}
			if got := facts.lastOpenIn != nil; got != tc.wantOpenIn {
				t.Fatalf("lastOpenIn present = %v, want %v", got, tc.wantOpenIn)
			}
			if tc.wantOpenIn {
				if d := facts.lastOpenIn.OccurredAt.Sub(now.Add(-10 * time.Hour)); d < -time.Second || d > time.Second {
					t.Errorf("handed on the row at %v, want the entry 10 h ago", facts.lastOpenIn.OccurredAt)
				}
			}
		})
	}
}
