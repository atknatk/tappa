package ledger

// people_holding_db_test.go -- the People screen's reads (M12-17 ...) against a holding (ADR 0027,
// K-39), in both directions: KM's staff tapping at KF's venue and KF's at KM's. A record taken at
// a peer's venue is the EMPLOYER's record, so the employer's People screen shows it -- the instant,
// the verdict word and the channel, never the venue's evidence -- and the site's People screen
// does not hold the employer's staff at all.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

func TestPeopleReaders_APartnerVenueTapIsTheEmployersOutcomeOnly(t *testing.T) {
	w := newHWorld(t)
	at := hLocal(t, "2026-09-16", "09:30:00")
	for _, dir := range []struct {
		name           string
		employer, site uuid.UUID
	}{
		{"KM staff at KF", w.km, w.kf},
		{"KF staff at KM", w.kf, w.km},
	} {
		e := w.staff[dir.employer][0]
		w.row(hRow{employer: dir.employer, site: dir.site, employee: e, at: at, verdict: "flag", kind: "in",
			ipMatch: hBool(false), gpsMatch: hBool(true), trust: hInt16(50)})
		w.row(hRow{employer: dir.employer, site: dir.site, employee: e, at: at.Add(time.Hour), verdict: "reject"})

		t.Run(dir.name+"/ListLastTapsForEmployees", func(t *testing.T) {
			s, err := w.r.Roster(w.ctx, dir.employer, RosterFilter{})
			if err != nil {
				t.Fatal(err)
			}
			if s.LastTaps != Measured {
				t.Fatalf("last taps %v", s.LastTaps)
			}
			var found bool
			for _, p := range s.People {
				if p.ID != e {
					continue
				}
				found = true
				if p.LastTap == nil || !p.LastTap.At.Equal(at.Add(time.Hour)) || p.LastTap.Verdict != "reject" || p.LastTap.Channel != "nfc" {
					t.Errorf("the employer's row for its own person: last tap %+v, want the refusal at the partner venue", p.LastTap)
				}
			}
			if !found {
				t.Fatal("the employer's roster does not list its own person")
			}
			// The site's People screen does not hold the employer's staff, and its connection reads
			// none of the employer's records even when handed the employer's ids.
			site, err := w.r.Roster(w.ctx, dir.site, RosterFilter{})
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range site.People {
				if p.ID == e {
					t.Errorf("the site's roster lists the employer's person %s", p.Name)
				}
			}
			var rows []store.ListLastTapsForEmployeesRow
			if err := w.data.WithTenant(context.Background(), dir.site, func(ctx context.Context, tx pgx.Tx) error {
				var err error
				rows, err = store.New(tx).ListLastTapsForEmployees(ctx, store.ListLastTapsForEmployeesParams{TenantID: dir.site, EmployeeIds: []uuid.UUID{e}})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if len(rows) != 0 {
				t.Errorf("the site reads the employer's last tap: %+v", rows)
			}
		})
		t.Run(dir.name+"/ListRecentTapsForEmployee", func(t *testing.T) {
			got := w.r.RecentTaps(w.ctx, dir.employer, e)
			if got.State != Measured || len(got.Taps) != 2 {
				t.Fatalf("the employer's strip: %v with %+v, want both partner-venue records", got.State, got.Taps)
			}
			want := []RecentTap{
				{At: at.Add(time.Hour), Verdict: "reject", Channel: "nfc"},
				{At: at, Direction: "in", Verdict: "flag", Channel: "nfc"},
			}
			for i, w := range want {
				if g := got.Taps[i]; !g.At.Equal(w.At) || g != (RecentTap{At: g.At, Direction: w.Direction, Verdict: w.Verdict, Channel: w.Channel}) {
					t.Errorf("tap %d: %+v, want %+v (day, direction, time and word only)", i, g, w)
				}
			}
			if site := w.r.RecentTaps(w.ctx, dir.site, e); site.State != Measured || len(site.Taps) != 0 {
				t.Errorf("the site reads the employer's person's strip: %+v", site)
			}
		})
	}
}
