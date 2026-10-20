package ledger

// oddshapes_db_test.go -- the RLS half of the Odd shapes reads (M12-22 .. M12-24; the P3 common
// criterion (c), CLAUDE.md §6): tenant A's connection does not see tenant B's rows through the
// SHAPE of each new read, and the probe is written WITHOUT a tenant predicate -- with one, a zero
// would be the WHERE's doing and the test would stay green with RLS switched off (measured, §6).
// The production statements carry the explicit tenant_id as well (§4.5, belt and braces); that
// half is TestPanelQueries_CarryAnExplicitTenantPredicate's.

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
)

// TestRLS_OddShapes_TenantIsolation: B (C in hWorld) has a record of every evidence shape at its
// venue; A (D) has none. Through A's connection the filter-less split counts nothing, through B's
// it counts B's rows (the positive control that makes A's zero mean RLS).
func TestRLS_OddShapes_TenantIsolation(t *testing.T) {
	w := newHWorld(t)
	a, b := w.d, w.c
	venue := uuid.Nil
	if err := w.owner.QueryRow(w.ctx, `INSERT INTO locations (tenant_id, name) VALUES ($1, $2) RETURNING id`,
		b, "rls odd "+uuid.NewString()[:6]).Scan(&venue); err != nil {
		t.Fatal(err)
	}
	plaque := w.tag(b, venue)
	person := w.employee(b)
	at := hLocal(t, "2026-03-04", "09:00:00")
	yes, no := hBool(true), hBool(false)
	for i, s := range []struct{ ip, gps *bool }{{yes, yes}, {yes, no}, {no, yes}, {no, no}} {
		w.exec(`INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr, ip_match, gps_match,
		                                  location_id, tag_uid)
		        VALUES ($1, $2, $3, 'ok', 'nfc', 'in', true, $4, $5, $6, $7, $8)`,
			b, person, at.Add(time.Duration(i)*time.Minute), 1001+i, s.ip, s.gps, venue, plaque)
	}

	// The M12-22 split, as ListAnomalyVenues computes it, with no tenant predicate.
	split := func(tenant uuid.UUID) (records, both, network, distance, neither int64) {
		t.Helper()
		if err := w.data.WithTenant(w.ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				SELECT count(*),
				       count(*) FILTER (WHERE t.ip_match AND t.gps_match),
				       count(*) FILTER (WHERE t.ip_match AND NOT coalesce(t.gps_match, false)),
				       count(*) FILTER (WHERE t.gps_match AND NOT t.ip_match),
				       count(*) FILTER (WHERE (t.ip_match IS NOT NULL OR t.gps_match IS NOT NULL)
				                          AND NOT coalesce(t.ip_match, false) AND NOT coalesce(t.gps_match, false))
				FROM transactions t
				WHERE t.location_id = $1`, venue).Scan(&records, &both, &network, &distance, &neither)
		}); err != nil {
			t.Fatal(err)
		}
		return
	}
	if r, bo, n, d, ne := split(b); r != 4 || bo != 1 || n != 1 || d != 1 || ne != 1 {
		t.Fatalf("CONTROL: B's own connection splits its venue %d/%d/%d/%d of %d, want 1/1/1/1 of 4", bo, n, d, ne, r)
	}
	if r, bo, n, d, ne := split(a); r+bo+n+d+ne != 0 {
		t.Errorf("§4.5 BREACH: A's connection splits B's venue %d/%d/%d/%d of %d records", bo, n, d, ne, r)
	}

	// M12-23: a second person of B's taps 5 s after the first on two days; the pair's local days,
	// as ListTapsTakenTogether builds them (lag over the venue, array_agg of the local date), with
	// no tenant predicate.
	mate := w.employee(b)
	for i, day := range []string{"2026-03-04", "2026-03-05"} {
		w.exec(`INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr, location_id, tag_uid)
		        VALUES ($1, $2, $3, 'ok', 'nfc', 'in', true, $4, $5, $6)`,
			b, mate, hLocal(t, day, "09:00:05"), 1101+i, venue, plaque)
	}
	w.exec(`INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr, location_id, tag_uid)
	        VALUES ($1, $2, $3, 'ok', 'nfc', 'in', true, 1201, $4, $5)`, b, person, hLocal(t, "2026-03-05", "09:00:00"), venue, plaque)
	days := func(tenant uuid.UUID) (n int) {
		t.Helper()
		if err := w.data.WithTenant(w.ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				WITH o AS (
				    SELECT t.employee_id, t.occurred_at,
				           lag(t.employee_id) OVER (PARTITION BY t.location_id ORDER BY t.occurred_at, t.id) AS prev_employee_id,
				           lag(t.occurred_at) OVER (PARTITION BY t.location_id ORDER BY t.occurred_at, t.id) AS prev_occurred_at
				    FROM transactions t
				    WHERE t.location_id = $1
				)
				SELECT coalesce(cardinality(array_agg(DISTINCT (o.occurred_at AT TIME ZONE 'Europe/Malta')::date)), 0)
				FROM o
				WHERE o.prev_employee_id <> o.employee_id AND o.occurred_at - o.prev_occurred_at < interval '20 seconds'`,
				venue).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := days(b); n != 2 {
		t.Fatalf("CONTROL: B's own connection finds the pair on %d local days, want 2", n)
	}
	if n := days(a); n != 0 {
		t.Errorf("§4.5 BREACH: A's connection finds B's pair on %d local days", n)
	}

	// M12-24: B's plaque's counters, as ListAnomalyPlaqueJumps reads them, with no tenant predicate.
	counters := func(tenant uuid.UUID) (n int) {
		t.Helper()
		if err := w.data.WithTenant(w.ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(t.ctr) FROM transactions t WHERE t.tag_uid = $1 AND t.sun_valid`, plaque).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := counters(b); n != 7 {
		t.Fatalf("CONTROL: B's own connection reads %d counters of its plaque, want 7", n)
	}
	if n := counters(a); n != 0 {
		t.Errorf("§4.5 BREACH: A's connection reads %d counters of B's plaque", n)
	}
}

// jumpTap writes one tap of KM's on uid at KM's venue: a verified read (ok, in) with the given
// counter and tap:ctrGap, or -- sun false -- a replay refused (reject, no direction).
func (w *hWorld) jumpTap(uid string, at time.Time, ctr, gap int, sun bool) {
	w.t.Helper()
	verdict, kind := "ok", any("in")
	if !sun {
		verdict, kind = "reject", nil
	}
	w.exec(`INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr, location_id, tag_uid, policy_context)
	        VALUES ($1, $2, $3, $4, 'nfc', $5, $6, $7, $8, $9, jsonb_build_object('tap:ctrGap', $10::int))`,
		w.km, w.staff[w.km][0], at, verdict, kind, sun, ctr, w.venue[w.km], uid, gap)
}

// TestAnomalies_ThePlaqueJumpNamesItsCounters (M12-24): the counters around each listed plaque's
// largest gap, one week of KM's (Mon 9 - Sun 15 February 2026, Malta):
//
//	P  ctr 600 on Fri 6 Feb 22:00 (BEFORE the week), a replay of 600 refused on Mon 9 01:00,
//	   ctr 641 gap 40 on Tue 10 09:30, ctr 645 gap 3 on Wed 11 -> #600 -> #641, between
//	   Fri 22:00 (the verified read, not the later replay) and Tue 09:30
//	Q  ctr 50 gap 7, no earlier read (an installer's reads, an activation tap) -> #42 -> #50,
//	   the earlier read not on record
//	R  ctr 10 on Thu 12 08:00, ctr 20 gap 5 at 09:00 -> #14 -> #20, the earlier time NOT the
//	   08:00 read's (its counter is 10, not 14)
//
// and this week, a KF employee's tap on KM's plaque S revealing a gap of 90 (the projection,
// K-1i) beside KM's own gap of 2 there -> the row's largest gap is the peer's, so its counters
// are "elsewhere". MUTANTS (measured 2026-10-20): `q.sun_valid` dropped -> P's earlier time is the
// replay's; the JumpFrom check dropped -> R carries the 08:00 time; the gap check dropped -> S
// prints KM's own counters beside the peer's 90; the read outside a savepoint -> the page fails
// (TestAnomalies_AFailedCounterReadLeavesThePageStanding) -- each red.
func TestAnomalies_ThePlaqueJumpNamesItsCounters(t *testing.T) {
	w := newHWorld(t)
	p, q, r := w.tag(w.km, w.venue[w.km]), w.tag(w.km, w.venue[w.km]), w.tag(w.km, w.venue[w.km])
	w.jumpTap(p, hLocal(t, "2026-02-06", "22:00:00"), 600, 0, true)
	w.jumpTap(p, hLocal(t, "2026-02-09", "01:00:00"), 600, 0, false)
	w.jumpTap(p, hLocal(t, "2026-02-10", "09:30:00"), 641, 40, true)
	w.jumpTap(p, hLocal(t, "2026-02-11", "09:00:00"), 645, 3, true)
	w.jumpTap(q, hLocal(t, "2026-02-12", "10:00:00"), 50, 7, true)
	w.jumpTap(r, hLocal(t, "2026-02-12", "08:00:00"), 10, 0, true)
	w.jumpTap(r, hLocal(t, "2026-02-12", "09:00:00"), 20, 5, true)
	a, err := w.r.Anomalies(w.ctx, w.km, ReportFilter{Day: Date{Year: 2026, Month: time.February, Day: 11}})
	if err != nil {
		t.Fatal(err)
	}
	plaque := func(uid string) AnomalyPlaque {
		t.Helper()
		for _, pl := range a.Plaques {
			if pl.TagUID == uid {
				return pl
			}
		}
		t.Fatalf("plaque %s is not on the gap list: %+v", uid, a.Plaques)
		return AnomalyPlaque{}
	}
	at := func(pl AnomalyPlaque) string {
		if pl.JumpPreviousAt == nil {
			return "nil"
		}
		return pl.JumpPreviousAt.UTC().Format(time.RFC3339)
	}
	for _, c := range []struct {
		name     string
		uid      string
		from, to int
		at, prev time.Time
	}{
		{"P: the earlier read before the week, the replay skipped", p, 600, 641, hLocal(t, "2026-02-10", "09:30:00"), hLocal(t, "2026-02-06", "22:00:00")},
		{"Q: no earlier read on record", q, 42, 50, hLocal(t, "2026-02-12", "10:00:00"), time.Time{}},
		{"R: the earlier read is not the one at #from", r, 14, 20, hLocal(t, "2026-02-12", "09:00:00"), time.Time{}},
	} {
		pl := plaque(c.uid)
		want := "nil"
		if !c.prev.IsZero() {
			want = c.prev.UTC().Format(time.RFC3339)
		}
		if pl.JumpState != CounterJumpRead || pl.JumpFrom != c.from || pl.JumpTo != c.to || !pl.JumpAt.Equal(c.at) || at(pl) != want {
			t.Errorf("%s: %s #%d -> #%d at %v, earlier %s; want read #%d -> #%d at %v, earlier %s",
				c.name, pl.JumpState, pl.JumpFrom, pl.JumpTo, pl.JumpAt, at(pl), c.from, c.to, c.at, want)
		}
	}

	s := w.tag(w.km, w.venue[w.km])
	w.jumpTap(s, time.Now().Add(-2*time.Minute), 700, 2, true)
	w.exec(`INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr, queued, policy_context,
	                                  site_tenant_id, site_location_id, site_tag_uid)
	        VALUES ($1, $2, now() - interval '1 minute', 'ok', 'nfc', 'in', true, 791, false, '{"tap:ctrGap": 90}', $3, $4, $5)`,
		w.kf, w.staff[w.kf][0], w.km, w.venue[w.km], s)
	a, err = w.r.Anomalies(w.ctx, w.km, ReportFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if pl := plaque(s); pl.LargestGap != 90 || pl.JumpState != CounterJumpElsewhere || pl.JumpTo != 0 || pl.JumpPreviousAt != nil {
		t.Errorf("S, the peer's gap the larger: largest %d, %s #%d -> #%d; want 90 and elsewhere, no counters", pl.LargestGap, pl.JumpState, pl.JumpFrom, pl.JumpTo)
	}
}

// failingTx is a transaction whose ListAnomalyPlaqueJumps statement fails IN POSTGRES (a
// division by zero, so the transaction really is aborted -- a Go error would prove nothing about
// the savepoint); every other statement, and the savepoint it opens, is the real one.
type failingTx struct {
	pgx.Tx
	match string
}

func (f failingTx) Begin(ctx context.Context) (pgx.Tx, error) {
	sp, err := f.Tx.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return failingTx{sp, f.match}, nil
}

func (f failingTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, f.match) {
		return f.Tx.Query(ctx, "SELECT 1/0")
	}
	return f.Tx.Query(ctx, sql, args...)
}

type failingData struct {
	inner Database
	match string
}

func (f failingData) WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error {
	return f.inner.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return fn(ctx, failingTx{tx, f.match})
	})
}

// TestAnomalies_AFailedCounterReadLeavesThePageStanding (M12-24, P3 (f)): the counters' statement
// fails in Postgres -> the page still answers (Queried), the gap list is there with every plaque
// "failed" (never zero counters as if measured), the reads AFTER it in the same transaction still
// ran (the open check-in is listed -- the savepoint cleared the abort), and the failure is logged.
// CONTROL: the same reader without the planted failure reads the counters.
func TestAnomalies_AFailedCounterReadLeavesThePageStanding(t *testing.T) {
	w := newHWorld(t)
	uid := w.tag(w.km, w.venue[w.km])
	w.jumpTap(uid, hLocal(t, "2026-01-13", "09:00:00"), 30, 4, true)
	day := ReportFilter{Day: Date{Year: 2026, Month: time.January, Day: 13}}

	logs := &logCounter{}
	failing, err := NewReader(failingData{inner: w.data, match: "name: ListAnomalyPlaqueJumps"}, slog.New(logs))
	if err != nil {
		t.Fatal(err)
	}
	a, err := failing.Anomalies(w.ctx, w.km, day)
	if err != nil {
		t.Fatalf("a failed counter read failed the page: %v", err)
	}
	if !a.Queried || len(a.Plaques) != 1 || a.Plaques[0].TagUID != uid || a.Plaques[0].LargestGap != 4 {
		t.Fatalf("the page: queried %v, plaques %+v; want the gap list standing", a.Queried, a.Plaques)
	}
	if pl := a.Plaques[0]; pl.JumpState != CounterJumpFailed || pl.JumpTo != 0 || pl.JumpPreviousAt != nil {
		t.Errorf("the counters after a failed read: %s #%d -> #%d; want failed and nothing else", pl.JumpState, pl.JumpFrom, pl.JumpTo)
	}
	if len(a.Open) != 1 {
		t.Errorf("the open list after the failed read holds %d entries, want the 1 check-in -- the transaction stayed aborted", len(a.Open))
	}
	if logs.errors() != 1 {
		t.Errorf("%d errors logged, want 1 -- a swallowed failure is §7's `_ = err`", logs.errors())
	}

	b, err := w.r.Anomalies(w.ctx, w.km, day)
	if err != nil {
		t.Fatal(err)
	}
	if pl := b.Plaques[0]; pl.JumpState != CounterJumpRead || pl.JumpFrom != 25 || pl.JumpTo != 30 {
		t.Errorf("CONTROL: %s #%d -> #%d, want read #25 -> #30", pl.JumpState, pl.JumpFrom, pl.JumpTo)
	}
}
