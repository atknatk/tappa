package tenant

// tapprediction.go -- the tap screen's predicted direction (M15-12; ADR 0032 §B.1, §B.2): the
// word on the button, "Clock in" or "Clock out", and the small "Not right? …" button under it.
//
// 🔴 IT IS THE ENGINE'S RULE ON THE ENGINE'S FACTS, READ BY THE ENGINE'S QUERIES. The person's
// resolved cutoff and zone come from GetEmployeeForTap (the decision's own employee read), the
// open check-in from GetLastOpenTransaction (the decision's own open-entry read, @at = now), the
// switch from Directory.DayCutoff -> tap.SwitchedDayCutoff (the orchestrator's gate) and the
// direction from tap.PredictDirection (resolveDirection's own first step). So the screen cannot
// say one thing by one rule while the record is decided by another; ListOpenCheckIns -- the
// anomaly list's definition of "open" -- is NOT used, because the button says what the next
// tap will be decided as, and the decision reads GetLastOpenTransaction.
//
// THE PREDICTION IS ADVICE (ADR 0032 §B.2): the record is decided at POST time, under the
// person's advisory lock (ADR 0006), and both buttons send an explicit direction (§B.3).

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/store"
)

// errPredictionRollback ends a tap page's transaction when the prediction read failed: the reads
// before it are already in Go values, the transaction is read-only, and a failed statement has
// aborted it, so a rollback loses nothing (errBrandRollback's reasoning).
var errPredictionRollback = errors.New("tenant: prediction read failed; rolling back the page's reads")

// predictTap reads the prediction inside the page's transaction, the session tenant's: nil when
// the person is not active or the business-day switch is off at the server's clock; otherwise
// tap.PredictDirection at that clock over the person's open check-in and resolved cutoff.
//
// THE INSTANT IS d.clock(), THE SERVER'S, and nothing else (security lens F-2): GET /t declares
// no instant, and this function takes none, so neither the switch nor the business day can be
// judged at one a client chose.
//
// THE SWITCH IS ASKED BEFORE THE OPEN CHECK-IN IS READ: with the switch off nothing about the
// person's day is read at all. The person's status is asked first of all -- a deactivated
// person's page offers no direction (ADR 0032 §B.2) and their day is not read either.
func (d *Directory) predictTap(ctx context.Context, q *store.Queries, tenantID, employeeID uuid.UUID) (*tap.Prediction, error) {
	emp, err := q.GetEmployeeForTap(ctx, store.GetEmployeeForTapParams{TenantID: tenantID, EmployeeID: employeeID})
	if err != nil {
		return nil, fmt.Errorf("load the person's day: %w", err)
	}
	if tap.EmployeeStatus(emp.Status) != tap.EmployeeActive || !emp.EffectiveDayCutoff.Valid {
		// NOT NULL through tenants.day_cutoff, so an invalid value is a broken invariant; it is
		// "no business day" for the engine (checkin's gather) and "no prediction" here.
		return nil, nil
	}
	now := d.clock()
	cutoff := d.DayCutoff(now, time.Duration(emp.EffectiveDayCutoff.Microseconds)*time.Microsecond, emp.TenantTimezone)
	if cutoff == nil {
		return nil, nil
	}
	var open *tap.Transaction
	row, err := q.GetLastOpenTransaction(ctx, store.GetLastOpenTransactionParams{
		TenantID: tenantID, EmployeeID: &employeeID, At: now,
	})
	switch {
	case err == nil:
		// The query excludes practice rows (ADR 0008); the flag is carried so the engine's own
		// defence (PredictDirection treats a practice row as no open check-in) reads it too.
		open = &tap.Transaction{ID: row.ID, OccurredAt: row.OccurredAt, Practice: row.Practice, Direction: tap.TypeIn}
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return nil, fmt.Errorf("load the open check-in: %w", err)
	}
	p := tap.PredictDirection(now, open, cutoff)
	return &p, nil
}
