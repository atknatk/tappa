package fixtures

// daycutoff.go — what makes the DB suite independent of the hour it runs at (M15-04, ADR 0032
// §A.8; the M15 engine lens's M-Y2).
//
// THE PROBLEM, MEASURED. Migration 00064 gives every business a 05:00 cutoff, and the harnesses
// run the orchestrator with the business-day switch ON (production's mode). A test that writes a
// check-in at now − 3 h and expects the next tap to close it is then red between 05:00 and
// 08:00 Malta time and green the rest of the day: the open check-in is of the previous business
// day, so the tap is an `in` (ADR 0032 §A.1). Every test that expects the engine to say `out`
// has such a window — a few seconds wide for two live taps, hours wide for an aged predecessor.
//
// THE FIX IS THE PERSON TIER, ON THE TEST'S OWN EMPLOYEE. GiveFarDayCutoff writes
// employees.day_cutoff — the most specific tier, so it beats the department, the profile branch
// and the business — for an employee the test itself created, with a value whose boundary is far
// from now (FarDayCutoff). It never writes a tenant's or a location's cutoff: the seed tenant
// (TenantKF) is shared by every harness that runs in parallel and by the developer's demo
// database, and a tenant-wide cutoff would move every other test's business day and stay behind
// after the run. A test that exercises the cutoff or the hierarchy ON PURPOSE builds its own
// tenant and branches and writes the value it means.

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// BusinessDaySwitchOn is the business-day switch's value the harnesses run with (ADR 0032 §A.8):
// far in the past, so the switch is on for every tap — production's mode, the one every direction
// test is written against. "Unset" and "the server's clock before the value" are separate, named
// tests, never a harness default. A fresh pointer per call: a test that changes its copy changes
// nobody else's.
func BusinessDaySwitchOn() *time.Time {
	v := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	return &v
}

// FarDayCutoffLead is how far after now the cutoff FarDayCutoff picks lies: 9 h. The window a
// test needs free of a business-day boundary is [now − 12 h, now + 6 h] (the M15-04 card's): the
// oldest predecessor a direction-chain DB test writes relative to now is 10 h
// (TestRecordDB_TheOpenCheckInIsReadAtTheDeclaredInstant, measured 2026-10-21 over the inventory
// in TestFarDayCutoff_EveryDirectionChainTestHasACutoffAwayFromNow), and a test runs for minutes,
// not hours. A boundary 9 h ahead is also one 15 h behind, so it misses the window by ~3 h on each
// side, less the 15-minute rounding and a DST step (1 h in Malta, 30 min in Lord Howe — the zones
// the property test sweeps, every 5 minutes of ordinary and DST days).
const FarDayCutoffLead = 9 * time.Hour

// FarDayCutoff is a person-tier cutoff (time of day, in zone) whose business-day boundary lies
// FarDayCutoffLead after now, rounded to the nearest quarter hour — never inside
// [now − 12 h, now + 6 h]. The value is a minute of the day (migration 00064's CHECK: no seconds,
// below 24:00).
func FarDayCutoff(now time.Time, zone string) (time.Duration, error) {
	loc, err := time.LoadLocation(zone)
	if err != nil || zone == "" {
		return 0, fmt.Errorf("fixtures: zone %q: %v", zone, err)
	}
	wall := now.Add(FarDayCutoffLead).In(loc)
	minutes := wall.Hour()*60 + wall.Minute()
	minutes = (minutes + 7) / 15 * 15 % (24 * 60)
	return time.Duration(minutes) * time.Minute, nil
}

// GiveFarDayCutoff writes FarDayCutoff(now, the tenant's zone) to the employee's own
// day_cutoff, inside tx — a transaction that already carries the tenant's context (SET LOCAL
// app.tenant_id; db.WithTenant gives one). It reads the zone from the tenant row, as the tap
// does, and refuses unless exactly one row changed: a silent miss would leave the test on the
// business's 05:00 and red at five in the morning, which is the failure this exists to remove.
func GiveFarDayCutoff(ctx context.Context, tx pgx.Tx, tenantID, employeeID uuid.UUID, now time.Time) (time.Duration, error) {
	var zone string
	if err := tx.QueryRow(ctx, `SELECT timezone FROM tenants WHERE id = $1`, tenantID).Scan(&zone); err != nil {
		return 0, fmt.Errorf("fixtures: the tenant's zone: %w", err)
	}
	cutoff, err := FarDayCutoff(now, zone)
	if err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `UPDATE employees SET day_cutoff = $3::time WHERE tenant_id = $1 AND id = $2`,
		tenantID, employeeID, fmt.Sprintf("%02d:%02d", int(cutoff.Hours()), int(cutoff.Minutes())%60))
	if err != nil {
		return 0, fmt.Errorf("fixtures: the employee's day cutoff: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return 0, fmt.Errorf("fixtures: the employee's day cutoff changed %d rows, want 1 (wrong tenant context?)", tag.RowsAffected())
	}
	return cutoff, nil
}
