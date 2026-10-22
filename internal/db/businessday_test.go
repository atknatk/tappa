package db

// businessday_test.go -- M15-02 (migration 00064, ADR 0032 §A.2, §A.4, §A.5, §A.6): the four
// business-day cutoffs, the frozen business day of a tap, the checkout classification and the two
// tap-path reads that changed with them (GetEmployeeForTap's resolved cutoff, GetLastOpenTransaction's
// @at bound and (occurred_at, id) order).
//
// Every app-side test runs as tappa_app under RLS (appDB) on fixture tenants of its own; nothing
// here commits a row with a business_day_start -- such a row would make 00064's Down refuse for
// every later run (that refusal is itself tested, inside a rolled-back savepoint).

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/atknatk/tappa/internal/store"
)

const bd00064File = "00064_add_the_business_day.sql"

// bdCutoff is a wall-clock cutoff as the driver carries it.
func bdCutoff(h, m int) pgtype.Time {
	return pgtype.Time{Microseconds: int64(time.Duration(h)*time.Hour+time.Duration(m)*time.Minute) / int64(time.Microsecond), Valid: true}
}

func bdClock(c pgtype.Time) string {
	if !c.Valid {
		return "NULL"
	}
	d := time.Duration(c.Microseconds) * time.Microsecond
	return time.Time{}.Add(d).Format("15:04:05.000000")
}

// bdWorld is a tenant of its own with two branches, a department on the FIRST branch and the
// tenant's own cutoff -- committed through tappa_app, so every read below runs under RLS.
type bdWorld struct {
	tenant, branchA, branchB, dept uuid.UUID
}

func bdNewWorld(t *testing.T, d *DB) bdWorld {
	t.Helper()
	w := bdWorld{tenant: uuid.New(), branchA: uuid.New(), branchB: uuid.New(), dept: uuid.New()}
	if err := d.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		for _, s := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			  VALUES ($1, 'm15 business day', $2, 'restaurant', 'multi')`, []any{w.tenant, "VAT-M15-" + w.tenant.String()}},
			{`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'm15 branch A')`, []any{w.branchA, w.tenant}},
			{`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'm15 branch B')`, []any{w.branchB, w.tenant}},
			{`INSERT INTO departments (id, tenant_id, location_id, name) VALUES ($1, $2, $3, 'm15 kitchen')`, []any{w.dept, w.tenant, w.branchA}},
		} {
			if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("business-day world: %v", err)
	}
	return w
}

// bdEmployee commits an active employee. branch nil makes the API-managed shape (K-14: no home
// venue, which needs an external ref); dept nil is no department.
func bdEmployee(t *testing.T, d *DB, w bdWorld, branch, dept *uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	var ref *string
	if branch == nil {
		r := "M15-" + strings.ToUpper(id.String()[:8])
		ref = &r
	}
	if err := m11Exec(t, d, w.tenant, `INSERT INTO employees (id, tenant_id, location_id, department_id, full_name, status, external_ref, invited_at, activated_at)
	                                    VALUES ($1, $2, $3, $4, 'm15 person', 'active', $5, now(), now())`, id, w.tenant, branch, dept, ref); err != nil {
		t.Fatalf("employee: %v", err)
	}
	return id
}

// TestBusinessDayCutoff_TheAppSetsItsOwnCutoffAndNothingThe00024ListKeepsShut is the card's
// criterion "tappa_app updates its own tenant's day_cutoff; the columns 00024 keeps shut stay shut".
// The four setters each write their own row and hand the stored value back; another tenant's row
// is not reached (RLS and the statement's own tenant predicate -- §4.5); and the tenants columns
// the column-level grant of 00024 (and 00063) keeps shut stay shut: 00064's grant named
// day_cutoff, not the table.
func TestBusinessDayCutoff_TheAppSetsItsOwnCutoffAndNothingThe00024ListKeepsShut(t *testing.T) {
	d := appDB(t)
	w, other := bdNewWorld(t, d), bdNewWorld(t, d)
	person := bdEmployee(t, d, w, &w.branchA, nil)

	var def pgtype.Time
	if err := d.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT day_cutoff FROM tenants WHERE id = $1`, w.tenant).Scan(&def)
	}); err != nil {
		t.Fatal(err)
	}
	if bdClock(def) != "05:00:00.000000" {
		t.Errorf("a new tenant's cutoff = %s, want the 05:00 default (ADR 0032 §A.2)", bdClock(def))
	}

	in := func(tenant uuid.UUID, fn func(ctx context.Context, q *store.Queries) error) error {
		return d.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
			return fn(ctx, store.New(tx))
		})
	}
	writes := []struct {
		name string
		run  func(ctx context.Context, q *store.Queries, tenant, id uuid.UUID, c pgtype.Time) (pgtype.Time, error)
		own  uuid.UUID
		peer uuid.UUID
	}{
		{"SetTenantDayCutoff", func(ctx context.Context, q *store.Queries, tenant, _ uuid.UUID, c pgtype.Time) (pgtype.Time, error) {
			r, err := q.SetTenantDayCutoff(ctx, store.SetTenantDayCutoffParams{DayCutoff: c, TenantID: tenant})
			return r.DayCutoff, err
		}, w.tenant, other.tenant},
		{"SetLocationDayCutoff", func(ctx context.Context, q *store.Queries, tenant, id uuid.UUID, c pgtype.Time) (pgtype.Time, error) {
			r, err := q.SetLocationDayCutoff(ctx, store.SetLocationDayCutoffParams{DayCutoff: c, TenantID: tenant, ID: id})
			return r.DayCutoff, err
		}, w.branchA, other.branchA},
		{"SetDepartmentDayCutoff", func(ctx context.Context, q *store.Queries, tenant, id uuid.UUID, c pgtype.Time) (pgtype.Time, error) {
			r, err := q.SetDepartmentDayCutoff(ctx, store.SetDepartmentDayCutoffParams{DayCutoff: c, TenantID: tenant, ID: id})
			return r.DayCutoff, err
		}, w.dept, other.dept},
		{"SetEmployeeDayCutoff", func(ctx context.Context, q *store.Queries, tenant, id uuid.UUID, c pgtype.Time) (pgtype.Time, error) {
			r, err := q.SetEmployeeDayCutoff(ctx, store.SetEmployeeDayCutoffParams{DayCutoff: c, TenantID: tenant, ID: id})
			return r.DayCutoff, err
		}, person, uuid.Nil},
	}
	for _, wr := range writes {
		t.Run(wr.name, func(t *testing.T) {
			var got pgtype.Time
			if err := in(w.tenant, func(ctx context.Context, q *store.Queries) error {
				var e error
				got, e = wr.run(ctx, q, w.tenant, wr.own, bdCutoff(23, 0))
				return e
			}); err != nil {
				t.Fatalf("own row: %v", err)
			}
			if bdClock(got) != "23:00:00.000000" {
				t.Errorf("stored %s, want 23:00", bdClock(got))
			}
			if wr.peer == uuid.Nil {
				return
			}
			// Another tenant's row, named outright and also through this tenant's own id with the
			// other's row id: neither reaches a row (RLS; and the predicate is the second defence).
			// (The tenant setter has no row id -- its row IS the tenant -- so only the first applies.)
			reach := []struct{ tenant, id uuid.UUID }{{other.tenant, wr.peer}}
			if wr.name != "SetTenantDayCutoff" {
				reach = append(reach, struct{ tenant, id uuid.UUID }{w.tenant, wr.peer})
			}
			for _, c := range reach {
				err := in(w.tenant, func(ctx context.Context, q *store.Queries) error {
					_, e := wr.run(ctx, q, c.tenant, c.id, bdCutoff(1, 0))
					return e
				})
				if !errors.Is(err, pgx.ErrNoRows) {
					t.Errorf("tenant %s writing (%s, %s): err %v, want pgx.ErrNoRows", w.tenant, c.tenant, c.id, err)
				}
			}
			var peer pgtype.Time
			table := map[string]string{"SetTenantDayCutoff": "tenants", "SetLocationDayCutoff": "locations", "SetDepartmentDayCutoff": "departments"}[wr.name]
			if err := d.WithTenant(context.Background(), other.tenant, func(ctx context.Context, tx pgx.Tx) error {
				return tx.QueryRow(ctx, `SELECT day_cutoff FROM `+table+` WHERE id = $1`, wr.peer).Scan(&peer)
			}); err != nil {
				t.Fatal(err)
			}
			if want := map[bool]string{true: "05:00:00.000000", false: "NULL"}[table == "tenants"]; bdClock(peer) != want {
				t.Errorf("the other tenant's %s cutoff is %s, want %s untouched", table, bdClock(peer), want)
			}
		})
	}

	// Inherit = NULL on the three lower levels: the setter clears an override.
	if err := in(w.tenant, func(ctx context.Context, q *store.Queries) error {
		r, e := q.SetLocationDayCutoff(ctx, store.SetLocationDayCutoffParams{TenantID: w.tenant, ID: w.branchA})
		if e == nil && r.DayCutoff.Valid {
			t.Errorf("clearing the branch override stored %s, want NULL (inherit)", bdClock(r.DayCutoff))
		}
		return e
	}); err != nil {
		t.Fatalf("clear the branch override: %v", err)
	}

	// The business level never inherits: NOT NULL.
	err := in(w.tenant, func(ctx context.Context, q *store.Queries) error {
		_, e := q.SetTenantDayCutoff(ctx, store.SetTenantDayCutoffParams{TenantID: w.tenant})
		return e
	})
	if code, _ := m11Constraint(err); code != "23502" {
		t.Errorf("a NULL business cutoff answered %v, want 23502 (the business level is the end of the coalesce)", err)
	}

	// 00024's closed columns (and 00063's) stay closed; name is the positive control that the
	// statement shape is a writable one.
	if err := m11Exec(t, d, w.tenant, `UPDATE tenants SET name = 'm15 renamed' WHERE id = $1`, w.tenant); err != nil {
		t.Fatalf("PREMISE: tappa_app renames its own tenant: %v", err)
	}
	for _, col := range []string{"vat_number = 'VAT-X'", "structure = 'single'", "plan = 'pro'", "vat_verified = true",
		"vat_checked_at = now()", "api_enabled_at = now()", "api_code = 'm15x'", "suspended_at = now()", "suspension_reason = 'other'",
		"price_per_employee_month = 0", "timezone = 'Europe/Malta', vat_number = 'VAT-Y'"} {
		err := m11Exec(t, d, w.tenant, `UPDATE tenants SET `+col+` WHERE id = $1`, w.tenant)
		if code, _ := m11Constraint(err); code != "42501" {
			t.Errorf("UPDATE tenants SET %s: %v, want 42501 (00024's column list; 00064 granted day_cutoff alone)", col, err)
		}
	}
}

// TestBusinessDayCutoff_ASecondOrTwentyFourHundredIsRefused is the card's criterion "'24:00' and a
// value with seconds are refused", on each of the four levels by its own CHECK; the two ends of the day that ARE
// a cutoff (00:00, 23:59) are the positive controls.
func TestBusinessDayCutoff_ASecondOrTwentyFourHundredIsRefused(t *testing.T) {
	d := appDB(t)
	w := bdNewWorld(t, d)
	person := bdEmployee(t, d, w, &w.branchA, nil)
	levels := []struct{ table, constraint, where string }{
		{"tenants", "tenants_day_cutoff_is_a_minute_of_the_day", "id = $2"},
		{"locations", "locations_day_cutoff_is_a_minute_of_the_day", "tenant_id = $3 AND id = $2"},
		{"departments", "departments_day_cutoff_is_a_minute_of_the_day", "tenant_id = $3 AND id = $2"},
		{"employees", "employees_day_cutoff_is_a_minute_of_the_day", "tenant_id = $3 AND id = $2"},
	}
	ids := map[string]uuid.UUID{"tenants": w.tenant, "locations": w.branchA, "departments": w.dept, "employees": person}
	for _, l := range levels {
		for _, c := range []struct {
			value string
			ok    bool
		}{
			{"00:00", true}, {"23:59", true}, {"05:00", true},
			{"24:00", false}, {"24:00:00", false}, {"05:00:30", false}, {"05:00:00.5", false}, {"23:59:59.999999", false},
		} {
			args := []any{c.value, ids[l.table]}
			if l.table != "tenants" {
				args = append(args, w.tenant)
			}
			err := m11Exec(t, d, w.tenant, `UPDATE `+l.table+` SET day_cutoff = $1::time WHERE `+l.where, args...)
			code, name := m11Constraint(err)
			switch {
			case c.ok && err != nil:
				t.Errorf("%s := %s refused: %v", l.table, c.value, err)
			case !c.ok && (code != "23514" || name != l.constraint):
				t.Errorf("%s := %s answered %v, want 23514 on %s", l.table, c.value, err, l.constraint)
			}
		}
	}
}

// TestGetEmployeeForTap_TheCutoffIsTheMostSpecificLevel is the card's hierarchy test (ADR 0032 §A.2,
// K1): person > department > PROFILE branch > business; a person with no profile branch skips the
// branch level; a department whose own branch is not the person's profile branch still wins.
func TestGetEmployeeForTap_TheCutoffIsTheMostSpecificLevel(t *testing.T) {
	d := appDB(t)
	w := bdNewWorld(t, d)
	// Business 04:45 (not the 05:00 default, so the business level is seen being READ), branch A
	// (the department's own) none, branch B 03:00, the department 14:00.
	for _, s := range []struct {
		sql  string
		args []any
	}{
		{`UPDATE tenants SET day_cutoff = '04:45' WHERE id = $1`, []any{w.tenant}},
		{`UPDATE locations SET day_cutoff = '03:00' WHERE tenant_id = $1 AND id = $2`, []any{w.tenant, w.branchB}},
		{`UPDATE departments SET day_cutoff = '14:00' WHERE tenant_id = $1 AND id = $2`, []any{w.tenant, w.dept}},
	} {
		if err := m11Exec(t, d, w.tenant, s.sql, s.args...); err != nil {
			t.Fatal(err)
		}
	}
	// A second department, with no cutoff of its own, on branch B.
	inheriting := uuid.New()
	if err := m11Exec(t, d, w.tenant, `INSERT INTO departments (id, tenant_id, location_id, name) VALUES ($1, $2, $3, 'm15 bar')`,
		inheriting, w.tenant, w.branchB); err != nil {
		t.Fatal(err)
	}
	ptr := func(u uuid.UUID) *uuid.UUID { return &u }
	tests := []struct {
		name           string
		branch, dept   *uuid.UUID
		personOverride string
		want           string
	}{
		{"the person beats every level", ptr(w.branchB), ptr(w.dept), "07:30", "07:30:00.000000"},
		{"the department beats the profile branch -- and the department's own branch is A, the profile branch B",
			ptr(w.branchB), ptr(w.dept), "", "14:00:00.000000"},
		{"a department with no cutoff inherits: the profile branch", ptr(w.branchB), ptr(inheriting), "", "03:00:00.000000"},
		// The branch level is the PROFILE branch, never the department's: profile A (no cutoff), the
		// inheriting department on B (03:00) -> the business, not B. A join through
		// coalesce(d.location_id, e.location_id) would answer 03:00 here (measured, M15-02 review).
		{"the branch level is the profile branch, not the department's branch", ptr(w.branchA), ptr(inheriting), "", "04:45:00.000000"},
		{"no department: the profile branch beats the business", ptr(w.branchB), nil, "", "03:00:00.000000"},
		{"a profile branch with no cutoff inherits: the business", ptr(w.branchA), nil, "", "04:45:00.000000"},
		{"no profile branch (K-14): the branch level is skipped -- the business", nil, nil, "", "04:45:00.000000"},
		{"no profile branch, a department: the department", nil, ptr(w.dept), "", "14:00:00.000000"},
		{"no profile branch, the person: the person", nil, nil, "22:15", "22:15:00.000000"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id := bdEmployee(t, d, w, tc.branch, tc.dept)
			if tc.personOverride != "" {
				if err := m11Exec(t, d, w.tenant, `UPDATE employees SET day_cutoff = $1::time WHERE tenant_id = $2 AND id = $3`,
					tc.personOverride, w.tenant, id); err != nil {
					t.Fatal(err)
				}
			}
			var row store.GetEmployeeForTapRow
			if err := d.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
				var e error
				row, e = store.New(tx).GetEmployeeForTap(ctx, store.GetEmployeeForTapParams{TenantID: w.tenant, EmployeeID: id})
				return e
			}); err != nil {
				t.Fatalf("GetEmployeeForTap: %v", err)
			}
			if got := bdClock(row.EffectiveDayCutoff); got != tc.want {
				t.Errorf("effective cutoff %s, want %s", got, tc.want)
			}
			// The joins add a column, never a row or a different person: the location the
			// cross-location fact reads is still the profile branch.
			if (row.LocationID == nil) != (tc.branch == nil) || (row.LocationID != nil && *row.LocationID != *tc.branch) {
				t.Errorf("location_id %v, want the profile branch %v", row.LocationID, tc.branch)
			}
		})
	}
}

// bdOrdered returns two fresh ids with lo < hi in PostgreSQL's uuid order (memcmp of the 16 bytes).
func bdOrdered() (lo, hi uuid.UUID) {
	a, b := uuid.New(), uuid.New()
	if bytes.Compare(a[:], b[:]) > 0 {
		a, b = b, a
	}
	return a, b
}

// bdRow is one fixture row of a person's chain.
type bdRow struct {
	id        uuid.UUID
	direction string
	at        time.Time
}

func bdChain(t *testing.T, d *DB, w bdWorld, rows []bdRow) uuid.UUID {
	t.Helper()
	person := bdEmployee(t, d, w, &w.branchA, nil)
	if err := d.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		for _, r := range rows {
			if _, err := tx.Exec(ctx, `INSERT INTO transactions (id, tenant_id, employee_id, location_id, type, occurred_at, verdict, channel)
			                           VALUES ($1, $2, $3, $4, $5, $6, 'ok', 'nfc')`, r.id, w.tenant, person, w.branchA, r.direction, r.at); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("chain: %v", err)
	}
	return person
}

func bdLastOpen(t *testing.T, d *DB, w bdWorld, person uuid.UUID, at time.Time) (uuid.UUID, bool) {
	t.Helper()
	var row store.Transaction
	err := d.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		row, e = store.New(tx).GetLastOpenTransaction(ctx, store.GetLastOpenTransactionParams{TenantID: w.tenant, EmployeeID: &person, At: at})
		return e
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return uuid.Nil, false
	case err != nil:
		t.Fatalf("GetLastOpenTransaction: %v", err)
	}
	if row.BusinessDayStart != nil {
		t.Errorf("a fixture row came back with business_day_start %v; nothing writes one before M15-04", row.BusinessDayStart)
	}
	return row.ID, true
}

// TestGetLastOpenTransaction_TheCheckInOpenAtTheInstant is the card's @at criterion (ADR 0032 §A.4):
// the open check-in is the one open AT @at -- a later out has not closed it yet, a later in had not
// happened -- and @at = now answers what the query answered before (TestGatherDB_ pins the rest).
func TestGetLastOpenTransaction_TheCheckInOpenAtTheInstant(t *testing.T) {
	d := appDB(t)
	w := bdNewWorld(t, d)
	now := time.Now().UTC().Truncate(time.Microsecond)
	mon, monOut, tue := uuid.New(), uuid.New(), uuid.New()
	person := bdChain(t, d, w, []bdRow{
		{mon, "in", now.Add(-30 * time.Hour)},
		{monOut, "out", now.Add(-22 * time.Hour)},
		{tue, "in", now.Add(-2 * time.Hour)},
	})
	tests := []struct {
		name string
		at   time.Time
		want uuid.UUID // uuid.Nil: none
	}{
		{"now: the newest entry, as before @at", now, tue},
		{"between the out and the next entry: nothing is open", now.Add(-10 * time.Hour), uuid.Nil},
		{"between the first entry and its out: the first entry (the later out had not happened)", now.Add(-25 * time.Hour), mon},
		{"exactly at the out: it closes (<= @at)", now.Add(-22 * time.Hour), uuid.Nil},
		{"exactly at the entry: it is open (<= @at)", now.Add(-30 * time.Hour), mon},
		{"before every row: nothing", now.Add(-31 * time.Hour), uuid.Nil},
		{"the zero instant: nothing -- why the caller must pass it", time.Time{}, uuid.Nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := bdLastOpen(t, d, w, person, tc.at)
			if ok != (tc.want != uuid.Nil) || got != tc.want {
				t.Errorf("open at %v = %v (found %v), want %v", tc.at, got, ok, tc.want)
			}
		})
	}
}

// TestGetLastOpenTransaction_SameInstantRowsAgreeWithTheClassification is the card's criterion "two
// rows at the same occurred_at give, in id order, the classification's answer": an in and an out at ONE
// instant are ordered by id in both readers. The classification's answer is computed the way its
// callers will (M15-05): P is the out's predecessor in (occurred_at, id) order, handed to
// tappa_checkout_class. The query says "open" exactly when that out does not close the in.
func TestGetLastOpenTransaction_SameInstantRowsAgreeWithTheClassification(t *testing.T) {
	d := appDB(t)
	w := bdNewWorld(t, d)
	x := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Microsecond)
	lo, hi := bdOrdered()
	tests := []struct {
		name         string
		in, out      uuid.UUID
		wantOpen     bool
		wantOutClass string
	}{
		{"the out's id is the larger: the out is later and closes the in", lo, hi, false, "closes"},
		{"the in's id is the larger: the in is later and stays open; the out has no in before it", hi, lo, true, "unanchored"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Fresh ids per arm (the PK is global), in the arm's order.
			a, b := bdOrdered()
			ids := map[uuid.UUID]uuid.UUID{lo: a, hi: b}
			in, out := ids[tc.in], ids[tc.out]
			person := bdChain(t, d, w, []bdRow{{in, "in", x}, {out, "out", x}})

			got, open := bdLastOpen(t, d, w, person, x.Add(time.Hour))
			if open != tc.wantOpen || (open && got != in) {
				t.Errorf("GetLastOpenTransaction: open=%v row=%v, want open=%v", open, got, tc.wantOpen)
			}
			var class string
			if err := d.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
				return tx.QueryRow(ctx, `
					SELECT public.tappa_checkout_class(
					           CASE WHEN p.type = 'in' THEN p.occurred_at END, p.type = 'out',
					           false, false, o.occurred_at, o.business_day_start)
					  FROM transactions o
					  LEFT JOIN LATERAL (
					      SELECT x.type, x.occurred_at FROM transactions x
					       WHERE x.tenant_id = o.tenant_id AND x.employee_id = o.employee_id
					         AND x.type IS NOT NULL AND NOT x.practice
					         AND (x.occurred_at, x.id) < (o.occurred_at, o.id)
					       ORDER BY x.occurred_at DESC, x.id DESC LIMIT 1) p ON true
					 WHERE o.tenant_id = $1 AND o.id = $2`, w.tenant, out).Scan(&class)
			}); err != nil {
				t.Fatalf("classify: %v", err)
			}
			if class != tc.wantOutClass {
				t.Errorf("the out's class = %q, want %q", class, tc.wantOutClass)
			}
			if (class == "closes") == open {
				t.Errorf("the two readers disagree: the out is %q and the query says open=%v", class, open)
			}
		})
	}

	// Two entries at one instant and no out: the larger id is the later one, so it is the open one --
	// whichever was written first (both orders: without the id in the ORDER BY the answer is
	// whatever the plan meets first, and one of the two orders shows it).
	for _, largerFirst := range []bool{true, false} {
		a, b := bdOrdered()
		rows := []bdRow{{a, "in", x}, {b, "in", x}}
		if largerFirst {
			rows[0], rows[1] = rows[1], rows[0]
		}
		person := bdChain(t, d, w, rows)
		if got, ok := bdLastOpen(t, d, w, person, x.Add(time.Hour)); !ok || got != b {
			t.Errorf("two same-instant entries (larger id written first: %v): %v (found %v), want the larger id %v", largerFirst, got, ok, b)
		}
	}
}

// TestCheckoutClass_SixClassesAndTheirBounds is the card's criterion "the six classes of the
// classification function and their bounds, one DB case each" (ADR 0032 §A.6), called as tappa_app -- the role 00064 grants
// EXECUTE to. Parity with the Go function is M15-05's.
func TestCheckoutClass_SixClassesAndTheirBounds(t *testing.T) {
	d := appDB(t)
	w := bdNewWorld(t, d)
	s := time.Date(2026, 10, 19, 3, 0, 0, 0, time.UTC) // a business day start (05:00 Malta, CEST)
	at := func(h float64) *time.Time { v := s.Add(time.Duration(h * float64(time.Hour))); return &v }
	b := func(v bool) *bool { return &v }
	const us = time.Microsecond
	tests := []struct {
		name            string
		prevIn          *time.Time
		prevIsOut       *bool
		dayHasIn, wrote *bool
		out, outDay     *time.Time
		want            string // "" = NULL
	}{
		// closes
		{"closes: an in of the same business day, 8 h", at(1), b(false), b(true), b(false), at(9), &s, "closes"},
		{"closes: the in AT the day start (the boundary is the new day's)", &s, b(false), b(true), b(false), at(8), &s, "closes"},
		{"closes: exactly 18 h", at(1), b(false), b(true), b(false), at(19), &s, "closes"},
		{"closes: no business day on the out (pre-M15 / manual), 18 h", at(-20), b(false), nil, nil, at(-2), nil, "closes"},
		// (a) too_long
		{"(a) too_long: 18 h + 1 µs", at(1), b(false), b(true), b(false), func() *time.Time { v := s.Add(19*time.Hour + us); return &v }(), &s, "too_long"},
		{"(a) too_long: the historical stale shape, S NULL, 24 h", at(-24), b(false), nil, nil, &s, nil, "too_long"},
		// (b) other_day, and a AND b -> b
		{"(b) other_day: the in 1 µs before the day start", func() *time.Time { v := s.Add(-us); return &v }(), b(false), b(false), b(false), at(1), &s, "other_day"},
		{"(b) other_day: a AND b -> b (previous day and 20 h)", at(-10), b(false), b(false), b(false), at(10), &s, "other_day"},
		// (c) orphan
		{"(c) orphan: no event before it, S set", nil, b(false), b(false), b(false), at(2), &s, "orphan"},
		{"(c) orphan: an out before it, no in of its day (I false), W true", nil, b(true), b(false), b(true), at(2), &s, "orphan"},
		{"(c) orphan: the break shape -- an out before it, I true, W false", nil, b(true), b(true), b(false), at(8), &s, "orphan"},
		{"(c) orphan: P absent with I and W (only an out before it makes (e))", nil, b(false), b(true), b(true), at(8), &s, "orphan"},
		{"(c) orphan: NULL booleans count as false", nil, nil, nil, nil, at(2), &s, "orphan"},
		// (e) extra
		{"(e) extra: an out before it, I, W, S set (the manager's correction)", nil, b(true), b(true), b(true), at(11), &s, "extra"},
		// (d) unanchored
		{"(d) unanchored: no event before it, S NULL", nil, b(false), nil, nil, at(2), nil, "unanchored"},
		{"(d) unanchored: S NULL is read before (e)", nil, b(true), b(true), b(true), at(2), nil, "unanchored"},
		// no instant
		{"no out instant: NULL, never 'closes' by a NULL comparison", at(1), b(false), b(true), b(false), nil, &s, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got *string
			if err := d.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
				return tx.QueryRow(ctx, `SELECT public.tappa_checkout_class($1, $2, $3, $4, $5, $6)`,
					tc.prevIn, tc.prevIsOut, tc.dayHasIn, tc.wrote, tc.out, tc.outDay).Scan(&got)
			}); err != nil {
				t.Fatalf("tappa_checkout_class: %v", err)
			}
			g := ""
			if got != nil {
				g = *got
			}
			if g != tc.want {
				t.Errorf("class %q, want %q", g, tc.want)
			}
		})
	}
}

// TestBusinessDayStart_TheCheckAdmitsNullAndRefusesItOnAnUndirectedRow pins the second safety
// (ADR 0032 §A.5): a business day on a row with no direction is refused 23514 by
// transactions_business_day_needs_a_direction; on a directed row, and NULL anywhere, it passes.
// Every write is rolled back (nothing with a business day is committed -- 00064's Down).
func TestBusinessDayStart_TheCheckAdmitsNullAndRefusesItOnAnUndirectedRow(t *testing.T) {
	d := appDB(t)
	w := bdNewWorld(t, d)
	person := bdEmployee(t, d, w, &w.branchA, nil)
	rollback := errors.New("roll back")
	tests := []struct {
		name      string
		verdict   string
		direction *string
		day       *time.Time
		want      string // "" ok, else the SQLSTATE
	}{
		{"a directed row with a business day", "ok", ptrStr("in"), ptrTime(time.Now().Add(-time.Hour)), ""},
		{"a directed row without one (every row before M15)", "ok", ptrStr("out"), nil, ""},
		{"an undirected refusal without one", "reject", nil, nil, ""},
		{"an undirected refusal WITH one", "reject", nil, ptrTime(time.Now().Add(-time.Hour)), "23514"},
		{"an ignored tap WITH one", "ignored", nil, ptrTime(time.Now().Add(-time.Hour)), "23514"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := d.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
				_, e := store.New(tx).InsertTransaction(ctx, store.InsertTransactionParams{
					TenantID: w.tenant, EmployeeID: &person, LocationID: &w.branchA, Type: tc.direction,
					OccurredAt: time.Now().UTC(), Verdict: tc.verdict, Channel: "nfc", BusinessDayStart: tc.day,
				})
				if e != nil {
					return e
				}
				return rollback
			})
			code, name := m11Constraint(err)
			switch {
			case tc.want == "" && !errors.Is(err, rollback):
				t.Errorf("refused: %v", err)
			case tc.want != "" && (code != tc.want || name != "transactions_business_day_needs_a_direction"):
				t.Errorf("answered %v, want %s on transactions_business_day_needs_a_direction", err, tc.want)
			}
		})
	}
}

func ptrStr(s string) *string        { return &s }
func ptrTime(v time.Time) *time.Time { v = v.UTC(); return &v }

// bdSurface is what 00064 adds, read from the catalogue.
type bdSurface struct {
	columns, constraints, function, tenantsUpdate string
}

func bdRead(t *testing.T, ctx context.Context, q opQuerier) bdSurface {
	t.Helper()
	var s bdSurface
	for dst, sql := range map[*string]string{
		&s.columns: `
			SELECT coalesce(string_agg(a.attrelid::regclass::text || '.' || a.attname || ' ' || format_type(a.atttypid, a.atttypmod)
			                           || ' notnull=' || a.attnotnull || ' default=' || coalesce(pg_get_expr(ad.adbin, ad.adrelid), '-'),
			                           ', ' ORDER BY a.attrelid::regclass::text, a.attname), '')
			  FROM pg_attribute a
			  LEFT JOIN pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
			 WHERE a.attrelid IN ('public.tenants'::regclass, 'public.locations'::regclass, 'public.departments'::regclass,
			                      'public.employees'::regclass, 'public.transactions'::regclass)
			   AND a.attname IN ('day_cutoff', 'business_day_start') AND a.attnum > 0 AND NOT a.attisdropped`,
		&s.constraints: `
			SELECT coalesce(string_agg(conrelid::regclass::text || '.' || conname || ' validated=' || convalidated || ' ' || pg_get_constraintdef(oid),
			                           ' | ' ORDER BY conname), '')
			  FROM pg_constraint
			 WHERE conname IN ('tenants_day_cutoff_is_a_minute_of_the_day', 'locations_day_cutoff_is_a_minute_of_the_day',
			                   'departments_day_cutoff_is_a_minute_of_the_day', 'employees_day_cutoff_is_a_minute_of_the_day',
			                   'transactions_business_day_needs_a_direction')`,
		&s.function: `
			SELECT coalesce((SELECT p.oid::regprocedure::text || ' vol=' || p.provolatile::text || ' par=' || p.proparallel::text || ' definer=' || p.prosecdef
			                        || ' config=' || coalesce(array_to_string(p.proconfig, ','), '') || ' acl=' || coalesce(array_to_string(p.proacl, ','), 'default')
			                        || ' ret=' || p.prorettype::regtype::text || ' src=' || md5(p.prosrc)
			                   FROM pg_proc p WHERE p.proname = 'tappa_checkout_class' AND p.pronamespace = 'public'::regnamespace), '')`,
	} {
		if err := q.QueryRow(ctx, sql).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	s.tenantsUpdate = opColumns(t, ctx, q, "tappa_app", "tenants", "UPDATE")
	return s
}

// TestBusinessDay00064_DownRefusesAFrozenDayAndOtherwiseGivesBack00063 is the card's Down criterion
// (ADR 0032 "Geri alma"; the 00043 precedent): with a row carrying a business_day_start the Down is
// 55000 naming 00064 and changes nothing; without one it takes back the five columns, the five
// CHECKs, the function and the column grant -- tappa_app's UPDATE on tenants is 00024's list again
// -- and the Up takes them again to the same catalogue. Also read: the transactions CHECK is NOT VALID
// (00066 validates it) and the four cutoff CHECKs are validated.
//
// The walk runs in this test's transaction (rolled back), with the tables the Down names locked
// first (lockForDowns -- the 40P01 class ADR 0027's API-1b note measured), and the Down may take no
// strong lock outside them.
func TestBusinessDay00064_DownRefusesAFrozenDayAndOtherwiseGivesBack00063(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, bd00064File)
	tables := downTables(down)
	if strings.Join(tables, ",") == "" || !strings.Contains(strings.Join(tables, ","), "transactions") {
		t.Fatalf("PREMISE: downTables found %v in 00064's Down", tables)
	}
	lockForDowns(t, ctx, tx, tables)

	head := bdRead(t, ctx, tx)
	for _, want := range []string{
		"departments.day_cutoff time without time zone notnull=false default=-",
		"employees.day_cutoff time without time zone notnull=false default=-",
		"locations.day_cutoff time without time zone notnull=false default=-",
		"tenants.day_cutoff time without time zone notnull=true default='05:00:00'::time without time zone",
		"transactions.business_day_start timestamp with time zone notnull=false default=-",
	} {
		if !strings.Contains(head.columns, want) {
			t.Errorf("PREMISE at HEAD: columns %q lack %q", head.columns, want)
		}
	}
	if strings.Count(head.constraints, "validated=true") != 4 ||
		!strings.Contains(head.constraints, "transactions.transactions_business_day_needs_a_direction validated=false CHECK (((business_day_start IS NULL) OR (type IS NOT NULL))) NOT VALID") {
		t.Errorf("PREMISE at HEAD: constraints %q -- want the four cutoff CHECKs validated and the transactions CHECK NOT VALID", head.constraints)
	}
	if !strings.Contains(head.function, "vol=i par=s definer=false config=search_path=pg_catalog, pg_temp acl=tappa_owner=X/tappa_owner,tappa_app=X/tappa_owner ret=text") {
		t.Errorf("PREMISE at HEAD: function %q", head.function)
	}
	if head.tenantsUpdate != "name,business_type,timezone,day_cutoff" {
		t.Errorf("PREMISE at HEAD: tappa_app UPDATE on tenants = %q", head.tenantsUpdate)
	}

	savepoint := func(body func(sp pgx.Tx)) {
		t.Helper()
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := sp.Rollback(ctx); err != nil {
				t.Fatalf("rollback to savepoint: %v", err)
			}
		}()
		body(sp)
	}

	t.Run("a row with a business day: the Down refuses and changes nothing", func(t *testing.T) {
		savepoint(func(sp pgx.Tx) {
			tenant, venue, person := uuid.New(), uuid.New(), uuid.New()
			for _, s := range []struct {
				sql  string
				args []any
			}{
				{`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'm15 down', $2, 'restaurant', 'single')`,
					[]any{tenant, "VAT-M15D-" + tenant.String()}},
				{`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'm15 down venue')`, []any{venue, tenant}},
				{`INSERT INTO employees (id, tenant_id, location_id, full_name, status) VALUES ($1, $2, $3, 'm15 down person', 'active')`,
					[]any{person, tenant, venue}},
				// punch_feed (00049) asks the write to run in its tenant's context, as every tap does.
				{`SELECT set_config('app.tenant_id', $1::text, true)`, []any{tenant}},
				{`INSERT INTO transactions (tenant_id, employee_id, location_id, type, occurred_at, verdict, channel, business_day_start)
				  VALUES ($1, $2, $3, 'in', now(), 'ok', 'nfc', now() - interval '2 hours')`, []any{tenant, person, venue}},
			} {
				if _, err := sp.Exec(ctx, s.sql, s.args...); err != nil {
					t.Fatalf("fixture: %v", err)
				}
			}
			nested, err := sp.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			_, runErr := nested.Conn().PgConn().Exec(ctx, down).ReadAll()
			if err := nested.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			var pg *pgconn.PgError
			if !errors.As(runErr, &pg) || pg.Code != "55000" || !strings.Contains(pg.Message, "migration 00064 Down") {
				t.Fatalf("the Down over a frozen business day answered %v, want 55000 naming 00064", runErr)
			}
			if after := bdRead(t, ctx, sp); after != head {
				t.Errorf("the refused Down changed the catalogue:\n %+v\nwant HEAD's\n %+v", after, head)
			}
		})
	})

	t.Run("no business day: the Down gives 00063 back and the Up takes it again", func(t *testing.T) {
		savepoint(func(sp pgx.Tx) {
			// A business day another package committed would be evidence this test may not drop -- it
			// is set aside inside the savepoint (the append-only trigger off, NULLed, back on), and the
			// rollback restores it.
			if opInt(t, ctx, sp, `SELECT count(*) FROM transactions WHERE business_day_start IS NOT NULL`) > 0 {
				for _, s := range []string{
					`ALTER TABLE transactions DISABLE TRIGGER transactions_no_mutation`,
					`UPDATE transactions SET business_day_start = NULL WHERE business_day_start IS NOT NULL`,
					`ALTER TABLE transactions ENABLE TRIGGER transactions_no_mutation`,
				} {
					if _, err := sp.Exec(ctx, s); err != nil {
						t.Fatalf("%s: %v", s, err)
					}
				}
			}
			opRunSection(t, ctx, sp, down, "00064 Down")
			at63 := bdRead(t, ctx, sp)
			if at63.columns != "" || at63.constraints != "" || at63.function != "" || at63.tenantsUpdate != "name,business_type,timezone" {
				t.Errorf("after 00064's Down: %+v\nwant no column, no constraint, no function and tenants UPDATE = name,business_type,timezone (00024's list)", at63)
			}
			if extra := strongTableLocksOutside(t, ctx, sp, tables); len(extra) > 0 {
				t.Errorf("the Down took a strong lock on %v, outside the tables it names", extra)
			}
			opRunSection(t, ctx, sp, up, "00064 Up again")
			if again := bdRead(t, ctx, sp); again != head {
				t.Errorf("after the Up again:\n %+v\nwant HEAD's\n %+v", again, head)
			}
		})
	})
}
