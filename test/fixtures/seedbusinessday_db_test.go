package fixtures

// seedbusinessday_db_test.go — seed.sql's KM business-day backfill (M15-08) run against a real
// Postgres, INSIDE A TRANSACTION THAT IS ALWAYS ROLLED BACK: the shared seed tenant KM is put in
// each of the three states the guard tells apart, the statement is executed as seed.sql carries
// it (read from the file, comments stripped the way PostgreSQL strips them), and what it did is
// read back before the rollback. Nothing the test writes survives it, whatever it finds.

import (
	"context"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// kmBackfill is seed.sql's one statement that writes tenants.day_cutoff.
var (
	kmBackfill    = regexp.MustCompile(`(?is)\bupdate\s+tenants\s+set\s+day_cutoff\b.*?;`)
	anyKMBackfill = regexp.MustCompile(`(?i)\bupdate\s+tenants\s+set\s+day_cutoff\b`)
)

// TestSeedSQL_TheKMBusinessDayBackfillTouchesOnlyAnUnsetRow: the backfill turns KM's 05:00 into
// 23:00 only while nobody has set it — (1) 05:00 and no trail row → 23:00; (2) 05:00 and a
// tenant.day_cutoff_updated row of KM's (an owner chose 05:00 on the panel) → stays 05:00;
// (3) 06:00 → untouched. KF is never written. An unconditional UPDATE is red in (2) and (3).
func TestSeedSQL_TheKMBusinessDayBackfillTouchesOnlyAnUnsetRow(t *testing.T) {
	ownerDSN := os.Getenv("DATABASE_MIGRATE_URL")
	if ownerDSN == "" {
		t.Skip("DATABASE_MIGRATE_URL is required (real Postgres; the backfill runs as the seed's role)")
	}
	raw, err := os.ReadFile("seed.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := stripSQLComments(string(raw))
	if n := len(anyKMBackfill.FindAllStringIndex(sql, -1)); n != 1 {
		t.Fatalf("seed.sql has %d `UPDATE tenants SET day_cutoff` statements, want exactly 1", n)
	}
	stmt := kmBackfill.FindString(sql)
	if stmt == "" {
		t.Fatal("seed.sql has no terminated `UPDATE tenants SET day_cutoff … ;`")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	conn, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(context.Background()) }()

	cutoffOf := func(tx pgx.Tx, id string) string {
		t.Helper()
		var v string
		if err := tx.QueryRow(ctx, `SELECT to_char(day_cutoff, 'HH24:MI') FROM tenants WHERE id = $1`, id).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, c := range []struct {
		name  string
		start string
		trail bool
		want  string
	}{
		{"unset default is backfilled", "05:00", false, "23:00"},
		{"an owner's 05:00 is kept", "05:00", true, "05:00"},
		{"another value is untouched", "06:00", false, "06:00"},
	} {
		t.Run(c.name, func(t *testing.T) {
			tx, err := conn.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			// ALWAYS rolled back: the shared seed tenant leaves this test as it came.
			defer func() { _ = tx.Rollback(context.Background()) }()
			if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, TenantKM.String()); err != nil {
				t.Fatal(err)
			}
			kf := cutoffOf(tx, TenantKF.String())
			if _, err := tx.Exec(ctx, `UPDATE tenants SET day_cutoff = $2::time WHERE id = $1`, TenantKM, c.start); err != nil {
				t.Fatal(err)
			}
			if c.trail {
				if _, err := tx.Exec(ctx, `INSERT INTO audit_log (tenant_id, action, target, detail)
				                           VALUES ($1::uuid, 'tenant.day_cutoff_updated', $2::text, '{}')`, TenantKM, TenantKM.String()); err != nil {
					t.Fatal(err)
				}
			}
			var trail int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = 'tenant.day_cutoff_updated'`,
				TenantKM).Scan(&trail); err != nil {
				t.Fatal(err)
			}
			if (trail > 0) != c.trail {
				t.Fatalf("precondition: KM holds %d business-day trail row(s); this case wants trail=%v "+
					"(the shared seed tenant's cutoff was changed on the panel?)", trail, c.trail)
			}
			if _, err := tx.Exec(ctx, stmt); err != nil {
				t.Fatalf("the seed's backfill: %v", err)
			}
			if got := cutoffOf(tx, TenantKM.String()); got != c.want {
				t.Errorf("KM from %s (trail %v): the backfill left %s, want %s", c.start, c.trail, got, c.want)
			}
			if got := cutoffOf(tx, TenantKF.String()); got != kf {
				t.Errorf("the backfill moved KF from %s to %s", kf, got)
			}
		})
	}
}
