package db

// venuewrites_test.go -- the RLS proofs for the SIX statements M6-06 phase A added to
// db/queries/locations.sql and db/queries/departments.sql, at the layer underneath the
// application predicate.
//
// 🔴 THE PROBES HERE CARRY NO tenant_id FILTER, AND THAT IS THE WHOLE DESIGN
// (CLAUDE.md section 6). Production statements MUST carry one -- section 4.5, belt and
// braces -- but an isolation test that carries one proves nothing: a 0 would be the
// WHERE's doing, not RLS's, and the test would stay green with row-level security
// switched off entirely. So each probe identifies the target row by its id alone and
// asks whether the policy hides it. Every negative assertion is paired with a positive
// control in the OWNING tenant, or a broken fixture would look like a working policy.
//
// 🔴 AND THE STATEMENT SHAPE THAT MATTERS HERE IS UPDATE, which rls_test.go does not
// cover for these two tables. It already proves READ isolation
// (TestRLS_ReadIsolation_AllTables) and INSERT forgery refusal
// (TestRLS_WriteWithCheck_AllTables) for locations and departments. Phase A is the
// first task that UPDATES either one, and an UPDATE fails differently: it does not
// raise, it simply matches no rows -- so a policy gap here would be silent in a way an
// INSERT's 42501 never is.
//
// WHAT THESE STATEMENTS CHANGE, so the stakes are on the page rather than in another
// file: static_ips is the IP half of proof of place (section 5, row 6, 50 of the 100
// trust points), gps_lat/gps_lng the backup half, and the shift columns are what
// lateness is measured against. A cross-tenant UPDATE here would not steal data -- it
// would change how another business's taps are JUDGED.
//
// Fixtures are not cleaned up, for the reason store_test.go states.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// rawUpdateCtx runs an UPDATE in one tenant's context and returns how many rows it
// touched. An error is fatal: what is under test is that the statement matches
// NOTHING, which is different from failing.
func rawUpdateCtx(t *testing.T, d *DB, ctxTenant uuid.UUID, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := d.WithTenant(context.Background(), ctxTenant, func(ctx context.Context, tx pgx.Tx) error {
		tag, e := tx.Exec(ctx, query, args...)
		if e != nil {
			return e
		}
		n = tag.RowsAffected()
		return nil
	}); err != nil {
		t.Fatalf("rawUpdateCtx(%q): %v", query, err)
	}
	return n
}

// TestRLS_VenueWrites_UpdateIsolation. B's connection must not be able to rewrite A's
// venue or A's department, and the statements below are the ones the panel runs with
// their tenant predicate REMOVED -- so what is measured is the policy alone.
func TestRLS_VenueWrites_UpdateIsolation(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app) // runtime proof: tappa_app, no super, no bypass

	a := buildFixture(t, app)
	b := buildFixture(t, app)

	cases := []struct {
		name  string
		query string
		arg   any
		// check reads the column the UPDATE aims at, so "0 rows affected" is confirmed
		// by the value still being what it was rather than only by a row count.
		check func(t *testing.T, ctxTenant uuid.UUID) string
		want  string
	}{
		{
			// 🔴 static_ips IS PROOF OF PLACE. Emptying another business's list would send
			// every later tap at that venue down the GPS path, and one with no GPS to
			// `flag` -- a change to how they are policed, from outside their tenant.
			name:  "locations.static_ips",
			query: `UPDATE locations SET static_ips = '{10.66.66.0/24}' WHERE id = $1`,
			arg:   a.locationID,
			check: func(t *testing.T, ctxTenant uuid.UUID) string {
				return rawTextCtx(t, app, ctxTenant,
					`SELECT static_ips::text FROM locations WHERE id = $1`, a.locationID)
			},
			want: "{203.0.113.0/24}",
		},
		{
			// 🔴 THE SHIFT IS WHAT LATENESS IS MEASURED AGAINST. Writing 00:00 into
			// another business's venue would mark every arrival there late, forever.
			name:  "locations.shift",
			query: `UPDATE locations SET shift_start = '00:00', shift_end = '00:00' WHERE id = $1`,
			arg:   a.locationID,
			check: func(t *testing.T, ctxTenant uuid.UUID) string {
				return rawTextCtx(t, app, ctxTenant,
					`SELECT coalesce(shift_start::text, 'NULL') FROM locations WHERE id = $1`,
					a.locationID)
			},
			want: "NULL",
		},
		{
			name:  "locations.name",
			query: `UPDATE locations SET name = 'seized' WHERE id = $1`,
			arg:   a.locationID,
			check: func(t *testing.T, ctxTenant uuid.UUID) string {
				return rawTextCtx(t, app, ctxTenant,
					`SELECT name FROM locations WHERE id = $1`, a.locationID)
			},
			want: "loc",
		},
		{
			// A department's shift BEATS its venue's (section 5, Q17), so this is the
			// same attack one level in.
			name:  "departments.shift",
			query: `UPDATE departments SET shift_start = '00:00', shift_end = '00:00' WHERE id = $1`,
			arg:   a.departmentID,
			check: func(t *testing.T, ctxTenant uuid.UUID) string {
				return rawTextCtx(t, app, ctxTenant,
					`SELECT coalesce(shift_start::text, 'NULL') FROM departments WHERE id = $1`,
					a.departmentID)
			},
			want: "NULL",
		},
		{
			name:  "departments.name",
			query: `UPDATE departments SET name = 'seized' WHERE id = $1`,
			arg:   a.departmentID,
			check: func(t *testing.T, ctxTenant uuid.UUID) string {
				return rawTextCtx(t, app, ctxTenant,
					`SELECT name FROM departments WHERE id = $1`, a.departmentID)
			},
			want: "dep",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// ANTI-VACUITY: the row is where the test thinks it is, and holds what the
			// test thinks it holds. Without this, a fixture that never inserted anything
			// would make both assertions below pass.
			if got := tc.check(t, a.tenantID); got != tc.want {
				t.Fatalf("fixture: A's column reads %q before the attack, want %q; the "+
					"assertions below would prove nothing", got, tc.want)
			}
			// ISOLATION: B's context must match NO rows. Note there is no tenant_id in
			// the WHERE -- a 0 here is the policy, not the predicate.
			if n := rawUpdateCtx(t, app, b.tenantID, tc.query, tc.arg); n != 0 {
				t.Fatalf("RLS FAILED: B's context updated %d of A's rows with %q", n, tc.query)
			}
			if got := tc.check(t, a.tenantID); got != tc.want {
				t.Fatalf("RLS FAILED: A's column now reads %q, want %q", got, tc.want)
			}
			// POSITIVE CONTROL: the SAME statement inside A's own context DOES match.
			// Without it, a typo in the WHERE (or an id that matches nothing) would look
			// exactly like a working policy.
			if n := rawUpdateCtx(t, app, a.tenantID, tc.query, tc.arg); n != 1 {
				t.Fatalf("control: A's own context updated %d rows with %q, want 1 -- the "+
					"isolation assertion above is therefore vacuous", n, tc.query)
			}
		})
	}
}

// TestRLS_VenueWrites_CannotBeStampedWithAnotherTenant. The panel never supplies a
// tenant_id -- it comes from the resolved session -- but the column is writable, and
// what stops an UPDATE from RE-STAMPING a row into another business is the policy's
// WITH CHECK rather than anything in the query text.
//
// ⚠️ THE SHIPPED UpdateLocation DOES NOT NAME tenant_id AT ALL, which is a stronger
// guarantee than this test measures and is stated in db/queries/locations.sql. This
// asserts the floor underneath that choice: if somebody adds the column to a SET list
// tomorrow, the database still refuses.
func TestRLS_VenueWrites_CannotBeStampedWithAnotherTenant(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)

	a := buildFixture(t, app)
	b := buildFixture(t, app)

	for _, tc := range []struct {
		name  string
		query string
		arg   any
	}{
		{"locations", `UPDATE locations SET tenant_id = $2 WHERE id = $1`, b.locationID},
		{"departments", `UPDATE departments SET tenant_id = $2 WHERE id = $1`, b.departmentID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := app.WithTenant(context.Background(), b.tenantID, func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, tc.query, tc.arg, a.tenantID)
				return e
			})
			if err == nil {
				t.Fatalf("RLS FAILED: B moved its own %s row into A's business", tc.name)
			}
			if pg := asPgErr(err); pg == nil || pg.Code != "42501" {
				t.Fatalf("the refusal was %v, want a 42501 row-level security violation", err)
			}
		})
	}
}

// TestRLS_VenueWrites_InsertCannotBeAimedAtAnotherTenantsVenue. CreateDepartment is an
// INSERT ... SELECT over `locations`, so its second defence is that the SELECT finds
// nothing. This measures the FIRST one -- departments_location_fk is a COMPOSITE key
// on (location_id, tenant_id), so the pairing is structurally impossible even with
// every predicate deleted.
//
// ⚠️ FK CHECKS BYPASS RLS, so the foreign location really is resolvable here. What
// refuses this is the composite key itself, and that is the point: it holds when the
// query text does not.
func TestRLS_VenueWrites_InsertCannotBeAimedAtAnotherTenantsVenue(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)

	a := buildFixture(t, app)
	b := buildFixture(t, app)

	// B, in B's own context, stamping a row with B's tenant_id -- so the RLS WITH
	// CHECK is satisfied -- but pointing it at A's venue.
	err := app.WithTenant(context.Background(), b.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO departments (id, tenant_id, location_id, name)
			 VALUES ($1, $2, $3, 'trojan')`, uuid.New(), b.tenantID, a.locationID)
		return e
	})
	if err == nil {
		t.Fatal("a department in B's business was created inside A's venue; the composite " +
			"foreign key is not holding and every employee placed in it would be judged " +
			"against another business's shift")
	}
	if pg := asPgErr(err); pg == nil || pg.Code != "23503" {
		t.Fatalf("the refusal was %v, want a 23503 foreign key violation", err)
	}

	// POSITIVE CONTROL: the same insert at B's OWN venue succeeds, so the refusal above
	// is about the pairing rather than about the statement being broken.
	if err := app.WithTenant(context.Background(), b.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO departments (id, tenant_id, location_id, name)
			 VALUES ($1, $2, $3, 'legit')`, uuid.New(), b.tenantID, b.locationID)
		return e
	}); err != nil {
		t.Fatalf("control: B could not create a department in its own venue: %v", err)
	}
}

// rawTextCtx reads one text value in a tenant's context. It exists beside rawCountCtx
// because a row count says an UPDATE matched nothing and a VALUE says nothing changed
// -- and the second is the claim these tests actually make.
func rawTextCtx(t *testing.T, d *DB, ctxTenant uuid.UUID, query string, arg any) string {
	t.Helper()
	var s string
	if err := d.WithTenant(context.Background(), ctxTenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, query, arg).Scan(&s)
	}); err != nil {
		t.Fatalf("rawTextCtx(%q): %v", query, err)
	}
	return s
}

// TestRLS_VenueWrites_DeleteIsolation.
//
// 🔴 DELETE IS THE THIRD STATEMENT SHAPE AND IT FAILS DIFFERENTLY FROM THE OTHER TWO.
// An INSERT that breaks the policy raises 42501; an UPDATE and a DELETE simply match
// no rows. So a policy gap on DELETE would be SILENT — and the thing it would silently
// destroy is a whole venue belonging to another business.
//
// The probe carries NO tenant predicate (CLAUDE.md section 6), so a 0 here is the
// policy rather than the WHERE.
func TestRLS_VenueWrites_DeleteIsolation(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)

	a := buildFixture(t, app)
	b := buildFixture(t, app)

	// The fixtures' own locations and departments are referenced by employees and
	// transactions, so they could not be deleted even with a broken policy. These two
	// are unreferenced, which is what makes the isolation assertion meaningful: the
	// ONLY thing standing in the way is RLS.
	aVenue, aDept := uuid.New(), uuid.New()
	if err := app.WithTenant(context.Background(), a.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'iso-deletable')`,
			aVenue, a.tenantID); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO departments (id, tenant_id, location_id, name)
			 VALUES ($1, $2, $3, 'iso-deletable-dept')`, aDept, a.tenantID, aVenue)
		return e
	}); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	for _, tc := range []struct {
		name  string
		query string
		arg   any
		table string
	}{
		{"locations", `DELETE FROM locations WHERE id = $1`, aVenue, "locations"},
		{"departments", `DELETE FROM departments WHERE id = $1`, aDept, "departments"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// ISOLATION: B's context must match nothing. Departments first, or the
			// venue delete would fail on its own child rather than on the policy.
			if n := rawUpdateCtx(t, app, b.tenantID, tc.query, tc.arg); n != 0 {
				t.Fatalf("RLS FAILED: B's context deleted %d of A's %s rows", n, tc.table)
			}
			// The row is still there, read in its OWNER's context.
			if got := rawCountCtx(t, app, a.tenantID,
				`SELECT count(*) FROM `+tc.table+` WHERE id = $1`, tc.arg); got != 1 {
				t.Fatalf("RLS FAILED: A's %s row is gone (count %d)", tc.table, got)
			}
		})
	}

	// POSITIVE CONTROL, in dependency order: without it a typo in either WHERE would
	// look exactly like a working policy.
	if n := rawUpdateCtx(t, app, a.tenantID, `DELETE FROM departments WHERE id = $1`, aDept); n != 1 {
		t.Fatalf("control: A deleted %d of its own departments, want 1", n)
	}
	if n := rawUpdateCtx(t, app, a.tenantID, `DELETE FROM locations WHERE id = $1`, aVenue); n != 1 {
		t.Fatalf("control: A deleted %d of its own locations, want 1", n)
	}
}

// TestRLS_VenueWrites_RestrictKeysRefuseARowInUse is the guard the panel's reference
// count is only an ADVERTISEMENT for. It runs as tappa_app against the real keys.
func TestRLS_VenueWrites_RestrictKeysRefuseARowInUse(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	a := buildFixture(t, app)

	for _, tc := range []struct {
		name  string
		query string
		arg   any
	}{
		{"a location with employees, tags and transactions", `DELETE FROM locations WHERE id = $1 AND tenant_id = $2`, a.locationID},
		{"a department with employees", `DELETE FROM departments WHERE id = $1 AND tenant_id = $2`, a.departmentID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := app.WithTenant(context.Background(), a.tenantID, func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, tc.query, tc.arg, a.tenantID)
				return e
			})
			if err == nil {
				t.Fatal("a row in use was deleted; the ON DELETE RESTRICT keys are not holding " +
					"and a mesai record's venue can be edited away (section 4.3)")
			}
			if pg := asPgErr(err); pg == nil || pg.Code != "23503" {
				t.Fatalf("the refusal was %v, want a 23503 foreign key violation", err)
			}
		})
	}
}
