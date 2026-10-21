package ledger

// rollhomes_db_test.go -- M12-21: the Roll's roster read (ListRollEmployees) is a widget read, so
// a failure of it costs the titles and the other-venue marks and NEVER the week's hours. Measured
// the advisories_db_test.go way: a TEMP table shadows `employees` for one transaction with only
// the three columns ListWorkedShiftEvents reads (id, tenant_id, full_name), so the hours read
// succeeds and the roster read fails with 42703 on e.role -- a genuine statement error.

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
)

type shadowEmployeesDB struct{ real *db.DB }

func (d shadowEmployeesDB) WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error {
	return d.real.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`CREATE TEMP TABLE employees (id uuid, tenant_id uuid, full_name text) ON COMMIT DROP`); err != nil {
			return err
		}
		return fn(ctx, tx)
	})
}

func TestHoursDB_AFailedRosterReadCostsTheMarksAndNeverTheHours(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping (real Postgres required). " +
			"Run `make test` -- a bare `go test` skips this silently and proves nothing.")
	}
	ctx := context.Background()
	data, err := db.New(ctx, &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)

	zone, err := time.LoadLocation(maltaTenantZone)
	if err != nil {
		t.Fatal(err)
	}
	day := Date{Year: 2026, Month: time.June, Day: 3}
	week := WeekOf(day, zone)
	tenantID, home, employee := uuid.New(), uuid.New(), uuid.New()
	if err := data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		for _, stmt := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO tenants (id, name, vat_number, business_type, structure, timezone)
			  VALUES ($1, 'Roll Probe Ltd', $2, 'bar', 'single', $3)`, []any{tenantID, "VAT-" + tenantID.String(), maltaTenantZone}},
			{`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'Roll Home')`, []any{home, tenantID}},
			{`INSERT INTO employees (id, tenant_id, location_id, full_name, role, status)
			  VALUES ($1, $2, $3, 'Roll Worker', 'Line Cook', 'active')`, []any{employee, tenantID, home}},
			{`INSERT INTO transactions (tenant_id, employee_id, location_id, type, occurred_at, verdict, channel)
			  VALUES ($1, $2, $3, 'in', $4, 'ok', 'nfc'), ($1, $2, $3, 'out', $5, 'ok', 'nfc')`,
				[]any{tenantID, employee, home, week.From.Add(10 * time.Hour), week.From.Add(18 * time.Hour)}},
		} {
			if _, e := tx.Exec(ctx, stmt.sql, stmt.args...); e != nil {
				return e
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	// CONTROL: on the real database the read answers and the person carries title and home.
	healthy, err := NewReader(data, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	ok, err := healthy.Hours(ctx, tenantID, ReportFilter{Day: day})
	if err != nil {
		t.Fatalf("Hours: %v", err)
	}
	if !ok.Homes.Queried || len(ok.People) != 1 || ok.People[0].Role != "Line Cook" || ok.People[0].HomeVenue != "Roll Home" {
		t.Fatalf("CONTROL: the healthy read gives %+v / %+v; the degraded half below would be vacuous", ok.Homes, ok.People)
	}
	if ok.Totals.Shifts != 1 || ok.Totals.OtherVenue != 0 || ok.Totals.OtherVenueUnknown != 0 {
		t.Fatalf("CONTROL: totals %+v, want one shift judged at home", ok.Totals)
	}

	var logged logCounter
	broken, err := NewReader(shadowEmployeesDB{real: data}, slog.New(&logged))
	if err != nil {
		t.Fatal(err)
	}
	got, err := broken.Hours(ctx, tenantID, ReportFilter{Day: day})
	if err != nil {
		t.Fatalf("Hours returned %v: a failed roster read took the week's hours away -- the savepoint in rollHomes is what stops that", err)
	}
	if !got.Queried || got.Totals.Shifts != 1 || got.Totals.Worked != 8*time.Hour {
		t.Fatalf("the hours did not survive: queried %v, %d shift(s), %v", got.Queried, got.Totals.Shifts, got.Totals.Worked)
	}
	if got.Homes.Queried || !got.Homes.Failed {
		t.Errorf("Homes = %+v, want Failed -- a failed read must not be reported as measured", got.Homes)
	}
	// NEVER A ZERO FOR WHAT WAS NOT MEASURED: the one shift is "not judged", not "at home".
	if got.Totals.OtherVenue != 0 || got.Totals.OtherVenueUnknown != 1 {
		t.Errorf("Totals other %d / not judged %d, want 0 / 1", got.Totals.OtherVenue, got.Totals.OtherVenueUnknown)
	}
	if len(got.People) == 1 && (got.People[0].Role != "" || got.People[0].HomeLocationID != nil) {
		t.Errorf("a person carries a title or home the failed read never returned: %+v", got.People[0])
	}
	if n := logged.errors(); n != 1 {
		t.Errorf("%d error(s) logged, want exactly 1 (§7: a failed read is logged, not swallowed)", n)
	}
}
