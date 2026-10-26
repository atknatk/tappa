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
	// THE INSTANTS ARE THE TEST'S, NOT THE CLOCK'S (M15-17 fix round). This test compared the row
	// with a time.Now() read AFTER the fixture's commit, within a second: on CI (run 40249469328,
	// -race, every package at once) the commit took longer and the right row was reported as the
	// wrong one -- reproduced by putting 1.1 s between the two clocks. The query reads no zone and
	// no business day, so the instant CI ran at (2026-10-26 00:34 UTC, the night Malta left summer
	// time) is a case here, beside the repeated hour itself and the live clock: one fixture each,
	// and every question and answer measured from that fixture's own base.
	for _, base := range []time.Time{
		time.Date(2026, 10, 26, 0, 34, 36, 157720000, time.UTC),
		time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC),
		time.Now().UTC().Truncate(time.Microsecond),
	} {
		locationID, employeeID := chainFixtureAt(t, data, tenantID, base, []chainRow{
			{ago: -10 * time.Hour, direction: "in"},
			{ago: -2 * time.Hour, direction: "out"},
		})
		tests := []struct {
			name       string
			at         time.Time
			wantOpenIn bool
		}{
			{"at base: the entry is closed", base, false},
			{"5 h before: the entry was open -- its out had not happened", base.Add(-5 * time.Hour), true},
			{"11 h before: the entry had not happened", base.Add(-11 * time.Hour), false},
		}
		for _, tc := range tests {
			t.Run(base.Format(time.RFC3339)+"/"+tc.name, func(t *testing.T) {
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
				if want := base.Add(-10 * time.Hour); tc.wantOpenIn && !facts.lastOpenIn.OccurredAt.Equal(want) {
					t.Errorf("handed on the row at %v, want the entry at %v", facts.lastOpenIn.OccurredAt, want)
				}
			})
		}
	}
}
