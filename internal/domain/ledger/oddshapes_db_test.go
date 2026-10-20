package ledger

// oddshapes_db_test.go -- the RLS half of the Odd shapes reads (M12-22 .. M12-24; the P3 common
// criterion (c), CLAUDE.md §6): tenant A's connection does not see tenant B's rows through the
// SHAPE of each new read, and the probe is written WITHOUT a tenant predicate -- with one, a zero
// would be the WHERE's doing and the test would stay green with RLS switched off (measured, §6).
// The production statements carry the explicit tenant_id as well (§4.5, belt and braces); that
// half is TestPanelQueries_CarryAnExplicitTenantPredicate's.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
}
