package tenant

// presence_db_test.go -- M16-45 against real Postgres: AppHome's presence comes from the SAME open
// check-in the prediction read (its frozen shift, 00095), the owner's switch (00116) gates every
// use of a shift, the usual shift is the department's, else the profile location's, and a closed
// check-in or a deactivated person has no presence.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/test/fixtures"
)

func TestAppHomeDB_PresenceAndUsualShift(t *testing.T) {
	ctx := context.Background()
	loc, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	f := newPredictionFixture(t, fixtures.BusinessDaySwitchOn())
	f.exec(t, `UPDATE employees SET day_cutoff = '05:00' WHERE tenant_id = $1 AND id = $2`, f.tenantID, f.employeeID)
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, loc)
	f.dir.clock = func() time.Time { return now }
	owner := func(sql string, args ...any) {
		t.Helper()
		if err := ownerTenantTx(t, f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, sql, args...)
			return err
		}); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	home, err := f.dir.AppHome(ctx, f.tenantID, f.employeeID)
	if err != nil {
		t.Fatal(err)
	}
	if home.Presence != nil || home.Shift != nil {
		t.Fatalf("no open check-in, no shift: presence %+v, shift %+v", home.Presence, home.Shift)
	}

	// A 09:00 check-in judged against the venue's 09:00-17:00 (its frozen shift).
	in := time.Date(2026, 11, 23, 9, 0, 0, 0, loc)
	owner(`INSERT INTO transactions (tenant_id, employee_id, location_id, occurred_at, created_at, verdict, channel, type, practice,
	                                 shift_source, shift_start, shift_end, shift_overnight, shift_timezone)
	       VALUES ($1, $2, $3, $4, $4, 'ok', 'nfc', 'in', false, 'location', '09:00', '17:00', false, 'Europe/Malta')`,
		f.tenantID, f.employeeID, f.locationID, in)
	f.exec(t, `UPDATE locations SET shift_start = '10:00', shift_end = '22:00' WHERE tenant_id = $1 AND id = $2`, f.tenantID, f.locationID)

	t.Run("G45-g the switch off: no shift is used", func(t *testing.T) {
		home, err := f.dir.AppHome(ctx, f.tenantID, f.employeeID)
		if err != nil {
			t.Fatal(err)
		}
		p := home.Presence
		if p == nil || !p.InSince.Equal(in) || p.ShiftEndsAt != nil || p.RemindAt == nil ||
			!p.RemindAt.Equal(in.Add(10*time.Hour)) || !p.StaleAt.Equal(time.Date(2026, 11, 24, 3, 0, 0, 0, loc)) {
			t.Fatalf("presence %+v", p)
		}
		if home.Shift != nil {
			t.Fatalf("shift %+v with the switch off", home.Shift)
		}
	})

	owner(`UPDATE tenants SET staff_shift_visible = true WHERE id = $1`, f.tenantID)

	t.Run("G45-a the switch on: the frozen shift's end, the profile venue's usual shift", func(t *testing.T) {
		home, err := f.dir.AppHome(ctx, f.tenantID, f.employeeID)
		if err != nil {
			t.Fatal(err)
		}
		p := home.Presence
		if p == nil || p.ShiftEndsAt == nil || !p.ShiftEndsAt.Equal(time.Date(2026, 11, 23, 17, 0, 0, 0, loc)) ||
			!p.RemindAt.Equal(time.Date(2026, 11, 23, 17, 30, 0, 0, loc)) || !p.ShowUntil.Equal(time.Date(2026, 11, 23, 19, 30, 0, 0, loc)) {
			t.Fatalf("presence %+v", p)
		}
		if s := home.Shift; s == nil || s.Start != 10*time.Hour || s.End != 22*time.Hour || s.Venue == "" {
			t.Fatalf("usual shift %+v, want the profile venue's 10:00-22:00", s)
		}
	})

	t.Run("G45-g the department's shift comes first", func(t *testing.T) {
		dept := uuid.New()
		f.exec(t, `INSERT INTO departments (id, tenant_id, location_id, name, shift_start, shift_end, overnight)
		           VALUES ($1, $2, $3, 'Kitchen', '18:00', '02:00', true)`, dept, f.tenantID, f.locationID)
		f.exec(t, `UPDATE employees SET department_id = $3 WHERE tenant_id = $1 AND id = $2`, f.tenantID, f.employeeID, dept)
		home, err := f.dir.AppHome(ctx, f.tenantID, f.employeeID)
		if err != nil {
			t.Fatal(err)
		}
		if s := home.Shift; s == nil || s.Start != 18*time.Hour || !s.Overnight {
			t.Fatalf("usual shift %+v, want the department's 18:00-02:00", s)
		}
		f.exec(t, `UPDATE employees SET department_id = NULL WHERE tenant_id = $1 AND id = $2`, f.tenantID, f.employeeID)
	})

	t.Run("G45-a after the clock-out there is no presence", func(t *testing.T) {
		owner(`INSERT INTO transactions (tenant_id, employee_id, location_id, occurred_at, created_at, verdict, channel, type, practice)
		       VALUES ($1, $2, $3, $4, $4, 'ok', 'nfc', 'out', false)`, f.tenantID, f.employeeID, f.locationID, in.Add(2*time.Hour))
		home, err := f.dir.AppHome(ctx, f.tenantID, f.employeeID)
		if err != nil {
			t.Fatal(err)
		}
		if home.Presence != nil {
			t.Fatalf("presence %+v after the clock-out", home.Presence)
		}
	})

	t.Run("G45-d a check-in from the previous business day is not at work", func(t *testing.T) {
		owner(`INSERT INTO transactions (tenant_id, employee_id, location_id, occurred_at, created_at, verdict, channel, type, practice)
		       VALUES ($1, $2, $3, $4, $4, 'ok', 'nfc', 'in', false)`, f.tenantID, f.employeeID, f.locationID, in.Add(-20*time.Hour))
		f.dir.clock = func() time.Time { return in.Add(-time.Hour) } // 08:00: the 13:00-yesterday in is from the day before
		home, err := f.dir.AppHome(ctx, f.tenantID, f.employeeID)
		if err != nil {
			t.Fatal(err)
		}
		if home.Presence != nil {
			t.Fatalf("presence %+v for yesterday's open check-in", home.Presence)
		}
	})

	t.Run("G45-f a deactivated person has no presence and no shift", func(t *testing.T) {
		gone := uuid.New()
		f.exec(t, `INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at, activated_at, deactivated_at)
		           VALUES ($1, $2, $3, 'Joseph Camilleri', 'deactivated', now(), now(), now())`, gone, f.tenantID, f.locationID)
		home, err := f.dir.AppHome(ctx, f.tenantID, gone)
		if err != nil {
			t.Fatal(err)
		}
		if home.Presence != nil || home.Shift != nil {
			t.Fatalf("deactivated: presence %+v, shift %+v", home.Presence, home.Shift)
		}
	})
}
