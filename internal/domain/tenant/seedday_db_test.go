package tenant

// seedday_db_test.go -- the seed's two business days as loaded (M15-08, Q30), READ ONLY: the
// shared seed tenants are never written here.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/test/fixtures"
)

// TestDayCutoffDB_TheSeedCutoffsAreQ30s reads the seed businesses' business days back (Q30,
// 2026-10-21: KF 05:00, KM 23:00) and holds them equal to the pair the KM day test drives
// through the engine, and checks both businesses: the business level's value -- the one the
// seed sets -- cuts none of the seed's own venues' or departments' shifts. Only that level and
// the seed's own names are asserted on: other DB tests add venues and people to the seed
// businesses, and give their own people a person-level value far from the hour they run at
// (fixtures.GiveFarDayCutoff) -- values that cut shifts by design and are not the seed's.
func TestDayCutoffDB_TheSeedCutoffsAreQ30s(t *testing.T) {
	f := newCutoffFixture(t, nil)
	seedNames := map[string]bool{}
	for _, s := range []CutoffShift{seedRustyBar, seedKF1022, seedKFPaceville, seedKFHQ, seedKMPlant,
		seedKMGeneral, seedKMMeat, seedKMWarehouse, seedKMKitchen, seedKMPastry} {
		seedNames[s.Owner] = true
	}
	for _, c := range []struct {
		name   string
		tenant uuid.UUID
		want   time.Duration
	}{{"KF", fixtures.TenantKF, seedKFCutoff}, {"KM", fixtures.TenantKM, seedKMCutoff}} {
		var got *time.Duration
		if err := f.data.WithTenant(context.Background(), c.tenant, func(ctx context.Context, tx pgx.Tx) error {
			b, err := store.New(tx).GetDayCutoffBusiness(ctx, c.tenant)
			got = durationOf(b.DayCutoff)
			return err
		}); err != nil {
			t.Fatalf("%s: %v (CI and `make seed` load the seed)", c.name, err)
		}
		if got == nil || *got != c.want {
			t.Fatalf("seed %s's business day is %s, Q30 says %s (a database seeded before M15-08 gets KM's from `make seed`'s backfill)",
				c.name, ClockText(got), ClockText(&c.want))
		}
		chk, err := f.cutoffs.Check(context.Background(), c.tenant, time.Now())
		if err != nil {
			t.Fatalf("%s: Check: %v", c.name, err)
		}
		if len(chk.Levels) == 0 || chk.Levels[0].Level != CutoffBusiness || chk.Levels[0].Value != c.want {
			t.Fatalf("%s: the check's first level is %+v, want the business at %s", c.name, chk.Levels, ClockText(&c.want))
		}
		for _, l := range chk.Levels {
			if l.Level == CutoffPerson {
				continue
			}
			for _, cf := range l.Conflicts {
				if seedNames[cf.Shift.Owner] {
					t.Errorf("seed %s: the %s level's %s cuts the seed's %s", c.name, l.Level, ClockText(&l.Value), cf.Shift.Owner)
				}
			}
		}
	}
}
