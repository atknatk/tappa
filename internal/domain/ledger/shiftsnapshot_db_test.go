package ledger

// shiftsnapshot_db_test.go -- T152 (migration 00095, ADR 0042) end to end on the real read: a week
// whose check-ins carry a shift snapshot keeps its late count when the venue's hours are edited
// afterwards; a week of rows without one (written before 00095) moves with the edit -- and the
// report counts those arrivals so the screen can say so.
//
// The rows are written as tappa_app through the tap's own statement (store.InsertTransaction) under
// RLS, with the snapshot the tap path writes (internal/domain/checkin's shiftsnapshot DB tests pin
// that the path does). The venue is edited as tappa_app too -- the panel's grant.

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/store"
)

func TestHoursDB_EditingAVenuesShiftDoesNotMoveASnapshottedWeek(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping (real Postgres required). " +
			"Run `make test` -- a bare `go test` skips this silently and proves nothing.")
	}
	ctx := context.Background()
	data, err := db.New(ctx, &config.Config{DatabaseURL: config.NewDSN(dsn)})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)
	zone, err := time.LoadLocation(maltaTenantZone)
	if err != nil {
		t.Fatal(err)
	}
	day := Date{Year: 2026, Month: time.June, Day: 10}
	week := WeekOf(day, zone)
	monday := func(hh, mm int) time.Time {
		return time.Date(week.FirstDay.Year, week.FirstDay.Month, week.FirstDay.Day, hh, mm, 0, 0, zone).UTC()
	}

	tenantID, venue, recorded, old := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	nine, five := pgtype.Time{Microseconds: int64(9 * time.Hour / time.Microsecond), Valid: true},
		pgtype.Time{Microseconds: int64(17 * time.Hour / time.Microsecond), Valid: true}
	src, no, malta := string(tap.ShiftSourceLocation), false, maltaTenantZone
	if err := data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		for _, stmt := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO tenants (id, name, vat_number, business_type, structure, timezone)
			  VALUES ($1, 'T152 Probe Ltd', $2, 'bar', 'single', $3)`, []any{tenantID, "VAT-" + tenantID.String(), maltaTenantZone}},
			{`INSERT INTO locations (id, tenant_id, name, shift_start, shift_end) VALUES ($1, $2, 'T152 Venue', '09:00', '17:00')`,
				[]any{venue, tenantID}},
			{`INSERT INTO employees (id, tenant_id, location_id, full_name, status) VALUES ($1, $2, $3, 'Recorded Worker', 'active'),
			                                                                              ($4, $2, $3, 'Old Worker', 'active')`,
				[]any{recorded, tenantID, venue, old}},
		} {
			if _, e := tx.Exec(ctx, stmt.sql, stmt.args...); e != nil {
				return e
			}
		}
		q := store.New(tx)
		for _, r := range []struct {
			who      uuid.UUID
			kind     string
			at       time.Time
			snapshot bool
		}{
			{recorded, "in", monday(9, 10), true}, {recorded, "out", monday(17, 0), true},
			{old, "in", monday(9, 10), false}, {old, "out", monday(17, 0), false},
		} {
			p := store.InsertTransactionParams{
				TenantID: tenantID, EmployeeID: &r.who, LocationID: &venue, Type: &r.kind,
				OccurredAt: r.at, Verdict: "ok", Channel: "nfc",
			}
			if r.snapshot {
				p.ShiftSource, p.ShiftStart, p.ShiftEnd, p.ShiftOvernight, p.ShiftTimezone = &src, nine, five, &no, &malta
			}
			if _, e := q.InsertTransaction(ctx, p); e != nil {
				return e
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	reader, err := NewReader(data, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	late := func(rep ReportScreen, who uuid.UUID) (int, time.Duration) {
		t.Helper()
		for _, p := range rep.People {
			if p.EmployeeID == who {
				return p.LateShifts, p.LateBy
			}
		}
		t.Fatalf("%s is not in the report", who)
		return 0, 0
	}

	before, err := reader.Hours(ctx, tenantID, ReportFilter{Day: day})
	if err != nil {
		t.Fatalf("Hours: %v", err)
	}
	for _, who := range []uuid.UUID{recorded, old} {
		if n, by := late(before, who); n != 1 || by != 10*time.Minute {
			t.Fatalf("PREMISE before the edit: %s late %d times by %v, want once by 10m against 09:00", who, n, by)
		}
	}
	if before.Totals.ShiftFromToday != 1 {
		t.Errorf("before the edit Totals.ShiftFromToday = %d, want 1 (the old row's check-in)", before.Totals.ShiftFromToday)
	}

	// The venue's hours move to 10:00-18:00 -- an edit after the week was worked.
	if err := data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE locations SET shift_start = '10:00', shift_end = '18:00' WHERE tenant_id = $1 AND id = $2`, tenantID, venue)
		return e
	}); err != nil {
		t.Fatalf("edit the venue's shift: %v", err)
	}
	after, err := reader.Hours(ctx, tenantID, ReportFilter{Day: day})
	if err != nil {
		t.Fatalf("Hours: %v", err)
	}
	if n, by := late(after, recorded); n != 1 || by != 10*time.Minute {
		t.Errorf("the snapshotted check-in after the edit: late %d by %v, want unchanged (once, 10m) -- the record keeps its shift", n, by)
	}
	// CONTROL: the row without a snapshot is measured against today's 10:00, so the same 09:10 is
	// early now -- the edit does reach the read, so the unchanged figure above is the snapshot's.
	if n, by := late(after, old); n != 0 || by != 0 {
		t.Errorf("CONTROL: the old check-in after the edit: late %d by %v, want 0 against today's 10:00", n, by)
	}
	if after.Totals.ShiftFromToday != 1 {
		t.Errorf("after the edit Totals.ShiftFromToday = %d, want 1", after.Totals.ShiftFromToday)
	}
}
