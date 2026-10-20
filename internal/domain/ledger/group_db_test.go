package ledger

// group_db_test.go -- GRP-1's ledger half against real Postgres (hWorld: KF and KM in H, written by
// the owner, read on the application pool as tappa_app): the ORDER the two readers of the partner
// projection list in (the Reports week the earliest first, the Group section the latest first, one
// query and one flag) and the one figure the Group section counts of this business's own rows.

import (
	"bytes"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

// groupOrderBase picks the instant the order test's four rows start at: a minute ago, unless a
// Malta week began less than a minute ago -- then the week's first second (or, with under ten
// seconds of the week gone, ten seconds before it), so the four rows (base .. base+2s) sit in ONE
// Reports week and before now, and inside the Group section's 30 days.
func groupOrderBase(t *testing.T, now time.Time) time.Time {
	t.Helper()
	zone, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	local := now.In(zone)
	start := WeekOf(Date{Year: local.Year(), Month: local.Month(), Day: local.Day()}, zone).From
	base := now.Add(-time.Minute).Truncate(time.Second)
	if base.Before(start) {
		if now.Sub(start) > 10*time.Second {
			return start
		}
		return start.Add(-10 * time.Second)
	}
	return base
}

// TestPartnerTaps_ReportsListTheEarliestFirstAndGroupTheLatest pins the projection reader's two
// orders (the GRP-1 review's ORTA: the newest_first flag had no test of its own). KM's staff tap KF's
// plaque four times -- at base, base+1s and base+2s twice (an equal-time pair, told apart by the
// SECONDARY key, transaction_id, ascending in both orders), each by a different employee so the
// listing names the row:
//
//   - KF's Reports week lists them by (occurred_at, transaction_id) ascending;
//   - KF's Group section lists them by occurred_at DESCENDING, the pair still by transaction_id
//     ascending;
//   - with one row past PartnerTapCap (97 older rows), the Group section keeps the NEWEST
//     PartnerTapCap and says More -- the oldest is the one dropped.
//
// Mutants (each red here, measured): report.go passing newestFirst=true; group.go passing false;
// the query's `CASE WHEN newest_first THEN occurred_at END DESC` term removed.
func TestPartnerTaps_ReportsListTheEarliestFirstAndGroupTheLatest(t *testing.T) {
	w := newHWorld(t)
	now := time.Now()
	base := groupOrderBase(t, now)
	staff := []uuid.UUID{w.employee(w.km), w.employee(w.km), w.employee(w.km), w.employee(w.km)}
	names := map[uuid.UUID]string{}
	for _, e := range staff {
		var n string
		if err := w.owner.QueryRow(w.ctx, `SELECT full_name FROM employees WHERE id = $1`, e).Scan(&n); err != nil {
			t.Fatal(err)
		}
		names[e] = n
	}
	at := []time.Time{base, base.Add(time.Second), base.Add(2 * time.Second), base.Add(2 * time.Second)}
	// Written out of order, so neither insertion order nor created_at can stand in for the sort.
	ids := make([]uuid.UUID, 4)
	for _, i := range []int{2, 0, 3, 1} {
		ids[i] = w.row(hRow{employer: w.km, site: w.kf, employee: staff[i], at: at[i], verdict: "ok", kind: "in"})
	}
	lo, hi := 2, 3 // the equal-time pair, the lower transaction_id first
	if bytes.Compare(ids[2][:], ids[3][:]) > 0 {
		lo, hi = 3, 2
	}
	ascending := []string{names[staff[0]], names[staff[1]], names[staff[lo]], names[staff[hi]]}
	descending := []string{names[staff[lo]], names[staff[hi]], names[staff[1]], names[staff[0]]}
	listed := func(p PartnerTaps) []string {
		var out []string
		for _, tp := range p.Taps {
			out = append(out, tp.EmployeeName)
		}
		return out
	}

	local := base.In(w.mustZone(t))
	report, err := w.r.Hours(w.ctx, w.kf, ReportFilter{Day: Date{Year: local.Year(), Month: local.Month(), Day: local.Day()}})
	if err != nil {
		t.Fatal(err)
	}
	if got := listed(report.Partners); !slices.Equal(got, ascending) {
		t.Errorf("Reports lists %v, want %v (occurred_at, then transaction_id, ascending)", got, ascending)
	}
	group, err := w.r.Group(w.ctx, w.kf)
	if err != nil {
		t.Fatal(err)
	}
	if !group.Member {
		t.Fatal("PREMISE: KF is a live member of H")
	}
	if got := listed(group.Partners); !slices.Equal(got, descending) {
		t.Errorf("Group lists %v, want %v (occurred_at descending, then transaction_id ascending)", got, descending)
	}

	// One row past the cap: 97 older rows, a minute apart, the oldest PartnerTapCap+1-4 minutes back.
	older := PartnerTapCap + 1 - len(at)
	filler := w.employee(w.km)
	w.exec(`INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr, trust,
	                                  site_tenant_id, site_location_id, site_tag_uid)
	        SELECT $1, $2, $3::timestamptz - make_interval(mins => g), 'ok', 'nfc', 'in', true, 1000, 70, $4, $5, $6
	        FROM generate_series(1, $7::int) AS g`,
		w.km, filler, base, w.kf, w.venue[w.kf], w.plaque[w.kf], older)
	group, err = w.r.Group(w.ctx, w.kf)
	if err != nil {
		t.Fatal(err)
	}
	if len(group.Partners.Taps) != PartnerTapCap || !group.Partners.More {
		t.Fatalf("Group lists %d (More %v) of %d rows, want %d and More", len(group.Partners.Taps), group.Partners.More, PartnerTapCap+1, PartnerTapCap)
	}
	if got := listed(group.Partners)[:len(descending)]; !slices.Equal(got, descending) {
		t.Errorf("past the cap Group starts with %v, want the newest four %v", got, descending)
	}
	oldest := base.Add(-time.Duration(older) * time.Minute)
	last := group.Partners.Taps[PartnerTapCap-1].At
	if !last.Equal(oldest.Add(time.Minute)) {
		t.Errorf("past the cap Group's last row is at %v, want %v (the oldest, %v, is the one dropped)", last, oldest.Add(time.Minute), oldest)
	}
	for _, tp := range group.Partners.Taps {
		if tp.At.Equal(oldest) {
			t.Errorf("past the cap Group keeps the oldest row (%v)", oldest)
		}
	}
}

// TestGroup_CountsOwnRecordsAtPartnerVenuesAndNothingElse pins CountRecordsAtPartnerVenues' two
// filters (the GRP-1 review's DUSUK-1). In the window KF has: a tap at KM's plaque and a record its
// manager typed for KM's venue (both counted -- site_tenant_id set, not practice); a tap at its OWN
// plaque (site_tenant_id NULL -- not counted); and a training tap at KM's plaque (practice -- not
// counted). The figure is 2. The manual one is also what KM's Group section lists apart, as entered
// by KF ("employer_manual" -- the shared-list sentence says a partner sees it).
//
// Mutants (each red here, measured): `site_tenant_id IS NOT NULL` removed (3); `NOT practice`
// removed (3).
func TestGroup_CountsOwnRecordsAtPartnerVenuesAndNothingElse(t *testing.T) {
	w := newHWorld(t)
	at := time.Now().Add(-time.Hour).Truncate(time.Second)
	e := w.staff[w.kf]
	w.row(hRow{employer: w.kf, site: w.km, employee: e[0], at: at, verdict: "ok", kind: "in"})
	w.row(hRow{employer: w.kf, site: w.km, employee: e[1], at: at, verdict: "ok", kind: "in", channel: "manual"})
	w.exec(`INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, verdict, channel, type, sun_valid, ctr, trust)
	        VALUES ($1, $2, $3, $4, $5, 'ok', 'nfc', 'in', true, 1000, 70)`, w.kf, e[2], w.venue[w.kf], w.plaque[w.kf], at)
	w.exec(`INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr, trust, practice,
	                                  site_tenant_id, site_location_id, site_tag_uid)
	        VALUES ($1, $2, $3, 'ok', 'nfc', 'in', true, 1000, 70, true, $4, $5, $6)`, w.kf, e[2], at, w.km, w.venue[w.km], w.plaque[w.km])

	group, err := w.r.Group(w.ctx, w.kf)
	if err != nil {
		t.Fatal(err)
	}
	if !group.Member {
		t.Fatal("PREMISE: KF is a live member of H")
	}
	if group.AtPartnerVenues != 2 {
		t.Errorf("KF's records at partner venues: %d, want 2 (the tap and the manual record at KM; never its own venue's, never a training tap)", group.AtPartnerVenues)
	}
	site, err := w.r.Group(w.ctx, w.km)
	if err != nil {
		t.Fatal(err)
	}
	if len(site.Partners.Manual) != 1 || site.Partners.Manual[0].Outcome != "employer_manual" {
		t.Errorf("KM's Group section lists %+v apart, want KF's manual record as employer_manual", site.Partners.Manual)
	}
}

// mustZone is the fixture tenants' zone (00008's default).
func (w *hWorld) mustZone(t *testing.T) *time.Location {
	t.Helper()
	zone, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	return zone
}
